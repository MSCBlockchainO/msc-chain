package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func newTestNodeForResultGossip(t *testing.T, dataDir string, validators []string) *Node {
	t.Helper()

	db := OpenNodeDB(dataDir)
	t.Cleanup(func() {
		_ = db.Close()
	})

	bc := NewBlockchain()

	n := &Node{
		ID:         "TEST",
		Role:       "full",
		Ledger:     NewLedger(),
		DataDir:    dataDir,
		Blockchain: &bc,
		DB:         db,
		shutdownCh: make(chan struct{}),
		closeChan:  make(chan struct{}),

		SeenTxIDs:       make(map[string]bool),
		SeenBlockHashes: make(map[string]bool),
		ForkBlocks:      make(map[uint64][]Block),
		Mempool: Mempool{
			SeenTxIDs: make(map[string]bool),
		},
		ProposalHistory: make(map[uint64]string),
		MisbehaviorLog:  make(map[string][]SlashEvidence),

		peerSetHash:      make(map[string]string),
		peerHashMatch:    make(map[string]bool),
		peerToValidator:  make(map[string]string),
		peerSuspectAt:    make(map[string]time.Time),
		peerHelloSentAt:  make(map[string]time.Time),
		peerFlapTimes:    make(map[string][]time.Time),
		quarantineUntil:  make(map[string]time.Time),
		connectedPeers:   make(map[string]bool),
		validatorSuspect: make(map[string]time.Time),
		connectingPeers:  make(map[string]bool),
		allowedPeerIDs:   make(map[string]bool),

		pendingValidators:        make(map[string]uint64),
		pendingValidatorRemovals: make(map[string]uint64),
		validatorStatus:          make(map[string]*ValidatorStatus),

		execResults:                make(map[string]map[string]ExecutionResult),
		execBroadcasted:            make(map[uint64]map[string]bool),
		execSignerSeen:             make(map[uint64]map[string]map[string]bool),
		execBroadcastedByValidator: make(map[uint64]map[string]map[string]bool),
		localExecVoteByRound:       make(map[uint64]map[uint32]string),
		pendingBlocks:              make(map[string]Block),
		queuedExecVotes:            make(map[string][]ExecutionResultMsg),
		acceptedProposal:           make(map[string]string),
		acceptedProposalBlocks:     make(map[string]Block),
		quorumLockedProposal:       make(map[string]string),
		leaderBlocks:               make(map[uint64]Block),
		committed:                  make(map[uint64]string),
		commitVotes:                make(map[uint64]map[string]map[string]struct{}),
		commitVoted:                make(map[uint64]map[string]string),
		logicalClock:               LogicalTimeForEpoch(1),
	}

	n.validatorSetMu.Lock()
	n.validatorSetHeight = 1
	n.epochValidators = make(map[uint64][]string, 1024)
	for h := uint64(1); h <= 1024; h++ {
		n.epochValidators[h] = append([]string{}, validators...)
	}
	n.validatorSetMu.Unlock()
	n.GenesisValidators = append([]string{}, validators...)

	n.Consensus = &ConsensusState{
		Height:    1,
		Proposals: make(map[uint64]Block),
		Votes:     make(map[uint64]map[string]BlockVote),
		ExecVotes: make(map[string]map[string]ExecutionResult),
	}
	rootCtx, rootCancel := context.WithCancel(context.Background())
	n.SetRootContext(rootCtx, rootCancel)
	n.ensureDedicatedThreads()
	t.Cleanup(func() {
		n.CancelRootContext()
		n.stopDedicatedThreads()
		done := make(chan struct{})
		go func() {
			n.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Log("timed out waiting for dedicated test threads to stop")
		}
	})

	return n
}

func waitForConsensusTargetHeight(t *testing.T, node *Node, want uint64) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var consensusHeight uint64
		if node.Consensus != nil {
			node.Consensus.mu.Lock()
			consensusHeight = node.Consensus.Height
			node.Consensus.mu.Unlock()
		}
		node.logicalMu.Lock()
		logicalEpoch := node.logicalClock.Epoch
		node.logicalMu.Unlock()
		if logicalEpoch >= want && (node.Consensus == nil || consensusHeight >= want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	var consensusHeight uint64
	if node.Consensus != nil {
		node.Consensus.mu.Lock()
		consensusHeight = node.Consensus.Height
		node.Consensus.mu.Unlock()
	}
	node.logicalMu.Lock()
	logicalEpoch := node.logicalClock.Epoch
	node.logicalMu.Unlock()
	t.Fatalf("expected consensus to advance to height %d, got chain=%d consensus=%d logical_epoch=%d",
		want,
		node.Blockchain.Height(),
		consensusHeight,
		logicalEpoch,
	)
}

func TestResultGossipCommitTenTimes(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldResultGossipOnly := ResultGossipOnly
	oldDebugConsensus := DebugConsensus
	oldValidatorPubKeys := ValidatorPubKeys
	ExecPool.mu.Lock()
	oldExecPoolPool := ExecPool.pool
	oldExecPoolTxMerkle := ExecPool.txMerkle
	oldExecPoolFrozen := ExecPool.frozen
	oldExecPoolSigners := ExecPool.signers
	oldExecPoolChoice := ExecPool.choice
	oldExecPoolEpochChoice := ExecPool.epochChoice
	ExecPool.mu.Unlock()
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		ResultGossipOnly = oldResultGossipOnly
		DebugConsensus = oldDebugConsensus
		ValidatorPubKeys = oldValidatorPubKeys
		ExecPool.mu.Lock()
		ExecPool.pool = oldExecPoolPool
		ExecPool.txMerkle = oldExecPoolTxMerkle
		ExecPool.frozen = oldExecPoolFrozen
		ExecPool.signers = oldExecPoolSigners
		ExecPool.choice = oldExecPoolChoice
		ExecPool.epochChoice = oldExecPoolEpochChoice
		ExecPool.mu.Unlock()
	})

	GenesisHash = "genesis"
	ResultGossipOnly = true
	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
	}
	ExecPool.mu.Lock()
	ExecPool.pool = make(map[uint64]map[string]map[string]ExecutionResult)
	ExecPool.txMerkle = make(map[uint64]map[string]string)
	ExecPool.frozen = make(map[uint64]map[string]string)
	ExecPool.signers = make(map[uint64]map[string]map[string]bool)
	ExecPool.choice = make(map[uint64]map[string]map[string]string)
	ExecPool.epochChoice = make(map[uint64]map[string]string)
	ExecPool.mu.Unlock()

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	for i := 0; i < 10; i++ {
		epoch := node.currentEpoch()

		block := node.BuildLeaderBlock(epoch)
		if block.ID != epoch {
			t.Fatalf("epoch mismatch: expected %d, got block %d", epoch, block.ID)
		}
		if validatorsAtEpoch := node.freezeValidatorSetForHeight(epoch, node.GetConsensusValidators(int(epoch))); len(validatorsAtEpoch) == 0 {
			t.Fatalf("unresolved validator set at epoch %d", epoch)
		}
		_ = node.storeLeaderBlock(block)
		node.execVoteGuardMu.Lock()
		node.execVoteLimiter = nil
		node.execVoteSeen = make(map[string]time.Time)
		node.execVoteGuardMu.Unlock()

		execHash := block.StateRoot
		if execHash == "" {
			t.Fatalf("empty exec hash at epoch %d", epoch)
		}

		txMerkle := block.MempoolRoot

		for _, id := range validators {
			sig := ed25519.Sign(privKeys[id], execResultSignBytes(epoch, execHash, txMerkle))
			msg := ExecutionResultMsg{
				HeightHint: epoch,
				ExecHash:   execHash,
				TxMerkle:   txMerkle,
				Signer:     id,
				Signature:  hex.EncodeToString(sig),
			}
			node.processExecutionResultMsg(msg, false)
		}

		if got := node.Blockchain.Height(); got != epoch {
			leader, ok := node.getLeaderBlock(epoch)
			if !ok {
				t.Fatalf("commit failed at epoch %d: height=%d (missing leader block)", epoch, got)
			}
			finalCandidate := leader
			finalCandidate.BlockTime = LogicalTimeForEpochTick(epoch, TickFinalize)
			finalCandidate.Timestamp = int64(SystemTimeUnits(finalCandidate.BlockTime))
			finalCandidate.BlockHash = HashBlock(finalCandidate)
			verifyErr := node.VerifyBlock(finalCandidate, node.Blockchain)
			t.Fatalf("commit failed at epoch %d: height=%d proposer=%s round=%d verify_err=%v", epoch, got, finalCandidate.Proposer, finalCandidate.Round, verifyErr)
		}
	}

	if node.Blockchain.Height() < 10 {
		t.Fatalf("expected at least 10 commits, got height=%d", node.Blockchain.Height())
	}
}

func TestFinalizeExecutionResultRequiresPrecommitQuorumLock(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	results := []ExecutionResult{
		{Height: epoch, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "B", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "C", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}
	signers := []string{"A", "B", "C"}
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, res := range results {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, res); !ok {
			t.Fatalf("failed to record exec quorum result for signer %s", res.Signer)
		}
	}

	if node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers) {
		t.Fatalf("expected finalize to wait for precommit quorum lock")
	}
	if got := node.Blockchain.Height(); got != 0 {
		t.Fatalf("unexpected committed height without precommit quorum: got=%d", got)
	}
}

func TestFinalizeExecutionResultDefersWhileLocalNodeSyncing(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
	})
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false

	validators := []string{"A", "B", "C", "D"}
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	results := []ExecutionResult{
		{Height: epoch, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "B", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "C", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}
	signers := []string{"A", "B", "C"}
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, res := range results {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, res); !ok {
			t.Fatalf("failed to record exec quorum result for signer %s", res.Signer)
		}
	}
	node.execResultsMu.Lock()
	if ok := node.setQuorumLockedProposalLocked(block, "test_syncing_finality_defer", 3, 3); !ok {
		node.execResultsMu.Unlock()
		t.Fatalf("failed to set quorum precommit lock")
	}
	node.execResultsMu.Unlock()

	node.ID = "A"
	node.Role = "validator"
	node.Consensus.mu.Lock()
	node.Consensus.Syncing = true
	node.Consensus.syncInFlight = true
	node.Consensus.SyncTarget = epoch + 100
	node.Consensus.mu.Unlock()

	if node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers) {
		t.Fatalf("syncing validator must not synthesize a local final block from execution gossip")
	}
	if got := node.Blockchain.Height(); got != 0 {
		t.Fatalf("unexpected committed height while syncing: got=%d", got)
	}
}

func TestLocalExecutionFinalityReadyAllowsParticipationBlockedObserverApply(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
	})
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = true
	ValidatorOnboardingStrictActivation = true

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "D"
	node.Role = "validator"
	node.Consensus.mu.Lock()
	node.Consensus.Syncing = false
	node.Consensus.syncInFlight = false
	node.Consensus.SyncTarget = 0
	node.Consensus.Paused = false
	node.Consensus.mu.Unlock()

	if ready, reason := node.validatorParticipationGateStatus(1); ready || reason == "" {
		t.Fatalf("expected participation gate to be blocked in test setup, ready=%t reason=%q", ready, reason)
	}
	if ready, reason := node.localExecutionFinalityReady(1); !ready {
		t.Fatalf("observer apply finality must not require local participation, reason=%q", reason)
	}
}

func TestNewConsensusStateInitializesExecutionArchitectureView(t *testing.T) {
	cs := NewConsensusState(7)

	if cs.Height != 7 {
		t.Fatalf("unexpected height: got=%d want=7", cs.Height)
	}
	if cs.ExecVotes == nil {
		t.Fatalf("expected exec vote view to be initialized")
	}
	if cs.LockedBlock != "" {
		t.Fatalf("expected empty locked block, got=%q", cs.LockedBlock)
	}
	if cs.Committed {
		t.Fatalf("expected new consensus state to start uncommitted")
	}
}

