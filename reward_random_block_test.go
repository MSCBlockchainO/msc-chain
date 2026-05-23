package main

import "testing"

func TestShouldMintUserRewardOnBlockRespectsConfig(t *testing.T) {
	oldEnabled := RandomUserRewardEnabled
	oldChance := RandomUserRewardChanceBPS
	defer func() {
		RandomUserRewardEnabled = oldEnabled
		RandomUserRewardChanceBPS = oldChance
	}()

	block := Block{
		ID:        42,
		BlockHash: "abc123",
		PrevHash:  "prev123",
		Proposer:  "A",
	}

	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 0
	if shouldMintUserRewardOnBlock(block) {
		t.Fatalf("expected false when chance is 0")
	}

	RandomUserRewardChanceBPS = 10000
	if !shouldMintUserRewardOnBlock(block) {
		t.Fatalf("expected true when chance is 10000")
	}

	RandomUserRewardEnabled = false
	RandomUserRewardChanceBPS = 0
	if !shouldMintUserRewardOnBlock(block) {
		t.Fatalf("expected true when random mode disabled")
	}

	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 2500
	first := shouldMintUserRewardOnBlock(block)
	second := shouldMintUserRewardOnBlock(block)
	if first != second {
		t.Fatalf("expected deterministic gate result, got %v then %v", first, second)
	}
}

func TestDistributeBlockRewardUserWalletRandomGate(t *testing.T) {
	oldEnabled := RandomUserRewardEnabled
	oldChance := RandomUserRewardChanceBPS
	oldMinted := TotalMintedMSC
	defer func() {
		RandomUserRewardEnabled = oldEnabled
		RandomUserRewardChanceBPS = oldChance
		TotalMintedMSC = oldMinted
	}()

	block := Block{
		ID:        99,
		BlockHash: "reward-gate-99",
		Proposer:  "P",
		Transactions: []Transaction{
			{Fee: 1000},
		},
	}
	validators := []string{"A", "B", "C"}

	ledgerNo := Ledger{
		Balances:               map[string]int{},
		Nonces:                 map[string]int{},
		Stakes:                 map[string]StakeLock{},
		ValidatorRewardWallets: map[string]string{},
	}
	setValidatorRewardWallet(&ledgerNo, "A", "wallet-a")
	setValidatorRewardWallet(&ledgerNo, "B", "wallet-b")
	setValidatorRewardWallet(&ledgerNo, "C", "wallet-c")
	setValidatorRewardWallet(&ledgerNo, "P", "wallet-p")
	TotalMintedMSC = 0
	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 0
	DistributeBlockReward(&ledgerNo, block, "P", validators, OWNER_ADDRESS)
	if got := getBalance(ledgerNo, CoinSymbol, "wallet-a") + getBalance(ledgerNo, CoinSymbol, "wallet-b") + getBalance(ledgerNo, CoinSymbol, "wallet-c"); got <= 0 {
		t.Fatalf("expected validator rewards to be minted, got %d", got)
	}
	if got := getBalance(ledgerNo, CoinSymbol, USER_REWARD_POOL); got != 0 {
		t.Fatalf("expected no pool user reward mint, got %d", got)
	}
	baseA := getBalance(ledgerNo, CoinSymbol, "wallet-a")
	baseB := getBalance(ledgerNo, CoinSymbol, "wallet-b")
	baseC := getBalance(ledgerNo, CoinSymbol, "wallet-c")

	ledgerYes := Ledger{
		Balances:               map[string]int{},
		Nonces:                 map[string]int{},
		Stakes:                 map[string]StakeLock{},
		ValidatorRewardWallets: map[string]string{},
	}
	setValidatorRewardWallet(&ledgerYes, "A", "wallet-a")
	setValidatorRewardWallet(&ledgerYes, "B", "wallet-b")
	setValidatorRewardWallet(&ledgerYes, "C", "wallet-c")
	setValidatorRewardWallet(&ledgerYes, "P", "wallet-p")
	TotalMintedMSC = 0
	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 10000
	DistributeBlockReward(&ledgerYes, block, "P", validators, OWNER_ADDRESS)

	winner := deterministicUserRewardValidator(block, validators)
	if winner == "" {
		t.Fatalf("expected deterministic winner")
	}
	winnerWallet, ok := validatorRewardWallet(&ledgerYes, winner)
	if !ok || winnerWallet == "" {
		t.Fatalf("expected winner wallet resolution for %s", winner)
	}
	walletBalances := map[string]int{
		"wallet-a": getBalance(ledgerYes, CoinSymbol, "wallet-a"),
		"wallet-b": getBalance(ledgerYes, CoinSymbol, "wallet-b"),
		"wallet-c": getBalance(ledgerYes, CoinSymbol, "wallet-c"),
	}
	baseBalances := map[string]int{
		"wallet-a": baseA,
		"wallet-b": baseB,
		"wallet-c": baseC,
	}
	if walletBalances[winnerWallet] <= baseBalances[winnerWallet] {
		t.Fatalf("expected winner wallet bonus: winner=%s got=%d base=%d", winnerWallet, walletBalances[winnerWallet], baseBalances[winnerWallet])
	}
	for wallet, base := range baseBalances {
		if wallet == winnerWallet {
			continue
		}
		if walletBalances[wallet] != base {
			t.Fatalf("non-winner wallet changed unexpectedly: wallet=%s got=%d base=%d", wallet, walletBalances[wallet], base)
		}
	}
	if got := getBalance(ledgerYes, CoinSymbol, USER_REWARD_POOL); got != 0 {
		t.Fatalf("expected no user reward mint to pool, got %d", got)
	}
}
