package main

import (
	"fmt"
	"strings"
)

const parallelDTLBatchMin = 2

type parallelDTLExecutionPlan struct {
	tx              Transaction
	requiredFee     int
	coin            string
	kind            DTLTxType
	height          uint64
	tokenID         string
	from            string
	to              string
	owner           string
	spender         string
	collectionID    string
	nftTokenID      uint64
	symbol          string
	createTx        *DTLCreateTx
	nft721CreateTx  *DTLNFT721CreateTx
	nft721MintTx    *DTLNFT721MintTx
	nft1155CreateTx *DTLNFT1155CreateTx
	nft1155MintTx   *DTLNFT1155MintTx
	poolCreateTx    *DTLPoolCreateTx
	poolAddTx       *DTLPoolAddLiquidityTx
	poolRemoveTx    *DTLPoolRemoveLiquidityTx
	poolSwapTx      *DTLPoolSwapTx
	oracleFeedTx    *DTLOracleFeedCreateTx
	oraclePriceTx   *DTLOraclePriceSubmitTx
	feedID          string
	poolID          string
	pairKey         string
	tokenA          string
	tokenB          string
	amount          uint64
	tax             uint64
	net             uint64
	fromKey         string
	toKey           string
	treasuryKey     string
	allowanceKey    string
	tokenSupplyKey  string
	writeKeys       []string
	event           string
}

type parallelDTLExecutionResult struct {
	index int
	patch *parallelDTLStatePatch
	err   error
}

type parallelDTLStatePatch struct {
	state *DTLState
}

func tryApplyParallelDTLBatch(
	block Block,
	txs []Transaction,
	start int,
	ledger *Ledger,
	totalFees map[string]int,
) (int, error) {
	if ledger == nil || start < 0 || start >= len(txs) {
		return 0, nil
	}
	if len(txs)-start < parallelDTLBatchMin {
		return 0, nil
	}
	first := txs[start]
	normalizeIncomingTx(&first)
	if first.Type != TxDTL {
		return 0, nil
	}
	firstKind, err := parseDTLTxType(first.DTLTxType)
	if err != nil || !isParallelDTLSupportedKind(firstKind) {
		return 0, nil
	}
	ensureDTLState(ledger)

	snapshot := ledger.Clone()
	plans := make([]parallelDTLExecutionPlan, 0, len(txs)-start)
	seenCoinSenders := make(map[string]struct{})
	seenDTLWrites := make(map[string]struct{})

	for i := start; i < len(txs); i++ {
		plan, ok := parallelDTLPlanFromTx(snapshot, txs[i], block.ID)
		if !ok {
			break
		}
		if parallelDTLPlanConflicts(plan, seenCoinSenders, seenDTLWrites) {
			break
		}
		plans = append(plans, plan)
	}
	if len(plans) < parallelDTLBatchMin {
		return 0, nil
	}

	results := executeParallelDTLExecutionPlans(snapshot.DTL, plans, 0)
	orderedResults := make([]parallelDTLExecutionResult, len(plans))
	seenResults := make([]bool, len(plans))
	for _, result := range results {
		if result.index < 0 || result.index >= len(plans) {
			return 0, fmt.Errorf("dtl execution failed: invalid parallel result index")
		}
		if seenResults[result.index] {
			return 0, fmt.Errorf("dtl execution failed: duplicate parallel result index")
		}
		seenResults[result.index] = true
		orderedResults[result.index] = result
		if result.err != nil {
			return 0, fmt.Errorf("invalid dtl payload: %w", result.err)
		}
		plan := plans[result.index]
		if getBalance(*ledger, plan.coin, plan.tx.From) < plan.requiredFee {
			return 0, fmt.Errorf("insufficient balance: %s", plan.tx.From)
		}
	}

	for i, plan := range plans {
		if !seenResults[i] {
			return 0, fmt.Errorf("dtl execution failed: missing parallel result index")
		}
		if err := mergeParallelDTLStatePatch(ledger.DTL, plan, orderedResults[i].patch); err != nil {
			return 0, fmt.Errorf("dtl execution failed: %w", err)
		}
		addBalance(ledger, plan.coin, plan.tx.From, -plan.requiredFee)
		addBalance(ledger, plan.coin, TREASURY_ADDRESS, plan.requiredFee)
		setNonce(ledger, plan.tx.From, plan.tx.Nonce)
		totalFees[plan.coin] += plan.requiredFee
	}
	return len(plans), nil
}

func isParallelDTLSupportedKind(kind DTLTxType) bool {
	return kind == DTLTxTokenCreate ||
		kind == DTLTxTokenTransfer ||
		kind == DTLTxTokenApprove ||
		kind == DTLTxTokenTransferFrom ||
		kind == DTLTxTokenBurn ||
		kind == DTLTxNFT721Create ||
		kind == DTLTxNFT721Mint ||
		kind == DTLTxNFT721Transfer ||
		kind == DTLTxNFT1155Create ||
		kind == DTLTxNFT1155Mint ||
		kind == DTLTxNFT1155Transfer ||
		kind == DTLTxPoolCreate ||
		kind == DTLTxPoolAdd ||
		kind == DTLTxPoolRemove ||
		kind == DTLTxPoolSwap ||
		kind == DTLTxOracleFeedCreate ||
		kind == DTLTxOraclePriceSubmit
}

func parallelDTLPlanFromTx(ledger Ledger, tx Transaction, height uint64) (parallelDTLExecutionPlan, bool) {
	normalizeIncomingTx(&tx)
	if tx.Type != TxDTL {
		return parallelDTLExecutionPlan{}, false
	}
	kind, err := parseDTLTxType(tx.DTLTxType)
	if err != nil || !isParallelDTLSupportedKind(kind) {
		return parallelDTLExecutionPlan{}, false
	}
	if !isProtocolChainID(tx.ChainID) {
		return parallelDTLExecutionPlan{}, false
	}
	coin := normalizeCoin(tx.Coin)
	if !isProtocolCoinAllowed(coin) {
		return parallelDTLExecutionPlan{}, false
	}
	requiredFee := requiredFeeForTx(tx)
	if err := validateDTLFeeBounds(tx.Fee, requiredFee); err != nil {
		return parallelDTLExecutionPlan{}, false
	}
	if tx.Nonce != getNonce(ledger, tx.From)+1 {
		return parallelDTLExecutionPlan{}, false
	}
	if balanceKey(coin, tx.From) == balanceKey(coin, TREASURY_ADDRESS) {
		return parallelDTLExecutionPlan{}, false
	}

	decoded, err := decodeDTLTransaction(tx)
	if err != nil {
		return parallelDTLExecutionPlan{}, false
	}
	if ledger.DTL == nil {
		return parallelDTLExecutionPlan{}, false
	}

	plan := parallelDTLExecutionPlan{
		tx:          tx,
		requiredFee: requiredFee,
		coin:        coin,
		kind:        kind,
		height:      height,
	}

	switch kind {
	case DTLTxTokenCreate:
		if decoded.Create == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLCreatePlan(&plan, ledger.DTL, tx, *decoded.Create) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxTokenTransfer:
		if decoded.Transfer == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLTransferPlan(&plan, ledger.DTL, *decoded.Transfer) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxTokenApprove:
		if decoded.Approve == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLApprovePlan(&plan, ledger.DTL, *decoded.Approve) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxTokenTransferFrom:
		if decoded.TransferFrom == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLTransferFromPlan(&plan, ledger.DTL, *decoded.TransferFrom) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxTokenBurn:
		if decoded.Burn == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLBurnPlan(&plan, ledger.DTL, *decoded.Burn) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxNFT721Create:
		if decoded.NFT721Create == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLNFT721CreatePlan(&plan, ledger.DTL, tx, *decoded.NFT721Create) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxNFT721Mint:
		if decoded.NFT721Mint == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLNFT721MintPlan(&plan, ledger.DTL, *decoded.NFT721Mint) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxNFT721Transfer:
		if decoded.NFT721Transfer == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLNFT721TransferPlan(&plan, ledger.DTL, *decoded.NFT721Transfer) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxNFT1155Transfer:
		if decoded.NFT1155Transfer == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLNFT1155TransferPlan(&plan, ledger.DTL, *decoded.NFT1155Transfer) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxNFT1155Create:
		if decoded.NFT1155Create == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLNFT1155CreatePlan(&plan, ledger.DTL, tx, *decoded.NFT1155Create) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxNFT1155Mint:
		if decoded.NFT1155Mint == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLNFT1155MintPlan(&plan, ledger.DTL, *decoded.NFT1155Mint) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxPoolCreate:
		if decoded.PoolCreate == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLPoolCreatePlan(&plan, ledger.DTL, tx, *decoded.PoolCreate) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxPoolAdd:
		if decoded.PoolAdd == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLPoolAddPlan(&plan, ledger.DTL, *decoded.PoolAdd) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxPoolRemove:
		if decoded.PoolRemove == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLPoolRemovePlan(&plan, ledger.DTL, *decoded.PoolRemove) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxPoolSwap:
		if decoded.PoolSwap == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLPoolSwapPlan(&plan, ledger.DTL, *decoded.PoolSwap) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxOracleFeedCreate:
		if decoded.OracleFeedCreate == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLOracleFeedCreatePlan(&plan, ledger.DTL, tx, *decoded.OracleFeedCreate) {
			return parallelDTLExecutionPlan{}, false
		}
	case DTLTxOraclePriceSubmit:
		if decoded.OraclePriceSubmit == nil {
			return parallelDTLExecutionPlan{}, false
		}
		if !populateParallelDTLOraclePriceSubmitPlan(&plan, ledger.DTL, *decoded.OraclePriceSubmit) {
			return parallelDTLExecutionPlan{}, false
		}
	default:
		return parallelDTLExecutionPlan{}, false
	}
	return plan, true
}

func populateParallelDTLCreatePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx Transaction, create DTLCreateTx) bool {
	chainID, err := canonicalDTLTransactionChainID(tx)
	if err != nil {
		return false
	}
	tokenID := normalizeDTLTokenID(DTLTokenIDFromCreate(chainID, create, uint64(tx.Nonce)))
	symbol := normalizeDTLSymbol(create.Symbol)
	creator := normalizeDTLAccount(create.Creator)
	if tokenID == "" || symbol == "" || creator == "" {
		return false
	}
	if _, exists := state.SymbolIndex[symbol]; state.Tokens[tokenID] != nil || exists {
		return false
	}
	plan.tokenID = tokenID
	plan.symbol = symbol
	plan.from = creator
	plan.createTx = &create
	plan.writeKeys = []string{
		dtlWriteKey("token", tokenID),
		dtlWriteKey("symbol", symbol),
	}
	if create.InitialSupply > 0 {
		plan.fromKey = dtlBalanceKey(tokenID, creator)
		plan.writeKeys = append(plan.writeKeys, dtlWriteKey("balance", plan.fromKey))
	}
	plan.event = fmt.Sprintf("TOKEN_CREATE:%s", tokenID)
	return true
}

func populateParallelDTLTransferPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLTransferTx) bool {
	tokenID := normalizeDTLTokenID(tx.TokenID)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if tokenID == "" || from == "" || to == "" {
		return false
	}
	token := state.Tokens[tokenID]
	if token == nil {
		return false
	}
	amount := tx.Amount
	tax := uint64(0)
	if token.TaxBPS > 0 {
		tax = (amount * uint64(token.TaxBPS)) / DTLMaxTaxBPS
	}
	net := amount - tax
	fromKey := dtlBalanceKey(tokenID, from)
	toKey := dtlBalanceKey(tokenID, to)
	treasuryKey := ""
	writeKeys := []string{dtlWriteKey("balance", fromKey), dtlWriteKey("balance", toKey)}
	if tax > 0 {
		treasuryKey = dtlBalanceKey(tokenID, DTLTreasuryAccount)
		writeKeys = append(writeKeys, dtlWriteKey("balance", treasuryKey))
	}
	plan.tokenID = tokenID
	plan.from = from
	plan.to = to
	plan.amount = amount
	plan.tax = tax
	plan.net = net
	plan.fromKey = fromKey
	plan.toKey = toKey
	plan.treasuryKey = treasuryKey
	plan.writeKeys = writeKeys
	plan.event = fmt.Sprintf("TOKEN_TRANSFER:%s:%s->%s:%d", tokenID, from, to, amount)
	return true
}

func populateParallelDTLApprovePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLApproveTx) bool {
	tokenID := normalizeDTLTokenID(tx.TokenID)
	owner := normalizeDTLAccount(tx.Owner)
	spender := normalizeDTLAccount(tx.Spender)
	if tokenID == "" || owner == "" || spender == "" {
		return false
	}
	if state.Tokens[tokenID] == nil {
		return false
	}
	allowanceKey := dtlAllowanceKey(tokenID, owner, spender)
	plan.tokenID = tokenID
	plan.owner = owner
	plan.spender = spender
	plan.amount = tx.Amount
	plan.allowanceKey = allowanceKey
	plan.writeKeys = []string{dtlWriteKey("allowance", allowanceKey)}
	plan.event = fmt.Sprintf("TOKEN_APPROVE:%s:%s->%s:%d", tokenID, owner, spender, tx.Amount)
	return true
}

func populateParallelDTLTransferFromPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLTransferFromTx) bool {
	tokenID := normalizeDTLTokenID(tx.TokenID)
	spender := normalizeDTLAccount(tx.Spender)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if tokenID == "" || spender == "" || from == "" || to == "" {
		return false
	}
	token := state.Tokens[tokenID]
	if token == nil {
		return false
	}
	amount := tx.Amount
	tax := uint64(0)
	if token.TaxBPS > 0 {
		tax = (amount * uint64(token.TaxBPS)) / DTLMaxTaxBPS
	}
	net := amount - tax
	allowanceKey := dtlAllowanceKey(tokenID, from, spender)
	fromKey := dtlBalanceKey(tokenID, from)
	toKey := dtlBalanceKey(tokenID, to)
	treasuryKey := ""
	writeKeys := []string{
		dtlWriteKey("allowance", allowanceKey),
		dtlWriteKey("balance", fromKey),
		dtlWriteKey("balance", toKey),
	}
	if tax > 0 {
		treasuryKey = dtlBalanceKey(tokenID, DTLTreasuryAccount)
		writeKeys = append(writeKeys, dtlWriteKey("balance", treasuryKey))
	}
	plan.tokenID = tokenID
	plan.spender = spender
	plan.from = from
	plan.to = to
	plan.amount = amount
	plan.tax = tax
	plan.net = net
	plan.allowanceKey = allowanceKey
	plan.fromKey = fromKey
	plan.toKey = toKey
	plan.treasuryKey = treasuryKey
	plan.writeKeys = writeKeys
	plan.event = fmt.Sprintf("TOKEN_TRANSFER_FROM:%s:%s->%s:by=%s:%d", tokenID, from, to, spender, amount)
	return true
}

func populateParallelDTLBurnPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLBurnTx) bool {
	tokenID := normalizeDTLTokenID(tx.TokenID)
	from := normalizeDTLAccount(tx.From)
	if tokenID == "" || from == "" {
		return false
	}
	if state.Tokens[tokenID] == nil {
		return false
	}
	fromKey := dtlBalanceKey(tokenID, from)
	plan.tokenID = tokenID
	plan.from = from
	plan.amount = tx.Amount
	plan.fromKey = fromKey
	plan.tokenSupplyKey = tokenID
	plan.writeKeys = []string{
		dtlWriteKey("balance", fromKey),
		dtlWriteKey("token_supply", tokenID),
	}
	plan.event = fmt.Sprintf("TOKEN_BURN:%s:%s:%d", tokenID, from, tx.Amount)
	return true
}

