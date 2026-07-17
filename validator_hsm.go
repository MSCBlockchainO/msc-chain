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
	// `ValidatorHSMEnabled` stores whether the related condition is satisfied.
	ValidatorHSMEnabled bool
	// `ValidatorHSMProvider` stores whether the related condition is satisfied.
	ValidatorHSMProvider = "external"
	// `ValidatorHSMKeyID` stores whether the related condition is satisfied.
	ValidatorHSMKeyID string
	// `ValidatorHSMPublicKeyHex` stores whether the related condition is satisfied.
	ValidatorHSMPublicKeyHex string
	// `ValidatorHSMExternalSignerCommand` stores whether the related condition is satisfied.
	ValidatorHSMExternalSignerCommand string
	// `ValidatorHSMTimeoutMS` stores whether the related condition is satisfied.
	ValidatorHSMTimeoutMS = 3000
	// `ValidatorHSMRequireUserPresence` stores whether the related condition is satisfied.
	ValidatorHSMRequireUserPresence bool
)

type ValidatorHSMStatus struct {
	// `Enabled` stores whether the related condition is satisfied.
	Enabled bool `json:"enabled"`
	// `Ready` stores the value associated with this record.
	Ready bool `json:"ready"`
	// `Provider` stores the value associated with this record.
	Provider string `json:"provider"`
	// `KeyID` stores the key used to access the related value.
	KeyID string `json:"key_id,omitempty"`
	// `PublicKeyHex` stores the value associated with this record.
	PublicKeyHex string `json:"public_key_hex,omitempty"`
	// `Fingerprint` stores the value associated with this record.
	Fingerprint string `json:"fingerprint,omitempty"`
	// `ExternalSignerConfigured` stores the value associated with this record.
	ExternalSignerConfigured bool `json:"external_signer_configured"`
	// `RequireUserPresence` stores the request data being processed.
	RequireUserPresence bool `json:"require_user_presence"`
	// `TimeoutMS` stores the value associated with this record.
	TimeoutMS int `json:"timeout_ms"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
}

type validatorHSMRequest struct {
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `SignerMode` stores the value associated with this record.
	SignerMode string `json:"signer_mode,omitempty"`
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string `json:"validator_id"`
	// `Provider` stores the value associated with this record.
	Provider string `json:"provider"`
	// `KeyID` stores the key used to access the related value.
	KeyID string `json:"key_id,omitempty"`
	// `PublicKeyHex` stores the value associated with this record.
	PublicKeyHex string `json:"public_key_hex"`
	// `PayloadHex` stores the value associated with this record.
	PayloadHex string `json:"payload_hex"`
	// `Threshold` stores the value associated with this record.
	Threshold int `json:"threshold,omitempty"`
	// `Participants` stores the value associated with this record.
	Participants int `json:"participants,omitempty"`
}

type validatorHSMResponse struct {
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature"`
	// `SignatureHex` stores the value associated with this record.
	SignatureHex string `json:"signature_hex"`
	// `SigHex` stores the value associated with this record.
	SigHex string `json:"sig_hex"`
}

// `validatorHSMExternalSignerRunner` stores whether the related condition is satisfied.
var validatorHSMExternalSignerRunner = runValidatorHSMExternalSigner

// normalizeValidatorHSMProvider normalizes validator hsm provider.
func normalizeValidatorHSMProvider(provider string) string {
	// `p` stores the value produced by this operation.
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

// validatorHSMTimeout implements the validator hsm timeout helper.
func validatorHSMTimeout() time.Duration {
	// `ms` stores the value produced by this operation.
	ms := ValidatorHSMTimeoutMS
	if ms <= 0 {
		ms = 3000
	}
	if ms < 500 {
		ms = 500
	}
	return time.Duration(ms) * time.Millisecond
}

// validatorHSMConfiguredPublicKey implements the validator hsm configured public key helper.
func validatorHSMConfiguredPublicKey() (ed25519.PublicKey, bool) {
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(ValidatorHSMPublicKeyHex)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "0x"), "0X")
	if raw == "" {
		return nil, false
	}
	// `pub` and `err` store the error produced by this operation.
	pub, err := hex.DecodeString(raw)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(append([]byte(nil), pub...)), true
}

// validatorHSMFingerprint implements the validator hsm fingerprint helper.
func validatorHSMFingerprint() string {
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := validatorHSMConfiguredPublicKey()
	if !ok {
		return ""
	}
	return validatorKeyFingerprint(pub)
}

