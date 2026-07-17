package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/bits"
	"sort"
	"strings"
)

func dtlSafeAddU64(a, b uint64) (uint64, error) {
	if a > ^uint64(0)-b {
		return 0, fmt.Errorf("dtl: uint64 overflow")
	}
	return a + b, nil
}

func dtlMulDivU64(a, b, denom uint64) (uint64, error) {
	if denom == 0 {
		return 0, fmt.Errorf("dtl: division by zero")
	}
	num := new(big.Int).SetUint64(a)
	num.Mul(num, new(big.Int).SetUint64(b))
	num.Div(num, new(big.Int).SetUint64(denom))
	if num.Sign() < 0 || num.BitLen() > 64 {
		return 0, fmt.Errorf("dtl: uint64 overflow")
	}
	return num.Uint64(), nil
}

func dtlEqCrossMul(a, b, c, d uint64) bool {
	hi1, lo1 := bits.Mul64(a, b)
	hi2, lo2 := bits.Mul64(c, d)
	return hi1 == hi2 && lo1 == lo2
}

func dtlInitialPoolShare(amountA, amountB uint64) (uint64, error) {
	if amountA == 0 || amountB == 0 {
		return 0, fmt.Errorf("dtl: initial liquidity must be > 0")
	}
	prod := new(big.Int).SetUint64(amountA)
	prod.Mul(prod, new(big.Int).SetUint64(amountB))
	root := new(big.Int).Sqrt(prod)
	if root.Sign() <= 0 {
		return 0, fmt.Errorf("dtl: initial LP share is zero")
	}
	if root.BitLen() > 64 {
		return 0, fmt.Errorf("dtl: LP share overflow")
	}
	return root.Uint64(), nil
}

func dtlLiquidityShareMint(pool *DTLPoolState, amountA, amountB uint64) (uint64, error) {
	if pool == nil {
		return 0, fmt.Errorf("dtl: nil pool")
	}
	if pool.ReserveA == 0 || pool.ReserveB == 0 || pool.TotalLPShares == 0 {
		return 0, fmt.Errorf("dtl: invalid pool reserves")
	}
	shareA, err := dtlMulDivU64(amountA, pool.TotalLPShares, pool.ReserveA)
	if err != nil {
		return 0, err
	}
	shareB, err := dtlMulDivU64(amountB, pool.TotalLPShares, pool.ReserveB)
	if err != nil {
		return 0, err
	}
	if shareA < shareB {
		return shareA, nil
	}
	return shareB, nil
}

func dtlLiquidityShareBurn(pool *DTLPoolState, lpShares uint64) (uint64, uint64, error) {
	if pool == nil {
		return 0, 0, fmt.Errorf("dtl: nil pool")
	}
	if pool.TotalLPShares == 0 {
		return 0, 0, fmt.Errorf("dtl: invalid pool LP supply")
	}
	outA, err := dtlMulDivU64(lpShares, pool.ReserveA, pool.TotalLPShares)
	if err != nil {
		return 0, 0, err
	}
	outB, err := dtlMulDivU64(lpShares, pool.ReserveB, pool.TotalLPShares)
	if err != nil {
		return 0, 0, err
	}
	return outA, outB, nil
}

func dtlPoolSwapOutAmount(reserveIn, reserveOut, amountIn uint64, feeBPS uint16) (uint64, error) {
	if reserveIn == 0 || reserveOut == 0 {
		return 0, fmt.Errorf("dtl: empty pool reserves")
	}
	if amountIn == 0 {
		return 0, fmt.Errorf("dtl: amount_in must be > 0")
	}
	if feeBPS > DTLMaxPoolFeeBPS {
		return 0, fmt.Errorf("dtl: invalid pool fee")
	}
	feeFactor := uint64(DTLMaxTaxBPS - feeBPS)
	inAfterFee := new(big.Int).SetUint64(amountIn)
	inAfterFee.Mul(inAfterFee, new(big.Int).SetUint64(feeFactor))

	numerator := new(big.Int).Set(inAfterFee)
	numerator.Mul(numerator, new(big.Int).SetUint64(reserveOut))

	denominator := new(big.Int).SetUint64(reserveIn)
	denominator.Mul(denominator, new(big.Int).SetUint64(DTLMaxTaxBPS))
	denominator.Add(denominator, inAfterFee)
	if denominator.Sign() == 0 {
		return 0, fmt.Errorf("dtl: invalid swap denominator")
	}

	out := new(big.Int).Div(numerator, denominator)
	if out.Sign() <= 0 {
		return 0, nil
	}
	if out.BitLen() > 64 {
		return 0, fmt.Errorf("dtl: swap output overflow")
	}
	return out.Uint64(), nil
}

func dtlDuelWinner(duelID, playerA, playerB, revealA, revealB string) string {
	a := normalizeDTLAccount(playerA)
	b := normalizeDTLAccount(playerB)
	if a == "" || b == "" {
		return ""
	}
	payload := strings.Join([]string{
		normalizeDTLTokenID(duelID),
		a,
		b,
		strings.TrimSpace(revealA),
		strings.TrimSpace(revealB),
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	if sum[len(sum)-1]&1 == 0 {
		return a
	}
	return b
}

func dtlLendingMaxDebt(collateral uint64, collateralFactorBPS uint16) (uint64, error) {
	if collateralFactorBPS == 0 || collateralFactorBPS >= DTLMaxTaxBPS {
		return 0, fmt.Errorf("dtl: invalid collateral factor")
	}
	return dtlMulDivU64(collateral, uint64(collateralFactorBPS), DTLMaxTaxBPS)
}

func dtlLendingIsHealthy(collateral, debt uint64, collateralFactorBPS uint16) (bool, error) {
	maxDebt, err := dtlLendingMaxDebt(collateral, collateralFactorBPS)
	if err != nil {
		return false, err
	}
	return debt <= maxDebt, nil
}

func dtlLendingSeizeCollateral(repayAmount uint64, bonusBPS uint16) (uint64, error) {
	if bonusBPS > DTLMaxLiqBonusBPS {
		return 0, fmt.Errorf("dtl: invalid liquidation bonus")
	}
	factor, err := dtlSafeAddU64(uint64(DTLMaxTaxBPS), uint64(bonusBPS))
	if err != nil {
		return 0, err
	}
	return dtlMulDivU64(repayAmount, factor, DTLMaxTaxBPS)
}

func dtlTournamentWinner(tournamentID string, candidates []string, reveals map[string]string) string {
	if len(candidates) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(candidates))
	for _, c := range candidates {
		n := normalizeDTLAccount(c)
		if n == "" {
			continue
		}
		normalized = append(normalized, n)
	}
	if len(normalized) == 0 {
		return ""
	}
	sort.Strings(normalized)
	var b strings.Builder
	b.WriteString(normalizeDTLTournamentID(tournamentID))
	for _, candidate := range normalized {
		b.WriteString("|")
		b.WriteString(candidate)
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(reveals[candidate]))
	}
	sum := sha256.Sum256([]byte(b.String()))
	idx := binary.BigEndian.Uint64(sum[:8]) % uint64(len(normalized))
	return normalized[idx]
}
