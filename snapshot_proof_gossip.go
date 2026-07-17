package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const (
	// `validatorSnapshotAutoPublishIntervalBlocks` defines whether the related condition is satisfied.
	validatorSnapshotAutoPublishIntervalBlocks uint64 = 10
	maxSnapshotProofCacheGroups                       = 256
	maxSnapshotProofRegistryEntries                   = 4096
	maxSnapshotProofFieldBytes                        = 512
)

// betterStrictSnapshotCandidate implements the better strict snapshot candidate helper.
func betterStrictSnapshotCandidate(a *strictSnapshotMetaCandidate, b *strictSnapshotMetaCandidate) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	if a.Height != b.Height {
		return a.Height > b.Height
	}
	if len(a.Validators) != len(b.Validators) {
		return len(a.Validators) > len(b.Validators)
	}
	return strictSnapshotMetaCandidateKey(a) < strictSnapshotMetaCandidateKey(b)
}

// normalizeSnapshotProof normalizes snapshot proof.
func normalizeSnapshotProof(proof *SnapshotProof) {
	if proof == nil {
		return
	}
	proof.BlockHash = strings.TrimSpace(proof.BlockHash)
	proof.SnapshotHash = strings.ToLower(strings.TrimSpace(proof.SnapshotHash))
	proof.StateRoot = strings.TrimSpace(proof.StateRoot)
	proof.StateMerkleRoot = strings.TrimSpace(proof.StateMerkleRoot)
	proof.LedgerHash = strings.TrimSpace(proof.LedgerHash)
	proof.ValidatorSetHash = strings.TrimSpace(proof.ValidatorSetHash)
	proof.ValidatorSetRoot = strings.TrimSpace(proof.ValidatorSetRoot)
	proof.ValidatorRegistryHash = strings.TrimSpace(proof.ValidatorRegistryHash)
	proof.ValidatorRegistry = copyValidatorRegistrySnapshot(proof.ValidatorRegistry)
	proof.CheckpointDomain = strings.TrimSpace(proof.CheckpointDomain)
	proof.Validator = normalizeValidatorID(proof.Validator)
	proof.SignatureHex = strings.ToLower(strings.TrimSpace(proof.SignatureHex))
	if proof.CheckpointHeight == 0 {
		proof.CheckpointHeight = snapshotCheckpointHeightFor(proof.Height)
	}
	if proof.CheckpointDomain == "" && snapshotCheckpointV1EnabledAt(proof.Height) {
		proof.CheckpointDomain = syncSnapshotCheckpointDomain()
	}
}

// snapshotProofFromSnapshot implements the snapshot proof from snapshot helper.
func snapshotProofFromSnapshot(validatorID string, snapshot *StateSnapshot) SnapshotProof {
	// `proof` stores the value produced by this operation.
	proof := SnapshotProof{
		Validator: normalizeValidatorID(validatorID),
	}
	if snapshot == nil {
		return proof
	}
	populateSnapshotDerivedFields(snapshot)
	proof.Height = snapshot.Height
	proof.CheckpointHeight = snapshot.CheckpointHeight
	if proof.CheckpointHeight == 0 {
		proof.CheckpointHeight = snapshotCheckpointHeightFor(snapshot.Height)
	}
	proof.BlockHash = strings.TrimSpace(snapshot.BlockHash)
	proof.SnapshotHash = strings.TrimSpace(snapshot.SnapshotHash)
	proof.StateRoot = strings.TrimSpace(snapshot.StateRoot)
	proof.StateMerkleRoot = strings.TrimSpace(snapshot.StateMerkleRoot)
	proof.LedgerHash = strings.TrimSpace(snapshot.LedgerHash)
	proof.ValidatorSetHash = strings.TrimSpace(snapshotValidatorSetHash(snapshot))
	proof.ValidatorSetRoot = strings.TrimSpace(snapshotValidatorSetRoot(snapshot))
	proof.ValidatorRegistryHash = strings.TrimSpace(snapshotValidatorRegistryHash(snapshot))
	proof.ValidatorRegistry = copyValidatorRegistrySnapshot(snapshot.ValidatorRegistry)
	proof.CheckpointDomain = strings.TrimSpace(snapshot.CheckpointDomain)
	proof.Timestamp = time.Now().Unix()
	if proof.Validator != "" && len(snapshot.CheckpointProof) > 0 {
		// `sigHex` stores the value produced by this operation.
		if sigHex := strings.TrimSpace(snapshot.CheckpointProof[proof.Validator]); sigHex != "" {
			proof.SignatureHex = sigHex
		} else {
			// `key` and `value` track the key used to access the related value.
			for key, value := range snapshot.CheckpointProof {
				if normalizeValidatorID(key) == proof.Validator {
					proof.SignatureHex = strings.TrimSpace(value)
					break
				}
			}
		}
	}
	normalizeSnapshotProof(&proof)
	return proof
}

// snapshotStubFromProof implements the snapshot stub from proof helper.
func snapshotStubFromProof(proof *SnapshotProof) *StateSnapshot {
	if proof == nil {
		return nil
	}
	// `clone` stores the value produced by this operation.
	clone := *proof
	normalizeSnapshotProof(&clone)
	if clone.Height == 0 || clone.Validator == "" || clone.SignatureHex == "" {
		return nil
	}
	registry := copyValidatorRegistrySnapshot(clone.ValidatorRegistry)
	if len(registry) > 0 {
		registryHash := strings.TrimSpace(ValidatorRegistrySnapshotHash(registry))
		if registryHash == "" {
			return nil
		}
		if clone.ValidatorRegistryHash != "" && !strings.EqualFold(strings.TrimSpace(clone.ValidatorRegistryHash), registryHash) {
			return nil
		}
		if clone.ValidatorRegistryHash == "" {
			clone.ValidatorRegistryHash = registryHash
		}
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                clone.Height,
		BlockHash:             clone.BlockHash,
		StateRoot:             clone.StateRoot,
		StateMerkleRoot:       clone.StateMerkleRoot,
		LedgerHash:            clone.LedgerHash,
		GenesisHash:           GenesisHash,
		SnapshotHash:          clone.SnapshotHash,
		ValidatorSetHash:      clone.ValidatorSetHash,
		ValidatorSetRoot:      clone.ValidatorSetRoot,
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: clone.ValidatorRegistryHash,
		CheckpointHeight:      clone.CheckpointHeight,
		CheckpointDomain:      clone.CheckpointDomain,
		CheckpointProof: map[string]string{
			clone.Validator: clone.SignatureHex,
		},
	}
	populateSnapshotDerivedFields(snapshot)
	return snapshot
}

