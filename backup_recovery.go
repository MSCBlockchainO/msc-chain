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
	// `recoveryBackupVersion` defines the constant value used by this package.
	recoveryBackupVersion = 2
	// `recoveryBackupKind` defines the constant value used by this package.
	recoveryBackupKind = "msc_recovery_backup_v2"
	// Legacy backups lack the committed anchor block required to reconstruct
	// deterministic post-commit state.
	recoveryBackupLegacyVersion = 1
	recoveryBackupLegacyKind    = "msc_recovery_backup_v1"
	// `recoveryBackupManifestFile` defines the constant value used by this package.
	recoveryBackupManifestFile = "backup_manifest.json"
	// `recoveryBackupSnapshotFile` defines the constant value used by this package.
	recoveryBackupSnapshotFile = "snapshot.json"
	// `recoveryBackupSnapshotMetaFile` defines the constant value used by this package.
	recoveryBackupSnapshotMetaFile = "snapshot_manifest.json"
	// `recoveryBackupLayoutFile` defines the constant value used by this package.
	recoveryBackupLayoutFile = "storage_layout.json"
	// `recoveryBackupCheckpointFile` defines the constant value used by this package.
	recoveryBackupCheckpointFile = "state_checkpoint.json"
	// `recoveryBackupAnchorBlockFile` contains the committed block whose raw
	// execution state is carried by the snapshot.
	recoveryBackupAnchorBlockFile = "anchor_block.json"
)

// `RecoveryBackupKeepLast` stores the value used by this operation.
var RecoveryBackupKeepLast uint64 = 8

type RecoveryBackupFile struct {
	// `Path` stores the value associated with this record.
	Path string `json:"path"`
	// `SHA256` stores the value associated with this record.
	SHA256 string `json:"sha256"`
	// `Size` stores the measured quantity used by this operation.
	Size int64 `json:"size"`
	// `Required` stores the request data being processed.
	Required bool `json:"required"`
}

type RecoveryBackupManifest struct {
	// `Version` stores the value associated with this record.
	Version int `json:"version"`
	// `Kind` stores the value associated with this record.
	Kind string `json:"kind"`
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `CreatedAtUnix` stores the value associated with this record.
	CreatedAtUnix int64 `json:"created_at_unix"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
	// `BackupDir` stores the value associated with this record.
	BackupDir string `json:"backup_dir,omitempty"`
	// `SnapshotManifest` stores the value associated with this record.
	SnapshotManifest *SnapshotManifest `json:"snapshot_manifest,omitempty"`
	// `StorageLayout` stores the result produced by this operation.
	StorageLayout *StorageLayoutManifest `json:"storage_layout,omitempty"`
	// `StateCheckpoint` stores the value associated with this record.
	StateCheckpoint *StateCheckpoint `json:"state_checkpoint,omitempty"`
	// `Files` stores the value associated with this record.
	Files []RecoveryBackupFile `json:"files"`
}

type RecoveryImportResult struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `BackupDir` stores the value associated with this record.
	BackupDir string `json:"backup_dir"`
	// `Stored` stores the value associated with this record.
	Stored bool `json:"stored"`
	// `Applied` stores the value associated with this record.
	Applied bool `json:"applied"`
}

type PointInTimeRecoveryOptions struct {
	// `TargetHeight` stores the value associated with this record.
	TargetHeight uint64
	// `Apply` stores the value associated with this record.
	Apply bool
	// `AllowFinalizedRollback` stores the value associated with this record.
	AllowFinalizedRollback bool
	// `VerifyReplayStateRoot` stores the digest used to identify or verify the related data.
	VerifyReplayStateRoot bool
	// `RequireContiguousReplay` stores the request data being processed.
	RequireContiguousReplay bool
}

type PointInTimeRecoveryReport struct {
	// `TargetHeight` stores the value associated with this record.
	TargetHeight uint64 `json:"target_height"`
	// `BaseHeight` stores the value associated with this record.
	BaseHeight uint64 `json:"base_height"`
	// `ReplayedBlocks` stores the value associated with this record.
	ReplayedBlocks uint64 `json:"replayed_blocks"`
	// `BackupDir` stores the value associated with this record.
	BackupDir string `json:"backup_dir"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `Applied` stores the value associated with this record.
	Applied bool `json:"applied"`
}

