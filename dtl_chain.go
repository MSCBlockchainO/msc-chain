package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type dtlDecodedTx struct {
	Kind               DTLTxType
	Create             *DTLCreateTx
	Transfer           *DTLTransferTx
	Approve            *DTLApproveTx
	TransferFrom       *DTLTransferFromTx
	Mint               *DTLMintTx
	Burn               *DTLBurnTx
	NFT721Create       *DTLNFT721CreateTx
	NFT721Mint         *DTLNFT721MintTx
	NFT721Transfer     *DTLNFT721TransferTx
	NFT1155Create      *DTLNFT1155CreateTx
	NFT1155Mint        *DTLNFT1155MintTx
	NFT1155Transfer    *DTLNFT1155TransferTx
	PoolCreate         *DTLPoolCreateTx
	PoolAdd            *DTLPoolAddLiquidityTx
	PoolRemove         *DTLPoolRemoveLiquidityTx
	PoolSwap           *DTLPoolSwapTx
	PoolSwapRoute      *DTLPoolSwapRouteTx
	FarmCreate         *DTLFarmCreateTx
	FarmStakeLP        *DTLFarmStakeLPTx
	FarmUnstakeLP      *DTLFarmUnstakeLPTx
	FarmClaim          *DTLFarmClaimTx
	DuelCreate         *DTLDuelCreateTx
	DuelJoin           *DTLDuelJoinTx
	DuelReveal         *DTLDuelRevealTx
	DuelFinal          *DTLDuelFinalizeTx
	LendMarketCreate   *DTLLendMarketCreateTx
	LendDeposit        *DTLLendDepositCollateralTx
	LendBorrow         *DTLLendBorrowTx
	LendRepay          *DTLLendRepayTx
	LendWithdraw       *DTLLendWithdrawCollateralTx
	LendLiquidate      *DTLLendLiquidateTx
	TournamentCreate   *DTLTournamentCreateTx
	TournamentJoin     *DTLTournamentJoinTx
	TournamentReveal   *DTLTournamentRevealTx
	TournamentFinalize *DTLTournamentFinalizeTx
	SeasonCreate       *DTLSeasonCreateTx
	SeasonFinalize     *DTLSeasonFinalizeTx
	SeasonClaim        *DTLSeasonClaimTx
	OracleFeedCreate   *DTLOracleFeedCreateTx
	OraclePriceSubmit  *DTLOraclePriceSubmitTx
	ContractDeploy     *DTLContractDeployTx
	ContractCall       *DTLContractCallTx
	MintCert           *DTLGovernanceCert
}

func ensureDTLState(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.DTL == nil {
		ledger.DTL = NewDTLState()
		return
	}
	ledger.DTL.ensure()
}

