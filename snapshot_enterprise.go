package main

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

type SnapshotCatalogEntry struct {
	Height            uint64   `json:"height"`
	StateRoot         string   `json:"state_root"`
	ChunkCount        uint64   `json:"chunk_count"`
	ProviderSet       []string `json:"provider_set"`
	ProofSet          []string `json:"proof_set"`
	AvailabilityRatio float64  `json:"availability_ratio"`
	UpdatedAtUnix     int64    `json:"updated_at_unix"`
}

func syncSnapshotReplicationMinCopies() int {
	if SyncSnapshotReplicationMinCopies <= 0 {
		return 3
	}
	return SyncSnapshotReplicationMinCopies
}

func syncSnapshotWarmupBlocks() uint64 {
	if SyncSnapshotWarmupBlocks == 0 {
		return 5
	}
	return SyncSnapshotWarmupBlocks
}

func normalizeSnapshotCatalogSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func mergeSnapshotCatalogSet(current []string, updates []string) []string {
	current = normalizeSnapshotCatalogSet(current)
	updates = normalizeSnapshotCatalogSet(updates)
	if len(current) == 0 {
		return updates
	}
	if len(updates) == 0 {
		return current
	}
	seen := make(map[string]struct{}, len(current)+len(updates))
	out := make([]string, 0, len(current)+len(updates))
	for _, value := range current {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range updates {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func clampSnapshotAvailabilityRatio(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func snapshotCatalogAvailabilityRatio(providerCount int, minCopies int) float64 {
	if providerCount <= 0 {
		return 0
	}
	if minCopies <= 0 {
		minCopies = 1
	}
	available := providerCount
	if available > minCopies {
		available = minCopies
	}
	return clampSnapshotAvailabilityRatio(float64(available) / float64(minCopies))
}

func (n *Node) updateSnapshotCatalogMeta(height uint64, stateRoot string, chunkCount uint64, providers []string) {
	if n == nil || height == 0 {
		return
	}
	providers = normalizeSnapshotCatalogSet(providers)
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	entry := n.snapshotCatalog[height]
	entry.Height = height
	if strings.TrimSpace(stateRoot) != "" {
		entry.StateRoot = strings.TrimSpace(stateRoot)
	}
	if chunkCount > 0 {
		entry.ChunkCount = chunkCount
	}
	entry.ProviderSet = mergeSnapshotCatalogSet(entry.ProviderSet, providers)
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	n.snapshotCatalogMu.Unlock()
}

func (n *Node) updateSnapshotCatalogProofSet(height uint64, validators []string) {
	if n == nil || height == 0 {
		return
	}
	validators = normalizeSnapshotCatalogSet(validators)
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	entry := n.snapshotCatalog[height]
	entry.Height = height
	entry.ProofSet = mergeSnapshotCatalogSet(entry.ProofSet, validators)
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	n.snapshotCatalogMu.Unlock()
}

func (n *Node) updateSnapshotCatalogAvailability(height uint64, ratio float64) {
	if n == nil || height == 0 {
		return
	}
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	entry := n.snapshotCatalog[height]
	entry.Height = height
	entry.AvailabilityRatio = clampSnapshotAvailabilityRatio(ratio)
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	n.snapshotCatalogMu.Unlock()
}

func (n *Node) updateSnapshotCatalogProviders(height uint64, providers []string) {
	if n == nil || height == 0 {
		return
	}
	providers = normalizeSnapshotCatalogSet(providers)
	if len(providers) == 0 {
		return
	}
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	entry := n.snapshotCatalog[height]
	entry.Height = height
	entry.ProviderSet = mergeSnapshotCatalogSet(entry.ProviderSet, providers)
	computed := snapshotCatalogAvailabilityRatio(len(entry.ProviderSet), syncSnapshotReplicationMinCopies())
	// Keep availability monotonic unless a stricter ratio is explicitly set elsewhere.
	if computed > entry.AvailabilityRatio {
		entry.AvailabilityRatio = computed
	}
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	n.snapshotCatalogMu.Unlock()
}

func (n *Node) snapshotCatalogEntry(height uint64) (SnapshotCatalogEntry, bool) {
	if n == nil || height == 0 {
		return SnapshotCatalogEntry{}, false
	}
	n.snapshotCatalogMu.RLock()
	entry, ok := n.snapshotCatalog[height]
	n.snapshotCatalogMu.RUnlock()
	if !ok || entry.Height == 0 {
		return SnapshotCatalogEntry{}, false
	}
	entry.ProviderSet = append([]string{}, entry.ProviderSet...)
	entry.ProofSet = append([]string{}, entry.ProofSet...)
	return entry, true
}

func (n *Node) latestSnapshotCatalogEntry() (SnapshotCatalogEntry, bool) {
	if n == nil {
		return SnapshotCatalogEntry{}, false
	}
	n.snapshotCatalogMu.RLock()
	if len(n.snapshotCatalog) == 0 {
		n.snapshotCatalogMu.RUnlock()
		return SnapshotCatalogEntry{}, false
	}
	var (
		bestHeight uint64
		best       SnapshotCatalogEntry
	)
	for height, entry := range n.snapshotCatalog {
		if height > bestHeight {
			bestHeight = height
			best = entry
		}
	}
	n.snapshotCatalogMu.RUnlock()
	if bestHeight == 0 {
		return SnapshotCatalogEntry{}, false
	}
	best.ProviderSet = append([]string{}, best.ProviderSet...)
	best.ProofSet = append([]string{}, best.ProofSet...)
	return best, true
}

func snapshotWarmupJoinHeight(appliedHeight uint64, warmupBlocks uint64) uint64 {
	if appliedHeight == 0 || warmupBlocks == 0 {
		return 0
	}
	const maxU64 = ^uint64(0)
	needed := warmupBlocks + 1
	if appliedHeight > maxU64-needed {
		return maxU64
	}
	return appliedHeight + needed
}

func (n *Node) setSnapshotWarmupJoinHeight(appliedHeight uint64) {
	if n == nil {
		return
	}
	n.syncMu.Lock()
	n.syncWarmupJoinHeight = 0
	n.syncWarmupStartAt = time.Now()
	n.syncWarmupLastHeight = appliedHeight
	n.syncWarmupLastHeightAt = time.Now()
	n.syncWarmupQuorumHash = ""
	n.syncWarmupQuorumVotes = 0
	n.syncWarmupQuorumSince = time.Time{}
	n.syncMu.Unlock()
}

func (n *Node) snapshotWarmupRemaining(currentHeight uint64) uint64 {
	if n == nil {
		return 0
	}
	currentHeight = n.snapshotWarmupReferenceHeight(currentHeight)
	active, remaining := n.snapshotWarmupState(currentHeight)
	if !active {
		return 0
	}
	seconds := uint64(math.Ceil(remaining.Seconds()))
	if seconds == 0 {
		seconds = 1
	}
	return seconds
}

func (n *Node) snapshotWarmupActive(currentHeight uint64) bool {
	if n == nil {
		return false
	}
	currentHeight = n.snapshotWarmupReferenceHeight(currentHeight)
	active, _ := n.snapshotWarmupState(currentHeight)
	return active
}

func (n *Node) snapshotWarmupReferenceHeight(requested uint64) uint64 {
	if n == nil || n.Blockchain == nil {
		return requested
	}
	tip := n.Blockchain.Height()
	if tip == 0 {
		return requested
	}
	if requested == 0 || requested > tip {
		return tip
	}
	return requested
}

func syncSnapshotWarmupDuration() time.Duration {
	seconds := SyncSnapshotWarmupSeconds
	if seconds == 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func (n *Node) snapshotWarmupQuorumState(currentHeight uint64) (int, int, string, bool, int) {
	if n == nil {
		return 0, 0, "", false, 0
	}
	validators := n.GetConsensusValidators(int(currentHeight))
	total := len(validators)
	if total == 0 {
		return 0, 0, "", false, 0
	}
	required := execQuorumRequired(total)
	hashVotes := make(map[string]int, total)
	now := time.Now()
	maxAge := validatorLivenessHeartbeatTTL() + validatorLivenessGrace()
	n.validatorMu.RLock()
	for _, id := range validators {
		st := n.validatorStatus[id]
		if st == nil {
			continue
		}
		if !st.LastSeen.IsZero() && now.Sub(st.LastSeen) > maxAge {
			continue
		}
		hash := strings.ToLower(strings.TrimSpace(st.ValidatorSetHash))
		if hash == "" {
			continue
		}
		hashVotes[hash]++
	}
	n.validatorMu.RUnlock()
	if total > 0 {
		localHash := strings.ToLower(strings.TrimSpace(n.validatorSetHashFromFinalizedSnapshot(currentHeight, validators)))
		if localHash != "" {
			hashVotes[localHash]++
		}
	}
	if len(hashVotes) == 0 {
		return 0, required, "", false, total
	}
	bestHash := ""
	bestVotes := 0
	for hash, votes := range hashVotes {
		if votes > bestVotes || (votes == bestVotes && (bestHash == "" || hash < bestHash)) {
			bestHash = hash
			bestVotes = votes
		}
	}
	single := len(hashVotes) == 1
	return bestVotes, required, bestHash, single, total
}

func (n *Node) snapshotWarmupState(currentHeight uint64) (bool, time.Duration) {
	if n == nil {
		return false, 0
	}
	if currentHeight == 0 && n.Blockchain != nil {
		currentHeight = n.Blockchain.Height()
	}
	warmupDuration := syncSnapshotWarmupDuration()
	n.syncMu.Lock()
	startAt := n.syncWarmupStartAt
	lastHeight := n.syncWarmupLastHeight
	lastHeightAt := n.syncWarmupLastHeightAt
	lastHash := n.syncWarmupQuorumHash
	lastVotes := n.syncWarmupQuorumVotes
	lastQuorumSince := n.syncWarmupQuorumSince
	n.syncMu.Unlock()
	if startAt.IsZero() {
		return false, 0
	}
	now := time.Now()
	warmupElapsed := now.Sub(startAt)
	if currentHeight != 0 && currentHeight != lastHeight {
		lastHeight = currentHeight
		lastHeightAt = now
	}
	if lastHeightAt.IsZero() {
		lastHeightAt = now
	}
	votes, required, hash, single, total := n.snapshotWarmupQuorumState(currentHeight)
	quorumStable := time.Duration(0)
	if votes >= required && single && hash != "" {
		if hash != lastHash || votes != lastVotes {
			lastQuorumSince = now
		}
		if lastQuorumSince.IsZero() {
			lastQuorumSince = now
		}
		quorumStable = now.Sub(lastQuorumSince)
	} else {
		lastQuorumSince = now
	}

	n.syncMu.Lock()
	n.syncWarmupLastHeight = lastHeight
	n.syncWarmupLastHeightAt = lastHeightAt
	n.syncWarmupQuorumHash = hash
	n.syncWarmupQuorumVotes = votes
	n.syncWarmupQuorumSince = lastQuorumSince
	n.syncMu.Unlock()

	if total < 3 {
		return true, warmupDuration
	}
	if votes < required || !single || hash == "" {
		return true, warmupDuration
	}
	if quorumStable >= warmupDuration && warmupElapsed >= warmupDuration {
		return false, 0
	}
	remaining := warmupDuration
	if quorumStable < remaining {
		remaining = quorumStable
	}
	if warmupElapsed < remaining {
		remaining = warmupElapsed
	}
	if remaining >= warmupDuration {
		return false, 0
	}
	return true, warmupDuration - remaining
}

func (n *Node) validatorSyncIsolationState(height uint64) (bool, string) {
	if n == nil {
		return false, ""
	}
	if session := n.snapshotSessionSnapshot(); session.Active {
		localHeight := uint64(0)
		if n.Blockchain != nil {
			localHeight = n.Blockchain.Height()
		}
		target := session.FreezeHeight
		if session.CandidateHeight > target {
			target = session.CandidateHeight
		}
		if target > localHeight || localHeight+1 < height {
			return true, "snapshot_session_active"
		}
		if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_session_participation_bypass:%d", height), 10*time.Second) {
			log.Printf("[SNAPSHOT-SESSION] participation_bypass height=%d local_height=%d target=%d stage=%s retries=%d",
				height,
				localHeight,
				target,
				normalizeSnapshotSyncStage(session.Stage),
				session.RetryCount,
			)
		}
	}
	if n.Consensus != nil {
		if blocked, reason, _ := n.consensusSyncGateForHeight(height); blocked {
			if strings.TrimSpace(reason) == "" {
				reason = "syncing"
			}
			return true, reason
		}
	}
	if n.snapshotWarmupActive(height) {
		return true, "warmup"
	}
	return false, ""
}
