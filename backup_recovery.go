package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	recoveryBackupVersion          = 1
	recoveryBackupKind             = "msc_recovery_backup_v1"
	recoveryBackupManifestFile     = "backup_manifest.json"
	recoveryBackupSnapshotFile     = "snapshot.json"
	recoveryBackupSnapshotMetaFile = "snapshot_manifest.json"
	recoveryBackupLayoutFile       = "storage_layout.json"
	recoveryBackupCheckpointFile   = "state_checkpoint.json"
)

var RecoveryBackupKeepLast uint64 = 8

type RecoveryBackupFile struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Required bool   `json:"required"`
}

type RecoveryBackupManifest struct {
	Version               int                    `json:"version"`
	Kind                  string                 `json:"kind"`
	NodeID                string                 `json:"node_id"`
	ChainID               string                 `json:"chain_id"`
	Height                uint64                 `json:"height"`
	SnapshotHash          string                 `json:"snapshot_hash"`
	StateRoot             string                 `json:"state_root"`
	ValidatorSetHash      string                 `json:"validator_set_hash"`
	ValidatorRegistryHash string                 `json:"validator_registry_hash,omitempty"`
	FinalizedHeight       uint64                 `json:"finalized_height,omitempty"`
	FinalizedHash         string                 `json:"finalized_hash,omitempty"`
	EpochAnchorHash       string                 `json:"epoch_anchor_hash,omitempty"`
	FinalityRoot          string                 `json:"finality_root,omitempty"`
	CreatedAtUnix         int64                  `json:"created_at_unix"`
	Reason                string                 `json:"reason,omitempty"`
	BackupDir             string                 `json:"backup_dir,omitempty"`
	SnapshotManifest      *SnapshotManifest      `json:"snapshot_manifest,omitempty"`
	StorageLayout         *StorageLayoutManifest `json:"storage_layout,omitempty"`
	StateCheckpoint       *StateCheckpoint       `json:"state_checkpoint,omitempty"`
	Files                 []RecoveryBackupFile   `json:"files"`
}

type RecoveryImportResult struct {
	Height       uint64 `json:"height"`
	SnapshotHash string `json:"snapshot_hash"`
	BackupDir    string `json:"backup_dir"`
	Stored       bool   `json:"stored"`
	Applied      bool   `json:"applied"`
}

type PointInTimeRecoveryOptions struct {
	TargetHeight            uint64
	Apply                   bool
	AllowFinalizedRollback  bool
	VerifyReplayStateRoot   bool
	RequireContiguousReplay bool
}

type PointInTimeRecoveryReport struct {
	TargetHeight   uint64 `json:"target_height"`
	BaseHeight     uint64 `json:"base_height"`
	ReplayedBlocks uint64 `json:"replayed_blocks"`
	BackupDir      string `json:"backup_dir"`
	SnapshotHash   string `json:"snapshot_hash"`
	Applied        bool   `json:"applied"`
}

func (n *Node) recoveryNodeRoot() string {
	if n == nil {
		return ""
	}
	base := strings.TrimSpace(n.DataDir)
	if base == "" {
		base = "."
	}
	id := strings.TrimSpace(n.ID)
	if id == "" {
		id = "node"
	}
	return nodeDataPath(base, id)
}

func (n *Node) recoveryBackupRootDir() string {
	root := n.recoveryNodeRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "backups")
}

func recoveryBackupDirName(height uint64, createdAt time.Time) string {
	stamp := createdAt.UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("backup_%020d_%s", height, stamp)
}

func checksumBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func checksumFile(path string) (string, int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return checksumBytes(raw), int64(len(raw)), nil
}

func writeJSONAtomic(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, raw, 0o600)
}

func backupRelativePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func backupFilePath(dir string, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel))
}

func addBackupFile(files *[]RecoveryBackupFile, dir string, rel string, required bool) error {
	path := backupFilePath(dir, rel)
	hash, size, err := checksumFile(path)
	if err != nil {
		return err
	}
	*files = append(*files, RecoveryBackupFile{
		Path:     backupRelativePath(rel),
		SHA256:   hash,
		Size:     size,
		Required: required,
	})
	return nil
}

