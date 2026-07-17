package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	// `hdSchemeSLIP10Ed25519` defines the constant value used by this package.
	hdSchemeSLIP10Ed25519 = "bip39-slip10-ed25519"

	// `hdPurposeBIP44` defines the constant value used by this package.
	hdPurposeBIP44    uint32 = 44
	// `hdDefaultCoinType` defines the constant value used by this package.
	hdDefaultCoinType        = uint32(91938)

	// `hdMaxNonHardened` defines the constant value used by this package.
	hdMaxNonHardened = uint32(0x7fffffff)
	// `hdHardenedOffset` defines the constant value used by this package.
	hdHardenedOffset = uint32(0x80000000)

	// `hdDefaultAccount` defines the measured quantity used by this operation.
	hdDefaultAccount = uint32(0)
	// `hdDefaultChange` defines the constant value used by this package.
	hdDefaultChange  = uint32(0)
	// `hdDefaultIndex` defines the current position in the related collection.
	hdDefaultIndex   = uint32(0)
)

// hdCoinTypeFromChainID implements the hd coin type from chain id helper.
func hdCoinTypeFromChainID(chainID string) uint32 {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return hdDefaultCoinType
	}
	// `v` and `err` store the error produced by this operation.
	if v, err := strconv.ParseUint(chainID, 10, 31); err == nil {
		return uint32(v)
	}

	// Non-numeric chain IDs map to a stable 31-bit coin type.
	sum := sha256.Sum256([]byte(chainID))
	return binary.BigEndian.Uint32(sum[:4]) & hdMaxNonHardened
}

// hdPath implements the hd path helper.
func hdPath(coinType, account, change, index uint32) string {
	return fmt.Sprintf(
		"m/%d'/%d'/%d'/%d'/%d'",
		hdPurposeBIP44,
		coinType,
		account,
		change,
		index,
	)
}

// hdMeta implements the hd meta helper.
func hdMeta(account, change, index uint32) *HDWalletMeta {
	// `coinType` stores the value produced by this operation.
	coinType := hdCoinTypeFromChainID(protocolChainID())
	return &HDWalletMeta{
		Scheme:   hdSchemeSLIP10Ed25519,
		Path:     hdPath(coinType, account, change, index),
		Purpose:  hdPurposeBIP44,
		CoinType: coinType,
		Account:  account,
		Change:   change,
		Index:    index,
	}
}

// hdHardenedIndex implements the hd hardened index helper.
func hdHardenedIndex(v uint32) (uint32, error) {
	if v > hdMaxNonHardened {
		return 0, fmt.Errorf("hd index out of range: %d", v)
	}
	return v + hdHardenedOffset, nil
}

// slip10MasterEd25519 implements the slip10 master ed25519 helper.
func slip10MasterEd25519(seed []byte) ([]byte, []byte, error) {
	if len(seed) == 0 {
		return nil, nil, fmt.Errorf("empty seed")
	}
	// `mac` stores the value produced by this operation.
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	// `err` stores the error produced by this operation.
	if _, err := mac.Write(seed); err != nil {
		return nil, nil, err
	}
	// `out` stores the result produced by this operation.
	out := mac.Sum(nil)
	if len(out) != 64 {
		return nil, nil, fmt.Errorf("invalid master key length")
	}
	// `key` stores the key used to access the related value.
	key := append([]byte(nil), out[:32]...)
	// `cc` stores the value produced by this operation.
	cc := append([]byte(nil), out[32:]...)
	return key, cc, nil
}

// slip10DeriveChildEd25519 implements the slip10 derive child ed25519 helper.
func slip10DeriveChildEd25519(
	key []byte,
	chainCode []byte,
	childIndex uint32,
) ([]byte, []byte, error) {
	if len(key) != 32 || len(chainCode) != 32 {
		return nil, nil, fmt.Errorf("invalid slip10 state")
	}
	if childIndex < hdHardenedOffset {
		return nil, nil, fmt.Errorf("ed25519 supports hardened derivation only")
	}

	// `data` stores the value produced by this operation.
	data := make([]byte, 1+32+4)
	data[0] = 0x00
	copy(data[1:], key)
	binary.BigEndian.PutUint32(data[33:], childIndex)

	// `mac` stores the value produced by this operation.
	mac := hmac.New(sha512.New, chainCode)
	// `err` stores the error produced by this operation.
	if _, err := mac.Write(data); err != nil {
		return nil, nil, err
	}
	// `out` stores the result produced by this operation.
	out := mac.Sum(nil)
	if len(out) != 64 {
		return nil, nil, fmt.Errorf("invalid child key length")
	}
	// `nextKey` stores the key used to access the related value.
	nextKey := append([]byte(nil), out[:32]...)
	// `nextCC` stores the value produced by this operation.
	nextCC := append([]byte(nil), out[32:]...)
	return nextKey, nextCC, nil
}

// deriveHDPrivateKeyFromSeed implements the derive hd private key from seed helper.
func deriveHDPrivateKeyFromSeed(
	seed []byte,
	account uint32,
	change uint32,
	index uint32,
) (ed25519.PrivateKey, *HDWalletMeta, error) {
	// `key`, `cc`, and `err` store the error produced by this operation.
	key, cc, err := slip10MasterEd25519(seed)
	if err != nil {
		return nil, nil, err
	}

	// `parts` stores the value produced by this operation.
	parts := []uint32{
		hdPurposeBIP44,
		hdCoinTypeFromChainID(protocolChainID()),
		account,
		change,
		index,
	}
	// `part` tracks the current values while iterating.
	for _, part := range parts {
		// `h` and `err` store the error produced by this operation.
		h, err := hdHardenedIndex(part)
		if err != nil {
			ZeroMemory(key)
			ZeroMemory(cc)
			return nil, nil, err
		}
		// `nextKey`, `nextCC`, and `err` store the error produced by this operation.
		nextKey, nextCC, err := slip10DeriveChildEd25519(key, cc, h)
		ZeroMemory(key)
		ZeroMemory(cc)
		if err != nil {
			return nil, nil, err
		}
		key, cc = nextKey, nextCC
	}

	// `priv` stores the value produced by this operation.
	priv := ed25519.NewKeyFromSeed(key)
	// `out` stores the result produced by this operation.
	out := make([]byte, len(priv))
	copy(out, priv)
	ZeroMemory(key)
	ZeroMemory(cc)

	return ed25519.PrivateKey(out), hdMeta(account, change, index), nil
}

// deriveHDKeypairFromSeed implements the derive hd keypair from seed helper.
func deriveHDKeypairFromSeed(
	seed []byte,
	account uint32,
	change uint32,
	index uint32,
) (ed25519.PublicKey, ed25519.PrivateKey, *HDWalletMeta, error) {
	// `priv`, `meta`, and `err` store the error produced by this operation.
	priv, meta, err := deriveHDPrivateKeyFromSeed(seed, account, change, index)
	if err != nil {
		return nil, nil, nil, err
	}

	// `pubAny` stores the value produced by this operation.
	pubAny := priv.Public()
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		ZeroMemory(priv)
		return nil, nil, nil, fmt.Errorf("failed to derive public key")
	}

	// `pubCopy` stores the value produced by this operation.
	pubCopy := make([]byte, len(pub))
	copy(pubCopy, pub)

	return ed25519.PublicKey(pubCopy), priv, meta, nil
}
