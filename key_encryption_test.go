package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func legacyEncryptForTest(priv ed25519.PrivateKey, password string) EncryptedKey {
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	_, _ = cryptorand.Read(salt)
	_, _ = cryptorand.Read(nonce)

	key := sha256.Sum256(append([]byte(password), salt...))
	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
	ciphertext := gcm.Seal(nil, nonce, priv, nil)

	return EncryptedKey{
		Ciphertext: hex.EncodeToString(ciphertext),
		Nonce:      hex.EncodeToString(nonce),
		Salt:       hex.EncodeToString(salt),
	}
}

func TestEncryptPrivateKeyV2RoundTrip(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	defer ZeroMemory(priv)

	enc, err := EncryptPrivateKey(priv, "test-password-123")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if !isEncryptedKeyV2(enc) {
		t.Fatalf("expected v2 encrypted key metadata")
	}

	sw := SecureWallet{
		Address:   "dummy",
		PublicKey: "dummy",
		Crypto:    enc,
	}
	got, err := DecryptPrivateKey(sw, "test-password-123")
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	defer ZeroMemory(got)

	if len(got) != len(priv) {
		t.Fatalf("private key length mismatch: got=%d want=%d", len(got), len(priv))
	}
	for i := range got {
		if got[i] != priv[i] {
			t.Fatalf("private key mismatch at byte %d", i)
		}
	}
}

func TestDecryptPrivateKeyLegacyCompatibility(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	defer ZeroMemory(priv)

	legacy := legacyEncryptForTest(priv, "legacy-pass")
	if isEncryptedKeyV2(legacy) {
		t.Fatalf("legacy fixture must not be v2")
	}

	sw := SecureWallet{
		Address:   "dummy",
		PublicKey: "dummy",
		Crypto:    legacy,
	}
	got, err := DecryptPrivateKey(sw, "legacy-pass")
	if err != nil {
		t.Fatalf("legacy decrypt failed: %v", err)
	}
	defer ZeroMemory(got)

	if len(got) != len(priv) {
		t.Fatalf("private key length mismatch: got=%d want=%d", len(got), len(priv))
	}
	for i := range got {
		if got[i] != priv[i] {
			t.Fatalf("private key mismatch at byte %d", i)
		}
	}
}
