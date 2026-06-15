package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func (bc *Blockchain) LastBlock() Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.Blocks) == 0 {
		return Block{}
	}
	return bc.Blocks[len(bc.Blocks)-1]
}
func (n *Node) SignBlock(block *Block) {
	if block == nil {
		return
	}
	if n != nil && isValidatorSigningKeyUsable(n.ValidatorKey) {
		signerID := normalizeValidatorID(n.ValidatorKey.ID)
		proposerID := normalizeValidatorID(block.Proposer)
		if proposerID == "" {
			proposerID = signerID
			block.Proposer = signerID
		}
		if signerID != "" && proposerID == signerID {
			if ready, _ := n.validatorConsensusSigningAuthorityStatus(block.ID); !ready {
				return
			}
		}
	}
	n.applyBlockQuorumPolicyMetadata(block)
	hash := HashBlock(*block)
	block.BlockHash = hash
	if n == nil || !isValidatorSigningKeyUsable(n.ValidatorKey) {
		return
	}
	signerID := normalizeValidatorID(n.ValidatorKey.ID)
	proposerID := normalizeValidatorID(block.Proposer)
	if proposerID == "" {
		proposerID = signerID
		block.Proposer = signerID
	}
	// Only proposer can sign proposer-auth block.
	if signerID == "" || proposerID != signerID {
		return
	}
	if len(n.ValidatorKey.PublicKey) == ed25519.PublicKeySize {
		validatorPubKeysMu.Lock()
		ValidatorPubKeys[signerID] = append(ed25519.PublicKey(nil), n.ValidatorKey.PublicKey...)
		validatorPubKeysMu.Unlock()
	}
	sig, ok := n.signValidatorPayload([]byte(hash))
	if !ok {
		return
	}
	block.Signature = append([]byte(nil), sig...)
}

func (n *Node) applyBlockQuorumPolicyMetadata(block *Block) {
	if n == nil || block == nil || block.ID == 0 {
		return
	}
	validators, _, ok := n.deterministicCommitteeValidatorsForHeight(block.ID)
	if !ok || len(validators) == 0 {
		return
	}
	required := execQuorumRequired(len(validators))
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = len(validators)
	block.RequiredQuorum = required
	block.StrictQuorum = required
}

const executionStateRootVersionV1 = "v1"

type executionParentLedgerContext struct {
	ParentHeight        uint64
	ParentSource        string
	ParentHash          string
	RuntimeLedgerHash   string
	ExecutionLedgerHash string
}

type executionRootContext struct {
	ParentHeight        uint64
	ParentSource        string
	ParentHash          string
	ParentLedgerHash    string
	RuntimeLedgerHash   string
	ExecutionLedgerHash string
	RootVersion         string
}

type ExecutionSandbox struct {
	Ledger Ledger
}

func NewExecutionSandbox(parent Ledger) *ExecutionSandbox {
	return &ExecutionSandbox{Ledger: parent.Clone()}
}

func (s *ExecutionSandbox) ApplyBlock(n *Node, block Block) (Ledger, error) {
	if s == nil {
		return Ledger{}, errors.New("nil execution sandbox")
	}
	nextLedger, err := ApplyBlockStateWithNode(n, s.Ledger, block)
	if err != nil {
		return Ledger{}, err
	}
	s.Ledger = nextLedger.Clone()
	return s.Ledger.Clone(), nil
}

func executionStateRootVersionForHeight(height uint64) string {
	_ = height
	return executionStateRootVersionV1
}

func ledgerHasInitializedBacking(ledger Ledger) bool {
	return ledger.Balances != nil ||
		ledger.Nonces != nil ||
		ledger.Stakes != nil ||
		ledger.ValidatorRewardWallets != nil ||
		ledger.EVMState != nil ||
		ledger.EVMCode != nil ||
		ledger.EVMStorage != nil ||
		ledger.DTL != nil ||
		ledger.UsedValidatorUpdateCerts != nil
}

func (n *Node) committedBlockForLedgerHeight(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	if n.Blockchain != nil {
		if block, ok := n.Blockchain.GetBlock(height); ok {
			return block, true
		}
	}
	return n.LoadBlock(int(height))
}

func (n *Node) executionSnapshotLedgerMatchesBlock(height uint64, ledger Ledger) (bool, string, string) {
	if !ledgerHasInitializedBacking(ledger) {
		return false, "", ""
	}
	block, ok := n.committedBlockForLedgerHeight(height)
	if !ok || strings.TrimSpace(block.StateRoot) == "" {
		return true, "", strings.TrimSpace(HashLedger(ledger))
	}
	ledgerHash := strings.TrimSpace(HashLedger(ledger))
	expectedRoot := strings.TrimSpace(ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID)))
	return expectedRoot != "" && strings.EqualFold(expectedRoot, strings.TrimSpace(block.StateRoot)), expectedRoot, ledgerHash
}

func (n *Node) evictExecutionSnapshotLedger(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.snapshotExecutionLedgerMu.Lock()
	if n.snapshotExecutionLedgerByHeight != nil {
		delete(n.snapshotExecutionLedgerByHeight, height)
	}
	n.snapshotExecutionLedgerMu.Unlock()
}

func (n *Node) evictPostCommitLedger(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.postCommitLedgerMu.Lock()
	if n.postCommitLedgerByHeight != nil {
		delete(n.postCommitLedgerByHeight, height)
	}
	n.postCommitLedgerMu.Unlock()
}

func (n *Node) beginExecutionLedgerConsistencyCheck(height uint64) (uint64, bool) {
	if n == nil || height == 0 {
		return 0, false
	}
	n.executionLedgerConsistencyMu.Lock()
	defer n.executionLedgerConsistencyMu.Unlock()
	generation := n.executionLedgerGeneration
	if n.executionLedgerConsistencyHeight == height &&
		n.executionLedgerConsistencyGeneration == generation {
		return generation, false
	}
	if n.executionLedgerConsistencyCheckingHeight == height &&
		n.executionLedgerConsistencyCheckingGeneration == generation {
		// The authoritative cache is safe to return while another caller
		// performs the one strict live-ledger reconciliation for this height.
		return generation, false
	}
	n.executionLedgerConsistencyCheckingHeight = height
	n.executionLedgerConsistencyCheckingGeneration = generation
	n.executionLedgerConsistencyChecks++
	return generation, true
}

func (n *Node) finishExecutionLedgerConsistencyCheck(height uint64, generation uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	defer n.executionLedgerConsistencyMu.Unlock()
	if n.executionLedgerConsistencyCheckingHeight == height &&
		n.executionLedgerConsistencyCheckingGeneration == generation {
		n.executionLedgerConsistencyCheckingHeight = 0
		n.executionLedgerConsistencyCheckingGeneration = 0
	}
	if n.executionLedgerGeneration != generation {
		return
	}
	n.executionLedgerConsistencyHeight = height
	n.executionLedgerConsistencyGeneration = generation
}

func (n *Node) cancelExecutionLedgerConsistencyCheck(height uint64, generation uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	if n.executionLedgerConsistencyCheckingHeight == height &&
		n.executionLedgerConsistencyCheckingGeneration == generation {
		n.executionLedgerConsistencyCheckingHeight = 0
		n.executionLedgerConsistencyCheckingGeneration = 0
	}
	n.executionLedgerConsistencyMu.Unlock()
}

func (n *Node) markExecutionLedgerConsistent(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	n.executionLedgerConsistencyHeight = height
	n.executionLedgerConsistencyGeneration = n.executionLedgerGeneration
	n.executionLedgerConsistencyCheckingHeight = 0
	n.executionLedgerConsistencyCheckingGeneration = 0
	n.executionLedgerConsistencyMu.Unlock()
}

func (n *Node) blockExecutionLedgerRepair(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	n.executionLedgerRepairBlockedHeight = height
	n.executionLedgerConsistencyMu.Unlock()
}

func (n *Node) executionLedgerRepairBlocked(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	n.executionLedgerConsistencyMu.Lock()
	defer n.executionLedgerConsistencyMu.Unlock()
	if n.executionLedgerRepairBlockedHeight != height {
		return false
	}
	nextHeight := height
	if height < ^uint64(0) {
		nextHeight = height + 1
	}
	if allowed, _ := n.allowExecutionLedgerDriftRepair(nextHeight); allowed {
		n.executionLedgerRepairBlockedHeight = 0
		return false
	}
	return true
}

func (n *Node) currentExecutionLedgerFromAuthoritative(height uint64, authoritative Ledger, reason string) Ledger {
	if n == nil || height == 0 || !ledgerHasInitializedBacking(authoritative) {
		return authoritative
	}
	generation, shouldCheck := n.beginExecutionLedgerConsistencyCheck(height)
	if !shouldCheck {
		return authoritative
	}

	liveLedger := n.ExecutionLedger.Clone()
	liveHash := ""
	authoritativeHash := ""
	mismatch := !ledgerHasInitializedBacking(liveLedger)
	if !mismatch {
		liveHash = HashLedger(liveLedger)
		authoritativeHash = HashLedger(authoritative)
		mismatch = !strings.EqualFold(liveHash, authoritativeHash)
	}
	if mismatch {
		if liveHash == "" && ledgerHasInitializedBacking(liveLedger) {
			liveHash = HashLedger(liveLedger)
		}
		if authoritativeHash == "" {
			authoritativeHash = HashLedger(authoritative)
		}
		if n.executionLedgerRepairBlocked(height) {
			n.cancelExecutionLedgerConsistencyCheck(height, generation)
			if n.shouldLogLivenessReason(fmt.Sprintf("execution_ledger_authority_mismatch:%d", height), livenessReasonLogCooldown) {
				log.Printf("[LEDGER-DRIFT-CRITICAL] reason=%s height=%d mode=fail_closed repair_state=explicit_live_drift_lock live_ledger=%s authoritative_ledger=%s",
					strings.TrimSpace(reason),
					height,
					ShortHash(liveHash),
					ShortHash(authoritativeHash),
				)
			}
			return liveLedger
		}
		n.setExecutionLedger(authoritative)
		n.markExecutionLedgerConsistent(height)
		log.Printf("[LEDGER-REBUILD] reason=%s height=%d live_ledger=%s restored_ledger=%s",
			strings.TrimSpace(reason),
			height,
			ShortHash(liveHash),
			ShortHash(authoritativeHash),
		)
		return authoritative
	}
	n.finishExecutionLedgerConsistencyCheck(height, generation)
	return authoritative
}

func (n *Node) currentExecutionLedgerClone() Ledger {
	if n == nil {
		return Ledger{}.Clone()
	}
	tipHeight := uint64(0)
	if n.Blockchain != nil {
		tipHeight = n.Blockchain.Height()
	}
	n.commitMu.Lock()
	if n.committedHeight > tipHeight {
		tipHeight = n.committedHeight
	}
	n.commitMu.Unlock()
	if tipHeight > 0 {
		if cachedPostCommitLedger, ok := n.cachedPostCommitLedger(tipHeight); ok && ledgerHasInitializedBacking(cachedPostCommitLedger) {
			return n.currentExecutionLedgerFromAuthoritative(tipHeight, cachedPostCommitLedger, "current_execution_snapshot_preferred")
		}
		if restored, ok := n.committedTipLedgerFromExecutionSnapshot(tipHeight); ok && ledgerHasInitializedBacking(restored) {
			return n.currentExecutionLedgerFromAuthoritative(tipHeight, restored, "current_execution_tip_replay")
		}
	}
	if ledgerHasInitializedBacking(n.ExecutionLedger) {
		cloned := n.ExecutionLedger.Clone()
		if !strings.EqualFold(HashLedger(n.Ledger.Clone()), HashLedger(cloned)) {
			n.Ledger = cloned.Clone()
		}
		return cloned
	}
	if tipHeight > 0 {
		if restored, ok := n.committedTipLedgerFromExecutionSnapshot(tipHeight); ok && ledgerHasInitializedBacking(restored) {
			n.setExecutionLedger(restored)
			return restored.Clone()
		}
		if n.shouldLogLivenessReason(fmt.Sprintf("execution_ledger_runtime_fallback:%d", tipHeight), livenessReasonLogCooldown) {
			log.Printf("[EXEC-LEDGER-FALLBACK] height=%d source=runtime runtime_ledger=%s",
				tipHeight,
				ShortHash(HashLedger(n.Ledger.Clone())),
			)
		}
	}
	return n.Ledger.Clone()
}

func (n *Node) committedExecutionLedgerForPostBlockEffects(block Block) Ledger {
	if n == nil {
		return Ledger{}.Clone()
	}
	matchesCommittedRoot := func(ledger Ledger) bool {
		if !ledgerHasInitializedBacking(ledger) {
			return false
		}
		if block.ID == 0 || strings.TrimSpace(block.StateRoot) == "" {
			return true
		}
		expectedRoot := ComputeExecHashVersioned(block, HashLedger(ledger), executionStateRootVersionForHeight(block.ID))
		return expectedRoot != "" && strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot))
	}

	if cachedLedger, ok := n.cachedExecutionSnapshotLedger(block.ID); ok && matchesCommittedRoot(cachedLedger) {
		return cachedLedger.Clone()
	}
	if snap, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(block.ID); ok && snap != nil && matchesCommittedRoot(snap.Ledger) {
		return snap.Ledger.Clone()
	}
	if matchesCommittedRoot(n.ExecutionLedger) {
		return n.ExecutionLedger.Clone()
	}
	if matchesCommittedRoot(n.Ledger) {
		return n.Ledger.Clone()
	}

	fallback := n.currentExecutionLedgerClone()
	if !matchesCommittedRoot(fallback) && n.shouldLogLivenessReason(fmt.Sprintf("post_block_effects_execution_snapshot_unmatched:%d", block.ID), livenessReasonLogCooldown) {
		log.Printf("[LEDGER-REBUILD] reason=post_block_effects_execution_snapshot_unmatched height=%d fallback_ledger=%s block_root=%s",
			block.ID,
			ShortHash(HashLedger(fallback)),
			ShortHash(block.StateRoot),
		)
	}
	return fallback.Clone()
}