// snapshotProofCandidateFromProof implements the snapshot proof candidate from proof helper.
func snapshotProofCandidateFromProof(proof *SnapshotProof) *strictSnapshotMetaCandidate {
	if proof == nil {
		return nil
	}
	// `clone` stores the value produced by this operation.
	clone := *proof
	normalizeSnapshotProof(&clone)
	if clone.Height == 0 || clone.SnapshotHash == "" || clone.StateRoot == "" || clone.ValidatorSetHash == "" || clone.ValidatorRegistryHash == "" {
		return nil
	}
	// `candidate` stores the value produced by this operation.
	candidate := &strictSnapshotMetaCandidate{
		Height:                clone.Height,
		CheckpointHeight:      clone.CheckpointHeight,
		SnapshotHash:          clone.SnapshotHash,
		StateRoot:             clone.StateRoot,
		StateMerkleRoot:       clone.StateMerkleRoot,
		ValidatorSetHash:      clone.ValidatorSetHash,
		ValidatorRegistryHash: clone.ValidatorRegistryHash,
		Validators:            make(map[string]struct{}),
	}
	if clone.Validator != "" {
		candidate.Validators[clone.Validator] = struct{}{}
	}
	return candidate
}

// snapshotVoteFromProof implements the snapshot vote from proof helper.
func snapshotVoteFromProof(proof *SnapshotProof) SnapshotVote {
	if proof == nil {
		return SnapshotVote{}
	}
	// `clone` stores the value produced by this operation.
	clone := *proof
	normalizeSnapshotProof(&clone)
	return SnapshotVote{
		ValidatorID:           clone.Validator,
		Height:                clone.CheckpointHeight,
		SnapshotHash:          clone.SnapshotHash,
		StateRoot:             clone.StateRoot,
		ValidatorSetHash:      clone.ValidatorSetHash,
		ValidatorSetRoot:      clone.ValidatorSetRoot,
		ValidatorRegistryHash: clone.ValidatorRegistryHash,
		SignatureHex:          clone.SignatureHex,
	}
}

// snapshotProofLogHash implements the snapshot proof log hash helper.
func snapshotProofLogHash(proof *SnapshotProof) string {
	if proof == nil {
		return ""
	}
	if strings.TrimSpace(proof.SnapshotHash) != "" {
		return ShortHash(proof.SnapshotHash)
	}
	if strings.TrimSpace(proof.StateRoot) != "" {
		return ShortHash(proof.StateRoot)
	}
	return ""
}

func (n *Node) trustedSnapshotCheckpointPubKeyCandidates(validatorID string, height uint64) []ed25519.PublicKey {
	validatorID = normalizeValidatorID(validatorID)
	if validatorID == "" {
		return nil
	}
	if n == nil {
		return execResultPubKeyCandidates(validatorID)
	}
	trustedHeight := height
	localHeight := uint64(0)
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
		if localHeight > 0 && trustedHeight > localHeight+1 {
			trustedHeight = localHeight + 1
		}
	}
	trustedKeys := n.execResultPubKeyCandidatesForHeight(validatorID, trustedHeight)
	if len(trustedKeys) > 0 {
		return trustedKeys
	}
	allowBootstrapKey := n.Blockchain == nil || localHeight == 0
	if !allowBootstrapKey {
		for _, authorityID := range n.validatorAuthorityIDsForQuorum(trustedHeight) {
			if normalizeValidatorID(authorityID) == validatorID {
				allowBootstrapKey = true
				break
			}
		}
	}
	if !allowBootstrapKey {
		return nil
	}
	return execResultPubKeyCandidates(validatorID)
}

// verifySnapshotProof verifies snapshot proof.
func (n *Node) verifySnapshotProof(proof *SnapshotProof) bool {
	if n == nil || proof == nil {
		return false
	}
	if len(proof.ValidatorRegistry) > maxSnapshotProofRegistryEntries ||
		len(proof.Validator) > maxSnapshotProofFieldBytes ||
		len(proof.BlockHash) > maxSnapshotProofFieldBytes ||
		len(proof.SnapshotHash) > maxSnapshotProofFieldBytes ||
		len(proof.StateRoot) > maxSnapshotProofFieldBytes ||
		len(proof.StateMerkleRoot) > maxSnapshotProofFieldBytes ||
		len(proof.LedgerHash) > maxSnapshotProofFieldBytes ||
		len(proof.ValidatorSetHash) > maxSnapshotProofFieldBytes ||
		len(proof.ValidatorSetRoot) > maxSnapshotProofFieldBytes ||
		len(proof.ValidatorRegistryHash) > maxSnapshotProofFieldBytes ||
		len(proof.CheckpointDomain) > maxSnapshotProofFieldBytes ||
		len(proof.SignatureHex) > maxSnapshotProofFieldBytes {
		return false
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := snapshotStubFromProof(proof)
	if snapshot == nil {
		return false
	}
	if len(snapshot.ValidatorRegistry) == 0 && strings.TrimSpace(snapshot.ValidatorRegistryHash) != "" {
		targetHash := strings.TrimSpace(snapshot.ValidatorRegistryHash)
		if registry, _, ok := n.findCommittedValidatorRegistrySnapshotByHashAtOrBelow(snapshot.Height+1, targetHash); ok && len(registry) > 0 {
			snapshot.ValidatorRegistry = copyValidatorRegistrySnapshot(registry)
		} else if registry, _, ok := n.findCommittedStateSnapshotRegistryByHashAtOrBelow(snapshot.Height, targetHash); ok && len(registry) > 0 {
			snapshot.ValidatorRegistry = copyValidatorRegistrySnapshot(registry)
		}
	}
	// A proof-carried registry is data being asserted by the proof, not a trust
	// root. Require its signer key to match the key already anchored by our
	// committed/current validator authority; otherwise any peer could invent a
	// registry for a real validator ID and validate its own signature.
	if len(snapshot.ValidatorRegistry) > 0 {
		declaredKeys := execResultPubKeyCandidatesFromRegistry(snapshot.ValidatorRegistry, proof.Validator)
		if len(declaredKeys) == 0 {
			return false
		}
		trustedKeys := n.trustedSnapshotCheckpointPubKeyCandidates(proof.Validator, proof.Height)
		keyMatchesAuthority := false
		for _, declared := range declaredKeys {
			for _, trusted := range trustedKeys {
				if bytes.Equal(declared, trusted) {
					keyMatchesAuthority = true
					break
				}
			}
			if keyMatchesAuthority {
				break
			}
		}
		if !keyMatchesAuthority {
			return false
		}
	}
	return n.verifySnapshotCheckpointProofForValidator(snapshot, proof.Validator)
}

// snapshotAnchorCacheFromCandidate implements the snapshot anchor cache from candidate helper.
func snapshotAnchorCacheFromCandidate(candidate *strictSnapshotMetaCandidate) SnapshotAnchorCache {
	// `cache` stores the value produced by this operation.
	cache := SnapshotAnchorCache{}
	if candidate == nil {
		return cache
	}
	cache.CandidateKey = strictSnapshotMetaCandidateKey(candidate)
	cache.Height = candidate.Height
	cache.CheckpointHeight = candidate.CheckpointHeight
	cache.SnapshotHash = strings.TrimSpace(candidate.SnapshotHash)
	cache.StateRoot = strings.TrimSpace(candidate.StateRoot)
	cache.StateMerkleRoot = strings.TrimSpace(candidate.StateMerkleRoot)
	cache.ValidatorSetHash = strings.TrimSpace(candidate.ValidatorSetHash)
	cache.ValidatorRegistryHash = strings.TrimSpace(candidate.ValidatorRegistryHash)
	cache.Validators = make([]string, 0, len(candidate.Validators))
	// `id` tracks the current position in the related collection.
	for id := range candidate.Validators {
		cache.Validators = append(cache.Validators, id)
	}
	sort.Strings(cache.Validators)
	cache.Votes = len(cache.Validators)
	cache.UpdatedAt = time.Now()
	return cache
}

// betterSnapshotAnchorCache implements the better snapshot anchor cache helper.
func betterSnapshotAnchorCache(a SnapshotAnchorCache, b SnapshotAnchorCache) bool {
	if a.Height == 0 {
		return false
	}
	if b.Height == 0 {
		return true
	}
	if a.Height != b.Height {
		return a.Height > b.Height
	}
	if a.Votes != b.Votes {
		return a.Votes > b.Votes
	}
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	return a.CandidateKey < b.CandidateKey
}

// updateSnapshotAnchorCacheLocked implements the update snapshot anchor cache locked helper.
func (n *Node) updateSnapshotAnchorCacheLocked(candidate *strictSnapshotMetaCandidate) {
	if n == nil || candidate == nil || candidate.CheckpointHeight == 0 {
		return
	}
	if n.snapshotAnchorCache == nil {
		n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)
	}
	// `cache` stores the value produced by this operation.
	cache := snapshotAnchorCacheFromCandidate(candidate)
	// `existing` stores the value produced by this operation.
	existing := n.snapshotAnchorCache[candidate.CheckpointHeight]
	if betterSnapshotAnchorCache(cache, existing) {
		n.snapshotAnchorCache[candidate.CheckpointHeight] = cache
	}
}

