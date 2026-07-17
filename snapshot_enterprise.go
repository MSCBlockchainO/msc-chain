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
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `ChunkCount` stores the measured quantity used by this operation.
	ChunkCount uint64 `json:"chunk_count"`
	// `ProviderSet` stores the value associated with this record.
	ProviderSet []string `json:"provider_set"`
	// `ProofSet` stores the value associated with this record.
	ProofSet []string `json:"proof_set"`
	// `AvailabilityRatio` stores the value associated with this record.
	AvailabilityRatio float64 `json:"availability_ratio"`
	// `UpdatedAtUnix` stores the value associated with this record.
	UpdatedAtUnix int64 `json:"updated_at_unix"`
}

const (
	maxSnapshotCatalogEntries    = 256
	maxSnapshotCatalogSetEntries = 128
	maxSnapshotCatalogValueBytes = 512
)

// syncSnapshotReplicationMinCopies implements the sync snapshot replication min copies helper.
func syncSnapshotReplicationMinCopies() int {
	if SyncSnapshotReplicationMinCopies <= 0 {
		return 3
	}
	return SyncSnapshotReplicationMinCopies
}

// syncSnapshotWarmupBlocks implements the sync snapshot warmup blocks helper.
func syncSnapshotWarmupBlocks() uint64 {
	if SyncSnapshotWarmupBlocks == 0 {
		return 5
	}
	return SyncSnapshotWarmupBlocks
}

// normalizeSnapshotCatalogSet normalizes snapshot catalog set.
func normalizeSnapshotCatalogSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(values))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(values))
	// `value` tracks the value currently being processed.
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > maxSnapshotCatalogValueBytes {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
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
	if len(out) > maxSnapshotCatalogSetEntries {
		out = append([]string{}, out[len(out)-maxSnapshotCatalogSetEntries:]...)
	}
	return out
}

// mergeSnapshotCatalogSet implements the merge snapshot catalog set helper.
func mergeSnapshotCatalogSet(current []string, updates []string) []string {
	current = normalizeSnapshotCatalogSet(current)
	updates = normalizeSnapshotCatalogSet(updates)
	if len(current) == 0 {
		return updates
	}
	if len(updates) == 0 {
		return current
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(current)+len(updates))
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(current)+len(updates))
	// `value` tracks the value currently being processed.
	for _, value := range current {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	// `value` tracks the value currently being processed.
	for _, value := range updates {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) > maxSnapshotCatalogSetEntries {
		out = append([]string{}, out[len(out)-maxSnapshotCatalogSetEntries:]...)
	}
	return out
}

func boundSnapshotCatalogLocked(catalog map[uint64]SnapshotCatalogEntry) {
	for len(catalog) > maxSnapshotCatalogEntries {
		oldestHeight := uint64(0)
		oldestUpdatedAt := int64(0)
		for height, entry := range catalog {
			if oldestHeight == 0 || entry.UpdatedAtUnix < oldestUpdatedAt ||
				(entry.UpdatedAtUnix == oldestUpdatedAt && height < oldestHeight) {
				oldestHeight = height
				oldestUpdatedAt = entry.UpdatedAtUnix
			}
		}
		if oldestHeight == 0 {
			return
		}
		delete(catalog, oldestHeight)
	}
}

// clampSnapshotAvailabilityRatio implements the clamp snapshot availability ratio helper.
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

// snapshotCatalogAvailabilityRatio implements the snapshot catalog availability ratio helper.
func snapshotCatalogAvailabilityRatio(providerCount int, minCopies int) float64 {
	if providerCount <= 0 {
		return 0
	}
	if minCopies <= 0 {
		minCopies = 1
	}
	// `available` stores the value produced by this operation.
	available := providerCount
	if available > minCopies {
		available = minCopies
	}
	return clampSnapshotAvailabilityRatio(float64(available) / float64(minCopies))
}

