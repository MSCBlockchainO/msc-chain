package main

import "testing"

func TestSelectDeterministicValidatorsVRFIgnoresStake(t *testing.T) {
	oldEqual := ValidatorEqualChanceSelection
	oldWindow := ValidatorSetRotationWindow
	oldMinStake := ValidatorMinStake
	defer func() {
		ValidatorEqualChanceSelection = oldEqual
		ValidatorSetRotationWindow = oldWindow
		ValidatorMinStake = oldMinStake
	}()

	ValidatorEqualChanceSelection = true
	ValidatorSetRotationWindow = 10
	ValidatorMinStake = 100

	snapshotA := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 1000, Status: ValidatorActive},
		"C": {ID: "C", Stake: 5000, Status: ValidatorActive},
		"D": {ID: "D", Stake: 250, Status: ValidatorActive},
	}
	snapshotB := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100_000, Status: ValidatorActive},
		"B": {ID: "B", Stake: 1_000_000, Status: ValidatorActive},
		"C": {ID: "C", Stake: 5_000_000, Status: ValidatorActive},
		"D": {ID: "D", Stake: 250_000, Status: ValidatorActive},
	}

	got1 := selectDeterministicValidatorsFromSnapshot(10, 2, snapshotA)
	got2 := selectDeterministicValidatorsFromSnapshot(10, 2, snapshotA)
	if len(got1) != 2 {
		t.Fatalf("unexpected set size: got=%v", got1)
	}
	if got1[0] != got2[0] || got1[1] != got2[1] {
		t.Fatalf("non-deterministic output: %v vs %v", got1, got2)
	}
	gotStakeShifted := selectDeterministicValidatorsFromSnapshot(10, 2, snapshotB)
	if got1[0] != gotStakeShifted[0] || got1[1] != gotStakeShifted[1] {
		t.Fatalf("stake should not influence VRF deterministic selection: %v vs %v", got1, gotStakeShifted)
	}
}

func TestSelectDeterministicValidatorsVRFDeterministicAndOrderIndependent(t *testing.T) {
	oldEqual := ValidatorEqualChanceSelection
	oldWindow := ValidatorSetRotationWindow
	oldMinStake := ValidatorMinStake
	defer func() {
		ValidatorEqualChanceSelection = oldEqual
		ValidatorSetRotationWindow = oldWindow
		ValidatorMinStake = oldMinStake
	}()

	ValidatorEqualChanceSelection = true
	ValidatorSetRotationWindow = 50
	ValidatorMinStake = 100

	snapshotA := map[string]ValidatorRecord{
		"B": {ID: "B", Stake: 1000, Status: ValidatorActive},
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 10000, Status: ValidatorActive},
		"C": {ID: "C", Stake: 250, Status: ValidatorActive},
	}
	snapshotB := map[string]ValidatorRecord{
		"D": {ID: "D", Stake: 10000, Status: ValidatorActive},
		"C": {ID: "C", Stake: 250, Status: ValidatorActive},
		"B": {ID: "B", Stake: 1000, Status: ValidatorActive},
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	}

	first := selectDeterministicValidatorsFromSnapshot(151, 3, snapshotA)
	second := selectDeterministicValidatorsFromSnapshot(151, 3, snapshotA)
	third := selectDeterministicValidatorsFromSnapshot(151, 3, snapshotB)

	if len(first) != len(second) {
		t.Fatalf("set length mismatch: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("non-deterministic output: %v vs %v", first, second)
		}
		if first[i] != third[i] {
			t.Fatalf("order-dependent output: %v vs %v", first, third)
		}
	}
}

func TestSelectDeterministicValidatorsVRFStableInsideRotationBucket(t *testing.T) {
	oldEqual := ValidatorEqualChanceSelection
	oldWindow := ValidatorSetRotationWindow
	oldMinStake := ValidatorMinStake
	defer func() {
		ValidatorEqualChanceSelection = oldEqual
		ValidatorSetRotationWindow = oldWindow
		ValidatorMinStake = oldMinStake
	}()

	ValidatorEqualChanceSelection = true
	ValidatorSetRotationWindow = 50
	ValidatorMinStake = 100

	snapshot := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive},
		"E": {ID: "E", Stake: 100, Status: ValidatorActive},
	}

	inBucketA := selectDeterministicValidatorsFromSnapshot(151, 3, snapshot)
	inBucketB := selectDeterministicValidatorsFromSnapshot(199, 3, snapshot)
	if len(inBucketA) != 3 || len(inBucketB) != 3 {
		t.Fatalf("unexpected output sizes: %v %v", inBucketA, inBucketB)
	}
	for i := range inBucketA {
		if inBucketA[i] != inBucketB[i] {
			t.Fatalf("selection should stay stable inside rotation bucket: %v vs %v", inBucketA, inBucketB)
		}
	}
}