func (n *Node) snapshotForRecoveryBackup(height uint64) (*StateSnapshot, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	var snapshot *StateSnapshot
	var err error
	if height > 0 {
		snapshot, err = n.GetSnapshotAtOrBelow(height)
	} else {
		snapshot, err = n.LoadBestSnapshot()
	}
	if err == nil && snapshot != nil && snapshot.Height > 0 {
		return snapshot, nil
	}
	if n.Blockchain == nil || n.Blockchain.Height() == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrKeyNotFound
	}
	tip := n.Blockchain.Height()
	if height > 0 && height < tip {
		if err != nil {
			return nil, err
		}
		return nil, ErrKeyNotFound
	}
	created, _, _, createErr := n.createCommittedTipSnapshot("recovery_backup", false)
	if createErr != nil {
		if err != nil {
			return nil, fmt.Errorf("%w; create backup snapshot: %v", err, createErr)
		}
		return nil, createErr
	}
	if created == nil || created.Height == 0 {
		return nil, ErrKeyNotFound
	}
	return created, nil
}

func (n *Node) stateCheckpointForBackup(height uint64) (*StateCheckpoint, error) {
	if n == nil || height == 0 {
		return nil, nil
	}
	path := stateCheckpointFilePath(n.DataDir, n.ID, height)
	if raw, err := os.ReadFile(path); err == nil {
		var checkpoint StateCheckpoint
		if err := json.Unmarshal(raw, &checkpoint); err != nil {
			return nil, err
		}
		return &checkpoint, nil
	}
	if n.DB == nil || n.DB.Meta == nil {
		return nil, nil
	}
	var checkpoint StateCheckpoint
	found := false
	if err := n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(stateCheckpointDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &checkpoint)
		})
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &checkpoint, nil
}

func (n *Node) ExportSnapshotBackup(height uint64, reason string) (*RecoveryBackupManifest, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	snapshot, err := n.snapshotForRecoveryBackup(height)
	if err != nil {
		return nil, fmt.Errorf("snapshot backup unavailable: %w", err)
	}
	if err := (SnapshotVerifier{}).Verify(snapshot); err != nil {
		return nil, fmt.Errorf("snapshot backup verification failed: %w", err)
	}
	manifest, payload, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	verified, err := verifySnapshotPayloadAgainstManifest(payload, manifest, 0)
	if err != nil {
		return nil, err
	}
	if verified.Height != snapshot.Height {
		return nil, fmt.Errorf("snapshot backup height mismatch")
	}

	root := n.recoveryBackupRootDir()
	if root == "" {
		return nil, fmt.Errorf("backup root unavailable")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	createdAt := time.Now().UTC()
	finalDir := filepath.Join(root, recoveryBackupDirName(snapshot.Height, createdAt))
	tmpDir, err := os.MkdirTemp(root, ".tmp-backup-*")
	if err != nil {
		return nil, err
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	files := make([]RecoveryBackupFile, 0, 5)
	if err := writeFileAtomic(filepath.Join(tmpDir, recoveryBackupSnapshotFile), payload, 0o600); err != nil {
		return nil, err
	}
	if err := addBackupFile(&files, tmpDir, recoveryBackupSnapshotFile, true); err != nil {
		return nil, err
	}
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupSnapshotMetaFile), manifest); err != nil {
		return nil, err
	}
	if err := addBackupFile(&files, tmpDir, recoveryBackupSnapshotMetaFile, true); err != nil {
		return nil, err
	}

	layout := storageLayoutManifestForRoot(n.recoveryNodeRoot())
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupLayoutFile), layout); err != nil {
		return nil, err
	}
	if err := addBackupFile(&files, tmpDir, recoveryBackupLayoutFile, false); err != nil {
		return nil, err
	}

	var checkpoint *StateCheckpoint
	if cp, err := n.stateCheckpointForBackup(snapshot.Height); err != nil {
		return nil, err
	} else if cp != nil && cp.Height == snapshot.Height {
		checkpoint = cp
		if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupCheckpointFile), checkpoint); err != nil {
			return nil, err
		}
		if err := addBackupFile(&files, tmpDir, recoveryBackupCheckpointFile, false); err != nil {
			return nil, err
		}
	}

	backupManifest := &RecoveryBackupManifest{
		Version:               recoveryBackupVersion,
		Kind:                  recoveryBackupKind,
		NodeID:                strings.TrimSpace(n.ID),
		ChainID:               strings.TrimSpace(ChainID),
		Height:                snapshot.Height,
		SnapshotHash:          strings.TrimSpace(snapshot.SnapshotHash),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		ValidatorSetHash:      strings.TrimSpace(snapshotValidatorSetHash(snapshot)),
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		FinalizedHeight:       snapshot.FinalizedHeight,
		FinalizedHash:         strings.TrimSpace(snapshot.FinalizedHash),
		EpochAnchorHash:       strings.TrimSpace(snapshot.EpochAnchorHash),
		FinalityRoot:          strings.TrimSpace(snapshot.FinalityRoot),
		CreatedAtUnix:         createdAt.Unix(),
		Reason:                strings.TrimSpace(reason),
		BackupDir:             finalDir,
		SnapshotManifest:      manifest,
		StorageLayout:         &layout,
		StateCheckpoint:       checkpoint,
		Files:                 files,
	}
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupManifestFile), backupManifest); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return nil, err
	}
	cleanupTmp = false
	backupManifest.BackupDir = finalDir
	if err := n.pruneRecoveryBackups(RecoveryBackupKeepLast); err != nil {
		return nil, err
	}
	return backupManifest, nil
}