// updateSnapshotCatalogMeta implements the update snapshot catalog meta helper.
func (n *Node) updateSnapshotCatalogMeta(height uint64, stateRoot string, chunkCount uint64, providers []string) {
	if n == nil || height == 0 {
		return
	}
	providers = normalizeSnapshotCatalogSet(providers)
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	// `entry` stores the value produced by this operation.
	entry := n.snapshotCatalog[height]
	entry.Height = height
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot != "" && len(stateRoot) <= maxSnapshotCatalogValueBytes {
		entry.StateRoot = stateRoot
	}
	if chunkCount > 0 && chunkCount <= maxSnapshotTransferChunkCount {
		entry.ChunkCount = chunkCount
	}
	entry.ProviderSet = mergeSnapshotCatalogSet(entry.ProviderSet, providers)
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	boundSnapshotCatalogLocked(n.snapshotCatalog)
	n.snapshotCatalogMu.Unlock()
}

// updateSnapshotCatalogProofSet implements the update snapshot catalog proof set helper.
func (n *Node) updateSnapshotCatalogProofSet(height uint64, validators []string) {
	if n == nil || height == 0 {
		return
	}
	validators = normalizeSnapshotCatalogSet(validators)
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	// `entry` stores the value produced by this operation.
	entry := n.snapshotCatalog[height]
	entry.Height = height
	entry.ProofSet = mergeSnapshotCatalogSet(entry.ProofSet, validators)
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	boundSnapshotCatalogLocked(n.snapshotCatalog)
	n.snapshotCatalogMu.Unlock()
}

// updateSnapshotCatalogAvailability implements the update snapshot catalog availability helper.
func (n *Node) updateSnapshotCatalogAvailability(height uint64, ratio float64) {
	if n == nil || height == 0 {
		return
	}
	n.snapshotCatalogMu.Lock()
	if n.snapshotCatalog == nil {
		n.snapshotCatalog = make(map[uint64]SnapshotCatalogEntry)
	}
	// `entry` stores the value produced by this operation.
	entry := n.snapshotCatalog[height]
	entry.Height = height
	entry.AvailabilityRatio = clampSnapshotAvailabilityRatio(ratio)
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	boundSnapshotCatalogLocked(n.snapshotCatalog)
	n.snapshotCatalogMu.Unlock()
}

// updateSnapshotCatalogProviders implements the update snapshot catalog providers helper.
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
	// `entry` stores the value produced by this operation.
	entry := n.snapshotCatalog[height]
	entry.Height = height
	entry.ProviderSet = mergeSnapshotCatalogSet(entry.ProviderSet, providers)
	// `computed` stores the value produced by this operation.
	computed := snapshotCatalogAvailabilityRatio(len(entry.ProviderSet), syncSnapshotReplicationMinCopies())
	// Keep availability monotonic unless a stricter ratio is explicitly set elsewhere.
	if computed > entry.AvailabilityRatio {
		entry.AvailabilityRatio = computed
	}
	entry.UpdatedAtUnix = time.Now().Unix()
	n.snapshotCatalog[height] = entry
	boundSnapshotCatalogLocked(n.snapshotCatalog)
	n.snapshotCatalogMu.Unlock()
}

// snapshotCatalogEntry implements the snapshot catalog entry helper.
func (n *Node) snapshotCatalogEntry(height uint64) (SnapshotCatalogEntry, bool) {
	if n == nil || height == 0 {
		return SnapshotCatalogEntry{}, false
	}
	n.snapshotCatalogMu.RLock()
	// `entry` and `ok` store whether the related condition is satisfied.
	entry, ok := n.snapshotCatalog[height]
	n.snapshotCatalogMu.RUnlock()
	if !ok || entry.Height == 0 {
		return SnapshotCatalogEntry{}, false
	}
	entry.ProviderSet = append([]string{}, entry.ProviderSet...)
	entry.ProofSet = append([]string{}, entry.ProofSet...)
	return entry, true
}

