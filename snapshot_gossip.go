package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

type SnapshotValidatorPeer struct {
	PeerID      peer.ID
	ValidatorID string
}

type SnapshotProofQuorum struct {
	Required      int
	Peers         []SnapshotValidatorPeer
	Observations  []strictSnapshotMetaObservation
	Votes         []SnapshotVote
	Candidate     *strictSnapshotMetaCandidate
	BestCandidate *strictSnapshotMetaCandidate
	Proofs        map[string]SnapshotProof
}

type SnapshotProofCollector struct {
	Node             *Node
	TargetHeight     uint64
	MinHeight        uint64
	CheckpointHeight uint64
	StrictCoreQuorum bool
	Required         int
	Peers            []SnapshotValidatorPeer
}

func RequiredSnapshotProofs(totalValidators int) int {
	if totalValidators <= 0 {
		return 1
	}
	required := execQuorumRequired(totalValidators)
	if required <= 0 {
		required = 1
	}
	return required
}

func (n *Node) cachedSnapshotAnchor(targetHeight uint64, minHeight uint64) (SnapshotAnchorCache, bool) {
	if n == nil {
		return SnapshotAnchorCache{}, false
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	return n.cachedSnapshotAnchorLocked(targetHeight, minHeight)
}

func (n *Node) snapshotProofsForCandidate(candidate *strictSnapshotMetaCandidate) map[string]SnapshotProof {
	if n == nil || candidate == nil {
		return nil
	}
	key := strictSnapshotMetaCandidateKey(candidate)
	if key == "" {
		return nil
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	group := n.snapshotProofs[key]
	if len(group) == 0 {
		return nil
	}
	out := make(map[string]SnapshotProof, len(group))
	for validatorID, proof := range group {
		out[validatorID] = proof
	}
	return out
}

func (n *Node) SelectSnapshotPeers(height uint64, strictCoreQuorum bool) []SnapshotValidatorPeer {
	if n == nil || n.Host == nil || height == 0 {
		return nil
	}
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		return nil
	}

	fallbackHeights := make([]uint64, 0, 6)
	seenHeights := make(map[uint64]struct{}, 6)
	addFallbackHeight := func(h uint64) {
		if h == 0 {
			return
		}
		if _, exists := seenHeights[h]; exists {
			return
		}
		seenHeights[h] = struct{}{}
		fallbackHeights = append(fallbackHeights, h)
	}
	addFallbackHeight(height)
	if height > 1 {
		addFallbackHeight(height - 1)
	}
	addFallbackHeight(height + 1)
	if n.Blockchain != nil {
		localHeight := n.Blockchain.Height()
		if localHeight > 0 {
			addFallbackHeight(localHeight + 1)
			addFallbackHeight(localHeight)
			if localHeight > 1 {
				addFallbackHeight(localHeight - 1)
			}
		}
	}

	authorityIDs := n.validatorAuthorityIDsForQuorum(height)
	authoritySet := make(map[string]struct{}, len(authorityIDs))
	for _, id := range authorityIDs {
		norm := normalizeValidatorID(id)
		if norm == "" {
			continue
		}
		authoritySet[norm] = struct{}{}
	}
	if len(authoritySet) == 0 {
		for _, fallbackHeight := range fallbackHeights {
			if fallbackHeight == height {
				continue
			}
			for _, id := range n.validatorAuthorityIDsForQuorum(fallbackHeight) {
				norm := normalizeValidatorID(id)
				if norm == "" {
					continue
				}
				authoritySet[norm] = struct{}{}
			}
			if len(authoritySet) > 0 {
				break
			}
		}
	}
	enforceAuthoritySet := len(authoritySet) > 0
	allowMappedFallback := strictCoreQuorum && !enforceAuthoritySet

	out := make([]SnapshotValidatorPeer, 0, len(peers))
	seen := make(map[string]struct{}, len(peers))
	for _, pid := range peers {
		if n.isPeerQuarantined(pid.String()) {
			continue
		}
		n.peerStateMu.Lock()
		vid := normalizeValidatorID(n.peerToValidator[pid.String()])
		n.peerStateMu.Unlock()
		if vid == "" {
			continue
		}
		if _, ok := seen[vid]; ok {
			continue
		}
		allowed := false
		if enforceAuthoritySet {
			_, allowed = authoritySet[vid]
		} else {
			allowed = n.isValidatorInSetForHeight(vid, height)
			if !allowed && height > 1 {
				allowed = n.isValidatorInSetForHeight(vid, height-1)
			}
			if !allowed {
				allowed = n.isValidatorInSetForHeight(vid, height+1)
			}
			if !allowed {
				for _, fallbackHeight := range fallbackHeights {
					if fallbackHeight == height || fallbackHeight == height+1 || (height > 1 && fallbackHeight == height-1) {
						continue
					}
					if n.isValidatorInSetForHeight(vid, fallbackHeight) {
						allowed = true
						break
					}
				}
			}
			if !allowed && allowMappedFallback {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		seen[vid] = struct{}{}
		out = append(out, SnapshotValidatorPeer{PeerID: pid, ValidatorID: vid})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := n.syncPeerReputationValue(out[i].PeerID.String())
		rj := n.syncPeerReputationValue(out[j].PeerID.String())
		if ri != rj {
			return ri > rj
		}
		si := n.syncPeerScoreValue(out[i].PeerID.String())
		sj := n.syncPeerScoreValue(out[j].PeerID.String())
		if si == sj {
			if out[i].ValidatorID == out[j].ValidatorID {
				return out[i].PeerID.String() < out[j].PeerID.String()
			}
			return out[i].ValidatorID < out[j].ValidatorID
		}
		return si > sj
	})
	return out
}

func (c *SnapshotProofCollector) Collect() (*SnapshotProofQuorum, error) {
	if c == nil || c.Node == nil {
		return nil, fmt.Errorf("snapshot proof collector unavailable")
	}
	if c.TargetHeight == 0 {
		return nil, fmt.Errorf("snapshot proof collector target unavailable")
	}
	peers := c.Peers
	if len(peers) == 0 {
		peers = c.Node.SelectSnapshotPeers(c.TargetHeight, c.StrictCoreQuorum)
	}
	validatorProviders := make(map[string]string, len(peers))
	for _, peerInfo := range peers {
		validatorID := normalizeValidatorID(peerInfo.ValidatorID)
		if validatorID == "" {
			continue
		}
		validatorProviders[validatorID] = strings.TrimSpace(peerInfo.PeerID.String())
	}
	required := c.Required
	if required <= 0 {
		required = RequiredSnapshotProofs(len(validatorProviders))
	}
	observations, votes := c.Node.cachedSnapshotProofObservations(c.TargetHeight, c.MinHeight, validatorProviders, required)
	for _, vote := range votes {
		_, _, _ = c.Node.updateSnapshotSessionVote(vote)
	}
	quorumCandidate, bestCandidate := selectStrictSnapshotMetaCandidate(observations, required)
	proofs := c.Node.snapshotProofsForCandidate(quorumCandidate)
	if len(proofs) == 0 {
		proofs = c.Node.snapshotProofsForCandidate(bestCandidate)
	}
	return &SnapshotProofQuorum{
		Required:      required,
		Peers:         peers,
		Observations:  observations,
		Votes:         votes,
		Candidate:     quorumCandidate,
		BestCandidate: bestCandidate,
		Proofs:        proofs,
	}, nil
}

func snapshotMetaGossipKey(height uint64, provider string, snapshotHash string) string {
	return fmt.Sprintf("%020d|%s|%s", height, strings.TrimSpace(provider), strings.ToLower(strings.TrimSpace(snapshotHash)))
}

func snapshotChunkGossipKey(height uint64, provider string, snapshotHash string) string {
	return fmt.Sprintf("%020d|%s|%s", height, strings.TrimSpace(provider), strings.ToLower(strings.TrimSpace(snapshotHash)))
}

func (n *Node) recordSnapshotMetaGossip(msg SnapshotMetaGossip) {
	if n == nil || msg.Height == 0 {
		return
	}
	key := snapshotMetaGossipKey(msg.Height, msg.From, msg.Meta.SnapshotHash)
	n.snapshotGossipMu.Lock()
	defer n.snapshotGossipMu.Unlock()
	if n.snapshotMetaGossipCache == nil {
		n.snapshotMetaGossipCache = make(map[string]SnapshotMetaGossip)
	}
	n.snapshotMetaGossipCache[key] = msg
}

func (n *Node) recordSnapshotChunkGossip(msg SnapshotChunkGossip) {
	if n == nil || msg.Height == 0 || strings.TrimSpace(msg.SnapshotHash) == "" {
		return
	}
	key := snapshotChunkGossipKey(msg.Height, msg.From, msg.SnapshotHash)
	n.snapshotGossipMu.Lock()
	defer n.snapshotGossipMu.Unlock()
	if n.snapshotChunkGossipCache == nil {
		n.snapshotChunkGossipCache = make(map[string]SnapshotChunkGossip)
	}
	n.snapshotChunkGossipCache[key] = msg
}

func (n *Node) cachedSnapshotMetaAvailabilities(targetHeight uint64, minHeight uint64, validatorProviders map[string]string) []strictSnapshotMetaAvailability {
	if n == nil || len(validatorProviders) == 0 {
		return nil
	}
	n.snapshotGossipMu.Lock()
	cached := make([]SnapshotMetaGossip, 0, len(n.snapshotMetaGossipCache))
	for _, msg := range n.snapshotMetaGossipCache {
		cached = append(cached, msg)
	}
	n.snapshotGossipMu.Unlock()
	if len(cached) == 0 {
		return nil
	}
	bestByValidator := make(map[string]strictSnapshotMetaAvailability, len(validatorProviders))
	for _, msg := range cached {
		validatorID := normalizeValidatorID(msg.From)
		if validatorID == "" {
			continue
		}
		provider := strings.TrimSpace(validatorProviders[validatorID])
		if provider == "" {
			continue
		}
		meta := msg.Meta
		if !meta.Available {
			continue
		}
		observation, _ := n.strictSnapshotMetaObservationForTarget(&meta, provider, validatorID, targetHeight, minHeight, false)
		if observation == nil || observation.Candidate == nil {
			continue
		}
		availability := strictSnapshotMetaAvailability{
			Provider:    provider,
			ValidatorID: validatorID,
			Meta:        &meta,
			Candidate:   observation.Candidate,
		}
		if existing, ok := bestByValidator[validatorID]; ok {
			if !betterStrictSnapshotCandidate(availability.Candidate, existing.Candidate) {
				continue
			}
		}
		bestByValidator[validatorID] = availability
	}
	if len(bestByValidator) == 0 {
		return nil
	}
	out := make([]strictSnapshotMetaAvailability, 0, len(bestByValidator))
	for _, availability := range bestByValidator {
		out = append(out, availability)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Candidate != nil && out[j].Candidate != nil {
			if out[i].Candidate.Height != out[j].Candidate.Height {
				return out[i].Candidate.Height > out[j].Candidate.Height
			}
		}
		if out[i].ValidatorID != out[j].ValidatorID {
			return out[i].ValidatorID < out[j].ValidatorID
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func (n *Node) snapshotChunkGossipProviderScore(height uint64, snapshotHash string, provider string) int {
	if n == nil || height == 0 || strings.TrimSpace(snapshotHash) == "" || strings.TrimSpace(provider) == "" {
		return 0
	}
	hash := strings.ToLower(strings.TrimSpace(snapshotHash))
	provider = strings.TrimSpace(provider)
	score := 0
	n.snapshotGossipMu.Lock()
	for _, msg := range n.snapshotChunkGossipCache {
		if msg.Height != height {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(msg.SnapshotHash), hash) {
			continue
		}
		if strings.TrimSpace(msg.From) != provider {
			continue
		}
		score++
	}
	n.snapshotGossipMu.Unlock()
	return score
}

func (n *Node) handleSnapshotMetaGossipMessage(msg SnapshotMetaGossip) {
	if n == nil {
		return
	}
	if msg.Meta.Available {
		n.recordSnapshotMetaGossip(msg)
		provider := normalizeValidatorID(msg.From)
		if provider == "" {
			provider = strings.TrimSpace(msg.From)
		}
		chunkCount := msg.Meta.TotalChunks
		if chunkCount == 0 && msg.Manifest != nil {
			chunkCount = msg.Manifest.ChunkCount
		}
		n.updateSnapshotCatalogMeta(msg.Height, strings.TrimSpace(msg.Meta.StateRoot), chunkCount, []string{provider})
		n.updateSnapshotCatalogProviders(msg.Height, []string{provider})
	}
}

func (n *Node) handleSnapshotChunkGossipMessage(msg SnapshotChunkGossip) {
	if n == nil {
		return
	}
	n.recordSnapshotChunkGossip(msg)
	provider := normalizeValidatorID(msg.From)
	if provider == "" {
		provider = strings.TrimSpace(msg.From)
	}
	n.updateSnapshotCatalogMeta(msg.Height, "", msg.ChunkCount, []string{provider})
	n.updateSnapshotCatalogProviders(msg.Height, []string{provider})
}

func (n *Node) handleSnapshotMetaGossip(sub *pubsub.Subscription) {
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
		if n.handleConsensusEnvelope(msg.Data) {
			continue
		}
		var payload SnapshotMetaGossip
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			continue
		}
		n.handleSnapshotMetaGossipMessage(payload)
	}
}

func (n *Node) handleSnapshotChunkGossip(sub *pubsub.Subscription) {
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
		if n.handleConsensusEnvelope(msg.Data) {
			continue
		}
		var payload SnapshotChunkGossip
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			continue
		}
		n.handleSnapshotChunkGossipMessage(payload)
	}
}

func (n *Node) publishSnapshotMetaGossip(snapshot *StateSnapshot) bool {
	return n.publishSnapshotMetaGossipInternal(snapshot, false)
}

func (n *Node) publishSnapshotMetaGossipForce(snapshot *StateSnapshot) bool {
	return n.publishSnapshotMetaGossipInternal(snapshot, true)
}

func (n *Node) snapshotPublishCooldown() time.Duration {
	if n == nil {
		return syncSnapshotPublishReannounceCooldown()
	}
	n.snapshotGossipMu.RLock()
	until := n.snapshotBoostUntil
	n.snapshotGossipMu.RUnlock()
	if !until.IsZero() && time.Now().Before(until) {
		return 4 * time.Second
	}
	return syncSnapshotPublishReannounceCooldown()
}

func (n *Node) markSnapshotPublishBoost(reason string) {
	if n == nil {
		return
	}
	if strings.TrimSpace(reason) != "new_node_join" {
		return
	}
	until := time.Now().Add(15 * time.Second)
	n.snapshotGossipMu.Lock()
	if until.After(n.snapshotBoostUntil) {
		n.snapshotBoostUntil = until
	}
	n.snapshotGossipMu.Unlock()
}

func (n *Node) publishSnapshotMetaGossipInternal(snapshot *StateSnapshot, allowRepeat bool) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	manifest, _, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil || manifest == nil {
		return false
	}
	msg := SnapshotMetaGossip{
		From:   normalizeValidatorID(n.ID),
		Height: snapshot.Height,
		Meta: SnapshotMetaResponse{
			Height:                snapshot.Height,
			SnapshotHash:          strings.TrimSpace(snapshot.SnapshotHash),
			StateRoot:             strings.TrimSpace(snapshot.StateRoot),
			StateMerkleRoot:       strings.TrimSpace(snapshot.StateMerkleRoot),
			ValidatorSetHash:      strings.TrimSpace(snapshotValidatorSetHash(snapshot)),
			ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
			FinalizedHeight:       snapshot.FinalizedHeight,
			FinalizedHash:         strings.TrimSpace(snapshot.FinalizedHash),
			EpochAnchorHash:       strings.TrimSpace(snapshot.EpochAnchorHash),
			FinalityRoot:          strings.TrimSpace(snapshot.FinalityRoot),
			ChunkSize:             manifest.ChunkSize,
			TotalChunks:           manifest.ChunkCount,
			ChunkHashes:           append([]string{}, manifest.ChunkHashes...),
			CheckpointProof:       copyStringMap(snapshot.CheckpointProof),
			Manifest:              manifest,
			Available:             true,
		},
		Manifest:  manifest,
		Timestamp: time.Now().Unix(),
	}
	n.recordSnapshotMetaGossip(msg)
	raw, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	wrapped, err := json.Marshal(Message{Type: MsgSnapshotMeta, Data: raw})
	if err != nil {
		return false
	}
	key := snapshotMetaGossipKey(msg.Height, msg.From, msg.Meta.SnapshotHash)
	now := time.Now()
	n.snapshotGossipMu.Lock()
	if n.snapshotMetaLastPublished == key {
		if !allowRepeat || now.Sub(n.snapshotMetaLastPublishedAt) < n.snapshotPublishCooldown() {
			n.snapshotGossipMu.Unlock()
			return true
		}
	}
	n.snapshotMetaLastPublished = key
	n.snapshotMetaLastPublishedAt = now
	n.snapshotGossipMu.Unlock()
	if n.SnapshotMetaTopic != nil {
		ctx, cancel := context.WithTimeout(n.RootContext(), validatorHeartbeatPublishTimeout())
		err := n.SnapshotMetaTopic.Publish(ctx, wrapped)
		cancel()
		if err == nil {
			return true
		}
	}
	if n.PubSub != nil {
		if err := n.PubSub.Publish(TopicSnapshotMeta, wrapped); err == nil {
			return true
		}
	}
	return false
}