func (n *Node) committedTipLedgerFromExecutionSnapshot(height uint64) (Ledger, bool) {
	if n == nil || height == 0 {
		return Ledger{}, false
	}
	var (
		authoritative Ledger
		ok            bool
	)
	if cachedPostCommitLedger, found := n.cachedPostCommitLedger(height); found {
		return cachedPostCommitLedger.Clone(), true
	}
	if cachedLedger, found := n.cachedExecutionSnapshotLedger(height); found {
		if matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, cachedLedger); matched {
			authoritative = cachedLedger
			ok = true
		} else {
			n.evictExecutionSnapshotLedger(height)
			n.evictPostCommitLedger(height)
			if n.shouldLogLivenessReason(fmt.Sprintf("execution_snapshot_cache_mismatch:%d", height), livenessReasonLogCooldown) {
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-EVICT] reason=execution_snapshot_cache_mismatch height=%d snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
	}
	if !ok {
		if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil {
			if matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger); matched {
				authoritative = snap.Ledger.Clone()
				ok = true
				n.cacheExecutionSnapshotLedger(height, authoritative)
			} else if n.shouldLogLivenessReason(fmt.Sprintf("execution_snapshot_storage_mismatch:%d", height), livenessReasonLogCooldown) {
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-REJECT] reason=execution_snapshot_storage_mismatch height=%d snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
	}
	if !ok || !ledgerHasInitializedBacking(authoritative) {
		return Ledger{}, false
	}
	if n.Blockchain == nil {
		return authoritative.Clone(), true
	}
	block, found := n.Blockchain.GetBlock(height)
	if !found || strings.TrimSpace(block.StateRoot) == "" {
		return authoritative.Clone(), true
	}
	restored := n.replayPostBlockEffectsToLedger(block, authoritative)
	n.cachePostCommitLedger(height, restored)
	return restored.Clone(), true
}

func (n *Node) cachePostCommitLedger(height uint64, ledger Ledger) {
	if n == nil || height == 0 {
		return
	}
	n.postCommitLedgerMu.Lock()
	defer n.postCommitLedgerMu.Unlock()
	if n.postCommitLedgerByHeight == nil {
		n.postCommitLedgerByHeight = make(map[uint64]Ledger)
	}
	n.postCommitLedgerByHeight[height] = ledger.Clone()
	cacheDepth := n.ledgerMemoryCacheDepth()
	removed := 0
	for h := range n.postCommitLedgerByHeight {
		if h+cacheDepth <= height {
			delete(n.postCommitLedgerByHeight, h)
			removed++
		}
	}
	maybeReleaseMemoryAfterLedgerCachePrune(removed, height)
}

func (n *Node) cachedPostCommitLedger(height uint64) (Ledger, bool) {
	if n == nil || height == 0 {
		return Ledger{}, false
	}
	n.postCommitLedgerMu.Lock()
	defer n.postCommitLedgerMu.Unlock()
	if n.postCommitLedgerByHeight == nil {
		return Ledger{}, false
	}
	ledger, ok := n.postCommitLedgerByHeight[height]
	if !ok {
		return Ledger{}, false
	}
	return ledger.Clone(), true
}

func (n *Node) setExecutionLedger(ledger Ledger) {
	if n == nil {
		return
	}
	cloned := ledger.Clone()
	n.ExecutionLedger = cloned
	n.Ledger = cloned.Clone()
	n.executionLedgerConsistencyMu.Lock()
	n.executionLedgerGeneration++
	n.executionLedgerConsistencyHeight = 0
	n.executionLedgerConsistencyGeneration = 0
	n.executionLedgerRepairBlockedHeight = 0
	n.executionLedgerConsistencyMu.Unlock()
}

func (n *Node) mutateAuthoritativeLedger(mutator func(*Ledger)) Ledger {
	if n == nil {
		return Ledger{}.Clone()
	}
	ledger := n.currentExecutionLedgerClone()
	if mutator != nil {
		mutator(&ledger)
	}
	n.setExecutionLedger(ledger)
	return ledger.Clone()
}

func (n *Node) observeConsensusExecutionDrift(height uint64, reason string, parentLedgerHash string, runtimeLedgerHash string, executionLedgerHash string) {
	if n == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(parentLedgerHash), strings.TrimSpace(runtimeLedgerHash)) &&
		strings.EqualFold(strings.TrimSpace(parentLedgerHash), strings.TrimSpace(executionLedgerHash)) {
		return
	}
	log.Printf("[EXEC-CHECK] height=%d reason=%s parent_ledger=%s runtime_ledger=%s execution_ledger=%s",
		height,
		reason,
		ShortHash(parentLedgerHash),
		ShortHash(runtimeLedgerHash),
		ShortHash(executionLedgerHash),
	)
}

func (n *Node) resetRuntimeLedgerToExecution(reason string, height uint64) bool {
	if n == nil {
		return false
	}
	execLedger := n.currentExecutionLedgerClone()
	if !ledgerHasInitializedBacking(execLedger) {
		return false
	}
	runtimeHash := HashLedger(n.Ledger.Clone())
	execHash := HashLedger(execLedger)
	if strings.EqualFold(runtimeHash, execHash) {
		return false
	}
	n.setExecutionLedger(execLedger)
	log.Printf("[LEDGER-RESET] reason=%s height=%d runtime_ledger=%s execution_ledger=%s",
		reason,
		height,
		ShortHash(runtimeHash),
		ShortHash(execHash),
	)
	return true
}

func (n *Node) restoreLedgersFromAuthoritativeExecution(height uint64, reason string) bool {
	if n == nil || height == 0 {
		return false
	}
	runtimeHash := HashLedger(n.Ledger.Clone())
	executionHash := HashLedger(n.currentExecutionLedgerClone())
	var (
		authoritative Ledger
		source        string
		ok            bool
	)
	loadAuthoritative := func() bool {
		if cachedLedger, found := n.cachedExecutionSnapshotLedger(height); found {
			matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, cachedLedger)
			if matched {
				authoritative = cachedLedger
				source = "execution_cache"
				ok = true
				return true
			}
			n.evictExecutionSnapshotLedger(height)
			n.evictPostCommitLedger(height)
			if n.shouldLogLivenessReason(fmt.Sprintf("restore_execution_snapshot_cache_mismatch:%d", height), livenessReasonLogCooldown) {
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-EVICT] reason=execution_snapshot_cache_mismatch height=%d source=execution_cache snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
		if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil {
			matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger)
			if matched {
				authoritative = snap.Ledger.Clone()
				source = "trusted_snapshot"
				ok = true
				n.cacheExecutionSnapshotLedger(height, authoritative)
				return true
			}
			if n.shouldLogLivenessReason(fmt.Sprintf("restore_execution_snapshot_storage_mismatch:%d", height), livenessReasonLogCooldown) {
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-REJECT] reason=execution_snapshot_storage_mismatch height=%d source=trusted_snapshot snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
		return false
	}
	loadAuthoritative()
	if !ok && n.startupExecutionSnapshotCanRebuildLocally(height) {
		if err := n.rebuildTrustedExecutionSnapshotsUpTo(height); err != nil {
			if n.shouldLogLivenessReason(fmt.Sprintf("restore_execution_snapshot_rebuild_failed:%d:%s", height, err.Error()), livenessReasonLogCooldown) {
				log.Printf("[LEDGER-REBUILD-REJECT] reason=execution_snapshot_rebuild_failed height=%d err=%v", height, err)
			}
		} else {
			loadAuthoritative()
		}
	}
	if !ok || !ledgerHasInitializedBacking(authoritative) {
		return false
	}
	authoritativeHash := HashLedger(authoritative)
	restored := authoritative.Clone()
	restoredHash := authoritativeHash
	if n.Blockchain != nil {
		if block, found := n.Blockchain.GetBlock(height); found && strings.TrimSpace(block.StateRoot) != "" {
			restored = n.replayPostBlockEffectsToLedger(block, authoritative)
			restoredHash = HashLedger(restored)
			source += "_post_effects"
		}
	}
	n.cachePostCommitLedger(height, restored)
	n.setExecutionLedger(restored)
	log.Printf("[LEDGER-REBUILD] reason=%s height=%d source=%s runtime_ledger=%s execution_ledger=%s authoritative_ledger=%s restored_ledger=%s",
		reason,
		height,
		source,
		ShortHash(runtimeHash),
		ShortHash(executionHash),
		ShortHash(authoritativeHash),
		ShortHash(restoredHash),
	)
	return true
}

func (n *Node) authoritativeExecutionSnapshotLedger(height uint64) (Ledger, string, bool) {
	if n == nil || height == 0 {
		return Ledger{}, "", false
	}
	if cachedLedger, found := n.cachedExecutionSnapshotLedger(height); found && ledgerHasInitializedBacking(cachedLedger) {
		if matched, _, _ := n.executionSnapshotLedgerMatchesBlock(height, cachedLedger); matched {
			return cachedLedger.Clone(), "execution_cache", true
		}
		n.evictExecutionSnapshotLedger(height)
		n.evictPostCommitLedger(height)
	}
	if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil && ledgerHasInitializedBacking(snap.Ledger) {
		if matched, _, _ := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger); matched {
			return snap.Ledger.Clone(), "trusted_snapshot", true
		}
	}
	return Ledger{}, "", false
}

func (n *Node) legacyExecutionSnapshotStateRootForBlock(block Block, reason string) (string, executionRootContext, bool) {
	ctx := executionRootContext{RootVersion: executionStateRootVersionForHeight(block.ID)}
	if n == nil || block.ID <= 1 {
		return "", ctx, false
	}
	parentHeight := block.ID - 1
	parentLedger, source, ok := n.authoritativeExecutionSnapshotLedger(parentHeight)
	if !ok {
		ctx.ParentSource = "execution_snapshot_unavailable"
		return "", ctx, false
	}
	ctx.ParentHeight = parentHeight
	ctx.ParentSource = source + "_legacy_parent"
	ctx.ParentLedgerHash = HashLedger(parentLedger)
	ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
	ctx.ExecutionLedgerHash = HashLedger(n.currentExecutionLedgerClone())
	if parentBlock, found := n.LoadBlock(int(parentHeight)); found {
		ctx.ParentHash = strings.TrimSpace(parentBlock.BlockHash)
		if strings.TrimSpace(block.PrevHash) != "" && ctx.ParentHash != "" &&
			!strings.EqualFold(strings.TrimSpace(block.PrevHash), ctx.ParentHash) {
			ctx.ParentSource = "legacy_parent_hash_mismatch"
			return "", ctx, false
		}
	} else if n.Blockchain != nil {
		if parentBlock, found := n.Blockchain.GetBlock(parentHeight); found {
			ctx.ParentHash = strings.TrimSpace(parentBlock.BlockHash)
			if strings.TrimSpace(block.PrevHash) != "" && ctx.ParentHash != "" &&
				!strings.EqualFold(strings.TrimSpace(block.PrevHash), ctx.ParentHash) {
				ctx.ParentSource = "legacy_parent_hash_mismatch"
				return "", ctx, false
			}
		}
	}

	sandbox := NewExecutionSandbox(parentLedger)
	newLedger, err := sandbox.ApplyBlock(n, block)
	if err != nil {
		ctx.ParentSource = "legacy_parent_apply_failed"
		return "", ctx, false
	}
	expectedRoot := ComputeExecHashVersioned(block, HashLedger(newLedger), ctx.RootVersion)
	if expectedRoot == "" || !strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(expectedRoot)) {
		return expectedRoot, ctx, false
	}
	// The committed chain may contain blocks sealed before post-commit reward
	// replay completed on the proposer. Cache this verified parent choice so the
	// immediately following ProcessBlock execution uses the same deterministic
	// parent ledger that passed state-root verification.
	n.cachePostCommitLedger(parentHeight, parentLedger)
	n.setExecutionLedger(parentLedger)
	if n.shouldLogLivenessReason(fmt.Sprintf("verify_state_root_legacy_parent:%d:%s", block.ID, strings.TrimSpace(block.BlockHash)), livenessReasonLogCooldown) {
		log.Printf("[VERIFY-STATE-ROOT-LEGACY-PARENT] height=%d reason=%s block=%s parent_height=%d parent_source=%s parent_ledger=%s expected_root=%s block_root=%s",
			block.ID,
			strings.TrimSpace(reason),
			ShortHash(block.BlockHash),
			parentHeight,
			ctx.ParentSource,
			ShortHash(ctx.ParentLedgerHash),
			ShortHash(expectedRoot),
			ShortHash(block.StateRoot),
		)
	}
	return expectedRoot, ctx, true
}

func (n *Node) snapshotAnchorBlockForLedgerReplay(snapshot StateSnapshot) (Block, bool) {
	if n == nil || snapshot.Height == 0 {
		return Block{}, false
	}
	matches := func(block Block) bool {
		if block.ID != snapshot.Height {
			return false
		}
		if strings.TrimSpace(block.BlockHash) == "" || strings.TrimSpace(block.StateRoot) == "" {
			return false
		}
		if strings.TrimSpace(snapshot.BlockHash) != "" &&
			!strings.EqualFold(strings.TrimSpace(block.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			return false
		}
		return true
	}
	if block, ok := n.LoadBlock(int(snapshot.Height)); ok && matches(block) {
		return block, true
	}
	if n.Blockchain != nil {
		if block, ok := n.Blockchain.GetBlock(snapshot.Height); ok && matches(block) {
			return block, true
		}
	}
	return Block{}, false
}

func (n *Node) applySnapshotExecutionTipLedger(snapshot StateSnapshot, reason string) Ledger {
	if n == nil || snapshot.Height == 0 {
		return snapshot.Ledger.Clone()
	}
	executionLedger := snapshot.Ledger.Clone()
	n.cacheExecutionSnapshotLedger(snapshot.Height, executionLedger)
	n.markExecutionSnapshotReadyHeight(snapshot.Height)

	resumeLedger := executionLedger.Clone()
	source := "snapshot_execution"
	if block, ok := n.snapshotAnchorBlockForLedgerReplay(snapshot); ok {
		executionHash := HashLedger(executionLedger)
		expectedRoot := ComputeExecHashVersioned(block, executionHash, executionStateRootVersionForHeight(block.ID))
		if strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot)) {
			resumeLedger = n.startupExecutionParentLedgerAfterBlock(block, executionLedger, snapshot.Height+1)
			n.cachePostCommitLedger(snapshot.Height, resumeLedger)
			source = "snapshot_post_commit"
		} else if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_execution_root_mismatch:%d:%s", snapshot.Height, reason), livenessReasonLogCooldown) {
			log.Printf("[LEDGER-REBUILD-REJECT] reason=snapshot_execution_root_mismatch height=%d source=%s snapshot_ledger=%s expected_root=%s block_root=%s",
				snapshot.Height,
				strings.TrimSpace(reason),
				ShortHash(executionHash),
				ShortHash(expectedRoot),
				ShortHash(block.StateRoot),
			)
		}
	}
	n.setExecutionLedger(resumeLedger)
	if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_execution_tip:%d:%s", snapshot.Height, reason), livenessReasonLogCooldown) {
		log.Printf("[LEDGER-REBUILD] reason=%s height=%d source=%s execution_ledger=%s resume_ledger=%s",
			strings.TrimSpace(reason),
			snapshot.Height,
			source,
			ShortHash(HashLedger(executionLedger)),
			ShortHash(HashLedger(resumeLedger)),
		)
	}
	return resumeLedger.Clone()
}

