package main

import (
	"strings"
	"testing"
	"time"
)

func TestFaucetTxRejectedOnMainnetSubmitPath(t *testing.T) {
	oldIsTestnet := IsTestnet
	IsTestnet = false
	t.Cleanup(func() { IsTestnet = oldIsTestnet })

	ledger := Ledger{
		Balances: map[string]int{
			balanceKey(CoinSymbol, USER_REWARD_POOL): 10_000,
		},
		Nonces: make(map[string]int),
		Stakes: make(map[string]StakeLock),
	}
	tx := Transaction{
		From:    USER_REWARD_POOL,
		To:      "MSC_FAUCET_RECIPIENT",
		Amount:  10,
		Nonce:   1,
		Fee:     ComputeTxFee(10),
		Expiry:  time.Now().Add(time.Hour).Unix(),
		Type:    TxFaucet,
		ChainID: ChainID,
		Coin:    CoinSymbol,
	}

	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err == nil || !strings.Contains(err.Error(), "faucet disabled on mainnet") {
		t.Fatalf("expected mempool mainnet faucet rejection, got %v", err)
	}
	if _, err := ExecuteTransaction(&ledger, tx, 1); err == nil || !strings.Contains(err.Error(), "faucet disabled on mainnet") {
		t.Fatalf("expected execution mainnet faucet rejection, got %v", err)
	}
}
