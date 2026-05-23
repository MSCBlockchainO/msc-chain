package main

import (
	"strings"
	"testing"
)

func TestDTLCreateAndTransferWithTax(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 3)
	create := DTLCreateTx{
		Creator:            "alice",
		Name:               "Mythical System Coin Test",
		Symbol:             "MSCT",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      1_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 2,
		FreezeEnabled:      true,
		TaxBPS:             100,
	}

	tokenID, err := ApplyDTLCreateTx(state, "91938", 1, create)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if got := state.BalanceOf(tokenID, "alice"); got != 1000 {
		t.Fatalf("unexpected creator balance: got=%d want=1000", got)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  100,
	}); err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	if got := state.BalanceOf(tokenID, "alice"); got != 900 {
		t.Fatalf("unexpected alice balance: got=%d want=900", got)
	}
	if got := state.BalanceOf(tokenID, "bob"); got != 99 {
		t.Fatalf("unexpected bob balance: got=%d want=99", got)
	}
	if got := state.BalanceOf(tokenID, DTLTreasuryAccount); got != 1 {
		t.Fatalf("unexpected treasury balance: got=%d want=1", got)
	}
}

func TestDTLApproveAndTransferFrom(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	tokenID, err := ApplyDTLCreateTx(state, "91938", 1, DTLCreateTx{
		Creator:            "alice",
		Name:               "Approve Token",
		Symbol:             "APR",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      100,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
		FreezeEnabled:      true,
		TaxBPS:             0,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := ApplyDTLApproveTx(state, DTLApproveTx{
		Owner:   "alice",
		Spender: "charlie",
		TokenID: tokenID,
		Amount:  60,
	}); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if got := state.AllowanceOf(tokenID, "alice", "charlie"); got != 60 {
		t.Fatalf("unexpected allowance after approve: got=%d want=60", got)
	}

	if err := ApplyDTLTransferFromTx(state, DTLTransferFromTx{
		Spender: "charlie",
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  25,
	}); err != nil {
		t.Fatalf("transfer_from failed: %v", err)
	}
	if got := state.BalanceOf(tokenID, "alice"); got != 75 {
		t.Fatalf("unexpected owner balance after transfer_from: got=%d want=75", got)
	}
	if got := state.BalanceOf(tokenID, "bob"); got != 25 {
		t.Fatalf("unexpected recipient balance after transfer_from: got=%d want=25", got)
	}
	if got := state.AllowanceOf(tokenID, "alice", "charlie"); got != 35 {
		t.Fatalf("unexpected allowance after transfer_from: got=%d want=35", got)
	}

	if err := ValidateDTLTransferFromTx(state, DTLTransferFromTx{
		Spender: "charlie",
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  36,
	}); err == nil {
		t.Fatalf("expected insufficient allowance validation failure")
	}
}

func TestDTLNFT721CreateMintTransfer(t *testing.T) {
	state := NewDTLState()

	collectionID, err := ApplyDTLNFT721CreateTx(state, "91938", 1, DTLNFT721CreateTx{
		Creator: "alice",
		Name:    "Collectibles",
		Symbol:  "CL721",
		BaseURI: "ipfs://collection/",
	})
	if err != nil {
		t.Fatalf("nft721 create failed: %v", err)
	}

	tokenID, err := ApplyDTLNFT721MintTx(state, DTLNFT721MintTx{
		Creator:      "alice",
		CollectionID: collectionID,
		To:           "bob",
		TokenURI:     "ipfs://collection/1",
	})
	if err != nil {
		t.Fatalf("nft721 mint failed: %v", err)
	}
	if tokenID != 1 {
		t.Fatalf("unexpected nft721 token id: got=%d want=1", tokenID)
	}
	if got := state.NFT721OwnerOf(collectionID, tokenID); got != "bob" {
		t.Fatalf("unexpected nft721 owner after mint: got=%s want=bob", got)
	}

	if err := ApplyDTLNFT721TransferTx(state, DTLNFT721TransferTx{
		From:         "bob",
		To:           "carol",
		CollectionID: collectionID,
		TokenID:      tokenID,
	}); err != nil {
		t.Fatalf("nft721 transfer failed: %v", err)
	}
	if got := state.NFT721OwnerOf(collectionID, tokenID); got != "carol" {
		t.Fatalf("unexpected nft721 owner after transfer: got=%s want=carol", got)
	}

	if err := ValidateDTLNFT721TransferTx(state, DTLNFT721TransferTx{
		From:         "bob",
		To:           "dave",
		CollectionID: collectionID,
		TokenID:      tokenID,
	}); err == nil {
		t.Fatalf("expected nft721 transfer validation to fail for non-owner")
	}
}

func TestDTLNFT1155CreateMintTransfer(t *testing.T) {
	state := NewDTLState()

	collectionID, err := ApplyDTLNFT1155CreateTx(state, "91938", 1, DTLNFT1155CreateTx{
		Creator: "alice",
		Name:    "Game Items",
		Symbol:  "GI1155",
		BaseURI: "ipfs://items/{id}.json",
	})
	if err != nil {
		t.Fatalf("nft1155 create failed: %v", err)
	}

	if err := ApplyDTLNFT1155MintTx(state, DTLNFT1155MintTx{
		Creator:      "alice",
		CollectionID: collectionID,
		To:           "bob",
		TokenID:      7,
		Amount:       50,
	}); err != nil {
		t.Fatalf("nft1155 mint failed: %v", err)
	}
	if got := state.NFT1155BalanceOf(collectionID, 7, "bob"); got != 50 {
		t.Fatalf("unexpected nft1155 balance after mint: got=%d want=50", got)
	}

	if err := ApplyDTLNFT1155TransferTx(state, DTLNFT1155TransferTx{
		From:         "bob",
		To:           "carol",
		CollectionID: collectionID,
		TokenID:      7,
		Amount:       20,
	}); err != nil {
		t.Fatalf("nft1155 transfer failed: %v", err)
	}
	if got := state.NFT1155BalanceOf(collectionID, 7, "bob"); got != 30 {
		t.Fatalf("unexpected nft1155 sender balance after transfer: got=%d want=30", got)
	}
	if got := state.NFT1155BalanceOf(collectionID, 7, "carol"); got != 20 {
		t.Fatalf("unexpected nft1155 receiver balance after transfer: got=%d want=20", got)
	}

	if err := ValidateDTLNFT1155TransferTx(state, DTLNFT1155TransferTx{
		From:         "bob",
		To:           "carol",
		CollectionID: collectionID,
		TokenID:      7,
		Amount:       31,
	}); err == nil {
		t.Fatalf("expected nft1155 transfer validation to fail for insufficient balance")
	}
}

func TestDTLMintRequiresThreshold(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 3)
	tokenID, err := ApplyDTLCreateTx(state, "91938", 2, DTLCreateTx{
		Creator:            "alice",
		Name:               "Mythical Mint Test",
		Symbol:             "MMT",
		Decimals:           18,
		MaxSupply:          1_000,
		InitialSupply:      100,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 2,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	mint := DTLMintTx{
		Proposer: authSigners[0].Address,
		To:       "bob",
		TokenID:  tokenID,
		Amount:   200,
	}
	payloadHash, err := DTLPayloadHash(struct {
		TokenID string `json:"token_id"`
		To      string `json:"to"`
		Amount  uint64 `json:"amount"`
	}{
		TokenID: tokenID,
		To:      "bob",
		Amount:  200,
	})
	if err != nil {
		t.Fatalf("payload hash failed: %v", err)
	}

	err = ApplyDTLMintTx(
		state,
		mint,
		buildDTLCertForSigners(
			t,
			tokenID,
			10,
			DTLGovMint,
			payloadHash,
			authSigners[:1],
		),
		10,
		64,
	)
	if err == nil {
		t.Fatalf("expected threshold failure, got nil")
	}

	if err := ApplyDTLMintTx(
		state,
		mint,
		buildDTLCertForSigners(
			t,
			tokenID,
			11,
			DTLGovMint,
			payloadHash,
			authSigners[:2],
		),
		11,
		64,
	); err != nil {
		t.Fatalf("mint failed: %v", err)
	}

	token := state.Tokens[tokenID]
	if token.TotalSupply != 300 {
		t.Fatalf("unexpected total supply: got=%d want=300", token.TotalSupply)
	}
	if got := state.BalanceOf(tokenID, "bob"); got != 200 {
		t.Fatalf("unexpected bob balance: got=%d want=200", got)
	}
}

func TestDTLGovernancePauseFreezeRotateAndReplay(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 3)
	tokenID, err := ApplyDTLCreateTx(state, "91938", 3, DTLCreateTx{
		Creator:            "alice",
		Name:               "Gov Test",
		Symbol:             "GOVT",
		Decimals:           18,
		MaxSupply:          10_000,
		InitialSupply:      1_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 2,
		FreezeEnabled:      true,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	pausePayload := struct {
		Reason string `json:"reason"`
	}{Reason: "maintenance"}
	pauseHash, _ := DTLPayloadHash(pausePayload)
	pauseCert := buildDTLCertForSigners(t, tokenID, 20, DTLGovPause, pauseHash, authSigners[:2])
	if err := ApplyDTLGovernanceAction(state, tokenID, DTLGovPause, pausePayload, pauseCert, 20, 64); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	if !state.Tokens[tokenID].Paused {
		t.Fatalf("expected paused=true")
	}

	if err := ApplyDTLGovernanceAction(state, tokenID, DTLGovPause, pausePayload, pauseCert, 20, 64); err == nil {
		t.Fatalf("expected replay rejection")
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  1,
	}); err == nil {
		t.Fatalf("expected transfer blocked while paused")
	}

	unpausePayload := struct {
		Reason string `json:"reason"`
	}{Reason: "resume"}
	unpauseHash, _ := DTLPayloadHash(unpausePayload)
	unpauseCert := buildDTLCertForSigners(
		t,
		tokenID,
		21,
		DTLGovUnpause,
		unpauseHash,
		[]dtlTestSigner{authSigners[0], authSigners[2]},
	)
	if err := ApplyDTLGovernanceAction(state, tokenID, DTLGovUnpause, unpausePayload, unpauseCert, 21, 64); err != nil {
		t.Fatalf("unpause failed: %v", err)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  100,
	}); err != nil {
		t.Fatalf("transfer should pass after unpause: %v", err)
	}

	freezePayload := DTLFreezeAccountPayload{Account: "bob"}
	freezeHash, _ := DTLPayloadHash(freezePayload)
	freezeCert := buildDTLCertForSigners(
		t,
		tokenID,
		22,
		DTLGovFreezeAccount,
		freezeHash,
		authSigners[:2],
	)
	if err := ApplyDTLGovernanceAction(state, tokenID, DTLGovFreezeAccount, freezePayload, freezeCert, 22, 64); err != nil {
		t.Fatalf("freeze failed: %v", err)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "bob",
		To:      "alice",
		TokenID: tokenID,
		Amount:  1,
	}); err == nil {
		t.Fatalf("expected frozen transfer rejection")
	}

	rotatePayload := DTLRotateAuthorityPayload{
		AuthoritySigners:   dtlSignerAddresses(newDTLTestSigners(t, 3)),
		AuthorityThreshold: 2,
	}
	rotateHash, _ := DTLPayloadHash(rotatePayload)
	rotateCert := buildDTLCertForSigners(
		t,
		tokenID,
		23,
		DTLGovRotateAuthority,
		rotateHash,
		[]dtlTestSigner{authSigners[0], authSigners[2]},
	)
	if err := ApplyDTLGovernanceAction(state, tokenID, DTLGovRotateAuthority, rotatePayload, rotateCert, 23, 64); err != nil {
		t.Fatalf("rotate authority failed: %v", err)
	}

	token := state.Tokens[tokenID]
	if token.AuthorityThreshold != 2 || len(token.AuthoritySigners) != 3 {
		t.Fatalf("authority not rotated correctly: threshold=%d signers=%d", token.AuthorityThreshold, len(token.AuthoritySigners))
	}
}

func TestDTLPoolLifecycleAndSwap(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)

	tokenA, err := ApplyDTLCreateTx(state, ChainID, 11, DTLCreateTx{
		Creator:            "alice",
		Name:               "Pool Token A",
		Symbol:             "PTA",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      10_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token A failed: %v", err)
	}
	tokenB, err := ApplyDTLCreateTx(state, ChainID, 12, DTLCreateTx{
		Creator:            "alice",
		Name:               "Pool Token B",
		Symbol:             "PTB",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      10_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token B failed: %v", err)
	}

	poolID, err := ApplyDTLPoolCreateTx(state, ChainID, 77, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenB,
		AmountA: 2_000,
		AmountB: 2_000,
	})
	if err != nil {
		t.Fatalf("pool create failed: %v", err)
	}
	if state.Pools[poolID] == nil {
		t.Fatalf("pool not found after create")
	}
	initialLP := state.LPBalanceOf(poolID, "alice")
	if initialLP == 0 {
		t.Fatalf("expected non-zero LP shares")
	}

	if err := ApplyDTLPoolAddLiquidityTx(state, DTLPoolAddLiquidityTx{
		Provider:    "alice",
		PoolID:      poolID,
		AmountA:     500,
		AmountB:     500,
		MinLPShares: 1,
	}); err != nil {
		t.Fatalf("add liquidity failed: %v", err)
	}
	if got := state.LPBalanceOf(poolID, "alice"); got <= initialLP {
		t.Fatalf("expected LP balance increase: before=%d after=%d", initialLP, got)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenA,
		Amount:  300,
	}); err != nil {
		t.Fatalf("fund bob tokenA failed: %v", err)
	}
	bobTokenBBefore := state.BalanceOf(tokenB, "bob")
	amountOut, err := ApplyDTLPoolSwapTx(state, DTLPoolSwapTx{
		Trader:       "bob",
		PoolID:       poolID,
		TokenIn:      tokenA,
		AmountIn:     100,
		MinAmountOut: 1,
	})
	if err != nil {
		t.Fatalf("swap failed: %v", err)
	}
	if amountOut == 0 {
		t.Fatalf("expected non-zero swap output")
	}
	if got := state.BalanceOf(tokenB, "bob"); got <= bobTokenBBefore {
		t.Fatalf("expected bob tokenB increase: before=%d after=%d", bobTokenBBefore, got)
	}

	if err := ApplyDTLPoolRemoveLiquidityTx(state, DTLPoolRemoveLiquidityTx{
		Provider: "alice",
		PoolID:   poolID,
		LPShares: 200,
	}); err != nil {
		t.Fatalf("remove liquidity failed: %v", err)
	}
}