func (n *Node) publishSnapshotChunkGossip(snapshot *StateSnapshot) bool {
	return n.publishSnapshotChunkGossipInternal(snapshot, false)
}

func (n *Node) publishSnapshotChunkGossipForce(snapshot *StateSnapshot) bool {
	return n.publishSnapshotChunkGossipInternal(snapshot, true)
}

func (n *Node) publishSnapshotChunkGossipInternal(snapshot *StateSnapshot, allowRepeat bool) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	manifest, _, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil || manifest == nil {
		return false
	}
	msg := SnapshotChunkGossip{
		From:         normalizeValidatorID(n.ID),
		Height:       snapshot.Height,
		SnapshotHash: strings.TrimSpace(snapshot.SnapshotHash),
		ChunkSize:    manifest.ChunkSize,
		ChunkCount:   manifest.ChunkCount,
		Timestamp:    time.Now().Unix(),
	}
	n.recordSnapshotChunkGossip(msg)
	raw, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	wrapped, err := json.Marshal(Message{Type: MsgSnapshotChunk, Data: raw})
	if err != nil {
		return false
	}
	key := snapshotChunkGossipKey(msg.Height, msg.From, msg.SnapshotHash)
	now := time.Now()
	n.snapshotGossipMu.Lock()
	if n.snapshotChunkLastPublished == key {
		if !allowRepeat || now.Sub(n.snapshotChunkLastPublishedAt) < n.snapshotPublishCooldown() {
			n.snapshotGossipMu.Unlock()
			return true
		}
	}
	n.snapshotChunkLastPublished = key
	n.snapshotChunkLastPublishedAt = now
	n.snapshotGossipMu.Unlock()
	if n.SnapshotChunkTopic != nil {
		ctx, cancel := context.WithTimeout(n.RootContext(), validatorHeartbeatPublishTimeout())
		err := n.SnapshotChunkTopic.Publish(ctx, wrapped)
		cancel()
		if err == nil {
			return true
		}
	}
	if n.PubSub != nil {
		if err := n.PubSub.Publish(TopicSnapshotChunk, wrapped); err == nil {
			return true
		}
	}
	return false
}
