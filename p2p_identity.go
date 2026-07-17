package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
)

// p2pIdentityKeyPath implements the p2p identity key path helper.
func p2pIdentityKeyPath(dataDir, nodeID string) string {
	// `base` stores the value produced by this operation.
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = "."
	}
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(nodeID)
	if id == "" {
		id = "node"
	}
	return filepath.Join(nodeDataPath(base, id), "p2p_identity.key")
}

// loadOrCreateP2PIdentityKey implements the load or create p2 p identity key helper.
func loadOrCreateP2PIdentityKey(dataDir, nodeID string) (libp2pcrypto.PrivKey, error) {
	// `path` stores the value produced by this operation.
	path := p2pIdentityKeyPath(dataDir, nodeID)
	// `dir` stores the value produced by this operation.
	dir := filepath.Dir(path)
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create p2p identity dir %s: %w", dir, err)
	}

	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(path); err == nil {
		// `key` and `err` store the error produced by this operation.
		key, err := libp2pcrypto.UnmarshalPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse p2p identity key %s: %w", path, err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read p2p identity key %s: %w", path, err)
	}

	// `key` and `err` store the error produced by this operation.
	key, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate p2p identity key: %w", err)
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := libp2pcrypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal p2p identity key: %w", err)
	}
	// `err` stores the error produced by this operation.
	if err := writeFileAtomic(path, raw, 0o600); err != nil {
		// `statErr` stores the error produced by this operation.
		if _, statErr := os.Stat(path); statErr == nil {
			// `raw` and `readErr` store the error produced by this operation.
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("failed to read p2p identity key %s: %w", path, readErr)
			}
			// `key` and `parseErr` store the error produced by this operation.
			key, parseErr := libp2pcrypto.UnmarshalPrivateKey(raw)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse p2p identity key %s: %w", path, parseErr)
			}
			return key, nil
		}
		return nil, fmt.Errorf("failed to persist p2p identity key %s: %w", path, err)
	}
	return key, nil
}

// writeFileAtomic implements the write file atomic helper.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	// `dir` stores the value produced by this operation.
	dir := filepath.Dir(path)
	// `tmp` and `err` store the error produced by this operation.
	tmp, err := os.CreateTemp(dir, "p2p_identity_*.tmp")
	if err != nil {
		return err
	}
	// `tmpName` stores the value produced by this operation.
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	// `err` stores the error produced by this operation.
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	// `err` stores the error produced by this operation.
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// `err` stores the error produced by this operation.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	// `err` stores the error produced by this operation.
	if err := tmp.Close(); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// `dirHandle` and `err` store the error produced by this operation.
	dirHandle, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer dirHandle.Close()
	// Best effort: POSIX filesystems need the directory fsync to durably record
	// the rename. Some platforms reject directory sync; the file itself is
	// already synced above, so do not turn that portability detail into a write
	// failure.
	_ = dirHandle.Sync()
	return nil
}
