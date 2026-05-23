package main

import (
	"testing"
	"time"
)

func waitForTestBlockHeight(t *testing.T, node *Node, height uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if node.Blockchain.Height() >= height {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected chain height >= %d, got %d", height, node.Blockchain.Height())
}

func TestTimingDelayedLowerRoundQuorumCannotOverrideLaterFinality(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	privKeys, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 10, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 11, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highBlock = buildProposalForRound(t, epoch, highBlock.Round+1, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store low-round proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("failed to store high-round proposal")
	}

	target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, "A", privKeys["A"], lowBlock), false)
	lowKey := proposalVoteKey(epoch, lowBlock.Round, lowBlock.BlockHash, lowBlock.MempoolRoot, lowBlock.StateRoot)
	if got := getExecCountGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot); got != 1 {
		t.Fatalf("expected one partial low-round vote, got=%d", got)
	}
	if target.Blockchain.Height() != 0 {
		t.Fatalf("partial proof must not finalize, height=%d", target.Blockchain.Height())
	}

	for _, signer := range []string{"B", "C", "D"} {
		target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, signer, privKeys[signer], highBlock), false)
	}
	waitForTestBlockHeight(t, target, epoch)
	finalBlock, ok := target.Blockchain.GetBlock(epoch)
	if !ok || finalBlock.Round != highBlock.Round || finalBlock.StateRoot != highBlock.StateRoot {
		t.Fatalf("expected high-round block to finalize, got round=%d root=%s ok=%t want round=%d root=%s",
			finalBlock.Round, finalBlock.StateRoot, ok, highBlock.Round, highBlock.StateRoot)
	}

	for _, signer := range []string{"B", "C"} {
		target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, signer, privKeys[signer], lowBlock), false)
	}
	if got := getExecCountGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot); got != 1 {
		t.Fatalf("delayed low-round quorum must be replay-fenced after finality, got=%d", got)
	}
	if got := target.Blockchain.LastBlock(); got.Round != highBlock.Round || got.StateRoot != highBlock.StateRoot {
		t.Fatalf("delayed quorum changed finalized tip: got round=%d root=%s want round=%d root=%s",
			got.Round, got.StateRoot, highBlock.Round, highBlock.StateRoot)
	}
}

func TestTimingPartialProofDeliveryDoesNotFreezeValidQuorum(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	privKeys, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := buildProposalForRound(t, 1, 10, validators, sources)
	target.noteObservedProposal(block)
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)

	for _, signer := range []string{"A", "B"} {
		target.processExecutionResultMsg(signedExecutionResultMsgWithRootForTest(t, signer, privKeys[signer], block, "partial-bad-root"), false)
	}
	if got := getExecCountGlobal(block.ID, proposalKey, "partial-bad-root", block.MempoolRoot); got != 0 {
		t.Fatalf("partial invalid proof must not enter exec pool, got=%d", got)
	}

	for _, signer := range []string{"B", "C", "D"} {
		target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, signer, privKeys[signer], block), false)
	}
	waitForTestBlockHeight(t, target, block.ID)
	if got := target.Blockchain.LastBlock(); got.Round != block.Round || got.StateRoot != block.StateRoot {
		t.Fatalf("valid quorum should finalize despite partial invalid proof: got round=%d root=%s want round=%d root=%s",
			got.Round, got.StateRoot, block.Round, block.StateRoot)
	}
}

func TestTimingAsynchronousEquivocationDoesNotPoisonValidQuorum(t *testing.T) {
	resetExecPoolForTest(t)
	setProposerRoundMaxForTest(t, 0)

	validators := []string{"A", "B", "C", "D"}
	privKeys, sources := replayTestValidatorSources(t, validators)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := buildProposalForRound(t, 1, 10, validators, sources)
	target.noteObservedProposal(block)
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)

	target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, "A", privKeys["A"], block), false)
	target.processExecutionResultMsg(signedExecutionResultMsgWithRootForTest(t, "A", privKeys["A"], block, "async-conflict-root"), false)
	if got := getExecCountGlobal(block.ID, proposalKey, "async-conflict-root", block.MempoolRoot); got != 0 {
		t.Fatalf("async equivocation must not count conflicting root, got=%d", got)
	}
	if got := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); got != 1 {
		t.Fatalf("first valid vote should remain after async equivocation, got=%d", got)
	}

	for _, signer := range []string{"B", "C"} {
		target.processExecutionResultMsg(signedExecutionResultMsgForBlock(t, signer, privKeys[signer], block), false)
	}
	waitForTestBlockHeight(t, target, block.ID)
	if got := target.Blockchain.LastBlock(); got.Round != block.Round || got.StateRoot != block.StateRoot {
		t.Fatalf("valid quorum should survive asynchronous equivocation: got round=%d root=%s want round=%d root=%s",
			got.Round, got.StateRoot, block.Round, block.StateRoot)
	}
}
