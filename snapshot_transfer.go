package main

import (
	"fmt"
	"strings"
	"time"
)

type SnapshotDownloadResult struct {
	// `Snapshot` stores the value associated with this record.
	Snapshot *StateSnapshot `json:"-"`
	// `Meta` stores the value associated with this record.
	Meta *SnapshotMetaRecord `json:"-"`
	// `Manifest` stores the value associated with this record.
	Manifest *SnapshotManifest `json:"-"`
	// `ExecPool` stores the value associated with this record.
	ExecPool *ExecPoolSnapshot `json:"-"`
	// `Source` stores the value associated with this record.
	Source string `json:"source,omitempty"`
	// `Stored` stores the value associated with this record.
	Stored bool `json:"stored"`
	// `Applied` stores the value associated with this record.
	Applied bool `json:"applied"`
	// `ExportDir` stores the value associated with this record.
	ExportDir string `json:"export_dir,omitempty"`
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64 `json:"checkpoint_height,omitempty"`
	// `RequiredProofs` stores the request data being processed.
	RequiredProofs int `json:"required_proofs,omitempty"`
	// `Proofs` stores the value associated with this record.
	Proofs int `json:"proofs,omitempty"`
}

// snapshotForTransfer implements the snapshot for transfer helper.
func (n *Node) snapshotForTransfer(height uint64) (*StateSnapshot, *SnapshotMetaRecord, string, error) {
	if n == nil {
		return nil, nil, "", fmt.Errorf("node unavailable")
	}
	if height == 0 {
		return n.latestCommittedSnapshotMeta()
	}
	// `snapshot`, `source`, and `ok` store whether the related condition is satisfied.
	snapshot, _, source, ok := n.ResolveCommittedStateSnapshot(height)
	if !ok || snapshot == nil {
		return nil, nil, "", fmt.Errorf("snapshot not found")
	}
	// `meta` and `err` store the error produced by this operation.
	meta, err := n.loadSnapshotMetaRecord(snapshot.Height)
	if err != nil || meta == nil {
		_ = n.ensureSnapshotMetaRecord(snapshot, source)
		meta, _ = n.loadSnapshotMetaRecord(snapshot.Height)
	}
	if meta == nil {
		meta = snapshotMetaFromSnapshot(snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
	}
	return snapshot, meta, source, nil
}

func (n *Node) snapshotPayloadForTransfer(height uint64) (*StateSnapshot, *SnapshotManifest, []byte, *SnapshotMetaRecord, string, error) {
	// `snapshot`, `meta`, `source`, and `err` store the error produced by this operation.
	snapshot, meta, source, err := n.snapshotForTransfer(height)
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	// Committed snapshots are immutable protocol records. Promotion-window state
	// was attached before the snapshot hash was sealed; re-attaching the current
	// window here can mutate a historical payload without updating its canonical
	// hash and make different requests serve different bytes for the same record.
	// `manifest`, `payload`, and `err` store the error produced by this operation.
	manifest, payload, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	return snapshot, manifest, payload, meta, source, nil
}

// snapshotManifestForTransfer implements the snapshot manifest for transfer helper.
func (n *Node) snapshotManifestForTransfer(height uint64) (*StateSnapshot, *SnapshotManifest, *SnapshotMetaRecord, string, error) {
	snapshot, manifest, _, meta, source, err := n.snapshotPayloadForTransfer(height)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return snapshot, manifest, meta, source, nil
}

// snapshotChunkResponseFromPayload implements the snapshot chunk response from payload helper.
func snapshotChunkResponseFromPayload(snapshot *StateSnapshot, manifest *SnapshotManifest, payload []byte, index uint64) (*SnapshotChunkResponse, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot unavailable")
	}
	if manifest == nil || manifest.ChunkCount == 0 {
		return nil, fmt.Errorf("snapshot manifest unavailable")
	}
	if !snapshotManifestPayloadLayoutValid(manifest, uint64(len(payload))) {
		return nil, fmt.Errorf("snapshot manifest payload layout invalid")
	}
	if index >= manifest.ChunkCount {
		return nil, fmt.Errorf("snapshot chunk unavailable")
	}
	// `chunkSize` stores the measured quantity used by this operation.
	chunkSize := manifest.ChunkSize
	if chunkSize == 0 {
		chunkSize = syncSnapshotChunkSizeBytes()
	}
	// `start` stores the value produced by this operation.
	start := index * chunkSize
	// `end` stores the value produced by this operation.
	end := start + chunkSize
	if end > uint64(len(payload)) {
		end = uint64(len(payload))
	}
	if start >= end || end > uint64(len(payload)) {
		return nil, fmt.Errorf("snapshot chunk range invalid")
	}
	// `chunk` stores the value produced by this operation.
	chunk := append([]byte{}, payload[start:end]...)
	if !snapshotManifestChunkDataValid(manifest, index, chunk) {
		return nil, fmt.Errorf("snapshot chunk layout invalid")
	}
	return &SnapshotChunkResponse{
		Height:       snapshot.Height,
		Index:        index,
		ChunkHash:    snapshotChunkHash(chunk),
		SnapshotHash: strings.TrimSpace(snapshot.SnapshotHash),
		Encoding:     manifest.Encoding,
		Compression:  manifest.Compression,
		Data:         chunk,
	}, nil
}