func (n *Node) advanceConsensusToCommittedTip(reason string) bool {
	if n == nil || n.isShuttingDown() {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "committed_tip"
	}

	committedHeight := uint64(0)
	if n.Blockchain != nil {
		committedHeight = n.Blockchain.Height()
	}

	n.commitMu.Lock()
	if n.committedHeight > committedHeight {
		committedHeight = n.committedHeight
	}
	if committedHeight > n.committedHeight {
		n.committedHeight = committedHeight
	}
	if committedHeight > 0 && (n.lastCommitHeight < committedHeight || n.lastCommitAt.IsZero()) {
		n.lastCommitHeight = committedHeight
		n.lastCommitAt = time.Now()
	}
	n.commitMu.Unlock()

	if committedHeight == 0 {
		return false
	}

	if !n.restoreLedgersFromAuthoritativeExecution(committedHeight, reason) {
		if _, repaired := n.ensureCommittedTipStateSnapshot(committedHeight, reason); repaired {
			_ = n.restoreLedgersFromAuthoritativeExecution(committedHeight, reason)
		}
	}
	parentLedger := n.currentExecutionLedgerClone()
	n.markConsensusCommittedHeight(committedHeight)

	n.clearAcceptedProposal(committedHeight)
	n.clearLeaderBlock(committedHeight)
	n.requestHeartbeatBroadcast(true)

	if !ResultGossipOnly {
		if n.Consensus != nil {
			n.Consensus.FinalizeHeight(committedHeight)
		}
		return true
	}

	n.startNextRoundImmediatelyWithReason(committedHeight+1, parentLedger, "committed_tip")
	return true
}

func (n *Node) allowExecutionLedgerDriftRepair(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if n.consensusRecomputePauseActive() {
		return true, "recompute_pause"
	}
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		syncing := n.Consensus.Syncing
		paused := n.Consensus.Paused
		n.Consensus.mu.Unlock()
		if syncing {
			return true, "syncing"
		}
		if paused {
			return true, "consensus_paused"
		}
	}
	if strings.TrimSpace(n.Role) != "validator" {
		return true, "non_validator"
	}
	currentEpoch := n.currentEpoch()
	if height > 0 && height >= currentEpoch {
		return false, "validator_live_consensus"
	}
	return true, "historical_replay"
}

func (n *Node) enforceRuntimeLedgerMatchesExecution(height uint64, ctx *executionRootContext) bool {
	if n == nil || ctx == nil {
		return false
	}
	runtimeHash := strings.TrimSpace(ctx.RuntimeLedgerHash)
	executionHash := strings.TrimSpace(ctx.ExecutionLedgerHash)
	if runtimeHash == "" || executionHash == "" || strings.EqualFold(runtimeHash, executionHash) {
		return true
	}
	if allowed, mode := n.allowExecutionLedgerDriftRepair(height); !allowed {
		n.blockExecutionLedgerRepair(ctx.ParentHeight)
		log.Printf("[LEDGER-DRIFT-CRITICAL] reason=before_execution_drift height=%d mode=fail_closed repair_state=%s parent_height=%d runtime_ledger=%s execution_ledger=%s",
			height,
			mode,
			ctx.ParentHeight,
			ShortHash(runtimeHash),
			ShortHash(executionHash),
		)
		return false
	}
	if ctx.ParentHeight > 0 {
		if !n.restoreLedgersFromAuthoritativeExecution(ctx.ParentHeight, "before_execution_drift") {
			return false
		}
	} else if !n.resetRuntimeLedgerToExecution("before_execution_drift", height) {
		return false
	}
	ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
	ctx.ExecutionLedgerHash = HashLedger(n.currentExecutionLedgerClone())
	return strings.EqualFold(strings.TrimSpace(ctx.RuntimeLedgerHash), strings.TrimSpace(ctx.ExecutionLedgerHash))
}

func (n *Node) recordLocalExecutionMismatch(height uint64, blockHash string) int {
	if n == nil {
		return 0
	}
	n.localExecMismatchMu.Lock()
	defer n.localExecMismatchMu.Unlock()
	blockHash = strings.TrimSpace(blockHash)
	if n.localExecMismatchHeight != height || !strings.EqualFold(strings.TrimSpace(n.localExecMismatchBlockHash), blockHash) {
		n.localExecMismatchCount = 0
	}
	n.localExecMismatchHeight = height
	n.localExecMismatchBlockHash = blockHash
	n.localExecMismatchCount++
	return n.localExecMismatchCount
}

func (n *Node) clearLocalExecutionMismatch() {
	if n == nil {
		return
	}
	n.localExecMismatchMu.Lock()
	n.localExecMismatchCount = 0
	n.localExecMismatchHeight = 0
	n.localExecMismatchBlockHash = ""
	n.localExecMismatchMu.Unlock()
}

func (n *Node) applyLocalExecutionSafetyLock(block Block, ctx executionRootContext, expectedRoot string) {
	if n == nil {
		return
	}
	strikes := n.recordLocalExecutionMismatch(block.ID, block.BlockHash)
	if strikes < 3 {
		return
	}
	runtimeReset := n.resetRuntimeLedgerToExecution("preflight_state_root_mismatch", block.ID)
	n.clearLeaderBlock(block.ID)
	log.Printf("[EXEC-SAFETY-LOCK] height=%d round=%d block=%s strikes=%d runtime_reset=%t parent_ledger=%s runtime_ledger=%s execution_ledger=%s expected_root=%s block_root=%s",
		block.ID,
		block.Round,
		ShortHash(block.BlockHash),
		strikes,
		runtimeReset,
		ShortHash(ctx.ParentLedgerHash),
		ShortHash(ctx.RuntimeLedgerHash),
		ShortHash(ctx.ExecutionLedgerHash),
		ShortHash(expectedRoot),
		ShortHash(block.StateRoot),
	)
	n.clearLocalExecutionMismatch()
}

func (n *Node) deterministicExecutionParentForEpoch(epoch uint64) (Ledger, executionParentLedgerContext, bool) {
	if n == nil || n.Blockchain == nil {
		return Ledger{}, executionParentLedgerContext{}, false
	}
	last := n.Blockchain.LastBlock()
	nextID := last.ID + 1
	if epoch == 0 || epoch != nextID {
		epoch = nextID
	}
	return n.executionParentLedgerForBlock(Block{
		ID:       epoch,
		PrevHash: last.BlockHash,
	})
}

func (n *Node) executionLedgerForBlock(block Block) (Ledger, executionRootContext, bool) {
	ctx := executionRootContext{
		RootVersion: executionStateRootVersionForHeight(block.ID),
	}
	parentLedger, parentCtx, ok := n.executionParentLedgerForBlock(block)
	ctx.ParentHeight = parentCtx.ParentHeight
	ctx.ParentSource = parentCtx.ParentSource
	ctx.ParentHash = parentCtx.ParentHash
	ctx.RuntimeLedgerHash = parentCtx.RuntimeLedgerHash
	ctx.ExecutionLedgerHash = parentCtx.ExecutionLedgerHash
	if !ok {
		if n != nil {
			log.Printf("[EXEC-PARENT] unavailable height=%d prev=%s parent_height=%d parent_hash=%s source=%s runtime_ledger=%s execution_ledger=%s root_version=%s",
				block.ID,
				ShortHash(block.PrevHash),
				ctx.ParentHeight,
				ShortHash(ctx.ParentHash),
				ctx.ParentSource,
				ShortHash(ctx.RuntimeLedgerHash),
				ShortHash(ctx.ExecutionLedgerHash),
				ctx.RootVersion,
			)
		}
		return Ledger{}, ctx, false
	}

	ctx.ParentLedgerHash = HashLedger(parentLedger)
	n.observeConsensusExecutionDrift(block.ID, "before_execution", ctx.ParentLedgerHash, ctx.RuntimeLedgerHash, ctx.ExecutionLedgerHash)
	if err := n.deterministicEnsureExecutionLedgerAligned(block.ID, &ctx); err != nil {
		log.Printf("[EXEC-CHECK] height=%d reason=runtime_execution_mismatch_unresolved parent_ledger=%s runtime_ledger=%s execution_ledger=%s",
			block.ID,
			ShortHash(ctx.ParentLedgerHash),
			ShortHash(ctx.RuntimeLedgerHash),
			ShortHash(ctx.ExecutionLedgerHash),
		)
		return Ledger{}, ctx, false
	}
	sandbox := NewExecutionSandbox(parentLedger)
	newLedger, err := sandbox.ApplyBlock(n, block)
	if err != nil {
		log.Printf("[EXEC-APPLY] height=%d round=%d block=%s parent_source=%s parent_height=%d parent_ledger=%s reason=%s",
			block.ID,
			block.Round,
			ShortHash(block.BlockHash),
			ctx.ParentSource,
			ctx.ParentHeight,
			ShortHash(ctx.ParentLedgerHash),
			err.Error(),
		)
		return Ledger{}, ctx, false
	}
	ledgerHash := HashLedger(newLedger)
	stateMerkleRoot := LedgerStateMerkleRoot(newLedger)
	if !ExecutionDeterminismGuardEnabled {
		return newLedger, ctx, true
	}
	replaySandbox := NewExecutionSandbox(parentLedger)
	replayLedger, err := replaySandbox.ApplyBlock(n, block)
	if err != nil {
		log.Printf("[EXEC-APPLY] height=%d round=%d block=%s parent_source=%s parent_height=%d parent_ledger=%s reason=replay_%s",
			block.ID,
			block.Round,
			ShortHash(block.BlockHash),
			ctx.ParentSource,
			ctx.ParentHeight,
			ShortHash(ctx.ParentLedgerHash),
			err.Error(),
		)
		return Ledger{}, ctx, false
	}
	replayLedgerHash := HashLedger(replayLedger)
	replayStateMerkleRoot := LedgerStateMerkleRoot(replayLedger)
	replayExecHash := ComputeExecHashVersioned(block, replayLedgerHash, ctx.RootVersion)
	execHash := ComputeExecHashVersioned(block, ledgerHash, ctx.RootVersion)
	if !strings.EqualFold(ledgerHash, replayLedgerHash) ||
		!strings.EqualFold(stateMerkleRoot, replayStateMerkleRoot) ||
		!strings.EqualFold(execHash, replayExecHash) {
		log.Printf("[EXEC-DETERMINISM] mismatch height=%d round=%d proposer=%s parent_source=%s parent_height=%d parent_hash=%s parent_ledger=%s runtime_ledger=%s execution_ledger=%s root_version=%s ledger_hash_a=%s ledger_hash_b=%s state_merkle_a=%s state_merkle_b=%s exec_a=%s exec_b=%s",
			block.ID,
			block.Round,
			ShortID(block.Proposer),
			ctx.ParentSource,
			ctx.ParentHeight,
			ShortHash(ctx.ParentHash),
			ShortHash(ctx.ParentLedgerHash),
			ShortHash(ctx.RuntimeLedgerHash),
			ShortHash(ctx.ExecutionLedgerHash),
			ctx.RootVersion,
			ShortHash(ledgerHash),
			ShortHash(replayLedgerHash),
			ShortHash(stateMerkleRoot),
			ShortHash(replayStateMerkleRoot),
			ShortHash(execHash),
			ShortHash(replayExecHash),
		)
		return Ledger{}, ctx, false
	}
	return newLedger, ctx, true
}

func computeExecHashV1(block Block, ledgerHash string) string {
	if ledgerHash == "" || block.ID == 0 {
		return ""
	}
	epoch := block.BlockTime.Epoch
	if epoch == 0 {
		epoch = block.ID
	}
	return HashStrings([]string{
		fmt.Sprintf("%d", block.ID),
		block.Proposer,
		block.MempoolRoot,
		block.PrevHash,
		fmt.Sprintf("%d", epoch),
		ledgerHash,
	})
}

func ComputeExecHashVersioned(block Block, ledgerHash string, version string) string {
	switch strings.TrimSpace(version) {
	case "", executionStateRootVersionV1:
		return computeExecHashV1(block, ledgerHash)
	default:
		return ""
	}
}

func (n *Node) executionParentLedgerForBlock(block Block) (Ledger, executionParentLedgerContext, bool) {
	ctx := executionParentLedgerContext{}
	if n == nil || block.ID == 0 {
		return Ledger{}, ctx, false
	}

	parentHeight := block.ID - 1
	liveExecutionLedger := n.currentExecutionLedgerClone()
	ctx.ParentHeight = parentHeight
	ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
	ctx.ExecutionLedgerHash = HashLedger(liveExecutionLedger)

	expectedPrevHash := strings.TrimSpace(block.PrevHash)
	if parentBlock, ok := n.LoadBlock(int(parentHeight)); ok {
		ctx.ParentHash = strings.TrimSpace(parentBlock.BlockHash)
	}
	if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
		ctx.ParentSource = "parent_hash_mismatch"
		return Ledger{}, ctx, false
	}

	if parentHeight == 0 {
		if n.Blockchain != nil && n.Blockchain.Height() == 0 {
			last := n.Blockchain.LastBlock()
			if ctx.ParentHash == "" {
				ctx.ParentHash = strings.TrimSpace(last.BlockHash)
			}
			if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
				ctx.ParentSource = "parent_hash_mismatch"
				return Ledger{}, ctx, false
			}
			ctx.ParentSource = "runtime_genesis"
			return n.currentExecutionLedgerClone(), ctx, true
		}
		if snapshot, err := n.GetSnapshot(0); err == nil && snapshot != nil {
			if ctx.ParentHash == "" {
				ctx.ParentHash = strings.TrimSpace(snapshot.BlockHash)
			}
			if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
				ctx.ParentSource = "parent_hash_mismatch"
				return Ledger{}, ctx, false
			}
			ctx.ParentSource = "snapshot_genesis"
			return snapshot.Ledger.Clone(), ctx, true
		}
		ctx.ParentSource = "genesis_state_unavailable"
		return Ledger{}, ctx, false
	}

	if n.Blockchain != nil {
		liveTip := n.Blockchain.Height()
		n.commitMu.Lock()
		if n.committedHeight > liveTip {
			liveTip = n.committedHeight
		}
		n.commitMu.Unlock()
		if parentHeight == liveTip && ledgerHasInitializedBacking(liveExecutionLedger) {
			ctx.ParentSource = "live_execution_tip"
			return liveExecutionLedger, ctx, true
		}
	}

	if restored, ok := n.committedTipLedgerFromExecutionSnapshot(parentHeight); ok && ledgerHasInitializedBacking(restored) {
		ctx.ParentSource = "post_commit_execution_snapshot"
		return restored, ctx, true
	}
	if cachedLedger, ok := n.cachedExecutionSnapshotLedger(parentHeight); ok {
		if parentBlock, found := n.LoadBlock(int(parentHeight)); !found || strings.TrimSpace(parentBlock.StateRoot) == "" {
			ctx.ParentSource = "execution_cache_legacy"
			return cachedLedger, ctx, true
		}
	}
	if snapshot, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(parentHeight); ok && snapshot != nil {
		if ctx.ParentHash == "" {
			ctx.ParentHash = strings.TrimSpace(snapshot.BlockHash)
		}
		if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
			ctx.ParentSource = "parent_hash_mismatch"
			return Ledger{}, ctx, false
		}
		if parentBlock, found := n.LoadBlock(int(parentHeight)); !found || strings.TrimSpace(parentBlock.StateRoot) == "" {
			ctx.ParentSource = "trusted_snapshot_legacy"
			return snapshot.Ledger.Clone(), ctx, true
		}
	}

	ctx.ParentSource = "parent_state_unavailable"
	return Ledger{}, ctx, false
}

