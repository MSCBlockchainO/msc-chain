package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newBackupRecoveryTestNode(t *testing.T, baseDir string, id string) (*Node, func()) {
	t.Helper()
	root := nodeDataPath(baseDir, id)
	db := OpenNodeDB(root)
	node := &Node{
		ID:         id,
		DataDir:    baseDir,
		DB:         db,
		Blockchain: &Blockchain{},
		Ledger:     NewLedger(),
	}
	cleanup := func() {
		if db != nil {
			_ = db.Close()
		}
	}
	return node, cleanup
}

func storeBackupRecoverySnapshot(t *testing.T, node *Node, height uint64, prevHash string, ledger Ledger) (Block, StateSnapshot) {
	t.Helper()
	block, snapshot := makeSnapshotLayerFixture(height, prevHash, ledger, testValidatorSetMaterializationRegistry())
	if err := node.storeCommittedStateSnapshotRecord(&snapshot, "backup_recovery_test"); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
	return block, snapshot
}

func TestSnapshotBackupExportImportRoundTrip(t *testing.T) {
	base := t.TempDir()
	source, cleanupSource := newBackupRecoveryTestNode(t, filepath.Join(base, "source"), "A")
	defer cleanupSource()

	ledger := NewLedger()
	ledger.Balances["alice"] = 100
	_, snapshot := storeBackupRecoverySnapshot(t, source, 7, "block-6", ledger)

	backup, err := source.ExportSnapshotBackup(snapshot.Height, "round_trip")
	if err != nil {
		t.Fatalf("ExportSnapshotBackup: %v", err)
	}
	if backup.Height != snapshot.Height || backup.SnapshotHash == "" {
		t.Fatalf("unexpected backup manifest: %+v", backup)
	}
	if _, err := os.Stat(filepath.Join(backup.BackupDir, recoveryBackupManifestFile)); err != nil {
		t.Fatalf("backup manifest not written: %v", err)
	}

	target, cleanupTarget := newBackupRecoveryTestNode(t, filepath.Join(base, "target"), "B")
	defer cleanupTarget()
	result, err := target.ImportSnapshotBackup(backup.BackupDir, true)
	if err != nil {
		t.Fatalf("ImportSnapshotBackup: %v", err)
	}
	if !result.Stored || !result.Applied || result.Height != snapshot.Height {
		t.Fatalf("unexpected import result: %+v", result)
	}
	loaded, err := target.LoadBestSnapshot()
	if err != nil {
		t.Fatalf("LoadBestSnapshot after import: %v", err)
	}
	if loaded.Height != snapshot.Height || loaded.SnapshotHash != snapshot.SnapshotHash {
		t.Fatalf("restored snapshot mismatch height=%d hash=%s want height=%d hash=%s",
			loaded.Height, loaded.SnapshotHash, snapshot.Height, snapshot.SnapshotHash)
	}
	if got := target.Blockchain.Height(); got != snapshot.Height {
		t.Fatalf("backup apply should restore chain anchor height got=%d want=%d", got, snapshot.Height)
	}
}

func TestCorruptSnapshotBackupImportRejected(t *testing.T) {
	base := t.TempDir()
	source, cleanupSource := newBackupRecoveryTestNode(t, filepath.Join(base, "source"), "A")
	defer cleanupSource()
	_, snapshot := storeBackupRecoverySnapshot(t, source, 9, "block-8", NewLedger())

	backup, err := source.ExportSnapshotBackup(snapshot.Height, "corrupt_import")
	if err != nil {
		t.Fatalf("ExportSnapshotBackup: %v", err)
	}
	payloadPath := filepath.Join(backup.BackupDir, recoveryBackupSnapshotFile)
	if err := os.WriteFile(payloadPath, []byte(`{"height":9,"corrupt":true}`), 0o600); err != nil {
		t.Fatalf("corrupt snapshot payload: %v", err)
	}

	target, cleanupTarget := newBackupRecoveryTestNode(t, filepath.Join(base, "target"), "B")
	defer cleanupTarget()
	_, err = target.ImportSnapshotBackup(backup.BackupDir, false)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected backup mismatch rejection, got %v", err)
	}
}