func TestDTLPoolSwapRouteApplyAndQuote(t *testing.T) {
	prevRouterEnabled := ConfigDTLRouterEnabled
	prevMaxHops := ConfigDTLRouterMaxHops
	prevDeadline := ConfigDTLRouterDeadlineMaxBlocks
	prevImpact := ConfigDTLRouterMaxPriceImpactBPS
	t.Cleanup(func() {
		ConfigDTLRouterEnabled = prevRouterEnabled
		ConfigDTLRouterMaxHops = prevMaxHops
		ConfigDTLRouterDeadlineMaxBlocks = prevDeadline
		ConfigDTLRouterMaxPriceImpactBPS = prevImpact
	})
	ConfigDTLRouterEnabled = true
	ConfigDTLRouterMaxHops = 4
	ConfigDTLRouterDeadlineMaxBlocks = 50
	ConfigDTLRouterMaxPriceImpactBPS = 9000

	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)

	tokenA, err := ApplyDTLCreateTx(state, ChainID, 101, DTLCreateTx{
		Creator:            "alice",
		Name:               "Route Token A",
		Symbol:             "RTA",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      200_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token A failed: %v", err)
	}
	tokenB, err := ApplyDTLCreateTx(state, ChainID, 102, DTLCreateTx{
		Creator:            "alice",
		Name:               "Route Token B",
		Symbol:             "RTB",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      200_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token B failed: %v", err)
	}
	tokenC, err := ApplyDTLCreateTx(state, ChainID, 103, DTLCreateTx{
		Creator:            "alice",
		Name:               "Route Token C",
		Symbol:             "RTC",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      200_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token C failed: %v", err)
	}

	poolAB, err := ApplyDTLPoolCreateTx(state, ChainID, 201, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenB,
		AmountA: 20_000,
		AmountB: 20_000,
	})
	if err != nil {
		t.Fatalf("create pool AB failed: %v", err)
	}
	poolBC, err := ApplyDTLPoolCreateTx(state, ChainID, 202, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenB,
		TokenB:  tokenC,
		AmountA: 20_000,
		AmountB: 20_000,
	})
	if err != nil {
		t.Fatalf("create pool BC failed: %v", err)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenA,
		Amount:  2_000,
	}); err != nil {
		t.Fatalf("fund bob with tokenA failed: %v", err)
	}

	routeTx := DTLPoolSwapRouteTx{
		Trader:         "bob",
		TokenIn:        tokenA,
		AmountIn:       500,
		MinAmountOut:   1,
		Path:           []string{poolAB, poolBC},
		DeadlineHeight: 40,
	}
	if err := ValidateDTLPoolSwapRouteTx(state, routeTx, 10); err != nil {
		t.Fatalf("route validation failed: %v", err)
	}

	preview, err := dtlQuotePoolSwapRoute(state, routeTx.TokenIn, routeTx.AmountIn, routeTx.Path)
	if err != nil {
		t.Fatalf("route quote failed: %v", err)
	}
	if preview.AmountOut == 0 {
		t.Fatalf("expected non-zero route output")
	}
	if len(preview.Hops) != 2 {
		t.Fatalf("unexpected hop count: got=%d want=2", len(preview.Hops))
	}

	bobBeforeA := state.BalanceOf(tokenA, "bob")
	bobBeforeC := state.BalanceOf(tokenC, "bob")
	out, err := ApplyDTLPoolSwapRouteTx(state, routeTx, 10)
	if err != nil {
		t.Fatalf("route apply failed: %v", err)
	}
	if out != preview.AmountOut {
		t.Fatalf("output mismatch: got=%d want=%d", out, preview.AmountOut)
	}
	if got := state.BalanceOf(tokenA, "bob"); got >= bobBeforeA {
		t.Fatalf("expected tokenA decrease: before=%d after=%d", bobBeforeA, got)
	}
	if got := state.BalanceOf(tokenC, "bob"); got <= bobBeforeC {
		t.Fatalf("expected tokenC increase: before=%d after=%d", bobBeforeC, got)
	}
	if len(state.EventLogs) < 3 {
		t.Fatalf("expected structured route event logs, got=%d", len(state.EventLogs))
	}
	foundSummary := false
	for _, ev := range state.Events {
		if strings.HasPrefix(ev, "POOL_ROUTE_SWAP:") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("expected POOL_ROUTE_SWAP event in event stream")
	}
}

