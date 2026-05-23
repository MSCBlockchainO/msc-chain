package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnforceValidatorFingerprintLock_CreateAndMatch(t *testing.T) {
	nodePath := t.TempDir()
	fp := "0011223344556677"
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		t.Fatalf("create lock: %v", err)
	}
	raw, err := os.ReadFile(validatorFingerprintLockPath(nodePath))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if strings.TrimSpace(string(raw)) != fp {
		t.Fatalf("lock mismatch: got=%q want=%q", strings.TrimSpace(string(raw)), fp)
	}
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		t.Fatalf("re-check lock: %v", err)
	}
}

func TestEnforceValidatorFingerprintLock_MismatchFails(t *testing.T) {
	nodePath := t.TempDir()
	if err := enforceValidatorFingerprintLock(nodePath, "aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	err := enforceValidatorFingerprintLock(nodePath, "bbbbbbbbbbbbbbbb")
	if err == nil {
		t.Fatalf("expected mismatch error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadOrCreateValidatorKey_WritesPublicAndLockArtifacts(t *testing.T) {
	snap := snapshotValidatorKeyConfig()
	defer snap.restore()

	IsTestnet = true
	ValidatorKeyBackupRequired = true
	ValidatorKeyRestoreAllowedOnMissing = true
	ValidatorRequiredKeyFingerprint = ""

	nodePath := t.TempDir()
	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	t.Setenv(validatorKeyCreateOverride, "1")

	v := LoadOrCreateValidatorKey("VLOCK", nodePath)
	if !isValidatorKeyUsable(v) {
		t.Fatalf("expected generated validator key to be usable")
	}
	pubRaw, err := os.ReadFile(validatorPublicPath(nodePath))
	if err != nil {
		t.Fatalf("validator.pub missing: %v", err)
	}
	if strings.TrimSpace(string(pubRaw)) != hex.EncodeToString(v.PublicKey) {
		t.Fatalf("validator.pub mismatch")
	}
	lockRaw, err := os.ReadFile(validatorFingerprintLockPath(nodePath))
	if err != nil {
		t.Fatalf("fingerprint.lock missing: %v", err)
	}
	fp := validatorKeyFingerprint(v.PublicKey)
	if strings.TrimSpace(string(lockRaw)) != fp {
		t.Fatalf("fingerprint.lock mismatch got=%q want=%q", strings.TrimSpace(string(lockRaw)), fp)
	}

	// Simulate accidental key replacement; lock must reject it deterministically.
	_, _ = writeEncryptedValidatorFile(t, "VLOCK", filepath.Join(nodePath, "validator.sec"), "StrongPass123!")
	v2 := LoadOrCreateValidatorKey("VLOCK", nodePath)
	if isValidatorKeyUsable(v2) {
		t.Fatalf("expected lock mismatch to block replaced key")
	}
}