func TestAutomaticBackupRetentionPrunesOldBackups(t *testing.T) {
	base := t.TempDir()
	node, cleanup := newBackupRecoveryTestNode(t, base, "A")
	defer cleanup()

	oldKeep := RecoveryBackupKeepLast
	RecoveryBackupKeepLast = 2
	defer func() { RecoveryBackupKeepLast = oldKeep }()

	prev := ""
	for height := uint64(1); height <= 4; height++ {
		block, snapshot := storeBackupRecoverySnapshot(t, node, height, prev, NewLedger())
		prev = block.BlockHash
		if _, err := node.ExportSnapshotBackup(snapshot.Height, "retention"); err != nil {
			t.Fatalf("ExportSnapshotBackup height=%d: %v", height, err)
		}
	}
	backups, err := node.listRecoveryBackups()
	if err != nil {
		t.Fatalf("listRecoveryBackups: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 retained backups, got %d", len(backups))
	}
	if backups[0].Height != 4 || backups[1].Height != 3 {
		t.Fatalf("unexpected retained heights: got %d,%d", backups[0].Height, backups[1].Height)
	}
}

func TestPointInTimeRecoveryUsesBackupAndReplaysRecentBlocks(t *testing.T) {
	base := t.TempDir()
	source, cleanupSource := newBackupRecoveryTestNode(t, base, "A")

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block2, snapshot2 := storeBackupRecoverySnapshot(t, source, 2, "block-1", ledger)
	backup, err := source.ExportSnapshotBackup(snapshot2.Height, "pit_base")
	if err != nil {
		t.Fatalf("ExportSnapshotBackup: %v", err)
	}
	block3, _ := makeSnapshotLayerFixture(3, block2.BlockHash, ledger, testValidatorSetMaterializationRegistry())
	if err := source.persistBlockFile(block3); err != nil {
		t.Fatalf("persist replay block: %v", err)
	}
	cleanupSource()

	target, cleanupTarget := newBackupRecoveryTestNode(t, base, "A")
	defer cleanupTarget()
	report, err := target.RecoverToPoint(3)
	if err != nil {
		t.Fatalf("RecoverToPoint: %v (backup=%s)", err, backup.BackupDir)
	}
	if report.BaseHeight != 2 || report.TargetHeight != 3 || report.ReplayedBlocks != 1 {
		t.Fatalf("unexpected PIT report: %+v", report)
	}
	if got := target.Blockchain.Height(); got != 3 {
		t.Fatalf("PIT recovery chain height got=%d want=3", got)
	}
	if got, ok := target.getCommittedHash(3); !ok || got != block3.BlockHash {
		t.Fatalf("PIT recovery committed hash got=%q ok=%t want=%q", got, ok, block3.BlockHash)
	}
}

func TestDatabaseRecoveryQuarantinesCorruptSnapshotDBThenRestoresBackup(t *testing.T) {
	base := t.TempDir()
	node, cleanup := newBackupRecoveryTestNode(t, base, "A")
	_, snapshot := storeBackupRecoverySnapshot(t, node, 11, "block-10", NewLedger())
	backup, err := node.ExportSnapshotBackup(snapshot.Height, "db_recovery")
	if err != nil {
		t.Fatalf("ExportSnapshotBackup: %v", err)
	}
	cleanup()

	root := nodeDataPath(base, "A")
	snapshotDBPath := filepath.Join(root, "snapshot.db")
	if err := os.RemoveAll(snapshotDBPath); err != nil {
		t.Fatalf("remove snapshot db: %v", err)
	}
	if err := os.WriteFile(snapshotDBPath, []byte("not a pebble db"), 0o600); err != nil {
		t.Fatalf("write corrupt snapshot db: %v", err)
	}

	recovered, cleanupRecovered := newBackupRecoveryTestNode(t, base, "A")
	defer cleanupRecovered()
	matches, err := filepath.Glob(filepath.Join(root, "snapshot.db.corrupt-*"))
	if err != nil {
		t.Fatalf("glob quarantined db: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected corrupt snapshot db quarantine")
	}
	result, err := recovered.ImportSnapshotBackup(backup.BackupDir, true)
	if err != nil {
		t.Fatalf("ImportSnapshotBackup after db recovery: %v", err)
	}
	if result.Height != snapshot.Height || !result.Applied {
		t.Fatalf("unexpected recovery import result: %+v", result)
	}
}