func TestDTLPoolSwapRouteValidationGuards(t *testing.T) {
	prevRouterEnabled := ConfigDTLRouterEnabled
	prevMaxHops := ConfigDTLRouterMaxHops
	prevDeadline := ConfigDTLRouterDeadlineMaxBlocks
	prevImpact := ConfigDTLRouterMaxPriceImpactBPS
	t.Cleanup(func() {
		ConfigDTLRouterEnabled = prevRouterEnabled
		ConfigDTLRouterMaxHops = prevMaxHops
		ConfigDTLRouterDeadlineMaxBlocks = prevDeadline
		ConfigDTLRouterMaxPriceImpactBPS = prevImpact
	})
	ConfigDTLRouterEnabled = true
	ConfigDTLRouterMaxHops = 2
	ConfigDTLRouterDeadlineMaxBlocks = 3
	ConfigDTLRouterMaxPriceImpactBPS = 9000

	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	tokenA, err := ApplyDTLCreateTx(state, ChainID, 301, DTLCreateTx{
		Creator:            "alice",
		Name:               "Guard A",
		Symbol:             "GDA",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      100_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create guard token A failed: %v", err)
	}
	tokenB, err := ApplyDTLCreateTx(state, ChainID, 302, DTLCreateTx{
		Creator:            "alice",
		Name:               "Guard B",
		Symbol:             "GDB",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      100_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create guard token B failed: %v", err)
	}
	poolAB, err := ApplyDTLPoolCreateTx(state, ChainID, 401, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenB,
		AmountA: 10_000,
		AmountB: 10_000,
	})
	if err != nil {
		t.Fatalf("create guard pool failed: %v", err)
	}
	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenA,
		Amount:  1_000,
	}); err != nil {
		t.Fatalf("fund bob failed: %v", err)
	}

	tooFar := DTLPoolSwapRouteTx{
		Trader:         "bob",
		TokenIn:        tokenA,
		AmountIn:       100,
		MinAmountOut:   1,
		Path:           []string{poolAB},
		DeadlineHeight: 20,
	}
	if err := ValidateDTLPoolSwapRouteTx(state, tooFar, 10); err == nil || !strings.Contains(err.Error(), "deadline too far") {
		t.Fatalf("expected deadline-too-far error, got: %v", err)
	}

	expired := tooFar
	expired.DeadlineHeight = 9
	if err := ValidateDTLPoolSwapRouteTx(state, expired, 10); err == nil || !strings.Contains(err.Error(), "deadline expired") {
		t.Fatalf("expected deadline-expired error, got: %v", err)
	}

	ConfigDTLRouterDeadlineMaxBlocks = 30
	ConfigDTLRouterMaxPriceImpactBPS = 1
	highImpact := tooFar
	highImpact.DeadlineHeight = 20
	highImpact.AmountIn = 800
	if err := ValidateDTLPoolSwapRouteTx(state, highImpact, 10); err == nil || !strings.Contains(err.Error(), "price impact") {
		t.Fatalf("expected price-impact guard error, got: %v", err)
	}
}

