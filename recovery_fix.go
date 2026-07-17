package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

type BlockApplyError struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string
	// `Reason` stores the value associated with this record.
	Reason string
	// `Err` stores the error produced by this operation.
	Err error
}

// Error implements the error helper.
func (e *BlockApplyError) Error() string {
	if e == nil {
		return "apply failed"
	}
	return fmt.Sprintf("apply failed h=%d hash=%s reason=%s err=%v",
		e.Height, e.Hash, strings.TrimSpace(e.Reason), e.Err)
}

// Unwrap implements the unwrap helper.
func (e *BlockApplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// normalizeBlockApplyReason normalizes block apply reason.
func normalizeBlockApplyReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "prev_hash_mismatch":
		return "parent_mismatch"
	case "state_transition_failure", "process_block_incomplete":
		return "execution_failed"
	case "future_block_gap":
		return "future_block"
	case "invalid_height", "validator_set_hash_mismatch", "validator_set_unresolved", "invalid_block_signature", "invalid_proposer", "committed_different_hash":
		return "validation_failed"
	default:
		return strings.TrimSpace(reason)
	}
}

// newBlockApplyError implements the new block apply error helper.
func newBlockApplyError(block Block, reason string, err error) *BlockApplyError {
	// `normalized` stores the value produced by this operation.
	normalized := normalizeBlockApplyReason(reason)
	if err == nil && strings.TrimSpace(reason) != "" && normalized != strings.TrimSpace(reason) {
		err = errors.New(strings.TrimSpace(reason))
	}
	return &BlockApplyError{
		Height: block.ID,
		Hash:   strings.TrimSpace(block.BlockHash),
		Reason: normalized,
		Err:    err,
	}
}

// handleNextValidatorSetCommitmentMismatch handles next validator set commitment mismatch.
func (n *Node) handleNextValidatorSetCommitmentMismatch(localHeight uint64, block Block, reason string) {
	if n == nil || block.ID == 0 {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "next_validator_set_hash_mismatch"
	}
	// `resyncReason` stores the result produced by this operation.
	resyncReason := "next-validator-set-hash-mismatch-autoheal"
	if strings.Contains(strings.ToLower(reason), "root") {
		resyncReason = "next-validator-set-root-mismatch-autoheal"
	}
	if DebugConsensus || DebugSync {
		fmt.Printf("[SYNC-ESCALATE] reason=%s local=%d target=%d detail=full_set_unavailable_from_header_snapshot_recovery_required\n",
			reason,
			localHeight,
			block.ID,
		)
	}
	n.requestConsensusRecomputePause(block.ID, reason)
	n.maybeForceNearTipValidatorSetResync(localHeight, block.ID, resyncReason)
	n.maybeSyncToBestObservedHeight("next_validator_set_mismatch")
}