func (n *Node) executionStateRootForBlock(block Block) (string, executionRootContext, bool) {
	newLedger, ctx, ok := n.executionLedgerForBlock(block)
	if !ok {
		return "", ctx, false
	}
	return ComputeExecHashVersioned(block, HashLedger(newLedger), ctx.RootVersion), ctx, true
}

func (n *Node) ExecuteBlockAndGetStateRoot(block Block) string {
	execHash, _, ok := n.executionStateRootForBlock(block)
	if !ok {
		return ""
	}
	return execHash
}

func (n *Node) verifyExecutionStateRootWithAuthoritativeRepair(block Block, reason string) (string, executionRootContext, bool) {
	expectedRoot, execCtx, ok := n.executionStateRootForBlock(block)
	if ok && expectedRoot != "" && strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(expectedRoot)) {
		return expectedRoot, execCtx, true
	}
	if n == nil || block.ID <= 1 {
		return expectedRoot, execCtx, false
	}

	parentHeight := block.ID - 1
	if !n.restoreLedgersFromAuthoritativeExecution(parentHeight, reason) {
		return expectedRoot, execCtx, false
	}

	expectedRoot, execCtx, ok = n.executionStateRootForBlock(block)
	matched := ok && expectedRoot != "" && strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(expectedRoot))
	if matched && n.shouldLogLivenessReason(fmt.Sprintf("verify_state_root_repair:%d:%s", block.ID, strings.TrimSpace(reason)), livenessReasonLogCooldown) {
		log.Printf("[VERIFY-STATE-ROOT-REPAIR] height=%d reason=%s block=%s parent_height=%d parent_source=%s parent_ledger=%s expected_root=%s block_root=%s",
			block.ID,
			strings.TrimSpace(reason),
			ShortHash(block.BlockHash),
			execCtx.ParentHeight,
			execCtx.ParentSource,
			ShortHash(execCtx.ParentLedgerHash),
			ShortHash(expectedRoot),
			ShortHash(block.StateRoot),
		)
	}
	if !matched {
		if legacyRoot, legacyCtx, legacyOK := n.legacyExecutionSnapshotStateRootForBlock(block, reason); legacyOK {
			return legacyRoot, legacyCtx, true
		}
	}
	return expectedRoot, execCtx, matched
}

func (n *Node) executionTraceContext() (runtimeLedgerHash string, executionLedgerHash string, tipHeight uint64, tipHash string) {
	if n == nil {
		return "", "", 0, ""
	}
	if n.Blockchain != nil {
		last := n.Blockchain.LastBlock()
		tipHeight = last.ID
		tipHash = strings.TrimSpace(last.BlockHash)
	}
	executionLedgerHash = strings.TrimSpace(HashLedger(n.currentExecutionLedgerClone()))
	runtimeLedgerHash = strings.TrimSpace(HashLedger(n.Ledger.Clone()))
	return runtimeLedgerHash, executionLedgerHash, tipHeight, tipHash
}

// preflightOwnLeaderBlock enforces strict local validity before a node
// can publish a leader block. If this fails, the block is never broadcast.
func (n *Node) preflightOwnLeaderBlock(block Block) error {
	if n == nil {
		return errors.New("nil node")
	}
	if block.ID == 0 {
		return errors.New("zero height block")
	}
	last := n.Blockchain.LastBlock()
	if block.ID != last.ID+1 {
		return fmt.Errorf("stale/future proposal: got=%d want=%d", block.ID, last.ID+1)
	}
	if block.PrevHash != last.BlockHash {
		return errors.New("prev hash mismatch")
	}
	if ConsensusProposeRequiresSyncReady {
		if ready, reason := n.syncReadyForConsensus(block.ID); !ready {
			return fmt.Errorf("proposal gated: %s", reason)
		}
	}
	if ConsensusPostBlockSafeModeEnabled {
		if active, _, _ := n.postBlockSafeModeState(block.ID); active {
			return errors.New("proposal gated: safe_mode_active")
		}
	}

	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	expectedLeader := n.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	if expectedLeader != "" && block.Proposer != expectedLeader {
		if n.syncExecutionResultQuorumFallback(block, validators) {
			if DebugConsensus || DebugSync {
				fmt.Printf("[SYNC-LEADER-FALLBACK] height=%d source=block_quorum_metadata validators=%d hash=%s\n",
					block.ID, len(validators), ShortHash(block.ValidatorSetHash))
			}
		} else {
			return fmt.Errorf("unexpected proposer: got=%s want=%s", block.Proposer, expectedLeader)
		}
	}
	expectedHash, source := n.expectedValidatorSetHashWithSource(block.ID)
	hashMode := validatorSetHashModeForHeight(block.ID)
	if validatorSetSourceIsChainAuthoritative(source) && strings.TrimSpace(expectedHash) == "" {
		if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-REJECT] height=%d reason=missing_parent_commitment source=%s mode=%s got=%s\n",
				block.ID, source, hashMode, ShortHash(block.ValidatorSetHash))
		}
		return errors.New("validator_set_hash_expected_from_parent_missing")
	}
	if strings.TrimSpace(expectedHash) != "" {
		if block.ValidatorSetHash == "" || !strings.EqualFold(strings.TrimSpace(block.ValidatorSetHash), strings.TrimSpace(expectedHash)) {
			if validatorSetSourceIsChainAuthoritative(source) {
				if DebugConsensus {
					fmt.Printf("[SET-COMMITMENT-REJECT] height=%d source=%s mode=%s expected=%s got=%s\n",
						block.ID, source, hashMode, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash))
				}
				return fmt.Errorf("validator set hash mismatch: got=%s want=%s", ShortHash(block.ValidatorSetHash), ShortHash(expectedHash))
			}
			if DebugConsensus {
				fmt.Printf("[SET-COMMITMENT-SOURCE] weak-source active-set mismatch accepted height=%d source=%s mode=%s got=%s want=%s\n",
					block.ID, source, hashMode, ShortHash(block.ValidatorSetHash), ShortHash(expectedHash))
			}
		} else if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-APPLY] height=%d source=%s mode=%s hash=%s\n",
				block.ID, source, hashMode, ShortHash(block.ValidatorSetHash))
		}
	}
	if err := n.validateBlockValidatorSetHashHeaderCommitment(block); err != nil {
		return err
	}
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		return err
	}
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		return err
	}
	if err := n.validateBlockValidatorSetRootCommitment(block); err != nil {
		return err
	}
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		return err
	}
	if err := verifyBlockQuorumMetadata(block, len(validators)); err != nil {
		return err
	}

	if err := VerifyMempoolRoot(block); err != nil {
		return err
	}
	if err := VerifyReceiptRoot(block); err != nil {
		return err
	}
	expectedRoot, execCtx, rootOK := n.verifyExecutionStateRootWithAuthoritativeRepair(block, "preflight_state_root_mismatch")
	if !rootOK {
		clearedProposal := n.clearAcceptedProposalIfBlock(block.ID, block, "state_root_mismatch")
		currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
		log.Printf("[EXEC-PREFLIGHT] height=%d round=%d proposer=%s prev=%s tx_count=%d block=%s block_root=%s expected_root=%s parent_source=%s parent_height=%d parent_hash=%s parent_ledger=%s runtime_ledger=%s execution_ledger=%s root_version=%s current_runtime=%s current_execution=%s current_tip=%d/%s proposal=%s cleared_proposal=%t reason=state_root_mismatch",
			block.ID,
			block.Round,
			ShortID(block.Proposer),
			ShortHash(block.PrevHash),
			len(block.Transactions),
			ShortHash(block.BlockHash),
			ShortHash(block.StateRoot),
			ShortHash(expectedRoot),
			execCtx.ParentSource,
			execCtx.ParentHeight,
			ShortHash(execCtx.ParentHash),
			ShortHash(execCtx.ParentLedgerHash),
			ShortHash(execCtx.RuntimeLedgerHash),
			ShortHash(execCtx.ExecutionLedgerHash),
			execCtx.RootVersion,
			ShortHash(currentRuntimeLedgerHash),
			ShortHash(currentExecutionLedgerHash),
			tipHeight,
			ShortHash(tipHash),
			proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot),
			clearedProposal,
		)
		n.applyLocalExecutionSafetyLock(block, execCtx, expectedRoot)
		return errors.New("state root mismatch")
	}
	n.clearLocalExecutionMismatch()
	if block.BlockHash == "" || HashBlock(block) != block.BlockHash {
		return errors.New("block hash mismatch")
	}
	if len(block.Signature) == 0 || !VerifyBlockSignature(block) {
		return errors.New("invalid proposer signature")
	}
	return nil
}

// enterProposePhase runs the leader's propose step for a locally-built block:
// validate it, install it as the active candidate, then publish it.
func (n *Node) enterProposePhase(block Block, voteTrigger string) bool {
	if n == nil || block.ID == 0 || n.isShuttingDown() {
		return false
	}
	if err := n.preflightOwnLeaderBlock(block); err != nil {
		if DebugConsensus {
			fmt.Printf("Skipping leader proposal @ epoch %d: %v\n", block.ID, err)
		}
		return false
	}
	if !n.storeLeaderBlock(block) {
		return false
	}
	n.processQueuedExecutionVotesForProposal(block)
	if n.executionResultAlreadyCommitted(block.ID) {
		return true
	}
	if n.tryFinalizeProposalIfQuorum(block, "proposal_existing_quorum") {
		return true
	}
	if n.isShuttingDown() {
		return false
	}
	n.setLogicalTick(block.ID, TickExec)
	n.broadcastLeaderBlockUnchecked(block)
	if n.isShuttingDown() {
		return false
	}
	n.maybeBroadcastCurrentLeaderExecutionVote(voteTrigger)
	return true
}

func proposalDeadlineGuardDuration() time.Duration {
	if ConsensusProposalDeadlineGuard <= 0 {
		return 200 * time.Millisecond
	}
	return ConsensusProposalDeadlineGuard
}

func (n *Node) consensusRoundSnapshot(height uint64) (uint32, time.Time, bool) {
	if n == nil || n.Consensus == nil || height == 0 {
		return 0, time.Time{}, false
	}
	n.Consensus.mu.Lock()
	defer n.Consensus.mu.Unlock()
	if n.Consensus.Height != height {
		return 0, time.Time{}, false
	}
	return n.Consensus.Round, n.Consensus.RoundStart, true
}

func (n *Node) realignConsensusHeightToEpoch(epoch uint64, reason string) bool {
	if n == nil || n.Consensus == nil || epoch == 0 || n.isShuttingDown() {
		return false
	}
	n.Consensus.mu.Lock()
	current := n.Consensus.Height
	syncing := n.Consensus.Syncing || n.Consensus.syncInFlight
	paused := n.Consensus.Paused
	n.Consensus.mu.Unlock()
	if syncing || paused || current == epoch {
		return false
	}
	n.clearImmediateRoundStart(epoch)
	n.hardResetConsensus(epoch)
	if DebugConsensus {
		fmt.Printf("[CONSENSUS-REALIGN] reason=%s from=%d to=%d\n", strings.TrimSpace(reason), current, epoch)
	}
	return true
}

func (n *Node) startConsensusRound(height uint64, round uint32) bool {
	if n == nil || n.Consensus == nil || height == 0 || n.isShuttingDown() {
		return false
	}
	round = clampProposerRound(round)
	now := time.Now()
	n.Consensus.mu.Lock()
	defer n.Consensus.mu.Unlock()
	if n.Consensus.Height != height {
		return false
	}
	if round < n.Consensus.Round {
		return false
	}
	if round > n.Consensus.Round || n.Consensus.RoundStart.IsZero() {
		n.Consensus.Round = round
		n.Consensus.RoundStart = now
	}
	n.Consensus.Phase = PhasePropose
	n.Consensus.Committed = false
	return true
}

func (n *Node) isRoundLeader(height uint64, round uint32) bool {
	if n == nil || height == 0 {
		return false
	}
	validators := n.freezeValidatorSetForHeight(height, n.GetConsensusValidators(int(height)))
	if len(validators) == 0 {
		return false
	}
	leaderID := normalizeValidatorID(n.consensusLeaderForHeightRound(height, round, validators))
	return leaderID != "" && leaderID == normalizeValidatorID(n.ID)
}

func (n *Node) markLeaderProposalSent(height uint64, round uint32) {
	if n == nil || height == 0 {
		return
	}
	nowNs := time.Now().UnixNano()
	n.leaderMu.Lock()
	n.lastLeaderEpoch = height
	n.lastLeaderRound = round
	n.lastLeaderSlot = nowNs
	n.leaderMu.Unlock()
}

