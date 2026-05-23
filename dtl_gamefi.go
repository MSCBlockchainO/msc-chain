package main

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
)

func dtlResolveGameFiRewardTokenID(state *DTLState) (string, bool) {
	if state == nil {
		return "", false
	}
	state.ensure()
	ref := strings.TrimSpace(dtlGameFiRewardTokenRef())
	if ref == "" {
		return "", false
	}
	if tokenID, ok := resolveDTLTokenRef(state, ref); ok {
		return tokenID, true
	}
	tokenID := normalizeDTLTokenID(ref)
	if tokenID == "" {
		return "", false
	}
	if state.Tokens[tokenID] == nil {
		return "", false
	}
	return tokenID, true
}

func dtlResolveActiveSeason(state *DTLState, currentHeight uint64) (string, *DTLSeasonState) {
	if state == nil {
		return "", nil
	}
	state.ensure()
	if len(state.Seasons) == 0 {
		return "", nil
	}
	seasonIDs := make([]string, 0, len(state.Seasons))
	for seasonID := range state.Seasons {
		seasonIDs = append(seasonIDs, normalizeDTLSeasonID(seasonID))
	}
	sort.Strings(seasonIDs)
	for _, seasonID := range seasonIDs {
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

func dtlActiveSeasonSummary(state *DTLState, currentHeight uint64) (string, uint64) {
	seasonID, season := dtlResolveActiveSeason(state, currentHeight)
	if season == nil {
		return "", 0
	}
	return seasonID, season.EndHeight
}

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
	current := state.SeasonVaults[seasonID]
	next, err := dtlSafeAddU64(current, amount)
	if err != nil {
		return err
	}
	state.SeasonVaults[seasonID] = next
	return nil
}

func dtlActiveRewardSeasonID(state *DTLState, currentHeight uint64, tokenID string) (string, bool) {
	if state == nil || !dtlGameFiSeasonEnabled() {
		return "", false
	}
	rewardTokenID, ok := dtlResolveGameFiRewardTokenID(state)
	if !ok {
		return "", false
	}
	if normalizeDTLTokenID(tokenID) != normalizeDTLTokenID(rewardTokenID) {
		return "", false
	}
	seasonID, _ := dtlResolveActiveSeason(state, currentHeight)
	if seasonID == "" {
		return "", false
	}
	return seasonID, true
}

func dtlAddSeasonScore(state *DTLState, currentHeight uint64, account string, points uint64, source string) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlGameFiSeasonEnabled() || points == 0 {
		return nil
	}
	state.ensure()
	seasonID, season := dtlResolveActiveSeason(state, currentHeight)
	if season == nil {
		return nil
	}
	account = normalizeDTLAccount(account)
	if account == "" {
		return nil
	}
	scoreKey := dtlSeasonAccountKey(seasonID, account)
	currentScore := state.SeasonScores[scoreKey]
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

func getOrCreateDTLFarmPosition(state *DTLState, farmID, account string, currentHeight uint64) *DTLFarmPositionState {
	key := dtlFarmPositionKey(farmID, account)
	if existing := state.FarmPositions[key]; existing != nil {
		return existing
	}
	pos := &DTLFarmPositionState{
		FarmID:            normalizeDTLFarmID(farmID),
		Account:           normalizeDTLAccount(account),
		LastAccrualHeight: currentHeight,
	}
	state.FarmPositions[key] = pos
	return pos
}

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
	blocks := currentHeight - pos.LastAccrualHeight
	pos.LastAccrualHeight = currentHeight
	if pos.StakedLP == 0 || blocks == 0 {
		return nil
	}
	steps := new(big.Int).SetUint64(pos.StakedLP)
	steps.Mul(steps, new(big.Int).SetUint64(dtlFarmLPPointsPerBlock()))
	steps.Mul(steps, new(big.Int).SetUint64(blocks))
	steps.Mul(steps, new(big.Int).SetUint64(uint64(farm.MultiplierBPS)))
	steps.Div(steps, new(big.Int).SetUint64(DTLMaxTaxBPS))
	if steps.Sign() <= 0 {
		return nil
	}
	if steps.BitLen() > 64 {
		return fmt.Errorf("dtl: farm points overflow")
	}
	earned := steps.Uint64()
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

func ValidateDTLFarmCreateTx(state *DTLState, tx DTLFarmCreateTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid farm creator")
	}
	poolID := normalizeDTLPoolID(tx.PoolID)
	if poolID == "" {
		return fmt.Errorf("dtl: invalid farm pool_id")
	}
	if state.Pools[poolID] == nil {
		return fmt.Errorf("dtl: unknown pool")
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	if farmID != "" && state.FarmPools[farmID] != nil {
		return fmt.Errorf("dtl: farm already exists")
	}
	multiplier := tx.MultiplierBPS
	if multiplier == 0 {
		multiplier = DTLMaxTaxBPS
	}
	if multiplier > dtlFarmMaxMultiplierBPS() {
		return fmt.Errorf("dtl: farm multiplier exceeds max")
	}
	return nil
}

func ValidateDTLFarmStakeLPTx(state *DTLState, tx DTLFarmStakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid farm account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: farm stake amount must be > 0")
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
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

func ValidateDTLFarmUnstakeLPTx(state *DTLState, tx DTLFarmUnstakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid farm account")
	}
	if tx.Amount == 0 {
		return fmt.Errorf("dtl: farm unstake amount must be > 0")
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	farm := state.FarmPools[farmID]
	if farm == nil {
		return ErrDTLUnknownFarm
	}
	pos := state.FarmPositions[dtlFarmPositionKey(farmID, account)]
	if pos == nil || pos.StakedLP < tx.Amount {
		return fmt.Errorf("dtl: insufficient farm stake")
	}
	return nil
}

func ValidateDTLFarmClaimTx(state *DTLState, tx DTLFarmClaimTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlDeFiFarmEnabled() {
		return fmt.Errorf("dtl: farm is disabled")
	}
	state.ensure()
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid farm account")
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	if state.FarmPools[farmID] == nil {
		return ErrDTLUnknownFarm
	}
	if state.FarmPositions[dtlFarmPositionKey(farmID, account)] == nil {
		return fmt.Errorf("dtl: no farm position")
	}
	return nil
}

func ValidateDTLSeasonCreateTx(state *DTLState, tx DTLSeasonCreateTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlGameFiSeasonEnabled() {
		return fmt.Errorf("dtl: season is disabled")
	}
	state.ensure()
	creator := normalizeDTLAccount(tx.Creator)
	if creator == "" {
		return fmt.Errorf("dtl: invalid season creator")
	}
	start := tx.StartHeight
	if start == 0 {
		start = currentHeight
	}
	seasonLen := dtlGameFiSeasonLengthBlocks()
	if seasonLen == 0 {
		return fmt.Errorf("dtl: season length must be > 0")
	}
	if start > ^uint64(0)-seasonLen {
		return fmt.Errorf("dtl: season end overflow")
	}
	end := start + seasonLen
	grace := dtlGameFiClaimGraceBlocks()
	if end > ^uint64(0)-grace {
		return fmt.Errorf("dtl: season grace overflow")
	}
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	if seasonID != "" && state.Seasons[seasonID] != nil {
		return fmt.Errorf("dtl: season already exists")
	}
	for existingID, season := range state.Seasons {
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

func ValidateDTLSeasonFinalizeTx(state *DTLState, tx DTLSeasonFinalizeTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlGameFiSeasonEnabled() {
		return fmt.Errorf("dtl: season is disabled")
	}
	state.ensure()
	caller := normalizeDTLAccount(tx.Caller)
	if caller == "" {
		return fmt.Errorf("dtl: invalid season caller")
	}
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
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

func ValidateDTLSeasonClaimTx(state *DTLState, tx DTLSeasonClaimTx, currentHeight uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	if !dtlGameFiSeasonEnabled() {
		return fmt.Errorf("dtl: season is disabled")
	}
	state.ensure()
	account := normalizeDTLAccount(tx.Account)
	if account == "" {
		return fmt.Errorf("dtl: invalid season account")
	}
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
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
	key := dtlSeasonAccountKey(seasonID, account)
	if state.SeasonClaims[key] {
		return fmt.Errorf("dtl: season reward already claimed")
	}
	if state.SeasonScores[key] == 0 {
		return fmt.Errorf("dtl: season score is zero")
	}
	return nil
}

func ApplyDTLFarmCreateTx(state *DTLState, chainID string, nonce uint64, currentHeight uint64, tx DTLFarmCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLFarmCreateTx(state, tx); err != nil {
		return "", err
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	if farmID == "" {
		farmID = normalizeDTLFarmID(DTLFarmIDFromCreate(chainID, nonce, tx))
	}
	if state.FarmPools[farmID] != nil {
		return "", fmt.Errorf("dtl: farm already exists")
	}
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

func ApplyDTLFarmStakeLPTx(state *DTLState, currentHeight uint64, tx DTLFarmStakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLFarmStakeLPTx(state, tx); err != nil {
		return err
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	account := normalizeDTLAccount(tx.Account)
	farm := state.FarmPools[farmID]
	pos := getOrCreateDTLFarmPosition(state, farmID, account, currentHeight)
	if err := dtlAccrueFarmPosition(state, farm, pos, currentHeight); err != nil {
		return err
	}
	lpAccountKey := dtlLPBalanceKey(farm.PoolID, account)
	if state.LPBalances[lpAccountKey] < tx.Amount {
		return fmt.Errorf("dtl: insufficient LP balance")
	}
	state.LPBalances[lpAccountKey] -= tx.Amount
	if state.LPBalances[lpAccountKey] == 0 {
		delete(state.LPBalances, lpAccountKey)
	}
	if err := dtlAddBalance(state.LPBalances, dtlLPBalanceKey(farm.PoolID, dtlFarmVaultAccount(farmID)), tx.Amount); err != nil {
		return err
	}
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

func ApplyDTLFarmUnstakeLPTx(state *DTLState, currentHeight uint64, tx DTLFarmUnstakeLPTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLFarmUnstakeLPTx(state, tx); err != nil {
		return err
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	account := normalizeDTLAccount(tx.Account)
	farm := state.FarmPools[farmID]
	posKey := dtlFarmPositionKey(farmID, account)
	pos := state.FarmPositions[posKey]
	if pos == nil {
		return fmt.Errorf("dtl: no farm position")
	}
	if err := dtlAccrueFarmPosition(state, farm, pos, currentHeight); err != nil {
		return err
	}
	if pos.StakedLP < tx.Amount {
		return fmt.Errorf("dtl: insufficient farm stake")
	}
	if minStake := dtlFarmMinStakeBlocks(); minStake > 0 && pos.LastStakeHeight > 0 {
		if currentHeight < pos.LastStakeHeight+minStake {
			pos.AccruedPoints = 0
		}
	}
	pos.StakedLP -= tx.Amount
	lpVaultKey := dtlLPBalanceKey(farm.PoolID, dtlFarmVaultAccount(farmID))
	if state.LPBalances[lpVaultKey] < tx.Amount {
		return fmt.Errorf("dtl: farm vault insufficient LP")
	}
	state.LPBalances[lpVaultKey] -= tx.Amount
	if state.LPBalances[lpVaultKey] == 0 {
		delete(state.LPBalances, lpVaultKey)
	}
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

func ApplyDTLFarmClaimTx(state *DTLState, currentHeight uint64, tx DTLFarmClaimTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLFarmClaimTx(state, tx); err != nil {
		return 0, err
	}
	farmID := normalizeDTLFarmID(tx.FarmID)
	account := normalizeDTLAccount(tx.Account)
	farm := state.FarmPools[farmID]
	posKey := dtlFarmPositionKey(farmID, account)
	pos := state.FarmPositions[posKey]
	if pos == nil {
		return 0, fmt.Errorf("dtl: no farm position")
	}
	if err := dtlAccrueFarmPosition(state, farm, pos, currentHeight); err != nil {
		return 0, err
	}
	if pos.AccruedPoints == 0 {
		return 0, fmt.Errorf("dtl: no claimable farm points")
	}
	points := pos.AccruedPoints
	pos.AccruedPoints = 0
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

func ApplyDTLSeasonCreateTx(state *DTLState, chainID string, nonce uint64, currentHeight uint64, tx DTLSeasonCreateTx) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLSeasonCreateTx(state, tx, currentHeight); err != nil {
		return "", err
	}
	start := tx.StartHeight
	if start == 0 {
		start = currentHeight
	}
	end := start + dtlGameFiSeasonLengthBlocks()
	graceEnd := end + dtlGameFiClaimGraceBlocks()
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
	rewardToken := normalizeDTLTokenID(strings.TrimSpace(dtlGameFiRewardTokenRef()))
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

func ApplyDTLSeasonFinalizeTx(state *DTLState, currentHeight uint64, tx DTLSeasonFinalizeTx) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLSeasonFinalizeTx(state, tx, currentHeight); err != nil {
		return err
	}
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
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

func ApplyDTLSeasonClaimTx(state *DTLState, currentHeight uint64, tx DTLSeasonClaimTx) (uint64, error) {
	if state == nil {
		return 0, ErrDTLInvalidState
	}
	state.ensure()
	if err := ValidateDTLSeasonClaimTx(state, tx, currentHeight); err != nil {
		return 0, err
	}
	seasonID := normalizeDTLSeasonID(tx.SeasonID)
	account := normalizeDTLAccount(tx.Account)
	season := state.Seasons[seasonID]
	scoreKey := dtlSeasonAccountKey(seasonID, account)
	userScore := state.SeasonScores[scoreKey]
	if userScore == 0 {
		return 0, fmt.Errorf("dtl: season score is zero")
	}
	totalScore := season.TotalScore
	if totalScore == 0 {
		return 0, fmt.Errorf("dtl: season total score is zero")
	}
	rewardPool := state.SeasonVaults[seasonID]
	if rewardPool == 0 {
		return 0, fmt.Errorf("dtl: season reward pool is empty")
	}
	reward, err := dtlMulDivU64(rewardPool, userScore, totalScore)
	if err != nil {
		return 0, err
	}
	maxReward := ConfigDTLGameFiMaxRewardPerSeason
	if maxReward > 0 && reward > maxReward {
		reward = maxReward
	}
	if reward > rewardPool {
		reward = rewardPool
	}
	if reward == 0 {
		return 0, fmt.Errorf("dtl: season reward is zero")
	}
	rewardTokenID, ok := dtlResolveGameFiRewardTokenID(state)
	if !ok {
		return 0, fmt.Errorf("dtl: gamefi reward token unavailable")
	}
	if err := dtlMoveBalance(state, rewardTokenID, DTLTreasuryAccount, account, reward); err != nil {
		return 0, err
	}
	state.SeasonVaults[seasonID] -= reward
	state.SeasonClaims[scoreKey] = true
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
