package main

import "testing"

func TestValidatorSetHashCanonicalCaseInsensitive(t *testing.T) {
	a := []string{"c", "A", " b ", "a", ""}
	b := []string{"B", "C", "A"}

	ha := ValidatorSetHash(a)
	hb := ValidatorSetHash(b)
	if ha == "" || hb == "" {
		t.Fatalf("validator set hash should not be empty: ha=%q hb=%q", ha, hb)
	}
	if ha != hb {
		t.Fatalf("canonical hash mismatch (case-insensitive): ha=%s hb=%s", ha, hb)
	}
}

func TestQueuePendingValidatorNormalizesID(t *testing.T) {
	n := &Node{}

	n.queuePendingValidator("a", 10)
	n.queuePendingValidator("A", 5)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()

	if len(n.pendingValidators) != 1 {
		t.Fatalf("expected one normalized pending validator, got=%v", n.pendingValidators)
	}
	act, ok := n.pendingValidators["A"]
	if !ok {
		t.Fatalf("expected normalized key A, got=%v", n.pendingValidators)
	}
	if act != 5 {
		t.Fatalf("expected earliest activation height 5, got=%d", act)
	}
}

func TestQueuePendingValidatorRemovalNormalizesID(t *testing.T) {
	n := &Node{}

	n.queuePendingValidator("b", 9)
	n.queuePendingValidatorRemoval(" B ", 7)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()

	if len(n.pendingValidators) != 0 {
		t.Fatalf("expected pending add to be cleared by removal, got=%v", n.pendingValidators)
	}
	if len(n.pendingValidatorRemovals) != 1 {
		t.Fatalf("expected one normalized pending removal, got=%v", n.pendingValidatorRemovals)
	}
	act, ok := n.pendingValidatorRemovals["B"]
	if !ok {
		t.Fatalf("expected normalized remove key B, got=%v", n.pendingValidatorRemovals)
	}
	if act != 7 {
		t.Fatalf("expected removal activation height 7, got=%d", act)
	}
}

func TestApplySnapshotValidatorTransitionsNormalizesIDs(t *testing.T) {
	n := &Node{}
	snap := StateSnapshot{
		Height: 10,
		PendingValidators: map[string]uint64{
			"a": 15,
			"A": 12,
		},
		PendingValidatorRemovals: map[string]uint64{
			" b ": 20,
			"B":   18,
		},
		ValidatorSetHeight: 10,
	}

	n.applySnapshotValidatorTransitions(snap)

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()

	if len(n.pendingValidators) != 1 || n.pendingValidators["A"] != 12 {
		t.Fatalf("unexpected normalized pending validators: %v", n.pendingValidators)
	}
	if len(n.pendingValidatorRemovals) != 1 || n.pendingValidatorRemovals["B"] != 18 {
		t.Fatalf("unexpected normalized pending removals: %v", n.pendingValidatorRemovals)
	}
}