// validatorHSMReady implements the validator hsm ready helper.
func validatorHSMReady() bool {
	if ValidatorMPCEnabled {
		return validatorMPCReady()
	}
	if !ValidatorHSMEnabled {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := validatorHSMConfiguredPublicKey(); !ok {
		return false
	}
	return strings.TrimSpace(ValidatorHSMExternalSignerCommand) != ""
}

// validatorHSMStatus implements the validator hsm status helper.
func validatorHSMStatus(_ string, key ValidatorKey) ValidatorHSMStatus {
	// `status` stores the value produced by this operation.
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
	// `pub` and `ok` store whether the related condition is satisfied.
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

// isValidatorSigningKeyUsable implements the is validator signing key usable helper.
func isValidatorSigningKeyUsable(v ValidatorKey) bool {
	if len(v.PublicKey) != ed25519.PublicKeySize {
		return false
	}
	if ValidatorMPCEnabled {
		return validatorMPCSigningKeyUsable(v)
	}
	if ValidatorHSMEnabled {
		if !validatorHSMReady() {
			return false
		}
		// `pub` and `ok` store whether the related condition is satisfied.
		pub, ok := validatorHSMConfiguredPublicKey()
		return ok && bytes.Equal(pub, v.PublicKey)
	}
	return len(v.PrivateKey) == ed25519.PrivateKeySize
}

// LoadValidatorHSMKey loads validator hsm key.
func LoadValidatorHSMKey(nodeID, nodePath string) (ValidatorKey, bool) {
	if ValidatorMPCEnabled {
		return loadValidatorMPCKey(nodeID, nodePath)
	}
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if !ValidatorHSMEnabled {
		return ValidatorKey{}, false
	}
	if id == "" {
		return fallbackValidatorKey(nodeID, "validator HSM requires node id"), true
	}
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := validatorHSMConfiguredPublicKey()
	if !ok {
		return fallbackValidatorKey(id, "validator HSM enabled but validators.hsm_public_key is invalid or missing"), true
	}
	if strings.TrimSpace(ValidatorHSMExternalSignerCommand) == "" {
		return fallbackValidatorKey(id, "validator HSM enabled but validators.hsm_external_signer_command is empty"), true
	}
	// `fp` stores the value produced by this operation.
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return fallbackValidatorKey(id, "validator HSM public key fingerprint compute failed"), true
	}
	// `expected` stores the value produced by this operation.
	expected := strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	if expected != "" && !strings.EqualFold(fp, expected) {
		return fallbackValidatorKey(id, fmt.Sprintf("validator HSM fingerprint mismatch: expected=%s got=%s", expected, fp)), true
	}
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return fallbackValidatorKey(id, err.Error()), true
	}
	// `err` stores the error produced by this operation.
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		return fallbackValidatorKey(id, err.Error()), true
	}
	// `err` stores the error produced by this operation.
	if err := writeValidatorKeyMeta(id, nodePath, fp, 0); err != nil {
		return fallbackValidatorKey(id, fmt.Sprintf("validator HSM metadata write failed: %v", err)), true
	}
	// `err` stores the error produced by this operation.
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

// logValidatorHSMLoaded implements the log validator hsm loaded helper.
func logValidatorHSMLoaded(nodeID, fingerprint string) {
	fmt.Printf("[VALIDATOR-HSM] validator=%s provider=%s key_id=%s fingerprint=%s signer=external\n",
		normalizeValidatorID(nodeID),
		normalizeValidatorHSMProvider(ValidatorHSMProvider),
		strings.TrimSpace(ValidatorHSMKeyID),
		strings.TrimSpace(fingerprint),
	)
}

// signValidatorPayload implements the sign validator payload helper.
func (n *Node) signValidatorPayload(payload []byte) ([]byte, bool) {
	if n == nil || len(payload) == 0 || len(n.ValidatorKey.PublicKey) != ed25519.PublicKeySize {
		return nil, false
	}
	if ValidatorMPCEnabled {
		return n.signValidatorPayloadWithMPC(payload)
	}
	if ValidatorHSMEnabled {
		if !validatorHSMReady() {
			return nil, false
		}
		// `pub` and `ok` store whether the related condition is satisfied.
		pub, ok := validatorHSMConfiguredPublicKey()
		if !ok || !bytes.Equal(pub, n.ValidatorKey.PublicKey) {
			return nil, false
		}
		// `sig` and `err` store the error produced by this operation.
		sig, err := validatorHSMExternalSignerRunner(validatorHSMRequest{
			Domain:       "msc-validator-ed25519-v1",
			SignerMode:   "hsm",
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
	if len(n.ValidatorKey.PrivateKey) == ed25519.PrivateKeySize {
		return ed25519.Sign(n.ValidatorKey.PrivateKey, payload), true
	}
	return nil, false
}

// runValidatorHSMExternalSigner implements the run validator hsm external signer helper.
func runValidatorHSMExternalSigner(req validatorHSMRequest) ([]byte, error) {
	// `command` stores the value produced by this operation.
	command := strings.TrimSpace(ValidatorHSMExternalSignerCommand)
	if command == "" {
		return nil, errors.New("external signer command is empty")
	}
	// `payload` and `err` store the error produced by this operation.
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), validatorHSMTimeout())
	defer cancel()
	// `cmd` stores the value produced by this operation.
	cmd := validatorHSMShellCommand(ctx, command)
	cmd.Stdin = bytes.NewReader(payload)
	// `stdout` stores the result produced by this operation.
	var stdout bytes.Buffer
	// `stderr` stores the error produced by this operation.
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// `err` stores the error produced by this operation.
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("external signer timed out after %s", validatorHSMTimeout())
		}
		// `msg` stores the value produced by this operation.
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

// validatorHSMShellCommand implements the validator hsm shell command helper.
func validatorHSMShellCommand(ctx context.Context, command string) *exec.Cmd {
	if goruntime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

// validatorHSMDecodeSignature implements the validator hsm decode signature helper.
func validatorHSMDecodeSignature(raw []byte) ([]byte, error) {
	// `out` stores the result produced by this operation.
	out := strings.TrimSpace(string(raw))
	if out == "" {
		return nil, errors.New("external signer returned empty output")
	}
	if strings.HasPrefix(out, "{") {
		// `resp` stores the response produced by this operation.
		var resp validatorHSMResponse
		// `err` stores the error produced by this operation.
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
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(out)
	if err != nil {
		return nil, err
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("external signer returned %d-byte signature, want %d", len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}