func populateParallelDTLNFT721TransferPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLNFT721TransferTx) bool {
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if collectionID == "" || from == "" || to == "" {
		return false
	}
	if state.NFT721Collections[collectionID] == nil {
		return false
	}
	ownerKey := dtlNFT721OwnerKey(collectionID, tx.TokenID)
	plan.collectionID = collectionID
	plan.nftTokenID = tx.TokenID
	plan.from = from
	plan.to = to
	plan.fromKey = ownerKey
	plan.writeKeys = []string{dtlWriteKey("nft721_owner", ownerKey)}
	plan.event = fmt.Sprintf("NFT721_TRANSFER:%s:%d:%s->%s", collectionID, tx.TokenID, from, to)
	return true
}

func populateParallelDTLNFT721CreatePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx Transaction, create DTLNFT721CreateTx) bool {
	chainID, err := canonicalDTLTransactionChainID(tx)
	if err != nil {
		return false
	}
	collectionID := normalizeDTLCollectionID(DTLNFT721CollectionIDFromCreate(chainID, create, uint64(tx.Nonce)))
	symbol := normalizeDTLSymbol(create.Symbol)
	creator := normalizeDTLAccount(create.Creator)
	if collectionID == "" || symbol == "" || creator == "" {
		return false
	}
	if _, exists := state.NFT721SymbolIndex[symbol]; state.NFT721Collections[collectionID] != nil || exists {
		return false
	}
	plan.collectionID = collectionID
	plan.symbol = symbol
	plan.from = creator
	plan.nft721CreateTx = &create
	plan.writeKeys = []string{
		dtlWriteKey("nft721_collection", collectionID),
		dtlWriteKey("nft721_symbol", symbol),
	}
	plan.event = fmt.Sprintf("NFT721_CREATE:%s", collectionID)
	return true
}

func populateParallelDTLNFT721MintPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLNFT721MintTx) bool {
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	creator := normalizeDTLAccount(tx.Creator)
	to := normalizeDTLAccount(tx.To)
	if collectionID == "" || creator == "" || to == "" {
		return false
	}
	collection := state.NFT721Collections[collectionID]
	if collection == nil || collection.NextTokenID == ^uint64(0) {
		return false
	}
	tokenID := collection.NextTokenID + 1
	ownerKey := dtlNFT721OwnerKey(collectionID, tokenID)
	plan.collectionID = collectionID
	plan.nftTokenID = tokenID
	plan.from = creator
	plan.to = to
	plan.fromKey = ownerKey
	plan.nft721MintTx = &tx
	plan.writeKeys = []string{
		dtlWriteKey("nft721_collection_counter", collectionID),
		dtlWriteKey("nft721_owner", ownerKey),
	}
	if strings.TrimSpace(tx.TokenURI) != "" {
		plan.writeKeys = append(plan.writeKeys, dtlWriteKey("nft721_uri", ownerKey))
	}
	plan.event = fmt.Sprintf("NFT721_MINT:%s:%d:%s", collectionID, tokenID, to)
	return true
}

func populateParallelDTLNFT1155TransferPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLNFT1155TransferTx) bool {
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if collectionID == "" || from == "" || to == "" {
		return false
	}
	if state.NFT1155Collections[collectionID] == nil {
		return false
	}
	fromKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, from)
	toKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, to)
	plan.collectionID = collectionID
	plan.nftTokenID = tx.TokenID
	plan.from = from
	plan.to = to
	plan.amount = tx.Amount
	plan.fromKey = fromKey
	plan.toKey = toKey
	plan.writeKeys = []string{
		dtlWriteKey("nft1155_balance", fromKey),
		dtlWriteKey("nft1155_balance", toKey),
	}
	plan.event = fmt.Sprintf("NFT1155_TRANSFER:%s:%d:%s->%s:%d", collectionID, tx.TokenID, from, to, tx.Amount)
	return true
}

func populateParallelDTLNFT1155CreatePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx Transaction, create DTLNFT1155CreateTx) bool {
	chainID, err := canonicalDTLTransactionChainID(tx)
	if err != nil {
		return false
	}
	collectionID := normalizeDTLCollectionID(DTLNFT1155CollectionIDFromCreate(chainID, create, uint64(tx.Nonce)))
	symbol := normalizeDTLSymbol(create.Symbol)
	creator := normalizeDTLAccount(create.Creator)
	if collectionID == "" || symbol == "" || creator == "" {
		return false
	}
	if _, exists := state.NFT1155SymbolIndex[symbol]; state.NFT1155Collections[collectionID] != nil || exists {
		return false
	}
	plan.collectionID = collectionID
	plan.symbol = symbol
	plan.from = creator
	plan.nft1155CreateTx = &create
	plan.writeKeys = []string{
		dtlWriteKey("nft1155_collection", collectionID),
		dtlWriteKey("nft1155_symbol", symbol),
	}
	plan.event = fmt.Sprintf("NFT1155_CREATE:%s", collectionID)
	return true
}

func populateParallelDTLNFT1155MintPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLNFT1155MintTx) bool {
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	creator := normalizeDTLAccount(tx.Creator)
	to := normalizeDTLAccount(tx.To)
	if collectionID == "" || creator == "" || to == "" {
		return false
	}
	if state.NFT1155Collections[collectionID] == nil {
		return false
	}
	balanceKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, to)
	supplyKey := dtlNFT1155SupplyKey(collectionID, tx.TokenID)
	plan.collectionID = collectionID
	plan.nftTokenID = tx.TokenID
	plan.from = creator
	plan.to = to
	plan.amount = tx.Amount
	plan.fromKey = balanceKey
	plan.toKey = supplyKey
	plan.nft1155MintTx = &tx
	plan.writeKeys = []string{
		dtlWriteKey("nft1155_balance", balanceKey),
		dtlWriteKey("nft1155_supply", supplyKey),
	}
	plan.event = fmt.Sprintf("NFT1155_MINT:%s:%d:%s:%d", collectionID, tx.TokenID, to, tx.Amount)
	return true
}

func populateParallelDTLPoolCreatePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx Transaction, create DTLPoolCreateTx) bool {
	chainID, err := canonicalDTLTransactionChainID(tx)
	if err != nil {
		return false
	}
	creator := normalizeDTLAccount(create.Creator)
	tokenA, tokenB, _, _ := canonicalizeDTLPoolPair(create.TokenA, create.TokenB, create.AmountA, create.AmountB)
	if creator == "" || tokenA == "" || tokenB == "" || tokenA == tokenB {
		return false
	}
	pairKey := dtlPoolPairKey(tokenA, tokenB)
	poolID := normalizeDTLPoolID(DTLPoolIDFromTokens(chainID, tokenA, tokenB))
	if poolID == "" || state.PoolIndex[pairKey] != "" || state.Pools[poolID] != nil {
		return false
	}
	vault := dtlPoolVaultAccount(poolID)
	lpKey := dtlLPBalanceKey(poolID, creator)
	plan.poolID = poolID
	plan.pairKey = pairKey
	plan.from = creator
	plan.tokenA = tokenA
	plan.tokenB = tokenB
	plan.poolCreateTx = &create
	plan.writeKeys = []string{
		dtlWriteKey("pool", poolID),
		dtlWriteKey("pool_pair", pairKey),
		dtlWriteKey("pool_twap", poolID),
		dtlWriteKey("lp_balance", lpKey),
		dtlWriteKey("balance", dtlBalanceKey(tokenA, creator)),
		dtlWriteKey("balance", dtlBalanceKey(tokenA, vault)),
		dtlWriteKey("balance", dtlBalanceKey(tokenB, creator)),
		dtlWriteKey("balance", dtlBalanceKey(tokenB, vault)),
	}
	plan.event = fmt.Sprintf("POOL_CREATE:%s:%s/%s", poolID, tokenA, tokenB)
	return true
}

func populateParallelDTLPoolAddPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLPoolAddLiquidityTx) bool {
	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	provider := normalizeDTLAccount(tx.Provider)
	if pool == nil || provider == "" {
		return false
	}
	vault := dtlPoolVaultAccount(poolID)
	lpKey := dtlLPBalanceKey(poolID, provider)
	plan.poolID = poolID
	plan.from = provider
	plan.tokenA = pool.TokenA
	plan.tokenB = pool.TokenB
	plan.poolAddTx = &tx
	plan.writeKeys = []string{
		dtlWriteKey("pool", poolID),
		dtlWriteKey("pool_twap", poolID),
		dtlWriteKey("lp_balance", lpKey),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenA, provider)),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenA, vault)),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenB, provider)),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenB, vault)),
	}
	return true
}

func populateParallelDTLPoolRemovePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLPoolRemoveLiquidityTx) bool {
	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	provider := normalizeDTLAccount(tx.Provider)
	if pool == nil || provider == "" {
		return false
	}
	vault := dtlPoolVaultAccount(poolID)
	lpKey := dtlLPBalanceKey(poolID, provider)
	plan.poolID = poolID
	plan.from = provider
	plan.tokenA = pool.TokenA
	plan.tokenB = pool.TokenB
	plan.poolRemoveTx = &tx
	plan.writeKeys = []string{
		dtlWriteKey("pool", poolID),
		dtlWriteKey("pool_twap", poolID),
		dtlWriteKey("lp_balance", lpKey),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenA, provider)),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenA, vault)),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenB, provider)),
		dtlWriteKey("balance", dtlBalanceKey(pool.TokenB, vault)),
	}
	return true
}

func populateParallelDTLPoolSwapPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLPoolSwapTx) bool {
	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	trader := normalizeDTLAccount(tx.Trader)
	tokenIn := normalizeDTLTokenID(tx.TokenIn)
	if pool == nil || trader == "" || (tokenIn != pool.TokenA && tokenIn != pool.TokenB) {
		return false
	}
	if pool.ProtocolFeeBPS != 0 {
		return false
	}
	tokenOut := pool.TokenB
	if tokenIn == pool.TokenB {
		tokenOut = pool.TokenA
	}
	vault := dtlPoolVaultAccount(poolID)
	plan.poolID = poolID
	plan.from = trader
	plan.tokenID = tokenIn
	plan.tokenA = pool.TokenA
	plan.tokenB = pool.TokenB
	plan.poolSwapTx = &tx
	plan.writeKeys = []string{
		dtlWriteKey("pool", poolID),
		dtlWriteKey("pool_twap", poolID),
		dtlWriteKey("balance", dtlBalanceKey(tokenIn, trader)),
		dtlWriteKey("balance", dtlBalanceKey(tokenIn, vault)),
		dtlWriteKey("balance", dtlBalanceKey(tokenOut, trader)),
		dtlWriteKey("balance", dtlBalanceKey(tokenOut, vault)),
	}
	return true
}

func populateParallelDTLOracleFeedCreatePlan(plan *parallelDTLExecutionPlan, state *DTLState, tx Transaction, create DTLOracleFeedCreateTx) bool {
	chainID, err := canonicalDTLTransactionChainID(tx)
	if err != nil {
		return false
	}
	feedID := normalizeDTLTokenID(dtlOracleFeedIDFromCreate(chainID, uint64(tx.Nonce), create))
	if feedID == "" || state.OracleFeeds[feedID] != nil {
		return false
	}
	plan.feedID = feedID
	plan.tokenA = normalizeDTLTokenID(create.BaseTokenID)
	plan.tokenB = normalizeDTLTokenID(create.QuoteTokenID)
	plan.oracleFeedTx = &create
	plan.writeKeys = []string{dtlWriteKey("oracle_feed", feedID)}
	plan.event = fmt.Sprintf("ORACLE_FEED_CREATE:%s", feedID)
	return true
}

func populateParallelDTLOraclePriceSubmitPlan(plan *parallelDTLExecutionPlan, state *DTLState, tx DTLOraclePriceSubmitTx) bool {
	feedID := normalizeDTLTokenID(tx.FeedID)
	submitter := normalizeDTLAccount(tx.Submitter)
	if feedID == "" || submitter == "" || state.OracleFeeds[feedID] == nil {
		return false
	}
	plan.feedID = feedID
	plan.from = submitter
	plan.amount = tx.Price
	plan.oraclePriceTx = &tx
	plan.writeKeys = []string{
		dtlWriteKey("oracle_feed", feedID),
		dtlWriteKey("oracle_sample", feedID+"|"+submitter),
	}
	plan.event = fmt.Sprintf("ORACLE_PRICE_SUBMIT:%s:%s:%d", feedID, submitter, tx.Price)
	return true
}

func dtlWriteKey(namespace string, key string) string {
	return namespace + ":" + key
}

func parallelDTLPlanConflicts(
	plan parallelDTLExecutionPlan,
	seenCoinSenders map[string]struct{},
	seenDTLWrites map[string]struct{},
) bool {
	coinSenderKey := balanceKey(plan.coin, plan.tx.From)
	if _, exists := seenCoinSenders[coinSenderKey]; exists {
		return true
	}
	for _, key := range plan.writeKeys {
		if key == "" {
			return true
		}
		if _, exists := seenDTLWrites[key]; exists {
			return true
		}
	}
	seenCoinSenders[coinSenderKey] = struct{}{}
	for _, key := range plan.writeKeys {
		seenDTLWrites[key] = struct{}{}
	}
	return false
}

func validateParallelDTLExecutionPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	switch plan.kind {
	case DTLTxTokenCreate:
		return validateParallelDTLCreatePlan(state, plan)
	case DTLTxTokenTransfer:
		return validateParallelDTLTransferPlan(state, plan)
	case DTLTxTokenApprove:
		return validateParallelDTLApprovePlan(state, plan)
	case DTLTxTokenTransferFrom:
		return validateParallelDTLTransferFromPlan(state, plan)
	case DTLTxTokenBurn:
		return validateParallelDTLBurnPlan(state, plan)
	case DTLTxNFT721Create:
		return validateParallelDTLNFT721CreatePlan(state, plan)
	case DTLTxNFT721Mint:
		return validateParallelDTLNFT721MintPlan(state, plan)
	case DTLTxNFT721Transfer:
		return validateParallelDTLNFT721TransferPlan(state, plan)
	case DTLTxNFT1155Create:
		return validateParallelDTLNFT1155CreatePlan(state, plan)
	case DTLTxNFT1155Mint:
		return validateParallelDTLNFT1155MintPlan(state, plan)
	case DTLTxNFT1155Transfer:
		return validateParallelDTLNFT1155TransferPlan(state, plan)
	case DTLTxPoolCreate:
		return validateParallelDTLPoolCreatePlan(state, plan)
	case DTLTxPoolAdd:
		return validateParallelDTLPoolAddPlan(state, plan)
	case DTLTxPoolRemove:
		return validateParallelDTLPoolRemovePlan(state, plan)
	case DTLTxPoolSwap:
		return validateParallelDTLPoolSwapPlan(state, plan)
	case DTLTxOracleFeedCreate:
		return validateParallelDTLOracleFeedCreatePlan(state, plan)
	case DTLTxOraclePriceSubmit:
		return validateParallelDTLOraclePriceSubmitPlan(state, plan)
	default:
		return fmt.Errorf("dtl: unsupported parallel tx kind")
	}
}

func validateParallelDTLCreatePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.createTx == nil {
		return ErrDTLInvalidState
	}
	tx := *plan.createTx
	if strings.TrimSpace(tx.Name) == "" || len(strings.TrimSpace(tx.Name)) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid name length")
	}
	if plan.symbol == "" || len(plan.symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid symbol length")
	}
	if _, exists := state.SymbolIndex[plan.symbol]; exists {
		return fmt.Errorf("dtl: symbol already exists: %s", plan.symbol)
	}
	if tx.Decimals > DTLMaxDecimals {
		return fmt.Errorf("dtl: decimals exceeds max %d", DTLMaxDecimals)
	}
	if tx.MaxSupply == 0 {
		return fmt.Errorf("dtl: max supply must be > 0")
	}
	if tx.InitialSupply > tx.MaxSupply {
		return fmt.Errorf("dtl: initial supply exceeds max supply")
	}
	if tx.TaxBPS > DTLMaxTaxBPS {
		return fmt.Errorf("dtl: tax bps exceeds %d", DTLMaxTaxBPS)
	}
	if tx.AuthorityThreshold == 0 {
		return fmt.Errorf("dtl: authority threshold must be > 0")
	}
	seen := make(map[string]struct{})
	for _, signer := range tx.AuthoritySigners {
		n := normalizeDTLAccount(signer)
		if n == "" {
			return fmt.Errorf("dtl: empty authority signer")
		}
		if !isMSCAddressLike(n) {
			return fmt.Errorf("dtl: authority signer must be an MSC address: %s", n)
		}
		if _, exists := seen[n]; exists {
			return fmt.Errorf("dtl: duplicate authority signer: %s", n)
		}
		seen[n] = struct{}{}
	}
	if len(seen) == 0 {
		return fmt.Errorf("dtl: authority signer set is empty")
	}
	if int(tx.AuthorityThreshold) > len(seen) {
		return fmt.Errorf("dtl: threshold %d exceeds signer set %d", tx.AuthorityThreshold, len(seen))
	}
	if state.Tokens[plan.tokenID] != nil {
		return fmt.Errorf("dtl: token id collision")
	}
	return nil
}

func validateParallelDTLTransferPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	token, exists := state.Tokens[plan.tokenID]
	if !exists || token == nil {
		return ErrDTLUnknownToken
	}
	if plan.amount == 0 {
		return fmt.Errorf("dtl: transfer amount must be > 0")
	}
	if plan.from == "" || plan.to == "" {
		return fmt.Errorf("dtl: invalid transfer account")
	}
	if plan.from == plan.to {
		return fmt.Errorf("dtl: self transfer not allowed")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled {
		if frozenByToken := state.FrozenAccounts[plan.tokenID]; frozenByToken != nil && frozenByToken[plan.from] {
			return ErrDTLFrozen
		}
	}
	if state.Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	return nil
}

func validateParallelDTLApprovePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	token, exists := state.Tokens[plan.tokenID]
	if !exists || token == nil {
		return ErrDTLUnknownToken
	}
	if plan.owner == "" || plan.spender == "" {
		return fmt.Errorf("dtl: invalid approve account")
	}
	if plan.owner == plan.spender {
		return fmt.Errorf("dtl: approve to self is not allowed")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled {
		if frozenByToken := state.FrozenAccounts[plan.tokenID]; frozenByToken != nil && frozenByToken[plan.owner] {
			return ErrDTLFrozen
		}
	}
	return nil
}

func validateParallelDTLTransferFromPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	token, exists := state.Tokens[plan.tokenID]
	if !exists || token == nil {
		return ErrDTLUnknownToken
	}
	if plan.amount == 0 {
		return fmt.Errorf("dtl: transfer amount must be > 0")
	}
	if plan.spender == "" || plan.from == "" || plan.to == "" {
		return fmt.Errorf("dtl: invalid transfer account")
	}
	if plan.from == plan.to {
		return fmt.Errorf("dtl: self transfer not allowed")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled {
		if frozenByToken := state.FrozenAccounts[plan.tokenID]; frozenByToken != nil && frozenByToken[plan.from] {
			return ErrDTLFrozen
		}
	}
	if state.Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	if state.Allowances[plan.allowanceKey] < plan.amount {
		return ErrDTLInsufficientAllowance
	}
	return nil
}

func validateParallelDTLBurnPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	token, exists := state.Tokens[plan.tokenID]
	if !exists || token == nil {
		return ErrDTLUnknownToken
	}
	if plan.amount == 0 {
		return fmt.Errorf("dtl: burn amount must be > 0")
	}
	if plan.from == "" {
		return fmt.Errorf("dtl: invalid burn account")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled {
		if frozenByToken := state.FrozenAccounts[plan.tokenID]; frozenByToken != nil && frozenByToken[plan.from] {
			return ErrDTLFrozen
		}
	}
	if state.Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	if token.TotalSupply < plan.amount {
		return fmt.Errorf("dtl: burn exceeds total supply")
	}
	return nil
}

func validateParallelDTLNFT721TransferPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	collection := state.NFT721Collections[plan.collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	if plan.from == "" || plan.to == "" {
		return fmt.Errorf("dtl: invalid nft721 account")
	}
	if plan.from == plan.to {
		return fmt.Errorf("dtl: nft721 self transfer not allowed")
	}
	owner := normalizeDTLAccount(state.NFT721Owners[plan.fromKey])
	if owner == "" {
		return ErrDTLUnknownNFTToken
	}
	if owner != plan.from {
		return ErrDTLNotNFTTokenOwner
	}
	return nil
}

func validateParallelDTLNFT721CreatePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.nft721CreateTx == nil {
		return ErrDTLInvalidState
	}
	tx := *plan.nft721CreateTx
	if plan.from == "" {
		return fmt.Errorf("dtl: invalid nft721 creator")
	}
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid nft721 name length")
	}
	if plan.symbol == "" || len(plan.symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid nft721 symbol length")
	}
	if _, exists := state.NFT721SymbolIndex[plan.symbol]; exists {
		return fmt.Errorf("dtl: nft721 symbol already exists: %s", plan.symbol)
	}
	if len(strings.TrimSpace(tx.BaseURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft721 base uri too long")
	}
	if state.NFT721Collections[plan.collectionID] != nil {
		return fmt.Errorf("dtl: nft721 collection id collision")
	}
	return nil
}

func validateParallelDTLNFT721MintPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.nft721MintTx == nil {
		return ErrDTLInvalidState
	}
	tx := *plan.nft721MintTx
	collection := state.NFT721Collections[plan.collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	if plan.from == "" || plan.from != normalizeDTLAccount(collection.Creator) {
		return fmt.Errorf("dtl: nft721 mint creator mismatch")
	}
	if plan.to == "" {
		return fmt.Errorf("dtl: invalid nft721 receiver")
	}
	if len(strings.TrimSpace(tx.TokenURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft721 token uri too long")
	}
	if collection.NextTokenID == ^uint64(0) {
		return fmt.Errorf("dtl: nft721 token id overflow")
	}
	if existing := normalizeDTLAccount(state.NFT721Owners[plan.fromKey]); existing != "" {
		return fmt.Errorf("dtl: nft721 token already minted")
	}
	return nil
}

func validateParallelDTLNFT1155TransferPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	collection := state.NFT1155Collections[plan.collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	if plan.from == "" || plan.to == "" {
		return fmt.Errorf("dtl: invalid nft1155 account")
	}
	if plan.from == plan.to {
		return fmt.Errorf("dtl: nft1155 self transfer not allowed")
	}
	if plan.amount == 0 {
		return fmt.Errorf("dtl: nft1155 transfer amount must be > 0")
	}
	if state.NFT1155Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	return nil
}

func validateParallelDTLNFT1155CreatePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.nft1155CreateTx == nil {
		return ErrDTLInvalidState
	}
	tx := *plan.nft1155CreateTx
	if plan.from == "" {
		return fmt.Errorf("dtl: invalid nft1155 creator")
	}
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid nft1155 name length")
	}
	if plan.symbol == "" || len(plan.symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid nft1155 symbol length")
	}
	if _, exists := state.NFT1155SymbolIndex[plan.symbol]; exists {
		return fmt.Errorf("dtl: nft1155 symbol already exists: %s", plan.symbol)
	}
	if len(strings.TrimSpace(tx.BaseURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft1155 base uri too long")
	}
	if state.NFT1155Collections[plan.collectionID] != nil {
		return fmt.Errorf("dtl: nft1155 collection id collision")
	}
	return nil
}

