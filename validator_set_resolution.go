package main

import (
	"sort"
	"strings"
	"time"
)

// dedupeValidatorSetHashes deduplicates validator set hashes.
func dedupeValidatorSetHashes(hashes []string) []string {
	if len(hashes) == 0 {
		return nil
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(hashes))
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(hashes))
	// `hash` tracks the digest used to identify or verify the related data.
	for _, hash := range hashes {
		// `trimmed` stores the value produced by this operation.
		trimmed := strings.TrimSpace(hash)
		if trimmed == "" {
			continue
		}
		// `key` stores the key used to access the related value.
		key := strings.ToLower(trimmed)
		// `ok` stores whether the related condition is satisfied.
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

// validatorSetCandidateHashesForHeight implements the validator set candidate hashes for height helper.
func (n *Node) validatorSetCandidateHashesForHeight(height uint64, values []string, registrySnapshot map[string]ValidatorRecord) []string {
	// `canonical` stores the value produced by this operation.
	canonical := canonicalValidatorIDs(append([]string{}, values...))
	if len(canonical) == 0 || height == 0 {
		return nil
	}
	// `hashes` stores the digest used to identify or verify the related data.
	hashes := make([]string, 0, 3)
	if n != nil {
		// `hash` stores the digest used to identify or verify the related data.
		if hash := strings.TrimSpace(n.validatorSetHashFromFinalizedSnapshot(height, canonical)); hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if len(registrySnapshot) == 0 && n != nil {
		registrySnapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if len(registrySnapshot) > 0 {
		// `hash` stores the digest used to identify or verify the related data.
		if hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(height, canonical, registrySnapshot)); hash != "" {
			hashes = append(hashes, hash)
		}
	}
	// `hash` stores the digest used to identify or verify the related data.
	if hash := strings.TrimSpace(ValidatorSetHash(canonical)); hash != "" {
		hashes = append(hashes, hash)
	}
	return dedupeValidatorSetHashes(hashes)
}

// preferredValidatorSetHashForHeight implements the preferred validator set hash for height helper.
func (n *Node) preferredValidatorSetHashForHeight(height uint64, values []string, registrySnapshot map[string]ValidatorRecord) string {
	// `hashes` stores the digest used to identify or verify the related data.
	hashes := n.validatorSetCandidateHashesForHeight(height, values, registrySnapshot)
	if len(hashes) == 0 {
		return ""
	}
	return strings.TrimSpace(hashes[0])
}

// validatorSetCandidateMatchesTarget implements the validator set candidate matches target helper.
func (n *Node) validatorSetCandidateMatchesTarget(height uint64, targetHash string, values []string, registrySnapshot map[string]ValidatorRecord) ([]string, bool) {
	// `canonical` stores the value produced by this operation.
	canonical := canonicalValidatorIDs(append([]string{}, values...))
	if len(canonical) == 0 {
		return nil, false
	}
	// `target` stores the value produced by this operation.
	target := strings.TrimSpace(targetHash)
	if target == "" {
		return canonical, true
	}
	// `hash` tracks the digest used to identify or verify the related data.
	for _, hash := range n.validatorSetCandidateHashesForHeight(height, canonical, registrySnapshot) {
		if strings.EqualFold(strings.TrimSpace(hash), target) {
			return canonical, true
		}
	}
	return nil, false
}

// validatorSetIDsMatchCommittedHash implements the validator set i ds match committed hash helper.
func (n *Node) validatorSetIDsMatchCommittedHash(height uint64, targetHash string, values []string) bool {
	// `target` stores the value produced by this operation.
	target := strings.TrimSpace(targetHash)
	// `canonical` stores the value produced by this operation.
	canonical := canonicalValidatorIDs(append([]string{}, values...))
	if target == "" || len(canonical) == 0 || height == 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(ValidatorSetHash(canonical)), target) {
		return true
	}
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := map[string]ValidatorRecord(nil)
	if n != nil {
		registrySnapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if len(registrySnapshot) > 0 {
		// `hash` stores the digest used to identify or verify the related data.
		if hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(height, canonical, registrySnapshot)); hash != "" && strings.EqualFold(hash, target) {
			return true
		}
	}
	return false
}

// frozenValidatorsForCommittedHash implements the frozen validators for committed hash helper.
func (n *Node) frozenValidatorsForCommittedHash(targetHash string, preferredHeights ...uint64) []string {
	if n == nil {
		return nil
	}
	// `target` stores the value produced by this operation.
	target := strings.TrimSpace(targetHash)
	if target == "" {
		return nil
	}
	type candidate struct {
		// `height` stores the value associated with this record.
		height uint64
		// `values` stores the value currently being processed.
		values []string
	}
	// `candidates` stores the value produced by this operation.
	candidates := make([]candidate, 0, 4)
	n.validatorSetMu.RLock()
	if n.frozenValidatorHashByHeight != nil && n.frozenValidatorsByHeight != nil {
		// `height` tracks the current values while iterating.
		for _, height := range preferredHeights {
			if height == 0 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(n.frozenValidatorHashByHeight[height]), target) {
				// `values` stores the value currently being processed.
				if values := canonicalValidatorIDs(n.frozenValidatorsByHeight[height]); len(values) > 0 {
					n.validatorSetMu.RUnlock()
					// `matched` and `ok` store whether the related condition is satisfied.
					if matched, ok := n.validatorSetCandidateMatchesTarget(height, target, values, nil); ok {
						return matched
					}
					n.validatorSetMu.RLock()
				}
			}
		}
		// `height` and `hash` track the digest used to identify or verify the related data.
		for height, hash := range n.frozenValidatorHashByHeight {
			if !strings.EqualFold(strings.TrimSpace(hash), target) {
				continue
			}
			// `values` stores the value currently being processed.
			if values := canonicalValidatorIDs(n.frozenValidatorsByHeight[height]); len(values) > 0 {
				candidates = append(candidates, candidate{height: height, values: values})
			}
		}
	}
	n.validatorSetMu.RUnlock()
	if len(candidates) == 0 {
		return nil
	}
	// `verified` stores the value produced by this operation.
	verified := candidates[:0]
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		// `matched` and `ok` store whether the related condition is satisfied.
		if matched, ok := n.validatorSetCandidateMatchesTarget(candidate.height, target, candidate.values, nil); ok {
			candidate.values = matched
			verified = append(verified, candidate)
		}
	}
	candidates = verified
	if len(candidates) == 0 {
		return nil
	}
	// `preferred` stores the value produced by this operation.
	preferred := uint64(0)
	// `height` tracks the current values while iterating.
	for _, height := range preferredHeights {
		if height > 0 {
			preferred = height
			break
		}
	}
	// `distance` stores the value produced by this operation.
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
		// `di` and `dj` store the current position in the related collection.
		di, dj := distance(candidates[i].height), distance(candidates[j].height)
		if di != dj {
			return di < dj
		}
		return candidates[i].height > candidates[j].height
	})
	return append([]string{}, candidates[0].values...)
}