// recoveryNodeRoot implements the recovery node root helper.
func (n *Node) recoveryNodeRoot() string {
	if n == nil {
		return ""
	}
	// `base` stores the value produced by this operation.
	base := strings.TrimSpace(n.DataDir)
	if base == "" {
		base = "."
	}
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(n.ID)
	if id == "" {
		id = "node"
	}
	return nodeDataPath(base, id)
}

// recoveryBackupRootDir implements the recovery backup root dir helper.
func (n *Node) recoveryBackupRootDir() string {
	// `root` stores the digest used to identify or verify the related data.
	root := n.recoveryNodeRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "backups")
}

// recoveryBackupDirName implements the recovery backup dir name helper.
func recoveryBackupDirName(height uint64, createdAt time.Time) string {
	// `stamp` stores the value produced by this operation.
	stamp := createdAt.UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("backup_%020d_%s", height, stamp)
}

// checksumBytes implements the checksum bytes helper.
func checksumBytes(data []byte) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// checksumFile implements the checksum file helper.
func checksumFile(path string) (string, int64, error) {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return checksumBytes(raw), int64(len(raw)), nil
}

// writeJSONAtomic implements the write json atomic helper.
func writeJSONAtomic(path string, value any) error {
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, raw, 0o600)
}

// backupRelativePath implements the backup relative path helper.
func backupRelativePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

