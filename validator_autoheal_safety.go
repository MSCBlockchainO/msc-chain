package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// resolveValidatorSetForHashAtHeight implements the resolve validator set for hash at height helper.
func (n *Node) resolveValidatorSetForHashAtHeight(height uint64, targetHash string) ([]string, bool) {
	if n == nil || height == 0 {
		return nil, false
	}
	// `target` stores the value produced by this operation.
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return nil, false
	}
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := n.validatorRegistrySnapshotForHeight(height)

	// `candidates` stores the value produced by this operation.
	candidates := make([][]string, 0, 12)
	// `frozen` stores the value produced by this operation.
	if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
		candidates = append(candidates, frozen)
	}
	// `frozenNext` stores the value produced by this operation.
	if frozenNext := n.frozenValidatorsForHeight(height + 1); len(frozenNext) > 0 {
		candidates = append(candidates, frozenNext)
	}
	if height > 1 {
		// `frozenPrev` stores the value produced by this operation.
		if frozenPrev := n.frozenValidatorsForHeight(height - 1); len(frozenPrev) > 0 {
			candidates = append(candidates, frozenPrev)
		}
	}
	// `hint` stores the value produced by this operation.
	if hint := n.consensusValidatorsForHeight(height); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	// `hint` stores the value produced by this operation.
	if hint := n.consensusValidatorsForHeight(height + 1); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	if height > 1 {
		// `hint` stores the value produced by this operation.
		if hint := n.consensusValidatorsForHeight(height - 1); len(hint) > 0 {
			candidates = append(candidates, hint)
		}
	}
	// `hint` stores the value produced by this operation.
	if hint := n.GetConsensusValidators(int(height)); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	// `hint` stores the value produced by this operation.
	if hint := n.GetConsensusValidators(int(height + 1)); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	if height > 1 {
		// `hint` stores the value produced by this operation.
		if hint := n.GetConsensusValidators(int(height - 1)); len(hint) > 0 {
			candidates = append(candidates, hint)
		}
	}
	// `core` stores the value produced by this operation.
	if core := n.activeCoreAuthorityIDs(); len(core) > 0 {
		candidates = append(candidates, core)
	}

	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(candidates))
	// `raw` tracks the current values while iterating.
	for _, raw := range candidates {
		// `canonical` stores the value produced by this operation.
		canonical := canonicalValidatorIDs(raw)
		if len(canonical) == 0 {
			continue
		}
		// `key` stores the key used to access the related value.
		key := strings.Join(canonical, "|")
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := n.validatorSetCandidateMatchesTarget(height, target, canonical, registrySnapshot); ok {
			return canonical, true
		}
	}

	// Recovery fallback:
	// when exact frozen history is unavailable (restart/catch-up), reconstruct the
	// committee by hash from a bounded universe of known validator IDs.
	universeSet := make(map[string]struct{})
	// `raw` tracks the current values while iterating.
	for _, raw := range candidates {
		// `id` tracks the current position in the related collection.
		for _, id := range canonicalValidatorIDs(raw) {
			if id == "" {
				continue
			}
			universeSet[id] = struct{}{}
		}
	}
	// `staked` stores the value produced by this operation.
	if staked := selectAllStakedValidators(height); len(staked) > 0 {
		// `id` tracks the current position in the related collection.
		for _, id := range canonicalValidatorIDs(staked) {
			if id == "" {
				continue
			}
			universeSet[id] = struct{}{}
		}
	}

	// `universe` stores the value produced by this operation.
	universe := make([]string, 0, len(universeSet))
	// `id` tracks the current position in the related collection.
	for id := range universeSet {
		universe = append(universe, id)
	}
	// `subset` and `ok` store whether the related condition is satisfied.
	if subset, ok := n.findValidatorSubsetByHash(height, universe, target, registrySnapshot); ok {
		if DebugConsensus {
			fmt.Printf("[SET-RESOLVE-BY-HASH] height=%d hash=%s size=%d\n", height, ShortHash(target), len(subset))
		}
		return subset, true
	}
	return nil, false
}

