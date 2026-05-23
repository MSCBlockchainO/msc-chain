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
	hdSchemeSLIP10Ed25519 = "bip39-slip10-ed25519"

	hdPurposeBIP44    uint32 = 44
	hdDefaultCoinType        = uint32(91938)

	hdMaxNonHardened = uint32(0x7fffffff)
	hdHardenedOffset = uint32(0x80000000)

	hdDefaultAccount = uint32(0)
	hdDefaultChange  = uint32(0)
	hdDefaultIndex   = uint32(0)
)

func hdCoinTypeFromChainID(chainID string) uint32 {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return hdDefaultCoinType
	}
	if v, err := strconv.ParseUint(chainID, 10, 31); err == nil {
		return uint32(v)
	}

	// Non-numeric chain IDs map to a stable 31-bit coin type.
	sum := sha256.Sum256([]byte(chainID))
	return binary.BigEndian.Uint32(sum[:4]) & hdMaxNonHardened
}

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

func hdMeta(account, change, index uint32) *HDWalletMeta {
	coinType := hdCoinTypeFromChainID(ChainID)
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

func hdHardenedIndex(v uint32) (uint32, error) {
	if v > hdMaxNonHardened {
		return 0, fmt.Errorf("hd index out of range: %d", v)
	}
	return v + hdHardenedOffset, nil
}

func slip10MasterEd25519(seed []byte) ([]byte, []byte, error) {
	if len(seed) == 0 {
		return nil, nil, fmt.Errorf("empty seed")
	}
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	if _, err := mac.Write(seed); err != nil {
		return nil, nil, err
	}
	out := mac.Sum(nil)
	if len(out) != 64 {
		return nil, nil, fmt.Errorf("invalid master key length")
	}
	key := append([]byte(nil), out[:32]...)
	cc := append([]byte(nil), out[32:]...)
	return key, cc, nil
}

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

	data := make([]byte, 1+32+4)
	data[0] = 0x00
	copy(data[1:], key)
	binary.BigEndian.PutUint32(data[33:], childIndex)

	mac := hmac.New(sha512.New, chainCode)
	if _, err := mac.Write(data); err != nil {
		return nil, nil, err
	}
	out := mac.Sum(nil)
	if len(out) != 64 {
		return nil, nil, fmt.Errorf("invalid child key length")
	}
	nextKey := append([]byte(nil), out[:32]...)
	nextCC := append([]byte(nil), out[32:]...)
	return nextKey, nextCC, nil
}

func deriveHDPrivateKeyFromSeed(
	seed []byte,
	account uint32,
	change uint32,
	index uint32,
) (ed25519.PrivateKey, *HDWalletMeta, error) {
	key, cc, err := slip10MasterEd25519(seed)
	if err != nil {
		return nil, nil, err
	}

	parts := []uint32{
		hdPurposeBIP44,
		hdCoinTypeFromChainID(ChainID),
		account,
		change,
		index,
	}
	for _, part := range parts {
		h, err := hdHardenedIndex(part)
		if err != nil {
			ZeroMemory(key)
			ZeroMemory(cc)
			return nil, nil, err
		}
		nextKey, nextCC, err := slip10DeriveChildEd25519(key, cc, h)
		ZeroMemory(key)
		ZeroMemory(cc)
		if err != nil {
			return nil, nil, err
		}
		key, cc = nextKey, nextCC
	}

	priv := ed25519.NewKeyFromSeed(key)
	out := make([]byte, len(priv))
	copy(out, priv)
	ZeroMemory(key)
	ZeroMemory(cc)

	return ed25519.PrivateKey(out), hdMeta(account, change, index), nil
}

func deriveHDKeypairFromSeed(
	seed []byte,
	account uint32,
	change uint32,
	index uint32,
) (ed25519.PublicKey, ed25519.PrivateKey, *HDWalletMeta, error) {
	priv, meta, err := deriveHDPrivateKeyFromSeed(seed, account, change, index)
	if err != nil {
		return nil, nil, nil, err
	}

	pubAny := priv.Public()
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		ZeroMemory(priv)
		return nil, nil, nil, fmt.Errorf("failed to derive public key")
	}

	pubCopy := make([]byte, len(pub))
	copy(pubCopy, pub)

	return ed25519.PublicKey(pubCopy), priv, meta, nil
}
