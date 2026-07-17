package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// resolveDTLTokenFromLedger implements the resolve dtl token from ledger helper.
func resolveDTLTokenFromLedger(ledger Ledger, tokenRef string) (string, *DTLTokenState, error) {
	// `ref` stores the value produced by this operation.
	ref := strings.TrimSpace(tokenRef)
	if ref == "" {
		return "", nil, errors.New("missing token reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}

	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(ref)
	// `tok` and `ok` store whether the related condition is satisfied.
	if tok, ok := ledger.DTL.Tokens[tokenID]; ok && tok != nil {
		return tokenID, tok, nil
	}

	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(ref)
	// `tokenIDBySymbol` and `ok` store whether the related condition is satisfied.
	if tokenIDBySymbol, ok := ledger.DTL.SymbolIndex[symbol]; ok {
		tokenIDBySymbol = normalizeDTLTokenID(tokenIDBySymbol)
		// `tok` and `exists` store whether the related condition is satisfied.
		if tok, exists := ledger.DTL.Tokens[tokenIDBySymbol]; exists && tok != nil {
			return tokenIDBySymbol, tok, nil
		}
	}

	return "", nil, errors.New("dtl token not found")
}

// resolveDTLMarketFromLedger implements the resolve dtl market from ledger helper.
func resolveDTLMarketFromLedger(ledger Ledger, marketRef string) (string, *DTLLendingMarketState, error) {
	// `ref` stores the value produced by this operation.
	ref := strings.TrimSpace(marketRef)
	if ref == "" {
		return "", nil, errors.New("missing market reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}

	// `marketID` stores the value produced by this operation.
	marketID := normalizeDTLMarketID(ref)
	// `market` and `ok` store whether the related condition is satisfied.
	if market, ok := ledger.DTL.LendingMarkets[marketID]; ok && market != nil {
		return marketID, market, nil
	}

	// `sep` tracks the current values while iterating.
	for _, sep := range []string{"|", "/", ":"} {
		if !strings.Contains(ref, sep) {
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(ref, sep, 2)
		if len(parts) != 2 {
			continue
		}
		// `left` and `err` store the error produced by this operation.
		left, _, err := resolveDTLTokenFromLedger(ledger, parts[0])
		if err != nil {
			continue
		}
		// `right` and `err` store the error produced by this operation.
		right, _, err := resolveDTLTokenFromLedger(ledger, parts[1])
		if err != nil {
			continue
		}
		// `pairKey` stores the key used to access the related value.
		pairKey := dtlLendingPairKey(left, right)
		// `candidate` stores the value produced by this operation.
		candidate := normalizeDTLMarketID(ledger.DTL.LendingIndex[pairKey])
		if candidate == "" {
			continue
		}
		// `market` and `ok` store whether the related condition is satisfied.
		if market, ok := ledger.DTL.LendingMarkets[candidate]; ok && market != nil {
			return candidate, market, nil
		}
	}

	return "", nil, errors.New("dtl lending market not found")
}

// resolveDTLTournamentFromLedger implements the resolve dtl tournament from ledger helper.
func resolveDTLTournamentFromLedger(ledger Ledger, tournamentRef string) (string, *DTLTournamentState, error) {
	// `ref` stores the value produced by this operation.
	ref := strings.TrimSpace(tournamentRef)
	if ref == "" {
		return "", nil, errors.New("missing tournament reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	// `tournamentID` stores the value produced by this operation.
	tournamentID := normalizeDTLTournamentID(ref)
	// `t` and `ok` store whether the related condition is satisfied.
	if t, ok := ledger.DTL.Tournaments[tournamentID]; ok && t != nil {
		return tournamentID, t, nil
	}
	return "", nil, errors.New("dtl tournament not found")
}

// resolveDTLContractFromLedger implements the resolve dtl contract from ledger helper.
func resolveDTLContractFromLedger(ledger Ledger, contractRef string) (string, *DTLContractState, error) {
	// `ref` stores the value produced by this operation.
	ref := strings.TrimSpace(contractRef)
	if ref == "" {
		return "", nil, errors.New("missing contract reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	// `contractID` stores the value produced by this operation.
	contractID := normalizeDTLContractID(ref)
	// `c` and `ok` store whether the related condition is satisfied.
	if c, ok := ledger.DTL.Contracts[contractID]; ok && c != nil {
		return contractID, c, nil
	}
	return "", nil, errors.New("dtl contract not found")
}

// dtlTokenInfo implements the dtl token info helper.
func (s *Server) dtlTokenInfo(tokenRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `tokenID`, `token`, and `err` store the error produced by this operation.
	tokenID, token, err := resolveDTLTokenFromLedger(ledger, tokenRef)
	if err != nil {
		return nil, err
	}
	// `signers` stores the value produced by this operation.
	signers := append([]string(nil), token.AuthoritySigners...)
	sort.Strings(signers)
	return map[string]any{
		"token_id":            tokenID,
		"name":                token.Name,
		"symbol":              token.Symbol,
		"decimals":            token.Decimals,
		"max_supply":          encodeRPCQuantityUint64(token.MaxSupply),
		"total_supply":        encodeRPCQuantityUint64(token.TotalSupply),
		"paused":              token.Paused,
		"freeze_enabled":      token.FreezeEnabled,
		"tax_bps":             token.TaxBPS,
		"authority_signers":   signers,
		"authority_threshold": token.AuthorityThreshold,
		"metadata_uri":        token.MetadataURI,
		"block_number":        encodeRPCQuantityUint64(height),
	}, nil
}

// dtlBalanceOf implements the dtl balance of helper.
func (s *Server) dtlBalanceOf(tokenRef, account string, stateTag json.RawMessage) (uint64, error) {
	if s == nil || s.Node == nil {
		return 0, errors.New("node unavailable")
	}
	// `ledger`, `ok`, and `err` store the error produced by this operation.
	ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("header not found")
	}
	// `tokenID` and `err` store the error produced by this operation.
	tokenID, _, err := resolveDTLTokenFromLedger(ledger, tokenRef)
	if err != nil {
		return 0, err
	}
	ensureDTLState(&ledger)
	return ledger.DTL.BalanceOf(tokenID, account), nil
}

// dtlTotalSupply implements the dtl total supply helper.
func (s *Server) dtlTotalSupply(tokenRef string, stateTag json.RawMessage) (uint64, error) {
	if s == nil || s.Node == nil {
		return 0, errors.New("node unavailable")
	}
	// `ledger`, `ok`, and `err` store the error produced by this operation.
	ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("header not found")
	}
	// `token` and `err` store the error produced by this operation.
	_, token, err := resolveDTLTokenFromLedger(ledger, tokenRef)
	if err != nil {
		return 0, err
	}
	return token.TotalSupply, nil
}

// dtlListTokens implements the dtl list tokens helper.
func (s *Server) dtlListTokens(account string, stateTag json.RawMessage) ([]map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}

	ensureDTLState(&ledger)
	if ledger.DTL == nil || len(ledger.DTL.Tokens) == 0 {
		return []map[string]any{}, nil
	}

	type tokenRow struct {
		// `TokenID` stores the value associated with this record.
		TokenID string
		// `Name` stores the value associated with this record.
		Name string
		// `Symbol` stores the value associated with this record.
		Symbol string
		// `Decimals` stores the value associated with this record.
		Decimals uint8
		// `MaxSupply` stores the value associated with this record.
		MaxSupply uint64
		// `TotalSupply` stores the measured quantity used by this operation.
		TotalSupply uint64
		// `Paused` stores the value associated with this record.
		Paused bool
		// `FreezeEnabled` stores whether the related condition is satisfied.
		FreezeEnabled bool
		// `TaxBPS` stores the value associated with this record.
		TaxBPS uint16
		// `AuthoritySigners` stores the value associated with this record.
		AuthoritySigners []string
		// `AuthorityThreshold` stores the value associated with this record.
		AuthorityThreshold uint16
		// `MetadataURI` stores the current position in the related collection.
		MetadataURI string
		// `Balance` stores the value associated with this record.
		Balance uint64
	}

	// `rows` stores the value produced by this operation.
	rows := make([]tokenRow, 0, len(ledger.DTL.Tokens))
	// `normalizedAccount` stores the measured quantity used by this operation.
	normalizedAccount := normalizeDTLAccount(account)
	// `tokenID` and `token` track the current values while iterating.
	for tokenID, token := range ledger.DTL.Tokens {
		if token == nil {
			continue
		}
		// `signers` stores the value produced by this operation.
		signers := append([]string(nil), token.AuthoritySigners...)
		sort.Strings(signers)
		// `balance` stores the value produced by this operation.
		balance := uint64(0)
		if normalizedAccount != "" {
			balance = ledger.DTL.BalanceOf(tokenID, normalizedAccount)
		}
		rows = append(rows, tokenRow{
			TokenID:            normalizeDTLTokenID(tokenID),
			Name:               token.Name,
			Symbol:             normalizeDTLSymbol(token.Symbol),
			Decimals:           token.Decimals,
			MaxSupply:          token.MaxSupply,
			TotalSupply:        token.TotalSupply,
			Paused:             token.Paused,
			FreezeEnabled:      token.FreezeEnabled,
			TaxBPS:             token.TaxBPS,
			AuthoritySigners:   signers,
			AuthorityThreshold: token.AuthorityThreshold,
			MetadataURI:        token.MetadataURI,
			Balance:            balance,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Symbol == rows[j].Symbol {
			return rows[i].TokenID < rows[j].TokenID
		}
		return rows[i].Symbol < rows[j].Symbol
	})

	// `out` stores the result produced by this operation.
	out := make([]map[string]any, 0, len(rows))
	// `row` tracks the current values while iterating.
	for _, row := range rows {
		out = append(out, map[string]any{
			"token_id":            row.TokenID,
			"name":                row.Name,
			"symbol":              row.Symbol,
			"decimals":            row.Decimals,
			"max_supply":          strconv.FormatUint(row.MaxSupply, 10),
			"total_supply":        strconv.FormatUint(row.TotalSupply, 10),
			"paused":              row.Paused,
			"freeze_enabled":      row.FreezeEnabled,
			"tax_bps":             row.TaxBPS,
			"authority_signers":   row.AuthoritySigners,
			"authority_threshold": row.AuthorityThreshold,
			"metadata_uri":        row.MetadataURI,
			"balance":             strconv.FormatUint(row.Balance, 10),
			"block_number":        encodeRPCQuantityUint64(height),
		})
	}
	return out, nil
}

// sanitizeDTLListWindow implements the sanitize dtl list window helper.
func sanitizeDTLListWindow(offset, limit uint64) (int, int) {
	// `maxInt` defines the constant value used by this package.
	const maxInt = int(^uint(0) >> 1)
	if offset > uint64(maxInt) {
		offset = uint64(maxInt)
	}
	if limit == 0 {
		limit = dtlNFTListLimitDefault
	}
	if limit > dtlNFTListLimitMax {
		limit = dtlNFTListLimitMax
	}
	return int(offset), int(limit)
}

// parseDTLNFT721OwnerKey parses dtlnft721 owner key.
func parseDTLNFT721OwnerKey(raw string) (string, uint64, bool) {
	// `parts` stores the value produced by this operation.
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(parts[0])
	// `tokenID` and `err` store the error produced by this operation.
	tokenID, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if collectionID == "" || err != nil {
		return "", 0, false
	}
	return collectionID, tokenID, true
}

// parseDTLNFT1155BalanceKey parses dtlnft1155 balance key.
func parseDTLNFT1155BalanceKey(raw string) (string, uint64, string, bool) {
	// `parts` stores the value produced by this operation.
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 3)
	if len(parts) != 3 {
		return "", 0, "", false
	}
	// `collectionID` stores the value produced by this operation.
	collectionID := normalizeDTLCollectionID(parts[0])
	// `tokenID` and `err` store the error produced by this operation.
	tokenID, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(parts[2])
	if collectionID == "" || account == "" || err != nil {
		return "", 0, "", false
	}
	return collectionID, tokenID, account, true
}

// dtlListNFT721ByOwner implements the dtl list nft721 by owner helper.
func (s *Server) dtlListNFT721ByOwner(account string, offset, limit uint64, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `normalizedOwner` stores the value produced by this operation.
	normalizedOwner := normalizeDTLAccount(account)
	if normalizedOwner == "" {
		return nil, errors.New("invalid account")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return map[string]any{
			"items":        []map[string]any{},
			"total":        0,
			"next_offset":  0,
			"block_number": encodeRPCQuantityUint64(height),
		}, nil
	}

	// `rows` stores the value produced by this operation.
	rows := make([]dtlNFT721OwnerRow, 0)
	// `ownerKey` and `owner` track the key used to access the related value.
	for ownerKey, owner := range ledger.DTL.NFT721Owners {
		if normalizeDTLAccount(owner) != normalizedOwner {
			continue
		}
		// `collectionID`, `tokenID`, and `ok` store whether the related condition is satisfied.
		collectionID, tokenID, ok := parseDTLNFT721OwnerKey(ownerKey)
		if !ok {
			continue
		}
		// `row` stores the value produced by this operation.
		row := dtlNFT721OwnerRow{
			CollectionID: collectionID,
			TokenID:      tokenID,
			Owner:        normalizedOwner,
			TokenURI:     strings.TrimSpace(ledger.DTL.NFT721TokenURIs[ownerKey]),
		}
		// `collection` stores the value produced by this operation.
		if collection := ledger.DTL.NFT721Collections[collectionID]; collection != nil {
			row.CollectionName = strings.TrimSpace(collection.Name)
			row.CollectionSym = normalizeDTLSymbol(collection.Symbol)
			row.BaseURI = strings.TrimSpace(collection.BaseURI)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CollectionID != rows[j].CollectionID {
			return rows[i].CollectionID < rows[j].CollectionID
		}
		return rows[i].TokenID < rows[j].TokenID
	})

	// `start` and `pageSize` store the measured quantity used by this operation.
	start, pageSize := sanitizeDTLListWindow(offset, limit)
	if start > len(rows) {
		start = len(rows)
	}
	// `end` stores the value produced by this operation.
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}

	// `items` stores the current position in the related collection.
	items := make([]map[string]any, 0, end-start)
	// `row` tracks the current values while iterating.
	for _, row := range rows[start:end] {
		items = append(items, map[string]any{
			"collection_id":     row.CollectionID,
			"collection_name":   row.CollectionName,
			"collection_symbol": row.CollectionSym,
			"token_id":          strconv.FormatUint(row.TokenID, 10),
			"owner":             row.Owner,
			"token_uri":         row.TokenURI,
			"base_uri":          row.BaseURI,
		})
	}

	// `nextOffset` stores the value produced by this operation.
	nextOffset := 0
	if end < len(rows) {
		nextOffset = end
	}
	return map[string]any{
		"items":        items,
		"total":        len(rows),
		"next_offset":  nextOffset,
		"block_number": encodeRPCQuantityUint64(height),
	}, nil
}

// dtlListNFT1155ByOwner implements the dtl list nft1155 by owner helper.
func (s *Server) dtlListNFT1155ByOwner(account string, offset, limit uint64, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `normalizedOwner` stores the value produced by this operation.
	normalizedOwner := normalizeDTLAccount(account)
	if normalizedOwner == "" {
		return nil, errors.New("invalid account")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return map[string]any{
			"items":        []map[string]any{},
			"total":        0,
			"next_offset":  0,
			"block_number": encodeRPCQuantityUint64(height),
		}, nil
	}

	// `rows` stores the value produced by this operation.
	rows := make([]dtlNFT1155OwnerRow, 0)
	// `balanceKey` and `balance` track the key used to access the related value.
	for balanceKey, balance := range ledger.DTL.NFT1155Balances {
		if balance == 0 {
			continue
		}
		// `collectionID`, `tokenID`, `owner`, and `ok` store whether the related condition is satisfied.
		collectionID, tokenID, owner, ok := parseDTLNFT1155BalanceKey(balanceKey)
		if !ok || owner != normalizedOwner {
			continue
		}
		// `row` stores the value produced by this operation.
		row := dtlNFT1155OwnerRow{
			CollectionID: collectionID,
			TokenID:      tokenID,
			Owner:        normalizedOwner,
			Balance:      balance,
		}
		// `collection` stores the value produced by this operation.
		if collection := ledger.DTL.NFT1155Collections[collectionID]; collection != nil {
			row.CollectionName = strings.TrimSpace(collection.Name)
			row.CollectionSym = normalizeDTLSymbol(collection.Symbol)
			row.BaseURI = strings.TrimSpace(collection.BaseURI)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CollectionID != rows[j].CollectionID {
			return rows[i].CollectionID < rows[j].CollectionID
		}
		return rows[i].TokenID < rows[j].TokenID
	})

	// `start` and `pageSize` store the measured quantity used by this operation.
	start, pageSize := sanitizeDTLListWindow(offset, limit)
	if start > len(rows) {
		start = len(rows)
	}
	// `end` stores the value produced by this operation.
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}

	// `items` stores the current position in the related collection.
	items := make([]map[string]any, 0, end-start)
	// `row` tracks the current values while iterating.
	for _, row := range rows[start:end] {
		items = append(items, map[string]any{
			"collection_id":     row.CollectionID,
			"collection_name":   row.CollectionName,
			"collection_symbol": row.CollectionSym,
			"token_id":          strconv.FormatUint(row.TokenID, 10),
			"owner":             row.Owner,
			"balance":           strconv.FormatUint(row.Balance, 10),
			"base_uri":          row.BaseURI,
		})
	}

	// `nextOffset` stores the value produced by this operation.
	nextOffset := 0
	if end < len(rows) {
		nextOffset = end
	}
	return map[string]any{
		"items":        items,
		"total":        len(rows),
		"next_offset":  nextOffset,
		"block_number": encodeRPCQuantityUint64(height),
	}, nil
}

// resolveDTLPoolFromLedger implements the resolve dtl pool from ledger helper.
func resolveDTLPoolFromLedger(ledger Ledger, poolRef string) (string, *DTLPoolState, error) {
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	// `ref` stores the value produced by this operation.
	ref := strings.TrimSpace(poolRef)
	if ref == "" {
		return "", nil, errors.New("missing pool reference")
	}

	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(ref)
	if poolID != "" {
		// `pool` stores the value produced by this operation.
		if pool := ledger.DTL.Pools[poolID]; pool != nil {
			return poolID, pool, nil
		}
	}

	// `left` and `right` store the value used by this operation.
	var left, right string
	if strings.Contains(ref, "/") {
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(ref, "/", 2)
		left = normalizeDTLTokenID(parts[0])
		right = normalizeDTLTokenID(parts[1])
	} else if strings.Contains(ref, "|") {
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(ref, "|", 2)
		left = normalizeDTLTokenID(parts[0])
		right = normalizeDTLTokenID(parts[1])
	}
	if left != "" && right != "" {
		// `mappedID` stores the value produced by this operation.
		if mappedID := normalizeDTLPoolID(ledger.DTL.PoolIndex[dtlPoolPairKey(left, right)]); mappedID != "" {
			// `pool` stores the value produced by this operation.
			if pool := ledger.DTL.Pools[mappedID]; pool != nil {
				return mappedID, pool, nil
			}
		}
	}

	return "", nil, errors.New("dtl pool not found")
}

// dtlPoolInfo implements the dtl pool info helper.
func (s *Server) dtlPoolInfo(poolRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `poolID`, `pool`, and `err` store the error produced by this operation.
	poolID, pool, err := resolveDTLPoolFromLedger(ledger, poolRef)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"pool_id":              poolID,
		"token_a":              normalizeDTLTokenID(pool.TokenA),
		"token_b":              normalizeDTLTokenID(pool.TokenB),
		"reserve_a":            encodeRPCQuantityUint64(pool.ReserveA),
		"reserve_b":            encodeRPCQuantityUint64(pool.ReserveB),
		"total_lp_shares":      encodeRPCQuantityUint64(pool.TotalLPShares),
		"fee_bps":              pool.FeeBPS,
		"protocol_fee_bps":     pool.ProtocolFeeBPS,
		"protocol_fee_account": normalizeDTLAccount(pool.ProtocolFeeAccount),
		"pool_vault_account":   dtlPoolVaultAccount(poolID),
		"router_enabled":       dtlRouterEnabled(),
		"router_max_hops":      dtlRouterMaxHops(),
		"router_max_paths":     dtlRouterQuoteMaxPaths(),
		"block_number":         encodeRPCQuantityUint64(height),
		"max_price_impact_bps": dtlRouterMaxPriceImpactBPS(),
		"deadline_max_blocks":  dtlRouterDeadlineMaxBlocks(),
	}, nil
}

// dtlListPools implements the dtl list pools helper.
func (s *Server) dtlListPools(stateTag json.RawMessage) ([]map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil || len(ledger.DTL.Pools) == 0 {
		return []map[string]any{}, nil
	}

	// `poolIDs` stores the value produced by this operation.
	poolIDs := make([]string, 0, len(ledger.DTL.Pools))
	// `poolID` tracks the current values while iterating.
	for poolID := range ledger.DTL.Pools {
		poolIDs = append(poolIDs, normalizeDTLPoolID(poolID))
	}
	sort.Strings(poolIDs)
	// `out` stores the result produced by this operation.
	out := make([]map[string]any, 0, len(poolIDs))
	// `poolID` tracks the current values while iterating.
	for _, poolID := range poolIDs {
		// `pool` stores the value produced by this operation.
		pool := ledger.DTL.Pools[poolID]
		if pool == nil {
			continue
		}
		out = append(out, map[string]any{
			"pool_id":          poolID,
			"token_a":          normalizeDTLTokenID(pool.TokenA),
			"token_b":          normalizeDTLTokenID(pool.TokenB),
			"reserve_a":        encodeRPCQuantityUint64(pool.ReserveA),
			"reserve_b":        encodeRPCQuantityUint64(pool.ReserveB),
			"total_lp_shares":  encodeRPCQuantityUint64(pool.TotalLPShares),
			"fee_bps":          pool.FeeBPS,
			"protocol_fee_bps": pool.ProtocolFeeBPS,
			"block_number":     encodeRPCQuantityUint64(height),
		})
	}
	return out, nil
}

// dtlFarmInfo implements the dtl farm info helper.
func (s *Server) dtlFarmInfo(farmRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(farmRef)
	if farmID == "" {
		return nil, errors.New("missing farm reference")
	}
	// `farm` stores the value produced by this operation.
	farm := ledger.DTL.FarmPools[farmID]
	if farm == nil {
		return nil, errors.New("dtl farm not found")
	}
	// `positionCount` stores the measured quantity used by this operation.
	positionCount := 0
	// `totalStakedLP` stores the measured quantity used by this operation.
	totalStakedLP := uint64(0)
	// `pos` tracks the current values while iterating.
	for _, pos := range ledger.DTL.FarmPositions {
		if pos == nil || normalizeDTLFarmID(pos.FarmID) != farmID {
			continue
		}
		positionCount++
		if ^uint64(0)-totalStakedLP < pos.StakedLP {
			totalStakedLP = ^uint64(0)
		} else {
			totalStakedLP += pos.StakedLP
		}
	}
	return map[string]any{
		"farm_id":            farmID,
		"pool_id":            normalizeDTLPoolID(farm.PoolID),
		"creator":            normalizeDTLAccount(farm.Creator),
		"multiplier_bps":     farm.MultiplierBPS,
		"created_height":     encodeRPCQuantityUint64(farm.CreatedHeight),
		"last_update_height": encodeRPCQuantityUint64(farm.LastUpdateHeight),
		"active":             farm.Active,
		"total_staked_lp":    encodeRPCQuantityUint64(totalStakedLP),
		"positions":          encodeRPCQuantityInt(positionCount),
		"vault_account":      dtlFarmVaultAccount(farmID),
		"block_number":       encodeRPCQuantityUint64(height),
	}, nil
}

// dtlPositionFarm implements the dtl position farm helper.
func (s *Server) dtlPositionFarm(farmRef, account string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(farmRef)
	if farmID == "" {
		return nil, errors.New("missing farm reference")
	}
	if ledger.DTL.FarmPools[farmID] == nil {
		return nil, errors.New("dtl farm not found")
	}
	account = normalizeDTLAccount(account)
	if account == "" {
		return nil, errors.New("missing account")
	}
	// `pos` stores the value produced by this operation.
	pos := ledger.DTL.FarmPositions[dtlFarmPositionKey(farmID, account)]
	if pos == nil {
		return map[string]any{
			"farm_id":             farmID,
			"account":             account,
			"staked_lp":           encodeRPCQuantityUint64(0),
			"accrued_points":      encodeRPCQuantityUint64(0),
			"last_stake_height":   encodeRPCQuantityUint64(0),
			"last_accrual_height": encodeRPCQuantityUint64(0),
			"block_number":        encodeRPCQuantityUint64(height),
		}, nil
	}
	return map[string]any{
		"farm_id":             farmID,
		"account":             account,
		"staked_lp":           encodeRPCQuantityUint64(pos.StakedLP),
		"accrued_points":      encodeRPCQuantityUint64(pos.AccruedPoints),
		"last_stake_height":   encodeRPCQuantityUint64(pos.LastStakeHeight),
		"last_accrual_height": encodeRPCQuantityUint64(pos.LastAccrualHeight),
		"block_number":        encodeRPCQuantityUint64(height),
	}, nil
}

// dtlSeasonInfo implements the dtl season info helper.
func (s *Server) dtlSeasonInfo(seasonRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(seasonRef)
	if seasonID == "" {
		seasonID, _ = dtlResolveActiveSeason(ledger.DTL, height+1)
	}
	if seasonID == "" {
		return nil, errors.New("dtl season not found")
	}
	// `season` stores the value produced by this operation.
	season := ledger.DTL.Seasons[seasonID]
	if season == nil {
		return nil, errors.New("dtl season not found")
	}
	// `participants` stores the value produced by this operation.
	participants := 0
	// `claims` stores the value produced by this operation.
	claims := 0
	// `prefix` stores the value produced by this operation.
	prefix := seasonID + "|"
	// `key` tracks the key used to access the related value.
	for key := range ledger.DTL.SeasonScores {
		if strings.HasPrefix(strings.TrimSpace(key), prefix) {
			participants++
		}
	}
	// `key` and `claimed` track the key used to access the related value.
	for key, claimed := range ledger.DTL.SeasonClaims {
		if claimed && strings.HasPrefix(strings.TrimSpace(key), prefix) {
			claims++
		}
	}
	return map[string]any{
		"season_id":              seasonID,
		"creator":                normalizeDTLAccount(season.Creator),
		"reward_token":           normalizeDTLTokenID(season.RewardToken),
		"start_height":           encodeRPCQuantityUint64(season.StartHeight),
		"end_height":             encodeRPCQuantityUint64(season.EndHeight),
		"claim_grace_end_height": encodeRPCQuantityUint64(season.ClaimGraceEndHeight),
		"finalized":              season.Finalized,
		"finalized_height":       encodeRPCQuantityUint64(season.FinalizedHeight),
		"total_score":            encodeRPCQuantityUint64(season.TotalScore),
		"total_claimed":          encodeRPCQuantityUint64(season.TotalClaimed),
		"vault_balance":          encodeRPCQuantityUint64(ledger.DTL.SeasonVaults[seasonID]),
		"participants":           encodeRPCQuantityInt(participants),
		"claims":                 encodeRPCQuantityInt(claims),
		"active":                 !season.Finalized && height >= season.StartHeight && height <= season.EndHeight,
		"block_number":           encodeRPCQuantityUint64(height),
	}, nil
}

// dtlLeaderboard implements the dtl leaderboard helper.
func (s *Server) dtlLeaderboard(seasonRef string, limit int, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(seasonRef)
	if seasonID == "" {
		seasonID, _ = dtlResolveActiveSeason(ledger.DTL, height+1)
	}
	if seasonID == "" || ledger.DTL.Seasons[seasonID] == nil {
		return nil, errors.New("dtl season not found")
	}
	if limit < 1 {
		limit = int(DTLDefaultLeaderboardLimit)
	}
	if limit > int(DTLMaxLeaderboardLimit) {
		limit = int(DTLMaxLeaderboardLimit)
	}
	type entry struct {
		// `Account` stores the measured quantity used by this operation.
		Account string
		// `Score` stores the value associated with this record.
		Score uint64
		// `Claimed` stores the value associated with this record.
		Claimed bool
	}
	// `entries` stores the value produced by this operation.
	entries := make([]entry, 0, len(ledger.DTL.SeasonScores))
	// `prefix` stores the value produced by this operation.
	prefix := seasonID + "|"
	// `key` and `score` track the key used to access the related value.
	for key, score := range ledger.DTL.SeasonScores {
		if !strings.HasPrefix(strings.TrimSpace(key), prefix) {
			continue
		}
		// `account` stores the measured quantity used by this operation.
		account := normalizeDTLAccount(strings.TrimPrefix(strings.TrimSpace(key), prefix))
		if account == "" {
			continue
		}
		entries = append(entries, entry{
			Account: account,
			Score:   score,
			Claimed: ledger.DTL.SeasonClaims[key],
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Account < entries[j].Account
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	// `result` stores the result produced by this operation.
	result := make([]map[string]any, 0, len(entries))
	// `i` and `entry` track the current position in the related collection.
	for i, entry := range entries {
		result = append(result, map[string]any{
			"rank":    encodeRPCQuantityInt(i + 1),
			"account": entry.Account,
			"score":   encodeRPCQuantityUint64(entry.Score),
			"claimed": entry.Claimed,
		})
	}
	return map[string]any{
		"season_id":     seasonID,
		"limit":         encodeRPCQuantityInt(limit),
		"total_entries": encodeRPCQuantityInt(len(result)),
		"entries":       result,
		"block_number":  encodeRPCQuantityUint64(height),
	}, nil
}

// dtlRouteQuote implements the dtl route quote helper.
func (s *Server) dtlRouteQuote(tokenIn, tokenOut string, amountIn uint64, maxHops int, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	// `quote` and `err` store the error produced by this operation.
	quote, err := dtlBestPoolSwapRouteQuote(ledger.DTL, tokenIn, tokenOut, amountIn, maxHops)
	if err != nil {
		return nil, err
	}

	// `totalFeeBPS` stores the measured quantity used by this operation.
	totalFeeBPS := uint64(0)
	// `hopFees` stores the value produced by this operation.
	hopFees := make([]map[string]any, 0, len(quote.Hops))
	// `hops` stores the value produced by this operation.
	hops := make([]map[string]any, 0, len(quote.Hops))
	// `i` and `hop` track the current position in the related collection.
	for i, hop := range quote.Hops {
		totalFeeBPS += uint64(hop.FeeBPS)
		hopFees = append(hopFees, map[string]any{
			"hop":     i + 1,
			"pool_id": hop.PoolID,
			"fee_bps": hop.FeeBPS,
		})
		hops = append(hops, map[string]any{
			"hop":        i + 1,
			"pool_id":    hop.PoolID,
			"token_in":   hop.TokenIn,
			"token_out":  hop.TokenOut,
			"amount_in":  encodeRPCQuantityUint64(hop.AmountIn),
			"amount_out": encodeRPCQuantityUint64(hop.AmountOut),
			"fee_bps":    hop.FeeBPS,
		})
	}
	if totalFeeBPS > DTLMaxTaxBPS {
		totalFeeBPS = DTLMaxTaxBPS
	}

	return map[string]any{
		"token_in":            quote.TokenIn,
		"token_out":           quote.TokenOut,
		"amount_in":           encodeRPCQuantityUint64(quote.AmountIn),
		"best_path":           quote.Path,
		"expected_amount_out": encodeRPCQuantityUint64(quote.AmountOut),
		"price_impact_bps":    quote.PriceImpactBPS,
		"hops":                hops,
		"fee_breakdown": map[string]any{
			"total_fee_bps_estimate": totalFeeBPS,
			"hops":                   hopFees,
		},
		"valid_until_height": encodeRPCQuantityUint64(height + dtlRouterDeadlineMaxBlocks()),
		"block_number":       encodeRPCQuantityUint64(height),
	}, nil
}

// dtlMarketInfo implements the dtl market info helper.
func (s *Server) dtlMarketInfo(marketRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `marketID`, `market`, and `err` store the error produced by this operation.
	marketID, market, err := resolveDTLMarketFromLedger(ledger, marketRef)
	if err != nil {
		return nil, err
	}
	// `collateralSymbol` stores the value produced by this operation.
	collateralSymbol := ""
	// `tok` stores whether the related condition is satisfied.
	if tok := ledger.DTL.Tokens[normalizeDTLTokenID(market.CollateralTokenID)]; tok != nil {
		collateralSymbol = normalizeDTLSymbol(tok.Symbol)
	}
	// `debtSymbol` stores the value produced by this operation.
	debtSymbol := ""
	// `tok` stores whether the related condition is satisfied.
	if tok := ledger.DTL.Tokens[normalizeDTLTokenID(market.DebtTokenID)]; tok != nil {
		debtSymbol = normalizeDTLSymbol(tok.Symbol)
	}
	return map[string]any{
		"market_id":             marketID,
		"collateral_token_id":   normalizeDTLTokenID(market.CollateralTokenID),
		"collateral_symbol":     collateralSymbol,
		"debt_token_id":         normalizeDTLTokenID(market.DebtTokenID),
		"debt_symbol":           debtSymbol,
		"collateral_factor_bps": market.CollateralFactorBPS,
		"liquidation_bonus_bps": market.LiquidationBonusBPS,
		"total_collateral":      encodeRPCQuantityUint64(market.TotalCollateral),
		"total_debt":            encodeRPCQuantityUint64(market.TotalDebt),
		"vault_account":         dtlLendingVaultAccount(marketID),
		"block_number":          encodeRPCQuantityUint64(height),
	}, nil
}

// dtlPositionOf implements the dtl position of helper.
func (s *Server) dtlPositionOf(marketRef, account string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `marketID`, `market`, and `err` store the error produced by this operation.
	marketID, market, err := resolveDTLMarketFromLedger(ledger, marketRef)
	if err != nil {
		return nil, err
	}
	// `normalizedAccount` stores the measured quantity used by this operation.
	normalizedAccount := normalizeDTLAccount(account)
	if normalizedAccount == "" {
		return nil, errors.New("invalid account")
	}
	// `key` stores the key used to access the related value.
	key := dtlLendingPositionKey(marketID, normalizedAccount)
	// `position` stores the value produced by this operation.
	position := ledger.DTL.LendingPositions[key]
	// `collateral` stores the value produced by this operation.
	collateral := uint64(0)
	// `debt` stores the value produced by this operation.
	debt := uint64(0)
	if position != nil {
		collateral = position.Collateral
		debt = position.Debt
	}
	// `maxDebt` and `err` store the error produced by this operation.
	maxDebt, err := dtlLendingMaxDebt(collateral, market.CollateralFactorBPS)
	if err != nil {
		return nil, err
	}
	// `isHealthy` and `err` store the error produced by this operation.
	isHealthy, err := dtlLendingIsHealthy(collateral, debt, market.CollateralFactorBPS)
	if err != nil {
		return nil, err
	}
	// `healthBPS` stores the value produced by this operation.
	healthBPS := uint64(0)
	if debt == 0 {
		healthBPS = ^uint64(0)
	} else {
		healthBPS = (maxDebt * DTLMaxTaxBPS) / debt
	}
	return map[string]any{
		"market_id":       marketID,
		"account":         normalizedAccount,
		"collateral":      encodeRPCQuantityUint64(collateral),
		"debt":            encodeRPCQuantityUint64(debt),
		"max_debt":        encodeRPCQuantityUint64(maxDebt),
		"health_bps":      encodeRPCQuantityUint64(healthBPS),
		"is_healthy":      isHealthy,
		"is_liquidatable": debt > 0 && !isHealthy,
		"block_number":    encodeRPCQuantityUint64(height),
	}, nil
}

// dtlTournamentInfo implements the dtl tournament info helper.
func (s *Server) dtlTournamentInfo(tournamentRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `tournamentID`, `tournament`, and `err` store the error produced by this operation.
	tournamentID, tournament, err := resolveDTLTournamentFromLedger(ledger, tournamentRef)
	if err != nil {
		return nil, err
	}
	// `players` stores the value produced by this operation.
	players := append([]string(nil), tournament.Players...)
	// `revealers` stores the value produced by this operation.
	revealers := make([]string, 0, len(tournament.Reveals))
	// `player` and `secret` track the current values while iterating.
	for player, secret := range tournament.Reveals {
		if strings.TrimSpace(secret) != "" {
			revealers = append(revealers, normalizeDTLAccount(player))
		}
	}
	sort.Strings(revealers)
	// `tokenSymbol` stores the value produced by this operation.
	tokenSymbol := ""
	// `tok` stores whether the related condition is satisfied.
	if tok := ledger.DTL.Tokens[normalizeDTLTokenID(tournament.TokenID)]; tok != nil {
		tokenSymbol = normalizeDTLSymbol(tok.Symbol)
	}
	return map[string]any{
		"tournament_id":   tournamentID,
		"token_id":        normalizeDTLTokenID(tournament.TokenID),
		"token_symbol":    tokenSymbol,
		"creator":         normalizeDTLAccount(tournament.Creator),
		"entry_fee":       encodeRPCQuantityUint64(tournament.EntryFee),
		"max_players":     tournament.MaxPlayers,
		"join_deadline":   encodeRPCQuantityUint64(tournament.JoinDeadline),
		"reveal_deadline": encodeRPCQuantityUint64(tournament.RevealDeadline),
		"players":         players,
		"player_count":    encodeRPCQuantityUint64(uint64(len(players))),
		"revealed_count":  encodeRPCQuantityUint64(uint64(len(revealers))),
		"revealers":       revealers,
		"pot":             encodeRPCQuantityUint64(tournament.Pot),
		"settled":         tournament.Settled,
		"winner":          normalizeDTLAccount(tournament.Winner),
		"vault_account":   dtlTournamentVaultAccount(tournamentID),
		"block_number":    encodeRPCQuantityUint64(height),
	}, nil
}

// dtlContractInfo implements the dtl contract info helper.
func (s *Server) dtlContractInfo(contractRef string, stateTag json.RawMessage) (map[string]any, error) {
	if dtlContractRuntimeRemoved() {
		return nil, dtlContractRuntimeRemovedError("dtl_contractInfo")
	}
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `contractID`, `contract`, and `err` store the error produced by this operation.
	contractID, contract, err := resolveDTLContractFromLedger(ledger, contractRef)
	if err != nil {
		return nil, err
	}
	// `methodNames` stores the value produced by this operation.
	methodNames := make([]string, 0, len(contract.Methods))
	// `methods` stores the value produced by this operation.
	methods := make([]map[string]any, 0, len(contract.Methods))
	// `logicABI` stores the current position in the related collection.
	logicABI := make([]map[string]any, 0)
	// `logicStorage` stores the value produced by this operation.
	logicStorage := make([]map[string]any, 0)
	// `logicMethods` stores the value produced by this operation.
	logicMethods := make([]map[string]any, 0)
	// `logicLimits` stores the value produced by this operation.
	logicLimits := map[string]any{}
	// `logicPack` stores the value used by this operation.
	var logicPack any
	if contract.LogicPack != nil {
		logicABI = make([]map[string]any, 0, len(contract.LogicPack.ABI))
		// `abiMethod` tracks the current values while iterating.
		for _, abiMethod := range contract.LogicPack.ABI {
			// `args` stores the value produced by this operation.
			args := make([]map[string]any, 0, len(abiMethod.Args))
			// `arg` tracks the current values while iterating.
			for _, arg := range abiMethod.Args {
				args = append(args, map[string]any{
					"name": strings.ToLower(strings.TrimSpace(arg.Name)),
					"type": strings.ToLower(strings.TrimSpace(arg.Type)),
				})
			}
			logicABI = append(logicABI, map[string]any{
				"name":    strings.ToLower(strings.TrimSpace(abiMethod.Name)),
				"args":    args,
				"returns": append([]string(nil), abiMethod.Returns...),
			})
		}
		sort.Slice(logicABI, func(i, j int) bool {
			// `left` stores the value produced by this operation.
			left, _ := logicABI[i]["name"].(string)
			// `right` stores the value produced by this operation.
			right, _ := logicABI[j]["name"].(string)
			return left < right
		})

		logicStorage = make([]map[string]any, 0, len(contract.LogicPack.Storage))
		// `field` tracks the current values while iterating.
		for _, field := range contract.LogicPack.Storage {
			logicStorage = append(logicStorage, map[string]any{
				"key":  strings.ToLower(strings.TrimSpace(field.Key)),
				"type": strings.ToLower(strings.TrimSpace(field.Type)),
				"init": strings.TrimSpace(field.Init),
			})
		}
		sort.Slice(logicStorage, func(i, j int) bool {
			// `left` stores the value produced by this operation.
			left, _ := logicStorage[i]["key"].(string)
			// `right` stores the value produced by this operation.
			right, _ := logicStorage[j]["key"].(string)
			return left < right
		})

		logicMethods = make([]map[string]any, 0, len(contract.LogicPack.Methods))
		// `method` tracks the current values while iterating.
		for _, method := range contract.LogicPack.Methods {
			// `ops` stores the value produced by this operation.
			ops := make([]map[string]any, 0, len(method.Ops))
			// `op` tracks the current values while iterating.
			for _, op := range method.Ops {
				ops = append(ops, map[string]any{
					"op":         strings.ToUpper(strings.TrimSpace(op.Op)),
					"dest":       strings.ToLower(strings.TrimSpace(op.Dest)),
					"a":          strings.ToLower(strings.TrimSpace(op.A)),
					"b":          strings.ToLower(strings.TrimSpace(op.B)),
					"src":        strings.ToLower(strings.TrimSpace(op.Src)),
					"cond":       strings.ToLower(strings.TrimSpace(op.Cond)),
					"key":        strings.ToLower(strings.TrimSpace(op.Key)),
					"arg":        strings.ToLower(strings.TrimSpace(op.Arg)),
					"token_id":   normalizeDTLTokenID(op.TokenID),
					"to_arg":     strings.ToLower(strings.TrimSpace(op.ToArg)),
					"amount_arg": strings.ToLower(strings.TrimSpace(op.AmountArg)),
					"from":       strings.ToLower(strings.TrimSpace(op.From)),
					"message":    strings.TrimSpace(op.Message),
					"target":     op.Target,
				})
			}
			logicMethods = append(logicMethods, map[string]any{
				"name":      strings.ToLower(strings.TrimSpace(method.Name)),
				"max_steps": encodeRPCQuantityUint64(uint64(method.MaxSteps)),
				"op_count":  encodeRPCQuantityUint64(uint64(len(method.Ops))),
				"ops":       ops,
			})
			methods = append(methods, map[string]any{
				"name":      strings.ToLower(strings.TrimSpace(method.Name)),
				"max_steps": encodeRPCQuantityUint64(uint64(method.MaxSteps)),
				"op_count":  encodeRPCQuantityUint64(uint64(len(method.Ops))),
			})
		}
		sort.Slice(logicMethods, func(i, j int) bool {
			// `left` stores the value produced by this operation.
			left, _ := logicMethods[i]["name"].(string)
			// `right` stores the value produced by this operation.
			right, _ := logicMethods[j]["name"].(string)
			return left < right
		})
		sort.Slice(methods, func(i, j int) bool {
			// `left` stores the value produced by this operation.
			left, _ := methods[i]["name"].(string)
			// `right` stores the value produced by this operation.
			right, _ := methods[j]["name"].(string)
			return left < right
		})
		logicLimits = map[string]any{
			"max_reads":           encodeRPCQuantityUint64(uint64(contract.LogicPack.Limits.MaxReads)),
			"max_writes":          encodeRPCQuantityUint64(uint64(contract.LogicPack.Limits.MaxWrites)),
			"max_token_transfers": encodeRPCQuantityUint64(uint64(contract.LogicPack.Limits.MaxTokenTransfers)),
		}
		logicPack = map[string]any{
			"version": encodeRPCQuantityUint64(uint64(contract.LogicPack.Version)),
			"name":    strings.ToLower(strings.TrimSpace(contract.LogicPack.Name)),
			"abi":     logicABI,
			"storage": logicStorage,
			"methods": logicMethods,
			"limits":  logicLimits,
		}
	} else {
		// `name` tracks the current values while iterating.
		for name := range contract.Methods {
			methodNames = append(methodNames, normalizeDTLContractMethodName(name))
		}
		sort.Strings(methodNames)
		// `name` tracks the current values while iterating.
		for _, name := range methodNames {
			// `method` stores the value produced by this operation.
			method := contract.Methods[name]
			if method == nil {
				continue
			}
			methods = append(methods, map[string]any{
				"name":     normalizeDTLContractMethodName(method.Name),
				"op":       strings.ToUpper(strings.TrimSpace(string(method.Op))),
				"key":      strings.TrimSpace(method.Key),
				"arg":      strings.TrimSpace(method.Arg),
				"to_arg":   strings.TrimSpace(method.ToArg),
				"token_id": normalizeDTLTokenID(method.TokenID),
				"from":     strings.ToLower(strings.TrimSpace(method.From)),
			})
		}
	}
	return map[string]any{
		"contract_id":     contractID,
		"creator":         normalizeDTLAccount(contract.Creator),
		"name":            strings.TrimSpace(contract.Name),
		"lang":            strings.ToLower(strings.TrimSpace(contract.Lang)),
		"version":         contract.Version,
		"logic_mode":      contract.LogicPack != nil,
		"logic_hash":      strings.ToLower(strings.TrimSpace(contract.LogicHash)),
		"logic_pack_hash": strings.ToLower(strings.TrimSpace(contract.LogicHash)),
		"logic_version": func() string {
			if contract.LogicPack == nil {
				return "0x0"
			}
			return encodeRPCQuantityUint64(uint64(contract.LogicPack.Version))
		}(),
		"logic_pack":    logicPack,
		"logic_abi":     logicABI,
		"logic_storage": logicStorage,
		"logic_limits":  logicLimits,
		"logic_methods": logicMethods,
		"paused":        contract.Paused,
		"method_count":  encodeRPCQuantityUint64(uint64(len(methods))),
		"storage_count": encodeRPCQuantityUint64(uint64(len(contract.Storage))),
		"vault_account": dtlContractVaultAccount(contractID),
		"methods":       methods,
		"block_number":  encodeRPCQuantityUint64(height),
	}, nil
}

// dtlContractStorage implements the dtl contract storage helper.
func (s *Server) dtlContractStorage(contractRef, storageKey string, stateTag json.RawMessage) (map[string]any, error) {
	if dtlContractRuntimeRemoved() {
		return nil, dtlContractRuntimeRemovedError("dtl_contractStorage")
	}
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	// `ledger`, `height`, `ok`, and `err` store the error produced by this operation.
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	// `contractID`, `contract`, and `err` store the error produced by this operation.
	contractID, contract, err := resolveDTLContractFromLedger(ledger, contractRef)
	if err != nil {
		return nil, err
	}
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(storageKey)
	if key != "" {
		return map[string]any{
			"contract_id":  contractID,
			"key":          key,
			"value":        strings.TrimSpace(contract.Storage[key]),
			"block_number": encodeRPCQuantityUint64(height),
		}, nil
	}
	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(contract.Storage))
	// `k` tracks the current values while iterating.
	for k := range contract.Storage {
		keys = append(keys, strings.TrimSpace(k))
	}
	sort.Strings(keys)
	// `items` stores the current position in the related collection.
	items := make([]map[string]any, 0, len(keys))
	// `k` tracks the current values while iterating.
	for _, k := range keys {
		items = append(items, map[string]any{
			"key":   k,
			"value": strings.TrimSpace(contract.Storage[k]),
		})
	}
	return map[string]any{
		"contract_id":  contractID,
		"items":        items,
		"block_number": encodeRPCQuantityUint64(height),
	}, nil
}

// submitDTLTransactionObject implements the submit dtl transaction object helper.
func (s *Server) submitDTLTransactionObject(raw json.RawMessage) (string, error) {
	if s == nil || s.Node == nil {
		return "", errors.New("node unavailable")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", errors.New("missing transaction object")
	}
	if containsRemovedVMJSONFields(raw) {
		return "", errors.New("evm/vm removed permanently")
	}

	// `tx` stores the transaction data handled by this operation.
	var tx Transaction
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&tx); err != nil {
		return "", errors.New("invalid transaction object")
	}

	tx.Type = TxDTL
	tx.Coin = normalizeCoin(tx.Coin)
	if strings.TrimSpace(tx.Coin) == "" {
		tx.Coin = CoinSymbol
	}
	if strings.TrimSpace(tx.ChainID) == "" {
		tx.ChainID = protocolChainID()
	}
	if tx.Expiry <= 0 {
		tx.Expiry = time.Now().Add(2 * time.Minute).Unix()
	}
	normalizeIncomingTx(&tx)
	if err := validateRemovedVMEnvelope(tx); err != nil {
		return "", err
	}
	// `err` stores the error produced by this operation.
	if err := validateTransactionShape(tx); err != nil {
		return "", err
	}
	if tx.ID == "" {
		tx.ID = ComputeTxID(tx)
	}

	// `ok` and `reason` store whether the related condition is satisfied.
	if ok, reason := s.Node.ReceiveTransactionWithReason(tx); !ok {
		if reason == "duplicate transaction" {
			return tx.ID, nil
		}
		if reason == "" {
			reason = "transaction rejected"
		}
		return "", errors.New(reason)
	}
	return tx.ID, nil
}