// committedFrozenValidatorSetCandidate implements the committed frozen validator set candidate helper.
func (n *Node) committedFrozenValidatorSetCandidate(targetHash string, preferredHeights ...uint64) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return nil, false
	}
	type candidate struct {
		// `height` stores the value associated with this record.
		height uint64
		// `values` stores the value currently being processed.
		values []string
	}
	// `candidates` stores the value produced by this operation.
	candidates := make([]candidate, 0, len(preferredHeights))
	n.validatorSetMu.RLock()
	// `height` tracks the current values while iterating.
	for _, height := range preferredHeights {
		if height == 0 {
			continue
		}
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.TrimSpace(n.frozenValidatorHashByHeight[height])
		if hash == "" || !strings.EqualFold(hash, targetHash) {
			continue
		}
		// `values` stores the value currently being processed.
		if values := canonicalValidatorIDs(n.frozenValidatorsByHeight[height]); len(values) > 0 {
			candidates = append(candidates, candidate{height: height, values: values})
		}
	}
	n.validatorSetMu.RUnlock()
	// Frozen set values and their hash are recorded together only after the
	// commitment has been resolved. An exact stored-hash match is therefore the
	// verified fast path; reconstructing it here can require unavailable
	// historical registry state and turn a valid committed cache into a
	// node-local acceptance difference.
	for _, candidate := range candidates {
		return append([]string{}, candidate.values...), true
	}
	return nil, false
}

