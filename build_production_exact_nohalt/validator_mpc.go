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
	ValidatorMPCEnabled               bool
	ValidatorMPCProvider              = "threshold"
	ValidatorMPCKeyID                 string
	ValidatorMPCPublicKeyHex          string
	ValidatorMPCExternalSignerCommand string
	ValidatorMPCTimeoutMS             = 3000
	ValidatorMPCThreshold             int
	ValidatorMPCParticipants          int
)

type ValidatorMPCStatus struct {
	Enabled                  bool   `json:"enabled"`
	Ready                    bool   `json:"ready"`
	Provider                 string `json:"provider"`
	KeyID                    string `json:"key_id,omitempty"`
	PublicKeyHex             string `json:"public_key_hex,omitempty"`
	Fingerprint              string `json:"fingerprint,omitempty"`
	ExternalSignerConfigured bool   `json:"external_signer_configured"`
	Threshold                int    `json:"threshold,omitempty"`
	Participants             int    `json:"participants,omitempty"`
	TimeoutMS                int    `json:"timeout_ms"`
	Reason                   string `json:"reason,omitempty"`
}

var validatorMPCExternalSignerRunner = runValidatorMPCExternalSigner

func normalizeValidatorMPCProvider(provider string) string {
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

func validatorMPCTimeout() time.Duration {
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

func validatorMPCConfiguredPublicKey() (ed25519.PublicKey, bool) {
	raw := strings.TrimSpace(ValidatorMPCPublicKeyHex)
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

func validatorMPCReady() bool {
	if !ValidatorMPCEnabled {
		return false
	}
	if _, ok := validatorMPCConfiguredPublicKey(); !ok {
		return false
	}
	if strings.TrimSpace(ValidatorMPCExternalSignerCommand) == "" {
		return false
	}
	return ValidatorMPCThreshold <= 0 || ValidatorMPCParticipants <= 0 || ValidatorMPCThreshold <= ValidatorMPCParticipants
}

func validatorMPCSigningKeyUsable(v ValidatorKey) bool {
	if len(v.PublicKey) != ed25519.PublicKeySize || !validatorMPCReady() {
		return false
	}
	pub, ok := validatorMPCConfiguredPublicKey()
	return ok && bytes.Equal(pub, v.PublicKey)
}

func validatorMPCStatus(nodeID string, key ValidatorKey) ValidatorMPCStatus {
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

func loadValidatorMPCKey(nodeID, nodePath string) (ValidatorKey, bool) {
	id := normalizeValidatorID(nodeID)
	if !ValidatorMPCEnabled {
		return ValidatorKey{}, false
	}
	if id == "" {
		return fallbackValidatorKey(nodeID, "validator MPC requires node id"), true
	}
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
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return fallbackValidatorKey(id, "validator MPC public key fingerprint compute failed"), true
	}
	expected := strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	if expected != "" && !strings.EqualFold(fp, expected) {
		return fallbackValidatorKey(id, fmt.Sprintf("validator MPC fingerprint mismatch: expected=%s got=%s", expected, fp)), true
	}
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return fallbackValidatorKey(id, err.Error()), true
	}
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		return fallbackValidatorKey(id, err.Error()), true
	}
	if err := writeValidatorKeyMeta(id, nodePath, fp, 0); err != nil {
		return fallbackValidatorKey(id, fmt.Sprintf("validator MPC metadata write failed: %v", err)), true
	}
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

func (n *Node) signValidatorPayloadWithMPC(payload []byte) ([]byte, bool) {
	if n == nil || len(payload) == 0 || len(n.ValidatorKey.PublicKey) != ed25519.PublicKeySize {
		return nil, false
	}
	if !ValidatorMPCEnabled || !validatorMPCReady() {
		return nil, false
	}
	pub, ok := validatorMPCConfiguredPublicKey()
	if !ok || !bytes.Equal(pub, n.ValidatorKey.PublicKey) {
		return nil, false
	}
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

func runValidatorMPCExternalSigner(req validatorHSMRequest) ([]byte, error) {
	command := strings.TrimSpace(ValidatorMPCExternalSignerCommand)
	if command == "" {
		return nil, errors.New("MPC external signer command is empty")
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), validatorMPCTimeout())
	defer cancel()
	cmd := validatorHSMShellCommand(ctx, command)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("MPC external signer timed out after %s", validatorMPCTimeout())
		}
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
