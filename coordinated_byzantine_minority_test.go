package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"
)

func signedExecutionResultMsgWithRootForTest(t *testing.T, signer string, priv ed25519.PrivateKey, block Block, execRoot string) ExecutionResultMsg {
	t.Helper()
	msg := ExecutionResultMsg{
		HeightHint:    block.ID,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      execRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        signer,
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(priv, execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))
	return msg
}

func setStrictQuorumMetadataForByzantineTest(block *Block, validators []string) {
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = len(canonicalValidatorIDs(validators))
	block.RequiredQuorum = strictExecSupermajority(block.ActiveReadyCount)
	block.StrictQuorum = block.RequiredQuorum
	block.BlockHash = HashBlock(*block)
}

func TestCoordinatedByzantineMinorityInvalidExecutionProofsRejected(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	privKeys, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := buildProposalForRound(t, 1, 10, validators, sources)
	setStrictQuorumMetadataForByzantineTest(&block, validators)
	target.noteObservedProposal(block)

	const badRoot = "coordinated-invalid-exec-root"
	for _, signer := range []string{"B", "C"} {
		target.processExecutionResultMsg(signedExecutionResultMsgWithRootForTest(t, signer, privKeys[signer], block, badRoot), false)
	}

	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(block.ID, proposalKey, badRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("invalid coordinated root must not enter exec pool, got=%d", got)
	}
	if got := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("invalid proofs must not count toward valid root, got=%d", got)
	}
	if target.Blockchain.Height() != 0 {
		t.Fatalf("invalid byzantine minority must not finalize a block, height=%d", target.Blockchain.Height())
	}
	if got := target.executionMismatchUniqueSignersAtEpoch(block.ID); got != 2 {
		t.Fatalf("expected both malicious signers tracked as execution mismatches, got=%d", got)
	}
}

func TestCoordinatedByzantineConflictingQuorumPropagationDoesNotPoisonValidQuorum(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	privKeys, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := buildProposalForRound(t, 1, 10, validators, sources)
	setStrictQuorumMetadataForByzantineTest(&block, validators)
	target.noteObservedProposal(block)
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	const badRoot = "propagated-conflicting-invalid-root"

	target.mergeExecPoolSnapshot(ExecPoolSnapshot{
		Epoch:       block.ID,
		ProposalKey: proposalKey,
		Hashes: map[string][]string{
			badRoot:         []string{"B", "C", "D"},
			block.StateRoot: []string{"A", "B", "C"},
		},
		TxMerkle: map[string]string{
			badRoot:         block.MempoolRoot,
			block.StateRoot: block.MempoolRoot,
		},
	})
	if got := getExecCountGlobal(block.ID, proposalKey, badRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("conflicting propagated invalid quorum must be ignored, got=%d", got)
	}
	if got := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("unsigned propagated valid quorum must also be ignored, got=%d", got)
	}

	for _, signer := range []string{"A", "B", "C"} {
		target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, signer, privKeys[signer], block), false)
	}
	deadline := time.Now().Add(2 * time.Second)
	for target.Blockchain.Height() < block.ID && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := target.Blockchain.Height(); got != block.ID {
		t.Fatalf("valid signed quorum should still finalize after conflicting propagation is rejected, got height=%d want=%d", got, block.ID)
	}
	if target.hasCommittedDifferentHash(block.ID, badRoot) == false {
		t.Fatal("invalid propagated root must remain rejected by committed-hash invariant")
	}
}
