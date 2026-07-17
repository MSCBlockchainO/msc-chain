package main

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// dtlResolveGameFiRewardTokenID implements the dtl resolve game fi reward token id helper.
func dtlResolveGameFiRewardTokenID(state *DTLState) (string, bool) {
	if state == nil {
		return "", false
	}
	state.ensure()
	// `ref` stores the value produced by this operation.
	ref := strings.TrimSpace(dtlProtocolGameFiRewardTokenRef())
	if ref == "" {
		return "", false
	}
	// `tokenID` and `ok` store whether the related condition is satisfied.
	if tokenID, ok := resolveDTLTokenRef(state, ref); ok {
		return tokenID, true
	}
	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(ref)
	if tokenID == "" {
		return "", false
	}
	if state.Tokens[tokenID] == nil {
		return "", false
	}
	return tokenID, true
}

// dtlResolveActiveSeason implements the dtl resolve active season helper.
func dtlResolveActiveSeason(state *DTLState, currentHeight uint64) (string, *DTLSeasonState) {
	if state == nil {
		return "", nil
	}
	state.ensure()
	if len(state.Seasons) == 0 {
		return "", nil
	}
	// `seasonIDs` stores the value produced by this operation.
	seasonIDs := make([]string, 0, len(state.Seasons))
	// `seasonID` tracks the current values while iterating.
	for seasonID := range state.Seasons {
		seasonIDs = append(seasonIDs, normalizeDTLSeasonID(seasonID))
	}
	sort.Strings(seasonIDs)
	// `seasonID` tracks the current values while iterating.
	for _, seasonID := range seasonIDs {
		// `season` stores the value produced by this operation.
		season := state.Seasons[seasonID]
		if season == nil || season.Finalized {
			continue
		}
		if currentHeight < season.StartHeight || currentHeight > season.EndHeight {
			continue
		}
		return seasonID, season
	}
	return "", nil
}

// dtlActiveSeasonSummary implements the dtl active season summary helper.
func dtlActiveSeasonSummary(state *DTLState, currentHeight uint64) (string, uint64) {
	// `seasonID` and `season` store the value produced by this operation.
	seasonID, season := dtlResolveActiveSeason(state, currentHeight)
	if season == nil {
		return "", 0
	}
	return seasonID, season.EndHeight
}

// dtlAddSeasonVaultBalance implements the dtl add season vault balance helper.
func dtlAddSeasonVaultBalance(state *DTLState, seasonID string, amount uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if amount == 0 {
		return nil
	}
	seasonID = normalizeDTLSeasonID(seasonID)
	if seasonID == "" {
		return fmt.Errorf("dtl: invalid season id")
	}
	// `current` stores the value produced by this operation.
	current := state.SeasonVaults[seasonID]
	// `next` and `err` store the error produced by this operation.
	next, err := dtlSafeAddU64(current, amount)
	if err != nil {
		return err
	}
	state.SeasonVaults[seasonID] = next
	return nil
}

// dtlActiveRewardSeasonID implements the dtl active reward season id helper.
func dtlActiveRewardSeasonID(state *DTLState, currentHeight uint64, tokenID string) (string, bool) {
	if state == nil || !dtlProtocolGameFiSeasonEnabled() {
		return "", false
	}
	// `rewardTokenID` and `ok` store whether the related condition is satisfied.
	rewardTokenID, ok := dtlResolveGameFiRewardTokenID(state)
	if !ok {
		return "", false
	}
	if normalizeDTLTokenID(tokenID) != normalizeDTLTokenID(rewardTokenID) {
		return "", false
	}
	// `seasonID` stores the value produced by this operation.
	seasonID, _ := dtlResolveActiveSeason(state, currentHeight)
	if seasonID == "" {
		return "", false
	}
	return seasonID, true
}