func cloneDTLState(src *DTLState) *DTLState {
	if src == nil {
		return nil
	}
	src.ensure()
	out := NewDTLState()

	for tokenID, token := range src.Tokens {
		if token == nil {
			continue
		}
		signers := append([]string(nil), token.AuthoritySigners...)
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
	for symbol, tokenID := range src.SymbolIndex {
		out.SymbolIndex[symbol] = tokenID
	}
	for key, bal := range src.Balances {
		out.Balances[key] = bal
	}
	for key, allowance := range src.Allowances {
		out.Allowances[key] = allowance
	}
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
	for symbol, collectionID := range src.NFT721SymbolIndex {
		out.NFT721SymbolIndex[symbol] = collectionID
	}
	for key, owner := range src.NFT721Owners {
		out.NFT721Owners[key] = owner
	}
	for key, tokenURI := range src.NFT721TokenURIs {
		out.NFT721TokenURIs[key] = tokenURI
	}
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
	for symbol, collectionID := range src.NFT1155SymbolIndex {
		out.NFT1155SymbolIndex[symbol] = collectionID
	}
	for key, bal := range src.NFT1155Balances {
		out.NFT1155Balances[key] = bal
	}
	for key, total := range src.NFT1155Supplies {
		out.NFT1155Supplies[key] = total
	}
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
	for key, poolID := range src.PoolIndex {
		out.PoolIndex[key] = poolID
	}
	for key, bal := range src.LPBalances {
		out.LPBalances[key] = bal
	}
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
	for key, marketID := range src.LendingIndex {
		out.LendingIndex[key] = marketID
	}
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
	for tournamentID, tournament := range src.Tournaments {
		if tournament == nil {
			continue
		}
		players := append([]string(nil), tournament.Players...)
		commits := make(map[string]string, len(tournament.Commits))
		for k, v := range tournament.Commits {
			commits[k] = v
		}
		reveals := make(map[string]string, len(tournament.Reveals))
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
	for key, score := range src.SeasonScores {
		out.SeasonScores[key] = score
	}
	for key, claimed := range src.SeasonClaims {
		out.SeasonClaims[key] = claimed
	}
	for seasonID, amount := range src.SeasonVaults {
		out.SeasonVaults[seasonID] = amount
	}
	for feedID, feed := range src.OracleFeeds {
		if feed == nil {
			continue
		}
		out.OracleFeeds[feedID] = &DTLOracleFeedState{
			FeedID:           normalizeDTLTokenID(feed.FeedID),
			BaseTokenID:      normalizeDTLTokenID(feed.BaseTokenID),
			QuoteTokenID:     normalizeDTLTokenID(feed.QuoteTokenID),
			Signers:          append([]string(nil), feed.Signers...),
			Threshold:        feed.Threshold,
			Decimals:         feed.Decimals,
			LastMedianPrice:  feed.LastMedianPrice,
			LastUpdateHeight: feed.LastUpdateHeight,
		}
	}
	for feedID, bySigner := range src.OracleSamples {
		if bySigner == nil {
			continue
		}
		dst := make(map[string]DTLOracleSampleState, len(bySigner))
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
	for contractID, contract := range src.Contracts {
		if contract == nil {
			continue
		}
		methods := make(map[string]*DTLContractMethodState, len(contract.Methods))
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
		storage := make(map[string]string, len(contract.Storage))
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
			ABI:             append(json.RawMessage(nil), contract.ABI...),
			MetadataURI:     strings.TrimSpace(contract.MetadataURI),
			Interfaces:      append([]string(nil), contract.Interfaces...),
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
	for tokenID, byAccount := range src.FrozenAccounts {
		if byAccount == nil {
			continue
		}
		dstByAccount := make(map[string]bool, len(byAccount))
		for account, frozen := range byAccount {
			dstByAccount[account] = frozen
		}
		out.FrozenAccounts[tokenID] = dstByAccount
	}
	for key, epoch := range src.GovernanceReplay {
		out.GovernanceReplay[key] = epoch
	}
	if len(src.Events) > 0 {
		out.Events = append([]string(nil), src.Events...)
	}
	if len(src.EventLogs) > 0 {
		out.EventLogs = make([]DTLEventLog, 0, len(src.EventLogs))
		for _, logEntry := range src.EventLogs {
			copied := DTLEventLog{
				ContractID:  normalizeDTLContractID(logEntry.ContractID),
				Topics:      append([]string(nil), logEntry.Topics...),
				Data:        strings.TrimSpace(logEntry.Data),
				BlockHeight: logEntry.BlockHeight,
				TxID:        strings.ToLower(strings.TrimSpace(logEntry.TxID)),
				TxIndex:     logEntry.TxIndex,
				LogIndex:    logEntry.LogIndex,
			}
			out.EventLogs = append(out.EventLogs, copied)
		}
	}
	return out
}

func appendDTLStateHashMaterial(b *strings.Builder, state *DTLState) {
	if b == nil || state == nil {
		return
	}
	state.ensure()

	tokenIDs := make([]string, 0, len(state.Tokens))
	for tokenID := range state.Tokens {
		tokenIDs = append(tokenIDs, normalizeDTLTokenID(tokenID))
	}
	sort.Strings(tokenIDs)
	for _, tokenID := range tokenIDs {
		token := state.Tokens[tokenID]
		if token == nil {
			continue
		}
		signers := append([]string(nil), token.AuthoritySigners...)
		for i := range signers {
			signers[i] = normalizeDTLAccount(signers[i])
		}
		sort.Strings(signers)
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
		for i, signer := range signers {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(signer)
		}
		b.WriteString(";")
	}

	balanceKeys := make([]string, 0, len(state.Balances))
	for key := range state.Balances {
		balanceKeys = append(balanceKeys, key)
	}
	sort.Strings(balanceKeys)
	for _, key := range balanceKeys {
		b.WriteString("dtl_balance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.Balances[key], 10))
		b.WriteString(";")
	}

	allowanceKeys := make([]string, 0, len(state.Allowances))
	for key := range state.Allowances {
		allowanceKeys = append(allowanceKeys, key)
	}
	sort.Strings(allowanceKeys)
	for _, key := range allowanceKeys {
		b.WriteString("dtl_allowance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.Allowances[key], 10))
		b.WriteString(";")
	}

	nft721CollectionIDs := make([]string, 0, len(state.NFT721Collections))
	for collectionID := range state.NFT721Collections {
		nft721CollectionIDs = append(nft721CollectionIDs, normalizeDTLCollectionID(collectionID))
	}
	sort.Strings(nft721CollectionIDs)
	for _, collectionID := range nft721CollectionIDs {
		collection := state.NFT721Collections[collectionID]
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

	nft721SymbolKeys := make([]string, 0, len(state.NFT721SymbolIndex))
	for key := range state.NFT721SymbolIndex {
		nft721SymbolKeys = append(nft721SymbolKeys, key)
	}
	sort.Strings(nft721SymbolKeys)
	for _, symbol := range nft721SymbolKeys {
		b.WriteString("dtl_nft721_symbol|")
		b.WriteString(normalizeDTLSymbol(symbol))
		b.WriteString("=")
		b.WriteString(normalizeDTLCollectionID(state.NFT721SymbolIndex[symbol]))
		b.WriteString(";")
	}

	nft721OwnerKeys := make([]string, 0, len(state.NFT721Owners))
	for key := range state.NFT721Owners {
		nft721OwnerKeys = append(nft721OwnerKeys, key)
	}
	sort.Strings(nft721OwnerKeys)
	for _, key := range nft721OwnerKeys {
		b.WriteString("dtl_nft721_owner|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLAccount(state.NFT721Owners[key]))
		b.WriteString(";")
	}

	nft721URIKeys := make([]string, 0, len(state.NFT721TokenURIs))
	for key := range state.NFT721TokenURIs {
		nft721URIKeys = append(nft721URIKeys, key)
	}
	sort.Strings(nft721URIKeys)
	for _, key := range nft721URIKeys {
		b.WriteString("dtl_nft721_uri|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(state.NFT721TokenURIs[key]))
		b.WriteString(";")
	}

	nft1155CollectionIDs := make([]string, 0, len(state.NFT1155Collections))
	for collectionID := range state.NFT1155Collections {
		nft1155CollectionIDs = append(nft1155CollectionIDs, normalizeDTLCollectionID(collectionID))
	}
	sort.Strings(nft1155CollectionIDs)
	for _, collectionID := range nft1155CollectionIDs {
		collection := state.NFT1155Collections[collectionID]
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

	nft1155SymbolKeys := make([]string, 0, len(state.NFT1155SymbolIndex))
	for key := range state.NFT1155SymbolIndex {
		nft1155SymbolKeys = append(nft1155SymbolKeys, key)
	}
	sort.Strings(nft1155SymbolKeys)
	for _, symbol := range nft1155SymbolKeys {
		b.WriteString("dtl_nft1155_symbol|")
		b.WriteString(normalizeDTLSymbol(symbol))
		b.WriteString("=")
		b.WriteString(normalizeDTLCollectionID(state.NFT1155SymbolIndex[symbol]))
		b.WriteString(";")
	}

	nft1155BalanceKeys := make([]string, 0, len(state.NFT1155Balances))
	for key := range state.NFT1155Balances {
		nft1155BalanceKeys = append(nft1155BalanceKeys, key)
	}
	sort.Strings(nft1155BalanceKeys)
	for _, key := range nft1155BalanceKeys {
		b.WriteString("dtl_nft1155_balance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.NFT1155Balances[key], 10))
		b.WriteString(";")
	}

	nft1155SupplyKeys := make([]string, 0, len(state.NFT1155Supplies))
	for key := range state.NFT1155Supplies {
		nft1155SupplyKeys = append(nft1155SupplyKeys, key)
	}
	sort.Strings(nft1155SupplyKeys)
	for _, key := range nft1155SupplyKeys {
		b.WriteString("dtl_nft1155_supply|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.NFT1155Supplies[key], 10))
		b.WriteString(";")
	}

	poolIDs := make([]string, 0, len(state.Pools))
	for poolID := range state.Pools {
		poolIDs = append(poolIDs, normalizeDTLPoolID(poolID))
	}
	sort.Strings(poolIDs)
	for _, poolID := range poolIDs {
		pool := state.Pools[poolID]
		if pool == nil {
			continue
		}
		b.WriteString("dtl_pool|")
		b.WriteString(poolID)
		b.WriteString("=")
		b.WriteString(pool.TokenA)
		b.WriteString("|")
		b.WriteString(pool.TokenB)
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

	pairKeys := make([]string, 0, len(state.PoolIndex))
	for key := range state.PoolIndex {
		pairKeys = append(pairKeys, key)
	}
	sort.Strings(pairKeys)
	for _, key := range pairKeys {
		b.WriteString("dtl_pool_idx|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLPoolID(state.PoolIndex[key]))
		b.WriteString(";")
	}

	lpKeys := make([]string, 0, len(state.LPBalances))
	for key := range state.LPBalances {
		lpKeys = append(lpKeys, key)
	}
	sort.Strings(lpKeys)
	for _, key := range lpKeys {
		b.WriteString("dtl_lp_balance|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.LPBalances[key], 10))
		b.WriteString(";")
	}

	duelIDs := make([]string, 0, len(state.Duels))
	for duelID := range state.Duels {
		duelIDs = append(duelIDs, normalizeDTLTokenID(duelID))
	}
	sort.Strings(duelIDs)
	for _, duelID := range duelIDs {
		duel := state.Duels[duelID]
		if duel == nil {
			continue
		}
		b.WriteString("dtl_duel|")
		b.WriteString(duelID)
		b.WriteString("=")
		b.WriteString(duel.TokenID)
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

	marketIDs := make([]string, 0, len(state.LendingMarkets))
	for marketID := range state.LendingMarkets {
		marketIDs = append(marketIDs, normalizeDTLMarketID(marketID))
	}
	sort.Strings(marketIDs)
	for _, marketID := range marketIDs {
		market := state.LendingMarkets[marketID]
		if market == nil {
			continue
		}
		b.WriteString("dtl_lend_market|")
		b.WriteString(marketID)
		b.WriteString("=")
		b.WriteString(market.CollateralTokenID)
		b.WriteString("|")
		b.WriteString(market.DebtTokenID)
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

	lendingIndexKeys := make([]string, 0, len(state.LendingIndex))
	for key := range state.LendingIndex {
		lendingIndexKeys = append(lendingIndexKeys, key)
	}
	sort.Strings(lendingIndexKeys)
	for _, key := range lendingIndexKeys {
		b.WriteString("dtl_lend_idx|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(normalizeDTLMarketID(state.LendingIndex[key]))
		b.WriteString(";")
	}

	positionKeys := make([]string, 0, len(state.LendingPositions))
	for key := range state.LendingPositions {
		positionKeys = append(positionKeys, key)
	}
	sort.Strings(positionKeys)
	for _, key := range positionKeys {
		position := state.LendingPositions[key]
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

	tournamentIDs := make([]string, 0, len(state.Tournaments))
	for tournamentID := range state.Tournaments {
		tournamentIDs = append(tournamentIDs, normalizeDTLTournamentID(tournamentID))
	}
	sort.Strings(tournamentIDs)
	for _, tournamentID := range tournamentIDs {
		tournament := state.Tournaments[tournamentID]
		if tournament == nil {
			continue
		}
		b.WriteString("dtl_tournament|")
		b.WriteString(tournamentID)
		b.WriteString("=")
		b.WriteString(tournament.TokenID)
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
		for i, player := range tournament.Players {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(normalizeDTLAccount(player))
		}
		b.WriteString("|")

		commitKeys := make([]string, 0, len(tournament.Commits))
		for player := range tournament.Commits {
			commitKeys = append(commitKeys, normalizeDTLAccount(player))
		}
		sort.Strings(commitKeys)
		for i, player := range commitKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(player)
			b.WriteString(":")
			b.WriteString(strings.ToLower(strings.TrimSpace(tournament.Commits[player])))
		}
		b.WriteString("|")

		revealKeys := make([]string, 0, len(tournament.Reveals))
		for player := range tournament.Reveals {
			revealKeys = append(revealKeys, normalizeDTLAccount(player))
		}
		sort.Strings(revealKeys)
		for i, player := range revealKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(player)
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(tournament.Reveals[player]))
		}
		b.WriteString(";")
	}

	farmIDs := make([]string, 0, len(state.FarmPools))
	for farmID := range state.FarmPools {
		farmIDs = append(farmIDs, normalizeDTLFarmID(farmID))
	}
	sort.Strings(farmIDs)
	for _, farmID := range farmIDs {
		farm := state.FarmPools[farmID]
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

	farmPositionKeys := make([]string, 0, len(state.FarmPositions))
	for key := range state.FarmPositions {
		farmPositionKeys = append(farmPositionKeys, key)
	}
	sort.Strings(farmPositionKeys)
	for _, key := range farmPositionKeys {
		pos := state.FarmPositions[key]
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

	seasonIDs := make([]string, 0, len(state.Seasons))
	for seasonID := range state.Seasons {
		seasonIDs = append(seasonIDs, normalizeDTLSeasonID(seasonID))
	}
	sort.Strings(seasonIDs)
	for _, seasonID := range seasonIDs {
		season := state.Seasons[seasonID]
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

	seasonScoreKeys := make([]string, 0, len(state.SeasonScores))
	for key := range state.SeasonScores {
		seasonScoreKeys = append(seasonScoreKeys, key)
	}
	sort.Strings(seasonScoreKeys)
	for _, key := range seasonScoreKeys {
		b.WriteString("dtl_season_score|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.SeasonScores[key], 10))
		b.WriteString(";")
	}

	seasonClaimKeys := make([]string, 0, len(state.SeasonClaims))
	for key := range state.SeasonClaims {
		seasonClaimKeys = append(seasonClaimKeys, key)
	}
	sort.Strings(seasonClaimKeys)
	for _, key := range seasonClaimKeys {
		b.WriteString("dtl_season_claim|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatBool(state.SeasonClaims[key]))
		b.WriteString(";")
	}

	seasonVaultIDs := make([]string, 0, len(state.SeasonVaults))
	for seasonID := range state.SeasonVaults {
		seasonVaultIDs = append(seasonVaultIDs, normalizeDTLSeasonID(seasonID))
	}
	sort.Strings(seasonVaultIDs)
	for _, seasonID := range seasonVaultIDs {
		b.WriteString("dtl_season_vault|")
		b.WriteString(seasonID)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.SeasonVaults[seasonID], 10))
		b.WriteString(";")
	}

	oracleFeedIDs := make([]string, 0, len(state.OracleFeeds))
	for feedID := range state.OracleFeeds {
		oracleFeedIDs = append(oracleFeedIDs, normalizeDTLTokenID(feedID))
	}
	sort.Strings(oracleFeedIDs)
	for _, feedID := range oracleFeedIDs {
		feed := state.OracleFeeds[feedID]
		if feed == nil {
			continue
		}
		signers := append([]string(nil), feed.Signers...)
		for i := range signers {
			signers[i] = normalizeDTLAccount(signers[i])
		}
		sort.Strings(signers)
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
		for i, signer := range signers {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(signer)
		}
		b.WriteString(";")
	}

	oracleSampleFeedIDs := make([]string, 0, len(state.OracleSamples))
	for feedID := range state.OracleSamples {
		oracleSampleFeedIDs = append(oracleSampleFeedIDs, normalizeDTLTokenID(feedID))
	}
	sort.Strings(oracleSampleFeedIDs)
	for _, feedID := range oracleSampleFeedIDs {
		bySigner := state.OracleSamples[feedID]
		if bySigner == nil {
			continue
		}
		signerKeys := make([]string, 0, len(bySigner))
		for signer := range bySigner {
			signerKeys = append(signerKeys, normalizeDTLAccount(signer))
		}
		sort.Strings(signerKeys)
		for _, signer := range signerKeys {
			sample := bySigner[signer]
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

	contractIDs := make([]string, 0, len(state.Contracts))
	for contractID := range state.Contracts {
		contractIDs = append(contractIDs, normalizeDTLContractID(contractID))
	}
	sort.Strings(contractIDs)
	for _, contractID := range contractIDs {
		contract := state.Contracts[contractID]
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

		methodNames := make([]string, 0, len(contract.Methods))
		for name := range contract.Methods {
			methodNames = append(methodNames, normalizeDTLContractMethodName(name))
		}
		sort.Strings(methodNames)
		for i, methodName := range methodNames {
			method := contract.Methods[methodName]
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

		storageKeys := make([]string, 0, len(contract.Storage))
		for key := range contract.Storage {
			storageKeys = append(storageKeys, strings.TrimSpace(key))
		}
		sort.Strings(storageKeys)
		for i, key := range storageKeys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(key)
			b.WriteString(":")
			b.WriteString(strings.TrimSpace(contract.Storage[key]))
		}
		b.WriteString(";")
	}

	frozenTokenIDs := make([]string, 0, len(state.FrozenAccounts))
	for tokenID := range state.FrozenAccounts {
		frozenTokenIDs = append(frozenTokenIDs, tokenID)
	}
	sort.Strings(frozenTokenIDs)
	for _, tokenID := range frozenTokenIDs {
		byAccount := state.FrozenAccounts[tokenID]
		if len(byAccount) == 0 {
			continue
		}
		accounts := make([]string, 0, len(byAccount))
		for account := range byAccount {
			accounts = append(accounts, normalizeDTLAccount(account))
		}
		sort.Strings(accounts)
		for _, account := range accounts {
			b.WriteString("dtl_frozen|")
			b.WriteString(tokenID)
			b.WriteString("|")
			b.WriteString(account)
			b.WriteString("=")
			b.WriteString(strconv.FormatBool(byAccount[account]))
			b.WriteString(";")
		}
	}

	replayKeys := make([]string, 0, len(state.GovernanceReplay))
	for key := range state.GovernanceReplay {
		replayKeys = append(replayKeys, key)
	}
	sort.Strings(replayKeys)
	for _, key := range replayKeys {
		b.WriteString("dtl_replay|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.GovernanceReplay[key], 10))
		b.WriteString(";")
	}
}

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

func decodeDTLTransaction(tx Transaction) (dtlDecodedTx, error) {
	out := dtlDecodedTx{}
	kind, err := parseDTLTxType(tx.DTLTxType)
	if err != nil {
		return out, err
	}
	out.Kind = kind

	rawPayload := strings.TrimSpace(tx.DTLPayload)
	if rawPayload == "" {
		return out, fmt.Errorf("dtl: missing dtl_payload")
	}

	switch kind {
	case DTLTxTokenCreate:
		var p DTLCreateTx
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
		var p DTLTransferTx
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
		var p DTLApproveTx
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
		var p DTLTransferFromTx
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
		var p DTLMintTx
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

		rawCert := strings.TrimSpace(tx.DTLGovernanceCert)
		if rawCert == "" {
			return out, fmt.Errorf("dtl: mint requires governance cert")
		}
		var cert DTLGovernanceCert
		if err := json.Unmarshal([]byte(rawCert), &cert); err != nil {
			return out, fmt.Errorf("dtl: invalid governance cert: %w", err)
		}
		out.MintCert = &cert
	case DTLTxTokenBurn:
		var p DTLBurnTx
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
		var p DTLNFT721CreateTx
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
		var p DTLNFT721MintTx
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
		var p DTLNFT721TransferTx
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
		var p DTLNFT1155CreateTx
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
		var p DTLNFT1155MintTx
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
		var p DTLNFT1155TransferTx
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
		var p DTLPoolCreateTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.PoolCreate = &p
	case DTLTxPoolAdd:
		var p DTLPoolAddLiquidityTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool add payload: %w", err)
		}
		if strings.TrimSpace(p.Provider) == "" {
			p.Provider = tx.From
		}
		out.PoolAdd = &p
	case DTLTxPoolRemove:
		var p DTLPoolRemoveLiquidityTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool remove payload: %w", err)
		}
		if strings.TrimSpace(p.Provider) == "" {
			p.Provider = tx.From
		}
		out.PoolRemove = &p
	case DTLTxPoolSwap:
		var p DTLPoolSwapTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid pool swap payload: %w", err)
		}
		if strings.TrimSpace(p.Trader) == "" {
			p.Trader = tx.From
		}
		out.PoolSwap = &p
	case DTLTxPoolSwapRoute:
		var p DTLPoolSwapRouteTx
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
		var p DTLFarmCreateTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.FarmCreate = &p
	case DTLTxFarmStakeLP:
		var p DTLFarmStakeLPTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm stake payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.FarmStakeLP = &p
	case DTLTxFarmUnstakeLP:
		var p DTLFarmUnstakeLPTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm unstake payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.FarmUnstakeLP = &p
	case DTLTxFarmClaim:
		var p DTLFarmClaimTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid farm claim payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.FarmClaim = &p
	case DTLTxDuelCreate:
		var p DTLDuelCreateTx
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
		var p DTLDuelJoinTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel join payload: %w", err)
		}
		if strings.TrimSpace(p.Joiner) == "" {
			p.Joiner = tx.From
		}
		out.DuelJoin = &p
	case DTLTxDuelReveal:
		var p DTLDuelRevealTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel reveal payload: %w", err)
		}
		if strings.TrimSpace(p.Player) == "" {
			p.Player = tx.From
		}
		out.DuelReveal = &p
	case DTLTxDuelFinalize:
		var p DTLDuelFinalizeTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid duel finalize payload: %w", err)
		}
		if strings.TrimSpace(p.Caller) == "" {
			p.Caller = tx.From
		}
		out.DuelFinal = &p
	case DTLTxLendMarketCreate:
		var p DTLLendMarketCreateTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend market create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.LendMarketCreate = &p
	case DTLTxLendDeposit:
		var p DTLLendDepositCollateralTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend deposit payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendDeposit = &p
	case DTLTxLendBorrow:
		var p DTLLendBorrowTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend borrow payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendBorrow = &p
	case DTLTxLendRepay:
		var p DTLLendRepayTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend repay payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendRepay = &p
	case DTLTxLendWithdraw:
		var p DTLLendWithdrawCollateralTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend withdraw payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.LendWithdraw = &p
	case DTLTxLendLiquidate:
		var p DTLLendLiquidateTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid lend liquidate payload: %w", err)
		}
		if strings.TrimSpace(p.Liquidator) == "" {
			p.Liquidator = tx.From
		}
		out.LendLiquidate = &p
	case DTLTxTournamentCreate:
		var p DTLTournamentCreateTx
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
		var p DTLTournamentJoinTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament join payload: %w", err)
		}
		if strings.TrimSpace(p.Player) == "" {
			p.Player = tx.From
		}
		out.TournamentJoin = &p
	case DTLTxTournamentReveal:
		var p DTLTournamentRevealTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament reveal payload: %w", err)
		}
		if strings.TrimSpace(p.Player) == "" {
			p.Player = tx.From
		}
		out.TournamentReveal = &p
	case DTLTxTournamentFinalize:
		var p DTLTournamentFinalizeTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid tournament finalize payload: %w", err)
		}
		if strings.TrimSpace(p.Caller) == "" {
			p.Caller = tx.From
		}
		out.TournamentFinalize = &p
	case DTLTxSeasonCreate:
		var p DTLSeasonCreateTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid season create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.SeasonCreate = &p
	case DTLTxSeasonFinalize:
		var p DTLSeasonFinalizeTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid season finalize payload: %w", err)
		}
		if strings.TrimSpace(p.Caller) == "" {
			p.Caller = tx.From
		}
		out.SeasonFinalize = &p
	case DTLTxSeasonClaim:
		var p DTLSeasonClaimTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid season claim payload: %w", err)
		}
		if strings.TrimSpace(p.Account) == "" {
			p.Account = tx.From
		}
		out.SeasonClaim = &p
	case DTLTxOracleFeedCreate:
		var p DTLOracleFeedCreateTx
		if err := json.Unmarshal([]byte(rawPayload), &p); err != nil {
			return out, fmt.Errorf("dtl: invalid oracle feed create payload: %w", err)
		}
		if strings.TrimSpace(p.Creator) == "" {
			p.Creator = tx.From
		}
		out.OracleFeedCreate = &p
	case DTLTxOraclePriceSubmit:
		var p DTLOraclePriceSubmitTx
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

func validateDTLTransaction(ledger *Ledger, tx Transaction, currentEpoch uint64) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)

	decoded, err := decodeDTLTransaction(tx)
	if err != nil {
		return err
	}

	switch decoded.Kind {
	case DTLTxTokenCreate:
		return ValidateDTLCreateTx(ledger.DTL, *decoded.Create)
	case DTLTxTokenTransfer:
		return ValidateDTLTransferTx(ledger.DTL, *decoded.Transfer)
	case DTLTxTokenApprove:
		return ValidateDTLApproveTx(ledger.DTL, *decoded.Approve)
	case DTLTxTokenTransferFrom:
		return ValidateDTLTransferFromTx(ledger.DTL, *decoded.TransferFrom)
	case DTLTxTokenMint:
		if err := ValidateDTLMintTx(ledger.DTL, *decoded.Mint); err != nil {
			return err
		}
		tokenID := normalizeDTLTokenID(decoded.Mint.TokenID)
		token := ledger.DTL.Tokens[tokenID]
		if token == nil {
			return ErrDTLUnknownToken
		}
		payloadHash, err := DTLPayloadHash(struct {
			TokenID string `json:"token_id"`
			To      string `json:"to"`
			Amount  uint64 `json:"amount"`
		}{
			TokenID: tokenID,
			To:      normalizeDTLAccount(decoded.Mint.To),
			Amount:  decoded.Mint.Amount,
		})
		if err != nil {
			return err
		}
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
		replayKey := dtlReplayKey(*decoded.MintCert)
		if lastEpoch, exists := ledger.DTL.GovernanceReplay[replayKey]; exists && decoded.MintCert.Epoch <= lastEpoch {
			return ErrDTLReplay
		}
		return nil
	case DTLTxTokenBurn:
		return ValidateDTLBurnTx(ledger.DTL, *decoded.Burn)
	case DTLTxNFT721Create:
		return ValidateDTLNFT721CreateTx(ledger.DTL, *decoded.NFT721Create)
	case DTLTxNFT721Mint:
		return ValidateDTLNFT721MintTx(ledger.DTL, *decoded.NFT721Mint)
	case DTLTxNFT721Transfer:
		return ValidateDTLNFT721TransferTx(ledger.DTL, *decoded.NFT721Transfer)
	case DTLTxNFT1155Create:
		return ValidateDTLNFT1155CreateTx(ledger.DTL, *decoded.NFT1155Create)
	case DTLTxNFT1155Mint:
		return ValidateDTLNFT1155MintTx(ledger.DTL, *decoded.NFT1155Mint)
	case DTLTxNFT1155Transfer:
		return ValidateDTLNFT1155TransferTx(ledger.DTL, *decoded.NFT1155Transfer)
	case DTLTxPoolCreate:
		return ValidateDTLPoolCreateTx(ledger.DTL, *decoded.PoolCreate)
	case DTLTxPoolAdd:
		return ValidateDTLPoolAddLiquidityTx(ledger.DTL, *decoded.PoolAdd)
	case DTLTxPoolRemove:
		return ValidateDTLPoolRemoveLiquidityTx(ledger.DTL, *decoded.PoolRemove)
	case DTLTxPoolSwap:
		return ValidateDTLPoolSwapTx(ledger.DTL, *decoded.PoolSwap)
	case DTLTxPoolSwapRoute:
		return ValidateDTLPoolSwapRouteTx(ledger.DTL, *decoded.PoolSwapRoute, currentEpoch)
	case DTLTxFarmCreate:
		return ValidateDTLFarmCreateTx(ledger.DTL, *decoded.FarmCreate)
	case DTLTxFarmStakeLP:
		return ValidateDTLFarmStakeLPTx(ledger.DTL, *decoded.FarmStakeLP)
	case DTLTxFarmUnstakeLP:
		return ValidateDTLFarmUnstakeLPTx(ledger.DTL, *decoded.FarmUnstakeLP)
	case DTLTxFarmClaim:
		return ValidateDTLFarmClaimTx(ledger.DTL, *decoded.FarmClaim)
	case DTLTxDuelCreate:
		return ValidateDTLDuelCreateTx(ledger.DTL, *decoded.DuelCreate, currentEpoch)
	case DTLTxDuelJoin:
		return ValidateDTLDuelJoinTx(ledger.DTL, *decoded.DuelJoin, currentEpoch)
	case DTLTxDuelReveal:
		return ValidateDTLDuelRevealTx(ledger.DTL, *decoded.DuelReveal, currentEpoch)
	case DTLTxDuelFinalize:
		return ValidateDTLDuelFinalizeTx(ledger.DTL, *decoded.DuelFinal, currentEpoch)
	case DTLTxLendMarketCreate:
		return ValidateDTLLendMarketCreateTx(ledger.DTL, *decoded.LendMarketCreate)
	case DTLTxLendDeposit:
		return ValidateDTLLendDepositCollateralTx(ledger.DTL, *decoded.LendDeposit)
	case DTLTxLendBorrow:
		return ValidateDTLLendBorrowTx(ledger.DTL, *decoded.LendBorrow)
	case DTLTxLendRepay:
		return ValidateDTLLendRepayTx(ledger.DTL, *decoded.LendRepay)
	case DTLTxLendWithdraw:
		return ValidateDTLLendWithdrawCollateralTx(ledger.DTL, *decoded.LendWithdraw)
	case DTLTxLendLiquidate:
		return ValidateDTLLendLiquidateTx(ledger.DTL, *decoded.LendLiquidate, currentEpoch)
	case DTLTxTournamentCreate:
		return ValidateDTLTournamentCreateTx(ledger.DTL, *decoded.TournamentCreate, currentEpoch)
	case DTLTxTournamentJoin:
		return ValidateDTLTournamentJoinTx(ledger.DTL, *decoded.TournamentJoin, currentEpoch)
	case DTLTxTournamentReveal:
		return ValidateDTLTournamentRevealTx(ledger.DTL, *decoded.TournamentReveal, currentEpoch)
	case DTLTxTournamentFinalize:
		return ValidateDTLTournamentFinalizeTx(ledger.DTL, *decoded.TournamentFinalize, currentEpoch)
	case DTLTxSeasonCreate:
		return ValidateDTLSeasonCreateTx(ledger.DTL, *decoded.SeasonCreate, currentEpoch)
	case DTLTxSeasonFinalize:
		return ValidateDTLSeasonFinalizeTx(ledger.DTL, *decoded.SeasonFinalize, currentEpoch)
	case DTLTxSeasonClaim:
		return ValidateDTLSeasonClaimTx(ledger.DTL, *decoded.SeasonClaim, currentEpoch)
	case DTLTxOracleFeedCreate:
		if !dtlV2EnabledAtHeight(currentEpoch) {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", currentEpoch)
		}
		return ValidateDTLOracleFeedCreateTx(ledger.DTL, *decoded.OracleFeedCreate)
	case DTLTxOraclePriceSubmit:
		if !dtlV2EnabledAtHeight(currentEpoch) {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", currentEpoch)
		}
		return ValidateDTLOraclePriceSubmitTx(ledger.DTL, *decoded.OraclePriceSubmit, currentEpoch)
	case DTLTxContractDeploy:
		return dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	case DTLTxContractCall:
		return dtlContractRuntimeRemovedError("CONTRACT_CALL")
	default:
		return fmt.Errorf("dtl: unsupported tx type")
	}
}

func applyDTLTransaction(ledger *Ledger, tx Transaction, height int) error {
	if ledger == nil {
		return ErrDTLInvalidState
	}
	ensureDTLState(ledger)

	decoded, err := decodeDTLTransaction(tx)
	if err != nil {
		return err
	}

	switch decoded.Kind {
	case DTLTxTokenCreate:
		_, err = ApplyDTLCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), *decoded.Create)
		return err
	case DTLTxTokenTransfer:
		return ApplyDTLTransferTx(ledger.DTL, *decoded.Transfer)
	case DTLTxTokenApprove:
		return ApplyDTLApproveTx(ledger.DTL, *decoded.Approve)
	case DTLTxTokenTransferFrom:
		return ApplyDTLTransferFromTx(ledger.DTL, *decoded.TransferFrom)
	case DTLTxTokenMint:
		return ApplyDTLMintTx(
			ledger.DTL,
			*decoded.Mint,
			*decoded.MintCert,
			uint64(height),
			DTLDefaultReplayWindow,
		)
	case DTLTxTokenBurn:
		return ApplyDTLBurnTx(ledger.DTL, *decoded.Burn)
	case DTLTxNFT721Create:
		_, err = ApplyDTLNFT721CreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), *decoded.NFT721Create)
		return err
	case DTLTxNFT721Mint:
		_, err = ApplyDTLNFT721MintTx(ledger.DTL, *decoded.NFT721Mint)
		return err
	case DTLTxNFT721Transfer:
		return ApplyDTLNFT721TransferTx(ledger.DTL, *decoded.NFT721Transfer)
	case DTLTxNFT1155Create:
		_, err = ApplyDTLNFT1155CreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), *decoded.NFT1155Create)
		return err
	case DTLTxNFT1155Mint:
		return ApplyDTLNFT1155MintTx(ledger.DTL, *decoded.NFT1155Mint)
	case DTLTxNFT1155Transfer:
		return ApplyDTLNFT1155TransferTx(ledger.DTL, *decoded.NFT1155Transfer)
	case DTLTxPoolCreate:
		_, err = ApplyDTLPoolCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), *decoded.PoolCreate)
		return err
	case DTLTxPoolAdd:
		return ApplyDTLPoolAddLiquidityTx(ledger.DTL, *decoded.PoolAdd)
	case DTLTxPoolRemove:
		return ApplyDTLPoolRemoveLiquidityTx(ledger.DTL, *decoded.PoolRemove)
	case DTLTxPoolSwap:
		_, err = ApplyDTLPoolSwapTx(ledger.DTL, *decoded.PoolSwap)
		return err
	case DTLTxPoolSwapRoute:
		_, err = ApplyDTLPoolSwapRouteTx(ledger.DTL, *decoded.PoolSwapRoute, uint64(height))
		return err
	case DTLTxFarmCreate:
		_, err = ApplyDTLFarmCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), uint64(height), *decoded.FarmCreate)
		return err
	case DTLTxFarmStakeLP:
		return ApplyDTLFarmStakeLPTx(ledger.DTL, uint64(height), *decoded.FarmStakeLP)
	case DTLTxFarmUnstakeLP:
		return ApplyDTLFarmUnstakeLPTx(ledger.DTL, uint64(height), *decoded.FarmUnstakeLP)
	case DTLTxFarmClaim:
		_, err = ApplyDTLFarmClaimTx(ledger.DTL, uint64(height), *decoded.FarmClaim)
		return err
	case DTLTxDuelCreate:
		_, err = ApplyDTLDuelCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), uint64(height), *decoded.DuelCreate)
		return err
	case DTLTxDuelJoin:
		return ApplyDTLDuelJoinTx(ledger.DTL, uint64(height), *decoded.DuelJoin)
	case DTLTxDuelReveal:
		return ApplyDTLDuelRevealTx(ledger.DTL, uint64(height), *decoded.DuelReveal)
	case DTLTxDuelFinalize:
		return ApplyDTLDuelFinalizeTx(ledger.DTL, uint64(height), *decoded.DuelFinal)
	case DTLTxLendMarketCreate:
		_, err = ApplyDTLLendMarketCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), *decoded.LendMarketCreate)
		return err
	case DTLTxLendDeposit:
		return ApplyDTLLendDepositCollateralTxWithHeight(ledger.DTL, uint64(height), *decoded.LendDeposit)
	case DTLTxLendBorrow:
		return ApplyDTLLendBorrowTxWithHeight(ledger.DTL, uint64(height), *decoded.LendBorrow)
	case DTLTxLendRepay:
		return ApplyDTLLendRepayTxWithHeight(ledger.DTL, uint64(height), *decoded.LendRepay)
	case DTLTxLendWithdraw:
		return ApplyDTLLendWithdrawCollateralTxWithHeight(ledger.DTL, uint64(height), *decoded.LendWithdraw)
	case DTLTxLendLiquidate:
		return ApplyDTLLendLiquidateTxWithHeight(ledger.DTL, uint64(height), *decoded.LendLiquidate)
	case DTLTxTournamentCreate:
		_, err = ApplyDTLTournamentCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), uint64(height), *decoded.TournamentCreate)
		return err
	case DTLTxTournamentJoin:
		return ApplyDTLTournamentJoinTx(ledger.DTL, uint64(height), *decoded.TournamentJoin)
	case DTLTxTournamentReveal:
		return ApplyDTLTournamentRevealTx(ledger.DTL, uint64(height), *decoded.TournamentReveal)
	case DTLTxTournamentFinalize:
		return ApplyDTLTournamentFinalizeTx(ledger.DTL, uint64(height), *decoded.TournamentFinalize)
	case DTLTxSeasonCreate:
		_, err = ApplyDTLSeasonCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), uint64(height), *decoded.SeasonCreate)
		return err
	case DTLTxSeasonFinalize:
		return ApplyDTLSeasonFinalizeTx(ledger.DTL, uint64(height), *decoded.SeasonFinalize)
	case DTLTxSeasonClaim:
		_, err = ApplyDTLSeasonClaimTx(ledger.DTL, uint64(height), *decoded.SeasonClaim)
		return err
	case DTLTxOracleFeedCreate:
		if !dtlV2EnabledAtHeight(uint64(height)) {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", uint64(height))
		}
		_, err = ApplyDTLOracleFeedCreateTx(ledger.DTL, tx.ChainID, uint64(tx.Nonce), *decoded.OracleFeedCreate)
		return err
	case DTLTxOraclePriceSubmit:
		if !dtlV2EnabledAtHeight(uint64(height)) {
			return fmt.Errorf("dtl: oracle v2 not active at height %d", uint64(height))
		}
		return ApplyDTLOraclePriceSubmitTx(ledger.DTL, uint64(height), *decoded.OraclePriceSubmit)
	case DTLTxContractDeploy:
		return dtlContractRuntimeRemovedError("CONTRACT_DEPLOY")
	case DTLTxContractCall:
		return dtlContractRuntimeRemovedError("CONTRACT_CALL")
	default:
		return fmt.Errorf("dtl: unsupported tx kind")
	}
}