// ReceiveBlock verifies and applies a block.
func (n *Node) ReceiveBlock(block Block, bc *Blockchain) error {
	if n == nil || bc == nil || n.isShuttingDown() {
		return nil
	}
	n.receiveMu.Lock()
	defer n.receiveMu.Unlock()
	// Committed heights must still drive the local node forward; do not let a
	// committed-tip observation die on a branch-local return.
	alreadyCommitted := false
	n.commitMu.Lock()
	alreadyCommitted = n.committedHeight >= block.ID
	n.commitMu.Unlock()
	if alreadyCommitted {
		_ = n.advanceConsensusToCommittedTip("receive_block_already_committed")
	}

	// `currentHeight` stores the value produced by this operation.
	currentHeight := bc.Height()
	if block.ID <= currentHeight {
		if block.ID > 0 && strings.TrimSpace(block.BlockHash) != "" && n.hasCommittedDifferentHash(block.ID, block.BlockHash) {
			n.recordFinalizedHashConflictEvidence(block.ID, block.Round, "", block.BlockHash, "already_committed_observation")
			n.emitConsensusTelemetry(consensusTelemetryEvent{
				Type:      "finalized_hash_conflict",
				Reason:    "already_committed_observation",
				Height:    block.ID,
				Round:     block.Round,
				BlockHash: block.BlockHash,
			})
			return newBlockApplyError(block, "committed_different_hash", nil)
		}
		if block.ID == currentHeight && !alreadyCommitted {
			_ = n.advanceConsensusToCommittedTip("receive_block_tip_already_applied")
		}
		return nil
	}
	// `nextHeight` stores the value produced by this operation.
	nextHeight := currentHeight + 1
	// `finalizedHeight` stores the value produced by this operation.
	finalizedHeight := n.getFinalizedHeight()
	// `deferMaintenance` stores the value produced by this operation.
	deferMaintenance := n.shouldDeferNonConsensusCommitMaintenance()
	// `syncStage` and `syncProvider` store the value produced by this operation.
	syncStage, syncProvider := n.syncDiagnosticContext()
	// `stageActive` stores the value produced by this operation.
	stageActive := strings.TrimSpace(syncStage) != "" && !strings.EqualFold(strings.TrimSpace(syncStage), "idle")
	// `exactNextSyncBackfill` stores the value produced by this operation.
	exactNextSyncBackfill := false
	if stageActive && block.ID == nextHeight {
		// `lastBlock` stores the synchronization state protecting shared data.
		lastBlock := bc.LastBlock()
		exactNextSyncBackfill = strings.TrimSpace(lastBlock.BlockHash) != "" &&
			strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(lastBlock.BlockHash))
	}
	// `logSyncCommitPhase` stores the value produced by this operation.
	logSyncCommitPhase := func(phase string) {
		if strings.TrimSpace(syncStage) == "" {
			return
		}
		if !DebugSync && !DebugConsensus {
			if strings.TrimSpace(syncStage) == "idle" {
				return
			}
			// `key` stores the key used to access the related value.
			key := fmt.Sprintf("sync_commit_phase:%s:%s", strings.TrimSpace(syncStage), ShortID(syncProvider))
			if !n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
				return
			}
		}
		fmt.Printf("[SYNC-COMMIT-PHASE] node=%s stage=%s provider=%s height=%d local=%d phase=%s\n",
			ShortID(n.ID),
			syncStage,
			ShortID(syncProvider),
			block.ID,
			bc.Height(),
			strings.TrimSpace(phase),
		)
	}
	// `logImmediateReject` stores the value produced by this operation.
	logImmediateReject := func(reason string) {
		if block.ID != nextHeight {
			return
		}
		// `queueTip` stores the value produced by this operation.
		queueTip := n.maxQueuedFutureHeight(currentHeight)
		fmt.Printf("[SYNC-BLOCK-REJECT] stage=%s provider=%s local=%d next=%d got=%d reason=%s queue_tip=%d proposer=%s hash=%s prev=%s\n",
			syncStage,
			ShortID(syncProvider),
			currentHeight,
			nextHeight,
			block.ID,
			strings.TrimSpace(reason),
			queueTip,
			ShortID(block.Proposer),
			ShortHash(block.BlockHash),
			ShortHash(block.PrevHash),
		)
	}
	// `rejectBlock` stores the synchronization state protecting shared data.
	rejectBlock := func(reason string, err error) (*BlockApplyError, uint64) {
		reason = strings.TrimSpace(reason)
		// `applyErr` stores the error produced by this operation.
		applyErr := newBlockApplyError(block, reason, err)
		logImmediateReject(applyErr.Reason)
		// `failCount` stores the measured quantity used by this operation.
		failCount := n.recordSyncApplyFailure(currentHeight, block, applyErr.Reason)
		if block.ID == nextHeight {
			detail := ""
			if applyErr.Err != nil {
				detail = strings.TrimSpace(applyErr.Err.Error())
			}
			fmt.Printf("[APPLY-FAIL] h=%d hash=%s reason=%s detail=%q fail_count=%d\n",
				block.ID, ShortHash(block.BlockHash), strings.TrimSpace(applyErr.Reason), detail, failCount)
		}
		return applyErr, failCount
	}

	// Weak-subjectivity guard against long-range rewrites:
	// very old blocks should be accepted only via snapshot trust path.
	if WeakSubjectivityDepth > 0 && finalizedHeight > 0 {
		if block.ID+WeakSubjectivityDepth <= finalizedHeight && !exactNextSyncBackfill {
			if DebugConsensus {
				fmt.Printf("Rejected long-range block | local_finalized=%d block=%d depth=%d proposer=%s\n",
					finalizedHeight, block.ID, WeakSubjectivityDepth, ShortID(block.Proposer))
			}
			return nil
		}
	}

	// Far-future block flood guard (eclipse/sybil DoS resistance).
	if MaxFutureBlockGap > 0 && block.ID > nextHeight+MaxFutureBlockGap {
		if DebugConsensus || DebugSync {
			fmt.Printf("Dropped far-future block | local=%d got=%d max_gap=%d\n",
				currentHeight, block.ID, MaxFutureBlockGap)
		}
		n.maybeSyncToBestObservedHeight("future_block_gap")
		return newBlockApplyError(block, "future_block_gap", nil)
	}

	if block.ID > nextHeight {
		// Out-of-order block: queue and wait for missing predecessor(s).
		queued := n.QueueFutureBlock(block)
		if queued && (DebugSync || DebugConsensus) {
			fmt.Printf("Queued future block for catch-up | local=%d got=%d\n", currentHeight, block.ID)
		}
		n.maybeRecoverMissingBlock(nextHeight, "future_block_gap")
		// Trigger sync selection only when we learned something new,
		// or when commit appears stalled for a while.
		if queued || n.commitStallDuration() >= execQuorumEmergencyStallTimeout {
			n.maybeSyncToBestObservedHeight("future_block")
		}
		return nil
	}
	if block.ID == 0 || block.BlockHash == "" || block.PrevHash == "" {
		return newBlockApplyError(block, "validation_failed", errors.New("missing_block_fields"))
	}

	if block.ValidatorSetHash != "" {
		// `expectedHash` stores the digest used to identify or verify the related data.
		expectedHash := n.expectedValidatorSetHash(block.ID)
		if expectedHash != "" && block.ValidatorSetHash != expectedHash {
			if n.shouldTreatValidatorSetMismatchAsPeerDrift(block.ID, expectedHash, block.ValidatorSetHash) {
				n.handlePersistentPeerDrift(currentHeight, block.ID, expectedHash, block.ValidatorSetHash, "validator-set-hash-mismatch-autoheal")
				return nil
			}
			if n.maybeAdoptCoreQuorumValidatorSetHash(block.ID, expectedHash, block.ValidatorSetHash) {
				expectedHash = n.expectedValidatorSetHash(block.ID)
			}
			if n.tryRepairValidatorSetHash(block.ID, block.ValidatorSetHash) {
				expectedHash = n.expectedValidatorSetHash(block.ID)
			}
		}
		if expectedHash != "" && block.ValidatorSetHash != expectedHash {
			if DebugConsensus {
				fmt.Printf("Validator set hash mismatch at height %d | expected=%s got=%s\n",
					block.ID, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash))
			}
			// `applyErr` stores the error produced by this operation.
			applyErr, _ := rejectBlock("validator_set_hash_mismatch", nil)
			if n.recordValidatorSetMismatchWithLocal(currentHeight, block.ID, expectedHash, block.ValidatorSetHash) {
				n.requestConsensusRecomputePause(block.ID, "validator_set_hash_mismatch")
				if shouldForceSnapshotResyncForValidatorSetMismatch(currentHeight, block.ID) {
					n.forceSnapshotResyncNow(block.ID, "validator-set-hash-mismatch-autoheal")
				} else {
					n.maybeForceNearTipValidatorSetResync(currentHeight, block.ID, "validator-set-hash-mismatch-autoheal")
				}
			}
			// Keep forward progress even when force-resync is skipped near tip.
			n.maybeSyncToBestObservedHeight("validator_set_mismatch")
			return applyErr
		}
	}
	// Surface deterministic next-validator-set commitment drift before deeper
	// envelope checks such as logical time. This is intentionally gated on exact
	// parent continuity so malformed/forked blocks still report the parent
	// mismatch reason from the normal verification path.
	if validatorSetCommitmentV2EnabledAt(block.ID) {
		// `lastForCommitmentPrecheck` stores the value produced by this operation.
		lastForCommitmentPrecheck := bc.LastBlock()
		if lastForCommitmentPrecheck.ID+1 == block.ID &&
			strings.TrimSpace(lastForCommitmentPrecheck.BlockHash) != "" &&
			strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(lastForCommitmentPrecheck.BlockHash)) {
			if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
				// `applyErr` stores the error produced by this operation.
				applyErr, _ := rejectBlock(err.Error(), err)
				if err.Error() == "next_validator_set_hash_mismatch" || err.Error() == "next_validator_set_root_mismatch" {
					n.handleNextValidatorSetCommitmentMismatch(currentHeight, block, err.Error())
				}
				return applyErr
			}
			if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
				// `applyErr` stores the error produced by this operation.
				applyErr, _ := rejectBlock(err.Error(), err)
				if err.Error() == "next_validator_set_hash_mismatch" || err.Error() == "next_validator_set_root_mismatch" {
					n.handleNextValidatorSetCommitmentMismatch(currentHeight, block, err.Error())
				}
				return applyErr
			}
		}
	}

	// `syncContinuityFallback` stores the value produced by this operation.
	syncContinuityFallback := false
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	// `ok` stores whether the related condition is satisfied.
	if len(validators) == 0 {
		// `fallback` stores the value produced by this operation.
		if fallback := n.syncContinuityValidatorFallback(block); len(fallback) > 0 {
			validators = fallback
			syncContinuityFallback = true
			if DebugConsensus || DebugSync {
				fmt.Printf("[SYNC-VALIDATOR-FALLBACK] height=%d source=queued_child_continuity validators=%d hash=%s\n",
					block.ID, len(validators), ShortHash(block.ValidatorSetHash))
			}
		} else {
			// `applyErr` stores the error produced by this operation.
			applyErr, _ := rejectBlock("validator_set_unresolved", nil)
			n.maybeSyncToBestObservedHeight("validator_set_unresolved")
			return applyErr
		}
	} else if _, ok := n.queuedChildExtendsBlockDuringSync(block); ok {
		syncContinuityFallback = true
		if DebugConsensus || DebugSync {
			fmt.Printf("[SYNC-LEADER-FALLBACK] height=%d source=queued_child_continuity validators=%d hash=%s\n",
				block.ID, len(validators), ShortHash(block.ValidatorSetHash))
		}
	}
	// `expectedLeader` stores the value produced by this operation.
	expectedLeader := n.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	// `gotProposer` stores the value produced by this operation.
	gotProposer := normalizeValidatorID(block.Proposer)
	// `wantProposer` stores the value produced by this operation.
	wantProposer := normalizeValidatorID(expectedLeader)
	if !syncContinuityFallback && wantProposer != "" && gotProposer != wantProposer && n.syncExecutionResultQuorumFallback(block, validators) {
		syncContinuityFallback = true
		if DebugConsensus || DebugSync {
			fmt.Printf("[SYNC-LEADER-FALLBACK] height=%d source=execution_result_quorum validators=%d hash=%s\n",
				block.ID, len(validators), ShortHash(block.ValidatorSetHash))
		}
	}
	if !syncContinuityFallback && wantProposer != "" && gotProposer != wantProposer {
		if !n.shouldCountInvalidProposerEvidence(block.ID, block.Round, wantProposer, gotProposer, block.BlockHash) {
			return nil
		}
		// `count` and `shouldPause` store the measured quantity used by this operation.
		count, shouldPause := n.invalidProposerEvent(block.ID, wantProposer, gotProposer)
		if DebugConsensus && (count <= 3 || count%25 == 0) {
			fmt.Printf("Invalid proposer at height %d | round=%d expected=%s got=%s seen=%d block=%s\n",
				block.ID, block.Round, ShortID(wantProposer), ShortID(gotProposer), count, ShortHash(block.BlockHash))
		}
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock("invalid_proposer", nil)
		if shouldPause {
			n.requestConsensusRecomputePause(block.ID, "invalid_proposer")
		}
		n.handleInvalidProposerPolicy("", block.ID, wantProposer, gotProposer)
		// Production safety: invalid proposer is treated as a soft liveness
		// fault. Avoid force-snapshot loops; rely on normal sync selection.
		n.maybeSyncToBestObservedHeight("invalid_proposer")
		return applyErr
	}

	// Strict finality: only one block hash per height is allowed once committed.
	if n.hasCommittedDifferentHash(block.ID, block.BlockHash) {
		n.recordFinalizedHashConflictEvidence(block.ID, block.Round, "", block.BlockHash, "committed_hash_precheck")
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock("committed_different_hash", nil)
		return applyErr
	}

	// `seenKey` stores the key used to access the related value.
	seenKey := blockSeenKey(block)
	if seenKey == "" {
		seenKey = block.BlockHash
	}
	if n.markBlockSeen(seenKey) {
		if block.ID == nextHeight {
			// A queued/gossiped proposal can mark the exact next block as seen
			// before finality applies it. Treating that marker as final silently
			// traps the node one block behind; verification below is still the
			// authority for whether this body may advance the chain.
			n.unmarkBlockSeen(seenKey)
		} else {
			return nil
		}
	}
	// `keepSeen` stores the value produced by this operation.
	keepSeen := false
	defer func() {
		if keepSeen {
			return
		}
		n.unmarkBlockSeen(seenKey)
	}()

	// `err` stores the error produced by this operation.
	if err := n.VerifyBlock(block, bc); err != nil {
		// `applyErr` and `failCount` store the error produced by this operation.
		applyErr, failCount := rejectBlock(err.Error(), err)
		if err.Error() == "prev_hash_mismatch" {
			// `expectedPrev` stores the value produced by this operation.
			expectedPrev := ""
			// `last` stores the value produced by this operation.
			if last := bc.LastBlock(); last.ID == currentHeight {
				expectedPrev = ShortHash(last.BlockHash)
			}
			fmt.Printf("[CHAIN-DIVERGENCE] stage=%s provider=%s local=%d next=%d got=%d reason=prev_hash_mismatch expected_prev=%s got_prev=%s fail_count=%d proposer=%s hash=%s\n",
				syncStage,
				ShortID(syncProvider),
				currentHeight,
				nextHeight,
				block.ID,
				expectedPrev,
				ShortHash(block.PrevHash),
				failCount,
				ShortID(block.Proposer),
				ShortHash(block.BlockHash),
			)
			if strings.TrimSpace(syncProvider) != "" {
				n.setSyncAvoidProviderOnce(syncProvider)
			}
			n.maybeSyncToBestObservedHeight("prev_hash_mismatch")
		}
		if err.Error() == "invalid_block_signature" {
			// `queued` stores the value produced by this operation.
			queued := false
			if block.ID == nextHeight {
				queued = n.QueueFutureBlock(block)
			}
			// `queueTip` stores the value produced by this operation.
			queueTip := n.maxQueuedFutureHeight(currentHeight)
			if queueTip < block.ID && block.ID > currentHeight {
				queueTip = block.ID
			}
			if queueTip > currentHeight {
				n.handleQueuedSyncStall("invalid_block_signature", currentHeight, queueTip)
			} else {
				n.maybeSyncToBestObservedHeight("invalid_block_signature")
			}
			if DebugConsensus && queued {
				fmt.Printf("Queued signature-failed block for retry | local=%d got=%d proposer=%s hash=%s\n",
					currentHeight, block.ID, ShortID(block.Proposer), ShortHash(block.BlockHash))
			}
		}
		if err.Error() == "next_validator_set_hash_mismatch" || err.Error() == "next_validator_set_root_mismatch" {
			n.handleNextValidatorSetCommitmentMismatch(currentHeight, block, err.Error())
		}
		return applyErr
	}

	// Pause consensus while applying finalized state.
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		if !n.Consensus.Syncing {
			n.Consensus.Paused = true
		}
		n.Consensus.mu.Unlock()
	}
	// Capture committed-source registry snapshot before block execution mutates
	// runtime validator registry state.
	preCommitRegistrySnapshot, preCommitRegistrySource, err := n.deterministicPreCommitRegistrySnapshot(block)
	if err != nil {
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock(err.Error(), err)
		logSyncCommitPhase("process_block_incomplete")
		return applyErr
	}
	if DebugConsensus {
		// `headerRegistryHash` stores the block data handled by this operation.
		headerRegistryHash := strings.TrimSpace(block.ValidatorRegistryHash)
		// `resolvedRegistryHash` stores the digest used to identify or verify the related data.
		resolvedRegistryHash := headerRegistryHash
		if resolvedRegistryHash == "" {
			resolvedRegistryHash = strings.TrimSpace(deterministicRegistryHash(preCommitRegistrySnapshot))
		}
		fmt.Printf("[REGISTRY-PRECOMMIT] height=%d source=%s header=%s resolved=%s validators=%d\n",
			block.ID,
			preCommitRegistrySource,
			ShortHash(headerRegistryHash),
			ShortHash(resolvedRegistryHash),
			len(preCommitRegistrySnapshot),
		)
	}
	logSyncCommitPhase("supply_invariant_precheck_begin")
	// `executionLedgerForSupply` stores the deterministic execution-state ledger
	// that post-block rewards will extend if this block is committed.
	executionLedgerForSupply, _, ok := n.executionLedgerForBlock(block)
	if !ok {
		applyErr, _ := rejectBlock("supply_invariant_execution_unavailable", nil)
		logSyncCommitPhase("supply_invariant_precheck_failed")
		return applyErr
	}
	if _, _, err := n.applyPostBlockEffectsToLedgerWithRegistryChecked(
		block,
		executionLedgerForSupply,
		preCommitRegistrySnapshot,
	); err != nil {
		applyErr, _ := rejectBlock("supply_invariant_failed", err)
		logSyncCommitPhase("supply_invariant_precheck_failed")
		return applyErr
	}
	logSyncCommitPhase("supply_invariant_precheck_done")

	logSyncCommitPhase("process_block_begin")
	// `err` stores the error produced by this operation.
	if err := n.ProcessBlock(block, bc); err != nil {
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock(err.Error(), err)
		logSyncCommitPhase("process_block_incomplete")
		return applyErr
	}
	if bc.Height() != block.ID {
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock("process_block_incomplete", nil)
		logSyncCommitPhase("process_block_incomplete")
		return applyErr
	}
	keepSeen = true
	n.clearSyncApplyFailure(block.ID, block.BlockHash)
	logSyncCommitPhase("process_block_committed")
	// `err` stores the error produced by this operation.
	if err := n.persistFinalizedHashInvariant(block); err != nil {
		n.recordFinalizedHashConflictEvidence(block.ID, block.Round, "", block.BlockHash, "persistent_invariant")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "finalized_hash_conflict",
			Reason:    "persistent_invariant",
			Height:    block.ID,
			Round:     block.Round,
			BlockHash: block.BlockHash,
			Fields: map[string]interface{}{
				"error": err.Error(),
			},
		})
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock("finalized_hash_conflict", err)
		logSyncCommitPhase("finalized_hash_conflict")
		return applyErr
	}
	// `err` stores the error produced by this operation.
	if err := n.persistFinalityCheckpoint(block); err != nil {
		n.recordFinalizedHashConflictEvidence(block.ID, block.Round, "", block.BlockHash, "finality_checkpoint")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "finality_checkpoint_conflict",
			Reason:    "finality_checkpoint",
			Height:    block.ID,
			Round:     block.Round,
			BlockHash: block.BlockHash,
			Fields: map[string]interface{}{
				"error": err.Error(),
			},
		})
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock("finality_checkpoint_conflict", err)
		logSyncCommitPhase("finality_checkpoint_conflict")
		return applyErr
	}
	// Capture the execution-stage snapshot immediately after deterministic apply.
	// The live authoritative ledger may continue with post-commit effects, but
	// the committed state_root anchor must remain fixed to this snapshot.
	executionSnapshotLedger := n.Ledger.Clone()
	if ledgerHasInitializedBacking(n.ExecutionLedger) {
		executionSnapshotLedger = n.ExecutionLedger.Clone()
	}
	n.cacheExecutionSnapshotLedger(block.ID, executionSnapshotLedger)
	n.markLiveCommittedExecutionSnapshotReadyHeight(block.ID)

	n.commitMu.Lock()
	if n.committed == nil {
		n.committed = make(map[uint64]string)
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := n.committed[block.ID]; ok && existing != block.BlockHash {
		n.commitMu.Unlock()
		// `err` stores the error produced by this operation.
		err := fmt.Errorf("finalized hash immutable violation height=%d existing=%s got=%s", block.ID, existing, block.BlockHash)
		n.recordFinalizedHashConflictEvidence(block.ID, block.Round, existing, block.BlockHash, "memory_invariant")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "finalized_hash_conflict",
			Reason:    "memory_invariant",
			Height:    block.ID,
			Round:     block.Round,
			BlockHash: block.BlockHash,
			Fields: map[string]interface{}{
				"existing": existing,
			},
		})
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock("finalized_hash_conflict", err)
		return applyErr
	}
	n.committed[block.ID] = block.BlockHash
	if block.ID > n.committedHeight {
		n.committedHeight = block.ID
		n.lastCommitHeight = block.ID
		n.lastCommitAt = time.Now()
	} else if n.lastCommitAt.IsZero() {
		n.lastCommitHeight = n.committedHeight
		n.lastCommitAt = time.Now()
	}
	n.commitMu.Unlock()
	n.emitConsensusTelemetry(consensusTelemetryEvent{
		Type:                "block_finalized",
		Reason:              "commit_barrier",
		Height:              block.ID,
		Round:               block.Round,
		BlockHash:           block.BlockHash,
		ConsensusMode:       block.ConsensusMode,
		QuorumPolicyVersion: block.QuorumPolicyVersion,
		Required:            block.RequiredQuorum,
		ActiveReady:         block.ActiveReadyCount,
	})
	n.maybeFinalizeValidatorAutohealSuccess(block.ID)
	n.markConsensusCommittedHeight(block.ID)
	n.requestHeartbeatBroadcast(true)

	// Rewards and post-block effects must run only after the block is finalized
	// (committed barrier crossed on this node).
	logSyncCommitPhase("post_block_effects_begin")
	n.Consensus.FinalizeHeight(block.ID)
	// Keep consensus-critical state transitions inline, then hand off the
	// persistence/cleanup tail so commit can release sooner.
	postCommitLedger := n.applyPostBlockEffectsWithRegistry(block, preCommitRegistrySnapshot)
	n.cachePostCommitLedger(block.ID, postCommitLedger)
	n.runPostBlockEffectsAsync(block, postCommitLedger)
	logSyncCommitPhase("post_block_effects_done")

	// Persist committed registry snapshot from the captured pre-commit source.
	logSyncCommitPhase("persist_registry_begin")
	// `err` stores the error produced by this operation.
	if err := n.deterministicPersistRegistrySnapshot(block.ID, preCommitRegistrySnapshot, strings.TrimSpace(block.ValidatorRegistryHash)); err != nil {
		// `applyErr` stores the error produced by this operation.
		applyErr, _ := rejectBlock(err.Error(), err)
		logSyncCommitPhase("persist_registry_failed")
		return applyErr
	}
	// `registry`, `source`, and `ok` store whether the related condition is satisfied.
	registry, _, source, ok := n.resolveCommittedValidatorRegistrySnapshot(block.ID)
	if !ok && n.Blockchain != nil && n.Blockchain.Height() == block.ID {
		// Tip-only one-shot retry: allows immediate backfill when the committed
		// write landed late but runtime state is hash-consistent with the block.
		registry, _, source, ok = n.resolveCommittedValidatorRegistrySnapshot(block.ID)
	}
	if ok && (source == "live_tip_runtime_repair" || source == "tip_snapshot_repair") {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("registry_repair_applied:%d", block.ID)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[REGISTRY-REPAIR] height=%d source=%s validators=%d", block.ID, source, len(registry))
		}
	} else if !ok {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("registry_repair_missing:%d", block.ID)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[WARN] registry snapshot unresolved after commit height=%d", block.ID)
		}
	}
	logSyncCommitPhase("persist_registry_done")
	logSyncCommitPhase("apply_validator_updates_begin")
	n.applyValidatorUpdatesFromBlock(block)
	logSyncCommitPhase("apply_validator_updates_done")
	logSyncCommitPhase("observe_candidates_begin")
	n.observeCandidatesOnCommit(block)
	logSyncCommitPhase("observe_candidates_done")
	logSyncCommitPhase("apply_validator_update_txs_begin")
	n.applyValidatorUpdateTransactionsFromBlock(block)
	logSyncCommitPhase("apply_validator_update_txs_done")
	logSyncCommitPhase("apply_scheduled_updates_begin")
	n.applyScheduledValidatorUpdates(block.ID)
	logSyncCommitPhase("apply_scheduled_updates_done")
	if protocolDynamicValidatorSelectionEnabledFlag() {
		logSyncCommitPhase("snapshot_epoch_validators_begin")
		n.snapshotEpochValidators(block.ID + 1)
		logSyncCommitPhase("snapshot_epoch_validators_done")
	}
	logSyncCommitPhase("start_next_round_begin")
	n.startNextRoundImmediatelyWithReason(block.ID+1, postCommitLedger, "post_commit")
	logSyncCommitPhase("start_next_round_done")

	logSyncCommitPhase("clear_exec_results_begin")
	n.execResultsMu.Lock()
	// `key` tracks the key used to access the related value.
	for key := range n.execResults {
		// `h` and `ok` store whether the related condition is satisfied.
		if h, ok := parseHeightPrefix(key); ok && h == block.ID {
			delete(n.execResults, key)
		}
	}
	if n.execBroadcasted != nil {
		delete(n.execBroadcasted, block.ID)
	}
	if n.execSignerSeen != nil {
		delete(n.execSignerSeen, block.ID)
	}
	if n.execBroadcastedByValidator != nil {
		delete(n.execBroadcastedByValidator, block.ID)
	}
	if n.execRebroadcastAt != nil {
		delete(n.execRebroadcastAt, block.ID)
	}
	if n.execRebroadcastState != nil {
		delete(n.execRebroadcastState, block.ID)
	}
	if n.localExecVoteByRound != nil {
		delete(n.localExecVoteByRound, block.ID)
	}
	if n.acceptedProposal != nil {
		delete(n.acceptedProposal, acceptedProposalHeightKey(block.ID))
	}
	if n.acceptedProposalBlocks != nil {
		// `key` and `accepted` track the key used to access the related value.
		for key, accepted := range n.acceptedProposalBlocks {
			if accepted.ID == block.ID {
				delete(n.acceptedProposalBlocks, key)
			}
		}
	}
	n.execResultsMu.Unlock()
	logSyncCommitPhase("clear_exec_results_done")

	if ResultGossipOnly {
		n.Mempool.RemoveIncluded(block.Transactions)
	}

	logSyncCommitPhase("update_validator_metrics_begin")
	n.updateValidatorMetricsFromBlockWithRegistry(block, preCommitRegistrySnapshot)
	logSyncCommitPhase("update_validator_metrics_done")
	logSyncCommitPhase("enter_safe_mode_begin")
	if deferMaintenance {
		logSyncCommitPhase("enter_safe_mode_deferred")
		logSyncCommitPhase("enter_safe_mode_done")
	} else {
		n.enterPostBlockSafeModeAsync(block.ID + 1)
		logSyncCommitPhase("enter_safe_mode_done")
	}
	logSyncCommitPhase("ensure_tip_snapshot_begin")
	// `snapSource` and `snapOK` store whether the related condition is satisfied.
	if deferMaintenance {
		logSyncCommitPhase("ensure_tip_snapshot_deferred")
	} else if snapSource, snapOK := n.ensureCommittedTipStateSnapshot(block.ID, "post_commit"); !snapOK {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_materialize_missing:%d", block.ID)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[WARN] committed snapshot unresolved after commit height=%d", block.ID)
		}
	} else if snapSource == "tip_create_snapshot_repair" {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_repair_applied:%d", block.ID)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-REPAIR] height=%d source=%s", block.ID, snapSource)
		}
	}
	logSyncCommitPhase("ensure_tip_snapshot_done")
	if deferMaintenance {
		logSyncCommitPhase("prune_snapshots_deferred")
	} else {
		n.runStorageManagerAfterFinalizedEpoch(block, "post_commit_finalized_epoch")
		logSyncCommitPhase("prune_snapshots_done")
	}
	if normalizeNodeRole(n.Role) == "validator" {
		logSyncCommitPhase("publish_required_snapshot_begin")
		if deferMaintenance {
			logSyncCommitPhase("publish_required_snapshot_deferred")
			logSyncCommitPhase("publish_required_snapshot_done")
		} else {
			// `commitHeight` stores the value produced by this operation.
			commitHeight := block.ID
			n.SafeGo(fmt.Sprintf("required_snapshot_publish_%d", commitHeight), func() {
				// `forcePublish` stores the value produced by this operation.
				forcePublish := false
				// `reason` stores the value produced by this operation.
				reason := "validator_commit_required"
				// `adaptiveForce` and `adaptiveReason` store the value produced by this operation.
				if adaptiveForce, adaptiveReason := n.validatorSnapshotAdaptivePublishDecision(commitHeight); adaptiveForce {
					forcePublish = true
					// `trimmed` stores the value produced by this operation.
					if trimmed := strings.TrimSpace(adaptiveReason); trimmed != "" {
						reason = "adaptive_validator_commit_required_" + trimmed
					}
				}
				// `snapshot` and `err` store the error produced by this operation.
				snapshot, err := n.publishRequiredValidatorSnapshot(reason, forcePublish)
				if err != nil {
					validatorID := n.localConsensusValidatorIDForHeight(commitHeight)
					// `key` stores the key used to access the related value.
					key := fmt.Sprintf("snapshot_publish_required_commit:%s:%d", validatorID, commitHeight)
					if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
						log.Printf("[SNAPSHOT-PUBLISH-REQUIRED] validator=%s height=%d error=%v", validatorID, commitHeight, err)
					}
				} else if snapshot != nil && (DebugConsensus || DebugSync) {
					fmt.Printf("[SNAPSHOT-PUBLISH] height=%d validator=%s hash=%s reason=%s\n",
						snapshot.Height,
						n.localConsensusValidatorIDForHeight(snapshot.Height),
						ShortHash(snapshot.SnapshotHash),
						reason,
					)
				}
			})
			logSyncCommitPhase("publish_required_snapshot_done")
		}
	}
	if DebugConsensus {
		fmt.Printf("[COMMIT] node=%s h=%d hash=%s by=%s type=%s txs=%d\n",
			ShortID(n.ID),
			block.ID,
			ShortHash(block.BlockHash),
			ShortID(block.Proposer),
			block.Type.String(),
			len(block.Transactions))
	}
	logSyncCommitPhase("commit_log_done")

	// Refresh liveness for executors on commit
	logSyncCommitPhase("touch_signers_begin")
	// `res` tracks the result produced by this operation.
	for _, res := range block.ExecutionResults {
		n.touchValidator(res.Signer, block.ID)
	}
	logSyncCommitPhase("touch_signers_done")

	if block.Type == BlockTypeWork {
		// `tx` tracks the transaction data handled by this operation.
		for _, tx := range HighFeePendingTxs(block, n.Mempool.Transactions) {
			// `ev` stores the value produced by this operation.
			ev := n.BuildCensorshipEvidence(block, tx)
			if ApplyCensorshipEvidence(n, ev, block) {
				CheckCensorshipSlashing(ev.Leader, int(ev.Height))
				n.GossipCensorship(ev)
			}
		}
	}

	// If catch-up already reached target, exit sync gate so normal gossip can resume.
	logSyncCommitPhase("maybe_exit_sync_begin")
	if !n.maybeExitSyncMode("commit") {
		n.schedulePostCommitConsensusDrain(block.ID)
	}
	logSyncCommitPhase("done")
	return nil
}

