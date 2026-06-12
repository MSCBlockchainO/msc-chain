package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func crashRecoveryValidators() []string {
	return canonicalValidatorIDs([]string{"A", "B", "C", "D"})
}

func seedCrashRecoveryFrozenSet(node *Node, height uint64, validators []string) {
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
	set := canonicalValidatorIDs(validators)
	node.frozenValidatorsByHeight[height] = append([]string{}, set...)
	node.frozenValidatorHashByHeight[height] = ValidatorSetHash(set)
	node.epochValidators[height] = append([]string{}, set...)
	node.validatorSetMu.Unlock()
}

func seedCrashRecoveryCommittedState(node *Node, height uint64, hash string) {
	node.commitMu.Lock()
	if node.committed == nil {
		node.committed = make(map[uint64]string)
	}
	node.committed[height] = hash
	node.committedHeight = height
	node.finalizedHeight = height
	node.lastCommitHeight = height
	node.lastCommitAt = time.Now()
	node.commitMu.Unlock()
}

func TestValidatorCrashRecoveryPrecommitRestoresLock(t *testing.T) {
	validators := crashRecoveryValidators()
	privKeys := installCommitVoteKeysForTest(t, validators)
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), validators)
	node.Blockchain.AddBlock(Block{ID: 7, PrevHash: "hash-six", BlockHash: "chain-seven", ValidatorSetHash: ValidatorSetHash(validators)})
	seedCrashRecoveryFrozenSet(node, 8, validators)
	seedCrashRecoveryCommittedState(node, 7, "chain-seven")

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

	node.Consensus = NewConsensusState(locked.ID)
	node.Consensus.mu.Lock()
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

	recordSignedCommitVotesForTest(t, node, locked, []string{"A", "B", "C"}, privKeys)
	if err := node.persistConsensusSafetyState("precommit_crash_test"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.Consensus = NewConsensusState(locked.ID)
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
		t.Fatalf("expected precommit lock restored after crash: keep=%t reason=%s votes=%d block=%q", keep, reason, votes, restored.BlockHash)
	}
	conflict := locked
	conflict.BlockHash = "conflicting-eight"
	if !proposalConflictsWithAcceptedLock(restored, conflict) {
		t.Fatal("restored precommit lock must reject a conflicting proposal after restart")
	}
}

func TestValidatorCrashRecoveryFinalizeAdvancesNextRound(t *testing.T) {
	oldResultGossipOnly := ResultGossipOnly
	ResultGossipOnly = false
	defer func() { ResultGossipOnly = oldResultGossipOnly }()

	validators := crashRecoveryValidators()
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), validators)
	for h := uint64(1); h <= 5; h++ {
		prev := ""
		if h > 1 {
			prev = fmt.Sprintf("hash-%d", h-1)
		}
		node.Blockchain.AddBlock(Block{
			ID:               h,
			Height:           h,
			PrevHash:         prev,
			BlockHash:        fmt.Sprintf("hash-%d", h),
			Proposer:         validators[int(h-1)%len(validators)],
			ValidatorSetHash: ValidatorSetHash(validators),
		})
	}
	node.Consensus = NewConsensusState(5)
	seedCrashRecoveryCommittedState(node, 5, "hash-5")

	if err := node.persistConsensusSafetyState("finalize_crash_test"); err != nil {
		t.Fatalf("persist consensus safety state: %v", err)
	}

	node.Consensus = NewConsensusState(5)
	node.commitMu.Lock()
	node.committed = make(map[uint64]string)
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()

	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}
	if got := node.getFinalizedHeight(); got != 5 {
		t.Fatalf("expected finalized height restored after finalize crash, got=%d", got)
	}
	if !node.advanceConsensusToCommittedTip("finalize_crash_recovery_test") {
		t.Fatal("expected committed tip recovery to advance consensus")
	}
	node.Consensus.mu.Lock()
	nextHeight := node.Consensus.Height
	node.Consensus.mu.Unlock()
	if nextHeight != 6 {
		t.Fatalf("expected consensus to restart at next height after finalize recovery, got=%d", nextHeight)
	}
	if !node.hasCommittedDifferentHash(5, "forked-hash-5") {
		t.Fatal("restored finalized hash must reject same-height fork after crash")
	}
}

