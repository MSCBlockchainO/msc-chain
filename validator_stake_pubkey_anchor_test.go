package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func withStakeConsensusPubKeyGlobals(t *testing.T) func() {
	t.Helper()

	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRegistry := GlobalValidatorRegistry.Snapshot()

	validatorPubKeysMu.RLock()
	oldRuntimePub := make(map[string]ed25519.PublicKey, len(ValidatorPubKeys))
	for id, pub := range ValidatorPubKeys {
		oldRuntimePub[id] = append(ed25519.PublicKey(nil), pub...)
	}
	oldGenesisPub := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pub := range GenesisValidatorPubKeys {
		oldGenesisPub[id] = append(ed25519.PublicKey(nil), pub...)
	}
	validatorPubKeysMu.RUnlock()

	return func() {
		ConfigAuthCoreValidators = oldCoreValidators
		GlobalValidatorRegistry.Load(oldRegistry)
		validatorPubKeysMu.Lock()
		ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldRuntimePub))
		for id, pub := range oldRuntimePub {
			ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisPub))
		for id, pub := range oldGenesisPub {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		validatorPubKeysMu.Unlock()
	}
}

func newStakeConsensusPubKeyTestWallet(t *testing.T, password string) SecureWallet {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	t.Cleanup(func() { ZeroMemory(priv) })

	enc, err := EncryptPrivateKey(priv, password)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	return SecureWallet{
		Address:   AddressFromPublicKey(pub),
		PublicKey: strings.ToLower(hex.EncodeToString(pub)),
		Crypto:    enc,
	}
}

func TestValidateStakeTransactionRequiresValidatorPubKeyForFirstNonCoreStake(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(nil)

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)

	tx, err := BuildSignedStakeTxSecure(wallet, "stake-pass", "F", "", 100, getNonce(ledger, wallet.Address), CoinSymbol, DefaultStakeLockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}

	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err == nil || !strings.Contains(err.Error(), "validator_pubkey required") {
		t.Fatalf("expected validator_pubkey required error, got=%v", err)
	}
}

func TestValidateStakeTransactionRejectsWalletPublicKeyAsValidatorPubKey(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(nil)

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)

	tx, err := BuildSignedStakeTxSecure(wallet, "stake-pass", "F", wallet.PublicKey, 100, getNonce(ledger, wallet.Address), CoinSymbol, DefaultStakeLockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}

	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err == nil || !strings.Contains(err.Error(), "not wallet public key") {
		t.Fatalf("expected wallet public key rejection, got=%v", err)
	}
}

func TestUpdateValidatorMetricsFromBlockAnchorsConsensusPubKeyFromStake(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(nil)

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)

	validatorPub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("validator keygen failed: %v", err)
	}
	validatorPubHex := strings.ToLower(hex.EncodeToString(validatorPub))

	tx, err := BuildSignedStakeTxSecure(wallet, "stake-pass", "F", validatorPubHex, 100, getNonce(ledger, wallet.Address), CoinSymbol, DefaultStakeLockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}
	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err != nil {
		t.Fatalf("validate stake tx: %v", err)
	}

	bc := NewBlockchain()
	node := &Node{Blockchain: &bc}
	node.UpdateValidatorMetricsFromBlock(Block{
		ID:           1,
		Transactions: []Transaction{tx},
	})

	snapshot := GlobalValidatorRegistry.Snapshot()
	rec, ok := snapshot["F"]
	if !ok {
		t.Fatalf("expected validator record for F to be created")
	}
	if rec.ConsensusPubKey != validatorPubHex {
		t.Fatalf("expected committed registry to anchor validator pubkey, got=%q want=%q", rec.ConsensusPubKey, validatorPubHex)
	}
}

func TestApplyBlockStatePersistsStakeConsensusPubKeyInLedger(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(nil)

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)

	validatorPub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("validator keygen failed: %v", err)
	}
	validatorPubHex := strings.ToLower(hex.EncodeToString(validatorPub))

	tx, err := BuildSignedStakeTxSecure(wallet, "stake-pass", "F", validatorPubHex, 100, getNonce(ledger, wallet.Address), CoinSymbol, DefaultStakeLockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}
	next, err := ApplyBlockState(ledger, Block{ID: 1, Transactions: []Transaction{tx}})
	if err != nil {
		t.Fatalf("apply block: %v", err)
	}
	lock := next.Stakes[stakeKey(wallet.Address, "F")]
	if lock.ConsensusPubKey != validatorPubHex {
		t.Fatalf("stake lock consensus pubkey = %q, want %q", lock.ConsensusPubKey, validatorPubHex)
	}
}

func TestUpdateValidatorMetricsSchedulesAlreadyStakedValidatorWhenPubKeyAnchored(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	oldFrozen := GenesisValidatorSetFrozen
	defer func() { GenesisValidatorSetFrozen = oldFrozen }()
	GenesisValidatorSetFrozen = false

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"F": {
			ID:         "F",
			Stake:      ValidatorMinStake,
			Status:     ValidatorPending,
			JoinHeight: 0,
		},
	})

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	validatorPub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("validator keygen failed: %v", err)
	}
	validatorPubHex := strings.ToLower(hex.EncodeToString(validatorPub))
	tx, err := BuildSignedStakeTxSecure(wallet, "stake-pass", "F", validatorPubHex, 1, 0, CoinSymbol, DefaultStakeLockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}

	const height uint64 = 10
	node := &Node{
		ID:                       "A",
		pendingValidators:        map[string]uint64{},
		pendingValidatorRemovals: map[string]uint64{},
	}
	node.UpdateValidatorMetricsFromBlock(Block{ID: height, Transactions: []Transaction{tx}})

	snapshot := GlobalValidatorRegistry.Snapshot()
	rec := snapshot["F"]
	if rec.ConsensusPubKey != validatorPubHex {
		t.Fatalf("expected registry to anchor validator pubkey, got=%q want=%q", rec.ConsensusPubKey, validatorPubHex)
	}
	wantActivation := height + validatorSetActivationDelayBlocks()
	if got := node.onboardingPendingAddHeight("F"); got != wantActivation {
		t.Fatalf("pending add height = %d, want %d", got, wantActivation)
	}
}

func TestValidateStakeTransactionRejectsConflictingAnchoredValidatorPubKey(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize),
			Stake:           100,
			Status:          ValidatorPending,
			JoinHeight:      1,
		},
	})

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)

	tx, err := BuildSignedStakeTxSecure(wallet, "stake-pass", "F", strings.Repeat("22", ed25519.PublicKeySize), 100, getNonce(ledger, wallet.Address), CoinSymbol, DefaultStakeLockEpochs)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}

	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err == nil || !strings.Contains(err.Error(), "conflicts with anchored consensus pubkey") {
		t.Fatalf("expected anchored pubkey conflict error, got=%v", err)
	}
}
