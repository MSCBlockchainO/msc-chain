package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

func (n *Node) loadBestReadableSnapshotAtOrBelow(targetHeight uint64) (*StateSnapshot, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), targetHeight)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrKeyNotFound
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].height > keys[j].height })
	var lastErr error
	for _, candidate := range keys {
		snapshot, err := readSnapshotFromStores(n.DB.SnapshotStoresForRead(), candidate.key)
		if err == nil && snapshot != nil {
			return snapshot, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrKeyNotFound
}

func (n *Node) loadBestAnchoredStartupRecoverySnapshot() (*StateSnapshot, Block, string) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, Block{}, "snapshot_db_unavailable"
	}
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return nil, Block{}, err.Error()
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].height > keys[j].height })
	lastReason := "anchored_snapshot_unavailable"
	audited := 0
	auditSkip := func(height uint64, reason string) {
		if audited >= 12 {
			return
		}
		audited++
		log.Printf("[SNAPSHOT-RECOVERY-CANDIDATE] height=%d status=skip reason=%s", height, strings.TrimSpace(reason))
	}
	for _, candidate := range keys {
		snapshot, err := readSnapshotFromStores(n.DB.SnapshotStoresForRead(), candidate.key)
		if err != nil || snapshot == nil {
			if err != nil {
				lastReason = err.Error()
			}
			auditSkip(candidate.height, lastReason)
			continue
		}
		if ok, reason := n.canApplyUnanchoredStartupRecoverySnapshot(snapshot, true); !ok {
			if strings.TrimSpace(reason) != "" {
				lastReason = strings.TrimSpace(reason)
			}
			auditSkip(snapshot.Height, lastReason)
			continue
		}
		anchor, ok := n.loadDurableBlock(snapshot.Height)
		if !ok {
			lastReason = "anchor_block_unavailable"
			auditSkip(snapshot.Height, lastReason)
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(anchor.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			lastReason = "block_hash_mismatch"
			auditSkip(snapshot.Height, lastReason)
			continue
		}
		log.Printf("[SNAPSHOT-RECOVERY-CANDIDATE] height=%d status=selected hash=%s",
			snapshot.Height, ShortHash(snapshot.BlockHash))
		return snapshot, anchor, ""
	}
	return nil, Block{}, lastReason
}

func (n *Node) startupSnapshotMatchesCommittedTip(snapshot *StateSnapshot) (bool, string) {
	if n == nil || snapshot == nil || n.Blockchain == nil {
		return false, "snapshot_metadata_invalid"
	}
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "anchor_verification_failed"
		}
		return false, reason
	}
	if block, ok := n.Blockchain.GetBlock(snapshot.Height); ok {
		expectedRegistry := strings.TrimSpace(block.ValidatorRegistryHash)
		if expectedRegistry != "" && !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)), expectedRegistry) {
			return false, "registry_hash_mismatch"
		}
	}
	return true, ""
}

func (n *Node) applyStartupBestSnapshot(snapshot *StateSnapshot, startupChainTruncated bool) bool {
	if n == nil || snapshot == nil || n.Blockchain == nil {
		return false
	}
	chainHeight := n.Blockchain.Height()
	switch {
	case snapshot.Height > chainHeight:
		if ok, reason := n.canApplyUnanchoredStartupRecoverySnapshot(snapshot, startupChainTruncated); ok {
			log.Printf("[SNAPSHOT-RECOVERY] startup_apply_unanchored height=%d tip=%d source=backup_import",
				snapshot.Height,
				chainHeight,
			)
			return n.ApplySnapshotForRecovery(*snapshot)
		} else if reason != "" {
			log.Printf("[SNAPSHOT-SCRUB] skipped_unanchored_local_snapshot height=%d tip=%d chain_truncated=%t reason=%s",
				snapshot.Height, chainHeight, startupChainTruncated, reason)
			return false
		}
		log.Printf("[SNAPSHOT-SCRUB] skipped_unanchored_local_snapshot height=%d tip=%d chain_truncated=%t",
			snapshot.Height, chainHeight, startupChainTruncated)
		return false
	case snapshot.Height == chainHeight && chainHeight > 0:
		if ok, reason := n.startupSnapshotMatchesCommittedTip(snapshot); !ok {
			log.Printf("[WARN] startup snapshot metadata skipped: height=%d reason=%s snapshot_hash=%s",
				snapshot.Height, reason, ShortHash(snapshot.SnapshotHash))
			return false
		}
		if snapshotHasTrustedExecutionLedger(snapshot) {
			n.setExecutionLedger(snapshot.Ledger)
			n.cacheExecutionSnapshotLedger(snapshot.Height, snapshot.Ledger)
			n.markExecutionSnapshotReadyHeight(snapshot.Height)
		}
		n.applySnapshotValidators(*snapshot)
		n.applySnapshotValidatorTransitions(*snapshot)
		n.applySnapshotValidatorRegistry(*snapshot)
		return true
	case snapshot.Height == 0 && chainHeight == 0:
		n.applySnapshotValidators(*snapshot)
		n.applySnapshotValidatorTransitions(*snapshot)
		n.applySnapshotValidatorRegistry(*snapshot)
		return true
	default:
		// Stale snapshot metadata is intentionally ignored on startup.
		return false
	}
}

func snapshotMetaSourceAllowsUnanchoredStartupRecovery(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "backup_import":
		return true
	default:
		return false
	}
}

func (n *Node) snapshotHasUnanchoredStartupRecoverySource(snapshot *StateSnapshot) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	meta, err := n.loadSnapshotMetaRecord(snapshot.Height)
	if err != nil || meta == nil {
		return false
	}
	if !snapshotMetaSourceAllowsUnanchoredStartupRecovery(meta.Source) {
		return false
	}
	if strings.TrimSpace(meta.SnapshotHash) != "" &&
		!strings.EqualFold(strings.TrimSpace(meta.SnapshotHash), strings.TrimSpace(snapshot.SnapshotHash)) {
		return false
	}
	if strings.TrimSpace(meta.StateRoot) != "" &&
		!strings.EqualFold(strings.TrimSpace(meta.StateRoot), strings.TrimSpace(snapshot.StateRoot)) {
		return false
	}
	return true
}

func (n *Node) canApplyUnanchoredStartupRecoverySnapshot(snapshot *StateSnapshot, startupChainTruncated bool) (bool, string) {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false, "snapshot_metadata_invalid"
	}
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	if snapshot.Height <= chainHeight {
		return false, "snapshot_not_ahead"
	}
	if chainHeight > 0 && !startupChainTruncated {
		return false, "local_anchor_missing"
	}
	hasRecoveryImportSource := n.snapshotHasUnanchoredStartupRecoverySource(snapshot)
	hasDurableAnchor := false
	if ok, _ := n.snapshotMatchesLocalAnchorDetailed(snapshot); ok {
		hasDurableAnchor = true
	}
	if !hasRecoveryImportSource && !hasDurableAnchor {
		return false, "source_not_recovery_import"
	}
	if !snapshotHasTrustedExecutionLedger(snapshot) {
		return false, "execution_ledger_untrusted"
	}
	if err := (SnapshotVerifier{Node: n}).Verify(snapshot); err != nil {
		return false, err.Error()
	}
	return true, ""
}
