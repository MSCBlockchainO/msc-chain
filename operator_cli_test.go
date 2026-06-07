package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOperatorWalletFromPrivateKeyHexAcceptsSeedAndPrivateKey(t *testing.T) {
	password := "operator-pass"
	seed := make([]byte, ed25519.SeedSize)
	if _, err := cryptorand.Read(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	fromSeed, err := operatorWalletFromPrivateKeyHex(hex.EncodeToString(seed), password)
	if err != nil {
		t.Fatalf("import seed: %v", err)
	}
	if fromSeed.PublicKey != hex.EncodeToString(pub) {
		t.Fatalf("seed import public key mismatch")
	}

	fromPrivate, err := operatorWalletFromPrivateKeyHex("0x"+hex.EncodeToString(priv), password)
	if err != nil {
		t.Fatalf("import private key: %v", err)
	}
	if fromPrivate.Address != fromSeed.Address || fromPrivate.PublicKey != fromSeed.PublicKey {
		t.Fatalf("seed/private import mismatch: seed=%+v private=%+v", fromSeed, fromPrivate)
	}
	if _, err := DecryptPrivateKey(fromPrivate, password); err != nil {
		t.Fatalf("decrypt imported wallet: %v", err)
	}
}

func TestOperatorEndpointNormalizesBaseURL(t *testing.T) {
	got, err := operatorEndpoint("127.0.0.1:26657", "/v1/peers", nil)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if got != "http://127.0.0.1:26657/v1/peers" {
		t.Fatalf("unexpected endpoint %q", got)
	}
}

func TestOperatorCLICommandRecognition(t *testing.T) {
	for _, cmd := range []string{"wallet", "validator-keygen", "validator-pubkey", "validator", "stake", "unstake", "claim-rewards", "status", "peers", "sync-status", "setup", "install", "doctor", "service", "start", "stop", "repair", "update", "restore", "uninstall", "backup", "snapshot", "indexer"} {
		if !isOperatorCLICommand(cmd) {
			t.Fatalf("expected %s to be an operator command", cmd)
		}
	}
	if isOperatorCLICommand("--mode") || isOperatorCLICommand("node") || strings.TrimSpace(" ") != "" {
		t.Fatalf("unexpected operator command recognition")
	}
}

func TestOperatorSetupDryRunDispatchesInstaller(t *testing.T) {
	err := operatorSetupCommand([]string{
		"validator",
		"--id", "HOME1",
		"--low-ram",
		"--auto-start",
		"--source", "local",
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("setup dry-run: %v", err)
	}
}

func TestOperatorNormalizeSetupSource(t *testing.T) {
	if got := operatorNormalizeSetupSource("auto", "", ""); got != "local" {
		t.Fatalf("auto without release config = %q", got)
	}
	if got := operatorNormalizeSetupSource("auto", "https://example.test/manifest.json", "abc"); got != "release" {
		t.Fatalf("auto with release config = %q", got)
	}
	if got := operatorNormalizeSetupSource("bad", "", ""); got != "" {
		t.Fatalf("bad source should be rejected, got %q", got)
	}
}

func TestOperatorReleaseArtifactVerificationChecksumAndSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("msc release artifact")
	sum := sha256.Sum256(data)
	sumHex := hex.EncodeToString(sum[:])
	sig := ed25519.Sign(priv, []byte(sumHex))
	artifact := operatorReleaseArtifact{
		OS:        "linux",
		Arch:      "amd64",
		File:      "msc",
		SHA256:    sumHex,
		Signature: hex.EncodeToString(sig),
	}
	ok, err := operatorVerifyReleaseArtifactBytes(artifact, data, hex.EncodeToString(pub))
	if err != nil {
		t.Fatalf("verify release: %v", err)
	}
	if !ok {
		t.Fatalf("expected signature verification")
	}
	artifact.SHA256 = strings.Repeat("0", 64)
	if _, err := operatorVerifyReleaseArtifactBytes(artifact, data, ""); err == nil {
		t.Fatalf("expected checksum mismatch")
	}
}

func TestOperatorSelectReleaseArtifactResolvesRelativeURL(t *testing.T) {
	manifest := []byte(`{"artifacts":[{"os":"linux","arch":"amd64","file":"msc-linux-amd64","sha256":"` + strings.Repeat("a", 64) + `"}]}`)
	artifact, artifactURL, err := operatorSelectReleaseArtifact(manifest, "https://example.test/releases/v1/manifest.json", "linux", "amd64")
	if err != nil {
		t.Fatalf("select release artifact: %v", err)
	}
	if artifact.File != "msc-linux-amd64" {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
	if artifactURL != "https://example.test/releases/v1/msc-linux-amd64" {
		t.Fatalf("unexpected artifact URL %q", artifactURL)
	}
}

func TestOperatorDoctorCollectsLocalAndRPCChecks(t *testing.T) {
	root := t.TempDir()
	genesis := filepath.Join(root, "genesis.json")
	if err := os.WriteFile(genesis, []byte(`{"chain_id":"91938"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(`{"chain_id":"91938"}`))
	config := filepath.Join(root, "config.toml")
	configText := "[chain]\ngenesis_hash = \"" + hex.EncodeToString(sum[:]) + "\"\n\n[rpc]\nladdr = \"127.0.0.1:26657\"\n"
	if err := os.WriteFile(config, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(root, "node_A")
	if err := ensurePrivateDirectory(nodePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodePath, "validator.sec"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status":
			_, _ = w.Write([]byte(`{"height":12,"finalized_height":12}`))
		case "/v1/peers":
			_, _ = w.Write([]byte(`{"count":3}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	report := operatorCollectDoctorReport("A", "validator", root, nodePath, config, genesis, &operatorRPCFlags{rpc: ts.URL, timeout: time.Second})
	if report.Result == "error" {
		t.Fatalf("unexpected doctor error: %+v", report.Checks)
	}
	if len(report.Checks) == 0 {
		t.Fatalf("expected doctor checks")
	}
}

func TestOperatorServiceStatusDryRun(t *testing.T) {
	if err := operatorServiceCommand([]string{"status", "--install-dir", t.TempDir(), "--dry-run"}); err != nil {
		t.Fatalf("service status dry-run: %v", err)
	}
}

func TestOperatorBackupWizardJSON(t *testing.T) {
	root := t.TempDir()
	nodePath := filepath.Join(root, "node_A")
	if err := ensurePrivateDirectory(nodePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodePath, "validator.sec"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := operatorBackupWizard([]string{"--id", "A", "--datadir", root, "--json"}); err != nil {
		t.Fatalf("backup wizard: %v", err)
	}
}

func TestOperatorInstallManifestRoundTripAndProtectedData(t *testing.T) {
	installDir := t.TempDir()
	nodePath := filepath.Join(installDir, "data", "node_HOME1")
	if err := ensurePrivateDirectory(nodePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodePath, "validator.sec"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := operatorInstallManifest{
		SchemaVersion:   1,
		NodeID:          "HOME1",
		Role:            "validator",
		InstallDir:      installDir,
		DataDir:         filepath.Join(installDir, "data"),
		NodePath:        nodePath,
		ConfigPath:      filepath.Join(installDir, "config.toml"),
		BinaryPath:      filepath.Join(installDir, operatorBinaryName()),
		AliasPath:       filepath.Join(installDir, operatorAliasBinaryName()),
		GenesisHash:     GenesisHashExpected,
		ServiceName:     operatorServiceName("HOME1"),
		OS:              "test",
		Arch:            "test",
		Source:          "local",
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
		ValidatorPubkey: strings.Repeat("a", 64),
	}
	if err := operatorWriteInstallManifest(installDir, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, ok := operatorReadInstallManifest(installDir)
	if !ok {
		t.Fatalf("expected manifest")
	}
	if got.NodeID != "HOME1" || got.Role != "validator" {
		t.Fatalf("unexpected manifest: %+v", got)
	}
	if !operatorInstallHasProtectedData(installDir) {
		t.Fatalf("expected protected data")
	}
	preserved := operatorInstallPreservationReport(installDir)
	if preserved["keys"] != true || preserved["protected"] != true {
		t.Fatalf("unexpected preservation report: %+v", preserved)
	}
}

func TestOperatorBackupBundleAndRestoreRefusesWrongPubkey(t *testing.T) {
	root := t.TempDir()
	nodePath := filepath.Join(root, "node_A")
	if err := ensurePrivateDirectory(nodePath); err != nil {
		t.Fatal(err)
	}
	pubA, privA, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer ZeroMemory(privA)
	if err := os.WriteFile(filepath.Join(nodePath, "validator.sec"), []byte("secret-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validatorPublicPath(nodePath), []byte(hex.EncodeToString(pubA)), 0o644); err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(t.TempDir(), "backups")
	if err := operatorBackupBundle([]string{"--id", "A", "--datadir", root, "--out", backupRoot, "--json"}); err != nil {
		t.Fatalf("backup bundle: %v", err)
	}
	entries, err := os.ReadDir(backupRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one backup bundle, entries=%d err=%v", len(entries), err)
	}
	bundle := filepath.Join(backupRoot, entries[0].Name())
	installDir := t.TempDir()
	restoreNodePath := filepath.Join(installDir, "data", "node_A")
	if err := ensurePrivateDirectory(restoreNodePath); err != nil {
		t.Fatal(err)
	}
	pubB, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validatorPublicPath(restoreNodePath), []byte(hex.EncodeToString(pubB)), 0o644); err != nil {
		t.Fatal(err)
	}
	err = operatorRestoreCommand([]string{"--id", "A", "--install-dir", installDir, "--backup", bundle, "--dry-run"})
	if err == nil {
		t.Fatalf("expected restore to refuse pubkey mismatch")
	}
	if err := operatorRestoreCommand([]string{"--id", "A", "--install-dir", installDir, "--backup", bundle, "--replace-key", "--confirm-validator-pubkey", hex.EncodeToString(pubA), "--dry-run"}); err != nil {
		t.Fatalf("restore with confirmation: %v", err)
	}
}

func TestOperatorUninstallRequiresConfirmationForPurge(t *testing.T) {
	installDir := t.TempDir()
	if err := operatorWriteInstallManifest(installDir, operatorInstallManifest{NodeID: "HOME1", Role: "validator"}); err != nil {
		t.Fatal(err)
	}
	if err := operatorUninstallCommand([]string{"--id", "HOME1", "--install-dir", installDir, "--purge-data", "--dry-run"}); err == nil {
		t.Fatalf("expected purge confirmation error")
	}
	if err := operatorUninstallCommand([]string{"--id", "HOME1", "--install-dir", installDir, "--purge-data", "--confirm-delete-node-id", "HOME1", "--dry-run"}); err != nil {
		t.Fatalf("confirmed purge dry-run: %v", err)
	}
}

func TestOperatorStatusAliasKeepsExplicitRPCMode(t *testing.T) {
	if !operatorStatusArgsAreRPC([]string{"--rpc", "http://127.0.0.1:26657"}) {
		t.Fatalf("expected explicit --rpc to use RPC status")
	}
	if operatorStatusArgsAreRPC(nil) {
		t.Fatalf("plain status should be local service status")
	}
}

func TestOperatorMPCKeygenCreatesSharesWithoutValidatorSec(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "mpc")
	t.Setenv(operatorMPCSharePasswordEnv, "mpc-share-pass")
	if err := operatorValidatorMPCKeygenCommand([]string{"--validator", "F", "--threshold", "2", "--participants", "3", "--outdir", outDir}); err != nil {
		t.Fatalf("mpc-keygen: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "validator.sec")); !os.IsNotExist(err) {
		t.Fatalf("mpc-keygen must not create validator.sec, err=%v", err)
	}
	pubRaw, err := os.ReadFile(filepath.Join(outDir, "validator.pub"))
	if err != nil {
		t.Fatalf("validator.pub missing: %v", err)
	}
	pub := normalizeConsensusPubKeyHex(string(pubRaw))
	if pub == "" {
		t.Fatalf("invalid mpc public key file")
	}
	_, seed, ref, err := reconstructValidatorMPCSeedFromFiles([]string{
		filepath.Join(outDir, "share1.sec"),
		filepath.Join(outDir, "share2.sec"),
	}, "mpc-share-pass")
	if err != nil {
		t.Fatalf("reconstruct mpc seed: %v", err)
	}
	defer ZeroMemory(seed)
	if ref.ValidatorID != "F" || ref.Threshold != 2 || ref.Participants != 3 {
		t.Fatalf("unexpected mpc ref: %+v", ref)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	got := hex.EncodeToString(priv.Public().(ed25519.PublicKey))
	if got != pub {
		t.Fatalf("reconstructed public key mismatch: got=%s want=%s", got, pub)
	}
}

func TestOperatorMPCImportKeyPreservesExistingValidatorPubkey(t *testing.T) {
	root := t.TempDir()
	nodePath := filepath.Join(root, "node_A")
	if err := ensurePrivateDirectory(nodePath); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer ZeroMemory(priv)
	enc, err := EncryptPrivateKey(priv, "validator-pass")
	if err != nil {
		t.Fatal(err)
	}
	w := SecureWallet{Address: "A", PublicKey: hex.EncodeToString(pub), Crypto: enc}
	raw, _ := json.MarshalIndent(w, "", "  ")
	if err := writePrivateFile(filepath.Join(nodePath, "validator.sec"), raw); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validatorPasswordEnv, "validator-pass")
	t.Setenv(operatorMPCSharePasswordEnv, "mpc-share-pass")
	outDir := filepath.Join(root, "mpc")
	if err := operatorValidatorMPCImportKeyCommand([]string{"--id", "A", "--nodepath", nodePath, "--threshold", "2", "--participants", "3", "--outdir", outDir}); err != nil {
		t.Fatalf("mpc-import-key: %v", err)
	}
	pubRaw, err := os.ReadFile(filepath.Join(outDir, "validator.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if normalizeConsensusPubKeyHex(string(pubRaw)) != hex.EncodeToString(pub) {
		t.Fatalf("mpc import changed validator public key")
	}
	_, seed, _, err := reconstructValidatorMPCSeedFromFiles([]string{filepath.Join(outDir, "share1.sec"), filepath.Join(outDir, "share2.sec")}, "mpc-share-pass")
	if err != nil {
		t.Fatalf("reconstruct imported shares: %v", err)
	}
	defer ZeroMemory(seed)
	gotPriv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(gotPriv)
	if hex.EncodeToString(gotPriv.Public().(ed25519.PublicKey)) != hex.EncodeToString(pub) {
		t.Fatalf("imported shares reconstruct wrong public key")
	}
}

func TestOperatorMPCSignCommandSignsExternalSignerRequest(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "mpc")
	t.Setenv(operatorMPCSharePasswordEnv, "mpc-share-pass")
	result, err := writeValidatorMPCShares("F", outDir, 2, 3, "mpc-share-pass", false)
	if err != nil {
		t.Fatalf("write shares: %v", err)
	}
	payload := []byte("msc mpc signer payload")
	req := validatorHSMRequest{
		Domain:       "msc-validator-mpc-ed25519-v1",
		SignerMode:   "mpc",
		ValidatorID:  "F",
		Provider:     "threshold_ed25519",
		PublicKeyHex: result.PublicKeyHex,
		PayloadHex:   hex.EncodeToString(payload),
		Threshold:    2,
		Participants: 3,
	}
	raw, _ := json.Marshal(req)
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if err := operatorValidatorMPCSignCommand([]string{"--shares", filepath.Join(outDir, "share1.sec") + "," + filepath.Join(outDir, "share2.sec")}); err != nil {
		t.Fatalf("mpc-sign: %v", err)
	}
}

func TestOperatorMPCSignCommandAcceptsPasswordFile(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "mpc")
	if _, err := writeValidatorMPCShares("F", outDir, 2, 3, "mpc-share-pass", false); err != nil {
		t.Fatalf("write shares: %v", err)
	}
	passFile := filepath.Join(outDir, "share.pass")
	if err := os.WriteFile(passFile, []byte("mpc-share-pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := operatorReadMPCSharePassword("", passFile)
	if err != nil {
		t.Fatalf("read password file: %v", err)
	}
	if got != "mpc-share-pass" {
		t.Fatalf("password file read mismatch")
	}
}

func TestValidatorMPCSignRequestAcceptsUTF8BOM(t *testing.T) {
	req := []byte{0xef, 0xbb, 0xbf}
	req = append(req, []byte(`{"public_key_hex":"`+strings.Repeat("11", ed25519.PublicKeySize)+`","payload_hex":"abcd"}`)...)
	got, err := validatorMPCSignRequestFromReader(strings.NewReader(string(req)))
	if err != nil {
		t.Fatalf("parse BOM signer request: %v", err)
	}
	if got.PayloadHex != "abcd" {
		t.Fatalf("unexpected request: %+v", got)
	}
}

func TestOperatorRecoveryNodeLocationFromNodePath(t *testing.T) {
	root := t.TempDir()
	nodePath := filepath.Join(root, "node_RESTORE")
	base, id, gotPath, err := operatorRecoveryNodeLocation("", "", nodePath)
	if err != nil {
		t.Fatalf("operatorRecoveryNodeLocation: %v", err)
	}
	if id != "RESTORE" || base != root || gotPath != nodePath {
		t.Fatalf("unexpected location base=%q id=%q path=%q", base, id, gotPath)
	}
}