// snapshotOfferCooldownActive implements the snapshot offer cooldown active helper.
func (n *Node) snapshotOfferCooldownActive(validatorID string, now time.Time, cooldown time.Duration) bool {
	if n == nil || validatorID == "" || cooldown <= 0 {
		return false
	}
	n.snapshotOfferMu.Lock()
	defer n.snapshotOfferMu.Unlock()
	// `lastAt` stores the value produced by this operation.
	if lastAt := n.snapshotOfferSentAt[validatorID]; !lastAt.IsZero() && now.Sub(lastAt) < cooldown {
		return true
	}
	return false
}

// maybeOfferSnapshotToValidator implements the maybe offer snapshot to validator helper.
func (n *Node) maybeOfferSnapshotToValidator(validatorID string, _ uint64) {
	if validatorID == "" || (n.ConsensusTopic == nil && n.ValidatorTopic == nil) {
		return
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `snapshotOfferReannounceCooldown` defines the constant value used by this package.
	const snapshotOfferReannounceCooldown = 15 * time.Second
	if n.snapshotOfferCooldownActive(validatorID, now, snapshotOfferReannounceCooldown) {
		return
	}

	// `height` stores the value produced by this operation.
	height := n.committedHeight
	if height == 0 {
		height = n.Blockchain.Height()
	}
	if height == 0 {
		return
	}
	// The latest pointer is a direct lookup. A full at-or-below scan is only
	// needed when the pointer is unavailable or ahead of the committed tip.
	snap, err := n.GetLatestSnapshot()
	if err != nil || snap == nil || snap.Height == 0 || snap.Height > height {
		snap, err = n.GetSnapshotAtOrBelow(height)
		if err != nil || snap == nil || snap.Height == 0 {
			return
		}
	}

	n.snapshotOfferMu.Lock()
	if n.snapshotOfferSent == nil {
		n.snapshotOfferSent = make(map[string]uint64)
	}
	if n.snapshotOfferSentAt == nil {
		n.snapshotOfferSentAt = make(map[string]time.Time)
	}
	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.snapshotOfferSent[validatorID]; ok && last >= snap.Height {
		// `lastAt` stores the value produced by this operation.
		if lastAt := n.snapshotOfferSentAt[validatorID]; !lastAt.IsZero() && now.Sub(lastAt) < snapshotOfferReannounceCooldown {
			n.snapshotOfferMu.Unlock()
			return
		}
	}
	if len(n.snapshotOfferSentAt) > 1024 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-5 * time.Minute)
		// `id` and `sentAt` track the current position in the related collection.
		for id, sentAt := range n.snapshotOfferSentAt {
			if sentAt.Before(cutoff) {
				delete(n.snapshotOfferSentAt, id)
				delete(n.snapshotOfferSent, id)
			}
		}
	}
	if len(n.snapshotOfferSentAt) > 2048 {
		n.snapshotOfferSentAt = make(map[string]time.Time)
		n.snapshotOfferSent = make(map[string]uint64)
	}
	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.snapshotOfferSent[validatorID]; ok && last > snap.Height {
		n.snapshotOfferMu.Unlock()
		return
	}
	n.snapshotOfferSent[validatorID] = snap.Height
	n.snapshotOfferSentAt[validatorID] = now
	n.snapshotOfferMu.Unlock()

	// `offer` stores the value produced by this operation.
	offer := SnapshotOffer{
		From:      n.ID,
		To:        validatorID,
		Height:    snap.Height,
		BlockHash: snap.BlockHash,
		StateRoot: snap.StateRoot,
	}
	// `raw` stores the value produced by this operation.
	raw, _ := json.Marshal(offer)
	// `msg` stores the value produced by this operation.
	msg, _ := MarshalP2PMessage(Message{Type: MsgSnapshotOffer, Data: raw})
	// `publishTopic` stores the value produced by this operation.
	publishTopic := n.ConsensusTopic
	if publishTopic == nil {
		publishTopic = n.ValidatorTopic
	}
	if publishTopic != nil {
		_ = publishTopic.Publish(context.Background(), msg)
	}
}

