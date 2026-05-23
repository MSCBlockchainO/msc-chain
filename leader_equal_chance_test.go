package main

import (
	"testing"
)

func TestLeaderForHeightVRFDeterministicAcrossCalls(t *testing.T) {
	validators := []string{"A", "B", "C", "D", "E"}
	for h := uint64(1); h <= 200; h++ {
		a := LeaderForHeight(h, validators)
		b := LeaderForHeight(h, validators)
		if a == "" {
			t.Fatalf("empty leader at height=%d", h)
		}
		if a != b {
			t.Fatalf("non-deterministic leader at h=%d: %s vs %s", h, a, b)
		}
	}
}

func TestLeaderForHeightVRFDeterministicAcrossInputOrder(t *testing.T) {
	a := []string{"A", "B", "C", "D", "E"}
	b := []string{"E", "C", "A", "D", "B"}

	for h := uint64(1); h <= 100; h++ {
		la := LeaderForHeight(h, a)
		lb := LeaderForHeight(h, b)
		if la != lb {
			t.Fatalf("non-deterministic leader at h=%d: %s vs %s", h, la, lb)
		}
	}
}

func TestLeaderForHeightVRFUsesAllValidatorsOverWindow(t *testing.T) {
	validators := []string{"A", "B", "C", "D", "E"}
	seen := make(map[string]bool, len(validators))
	for h := uint64(1); h <= 4096; h++ {
		seen[LeaderForHeight(h, validators)] = true
	}
	for _, id := range validators {
		if !seen[id] {
			t.Fatalf("validator %s never selected in VRF window", id)
		}
	}
}

func TestGetConsensusLeaderFromListUsesVRFSelection(t *testing.T) {
	validators := []string{"A", "B", "C", "D", "E"}
	for h := 0; h <= 200; h++ {
		got := GetConsensusLeaderFromList(append([]string{}, validators...), h)
		want := LeaderForHeight(uint64(h), validators)
		if got != want {
			t.Fatalf("unexpected proposer source at h=%d: got=%s want=%s", h, got, want)
		}
	}
}
