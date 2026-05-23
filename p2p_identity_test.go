package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestP2PIdentityKeyPersists(t *testing.T) {
	dir := t.TempDir()
	nodeID := "A"

	key1, err := loadOrCreateP2PIdentityKey(dir, nodeID)
	if err != nil {
		t.Fatalf("loadOrCreateP2PIdentityKey first: %v", err)
	}
	key2, err := loadOrCreateP2PIdentityKey(dir, nodeID)
	if err != nil {
		t.Fatalf("loadOrCreateP2PIdentityKey second: %v", err)
	}
	id1, err := peer.IDFromPrivateKey(key1)
	if err != nil {
		t.Fatalf("peer.IDFromPrivateKey first: %v", err)
	}
	id2, err := peer.IDFromPrivateKey(key2)
	if err != nil {
		t.Fatalf("peer.IDFromPrivateKey second: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected stable peer id, got %s vs %s", id1, id2)
	}

	path := p2pIdentityKeyPath(dir, nodeID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat identity key: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected identity key file to be non-empty")
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("expected 0600 perms, got %v", info.Mode().Perm())
		}
	}
}

func TestP2PIdentityKeyUnreadableFails(t *testing.T) {
	dir := t.TempDir()
	nodeID := "B"
	path := p2pIdentityKeyPath(dir, nodeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir identity dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not-a-key"), 0o600); err != nil {
		t.Fatalf("write identity key: %v", err)
	}
	if _, err := loadOrCreateP2PIdentityKey(dir, nodeID); err == nil {
		t.Fatalf("expected error for unreadable identity key")
	}
}