// handleSnapshotOffer handles snapshot offer.
func (n *Node) handleSnapshotOffer(offer SnapshotOffer) {
	if offer.Height == 0 {
		return
	}
	if offer.To != "" && offer.To != n.ID {
		return
	}
	// Dedupe repeated identical offers to avoid sync storms.
	if !n.shouldProcessSnapshotOffer(offer.From, offer.Height) {
		return
	}
	n.maybeSnapshotSyncToHeight(offer.Height, "snapshot_offer", 0, 0)
}

// getFinalizedHeight returns the best-known finalized height based on supermajority.
// Falls back to committed height if no supermajority has been observed yet.
func (n *Node) getFinalizedHeight() uint64 {
	n.commitMu.Lock()
	// `localFinalized` stores the value produced by this operation.
	localFinalized := n.finalizedHeight
	// `committed` stores the value produced by this operation.
	committed := n.committedHeight
	n.commitMu.Unlock()
	if localFinalized == 0 {
		return committed
	}
	if committed > localFinalized {
		return committed
	}
	return localFinalized
}

// recomputeFinalizedHeight updates the supermajority-finalized height monotonically.
func (n *Node) recomputeFinalizedHeight() {
	// `bestHeight` and `ok` store whether the related condition is satisfied.
	bestHeight, _, _, ok := n.majorityHeartbeatHeight()
	if !ok || bestHeight == 0 {
		return
	}
	n.commitMu.Lock()
	if bestHeight > n.finalizedHeight {
		n.finalizedHeight = bestHeight
	}
	n.commitMu.Unlock()
}