func TestHardResetConsensusClearsExecutionArchitectureView(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Consensus.mu.Lock()
	node.Consensus.Height = 5
	node.Consensus.Round = 3
	node.Consensus.LockedBlock = "locked-block"
	node.Consensus.LockedBlockHash = "locked-block"
	node.Consensus.LockedRound = 3
	node.Consensus.Committed = true
	node.Consensus.ExecVotes = map[string]map[string]ExecutionResult{
		"locked-block": {
			"A": {
				Height:     5,
				BlockHash:  "locked-block",
				Signer:     "A",
				ResultHash: "exec-root",
				TxMerkle:   "tx-root",
			},
		},
	}
	node.Consensus.mu.Unlock()

	node.hardResetConsensus(6)

	node.Consensus.mu.Lock()
	defer node.Consensus.mu.Unlock()
	if node.Consensus.Height != 6 {
		t.Fatalf("unexpected reset height: got=%d want=6", node.Consensus.Height)
	}
	if node.Consensus.LockedBlock != "" || node.Consensus.LockedBlockHash != "" {
		t.Fatalf("expected locked block to clear on reset")
	}
	if node.Consensus.LockedRound != 0 {
		t.Fatalf("expected locked round to clear on reset, got=%d", node.Consensus.LockedRound)
	}
	if node.Consensus.Committed {
		t.Fatalf("expected committed flag to clear on reset")
	}
	if len(node.Consensus.ExecVotes) != 0 {
		t.Fatalf("expected exec votes to clear on reset, got=%d buckets", len(node.Consensus.ExecVotes))
	}
}

func TestSetQuorumLockedProposalLockedSyncsConsensusStateLock(t *testing.T) {
	target := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	epoch := target.currentEpoch()
	block := target.BuildLeaderBlock(epoch)
	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	target.execResultsMu.Lock()
	if ok := target.setQuorumLockedProposalLocked(block, "test_quorum_lock_sync", 3, 3); !ok {
		target.execResultsMu.Unlock()
		t.Fatalf("expected quorum lock to be recorded")
	}
	target.execResultsMu.Unlock()

	target.Consensus.mu.Lock()
	defer target.Consensus.mu.Unlock()
	if target.Consensus.LockedBlock != block.BlockHash {
		t.Fatalf("expected consensus lock to track block hash: got=%s want=%s", target.Consensus.LockedBlock, block.BlockHash)
	}
	if target.Consensus.LockedBlockHash != block.BlockHash {
		t.Fatalf("expected compatibility lock hash to track block hash: got=%s want=%s", target.Consensus.LockedBlockHash, block.BlockHash)
	}
	if target.Consensus.LockedRound != block.Round {
		t.Fatalf("expected locked round to track quorum lock: got=%d want=%d", target.Consensus.LockedRound, block.Round)
	}
}

func TestFinalizeExecutionResultCommitsAfterPrecommitQuorum(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	results := []ExecutionResult{
		{Height: epoch, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "B", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "C", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}
	signers := []string{"A", "B", "C"}
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, res := range results {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, res); !ok {
			t.Fatalf("failed to record exec quorum result for signer %s", res.Signer)
		}
	}

	node.execResultsMu.Lock()
	if ok := node.setQuorumLockedProposalLocked(block, "test_quorum_precommit", 3, 3); !ok {
		node.execResultsMu.Unlock()
		t.Fatalf("failed to set quorum precommit lock")
	}
	node.execResultsMu.Unlock()

	if !node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers) {
		t.Fatalf("expected finalize to commit after precommit quorum")
	}
	if got := node.Blockchain.Height(); got != epoch {
		t.Fatalf("expected committed height %d, got %d", epoch, got)
	}
	final := node.Blockchain.LastBlock()
	if final.ID != epoch {
		t.Fatalf("expected committed block at height %d, got %d", epoch, final.ID)
	}
	if !node.hasCommitQuorum(epoch, final.BlockHash) {
		t.Fatalf("expected commit quorum for finalized block hash %s", final.BlockHash)
	}
}

func TestFinalizeExecutionResultPreservesSignedQuorumPolicyMetadata(t *testing.T) {
	oldEmergency := ExecQuorumEmergencyEnabled
	oldTimeout := execQuorumEmergencyStallTimeout
	oldDrop := execQuorumEmergencyMaxDrop
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ExecQuorumEmergencyEnabled = oldEmergency
		execQuorumEmergencyStallTimeout = oldTimeout
		execQuorumEmergencyMaxDrop = oldDrop
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	ExecQuorumEmergencyEnabled = true
	execQuorumEmergencyStallTimeout = time.Second
	execQuorumEmergencyMaxDrop = 1

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		privKeys[id] = priv
	}

	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	epoch := node.currentEpoch()
	leader := node.consensusLeaderForHeightRound(epoch, 0, validators)
	if leader == "" {
		t.Fatalf("expected deterministic leader")
	}
	node.ID = leader
	node.ValidatorKey = ValidatorKey{
		ID:         leader,
		PublicKey:  ValidatorPubKeys[leader],
		PrivateKey: privKeys[leader],
	}

	block := node.BuildLeaderBlock(epoch)
	if len(block.Signature) == 0 || !VerifyBlockSignature(block) {
		t.Fatalf("expected signed proposal to verify before quorum-policy drift")
	}
	if strings.EqualFold(block.ConsensusMode, "DEGRADED") {
		t.Fatalf("expected proposal to start outside degraded mode")
	}
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	now := time.Now()
	node.validatorMu.Lock()
	for _, id := range []string{"A", "B"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      epoch,
			FinalizedHeight:     epoch - 1,
			ExecEpoch:           epoch,
			ValidatorSetHeight:  epoch,
			ValidatorSetHash:    block.ValidatorSetHash,
		}
	}
	for _, id := range []string{"C", "D"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             false,
			ConsensusReadyKnown: true,
			ReportedHeight:      epoch,
			FinalizedHeight:     epoch - 1,
			ExecEpoch:           epoch,
			ValidatorSetHeight:  epoch,
			ValidatorSetHash:    block.ValidatorSetHash,
		}
	}
	if node.validatorOfflineSince == nil {
		node.validatorOfflineSince = make(map[string]time.Time)
	}
	node.validatorOfflineSince["C"] = now
	node.validatorOfflineSince["D"] = now
	node.validatorMu.Unlock()

	node.commitMu.Lock()
	node.committedHeight = epoch - 1
	node.finalizedHeight = epoch - 1
	node.lastCommitAt = now.Add(-2 * time.Second)
	node.commitMu.Unlock()

	if policy := node.executionQuorumPolicy(epoch); policy.Relaxed || policy.Mode != "NORMAL" || policy.Required != 3 {
		t.Fatalf("expected current quorum policy to keep strict finality, got mode=%s relaxed=%t required=%d",
			policy.Mode, policy.Relaxed, policy.Required)
	}

	results := []ExecutionResult{
		{Height: epoch, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "B", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "C", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}
	signers := []string{"A", "B", "C"}
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, res := range results {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, res); !ok {
			t.Fatalf("failed to record exec quorum result for signer %s", res.Signer)
		}
	}

	node.execResultsMu.Lock()
	if ok := node.setQuorumLockedProposalLocked(block, "test_quorum_metadata_preserve", 3, 3); !ok {
		node.execResultsMu.Unlock()
		t.Fatalf("failed to set quorum precommit lock")
	}
	node.execResultsMu.Unlock()

	if !node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers) {
		t.Fatalf("expected finalize to commit despite runtime quorum-policy drift")
	}
	final := node.Blockchain.LastBlock()
	if final.ID != epoch {
		t.Fatalf("expected committed height %d, got %d", epoch, final.ID)
	}
	if final.ConsensusMode != block.ConsensusMode ||
		final.QuorumPolicyVersion != block.QuorumPolicyVersion ||
		final.ActiveReadyCount != block.ActiveReadyCount ||
		final.RequiredQuorum != block.RequiredQuorum ||
		final.StrictQuorum != block.StrictQuorum {
		t.Fatalf("final block quorum metadata drifted: proposal=%s/%s/%d/%d/%d final=%s/%s/%d/%d/%d",
			block.ConsensusMode, block.QuorumPolicyVersion, block.ActiveReadyCount, block.RequiredQuorum, block.StrictQuorum,
			final.ConsensusMode, final.QuorumPolicyVersion, final.ActiveReadyCount, final.RequiredQuorum, final.StrictQuorum)
	}
	if !VerifyBlockSignature(final) {
		t.Fatalf("expected finalized block to verify using preserved signed proposal metadata")
	}
}

func TestExecutionQuorumPolicyCountsLocalReadyValidator(t *testing.T) {
	oldValidatorRequireStake := ValidatorRequireStake
	oldConfigAuthRequireWallet := ConfigAuthRequireWallet
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorRequireStake = oldValidatorRequireStake
		ConfigAuthRequireWallet = oldConfigAuthRequireWallet
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false
	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		privKeys[id] = priv
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = ValidatorKey{ID: "A", PublicKey: ValidatorPubKeys["A"], PrivateKey: privKeys["A"]}

	now := time.Now()
	node.validatorMu.Lock()
	for _, id := range []string{"B", "C"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      1,
			FinalizedHeight:     0,
			ExecEpoch:           1,
			ValidatorSetHeight:  1,
		}
	}
	node.validatorMu.Unlock()

	policy := node.executionQuorumPolicy(1)
	if policy.ActiveReadyCount != 3 {
		t.Fatalf("expected local ready validator plus two remote ready validators, got active_ready=%d policy=%+v", policy.ActiveReadyCount, policy)
	}
	if policy.Required != 3 || policy.Mode != "NORMAL" {
		t.Fatalf("expected strict normal quorum, got mode=%s required=%d", policy.Mode, policy.Required)
	}
}

func TestCommitMetadataFailureClearsBadQuorumLock(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(1)
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = 2
	block.RequiredQuorum = 3
	block.StrictQuorum = 3
	block.BlockHash = HashBlock(block)
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if proposalKey == "" {
		t.Fatal("expected proposal key")
	}

	node.execResultsMu.Lock()
	node.acceptedProposal[acceptedProposalHeightKey(block.ID)] = proposalKey
	node.quorumLockedProposal[acceptedProposalHeightKey(block.ID)] = proposalKey
	node.acceptedProposalBlocks[proposalKey] = block
	node.localExecVoteByRound[block.ID] = map[uint32]string{block.Round: proposalKey}
	node.execResultsMu.Unlock()
	node.leaderMu.Lock()
	node.leaderBlocks[block.ID] = block
	node.leaderMu.Unlock()

	clearExecPoolProposal(block.ID, proposalKey)
	if _, ok, _ := recordExecResultGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot, ExecutionResult{
		Height:     block.ID,
		Round:      block.Round,
		BlockHash:  block.BlockHash,
		Signer:     "A",
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
	}); !ok {
		t.Fatal("failed to seed exec pool")
	}

	node.invalidateExecutionProposalAfterCommitFailure(block.ID, block, fmt.Errorf("required_quorum_exceeds_active_ready: required=3 ready=2"))

	node.execResultsMu.Lock()
	_, accepted := node.acceptedProposal[acceptedProposalHeightKey(block.ID)]
	_, locked := node.quorumLockedProposal[acceptedProposalHeightKey(block.ID)]
	_, cached := node.acceptedProposalBlocks[proposalKey]
	_, marker := node.localExecVoteByRound[block.ID][block.Round]
	node.execResultsMu.Unlock()
	if accepted || locked || cached || marker {
		t.Fatalf("expected invalid proposal lock/cache/local marker cleared accepted=%t locked=%t cached=%t marker=%t", accepted, locked, cached, marker)
	}
	if count := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); count != 0 {
		t.Fatalf("expected bad proposal exec pool cleared, got count=%d", count)
	}
	if current, ok := node.getLeaderBlock(block.ID); ok && current.BlockHash == block.BlockHash {
		t.Fatalf("expected bad leader block cleared")
	}
	next := block
	next.Round++
	next.StateRoot = "next-root"
	next.BlockHash = HashBlock(next)
	nextKey := proposalVoteKey(next.ID, next.Round, next.BlockHash, next.MempoolRoot, next.StateRoot)
	if _, ok, equivocation := recordExecResultGlobal(next.ID, nextKey, next.StateRoot, next.MempoolRoot, ExecutionResult{
		Height:     next.ID,
		Round:      next.Round,
		BlockHash:  next.BlockHash,
		Signer:     "A",
		ResultHash: next.StateRoot,
		TxMerkle:   next.MempoolRoot,
	}); !ok || equivocation {
		t.Fatalf("expected signer to be free to vote new proposal after invalidation, ok=%t equivocation=%t", ok, equivocation)
	}
}

