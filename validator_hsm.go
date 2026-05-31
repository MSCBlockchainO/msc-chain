package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"
)

var (
	ValidatorHSMEnabled               bool
	ValidatorHSMProvider              = "external"
	ValidatorHSMKeyID                 string
	ValidatorHSMPublicKeyHex          string
	ValidatorHSMExternalSignerCommand string
	ValidatorHSMTimeoutMS             = 3000
	ValidatorHSMRequireUserPresence   bool
)

type ValidatorHSMStatus struct {
	Enabled                  bool   `json:"enabled"`
	Ready                    bool   `json:"ready"`
	Provider                 string `json:"provider"`
	KeyID                    string `json:"key_id,omitempty"`
	PublicKeyHex             string `json:"public_key_hex,omitempty"`
	Fingerprint              string `json:"fingerprint,omitempty"`
	ExternalSignerConfigured bool   `json:"external_signer_configured"`
	RequireUserPresence      bool   `json:"require_user_presence"`
	TimeoutMS                int    `json:"timeout_ms"`
	Reason                   string `json:"reason,omitempty"`
}

type validatorHSMRequest struct {
	Domain       string `json:"domain"`
	ValidatorID  string `json:"validator_id"`
	Provider     string `json:"provider"`
	KeyID        string `json:"key_id,omitempty"`
	PublicKeyHex string `json:"public_key_hex"`
	PayloadHex   string `json:"payload_hex"`
}

type validatorHSMResponse struct {
	Signature    string `json:"signature"`
	SignatureHex string `json:"signature_hex"`
	SigHex       string `json:"sig_hex"`
}

var validatorHSMExternalSignerRunner = runValidatorHSMExternalSigner

func normalizeValidatorHSMProvider(provider string) string {
	p := strings.TrimSpace(strings.ToLower(provider))
	p = strings.ReplaceAll(p, " ", "_")
	p = strings.ReplaceAll(p, "-", "_")
	switch p {
	case "", "external", "command", "signer":
		return "external"
	case "hsm", "pkcs11", "yubihsm", "yubi_hsm", "ledger", "ledger_enterprise":
		return p
	default:
		return p
	}
}

func validatorHSMTimeout() time.Duration {
	ms := ValidatorHSMTimeoutMS
	if ms <= 0 {
		ms = 3000
	}
	if ms < 500 {
		ms = 500
	}
	return time.Duration(ms) * time.Millisecond
}

func validatorHSMConfiguredPublicKey() (ed25519.PublicKey, bool) {
	raw := strings.TrimSpace(ValidatorHSMPublicKeyHex)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "0x"), "0X")
	if raw == "" {
		return nil, false
	}
	pub, err := hex.DecodeString(raw)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(append([]byte(nil), pub...)), true
}

func validatorHSMFingerprint() string {
	pub, ok := validatorHSMConfiguredPublicKey()
	if !ok {
		return ""
	}
	return validatorKeyFingerprint(pub)
}

func validatorHSMReady() bool {
	if !ValidatorHSMEnabled {
		return false
	}
	if _, ok := validatorHSMConfiguredPublicKey(); !ok {
		return false
	}
	return strings.TrimSpace(ValidatorHSMExternalSignerCommand) != ""
}

func validatorHSMStatus(nodeID string, key ValidatorKey) ValidatorHSMStatus {
	status := ValidatorHSMStatus{
		Enabled:                  ValidatorHSMEnabled,
		Provider:                 normalizeValidatorHSMProvider(ValidatorHSMProvider),
		KeyID:                    strings.TrimSpace(ValidatorHSMKeyID),
		ExternalSignerConfigured: strings.TrimSpace(ValidatorHSMExternalSignerCommand) != "",
		RequireUserPresence:      ValidatorHSMRequireUserPresence,
		TimeoutMS:                int(validatorHSMTimeout() / time.Millisecond),
	}
	if !ValidatorHSMEnabled {
		status.Reason = "disabled"
		return status
	}
	pub, ok := validatorHSMConfiguredPublicKey()
	if !ok {
		status.Reason = "invalid_or_missing_public_key"
		return status
	}
	status.PublicKeyHex = hex.EncodeToString(pub)
	status.Fingerprint = validatorKeyFingerprint(pub)
	if strings.TrimSpace(ValidatorHSMExternalSignerCommand) == "" {
		status.Reason = "missing_external_signer_command"
		return status
	}
	if len(key.PublicKey) == ed25519.PublicKeySize && !bytes.Equal(key.PublicKey, pub) {
		status.Reason = "loaded_key_mismatch"
		return status
	}
	status.Ready = true
	return status
}

func isValidatorSigningKeyUsable(v ValidatorKey) bool {
	if len(v.PublicKey) != ed25519.PublicKeySize {
		return false
	}
	if len(v.PrivateKey) == ed25519.PrivateKeySize {
		return true
	}
	if !ValidatorHSMEnabled || !validatorHSMReady() {
		return false
	}
	pub, ok := validatorHSMConfiguredPublicKey()
	return ok && bytes.Equal(pub, v.PublicKey)
}

