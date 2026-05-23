package main

import "testing"

type dtlGameFiConfigSnapshot struct {
	deFiFarmEnabled          bool
	gameFiSeasonEnabled      bool
	gameFiRewardToken        string
	seasonLengthBlocks       uint64
	claimGraceBlocks         uint64
	duelWinPoints            uint64
	tournamentWinPoints      uint64
	tournamentPartPoints     uint64
	farmMinStakeBlocks       uint64
	farmLPPointsPerBlock     uint64
	farmMaxMultiplierBPS     uint16
	gameFiMaxRewardPerSeason uint64
}

func snapshotDTLGameFiConfig() dtlGameFiConfigSnapshot {
	return dtlGameFiConfigSnapshot{
		deFiFarmEnabled:          ConfigDTLDeFiFarmEnabled,
		gameFiSeasonEnabled:      ConfigDTLGameFiSeasonEnabled,
		gameFiRewardToken:        ConfigDTLGameFiRewardToken,
		seasonLengthBlocks:       ConfigDTLGameFiSeasonLengthBlocks,
		claimGraceBlocks:         ConfigDTLGameFiClaimGraceBlocks,
		duelWinPoints:            ConfigDTLGameFiDuelWinPoints,
		tournamentWinPoints:      ConfigDTLGameFiTournamentWinPoints,
		tournamentPartPoints:     ConfigDTLGameFiTournamentPartPoints,
		farmMinStakeBlocks:       ConfigDTLFarmMinStakeBlocks,
		farmLPPointsPerBlock:     ConfigDTLFarmLPPointsPerBlock,
		farmMaxMultiplierBPS:     ConfigDTLFarmMaxMultiplierBPS,
		gameFiMaxRewardPerSeason: ConfigDTLGameFiMaxRewardPerSeason,
	}
}

func (s dtlGameFiConfigSnapshot) restore() {
	ConfigDTLDeFiFarmEnabled = s.deFiFarmEnabled
	ConfigDTLGameFiSeasonEnabled = s.gameFiSeasonEnabled
	ConfigDTLGameFiRewardToken = s.gameFiRewardToken
	ConfigDTLGameFiSeasonLengthBlocks = s.seasonLengthBlocks
	ConfigDTLGameFiClaimGraceBlocks = s.claimGraceBlocks
	ConfigDTLGameFiDuelWinPoints = s.duelWinPoints
	ConfigDTLGameFiTournamentWinPoints = s.tournamentWinPoints
	ConfigDTLGameFiTournamentPartPoints = s.tournamentPartPoints
	ConfigDTLFarmMinStakeBlocks = s.farmMinStakeBlocks
	ConfigDTLFarmLPPointsPerBlock = s.farmLPPointsPerBlock
	ConfigDTLFarmMaxMultiplierBPS = s.farmMaxMultiplierBPS
	ConfigDTLGameFiMaxRewardPerSeason = s.gameFiMaxRewardPerSeason
}

func TestParseDTLTxTypeIncludesGameFiAndFarm(t *testing.T) {
	cases := []DTLTxType{
		DTLTxFarmCreate,
		DTLTxFarmStakeLP,
		DTLTxFarmUnstakeLP,
		DTLTxFarmClaim,
		DTLTxSeasonCreate,
		DTLTxSeasonFinalize,
		DTLTxSeasonClaim,
	}
	for _, tt := range cases {
		got, err := parseDTLTxType(string(tt))
		if err != nil {
			t.Fatalf("parseDTLTxType(%s) returned error: %v", tt, err)
		}
		if got != tt {
			t.Fatalf("parseDTLTxType(%s) = %s, want %s", tt, got, tt)
		}
	}
}

