package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func ValidateDTLCreateTx(state *DTLState, tx DTLCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid name length")
	}
	symbol := normalizeDTLSymbol(tx.Symbol)
	if symbol == "" || len(symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid symbol length")
	}
	if _, exists := state.SymbolIndex[symbol]; exists {
		return fmt.Errorf("dtl: symbol already exists: %s", symbol)
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
	return nil
}

func ValidateDTLTransferTx(state *DTLState, tx DTLTransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: transfer amount must be > 0")
	}

	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if from == "" || to == "" {
		return fmt.Errorf("dtl: invalid transfer account")
	}
	if from == to {
		return fmt.Errorf("dtl: self transfer not allowed")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled && state.IsFrozen(tokenID, from) {
		return ErrDTLFrozen
	}
	if state.BalanceOf(tokenID, from) < tx.Amount {
		return ErrDTLInsufficientFunds
	}
	return nil
}

func ValidateDTLApproveTx(state *DTLState, tx DTLApproveTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}

	owner := normalizeDTLAccount(tx.Owner)
	spender := normalizeDTLAccount(tx.Spender)
	if owner == "" || spender == "" {
		return fmt.Errorf("dtl: invalid approve account")
	}
	if owner == spender {
		return fmt.Errorf("dtl: approve to self is not allowed")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled && state.IsFrozen(tokenID, owner) {
		return ErrDTLFrozen
	}
	return nil
}

func ValidateDTLTransferFromTx(state *DTLState, tx DTLTransferFromTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: transfer amount must be > 0")
	}

	spender := normalizeDTLAccount(tx.Spender)
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if spender == "" || from == "" || to == "" {
		return fmt.Errorf("dtl: invalid transfer account")
	}
	if from == to {
		return fmt.Errorf("dtl: self transfer not allowed")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled && state.IsFrozen(tokenID, from) {
		return ErrDTLFrozen
	}
	if state.BalanceOf(tokenID, from) < tx.Amount {
		return ErrDTLInsufficientFunds
	}
	if state.AllowanceOf(tokenID, from, spender) < tx.Amount {
		return ErrDTLInsufficientAllowance
	}
	return nil
}

func ValidateDTLBurnTx(state *DTLState, tx DTLBurnTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: burn amount must be > 0")
	}
	from := normalizeDTLAccount(tx.From)
	if from == "" {
		return fmt.Errorf("dtl: invalid burn account")
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if token.FreezeEnabled && state.IsFrozen(tokenID, from) {
		return ErrDTLFrozen
	}
	if state.BalanceOf(tokenID, from) < tx.Amount {
		return ErrDTLInsufficientFunds
	}
	if token.TotalSupply < tx.Amount {
		return fmt.Errorf("dtl: burn exceeds total supply")
	}
	return nil
}

func ValidateDTLMintTx(state *DTLState, tx DTLMintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	tokenID := normalizeDTLTokenID(tx.TokenID)
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: mint amount must be > 0")
	}
	to := normalizeDTLAccount(tx.To)
	if to == "" {
		return fmt.Errorf("dtl: invalid mint receiver")
	}
	if token.TotalSupply > token.MaxSupply || token.MaxSupply-token.TotalSupply < tx.Amount {
		return fmt.Errorf("dtl: mint exceeds max supply")
	}
	return nil
}

func ValidateDTLNFT721CreateTx(state *DTLState, tx DTLNFT721CreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid nft721 creator")
	}
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid nft721 name length")
	}
	symbol := normalizeDTLSymbol(tx.Symbol)
	if symbol == "" || len(symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid nft721 symbol length")
	}
	if _, exists := state.NFT721SymbolIndex[symbol]; exists {
		return fmt.Errorf("dtl: nft721 symbol already exists: %s", symbol)
	}
	if len(strings.TrimSpace(tx.BaseURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft721 base uri too long")
	}
	return nil
}

func ValidateDTLNFT721MintTx(state *DTLState, tx DTLNFT721MintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	collection := state.NFT721Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" || creator != normalizeDTLAccount(collection.Creator) {
		return fmt.Errorf("dtl: nft721 mint creator mismatch")
	}
	to := normalizeDTLAccount(tx.To)
	if to == "" {
		return fmt.Errorf("dtl: invalid nft721 receiver")
	}
	if len(strings.TrimSpace(tx.TokenURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft721 token uri too long")
	}
	return nil
}

func ValidateDTLNFT721TransferTx(state *DTLState, tx DTLNFT721TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	collection := state.NFT721Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if from == "" || to == "" {
		return fmt.Errorf("dtl: invalid nft721 account")
	}
	if from == to {
		return fmt.Errorf("dtl: nft721 self transfer not allowed")
	}
	owner := state.NFT721OwnerOf(collectionID, tx.TokenID)
	if owner == "" {
		return ErrDTLUnknownNFTToken
	}
	if normalizeDTLAccount(owner) != from {
		return ErrDTLNotNFTTokenOwner
	}
	return nil
}

func ValidateDTLNFT1155CreateTx(state *DTLState, tx DTLNFT1155CreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid nft1155 creator")
	}
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid nft1155 name length")
	}
	symbol := normalizeDTLSymbol(tx.Symbol)
	if symbol == "" || len(symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid nft1155 symbol length")
	}
	if _, exists := state.NFT1155SymbolIndex[symbol]; exists {
		return fmt.Errorf("dtl: nft1155 symbol already exists: %s", symbol)
	}
	if len(strings.TrimSpace(tx.BaseURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft1155 base uri too long")
	}
	return nil
}

func ValidateDTLNFT1155MintTx(state *DTLState, tx DTLNFT1155MintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	collection := state.NFT1155Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" || creator != normalizeDTLAccount(collection.Creator) {
		return fmt.Errorf("dtl: nft1155 mint creator mismatch")
	}
	to := normalizeDTLAccount(tx.To)
	if to == "" {
		return fmt.Errorf("dtl: invalid nft1155 receiver")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: nft1155 mint amount must be > 0")
	}
	return nil
}

func ValidateDTLNFT1155TransferTx(state *DTLState, tx DTLNFT1155TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	collection := state.NFT1155Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	from := normalizeDTLAccount(tx.From)
	to := normalizeDTLAccount(tx.To)
	if from == "" || to == "" {
		return fmt.Errorf("dtl: invalid nft1155 account")
	}
	if from == to {
		return fmt.Errorf("dtl: nft1155 self transfer not allowed")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: nft1155 transfer amount must be > 0")
	}
	if state.NFT1155BalanceOf(collectionID, tx.TokenID, from) < tx.Amount {
		return ErrDTLInsufficientFunds
	}
	return nil
}

func validateDTLSpendable(
	state *DTLState,
	tokenIDRaw string,
	accountRaw string,
	amount uint64,
) (*DTLTokenState, string, string, error) {
	if state == nil {
		return nil, "", "", ErrDTLInvalidState
	}
	state.ensure()

	tokenID := normalizeDTLTokenID(tokenIDRaw)
	token := state.Tokens[tokenID]
	if token == nil {
		return nil, "", "", ErrDTLUnknownToken
	}
	if amount == 0 {
		return nil, "", "", fmt.Errorf("dtl: amount must be > 0")
	}

	account := normalizeDTLAccount(accountRaw)
	if account == "" {
		return nil, "", "", fmt.Errorf("dtl: invalid account")
	}
	if token.Paused {
		return nil, "", "", ErrDTLPaused
	}
	if token.FreezeEnabled && state.IsFrozen(tokenID, account) {
		return nil, "", "", ErrDTLFrozen
	}
	if state.BalanceOf(tokenID, account) < amount {
		return nil, "", "", ErrDTLInsufficientFunds
	}
	return token, tokenID, account, nil
}

