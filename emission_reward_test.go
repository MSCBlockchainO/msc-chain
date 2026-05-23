package main

import (
	"fmt"
	"testing"
)

func TestScheduledEmissionRewardDistributionSplit(t *testing.T) {
	oldEnabled := EmissionRewardEnabled
	oldMin := EmissionMinReward
	oldMax := EmissionMaxReward
	oldJackpot := EmissionJackpotChanceBPS
	oldBaseChance := EmissionBaseChanceBPS
	oldHighAfter := EmissionHighChanceAfterBlocks
	oldHighChance := EmissionHighChanceBPS
	oldHalving := EmissionHalvingIntervalBlocks
	oldTreasury := EmissionTreasuryBPS
	oldValidator := EmissionValidatorBPS
	oldBurn := EmissionBurnBPS
	oldIntervalMode := EmissionIntervalMode
	oldGapMin := EmissionGapMinBlocks
	oldGapMax := EmissionGapMaxBlocks
	oldValidatorToProposer := EmissionValidatorToProposer
	oldMinted := TotalMintedMSC
	defer func() {
		EmissionRewardEnabled = oldEnabled
		EmissionMinReward = oldMin
		EmissionMaxReward = oldMax
		EmissionJackpotChanceBPS = oldJackpot
		EmissionBaseChanceBPS = oldBaseChance
		EmissionHighChanceAfterBlocks = oldHighAfter
		EmissionHighChanceBPS = oldHighChance
		EmissionHalvingIntervalBlocks = oldHalving
		EmissionTreasuryBPS = oldTreasury
		EmissionValidatorBPS = oldValidator
		EmissionBurnBPS = oldBurn
		EmissionIntervalMode = oldIntervalMode
		EmissionGapMinBlocks = oldGapMin
		EmissionGapMaxBlocks = oldGapMax
		EmissionValidatorToProposer = oldValidatorToProposer
		resetEmissionIntervalSchedule()
		TotalMintedMSC = oldMinted
	}()

	EmissionRewardEnabled = true
	EmissionMinReward = 2
	EmissionMaxReward = 50
	EmissionJackpotChanceBPS = 10000
	EmissionBaseChanceBPS = 10000
	EmissionHighChanceAfterBlocks = 0
	EmissionHighChanceBPS = 10000
	EmissionHalvingIntervalBlocks = 1105840
	EmissionTreasuryBPS = 7000
	EmissionValidatorBPS = 2000
	EmissionBurnBPS = 1000
	EmissionIntervalMode = false
	EmissionValidatorToProposer = false
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
		ID:        1,
		BlockHash: "emission-1",
		Proposer:  "A",
	}
	distributeScheduledEmissionReward(&ledger, block, []string{"A", "B"})

	if got := getBalance(ledger, CoinSymbol, TREASURY_ADDRESS); got != 30 {
		t.Fatalf("treasury split mismatch: got=%d want=%d", got, 30)
	}
	if got := getBalance(ledger, CoinSymbol, "wallet-a"); got != 5 {
		t.Fatalf("validator A split mismatch: got=%d want=%d", got, 5)
	}
	if got := getBalance(ledger, CoinSymbol, "wallet-b"); got != 5 {
		t.Fatalf("validator B split mismatch: got=%d want=%d", got, 5)
	}
	if got := getBalance(ledger, CoinSymbol, USER_REWARD_POOL); got != 0 {
		t.Fatalf("unexpected user pool mint: got=%d", got)
	}
	if got := getBalance(ledger, CoinSymbol, TREASURY_ADDRESS) + getBalance(ledger, CoinSymbol, "wallet-a") + getBalance(ledger, CoinSymbol, "wallet-b"); got != 40 {
		t.Fatalf("total supply delta mismatch: got=%d want=%d", got, 40)
	}
}