func (n *Node) leaderProposalRetryState(height uint64, round uint32, retry time.Duration) (sameEpoch bool, sameRound bool, throttle bool) {
	if n == nil || height == 0 {
		return false, false, false
	}
	n.leaderMu.Lock()
	defer n.leaderMu.Unlock()
	if n.lastLeaderEpoch != height {
		return false, false, false
	}
	sameEpoch = true
	sameRound = n.lastLeaderRound == round
	if !sameRound {
		return sameEpoch, sameRound, false
	}
	if retry <= 0 || n.lastLeaderSlot <= 0 {
		return sameEpoch, sameRound, false
	}
	lastSent := time.Unix(0, n.lastLeaderSlot)
	if time.Since(lastSent) < retry {
		return sameEpoch, sameRound, true
	}
	return sameEpoch, sameRound, false
}

func (n *Node) reuseLeaderProposalForRound(height uint64, round uint32, trigger string) bool {
	if n == nil || height == 0 {
		return false
	}
	existing, ok := n.getLeaderBlock(height)
	if !ok || existing.Round != round || existing.BlockHash == "" {
		return false
	}
	if normalizeValidatorID(existing.Proposer) != normalizeValidatorID(n.ID) {
		return false
	}
	n.setLogicalTick(existing.ID, TickExec)
	n.broadcastLeaderBlockUnchecked(existing)
	n.maybeBroadcastCurrentLeaderExecutionVote(strings.TrimSpace(trigger) + "_rebroadcast")
	n.markLeaderProposalSent(height, round)
	return true
}

func (n *Node) forceRoundProposal(height uint64, round uint32, parentLedger Ledger, trigger string) bool {
	if n == nil || height == 0 || n.isShuttingDown() || !ResultGossipOnly {
		return false
	}
	if n.currentEpoch() != height {
		return false
	}
	if !n.startConsensusRound(height, round) {
		return false
	}
	if ready, _ := n.validatorParticipationGateStatus(height); !ready {
		return false
	}
	if ConsensusProposeRequiresSyncReady {
		if ready, _ := n.syncReadyForConsensus(height); !ready {
			return false
		}
	}
	if ConsensusPostBlockSafeModeEnabled && !n.tryExitPostBlockSafeMode(height) {
		return false
	}
	if blocked, _, _ := n.consensusSyncGateForHeight(height); blocked {
		return false
	}
	if !n.isRoundLeader(height, round) {
		return false
	}
	if lockedBlock, _, locked, _ := n.acceptedProposalVoteLockForRound(height, round); locked {
		n.maybeBroadcastExecutionVoteForBlock(lockedBlock, strings.TrimSpace(trigger)+"_accepted_vote_lock")
		return false
	}
	if existing, ok := n.getLeaderBlock(height); ok {
		if existing.Round == round && n.reuseLeaderProposalForRound(height, round, trigger) {
			return true
		}
		if existing.Round < round {
			n.clearLeaderBlock(height)
		}
	}
	if ledgerHasInitializedBacking(parentLedger) {
		n.setExecutionLedger(parentLedger)
	}
	n.setProposedRound(height, round)
	block := n.BuildLeaderBlock(height)
	if block.StateRoot == "" || block.Round != round || n.isShuttingDown() {
		return false
	}
	if ledgerHasInitializedBacking(parentLedger) {
		n.setExecutionLedger(parentLedger)
	}
	if !n.enterProposePhase(block, trigger) {
		return false
	}
	n.markLeaderProposalSent(height, round)
	return true
}

func (n *Node) forceRoundProposalIfLate(height uint64, round uint32, parentLedger Ledger, trigger string) bool {
	if n == nil || height == 0 || n.isShuttingDown() {
		return false
	}
	round = clampProposerRound(round)
	activeRound, roundStart, ok := n.consensusRoundSnapshot(height)
	if !ok || activeRound != round || roundStart.IsZero() {
		return false
	}
	if time.Since(roundStart) < proposalDeadlineGuardDuration() {
		return false
	}
	if existing, ok := n.getLeaderBlock(height); ok && existing.Round == round && existing.BlockHash != "" {
		return false
	}
	return n.forceRoundProposal(height, round, parentLedger, trigger)
}

func (n *Node) forceRoundZeroProposalIfLate(height uint64, parentLedger Ledger, trigger string) bool {
	return n.forceRoundProposalIfLate(height, 0, parentLedger, trigger)
}

func (n *Node) scheduleProposalDeadlineGuard(height uint64, round uint32, parentLedger Ledger) {
	if n == nil || height == 0 || n.isShuttingDown() {
		return
	}
	round = clampProposerRound(round)
	deadline := proposalDeadlineGuardDuration()
	if deadline <= 0 {
		return
	}
	queuedLedger := parentLedger.Clone()
	ctx := n.RootContext()
	n.SafeGo(fmt.Sprintf("proposal_deadline_guard_%d_%d", height, round), func() {
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		_ = n.scheduleConsensusPriorityTask(func() {
			_ = n.forceRoundProposalIfLate(height, round, queuedLedger, "round_start_deadline")
		})
	})
}

func (n *Node) scheduleRoundZeroProposalDeadlineGuard(height uint64, parentLedger Ledger) {
	n.scheduleProposalDeadlineGuard(height, 0, parentLedger)
}

func (n *Node) startNextRoundImmediately(nextHeight uint64, parentLedger Ledger) {
	n.startNextRoundImmediatelyWithReason(nextHeight, parentLedger, "direct")
}

func (n *Node) startNextRoundImmediatelyWithReason(nextHeight uint64, parentLedger Ledger, reason string) {
	if n == nil || nextHeight == 0 || !ResultGossipOnly {
		return
	}
	if !n.tryScheduleImmediateRoundStart(nextHeight) {
		return
	}
	queuedLedger := parentLedger.Clone()
	if !n.scheduleConsensusPriorityTask(func() {
		started := n.startNextRoundImmediatelyNow(nextHeight, queuedLedger, reason)
		n.finishImmediateRoundStart(nextHeight, started)
	}) {
		n.clearImmediateRoundStart(nextHeight)
		return
	}
}

func (n *Node) startNextRoundImmediatelyNow(nextHeight uint64, parentLedger Ledger, reason string) bool {
	if n == nil || nextHeight == 0 || !ResultGossipOnly || n.isShuttingDown() {
		return false
	}
	if n.immediateRoundStartAlreadyHandled(nextHeight) {
		return false
	}
	if n.currentEpoch() != nextHeight {
		return false
	}
	if ledgerHasInitializedBacking(parentLedger) {
		n.setExecutionLedger(parentLedger)
	}

	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		if !n.Consensus.Syncing {
			n.Consensus.Paused = false
		}
		n.Consensus.mu.Unlock()
	}

	n.hardResetConsensus(nextHeight)
	if !n.startConsensusRound(nextHeight, 0) {
		return false
	}
	n.setLogicalTick(nextHeight, TickExec)
	n.scheduleProposalDeadlineGuard(nextHeight, 0, parentLedger)
	if reason == "" {
		reason = "immediate_round_start"
	}
	_ = n.forceRoundProposal(nextHeight, 0, parentLedger, reason)
	return true
}

func ComputeExecHash(block Block, ledgerHash string) string {
	return ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
}

func (n *Node) enterProposerRoundRecoveryMode(height uint64, round uint32, maxRounds uint32) {
	if n == nil || height == 0 {
		return
	}
	if maxRounds == 0 {
		maxRounds = proposerRoundRecoveryCap()
	}
	key := fmt.Sprintf("round_recovery:%d:%d", height, maxRounds)
	if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
		log.Printf("[ROUND-RECOVERY] height=%d round=%d max_round=%d action=recompute_sync",
			height,
			round,
			maxRounds,
		)
	}
	n.requestConsensusRecomputePause(height, "round_cap_exceeded")
	n.maybeSyncToBestObservedHeight("round_cap_exceeded")
}

func (n *Node) pauseConsensusForLivenessShortfall(height uint64, required int, snap CommitteeLivenessSnapshot) {
	if n == nil || height == 0 || required <= 0 {
		return
	}
	if snap.Live >= required {
		return
	}
	key := fmt.Sprintf("liveness_pause:%d:%d", height, required)
	if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
		log.Printf("[LIVENESS-PAUSE] height=%d live=%d required=%d action=recompute_pause",
			height,
			snap.Live,
			required,
		)
	}
	n.requestConsensusRecomputePause(height, "live_quorum_unavailable")
}

func (n *Node) ActivateConsensus(ctx context.Context) error {
	// Run the consensus loop for validator/full nodes so observer mode still
	// verifies/executes and tracks network progress. Propose/vote remains gated
	// by canParticipateInConsensusNow inside the loop.
	if n == nil {
		return errors.New("node unavailable")
	}
	if n.Role != "validator" && normalizeNodeRole(n.Role) != "full" {
		return nil
	}
	if !ResultGossipOnly {
		return errors.New("legacy consensus removed")
	}
	if !consensusStarted.CompareAndSwap(false, true) {
		return errors.New("consensus already running")
	}
	go func() {
		defer consensusStarted.Store(false)

		realTick := GlobalConfig.RealTick
		if realTick <= 0 {
			realTick = 2 * time.Second
		}
		minBlockInterval := ConsensusMinBlockInterval
		if minBlockInterval <= 0 {
			minBlockInterval = 4 * time.Second
		}
		ticker := time.NewTicker(realTick)
		defer ticker.Stop()

		var lastEpoch uint64
		var lastEpochAt time.Time
		var lastFallbackEpoch uint64
		var lastRound uint32
		var lastParticipationGateEpoch uint64
		var lastParticipationGateReason string
		var lastProposalGateEpoch uint64
		var lastProposalGateReason string
		var lastStartupGateEpoch uint64
		var lastStartupGateReason string
		var lastRoundGateEpoch uint64
		var lastRoundGateAt time.Time
		holdRoundClock := func(epoch uint64) {
			if epoch == 0 {
				return
			}
			lastRoundGateEpoch = epoch
			lastRoundGateAt = time.Now()
		}

		// Round 0 must not wait for the first consensus ticker pulse. Kick the
		// current epoch onto the consensus lane immediately on loop startup, then
		// let the regular ticker handle retries/failover afterward.
		n.startNextRoundImmediatelyWithReason(n.currentEpoch(), n.currentExecutionLedgerClone(), "activate_consensus")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n.consensusRecomputePauseActive() {
					holdRoundClock(n.currentEpoch())
					continue
				}
				epoch := n.currentEpoch()
				if ready, reason := n.validatorParticipationGateStatus(epoch); !ready {
					if DebugConsensus && (lastParticipationGateEpoch != epoch || lastParticipationGateReason != reason) {
						fmt.Printf("[PARTICIPATION-GATE] validator=%s height=%d reason=%s\n", ShortID(n.ID), epoch, reason)
						lastParticipationGateEpoch = epoch
						lastParticipationGateReason = reason
					}
					holdRoundClock(epoch)
					continue
				}
				if blocked, _, _ := n.consensusSyncGateForHeight(epoch); blocked {
					holdRoundClock(epoch)
					continue
				}
				if ok, reason := n.startupValidatorSetSelfCheck(); !ok {
					if DebugConsensus && (lastStartupGateEpoch != epoch || lastStartupGateReason != reason) {
						fmt.Printf("[STARTUP-GATE] validator=%s height=%d reason=%s\n", ShortID(n.ID), epoch, reason)
						lastStartupGateEpoch = epoch
						lastStartupGateReason = reason
					}
					holdRoundClock(epoch)
					continue
				}
				if n.realignConsensusHeightToEpoch(epoch, "consensus_tick") {
					lastEpoch = 0
					lastEpochAt = time.Time{}
					lastFallbackEpoch = 0
					lastRound = 0
					lastRoundGateEpoch = 0
					lastRoundGateAt = time.Time{}
				}
				if epoch != lastEpoch {
					lastEpoch = epoch
					lastEpochAt = time.Now()
					lastFallbackEpoch = 0
					lastRound = 0
					lastParticipationGateEpoch = 0
					lastParticipationGateReason = ""
					lastProposalGateEpoch = 0
					lastProposalGateReason = ""
					lastStartupGateEpoch = 0
					lastStartupGateReason = ""
					lastRoundGateEpoch = 0
					lastRoundGateAt = time.Time{}
					epochStartHeight := epoch
					epochStartLedger := n.currentExecutionLedgerClone()
					_ = n.scheduleConsensusPriorityTask(func() {
						n.startConsensusRound(epochStartHeight, 0)
						_ = n.forceRoundProposal(epochStartHeight, 0, epochStartLedger, "consensus_epoch_start")
					})
					n.scheduleProposalDeadlineGuard(epochStartHeight, 0, epochStartLedger)
				}
				if !lastEpochAt.IsZero() && time.Since(lastEpochAt) < minBlockInterval {
					continue
				}
				if ConsensusPostBlockSafeModeEnabled {
					if !n.tryExitPostBlockSafeMode(epoch) {
						continue
					}
				}
				validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
				total := len(validators)
				required := n.executionQuorumRequiredForEpoch(epoch)
				if required == 0 {
					required = execQuorumRequired(total)
				}
				if total == 0 || required == 0 {
					continue
				}
				observedRound, observedAt := n.proposedRoundAnchorForHeight(epoch)
				roundEpochStartedAt := lastEpochAt
				if lastRoundGateEpoch == epoch {
					roundEpochStartedAt, observedAt = consensusRoundAnchorsWithGateHold(roundEpochStartedAt, observedAt, lastRoundGateAt)
				}
				rawRound := computeConsensusRound(time.Now(), roundEpochStartedAt, observedRound, observedAt, minBlockInterval, ProposerRoundTimeout, realTick)
				if maxRounds := proposerRoundRecoveryCap(); maxRounds > 0 && rawRound > maxRounds {
					n.enterProposerRoundRecoveryMode(epoch, rawRound, maxRounds)
					continue
				}
				round := clampProposerRound(rawRound)
				if ConsensusProposeRequiresSyncReady {
					if ready, reason := n.syncReadyForConsensus(epoch); !ready {
						if DebugConsensus && (lastProposalGateEpoch != epoch || lastProposalGateReason != reason) {
							fmt.Printf("[PROPOSAL-GATE] skipped lagging validator=%s height=%d reason=%s\n", ShortID(n.ID), epoch, reason)
							lastProposalGateEpoch = epoch
							lastProposalGateReason = reason
						}
						holdRoundClock(epoch)
						continue
					}
				}
				leaderID, _, _ := n.selectLiveLeaderForHeightRound(epoch, round, validators)
				if leaderID == "" {
					continue
				}
				if round != lastRound {
					if DebugConsensus && round > 0 {
						fmt.Printf("[ROUND-FAILOVER] height=%d round=%d leader=%s\n", epoch, round, ShortID(leaderID))
					}
					lastRound = round
					roundHeight := epoch
					roundToStart := round
					roundLedger := n.currentExecutionLedgerClone()
					_ = n.scheduleConsensusPriorityTask(func() {
						n.startConsensusRound(roundHeight, roundToStart)
					})
					n.scheduleProposalDeadlineGuard(roundHeight, roundToStart, roundLedger)
				}
				snap := n.committeeLivenessSnapshot(epoch)
				live := snap.Live
				if live < required {
					if DebugConsensus {
						n.logLivenessReasonSummary("consensus", epoch, required, snap)
					}
				}
				n.setProposedRound(epoch, round)

				// Leader-stall fallback: broadcast a deterministic empty block if the leader stalls.
				if !lastEpochAt.IsZero() && lastFallbackEpoch != epoch && leaderID == n.ID {
					timeout := LeaderStallTimeout
					if timeout <= 0 {
						timeout = 3 * realTick
					}
					minTimeout := 3 * realTick
					if timeout < minTimeout {
						timeout = minTimeout
					}
					if time.Since(lastEpochAt) >= timeout {
						// Keep fallback block construction/publication on the
						// consensus lane so stalled recovery cannot compete with
						// normal proposal work on arbitrary goroutines.
						fallbackHeight := epoch
						fallbackRound := round
						if n.scheduleConsensusPriorityTask(func() {
							if currentRound := n.proposedRoundForHeight(fallbackHeight); currentRound > fallbackRound {
								return
							}
							if !n.isRoundLeader(fallbackHeight, fallbackRound) {
								return
							}
							n.setProposedRound(fallbackHeight, fallbackRound)
							if n.reuseLeaderProposalForRound(fallbackHeight, fallbackRound, "fallback_block") {
								return
							}
							n.clearLeaderBlock(fallbackHeight)
							fallback := n.BuildFallbackBlock(fallbackHeight)
							if fallback.StateRoot != "" && n.enterProposePhase(fallback, "fallback_block") {
								n.markLeaderProposalSent(fallbackHeight, fallback.Round)
							}
						}) {
							lastFallbackEpoch = epoch
						}
					}
				}

				if leaderID == "" || leaderID != n.ID {
					continue
				}

				proposeRetry := LeaderStallTimeout
				if proposeRetry <= 0 {
					proposeRetry = 3 * realTick
				}
				if proposeRetry < realTick {
					proposeRetry = realTick
				}

				trigger := "built_block"
				sameEpoch, sameRound, throttle := n.leaderProposalRetryState(epoch, round, proposeRetry)
				if throttle {
					continue
				}
				if sameEpoch && sameRound {
					trigger = "rebroadcast_block"
				}
				proposalHeight := epoch
				proposalRound := round
				proposalLedger := n.currentExecutionLedgerClone()
				_ = n.scheduleConsensusPriorityTask(func() {
					_ = n.forceRoundProposal(proposalHeight, proposalRound, proposalLedger, trigger)
				})
			}
		}
	}()
	return nil
}

