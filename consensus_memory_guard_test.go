package main

import (
	"fmt"
	"testing"
	"time"
)

func TestExecPoolRejectsNewScopeAtHardCapButAllowsKnownScope(t *testing.T) {
	resetExecPoolForTest(t)

	epoch := uint64(7000)
	knownProposal := ""
	knownExecHash := "root-known"
	ExecPool.mu.Lock()
	ensureExecPoolEpochMapsLocked(epoch)
	for i := 0; i < ExecPoolMaxScopesPerEpoch; i++ {
		proposalKey := proposalVoteKey(epoch, uint32(i), fmt.Sprintf("block-%03d", i), "tx", "")
		scope := execPoolScopeKey(epoch, proposalKey)
		ExecPool.signers[epoch][scope] = map[string]bool{"A": true}
		ExecPool.choice[epoch][scope] = map[string]string{"A": execBroadcastKey("root-a", "tx")}
		if i == 0 {
			knownProposal = proposalKey
		}
	}
	ExecPool.mu.Unlock()

	count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, knownProposal, knownExecHash, "tx", ExecutionResult{
		Height:    epoch,
		Round:     0,
		BlockHash: "block-000",
		Signer:    "B",
		TxMerkle:  "tx",
	}, 3)
	if !ok || equivocation || count != 1 {
		t.Fatalf("known scope should still admit a valid signer at cap: count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}

	newProposal := proposalVoteKey(epoch, 1, "block-new", "tx", "")
	count, ok, equivocation = recordExecResultGlobalWithRequired(epoch, newProposal, "root-new", "tx", ExecutionResult{
		Height:    epoch,
		Round:     1,
		BlockHash: "block-new",
		Signer:    "C",
		TxMerkle:  "tx",
	}, 3)
	if ok || equivocation || count != 0 {
		t.Fatalf("new scope should be rejected once scope cap is reached: count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
}

func TestExecPoolPrunePreservesProtectedScope(t *testing.T) {
	resetExecPoolForTest(t)

	epoch := uint64(7100)
	protectedProposal := proposalVoteKey(epoch, 9, "protected-block", "tx", "")
	protectedScope := execPoolScopeKey(epoch, protectedProposal)
	ExecPool.mu.Lock()
	ensureExecPoolEpochMapsLocked(epoch)
	ExecPool.signers[epoch][protectedScope] = map[string]bool{"A": true}
	ExecPool.choice[epoch][protectedScope] = map[string]string{"A": execBroadcastKey("root-protected", "tx")}
	for i := 0; i < ExecPoolMaxScopesPerEpoch+25; i++ {
		proposalKey := proposalVoteKey(epoch, uint32(i), fmt.Sprintf("overflow-block-%03d", i), "tx", "")
		scope := execPoolScopeKey(epoch, proposalKey)
		ExecPool.signers[epoch][scope] = map[string]bool{"Z": true}
		ExecPool.choice[epoch][scope] = map[string]string{"Z": execBroadcastKey("root-overflow", "tx")}
	}
	protected := map[uint64]map[string]bool{epoch: {protectedScope: true}}
	pruned := pruneExecPoolLocked(0, protected)
	if !execPoolScopeKnownLocked(epoch, protectedScope) {
		t.Fatalf("protected exec pool scope was pruned")
	}
	if got := len(execPoolScopeSetLocked(epoch)); got > ExecPoolMaxScopesPerEpoch {
		t.Fatalf("exec pool scopes=%d, want <= %d", got, ExecPoolMaxScopesPerEpoch)
	}
	ExecPool.mu.Unlock()
	if pruned.ExecPoolScopes == 0 {
		t.Fatalf("expected overflow scopes to be pruned")
	}
}

func TestAcceptedProposalBlocksGlobalCapPreservesProtectedProposal(t *testing.T) {
	node := &Node{
		acceptedProposal:       make(map[string]string),
		acceptedProposalBlocks: make(map[string]Block),
		quorumLockedProposal:   make(map[string]string),
	}
	protectedHeight := uint64(8000)
	protectedBlock := Block{ID: protectedHeight, Round: 11, BlockHash: "protected-block", MempoolRoot: "tx"}
	protectedKey := proposalVoteKey(protectedBlock.ID, protectedBlock.Round, protectedBlock.BlockHash, protectedBlock.MempoolRoot, protectedBlock.StateRoot)
	node.acceptedProposal[acceptedProposalHeightKey(protectedHeight)] = protectedKey
	node.acceptedProposalBlocks[protectedKey] = protectedBlock

	for i := 0; i < AcceptedProposalBlocksMaxKeys+50; i++ {
		height := uint64(1000 + i)
		block := Block{ID: height, Round: uint32(i % 32), BlockHash: fmt.Sprintf("block-%04d", i), MempoolRoot: "tx"}
		key := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
		node.acceptedProposalBlocks[key] = block
	}

	node.execResultsMu.Lock()
	pruned := node.pruneAcceptedProposalBlocksGlobalLocked(0)
	_, protectedOK := node.acceptedProposalBlocks[protectedKey]
	got := len(node.acceptedProposalBlocks)
	node.execResultsMu.Unlock()

	if !protectedOK {
		t.Fatalf("protected accepted proposal block was pruned")
	}
	if got > AcceptedProposalBlocksMaxKeys {
		t.Fatalf("accepted proposal blocks=%d, want <= %d", got, AcceptedProposalBlocksMaxKeys)
	}
	if pruned == 0 {
		t.Fatalf("expected accepted proposal block overflow to be pruned")
	}
}

func TestValidatorStatusCapPreservesProtectedValidators(t *testing.T) {
	node := &Node{
		ID:              "A",
		validatorStatus: make(map[string]*ValidatorStatus),
	}
	protected := map[string]bool{"A": true, "B": true}
	node.validatorStatus["A"] = &ValidatorStatus{Active: true, LastSeen: time.Now()}
	node.validatorStatus["B"] = &ValidatorStatus{Active: true, LastSeen: time.Now()}
	for i := 0; i < ValidatorStatusMinCap+40; i++ {
		id := fmt.Sprintf("stale-%03d", i)
		node.validatorStatus[id] = &ValidatorStatus{
			ReportedHeight: uint64(i),
			LastSeen:       time.Now().Add(-time.Duration(i+1) * time.Hour),
		}
	}

	node.validatorMu.Lock()
	pruned := node.pruneValidatorStatusLocked(protected)
	_, keepA := node.validatorStatus["A"]
	_, keepB := node.validatorStatus["B"]
	got := len(node.validatorStatus)
	node.validatorMu.Unlock()

	if !keepA || !keepB {
		t.Fatalf("protected validator status entries were pruned: A=%t B=%t", keepA, keepB)
	}
	if got > ValidatorStatusMinCap {
		t.Fatalf("validator status entries=%d, want <= %d", got, ValidatorStatusMinCap)
	}
	if pruned == 0 {
		t.Fatalf("expected stale validator status entries to be pruned")
	}
}