func validateDTLCommitHash(raw string) (string, error) {
	h := normalizeDTLHex(raw)
	if h == "" {
		return "", fmt.Errorf("dtl: commit hash is required")
	}
	if len(h) != 64 {
		return "", fmt.Errorf("dtl: commit hash must be 32-byte hex")
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", fmt.Errorf("dtl: invalid commit hash")
	}
	return h, nil
}

func ValidateDTLPoolCreateTx(state *DTLState, tx DTLPoolCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid pool creator")
	}
	tokenA := normalizeDTLTokenID(tx.TokenA)
	tokenB := normalizeDTLTokenID(tx.TokenB)
	if tokenA == "" || tokenB == "" {
		return fmt.Errorf("dtl: invalid pool token id")
	}
	if tokenA == tokenB {
		return fmt.Errorf("dtl: pool token pair must be distinct")
	}
	if tx.AmountA == 0 || tx.AmountB == 0 {
		return fmt.Errorf("dtl: pool initial liquidity must be > 0")
	}
	if tx.FeeBPS > DTLMaxPoolFeeBPS {
		return fmt.Errorf("dtl: pool fee exceeds %d bps", DTLMaxPoolFeeBPS)
	}
	pairKey := dtlPoolPairKey(tokenA, tokenB)
	if existing := normalizeDTLPoolID(state.PoolIndex[pairKey]); existing != "" {
		return fmt.Errorf("dtl: pool already exists for pair: %s", existing)
	}
	tokenAState, _, _, err := validateDTLSpendable(state, tokenA, creator, tx.AmountA)
	if err != nil {
		return err
	}
	tokenBState, _, _, err := validateDTLSpendable(state, tokenB, creator, tx.AmountB)
	if err != nil {
		return err
	}
	if tokenAState.TaxBPS > 0 || tokenBState.TaxBPS > 0 {
		return fmt.Errorf("dtl: pool tokens with transfer tax are not supported")
	}
	share, err := dtlInitialPoolShare(tx.AmountA, tx.AmountB)
	if err != nil {
		return err
	}
	if share == 0 {
		return fmt.Errorf("dtl: initial LP share must be > 0")
	}
	return nil
}

func ValidateDTLPoolAddLiquidityTx(state *DTLState, tx DTLPoolAddLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	provider := normalizeDTLAccount(tx.Provider)
	if provider == "" {
		return fmt.Errorf("dtl: invalid liquidity provider")
	}
	if tx.AmountA == 0 || tx.AmountB == 0 {
		return fmt.Errorf("dtl: add liquidity amounts must be > 0")
	}
	tokenAState, _, _, err := validateDTLSpendable(state, pool.TokenA, provider, tx.AmountA)
	if err != nil {
		return err
	}
	tokenBState, _, _, err := validateDTLSpendable(state, pool.TokenB, provider, tx.AmountB)
	if err != nil {
		return err
	}
	if tokenAState.TaxBPS > 0 || tokenBState.TaxBPS > 0 {
		return fmt.Errorf("dtl: pool tokens with transfer tax are not supported")
	}
	if pool.ReserveA == 0 || pool.ReserveB == 0 || pool.TotalLPShares == 0 {
		return fmt.Errorf("dtl: invalid pool reserves")
	}
	if !dtlEqCrossMul(tx.AmountA, pool.ReserveB, tx.AmountB, pool.ReserveA) {
		return fmt.Errorf("dtl: liquidity ratio mismatch")
	}
	share, err := dtlLiquidityShareMint(pool, tx.AmountA, tx.AmountB)
	if err != nil {
		return err
	}
	if share == 0 {
		return fmt.Errorf("dtl: minted LP share is zero")
	}
	if tx.MinLPShares > 0 && share < tx.MinLPShares {
		return fmt.Errorf("dtl: slippage: LP share below minimum")
	}
	return nil
}

func ValidateDTLPoolRemoveLiquidityTx(state *DTLState, tx DTLPoolRemoveLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	provider := normalizeDTLAccount(tx.Provider)
	if provider == "" {
		return fmt.Errorf("dtl: invalid liquidity provider")
	}
	if tx.LPShares == 0 {
		return fmt.Errorf("dtl: lp_shares must be > 0")
	}
	if pool.TotalLPShares == 0 {
		return fmt.Errorf("dtl: invalid pool LP supply")
	}
	if state.LPBalanceOf(poolID, provider) < tx.LPShares {
		return fmt.Errorf("dtl: insufficient LP balance")
	}
	outA, outB, err := dtlLiquidityShareBurn(pool, tx.LPShares)
	if err != nil {
		return err
	}
	if outA == 0 || outB == 0 {
		return fmt.Errorf("dtl: remove amount too small")
	}
	if tx.MinAmountA > 0 && outA < tx.MinAmountA {
		return fmt.Errorf("dtl: slippage: token A below minimum")
	}
	if tx.MinAmountB > 0 && outB < tx.MinAmountB {
		return fmt.Errorf("dtl: slippage: token B below minimum")
	}
	return nil
}