// latestSnapshotCatalogEntry implements the latest snapshot catalog entry helper.
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
		// `bestHeight` stores the value used by this operation.
		bestHeight uint64
		// `best` stores the value used by this operation.
		best SnapshotCatalogEntry
	)
	// `height` and `entry` track the current values while iterating.
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

// snapshotWarmupJoinHeight implements the snapshot warmup join height helper.
func snapshotWarmupJoinHeight(appliedHeight uint64, warmupBlocks uint64) uint64 {
	if appliedHeight == 0 || warmupBlocks == 0 {
		return 0
	}
	// `maxU64` defines the constant value used by this package.
	const maxU64 = ^uint64(0)
	// `needed` stores the value produced by this operation.
	needed := warmupBlocks + 1
	if appliedHeight > maxU64-needed {
		return maxU64
	}
	return appliedHeight + needed
}

// setSnapshotWarmupJoinHeight implements the set snapshot warmup join height helper.
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

// clearSnapshotWarmupStateIfCurrent implements the clear snapshot warmup state if current helper.
func (n *Node) clearSnapshotWarmupStateIfCurrent(startAt time.Time) bool {
	if n == nil || startAt.IsZero() {
		return false
	}
	n.syncMu.Lock()
	defer n.syncMu.Unlock()
	if !n.syncWarmupStartAt.Equal(startAt) {
		return false
	}
	n.syncWarmupJoinHeight = 0
	n.syncWarmupStartAt = time.Time{}
	n.syncWarmupLastHeight = 0
	n.syncWarmupLastHeightAt = time.Time{}
	n.syncWarmupQuorumHash = ""
	n.syncWarmupQuorumVotes = 0
	n.syncWarmupQuorumSince = time.Time{}
	return true
}

// snapshotWarmupRemaining implements the snapshot warmup remaining helper.
func (n *Node) snapshotWarmupRemaining(currentHeight uint64) uint64 {
	if n == nil {
		return 0
	}
	currentHeight = n.snapshotWarmupReferenceHeight(currentHeight)
	// `active` and `remaining` store the value produced by this operation.
	active, remaining := n.snapshotWarmupState(currentHeight)
	if !active {
		return 0
	}
	// `seconds` stores the value produced by this operation.
	seconds := uint64(math.Ceil(remaining.Seconds()))
	if seconds == 0 {
		seconds = 1
	}
	return seconds
}

// snapshotWarmupActive implements the snapshot warmup active helper.
func (n *Node) snapshotWarmupActive(currentHeight uint64) bool {
	if n == nil {
		return false
	}
	currentHeight = n.snapshotWarmupReferenceHeight(currentHeight)
	// `active` stores the value produced by this operation.
	active, _ := n.snapshotWarmupState(currentHeight)
	return active
}

