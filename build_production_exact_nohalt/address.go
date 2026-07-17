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
	addressPrefix                 = "MSC"
	addressVersionV1         byte = 0x01
	addressPayloadSizeV1          = 21 // 1-byte version + 20-byte hash
	addressPayloadSizeLegacy      = 20 // legacy NewWallet() format (kept for reads)
)

func addressPayloadFromPublicKeyForChain(pub ed25519.PublicKey, chainID string) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("invalid public key length")
	}

	payload := append([]byte("MSC-ADDR|"+strings.TrimSpace(chainID)+"|"), pub...)
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])

	out := make([]byte, addressPayloadSizeV1)
	out[0] = addressVersionV1
	copy(out[1:], h2[:20])
	return out, nil
}

func addressPayloadFromPublicKey(pub ed25519.PublicKey) ([]byte, error) {
	return addressPayloadFromPublicKeyForChain(pub, ChainID)
}

func encodeLegacyAddress(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	return hex.EncodeToString(payload)
}

func encodePrefixedAddress(payload []byte) string {
	legacy := encodeLegacyAddress(payload)
	if legacy == "" {
		return ""
	}
	return addressPrefix + legacy
}

func stripAddressPrefix(addr string) string {
	addr = strings.TrimSpace(addr)
	if len(addr) >= len(addressPrefix) && strings.EqualFold(addr[:len(addressPrefix)], addressPrefix) {
		return strings.TrimSpace(addr[len(addressPrefix):])
	}
	return addr
}

func decodeAddressPayload(addr string) ([]byte, error) {
	raw := stripAddressPrefix(addr)
	if raw == "" {
		return nil, errors.New("empty address")
	}
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

func canonicalAddressKey(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if payload, err := decodeAddressPayload(addr); err == nil {
		return encodeLegacyAddress(payload)
	}
	return addr
}

func displayAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if payload, err := decodeAddressPayload(addr); err == nil {
		return encodePrefixedAddress(payload)
	}
	return addr
}

func addressesEqual(a, b string) bool {
	ak := canonicalAddressKey(a)
	bk := canonicalAddressKey(b)
	if ak == "" || bk == "" {
		return false
	}
	return strings.EqualFold(ak, bk)
}

func AddressMatchesPublicKey(addr string, pub ed25519.PublicKey) bool {
	got, err := decodeAddressPayload(addr)
	if err != nil {
		return false
	}
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

func AddressFromPublicKeyForChain(pub ed25519.PublicKey, chainID string) string {
	payload, err := addressPayloadFromPublicKeyForChain(pub, chainID)
	if err != nil {
		return ""
	}
	return encodePrefixedAddress(payload)
}

func AddressFromPublicKey(pub ed25519.PublicKey) string {
	return AddressFromPublicKeyForChain(pub, ChainID)
}

func LegacyAddressFromPublicKey(pub ed25519.PublicKey) string {
	payload, err := addressPayloadFromPublicKey(pub)
	if err != nil {
		return ""
	}
	return encodeLegacyAddress(payload)
}
