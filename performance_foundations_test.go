package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

func signedPerformanceTx(t *testing.T, amount int) Transaction {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	from := AddressFromPublicKey(pub)
	tx := Transaction{
		From:      from,
		To:        "MSC010000000000000000000000000000000000000000",
		Amount:    amount,
		Nonce:     1,
		PublicKey: hex.EncodeToString(pub),
		Fee:       ComputeTxFee(amount),
		Expiry:    time.Now().Add(time.Hour).Unix(),
		ChainID:   ChainID,
		Coin:      CoinSymbol,
		Type:      TxTransfer,
	}
	tx.Signature = hex.EncodeToString(Sign(priv, TxPayload(tx)))
	tx.ID = ComputeTxID(tx)
	return tx
}

func TestParallelValidateTransactionsPreservesOrderAndMatchesSequential(t *testing.T) {
	txs := []Transaction{
		signedPerformanceTx(t, 10),
		signedPerformanceTx(t, 20),
		signedPerformanceTx(t, 30),
	}
	txs[1].Signature = "00"

	ledger := NewLedger()
	for _, tx := range txs {
		addBalance(&ledger, tx.Coin, tx.From, 10_000)
	}

	got := ParallelValidateTransactions(txs, ledger, 3)
	validator := &Mempool{}
	for i, tx := range txs {
		ledgerCopy := ledger.Clone()
		err := validator.ValidateTransaction(tx, &ledgerCopy)
		wantValid := err == nil
		wantErr := ""
		if err != nil {
			wantErr = err.Error()
		}
		if got[i].Index != i {
			t.Fatalf("result index[%d]=%d", i, got[i].Index)
		}
		if got[i].Valid != wantValid || got[i].Error != wantErr {
			t.Fatalf("result[%d]=%+v want valid=%v err=%q", i, got[i], wantValid, wantErr)
		}
	}
}

func TestMempoolPriorityTransactionsPreservesNonceChains(t *testing.T) {
	oldMaxBlock := GlobalConfig.MaxTxPerBlock
	defer func() {
		GlobalConfig.MaxTxPerBlock = oldMaxBlock
	}()
	GlobalConfig.MaxTxPerBlock = 10

	ledger := NewLedger()
	ledger.Nonces[nonceKey("A")] = 0
	ledger.Nonces[nonceKey("B")] = 0

	a2 := Transaction{ID: "a2", From: "A", Nonce: 2, Fee: 100}
	b1 := Transaction{ID: "b1", From: "B", Nonce: 1, Fee: 50}
	a1 := Transaction{ID: "a1", From: "A", Nonce: 1, Fee: 10}
	mempool := &Mempool{Transactions: []Transaction{a2, b1, a1}}

	got := mempool.PriorityTransactions(ledger)
	ids := make([]string, 0, len(got))
	for _, tx := range got {
		ids = append(ids, tx.ID)
	}
	want := []string{"b1", "a1", "a2"}
	if len(ids) != len(want) {
		t.Fatalf("selected ids=%v want=%v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("selected ids=%v want=%v", ids, want)
		}
	}
}

func TestAccountStateCacheHitMissInvalidate(t *testing.T) {
	loads := 0
	cache := NewAccountStateCache(func(coin string, address string) (int, bool) {
		loads++
		if address != "alice" || coin != CoinSymbol {
			return 0, false
		}
		return 42, true
	})

	if value, ok := cache.GetBalance(CoinSymbol, "alice"); !ok || value != 42 {
		t.Fatalf("first cache load got value=%d ok=%v", value, ok)
	}
	if value, ok := cache.GetBalance(CoinSymbol, "alice"); !ok || value != 42 {
		t.Fatalf("second cache load got value=%d ok=%v", value, ok)
	}
	if loads != 1 {
		t.Fatalf("loader calls = %d, want 1", loads)
	}
	cache.PutBalance(CoinSymbol, "alice", 99)
	if value, ok := cache.GetBalance(CoinSymbol, "alice"); !ok || value != 99 {
		t.Fatalf("cached put got value=%d ok=%v", value, ok)
	}
	cache.InvalidateBalance(CoinSymbol, "alice")
	if value, ok := cache.GetBalance(CoinSymbol, "alice"); !ok || value != 42 {
		t.Fatalf("after invalidate got value=%d ok=%v", value, ok)
	}
	if cache.Size() != 1 {
		t.Fatalf("cache size = %d, want 1", cache.Size())
	}
}
