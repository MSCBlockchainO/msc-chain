package main

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func withOnboardingTestGlobals(t *testing.T) func() {
	t.Helper()
	oldMinStake := ValidatorMinStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldGrace := ValidatorOnboardingGraceBlocks
	oldSlots := ValidatorOnboardingMaxNewSlots
	oldBanned := append([]string{}, ValidatorBannedList...)
	return func() {
		ValidatorMinStake = oldMinStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		ValidatorOnboardingGraceBlocks = oldGrace
		ValidatorOnboardingMaxNewSlots = oldSlots
		setValidatorBannedValidators(oldBanned)
	}
}

func TestEvaluateValidatorOnboardingWithinGrace(t *testing.T) {
	defer withOnboardingTestGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	setValidatorBannedValidators(nil)

	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      200,
		},
	}

	eval := evaluateValidatorOnboardingFromSnapshot("F", 220, snapshot)
	if !eval.Eligible {
		t.Fatalf("expected onboarding eligibility inside grace window, got reason=%s", eval.Reason)
	}
	if eval.Reason != "within_grace" {
		t.Fatalf("unexpected reason: %s", eval.Reason)
	}
	if eval.GraceUntil != 264 {
		t.Fatalf("unexpected grace_until: got=%d want=264", eval.GraceUntil)
	}
}

func TestEvaluateValidatorOnboardingGraceExpired(t *testing.T) {
	defer withOnboardingTestGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	setValidatorBannedValidators(nil)

	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      200,
		},
	}

	eval := evaluateValidatorOnboardingFromSnapshot("F", 300, snapshot)
	if eval.Eligible {
		t.Fatalf("expected onboarding ineligible after grace expiry")
	}
	if eval.Reason != "grace_expired" {
		t.Fatalf("unexpected reason: %s", eval.Reason)
	}
}

func TestComputeValidatorOnboardingAdmissionsSlotCapDeterministic(t *testing.T) {
	defer withOnboardingTestGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	ValidatorOnboardingMaxNewSlots = 1
	setValidatorBannedValidators(nil)

	base := []string{"Z", "G", "F"} // canonical order should prefer F first.
	signedCounts := map[string]uint64{}
	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
		},
		"G": {
			ID:              "G",
			ConsensusPubKey: strings.Repeat("22", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
		},
		"Z": {
			ID:              "Z",
			ConsensusPubKey: strings.Repeat("33", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
		},
	}

	admitted, candidates, decisions := computeValidatorOnboardingAdmissions(base, signedCounts, 1, 120, snapshot, 1)
	if len(admitted) != 1 || admitted[0] != "F" {
		t.Fatalf("expected deterministic first admission for F, got admitted=%v", admitted)
	}
	if len(candidates) != 3 || candidates[0] != "F" || candidates[1] != "G" || candidates[2] != "Z" {
		t.Fatalf("unexpected candidates ordering: %v", candidates)
	}

	reasons := map[string]string{}
	for _, d := range decisions {
		reasons[d.ID] = d.Reason
	}
	if reasons["F"] != "within_grace" {
		t.Fatalf("expected F within_grace, got=%s", reasons["F"])
	}
	if reasons["G"] != "slots_full" || reasons["Z"] != "slots_full" {
		t.Fatalf("expected remaining candidates to be slots_full, got G=%s Z=%s", reasons["G"], reasons["Z"])
	}
}

func TestComputeValidatorOnboardingAdmissionsRejectsJailedExited(t *testing.T) {
	defer withOnboardingTestGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	setValidatorBannedValidators(nil)

	base := []string{"F", "G"}
	signedCounts := map[string]uint64{}
	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
			JailUntilHeight: 200,
		},
		"G": {
			ID:              "G",
			ConsensusPubKey: strings.Repeat("22", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorExited,
			JoinHeight:      100,
		},
	}

	admitted, _, decisions := computeValidatorOnboardingAdmissions(base, signedCounts, 1, 120, snapshot, 2)
	if len(admitted) != 0 {
		t.Fatalf("expected no admissions for jailed/exited validators, got=%v", admitted)
	}

	reasons := map[string]string{}
	for _, d := range decisions {
		reasons[d.ID] = d.Reason
	}
	if reasons["F"] != "jailed" {
		t.Fatalf("expected F jailed reason, got=%s", reasons["F"])
	}
	if reasons["G"] != "exited" {
		t.Fatalf("expected G exited reason, got=%s", reasons["G"])
	}
}

