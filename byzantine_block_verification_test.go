package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

func TestByzantineFakeProposerSignatureRejected(t *testing.T) {
	old := IsTestnet
	IsTestnet = false
	t.Cleanup(func() { IsTestnet = old })

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	_ = installValidatorPubKeyForTest(t, nil, block.Proposer)
	_, fakePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate fake key: %v", err)
	}
	block.Signature = ed25519.Sign(fakePriv, []byte(block.BlockHash))

	err = node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "invalid_block_signature" {
		t.Fatalf("expected fake proposer signature to be rejected instantly, got %v", err)
	}
}

func TestByzantineUnknownCommitSignerRejected(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.Signatures = []string{"A", "B", "Z"}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "signature_signer_not_validator") {
		t.Fatalf("expected unknown commit signer rejection, got %v", err)
	}
}

func TestByzantineDoubleExecutionVoteRejected(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.ExecutionResults = []ExecutionResult{
		{Height: block.ID, BlockHash: block.BlockHash, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: block.ID, BlockHash: block.BlockHash, Signer: "A", ResultHash: "different-state-root", TxMerkle: block.MempoolRoot},
	}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "duplicate_execution_result_signer" {
		t.Fatalf("expected double execution vote rejection, got %v", err)
	}
}

func TestByzantineBadQuorumProofRejected(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = 2
	block.RequiredQuorum = 3
	block.StrictQuorum = 3
	block.Signatures = []string{"A", "B", "C"}
	block.BlockHash = HashBlock(block)

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "required_quorum_exceeds_active_ready") {
		t.Fatalf("expected bad quorum proof rejection, got %v", err)
	}
}

func TestByzantineForkedFinalizedBlockRejected(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	finalized := Block{ID: 1, Height: 1, PrevHash: GenesisHash, BlockHash: "finalized-a", Proposer: "A", StateRoot: "state-a"}
	node.Blockchain.AddBlock(finalized)
	node.commitMu.Lock()
	node.committed = map[uint64]string{finalized.ID: finalized.BlockHash}
	node.committedHeight = finalized.ID
	node.finalizedHeight = finalized.ID
	node.lastCommitHeight = finalized.ID
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(finalized); err != nil {
		t.Fatalf("persist finalized invariant: %v", err)
	}

	fork := finalized
	fork.BlockHash = "finalized-b"
	err := node.ReceiveBlock(fork, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "committed_different_hash") {
		t.Fatalf("expected forked finalized block rejection, got %v", err)
	}
	if got := node.Blockchain.LastBlock().BlockHash; got != finalized.BlockHash {
		t.Fatalf("forked finalized block changed local tip: got=%q want=%q", got, finalized.BlockHash)
	}
	if !hasConsensusEvidenceForTest(t, node, "finalized_hash_conflict", finalized.ID, finalized.BlockHash, fork.BlockHash) {
		t.Fatal("expected forked finalized block evidence to persist")
	}
}
