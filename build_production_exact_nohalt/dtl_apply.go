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

const DTLDefaultReplayWindow uint64 = 256

const dtlLendingIndexScale uint64 = 1_000_000

func dtlOracleFeedIDFromCreate(chainID string, nonce uint64, tx DTLOracleFeedCreateTx) string {
	base := normalizeDTLTokenID(tx.BaseTokenID)
	quote := normalizeDTLTokenID(tx.QuoteTokenID)
	if id := normalizeDTLTokenID(tx.FeedID); id != "" {
		return id
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(chainID),
		strconv.FormatUint(nonce, 10),
		base,
		quote,
	}, "|")))
	return "orcl" + hex.EncodeToString(sum[:12])
}

func dtlMulBPSAndBlocks(value uint64, rateBPS uint16, blocks uint64) (uint64, error) {
	if value == 0 || rateBPS == 0 || blocks == 0 {
		return 0, nil
	}
	num := new(big.Int).SetUint64(value)
	num.Mul(num, new(big.Int).SetUint64(uint64(rateBPS)))
	num.Mul(num, new(big.Int).SetUint64(blocks))
	num.Div(num, big.NewInt(DTLMaxTaxBPS))
	if num.Sign() < 0 || num.BitLen() > 64 {
		return 0, fmt.Errorf("dtl: interest overflow")
	}
	return num.Uint64(), nil
}

