package main

import (
	"errors"
	"encoding/json"
	"strings"
	"testing"
)

func TestExecuteTransactionDTLCreateAndTransfer(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 1000)

	authSigners := newDTLTestSigners(t, 3)
	createObj := DTLCreateTx{
		Creator:            "alice",
		Name:               "Chain DTL",
		Symbol:             "CDTL",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      500,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 2,
		FreezeEnabled:      true,
		TaxBPS:             0,
	}
	createPayloadBytes, err := json.Marshal(createObj)
	if err != nil {
		t.Fatalf("marshal create payload failed: %v", err)
	}
	createPayload := string(createPayloadBytes)

	createTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: createPayload,
		ChainID:    ChainID,
	}
	createTx.Fee = requiredFeeForTx(createTx)

	next, err := ExecuteTransaction(&ledger, createTx, 1)
	if err != nil {
		t.Fatalf("create tx failed: %v", err)
	}

	tokenID := next.DTL.SymbolIndex["CDTL"]
	if tokenID == "" {
		t.Fatalf("token id not indexed for symbol CDTL")
	}
	if got := next.DTL.BalanceOf(tokenID, "alice"); got != 500 {
		t.Fatalf("unexpected DTL balance: got=%d want=500", got)
	}
	if got := getBalance(next, CoinSymbol, "alice"); got != 999 {
		t.Fatalf("unexpected base balance after create: got=%d want=999", got)
	}
	if got := getNonce(next, "alice"); got != 1 {
		t.Fatalf("unexpected nonce after create: got=%d want=1", got)
	}

	transferPayload := `{"from":"alice","to":"bob","token_id":"` + tokenID + `","amount":120}`
	transferTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenTransfer),
		DTLTokenID: tokenID,
		DTLPayload: transferPayload,
		ChainID:    ChainID,
	}
	transferTx.Fee = requiredFeeForTx(transferTx)

	next2, err := ExecuteTransaction(&next, transferTx, 2)
	if err != nil {
		t.Fatalf("transfer tx failed: %v", err)
	}
	if got := next2.DTL.BalanceOf(tokenID, "alice"); got != 380 {
		t.Fatalf("unexpected alice DTL balance: got=%d want=380", got)
	}
	if got := next2.DTL.BalanceOf(tokenID, "bob"); got != 120 {
		t.Fatalf("unexpected bob DTL balance: got=%d want=120", got)
	}
	if got := getBalance(next2, CoinSymbol, "alice"); got != 998 {
		t.Fatalf("unexpected base balance after transfer: got=%d want=998", got)
	}
}

func TestExecuteTransactionDTLApproveAndTransferFrom(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 1000)
	addBalance(&ledger, CoinSymbol, "charlie", 1000)

	authSigners := newDTLTestSigners(t, 2)
	createObj := DTLCreateTx{
		Creator:            "alice",
		Name:               "Allowance Token",
		Symbol:             "ALW",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      500,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
		FreezeEnabled:      true,
		TaxBPS:             0,
	}
	createPayloadBytes, err := json.Marshal(createObj)
	if err != nil {
		t.Fatalf("marshal create payload failed: %v", err)
	}

	createTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: string(createPayloadBytes),
		ChainID:    ChainID,
	}
	createTx.Fee = requiredFeeForTx(createTx)
	next, err := ExecuteTransaction(&ledger, createTx, 1)
	if err != nil {
		t.Fatalf("create tx failed: %v", err)
	}

	tokenID := next.DTL.SymbolIndex["ALW"]
	if tokenID == "" {
		t.Fatalf("token id not indexed for symbol ALW")
	}

	approvePayload := `{"owner":"alice","spender":"charlie","token_id":"` + tokenID + `","amount":70}`
	approveTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenApprove),
		DTLTokenID: tokenID,
		DTLPayload: approvePayload,
		ChainID:    ChainID,
	}
	approveTx.Fee = requiredFeeForTx(approveTx)
	next2, err := ExecuteTransaction(&next, approveTx, 2)
	if err != nil {
		t.Fatalf("approve tx failed: %v", err)
	}
	if got := next2.DTL.AllowanceOf(tokenID, "alice", "charlie"); got != 70 {
		t.Fatalf("unexpected allowance after approve: got=%d want=70", got)
	}

	transferFromPayload := `{"spender":"charlie","from":"alice","to":"bob","token_id":"` + tokenID + `","amount":30}`
	transferFromTx := Transaction{
		From:       "charlie",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenTransferFrom),
		DTLTokenID: tokenID,
		DTLPayload: transferFromPayload,
		ChainID:    ChainID,
	}
	transferFromTx.Fee = requiredFeeForTx(transferFromTx)
	next3, err := ExecuteTransaction(&next2, transferFromTx, 3)
	if err != nil {
		t.Fatalf("transfer_from tx failed: %v", err)
	}
	if got := next3.DTL.BalanceOf(tokenID, "alice"); got != 470 {
		t.Fatalf("unexpected alice DTL balance: got=%d want=470", got)
	}
	if got := next3.DTL.BalanceOf(tokenID, "bob"); got != 30 {
		t.Fatalf("unexpected bob DTL balance: got=%d want=30", got)
	}
	if got := next3.DTL.AllowanceOf(tokenID, "alice", "charlie"); got != 40 {
		t.Fatalf("unexpected remaining allowance: got=%d want=40", got)
	}
}

