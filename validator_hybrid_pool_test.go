package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func resetHybridValidatorPoolGlobals(t *testing.T) {
	t.Helper()
	oldMode := ValidatorActiveSetMode
	oldMax := ValidatorHybridMaxActiveValidators
	oldPerf := ValidatorHybridPerformanceSlots
	oldRot := ValidatorHybridRotationSlots
	oldCap := ValidatorHybridEffectiveStakeCap
	oldEpoch := ValidatorHybridEpochBlocks
	oldStakeWeight := ValidatorHybridStakeWeight
	oldUptimeWeight := ValidatorHybridUptimeWeight
	oldPerfWeight := ValidatorHybridPerformanceWeight
	oldDecWeight := ValidatorHybridDecentralizationWeight
	oldPerfMinSigned := ValidatorHybridPerformanceMinSignedBPS
	oldPromotionEpochs := ValidatorHybridPromotionWindowEpochs
	oldMinPerformanceAgeEpochs := ValidatorHybridMinimumPerformanceAgeEpochs
	oldPromotionRecordHeight := PromotionWindowRecordV1Height
	oldMinOnlineFull := ValidatorHybridMinimumOnlineWhenFull
	oldDivASN := ValidatorHybridDiversityASNWeight
	oldDivRegion := ValidatorHybridDiversityRegionWeight
	oldDivProvider := ValidatorHybridDiversityProviderWeight
	oldDivHomePC := ValidatorHybridDiversityHomePCWeight
	oldMinStake := ValidatorMinStake
	oldRequireStake := ValidatorRequireStake
	oldBanned := append([]string{}, ValidatorBannedList...)
	t.Cleanup(func() {
		ValidatorActiveSetMode = oldMode
		ValidatorHybridMaxActiveValidators = oldMax
		ValidatorHybridPerformanceSlots = oldPerf
		ValidatorHybridRotationSlots = oldRot
		ValidatorHybridEffectiveStakeCap = oldCap
		ValidatorHybridEpochBlocks = oldEpoch
		ValidatorHybridStakeWeight = oldStakeWeight
		ValidatorHybridUptimeWeight = oldUptimeWeight
		ValidatorHybridPerformanceWeight = oldPerfWeight
		ValidatorHybridDecentralizationWeight = oldDecWeight
		ValidatorHybridPerformanceMinSignedBPS = oldPerfMinSigned
		ValidatorHybridPromotionWindowEpochs = oldPromotionEpochs
		ValidatorHybridMinimumPerformanceAgeEpochs = oldMinPerformanceAgeEpochs
		PromotionWindowRecordV1Height = oldPromotionRecordHeight
		ValidatorHybridMinimumOnlineWhenFull = oldMinOnlineFull
		ValidatorHybridDiversityASNWeight = oldDivASN
		ValidatorHybridDiversityRegionWeight = oldDivRegion
		ValidatorHybridDiversityProviderWeight = oldDivProvider
		ValidatorHybridDiversityHomePCWeight = oldDivHomePC
		ValidatorMinStake = oldMinStake
		ValidatorRequireStake = oldRequireStake
		setValidatorBannedValidators(oldBanned)
	})
	ValidatorActiveSetMode = "hybrid_score_rotation"
	ValidatorHybridMaxActiveValidators = 21
	ValidatorHybridPerformanceSlots = 15
	ValidatorHybridRotationSlots = 6
	ValidatorHybridEffectiveStakeCap = 5_000_000
	ValidatorHybridEpochBlocks = 10_000
	ValidatorHybridStakeWeight = 40
	ValidatorHybridUptimeWeight = 35
	ValidatorHybridPerformanceWeight = 15
	ValidatorHybridDecentralizationWeight = 10
	ValidatorHybridPerformanceMinSignedBPS = 9000
	ValidatorHybridPromotionWindowEpochs = 10
	ValidatorHybridMinimumPerformanceAgeEpochs = 10
	ValidatorHybridMinimumOnlineWhenFull = 15
	ValidatorHybridDiversityASNWeight = 40
	ValidatorHybridDiversityRegionWeight = 30
	ValidatorHybridDiversityProviderWeight = 20
	ValidatorHybridDiversityHomePCWeight = 10
	ValidatorMinStake = 100
	ValidatorRequireStake = true
	setValidatorBannedValidators(nil)
}

