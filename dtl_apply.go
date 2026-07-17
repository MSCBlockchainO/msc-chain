package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// `DTLDefaultReplayWindow` defines the constant value used by this package.
const DTLDefaultReplayWindow uint64 = 256

// `dtlLendingIndexScale` defines the constant value used by this package.
const dtlLendingIndexScale uint64 = 1_000_000

// dtlOracleFeedIDFromCreate implements the dtl oracle feed id from create helper.
func dtlOracleFeedIDFromCreate(chainID string, nonce uint64, tx DTLOracleFeedCreateTx) string {
	// `base` stores the value produced by this operation.
	base := normalizeDTLTokenID(tx.BaseTokenID)
	// `quote` stores the value produced by this operation.
	quote := normalizeDTLTokenID(tx.QuoteTokenID)
	// `id` stores the current position in the related collection.
	if id := normalizeDTLTokenID(tx.FeedID); id != "" {
		return id
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(chainID),
		strconv.FormatUint(nonce, 10),
		base,
		quote,
	}, "|")))
	return "orcl" + hex.EncodeToString(sum[:12])
}

// dtlMulBPSAndBlocks implements the dtl mul bps and blocks helper.
func dtlMulBPSAndBlocks(value uint64, rateBPS uint16, blocks uint64) (uint64, error) {
	if value == 0 || rateBPS == 0 || blocks == 0 {
		return 0, nil
	}
	// `num` stores the value produced by this operation.
	num := new(big.Int).SetUint64(value)
	num.Mul(num, new(big.Int).SetUint64(uint64(rateBPS)))
	num.Mul(num, new(big.Int).SetUint64(blocks))
	num.Div(num, big.NewInt(DTLMaxTaxBPS))
	if num.Sign() < 0 || num.BitLen() > 64 {
		return 0, fmt.Errorf("dtl: interest overflow")
	}
	return num.Uint64(), nil
}

// dtlDeterministicBeaconHash implements the dtl deterministic beacon hash helper.
func dtlDeterministicBeaconHash(scope string, height uint64) string {
	// `payload` stores the value produced by this operation.
	payload := strings.Join([]string{
		"msc-dtl-beacon",
		strings.TrimSpace(scope),
		strconv.FormatUint(height, 10),
	}, "|")
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// dtlOracleMedianPrice implements the dtl oracle median price helper.
func dtlOracleMedianPrice(state *DTLState, feedID string, currentHeight uint64) (uint64, bool) {
	if state == nil {
		return 0, false
	}
	state.ensure()
	feedID = normalizeDTLTokenID(feedID)
	// `feed` stores the value produced by this operation.
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		return 0, false
	}
	if feed.LastMedianPrice == 0 {
		return 0, false
	}
	// `maxStaleness` stores the value produced by this operation.
	maxStaleness := dtlProtocolOracleMaxStalenessBlocks()
	if maxStaleness == 0 {
		return feed.LastMedianPrice, true
	}
	if currentHeight >= feed.LastUpdateHeight && currentHeight-feed.LastUpdateHeight > maxStaleness {
		return 0, false
	}
	return feed.LastMedianPrice, true
}

// dtlPoolProtocolFeeCut implements the dtl pool protocol fee cut helper.
func dtlPoolProtocolFeeCut(amountIn uint64, feeBPS, protocolFeeBPS uint16) (uint64, error) {
	if amountIn == 0 || feeBPS == 0 || protocolFeeBPS == 0 {
		return 0, nil
	}
	// `feeAmount` and `err` store the error produced by this operation.
	feeAmount, err := dtlMulDivU64(amountIn, uint64(feeBPS), DTLMaxTaxBPS)
	if err != nil || feeAmount == 0 {
		return 0, err
	}
	return dtlMulDivU64(feeAmount, uint64(protocolFeeBPS), DTLMaxTaxBPS)
}

// dtlLendingHealthFactorBPS implements the dtl lending health factor bps helper.
func dtlLendingHealthFactorBPS(
	state *DTLState,
	market *DTLLendingMarketState,
	collateral uint64,
	debt uint64,
	currentHeight uint64,
) (bool, uint64, error) {
	if market == nil {
		return false, 0, fmt.Errorf("dtl: nil lending market")
	}
	if debt == 0 {
		return true, DTLMaxTaxBPS, nil
	}
	// `collPrice` and `hasColl` store the value produced by this operation.
	collPrice, hasColl := dtlOracleMedianPrice(state, market.CollateralFeedID, currentHeight)
	// `debtPrice` and `hasDebt` store the value produced by this operation.
	debtPrice, hasDebt := dtlOracleMedianPrice(state, market.DebtFeedID, currentHeight)
	if !hasColl || !hasDebt || collPrice == 0 || debtPrice == 0 {
		// `healthy` and `err` store the error produced by this operation.
		healthy, err := dtlLendingIsHealthy(collateral, debt, market.CollateralFactorBPS)
		if err != nil {
			return false, 0, err
		}
		// `maxDebt` and `err` store the error produced by this operation.
		maxDebt, err := dtlLendingMaxDebt(collateral, market.CollateralFactorBPS)
		if err != nil {
			return false, 0, err
		}
		if debt == 0 {
			return healthy, DTLMaxTaxBPS, nil
		}
		// `hf` and `err` store the error produced by this operation.
		hf, err := dtlMulDivU64(maxDebt, DTLMaxTaxBPS, debt)
		if err != nil {
			return healthy, 0, err
		}
		return healthy, hf, nil
	}

	// `collValue` stores the value currently being processed.
	collValue := new(big.Int).SetUint64(collateral)
	collValue.Mul(collValue, new(big.Int).SetUint64(collPrice))
	// `maxDebtValue` stores the value currently being processed.
	maxDebtValue := new(big.Int).Mul(collValue, new(big.Int).SetUint64(uint64(market.CollateralFactorBPS)))
	maxDebtValue.Div(maxDebtValue, big.NewInt(DTLMaxTaxBPS))
	// `debtValue` stores the value currently being processed.
	debtValue := new(big.Int).SetUint64(debt)
	debtValue.Mul(debtValue, new(big.Int).SetUint64(debtPrice))
	// `healthy` stores the value produced by this operation.
	healthy := maxDebtValue.Cmp(debtValue) >= 0
	if debtValue.Sign() == 0 {
		return healthy, DTLMaxTaxBPS, nil
	}
	// `hf` stores the value produced by this operation.
	hf := new(big.Int).Mul(maxDebtValue, new(big.Int).SetUint64(DTLMaxTaxBPS))
	hf.Div(hf, debtValue)
	if hf.Sign() < 0 {
		return healthy, 0, nil
	}
	if hf.BitLen() > 64 {
		return healthy, ^uint64(0), nil
	}
	return healthy, hf.Uint64(), nil
}

// dtlLendingAccrueMarket implements the dtl lending accrue market helper.
func dtlLendingAccrueMarket(state *DTLState, marketID string, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[normalizeDTLMarketID(marketID)]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	if market.BorrowIndex == 0 {
		market.BorrowIndex = dtlLendingIndexScale
	}
	if market.LastAccrualHeight == 0 {
		market.LastAccrualHeight = currentHeight
		return nil
	}
	if currentHeight <= market.LastAccrualHeight {
		return nil
	}
	// `blocks` stores the block data handled by this operation.
	blocks := currentHeight - market.LastAccrualHeight
	// `interval` stores the value currently being processed.
	interval := dtlProtocolLendingAccrualIntervalBlocks()
	if interval == 0 {
		interval = 1
	}
	if blocks < interval {
		return nil
	}
	// `utilBPS` stores the value produced by this operation.
	utilBPS := uint64(0)
	if market.TotalCollateral > 0 && market.TotalDebt > 0 {
		// `v` and `err` store the error produced by this operation.
		v, err := dtlMulDivU64(market.TotalDebt, DTLMaxTaxBPS, market.TotalCollateral)
		if err != nil {
			return err
		}
		if v > DTLMaxTaxBPS {
			v = DTLMaxTaxBPS
		}
		utilBPS = v
	}
	// `base` stores the value produced by this operation.
	base := uint64(market.BaseBorrowRateBPS)
	// `slope` stores the value produced by this operation.
	slope := uint64(market.SlopeBorrowRateBPS)
	// `rateSlope` and `err` store the error produced by this operation.
	rateSlope, err := dtlMulDivU64(slope, utilBPS, DTLMaxTaxBPS)
	if err != nil {
		return err
	}
	// `rateBPS` stores the value produced by this operation.
	rateBPS := uint16(base + rateSlope)
	if rateBPS == 0 {
		market.LastAccrualHeight = currentHeight
		return nil
	}
	// `totalIncrease` and `err` store the error produced by this operation.
	totalIncrease, err := dtlMulBPSAndBlocks(market.TotalDebt, rateBPS, blocks)
	if err != nil {
		return err
	}
	if totalIncrease > 0 {
		// `nextTotalDebt` and `err` store the error produced by this operation.
		nextTotalDebt, err := dtlSafeAddU64(market.TotalDebt, totalIncrease)
		if err != nil {
			return err
		}
		market.TotalDebt = nextTotalDebt
		// `positionKeys` stores the value produced by this operation.
		positionKeys := make([]string, 0, len(state.LendingPositions))
		// `key` tracks the key used to access the related value.
		for key := range state.LendingPositions {
			positionKeys = append(positionKeys, key)
		}
		sort.Strings(positionKeys)
		// `key` tracks the key used to access the related value.
		for _, key := range positionKeys {
			// `position` stores the value produced by this operation.
			position := state.LendingPositions[key]
			if position == nil || normalizeDTLMarketID(position.MarketID) != normalizeDTLMarketID(marketID) {
				continue
			}
			// `inc` and `err` store the error produced by this operation.
			inc, err := dtlMulBPSAndBlocks(position.Debt, rateBPS, blocks)
			if err != nil {
				return err
			}
			position.Debt, err = dtlSafeAddU64(position.Debt, inc)
			if err != nil {
				return err
			}
			position.ScaledDebt = position.Debt
			state.LendingPositions[key] = position
		}
		// `indexInc` and `err` store the error produced by this operation.
		indexInc, err := dtlMulBPSAndBlocks(market.BorrowIndex, rateBPS, blocks)
		if err != nil {
			return err
		}
		market.BorrowIndex, err = dtlSafeAddU64(market.BorrowIndex, indexInc)
		if err != nil {
			return err
		}
		// DeFi -> GameFi treasury share from lending accrual (reward token only).
		if shareBPS := dtlProtocolGameFiFeeShareFromLendingBPS(); shareBPS > 0 {
			// `seasonID` and `ok` store whether the related condition is satisfied.
			if seasonID, ok := dtlActiveRewardSeasonID(state, currentHeight, market.DebtTokenID); ok {
				// `seasonShare` and `err` store the error produced by this operation.
				seasonShare, err := dtlMulDivU64(totalIncrease, uint64(shareBPS), DTLMaxTaxBPS)
				if err != nil {
					return err
				}
				if seasonShare > 0 {
					// `vault` stores the value produced by this operation.
					vault := dtlLendingVaultAccount(marketID)
					// `available` stores the value produced by this operation.
					available := state.BalanceOf(market.DebtTokenID, vault)
					if seasonShare > available {
						seasonShare = available
					}
					if seasonShare > 0 {
						// `err` stores the error produced by this operation.
						if err := dtlMoveBalance(state, market.DebtTokenID, vault, DTLTreasuryAccount, seasonShare); err != nil {
							return err
						}
						// `err` stores the error produced by this operation.
						if err := dtlAddSeasonVaultBalance(state, seasonID, seasonShare); err != nil {
							return err
						}
						state.Events = append(state.Events, fmt.Sprintf("GAMEFI_VAULT_FUND_LENDING:%s:%s:%d", seasonID, market.DebtTokenID, seasonShare))
						dtlAppendStructuredEventLog(state, []string{"GAMEFI_VAULT_FUND_LENDING", seasonID}, map[string]any{
							"season_id": seasonID,
							"market_id": normalizeDTLMarketID(marketID),
							"token_id":  normalizeDTLTokenID(market.DebtTokenID),
							"amount":    seasonShare,
						})
					}
				}
			}
		}
	}
	market.LastAccrualHeight = currentHeight
	return nil
}