func TestEmergencyQuorumGraceWindowKeepsMainnetMinimum(t *testing.T) {
	oldIsTestnet := IsTestnet
	oldTimeout := execQuorumEmergencyStallTimeout
	t.Cleanup(func() {
		IsTestnet = oldIsTestnet
		execQuorumEmergencyStallTimeout = oldTimeout
	})

	execQuorumEmergencyStallTimeout = 8 * time.Second

	IsTestnet = true
	if got := emergencyQuorumGraceWindow(); got != 8*time.Second {
		t.Fatalf("expected fast testnet emergency grace, got %s", got)
	}

	IsTestnet = false
	if got := emergencyQuorumGraceWindow(); got != mainnetDegradedGraceWindow() {
		t.Fatalf("expected mainnet grace minimum %s, got %s", mainnetDegradedGraceWindow(), got)
	}
}

func TestExecutionQuorumPolicyDoesNotRelaxWhenStrictReadyCountAvailable(t *testing.T) {
	oldEmergency := ExecQuorumEmergencyEnabled
	oldTimeout := execQuorumEmergencyStallTimeout
	oldDrop := execQuorumEmergencyMaxDrop
	t.Cleanup(func() {
		ExecQuorumEmergencyEnabled = oldEmergency
		execQuorumEmergencyStallTimeout = oldTimeout
		execQuorumEmergencyMaxDrop = oldDrop
	})

	ExecQuorumEmergencyEnabled = true
	execQuorumEmergencyStallTimeout = time.Second
	execQuorumEmergencyMaxDrop = 1

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	epoch := node.currentEpoch()
	now := time.Now()

	node.validatorMu.Lock()
	for _, id := range []string{"A", "B", "C"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      epoch,
			FinalizedHeight:     epoch - 1,
			ExecEpoch:           epoch,
			ValidatorSetHeight:  epoch,
		}
	}
	node.validatorStatus["D"] = &ValidatorStatus{
		LastSeen:            now,
		Active:              true,
		Enabled:             false,
		ConsensusReadyKnown: true,
		ReportedHeight:      epoch,
		FinalizedHeight:     epoch - 1,
		ExecEpoch:           epoch,
		ValidatorSetHeight:  epoch,
	}
	if node.validatorOfflineSince == nil {
		node.validatorOfflineSince = make(map[string]time.Time)
	}
	node.validatorOfflineSince["D"] = now
	node.validatorMu.Unlock()

	node.commitMu.Lock()
	node.committedHeight = epoch - 1
	node.finalizedHeight = epoch - 1
	node.lastCommitAt = now.Add(-2 * time.Second)
	node.commitMu.Unlock()

	policy := node.executionQuorumPolicy(epoch)
	if policy.Relaxed || policy.Mode != "NORMAL" || policy.Required != 3 {
		t.Fatalf("strict-ready quorum must remain normal 3-of-4, got %+v", policy)
	}
	if policy.ActiveReadyCount != 3 {
		t.Fatalf("unexpected active-ready count: got=%d want=3", policy.ActiveReadyCount)
	}
}

func TestExecutionQuorumPolicyContinuesRecentDegradedMode(t *testing.T) {
	oldEmergency := ExecQuorumEmergencyEnabled
	oldTimeout := execQuorumEmergencyStallTimeout
	oldDrop := execQuorumEmergencyMaxDrop
	t.Cleanup(func() {
		ExecQuorumEmergencyEnabled = oldEmergency
		execQuorumEmergencyStallTimeout = oldTimeout
		execQuorumEmergencyMaxDrop = oldDrop
	})

	ExecQuorumEmergencyEnabled = true
	execQuorumEmergencyStallTimeout = 30 * time.Second
	execQuorumEmergencyMaxDrop = 1

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	now := time.Now()

	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = append(node.Blockchain.Blocks, Block{
		ID:                  1,
		ConsensusMode:       "DEGRADED",
		QuorumPolicyVersion: quorumPolicyVersionV1,
		ActiveReadyCount:    2,
		RequiredQuorum:      2,
		StrictQuorum:        3,
	})
	node.Blockchain.mu.Unlock()

	node.validatorMu.Lock()
	for _, id := range []string{"A", "B"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      2,
			FinalizedHeight:     1,
			ExecEpoch:           2,
			ValidatorSetHeight:  2,
		}
	}
	for _, id := range []string{"C", "D"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now.Add(-2 * validatorLivenessHeartbeatTTL()),
			Active:              true,
			Enabled:             false,
			ConsensusReadyKnown: true,
			ReportedHeight:      1,
			FinalizedHeight:     1,
			ExecEpoch:           1,
			ValidatorSetHeight:  2,
		}
	}
	node.validatorMu.Unlock()

	node.commitMu.Lock()
	node.committedHeight = 1
	node.finalizedHeight = 1
	node.lastCommitAt = now
	node.commitMu.Unlock()

	policy := node.executionQuorumPolicy(2)
	if policy.Relaxed || policy.Mode != "NORMAL" || policy.Required != 3 {
		t.Fatalf("expected recent degraded mode below strict to be ignored, got %+v", policy)
	}
	if policy.Reason != "strict_finality_required" &&
		policy.Reason != "recent_degraded_below_strict_ignored" &&
		policy.Reason != "grace_window" {
		t.Fatalf("unexpected strict-finality reason: %s", policy.Reason)
	}
	if policy.StrictRequired != 3 || policy.ActiveReadyCount != 2 {
		t.Fatalf("unexpected quorum metadata: %+v", policy)
	}
}

func TestFinalizeExecutionResultRequiresStrictExecutionQuorumForCommitVotes(t *testing.T) {
	oldEmergency := ExecQuorumEmergencyEnabled
	oldTimeout := execQuorumEmergencyStallTimeout
	oldDrop := execQuorumEmergencyMaxDrop
	ExecQuorumEmergencyEnabled = true
	execQuorumEmergencyStallTimeout = 20 * time.Second
	execQuorumEmergencyMaxDrop = 1
	t.Cleanup(func() {
		ExecQuorumEmergencyEnabled = oldEmergency
		execQuorumEmergencyStallTimeout = oldTimeout
		execQuorumEmergencyMaxDrop = oldDrop
	})

	validators := []string{"A", "B", "C", "D"}
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	now := time.Now()
	node.validatorMu.Lock()
	for _, id := range []string{"A", "B"} {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			Active:             true,
			ReportedHeight:     1,
			FinalizedHeight:    1,
			ExecEpoch:          1,
			ValidatorSetHeight: 1,
		}
	}
	if node.validatorOfflineSince == nil {
		node.validatorOfflineSince = make(map[string]time.Time)
	}
	node.validatorOfflineSince["C"] = now
	node.validatorOfflineSince["D"] = now
	node.validatorMu.Unlock()
	node.commitMu.Lock()
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitAt = now.Add(-25 * time.Second)
	node.commitMu.Unlock()

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	results := []ExecutionResult{
		{Height: epoch, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: epoch, Signer: "B", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}
	signers := []string{"A", "B"}
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, res := range results {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, res); !ok {
			t.Fatalf("failed to record exec quorum result for signer %s", res.Signer)
		}
	}
	if got := node.executionQuorumRequired(epoch); got != 3 {
		t.Fatalf("expected strict execution quorum 3, got %d", got)
	}

	node.execResultsMu.Lock()
	if ok := node.setQuorumLockedProposalLocked(block, "test_strict_precommit_shortfall", 2, 3); !ok {
		node.execResultsMu.Unlock()
		t.Fatalf("failed to set quorum precommit lock")
	}
	node.execResultsMu.Unlock()

	if node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers) {
		t.Fatalf("did not expect finalize with only 2-of-4 execution quorum")
	}
	if got := node.Blockchain.Height(); got >= epoch {
		t.Fatalf("unexpected committed height %d with short quorum", got)
	}

	extra := ExecutionResult{Height: epoch, Signer: "C", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot}
	if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, extra); !ok {
		t.Fatalf("failed to record exec quorum result for signer C")
	}
	results = append(results, extra)
	signers = append(signers, "C")
	node.execResultsMu.Lock()
	_ = node.setQuorumLockedProposalLocked(block, "test_strict_precommit", 3, 3)
	node.execResultsMu.Unlock()

	if !node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers) {
		t.Fatalf("expected finalize to commit with strict execution quorum")
	}
	if got := node.Blockchain.Height(); got != epoch {
		t.Fatalf("expected committed height %d, got %d", epoch, got)
	}
	if !node.hasCommitQuorum(epoch, node.Blockchain.LastBlock().BlockHash) {
		t.Fatalf("expected commit quorum to use strict execution quorum")
	}
}

func TestFinalizeExecutionResultAlreadyCommittedStillMovesToNextHeight(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false

	validators := []string{"A", "B", "C", "D"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(63, "A")
	node.Consensus = &ConsensusState{
		Height:    1,
		Proposals: make(map[uint64]Block),
		Votes:     make(map[uint64]map[string]BlockVote),
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	node.Blockchain.AddBlock(block)
	node.cacheExecutionSnapshotLedger(block.ID, node.currentExecutionLedgerClone())
	node.markExecutionSnapshotReadyHeight(block.ID)
	node.commitMu.Lock()
	node.committedHeight = block.ID
	node.lastCommitHeight = block.ID
	node.lastCommitAt = time.Now()
	node.committed[block.ID] = block.BlockHash
	node.commitMu.Unlock()

	if !node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, nil, nil) {
		t.Fatalf("expected already-committed finalize path to continue consensus")
	}

	waitForConsensusTargetHeight(t, node, block.ID+1)
}

func TestProcessExecutionResultMsgIgnoresStaleProposalWithoutStrike(t *testing.T) {
	oldPenaltyMode := ConsensusPenaltyEnforceMode
	oldDebugConsensus := DebugConsensus
	oldValidatorPubKeys := ValidatorPubKeys
	t.Cleanup(func() {
		ConsensusPenaltyEnforceMode = oldPenaltyMode
		DebugConsensus = oldDebugConsensus
		ValidatorPubKeys = oldValidatorPubKeys
	})

	ConsensusPenaltyEnforceMode = "always_strict"
	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if block.ID != epoch {
		t.Fatalf("unexpected epoch block id: got=%d want=%d", block.ID, epoch)
	}
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	stale := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     block.Round,
		BlockHashHint: "stale-proposal-hash",
		SigVersion:    execResultSigVersionV2,
		ExecHash:      "stale-exec-hash",
		TxMerkle:      block.MempoolRoot,
		Signer:        "A",
	}
	sig := ed25519.Sign(priv, execResultSignBytesV2(stale.HeightHint, stale.RoundHint, stale.BlockHashHint, stale.ExecHash, stale.TxMerkle))
	stale.Signature = hex.EncodeToString(sig)

	node.processExecutionResultMsg(stale, false)

	node.execVoteGuardMu.Lock()
	_, hasStrike := node.execMismatch["A"]
	node.execVoteGuardMu.Unlock()
	if hasStrike {
		t.Fatalf("stale proposal vote should not create exec mismatch strike")
	}
}