// backupFilePath implements the backup file path helper.
func backupFilePath(dir string, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// addBackupFile implements the add backup file helper.
func addBackupFile(files *[]RecoveryBackupFile, dir string, rel string, required bool) error {
	// `path` stores the value produced by this operation.
	path := backupFilePath(dir, rel)
	// `hash`, `size`, and `err` store the error produced by this operation.
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

// snapshotForRecoveryBackup implements the snapshot for recovery backup helper.
func (n *Node) snapshotForRecoveryBackup(height uint64) (*StateSnapshot, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	// `snapshot` stores the value used by this operation.
	var snapshot *StateSnapshot
	// `err` stores the error produced by this operation.
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
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if height > 0 && height < tip {
		if err != nil {
			return nil, err
		}
		return nil, ErrKeyNotFound
	}
	// `created` and `createErr` store the error produced by this operation.
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

// stateCheckpointForBackup implements the state checkpoint for backup helper.
func (n *Node) stateCheckpointForBackup(height uint64) (*StateCheckpoint, error) {
	if n == nil || height == 0 {
		return nil, nil
	}
	// `path` stores the value produced by this operation.
	path := stateCheckpointFilePath(n.DataDir, n.ID, height)
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(path); err == nil {
		// `checkpoint` stores the value used by this operation.
		var checkpoint StateCheckpoint
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(raw, &checkpoint); err != nil {
			return nil, err
		}
		return &checkpoint, nil
	}
	if n.DB == nil || n.DB.Meta == nil {
		return nil, nil
	}
	// `checkpoint` stores the value used by this operation.
	var checkpoint StateCheckpoint
	// `found` stores whether the related condition is satisfied.
	found := false
	// `err` stores the error produced by this operation.
	if err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
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

// ExportSnapshotBackup implements the export snapshot backup helper.
func (n *Node) ExportSnapshotBackup(height uint64, reason string) (*RecoveryBackupManifest, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.snapshotForRecoveryBackup(height)
	if err != nil {
		return nil, fmt.Errorf("snapshot backup unavailable: %w", err)
	}
	// `err` stores the error produced by this operation.
	if err := (SnapshotVerifier{}).Verify(snapshot); err != nil {
		return nil, fmt.Errorf("snapshot backup verification failed: %w", err)
	}
	// `manifest`, `payload`, and `err` store the error produced by this operation.
	manifest, payload, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	// `verified` and `err` store the error produced by this operation.
	verified, err := verifySnapshotPayloadAgainstManifest(payload, manifest, 0)
	if err != nil {
		return nil, err
	}
	if verified.Height != snapshot.Height {
		return nil, fmt.Errorf("snapshot backup height mismatch")
	}

	// `root` stores the digest used to identify or verify the related data.
	root := n.recoveryBackupRootDir()
	if root == "" {
		return nil, fmt.Errorf("backup root unavailable")
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	// `createdAt` stores the value produced by this operation.
	createdAt := time.Now().UTC()
	// `finalDir` stores the value produced by this operation.
	finalDir := filepath.Join(root, recoveryBackupDirName(snapshot.Height, createdAt))
	// `tmpDir` and `err` store the error produced by this operation.
	tmpDir, err := os.MkdirTemp(root, ".tmp-backup-*")
	if err != nil {
		return nil, err
	}
	// `cleanupTmp` stores the value produced by this operation.
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	// `files` stores the value produced by this operation.
	// A raw execution snapshot is not enough to resume consensus: post-commit
	// rewards depend on the committed block type, proposer, and signer evidence.
	anchorBlock, ok := n.snapshotAnchorBlockForLedgerReplay(*snapshot)
	if !ok {
		return nil, fmt.Errorf("snapshot backup anchor block unavailable height=%d", snapshot.Height)
	}
	if err := validateRecoveryBackupAnchorBlock(anchorBlock, snapshot); err != nil {
		return nil, err
	}

	files := make([]RecoveryBackupFile, 0, 6)
	// `err` stores the error produced by this operation.
	if err := writeFileAtomic(filepath.Join(tmpDir, recoveryBackupSnapshotFile), payload, 0o600); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := addBackupFile(&files, tmpDir, recoveryBackupSnapshotFile, true); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupSnapshotMetaFile), manifest); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := addBackupFile(&files, tmpDir, recoveryBackupSnapshotMetaFile, true); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupAnchorBlockFile), anchorBlock); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := addBackupFile(&files, tmpDir, recoveryBackupAnchorBlockFile, true); err != nil {
		return nil, err
	}

	// `layout` stores the result produced by this operation.
	layout := storageLayoutManifestForRoot(n.recoveryNodeRoot())
	// `err` stores the error produced by this operation.
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupLayoutFile), layout); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := addBackupFile(&files, tmpDir, recoveryBackupLayoutFile, false); err != nil {
		return nil, err
	}

	// `checkpoint` stores the value used by this operation.
	var checkpoint *StateCheckpoint
	// `cp` and `err` store the error produced by this operation.
	if cp, err := n.stateCheckpointForBackup(snapshot.Height); err != nil {
		return nil, err
	} else if cp != nil && cp.Height == snapshot.Height {
		checkpoint = cp
		// `err` stores the error produced by this operation.
		if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupCheckpointFile), checkpoint); err != nil {
			return nil, err
		}
		// `err` stores the error produced by this operation.
		if err := addBackupFile(&files, tmpDir, recoveryBackupCheckpointFile, false); err != nil {
			return nil, err
		}
	}

	// `backupManifest` stores the value produced by this operation.
	backupManifest := &RecoveryBackupManifest{
		Version:               recoveryBackupVersion,
		Kind:                  recoveryBackupKind,
		NodeID:                strings.TrimSpace(n.ID),
		ChainID:               protocolChainID(),
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
	// `err` stores the error produced by this operation.
	if err := writeJSONAtomic(filepath.Join(tmpDir, recoveryBackupManifestFile), backupManifest); err != nil {
		return nil, err
	}
	// `err` stores the error produced by this operation.
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return nil, err
	}
	cleanupTmp = false
	backupManifest.BackupDir = finalDir
	// `err` stores the error produced by this operation.
	if err := n.pruneRecoveryBackups(RecoveryBackupKeepLast); err != nil {
		return nil, err
	}
	return backupManifest, nil
}

// RunAutomaticBackup runs automatic backup.
func (n *Node) RunAutomaticBackup(reason string) (*RecoveryBackupManifest, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.LoadBestSnapshot()
	if err != nil || snapshot == nil || snapshot.Height == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrKeyNotFound
	}
	return n.ExportSnapshotBackup(snapshot.Height, strings.TrimSpace(reason))
}