func TestDTLFarmSeasonLifecycle(t *testing.T) {
	snap := snapshotDTLGameFiConfig()
	defer snap.restore()

	ConfigDTLDeFiFarmEnabled = true
	ConfigDTLGameFiSeasonEnabled = true
	ConfigDTLGameFiRewardToken = CoinSymbol
	ConfigDTLGameFiSeasonLengthBlocks = 10
	ConfigDTLGameFiClaimGraceBlocks = 10
	ConfigDTLFarmMinStakeBlocks = 0
	ConfigDTLFarmLPPointsPerBlock = 2
	ConfigDTLFarmMaxMultiplierBPS = 30000
	ConfigDTLGameFiMaxRewardPerSeason = 1_000_000

	ledger := NewLedger()
	ensureDTLState(&ledger)

	rewardTokenID := normalizeDTLTokenID(CoinSymbol)
	auxTokenID := normalizeDTLTokenID("USD")
	ledger.DTL.Tokens[rewardTokenID] = &DTLTokenState{
		TokenID:            rewardTokenID,
		Name:               "Native Coin",
		Symbol:             CoinSymbol,
		Decimals:           CoinDecimals,
		MaxSupply:          ^uint64(0),
		TotalSupply:        10_000_000,
		FreezeEnabled:      true,
		TaxBPS:             0,
		AuthoritySigners:   []string{"alice"},
		AuthorityThreshold: 1,
	}
	ledger.DTL.SymbolIndex[normalizeDTLSymbol(CoinSymbol)] = rewardTokenID
	ledger.DTL.Tokens[auxTokenID] = &DTLTokenState{
		TokenID:            auxTokenID,
		Name:               "USD",
		Symbol:             "USD",
		Decimals:           6,
		MaxSupply:          ^uint64(0),
		TotalSupply:        10_000_000,
		FreezeEnabled:      true,
		TaxBPS:             0,
		AuthoritySigners:   []string{"alice"},
		AuthorityThreshold: 1,
	}
	ledger.DTL.SymbolIndex[normalizeDTLSymbol("USD")] = auxTokenID

	poolID := normalizeDTLPoolID("farm_pool_1")
	ledger.DTL.Pools[poolID] = &DTLPoolState{
		PoolID:             poolID,
		TokenA:             rewardTokenID,
		TokenB:             auxTokenID,
		ReserveA:           1_000_000,
		ReserveB:           1_000_000,
		TotalLPShares:      1_000,
		FeeBPS:             30,
		ProtocolFeeBPS:     5,
		ProtocolFeeAccount: DTLTreasuryAccount,
	}
	ledger.DTL.LPBalances[dtlLPBalanceKey(poolID, "alice")] = 200
	ledger.DTL.Balances[dtlBalanceKey(rewardTokenID, DTLTreasuryAccount)] = 5_000

	seasonCreateTx := Transaction{
		From:      "alice",
		Type:      TxDTL,
		DTLTxType: string(DTLTxSeasonCreate),
		DTLPayload: `{
			"creator":"alice",
			"season_id":"season1",
			"start_height":100
		}`,
		ChainID: ChainID,
	}
	if err := validateDTLTransaction(&ledger, seasonCreateTx, 100); err != nil {
		t.Fatalf("validate season create failed: %v", err)
	}
	if err := applyDTLTransaction(&ledger, seasonCreateTx, 100); err != nil {
		t.Fatalf("apply season create failed: %v", err)
	}

	farmCreateTx := Transaction{
		From:      "alice",
		Type:      TxDTL,
		DTLTxType: string(DTLTxFarmCreate),
		DTLPayload: `{
			"creator":"alice",
			"farm_id":"farm1",
			"pool_id":"` + poolID + `",
			"multiplier_bps":20000
		}`,
		ChainID: ChainID,
	}
	if err := validateDTLTransaction(&ledger, farmCreateTx, 100); err != nil {
		t.Fatalf("validate farm create failed: %v", err)
	}
	if err := applyDTLTransaction(&ledger, farmCreateTx, 100); err != nil {
		t.Fatalf("apply farm create failed: %v", err)
	}

	stakeTx := Transaction{
		From:      "alice",
		Type:      TxDTL,
		DTLTxType: string(DTLTxFarmStakeLP),
		DTLPayload: `{
			"account":"alice",
			"farm_id":"farm1",
			"amount":100
		}`,
		ChainID: ChainID,
	}
	if err := validateDTLTransaction(&ledger, stakeTx, 101); err != nil {
		t.Fatalf("validate farm stake failed: %v", err)
	}
	if err := applyDTLTransaction(&ledger, stakeTx, 101); err != nil {
		t.Fatalf("apply farm stake failed: %v", err)
	}

	claimFarmTx := Transaction{
		From:      "alice",
		Type:      TxDTL,
		DTLTxType: string(DTLTxFarmClaim),
		DTLPayload: `{
			"account":"alice",
			"farm_id":"farm1"
		}`,
		ChainID: ChainID,
	}
	if err := validateDTLTransaction(&ledger, claimFarmTx, 105); err != nil {
		t.Fatalf("validate farm claim failed: %v", err)
	}
	if err := applyDTLTransaction(&ledger, claimFarmTx, 105); err != nil {
		t.Fatalf("apply farm claim failed: %v", err)
	}

	scoreKey := dtlSeasonAccountKey("season1", "alice")
	if got := ledger.DTL.SeasonScores[scoreKey]; got == 0 {
		t.Fatalf("expected non-zero season score after farm claim")
	}

	ledger.DTL.SeasonVaults["season1"] = 1_000

	finalizeSeasonTx := Transaction{
		From:      "alice",
		Type:      TxDTL,
		DTLTxType: string(DTLTxSeasonFinalize),
		DTLPayload: `{
			"caller":"alice",
			"season_id":"season1"
		}`,
		ChainID: ChainID,
	}
	if err := validateDTLTransaction(&ledger, finalizeSeasonTx, 111); err != nil {
		t.Fatalf("validate season finalize failed: %v", err)
	}
	if err := applyDTLTransaction(&ledger, finalizeSeasonTx, 111); err != nil {
		t.Fatalf("apply season finalize failed: %v", err)
	}

	claimSeasonTx := Transaction{
		From:      "alice",
		Type:      TxDTL,
		DTLTxType: string(DTLTxSeasonClaim),
		DTLPayload: `{
			"account":"alice",
			"season_id":"season1"
		}`,
		ChainID: ChainID,
	}
	if err := validateDTLTransaction(&ledger, claimSeasonTx, 112); err != nil {
		t.Fatalf("validate season claim failed: %v", err)
	}
	if err := applyDTLTransaction(&ledger, claimSeasonTx, 112); err != nil {
		t.Fatalf("apply season claim failed: %v", err)
	}

	if !ledger.DTL.SeasonClaims[scoreKey] {
		t.Fatalf("expected season claim marker for alice")
	}
	if got := ledger.DTL.SeasonVaults["season1"]; got != 0 {
		t.Fatalf("expected season vault drained, got=%d", got)
	}
	if got := ledger.DTL.BalanceOf(rewardTokenID, "alice"); got == 0 {
		t.Fatalf("expected alice to receive season reward")
	}
	if err := validateDTLTransaction(&ledger, claimSeasonTx, 113); err == nil {
		t.Fatalf("expected duplicate season claim validation failure")
	}
}