// dtlPoolBumpTWAP implements the dtl pool bump twap helper.
func dtlPoolBumpTWAP(pool *DTLPoolState) {
	if pool == nil || pool.ReserveA == 0 || pool.ReserveB == 0 {
		return
	}
	// `priceA` and `errA` store the error produced by this operation.
	priceA, errA := dtlMulDivU64(pool.ReserveB, DTLMaxTaxBPS, pool.ReserveA)
	// `priceB` and `errB` store the error produced by this operation.
	priceB, errB := dtlMulDivU64(pool.ReserveA, DTLMaxTaxBPS, pool.ReserveB)
	if errA == nil {
		// `next` and `err` store the error produced by this operation.
		if next, err := dtlSafeAddU64(pool.PriceCumulativeA, priceA); err == nil {
			pool.PriceCumulativeA = next
		}
	}
	if errB == nil {
		// `next` and `err` store the error produced by this operation.
		if next, err := dtlSafeAddU64(pool.PriceCumulativeB, priceB); err == nil {
			pool.PriceCumulativeB = next
		}
	}
	if pool.LastTwapHeight < ^uint64(0) {
		pool.LastTwapHeight++
	}
}

// ApplyDTLCreateTx applies dtl create tx.
func ApplyDTLCreateTx(state *DTLState, chainID string, nonce uint64, tx DTLCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()

	// `err` stores the error produced by this operation.
	if err := ValidateDTLCreateTx(state, tx); err != nil {
		return "", err
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := DTLTokenIDFromCreate(chainID, tx, nonce)
	tokenID = normalizeDTLTokenID(tokenID)
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.Tokens[tokenID]; exists {
		return "", fmt.Errorf("dtl: token id collision")
	}

	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(tx.AuthoritySigners))
	// `signer` tracks the current values while iterating.
	for _, signer := range tx.AuthoritySigners {
		signers = append(signers, normalizeDTLAccount(signer))
	}

	// `token` stores the value produced by this operation.
	token := &DTLTokenState{
		TokenID:            tokenID,
		Name:               tx.Name,
		Symbol:             normalizeDTLSymbol(tx.Symbol),
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

	state.Tokens[tokenID] = token
	state.SymbolIndex[token.Symbol] = tokenID

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if tx.InitialSupply > 0 {
		state.Balances[dtlBalanceKey(tokenID, creator)] = tx.InitialSupply
	}
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_CREATE:%s", tokenID))
	return tokenID, nil
}

// applyDTLTransferWithTax applies dtl transfer with tax.
func applyDTLTransferWithTax(state *DTLState, tokenID, from, to string, amount uint64) error {
	// `token` stores the value produced by this operation.
	token := state.Tokens[tokenID]
	// `fromKey` stores the key used to access the related value.
	fromKey := dtlBalanceKey(tokenID, from)
	// `toKey` stores the key used to access the related value.
	toKey := dtlBalanceKey(tokenID, to)

	// `tax` stores the value produced by this operation.
	tax := uint64(0)
	if token.TaxBPS > 0 {
		tax = (amount * uint64(token.TaxBPS)) / DTLMaxTaxBPS
	}
	// `net` stores the value produced by this operation.
	net := amount - tax

	state.Balances[fromKey] -= amount
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.Balances, toKey, net); err != nil {
		return err
	}
	if tax > 0 {
		// `treasuryKey` stores the key used to access the related value.
		treasuryKey := dtlBalanceKey(tokenID, DTLTreasuryAccount)
		// `err` stores the error produced by this operation.
		if err := dtlAddBalance(state.Balances, treasuryKey, tax); err != nil {
			return err
		}
	}
	return nil
}

// ApplyDTLTransferTx applies dtl transfer tx.
func ApplyDTLTransferTx(state *DTLState, tx DTLTransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLTransferTx(state, tx); err != nil {
		return err
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)

	// `err` stores the error produced by this operation.
	if err := applyDTLTransferWithTax(state, tokenID, from, to, tx.Amount); err != nil {
		return err
	}

	state.Events = append(state.Events, fmt.Sprintf("TOKEN_TRANSFER:%s:%s->%s:%d", tokenID, from, to, tx.Amount))
	return nil
}

