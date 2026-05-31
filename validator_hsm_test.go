package main

import (
	"bytes"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restoreValidatorHSMGlobals(t *testing.T) func() {
	t.Helper()
	prevEnabled := ValidatorHSMEnabled
	prevProvider := ValidatorHSMProvider
	prevKeyID := ValidatorHSMKeyID
	prevPub := ValidatorHSMPublicKeyHex
	prevCommand := ValidatorHSMExternalSignerCommand
	prevTimeout := ValidatorHSMTimeoutMS
	prevPresence := ValidatorHSMRequireUserPresence
	prevFingerprint := ValidatorRequiredKeyFingerprint
	prevRunner := validatorHSMExternalSignerRunner
	return func() {
		ValidatorHSMEnabled = prevEnabled
		ValidatorHSMProvider = prevProvider
		ValidatorHSMKeyID = prevKeyID
		ValidatorHSMPublicKeyHex = prevPub
		ValidatorHSMExternalSignerCommand = prevCommand
		ValidatorHSMTimeoutMS = prevTimeout
		ValidatorHSMRequireUserPresence = prevPresence
		ValidatorRequiredKeyFingerprint = prevFingerprint
		validatorHSMExternalSignerRunner = prevRunner
	}
}

func configureTestValidatorHSM(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	ValidatorHSMEnabled = true
	ValidatorHSMProvider = "yubihsm"
	ValidatorHSMKeyID = "slot-1"
	ValidatorHSMPublicKeyHex = hex.EncodeToString(pub)
	ValidatorHSMExternalSignerCommand = "test-signer"
	ValidatorHSMTimeoutMS = 3000
	ValidatorHSMRequireUserPresence = true
	ValidatorRequiredKeyFingerprint = ""
}

func TestValidatorHSMExternalSignerSignsAndVerifies(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configureTestValidatorHSM(t, pub)
	validatorHSMExternalSignerRunner = func(req validatorHSMRequest) ([]byte, error) {
		if req.Provider != "yubihsm" {
			t.Fatalf("provider = %q, want yubihsm", req.Provider)
		}
		payload, err := hex.DecodeString(req.PayloadHex)
		if err != nil {
			t.Fatal(err)
		}
		return ed25519.Sign(priv, payload), nil
	}
	n := &Node{
		ID: "H",
		ValidatorKey: ValidatorKey{
			ID:        "H",
			PublicKey: pub,
		},
	}
	payload := []byte("msc hsm sign payload")
	sig, ok := n.signValidatorPayload(payload)
	if !ok {
		t.Fatal("expected HSM signer success")
	}
	if !ed25519.Verify(pub, payload, sig) {
		t.Fatal("signature did not verify")
	}
}

func TestValidatorHSMRejectsBadExternalSignature(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configureTestValidatorHSM(t, pub)
	validatorHSMExternalSignerRunner = func(req validatorHSMRequest) ([]byte, error) {
		return bytes.Repeat([]byte{0x42}, ed25519.SignatureSize), nil
	}
	n := &Node{
		ID: "H",
		ValidatorKey: ValidatorKey{
			ID:        "H",
			PublicKey: pub,
		},
	}
	if _, ok := n.signValidatorPayload([]byte("bad signature must fail")); ok {
		t.Fatal("expected HSM signer result to be rejected")
	}
}

func TestLoadValidatorHSMKeyDoesNotRequirePrivateKey(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configureTestValidatorHSM(t, pub)
	dir := t.TempDir()
	key, handled := LoadValidatorHSMKey("H", dir)
	if !handled {
		t.Fatal("expected HSM key path to be handled")
	}
	if key.ID != "H" {
		t.Fatalf("key id = %q, want H", key.ID)
	}
	if !bytes.Equal(key.PublicKey, pub) {
		t.Fatal("loaded HSM public key mismatch")
	}
	if len(key.PrivateKey) != 0 {
		t.Fatal("HSM key must not load a private key into process memory")
	}
	if !isValidatorSigningKeyUsable(key) {
		t.Fatal("HSM public key should be usable through external signer")
	}
	if _, err := os.Stat(filepath.Join(dir, "validator.sec")); !os.IsNotExist(err) {
		t.Fatalf("validator.sec should not be created in HSM mode, err=%v", err)
	}
	if raw, err := os.ReadFile(validatorPublicPath(dir)); err != nil {
		t.Fatalf("expected validator public key file: %v", err)
	} else if got := strings.TrimSpace(string(raw)); got != hex.EncodeToString(pub) {
		t.Fatalf("validator public key file = %q, want %q", got, hex.EncodeToString(pub))
	}
}

func TestValidatorHSMStatusReasons(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	ValidatorHSMEnabled = true
	ValidatorHSMPublicKeyHex = "not-hex"
	ValidatorHSMExternalSignerCommand = "test-signer"
	st := validatorHSMStatus("H", ValidatorKey{})
	if st.Ready || st.Reason != "invalid_or_missing_public_key" {
		t.Fatalf("status = %+v", st)
	}
	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ValidatorHSMPublicKeyHex = hex.EncodeToString(pub)
	ValidatorHSMExternalSignerCommand = ""
	st = validatorHSMStatus("H", ValidatorKey{ID: "H", PublicKey: pub})
	if st.Ready || st.Reason != "missing_external_signer_command" {
		t.Fatalf("status = %+v", st)
	}
}