func ValidateDTLPoolSwapTx(state *DTLState, tx DTLPoolSwapTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	poolID := normalizeDTLPoolID(tx.PoolID)
	pool := state.Pools[poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	trader := normalizeDTLAccount(tx.Trader)
	if trader == "" {
		return fmt.Errorf("dtl: invalid trader")
	}
	if tx.AmountIn == 0 {
		return fmt.Errorf("dtl: swap amount_in must be > 0")
	}
	tokenIn := normalizeDTLTokenID(tx.TokenIn)
	if tokenIn != pool.TokenA && tokenIn != pool.TokenB {
		return fmt.Errorf("dtl: token_in not in pool")
	}
	tokenState, _, _, err := validateDTLSpendable(state, tokenIn, trader, tx.AmountIn)
	if err != nil {
		return err
	}
	if tokenState.TaxBPS > 0 {
		return fmt.Errorf("dtl: pool tokens with transfer tax are not supported")
	}
	reserveIn := pool.ReserveA
	reserveOut := pool.ReserveB
	if tokenIn == pool.TokenB {
		reserveIn = pool.ReserveB
		reserveOut = pool.ReserveA
	}
	amountOut, err := dtlPoolSwapOutAmount(reserveIn, reserveOut, tx.AmountIn, pool.FeeBPS)
	if err != nil {
		return err
	}
	if amountOut == 0 {
		return fmt.Errorf("dtl: swap output is zero")
	}
	if tx.MinAmountOut > 0 && amountOut < tx.MinAmountOut {
		return fmt.Errorf("dtl: slippage: output below minimum")
	}
	return nil
}

func ValidateDTLPoolSwapRouteTx(state *DTLState, tx DTLPoolSwapRouteTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if !dtlRouterEnabled() {
		return fmt.Errorf("dtl: router is disabled")
	}

	trader := normalizeDTLAccount(tx.Trader)
	if trader == "" {
		return fmt.Errorf("dtl: invalid trader")
	}

	tokenIn := normalizeDTLTokenID(tx.TokenIn)
	if tokenIn == "" {
		return fmt.Errorf("dtl: token_in is required")
	}
	if tx.AmountIn == 0 {
		return fmt.Errorf("dtl: swap amount_in must be > 0")
	}

	hops := len(tx.Path)
	if hops < 1 {
		return fmt.Errorf("dtl: route path must contain at least 1 pool")
	}
	if hops > dtlRouterMaxHops() {
		return fmt.Errorf("dtl: route path exceeds max hops")
	}

	if tx.DeadlineHeight == 0 {
		return fmt.Errorf("dtl: route deadline_height is required")
	}
	if tx.DeadlineHeight < currentHeight {
		return fmt.Errorf("dtl: route deadline expired")
	}
	maxDeadline := currentHeight + dtlRouterDeadlineMaxBlocks()
	if maxDeadline < currentHeight || tx.DeadlineHeight > maxDeadline {
		return fmt.Errorf("dtl: route deadline too far")
	}

	currentToken := tokenIn
	for i, rawPoolID := range tx.Path {
		poolID := normalizeDTLPoolID(rawPoolID)
		if poolID == "" {
			return fmt.Errorf("dtl: route path has empty pool id at hop %d", i+1)
		}
		pool := state.Pools[poolID]
		if pool == nil {
			return fmt.Errorf("dtl: unknown pool in route path: %s", poolID)
		}
		switch currentToken {
		case pool.TokenA:
			currentToken = pool.TokenB
		case pool.TokenB:
			currentToken = pool.TokenA
		default:
			return fmt.Errorf("dtl: route path disconnected at hop %d", i+1)
		}
	}

	quote, err := dtlQuotePoolSwapRoute(state, tokenIn, tx.AmountIn, tx.Path)
	if err != nil {
		return err
	}

	if tx.MinAmountOut > 0 && quote.AmountOut < tx.MinAmountOut {
		return fmt.Errorf("dtl: slippage: output below minimum")
	}
	if quote.PriceImpactBPS > dtlRouterMaxPriceImpactBPS() {
		return fmt.Errorf("dtl: route price impact exceeds max")
	}

	tradeShadow := cloneDTLState(state)
	if tradeShadow == nil {
		return ErrDTLInvalidState
	}
	amountIn := tx.AmountIn
	routeToken := tokenIn
	for _, rawPoolID := range tx.Path {
		poolID := normalizeDTLPoolID(rawPoolID)
		amountOut, err := ApplyDTLPoolSwapTx(tradeShadow, DTLPoolSwapTx{
			Trader:       trader,
			PoolID:       poolID,
			TokenIn:      routeToken,
			AmountIn:     amountIn,
			MinAmountOut: 0,
		})
		if err != nil {
			return err
		}
		pool := tradeShadow.Pools[poolID]
		if pool == nil {
			return fmt.Errorf("dtl: unknown pool")
		}
		if routeToken == pool.TokenA {
			routeToken = pool.TokenB
		} else {
			routeToken = pool.TokenA
		}
		amountIn = amountOut
	}

	return nil
}

func ValidateDTLDuelCreateTx(state *DTLState, tx DTLDuelCreateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid duel creator")
	}
	if _, err := validateDTLCommitHash(tx.CommitHash); err != nil {
		return err
	}
	if tx.Stake == 0 {
		return fmt.Errorf("dtl: duel stake must be > 0")
	}
	token, _, _, err := validateDTLSpendable(state, tx.TokenID, creator, tx.Stake)
	if err != nil {
		return err
	}
	if token.TaxBPS > 0 {
		return fmt.Errorf("dtl: duel token with transfer tax is not supported")
	}

	joinBlocks := tx.JoinExpiryBlocks
	if joinBlocks == 0 {
		joinBlocks = DTLDefaultDuelJoinBlocks
	}
	revealBlocks := tx.RevealExpiryBlocks
	if revealBlocks == 0 {
		revealBlocks = DTLDefaultDuelRevealBlocks
	}
	if joinBlocks == 0 || revealBlocks == 0 {
		return fmt.Errorf("dtl: duel deadlines must be > 0")
	}
	if currentHeight > ^uint64(0)-joinBlocks || currentHeight+joinBlocks > ^uint64(0)-revealBlocks {
		return fmt.Errorf("dtl: duel deadline overflow")
	}
	return nil
}

func ValidateDTLDuelJoinTx(state *DTLState, tx DTLDuelJoinTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	duelID := normalizeDTLTokenID(tx.DuelID)
	duel := state.Duels[duelID]
	if duel == nil {
		return fmt.Errorf("dtl: unknown duel")
	}
	if duel.Settled {
		return fmt.Errorf("dtl: duel already settled")
	}
	if duel.PlayerB != "" {
		return fmt.Errorf("dtl: duel already joined")
	}
	if currentHeight > duel.JoinDeadline {
		return fmt.Errorf("dtl: duel join deadline passed")
	}

	joiner := normalizeDTLAccount(tx.Joiner)
	if joiner == "" {
		return fmt.Errorf("dtl: invalid duel joiner")
	}
	if joiner == normalizeDTLAccount(duel.PlayerA) {
		return fmt.Errorf("dtl: creator cannot join own duel")
	}
	if _, err := validateDTLCommitHash(tx.CommitHash); err != nil {
		return err
	}
	_, _, _, err := validateDTLSpendable(state, duel.TokenID, joiner, duel.Stake)
	return err
}

func ValidateDTLDuelRevealTx(state *DTLState, tx DTLDuelRevealTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	duelID := normalizeDTLTokenID(tx.DuelID)
	duel := state.Duels[duelID]
	if duel == nil {
		return fmt.Errorf("dtl: unknown duel")
	}
	if duel.Settled {
		return fmt.Errorf("dtl: duel already settled")
	}
	if duel.PlayerB == "" {
		return fmt.Errorf("dtl: duel is not yet matched")
	}
	if currentHeight > duel.RevealDeadline {
		return fmt.Errorf("dtl: duel reveal deadline passed")
	}
	player := normalizeDTLAccount(tx.Player)
	if player == "" {
		return fmt.Errorf("dtl: invalid reveal player")
	}
	secret := strings.TrimSpace(tx.Secret)
	if secret == "" {
		return fmt.Errorf("dtl: reveal secret is required")
	}
	if len(secret) > 256 {
		return fmt.Errorf("dtl: reveal secret too long")
	}

	secretHash := DTLDuelCommitHash(secret)
	switch player {
	case normalizeDTLAccount(duel.PlayerA):
		if duel.RevealA != "" {
			return fmt.Errorf("dtl: player A already revealed")
		}
		if !strings.EqualFold(secretHash, duel.CommitA) {
			return fmt.Errorf("dtl: reveal does not match player A commitment")
		}
	case normalizeDTLAccount(duel.PlayerB):
		if duel.RevealB != "" {
			return fmt.Errorf("dtl: player B already revealed")
		}
		if !strings.EqualFold(secretHash, duel.CommitB) {
			return fmt.Errorf("dtl: reveal does not match player B commitment")
		}
	default:
		return fmt.Errorf("dtl: player is not part of duel")
	}
	return nil
}

func ValidateDTLDuelFinalizeTx(state *DTLState, tx DTLDuelFinalizeTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	if normalizeDTLAccount(tx.Caller) == "" {
		return fmt.Errorf("dtl: invalid finalize caller")
	}
	duelID := normalizeDTLTokenID(tx.DuelID)
	duel := state.Duels[duelID]
	if duel == nil {
		return fmt.Errorf("dtl: unknown duel")
	}
	if duel.Settled {
		return fmt.Errorf("dtl: duel already settled")
	}
	if duel.PlayerB == "" {
		if currentHeight < duel.JoinDeadline {
			return fmt.Errorf("dtl: duel still waiting for join")
		}
		return nil
	}
	if duel.RevealA != "" && duel.RevealB != "" {
		beaconHeight := duel.BeaconHeight
		if beaconHeight == 0 {
			beaconHeight = duel.RevealDeadline + dtlBeaconDelayAtHeight(currentHeight)
		}
		if currentHeight < beaconHeight {
			return fmt.Errorf("dtl: duel waiting for beacon")
		}
		return nil
	}
	if currentHeight < duel.RevealDeadline {
		return fmt.Errorf("dtl: duel still waiting for reveal")
	}
	return nil
}

