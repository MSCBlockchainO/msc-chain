package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type dtlDecodedTx struct {
	// `Kind` stores the value associated with this record.
	Kind DTLTxType
	// `Create` stores the value associated with this record.
	Create *DTLCreateTx
	// `Transfer` stores the value associated with this record.
	Transfer *DTLTransferTx
	// `Approve` stores the value associated with this record.
	Approve *DTLApproveTx
	// `TransferFrom` stores the value associated with this record.
	TransferFrom *DTLTransferFromTx
	// `Mint` stores the value associated with this record.
	Mint *DTLMintTx
	// `Burn` stores the value associated with this record.
	Burn *DTLBurnTx
	// `NFT721Create` stores the value associated with this record.
	NFT721Create *DTLNFT721CreateTx
	// `NFT721Mint` stores the value associated with this record.
	NFT721Mint *DTLNFT721MintTx
	// `NFT721Transfer` stores the value associated with this record.
	NFT721Transfer *DTLNFT721TransferTx
	// `NFT1155Create` stores the value associated with this record.
	NFT1155Create *DTLNFT1155CreateTx
	// `NFT1155Mint` stores the value associated with this record.
	NFT1155Mint *DTLNFT1155MintTx
	// `NFT1155Transfer` stores the value associated with this record.
	NFT1155Transfer *DTLNFT1155TransferTx
	// `PoolCreate` stores the value associated with this record.
	PoolCreate *DTLPoolCreateTx
	// `PoolAdd` stores the value associated with this record.
	PoolAdd *DTLPoolAddLiquidityTx
	// `PoolRemove` stores the value associated with this record.
	PoolRemove *DTLPoolRemoveLiquidityTx
	// `PoolSwap` stores the value associated with this record.
	PoolSwap *DTLPoolSwapTx
	// `PoolSwapRoute` stores the value associated with this record.
	PoolSwapRoute *DTLPoolSwapRouteTx
	// `FarmCreate` stores the value associated with this record.
	FarmCreate *DTLFarmCreateTx
	// `FarmStakeLP` stores the value associated with this record.
	FarmStakeLP *DTLFarmStakeLPTx
	// `FarmUnstakeLP` stores the value associated with this record.
	FarmUnstakeLP *DTLFarmUnstakeLPTx
	// `FarmClaim` stores the value associated with this record.
	FarmClaim *DTLFarmClaimTx
	// `DuelCreate` stores the value associated with this record.
	DuelCreate *DTLDuelCreateTx
	// `DuelJoin` stores the value associated with this record.
	DuelJoin *DTLDuelJoinTx
	// `DuelReveal` stores the value associated with this record.
	DuelReveal *DTLDuelRevealTx
	// `DuelFinal` stores the value associated with this record.
	DuelFinal *DTLDuelFinalizeTx
	// `LendMarketCreate` stores the measured quantity used by this operation.
	LendMarketCreate *DTLLendMarketCreateTx
	// `LendDeposit` stores the measured quantity used by this operation.
	LendDeposit *DTLLendDepositCollateralTx
	// `LendBorrow` stores the measured quantity used by this operation.
	LendBorrow *DTLLendBorrowTx
	// `LendRepay` stores the measured quantity used by this operation.
	LendRepay *DTLLendRepayTx
	// `LendWithdraw` stores the measured quantity used by this operation.
	LendWithdraw *DTLLendWithdrawCollateralTx
	// `LendLiquidate` stores the measured quantity used by this operation.
	LendLiquidate *DTLLendLiquidateTx
	// `TournamentCreate` stores the value associated with this record.
	TournamentCreate *DTLTournamentCreateTx
	// `TournamentJoin` stores the value associated with this record.
	TournamentJoin *DTLTournamentJoinTx
	// `TournamentReveal` stores the value associated with this record.
	TournamentReveal *DTLTournamentRevealTx
	// `TournamentFinalize` stores the value associated with this record.
	TournamentFinalize *DTLTournamentFinalizeTx
	// `SeasonCreate` stores the value associated with this record.
	SeasonCreate *DTLSeasonCreateTx
	// `SeasonFinalize` stores the value associated with this record.
	SeasonFinalize *DTLSeasonFinalizeTx
	// `SeasonClaim` stores the value associated with this record.
	SeasonClaim *DTLSeasonClaimTx
	// `OracleFeedCreate` stores the value associated with this record.
	OracleFeedCreate *DTLOracleFeedCreateTx
	// `OraclePriceSubmit` stores the value associated with this record.
	OraclePriceSubmit *DTLOraclePriceSubmitTx
	// `ContractDeploy` stores the value associated with this record.
	ContractDeploy *DTLContractDeployTx
	// `ContractCall` stores the value associated with this record.
	ContractCall *DTLContractCallTx
	// `MintCert` stores the value associated with this record.
	MintCert *DTLGovernanceCert
}

// ensureDTLState implements the ensure dtl state helper.
func ensureDTLState(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.DTL == nil {
		ledger.DTL = NewDTLState()
		return
	}
	ledger.DTL.ensure()
	if !ledger.DTL.canonical {
		ledger.DTL = cloneDTLState(ledger.DTL)
		if ledger.DTL == nil {
			ledger.DTL = NewDTLState()
		}
	}
}

// cloneDTLState clones dtl state.
func cloneDTLState(src *DTLState) *DTLState {
	if src == nil {
		return nil
	}
	src.ensure()
	// `out` stores the result produced by this operation.
	out := NewDTLState()

	// `tokenID` and `token` track the current values while iterating.
	for tokenID, token := range src.Tokens {
		if token == nil {
			continue
		}
		// `signers` stores the value produced by this operation.
		signers := uniqueDTLSigners(token.AuthoritySigners)
		out.Tokens[tokenID] = &DTLTokenState{
			TokenID:            token.TokenID,
			Name:               token.Name,
			Symbol:             token.Symbol,
			Decimals:           token.Decimals,
			MaxSupply:          token.MaxSupply,
			TotalSupply:        token.TotalSupply,
			Paused:             token.Paused,
			FreezeEnabled:      token.FreezeEnabled,
			TaxBPS:             token.TaxBPS,
			AuthoritySigners:   signers,
			AuthorityThreshold: token.AuthorityThreshold,
			MetadataURI:        token.MetadataURI,
		}
	}
	// `symbol` and `tokenID` track the current values while iterating.
	for symbol, tokenID := range src.SymbolIndex {
		out.SymbolIndex[symbol] = tokenID
	}
	// `key` and `bal` track the key used to access the related value.
	for key, bal := range src.Balances {
		out.Balances[key] = bal
	}
	// `key` and `allowance` track the key used to access the related value.
	for key, allowance := range src.Allowances {
		out.Allowances[key] = allowance
	}
	// `collectionID` and `collection` track the current values while iterating.
	for collectionID, collection := range src.NFT721Collections {
		if collection == nil {
			continue
		}
		out.NFT721Collections[collectionID] = &DTLNFT721CollectionState{
			CollectionID: collection.CollectionID,
			Creator:      collection.Creator,
			Name:         collection.Name,
			Symbol:       collection.Symbol,
			BaseURI:      collection.BaseURI,
			NextTokenID:  collection.NextTokenID,
			TotalMinted:  collection.TotalMinted,
			Paused:       collection.Paused,
		}
	}
	// `symbol` and `collectionID` track the current values while iterating.
	for symbol, collectionID := range src.NFT721SymbolIndex {
		out.NFT721SymbolIndex[symbol] = collectionID
	}
	// `key` and `owner` track the key used to access the related value.
	for key, owner := range src.NFT721Owners {
		out.NFT721Owners[key] = owner
	}
	// `key` and `tokenURI` track the key used to access the related value.
	for key, tokenURI := range src.NFT721TokenURIs {
		out.NFT721TokenURIs[key] = tokenURI
	}
	// `collectionID` and `collection` track the current values while iterating.
	for collectionID, collection := range src.NFT1155Collections {
		if collection == nil {
			continue
		}
		out.NFT1155Collections[collectionID] = &DTLNFT1155CollectionState{
			CollectionID: collection.CollectionID,
			Creator:      collection.Creator,
			Name:         collection.Name,
			Symbol:       collection.Symbol,
			BaseURI:      collection.BaseURI,
			Paused:       collection.Paused,
		}
	}
	// `symbol` and `collectionID` track the current values while iterating.
	for symbol, collectionID := range src.NFT1155SymbolIndex {
		out.NFT1155SymbolIndex[symbol] = collectionID
	}
	// `key` and `bal` track the key used to access the related value.
	for key, bal := range src.NFT1155Balances {
		out.NFT1155Balances[key] = bal
	}
	// `key` and `total` track the key used to access the related value.
	for key, total := range src.NFT1155Supplies {
		out.NFT1155Supplies[key] = total
	}
	// `poolID` and `pool` track the current values while iterating.
	for poolID, pool := range src.Pools {
		if pool == nil {
			continue
		}
		out.Pools[poolID] = &DTLPoolState{
			PoolID:             pool.PoolID,
			TokenA:             pool.TokenA,
			TokenB:             pool.TokenB,
			ReserveA:           pool.ReserveA,
			ReserveB:           pool.ReserveB,
			TotalLPShares:      pool.TotalLPShares,
			FeeBPS:             pool.FeeBPS,
			ProtocolFeeBPS:     pool.ProtocolFeeBPS,
			ProtocolFeeAccount: normalizeDTLAccount(pool.ProtocolFeeAccount),
			PriceCumulativeA:   pool.PriceCumulativeA,
			PriceCumulativeB:   pool.PriceCumulativeB,
			LastTwapHeight:     pool.LastTwapHeight,
		}
	}
	// `key` and `poolID` track the key used to access the related value.
	for key, poolID := range src.PoolIndex {
		out.PoolIndex[key] = poolID
	}
	// `key` and `bal` track the key used to access the related value.
	for key, bal := range src.LPBalances {
		out.LPBalances[key] = bal
	}
	// `duelID` and `duel` track the current values while iterating.
	for duelID, duel := range src.Duels {
		if duel == nil {
			continue
		}
		out.Duels[duelID] = &DTLDuelState{
			DuelID:           duel.DuelID,
			TokenID:          duel.TokenID,
			Stake:            duel.Stake,
			PlayerA:          duel.PlayerA,
			PlayerB:          duel.PlayerB,
			CommitA:          duel.CommitA,
			CommitB:          duel.CommitB,
			RevealA:          duel.RevealA,
			RevealB:          duel.RevealB,
			JoinDeadline:     duel.JoinDeadline,
			RevealDeadline:   duel.RevealDeadline,
			Settled:          duel.Settled,
			Winner:           duel.Winner,
			BeaconHeight:     duel.BeaconHeight,
			BeaconHash:       strings.TrimSpace(duel.BeaconHash),
			FinalizationSeed: strings.TrimSpace(duel.FinalizationSeed),
		}
	}
	// `marketID` and `market` track the current values while iterating.
	for marketID, market := range src.LendingMarkets {
		if market == nil {
			continue
		}
		out.LendingMarkets[marketID] = &DTLLendingMarketState{
			MarketID:            market.MarketID,
			CollateralTokenID:   market.CollateralTokenID,
			DebtTokenID:         market.DebtTokenID,
			CollateralFactorBPS: market.CollateralFactorBPS,
			LiquidationBonusBPS: market.LiquidationBonusBPS,
			TotalCollateral:     market.TotalCollateral,
			TotalDebt:           market.TotalDebt,
			CollateralFeedID:    normalizeDTLTokenID(market.CollateralFeedID),
			DebtFeedID:          normalizeDTLTokenID(market.DebtFeedID),
			ReserveFactorBPS:    market.ReserveFactorBPS,
			BaseBorrowRateBPS:   market.BaseBorrowRateBPS,
			SlopeBorrowRateBPS:  market.SlopeBorrowRateBPS,
			CloseFactorBPS:      market.CloseFactorBPS,
			BorrowIndex:         market.BorrowIndex,
			LastAccrualHeight:   market.LastAccrualHeight,
		}
	}
	// `key` and `marketID` track the key used to access the related value.
	for key, marketID := range src.LendingIndex {
		out.LendingIndex[key] = marketID
	}
	// `key` and `position` track the key used to access the related value.
	for key, position := range src.LendingPositions {
		if position == nil {
			continue
		}
		out.LendingPositions[key] = &DTLLendingPositionState{
			MarketID:   position.MarketID,
			Account:    position.Account,
			Collateral: position.Collateral,
			Debt:       position.Debt,
			ScaledDebt: position.ScaledDebt,
		}
	}
	// `tournamentID` and `tournament` track the current values while iterating.
	for tournamentID, tournament := range src.Tournaments {
		if tournament == nil {
			continue
		}
		// `players` stores the value produced by this operation.
		players := append([]string(nil), tournament.Players...)
		// `commits` stores the value produced by this operation.
		commits := make(map[string]string, len(tournament.Commits))
		// `k` and `v` track the current values while iterating.
		for k, v := range tournament.Commits {
			commits[k] = v
		}
		// `reveals` stores the value produced by this operation.
		reveals := make(map[string]string, len(tournament.Reveals))
		// `k` and `v` track the current values while iterating.
		for k, v := range tournament.Reveals {
			reveals[k] = v
		}
		out.Tournaments[tournamentID] = &DTLTournamentState{
			TournamentID:     tournament.TournamentID,
			TokenID:          tournament.TokenID,
			Creator:          tournament.Creator,
			EntryFee:         tournament.EntryFee,
			MaxPlayers:       tournament.MaxPlayers,
			JoinDeadline:     tournament.JoinDeadline,
			RevealDeadline:   tournament.RevealDeadline,
			Players:          players,
			Commits:          commits,
			Reveals:          reveals,
			Pot:              tournament.Pot,
			Settled:          tournament.Settled,
			Winner:           tournament.Winner,
			BeaconHeight:     tournament.BeaconHeight,
			BeaconHash:       strings.TrimSpace(tournament.BeaconHash),
			FinalizationSeed: strings.TrimSpace(tournament.FinalizationSeed),
		}
	}
	// `farmID` and `farm` track the current values while iterating.
	for farmID, farm := range src.FarmPools {
		if farm == nil {
			continue
		}
		out.FarmPools[farmID] = &DTLFarmPoolState{
			FarmID:           normalizeDTLFarmID(farm.FarmID),
			PoolID:           normalizeDTLPoolID(farm.PoolID),
			Creator:          normalizeDTLAccount(farm.Creator),
			MultiplierBPS:    farm.MultiplierBPS,
			CreatedHeight:    farm.CreatedHeight,
			LastUpdateHeight: farm.LastUpdateHeight,
			Active:           farm.Active,
		}
	}
	// `key` and `pos` track the key used to access the related value.
	for key, pos := range src.FarmPositions {
		if pos == nil {
			continue
		}
		out.FarmPositions[key] = &DTLFarmPositionState{
			FarmID:            normalizeDTLFarmID(pos.FarmID),
			Account:           normalizeDTLAccount(pos.Account),
			StakedLP:          pos.StakedLP,
			LastStakeHeight:   pos.LastStakeHeight,
			LastAccrualHeight: pos.LastAccrualHeight,
			AccruedPoints:     pos.AccruedPoints,
		}
	}
	// `seasonID` and `season` track the current values while iterating.
	for seasonID, season := range src.Seasons {
		if season == nil {
			continue
		}
		out.Seasons[seasonID] = &DTLSeasonState{
			SeasonID:            normalizeDTLSeasonID(season.SeasonID),
			Creator:             normalizeDTLAccount(season.Creator),
			RewardToken:         normalizeDTLTokenID(season.RewardToken),
			StartHeight:         season.StartHeight,
			EndHeight:           season.EndHeight,
			ClaimGraceEndHeight: season.ClaimGraceEndHeight,
			Finalized:           season.Finalized,
			FinalizedHeight:     season.FinalizedHeight,
			TotalScore:          season.TotalScore,
			TotalClaimed:        season.TotalClaimed,
		}
	}
	// `key` and `score` track the key used to access the related value.
	for key, score := range src.SeasonScores {
		out.SeasonScores[key] = score
	}
	// `key` and `claimed` track the key used to access the related value.
	for key, claimed := range src.SeasonClaims {
		out.SeasonClaims[key] = claimed
	}
	// `seasonID` and `amount` track the current values while iterating.
	for seasonID, amount := range src.SeasonVaults {
		out.SeasonVaults[seasonID] = amount
	}
	// `feedID` and `feed` track the current values while iterating.
	for feedID, feed := range src.OracleFeeds {
		if feed == nil {
			continue
		}
		out.OracleFeeds[feedID] = &DTLOracleFeedState{
			FeedID:           normalizeDTLTokenID(feed.FeedID),
			BaseTokenID:      normalizeDTLTokenID(feed.BaseTokenID),
			QuoteTokenID:     normalizeDTLTokenID(feed.QuoteTokenID),
			Signers:          uniqueDTLSigners(feed.Signers),
			Threshold:        feed.Threshold,
			Decimals:         feed.Decimals,
			LastMedianPrice:  feed.LastMedianPrice,
			LastUpdateHeight: feed.LastUpdateHeight,
		}
	}
	// `feedID` and `bySigner` track the current values while iterating.
	for feedID, bySigner := range src.OracleSamples {
		if bySigner == nil {
			continue
		}
		// `dst` stores the value produced by this operation.
		dst := make(map[string]DTLOracleSampleState, len(bySigner))
		// `signer` and `sample` track the current values while iterating.
		for signer, sample := range bySigner {
			dst[signer] = DTLOracleSampleState{
				FeedID: normalizeDTLTokenID(sample.FeedID),
				Signer: normalizeDTLAccount(sample.Signer),
				Price:  sample.Price,
				Height: sample.Height,
			}
		}
		out.OracleSamples[normalizeDTLTokenID(feedID)] = dst
	}
	// `contractID` and `contract` track the current values while iterating.
	for contractID, contract := range src.Contracts {
		if contract == nil {
			continue
		}
		// `methods` stores the value produced by this operation.
		methods := make(map[string]*DTLContractMethodState, len(contract.Methods))
		// `name` and `method` track the current values while iterating.
		for name, method := range contract.Methods {
			if method == nil {
				continue
			}
			methods[name] = &DTLContractMethodState{
				Name:    method.Name,
				Op:      method.Op,
				Key:     method.Key,
				Arg:     method.Arg,
				ToArg:   method.ToArg,
				TokenID: method.TokenID,
				From:    method.From,
			}
		}
		// `storage` stores the value produced by this operation.
		storage := make(map[string]string, len(contract.Storage))
		// `key` and `value` track the key used to access the related value.
		for key, value := range contract.Storage {
			storage[key] = value
		}
		out.Contracts[contractID] = &DTLContractState{
			ContractID:      contract.ContractID,
			Creator:         contract.Creator,
			Name:            contract.Name,
			Lang:            contract.Lang,
			Version:         contract.Version,
			Methods:         methods,
			Storage:         storage,
			LogicPack:       cloneDTLLogicPack(contract.LogicPack),
			LogicHash:       strings.ToLower(strings.TrimSpace(contract.LogicHash)),
			Paused:          contract.Paused,
			Standard:        strings.ToUpper(strings.TrimSpace(contract.Standard)),
			ABI:             canonicalDTLRawJSONMessageForState(contract.ABI),
			MetadataURI:     strings.TrimSpace(contract.MetadataURI),
			Interfaces:      canonicalDTLInterfaces(contract.Interfaces),
			Upgradeable:     contract.Upgradeable,
			ProxyTarget:     normalizeDTLContractID(contract.ProxyTarget),
			Bytecode:        strings.ToLower(strings.TrimSpace(contract.Bytecode)),
			BytecodeFormat:  normalizeDTLBytecodeFormat(contract.BytecodeFormat),
			BytecodeHash:    strings.ToLower(strings.TrimSpace(contract.BytecodeHash)),
			Compiler:        strings.TrimSpace(contract.Compiler),
			SourceHash:      strings.TrimSpace(contract.SourceHash),
			BytecodeVersion: contract.BytecodeVersion,
		}
	}
	// `tokenID` and `byAccount` track the measured quantity used by this operation.
	for tokenID, byAccount := range src.FrozenAccounts {
		if byAccount == nil {
			continue
		}
		// `dstByAccount` stores the measured quantity used by this operation.
		dstByAccount := make(map[string]bool, len(byAccount))
		// `account` and `frozen` track the measured quantity used by this operation.
		for account, frozen := range byAccount {
			dstByAccount[account] = frozen
		}
		out.FrozenAccounts[tokenID] = dstByAccount
	}
	// `key` and `epoch` track the key used to access the related value.
	for key, epoch := range src.GovernanceReplay {
		out.GovernanceReplay[key] = epoch
	}
	if len(src.Events) > 0 {
		out.Events = append([]string(nil), src.Events...)
	}
	if len(src.EventLogs) > 0 {
		out.EventLogs = make([]DTLEventLog, 0, len(src.EventLogs))
		// `logEntry` tracks the current values while iterating.
		for _, logEntry := range src.EventLogs {
			// `copied` stores the value produced by this operation.
			copied := DTLEventLog{
				ContractID:  normalizeDTLContractID(logEntry.ContractID),
				Topics:      append([]string(nil), logEntry.Topics...),
				Data:        canonicalDTLRawJSONStringForState(logEntry.Data),
				BlockHeight: logEntry.BlockHeight,
				TxID:        strings.ToLower(strings.TrimSpace(logEntry.TxID)),
				TxIndex:     logEntry.TxIndex,
				LogIndex:    logEntry.LogIndex,
			}
			out.EventLogs = append(out.EventLogs, copied)
		}
	}
	out.canonical = false
	canonicalizeDTLState(out)
	return out
}

