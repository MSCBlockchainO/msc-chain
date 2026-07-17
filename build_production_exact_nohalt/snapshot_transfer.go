package main

import (
	"fmt"
	"strings"
	"time"
)

type SnapshotDownloadResult struct {
	Snapshot         *StateSnapshot      `json:"-"`
	Meta             *SnapshotMetaRecord `json:"-"`
	Manifest         *SnapshotManifest   `json:"-"`
	ExecPool         *ExecPoolSnapshot   `json:"-"`
	Source           string              `json:"source,omitempty"`
	Stored           bool                `json:"stored"`
	Applied          bool                `json:"applied"`
	ExportDir        string              `json:"export_dir,omitempty"`
	CheckpointHeight uint64              `json:"checkpoint_height,omitempty"`
	RequiredProofs   int                 `json:"required_proofs,omitempty"`
	Proofs           int                 `json:"proofs,omitempty"`
}

func (n *Node) snapshotForTransfer(height uint64) (*StateSnapshot, *SnapshotMetaRecord, string, error) {
	if n == nil {
		return nil, nil, "", fmt.Errorf("node unavailable")
	}
	if height == 0 {
		return n.latestCommittedSnapshotMeta()
	}
	snapshot, _, source, ok := n.ResolveCommittedStateSnapshot(height)
	if !ok || snapshot == nil {
		return nil, nil, "", fmt.Errorf("snapshot not found")
	}
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

func (n *Node) snapshotManifestForTransfer(height uint64) (*StateSnapshot, *SnapshotManifest, *SnapshotMetaRecord, string, error) {
	snapshot, meta, source, err := n.snapshotForTransfer(height)
	if err != nil {
		return nil, nil, nil, "", err
	}
	n.attachPromotionWindowStateToSnapshot(snapshot)
	manifest, _, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return snapshot, manifest, meta, source, nil
}

func snapshotChunkResponseFromPayload(snapshot *StateSnapshot, manifest *SnapshotManifest, payload []byte, index uint64) (*SnapshotChunkResponse, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot unavailable")
	}
	if manifest == nil || manifest.ChunkCount == 0 {
		return nil, fmt.Errorf("snapshot manifest unavailable")
	}
	if index >= manifest.ChunkCount {
		return nil, fmt.Errorf("snapshot chunk unavailable")
	}
	chunkSize := manifest.ChunkSize
	if chunkSize == 0 {
		chunkSize = syncSnapshotChunkSizeBytes()
	}
	start := index * chunkSize
	end := start + chunkSize
	if end > uint64(len(payload)) {
		end = uint64(len(payload))
	}
	chunk := append([]byte{}, payload[start:end]...)
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

func (n *Node) snapshotChunkForTransfer(height uint64, index uint64) (*SnapshotChunkResponse, *SnapshotManifest, *SnapshotMetaRecord, string, error) {
	snapshot, manifest, meta, source, err := n.snapshotManifestForTransfer(height)
	if err != nil {
		return nil, nil, nil, "", err
	}
	_, payload, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return nil, nil, nil, "", err
	}
	resp, err := snapshotChunkResponseFromPayload(snapshot, manifest, payload, index)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return resp, manifest, meta, source, nil
}

func (n *Node) defaultSnapshotDownloadTargetHeight() uint64 {
	if n == nil {
		return 0
	}
	session := n.snapshotSessionSnapshot()
	if session.Active && session.FreezeHeight > 0 {
		return session.FreezeHeight
	}
	if finalized := n.getFinalizedHeight(); finalized > 0 {
		return finalized
	}
	if n.Blockchain != nil {
		return n.Blockchain.Height()
	}
	return 0
}

func (n *Node) applyDownloadedSnapshot(snapshot *StateSnapshot, allowReapply bool) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	if reason := n.snapshotLocalFinalityRejectReason(snapshot); reason != "" {
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
	localHeight := uint64(0)
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
	}
	if snapshot.Height > localHeight {
		if !n.ApplySnapshotForSync(*snapshot) {
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
		if session := n.snapshotSessionSnapshot(); session.Active {
			n.markSnapshotSessionApplied(snapshot, 0)
		}
		return true
	}
	if snapshot.Height < localHeight {
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
		if session := n.snapshotSessionSnapshot(); session.Active {
			n.markSnapshotSessionApplied(snapshot, 0)
		}
		return true
	}
	return false
}

func (n *Node) resetActiveSnapshotSessionForManualDownload(targetHeight uint64, minHeight uint64) {
	if n == nil || targetHeight == 0 {
		return
	}
	session := n.snapshotSessionSnapshot()
	if !session.Active {
		return
	}
	if session.FreezeHeight == targetHeight || session.CandidateHeight == targetHeight {
		required := session.Required
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
	oldFreeze := n.snapshotSession.FreezeHeight
	oldCheckpoint := n.snapshotSession.CheckpointHeight
	oldRetries := n.snapshotSession.RetryCount
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
	if existing, err := n.verifiedStoredSnapshotAtOrBelow(targetHeight); err == nil && existing != nil {
		if snapshotDownloadExistingSnapshotAcceptable(existing.Height, targetHeight, minHeight) {
			meta, metaErr := n.loadSnapshotMetaRecord(existing.Height)
			if metaErr != nil || meta == nil {
				_ = n.ensureSnapshotMetaRecord(existing, "existing_verified_store")
				meta, _ = n.loadSnapshotMetaRecord(existing.Height)
			}
			if meta == nil {
				meta = snapshotMetaFromSnapshot(existing, "existing_verified_store", "committed_full", snapshotBaseHeight(existing.Height))
			}
			manifest, _, manifestErr := snapshotManifestFromSnapshot(existing)
			if manifestErr != nil {
				return nil, manifestErr
			}
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

	manager := NewSnapshotManager(n, targetHeight, minHeight, strictCoreQuorum)
	if err := manager.DiscoverCheckpoint(); err != nil {
		return nil, err
	}
	_ = manager.CollectProofs()
	if err := manager.DownloadSnapshot(); err != nil {
		return nil, err
	}
	if err := manager.VerifySnapshot(); err != nil {
		return nil, err
	}
	if err := manager.PersistSnapshot("trusted_snapshot_download"); err != nil {
		return nil, err
	}
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
		if err := manager.ApplySnapshot(allowReapply); err != nil {
			return nil, err
		}
		result.Applied = true
	}
	return result, nil
}

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
	recentWindow := SyncUsableHeadRecentReplayWindowBlocks
	if recentWindow == 0 {
		recentWindow = syncDirectGossipMaxBlocks()
	}
	if recentWindow == 0 {
		recentWindow = 128
	}
	return targetHeight-existingHeight <= recentWindow
}
