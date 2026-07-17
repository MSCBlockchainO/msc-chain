package main

import (
	"sort"
	"strings"
	"time"
)

func dedupeValidatorSetHashes(hashes []string) []string {
	if len(hashes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(hashes))
	out := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		trimmed := strings.TrimSpace(hash)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (n *Node) validatorSetCandidateHashesForHeight(height uint64, values []string, registrySnapshot map[string]ValidatorRecord) []string {
	canonical := canonicalValidatorIDs(append([]string{}, values...))
	if len(canonical) == 0 || height == 0 {
		return nil
	}
	hashes := make([]string, 0, 3)
	if n != nil {
		if hash := strings.TrimSpace(n.validatorSetHashFromFinalizedSnapshot(height, canonical)); hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if len(registrySnapshot) == 0 && n != nil {
		registrySnapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if len(registrySnapshot) > 0 {
		if hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(height, canonical, registrySnapshot)); hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if hash := strings.TrimSpace(ValidatorSetHash(canonical)); hash != "" {
		hashes = append(hashes, hash)
	}
	return dedupeValidatorSetHashes(hashes)
}

func (n *Node) preferredValidatorSetHashForHeight(height uint64, values []string, registrySnapshot map[string]ValidatorRecord) string {
	hashes := n.validatorSetCandidateHashesForHeight(height, values, registrySnapshot)
	if len(hashes) == 0 {
		return ""
	}
	return strings.TrimSpace(hashes[0])
}

func (n *Node) validatorSetCandidateMatchesTarget(height uint64, targetHash string, values []string, registrySnapshot map[string]ValidatorRecord) ([]string, bool) {
	canonical := canonicalValidatorIDs(append([]string{}, values...))
	if len(canonical) == 0 {
		return nil, false
	}
	target := strings.TrimSpace(targetHash)
	if target == "" {
		return canonical, true
	}
	for _, hash := range n.validatorSetCandidateHashesForHeight(height, canonical, registrySnapshot) {
		if strings.EqualFold(strings.TrimSpace(hash), target) {
			return canonical, true
		}
	}
	return nil, false
}

func (n *Node) validatorSetIDsMatchCommittedHash(height uint64, targetHash string, values []string) bool {
	target := strings.TrimSpace(targetHash)
	canonical := canonicalValidatorIDs(append([]string{}, values...))
	if target == "" || len(canonical) == 0 || height == 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(ValidatorSetHash(canonical)), target) {
		return true
	}
	registrySnapshot := map[string]ValidatorRecord(nil)
	if n != nil {
		registrySnapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if len(registrySnapshot) > 0 {
		if hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(height, canonical, registrySnapshot)); hash != "" && strings.EqualFold(hash, target) {
			return true
		}
	}
	return false
}

func (n *Node) frozenValidatorsForCommittedHash(targetHash string, preferredHeights ...uint64) []string {
	if n == nil {
		return nil
	}
	target := strings.TrimSpace(targetHash)
	if target == "" {
		return nil
	}
	type candidate struct {
		height uint64
		values []string
	}
	candidates := make([]candidate, 0, 4)
	n.validatorSetMu.RLock()
	if n.frozenValidatorHashByHeight != nil && n.frozenValidatorsByHeight != nil {
		for _, height := range preferredHeights {
			if height == 0 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(n.frozenValidatorHashByHeight[height]), target) {
				if values := canonicalValidatorIDs(n.frozenValidatorsByHeight[height]); len(values) > 0 {
					n.validatorSetMu.RUnlock()
					if matched, ok := n.validatorSetCandidateMatchesTarget(height, target, values, nil); ok {
						return matched
					}
					n.validatorSetMu.RLock()
				}
			}
		}
		for height, hash := range n.frozenValidatorHashByHeight {
			if !strings.EqualFold(strings.TrimSpace(hash), target) {
				continue
			}
			if values := canonicalValidatorIDs(n.frozenValidatorsByHeight[height]); len(values) > 0 {
				candidates = append(candidates, candidate{height: height, values: values})
			}
		}
	}
	n.validatorSetMu.RUnlock()
	if len(candidates) == 0 {
		return nil
	}
	verified := candidates[:0]
	for _, candidate := range candidates {
		if matched, ok := n.validatorSetCandidateMatchesTarget(candidate.height, target, candidate.values, nil); ok {
			candidate.values = matched
			verified = append(verified, candidate)
		}
	}
	candidates = verified
	if len(candidates) == 0 {
		return nil
	}
	preferred := uint64(0)
	for _, height := range preferredHeights {
		if height > 0 {
			preferred = height
			break
		}
	}
	distance := func(height uint64) uint64 {
		if preferred == 0 {
			return 0
		}
		if height > preferred {
			return height - preferred
		}
		return preferred - height
	}
	sort.Slice(candidates, func(i, j int) bool {
		di, dj := distance(candidates[i].height), distance(candidates[j].height)
		if di != dj {
			return di < dj
		}
		return candidates[i].height > candidates[j].height
	})
	return append([]string{}, candidates[0].values...)
}

func (n *Node) committedFrozenValidatorSetCandidate(targetHash string, preferredHeights ...uint64) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return nil, false
	}
	type candidate struct {
		height uint64
		values []string
	}
	candidates := make([]candidate, 0, len(preferredHeights))
	n.validatorSetMu.RLock()
	for _, height := range preferredHeights {
		if height == 0 {
			continue
		}
		hash := strings.TrimSpace(n.frozenValidatorHashByHeight[height])
		if hash == "" || !strings.EqualFold(hash, targetHash) {
			continue
		}
		if values := canonicalValidatorIDs(n.frozenValidatorsByHeight[height]); len(values) > 0 {
			candidates = append(candidates, candidate{height: height, values: values})
		}
	}
	n.validatorSetMu.RUnlock()
	for _, candidate := range candidates {
		if matched, ok := n.validatorSetCandidateMatchesTarget(candidate.height, targetHash, candidate.values, nil); ok {
			return matched, true
		}
	}
	return nil, false
}