func TestDTLBestPoolSwapRouteQuoteSelectsBestPath(t *testing.T) {
	prevRouterEnabled := ConfigDTLRouterEnabled
	prevMaxHops := ConfigDTLRouterMaxHops
	prevQuoteMaxPaths := ConfigDTLRouterQuoteMaxPaths
	prevImpact := ConfigDTLRouterMaxPriceImpactBPS
	t.Cleanup(func() {
		ConfigDTLRouterEnabled = prevRouterEnabled
		ConfigDTLRouterMaxHops = prevMaxHops
		ConfigDTLRouterQuoteMaxPaths = prevQuoteMaxPaths
		ConfigDTLRouterMaxPriceImpactBPS = prevImpact
	})
	ConfigDTLRouterEnabled = true
	ConfigDTLRouterMaxHops = 3
	ConfigDTLRouterQuoteMaxPaths = 16
	ConfigDTLRouterMaxPriceImpactBPS = 9000

	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	tokenA, err := ApplyDTLCreateTx(state, ChainID, 501, DTLCreateTx{
		Creator:            "alice",
		Name:               "Best A",
		Symbol:             "BSA",
		Decimals:           18,
		MaxSupply:          2_000_000,
		InitialSupply:      300_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token A failed: %v", err)
	}
	tokenB, err := ApplyDTLCreateTx(state, ChainID, 502, DTLCreateTx{
		Creator:            "alice",
		Name:               "Best B",
		Symbol:             "BSB",
		Decimals:           18,
		MaxSupply:          2_000_000,
		InitialSupply:      300_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token B failed: %v", err)
	}
	tokenC, err := ApplyDTLCreateTx(state, ChainID, 503, DTLCreateTx{
		Creator:            "alice",
		Name:               "Best C",
		Symbol:             "BSC",
		Decimals:           18,
		MaxSupply:          2_000_000,
		InitialSupply:      300_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token C failed: %v", err)
	}

	_, err = ApplyDTLPoolCreateTx(state, ChainID, 601, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenC,
		AmountA: 1_000,
		AmountB: 1_000,
	})
	if err != nil {
		t.Fatalf("create direct pool failed: %v", err)
	}
	poolAB, err := ApplyDTLPoolCreateTx(state, ChainID, 602, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenB,
		AmountA: 20_000,
		AmountB: 20_000,
	})
	if err != nil {
		t.Fatalf("create pool AB failed: %v", err)
	}
	poolBC, err := ApplyDTLPoolCreateTx(state, ChainID, 603, DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenB,
		TokenB:  tokenC,
		AmountA: 20_000,
		AmountB: 20_000,
	})
	if err != nil {
		t.Fatalf("create pool BC failed: %v", err)
	}

	quote, err := dtlBestPoolSwapRouteQuote(state, tokenA, tokenC, 200, 3)
	if err != nil {
		t.Fatalf("best route quote failed: %v", err)
	}
	if len(quote.Path) != 2 {
		t.Fatalf("expected 2-hop path, got=%d path=%v", len(quote.Path), quote.Path)
	}
	if quote.Path[0] != normalizeDTLPoolID(poolAB) || quote.Path[1] != normalizeDTLPoolID(poolBC) {
		t.Fatalf("unexpected best path: got=%v want=[%s %s]", quote.Path, poolAB, poolBC)
	}
	if quote.AmountOut == 0 {
		t.Fatalf("expected non-zero amount_out")
	}
}