// readRecoveryBackupManifest implements the read recovery backup manifest helper.
func readRecoveryBackupManifest(dir string) (*RecoveryBackupManifest, error) {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(filepath.Join(dir, recoveryBackupManifestFile))
	if err != nil {
		return nil, err
	}
	// `manifest` stores the value used by this operation.
	var manifest RecoveryBackupManifest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	current := manifest.Version == recoveryBackupVersion &&
		strings.TrimSpace(manifest.Kind) == recoveryBackupKind
	legacy := manifest.Version == recoveryBackupLegacyVersion &&
		strings.TrimSpace(manifest.Kind) == recoveryBackupLegacyKind
	if !current && !legacy {
		return nil, fmt.Errorf("invalid recovery backup manifest")
	}
	if manifest.Height == 0 || strings.TrimSpace(manifest.SnapshotHash) == "" {
		return nil, fmt.Errorf("incomplete recovery backup manifest")
	}
	manifest.BackupDir = dir
	return &manifest, nil
}

// verifyRecoveryBackupFiles verifies recovery backup files.
func verifyRecoveryBackupFiles(dir string, manifest *RecoveryBackupManifest) error {
	if manifest == nil {
		return fmt.Errorf("backup manifest unavailable")
	}
	// `file` tracks the current values while iterating.
	for _, file := range manifest.Files {
		if strings.TrimSpace(file.Path) == "" {
			if file.Required {
				return fmt.Errorf("backup file path missing")
			}
			continue
		}
		// `hash`, `size`, and `err` store the error produced by this operation.
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

// validateRecoveryBackupAnchorBlock proves that the anchor block and raw
// execution snapshot describe the same committed state transition.
func validateRecoveryBackupAnchorBlock(block Block, snapshot *StateSnapshot) error {
	if snapshot == nil || snapshot.Height == 0 {
		return fmt.Errorf("backup snapshot unavailable")
	}
	if block.ID != snapshot.Height {
		return fmt.Errorf("backup anchor height mismatch got=%d want=%d", block.ID, snapshot.Height)
	}
	if !strings.EqualFold(strings.TrimSpace(block.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
		return fmt.Errorf("backup anchor block hash mismatch")
	}
	if computed := strings.TrimSpace(HashBlock(block)); computed == "" ||
		!strings.EqualFold(computed, strings.TrimSpace(block.BlockHash)) {
		return fmt.Errorf("backup anchor header hash mismatch")
	}
	if strings.TrimSpace(block.StateRoot) == "" ||
		!strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(snapshot.StateRoot)) {
		return fmt.Errorf("backup anchor state root mismatch")
	}
	ledgerHash := HashLedger(snapshot.Ledger)
	expected := ComputeExecHashVersioned(
		block,
		ledgerHash,
		executionStateRootVersionForHeight(block.ID),
	)
	if expected != "" && strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(block.StateRoot)) {
		return nil
	}
	legacy := ComputeExecHash(block, ledgerHash)
	if legacy != "" && strings.EqualFold(strings.TrimSpace(legacy), strings.TrimSpace(block.StateRoot)) {
		return nil
	}
	return fmt.Errorf("backup anchor does not commit snapshot execution ledger")
}

// readRecoveryBackupAnchorBlock loads and validates the committed anchor used
// to reconstruct post-commit state after snapshot import.
func readRecoveryBackupAnchorBlock(dir string, snapshot *StateSnapshot) (Block, error) {
	raw, err := os.ReadFile(filepath.Join(dir, recoveryBackupAnchorBlockFile))
	if err != nil {
		return Block{}, err
	}
	// `block` stores the block data handled by this operation.
	var block Block
	if err := json.Unmarshal(raw, &block); err != nil {
		return Block{}, err
	}
	if err := validateRecoveryBackupAnchorBlock(block, snapshot); err != nil {
		return Block{}, err
	}
	return block, nil
}

// verifyRecoveryBackupDir verifies recovery backup dir.
func verifyRecoveryBackupDir(dir string) (*RecoveryBackupManifest, *StateSnapshot, error) {
	// `manifest` and `err` store the error produced by this operation.
	manifest, err := readRecoveryBackupManifest(dir)
	if err != nil {
		return nil, nil, err
	}
	// `err` stores the error produced by this operation.
	if err := verifyRecoveryBackupFiles(dir, manifest); err != nil {
		return nil, nil, err
	}
	// `payload` and `err` store the error produced by this operation.
	payload, err := os.ReadFile(filepath.Join(dir, recoveryBackupSnapshotFile))
	if err != nil {
		return nil, nil, err
	}
	// `snapManifest` stores the value used by this operation.
	var snapManifest SnapshotManifest
	// `rawManifest` and `err` store the error produced by this operation.
	rawManifest, err := os.ReadFile(filepath.Join(dir, recoveryBackupSnapshotMetaFile))
	if err != nil {
		return nil, nil, err
	}
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(rawManifest, &snapManifest); err != nil {
		return nil, nil, err
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := verifySnapshotPayloadAgainstManifest(payload, &snapManifest, 0)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.SnapshotHash), manifest.SnapshotHash) || snapshot.Height != manifest.Height {
		return nil, nil, fmt.Errorf("backup manifest snapshot mismatch")
	}
	if manifest.SnapshotManifest != nil && !snapshotManifestMatches(&snapManifest, manifest.SnapshotManifest) {
		return nil, nil, fmt.Errorf("backup embedded snapshot manifest mismatch")
	}
	// `err` stores the error produced by this operation.
	if err := (SnapshotVerifier{}).Verify(snapshot); err != nil {
		return nil, nil, err
	}
	if manifest.Version >= recoveryBackupVersion {
		if _, err := readRecoveryBackupAnchorBlock(dir, snapshot); err != nil {
			return nil, nil, err
		}
	}
	return manifest, snapshot, nil
}

