package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func testDBKeyExists(t *testing.T, db *DB, key string) bool {
	t.Helper()
	err := db.View(func(txn *Txn) error {
		_, err := txn.Get([]byte(key))
		return err
	})
	if err == nil {
		return true
	}
	if errors.Is(err, ErrKeyNotFound) {
		return false
	}
	t.Fatalf("read key %s: %v", key, err)
	return false
}

func TestOpenNodeDBCreatesSeparatedStores(t *testing.T) {
	root := t.TempDir()
	db := OpenNodeDB(root)
	defer db.Close()

	for _, name := range []string{"state.db", "blocks.db", "snapshot.db", "tx.db", "meta.db"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("expected %s store: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", name)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, storageLayoutManifestFile))
	if err != nil {
		t.Fatalf("read storage layout manifest: %v", err)
	}
	var manifest StorageLayoutManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode storage layout manifest: %v", err)
	}
	if manifest.StateDB == "" || manifest.BlockDB == "" || manifest.SnapshotDB == "" {
		t.Fatalf("manifest missing separated DB paths: %+v", manifest)
	}

	if err := db.State.Update(func(txn *Txn) error {
		return txn.Set([]byte("state-only"), []byte("1"))
	}); err != nil {
		t.Fatalf("write state db: %v", err)
	}
	if err := db.Blocks.Update(func(txn *Txn) error {
		return txn.Set([]byte("block-only"), []byte("1"))
	}); err != nil {
		t.Fatalf("write block db: %v", err)
	}
	if err := db.Snapshot.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot-only"), []byte("1"))
	}); err != nil {
		t.Fatalf("write snapshot db: %v", err)
	}
	if !testDBKeyExists(t, db.State, "state-only") || testDBKeyExists(t, db.Snapshot, "state-only") {
		t.Fatal("state key leaked into snapshot db")
	}
	if !testDBKeyExists(t, db.Blocks, "block-only") || testDBKeyExists(t, db.State, "block-only") {
		t.Fatal("block key leaked into state db")
	}
	if !testDBKeyExists(t, db.Snapshot, "snapshot-only") || testDBKeyExists(t, db.State, "snapshot-only") {
		t.Fatal("snapshot key leaked into state db")
	}
}

func TestOpenNodeDBRecoversCorruptSeparatedStore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "snapshot.db"), []byte("not a pebble directory"), 0o600); err != nil {
		t.Fatalf("seed corrupt snapshot db path: %v", err)
	}

	db := OpenNodeDB(root)
	defer db.Close()
	if db.Snapshot == nil {
		t.Fatal("snapshot db was not opened after corruption recovery")
	}
	info, err := os.Stat(filepath.Join(root, "snapshot.db"))
	if err != nil {
		t.Fatalf("stat recovered snapshot db: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("recovered snapshot db path is not a directory")
	}
	matches, err := filepath.Glob(filepath.Join(root, "snapshot.db.corrupt-*"))
	if err != nil {
		t.Fatalf("glob corrupt snapshot quarantine: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one quarantined corrupt snapshot db, got %d: %v", len(matches), matches)
	}
}

func TestCommittedSnapshotsUseSnapshotDBWithLegacyFallback(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()
	n := &Node{ID: "A", DataDir: t.TempDir(), DB: db}
	snapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      7,
		BlockHash:   "snapshot-db-block-7",
		StateRoot:   "snapshot-db-state-7",
		Ledger:      NewLedger(),
		GenesisHash: GenesisHash,
		Validators:  map[string]bool{"A": true},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snapshot, "storage_layout_test"); err != nil {
		t.Fatalf("store committed snapshot: %v", err)
	}
	key := fmt.Sprintf("snapshot:%d", snapshot.Height)
	if !testDBKeyExists(t, db.Snapshot, key) {
		t.Fatal("new committed snapshot was not stored in snapshot db")
	}
	if testDBKeyExists(t, db.State, key) {
		t.Fatal("new committed snapshot leaked into state db")
	}
	loaded, err := n.GetSnapshot(snapshot.Height)
	if err != nil {
		t.Fatalf("load snapshot from separated snapshot db: %v", err)
	}
	if loaded.Height != snapshot.Height {
		t.Fatalf("unexpected loaded snapshot height: got=%d want=%d", loaded.Height, snapshot.Height)
	}

	legacy := StateSnapshot{Version: SnapshotVersion, Height: 8, BlockHash: "legacy", StateRoot: "legacy-state", Ledger: NewLedger(), GenesisHash: GenesisHash}
	storeSnapshotForHeight(t, &NodeDB{State: db.State}, legacy)
	loaded, err = n.GetSnapshot(legacy.Height)
	if err != nil {
		t.Fatalf("legacy state-db snapshot fallback failed: %v", err)
	}
	if loaded.Height != legacy.Height {
		t.Fatalf("unexpected legacy snapshot height: got=%d want=%d", loaded.Height, legacy.Height)
	}
}
