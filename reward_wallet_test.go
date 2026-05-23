package main

import "testing"

func TestValidatorRewardWallet(t *testing.T) {
	setGenesisRewardWallets(nil)
	ledger := NewLedger()

	// Highest stake wins.
	ledger.Stakes[stakeKey("wallet-z", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 100}
	ledger.Stakes[stakeKey("wallet-b", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 250}
	ledger.Stakes[stakeKey("wallet-a", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 250}

	got, ok := validatorRewardWallet(&ledger, "VAL1")
	if !ok {
		t.Fatalf("expected reward wallet for validator")
	}
	// Tie-break on equal stake must be lexicographic (wallet-a over wallet-b).
	if got != "wallet-a" {
		t.Fatalf("unexpected reward wallet: got=%q want=%q", got, "wallet-a")
	}
}

func TestValidatorRewardWalletPrefersBoundWallet(t *testing.T) {
	setGenesisRewardWallets(nil)
	ledger := NewLedger()

	ledger.Stakes[stakeKey("wallet-owner", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 100}
	ledger.Stakes[stakeKey("wallet-delegator", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 1000}
	setValidatorRewardWallet(&ledger, "VAL1", "wallet-owner")

	got, ok := validatorRewardWallet(&ledger, "VAL1")
	if !ok {
		t.Fatalf("expected reward wallet for validator")
	}
	if got != "wallet-owner" {
		t.Fatalf("bound wallet not preferred: got=%q want=%q", got, "wallet-owner")
	}
}

func TestRefreshValidatorRewardWalletBindingRebindsOnStakeChange(t *testing.T) {
	setGenesisRewardWallets(nil)
	ledger := NewLedger()

	ledger.Stakes[stakeKey("wallet-owner", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 200}
	ledger.Stakes[stakeKey("wallet-delegator", "VAL1")] = StakeLock{ValidatorID: "VAL1", Amount: 300}
	setValidatorRewardWallet(&ledger, "VAL1", "wallet-owner")

	delete(ledger.Stakes, stakeKey("wallet-owner", "VAL1"))
	refreshValidatorRewardWalletBinding(&ledger, "VAL1")
	if got := ledger.ValidatorRewardWallets["VAL1"]; got != "wallet-delegator" {
		t.Fatalf("expected rebind to remaining staker: got=%q want=%q", got, "wallet-delegator")
	}

	delete(ledger.Stakes, stakeKey("wallet-delegator", "VAL1"))
	refreshValidatorRewardWalletBinding(&ledger, "VAL1")
	if _, ok := ledger.ValidatorRewardWallets["VAL1"]; ok {
		t.Fatalf("expected reward wallet binding removal when no stake remains")
	}
}

func TestValidatorRewardWalletGenesisFallback(t *testing.T) {
	setGenesisRewardWallets(map[string]string{"VAL2": "wallet-genesis"})
	defer setGenesisRewardWallets(nil)

	got, ok := validatorRewardWallet(nil, "VAL2")
	if !ok {
		t.Fatalf("expected genesis fallback reward wallet")
	}
	if got != "wallet-genesis" {
		t.Fatalf("unexpected genesis fallback wallet: got=%q want=%q", got, "wallet-genesis")
	}
}

func TestDistributeBlockRewardCreditsStakeWallet(t *testing.T) {
	setGenesisRewardWallets(nil)
	oldMinted := TotalMintedMSC
	oldWorkEnabled := WorkBlockRewardEnabled
	oldWorkBase := WorkBlockBaseReward
	TotalMintedMSC = 0
	WorkBlockRewardEnabled = false
	WorkBlockBaseReward = 0
	defer func() {
		TotalMintedMSC = oldMinted
		WorkBlockRewardEnabled = oldWorkEnabled
		WorkBlockBaseReward = oldWorkBase
	}()

	ledger := NewLedger()
	ledger.Stakes[stakeKey("wallet-f", "F")] = StakeLock{
		ValidatorID: "F",
		Amount:      1000,
	}

	block := Block{
		Transactions: []Transaction{
			{Fee: 10},
		},
	}
	reward := CalculateBlockReward(10)
	if reward <= 0 {
		t.Fatalf("expected positive reward, got=%d", reward)
	}

	DistributeBlockReward(&ledger, block, "F", []string{"F"}, OWNER_ADDRESS)

	wantWallet := int(reward*40/100 + reward*30/100)
	gotWallet := getBalance(ledger, CoinSymbol, "wallet-f")
	if gotWallet != wantWallet {
		t.Fatalf("wallet reward mismatch: got=%d want=%d", gotWallet, wantWallet)
	}
	gotValidatorID := getBalance(ledger, CoinSymbol, "F")
	if gotValidatorID != 0 {
		t.Fatalf("validator id should not receive reward when wallet stake exists: got=%d", gotValidatorID)
	}
}

func TestDistributeBlockRewardIgnoresGenesisRewardWalletFallback(t *testing.T) {
	setGenesisRewardWallets(map[string]string{"F": "wallet-f-genesis"})
	defer setGenesisRewardWallets(nil)
	if addr, ok := genesisRewardWallet("F"); !ok || addr != "wallet-f-genesis" {
		t.Fatalf("genesis map not set: ok=%v addr=%q", ok, addr)
	}

	oldMinted := TotalMintedMSC
	oldWorkEnabled := WorkBlockRewardEnabled
	oldWorkBase := WorkBlockBaseReward
	TotalMintedMSC = 0
	WorkBlockRewardEnabled = false
	WorkBlockBaseReward = 0
	defer func() {
		TotalMintedMSC = oldMinted
		WorkBlockRewardEnabled = oldWorkEnabled
		WorkBlockBaseReward = oldWorkBase
	}()

	ledger := NewLedger()
	if resolved, ok := validatorRewardWallet(&ledger, "F"); !ok || resolved != "wallet-f-genesis" {
		t.Fatalf("resolver did not use genesis fallback: ok=%v resolved=%q", ok, resolved)
	}

	block := Block{
		Transactions: []Transaction{
			{Fee: 10},
		},
	}
	reward := CalculateBlockReward(10)
	if reward <= 0 {
		t.Fatalf("expected positive reward, got=%d", reward)
	}

	DistributeBlockReward(&ledger, block, "F", []string{"F"}, OWNER_ADDRESS)

	wantWallet := int(reward*40/100 + reward*30/100)
	gotWallet := getBalance(ledger, CoinSymbol, "wallet-f-genesis")
	if gotWallet != 0 {
		t.Fatalf("consensus reward should ignore process-global genesis fallback wallet: got=%d", gotWallet)
	}
	if got := getBalance(ledger, CoinSymbol, "F"); got != wantWallet {
		t.Fatalf("validator id fallback reward mismatch: got=%d want=%d", got, wantWallet)
	}
}

func TestDistributeTimeBlockRewardCreditsStakeWallet(t *testing.T) {
	setGenesisRewardWallets(nil)
	oldMinted := TotalMintedMSC
	TotalMintedMSC = 0
	defer func() { TotalMintedMSC = oldMinted }()

	ledger := NewLedger()
	ledger.Stakes[stakeKey("wallet-f", "F")] = StakeLock{
		ValidatorID: "F",
		Amount:      1000,
	}

	DistributeTimeBlockReward(&ledger, "F", []string{"F"}, OWNER_ADDRESS)

	const reward int64 = 10
	wantWallet := int(reward*30/100 + reward*40/100)
	gotWallet := getBalance(ledger, CoinSymbol, "wallet-f")
	if gotWallet != wantWallet {
		t.Fatalf("wallet time-reward mismatch: got=%d want=%d", gotWallet, wantWallet)
	}
	gotValidatorID := getBalance(ledger, CoinSymbol, "F")
	if gotValidatorID != 0 {
		t.Fatalf("validator id should not receive time-reward when wallet stake exists: got=%d", gotValidatorID)
	}
}
