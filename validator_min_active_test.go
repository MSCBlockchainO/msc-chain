package main

import "testing"

func TestApplyScheduledValidatorUpdatesBlocksBelowMinActive(t *testing.T) {
	prevDeterministic := DeterministicValidatorSelection
	prevDynamic := DynamicValidatorSelectionEnabled
	prevMinActive := ValidatorMinActiveSet
	prevDelay := ValidatorSetActivationDelay
	prevCheckpoint := SyncCheckpointIntervalBlocks
	prevV2 := ValidatorSetActivationModelV2Height
	defer func() {
		DeterministicValidatorSelection = prevDeterministic
		DynamicValidatorSelectionEnabled = prevDynamic
		ValidatorMinActiveSet = prevMinActive
		ValidatorSetActivationDelay = prevDelay
		SyncCheckpointIntervalBlocks = prevCheckpoint
		ValidatorSetActivationModelV2Height = prevV2
	}()

	DeterministicValidatorSelection = false
	DynamicValidatorSelectionEnabled = false
	ValidatorMinActiveSet = 4
	ValidatorSetActivationDelay = 1
	SyncCheckpointIntervalBlocks = 1
	ValidatorSetActivationModelV2Height = 0

	n := &Node{
		epochValidators:          map[uint64][]string{2: {"A", "B", "C", "D"}},
		pendingValidators:        make(map[string]uint64),
		pendingValidatorRemovals: map[string]uint64{"D": 1},
	}

	n.applyScheduledValidatorUpdates(2)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	if _, ok := n.pendingValidatorRemovals["D"]; !ok {
		t.Fatalf("pending removal should remain queued when floor would be violated")
	}
}

func TestApplyScheduledValidatorUpdatesAllowsToMinActive(t *testing.T) {
	prevDeterministic := DeterministicValidatorSelection
	prevDynamic := DynamicValidatorSelectionEnabled
	prevMinActive := ValidatorMinActiveSet
	prevDelay := ValidatorSetActivationDelay
	prevCheckpoint := SyncCheckpointIntervalBlocks
	prevV2 := ValidatorSetActivationModelV2Height
	defer func() {
		DeterministicValidatorSelection = prevDeterministic
		DynamicValidatorSelectionEnabled = prevDynamic
		ValidatorMinActiveSet = prevMinActive
		ValidatorSetActivationDelay = prevDelay
		SyncCheckpointIntervalBlocks = prevCheckpoint
		ValidatorSetActivationModelV2Height = prevV2
	}()

	DeterministicValidatorSelection = false
	DynamicValidatorSelectionEnabled = false
	ValidatorMinActiveSet = 4
	ValidatorSetActivationDelay = 1
	SyncCheckpointIntervalBlocks = 1
	ValidatorSetActivationModelV2Height = 0

	n := &Node{
		epochValidators:          map[uint64][]string{2: {"A", "B", "C", "D", "E"}},
		pendingValidators:        make(map[string]uint64),
		pendingValidatorRemovals: map[string]uint64{"E": 1},
	}

	n.applyScheduledValidatorUpdates(2)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	if _, ok := n.pendingValidatorRemovals["E"]; ok {
		t.Fatalf("pending removal should be cleared after successful apply")
	}
}

func TestApplyScheduledValidatorUpdatesSkipsNoopRemovalPipeline(t *testing.T) {
	prevDeterministic := DeterministicValidatorSelection
	prevDynamic := DynamicValidatorSelectionEnabled
	prevMinActive := ValidatorMinActiveSet
	prevDelay := ValidatorSetActivationDelay
	prevCheckpoint := SyncCheckpointIntervalBlocks
	prevV2 := ValidatorSetActivationModelV2Height
	defer func() {
		DeterministicValidatorSelection = prevDeterministic
		DynamicValidatorSelectionEnabled = prevDynamic
		ValidatorMinActiveSet = prevMinActive
		ValidatorSetActivationDelay = prevDelay
		SyncCheckpointIntervalBlocks = prevCheckpoint
		ValidatorSetActivationModelV2Height = prevV2
	}()

	DeterministicValidatorSelection = false
	DynamicValidatorSelectionEnabled = false
	ValidatorMinActiveSet = 4
	ValidatorSetActivationDelay = 1
	SyncCheckpointIntervalBlocks = 1
	ValidatorSetActivationModelV2Height = 0

	n := &Node{
		epochValidators:          map[uint64][]string{2: {"A", "B", "C", "D"}},
		pendingValidators:        make(map[string]uint64),
		pendingValidatorRemovals: map[string]uint64{"Z": 1},
	}

	n.applyScheduledValidatorUpdates(2)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	if _, ok := n.pendingValidatorRemovals["Z"]; ok {
		t.Fatalf("noop removal should still be cleared once due")
	}
	if len(n.currentValidators) != 0 {
		t.Fatalf("noop removal should not rewrite current validator cache, got=%v", n.currentValidators)
	}
}

func TestDeterministicInactiveRemovalIgnoresProposalValidatorSetSignatures(t *testing.T) {
	prevDeterministic := DeterministicValidatorSelection
	prevDynamic := DynamicValidatorSelectionEnabled
	prevInactive := ValidatorInactiveBlocks
	prevMinActive := ValidatorMinActiveSet
	prevDelay := ValidatorSetActivationDelay
	prevV2 := ValidatorSetCommitmentV2Height
	defer func() {
		DeterministicValidatorSelection = prevDeterministic
		DynamicValidatorSelectionEnabled = prevDynamic
		ValidatorInactiveBlocks = prevInactive
		ValidatorMinActiveSet = prevMinActive
		ValidatorSetActivationDelay = prevDelay
		ValidatorSetCommitmentV2Height = prevV2
	}()

	DeterministicValidatorSelection = true
	DynamicValidatorSelectionEnabled = false
	ValidatorInactiveBlocks = 4
	ValidatorMinActiveSet = 3
	ValidatorSetActivationDelay = 1
	ValidatorSetCommitmentV2Height = 1

	validators := []string{"A", "B", "C", "D"}
	setHash := ValidatorSetHash(validators)
	bc := NewBlockchain()
	proposers := []string{"A", "B", "C", "A", "B", "C", "A", "B"}
	for i, proposer := range proposers {
		height := uint64(i + 1)
		bc.AddBlock(Block{
			ID:                   height,
			Proposer:             proposer,
			Signatures:           append([]string{}, validators...),
			ValidatorSetHash:     setHash,
			NextValidatorSetHash: setHash,
		})
	}
	n := &Node{
		Blockchain:                  &bc,
		GenesisValidators:           append([]string{}, validators...),
		epochValidators:             map[uint64][]string{8: append([]string{}, validators...)},
		frozenValidatorsByHeight:    map[uint64][]string{8: append([]string{}, validators...)},
		frozenValidatorHashByHeight: map[uint64]string{8: setHash},
		pendingValidators:           make(map[string]uint64),
		pendingValidatorRemovals:    make(map[string]uint64),
	}

	n.queueDeterministicInactiveRemovals(8)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	if got := n.pendingValidatorRemovals["D"]; got != 9 {
		t.Fatalf("expected inactive D removal at height 9, got=%d pending=%v", got, n.pendingValidatorRemovals)
	}
	for _, active := range []string{"A", "B", "C"} {
		if _, removed := n.pendingValidatorRemovals[active]; removed {
			t.Fatalf("active validator %s should not be queued for removal: %v", active, n.pendingValidatorRemovals)
		}
	}
}
