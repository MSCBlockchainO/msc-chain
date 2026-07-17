package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ValidateDTLCreateTx validates dtl create tx.
func ValidateDTLCreateTx(state *DTLState, tx DTLCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `name` stores the value produced by this operation.
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid name length")
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(tx.Symbol)
	if symbol == "" || len(symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid symbol length")
	}
	// `exists` stores whether the related condition is satisfied.
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

	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	// `signer` tracks the current values while iterating.
	for _, signer := range tx.AuthoritySigners {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(signer)
		if n == "" {
			return fmt.Errorf("dtl: empty authority signer")
		}
		if !isMSCAddressLike(n) {
			return fmt.Errorf("dtl: authority signer must be an MSC address: %s", n)
		}
		// `exists` stores whether the related condition is satisfied.
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

// ValidateDTLTransferTx validates dtl transfer tx.
func ValidateDTLTransferTx(state *DTLState, tx DTLTransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` and `exists` store whether the related condition is satisfied.
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: transfer amount must be > 0")
	}

	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
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

// ValidateDTLApproveTx validates dtl approve tx.
func ValidateDTLApproveTx(state *DTLState, tx DTLApproveTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` and `exists` store whether the related condition is satisfied.
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}

	// `owner` stores the value produced by this operation.
	owner := normalizeDTLAccount(tx.Owner)
	// `spender` stores the value produced by this operation.
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

// ValidateDTLTransferFromTx validates dtl transfer from tx.
func ValidateDTLTransferFromTx(state *DTLState, tx DTLTransferFromTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` and `exists` store whether the related condition is satisfied.
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: transfer amount must be > 0")
	}

	// `spender` stores the value produced by this operation.
	spender := normalizeDTLAccount(tx.Spender)
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
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

// ValidateDTLBurnTx validates dtl burn tx.
func ValidateDTLBurnTx(state *DTLState, tx DTLBurnTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` and `exists` store whether the related condition is satisfied.
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: burn amount must be > 0")
	}
	// `from` stores the value produced by this operation.
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

// ValidateDTLMintTx validates dtl mint tx.
func ValidateDTLMintTx(state *DTLState, tx DTLMintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` and `exists` store whether the related condition is satisfied.
	token, exists := state.Tokens[tokenID]
	if !exists {
		return ErrDTLUnknownToken
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: mint amount must be > 0")
	}
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	if to == "" {
		return fmt.Errorf("dtl: invalid mint receiver")
	}
	if token.TotalSupply > token.MaxSupply || token.MaxSupply-token.TotalSupply < tx.Amount {
		return fmt.Errorf("dtl: mint exceeds max supply")
	}
	return nil
}

// ValidateDTLNFT721CreateTx validates dtlnft721 create tx.
func ValidateDTLNFT721CreateTx(state *DTLState, tx DTLNFT721CreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid nft721 creator")
	}
	// `name` stores the value produced by this operation.
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid nft721 name length")
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(tx.Symbol)
	if symbol == "" || len(symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid nft721 symbol length")
	}
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.NFT721SymbolIndex[symbol]; exists {
		return fmt.Errorf("dtl: nft721 symbol already exists: %s", symbol)
	}
	if len(strings.TrimSpace(tx.BaseURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft721 base uri too long")
	}
	return nil
}

// ValidateDTLNFT721MintTx validates dtlnft721 mint tx.
func ValidateDTLNFT721MintTx(state *DTLState, tx DTLNFT721MintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `collection` stores the value produced by this operation.
	collection := state.NFT721Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" || creator != normalizeDTLAccount(collection.Creator) {
		return fmt.Errorf("dtl: nft721 mint creator mismatch")
	}
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	if to == "" {
		return fmt.Errorf("dtl: invalid nft721 receiver")
	}
	if len(strings.TrimSpace(tx.TokenURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft721 token uri too long")
	}
	return nil
}

// ValidateDTLNFT721TransferTx validates dtlnft721 transfer tx.
func ValidateDTLNFT721TransferTx(state *DTLState, tx DTLNFT721TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `collection` stores the value produced by this operation.
	collection := state.NFT721Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	if from == "" || to == "" {
		return fmt.Errorf("dtl: invalid nft721 account")
	}
	if from == to {
		return fmt.Errorf("dtl: nft721 self transfer not allowed")
	}
	// `owner` stores the value produced by this operation.
	owner := state.NFT721OwnerOf(collectionID, tx.TokenID)
	if owner == "" {
		return ErrDTLUnknownNFTToken
	}
	if normalizeDTLAccount(owner) != from {
		return ErrDTLNotNFTTokenOwner
	}
	return nil
}

// ValidateDTLNFT1155CreateTx validates dtlnft1155 create tx.
func ValidateDTLNFT1155CreateTx(state *DTLState, tx DTLNFT1155CreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid nft1155 creator")
	}
	// `name` stores the value produced by this operation.
	name := strings.TrimSpace(tx.Name)
	if name == "" || len(name) > DTLMaxNameLen {
		return fmt.Errorf("dtl: invalid nft1155 name length")
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(tx.Symbol)
	if symbol == "" || len(symbol) > DTLMaxSymbolLen {
		return fmt.Errorf("dtl: invalid nft1155 symbol length")
	}
	// `exists` stores whether the related condition is satisfied.
	if _, exists := state.NFT1155SymbolIndex[symbol]; exists {
		return fmt.Errorf("dtl: nft1155 symbol already exists: %s", symbol)
	}
	if len(strings.TrimSpace(tx.BaseURI)) > DTLMaxContractValueLen {
		return fmt.Errorf("dtl: nft1155 base uri too long")
	}
	return nil
}

// ValidateDTLNFT1155MintTx validates dtlnft1155 mint tx.
func ValidateDTLNFT1155MintTx(state *DTLState, tx DTLNFT1155MintTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `collection` stores the value produced by this operation.
	collection := state.NFT1155Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" || creator != normalizeDTLAccount(collection.Creator) {
		return fmt.Errorf("dtl: nft1155 mint creator mismatch")
	}
	// `to` stores the value produced by this operation.
	to := normalizeDTLAccount(tx.To)
	if to == "" {
		return fmt.Errorf("dtl: invalid nft1155 receiver")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: nft1155 mint amount must be > 0")
	}
	return nil
}

// ValidateDTLNFT1155TransferTx validates dtlnft1155 transfer tx.
func ValidateDTLNFT1155TransferTx(state *DTLState, tx DTLNFT1155TransferTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(tx.CollectionID)
	// `collection` stores the value produced by this operation.
	collection := state.NFT1155Collections[collectionID]
	if collection == nil {
		return ErrDTLUnknownNFTCollection
	}
	if collection.Paused {
		return ErrDTLPaused
	}
	// `from` stores the value produced by this operation.
	from := normalizeDTLAccount(tx.From)
	// `to` stores the value produced by this operation.
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

// validateDTLSpendable validates dtl spendable.
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

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tokenIDRaw)
	// `token` stores the value produced by this operation.
	token := state.Tokens[tokenID]
	if token == nil {
		return nil, "", "", ErrDTLUnknownToken
	}
	if amount == 0 {
		return nil, "", "", fmt.Errorf("dtl: amount must be > 0")
	}

	// `account` stores the measured quantity used by this operation.
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

// validateDTLCommitHash validates dtl commit hash.
func validateDTLCommitHash(raw string) (string, error) {
	// `h` stores the value produced by this operation.
	h := normalizeDTLHex(raw)
	if h == "" {
		return "", fmt.Errorf("dtl: commit hash is required")
	}
	if len(h) != 64 {
		return "", fmt.Errorf("dtl: commit hash must be 32-byte hex")
	}
	// `err` stores the error produced by this operation.
	if _, err := hex.DecodeString(h); err != nil {
		return "", fmt.Errorf("dtl: invalid commit hash")
	}
	return h, nil
}

// ValidateDTLPoolCreateTx validates dtl pool create tx.
func ValidateDTLPoolCreateTx(state *DTLState, tx DTLPoolCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid pool creator")
	}
	// `tokenA` stores the value produced by this operation.
	tokenA := normalizeDTLTokenID(tx.TokenA)
	// `tokenB` stores the value produced by this operation.
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
	// `pairKey` stores the key used to access the related value.
	pairKey := dtlPoolPairKey(tokenA, tokenB)
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLPoolID(state.PoolIndex[pairKey]); existing != "" {
		return fmt.Errorf("dtl: pool already exists for pair: %s", existing)
	}
	// `tokenAState` and `err` store the error produced by this operation.
	tokenAState, _, _, err := validateDTLSpendable(state, tokenA, creator, tx.AmountA)
	if err != nil {
		return err
	}
	// `tokenBState` and `err` store the error produced by this operation.
	tokenBState, _, _, err := validateDTLSpendable(state, tokenB, creator, tx.AmountB)
	if err != nil {
		return err
	}
	if tokenAState.TaxBPS > 0 || tokenBState.TaxBPS > 0 {
		return fmt.Errorf("dtl: pool tokens with transfer tax are not supported")
	}
	// `share` and `err` store the error produced by this operation.
	share, err := dtlInitialPoolShare(tx.AmountA, tx.AmountB)
	if err != nil {
		return err
	}
	if share == 0 {
		return fmt.Errorf("dtl: initial LP share must be > 0")
	}
	return nil
}

// ValidateDTLPoolAddLiquidityTx validates dtl pool add liquidity tx.
func ValidateDTLPoolAddLiquidityTx(state *DTLState, tx DTLPoolAddLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	// `pool` stores the value produced by this operation.
	pool := state.Pools[poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	// `provider` stores the value produced by this operation.
	provider := normalizeDTLAccount(tx.Provider)
	if provider == "" {
		return fmt.Errorf("dtl: invalid liquidity provider")
	}
	if tx.AmountA == 0 || tx.AmountB == 0 {
		return fmt.Errorf("dtl: add liquidity amounts must be > 0")
	}
	// `tokenAState` and `err` store the error produced by this operation.
	tokenAState, _, _, err := validateDTLSpendable(state, pool.TokenA, provider, tx.AmountA)
	if err != nil {
		return err
	}
	// `tokenBState` and `err` store the error produced by this operation.
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
	// `share` and `err` store the error produced by this operation.
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

// ValidateDTLPoolRemoveLiquidityTx validates dtl pool remove liquidity tx.
func ValidateDTLPoolRemoveLiquidityTx(state *DTLState, tx DTLPoolRemoveLiquidityTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	// `pool` stores the value produced by this operation.
	pool := state.Pools[poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	// `provider` stores the value produced by this operation.
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
	// `outA`, `outB`, and `err` store the error produced by this operation.
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

// ValidateDTLPoolSwapTx validates dtl pool swap tx.
func ValidateDTLPoolSwapTx(state *DTLState, tx DTLPoolSwapTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	// `pool` stores the value produced by this operation.
	pool := state.Pools[poolID]
	if pool == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	// `trader` stores the value produced by this operation.
	trader := normalizeDTLAccount(tx.Trader)
	if trader == "" {
		return fmt.Errorf("dtl: invalid trader")
	}
	if tx.AmountIn == 0 {
		return fmt.Errorf("dtl: swap amount_in must be > 0")
	}
	// `tokenIn` stores the value produced by this operation.
	tokenIn := normalizeDTLTokenID(tx.TokenIn)
	if tokenIn != pool.TokenA && tokenIn != pool.TokenB {
		return fmt.Errorf("dtl: token_in not in pool")
	}
	// `tokenState` and `err` store the error produced by this operation.
	tokenState, _, _, err := validateDTLSpendable(state, tokenIn, trader, tx.AmountIn)
	if err != nil {
		return err
	}
	if tokenState.TaxBPS > 0 {
		return fmt.Errorf("dtl: pool tokens with transfer tax are not supported")
	}
	// `reserveIn` stores the result produced by this operation.
	reserveIn := pool.ReserveA
	// `reserveOut` stores the result produced by this operation.
	reserveOut := pool.ReserveB
	if tokenIn == pool.TokenB {
		reserveIn = pool.ReserveB
		reserveOut = pool.ReserveA
	}
	// `amountOut` and `err` store the error produced by this operation.
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

// ValidateDTLPoolSwapRouteTx validates dtl pool swap route tx.
func ValidateDTLPoolSwapRouteTx(state *DTLState, tx DTLPoolSwapRouteTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if !dtlProtocolRouterEnabled() {
		return fmt.Errorf("dtl: router is disabled")
	}

	// `trader` stores the value produced by this operation.
	trader := normalizeDTLAccount(tx.Trader)
	if trader == "" {
		return fmt.Errorf("dtl: invalid trader")
	}

	// `tokenIn` stores the value produced by this operation.
	tokenIn := normalizeDTLTokenID(tx.TokenIn)
	if tokenIn == "" {
		return fmt.Errorf("dtl: token_in is required")
	}
	if tx.AmountIn == 0 {
		return fmt.Errorf("dtl: swap amount_in must be > 0")
	}

	// `hops` stores the value produced by this operation.
	hops := len(tx.Path)
	if hops < 1 {
		return fmt.Errorf("dtl: route path must contain at least 1 pool")
	}
	if hops > dtlProtocolRouterMaxHops() {
		return fmt.Errorf("dtl: route path exceeds max hops")
	}

	if tx.DeadlineHeight == 0 {
		return fmt.Errorf("dtl: route deadline_height is required")
	}
	if tx.DeadlineHeight < currentHeight {
		return fmt.Errorf("dtl: route deadline expired")
	}
	// `maxDeadline` stores the value produced by this operation.
	maxDeadline := currentHeight + dtlProtocolRouterDeadlineMaxBlocks()
	if maxDeadline < currentHeight || tx.DeadlineHeight > maxDeadline {
		return fmt.Errorf("dtl: route deadline too far")
	}

	// `currentToken` stores the value produced by this operation.
	currentToken := tokenIn
	// `i` and `rawPoolID` track the current position in the related collection.
	for i, rawPoolID := range tx.Path {
		// `poolID` stores the value produced by this operation.
		poolID := normalizeDTLPoolID(rawPoolID)
		if poolID == "" {
			return fmt.Errorf("dtl: route path has empty pool id at hop %d", i+1)
		}
		// `pool` stores the value produced by this operation.
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

	// `quote` and `err` store the error produced by this operation.
	quote, err := dtlQuotePoolSwapRoute(state, tokenIn, tx.AmountIn, tx.Path)
	if err != nil {
		return err
	}

	if tx.MinAmountOut > 0 && quote.AmountOut < tx.MinAmountOut {
		return fmt.Errorf("dtl: slippage: output below minimum")
	}
	if quote.PriceImpactBPS > dtlProtocolRouterMaxPriceImpactBPS() {
		return fmt.Errorf("dtl: route price impact exceeds max")
	}

	// `tradeShadow` stores the value produced by this operation.
	tradeShadow := cloneDTLState(state)
	if tradeShadow == nil {
		return ErrDTLInvalidState
	}
	// `amountIn` stores the value produced by this operation.
	amountIn := tx.AmountIn
	// `routeToken` stores the value produced by this operation.
	routeToken := tokenIn
	// `rawPoolID` tracks the current values while iterating.
	for _, rawPoolID := range tx.Path {
		// `poolID` stores the value produced by this operation.
		poolID := normalizeDTLPoolID(rawPoolID)
		// `amountOut` and `err` store the error produced by this operation.
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
		// `pool` stores the value produced by this operation.
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

// ValidateDTLDuelCreateTx validates dtl duel create tx.
func ValidateDTLDuelCreateTx(state *DTLState, tx DTLDuelCreateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid duel creator")
	}
	// `err` stores the error produced by this operation.
	if _, err := validateDTLCommitHash(tx.CommitHash); err != nil {
		return err
	}
	if tx.Stake == 0 {
		return fmt.Errorf("dtl: duel stake must be > 0")
	}
	// `token` and `err` store the error produced by this operation.
	token, _, _, err := validateDTLSpendable(state, tx.TokenID, creator, tx.Stake)
	if err != nil {
		return err
	}
	if token.TaxBPS > 0 {
		return fmt.Errorf("dtl: duel token with transfer tax is not supported")
	}

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
	if joinBlocks == 0 || revealBlocks == 0 {
		return fmt.Errorf("dtl: duel deadlines must be > 0")
	}
	if currentHeight > ^uint64(0)-joinBlocks || currentHeight+joinBlocks > ^uint64(0)-revealBlocks {
		return fmt.Errorf("dtl: duel deadline overflow")
	}
	return nil
}

// ValidateDTLDuelJoinTx validates dtl duel join tx.
func ValidateDTLDuelJoinTx(state *DTLState, tx DTLDuelJoinTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(tx.DuelID)
	// `duel` stores the value produced by this operation.
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

	// `joiner` stores the current position in the related collection.
	joiner := normalizeDTLAccount(tx.Joiner)
	if joiner == "" {
		return fmt.Errorf("dtl: invalid duel joiner")
	}
	if joiner == normalizeDTLAccount(duel.PlayerA) {
		return fmt.Errorf("dtl: creator cannot join own duel")
	}
	// `err` stores the error produced by this operation.
	if _, err := validateDTLCommitHash(tx.CommitHash); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	_, _, _, err := validateDTLSpendable(state, duel.TokenID, joiner, duel.Stake)
	return err
}

// ValidateDTLDuelRevealTx validates dtl duel reveal tx.
func ValidateDTLDuelRevealTx(state *DTLState, tx DTLDuelRevealTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(tx.DuelID)
	// `duel` stores the value produced by this operation.
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
	// `player` stores the value produced by this operation.
	player := normalizeDTLAccount(tx.Player)
	if player == "" {
		return fmt.Errorf("dtl: invalid reveal player")
	}
	// `secret` stores the value produced by this operation.
	secret := strings.TrimSpace(tx.Secret)
	if secret == "" {
		return fmt.Errorf("dtl: reveal secret is required")
	}
	if len(secret) > 256 {
		return fmt.Errorf("dtl: reveal secret too long")
	}

	// `secretHash` stores the digest used to identify or verify the related data.
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

// ValidateDTLDuelFinalizeTx validates dtl duel finalize tx.
func ValidateDTLDuelFinalizeTx(state *DTLState, tx DTLDuelFinalizeTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	if normalizeDTLAccount(tx.Caller) == "" {
		return fmt.Errorf("dtl: invalid finalize caller")
	}
	// `duelID` stores the value produced by this operation.
	duelID := normalizeDTLTokenID(tx.DuelID)
	// `duel` stores the value produced by this operation.
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
		// `beaconHeight` stores the value produced by this operation.
		beaconHeight := duel.BeaconHeight
		if beaconHeight == 0 {
			beaconHeight = duel.RevealDeadline + dtlProtocolBeaconDelayAtHeight(currentHeight)
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

// validateDTLLendingRiskParams validates dtl lending risk params.
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

// getDTLLendingPosition implements the get dtl lending position helper.
func getDTLLendingPosition(state *DTLState, marketID, account string) *DTLLendingPositionState {
	if state == nil {
		return nil
	}
	return state.LendingPositions[dtlLendingPositionKey(marketID, account)]
}

// ValidateDTLLendMarketCreateTx validates dtl lend market create tx.
func ValidateDTLLendMarketCreateTx(state *DTLState, tx DTLLendMarketCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid lending market creator")
	}
	// `collateralTokenID` stores the value produced by this operation.
	collateralTokenID := normalizeDTLTokenID(tx.CollateralTokenID)
	// `debtTokenID` stores the value produced by this operation.
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
	// `err` stores the error produced by this operation.
	if _, _, err := validateDTLLendingRiskParams(tx.CollateralFactorBPS, tx.LiquidationBonusBPS); err != nil {
		return err
	}
	if tx.ReserveFactorBPS > DTLMaxTaxBPS {
		return fmt.Errorf("dtl: invalid reserve factor")
	}
	if tx.CloseFactorBPS > DTLMaxTaxBPS {
		return fmt.Errorf("dtl: invalid close factor")
	}
	// `pairKey` stores the key used to access the related value.
	pairKey := dtlLendingPairKey(collateralTokenID, debtTokenID)
	// `existing` stores the value produced by this operation.
	if existing := normalizeDTLMarketID(state.LendingIndex[pairKey]); existing != "" {
		return fmt.Errorf("dtl: lending market already exists for pair: %s", existing)
	}
	// `collateralToken` stores the value produced by this operation.
	collateralToken := state.Tokens[collateralTokenID]
	if collateralToken == nil {
		return ErrDTLUnknownToken
	}
	// `debtToken` and `err` store the error produced by this operation.
	debtToken, _, _, err := validateDTLSpendable(state, debtTokenID, creator, tx.DebtLiquidity)
	if err != nil {
		return err
	}
	// `feedID` stores the value produced by this operation.
	if feedID := normalizeDTLTokenID(tx.CollateralFeedID); feedID != "" && state.OracleFeeds[feedID] == nil {
		return fmt.Errorf("dtl: unknown collateral oracle feed")
	}
	// `feedID` stores the value produced by this operation.
	if feedID := normalizeDTLTokenID(tx.DebtFeedID); feedID != "" && state.OracleFeeds[feedID] == nil {
		return fmt.Errorf("dtl: unknown debt oracle feed")
	}
	if collateralToken.TaxBPS > 0 || debtToken.TaxBPS > 0 {
		return fmt.Errorf("dtl: lending tokens with transfer tax are not supported")
	}
	return nil
}

// ValidateDTLLendDepositCollateralTx validates dtl lend deposit collateral tx.
func ValidateDTLLendDepositCollateralTx(state *DTLState, tx DTLLendDepositCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: deposit amount must be > 0")
	}
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	// `collateralToken` and `err` store the error produced by this operation.
	collateralToken, _, _, err := validateDTLSpendable(state, market.CollateralTokenID, account, tx.Amount)
	if err != nil {
		return err
	}
	if collateralToken.TaxBPS > 0 {
		return fmt.Errorf("dtl: lending tokens with transfer tax are not supported")
	}
	return nil
}

// ValidateDTLLendBorrowTx validates dtl lend borrow tx.
func ValidateDTLLendBorrowTx(state *DTLState, tx DTLLendBorrowTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: borrow amount must be > 0")
	}
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	// `position` stores the value produced by this operation.
	position := getDTLLendingPosition(state, marketID, account)
	if position == nil || position.Collateral == 0 {
		return fmt.Errorf("dtl: no collateral position")
	}
	// `newDebt` and `err` store the error produced by this operation.
	newDebt, err := dtlSafeAddU64(position.Debt, tx.Amount)
	if err != nil {
		return err
	}
	// `healthy` and `err` store the error produced by this operation.
	healthy, err := dtlLendingIsHealthy(position.Collateral, newDebt, market.CollateralFactorBPS)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: borrow would exceed collateral limit")
	}
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if _, _, _, err := validateDTLSpendable(state, market.DebtTokenID, vault, tx.Amount); err != nil {
		return err
	}
	return nil
}

// ValidateDTLLendRepayTx validates dtl lend repay tx.
func ValidateDTLLendRepayTx(state *DTLState, tx DTLLendRepayTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: repay amount must be > 0")
	}
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	// `position` stores the value produced by this operation.
	position := getDTLLendingPosition(state, marketID, account)
	if position == nil || position.Debt == 0 {
		return fmt.Errorf("dtl: no outstanding debt")
	}
	if tx.Amount > position.Debt {
		return fmt.Errorf("dtl: repay exceeds outstanding debt")
	}
	// `err` stores the error produced by this operation.
	if _, _, _, err := validateDTLSpendable(state, market.DebtTokenID, account, tx.Amount); err != nil {
		return err
	}
	return nil
}

// ValidateDTLLendWithdrawCollateralTx validates dtl lend withdraw collateral tx.
func ValidateDTLLendWithdrawCollateralTx(state *DTLState, tx DTLLendWithdrawCollateralTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid lending account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: withdraw amount must be > 0")
	}
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	// `position` stores the value produced by this operation.
	position := getDTLLendingPosition(state, marketID, account)
	if position == nil || position.Collateral < tx.Amount {
		return fmt.Errorf("dtl: insufficient collateral")
	}
	// `remainingCollateral` stores the value produced by this operation.
	remainingCollateral := position.Collateral - tx.Amount
	// `healthy` and `err` store the error produced by this operation.
	healthy, err := dtlLendingIsHealthy(remainingCollateral, position.Debt, market.CollateralFactorBPS)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("dtl: withdraw would make position unhealthy")
	}
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if _, _, _, err := validateDTLSpendable(state, market.CollateralTokenID, vault, tx.Amount); err != nil {
		return err
	}
	return nil
}

// ValidateDTLLendLiquidateTx validates dtl lend liquidate tx.
func ValidateDTLLendLiquidateTx(state *DTLState, tx DTLLendLiquidateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `liquidator` stores the value produced by this operation.
	liquidator := normalizeDTLAccount(tx.Liquidator)
	// `borrower` stores the value produced by this operation.
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
	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(tx.MarketID)
	// `market` stores the value produced by this operation.
	market := state.LendingMarkets[marketID]
	if market == nil {
		return fmt.Errorf("dtl: unknown lending market")
	}
	// `position` stores the value produced by this operation.
	position := getDTLLendingPosition(state, marketID, borrower)
	if position == nil || position.Debt == 0 {
		return fmt.Errorf("dtl: borrower has no debt")
	}
	if tx.RepayAmount > position.Debt {
		return fmt.Errorf("dtl: repay exceeds borrower debt")
	}
	// `healthy` and `err` store the error produced by this operation.
	healthy, _, err := dtlLendingHealthFactorBPS(state, market, position.Collateral, position.Debt, currentHeight)
	if err != nil {
		return err
	}
	if healthy {
		return fmt.Errorf("dtl: position is healthy")
	}
	// `err` stores the error produced by this operation.
	if _, _, _, err := validateDTLSpendable(state, market.DebtTokenID, liquidator, tx.RepayAmount); err != nil {
		return err
	}
	// `seize` and `err` store the error produced by this operation.
	seize, err := dtlLendingSeizeCollateral(tx.RepayAmount, market.LiquidationBonusBPS)
	if err != nil {
		return err
	}
	if market.CloseFactorBPS > 0 {
		// `maxRepay` and `err` store the error produced by this operation.
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
	// `vault` stores the value produced by this operation.
	vault := dtlLendingVaultAccount(marketID)
	// `err` stores the error produced by this operation.
	if _, _, _, err := validateDTLSpendable(state, market.CollateralTokenID, vault, seize); err != nil {
		return err
	}
	return nil
}

// ValidateDTLTournamentCreateTx validates dtl tournament create tx.
func ValidateDTLTournamentCreateTx(state *DTLState, tx DTLTournamentCreateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid tournament creator")
	}
	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tx.TokenID)
	// `token` stores the value produced by this operation.
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
	if joinBlocks == 0 || revealBlocks == 0 {
		return fmt.Errorf("dtl: tournament deadlines must be > 0")
	}
	if currentHeight > ^uint64(0)-joinBlocks || currentHeight+joinBlocks > ^uint64(0)-revealBlocks {
		return fmt.Errorf("dtl: tournament deadline overflow")
	}
	return nil
}

// ValidateDTLTournamentJoinTx validates dtl tournament join tx.
func ValidateDTLTournamentJoinTx(state *DTLState, tx DTLTournamentJoinTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `player` stores the value produced by this operation.
	player := normalizeDTLAccount(tx.Player)
	if player == "" {
		return fmt.Errorf("dtl: invalid tournament player")
	}
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	// `tournament` stores the value produced by this operation.
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
	// `err` stores the error produced by this operation.
	if _, err := validateDTLCommitHash(tx.CommitHash); err != nil {
		return err
	}
	// `existing` tracks the current values while iterating.
	for _, existing := range tournament.Players {
		if normalizeDTLAccount(existing) == player {
			return fmt.Errorf("dtl: player already joined")
		}
	}
	// `err` stores the error produced by this operation.
	if _, _, _, err := validateDTLSpendable(state, tournament.TokenID, player, tournament.EntryFee); err != nil {
		return err
	}
	return nil
}

// ValidateDTLTournamentRevealTx validates dtl tournament reveal tx.
func ValidateDTLTournamentRevealTx(state *DTLState, tx DTLTournamentRevealTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `player` stores the value produced by this operation.
	player := normalizeDTLAccount(tx.Player)
	if player == "" {
		return fmt.Errorf("dtl: invalid tournament player")
	}
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	// `tournament` stores the value produced by this operation.
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
	// `commit` stores the value produced by this operation.
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
	// `secret` stores the value produced by this operation.
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

// ValidateDTLTournamentFinalizeTx validates dtl tournament finalize tx.
func ValidateDTLTournamentFinalizeTx(state *DTLState, tx DTLTournamentFinalizeTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	if normalizeDTLAccount(tx.Caller) == "" {
		return fmt.Errorf("dtl: invalid tournament finalize caller")
	}
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(tx.TournamentID)
	// `tournament` stores the value produced by this operation.
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
	// `beaconHeight` stores the value produced by this operation.
	beaconHeight := tournament.BeaconHeight
	if beaconHeight == 0 {
		beaconHeight = tournament.RevealDeadline + dtlProtocolBeaconDelayAtHeight(currentHeight)
	}
	if currentHeight < beaconHeight {
		return fmt.Errorf("dtl: tournament waiting for beacon")
	}
	return nil
}

// ValidateDTLOracleFeedCreateTx validates dtl oracle feed create tx.
func ValidateDTLOracleFeedCreateTx(state *DTLState, tx DTLOracleFeedCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid oracle creator")
	}
	// `base` stores the value produced by this operation.
	base := normalizeDTLTokenID(tx.BaseTokenID)
	// `quote` stores the value produced by this operation.
	quote := normalizeDTLTokenID(tx.QuoteTokenID)
	if base == "" || quote == "" {
		return fmt.Errorf("dtl: oracle requires base and quote token")
	}
	if base == quote {
		return fmt.Errorf("dtl: oracle base and quote must differ")
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(tx.Signers))
	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(tx.Signers))
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
	if len(signers) == 0 {
		return fmt.Errorf("dtl: oracle signers required")
	}
	if len(signers) < int(dtlProtocolOracleMinSigners()) {
		return fmt.Errorf("dtl: oracle signer count below minimum")
	}
	if tx.Threshold == 0 || int(tx.Threshold) > len(signers) {
		return fmt.Errorf("dtl: invalid oracle threshold")
	}
	if tx.Decimals > DTLMaxDecimals {
		return fmt.Errorf("dtl: invalid oracle decimals")
	}
	// `feedID` stores the value produced by this operation.
	if feedID := normalizeDTLTokenID(tx.FeedID); feedID != "" {
		// `exists` stores whether the related condition is satisfied.
		if _, exists := state.OracleFeeds[feedID]; exists {
			return fmt.Errorf("dtl: oracle feed already exists")
		}
	}
	return nil
}

// ValidateDTLOraclePriceSubmitTx validates dtl oracle price submit tx.
func ValidateDTLOraclePriceSubmitTx(state *DTLState, tx DTLOraclePriceSubmitTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()

	// `submitter` stores the value produced by this operation.
	submitter := normalizeDTLAccount(tx.Submitter)
	if submitter == "" {
		return fmt.Errorf("dtl: invalid oracle submitter")
	}
	if tx.Price == 0 {
		return fmt.Errorf("dtl: oracle price must be > 0")
	}
	// `feedID` stores the value produced by this operation.
	feedID := normalizeDTLTokenID(tx.FeedID)
	// `feed` stores the value produced by this operation.
	feed := state.OracleFeeds[feedID]
	if feed == nil {
		return fmt.Errorf("dtl: unknown oracle feed")
	}
	// `authorized` stores the value produced by this operation.
	authorized := false
	// `signer` tracks the current values while iterating.
	for _, signer := range feed.Signers {
		if normalizeDTLAccount(signer) == submitter {
			authorized = true
			break
		}
	}
	if !authorized {
		return fmt.Errorf("dtl: submitter is not feed signer")
	}
	// `samples` stores the value produced by this operation.
	if samples := state.OracleSamples[feedID]; samples != nil {
		// `sample` stores the value produced by this operation.
		if sample := samples[submitter]; sample.Height > currentHeight {
			return fmt.Errorf("dtl: oracle sample height regression")
		}
	}
	return nil
}

// ValidateDTLContractDeployTx permanently rejects the removed programmable
// contract/VM transaction class. Native DTL transactions remain supported.
func ValidateDTLContractDeployTx(_ *DTLState, _ string, _ uint64, _ DTLContractDeployTx) error {
	return dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
}

// ValidateDTLContractCallTx permanently rejects the removed programmable
// contract/VM transaction class. Native DTL transactions remain supported.
func ValidateDTLContractCallTx(_ *DTLState, _ DTLContractCallTx) error {
	return dtlContractRuntimeRemovedError("CONTRACT_CALL")
}

// ValidateDTLGovernanceCert validates dtl governance cert.
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
	if len(cert.SignerPublicKeys) != len(cert.Signers) {
		return fmt.Errorf("dtl: signer/public key length mismatch")
	}
	if token.AuthorityThreshold == 0 {
		return fmt.Errorf("dtl: token threshold must be > 0")
	}

	if cert.Epoch > currentEpoch {
		return fmt.Errorf("dtl: cert epoch is in the future")
	}
	if replayWindow > 0 && currentEpoch > cert.Epoch && currentEpoch-cert.Epoch > replayWindow {
		return fmt.Errorf("dtl: cert epoch is stale")
	}
	if err := validateDTLGovernanceCertReplayEnvelope(cert, currentEpoch); err != nil {
		return err
	}

	// `allowed` stores whether the related condition is satisfied.
	allowed := make(map[string]struct{}, len(token.AuthoritySigners))
	// `signer` tracks the current values while iterating.
	for _, signer := range token.AuthoritySigners {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(signer)
		if n == "" {
			continue
		}
		allowed[n] = struct{}{}
	}

	// `signBytes` stores the value produced by this operation.
	signBytes := dtlGovernanceCertSigningBytes(cert)

	// `validUnique` stores whether the related condition is satisfied.
	validUnique := make(map[string]struct{})
	// `i` and `signer` track the current position in the related collection.
	for i, signer := range cert.Signers {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(signer)
		if n == "" {
			return fmt.Errorf("dtl: empty signer in cert")
		}
		// `dup` stores the value produced by this operation.
		if _, dup := validUnique[n]; dup {
			return fmt.Errorf("dtl: duplicate signer in cert: %s", n)
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := allowed[n]; !ok {
			return fmt.Errorf("dtl: signer not in authority set: %s", n)
		}
		// `sigHex` stores the value produced by this operation.
		sigHex := normalizeDTLHex(cert.Signatures[i])
		if sigHex == "" {
			return fmt.Errorf("dtl: empty signature for signer %s", n)
		}
		// `sig` and `err` store the error produced by this operation.
		sig, err := hex.DecodeString(sigHex)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("dtl: invalid signature for signer %s", n)
		}

		// `pub` and `err` store the error produced by this operation.
		pub, err := resolveDTLGovernanceSignerPublicKey(signer, certSignerPublicKeyAt(cert, i))
		if err != nil {
			return err
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

// validateDTLGovernanceCertReplayEnvelope validates the v2 replay envelope.
func validateDTLGovernanceCertReplayEnvelope(cert DTLGovernanceCert, currentEpoch uint64) error {
	v2 := dtlGovernanceCertHasV2Fields(cert)
	if currentEpoch >= DTLGovernanceCertV2ActivationEpoch && !v2 {
		return fmt.Errorf("dtl: governance cert v2 envelope required")
	}
	if !v2 {
		return nil
	}
	nonce := normalizeDTLGovernanceNonce(cert.Nonce)
	if nonce == "" {
		return fmt.Errorf("dtl: governance cert nonce required")
	}
	if len(nonce) > DTLGovernanceCertMaxNonceLen {
		return fmt.Errorf("dtl: governance cert nonce too long")
	}
	if cert.Sequence == 0 {
		return fmt.Errorf("dtl: governance cert sequence required")
	}
	if cert.Expiry == 0 {
		return fmt.Errorf("dtl: governance cert expiry required")
	}
	if currentEpoch > cert.Expiry {
		return fmt.Errorf("dtl: governance cert expired")
	}
	return nil
}

// certSignerPublicKeyAt implements the cert signer public key at helper.
func certSignerPublicKeyAt(cert DTLGovernanceCert, idx int) string {
	if idx < 0 || idx >= len(cert.SignerPublicKeys) {
		return ""
	}
	return cert.SignerPublicKeys[idx]
}

// resolveDTLGovernanceSignerPublicKey implements the resolve dtl governance signer public key helper.
func resolveDTLGovernanceSignerPublicKey(signerRaw, provided string) (ed25519.PublicKey, error) {
	// `signer` stores the value produced by this operation.
	signer := normalizeDTLAccount(signerRaw)
	if !isMSCAddressLike(signer) {
		return nil, fmt.Errorf("dtl: governance signer must be an MSC address: %s", signer)
	}

	provided = normalizeDTLHex(provided)
	if provided == "" {
		return nil, fmt.Errorf("dtl: signer_public_keys required for %s", signer)
	}
	// `b` and `err` store the error produced by this operation.
	b, err := hex.DecodeString(provided)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("dtl: invalid signer public key for %s", signer)
	}
	// `out` stores the result produced by this operation.
	out := make([]byte, len(b))
	copy(out, b)
	// `pub` stores the value produced by this operation.
	pub := ed25519.PublicKey(out)
	if !AddressMatchesPublicKey(signer, pub) {
		return nil, fmt.Errorf("dtl: signer/public key mismatch for %s", signer)
	}
	return pub, nil
}

// isMSCAddressLike implements the is msc address like helper.
func isMSCAddressLike(v string) bool {
	v = strings.TrimSpace(v)
	return len(v) >= 3 && strings.EqualFold(v[:3], "MSC")
}

// ValidateDTLRotateAuthorityPayload validates dtl rotate authority payload.
func ValidateDTLRotateAuthorityPayload(p DTLRotateAuthorityPayload) error {
	if p.AuthorityThreshold == 0 {
		return fmt.Errorf("dtl: rotate threshold must be > 0")
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	// `signer` tracks the current values while iterating.
	for _, signer := range p.AuthoritySigners {
		// `n` stores the value produced by this operation.
		n := normalizeDTLAccount(signer)
		if n == "" {
			return fmt.Errorf("dtl: rotate has empty signer")
		}
		if !isMSCAddressLike(n) {
			return fmt.Errorf("dtl: rotate signer must be an MSC address: %s", n)
		}
		// `dup` stores the value produced by this operation.
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
