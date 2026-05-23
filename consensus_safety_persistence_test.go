package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsensusSafetyStatePersistsRestartLineage(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	node.Blockchain.AddBlock(Block{ID: 7, BlockHash: "chain-seven"})
	node.Consensus = NewConsensusState(7)
	block := Block{
		ID:        7,
		Round:     2,
		BlockHash: "hash-seven",
		Proposer:  "A",
	}

	node.Consensus.mu.Lock()
	node.Consensus.Round = 2
	node.Consensus.Phase = PhaseVote
	node.Consensus.LockedBlock = block.BlockHash
	node.Consensus.LockedBlockHash = block.BlockHash
	node.Consensus.LockedRound = block.Round
	node.Consensus.Votes = map[uint64]map[string]BlockVote{
		7: {
			"A": {Height: 7, BlockHash: block.BlockHash, Validator: "A"},
			"B": {Height: 7, BlockHash: block.BlockHash, Validator: "B"},
		},
	}
	node.Consensus.Proposals = map[uint64]Block{7: block}
	node.Consensus.ExecVotes = map[string]map[string]ExecutionResult{
		block.BlockHash: {
			"A": {Height: 7, BlockHash: block.BlockHash, Signer: "A", ResultHash: "exec-a"},
		},
	}
	node.Consensus.mu.Unlock()

	heightKey := acceptedProposalHeightKey(7)
	proposalKey := "proposal-seven"
	node.execResultsMu.Lock()
	node.acceptedProposal = map[string]string{heightKey: proposalKey}
	node.quorumLockedProposal = map[string]string{heightKey: proposalKey}
	node.acceptedProposalBlocks = map[string]Block{proposalKey: block}
	node.localExecVoteByRound = map[uint64]map[uint32]string{7: {2: proposalKey}}
	node.execResultsMu.Unlock()

	node.commitMu.Lock()
	node.commitVotes = map[uint64]map[string]map[string]struct{}{
		7: {block.BlockHash: {"A": {}, "B": {}}},
	}
	node.commitVoted = map[uint64]map[string]string{7: {"A": block.BlockHash, "B": block.BlockHash}}
	node.committed = map[uint64]string{6: "hash-six"}
	node.committedHeight = 6
	node.finalizedHeight = 6
	node.lastCommitHeight = 6
	node.lastCommitAt = time.Now()
	node.commitMu.Unlock()

	if err := node.persistConsensusSafetyState("unit_test"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.Consensus = NewConsensusState(7)
	node.execResultsMu.Lock()
	node.acceptedProposal = nil
	node.quorumLockedProposal = nil
	node.acceptedProposalBlocks = nil
	node.localExecVoteByRound = nil
	node.execResultsMu.Unlock()
	node.commitMu.Lock()
	node.commitVotes = nil
	node.commitVoted = nil
	node.committed = nil
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.lastCommitAt = time.Time{}
	node.commitMu.Unlock()

	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}

	if node.Consensus.Round != 2 || node.Consensus.LockedBlockHash != block.BlockHash {
		t.Fatalf("lock/round not restored: round=%d lock=%q", node.Consensus.Round, node.Consensus.LockedBlockHash)
	}
	if got := node.Consensus.Votes[7]["B"].BlockHash; got != block.BlockHash {
		t.Fatalf("vote lineage not restored: got=%q", got)
	}
	node.execResultsMu.Lock()
	gotAccepted := node.acceptedProposal[heightKey]
	gotQuorumLock := node.quorumLockedProposal[heightKey]
	gotLocalVote := node.localExecVoteByRound[7][2]
	node.execResultsMu.Unlock()
	if gotAccepted != proposalKey || gotQuorumLock != proposalKey || gotLocalVote != proposalKey {
		t.Fatalf("proposal lineage not restored: accepted=%q quorum=%q local=%q", gotAccepted, gotQuorumLock, gotLocalVote)
	}
	node.commitMu.Lock()
	_, gotCommitVote := node.commitVotes[7][block.BlockHash]["B"]
	gotCommitted := node.committed[6]
	gotCommittedHeight := node.committedHeight
	node.commitMu.Unlock()
	if !gotCommitVote || gotCommitted != "hash-six" || gotCommittedHeight != 6 {
		t.Fatalf("commit lineage not restored: vote=%t committed=%q height=%d", gotCommitVote, gotCommitted, gotCommittedHeight)
	}
}