func (n *Node) initPubSubTopics() error {

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ HARD GUARD ÃƒÂ¢Ã¢â€šÂ¬Ã¢â‚¬Â PubSub must exist
	// =====================================================
	if n.PubSub == nil {
		return fmt.Errorf("pubsub not initialized")
	}

	var err error

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã‚Â§Ã‚Â± BLOCKS TOPIC (AUTHORITATIVE STATE)
	// =====================================================
	if n.BlockTopic == nil {
		n.BlockTopic, err = n.PubSub.Join(TopicBlock)
		if err != nil {
			return err
		}
	}
	// Legacy block topic (best-effort)
	if n.TopicBlocks == nil {
		if legacy, legacyErr := n.PubSub.Join(TopicBlocksLegacy); legacyErr == nil {
			n.TopicBlocks = legacy
		} else if DebugNet {
			fmt.Printf("ÃƒÂ¢Ã‚ÂÃ…â€™ Legacy block topic join failed: %v\n", legacyErr)
		}
	}

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã¢â‚¬â„¢Ã‚Â¸ TRANSACTIONS TOPIC (MEMPOOL)
	// =====================================================
	if n.TxTopic == nil {
		n.TxTopic, err = n.PubSub.Join(TopicTx)
		if err != nil {
			return err
		}
	}

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã‚Â¤Ã‚Â VALIDATORS TOPIC (CONSENSUS MEMBERSHIP) ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ‚Â¥ FIX
	// =====================================================
	if n.ValidatorTopic == nil {
		n.ValidatorTopic, err = n.PubSub.Join(TopicValidator)
		if err != nil {
			return fmt.Errorf("validator topic join failed: %w", err)
		}
	}
	if n.ValidatorSub == nil {
		n.ValidatorSub, err = n.ValidatorTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("validator subscription failed: %w", err)
		}
	}

	// =====================================================
	// ÃƒÆ’Ã‚Â°Ãƒâ€¦Ã‚Â¸ÃƒÂ¢Ã¢â€šÂ¬Ã‚ÂºÃƒâ€šÃ‚Â¤ CONSENSUS TOPIC (EXEC/COMMIT/SNAPSHOTS)
	// =====================================================
	if n.ConsensusTopic == nil {
		n.ConsensusTopic, err = n.PubSub.Join(TopicConsensus)
		if err != nil {
			return fmt.Errorf("consensus topic join failed: %w", err)
		}
	}
	if n.ConsensusSub == nil {
		n.ConsensusSub, err = n.ConsensusTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("consensus subscription failed: %w", err)
		}
	}

	if n.SnapshotMetaTopic == nil {
		n.SnapshotMetaTopic, err = n.PubSub.Join(TopicSnapshotMeta)
		if err != nil {
			return fmt.Errorf("snapshot meta topic join failed: %w", err)
		}
	}
	if n.SnapshotMetaSub == nil {
		n.SnapshotMetaSub, err = n.SnapshotMetaTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("snapshot meta subscription failed: %w", err)
		}
	}

	if n.SnapshotChunkTopic == nil {
		n.SnapshotChunkTopic, err = n.PubSub.Join(TopicSnapshotChunk)
		if err != nil {
			return fmt.Errorf("snapshot chunk topic join failed: %w", err)
		}
	}
	if n.SnapshotChunkSub == nil {
		n.SnapshotChunkSub, err = n.SnapshotChunkTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("snapshot chunk subscription failed: %w", err)
		}
	}

	if n.SnapshotProofTopic == nil {
		n.SnapshotProofTopic, err = n.PubSub.Join(TopicSnapshotProof)
		if err != nil {
			return fmt.Errorf("snapshot proof topic join failed: %w", err)
		}
	}
	if n.SnapshotProofSub == nil {
		n.SnapshotProofSub, err = n.SnapshotProofTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("snapshot proof subscription failed: %w", err)
		}
	}

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã¢â‚¬Å“Ã‚Â¡ DEBUG LOGS
	// =====================================================
	if DebugNet {
		fmt.Println("ÃƒÂ°Ã…Â¸Ã¢â‚¬Å“Ã‚Â¡ PubSub topics initialized (MSC)")
		fmt.Println("   msc-block        block propagation")
		fmt.Println("   msc-tx           mempool ingress")
		fmt.Println("   msc-consensus    exec/commit/snapshot")
		fmt.Println("   msc-validator    validator gossip")
		fmt.Println("   msc-snapshot-meta snapshot manifest metadata")
		fmt.Println("   msc-snapshot-chunk snapshot chunk availability")
		fmt.Println("   msc-snapshot-proof snapshot anchor proofs")
	}

	return nil
}

func (bc *Blockchain) Height() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.Blocks) == 0 {
		return 0
	}
	return bc.Blocks[len(bc.Blocks)-1].ID
}

func (bc *Blockchain) FinalizedHeight() uint64 {
	// In this codebase, the canonical chain tip is the finalized height.
	return bc.Height()
}

func (bc *Blockchain) GetBlock(height uint64) (Block, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if height == 0 || len(bc.Blocks) == 0 {
		return Block{}, false
	}
	idx := int(height - 1)
	if idx >= 0 && idx < len(bc.Blocks) {
		if bc.Blocks[idx].ID == height {
			return bc.Blocks[idx], true
		}
	}
	// Fallback for safety if the slice index doesn't align.
	for i := len(bc.Blocks) - 1; i >= 0; i-- {
		if bc.Blocks[i].ID == height {
			return bc.Blocks[i], true
		}
		if bc.Blocks[i].ID < height {
			break
		}
	}
	return Block{}, false
}
func (n *Node) GetActiveValidators() []string {
	consensusHeight := n.currentEpoch()
	consensus := canonicalValidatorIDs(n.GetConsensusValidators(int(consensusHeight)))
	if len(consensus) == 0 {
		return nil
	}

	now := time.Now()
	localFinalized := n.getFinalizedHeight()
	out := make([]string, 0, len(consensus))

	n.validatorMu.RLock()
	for _, id := range consensus {
		normID := normalizeValidatorID(id)
		if normID == "" {
			continue
		}
		st := n.validatorStatus[normID]
		if !n.isValidatorLiveForConsensusLocked(normID, st, now, localFinalized) {
			continue
		}
		out = append(out, id)
	}
	n.validatorMu.RUnlock()
	sort.Strings(out)

	if DebugConsensus {
		chainHeight := uint64(0)
		if n.Blockchain != nil {
			chainHeight = n.Blockchain.Height()
		}
		fmt.Printf(
			"[LIVENESS] live_validators=%d height=%d local_finalized=%d drift_limit=%d ids=%v\n",
			len(out),
			chainHeight,
			localFinalized,
			validatorLivenessMaxHeightDriftBlocks(),
			out,
		)
	}

	return out
}

func (n *Node) validatorInAnyHeartbeatSet(id string, reportedHeight uint64, finalizedHeight uint64, execEpoch uint64, validatorSetHeight uint64) bool {
	id = normalizeValidatorID(id)
	if id == "" || n == nil {
		return false
	}
	heights := []uint64{
		finalizedHeight,
		reportedHeight,
		execEpoch,
		validatorSetHeight,
		n.currentEpoch(),
	}
	checked := make(map[uint64]struct{}, len(heights)*2)
	for _, h := range heights {
		if h == 0 {
			continue
		}
		candidates := []uint64{h}
		if h > 1 {
			candidates = append(candidates, h-1)
		}
		for _, target := range candidates {
			if target == 0 {
				continue
			}
			if _, ok := checked[target]; ok {
				continue
			}
			checked[target] = struct{}{}
			if inSet, _, ok := n.authoritativeHeartbeatMembershipAtHeight(id, target); ok {
				if inSet {
					return true
				}
				continue
			}
			if n.isValidatorInSetForHeight(id, target) {
				return true
			}
		}
	}
	// Bootstrap-only fallback: once chain history exists, sync must converge via
	// committed validator authority rather than core membership.
	if validatorSetAutohealStrictCoreQuorum() && n.coreBootstrapAuthorityAllowed() {
		for _, coreID := range n.activeCoreAuthorityIDs() {
			if normalizeValidatorID(coreID) == id {
				return true
			}
		}
	}
	return false
}

func (n *Node) authoritativeHeartbeatMembershipForAnnouncement(id string, reportedHeight uint64, finalizedHeight uint64, execEpoch uint64, validatorSetHeight uint64) (bool, string) {
	id = normalizeValidatorID(id)
	if id == "" || n == nil {
		return false, "none"
	}
	heights := []uint64{
		finalizedHeight,
		reportedHeight,
		execEpoch,
		validatorSetHeight,
		n.currentEpoch(),
	}
	checked := make(map[uint64]struct{}, len(heights)*2)
	for _, h := range heights {
		if h == 0 {
			continue
		}
		candidates := []uint64{h}
		if h > 1 {
			candidates = append(candidates, h-1)
		}
		for _, target := range candidates {
			if target == 0 {
				continue
			}
			if _, ok := checked[target]; ok {
				continue
			}
			checked[target] = struct{}{}
			if inSet, source, ok := n.authoritativeHeartbeatMembershipAtHeight(id, target); ok {
				if inSet {
					return true, source
				}
				continue
			}
		}
	}
	return false, "none"
}

