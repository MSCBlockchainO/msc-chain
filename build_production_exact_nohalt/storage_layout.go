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
	storageLayoutManifestFile = "storage_layout.json"
	storageLayoutVersion      = 1
)

type StorageLayoutManifest struct {
	Version       int    `json:"version"`
	Root          string `json:"root"`
	StateDB       string `json:"state_db"`
	BlockDB       string `json:"block_db"`
	SnapshotDB    string `json:"snapshot_db"`
	TxDB          string `json:"tx_db"`
	MetaDB        string `json:"meta_db"`
	BlockFiles    string `json:"block_files"`
	SnapshotFiles string `json:"snapshot_files"`
	Checkpoints   string `json:"checkpoints"`
	Archive       string `json:"archive"`
	UpdatedAtUnix int64  `json:"updated_at_unix"`
}

type snapshotStoreKey struct {
	key    []byte
	height uint64
}

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

func writeStorageLayoutManifest(root string) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	manifest := storageLayoutManifestForRoot(root)
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(root, storageLayoutManifestFile), raw, 0o600)
}

func openPebbleDBWithRecovery(path string, label string) (*DB, error) {
	db, err := openPebbleDB(path)
	if err == nil {
		return db, nil
	}
	if !shouldQuarantineStorageOpenError(err) {
		return nil, err
	}
	quarantinePath, qerr := quarantineStoragePath(path, label)
	if qerr != nil {
		return nil, fmt.Errorf("open %s db %s: %w; quarantine failed: %v", label, path, err, qerr)
	}
	log.Printf("[STORAGE-RECOVERY] db=%s path=%s quarantined=%s err=%v", strings.TrimSpace(label), path, quarantinePath, err)
	return openPebbleDB(path)
}

func shouldQuarantineStorageOpenError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, blocked := range []string{"lock", "being used", "in use", "resource temporarily unavailable"} {
		if strings.Contains(msg, blocked) {
			return false
		}
	}
	return true
}

func quarantineStoragePath(path string, label string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty storage path")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	suffix := fmt.Sprintf(".corrupt-%s-%d", sanitizeStorageLabel(label), time.Now().UTC().UnixNano())
	target := filepath.Join(parent, base+suffix)
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}

func sanitizeStorageLabel(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return "db"
	}
	var b strings.Builder
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

func (db *NodeDB) Close() error {
	var firstErr error
	for _, store := range uniqueDBStores(db.State, db.Blocks, db.Snapshot, db.Tx, db.Meta) {
		if err := store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func uniqueDBStores(stores ...*DB) []*DB {
	out := make([]*DB, 0, len(stores))
	seen := map[*DB]struct{}{}
	for _, store := range stores {
		if store == nil {
			continue
		}
		if _, ok := seen[store]; ok {
			continue
		}
		seen[store] = struct{}{}
		out = append(out, store)
	}
	return out
}

func (db *NodeDB) SnapshotStore() *DB {
	if db == nil {
		return nil
	}
	if db.Snapshot != nil {
		return db.Snapshot
	}
	return db.State
}

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

func (db *NodeDB) SnapshotStoresForRead() []*DB {
	if db == nil {
		return nil
	}
	return uniqueDBStores(db.Snapshot, db.State)
}

func (db *NodeDB) SnapshotMetaStoresForRead() []*DB {
	if db == nil {
		return nil
	}
	return uniqueDBStores(db.Snapshot, db.Meta, db.State)
}

func readSnapshotFromStores(stores []*DB, key []byte) (*StateSnapshot, error) {
	if len(stores) == 0 {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	var lastErr error
	for _, store := range stores {
		if store == nil {
			continue
		}
		var snapshot StateSnapshot
		err := store.View(func(txn *Txn) error {
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
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

func listSnapshotKeysFromStores(stores []*DB, maxHeight uint64) ([]snapshotStoreKey, error) {
	if len(stores) == 0 {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	byHeight := map[uint64][]byte{}
	for _, store := range stores {
		if store == nil {
			continue
		}
		if err := store.View(func(txn *Txn) error {
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			prefix := []byte("snapshot:")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := append([]byte(nil), it.Item().Key()...)
				if bytes.Equal(key, []byte("snapshot:latest")) {
					continue
				}
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				height, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || height == 0 {
					continue
				}
				if maxHeight > 0 && height > maxHeight {
					continue
				}
				if _, exists := byHeight[height]; !exists {
					byHeight[height] = key
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	heights := make([]uint64, 0, len(byHeight))
	for height := range byHeight {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	out := make([]snapshotStoreKey, 0, len(heights))
	for _, height := range heights {
		out = append(out, snapshotStoreKey{key: append([]byte(nil), byHeight[height]...), height: height})
	}
	return out, nil
}
