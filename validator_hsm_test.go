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
	prevMPCEnabled := ValidatorMPCEnabled
	prevMPCProvider := ValidatorMPCProvider
	prevMPCKeyID := ValidatorMPCKeyID
	prevMPCPub := ValidatorMPCPublicKeyHex
	prevMPCCommand := ValidatorMPCExternalSignerCommand
	prevMPCTimeout := ValidatorMPCTimeoutMS
	prevMPCThreshold := ValidatorMPCThreshold
	prevMPCParticipants := ValidatorMPCParticipants
	prevMPCRunner := validatorMPCExternalSignerRunner
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
		ValidatorMPCEnabled = prevMPCEnabled
		ValidatorMPCProvider = prevMPCProvider
		ValidatorMPCKeyID = prevMPCKeyID
		ValidatorMPCPublicKeyHex = prevMPCPub
		ValidatorMPCExternalSignerCommand = prevMPCCommand
		ValidatorMPCTimeoutMS = prevMPCTimeout
		ValidatorMPCThreshold = prevMPCThreshold
		ValidatorMPCParticipants = prevMPCParticipants
		validatorMPCExternalSignerRunner = prevMPCRunner
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

func TestValidatorHSMModeDisablesSoftwareKeyFallback(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ValidatorHSMEnabled = true
	ValidatorHSMProvider = "external"
	ValidatorHSMKeyID = "slot-1"
	ValidatorHSMPublicKeyHex = hex.EncodeToString(pub)
	ValidatorHSMExternalSignerCommand = ""

	key := ValidatorKey{ID: "H", PublicKey: pub, PrivateKey: priv}
	if isValidatorSigningKeyUsable(key) {
		t.Fatal("hsm mode must not fall back to local private key when signer is not ready")
	}
	n := &Node{ID: "H", ValidatorKey: key}
	if _, ok := n.signValidatorPayload([]byte("must fail closed")); ok {
		t.Fatal("expected HSM mode to fail closed without external signer")
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

func TestValidatorMPCExternalSignerSignsAndVerifies(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ValidatorMPCEnabled = true
	ValidatorMPCProvider = "threshold_ed25519"
	ValidatorMPCKeyID = "cluster-F"
	ValidatorMPCPublicKeyHex = hex.EncodeToString(pub)
	ValidatorMPCExternalSignerCommand = "test-mpc-signer"
	ValidatorMPCTimeoutMS = 4000
	ValidatorMPCThreshold = 2
	ValidatorMPCParticipants = 3
	validatorMPCExternalSignerRunner = func(req validatorHSMRequest) ([]byte, error) {
		if req.Domain != "msc-validator-mpc-ed25519-v1" {
			t.Fatalf("domain = %q", req.Domain)
		}
		if req.SignerMode != "mpc" {
			t.Fatalf("signer mode = %q, want mpc", req.SignerMode)
		}
		if req.Provider != "threshold_ed25519" {
			t.Fatalf("provider = %q", req.Provider)
		}
		if req.Threshold != 2 || req.Participants != 3 {
			t.Fatalf("threshold tuple = %d/%d", req.Threshold, req.Participants)
		}
		payload, err := hex.DecodeString(req.PayloadHex)
		if err != nil {
			t.Fatal(err)
		}
		return ed25519.Sign(priv, payload), nil
	}
	n := &Node{
		ID: "F",
		ValidatorKey: ValidatorKey{
			ID:        "F",
			PublicKey: pub,
		},
	}
	payload := []byte("msc mpc sign payload")
	sig, ok := n.signValidatorPayload(payload)
	if !ok {
		t.Fatal("expected MPC signer success")
	}
	if !ed25519.Verify(pub, payload, sig) {
		t.Fatal("signature did not verify")
	}
}

func TestValidatorMPCModeDisablesSoftwareKeyFallback(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ValidatorMPCEnabled = true
	ValidatorMPCProvider = "threshold"
	ValidatorMPCKeyID = "cluster-F"
	ValidatorMPCPublicKeyHex = hex.EncodeToString(pub)
	ValidatorMPCExternalSignerCommand = ""

	key := ValidatorKey{ID: "F", PublicKey: pub, PrivateKey: priv}
	if isValidatorSigningKeyUsable(key) {
		t.Fatal("mpc mode must not fall back to local private key when signer is not ready")
	}
	n := &Node{ID: "F", ValidatorKey: key}
	if _, ok := n.signValidatorPayload([]byte("must fail closed")); ok {
		t.Fatal("expected MPC mode to fail closed without external signer")
	}
}

func TestLoadValidatorMPCKeyDoesNotRequirePrivateKey(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ValidatorMPCEnabled = true
	ValidatorMPCProvider = "threshold"
	ValidatorMPCKeyID = "cluster-F"
	ValidatorMPCPublicKeyHex = hex.EncodeToString(pub)
	ValidatorMPCExternalSignerCommand = "test-mpc-signer"
	ValidatorMPCThreshold = 2
	ValidatorMPCParticipants = 3

	dir := t.TempDir()
	key, handled := LoadValidatorHSMKey("F", dir)
	if !handled {
		t.Fatal("expected MPC key path to be handled")
	}
	if key.ID != "F" {
		t.Fatalf("key id = %q, want F", key.ID)
	}
	if !bytes.Equal(key.PublicKey, pub) {
		t.Fatal("loaded MPC public key mismatch")
	}
	if len(key.PrivateKey) != 0 {
		t.Fatal("MPC key must not load a private key into process memory")
	}
	if !isValidatorSigningKeyUsable(key) {
		t.Fatal("MPC public key should be usable through external signer")
	}
	if _, err := os.Stat(filepath.Join(dir, "validator.sec")); !os.IsNotExist(err) {
		t.Fatalf("validator.sec should not be created in MPC mode, err=%v", err)
	}
}

func TestValidatorSignerStatusEffectiveModes(t *testing.T) {
	defer restoreValidatorHSMGlobals(t)()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := ValidatorKey{ID: "S", PublicKey: pub, PrivateKey: priv}
	if st := validatorSignerStatus("S", key); !st.Ready || st.Mode != "software" {
		t.Fatalf("software signer status = %+v", st)
	}

	ValidatorHSMEnabled = true
	ValidatorHSMProvider = "ledger_enterprise"
	ValidatorHSMKeyID = "key-S"
	ValidatorHSMPublicKeyHex = hex.EncodeToString(pub)
	ValidatorHSMExternalSignerCommand = "signer"
	if st := validatorSignerStatus("S", key); !st.Ready || st.Mode != "hsm" || st.Provider != "ledger_enterprise" {
		t.Fatalf("hsm signer status = %+v", st)
	}

	ValidatorMPCEnabled = true
	ValidatorMPCProvider = "threshold_ed25519"
	ValidatorMPCKeyID = "cluster-S"
	ValidatorMPCPublicKeyHex = hex.EncodeToString(pub)
	ValidatorMPCExternalSignerCommand = "mpc-signer"
	ValidatorMPCThreshold = 2
	ValidatorMPCParticipants = 3
	if st := validatorSignerStatus("S", key); !st.Ready || st.Mode != "mpc" || st.Threshold != 2 || st.Participants != 3 {
		t.Fatalf("mpc signer status = %+v", st)
	}
}