func TestDTLDuelCommitRevealFinalize(t *testing.T) {
	prevV2 := ConfigDTLContractsV2Enabled
	prevHeight := ConfigDTLV2ActivationHeight
	ConfigDTLContractsV2Enabled = true
	ConfigDTLV2ActivationHeight = 0
	t.Cleanup(func() {
		ConfigDTLContractsV2Enabled = prevV2
		ConfigDTLV2ActivationHeight = prevHeight
	})

	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	tokenID, err := ApplyDTLCreateTx(state, ChainID, 20, DTLCreateTx{
		Creator:            "alice",
		Name:               "Duel Token",
		Symbol:             "DUL",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      2_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create duel token failed: %v", err)
	}

	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  500,
	}); err != nil {
		t.Fatalf("fund bob failed: %v", err)
	}

	secretA := "alice-secret"
	secretB := "bob-secret"
	createTx := DTLDuelCreateTx{
		Creator:            "alice",
		TokenID:            tokenID,
		Stake:              100,
		CommitHash:         DTLDuelCommitHash(secretA),
		JoinExpiryBlocks:   5,
		RevealExpiryBlocks: 5,
	}
	duelID, err := ApplyDTLDuelCreateTx(state, ChainID, 99, 10, createTx)
	if err != nil {
		t.Fatalf("duel create failed: %v", err)
	}
	if state.Duels[duelID] == nil {
		t.Fatalf("duel state missing")
	}

	if err := ApplyDTLDuelJoinTx(state, 12, DTLDuelJoinTx{
		Joiner:     "bob",
		DuelID:     duelID,
		CommitHash: DTLDuelCommitHash(secretB),
	}); err != nil {
		t.Fatalf("duel join failed: %v", err)
	}
	if err := ApplyDTLDuelRevealTx(state, 13, DTLDuelRevealTx{
		Player: "alice",
		DuelID: duelID,
		Secret: secretA,
	}); err != nil {
		t.Fatalf("duel reveal A failed: %v", err)
	}
	if err := ApplyDTLDuelRevealTx(state, 14, DTLDuelRevealTx{
		Player: "bob",
		DuelID: duelID,
		Secret: secretB,
	}); err != nil {
		t.Fatalf("duel reveal B failed: %v", err)
	}

	duel := state.Duels[duelID]
	finalizeHeight := duel.BeaconHeight
	if finalizeHeight == 0 {
		finalizeHeight = duel.RevealDeadline + ConfigDTLGameBeaconDelayBlocks
	}
	beaconHash := dtlDeterministicBeaconHash(duelID, finalizeHeight)
	expectedWinner := dtlDuelWinner(duelID+"|"+beaconHash, "alice", "bob", secretA, secretB)
	if expectedWinner == "" {
		t.Fatalf("winner calculation failed")
	}
	if err := ApplyDTLDuelFinalizeTx(state, finalizeHeight, DTLDuelFinalizeTx{
		Caller: "alice",
		DuelID: duelID,
	}); err != nil {
		t.Fatalf("duel finalize failed: %v", err)
	}

	duel = state.Duels[duelID]
	if !duel.Settled {
		t.Fatalf("expected settled duel")
	}
	if duel.Winner != expectedWinner {
		t.Fatalf("winner mismatch: got=%s want=%s", duel.Winner, expectedWinner)
	}
}