// recordHeightReport implements the record height report helper.
func (n *Node) recordHeightReport(validatorID string, height uint64) {
	validatorID = normalizeValidatorID(validatorID)
	if validatorID == "" || height == 0 {
		return
	}
	if !n.isValidatorInSet(validatorID) {
		return
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.heightReportMu.Lock()
	defer n.heightReportMu.Unlock()

	if n.heightReports == nil {
		n.heightReports = make(map[uint64]map[string]time.Time)
	}
	if n.validatorReportHeight == nil {
		n.validatorReportHeight = make(map[string]uint64)
	}

	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.validatorReportHeight[validatorID]; ok && prev != height {
		// `m` and `ok` store whether the related condition is satisfied.
		if m, ok := n.heightReports[prev]; ok {
			delete(m, validatorID)
			if len(m) == 0 {
				delete(n.heightReports, prev)
			}
		}
	}

	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.heightReports[height]; !ok {
		n.heightReports[height] = make(map[string]time.Time)
	}
	n.heightReports[height][validatorID] = now
	n.validatorReportHeight[validatorID] = height
}

// hardResetConsensus implements the hard reset consensus helper.
func (n *Node) hardResetConsensus(nextHeight uint64) {
	if n.Consensus == nil {
		return
	}
	// `cs` stores the value produced by this operation.
	cs := n.Consensus
	cs.mu.Lock()
	cs.Height = nextHeight
	cs.Round = 0
	if ResultGossipOnly {
		cs.RoundStart = time.Time{}
	} else {
		cs.RoundStart = time.Now()
	}
	cs.clearActiveExecutionViewLocked()
	cs.LockedBlock = ""
	cs.LockedBlockHash = ""
	cs.LockedRound = 0
	cs.LastProposedHeight = 0
	cs.LastProposedRound = 0
	cs.LastFinalized = nextHeight - 1
	cs.Proposals = make(map[uint64]Block)
	cs.mu.Unlock()
}

// resetTransientStateForRecovery implements the reset transient state for recovery helper.
func (n *Node) resetTransientStateForRecovery(height uint64) {
	n.seenBlockMu.Lock()
	n.SeenBlockHashes = make(map[string]bool)
	n.seenBlockQueue = nil
	n.seenBlockHead = 0
	n.seenBlockMu.Unlock()

	n.seenTxMu.Lock()
	n.SeenTxIDs = make(map[string]bool)
	n.seenTxQueue = nil
	n.seenTxHead = 0
	n.seenTxMu.Unlock()

	n.forkMu.Lock()
	n.ForkBlocks = make(map[uint64][]Block)
	n.forkMu.Unlock()

	n.execResultsMu.Lock()
	n.execResults = make(map[string]map[string]ExecutionResult)
	n.pendingBlocks = make(map[string]Block)
	n.queuedExecVotes = make(map[string][]ExecutionResultMsg)
	n.acceptedProposal = make(map[string]string)
	n.acceptedProposalBlocks = make(map[string]Block)
	n.execBroadcasted = make(map[uint64]map[string]bool)
	n.execSignerSeen = make(map[uint64]map[string]map[string]bool)
	n.execBroadcastedByValidator = make(map[uint64]map[string]map[string]bool)
	n.localExecVoteByRound = make(map[uint64]map[uint32]string)
	n.execVoteSeen = make(map[string]time.Time)
	n.execVoteLimiter = make(map[string]*rate.Limiter)
	n.execMismatch = make(map[string]ExecMismatchTracker)
	n.execResultsMu.Unlock()

	n.execRebroadcastMu.Lock()
	n.execRebroadcastAt = make(map[uint64]time.Time)
	n.execRebroadcastMu.Unlock()

	n.leaderMu.Lock()
	n.leaderBlocks = make(map[uint64]Block)
	n.queuedFutureLeaderBlocks = make(map[uint64][]Block)
	n.lastLeaderEpoch = 0
	n.lastLeaderRound = 0
	n.lastLeaderSlot = 0
	n.leaderMu.Unlock()

	n.invalidProposerMu.Lock()
	n.invalidProposerSeen = make(map[uint64]map[string]int)
	n.invalidProposerEvidenceSeen = make(map[string]time.Time)
	n.invalidProposerStrikes = make(map[string]ExecMismatchTracker)
	n.invalidProposerPeerStrikes = make(map[string]ExecMismatchTracker)
	n.invalidProposerMu.Unlock()

	n.heightReportMu.Lock()
	n.heightReports = make(map[uint64]map[string]time.Time)
	n.validatorReportHeight = make(map[string]uint64)
	n.heightReportMu.Unlock()

	n.validatorMu.Lock()
	n.validatorStatus = make(map[string]*ValidatorStatus)
	n.validatorMu.Unlock()

	n.candidateMu.Lock()
	n.candidates = make(map[string]*CandidateStatus)
	n.promotionWindowIdx = 0
	n.promotionsInWindow = 0
	n.candidateMu.Unlock()

	n.validatorSetMu.Lock()
	n.pendingValidators = make(map[string]uint64)
	n.pendingValidatorRemovals = make(map[string]uint64)
	n.epochValidators = make(map[uint64][]string)
	n.frozenValidatorsByHeight = make(map[uint64][]string)
	n.frozenValidatorHashByHeight = make(map[uint64]string)
	n.committeeByHeight = make(map[uint64][]string)
	n.committeeHashByHeight = make(map[uint64]string)
	n.committeeLiveByHeight = make(map[uint64]map[string]bool)
	n.safeModeUntilByHeight = make(map[uint64]time.Time)
	n.safeModeWindowByHeight = make(map[uint64]time.Duration)
	n.safeModeObservedDelays = make([]time.Duration, 0, postBlockSafeModeHistoryLimit())
	n.eligibleSortedValidators = nil
	n.eligibleIndexVersion = 0
	n.queuedValidatorSetUpdates = make(map[uint64]ValidatorSetUpdate)
	n.validatorSetHeight = height
	n.validatorSetMu.Unlock()
	n.clearImmediateRoundStart(0)
	n.clearPostBlockSafeModeGate(0)

	n.commitMu.Lock()
	n.commitVotes = make(map[uint64]map[string]map[string]struct{})
	n.commitVoted = make(map[uint64]map[string]string)
	n.commitVoteSignatures = make(map[uint64]map[string]map[string]string)
	n.commitMu.Unlock()

	n.Mempool.Clear()
}

// rewindLocalChainToHeight implements the rewind local chain to height helper.
func (n *Node) rewindLocalChainToHeight(height uint64, reason string) bool {
	if n == nil || n.Blockchain == nil {
		return false
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := n.Blockchain.Height()
	if height == 0 || height >= localHeight {
		return false
	}
	if shouldUseSnapshotResyncInsteadOfRewind(localHeight, height) {
		n.scheduleSnapshotResyncForRewindFailure(localHeight, height, reason, fmt.Errorf("unsafe_deep_rewind"))
		return false
	}
	// `anchor` and `ok` store whether the related condition is satisfied.
	anchor, ok := n.Blockchain.GetBlock(height)
	if !ok {
		anchor, ok = n.LoadBlock(int(height))
	}
	if !ok || anchor.ID != height || strings.TrimSpace(anchor.BlockHash) == "" {
		if DebugSync || DebugConsensus {
			fmt.Printf("[SYNC-REWIND] action=skip reason=%s local=%d target=%d detail=anchor_unavailable\n",
				strings.TrimSpace(reason), localHeight, height)
		}
		return false
	}

	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	// `rebuiltBlocks` and `err` store the error produced by this operation.
	rebuiltBlocks, err := n.rebuildLocalChainBlocksForRewind(height, anchor)
	if err != nil {
		log.Printf("[SYNC-REWIND] action=skip reason=%s local=%d target=%d detail=rebuild_failed err=%v",
			strings.TrimSpace(reason), localHeight, height, err)
		n.scheduleSnapshotResyncForRewindFailure(localHeight, height, reason, err)
		return false
	}

	n.Blockchain.mu.Lock()
	n.Blockchain.Blocks = rebuiltBlocks
	n.Blockchain.mu.Unlock()
	n.pruneBlocksAboveHeight(height)
	// `err` stores the error produced by this operation.
	if err := n.pruneFinalizedHashInvariantsAboveHeight(height); err != nil {
		log.Printf("[WARN] finalized hash invariant prune failed height=%d err=%v", height, err)
	}

	n.commitMu.Lock()
	if n.committed == nil {
		n.committed = make(map[uint64]string)
	}
	// `h` tracks the current values while iterating.
	for h := range n.committed {
		if h > height {
			delete(n.committed, h)
		}
	}
	n.committed[height] = anchor.BlockHash
	n.committedHeight = height
	n.finalizedHeight = height
	n.lastCommitHeight = height
	n.lastCommitAt = time.Now()
	n.commitVotes = make(map[uint64]map[string]map[string]struct{})
	n.commitVoted = make(map[uint64]map[string]string)
	n.commitVoteSignatures = make(map[uint64]map[string]map[string]string)
	n.commitMu.Unlock()

	if !n.restoreLedgersFromAuthoritativeExecution(height, "rewind_"+strings.TrimSpace(reason)) {
		if DebugSync || DebugConsensus {
			fmt.Printf("[SYNC-REWIND] ledger_restore_unavailable height=%d reason=%s\n",
				height, strings.TrimSpace(reason))
		}
	}

	n.forkMu.Lock()
	n.ForkBlocks = make(map[uint64][]Block)
	n.forkMu.Unlock()

	n.seenBlockMu.Lock()
	n.SeenBlockHashes = make(map[string]bool)
	n.seenBlockQueue = nil
	n.seenBlockHead = 0
	n.seenBlockMu.Unlock()

	n.execResultsMu.Lock()
	n.execResults = make(map[string]map[string]ExecutionResult)
	n.pendingBlocks = make(map[string]Block)
	n.queuedExecVotes = make(map[string][]ExecutionResultMsg)
	n.acceptedProposal = make(map[string]string)
	n.acceptedProposalBlocks = make(map[string]Block)
	n.quorumLockedProposal = make(map[string]string)
	n.execBroadcasted = make(map[uint64]map[string]bool)
	n.execSignerSeen = make(map[uint64]map[string]map[string]bool)
	n.execBroadcastedByValidator = make(map[uint64]map[string]map[string]bool)
	n.localExecVoteByRound = make(map[uint64]map[uint32]string)
	n.execVoteSeen = make(map[string]time.Time)
	n.execVoteLimiter = make(map[string]*rate.Limiter)
	n.execResultsMu.Unlock()

	n.leaderMu.Lock()
	n.leaderBlocks = make(map[uint64]Block)
	n.queuedFutureLeaderBlocks = make(map[uint64][]Block)
	n.lastLeaderEpoch = 0
	n.lastLeaderRound = 0
	n.lastLeaderSlot = 0
	n.leaderMu.Unlock()

	n.setLogicalTick(height+1, TickExec)
	n.hardResetConsensus(height + 1)
	n.snapshotEpochValidators(height + 1)
	n.syncFrozenValidatorSetHashesFromChain()
	n.persistConsensusSafetyStateAsync("rewind")
	n.requestHeartbeatBroadcast(true)

	fmt.Printf("[SYNC-REWIND] action=applied reason=%s from=%d to=%d hash=%s\n",
		strings.TrimSpace(reason), localHeight, height, ShortHash(anchor.BlockHash))
	return true
}

// shouldUseSnapshotResyncInsteadOfRewind reports whether a local fork is too
// deep for block replay rewind and should be repaired via trusted snapshot sync.
func shouldUseSnapshotResyncInsteadOfRewind(localHeight, targetHeight uint64) bool {
	if targetHeight == 0 || targetHeight >= localHeight {
		return false
	}
	if targetHeight <= 1 && localHeight > 2 {
		return true
	}
	lag := localHeight - targetHeight
	limit := syncSnapshotDeltaMaxBlocks()
	if limit == 0 {
		limit = 128
	}
	return lag > limit
}

// scheduleSnapshotResyncForRewindFailure escalates sparse-history rewind
// failures to snapshot repair. It only repairs chain/state data; node identity
// and validator keys are outside the snapshot apply path.
func (n *Node) scheduleSnapshotResyncForRewindFailure(localHeight, targetHeight uint64, reason string, rewindErr error) {
	if n == nil || n.Blockchain == nil || localHeight == 0 {
		return
	}
	errText := strings.ToLower(strings.TrimSpace(fmt.Sprint(rewindErr)))
	if errText == "" {
		return
	}
	if !strings.Contains(errText, "missing_block") &&
		!strings.Contains(errText, "prev_hash_mismatch") &&
		!strings.Contains(errText, "unsafe_deep_rewind") {
		return
	}
	resyncTarget := localHeight + 1
	if observedHeight, _ := n.bestObservedSyncHeight(); observedHeight > resyncTarget {
		resyncTarget = observedHeight
	}
	if resyncTarget <= targetHeight {
		resyncTarget = targetHeight + 1
	}
	log.Printf("[SYNC-REWIND] action=snapshot_resync reason=%s local=%d target=%d resync_target=%d err=%s",
		strings.TrimSpace(reason),
		localHeight,
		targetHeight,
		resyncTarget,
		errText,
	)
	if !strings.Contains(strings.ToLower(strings.TrimSpace(reason)), "snapshot_anchor_rewind") &&
		n.tryTrustedSnapshotAnchorRewind(localHeight, reason, errText) {
		return
	}
	go n.forceSnapshotResyncNow(resyncTarget, "rewind_rebuild_prev_hash_mismatch_"+strings.TrimSpace(reason))
}

// tryTrustedSnapshotAnchorRewind rewinds to the latest trusted local snapshot
// anchor below the forked tip. Normal block catch-up can then replay from that
// known-good parent instead of looping on missing early block history.
func (n *Node) tryTrustedSnapshotAnchorRewind(localHeight uint64, reason string, errText string) bool {
	if n == nil || n.Blockchain == nil || localHeight == 0 {
		return false
	}
	snap, err := n.GetSnapshotAtOrBelow(localHeight)
	if err != nil || snap == nil || snap.Height == 0 || snap.Height >= localHeight {
		if localHeight <= 1 {
			return false
		}
		snap, err = n.GetSnapshotAtOrBelow(localHeight - 1)
		if err != nil || snap == nil || snap.Height == 0 || snap.Height >= localHeight {
			return false
		}
	}
	trusted, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(snap.Height)
	if !ok || trusted == nil || strings.TrimSpace(trusted.BlockHash) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(trusted.BlockHash), strings.TrimSpace(snap.BlockHash)) {
		return false
	}
	anchor := snapshotAnchorBlock(*snap)
	if anchor.ID != snap.Height || strings.TrimSpace(anchor.BlockHash) == "" {
		return false
	}
	n.ensureSnapshotAnchorBlockStored(anchor)
	if !n.rewindLocalChainToHeight(snap.Height, "snapshot_anchor_rewind_"+strings.TrimSpace(reason)) {
		if !n.rewindLocalChainToSparseSnapshotAnchor(anchor, "snapshot_anchor_rewind_"+strings.TrimSpace(reason)) {
			return false
		}
	}
	log.Printf("[SYNC-REWIND] action=snapshot_anchor_rewind reason=%s from=%d to=%d err=%s",
		strings.TrimSpace(reason),
		localHeight,
		snap.Height,
		errText,
	)
	return true
}

// rewindLocalChainToSparseSnapshotAnchor rewinds a sparse-history node directly
// to a trusted snapshot anchor when historical block files are unavailable.
func (n *Node) rewindLocalChainToSparseSnapshotAnchor(anchor Block, reason string) bool {
	if n == nil || n.Blockchain == nil || anchor.ID == 0 || strings.TrimSpace(anchor.BlockHash) == "" {
		return false
	}
	localHeight := n.Blockchain.Height()
	if anchor.ID >= localHeight {
		return false
	}

	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	n.Blockchain.mu.Lock()
	n.Blockchain.Blocks = []Block{anchor}
	n.Blockchain.mu.Unlock()
	n.pruneBlocksAboveHeight(anchor.ID)
	if err := n.pruneFinalizedHashInvariantsAboveHeight(anchor.ID); err != nil {
		log.Printf("[WARN] finalized hash invariant prune failed height=%d err=%v", anchor.ID, err)
	}

	n.commitMu.Lock()
	if n.committed == nil {
		n.committed = make(map[uint64]string)
	}
	for h := range n.committed {
		if h > anchor.ID {
			delete(n.committed, h)
		}
	}
	n.committed[anchor.ID] = anchor.BlockHash
	n.committedHeight = anchor.ID
	n.finalizedHeight = anchor.ID
	n.lastCommitHeight = anchor.ID
	n.lastCommitAt = time.Now()
	n.commitVotes = make(map[uint64]map[string]map[string]struct{})
	n.commitVoted = make(map[uint64]map[string]string)
	n.commitVoteSignatures = make(map[uint64]map[string]map[string]string)
	n.commitMu.Unlock()

	if !n.restoreLedgersFromAuthoritativeExecution(anchor.ID, "sparse_"+strings.TrimSpace(reason)) {
		if DebugSync || DebugConsensus {
			fmt.Printf("[SYNC-REWIND] ledger_restore_unavailable height=%d reason=%s\n",
				anchor.ID, strings.TrimSpace(reason))
		}
	}

	n.forkMu.Lock()
	n.ForkBlocks = make(map[uint64][]Block)
	n.forkMu.Unlock()

	n.seenBlockMu.Lock()
	n.SeenBlockHashes = make(map[string]bool)
	n.seenBlockQueue = nil
	n.seenBlockHead = 0
	n.seenBlockMu.Unlock()

	n.execResultsMu.Lock()
	n.execResults = make(map[string]map[string]ExecutionResult)
	n.pendingBlocks = make(map[string]Block)
	n.queuedExecVotes = make(map[string][]ExecutionResultMsg)
	n.acceptedProposal = make(map[string]string)
	n.acceptedProposalBlocks = make(map[string]Block)
	n.quorumLockedProposal = make(map[string]string)
	n.execBroadcasted = make(map[uint64]map[string]bool)
	n.execSignerSeen = make(map[uint64]map[string]map[string]bool)
	n.execBroadcastedByValidator = make(map[uint64]map[string]map[string]bool)
	n.localExecVoteByRound = make(map[uint64]map[uint32]string)
	n.execVoteSeen = make(map[string]time.Time)
	n.execVoteLimiter = make(map[string]*rate.Limiter)
	n.execResultsMu.Unlock()

	n.leaderMu.Lock()
	n.leaderBlocks = make(map[uint64]Block)
	n.queuedFutureLeaderBlocks = make(map[uint64][]Block)
	n.lastLeaderEpoch = 0
	n.lastLeaderRound = 0
	n.lastLeaderSlot = 0
	n.leaderMu.Unlock()

	n.setLogicalTick(anchor.ID+1, TickExec)
	n.hardResetConsensus(anchor.ID + 1)
	n.snapshotEpochValidators(anchor.ID + 1)
	n.syncFrozenValidatorSetHashesFromChain()
	n.persistConsensusSafetyStateAsync("sparse_snapshot_anchor_rewind")
	n.requestHeartbeatBroadcast(true)

	fmt.Printf("[SYNC-REWIND] action=applied_sparse_anchor reason=%s from=%d to=%d hash=%s\n",
		strings.TrimSpace(reason), localHeight, anchor.ID, ShortHash(anchor.BlockHash))
	return true
}

// rebuildLocalChainBlocksForRewind implements the rebuild local chain blocks for rewind helper.
func (n *Node) rebuildLocalChainBlocksForRewind(height uint64, anchor Block) ([]Block, error) {
	if n == nil || n.Blockchain == nil {
		return nil, fmt.Errorf("node_or_blockchain_unavailable")
	}
	if height == 0 || anchor.ID != height || strings.TrimSpace(anchor.BlockHash) == "" {
		return nil, fmt.Errorf("invalid_anchor height=%d anchor=%d", height, anchor.ID)
	}

	n.Blockchain.mu.RLock()
	// `existing` stores the value produced by this operation.
	existing := append([]Block(nil), n.Blockchain.Blocks...)
	n.Blockchain.mu.RUnlock()

	// `existingByHeight` stores the value produced by this operation.
	existingByHeight := make(map[uint64]Block, len(existing)+1)
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range existing {
		if block.ID == 0 {
			existingByHeight[0] = block
			continue
		}
		if block.ID > 0 && block.ID <= height && strings.TrimSpace(block.BlockHash) != "" {
			existingByHeight[block.ID] = block
		}
	}

	// `genesis` and `ok` store whether the related condition is satisfied.
	genesis, ok := existingByHeight[0]
	if !ok {
		genesis = NewBlockchain().Blocks[0]
	}
	if genesis.ID != 0 {
		return nil, fmt.Errorf("invalid_genesis_id=%d", genesis.ID)
	}
	if strings.TrimSpace(genesis.BlockHash) == "" {
		genesis.BlockHash = GenesisHash
	}

	// `rebuilt` stores the value produced by this operation.
	rebuilt := make([]Block, 0, int(height)+1)
	rebuilt = append(rebuilt, genesis)
	// `h` stores the value produced by this operation.
	for h := uint64(1); h <= height; h++ {
		// `block` and `ok` store whether the related condition is satisfied.
		block, ok := n.loadPersistedBlockForRewind(h)
		if !ok && h == height {
			block, ok = anchor, true
		}
		if !ok {
			block, ok = existingByHeight[h]
		}
		if !ok || block.ID != h || strings.TrimSpace(block.BlockHash) == "" {
			if n.canUseSparseSnapshotRewind(height, anchor, existing, h) {
				return []Block{anchor}, nil
			}
			return nil, fmt.Errorf("missing_block_%d", h)
		}
		// `prev` stores the value produced by this operation.
		prev := rebuilt[len(rebuilt)-1]
		if h == 1 && prev.ID == 0 &&
			strings.TrimSpace(block.PrevHash) != "" &&
			strings.TrimSpace(prev.BlockHash) != "" &&
			!strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(prev.BlockHash)) {
			// Rewind reconstruction may run after snapshot bootstrap, where the
			// in-memory genesis is only a process default. Trust persisted block
			// one's parent hash so the contiguous historical chain can be rebuilt.
			rebuilt[0].BlockHash = strings.TrimSpace(block.PrevHash)
			prev = rebuilt[0]
		}
		if strings.TrimSpace(block.PrevHash) != "" &&
			strings.TrimSpace(prev.BlockHash) != "" &&
			!strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(prev.BlockHash)) {
			return nil, fmt.Errorf("prev_hash_mismatch_%d", h)
		}
		rebuilt = append(rebuilt, block)
	}
	return rebuilt, nil
}

