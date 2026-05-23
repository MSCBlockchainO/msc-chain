package main

import "testing"

func TestDTLOraclePriceSubmitHandlesInitialSampleMap(t *testing.T) {
	state := NewDTLState()
	feedID, err := ApplyDTLOracleFeedCreateTx(state, ChainID, 1, DTLOracleFeedCreateTx{
		Creator:      "alice",
		BaseTokenID:  "MSC",
		QuoteTokenID: "USD",
		Signers:      []string{"alice", "bob", "carol"},
		Threshold:    2,
		Decimals:     8,
	})
	if err != nil {
		t.Fatalf("oracle feed create failed: %v", err)
	}

	// This used to panic when feed sample map was nil.
	if err := ValidateDTLOraclePriceSubmitTx(state, DTLOraclePriceSubmitTx{
		Submitter: "alice",
		FeedID:    feedID,
		Price:     100,
	}, 10); err != nil {
		t.Fatalf("oracle price validate failed: %v", err)
	}

	if err := ApplyDTLOraclePriceSubmitTx(state, 10, DTLOraclePriceSubmitTx{
		Submitter: "alice",
		FeedID:    feedID,
		Price:     100,
	}); err != nil {
		t.Fatalf("oracle price submit alice failed: %v", err)
	}
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		t.Fatalf("missing oracle feed state")
	}
	if feed.LastMedianPrice != 0 {
		t.Fatalf("median should not update before threshold, got %d", feed.LastMedianPrice)
	}
	if err := ApplyDTLOraclePriceSubmitTx(state, 11, DTLOraclePriceSubmitTx{
		Submitter: "bob",
		FeedID:    feedID,
		Price:     200,
	}); err != nil {
		t.Fatalf("oracle price submit bob failed: %v", err)
	}
	if feed.LastMedianPrice != 150 {
		t.Fatalf("median mismatch: got=%d want=150", feed.LastMedianPrice)
	}
}

func TestDTLPoolSwapProtocolFeeSplit(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	tokenA, err := ApplyDTLCreateTx(state, ChainID, 301, DTLCreateTx{
		Creator:            "alice",
		Name:               "Token A",
		Symbol:             "TKA",
		Decimals:           6,
		MaxSupply:          1_000_000,
		InitialSupply:      10_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create tokenA failed: %v", err)
	}
	tokenB, err := ApplyDTLCreateTx(state, ChainID, 302, DTLCreateTx{
		Creator:            "alice",
		Name:               "Token B",
		Symbol:             "TKB",
		Decimals:           6,
		MaxSupply:          1_000_000,
		InitialSupply:      10_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create tokenB failed: %v", err)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenA,
		Amount:  500,
	}); err != nil {
		t.Fatalf("fund bob failed: %v", err)
	}

	poolID, err := ApplyDTLPoolCreateTx(state, ChainID, 303, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenB,
		AmountA: 1_000,
		AmountB: 1_000,
		FeeBPS:  30,
	})
	if err != nil {
		t.Fatalf("pool create failed: %v", err)
	}
	pool := state.Pools[poolID]
	if pool == nil {
		t.Fatalf("missing pool state")
	}
	pool.ProtocolFeeBPS = 5000
	pool.ProtocolFeeAccount = DTLTreasuryAccount

	beforeTreasury := state.BalanceOf(tokenA, DTLTreasuryAccount)
	beforeReserveIn := pool.ReserveA
	tokenInIsA := normalizeDTLTokenID(tokenA) == normalizeDTLTokenID(pool.TokenA)
	if !tokenInIsA {
		beforeReserveIn = pool.ReserveB
	}
	amountIn := uint64(100)

	if _, err := ApplyDTLPoolSwapTx(state, DTLPoolSwapTx{
		Trader:   "bob",
		PoolID:   poolID,
		TokenIn:  tokenA,
		AmountIn: amountIn,
	}); err != nil {
		t.Fatalf("pool swap failed: %v", err)
	}

	expectedCut, err := dtlPoolProtocolFeeCut(amountIn, pool.FeeBPS, pool.ProtocolFeeBPS)
	if err != nil {
		t.Fatalf("protocol fee cut compute failed: %v", err)
	}
	afterTreasury := state.BalanceOf(tokenA, DTLTreasuryAccount)
	if got := afterTreasury - beforeTreasury; got != expectedCut {
		t.Fatalf("treasury protocol fee mismatch: got=%d want=%d", got, expectedCut)
	}
	afterReserveIn := pool.ReserveA
	if !tokenInIsA {
		afterReserveIn = pool.ReserveB
	}
	if got := afterReserveIn - beforeReserveIn; got != amountIn-expectedCut {
		t.Fatalf("reserve increase mismatch: got=%d want=%d", got, amountIn-expectedCut)
	}
}