// canonicalDTLMap returns canonical dtl map.
func canonicalDTLMap[V any](src map[string]V, normalize func(string) string) ([]string, map[string]V) {
	if len(src) == 0 {
		return nil, nil
	}
	// `rawKeys` stores the value produced by this operation.
	rawKeys := make([]string, 0, len(src))
	// `rawKey` tracks the key used to access the related value.
	for rawKey := range src {
		rawKeys = append(rawKeys, rawKey)
	}
	sort.Strings(rawKeys)

	// `canonical` stores the value produced by this operation.
	canonical := make(map[string]V, len(src))
	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(src))
	// `rawKey` tracks the key used to access the related value.
	for _, rawKey := range rawKeys {
		// `key` stores the key used to access the related value.
		key := normalize(rawKey)
		if key == "" {
			continue
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := canonical[key]; exists {
			continue
		}
		canonical[key] = src[rawKey]
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, canonical
}

// trimDTLHashKey implements the trim dtl hash key helper.
func trimDTLHashKey(key string) string {
	return strings.TrimSpace(key)
}

// normalizeDTLUintHashKeyPart normalizes dtl uint hash key part.
func normalizeDTLUintHashKeyPart(raw string) string {
	// `s` stores the value produced by this operation.
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// `n` and `err` store the error produced by this operation.
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return s
	}
	return strconv.FormatUint(n, 10)
}

// canonicalDTLCompositeHashKey returns canonical dtl composite hash key.
func canonicalDTLCompositeHashKey(raw string, normalizers ...func(string) string) string {
	// `parts` stores the value produced by this operation.
	parts := strings.Split(strings.TrimSpace(raw), "|")
	if len(parts) != len(normalizers) {
		return strings.TrimSpace(raw)
	}
	// `i` and `normalize` track the current position in the related collection.
	for i, normalize := range normalizers {
		parts[i] = normalize(parts[i])
	}
	return strings.Join(parts, "|")
}

// normalizeDTLPoolPairHashKey normalizes dtl pool pair hash key.
func normalizeDTLPoolPairHashKey(raw string) string {
	// `parts` stores the value produced by this operation.
	parts := strings.Split(strings.TrimSpace(raw), "|")
	if len(parts) != 2 {
		return strings.TrimSpace(raw)
	}
	return dtlPoolPairKey(parts[0], parts[1])
}

// normalizeDTLGovernanceReplayHashKey normalizes dtl governance replay hash key.
func normalizeDTLGovernanceReplayHashKey(raw string) string {
	// `parts` stores the value produced by this operation.
	parts := strings.Split(strings.TrimSpace(raw), "|")
	if len(parts) >= 3 && strings.EqualFold(parts[1], "v2") {
		switch len(parts) {
		case 3:
			return strings.Join([]string{
				normalizeDTLTokenID(parts[0]),
				"v2",
				strings.ToLower(strings.TrimSpace(parts[2])),
			}, "|")
		case 4:
			return strings.Join([]string{
				normalizeDTLTokenID(parts[0]),
				"v2",
				strings.ToLower(strings.TrimSpace(parts[2])),
				strings.ToLower(strings.TrimSpace(parts[3])),
			}, "|")
		}
	}
	switch len(parts) {
	case 3:
		return strings.Join([]string{
			normalizeDTLTokenID(parts[0]),
			strings.ToUpper(strings.TrimSpace(parts[1])),
			strings.ToLower(strings.TrimSpace(parts[2])),
		}, "|")
	case 4:
		return strings.Join([]string{
			normalizeDTLTokenID(parts[0]),
			strings.TrimSpace(parts[1]),
			strings.ToUpper(strings.TrimSpace(parts[2])),
			strings.ToLower(strings.TrimSpace(parts[3])),
		}, "|")
	default:
		return strings.TrimSpace(raw)
	}
}

// canonicalDTLValueMap returns canonical dtl value map.
func canonicalDTLValueMap[V any](src map[string]V, normalize func(string) string) map[string]V {
	// `out` stores the result produced by this operation.
	out := make(map[string]V, len(src))
	// `keys` and `canonical` store the key used to access the related value.
	keys, canonical := canonicalDTLMap(src, normalize)
	// `key` tracks the key used to access the related value.
	for _, key := range keys {
		out[key] = canonical[key]
	}
	return out
}

// canonicalDTLRawJSONMessage returns compact deterministic JSON bytes for
// persisted DTL JSON blobs. It preserves array order and JSON values, while
// removing whitespace and relying on encoding/json's stable object-key order.
func canonicalDTLRawJSONMessage(raw json.RawMessage) (json.RawMessage, error) {
	// `trimmed` stores the value produced by this operation.
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	// `decoded` stores the value produced by this operation.
	var decoded any
	// `dec` stores the current position in the related collection.
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return json.RawMessage(trimmed), err
	}
	// `extra` stores the value produced by this operation.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return json.RawMessage(trimmed), fmt.Errorf("dtl: trailing JSON value")
		}
		return json.RawMessage(trimmed), err
	}
	// `canonical` and `err` store the error produced by this operation.
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return json.RawMessage(trimmed), err
	}
	return append(json.RawMessage(nil), canonical...), nil
}

