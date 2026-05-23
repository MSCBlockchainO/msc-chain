package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type validatorKeyConfigSnapshot struct {
	allowRotation     bool
	restoreOnMissing  bool
	backupRequired    bool
	requiredFP        string
	backupDir         string
	backupMaxAgeHours uint64
	allowEnvProd      bool
	isTestnet         bool
}

func snapshotValidatorKeyConfig() validatorKeyConfigSnapshot {
	return validatorKeyConfigSnapshot{
		allowRotation:     ValidatorAllowIdentityRotationOnExistingChain,
		restoreOnMissing:  ValidatorKeyRestoreAllowedOnMissing,
		backupRequired:    ValidatorKeyBackupRequired,
		requiredFP:        ValidatorRequiredKeyFingerprint,
		backupDir:         ValidatorKeyBackupDir,
		backupMaxAgeHours: ValidatorKeyBackupMaxAgeHours,
		allowEnvProd:      ValidatorAllowEnvPasswordInProduction,
		isTestnet:         IsTestnet,
	}
}

func (s validatorKeyConfigSnapshot) restore() {
	ValidatorAllowIdentityRotationOnExistingChain = s.allowRotation
	ValidatorKeyRestoreAllowedOnMissing = s.restoreOnMissing
	ValidatorKeyBackupRequired = s.backupRequired
	ValidatorRequiredKeyFingerprint = s.requiredFP
	ValidatorKeyBackupDir = s.backupDir
	ValidatorKeyBackupMaxAgeHours = s.backupMaxAgeHours
	ValidatorAllowEnvPasswordInProduction = s.allowEnvProd
	IsTestnet = s.isTestnet
}

func writeEncryptedValidatorFile(t *testing.T, nodeID, keyPath, password string) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	enc, err := EncryptPrivateKey(priv, password)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	sw := SecureWallet{
		Address:   nodeID,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    enc,
	}
	raw, err := json.MarshalIndent(sw, "", "  ")
	if err != nil {
		t.Fatalf("marshal secure wallet: %v", err)
	}
	if err := writePrivateFile(keyPath, raw); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return pub, priv
}

func TestLoadOrCreateValidatorKey_CreateOnFreshNodeWithExplicitOverride(t *testing.T) {
	snap := snapshotValidatorKeyConfig()
	defer snap.restore()

	IsTestnet = true
	ValidatorKeyBackupRequired = true
	ValidatorKeyRestoreAllowedOnMissing = true
	ValidatorRequiredKeyFingerprint = ""

	nodePath := t.TempDir()
	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	t.Setenv(validatorKeyCreateOverride, "1")

	v := LoadOrCreateValidatorKey("V1", nodePath)
	if !isValidatorKeyUsable(v) {
		t.Fatalf("expected generated validator key to be usable")
	}
	if _, err := os.Stat(filepath.Join(nodePath, "validator.sec")); err != nil {
		t.Fatalf("validator.sec not written: %v", err)
	}
}