func TestAllowExecutionVoteIngressScopesReplayByProposalAndSigner(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	if ok, reason := node.allowExecutionVoteIngress("A", 7, "proposal-1", "exec-1", "tx-1"); !ok {
		t.Fatalf("expected first tuple to pass, reason=%s", reason)
	}
	if ok, reason := node.allowExecutionVoteIngress("A", 7, "proposal-1", "exec-1", "tx-1"); !ok {
		t.Fatalf("expected uncredited duplicate tuple to retry, reason=%s", reason)
	}
	count, ok, equivocation := recordExecResultGlobal(7, "proposal-1", "exec-1", "tx-1", ExecutionResult{
		Height:     7,
		Signer:     "A",
		ResultHash: "exec-1",
		TxMerkle:   "tx-1",
	})
	if !ok || equivocation || count != 1 {
		t.Fatalf("expected tuple to be credited, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if ok, reason := node.allowExecutionVoteIngress("A", 7, "proposal-1", "exec-1", "tx-1"); ok || reason != "replay_cache" {
		t.Fatalf("expected credited duplicate tuple to be deduped, ok=%t reason=%s", ok, reason)
	}
	if ok, reason := node.allowExecutionVoteIngress("A", 7, "proposal-2", "exec-1", "tx-1"); !ok {
		t.Fatalf("expected different proposal to bypass replay cache, reason=%s", reason)
	}
	if ok, reason := node.allowExecutionVoteIngress("B", 7, "proposal-1", "exec-1", "tx-1"); !ok {
		t.Fatalf("expected different signer to bypass replay cache, reason=%s", reason)
	}
	if ok, reason := node.allowExecutionVoteIngress("A", 7, "proposal-1", "exec-2", "tx-1"); !ok {
		t.Fatalf("expected different exec hash to bypass replay cache, reason=%s", reason)
	}
}

func TestAllowExecutionVoteNetworkIngressAllowsRecentLagAndDuplicateVoteIDRetries(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	tip := Block{
		ID:        4,
		BlockHash: "tip-hash",
		BlockTime: LogicalTimeForEpoch(4),
	}
	tip.Timestamp = int64(SystemTimeUnits(tip.BlockTime))
	node.Blockchain.AddBlock(tip)
	node.commitMu.Lock()
	node.committedHeight = tip.ID
	node.commitMu.Unlock()

	fresh := ExecutionResultMsg{
		HeightHint:    5,
		RoundHint:     3,
		BlockHashHint: "block-5",
		ExecHash:      "exec-5",
		TxMerkle:      "tx-5",
		Signer:        "A",
	}
	if ok, reason := node.allowExecutionVoteNetworkIngress(fresh); !ok {
		t.Fatalf("expected fresh vote to pass ingress, reason=%s", reason)
	}
	if ok, reason := node.allowExecutionVoteNetworkIngress(fresh); !ok {
		t.Fatalf("expected duplicate vote ID retry to remain ingress-eligible, reason=%s", reason)
	}

	conflicting := fresh
	conflicting.RoundHint = fresh.RoundHint + 1
	conflicting.BlockHashHint = "block-5-alt"
	if ok, reason := node.allowExecutionVoteNetworkIngress(conflicting); !ok {
		t.Fatalf("expected conflicting block/round variant to bypass idempotent duplicate filter and stay ingress-eligible, reason=%s", reason)
	}

	recentLag := fresh
	recentLag.HeightHint = 2
	recentLag.RoundHint = 1
	recentLag.BlockHashHint = "block-2"
	recentLag.ExecHash = "exec-2"
	recentLag.TxMerkle = "tx-2"
	if ok, reason := node.allowExecutionVoteNetworkIngress(recentLag); !ok {
		t.Fatalf("expected vote within relaxed lag window to pass ingress, reason=%s", reason)
	}

	stale := recentLag
	stale.HeightHint = 1
	stale.BlockHashHint = "block-1"
	stale.ExecHash = "exec-1"
	stale.TxMerkle = "tx-1"
	if ok, reason := node.allowExecutionVoteNetworkIngress(stale); ok || reason != "ignored_late_vote" {
		t.Fatalf("expected stale epoch vote to be rejected, ok=%t reason=%s", ok, reason)
	}
	if ok, reason := node.allowExecutionVoteNetworkIngress(stale); ok || reason != "ignored_late_vote_cached" {
		t.Fatalf("expected repeated stale epoch vote to be cache-rejected, ok=%t reason=%s", ok, reason)
	}
}

func TestDuplicateExecutionVoteIngressRetryCanLandAfterQueuedUnresolvedVote(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	block := buildProposalForRound(t, epoch, 10, validators, sources)
	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	if ok, reason := target.allowExecutionVoteNetworkIngress(msg); !ok {
		t.Fatalf("expected first ingress attempt to pass, reason=%s", reason)
	}
	target.processExecutionResultMsg(msg, true)

	target.execResultsMu.Lock()
	queued := len(target.queuedExecVotes[fmt.Sprintf("%d", epoch)])
	target.execResultsMu.Unlock()
	if queued != 1 {
		t.Fatalf("expected unresolved vote to be queued once, got=%d", queued)
	}

	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store proposal for retry")
	}
	if ok, reason := target.allowExecutionVoteNetworkIngress(msg); !ok {
		t.Fatalf("expected duplicate retry to pass ingress after unresolved queue, reason=%s", reason)
	}
	target.processExecutionResultMsg(msg, true)

	key := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(epoch, key, block.StateRoot, block.MempoolRoot); got != 1 {
		t.Fatalf("expected duplicate ingress retry to land vote after proposal resolution, got=%d want=1", got)
	}
}

func TestHandleLeaderBlockVotesForIncomingProposalDirectly(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRequireSyncReady := ConsensusProposeRequiresSyncReady
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		ConsensusProposeRequiresSyncReady = oldRequireSyncReady
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
	})
	resetExecPoolForTest(t)

	ConsensusProposeRequiresSyncReady = false
	ValidatorOnboardingStrictActivation = false
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newValidatorRoundTestNode(t, t.TempDir(), "A", validators, ValidatorPubKeys["A"], privKeys["A"])
	const epoch uint64 = 1
	round := uint32(0)
	for ; round < 16; round++ {
		if normalizeValidatorID(sources["A"].consensusLeaderForHeightRound(epoch, round, validators)) != "A" {
			break
		}
	}
	if round == 16 {
		t.Fatalf("failed to find non-self leader round")
	}
	block := buildProposalForRound(t, epoch, round, validators, sources)

	target.handleLeaderBlock(block, "peer-direct")

	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot); got != 1 {
		t.Fatalf("expected target to record its local vote for incoming proposal, got=%d", got)
	}
	if !target.hasExecBroadcastedByValidator(epoch, proposalKey, "A") {
		t.Fatalf("expected target broadcast marker for incoming proposal")
	}
}

func TestProcessExecutionResultMsgAcceptsRecentLaggedEpoch(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	tip := Block{
		ID:        3,
		BlockHash: "tip-hash",
		BlockTime: LogicalTimeForEpoch(3),
	}
	tip.Timestamp = int64(SystemTimeUnits(tip.BlockTime))
	target.Blockchain.AddBlock(tip)
	const epoch uint64 = 1
	target.freezeValidatorSetForHeight(epoch, validators)
	block := buildProposalForRound(t, epoch, 10, validators, sources)
	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store lagged proposal")
	}

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, false)

	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot); got != 1 {
		t.Fatalf("expected recent lagged vote to record once, got=%d", got)
	}
}

func TestProcessExecutionResultMsgMirrorsActiveVoteIntoConsensusState(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	epoch := target.currentEpoch()
	block := buildProposalForRound(t, epoch, 10, validators, sources)
	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store proposal")
	}

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, false)

	target.Consensus.mu.Lock()
	defer target.Consensus.mu.Unlock()
	byBlock := target.Consensus.ExecVotes[block.BlockHash]
	if byBlock == nil {
		t.Fatalf("expected consensus exec votes for block %s", block.BlockHash)
	}
	got, ok := byBlock["B"]
	if !ok {
		t.Fatalf("expected signer B vote to be mirrored into consensus state")
	}
	if got.ResultHash != block.StateRoot {
		t.Fatalf("unexpected mirrored exec hash: got=%s want=%s", got.ResultHash, block.StateRoot)
	}
}

func TestProcessExecutionResultMsgSafelyIgnoresTooFarBehindVote(t *testing.T) {
	resetExecPoolForTest(t)

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Blockchain.AddBlock(Block{ID: 4, BlockHash: "tip-4"})

	msg := ExecutionResultMsg{
		HeightHint:    1,
		RoundHint:     7,
		BlockHashHint: "old-block",
		ExecHash:      "old-exec",
		TxMerkle:      "old-tx",
		Signer:        "A",
	}

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	node.processExecutionResultMsg(msg, false)

	ExecPool.mu.Lock()
	poolEntries := len(ExecPool.pool)
	ExecPool.mu.Unlock()
	if poolEntries != 0 {
		t.Fatalf("expected too-far-behind vote to be ignored without recording, pools=%d", poolEntries)
	}

	out := buf.String()
	if strings.Contains(out, "[VOTE-DROP]") {
		t.Fatalf("expected too-far-behind vote to be ignored without drop log, got: %s", out)
	}
}

func TestProcessExecutionResultMsgRejectsCommittedEpochVoteReplay(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	block := buildProposalForRound(t, epoch, 10, validators, sources)
	target.Blockchain.AddBlock(block)
	target.commitMu.Lock()
	target.committedHeight = epoch
	target.lastCommitHeight = epoch
	target.commitMu.Unlock()

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	target.processExecutionResultMsg(msg, false)
	target.processExecutionResultMsg(msg, false)
	if allowed, reason := target.allowExecutionVoteNetworkIngress(msg); allowed || reason != "ignored_committed_vote" {
		t.Fatalf("expected committed vote to be stopped at network ingress, allowed=%t reason=%q", allowed, reason)
	}
	if allowed, reason := target.allowExecutionVoteNetworkIngress(msg); allowed || reason != "ignored_committed_vote_cached" {
		t.Fatalf("expected committed vote replay to be cached at network ingress, allowed=%t reason=%q", allowed, reason)
	}

	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot); got != 0 {
		t.Fatalf("committed-height vote replay should not be recorded, got=%d", got)
	}
	if got := target.Blockchain.Height(); got != epoch {
		t.Fatalf("expected committed chain height to remain %d, got=%d", epoch, got)
	}
	out := buf.String()
	if !strings.Contains(out, "[VOTE-DROP] reason=stale_committed_height signer=B height=1 round=10") {
		t.Fatalf("expected committed-height replay drop log, got: %s", out)
	}
	if got := strings.Count(out, "[VOTE-DROP] reason=stale_committed_height signer=B height=1 round=10"); got != 1 {
		t.Fatalf("expected committed-height replay drop to be log-rate-limited, got %d logs: %s", got, out)
	}
	if strings.Contains(out, "[VOTE-ACCEPT]") {
		t.Fatalf("committed-height replay must not be accepted, got: %s", out)
	}
	if target.currentEpoch() != epoch+1 {
		t.Fatalf("committed replay rejection should leave next epoch unchanged, got=%d want=%d", target.currentEpoch(), epoch+1)
	}
}