func (n *Node) RunAutomaticBackup(reason string) (*RecoveryBackupManifest, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	snapshot, err := n.LoadBestSnapshot()
	if err != nil || snapshot == nil || snapshot.Height == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrKeyNotFound
	}
	return n.ExportSnapshotBackup(snapshot.Height, strings.TrimSpace(reason))
}

func readRecoveryBackupManifest(dir string) (*RecoveryBackupManifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, recoveryBackupManifestFile))
	if err != nil {
		return nil, err
	}
	var manifest RecoveryBackupManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != recoveryBackupVersion || strings.TrimSpace(manifest.Kind) != recoveryBackupKind {
		return nil, fmt.Errorf("invalid recovery backup manifest")
	}
	if manifest.Height == 0 || strings.TrimSpace(manifest.SnapshotHash) == "" {
		return nil, fmt.Errorf("incomplete recovery backup manifest")
	}
	manifest.BackupDir = dir
	return &manifest, nil
}

func verifyRecoveryBackupFiles(dir string, manifest *RecoveryBackupManifest) error {
	if manifest == nil {
		return fmt.Errorf("backup manifest unavailable")
	}
	for _, file := range manifest.Files {
		if strings.TrimSpace(file.Path) == "" {
			if file.Required {
				return fmt.Errorf("backup file path missing")
			}
			continue
		}
		hash, size, err := checksumFile(backupFilePath(dir, file.Path))
		if err != nil {
			if file.Required {
				return err
			}
			continue
		}
		if file.Size > 0 && size != file.Size {
			return fmt.Errorf("backup file size mismatch path=%s", file.Path)
		}
		if strings.TrimSpace(file.SHA256) != "" && !strings.EqualFold(hash, strings.TrimSpace(file.SHA256)) {
			return fmt.Errorf("backup file checksum mismatch path=%s", file.Path)
		}
	}
	return nil
}