func TestConsensusSafetyRestoreDoesNotReactivateFutureHeightLock(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	node.Blockchain.AddBlock(Block{ID: 7, BlockHash: "chain-seven"})
	node.Consensus = NewConsensusState(8)
	block := Block{ID: 8, Round: 1, BlockHash: "future-lock", Proposer: "B"}

	node.Consensus.mu.Lock()
	node.Consensus.Round = 1
	node.Consensus.LockedBlock = block.BlockHash
	node.Consensus.LockedBlockHash = block.BlockHash
	node.Consensus.LockedRound = 1
	node.Consensus.mu.Unlock()
	node.execResultsMu.Lock()
	node.acceptedProposal = map[string]string{acceptedProposalHeightKey(8): "future-proposal"}
	node.quorumLockedProposal = map[string]string{acceptedProposalHeightKey(8): "future-proposal"}
	node.acceptedProposalBlocks = map[string]Block{"future-proposal": block}
	node.execResultsMu.Unlock()
	node.commitMu.Lock()
	node.commitVotes = map[uint64]map[string]map[string]struct{}{
		8: {block.BlockHash: {"B": {}}},
	}
	node.commitVoted = map[uint64]map[string]string{8: {"B": block.BlockHash}}
	node.committed = map[uint64]string{7: "chain-seven", 8: block.BlockHash}
	node.committedHeight = 8
	node.finalizedHeight = 8
	node.lastCommitHeight = 8
	node.commitMu.Unlock()

	if err := node.persistConsensusSafetyState("unit_future_lock"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.Consensus = NewConsensusState(7)
	node.commitMu.Lock()
	node.commitVotes = nil
	node.commitVoted = nil
	node.committed = nil
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()
	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}
	if node.Consensus.LockedBlockHash != "" || node.Consensus.Round != 0 {
		t.Fatalf("future-height lock should not reactivate during catch-up: round=%d lock=%q", node.Consensus.Round, node.Consensus.LockedBlockHash)
	}
	node.execResultsMu.Lock()
	_, acceptedRestored := node.acceptedProposal[acceptedProposalHeightKey(8)]
	_, quorumRestored := node.quorumLockedProposal[acceptedProposalHeightKey(8)]
	node.execResultsMu.Unlock()
	if acceptedRestored || quorumRestored {
		t.Fatal("future-height accepted/quorum proposal should stay persisted but inactive at runtime")
	}
	node.commitMu.Lock()
	committedHeight := node.committedHeight
	finalizedHeight := node.finalizedHeight
	lastCommitHeight := node.lastCommitHeight
	_, futureCommitted := node.committed[8]
	_, futureVotes := node.commitVotes[8]
	_, futureVoted := node.commitVoted[8]
	node.commitMu.Unlock()
	if committedHeight != 7 || finalizedHeight != 7 || lastCommitHeight != 7 {
		t.Fatalf("future commit state should clamp to chain tip: committed=%d finalized=%d last=%d", committedHeight, finalizedHeight, lastCommitHeight)
	}
	if futureCommitted || futureVotes || futureVoted {
		t.Fatal("future commit/vote lineage should not reactivate above local chain tip")
	}
}

func TestConsensusSafetyRestoreDropsUnbackedLocalVoteMarker(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	node.Blockchain.AddBlock(Block{ID: 7, BlockHash: "chain-seven"})
	node.Consensus = NewConsensusState(8)
	staleKey := proposalVoteKey(8, 0, "stale-block", "stale-tx", "stale-root")

	node.execResultsMu.Lock()
	node.localExecVoteByRound = map[uint64]map[uint32]string{8: {0: staleKey}}
	node.execResultsMu.Unlock()
	node.commitMu.Lock()
	node.committed = map[uint64]string{7: "chain-seven"}
	node.committedHeight = 7
	node.finalizedHeight = 7
	node.lastCommitHeight = 7
	node.commitMu.Unlock()

	if err := node.persistConsensusSafetyState("unbacked_local_vote"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.execResultsMu.Lock()
	node.localExecVoteByRound = nil
	node.execResultsMu.Unlock()
	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}

	node.execResultsMu.Lock()
	_, restored := node.localExecVoteByRound[8][0]
	node.execResultsMu.Unlock()
	if restored {
		t.Fatalf("unbacked local vote marker should not survive restart")
	}
	nextKey := proposalVoteKey(8, 0, "fresh-block", "", "fresh-root")
	if !node.allowLocalExecutionVoteRound(8, 0, nextKey) {
		t.Fatalf("fresh proposal should be allowed after stale marker is dropped")
	}
}