func (n *Node) reconstructValidatorSetCandidateForTarget(height uint64, targetHash string, registrySnapshot map[string]ValidatorRecord, candidates ...[]string) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return nil, false
	}
	universeSet := make(map[string]struct{}, len(registrySnapshot)+len(candidates)*4)
	add := func(values []string) {
		for _, id := range canonicalValidatorIDs(values) {
			if id == "" {
				continue
			}
			universeSet[id] = struct{}{}
		}
	}
	for _, candidate := range candidates {
		add(candidate)
	}
	if len(registrySnapshot) > 0 {
		add(canonicalValidatorIDsFromMapKeys(registrySnapshot))
	}
	if len(universeSet) == 0 {
		return nil, false
	}
	universe := make([]string, 0, len(universeSet))
	for id := range universeSet {
		universe = append(universe, id)
	}
	return n.findValidatorSubsetByHash(height, universe, targetHash, registrySnapshot)
}

func validatorRegistrySnapshotFromStateSnapshot(snapshot *StateSnapshot) map[string]ValidatorRecord {
	if snapshot == nil {
		return nil
	}
	if len(snapshot.ValidatorRegistry) > 0 {
		return copyValidatorRegistrySnapshot(snapshot.ValidatorRegistry)
	}
	if len(snapshot.StateValidators) > 0 {
		return validatorRegistrySnapshotFromOnChainValidators(snapshot.StateValidators)
	}
	return nil
}

func (n *Node) validatorSetCandidateFromSnapshot(height uint64, targetHash string, snapshot *StateSnapshot) ([]string, string, string, bool) {
	if n == nil || snapshot == nil || height == 0 {
		return nil, "", "none", false
	}
	registrySnapshot := validatorRegistrySnapshotFromStateSnapshot(snapshot)
	snapshotHash := strings.TrimSpace(snapshotValidatorSetHash(snapshot))
	if strings.TrimSpace(targetHash) == "" {
		targetHash = snapshotHash
	}
	targetHash = strings.TrimSpace(targetHash)

	if out := validatorsFromSnapshot(snapshot); len(out) > 0 {
		if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, out, registrySnapshot); ok {
			resolvedHash := snapshotHash
			if resolvedHash == "" {
				resolvedHash = strings.TrimSpace(targetHash)
			}
			if resolvedHash == "" {
				resolvedHash = strings.TrimSpace(n.preferredValidatorSetHashForHeight(height, matched, registrySnapshot))
			}
			if resolvedHash == "" {
				resolvedHash = ValidatorSetHash(matched)
			}
			return matched, resolvedHash, "snapshot_committed", true
		}
		if targetHash != "" {
			return nil, "", "none", false
		}
	}

	if targetHash == "" || len(registrySnapshot) == 0 {
		return nil, "", "none", false
	}
	if matched, ok := n.reconstructValidatorSetCandidateForTarget(height, targetHash, registrySnapshot); ok {
		return matched, targetHash, "snapshot_committed", true
	}
	return nil, "", "none", false
}

func (n *Node) validatorSetCandidateFromStoredSnapshot(height uint64, targetHash string) ([]string, string, string, bool) {
	if n == nil || height <= 1 || n.DB == nil || n.DB.State == nil {
		return nil, "", "none", false
	}
	snapshot, err := n.GetSnapshot(height - 1)
	if err != nil || snapshot == nil {
		return nil, "", "none", false
	}
	return n.validatorSetCandidateFromSnapshot(height, targetHash, snapshot)
}

