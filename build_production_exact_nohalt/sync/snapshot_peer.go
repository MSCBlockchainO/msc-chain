package syncpipeline

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
)

type Peer struct {
	ID          string
	IsValidator bool
	IsArchival  bool
	Score       float64
}

func SelectSnapshotPeers(peers []Peer) []Peer {
	out := make([]Peer, 0, len(peers))
	for _, peer := range peers {
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

func SelectSnapshotPeer(peers []Peer) (Peer, bool) {
	sorted := SelectSnapshotPeers(peers)
	if len(sorted) == 0 {
		return Peer{}, false
	}
	return sorted[0], true
}

func SelectSnapshotReplicationPeers(peers []Peer, minReplicas int, seed string) []Peer {
	sorted := SelectSnapshotPeers(peers)
	if len(sorted) == 0 {
		return nil
	}
	if minReplicas <= 0 {
		minReplicas = 3
	}
	target := minInt(minReplicas, len(sorted))
	selected := make([]Peer, 0, target)
	seen := make(map[string]struct{}, target)
	add := func(peer Peer) bool {
		id := strings.TrimSpace(peer.ID)
		if id == "" {
			return false
		}
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
		pool := make([]Peer, 0, len(sorted))
		for _, peer := range sorted {
			if _, ok := seen[peer.ID]; ok {
				continue
			}
			pool = append(pool, peer)
		}
		if len(pool) > 0 {
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

func deterministicPeerIndex(seed string, peers []Peer) int {
	if len(peers) == 0 {
		return 0
	}
	if strings.TrimSpace(seed) == "" {
		var b strings.Builder
		for _, peer := range peers {
			b.WriteString(peer.ID)
			b.WriteByte('|')
		}
		seed = b.String()
	}
	sum := sha256.Sum256([]byte(seed))
	value := binary.BigEndian.Uint64(sum[:8])
	return int(value % uint64(len(peers)))
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