func TestConsensusSafetyRestoreDropsAcceptedLocalVoteWithoutExecEvidence(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	node.Blockchain.AddBlock(Block{ID: 7, BlockHash: "chain-seven"})
	node.Consensus = NewConsensusState(8)

	stale := Block{
		ID:          8,
		Round:       0,
		PrevHash:    "chain-seven",
		BlockHash:   "stale-eight",
		Proposer:    "A",
		StateRoot:   "stale-root",
		MempoolRoot: "stale-tx",
	}
	staleKey := proposalVoteKey(stale.ID, stale.Round, stale.BlockHash, stale.MempoolRoot, stale.StateRoot)
	node.Consensus.mu.Lock()
	node.Consensus.Height = 8
	node.Consensus.Round = 3
	node.Consensus.Proposals = map[uint64]Block{8: stale}
	node.Consensus.ExecVotes = nil
	node.Consensus.mu.Unlock()

	node.execResultsMu.Lock()
	node.acceptedProposal = map[string]string{acceptedProposalHeightKey(8): staleKey}
	node.acceptedProposalBlocks = map[string]Block{staleKey: stale}
	node.localExecVoteByRound = map[uint64]map[uint32]string{8: {0: staleKey}}
	node.execResultsMu.Unlock()
	node.commitMu.Lock()
	node.committed = map[uint64]string{7: "chain-seven"}
	node.committedHeight = 7
	node.finalizedHeight = 7
	node.lastCommitHeight = 7
	node.commitMu.Unlock()

	if err := node.persistConsensusSafetyState("accepted_without_exec_evidence"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.execResultsMu.Lock()
	node.acceptedProposal = nil
	node.acceptedProposalBlocks = nil
	node.localExecVoteByRound = nil
	node.execResultsMu.Unlock()
	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}

	node.execResultsMu.Lock()
	_, restored := node.localExecVoteByRound[8][0]
	node.execResultsMu.Unlock()
	if restored {
		t.Fatalf("accepted proposal without execution vote evidence should not restore local vote marker")
	}
	freshKey := proposalVoteKey(8, 3, "fresh-eight", "fresh-tx", "fresh-root")
	if !node.allowLocalExecutionVoteRound(8, 3, freshKey) {
		t.Fatalf("fresh proposal should be allowed after evidence-free stale marker is dropped")
	}
}

