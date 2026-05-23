package main

import "testing"

func TestComputeValidatorLifecycleAliasCounts(t *testing.T) {
	registry := map[string]ValidatorRecord{
		"P": {ID: "P", Status: ValidatorPending},
		"A": {ID: "A", Status: ValidatorActive},
		"I": {ID: "I", Status: ValidatorActive},
		"J": {ID: "J", Status: ValidatorJailed},
		"R": {ID: "R", Status: ValidatorExited},
	}
	pendingRemovals := map[string]uint64{
		"I": 101,
	}

	got := computeValidatorLifecycleAliasCounts(100, registry, pendingRemovals)
	if got.Pending != 1 {
		t.Fatalf("pending mismatch: got=%d want=1", got.Pending)
	}
	if got.Active != 1 {
		t.Fatalf("active mismatch: got=%d want=1", got.Active)
	}
	if got.Inactive != 1 {
		t.Fatalf("inactive mismatch: got=%d want=1", got.Inactive)
	}
	if got.Slashed != 1 {
		t.Fatalf("slashed mismatch: got=%d want=1", got.Slashed)
	}
	if got.Removed != 1 {
		t.Fatalf("removed mismatch: got=%d want=1", got.Removed)
	}
}