func hybridPoolRecord(id string, stake int64, signed, active int) ValidatorRecord {
	activeHeights := make([]uint64, 0, active)
	signedHeights := make([]uint64, 0, signed)
	for i := 0; i < active; i++ {
		h := uint64(i + 1)
		activeHeights = append(activeHeights, h)
		if i < signed {
			signedHeights = append(signedHeights, h)
		}
	}
	rec := ValidatorRecord{
		ID:            normalizeValidatorID(id),
		Stake:         stake,
		Reputation:    1,
		Status:        ValidatorActive,
		JoinHeight:    0,
		LastActive:    uint64(active),
		ActiveHeights: activeHeights,
		SignedHeights: signedHeights,
	}
	rec.UptimeWindowCounter = uint64(active)
	if active >= signed {
		rec.MissedBlocksWindow = uint64(active - signed)
	}
	return rec
}

func hybridPoolSnapshotOf(count int) map[string]ValidatorRecord {
	out := make(map[string]ValidatorRecord, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("V%02d", i+1)
		out[id] = hybridPoolRecord(id, int64(1_000_000+i*1000), 100, 100)
	}
	return out
}

func hybridPoolEntryByID(pool ValidatorPoolSnapshot, id string) (ValidatorPoolEntry, bool) {
	id = normalizeValidatorID(id)
	for _, entry := range pool.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return ValidatorPoolEntry{}, false
}

func TestHybridPoolKeepsAllValidatorsActiveUntilTwentyOne(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	for _, count := range []int{4, 8, 15, 21} {
		selected := selectHybridValidatorsFromRegistrySnapshot(10_000, hybridPoolSnapshotOf(count), "anchor")
		if len(selected) != count {
			t.Fatalf("eligible=%d selected=%d set=%v", count, len(selected), selected)
		}
	}
}

func TestHybridPoolThirtyValidatorsSplitsPerformanceRotationStandby(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	pool := validatorHybridPoolSnapshot(10_000, hybridPoolSnapshotOf(30), "anchor")
	if pool.ActiveCount != 21 || pool.PerformanceActiveCount != 15 || pool.RotationActiveCount != 6 || pool.StandbyCount != 9 {
		t.Fatalf("unexpected pool counts: %+v", pool)
	}
	selected := selectHybridValidatorsFromRegistrySnapshot(10_000, hybridPoolSnapshotOf(30), "anchor")
	if len(selected) != 21 {
		t.Fatalf("expected 21 active validators, got %d: %v", len(selected), selected)
	}
}

func TestHybridPoolEffectiveStakeCapAndHealthyValidatorPreference(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	whale := hybridPoolRecord("WHALE", 50_000_000, 100, 100)
	nearCap := hybridPoolRecord("CAP", 6_000_000, 100, 100)
	whaleScore := validatorHybridScore(&whale)
	nearCapScore := validatorHybridScore(&nearCap)
	if whaleScore.EffectiveStake != 5_000_000 || nearCapScore.EffectiveStake != 5_000_000 {
		t.Fatalf("stake cap not applied: whale=%d near_cap=%d", whaleScore.EffectiveStake, nearCapScore.EffectiveStake)
	}

	slowWhale := hybridPoolRecord("SLOW", 50_000_000, 50, 100)
	healthyCap := hybridPoolRecord("HOME", 5_000_000, 100, 100)
	if validatorHybridScore(&healthyCap).FinalScore <= validatorHybridScore(&slowWhale).FinalScore {
		t.Fatalf("healthy capped validator should outrank larger low-uptime validator")
	}
}

func TestHybridPoolRotationDeterministicAndEpochBucketed(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	snapshot := hybridPoolSnapshotOf(50)
	first := selectHybridValidatorsFromRegistrySnapshot(9_999, snapshot, "anchor")
	second := selectHybridValidatorsFromRegistrySnapshot(9_999, snapshot, "anchor")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("hybrid selection must be deterministic: %v vs %v", first, second)
	}
	before := validatorHybridPoolSnapshot(9_999, snapshot, "anchor")
	after := validatorHybridPoolSnapshot(10_000, snapshot, "anchor")
	if before.EpochBucket == after.EpochBucket {
		t.Fatalf("expected epoch bucket to change at validator_epoch_blocks boundary")
	}
}