// canonicalDTLRawJSONMessageForState returns canonical JSON for valid persisted
// DTL state and trimmed raw material for invalid/corrupt state so hash checks do
// not hide malformed data.
func canonicalDTLRawJSONMessageForState(raw json.RawMessage) json.RawMessage {
	// `canonical` and `err` store the error produced by this operation.
	canonical, err := canonicalDTLRawJSONMessage(raw)
	if err == nil {
		return canonical
	}
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

// canonicalDTLRawJSONMessageForHash implements the canonical dtl raw json
// message for hash helper.
func canonicalDTLRawJSONMessageForHash(raw json.RawMessage) string {
	return string(canonicalDTLRawJSONMessageForState(raw))
}

func canonicalDTLRawJSONStringForState(raw string) string {
	return string(canonicalDTLRawJSONMessageForState(json.RawMessage(raw)))
}

func canonicalDTLRawJSONStringForHash(raw string) string {
	return string(canonicalDTLRawJSONMessageForState(json.RawMessage(raw)))
}

// appendDTLCanonicalDuplicateKeyMaterial implements the append dtl canonical duplicate key material helper.
func appendDTLCanonicalDuplicateKeyMaterial[V any](b *strings.Builder, label string, src map[string]V, normalize func(string) string) {
	if b == nil || len(src) < 2 {
		return
	}
	// `buckets` stores the value produced by this operation.
	buckets := make(map[string][]string, len(src))
	// `rawKey` tracks the key used to access the related value.
	for rawKey := range src {
		// `key` stores the key used to access the related value.
		key := normalize(rawKey)
		if key == "" {
			continue
		}
		buckets[key] = append(buckets[key], rawKey)
	}
	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(buckets))
	// `key` and `rawKeys` track the key used to access the related value.
	for key, rawKeys := range buckets {
		if len(rawKeys) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	// `key` tracks the key used to access the related value.
	for _, key := range keys {
		// `rawKeys` stores the value produced by this operation.
		rawKeys := append([]string(nil), buckets[key]...)
		sort.Strings(rawKeys)
		b.WriteString("dtl_duplicate_key|")
		b.WriteString(label)
		b.WriteString("|")
		b.WriteString(key)
		b.WriteString("=")
		// `i` and `rawKey` track the key used to access the related value.
		for i, rawKey := range rawKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(strconv.Quote(rawKey))
		}
		b.WriteString(";")
	}
}

// appendDTLNestedCanonicalDuplicateMarkers emits nested duplicate markers in a
// stable raw-key order so ambiguous nested maps cannot make state hashing depend
// on Go map iteration order.
func appendDTLNestedCanonicalDuplicateMarkers[V any](src map[string]V, normalize func(string) string, emit func(string, V)) {
	if len(src) == 0 || emit == nil {
		return
	}
	rawKeys := make([]string, 0, len(src))
	for rawKey := range src {
		rawKeys = append(rawKeys, rawKey)
	}
	sort.Strings(rawKeys)
	for _, rawKey := range rawKeys {
		key := normalize(rawKey)
		if key == "" {
			continue
		}
		emit(key, src[rawKey])
	}
}

// appendDTLStateCanonicalDuplicateMarkers implements the append dtl state canonical duplicate markers helper.
func appendDTLStateCanonicalDuplicateMarkers(b *strings.Builder, state *DTLState) {
	if b == nil || state == nil {
		return
	}
	appendDTLCanonicalDuplicateKeyMaterial(b, "tokens", state.Tokens, normalizeDTLTokenID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "symbol_index", state.SymbolIndex, normalizeDTLSymbol)
	appendDTLCanonicalDuplicateKeyMaterial(b, "balances", state.Balances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "allowances", state.Allowances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLAccount, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft721_collections", state.NFT721Collections, normalizeDTLCollectionID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft721_symbol_index", state.NFT721SymbolIndex, normalizeDTLSymbol)
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft721_owners", state.NFT721Owners, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft721_token_uris", state.NFT721TokenURIs, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft1155_collections", state.NFT1155Collections, normalizeDTLCollectionID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft1155_symbol_index", state.NFT1155SymbolIndex, normalizeDTLSymbol)
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft1155_balances", state.NFT1155Balances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "nft1155_supplies", state.NFT1155Supplies, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "pools", state.Pools, normalizeDTLPoolID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "pool_index", state.PoolIndex, normalizeDTLPoolPairHashKey)
	appendDTLCanonicalDuplicateKeyMaterial(b, "lp_balances", state.LPBalances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLPoolID, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "duels", state.Duels, normalizeDTLTokenID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "lending_markets", state.LendingMarkets, normalizeDTLMarketID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "lending_index", state.LendingIndex, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLTokenID)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "lending_positions", state.LendingPositions, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLMarketID, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "tournaments", state.Tournaments, normalizeDTLTournamentID)
	appendDTLNestedCanonicalDuplicateMarkers(state.Tournaments, normalizeDTLTournamentID, func(tournamentID string, tournament *DTLTournamentState) {
		if tournament == nil {
			return
		}
		// `labelPrefix` stores the value produced by this operation.
		labelPrefix := "tournament:" + tournamentID
		appendDTLCanonicalDuplicateKeyMaterial(b, labelPrefix+":commits", tournament.Commits, normalizeDTLAccount)
		appendDTLCanonicalDuplicateKeyMaterial(b, labelPrefix+":reveals", tournament.Reveals, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "farm_pools", state.FarmPools, normalizeDTLFarmID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "farm_positions", state.FarmPositions, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLFarmID, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "seasons", state.Seasons, normalizeDTLSeasonID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "season_scores", state.SeasonScores, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLSeasonID, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "season_claims", state.SeasonClaims, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLSeasonID, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "season_vaults", state.SeasonVaults, normalizeDTLSeasonID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "oracle_feeds", state.OracleFeeds, normalizeDTLTokenID)
	appendDTLCanonicalDuplicateKeyMaterial(b, "oracle_samples", state.OracleSamples, normalizeDTLTokenID)
	appendDTLNestedCanonicalDuplicateMarkers(state.OracleSamples, normalizeDTLTokenID, func(feedID string, bySigner map[string]DTLOracleSampleState) {
		appendDTLCanonicalDuplicateKeyMaterial(b, "oracle_sample:"+feedID, bySigner, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "contracts", state.Contracts, normalizeDTLContractID)
	appendDTLNestedCanonicalDuplicateMarkers(state.Contracts, normalizeDTLContractID, func(contractID string, contract *DTLContractState) {
		if contract == nil {
			return
		}
		// `labelPrefix` stores the value produced by this operation.
		labelPrefix := "contract:" + contractID
		appendDTLCanonicalDuplicateKeyMaterial(b, labelPrefix+":methods", contract.Methods, normalizeDTLContractMethodName)
		appendDTLCanonicalDuplicateKeyMaterial(b, labelPrefix+":storage", contract.Storage, trimDTLHashKey)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "frozen_accounts", state.FrozenAccounts, normalizeDTLTokenID)
	appendDTLNestedCanonicalDuplicateMarkers(state.FrozenAccounts, normalizeDTLTokenID, func(tokenID string, byAccount map[string]bool) {
		appendDTLCanonicalDuplicateKeyMaterial(b, "frozen_account:"+tokenID, byAccount, normalizeDTLAccount)
	})
	appendDTLCanonicalDuplicateKeyMaterial(b, "governance_replay", state.GovernanceReplay, normalizeDTLGovernanceReplayHashKey)
}

// canonicalizeDTLState returns canonical ize dtl state.
func canonicalizeDTLState(state *DTLState) {
	if state == nil {
		return
	}
	state.ensure()

	state.Tokens = canonicalDTLValueMap(state.Tokens, normalizeDTLTokenID)
	// `tokenID` and `token` track the current values while iterating.
	for tokenID, token := range state.Tokens {
		if token == nil {
			delete(state.Tokens, tokenID)
			continue
		}
		token.TokenID = tokenID
		token.Symbol = normalizeDTLSymbol(token.Symbol)
		token.AuthoritySigners = uniqueDTLSigners(token.AuthoritySigners)
		token.MetadataURI = strings.TrimSpace(token.MetadataURI)
	}
	state.SymbolIndex = canonicalDTLValueMap(state.SymbolIndex, normalizeDTLSymbol)
	// `symbol` and `tokenID` track the current values while iterating.
	for symbol, tokenID := range state.SymbolIndex {
		state.SymbolIndex[symbol] = normalizeDTLTokenID(tokenID)
	}
	state.Balances = canonicalDTLValueMap(state.Balances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLAccount)
	})
	state.Allowances = canonicalDTLValueMap(state.Allowances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLAccount, normalizeDTLAccount)
	})

	state.NFT721Collections = canonicalDTLValueMap(state.NFT721Collections, normalizeDTLCollectionID)
	// `collectionID` and `collection` track the current values while iterating.
	for collectionID, collection := range state.NFT721Collections {
		if collection == nil {
			delete(state.NFT721Collections, collectionID)
			continue
		}
		collection.CollectionID = collectionID
		collection.Creator = normalizeDTLAccount(collection.Creator)
		collection.Symbol = normalizeDTLSymbol(collection.Symbol)
		collection.BaseURI = strings.TrimSpace(collection.BaseURI)
	}
	state.NFT721SymbolIndex = canonicalDTLValueMap(state.NFT721SymbolIndex, normalizeDTLSymbol)
	// `symbol` and `collectionID` track the current values while iterating.
	for symbol, collectionID := range state.NFT721SymbolIndex {
		state.NFT721SymbolIndex[symbol] = normalizeDTLCollectionID(collectionID)
	}
	state.NFT721Owners = canonicalDTLValueMap(state.NFT721Owners, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	// `key` and `owner` track the key used to access the related value.
	for key, owner := range state.NFT721Owners {
		state.NFT721Owners[key] = normalizeDTLAccount(owner)
	}
	state.NFT721TokenURIs = canonicalDTLValueMap(state.NFT721TokenURIs, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	// `key` and `uri` track the key used to access the related value.
	for key, uri := range state.NFT721TokenURIs {
		state.NFT721TokenURIs[key] = strings.TrimSpace(uri)
	}

	state.NFT1155Collections = canonicalDTLValueMap(state.NFT1155Collections, normalizeDTLCollectionID)
	// `collectionID` and `collection` track the current values while iterating.
	for collectionID, collection := range state.NFT1155Collections {
		if collection == nil {
			delete(state.NFT1155Collections, collectionID)
			continue
		}
		collection.CollectionID = collectionID
		collection.Creator = normalizeDTLAccount(collection.Creator)
		collection.Symbol = normalizeDTLSymbol(collection.Symbol)
		collection.BaseURI = strings.TrimSpace(collection.BaseURI)
	}
	state.NFT1155SymbolIndex = canonicalDTLValueMap(state.NFT1155SymbolIndex, normalizeDTLSymbol)
	// `symbol` and `collectionID` track the current values while iterating.
	for symbol, collectionID := range state.NFT1155SymbolIndex {
		state.NFT1155SymbolIndex[symbol] = normalizeDTLCollectionID(collectionID)
	}
	state.NFT1155Balances = canonicalDTLValueMap(state.NFT1155Balances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart, normalizeDTLAccount)
	})
	state.NFT1155Supplies = canonicalDTLValueMap(state.NFT1155Supplies, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})

	state.Pools = canonicalDTLValueMap(state.Pools, normalizeDTLPoolID)
	// `poolID` and `pool` track the current values while iterating.
	for poolID, pool := range state.Pools {
		if pool == nil {
			delete(state.Pools, poolID)
			continue
		}
		pool.PoolID = poolID
		pool.TokenA = normalizeDTLTokenID(pool.TokenA)
		pool.TokenB = normalizeDTLTokenID(pool.TokenB)
		pool.ProtocolFeeAccount = normalizeDTLAccount(pool.ProtocolFeeAccount)
	}
	state.PoolIndex = canonicalDTLValueMap(state.PoolIndex, normalizeDTLPoolPairHashKey)
	// `key` and `poolID` track the key used to access the related value.
	for key, poolID := range state.PoolIndex {
		state.PoolIndex[key] = normalizeDTLPoolID(poolID)
	}
	state.LPBalances = canonicalDTLValueMap(state.LPBalances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLPoolID, normalizeDTLAccount)
	})

	state.Duels = canonicalDTLValueMap(state.Duels, normalizeDTLTokenID)
	// `duelID` and `duel` track the current values while iterating.
	for duelID, duel := range state.Duels {
		if duel == nil {
			delete(state.Duels, duelID)
			continue
		}
		duel.DuelID = duelID
		duel.TokenID = normalizeDTLTokenID(duel.TokenID)
		duel.PlayerA = normalizeDTLAccount(duel.PlayerA)
		duel.PlayerB = normalizeDTLAccount(duel.PlayerB)
		duel.Winner = normalizeDTLAccount(duel.Winner)
		duel.CommitA = strings.ToLower(strings.TrimSpace(duel.CommitA))
		duel.CommitB = strings.ToLower(strings.TrimSpace(duel.CommitB))
		duel.BeaconHash = strings.TrimSpace(duel.BeaconHash)
		duel.FinalizationSeed = strings.TrimSpace(duel.FinalizationSeed)
	}

	state.LendingMarkets = canonicalDTLValueMap(state.LendingMarkets, normalizeDTLMarketID)
	// `marketID` and `market` track the current values while iterating.
	for marketID, market := range state.LendingMarkets {
		if market == nil {
			delete(state.LendingMarkets, marketID)
			continue
		}
		market.MarketID = marketID
		market.CollateralTokenID = normalizeDTLTokenID(market.CollateralTokenID)
		market.DebtTokenID = normalizeDTLTokenID(market.DebtTokenID)
		market.CollateralFeedID = normalizeDTLTokenID(market.CollateralFeedID)
		market.DebtFeedID = normalizeDTLTokenID(market.DebtFeedID)
	}
	state.LendingIndex = canonicalDTLValueMap(state.LendingIndex, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLTokenID)
	})
	// `key` and `marketID` track the key used to access the related value.
	for key, marketID := range state.LendingIndex {
		state.LendingIndex[key] = normalizeDTLMarketID(marketID)
	}
	state.LendingPositions = canonicalDTLValueMap(state.LendingPositions, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLMarketID, normalizeDTLAccount)
	})
	// `key` and `position` track the key used to access the related value.
	for key, position := range state.LendingPositions {
		if position == nil {
			delete(state.LendingPositions, key)
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.Split(key, "|")
		if len(parts) == 2 {
			position.MarketID = parts[0]
			position.Account = parts[1]
		} else {
			position.MarketID = normalizeDTLMarketID(position.MarketID)
			position.Account = normalizeDTLAccount(position.Account)
		}
	}

	state.Tournaments = canonicalDTLValueMap(state.Tournaments, normalizeDTLTournamentID)
	// `tournamentID` and `tournament` track the current values while iterating.
	for tournamentID, tournament := range state.Tournaments {
		if tournament == nil {
			delete(state.Tournaments, tournamentID)
			continue
		}
		tournament.TournamentID = tournamentID
		tournament.TokenID = normalizeDTLTokenID(tournament.TokenID)
		tournament.Creator = normalizeDTLAccount(tournament.Creator)
		tournament.Winner = normalizeDTLAccount(tournament.Winner)
		// `i` tracks the current position in the related collection.
		for i := range tournament.Players {
			tournament.Players[i] = normalizeDTLAccount(tournament.Players[i])
		}
		tournament.Commits = canonicalDTLValueMap(tournament.Commits, normalizeDTLAccount)
		// `player` and `commit` track the current values while iterating.
		for player, commit := range tournament.Commits {
			tournament.Commits[player] = strings.ToLower(strings.TrimSpace(commit))
		}
		tournament.Reveals = canonicalDTLValueMap(tournament.Reveals, normalizeDTLAccount)
		// `player` and `reveal` track the current values while iterating.
		for player, reveal := range tournament.Reveals {
			tournament.Reveals[player] = strings.TrimSpace(reveal)
		}
		tournament.BeaconHash = strings.TrimSpace(tournament.BeaconHash)
		tournament.FinalizationSeed = strings.TrimSpace(tournament.FinalizationSeed)
	}

	state.FarmPools = canonicalDTLValueMap(state.FarmPools, normalizeDTLFarmID)
	// `farmID` and `farm` track the current values while iterating.
	for farmID, farm := range state.FarmPools {
		if farm == nil {
			delete(state.FarmPools, farmID)
			continue
		}
		farm.FarmID = farmID
		farm.PoolID = normalizeDTLPoolID(farm.PoolID)
		farm.Creator = normalizeDTLAccount(farm.Creator)
	}
	state.FarmPositions = canonicalDTLValueMap(state.FarmPositions, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLFarmID, normalizeDTLAccount)
	})
	// `key` and `pos` track the key used to access the related value.
	for key, pos := range state.FarmPositions {
		if pos == nil {
			delete(state.FarmPositions, key)
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.Split(key, "|")
		if len(parts) == 2 {
			pos.FarmID = parts[0]
			pos.Account = parts[1]
		} else {
			pos.FarmID = normalizeDTLFarmID(pos.FarmID)
			pos.Account = normalizeDTLAccount(pos.Account)
		}
	}

	state.Seasons = canonicalDTLValueMap(state.Seasons, normalizeDTLSeasonID)
	// `seasonID` and `season` track the current values while iterating.
	for seasonID, season := range state.Seasons {
		if season == nil {
			delete(state.Seasons, seasonID)
			continue
		}
		season.SeasonID = seasonID
		season.Creator = normalizeDTLAccount(season.Creator)
		season.RewardToken = normalizeDTLTokenID(season.RewardToken)
	}
	state.SeasonScores = canonicalDTLValueMap(state.SeasonScores, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLSeasonID, normalizeDTLAccount)
	})
	state.SeasonClaims = canonicalDTLValueMap(state.SeasonClaims, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLSeasonID, normalizeDTLAccount)
	})
	state.SeasonVaults = canonicalDTLValueMap(state.SeasonVaults, normalizeDTLSeasonID)

	state.OracleFeeds = canonicalDTLValueMap(state.OracleFeeds, normalizeDTLTokenID)
	// `feedID` and `feed` track the current values while iterating.
	for feedID, feed := range state.OracleFeeds {
		if feed == nil {
			delete(state.OracleFeeds, feedID)
			continue
		}
		feed.FeedID = feedID
		feed.BaseTokenID = normalizeDTLTokenID(feed.BaseTokenID)
		feed.QuoteTokenID = normalizeDTLTokenID(feed.QuoteTokenID)
		feed.Signers = uniqueDTLSigners(feed.Signers)
	}
	state.OracleSamples = canonicalDTLValueMap(state.OracleSamples, normalizeDTLTokenID)
	// `feedID` and `bySigner` track the current values while iterating.
	for feedID, bySigner := range state.OracleSamples {
		if bySigner == nil {
			delete(state.OracleSamples, feedID)
			continue
		}
		state.OracleSamples[feedID] = canonicalDTLValueMap(bySigner, normalizeDTLAccount)
		// `signer` and `sample` track the current values while iterating.
		for signer, sample := range state.OracleSamples[feedID] {
			sample.FeedID = feedID
			sample.Signer = signer
			state.OracleSamples[feedID][signer] = sample
		}
	}

	state.Contracts = canonicalDTLValueMap(state.Contracts, normalizeDTLContractID)
	// `contractID` and `contract` track the current values while iterating.
	for contractID, contract := range state.Contracts {
		if contract == nil {
			delete(state.Contracts, contractID)
			continue
		}
		contract.ContractID = contractID
		contract.Creator = normalizeDTLAccount(contract.Creator)
		contract.Lang = strings.ToLower(strings.TrimSpace(contract.Lang))
		contract.LogicHash = strings.ToLower(strings.TrimSpace(contract.LogicHash))
		contract.LogicPack = cloneDTLLogicPack(contract.LogicPack)
		contract.Standard = strings.ToUpper(strings.TrimSpace(contract.Standard))
		contract.ABI = canonicalDTLRawJSONMessageForState(contract.ABI)
		contract.MetadataURI = strings.TrimSpace(contract.MetadataURI)
		contract.Interfaces = canonicalDTLInterfaces(contract.Interfaces)
		contract.ProxyTarget = normalizeDTLContractID(contract.ProxyTarget)
		contract.Bytecode = strings.ToLower(strings.TrimSpace(contract.Bytecode))
		contract.BytecodeFormat = normalizeDTLBytecodeFormat(contract.BytecodeFormat)
		contract.BytecodeHash = strings.ToLower(strings.TrimSpace(contract.BytecodeHash))
		contract.Compiler = strings.TrimSpace(contract.Compiler)
		contract.SourceHash = strings.TrimSpace(contract.SourceHash)
		contract.Methods = canonicalDTLValueMap(contract.Methods, normalizeDTLContractMethodName)
		// `methodName` and `method` track the current values while iterating.
		for methodName, method := range contract.Methods {
			if method == nil {
				delete(contract.Methods, methodName)
				continue
			}
			method.Name = methodName
			method.Op = DTLContractOp(strings.ToUpper(strings.TrimSpace(string(method.Op))))
			method.Key = strings.TrimSpace(method.Key)
			method.Arg = strings.TrimSpace(method.Arg)
			method.ToArg = strings.TrimSpace(method.ToArg)
			method.TokenID = normalizeDTLTokenID(method.TokenID)
			method.From = strings.ToLower(strings.TrimSpace(method.From))
		}
		contract.Storage = canonicalDTLValueMap(contract.Storage, trimDTLHashKey)
		// `key` and `value` track the key used to access the related value.
		for key, value := range contract.Storage {
			contract.Storage[key] = strings.TrimSpace(value)
		}
	}

	state.FrozenAccounts = canonicalDTLValueMap(state.FrozenAccounts, normalizeDTLTokenID)
	// `tokenID` and `byAccount` track the measured quantity used by this operation.
	for tokenID, byAccount := range state.FrozenAccounts {
		if byAccount == nil {
			delete(state.FrozenAccounts, tokenID)
			continue
		}
		state.FrozenAccounts[tokenID] = canonicalDTLValueMap(byAccount, normalizeDTLAccount)
	}
	state.GovernanceReplay = canonicalDTLValueMap(state.GovernanceReplay, normalizeDTLGovernanceReplayHashKey)
	// `i` tracks the current position in the related collection.
	for i := range state.Events {
		state.Events[i] = strings.TrimSpace(state.Events[i])
	}
	// `i` tracks the current position in the related collection.
	for i := range state.EventLogs {
		state.EventLogs[i].ContractID = normalizeDTLContractID(state.EventLogs[i].ContractID)
		state.EventLogs[i].Data = canonicalDTLRawJSONStringForState(state.EventLogs[i].Data)
		state.EventLogs[i].TxID = strings.ToLower(strings.TrimSpace(state.EventLogs[i].TxID))
		// `j` tracks the current position in the related collection.
		for j := range state.EventLogs[i].Topics {
			state.EventLogs[i].Topics[j] = strings.TrimSpace(state.EventLogs[i].Topics[j])
		}
	}
	state.canonical = true
}