// reconstructValidatorSetCandidateForTarget implements the reconstruct validator set candidate for target helper.
func (n *Node) reconstructValidatorSetCandidateForTarget(height uint64, targetHash string, registrySnapshot map[string]ValidatorRecord, candidates ...[]string) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return nil, false
	}
	// `universeSet` stores the value produced by this operation.
	universeSet := make(map[string]struct{}, len(registrySnapshot)+len(candidates)*4)
	// `add` stores the value produced by this operation.
	add := func(values []string) {
		// `id` tracks the current position in the related collection.
		for _, id := range canonicalValidatorIDs(values) {
			if id == "" {
				continue
			}
			universeSet[id] = struct{}{}
		}
	}
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		add(candidate)
	}
	if len(registrySnapshot) > 0 {
		add(canonicalValidatorIDsFromMapKeys(registrySnapshot))
	}
	if len(universeSet) == 0 {
		return nil, false
	}
	// `universe` stores the value produced by this operation.
	universe := make([]string, 0, len(universeSet))
	// `id` tracks the current position in the related collection.
	for id := range universeSet {
		universe = append(universe, id)
	}
	return n.findValidatorSubsetByHash(height, universe, targetHash, registrySnapshot)
}

// validatorRegistrySnapshotFromStateSnapshot implements the validator registry snapshot from state snapshot helper.
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

// validatorSetCandidateFromSnapshot implements the validator set candidate from snapshot helper.
func (n *Node) validatorSetCandidateFromSnapshot(height uint64, targetHash string, snapshot *StateSnapshot) ([]string, string, string, bool) {
	if n == nil || snapshot == nil || height == 0 {
		return nil, "", "none", false
	}
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := validatorRegistrySnapshotFromStateSnapshot(snapshot)
	// `snapshotHash` stores the digest used to identify or verify the related data.
	snapshotHash := strings.TrimSpace(snapshotValidatorSetHash(snapshot))
	if strings.TrimSpace(targetHash) == "" {
		targetHash = snapshotHash
	}
	targetHash = strings.TrimSpace(targetHash)

	// `out` stores the result produced by this operation.
	if out := validatorsFromSnapshot(snapshot); len(out) > 0 {
		// `matched` and `ok` store whether the related condition is satisfied.
		if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, out, registrySnapshot); ok {
			// `resolvedHash` stores the digest used to identify or verify the related data.
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
	// `matched` and `ok` store whether the related condition is satisfied.
	if matched, ok := n.reconstructValidatorSetCandidateForTarget(height, targetHash, registrySnapshot); ok {
		return matched, targetHash, "snapshot_committed", true
	}
	return nil, "", "none", false
}

// validatorSetCandidateFromStoredSnapshot implements the validator set candidate from stored snapshot helper.
func (n *Node) validatorSetCandidateFromStoredSnapshot(height uint64, targetHash string) ([]string, string, string, bool) {
	if n == nil || height <= 1 || n.DB == nil || n.DB.State == nil {
		return nil, "", "none", false
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(height - 1)
	if err != nil || snapshot == nil {
		return nil, "", "none", false
	}
	return n.validatorSetCandidateFromSnapshot(height, targetHash, snapshot)
}

// syncHeartbeatValidatorSetCandidate implements the sync heartbeat validator set candidate helper.
func (n *Node) syncHeartbeatValidatorSetCandidate(height uint64, targetHash string) ([]string, bool) {
	if n == nil || height == 0 || strings.TrimSpace(targetHash) == "" {
		return nil, false
	}
	// `stage` stores the value produced by this operation.
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return nil, false
	}
	// `heartbeatSetTTL` defines the constant value used by this package.
	const heartbeatSetTTL = 2 * time.Minute
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `minHeight` stores the value produced by this operation.
	minHeight := height
	if minHeight > 0 {
		minHeight--
	}
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, 8)
	// `hashMatchedIDs` stores the digest used to identify or verify the related data.
	hashMatchedIDs := make([]string, 0, 8)
	n.validatorMu.RLock()
	// `id` and `st` track the current position in the related collection.
	for id, st := range n.validatorStatus {
		if st == nil || !st.Active {
			continue
		}
		if !st.LastSeen.IsZero() && now.Sub(st.LastSeen) > heartbeatSetTTL {
			continue
		}
		// `reported` stores the value produced by this operation.
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
		// `statusHashMatches` stores the value produced by this operation.
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
	// `matched` and `ok` store whether the related condition is satisfied.
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

// syncPeerAdvertisedValidatorSetCandidate implements the sync peer advertised validator set candidate helper.
func (n *Node) syncPeerAdvertisedValidatorSetCandidate(height uint64, targetHash string) ([]string, bool) {
	if n == nil || height == 0 || strings.TrimSpace(targetHash) == "" {
		return nil, false
	}
	// `stage` stores the value produced by this operation.
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return nil, false
	}

	targetHash = strings.ToLower(strings.TrimSpace(targetHash))
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, 8)
	n.peerStateMu.Lock()
	// `peerID` and `rawID` track the current values while iterating.
	for peerID, rawID := range n.peerToValidator {
		// `id` stores the current position in the related collection.
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
	// `matched` and `ok` store whether the related condition is satisfied.
	if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, ids, nil); ok {
		return matched, true
	}
	// `required` stores the request data being processed.
	required := execQuorumRequired(len(ids))
	if required <= 0 {
		required = 1
	}
	if len(ids) >= required {
		return ids, true
	}
	return nil, false
}

