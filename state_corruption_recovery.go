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
	// `maxStartupExportedSnapshotBytes` defines the constant value used by this package.
	maxStartupExportedSnapshotBytes         = uint64(1 << 30)
	// `maxStartupExportedSnapshotChunks` defines the constant value used by this package.
	maxStartupExportedSnapshotChunks        = uint64(1 << 16)
	// `maxStartupExportedSnapshotManifestBytes` defines the constant value used by this package.
	maxStartupExportedSnapshotManifestBytes = int64(4 << 20)
)

// loadBestReadableSnapshotAtOrBelow implements the load best readable snapshot at or below helper.
func (n *Node) loadBestReadableSnapshotAtOrBelow(targetHeight uint64) (*StateSnapshot, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), targetHeight)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrKeyNotFound
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].height > keys[j].height })
	// `lastErr` stores the error produced by this operation.
	var lastErr error
	// `candidate` tracks the current values while iterating.
	for _, candidate := range keys {
		// `snapshot` and `err` store the error produced by this operation.
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

// loadBestAnchoredStartupRecoverySnapshot implements the load best anchored startup recovery snapshot helper.
func (n *Node) loadBestAnchoredStartupRecoverySnapshot() (*StateSnapshot, Block, string) {
	if n == nil {
		return nil, Block{}, "node_unavailable"
	}

	// `dbKeys` stores the value produced by this operation.
	dbKeys := make(map[uint64][]byte)
	// `lastReason` stores the value produced by this operation.
	lastReason := "anchored_snapshot_unavailable"
	if n.DB != nil && n.DB.SnapshotStore() != nil {
		// `keys` and `err` store the error produced by this operation.
		keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
		if err != nil {
			lastReason = err.Error()
		} else {
			// `candidate` tracks the current values while iterating.
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

	// `exportedHeights` and `exportErr` store the error produced by this operation.
	exportedHeights, exportErr := n.exportedSnapshotArtifactHeights()
	if exportErr != nil && len(dbKeys) == 0 {
		lastReason = exportErr.Error()
	}
	// `heightSet` stores the value produced by this operation.
	heightSet := make(map[uint64]struct{}, len(dbKeys)+len(exportedHeights))
	// `exportedHeightSet` stores the value produced by this operation.
	exportedHeightSet := make(map[uint64]struct{}, len(exportedHeights))
	// `height` tracks the current values while iterating.
	for height := range dbKeys {
		heightSet[height] = struct{}{}
	}
	// `height` tracks the current values while iterating.
	for _, height := range exportedHeights {
		heightSet[height] = struct{}{}
		exportedHeightSet[height] = struct{}{}
	}
	// `heights` stores the value produced by this operation.
	heights := make([]uint64, 0, len(heightSet))
	// `height` tracks the current values while iterating.
	for height := range heightSet {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })

	// `audited` stores the value produced by this operation.
	audited := 0
	// `auditSkip` stores the value produced by this operation.
	auditSkip := func(height uint64, source string, reason string) {
		if audited >= 12 {
			return
		}
		audited++
		log.Printf("[SNAPSHOT-RECOVERY-CANDIDATE] height=%d source=%s status=skip reason=%s",
			height, strings.TrimSpace(source), strings.TrimSpace(reason))
	}
	// `validate` stores whether the related condition is satisfied.
	validate := func(snapshot *StateSnapshot, anchor Block, source string) (*StateSnapshot, Block, bool) {
		if snapshot == nil {
			lastReason = "snapshot_unavailable"
			return nil, Block{}, false
		}
		// `ok` and `reason` store whether the related condition is satisfied.
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

	// `height` tracks the current values while iterating.
	for _, height := range heights {
		// `anchor` and `ok` store whether the related condition is satisfied.
		anchor, ok := n.loadDurableBlock(height)
		if !ok {
			lastReason = "anchor_block_unavailable"
			auditSkip(height, "durable_anchor", lastReason)
			continue
		}
		// `key` stores the key used to access the related value.
		if key := dbKeys[height]; len(key) > 0 {
			// `snapshot` and `err` store the error produced by this operation.
			snapshot, err := readSnapshotFromStores(n.DB.SnapshotStoresForRead(), key)
			// `selected`, `anchor`, and `ok` store whether the related condition is satisfied.
			if err != nil || snapshot == nil {
				if err != nil {
					lastReason = err.Error()
				}
				auditSkip(height, "snapshot_db", lastReason)
			} else if selected, anchor, ok := validate(snapshot, anchor, "snapshot_db"); ok {
				return selected, anchor, ""
			}
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := exportedHeightSet[height]; ok {
			// `snapshot` and `err` store the error produced by this operation.
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

// exportedSnapshotArtifactHeights implements the exported snapshot artifact heights helper.
func (n *Node) exportedSnapshotArtifactHeights() ([]uint64, error) {
	if n == nil || strings.TrimSpace(n.DataDir) == "" {
		return nil, fmt.Errorf("snapshot export directory unavailable")
	}
	// `entries` and `err` store the error produced by this operation.
	entries, err := os.ReadDir(filepath.Join(n.DataDir, "snapshots"))
	if err != nil {
		return nil, err
	}
	// `heights` stores the value produced by this operation.
	heights := make([]uint64, 0, len(entries))
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// `height` and `err` store the error produced by this operation.
		height, err := strconv.ParseUint(strings.TrimSpace(entry.Name()), 10, 64)
		if err != nil || height == 0 {
			continue
		}
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })
	return heights, nil
}

// loadExportedSnapshotArtifact implements the load exported snapshot artifact helper.
func (n *Node) loadExportedSnapshotArtifact(height uint64) (*StateSnapshot, error) {
	if n == nil || height == 0 || strings.TrimSpace(n.DataDir) == "" {
		return nil, fmt.Errorf("snapshot export unavailable")
	}
	// `dir` stores the value produced by this operation.
	dir := filepath.Join(n.DataDir, "snapshots", fmt.Sprintf("%020d", height))
	// `manifestPath` stores the value produced by this operation.
	manifestPath := filepath.Join(dir, "meta.json")
	// `manifestInfo` and `err` store the error produced by this operation.
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		return nil, err
	}
	if manifestInfo.Size() <= 0 || manifestInfo.Size() > maxStartupExportedSnapshotManifestBytes {
		return nil, fmt.Errorf("exported snapshot manifest size invalid")
	}
	// `rawManifest` and `err` store the error produced by this operation.
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if int64(len(rawManifest)) > maxStartupExportedSnapshotManifestBytes {
		return nil, fmt.Errorf("exported snapshot manifest size invalid")
	}
	// `manifest` stores the value used by this operation.
	var manifest SnapshotManifest
	// `err` stores the error produced by this operation.
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
	// `expectedChunks` stores the value produced by this operation.
	expectedChunks := manifest.SnapshotSizeBytes / manifest.ChunkSize
	if manifest.SnapshotSizeBytes%manifest.ChunkSize != 0 {
		expectedChunks++
	}
	if manifest.ChunkCount == 0 ||
		manifest.ChunkCount > maxStartupExportedSnapshotChunks ||
		manifest.ChunkCount != expectedChunks {
		return nil, fmt.Errorf("exported snapshot chunk count invalid")
	}

	// `payload` stores the value used by this operation.
	var payload bytes.Buffer
	payload.Grow(int(manifest.SnapshotSizeBytes))
	// `idx` stores the current position in the related collection.
	for idx := uint64(0); idx < manifest.ChunkCount; idx++ {
		// `chunkPath` stores the value produced by this operation.
		chunkPath := filepath.Join(dir, fmt.Sprintf("chunk_%04d", idx))
		// `info` and `err` store the error produced by this operation.
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
		// `raw` and `err` store the error produced by this operation.
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

// startupSnapshotMatchesCommittedTip implements the startup snapshot matches committed tip helper.
func (n *Node) startupSnapshotMatchesCommittedTip(snapshot *StateSnapshot) (bool, string) {
	if n == nil || snapshot == nil || n.Blockchain == nil {
		return false, "snapshot_metadata_invalid"
	}
	// `ok` and `reason` store whether the related condition is satisfied.
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "anchor_verification_failed"
		}
		return false, reason
	}
	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := n.Blockchain.GetBlock(snapshot.Height); ok {
		// `expectedRegistry` stores the value produced by this operation.
		expectedRegistry := strings.TrimSpace(block.ValidatorRegistryHash)
		if expectedRegistry != "" && !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)), expectedRegistry) {
			return false, "registry_hash_mismatch"
		}
	}
	return true, ""
}

// applyStartupBestSnapshot applies startup best snapshot.
func (n *Node) applyStartupBestSnapshot(snapshot *StateSnapshot, startupChainTruncated bool) bool {
	if n == nil || snapshot == nil || n.Blockchain == nil {
		return false
	}
	// `chainHeight` stores the value produced by this operation.
	chainHeight := n.Blockchain.Height()
	switch {
	case snapshot.Height > chainHeight:
		// `ok` and `reason` store whether the related condition is satisfied.
		if ok, reason := n.canApplyUnanchoredStartupRecoverySnapshot(snapshot, startupChainTruncated); ok {
			// `recoverySource` stores the value produced by this operation.
			recoverySource := "backup_import"
			// `persistSource` stores the value produced by this operation.
			persistSource := "backup_import"
			// `anchorOK` stores whether the related condition is satisfied.
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
			// `err` stores the error produced by this operation.
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
		// `ok` and `reason` store whether the related condition is satisfied.
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

// snapshotMetaSourceAllowsUnanchoredStartupRecovery implements the snapshot meta source allows unanchored startup recovery helper.
func snapshotMetaSourceAllowsUnanchoredStartupRecovery(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "backup_import":
		return true
	default:
		return false
	}
}

// snapshotHasUnanchoredStartupRecoverySource implements the snapshot has unanchored startup recovery source helper.
func (n *Node) snapshotHasUnanchoredStartupRecoverySource(snapshot *StateSnapshot) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	// `meta` and `err` store the error produced by this operation.
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

// canApplyUnanchoredStartupRecoverySnapshot implements the can apply unanchored startup recovery snapshot helper.
func (n *Node) canApplyUnanchoredStartupRecoverySnapshot(snapshot *StateSnapshot, startupChainTruncated bool) (bool, string) {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false, "snapshot_metadata_invalid"
	}
	// `chainHeight` stores the value produced by this operation.
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
	// `hasRecoveryImportSource` stores the value produced by this operation.
	hasRecoveryImportSource := n.snapshotHasUnanchoredStartupRecoverySource(snapshot)
	// `hasDurableAnchor` stores the value produced by this operation.
	hasDurableAnchor := false
	// `ok` stores whether the related condition is satisfied.
	if ok, _ := n.snapshotMatchesLocalAnchorDetailed(snapshot); ok {
		hasDurableAnchor = true
	}
	if !hasRecoveryImportSource && !hasDurableAnchor {
		return false, "source_not_recovery_import"
	}
	if !snapshotHasTrustedExecutionLedger(snapshot) {
		return false, "execution_ledger_untrusted"
	}
	// `err` stores the error produced by this operation.
	if err := (SnapshotVerifier{Node: n}).Verify(snapshot); err != nil {
		return false, err.Error()
	}
	return true, ""
}