func (n *Node) syncHeartbeatValidatorSetCandidate(height uint64, targetHash string) ([]string, bool) {
	if n == nil || height == 0 || strings.TrimSpace(targetHash) == "" {
		return nil, false
	}
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return nil, false
	}
	const heartbeatSetTTL = 2 * time.Minute
	now := time.Now()
	minHeight := height
	if minHeight > 0 {
		minHeight--
	}
	ids := make([]string, 0, 8)
	hashMatchedIDs := make([]string, 0, 8)
	n.validatorMu.RLock()
	for id, st := range n.validatorStatus {
		if st == nil || !st.Active {
			continue
		}
		if !st.LastSeen.IsZero() && now.Sub(st.LastSeen) > heartbeatSetTTL {
			continue
		}
		reported := st.FinalizedHeight
		if st.ReportedHeight > reported {
			reported = st.ReportedHeight
		}
		if st.ExecEpoch > reported {
			reported = st.ExecEpoch
		}
		if st.ValidatorSetHeight > reported {
			reported = st.ValidatorSetHeight
		}
		statusHashMatches := strings.EqualFold(strings.TrimSpace(st.ValidatorSetHash), strings.TrimSpace(targetHash))
		if reported < minHeight && !statusHashMatches {
			continue
		}
		ids = append(ids, id)
		if statusHashMatches {
			hashMatchedIDs = append(hashMatchedIDs, id)
		}
	}
	n.validatorMu.RUnlock()
	matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, ids, nil)
	if ok {
		return matched, true
	}
	hashMatchedIDs = canonicalValidatorIDs(hashMatchedIDs)
	if len(hashMatchedIDs) >= execQuorumRequired(len(hashMatchedIDs)) && len(hashMatchedIDs) > 0 {
		return hashMatchedIDs, true
	}
	return nil, false
}

func (n *Node) syncPeerAdvertisedValidatorSetCandidate(height uint64, targetHash string) ([]string, bool) {
	if n == nil || height == 0 || strings.TrimSpace(targetHash) == "" {
		return nil, false
	}
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return nil, false
	}

	targetHash = strings.ToLower(strings.TrimSpace(targetHash))
	ids := make([]string, 0, 8)
	n.peerStateMu.Lock()
	for peerID, rawID := range n.peerToValidator {
		id := normalizeValidatorID(rawID)
		if id == "" {
			continue
		}
		if !n.connectedPeers[peerID] || !n.peerHelloOK[peerID] {
			continue
		}
		if normalizeNodeRole(n.peerRole[peerID]) != "validator" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(n.peerSetHash[peerID])) != targetHash {
			continue
		}
		ids = append(ids, id)
	}
	n.peerStateMu.Unlock()

	ids = canonicalValidatorIDs(ids)
	if len(ids) == 0 {
		return nil, false
	}
	if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, ids, nil); ok {
		return matched, true
	}
	required := execQuorumRequired(len(ids))
	if required <= 0 {
		required = 1
	}
	if len(ids) >= required {
		return ids, true
	}
	return nil, false
}