// canUseSparseSnapshotRewind implements the can use sparse snapshot rewind helper.
func (n *Node) canUseSparseSnapshotRewind(height uint64, anchor Block, existing []Block, missingHeight uint64) bool {
	if n == nil || height == 0 || missingHeight == 0 || missingHeight >= height {
		return false
	}
	if anchor.ID != height || strings.TrimSpace(anchor.BlockHash) == "" {
		return false
	}
	// `sparse` stores the value produced by this operation.
	sparse := false
	if len(existing) > 0 && existing[0].ID != 0 {
		sparse = true
	}
	// `i` stores the current position in the related collection.
	for i := 1; i < len(existing); i++ {
		if existing[i].ID != existing[i-1].ID+1 {
			sparse = true
			break
		}
	}
	if !sparse {
		return false
	}
	// `snap` and `ok` store whether the related condition is satisfied.
	if snap, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(height); ok && snap != nil {
		return strings.EqualFold(strings.TrimSpace(snap.BlockHash), strings.TrimSpace(anchor.BlockHash))
	}
	if committedHash, ok := n.getCommittedHash(height); ok &&
		strings.EqualFold(strings.TrimSpace(committedHash), strings.TrimSpace(anchor.BlockHash)) &&
		n.getFinalizedHeight() >= height {
		if DebugSync || DebugConsensus {
			fmt.Printf("[SYNC-REWIND] action=sparse_local_anchor_allowed height=%d missing=%d hash=%s source=committed\n",
				height, missingHeight, ShortHash(anchor.BlockHash))
		}
		return true
	}
	if n.hasCommitQuorum(height, anchor.BlockHash) {
		if DebugSync || DebugConsensus {
			fmt.Printf("[SYNC-REWIND] action=sparse_local_anchor_allowed height=%d missing=%d hash=%s source=quorum\n",
				height, missingHeight, ShortHash(anchor.BlockHash))
		}
		return true
	}
	return false
}