func (n *Node) handleValidatorAnnouncement(data []byte) {
	var ann ValidatorAnnouncement
	if err := json.Unmarshal(data, &ann); err != nil {
		return
	}
	ann.NodeID = normalizeValidatorID(ann.NodeID)

	// Ignore self & invalid
	if ann.NodeID == "" || ann.NodeID == normalizeValidatorID(n.ID) {
		return
	}

	if ann.PubKey == "" {
		return
	}
	pkBytes, err := hex.DecodeString(ann.PubKey)
	if err != nil || len(pkBytes) != ed25519.PublicKeySize {
		return
	}

	if ann.Signature != "" {
		sigBytes, err := hex.DecodeString(ann.Signature)
		if err != nil {
			return
		}
		reported := ann.ReportedHeight
		if reported == 0 {
			reported = ann.Height
		}
		finalized := ann.FinalizedHeight
		if finalized == 0 {
			finalized = reported
		}
		execEpoch := ann.ExecEpoch
		if execEpoch == 0 {
			execEpoch = finalized + 1
		}
		validatorSetHeight := validatorAnnouncementActivationHeight(ann)
		if validatorSetHeight == 0 {
			validatorSetHeight = execEpoch
		}
		validatorSetHash := strings.ToLower(strings.TrimSpace(ann.ValidatorSetHash))
		nextActivationHeight := ann.NextActivationHeight
		if nextActivationHeight == 0 && validatorSetHeight > 0 {
			nextActivationHeight = validatorSetHeight + 1
		}
		nextValidatorSetHash := strings.ToLower(strings.TrimSpace(ann.NextValidatorSetHash))
		if nextValidatorSetHash == "" {
			nextValidatorSetHash = validatorSetHash
		}
		v5OK := false
		if ann.ConsensusReadySet {
			v5OK = ed25519.Verify(
				ed25519.PublicKey(pkBytes),
				validatorAnnounceSignBytesV5(
					ann.NodeID,
					ann.PubKey,
					ann.P2PAddr,
					reported,
					finalized,
					execEpoch,
					validatorSetHeight,
					validatorSetHash,
					nextValidatorSetHash,
					nextActivationHeight,
					ann.ConsensusReadySet,
					ann.ConsensusReady,
					ann.IsValidator,
				),
				sigBytes,
			)
		}
		v4OK := false
		v4OK = ed25519.Verify(
			ed25519.PublicKey(pkBytes),
			validatorAnnounceSignBytesV4(
				ann.NodeID,
				ann.PubKey,
				ann.P2PAddr,
				reported,
				finalized,
				execEpoch,
				validatorSetHeight,
				validatorSetHash,
				nextValidatorSetHash,
				nextActivationHeight,
				ann.IsValidator,
			),
			sigBytes,
		)
		v3OK := false
		v3OK = ed25519.Verify(
			ed25519.PublicKey(pkBytes),
			validatorAnnounceSignBytesV3(
				ann.NodeID,
				ann.PubKey,
				ann.P2PAddr,
				reported,
				finalized,
				execEpoch,
				validatorSetHeight,
				validatorSetHash,
				ann.IsValidator,
			),
			sigBytes,
		)
		if !v5OK && !v4OK && !v3OK && !ed25519.Verify(
			ed25519.PublicKey(pkBytes),
			validatorAnnounceSignBytesV2(ann.NodeID, ann.PubKey, ann.P2PAddr, reported, finalized, execEpoch, ann.IsValidator),
			sigBytes,
		) {
			if !ed25519.Verify(
				ed25519.PublicKey(pkBytes),
				validatorAnnounceSignBytes(ann.NodeID, ann.PubKey, reported, finalized, execEpoch, ann.IsValidator),
				sigBytes,
			) {
				if !ed25519.Verify(
					ed25519.PublicKey(pkBytes),
					validatorAnnounceSignBytesLegacy(ann.NodeID, ann.PubKey, ann.Height, ann.IsValidator),
					sigBytes,
				) {
					if DebugConsensus {
						fmt.Printf("Invalid validator announce signature: %s\n", ShortID(ann.NodeID))
					}
					return
				}
			}
		}
	}

	// Store pubkey (testnet allows override; mainnet rejects mismatches)
	validatorPubKeysMu.RLock()
	existing, existingOK := ValidatorPubKeys[ann.NodeID]
	validatorPubKeysMu.RUnlock()
	pubKeyUpdated := !existingOK || !bytes.Equal(existing, pkBytes)
	if existingOK && len(existing) == ed25519.PublicKeySize {
		if !bytes.Equal(existing, pkBytes) {
			if !IsTestnet {
				if DebugConsensus {
					fmt.Printf("Pubkey mismatch for validator %s\n", ShortID(ann.NodeID))
				}
				return
			}
			if DebugConsensus {
				fmt.Printf("Pubkey override (testnet) for validator %s\n", ShortID(ann.NodeID))
			}
		}
	}
	validatorPubKeysMu.Lock()
	ValidatorPubKeys[ann.NodeID] = ed25519.PublicKey(pkBytes)
	validatorPubKeysMu.Unlock()
	if pubKeyUpdated {
		// A queued block may have failed signature verification before this
		// validator key refresh landed; retry immediately.
		go n.ProcessQueuedBlocks()
	}

	// Normalize heights for logging/state.
	reported := ann.ReportedHeight
	if reported == 0 {
		reported = ann.Height
	}
	finalized := ann.FinalizedHeight
	if finalized == 0 {
		finalized = reported
	}
	execEpoch := ann.ExecEpoch
	if execEpoch == 0 {
		execEpoch = finalized + 1
	}
	setHeight := validatorAnnouncementActivationHeight(ann)
	if setHeight == 0 {
		setHeight = execEpoch
	}

	if ann.P2PAddr != "" {
		n.HandlePeerHello("", ann.NodeID, ann.P2PAddr)
		if n.Host != nil && n.canDialPeer() {
			if maddr, err := ma.NewMultiaddr(ann.P2PAddr); err == nil {
				if info, err := peer.AddrInfoFromP2pAddr(maddr); err == nil {
					if info.ID != n.Host.ID() && len(n.Host.Network().ConnsToPeer(info.ID)) == 0 {
						n.connectToPeersAsync([]string{ann.P2PAddr}, 12*time.Second)
					}
				}
			}
		}
	}

	setHash := strings.ToLower(strings.TrimSpace(ann.ValidatorSetHash))
	inSet := n.validatorInAnyHeartbeatSet(ann.NodeID, reported, finalized, execEpoch, setHeight)
	if !ann.IsValidator {
		if authInSet, source := n.authoritativeHeartbeatMembershipForAnnouncement(ann.NodeID, reported, finalized, execEpoch, setHeight); authInSet {
			ann.IsValidator = true
			inSet = true
			if DebugConsensus {
				if n.shouldLogLivenessReason(fmt.Sprintf("heartbeat_fallback:%s:%d", ann.NodeID, reported), livenessReasonLogCooldown) {
					fmt.Printf("[HEARTBEAT-FALLBACK] id=%s height=%d source=%s reason=peer_advertised_candidate_but_in_set\n",
						ShortID(ann.NodeID), reported, source)
				}
			}
		}
	}

	// Candidate heartbeats (permissionless, no owner approval).
	// Strict activation relies on IsValidator=false to keep join/rejoin nodes
	// in candidate lane until frozen-set activation.
	if !ann.IsValidator || !inSet {
		n.registerCandidateHeartbeat(ann, ed25519.PublicKey(pkBytes), reported, finalized, execEpoch, setHeight, setHash)
		n.maybeOfferSnapshotToValidator(ann.NodeID, reported)
		if DebugConsensus {
			fmt.Printf(
				"Candidate heartbeat received: %s | reported_height=%d | finalized_height=%d | local_exec_epoch=%d\n",
				ShortID(ann.NodeID),
				reported,
				finalized,
				execEpoch,
			)
		}
		return
	}

	// Ignore only deeply stale heartbeats; allow small drift so quorum does not
	// collapse while lagging validators catch up.
	const staleHeartbeatIgnoreDrift uint64 = 8
	localFinalized := uint64(0)
	localFinalized = n.getFinalizedHeight()
	if localFinalized > 0 && reported > 0 && localFinalized > reported+staleHeartbeatIgnoreDrift {
		n.maybeOfferSnapshotToValidator(ann.NodeID, reported)
		if DebugConsensus {
			fmt.Printf("Ignoring stale heartbeat: %s reported=%d local_finalized=%d\n",
				ShortID(ann.NodeID), reported, localFinalized)
		}
		return
	}

	// Register heartbeat + activation
	n.RegisterValidator(ann.NodeID, reported, finalized, execEpoch, setHeight, setHash)
	if ann.ConsensusReadySet {
		n.setValidatorConsensusReady(ann.NodeID, ann.ConsensusReady)
	}
	n.recordHeightReport(ann.NodeID, finalized)
	n.recomputeFinalizedHeight()
	n.addPendingValidator(ann.NodeID)
	currentEpoch := n.currentEpoch()
	syncing, _, _ := n.effectiveConsensusSyncState(n.getFinalizedHeight())
	if !syncing {
		safeModeProgressed := false
		if ConsensusPostBlockSafeModeEnabled && currentEpoch > 0 {
			if active, until, _ := n.postBlockSafeModeState(currentEpoch); active || !until.IsZero() {
				safeModeProgressed = n.tryExitPostBlockSafeMode(currentEpoch)
			}
		}
		n.replayQueuedExecutionVotes()
		if safeModeProgressed {
			n.maybeBroadcastCurrentLeaderExecutionVote("validator_heartbeat")
		}
	}
	n.maybeRequestSyncFromHeartbeats()
	n.maybeOfferSnapshotToValidator(ann.NodeID, reported)
	n.maybeTriggerAdaptiveValidatorSnapshotPublish("validator_heartbeat")

	if DebugConsensus {
		fmt.Printf(
			"Validator heartbeat received: %s | reported_height=%d | finalized_height=%d | local_exec_epoch=%d\n",
			ShortID(ann.NodeID),
			reported,
			finalized,
			execEpoch,
		)
	}
}

func (n *Node) isSelfCandidateForHeight(height uint64) bool {
	n.candidateMu.RLock()
	cand := n.candidates[n.ID]
	n.candidateMu.RUnlock()
	if cand == nil || cand.PermanentBan {
		return false
	}
	if cand.BanUntil > 0 && height < cand.BanUntil {
		return false
	}
	if height > 0 {
		if cand.LastFinalizedHeight < height && cand.LastReportedHeight < height {
			return false
		}
	}
	return true
}

func (n *Node) registerCandidateHeartbeat(
	ann ValidatorAnnouncement,
	pk ed25519.PublicKey,
	reported uint64,
	finalized uint64,
	execEpoch uint64,
	validatorSetHeight uint64,
	validatorSetHash string,
) {
	n.candidateMu.Lock()
	defer n.candidateMu.Unlock()

	cand, ok := n.candidates[ann.NodeID]
	if !ok {
		cand = &CandidateStatus{
			ID:         ann.NodeID,
			PubKey:     pk,
			ExecHashes: make(map[uint64]string),
		}
		cand.FirstSeenHeight = n.Blockchain.Height()
		n.candidates[ann.NodeID] = cand
	}

	if len(cand.PubKey) == 0 {
		cand.PubKey = pk
	} else if !bytes.Equal(cand.PubKey, pk) {
		if !IsTestnet {
			return
		}
		cand.PubKey = pk
	}

	cand.LastReportedHeight = reported
	cand.LastFinalizedHeight = finalized
	cand.HeartbeatTotal++
	if cand.LastHeartbeatEpoch == 0 || execEpoch >= cand.LastHeartbeatEpoch {
		cand.HeartbeatGood++
	}
	cand.LastHeartbeatEpoch = execEpoch
	if validatorSetHeight == 0 {
		validatorSetHeight = execEpoch
	}
	cand.LastValidatorSetHeight = validatorSetHeight
	cand.LastValidatorSetHash = strings.ToLower(strings.TrimSpace(validatorSetHash))
	cand.LastHeartbeatAt = time.Now()

	if !DeterministicValidatorSelection {
		GlobalValidatorRegistry.Ensure(ann.NodeID, n.Blockchain.Height())
	}

	if cand.ObservationStartHeight == 0 {
		if reported > 0 || finalized > 0 {
			start := reported
			if finalized > 0 && (start == 0 || finalized < start) {
				start = finalized
			}
			if start > 0 {
				cand.ObservationStartHeight = start
			}
		}
	}

	if cand.BanUntil > 0 && n.Blockchain.Height() >= cand.BanUntil {
		cand.BanUntil = 0
	}
}

