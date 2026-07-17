package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
)

func p2pIdentityKeyPath(dataDir, nodeID string) string {
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = "."
	}
	id := strings.TrimSpace(nodeID)
	if id == "" {
		id = "node"
	}
	return filepath.Join(base, "node_"+id, "p2p_identity.key")
}

func loadOrCreateP2PIdentityKey(dataDir, nodeID string) (libp2pcrypto.PrivKey, error) {
	path := p2pIdentityKeyPath(dataDir, nodeID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create p2p identity dir %s: %w", dir, err)
	}

	if raw, err := os.ReadFile(path); err == nil {
		key, err := libp2pcrypto.UnmarshalPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse p2p identity key %s: %w", path, err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read p2p identity key %s: %w", path, err)
	}

	key, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate p2p identity key: %w", err)
	}
	raw, err := libp2pcrypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal p2p identity key: %w", err)
	}
	if err := writeFileAtomic(path, raw, 0o600); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("failed to read p2p identity key %s: %w", path, readErr)
			}
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

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "p2p_identity_*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
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
