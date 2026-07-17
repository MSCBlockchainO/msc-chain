package main

import (
	"fmt"
	"sort"
	"strings"
)

type dtlRouteEdge struct {
	PoolID    string
	NextToken string
}

func dtlRoutePathLexKey(path []string) string {
	return strings.Join(path, "|")
}

func dtlEnumeratePoolRoutePaths(state *DTLState, tokenIn, tokenOut string, maxHops, maxPaths int) [][]string {
	if state == nil {
		return nil
	}
	state.ensure()

	in := normalizeDTLTokenID(tokenIn)
	out := normalizeDTLTokenID(tokenOut)
	if in == "" || out == "" || in == out {
		return nil
	}
	if maxHops < 1 {
		maxHops = 1
	}
	if maxPaths < 1 {
		maxPaths = 1
	}

	poolIDs := make([]string, 0, len(state.Pools))
	for poolID := range state.Pools {
		poolIDs = append(poolIDs, normalizeDTLPoolID(poolID))
	}
	sort.Strings(poolIDs)

	adj := make(map[string][]dtlRouteEdge)
	for _, poolID := range poolIDs {
		pool := state.Pools[poolID]
		if pool == nil {
			continue
		}
		a := normalizeDTLTokenID(pool.TokenA)
		b := normalizeDTLTokenID(pool.TokenB)
		if a == "" || b == "" || a == b {
			continue
		}
		adj[a] = append(adj[a], dtlRouteEdge{PoolID: poolID, NextToken: b})
		adj[b] = append(adj[b], dtlRouteEdge{PoolID: poolID, NextToken: a})
	}
	for token := range adj {
		sort.Slice(adj[token], func(i, j int) bool {
			left := adj[token][i]
			right := adj[token][j]
			if left.PoolID == right.PoolID {
				return left.NextToken < right.NextToken
			}
			return left.PoolID < right.PoolID
		})
	}

	paths := make([][]string, 0, maxPaths)
	usedPools := make(map[string]bool)
	visitedTokens := map[string]bool{in: true}
	currentPath := make([]string, 0, maxHops)

	var dfs func(current string)
	dfs = func(current string) {
		if len(paths) >= maxPaths {
			return
		}
		if current == out && len(currentPath) > 0 {
			copied := append([]string(nil), currentPath...)
			paths = append(paths, copied)
			// Keep exploring until max hops; longer routes can produce better output.
		}
		if len(currentPath) >= maxHops {
			return
		}
		for _, edge := range adj[current] {
			if len(paths) >= maxPaths {
				return
			}
			if usedPools[edge.PoolID] {
				continue
			}
			if visitedTokens[edge.NextToken] && edge.NextToken != out {
				continue
			}
			usedPools[edge.PoolID] = true
			alreadyVisited := visitedTokens[edge.NextToken]
			visitedTokens[edge.NextToken] = true
			currentPath = append(currentPath, edge.PoolID)
			dfs(edge.NextToken)
			currentPath = currentPath[:len(currentPath)-1]
			if !alreadyVisited {
				delete(visitedTokens, edge.NextToken)
			}
			delete(usedPools, edge.PoolID)
		}
	}

	dfs(in)

	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) == len(paths[j]) {
			return dtlRoutePathLexKey(paths[i]) < dtlRoutePathLexKey(paths[j])
		}
		return len(paths[i]) < len(paths[j])
	})
	if len(paths) > maxPaths {
		paths = paths[:maxPaths]
	}
	return paths
}

func dtlBestPoolSwapRouteQuote(state *DTLState, tokenIn, tokenOut string, amountIn uint64, maxHops int) (DTLRouteQuote, error) {
	empty := DTLRouteQuote{}
	if state == nil {
		return empty, ErrDTLInvalidState
	}
	if amountIn == 0 {
		return empty, fmt.Errorf("dtl: amount_in must be > 0")
	}
	if !dtlRouterEnabled() {
		return empty, fmt.Errorf("dtl: router is disabled")
	}

	in := normalizeDTLTokenID(tokenIn)
	out := normalizeDTLTokenID(tokenOut)
	if in == "" || out == "" {
		return empty, fmt.Errorf("dtl: token_in and token_out are required")
	}
	if in == out {
		return empty, fmt.Errorf("dtl: token_in and token_out must differ")
	}

	if maxHops < 1 || maxHops > dtlRouterMaxHops() {
		maxHops = dtlRouterMaxHops()
	}
	paths := dtlEnumeratePoolRoutePaths(state, in, out, maxHops, dtlRouterQuoteMaxPaths())
	if len(paths) == 0 {
		return empty, fmt.Errorf("dtl: no route found")
	}

	var best *DTLRouteQuote
	for _, path := range paths {
		quote, err := dtlQuotePoolSwapRoute(state, in, amountIn, path)
		if err != nil {
			continue
		}
		if normalizeDTLTokenID(quote.TokenOut) != out {
			continue
		}
		if quote.PriceImpactBPS > dtlRouterMaxPriceImpactBPS() {
			continue
		}
		if best == nil {
			copyQuote := quote
			best = &copyQuote
			continue
		}
		switch {
		case quote.AmountOut > best.AmountOut:
			copyQuote := quote
			best = &copyQuote
		case quote.AmountOut == best.AmountOut && len(quote.Path) < len(best.Path):
			copyQuote := quote
			best = &copyQuote
		case quote.AmountOut == best.AmountOut && len(quote.Path) == len(best.Path):
			if dtlRoutePathLexKey(quote.Path) < dtlRoutePathLexKey(best.Path) {
				copyQuote := quote
				best = &copyQuote
			}
		}
	}

	if best == nil {
		return empty, fmt.Errorf("dtl: no route found within safety limits")
	}
	return *best, nil
}