func TestExecPoolSharesVotesAcrossRoundsForSameBlockHash(t *testing.T) {
	resetExecPoolForTest(t)

	const epoch uint64 = 7
	const execHash = "exec-root"
	const blockHash = "shared-block"
	const txMerkle = "shared-merkle"

	round10Key := proposalVoteKey(epoch, 10, blockHash, txMerkle, execHash)
	round11Key := proposalVoteKey(epoch, 11, blockHash, txMerkle, execHash)

	count, ok, equivocation := recordExecResultGlobal(epoch, round10Key, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		BlockHash:  blockHash,
		Signer:     "A",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if !ok || equivocation || count != 1 {
		t.Fatalf("expected first vote to record once, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}

	if got := getExecCountGlobal(epoch, round11Key, execHash, txMerkle); got != 1 {
		t.Fatalf("expected same-block higher round to see existing vote, got=%d", got)
	}

	count, ok, equivocation = recordExecResultGlobal(epoch, round11Key, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		BlockHash:  blockHash,
		Signer:     "A",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if ok || equivocation || count != 1 {
		t.Fatalf("expected duplicate signer across rounds to dedupe, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}

	count, ok, equivocation = recordExecResultGlobal(epoch, round11Key, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		BlockHash:  blockHash,
		Signer:     "B",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if !ok || equivocation || count != 2 {
		t.Fatalf("expected second signer on later round to reuse same pool, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}

	if got := getExecCountGlobal(epoch, round10Key, execHash, txMerkle); got != 2 {
		t.Fatalf("expected original round to see global pooled votes, got=%d", got)
	}
}

func TestExecutionVoteStateSurvivesRoundChangeForSameBlock(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	const epoch uint64 = 9
	const execHash = "exec-root"
	const blockHash = "shared-block"
	const txMerkle = "shared-merkle"

	round10Key := proposalVoteKey(epoch, 10, blockHash, txMerkle, execHash)
	round11Key := proposalVoteKey(epoch, 11, blockHash, txMerkle, execHash)

	if !node.markExecSignerSeenForProposal(epoch, round10Key, "A") {
		t.Fatalf("expected first signer mark to succeed")
	}
	if !node.hasExecSignerSeenForProposal(epoch, round11Key, "A") {
		t.Fatalf("expected signer-seen state to survive same-block round change")
	}
	if node.markExecSignerSeenForProposal(epoch, round11Key, "A") {
		t.Fatalf("expected same signer on same block across rounds to stay deduped")
	}

	if !node.markExecBroadcastedByValidator(epoch, round10Key, "A") {
		t.Fatalf("expected first validator broadcast mark to succeed")
	}
	if !node.hasExecBroadcastedByValidator(epoch, round11Key, "A") {
		t.Fatalf("expected validator broadcast state to survive same-block round change")
	}
	if node.markExecBroadcastedByValidator(epoch, round11Key, "A") {
		t.Fatalf("expected validator broadcast mark to remain deduped across rounds")
	}

	if !node.markExecBroadcasted(epoch, round10Key, execHash, txMerkle) {
		t.Fatalf("expected first exec broadcast mark to succeed")
	}
	if node.markExecBroadcasted(epoch, round11Key, execHash, txMerkle) {
		t.Fatalf("expected exec broadcast mark to stay block-scoped across rounds")
	}

	if allowed, reason := node.allowExecutionVoteIngress("A", epoch, round10Key, execHash, txMerkle); !allowed {
		t.Fatalf("expected first ingress vote to pass, reason=%s", reason)
	}
	if allowed, reason := node.allowExecutionVoteIngress("A", epoch, round11Key, execHash, txMerkle); !allowed {
		t.Fatalf("expected uncredited same-block higher-round retry to pass, reason=%s", reason)
	}
	count, ok, equivocation := recordExecResultGlobal(epoch, round10Key, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		Round:      10,
		BlockHash:  blockHash,
		Signer:     "A",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if !ok || equivocation || count != 1 {
		t.Fatalf("expected same-block vote to be credited, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if allowed, reason := node.allowExecutionVoteIngress("A", epoch, round11Key, execHash, txMerkle); allowed || reason != "replay_cache" {
		t.Fatalf("expected credited same-block higher-round vote to hit replay cache, allowed=%t reason=%s", allowed, reason)
	}

	const cooldown = 40 * time.Millisecond
	if node.shouldForceExecutionVoteRebroadcast(epoch, round10Key, 2, cooldown) {
		t.Fatalf("expected first shortfall observation to stay quiet")
	}
	time.Sleep(cooldown + 20*time.Millisecond)
	if !node.shouldForceExecutionVoteRebroadcast(epoch, round11Key, 2, cooldown) {
		t.Fatalf("expected rebroadcast state to carry across same-block round change")
	}
}

func TestProcessExecutionResultMsgLogsVoteAcceptAndReplayDrop(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	block := buildProposalForRound(t, epoch, 10, validators, sources)
	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store proposal")
	}

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	target.processExecutionResultMsg(msg, false)
	target.processExecutionResultMsg(msg, false)

	out := buf.String()
	if !strings.Contains(out, "[VOTE-ACCEPT] reason=recorded signer=B height=1 round=10") {
		t.Fatalf("expected vote accept log, got: %s", out)
	}
	if !strings.Contains(out, "[VOTE-DROP] reason=replay_cache signer=B height=1 round=10") {
		t.Fatalf("expected replay-cache drop log, got: %s", out)
	}
}

func TestProcessExecutionResultMsgQueuesCurrentRoundUnresolvedVoteAsStaleAccept(t *testing.T) {
	resetExecPoolForTest(t)

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     0,
		BlockHashHint: block.BlockHash,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        "B",
	}

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	node.processExecutionResultMsg(msg, true)

	node.execResultsMu.Lock()
	queued := len(node.queuedExecVotes[fmt.Sprintf("%d", epoch)])
	node.execResultsMu.Unlock()
	if queued != 1 {
		t.Fatalf("expected unresolved current-round vote to stay queued, got=%d", queued)
	}

	out := buf.String()
	if !strings.Contains(out, "[VOTE-STALE-ACCEPT] reason=queued_proposal_unresolved signer=B") {
		t.Fatalf("expected stale-accept log for unresolved current-round vote, got: %s", out)
	}
	if strings.Contains(out, "[VOTE-DROP] reason=queued_proposal_unresolved") {
		t.Fatalf("expected unresolved current-round vote to avoid hard drop log, got: %s", out)
	}
}

func TestHandleLeaderBlockCachesRejectedProposalForQueuedVoteReplay(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})
	resetExecPoolForTest(t)

	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 10, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 11, validators, sources)
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("failed to store higher-round proposal")
	}

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     lowBlock.Round,
		BlockHashHint: lowBlock.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      lowBlock.StateRoot,
		TxMerkle:      lowBlock.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, true)

	target.execResultsMu.Lock()
	queuedBefore := len(target.queuedExecVotes["1"])
	target.execResultsMu.Unlock()
	if queuedBefore != 1 {
		t.Fatalf("expected unresolved vote to queue once, got=%d", queuedBefore)
	}

	lowKey := proposalVoteKey(epoch, lowBlock.Round, lowBlock.BlockHash, lowBlock.MempoolRoot, lowBlock.StateRoot)
	if got := getExecCountGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot); got != 0 {
		t.Fatalf("expected no votes recorded before proposal is observed, got=%d", got)
	}
	if ok := target.verifyLeaderBlock(lowBlock, "peer-low"); !ok {
		t.Fatalf("expected lower-round proposal to pass verification so it can be cached")
	}

	target.handleLeaderBlock(lowBlock, "peer-low")

	target.execResultsMu.Lock()
	queuedAfter := len(target.queuedExecVotes["1"])
	observed, ok := target.acceptedProposalBlocks[lowKey]
	target.execResultsMu.Unlock()
	if queuedAfter != 0 {
		t.Fatalf("expected queued vote to replay after proposal arrival, got=%d", queuedAfter)
	}
	if !ok || observed.BlockHash != lowBlock.BlockHash {
		t.Fatalf("expected rejected proposal to stay cached for resolution")
	}
	if got := getExecCountGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot); got != 1 {
		t.Fatalf("expected queued vote to record once proposal was observed, got=%d", got)
	}
}

func TestProcessQueuedExecutionVotesForProposalReplaysOnlyMatchingVotes(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})
	resetExecPoolForTest(t)

	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 10, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 11, validators, sources)
	target.noteObservedProposal(lowBlock)

	matching := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     lowBlock.Round,
		BlockHashHint: lowBlock.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      lowBlock.StateRoot,
		TxMerkle:      lowBlock.MempoolRoot,
		Signer:        "B",
	}
	matching.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(matching.HeightHint, matching.RoundHint, matching.BlockHashHint, matching.ExecHash, matching.TxMerkle)))

	other := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     highBlock.Round,
		BlockHashHint: highBlock.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      highBlock.StateRoot,
		TxMerkle:      highBlock.MempoolRoot,
		Signer:        "C",
	}
	other.Signature = hex.EncodeToString(ed25519.Sign(privKeys["C"], execResultSignBytesV2(other.HeightHint, other.RoundHint, other.BlockHashHint, other.ExecHash, other.TxMerkle)))

	target.queueExecResult(matching)
	target.queueExecResult(other)

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	target.processQueuedExecutionVotesForProposal(lowBlock)

	lowKey := proposalVoteKey(epoch, lowBlock.Round, lowBlock.BlockHash, lowBlock.MempoolRoot, lowBlock.StateRoot)
	if got := getExecCountGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot); got != 1 {
		t.Fatalf("expected matching queued vote to replay once proposal arrived, got=%d", got)
	}

	target.execResultsMu.Lock()
	queued := append([]ExecutionResultMsg(nil), target.queuedExecVotes[fmt.Sprintf("%d", epoch)]...)
	target.execResultsMu.Unlock()
	if len(queued) != 1 {
		t.Fatalf("expected exactly one unrelated queued vote to remain, got=%d", len(queued))
	}
	if queued[0].Signer != "C" || queued[0].BlockHashHint != highBlock.BlockHash || queued[0].RoundHint != highBlock.Round {
		t.Fatalf("unexpected remaining queued vote: signer=%s round=%d block=%s", queued[0].Signer, queued[0].RoundHint, queued[0].BlockHashHint)
	}

	out := buf.String()
	if strings.Contains(out, "reason=queued_proposal_unresolved signer=C") {
		t.Fatalf("expected unrelated queued vote to remain untouched during proposal-specific replay, got logs: %s", out)
	}
}

func TestFutureLeaderBlockQueuesAndReplaysQueuedExecutionVote(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})
	resetExecPoolForTest(t)

	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	block1 := buildProposalForRound(t, 1, 0, validators, sources)
	for _, source := range sources {
		source.Blockchain.AddBlock(block1)
	}
	block2 := buildProposalForRound(t, 2, 0, validators, sources)

	msg := ExecutionResultMsg{
		HeightHint:    2,
		RoundHint:     block2.Round,
		BlockHashHint: block2.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block2.StateRoot,
		TxMerkle:      block2.MempoolRoot,
		Signer:        "C",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["C"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, true)
	target.handleLeaderBlock(block2, "peer-future")

	target.leaderMu.Lock()
	queuedLeaders := len(target.queuedFutureLeaderBlocks[2])
	target.leaderMu.Unlock()
	if queuedLeaders != 1 {
		t.Fatalf("expected future leader proposal to queue, got=%d", queuedLeaders)
	}
	if _, ok := target.getLeaderBlock(2); ok {
		t.Fatalf("future proposal should not be stored before local epoch catches up")
	}

	target.Blockchain.AddBlock(block1)
	target.replayQueuedLeaderBlocksForCurrentEpoch()

	stored, ok := target.getLeaderBlock(2)
	if !ok || stored.BlockHash != block2.BlockHash {
		t.Fatalf("expected queued future proposal to replay at epoch 2")
	}
	proposalKey := proposalVoteKey(2, block2.Round, block2.BlockHash, block2.MempoolRoot, block2.StateRoot)
	if got := getExecCountGlobal(2, proposalKey, block2.StateRoot, block2.MempoolRoot); got != 1 {
		t.Fatalf("expected queued execution vote to resolve after future proposal replay, got=%d", got)
	}
}

