package main

import "testing"

func TestDistributeBlockRewardAddsWorkBlockBaseReward(t *testing.T) {
	oldEnabled := WorkBlockRewardEnabled
	oldBase := WorkBlockBaseReward
	oldRandom := RandomUserRewardEnabled
	oldChance := RandomUserRewardChanceBPS
	oldMinted := TotalMintedMSC
	defer func() {
		WorkBlockRewardEnabled = oldEnabled
		WorkBlockBaseReward = oldBase
		RandomUserRewardEnabled = oldRandom
		RandomUserRewardChanceBPS = oldChance
		TotalMintedMSC = oldMinted
	}()

	WorkBlockRewardEnabled = true
	WorkBlockBaseReward = 2
	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 0
	TotalMintedMSC = 0

	ledger := Ledger{
		Balances:               map[string]int{},
		Nonces:                 map[string]int{},
		Stakes:                 map[string]StakeLock{},
		ValidatorRewardWallets: map[string]string{},
	}
	setValidatorRewardWallet(&ledger, "A", "wallet-a")
	setValidatorRewardWallet(&ledger, "B", "wallet-b")

	block := Block{
		ID:       1,
		Type:     BlockTypeWork,
		Proposer: "A",
	}

	DistributeBlockReward(&ledger, block, "A", []string{"A", "B"}, OWNER_ADDRESS)

	if got := getBalance(ledger, CoinSymbol, "wallet-a"); got != 2 {
		t.Fatalf("work base reward expected proposer wallet=2, got=%d", got)
	}
	if got := getBalance(ledger, CoinSymbol, "wallet-b"); got != 0 {
		t.Fatalf("unexpected validator mint at low reward, got=%d", got)
	}
}

func TestDistributeBlockRewardSkipsWorkBlockBaseRewardWhenDisabled(t *testing.T) {
	oldEnabled := WorkBlockRewardEnabled
	oldBase := WorkBlockBaseReward
	oldRandom := RandomUserRewardEnabled
	oldChance := RandomUserRewardChanceBPS
	oldMinted := TotalMintedMSC
	defer func() {
		WorkBlockRewardEnabled = oldEnabled
		WorkBlockBaseReward = oldBase
		RandomUserRewardEnabled = oldRandom
		RandomUserRewardChanceBPS = oldChance
		TotalMintedMSC = oldMinted
	}()

	WorkBlockRewardEnabled = false
	WorkBlockBaseReward = 2
	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 0
	TotalMintedMSC = 0

	ledger := Ledger{
		Balances:               map[string]int{},
		Nonces:                 map[string]int{},
		Stakes:                 map[string]StakeLock{},
		ValidatorRewardWallets: map[string]string{},
	}
	setValidatorRewardWallet(&ledger, "A", "wallet-a")

	block := Block{
		ID:       2,
		Type:     BlockTypeWork,
		Proposer: "A",
	}

	DistributeBlockReward(&ledger, block, "A", []string{"A"}, OWNER_ADDRESS)

	if got := getBalance(ledger, CoinSymbol, "wallet-a"); got != 0 {
		t.Fatalf("expected no work base reward when disabled, got=%d", got)
	}
}