// loadPersistedBlockForRewind implements the load persisted block for rewind helper.
func (n *Node) loadPersistedBlockForRewind(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := n.loadBlockFile(height); ok {
		return block, true
	}
	if n.DB == nil || n.DB.Blocks == nil {
		return Block{}, false
	}
	// `block` stores the synchronization state protecting shared data.
	var block Block
	// `err` stores the error produced by this operation.
	err := n.DB.Blocks.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get([]byte(fmt.Sprintf("block:%d", height)))
		if err != nil {
			return err
		}
		return item.Value(func(v []byte) error {
			// `plain` and `derr` store the error produced by this operation.
			plain, derr := decryptDBValue(v)
			if derr != nil {
				return derr
			}
			return json.Unmarshal(plain, &block)
		})
	})
	if err != nil || block.ID != height || strings.TrimSpace(block.BlockHash) == "" {
		return Block{}, false
	}
	return block, true
}

// auditAndRewindInvalidQuorumEvidence implements the audit and rewind invalid quorum evidence helper.
func (n *Node) auditAndRewindInvalidQuorumEvidence(reason string) (uint64, bool) {
	if n == nil || n.Blockchain == nil {
		return 0, false
	}
	n.Blockchain.mu.RLock()
	// `blocks` stores the block data handled by this operation.
	blocks := append([]Block(nil), n.Blockchain.Blocks...)
	n.Blockchain.mu.RUnlock()
	if len(blocks) == 0 {
		return 0, false
	}
	// `lastValidHeight` stores the value produced by this operation.
	lastValidHeight := uint64(0)
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range blocks {
		// `err` stores the error produced by this operation.
		if err := n.validateCommittedBlockQuorumEvidence(block); err != nil {
			if block.ID <= 1 {
				log.Printf("[QUORUM-AUDIT] invalid_genesis_or_anchor height=%d hash=%s err=%v",
					block.ID, ShortHash(block.BlockHash), err)
				return block.ID, false
			}
			// `target` stores the value produced by this operation.
			target := block.ID - 1
			if lastValidHeight > 0 && lastValidHeight < block.ID {
				target = lastValidHeight
			}
			log.Printf("[QUORUM-AUDIT] invalid_finalized_evidence height=%d hash=%s target=%d reason=%s err=%v",
				block.ID, ShortHash(block.BlockHash), target, strings.TrimSpace(reason), err)
			return block.ID, n.rewindLocalChainToHeight(target, "invalid_quorum_evidence_"+strings.TrimSpace(reason))
		}
		if block.ID > lastValidHeight && strings.TrimSpace(block.BlockHash) != "" {
			lastValidHeight = block.ID
		}
	}
	return 0, false
}