// dtlAddSeasonScore implements the dtl add season score helper.
func dtlAddSeasonScore(state *DTLState, currentHeight uint64, account string, points uint64, source string) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolGameFiSeasonEnabled() || points == 0 {
		return nil
	}
	state.ensure()
	// `seasonID` and `season` store the value produced by this operation.
	seasonID, season := dtlResolveActiveSeason(state, currentHeight)
	if season == nil {
		return nil
	}
	account = normalizeDTLAccount(account)
	if account == "" {
		return nil
	}
	// `scoreKey` stores the key used to access the related value.
	scoreKey := dtlSeasonAccountKey(seasonID, account)
	// `currentScore` stores the value produced by this operation.
	currentScore := state.SeasonScores[scoreKey]
	// `nextScore` and `err` store the error produced by this operation.
	nextScore, err := dtlSafeAddU64(currentScore, points)
	if err != nil {
		return err
	}
	state.SeasonScores[scoreKey] = nextScore
	season.TotalScore, err = dtlSafeAddU64(season.TotalScore, points)
	if err != nil {
		return err
	}
	state.Events = append(state.Events, fmt.Sprintf("SEASON_SCORE_UPDATE:%s:%s:%d:%s", seasonID, account, points, strings.TrimSpace(source)))
	dtlAppendStructuredEventLog(state, []string{"SEASON_SCORE_UPDATE"}, map[string]any{
		"season_id": seasonID,
		"account":   account,
		"points":    points,
		"source":    strings.TrimSpace(source),
	})
	return nil
}

// getOrCreateDTLFarmPosition implements the get or create dtl farm position helper.
func getOrCreateDTLFarmPosition(state *DTLState, farmID, account string, currentHeight uint64) *DTLFarmPositionState {
	// `key` stores the key used to access the related value.
	key := dtlFarmPositionKey(farmID, account)
	// `existing` stores the value produced by this operation.
	if existing := state.FarmPositions[key]; existing != nil {
		return existing
	}
	// `pos` stores the value produced by this operation.
	pos := &DTLFarmPositionState{
		FarmID:            normalizeDTLFarmID(farmID),
		Account:           normalizeDTLAccount(account),
		LastAccrualHeight: currentHeight,
	}
	state.FarmPositions[key] = pos
	return pos
}

// dtlAccrueFarmPosition implements the dtl accrue farm position helper.
func dtlAccrueFarmPosition(state *DTLState, farm *DTLFarmPoolState, pos *DTLFarmPositionState, currentHeight uint64) error {
	if state == nil || farm == nil || pos == nil {
		return ErrDTLInvalidState
	}
	if pos.LastAccrualHeight == 0 {
		pos.LastAccrualHeight = currentHeight
		return nil
	}
	if currentHeight <= pos.LastAccrualHeight {
		return nil
	}
	// `blocks` stores the block data handled by this operation.
	blocks := currentHeight - pos.LastAccrualHeight
	pos.LastAccrualHeight = currentHeight
	if pos.StakedLP == 0 || blocks == 0 {
		return nil
	}
	// `steps` stores the value produced by this operation.
	steps := new(big.Int).SetUint64(pos.StakedLP)
	steps.Mul(steps, new(big.Int).SetUint64(dtlProtocolFarmLPPointsPerBlock()))
	steps.Mul(steps, new(big.Int).SetUint64(blocks))
	steps.Mul(steps, new(big.Int).SetUint64(uint64(farm.MultiplierBPS)))
	steps.Div(steps, new(big.Int).SetUint64(DTLMaxTaxBPS))
	if steps.Sign() <= 0 {
		return nil
	}
	if steps.BitLen() > 64 {
		return fmt.Errorf("dtl: farm points overflow")
	}
	// `earned` stores the value produced by this operation.
	earned := steps.Uint64()
	// `nextAccrued` and `err` store the error produced by this operation.
	nextAccrued, err := dtlSafeAddU64(pos.AccruedPoints, earned)
	if err != nil {
		return err
	}
	pos.AccruedPoints = nextAccrued
	if farm.LastUpdateHeight < currentHeight {
		farm.LastUpdateHeight = currentHeight
	}
	return nil
}