func LoadValidatorHSMKey(nodeID, nodePath string) (ValidatorKey, bool) {
	id := normalizeValidatorID(nodeID)
	if !ValidatorHSMEnabled {
		return ValidatorKey{}, false
	}
	if id == "" {
		return fallbackValidatorKey(nodeID, "validator HSM requires node id"), true
	}
	pub, ok := validatorHSMConfiguredPublicKey()
	if !ok {
		return fallbackValidatorKey(id, "validator HSM enabled but validators.hsm_public_key is invalid or missing"), true
	}
	if strings.TrimSpace(ValidatorHSMExternalSignerCommand) == "" {
		return fallbackValidatorKey(id, "validator HSM enabled but validators.hsm_external_signer_command is empty"), true
	}
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return fallbackValidatorKey(id, "validator HSM public key fingerprint compute failed"), true
	}
	expected := strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	if expected != "" && !strings.EqualFold(fp, expected) {
		return fallbackValidatorKey(id, fmt.Sprintf("validator HSM fingerprint mismatch: expected=%s got=%s", expected, fp)), true
	}
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return fallbackValidatorKey(id, err.Error()), true
	}
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		return fallbackValidatorKey(id, err.Error()), true
	}
	if err := writeValidatorKeyMeta(id, nodePath, fp, 0); err != nil {
		return fallbackValidatorKey(id, fmt.Sprintf("validator HSM metadata write failed: %v", err)), true
	}
	if err := writeValidatorPublicKeyFile(nodePath, pub); err != nil {
		return fallbackValidatorKey(id, fmt.Sprintf("validator HSM public key write failed: %v", err)), true
	}
	recordValidatorKeyLoadMeta(id, validatorKeyLoadMeta{
		Source:      "hsm:" + normalizeValidatorHSMProvider(ValidatorHSMProvider),
		IntegrityOK: true,
		ErrorReason: "",
	})
	logValidatorHSMLoaded(id, fp)
	return ValidatorKey{ID: id, PublicKey: pub}, true
}

func logValidatorHSMLoaded(nodeID, fingerprint string) {
	fmt.Printf("[VALIDATOR-HSM] validator=%s provider=%s key_id=%s fingerprint=%s signer=external\n",
		normalizeValidatorID(nodeID),
		normalizeValidatorHSMProvider(ValidatorHSMProvider),
		strings.TrimSpace(ValidatorHSMKeyID),
		strings.TrimSpace(fingerprint),
	)
}

func (n *Node) signValidatorPayload(payload []byte) ([]byte, bool) {
	if n == nil || len(payload) == 0 || len(n.ValidatorKey.PublicKey) != ed25519.PublicKeySize {
		return nil, false
	}
	if len(n.ValidatorKey.PrivateKey) == ed25519.PrivateKeySize {
		return ed25519.Sign(n.ValidatorKey.PrivateKey, payload), true
	}
	if !ValidatorHSMEnabled || !validatorHSMReady() {
		return nil, false
	}
	pub, ok := validatorHSMConfiguredPublicKey()
	if !ok || !bytes.Equal(pub, n.ValidatorKey.PublicKey) {
		return nil, false
	}
	sig, err := validatorHSMExternalSignerRunner(validatorHSMRequest{
		Domain:       "msc-validator-ed25519-v1",
		ValidatorID:  normalizeValidatorID(n.ValidatorKey.ID),
		Provider:     normalizeValidatorHSMProvider(ValidatorHSMProvider),
		KeyID:        strings.TrimSpace(ValidatorHSMKeyID),
		PublicKeyHex: hex.EncodeToString(pub),
		PayloadHex:   hex.EncodeToString(payload),
	})
	if err != nil {
		fmt.Printf("[VALIDATOR-HSM] signer_failed validator=%s provider=%s error=%v\n",
			normalizeValidatorID(n.ValidatorKey.ID),
			normalizeValidatorHSMProvider(ValidatorHSMProvider),
			err,
		)
		return nil, false
	}
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, payload, sig) {
		fmt.Printf("[VALIDATOR-HSM] signer_rejected validator=%s provider=%s reason=invalid_signature\n",
			normalizeValidatorID(n.ValidatorKey.ID),
			normalizeValidatorHSMProvider(ValidatorHSMProvider),
		)
		return nil, false
	}
	return sig, true
}

func runValidatorHSMExternalSigner(req validatorHSMRequest) ([]byte, error) {
	command := strings.TrimSpace(ValidatorHSMExternalSignerCommand)
	if command == "" {
		return nil, errors.New("external signer command is empty")
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), validatorHSMTimeout())
	defer cancel()
	cmd := validatorHSMShellCommand(ctx, command)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("external signer timed out after %s", validatorHSMTimeout())
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 240 {
			msg = msg[:240]
		}
		if msg != "" {
			return nil, fmt.Errorf("external signer failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("external signer failed: %w", err)
	}
	return validatorHSMDecodeSignature(stdout.Bytes())
}

func validatorHSMShellCommand(ctx context.Context, command string) *exec.Cmd {
	if goruntime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func validatorHSMDecodeSignature(raw []byte) ([]byte, error) {
	out := strings.TrimSpace(string(raw))
	if out == "" {
		return nil, errors.New("external signer returned empty output")
	}
	if strings.HasPrefix(out, "{") {
		var resp validatorHSMResponse
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return nil, err
		}
		switch {
		case strings.TrimSpace(resp.SignatureHex) != "":
			out = resp.SignatureHex
		case strings.TrimSpace(resp.Signature) != "":
			out = resp.Signature
		case strings.TrimSpace(resp.SigHex) != "":
			out = resp.SigHex
		default:
			return nil, errors.New("external signer JSON missing signature")
		}
	}
	out = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(out), "0x"), "0X")
	sig, err := hex.DecodeString(out)
	if err != nil {
		return nil, err
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("external signer returned %d-byte signature, want %d", len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}
