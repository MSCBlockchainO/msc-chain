package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestAdaptiveCommitteeTargetBounds(t *testing.T) {
	oldMode := ValidatorActiveSetMode
	oldMax := ValidatorMaxActiveCommittee
	oldMult := ValidatorAdaptiveCommitteeLogMult
	oldMin := ValidatorMinActiveSet
	defer func() {
		ValidatorActiveSetMode = oldMode
		ValidatorMaxActiveCommittee = oldMax
		ValidatorAdaptiveCommitteeLogMult = oldMult
		ValidatorMinActiveSet = oldMin
	}()

	ValidatorActiveSetMode = "adaptive_committee"
	ValidatorMaxActiveCommittee = 512
	ValidatorAdaptiveCommitteeLogMult = 16
	ValidatorMinActiveSet = 3

	n := &Node{}
	if got := n.adaptiveCommitteeTarget(3); got != 3 {
		t.Fatalf("expected committee target=3 for eligible=3, got=%d", got)
	}

	got := n.adaptiveCommitteeTarget(10_000_000)
	if got <= 0 || got > 512 {
		t.Fatalf("committee target out of bounds for 10M eligible: got=%d", got)
	}
}

func TestAdaptiveCommitteeDeterministic(t *testing.T) {
	oldChainID := ChainID
	defer func() { ChainID = oldChainID }()
	ChainID = "test-chain"

	eligible := make([]string, 0, 128)
	for i := 0; i < 128; i++ {
		eligible = append(eligible, fmt.Sprintf("V%03d", i))
	}

	n1 := &Node{}
	n2 := &Node{}

	c1 := n1.buildAdaptiveCommittee(1024, eligible, 32)
	c2 := n2.buildAdaptiveCommittee(1024, eligible, 32)

	if !reflect.DeepEqual(c1, c2) {
		t.Fatalf("committee selection is not deterministic")
	}
	if len(c1) != 32 {
		t.Fatalf("unexpected committee size: got=%d want=32", len(c1))
	}
}

func TestAdaptiveCommitteeVRFOrderIndependent(t *testing.T) {
	oldChainID := ChainID
	oldRotation := ValidatorCommitteeRotationBlocks
	defer func() {
		ChainID = oldChainID
		ValidatorCommitteeRotationBlocks = oldRotation
	}()
	ChainID = "test-chain"
	ValidatorCommitteeRotationBlocks = 32

	eligibleA := []string{"V005", "V002", "V004", "V001", "V003", "V006"}
	eligibleB := []string{"V006", "V004", "V002", "V001", "V005", "V003"}

	var n *Node
	c1 := n.buildAdaptiveCommittee(1024, eligibleA, 3)
	c2 := n.buildAdaptiveCommittee(1024, eligibleB, 3)
	if !reflect.DeepEqual(c1, c2) {
		t.Fatalf("committee should be input-order independent: %v vs %v", c1, c2)
	}
}

func TestAdaptiveCommitteeVRFStableWithinRotationBucket(t *testing.T) {
	oldChainID := ChainID
	oldRotation := ValidatorCommitteeRotationBlocks
	defer func() {
		ChainID = oldChainID
		ValidatorCommitteeRotationBlocks = oldRotation
	}()
	ChainID = "test-chain"
	ValidatorCommitteeRotationBlocks = 32

	eligible := []string{"A", "B", "C", "D", "E", "F", "G"}
	var n *Node
	first := n.buildAdaptiveCommittee(64, eligible, 4)  // bucket=2
	second := n.buildAdaptiveCommittee(95, eligible, 4) // bucket=2

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("committee should stay stable inside rotation bucket: %v vs %v", first, second)
	}
}

func TestSafeModeWindowClamp(t *testing.T) {
	oldMin := ConsensusPostBlockSafeModeMin
	oldMax := ConsensusPostBlockSafeModeMax
	defer func() {
		ConsensusPostBlockSafeModeMin = oldMin
		ConsensusPostBlockSafeModeMax = oldMax
	}()

	ConsensusPostBlockSafeModeMin = 5 * time.Second
	ConsensusPostBlockSafeModeMax = 8 * time.Second

	n := &Node{
		safeModeObservedDelays: []time.Duration{
			1 * time.Second,
			2 * time.Second,
			10 * time.Second,
		},
	}
	got := n.adaptiveSafeModeWindowLocked()
	if got != 8*time.Second {
		t.Fatalf("expected clamped window=8s, got=%s", got)
	}

	n.safeModeObservedDelays = nil
	got = n.adaptiveSafeModeWindowLocked()
	if got != 5*time.Second {
		t.Fatalf("expected min window=5s with empty history, got=%s", got)
	}
}

func TestSafeModeLiveRequired(t *testing.T) {
	old := ConsensusPostBlockSafeModeLiveQuorumBPS
	defer func() { ConsensusPostBlockSafeModeLiveQuorumBPS = old }()
	ConsensusPostBlockSafeModeLiveQuorumBPS = 6700

	if got := safeModeLiveRequired(3); got != 3 {
		t.Fatalf("expected required=3 for committee=3, got=%d", got)
	}
	if got := safeModeLiveRequired(10); got != 7 {
		t.Fatalf("expected required=7 for committee=10, got=%d", got)
	}
}