func TestDTLLendingLifecycleAndLiquidation(t *testing.T) {
	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	collateralToken, err := ApplyDTLCreateTx(state, ChainID, 31, DTLCreateTx{
		Creator:            "alice",
		Name:               "Collateral Token",
		Symbol:             "COL",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      20_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create collateral token failed: %v", err)
	}
	debtToken, err := ApplyDTLCreateTx(state, ChainID, 32, DTLCreateTx{
		Creator:            "alice",
		Name:               "Debt Token",
		Symbol:             "DEBT",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      20_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create debt token failed: %v", err)
	}
	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: collateralToken,
		Amount:  2_000,
	}); err != nil {
		t.Fatalf("fund bob collateral failed: %v", err)
	}

	marketID, err := ApplyDTLLendMarketCreateTx(state, ChainID, 300, DTLLendMarketCreateTx{
		Creator:             "alice",
		CollateralTokenID:   collateralToken,
		DebtTokenID:         debtToken,
		DebtLiquidity:       5_000,
		CollateralFactorBPS: 7000,
		LiquidationBonusBPS: 500,
	})
	if err != nil {
		t.Fatalf("lend market create failed: %v", err)
	}
	if state.LendingMarkets[marketID] == nil {
		t.Fatalf("expected market in state")
	}

	if err := ApplyDTLLendDepositCollateralTx(state, DTLLendDepositCollateralTx{
		Account:  "bob",
		MarketID: marketID,
		Amount:   500,
	}); err != nil {
		t.Fatalf("deposit collateral failed: %v", err)
	}
	if err := ApplyDTLLendBorrowTx(state, DTLLendBorrowTx{
		Account:  "bob",
		MarketID: marketID,
		Amount:   300,
	}); err != nil {
		t.Fatalf("borrow failed: %v", err)
	}
	position := state.LendingPositions[dtlLendingPositionKey(marketID, "bob")]
	if position == nil || position.Debt != 300 || position.Collateral != 500 {
		t.Fatalf("unexpected lending position after borrow: %+v", position)
	}

	if err := ApplyDTLLendRepayTx(state, DTLLendRepayTx{
		Account:  "bob",
		MarketID: marketID,
		Amount:   50,
	}); err != nil {
		t.Fatalf("repay failed: %v", err)
	}
	if err := ApplyDTLLendWithdrawCollateralTx(state, DTLLendWithdrawCollateralTx{
		Account:  "bob",
		MarketID: marketID,
		Amount:   100,
	}); err != nil {
		t.Fatalf("withdraw collateral failed: %v", err)
	}

	// Force unhealthy by changing risk parameter (simulates governance risk update).
	market := state.LendingMarkets[marketID]
	market.CollateralFactorBPS = 3000
	beforeDebt := position.Debt
	beforeCollateral := position.Collateral
	if err := ApplyDTLLendLiquidateTx(state, DTLLendLiquidateTx{
		Liquidator:  "alice",
		Borrower:    "bob",
		MarketID:    marketID,
		RepayAmount: 50,
	}); err != nil {
		t.Fatalf("liquidation failed: %v", err)
	}
	if position.Debt >= beforeDebt {
		t.Fatalf("expected debt reduction after liquidation: before=%d after=%d", beforeDebt, position.Debt)
	}
	if position.Collateral >= beforeCollateral {
		t.Fatalf("expected collateral reduction after liquidation: before=%d after=%d", beforeCollateral, position.Collateral)
	}
}

