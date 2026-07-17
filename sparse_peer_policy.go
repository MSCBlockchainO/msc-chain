package main

import (
	"bytes"
	"crypto/sha256"
	"sort"
	"strconv"
	"strings"
)

type sparsePeerTarget struct {
	addr  string
	score [sha256.Size]byte
}

func normalizedMinimumPeers(configured int) int {
	if configured < MinimumPeers {
		return MinimumPeers
	}
	if MaxPeers > 0 && configured > MaxPeers {
		return MaxPeers
	}
	return configured
}

// sparsePeerRotationBucket keeps peer choices stable within a protocol
// committee window. A bounded amount of churn at the next window avoids a
// permanent topology without changing any consensus decision.
func sparsePeerRotationBucket(height uint64) uint64 {
	rotation := committeeRotationBlocks()
	if rotation == 0 {
		return 0
	}
	return height / rotation
}

// selectSparsePeerTargets deterministically chooses a node-specific subset of
// active validator addresses. Connections remain sparse at large validator
// counts while GossipSub carries blocks, transactions, and votes over multiple
// hops. The selection is local networking policy and never enters chain state.
func selectSparsePeerTargets(localID string, height uint64, candidates []string, limit int) []string {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	localID = strings.ToUpper(strings.TrimSpace(localID))
	bucket := sparsePeerRotationBucket(height)

	unique := make(map[string]string, len(candidates))
	for _, raw := range candidates {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		_, peerID, hasPeerID := splitPeerAddress(addr)
		identity := strings.ToLower(addr)
		if hasPeerID && strings.TrimSpace(peerID) != "" {
			identity = strings.TrimSpace(peerID)
		}
		if previous, exists := unique[identity]; !exists || addr < previous {
			unique[identity] = addr
		}
	}

	targets := make([]sparsePeerTarget, 0, len(unique))
	for identity, addr := range unique {
		seed := strings.Join([]string{
			"msc-sparse-peer-v1",
			protocolChainID(),
			localID,
			strconv.FormatUint(bucket, 10),
			identity,
		}, "|")
		targets = append(targets, sparsePeerTarget{
			addr:  addr,
			score: sha256.Sum256([]byte(seed)),
		})
	}

	sort.Slice(targets, func(i, j int) bool {
		if cmp := bytes.Compare(targets[i].score[:], targets[j].score[:]); cmp != 0 {
			return cmp < 0
		}
		return targets[i].addr < targets[j].addr
	})
	if limit > len(targets) {
		limit = len(targets)
	}
	out := make([]string, 0, limit)
	for _, target := range targets[:limit] {
		out = append(out, target.addr)
	}
	return out
}