func TestExecuteTransactionDTLBurnRequiresSelfAndBalance(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 1000)
	addBalance(&ledger, CoinSymbol, "bob", 1000)

	authSigners := newDTLTestSigners(t, 2)
	createObj := DTLCreateTx{
		Creator:            "alice",
		Name:               "Burn Token",
		Symbol:             "BRN",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      500,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
		FreezeEnabled:      true,
		TaxBPS:             0,
	}
	createPayloadBytes, err := json.Marshal(createObj)
	if err != nil {
		t.Fatalf("marshal create payload failed: %v", err)
	}
	createTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: string(createPayloadBytes),
		ChainID:    ChainID,
	}
	createTx.Fee = requiredFeeForTx(createTx)
	next, err := ExecuteTransaction(&ledger, createTx, 1)
	if err != nil {
		t.Fatalf("create tx failed: %v", err)
	}

	tokenID := next.DTL.SymbolIndex["BRN"]
	if tokenID == "" {
		t.Fatalf("token id not indexed for symbol BRN")
	}

	transferPayload := `{"from":"alice","to":"bob","token_id":"` + tokenID + `","amount":120}`
	transferTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenTransfer),
		DTLTokenID: tokenID,
		DTLPayload: transferPayload,
		ChainID:    ChainID,
	}
	transferTx.Fee = requiredFeeForTx(transferTx)
	next2, err := ExecuteTransaction(&next, transferTx, 2)
	if err != nil {
		t.Fatalf("transfer tx failed: %v", err)
	}

	bobBurnPayload := `{"from":"bob","token_id":"` + tokenID + `","amount":100}`
	bobBurnTx := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenBurn),
		DTLTokenID: tokenID,
		DTLPayload: bobBurnPayload,
		ChainID:    ChainID,
	}
	bobBurnTx.Fee = requiredFeeForTx(bobBurnTx)
	next3, err := ExecuteTransaction(&next2, bobBurnTx, 3)
	if err != nil {
		t.Fatalf("bob burn tx failed: %v", err)
	}
	if got := next3.DTL.BalanceOf(tokenID, "bob"); got != 20 {
		t.Fatalf("unexpected bob balance after burn: got=%d want=20", got)
	}
	if got := next3.DTL.Tokens[tokenID].TotalSupply; got != 400 {
		t.Fatalf("unexpected supply after burn: got=%d want=400", got)
	}

	creatorBurnOtherPayload := `{"from":"bob","token_id":"` + tokenID + `","amount":1}`
	creatorBurnOtherTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      3,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenBurn),
		DTLTokenID: tokenID,
		DTLPayload: creatorBurnOtherPayload,
		ChainID:    ChainID,
	}
	creatorBurnOtherTx.Fee = requiredFeeForTx(creatorBurnOtherTx)
	if _, err := ExecuteTransaction(&next3, creatorBurnOtherTx, 4); err == nil || !strings.Contains(err.Error(), "dtl: burn from mismatch") {
		t.Fatalf("expected burn from mismatch error, got: %v", err)
	}

	overBurnPayload := `{"from":"bob","token_id":"` + tokenID + `","amount":25}`
	overBurnTx := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenBurn),
		DTLTokenID: tokenID,
		DTLPayload: overBurnPayload,
		ChainID:    ChainID,
	}
	overBurnTx.Fee = requiredFeeForTx(overBurnTx)
	if _, err := ExecuteTransaction(&next3, overBurnTx, 5); !errors.Is(err, ErrDTLInsufficientFunds) {
		t.Fatalf("expected ErrDTLInsufficientFunds, got: %v", err)
	}
}