// ApplyDTLApproveTx applies dtl approve tx.
func ApplyDTLApproveTx(state *DTLState, tx DTLApproveTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLApproveTx(state, tx); err != nil {
		return err
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `owner` stores the value produced by this operation.
	owner := normalizeDTLAccount(tx.Owner)
	// `spender` stores the value produced by this operation.
	spender := normalizeDTLAccount(tx.Spender)
	// `allowanceKey` stores the key used to access the related value.
	allowanceKey := dtlAllowanceKey(tokenID, owner, spender)
	if tx.Amount == 0 {
		delete(state.Allowances, allowanceKey)
	} else {
		state.Allowances[allowanceKey] = tx.Amount
	}
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_APPROVE:%s:%s->%s:%d", tokenID, owner, spender, tx.Amount))
	return nil
}

// ApplyDTLTransferFromTx applies dtl transfer from tx.
func ApplyDTLTransferFromTx(state *DTLState, tx DTLTransferFromTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLTransferFromTx(state, tx); err != nil {
		return err
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `spender` stores the value produced by this operation.
	spender := normalizeDTLAccount(tx.Spender)
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)

	// `allowanceKey` stores the key used to access the related value.
	allowanceKey := dtlAllowanceKey(tokenID, from, spender)
	// `nextAllowance` stores the value produced by this operation.
	nextAllowance := state.Allowances[allowanceKey] - tx.Amount
	if nextAllowance == 0 {
		delete(state.Allowances, allowanceKey)
	} else {
		state.Allowances[allowanceKey] = nextAllowance
	}
	// `err` stores the error produced by this operation.
	if err := applyDTLTransferWithTax(state, tokenID, from, to, tx.Amount); err != nil {
		return err
	}
	state.Events = append(
		state.Events,
		fmt.Sprintf("TOKEN_TRANSFER_FROM:%s:%s->%s:by=%s:%d", tokenID, from, to, spender, tx.Amount),
	)
	return nil
}

// ApplyDTLBurnTx applies dtl burn tx.
func ApplyDTLBurnTx(state *DTLState, tx DTLBurnTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLBurnTx(state, tx); err != nil {
		return err
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` stores the value produced by this operation.
	token := state.Tokens[tokenID]
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `fromKey` stores the key used to access the related value.
	fromKey := dtlBalanceKey(tokenID, from)

	state.Balances[fromKey] -= tx.Amount
	token.TotalSupply -= tx.Amount
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_BURN:%s:%s:%d", tokenID, from, tx.Amount))
	return nil
}

// ApplyDTLNFT721CreateTx applies dtlnft721 create tx.
func ApplyDTLNFT721CreateTx(state *DTLState, chainID string, nonce uint64, tx DTLNFT721CreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLNFT721CreateTx(state, tx); err != nil {
		return "", err
	}

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(DTLNFT721CollectionIDFromCreate(chainID, tx, nonce))
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.NFT721Collections[collectionID]; exists {
		return "", fmt.Errorf("dtl: nft721 collection id collision")
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(tx.Symbol)
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLCollectionID(state.NFT721SymbolIndex[symbol]); existing != "" {
		return "", fmt.Errorf("dtl: nft721 symbol already exists: %s", symbol)
	}

	state.NFT721Collections[collectionID] = &DTLNFT721CollectionState{
		CollectionID: collectionID,
		Creator:      normalizeDTLAccount(tx.Creator),
		Name:         strings.TrimSpace(tx.Name),
		Symbol:       symbol,
		BaseURI:      strings.TrimSpace(tx.BaseURI),
		NextTokenID:  0,
		TotalMinted:  0,
		Paused:       false,
	}
	state.NFT721SymbolIndex[symbol] = collectionID
	state.Events = append(state.Events, fmt.Sprintf("NFT721_CREATE:%s", collectionID))
	return collectionID, nil
}

// ApplyDTLNFT721MintTx applies dtlnft721 mint tx.
func ApplyDTLNFT721MintTx(state *DTLState, tx DTLNFT721MintTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLNFT721MintTx(state, tx); err != nil {
		return 0, err
	}

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `collection` stores the value produced by this operation.
	collection := state.NFT721Collections[collectionID]
	if collection.NextTokenID == ^uint64(0) {
		return 0, fmt.Errorf("dtl: nft721 token id overflow")
	}
	// `tokenID` stores the value produced by this operation.
	tokenID := collection.NextTokenID + 1
	// `ownerKey` stores the key used to access the related value.
	ownerKey := dtlNFT721OwnerKey(collectionID, tokenID)
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLAccount(state.NFT721Owners[ownerKey]); existing != "" {
		return 0, fmt.Errorf("dtl: nft721 token already minted")
	}
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	state.NFT721Owners[ownerKey] = to
	// `tokenURI` stores the current position in the related collection.
	tokenURI := strings.TrimSpace(tx.TokenURI)
	if tokenURI != "" {
		state.NFT721TokenURIs[ownerKey] = tokenURI
	}
	collection.NextTokenID = tokenID
	collection.TotalMinted++
	state.Events = append(state.Events, fmt.Sprintf("NFT721_MINT:%s:%d:%s", collectionID, tokenID, to))
	return tokenID, nil
}

// ApplyDTLNFT721TransferTx applies dtlnft721 transfer tx.
func ApplyDTLNFT721TransferTx(state *DTLState, tx DTLNFT721TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLNFT721TransferTx(state, tx); err != nil {
		return err
	}

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `ownerKey` stores the key used to access the related value.
	ownerKey := dtlNFT721OwnerKey(collectionID, tx.TokenID)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	state.NFT721Owners[ownerKey] = to
	state.Events = append(
		state.Events,
		fmt.Sprintf("NFT721_TRANSFER:%s:%d:%s->%s", collectionID, tx.TokenID, normalizeDTLAccount(tx.From), to),
	)
	return nil
}

// ApplyDTLNFT1155CreateTx applies dtlnft1155 create tx.
func ApplyDTLNFT1155CreateTx(state *DTLState, chainID string, nonce uint64, tx DTLNFT1155CreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLNFT1155CreateTx(state, tx); err != nil {
		return "", err
	}

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(DTLNFT1155CollectionIDFromCreate(chainID, tx, nonce))
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.NFT1155Collections[collectionID]; exists {
		return "", fmt.Errorf("dtl: nft1155 collection id collision")
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(tx.Symbol)
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLCollectionID(state.NFT1155SymbolIndex[symbol]); existing != "" {
		return "", fmt.Errorf("dtl: nft1155 symbol already exists: %s", symbol)
	}

	state.NFT1155Collections[collectionID] = &DTLNFT1155CollectionState{
		CollectionID: collectionID,
		Creator:      normalizeDTLAccount(tx.Creator),
		Name:         strings.TrimSpace(tx.Name),
		Symbol:       symbol,
		BaseURI:      strings.TrimSpace(tx.BaseURI),
		Paused:       false,
	}
	state.NFT1155SymbolIndex[symbol] = collectionID
	state.Events = append(state.Events, fmt.Sprintf("NFT1155_CREATE:%s", collectionID))
	return collectionID, nil
}

// ApplyDTLNFT1155MintTx applies dtlnft1155 mint tx.
func ApplyDTLNFT1155MintTx(state *DTLState, tx DTLNFT1155MintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLNFT1155MintTx(state, tx); err != nil {
		return err
	}

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	// `balanceKey` stores the key used to access the related value.
	balanceKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, to)
	// `supplyKey` stores the key used to access the related value.
	supplyKey := dtlNFT1155SupplyKey(collectionID, tx.TokenID)
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.NFT1155Balances, balanceKey, tx.Amount); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.NFT1155Supplies, supplyKey, tx.Amount); err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("NFT1155_MINT:%s:%d:%s:%d", collectionID, tx.TokenID, to, tx.Amount))
	return nil
}

// ApplyDTLNFT1155TransferTx applies dtlnft1155 transfer tx.
func ApplyDTLNFT1155TransferTx(state *DTLState, tx DTLNFT1155TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLNFT1155TransferTx(state, tx); err != nil {
		return err
	}

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	// `fromKey` stores the key used to access the related value.
	fromKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, from)
	// `toKey` stores the key used to access the related value.
	toKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, to)
	state.NFT1155Balances[fromKey] -= tx.Amount
	if state.NFT1155Balances[fromKey] == 0 {
		delete(state.NFT1155Balances, fromKey)
	}
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.NFT1155Balances, toKey, tx.Amount); err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("NFT1155_TRANSFER:%s:%d:%s->%s:%d", collectionID, tx.TokenID, from, to, tx.Amount))
	return nil
}

// canonicalizeDTLPoolPair returns canonical ize dtl pool pair.
func canonicalizeDTLPoolPair(tokenA, tokenB string, amountA, amountB uint64) (string, string, uint64, uint64) {
	// `a` stores the value produced by this operation.
	a := normalizeDTLTokenID(tokenA)
	// `b` stores the value produced by this operation.
	b := normalizeDTLTokenID(tokenB)
	if a <= b {
		return a, b, amountA, amountB
	}
	return b, a, amountB, amountA
}

// ApplyDTLPoolCreateTx applies dtl pool create tx.
func ApplyDTLPoolCreateTx(
	state *DTLState,
	chainID string,
	nonce uint64,
	tx DTLPoolCreateTx,
) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLPoolCreateTx(state, tx); err != nil {
		return "", err
	}

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	// `tokenA`, `tokenB`, `amountA`, and `amountB` store the value produced by this operation.
	tokenA, tokenB, amountA, amountB := canonicalizeDTLPoolPair(
		tx.TokenA,
		tx.TokenB,
		tx.AmountA,
		tx.AmountB,
	)
	// `pairKey` stores the key used to access the related value.
	pairKey := dtlPoolPairKey(tokenA, tokenB)
	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(DTLPoolIDFromTokens(chainID, tokenA, tokenB))
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLPoolID(state.PoolIndex[pairKey]); existing != "" {
		return "", fmt.Errorf("dtl: pool already exists for pair: %s", existing)
	}
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.Pools[poolID]; exists {
		return "", fmt.Errorf("dtl: pool id collision")
	}

	// `share` and `err` store the error produced by this operation.
	share, err := dtlInitialPoolShare(amountA, amountB)
	if err != nil {
		return "", err
	}
	if share == 0 {
		return "", fmt.Errorf("dtl: initial LP share must be > 0")
	}
	// `feeBPS` stores the value produced by this operation.
	feeBPS := tx.FeeBPS
	if feeBPS == 0 {
		feeBPS = DTLDefaultPoolFeeBPS
	}

	// `vault` stores the value produced by this operation.
	vault := dtlPoolVaultAccount(poolID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tokenA, creator, vault, amountA); err != nil {
		return "", err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tokenB, creator, vault, amountB); err != nil {
		return "", err
	}

	state.Pools[poolID] = &DTLPoolState{
		PoolID:             poolID,
		TokenA:             tokenA,
		TokenB:             tokenB,
		ReserveA:           amountA,
		ReserveB:           amountB,
		TotalLPShares:      share,
		FeeBPS:             feeBPS,
		ProtocolFeeBPS:     0,
		ProtocolFeeAccount: DTLTreasuryAccount,
	}
	dtlPoolBumpTWAP(state.Pools[poolID])
	state.PoolIndex[pairKey] = poolID
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(poolID, creator), share); err != nil {
		return "", err
	}
	state.Events = append(state.Events, fmt.Sprintf("POOL_CREATE:%s:%s/%s", poolID, tokenA, tokenB))
	return poolID, nil
}

// ApplyDTLPoolAddLiquidityTx applies dtl pool add liquidity tx.
func ApplyDTLPoolAddLiquidityTx(state *DTLState, tx DTLPoolAddLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLPoolAddLiquidityTx(state, tx); err != nil {
		return err
	}

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	// `pool` stores the value produced by this operation.
	pool := state.Pools[poolID]
	// `provider` stores the value produced by this operation.
	provider := normalizeDTLAccount(tx.Provider)
	// `share` and `err` store the error produced by this operation.
	share, err := dtlLiquidityShareMint(pool, tx.AmountA, tx.AmountB)
	if err != nil {
		return err
	}
	if tx.MinLPShares > 0 && share < tx.MinLPShares {
		return fmt.Errorf("dtl: slippage: LP share below minimum")
	}

	// `vault` stores the value produced by this operation.
	vault := dtlPoolVaultAccount(poolID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, pool.TokenA, provider, vault, tx.AmountA); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, pool.TokenB, provider, vault, tx.AmountB); err != nil {
		return err
	}

	if pool.ReserveA > ^uint64(0)-tx.AmountA || pool.ReserveB > ^uint64(0)-tx.AmountB || pool.TotalLPShares > ^uint64(0)-share {
		return fmt.Errorf("dtl: pool reserve overflow")
	}
	pool.ReserveA += tx.AmountA
	pool.ReserveB += tx.AmountB
	pool.TotalLPShares += share
	dtlPoolBumpTWAP(pool)
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(poolID, provider), share); err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("POOL_ADD:%s:%s:%d", poolID, provider, share))
	return nil
}

// ApplyDTLPoolRemoveLiquidityTx applies dtl pool remove liquidity tx.
func ApplyDTLPoolRemoveLiquidityTx(state *DTLState, tx DTLPoolRemoveLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLPoolRemoveLiquidityTx(state, tx); err != nil {
		return err
	}

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	// `pool` stores the value produced by this operation.
	pool := state.Pools[poolID]
	// `provider` stores the value produced by this operation.
	provider := normalizeDTLAccount(tx.Provider)
	// `outA`, `outB`, and `err` store the error produced by this operation.
	outA, outB, err := dtlLiquidityShareBurn(pool, tx.LPShares)
	if err != nil {
		return err
	}
	if tx.MinAmountA > 0 && outA < tx.MinAmountA {
		return fmt.Errorf("dtl: slippage: token A below minimum")
	}
	if tx.MinAmountB > 0 && outB < tx.MinAmountB {
		return fmt.Errorf("dtl: slippage: token B below minimum")
	}

	// `lpKey` stores the key used to access the related value.
	lpKey := dtlLPBalanceKey(poolID, provider)
	if state.LPBalances[lpKey] < tx.LPShares {
		return fmt.Errorf("dtl: insufficient LP balance")
	}
	state.LPBalances[lpKey] -= tx.LPShares
	pool.TotalLPShares -= tx.LPShares
	pool.ReserveA -= outA
	pool.ReserveB -= outB
	dtlPoolBumpTWAP(pool)

	// `vault` stores the value produced by this operation.
	vault := dtlPoolVaultAccount(poolID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, pool.TokenA, vault, provider, outA); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, pool.TokenB, vault, provider, outB); err != nil {
		return err
	}

	state.Events = append(state.Events, fmt.Sprintf("POOL_REMOVE:%s:%s:%d", poolID, provider, tx.LPShares))
	return nil
}

// ApplyDTLPoolSwapTx applies dtl pool swap tx.
func ApplyDTLPoolSwapTx(state *DTLState, tx DTLPoolSwapTx) (uint64, error) {
	return ApplyDTLPoolSwapTxWithHeight(state, 0, tx)
}

// ApplyDTLPoolSwapTxWithHeight applies dtl pool swap tx with height.
func ApplyDTLPoolSwapTxWithHeight(state *DTLState, currentHeight uint64, tx DTLPoolSwapTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLPoolSwapTx(state, tx); err != nil {
		return 0, err
	}

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	// `pool` stores the value produced by this operation.
	pool := state.Pools[poolID]
	// `trader` stores the value produced by this operation.
	trader := normalizeDTLAccount(tx.Trader)
	// `tokenIn` stores the value produced by this operation.
	tokenIn := normalizeDTLTokenID(tx.TokenIn)

	// `tokenOut` stores the result produced by this operation.
	tokenOut := pool.TokenB
	// `reserveIn` stores the result produced by this operation.
	reserveIn := pool.ReserveA
	// `reserveOut` stores the result produced by this operation.
	reserveOut := pool.ReserveB
	// `inIsA` stores the current position in the related collection.
	inIsA := true
	if tokenIn == pool.TokenB {
		tokenOut = pool.TokenA
		reserveIn = pool.ReserveB
		reserveOut = pool.ReserveA
		inIsA = false
	}

	// `amountOut` and `err` store the error produced by this operation.
	amountOut, err := dtlPoolSwapOutAmount(reserveIn, reserveOut, tx.AmountIn, pool.FeeBPS)
	if err != nil {
		return 0, err
	}
	if tx.MinAmountOut > 0 && amountOut < tx.MinAmountOut {
		return 0, fmt.Errorf("dtl: slippage: output below minimum")
	}

	// `vault` stores the value produced by this operation.
	vault := dtlPoolVaultAccount(poolID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tokenIn, trader, vault, tx.AmountIn); err != nil {
		return 0, err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tokenOut, vault, trader, amountOut); err != nil {
		return 0, err
	}
	// `protocolFeeCut` and `err` store the error produced by this operation.
	protocolFeeCut, err := dtlPoolProtocolFeeCut(tx.AmountIn, pool.FeeBPS, pool.ProtocolFeeBPS)
	if err != nil {
		return 0, err
	}
	if protocolFeeCut > 0 {
		// `seasonID` and `ok` store whether the related condition is satisfied.
		if seasonID, ok := dtlActiveRewardSeasonID(state, currentHeight, tokenIn); ok {
			// `shareBPS` stores the value produced by this operation.
			shareBPS := dtlProtocolGameFiFeeShareFromPoolBPS()
			if shareBPS > 0 {
				// `seasonShare` and `err` store the error produced by this operation.
				seasonShare, err := dtlMulDivU64(protocolFeeCut, uint64(shareBPS), DTLMaxTaxBPS)
				if err != nil {
					return 0, err
				}
				if seasonShare > protocolFeeCut {
					seasonShare = protocolFeeCut
				}
				if seasonShare > 0 {
					// `err` stores the error produced by this operation.
					if err := dtlMoveBalance(state, tokenIn, vault, DTLTreasuryAccount, seasonShare); err != nil {
						return 0, err
					}
					// `err` stores the error produced by this operation.
					if err := dtlAddSeasonVaultBalance(state, seasonID, seasonShare); err != nil {
						return 0, err
					}
					protocolFeeCut -= seasonShare
					state.Events = append(state.Events, fmt.Sprintf("GAMEFI_VAULT_FUND_POOL:%s:%s:%d", seasonID, tokenIn, seasonShare))
					dtlAppendStructuredEventLog(state, []string{"GAMEFI_VAULT_FUND_POOL", seasonID}, map[string]any{
						"season_id": seasonID,
						"pool_id":   poolID,
						"token_id":  normalizeDTLTokenID(tokenIn),
						"amount":    seasonShare,
					})
				}
			}
		}
	}
	if protocolFeeCut > 0 {
		// `protocolAccount` stores the measured quantity used by this operation.
		protocolAccount := normalizeDTLAccount(pool.ProtocolFeeAccount)
		if protocolAccount == "" {
			protocolAccount = DTLTreasuryAccount
		}
		if protocolAccount != vault {
			// `err` stores the error produced by this operation.
			if err := dtlMoveBalance(state, tokenIn, vault, protocolAccount, protocolFeeCut); err != nil {
				return 0, err
			}
		}
	}
	// `reserveInDelta` stores the result produced by this operation.
	reserveInDelta := tx.AmountIn
	if protocolFeeCut > reserveInDelta {
		return 0, fmt.Errorf("dtl: protocol fee exceeds input")
	}
	reserveInDelta -= protocolFeeCut

	if inIsA {
		if pool.ReserveA > ^uint64(0)-reserveInDelta || pool.ReserveB < amountOut {
			return 0, fmt.Errorf("dtl: pool reserve update failed")
		}
		pool.ReserveA += reserveInDelta
		pool.ReserveB -= amountOut
	} else {
		if pool.ReserveB > ^uint64(0)-reserveInDelta || pool.ReserveA < amountOut {
			return 0, fmt.Errorf("dtl: pool reserve update failed")
		}
		pool.ReserveB += reserveInDelta
		pool.ReserveA -= amountOut
	}
	dtlPoolBumpTWAP(pool)

	state.Events = append(state.Events, fmt.Sprintf("POOL_SWAP:%s:%s:%s:%d:%d", poolID, trader, tokenIn, tx.AmountIn, amountOut))
	if protocolFeeCut > 0 {
		state.Events = append(state.Events, fmt.Sprintf("POOL_PROTOCOL_FEE:%s:%s:%d", poolID, tokenIn, protocolFeeCut))
	}
	return amountOut, nil
}

type DTLRouteHopQuote struct {
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `TokenIn` stores the value associated with this record.
	TokenIn string `json:"token_in"`
	// `TokenOut` stores the result produced by this operation.
	TokenOut string `json:"token_out"`
	// `AmountIn` stores the value associated with this record.
	AmountIn uint64 `json:"amount_in"`
	// `AmountOut` stores the result produced by this operation.
	AmountOut uint64 `json:"amount_out"`
	// `FeeBPS` stores the value associated with this record.
	FeeBPS uint16 `json:"fee_bps"`
}

type DTLRouteQuote struct {
	// `Path` stores the value associated with this record.
	Path []string `json:"path"`
	// `TokenIn` stores the value associated with this record.
	TokenIn string `json:"token_in"`
	// `TokenOut` stores the result produced by this operation.
	TokenOut string `json:"token_out"`
	// `AmountIn` stores the value associated with this record.
	AmountIn uint64 `json:"amount_in"`
	// `AmountOut` stores the result produced by this operation.
	AmountOut uint64 `json:"amount_out"`
	// `PriceImpactBPS` stores the value associated with this record.
	PriceImpactBPS uint16 `json:"price_impact_bps"`
	// `Hops` stores the value associated with this record.
	Hops []DTLRouteHopQuote `json:"hops"`
}

// dtlRoutePriceImpactBPS implements the dtl route price impact bps helper.
func dtlRoutePriceImpactBPS(expectedOut, actualOut uint64) uint16 {
	if expectedOut == 0 || actualOut >= expectedOut {
		return 0
	}
	// `diff` stores the value produced by this operation.
	diff := new(big.Int).SetUint64(expectedOut - actualOut)
	// `num` stores the value produced by this operation.
	num := diff.Mul(diff, new(big.Int).SetUint64(DTLMaxTaxBPS))
	// `den` stores the value produced by this operation.
	den := new(big.Int).SetUint64(expectedOut)
	if den.Sign() == 0 {
		return 0
	}
	// `out` stores the result produced by this operation.
	out := new(big.Int).Div(num, den)
	if out.Sign() <= 0 {
		return 0
	}
	if out.Cmp(new(big.Int).SetUint64(DTLMaxTaxBPS)) > 0 {
		return DTLMaxTaxBPS
	}
	return uint16(out.Uint64())
}

// dtlAppendStructuredEventLog implements the dtl append structured event log helper.
func dtlAppendStructuredEventLog(state *DTLState, topics []string, payload any) {
	if state == nil {
		return
	}
	state.ensure()
	// `cleanTopics` stores the value produced by this operation.
	cleanTopics := make([]string, 0, len(topics))
	// `topic` tracks the current values while iterating.
	for _, topic := range topics {
		// `t` stores the value produced by this operation.
		t := strings.TrimSpace(topic)
		if t == "" {
			continue
		}
		cleanTopics = append(cleanTopics, t)
	}
	// `data` stores the value produced by this operation.
	data := ""
	if payload != nil {
		// `encoded` and `err` store the error produced by this operation.
		if encoded, err := json.Marshal(payload); err == nil {
			data = string(encoded)
		}
	}
	state.EventLogs = append(state.EventLogs, DTLEventLog{
		Topics: cleanTopics,
		Data:   data,
	})
}

// dtlQuotePoolSwapRoute implements the dtl quote pool swap route helper.
func dtlQuotePoolSwapRoute(state *DTLState, tokenIn string, amountIn uint64, path []string) (DTLRouteQuote, error) {
	// `quote` stores the value produced by this operation.
	quote := DTLRouteQuote{}
	if state == nil {
		return quote, ErrDTLInvalidState
	}
	if amountIn == 0 {
		return quote, fmt.Errorf("dtl: swap amount_in must be > 0")
	}
	if len(path) == 0 {
		return quote, fmt.Errorf("dtl: route path must contain at least 1 pool")
	}

	// `shadow` stores the value produced by this operation.
	shadow := cloneDTLState(state)
	if shadow == nil {
		return quote, ErrDTLInvalidState
	}
	shadow.ensure()

	// `normalizedTokenIn` stores the value produced by this operation.
	normalizedTokenIn := normalizeDTLTokenID(tokenIn)
	if normalizedTokenIn == "" {
		return quote, fmt.Errorf("dtl: token_in is required")
	}
	quote.TokenIn = normalizedTokenIn
	quote.Path = make([]string, 0, len(path))
	quote.Hops = make([]DTLRouteHopQuote, 0, len(path))
	quote.AmountIn = amountIn

	// `expectedNum` stores the value produced by this operation.
	expectedNum := new(big.Int).SetUint64(amountIn)
	// `expectedDen` stores the value produced by this operation.
	expectedDen := big.NewInt(1)
	// `currentToken` stores the value produced by this operation.
	currentToken := normalizedTokenIn
	// `currentIn` stores the value produced by this operation.
	currentIn := amountIn

	// `i` and `rawPoolID` track the current position in the related collection.
	for i, rawPoolID := range path {
		// `poolID` stores the value produced by this operation.
		poolID := normalizeDTLPoolID(rawPoolID)
		if poolID == "" {
			return quote, fmt.Errorf("dtl: route path has empty pool id at hop %d", i+1)
		}
		// `pool` stores the value produced by this operation.
		pool := shadow.Pools[poolID]
		if pool == nil {
			return quote, fmt.Errorf("dtl: unknown pool in route path: %s", poolID)
		}

		// `reserveIn` stores the result produced by this operation.
		reserveIn := pool.ReserveA
		// `reserveOut` stores the result produced by this operation.
		reserveOut := pool.ReserveB
		// `tokenOut` stores the result produced by this operation.
		tokenOut := pool.TokenB
		// `inIsA` stores the current position in the related collection.
		inIsA := true
		switch currentToken {
		case pool.TokenA:
			inIsA = true
		case pool.TokenB:
			inIsA = false
			reserveIn = pool.ReserveB
			reserveOut = pool.ReserveA
			tokenOut = pool.TokenA
		default:
			return quote, fmt.Errorf("dtl: route path disconnected at hop %d", i+1)
		}
		if reserveIn == 0 || reserveOut == 0 {
			return quote, fmt.Errorf("dtl: invalid pool reserves")
		}

		expectedNum.Mul(expectedNum, new(big.Int).SetUint64(reserveOut))
		expectedDen.Mul(expectedDen, new(big.Int).SetUint64(reserveIn))

		// `amountOut` and `err` store the error produced by this operation.
		amountOut, err := dtlPoolSwapOutAmount(reserveIn, reserveOut, currentIn, pool.FeeBPS)
		if err != nil {
			return quote, err
		}
		if amountOut == 0 {
			return quote, fmt.Errorf("dtl: swap output is zero")
		}

		// `protocolFeeCut` and `err` store the error produced by this operation.
		protocolFeeCut, err := dtlPoolProtocolFeeCut(currentIn, pool.FeeBPS, pool.ProtocolFeeBPS)
		if err != nil {
			return quote, err
		}
		// `reserveInDelta` stores the result produced by this operation.
		reserveInDelta := currentIn
		if protocolFeeCut > reserveInDelta {
			return quote, fmt.Errorf("dtl: protocol fee exceeds input")
		}
		reserveInDelta -= protocolFeeCut

		if inIsA {
			if pool.ReserveA > ^uint64(0)-reserveInDelta || pool.ReserveB < amountOut {
				return quote, fmt.Errorf("dtl: pool reserve update failed")
			}
			pool.ReserveA += reserveInDelta
			pool.ReserveB -= amountOut
		} else {
			if pool.ReserveB > ^uint64(0)-reserveInDelta || pool.ReserveA < amountOut {
				return quote, fmt.Errorf("dtl: pool reserve update failed")
			}
			pool.ReserveB += reserveInDelta
			pool.ReserveA -= amountOut
		}

		quote.Path = append(quote.Path, poolID)
		quote.Hops = append(quote.Hops, DTLRouteHopQuote{
			PoolID:    poolID,
			TokenIn:   currentToken,
			TokenOut:  tokenOut,
			AmountIn:  currentIn,
			AmountOut: amountOut,
			FeeBPS:    pool.FeeBPS,
		})
		currentToken = tokenOut
		currentIn = amountOut
	}

	quote.TokenOut = currentToken
	quote.AmountOut = currentIn
	if expectedDen.Sign() > 0 {
		// `expectedOut` stores the result produced by this operation.
		expectedOut := new(big.Int).Quo(expectedNum, expectedDen)
		// `expectedOutU64` stores the value produced by this operation.
		expectedOutU64 := uint64(0)
		if expectedOut.Sign() > 0 {
			if expectedOut.BitLen() > 64 {
				expectedOutU64 = ^uint64(0)
			} else {
				expectedOutU64 = expectedOut.Uint64()
			}
		}
		quote.PriceImpactBPS = dtlRoutePriceImpactBPS(expectedOutU64, quote.AmountOut)
	}
	return quote, nil
}

// ApplyDTLPoolSwapRouteTx applies dtl pool swap route tx.
func ApplyDTLPoolSwapRouteTx(state *DTLState, tx DTLPoolSwapRouteTx, currentHeight uint64) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLPoolSwapRouteTx(state, tx, currentHeight); err != nil {
		return 0, err
	}

	// `preview` and `err` store the error produced by this operation.
	preview, err := dtlQuotePoolSwapRoute(state, tx.TokenIn, tx.AmountIn, tx.Path)
	if err != nil {
		return 0, err
	}

	// `shadow` stores the value produced by this operation.
	shadow := cloneDTLState(state)
	if shadow == nil {
		return 0, ErrDTLInvalidState
	}
	shadow.ensure()

	// `trader` stores the value produced by this operation.
	trader := normalizeDTLAccount(tx.Trader)
	// `currentToken` stores the value produced by this operation.
	currentToken := normalizeDTLTokenID(tx.TokenIn)
	// `amountIn` stores the value produced by this operation.
	amountIn := tx.AmountIn
	// `rawPoolID` tracks the current values while iterating.
	for _, rawPoolID := range tx.Path {
		// `poolID` stores the value produced by this operation.
		poolID := normalizeDTLPoolID(rawPoolID)
		// `amountOut` and `err` store the error produced by this operation.
		amountOut, err := ApplyDTLPoolSwapTxWithHeight(shadow, currentHeight, DTLPoolSwapTx{
			Trader:       trader,
			PoolID:       poolID,
			TokenIn:      currentToken,
			AmountIn:     amountIn,
			MinAmountOut: 0,
		})
		if err != nil {
			return 0, err
		}
		// `pool` stores the value produced by this operation.
		pool := shadow.Pools[poolID]
		if pool == nil {
			return 0, fmt.Errorf("dtl: unknown pool")
		}
		if currentToken == pool.TokenA {
			currentToken = pool.TokenB
		} else {
			currentToken = pool.TokenA
		}
		amountIn = amountOut
	}

	if tx.MinAmountOut > 0 && amountIn < tx.MinAmountOut {
		return 0, fmt.Errorf("dtl: slippage: output below minimum")
	}

	*state = *shadow

	state.Events = append(state.Events, fmt.Sprintf(
		"POOL_ROUTE_SWAP:%s:%s:%s:%d:%d:%d",
		trader,
		preview.TokenIn,
		preview.TokenOut,
		tx.AmountIn,
		amountIn,
		len(preview.Hops),
	))
	// `i` and `hop` track the current position in the related collection.
	for i, hop := range preview.Hops {
		state.Events = append(state.Events, fmt.Sprintf(
			"POOL_ROUTE_HOP:%d:%s:%s:%s:%d:%d",
			i+1,
			hop.PoolID,
			hop.TokenIn,
			hop.TokenOut,
			hop.AmountIn,
			hop.AmountOut,
		))
		dtlAppendStructuredEventLog(state, []string{"POOL_ROUTE_SWAP_HOP", hop.PoolID}, map[string]any{
			"hop":        i + 1,
			"pool_id":    hop.PoolID,
			"token_in":   hop.TokenIn,
			"token_out":  hop.TokenOut,
			"amount_in":  hop.AmountIn,
			"amount_out": hop.AmountOut,
			"fee_bps":    hop.FeeBPS,
		})
	}
	dtlAppendStructuredEventLog(state, []string{"POOL_ROUTE_SWAP", trader}, map[string]any{
		"trader":           trader,
		"path":             preview.Path,
		"token_in":         preview.TokenIn,
		"token_out":        preview.TokenOut,
		"amount_in":        tx.AmountIn,
		"amount_out":       amountIn,
		"min_amount_out":   tx.MinAmountOut,
		"route_hops":       len(preview.Hops),
		"price_impact_bps": preview.PriceImpactBPS,
		"deadline_height":  tx.DeadlineHeight,
	})
	return amountIn, nil
}

// ApplyDTLDuelCreateTx applies dtl duel create tx.
func ApplyDTLDuelCreateTx(
	state *DTLState,
	chainID string,
	nonce uint64,
	currentHeight uint64,
	tx DTLDuelCreateTx,
) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLDuelCreateTx(state, tx, currentHeight); err != nil {
		return "", err
	}

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `commit` stores the value produced by this operation.
	commit := normalizeDTLHex(tx.CommitHash)
	// `joinBlocks` stores the current position in the related collection.
	joinBlocks := tx.JoinExpiryBlocks
	if joinBlocks == 0 {
		joinBlocks = DTLDefaultDuelJoinBlocks
	}
	// `revealBlocks` stores the value produced by this operation.
	revealBlocks := tx.RevealExpiryBlocks
	if revealBlocks == 0 {
		revealBlocks = DTLDefaultDuelRevealBlocks
	}
	// `joinDeadline` stores the current position in the related collection.
	joinDeadline := currentHeight + joinBlocks
	// `revealDeadline` stores the value produced by this operation.
	revealDeadline := joinDeadline + revealBlocks
	// `beaconDelay` stores the value produced by this operation.
	beaconDelay := dtlProtocolBeaconDelayAtHeight(currentHeight)
	// `beaconHeight` stores the value produced by this operation.
	beaconHeight := revealDeadline
	if beaconDelay > 0 && beaconHeight <= ^uint64(0)-beaconDelay {
		beaconHeight += beaconDelay
	}
	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(DTLDuelIDFromCreate(chainID, nonce, tx))
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.Duels[duelID]; exists {
		return "", fmt.Errorf("dtl: duel id collision")
	}

	// `vault` stores the value produced by this operation.
	vault := dtlDuelVaultAccount(duelID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tokenID, creator, vault, tx.Stake); err != nil {
		return "", err
	}

	state.Duels[duelID] = &DTLDuelState{
		DuelID:         duelID,
		TokenID:        tokenID,
		Stake:          tx.Stake,
		PlayerA:        creator,
		CommitA:        commit,
		JoinDeadline:   joinDeadline,
		RevealDeadline: revealDeadline,
		BeaconHeight:   beaconHeight,
	}
	state.Events = append(state.Events, fmt.Sprintf("DUEL_CREATE:%s:%s", duelID, creator))
	return duelID, nil
}

// ApplyDTLDuelJoinTx applies dtl duel join tx.
func ApplyDTLDuelJoinTx(state *DTLState, currentHeight uint64, tx DTLDuelJoinTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLDuelJoinTx(state, tx, currentHeight); err != nil {
		return err
	}

	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(tx.DuelID)
	// `duel` stores the value produced by this operation.
	duel := state.Duels[duelID]
	// `joiner` stores the current position in the related collection.
	joiner := normalizeDTLAccount(tx.Joiner)
	// `commit` stores the value produced by this operation.
	commit := normalizeDTLHex(tx.CommitHash)
	// `vault` stores the value produced by this operation.
	vault := dtlDuelVaultAccount(duelID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, duel.TokenID, joiner, vault, duel.Stake); err != nil {
		return err
	}
	duel.PlayerB = joiner
	duel.CommitB = commit
	state.Events = append(state.Events, fmt.Sprintf("DUEL_JOIN:%s:%s", duelID, joiner))
	return nil
}

// ApplyDTLDuelRevealTx applies dtl duel reveal tx.
func ApplyDTLDuelRevealTx(state *DTLState, currentHeight uint64, tx DTLDuelRevealTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLDuelRevealTx(state, tx, currentHeight); err != nil {
		return err
	}

	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(tx.DuelID)
	// `duel` stores the value produced by this operation.
	duel := state.Duels[duelID]
	// `player` stores the value produced by this operation.
	player := normalizeDTLAccount(tx.Player)
	// `secret` stores the value produced by this operation.
	secret := strings.TrimSpace(tx.Secret)
	if player == normalizeDTLAccount(duel.PlayerA) {
		duel.RevealA = secret
	} else {
		duel.RevealB = secret
	}
	state.Events = append(state.Events, fmt.Sprintf("DUEL_REVEAL:%s:%s", duelID, player))
	return nil
}

// ApplyDTLDuelFinalizeTx applies dtl duel finalize tx.
func ApplyDTLDuelFinalizeTx(state *DTLState, currentHeight uint64, tx DTLDuelFinalizeTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLDuelFinalizeTx(state, tx, currentHeight); err != nil {
		return err
	}

	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(tx.DuelID)
	// `duel` stores the value produced by this operation.
	duel := state.Duels[duelID]
	// `vault` stores the value produced by this operation.
	vault := dtlDuelVaultAccount(duelID)
	if duel.PlayerB == "" {
		// `err` stores the error produced by this operation.
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerA, duel.Stake); err != nil {
			return err
		}
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_CANCEL:%s", duelID))
		return nil
	}
	if duel.BeaconHeight == 0 {
		duel.BeaconHeight = duel.RevealDeadline + dtlProtocolBeaconDelayAtHeight(currentHeight)
	}
	if currentHeight < duel.BeaconHeight {
		return fmt.Errorf("dtl: duel waiting for beacon height")
	}
	// `beaconHash` stores the digest used to identify or verify the related data.
	beaconHash := ""
	if duel.BeaconHeight > duel.RevealDeadline {
		beaconHash = dtlDeterministicBeaconHash(duelID, duel.BeaconHeight)
	}
	duel.BeaconHash = beaconHash
	// `stakeX2` and `err` store the error produced by this operation.
	stakeX2, err := dtlSafeAddU64(duel.Stake, duel.Stake)
	if err != nil {
		return err
	}

	switch {
	case duel.RevealA != "" && duel.RevealB != "":
		// `seedParts` stores the value produced by this operation.
		seedParts := []string{duelID, duel.RevealA, duel.RevealB}
		if beaconHash != "" {
			seedParts = append(seedParts, beaconHash)
		}
		// `seed` stores the value produced by this operation.
		seed := strings.Join(seedParts, "|")
		duel.FinalizationSeed = seed
		// `winnerScope` stores the value produced by this operation.
		winnerScope := duelID
		if beaconHash != "" {
			winnerScope = duelID + "|" + beaconHash
		}
		// `winner` stores the value produced by this operation.
		winner := dtlDuelWinner(winnerScope, duel.PlayerA, duel.PlayerB, duel.RevealA, duel.RevealB)
		if winner == "" {
			return fmt.Errorf("dtl: duel winner resolution failed")
		}
		// `err` stores the error produced by this operation.
		if err := dtlMoveBalance(state, duel.TokenID, vault, winner, stakeX2); err != nil {
			return err
		}
		duel.Winner = winner
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_FINALIZE:%s:%s", duelID, winner))
		// `err` stores the error produced by this operation.
		if err := dtlAddSeasonScore(state, currentHeight, winner, dtlProtocolGameFiDuelWinPoints(), "duel_finalize"); err != nil {
			return err
		}
		return nil
	case currentHeight >= duel.RevealDeadline && duel.RevealA != "" && duel.RevealB == "":
		// `err` stores the error produced by this operation.
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerA, stakeX2); err != nil {
			return err
		}
		duel.Winner = duel.PlayerA
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_FORFEIT:%s:%s", duelID, duel.PlayerA))
		// `err` stores the error produced by this operation.
		if err := dtlAddSeasonScore(state, currentHeight, duel.PlayerA, dtlProtocolGameFiDuelWinPoints(), "duel_forfeit"); err != nil {
			return err
		}
		return nil
	case currentHeight >= duel.RevealDeadline && duel.RevealB != "" && duel.RevealA == "":
		// `err` stores the error produced by this operation.
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerB, stakeX2); err != nil {
			return err
		}
		duel.Winner = duel.PlayerB
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_FORFEIT:%s:%s", duelID, duel.PlayerB))
		// `err` stores the error produced by this operation.
		if err := dtlAddSeasonScore(state, currentHeight, duel.PlayerB, dtlProtocolGameFiDuelWinPoints(), "duel_forfeit"); err != nil {
			return err
		}
		return nil
	case currentHeight >= duel.RevealDeadline:
		// `err` stores the error produced by this operation.
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerA, duel.Stake); err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerB, duel.Stake); err != nil {
			return err
		}
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_DRAW:%s", duelID))
		return nil
	default:
		return fmt.Errorf("dtl: duel finalize conditions not met")
	}
}

// getOrCreateDTLLendingPosition implements the get or create dtl lending position helper.
func getOrCreateDTLLendingPosition(state *DTLState, marketID, account string) *DTLLendingPositionState {
	// `key` stores the key used to access the related value.
	key := dtlLendingPositionKey(marketID, account)
	// `existing` stores the value produced by this operation.
	if existing := state.LendingPositions[key]; existing != nil {
		return existing
	}
	// `p` stores the value produced by this operation.
	p := &DTLLendingPositionState{
		MarketID: normalizeDTLMarketID(marketID),
		Account:  normalizeDTLAccount(account),
	}
	state.LendingPositions[key] = p
	return p
}

// ApplyDTLLendMarketCreateTx applies dtl lend market create tx.
func ApplyDTLLendMarketCreateTx(
	state *DTLState,
	chainID string,
	nonce uint64,
	tx DTLLendMarketCreateTx,
) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLLendMarketCreateTx(state, tx); err != nil {
		return "", err
	}
	// `collateralFactorBPS`, `liquidationBonusBPS`, and `err` store the error produced by this operation.
	collateralFactorBPS, liquidationBonusBPS, err := validateDTLLendingRiskParams(
		tx.CollateralFactorBPS,
		tx.LiquidationBonusBPS,
	)
	if err != nil {
		return "", err
	}

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	// `collateralTokenID` stores the value produced by this operation.
	collateralTokenID := normalizeDTLTokenID(tx.CollateralTokenID)
	// `debtTokenID` stores the value produced by this operation.
	debtTokenID := normalizeDTLTokenID(tx.DebtTokenID)
	// `pairKey` stores the key used to access the related value.
	pairKey := dtlLendingPairKey(collateralTokenID, debtTokenID)
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLMarketID(state.LendingIndex[pairKey]); existing != "" {
		return "", fmt.Errorf("dtl: lending market already exists for pair: %s", existing)
	}
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(DTLLendingMarketIDFromTokens(chainID, collateralTokenID, debtTokenID))
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.LendingMarkets[marketID]; exists {
		return "", fmt.Errorf("dtl: lending market id collision")
	}
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, debtTokenID, creator, vault, tx.DebtLiquidity); err != nil {
		return "", err
	}

	state.LendingMarkets[marketID] = &DTLLendingMarketState{
		MarketID:            marketID,
		CollateralTokenID:   collateralTokenID,
		DebtTokenID:         debtTokenID,
		CollateralFactorBPS: collateralFactorBPS,
		LiquidationBonusBPS: liquidationBonusBPS,
		CollateralFeedID:    normalizeDTLTokenID(tx.CollateralFeedID),
		DebtFeedID:          normalizeDTLTokenID(tx.DebtFeedID),
		ReserveFactorBPS:    tx.ReserveFactorBPS,
		BaseBorrowRateBPS:   tx.BaseBorrowRateBPS,
		SlopeBorrowRateBPS:  tx.SlopeBorrowRateBPS,
		CloseFactorBPS:      tx.CloseFactorBPS,
		BorrowIndex:         dtlLendingIndexScale,
	}
	if state.LendingMarkets[marketID].CloseFactorBPS == 0 {
		state.LendingMarkets[marketID].CloseFactorBPS = 5000
	}
	if state.LendingMarkets[marketID].SlopeBorrowRateBPS == 0 {
		state.LendingMarkets[marketID].SlopeBorrowRateBPS = 100
	}
	state.LendingIndex[pairKey] = marketID
	state.Events = append(state.Events, fmt.Sprintf("LEND_MARKET_CREATE:%s", marketID))
	return marketID, nil
}

// ApplyDTLLendDepositCollateralTx applies dtl lend deposit collateral tx.
func ApplyDTLLendDepositCollateralTx(state *DTLState, tx DTLLendDepositCollateralTx) error {
	return ApplyDTLLendDepositCollateralTxWithHeight(state, 0, tx)
}

// ApplyDTLLendDepositCollateralTxWithHeight applies dtl lend deposit collateral tx with height.
func ApplyDTLLendDepositCollateralTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendDepositCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLLendDepositCollateralTx(state, tx); err != nil {
		return err
	}
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `err` stores the error produced by this operation.
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	// `position` stores the value produced by this operation.
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)

	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, market.CollateralTokenID, account, vault, tx.Amount); err != nil {
		return err
	}
	// `newCollateral` and `err` store the error produced by this operation.
	newCollateral, err := dtlSafeAddU64(position.Collateral, tx.Amount)
	if err != nil {
		return err
	}
	// `newTotalCollateral` and `err` store the error produced by this operation.
	newTotalCollateral, err := dtlSafeAddU64(market.TotalCollateral, tx.Amount)
	if err != nil {
		return err
	}
	position.Collateral = newCollateral
	position.ScaledDebt = position.Debt
	market.TotalCollateral = newTotalCollateral
	state.Events = append(state.Events, fmt.Sprintf("LEND_DEPOSIT:%s:%s:%d", marketID, account, tx.Amount))
	return nil
}

// ApplyDTLLendBorrowTx applies dtl lend borrow tx.
func ApplyDTLLendBorrowTx(state *DTLState, tx DTLLendBorrowTx) error {
	return ApplyDTLLendBorrowTxWithHeight(state, 0, tx)
}

// ApplyDTLLendBorrowTxWithHeight applies dtl lend borrow tx with height.
func ApplyDTLLendBorrowTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendBorrowTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLLendBorrowTx(state, tx); err != nil {
		return err
	}
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `err` stores the error produced by this operation.
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	// `position` stores the value produced by this operation.
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	// `newDebt` and `err` store the error produced by this operation.
	newDebt, err := dtlSafeAddU64(position.Debt, tx.Amount)
	if err != nil {
		return err
	}
	// `healthy` and `err` store the error produced by this operation.
	healthy, _, err := dtlLendingHealthFactorBPS(state, market, position.Collateral, newDebt, currentHeight)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: borrow would exceed collateral limit")
	}
	// `newTotalDebt` and `err` store the error produced by this operation.
	newTotalDebt, err := dtlSafeAddU64(market.TotalDebt, tx.Amount)
	if err != nil {
		return err
	}

	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, market.DebtTokenID, vault, account, tx.Amount); err != nil {
		return err
	}
	position.Debt = newDebt
	position.ScaledDebt = newDebt
	market.TotalDebt = newTotalDebt
	state.Events = append(state.Events, fmt.Sprintf("LEND_BORROW:%s:%s:%d", marketID, account, tx.Amount))
	return nil
}

// ApplyDTLLendRepayTx applies dtl lend repay tx.
func ApplyDTLLendRepayTx(state *DTLState, tx DTLLendRepayTx) error {
	return ApplyDTLLendRepayTxWithHeight(state, 0, tx)
}

// ApplyDTLLendRepayTxWithHeight applies dtl lend repay tx with height.
func ApplyDTLLendRepayTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendRepayTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLLendRepayTx(state, tx); err != nil {
		return err
	}
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `err` stores the error produced by this operation.
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	// `position` stores the value produced by this operation.
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)

	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, market.DebtTokenID, account, vault, tx.Amount); err != nil {
		return err
	}
	position.Debt -= tx.Amount
	position.ScaledDebt = position.Debt
	market.TotalDebt -= tx.Amount
	if position.Debt == 0 && position.Collateral == 0 {
		delete(state.LendingPositions, dtlLendingPositionKey(marketID, account))
	}
	state.Events = append(state.Events, fmt.Sprintf("LEND_REPAY:%s:%s:%d", marketID, account, tx.Amount))
	return nil
}

// ApplyDTLLendWithdrawCollateralTx applies dtl lend withdraw collateral tx.
func ApplyDTLLendWithdrawCollateralTx(state *DTLState, tx DTLLendWithdrawCollateralTx) error {
	return ApplyDTLLendWithdrawCollateralTxWithHeight(state, 0, tx)
}

// ApplyDTLLendWithdrawCollateralTxWithHeight applies dtl lend withdraw collateral tx with height.
func ApplyDTLLendWithdrawCollateralTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendWithdrawCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLLendWithdrawCollateralTx(state, tx); err != nil {
		return err
	}
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `err` stores the error produced by this operation.
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	// `position` stores the value produced by this operation.
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	// `remainingCollateral` stores the value produced by this operation.
	remainingCollateral := position.Collateral - tx.Amount
	// `healthy` and `err` store the error produced by this operation.
	healthy, _, err := dtlLendingHealthFactorBPS(state, market, remainingCollateral, position.Debt, currentHeight)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: withdraw would make position unhealthy")
	}

	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, market.CollateralTokenID, vault, account, tx.Amount); err != nil {
		return err
	}
	position.Collateral = remainingCollateral
	position.ScaledDebt = position.Debt
	market.TotalCollateral -= tx.Amount
	if position.Debt == 0 && position.Collateral == 0 {
		delete(state.LendingPositions, dtlLendingPositionKey(marketID, account))
	}
	state.Events = append(state.Events, fmt.Sprintf("LEND_WITHDRAW:%s:%s:%d", marketID, account, tx.Amount))
	return nil
}

// ApplyDTLLendLiquidateTx applies dtl lend liquidate tx.
func ApplyDTLLendLiquidateTx(state *DTLState, tx DTLLendLiquidateTx) error {
	return ApplyDTLLendLiquidateTxWithHeight(state, 0, tx)
}

// ApplyDTLLendLiquidateTxWithHeight applies dtl lend liquidate tx with height.
func ApplyDTLLendLiquidateTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendLiquidateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLLendLiquidateTx(state, tx, currentHeight); err != nil {
		return err
	}
	// `liquidator` stores the value produced by this operation.
	liquidator := normalizeDTLAccount(tx.Liquidator)
	// `borrower` stores the value produced by this operation.
	borrower := normalizeDTLAccount(tx.Borrower)
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `err` stores the error produced by this operation.
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	// `position` stores the value produced by this operation.
	position := getOrCreateDTLLendingPosition(state, marketID, borrower)

	// `seize` and `err` store the error produced by this operation.
	seize, err := dtlLendingSeizeCollateral(tx.RepayAmount, market.LiquidationBonusBPS)
	if err != nil {
		return err
	}
	if seize > position.Collateral {
		seize = position.Collateral
	}
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, market.DebtTokenID, liquidator, vault, tx.RepayAmount); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, market.CollateralTokenID, vault, liquidator, seize); err != nil {
		return err
	}

	position.Debt -= tx.RepayAmount
	position.Collateral -= seize
	position.ScaledDebt = position.Debt
	market.TotalDebt -= tx.RepayAmount
	market.TotalCollateral -= seize
	if position.Debt == 0 && position.Collateral == 0 {
		delete(state.LendingPositions, dtlLendingPositionKey(marketID, borrower))
	}
	state.Events = append(state.Events, fmt.Sprintf("LEND_LIQUIDATE:%s:%s:%s:%d:%d", marketID, borrower, liquidator, tx.RepayAmount, seize))
	return nil
}

// ApplyDTLTournamentCreateTx applies dtl tournament create tx.
func ApplyDTLTournamentCreateTx(
	state *DTLState,
	chainID string,
	nonce uint64,
	currentHeight uint64,
	tx DTLTournamentCreateTx,
) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLTournamentCreateTx(state, tx, currentHeight); err != nil {
		return "", err
	}
	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `joinBlocks` stores the current position in the related collection.
	joinBlocks := tx.JoinExpiryBlocks
	if joinBlocks == 0 {
		joinBlocks = DTLDefaultTournamentJoinBlocks
	}
	// `revealBlocks` stores the value produced by this operation.
	revealBlocks := tx.RevealExpiryBlocks
	if revealBlocks == 0 {
		revealBlocks = DTLDefaultTournamentRevealBlocks
	}
	// `joinDeadline` stores the current position in the related collection.
	joinDeadline := currentHeight + joinBlocks
	// `revealDeadline` stores the value produced by this operation.
	revealDeadline := joinDeadline + revealBlocks
	// `beaconDelay` stores the value produced by this operation.
	beaconDelay := dtlProtocolBeaconDelayAtHeight(currentHeight)
	// `beaconHeight` stores the value produced by this operation.
	beaconHeight := revealDeadline
	if beaconDelay > 0 && beaconHeight <= ^uint64(0)-beaconDelay {
		beaconHeight += beaconDelay
	}
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(DTLTournamentIDFromCreate(chainID, nonce, tx))
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.Tournaments[tournamentID]; exists {
		return "", fmt.Errorf("dtl: tournament id collision")
	}
	state.Tournaments[tournamentID] = &DTLTournamentState{
		TournamentID:   tournamentID,
		TokenID:        tokenID,
		Creator:        creator,
		EntryFee:       tx.EntryFee,
		MaxPlayers:     tx.MaxPlayers,
		JoinDeadline:   joinDeadline,
		RevealDeadline: revealDeadline,
		Players:        make([]string, 0, tx.MaxPlayers),
		Commits:        make(map[string]string),
		Reveals:        make(map[string]string),
		BeaconHeight:   beaconHeight,
	}
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_CREATE:%s:%s", tournamentID, creator))
	return tournamentID, nil
}

// ApplyDTLTournamentJoinTx applies dtl tournament join tx.
func ApplyDTLTournamentJoinTx(state *DTLState, currentHeight uint64, tx DTLTournamentJoinTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLTournamentJoinTx(state, tx, currentHeight); err != nil {
		return err
	}
	// `player` stores the value produced by this operation.
	player := normalizeDTLAccount(tx.Player)
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	// `tournament` stores the value produced by this operation.
	tournament := state.Tournaments[tournamentID]
	// `commit` stores the value produced by this operation.
	commit := normalizeDTLHex(tx.CommitHash)
	// `vault` stores the value produced by this operation.
	vault := dtlTournamentVaultAccount(tournamentID)
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tournament.TokenID, player, vault, tournament.EntryFee); err != nil {
		return err
	}
	tournament.Players = append(tournament.Players, player)
	if tournament.Commits == nil {
		tournament.Commits = make(map[string]string)
	}
	tournament.Commits[player] = commit
	if tournament.Reveals == nil {
		tournament.Reveals = make(map[string]string)
	}
	// `newPot` and `err` store the error produced by this operation.
	newPot, err := dtlSafeAddU64(tournament.Pot, tournament.EntryFee)
	if err != nil {
		return err
	}
	tournament.Pot = newPot
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_JOIN:%s:%s", tournamentID, player))
	return nil
}

// ApplyDTLTournamentRevealTx applies dtl tournament reveal tx.
func ApplyDTLTournamentRevealTx(state *DTLState, currentHeight uint64, tx DTLTournamentRevealTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLTournamentRevealTx(state, tx, currentHeight); err != nil {
		return err
	}
	// `player` stores the value produced by this operation.
	player := normalizeDTLAccount(tx.Player)
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	// `tournament` stores the value produced by this operation.
	tournament := state.Tournaments[tournamentID]
	if tournament.Reveals == nil {
		tournament.Reveals = make(map[string]string)
	}
	tournament.Reveals[player] = strings.TrimSpace(tx.Secret)
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_REVEAL:%s:%s", tournamentID, player))
	// `err` stores the error produced by this operation.
	if err := dtlAddSeasonScore(state, currentHeight, player, dtlProtocolGameFiTournamentPartPoints(), "tournament_reveal"); err != nil {
		return err
	}
	return nil
}

// ApplyDTLTournamentFinalizeTx applies dtl tournament finalize tx.
func ApplyDTLTournamentFinalizeTx(state *DTLState, currentHeight uint64, tx DTLTournamentFinalizeTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLTournamentFinalizeTx(state, tx, currentHeight); err != nil {
		return err
	}
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	// `tournament` stores the value produced by this operation.
	tournament := state.Tournaments[tournamentID]
	// `vault` stores the value produced by this operation.
	vault := dtlTournamentVaultAccount(tournamentID)
	if len(tournament.Players) == 0 {
		tournament.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_CANCEL:%s", tournamentID))
		return nil
	}
	if tournament.BeaconHeight == 0 {
		tournament.BeaconHeight = tournament.RevealDeadline + dtlProtocolBeaconDelayAtHeight(currentHeight)
	}
	if currentHeight < tournament.BeaconHeight {
		return fmt.Errorf("dtl: tournament waiting for beacon height")
	}
	// `beaconHash` stores the digest used to identify or verify the related data.
	beaconHash := ""
	if tournament.BeaconHeight > tournament.RevealDeadline {
		beaconHash = dtlDeterministicBeaconHash(tournamentID, tournament.BeaconHeight)
	}
	tournament.BeaconHash = beaconHash

	// `candidates` stores the value produced by this operation.
	candidates := make([]string, 0, len(tournament.Players))
	// `player` tracks the current values while iterating.
	for _, player := range tournament.Players {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(player)
		if n == "" {
			continue
		}
		if strings.TrimSpace(tournament.Reveals[n]) == "" {
			continue
		}
		candidates = append(candidates, n)
	}

	if len(candidates) == 0 {
		// `player` tracks the current values while iterating.
		for _, player := range tournament.Players {
			// `n` stores the value produced by this operation.
			n := normalizeDTLAccount(player)
			if n == "" {
				continue
			}
			// `err` stores the error produced by this operation.
			if err := dtlMoveBalance(state, tournament.TokenID, vault, n, tournament.EntryFee); err != nil {
				return err
			}
		}
		tournament.Pot = 0
		tournament.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_REFUND:%s", tournamentID))
		return nil
	}

	// `winnerScope` stores the value produced by this operation.
	winnerScope := tournamentID
	if beaconHash != "" {
		tournament.FinalizationSeed = strings.Join([]string{tournamentID, beaconHash}, "|")
		winnerScope = tournamentID + "|" + beaconHash
	} else {
		tournament.FinalizationSeed = tournamentID
	}
	// `winner` stores the value produced by this operation.
	winner := dtlTournamentWinner(winnerScope, candidates, tournament.Reveals)
	if winner == "" {
		return fmt.Errorf("dtl: tournament winner resolution failed")
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, tournament.TokenID, vault, winner, tournament.Pot); err != nil {
		return err
	}
	tournament.Winner = winner
	tournament.Pot = 0
	tournament.Settled = true
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_FINALIZE:%s:%s", tournamentID, winner))
	// `err` stores the error produced by this operation.
	if err := dtlAddSeasonScore(state, currentHeight, winner, dtlProtocolGameFiTournamentWinPoints(), "tournament_finalize"); err != nil {
		return err
	}
	return nil
}

// ApplyDTLOracleFeedCreateTx applies dtl oracle feed create tx.
func ApplyDTLOracleFeedCreateTx(state *DTLState, chainID string, nonce uint64, tx DTLOracleFeedCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLOracleFeedCreateTx(state, tx); err != nil {
		return "", err
	}
	// `feedID` stores the value produced by this operation.
	feedID := normalizeDTLTokenID(dtlOracleFeedIDFromCreate(chainID, nonce, tx))
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.OracleFeeds[feedID]; exists {
		return "", fmt.Errorf("dtl: oracle feed id collision")
	}
	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(tx.Signers))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(tx.Signers))
	// `signer` tracks the current values while iterating.
	for _, signer := range tx.Signers {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(signer)
		if n == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		signers = append(signers, n)
	}
	sort.Strings(signers)
	state.OracleFeeds[feedID] = &DTLOracleFeedState{
		FeedID:       feedID,
		BaseTokenID:  normalizeDTLTokenID(tx.BaseTokenID),
		QuoteTokenID: normalizeDTLTokenID(tx.QuoteTokenID),
		Signers:      signers,
		Threshold:    tx.Threshold,
		Decimals:     tx.Decimals,
	}
	state.Events = append(state.Events, fmt.Sprintf("ORACLE_FEED_CREATE:%s", feedID))
	return feedID, nil
}

// ApplyDTLOraclePriceSubmitTx applies dtl oracle price submit tx.
func ApplyDTLOraclePriceSubmitTx(state *DTLState, currentHeight uint64, tx DTLOraclePriceSubmitTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLOraclePriceSubmitTx(state, tx, currentHeight); err != nil {
		return err
	}
	// `feedID` stores the value produced by this operation.
	feedID := normalizeDTLTokenID(tx.FeedID)
	// `feed` stores the value produced by this operation.
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		return fmt.Errorf("dtl: unknown oracle feed")
	}
	// `submitter` stores the value produced by this operation.
	submitter := normalizeDTLAccount(tx.Submitter)
	if state.OracleSamples[feedID] == nil {
		state.OracleSamples[feedID] = make(map[string]DTLOracleSampleState)
	}
	state.OracleSamples[feedID][submitter] = DTLOracleSampleState{
		FeedID: feedID,
		Signer: submitter,
		Price:  tx.Price,
		Height: currentHeight,
	}
	// `prices` stores the value produced by this operation.
	prices := make([]uint64, 0, len(feed.Signers))
	// `signer` tracks the current values while iterating.
	for _, signer := range feed.Signers {
		// `s` stores the value produced by this operation.
		s := state.OracleSamples[feedID][normalizeDTLAccount(signer)]
		if s.Price == 0 {
			continue
		}
		// `maxStaleness` stores the value produced by this operation.
		maxStaleness := dtlProtocolOracleMaxStalenessBlocks()
		if maxStaleness > 0 && currentHeight >= s.Height &&
			currentHeight-s.Height > maxStaleness {
			continue
		}
		prices = append(prices, s.Price)
	}
	if len(prices) >= int(feed.Threshold) {
		sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
		// `median` stores the value produced by this operation.
		median := prices[len(prices)/2]
		if len(prices)%2 == 0 {
			// `sum` and `err` store the error produced by this operation.
			sum, err := dtlSafeAddU64(prices[len(prices)/2-1], prices[len(prices)/2])
			if err != nil {
				return err
			}
			median = sum / 2
		}
		feed.LastMedianPrice = median
		feed.LastUpdateHeight = currentHeight
	}
	state.Events = append(state.Events, fmt.Sprintf("ORACLE_PRICE_SUBMIT:%s:%s:%d", feedID, submitter, tx.Price))
	return nil
}

// ApplyDTLContractDeployTx permanently rejects the removed programmable
// contract/VM transaction class. Native DTL transactions remain supported.
func ApplyDTLContractDeployTx(_ *DTLState, _ string, _ uint64, _ DTLContractDeployTx) (string, error) {
	return "", dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
}

// ApplyDTLContractCallTx permanently rejects the removed programmable
// contract/VM transaction class.
func ApplyDTLContractCallTx(_ *DTLState, _ DTLContractCallTx) error {
	return dtlContractRuntimeRemovedError("CONTRACT_CALL")
}

// ApplyDTLContractCallTxWithContext permanently rejects the removed
// programmable contract/VM transaction class.
func ApplyDTLContractCallTxWithContext(_ *DTLState, _ DTLContractCallTx, _ uint64, _ string) error {
	return dtlContractRuntimeRemovedError("CONTRACT_CALL")
}

// ApplyDTLMintTx applies dtl mint tx.
func ApplyDTLMintTx(
	state *DTLState,
	tx DTLMintTx,
	cert DTLGovernanceCert,
	currentEpoch uint64,
	replayWindow uint64,
) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLMintTx(state, tx); err != nil {
		return err
	}
	if replayWindow == 0 {
		replayWindow = DTLDefaultReplayWindow
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` stores the value produced by this operation.
	token := state.Tokens[tokenID]

	// `payloadHash` and `err` store the error produced by this operation.
	payloadHash, err := DTLPayloadHash(struct {
		// `TokenID` stores the value associated with this record.
		TokenID string `json:"token_id"`
		// `To` stores the value associated with this record.
		To string `json:"to"`
		// `Amount` stores the value associated with this record.
		Amount uint64 `json:"amount"`
	}{
		TokenID: tokenID,
		To:      normalizeDTLAccount(tx.To),
		Amount:  tx.Amount,
	})
	if err != nil {
		return err
	}

	// `err` stores the error produced by this operation.
	if err := ValidateDTLGovernanceCert(
		token,
		cert,
		DTLGovMint,
		payloadHash,
		currentEpoch,
		replayWindow,
	); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMarkReplay(state, cert); err != nil {
		return err
	}

	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	// `toKey` stores the key used to access the related value.
	toKey := dtlBalanceKey(tokenID, to)
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.Balances, toKey, tx.Amount); err != nil {
		return err
	}
	token.TotalSupply += tx.Amount
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_MINT:%s:%s:%d", tokenID, to, tx.Amount))
	return nil
}

// ApplyDTLGovernanceAction applies dtl governance action.
func ApplyDTLGovernanceAction(
	state *DTLState,
	tokenID string,
	action DTLGovernanceAction,
	payload any,
	cert DTLGovernanceCert,
	currentEpoch uint64,
	replayWindow uint64,
) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if replayWindow == 0 {
		replayWindow = DTLDefaultReplayWindow
	}

	tokenID = normalizeDTLTokenID(tokenID)
	// `token` stores the value produced by this operation.
	token := state.Tokens[tokenID]
	if token == nil {
		return ErrDTLUnknownToken
	}

	if action == DTLGovRotateAuthority {
		// `p` and `ok` store whether the related condition is satisfied.
		p, ok := payload.(DTLRotateAuthorityPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid rotate authority payload")
		}
		// `err` stores the error produced by this operation.
		if err := ValidateDTLRotateAuthorityPayload(p); err != nil {
			return err
		}
	}

	// `payloadHash` and `err` store the error produced by this operation.
	payloadHash, err := DTLPayloadHash(payload)
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := ValidateDTLGovernanceCert(
		token,
		cert,
		action,
		payloadHash,
		currentEpoch,
		replayWindow,
	); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := dtlMarkReplay(state, cert); err != nil {
		return err
	}

	switch action {
	case DTLGovPause:
		token.Paused = true
	case DTLGovUnpause:
		token.Paused = false
	case DTLGovFreezeAccount:
		// `p` and `ok` store whether the related condition is satisfied.
		p, ok := payload.(DTLFreezeAccountPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid freeze payload")
		}
		// `account` stores the measured quantity used by this operation.
		account := normalizeDTLAccount(p.Account)
		if account == "" {
			return fmt.Errorf("dtl: invalid freeze account")
		}
		if !token.FreezeEnabled {
			return fmt.Errorf("dtl: freeze is disabled for token")
		}
		if state.FrozenAccounts[tokenID] == nil {
			state.FrozenAccounts[tokenID] = make(map[string]bool)
		}
		state.FrozenAccounts[tokenID][account] = true
	case DTLGovUnfreezeAccount:
		// `p` and `ok` store whether the related condition is satisfied.
		p, ok := payload.(DTLFreezeAccountPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid unfreeze payload")
		}
		// `account` stores the measured quantity used by this operation.
		account := normalizeDTLAccount(p.Account)
		if account == "" {
			return fmt.Errorf("dtl: invalid unfreeze account")
		}
		if state.FrozenAccounts[tokenID] == nil {
			state.FrozenAccounts[tokenID] = make(map[string]bool)
		}
		delete(state.FrozenAccounts[tokenID], account)
	case DTLGovRotateAuthority:
		// `p` and `ok` store whether the related condition is satisfied.
		p, ok := payload.(DTLRotateAuthorityPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid rotate payload")
		}
		token.AuthoritySigners = uniqueDTLSigners(p.AuthoritySigners)
		token.AuthorityThreshold = p.AuthorityThreshold
	default:
		return fmt.Errorf("dtl: unsupported governance action %s", action)
	}

	state.Events = append(state.Events, fmt.Sprintf("TOKEN_GOV:%s:%s", tokenID, action))
	return nil
}

