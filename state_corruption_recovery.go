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