// appendDTLStateHashMaterial implements the append dtl state hash material helper.
func appendDTLStateHashMaterial(b *strings.Builder, state *DTLState) {
	if b == nil || state == nil {
		return
	}
	state.ensure()
	appendDTLStateCanonicalDuplicateMarkers(b, state)

	// `tokenIDs` and `tokensByID` store the value produced by this operation.
	tokenIDs, tokensByID := canonicalDTLMap(state.Tokens, normalizeDTLTokenID)
	// `tokenID` tracks the current values while iterating.
	for _, tokenID := range tokenIDs {
		// `token` stores the value produced by this operation.
		token := tokensByID[tokenID]
		if token == nil {
			continue
		}
		// `signers` stores the value produced by this operation.
		signers := uniqueDTLSigners(token.AuthoritySigners)
		b.WriteString("dtl_token|")
		b.WriteString(tokenID)
		b.WriteString("=")
		b.WriteString(token.Name)
		b.WriteString("|")
		b.WriteString(normalizeDTLSymbol(token.Symbol))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(token.Decimals)))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(token.MaxSupply, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(token.TotalSupply, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(token.Paused))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(token.FreezeEnabled))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(token.TaxBPS)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(token.AuthorityThreshold)))
		b.WriteString("|")
		// `i` and `signer` track the current position in the related collection.
		for i, signer := range signers {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(signer)
		}
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(token.MetadataURI))
		b.WriteString(";")
	}

	// `symbolKeys` and `symbolCanonical` store the value produced by this operation.
	symbolKeys, symbolCanonical := canonicalDTLMap(state.SymbolIndex, normalizeDTLSymbol)
	// `symbol` tracks the current values while iterating.
	for _, symbol := range symbolKeys {
		b.WriteString("dtl_symbol|")
		b.WriteString(symbol)
		b.WriteString("=")
		b.WriteString(normalizeDTLTokenID(symbolCanonical[symbol]))
		b.WriteString(";")
	}

	// `balanceKeys` and `balancesByKey` store the key used to access the related value.
	balanceKeys, balancesByKey := canonicalDTLMap(state.Balances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range balanceKeys {
		b.WriteString("dtl_balance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(balancesByKey[key], 10))
		b.WriteString(";")
	}

	// `allowanceKeys` and `allowancesByKey` store the key used to access the related value.
	allowanceKeys, allowancesByKey := canonicalDTLMap(state.Allowances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLAccount, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range allowanceKeys {
		b.WriteString("dtl_allowance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(allowancesByKey[key], 10))
		b.WriteString(";")
	}

	// `nft721CollectionIDs` and `nft721CollectionsByID` store the value produced by this operation.
	nft721CollectionIDs, nft721CollectionsByID := canonicalDTLMap(state.NFT721Collections, normalizeDTLCollectionID)
	// `collectionID` tracks the current values while iterating.
	for _, collectionID := range nft721CollectionIDs {
		// `collection` stores the value produced by this operation.
		collection := nft721CollectionsByID[collectionID]
		if collection == nil {
			continue
		}
		b.WriteString("dtl_nft721_collection|")
		b.WriteString(collectionID)
		b.WriteString("=")
		b.WriteString(normalizeDTLAccount(collection.Creator))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(collection.Name))
		b.WriteString("|")
		b.WriteString(normalizeDTLSymbol(collection.Symbol))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(collection.BaseURI))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(collection.NextTokenID, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(collection.TotalMinted, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(collection.Paused))
		b.WriteString(";")
	}

	// `nft721SymbolKeys` and `nft721SymbolCanonical` store the value produced by this operation.
	nft721SymbolKeys, nft721SymbolCanonical := canonicalDTLMap(state.NFT721SymbolIndex, normalizeDTLSymbol)
	// `symbol` tracks the current values while iterating.
	for _, symbol := range nft721SymbolKeys {
		b.WriteString("dtl_nft721_symbol|")
		b.WriteString(symbol)
		b.WriteString("=")
		b.WriteString(normalizeDTLCollectionID(nft721SymbolCanonical[symbol]))
		b.WriteString(";")
	}

	// `nft721OwnerKeys` and `nft721OwnersByKey` store the key used to access the related value.
	nft721OwnerKeys, nft721OwnersByKey := canonicalDTLMap(state.NFT721Owners, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range nft721OwnerKeys {
		b.WriteString("dtl_nft721_owner|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLAccount(nft721OwnersByKey[key]))
		b.WriteString(";")
	}

	// `nft721URIKeys` and `nft721URIsByKey` store the key used to access the related value.
	nft721URIKeys, nft721URIsByKey := canonicalDTLMap(state.NFT721TokenURIs, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range nft721URIKeys {
		b.WriteString("dtl_nft721_uri|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(nft721URIsByKey[key]))
		b.WriteString(";")
	}

	// `nft1155CollectionIDs` and `nft1155CollectionsByID` store the value produced by this operation.
	nft1155CollectionIDs, nft1155CollectionsByID := canonicalDTLMap(state.NFT1155Collections, normalizeDTLCollectionID)
	// `collectionID` tracks the current values while iterating.
	for _, collectionID := range nft1155CollectionIDs {
		// `collection` stores the value produced by this operation.
		collection := nft1155CollectionsByID[collectionID]
		if collection == nil {
			continue
		}
		b.WriteString("dtl_nft1155_collection|")
		b.WriteString(collectionID)
		b.WriteString("=")
		b.WriteString(normalizeDTLAccount(collection.Creator))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(collection.Name))
		b.WriteString("|")
		b.WriteString(normalizeDTLSymbol(collection.Symbol))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(collection.BaseURI))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(collection.Paused))
		b.WriteString(";")
	}

	// `nft1155SymbolKeys` and `nft1155SymbolCanonical` store the value produced by this operation.
	nft1155SymbolKeys, nft1155SymbolCanonical := canonicalDTLMap(state.NFT1155SymbolIndex, normalizeDTLSymbol)
	// `symbol` tracks the current values while iterating.
	for _, symbol := range nft1155SymbolKeys {
		b.WriteString("dtl_nft1155_symbol|")
		b.WriteString(symbol)
		b.WriteString("=")
		b.WriteString(normalizeDTLCollectionID(nft1155SymbolCanonical[symbol]))
		b.WriteString(";")
	}

	// `nft1155BalanceKeys` and `nft1155BalancesByKey` store the key used to access the related value.
	nft1155BalanceKeys, nft1155BalancesByKey := canonicalDTLMap(state.NFT1155Balances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range nft1155BalanceKeys {
		b.WriteString("dtl_nft1155_balance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(nft1155BalancesByKey[key], 10))
		b.WriteString(";")
	}

	// `nft1155SupplyKeys` and `nft1155SuppliesByKey` store the key used to access the related value.
	nft1155SupplyKeys, nft1155SuppliesByKey := canonicalDTLMap(state.NFT1155Supplies, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLCollectionID, normalizeDTLUintHashKeyPart)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range nft1155SupplyKeys {
		b.WriteString("dtl_nft1155_supply|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(nft1155SuppliesByKey[key], 10))
		b.WriteString(";")
	}

	// `poolIDs` and `poolsByID` store the value produced by this operation.
	poolIDs, poolsByID := canonicalDTLMap(state.Pools, normalizeDTLPoolID)
	// `poolID` tracks the current values while iterating.
	for _, poolID := range poolIDs {
		// `pool` stores the value produced by this operation.
		pool := poolsByID[poolID]
		if pool == nil {
			continue
		}
		b.WriteString("dtl_pool|")
		b.WriteString(poolID)
		b.WriteString("=")
		b.WriteString(normalizeDTLTokenID(pool.TokenA))
		b.WriteString("|")
		b.WriteString(normalizeDTLTokenID(pool.TokenB))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pool.ReserveA, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pool.ReserveB, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pool.TotalLPShares, 10))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(pool.FeeBPS)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(pool.ProtocolFeeBPS)))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(pool.ProtocolFeeAccount))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pool.PriceCumulativeA, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pool.PriceCumulativeB, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pool.LastTwapHeight, 10))
		b.WriteString(";")
	}

	// `pairKeys` and `poolIndexByKey` store the key used to access the related value.
	pairKeys, poolIndexByKey := canonicalDTLMap(state.PoolIndex, normalizeDTLPoolPairHashKey)
	// `key` tracks the key used to access the related value.
	for _, key := range pairKeys {
		b.WriteString("dtl_pool_idx|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLPoolID(poolIndexByKey[key]))
		b.WriteString(";")
	}

	// `lpKeys` and `lpBalancesByKey` store the key used to access the related value.
	lpKeys, lpBalancesByKey := canonicalDTLMap(state.LPBalances, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLPoolID, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range lpKeys {
		b.WriteString("dtl_lp_balance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(lpBalancesByKey[key], 10))
		b.WriteString(";")
	}

	// `duelIDs` and `duelsByID` store the value produced by this operation.
	duelIDs, duelsByID := canonicalDTLMap(state.Duels, normalizeDTLTokenID)
	// `duelID` tracks the current values while iterating.
	for _, duelID := range duelIDs {
		// `duel` stores the value produced by this operation.
		duel := duelsByID[duelID]
		if duel == nil {
			continue
		}
		b.WriteString("dtl_duel|")
		b.WriteString(duelID)
		b.WriteString("=")
		b.WriteString(normalizeDTLTokenID(duel.TokenID))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(duel.Stake, 10))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(duel.PlayerA))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(duel.PlayerB))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(duel.CommitA)))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(duel.CommitB)))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(duel.RevealA))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(duel.RevealB))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(duel.JoinDeadline, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(duel.RevealDeadline, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(duel.Settled))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(duel.Winner))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(duel.BeaconHeight, 10))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(duel.BeaconHash))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(duel.FinalizationSeed))
		b.WriteString(";")
	}

	// `marketIDs` and `marketsByID` store the value produced by this operation.
	marketIDs, marketsByID := canonicalDTLMap(state.LendingMarkets, normalizeDTLMarketID)
	// `marketID` tracks the current values while iterating.
	for _, marketID := range marketIDs {
		// `market` stores the value produced by this operation.
		market := marketsByID[marketID]
		if market == nil {
			continue
		}
		b.WriteString("dtl_lend_market|")
		b.WriteString(marketID)
		b.WriteString("=")
		b.WriteString(normalizeDTLTokenID(market.CollateralTokenID))
		b.WriteString("|")
		b.WriteString(normalizeDTLTokenID(market.DebtTokenID))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(market.CollateralFactorBPS)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(market.LiquidationBonusBPS)))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(market.TotalCollateral, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(market.TotalDebt, 10))
		b.WriteString("|")
		b.WriteString(normalizeDTLTokenID(market.CollateralFeedID))
		b.WriteString("|")
		b.WriteString(normalizeDTLTokenID(market.DebtFeedID))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(market.ReserveFactorBPS)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(market.BaseBorrowRateBPS)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(market.SlopeBorrowRateBPS)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(market.CloseFactorBPS)))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(market.BorrowIndex, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(market.LastAccrualHeight, 10))
		b.WriteString(";")
	}

	// `lendingIndexKeys` and `lendingIndexByKey` store the key used to access the related value.
	lendingIndexKeys, lendingIndexByKey := canonicalDTLMap(state.LendingIndex, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLTokenID, normalizeDTLTokenID)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range lendingIndexKeys {
		b.WriteString("dtl_lend_idx|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLMarketID(lendingIndexByKey[key]))
		b.WriteString(";")
	}

	// `positionKeys` and `positionsByKey` store the key used to access the related value.
	positionKeys, positionsByKey := canonicalDTLMap(state.LendingPositions, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLMarketID, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range positionKeys {
		// `position` stores the value produced by this operation.
		position := positionsByKey[key]
		if position == nil {
			continue
		}
		b.WriteString("dtl_lend_pos|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(position.Collateral, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(position.Debt, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(position.ScaledDebt, 10))
		b.WriteString(";")
	}

	// `tournamentIDs` and `tournamentsByID` store the value produced by this operation.
	tournamentIDs, tournamentsByID := canonicalDTLMap(state.Tournaments, normalizeDTLTournamentID)
	// `tournamentID` tracks the current values while iterating.
	for _, tournamentID := range tournamentIDs {
		// `tournament` stores the value produced by this operation.
		tournament := tournamentsByID[tournamentID]
		if tournament == nil {
			continue
		}
		b.WriteString("dtl_tournament|")
		b.WriteString(tournamentID)
		b.WriteString("=")
		b.WriteString(normalizeDTLTokenID(tournament.TokenID))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(tournament.Creator))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(tournament.EntryFee, 10))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(tournament.MaxPlayers)))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(tournament.JoinDeadline, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(tournament.RevealDeadline, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(tournament.Pot, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(tournament.Settled))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(tournament.Winner))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(tournament.BeaconHeight, 10))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(tournament.BeaconHash))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(tournament.FinalizationSeed))
		b.WriteString("|")
		// `i` and `player` track the current position in the related collection.
		for i, player := range tournament.Players {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(normalizeDTLAccount(player))
		}
		b.WriteString("|")

		// `commitKeys` and `commitsByPlayer` store the value produced by this operation.
		commitKeys, commitsByPlayer := canonicalDTLMap(tournament.Commits, normalizeDTLAccount)
		// `i` and `player` track the current position in the related collection.
		for i, player := range commitKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(player)
			b.WriteString(":")
			b.WriteString(strings.ToLower(strings.TrimSpace(commitsByPlayer[player])))
		}
		b.WriteString("|")

		// `revealKeys` and `revealsByPlayer` store the value produced by this operation.
		revealKeys, revealsByPlayer := canonicalDTLMap(tournament.Reveals, normalizeDTLAccount)
		// `i` and `player` track the current position in the related collection.
		for i, player := range revealKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(player)
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(revealsByPlayer[player]))
		}
		b.WriteString(";")
	}

	// `farmIDs` and `farmsByID` store the value produced by this operation.
	farmIDs, farmsByID := canonicalDTLMap(state.FarmPools, normalizeDTLFarmID)
	// `farmID` tracks the current values while iterating.
	for _, farmID := range farmIDs {
		// `farm` stores the value produced by this operation.
		farm := farmsByID[farmID]
		if farm == nil {
			continue
		}
		b.WriteString("dtl_farm|")
		b.WriteString(farmID)
		b.WriteString("=")
		b.WriteString(normalizeDTLPoolID(farm.PoolID))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(farm.Creator))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(farm.MultiplierBPS)))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(farm.CreatedHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(farm.LastUpdateHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(farm.Active))
		b.WriteString(";")
	}

	// `farmPositionKeys` and `farmPositionsByKey` store the key used to access the related value.
	farmPositionKeys, farmPositionsByKey := canonicalDTLMap(state.FarmPositions, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLFarmID, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range farmPositionKeys {
		// `pos` stores the value produced by this operation.
		pos := farmPositionsByKey[key]
		if pos == nil {
			continue
		}
		b.WriteString("dtl_farm_pos|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLFarmID(pos.FarmID))
		b.WriteString("|")
		b.WriteString(normalizeDTLAccount(pos.Account))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pos.StakedLP, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pos.LastStakeHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pos.LastAccrualHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(pos.AccruedPoints, 10))
		b.WriteString(";")
	}

	// `seasonIDs` and `seasonsByID` store the value produced by this operation.
	seasonIDs, seasonsByID := canonicalDTLMap(state.Seasons, normalizeDTLSeasonID)
	// `seasonID` tracks the current values while iterating.
	for _, seasonID := range seasonIDs {
		// `season` stores the value produced by this operation.
		season := seasonsByID[seasonID]
		if season == nil {
			continue
		}
		b.WriteString("dtl_season|")
		b.WriteString(seasonID)
		b.WriteString("=")
		b.WriteString(normalizeDTLAccount(season.Creator))
		b.WriteString("|")
		b.WriteString(normalizeDTLTokenID(season.RewardToken))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(season.StartHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(season.EndHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(season.ClaimGraceEndHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(season.Finalized))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(season.FinalizedHeight, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(season.TotalScore, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(season.TotalClaimed, 10))
		b.WriteString(";")
	}

	// `seasonScoreKeys` and `seasonScoresByKey` store the key used to access the related value.
	seasonScoreKeys, seasonScoresByKey := canonicalDTLMap(state.SeasonScores, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLSeasonID, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range seasonScoreKeys {
		b.WriteString("dtl_season_score|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(seasonScoresByKey[key], 10))
		b.WriteString(";")
	}

	// `seasonClaimKeys` and `seasonClaimsByKey` store the key used to access the related value.
	seasonClaimKeys, seasonClaimsByKey := canonicalDTLMap(state.SeasonClaims, func(key string) string {
		return canonicalDTLCompositeHashKey(key, normalizeDTLSeasonID, normalizeDTLAccount)
	})
	// `key` tracks the key used to access the related value.
	for _, key := range seasonClaimKeys {
		b.WriteString("dtl_season_claim|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatBool(seasonClaimsByKey[key]))
		b.WriteString(";")
	}

	// `seasonVaultIDs` and `seasonVaultsByID` store the value produced by this operation.
	seasonVaultIDs, seasonVaultsByID := canonicalDTLMap(state.SeasonVaults, normalizeDTLSeasonID)
	// `seasonID` tracks the current values while iterating.
	for _, seasonID := range seasonVaultIDs {
		b.WriteString("dtl_season_vault|")
		b.WriteString(seasonID)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(seasonVaultsByID[seasonID], 10))
		b.WriteString(";")
	}

	// `oracleFeedIDs` and `oracleFeedsByID` store the value produced by this operation.
	oracleFeedIDs, oracleFeedsByID := canonicalDTLMap(state.OracleFeeds, normalizeDTLTokenID)
	// `feedID` tracks the current values while iterating.
	for _, feedID := range oracleFeedIDs {
		// `feed` stores the value produced by this operation.
		feed := oracleFeedsByID[feedID]
		if feed == nil {
			continue
		}
		// `signers` stores the value produced by this operation.
		signers := uniqueDTLSigners(feed.Signers)
		b.WriteString("dtl_oracle_feed|")
		b.WriteString(feedID)
		b.WriteString("=")
		b.WriteString(normalizeDTLTokenID(feed.BaseTokenID))
		b.WriteString("|")
		b.WriteString(normalizeDTLTokenID(feed.QuoteTokenID))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(feed.Threshold)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(feed.Decimals)))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(feed.LastMedianPrice, 10))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(feed.LastUpdateHeight, 10))
		b.WriteString("|")
		// `i` and `signer` track the current position in the related collection.
		for i, signer := range signers {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(signer)
		}
		b.WriteString(";")
	}

	// `oracleSampleFeedIDs` and `oracleSamplesByFeedID` store the value produced by this operation.
	oracleSampleFeedIDs, oracleSamplesByFeedID := canonicalDTLMap(state.OracleSamples, normalizeDTLTokenID)
	// `feedID` tracks the current values while iterating.
	for _, feedID := range oracleSampleFeedIDs {
		// `bySigner` stores the value produced by this operation.
		bySigner := oracleSamplesByFeedID[feedID]
		if bySigner == nil {
			continue
		}
		// `signerKeys` and `samplesBySigner` store the value produced by this operation.
		signerKeys, samplesBySigner := canonicalDTLMap(bySigner, normalizeDTLAccount)
		// `signer` tracks the current values while iterating.
		for _, signer := range signerKeys {
			// `sample` stores the value produced by this operation.
			sample := samplesBySigner[signer]
			b.WriteString("dtl_oracle_sample|")
			b.WriteString(feedID)
			b.WriteString("|")
			b.WriteString(signer)
			b.WriteString("=")
			b.WriteString(strconv.FormatUint(sample.Price, 10))
			b.WriteString("|")
			b.WriteString(strconv.FormatUint(sample.Height, 10))
			b.WriteString(";")
		}
	}

	// `contractIDs` and `contractsByID` store the value produced by this operation.
	contractIDs, contractsByID := canonicalDTLMap(state.Contracts, normalizeDTLContractID)
	// `contractID` tracks the current values while iterating.
	for _, contractID := range contractIDs {
		// `contract` stores the value produced by this operation.
		contract := contractsByID[contractID]
		if contract == nil {
			continue
		}
		b.WriteString("dtl_contract|")
		b.WriteString(contractID)
		b.WriteString("=")
		b.WriteString(normalizeDTLAccount(contract.Creator))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(contract.Name))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(contract.Lang)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(contract.Version)))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(contract.LogicHash)))
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(contract.Paused))
		b.WriteString("|")
		b.WriteString(strings.ToUpper(strings.TrimSpace(contract.Standard)))
		b.WriteString("|")
		b.WriteString(canonicalDTLRawJSONMessageForHash(contract.ABI))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(contract.MetadataURI))
		b.WriteString("|")
		// `interfaces` stores the current position in the related collection.
		interfaces := canonicalDTLInterfaces(contract.Interfaces)
		// `i` and `iface` track the current position in the related collection.
		for i, iface := range interfaces {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(iface)
		}
		b.WriteString("|")
		b.WriteString(strconv.FormatBool(contract.Upgradeable))
		b.WriteString("|")
		b.WriteString(normalizeDTLContractID(contract.ProxyTarget))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(contract.Bytecode)))
		b.WriteString("|")
		b.WriteString(normalizeDTLBytecodeFormat(contract.BytecodeFormat))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(contract.BytecodeHash)))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(contract.Compiler))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(contract.SourceHash))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(int(contract.BytecodeVersion)))
		b.WriteString("|")
		if contract.LogicPack != nil {
			// `logicPackHash` and `err` store the error produced by this operation.
			if logicPackHash, err := dtlLogicPackHash(contract.LogicPack); err == nil {
				b.WriteString(strings.ToLower(strings.TrimSpace(logicPackHash)))
			}
		}
		b.WriteString("|")

		// `methodNames` and `methodsByName` store the value produced by this operation.
		methodNames, methodsByName := canonicalDTLMap(contract.Methods, normalizeDTLContractMethodName)
		// `i` and `methodName` track the current position in the related collection.
		for i, methodName := range methodNames {
			// `method` stores the value produced by this operation.
			method := methodsByName[methodName]
			if method == nil {
				continue
			}
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(methodName)
			b.WriteString(":")
			b.WriteString(strings.ToUpper(strings.TrimSpace(string(method.Op))))
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(method.Key))
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(method.Arg))
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(method.ToArg))
			b.WriteString(":")
			b.WriteString(normalizeDTLTokenID(method.TokenID))
			b.WriteString(":")
			b.WriteString(strings.ToLower(strings.TrimSpace(method.From)))
		}
		b.WriteString("|")

		// `storageKeys` and `storageByKey` store the key used to access the related value.
		storageKeys, storageByKey := canonicalDTLMap(contract.Storage, trimDTLHashKey)
		// `i` and `key` track the key used to access the related value.
		for i, key := range storageKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(key)
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(storageByKey[key]))
		}
		b.WriteString(";")
	}

	// `frozenTokenIDs` and `frozenByTokenID` store the value produced by this operation.
	frozenTokenIDs, frozenByTokenID := canonicalDTLMap(state.FrozenAccounts, normalizeDTLTokenID)
	// `tokenID` tracks the current values while iterating.
	for _, tokenID := range frozenTokenIDs {
		// `byAccount` stores the measured quantity used by this operation.
		byAccount := frozenByTokenID[tokenID]
		if len(byAccount) == 0 {
			continue
		}
		// `accounts` and `frozenByAccount` store the measured quantity used by this operation.
		accounts, frozenByAccount := canonicalDTLMap(byAccount, normalizeDTLAccount)
		// `account` tracks the measured quantity used by this operation.
		for _, account := range accounts {
			b.WriteString("dtl_frozen|")
			b.WriteString(tokenID)
			b.WriteString("|")
			b.WriteString(account)
			b.WriteString("=")
			b.WriteString(strconv.FormatBool(frozenByAccount[account]))
			b.WriteString(";")
		}
	}

	// `replayKeys` and `governanceReplayByKey` store the key used to access the related value.
	replayKeys, governanceReplayByKey := canonicalDTLMap(state.GovernanceReplay, normalizeDTLGovernanceReplayHashKey)
	// `key` tracks the key used to access the related value.
	for _, key := range replayKeys {
		b.WriteString("dtl_replay|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(governanceReplayByKey[key], 10))
		b.WriteString(";")
	}

	// `i` and `event` track the current position in the related collection.
	for i, event := range state.Events {
		b.WriteString("dtl_event|")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(event))
		b.WriteString(";")
	}

	// `i` and `logEntry` track the current position in the related collection.
	for i, logEntry := range state.EventLogs {
		b.WriteString("dtl_event_log|")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("=")
		b.WriteString(normalizeDTLContractID(logEntry.ContractID))
		b.WriteString("|")
		// `j` and `topic` track the current position in the related collection.
		for j, topic := range logEntry.Topics {
			if j > 0 {
				b.WriteString(",")
			}
			b.WriteString(strings.TrimSpace(topic))
		}
		b.WriteString("|")
		b.WriteString(canonicalDTLRawJSONStringForHash(logEntry.Data))
		b.WriteString("|")
		b.WriteString(strconv.FormatUint(logEntry.BlockHeight, 10))
		b.WriteString("|")
		b.WriteString(strings.ToLower(strings.TrimSpace(logEntry.TxID)))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(logEntry.TxIndex))
		b.WriteString("|")
		b.WriteString(strconv.Itoa(logEntry.LogIndex))
		b.WriteString(";")
	}
}