func TestCommittedUnresolvedExecutionVoteIsNotQueued(t *testing.T) {
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "TARGET"
	node.commitMu.Lock()
	node.committedHeight = 1
	node.commitMu.Unlock()

	msg := ExecutionResultMsg{
		HeightHint:    1,
		RoundHint:     0,
		BlockHashHint: "missing-committed-proposal",
		SigVersion:    execResultSigVersionV2,
		ExecHash:      strings.Repeat("a", 64),
		Signer:        "B",
	}

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	node.processExecutionResultMsg(msg, true)

	node.execResultsMu.Lock()
	queued := len(node.queuedExecVotes["1"])
	node.execResultsMu.Unlock()
	if queued != 0 {
		t.Fatalf("expected committed unresolved vote not to queue, got=%d", queued)
	}
	out := buf.String()
	if !strings.Contains(out, "[VOTE-STALE-ACCEPT] reason=committed_proposal_unresolved signer=B height=1") {
		t.Fatalf("expected committed stale vote log, got: %s", out)
	}
}

func TestHandleLeaderBlockCommitsImmediatelyWhenProposalAlreadyHasQuorumVotes(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})
	resetExecPoolForTest(t)

	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	const epoch uint64 = 1
	block := buildProposalForRound(t, epoch, 10, validators, sources)
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, signer := range []string{"A", "B", "C"} {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, ExecutionResult{
			Height:     epoch,
			BlockHash:  block.BlockHash,
			Signer:     signer,
			ResultHash: block.StateRoot,
			TxMerkle:   block.MempoolRoot,
		}); !ok {
			t.Fatalf("failed to preload exec quorum result for signer %s", signer)
		}
	}

	target.handleLeaderBlock(block, "peer-quorum")

	if got := target.Blockchain.Height(); got != epoch {
		t.Fatalf("expected accepted proposal to commit immediately at height %d, got=%d", epoch, got)
	}
	finalBlock, ok := target.Blockchain.GetBlock(epoch)
	if !ok {
		t.Fatalf("missing committed block at height %d", epoch)
	}
	if finalBlock.BlockHash == "" || finalBlock.StateRoot != block.StateRoot {
		t.Fatalf("unexpected committed block after immediate proposal commit: hash=%s state=%s want_state=%s", finalBlock.BlockHash, finalBlock.StateRoot, block.StateRoot)
	}
}

func TestShouldForceExecutionVoteRebroadcastWaitsForStuckShortfall(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	const epoch uint64 = 7
	const proposalKey = "proposal-7"

	if node.shouldForceExecutionVoteRebroadcast(epoch, proposalKey, 2, 100*time.Millisecond) {
		t.Fatalf("expected first shortfall observation to stay quiet")
	}
	if node.shouldForceExecutionVoteRebroadcast(epoch, proposalKey, 2, 100*time.Millisecond) {
		t.Fatalf("expected unchanged shortfall inside cooldown to stay quiet")
	}
	time.Sleep(120 * time.Millisecond)
	if !node.shouldForceExecutionVoteRebroadcast(epoch, proposalKey, 2, 100*time.Millisecond) {
		t.Fatalf("expected unchanged shortfall after cooldown to trigger rebroadcast")
	}
	if node.shouldForceExecutionVoteRebroadcast(epoch, proposalKey, 3, 100*time.Millisecond) {
		t.Fatalf("expected vote-count progress to reset rebroadcast state")
	}
}

func TestFinalizeExecutionResultRequiresValidLeaderCommitments(t *testing.T) {
	oldCommitmentHeight := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = oldCommitmentHeight }()

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if block.ID != epoch {
		t.Fatalf("unexpected epoch block id: got=%d want=%d", block.ID, epoch)
	}
	// Break commitment-v2 rule intentionally: next hash is required post-fork.
	block.NextValidatorSetHash = ""
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	ok := node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, nil, []string{"A", "B", "C"})
	if ok {
		t.Fatalf("expected finalization to fail when leader commitments are invalid")
	}
}

func TestFinalizeExecutionResultAppliesDeterministicSortingRule(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	epoch := node.currentEpoch()
	block := node.BuildLeaderBlock(epoch)
	if block.ID != epoch {
		t.Fatalf("unexpected epoch block id: got=%d want=%d", block.ID, epoch)
	}
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}
	node.execResultsMu.Lock()
	if ok := node.setQuorumLockedProposalLocked(block, "test_deterministic_sorting", 3, 3); !ok {
		node.execResultsMu.Unlock()
		t.Fatalf("failed to set precommit lock")
	}
	node.execResultsMu.Unlock()

	results := []ExecutionResult{
		{Signer: "b", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Signer: "a", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Signer: "C", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}
	signers := []string{"c", "A", "b", "A"}
	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	recorded := make(map[string]struct{}, len(results))
	for _, res := range results {
		signer := normalizeValidatorID(res.Signer)
		if signer == "" {
			continue
		}
		if _, seen := recorded[signer]; seen {
			continue
		}
		recorded[signer] = struct{}{}
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, res); !ok {
			t.Fatalf("failed to record exec quorum result for signer %s", res.Signer)
		}
	}

	ok := node.finalizeExecutionResult(epoch, block.StateRoot, block.MempoolRoot, results, signers)
	if !ok {
		t.Fatalf("expected finalization success")
	}

	finalBlock, ok := node.Blockchain.GetBlock(epoch)
	if !ok {
		t.Fatalf("missing finalized block at height %d", epoch)
	}

	wantSigners := []string{"A", "B", "C"}
	if !reflect.DeepEqual(finalBlock.Signatures, wantSigners) {
		t.Fatalf("unexpected deterministic signer order: got=%v want=%v", finalBlock.Signatures, wantSigners)
	}

	gotResultSigners := make([]string, 0, len(finalBlock.ExecutionResults))
	for _, res := range finalBlock.ExecutionResults {
		gotResultSigners = append(gotResultSigners, res.Signer)
	}
	if !reflect.DeepEqual(gotResultSigners, wantSigners) {
		t.Fatalf("unexpected deterministic execution-result signer order: got=%v want=%v", gotResultSigners, wantSigners)
	}
}

func TestLeaderFromExecHashUsesCanonicalSortingRule(t *testing.T) {
	execHash := "execution-hash"
	epoch := uint64(77)

	validatorsA := []string{"b", "A", "c", "a", "B"}
	validatorsB := []string{"C", "B", "A"}
	inputCopy := append([]string{}, validatorsA...)

	leaderA := leaderFromExecHash(execHash, epoch, validatorsA)
	leaderB := leaderFromExecHash(execHash, epoch, validatorsB)
	if leaderA != leaderB {
		t.Fatalf("leader selection should be permutation/case stable: got=%s want=%s", leaderA, leaderB)
	}
	if !reflect.DeepEqual(validatorsA, inputCopy) {
		t.Fatalf("leader selection mutated input validators: got=%v want=%v", validatorsA, inputCopy)
	}
}

func TestExecutionMismatchDiagnosticsHighSeverityOnTwoSigners(t *testing.T) {
	oldPenaltyMode := ConsensusPenaltyEnforceMode
	oldDebugConsensus := DebugConsensus
	oldQuarantine := ConsensusExecMismatchQuarantineAfter
	oldSlash := ConsensusExecMismatchSlashAfter
	t.Cleanup(func() {
		ConsensusPenaltyEnforceMode = oldPenaltyMode
		DebugConsensus = oldDebugConsensus
		ConsensusExecMismatchQuarantineAfter = oldQuarantine
		ConsensusExecMismatchSlashAfter = oldSlash
	})

	ConsensusPenaltyEnforceMode = "always_strict"
	DebugConsensus = false
	ConsensusExecMismatchQuarantineAfter = 4
	ConsensusExecMismatchSlashAfter = 5

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	epoch := uint64(4)

	var logBuf bytes.Buffer
	oldLogWriter := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(oldLogWriter)
	})

	node.handleExecutionMismatchPolicy("B", epoch, "expected-hash", "got-hash-1")
	if strings.Contains(logBuf.String(), "severity=high") {
		t.Fatalf("high-severity mismatch should not fire on first unique signer")
	}

	node.handleExecutionMismatchPolicy("C", epoch, "expected-hash", "got-hash-2")
	logs := logBuf.String()
	if !strings.Contains(logs, "[EXEC-MISMATCH] severity=high") {
		t.Fatalf("expected high-severity mismatch diagnostic after two unique signers, logs=%q", logs)
	}
	if got := node.executionMismatchUniqueSignersAtEpoch(epoch); got != 2 {
		t.Fatalf("unexpected unique signer count: got=%d want=2", got)
	}

	node.execVoteGuardMu.Lock()
	strikeB := node.execMismatch["B"].Count
	strikeC := node.execMismatch["C"].Count
	node.execVoteGuardMu.Unlock()
	if strikeB != 1 || strikeC != 1 {
		t.Fatalf("unexpected strike counts: B=%d C=%d", strikeB, strikeC)
	}
	if len(node.MisbehaviorLog) != 0 {
		t.Fatalf("unexpected slashing evidence for single-strike mismatches: %+v", node.MisbehaviorLog)
	}
}

func TestSafeModeGatesExecutionBroadcast(t *testing.T) {
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldRequireSyncReady := ConsensusProposeRequiresSyncReady
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	t.Cleanup(func() {
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ConsensusProposeRequiresSyncReady = oldRequireSyncReady
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
	})

	ConsensusPostBlockSafeModeEnabled = true
	ConsensusProposeRequiresSyncReady = false
	ValidatorOnboardingStrictActivation = false
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(44, "A")
	node.ValidatorTopic = &pubsub.Topic{}

	height := node.currentEpoch()
	now := time.Now()
	node.validatorSetMu.Lock()
	if node.safeModeUntilByHeight == nil {
		node.safeModeUntilByHeight = make(map[uint64]time.Time)
	}
	if node.safeModeWindowByHeight == nil {
		node.safeModeWindowByHeight = make(map[uint64]time.Duration)
	}
	node.safeModeUntilByHeight[height] = now.Add(30 * time.Second)
	node.safeModeWindowByHeight[height] = 2 * time.Second
	node.validatorSetMu.Unlock()

	node.broadcastExecutionResultInternal(height, "exec-hash", "tx-merkle", false)

	if len(node.execBroadcasted) != 0 {
		t.Fatalf("execution broadcast should remain gated under safe mode, got=%v", node.execBroadcasted)
	}
	if len(node.execBroadcastedByValidator) != 0 {
		t.Fatalf("validator broadcast marker should remain empty under safe mode, got=%v", node.execBroadcastedByValidator)
	}
}

func TestHeartbeatPriorityLoopTriggersImmediateForcedHeartbeat(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(62, "A")
	node.shutdownCh = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	node.SetRootContext(ctx, cancel)
	done := make(chan struct{})
	go func() {
		defer close(done)
		node.startHeartbeatPriorityLoop(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		close(node.shutdownCh)
		<-done
	})

	var first time.Time
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		node.heartbeatMu.Lock()
		first = node.lastHeartbeatAt
		node.heartbeatMu.Unlock()
		if !first.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if first.IsZero() {
		t.Fatalf("expected priority heartbeat loop to send initial heartbeat")
	}

	node.requestHeartbeatBroadcast(true)

	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		node.heartbeatMu.Lock()
		updated := node.lastHeartbeatAt
		node.heartbeatMu.Unlock()
		if updated.After(first) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected forced heartbeat trigger to bypass periodic delay")
}

func TestHeartbeatPriorityLoopWatchdogForcesStaleHeartbeat(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldTTL := ValidatorLivenessHeartbeatTTLSeconds
	oldGrace := ValidatorLivenessGraceSeconds
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ValidatorLivenessHeartbeatTTLSeconds = oldTTL
		ValidatorLivenessGraceSeconds = oldGrace
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ValidatorLivenessHeartbeatTTLSeconds = 1
	ValidatorLivenessGraceSeconds = 1

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(65, "A")
	node.shutdownCh = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	node.SetRootContext(ctx, cancel)
	done := make(chan struct{})
	go func() {
		defer close(done)
		node.startHeartbeatPriorityLoop(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		close(node.shutdownCh)
		<-done
	})

	var first time.Time
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		node.heartbeatMu.Lock()
		first = node.lastHeartbeatAt
		node.heartbeatMu.Unlock()
		if !first.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if first.IsZero() {
		t.Fatalf("expected initial heartbeat before watchdog test")
	}

	staleAt := time.Now().Add(-3500 * time.Millisecond)
	node.heartbeatMu.Lock()
	node.lastHeartbeatReported = node.Blockchain.Height()
	node.lastHeartbeatFinalized = node.committedHeight
	node.lastHeartbeatEpoch = node.currentEpoch()
	node.lastHeartbeatAt = staleAt
	node.heartbeatMu.Unlock()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		node.heartbeatMu.Lock()
		updated := node.lastHeartbeatAt
		node.heartbeatMu.Unlock()
		if updated.After(staleAt) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected heartbeat watchdog to force rebroadcast when stale")
}