func TestHybridPoolExcludesUnsafeValidatorsAndPromotesStandby(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	snapshot := hybridPoolSnapshotOf(30)
	jailed := snapshot["V01"]
	jailed.Status = ValidatorJailed
	jailed.JailUntilHeight = 20_000
	snapshot["V01"] = jailed
	exited := snapshot["V02"]
	exited.Status = ValidatorExited
	snapshot["V02"] = exited
	low := snapshot["V03"]
	low.Stake = ValidatorMinStake - 1
	snapshot["V03"] = low
	future := snapshot["V04"]
	future.JoinHeight = 20_000
	snapshot["V04"] = future
	setValidatorBannedValidators([]string{"V05"})

	selected := selectHybridValidatorsFromRegistrySnapshot(10_000, snapshot, "anchor")
	for _, bad := range []string{"V01", "V02", "V03", "V04", "V05"} {
		if containsValidatorID(selected, bad) {
			t.Fatalf("unsafe validator %s selected in %v", bad, selected)
		}
	}
	if len(selected) != 21 {
		t.Fatalf("expected standby promotion to keep 21 active, got %d: %v", len(selected), selected)
	}
}

func TestHybridPoolOfflinePenaltyDoesNotShrinkBelowTwentyOneUnlessJailed(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	snapshot := hybridPoolSnapshotOf(8)
	rec := snapshot["V01"]
	rec.DisconnectPattern = 10
	rec.MissedBlocksWindow = 10
	snapshot["V01"] = rec
	selected := selectHybridValidatorsFromRegistrySnapshot(10_000, snapshot, "anchor")
	if len(selected) != 8 || !containsValidatorID(selected, "V01") {
		t.Fatalf("offline-only scoring should not shrink low-count active set: selected=%v", selected)
	}
}

func TestHybridPoolPerformanceSlotRequiresNinetyPercentSigning(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	snapshot := hybridPoolSnapshotOf(30)
	lowSigner := hybridPoolRecord("V01", 50_000_000, 89, 100)
	snapshot["V01"] = lowSigner
	pool := validatorHybridPoolSnapshot(10_000, snapshot, "anchor")
	entry, ok := hybridPoolEntryByID(pool, "V01")
	if !ok {
		t.Fatalf("expected V01 in pool")
	}
	if entry.PerformanceEligible || entry.SignedRatioBPS != 8900 {
		t.Fatalf("expected V01 below performance threshold, got eligible=%t signed=%d", entry.PerformanceEligible, entry.SignedRatioBPS)
	}
	if entry.SlotType == "performance" {
		t.Fatalf("below-threshold validator must not receive performance slot: %+v", entry)
	}
	if pool.ActiveCount != 21 {
		t.Fatalf("rotation should fill active set despite performance ineligibility, got active=%d", pool.ActiveCount)
	}
}

func TestHybridPoolPerformanceAgeGateBlocksInstantWhale(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	height := uint64(100_000)
	snapshot := hybridPoolSnapshotOf(30)
	youngWhale := hybridPoolRecord("V01", 50_000_000, 100, 100)
	youngWhale.JoinHeight = height - 500
	snapshot["V01"] = youngWhale

	pool := validatorHybridPoolSnapshot(height, snapshot, "anchor")
	entry, ok := hybridPoolEntryByID(pool, "V01")
	if !ok {
		t.Fatalf("expected young whale to remain in pool")
	}
	if entry.PerformanceAgeEligible || entry.PerformanceEligible {
		t.Fatalf("young validator must not be performance eligible: %+v", entry)
	}
	if !strings.Contains(entry.PerformanceIneligibleReason, "age_below_minimum") {
		t.Fatalf("expected age_below_minimum reason, got %+v", entry)
	}
	if entry.ValidatorAgeBlocks != 500 || entry.ValidatorAgeEpochs != 0 {
		t.Fatalf("unexpected age accounting: %+v", entry)
	}
	if entry.SlotType == "performance" {
		t.Fatalf("young high-stake validator must not receive fixed performance slot: %+v", entry)
	}
}