// sanitizeContiguousLoadedBlocks implements the sanitize contiguous loaded blocks helper.
func sanitizeContiguousLoadedBlocks(blocks []Block) ([]Block, uint64, error) {
	if len(blocks) == 0 {
		return blocks, 0, nil
	}
	// `first` stores the value produced by this operation.
	first := blocks[0]
	if first.ID > 1 {
		return nil, 0, fmt.Errorf("height_gap_0_to_%d", first.ID)
	}
	// `out` stores the result produced by this operation.
	out := make([]Block, 0, len(blocks))
	out = append(out, first)
	// `i` stores the current position in the related collection.
	for i := 1; i < len(blocks); i++ {
		// `prev` stores the value produced by this operation.
		prev := out[len(out)-1]
		// `block` stores the synchronization state protecting shared data.
		block := blocks[i]
		if block.ID != prev.ID+1 {
			return out, prev.ID, fmt.Errorf("height_gap_%d_to_%d", prev.ID, block.ID)
		}
		if strings.TrimSpace(prev.BlockHash) != "" &&
			strings.TrimSpace(block.PrevHash) != "" &&
			!strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(prev.BlockHash)) {
			return out, prev.ID, fmt.Errorf("prev_hash_mismatch_%d", block.ID)
		}
		out = append(out, block)
	}
	return out, 0, nil
}

// trimConsensusCachesLocked implements the trim consensus caches locked helper.
func (n *Node) trimConsensusCachesLocked(committedHeight uint64) {
	trimMapByHeight(n.execResults, ExecResultsMaxEntries, committedHeight)
	trimMapByHeight(n.pendingBlocks, PendingBlocksMaxEntries, committedHeight)
	trimMapByHeight(n.queuedExecVotes, QueuedExecVotesMaxKeys, committedHeight)
	trimMapByHeight(n.acceptedProposal, AcceptedProposalMaxKeys, committedHeight)
	trimMapByHeight(n.acceptedProposalBlocks, AcceptedProposalMaxKeys, committedHeight)
	trimMapByHeight(n.execVoteSeen, ExecVoteReplayMaxKeys, committedHeight)
	trimEpochBoolMap(n.execBroadcasted, ExecBroadcastedMaxEpoch, committedHeight)
	trimEpochNestedBoolMap(n.execSignerSeen, ExecSignerSeenMaxEpoch, committedHeight)
	trimEpochNestedBoolMap(n.execBroadcastedByValidator, ExecBroadcastedByValMax, committedHeight)
	n.validatorSetMu.Lock()
	n.pruneCommitteeStateLocked(committedHeight + 1)
	n.validatorSetMu.Unlock()
}

// trimExecRebroadcastAt implements the trim exec rebroadcast at helper.
func (n *Node) trimExecRebroadcastAt(committedHeight uint64) {
	n.execRebroadcastMu.Lock()
	trimEpochTimeMap(n.execRebroadcastAt, ExecBroadcastedMaxEpoch, committedHeight)
	n.execRebroadcastMu.Unlock()
}
