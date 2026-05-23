package main

import "testing"

func withAutohealBootstrapGlobals(t *testing.T) func() {
	t.Helper()
	oldMinStake := ValidatorMinStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldGrace := ValidatorOnboardingGraceBlocks
	oldSlots := ValidatorOnboardingMaxNewSlots
	oldBootEnabled := ValidatorOnboardingBootstrapLaneEnabled
	oldBootSlots := ValidatorOnboardingBootstrapMaxNewSlots
	oldBootStake := ValidatorOnboardingBootstrapRequireStake
	oldBootJailed := ValidatorOnboardingBootstrapRequireNotJailed
	oldMismatchThreshold := ValidatorSetMismatchResyncThreshold
	oldNearTipForce := ValidatorSetAutohealNearTipForceAfter
	oldBanned := append([]string{}, ValidatorBannedList...)
	return func() {
		ValidatorMinStake = oldMinStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		ValidatorOnboardingGraceBlocks = oldGrace
		ValidatorOnboardingMaxNewSlots = oldSlots
		ValidatorOnboardingBootstrapLaneEnabled = oldBootEnabled
		ValidatorOnboardingBootstrapMaxNewSlots = oldBootSlots
		ValidatorOnboardingBootstrapRequireStake = oldBootStake
		ValidatorOnboardingBootstrapRequireNotJailed = oldBootJailed
		ValidatorSetMismatchResyncThreshold = oldMismatchThreshold
		ValidatorSetAutohealNearTipForceAfter = oldNearTipForce
		setValidatorBannedValidators(oldBanned)
	}
}

func TestRecordValidatorSetMismatchNearTipUsesAutohealThreshold(t *testing.T) {
	defer withAutohealBootstrapGlobals(t)()

	ValidatorSetMismatchResyncThreshold = 5
	ValidatorSetAutohealNearTipForceAfter = 2

	n := &Node{}
	if n.recordValidatorSetMismatchWithLocal(100, 101, "exp", "got") {
		t.Fatalf("first near-tip mismatch should not trigger repair")
	}
	if !n.recordValidatorSetMismatchWithLocal(100, 101, "exp", "got") {
		t.Fatalf("second near-tip mismatch should trigger repair with threshold=2")
	}
}

func TestBootstrapLaneAdmitsGraceExpiredStakedValidator(t *testing.T) {
	defer withAutohealBootstrapGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	ValidatorOnboardingBootstrapLaneEnabled = true
	ValidatorOnboardingBootstrapMaxNewSlots = 1
	ValidatorOnboardingBootstrapRequireStake = true
	ValidatorOnboardingBootstrapRequireNotJailed = true
	setValidatorBannedValidators(nil)

	base := []string{"F"}
	signedCounts := map[string]uint64{}
	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:         "F",
			Stake:      100,
			Reputation: 1,
			Status:     ValidatorPending,
			JoinHeight: 100,
		},
	}

	admitted, candidates, decisions := computeValidatorBootstrapLaneAdmissions(base, signedCounts, 1, 300, snapshot, 1)
	if len(admitted) != 1 || admitted[0] != "F" {
		t.Fatalf("expected F admitted via bootstrap lane, got admitted=%v", admitted)
	}
	if len(candidates) != 1 || candidates[0] != "F" {
		t.Fatalf("expected F candidate via bootstrap lane, got candidates=%v", candidates)
	}
	if len(decisions) == 0 || decisions[0].Reason != "bootstrap_lane" {
		t.Fatalf("expected bootstrap_lane reason, got decisions=%v", decisions)
	}
}

func TestBootstrapLaneRejectsJailedWhenRequired(t *testing.T) {
	defer withAutohealBootstrapGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingBootstrapLaneEnabled = true
	ValidatorOnboardingBootstrapMaxNewSlots = 1
	ValidatorOnboardingBootstrapRequireStake = true
	ValidatorOnboardingBootstrapRequireNotJailed = true
	setValidatorBannedValidators(nil)

	base := []string{"F"}
	signedCounts := map[string]uint64{}
	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
			JailUntilHeight: 500,
		},
	}

	admitted, _, decisions := computeValidatorBootstrapLaneAdmissions(base, signedCounts, 1, 300, snapshot, 1)
	if len(admitted) != 0 {
		t.Fatalf("expected jailed validator rejected from bootstrap lane, got admitted=%v", admitted)
	}
	if len(decisions) == 0 || decisions[0].Reason != "jailed" {
		t.Fatalf("expected jailed reason, got decisions=%v", decisions)
	}
}

func TestHandlePersistentPeerDriftTriggersEscalationAtThreshold(t *testing.T) {
	defer withAutohealBootstrapGlobals(t)()

	ValidatorSetMismatchResyncThreshold = 2
	ValidatorSetAutohealNearTipForceAfter = 0

	n := &Node{
		Blockchain: &Blockchain{},
	}

	n.handlePersistentPeerDrift(100, 101, "exp", "got", "validator-set-hash-mismatch-autoheal")
	if n.consensusRecomputePauseActive() {
		t.Fatalf("first persistent peer-drift hit should not pause consensus before threshold")
	}

	n.handlePersistentPeerDrift(100, 101, "exp", "got", "validator-set-hash-mismatch-autoheal")
	if !n.consensusRecomputePauseActive() {
		t.Fatalf("expected consensus pause after persistent peer-drift reaches threshold")
	}

	n.validatorSetMismatchMu.Lock()
	if n.validatorSetMismatchCnt != 0 {
		n.validatorSetMismatchMu.Unlock()
		t.Fatalf("expected mismatch counter reset after threshold trigger, got=%d", n.validatorSetMismatchCnt)
	}
	if n.validatorSetMismatchHeight != 101 {
		h := n.validatorSetMismatchHeight
		n.validatorSetMismatchMu.Unlock()
		t.Fatalf("expected mismatch height=101, got=%d", h)
	}
	n.validatorSetMismatchMu.Unlock()
}

func TestHandlePersistentPeerDriftDoesNotAutoAdoptHash(t *testing.T) {
	defer withAutohealBootstrapGlobals(t)()

	ValidatorSetMismatchResyncThreshold = 2
	ValidatorSetAutohealNearTipForceAfter = 0

	expected := ValidatorSetHash([]string{"A", "B", "C", "D"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100, BlockHash: "h100"}},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			101: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			101: expected,
		},
	}

	n.handlePersistentPeerDrift(100, 101, expected, got, "validator-set-hash-mismatch-autoheal")
	n.handlePersistentPeerDrift(100, 101, expected, got, "validator-set-hash-mismatch-autoheal")

	hash, ok := n.frozenValidatorSetHash(101)
	if !ok {
		t.Fatalf("expected frozen hash to remain available at height 101")
	}
	if hash != expected {
		t.Fatalf("expected no auto-adopt on persistent peer-drift, got=%s want=%s", hash, expected)
	}
}