// parseDTLTxType parses dtl tx type.
func parseDTLTxType(raw string) (DTLTxType, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(DTLTxTokenCreate):
		return DTLTxTokenCreate, nil
	case string(DTLTxTokenTransfer):
		return DTLTxTokenTransfer, nil
	case string(DTLTxTokenApprove):
		return DTLTxTokenApprove, nil
	case string(DTLTxTokenTransferFrom):
		return DTLTxTokenTransferFrom, nil
	case string(DTLTxTokenMint):
		return DTLTxTokenMint, nil
	case string(DTLTxTokenBurn):
		return DTLTxTokenBurn, nil
	case string(DTLTxNFT721Create):
		return DTLTxNFT721Create, nil
	case string(DTLTxNFT721Mint):
		return DTLTxNFT721Mint, nil
	case string(DTLTxNFT721Transfer):
		return DTLTxNFT721Transfer, nil
	case string(DTLTxNFT1155Create):
		return DTLTxNFT1155Create, nil
	case string(DTLTxNFT1155Mint):
		return DTLTxNFT1155Mint, nil
	case string(DTLTxNFT1155Transfer):
		return DTLTxNFT1155Transfer, nil
	case string(DTLTxPoolCreate):
		return DTLTxPoolCreate, nil
	case string(DTLTxPoolAdd):
		return DTLTxPoolAdd, nil
	case string(DTLTxPoolRemove):
		return DTLTxPoolRemove, nil
	case string(DTLTxPoolSwap):
		return DTLTxPoolSwap, nil
	case string(DTLTxPoolSwapRoute):
		return DTLTxPoolSwapRoute, nil
	case string(DTLTxFarmCreate):
		return DTLTxFarmCreate, nil
	case string(DTLTxFarmStakeLP):
		return DTLTxFarmStakeLP, nil
	case string(DTLTxFarmUnstakeLP):
		return DTLTxFarmUnstakeLP, nil
	case string(DTLTxFarmClaim):
		return DTLTxFarmClaim, nil
	case string(DTLTxDuelCreate):
		return DTLTxDuelCreate, nil
	case string(DTLTxDuelJoin):
		return DTLTxDuelJoin, nil
	case string(DTLTxDuelReveal):
		return DTLTxDuelReveal, nil
	case string(DTLTxDuelFinalize):
		return DTLTxDuelFinalize, nil
	case string(DTLTxLendMarketCreate):
		return DTLTxLendMarketCreate, nil
	case string(DTLTxLendDeposit):
		return DTLTxLendDeposit, nil
	case string(DTLTxLendBorrow):
		return DTLTxLendBorrow, nil
	case string(DTLTxLendRepay):
		return DTLTxLendRepay, nil
	case string(DTLTxLendWithdraw):
		return DTLTxLendWithdraw, nil
	case string(DTLTxLendLiquidate):
		return DTLTxLendLiquidate, nil
	case string(DTLTxTournamentCreate):
		return DTLTxTournamentCreate, nil
	case string(DTLTxTournamentJoin):
		return DTLTxTournamentJoin, nil
	case string(DTLTxTournamentReveal):
		return DTLTxTournamentReveal, nil
	case string(DTLTxTournamentFinalize):
		return DTLTxTournamentFinalize, nil
	case string(DTLTxSeasonCreate):
		return DTLTxSeasonCreate, nil
	case string(DTLTxSeasonFinalize):
		return DTLTxSeasonFinalize, nil
	case string(DTLTxSeasonClaim):
		return DTLTxSeasonClaim, nil
	case string(DTLTxOracleFeedCreate):
		return DTLTxOracleFeedCreate, nil
	case string(DTLTxOraclePriceSubmit):
		return DTLTxOraclePriceSubmit, nil
	case string(DTLTxContractDeploy):
		return "", dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	case string(DTLTxContractCall):
		return "", dtlContractRuntimeRemovedError("CONTRACT_CALL")
	default:
		return "", fmt.Errorf("dtl: unsupported tx type: %s", raw)
	}
}