func TestExecuteTransactionDTLNFT721Flow(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 1000)
	addBalance(&ledger, CoinSymbol, "bob", 1000)

	createPayloadBytes, err := json.Marshal(DTLNFT721CreateTx{
		Creator: "alice",
		Name:    "Chain NFT 721",
		Symbol:  "CN721",
		BaseURI: "ipfs://cn721/",
	})
	if err != nil {
		t.Fatalf("marshal nft721 create payload failed: %v", err)
	}
	createTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxNFT721Create),
		DTLPayload: string(createPayloadBytes),
		ChainID:    ChainID,
	}
	createTx.Fee = requiredFeeForTx(createTx)
	next, err := ExecuteTransaction(&ledger, createTx, 1)
	if err != nil {
		t.Fatalf("nft721 create tx failed: %v", err)
	}

	collectionID := next.DTL.NFT721SymbolIndex["CN721"]
	if collectionID == "" {
		t.Fatalf("nft721 collection id not indexed for symbol CN721")
	}

	mintPayloadBytes, err := json.Marshal(DTLNFT721MintTx{
		Creator:      "alice",
		CollectionID: collectionID,
		To:           "bob",
		TokenURI:     "ipfs://cn721/1",
	})
	if err != nil {
		t.Fatalf("marshal nft721 mint payload failed: %v", err)
	}
	mintTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxNFT721Mint),
		DTLTokenID: collectionID,
		DTLPayload: string(mintPayloadBytes),
		ChainID:    ChainID,
	}
	mintTx.Fee = requiredFeeForTx(mintTx)
	next2, err := ExecuteTransaction(&next, mintTx, 2)
	if err != nil {
		t.Fatalf("nft721 mint tx failed: %v", err)
	}
	if got := next2.DTL.NFT721OwnerOf(collectionID, 1); got != "bob" {
		t.Fatalf("unexpected nft721 owner after mint: got=%s want=bob", got)
	}

	transferPayloadBytes, err := json.Marshal(DTLNFT721TransferTx{
		From:         "bob",
		To:           "carol",
		CollectionID: collectionID,
		TokenID:      1,
	})
	if err != nil {
		t.Fatalf("marshal nft721 transfer payload failed: %v", err)
	}
	transferTx := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxNFT721Transfer),
		DTLTokenID: collectionID,
		DTLPayload: string(transferPayloadBytes),
		ChainID:    ChainID,
	}
	transferTx.Fee = requiredFeeForTx(transferTx)
	next3, err := ExecuteTransaction(&next2, transferTx, 3)
	if err != nil {
		t.Fatalf("nft721 transfer tx failed: %v", err)
	}
	if got := next3.DTL.NFT721OwnerOf(collectionID, 1); got != "carol" {
		t.Fatalf("unexpected nft721 owner after transfer: got=%s want=carol", got)
	}
}