// resolveCommittedValidatorSetForHeight implements the resolve committed validator set for height helper.
func (n *Node) resolveCommittedValidatorSetForHeight(height uint64) ([]string, string, string, bool) {
	if n == nil || n.Blockchain == nil || height == 0 {
		return nil, "", "none", false
	}

	// `registrySnapshot` stores the value used by this operation.
	var registrySnapshot map[string]ValidatorRecord
	// `registrySnapshotLoaded` stores the value produced by this operation.
	registrySnapshotLoaded := false
	// `loadRegistrySnapshot` stores the value produced by this operation.
	loadRegistrySnapshot := func() map[string]ValidatorRecord {
		if !registrySnapshotLoaded {
			registrySnapshot = n.validatorRegistrySnapshotForHeight(height)
			registrySnapshotLoaded = true
		}
		return registrySnapshot
	}
	registryConfirmsActiveCandidate := func(values []string, registry map[string]ValidatorRecord) bool {
		values = canonicalValidatorIDs(values)
		if len(values) == 0 || len(registry) == 0 {
			return false
		}
		for _, id := range values {
			rec, ok := validatorRegistryRecordFromSnapshot(registry, id)
			if !ok || rec.Status != ValidatorActive {
				return false
			}
		}
		return true
	}
	// `tryTarget` stores the value produced by this operation.
	tryTarget := func(targetHash string, source string, candidates ...[]string) ([]string, string, string, bool) {
		targetHash = strings.TrimSpace(targetHash)
		if targetHash == "" {
			return nil, "", "none", false
		}
		// `registry` stores the value produced by this operation.
		registry := loadRegistrySnapshot()
		// `candidate` tracks the current values while iterating.
		for _, candidate := range candidates {
			// `matched` and `ok` store whether the related condition is satisfied.
			if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, candidate, registry); ok {
				if registryConfirmsActiveCandidate(matched, registry) {
					source = "registry_verified"
				}
				return matched, targetHash, source, true
			}
		}
		return nil, "", "none", false
	}
	// `reconstructTarget` stores the value produced by this operation.
	reconstructTarget := func(targetHash string, source string, candidates ...[]string) ([]string, string, string, bool) {
		targetHash = strings.TrimSpace(targetHash)
		if targetHash == "" {
			return nil, "", "none", false
		}
		// `registry` stores the value produced by this operation.
		registry := loadRegistrySnapshot()
		// `reconstructed` and `ok` store whether the related condition is satisfied.
		if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(height, targetHash, registry, candidates...); ok {
			if len(registry) > 0 {
				source = "registry_verified"
			}
			return reconstructed, targetHash, source, true
		}
		return nil, "", "none", false
	}

	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := n.Blockchain.GetBlock(height); ok {
		// `targetHash` stores the digest used to identify or verify the related data.
		targetHash := strings.TrimSpace(block.ValidatorSetHash)
		// `frozen` and `ok` store whether the related condition is satisfied.
		if frozen, ok := n.committedFrozenValidatorSetCandidate(targetHash, height); ok {
			return frozen, targetHash, "chain_pruned_frozen", true
		}
		// `candidates` stores the value produced by this operation.
		candidates := make([][]string, 0, 3)
		// `resolved`, `resolvedHash`, `source`, and `ok` store whether the related condition is satisfied.
		if resolved, resolvedHash, source, ok := n.validatorSetCandidateFromStoredSnapshot(height, block.ValidatorSetHash); ok {
			return resolved, resolvedHash, source, true
		}
		// `committed` and `ok` store whether the related condition is satisfied.
		if committed, ok := n.blockValidatorSetFromSignatures(block); ok && len(committed) > 0 {
			candidates = append(candidates, committed)
		}
		// `frozen` stores the value produced by this operation.
		if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
			candidates = append(candidates, frozen)
			// `frozenHash` and `ok` store whether the related condition is satisfied.
			if frozenHash, ok := n.frozenValidatorSetHash(height); ok &&
				strings.EqualFold(strings.TrimSpace(frozenHash), strings.TrimSpace(block.ValidatorSetHash)) {
				return canonicalValidatorIDs(frozen), strings.TrimSpace(block.ValidatorSetHash), "chain_pruned_frozen", true
			}
		}
		// `resolved`, `resolvedHash`, `source`, and `ok` store whether the related condition is satisfied.
		if resolved, resolvedHash, source, ok := tryTarget(block.ValidatorSetHash, "chain_block_signatures", candidates...); ok {
			return resolved, resolvedHash, source, true
		}
		// `resolved`, `resolvedHash`, `source`, and `ok` store whether the related condition is satisfied.
		if resolved, resolvedHash, source, ok := reconstructTarget(block.ValidatorSetHash, "chain_block_signatures", candidates...); ok {
			return resolved, resolvedHash, source, true
		}
		// `resolved` and `ok` store whether the related condition is satisfied.
		if resolved, ok := n.syncPeerAdvertisedValidatorSetCandidate(height, block.ValidatorSetHash); ok {
			return resolved, strings.TrimSpace(block.ValidatorSetHash), "sync_peer_advertised_block_commitment", true
		}
	}

	// `chainHeight` stores the value produced by this operation.
	chainHeight := n.Blockchain.Height()
	if chainHeight == 0 || height != chainHeight+1 || height <= 1 || !validatorSetCommitmentV2EnabledAt(height-1) {
		return nil, "", "none", false
	}

	// `parent` and `ok` store whether the related condition is satisfied.
	parent, ok := n.Blockchain.GetBlock(height - 1)
	if !ok {
		return nil, "", "none", false
	}

	// `targetHash` stores the digest used to identify or verify the related data.
	targetHash := strings.TrimSpace(parent.NextValidatorSetHash)
	// `frozen` and `ok` store whether the related condition is satisfied.
	if frozen, ok := n.committedFrozenValidatorSetCandidate(targetHash, height, parent.ID); ok {
		return frozen, targetHash, "chain_parent_commitment_carry_forward", true
	}
	// Height 1+ authority is the parent-committed registry. Resolve its ACTIVE
	// set before generic carry-forward candidates so both membership and source
	// attribution remain registry-authoritative. Keep the frozen fast path above
	// this check so pruned nodes do not need to load full state snapshots.
	if active, registry := n.trustedRegistrySmallValidatorSetForHeight(height, true); len(active) > 0 {
		if matched, ok := n.validatorSetCandidateMatchesTarget(height, targetHash, active, registry); ok {
			return matched, targetHash, "registry_verified", true
		}
	}
	// `candidates` stores the value produced by this operation.
	candidates := make([][]string, 0, 5)
	// `resolved`, `resolvedHash`, `source`, and `ok` store whether the related condition is satisfied.
	if resolved, resolvedHash, source, ok := n.validatorSetCandidateFromStoredSnapshot(height, parent.NextValidatorSetHash); ok {
		return resolved, resolvedHash, source, true
	}
	// `ctx` stores the context controlling this operation.
	if ctx := n.blockValidatorUpdatePlanContext(parent); ctx != nil {
		// `planned` stores the value produced by this operation.
		if planned := ctx.plannedValidatorsForHeight(height); len(planned) > 0 {
			candidates = append(candidates, planned)
		}
	}
	// `planned` stores the value produced by this operation.
	if planned := n.plannedValidatorSetForHeightFromChain(height); len(planned) > 0 {
		candidates = append(candidates, planned)
	}
	// `committed` and `ok` store whether the related condition is satisfied.
	if committed, ok := n.blockValidatorSetFromSignatures(parent); ok && len(committed) > 0 {
		candidates = append(candidates, committed)
	}
	// `frozen` stores the value produced by this operation.
	if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
		candidates = append(candidates, frozen)
	}
	// `frozen` stores the value produced by this operation.
	if frozen := n.frozenValidatorsForHeight(parent.ID); len(frozen) > 0 {
		candidates = append(candidates, frozen)
	}
	// `resolved`, `resolvedHash`, `source`, and `ok` store whether the related condition is satisfied.
	if resolved, resolvedHash, source, ok := tryTarget(parent.NextValidatorSetHash, "chain_parent_commitment", candidates...); ok {
		return resolved, resolvedHash, source, true
	}
	if targetHash != "" {
		// `frozen` stores the value produced by this operation.
		if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
			// `frozenHash` and `ok` store whether the related condition is satisfied.
			if frozenHash, ok := n.frozenValidatorSetHash(height); ok && strings.EqualFold(strings.TrimSpace(frozenHash), targetHash) {
				return canonicalValidatorIDs(frozen), targetHash, "chain_parent_pruned_frozen", true
			}
		}
		// `frozen` stores the value produced by this operation.
		if frozen := n.frozenValidatorsForHeight(parent.ID); len(frozen) > 0 {
			// `frozenHash` and `ok` store whether the related condition is satisfied.
			if frozenHash, ok := n.frozenValidatorSetHash(parent.ID); ok && strings.EqualFold(strings.TrimSpace(frozenHash), targetHash) {
				return canonicalValidatorIDs(frozen), targetHash, "chain_parent_commitment_carry_forward", true
			}
			if strings.EqualFold(strings.TrimSpace(parent.ValidatorSetHash), targetHash) {
				return canonicalValidatorIDs(frozen), targetHash, "chain_parent_commitment_carry_forward", true
			}
		}
	}
	// `resolved`, `resolvedHash`, `source`, and `ok` store whether the related condition is satisfied.
	if resolved, resolvedHash, source, ok := reconstructTarget(parent.NextValidatorSetHash, "chain_parent_commitment", candidates...); ok {
		return resolved, resolvedHash, source, true
	}
	// `resolved` and `ok` store whether the related condition is satisfied.
	if resolved, ok := n.syncPeerAdvertisedValidatorSetCandidate(height, parent.NextValidatorSetHash); ok {
		return resolved, strings.TrimSpace(parent.NextValidatorSetHash), "sync_peer_advertised_chain_parent_commitment", true
	}
	// `resolved` and `ok` store whether the related condition is satisfied.
	if resolved, ok := n.syncHeartbeatValidatorSetCandidate(height, parent.NextValidatorSetHash); ok {
		return resolved, strings.TrimSpace(parent.NextValidatorSetHash), "sync_heartbeat_chain_parent_commitment", true
	}
	return nil, "", "none", false
}
