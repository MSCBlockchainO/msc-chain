package main

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

var adaptiveReentryBaseIDs = []string{"A", "B", "C", "D", "F"}

func withAdaptiveReentryGlobals(t *testing.T) func() {
	t.Helper()

	oldMode := ValidatorActiveSetMode
	oldMaxCommittee := ValidatorMaxActiveCommittee
	oldLogMult := ValidatorAdaptiveCommitteeLogMult
	oldMinActive := ValidatorMinActiveSet
	oldActivityWindow := ValidatorSelectionActivityWindow
	oldMinSigned := ValidatorSelectionMinSignedBlocks
	oldOnboardingSlots := ValidatorOnboardingMaxNewSlots
	oldOnboardingStrict := ValidatorOnboardingStrictActivation
	oldBootstrapEnabled := ValidatorOnboardingBootstrapLaneEnabled
	oldBootstrapSlots := ValidatorOnboardingBootstrapMaxNewSlots
	oldMinStake := ValidatorMinStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldGenesisFrozen := GenesisValidatorSetFrozen
	oldGenesisFrozenSize := GenesisFrozenValidatorSetSize
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldTTL := ValidatorLivenessHeartbeatTTLSeconds
	oldGrace := ValidatorLivenessGraceSeconds
	oldDrift := ValidatorLivenessMaxHeightDriftBlocks
	oldBanned := append([]string{}, ValidatorBannedList...)
	oldRegistry := GlobalValidatorRegistry.Snapshot()

	runtimeCoreValidatorSet.mu.RLock()
	oldRuntimeCore := make([]string, 0, len(runtimeCoreValidatorSet.ids))
	for id := range runtimeCoreValidatorSet.ids {
		oldRuntimeCore = append(oldRuntimeCore, id)
	}
	runtimeCoreValidatorSet.mu.RUnlock()
	sort.Strings(oldRuntimeCore)

	return func() {
		ValidatorActiveSetMode = oldMode
		ValidatorMaxActiveCommittee = oldMaxCommittee
		ValidatorAdaptiveCommitteeLogMult = oldLogMult
		ValidatorMinActiveSet = oldMinActive
		ValidatorSelectionActivityWindow = oldActivityWindow
		ValidatorSelectionMinSignedBlocks = oldMinSigned
		ValidatorOnboardingMaxNewSlots = oldOnboardingSlots
		ValidatorOnboardingStrictActivation = oldOnboardingStrict
		ValidatorOnboardingBootstrapLaneEnabled = oldBootstrapEnabled
		ValidatorOnboardingBootstrapMaxNewSlots = oldBootstrapSlots
		ValidatorMinStake = oldMinStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		GenesisValidatorSetFrozen = oldGenesisFrozen
		GenesisFrozenValidatorSetSize = oldGenesisFrozenSize
		ConfigAuthCoreValidators = oldCoreValidators
		ValidatorLivenessHeartbeatTTLSeconds = oldTTL
		ValidatorLivenessGraceSeconds = oldGrace
		ValidatorLivenessMaxHeightDriftBlocks = oldDrift
		setValidatorBannedValidators(oldBanned)
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
		GlobalValidatorRegistry.Load(oldRegistry)
	}
}

func configureAdaptiveReentryDefaults() {
	ValidatorActiveSetMode = "adaptive_committee"
	ValidatorMaxActiveCommittee = 512
	ValidatorAdaptiveCommitteeLogMult = 16
	ValidatorMinActiveSet = 3
	ValidatorSelectionActivityWindow = 64
	ValidatorSelectionMinSignedBlocks = 1
	ValidatorOnboardingMaxNewSlots = 0
	ValidatorOnboardingStrictActivation = false
	ValidatorOnboardingBootstrapLaneEnabled = false
	ValidatorOnboardingBootstrapMaxNewSlots = 0
	ValidatorMinStake = 1
	ValidatorCoreStakeExempt = true
	GenesisValidatorSetFrozen = true
	GenesisFrozenValidatorSetSize = 4
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorLivenessHeartbeatTTLSeconds = 25
	ValidatorLivenessGraceSeconds = 10
	ValidatorLivenessMaxHeightDriftBlocks = 8
	setValidatorBannedValidators(nil)
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
}

func loadAdaptiveReentryRegistry(ids []string) {
	snapshot := make(map[string]ValidatorRecord, len(ids))
	for _, id := range ids {
		norm := normalizeValidatorID(id)
		snapshot[norm] = ValidatorRecord{
			ID:         norm,
			Stake:      10,
			Reputation: 1,
			Status:     ValidatorActive,
			JoinHeight: 1,
		}
	}
	GlobalValidatorRegistry.Load(snapshot)
}

