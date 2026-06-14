package main

import (
	"os"
	"path/filepath"
	"testing"
)

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

func TestResolveConfigPathExplicitWins(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, "config.A.toml"), []byte(""), 0600); err != nil {
		t.Fatalf("write config.A.toml: %v", err)
	}
	_, err := resolveConfigPath("custom.toml", "A", true)
	if err == nil {
		t.Fatalf("expected explicit non-config.toml to fail")
	}
}

func TestResolveConfigPathExplicitOverrideAllowed(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	t.Setenv("MSC_ALLOW_CONFIG_OVERRIDE", "1")
	got, err := resolveConfigPath("runtime-data/distributed/A/config.mpc.toml", "A", true)
	if err != nil {
		t.Fatalf("resolve config override: %v", err)
	}
	if got != "runtime-data/distributed/A/config.mpc.toml" {
		t.Fatalf("expected explicit override config path, got %q", got)
	}
}

func TestResolveConfigPathAllowsNodeScopedMPCConfig(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	got, err := resolveConfigPath("runtime-data/distributed/A/config.mpc.toml", "A", true)
	if err != nil {
		t.Fatalf("resolve node-scoped mpc config: %v", err)
	}
	if got != "runtime-data/distributed/A/config.mpc.toml" {
		t.Fatalf("expected node-scoped mpc config path, got %q", got)
	}
}

func TestResolveConfigPathDoesNotAutoSelectPerNode(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, "config.A.toml"), []byte(""), 0600); err != nil {
		t.Fatalf("write config.A.toml: %v", err)
	}
	got, err := resolveConfigPath("config.toml", "A", false)
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if got != "config.toml" {
		t.Fatalf("expected default config.toml, got %q", got)
	}
}

func TestResolveConfigPathFallsBackToDefault(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	got, err := resolveConfigPath("config.toml", "A", false)
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if got != "config.toml" {
		t.Fatalf("expected default config fallback, got %q", got)
	}
}

func TestResolveConfigPathReturnsNonExplicitCustomArg(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	got, err := resolveConfigPath("custom.toml", "A", false)
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if got != "config.toml" {
		t.Fatalf("expected single-config fallback to config.toml, got %q", got)
	}
}

func TestResolveConfigPathExplicitConfigTomlAllowed(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	got, err := resolveConfigPath("config.toml", "A", true)
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if got != "config.toml" {
		t.Fatalf("expected config.toml in explicit mode, got %q", got)
	}
}

func TestResolveConfigPathEmptyAlwaysConfigToml(t *testing.T) {
	tmp := t.TempDir()
	withWorkingDir(t, tmp)
	got, err := resolveConfigPath("", "A", false)
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if got != "config.toml" {
		t.Fatalf("expected empty path to resolve config.toml, got %q", got)
	}
}