func snapshotProofGroupRetentionRank(group map[string]SnapshotProof) (uint64, int64) {
	height := uint64(0)
	newest := int64(0)
	for _, proof := range group {
		if proof.Height > height {
			height = proof.Height
		}
		if proof.Timestamp > newest {
			newest = proof.Timestamp
		}
	}
	return height, newest
}

func (n *Node) evictLowestSnapshotProofGroupLocked() string {
	worstKey := ""
	worstHeight := uint64(0)
	worstTimestamp := int64(0)
	for key, group := range n.snapshotProofs {
		height, timestamp := snapshotProofGroupRetentionRank(group)
		if worstKey == "" || height < worstHeight ||
			(height == worstHeight && timestamp < worstTimestamp) ||
			(height == worstHeight && timestamp == worstTimestamp && key < worstKey) {
			worstKey = key
			worstHeight = height
			worstTimestamp = timestamp
		}
	}
	if worstKey == "" {
		return ""
	}
	delete(n.snapshotProofs, worstKey)
	delete(n.snapshotProofProviders, worstKey)
	for checkpointHeight, anchor := range n.snapshotAnchorCache {
		if anchor.CandidateKey == worstKey {
			delete(n.snapshotAnchorCache, checkpointHeight)
		}
	}
	return worstKey
}

func (n *Node) boundSnapshotProofCachesLocked() {
	for len(n.snapshotProofs) > maxSnapshotProofCacheGroups {
		if n.evictLowestSnapshotProofGroupLocked() == "" {
			return
		}
	}
}

// cachedSnapshotAnchorLocked implements the cached snapshot anchor locked helper.
func (n *Node) cachedSnapshotAnchorLocked(targetHeight uint64, minHeight uint64) (SnapshotAnchorCache, bool) {
	if n == nil || len(n.snapshotAnchorCache) == 0 {
		return SnapshotAnchorCache{}, false
	}
	// `best` stores the value used by this operation.
	var best SnapshotAnchorCache
	// `cache` tracks the current values while iterating.
	for _, cache := range n.snapshotAnchorCache {
		if cache.Height == 0 {
			continue
		}
		if targetHeight > 0 && cache.Height > targetHeight {
			continue
		}
		if minHeight > 0 && cache.Height < minHeight {
			continue
		}
		if betterSnapshotAnchorCache(cache, best) {
			best = cache
		}
	}
	if best.Height == 0 {
		return SnapshotAnchorCache{}, false
	}
	return best, true
}

