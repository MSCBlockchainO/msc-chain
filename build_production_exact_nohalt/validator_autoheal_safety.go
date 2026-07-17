package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func (n *Node) resolveValidatorSetForHashAtHeight(height uint64, targetHash string) ([]string, bool) {
	if n == nil || height == 0 {
		return nil, false
	}
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return nil, false
	}
	registrySnapshot := n.validatorRegistrySnapshotForHeight(height)

	candidates := make([][]string, 0, 12)
	if frozen := n.frozenValidatorsForHeight(height); len(frozen) > 0 {
		candidates = append(candidates, frozen)
	}
	if frozenNext := n.frozenValidatorsForHeight(height + 1); len(frozenNext) > 0 {
		candidates = append(candidates, frozenNext)
	}
	if height > 1 {
		if frozenPrev := n.frozenValidatorsForHeight(height - 1); len(frozenPrev) > 0 {
			candidates = append(candidates, frozenPrev)
		}
	}
	if hint := n.consensusValidatorsForHeight(height); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	if hint := n.consensusValidatorsForHeight(height + 1); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	if height > 1 {
		if hint := n.consensusValidatorsForHeight(height - 1); len(hint) > 0 {
			candidates = append(candidates, hint)
		}
	}
	if hint := n.GetConsensusValidators(int(height)); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	if hint := n.GetConsensusValidators(int(height + 1)); len(hint) > 0 {
		candidates = append(candidates, hint)
	}
	if height > 1 {
		if hint := n.GetConsensusValidators(int(height - 1)); len(hint) > 0 {
			candidates = append(candidates, hint)
		}
	}
	if core := n.activeCoreAuthorityIDs(); len(core) > 0 {
		candidates = append(candidates, core)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		canonical := canonicalValidatorIDs(raw)
		if len(canonical) == 0 {
			continue
		}
		key := strings.Join(canonical, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := n.validatorSetCandidateMatchesTarget(height, target, canonical, registrySnapshot); ok {
			return canonical, true
		}
	}

	// Recovery fallback:
	// when exact frozen history is unavailable (restart/catch-up), reconstruct the
	// committee by hash from a bounded universe of known validator IDs.
	universeSet := make(map[string]struct{})
	for _, raw := range candidates {
		for _, id := range canonicalValidatorIDs(raw) {
			if id == "" {
				continue
			}
			universeSet[id] = struct{}{}
		}
	}
	if staked := selectAllStakedValidators(height); len(staked) > 0 {
		for _, id := range canonicalValidatorIDs(staked) {
			if id == "" {
				continue
			}
			universeSet[id] = struct{}{}
		}
	}

	universe := make([]string, 0, len(universeSet))
	for id := range universeSet {
		universe = append(universe, id)
	}
	if subset, ok := n.findValidatorSubsetByHash(height, universe, target, registrySnapshot); ok {
		if DebugConsensus {
			fmt.Printf("[SET-RESOLVE-BY-HASH] height=%d hash=%s size=%d\n", height, ShortHash(target), len(subset))
		}
		return subset, true
	}
	return nil, false
}

