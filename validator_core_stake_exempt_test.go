package main

import "testing"

func snapshotRuntimeCoreIDs() []string {
	runtimeCoreValidatorSet.mu.RLock()
	defer runtimeCoreValidatorSet.mu.RUnlock()
	out := make([]string, 0, len(runtimeCoreValidatorSet.ids))
	for id := range runtimeCoreValidatorSet.ids {
		out = append(out, id)
	}
	return canonicalValidatorIDs(out)
}

func TestValidatorPassesStakeGateIgnoresCoreMembershipPostBootstrap(t *testing.T) {
	oldMinStake := ValidatorMinStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRuntimeCore := snapshotRuntimeCoreIDs()
	defer func() {
		ValidatorMinStake = oldMinStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
	}()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A"}
	setRuntimeCoreValidatorIDs([]string{"A"})

	if validatorPassesStakeGate("A", 0) {
		t.Fatalf("expected former core validator to fail below min stake")
	}
	if validatorPassesStakeGate("F", 99) {
		t.Fatalf("expected non-core validator to fail below min stake")
	}
	if !validatorPassesStakeGate("G", 100) {
		t.Fatalf("expected validator meeting min stake to pass")
	}
}

func TestValidatorStateMachineRequiresStakeForFormerCore(t *testing.T) {
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldMinStake := ValidatorMinStake
	oldRecovery := ValidatorReputationRecoveryThreshold
	oldRuntimeCore := snapshotRuntimeCoreIDs()
	defer func() {
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		ValidatorMinStake = oldMinStake
		ValidatorReputationRecoveryThreshold = oldRecovery
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
	}()

	ValidatorMinStake = 100
	ValidatorReputationRecoveryThreshold = 0.30
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A"}
	setRuntimeCoreValidatorIDs([]string{"A"})

	rec := &ValidatorRecord{
		ID:         "A",
		Stake:      0,
		Reputation: 1,
		Status:     ValidatorPending,
	}

	ValidatorStateMachine{}.Update(rec, 10)
	if rec.Status != ValidatorPending {
		t.Fatalf("expected validator without stake to remain pending, got=%s", rec.Status)
	}
}

func TestSelectDeterministicValidatorsFromSnapshotRequiresStakeForAllValidators(t *testing.T) {
	oldEqual := ValidatorEqualChanceSelection
	oldWindow := ValidatorSetRotationWindow
	oldMinStake := ValidatorMinStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRuntimeCore := snapshotRuntimeCoreIDs()
	defer func() {
		ValidatorEqualChanceSelection = oldEqual
		ValidatorSetRotationWindow = oldWindow
		ValidatorMinStake = oldMinStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
	}()

	ValidatorEqualChanceSelection = true
	ValidatorSetRotationWindow = 10
	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A"}
	setRuntimeCoreValidatorIDs([]string{"A"})

	snapshot := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 0, Status: ValidatorActive},
		"F": {ID: "F", Stake: 99, Status: ValidatorActive},
		"G": {ID: "G", Stake: 100, Status: ValidatorActive},
	}

	got := selectDeterministicValidatorsFromSnapshot(10, 10, snapshot)
	if len(got) != 1 || got[0] != "G" {
		t.Fatalf("expected only staked validators to be selected, got=%v", got)
	}
}

func TestValidatorStakeGateTransitionTreatsFormerCoreLikeAnyValidator(t *testing.T) {
	oldMinStake := ValidatorMinStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRuntimeCore := snapshotRuntimeCoreIDs()
	defer func() {
		ValidatorMinStake = oldMinStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
	}()

	ValidatorMinStake = 100
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A"}
	setRuntimeCoreValidatorIDs([]string{"A"})

	lost, gained := validatorStakeGateTransition("A", 100, 0)
	if !lost || gained {
		t.Fatalf("expected stake drop to trigger removal transition: lost=%t gained=%t", lost, gained)
	}
}

func TestValidatorStakeStatusBootstrapExemptionOnly(t *testing.T) {
	oldRequireStake := ValidatorRequireStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRuntimeCore := snapshotRuntimeCoreIDs()
	defer func() {
		ValidatorRequireStake = oldRequireStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
	}()

	ValidatorRequireStake = true
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A"}
	setRuntimeCoreValidatorIDs([]string{"A"})

	bc := NewBlockchain()
	node := &Node{
		ID:         "A",
		Role:       "validator",
		Blockchain: &bc,
		Ledger:     GenesisLedger(),
	}
	_, _, eligible, reason := validatorStakeStatus(node, "MSC_TEST_WALLET", "A")
	if !eligible {
		t.Fatalf("expected bootstrap core validator to remain eligible without stake")
	}
	if reason != "core stake exemption" {
		t.Fatalf("unexpected bootstrap exemption reason: %q", reason)
	}
}

func TestValidatorStakeStatusExistingChainHasNoCoreExemption(t *testing.T) {
	oldRequireStake := ValidatorRequireStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRuntimeCore := snapshotRuntimeCoreIDs()
	defer func() {
		ValidatorRequireStake = oldRequireStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
	}()

	ValidatorRequireStake = true
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A"}
	setRuntimeCoreValidatorIDs([]string{"A"})

	node := &Node{
		ID:         "A",
		Role:       "validator",
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1}}},
		Ledger:     GenesisLedger(),
	}
	_, _, eligible, reason := validatorStakeStatus(node, "MSC_TEST_WALLET", "A")
	if eligible {
		t.Fatalf("expected existing-chain validator to require stake")
	}
	if reason == "core stake exemption" {
		t.Fatalf("did not expect bootstrap-only exemption on existing chain")
	}
}