func dtlDeterministicBeaconHash(scope string, height uint64) string {
	payload := strings.Join([]string{
		"msc-dtl-beacon",
		strings.TrimSpace(scope),
		strconv.FormatUint(height, 10),
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func dtlOracleMedianPrice(state *DTLState, feedID string, currentHeight uint64) (uint64, bool) {
	if state == nil {
		return 0, false
	}
	state.ensure()
	feedID = normalizeDTLTokenID(feedID)
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		return 0, false
	}
	if feed.LastMedianPrice == 0 {
		return 0, false
	}
	if ConfigDTLOracleMaxStalenessBlocks == 0 {
		return feed.LastMedianPrice, true
	}
	if currentHeight >= feed.LastUpdateHeight && currentHeight-feed.LastUpdateHeight > ConfigDTLOracleMaxStalenessBlocks {
		return 0, false
	}
	return feed.LastMedianPrice, true
}

func dtlPoolProtocolFeeCut(amountIn uint64, feeBPS, protocolFeeBPS uint16) (uint64, error) {
	if amountIn == 0 || feeBPS == 0 || protocolFeeBPS == 0 {
		return 0, nil
	}
	feeAmount, err := dtlMulDivU64(amountIn, uint64(feeBPS), DTLMaxTaxBPS)
	if err != nil || feeAmount == 0 {
		return 0, err
	}
	return dtlMulDivU64(feeAmount, uint64(protocolFeeBPS), DTLMaxTaxBPS)
}

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
	collPrice, hasColl := dtlOracleMedianPrice(state, market.CollateralFeedID, currentHeight)
	debtPrice, hasDebt := dtlOracleMedianPrice(state, market.DebtFeedID, currentHeight)
	if !hasColl || !hasDebt || collPrice == 0 || debtPrice == 0 {
		healthy, err := dtlLendingIsHealthy(collateral, debt, market.CollateralFactorBPS)
		if err != nil {
			return false, 0, err
		}
		maxDebt, err := dtlLendingMaxDebt(collateral, market.CollateralFactorBPS)
		if err != nil {
			return false, 0, err
		}
		if debt == 0 {
			return healthy, DTLMaxTaxBPS, nil
		}
		hf, err := dtlMulDivU64(maxDebt, DTLMaxTaxBPS, debt)
		if err != nil {
			return healthy, 0, err
		}
		return healthy, hf, nil
	}

	collValue := new(big.Int).SetUint64(collateral)
	collValue.Mul(collValue, new(big.Int).SetUint64(collPrice))
	maxDebtValue := new(big.Int).Mul(collValue, new(big.Int).SetUint64(uint64(market.CollateralFactorBPS)))
	maxDebtValue.Div(maxDebtValue, big.NewInt(DTLMaxTaxBPS))
	debtValue := new(big.Int).SetUint64(debt)
	debtValue.Mul(debtValue, new(big.Int).SetUint64(debtPrice))
	healthy := maxDebtValue.Cmp(debtValue) >= 0
	if debtValue.Sign() == 0 {
		return healthy, DTLMaxTaxBPS, nil
	}
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

func dtlLendingAccrueMarket(state *DTLState, marketID string, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
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
	blocks := currentHeight - market.LastAccrualHeight
	interval := ConfigDTLLendingAccrualIntervalBlocks
	if interval == 0 {
		interval = 1
	}
	if blocks < interval {
		return nil
	}
	utilBPS := uint64(0)
	if market.TotalCollateral > 0 && market.TotalDebt > 0 {
		v, err := dtlMulDivU64(market.TotalDebt, DTLMaxTaxBPS, market.TotalCollateral)
		if err != nil {
			return err
		}
		if v > DTLMaxTaxBPS {
			v = DTLMaxTaxBPS
		}
		utilBPS = v
	}
	base := uint64(market.BaseBorrowRateBPS)
	slope := uint64(market.SlopeBorrowRateBPS)
	rateSlope, err := dtlMulDivU64(slope, utilBPS, DTLMaxTaxBPS)
	if err != nil {
		return err
	}
	rateBPS := uint16(base + rateSlope)
	if rateBPS == 0 {
		market.LastAccrualHeight = currentHeight
		return nil
	}
	totalIncrease, err := dtlMulBPSAndBlocks(market.TotalDebt, rateBPS, blocks)
	if err != nil {
		return err
	}
	if totalIncrease > 0 {
		nextTotalDebt, err := dtlSafeAddU64(market.TotalDebt, totalIncrease)
		if err != nil {
			return err
		}
		market.TotalDebt = nextTotalDebt
		for key, position := range state.LendingPositions {
			if position == nil || normalizeDTLMarketID(position.MarketID) != normalizeDTLMarketID(marketID) {
				continue
			}
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
		indexInc, err := dtlMulBPSAndBlocks(market.BorrowIndex, rateBPS, blocks)
		if err != nil {
			return err
		}
		market.BorrowIndex, err = dtlSafeAddU64(market.BorrowIndex, indexInc)
		if err != nil {
			return err
		}
		// DeFi -> GameFi treasury share from lending accrual (reward token only).
		if shareBPS := dtlGameFiFeeShareFromLendingBPS(); shareBPS > 0 {
			if seasonID, ok := dtlActiveRewardSeasonID(state, currentHeight, market.DebtTokenID); ok {
				seasonShare, err := dtlMulDivU64(totalIncrease, uint64(shareBPS), DTLMaxTaxBPS)
				if err != nil {
					return err
				}
				if seasonShare > 0 {
					vault := dtlLendingVaultAccount(marketID)
					available := state.BalanceOf(market.DebtTokenID, vault)
					if seasonShare > available {
						seasonShare = available
					}
					if seasonShare > 0 {
						if err := dtlMoveBalance(state, market.DebtTokenID, vault, DTLTreasuryAccount, seasonShare); err != nil {
							return err
						}
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

func dtlPoolBumpTWAP(pool *DTLPoolState) {
	if pool == nil || pool.ReserveA == 0 || pool.ReserveB == 0 {
		return
	}
	priceA, errA := dtlMulDivU64(pool.ReserveB, DTLMaxTaxBPS, pool.ReserveA)
	priceB, errB := dtlMulDivU64(pool.ReserveA, DTLMaxTaxBPS, pool.ReserveB)
	if errA == nil {
		if next, err := dtlSafeAddU64(pool.PriceCumulativeA, priceA); err == nil {
			pool.PriceCumulativeA = next
		}
	}
	if errB == nil {
		if next, err := dtlSafeAddU64(pool.PriceCumulativeB, priceB); err == nil {
			pool.PriceCumulativeB = next
		}
	}
	if pool.LastTwapHeight < ^uint64(0) {
		pool.LastTwapHeight++
	}
}

func ApplyDTLCreateTx(state *DTLState, chainID string, nonce uint64, tx DTLCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()

	if err := ValidateDTLCreateTx(state, tx); err != nil {
		return "", err
	}

	tokenID := DTLTokenIDFromCreate(chainID, tx, nonce)
	tokenID = normalizeDTLTokenID(tokenID)
	if _, exists := state.Tokens[tokenID]; exists {
		return "", fmt.Errorf("dtl: token id collision")
	}

	signers := make([]string, 0, len(tx.AuthoritySigners))
	for _, signer := range tx.AuthoritySigners {
		signers = append(signers, normalizeDTLAccount(signer))
	}

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

	creator := normalizeDTLAccount(tx.Creator)
	if tx.InitialSupply > 0 {
		state.Balances[dtlBalanceKey(tokenID, creator)] = tx.InitialSupply
	}
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_CREATE:%s", tokenID))
	return tokenID, nil
}

func applyDTLTransferWithTax(state *DTLState, tokenID, from, to string, amount uint64) error {
	token := state.Tokens[tokenID]
	fromKey := dtlBalanceKey(tokenID, from)
	toKey := dtlBalanceKey(tokenID, to)

	tax := uint64(0)
	if token.TaxBPS > 0 {
		tax = (amount * uint64(token.TaxBPS)) / DTLMaxTaxBPS
	}
	net := amount - tax

	state.Balances[fromKey] -= amount
	if err := dtlAddBalance(state.Balances, toKey, net); err != nil {
		return err
	}
	if tax > 0 {
		treasuryKey := dtlBalanceKey(tokenID, DTLTreasuryAccount)
		if err := dtlAddBalance(state.Balances, treasuryKey, tax); err != nil {
			return err
		}
	}
	return nil
}

func ApplyDTLTransferTx(state *DTLState, tx DTLTransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLTransferTx(state, tx); err != nil {
		return err
	}

	tokenID := normalizeDTLTokenID(tx.TokenID)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)

	if err := applyDTLTransferWithTax(state, tokenID, from, to, tx.Amount); err != nil {
		return err
	}

	state.Events = append(state.Events, fmt.Sprintf("TOKEN_TRANSFER:%s:%s->%s:%d", tokenID, from, to, tx.Amount))
	return nil
}

func ApplyDTLApproveTx(state *DTLState, tx DTLApproveTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLApproveTx(state, tx); err != nil {
		return err
	}

	tokenID := normalizeDTLTokenID(tx.TokenID)
	owner := normalizeDTLAccount(tx.Owner)
	spender := normalizeDTLAccount(tx.Spender)
	allowanceKey := dtlAllowanceKey(tokenID, owner, spender)
	if tx.Amount == 0 {
		delete(state.Allowances, allowanceKey)
	} else {
		state.Allowances[allowanceKey] = tx.Amount
	}
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_APPROVE:%s:%s->%s:%d", tokenID, owner, spender, tx.Amount))
	return nil
}

func ApplyDTLTransferFromTx(state *DTLState, tx DTLTransferFromTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLTransferFromTx(state, tx); err != nil {
		return err
	}

	tokenID := normalizeDTLTokenID(tx.TokenID)
	spender := normalizeDTLAccount(tx.Spender)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)

	allowanceKey := dtlAllowanceKey(tokenID, from, spender)
	nextAllowance := state.Allowances[allowanceKey] - tx.Amount
	if nextAllowance == 0 {
		delete(state.Allowances, allowanceKey)
	} else {
		state.Allowances[allowanceKey] = nextAllowance
	}
	if err := applyDTLTransferWithTax(state, tokenID, from, to, tx.Amount); err != nil {
		return err
	}
	state.Events = append(
		state.Events,
		fmt.Sprintf("TOKEN_TRANSFER_FROM:%s:%s->%s:by=%s:%d", tokenID, from, to, spender, tx.Amount),
	)
	return nil
}

func ApplyDTLBurnTx(state *DTLState, tx DTLBurnTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLBurnTx(state, tx); err != nil {
		return err
	}

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token := state.Tokens[tokenID]
	from := normalizeDTLAccount(tx.From)
	fromKey := dtlBalanceKey(tokenID, from)

	state.Balances[fromKey] -= tx.Amount
	token.TotalSupply -= tx.Amount
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_BURN:%s:%s:%d", tokenID, from, tx.Amount))
	return nil
}

func ApplyDTLNFT721CreateTx(state *DTLState, chainID string, nonce uint64, tx DTLNFT721CreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLNFT721CreateTx(state, tx); err != nil {
		return "", err
	}

	collectionID := normalizeDTLCollectionID(DTLNFT721CollectionIDFromCreate(chainID, tx, nonce))
	if _, exists := state.NFT721Collections[collectionID]; exists {
		return "", fmt.Errorf("dtl: nft721 collection id collision")
	}
	symbol := normalizeDTLSymbol(tx.Symbol)
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

func ApplyDTLNFT721MintTx(state *DTLState, tx DTLNFT721MintTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLNFT721MintTx(state, tx); err != nil {
		return 0, err
	}

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	collection := state.NFT721Collections[collectionID]
	if collection.NextTokenID == ^uint64(0) {
		return 0, fmt.Errorf("dtl: nft721 token id overflow")
	}
	tokenID := collection.NextTokenID + 1
	ownerKey := dtlNFT721OwnerKey(collectionID, tokenID)
	if existing := normalizeDTLAccount(state.NFT721Owners[ownerKey]); existing != "" {
		return 0, fmt.Errorf("dtl: nft721 token already minted")
	}
	to := normalizeDTLAccount(tx.To)
	state.NFT721Owners[ownerKey] = to
	tokenURI := strings.TrimSpace(tx.TokenURI)
	if tokenURI != "" {
		state.NFT721TokenURIs[ownerKey] = tokenURI
	}
	collection.NextTokenID = tokenID
	collection.TotalMinted++
	state.Events = append(state.Events, fmt.Sprintf("NFT721_MINT:%s:%d:%s", collectionID, tokenID, to))
	return tokenID, nil
}

func ApplyDTLNFT721TransferTx(state *DTLState, tx DTLNFT721TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLNFT721TransferTx(state, tx); err != nil {
		return err
	}

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	ownerKey := dtlNFT721OwnerKey(collectionID, tx.TokenID)
	to := normalizeDTLAccount(tx.To)
	state.NFT721Owners[ownerKey] = to
	state.Events = append(
		state.Events,
		fmt.Sprintf("NFT721_TRANSFER:%s:%d:%s->%s", collectionID, tx.TokenID, normalizeDTLAccount(tx.From), to),
	)
	return nil
}

func ApplyDTLNFT1155CreateTx(state *DTLState, chainID string, nonce uint64, tx DTLNFT1155CreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLNFT1155CreateTx(state, tx); err != nil {
		return "", err
	}

	collectionID := normalizeDTLCollectionID(DTLNFT1155CollectionIDFromCreate(chainID, tx, nonce))
	if _, exists := state.NFT1155Collections[collectionID]; exists {
		return "", fmt.Errorf("dtl: nft1155 collection id collision")
	}
	symbol := normalizeDTLSymbol(tx.Symbol)
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

func ApplyDTLNFT1155MintTx(state *DTLState, tx DTLNFT1155MintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLNFT1155MintTx(state, tx); err != nil {
		return err
	}

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	to := normalizeDTLAccount(tx.To)
	balanceKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, to)
	supplyKey := dtlNFT1155SupplyKey(collectionID, tx.TokenID)
	if err := dtlAddBalance(state.NFT1155Balances, balanceKey, tx.Amount); err != nil {
		return err
	}
	if err := dtlAddBalance(state.NFT1155Supplies, supplyKey, tx.Amount); err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("NFT1155_MINT:%s:%d:%s:%d", collectionID, tx.TokenID, to, tx.Amount))
	return nil
}

func ApplyDTLNFT1155TransferTx(state *DTLState, tx DTLNFT1155TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLNFT1155TransferTx(state, tx); err != nil {
		return err
	}

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	fromKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, from)
	toKey := dtlNFT1155BalanceKey(collectionID, tx.TokenID, to)
	state.NFT1155Balances[fromKey] -= tx.Amount
	if state.NFT1155Balances[fromKey] == 0 {
		delete(state.NFT1155Balances, fromKey)
	}
	if err := dtlAddBalance(state.NFT1155Balances, toKey, tx.Amount); err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("NFT1155_TRANSFER:%s:%d:%s->%s:%d", collectionID, tx.TokenID, from, to, tx.Amount))
	return nil
}

func canonicalizeDTLPoolPair(tokenA, tokenB string, amountA, amountB uint64) (string, string, uint64, uint64) {
	a := normalizeDTLTokenID(tokenA)
	b := normalizeDTLTokenID(tokenB)
	if a <= b {
		return a, b, amountA, amountB
	}
	return b, a, amountB, amountA
}

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
	if err := ValidateDTLPoolCreateTx(state, tx); err != nil {
		return "", err
	}

	creator := normalizeDTLAccount(tx.Creator)
	tokenA, tokenB, amountA, amountB := canonicalizeDTLPoolPair(
		tx.TokenA,
		tx.TokenB,
		tx.AmountA,
		tx.AmountB,
	)
	pairKey := dtlPoolPairKey(tokenA, tokenB)
	poolID := normalizeDTLPoolID(DTLPoolIDFromTokens(chainID, tokenA, tokenB))
	if existing := normalizeDTLPoolID(state.PoolIndex[pairKey]); existing != "" {
		return "", fmt.Errorf("dtl: pool already exists for pair: %s", existing)
	}
	if _, exists := state.Pools[poolID]; exists {
		return "", fmt.Errorf("dtl: pool id collision")
	}

	share, err := dtlInitialPoolShare(amountA, amountB)
	if err != nil {
		return "", err
	}
	if share == 0 {
		return "", fmt.Errorf("dtl: initial LP share must be > 0")
	}
	feeBPS := tx.FeeBPS
	if feeBPS == 0 {
		feeBPS = DTLDefaultPoolFeeBPS
	}

	vault := dtlPoolVaultAccount(poolID)
	if err := dtlMoveBalance(state, tokenA, creator, vault, amountA); err != nil {
		return "", err
	}
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
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(poolID, creator), share); err != nil {
		return "", err
	}
	state.Events = append(state.Events, fmt.Sprintf("POOL_CREATE:%s:%s/%s", poolID, tokenA, tokenB))
	return poolID, nil
}

func ApplyDTLPoolAddLiquidityTx(state *DTLState, tx DTLPoolAddLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLPoolAddLiquidityTx(state, tx); err != nil {
		return err
	}

	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	provider := normalizeDTLAccount(tx.Provider)
	share, err := dtlLiquidityShareMint(pool, tx.AmountA, tx.AmountB)
	if err != nil {
		return err
	}
	if tx.MinLPShares > 0 && share < tx.MinLPShares {
		return fmt.Errorf("dtl: slippage: LP share below minimum")
	}

	vault := dtlPoolVaultAccount(poolID)
	if err := dtlMoveBalance(state, pool.TokenA, provider, vault, tx.AmountA); err != nil {
		return err
	}
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
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(poolID, provider), share); err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("POOL_ADD:%s:%s:%d", poolID, provider, share))
	return nil
}

func ApplyDTLPoolRemoveLiquidityTx(state *DTLState, tx DTLPoolRemoveLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLPoolRemoveLiquidityTx(state, tx); err != nil {
		return err
	}

	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	provider := normalizeDTLAccount(tx.Provider)
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

	lpKey := dtlLPBalanceKey(poolID, provider)
	if state.LPBalances[lpKey] < tx.LPShares {
		return fmt.Errorf("dtl: insufficient LP balance")
	}
	state.LPBalances[lpKey] -= tx.LPShares
	pool.TotalLPShares -= tx.LPShares
	pool.ReserveA -= outA
	pool.ReserveB -= outB
	dtlPoolBumpTWAP(pool)

	vault := dtlPoolVaultAccount(poolID)
	if err := dtlMoveBalance(state, pool.TokenA, vault, provider, outA); err != nil {
		return err
	}
	if err := dtlMoveBalance(state, pool.TokenB, vault, provider, outB); err != nil {
		return err
	}

	state.Events = append(state.Events, fmt.Sprintf("POOL_REMOVE:%s:%s:%d", poolID, provider, tx.LPShares))
	return nil
}

func ApplyDTLPoolSwapTx(state *DTLState, tx DTLPoolSwapTx) (uint64, error) {
	return ApplyDTLPoolSwapTxWithHeight(state, 0, tx)
}

func ApplyDTLPoolSwapTxWithHeight(state *DTLState, currentHeight uint64, tx DTLPoolSwapTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLPoolSwapTx(state, tx); err != nil {
		return 0, err
	}

	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	trader := normalizeDTLAccount(tx.Trader)
	tokenIn := normalizeDTLTokenID(tx.TokenIn)

	tokenOut := pool.TokenB
	reserveIn := pool.ReserveA
	reserveOut := pool.ReserveB
	inIsA := true
	if tokenIn == pool.TokenB {
		tokenOut = pool.TokenA
		reserveIn = pool.ReserveB
		reserveOut = pool.ReserveA
		inIsA = false
	}

	amountOut, err := dtlPoolSwapOutAmount(reserveIn, reserveOut, tx.AmountIn, pool.FeeBPS)
	if err != nil {
		return 0, err
	}
	if tx.MinAmountOut > 0 && amountOut < tx.MinAmountOut {
		return 0, fmt.Errorf("dtl: slippage: output below minimum")
	}

	vault := dtlPoolVaultAccount(poolID)
	if err := dtlMoveBalance(state, tokenIn, trader, vault, tx.AmountIn); err != nil {
		return 0, err
	}
	if err := dtlMoveBalance(state, tokenOut, vault, trader, amountOut); err != nil {
		return 0, err
	}
	protocolFeeCut, err := dtlPoolProtocolFeeCut(tx.AmountIn, pool.FeeBPS, pool.ProtocolFeeBPS)
	if err != nil {
		return 0, err
	}
	if protocolFeeCut > 0 {
		if seasonID, ok := dtlActiveRewardSeasonID(state, currentHeight, tokenIn); ok {
			shareBPS := dtlGameFiFeeShareFromPoolBPS()
			if shareBPS > 0 {
				seasonShare, err := dtlMulDivU64(protocolFeeCut, uint64(shareBPS), DTLMaxTaxBPS)
				if err != nil {
					return 0, err
				}
				if seasonShare > protocolFeeCut {
					seasonShare = protocolFeeCut
				}
				if seasonShare > 0 {
					if err := dtlMoveBalance(state, tokenIn, vault, DTLTreasuryAccount, seasonShare); err != nil {
						return 0, err
					}
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
		protocolAccount := normalizeDTLAccount(pool.ProtocolFeeAccount)
		if protocolAccount == "" {
			protocolAccount = DTLTreasuryAccount
		}
		if protocolAccount != vault {
			if err := dtlMoveBalance(state, tokenIn, vault, protocolAccount, protocolFeeCut); err != nil {
				return 0, err
			}
		}
	}
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
	PoolID    string `json:"pool_id"`
	TokenIn   string `json:"token_in"`
	TokenOut  string `json:"token_out"`
	AmountIn  uint64 `json:"amount_in"`
	AmountOut uint64 `json:"amount_out"`
	FeeBPS    uint16 `json:"fee_bps"`
}

type DTLRouteQuote struct {
	Path           []string           `json:"path"`
	TokenIn        string             `json:"token_in"`
	TokenOut       string             `json:"token_out"`
	AmountIn       uint64             `json:"amount_in"`
	AmountOut      uint64             `json:"amount_out"`
	PriceImpactBPS uint16             `json:"price_impact_bps"`
	Hops           []DTLRouteHopQuote `json:"hops"`
}

func dtlRoutePriceImpactBPS(expectedOut, actualOut uint64) uint16 {
	if expectedOut == 0 || actualOut >= expectedOut {
		return 0
	}
	diff := new(big.Int).SetUint64(expectedOut - actualOut)
	num := diff.Mul(diff, new(big.Int).SetUint64(DTLMaxTaxBPS))
	den := new(big.Int).SetUint64(expectedOut)
	if den.Sign() == 0 {
		return 0
	}
	out := new(big.Int).Div(num, den)
	if out.Sign() <= 0 {
		return 0
	}
	if out.Cmp(new(big.Int).SetUint64(DTLMaxTaxBPS)) > 0 {
		return DTLMaxTaxBPS
	}
	return uint16(out.Uint64())
}

func dtlAppendStructuredEventLog(state *DTLState, topics []string, payload any) {
	if state == nil {
		return
	}
	state.ensure()
	cleanTopics := make([]string, 0, len(topics))
	for _, topic := range topics {
		t := strings.TrimSpace(topic)
		if t == "" {
			continue
		}
		cleanTopics = append(cleanTopics, t)
	}
	data := ""
	if payload != nil {
		if encoded, err := json.Marshal(payload); err == nil {
			data = string(encoded)
		}
	}
	state.EventLogs = append(state.EventLogs, DTLEventLog{
		Topics: cleanTopics,
		Data:   data,
	})
}

func dtlQuotePoolSwapRoute(state *DTLState, tokenIn string, amountIn uint64, path []string) (DTLRouteQuote, error) {
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

	shadow := cloneDTLState(state)
	if shadow == nil {
		return quote, ErrDTLInvalidState
	}
	shadow.ensure()

	normalizedTokenIn := normalizeDTLTokenID(tokenIn)
	if normalizedTokenIn == "" {
		return quote, fmt.Errorf("dtl: token_in is required")
	}
	quote.TokenIn = normalizedTokenIn
	quote.Path = make([]string, 0, len(path))
	quote.Hops = make([]DTLRouteHopQuote, 0, len(path))
	quote.AmountIn = amountIn

	expectedNum := new(big.Int).SetUint64(amountIn)
	expectedDen := big.NewInt(1)
	currentToken := normalizedTokenIn
	currentIn := amountIn

	for i, rawPoolID := range path {
		poolID := normalizeDTLPoolID(rawPoolID)
		if poolID == "" {
			return quote, fmt.Errorf("dtl: route path has empty pool id at hop %d", i+1)
		}
		pool := shadow.Pools[poolID]
		if pool == nil {
			return quote, fmt.Errorf("dtl: unknown pool in route path: %s", poolID)
		}

		reserveIn := pool.ReserveA
		reserveOut := pool.ReserveB
		tokenOut := pool.TokenB
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

		amountOut, err := dtlPoolSwapOutAmount(reserveIn, reserveOut, currentIn, pool.FeeBPS)
		if err != nil {
			return quote, err
		}
		if amountOut == 0 {
			return quote, fmt.Errorf("dtl: swap output is zero")
		}

		protocolFeeCut, err := dtlPoolProtocolFeeCut(currentIn, pool.FeeBPS, pool.ProtocolFeeBPS)
		if err != nil {
			return quote, err
		}
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
		expectedOut := new(big.Int).Quo(expectedNum, expectedDen)
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

func ApplyDTLPoolSwapRouteTx(state *DTLState, tx DTLPoolSwapRouteTx, currentHeight uint64) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLPoolSwapRouteTx(state, tx, currentHeight); err != nil {
		return 0, err
	}

	preview, err := dtlQuotePoolSwapRoute(state, tx.TokenIn, tx.AmountIn, tx.Path)
	if err != nil {
		return 0, err
	}

	shadow := cloneDTLState(state)
	if shadow == nil {
		return 0, ErrDTLInvalidState
	}
	shadow.ensure()

	trader := normalizeDTLAccount(tx.Trader)
	currentToken := normalizeDTLTokenID(tx.TokenIn)
	amountIn := tx.AmountIn
	for _, rawPoolID := range tx.Path {
		poolID := normalizeDTLPoolID(rawPoolID)
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
	if err := ValidateDTLDuelCreateTx(state, tx, currentHeight); err != nil {
		return "", err
	}

	creator := normalizeDTLAccount(tx.Creator)
	tokenID := normalizeDTLTokenID(tx.TokenID)
	commit := normalizeDTLHex(tx.CommitHash)
	joinBlocks := tx.JoinExpiryBlocks
	if joinBlocks == 0 {
		joinBlocks = DTLDefaultDuelJoinBlocks
	}
	revealBlocks := tx.RevealExpiryBlocks
	if revealBlocks == 0 {
		revealBlocks = DTLDefaultDuelRevealBlocks
	}
	joinDeadline := currentHeight + joinBlocks
	revealDeadline := joinDeadline + revealBlocks
	beaconDelay := dtlBeaconDelayAtHeight(currentHeight)
	beaconHeight := revealDeadline
	if beaconDelay > 0 && beaconHeight <= ^uint64(0)-beaconDelay {
		beaconHeight += beaconDelay
	}
	duelID := normalizeDTLTokenID(DTLDuelIDFromCreate(chainID, nonce, tx))
	if _, exists := state.Duels[duelID]; exists {
		return "", fmt.Errorf("dtl: duel id collision")
	}

	vault := dtlDuelVaultAccount(duelID)
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

func ApplyDTLDuelJoinTx(state *DTLState, currentHeight uint64, tx DTLDuelJoinTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLDuelJoinTx(state, tx, currentHeight); err != nil {
		return err
	}

	duelID := normalizeDTLTokenID(tx.DuelID)
	duel := state.Duels[duelID]
	joiner := normalizeDTLAccount(tx.Joiner)
	commit := normalizeDTLHex(tx.CommitHash)
	vault := dtlDuelVaultAccount(duelID)
	if err := dtlMoveBalance(state, duel.TokenID, joiner, vault, duel.Stake); err != nil {
		return err
	}
	duel.PlayerB = joiner
	duel.CommitB = commit
	state.Events = append(state.Events, fmt.Sprintf("DUEL_JOIN:%s:%s", duelID, joiner))
	return nil
}

func ApplyDTLDuelRevealTx(state *DTLState, currentHeight uint64, tx DTLDuelRevealTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLDuelRevealTx(state, tx, currentHeight); err != nil {
		return err
	}

	duelID := normalizeDTLTokenID(tx.DuelID)
	duel := state.Duels[duelID]
	player := normalizeDTLAccount(tx.Player)
	secret := strings.TrimSpace(tx.Secret)
	if player == normalizeDTLAccount(duel.PlayerA) {
		duel.RevealA = secret
	} else {
		duel.RevealB = secret
	}
	state.Events = append(state.Events, fmt.Sprintf("DUEL_REVEAL:%s:%s", duelID, player))
	return nil
}

func ApplyDTLDuelFinalizeTx(state *DTLState, currentHeight uint64, tx DTLDuelFinalizeTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLDuelFinalizeTx(state, tx, currentHeight); err != nil {
		return err
	}

	duelID := normalizeDTLTokenID(tx.DuelID)
	duel := state.Duels[duelID]
	vault := dtlDuelVaultAccount(duelID)
	if duel.PlayerB == "" {
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerA, duel.Stake); err != nil {
			return err
		}
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_CANCEL:%s", duelID))
		return nil
	}
	if duel.BeaconHeight == 0 {
		duel.BeaconHeight = duel.RevealDeadline + dtlBeaconDelayAtHeight(currentHeight)
	}
	if currentHeight < duel.BeaconHeight {
		return fmt.Errorf("dtl: duel waiting for beacon height")
	}
	beaconHash := ""
	if duel.BeaconHeight > duel.RevealDeadline {
		beaconHash = dtlDeterministicBeaconHash(duelID, duel.BeaconHeight)
	}
	duel.BeaconHash = beaconHash
	stakeX2, err := dtlSafeAddU64(duel.Stake, duel.Stake)
	if err != nil {
		return err
	}

	switch {
	case duel.RevealA != "" && duel.RevealB != "":
		seedParts := []string{duelID, duel.RevealA, duel.RevealB}
		if beaconHash != "" {
			seedParts = append(seedParts, beaconHash)
		}
		seed := strings.Join(seedParts, "|")
		duel.FinalizationSeed = seed
		winnerScope := duelID
		if beaconHash != "" {
			winnerScope = duelID + "|" + beaconHash
		}
		winner := dtlDuelWinner(winnerScope, duel.PlayerA, duel.PlayerB, duel.RevealA, duel.RevealB)
		if winner == "" {
			return fmt.Errorf("dtl: duel winner resolution failed")
		}
		if err := dtlMoveBalance(state, duel.TokenID, vault, winner, stakeX2); err != nil {
			return err
		}
		duel.Winner = winner
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_FINALIZE:%s:%s", duelID, winner))
		if err := dtlAddSeasonScore(state, currentHeight, winner, ConfigDTLGameFiDuelWinPoints, "duel_finalize"); err != nil {
			return err
		}
		return nil
	case currentHeight >= duel.RevealDeadline && duel.RevealA != "" && duel.RevealB == "":
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerA, stakeX2); err != nil {
			return err
		}
		duel.Winner = duel.PlayerA
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_FORFEIT:%s:%s", duelID, duel.PlayerA))
		if err := dtlAddSeasonScore(state, currentHeight, duel.PlayerA, ConfigDTLGameFiDuelWinPoints, "duel_forfeit"); err != nil {
			return err
		}
		return nil
	case currentHeight >= duel.RevealDeadline && duel.RevealB != "" && duel.RevealA == "":
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerB, stakeX2); err != nil {
			return err
		}
		duel.Winner = duel.PlayerB
		duel.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("DUEL_FORFEIT:%s:%s", duelID, duel.PlayerB))
		if err := dtlAddSeasonScore(state, currentHeight, duel.PlayerB, ConfigDTLGameFiDuelWinPoints, "duel_forfeit"); err != nil {
			return err
		}
		return nil
	case currentHeight >= duel.RevealDeadline:
		if err := dtlMoveBalance(state, duel.TokenID, vault, duel.PlayerA, duel.Stake); err != nil {
			return err
		}
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

func getOrCreateDTLLendingPosition(state *DTLState, marketID, account string) *DTLLendingPositionState {
	key := dtlLendingPositionKey(marketID, account)
	if existing := state.LendingPositions[key]; existing != nil {
		return existing
	}
	p := &DTLLendingPositionState{
		MarketID: normalizeDTLMarketID(marketID),
		Account:  normalizeDTLAccount(account),
	}
	state.LendingPositions[key] = p
	return p
}

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
	if err := ValidateDTLLendMarketCreateTx(state, tx); err != nil {
		return "", err
	}
	collateralFactorBPS, liquidationBonusBPS, err := validateDTLLendingRiskParams(
		tx.CollateralFactorBPS,
		tx.LiquidationBonusBPS,
	)
	if err != nil {
		return "", err
	}

	creator := normalizeDTLAccount(tx.Creator)
	collateralTokenID := normalizeDTLTokenID(tx.CollateralTokenID)
	debtTokenID := normalizeDTLTokenID(tx.DebtTokenID)
	pairKey := dtlLendingPairKey(collateralTokenID, debtTokenID)
	if existing := normalizeDTLMarketID(state.LendingIndex[pairKey]); existing != "" {
		return "", fmt.Errorf("dtl: lending market already exists for pair: %s", existing)
	}
	marketID := normalizeDTLMarketID(DTLLendingMarketIDFromTokens(chainID, collateralTokenID, debtTokenID))
	if _, exists := state.LendingMarkets[marketID]; exists {
		return "", fmt.Errorf("dtl: lending market id collision")
	}
	vault := dtlLendingVaultAccount(marketID)
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

func ApplyDTLLendDepositCollateralTx(state *DTLState, tx DTLLendDepositCollateralTx) error {
	return ApplyDTLLendDepositCollateralTxWithHeight(state, 0, tx)
}

func ApplyDTLLendDepositCollateralTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendDepositCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLLendDepositCollateralTx(state, tx); err != nil {
		return err
	}
	account := normalizeDTLAccount(tx.Account)
	marketID := normalizeDTLMarketID(tx.MarketID)
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	market := state.LendingMarkets[marketID]
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	vault := dtlLendingVaultAccount(marketID)

	if err := dtlMoveBalance(state, market.CollateralTokenID, account, vault, tx.Amount); err != nil {
		return err
	}
	newCollateral, err := dtlSafeAddU64(position.Collateral, tx.Amount)
	if err != nil {
		return err
	}
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

func ApplyDTLLendBorrowTx(state *DTLState, tx DTLLendBorrowTx) error {
	return ApplyDTLLendBorrowTxWithHeight(state, 0, tx)
}

func ApplyDTLLendBorrowTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendBorrowTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLLendBorrowTx(state, tx); err != nil {
		return err
	}
	account := normalizeDTLAccount(tx.Account)
	marketID := normalizeDTLMarketID(tx.MarketID)
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	market := state.LendingMarkets[marketID]
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	newDebt, err := dtlSafeAddU64(position.Debt, tx.Amount)
	if err != nil {
		return err
	}
	healthy, _, err := dtlLendingHealthFactorBPS(state, market, position.Collateral, newDebt, currentHeight)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: borrow would exceed collateral limit")
	}
	newTotalDebt, err := dtlSafeAddU64(market.TotalDebt, tx.Amount)
	if err != nil {
		return err
	}

	vault := dtlLendingVaultAccount(marketID)
	if err := dtlMoveBalance(state, market.DebtTokenID, vault, account, tx.Amount); err != nil {
		return err
	}
	position.Debt = newDebt
	position.ScaledDebt = newDebt
	market.TotalDebt = newTotalDebt
	state.Events = append(state.Events, fmt.Sprintf("LEND_BORROW:%s:%s:%d", marketID, account, tx.Amount))
	return nil
}

func ApplyDTLLendRepayTx(state *DTLState, tx DTLLendRepayTx) error {
	return ApplyDTLLendRepayTxWithHeight(state, 0, tx)
}

func ApplyDTLLendRepayTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendRepayTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLLendRepayTx(state, tx); err != nil {
		return err
	}
	account := normalizeDTLAccount(tx.Account)
	marketID := normalizeDTLMarketID(tx.MarketID)
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	market := state.LendingMarkets[marketID]
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	vault := dtlLendingVaultAccount(marketID)

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

func ApplyDTLLendWithdrawCollateralTx(state *DTLState, tx DTLLendWithdrawCollateralTx) error {
	return ApplyDTLLendWithdrawCollateralTxWithHeight(state, 0, tx)
}

func ApplyDTLLendWithdrawCollateralTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendWithdrawCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLLendWithdrawCollateralTx(state, tx); err != nil {
		return err
	}
	account := normalizeDTLAccount(tx.Account)
	marketID := normalizeDTLMarketID(tx.MarketID)
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	market := state.LendingMarkets[marketID]
	position := getOrCreateDTLLendingPosition(state, marketID, account)
	remainingCollateral := position.Collateral - tx.Amount
	healthy, _, err := dtlLendingHealthFactorBPS(state, market, remainingCollateral, position.Debt, currentHeight)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: withdraw would make position unhealthy")
	}

	vault := dtlLendingVaultAccount(marketID)
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

func ApplyDTLLendLiquidateTx(state *DTLState, tx DTLLendLiquidateTx) error {
	return ApplyDTLLendLiquidateTxWithHeight(state, 0, tx)
}

func ApplyDTLLendLiquidateTxWithHeight(state *DTLState, currentHeight uint64, tx DTLLendLiquidateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLLendLiquidateTx(state, tx, currentHeight); err != nil {
		return err
	}
	liquidator := normalizeDTLAccount(tx.Liquidator)
	borrower := normalizeDTLAccount(tx.Borrower)
	marketID := normalizeDTLMarketID(tx.MarketID)
	if err := dtlLendingAccrueMarket(state, marketID, currentHeight); err != nil {
		return err
	}
	market := state.LendingMarkets[marketID]
	position := getOrCreateDTLLendingPosition(state, marketID, borrower)

	seize, err := dtlLendingSeizeCollateral(tx.RepayAmount, market.LiquidationBonusBPS)
	if err != nil {
		return err
	}
	if seize > position.Collateral {
		seize = position.Collateral
	}
	vault := dtlLendingVaultAccount(marketID)
	if err := dtlMoveBalance(state, market.DebtTokenID, liquidator, vault, tx.RepayAmount); err != nil {
		return err
	}
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
	if err := ValidateDTLTournamentCreateTx(state, tx, currentHeight); err != nil {
		return "", err
	}
	creator := normalizeDTLAccount(tx.Creator)
	tokenID := normalizeDTLTokenID(tx.TokenID)
	joinBlocks := tx.JoinExpiryBlocks
	if joinBlocks == 0 {
		joinBlocks = DTLDefaultTournamentJoinBlocks
	}
	revealBlocks := tx.RevealExpiryBlocks
	if revealBlocks == 0 {
		revealBlocks = DTLDefaultTournamentRevealBlocks
	}
	joinDeadline := currentHeight + joinBlocks
	revealDeadline := joinDeadline + revealBlocks
	beaconDelay := dtlBeaconDelayAtHeight(currentHeight)
	beaconHeight := revealDeadline
	if beaconDelay > 0 && beaconHeight <= ^uint64(0)-beaconDelay {
		beaconHeight += beaconDelay
	}
	tournamentID := normalizeDTLTournamentID(DTLTournamentIDFromCreate(chainID, nonce, tx))
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

func ApplyDTLTournamentJoinTx(state *DTLState, currentHeight uint64, tx DTLTournamentJoinTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLTournamentJoinTx(state, tx, currentHeight); err != nil {
		return err
	}
	player := normalizeDTLAccount(tx.Player)
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	tournament := state.Tournaments[tournamentID]
	commit := normalizeDTLHex(tx.CommitHash)
	vault := dtlTournamentVaultAccount(tournamentID)
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
	newPot, err := dtlSafeAddU64(tournament.Pot, tournament.EntryFee)
	if err != nil {
		return err
	}
	tournament.Pot = newPot
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_JOIN:%s:%s", tournamentID, player))
	return nil
}

func ApplyDTLTournamentRevealTx(state *DTLState, currentHeight uint64, tx DTLTournamentRevealTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLTournamentRevealTx(state, tx, currentHeight); err != nil {
		return err
	}
	player := normalizeDTLAccount(tx.Player)
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	tournament := state.Tournaments[tournamentID]
	if tournament.Reveals == nil {
		tournament.Reveals = make(map[string]string)
	}
	tournament.Reveals[player] = strings.TrimSpace(tx.Secret)
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_REVEAL:%s:%s", tournamentID, player))
	if err := dtlAddSeasonScore(state, currentHeight, player, ConfigDTLGameFiTournamentPartPoints, "tournament_reveal"); err != nil {
		return err
	}
	return nil
}

func ApplyDTLTournamentFinalizeTx(state *DTLState, currentHeight uint64, tx DTLTournamentFinalizeTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLTournamentFinalizeTx(state, tx, currentHeight); err != nil {
		return err
	}
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	tournament := state.Tournaments[tournamentID]
	vault := dtlTournamentVaultAccount(tournamentID)
	if len(tournament.Players) == 0 {
		tournament.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_CANCEL:%s", tournamentID))
		return nil
	}
	if tournament.BeaconHeight == 0 {
		tournament.BeaconHeight = tournament.RevealDeadline + dtlBeaconDelayAtHeight(currentHeight)
	}
	if currentHeight < tournament.BeaconHeight {
		return fmt.Errorf("dtl: tournament waiting for beacon height")
	}
	beaconHash := ""
	if tournament.BeaconHeight > tournament.RevealDeadline {
		beaconHash = dtlDeterministicBeaconHash(tournamentID, tournament.BeaconHeight)
	}
	tournament.BeaconHash = beaconHash

	candidates := make([]string, 0, len(tournament.Players))
	for _, player := range tournament.Players {
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
		for _, player := range tournament.Players {
			n := normalizeDTLAccount(player)
			if n == "" {
				continue
			}
			if err := dtlMoveBalance(state, tournament.TokenID, vault, n, tournament.EntryFee); err != nil {
				return err
			}
		}
		tournament.Pot = 0
		tournament.Settled = true
		state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_REFUND:%s", tournamentID))
		return nil
	}

	winnerScope := tournamentID
	if beaconHash != "" {
		tournament.FinalizationSeed = strings.Join([]string{tournamentID, beaconHash}, "|")
		winnerScope = tournamentID + "|" + beaconHash
	} else {
		tournament.FinalizationSeed = tournamentID
	}
	winner := dtlTournamentWinner(winnerScope, candidates, tournament.Reveals)
	if winner == "" {
		return fmt.Errorf("dtl: tournament winner resolution failed")
	}
	if err := dtlMoveBalance(state, tournament.TokenID, vault, winner, tournament.Pot); err != nil {
		return err
	}
	tournament.Winner = winner
	tournament.Pot = 0
	tournament.Settled = true
	state.Events = append(state.Events, fmt.Sprintf("TOURNAMENT_FINALIZE:%s:%s", tournamentID, winner))
	if err := dtlAddSeasonScore(state, currentHeight, winner, ConfigDTLGameFiTournamentWinPoints, "tournament_finalize"); err != nil {
		return err
	}
	return nil
}

func ApplyDTLOracleFeedCreateTx(state *DTLState, chainID string, nonce uint64, tx DTLOracleFeedCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLOracleFeedCreateTx(state, tx); err != nil {
		return "", err
	}
	feedID := normalizeDTLTokenID(dtlOracleFeedIDFromCreate(chainID, nonce, tx))
	if _, exists := state.OracleFeeds[feedID]; exists {
		return "", fmt.Errorf("dtl: oracle feed id collision")
	}
	signers := make([]string, 0, len(tx.Signers))
	seen := make(map[string]struct{}, len(tx.Signers))
	for _, signer := range tx.Signers {
		n := normalizeDTLAccount(signer)
		if n == "" {
			continue
		}
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

func ApplyDTLOraclePriceSubmitTx(state *DTLState, currentHeight uint64, tx DTLOraclePriceSubmitTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLOraclePriceSubmitTx(state, tx, currentHeight); err != nil {
		return err
	}
	feedID := normalizeDTLTokenID(tx.FeedID)
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		return fmt.Errorf("dtl: unknown oracle feed")
	}
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
	prices := make([]uint64, 0, len(feed.Signers))
	for _, signer := range feed.Signers {
		s := state.OracleSamples[feedID][normalizeDTLAccount(signer)]
		if s.Price == 0 {
			continue
		}
		if ConfigDTLOracleMaxStalenessBlocks > 0 && currentHeight >= s.Height &&
			currentHeight-s.Height > ConfigDTLOracleMaxStalenessBlocks {
			continue
		}
		prices = append(prices, s.Price)
	}
	if len(prices) >= int(feed.Threshold) {
		sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
		median := prices[len(prices)/2]
		if len(prices)%2 == 0 {
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

func ApplyDTLContractDeployTx(state *DTLState, chainID string, nonce uint64, tx DTLContractDeployTx) (string, error) {
	if dtlContractRuntimeRemoved() {
		return "", dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	}
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	if generated, err := buildDTLStandardTemplate(tx); err != nil {
		return "", err
	} else {
		tx = generated
	}
	if err := ValidateDTLContractDeployTx(state, chainID, nonce, tx); err != nil {
		return "", err
	}

	contractID := normalizeDTLContractID(DTLContractIDFromDeploy(chainID, nonce, tx))
	methods := make(map[string]*DTLContractMethodState, len(tx.Methods))
	if tx.LogicPack == nil {
		for _, method := range tx.Methods {
			name := normalizeDTLContractMethodName(method.Name)
			op, _ := validateDTLContractOp(method.Op)
			fromMode := strings.ToLower(strings.TrimSpace(method.From))
			if fromMode == "" {
				fromMode = "caller"
			}
			tokenID := normalizeDTLTokenID(method.TokenID)
			if op == DTLContractOpTokenTransfer {
				if resolved, ok := resolveDTLTokenRef(state, method.TokenID); ok {
					tokenID = resolved
				}
			}
			methods[name] = &DTLContractMethodState{
				Name:    name,
				Op:      op,
				Key:     strings.TrimSpace(method.Key),
				Arg:     strings.TrimSpace(method.Arg),
				ToArg:   strings.TrimSpace(method.ToArg),
				TokenID: tokenID,
				From:    fromMode,
			}
		}
	}
	storage := make(map[string]string, len(tx.Init))
	for key, value := range tx.Init {
		storage[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	var logicPack *DTLLogicPack
	var logicHash string
	bytecode := ""
	bytecodeFormat := ""
	bytecodeHash := ""
	bytecodeVersion := uint16(0)
	if tx.LogicPack != nil {
		normalizedPack, err := validateAndNormalizeDTLLogicPack(state, tx.LogicPack)
		if err != nil {
			return "", err
		}
		logicPack = cloneDTLLogicPack(normalizedPack)
		for _, field := range logicPack.Storage {
			if _, exists := storage[field.Key]; !exists {
				storage[field.Key] = strings.TrimSpace(field.Init)
			}
		}
		hash, err := dtlLogicPackHash(logicPack)
		if err != nil {
			return "", err
		}
		logicHash = strings.ToLower(strings.TrimSpace(hash))
	}
	if strings.TrimSpace(tx.Bytecode) != "" {
		normalizedProgram, _, hash, err := decodeNormalizeValidateDTLBytecode(state, tx.Bytecode)
		if err != nil {
			return "", err
		}
		canonicalHex, err := EncodeDTLBytecode(normalizedProgram)
		if err != nil {
			return "", err
		}
		bytecode = canonicalHex
		bytecodeFormat = DTLBytecodeFormatV1
		bytecodeHash = strings.ToLower(strings.TrimSpace(hash))
		bytecodeVersion = normalizedProgram.Version
		for _, field := range normalizedProgram.Storage {
			if _, exists := storage[field.Key]; !exists {
				storage[field.Key] = strings.TrimSpace(field.Init)
			}
		}
		if len(tx.ABI) == 0 && len(normalizedProgram.ABI) > 0 {
			if abiRaw, err := json.Marshal(normalizedProgram.ABI); err == nil {
				tx.ABI = abiRaw
			}
		}
	}

	state.Contracts[contractID] = &DTLContractState{
		ContractID:      contractID,
		Creator:         normalizeDTLAccount(tx.Creator),
		Name:            strings.TrimSpace(tx.Name),
		Lang:            normalizeDTLContractLang(tx.Lang),
		Version:         tx.Version,
		Methods:         methods,
		Storage:         storage,
		LogicPack:       logicPack,
		LogicHash:       logicHash,
		Paused:          false,
		Standard:        normalizeDTLContractStandard(tx.Standard),
		ABI:             append(json.RawMessage(nil), tx.ABI...),
		MetadataURI:     strings.TrimSpace(tx.MetadataURI),
		Interfaces:      append([]string(nil), tx.Interfaces...),
		Upgradeable:     tx.Upgradeable,
		ProxyTarget:     normalizeDTLContractID(tx.ProxyTarget),
		Bytecode:        bytecode,
		BytecodeFormat:  bytecodeFormat,
		BytecodeHash:    bytecodeHash,
		Compiler:        strings.TrimSpace(tx.Compiler),
		SourceHash:      strings.TrimSpace(tx.SourceHash),
		BytecodeVersion: bytecodeVersion,
	}
	// Standard MSC20 templates start with total supply owned by the creator.
	if normalizeDTLContractStandard(tx.Standard) == DTLContractStandardMSC20 {
		totalSupply, err := parseDTLContractStoredU64(state.Contracts[contractID].Storage, "total_supply")
		if err == nil && totalSupply > 0 {
			creator := normalizeDTLAccount(tx.Creator)
			if creator != "" {
				state.Contracts[contractID].Storage[dtlLogicMapStorageKey("balances", creator)] = strconv.FormatUint(totalSupply, 10)
			}
		}
	}
	state.Events = append(state.Events, fmt.Sprintf("CONTRACT_DEPLOY:%s:%s", contractID, strings.TrimSpace(tx.Name)))
	return contractID, nil
}

func ApplyDTLContractCallTx(state *DTLState, tx DTLContractCallTx) error {
	return ApplyDTLContractCallTxWithContext(state, tx, 0, ChainID)
}

func ApplyDTLContractCallTxWithContext(state *DTLState, tx DTLContractCallTx, blockHeight uint64, chainID string) error {
	if dtlContractRuntimeRemoved() {
		return dtlContractRuntimeRemovedError("CONTRACT_CALL")
	}
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLContractCallTx(state, tx); err != nil {
		return err
	}

	caller := normalizeDTLAccount(tx.Caller)
	contractID := normalizeDTLContractID(tx.ContractID)
	contract := state.Contracts[contractID]
	methodName := normalizeDTLContractMethodName(tx.Method)
	if contract != nil && strings.TrimSpace(contract.Bytecode) != "" && contract.LogicPack != nil {
		warn := fmt.Sprintf("CONTRACT_RUNTIME_WARNING:%s:mixed_runtime_source:preferring_%s", contractID, DTLContractRuntimeModeBytecode)
		state.Events = append(state.Events, warn)
		fmt.Printf("[DTL-HYBRID] warning contract=%s mixed_runtime=bytecode+logic_pack action=prefer_bytecode\n", contractID)
	}
	if strings.TrimSpace(contract.Bytecode) != "" {
		ctx := newDTLLogicExecContext(blockHeight, chainID)
		if _, err := executeDTLBytecodeCallWithContext(state, contractID, contract, tx, ctx, false); err != nil {
			return err
		}
		state.Events = append(state.Events, fmt.Sprintf("CONTRACT_CALL:%s:%s:%s", contractID, methodName, caller))
		return nil
	}
	if contract.LogicPack != nil {
		ctx := newDTLLogicExecContext(blockHeight, chainID)
		if _, err := executeDTLLogicPackCallWithContext(state, contractID, contract, tx, ctx, false); err != nil {
			return err
		}
		state.Events = append(state.Events, fmt.Sprintf("CONTRACT_CALL:%s:%s:%s", contractID, methodName, caller))
		return nil
	}
	method := contract.Methods[methodName]

	args := tx.Args
	if args == nil {
		args = map[string]string{}
	}
	switch method.Op {
	case DTLContractOpSetStr:
		contract.Storage[method.Key] = strings.TrimSpace(args[method.Arg])
	case DTLContractOpSetU64:
		n, _ := parseDTLContractArgU64(args, method.Arg)
		contract.Storage[method.Key] = strconv.FormatUint(n, 10)
	case DTLContractOpAddU64:
		cur, _ := parseDTLContractStoredU64(contract.Storage, method.Key)
		add, _ := parseDTLContractArgU64(args, method.Arg)
		next, err := dtlSafeAddU64(cur, add)
		if err != nil {
			return err
		}
		contract.Storage[method.Key] = strconv.FormatUint(next, 10)
	case DTLContractOpSubU64:
		cur, _ := parseDTLContractStoredU64(contract.Storage, method.Key)
		sub, _ := parseDTLContractArgU64(args, method.Arg)
		if sub > cur {
			return fmt.Errorf("dtl: contract subtraction underflow")
		}
		contract.Storage[method.Key] = strconv.FormatUint(cur-sub, 10)
	case DTLContractOpTokenTransfer:
		amount, _ := parseDTLContractArgU64(args, method.Arg)
		to := normalizeDTLAccount(args[method.ToArg])
		fromMode := strings.ToLower(strings.TrimSpace(method.From))
		if fromMode == "" {
			fromMode = "caller"
		}
		from := caller
		if fromMode == "contract" {
			from = dtlContractVaultAccount(contractID)
		}
		tokenID := method.TokenID
		if resolved, ok := resolveDTLTokenRef(state, method.TokenID); ok {
			tokenID = resolved
		}
		if err := dtlMoveBalance(state, tokenID, from, to, amount); err != nil {
			return err
		}
	default:
		return fmt.Errorf("dtl: unsupported contract op")
	}

	state.Events = append(state.Events, fmt.Sprintf("CONTRACT_CALL:%s:%s:%s", contractID, methodName, caller))
	return nil
}

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
	if err := ValidateDTLMintTx(state, tx); err != nil {
		return err
	}
	if replayWindow == 0 {
		replayWindow = DTLDefaultReplayWindow
	}

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token := state.Tokens[tokenID]

	payloadHash, err := DTLPayloadHash(struct {
		TokenID string `json:"token_id"`
		To      string `json:"to"`
		Amount  uint64 `json:"amount"`
	}{
		TokenID: tokenID,
		To:      normalizeDTLAccount(tx.To),
		Amount:  tx.Amount,
	})
	if err != nil {
		return err
	}

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
	if err := dtlMarkReplay(state, cert); err != nil {
		return err
	}

	to := normalizeDTLAccount(tx.To)
	toKey := dtlBalanceKey(tokenID, to)
	if err := dtlAddBalance(state.Balances, toKey, tx.Amount); err != nil {
		return err
	}
	token.TotalSupply += tx.Amount
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_MINT:%s:%s:%d", tokenID, to, tx.Amount))
	return nil
}

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
	token := state.Tokens[tokenID]
	if token == nil {
		return ErrDTLUnknownToken
	}

	if action == DTLGovRotateAuthority {
		p, ok := payload.(DTLRotateAuthorityPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid rotate authority payload")
		}
		if err := ValidateDTLRotateAuthorityPayload(p); err != nil {
			return err
		}
	}

	payloadHash, err := DTLPayloadHash(payload)
	if err != nil {
		return err
	}
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
	if err := dtlMarkReplay(state, cert); err != nil {
		return err
	}

	switch action {
	case DTLGovPause:
		token.Paused = true
	case DTLGovUnpause:
		token.Paused = false
	case DTLGovFreezeAccount:
		p, ok := payload.(DTLFreezeAccountPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid freeze payload")
		}
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
		p, ok := payload.(DTLFreezeAccountPayload)
		if !ok {
			return fmt.Errorf("dtl: invalid unfreeze payload")
		}
		account := normalizeDTLAccount(p.Account)
		if account == "" {
			return fmt.Errorf("dtl: invalid unfreeze account")
		}
		if state.FrozenAccounts[tokenID] == nil {
			state.FrozenAccounts[tokenID] = make(map[string]bool)
		}
		delete(state.FrozenAccounts[tokenID], account)
	case DTLGovRotateAuthority:
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

func dtlAddBalance(m map[string]uint64, key string, amount uint64) error {
	if amount == 0 {
		return nil
	}
	current := m[key]
	if current > ^uint64(0)-amount {
		return errors.New("dtl: balance overflow")
	}
	m[key] = current + amount
	return nil
}

func dtlMoveBalance(state *DTLState, tokenIDRaw, fromRaw, toRaw string, amount uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if amount == 0 {
		return nil
	}
	tokenID := normalizeDTLTokenID(tokenIDRaw)
	from := normalizeDTLAccount(fromRaw)
	to := normalizeDTLAccount(toRaw)
	if from == "" || to == "" {
		return fmt.Errorf("dtl: invalid transfer endpoint")
	}
	if from == to {
		return nil
	}
	fromKey := dtlBalanceKey(tokenID, from)
	if state.Balances[fromKey] < amount {
		return ErrDTLInsufficientFunds
	}
	state.Balances[fromKey] -= amount
	return dtlAddBalance(state.Balances, dtlBalanceKey(tokenID, to), amount)
}

func uniqueDTLSigners(signers []string) []string {
	out := make([]string, 0, len(signers))
	seen := make(map[string]struct{}, len(signers))
	for _, signer := range signers {
		n := normalizeDTLAccount(signer)
		if n == "" {
			continue
		}
		if _, exists := seen[n]; exists {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func dtlReplayKey(cert DTLGovernanceCert) string {
	return normalizeDTLTokenID(cert.TokenID) + "|" + string(cert.Action) + "|" + strings.ToLower(cert.ActionPayloadHash)
}

func dtlMarkReplay(state *DTLState, cert DTLGovernanceCert) error {
	key := dtlReplayKey(cert)
	last, exists := state.GovernanceReplay[key]
	if exists && cert.Epoch <= last {
		return ErrDTLReplay
	}
	state.GovernanceReplay[key] = cert.Epoch
	return nil
}
