package main

import (
	"crypto/ed25519"
	"encoding/hex"
)

type ValidatorSignerStatus struct {
	Mode                     string `json:"mode"`
	Ready                    bool   `json:"ready"`
	Reason                   string `json:"reason,omitempty"`
	Provider                 string `json:"provider,omitempty"`
	KeyID                    string `json:"key_id,omitempty"`
	PublicKeyHex             string `json:"public_key_hex,omitempty"`
	Fingerprint              string `json:"fingerprint,omitempty"`
	ExternalSignerConfigured bool   `json:"external_signer_configured"`
	Threshold                int    `json:"threshold,omitempty"`
	Participants             int    `json:"participants,omitempty"`
	TimeoutMS                int    `json:"timeout_ms,omitempty"`
}

func validatorSignerStatus(nodeID string, key ValidatorKey) ValidatorSignerStatus {
	if ValidatorMPCEnabled {
		st := validatorMPCStatus(nodeID, key)
		return ValidatorSignerStatus{
			Mode:                     "mpc",
			Ready:                    st.Ready,
			Reason:                   signerStatusReason(st.Ready, st.Reason),
			Provider:                 st.Provider,
			KeyID:                    st.KeyID,
			PublicKeyHex:             st.PublicKeyHex,
			Fingerprint:              st.Fingerprint,
			ExternalSignerConfigured: st.ExternalSignerConfigured,
			Threshold:                st.Threshold,
			Participants:             st.Participants,
			TimeoutMS:                st.TimeoutMS,
		}
	}
	if ValidatorHSMEnabled {
		st := validatorHSMStatus(nodeID, key)
		return ValidatorSignerStatus{
			Mode:                     "hsm",
			Ready:                    st.Ready,
			Reason:                   signerStatusReason(st.Ready, st.Reason),
			Provider:                 st.Provider,
			KeyID:                    st.KeyID,
			PublicKeyHex:             st.PublicKeyHex,
			Fingerprint:              st.Fingerprint,
			ExternalSignerConfigured: st.ExternalSignerConfigured,
			TimeoutMS:                st.TimeoutMS,
		}
	}
	status := ValidatorSignerStatus{
		Mode:     "none",
		Ready:    false,
		Reason:   "no_validator_key_loaded",
		Provider: "local",
	}
	if len(key.PublicKey) == ed25519.PublicKeySize {
		status.PublicKeyHex = hex.EncodeToString(key.PublicKey)
		status.Fingerprint = validatorKeyFingerprint(key.PublicKey)
		status.Mode = "public_key_only"
		status.Reason = "software_private_key_missing"
	}
	if len(key.PublicKey) == ed25519.PublicKeySize && len(key.PrivateKey) == ed25519.PrivateKeySize {
		status.Mode = "software"
		status.Ready = true
		status.Reason = "software_private_key_loaded"
	}
	return status
}

func signerStatusReason(ready bool, reason string) string {
	if reason != "" {
		return reason
	}
	if ready {
		return "ready"
	}
	return "not_ready"
}

func validatorSignerModeCode(mode string) float64 {
	switch mode {
	case "software":
		return 0
	case "hsm":
		return 1
	case "mpc":
		return 2
	case "public_key_only":
		return 3
	default:
		return 4
	}
}