func (n *Node) WaitForValidatorQuorum(min int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if n.countLiveValidators() >= min {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func (n *Node) WaitForWalletAuth(ctx context.Context, timeout time.Duration) bool {
	// Stake gate for validator activation. Network startup is handled separately.
	if n.Role != "validator" {
		return true
	}
	// Core validators can be exempt from stake gate by policy.
	if ValidatorCoreStakeExempt && n.isCoreValidatorCurrent(n.ID) {
		return true
	}
	if !ValidatorRequireStake {
		return true
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if n.hasRequiredValidatorStake() {
			return true
		}
		if timeout > 0 && time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (n *Node) canParticipateAsValidator() bool {
	if n == nil {
		return false
	}
	if n.Role != "validator" {
		return false
	}
	if !isValidatorKeyUsable(n.ValidatorKey) {
		return false
	}
	height := n.currentEpoch()
	// Core validators can be held in pending phase and excluded from proposer/committee.
	if n.isCoreValidatorCurrent(n.ID) {
		if !n.coreEligibleForConsensus(height) {
			return false
		}
		return n.hasRequiredValidatorStake()
	}
	return n.hasRequiredValidatorStake()
}

func (n *Node) hasWalletLoginForValidator() bool {
	if n == nil {
		return false
	}
	if !n.requiresWalletAuthCurrent(n.ID) {
		return true
	}
	if n.bootstrapGenesisWalletAuthSatisfied(n.ID) {
		return true
	}
	authMu.Lock()
	walletAddr := authWalletAddr
	nodeID := authNodeID
	authMu.Unlock()
	return walletAddr != "" && normalizeValidatorID(nodeID) == normalizeValidatorID(n.ID)
}

func (n *Node) bootstrapGenesisWalletAuthSatisfied(nodeID string) bool {
	if n == nil {
		return false
	}
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return false
	}
	return genesisWalletAuthExemptValidator(id)
}

func (n *Node) canParticipateInConsensusNow() bool {
	ready, _ := n.validatorParticipationGateStatus(0)
	return ready
}

func (n *Node) validatorConsensusSigningAuthorityStatus(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	selfID := normalizeValidatorID(n.ID)
	if selfID == "" || len(n.ValidatorKey.PublicKey) != ed25519.PublicKeySize {
		return false, "validator_key_unavailable"
	}

	committee := n.frozenValidatorsForHeight(height)
	if len(committee) == 0 {
		return true, "committee_not_frozen"
	}
	if !containsNormalizedValidatorID(committee, selfID) {
		return true, "not_in_committee"
	}

	if height <= 1 || !validatorSetCommitmentV2EnabledAt(height-1) {
		return true, "registry_authority_not_committed"
	}
	if height == 2 {
		if _, committedAuthority := n.chainParentCommittedValidatorRegistryHash(height); !committedAuthority {
			return true, "registry_authority_not_committed"
		}
	}
	registrySnapshot := n.validatorRegistrySnapshotForHeight(height)
	record, exists := validatorRecordFromStakeSnapshot(registrySnapshot, selfID)
	expected := normalizeConsensusPubKeyHex(record.ConsensusPubKey)
	if !exists || expected == "" {
		return false, "validator_consensus_pubkey_unanchored"
	}
	local := strings.ToLower(hex.EncodeToString(n.ValidatorKey.PublicKey))
	if !strings.EqualFold(local, expected) {
		return false, "validator_consensus_pubkey_mismatch"
	}
	return true, "ready"
}

func (n *Node) validatorParticipationGateStatus(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	if isolated, reason := n.validatorSyncIsolationState(height); isolated {
		if strings.TrimSpace(reason) == "" {
			reason = "syncing"
		}
		return false, reason
	}
	if n.Role != "validator" {
		return false, "role_not_validator"
	}
	if !isValidatorKeyUsable(n.ValidatorKey) {
		return false, "validator_key_unavailable"
	}
	if ready, reason := n.validatorConsensusSigningAuthorityStatus(height); !ready {
		return false, reason
	}
	if n.isCoreValidatorCurrent(n.ID) && !n.coreEligibleForConsensus(height) {
		return false, "core_pending_activation"
	}
	if !n.hasRequiredValidatorStake() {
		return false, "validator_stake_required"
	}
	if !n.isCoreRegistryTrustReadyForValidatorParticipation() {
		return false, "core_registry_unverified"
	}
	if !n.hasWalletLoginForValidator() {
		return false, "wallet_auth_required"
	}
	if validatorOnboardingStrictActivationEnabled() {
		if active, reason := n.selfActiveValidatorAt(height); !active {
			if strings.TrimSpace(reason) == "" {
				reason = "activation_pending_not_in_frozen_set"
			}
			return false, reason
		}
	}
	return true, "ready"
}

func (n *Node) hasRequiredValidatorStake() bool {
	if n == nil {
		return false
	}
	// Core validators can be exempt from stake gate by policy.
	if ValidatorCoreStakeExempt && n.isCoreValidatorCurrent(n.ID) {
		return true
	}
	if !ValidatorRequireStake {
		return true
	}
	if n.ID == "" {
		return false
	}

	required := int(ValidatorMinStake)
	ledgers := []Ledger{
		n.currentExecutionLedgerClone(),
		n.Ledger.Clone(),
		n.ExecutionLedger.Clone(),
	}
	for _, ledger := range ledgers {
		total := 0
		for _, amount := range validatorStakeTotals(&ledger, n.ID) {
			total += amount
		}
		if total >= required {
			return true
		}
	}

	return false
}

// WaitForWalletLogin blocks until a wallet authentication is completed for this node.
// Unlike WaitForWalletAuth, it does NOT require stake eligibility.
func (n *Node) WaitForWalletLogin(ctx context.Context, timeout time.Duration) bool {
	if !n.requiresWalletAuthCurrent(n.ID) {
		return true
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if n.hasWalletLoginForValidator() {
			return true
		}
		if timeout > 0 && time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (n *Node) handleConsensusEnvelope(data []byte) bool {
	return n.handleConsensusEnvelopeFromPeer(data, "")
}

func (n *Node) handleConsensusEnvelopeFromPeer(data []byte, peerID string) bool {
	var wrapped Message
	if err := UnmarshalP2PMessage(data, &wrapped); err != nil || wrapped.Type == "" {
		return false
	}
	switch wrapped.Type {
	case MsgExecutionResult:
		var res ExecutionResultMsg
		if err := json.Unmarshal(wrapped.Data, &res); err == nil {
			if allowed, reason := n.allowExecutionVoteNetworkIngress(res); !allowed {
				n.logExecutionVoteIngressDrop(reason, res, "consensus_gossip")
				return true
			}
			_ = n.submitExecutionResultOnConsensusLane(res, true)
			return true
		}
	case MsgLeaderBlock:
		var block Block
		if err := json.Unmarshal(wrapped.Data, &block); err == nil {
			_ = n.submitLeaderBlockOnConsensusLane(block, "")
			return true
		}
	case MsgValidatorAnnounce:
		n.handleValidatorAnnouncement(wrapped.Data)
		return true
	case MsgCommit:
		var cm CommitMsg
		if err := json.Unmarshal(wrapped.Data, &cm); err == nil {
			_ = n.submitCommitMsgOnConsensusLane(cm)
			return true
		}
	case MsgSnapshotOffer:
		var offer SnapshotOffer
		if err := json.Unmarshal(wrapped.Data, &offer); err == nil {
			n.handleSnapshotOffer(offer)
			return true
		}
	case MsgSnapshotMeta:
		var meta SnapshotMetaGossip
		if err := json.Unmarshal(wrapped.Data, &meta); err == nil {
			n.handleSnapshotMetaGossipMessage(meta)
			return true
		}
	case MsgSnapshotChunk:
		var chunk SnapshotChunkGossip
		if err := json.Unmarshal(wrapped.Data, &chunk); err == nil {
			n.handleSnapshotChunkGossipMessage(chunk)
			return true
		}
	case MsgSnapshotProof:
		var proof SnapshotProof
		if err := json.Unmarshal(wrapped.Data, &proof); err == nil {
			n.handleSnapshotProofFromPeer(proof, peerID)
			return true
		}
	}
	return false
}

func (n *Node) listenConsensus(ctx context.Context) {
	sub := n.ConsensusSub
	if sub == nil {
		if n.PubSub == nil {
			log.Println("ConsensusSub is nil")
			return
		}
		var err error
		if n.ConsensusTopic == nil {
			n.ConsensusTopic, err = n.PubSub.Join(TopicConsensus)
			if err != nil {
				log.Printf("consensus topic rejoin failed: %v", err)
				return
			}
		}
		sub, err = n.ConsensusTopic.Subscribe()
		if err != nil {
			log.Printf("consensus subscription rejoin failed: %v", err)
			return
		}
		n.ConsensusSub = sub
		log.Println("[GOSSIP-REPAIR] consensus subscription restored")
	}
	defer func() {
		if n.ConsensusSub == sub {
			n.ConsensusSub = nil
		}
		sub.Cancel()
	}()

	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			log.Println("consensus listener stopped:", err)
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		_ = n.handleConsensusEnvelope(msg.Data)
	}
}

func (n *Node) listenValidators(ctx context.Context) {
	sub := n.ValidatorSub
	if sub == nil {
		if n.PubSub == nil {
			log.Println("ValidatorSub is nil")
			return
		}
		var err error
		if n.ValidatorTopic == nil {
			n.ValidatorTopic, err = n.PubSub.Join(TopicValidator)
			if err != nil {
				log.Printf("validator topic rejoin failed: %v", err)
				return
			}
		}
		sub, err = n.ValidatorTopic.Subscribe()
		if err != nil {
			log.Printf("validator subscription rejoin failed: %v", err)
			return
		}
		n.ValidatorSub = sub
		log.Println("[GOSSIP-REPAIR] validator subscription restored")
	}
	defer func() {
		if n.ValidatorSub == sub {
			n.ValidatorSub = nil
		}
		sub.Cancel()
	}()

	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			log.Println("validator listener stopped:", err)
			return
		}

		if n.handleConsensusEnvelope(msg.Data) {
			continue
		}

		// Support both raw ValidatorInfo and Message wrappers
		var wrapped Message
		if err := UnmarshalP2PMessage(msg.Data, &wrapped); err == nil && wrapped.Type != "" {
			switch wrapped.Type {
			case MsgValidatorAnnounce:
				n.handleValidatorAnnouncement(wrapped.Data)
			case MsgValidatorSetUpdate:
				var update ValidatorSetUpdate
				if err := json.Unmarshal(wrapped.Data, &update); err == nil {
					n.handleValidatorSetUpdate(update)
				}
			}
			continue
		}

		var info ValidatorInfo
		if err := json.Unmarshal(msg.Data, &info); err != nil {
			continue
		}
		info.ID = normalizeValidatorID(info.ID)
		if info.ID == "" {
			continue
		}

		n.validatorMu.Lock()

		st, ok := n.validatorStatus[info.ID]
		if !ok {
			st = &ValidatorStatus{}
			n.validatorStatus[info.ID] = st
		}

		st.Height = info.Height
		st.LastSeen = time.Now()

		participationMu.Lock()
		if _, ok := Participation[info.ID]; !ok {
			Participation[info.ID] = &ParticipationScore{
				ValidBlocks:   1,
				InvalidBlocks: 0,
				LastSeen:      time.Now(),
				CooldownUntil: 0,
				Reputation:    100,
			}
		}
		participationMu.Unlock()

		n.validatorMu.Unlock()
	}
}

func BuildGenesisBlock(g Genesis) (Block, error) {

	if g.ChainID == "" {
		return Block{}, errors.New("genesis missing chain_id")
	}
	if len(g.Validators) == 0 {
		return Block{}, errors.New("genesis has no validators")
	}

	// ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ deterministic payload (NO maps in block)
	payload := struct {
		ChainID    string
		Validators map[string]string
	}{
		ChainID:    g.ChainID,
		Validators: g.Validators,
	}

	payloadBytes, _ := json.Marshal(payload)
	stateHash := sha256.Sum256(payloadBytes)

	block := Block{
		ID:        0,
		Type:      BlockTypeGenesis,
		PrevHash:  "",
		Proposer:  "genesis",
		Timestamp: 0, // ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ MUST be deterministic
		BlockTime: LogicalTimeForEpoch(0),
		StateRoot: hex.EncodeToString(stateHash[:]),
	}

	block.BlockHash = HashBlock(block)
	return block, nil
}

func (b Block) CalculateHash() string {
	data := ""
	if validatorSetCommitmentV2EnabledAt(b.ID) {
		activationHeight := canonicalActivationHeight(b.NextValidatorSetHeight, b.ActivationHeight)
		data = fmt.Sprintf(
			"%d|%s|%d|%s|%s|%s|%s|%d|%s|%x",
			b.ID,
			b.PrevHash,
			SystemTimeUnits(b.BlockTime),
			b.Proposer,
			b.StateRoot,
			b.MempoolRoot,
			b.ValidatorSetHash,
			activationHeight,
			strings.TrimSpace(b.NextValidatorSetHash),
			b.Payload,
		)
	} else {
		data = fmt.Sprintf(
			"%d|%s|%d|%s|%s|%s|%s|%x",
			b.ID,
			b.PrevHash,
			SystemTimeUnits(b.BlockTime),
			b.Proposer,
			b.StateRoot,
			b.MempoolRoot,
			b.ValidatorSetHash,
			b.Payload,
		)
	}
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func (db *NodeDB) StoreBlock(block Block) error {
	err := db.Blocks.Update(func(txn *Txn) error {
		height := block.ID
		if height == 0 && block.Height != 0 {
			height = block.Height
		}
		key := []byte(fmt.Sprintf("block:%d", height))
		val, _ := json.Marshal(block)
		enc, err := encryptDBValue(val)
		if err != nil {
			return err
		}
		return txn.Set(key, enc)
	})
	if err != nil {
		return err
	}
	return db.StoreTxRecords(block)
}

func LoadGenesisFromFile(
	db *NodeDB,
	bc *Blockchain,
	path string,
) (*Genesis, error) {

	g, err := loadGenesisFromDisk(path)
	if err != nil {
		return nil, fmt.Errorf("invalid genesis json: %w", err)
	}
	if g.ChainID != "" {
		ChainID = g.ChainID
	}

	block, err := BuildGenesisBlock(*g)
	if err != nil {
		return nil, err
	}

	if err := db.StoreBlock(block); err != nil {
		return nil, err
	}

	bc.Blocks = []Block{block}

	log.Println("ÃƒÂ¢Ã…â€œÃ¢â‚¬Â¦ Genesis loaded from genesis.json (authoritative)")
	return g, nil
}

// At startup ONLY
func LoadGenesisValidators(node *Node) {
	genesis := LoadGenesisFile("genesis.json")

	node.validatorMu.Lock()
	defer node.validatorMu.Unlock()

	node.validatorStatus = make(map[string]*ValidatorStatus)

	for id := range genesis.Validators {
		id = normalizeValidatorID(id)
		if id == "" {
			continue
		}
		node.validatorStatus[id] = &ValidatorStatus{
			Height:   0,
			LastSeen: time.Unix(0, 0), // ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ deterministic
		}

		Participation[id] = &ParticipationScore{
			ValidBlocks:   1,
			InvalidBlocks: 0,
			LastSeen:      time.Unix(0, 0),
			CooldownUntil: 0,
			Reputation:    100,
		}
	}
}

func LoadGenesisFile(path string) *GenesisFile {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[WARN] Failed to read genesis file %s: %v", path, err)
		return &GenesisFile{
			ChainID:    ChainID,
			Validators: map[string]string{},
		}
	}

	var g GenesisFile
	if err := json.Unmarshal(data, &g); err != nil {
		log.Printf("[WARN] Invalid genesis format in %s: %v", path, err)
		return &GenesisFile{
			ChainID:    ChainID,
			Validators: map[string]string{},
		}
	}
	if g.ChainID != "" {
		ChainID = g.ChainID
	}

	if len(g.Validators) == 0 {
		log.Printf("[WARN] Genesis has zero validators (%s). Node will continue in degraded/sync mode.", path)
	}

	return &g
}
func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]) < fmt.Sprint(out[j])
	})
	return out
}
func (n *Node) RegisterValidator(addr string, reportedHeight uint64, finalizedHeight uint64, execEpoch uint64, validatorSetHeight uint64, validatorSetHash string) {
	addr = normalizeValidatorID(addr)
	if addr == "" {
		return
	}
	if !n.validatorInAnyHeartbeatSet(addr, reportedHeight, finalizedHeight, execEpoch, validatorSetHeight) {
		// Anti-flap grace: keep refreshing recently live validators during
		// transient set-height races around epoch transitions.
		st, ok := n.validatorStatusSnapshot(addr)
		if !ok || st.LastSeen.IsZero() || time.Since(st.LastSeen) > 20*time.Second {
			return
		}
	}
	n.validatorMu.Lock()
	defer n.validatorMu.Unlock()

	st, exists := n.validatorStatus[addr]
	if !exists {
		st = &ValidatorStatus{}
		n.validatorStatus[addr] = st
	}

	st.LastSeen = time.Now()
	st.ReportedHeight = reportedHeight
	if finalizedHeight == 0 {
		finalizedHeight = reportedHeight
	}
	st.FinalizedHeight = finalizedHeight
	st.ExecEpoch = execEpoch
	st.Height = finalizedHeight
	if validatorSetHeight == 0 {
		validatorSetHeight = execEpoch
	}
	st.ValidatorSetHeight = validatorSetHeight
	st.ValidatorSetHash = strings.ToLower(strings.TrimSpace(validatorSetHash))
	st.Active = true
	n.recordValidatorRejoinHeartbeatLocked(addr)

	participationMu.Lock()
	if _, ok := Participation[addr]; !ok {
		Participation[addr] = &ParticipationScore{
			ValidBlocks:   1,
			InvalidBlocks: 0,
			LastSeen:      time.Now(),
			CooldownUntil: 0,
			Reputation:    100,
		}
	}
	participationMu.Unlock()

	if DebugConsensus {
		fmt.Printf("Validator heartbeat: %s | reported_height=%d | finalized_height=%d | local_exec_epoch=%d\n",
			ShortID(addr), reportedHeight, finalizedHeight, execEpoch)
	}
}

func (n *Node) setValidatorConsensusReady(addr string, ready bool) {
	if n == nil {
		return
	}
	addr = normalizeValidatorID(addr)
	if addr == "" {
		return
	}
	n.validatorMu.Lock()
	defer n.validatorMu.Unlock()
	st, ok := n.validatorStatus[addr]
	if !ok || st == nil {
		return
	}
	st.Enabled = ready
	st.ConsensusReadyKnown = true
}
