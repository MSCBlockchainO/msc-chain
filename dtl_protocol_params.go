package main

import "strings"

// dtlProtocolV2EnabledAtHeight implements the dtl protocol v2 enabled at height helper.
func dtlProtocolV2EnabledAtHeight(_ uint64) bool {
	// Compatibility/UI helper only. Consensus execution selects V2 from the
	// block-committed ProtocolVersion + FeatureBitmap envelope.
	return false
}

// dtlProtocolBeaconDelayAtHeight implements the dtl protocol beacon delay at height helper.
func dtlProtocolBeaconDelayAtHeight(height uint64) uint64 {
	if !dtlProtocolV2EnabledAtHeight(height) {
		return 0
	}
	return DTLDefaultGameBeaconDelayBlocks
}

// dtlProtocolRouterEnabled implements the dtl protocol router enabled helper.
func dtlProtocolRouterEnabled() bool {
	return true
}

// dtlProtocolRouterMaxHops implements the dtl protocol router max hops helper.
func dtlProtocolRouterMaxHops() int {
	return int(DTLDefaultRouterMaxHops)
}

// dtlProtocolRouterDeadlineMaxBlocks implements the dtl protocol router deadline max blocks helper.
func dtlProtocolRouterDeadlineMaxBlocks() uint64 {
	return DTLDefaultRouterDeadlineMaxBlocks
}

// dtlProtocolRouterMaxPriceImpactBPS implements the dtl protocol router max price impact bps helper.
func dtlProtocolRouterMaxPriceImpactBPS() uint16 {
	return DTLDefaultRouterMaxPriceImpactBPS
}

// dtlProtocolRouterQuoteMaxPaths implements the dtl protocol router quote max paths helper.
func dtlProtocolRouterQuoteMaxPaths() int {
	return int(DTLDefaultRouterQuoteMaxPaths)
}

// dtlProtocolDeFiFarmEnabled implements the dtl protocol de fi farm enabled helper.
func dtlProtocolDeFiFarmEnabled() bool {
	return true
}

// dtlProtocolGameFiSeasonEnabled implements the dtl protocol game fi season enabled helper.
func dtlProtocolGameFiSeasonEnabled() bool {
	return true
}

// dtlProtocolGameFiRewardTokenRef implements the dtl protocol game fi reward token ref helper.
func dtlProtocolGameFiRewardTokenRef() string {
	return strings.TrimSpace(CoinSymbol)
}

// dtlProtocolGameFiSeasonLengthBlocks implements the dtl protocol game fi season length blocks helper.
func dtlProtocolGameFiSeasonLengthBlocks() uint64 {
	return DTLDefaultGameFiSeasonLengthBlocks
}

// dtlProtocolGameFiClaimGraceBlocks implements the dtl protocol game fi claim grace blocks helper.
func dtlProtocolGameFiClaimGraceBlocks() uint64 {
	return DTLDefaultGameFiClaimGraceBlocks
}

// dtlProtocolGameFiFeeShareFromPoolBPS implements the dtl protocol game fi fee share from pool bps helper.
func dtlProtocolGameFiFeeShareFromPoolBPS() uint16 {
	return DTLDefaultGameFiFeeSharePoolBPS
}

// dtlProtocolGameFiFeeShareFromLendingBPS implements the dtl protocol game fi fee share from lending bps helper.
func dtlProtocolGameFiFeeShareFromLendingBPS() uint16 {
	return DTLDefaultGameFiFeeShareLendingBPS
}

// dtlProtocolGameFiDuelWinPoints implements the dtl protocol game fi duel win points helper.
func dtlProtocolGameFiDuelWinPoints() uint64 {
	return DTLDefaultGameFiDuelWinPoints
}

// dtlProtocolGameFiTournamentWinPoints implements the dtl protocol game fi tournament win points helper.
func dtlProtocolGameFiTournamentWinPoints() uint64 {
	return DTLDefaultGameFiTournamentWinPoints
}

// dtlProtocolGameFiTournamentPartPoints implements the dtl protocol game fi tournament part points helper.
func dtlProtocolGameFiTournamentPartPoints() uint64 {
	return DTLDefaultGameFiTournamentPartPoints
}

// dtlProtocolGameFiMaxRewardPerSeason implements the dtl protocol game fi max reward per season helper.
func dtlProtocolGameFiMaxRewardPerSeason() uint64 {
	return DTLDefaultGameFiMaxRewardPerSeason
}

// dtlProtocolFarmMaxMultiplierBPS implements the dtl protocol farm max multiplier bps helper.
func dtlProtocolFarmMaxMultiplierBPS() uint16 {
	return DTLDefaultFarmMaxMultiplierBPS
}

// dtlProtocolFarmMinStakeBlocks implements the dtl protocol farm min stake blocks helper.
func dtlProtocolFarmMinStakeBlocks() uint64 {
	return DTLDefaultFarmMinStakeBlocks
}

// dtlProtocolFarmLPPointsPerBlock implements the dtl protocol farm lp points per block helper.
func dtlProtocolFarmLPPointsPerBlock() uint64 {
	return DTLDefaultFarmLPPointsPerBlock
}

// dtlProtocolOracleMinSigners implements the dtl protocol oracle min signers helper.
func dtlProtocolOracleMinSigners() uint16 {
	return DTLDefaultOracleMinSigners
}

// dtlProtocolOracleMaxStalenessBlocks implements the dtl protocol oracle max staleness blocks helper.
func dtlProtocolOracleMaxStalenessBlocks() uint64 {
	return DTLDefaultOracleMaxStalenessBlocks
}

// dtlProtocolLendingAccrualIntervalBlocks implements the dtl protocol lending accrual interval blocks helper.
func dtlProtocolLendingAccrualIntervalBlocks() uint64 {
	return DTLDefaultLendingAccrualIntervalBlocks
}

// dtlProtocolLogsIndexEnabled implements the dtl protocol logs index enabled helper.
func dtlProtocolLogsIndexEnabled() bool {
	return true
}