func (n *Node) resolveCommittedValidatorSetForHeight(height uint64) ([]string, string, string, bool) {
	if n == nil || n.Blockchain == nil || height == 0 {
		return nil, "", "none", false
	}

	var registrySnapshot map[string]ValidatorRecord
	registrySnapshotLoaded := false
	loadRegistrySnapshot := func() map[string]ValidatorRecord {
		if !registrySnapshotLoaded {
			registrySnapshot = n.validatorRegistrySnapshotForHeight(height)
			registrySnapshotLoaded = true
		}
		return registrySnapshot
	}
	tryTarget := func(targetHash string, source string, candidates ...[]string) ([]string, string, string, bool) {
		targetHash = strings.TrimSpace(targetHash)
		if targetHash == "" {
			return nil, "", "none", false
		}
		registry := loadRegistrySnapshot()
		for _, candidate := range candidates {
			if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, candidate, registry); ok {
				return matched, targetHash, source, true
			}
		}
		return nil, "", "none", false
	}
	reconstructTarget := func(targetHash string, source string, candidates ...[]string) ([]string, string, string, bool) {
		targetHash = strings.TrimSpace(targetHash)
		if targetHash == "" {
			return nil, "", "none", false
		}
		registry := loadRegistrySnapshot()
		if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(height, targetHash, registry, candidates...); ok {
			if len(registry) > 0 {
				source = "registry_verified"
			}
			return reconstructed, targetHash, source, true
		}
		return nil, "", "none", false
	}

	if block, ok := n.Blockchain.GetBlock(height); ok {
		targetHash := strings.TrimSpace(block.ValidatorSetHash)
		if frozen, ok := n.committedFrozenValidatorSetCandidate(targetHash, height); ok {
			return frozen, targetHash, "chain_pruned_frozen", true
		}
		candidates := make([][]string, 0, 3)
		if resolved, resolvedHash, source, ok := n.validatorSetCandidateFromStoredSnapshot(height, block.ValidatorSetHash); ok {
			return resolved, resolvedHash, source, true
		}
		if committed, ok := n.blockValidatorSetFromSignatures(block); ok && len(committed) > 0 {
			candidates = append(candidates, committed)
		}
		if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
			candidates = append(candidates, frozen)
			if frozenHash, ok := n.frozenValidatorSetHash(height); ok &&
				strings.EqualFold(strings.TrimSpace(frozenHash), strings.TrimSpace(block.ValidatorSetHash)) {
				return canonicalValidatorIDs(frozen), strings.TrimSpace(block.ValidatorSetHash), "chain_pruned_frozen", true
			}
		}
		if resolved, resolvedHash, source, ok := tryTarget(block.ValidatorSetHash, "chain_block_signatures", candidates...); ok {
			return resolved, resolvedHash, source, true
		}
		if resolved, resolvedHash, source, ok := reconstructTarget(block.ValidatorSetHash, "chain_block_signatures", candidates...); ok {
			return resolved, resolvedHash, source, true
		}
		if resolved, ok := n.syncPeerAdvertisedValidatorSetCandidate(height, block.ValidatorSetHash); ok {
			return resolved, strings.TrimSpace(block.ValidatorSetHash), "sync_peer_advertised_block_commitment", true
		}
	}

	chainHeight := n.Blockchain.Height()
	if chainHeight == 0 || height != chainHeight+1 || height <= 1 || !validatorSetCommitmentV2EnabledAt(height-1) {
		return nil, "", "none", false
	}

	parent, ok := n.Blockchain.GetBlock(height - 1)
	if !ok {
		return nil, "", "none", false
	}

	targetHash := strings.TrimSpace(parent.NextValidatorSetHash)
	if frozen, ok := n.committedFrozenValidatorSetCandidate(targetHash, height, parent.ID); ok {
		return frozen, targetHash, "chain_parent_commitment_carry_forward", true
	}
	candidates := make([][]string, 0, 5)
	if resolved, resolvedHash, source, ok := n.validatorSetCandidateFromStoredSnapshot(height, parent.NextValidatorSetHash); ok {
		return resolved, resolvedHash, source, true
	}
	if ctx := n.blockValidatorUpdatePlanContext(parent); ctx != nil {
		if planned := ctx.plannedValidatorsForHeight(height); len(planned) > 0 {
			candidates = append(candidates, planned)
		}
	}
	if planned := n.plannedValidatorSetForHeightFromChain(height); len(planned) > 0 {
		candidates = append(candidates, planned)
	}
	if committed, ok := n.blockValidatorSetFromSignatures(parent); ok && len(committed) > 0 {
		candidates = append(candidates, committed)
	}
	if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
		candidates = append(candidates, frozen)
	}
	if frozen := n.frozenValidatorsForHeight(parent.ID); len(frozen) > 0 {
		candidates = append(candidates, frozen)
	}
	if resolved, resolvedHash, source, ok := tryTarget(parent.NextValidatorSetHash, "chain_parent_commitment", candidates...); ok {
		return resolved, resolvedHash, source, true
	}
	if targetHash != "" {
		if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
			if frozenHash, ok := n.frozenValidatorSetHash(height); ok && strings.EqualFold(strings.TrimSpace(frozenHash), targetHash) {
				return canonicalValidatorIDs(frozen), targetHash, "chain_parent_pruned_frozen", true
			}
		}
		if frozen := n.frozenValidatorsForHeight(parent.ID); len(frozen) > 0 {
			if frozenHash, ok := n.frozenValidatorSetHash(parent.ID); ok && strings.EqualFold(strings.TrimSpace(frozenHash), targetHash) {
				return canonicalValidatorIDs(frozen), targetHash, "chain_parent_commitment_carry_forward", true
			}
			if strings.EqualFold(strings.TrimSpace(parent.ValidatorSetHash), targetHash) {
				return canonicalValidatorIDs(frozen), targetHash, "chain_parent_commitment_carry_forward", true
			}
		}
	}
	if resolved, resolvedHash, source, ok := reconstructTarget(parent.NextValidatorSetHash, "chain_parent_commitment", candidates...); ok {
		return resolved, resolvedHash, source, true
	}
	if resolved, ok := n.syncPeerAdvertisedValidatorSetCandidate(height, parent.NextValidatorSetHash); ok {
		return resolved, strings.TrimSpace(parent.NextValidatorSetHash), "sync_peer_advertised_chain_parent_commitment", true
	}
	if resolved, ok := n.syncHeartbeatValidatorSetCandidate(height, parent.NextValidatorSetHash); ok {
		return resolved, strings.TrimSpace(parent.NextValidatorSetHash), "sync_heartbeat_chain_parent_commitment", true
	}
	return nil, "", "none", false
}
