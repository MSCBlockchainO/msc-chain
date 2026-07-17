package syncpipeline

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
)

type Peer struct {
	// `ID` stores the current position in the related collection.
	ID          string
	// `IsValidator` stores the current position in the related collection.
	IsValidator bool
	// `IsArchival` stores the value currently being processed.
	IsArchival  bool
	// `Score` stores the value associated with this record.
	Score       float64
}

// SelectSnapshotPeers selects snapshot peers.
func SelectSnapshotPeers(peers []Peer) []Peer {
	// `out` stores the result produced by this operation.
	out := make([]Peer, 0, len(peers))
	// `peer` tracks the current values while iterating.
	for _, peer := range peers {
		// `id` stores the current position in the related collection.
		id := strings.TrimSpace(peer.ID)
		if id == "" {
			continue
		}
		peer.ID = id
		out = append(out, peer)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsValidator != out[j].IsValidator {
			return out[i].IsValidator
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// SelectSnapshotPeer selects snapshot peer.
func SelectSnapshotPeer(peers []Peer) (Peer, bool) {
	// `sorted` stores the value produced by this operation.
	sorted := SelectSnapshotPeers(peers)
	if len(sorted) == 0 {
		return Peer{}, false
	}
	return sorted[0], true
}

// SelectSnapshotReplicationPeers selects snapshot replication peers.
func SelectSnapshotReplicationPeers(peers []Peer, minReplicas int, seed string) []Peer {
	// `sorted` stores the value produced by this operation.
	sorted := SelectSnapshotPeers(peers)
	if len(sorted) == 0 {
		return nil
	}
	if minReplicas <= 0 {
		minReplicas = 3
	}
	// `target` stores the value produced by this operation.
	target := minInt(minReplicas, len(sorted))
	// `selected` stores the value produced by this operation.
	selected := make([]Peer, 0, target)
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, target)
	// `add` stores the value produced by this operation.
	add := func(peer Peer) bool {
		// `id` stores the current position in the related collection.
		id := strings.TrimSpace(peer.ID)
		if id == "" {
			return false
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
		selected = append(selected, peer)
		return true
	}

	// Policy 1: at least one validator provider.
	for _, peer := range sorted {
		if peer.IsValidator {
			add(peer)
			break
		}
	}
	// Policy 2: at least one archival/full provider.
	for _, peer := range sorted {
		if peer.IsArchival {
			if add(peer) {
				break
			}
		}
	}
	// Policy 3: one random provider (deterministic by seed) for diversity.
	if len(selected) < target {
		// `pool` stores the value produced by this operation.
		pool := make([]Peer, 0, len(sorted))
		// `peer` tracks the current values while iterating.
		for _, peer := range sorted {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := seen[peer.ID]; ok {
				continue
			}
			pool = append(pool, peer)
		}
		if len(pool) > 0 {
			// `idx` stores the current position in the related collection.
			idx := deterministicPeerIndex(seed, pool)
			add(pool[idx])
		}
	}
	// Fill remaining replicas by score order.
	for _, peer := range sorted {
		if len(selected) >= target {
			break
		}
		add(peer)
	}
	return selected
}

// deterministicPeerIndex implements the deterministic peer index helper.
func deterministicPeerIndex(seed string, peers []Peer) int {
	if len(peers) == 0 {
		return 0
	}
	if strings.TrimSpace(seed) == "" {
		// `b` stores the value used by this operation.
		var b strings.Builder
		// `peer` tracks the current values while iterating.
		for _, peer := range peers {
			b.WriteString(peer.ID)
			b.WriteByte('|')
		}
		seed = b.String()
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(seed))
	// `value` stores the value currently being processed.
	value := binary.BigEndian.Uint64(sum[:8])
	return int(value % uint64(len(peers)))
}

// minInt returns the minimum int.
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
