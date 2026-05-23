package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func signedMempoolIngressTx(t *testing.T, ledger *Ledger, seedLabel string, nonce int) (Transaction, ed25519.PrivateKey) {
	t.Helper()
	seed := sha256.Sum256([]byte(seedLabel))
	priv := ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)
	addr := AddressFromPublicKey(pub)
	toSeed := sha256.Sum256([]byte(seedLabel + "-to"))
	toPriv := ed25519.NewKeyFromSeed(toSeed[:])
	toPub := toPriv.Public().(ed25519.PublicKey)
	addBalance(ledger, CoinSymbol, addr, 1_000_000)

	tx := Transaction{
		From:      addr,
		To:        AddressFromPublicKey(toPub),
		Amount:    7,
		Nonce:     nonce,
		PublicKey: hex.EncodeToString(pub),
		Expiry:    time.Now().Add(5 * time.Minute).Unix(),
		Type:      TxTransfer,
		ChainID:   ChainID,
		Coin:      CoinSymbol,
	}
	tx.Fee = requiredFeeForTxWithLedger(ledger, tx)
	tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
	tx.ID = ComputeTxID(tx)
	return tx, priv
}

func TestMempoolIngressRejectsInvalidSignatureDirectAdd(t *testing.T) {
	resetTxAbuseForTest(t)
	ledger := NewLedger()
	tx, _ := signedMempoolIngressTx(t, &ledger, "mempool-invalid-signature", 1)
	tx.Signature = hex.EncodeToString(make([]byte, ed25519.SignatureSize))
	tx.ID = ComputeTxID(tx)

	ok, reason := (&Mempool{}).AddTransaction(tx, ledger, 1)
	if ok || !strings.Contains(reason, "signature verification failed") {
		t.Fatalf("invalid signature direct mempool add must be rejected, ok=%t reason=%q", ok, reason)
	}
}

func TestMempoolIngressRejectsWrongChainReplayDirectAdd(t *testing.T) {
	resetTxAbuseForTest(t)
	ledger := NewLedger()
	tx, priv := signedMempoolIngressTx(t, &ledger, "mempool-wrong-chain", 1)
	tx.ChainID = ChainID + "-replay"
	tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
	tx.ID = ComputeTxID(tx)

	ok, reason := (&Mempool{}).AddTransaction(tx, ledger, 1)
	if ok || !strings.Contains(reason, "invalid chain id") {
		t.Fatalf("wrong-chain replay direct mempool add must be rejected, ok=%t reason=%q", ok, reason)
	}
}

func TestMempoolIngressAcceptsValidSignedDirectAdd(t *testing.T) {
	resetTxAbuseForTest(t)
	pendingSpendMu.Lock()
	oldPendingSpend := PendingSpend
	PendingSpend = map[string]int{}
	pendingSpendMu.Unlock()
	t.Cleanup(func() {
		pendingSpendMu.Lock()
		PendingSpend = oldPendingSpend
		pendingSpendMu.Unlock()
	})

	ledger := NewLedger()
	tx, _ := signedMempoolIngressTx(t, &ledger, "mempool-valid-signed", 1)
	mempool := &Mempool{}

	ok, reason := mempool.AddTransaction(tx, ledger, 1)
	if !ok {
		t.Fatalf("valid signed direct mempool add rejected: %s", reason)
	}
	if !mempool.HasTx(tx.ID) {
		t.Fatalf("accepted tx missing from mempool index: %s", tx.ID)
	}
}
