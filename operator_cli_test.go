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
	for _, cmd := range []string{"wallet", "validator-keygen", "validator-pubkey", "validator", "stake", "unstake", "claim-rewards", "status", "peers", "sync-status", "backup", "snapshot"} {
		if !isOperatorCLICommand(cmd) {
			t.Fatalf("expected %s to be an operator command", cmd)
		}
	}
	if isOperatorCLICommand("--mode") || isOperatorCLICommand("node") || strings.TrimSpace(" ") != "" {
		t.Fatalf("unexpected operator command recognition")
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