// decodeDTLTransaction implements the decode dtl transaction helper.
func decodeDTLTransaction(tx Transaction) (dtlDecodedTx, error) {
	// `out` stores the result produced by this operation.
	out := dtlDecodedTx{}
	// `kind` and `err` store the error produced by this operation.
	kind, err := parseDTLTxType(tx.DTLTxType)
	if err != nil {
		return out, err
	}
	out.Kind = kind

	// `rawPayload` stores the value produced by this operation.
	rawPayload := strings.TrimSpace(tx.DTLPayload)
	if rawPayload == "" {
		return out, fmt.Errorf("dtl: missing dtl_payload")
	}

	switch kind {
	case DTLTxTokenCreate:
		// `p` stores the value used by this operation.
		var p DTLCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Creator), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: create creator mismatch")
		}
		out.Create = &p
	case DTLTxTokenTransfer:
		// `p` stores the value used by this operation.
		var p DTLTransferTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid transfer payload: %w", err)
		}
		if strings.TrimSpace(p.From) == "" {
			p.From = tx.From
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.Transfer = &p
	case DTLTxTokenApprove:
		// `p` stores the value used by this operation.
		var p DTLApproveTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid approve payload: %w", err)
		}
		if strings.TrimSpace(p.Owner) == "" {
			p.Owner = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Owner), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: approve owner mismatch")
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.Approve = &p
	case DTLTxTokenTransferFrom:
		// `p` stores the value used by this operation.
		var p DTLTransferFromTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid transfer-from payload: %w", err)
		}
		if strings.TrimSpace(p.Spender) == "" {
			p.Spender = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Spender), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: transfer-from spender mismatch")
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.TransferFrom = &p
	case DTLTxTokenMint:
		// `p` stores the value used by this operation.
		var p DTLMintTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid mint payload: %w", err)
		}
		if strings.TrimSpace(p.Proposer) == "" {
			p.Proposer = tx.From
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.Mint = &p

		// `rawCert` stores the value produced by this operation.
		rawCert := strings.TrimSpace(tx.DTLGovernanceCert)
		if rawCert == "" {
			return out, fmt.Errorf("dtl: mint requires governance cert")
		}
		// `cert` stores the value used by this operation.
		var cert DTLGovernanceCert
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawCert), &cert); err != nil {
			return out, fmt.Errorf("dtl: invalid governance cert: %w", err)
		}
		out.MintCert = &cert
	case DTLTxTokenBurn:
		// `p` stores the value used by this operation.
		var p DTLBurnTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid burn payload: %w", err)
		}
		if strings.TrimSpace(p.From) == "" {
			p.From = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.From), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: burn from mismatch")
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.Burn = &p
	case DTLTxNFT721Create:
		// `p` stores the value used by this operation.
		var p DTLNFT721CreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid nft721 create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Creator), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: nft721 create creator mismatch")
		}
		out.NFT721Create = &p
	case DTLTxNFT721Mint:
		// `p` stores the value used by this operation.
		var p DTLNFT721MintTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid nft721 mint payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Creator), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: nft721 mint creator mismatch")
		}
		if strings.TrimSpace(p.CollectionID) == "" {
			p.CollectionID = tx.DTLTokenID
		}
		out.NFT721Mint = &p
	case DTLTxNFT721Transfer:
		// `p` stores the value used by this operation.
		var p DTLNFT721TransferTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid nft721 transfer payload: %w", err)
		}
		if strings.TrimSpace(p.From) == "" {
			p.From = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.From), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: nft721 transfer from mismatch")
		}
		if strings.TrimSpace(p.CollectionID) == "" {
			p.CollectionID = tx.DTLTokenID
		}
		out.NFT721Transfer = &p
	case DTLTxNFT1155Create:
		// `p` stores the value used by this operation.
		var p DTLNFT1155CreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid nft1155 create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Creator), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: nft1155 create creator mismatch")
		}
		out.NFT1155Create = &p
	case DTLTxNFT1155Mint:
		// `p` stores the value used by this operation.
		var p DTLNFT1155MintTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid nft1155 mint payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.Creator), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: nft1155 mint creator mismatch")
		}
		if strings.TrimSpace(p.CollectionID) == "" {
			p.CollectionID = tx.DTLTokenID
		}
		out.NFT1155Mint = &p
	case DTLTxNFT1155Transfer:
		// `p` stores the value used by this operation.
		var p DTLNFT1155TransferTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid nft1155 transfer payload: %w", err)
		}
		if strings.TrimSpace(p.From) == "" {
			p.From = tx.From
		}
		if !strings.EqualFold(normalizeDTLAccount(p.From), normalizeDTLAccount(tx.From)) {
			return out, fmt.Errorf("dtl: nft1155 transfer from mismatch")
		}
		if strings.TrimSpace(p.CollectionID) == "" {
			p.CollectionID = tx.DTLTokenID
		}
		out.NFT1155Transfer = &p
	case DTLTxPoolCreate:
		// `p` stores the value used by this operation.
		var p DTLPoolCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.PoolCreate = &p
	case DTLTxPoolAdd:
		// `p` stores the value used by this operation.
		var p DTLPoolAddLiquidityTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool add payload: %w", err)
		}
		if strings.TrimSpace(p.Provider) == "" {
			p.Provider = tx.From
		}
		out.PoolAdd = &p
	case DTLTxPoolRemove:
		// `p` stores the value used by this operation.
		var p DTLPoolRemoveLiquidityTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool remove payload: %w", err)
		}
		if strings.TrimSpace(p.Provider) == "" {
			p.Provider = tx.From
		}
		out.PoolRemove = &p
	case DTLTxPoolSwap:
		// `p` stores the value used by this operation.
		var p DTLPoolSwapTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool swap payload: %w", err)
		}
		if strings.TrimSpace(p.Trader) == "" {
			p.Trader = tx.From
		}
		out.PoolSwap = &p
	case DTLTxPoolSwapRoute:
		// `p` stores the value used by this operation.
		var p DTLPoolSwapRouteTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool route swap payload: %w", err)
		}
		if strings.TrimSpace(p.Trader) == "" {
			p.Trader = tx.From
		}
		if strings.TrimSpace(p.TokenIn) == "" {
			p.TokenIn = tx.DTLTokenID
		}
		out.PoolSwapRoute = &p
	case DTLTxFarmCreate:
		// `p` stores the value used by this operation.
		var p DTLFarmCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.FarmCreate = &p
	case DTLTxFarmStakeLP:
		// `p` stores the value used by this operation.
		var p DTLFarmStakeLPTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm stake payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.FarmStakeLP = &p
	case DTLTxFarmUnstakeLP:
		// `p` stores the value used by this operation.
		var p DTLFarmUnstakeLPTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm unstake payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.FarmUnstakeLP = &p
	case DTLTxFarmClaim:
		// `p` stores the value used by this operation.
		var p DTLFarmClaimTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm claim payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.FarmClaim = &p
	case DTLTxDuelCreate:
		// `p` stores the value used by this operation.
		var p DTLDuelCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.DuelCreate = &p
	case DTLTxDuelJoin:
		// `p` stores the value used by this operation.
		var p DTLDuelJoinTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel join payload: %w", err)
		}
		if strings.TrimSpace(p.Joiner) == "" {
			p.Joiner = tx.From
		}
		out.DuelJoin = &p
	case DTLTxDuelReveal:
		// `p` stores the value used by this operation.
		var p DTLDuelRevealTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel reveal payload: %w", err)
		}
		if strings.TrimSpace(p.Player) == "" {
			p.Player = tx.From
		}
		out.DuelReveal = &p
	case DTLTxDuelFinalize:
		// `p` stores the value used by this operation.
		var p DTLDuelFinalizeTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel finalize payload: %w", err)
		}
		if strings.TrimSpace(p.Caller) == "" {
			p.Caller = tx.From
		}
		out.DuelFinal = &p
	case DTLTxLendMarketCreate:
		// `p` stores the value used by this operation.
		var p DTLLendMarketCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend market create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.LendMarketCreate = &p
	case DTLTxLendDeposit:
		// `p` stores the value used by this operation.
		var p DTLLendDepositCollateralTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend deposit payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendDeposit = &p
	case DTLTxLendBorrow:
		// `p` stores the value used by this operation.
		var p DTLLendBorrowTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend borrow payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendBorrow = &p
	case DTLTxLendRepay:
		// `p` stores the value used by this operation.
		var p DTLLendRepayTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend repay payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendRepay = &p
	case DTLTxLendWithdraw:
		// `p` stores the value used by this operation.
		var p DTLLendWithdrawCollateralTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend withdraw payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendWithdraw = &p
	case DTLTxLendLiquidate:
		// `p` stores the value used by this operation.
		var p DTLLendLiquidateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend liquidate payload: %w", err)
		}
		if strings.TrimSpace(p.Liquidator) == "" {
			p.Liquidator = tx.From
		}
		out.LendLiquidate = &p
	case DTLTxTournamentCreate:
		// `p` stores the value used by this operation.
		var p DTLTournamentCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		if strings.TrimSpace(p.TokenID) == "" {
			p.TokenID = tx.DTLTokenID
		}
		out.TournamentCreate = &p
	case DTLTxTournamentJoin:
		// `p` stores the value used by this operation.
		var p DTLTournamentJoinTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament join payload: %w", err)
		}
		if strings.TrimSpace(p.Player) == "" {
			p.Player = tx.From
		}
		out.TournamentJoin = &p
	case DTLTxTournamentReveal:
		// `p` stores the value used by this operation.
		var p DTLTournamentRevealTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament reveal payload: %w", err)
		}
		if strings.TrimSpace(p.Player) == "" {
			p.Player = tx.From
		}
		out.TournamentReveal = &p
	case DTLTxTournamentFinalize:
		// `p` stores the value used by this operation.
		var p DTLTournamentFinalizeTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament finalize payload: %w", err)
		}
		if strings.TrimSpace(p.Caller) == "" {
			p.Caller = tx.From
		}
		out.TournamentFinalize = &p
	case DTLTxSeasonCreate:
		// `p` stores the value used by this operation.
		var p DTLSeasonCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid season create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.SeasonCreate = &p
	case DTLTxSeasonFinalize:
		// `p` stores the value used by this operation.
		var p DTLSeasonFinalizeTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid season finalize payload: %w", err)
		}
		if strings.TrimSpace(p.Caller) == "" {
			p.Caller = tx.From
		}
		out.SeasonFinalize = &p
	case DTLTxSeasonClaim:
		// `p` stores the value used by this operation.
		var p DTLSeasonClaimTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid season claim payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.SeasonClaim = &p
	case DTLTxOracleFeedCreate:
		// `p` stores the value used by this operation.
		var p DTLOracleFeedCreateTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid oracle feed create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.OracleFeedCreate = &p
	case DTLTxOraclePriceSubmit:
		// `p` stores the value used by this operation.
		var p DTLOraclePriceSubmitTx
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid oracle price submit payload: %w", err)
		}
		if strings.TrimSpace(p.Submitter) == "" {
			p.Submitter = tx.From
		}
		out.OraclePriceSubmit = &p
	case DTLTxContractDeploy:
		return out, dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	case DTLTxContractCall:
		return out, dtlContractRuntimeRemovedError("CONTRACT_CALL")
	default:
		return out, fmt.Errorf("dtl: unsupported decoded tx kind")
	}

	return out, nil
}