// ImportSnapshotBackup implements the import snapshot backup helper.
func (n *Node) ImportSnapshotBackup(dir string, apply bool) (*RecoveryImportResult, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("backup dir required")
	}
	// `manifest`, `snapshot`, and `err` store the error produced by this operation.
	manifest, snapshot, err := verifyRecoveryBackupDir(dir)
	if err != nil {
		return nil, err
	}
	if apply && manifest.Version < recoveryBackupVersion {
		return nil, fmt.Errorf("legacy recovery backup lacks committed anchor block; apply requires v%d", recoveryBackupVersion)
	}
	// Persist the verified original anchor before applying the snapshot. The
	// recovery path uses it to rebuild deterministic post-commit rewards rather
	// than substituting a synthetic receipt block.
	if manifest.Version >= recoveryBackupVersion {
		anchorBlock, err := readRecoveryBackupAnchorBlock(dir, snapshot)
		if err != nil {
			return nil, err
		}
		if err := n.persistBlockFile(anchorBlock); err != nil {
			return nil, fmt.Errorf("persist backup anchor block: %w", err)
		}
	}
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `err` stores the error produced by this operation.
	if err := n.storeCommittedStateSnapshotRecord(snapshot, "backup_import"); err != nil {
		return nil, err
	}
	if manifest.StateCheckpoint != nil {
		// `err` stores the error produced by this operation.
		if err := n.persistImportedStateCheckpoint(*manifest.StateCheckpoint); err != nil {
			return nil, err
		}
	}
	// `result` stores the result produced by this operation.
	result := &RecoveryImportResult{
		Height:       snapshot.Height,
		SnapshotHash: strings.TrimSpace(snapshot.SnapshotHash),
		BackupDir:    dir,
		Stored:       true,
	}
	if apply {
		// `before` stores the value produced by this operation.
		before := uint64(0)
		if n.Blockchain != nil {
			before = n.Blockchain.Height()
		}
		n.ApplySnapshotForRecovery(*snapshot)
		// `after` stores the value produced by this operation.
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

// persistImportedStateCheckpoint implements the persist imported state checkpoint helper.
func (n *Node) persistImportedStateCheckpoint(checkpoint StateCheckpoint) error {
	if n == nil || checkpoint.Height == 0 {
		return nil
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	if n.DB != nil && n.DB.Meta != nil {
		// `err` stores the error produced by this operation.
		if err := n.DB.Meta.Update(func(txn *Txn) error {
			return txn.Set(stateCheckpointDBKey(checkpoint.Height), raw)
		}); err != nil {
			return err
		}
	}
	return writeFinalityArtifactJSON(stateCheckpointFilePath(n.DataDir, n.ID, checkpoint.Height), checkpoint)
}

// listRecoveryBackups implements the list recovery backups helper.
func (n *Node) listRecoveryBackups() ([]*RecoveryBackupManifest, error) {
	// `root` stores the digest used to identify or verify the related data.
	root := n.recoveryBackupRootDir()
	if root == "" {
		return nil, nil
	}
	// `entries` and `err` store the error produced by this operation.
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// `out` stores the result produced by this operation.
	out := make([]*RecoveryBackupManifest, 0, len(entries))
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		// `dir` stores the value produced by this operation.
		dir := filepath.Join(root, entry.Name())
		// `manifest` and `err` store the error produced by this operation.
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

// pruneRecoveryBackups implements the prune recovery backups helper.
func (n *Node) pruneRecoveryBackups(keep uint64) error {
	if keep == 0 {
		return nil
	}
	// `backups` and `err` store the error produced by this operation.
	backups, err := n.listRecoveryBackups()
	if err != nil {
		return err
	}
	if uint64(len(backups)) <= keep {
		return nil
	}
	// `backup` tracks the current values while iterating.
	for _, backup := range backups[keep:] {
		if strings.TrimSpace(backup.BackupDir) == "" {
			continue
		}
		// `err` stores the error produced by this operation.
		if err := os.RemoveAll(backup.BackupDir); err != nil {
			return err
		}
	}
	return nil
}

// RecoverToPoint implements the recover to point helper.
func (n *Node) RecoverToPoint(targetHeight uint64) (*PointInTimeRecoveryReport, error) {
	return n.RecoverToPointWithOptions(PointInTimeRecoveryOptions{
		TargetHeight:            targetHeight,
		Apply:                   true,
		VerifyReplayStateRoot:   true,
		RequireContiguousReplay: true,
	})
}

// RecoverToPointWithOptions implements the recover to point with options helper.
func (n *Node) RecoverToPointWithOptions(opts PointInTimeRecoveryOptions) (*PointInTimeRecoveryReport, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	// `target` stores the value produced by this operation.
	target := opts.TargetHeight
	if target == 0 {
		if n.Blockchain != nil {
			target = n.Blockchain.Height()
		}
	}
	if target == 0 {
		return nil, fmt.Errorf("target height required")
	}
	// `localFinalized` stores the value produced by this operation.
	localFinalized := n.getFinalizedHeight()
	if n.Blockchain != nil {
		// `finalized` stores the value produced by this operation.
		if finalized := n.Blockchain.FinalizedHeight(); finalized > localFinalized {
			localFinalized = finalized
		}
	}
	if !opts.AllowFinalizedRollback && localFinalized > target {
		return nil, fmt.Errorf("point-in-time recovery would roll back finalized height local=%d target=%d", localFinalized, target)
	}
	// `backups` and `err` store the error produced by this operation.
	backups, err := n.listRecoveryBackups()
	if err != nil {
		return nil, err
	}
	// `selected` stores the value used by this operation.
	var selected *RecoveryBackupManifest
	// `backup` tracks the current values while iterating.
	for _, backup := range backups {
		if backup.Height <= target {
			selected = backup
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("no recovery backup at or below height %d", target)
	}
	// `replayBlocks` stores the value used by this operation.
	var replayBlocks []Block
	if opts.Apply && target > selected.Height {
		replayBlocks = make([]Block, 0, target-selected.Height)
		// `height` stores the value produced by this operation.
		for height := selected.Height + 1; height <= target; height++ {
			// `block` and `ok` store whether the related condition is satisfied.
			block, ok := n.loadBlockFile(height)
			if !ok {
				return nil, fmt.Errorf("replay block file missing height=%d", height)
			}
			replayBlocks = append(replayBlocks, block)
		}
	}
	// `result` and `err` store the error produced by this operation.
	result, err := n.ImportSnapshotBackup(selected.BackupDir, opts.Apply)
	if err != nil {
		return nil, err
	}
	// `report` stores the value produced by this operation.
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
	// `replayed` and `err` store the error produced by this operation.
	replayed, err := n.replayBlocksAfterBackup(result.Height, target, opts, replayBlocks)
	if err != nil {
		return nil, err
	}
	report.ReplayedBlocks = replayed
	return report, nil
}

// replayBlocksAfterBackup implements the replay blocks after backup helper.
func (n *Node) replayBlocksAfterBackup(baseHeight uint64, targetHeight uint64, opts PointInTimeRecoveryOptions, preloaded []Block) (uint64, error) {
	if n == nil || targetHeight <= baseHeight {
		return 0, nil
	}
	if n.Blockchain == nil {
		n.Blockchain = &Blockchain{}
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(baseHeight)
	if err != nil || snapshot == nil {
		return 0, fmt.Errorf("base snapshot unavailable height=%d", baseHeight)
	}
	// Recovery snapshots commit the raw execution ledger used by StateRoot.
	// The next block, however, normally executes on the deterministic
	// post-commit ledger (rewards and supply repair included).
	executionLedger := snapshot.Ledger.Clone()
	ledger := executionLedger.Clone()
	if baseHeight > 0 {
		if restored, ok := n.committedTipLedgerFromExecutionSnapshot(baseHeight); ok {
			ledger = restored
		}
	}
	// `prevHash` stores the digest used to identify or verify the related data.
	prevHash := strings.TrimSpace(snapshot.BlockHash)
	// `replayed` stores the value produced by this operation.
	replayed := uint64(0)
	// `started` stores the value produced by this operation.
	started := time.Now()
	// `success` stores the value produced by this operation.
	success := false
	defer func() {
		n.observeReplayOperation(targetHeight, replayed, time.Since(started), success)
	}()
	// `height` stores the value produced by this operation.
	for height := baseHeight + 1; height <= targetHeight; height++ {
		// `block` stores the synchronization state protecting shared data.
		var block Block
		if len(preloaded) > 0 {
			// `idx` stores the current position in the related collection.
			idx := int(height - baseHeight - 1)
			if idx < 0 || idx >= len(preloaded) {
				return replayed, fmt.Errorf("replay block preload missing height=%d", height)
			}
			block = preloaded[idx]
		} else {
			// `loaded` and `ok` store whether the related condition is satisfied.
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
		// `nextLedger` and `err` store the error produced by this operation.
		nextLedger, err := ApplyBlockStateWithNode(n, ledger, block)
		if err != nil {
			return replayed, fmt.Errorf("replay block %d: %w", height, err)
		}
		if opts.VerifyReplayStateRoot && strings.TrimSpace(block.StateRoot) != "" {
			// `ledgerHash` stores the digest used to identify or verify the related data.
			ledgerHash := HashLedger(nextLedger)
			// `expected` stores the value produced by this operation.
			expected := ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
			if expected == "" || !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(block.StateRoot)) {
				// `legacy` stores the value produced by this operation.
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
		executionLedger = nextLedger.Clone()
		n.cacheExecutionSnapshotLedger(height, executionLedger)
		// Keep the two ledger stages distinct. The raw execution state proves
		// this block's StateRoot; the post-commit state is the authoritative
		// parent/runtime ledger for the following height.
		nextHeight := uint64(0)
		if height < targetHeight {
			nextHeight = height + 1
		}
		ledger = n.startupExecutionParentLedgerAfterBlock(
			block,
			executionLedger,
			nextHeight,
		)
		n.cachePostCommitLedger(height, ledger)
		prevHash = strings.TrimSpace(block.BlockHash)
		replayed++
	}
	n.setExecutionLedger(ledger)
	success = true
	return replayed, nil
}

// runAutomaticBackupBestEffort implements the run automatic backup best effort helper.
func (m *StorageManager) runAutomaticBackupBestEffort(reason string) {
	if m == nil || m.Node == nil {
		return
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := m.Node.LoadBestSnapshot()
	if err != nil || snapshot == nil || snapshot.Height == 0 {
		return
	}
	// `err` stores the error produced by this operation.
	if _, err := m.Node.ExportSnapshotBackup(snapshot.Height, reason); err != nil {
		// `logKey` stores the key used to access the related value.
		logKey := fmt.Sprintf("automatic_backup:%d", snapshot.Height)
		if m.Node.shouldLogLivenessReason(logKey, livenessReasonLogCooldown) {
			log.Printf("[BACKUP] automatic backup failed height=%d err=%v", snapshot.Height, err)
		}
	}
}

type backupImportRequest struct {
	// `Path` stores the value associated with this record.
	Path string `json:"path"`
	// `Apply` stores the value associated with this record.
	Apply *bool `json:"apply,omitempty"`
}

type backupRecoverRequest struct {
	// `TargetHeight` stores the value associated with this record.
	TargetHeight uint64 `json:"target_height"`
	// `AllowFinalizedRollback` stores the value associated with this record.
	AllowFinalizedRollback bool `json:"allow_finalized_rollback,omitempty"`
}

// backupRequestHeight implements the backup request height helper.
func backupRequestHeight(r *http.Request) (uint64, error) {
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(r.URL.Query().Get("height"))
	if raw == "" {
		return 0, nil
	}
	// `height` and `err` store the error produced by this operation.
	height, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid height")
	}
	return height, nil
}

// handleBackupExport handles backup export.
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
	// `height` and `err` store the error produced by this operation.
	height, err := backupRequestHeight(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// `manifest` and `err` store the error produced by this operation.
	manifest, err := s.Node.ExportSnapshotBackup(height, "rpc_backup_export")
	if err != nil {
		http.Error(w, "backup export failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(manifest)
}

// handleBackupImport handles backup import.
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
	// `apply` stores the value produced by this operation.
	apply := true
	// `raw` stores the value produced by this operation.
	if raw := strings.TrimSpace(r.URL.Query().Get("apply")); raw != "" {
		apply = snapshotQueryBool(r, "apply")
	}
	// `req` stores the request data being processed.
	req := backupImportRequest{Path: strings.TrimSpace(r.URL.Query().Get("path"))}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Apply != nil {
		apply = *req.Apply
	}
	// `result` and `err` store the error produced by this operation.
	result, err := s.Node.ImportSnapshotBackup(req.Path, apply)
	if err != nil {
		http.Error(w, "backup import failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// handleBackupRecover handles backup recover.
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
	// `height` and `err` store the error produced by this operation.
	height, err := backupRequestHeight(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// `req` stores the request data being processed.
	req := backupRecoverRequest{TargetHeight: height}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.TargetHeight == 0 {
		req.TargetHeight = height
	}
	// `report` and `err` store the error produced by this operation.
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

// handleV1BackupExport handles v1 backup export.
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
	// `height` and `err` store the error produced by this operation.
	height, err := backupRequestHeight(r)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	// `manifest` and `err` store the error produced by this operation.
	manifest, err := s.Node.ExportSnapshotBackup(height, "v1_backup_export")
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "backup export failed")
		return
	}
	writeV1Data(w, http.StatusOK, manifest)
}

// handleV1BackupImport handles v1 backup import.
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
	// `apply` stores the value produced by this operation.
	apply := true
	// `raw` stores the value produced by this operation.
	if raw := strings.TrimSpace(r.URL.Query().Get("apply")); raw != "" {
		apply = snapshotQueryBool(r, "apply")
	}
	// `req` stores the request data being processed.
	req := backupImportRequest{Path: strings.TrimSpace(r.URL.Query().Get("path"))}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Apply != nil {
		apply = *req.Apply
	}
	// `result` and `err` store the error produced by this operation.
	result, err := s.Node.ImportSnapshotBackup(req.Path, apply)
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "backup import failed")
		return
	}
	writeV1Data(w, http.StatusOK, result)
}

// handleV1BackupRecover handles v1 backup recover.
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
	// `height` and `err` store the error produced by this operation.
	height, err := backupRequestHeight(r)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	// `req` stores the request data being processed.
	req := backupRecoverRequest{TargetHeight: height}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.TargetHeight == 0 {
		req.TargetHeight = height
	}
	// `report` and `err` store the error produced by this operation.
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