func TestConsensusSafetyRestoreNextHeightPrecommitLockAfterCrash(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), validators)
	node.Blockchain.AddBlock(Block{ID: 7, PrevHash: "hash-six", BlockHash: "chain-seven", ValidatorSetHash: ValidatorSetHash(validators)})
	node.Consensus = NewConsensusState(8)
	node.validatorSetMu.Lock()
	if node.frozenValidatorsByHeight == nil {
		node.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if node.frozenValidatorHashByHeight == nil {
		node.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	if node.epochValidators == nil {
		node.epochValidators = make(map[uint64][]string)
	}
	node.frozenValidatorsByHeight[8] = append([]string{}, validators...)
	node.frozenValidatorHashByHeight[8] = ValidatorSetHash(validators)
	node.epochValidators[8] = append([]string{}, validators...)
	node.validatorSetMu.Unlock()

	locked := Block{
		ID:          8,
		Round:       1,
		PrevHash:    "chain-seven",
		BlockHash:   "locked-eight",
		Proposer:    "A",
		StateRoot:   "exec-eight",
		MempoolRoot: "tx-eight",
	}
	proposalKey := proposalVoteKey(locked.ID, locked.Round, locked.BlockHash, locked.MempoolRoot, locked.StateRoot)

	node.Consensus.mu.Lock()
	node.Consensus.Height = locked.ID
	node.Consensus.Round = locked.Round
	node.Consensus.Phase = PhaseVote
	node.Consensus.LockedBlock = locked.BlockHash
	node.Consensus.LockedBlockHash = locked.BlockHash
	node.Consensus.LockedRound = locked.Round
	node.Consensus.ExecVotes = map[string]map[string]ExecutionResult{
		locked.BlockHash: {
			"A": {Height: locked.ID, BlockHash: locked.BlockHash, Signer: "A", ResultHash: locked.StateRoot, TxMerkle: locked.MempoolRoot},
			"B": {Height: locked.ID, BlockHash: locked.BlockHash, Signer: "B", ResultHash: locked.StateRoot, TxMerkle: locked.MempoolRoot},
			"C": {Height: locked.ID, BlockHash: locked.BlockHash, Signer: "C", ResultHash: locked.StateRoot, TxMerkle: locked.MempoolRoot},
		},
	}
	node.Consensus.mu.Unlock()

	node.execResultsMu.Lock()
	node.acceptedProposal = map[string]string{acceptedProposalHeightKey(locked.ID): proposalKey}
	node.quorumLockedProposal = map[string]string{acceptedProposalHeightKey(locked.ID): proposalKey}
	node.acceptedProposalBlocks = map[string]Block{proposalKey: locked}
	node.localExecVoteByRound = map[uint64]map[uint32]string{locked.ID: {locked.Round: proposalKey}}
	node.execResultsMu.Unlock()

	node.commitMu.Lock()
	node.committed = map[uint64]string{7: "chain-seven"}
	node.committedHeight = 7
	node.finalizedHeight = 7
	node.lastCommitHeight = 7
	node.commitMu.Unlock()

	if err := node.persistConsensusSafetyState("precommit_crash"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.Consensus = NewConsensusState(8)
	node.execResultsMu.Lock()
	node.acceptedProposal = nil
	node.quorumLockedProposal = nil
	node.acceptedProposalBlocks = nil
	node.localExecVoteByRound = nil
	node.execResultsMu.Unlock()

	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}

	restored, votes, keep, reason := node.quorumLockedProposalLockState(locked.ID)
	if !keep || restored.BlockHash != locked.BlockHash || votes != 3 {
		t.Fatalf("expected next-height quorum lock restored after crash: keep=%t reason=%s votes=%d block=%q",
			keep, reason, votes, restored.BlockHash)
	}
	conflict := locked
	conflict.BlockHash = "conflicting-eight"
	if !proposalConflictsWithAcceptedLock(restored, conflict) {
		t.Fatal("restored precommit lock must reject conflicting finalized proposal after restart")
	}
}

func TestRestoreCommittedHeightFromChainBackfillsFinalizedHashes(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	blocks := []Block{
		{ID: 1, Height: 1, BlockHash: "hash-one"},
		{ID: 2, Height: 2, PrevHash: "hash-one", BlockHash: "hash-two"},
		{ID: 3, Height: 3, PrevHash: "hash-two", BlockHash: "hash-three"},
	}
	node.Blockchain.ReplaceChain(blocks)
	node.commitMu.Lock()
	node.committed = map[uint64]string{}
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()

	node.restoreCommittedHeightFromChain()

	node.commitMu.Lock()
	committedOne := node.committed[1]
	committedTwo := node.committed[2]
	committedThree := node.committed[3]
	committedHeight := node.committedHeight
	finalizedHeight := node.finalizedHeight
	node.commitMu.Unlock()
	if committedHeight != 3 || finalizedHeight != 3 {
		t.Fatalf("expected committed/finalized height restored to tip: committed=%d finalized=%d", committedHeight, finalizedHeight)
	}
	if committedOne != "hash-one" || committedTwo != "hash-two" || committedThree != "hash-three" {
		t.Fatalf("expected all committed hashes restored: h1=%q h2=%q h3=%q", committedOne, committedTwo, committedThree)
	}
	if got, found, err := node.loadFinalizedHashInvariant(1); err != nil || !found || got != "hash-one" {
		t.Fatalf("expected finalized invariant for height 1, got=%q found=%t err=%v", got, found, err)
	}
	if got, found, err := node.loadFinalizedHashInvariant(2); err != nil || !found || got != "hash-two" {
		t.Fatalf("expected finalized invariant for height 2, got=%q found=%t err=%v", got, found, err)
	}
	if !node.hasCommittedDifferentHash(1, "fork-one") {
		t.Fatal("lower finalized height fork must be rejected after startup backfill")
	}
	if !node.hasCommittedDifferentHash(2, "fork-two") {
		t.Fatal("middle finalized height fork must be rejected after startup backfill")
	}
}

func TestReceiveBlockAlreadyCommittedConflictIsObservable(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	committed := Block{ID: 5, PrevHash: "hash-four", BlockHash: "partition-a-five"}
	node.Blockchain.AddBlock(committed)
	node.commitMu.Lock()
	node.committed = map[uint64]string{5: committed.BlockHash}
	node.committedHeight = 5
	node.finalizedHeight = 5
	node.lastCommitHeight = 5
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(committed); err != nil {
		t.Fatalf("persist finalized invariant: %v", err)
	}

	conflict := committed
	conflict.BlockHash = "partition-b-five"
	conflict.ConsensusMode = "NORMAL"
	conflict.QuorumPolicyVersion = quorumPolicyVersionV1
	conflict.RequiredQuorum = 3
	conflict.StrictQuorum = 3
	conflict.ActiveReadyCount = 3
	conflict.Signatures = []string{"A", "B", "C"}
	err := node.ReceiveBlock(conflict, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "committed_different_hash") {
		t.Fatalf("expected observable committed hash conflict after partition heal, got %v", err)
	}
	if got := node.Blockchain.Height(); got != committed.ID {
		t.Fatalf("conflicting already-committed block must not change chain height: got=%d want=%d", got, committed.ID)
	}
	if !hasConsensusEvidenceForTest(t, node, "finalized_hash_conflict", committed.ID, committed.BlockHash, conflict.BlockHash) {
		t.Fatal("expected finalized hash conflict evidence to persist for partition-heal observation")
	}
}

func TestQueueFutureBlockRejectsFinalizedForkAfterPartitionHeal(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	committed := Block{ID: 6, PrevHash: "hash-five", BlockHash: "partition-a-six"}
	node.Blockchain.AddBlock(committed)
	node.commitMu.Lock()
	node.committed = map[uint64]string{6: committed.BlockHash}
	node.committedHeight = 6
	node.finalizedHeight = 6
	node.lastCommitHeight = 6
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(committed); err != nil {
		t.Fatalf("persist finalized invariant: %v", err)
	}

	conflict := committed
	conflict.BlockHash = "partition-b-six"
	if node.QueueFutureBlock(conflict) {
		t.Fatal("finalized-height fork must not be queued after partition heal")
	}
	node.forkMu.Lock()
	queued := len(node.ForkBlocks[conflict.ID])
	node.forkMu.Unlock()
	if queued != 0 {
		t.Fatalf("expected no queued finalized fork, got %d", queued)
	}
	if !hasConsensusEvidenceForTest(t, node, "finalized_hash_conflict", committed.ID, committed.BlockHash, conflict.BlockHash) {
		t.Fatal("expected queued finalized fork evidence to persist")
	}
}

func TestSwitchToForkRejectsFinalizedTipForkAfterPartitionHeal(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	committed := Block{ID: 1, PrevHash: GenesisHash, BlockHash: "partition-a-one"}
	node.Blockchain.AddBlock(committed)
	node.commitMu.Lock()
	node.committed = map[uint64]string{1: committed.BlockHash}
	node.committedHeight = 1
	node.finalizedHeight = 1
	node.lastCommitHeight = 1
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(committed); err != nil {
		t.Fatalf("persist finalized invariant: %v", err)
	}

	conflict := committed
	conflict.BlockHash = "partition-b-one"
	node.SwitchToFork(conflict)
	if got := node.Blockchain.LastBlock().BlockHash; got != committed.BlockHash {
		t.Fatalf("finalized tip fork must not replace local chain: got=%q want=%q", got, committed.BlockHash)
	}
	if !hasConsensusEvidenceForTest(t, node, "finalized_hash_conflict", committed.ID, committed.BlockHash, conflict.BlockHash) {
		t.Fatal("expected switch-to-fork finalized conflict evidence to persist")
	}
}

func TestVerifyBlockCommittedConflictPersistsEvidence(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	parent := Block{ID: 4, BlockHash: "hash-four"}
	node.Blockchain.AddBlock(parent)
	node.commitMu.Lock()
	node.committed = map[uint64]string{5: "final-five"}
	node.committedHeight = 4
	node.finalizedHeight = 4
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(Block{ID: 5, BlockHash: "final-five"}); err != nil {
		t.Fatalf("persist finalized invariant: %v", err)
	}

	conflict := Block{
		ID:        5,
		PrevHash:  parent.BlockHash,
		BlockHash: "fork-five",
		Proposer:  "A",
		StateRoot: "root-five",
	}
	_, err := node.verifyBlockProductionEnvelope(conflict, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "committed_different_hash") {
		t.Fatalf("expected verifier committed hash conflict, got %v", err)
	}
	if !hasConsensusEvidenceForTest(t, node, "finalized_hash_conflict", 5, "final-five", conflict.BlockHash) {
		t.Fatal("expected verifier finalized hash conflict evidence to persist")
	}
}

func TestConsensusEvidenceSeenPersistsAcrossReload(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	if !node.shouldCountDoubleProposalEvidence(8, 1, "A", "prev", "got") {
		t.Fatal("first double proposal evidence should count")
	}

	node.doubleProposalMu.Lock()
	node.doubleProposalEvidenceSeen = nil
	node.doubleProposalMu.Unlock()

	if err := node.loadConsensusEvidenceSeenFromDB(); err != nil {
		t.Fatalf("load consensus evidence: %v", err)
	}
	if node.shouldCountDoubleProposalEvidence(8, 1, "A", "prev", "got") {
		t.Fatal("persisted double proposal evidence should suppress duplicate after reload")
	}
}

func TestFinalizedHashInvariantPersistsAndRejectsConflict(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	block := Block{ID: 5, BlockHash: "final-hash"}
	if err := node.persistFinalizedHashInvariant(block); err != nil {
		t.Fatalf("persist finalized hash invariant: %v", err)
	}
	if !node.hasCommittedDifferentHash(5, "other-hash") {
		t.Fatal("persisted finalized hash should reject conflicting hash")
	}
	if err := node.persistFinalizedHashInvariant(Block{ID: 5, BlockHash: "other-hash"}); err == nil {
		t.Fatal("expected finalized hash conflict error")
	}
	if err := node.persistFinalizedHashInvariant(block); err != nil {
		t.Fatalf("same finalized hash should remain idempotent: %v", err)
	}
}

func consensusEvidenceRecordsForTest(t *testing.T, node *Node) []consensusEvidenceRecord {
	t.Helper()
	var records []consensusEvidenceRecord
	err := node.DB.State.View(func(txn *Txn) error {
		opts := DefaultIteratorOptions
		opts.Prefix = []byte(consensusEvidenceDBPrefix)
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if item == nil {
				continue
			}
			if err := item.Value(func(val []byte) error {
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				var ev consensusEvidenceRecord
				if uerr := json.Unmarshal(plain, &ev); uerr != nil {
					return uerr
				}
				records = append(records, ev)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("load consensus evidence records: %v", err)
	}
	return records
}

func hasConsensusEvidenceForTest(t *testing.T, node *Node, typ string, height uint64, expectedHash, gotHash string) bool {
	t.Helper()
	expectedHash = strings.ToLower(strings.TrimSpace(expectedHash))
	gotHash = strings.ToLower(strings.TrimSpace(gotHash))
	for _, ev := range consensusEvidenceRecordsForTest(t, node) {
		if ev.Type != typ || ev.Height != height {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ev.Expected), expectedHash) &&
			strings.EqualFold(strings.TrimSpace(ev.Got), gotHash) {
			return true
		}
	}
	return false
}
