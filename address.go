package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	// `addressPrefix` defines the address used by this operation.
	addressPrefix = "MSC"
	// `addressVersionV1` defines the address used by this operation.
	addressVersionV1 byte = 0x01
	// `addressPayloadSizeV1` defines the address used by this operation.
	addressPayloadSizeV1 = 21 // 1-byte version + 20-byte hash
	// `addressPayloadSizeLegacy` defines the address used by this operation.
	addressPayloadSizeLegacy = 20 // legacy NewWallet() format (kept for reads)
)

// addressPayloadFromPublicKeyForChain implements the address payload from public key for chain helper.
func addressPayloadFromPublicKeyForChain(pub ed25519.PublicKey, chainID string) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("invalid public key length")
	}

	// `payload` stores the value produced by this operation.
	payload := append([]byte("MSC-ADDR|"+strings.TrimSpace(chainID)+"|"), pub...)
	// `h1` stores the value produced by this operation.
	h1 := sha256.Sum256(payload)
	// `h2` stores the value produced by this operation.
	h2 := sha256.Sum256(h1[:])

	// `out` stores the result produced by this operation.
	out := make([]byte, addressPayloadSizeV1)
	out[0] = addressVersionV1
	copy(out[1:], h2[:20])
	return out, nil
}

// addressPayloadFromPublicKey implements the address payload from public key helper.
func addressPayloadFromPublicKey(pub ed25519.PublicKey) ([]byte, error) {
	return addressPayloadFromPublicKeyForChain(pub, protocolChainID())
}

// encodeLegacyAddress implements the encode legacy address helper.
func encodeLegacyAddress(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	return hex.EncodeToString(payload)
}

// encodePrefixedAddress implements the encode prefixed address helper.
func encodePrefixedAddress(payload []byte) string {
	// `legacy` stores the value produced by this operation.
	legacy := encodeLegacyAddress(payload)
	if legacy == "" {
		return ""
	}
	return addressPrefix + legacy
}

// stripAddressPrefix implements the strip address prefix helper.
func stripAddressPrefix(addr string) string {
	addr = strings.TrimSpace(addr)
	if len(addr) >= len(addressPrefix) && strings.EqualFold(addr[:len(addressPrefix)], addressPrefix) {
		return strings.TrimSpace(addr[len(addressPrefix):])
	}
	return addr
}

// decodeAddressPayload implements the decode address payload helper.
func decodeAddressPayload(addr string) ([]byte, error) {
	// `raw` stores the value produced by this operation.
	raw := stripAddressPrefix(addr)
	if raw == "" {
		return nil, errors.New("empty address")
	}
	// `decoded` and `err` store the error produced by this operation.
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid address encoding")
	}
	switch len(decoded) {
	case addressPayloadSizeV1:
		if decoded[0] != addressVersionV1 {
			return nil, errors.New("unsupported address version")
		}
		return decoded, nil
	case addressPayloadSizeLegacy:
		// Legacy 20-byte address format (older NewWallet implementation).
		return decoded, nil
	default:
		return nil, errors.New("invalid address length")
	}
}

// canonicalAddressKey returns canonical address key.
func canonicalAddressKey(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	// `payload` and `err` store the error produced by this operation.
	if payload, err := decodeAddressPayload(addr); err == nil {
		return encodeLegacyAddress(payload)
	}
	return addr
}

// displayAddress implements the display address helper.
func displayAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	// `payload` and `err` store the error produced by this operation.
	if payload, err := decodeAddressPayload(addr); err == nil {
		return encodePrefixedAddress(payload)
	}
	return addr
}

// addressesEqual implements the addresses equal helper.
func addressesEqual(a, b string) bool {
	// `ak` stores the value produced by this operation.
	ak := canonicalAddressKey(a)
	// `bk` stores the value produced by this operation.
	bk := canonicalAddressKey(b)
	if ak == "" || bk == "" {
		return false
	}
	return strings.EqualFold(ak, bk)
}

// AddressMatchesPublicKey implements the address matches public key helper.
func AddressMatchesPublicKey(addr string, pub ed25519.PublicKey) bool {
	// `got` and `err` store the error produced by this operation.
	got, err := decodeAddressPayload(addr)
	if err != nil {
		return false
	}
	// `want` and `err` store the error produced by this operation.
	want, err := addressPayloadFromPublicKey(pub)
	if err != nil {
		return false
	}
	// Signed transactions only support the versioned format for pubkey matching.
	if len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// AddressFromPublicKeyForChain implements the address from public key for chain helper.
func AddressFromPublicKeyForChain(pub ed25519.PublicKey, chainID string) string {
	// `payload` and `err` store the error produced by this operation.
	payload, err := addressPayloadFromPublicKeyForChain(pub, chainID)
	if err != nil {
		return ""
	}
	return encodePrefixedAddress(payload)
}

// AddressFromPublicKey implements the address from public key helper.
func AddressFromPublicKey(pub ed25519.PublicKey) string {
	return AddressFromPublicKeyForChain(pub, protocolChainID())
}

// LegacyAddressFromPublicKey implements the legacy address from public key helper.
func LegacyAddressFromPublicKey(pub ed25519.PublicKey) string {
	// `payload` and `err` store the error produced by this operation.
	payload, err := addressPayloadFromPublicKey(pub)
	if err != nil {
		return ""
	}
	return encodeLegacyAddress(payload)
}
