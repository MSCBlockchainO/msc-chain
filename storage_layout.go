package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// `storageLayoutManifestFile` defines the constant value used by this package.
	storageLayoutManifestFile = "storage_layout.json"
	// `storageLayoutVersion` defines the constant value used by this package.
	storageLayoutVersion      = 1
)

type StorageLayoutManifest struct {
	// `Version` stores the value associated with this record.
	Version       int    `json:"version"`
	// `Root` stores the digest used to identify or verify the related data.
	Root          string `json:"root"`
	// `StateDB` stores the value associated with this record.
	StateDB       string `json:"state_db"`
	// `BlockDB` stores the block data handled by this operation.
	BlockDB       string `json:"block_db"`
	// `SnapshotDB` stores the value associated with this record.
	SnapshotDB    string `json:"snapshot_db"`
	// `TxDB` stores the transaction data handled by this operation.
	TxDB          string `json:"tx_db"`
	// `MetaDB` stores the value associated with this record.
	MetaDB        string `json:"meta_db"`
	// `BlockFiles` stores the block data handled by this operation.
	BlockFiles    string `json:"block_files"`
	// `SnapshotFiles` stores the value associated with this record.
	SnapshotFiles string `json:"snapshot_files"`
	// `Checkpoints` stores the value associated with this record.
	Checkpoints   string `json:"checkpoints"`
	// `Archive` stores the value associated with this record.
	Archive       string `json:"archive"`
	// `UpdatedAtUnix` stores the value associated with this record.
	UpdatedAtUnix int64  `json:"updated_at_unix"`
}

type snapshotStoreKey struct {
	// `key` stores the key used to access the related value.
	key    []byte
	// `height` stores the value associated with this record.
	height uint64
}

// storageLayoutManifestForRoot implements the storage layout manifest for root helper.
func storageLayoutManifestForRoot(root string) StorageLayoutManifest {
	root = strings.TrimSpace(root)
	return StorageLayoutManifest{
		Version:       storageLayoutVersion,
		Root:          root,
		StateDB:       filepath.Join(root, "state.db"),
		BlockDB:       filepath.Join(root, "blocks.db"),
		SnapshotDB:    filepath.Join(root, "snapshot.db"),
		TxDB:          filepath.Join(root, "tx.db"),
		MetaDB:        filepath.Join(root, "meta.db"),
		BlockFiles:    filepath.Join(root, "blocks"),
		SnapshotFiles: filepath.Join(root, "snapshots"),
		Checkpoints:   filepath.Join(root, "state_checkpoints"),
		Archive:       filepath.Join(root, "cold-storage"),
		UpdatedAtUnix: time.Now().Unix(),
	}
}

// writeStorageLayoutManifest implements the write storage layout manifest helper.
func writeStorageLayoutManifest(root string) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	// `manifest` stores the value produced by this operation.
	manifest := storageLayoutManifestForRoot(root)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(root, storageLayoutManifestFile), raw, 0o600)
}

// openPebbleDBWithRecovery implements the open pebble db with recovery helper.
func openPebbleDBWithRecovery(path string, label string) (*DB, error) {
	// `db` and `err` store the error produced by this operation.
	db, err := openPebbleDB(path)
	if err == nil {
		return db, nil
	}
	if !shouldQuarantineStorageOpenError(err) {
		return nil, err
	}
	// `quarantinePath` and `qerr` store the error produced by this operation.
	quarantinePath, qerr := quarantineStoragePath(path, label)
	if qerr != nil {
		return nil, fmt.Errorf("open %s db %s: %w; quarantine failed: %v", label, path, err, qerr)
	}
	log.Printf("[STORAGE-RECOVERY] db=%s path=%s quarantined=%s err=%v", strings.TrimSpace(label), path, quarantinePath, err)
	return openPebbleDB(path)
}

// shouldQuarantineStorageOpenError implements the should quarantine storage open error helper.
func shouldQuarantineStorageOpenError(err error) bool {
	if err == nil {
		return false
	}
	// `msg` stores the value produced by this operation.
	msg := strings.ToLower(err.Error())
	// `blocked` tracks the block data handled by this operation.
	for _, blocked := range []string{"lock", "being used", "in use", "resource temporarily unavailable"} {
		if strings.Contains(msg, blocked) {
			return false
		}
	}
	return true
}

// quarantineStoragePath implements the quarantine storage path helper.
func quarantineStoragePath(path string, label string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty storage path")
	}
	// `err` stores the error produced by this operation.
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	// `parent` stores the value produced by this operation.
	parent := filepath.Dir(path)
	// `base` stores the value produced by this operation.
	base := filepath.Base(path)
	// `suffix` stores the value produced by this operation.
	suffix := fmt.Sprintf(".corrupt-%s-%d", sanitizeStorageLabel(label), time.Now().UTC().UnixNano())
	// `target` stores the value produced by this operation.
	target := filepath.Join(parent, base+suffix)
	// `err` stores the error produced by this operation.
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

