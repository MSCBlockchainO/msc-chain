package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	maxStartupExportedSnapshotBytes         = uint64(1 << 30)
	maxStartupExportedSnapshotChunks        = uint64(1 << 16)
	maxStartupExportedSnapshotManifestBytes = int64(4 << 20)
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
	if n == nil {
		return nil, Block{}, "node_unavailable"
	}

	dbKeys := make(map[uint64][]byte)
	lastReason := "anchored_snapshot_unavailable"
	if n.DB != nil && n.DB.SnapshotStore() != nil {
		keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
		if err != nil {
			lastReason = err.Error()
		} else {
			for _, candidate := range keys {
				if candidate.height == 0 {
					continue
				}
				dbKeys[candidate.height] = append([]byte{}, candidate.key...)
			}
		}
	} else {
		lastReason = "snapshot_db_unavailable"
	}

	exportedHeights, exportErr := n.exportedSnapshotArtifactHeights()
	if exportErr != nil && len(dbKeys) == 0 {
		lastReason = exportErr.Error()
	}
	heightSet := make(map[uint64]struct{}, len(dbKeys)+len(exportedHeights))
	exportedHeightSet := make(map[uint64]struct{}, len(exportedHeights))
	for height := range dbKeys {
		heightSet[height] = struct{}{}
	}
	for _, height := range exportedHeights {
		heightSet[height] = struct{}{}
		exportedHeightSet[height] = struct{}{}
	}
	heights := make([]uint64, 0, len(heightSet))
	for height := range heightSet {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })

	audited := 0
	auditSkip := func(height uint64, source string, reason string) {
		if audited >= 12 {
			return
		}
		audited++
		log.Printf("[SNAPSHOT-RECOVERY-CANDIDATE] height=%d source=%s status=skip reason=%s",
			height, strings.TrimSpace(source), strings.TrimSpace(reason))
	}
	validate := func(snapshot *StateSnapshot, anchor Block, source string) (*StateSnapshot, Block, bool) {
		if snapshot == nil {
			lastReason = "snapshot_unavailable"
			return nil, Block{}, false
		}
		if ok, reason := n.canApplyUnanchoredStartupRecoverySnapshot(snapshot, true); !ok {
			if strings.TrimSpace(reason) != "" {
				lastReason = strings.TrimSpace(reason)
			}
			auditSkip(snapshot.Height, source, lastReason)
			return nil, Block{}, false
		}
		if !strings.EqualFold(strings.TrimSpace(anchor.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			lastReason = "block_hash_mismatch"
			auditSkip(snapshot.Height, source, lastReason)
			return nil, Block{}, false
		}
		log.Printf("[SNAPSHOT-RECOVERY-CANDIDATE] height=%d source=%s status=selected hash=%s",
			snapshot.Height, strings.TrimSpace(source), ShortHash(snapshot.BlockHash))
		return snapshot, anchor, true
	}

	for _, height := range heights {
		anchor, ok := n.loadDurableBlock(height)
		if !ok {
			lastReason = "anchor_block_unavailable"
			auditSkip(height, "durable_anchor", lastReason)
			continue
		}
		if key := dbKeys[height]; len(key) > 0 {
			snapshot, err := readSnapshotFromStores(n.DB.SnapshotStoresForRead(), key)
			if err != nil || snapshot == nil {
				if err != nil {
					lastReason = err.Error()
				}
				auditSkip(height, "snapshot_db", lastReason)
			} else if selected, anchor, ok := validate(snapshot, anchor, "snapshot_db"); ok {
				return selected, anchor, ""
			}
		}
		if _, ok := exportedHeightSet[height]; ok {
			if snapshot, err := n.loadExportedSnapshotArtifact(height); err != nil || snapshot == nil {
				if err != nil {
					lastReason = err.Error()
					auditSkip(height, "snapshot_export", lastReason)
				}
			} else if selected, anchor, ok := validate(snapshot, anchor, "snapshot_export"); ok {
				return selected, anchor, ""
			}
		}
	}
	return nil, Block{}, lastReason
}