func TestValidatorCrashRecoverySnapshotApplyRestoresCommitSafety(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	node.Consensus = nil
	ledger := GenesisLedger()
	snapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      42,
		PrevHash:    "hash-41",
		BlockHash:   "hash-42",
		StateRoot:   "state-42",
		GenesisHash: GenesisHash,
		Ledger:      ledger,
		LedgerHash:  HashLedger(ledger),
		Validators:  map[string]bool{"A": true, "B": true, "C": true},
	}

	node.ApplySnapshotForSync(snapshot)

	node.Consensus = NewConsensusState(snapshot.Height)
	node.commitMu.Lock()
	node.committed = make(map[uint64]string)
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()

	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety state: %v", err)
	}
	assertSnapshotCommitSafetyPersisted(t, node, snapshot.Height, snapshot.BlockHash)
	if got := node.getFinalizedHeight(); got != snapshot.Height {
		t.Fatalf("expected finalized height restored from snapshot safety envelope, got=%d", got)
	}
	if !node.hasCommittedDifferentHash(snapshot.Height, "forked-hash-42") {
		t.Fatal("snapshot recovery envelope must reject lower/equal finalized fork after restart")
	}
}

func TestValidatorCrashRecoveryActivationTransitionReplaysDueStartupTransition(t *testing.T) {
	cleanupRegistry := withValidatorUpdateTestGlobals(t)
	defer cleanupRegistry()

	oldActivationModel := ValidatorSetActivationModelV2Height
	oldCheckpoint := SyncCheckpointIntervalBlocks
	oldRetryMode := TransitionBarrierRetryMode
	oldSafeMode := ConsensusPostBlockSafeModeEnabled
	defer func() {
		ValidatorSetActivationModelV2Height = oldActivationModel
		SyncCheckpointIntervalBlocks = oldCheckpoint
		TransitionBarrierRetryMode = oldRetryMode
		ConsensusPostBlockSafeModeEnabled = oldSafeMode
	}()

	DeterministicValidatorSelection = true
	DynamicValidatorSelectionEnabled = true
	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationModelV2Height = 1
	SyncCheckpointIntervalBlocks = 8
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	ConsensusPostBlockSafeModeEnabled = false
	installValidatorUpdateRegistry(t)

	validators := crashRecoveryValidators()
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), validators)
	node.ID = "A"
	node.Role = "validator"
	node.GenesisValidators = append([]string{}, validators...)
	node.Consensus = NewConsensusState(9)

	registry := GlobalValidatorRegistry.Snapshot()
	prevHash := ""
	for h := uint64(1); h <= 8; h++ {
		hash := fmt.Sprintf("hash-%d", h)
		block := Block{
			ID:                    h,
			Height:                h,
			PrevHash:              prevHash,
			BlockHash:             hash,
			Proposer:              validators[int(h-1)%len(validators)],
			ValidatorSetHash:      ValidatorSetHash(validators),
			ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
		}
		node.Blockchain.AddBlock(block)
		if err := node.storeValidatorRegistrySnapshotRecord(h, registry); err != nil {
			t.Fatalf("store registry snapshot h=%d: %v", h, err)
		}
		prevHash = hash
	}
	seedCrashRecoveryCommittedState(node, 8, "hash-8")
	seedCrashRecoveryFrozenSet(node, 8, validators)
	seedCrashRecoveryFrozenSet(node, 9, validators)
	node.currentValidators = []string{"Z"}
	node.pendingValidators = map[string]uint64{"F": 7}
	node.pendingValidatorRemovals = make(map[string]uint64)

	node.applyStartupConsensusRecovery()
	if !node.hasDueValidatorTransitionAtStartup(8) {
		t.Fatal("expected startup recovery to detect due activation transition")
	}
	if !node.recoverDueValidatorTransitionsAtStartup(8) {
		t.Fatal("expected startup recovery to replay due activation transition")
	}

	frozen := node.frozenValidatorsForHeight(8)
	if !containsValidatorIDInSet(frozen, "F") {
		t.Fatalf("expected due validator F to activate after startup crash recovery, got=%v", frozen)
	}
	if containsValidatorIDInSet(frozen, "Z") {
		t.Fatalf("stale runtime validator cache must not survive activation recovery, got=%v", frozen)
	}
	if tracker, ok := node.onboardingTrackerSnapshot("F"); !ok || tracker.State != OnboardingStateActivating {
		t.Fatalf("expected activation tracker to record startup-replayed transition, ok=%t tracker=%+v", ok, tracker)
	}
}