func TestHybridPoolPerformanceAgeGateAllowsAfterMinimumAge(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	minAgeBlocks := validatorHybridMinimumPerformanceAgeBlocks()
	height := uint64(200_000)
	snapshot := hybridPoolSnapshotOf(30)
	oldWhale := hybridPoolRecord("V01", 50_000_000, 100, 100)
	oldWhale.JoinHeight = height - minAgeBlocks
	snapshot["V01"] = oldWhale

	pool := validatorHybridPoolSnapshot(height, snapshot, "anchor")
	entry, ok := hybridPoolEntryByID(pool, "V01")
	if !ok {
		t.Fatalf("expected aged whale in pool")
	}
	if !entry.PerformanceAgeEligible || !entry.PerformanceEligible {
		t.Fatalf("aged validator should be performance eligible: %+v", entry)
	}
	if entry.ValidatorAgeBlocks != minAgeBlocks || entry.ValidatorAgeEpochs != validatorHybridMinimumPerformanceAgeEpochs() {
		t.Fatalf("unexpected age accounting: %+v", entry)
	}
	if entry.SlotType != "performance" {
		t.Fatalf("aged high-scoring validator should compete into performance slot: %+v", entry)
	}
}

func TestHybridPoolPerformanceAgeGateKeepsAllActiveBelowTwentyOne(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	height := uint64(100_000)
	snapshot := hybridPoolSnapshotOf(8)
	young := snapshot["V01"]
	young.Stake = 50_000_000
	young.JoinHeight = height - 10
	snapshot["V01"] = young

	pool := validatorHybridPoolSnapshot(height, snapshot, "anchor")
	if pool.ActiveCount != 8 {
		t.Fatalf("<=21 eligible validators should all remain active, got pool=%+v", pool)
	}
	entry, ok := hybridPoolEntryByID(pool, "V01")
	if !ok || !entry.Active {
		t.Fatalf("young validator should stay active below 21 total validators: ok=%t entry=%+v", ok, entry)
	}
	if entry.PerformanceAgeEligible || entry.PerformanceEligible {
		t.Fatalf("young validator should still be performance-ineligible below 21: %+v", entry)
	}
}

func TestPromotionWindowRecordSkipsAgeIneligiblePerformanceValidator(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	height := uint64(100_000)
	snapshot := hybridPoolSnapshotOf(30)
	youngWhale := hybridPoolRecord("V01", 50_000_000, 100, 100)
	youngWhale.JoinHeight = height - 500
	snapshot["V01"] = youngWhale
	pool := validatorHybridPoolSnapshot(height, snapshot, "anchor")
	record := buildPromotionWindowRecord(height, pool.Entries, ValidatorRegistrySnapshotHash(snapshot), "anchor")
	if record == nil {
		t.Fatalf("expected promotion window record")
	}
	if containsValidatorID(record.PerformanceValidators, "V01") {
		t.Fatalf("promotion window must not freeze age-ineligible validator into performance set: record=%+v", record)
	}
	entry, ok := hybridPoolEntryByID(pool, "V01")
	if !ok || entry.PerformanceAgeEligible || entry.SlotType == "performance" {
		t.Fatalf("expected V01 age-ineligible and non-performance: ok=%t entry=%+v", ok, entry)
	}
}