func validateDTLLendingRiskParams(collateralFactorBPS, liquidationBonusBPS uint16) (uint16, uint16, error) {
	if collateralFactorBPS == 0 {
		collateralFactorBPS = DTLDefaultLendingLTVBPS
	}
	if liquidationBonusBPS == 0 {
		liquidationBonusBPS = DTLDefaultLendingLiqBonusBPS
	}
	if collateralFactorBPS == 0 || collateralFactorBPS > DTLMaxLTVBPS || collateralFactorBPS >= DTLMaxTaxBPS {
		return 0, 0, fmt.Errorf("dtl: invalid collateral factor bps")
	}
	if liquidationBonusBPS > DTLMaxLiqBonusBPS {
		return 0, 0, fmt.Errorf("dtl: invalid liquidation bonus bps")
	}
	return collateralFactorBPS, liquidationBonusBPS, nil
}

func getDTLLendingPosition(state *DTLState, marketID, account string) *DTLLendingPositionState {
	if state == nil {
		return nil
	}
	return state.LendingPositions[dtlLendingPositionKey(marketID, account)]
}

func ValidateDTLLendMarketCreateTx(state *DTLState, tx DTLLendMarketCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid lending market creator")
	}
	collateralTokenID := normalizeDTLTokenID(tx.CollateralTokenID)
	debtTokenID := normalizeDTLTokenID(tx.DebtTokenID)
	if collateralTokenID == "" || debtTokenID == "" {
		return fmt.Errorf("dtl: invalid lending market token")
	}
	if collateralTokenID == debtTokenID {
		return fmt.Errorf("dtl: lending market tokens must be distinct")
	}
	if tx.DebtLiquidity == 0 {
		return fmt.Errorf("dtl: debt liquidity must be > 0")
	}
	if _, _, err := validateDTLLendingRiskParams(tx.CollateralFactorBPS, tx.LiquidationBonusBPS); err != nil {
		return err
	}
	if tx.ReserveFactorBPS > DTLMaxTaxBPS {
		return fmt.Errorf("dtl: invalid reserve factor")
	}
	if tx.CloseFactorBPS > DTLMaxTaxBPS {
		return fmt.Errorf("dtl: invalid close factor")
	}
	pairKey := dtlLendingPairKey(collateralTokenID, debtTokenID)
	if existing := normalizeDTLMarketID(state.LendingIndex[pairKey]); existing != "" {
		return fmt.Errorf("dtl: lending market already exists for pair: %s", existing)
	}
	collateralToken := state.Tokens[collateralTokenID]
	if collateralToken == nil {
		return ErrDTLUnknownToken
	}
	debtToken, _, _, err := validateDTLSpendable(state, debtTokenID, creator, tx.DebtLiquidity)
	if err != nil {
		return err
	}
	if feedID := normalizeDTLTokenID(tx.CollateralFeedID); feedID != "" && state.OracleFeeds[feedID] == nil {
		return fmt.Errorf("dtl: unknown collateral oracle feed")
	}
	if feedID := normalizeDTLTokenID(tx.DebtFeedID); feedID != "" && state.OracleFeeds[feedID] == nil {
		return fmt.Errorf("dtl: unknown debt oracle feed")
	}
	if collateralToken.TaxBPS > 0 || debtToken.TaxBPS > 0 {
		return fmt.Errorf("dtl: lending tokens with transfer tax are not supported")
	}
	return nil
}

func ValidateDTLLendDepositCollateralTx(state *DTLState, tx DTLLendDepositCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: deposit amount must be > 0")
	}
	marketID := normalizeDTLMarketID(tx.MarketID)
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	collateralToken, _, _, err := validateDTLSpendable(state, market.CollateralTokenID, account, tx.Amount)
	if err != nil {
		return err
	}
	if collateralToken.TaxBPS > 0 {
		return fmt.Errorf("dtl: lending tokens with transfer tax are not supported")
	}
	return nil
}

func ValidateDTLLendBorrowTx(state *DTLState, tx DTLLendBorrowTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: borrow amount must be > 0")
	}
	marketID := normalizeDTLMarketID(tx.MarketID)
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	position := getDTLLendingPosition(state, marketID, account)
	if position == nil || position.Collateral == 0 {
		return fmt.Errorf("dtl: no collateral position")
	}
	newDebt, err := dtlSafeAddU64(position.Debt, tx.Amount)
	if err != nil {
		return err
	}
	healthy, err := dtlLendingIsHealthy(position.Collateral, newDebt, market.CollateralFactorBPS)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: borrow would exceed collateral limit")
	}
	vault := dtlLendingVaultAccount(marketID)
	if _, _, _, err := validateDTLSpendable(state, market.DebtTokenID, vault, tx.Amount); err != nil {
		return err
	}
	return nil
}

func ValidateDTLLendRepayTx(state *DTLState, tx DTLLendRepayTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: repay amount must be > 0")
	}
	marketID := normalizeDTLMarketID(tx.MarketID)
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	position := getDTLLendingPosition(state, marketID, account)
	if position == nil || position.Debt == 0 {
		return fmt.Errorf("dtl: no outstanding debt")
	}
	if tx.Amount > position.Debt {
		return fmt.Errorf("dtl: repay exceeds outstanding debt")
	}
	if _, _, _, err := validateDTLSpendable(state, market.DebtTokenID, account, tx.Amount); err != nil {
		return err
	}
	return nil
}

func ValidateDTLLendWithdrawCollateralTx(state *DTLState, tx DTLLendWithdrawCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: withdraw amount must be > 0")
	}
	marketID := normalizeDTLMarketID(tx.MarketID)
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	position := getDTLLendingPosition(state, marketID, account)
	if position == nil || position.Collateral < tx.Amount {
		return fmt.Errorf("dtl: insufficient collateral")
	}
	remainingCollateral := position.Collateral - tx.Amount
	healthy, err := dtlLendingIsHealthy(remainingCollateral, position.Debt, market.CollateralFactorBPS)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: withdraw would make position unhealthy")
	}
	vault := dtlLendingVaultAccount(marketID)
	if _, _, _, err := validateDTLSpendable(state, market.CollateralTokenID, vault, tx.Amount); err != nil {
		return err
	}
	return nil
}