func TestRunPostBlockEffectsAsyncKeepsConsensusStateResponsive(t *testing.T) {
	oldDebugConsensus := DebugConsensus
	DebugConsensus = false
	t.Cleanup(func() {
		DebugConsensus = oldDebugConsensus
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	tx := Transaction{ID: "tx-1"}
	node.Mempool.Transactions = []Transaction{tx}
	node.Consensus.Proposals[1] = Block{ID: 1, BlockHash: "proposal-1"}

	block := Block{
		ID:           1,
		Type:         BlockTypeWork,
		Proposer:     "A",
		Transactions: []Transaction{tx},
	}

	node.Consensus.FinalizeHeight(block.ID)
	if got := node.Consensus.Height; got != 2 {
		t.Fatalf("expected consensus height to advance synchronously: got=%d want=2", got)
	}

	ledger := node.applyPostBlockEffects(block)
	if len(node.Mempool.Transactions) != 0 {
		t.Fatalf("expected included txs removed synchronously before async tail")
	}

	node.runPostBlockEffectsAsync(block, ledger)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		node.Consensus.mu.Lock()
		cleaned := node.Consensus.LastCleanedHeight
		node.Consensus.mu.Unlock()
		if cleaned >= block.ID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected async post-commit cleanup to complete")
}

func TestPrepareExecutionBroadcastAcceptsCurrentLeaderState(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	ctx, ok := node.prepareExecutionBroadcast(block.ID, block.StateRoot, block.MempoolRoot)
	if !ok {
		t.Fatalf("expected execution broadcast context to be ready")
	}
	if ctx.ExecHash != block.StateRoot {
		t.Fatalf("unexpected exec hash: got=%q want=%q", ctx.ExecHash, block.StateRoot)
	}
	if ctx.BlockHashHint != block.BlockHash {
		t.Fatalf("unexpected block hash hint: got=%q want=%q", ctx.BlockHashHint, block.BlockHash)
	}
	if ctx.TipHeight+1 != block.ID {
		t.Fatalf("unexpected tip/height relation: tip=%d block=%d", ctx.TipHeight, block.ID)
	}
}

func TestReceiveBlockStartsNextRoundImmediately(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(63, "A")
	node.Consensus = &ConsensusState{
		Height:    1,
		Proposals: make(map[uint64]Block),
		Votes:     make(map[uint64]map[string]BlockVote),
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	if err := node.ReceiveBlock(block, node.Blockchain); err != nil {
		t.Fatalf("receive block: %v", err)
	}

	nextHeight := block.ID + 1
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gotHeight := node.Blockchain.Height()
		node.logicalMu.Lock()
		gotClock := node.logicalClock
		node.logicalMu.Unlock()
		if gotHeight >= nextHeight && gotClock.Epoch >= nextHeight {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	node.logicalMu.Lock()
	gotClock := node.logicalClock
	node.logicalMu.Unlock()
	t.Fatalf("expected commit path to continue immediately, got chain height=%d logical_epoch=%d want_at_least=%d",
		node.Blockchain.Height(), gotClock.Epoch, nextHeight)
}

func TestStartNextRoundImmediatelyForcesRoundZeroProposalOnConsensusLane(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(71, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	executedBefore := uint64(0)
	if node.ConsensusThread != nil {
		executedBefore = node.ConsensusThread.ExecutedCount()
	}
	node.startNextRoundImmediately(1, node.currentExecutionLedgerClone())

	if node.ConsensusThread != nil {
		waitForThreadExecuted(t, node.ConsensusThread, executedBefore)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		block, ok := node.getLeaderBlock(1)
		if ok && block.Round == 0 && normalizeValidatorID(block.Proposer) == "A" {
			node.Consensus.mu.Lock()
			round := node.Consensus.Round
			roundStart := node.Consensus.RoundStart
			node.Consensus.mu.Unlock()
			if round != 0 {
				t.Fatalf("expected consensus round 0, got %d", round)
			}
			if roundStart.IsZero() {
				t.Fatalf("expected round start to be recorded")
			}
			return
		}
		if node.Blockchain.Height() >= 1 && node.proposedRoundForHeight(1) == 0 {
			node.Consensus.mu.Lock()
			roundStart := node.Consensus.RoundStart
			node.Consensus.mu.Unlock()
			if roundStart.IsZero() {
				t.Fatalf("expected round start to be recorded before immediate proposal")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected immediate round-0 proposal on consensus lane")
}

func TestActivateConsensusStartsRoundZeroWithoutTickerDependency(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	oldRealTick := GlobalConfig.RealTick
	oldResultGossipOnly := ResultGossipOnly
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
		GlobalConfig.RealTick = oldRealTick
		ResultGossipOnly = oldResultGossipOnly
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false
	GlobalConfig.RealTick = time.Hour
	ResultGossipOnly = true

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(73, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !consensusStarted.Load() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Log("timed out waiting for consensus loop to stop")
	})

	if err := node.ActivateConsensus(ctx); err != nil {
		t.Fatalf("activate consensus: %v", err)
	}

	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		block, ok := node.getLeaderBlock(1)
		if ok && block.Round == 0 && normalizeValidatorID(block.Proposer) == "A" {
			return
		}
		if node.Blockchain.Height() >= 1 && node.proposedRoundForHeight(1) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected round-0 proposal to start without waiting for the consensus ticker")
}

func TestForceRoundZeroProposalIfLateForcesProposal(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	oldDeadlineGuard := ConsensusProposalDeadlineGuard
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
		ConsensusProposalDeadlineGuard = oldDeadlineGuard
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false
	ConsensusProposalDeadlineGuard = 200 * time.Millisecond

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(72, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	node.hardResetConsensus(1)
	node.Consensus.mu.Lock()
	node.Consensus.Round = 0
	node.Consensus.RoundStart = time.Now().Add(-proposalDeadlineGuardDuration() - 50*time.Millisecond)
	node.Consensus.Paused = false
	node.Consensus.Syncing = false
	node.Consensus.mu.Unlock()

	if ok := node.forceRoundZeroProposalIfLate(1, node.currentExecutionLedgerClone(), "unit_deadline_guard"); !ok {
		t.Fatalf("expected deadline guard to force round-0 proposal")
	}

	if block, ok := node.getLeaderBlock(1); ok {
		if block.Round != 0 {
			t.Fatalf("expected deadline guard to keep round 0, got %d", block.Round)
		}
		return
	}
	if node.Blockchain.Height() < 1 {
		t.Fatalf("expected deadline guard to build or commit the round-0 proposal")
	}
	if gotRound := node.proposedRoundForHeight(1); gotRound != 0 {
		t.Fatalf("expected deadline guard to keep proposed round 0, got %d", gotRound)
	}
}

func TestForceRoundProposalIfLateForcesHigherRoundProposal(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	oldDeadlineGuard := ConsensusProposalDeadlineGuard
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
		ConsensusProposalDeadlineGuard = oldDeadlineGuard
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false
	ConsensusProposalDeadlineGuard = 200 * time.Millisecond

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(75, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	node.hardResetConsensus(1)
	node.Consensus.mu.Lock()
	node.Consensus.Round = 1
	node.Consensus.RoundStart = time.Now().Add(-proposalDeadlineGuardDuration() - 50*time.Millisecond)
	node.Consensus.Paused = false
	node.Consensus.Syncing = false
	node.Consensus.mu.Unlock()

	if ok := node.forceRoundProposalIfLate(1, 1, node.currentExecutionLedgerClone(), "unit_deadline_guard_round1"); !ok {
		t.Fatalf("expected deadline guard to force round-1 proposal")
	}

	if block, ok := node.getLeaderBlock(1); ok {
		if block.Round != 1 {
			t.Fatalf("expected deadline guard to keep round 1, got %d", block.Round)
		}
		return
	}
	if node.Blockchain.Height() < 1 {
		t.Fatalf("expected deadline guard to build or commit the round-1 proposal")
	}
	if gotRound := node.proposedRoundForHeight(1); gotRound != 1 {
		t.Fatalf("expected deadline guard to keep proposed round 1, got %d", gotRound)
	}
}

func TestNearTipSyncGraceAllowsProposalAndExecBroadcast(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	oldDeadlineGuard := ConsensusProposalDeadlineGuard
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
		ConsensusProposalDeadlineGuard = oldDeadlineGuard
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false
	ConsensusProposalDeadlineGuard = 200 * time.Millisecond

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(76, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	node.hardResetConsensus(1)
	node.Consensus.mu.Lock()
	node.Consensus.Round = 1
	node.Consensus.RoundStart = time.Now().Add(-proposalDeadlineGuardDuration() - 50*time.Millisecond)
	node.Consensus.Paused = true
	node.Consensus.Syncing = true
	node.Consensus.syncInFlight = true
	node.Consensus.SyncTarget = 1
	node.Consensus.mu.Unlock()

	if ready, reason := node.validatorParticipationGateStatus(1); !ready {
		t.Fatalf("expected near-tip sync worker pause to unblock validator participation, reason=%q", reason)
	}
	if ready, reason := node.syncReadyForConsensus(1); !ready {
		t.Fatalf("expected near-tip sync grace to unblock consensus readiness, reason=%q", reason)
	}
	if ready, reason := node.localExecutionBroadcastReady(1); !ready {
		t.Fatalf("expected near-tip sync grace to unblock execution broadcast, reason=%q", reason)
	}
	if ok := node.forceRoundProposalIfLate(1, 1, node.currentExecutionLedgerClone(), "unit_near_tip_sync_grace"); !ok {
		t.Fatalf("expected near-tip sync grace to allow forced proposal")
	}
}

func TestReuseLeaderProposalForRoundRebroadcastsStoredOwnProposal(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false

	validators := []string{"A", "B", "C", "D"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(77, "A")
	node.Consensus = NewConsensusState(1)
	validatorKeys := map[string]ValidatorKey{
		"A": node.ValidatorKey,
		"B": strictActivationTestValidatorKey(78, "B"),
		"C": strictActivationTestValidatorKey(79, "C"),
		"D": strictActivationTestValidatorKey(80, "D"),
	}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validatorKeys))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validatorKeys))
	for id, key := range validatorKeys {
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
	node.validatorMu.Lock()
	for _, id := range validators {
		node.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           time.Now(),
			Active:             true,
			FinalizedHeight:    1,
			ReportedHeight:     1,
			ExecEpoch:          2,
			ValidatorSetHeight: 2,
			ValidatorSetHash:   ValidatorSetHash(validators),
		}
	}
	node.validatorMu.Unlock()

	round := uint32(0)
	for !node.isRoundLeader(1, round) {
		round++
		if round > 16 {
			t.Fatalf("failed to find local leader round for test")
		}
	}

	node.setProposedRound(1, round)
	block := node.BuildLeaderBlock(1)
	if !node.storeLeaderBlock(block) {
		t.Fatalf("expected initial leader block to store")
	}

	if ok := node.reuseLeaderProposalForRound(1, block.Round, "unit_reuse"); !ok {
		t.Fatalf("expected same-round proposal reuse to succeed")
	}

	got, ok := node.getLeaderBlock(1)
	if !ok {
		t.Fatalf("expected stored leader block to remain available")
	}
	if got.BlockHash != block.BlockHash || got.Round != block.Round {
		t.Fatalf("expected same-round proposal reuse to preserve stored block, got=%s/%d want=%s/%d", got.BlockHash, got.Round, block.BlockHash, block.Round)
	}

	if ok := node.reuseLeaderProposalForRound(1, block.Round+1, "unit_reuse"); ok {
		t.Fatalf("expected reuse to fail for a different round")
	}
}

func TestReceiveBlockAlreadyCommittedStillMovesToNextHeight(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false

	validators := []string{"A"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(63, "A")
	node.Consensus = &ConsensusState{
		Height:    1,
		Proposals: make(map[uint64]Block),
		Votes:     make(map[uint64]map[string]BlockVote),
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	block := node.BuildLeaderBlock(1)
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	node.Blockchain.AddBlock(block)
	node.cacheExecutionSnapshotLedger(block.ID, node.currentExecutionLedgerClone())
	node.markExecutionSnapshotReadyHeight(block.ID)
	node.commitMu.Lock()
	node.committedHeight = block.ID
	node.lastCommitHeight = block.ID
	node.lastCommitAt = time.Now()
	node.committed[block.ID] = block.BlockHash
	node.commitMu.Unlock()

	if err := node.ReceiveBlock(block, node.Blockchain); err != nil {
		t.Fatalf("receive committed block: %v", err)
	}

	waitForConsensusTargetHeight(t, node, block.ID+1)
}

func TestPrepareExecutionBroadcastRejectsTipHashMismatch(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.PrevHash = "wrong-prev-hash"
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store mutated leader block")
	}

	if _, ok := node.prepareExecutionBroadcast(block.ID, block.StateRoot, block.MempoolRoot); ok {
		t.Fatalf("expected tip hash mismatch to defer execution broadcast")
	}
}

func TestPrepareExecutionBroadcastRejectsChangedExecHash(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	if _, ok := node.prepareExecutionBroadcast(block.ID, "stale-exec-hash", block.MempoolRoot); ok {
		t.Fatalf("expected changed exec hash to defer execution broadcast")
	}
}

func TestAcceptedProposalReleasesStaleStateRootLock(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Blockchain.AddBlock(Block{ID: 1, BlockHash: "parent-hash"})
	node.commitMu.Lock()
	node.committedHeight = 1
	node.commitMu.Unlock()

	stale := node.BuildLeaderBlock(2)
	stale.Round = 11
	stale.StateRoot = "stale-state-root"
	stale.BlockHash = HashBlock(stale)

	incoming := node.BuildLeaderBlock(2)
	incoming.Round = 12
	incoming.BlockHash = HashBlock(incoming)

	if ok, expected := node.proposalMatchesLocalExecution(stale); ok || expected == "" {
		t.Fatalf("stale proposal should mismatch local execution: ok=%t expected=%q", ok, expected)
	}
	if ok, expected := node.proposalMatchesLocalExecution(incoming); !ok || expected == "" {
		t.Fatalf("incoming proposal should match local execution: ok=%t expected=%q root=%q", ok, expected, incoming.StateRoot)
	}

	node.execResultsMu.Lock()
	if !node.setAcceptedProposalLocked(stale, "test_stale_lock", true) {
		node.execResultsMu.Unlock()
		t.Fatalf("failed to install stale accepted proposal")
	}
	if !node.setAcceptedProposalLocked(incoming, "test_release_stale_lock", false) {
		node.execResultsMu.Unlock()
		t.Fatalf("expected stale accepted proposal to release for higher-round valid proposal")
	}
	gotKey := strings.TrimSpace(node.acceptedProposal[acceptedProposalHeightKey(incoming.ID)])
	node.execResultsMu.Unlock()

	wantKey := proposalVoteKey(incoming.ID, incoming.Round, incoming.BlockHash, incoming.MempoolRoot, incoming.StateRoot)
	if gotKey != wantKey {
		t.Fatalf("unexpected accepted proposal key: got=%q want=%q", gotKey, wantKey)
	}
}

func TestPublishExecutionResultRecordsLocalVoteWithoutPubSubLoopback(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	node.ValidatorKey = ValidatorKey{
		ID:         "A",
		PublicKey:  pub,
		PrivateKey: priv,
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), pub...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), pub...),
	}

	block := node.BuildLeaderBlock(node.currentEpoch())
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	ctx, ok := node.prepareExecutionBroadcastForBlock(block, block.StateRoot, block.MempoolRoot)
	if !ok {
		t.Fatalf("expected execution broadcast context to be ready")
	}

	node.publishExecutionResult(ctx, false)

	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if got := getExecCountGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot); got != 1 {
		t.Fatalf("expected local self-vote to be recorded immediately, got=%d want=1", got)
	}
	node.execResultsMu.Lock()
	seen := node.execSignerSeen[block.ID][execPoolScopeKey(block.ID, proposalKey)]["A"]
	node.execResultsMu.Unlock()
	if !seen {
		t.Fatalf("expected local signer to be tracked without pubsub loopback")
	}
}

func TestPublishExecutionResultSkipsLocalDoubleVoteForSameRound(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	if !node.allowLocalExecutionVoteRound(9, 12, "proposal-A") {
		t.Fatalf("expected first local vote for round to succeed")
	}
	if !node.allowLocalExecutionVoteRound(9, 12, "proposal-A") {
		t.Fatalf("expected idempotent local vote replay for same proposal")
	}
	if node.allowLocalExecutionVoteRound(9, 12, "proposal-B") {
		t.Fatalf("expected conflicting local vote for same height/round to be rejected")
	}
}

func TestBeginExecutionCommitApplyIsIdempotent(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	if !node.beginExecutionCommitApply(10, "hash-a") {
		t.Fatalf("expected first commit apply guard to start")
	}
	if node.beginExecutionCommitApply(10, "hash-a") {
		t.Fatalf("duplicate commit apply for same height/hash should be rejected")
	}
	if node.beginExecutionCommitApply(10, "hash-b") {
		t.Fatalf("conflicting commit apply for same height should be rejected")
	}

	node.finishExecutionCommitApply(10, "hash-a")
	if !node.beginExecutionCommitApply(10, "hash-a") {
		t.Fatalf("expected guard to allow retry after finish")
	}
	node.finishExecutionCommitApply(10, "hash-a")

	node.commitMu.Lock()
	node.committedHeight = 10
	node.committed[10] = "hash-a"
	node.commitMu.Unlock()
	if node.beginExecutionCommitApply(10, "hash-a") {
		t.Fatalf("committed height should not be applied again")
	}
}

func TestMaybeBroadcastExecutionVoteForBlockDefersWhileConsensusPaused(t *testing.T) {
	oldRequireSyncReady := ConsensusProposeRequiresSyncReady
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ConsensusProposeRequiresSyncReady = oldRequireSyncReady
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	ConsensusProposeRequiresSyncReady = false
	ValidatorOnboardingStrictActivation = false
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(45, "A")
	node.ValidatorTopic = &pubsub.Topic{}
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.Blockchain.AddBlock(Block{ID: 1, BlockHash: "parent-hash"})
	node.commitMu.Lock()
	node.committedHeight = 1
	node.commitMu.Unlock()

	block := node.BuildLeaderBlock(node.currentEpoch())

	node.Consensus.mu.Lock()
	node.Consensus.Paused = true
	node.Consensus.Syncing = false
	node.Consensus.SyncTarget = 0
	node.Consensus.mu.Unlock()

	if ok := node.maybeBroadcastExecutionVoteForBlock(block, "test_paused"); ok {
		t.Fatalf("expected execution vote to defer while paused")
	}
	if len(node.execBroadcasted) != 0 {
		t.Fatalf("execution vote should be deferred while paused, got=%v", node.execBroadcasted)
	}
	if len(node.execBroadcastedByValidator) != 0 {
		t.Fatalf("validator exec broadcast marker should stay empty while paused, got=%v", node.execBroadcastedByValidator)
	}

	// The periodic recovery loop calls this lower-level path directly.
	// It must enforce the same pause/sync gate as the normal broadcast wrapper.
	node.broadcastExecutionResultForBlockInternal(block, block.StateRoot, block.MempoolRoot, true)
	if len(node.execBroadcasted) != 0 {
		t.Fatalf("direct execution vote should be deferred while paused, got=%v", node.execBroadcasted)
	}
	if len(node.execBroadcastedByValidator) != 0 {
		t.Fatalf("direct validator exec broadcast marker should stay empty while paused, got=%v", node.execBroadcastedByValidator)
	}
}

func TestMaybeExitSyncModeBroadcastsDeferredLeaderExecutionVote(t *testing.T) {
	oldRequireSyncReady := ConsensusProposeRequiresSyncReady
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ConsensusProposeRequiresSyncReady = oldRequireSyncReady
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	ConsensusProposeRequiresSyncReady = false
	ValidatorOnboardingStrictActivation = false
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(46, "A")
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()
	ps, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		t.Fatalf("create pubsub: %v", err)
	}
	node.Host = host
	node.PubSub = ps
	if err := node.initPubSubTopics(); err != nil {
		t.Fatalf("init pubsub topics: %v", err)
	}
	block := node.BuildLeaderBlock(node.currentEpoch())
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	node.Consensus.mu.Lock()
	node.Consensus.Paused = true
	node.Consensus.Syncing = false
	node.Consensus.SyncTarget = 0
	node.Consensus.mu.Unlock()

	if cleared := node.maybeExitSyncMode("commit"); !cleared {
		t.Fatalf("expected maybeExitSyncMode to clear paused state")
	}
	if len(node.execBroadcasted) == 0 {
		t.Fatalf("expected deferred execution vote to broadcast after sync clear")
	}
	if len(node.execBroadcastedByValidator) == 0 {
		t.Fatalf("expected validator broadcast marker after sync clear")
	}
}

func TestConsensusRecomputePauseResumeBroadcastsDeferredLeaderExecutionVote(t *testing.T) {
	oldRequireSyncReady := ConsensusProposeRequiresSyncReady
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ConsensusProposeRequiresSyncReady = oldRequireSyncReady
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	ConsensusProposeRequiresSyncReady = false
	ValidatorOnboardingStrictActivation = false
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(47, "A")
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.Blockchain.AddBlock(Block{ID: 1, BlockHash: "parent-hash"})
	node.commitMu.Lock()
	node.committedHeight = 1
	node.commitMu.Unlock()

	block := node.BuildLeaderBlock(node.currentEpoch())
	if !node.storeLeaderBlock(block) {
		t.Fatalf("failed to store leader block")
	}

	node.recomputePauseMu.Lock()
	node.recomputePauseUntil = time.Now().Add(500 * time.Millisecond)
	node.recomputePauseHeight = block.ID
	node.recomputePauseReason = "round_cap_exceeded"
	node.recomputePauseApplied = false
	node.recomputePauseMu.Unlock()

	node.Consensus.mu.Lock()
	node.Consensus.Paused = false
	node.Consensus.Syncing = false
	node.Consensus.SyncTarget = 0
	node.Consensus.mu.Unlock()

	if ok := node.maybeBroadcastCurrentLeaderExecutionVote("during_recompute_pause"); ok {
		t.Fatalf("expected execution vote to defer while recompute pause is active")
	}
	if len(node.execBroadcasted) != 0 {
		t.Fatalf("execution vote should be deferred during recompute pause, got=%v", node.execBroadcasted)
	}
	if len(node.execBroadcastedByValidator) != 0 {
		t.Fatalf("validator exec broadcast marker should stay empty during recompute pause, got=%v", node.execBroadcastedByValidator)
	}

	node.recomputePauseMu.Lock()
	node.recomputePauseUntil = time.Now().Add(-10 * time.Millisecond)
	node.recomputePauseMu.Unlock()

	if active := node.consensusRecomputePauseActive(); active {
		t.Fatalf("expected expired recompute pause to release")
	}
	if len(node.execBroadcasted) == 0 {
		t.Fatalf("expected deferred execution vote to broadcast after recompute resume")
	}
	if len(node.execBroadcastedByValidator) == 0 {
		t.Fatalf("expected validator broadcast marker after recompute resume")
	}
}