func TestScheduledEmissionRewardIgnoresProcessGlobalMintedSupply(t *testing.T) {
	oldEnabled := EmissionRewardEnabled
	oldMin := EmissionMinReward
	oldMax := EmissionMaxReward
	oldJackpot := EmissionJackpotChanceBPS
	oldBaseChance := EmissionBaseChanceBPS
	oldHighAfter := EmissionHighChanceAfterBlocks
	oldHighChance := EmissionHighChanceBPS
	oldHalving := EmissionHalvingIntervalBlocks
	oldTreasury := EmissionTreasuryBPS
	oldValidator := EmissionValidatorBPS
	oldBurn := EmissionBurnBPS
	oldIntervalMode := EmissionIntervalMode
	oldValidatorToProposer := EmissionValidatorToProposer
	oldMinted := TotalMintedMSC
	defer func() {
		EmissionRewardEnabled = oldEnabled
		EmissionMinReward = oldMin
		EmissionMaxReward = oldMax
		EmissionJackpotChanceBPS = oldJackpot
		EmissionBaseChanceBPS = oldBaseChance
		EmissionHighChanceAfterBlocks = oldHighAfter
		EmissionHighChanceBPS = oldHighChance
		EmissionHalvingIntervalBlocks = oldHalving
		EmissionTreasuryBPS = oldTreasury
		EmissionValidatorBPS = oldValidator
		EmissionBurnBPS = oldBurn
		EmissionIntervalMode = oldIntervalMode
		EmissionValidatorToProposer = oldValidatorToProposer
		TotalMintedMSC = oldMinted
		resetEmissionIntervalSchedule()
	}()

	EmissionRewardEnabled = true
	EmissionMinReward = 4
	EmissionMaxReward = 4
	EmissionJackpotChanceBPS = 10000
	EmissionBaseChanceBPS = 10000
	EmissionHighChanceAfterBlocks = 0
	EmissionHighChanceBPS = 10000
	EmissionHalvingIntervalBlocks = 0
	EmissionTreasuryBPS = 2500
	EmissionValidatorBPS = 7500
	EmissionBurnBPS = 0
	EmissionIntervalMode = false
	EmissionValidatorToProposer = false

	makeLedger := func() Ledger {
		ledger := NewLedger()
		setValidatorRewardWallet(&ledger, "A", "wallet-a")
		setValidatorRewardWallet(&ledger, "B", "wallet-b")
		return ledger
	}
	block := Block{ID: 7, BlockHash: "emission-global-minted", Proposer: "A"}

	ledgerA := makeLedger()
	TotalMintedMSC = 0
	distributeScheduledEmissionReward(&ledgerA, block, []string{"A", "B"})

	ledgerB := makeLedger()
	TotalMintedMSC = FixedTotalSupply
	distributeScheduledEmissionReward(&ledgerB, block, []string{"A", "B"})

	if got, want := HashLedger(ledgerB), HashLedger(ledgerA); got != want {
		t.Fatalf("emission depended on process-global minted supply: got=%s want=%s", got, want)
	}
}

func TestScheduledEmissionRewardIgnoresProcessGlobalGenesisWallets(t *testing.T) {
	oldEnabled := EmissionRewardEnabled
	oldMin := EmissionMinReward
	oldMax := EmissionMaxReward
	oldJackpot := EmissionJackpotChanceBPS
	oldBaseChance := EmissionBaseChanceBPS
	oldHighAfter := EmissionHighChanceAfterBlocks
	oldHighChance := EmissionHighChanceBPS
	oldHalving := EmissionHalvingIntervalBlocks
	oldTreasury := EmissionTreasuryBPS
	oldValidator := EmissionValidatorBPS
	oldBurn := EmissionBurnBPS
	oldIntervalMode := EmissionIntervalMode
	oldValidatorToProposer := EmissionValidatorToProposer
	oldMinted := TotalMintedMSC
	genesisRewardWalletsMu.RLock()
	oldGenesisWallets := make(map[string]string, len(genesisRewardWallets))
	for id, wallet := range genesisRewardWallets {
		oldGenesisWallets[id] = wallet
	}
	genesisRewardWalletsMu.RUnlock()
	defer func() {
		EmissionRewardEnabled = oldEnabled
		EmissionMinReward = oldMin
		EmissionMaxReward = oldMax
		EmissionJackpotChanceBPS = oldJackpot
		EmissionBaseChanceBPS = oldBaseChance
		EmissionHighChanceAfterBlocks = oldHighAfter
		EmissionHighChanceBPS = oldHighChance
		EmissionHalvingIntervalBlocks = oldHalving
		EmissionTreasuryBPS = oldTreasury
		EmissionValidatorBPS = oldValidator
		EmissionBurnBPS = oldBurn
		EmissionIntervalMode = oldIntervalMode
		EmissionValidatorToProposer = oldValidatorToProposer
		TotalMintedMSC = oldMinted
		setGenesisRewardWallets(oldGenesisWallets)
		resetEmissionIntervalSchedule()
	}()

	EmissionRewardEnabled = true
	EmissionMinReward = 4
	EmissionMaxReward = 4
	EmissionJackpotChanceBPS = 10000
	EmissionBaseChanceBPS = 10000
	EmissionHighChanceAfterBlocks = 0
	EmissionHighChanceBPS = 10000
	EmissionHalvingIntervalBlocks = 0
	EmissionTreasuryBPS = 2500
	EmissionValidatorBPS = 7500
	EmissionBurnBPS = 0
	EmissionIntervalMode = false
	EmissionValidatorToProposer = true
	TotalMintedMSC = 0

	block := Block{ID: 9, BlockHash: "emission-genesis-wallet", Proposer: "A"}
	ledgerA := NewLedger()
	setGenesisRewardWallets(map[string]string{"A": "wallet-a-local"})
	distributeScheduledEmissionReward(&ledgerA, block, []string{"A", "B"})

	ledgerB := NewLedger()
	setGenesisRewardWallets(map[string]string{"A": "wallet-a-other"})
	distributeScheduledEmissionReward(&ledgerB, block, []string{"A", "B"})

	if got, want := HashLedger(ledgerB), HashLedger(ledgerA); got != want {
		t.Fatalf("emission depended on process-global genesis reward wallets: got=%s want=%s", got, want)
	}
	if got := getBalance(ledgerA, CoinSymbol, "A"); got == 0 {
		t.Fatalf("expected consensus reward to fall back to validator ID")
	}
}