func ValidateDTLLendLiquidateTx(state *DTLState, tx DTLLendLiquidateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	liquidator := normalizeDTLAccount(tx.Liquidator)
	borrower := normalizeDTLAccount(tx.Borrower)
	if liquidator == "" || borrower == "" {
		return fmt.Errorf("dtl: invalid liquidation account")
	}
	if liquidator == borrower {
		return fmt.Errorf("dtl: self liquidation not allowed")
	}
	if tx.RepayAmount == 0 {
		return fmt.Errorf("dtl: liquidation repay amount must be > 0")
	}
	marketID := normalizeDTLMarketID(tx.MarketID)
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	position := getDTLLendingPosition(state, marketID, borrower)
	if position == nil || position.Debt == 0 {
		return fmt.Errorf("dtl: borrower has no debt")
	}
	if tx.RepayAmount > position.Debt {
		return fmt.Errorf("dtl: repay exceeds borrower debt")
	}
	healthy, _, err := dtlLendingHealthFactorBPS(state, market, position.Collateral, position.Debt, currentHeight)
	if err != nil {
		return err
	}
	if healthy {
		return fmt.Errorf("dtl: position is healthy")
	}
	if _, _, _, err := validateDTLSpendable(state, market.DebtTokenID, liquidator, tx.RepayAmount); err != nil {
		return err
	}
	seize, err := dtlLendingSeizeCollateral(tx.RepayAmount, market.LiquidationBonusBPS)
	if err != nil {
		return err
	}
	if market.CloseFactorBPS > 0 {
		maxRepay, err := dtlMulDivU64(position.Debt, uint64(market.CloseFactorBPS), DTLMaxTaxBPS)
		if err != nil {
			return err
		}
		if maxRepay > 0 && tx.RepayAmount > maxRepay {
			return fmt.Errorf("dtl: repay exceeds close factor")
		}
	}
	if seize == 0 {
		return fmt.Errorf("dtl: liquidation seize amount is zero")
	}
	if seize > position.Collateral {
		seize = position.Collateral
	}
	vault := dtlLendingVaultAccount(marketID)
	if _, _, _, err := validateDTLSpendable(state, market.CollateralTokenID, vault, seize); err != nil {
		return err
	}
	return nil
}

func ValidateDTLTournamentCreateTx(state *DTLState, tx DTLTournamentCreateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid tournament creator")
	}
	tokenID := normalizeDTLTokenID(tx.TokenID)
	token := state.Tokens[tokenID]
	if token == nil {
		return ErrDTLUnknownToken
	}
	if token.TaxBPS > 0 {
		return fmt.Errorf("dtl: tournament token with transfer tax is not supported")
	}
	if tx.EntryFee == 0 {
		return fmt.Errorf("dtl: tournament entry fee must be > 0")
	}
	if tx.MaxPlayers < 2 || tx.MaxPlayers > DTLMaxTournamentPlayers {
		return fmt.Errorf("dtl: invalid tournament max_players")
	}
	joinBlocks := tx.JoinExpiryBlocks
	if joinBlocks == 0 {
		joinBlocks = DTLDefaultTournamentJoinBlocks
	}
	revealBlocks := tx.RevealExpiryBlocks
	if revealBlocks == 0 {
		revealBlocks = DTLDefaultTournamentRevealBlocks
	}
	if joinBlocks == 0 || revealBlocks == 0 {
		return fmt.Errorf("dtl: tournament deadlines must be > 0")
	}
	if currentHeight > ^uint64(0)-joinBlocks || currentHeight+joinBlocks > ^uint64(0)-revealBlocks {
		return fmt.Errorf("dtl: tournament deadline overflow")
	}
	return nil
}

func ValidateDTLTournamentJoinTx(state *DTLState, tx DTLTournamentJoinTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	player := normalizeDTLAccount(tx.Player)
	if player == "" {
		return fmt.Errorf("dtl: invalid tournament player")
	}
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	tournament := state.Tournaments[tournamentID]
	if tournament == nil {
		return fmt.Errorf("dtl: unknown tournament")
	}
	if tournament.Settled {
		return fmt.Errorf("dtl: tournament already settled")
	}
	if currentHeight > tournament.JoinDeadline {
		return fmt.Errorf("dtl: tournament join deadline passed")
	}
	if len(tournament.Players) >= int(tournament.MaxPlayers) {
		return fmt.Errorf("dtl: tournament is full")
	}
	if _, err := validateDTLCommitHash(tx.CommitHash); err != nil {
		return err
	}
	for _, existing := range tournament.Players {
		if normalizeDTLAccount(existing) == player {
			return fmt.Errorf("dtl: player already joined")
		}
	}
	if _, _, _, err := validateDTLSpendable(state, tournament.TokenID, player, tournament.EntryFee); err != nil {
		return err
	}
	return nil
}

func ValidateDTLTournamentRevealTx(state *DTLState, tx DTLTournamentRevealTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	player := normalizeDTLAccount(tx.Player)
	if player == "" {
		return fmt.Errorf("dtl: invalid tournament player")
	}
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	tournament := state.Tournaments[tournamentID]
	if tournament == nil {
		return fmt.Errorf("dtl: unknown tournament")
	}
	if tournament.Settled {
		return fmt.Errorf("dtl: tournament already settled")
	}
	if currentHeight > tournament.RevealDeadline {
		return fmt.Errorf("dtl: tournament reveal deadline passed")
	}
	commit := ""
	if tournament.Commits != nil {
		commit = normalizeDTLHex(tournament.Commits[player])
	}
	if commit == "" {
		return fmt.Errorf("dtl: player is not committed in tournament")
	}
	if tournament.Reveals != nil {
		if strings.TrimSpace(tournament.Reveals[player]) != "" {
			return fmt.Errorf("dtl: player already revealed")
		}
	}
	secret := strings.TrimSpace(tx.Secret)
	if secret == "" {
		return fmt.Errorf("dtl: tournament secret is required")
	}
	if len(secret) > 256 {
		return fmt.Errorf("dtl: tournament secret too long")
	}
	if !strings.EqualFold(DTLDuelCommitHash(secret), commit) {
		return fmt.Errorf("dtl: reveal does not match commitment")
	}
	return nil
}

func ValidateDTLTournamentFinalizeTx(state *DTLState, tx DTLTournamentFinalizeTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	if normalizeDTLAccount(tx.Caller) == "" {
		return fmt.Errorf("dtl: invalid tournament finalize caller")
	}
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	tournament := state.Tournaments[tournamentID]
	if tournament == nil {
		return fmt.Errorf("dtl: unknown tournament")
	}
	if tournament.Settled {
		return fmt.Errorf("dtl: tournament already settled")
	}
	if len(tournament.Players) == 0 {
		if currentHeight < tournament.JoinDeadline {
			return fmt.Errorf("dtl: tournament still waiting for players")
		}
		return nil
	}
	if currentHeight < tournament.RevealDeadline {
		return fmt.Errorf("dtl: tournament still waiting for reveal")
	}
	beaconHeight := tournament.BeaconHeight
	if beaconHeight == 0 {
		beaconHeight = tournament.RevealDeadline + dtlBeaconDelayAtHeight(currentHeight)
	}
	if currentHeight < beaconHeight {
		return fmt.Errorf("dtl: tournament waiting for beacon")
	}
	return nil
}

func ValidateDTLOracleFeedCreateTx(state *DTLState, tx DTLOracleFeedCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid oracle creator")
	}
	base := normalizeDTLTokenID(tx.BaseTokenID)
	quote := normalizeDTLTokenID(tx.QuoteTokenID)
	if base == "" || quote == "" {
		return fmt.Errorf("dtl: oracle requires base and quote token")
	}
	if base == quote {
		return fmt.Errorf("dtl: oracle base and quote must differ")
	}
	seen := make(map[string]struct{}, len(tx.Signers))
	signers := make([]string, 0, len(tx.Signers))
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
	if len(signers) == 0 {
		return fmt.Errorf("dtl: oracle signers required")
	}
	if len(signers) < int(ConfigDTLOracleMinSigners) {
		return fmt.Errorf("dtl: oracle signer count below minimum")
	}
	if tx.Threshold == 0 || int(tx.Threshold) > len(signers) {
		return fmt.Errorf("dtl: invalid oracle threshold")
	}
	if tx.Decimals > DTLMaxDecimals {
		return fmt.Errorf("dtl: invalid oracle decimals")
	}
	if feedID := normalizeDTLTokenID(tx.FeedID); feedID != "" {
		if _, exists := state.OracleFeeds[feedID]; exists {
			return fmt.Errorf("dtl: oracle feed already exists")
		}
	}
	return nil
}

