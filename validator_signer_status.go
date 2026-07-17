package main

import (
	"crypto/ed25519"
	"encoding/hex"
)

type ValidatorSignerStatus struct {
	// `Mode` stores the value associated with this record.
	Mode                     string `json:"mode"`
	// `Ready` stores the value associated with this record.
	Ready                    bool   `json:"ready"`
	// `Reason` stores the value associated with this record.
	Reason                   string `json:"reason,omitempty"`
	// `Provider` stores the value associated with this record.
	Provider                 string `json:"provider,omitempty"`
	// `KeyID` stores the key used to access the related value.
	KeyID                    string `json:"key_id,omitempty"`
	// `PublicKeyHex` stores the value associated with this record.
	PublicKeyHex             string `json:"public_key_hex,omitempty"`
	// `Fingerprint` stores the value associated with this record.
	Fingerprint              string `json:"fingerprint,omitempty"`
	// `ExternalSignerConfigured` stores the value associated with this record.
	ExternalSignerConfigured bool   `json:"external_signer_configured"`
	// `Threshold` stores the value associated with this record.
	Threshold                int    `json:"threshold,omitempty"`
	// `Participants` stores the value associated with this record.
	Participants             int    `json:"participants,omitempty"`
	// `TimeoutMS` stores the value associated with this record.
	TimeoutMS                int    `json:"timeout_ms,omitempty"`
}

// validatorSignerStatus implements the validator signer status helper.
func validatorSignerStatus(nodeID string, key ValidatorKey) ValidatorSignerStatus {
	if ValidatorMPCEnabled {
		// `st` stores the value produced by this operation.
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
		// `st` stores the value produced by this operation.
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
	// `status` stores the value produced by this operation.
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

// signerStatusReason implements the signer status reason helper.
func signerStatusReason(ready bool, reason string) string {
	if reason != "" {
		return reason
	}
	if ready {
		return "ready"
	}
	return "not_ready"
}

// validatorSignerModeCode implements the validator signer mode code helper.
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