func validateParallelDTLNFT1155MintPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.nft1155MintTx == nil {
		return ErrDTLInvalidState
	}
	collection := state.NFT1155Collections[plan.collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	if plan.from == "" || plan.from != normalizeDTLAccount(collection.Creator) {
		return fmt.Errorf("dtl: nft1155 mint creator mismatch")
	}
	if plan.to == "" {
		return fmt.Errorf("dtl: invalid nft1155 receiver")
	}
	if plan.amount == 0 {
		return fmt.Errorf("dtl: nft1155 mint amount must be > 0")
	}
	return nil
}

func validateParallelDTLPoolCreatePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.poolCreateTx == nil {
		return ErrDTLInvalidState
	}
	return ValidateDTLPoolCreateTx(state, *plan.poolCreateTx)
}

func validateParallelDTLPoolAddPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.poolAddTx == nil {
		return ErrDTLInvalidState
	}
	return ValidateDTLPoolAddLiquidityTx(state, *plan.poolAddTx)
}

func validateParallelDTLPoolRemovePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.poolRemoveTx == nil {
		return ErrDTLInvalidState
	}
	return ValidateDTLPoolRemoveLiquidityTx(state, *plan.poolRemoveTx)
}

func validateParallelDTLPoolSwapPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.poolSwapTx == nil {
		return ErrDTLInvalidState
	}
	pool := state.Pools[plan.poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	if pool.ProtocolFeeBPS != 0 {
		return fmt.Errorf("dtl: parallel pool swap requires zero protocol fee")
	}
	return ValidateDTLPoolSwapTx(state, *plan.poolSwapTx)
}

func validateParallelDTLOracleFeedCreatePlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.oracleFeedTx == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolV2EnabledAtHeight(plan.height) {
		return fmt.Errorf("dtl: oracle v2 not active at height %d", plan.height)
	}
	return ValidateDTLOracleFeedCreateTx(state, *plan.oracleFeedTx)
}

func validateParallelDTLOraclePriceSubmitPlan(state *DTLState, plan parallelDTLExecutionPlan) error {
	if state == nil || plan.oraclePriceTx == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolV2EnabledAtHeight(plan.height) {
		return fmt.Errorf("dtl: oracle v2 not active at height %d", plan.height)
	}
	return ValidateDTLOraclePriceSubmitTx(state, *plan.oraclePriceTx, plan.height)
}

func executeParallelDTLPlan(state *DTLState, plan parallelDTLExecutionPlan) (*parallelDTLStatePatch, error) {
	if err := validateParallelDTLExecutionPlan(state, plan); err != nil {
		return nil, err
	}
	isolated := parallelDTLIsolatedState(state, plan)
	ledger := Ledger{DTL: isolated}
	if err := applyParallelDTLPlan(&ledger, plan); err != nil {
		return nil, err
	}
	return &parallelDTLStatePatch{state: ledger.DTL}, nil
}

func parallelDTLIsolatedState(source *DTLState, plan parallelDTLExecutionPlan) *DTLState {
	isolated := NewDTLState()
	if source == nil {
		return isolated
	}

	copyToken := func(tokenID string) {
		tokenID = normalizeDTLTokenID(tokenID)
		if tokenID == "" || isolated.Tokens[tokenID] != nil {
			return
		}
		if token := source.Tokens[tokenID]; token != nil {
			cloned := *token
			cloned.TokenID = tokenID
			cloned.Symbol = normalizeDTLSymbol(token.Symbol)
			cloned.AuthoritySigners = uniqueDTLSigners(token.AuthoritySigners)
			cloned.MetadataURI = strings.TrimSpace(token.MetadataURI)
			isolated.Tokens[tokenID] = &cloned
		}
		if frozen := source.FrozenAccounts[tokenID]; frozen != nil {
			cloned := canonicalDTLValueMap(frozen, normalizeDTLAccount)
			if len(cloned) > 0 {
				isolated.FrozenAccounts[tokenID] = cloned
			}
		}
	}
	copyToken(plan.tokenID)
	copyToken(plan.tokenA)
	copyToken(plan.tokenB)

	if collection := source.NFT721Collections[plan.collectionID]; collection != nil {
		cloned := *collection
		isolated.NFT721Collections[plan.collectionID] = &cloned
	}
	if collection := source.NFT1155Collections[plan.collectionID]; collection != nil {
		cloned := *collection
		isolated.NFT1155Collections[plan.collectionID] = &cloned
	}
	if pool := source.Pools[plan.poolID]; pool != nil {
		cloned := *pool
		isolated.Pools[plan.poolID] = &cloned
		copyToken(pool.TokenA)
		copyToken(pool.TokenB)
	}
	if feed := source.OracleFeeds[plan.feedID]; feed != nil {
		cloned := *feed
		cloned.Signers = uniqueDTLSigners(feed.Signers)
		isolated.OracleFeeds[plan.feedID] = &cloned
	}

	for _, writeKey := range plan.writeKeys {
		namespace, key, ok := strings.Cut(writeKey, ":")
		if !ok || key == "" {
			continue
		}
		switch namespace {
		case "token", "token_supply":
			copyToken(key)
		case "symbol":
			if value, exists := source.SymbolIndex[key]; exists {
				isolated.SymbolIndex[key] = value
			}
		case "balance":
			if value, exists := source.Balances[key]; exists {
				isolated.Balances[key] = value
			}
		case "allowance":
			if value, exists := source.Allowances[key]; exists {
				isolated.Allowances[key] = value
			}
		case "nft721_collection", "nft721_collection_counter":
			if collection := source.NFT721Collections[key]; collection != nil {
				cloned := *collection
				isolated.NFT721Collections[key] = &cloned
			}
		case "nft721_symbol":
			if value, exists := source.NFT721SymbolIndex[key]; exists {
				isolated.NFT721SymbolIndex[key] = value
			}
		case "nft721_owner":
			if value, exists := source.NFT721Owners[key]; exists {
				isolated.NFT721Owners[key] = value
			}
		case "nft721_uri":
			if value, exists := source.NFT721TokenURIs[key]; exists {
				isolated.NFT721TokenURIs[key] = value
			}
		case "nft1155_collection":
			if collection := source.NFT1155Collections[key]; collection != nil {
				cloned := *collection
				isolated.NFT1155Collections[key] = &cloned
			}
		case "nft1155_symbol":
			if value, exists := source.NFT1155SymbolIndex[key]; exists {
				isolated.NFT1155SymbolIndex[key] = value
			}
		case "nft1155_balance":
			if value, exists := source.NFT1155Balances[key]; exists {
				isolated.NFT1155Balances[key] = value
			}
		case "nft1155_supply":
			if value, exists := source.NFT1155Supplies[key]; exists {
				isolated.NFT1155Supplies[key] = value
			}
		case "pool":
			if pool := source.Pools[key]; pool != nil {
				cloned := *pool
				isolated.Pools[key] = &cloned
			}
		case "pool_pair":
			if value, exists := source.PoolIndex[key]; exists {
				isolated.PoolIndex[key] = value
			}
		case "lp_balance":
			if value, exists := source.LPBalances[key]; exists {
				isolated.LPBalances[key] = value
			}
		case "oracle_feed":
			if feed := source.OracleFeeds[key]; feed != nil {
				cloned := *feed
				cloned.Signers = uniqueDTLSigners(feed.Signers)
				isolated.OracleFeeds[key] = &cloned
			}
		case "oracle_sample":
			feedID, signer, found := strings.Cut(key, "|")
			if !found {
				continue
			}
			if samples := source.OracleSamples[feedID]; samples != nil {
				if sample, exists := samples[signer]; exists {
					isolated.OracleSamples[feedID] = map[string]DTLOracleSampleState{signer: sample}
				}
			}
		}
	}
	canonicalizeDTLState(isolated)
	return isolated
}