func ValidateDTLOraclePriceSubmitTx(state *DTLState, tx DTLOraclePriceSubmitTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	submitter := normalizeDTLAccount(tx.Submitter)
	if submitter == "" {
		return fmt.Errorf("dtl: invalid oracle submitter")
	}
	if tx.Price == 0 {
		return fmt.Errorf("dtl: oracle price must be > 0")
	}
	feedID := normalizeDTLTokenID(tx.FeedID)
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		return fmt.Errorf("dtl: unknown oracle feed")
	}
	authorized := false
	for _, signer := range feed.Signers {
		if normalizeDTLAccount(signer) == submitter {
			authorized = true
			break
		}
	}
	if !authorized {
		return fmt.Errorf("dtl: submitter is not feed signer")
	}
	if samples := state.OracleSamples[feedID]; samples != nil {
		if sample := samples[submitter]; sample.Height > currentHeight {
			return fmt.Errorf("dtl: oracle sample height regression")
		}
	}
	return nil
}

func normalizeDTLContractLang(raw string) string {
	lang := strings.ToLower(strings.TrimSpace(raw))
	if lang == "dtl-script" {
		return "dtl-script-v1"
	}
	if lang == "dtl-script-v2" {
		return "dtl-script-v2"
	}
	if lang == "dtl-bytecode" || lang == "bytecode" {
		return DTLContractLangBytecodeV1
	}
	return lang
}

func normalizeDTLContractStandard(raw string) string {
	standard := strings.ToUpper(strings.TrimSpace(raw))
	if standard == "" {
		return DTLContractStandardCustom
	}
	return standard
}

func validateDTLContractStandard(raw string) (string, error) {
	standard := normalizeDTLContractStandard(raw)
	switch standard {
	case DTLContractStandardCustom, DTLContractStandardMSC20, DTLContractStandardMSC721, DTLContractStandardMSC1155:
		return standard, nil
	default:
		return "", fmt.Errorf("dtl: unsupported contract standard")
	}
}

func dtlContractDeployUsesV2(tx DTLContractDeployTx) bool {
	standard := normalizeDTLContractStandard(tx.Standard)
	if standard != "" && standard != DTLContractStandardCustom {
		return true
	}
	if strings.TrimSpace(tx.Bytecode) != "" {
		return true
	}
	if len(tx.ABI) > 0 || strings.TrimSpace(tx.MetadataURI) != "" || len(tx.Interfaces) > 0 || tx.Upgradeable || strings.TrimSpace(tx.ProxyTarget) != "" {
		return true
	}
	if tx.LogicPack != nil {
		if tx.LogicPack.Version >= DTLLogicPackVersionV2 {
			return true
		}
		for _, method := range tx.LogicPack.Methods {
			for _, op := range method.Ops {
				if isDTLLogicPackV2Opcode(op.Op) {
					return true
				}
			}
		}
	}
	return false
}

func validateDTLContractOp(raw DTLContractOp) (DTLContractOp, error) {
	op := DTLContractOp(strings.ToUpper(strings.TrimSpace(string(raw))))
	switch op {
	case DTLContractOpSetStr, DTLContractOpSetU64, DTLContractOpAddU64, DTLContractOpSubU64, DTLContractOpTokenTransfer:
		return op, nil
	default:
		return "", fmt.Errorf("dtl: unsupported contract op: %s", raw)
	}
}

func parseDTLContractArgU64(args map[string]string, name string) (uint64, error) {
	v := strings.TrimSpace(args[name])
	if v == "" {
		return 0, fmt.Errorf("dtl: missing contract arg: %s", name)
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("dtl: invalid uint64 arg %s", name)
	}
	return n, nil
}

func parseDTLContractStoredU64(storage map[string]string, key string) (uint64, error) {
	if storage == nil {
		return 0, nil
	}
	raw := strings.TrimSpace(storage[key])
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("dtl: contract storage %s is not uint64", key)
	}
	return n, nil
}