func (n *Node) exportedSnapshotArtifactHeights() ([]uint64, error) {
	if n == nil || strings.TrimSpace(n.DataDir) == "" {
		return nil, fmt.Errorf("snapshot export directory unavailable")
	}
	entries, err := os.ReadDir(filepath.Join(n.DataDir, "snapshots"))
	if err != nil {
		return nil, err
	}
	heights := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		height, err := strconv.ParseUint(strings.TrimSpace(entry.Name()), 10, 64)
		if err != nil || height == 0 {
			continue
		}
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })
	return heights, nil
}

func (n *Node) loadExportedSnapshotArtifact(height uint64) (*StateSnapshot, error) {
	if n == nil || height == 0 || strings.TrimSpace(n.DataDir) == "" {
		return nil, fmt.Errorf("snapshot export unavailable")
	}
	dir := filepath.Join(n.DataDir, "snapshots", fmt.Sprintf("%020d", height))
	manifestPath := filepath.Join(dir, "meta.json")
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		return nil, err
	}
	if manifestInfo.Size() <= 0 || manifestInfo.Size() > maxStartupExportedSnapshotManifestBytes {
		return nil, fmt.Errorf("exported snapshot manifest size invalid")
	}
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if int64(len(rawManifest)) > maxStartupExportedSnapshotManifestBytes {
		return nil, fmt.Errorf("exported snapshot manifest size invalid")
	}
	var manifest SnapshotManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return nil, err
	}
	normalizeSnapshotManifest(&manifest)
	if manifest.Height != height || !snapshotManifestBasicValid(&manifest, height, height) {
		return nil, fmt.Errorf("invalid exported snapshot manifest")
	}
	if manifest.SnapshotSizeBytes == 0 || manifest.SnapshotSizeBytes > maxStartupExportedSnapshotBytes {
		return nil, fmt.Errorf("exported snapshot payload size invalid")
	}
	if manifest.ChunkSize == 0 || manifest.ChunkSize > maxStartupExportedSnapshotBytes {
		return nil, fmt.Errorf("exported snapshot chunk size invalid")
	}
	expectedChunks := manifest.SnapshotSizeBytes / manifest.ChunkSize
	if manifest.SnapshotSizeBytes%manifest.ChunkSize != 0 {
		expectedChunks++
	}
	if manifest.ChunkCount == 0 ||
		manifest.ChunkCount > maxStartupExportedSnapshotChunks ||
		manifest.ChunkCount != expectedChunks {
		return nil, fmt.Errorf("exported snapshot chunk count invalid")
	}

	var payload bytes.Buffer
	payload.Grow(int(manifest.SnapshotSizeBytes))
	for idx := uint64(0); idx < manifest.ChunkCount; idx++ {
		chunkPath := filepath.Join(dir, fmt.Sprintf("chunk_%04d", idx))
		info, err := os.Stat(chunkPath)
		if err != nil {
			return nil, err
		}
		if info.Size() <= 0 || uint64(info.Size()) > manifest.ChunkSize {
			return nil, fmt.Errorf("exported snapshot chunk size invalid index=%d", idx)
		}
		if uint64(payload.Len())+uint64(info.Size()) > manifest.SnapshotSizeBytes {
			return nil, fmt.Errorf("exported snapshot payload size mismatch")
		}
		raw, err := os.ReadFile(chunkPath)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 ||
			uint64(len(raw)) > manifest.ChunkSize ||
			uint64(payload.Len())+uint64(len(raw)) > manifest.SnapshotSizeBytes {
			return nil, fmt.Errorf("exported snapshot chunk size invalid index=%d", idx)
		}
		payload.Write(raw)
	}
	return verifySnapshotPayloadAgainstManifest(payload.Bytes(), &manifest, height)
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
			recoverySource := "backup_import"
			persistSource := "backup_import"
			if anchorOK, _ := n.snapshotMatchesLocalAnchorDetailed(snapshot); anchorOK {
				recoverySource = "durable_anchor"
				persistSource = "startup_durable_anchor_recovery"
			}
			log.Printf("[SNAPSHOT-RECOVERY] startup_apply_unanchored height=%d tip=%d source=%s",
				snapshot.Height,
				chainHeight,
				recoverySource,
			)
			if !n.ApplySnapshotForRecovery(*snapshot) {
				return false
			}
			if err := n.storeCommittedStateSnapshotRecord(snapshot, persistSource); err != nil {
				log.Printf("[WARN] startup recovered snapshot persist failed height=%d err=%v", snapshot.Height, err)
			}
			return true
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
