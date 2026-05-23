package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
)

func replayTestValidatorSources(t *testing.T, validators []string) (map[string]ed25519.PrivateKey, map[string]*Node) {
	t.Helper()

	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen %s: %v", id, err)
		}
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		privKeys[id] = priv
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}
	return privKeys, sources
}

func TestRecordCommitVoteRejectsOldCommitReuseAfterFinalizedHeight(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	blocks := stateRecoveryBlocks(2)
	stateRecoverySetCommitTip(t, node, blocks)

	for _, signer := range []string{"A", "B", "C"} {
		count, required := node.recordCommitVote(1, blocks[0].BlockHash, signer)
		if count != 0 {
			t.Fatalf("old commit vote from %s should be replay-fenced, count=%d required=%d", signer, count, required)
		}
	}
	if node.hasCommitQuorum(1, blocks[0].BlockHash) {
		t.Fatal("old commit reuse must not reconstruct quorum after finalized height")
	}
}

func TestReplayQueuedExecutionVotesRejectsStaleQuorumAfterCommit(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	privKeys, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := buildProposalForRound(t, 1, 10, validators, sources)
	target.noteObservedProposal(block)
	target.Blockchain.AddBlock(block)
	target.commitMu.Lock()
	target.committedHeight = block.ID
	target.finalizedHeight = block.ID
	target.lastCommitHeight = block.ID
	target.committed[block.ID] = block.BlockHash
	target.commitMu.Unlock()

	msgs := make([]ExecutionResultMsg, 0, 3)
	for _, signer := range []string{"A", "B", "C"} {
		msgs = append(msgs, signedExecutionResultMsgForBlock(t, signer, privKeys[signer], block))
	}
	target.execResultsMu.Lock()
	target.queuedExecVotes[fmt.Sprintf("%d", block.ID)] = msgs
	target.execResultsMu.Unlock()

	target.replayQueuedExecutionVotes()

	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("stale queued quorum replay should not enter exec pool, got=%d", got)
	}
	target.execResultsMu.Lock()
	remaining := len(target.queuedExecVotes)
	target.execResultsMu.Unlock()
	if remaining != 0 {
		t.Fatalf("stale queued votes should be drained after replay attempt, remaining maps=%d", remaining)
	}
	if got := target.Blockchain.Height(); got != block.ID {
		t.Fatalf("stale replay must not advance chain, got height=%d want=%d", got, block.ID)
	}
}

func TestMergeExecPoolSnapshotRejectsUnsignedDelayedProofInjection(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	_, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := buildProposalForRound(t, 1, 10, validators, sources)
	target.noteObservedProposal(block)
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)

	target.mergeExecPoolSnapshot(ExecPoolSnapshot{
		Epoch:       block.ID,
		ProposalKey: proposalKey,
		Hashes: map[string][]string{
			block.StateRoot: []string{"A", "B", "C"},
		},
		TxMerkle: map[string]string{
			block.StateRoot: block.MempoolRoot,
		},
	})

	if got := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("unsigned delayed proof injection should not be merged, got exec votes=%d", got)
	}
	if target.hasFinalExecutionResult(block.ID, block.StateRoot, block.MempoolRoot) {
		t.Fatal("unsigned delayed proof injection must not satisfy final execution quorum")
	}
}