func TestPromotionWindowRecordFreezesPerformanceSlotsInsideWindow(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	PromotionWindowRecordV1Height = 100_000
	snapshot := hybridPoolSnapshotOf(30)
	boundaryPool := validatorHybridPoolSnapshot(100_000, snapshot, "anchor-a")
	record := buildPromotionWindowRecord(100_000, boundaryPool.Entries, ValidatorRegistrySnapshotHash(snapshot), "anchor-a")
	if record == nil || len(record.PerformanceValidators) != 15 {
		t.Fatalf("expected committed promotion record with 15 performance validators, got %+v", record)
	}

	changed := copyValidatorRegistrySnapshot(snapshot)
	boostID := "V01"
	if containsValidatorID(record.PerformanceValidators, boostID) {
		t.Fatalf("test setup expected %s outside frozen performance set: %v", boostID, record.PerformanceValidators)
	}
	boosted := changed[boostID]
	boosted.Stake = 50_000_000
	boosted.SignedHeights = append([]uint64{}, boosted.ActiveHeights...)
	boosted.MissedBlocksWindow = 0
	changed[boostID] = boosted
	pool := validatorHybridPoolSnapshotWithPromotionState(100_001, changed, "anchor-a", record, nil, PromotionWindowStateHash(record, nil), "committed_state")
	if !pool.PromotionWindowFrozen {
		t.Fatalf("expected promotion window to remain frozen: %+v", pool)
	}
	for _, id := range record.PerformanceValidators {
		entry, ok := hybridPoolEntryByID(pool, id)
		if !ok || !entry.Active || entry.SlotType != "performance" {
			t.Fatalf("locked performance validator %s not kept in performance slot: ok=%t entry=%+v", id, ok, entry)
		}
	}
	entry, ok := hybridPoolEntryByID(pool, boostID)
	if !ok {
		t.Fatalf("expected boosted validator in pool")
	}
	if entry.SlotType == "performance" && !containsValidatorID(record.PerformanceValidators, boostID) {
		t.Fatalf("mid-window score change must not promote %s into frozen performance set: %+v", boostID, entry)
	}
}

func TestPromotionWindowMissingMidWindowDoesNotInventLocalRecord(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	PromotionWindowRecordV1Height = 100_000
	n := &Node{}
	selected := n.selectHybridValidatorsFromRegistrySnapshot(100_001, hybridPoolSnapshotOf(30), "anchor-a")
	if len(selected) != 0 {
		t.Fatalf("missing committed promotion window record must fail closed, selected=%v", selected)
	}
}

func TestHybridPoolDecentralizationCanBeatDuplicateCloudWhale(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	configureValidatorDiversityForTest(t, []string{
		"A|US|AS7018|HOMEISP|home-1|OP-A|home_pc",
		"B|US|AS14618|AWS|us-east-1|OP-B|false",
		"C|US|AS14618|AWS|us-east-1|OP-C|false",
	})
	snapshot := map[string]ValidatorRecord{
		"A": hybridPoolRecord("A", 4_500_000, 99, 100),
		"B": hybridPoolRecord("B", 50_000_000, 95, 100),
		"C": hybridPoolRecord("C", 1_000_000, 95, 100),
	}
	entries := validatorHybridEligibleEntries(10_000, snapshot)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].ID != "A" {
		t.Fatalf("expected decentralized high-uptime validator A to outrank duplicate-cloud whale, got order=%+v", entries)
	}
	if entries[0].DecentralizationScore <= entries[1].DecentralizationScore {
		t.Fatalf("expected A to have higher decentralization score than B: A=%+v B=%+v", entries[0], entries[1])
	}
}

func TestHybridPoolHomePCBonusRequiresUptimeAndNoSlashes(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	configureValidatorDiversityForTest(t, []string{
		"H1|US|AS7018|HOMEISP|home-1|OP-H1|home_pc",
		"H2|US|AS7922|HOMEISP2|home-2|OP-H2|home_pc",
		"H3|US|AS1239|HOMEISP3|home-3|OP-H3|home_pc",
	})
	snapshot := map[string]ValidatorRecord{
		"H1": hybridPoolRecord("H1", 1_000_000, 95, 100),
		"H2": hybridPoolRecord("H2", 1_000_000, 94, 100),
		"H3": hybridPoolRecord("H3", 1_000_000, 100, 100),
	}
	slashed := snapshot["H3"]
	slashed.TotalSlashes = 1
	snapshot["H3"] = slashed
	entries := validatorHybridEligibleEntries(10_000, snapshot)
	h1, h2, h3 := ValidatorPoolEntry{}, ValidatorPoolEntry{}, ValidatorPoolEntry{}
	for _, entry := range entries {
		switch entry.ID {
		case "H1":
			h1 = entry
		case "H2":
			h2 = entry
		case "H3":
			h3 = entry
		}
	}
	if h1.HomePCScore != 1 {
		t.Fatalf("expected H1 home-PC bonus, got %+v", h1)
	}
	if h2.HomePCScore != 0 {
		t.Fatalf("expected H2 below 95%% uptime to lose home-PC bonus, got %+v", h2)
	}
	if h3.HomePCScore != 0 {
		t.Fatalf("expected slashed H3 to lose home-PC bonus, got %+v", h3)
	}
}

