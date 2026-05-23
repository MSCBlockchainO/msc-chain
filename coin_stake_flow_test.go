package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func newSecureWalletForFlowTest(t *testing.T, password string) SecureWallet {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate wallet key: %v", err)
	}
	enc, err := EncryptPrivateKey(priv, password)
	if err != nil {
		t.Fatalf("encrypt wallet key: %v", err)
	}
	return SecureWallet{
		Address:   AddressFromPublicKey(pub),
		PublicKey: hex.EncodeToString(pub),
		Crypto:    enc,
	}
}

func TestCoinTransferStakeAndMaturedUnstakeFlow(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	password := "flow-test-password"
	sender := newSecureWalletForFlowTest(t, password)
	receiver := newSecureWalletForFlowTest(t, password)
	validatorPub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate validator pubkey: %v", err)
	}
	validatorPubHex := strings.ToLower(hex.EncodeToString(validatorPub))
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"G": {
			ID:              "G",
			Stake:           100,
			Status:          ValidatorActive,
			ConsensusPubKey: validatorPubHex,
		},
	})

	ledger := Ledger{
		Balances: make(map[string]int),
		Nonces:   make(map[string]int),
		Stakes:   make(map[string]StakeLock),
	}
	addBalance(&ledger, CoinSymbol, sender.Address, 1000)

	transfer, err := BuildSignedTxSecure(sender, password, receiver.Address, 120, getNonce(ledger, sender.Address), CoinSymbol)
	if err != nil {
		t.Fatalf("build transfer tx: %v", err)
	}
	ledgerAfterTransfer, err := ExecuteTransaction(&ledger, transfer, 1)
	if err != nil {
		t.Fatalf("execute transfer tx: %v", err)
	}
	ledger = ledgerAfterTransfer
	if got := getBalance(ledger, CoinSymbol, receiver.Address); got != 120 {
		t.Fatalf("receiver balance mismatch: got=%d want=120", got)
	}

	lockEpochs := minUnstakeEpochs()
	stake, err := BuildSignedStakeTxSecure(receiver, password, "G", validatorPubHex, 100, getNonce(ledger, receiver.Address), CoinSymbol, lockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}
	stakeHeight := 2
	ledgerAfterStake, err := ExecuteTransaction(&ledger, stake, stakeHeight)
	if err != nil {
		t.Fatalf("execute stake tx: %v", err)
	}
	ledger = ledgerAfterStake
	stakeRec := ledger.Stakes[stakeKey(receiver.Address, "G")]
	if stakeRec.Amount != 100 {
		t.Fatalf("stake amount mismatch: got=%d want=100", stakeRec.Amount)
	}
	if want := uint64(stakeHeight) + lockEpochs; stakeRec.LockedUntil != want {
		t.Fatalf("stake lock mismatch: got=%d want=%d", stakeRec.LockedUntil, want)
	}

	lockedUnstake, err := BuildSignedUnstakeTxSecure(receiver, password, "G", 60, getNonce(ledger, receiver.Address), CoinSymbol)
	if err != nil {
		t.Fatalf("build locked unstake tx: %v", err)
	}
	if _, err := ExecuteTransaction(&ledger, lockedUnstake, stakeHeight+1); err == nil {
		t.Fatalf("expected locked unstake to fail")
	} else if !strings.Contains(err.Error(), "stake still locked") {
		t.Fatalf("unexpected locked unstake error: %v", err)
	}

	maturedUnstake, err := BuildSignedUnstakeTxSecure(receiver, password, "G", 60, getNonce(ledger, receiver.Address), CoinSymbol)
	if err != nil {
		t.Fatalf("build matured unstake tx: %v", err)
	}
	ledgerAfterUnstake, err := ExecuteTransaction(&ledger, maturedUnstake, int(stakeRec.LockedUntil))
	if err != nil {
		t.Fatalf("execute matured unstake tx: %v", err)
	}
	ledger = ledgerAfterUnstake
	stakeRec = ledger.Stakes[stakeKey(receiver.Address, "G")]
	if stakeRec.Amount != 40 {
		t.Fatalf("remaining stake mismatch: got=%d want=40", stakeRec.Amount)
	}
	if got := getBalance(ledger, CoinSymbol, receiver.Address); got <= 0 {
		t.Fatalf("expected receiver to receive unstaked balance, got=%d", got)
	}
}
