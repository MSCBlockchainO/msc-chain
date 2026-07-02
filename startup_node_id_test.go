package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStartupNodeIDFromConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[node]\nnode_id = \"msc_config_node\"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := loadStartupNodeID(dir); got != "msc_config_node" {
		t.Fatalf("startup node id = %q, want config id", got)
	}
}

func TestLoadStartupNodeIDFallsBackToIdentityJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "node_identity.json"), []byte(`{"node_id":"msc_identity_node"}`), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if got := loadStartupNodeID(dir); got != "msc_identity_node" {
		t.Fatalf("startup node id = %q, want identity id", got)
	}
}