func TestScheduledEmissionRewardAmountHalving(t *testing.T) {
	oldEnabled := EmissionRewardEnabled
	oldMin := EmissionMinReward
	oldMax := EmissionMaxReward
	oldJackpot := EmissionJackpotChanceBPS
	oldHalving := EmissionHalvingIntervalBlocks
	defer func() {
		EmissionRewardEnabled = oldEnabled
		EmissionMinReward = oldMin
		EmissionMaxReward = oldMax
		EmissionJackpotChanceBPS = oldJackpot
		EmissionHalvingIntervalBlocks = oldHalving
	}()

	EmissionRewardEnabled = true
	EmissionMinReward = 50
	EmissionMaxReward = 50
	EmissionJackpotChanceBPS = 10000
	EmissionHalvingIntervalBlocks = 10

	a := scheduledEmissionRewardAmount(Block{ID: 1, BlockHash: "h1"})
	b := scheduledEmissionRewardAmount(Block{ID: 11, BlockHash: "h11"})
	c := scheduledEmissionRewardAmount(Block{ID: 21, BlockHash: "h21"})

	if a != 50 {
		t.Fatalf("expected 50 at era0, got %d", a)
	}
	if b != 25 {
		t.Fatalf("expected 25 at era1, got %d", b)
	}
	if c != 12 {
		t.Fatalf("expected 12 at era2, got %d", c)
	}
}

func TestShouldMintScheduledEmissionOnBlockChanceBounds(t *testing.T) {
	oldEnabled := EmissionRewardEnabled
	oldBaseChance := EmissionBaseChanceBPS
	oldHighAfter := EmissionHighChanceAfterBlocks
	oldHighChance := EmissionHighChanceBPS
	oldIntervalMode := EmissionIntervalMode
	oldGapMin := EmissionGapMinBlocks
	oldGapMax := EmissionGapMaxBlocks
	defer func() {
		EmissionRewardEnabled = oldEnabled
		EmissionBaseChanceBPS = oldBaseChance
		EmissionHighChanceAfterBlocks = oldHighAfter
		EmissionHighChanceBPS = oldHighChance
		EmissionIntervalMode = oldIntervalMode
		EmissionGapMinBlocks = oldGapMin
		EmissionGapMaxBlocks = oldGapMax
		resetEmissionIntervalSchedule()
	}()

	block := Block{ID: 100, BlockHash: "chance-100"}

	EmissionRewardEnabled = true
	EmissionIntervalMode = false
	EmissionBaseChanceBPS = 0
	EmissionHighChanceAfterBlocks = 0
	EmissionHighChanceBPS = 0
	if shouldMintScheduledEmissionOnBlock(block) {
		t.Fatalf("expected false at 0 chance")
	}

	EmissionBaseChanceBPS = 10000
	if !shouldMintScheduledEmissionOnBlock(block) {
		t.Fatalf("expected true at 100%% chance")
	}
}