// recordSnapshotProof implements the record snapshot proof helper.
func (n *Node) recordSnapshotProof(proof SnapshotProof) (int, bool) {
	if n == nil {
		return 0, false
	}
	normalizeSnapshotProof(&proof)
	if !n.verifySnapshotProof(&proof) {
		return 0, false
	}
	// `candidate` stores the value produced by this operation.
	candidate := snapshotProofCandidateFromProof(&proof)
	if candidate == nil {
		return 0, false
	}
	// `key` stores the key used to access the related value.
	key := strictSnapshotMetaCandidateKey(candidate)
	if key == "" {
		return 0, false
	}
	n.snapshotProofMu.Lock()
	proof.Timestamp = time.Now().Unix()
	// The full registry was needed only to authenticate the incoming proof.
	// Cached quorum selection uses its committed hash and signature, so retaining
	// a registry copy in every validator proof wastes memory quadratically.
	proof.ValidatorRegistry = nil
	if n.snapshotProofs == nil {
		n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	}
	if n.snapshotProofs[key] == nil {
		n.snapshotProofs[key] = make(map[string]SnapshotProof)
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := n.snapshotProofs[key][proof.Validator]; ok {
		if strings.EqualFold(strings.TrimSpace(existing.SignatureHex), strings.TrimSpace(proof.SignatureHex)) {
			// `votes` stores the value produced by this operation.
			votes := len(n.snapshotProofs[key])
			n.snapshotProofMu.Unlock()
			return votes, true
		}
	}
	n.snapshotProofs[key][proof.Validator] = proof
	// `validatorID` tracks whether the related condition is satisfied.
	for validatorID := range n.snapshotProofs[key] {
		candidate.Validators[validatorID] = struct{}{}
	}
	n.updateSnapshotAnchorCacheLocked(candidate)
	n.boundSnapshotProofCachesLocked()
	if _, retained := n.snapshotProofs[key]; !retained {
		n.snapshotProofMu.Unlock()
		return 0, true
	}
	// `votes` stores the value produced by this operation.
	votes := len(candidate.Validators)
	n.snapshotProofMu.Unlock()

	// `vote` stores the value produced by this operation.
	vote := snapshotVoteFromProof(&proof)
	_, _, _ = n.updateSnapshotSessionVote(vote)
	// `session` stores the value produced by this operation.
	session := n.snapshotSessionSnapshot()
	// `required` stores the request data being processed.
	required := n.snapshotSessionRequiredVotesForHeight(proof.Height)
	if session.Required > 0 {
		required = session.Required
	}
	if required <= 0 {
		required = 1
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := uint64(0)
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
	}
	if session.Active &&
		session.CheckpointHeight == proof.CheckpointHeight &&
		proof.Height > localHeight &&
		proof.Height > session.CandidateHeight &&
		votes >= required {
		n.setSnapshotSessionCandidate(proof.Height, proof.CheckpointHeight)
	}
	return votes, true
}

// handleSnapshotProof handles snapshot proof.
func (n *Node) handleSnapshotProof(proof SnapshotProof) {
	n.handleSnapshotProofFromPeer(proof, "")
}

// handleSnapshotProofFromPeer handles snapshot proof from peer.
func (n *Node) handleSnapshotProofFromPeer(proof SnapshotProof, peerID string) {
	n.handleSnapshotProofFromPeerWithPolicy(proof, peerID, true)
}

// handleSnapshotProofFromGossip handles snapshot proof from gossip.
func (n *Node) handleSnapshotProofFromGossip(proof SnapshotProof, peerID string) {
	n.handleSnapshotProofFromPeerWithPolicy(proof, peerID, false)
}

// handleSnapshotProofFromPeerWithPolicy handles snapshot proof from peer with policy.
func (n *Node) handleSnapshotProofFromPeerWithPolicy(proof SnapshotProof, peerID string, penalizeInvalidPeer bool) {
	if n == nil {
		return
	}
	normalizeSnapshotProof(&proof)
	// `votes` and `ok` store whether the related condition is satisfied.
	votes, ok := n.recordSnapshotProof(proof)
	if !ok {
		if penalizeInvalidPeer && strings.TrimSpace(peerID) != "" {
			n.recordSyncPeerInvalidProof(peerID)
		}
		return
	}
	if votes == 0 {
		return
	}
	// `validatorID` stores whether the related condition is satisfied.
	if validatorID := normalizeValidatorID(proof.Validator); validatorID != "" {
		if peerID = strings.TrimSpace(peerID); peerID != "" {
			// `candidate` stores the value produced by this operation.
			if candidate := snapshotProofCandidateFromProof(&proof); candidate != nil {
				// `key` stores the key used to access the related value.
				if key := strictSnapshotMetaCandidateKey(candidate); key != "" {
					n.snapshotProofMu.Lock()
					if n.snapshotProofProviders == nil {
						n.snapshotProofProviders = make(map[string]map[string]string)
					}
					if n.snapshotProofProviders[key] == nil {
						n.snapshotProofProviders[key] = make(map[string]string)
					}
					if strings.TrimSpace(n.snapshotProofProviders[key][validatorID]) == "" {
						n.snapshotProofProviders[key][validatorID] = peerID
					}
					n.snapshotProofMu.Unlock()
				}
			}
		}
		n.updateSnapshotCatalogProofSet(proof.Height, []string{validatorID})
	}
	if DebugConsensus || DebugSync {
		fmt.Printf("[SNAPSHOT-PROOF] height=%d checkpoint=%d validator=%s votes=%d hash=%s\n",
			proof.Height,
			proof.CheckpointHeight,
			normalizeValidatorID(proof.Validator),
			votes,
			snapshotProofLogHash(&proof),
		)
	}
}

// decodeSnapshotProofGossipPayload implements the decode snapshot proof gossip payload helper.
func decodeSnapshotProofGossipPayload(data []byte) (SnapshotProof, bool) {
	// `wrapped` stores the value used by this operation.
	var wrapped Message
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Type != "" {
		if wrapped.Type != MsgSnapshotProof {
			return SnapshotProof{}, false
		}
		data = wrapped.Data
	}
	// `proof` stores the value used by this operation.
	var proof SnapshotProof
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &proof); err != nil {
		return SnapshotProof{}, false
	}
	return proof, true
}

// handleSnapshotProofGossip handles snapshot proof gossip.
func (n *Node) handleSnapshotProofGossip(sub *pubsub.Subscription) {
	if n == nil || sub == nil {
		return
	}
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()
	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		// `peerID` stores the value produced by this operation.
		peerID := msg.ReceivedFrom.String()
		// `proof` and `ok` store whether the related condition is satisfied.
		if proof, ok := decodeSnapshotProofGossipPayload(msg.Data); ok {
			n.handleSnapshotProofFromGossip(proof, peerID)
			continue
		}
		if n.handleConsensusEnvelopeFromPeer(msg.Data, peerID) {
			continue
		}
	}
}

// listenSnapshotProofs implements the listen snapshot proofs helper.
func (n *Node) listenSnapshotProofs(ctx context.Context) {
	if n == nil || n.SnapshotProofSub == nil {
		return
	}
	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := n.SnapshotProofSub.Next(ctx)
		if err != nil {
			log.Println("snapshot proof listener stopped:", err)
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		// `peerID` stores the value produced by this operation.
		peerID := msg.ReceivedFrom.String()
		// `proof` and `ok` store whether the related condition is satisfied.
		if proof, ok := decodeSnapshotProofGossipPayload(msg.Data); ok {
			n.handleSnapshotProofFromGossip(proof, peerID)
			continue
		}
		if n.handleConsensusEnvelopeFromPeer(msg.Data, peerID) {
			continue
		}
	}
}

// publishSnapshotProof implements the publish snapshot proof helper.
func (n *Node) publishSnapshotProof(proof SnapshotProof) bool {
	if n == nil {
		return false
	}
	normalizeSnapshotProof(&proof)
	if proof.Height == 0 || proof.Validator == "" || proof.SignatureHex == "" {
		return false
	}
	// `candidate` stores the value produced by this operation.
	candidate := snapshotProofCandidateFromProof(&proof)
	if candidate == nil {
		return false
	}
	// `broadcastKey` stores the key used to access the related value.
	broadcastKey := fmt.Sprintf("%s|%s|%s", proof.Validator, strictSnapshotMetaCandidateKey(candidate), proof.SignatureHex)
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.snapshotProofMu.Lock()
	if n.snapshotProofLastPublished == broadcastKey && now.Sub(n.snapshotProofLastPublishedAt) < 10*time.Second {
		n.snapshotProofMu.Unlock()
		_, _ = n.recordSnapshotProof(proof)
		return true
	}
	n.snapshotProofLastPublished = broadcastKey
	n.snapshotProofLastPublishedAt = now
	n.snapshotProofMu.Unlock()

	_, _ = n.recordSnapshotProof(proof)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(proof)
	if err != nil {
		return false
	}
	// `wrapped` and `err` store the error produced by this operation.
	wrapped, err := MarshalP2PMessage(Message{Type: MsgSnapshotProof, Data: raw})
	if err != nil {
		return false
	}
	if n.SnapshotProofTopic != nil {
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(n.RootContext(), validatorHeartbeatPublishTimeout())
		// `err` stores the error produced by this operation.
		err := n.SnapshotProofTopic.Publish(ctx, wrapped)
		cancel()
		if err == nil {
			return true
		}
	}
	if n.PubSub != nil {
		// `err` stores the error produced by this operation.
		if err := n.PubSub.Publish(TopicSnapshotProof, wrapped); err == nil {
			return true
		}
	}
	return false
}

