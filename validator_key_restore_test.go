package main

import (
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