func (n *Node) ImportSnapshotBackup(dir string, apply bool) (*RecoveryImportResult, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("backup dir required")
	}
	manifest, err := readRecoveryBackupManifest(dir)
	if err != nil {
		return nil, err
	}
	if err := verifyRecoveryBackupFiles(dir, manifest); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(filepath.Join(dir, recoveryBackupSnapshotFile))
	if err != nil {
		return nil, err
	}
	var snapManifest SnapshotManifest
	rawManifest, err := os.ReadFile(filepath.Join(dir, recoveryBackupSnapshotMetaFile))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rawManifest, &snapManifest); err != nil {
		return nil, err
	}
	snapshot, err := verifySnapshotPayloadAgainstManifest(payload, &snapManifest, 0)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.SnapshotHash), manifest.SnapshotHash) || snapshot.Height != manifest.Height {
		return nil, fmt.Errorf("backup manifest snapshot mismatch")
	}
	if manifest.SnapshotManifest != nil && !snapshotManifestMatches(&snapManifest, manifest.SnapshotManifest) {
		return nil, fmt.Errorf("backup embedded snapshot manifest mismatch")
	}
	if err := (SnapshotVerifier{}).Verify(snapshot); err != nil {
		return nil, err
	}
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	if err := n.storeCommittedStateSnapshotRecord(snapshot, "backup_import"); err != nil {
		return nil, err
	}
	if manifest.StateCheckpoint != nil {
		if err := n.persistImportedStateCheckpoint(*manifest.StateCheckpoint); err != nil {
			return nil, err
		}
	}
	result := &RecoveryImportResult{
		Height:       snapshot.Height,
		SnapshotHash: strings.TrimSpace(snapshot.SnapshotHash),
		BackupDir:    dir,
		Stored:       true,
	}
	if apply {
		before := uint64(0)
		if n.Blockchain != nil {
			before = n.Blockchain.Height()
		}
		n.ApplySnapshotForRecovery(*snapshot)
		after := uint64(0)
		if n.Blockchain != nil {
			after = n.Blockchain.Height()
		}
		result.Applied = after >= snapshot.Height && (before != after || after == snapshot.Height)
		if !result.Applied {
			return result, fmt.Errorf("backup snapshot apply rejected")
		}
	}
	return result, nil
}

func (n *Node) persistImportedStateCheckpoint(checkpoint StateCheckpoint) error {
	if n == nil || checkpoint.Height == 0 {
		return nil
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	if n.DB != nil && n.DB.Meta != nil {
		if err := n.DB.Meta.Update(func(txn *Txn) error {
			return txn.Set(stateCheckpointDBKey(checkpoint.Height), raw)
		}); err != nil {
			return err
		}
	}
	return writeFinalityArtifactJSON(stateCheckpointFilePath(n.DataDir, n.ID, checkpoint.Height), checkpoint)
}

func (n *Node) listRecoveryBackups() ([]*RecoveryBackupManifest, error) {
	root := n.recoveryBackupRootDir()
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*RecoveryBackupManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		manifest, err := readRecoveryBackupManifest(dir)
		if err != nil {
			continue
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Height != out[j].Height {
			return out[i].Height > out[j].Height
		}
		return out[i].CreatedAtUnix > out[j].CreatedAtUnix
	})
	return out, nil
}