func TestScheduledEmissionSmallRewardEnsuresTreasuryShare(t *testing.T) {
	oldEnabled := EmissionRewardEnabled
	oldMin := EmissionMinReward
	oldMax := EmissionMaxReward
	oldJackpot := EmissionJackpotChanceBPS
	oldHalving := EmissionHalvingIntervalBlocks
	oldTreasury := EmissionTreasuryBPS
	oldValidator := EmissionValidatorBPS
	oldBurn := EmissionBurnBPS
	oldBaseChance := EmissionBaseChanceBPS
	oldHighAfter := EmissionHighChanceAfterBlocks
	oldHighChance := EmissionHighChanceBPS
	oldIntervalMode := EmissionIntervalMode
	oldGapMin := EmissionGapMinBlocks
	oldGapMax := EmissionGapMaxBlocks
	defer func() {
		EmissionRewardEnabled = oldEnabled
		EmissionMinReward = oldMin
		EmissionMaxReward = oldMax
		EmissionJackpotChanceBPS = oldJackpot
		EmissionHalvingIntervalBlocks = oldHalving
		EmissionTreasuryBPS = oldTreasury
		EmissionValidatorBPS = oldValidator
		EmissionBurnBPS = oldBurn
		EmissionBaseChanceBPS = oldBaseChance
		EmissionHighChanceAfterBlocks = oldHighAfter
		EmissionHighChanceBPS = oldHighChance
		EmissionIntervalMode = oldIntervalMode
		EmissionGapMinBlocks = oldGapMin
		EmissionGapMaxBlocks = oldGapMax
		resetEmissionIntervalSchedule()
	}()

	EmissionRewardEnabled = true
	EmissionMinReward = 2
	EmissionMaxReward = 2
	EmissionJackpotChanceBPS = 0
	EmissionHalvingIntervalBlocks = 0
	EmissionTreasuryBPS = 2000
	EmissionValidatorBPS = 7200
	EmissionBurnBPS = 800
	EmissionBaseChanceBPS = 10000
	EmissionHighChanceAfterBlocks = 0
	EmissionHighChanceBPS = 10000
	EmissionIntervalMode = false

	ledger := Ledger{
		Balances:               map[string]int{},
		Nonces:                 map[string]int{},
		Stakes:                 map[string]StakeLock{},
		ValidatorRewardWallets: map[string]string{},
	}
	setValidatorRewardWallet(&ledger, "A", "wallet-a")

	block := Block{ID: 589, BlockHash: "emission-small-589", Proposer: "A"}
	distributeScheduledEmissionReward(&ledger, block, []string{"A"})

	if got := getBalance(ledger, CoinSymbol, TREASURY_ADDRESS); got != 1 {
		t.Fatalf("expected treasury to receive minimum share 1, got %d", got)
	}
	if got := getBalance(ledger, CoinSymbol, "wallet-a"); got != 1 {
		t.Fatalf("expected validator to receive 1, got %d", got)
	}
}

func TestScheduledEmissionRewardAmountVariesWithinRange(t *testing.T) {
	oldMin := EmissionMinReward
	oldMax := EmissionMaxReward
	oldJackpot := EmissionJackpotChanceBPS
	oldHalving := EmissionHalvingIntervalBlocks
	defer func() {
		EmissionMinReward = oldMin
		EmissionMaxReward = oldMax
		EmissionJackpotChanceBPS = oldJackpot
		EmissionHalvingIntervalBlocks = oldHalving
	}()

	EmissionMinReward = 2
	EmissionMaxReward = 6
	EmissionJackpotChanceBPS = 0
	EmissionHalvingIntervalBlocks = 0

	seen := make(map[int64]struct{})
	for i := uint64(1); i <= 32; i++ {
		amount := scheduledEmissionRewardAmount(Block{
			ID:        i,
			BlockHash: fmt.Sprintf("emission-var-%d", i),
		})
		if amount < EmissionMinReward || amount > EmissionMaxReward {
			t.Fatalf("amount out of range: got=%d range=[%d,%d]", amount, EmissionMinReward, EmissionMaxReward)
		}
		seen[amount] = struct{}{}
	}

	if len(seen) < 2 {
		t.Fatalf("expected variable emission amounts, got only %d distinct value(s)", len(seen))
	}
}