// sanitizeStorageLabel implements the sanitize storage label helper.
func sanitizeStorageLabel(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return "db"
	}
	// `b` stores the value used by this operation.
	var b strings.Builder
	// `r` tracks the current values while iterating.
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "db"
	}
	return b.String()
}

// Close implements the close helper.
func (db *NodeDB) Close() error {
	// `firstErr` stores the error produced by this operation.
	var firstErr error
	// `store` tracks the current values while iterating.
	for _, store := range uniqueDBStores(db.State, db.Blocks, db.Snapshot, db.Tx, db.Meta) {
		// `err` stores the error produced by this operation.
		if err := store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// uniqueDBStores implements the unique db stores helper.
func uniqueDBStores(stores ...*DB) []*DB {
	// `out` stores the result produced by this operation.
	out := make([]*DB, 0, len(stores))
	// `seen` stores the value produced by this operation.
	seen := map[*DB]struct{}{}
	// `store` tracks the current values while iterating.
	for _, store := range stores {
		if store == nil {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[store]; ok {
			continue
		}
		seen[store] = struct{}{}
		out = append(out, store)
	}
	return out
}

// SnapshotStore implements the snapshot store helper.
func (db *NodeDB) SnapshotStore() *DB {
	if db == nil {
		return nil
	}
	if db.Snapshot != nil {
		return db.Snapshot
	}
	return db.State
}

// SnapshotMetaStore implements the snapshot meta store helper.
func (db *NodeDB) SnapshotMetaStore() *DB {
	if db == nil {
		return nil
	}
	if db.Snapshot != nil {
		return db.Snapshot
	}
	if db.Meta != nil {
		return db.Meta
	}
	return db.State
}

// SnapshotStoresForRead implements the snapshot stores for read helper.
func (db *NodeDB) SnapshotStoresForRead() []*DB {
	if db == nil {
		return nil
	}
	return uniqueDBStores(db.Snapshot, db.State)
}

// SnapshotMetaStoresForRead implements the snapshot meta stores for read helper.
func (db *NodeDB) SnapshotMetaStoresForRead() []*DB {
	if db == nil {
		return nil
	}
	return uniqueDBStores(db.Snapshot, db.Meta, db.State)
}

// readSnapshotFromStores implements the read snapshot from stores helper.
func readSnapshotFromStores(stores []*DB, key []byte) (*StateSnapshot, error) {
	if len(stores) == 0 {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `lastErr` stores the error produced by this operation.
	var lastErr error
	// `store` tracks the current values while iterating.
	for _, store := range stores {
		if store == nil {
			continue
		}
		// `snapshot` stores the value used by this operation.
		var snapshot StateSnapshot
		// `err` stores the error produced by this operation.
		err := store.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				// `plain` and `derr` store the error produced by this operation.
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				return json.Unmarshal(plain, &snapshot)
			})
		})
		if err == nil {
			populateSnapshotDerivedFields(&snapshot)
			return &snapshot, nil
		}
		if !errors.Is(err, ErrKeyNotFound) {
			return nil, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrKeyNotFound
}

// listSnapshotKeysFromStores implements the list snapshot keys from stores helper.
func listSnapshotKeysFromStores(stores []*DB, maxHeight uint64) ([]snapshotStoreKey, error) {
	if len(stores) == 0 {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `byHeight` stores the value produced by this operation.
	byHeight := map[uint64][]byte{}
	// `store` tracks the current values while iterating.
	for _, store := range stores {
		if store == nil {
			continue
		}
		// `err` stores the error produced by this operation.
		if err := store.View(func(txn *Txn) error {
			// `it` stores the current position in the related collection.
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			// `prefix` stores the value produced by this operation.
			prefix := []byte("snapshot:")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				// `key` stores the key used to access the related value.
				key := append([]byte(nil), it.Item().Key()...)
				if bytes.Equal(key, []byte("snapshot:latest")) {
					continue
				}
				// `parts` stores the value produced by this operation.
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				// `height` and `err` store the error produced by this operation.
				height, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || height == 0 {
					continue
				}
				if maxHeight > 0 && height > maxHeight {
					continue
				}
				// `exists` stores whether the related condition is satisfied.
				if _, exists := byHeight[height]; !exists {
					byHeight[height] = key
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	// `heights` stores the value produced by this operation.
	heights := make([]uint64, 0, len(byHeight))
	// `height` tracks the current values while iterating.
	for height := range byHeight {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	// `out` stores the result produced by this operation.
	out := make([]snapshotStoreKey, 0, len(heights))
	// `height` tracks the current values while iterating.
	for _, height := range heights {
		out = append(out, snapshotStoreKey{key: append([]byte(nil), byHeight[height]...), height: height})
	}
	return out, nil
}
