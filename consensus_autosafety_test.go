package main

import (
	"testing"
	"time"
)

func newConsensusAutoSafetyNode(validators []string) *Node {
	bc := NewBlockchain()
	n := &Node{
		ID:                          "A",
		Blockchain:                  &bc,
		GenesisValidators:           append([]string{}, validators...),
		validatorStatus:             make(map[string]*ValidatorStatus),
		frozenValidatorsByHeight:    make(map[uint64][]string),
		frozenValidatorHashByHeight: make(map[uint64]string),
		invalidProposerStrikes:      make(map[string]ExecMismatchTracker),
		invalidProposerPeerStrikes:  make(map[string]ExecMismatchTracker),
	}
	_ = n.freezeValidatorSetForHeight(1, validators)
	return n
}

func TestInvalidProposerStrikeTupleScoped(t *testing.T) {
	n := newConsensusAutoSafetyNode([]string{"A", "B", "C", "D"})

	first := n.recordInvalidProposerStrike("B", 10, "A", "B")
	second := n.recordInvalidProposerStrike("B", 10, "A", "B")
	otherTuple := n.recordInvalidProposerStrike("B", 10, "C", "B")

	if first != 1 || second != 2 {
		t.Fatalf("expected same tuple to increment 1->2, got %d->%d", first, second)
	}
	if otherTuple != 1 {
		t.Fatalf("expected different tuple to keep independent counter, got %d", otherTuple)
	}
}

func TestCanEnforceConsensusPenaltyConvergedOnly(t *testing.T) {
	oldMode := ConsensusPenaltyEnforceMode
	ConsensusPenaltyEnforceMode = "converged_only"
	t.Cleanup(func() {
		ConsensusPenaltyEnforceMode = oldMode
	})

	n := newConsensusAutoSafetyNode([]string{"A", "B", "C", "D"})
	ok, reason := n.canEnforceConsensusPenalty(1)
	if !ok {
		t.Fatalf("expected penalty enforcement allowed for converged set, reason=%s", reason)
	}

	n.recomputePauseMu.Lock()
	n.recomputePauseUntil = time.Now().Add(2 * time.Second)
	n.recomputePauseMu.Unlock()

	ok, reason = n.canEnforceConsensusPenalty(1)
	if ok {
		t.Fatalf("expected penalty enforcement blocked during recompute pause")
	}
	if reason != "recompute_pause_active" {
		t.Fatalf("unexpected block reason: %s", reason)
	}
}

func TestVerifyBlockRoundAwareProposerValidation(t *testing.T) {
	oldResultMode := ResultGossipOnly
	ResultGossipOnly = true
	t.Cleanup(func() {
		ResultGossipOnly = oldResultMode
	})

	n := newConsensusAutoSafetyNode([]string{"A", "B", "C", "D"})
	height := uint64(1)
	validators := n.freezeValidatorSetForHeight(height, []string{"A", "B", "C", "D"})
	if len(validators) != 4 {
		t.Fatalf("unexpected validator set: %v", validators)
	}

	round := uint32(1)
	roundLeader := n.consensusLeaderForHeightRound(height, round, validators)
	roundZeroLeader := n.consensusLeaderForHeightRound(height, 0, validators)
	if roundLeader == "" || roundZeroLeader == "" {
		t.Fatalf("missing leader selection for test")
	}
	if roundLeader == roundZeroLeader {
		t.Skipf("round leader equals round-0 leader for this schedule; cannot assert round-aware mismatch")
	}

	last := n.Blockchain.LastBlock()
	buildBlock := func(proposer string) Block {
		block := Block{
			ID:               height,
			Type:             BlockTypeTime,
			Round:            round,
			PrevHash:         last.BlockHash,
			Proposer:         proposer,
			BlockTime:        LogicalTimeForEpochTick(height, TickFinalize),
			StateRoot:        "state-root",
			ValidatorSetHash: ValidatorSetHash(validators),
		}
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		return block
	}

	validRoundBlock := buildBlock(roundLeader)
	if err := n.VerifyBlock(validRoundBlock, n.Blockchain); err != nil {
		t.Fatalf("expected round-aware valid proposer to pass VerifyBlock, got error: %v", err)
	}

	invalidRoundBlock := buildBlock(roundZeroLeader)
	if err := n.VerifyBlock(invalidRoundBlock, n.Blockchain); err == nil || err.Error() != "invalid_proposer" {
		t.Fatalf("expected invalid proposer for round-aware mismatch, got: %v", err)
	}
}