func (n *Node) pruneRecoveryBackups(keep uint64) error {
	if keep == 0 {
		return nil
	}
	backups, err := n.listRecoveryBackups()
	if err != nil {
		return err
	}
	if uint64(len(backups)) <= keep {
		return nil
	}
	for _, backup := range backups[keep:] {
		if strings.TrimSpace(backup.BackupDir) == "" {
			continue
		}
		if err := os.RemoveAll(backup.BackupDir); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) RecoverToPoint(targetHeight uint64) (*PointInTimeRecoveryReport, error) {
	return n.RecoverToPointWithOptions(PointInTimeRecoveryOptions{
		TargetHeight:            targetHeight,
		Apply:                   true,
		VerifyReplayStateRoot:   true,
		RequireContiguousReplay: true,
	})
}

func (n *Node) RecoverToPointWithOptions(opts PointInTimeRecoveryOptions) (*PointInTimeRecoveryReport, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	target := opts.TargetHeight
	if target == 0 {
		if n.Blockchain != nil {
			target = n.Blockchain.Height()
		}
	}
	if target == 0 {
		return nil, fmt.Errorf("target height required")
	}
	localFinalized := n.getFinalizedHeight()
	if n.Blockchain != nil {
		if finalized := n.Blockchain.FinalizedHeight(); finalized > localFinalized {
			localFinalized = finalized
		}
	}
	if !opts.AllowFinalizedRollback && localFinalized > target {
		return nil, fmt.Errorf("point-in-time recovery would roll back finalized height local=%d target=%d", localFinalized, target)
	}
	backups, err := n.listRecoveryBackups()
	if err != nil {
		return nil, err
	}
	var selected *RecoveryBackupManifest
	for _, backup := range backups {
		if backup.Height <= target {
			selected = backup
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("no recovery backup at or below height %d", target)
	}
	var replayBlocks []Block
	if opts.Apply && target > selected.Height {
		replayBlocks = make([]Block, 0, target-selected.Height)
		for height := selected.Height + 1; height <= target; height++ {
			block, ok := n.loadBlockFile(height)
			if !ok {
				return nil, fmt.Errorf("replay block file missing height=%d", height)
			}
			replayBlocks = append(replayBlocks, block)
		}
	}
	result, err := n.ImportSnapshotBackup(selected.BackupDir, opts.Apply)
	if err != nil {
		return nil, err
	}
	report := &PointInTimeRecoveryReport{
		TargetHeight: target,
		BaseHeight:   result.Height,
		BackupDir:    selected.BackupDir,
		SnapshotHash: result.SnapshotHash,
		Applied:      result.Applied,
	}
	if target == result.Height {
		return report, nil
	}
	if target < result.Height {
		return nil, fmt.Errorf("selected backup height %d above target %d", result.Height, target)
	}
	if !opts.Apply {
		return report, nil
	}
	replayed, err := n.replayBlocksAfterBackup(result.Height, target, opts, replayBlocks)
	if err != nil {
		return nil, err
	}
	report.ReplayedBlocks = replayed
	return report, nil
}

func (n *Node) replayBlocksAfterBackup(baseHeight uint64, targetHeight uint64, opts PointInTimeRecoveryOptions, preloaded []Block) (uint64, error) {
	if n == nil || targetHeight <= baseHeight {
		return 0, nil
	}
	if n.Blockchain == nil {
		n.Blockchain = &Blockchain{}
	}
	snapshot, err := n.GetSnapshot(baseHeight)
	if err != nil || snapshot == nil {
		return 0, fmt.Errorf("base snapshot unavailable height=%d", baseHeight)
	}
	ledger := snapshot.Ledger.Clone()
	prevHash := strings.TrimSpace(snapshot.BlockHash)
	replayed := uint64(0)
	started := time.Now()
	success := false
	defer func() {
		n.observeReplayOperation(targetHeight, replayed, time.Since(started), success)
	}()
	for height := baseHeight + 1; height <= targetHeight; height++ {
		var block Block
		if len(preloaded) > 0 {
			idx := int(height - baseHeight - 1)
			if idx < 0 || idx >= len(preloaded) {
				return replayed, fmt.Errorf("replay block preload missing height=%d", height)
			}
			block = preloaded[idx]
		} else {
			loaded, ok := n.loadBlockFile(height)
			if !ok {
				return replayed, fmt.Errorf("replay block file missing height=%d", height)
			}
			block = loaded
		}
		if block.ID == 0 {
			block.ID = height
		}
		if block.ID != height {
			return replayed, fmt.Errorf("replay block height mismatch got=%d want=%d", block.ID, height)
		}
		if opts.RequireContiguousReplay && strings.TrimSpace(block.PrevHash) != "" && prevHash != "" &&
			!strings.EqualFold(strings.TrimSpace(block.PrevHash), prevHash) {
			return replayed, fmt.Errorf("replay prev-hash mismatch height=%d", height)
		}
		nextLedger, err := ApplyBlockStateWithNode(n, ledger, block)
		if err != nil {
			return replayed, fmt.Errorf("replay block %d: %w", height, err)
		}
		if opts.VerifyReplayStateRoot && strings.TrimSpace(block.StateRoot) != "" {
			ledgerHash := HashLedger(nextLedger)
			expected := ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
			if expected == "" || !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(block.StateRoot)) {
				legacy := ComputeExecHash(block, ledgerHash)
				if legacy == "" || !strings.EqualFold(strings.TrimSpace(legacy), strings.TrimSpace(block.StateRoot)) {
					return replayed, fmt.Errorf("replay state root mismatch height=%d", height)
				}
			}
		}
		n.Blockchain.AddBlock(block)
		n.commitMu.Lock()
		if n.committed == nil {
			n.committed = make(map[uint64]string)
		}
		n.committed[height] = strings.TrimSpace(block.BlockHash)
		n.committedHeight = height
		n.lastCommitHeight = height
		if n.finalizedHeight < height {
			n.finalizedHeight = height
		}
		n.commitMu.Unlock()
		_ = n.persistFinalizedHashInvariant(block)
		ledger = nextLedger.Clone()
		n.cacheExecutionSnapshotLedger(height, ledger)
		n.cachePostCommitLedger(height, ledger)
		prevHash = strings.TrimSpace(block.BlockHash)
		replayed++
	}
	n.setExecutionLedger(ledger)
	success = true
	return replayed, nil
}

func (m *StorageManager) runAutomaticBackupBestEffort(reason string) {
	if m == nil || m.Node == nil {
		return
	}
	snapshot, err := m.Node.LoadBestSnapshot()
	if err != nil || snapshot == nil || snapshot.Height == 0 {
		return
	}
	if _, err := m.Node.ExportSnapshotBackup(snapshot.Height, reason); err != nil {
		logKey := fmt.Sprintf("automatic_backup:%d", snapshot.Height)
		if m.Node.shouldLogLivenessReason(logKey, livenessReasonLogCooldown) {
			log.Printf("[BACKUP] automatic backup failed height=%d err=%v", snapshot.Height, err)
		}
	}
}

type backupImportRequest struct {
	Path  string `json:"path"`
	Apply *bool  `json:"apply,omitempty"`
}

type backupRecoverRequest struct {
	TargetHeight           uint64 `json:"target_height"`
	AllowFinalizedRollback bool   `json:"allow_finalized_rollback,omitempty"`
}

func backupRequestHeight(r *http.Request) (uint64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("height"))
	if raw == "" {
		return 0, nil
	}
	height, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid height")
	}
	return height, nil
}