// markValidatorSnapshotPublishResult implements the mark validator snapshot publish result helper.
func (n *Node) markValidatorSnapshotPublishResult(snapshot *StateSnapshot, err error) {
	if n == nil {
		return
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.snapshotProofMu.Lock()
	defer n.snapshotProofMu.Unlock()
	if snapshot != nil {
		n.validatorSnapshotPublishHeight = snapshot.Height
		n.validatorSnapshotPublishHash = strings.TrimSpace(snapshot.SnapshotHash)
	}
	if err != nil {
		n.validatorSnapshotPublishError = strings.TrimSpace(err.Error())
		return
	}
	if snapshot != nil {
		n.validatorSnapshotPublished = cloneStateSnapshot(snapshot)
	}
	n.validatorSnapshotPublishAt = now
	n.validatorSnapshotPublishError = ""
}

// validatorSnapshotPublicationState implements the validator snapshot publication state helper.
func (n *Node) validatorSnapshotPublicationState() (uint64, string, time.Time, string) {
	if n == nil {
		return 0, "", time.Time{}, ""
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	return n.validatorSnapshotPublishHeight,
		strings.TrimSpace(n.validatorSnapshotPublishHash),
		n.validatorSnapshotPublishAt,
		strings.TrimSpace(n.validatorSnapshotPublishError)
}

// validatorSnapshotPublicationStatusState returns the publication state exposed
// by status/metrics. The live publish state is intentionally memory-only so the
// snapshot signer can reannounce after a process restart; for observability,
// though, a restarted validator should still report its latest durable committed
// snapshot height instead of looking like it has never published one.
func (n *Node) validatorSnapshotPublicationStatusState() (uint64, string, time.Time, string) {
	publishedHeight, publishedHash, publishedAt, publishErr := n.validatorSnapshotPublicationState()
	if n == nil ||
		publishedHeight > 0 ||
		strings.TrimSpace(publishErr) != "" ||
		normalizeNodeRole(n.Role) != "validator" {
		return publishedHeight, publishedHash, publishedAt, publishErr
	}
	targetHeight := uint64(0)
	if n.Blockchain != nil {
		targetHeight = n.Blockchain.Height()
	}
	snapshot, err := n.GetSnapshotAtOrBelow(targetHeight)
	if err != nil || snapshot == nil || snapshot.Height == 0 {
		return publishedHeight, publishedHash, publishedAt, publishErr
	}
	populateSnapshotDerivedFields(snapshot)
	if !n.snapshotMatchesLocalAnchor(snapshot) {
		return publishedHeight, publishedHash, publishedAt, publishErr
	}
	publishedHash = strings.TrimSpace(snapshot.SnapshotHash)
	if publishedHash == "" {
		publishedHash = snapshotCanonicalHash(snapshot)
	}
	return snapshot.Height, publishedHash, publishedAt, publishErr
}

// publishedValidatorSnapshotForSyncRequest implements the published validator snapshot for sync request helper.
func (n *Node) publishedValidatorSnapshotForSyncRequest(targetHeight uint64) *StateSnapshot {
	if n == nil {
		return nil
	}
	n.snapshotProofMu.RLock()
	// `snapshot` stores the value produced by this operation.
	snapshot := cloneStateSnapshot(n.validatorSnapshotPublished)
	n.snapshotProofMu.RUnlock()
	if snapshot == nil || snapshot.Height == 0 {
		return nil
	}
	if targetHeight > 0 && snapshot.Height > targetHeight {
		return nil
	}
	populateSnapshotDerivedFields(snapshot)
	if !n.snapshotMatchesLocalAnchor(snapshot) {
		return nil
	}
	return snapshot
}

// validatorSnapshotAdaptiveReason implements the validator snapshot adaptive reason helper.
func validatorSnapshotAdaptiveReason(localHeight uint64, peerHeight uint64) (string, uint64, bool) {
	if localHeight == 0 || peerHeight >= localHeight {
		return "", 0, false
	}
	// `lag` stores the value produced by this operation.
	lag := localHeight - peerHeight
	if peerHeight <= 1 {
		return "new_node_join", lag, true
	}
	// `threshold` stores the value produced by this operation.
	if threshold := syncSnapshotPublishNewNodeThresholdBlocks(); threshold > 0 && lag >= threshold {
		return "new_node_join", lag, true
	}
	// `threshold` stores the value produced by this operation.
	if threshold := syncSnapshotPublishLagThresholdBlocks(); threshold > 0 && lag >= threshold {
		return "lagging_peer", lag, true
	}
	return "", 0, false
}

// betterValidatorSnapshotAdaptiveReason implements the better validator snapshot adaptive reason helper.
func betterValidatorSnapshotAdaptiveReason(reason string, lag uint64, current string, currentLag uint64) bool {
	if strings.TrimSpace(reason) == "" {
		return false
	}
	if strings.TrimSpace(current) == "" {
		return true
	}
	if reason != current {
		return reason == "new_node_join"
	}
	return lag > currentLag
}

// validatorSnapshotAdaptivePublishDecision implements the validator snapshot adaptive publish decision helper.
func (n *Node) validatorSnapshotAdaptivePublishDecision(tip uint64) (bool, string) {
	if n == nil || tip == 0 || normalizeNodeRole(n.Role) != "validator" {
		return false, ""
	}
	// `bestReason` stores the value produced by this operation.
	bestReason := ""
	// `bestLag` stores the value produced by this operation.
	bestLag := uint64(0)
	// `consider` stores the value produced by this operation.
	consider := func(height uint64) {
		// `reason`, `lag`, and `ok` store whether the related condition is satisfied.
		reason, lag, ok := validatorSnapshotAdaptiveReason(tip, height)
		if !ok {
			return
		}
		if betterValidatorSnapshotAdaptiveReason(reason, lag, bestReason, bestLag) {
			bestReason = reason
			bestLag = lag
		}
	}

	n.peerStateMu.Lock()
	// `height` tracks the current values while iterating.
	for _, height := range n.peerAckHeight {
		consider(height)
	}
	n.peerStateMu.Unlock()

	// `now` stores the value produced by this operation.
	now := time.Now()
	// `maxAge` stores the value produced by this operation.
	maxAge := validatorLivenessHeartbeatTTL() + validatorLivenessGrace()
	// `selfID` stores the value produced by this operation.
	selfID := n.localConsensusValidatorIDForHeight(tip)
	n.validatorMu.RLock()
	// `id` and `st` track the current position in the related collection.
	for id, st := range n.validatorStatus {
		if st == nil || normalizeValidatorID(id) == selfID {
			continue
		}
		if !st.LastSeen.IsZero() {
			// `age` stores the value produced by this operation.
			age := now.Sub(st.LastSeen)
			if age < 0 {
				age = 0
			}
			if age > maxAge {
				continue
			}
		}
		// `height` stores the value produced by this operation.
		height := st.FinalizedHeight
		if height == 0 {
			height = st.ReportedHeight
		}
		consider(height)
	}
	n.validatorMu.RUnlock()

	return bestReason != "", bestReason
}

// isAutomaticValidatorSnapshotPublishReason implements the is automatic validator snapshot publish reason helper.
func isAutomaticValidatorSnapshotPublishReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	return strings.HasPrefix(reason, "validator_") ||
		strings.HasPrefix(reason, "adaptive_") ||
		strings.HasPrefix(reason, "snapshot_proof_signer")
}

// isAdaptiveValidatorSnapshotPublishReason implements the is adaptive validator snapshot publish reason helper.
func isAdaptiveValidatorSnapshotPublishReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	return strings.HasPrefix(reason, "adaptive_") ||
		strings.Contains(reason, "new_node_join") ||
		strings.Contains(reason, "lagging_peer") ||
		strings.Contains(reason, "sync_lag")
}