func TestComputeValidatorOnboardingAdmissionsSkipsUnanchoredPubkeyCandidates(t *testing.T) {
	defer withOnboardingTestGlobals(t)()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	ValidatorOnboardingMaxNewSlots = 1
	setValidatorBannedValidators(nil)

	base := []string{"F", "G"}
	signedCounts := map[string]uint64{}
	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:         "F",
			Stake:      100,
			Reputation: 1,
			Status:     ValidatorPending,
			JoinHeight: 100,
		},
		"G": {
			ID:              "G",
			ConsensusPubKey: strings.Repeat("22", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
		},
	}

	admitted, candidates, decisions := computeValidatorOnboardingAdmissions(base, signedCounts, 1, 120, snapshot, 1)
	if len(admitted) != 1 || admitted[0] != "G" {
		t.Fatalf("expected only anchored validator G to consume the slot, got admitted=%v", admitted)
	}
	if len(candidates) != 1 || candidates[0] != "G" {
		t.Fatalf("expected only anchored validator G in candidate list, got=%v", candidates)
	}

	reasons := map[string]string{}
	for _, d := range decisions {
		reasons[d.ID] = d.Reason
	}
	if reasons["F"] != "awaiting_pubkey" {
		t.Fatalf("expected F awaiting_pubkey, got=%s", reasons["F"])
	}
	if reasons["G"] != "within_grace" {
		t.Fatalf("expected G within_grace, got=%s", reasons["G"])
	}
}

func TestComputeValidatorBootstrapLaneAdmissionsSkipsUnanchoredPubkeyCandidates(t *testing.T) {
	defer withOnboardingTestGlobals(t)()

	oldBootstrapEnabled := ValidatorOnboardingBootstrapLaneEnabled
	oldBootstrapSlots := ValidatorOnboardingBootstrapMaxNewSlots
	oldBootstrapStake := ValidatorOnboardingBootstrapRequireStake
	oldBootstrapJailed := ValidatorOnboardingBootstrapRequireNotJailed
	t.Cleanup(func() {
		ValidatorOnboardingBootstrapLaneEnabled = oldBootstrapEnabled
		ValidatorOnboardingBootstrapMaxNewSlots = oldBootstrapSlots
		ValidatorOnboardingBootstrapRequireStake = oldBootstrapStake
		ValidatorOnboardingBootstrapRequireNotJailed = oldBootstrapJailed
	})

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingGraceBlocks = 64
	ValidatorOnboardingBootstrapLaneEnabled = true
	ValidatorOnboardingBootstrapMaxNewSlots = 1
	ValidatorOnboardingBootstrapRequireStake = true
	ValidatorOnboardingBootstrapRequireNotJailed = true
	setValidatorBannedValidators(nil)

	base := []string{"F", "G"}
	signedCounts := map[string]uint64{}
	snapshot := map[string]ValidatorRecord{
		"F": {
			ID:         "F",
			Stake:      100,
			Reputation: 1,
			Status:     ValidatorPending,
			JoinHeight: 100,
		},
		"G": {
			ID:              "G",
			ConsensusPubKey: strings.Repeat("22", ed25519.PublicKeySize),
			Stake:           100,
			Reputation:      1,
			Status:          ValidatorPending,
			JoinHeight:      100,
		},
	}

	admitted, candidates, decisions := computeValidatorBootstrapLaneAdmissions(base, signedCounts, 1, 300, snapshot, 1)
	if len(admitted) != 1 || admitted[0] != "G" {
		t.Fatalf("expected only anchored validator G to consume bootstrap lane slot, got admitted=%v", admitted)
	}
	if len(candidates) != 1 || candidates[0] != "G" {
		t.Fatalf("expected only anchored validator G in bootstrap candidates, got=%v", candidates)
	}

	reasons := map[string]string{}
	for _, d := range decisions {
		reasons[d.ID] = d.Reason
	}
	if reasons["F"] != "awaiting_pubkey" {
		t.Fatalf("expected F awaiting_pubkey, got=%s", reasons["F"])
	}
	if reasons["G"] != "bootstrap_lane" {
		t.Fatalf("expected G bootstrap_lane, got=%s", reasons["G"])
	}
}