func makeAdaptiveReentryNode(height uint64, live map[string]bool, prevFrozen []string) *Node {
	bc := NewBlockchain()
	baseHash := ValidatorSetHash(adaptiveReentryBaseIDs)
	for i, proposer := range []string{"A", "B", "C", "A", "B", "C", "A", "B", "C"} {
		bc.Blocks = append(bc.Blocks, Block{
			ID:                   uint64(i + 1),
			Proposer:             proposer,
			Signatures:           append([]string{}, adaptiveReentryBaseIDs...),
			ValidatorSetHash:     baseHash,
			NextValidatorSetHash: baseHash,
		})
	}

	epoch := map[uint64][]string{
		height: append([]string{}, adaptiveReentryBaseIDs...),
	}
	if height > 1 {
		epoch[height-1] = append([]string{}, adaptiveReentryBaseIDs...)
	}

	n := &Node{
		ID:                       "A",
		Blockchain:               &bc,
		validatorStatus:          make(map[string]*ValidatorStatus),
		epochValidators:          epoch,
		frozenValidatorsByHeight: make(map[uint64][]string),
	}
	if height > 0 && len(prevFrozen) > 0 {
		n.frozenValidatorsByHeight[height-1] = append([]string{}, prevFrozen...)
	}

	now := time.Now()
	for _, id := range adaptiveReentryBaseIDs {
		if !live[normalizeValidatorID(id)] {
			continue
		}
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			Active:             true,
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height,
			ValidatorSetHeight: height,
		}
	}
	return n
}

func TestAdaptiveCommitteeReentryDoesNotUsePostBootstrapCoreFallback(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(10)
	n := makeAdaptiveReentryNode(height, map[string]bool{"D": true}, []string{"A", "B", "C", "D"})

	got := n.getEligibleSortedValidatorIDs(height, nil)
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(canonicalValidatorIDs(got), canonicalValidatorIDs(want)) {
		t.Fatalf("unexpected eligible set: got=%v want=%v", got, want)
	}
}

func TestAdaptiveCommitteeReentrySkipsNonCorePrevMemberWithoutKnownPubKeyWhenStarved(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(10)
	n := makeAdaptiveReentryNode(height, map[string]bool{"D": true, "F": true}, []string{"A", "B", "C", "D", "F"})

	got := n.getEligibleSortedValidatorIDs(height, nil)
	// With strict deterministic eligibility, unknown-pubkey non-core validators
	// are not re-added from runtime state even if they were in the previous set.
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(canonicalValidatorIDs(got), canonicalValidatorIDs(want)) {
		t.Fatalf("unexpected eligible set: got=%v want=%v", got, want)
	}
}

func TestAdaptiveCommitteeReentryRejectsNonCoreWithoutPrevMembership(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(10)
	n := makeAdaptiveReentryNode(height, map[string]bool{"D": true, "F": true}, []string{"A", "B", "C", "D"})

	got := n.getEligibleSortedValidatorIDs(height, nil)
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(canonicalValidatorIDs(got), canonicalValidatorIDs(want)) {
		t.Fatalf("unexpected eligible set: got=%v want=%v", got, want)
	}
}

func TestAdaptiveCommitteeReentryNonCorePrevMemberIsHeightMinusOneOnly(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(10)
	n := makeAdaptiveReentryNode(height, map[string]bool{"D": true, "F": true}, []string{"A", "B", "C", "D"})
	// F was a member at H-2, but not at H-1.
	n.frozenValidatorsByHeight[height-2] = []string{"A", "B", "C", "D", "F"}

	got := n.getEligibleSortedValidatorIDs(height, nil)
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(canonicalValidatorIDs(got), canonicalValidatorIDs(want)) {
		t.Fatalf("expected H-1-only prev-member behavior, got=%v want=%v", got, want)
	}
}

func TestAdaptiveCommitteeReentrySkipsNonLiveValidators(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(10)
	n := makeAdaptiveReentryNode(height, map[string]bool{}, []string{"A", "B", "C", "D", "F"})

	got := n.getEligibleSortedValidatorIDs(height, nil)
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(canonicalValidatorIDs(got), canonicalValidatorIDs(want)) {
		t.Fatalf("unexpected eligible set: got=%v want=%v", got, want)
	}
}

func TestAdaptiveCommitteeReentryBootstrapLaneDoesNotBypassCommittedBase(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	ValidatorOnboardingBootstrapLaneEnabled = true
	ValidatorOnboardingBootstrapMaxNewSlots = 1
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(200)
	n := makeAdaptiveReentryNode(height, map[string]bool{"D": true, "F": true}, []string{"A", "B", "C", "D"})

	got := n.getEligibleSortedValidatorIDs(height, nil)
	if containsNormalizedValidatorID(got, "F") {
		t.Fatalf("did not expect bootstrap lane to bypass committed base membership, got=%v", got)
	}
}

func TestAdaptiveCommitteeReentryDeterministicOrder(t *testing.T) {
	defer withAdaptiveReentryGlobals(t)()
	configureAdaptiveReentryDefaults()
	loadAdaptiveReentryRegistry(adaptiveReentryBaseIDs)

	height := uint64(10)
	var first []string
	var firstHash string

	for i := 0; i < 20; i++ {
		n := makeAdaptiveReentryNode(height, map[string]bool{"D": true, "F": true}, []string{"A", "B", "C", "D", "F"})
		got := n.getEligibleSortedValidatorIDs(height, nil)
		hash := ValidatorSetHash(got)

		if i == 0 {
			first = append([]string{}, got...)
			firstHash = hash
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("eligible set order is not deterministic: first=%v got=%v", first, got)
		}
		if hash != firstHash {
			t.Fatalf("eligible hash is not deterministic: first=%s got=%s", firstHash, hash)
		}
	}
}