func mergeParallelDTLStatePatch(target *DTLState, plan parallelDTLExecutionPlan, patch *parallelDTLStatePatch) error {
	if target == nil || patch == nil || patch.state == nil {
		return ErrDTLInvalidState
	}
	target.ensure()
	source := patch.state

	for _, writeKey := range plan.writeKeys {
		namespace, key, ok := strings.Cut(writeKey, ":")
		if !ok || key == "" {
			return fmt.Errorf("dtl: invalid parallel write key")
		}
		switch namespace {
		case "token", "token_supply":
			if value := source.Tokens[key]; value != nil {
				target.Tokens[key] = value
			} else {
				delete(target.Tokens, key)
			}
		case "symbol":
			mergeParallelDTLStringMap(target.SymbolIndex, source.SymbolIndex, key)
		case "balance":
			mergeParallelDTLUint64Map(target.Balances, source.Balances, key)
		case "allowance":
			mergeParallelDTLUint64Map(target.Allowances, source.Allowances, key)
		case "nft721_collection", "nft721_collection_counter":
			if value := source.NFT721Collections[key]; value != nil {
				target.NFT721Collections[key] = value
			} else {
				delete(target.NFT721Collections, key)
			}
		case "nft721_symbol":
			mergeParallelDTLStringMap(target.NFT721SymbolIndex, source.NFT721SymbolIndex, key)
		case "nft721_owner":
			mergeParallelDTLStringMap(target.NFT721Owners, source.NFT721Owners, key)
		case "nft721_uri":
			mergeParallelDTLStringMap(target.NFT721TokenURIs, source.NFT721TokenURIs, key)
		case "nft1155_collection":
			if value := source.NFT1155Collections[key]; value != nil {
				target.NFT1155Collections[key] = value
			} else {
				delete(target.NFT1155Collections, key)
			}
		case "nft1155_symbol":
			mergeParallelDTLStringMap(target.NFT1155SymbolIndex, source.NFT1155SymbolIndex, key)
		case "nft1155_balance":
			mergeParallelDTLUint64Map(target.NFT1155Balances, source.NFT1155Balances, key)
		case "nft1155_supply":
			mergeParallelDTLUint64Map(target.NFT1155Supplies, source.NFT1155Supplies, key)
		case "pool":
			if value := source.Pools[key]; value != nil {
				target.Pools[key] = value
			} else {
				delete(target.Pools, key)
			}
		case "pool_twap":
			// TWAP accumulators are fields on DTLPoolState and merge with "pool".
		case "pool_pair":
			mergeParallelDTLStringMap(target.PoolIndex, source.PoolIndex, key)
		case "lp_balance":
			mergeParallelDTLUint64Map(target.LPBalances, source.LPBalances, key)
		case "oracle_feed":
			if value := source.OracleFeeds[key]; value != nil {
				target.OracleFeeds[key] = value
			} else {
				delete(target.OracleFeeds, key)
			}
		case "oracle_sample":
			feedID, signer, found := strings.Cut(key, "|")
			if !found {
				return fmt.Errorf("dtl: invalid oracle sample write key")
			}
			samples := source.OracleSamples[feedID]
			sample, exists := samples[signer]
			if !exists {
				if target.OracleSamples[feedID] != nil {
					delete(target.OracleSamples[feedID], signer)
				}
				continue
			}
			if target.OracleSamples[feedID] == nil {
				target.OracleSamples[feedID] = make(map[string]DTLOracleSampleState)
			}
			target.OracleSamples[feedID][signer] = sample
		default:
			return fmt.Errorf("dtl: unsupported parallel write namespace: %s", namespace)
		}
	}
	target.Events = append(target.Events, source.Events...)
	target.EventLogs = append(target.EventLogs, source.EventLogs...)
	return nil
}

func mergeParallelDTLUint64Map(target map[string]uint64, source map[string]uint64, key string) {
	if value, exists := source[key]; exists {
		target[key] = value
	} else {
		delete(target, key)
	}
}

func mergeParallelDTLStringMap(target map[string]string, source map[string]string, key string) {
	if value, exists := source[key]; exists {
		target[key] = value
	} else {
		delete(target, key)
	}
}

func applyParallelDTLPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	switch plan.kind {
	case DTLTxTokenCreate:
		return applyParallelDTLCreatePlan(ledger, plan)
	case DTLTxTokenTransfer:
		return applyParallelDTLTransferPlan(ledger, plan)
	case DTLTxTokenApprove:
		return applyParallelDTLApprovePlan(ledger, plan)
	case DTLTxTokenTransferFrom:
		return applyParallelDTLTransferFromPlan(ledger, plan)
	case DTLTxTokenBurn:
		return applyParallelDTLBurnPlan(ledger, plan)
	case DTLTxNFT721Create:
		return applyParallelDTLNFT721CreatePlan(ledger, plan)
	case DTLTxNFT721Mint:
		return applyParallelDTLNFT721MintPlan(ledger, plan)
	case DTLTxNFT721Transfer:
		return applyParallelDTLNFT721TransferPlan(ledger, plan)
	case DTLTxNFT1155Create:
		return applyParallelDTLNFT1155CreatePlan(ledger, plan)
	case DTLTxNFT1155Mint:
		return applyParallelDTLNFT1155MintPlan(ledger, plan)
	case DTLTxNFT1155Transfer:
		return applyParallelDTLNFT1155TransferPlan(ledger, plan)
	case DTLTxPoolCreate:
		return applyParallelDTLPoolCreatePlan(ledger, plan)
	case DTLTxPoolAdd:
		return applyParallelDTLPoolAddPlan(ledger, plan)
	case DTLTxPoolRemove:
		return applyParallelDTLPoolRemovePlan(ledger, plan)
	case DTLTxPoolSwap:
		return applyParallelDTLPoolSwapPlan(ledger, plan)
	case DTLTxOracleFeedCreate:
		return applyParallelDTLOracleFeedCreatePlan(ledger, plan)
	case DTLTxOraclePriceSubmit:
		return applyParallelDTLOraclePriceSubmitPlan(ledger, plan)
	default:
		return fmt.Errorf("dtl: unsupported parallel tx kind")
	}
}

func applyParallelDTLCreatePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.createTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.Tokens[plan.tokenID] != nil {
		return fmt.Errorf("dtl: token id collision")
	}
	if _, exists := ledger.DTL.SymbolIndex[plan.symbol]; exists {
		return fmt.Errorf("dtl: symbol already exists: %s", plan.symbol)
	}
	tx := *plan.createTx
	signers := make([]string, 0, len(tx.AuthoritySigners))
	for _, signer := range tx.AuthoritySigners {
		signers = append(signers, normalizeDTLAccount(signer))
	}
	ledger.DTL.Tokens[plan.tokenID] = &DTLTokenState{
		TokenID:            plan.tokenID,
		Name:               tx.Name,
		Symbol:             plan.symbol,
		Decimals:           tx.Decimals,
		MaxSupply:          tx.MaxSupply,
		TotalSupply:        tx.InitialSupply,
		Paused:             false,
		FreezeEnabled:      tx.FreezeEnabled,
		TaxBPS:             tx.TaxBPS,
		AuthoritySigners:   uniqueDTLSigners(signers),
		AuthorityThreshold: tx.AuthorityThreshold,
		MetadataURI:        tx.MetadataURI,
	}
	ledger.DTL.SymbolIndex[plan.symbol] = plan.tokenID
	if tx.InitialSupply > 0 {
		ledger.DTL.Balances[plan.fromKey] = tx.InitialSupply
	}
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLTransferPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	ledger.DTL.Balances[plan.fromKey] -= plan.amount
	if err := dtlAddBalance(ledger.DTL.Balances, plan.toKey, plan.net); err != nil {
		return err
	}
	if plan.tax > 0 {
		if err := dtlAddBalance(ledger.DTL.Balances, plan.treasuryKey, plan.tax); err != nil {
			return err
		}
	}
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLApprovePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if plan.amount == 0 {
		delete(ledger.DTL.Allowances, plan.allowanceKey)
	} else {
		ledger.DTL.Allowances[plan.allowanceKey] = plan.amount
	}
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLTransferFromPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.Allowances[plan.allowanceKey] < plan.amount {
		return ErrDTLInsufficientAllowance
	}
	nextAllowance := ledger.DTL.Allowances[plan.allowanceKey] - plan.amount
	if nextAllowance == 0 {
		delete(ledger.DTL.Allowances, plan.allowanceKey)
	} else {
		ledger.DTL.Allowances[plan.allowanceKey] = nextAllowance
	}
	if ledger.DTL.Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	ledger.DTL.Balances[plan.fromKey] -= plan.amount
	if err := dtlAddBalance(ledger.DTL.Balances, plan.toKey, plan.net); err != nil {
		return err
	}
	if plan.tax > 0 {
		if err := dtlAddBalance(ledger.DTL.Balances, plan.treasuryKey, plan.tax); err != nil {
			return err
		}
	}
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLBurnPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	token := ledger.DTL.Tokens[plan.tokenID]
	if token == nil {
		return ErrDTLUnknownToken
	}
	if ledger.DTL.Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	if token.TotalSupply < plan.amount {
		return fmt.Errorf("dtl: burn exceeds total supply")
	}
	ledger.DTL.Balances[plan.fromKey] -= plan.amount
	token.TotalSupply -= plan.amount
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLNFT721TransferPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if normalizeDTLAccount(ledger.DTL.NFT721Owners[plan.fromKey]) != plan.from {
		return ErrDTLNotNFTTokenOwner
	}
	ledger.DTL.NFT721Owners[plan.fromKey] = plan.to
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLNFT721CreatePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.nft721CreateTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.NFT721Collections[plan.collectionID] != nil {
		return fmt.Errorf("dtl: nft721 collection id collision")
	}
	if _, exists := ledger.DTL.NFT721SymbolIndex[plan.symbol]; exists {
		return fmt.Errorf("dtl: nft721 symbol already exists: %s", plan.symbol)
	}
	tx := *plan.nft721CreateTx
	ledger.DTL.NFT721Collections[plan.collectionID] = &DTLNFT721CollectionState{
		CollectionID: plan.collectionID,
		Creator:      normalizeDTLAccount(tx.Creator),
		Name:         strings.TrimSpace(tx.Name),
		Symbol:       plan.symbol,
		BaseURI:      strings.TrimSpace(tx.BaseURI),
		NextTokenID:  0,
		TotalMinted:  0,
		Paused:       false,
	}
	ledger.DTL.NFT721SymbolIndex[plan.symbol] = plan.collectionID
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLNFT721MintPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.nft721MintTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	collection := ledger.DTL.NFT721Collections[plan.collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.NextTokenID == ^uint64(0) {
		return fmt.Errorf("dtl: nft721 token id overflow")
	}
	if collection.NextTokenID+1 != plan.nftTokenID {
		return fmt.Errorf("dtl: nft721 mint sequence conflict")
	}
	if existing := normalizeDTLAccount(ledger.DTL.NFT721Owners[plan.fromKey]); existing != "" {
		return fmt.Errorf("dtl: nft721 token already minted")
	}
	ledger.DTL.NFT721Owners[plan.fromKey] = plan.to
	tokenURI := strings.TrimSpace(plan.nft721MintTx.TokenURI)
	if tokenURI != "" {
		ledger.DTL.NFT721TokenURIs[plan.fromKey] = tokenURI
	}
	collection.NextTokenID = plan.nftTokenID
	collection.TotalMinted++
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLNFT1155TransferPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.NFT1155Balances[plan.fromKey] < plan.amount {
		return ErrDTLInsufficientFunds
	}
	ledger.DTL.NFT1155Balances[plan.fromKey] -= plan.amount
	if ledger.DTL.NFT1155Balances[plan.fromKey] == 0 {
		delete(ledger.DTL.NFT1155Balances, plan.fromKey)
	}
	if err := dtlAddBalance(ledger.DTL.NFT1155Balances, plan.toKey, plan.amount); err != nil {
		return err
	}
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLNFT1155CreatePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.nft1155CreateTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.NFT1155Collections[plan.collectionID] != nil {
		return fmt.Errorf("dtl: nft1155 collection id collision")
	}
	if _, exists := ledger.DTL.NFT1155SymbolIndex[plan.symbol]; exists {
		return fmt.Errorf("dtl: nft1155 symbol already exists: %s", plan.symbol)
	}
	tx := *plan.nft1155CreateTx
	ledger.DTL.NFT1155Collections[plan.collectionID] = &DTLNFT1155CollectionState{
		CollectionID: plan.collectionID,
		Creator:      normalizeDTLAccount(tx.Creator),
		Name:         strings.TrimSpace(tx.Name),
		Symbol:       plan.symbol,
		BaseURI:      strings.TrimSpace(tx.BaseURI),
		Paused:       false,
	}
	ledger.DTL.NFT1155SymbolIndex[plan.symbol] = plan.collectionID
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLNFT1155MintPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	if ledger.DTL.NFT1155Collections[plan.collectionID] == nil {
		return ErrDTLUnknownNFTCollection
	}
	if err := dtlAddBalance(ledger.DTL.NFT1155Balances, plan.fromKey, plan.amount); err != nil {
		return err
	}
	if err := dtlAddBalance(ledger.DTL.NFT1155Supplies, plan.toKey, plan.amount); err != nil {
		return err
	}
	ledger.DTL.Events = append(ledger.DTL.Events, plan.event)
	return nil
}

func applyParallelDTLPoolCreatePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.poolCreateTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	chainID, err := canonicalDTLTransactionChainID(plan.tx)
	if err != nil {
		return err
	}
	_, err = ApplyDTLPoolCreateTx(ledger.DTL, chainID, uint64(plan.tx.Nonce), *plan.poolCreateTx)
	return err
}

func applyParallelDTLPoolAddPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.poolAddTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	return ApplyDTLPoolAddLiquidityTx(ledger.DTL, *plan.poolAddTx)
}

func applyParallelDTLPoolRemovePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.poolRemoveTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	return ApplyDTLPoolRemoveLiquidityTx(ledger.DTL, *plan.poolRemoveTx)
}

func applyParallelDTLPoolSwapPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.poolSwapTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	_, err := ApplyDTLPoolSwapTx(ledger.DTL, *plan.poolSwapTx)
	return err
}

func applyParallelDTLOracleFeedCreatePlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.oracleFeedTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	chainID, err := canonicalDTLTransactionChainID(plan.tx)
	if err != nil {
		return err
	}
	_, err = ApplyDTLOracleFeedCreateTx(ledger.DTL, chainID, uint64(plan.tx.Nonce), *plan.oracleFeedTx)
	return err
}

func applyParallelDTLOraclePriceSubmitPlan(ledger *Ledger, plan parallelDTLExecutionPlan) error {
	if ledger == nil || plan.oraclePriceTx == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)
	return ApplyDTLOraclePriceSubmitTx(ledger.DTL, plan.height, *plan.oraclePriceTx)
}