// ValidateDTLFarmCreateTx validates dtl farm create tx.
func ValidateDTLFarmCreateTx(state *DTLState, tx DTLFarmCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid farm creator")
	}
	// `poolID` stores the value produced by this operation.
	poolID := normalizeDTLPoolID(tx.PoolID)
	if poolID == "" {
		return fmt.Errorf("dtl: invalid farm pool_id")
	}
	if state.Pools[poolID] == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	if farmID != "" && state.FarmPools[farmID] != nil {
		return fmt.Errorf("dtl: farm already exists")
	}
	// `multiplier` stores the synchronization state protecting shared data.
	multiplier := tx.MultiplierBPS
	if multiplier == 0 {
		multiplier = DTLMaxTaxBPS
	}
	if multiplier > dtlProtocolFarmMaxMultiplierBPS() {
		return fmt.Errorf("dtl: farm multiplier exceeds max")
	}
	return nil
}

// ValidateDTLFarmStakeLPTx validates dtl farm stake lp tx.
func ValidateDTLFarmStakeLPTx(state *DTLState, tx DTLFarmStakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid farm account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: farm stake amount must be > 0")
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	// `farm` stores the value produced by this operation.
	farm := state.FarmPools[farmID]
	if farm == nil {
		return ErrDTLUnknownFarm
	}
	if !farm.Active {
		return fmt.Errorf("dtl: farm is not active")
	}
	if state.LPBalanceOf(farm.PoolID, account) < tx.Amount {
		return fmt.Errorf("dtl: insufficient LP balance")
	}
	return nil
}

// ValidateDTLFarmUnstakeLPTx validates dtl farm unstake lp tx.
func ValidateDTLFarmUnstakeLPTx(state *DTLState, tx DTLFarmUnstakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid farm account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: farm unstake amount must be > 0")
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	// `farm` stores the value produced by this operation.
	farm := state.FarmPools[farmID]
	if farm == nil {
		return ErrDTLUnknownFarm
	}
	// `pos` stores the value produced by this operation.
	pos := state.FarmPositions[dtlFarmPositionKey(farmID, account)]
	if pos == nil || pos.StakedLP < tx.Amount {
		return fmt.Errorf("dtl: insufficient farm stake")
	}
	return nil
}

// ValidateDTLFarmClaimTx validates dtl farm claim tx.
func ValidateDTLFarmClaimTx(state *DTLState, tx DTLFarmClaimTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid farm account")
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	if state.FarmPools[farmID] == nil {
		return ErrDTLUnknownFarm
	}
	if state.FarmPositions[dtlFarmPositionKey(farmID, account)] == nil {
		return fmt.Errorf("dtl: no farm position")
	}
	return nil
}

// ValidateDTLSeasonCreateTx validates dtl season create tx.
func ValidateDTLSeasonCreateTx(state *DTLState, tx DTLSeasonCreateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolGameFiSeasonEnabled() {
		return fmt.Errorf("dtl: season is disabled")
	}
	state.ensure()
	// `creator` stores the value produced by this operation.
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid season creator")
	}
	// `start` stores the value produced by this operation.
	start := tx.StartHeight
	if start == 0 {
		start = currentHeight
	}
	// `seasonLen` stores the measured quantity used by this operation.
	seasonLen := dtlProtocolGameFiSeasonLengthBlocks()
	if seasonLen == 0 {
		return fmt.Errorf("dtl: season length must be > 0")
	}
	if start > ^uint64(0)-seasonLen {
		return fmt.Errorf("dtl: season end overflow")
	}
	// `end` stores the value produced by this operation.
	end := start + seasonLen
	// `grace` stores the value produced by this operation.
	grace := dtlProtocolGameFiClaimGraceBlocks()
	if end > ^uint64(0)-grace {
		return fmt.Errorf("dtl: season grace overflow")
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	if seasonID != "" && state.Seasons[seasonID] != nil {
		return fmt.Errorf("dtl: season already exists")
	}
	// `existingIDs` stores the value produced by this operation.
	existingIDs := make([]string, 0, len(state.Seasons))
	// `existingID` tracks the current values while iterating.
	for existingID := range state.Seasons {
		existingIDs = append(existingIDs, normalizeDTLSeasonID(existingID))
	}
	sort.Strings(existingIDs)
	// `existingID` tracks the current values while iterating.
	for _, existingID := range existingIDs {
		// `season` stores the value produced by this operation.
		season := state.Seasons[existingID]
		if season == nil {
			continue
		}
		if end < season.StartHeight || start > season.EndHeight {
			continue
		}
		return fmt.Errorf("dtl: season range overlaps existing season %s", normalizeDTLSeasonID(existingID))
	}
	return nil
}

