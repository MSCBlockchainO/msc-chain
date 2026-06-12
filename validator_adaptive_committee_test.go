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

func TestAdaptiveCommitteeIgnoresLocalLivenessAndQueues(t *testing.T) {
	oldMode := ValidatorActiveSetMode
	oldMax := ValidatorMaxActiveCommittee
	oldMult := ValidatorAdaptiveCommitteeLogMult
	oldMin := ValidatorMinActiveSet
	oldFrozen := GenesisValidatorSetFrozen
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer func() {
		ValidatorActiveSetMode = oldMode
		ValidatorMaxActiveCommittee = oldMax
		ValidatorAdaptiveCommitteeLogMult = oldMult
		ValidatorMinActiveSet = oldMin
		GenesisValidatorSetFrozen = oldFrozen
		GlobalValidatorRegistry.Load(oldRegistry)
	}()

	ValidatorActiveSetMode = "adaptive_committee"
	ValidatorMaxActiveCommittee = 3
	ValidatorAdaptiveCommitteeLogMult = 1
	ValidatorMinActiveSet = 3
	GenesisValidatorSetFrozen = false

	validators := []string{"A", "B", "C", "D", "F"}
	registry := make(map[string]ValidatorRecord, len(validators))
	for _, id := range validators {
		registry[id] = ValidatorRecord{ID: id, Stake: 10, Status: ValidatorActive}
	}
	GlobalValidatorRegistry.Load(registry)

	n1 := &Node{
		GenesisValidators:        append([]string{}, validators...),
		validatorStatus:          map[string]*ValidatorStatus{"A": {Active: true, LastSeen: time.Now()}},
		pendingValidators:        map[string]uint64{"F": 1},
		pendingValidatorRemovals: map[string]uint64{"B": 1},
	}
	n2 := &Node{
		GenesisValidators:        append([]string{}, validators...),
		validatorStatus:          map[string]*ValidatorStatus{"F": {Active: true, LastSeen: time.Now().Add(-time.Hour)}},
		pendingValidators:        map[string]uint64{"B": 1},
		pendingValidatorRemovals: map[string]uint64{"F": 1},
	}

	c1 := n1.freezeValidatorSetForHeight(1, validators)
	c2 := n2.freezeValidatorSetForHeight(1, validators)
	if !reflect.DeepEqual(c1, c2) {
		t.Fatalf("local liveness or queues changed committee: node1=%v node2=%v", c1, c2)
	}
	if len(c1) != 3 {
		t.Fatalf("unexpected adaptive committee size: got=%d want=3", len(c1))
	}
	h1, ok1 := n1.frozenValidatorSetHash(1)
	h2, ok2 := n2.frozenValidatorSetHash(1)
	if !ok1 || !ok2 || h1 != h2 {
		t.Fatalf("local state changed frozen commitment: node1=%q node2=%q", h1, h2)
	}

	b1 := Block{ID: 1, Round: 2, BlockHash: "proposal", PrevHash: "parent", Proposer: "A"}
	b2 := b1
	n1.applyBlockQuorumPolicyMetadata(&b1)
	n2.applyBlockQuorumPolicyMetadata(&b2)
	if b1.ConsensusMode != b2.ConsensusMode ||
		b1.ActiveReadyCount != b2.ActiveReadyCount ||
		b1.RequiredQuorum != b2.RequiredQuorum ||
		b1.StrictQuorum != b2.StrictQuorum ||
		b1.QuorumPolicyVersion != b2.QuorumPolicyVersion {
		t.Fatalf("local liveness changed signed quorum metadata: node1=%+v node2=%+v", b1, b2)
	}
	if b1.ActiveReadyCount != len(c1) || b1.RequiredQuorum != strictExecSupermajority(len(c1)) {
		t.Fatalf("quorum metadata not derived from frozen committee: ready=%d required=%d committee=%d",
			b1.ActiveReadyCount, b1.RequiredQuorum, len(c1))
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
