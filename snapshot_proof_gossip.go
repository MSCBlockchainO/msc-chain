package main

import (
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

const validatorSnapshotAutoPublishIntervalBlocks uint64 = 10

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

func snapshotProofFromSnapshot(validatorID string, snapshot *StateSnapshot) SnapshotProof {
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
	proof.CheckpointDomain = strings.TrimSpace(snapshot.CheckpointDomain)
	proof.Timestamp = time.Now().Unix()
	if proof.Validator != "" && len(snapshot.CheckpointProof) > 0 {
		if sigHex := strings.TrimSpace(snapshot.CheckpointProof[proof.Validator]); sigHex != "" {
			proof.SignatureHex = sigHex
		} else {
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

func snapshotStubFromProof(proof *SnapshotProof) *StateSnapshot {
	if proof == nil {
		return nil
	}
	clone := *proof
	normalizeSnapshotProof(&clone)
	if clone.Height == 0 || clone.Validator == "" || clone.SignatureHex == "" {
		return nil
	}
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

func snapshotProofCandidateFromProof(proof *SnapshotProof) *strictSnapshotMetaCandidate {
	if proof == nil {
		return nil
	}
	clone := *proof
	normalizeSnapshotProof(&clone)
	if clone.Height == 0 || clone.SnapshotHash == "" || clone.StateRoot == "" || clone.ValidatorSetHash == "" || clone.ValidatorRegistryHash == "" {
		return nil
	}
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

func snapshotVoteFromProof(proof *SnapshotProof) SnapshotVote {
	if proof == nil {
		return SnapshotVote{}
	}
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

func (n *Node) verifySnapshotProof(proof *SnapshotProof) bool {
	if n == nil || proof == nil {
		return false
	}
	snapshot := snapshotStubFromProof(proof)
	if snapshot == nil {
		return false
	}
	return n.verifySnapshotCheckpointProofForValidator(snapshot, proof.Validator)
}

func snapshotAnchorCacheFromCandidate(candidate *strictSnapshotMetaCandidate) SnapshotAnchorCache {
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
	for id := range candidate.Validators {
		cache.Validators = append(cache.Validators, id)
	}
	sort.Strings(cache.Validators)
	cache.Votes = len(cache.Validators)
	cache.UpdatedAt = time.Now()
	return cache
}

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

func (n *Node) updateSnapshotAnchorCacheLocked(candidate *strictSnapshotMetaCandidate) {
	if n == nil || candidate == nil || candidate.CheckpointHeight == 0 {
		return
	}
	if n.snapshotAnchorCache == nil {
		n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)
	}
	cache := snapshotAnchorCacheFromCandidate(candidate)
	existing := n.snapshotAnchorCache[candidate.CheckpointHeight]
	if betterSnapshotAnchorCache(cache, existing) {
		n.snapshotAnchorCache[candidate.CheckpointHeight] = cache
	}
}

func (n *Node) cachedSnapshotAnchorLocked(targetHeight uint64, minHeight uint64) (SnapshotAnchorCache, bool) {
	if n == nil || len(n.snapshotAnchorCache) == 0 {
		return SnapshotAnchorCache{}, false
	}
	var best SnapshotAnchorCache
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

func (n *Node) recordSnapshotProof(proof SnapshotProof) (int, bool) {
	if n == nil {
		return 0, false
	}
	normalizeSnapshotProof(&proof)
	if !n.verifySnapshotProof(&proof) {
		return 0, false
	}
	candidate := snapshotProofCandidateFromProof(&proof)
	if candidate == nil {
		return 0, false
	}
	key := strictSnapshotMetaCandidateKey(candidate)
	if key == "" {
		return 0, false
	}
	n.snapshotProofMu.Lock()
	if n.snapshotProofs == nil {
		n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	}
	if n.snapshotProofs[key] == nil {
		n.snapshotProofs[key] = make(map[string]SnapshotProof)
	}
	if existing, ok := n.snapshotProofs[key][proof.Validator]; ok {
		if strings.EqualFold(strings.TrimSpace(existing.SignatureHex), strings.TrimSpace(proof.SignatureHex)) {
			votes := len(n.snapshotProofs[key])
			n.snapshotProofMu.Unlock()
			return votes, false
		}
	}
	n.snapshotProofs[key][proof.Validator] = proof
	for validatorID := range n.snapshotProofs[key] {
		candidate.Validators[validatorID] = struct{}{}
	}
	n.updateSnapshotAnchorCacheLocked(candidate)
	votes := len(candidate.Validators)
	n.snapshotProofMu.Unlock()

	vote := snapshotVoteFromProof(&proof)
	_, _, _ = n.updateSnapshotSessionVote(vote)
	session := n.snapshotSessionSnapshot()
	required := n.snapshotSessionRequiredVotesForHeight(proof.Height)
	if session.Required > 0 {
		required = session.Required
	}
	if required <= 0 {
		required = 1
	}
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

func (n *Node) handleSnapshotProof(proof SnapshotProof) {
	n.handleSnapshotProofFromPeer(proof, "")
}

func (n *Node) handleSnapshotProofFromPeer(proof SnapshotProof, peerID string) {
	n.handleSnapshotProofFromPeerWithPolicy(proof, peerID, true)
}

func (n *Node) handleSnapshotProofFromGossip(proof SnapshotProof, peerID string) {
	n.handleSnapshotProofFromPeerWithPolicy(proof, peerID, false)
}

func (n *Node) handleSnapshotProofFromPeerWithPolicy(proof SnapshotProof, peerID string, penalizeInvalidPeer bool) {
	if n == nil {
		return
	}
	normalizeSnapshotProof(&proof)
	votes, ok := n.recordSnapshotProof(proof)
	if !ok {
		if penalizeInvalidPeer && strings.TrimSpace(peerID) != "" {
			n.recordSyncPeerInvalidProof(peerID)
		}
		return
	}
	if validatorID := normalizeValidatorID(proof.Validator); validatorID != "" {
		if peerID = strings.TrimSpace(peerID); peerID != "" {
			if candidate := snapshotProofCandidateFromProof(&proof); candidate != nil {
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

func decodeSnapshotProofGossipPayload(data []byte) (SnapshotProof, bool) {
	var wrapped Message
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Type != "" {
		if wrapped.Type != MsgSnapshotProof {
			return SnapshotProof{}, false
		}
		data = wrapped.Data
	}
	var proof SnapshotProof
	if err := json.Unmarshal(data, &proof); err != nil {
		return SnapshotProof{}, false
	}
	return proof, true
}

func (n *Node) handleSnapshotProofGossip(sub *pubsub.Subscription) {
	if n == nil || sub == nil {
		return
	}
	ctx := n.RootContext()
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		peerID := msg.ReceivedFrom.String()
		if proof, ok := decodeSnapshotProofGossipPayload(msg.Data); ok {
			n.handleSnapshotProofFromGossip(proof, peerID)
			continue
		}
		if n.handleConsensusEnvelopeFromPeer(msg.Data, peerID) {
			continue
		}
	}
}

func (n *Node) listenSnapshotProofs(ctx context.Context) {
	if n == nil || n.SnapshotProofSub == nil {
		return
	}
	for {
		msg, err := n.SnapshotProofSub.Next(ctx)
		if err != nil {
			log.Println("snapshot proof listener stopped:", err)
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		peerID := msg.ReceivedFrom.String()
		if proof, ok := decodeSnapshotProofGossipPayload(msg.Data); ok {
			n.handleSnapshotProofFromGossip(proof, peerID)
			continue
		}
		if n.handleConsensusEnvelopeFromPeer(msg.Data, peerID) {
			continue
		}
	}
}

func (n *Node) publishSnapshotProof(proof SnapshotProof) bool {
	if n == nil {
		return false
	}
	normalizeSnapshotProof(&proof)
	if proof.Height == 0 || proof.Validator == "" || proof.SignatureHex == "" {
		return false
	}
	candidate := snapshotProofCandidateFromProof(&proof)
	if candidate == nil {
		return false
	}
	broadcastKey := fmt.Sprintf("%s|%s|%s", proof.Validator, strictSnapshotMetaCandidateKey(candidate), proof.SignatureHex)
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
	raw, err := json.Marshal(proof)
	if err != nil {
		return false
	}
	wrapped, err := json.Marshal(Message{Type: MsgSnapshotProof, Data: raw})
	if err != nil {
		return false
	}
	if n.SnapshotProofTopic != nil {
		ctx, cancel := context.WithTimeout(n.RootContext(), validatorHeartbeatPublishTimeout())
		err := n.SnapshotProofTopic.Publish(ctx, wrapped)
		cancel()
		if err == nil {
			return true
		}
	}
	if n.PubSub != nil {
		if err := n.PubSub.Publish(TopicSnapshotProof, wrapped); err == nil {
			return true
		}
	}
	return false
}

func (n *Node) markValidatorSnapshotPublishResult(snapshot *StateSnapshot, err error) {
	if n == nil {
		return
	}
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

func (n *Node) publishedValidatorSnapshotForSyncRequest(targetHeight uint64) *StateSnapshot {
	if n == nil {
		return nil
	}
	n.snapshotProofMu.RLock()
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

func validatorSnapshotAdaptiveReason(localHeight uint64, peerHeight uint64) (string, uint64, bool) {
	if localHeight == 0 || peerHeight >= localHeight {
		return "", 0, false
	}
	lag := localHeight - peerHeight
	if lag > 2 {
		return "sync_lag", lag, true
	}
	if peerHeight <= 1 {
		return "new_node_join", lag, true
	}
	if threshold := syncSnapshotPublishNewNodeThresholdBlocks(); threshold > 0 && lag >= threshold {
		return "new_node_join", lag, true
	}
	if threshold := syncSnapshotPublishLagThresholdBlocks(); threshold > 0 && lag >= threshold {
		return "lagging_peer", lag, true
	}
	return "", 0, false
}

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

func (n *Node) validatorSnapshotAdaptivePublishDecision(tip uint64) (bool, string) {
	if n == nil || tip == 0 || normalizeNodeRole(n.Role) != "validator" {
		return false, ""
	}
	bestReason := ""
	bestLag := uint64(0)
	consider := func(height uint64) {
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
	for _, height := range n.peerAckHeight {
		consider(height)
	}
	n.peerStateMu.Unlock()

	now := time.Now()
	maxAge := validatorLivenessHeartbeatTTL() + validatorLivenessGrace()
	selfID := normalizeValidatorID(n.ID)
	n.validatorMu.RLock()
	for id, st := range n.validatorStatus {
		if st == nil || normalizeValidatorID(id) == selfID {
			continue
		}
		if !st.LastSeen.IsZero() {
			age := now.Sub(st.LastSeen)
			if age < 0 {
				age = 0
			}
			if age > maxAge {
				continue
			}
		}
		height := st.FinalizedHeight
		if height == 0 {
			height = st.ReportedHeight
		}
		consider(height)
	}
	n.validatorMu.RUnlock()

	return bestReason != "", bestReason
}

func isAutomaticValidatorSnapshotPublishReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	return strings.HasPrefix(reason, "validator_") ||
		strings.HasPrefix(reason, "adaptive_") ||
		strings.HasPrefix(reason, "snapshot_proof_signer")
}

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

func shouldPublishAutomaticValidatorSnapshotAtHeight(height uint64) bool {
	if height == 0 {
		return false
	}
	if validatorSnapshotAutoPublishIntervalBlocks <= 1 || height <= 2 {
		return true
	}
	return height%validatorSnapshotAutoPublishIntervalBlocks == 0
}

func (n *Node) maybeTriggerAdaptiveValidatorSnapshotPublish(trigger string) {
	if n == nil || normalizeNodeRole(n.Role) != "validator" || n.Blockchain == nil {
		return
	}
	tip := n.Blockchain.Height()
	force, reason := n.validatorSnapshotAdaptivePublishDecision(tip)
	if !force || strings.TrimSpace(reason) == "" {
		return
	}
	if strings.TrimSpace(reason) == "new_node_join" {
		n.markSnapshotPublishBoost(reason)
	}
	publishedHeight, _, publishedAt, publishErr := n.validatorSnapshotPublicationState()
	if publishedHeight >= tip && strings.TrimSpace(publishErr) == "" && !publishedAt.IsZero() &&
		time.Since(publishedAt) < syncSnapshotPublishReannounceCooldown() {
		return
	}
	go func(height uint64, publishReason string) {
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
				normalizeValidatorID(n.ID),
				ShortHash(snapshot.SnapshotHash),
			)
		}
	}(tip, "adaptive_"+strings.TrimSpace(trigger)+"_"+strings.TrimSpace(reason))
}

func (n *Node) validatorSnapshotOfferTargets(height uint64) []string {
	if n == nil {
		return nil
	}
	if height == 0 && n.Blockchain != nil {
		height = n.Blockchain.Height()
	}
	targets := make(map[string]struct{})
	addAll := func(ids []string) {
		for _, raw := range canonicalValidatorIDs(ids) {
			id := normalizeValidatorID(raw)
			if id == "" || id == normalizeValidatorID(n.ID) {
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
		for id := range n.validatorStatus {
			nid := normalizeValidatorID(id)
			if nid == "" || nid == normalizeValidatorID(n.ID) {
				continue
			}
			targets[nid] = struct{}{}
		}
		n.validatorMu.RUnlock()
	}
	if len(targets) == 0 {
		return nil
	}
	out := make([]string, 0, len(targets))
	for id := range targets {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (n *Node) publishRequiredValidatorSnapshot(reason string, force bool) (*StateSnapshot, error) {
	if n == nil {
		return nil, fmt.Errorf("node unavailable")
	}
	if normalizeNodeRole(n.Role) != "validator" {
		return nil, nil
	}
	if n.Blockchain == nil || n.Blockchain.Height() == 0 {
		err := fmt.Errorf("committed tip unavailable")
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	tip := n.Blockchain.Height()
	if isAutomaticValidatorSnapshotPublishReason(reason) &&
		!shouldPublishAutomaticValidatorSnapshotAtHeight(tip) &&
		!(force && isAdaptiveValidatorSnapshotPublishReason(reason)) {
		if DebugConsensus || DebugSync {
			key := fmt.Sprintf("snapshot_publish_interval_deferred:%s:%d", normalizeValidatorID(n.ID), tip)
			if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
				fmt.Printf("[SNAPSHOT-PUBLISH] deferred height=%d reason=auto_publish_interval_%d\n",
					tip,
					validatorSnapshotAutoPublishIntervalBlocks,
				)
			}
		}
		return nil, nil
	}
	if !force && !shouldAutoCreateSnapshotAtHeight(tip) {
		if strings.Contains(reason, "snapshot_proof_signer") {
			force = true
		} else {
			if DebugConsensus || DebugSync {
				key := fmt.Sprintf("snapshot_publish_deferred:%s:%d", normalizeValidatorID(n.ID), tip)
				if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
					fmt.Printf("[SNAPSHOT-PUBLISH] deferred height=%d reason=checkpoint_interval\n", tip)
				}
			}
			return nil, nil
		}
	}
	if len(n.ValidatorKey.PrivateKey) != ed25519.PrivateKeySize {
		err := fmt.Errorf("validator key unavailable for snapshot publication")
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	if len(n.ValidatorKey.PublicKey) == ed25519.PublicKeySize {
		selfID := normalizeValidatorID(n.ID)
		if selfID == "" {
			selfID = normalizeValidatorID(n.ValidatorKey.ID)
		}
		if selfID != "" {
			validatorPubKeysMu.Lock()
			ValidatorPubKeys[selfID] = append(ed25519.PublicKey(nil), n.ValidatorKey.PublicKey...)
			validatorPubKeysMu.Unlock()
		}
	}
	snapshot, _, source, err := n.createCommittedTipSnapshot(reason, false)
	if err != nil {
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	if snapshot == nil {
		err := fmt.Errorf("snapshot unavailable after materialization")
		n.markValidatorSnapshotPublishResult(nil, err)
		return nil, err
	}
	populateSnapshotDerivedFields(snapshot)
	proof := snapshotProofFromSnapshot(n.ID, snapshot)
	if proof.Height == 0 || proof.SignatureHex == "" {
		err := fmt.Errorf("snapshot proof unavailable height=%d source=%s", snapshot.Height, strings.TrimSpace(source))
		n.markValidatorSnapshotPublishResult(snapshot, err)
		return nil, err
	}
	if !n.publishSnapshotProof(proof) {
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
	for _, validatorID := range n.validatorSnapshotOfferTargets(snapshot.Height) {
		n.maybeOfferSnapshotToValidator(validatorID, snapshot.Height)
	}
	n.markValidatorSnapshotPublishResult(snapshot, nil)
	return snapshot, nil
}

func (n *Node) startSnapshotProofSigner(ctx context.Context) {
	if n == nil {
		return
	}
	if normalizeNodeRole(n.Role) == "validator" {
		key := fmt.Sprintf("snapshot_publish_required_start:%s", normalizeValidatorID(n.ID))
		if n.shouldLogLivenessReason(key, time.Minute) {
			log.Printf("[SNAPSHOT-PUBLISH-REQUIRED] validator=%s policy=strict_snapshot_only", normalizeValidatorID(n.ID))
		}
	}
	publish := func() {
		if n == nil || normalizeNodeRole(n.Role) != "validator" {
			return
		}
		tip := uint64(0)
		if n.Blockchain != nil {
			tip = n.Blockchain.Height()
		}
		force, forceReason := n.validatorSnapshotAdaptivePublishDecision(tip)
		reason := "snapshot_proof_signer"
		if force && strings.TrimSpace(forceReason) != "" {
			reason = "adaptive_snapshot_proof_signer_" + strings.TrimSpace(forceReason)
			if strings.TrimSpace(forceReason) == "new_node_join" {
				n.markSnapshotPublishBoost(forceReason)
			}
		}
		snapshot, err := n.publishRequiredValidatorSnapshot(reason, force)
		if err != nil {
			key := fmt.Sprintf("snapshot_publish_required:%s:%d", normalizeValidatorID(n.ID), tip)
			if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
				log.Printf("[SNAPSHOT-PUBLISH-REQUIRED] validator=%s height=%d error=%v", normalizeValidatorID(n.ID), tip, err)
			}
			return
		}
		if snapshot != nil && (DebugConsensus || DebugSync) {
			proof := snapshotProofFromSnapshot(n.ID, snapshot)
			fmt.Printf("[SNAPSHOT-PUBLISH] height=%d checkpoint=%d validator=%s hash=%s reason=%s\n",
				snapshot.Height,
				proof.CheckpointHeight,
				normalizeValidatorID(n.ID),
				ShortHash(snapshot.SnapshotHash),
				strings.TrimSpace(reason),
			)
		}
	}
	publish()
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

func (n *Node) snapshotProofObservationsFromGroupLocked(candidateKey string, group map[string]SnapshotProof, validatorProviders map[string]string, targetHeight uint64, minHeight uint64) ([]strictSnapshotMetaObservation, []SnapshotVote) {
	if len(group) == 0 {
		return nil, nil
	}
	validatorIDs := make([]string, 0, len(group))
	for validatorID := range group {
		validatorIDs = append(validatorIDs, validatorID)
	}
	sort.Strings(validatorIDs)
	observations := make([]strictSnapshotMetaObservation, 0, len(validatorIDs))
	votes := make([]SnapshotVote, 0, len(validatorIDs))
	for _, validatorID := range validatorIDs {
		proof := group[validatorID]
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
		provider := ""
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
	if cache, ok := n.cachedSnapshotAnchorLocked(targetHeight, minHeight); ok && cache.CandidateKey != "" && cache.Votes >= required {
		if group := n.snapshotProofs[cache.CandidateKey]; len(group) > 0 {
			observations, votes := n.snapshotProofObservationsFromGroupLocked(cache.CandidateKey, group, validatorProviders, targetHeight, minHeight)
			if len(observations) >= required {
				return observations, votes
			}
		}
	}
	allObservations := make([]strictSnapshotMetaObservation, 0)
	allVotes := make([]SnapshotVote, 0)
	keys := make([]string, 0, len(n.snapshotProofs))
	for key := range n.snapshotProofs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		observations, votes := n.snapshotProofObservationsFromGroupLocked(key, n.snapshotProofs[key], validatorProviders, targetHeight, minHeight)
		allObservations = append(allObservations, observations...)
		allVotes = append(allVotes, votes...)
	}
	return allObservations, allVotes
}

func (n *Node) snapshotProofStats() (int, int, int) {
	if n == nil {
		return 0, 0, 0
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	groups := len(n.snapshotProofs)
	proofs := 0
	bestVotes := 0
	for _, group := range n.snapshotProofs {
		size := len(group)
		proofs += size
		if size > bestVotes {
			bestVotes = size
		}
	}
	return groups, proofs, bestVotes
}

func (n *Node) snapshotExportBaseDir() string {
	if n == nil {
		return ""
	}
	base := strings.TrimSpace(n.DataDir)
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, "snapshots")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func (n *Node) snapshotExportDirForHeight(height uint64) string {
	base := n.snapshotExportBaseDir()
	if base == "" || height == 0 {
		return ""
	}
	return filepath.Join(base, fmt.Sprintf("%020d", height))
}

func (n *Node) exportSnapshotArtifacts(snapshot *StateSnapshot) error {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return nil
	}
	base := n.snapshotExportBaseDir()
	if base == "" {
		return nil
	}
	manifest, payload, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		return err
	}
	dir := n.snapshotExportDirForHeight(snapshot.Height)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	metaPath := filepath.Join(dir, "meta.json")
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(metaPath, rawManifest, 0o600); err != nil {
		return err
	}
	chunkSize := manifest.ChunkSize
	if chunkSize == 0 {
		chunkSize = syncSnapshotChunkSizeBytes()
	}
	for idx := uint64(0); idx < manifest.ChunkCount; idx++ {
		start := idx * chunkSize
		end := start + chunkSize
		if end > uint64(len(payload)) {
			end = uint64(len(payload))
		}
		chunkPath := filepath.Join(dir, fmt.Sprintf("chunk_%04d", idx))
		if err := os.WriteFile(chunkPath, payload[start:end], 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) exportSnapshotArtifactsBestEffort(snapshot *StateSnapshot, source string) {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return
	}
	if err := n.exportSnapshotArtifacts(snapshot); err != nil {
		key := fmt.Sprintf("snapshot_export:%d:%s", snapshot.Height, strings.TrimSpace(source))
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-EXPORT] height=%d source=%s err=%v", snapshot.Height, strings.TrimSpace(source), err)
		}
	}
}