func TestDTLTournamentCommitRevealFinalize(t *testing.T) {
	prevV2 := ConfigDTLContractsV2Enabled
	prevHeight := ConfigDTLV2ActivationHeight
	ConfigDTLContractsV2Enabled = true
	ConfigDTLV2ActivationHeight = 0
	t.Cleanup(func() {
		ConfigDTLContractsV2Enabled = prevV2
		ConfigDTLV2ActivationHeight = prevHeight
	})

	state := NewDTLState()
	authSigners := newDTLTestSigners(t, 2)
	tokenID, err := ApplyDTLCreateTx(state, ChainID, 41, DTLCreateTx{
		Creator:            "alice",
		Name:               "Tournament Token",
		Symbol:             "TTOK",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      10_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}
	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenID,
		Amount:  500,
	}); err != nil {
		t.Fatalf("fund bob failed: %v", err)
	}
	if err := ApplyDTLTransferTx(state, DTLTransferTx{
		From:    "alice",
		To:      "carol",
		TokenID: tokenID,
		Amount:  500,
	}); err != nil {
		t.Fatalf("fund carol failed: %v", err)
	}

	tournamentID, err := ApplyDTLTournamentCreateTx(state, ChainID, 420, 10, DTLTournamentCreateTx{
		Creator:            "alice",
		TokenID:            tokenID,
		EntryFee:           100,
		MaxPlayers:         3,
		JoinExpiryBlocks:   5,
		RevealExpiryBlocks: 5,
	})
	if err != nil {
		t.Fatalf("tournament create failed: %v", err)
	}
	if err := ApplyDTLTournamentJoinTx(state, 11, DTLTournamentJoinTx{
		Player:       "alice",
		TournamentID: tournamentID,
		CommitHash:   DTLDuelCommitHash("a-secret"),
	}); err != nil {
		t.Fatalf("alice join failed: %v", err)
	}
	if err := ApplyDTLTournamentJoinTx(state, 11, DTLTournamentJoinTx{
		Player:       "bob",
		TournamentID: tournamentID,
		CommitHash:   DTLDuelCommitHash("b-secret"),
	}); err != nil {
		t.Fatalf("bob join failed: %v", err)
	}
	if err := ApplyDTLTournamentJoinTx(state, 11, DTLTournamentJoinTx{
		Player:       "carol",
		TournamentID: tournamentID,
		CommitHash:   DTLDuelCommitHash("c-secret"),
	}); err != nil {
		t.Fatalf("carol join failed: %v", err)
	}

	if err := ApplyDTLTournamentRevealTx(state, 13, DTLTournamentRevealTx{
		Player:       "alice",
		TournamentID: tournamentID,
		Secret:       "a-secret",
	}); err != nil {
		t.Fatalf("alice reveal failed: %v", err)
	}
	if err := ApplyDTLTournamentRevealTx(state, 13, DTLTournamentRevealTx{
		Player:       "bob",
		TournamentID: tournamentID,
		Secret:       "b-secret",
	}); err != nil {
		t.Fatalf("bob reveal failed: %v", err)
	}

	tournament := state.Tournaments[tournamentID]
	if tournament == nil {
		t.Fatalf("expected tournament state")
	}
	finalizeHeight := tournament.BeaconHeight
	if finalizeHeight == 0 {
		finalizeHeight = tournament.RevealDeadline + ConfigDTLGameBeaconDelayBlocks
	}
	winnerScope := tournamentID
	if finalizeHeight > tournament.RevealDeadline {
		winnerScope = tournamentID + "|" + dtlDeterministicBeaconHash(tournamentID, finalizeHeight)
	}
	winner := dtlTournamentWinner(winnerScope, []string{"alice", "bob"}, tournament.Reveals)
	if winner == "" {
		t.Fatalf("winner resolution failed")
	}
	beforeWinnerBalance := state.BalanceOf(tokenID, winner)
	if err := ApplyDTLTournamentFinalizeTx(state, finalizeHeight, DTLTournamentFinalizeTx{
		Caller:       "alice",
		TournamentID: tournamentID,
	}); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	tournament = state.Tournaments[tournamentID]
	if !tournament.Settled {
		t.Fatalf("expected tournament settled")
	}
	if tournament.Winner != winner {
		t.Fatalf("winner mismatch: got=%s want=%s", tournament.Winner, winner)
	}
	if got := state.BalanceOf(tokenID, winner); got <= beforeWinnerBalance {
		t.Fatalf("expected winner balance increase: before=%d after=%d", beforeWinnerBalance, got)
	}
}

func TestDTLContractDeployRejected(t *testing.T) {
	state := NewDTLState()
	_, err := ApplyDTLContractDeployTx(state, ChainID, 77, DTLContractDeployTx{
		Creator: "alice",
		Name:    "CounterAndPay",
		Lang:    "solidity-like",
		Version: 1,
		Methods: []DTLContractMethodState{
			{Name: "inc", Op: DTLContractOpAddU64, Key: "count", Arg: "delta"},
		},
		Init: map[string]string{
			"count": "0",
		},
	})
	if err == nil {
		t.Fatalf("expected contract deploy to be rejected")
	}
}

func TestDTLContractCallRejected(t *testing.T) {
	state := NewDTLState()
	err := ApplyDTLContractCallTx(state, DTLContractCallTx{
		Caller:     "alice",
		ContractID: "contract_1",
		Method:     "inc",
		Args: map[string]string{
			"delta": "1",
		},
	})
	if err == nil {
		t.Fatalf("expected contract call to be rejected")
	}
}