// snapshotChunkForTransfer implements the snapshot chunk for transfer helper.
func (n *Node) snapshotChunkForTransfer(height uint64, index uint64) (*SnapshotChunkResponse, *SnapshotManifest, *SnapshotMetaRecord, string, error) {
	// `snapshot`, `manifest`, `payload`, `meta`, `source`, and `err` store the error produced by this operation.
	snapshot, manifest, payload, meta, source, err := n.snapshotPayloadForTransfer(height)
	if err != nil {
		return nil, nil, nil, "", err
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := snapshotChunkResponseFromPayload(snapshot, manifest, payload, index)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return resp, manifest, meta, source, nil
}

// defaultSnapshotDownloadTargetHeight returns the default snapshot download target height.
func (n *Node) defaultSnapshotDownloadTargetHeight() uint64 {
	if n == nil {
		return 0
	}
	// `session` stores the value produced by this operation.
	session := n.snapshotSessionSnapshot()
	if session.Active && session.FreezeHeight > 0 {
		return session.FreezeHeight
	}
	// `finalized` stores the value produced by this operation.
	if finalized := n.getFinalizedHeight(); finalized > 0 {
		return finalized
	}
	if n.Blockchain != nil {
		return n.Blockchain.Height()
	}
	return 0
}

// applyDownloadedSnapshot applies downloaded snapshot.
func (n *Node) applyDownloadedSnapshot(snapshot *StateSnapshot, allowReapply bool) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	// `reason` stores the value produced by this operation.
	if reason := n.snapshotLocalFinalityRejectReason(snapshot); reason != "" {
		// `session` stores the value produced by this operation.
		if session := n.snapshotSessionSnapshot(); session.Active {
			n.recordSnapshotSessionStrictResult("", reason)
			n.snapshotSessionMarkFailure(reason)
		}
		fmt.Printf("[SNAPSHOT-REJECT] reason=%s local_finalized=%d snapshot=%d hash=%s\n",
			reason,
			n.getFinalizedHeight(),
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return false
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := uint64(0)
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
	}
	if snapshot.Height > localHeight {
		if !n.ApplySnapshotForSync(*snapshot) {
			// `session` stores the value produced by this operation.
			if session := n.snapshotSessionSnapshot(); session.Active {
				n.recordSnapshotSessionStrictResult("", "snapshot_apply_noop")
				n.snapshotSessionMarkFailure("snapshot_apply_noop")
			}
			fmt.Printf("[SNAPSHOT-REJECT] reason=apply_noop local=%d snapshot=%d hash=%s\n",
				localHeight,
				snapshot.Height,
				ShortHash(snapshot.BlockHash),
			)
			return false
		}
		n.noteSnapshotApplied(snapshot.Height)
		// `session` stores the value produced by this operation.
		if session := n.snapshotSessionSnapshot(); session.Active {
			n.markSnapshotSessionApplied(snapshot, 0)
		}
		return true
	}
	if snapshot.Height < localHeight {
		// `session` stores the value produced by this operation.
		if session := n.snapshotSessionSnapshot(); session.Active {
			n.recordSnapshotSessionStrictResult("", "snapshot_height_regression")
			n.snapshotSessionMarkFailure("snapshot_height_regression")
		}
		fmt.Printf("[SNAPSHOT-REJECT] reason=height_regression local=%d snapshot=%d hash=%s\n",
			localHeight,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return false
	}
	if allowReapply && snapshot.Height == localHeight {
		if !n.ApplySnapshotForRecovery(*snapshot) {
			// `session` stores the value produced by this operation.
			if session := n.snapshotSessionSnapshot(); session.Active {
				n.recordSnapshotSessionStrictResult("", "snapshot_apply_noop")
				n.snapshotSessionMarkFailure("snapshot_apply_noop")
			}
			fmt.Printf("[SNAPSHOT-REJECT] reason=recovery_apply_noop local=%d snapshot=%d hash=%s\n",
				localHeight,
				snapshot.Height,
				ShortHash(snapshot.BlockHash),
			)
			return false
		}
		n.noteSnapshotApplied(snapshot.Height)
		// `session` stores the value produced by this operation.
		if session := n.snapshotSessionSnapshot(); session.Active {
			n.markSnapshotSessionApplied(snapshot, 0)
		}
		return true
	}
	return false
}

// resetActiveSnapshotSessionForManualDownload implements the reset active snapshot session for manual download helper.
func (n *Node) resetActiveSnapshotSessionForManualDownload(targetHeight uint64, minHeight uint64) {
	if n == nil || targetHeight == 0 {
		return
	}
	// `session` stores the value produced by this operation.
	session := n.snapshotSessionSnapshot()
	if !session.Active {
		return
	}
	if session.FreezeHeight == targetHeight || session.CandidateHeight == targetHeight {
		// `required` stores the request data being processed.
		required := session.Required
		// `votes` stores the value produced by this operation.
		votes := len(session.Votes)
		if votes > 0 && (required <= 0 || votes >= required) {
			fmt.Printf("[SNAPSHOT-SESSION] manual_reuse target=%d min=%d votes=%d/%d stage=%s\n",
				targetHeight,
				minHeight,
				votes,
				required,
				session.Stage,
			)
			return
		}
	}
	n.snapshotSessionMu.Lock()
	if !n.snapshotSession.Active {
		n.snapshotSessionMu.Unlock()
		return
	}
	// `oldFreeze` stores the value produced by this operation.
	oldFreeze := n.snapshotSession.FreezeHeight
	// `oldCheckpoint` stores the value produced by this operation.
	oldCheckpoint := n.snapshotSession.CheckpointHeight
	// `oldRetries` stores the value produced by this operation.
	oldRetries := n.snapshotSession.RetryCount
	// `oldProvider` stores the value produced by this operation.
	oldProvider := strings.TrimSpace(n.snapshotSession.CurrentProvider)
	n.snapshotSession = SnapshotSession{}
	n.snapshotSessionMu.Unlock()

	n.resetSnapshotDownloadStatus()
	n.clearLateJoinAuthorityState()
	n.clearSnapshotSessionState()
	n.syncMu.Lock()
	n.syncSnapshotSessionFailures = 0
	n.syncSnapshotSessionLastFailAt = time.Time{}
	n.syncSnapshotSessionDegradedUntil = time.Time{}
	n.syncMu.Unlock()
	fmt.Printf("[SNAPSHOT-SESSION] manual_reset target=%d min=%d old_target=%d old_checkpoint=%d retries=%d provider=%s\n",
		targetHeight,
		minHeight,
		oldFreeze,
		oldCheckpoint,
		oldRetries,
		oldProvider,
	)
}

// downloadTrustedSnapshotAndStore implements the download trusted snapshot and store helper.
func (n *Node) downloadTrustedSnapshotAndStore(targetHeight uint64, minHeight uint64, strictCoreQuorum bool, apply bool, allowReapply bool, resetActiveSession bool) (*SnapshotDownloadResult, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	if targetHeight == 0 {
		targetHeight = n.defaultSnapshotDownloadTargetHeight()
	}
	if targetHeight == 0 {
		return nil, fmt.Errorf("snapshot target height required")
	}
	if minHeight > 0 && minHeight > targetHeight {
		return nil, fmt.Errorf("snapshot min height %d above target %d", minHeight, targetHeight)
	}
	if resetActiveSession {
		n.resetActiveSnapshotSessionForManualDownload(targetHeight, minHeight)
	}
	// `existing` and `err` store the error produced by this operation.
	if existing, err := n.verifiedStoredSnapshotAtOrBelow(targetHeight); err == nil && existing != nil {
		if snapshotDownloadExistingSnapshotAcceptable(existing.Height, targetHeight, minHeight) {
			// `meta` and `metaErr` store the error produced by this operation.
			meta, metaErr := n.loadSnapshotMetaRecord(existing.Height)
			if metaErr != nil || meta == nil {
				_ = n.ensureSnapshotMetaRecord(existing, "existing_verified_store")
				meta, _ = n.loadSnapshotMetaRecord(existing.Height)
			}
			if meta == nil {
				meta = snapshotMetaFromSnapshot(existing, "existing_verified_store", "committed_full", snapshotBaseHeight(existing.Height))
			}
			// `manifest` and `manifestErr` store the error produced by this operation.
			manifest, _, manifestErr := snapshotManifestFromSnapshot(existing)
			if manifestErr != nil {
				return nil, manifestErr
			}
			// `result` stores the result produced by this operation.
			result := &SnapshotDownloadResult{
				Snapshot:         existing,
				Meta:             meta,
				Manifest:         manifest,
				Source:           "existing_verified_store",
				Stored:           true,
				Applied:          false,
				ExportDir:        n.snapshotExportDirForHeight(existing.Height),
				CheckpointHeight: snapshotCheckpointHeightFor(existing.Height),
			}
			if apply {
				result.Applied = n.applyDownloadedSnapshot(existing, allowReapply)
			}
			return result, nil
		}
	}

	// `manager` stores the value produced by this operation.
	manager := NewSnapshotManager(n, targetHeight, minHeight, strictCoreQuorum)
	// `err` stores the error produced by this operation.
	if err := manager.DiscoverCheckpoint(); err != nil {
		return nil, err
	}
	_ = manager.CollectProofs()
	// `err` stores the error produced by this operation.
	if err := manager.DownloadSnapshot(); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := manager.VerifySnapshot(); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := manager.PersistSnapshot("trusted_snapshot_download"); err != nil {
		return nil, err
	}
	// `result` stores the result produced by this operation.
	result := &SnapshotDownloadResult{
		Snapshot:         manager.Snapshot,
		Meta:             manager.Meta,
		Source:           manager.Source,
		Stored:           manager.Stored,
		Applied:          false,
		ExecPool:         manager.ExecPool,
		ExportDir:        n.snapshotExportDirForHeight(manager.Snapshot.Height),
		CheckpointHeight: manager.CheckpointHeight,
		RequiredProofs:   manager.RequiredProofs,
		Proofs:           len(manager.Proofs),
	}
	if manager.Snapshot != nil {
		result.Manifest, _, _ = snapshotManifestFromSnapshot(manager.Snapshot)
	}
	if apply {
		// `err` stores the error produced by this operation.
		if err := manager.ApplySnapshot(allowReapply); err != nil {
			return nil, err
		}
		result.Applied = true
	}
	return result, nil
}

// snapshotDownloadExistingSnapshotAcceptable implements the snapshot download existing snapshot acceptable helper.
func snapshotDownloadExistingSnapshotAcceptable(existingHeight uint64, targetHeight uint64, minHeight uint64) bool {
	if existingHeight == 0 || targetHeight == 0 || existingHeight > targetHeight {
		return false
	}
	if existingHeight == targetHeight {
		return true
	}
	if minHeight > 0 {
		return existingHeight >= minHeight
	}
	// `recentWindow` stores the value produced by this operation.
	recentWindow := SyncUsableHeadRecentReplayWindowBlocks
	if recentWindow == 0 {
		recentWindow = syncDirectGossipMaxBlocks()
	}
	if recentWindow == 0 {
		recentWindow = 128
	}
	return targetHeight-existingHeight <= recentWindow
}