// validateNativeDTLTransaction validates a transaction using only DTL-owned
// state and deterministic protocol inputs.
func validateNativeDTLTransaction(state *NativeDTLState, tx Transaction, currentEpoch uint64) error {
	return validateNativeDTLTransactionVersion(state, tx, currentEpoch, false)
}

// validateNativeDTLTransactionVersion validates against block-committed DTL
// semantics. The boolean is chosen by DTLExecutorV1/V2, not local config.
func validateNativeDTLTransactionVersion(state *NativeDTLState, tx Transaction, currentEpoch uint64, v2 bool) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if state.State == nil {
		state.State = NewDTLState()
	}
	state.State.ensure()
	if state.UsedBridgeEvents == nil {
		state.UsedBridgeEvents = make(map[string]uint64)
	}

	// `decoded` and `err` store the error produced by this operation.
	decoded, err := decodeDTLTransaction(tx)
	if err != nil {
		return err
	}

	switch decoded.Kind {
	case DTLTxTokenCreate:
		return ValidateDTLCreateTx(state.State, *decoded.Create)
	case DTLTxTokenTransfer:
		return ValidateDTLTransferTx(state.State, *decoded.Transfer)
	case DTLTxTokenApprove:
		return ValidateDTLApproveTx(state.State, *decoded.Approve)
	case DTLTxTokenTransferFrom:
		return ValidateDTLTransferFromTx(state.State, *decoded.TransferFrom)
	case DTLTxTokenMint:
		// `err` stores the error produced by this operation.
		if err := ValidateDTLMintTx(state.State, *decoded.Mint); err != nil {
			return err
		}
		// `tokenID` stores the value produced by this operation.
		tokenID := normalizeDTLTokenID(decoded.Mint.TokenID)
		// `token` stores the value produced by this operation.
		token := state.State.Tokens[tokenID]
		if token == nil {
			return ErrDTLUnknownToken
		}
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
			To:      normalizeDTLAccount(decoded.Mint.To),
			Amount:  decoded.Mint.Amount,
		})
		if err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := ValidateDTLGovernanceCert(
			token,
			*decoded.MintCert,
			DTLGovMint,
			payloadHash,
			currentEpoch,
			DTLDefaultReplayWindow,
		); err != nil {
			return err
		}
		if err := dtlCheckReplay(state.State, *decoded.MintCert); err != nil {
			return err
		}
		if bridgeID, ok := bridgeIDFromMintCertificate(*decoded.MintCert); ok {
			if _, consumed := state.UsedBridgeEvents[bridgeID]; consumed {
				return fmt.Errorf("bridge event already consumed: %s", bridgeID)
			}
		}
		return nil
	case DTLTxTokenBurn:
		return ValidateDTLBurnTx(state.State, *decoded.Burn)
	case DTLTxNFT721Create:
		return ValidateDTLNFT721CreateTx(state.State, *decoded.NFT721Create)
	case DTLTxNFT721Mint:
		return ValidateDTLNFT721MintTx(state.State, *decoded.NFT721Mint)
	case DTLTxNFT721Transfer:
		return ValidateDTLNFT721TransferTx(state.State, *decoded.NFT721Transfer)
	case DTLTxNFT1155Create:
		return ValidateDTLNFT1155CreateTx(state.State, *decoded.NFT1155Create)
	case DTLTxNFT1155Mint:
		return ValidateDTLNFT1155MintTx(state.State, *decoded.NFT1155Mint)
	case DTLTxNFT1155Transfer:
		return ValidateDTLNFT1155TransferTx(state.State, *decoded.NFT1155Transfer)
	case DTLTxPoolCreate:
		return ValidateDTLPoolCreateTx(state.State, *decoded.PoolCreate)
	case DTLTxPoolAdd:
		return ValidateDTLPoolAddLiquidityTx(state.State, *decoded.PoolAdd)
	case DTLTxPoolRemove:
		return ValidateDTLPoolRemoveLiquidityTx(state.State, *decoded.PoolRemove)
	case DTLTxPoolSwap:
		return ValidateDTLPoolSwapTx(state.State, *decoded.PoolSwap)
	case DTLTxPoolSwapRoute:
		return ValidateDTLPoolSwapRouteTx(state.State, *decoded.PoolSwapRoute, currentEpoch)
	case DTLTxFarmCreate:
		return ValidateDTLFarmCreateTx(state.State, *decoded.FarmCreate)
	case DTLTxFarmStakeLP:
		return ValidateDTLFarmStakeLPTx(state.State, *decoded.FarmStakeLP)
	case DTLTxFarmUnstakeLP:
		return ValidateDTLFarmUnstakeLPTx(state.State, *decoded.FarmUnstakeLP)
	case DTLTxFarmClaim:
		return ValidateDTLFarmClaimTx(state.State, *decoded.FarmClaim)
	case DTLTxDuelCreate:
		return ValidateDTLDuelCreateTx(state.State, *decoded.DuelCreate, currentEpoch)
	case DTLTxDuelJoin:
		return ValidateDTLDuelJoinTx(state.State, *decoded.DuelJoin, currentEpoch)
	case DTLTxDuelReveal:
		return ValidateDTLDuelRevealTx(state.State, *decoded.DuelReveal, currentEpoch)
	case DTLTxDuelFinalize:
		return ValidateDTLDuelFinalizeTx(state.State, *decoded.DuelFinal, currentEpoch)
	case DTLTxLendMarketCreate:
		return ValidateDTLLendMarketCreateTx(state.State, *decoded.LendMarketCreate)
	case DTLTxLendDeposit:
		return ValidateDTLLendDepositCollateralTx(state.State, *decoded.LendDeposit)
	case DTLTxLendBorrow:
		return ValidateDTLLendBorrowTx(state.State, *decoded.LendBorrow)
	case DTLTxLendRepay:
		return ValidateDTLLendRepayTx(state.State, *decoded.LendRepay)
	case DTLTxLendWithdraw:
		return ValidateDTLLendWithdrawCollateralTx(state.State, *decoded.LendWithdraw)
	case DTLTxLendLiquidate:
		return ValidateDTLLendLiquidateTx(state.State, *decoded.LendLiquidate, currentEpoch)
	case DTLTxTournamentCreate:
		return ValidateDTLTournamentCreateTx(state.State, *decoded.TournamentCreate, currentEpoch)
	case DTLTxTournamentJoin:
		return ValidateDTLTournamentJoinTx(state.State, *decoded.TournamentJoin, currentEpoch)
	case DTLTxTournamentReveal:
		return ValidateDTLTournamentRevealTx(state.State, *decoded.TournamentReveal, currentEpoch)
	case DTLTxTournamentFinalize:
		return ValidateDTLTournamentFinalizeTx(state.State, *decoded.TournamentFinalize, currentEpoch)
	case DTLTxSeasonCreate:
		return ValidateDTLSeasonCreateTx(state.State, *decoded.SeasonCreate, currentEpoch)
	case DTLTxSeasonFinalize:
		return ValidateDTLSeasonFinalizeTx(state.State, *decoded.SeasonFinalize, currentEpoch)
	case DTLTxSeasonClaim:
		return ValidateDTLSeasonClaimTx(state.State, *decoded.SeasonClaim, currentEpoch)
	case DTLTxOracleFeedCreate:
		if !v2 {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", currentEpoch)
		}
		return ValidateDTLOracleFeedCreateTx(state.State, *decoded.OracleFeedCreate)
	case DTLTxOraclePriceSubmit:
		if !dtlProtocolV2EnabledAtHeight(currentEpoch) {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", currentEpoch)
		}
		return ValidateDTLOraclePriceSubmitTx(state.State, *decoded.OraclePriceSubmit, currentEpoch)
	case DTLTxContractDeploy:
		return dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	case DTLTxContractCall:
		return dtlContractRuntimeRemovedError("CONTRACT_CALL")
	default:
		return fmt.Errorf("dtl: unsupported tx type")
	}
}

