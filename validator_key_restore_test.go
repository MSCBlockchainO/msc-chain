package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreValidatorKeyFromLegacyParentBackup(t *testing.T) {
	base := t.TempDir()
	nodePath := filepath.Join(base, "node_msc_legacy")
	keyPath := filepath.Join(nodePath, "validator.sec")
	backupPath := filepath.Join(base, "secure-backups", validatorKeyBackupFileName)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0700); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	want := []byte(`{"legacy":true}`)
	if err := os.WriteFile(backupPath, want, 0600); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	oldBackupDir := ValidatorKeyBackupDir
	ValidatorKeyBackupDir = "secure-backups"
	t.Cleanup(func() { ValidatorKeyBackupDir = oldBackupDir })

	if err := restoreValidatorKeyFromBackup("msc_legacy", nodePath, keyPath); err != nil {
		t.Fatalf("restore backup: %v", err)
	}
	got, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read restored key: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("restored key = %q, want %q", got, want)
	}
}

func TestLoadOrCreateValidatorKeyLoadsLegacyPlaintextFile(t *testing.T) {
	base := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	legacy := legacyValidatorKeyFile{
		NodeID:      "msc_legacy",
		ValidatorID: "VAL_LEGACY",
		PublicKey:   hex.EncodeToString(pub),
		PrivateKey:  hex.EncodeToString(priv),
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "validator.sec"), raw, 0600); err != nil {
		t.Fatalf("write legacy key: %v", err)
	}

	got := LoadOrCreateValidatorKey("msc_legacy", base)
	if !isValidatorKeyUsable(got) {
		t.Fatalf("legacy key was not loaded")
	}
	if !got.PublicKey.Equal(pub) {
		t.Fatalf("public key mismatch after legacy load")
	}
}
