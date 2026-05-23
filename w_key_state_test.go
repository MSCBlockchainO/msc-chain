package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasExistingNodeState_PeersOnlyDoesNotBlockBootstrap(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "peers.json"), []byte(`{"peers":[]}`), 0o600); err != nil {
		t.Fatalf("write peers.json: %v", err)
	}
	if hasExistingNodeState(base) {
		t.Fatalf("expected peers-only directory to be treated as fresh state")
	}
}

func TestHasExistingNodeState_ValidatorsOnlyDoesNotBlockBootstrap(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "validators.json"), []byte(`{"validators":[]}`), 0o600); err != nil {
		t.Fatalf("write validators.json: %v", err)
	}
	if hasExistingNodeState(base) {
		t.Fatalf("expected validators-only directory to be treated as fresh state")
	}
}

func TestHasExistingNodeState_BlocksDBNonEmptyIsExisting(t *testing.T) {
	base := t.TempDir()
	blocksDir := filepath.Join(base, "blocks.db")
	if err := os.MkdirAll(blocksDir, 0o700); err != nil {
		t.Fatalf("mkdir blocks.db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocksDir, "CURRENT"), []byte("1"), 0o600); err != nil {
		t.Fatalf("write blocks marker: %v", err)
	}
	if !hasExistingNodeState(base) {
		t.Fatalf("expected non-empty blocks.db to be treated as existing state")
	}
}

func TestHasExistingNodeState_LedgerFileIsExisting(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "ledger.json"), []byte(`{"height":1}`), 0o600); err != nil {
		t.Fatalf("write ledger.json: %v", err)
	}
	if !hasExistingNodeState(base) {
		t.Fatalf("expected ledger.json to be treated as existing state")
	}
}