// findValidatorSubsetByHash implements the find validator subset by hash helper.
func (n *Node) findValidatorSubsetByHash(height uint64, ids []string, targetHash string, registrySnapshot map[string]ValidatorRecord) ([]string, bool) {
	// `target` stores the value produced by this operation.
	target := strings.ToLower(strings.TrimSpace(targetHash))
	// `base` stores the value produced by this operation.
	base := canonicalValidatorIDs(ids)
	if target == "" || len(base) == 0 {
		return nil, false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.validatorSetCandidateMatchesTarget(height, target, base, registrySnapshot); ok {
		return base, true
	}

	// Keep bounded to avoid expensive combinatorics in pathological cases.
	const maxUniverse = 12
	if len(base) > maxUniverse {
		return nil, false
	}

	// `comb` stores the value produced by this operation.
	comb := make([]string, 0, len(base))
	// `found` stores whether the related condition is satisfied.
	var found []string
	// `search` stores the value used by this operation.
	var search func(start, need int) bool
	search = func(start, need int) bool {
		if need == 0 {
			// `candidate` stores the value produced by this operation.
			candidate := append([]string{}, comb...)
			// `ok` stores whether the related condition is satisfied.
			if _, ok := n.validatorSetCandidateMatchesTarget(height, target, candidate, registrySnapshot); ok {
				found = candidate
				return true
			}
			return false
		}
		// `remaining` stores the value produced by this operation.
		remaining := len(base) - start
		if remaining < need {
			return false
		}
		// `i` stores the current position in the related collection.
		for i := start; i <= len(base)-need; i++ {
			comb = append(comb, base[i])
			if search(i+1, need-1) {
				return true
			}
			comb = comb[:len(comb)-1]
		}
		return false
	}

	// Prefer larger subsets first; this biases toward full committee reconstructions
	// before reduced committees when multiple hashes are being repaired.
	for size := len(base) - 1; size >= 1; size-- {
		comb = comb[:0]
		if search(0, size) {
			return found, true
		}
	}
	return nil, false
}

// coreExpectedConsensusPubKeyForID implements the core expected consensus pub key for id helper.
func (n *Node) coreExpectedConsensusPubKeyForID(nodeID string) (ed25519.PublicKey, bool) {
	if n == nil {
		return nil, false
	}
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return nil, false
	}
	n.coreRegistryMu.RLock()
	// `entry` and `ok` store whether the related condition is satisfied.
	entry, ok := n.coreRegistryEntries[id]
	n.coreRegistryMu.RUnlock()
	if !ok {
		return nil, false
	}
	// `hexKey` stores the key used to access the related value.
	hexKey := strings.TrimSpace(entry.ConsensusPubKey)
	if hexKey == "" {
		return nil, false
	}
	// `pubRaw` and `err` store the error produced by this operation.
	pubRaw, err := hex.DecodeString(hexKey)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(pubRaw), true
}

// strictStakeProofAmountForValidator implements the strict stake proof amount for validator helper.
func strictStakeProofAmountForValidator(ledger *Ledger, validatorID string) (int, bool) {
	if ledger == nil {
		return 0, false
	}
	// `target` stores the value produced by this operation.
	target := normalizeValidatorID(validatorID)
	if target == "" {
		return 0, false
	}
	// `total` stores the measured quantity used by this operation.
	total := 0
	// `proof` stores the value produced by this operation.
	proof := false
	// `key` and `rec` track the key used to access the related value.
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		// `keyValidator` stores the key used to access the related value.
		keyValidator := normalizeValidatorID(parts[1])
		// `recValidator` stores the value produced by this operation.
		recValidator := normalizeValidatorID(rec.ValidatorID)
		if keyValidator == "" || recValidator == "" {
			continue
		}
		if keyValidator != recValidator {
			continue
		}
		if recValidator != target {
			continue
		}
		total += rec.Amount
		proof = true
	}
	return total, proof
}

// localFinalizedHeightForSafety implements the local finalized height for safety helper.
func (n *Node) localFinalizedHeightForSafety(height uint64) uint64 {
	if n == nil {
		return 0
	}
	// `finalized` stores the value produced by this operation.
	finalized := uint64(0)
	if n.Blockchain != nil {
		finalized = n.Blockchain.FinalizedHeight()
		if finalized == 0 {
			finalized = n.Blockchain.Height()
		}
	}
	if finalized == 0 {
		finalized = n.getFinalizedHeight()
	}
	if finalized == 0 {
		finalized = height
	}
	return finalized
}