func (n *Node) findValidatorSubsetByHash(height uint64, ids []string, targetHash string, registrySnapshot map[string]ValidatorRecord) ([]string, bool) {
	target := strings.ToLower(strings.TrimSpace(targetHash))
	base := canonicalValidatorIDs(ids)
	if target == "" || len(base) == 0 {
		return nil, false
	}
	if _, ok := n.validatorSetCandidateMatchesTarget(height, target, base, registrySnapshot); ok {
		return base, true
	}

	// Keep bounded to avoid expensive combinatorics in pathological cases.
	const maxUniverse = 12
	if len(base) > maxUniverse {
		return nil, false
	}

	comb := make([]string, 0, len(base))
	var found []string
	var search func(start, need int) bool
	search = func(start, need int) bool {
		if need == 0 {
			candidate := append([]string{}, comb...)
			if _, ok := n.validatorSetCandidateMatchesTarget(height, target, candidate, registrySnapshot); ok {
				found = candidate
				return true
			}
			return false
		}
		remaining := len(base) - start
		if remaining < need {
			return false
		}
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

func (n *Node) coreExpectedConsensusPubKeyForID(nodeID string) (ed25519.PublicKey, bool) {
	if n == nil {
		return nil, false
	}
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return nil, false
	}
	n.coreRegistryMu.RLock()
	entry, ok := n.coreRegistryEntries[id]
	n.coreRegistryMu.RUnlock()
	if !ok {
		return nil, false
	}
	hexKey := strings.TrimSpace(entry.ConsensusPubKey)
	if hexKey == "" {
		return nil, false
	}
	pubRaw, err := hex.DecodeString(hexKey)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(pubRaw), true
}

func strictStakeProofAmountForValidator(ledger *Ledger, validatorID string) (int, bool) {
	if ledger == nil {
		return 0, false
	}
	target := normalizeValidatorID(validatorID)
	if target == "" {
		return 0, false
	}
	total := 0
	proof := false
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		keyValidator := normalizeValidatorID(parts[1])
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

func (n *Node) localFinalizedHeightForSafety(height uint64) uint64 {
	if n == nil {
		return 0
	}
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

func (n *Node) coreQuorumVotesStrictForHash(height uint64, targetHash string) (int, int) {
	if n == nil {
		return 0, 0
	}
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return 0, 0
	}
	authorityIDs := n.validatorAuthorityIDsForQuorum(height)
	if len(authorityIDs) == 0 {
		return 0, 0
	}
	required := execQuorumRequired(len(authorityIDs))
	if required <= 0 {
		required = 1
	}

	now := time.Now()
	localFinalized := n.localFinalizedHeightForSafety(height)
	selfID := normalizeValidatorID(n.ID)
	voted := make(map[string]struct{}, len(authorityIDs))

	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()

	for _, authorityID := range authorityIDs {
		id := normalizeValidatorID(authorityID)
		if id == "" {
			continue
		}
		if id == selfID {
			localHash := strings.ToLower(strings.TrimSpace(n.expectedValidatorSetHash(height)))
			if localHash == target {
				voted[id] = struct{}{}
			}
			continue
		}

		st := n.validatorStatus[id]
		if st == nil {
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
		hash := strings.ToLower(strings.TrimSpace(st.ValidatorSetHash))
		if hash != target {
			continue
		}
		voted[id] = struct{}{}
	}
	return len(voted), required
}

func (n *Node) autohealLocalSnapshotQuorumVerified(height uint64, targetHash string, required int) bool {
	if n == nil || required <= 0 {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(targetHash))
	if target == "" {
		return false
	}
	localFinalized := n.localFinalizedHeightForSafety(height)
	if localFinalized == 0 {
		return false
	}
	if absHeightLagBlocks(localFinalized, height) > validatorLivenessMaxHeightDriftBlocks() {
		return false
	}

	snap, err := n.GetSnapshot(localFinalized)
	if err != nil || snap == nil {
		return false
	}
	if !n.verifySnapshotAgainstLocalBlock(snap) {
		return false
	}
	vals := canonicalValidatorIDs(validatorsFromSnapshot(snap))
	if len(vals) == 0 {
		return false
	}
	if !strings.EqualFold(ValidatorSetHash(vals), target) {
		return false
	}

	authoritySet := make(map[string]struct{})
	for _, id := range n.validatorAuthorityIDsForQuorum(height) {
		authoritySet[id] = struct{}{}
	}
	matches := 0
	for _, id := range vals {
		if _, ok := authoritySet[id]; ok {
			matches++
		}
	}
	return matches >= required
}

func (n *Node) autohealSafetyAllowsMutation(height uint64, targetHash string) (bool, string) {
	if n == nil || height == 0 {
		return false, "invalid_height"
	}
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

	localFinalized := n.localFinalizedHeightForSafety(height)
	if localFinalized > 0 && absHeightLagBlocks(localFinalized, height) > validatorLivenessMaxHeightDriftBlocks() {
		return false, "near_tip_violation"
	}

	votes, required := n.coreQuorumVotesStrictForHash(height, target)
	if required <= 0 {
		return false, "validator_quorum_unavailable"
	}
	if votes < required && !n.autohealLocalSnapshotQuorumVerified(height, target, required) {
		return false, fmt.Sprintf("validator_quorum_%d_of_%d", votes, required)
	}

	validators, ok := n.resolveValidatorSetForHashAtHeight(height, target)
	if !ok {
		return false, "validator_set_unresolved"
	}

	ledgerRef := &n.Ledger
	registrySnap := n.validatorRegistrySnapshotForHeight(height)
	if len(registrySnap) == 0 {
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
		if snap, err := n.GetSnapshotAtOrBelow(localFinalized); err == nil && snap != nil {
			ledgerRef = &snap.Ledger
			if len(snap.ValidatorRegistry) > 0 {
				registrySnap = copyValidatorRegistrySnapshot(snap.ValidatorRegistry)
			}
		}
	}

	for _, rawID := range validators {
		id := normalizeValidatorID(rawID)
		if id == "" {
			return false, "invalid_validator_id"
		}
		if isValidatorBanned(id) {
			return false, "banned_validator_" + id
		}

		rec, ok := validatorRegistryRecordFromSnapshot(registrySnap, id)
		if !ok {
			return false, "missing_registry_" + id
		}
		if strings.TrimSpace(rec.ID) == "" {
			rec.ID = id
		}
		recCopy := rec
		ValidatorStateMachine{}.Update(&recCopy, height)
		if recCopy.Status == ValidatorExited {
			return false, "exited_validator_" + id
		}
		if recCopy.JailUntilHeight > 0 && height < recCopy.JailUntilHeight {
			return false, "jailed_validator_" + id
		}

		staked, proof := strictStakeProofAmountForValidator(ledgerRef, id)
		if !proof {
			return false, "missing_stake_proof_" + id
		}
		if staked < int(ValidatorMinStake) {
			return false, "stake_below_min_" + id
		}

		pub, ok := validatorPubKeyForID(id)
		if !ok || len(pub) != ed25519.PublicKeySize {
			return false, "missing_pubkey_" + id
		}
		fp := strings.TrimSpace(validatorKeyFingerprint(pub))
		if fp == "" {
			return false, "missing_fingerprint_" + id
		}
		if n.coreBootstrapAuthorityAllowed() {
			if expected, ok := n.coreExpectedFingerprintForID(id); ok && !strings.EqualFold(expected, fp) {
				return false, "fingerprint_mismatch_" + id
			}
			if expectedPub, ok := n.coreExpectedConsensusPubKeyForID(id); ok && !bytes.Equal(expectedPub, pub) {
				return false, "pubkey_mismatch_" + id
			}
		}
	}

	return true, ""
}