func TestReceiveBlockRoundAwareNoFalseInvalidProposer(t *testing.T) {
	n := newConsensusAutoSafetyNode([]string{"A", "B", "C", "D"})
	height := uint64(1)
	validators := n.freezeValidatorSetForHeight(height, []string{"A", "B", "C", "D"})
	round := uint32(1)
	roundLeader := n.consensusLeaderForHeightRound(height, round, validators)
	roundZeroLeader := n.consensusLeaderForHeightRound(height, 0, validators)
	if roundLeader == "" || roundZeroLeader == "" {
		t.Fatalf("missing leader selection for test")
	}
	if roundLeader == roundZeroLeader {
		t.Skipf("round leader equals round-0 leader for this schedule; cannot assert round-aware path")
	}

	last := n.Blockchain.LastBlock()
	block := Block{
		ID:        height,
		Type:      BlockTypeTime,
		Round:     round,
		PrevHash:  last.BlockHash,
		Proposer:  roundLeader,
		BlockTime: LogicalTimeForEpochTick(height, TickFinalize),
		// Force post-proposer validation failure so ReceiveBlock exits early without full commit path.
		BlockHash: "intentionally-invalid-hash",
		StateRoot: "state-root",
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	_ = n.ReceiveBlock(block, n.Blockchain)

	n.invalidProposerMu.Lock()
	defer n.invalidProposerMu.Unlock()
	if perHeight, ok := n.invalidProposerSeen[height]; ok && len(perHeight) > 0 {
		t.Fatalf("unexpected invalid proposer event for round-aware valid proposer: %#v", perHeight)
	}
	if len(n.invalidProposerEvidenceSeen) != 0 {
		t.Fatalf("unexpected invalid proposer evidence tracking for round-aware valid proposer")
	}
}

func TestDuplicateInvalidProposerEvidenceDoesNotDoubleStrike(t *testing.T) {
	oldMode := ConsensusPenaltyEnforceMode
	ConsensusPenaltyEnforceMode = "always_strict"
	t.Cleanup(func() {
		ConsensusPenaltyEnforceMode = oldMode
	})

	n := newConsensusAutoSafetyNode([]string{"A", "B", "C", "D"})
	height := uint64(1)
	round := uint32(0)
	validators := n.freezeValidatorSetForHeight(height, []string{"A", "B", "C", "D"})
	expected := n.consensusLeaderForHeightRound(height, round, validators)
	if expected == "" {
		t.Fatalf("missing expected proposer")
	}
	got := ""
	for _, id := range validators {
		if id != expected {
			got = id
			break
		}
	}
	if got == "" {
		t.Fatalf("failed to choose mismatched proposer")
	}

	last := n.Blockchain.LastBlock()
	buildInvalid := func(hash string) Block {
		return Block{
			ID:        height,
			Type:      BlockTypeTime,
			Round:     round,
			PrevHash:  last.BlockHash,
			Proposer:  got,
			BlockHash: hash,
			BlockTime: LogicalTimeForEpochTick(height, TickFinalize),
			StateRoot: "state-root",
			Timestamp: int64(SystemTimeUnits(LogicalTimeForEpochTick(height, TickFinalize))),
		}
	}

	duplicate := buildInvalid("dup-invalid-hash")
	_ = n.ReceiveBlock(duplicate, n.Blockchain)
	_ = n.ReceiveBlock(duplicate, n.Blockchain)

	strikeKey := invalidProposerStrikeKey(got, expected, got)
	n.invalidProposerMu.Lock()
	strikes := n.invalidProposerStrikes[strikeKey].Count
	n.invalidProposerMu.Unlock()
	if strikes != 1 {
		t.Fatalf("expected duplicate invalid proposer evidence to count as one strike, got %d", strikes)
	}

	unique := buildInvalid("dup-invalid-hash-2")
	_ = n.ReceiveBlock(unique, n.Blockchain)

	n.invalidProposerMu.Lock()
	strikes = n.invalidProposerStrikes[strikeKey].Count
	n.invalidProposerMu.Unlock()
	if strikes != 2 {
		t.Fatalf("expected unique invalid proposer evidence to increment strike, got %d", strikes)
	}
}