func ValidateDTLContractDeployTx(state *DTLState, chainID string, nonce uint64, tx DTLContractDeployTx) error {
	if dtlContractRuntimeRemoved() {
		return dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	}
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid contract creator")
	}
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid contract name length")
	}
	lang := normalizeDTLContractLang(tx.Lang)
	switch lang {
	case "solidity-like", "vyper-like", "dtl-script-v1", "dtl-script-v2", DTLContractLangBytecodeV1:
	default:
		return fmt.Errorf("dtl: unsupported contract language")
	}
	hasLogicPack := tx.LogicPack != nil
	hasLegacyMethods := len(tx.Methods) > 0
	hasBytecode := strings.TrimSpace(tx.Bytecode) != ""
	sourceCount := 0
	if hasLogicPack {
		sourceCount++
	}
	if hasLegacyMethods {
		sourceCount++
	}
	if hasBytecode {
		sourceCount++
	}
	if sourceCount != 1 {
		found := make([]string, 0, 3)
		if hasLogicPack {
			found = append(found, "logic_pack")
		}
		if hasLegacyMethods {
			found = append(found, "methods")
		}
		if hasBytecode {
			found = append(found, "bytecode")
		}
		foundList := "none"
		if len(found) > 0 {
			foundList = strings.Join(found, ", ")
		}
		return fmt.Errorf("dtl: contract deploy must define exactly one executable source: logic_pack | methods | bytecode (found: %s)", foundList)
	}
	if hasBytecode {
		if lang != DTLContractLangBytecodeV1 && lang != "solidity-like" {
			return fmt.Errorf("dtl: bytecode deploy requires lang dtl-bytecode-v1 or solidity-like")
		}
		if normalizeDTLBytecodeFormat(tx.BytecodeFormat) != DTLBytecodeFormatV1 {
			return fmt.Errorf("dtl: bytecode_format must be %s", DTLBytecodeFormatV1)
		}
		decodedBytes, err := decodeDTLBytecodeHex(tx.Bytecode)
		if err != nil {
			return err
		}
		if ConfigDTLBytecodeMaxSize > 0 && uint64(len(decodedBytes)) > ConfigDTLBytecodeMaxSize {
			return fmt.Errorf("dtl: bytecode exceeds max size")
		}
		if _, _, _, err := decodeNormalizeValidateDTLBytecode(state, tx.Bytecode); err != nil {
			return err
		}
	}
	standard, err := validateDTLContractStandard(tx.Standard)
	if err != nil {
		return err
	}
	if len(tx.MetadataURI) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: metadata_uri too long")
	}
	if len(tx.Interfaces) > DTLMaxContractMethods {
		return fmt.Errorf("dtl: too many interfaces")
	}
	for _, iface := range tx.Interfaces {
		if len(strings.TrimSpace(iface)) > DTLMaxNameLen {
			return fmt.Errorf("dtl: contract interface name too long")
		}
	}
	if strings.TrimSpace(tx.ProxyTarget) != "" && !tx.Upgradeable {
		return fmt.Errorf("dtl: proxy_target requires upgradeable=true")
	}
	if len(strings.TrimSpace(tx.Compiler)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: compiler too long")
	}
	if len(strings.TrimSpace(tx.SourceHash)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: source_hash too long")
	}
	if len(tx.ABI) > 0 {
		var anyObj any
		if err := json.Unmarshal(tx.ABI, &anyObj); err != nil {
			return fmt.Errorf("dtl: invalid contract abi")
		}
	}

	if lang == "dtl-script-v1" && tx.LogicPack == nil {
		return fmt.Errorf("dtl: dtl-script-v1 requires logic_pack")
	}
	if lang == "dtl-script-v2" && tx.LogicPack == nil {
		return fmt.Errorf("dtl: dtl-script-v2 requires logic_pack")
	}
	if lang == DTLContractLangBytecodeV1 && !hasBytecode {
		return fmt.Errorf("dtl: dtl-bytecode-v1 requires bytecode")
	}
	if standard != DTLContractStandardCustom && !hasLogicPack && !hasLegacyMethods && !hasBytecode {
		return fmt.Errorf("dtl: standard contract requires logic_pack, methods, or bytecode")
	}
	var normalizedLogicPack *DTLLogicPack
	if hasLogicPack {
		normalizedLogicPack, err = validateAndNormalizeDTLLogicPack(state, tx.LogicPack)
		if err != nil {
			return err
		}
	}
	if hasBytecode {
		_, normalizedPack, _, err := decodeNormalizeValidateDTLBytecode(state, tx.Bytecode)
		if err != nil {
			return err
		}
		normalizedLogicPack = cloneDTLLogicPack(normalizedPack)
	}
	if !hasLogicPack && !hasBytecode {
		if len(tx.Methods) == 0 || len(tx.Methods) > DTLMaxContractMethods {
			return fmt.Errorf("dtl: invalid contract method count")
		}
		seenMethods := make(map[string]struct{}, len(tx.Methods))
		for _, method := range tx.Methods {
			methodName := normalizeDTLContractMethodName(method.Name)
			if methodName == "" || len(methodName) > DTLMaxNameLen {
				return fmt.Errorf("dtl: invalid contract method name")
			}
			if _, exists := seenMethods[methodName]; exists {
				return fmt.Errorf("dtl: duplicate contract method: %s", methodName)
			}
			seenMethods[methodName] = struct{}{}

			op, err := validateDTLContractOp(method.Op)
			if err != nil {
				return err
			}
			key := strings.TrimSpace(method.Key)
			arg := strings.TrimSpace(method.Arg)
			toArg := strings.TrimSpace(method.ToArg)
			if len(key) > DTLMaxContractKeyLen {
				return fmt.Errorf("dtl: contract key too long")
			}
			if len(arg) > DTLMaxContractKeyLen || len(toArg) > DTLMaxContractKeyLen {
				return fmt.Errorf("dtl: contract arg key too long")
			}

			switch op {
			case DTLContractOpSetStr, DTLContractOpSetU64, DTLContractOpAddU64, DTLContractOpSubU64:
				if key == "" || arg == "" {
					return fmt.Errorf("dtl: contract method %s requires key and arg", methodName)
				}
			case DTLContractOpTokenTransfer:
				tokenID, ok := resolveDTLTokenRef(state, method.TokenID)
				if tokenID == "" {
					return fmt.Errorf("dtl: contract transfer method requires token reference")
				}
				if !ok {
					return ErrDTLUnknownToken
				}
				fromMode := strings.ToLower(strings.TrimSpace(method.From))
				if fromMode == "" {
					fromMode = "caller"
				}
				if fromMode != "caller" && fromMode != "contract" {
					return fmt.Errorf("dtl: contract transfer from must be caller or contract")
				}
				if arg == "" || toArg == "" {
					return fmt.Errorf("dtl: contract transfer method requires arg and to_arg")
				}
			}
		}
	}
	for key, value := range tx.Init {
		k := strings.TrimSpace(key)
		if k == "" || len(k) > DTLMaxContractKeyLen {
			return fmt.Errorf("dtl: invalid contract init key")
		}
		if len(value) > DTLMaxContractValueLen {
			return fmt.Errorf("dtl: contract init value too long")
		}
		if normalizedLogicPack != nil {
			known := false
			for _, field := range normalizedLogicPack.Storage {
				if strings.EqualFold(strings.TrimSpace(field.Key), k) {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("dtl: unknown logic pack storage key in init: %s", k)
			}
		}
	}

	contractID := normalizeDTLContractID(DTLContractIDFromDeploy(chainID, nonce, tx))
	if _, exists := state.Contracts[contractID]; exists {
		return fmt.Errorf("dtl: contract id collision")
	}
	return nil
}

func ValidateDTLContractCallTx(state *DTLState, tx DTLContractCallTx) error {
	if dtlContractRuntimeRemoved() {
		return dtlContractRuntimeRemovedError("CONTRACT_CALL")
	}
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	caller := normalizeDTLAccount(tx.Caller)
	if caller == "" {
		return fmt.Errorf("dtl: invalid contract caller")
	}
	contractID := normalizeDTLContractID(tx.ContractID)
	contract := state.Contracts[contractID]
	if contract == nil {
		return fmt.Errorf("dtl: unknown contract")
	}
	if contract.Paused {
		return ErrDTLPaused
	}
	if len(tx.Args) > DTLMaxContractArgs {
		return fmt.Errorf("dtl: too many contract args")
	}
	methodName := normalizeDTLContractMethodName(tx.Method)
	if methodName == "" {
		return fmt.Errorf("dtl: missing contract method")
	}
	if strings.TrimSpace(contract.Bytecode) != "" {
		program, normalizedPack, _, err := decodeNormalizeValidateDTLBytecode(state, contract.Bytecode)
		if err != nil {
			return err
		}
		if !methodExistsInDTLBytecodeProgram(program, methodName) {
			return fmt.Errorf("dtl: unknown contract method: %s", methodName)
		}
		execContract := *contract
		execContract.LogicPack = cloneDTLLogicPack(normalizedPack)
		return validateDTLLogicPackCall(state, contractID, &execContract, tx)
	}
	if contract.LogicPack != nil {
		return validateDTLLogicPackCall(state, contractID, contract, tx)
	}
	method := contract.Methods[methodName]
	if method == nil {
		return fmt.Errorf("dtl: unknown contract method: %s", methodName)
	}
	op, err := validateDTLContractOp(method.Op)
	if err != nil {
		return err
	}

	switch op {
	case DTLContractOpSetStr:
		v := strings.TrimSpace(tx.Args[method.Arg])
		if v == "" {
			return fmt.Errorf("dtl: missing contract arg: %s", method.Arg)
		}
		if len(v) > DTLMaxContractValueLen {
			return fmt.Errorf("dtl: contract value too long")
		}
	case DTLContractOpSetU64:
		if _, err := parseDTLContractArgU64(tx.Args, method.Arg); err != nil {
			return err
		}
	case DTLContractOpAddU64:
		cur, err := parseDTLContractStoredU64(contract.Storage, method.Key)
		if err != nil {
			return err
		}
		add, err := parseDTLContractArgU64(tx.Args, method.Arg)
		if err != nil {
			return err
		}
		if _, err := dtlSafeAddU64(cur, add); err != nil {
			return err
		}
	case DTLContractOpSubU64:
		cur, err := parseDTLContractStoredU64(contract.Storage, method.Key)
		if err != nil {
			return err
		}
		sub, err := parseDTLContractArgU64(tx.Args, method.Arg)
		if err != nil {
			return err
		}
		if sub > cur {
			return fmt.Errorf("dtl: contract subtraction underflow")
		}
	case DTLContractOpTokenTransfer:
		amount, err := parseDTLContractArgU64(tx.Args, method.Arg)
		if err != nil {
			return err
		}
		if amount == 0 {
			return fmt.Errorf("dtl: transfer amount must be > 0")
		}
		to := normalizeDTLAccount(tx.Args[method.ToArg])
		if to == "" {
			return fmt.Errorf("dtl: invalid transfer recipient")
		}
		tokenID, ok := resolveDTLTokenRef(state, method.TokenID)
		if tokenID == "" || !ok {
			return ErrDTLUnknownToken
		}
		fromMode := strings.ToLower(strings.TrimSpace(method.From))
		if fromMode == "" {
			fromMode = "caller"
		}
		switch fromMode {
		case "caller":
			if _, _, _, err := validateDTLSpendable(state, tokenID, caller, amount); err != nil {
				return err
			}
		case "contract":
			if _, _, _, err := validateDTLSpendable(state, tokenID, dtlContractVaultAccount(contractID), amount); err != nil {
				return err
			}
		default:
			return fmt.Errorf("dtl: invalid transfer from mode")
		}
	}
	return nil
}