// ValidateDTLSeasonFinalizeTx validates dtl season finalize tx.
func ValidateDTLSeasonFinalizeTx(state *DTLState, tx DTLSeasonFinalizeTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolGameFiSeasonEnabled() {
		return fmt.Errorf("dtl: season is disabled")
	}
	state.ensure()
	// `caller` stores the value produced by this operation.
	caller := normalizeDTLAccount(tx.Caller)
	if caller == "" {
		return fmt.Errorf("dtl: invalid season caller")
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	// `season` stores the value produced by this operation.
	season := state.Seasons[seasonID]
	if season == nil {
		return ErrDTLUnknownSeason
	}
	if season.Finalized {
		return fmt.Errorf("dtl: season already finalized")
	}
	if currentHeight < season.EndHeight {
		return fmt.Errorf("dtl: season not ended")
	}
	if season.ClaimGraceEndHeight > 0 && currentHeight > season.ClaimGraceEndHeight {
		return fmt.Errorf("dtl: season finalize window expired")
	}
	return nil
}

// ValidateDTLSeasonClaimTx validates dtl season claim tx.
func ValidateDTLSeasonClaimTx(state *DTLState, tx DTLSeasonClaimTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlProtocolGameFiSeasonEnabled() {
		return fmt.Errorf("dtl: season is disabled")
	}
	state.ensure()
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid season account")
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	// `season` stores the value produced by this operation.
	season := state.Seasons[seasonID]
	if season == nil {
		return ErrDTLUnknownSeason
	}
	if !season.Finalized {
		return fmt.Errorf("dtl: season is not finalized")
	}
	if season.ClaimGraceEndHeight > 0 && currentHeight > season.ClaimGraceEndHeight {
		return fmt.Errorf("dtl: season claim window expired")
	}
	// `key` stores the key used to access the related value.
	key := dtlSeasonAccountKey(seasonID, account)
	if state.SeasonClaims[key] {
		return fmt.Errorf("dtl: season reward already claimed")
	}
	if state.SeasonScores[key] == 0 {
		return fmt.Errorf("dtl: season score is zero")
	}
	return nil
}

// ApplyDTLFarmCreateTx applies dtl farm create tx.
func ApplyDTLFarmCreateTx(state *DTLState, chainID string, nonce uint64, currentHeight uint64, tx DTLFarmCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLFarmCreateTx(state, tx); err != nil {
		return "", err
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	if farmID == "" {
		farmID = normalizeDTLFarmID(DTLFarmIDFromCreate(chainID, nonce, tx))
	}
	if state.FarmPools[farmID] != nil {
		return "", fmt.Errorf("dtl: farm already exists")
	}
	// `multiplier` stores the synchronization state protecting shared data.
	multiplier := tx.MultiplierBPS
	if multiplier == 0 {
		multiplier = DTLMaxTaxBPS
	}
	state.FarmPools[farmID] = &DTLFarmPoolState{
		FarmID:           farmID,
		PoolID:           normalizeDTLPoolID(tx.PoolID),
		Creator:          normalizeDTLAccount(tx.Creator),
		MultiplierBPS:    multiplier,
		CreatedHeight:    currentHeight,
		LastUpdateHeight: currentHeight,
		Active:           true,
	}
	state.Events = append(state.Events, fmt.Sprintf("FARM_CREATE:%s:%s", farmID, normalizeDTLPoolID(tx.PoolID)))
	return farmID, nil
}

// ApplyDTLFarmStakeLPTx applies dtl farm stake lp tx.
func ApplyDTLFarmStakeLPTx(state *DTLState, currentHeight uint64, tx DTLFarmStakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLFarmStakeLPTx(state, tx); err != nil {
		return err
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `farm` stores the value produced by this operation.
	farm := state.FarmPools[farmID]
	// `pos` stores the value produced by this operation.
	pos := getOrCreateDTLFarmPosition(state, farmID, account, currentHeight)
	// `err` stores the error produced by this operation.
	if err := dtlAccrueFarmPosition(state, farm, pos, currentHeight); err != nil {
		return err
	}
	// `lpAccountKey` stores the key used to access the related value.
	lpAccountKey := dtlLPBalanceKey(farm.PoolID, account)
	if state.LPBalances[lpAccountKey] < tx.Amount {
		return fmt.Errorf("dtl: insufficient LP balance")
	}
	state.LPBalances[lpAccountKey] -= tx.Amount
	if state.LPBalances[lpAccountKey] == 0 {
		delete(state.LPBalances, lpAccountKey)
	}
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(farm.PoolID, dtlFarmVaultAccount(farmID)), tx.Amount); err != nil {
		return err
	}
	// `nextStaked` and `err` store the error produced by this operation.
	nextStaked, err := dtlSafeAddU64(pos.StakedLP, tx.Amount)
	if err != nil {
		return err
	}
	pos.StakedLP = nextStaked
	if pos.LastStakeHeight == 0 {
		pos.LastStakeHeight = currentHeight
	}
	pos.LastAccrualHeight = currentHeight
	state.Events = append(state.Events, fmt.Sprintf("FARM_STAKE:%s:%s:%d", farmID, account, tx.Amount))
	dtlAppendStructuredEventLog(state, []string{"FARM_STAKE"}, map[string]any{
		"farm_id":   farmID,
		"account":   account,
		"amount":    tx.Amount,
		"staked_lp": pos.StakedLP,
	})
	return nil
}

// ApplyDTLFarmUnstakeLPTx applies dtl farm unstake lp tx.
func ApplyDTLFarmUnstakeLPTx(state *DTLState, currentHeight uint64, tx DTLFarmUnstakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLFarmUnstakeLPTx(state, tx); err != nil {
		return err
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `farm` stores the value produced by this operation.
	farm := state.FarmPools[farmID]
	// `posKey` stores the key used to access the related value.
	posKey := dtlFarmPositionKey(farmID, account)
	// `pos` stores the value produced by this operation.
	pos := state.FarmPositions[posKey]
	if pos == nil {
		return fmt.Errorf("dtl: no farm position")
	}
	// `err` stores the error produced by this operation.
	if err := dtlAccrueFarmPosition(state, farm, pos, currentHeight); err != nil {
		return err
	}
	if pos.StakedLP < tx.Amount {
		return fmt.Errorf("dtl: insufficient farm stake")
	}
	// `minStake` stores the value produced by this operation.
	if minStake := dtlProtocolFarmMinStakeBlocks(); minStake > 0 && pos.LastStakeHeight > 0 {
		if currentHeight < pos.LastStakeHeight+minStake {
			pos.AccruedPoints = 0
		}
	}
	pos.StakedLP -= tx.Amount
	// `lpVaultKey` stores the key used to access the related value.
	lpVaultKey := dtlLPBalanceKey(farm.PoolID, dtlFarmVaultAccount(farmID))
	if state.LPBalances[lpVaultKey] < tx.Amount {
		return fmt.Errorf("dtl: farm vault insufficient LP")
	}
	state.LPBalances[lpVaultKey] -= tx.Amount
	if state.LPBalances[lpVaultKey] == 0 {
		delete(state.LPBalances, lpVaultKey)
	}
	// `err` stores the error produced by this operation.
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(farm.PoolID, account), tx.Amount); err != nil {
		return err
	}
	if pos.StakedLP == 0 && pos.AccruedPoints == 0 {
		delete(state.FarmPositions, posKey)
	}
	state.Events = append(state.Events, fmt.Sprintf("FARM_UNSTAKE:%s:%s:%d", farmID, account, tx.Amount))
	dtlAppendStructuredEventLog(state, []string{"FARM_UNSTAKE"}, map[string]any{
		"farm_id":    farmID,
		"account":    account,
		"amount":     tx.Amount,
		"staked_lp":  pos.StakedLP,
		"accrued_pt": pos.AccruedPoints,
	})
	return nil
}

// ApplyDTLFarmClaimTx applies dtl farm claim tx.
func ApplyDTLFarmClaimTx(state *DTLState, currentHeight uint64, tx DTLFarmClaimTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLFarmClaimTx(state, tx); err != nil {
		return 0, err
	}
	// `farmID` stores the value produced by this operation.
	farmID := normalizeDTLFarmID(tx.FarmID)
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `farm` stores the value produced by this operation.
	farm := state.FarmPools[farmID]
	// `posKey` stores the key used to access the related value.
	posKey := dtlFarmPositionKey(farmID, account)
	// `pos` stores the value produced by this operation.
	pos := state.FarmPositions[posKey]
	if pos == nil {
		return 0, fmt.Errorf("dtl: no farm position")
	}
	// `err` stores the error produced by this operation.
	if err := dtlAccrueFarmPosition(state, farm, pos, currentHeight); err != nil {
		return 0, err
	}
	if pos.AccruedPoints == 0 {
		return 0, fmt.Errorf("dtl: no claimable farm points")
	}
	// `points` stores the value produced by this operation.
	points := pos.AccruedPoints
	pos.AccruedPoints = 0
	// `err` stores the error produced by this operation.
	if err := dtlAddSeasonScore(state, currentHeight, account, points, "farm_claim"); err != nil {
		return 0, err
	}
	if pos.StakedLP == 0 {
		delete(state.FarmPositions, posKey)
	}
	state.Events = append(state.Events, fmt.Sprintf("FARM_CLAIM:%s:%s:%d", farmID, account, points))
	dtlAppendStructuredEventLog(state, []string{"FARM_CLAIM"}, map[string]any{
		"farm_id": farmID,
		"account": account,
		"points":  points,
	})
	return points, nil
}

// ApplyDTLSeasonCreateTx applies dtl season create tx.
func ApplyDTLSeasonCreateTx(state *DTLState, chainID string, nonce uint64, currentHeight uint64, tx DTLSeasonCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLSeasonCreateTx(state, tx, currentHeight); err != nil {
		return "", err
	}
	// `start` stores the value produced by this operation.
	start := tx.StartHeight
	if start == 0 {
		start = currentHeight
	}
	// `end` stores the value produced by this operation.
	end := start + dtlProtocolGameFiSeasonLengthBlocks()
	// `graceEnd` stores the value produced by this operation.
	graceEnd := end + dtlProtocolGameFiClaimGraceBlocks()
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	if seasonID == "" {
		seasonID = normalizeDTLSeasonID(DTLSeasonIDFromCreate(chainID, nonce, DTLSeasonCreateTx{
			Creator:     tx.Creator,
			SeasonID:    tx.SeasonID,
			StartHeight: start,
		}))
	}
	if state.Seasons[seasonID] != nil {
		return "", fmt.Errorf("dtl: season already exists")
	}
	// `rewardToken` stores the value produced by this operation.
	rewardToken := normalizeDTLTokenID(strings.TrimSpace(dtlProtocolGameFiRewardTokenRef()))
	// `resolved` and `ok` store whether the related condition is satisfied.
	if resolved, ok := dtlResolveGameFiRewardTokenID(state); ok {
		rewardToken = normalizeDTLTokenID(resolved)
	}
	state.Seasons[seasonID] = &DTLSeasonState{
		SeasonID:            seasonID,
		Creator:             normalizeDTLAccount(tx.Creator),
		RewardToken:         rewardToken,
		StartHeight:         start,
		EndHeight:           end,
		ClaimGraceEndHeight: graceEnd,
		Finalized:           false,
	}
	state.Events = append(state.Events, fmt.Sprintf("SEASON_CREATE:%s:%d:%d", seasonID, start, end))
	return seasonID, nil
}

// ApplyDTLSeasonFinalizeTx applies dtl season finalize tx.
func ApplyDTLSeasonFinalizeTx(state *DTLState, currentHeight uint64, tx DTLSeasonFinalizeTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLSeasonFinalizeTx(state, tx, currentHeight); err != nil {
		return err
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	// `season` stores the value produced by this operation.
	season := state.Seasons[seasonID]
	season.Finalized = true
	season.FinalizedHeight = currentHeight
	state.Events = append(state.Events, fmt.Sprintf("SEASON_FINALIZE:%s", seasonID))
	dtlAppendStructuredEventLog(state, []string{"SEASON_FINALIZE"}, map[string]any{
		"season_id": seasonID,
		"height":    currentHeight,
	})
	return nil
}

// ApplyDTLSeasonClaimTx applies dtl season claim tx.
func ApplyDTLSeasonClaimTx(state *DTLState, currentHeight uint64, tx DTLSeasonClaimTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	// `err` stores the error produced by this operation.
	if err := ValidateDTLSeasonClaimTx(state, tx, currentHeight); err != nil {
		return 0, err
	}
	// `seasonID` stores the value produced by this operation.
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	// `account` stores the measured quantity used by this operation.
	account := normalizeDTLAccount(tx.Account)
	// `season` stores the value produced by this operation.
	season := state.Seasons[seasonID]
	// `scoreKey` stores the key used to access the related value.
	scoreKey := dtlSeasonAccountKey(seasonID, account)
	// `userScore` stores the value produced by this operation.
	userScore := state.SeasonScores[scoreKey]
	if userScore == 0 {
		return 0, fmt.Errorf("dtl: season score is zero")
	}
	// `totalScore` stores the measured quantity used by this operation.
	totalScore := season.TotalScore
	if totalScore == 0 {
		return 0, fmt.Errorf("dtl: season total score is zero")
	}
	// `rewardPool` stores the value produced by this operation.
	rewardPool := state.SeasonVaults[seasonID]
	if rewardPool == 0 {
		return 0, fmt.Errorf("dtl: season reward pool is empty")
	}
	// `reward` and `err` store the error produced by this operation.
	reward, err := dtlMulDivU64(rewardPool, userScore, totalScore)
	if err != nil {
		return 0, err
	}
	// `maxReward` stores the value produced by this operation.
	maxReward := dtlProtocolGameFiMaxRewardPerSeason()
	if maxReward > 0 && reward > maxReward {
		reward = maxReward
	}
	if reward > rewardPool {
		reward = rewardPool
	}
	if reward == 0 {
		return 0, fmt.Errorf("dtl: season reward is zero")
	}
	// `rewardTokenID` and `ok` store whether the related condition is satisfied.
	rewardTokenID, ok := dtlResolveGameFiRewardTokenID(state)
	if !ok {
		return 0, fmt.Errorf("dtl: gamefi reward token unavailable")
	}
	// `err` stores the error produced by this operation.
	if err := dtlMoveBalance(state, rewardTokenID, DTLTreasuryAccount, account, reward); err != nil {
		return 0, err
	}
	state.SeasonVaults[seasonID] -= reward
	state.SeasonClaims[scoreKey] = true
	// `nextClaimed` and `err` store the error produced by this operation.
	nextClaimed, err := dtlSafeAddU64(season.TotalClaimed, reward)
	if err != nil {
		return 0, err
	}
	season.TotalClaimed = nextClaimed
	state.Events = append(state.Events, fmt.Sprintf("SEASON_CLAIM:%s:%s:%d", seasonID, account, reward))
	dtlAppendStructuredEventLog(state, []string{"SEASON_CLAIM"}, map[string]any{
		"season_id": seasonID,
		"account":   account,
		"reward":    reward,
	})
	return reward, nil
}