// validateDTLTransaction is the compatibility adapter used by mempool/RPC
// validation. Consensus execution itself crosses the DTLExecutor boundary.
func validateDTLTransaction(ledger *Ledger, tx Transaction, currentEpoch uint64) error {
	return validateNativeDTLTransactionVersionForLedger(ledger, tx, currentEpoch, false)
}

func validateNativeDTLTransactionVersionForLedger(ledger *Ledger, tx Transaction, currentEpoch uint64, v2 bool) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	state := nativeDTLStateFromLedger(*ledger)
	return validateNativeDTLTransactionVersion(&state, tx, currentEpoch, v2)
}

// canonicalDTLTransactionChainID returns canonical dtl transaction chain id.
func canonicalDTLTransactionChainID(tx Transaction) (string, error) {
	// `chainID` stores the value produced by this operation.
	chainID := strings.TrimSpace(tx.ChainID)
	if chainID == "" {
		chainID = protocolChainID()
	}
	if !isProtocolChainID(chainID) {
		return "", fmt.Errorf("dtl: invalid chain id: %s", chainID)
	}
	return chainID, nil
}

// applyNativeDTLTransaction mutates only DTL-owned state.
func applyNativeDTLTransaction(state *NativeDTLState, tx Transaction, height uint64) error {
	return applyNativeDTLTransactionVersion(state, tx, height, false)
}

// applyNativeDTLTransactionVersion applies the exact executor version selected
// by the block protocol envelope.
func applyNativeDTLTransactionVersion(state *NativeDTLState, tx Transaction, height uint64, v2 bool) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if state.State == nil {
		state.State = NewDTLState()
	}
	state.State.ensure()
	if state.UsedBridgeEvents == nil {
		state.UsedBridgeEvents = make(map[string]uint64)
	}

	// `decoded` and `err` store the error produced by this operation.
	decoded, err := decodeDTLTransaction(tx)
	if err != nil {
		return err
	}
	// `chainID` and `err` store the error produced by this operation.
	chainID, err := canonicalDTLTransactionChainID(tx)
	if err != nil {
		return err
	}

	switch decoded.Kind {
	case DTLTxTokenCreate:
		_, err = ApplyDTLCreateTx(state.State, chainID, uint64(tx.Nonce), *decoded.Create)
		return err
	case DTLTxTokenTransfer:
		return ApplyDTLTransferTx(state.State, *decoded.Transfer)
	case DTLTxTokenApprove:
		return ApplyDTLApproveTx(state.State, *decoded.Approve)
	case DTLTxTokenTransferFrom:
		return ApplyDTLTransferFromTx(state.State, *decoded.TransferFrom)
	case DTLTxTokenMint:
		if bridgeID, ok := bridgeIDFromMintCertificate(*decoded.MintCert); ok {
			if _, consumed := state.UsedBridgeEvents[bridgeID]; consumed {
				return fmt.Errorf("bridge event already consumed: %s", bridgeID)
			}
		}
		if err := ApplyDTLMintTx(
			state.State,
			*decoded.Mint,
			*decoded.MintCert,
			height,
			DTLDefaultReplayWindow,
		); err != nil {
			return err
		}
		if bridgeID, ok := bridgeIDFromMintCertificate(*decoded.MintCert); ok {
			state.UsedBridgeEvents[bridgeID] = height
		}
		return nil
	case DTLTxTokenBurn:
		return ApplyDTLBurnTx(state.State, *decoded.Burn)
	case DTLTxNFT721Create:
		_, err = ApplyDTLNFT721CreateTx(state.State, chainID, uint64(tx.Nonce), *decoded.NFT721Create)
		return err
	case DTLTxNFT721Mint:
		_, err = ApplyDTLNFT721MintTx(state.State, *decoded.NFT721Mint)
		return err
	case DTLTxNFT721Transfer:
		return ApplyDTLNFT721TransferTx(state.State, *decoded.NFT721Transfer)
	case DTLTxNFT1155Create:
		_, err = ApplyDTLNFT1155CreateTx(state.State, chainID, uint64(tx.Nonce), *decoded.NFT1155Create)
		return err
	case DTLTxNFT1155Mint:
		return ApplyDTLNFT1155MintTx(state.State, *decoded.NFT1155Mint)
	case DTLTxNFT1155Transfer:
		return ApplyDTLNFT1155TransferTx(state.State, *decoded.NFT1155Transfer)
	case DTLTxPoolCreate:
		_, err = ApplyDTLPoolCreateTx(state.State, chainID, uint64(tx.Nonce), *decoded.PoolCreate)
		return err
	case DTLTxPoolAdd:
		return ApplyDTLPoolAddLiquidityTx(state.State, *decoded.PoolAdd)
	case DTLTxPoolRemove:
		return ApplyDTLPoolRemoveLiquidityTx(state.State, *decoded.PoolRemove)
	case DTLTxPoolSwap:
		_, err = ApplyDTLPoolSwapTx(state.State, *decoded.PoolSwap)
		return err
	case DTLTxPoolSwapRoute:
		_, err = ApplyDTLPoolSwapRouteTx(state.State, *decoded.PoolSwapRoute, height)
		return err
	case DTLTxFarmCreate:
		_, err = ApplyDTLFarmCreateTx(state.State, chainID, uint64(tx.Nonce), height, *decoded.FarmCreate)
		return err
	case DTLTxFarmStakeLP:
		return ApplyDTLFarmStakeLPTx(state.State, height, *decoded.FarmStakeLP)
	case DTLTxFarmUnstakeLP:
		return ApplyDTLFarmUnstakeLPTx(state.State, height, *decoded.FarmUnstakeLP)
	case DTLTxFarmClaim:
		_, err = ApplyDTLFarmClaimTx(state.State, height, *decoded.FarmClaim)
		return err
	case DTLTxDuelCreate:
		_, err = ApplyDTLDuelCreateTx(state.State, chainID, uint64(tx.Nonce), height, *decoded.DuelCreate)
		return err
	case DTLTxDuelJoin:
		return ApplyDTLDuelJoinTx(state.State, height, *decoded.DuelJoin)
	case DTLTxDuelReveal:
		return ApplyDTLDuelRevealTx(state.State, height, *decoded.DuelReveal)
	case DTLTxDuelFinalize:
		return ApplyDTLDuelFinalizeTx(state.State, height, *decoded.DuelFinal)
	case DTLTxLendMarketCreate:
		_, err = ApplyDTLLendMarketCreateTx(state.State, chainID, uint64(tx.Nonce), *decoded.LendMarketCreate)
		return err
	case DTLTxLendDeposit:
		return ApplyDTLLendDepositCollateralTxWithHeight(state.State, height, *decoded.LendDeposit)
	case DTLTxLendBorrow:
		return ApplyDTLLendBorrowTxWithHeight(state.State, height, *decoded.LendBorrow)
	case DTLTxLendRepay:
		return ApplyDTLLendRepayTxWithHeight(state.State, height, *decoded.LendRepay)
	case DTLTxLendWithdraw:
		return ApplyDTLLendWithdrawCollateralTxWithHeight(state.State, height, *decoded.LendWithdraw)
	case DTLTxLendLiquidate:
		return ApplyDTLLendLiquidateTxWithHeight(state.State, height, *decoded.LendLiquidate)
	case DTLTxTournamentCreate:
		_, err = ApplyDTLTournamentCreateTx(state.State, chainID, uint64(tx.Nonce), height, *decoded.TournamentCreate)
		return err
	case DTLTxTournamentJoin:
		return ApplyDTLTournamentJoinTx(state.State, height, *decoded.TournamentJoin)
	case DTLTxTournamentReveal:
		return ApplyDTLTournamentRevealTx(state.State, height, *decoded.TournamentReveal)
	case DTLTxTournamentFinalize:
		return ApplyDTLTournamentFinalizeTx(state.State, height, *decoded.TournamentFinalize)
	case DTLTxSeasonCreate:
		_, err = ApplyDTLSeasonCreateTx(state.State, chainID, uint64(tx.Nonce), height, *decoded.SeasonCreate)
		return err
	case DTLTxSeasonFinalize:
		return ApplyDTLSeasonFinalizeTx(state.State, height, *decoded.SeasonFinalize)
	case DTLTxSeasonClaim:
		_, err = ApplyDTLSeasonClaimTx(state.State, height, *decoded.SeasonClaim)
		return err
	case DTLTxOracleFeedCreate:
		if !v2 {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", uint64(height))
		}
		_, err = ApplyDTLOracleFeedCreateTx(state.State, chainID, uint64(tx.Nonce), *decoded.OracleFeedCreate)
		return err
	case DTLTxOraclePriceSubmit:
		if !dtlProtocolV2EnabledAtHeight(uint64(height)) {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", uint64(height))
		}
		return ApplyDTLOraclePriceSubmitTx(state.State, height, *decoded.OraclePriceSubmit)
	case DTLTxContractDeploy:
		return dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	case DTLTxContractCall:
		return dtlContractRuntimeRemovedError("CONTRACT_CALL")
	default:
		return fmt.Errorf("dtl: unsupported tx kind")
	}
}

// applyDTLTransaction preserves the legacy call surface while routing the
// transition through the stateless DTL executor.
func applyDTLTransaction(ledger *Ledger, tx Transaction, height int) error {
	return applyDTLTransactionVersion(ledger, tx, height, false)
}

func applyDTLTransactionVersion(ledger *Ledger, tx Transaction, height int, v2 bool) error {
	if ledger == nil || height < 0 {
		return ErrDTLInvalidState
	}
	input, err := nativeDTLExecutionInput(
		nativeDTLStateFromLedger(*ledger),
		[]Transaction{tx},
		uint64(height),
		nativeDTLExecutorVersion(v2),
	)
	if err != nil {
		return err
	}
	result, err := (NativeDTLExecutor{}).Execute(input)
	if err != nil {
		return err
	}
	projectNativeDTLStateToLedger(ledger, result.Next)
	return nil
}