func TestLoadOrCreateValidatorKey_RestoreFromBackupOnExistingState(t *testing.T) {
	snap := snapshotValidatorKeyConfig()
	defer snap.restore()

	IsTestnet = true
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorKeyRestoreAllowedOnMissing = true
	ValidatorKeyBackupRequired = true
	ValidatorRequiredKeyFingerprint = ""
	ValidatorKeyBackupDir = "secure-backups"

	nodePath := t.TempDir()
	blocksDir := filepath.Join(nodePath, "blocks.db")
	if err := os.MkdirAll(blocksDir, 0o700); err != nil {
		t.Fatalf("mkdir blocks.db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocksDir, "CURRENT"), []byte("1"), 0o600); err != nil {
		t.Fatalf("write blocks marker: %v", err)
	}

	backupPath := defaultValidatorKeyBackupPath(nodePath)
	pub, _ := writeEncryptedValidatorFile(t, "V2", backupPath, "StrongPass123!")
	sum, err := fileSHA256Hex(backupPath)
	if err != nil {
		t.Fatalf("hash backup: %v", err)
	}
	manifest := validatorKeyBackupManifest{
		BackupPath:     toManifestPath(nodePath, backupPath),
		BackupSHA256:   sum,
		Fingerprint:    validatorKeyFingerprint(pub),
		UpdatedAt:      1,
		LastVerifiedAt: 1,
	}
	if err := writeValidatorKeyBackupManifest(nodePath, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	v := LoadOrCreateValidatorKey("V2", nodePath)
	if !isValidatorKeyUsable(v) {
		t.Fatalf("expected restored validator key to be usable")
	}
	meta, ok := getValidatorKeyLoadMeta("V2")
	if !ok {
		t.Fatalf("expected load metadata")
	}
	if meta.Source != "restored" {
		t.Fatalf("expected source restored, got %q", meta.Source)
	}
	if _, err := os.Stat(filepath.Join(nodePath, "validator.sec")); err != nil {
		t.Fatalf("restored validator.sec not found: %v", err)
	}
}

func TestLoadOrCreateValidatorKey_MissingKeyExistingStateRestoreFail(t *testing.T) {
	snap := snapshotValidatorKeyConfig()
	defer snap.restore()

	IsTestnet = true
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorKeyRestoreAllowedOnMissing = true
	ValidatorKeyBackupRequired = true

	nodePath := t.TempDir()
	blocksDir := filepath.Join(nodePath, "blocks.db")
	if err := os.MkdirAll(blocksDir, 0o700); err != nil {
		t.Fatalf("mkdir blocks.db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocksDir, "CURRENT"), []byte("1"), 0o600); err != nil {
		t.Fatalf("write blocks marker: %v", err)
	}

	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	v := LoadOrCreateValidatorKey("V3", nodePath)
	if isValidatorKeyUsable(v) {
		t.Fatalf("expected restore failure to keep key unusable")
	}
	meta, ok := getValidatorKeyLoadMeta("V3")
	if !ok {
		t.Fatalf("expected load metadata")
	}
	if !strings.Contains(meta.ErrorReason, "missing_key_restore_failed") {
		t.Fatalf("expected restore-failed reason, got %q", meta.ErrorReason)
	}
}

func TestLoadOrCreateValidatorKey_FingerprintMismatchFailsStrict(t *testing.T) {
	snap := snapshotValidatorKeyConfig()
	defer snap.restore()

	IsTestnet = true
	ValidatorKeyBackupRequired = false

	nodePath := t.TempDir()
	keyPath := filepath.Join(nodePath, "validator.sec")
	pub, _ := writeEncryptedValidatorFile(t, "V4", keyPath, "StrongPass123!")
	actualFP := validatorKeyFingerprint(pub)
	ValidatorRequiredKeyFingerprint = "deadbeefcafebabe"
	if strings.EqualFold(actualFP, ValidatorRequiredKeyFingerprint) {
		t.Fatalf("test fingerprint unexpectedly matched")
	}

	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	v := LoadOrCreateValidatorKey("V4", nodePath)
	if isValidatorKeyUsable(v) {
		t.Fatalf("expected fingerprint mismatch to fail")
	}
}

func TestValidateValidatorBackup_ChecksumMismatch(t *testing.T) {
	snap := snapshotValidatorKeyConfig()
	defer snap.restore()

	nodePath := t.TempDir()
	keyPath := filepath.Join(nodePath, "validator.sec")
	pub, _ := writeEncryptedValidatorFile(t, "V5", keyPath, "StrongPass123!")
	fp := validatorKeyFingerprint(pub)

	backupPath := defaultValidatorKeyBackupPath(nodePath)
	if err := copyFilePrivate(backupPath, keyPath); err != nil {
		t.Fatalf("copy backup: %v", err)
	}
	manifest := validatorKeyBackupManifest{
		BackupPath:     toManifestPath(nodePath, backupPath),
		BackupSHA256:   strings.Repeat("0", 64),
		Fingerprint:    fp,
		UpdatedAt:      1,
		LastVerifiedAt: 1,
	}
	if err := writeValidatorKeyBackupManifest(nodePath, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_, _, err := validateValidatorBackup(nodePath, fp)
	if err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
}

func TestValidateNewValidatorPassword_WeakRejected(t *testing.T) {
	if err := validateNewValidatorPassword("m"); err == nil {
		t.Fatalf("expected weak password rejection")
	}
	if err := validateNewValidatorPassword("StrongPass123!"); err != nil {
		t.Fatalf("expected strong password to pass: %v", err)
	}
}