// dtlAddBalance implements the dtl add balance helper.
func dtlAddBalance(m map[string]uint64, key string, amount uint64) error {
	if amount == 0 {
		return nil
	}
	// `current` stores the value produced by this operation.
	current := m[key]
	if current > ^uint64(0)-amount {
		return errors.New("dtl: balance overflow")
	}
	m[key] = current + amount
	return nil
}

// dtlMoveBalance implements the dtl move balance helper.
func dtlMoveBalance(state *DTLState, tokenIDRaw, fromRaw, toRaw string, amount uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if amount == 0 {
		return nil
	}
	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tokenIDRaw)
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(fromRaw)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(toRaw)
	if from == "" || to == "" {
		return fmt.Errorf("dtl: invalid transfer endpoint")
	}
	if from == to {
		return nil
	}
	// `fromKey` stores the key used to access the related value.
	fromKey := dtlBalanceKey(tokenID, from)
	if state.Balances[fromKey] < amount {
		return ErrDTLInsufficientFunds
	}
	state.Balances[fromKey] -= amount
	return dtlAddBalance(state.Balances, dtlBalanceKey(tokenID, to), amount)
}

// uniqueDTLSigners returns the canonical normalized authority signer set.
func uniqueDTLSigners(signers []string) []string {
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(signers))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(signers))
	// `signer` tracks the current values while iterating.
	for _, signer := range signers {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(signer)
		if n == "" {
			continue
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := seen[n]; exists {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// canonicalDTLInterfaces returns a deterministic contract interface set.
func canonicalDTLInterfaces(interfaces []string) []string {
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(interfaces))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(interfaces))
	// `iface` tracks the current position in the related collection.
	for _, iface := range interfaces {
		// `n` stores the value produced by this operation.
		n := strings.TrimSpace(iface)
		if n == "" {
			continue
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := seen[n]; exists {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// dtlReplayKey implements the dtl replay key helper.
func dtlReplayKey(cert DTLGovernanceCert) string {
	if dtlGovernanceCertHasV2Fields(cert) {
		return normalizeDTLTokenID(cert.TokenID) + "|v2|nonce|" + normalizeDTLGovernanceNonce(cert.Nonce)
	}
	return normalizeDTLTokenID(cert.TokenID) + "|" + string(cert.Action) + "|" + normalizeDTLHex(cert.ActionPayloadHash)
}

// dtlReplaySequenceKey returns the per-token v2 governance sequence key.
func dtlReplaySequenceKey(cert DTLGovernanceCert) string {
	return normalizeDTLTokenID(cert.TokenID) + "|v2|sequence"
}

// dtlCheckReplay validates governance replay state without mutating it.
func dtlCheckReplay(state *DTLState, cert DTLGovernanceCert) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `key` stores the key used to access the related value.
	key := dtlReplayKey(cert)
	if dtlGovernanceCertHasV2Fields(cert) {
		if _, exists := state.GovernanceReplay[key]; exists {
			return ErrDTLReplay
		}
		// `lastSequence` stores the latest committed governance sequence.
		lastSequence := state.GovernanceReplay[dtlReplaySequenceKey(cert)]
		if cert.Sequence <= lastSequence {
			return ErrDTLReplay
		}
		return nil
	}
	// `last` and `exists` store whether the related condition is satisfied.
	last, exists := state.GovernanceReplay[key]
	if exists && cert.Epoch <= last {
		return ErrDTLReplay
	}
	return nil
}

// dtlMarkReplay implements the dtl mark replay helper.
func dtlMarkReplay(state *DTLState, cert DTLGovernanceCert) error {
	if err := dtlCheckReplay(state, cert); err != nil {
		return err
	}
	// `key` stores the key used to access the related value.
	key := dtlReplayKey(cert)
	if dtlGovernanceCertHasV2Fields(cert) {
		state.GovernanceReplay[key] = cert.Epoch
		state.GovernanceReplay[dtlReplaySequenceKey(cert)] = cert.Sequence
		return nil
	}
	state.GovernanceReplay[key] = cert.Epoch
	return nil
}
