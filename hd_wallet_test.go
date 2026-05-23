package main

import (
	"bytes"
	"testing"

	"github.com/tyler-smith/go-bip39"
)

func TestHDKeyDerivationDeterministic(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed := bip39.NewSeed(mnemonic, "test-pass")

	pubA, privA, metaA, err := deriveHDKeypairFromSeed(seed, 0, 0, 0)
	if err != nil {
		t.Fatalf("derive A failed: %v", err)
	}
	defer ZeroMemory(privA)

	pubB, privB, metaB, err := deriveHDKeypairFromSeed(seed, 0, 0, 0)
	if err != nil {
		t.Fatalf("derive B failed: %v", err)
	}
	defer ZeroMemory(privB)

	if !bytes.Equal(pubA, pubB) {
		t.Fatalf("public key mismatch for same mnemonic/path")
	}
	if !bytes.Equal(privA, privB) {
		t.Fatalf("private key mismatch for same mnemonic/path")
	}
	if metaA == nil || metaB == nil || metaA.Path == "" || metaA.Path != metaB.Path {
		t.Fatalf("invalid HD metadata path")
	}
}

func TestHDKeyDerivationDifferentIndex(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed := bip39.NewSeed(mnemonic, "test-pass")

	pubA, privA, _, err := deriveHDKeypairFromSeed(seed, 0, 0, 0)
	if err != nil {
		t.Fatalf("derive index0 failed: %v", err)
	}
	defer ZeroMemory(privA)

	pubB, privB, _, err := deriveHDKeypairFromSeed(seed, 0, 0, 1)
	if err != nil {
		t.Fatalf("derive index1 failed: %v", err)
	}
	defer ZeroMemory(privB)

	if bytes.Equal(pubA, pubB) {
		t.Fatalf("expected different pubkeys for different HD indices")
	}
}

func TestRecoverWalletWithPathMetadata(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	w0, err := RecoverWalletWithPath(mnemonic, "test-pass", 0, 0, 0)
	if err != nil {
		t.Fatalf("recover failed: %v", err)
	}
	w1, err := RecoverWalletWithPath(mnemonic, "test-pass", 0, 0, 1)
	if err != nil {
		t.Fatalf("recover index 1 failed: %v", err)
	}

	if w0.Address == w1.Address {
		t.Fatalf("expected different addresses for different indices")
	}
	if w0.HD == nil || w1.HD == nil {
		t.Fatalf("expected HD metadata in recovered wallet")
	}
	if w0.HD.Index != 0 || w1.HD.Index != 1 {
		t.Fatalf("unexpected HD index metadata: got %d and %d", w0.HD.Index, w1.HD.Index)
	}
	if w0.HD.Path == "" || w1.HD.Path == "" {
		t.Fatalf("expected non-empty HD derivation path")
	}
}