func TestExecuteTransactionDTLNFT1155Flow(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 1000)
	addBalance(&ledger, CoinSymbol, "bob", 1000)

	createPayloadBytes, err := json.Marshal(DTLNFT1155CreateTx{
		Creator: "alice",
		Name:    "Chain NFT 1155",
		Symbol:  "CN1155",
		BaseURI: "ipfs://cn1155/{id}.json",
	})
	if err != nil {
		t.Fatalf("marshal nft1155 create payload failed: %v", err)
	}
	createTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxNFT1155Create),
		DTLPayload: string(createPayloadBytes),
		ChainID:    ChainID,
	}
	createTx.Fee = requiredFeeForTx(createTx)
	next, err := ExecuteTransaction(&ledger, createTx, 1)
	if err != nil {
		t.Fatalf("nft1155 create tx failed: %v", err)
	}

	collectionID := next.DTL.NFT1155SymbolIndex["CN1155"]
	if collectionID == "" {
		t.Fatalf("nft1155 collection id not indexed for symbol CN1155")
	}

	mintPayloadBytes, err := json.Marshal(DTLNFT1155MintTx{
		Creator:      "alice",
		CollectionID: collectionID,
		To:           "bob",
		TokenID:      5,
		Amount:       100,
	})
	if err != nil {
		t.Fatalf("marshal nft1155 mint payload failed: %v", err)
	}
	mintTx := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxNFT1155Mint),
		DTLTokenID: collectionID,
		DTLPayload: string(mintPayloadBytes),
		ChainID:    ChainID,
	}
	mintTx.Fee = requiredFeeForTx(mintTx)
	next2, err := ExecuteTransaction(&next, mintTx, 2)
	if err != nil {
		t.Fatalf("nft1155 mint tx failed: %v", err)
	}
	if got := next2.DTL.NFT1155BalanceOf(collectionID, 5, "bob"); got != 100 {
		t.Fatalf("unexpected nft1155 balance after mint: got=%d want=100", got)
	}

	transferPayloadBytes, err := json.Marshal(DTLNFT1155TransferTx{
		From:         "bob",
		To:           "carol",
		CollectionID: collectionID,
		TokenID:      5,
		Amount:       40,
	})
	if err != nil {
		t.Fatalf("marshal nft1155 transfer payload failed: %v", err)
	}
	transferTx := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Fee:        1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxNFT1155Transfer),
		DTLTokenID: collectionID,
		DTLPayload: string(transferPayloadBytes),
		ChainID:    ChainID,
	}
	transferTx.Fee = requiredFeeForTx(transferTx)
	next3, err := ExecuteTransaction(&next2, transferTx, 3)
	if err != nil {
		t.Fatalf("nft1155 transfer tx failed: %v", err)
	}
	if got := next3.DTL.NFT1155BalanceOf(collectionID, 5, "bob"); got != 60 {
		t.Fatalf("unexpected nft1155 sender balance: got=%d want=60", got)
	}
	if got := next3.DTL.NFT1155BalanceOf(collectionID, 5, "carol"); got != 40 {
		t.Fatalf("unexpected nft1155 receiver balance: got=%d want=40", got)
	}
}

func TestParseDTLTxTypePoolSwapRoute(t *testing.T) {
	kind, err := parseDTLTxType("POOL_SWAP_ROUTE")
	if err != nil {
		t.Fatalf("parseDTLTxType failed: %v", err)
	}
	if kind != DTLTxPoolSwapRoute {
		t.Fatalf("unexpected tx kind: got=%s want=%s", kind, DTLTxPoolSwapRoute)
	}
}