func TestHybridPoolOperatorBonusReducesMultiValidatorOperators(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	configureValidatorDiversityForTest(t, []string{
		"O1|US|AS1001|P1|r1|OP-WHALE|false",
		"O2|DE|AS1002|P2|r2|OP-WHALE|false",
		"O3|SG|AS1003|P3|r3|OP-WHALE|false",
		"S1|FR|AS1004|P4|r4|OP-SOLO|false",
	})
	snapshot := map[string]ValidatorRecord{
		"O1": hybridPoolRecord("O1", 1_000_000, 100, 100),
		"O2": hybridPoolRecord("O2", 1_000_000, 100, 100),
		"O3": hybridPoolRecord("O3", 1_000_000, 100, 100),
		"S1": hybridPoolRecord("S1", 1_000_000, 100, 100),
	}
	entries := validatorHybridEligibleEntries(10_000, snapshot)
	for _, entry := range entries {
		if strings.HasPrefix(entry.ID, "O") && entry.OperatorScore != 0 {
			t.Fatalf("expected 3-validator operator to receive no operator bonus, got %+v", entry)
		}
		if entry.ID == "S1" && entry.OperatorScore != 1 {
			t.Fatalf("expected solo operator to receive full operator score, got %+v", entry)
		}
	}
}

func TestHybridPoolPromotionWindowMetadataAndReplacement(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	snapshot := hybridPoolSnapshotOf(30)
	riser := hybridPoolRecord("V20", 50_000_000, 100, 100)
	snapshot["V20"] = riser
	pool := validatorHybridPoolSnapshot(100_000, snapshot, "anchor")
	entry, ok := hybridPoolEntryByID(pool, "V20")
	if !ok {
		t.Fatalf("expected V20 in pool")
	}
	if entry.SlotType != "performance" || !entry.Active {
		t.Fatalf("expected high-scoring V20 to promote into performance slot at promotion window, got %+v", entry)
	}
	if entry.PromotionWindowBucket != 1 || pool.EpochBucket != 10 {
		t.Fatalf("unexpected promotion/epoch buckets: pool=%+v entry=%+v", pool, entry)
	}
}

func TestHybridPoolMinimumOnlineOnlyAppliesWhenFull(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	if got := validatorHybridMinimumOnlineForActiveCount(20); got != 0 {
		t.Fatalf("minimum-online target should not apply below full active set, got %d", got)
	}
	if got := validatorHybridMinimumOnlineForActiveCount(21); got != 15 {
		t.Fatalf("expected full-set minimum online 15, got %d", got)
	}
	if !validatorHybridMinimumOnlineOK(20, 3) {
		t.Fatalf("below full active set should keep early-mainnet behavior")
	}
	if validatorHybridMinimumOnlineOK(21, 14) {
		t.Fatalf("full active set with 14 online should fail minimum-online target")
	}
}

func TestHybridPoolMinimumPerformanceAgeLoadsFromTOML(t *testing.T) {
	var cfg ConfigFile
	if _, err := toml.Decode("[validators]\nminimum_age_for_performance_slot_epochs = 7\n", &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.Validators.MinimumAgeForPerformanceSlotEpochs != 7 {
		t.Fatalf("expected minimum performance age 7 epochs, got %d", cfg.Validators.MinimumAgeForPerformanceSlotEpochs)
	}
}

func TestConsensusParamsHashIncludesMinimumPerformanceAge(t *testing.T) {
	resetHybridValidatorPoolGlobals(t)
	ValidatorHybridMinimumPerformanceAgeEpochs = 10
	first := consensusParamsHash()
	ValidatorHybridMinimumPerformanceAgeEpochs = 11
	second := consensusParamsHash()
	if first == "" || second == "" {
		t.Fatalf("consensus params hashes must be non-empty")
	}
	if first == second {
		t.Fatalf("changing minimum_age_for_performance_slot_epochs must change consensus params hash")
	}
}
