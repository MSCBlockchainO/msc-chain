package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func TestOperatorWalletFromPrivateKeyHexAcceptsSeedAndPrivateKey(t *testing.T) {
	password := "operator-pass"
	seed := make([]byte, ed25519.SeedSize)
	if _, err := cryptorand.Read(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	fromSeed, err := operatorWalletFromPrivateKeyHex(hex.EncodeToString(seed), password)
	if err != nil {
		t.Fatalf("import seed: %v", err)
	}
	if fromSeed.PublicKey != hex.EncodeToString(pub) {
		t.Fatalf("seed import public key mismatch")
	}

	fromPrivate, err := operatorWalletFromPrivateKeyHex("0x"+hex.EncodeToString(priv), password)
	if err != nil {
		t.Fatalf("import private key: %v", err)
	}
	if fromPrivate.Address != fromSeed.Address || fromPrivate.PublicKey != fromSeed.PublicKey {
		t.Fatalf("seed/private import mismatch: seed=%+v private=%+v", fromSeed, fromPrivate)
	}
	if _, err := DecryptPrivateKey(fromPrivate, password); err != nil {
		t.Fatalf("decrypt imported wallet: %v", err)
	}
}

func TestOperatorEndpointNormalizesBaseURL(t *testing.T) {
	got, err := operatorEndpoint("127.0.0.1:26657", "/v1/peers", nil)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if got != "http://127.0.0.1:26657/v1/peers" {
		t.Fatalf("unexpected endpoint %q", got)
	}
}

func TestOperatorCLICommandRecognition(t *testing.T) {
	for _, cmd := range []string{"wallet", "validator-keygen", "validator-pubkey", "validator", "stake", "unstake", "claim-rewards", "status", "peers", "sync-status"} {
		if !isOperatorCLICommand(cmd) {
			t.Fatalf("expected %s to be an operator command", cmd)
		}
	}
	if isOperatorCLICommand("--mode") || isOperatorCLICommand("node") || strings.TrimSpace(" ") != "" {
		t.Fatalf("unexpected operator command recognition")
	}
}