// shouldPublishAutomaticValidatorSnapshotAtHeight implements the should publish automatic validator snapshot at height helper.
func shouldPublishAutomaticValidatorSnapshotAtHeight(height uint64) bool {
	return shouldAutoCreateSnapshotAtHeight(height)
}

// maybeTriggerAdaptiveValidatorSnapshotPublish implements the maybe trigger adaptive validator snapshot publish helper.
func (n *Node) maybeTriggerAdaptiveValidatorSnapshotPublish(trigger string) {
	if n == nil || normalizeNodeRole(n.Role) != "validator" || n.Blockchain == nil {
		return
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	// `force` and `reason` store the value produced by this operation.
	force, reason := n.validatorSnapshotAdaptivePublishDecision(tip)
	if !force || strings.TrimSpace(reason) == "" {
		return
	}
	if strings.TrimSpace(reason) == "new_node_join" {
		n.markSnapshotPublishBoost(reason)
	}
	// `publishedHeight`, `publishedAt`, and `publishErr` store the error produced by this operation.
	publishedHeight, _, publishedAt, publishErr := n.validatorSnapshotPublicationState()
	if publishedHeight >= tip && strings.TrimSpace(publishErr) == "" && !publishedAt.IsZero() &&
		time.Since(publishedAt) < syncSnapshotPublishReannounceCooldown() {
		return
	}
	go func(height uint64, publishReason string) {
		// `snapshot` and `err` store the error produced by this operation.
		snapshot, err := n.publishRequiredValidatorSnapshot(publishReason, true)
		if err != nil {
			if DebugConsensus || DebugSync {
				log.Printf("[SNAPSHOT-PUBLISH] trigger=%s height=%d reason=%s status=failed err=%v",
					strings.TrimSpace(trigger),
					height,
					strings.TrimSpace(publishReason),
					err,
				)
			}
			return
		}
		if snapshot != nil && (DebugConsensus || DebugSync) {
			fmt.Printf("[SNAPSHOT-PUBLISH] trigger=%s height=%d reason=%s validator=%s hash=%s\n",
				strings.TrimSpace(trigger),
				snapshot.Height,
				strings.TrimSpace(publishReason),
				n.localConsensusValidatorIDForHeight(snapshot.Height),
				ShortHash(snapshot.SnapshotHash),
			)
		}
	}(tip, "adaptive_"+strings.TrimSpace(trigger)+"_"+strings.TrimSpace(reason))
}

// validatorSnapshotOfferTargets implements the validator snapshot offer targets helper.
func (n *Node) validatorSnapshotOfferTargets(height uint64) []string {
	if n == nil {
		return nil
	}
	if height == 0 && n.Blockchain != nil {
		height = n.Blockchain.Height()
	}
	selfID := n.localConsensusValidatorIDForHeight(height)
	// `targets` stores the value produced by this operation.
	targets := make(map[string]struct{})
	// `addAll` stores the value produced by this operation.
	addAll := func(ids []string) {
		// `raw` tracks the current values while iterating.
		for _, raw := range canonicalValidatorIDs(ids) {
			// `id` stores the current position in the related collection.
			id := normalizeValidatorID(raw)
			if id == "" || id == selfID {
				continue
			}
			targets[id] = struct{}{}
		}
	}
	if height > 0 {
		addAll(n.GetConsensusValidators(int(height)))
		addAll(n.GetConsensusValidators(int(height + 1)))
	}
	if len(targets) == 0 {
		n.validatorMu.RLock()
		// `id` tracks the current position in the related collection.
		for id := range n.validatorStatus {
			// `nid` stores the value produced by this operation.
			nid := normalizeValidatorID(id)
			if nid == "" || nid == selfID {
				continue
			}
			targets[nid] = struct{}{}
		}
		n.validatorMu.RUnlock()
	}
	if len(targets) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(targets))
	// `id` tracks the current position in the related collection.
	for id := range targets {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// publishRequiredValidatorSnapshot implements the publish required validator snapshot helper.
func (n *Node) publishRequiredValidatorSnapshot(reason string, force bool) (*StateSnapshot, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	n.validatorSnapshotPublishMu.Lock()
	defer n.validatorSnapshotPublishMu.Unlock()
	if normalizeNodeRole(n.Role) != "validator" {
		return nil, nil
	}
	if n.Blockchain == nil || n.Blockchain.Height() == 0 {
		// `err` stores the error produced by this operation.
		err := fmt.Errorf("committed tip unavailable")
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if isAutomaticValidatorSnapshotPublishReason(reason) {
		// `publishedHeight` and `publishErr` store the error produced by this operation.
		publishedHeight, _, _, publishErr := n.validatorSnapshotPublicationState()
		if publishedHeight >= tip && strings.TrimSpace(publishErr) == "" {
			return nil, nil
		}
		if !shouldPublishAutomaticValidatorSnapshotAtHeight(tip) {
			if DebugConsensus || DebugSync {
				validatorID := n.localConsensusValidatorIDForHeight(tip)
				// `key` stores the key used to access the related value.
				key := fmt.Sprintf("snapshot_publish_interval_deferred:%s:%d", validatorID, tip)
				if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
					fmt.Printf("[SNAPSHOT-PUBLISH] deferred height=%d reason=auto_publish_interval_%d\n",
						tip,
						syncCheckpointIntervalBlocks(),
					)
				}
			}
			return nil, nil
		}
		if !force && !n.authoritativeExecutionLedgerAvailable(tip) {
			return nil, nil
		}
	}
	if !force && !shouldAutoCreateSnapshotAtHeight(tip) {
		if strings.Contains(reason, "snapshot_proof_signer") {
			force = true
		} else {
			if DebugConsensus || DebugSync {
				validatorID := n.localConsensusValidatorIDForHeight(tip)
				// `key` stores the key used to access the related value.
				key := fmt.Sprintf("snapshot_publish_deferred:%s:%d", validatorID, tip)
				if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
					fmt.Printf("[SNAPSHOT-PUBLISH] deferred height=%d reason=checkpoint_interval\n", tip)
				}
			}
			return nil, nil
		}
	}
	if !isValidatorSigningKeyUsable(n.ValidatorKey) {
		// `err` stores the error produced by this operation.
		err := fmt.Errorf("validator key unavailable for snapshot publication")
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	if len(n.ValidatorKey.PublicKey) == ed25519.PublicKeySize {
		// `selfID` stores the value produced by this operation.
		selfID := n.localConsensusValidatorIDForHeight(tip)
		if selfID == "" {
			selfID = normalizeValidatorID(n.ValidatorKey.ID)
		}
		if selfID != "" {
			validatorPubKeysMu.Lock()
			ValidatorPubKeys[selfID] = append(ed25519.PublicKey(nil), n.ValidatorKey.PublicKey...)
			validatorPubKeysMu.Unlock()
		}
	}
	// `snapshot`, `source`, and `err` store the error produced by this operation.
	snapshot, _, source, err := n.createCommittedTipSnapshot(reason, false)
	if err != nil {
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	if snapshot == nil {
		// `err` stores the error produced by this operation.
		err := fmt.Errorf("snapshot unavailable after materialization")
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	populateSnapshotDerivedFields(snapshot)
	n.attachSnapshotCheckpointProof(snapshot)
	snapshot.SnapshotHash = snapshotCanonicalHash(snapshot)
	// `proof` stores the value produced by this operation.
	proof := snapshotProofFromSnapshot(n.localConsensusValidatorIDForHeight(snapshot.Height), snapshot)
	if proof.Height == 0 || proof.SignatureHex == "" {
		// `err` stores the error produced by this operation.
		err := fmt.Errorf("snapshot proof unavailable height=%d source=%s", snapshot.Height, strings.TrimSpace(source))
		n.markValidatorSnapshotPublishResult(snapshot, err)
		return nil, err
	}
	if !n.publishSnapshotProof(proof) {
		// `err` stores the error produced by this operation.
		err := fmt.Errorf("snapshot proof publish failed height=%d source=%s", snapshot.Height, strings.TrimSpace(source))
		n.markValidatorSnapshotPublishResult(snapshot, err)
		return nil, err
	}
	if force {
		_ = n.publishSnapshotMetaGossipForce(snapshot)
		_ = n.publishSnapshotChunkGossipForce(snapshot)
	} else {
		_ = n.publishSnapshotMetaGossip(snapshot)
		_ = n.publishSnapshotChunkGossip(snapshot)
	}
	// `validatorID` tracks whether the related condition is satisfied.
	for _, validatorID := range n.validatorSnapshotOfferTargets(snapshot.Height) {
		n.maybeOfferSnapshotToValidator(validatorID, snapshot.Height)
	}
	n.markValidatorSnapshotPublishResult(snapshot, nil)
	return snapshot, nil
}

// startSnapshotProofSigner implements the start snapshot proof signer helper.
func (n *Node) startSnapshotProofSigner(ctx context.Context) {
	if n == nil {
		return
	}
	if normalizeNodeRole(n.Role) == "validator" {
		validatorID := n.localConsensusValidatorID()
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_publish_required_start:%s", validatorID)
		if n.shouldLogLivenessReason(key, time.Minute) {
			log.Printf("[SNAPSHOT-PUBLISH-REQUIRED] validator=%s policy=strict_snapshot_only", validatorID)
		}
	}
	// `publish` stores the value produced by this operation.
	publish := func() {
		if n == nil || normalizeNodeRole(n.Role) != "validator" {
			return
		}
		// `tip` stores the value produced by this operation.
		tip := uint64(0)
		if n.Blockchain != nil {
			tip = n.Blockchain.Height()
		}
		// `force` and `forceReason` store the value produced by this operation.
		force, forceReason := n.validatorSnapshotAdaptivePublishDecision(tip)
		// `reason` stores the value produced by this operation.
		reason := "snapshot_proof_signer"
		if force && strings.TrimSpace(forceReason) != "" {
			reason = "adaptive_snapshot_proof_signer_" + strings.TrimSpace(forceReason)
			if strings.TrimSpace(forceReason) == "new_node_join" {
				n.markSnapshotPublishBoost(forceReason)
			}
		}
		// `snapshot` and `err` store the error produced by this operation.
		snapshot, err := n.publishRequiredValidatorSnapshot(reason, force)
		if err != nil {
			validatorID := n.localConsensusValidatorIDForHeight(tip)
			// `key` stores the key used to access the related value.
			key := fmt.Sprintf("snapshot_publish_required:%s:%d", validatorID, tip)
			if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
				log.Printf("[SNAPSHOT-PUBLISH-REQUIRED] validator=%s height=%d error=%v", validatorID, tip, err)
			}
			return
		}
		if snapshot != nil && (DebugConsensus || DebugSync) {
			// `proof` stores the value produced by this operation.
			validatorID := n.localConsensusValidatorIDForHeight(snapshot.Height)
			proof := snapshotProofFromSnapshot(validatorID, snapshot)
			fmt.Printf("[SNAPSHOT-PUBLISH] height=%d checkpoint=%d validator=%s hash=%s reason=%s\n",
				snapshot.Height,
				proof.CheckpointHeight,
				validatorID,
				ShortHash(snapshot.SnapshotHash),
				strings.TrimSpace(reason),
			)
		}
	}
	publish()
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			publish()
		case <-n.shutdownCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// snapshotProofObservationsFromGroupLocked implements the snapshot proof observations from group locked helper.
func (n *Node) snapshotProofObservationsFromGroupLocked(candidateKey string, group map[string]SnapshotProof, validatorProviders map[string]string, targetHeight uint64, minHeight uint64) ([]strictSnapshotMetaObservation, []SnapshotVote) {
	if len(group) == 0 {
		return nil, nil
	}
	// `validatorIDs` stores whether the related condition is satisfied.
	validatorIDs := make([]string, 0, len(group))
	// `validatorID` tracks whether the related condition is satisfied.
	for validatorID := range group {
		validatorIDs = append(validatorIDs, validatorID)
	}
	sort.Strings(validatorIDs)
	// `observations` stores the value produced by this operation.
	observations := make([]strictSnapshotMetaObservation, 0, len(validatorIDs))
	// `votes` stores the value produced by this operation.
	votes := make([]SnapshotVote, 0, len(validatorIDs))
	// `validatorID` tracks whether the related condition is satisfied.
	for _, validatorID := range validatorIDs {
		// `proof` stores the value produced by this operation.
		proof := group[validatorID]
		// `candidate` stores the value produced by this operation.
		candidate := snapshotProofCandidateFromProof(&proof)
		if candidate == nil {
			continue
		}
		if targetHeight > 0 && candidate.Height > targetHeight {
			continue
		}
		if minHeight > 0 && candidate.Height < minHeight {
			continue
		}
		// `provider` stores the value produced by this operation.
		provider := ""
		// `normalizedValidatorID` stores the value produced by this operation.
		normalizedValidatorID := normalizeValidatorID(validatorID)
		if len(validatorProviders) > 0 {
			provider = strings.TrimSpace(validatorProviders[normalizedValidatorID])
		}
		if provider == "" && n.snapshotProofProviders != nil {
			provider = strings.TrimSpace(n.snapshotProofProviders[strings.TrimSpace(candidateKey)][normalizedValidatorID])
		}
		if len(validatorProviders) > 0 && provider == "" {
			continue
		}
		observations = append(observations, strictSnapshotMetaObservation{
			Provider:    provider,
			ValidatorID: validatorID,
			Candidate:   candidate,
		})
		votes = append(votes, snapshotVoteFromProof(&proof))
	}
	return observations, votes
}

// cachedSnapshotProofObservations implements the cached snapshot proof observations helper.
func (n *Node) cachedSnapshotProofObservations(targetHeight uint64, minHeight uint64, validatorProviders map[string]string, required int) ([]strictSnapshotMetaObservation, []SnapshotVote) {
	if n == nil {
		return nil, nil
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	if len(n.snapshotProofs) == 0 {
		return nil, nil
	}
	if required <= 0 {
		required = 1
	}
	// `cache` and `ok` store whether the related condition is satisfied.
	if cache, ok := n.cachedSnapshotAnchorLocked(targetHeight, minHeight); ok && cache.CandidateKey != "" && cache.Votes >= required {
		// `group` stores the value produced by this operation.
		if group := n.snapshotProofs[cache.CandidateKey]; len(group) > 0 {
			// `observations` and `votes` store the value produced by this operation.
			observations, votes := n.snapshotProofObservationsFromGroupLocked(cache.CandidateKey, group, validatorProviders, targetHeight, minHeight)
			if len(observations) >= required {
				return observations, votes
			}
		}
	}
	// `allObservations` stores the value produced by this operation.
	allObservations := make([]strictSnapshotMetaObservation, 0)
	// `allVotes` stores the value produced by this operation.
	allVotes := make([]SnapshotVote, 0)
	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(n.snapshotProofs))
	// `key` tracks the key used to access the related value.
	for key := range n.snapshotProofs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// `key` tracks the key used to access the related value.
	for _, key := range keys {
		// `observations` and `votes` store the value produced by this operation.
		observations, votes := n.snapshotProofObservationsFromGroupLocked(key, n.snapshotProofs[key], validatorProviders, targetHeight, minHeight)
		allObservations = append(allObservations, observations...)
		allVotes = append(allVotes, votes...)
	}
	return allObservations, allVotes
}

// snapshotProofStats implements the snapshot proof stats helper.
func (n *Node) snapshotProofStats() (int, int, int) {
	if n == nil {
		return 0, 0, 0
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	// `groups` stores the value produced by this operation.
	groups := len(n.snapshotProofs)
	// `proofs` stores the value produced by this operation.
	proofs := 0
	// `bestVotes` stores the value produced by this operation.
	bestVotes := 0
	// `group` tracks the current values while iterating.
	for _, group := range n.snapshotProofs {
		// `size` stores the measured quantity used by this operation.
		size := len(group)
		proofs += size
		if size > bestVotes {
			bestVotes = size
		}
	}
	return groups, proofs, bestVotes
}

// snapshotExportBaseDir implements the snapshot export base dir helper.
func (n *Node) snapshotExportBaseDir() string {
	if n == nil {
		return ""
	}
	// `base` stores the value produced by this operation.
	base := strings.TrimSpace(n.DataDir)
	if base == "" {
		return ""
	}
	// `dir` stores the value produced by this operation.
	dir := filepath.Join(base, "snapshots")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// snapshotExportDirForHeight implements the snapshot export dir for height helper.
func (n *Node) snapshotExportDirForHeight(height uint64) string {
	// `base` stores the value produced by this operation.
	base := n.snapshotExportBaseDir()
	if base == "" || height == 0 {
		return ""
	}
	return filepath.Join(base, fmt.Sprintf("%020d", height))
}

// exportSnapshotArtifacts implements the export snapshot artifacts helper.
func (n *Node) exportSnapshotArtifacts(snapshot *StateSnapshot) error {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return nil
	}
	// `base` stores the value produced by this operation.
	base := n.snapshotExportBaseDir()
	if base == "" {
		return nil
	}
	// `manifest`, `payload`, and `err` store the error produced by this operation.
	manifest, payload, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return err
	}
	// `dir` stores the value produced by this operation.
	dir := n.snapshotExportDirForHeight(snapshot.Height)
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// `metaPath` stores the value produced by this operation.
	metaPath := filepath.Join(dir, "meta.json")
	// `rawManifest` and `err` store the error produced by this operation.
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := os.WriteFile(metaPath, rawManifest, 0o600); err != nil {
		return err
	}
	// `chunkSize` stores the measured quantity used by this operation.
	chunkSize := manifest.ChunkSize
	if chunkSize == 0 {
		chunkSize = syncSnapshotChunkSizeBytes()
	}
	// `idx` stores the current position in the related collection.
	for idx := uint64(0); idx < manifest.ChunkCount; idx++ {
		// `start` stores the value produced by this operation.
		start := idx * chunkSize
		// `end` stores the value produced by this operation.
		end := start + chunkSize
		if end > uint64(len(payload)) {
			end = uint64(len(payload))
		}
		// `chunkPath` stores the value produced by this operation.
		chunkPath := filepath.Join(dir, fmt.Sprintf("chunk_%04d", idx))
		// `err` stores the error produced by this operation.
		if err := os.WriteFile(chunkPath, payload[start:end], 0o600); err != nil {
			return err
		}
	}
	return nil
}

// exportSnapshotArtifactsBestEffort implements the export snapshot artifacts best effort helper.
func (n *Node) exportSnapshotArtifactsBestEffort(snapshot *StateSnapshot, source string) {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return
	}
	// `err` stores the error produced by this operation.
	if err := n.exportSnapshotArtifacts(snapshot); err != nil {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_export:%d:%s", snapshot.Height, strings.TrimSpace(source))
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-EXPORT] height=%d source=%s err=%v", snapshot.Height, strings.TrimSpace(source), err)
		}
	}
}