func TestValidateDTLMintWithGovernanceCertThreshold(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 1000)
	authSigners := newDTLTestSigners(t, 3)

	tokenID, err := ApplyDTLCreateTx(ledger.DTL, ChainID, 1, DTLCreateTx{
		Creator:            "alice",
		Name:               "Mint Threshold",
		Symbol:             "MTH",
		Decimals:           18,
		MaxSupply:          1000,
		InitialSupply:      100,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 2,
	})
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	mintPayloadObj := DTLMintTx{
		Proposer: authSigners[0].Address,
		To:       "bob",
		TokenID:  tokenID,
		Amount:   50,
	}
	mintPayloadBytes, err := json.Marshal(mintPayloadObj)
	if err != nil {
		t.Fatalf("marshal mint payload failed: %v", err)
	}
	mintPayload := string(mintPayloadBytes)

	payloadHash, err := DTLPayloadHash(struct {
		TokenID string `json:"token_id"`
		To      string `json:"to"`
		Amount  uint64 `json:"amount"`
	}{
		TokenID: tokenID,
		To:      "bob",
		Amount:  50,
	})
	if err != nil {
		t.Fatalf("payload hash error: %v", err)
	}

	invalidCert := buildDTLCertForSigners(
		t,
		tokenID,
		10,
		DTLGovMint,
		payloadHash,
		authSigners[:1],
	)
	invalidCertJSON, err := json.Marshal(invalidCert)
	if err != nil {
		t.Fatalf("marshal invalid cert failed: %v", err)
	}

	invalidTx := Transaction{
		From:              authSigners[0].Address,
		To:                "dtl",
		Amount:            0,
		Nonce:             1,
		Fee:               requiredFeeForTx(Transaction{Type: TxDTL}),
		Coin:              CoinSymbol,
		Type:              TxDTL,
		DTLTxType:         string(DTLTxTokenMint),
		DTLTokenID:        tokenID,
		DTLPayload:        mintPayload,
		DTLGovernanceCert: string(invalidCertJSON),
	}
	if err := validateDTLTransaction(&ledger, invalidTx, 10); err == nil {
		t.Fatalf("expected threshold validation failure")
	}

	validCert := buildDTLCertForSigners(
		t,
		tokenID,
		11,
		DTLGovMint,
		payloadHash,
		authSigners[:2],
	)
	validCertJSON, err := json.Marshal(validCert)
	if err != nil {
		t.Fatalf("marshal valid cert failed: %v", err)
	}

	validTx := invalidTx
	validTx.DTLGovernanceCert = string(validCertJSON)
	if err := validateDTLTransaction(&ledger, validTx, 11); err != nil {
		t.Fatalf("expected valid cert, got: %v", err)
	}
}

func TestExecuteTransactionDTLPoolAndDuelPaths(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 10_000)
	addBalance(&ledger, CoinSymbol, "bob", 10_000)
	authSigners := newDTLTestSigners(t, 2)

	createABytes, err := json.Marshal(DTLCreateTx{
		Creator:            "alice",
		Name:               "Path Token A",
		Symbol:             "PTX",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      5_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("marshal token A failed: %v", err)
	}
	tx1 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: string(createABytes),
		ChainID:    ChainID,
	}
	tx1.Fee = requiredFeeForTx(tx1)
	next, err := ExecuteTransaction(&ledger, tx1, 1)
	if err != nil {
		t.Fatalf("execute token A failed: %v", err)
	}

	createBBytes, err := json.Marshal(DTLCreateTx{
		Creator:            "alice",
		Name:               "Path Token B",
		Symbol:             "PTY",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      5_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("marshal token B failed: %v", err)
	}
	tx2 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: string(createBBytes),
		ChainID:    ChainID,
	}
	tx2.Fee = requiredFeeForTx(tx2)
	next, err = ExecuteTransaction(&next, tx2, 2)
	if err != nil {
		t.Fatalf("execute token B failed: %v", err)
	}

	tokenA := next.DTL.SymbolIndex["PTX"]
	tokenB := next.DTL.SymbolIndex["PTY"]
	if tokenA == "" || tokenB == "" {
		t.Fatalf("token ids missing after create")
	}

	poolCreatePayload, err := json.Marshal(DTLPoolCreateTx{
		Creator: "alice",
		TokenA:  tokenA,
		TokenB:  tokenB,
		AmountA: 1000,
		AmountB: 1000,
	})
	if err != nil {
		t.Fatalf("marshal pool create failed: %v", err)
	}
	tx3 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      3,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxPoolCreate),
		DTLPayload: string(poolCreatePayload),
		ChainID:    ChainID,
	}
	tx3.Fee = requiredFeeForTx(tx3)
	next, err = ExecuteTransaction(&next, tx3, 3)
	if err != nil {
		t.Fatalf("execute pool create failed: %v", err)
	}
	poolID := next.DTL.PoolIndex[dtlPoolPairKey(tokenA, tokenB)]
	if poolID == "" {
		t.Fatalf("pool id not indexed")
	}

	transferPayload, err := json.Marshal(DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: tokenA,
		Amount:  200,
	})
	if err != nil {
		t.Fatalf("marshal transfer failed: %v", err)
	}
	tx4 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      4,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenTransfer),
		DTLTokenID: tokenA,
		DTLPayload: string(transferPayload),
		ChainID:    ChainID,
	}
	tx4.Fee = requiredFeeForTx(tx4)
	next, err = ExecuteTransaction(&next, tx4, 4)
	if err != nil {
		t.Fatalf("execute transfer failed: %v", err)
	}

	bobTokenBBefore := next.DTL.BalanceOf(tokenB, "bob")
	swapPayload, err := json.Marshal(DTLPoolSwapTx{
		Trader:       "bob",
		PoolID:       poolID,
		TokenIn:      tokenA,
		AmountIn:     50,
		MinAmountOut: 1,
	})
	if err != nil {
		t.Fatalf("marshal swap failed: %v", err)
	}
	tx5 := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxPoolSwap),
		DTLPayload: string(swapPayload),
		ChainID:    ChainID,
	}
	tx5.Fee = requiredFeeForTx(tx5)
	next, err = ExecuteTransaction(&next, tx5, 5)
	if err != nil {
		t.Fatalf("execute pool swap failed: %v", err)
	}
	if got := next.DTL.BalanceOf(tokenB, "bob"); got <= bobTokenBBefore {
		t.Fatalf("expected bob tokenB increase after swap: before=%d after=%d", bobTokenBBefore, got)
	}

	duelCreatePayload, err := json.Marshal(DTLDuelCreateTx{
		Creator:            "alice",
		TokenID:            tokenA,
		Stake:              50,
		CommitHash:         DTLDuelCommitHash("a-secret"),
		JoinExpiryBlocks:   5,
		RevealExpiryBlocks: 5,
	})
	if err != nil {
		t.Fatalf("marshal duel create failed: %v", err)
	}
	tx6 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      5,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxDuelCreate),
		DTLPayload: string(duelCreatePayload),
		ChainID:    ChainID,
	}
	tx6.Fee = requiredFeeForTx(tx6)
	next, err = ExecuteTransaction(&next, tx6, 6)
	if err != nil {
		t.Fatalf("execute duel create failed: %v", err)
	}
	if len(next.DTL.Duels) == 0 {
		t.Fatalf("expected duel to be created")
	}
}

