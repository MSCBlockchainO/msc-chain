package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// `ValidatorMPCEnabled` stores whether the related condition is satisfied.
	ValidatorMPCEnabled bool
	// `ValidatorMPCProvider` stores whether the related condition is satisfied.
	ValidatorMPCProvider = "threshold"
	// `ValidatorMPCKeyID` stores whether the related condition is satisfied.
	ValidatorMPCKeyID string
	// `ValidatorMPCPublicKeyHex` stores whether the related condition is satisfied.
	ValidatorMPCPublicKeyHex string
	// `ValidatorMPCExternalSignerCommand` stores whether the related condition is satisfied.
	ValidatorMPCExternalSignerCommand string
	// `ValidatorMPCTimeoutMS` stores whether the related condition is satisfied.
	ValidatorMPCTimeoutMS = 3000
	// `ValidatorMPCThreshold` stores whether the related condition is satisfied.
	ValidatorMPCThreshold int
	// `ValidatorMPCParticipants` stores whether the related condition is satisfied.
	ValidatorMPCParticipants int
)

type ValidatorMPCStatus struct {
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
	// `Threshold` stores the value associated with this record.
	Threshold int `json:"threshold,omitempty"`
	// `Participants` stores the value associated with this record.
	Participants int `json:"participants,omitempty"`
	// `TimeoutMS` stores the value associated with this record.
	TimeoutMS int `json:"timeout_ms"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
}

// `validatorMPCExternalSignerRunner` stores whether the related condition is satisfied.
var validatorMPCExternalSignerRunner = runValidatorMPCExternalSigner

// normalizeValidatorMPCProvider normalizes validator mpc provider.
func normalizeValidatorMPCProvider(provider string) string {
	// `p` stores the value produced by this operation.
	p := strings.TrimSpace(strings.ToLower(provider))
	p = strings.ReplaceAll(p, " ", "_")
	p = strings.ReplaceAll(p, "-", "_")
	switch p {
	case "", "mpc", "threshold", "threshold_signer", "external":
		return "threshold"
	case "tss", "threshold_ecdsa", "threshold_ed25519", "fireblocks", "cubist", "dfns", "turnkey", "ledger_enterprise":
		return p
	default:
		return p
	}
}

// validatorMPCTimeout implements the validator mpc timeout helper.
func validatorMPCTimeout() time.Duration {
	// `ms` stores the value produced by this operation.
	ms := ValidatorMPCTimeoutMS
	if ms <= 0 {
		ms = ValidatorHSMTimeoutMS
	}
	if ms <= 0 {
		ms = 3000
	}
	if ms < 500 {
		ms = 500
	}
	return time.Duration(ms) * time.Millisecond
}

// validatorMPCConfiguredPublicKey implements the validator mpc configured public key helper.
func validatorMPCConfiguredPublicKey() (ed25519.PublicKey, bool) {
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(ValidatorMPCPublicKeyHex)
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

// validatorMPCReady implements the validator mpc ready helper.
func validatorMPCReady() bool {
	if !ValidatorMPCEnabled {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := validatorMPCConfiguredPublicKey(); !ok {
		return false
	}
	if strings.TrimSpace(ValidatorMPCExternalSignerCommand) == "" {
		return false
	}
	return ValidatorMPCThreshold <= 0 || ValidatorMPCParticipants <= 0 || ValidatorMPCThreshold <= ValidatorMPCParticipants
}

// validatorMPCSigningKeyUsable implements the validator mpc signing key usable helper.
func validatorMPCSigningKeyUsable(v ValidatorKey) bool {
	if len(v.PublicKey) != ed25519.PublicKeySize || !validatorMPCReady() {
		return false
	}
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := validatorMPCConfiguredPublicKey()
	return ok && bytes.Equal(pub, v.PublicKey)
}

// validatorMPCStatus implements the validator mpc status helper.
func validatorMPCStatus(_ string, key ValidatorKey) ValidatorMPCStatus {
	// `status` stores the value produced by this operation.
	status := ValidatorMPCStatus{
		Enabled:                  ValidatorMPCEnabled,
		Provider:                 normalizeValidatorMPCProvider(ValidatorMPCProvider),
		KeyID:                    strings.TrimSpace(ValidatorMPCKeyID),
		ExternalSignerConfigured: strings.TrimSpace(ValidatorMPCExternalSignerCommand) != "",
		Threshold:                ValidatorMPCThreshold,
		Participants:             ValidatorMPCParticipants,
		TimeoutMS:                int(validatorMPCTimeout() / time.Millisecond),
	}
	if !ValidatorMPCEnabled {
		status.Reason = "disabled"
		return status
	}
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := validatorMPCConfiguredPublicKey()
	if !ok {
		status.Reason = "invalid_or_missing_public_key"
		return status
	}
	status.PublicKeyHex = hex.EncodeToString(pub)
	status.Fingerprint = validatorKeyFingerprint(pub)
	if strings.TrimSpace(ValidatorMPCExternalSignerCommand) == "" {
		status.Reason = "missing_external_signer_command"
		return status
	}
	if ValidatorMPCThreshold > 0 && ValidatorMPCParticipants > 0 && ValidatorMPCThreshold > ValidatorMPCParticipants {
		status.Reason = "threshold_exceeds_participants"
		return status
	}
	if len(key.PublicKey) == ed25519.PublicKeySize && !bytes.Equal(key.PublicKey, pub) {
		status.Reason = "loaded_key_mismatch"
		return status
	}
	status.Ready = true
	return status
}

// loadValidatorMPCKey implements the load validator mpc key helper.
func loadValidatorMPCKey(nodeID, nodePath string) (ValidatorKey, bool) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if !ValidatorMPCEnabled {
		return ValidatorKey{}, false
	}
	if id == "" {
		return fallbackValidatorKey(nodeID, "validator MPC requires node id"), true
	}
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := validatorMPCConfiguredPublicKey()
	if !ok {
		return fallbackValidatorKey(id, "validator MPC enabled but validators.mpc_public_key is invalid or missing"), true
	}
	if strings.TrimSpace(ValidatorMPCExternalSignerCommand) == "" {
		return fallbackValidatorKey(id, "validator MPC enabled but validators.mpc_external_signer_command is empty"), true
	}
	if ValidatorMPCThreshold > 0 && ValidatorMPCParticipants > 0 && ValidatorMPCThreshold > ValidatorMPCParticipants {
		return fallbackValidatorKey(id, "validator MPC threshold exceeds participant count"), true
	}
	// `fp` stores the value produced by this operation.
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return fallbackValidatorKey(id, "validator MPC public key fingerprint compute failed"), true
	}
	// `expected` stores the value produced by this operation.
	expected := strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	if expected != "" && !strings.EqualFold(fp, expected) {
		return fallbackValidatorKey(id, fmt.Sprintf("validator MPC fingerprint mismatch: expected=%s got=%s", expected, fp)), true
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
		return fallbackValidatorKey(id, fmt.Sprintf("validator MPC metadata write failed: %v", err)), true
	}
	// `err` stores the error produced by this operation.
	if err := writeValidatorPublicKeyFile(nodePath, pub); err != nil {
		return fallbackValidatorKey(id, fmt.Sprintf("validator MPC public key write failed: %v", err)), true
	}
	recordValidatorKeyLoadMeta(id, validatorKeyLoadMeta{
		Source:      "mpc:" + normalizeValidatorMPCProvider(ValidatorMPCProvider),
		IntegrityOK: true,
		ErrorReason: "",
	})
	logValidatorMPCLoaded(id, fp)
	return ValidatorKey{ID: id, PublicKey: pub}, true
}

// logValidatorMPCLoaded implements the log validator mpc loaded helper.
func logValidatorMPCLoaded(nodeID, fingerprint string) {
	fmt.Printf("[VALIDATOR-MPC] validator=%s provider=%s key_id=%s threshold=%d participants=%d fingerprint=%s signer=external\n",
		normalizeValidatorID(nodeID),
		normalizeValidatorMPCProvider(ValidatorMPCProvider),
		strings.TrimSpace(ValidatorMPCKeyID),
		ValidatorMPCThreshold,
		ValidatorMPCParticipants,
		strings.TrimSpace(fingerprint),
	)
}

// signValidatorPayloadWithMPC implements the sign validator payload with mpc helper.
func (n *Node) signValidatorPayloadWithMPC(payload []byte) ([]byte, bool) {
	if n == nil || len(payload) == 0 || len(n.ValidatorKey.PublicKey) != ed25519.PublicKeySize {
		return nil, false
	}
	if !ValidatorMPCEnabled || !validatorMPCReady() {
		return nil, false
	}
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := validatorMPCConfiguredPublicKey()
	if !ok || !bytes.Equal(pub, n.ValidatorKey.PublicKey) {
		return nil, false
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := validatorMPCExternalSignerRunner(validatorHSMRequest{
		Domain:       "msc-validator-mpc-ed25519-v1",
		SignerMode:   "mpc",
		ValidatorID:  normalizeValidatorID(n.ValidatorKey.ID),
		Provider:     normalizeValidatorMPCProvider(ValidatorMPCProvider),
		KeyID:        strings.TrimSpace(ValidatorMPCKeyID),
		PublicKeyHex: hex.EncodeToString(pub),
		PayloadHex:   hex.EncodeToString(payload),
		Threshold:    ValidatorMPCThreshold,
		Participants: ValidatorMPCParticipants,
	})
	if err != nil {
		fmt.Printf("[VALIDATOR-MPC] signer_failed validator=%s provider=%s error=%v\n",
			normalizeValidatorID(n.ValidatorKey.ID),
			normalizeValidatorMPCProvider(ValidatorMPCProvider),
			err,
		)
		return nil, false
	}
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, payload, sig) {
		fmt.Printf("[VALIDATOR-MPC] signer_rejected validator=%s provider=%s reason=invalid_signature\n",
			normalizeValidatorID(n.ValidatorKey.ID),
			normalizeValidatorMPCProvider(ValidatorMPCProvider),
		)
		return nil, false
	}
	return sig, true
}

// runValidatorMPCExternalSigner implements the run validator mpc external signer helper.
func runValidatorMPCExternalSigner(req validatorHSMRequest) ([]byte, error) {
	// `command` stores the value produced by this operation.
	command := strings.TrimSpace(ValidatorMPCExternalSignerCommand)
	if command == "" {
		return nil, errors.New("MPC external signer command is empty")
	}
	// `payload` and `err` store the error produced by this operation.
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), validatorMPCTimeout())
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
			return nil, fmt.Errorf("MPC external signer timed out after %s", validatorMPCTimeout())
		}
		// `msg` stores the value produced by this operation.
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 240 {
			msg = msg[:240]
		}
		if msg != "" {
			return nil, fmt.Errorf("MPC external signer failed: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("MPC external signer failed: %w", err)
	}
	return validatorHSMDecodeSignature(stdout.Bytes())
}