func ValidateDTLGovernanceCert(
	token *DTLTokenState,
	cert DTLGovernanceCert,
	expectedAction DTLGovernanceAction,
	expectedPayloadHash string,
	currentEpoch uint64,
	replayWindow uint64,
) error {
	if token == nil {
		return ErrDTLUnknownToken
	}

	if cert.Action != expectedAction {
		return fmt.Errorf("dtl: cert action mismatch: got %s expected %s", cert.Action, expectedAction)
	}
	if normalizeDTLTokenID(cert.TokenID) != normalizeDTLTokenID(token.TokenID) {
		return fmt.Errorf("dtl: cert token id mismatch")
	}
	if strings.TrimSpace(cert.ActionPayloadHash) == "" {
		return fmt.Errorf("dtl: empty action payload hash")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.ActionPayloadHash), strings.TrimSpace(expectedPayloadHash)) {
		return fmt.Errorf("dtl: action payload hash mismatch")
	}
	if len(cert.Signers) == 0 {
		return fmt.Errorf("dtl: cert has no signers")
	}
	if len(cert.Signers) != len(cert.Signatures) {
		return fmt.Errorf("dtl: signer/signature length mismatch")
	}
	if len(cert.SignerPublicKeys) > 0 && len(cert.SignerPublicKeys) != len(cert.Signers) {
		return fmt.Errorf("dtl: signer/public key length mismatch")
	}
	if token.AuthorityThreshold == 0 {
		return fmt.Errorf("dtl: token threshold must be > 0")
	}

	if replayWindow > 0 && currentEpoch > cert.Epoch && currentEpoch-cert.Epoch > replayWindow {
		return fmt.Errorf("dtl: cert epoch is stale")
	}

	allowed := make(map[string]struct{}, len(token.AuthoritySigners))
	for _, signer := range token.AuthoritySigners {
		n := normalizeDTLAccount(signer)
		if n == "" {
			continue
		}
		allowed[n] = struct{}{}
	}

	signBytes := DTLGovernanceCertSignBytes(
		cert.TokenID,
		cert.Epoch,
		cert.Action,
		cert.ActionPayloadHash,
	)

	validUnique := make(map[string]struct{})
	for i, signer := range cert.Signers {
		n := normalizeDTLAccount(signer)
		if n == "" {
			return fmt.Errorf("dtl: empty signer in cert")
		}
		if _, dup := validUnique[n]; dup {
			return fmt.Errorf("dtl: duplicate signer in cert: %s", n)
		}
		if _, ok := allowed[n]; !ok {
			return fmt.Errorf("dtl: signer not in authority set: %s", n)
		}
		sigHex := normalizeDTLHex(cert.Signatures[i])
		if sigHex == "" {
			return fmt.Errorf("dtl: empty signature for signer %s", n)
		}
		sig, err := hex.DecodeString(sigHex)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("dtl: invalid signature for signer %s", n)
		}

		pub, err := resolveDTLGovernanceSignerPublicKey(signer, certSignerPublicKeyAt(cert, i))
		if err != nil {
			return err
		}
		if isMSCAddressLike(n) && !AddressMatchesPublicKey(n, pub) {
			return fmt.Errorf("dtl: signer/public key mismatch for %s", n)
		}
		if !ed25519.Verify(pub, signBytes, sig) {
			return fmt.Errorf("dtl: signature verification failed for signer %s", n)
		}

		validUnique[n] = struct{}{}
	}
	if len(validUnique) < int(token.AuthorityThreshold) {
		return fmt.Errorf(
			"dtl: threshold not met: got %d require %d",
			len(validUnique),
			token.AuthorityThreshold,
		)
	}
	return nil
}

func certSignerPublicKeyAt(cert DTLGovernanceCert, idx int) string {
	if idx < 0 || idx >= len(cert.SignerPublicKeys) {
		return ""
	}
	return cert.SignerPublicKeys[idx]
}

func resolveDTLGovernanceSignerPublicKey(signerRaw, provided string) (ed25519.PublicKey, error) {
	provided = normalizeDTLHex(provided)
	if provided != "" {
		b, err := hex.DecodeString(provided)
		if err != nil || len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("dtl: invalid signer public key for %s", normalizeDTLAccount(signerRaw))
		}
		out := make([]byte, len(b))
		copy(out, b)
		return ed25519.PublicKey(out), nil
	}

	signerID := strings.TrimSpace(signerRaw)
	signerNorm := normalizeValidatorID(signerID)
	validatorPubKeysMu.RLock()
	defer validatorPubKeysMu.RUnlock()

	tryGet := func(key string) (ed25519.PublicKey, bool) {
		if key == "" {
			return nil, false
		}
		if pk, ok := ValidatorPubKeys[key]; ok && len(pk) == ed25519.PublicKeySize {
			out := make([]byte, len(pk))
			copy(out, pk)
			return ed25519.PublicKey(out), true
		}
		if pk, ok := GenesisValidatorPubKeys[key]; ok && len(pk) == ed25519.PublicKeySize {
			out := make([]byte, len(pk))
			copy(out, pk)
			return ed25519.PublicKey(out), true
		}
		return nil, false
	}

	if pk, ok := tryGet(signerNorm); ok {
		return pk, nil
	}
	if pk, ok := tryGet(signerID); ok {
		return pk, nil
	}
	return nil, fmt.Errorf(
		"dtl: missing signer public key for %s (provide signer_public_keys in gcert)",
		normalizeDTLAccount(signerRaw),
	)
}

func isMSCAddressLike(v string) bool {
	v = strings.TrimSpace(v)
	return len(v) >= 3 && strings.EqualFold(v[:3], "MSC")
}

func ValidateDTLRotateAuthorityPayload(p DTLRotateAuthorityPayload) error {
	if p.AuthorityThreshold == 0 {
		return fmt.Errorf("dtl: rotate threshold must be > 0")
	}
	seen := make(map[string]struct{})
	for _, signer := range p.AuthoritySigners {
		n := normalizeDTLAccount(signer)
		if n == "" {
			return fmt.Errorf("dtl: rotate has empty signer")
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("dtl: rotate has duplicate signer %s", n)
		}
		seen[n] = struct{}{}
	}
	if len(seen) == 0 {
		return errors.New("dtl: rotate has empty signer set")
	}
	if int(p.AuthorityThreshold) > len(seen) {
		return fmt.Errorf("dtl: rotate threshold exceeds signer set")
	}
	return nil
}