// coreQuorumVotesStrictForHash implements the core quorum votes strict for hash helper.
func (n *Node) coreQuorumVotesStrictForHash(height uint64, targetHash string) (int, int) {
	if n == nil {
		return 0, 0
	}
	// `target` stores the value produced by this operation.
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return 0, 0
	}
	// `authorityIDs` stores the value produced by this operation.
	authorityIDs := n.validatorAuthorityIDsForQuorum(height)
	if len(authorityIDs) == 0 {
		return 0, 0
	}
	// `required` stores the request data being processed.
	required := execQuorumRequired(len(authorityIDs))
	if required <= 0 {
		required = 1
	}

	// `now` stores the value produced by this operation.
	now := time.Now()
	// `localFinalized` stores the value produced by this operation.
	localFinalized := n.localFinalizedHeightForSafety(height)
	// `selfID` stores the value produced by this operation.
	selfID := n.localConsensusValidatorIDForSet(authorityIDs)
	// `voted` stores the value produced by this operation.
	voted := make(map[string]struct{}, len(authorityIDs))

	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()

	// `authorityID` tracks the current values while iterating.
	for _, authorityID := range authorityIDs {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(authorityID)
		if id == "" {
			continue
		}
		if id == selfID {
			// `localHash` stores the digest used to identify or verify the related data.
			localHash := strings.ToLower(strings.TrimSpace(n.expectedValidatorSetHash(height)))
			if localHash == target {
				voted[id] = struct{}{}
			}
			continue
		}

		// `st` stores the value produced by this operation.
		st := n.validatorStatus[id]
		if st == nil {
			// `key` and `cand` track the key used to access the related value.
			for key, cand := range n.validatorStatus {
				if normalizeValidatorID(key) == id {
					st = cand
					break
				}
			}
		}
		if st == nil {
			continue
		}
		if !n.isValidatorLiveForConsensusLocked(id, st, now, localFinalized) {
			continue
		}
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.ToLower(strings.TrimSpace(st.ValidatorSetHash))
		if hash != target {
			continue
		}
		voted[id] = struct{}{}
	}
	return len(voted), required
}

// autohealLocalSnapshotQuorumVerified implements the autoheal local snapshot quorum verified helper.
func (n *Node) autohealLocalSnapshotQuorumVerified(height uint64, targetHash string, required int) bool {
	if n == nil || required <= 0 {
		return false
	}
	// `target` stores the value produced by this operation.
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return false
	}
	// `localFinalized` stores the value produced by this operation.
	localFinalized := n.localFinalizedHeightForSafety(height)
	if localFinalized == 0 {
		return false
	}
	if absHeightLagBlocks(localFinalized, height) > validatorLivenessMaxHeightDriftBlocks() {
		return false
	}

	// `snap` and `err` store the error produced by this operation.
	snap, err := n.GetSnapshot(localFinalized)
	if err != nil || snap == nil {
		return false
	}
	if !n.verifySnapshotAgainstLocalBlock(snap) {
		return false
	}
	// `vals` stores the value currently being processed.
	vals := canonicalValidatorIDs(validatorsFromSnapshot(snap))
	if len(vals) == 0 {
		return false
	}
	if !strings.EqualFold(ValidatorSetHash(vals), target) {
		return false
	}

	// `authoritySet` stores the value produced by this operation.
	authoritySet := make(map[string]struct{})
	// `id` tracks the current position in the related collection.
	for _, id := range n.validatorAuthorityIDsForQuorum(height) {
		authoritySet[id] = struct{}{}
	}
	// `matches` stores the value produced by this operation.
	matches := 0
	// `id` tracks the current position in the related collection.
	for _, id := range vals {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := authoritySet[id]; ok {
			matches++
		}
	}
	return matches >= required
}

