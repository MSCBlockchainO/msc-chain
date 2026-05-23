package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func withEconomicStakeMinimumGlobals(t *testing.T, minStake int64) {
	t.Helper()

	oldMinStake := ValidatorMinStake
	oldRequireStake := ValidatorRequireStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	t.Cleanup(func() {
		ValidatorMinStake = oldMinStake
		ValidatorRequireStake = oldRequireStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
	})

	ValidatorMinStake = minStake
	ValidatorRequireStake = true
	ValidatorCoreStakeExempt = false
}

func testConsensusValidatorPubKeyHex(t *testing.T) string {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("validator keygen failed: %v", err)
	}
	return strings.ToLower(hex.EncodeToString(pub))
}

func TestCurrentEconomicPolicyDefinesMainnetRules(t *testing.T) {
	policy := CurrentEconomicPolicy()
	if err := ValidateEconomicPolicy(policy); err != nil {
		t.Fatalf("current economic policy invalid: %v", err)
	}

	if policy.Version != EconomicPolicyVersion {
		t.Fatalf("unexpected policy version: got %q want %q", policy.Version, EconomicPolicyVersion)
	}
	if policy.Staking.ValidatorMinStake != ValidatorMinStake {
		t.Fatalf("policy min stake mismatch: got %d want %d", policy.Staking.ValidatorMinStake, ValidatorMinStake)
	}
	if !policy.Staking.OneWalletOneValidator {
		t.Fatalf("expected one-wallet-one-validator staking rule")
	}
	if !policy.Staking.ConsensusPubKeyRequired {
		t.Fatalf("expected validator consensus pubkey requirement")
	}
	if !policy.Staking.StakePersistsUntilUnstake {
		t.Fatalf("expected stake to persist until explicit unstake")
	}
	if policy.Staking.RejoinRequiresRestake {
		t.Fatalf("validator rejoin should not require restake while stake remains locked")
	}
	if policy.Slashing.SevereSlashExitAfter != SevereSlashExitAfter {
		t.Fatalf("severe slash exit mismatch: got %d want %d", policy.Slashing.SevereSlashExitAfter, SevereSlashExitAfter)
	}
	if policy.Inflation.TreasuryBPS+policy.Inflation.ValidatorBPS+policy.Inflation.BurnBPS > 10000 {
		t.Fatalf("emission bps exceeds 10000")
	}
	if !policy.Inflation.FixedSupplyCapEnforced {
		t.Fatalf("expected fixed supply cap enforcement in policy")
	}
	if !policy.Treasury.TransactionFeesToTreasury {
		t.Fatalf("expected transaction fees to route to treasury")
	}
	if !policy.Treasury.TreasuryOpsRequireAdmin {
		t.Fatalf("expected treasury ops to require admin control")
	}
	if policy.ValidatorMinimums.MaxActiveCommittee != ValidatorMaxActiveCommittee {
		t.Fatalf("active committee maximum mismatch: got %d want %d", policy.ValidatorMinimums.MaxActiveCommittee, ValidatorMaxActiveCommittee)
	}
}

func TestStakeBelowValidatorMinimumRejected(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()
	withEconomicStakeMinimumGlobals(t, 100)

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(nil)

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)

	tx, err := BuildSignedStakeTxSecure(
		wallet,
		"stake-pass",
		"F",
		testConsensusValidatorPubKeyHex(t),
		99,
		getNonce(ledger, wallet.Address),
		CoinSymbol,
		DefaultStakeLockEpochs,
	)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}

	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err == nil || !strings.Contains(err.Error(), "validator stake below minimum") {
		t.Fatalf("expected mempool min stake rejection, got=%v", err)
	}
	if _, err := ExecuteTransaction(&ledger, tx, 1); err == nil || !strings.Contains(err.Error(), "validator stake below minimum") {
		t.Fatalf("expected execution min stake rejection, got=%v", err)
	}
}

func TestStakeTopUpCanReachValidatorMinimum(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()
	withEconomicStakeMinimumGlobals(t, 100)

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	GlobalValidatorRegistry.Load(nil)

	wallet := newStakeConsensusPubKeyTestWallet(t, "stake-pass")
	validatorID := "F"
	ledger := GenesisLedger()
	addBalance(&ledger, CoinSymbol, wallet.Address, 10_000)
	ledger.Stakes[stakeKey(wallet.Address, validatorID)] = StakeLock{
		ValidatorID: validatorID,
		Amount:      60,
		LockedUntil: 10,
	}

	tx, err := BuildSignedStakeTxSecure(
		wallet,
		"stake-pass",
		validatorID,
		testConsensusValidatorPubKeyHex(t),
		40,
		getNonce(ledger, wallet.Address),
		CoinSymbol,
		DefaultStakeLockEpochs,
	)
	if err != nil {
		t.Fatalf("build stake tx: %v", err)
	}

	if err := (&Mempool{}).ValidateTransaction(tx, &ledger); err != nil {
		t.Fatalf("expected top-up to pass mempool validation: %v", err)
	}

	next, err := ExecuteTransaction(&ledger, tx, 1)
	if err != nil {
		t.Fatalf("expected top-up to execute: %v", err)
	}
	if got := validatorStakeTotalForMinimum(&next, validatorID); got != 100 {
		t.Fatalf("validator stake total mismatch: got %d want 100", got)
	}
}

func TestSevereSlashExitAfterPolicyMatchesRegistryBehavior(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() { GlobalValidatorRegistry.Load(oldRegistry) })

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"Z": {
			ID:     "Z",
			Status: ValidatorActive,
			Stake:  ValidatorMinStake,
		},
	})

	for i := 0; i < SevereSlashExitAfter; i++ {
		ApplyValidatorPenalty("Z", "invalid_block", uint64(10+i))
	}

	rec, ok := GlobalValidatorRegistry.Get("Z")
	if !ok || rec == nil {
		t.Fatalf("expected validator record after slashing")
	}
	if rec.Status != ValidatorExited {
		t.Fatalf("expected validator to exit after %d severe slashes, got %s", SevereSlashExitAfter, rec.Status)
	}
	if rec.TotalSlashes != SevereSlashExitAfter {
		t.Fatalf("total slashes mismatch: got %d want %d", rec.TotalSlashes, SevereSlashExitAfter)
	}
}