// snapshotWarmupReferenceHeight implements the snapshot warmup reference height helper.
func (n *Node) snapshotWarmupReferenceHeight(requested uint64) uint64 {
	if n == nil || n.Blockchain == nil {
		return requested
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if tip == 0 {
		return requested
	}
	if requested == 0 || requested > tip {
		return tip
	}
	return requested
}

// syncSnapshotWarmupDuration implements the sync snapshot warmup duration helper.
func syncSnapshotWarmupDuration() time.Duration {
	// `seconds` stores the value produced by this operation.
	seconds := SyncSnapshotWarmupSeconds
	if seconds == 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

// snapshotWarmupQuorumState implements the snapshot warmup quorum state helper.
func (n *Node) snapshotWarmupQuorumState(currentHeight uint64) (int, int, string, bool, int) {
	if n == nil {
		return 0, 0, "", false, 0
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.GetConsensusValidators(int(currentHeight))
	// `total` stores the measured quantity used by this operation.
	total := len(validators)
	if total == 0 {
		return 0, 0, "", false, 0
	}
	// `required` stores the request data being processed.
	required := execQuorumRequired(total)
	// `validatorSet` stores whether the related condition is satisfied.
	validatorSet := make(map[string]struct{}, total)
	// `id` tracks the current position in the related collection.
	for _, id := range validators {
		// `norm` stores the value produced by this operation.
		if norm := normalizeValidatorID(id); norm != "" {
			validatorSet[norm] = struct{}{}
		}
	}
	// `hashVotes` stores the digest used to identify or verify the related data.
	hashVotes := make(map[string]int, total)
	// `counted` stores the measured quantity used by this operation.
	counted := make(map[string]struct{}, total)
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `maxAge` stores the value produced by this operation.
	maxAge := validatorLivenessHeartbeatTTL() + validatorLivenessGrace()
	n.validatorMu.RLock()
	// `id` tracks the current position in the related collection.
	for _, id := range validators {
		// `normID` stores the value produced by this operation.
		normID := normalizeValidatorID(id)
		if normID == "" {
			continue
		}
		// `st` stores the value produced by this operation.
		st := n.validatorStatus[normID]
		if st == nil {
			continue
		}
		if !st.LastSeen.IsZero() && now.Sub(st.LastSeen) > maxAge {
			continue
		}
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.ToLower(strings.TrimSpace(st.ValidatorSetHash))
		if hash == "" {
			continue
		}
		hashVotes[hash]++
		counted[normID] = struct{}{}
	}
	n.validatorMu.RUnlock()
	if total > 0 {
		// `localHash` stores the digest used to identify or verify the related data.
		localHash := strings.ToLower(strings.TrimSpace(n.validatorSetHashFromFinalizedSnapshot(currentHeight, validators)))
		// `selfID` stores the value produced by this operation.
		selfID := n.localConsensusValidatorIDForSet(validators)
		if localHash != "" {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := validatorSet[selfID]; ok && selfID != "" {
				// `seen` stores the value produced by this operation.
				if _, seen := counted[selfID]; !seen {
					hashVotes[localHash]++
					counted[selfID] = struct{}{}
				}
			} else if selfID == "" {
				hashVotes[localHash]++
			}
		}
	}
	n.peerStateMu.Lock()
	// `peerID` and `role` track the current values while iterating.
	for peerID, role := range n.peerRole {
		if normalizeNodeRole(role) != "validator" || !n.peerHelloOK[peerID] {
			continue
		}
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := normalizeValidatorID(n.peerToValidator[peerID])
		if validatorID == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := validatorSet[validatorID]; !ok {
			continue
		}
		// `seen` stores the value produced by this operation.
		if _, seen := counted[validatorID]; seen {
			continue
		}
		// `ackHeight` stores the value produced by this operation.
		ackHeight := n.peerAckHeight[peerID]
		if currentHeight > 0 && ackHeight > 0 && !nearSyncTip(ackHeight, currentHeight) {
			continue
		}
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.ToLower(strings.TrimSpace(n.peerSetHash[peerID]))
		if hash != "" {
			hashVotes[hash]++
			counted[validatorID] = struct{}{}
		}
	}
	n.peerStateMu.Unlock()
	if len(hashVotes) == 0 {
		return 0, required, "", false, total
	}
	// `bestHash` stores the digest used to identify or verify the related data.
	bestHash := ""
	// `bestVotes` stores the value produced by this operation.
	bestVotes := 0
	// `hash` and `votes` track the digest used to identify or verify the related data.
	for hash, votes := range hashVotes {
		if votes > bestVotes || (votes == bestVotes && (bestHash == "" || hash < bestHash)) {
			bestHash = hash
			bestVotes = votes
		}
	}
	// `single` stores the value produced by this operation.
	single := len(hashVotes) == 1
	return bestVotes, required, bestHash, single, total
}

// snapshotWarmupState implements the snapshot warmup state helper.
func (n *Node) snapshotWarmupState(currentHeight uint64) (bool, time.Duration) {
	if n == nil {
		return false, 0
	}
	if currentHeight == 0 && n.Blockchain != nil {
		currentHeight = n.Blockchain.Height()
	}
	// `warmupDuration` stores the value produced by this operation.
	warmupDuration := syncSnapshotWarmupDuration()
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.syncMu.Lock()
	// `startAt` stores the value produced by this operation.
	startAt := n.syncWarmupStartAt
	if startAt.IsZero() {
		n.syncMu.Unlock()
		return false, 0
	}
	if currentHeight != 0 && (n.syncWarmupLastHeight == 0 || currentHeight > n.syncWarmupLastHeight) {
		n.syncWarmupLastHeight = currentHeight
		n.syncWarmupLastHeightAt = now
	} else if n.syncWarmupLastHeightAt.IsZero() {
		n.syncWarmupLastHeightAt = now
	}
	n.syncMu.Unlock()

	// `warmupElapsed` stores the value produced by this operation.
	warmupElapsed := now.Sub(startAt)
	// `votes`, `required`, `hash`, `single`, and `total` store the digest used to identify or verify the related data.
	votes, required, hash, single, total := n.snapshotWarmupQuorumState(currentHeight)
	// `quorumStable` stores the value produced by this operation.
	quorumStable := time.Duration(0)
	// `stableHash` stores the digest used to identify or verify the related data.
	stableHash := single && hash != ""
	// `lastQuorumSince` stores the value produced by this operation.
	var lastQuorumSince time.Time
	n.syncMu.Lock()
	if !n.syncWarmupStartAt.Equal(startAt) {
		n.syncMu.Unlock()
		return false, 0
	}
	if stableHash {
		// `lastHash` stores the digest used to identify or verify the related data.
		lastHash := n.syncWarmupQuorumHash
		// `lastVotes` stores the value produced by this operation.
		lastVotes := n.syncWarmupQuorumVotes
		lastQuorumSince = n.syncWarmupQuorumSince
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
	n.syncWarmupQuorumHash = hash
	n.syncWarmupQuorumVotes = votes
	n.syncWarmupQuorumSince = lastQuorumSince
	n.syncMu.Unlock()

	// `localTrusted` stores the value produced by this operation.
	localTrusted := false
	if hash != "" {
		// `validators` stores whether the related condition is satisfied.
		validators := n.GetConsensusValidators(int(currentHeight))
		// `localHash` stores the digest used to identify or verify the related data.
		localHash := strings.ToLower(strings.TrimSpace(n.validatorSetHashFromFinalizedSnapshot(currentHeight, validators)))
		localTrusted = localHash != "" && strings.EqualFold(localHash, hash)
	}

	if total < 3 && !localTrusted {
		return true, warmupDuration
	}
	if !stableHash {
		return true, warmupDuration
	}
	if votes >= required && quorumStable >= warmupDuration && warmupElapsed >= warmupDuration {
		n.clearSnapshotWarmupStateIfCurrent(startAt)
		return false, 0
	}
	if localTrusted && quorumStable >= warmupDuration && warmupElapsed >= warmupDuration {
		n.clearSnapshotWarmupStateIfCurrent(startAt)
		return false, 0
	}
	// `remaining` stores the value produced by this operation.
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

// validatorSyncIsolationState implements the validator sync isolation state helper.
func (n *Node) validatorSyncIsolationState(height uint64) (bool, string) {
	if n == nil {
		return false, ""
	}
	// `session` stores the value produced by this operation.
	if session := n.snapshotSessionSnapshot(); session.Active {
		// `localHeight` stores the value produced by this operation.
		localHeight := uint64(0)
		if n.Blockchain != nil {
			localHeight = n.Blockchain.Height()
		}
		// `target` stores the value produced by this operation.
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
		// `blocked` and `reason` store the block data handled by this operation.
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