// autohealSafetyAllowsMutation implements the autoheal safety allows mutation helper.
func (n *Node) autohealSafetyAllowsMutation(height uint64, targetHash string) (bool, string) {
	if n == nil || height == 0 {
		return false, "invalid_height"
	}
	// `target` stores the value produced by this operation.
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return false, "empty_target_hash"
	}

	// Identity rotation bypass must remain disabled for autoheal set mutation.
	if ValidatorAllowIdentityRotationOnExistingChain {
		return false, "identity_rotation_bypass_enabled"
	}

	// Stake proof is enforced against base chain coin.
	if !strings.EqualFold(strings.TrimSpace(CoinSymbol), "MSC") {
		return false, "unsupported_stake_coin"
	}

	// `localFinalized` stores the value produced by this operation.
	localFinalized := n.localFinalizedHeightForSafety(height)
	if localFinalized > 0 && absHeightLagBlocks(localFinalized, height) > validatorLivenessMaxHeightDriftBlocks() {
		return false, "near_tip_violation"
	}

	// `votes` and `required` store the request data being processed.
	votes, required := n.coreQuorumVotesStrictForHash(height, target)
	if required <= 0 {
		return false, "validator_quorum_unavailable"
	}
	if votes < required && !n.autohealLocalSnapshotQuorumVerified(height, target, required) {
		return false, fmt.Sprintf("validator_quorum_%d_of_%d", votes, required)
	}

	// `validators` and `ok` store whether the related condition is satisfied.
	validators, ok := n.resolveValidatorSetForHashAtHeight(height, target)
	if !ok {
		return false, "validator_set_unresolved"
	}

	// `ledgerRef` stores the value produced by this operation.
	ledgerRef := &n.Ledger
	// `registrySnap` stores the value produced by this operation.
	registrySnap := n.validatorRegistrySnapshotForHeight(height)
	if len(registrySnap) == 0 {
		// `legacyWeakSource` stores the value produced by this operation.
		legacyWeakSource := height <= 1 || !validatorSetCommitmentV2EnabledAt(height-1)
		if legacyWeakSource {
			// Legacy safety checks may read runtime registry hints when no committed
			// snapshot registry is locally available.
			registrySnap = copyValidatorRegistrySnapshot(GlobalValidatorRegistry.Snapshot())
		}
	}
	if len(registrySnap) == 0 && n.Blockchain != nil && n.Blockchain.Height() == 0 {
		// Genesis bootstrap only.
		registrySnap = copyValidatorRegistrySnapshot(GlobalValidatorRegistry.Snapshot())
	}
	if localFinalized > 0 {
		// `snap` and `err` store the error produced by this operation.
		if snap, err := n.GetSnapshotAtOrBelow(localFinalized); err == nil && snap != nil {
			ledgerRef = &snap.Ledger
			if len(snap.ValidatorRegistry) > 0 {
				registrySnap = copyValidatorRegistrySnapshot(snap.ValidatorRegistry)
			}
		}
	}

	// `rawID` tracks the current values while iterating.
	for _, rawID := range validators {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rawID)
		if id == "" {
			return false, "invalid_validator_id"
		}
		if isProtocolValidatorBanned(id) {
			return false, "banned_validator_" + id
		}

		// `rec` and `ok` store whether the related condition is satisfied.
		rec, ok := validatorRegistryRecordFromSnapshot(registrySnap, id)
		if !ok {
			return false, "missing_registry_" + id
		}
		if strings.TrimSpace(rec.ID) == "" {
			rec.ID = id
		}
		// `recCopy` stores the value produced by this operation.
		recCopy := rec
		ValidatorStateMachine{}.Update(&recCopy, height)
		if validatorStateIsRemoved(recCopy.Status) {
			return false, "removed_validator_" + id
		}
		if recCopy.Status != ValidatorActive {
			return false, "inactive_validator_" + id
		}
		if recCopy.JailUntilHeight > 0 && height < recCopy.JailUntilHeight {
			return false, "jailed_validator_" + id
		}

		// `staked` and `proof` store the value produced by this operation.
		staked, proof := strictStakeProofAmountForValidator(ledgerRef, id)
		if !proof {
			return false, "missing_stake_proof_" + id
		}
		if staked < int(ConsensusValidatorMinStake) {
			return false, "stake_below_min_" + id
		}

		// `pub` and `ok` store whether the related condition is satisfied.
		pub, ok := validatorPubKeyForID(id)
		if !ok || len(pub) != ed25519.PublicKeySize {
			return false, "missing_pubkey_" + id
		}
		// `fp` stores the value produced by this operation.
		fp := strings.TrimSpace(validatorKeyFingerprint(pub))
		if fp == "" {
			return false, "missing_fingerprint_" + id
		}
		if n.coreBootstrapAuthorityAllowed() {
			// `expected` and `ok` store whether the related condition is satisfied.
			if expected, ok := n.coreExpectedFingerprintForID(id); ok && !strings.EqualFold(expected, fp) {
				return false, "fingerprint_mismatch_" + id
			}
			// `expectedPub` and `ok` store whether the related condition is satisfied.
			if expectedPub, ok := n.coreExpectedConsensusPubKeyForID(id); ok && !bytes.Equal(expectedPub, pub) {
				return false, "pubkey_mismatch_" + id
			}
		}
	}

	return true, ""
}