func TestExecuteTransactionDTLLendingAndTournamentPaths(t *testing.T) {
	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, "alice", 20_000)
	addBalance(&ledger, CoinSymbol, "bob", 20_000)
	addBalance(&ledger, CoinSymbol, "carol", 20_000)
	authSigners := newDTLTestSigners(t, 2)

	createCollateralBytes, err := json.Marshal(DTLCreateTx{
		Creator:            "alice",
		Name:               "Exec Collateral",
		Symbol:             "EXC",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      20_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("marshal collateral token failed: %v", err)
	}
	tx1 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: string(createCollateralBytes),
		ChainID:    ChainID,
	}
	tx1.Fee = requiredFeeForTx(tx1)
	next, err := ExecuteTransaction(&ledger, tx1, 1)
	if err != nil {
		t.Fatalf("execute collateral token create failed: %v", err)
	}

	createDebtBytes, err := json.Marshal(DTLCreateTx{
		Creator:            "alice",
		Name:               "Exec Debt",
		Symbol:             "EXD",
		Decimals:           18,
		MaxSupply:          1_000_000,
		InitialSupply:      20_000,
		AuthoritySigners:   dtlSignerAddresses(authSigners),
		AuthorityThreshold: 1,
	})
	if err != nil {
		t.Fatalf("marshal debt token failed: %v", err)
	}
	tx2 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenCreate),
		DTLPayload: string(createDebtBytes),
		ChainID:    ChainID,
	}
	tx2.Fee = requiredFeeForTx(tx2)
	next, err = ExecuteTransaction(&next, tx2, 2)
	if err != nil {
		t.Fatalf("execute debt token create failed: %v", err)
	}

	collateralTokenID := next.DTL.SymbolIndex["EXC"]
	debtTokenID := next.DTL.SymbolIndex["EXD"]
	if collateralTokenID == "" || debtTokenID == "" {
		t.Fatalf("expected token ids")
	}

	transferCollateralToBob, err := json.Marshal(DTLTransferTx{
		From:    "alice",
		To:      "bob",
		TokenID: collateralTokenID,
		Amount:  1_000,
	})
	if err != nil {
		t.Fatalf("marshal transfer collateral failed: %v", err)
	}
	tx3 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      3,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTokenTransfer),
		DTLTokenID: collateralTokenID,
		DTLPayload: string(transferCollateralToBob),
		ChainID:    ChainID,
	}
	tx3.Fee = requiredFeeForTx(tx3)
	next, err = ExecuteTransaction(&next, tx3, 3)
	if err != nil {
		t.Fatalf("execute transfer collateral failed: %v", err)
	}

	lendMarketCreatePayload, err := json.Marshal(DTLLendMarketCreateTx{
		Creator:             "alice",
		CollateralTokenID:   collateralTokenID,
		DebtTokenID:         debtTokenID,
		DebtLiquidity:       5000,
		CollateralFactorBPS: 7000,
		LiquidationBonusBPS: 500,
	})
	if err != nil {
		t.Fatalf("marshal lend market create failed: %v", err)
	}
	tx4 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      4,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxLendMarketCreate),
		DTLPayload: string(lendMarketCreatePayload),
		ChainID:    ChainID,
	}
	tx4.Fee = requiredFeeForTx(tx4)
	next, err = ExecuteTransaction(&next, tx4, 4)
	if err != nil {
		t.Fatalf("execute lend market create failed: %v", err)
	}
	marketID := next.DTL.LendingIndex[dtlLendingPairKey(collateralTokenID, debtTokenID)]
	if marketID == "" {
		t.Fatalf("expected lending market id")
	}

	lendDepositPayload, err := json.Marshal(DTLLendDepositCollateralTx{
		Account:  "bob",
		MarketID: marketID,
		Amount:   500,
	})
	if err != nil {
		t.Fatalf("marshal lend deposit failed: %v", err)
	}
	tx5 := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      1,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxLendDeposit),
		DTLPayload: string(lendDepositPayload),
		ChainID:    ChainID,
	}
	tx5.Fee = requiredFeeForTx(tx5)
	next, err = ExecuteTransaction(&next, tx5, 5)
	if err != nil {
		t.Fatalf("execute lend deposit failed: %v", err)
	}

	lendBorrowPayload, err := json.Marshal(DTLLendBorrowTx{
		Account:  "bob",
		MarketID: marketID,
		Amount:   200,
	})
	if err != nil {
		t.Fatalf("marshal lend borrow failed: %v", err)
	}
	tx6 := Transaction{
		From:       "bob",
		To:         "dtl",
		Amount:     0,
		Nonce:      2,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxLendBorrow),
		DTLPayload: string(lendBorrowPayload),
		ChainID:    ChainID,
	}
	tx6.Fee = requiredFeeForTx(tx6)
	next, err = ExecuteTransaction(&next, tx6, 6)
	if err != nil {
		t.Fatalf("execute lend borrow failed: %v", err)
	}
	if got := next.DTL.LendingPositions[dtlLendingPositionKey(marketID, "bob")]; got == nil || got.Debt == 0 {
		t.Fatalf("expected active lending debt position")
	}

	tournamentCreatePayload, err := json.Marshal(DTLTournamentCreateTx{
		Creator:            "alice",
		TokenID:            collateralTokenID,
		EntryFee:           50,
		MaxPlayers:         3,
		JoinExpiryBlocks:   5,
		RevealExpiryBlocks: 5,
	})
	if err != nil {
		t.Fatalf("marshal tournament create failed: %v", err)
	}
	tx7 := Transaction{
		From:       "alice",
		To:         "dtl",
		Amount:     0,
		Nonce:      5,
		Coin:       CoinSymbol,
		Type:       TxDTL,
		DTLTxType:  string(DTLTxTournamentCreate),
		DTLPayload: string(tournamentCreatePayload),
		ChainID:    ChainID,
	}
	tx7.Fee = requiredFeeForTx(tx7)
	next, err = ExecuteTransaction(&next, tx7, 7)
	if err != nil {
		t.Fatalf("execute tournament create failed: %v", err)
	}
	if len(next.DTL.Tournaments) == 0 {
		t.Fatalf("expected tournament to be created")
	}

	// Contract runtime removed: CONTRACT_DEPLOY / CONTRACT_CALL coverage lives in
	// dedicated rejection tests.
}