func (s *Server) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}
	height, err := backupRequestHeight(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	manifest, err := s.Node.ExportSnapshotBackup(height, "rpc_backup_export")
	if err != nil {
		http.Error(w, "backup export failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(manifest)
}

func (s *Server) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}
	apply := true
	if raw := strings.TrimSpace(r.URL.Query().Get("apply")); raw != "" {
		apply = snapshotQueryBool(r, "apply")
	}
	req := backupImportRequest{Path: strings.TrimSpace(r.URL.Query().Get("path"))}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Apply != nil {
		apply = *req.Apply
	}
	result, err := s.Node.ImportSnapshotBackup(req.Path, apply)
	if err != nil {
		http.Error(w, "backup import failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleBackupRecover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}
	height, err := backupRequestHeight(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req := backupRecoverRequest{TargetHeight: height}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.TargetHeight == 0 {
		req.TargetHeight = height
	}
	report, err := s.Node.RecoverToPointWithOptions(PointInTimeRecoveryOptions{
		TargetHeight:            req.TargetHeight,
		Apply:                   true,
		AllowFinalizedRollback:  req.AllowFinalizedRollback,
		VerifyReplayStateRoot:   true,
		RequireContiguousReplay: true,
	})
	if err != nil {
		http.Error(w, "backup recovery failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

func (s *Server) handleV1BackupExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorizedSubmit(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}
	height, err := backupRequestHeight(r)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	manifest, err := s.Node.ExportSnapshotBackup(height, "v1_backup_export")
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "backup export failed")
		return
	}
	writeV1Data(w, http.StatusOK, manifest)
}

func (s *Server) handleV1BackupImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorizedSubmit(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}
	apply := true
	if raw := strings.TrimSpace(r.URL.Query().Get("apply")); raw != "" {
		apply = snapshotQueryBool(r, "apply")
	}
	req := backupImportRequest{Path: strings.TrimSpace(r.URL.Query().Get("path"))}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Apply != nil {
		apply = *req.Apply
	}
	result, err := s.Node.ImportSnapshotBackup(req.Path, apply)
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "backup import failed")
		return
	}
	writeV1Data(w, http.StatusOK, result)
}

func (s *Server) handleV1BackupRecover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorizedSubmit(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}
	height, err := backupRequestHeight(r)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	req := backupRecoverRequest{TargetHeight: height}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.TargetHeight == 0 {
		req.TargetHeight = height
	}
	report, err := s.Node.RecoverToPointWithOptions(PointInTimeRecoveryOptions{
		TargetHeight:            req.TargetHeight,
		Apply:                   true,
		AllowFinalizedRollback:  req.AllowFinalizedRollback,
		VerifyReplayStateRoot:   true,
		RequireContiguousReplay: true,
	})
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "backup recovery failed")
		return
	}
	writeV1Data(w, http.StatusOK, report)
}
