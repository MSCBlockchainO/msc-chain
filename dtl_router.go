package main

import (
	"fmt"
	"sort"
	"strings"
)

type dtlRouteEdge struct {
	// `PoolID` stores the value associated with this record.
	PoolID    string
	// `NextToken` stores the value associated with this record.
	NextToken string
}

// dtlRoutePathLexKey implements the dtl route path lex key helper.
func dtlRoutePathLexKey(path []string) string {
	return strings.Join(path, "|")
}

// dtlEnumeratePoolRoutePaths implements the dtl enumerate pool route paths helper.
func dtlEnumeratePoolRoutePaths(state *DTLState, tokenIn, tokenOut string, maxHops, maxPaths int) [][]string {
	if state == nil {
		return nil
	}
	state.ensure()

	// `in` stores the current position in the related collection.
	in := normalizeDTLTokenID(tokenIn)
	// `out` stores the result produced by this operation.
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

	// `poolIDs` stores the value produced by this operation.
	poolIDs := make([]string, 0, len(state.Pools))
	// `poolID` tracks the current values while iterating.
	for poolID := range state.Pools {
		poolIDs = append(poolIDs, normalizeDTLPoolID(poolID))
	}
	sort.Strings(poolIDs)

	// `adj` stores the current position in the related collection.
	adj := make(map[string][]dtlRouteEdge)
	// `poolID` tracks the current values while iterating.
	for _, poolID := range poolIDs {
		// `pool` stores the value produced by this operation.
		pool := state.Pools[poolID]
		if pool == nil {
			continue
		}
		// `a` stores the value produced by this operation.
		a := normalizeDTLTokenID(pool.TokenA)
		// `b` stores the value produced by this operation.
		b := normalizeDTLTokenID(pool.TokenB)
		if a == "" || b == "" || a == b {
			continue
		}
		adj[a] = append(adj[a], dtlRouteEdge{PoolID: poolID, NextToken: b})
		adj[b] = append(adj[b], dtlRouteEdge{PoolID: poolID, NextToken: a})
	}
	// `token` tracks the current values while iterating.
	for token := range adj {
		sort.Slice(adj[token], func(i, j int) bool {
			// `left` stores the value produced by this operation.
			left := adj[token][i]
			// `right` stores the value produced by this operation.
			right := adj[token][j]
			if left.PoolID == right.PoolID {
				return left.NextToken < right.NextToken
			}
			return left.PoolID < right.PoolID
		})
	}

	// `paths` stores the value produced by this operation.
	paths := make([][]string, 0, maxPaths)
	// `usedPools` stores the value produced by this operation.
	usedPools := make(map[string]bool)
	// `visitedTokens` stores the value produced by this operation.
	visitedTokens := map[string]bool{in: true}
	// `currentPath` stores the value produced by this operation.
	currentPath := make([]string, 0, maxHops)

	// `dfs` stores the value used by this operation.
	var dfs func(current string)
	dfs = func(current string) {
		if len(paths) >= maxPaths {
			return
		}
		if current == out && len(currentPath) > 0 {
			// `copied` stores the value produced by this operation.
			copied := append([]string(nil), currentPath...)
			paths = append(paths, copied)
			// Keep exploring until max hops; longer routes can produce better output.
		}
		if len(currentPath) >= maxHops {
			return
		}
		// `edge` tracks the current values while iterating.
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
			// `alreadyVisited` stores the value produced by this operation.
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

// dtlBestPoolSwapRouteQuote implements the dtl best pool swap route quote helper.
func dtlBestPoolSwapRouteQuote(state *DTLState, tokenIn, tokenOut string, amountIn uint64, maxHops int) (DTLRouteQuote, error) {
	// `empty` stores the value produced by this operation.
	empty := DTLRouteQuote{}
	if state == nil {
		return empty, ErrDTLInvalidState
	}
	if amountIn == 0 {
		return empty, fmt.Errorf("dtl: amount_in must be > 0")
	}
	if !dtlProtocolRouterEnabled() {
		return empty, fmt.Errorf("dtl: router is disabled")
	}

	// `in` stores the current position in the related collection.
	in := normalizeDTLTokenID(tokenIn)
	// `out` stores the result produced by this operation.
	out := normalizeDTLTokenID(tokenOut)
	if in == "" || out == "" {
		return empty, fmt.Errorf("dtl: token_in and token_out are required")
	}
	if in == out {
		return empty, fmt.Errorf("dtl: token_in and token_out must differ")
	}

	if maxHops < 1 || maxHops > dtlProtocolRouterMaxHops() {
		maxHops = dtlProtocolRouterMaxHops()
	}
	// `paths` stores the value produced by this operation.
	paths := dtlEnumeratePoolRoutePaths(state, in, out, maxHops, dtlProtocolRouterQuoteMaxPaths())
	if len(paths) == 0 {
		return empty, fmt.Errorf("dtl: no route found")
	}

	// `best` stores the value used by this operation.
	var best *DTLRouteQuote
	// `path` tracks the current values while iterating.
	for _, path := range paths {
		// `quote` and `err` store the error produced by this operation.
		quote, err := dtlQuotePoolSwapRoute(state, in, amountIn, path)
		if err != nil {
			continue
		}
		if normalizeDTLTokenID(quote.TokenOut) != out {
			continue
		}
		if quote.PriceImpactBPS > dtlProtocolRouterMaxPriceImpactBPS() {
			continue
		}
		if best == nil {
			// `copyQuote` stores the value produced by this operation.
			copyQuote := quote
			best = &copyQuote
			continue
		}
		switch {
		case quote.AmountOut > best.AmountOut:
			// `copyQuote` stores the value produced by this operation.
			copyQuote := quote
			best = &copyQuote
		case quote.AmountOut == best.AmountOut && len(quote.Path) < len(best.Path):
			// `copyQuote` stores the value produced by this operation.
			copyQuote := quote
			best = &copyQuote
		case quote.AmountOut == best.AmountOut && len(quote.Path) == len(best.Path):
			if dtlRoutePathLexKey(quote.Path) < dtlRoutePathLexKey(best.Path) {
				// `copyQuote` stores the value produced by this operation.
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
