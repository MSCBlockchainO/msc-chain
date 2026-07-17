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
	// `PeerID` stores the value associated with this record.
	PeerID peer.ID
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string
}

type SnapshotProofQuorum struct {
	// `Required` stores the request data being processed.
	Required int
	// `Peers` stores the value associated with this record.
	Peers []SnapshotValidatorPeer
	// `Observations` stores the value associated with this record.
	Observations []strictSnapshotMetaObservation
	// `Votes` stores the value associated with this record.
	Votes []SnapshotVote
	// `Candidate` stores the value associated with this record.
	Candidate *strictSnapshotMetaCandidate
	// `BestCandidate` stores the value associated with this record.
	BestCandidate *strictSnapshotMetaCandidate
	// `Proofs` stores the value associated with this record.
	Proofs map[string]SnapshotProof
}

type SnapshotProofCollector struct {
	// `Node` stores the value associated with this record.
	Node *Node
	// `TargetHeight` stores the value associated with this record.
	TargetHeight uint64
	// `MinHeight` stores the value associated with this record.
	MinHeight uint64
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64
	// `StrictCoreQuorum` stores the value associated with this record.
	StrictCoreQuorum bool
	// `Required` stores the request data being processed.
	Required int
	// `Peers` stores the value associated with this record.
	Peers []SnapshotValidatorPeer
}

const (
	maxSnapshotMetaGossipCacheEntries  = 64
	maxSnapshotChunkGossipCacheEntries = 256
	maxSnapshotMetaGossipCacheBytes    = 1 << 20
	snapshotGossipCacheTTL             = 30 * time.Minute
)

// RequiredSnapshotProofs implements the required snapshot proofs helper.
func RequiredSnapshotProofs(totalValidators int) int {
	if totalValidators <= 0 {
		return 1
	}
	// `required` stores the request data being processed.
	required := execQuorumRequired(totalValidators)
	if required <= 0 {
		required = 1
	}
	return required
}

// cachedSnapshotAnchor implements the cached snapshot anchor helper.
func (n *Node) cachedSnapshotAnchor(targetHeight uint64, minHeight uint64) (SnapshotAnchorCache, bool) {
	if n == nil {
		return SnapshotAnchorCache{}, false
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	return n.cachedSnapshotAnchorLocked(targetHeight, minHeight)
}

// snapshotProofsForCandidate implements the snapshot proofs for candidate helper.
func (n *Node) snapshotProofsForCandidate(candidate *strictSnapshotMetaCandidate) map[string]SnapshotProof {
	if n == nil || candidate == nil {
		return nil
	}
	// `key` stores the key used to access the related value.
	key := strictSnapshotMetaCandidateKey(candidate)
	if key == "" {
		return nil
	}
	n.snapshotProofMu.RLock()
	defer n.snapshotProofMu.RUnlock()
	// `group` stores the value produced by this operation.
	group := n.snapshotProofs[key]
	if len(group) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]SnapshotProof, len(group))
	// `validatorID` and `proof` track whether the related condition is satisfied.
	for validatorID, proof := range group {
		out[validatorID] = proof
	}
	return out
}

// SelectSnapshotPeers selects snapshot peers.
func (n *Node) SelectSnapshotPeers(height uint64, strictCoreQuorum bool) []SnapshotValidatorPeer {
	if n == nil || n.Host == nil || height == 0 {
		return nil
	}
	// `peers` stores the value produced by this operation.
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		return nil
	}

	// `fallbackHeights` stores the value produced by this operation.
	fallbackHeights := make([]uint64, 0, 6)
	// `seenHeights` stores the value produced by this operation.
	seenHeights := make(map[uint64]struct{}, 6)
	// `addFallbackHeight` stores the value produced by this operation.
	addFallbackHeight := func(h uint64) {
		if h == 0 {
			return
		}
		// `exists` stores whether the related condition is satisfied.
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
		// `localHeight` stores the value produced by this operation.
		localHeight := n.Blockchain.Height()
		if localHeight > 0 {
			addFallbackHeight(localHeight + 1)
			addFallbackHeight(localHeight)
			if localHeight > 1 {
				addFallbackHeight(localHeight - 1)
			}
		}
	}

	// `authorityIDs` stores the value produced by this operation.
	authorityIDs := n.validatorAuthorityIDsForQuorum(height)
	// `authoritySet` stores the value produced by this operation.
	authoritySet := make(map[string]struct{}, len(authorityIDs))
	// `id` tracks the current position in the related collection.
	for _, id := range authorityIDs {
		// `norm` stores the value produced by this operation.
		norm := normalizeValidatorID(id)
		if norm == "" {
			continue
		}
		authoritySet[norm] = struct{}{}
	}
	if len(authoritySet) == 0 {
		// `fallbackHeight` tracks the current values while iterating.
		for _, fallbackHeight := range fallbackHeights {
			if fallbackHeight == height {
				continue
			}
			// `id` tracks the current position in the related collection.
			for _, id := range n.validatorAuthorityIDsForQuorum(fallbackHeight) {
				// `norm` stores the value produced by this operation.
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
	// `enforceAuthoritySet` stores the value produced by this operation.
	enforceAuthoritySet := len(authoritySet) > 0
	// `allowMappedFallback` stores the value produced by this operation.
	allowMappedFallback := strictCoreQuorum && !enforceAuthoritySet

	// `out` stores the result produced by this operation.
	out := make([]SnapshotValidatorPeer, 0, len(peers))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(peers))
	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		if n.isPeerQuarantined(pid.String()) {
			continue
		}
		n.peerStateMu.Lock()
		// `vid` stores the value produced by this operation.
		vid := normalizeValidatorID(n.peerToValidator[pid.String()])
		n.peerStateMu.Unlock()
		if vid == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[vid]; ok {
			continue
		}
		// `allowed` stores whether the related condition is satisfied.
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
				// `fallbackHeight` tracks the current values while iterating.
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
		// `ri` stores the current position in the related collection.
		ri := n.syncPeerReputationValue(out[i].PeerID.String())
		// `rj` stores the current position in the related collection.
		rj := n.syncPeerReputationValue(out[j].PeerID.String())
		if ri != rj {
			return ri > rj
		}
		// `si` stores the current position in the related collection.
		si := n.syncPeerScoreValue(out[i].PeerID.String())
		// `sj` stores the current position in the related collection.
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

// Collect implements the collect helper.
func (c *SnapshotProofCollector) Collect() (*SnapshotProofQuorum, error) {
	if c == nil || c.Node == nil {
		return nil, fmt.Errorf("snapshot proof collector unavailable")
	}
	if c.TargetHeight == 0 {
		return nil, fmt.Errorf("snapshot proof collector target unavailable")
	}
	// `peers` stores the value produced by this operation.
	peers := c.Peers
	if len(peers) == 0 {
		peers = c.Node.SelectSnapshotPeers(c.TargetHeight, c.StrictCoreQuorum)
	}
	// `validatorProviders` stores whether the related condition is satisfied.
	validatorProviders := make(map[string]string, len(peers))
	// `peerInfo` tracks the current values while iterating.
	for _, peerInfo := range peers {
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := normalizeValidatorID(peerInfo.ValidatorID)
		if validatorID == "" {
			continue
		}
		validatorProviders[validatorID] = strings.TrimSpace(peerInfo.PeerID.String())
	}
	// `required` stores the request data being processed.
	required := c.Required
	if required <= 0 {
		required = RequiredSnapshotProofs(len(validatorProviders))
	}
	// `observations` and `votes` store the value produced by this operation.
	observations, votes := c.Node.cachedSnapshotProofObservations(c.TargetHeight, c.MinHeight, validatorProviders, required)
	// `vote` tracks the current values while iterating.
	for _, vote := range votes {
		_, _, _ = c.Node.updateSnapshotSessionVote(vote)
	}
	// `quorumCandidate` and `bestCandidate` store the value produced by this operation.
	quorumCandidate, bestCandidate := selectStrictSnapshotMetaCandidate(observations, required)
	// `proofs` stores the value produced by this operation.
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

// snapshotMetaGossipKey implements the snapshot meta gossip key helper.
func snapshotMetaGossipKey(height uint64, provider string, snapshotHash string) string {
	return fmt.Sprintf("%020d|%s|%s", height, strings.TrimSpace(provider), strings.ToLower(strings.TrimSpace(snapshotHash)))
}

// snapshotChunkGossipKey implements the snapshot chunk gossip key helper.
func snapshotChunkGossipKey(height uint64, provider string, snapshotHash string) string {
	return fmt.Sprintf("%020d|%s|%s", height, strings.TrimSpace(provider), strings.ToLower(strings.TrimSpace(snapshotHash)))
}

func pruneSnapshotMetaGossipCacheLocked(cache map[string]SnapshotMetaGossip, now time.Time) {
	cutoff := now.Add(-snapshotGossipCacheTTL).Unix()
	for key, msg := range cache {
		if msg.Timestamp > 0 && msg.Timestamp < cutoff {
			delete(cache, key)
		}
	}
}

func pruneSnapshotChunkGossipCacheLocked(cache map[string]SnapshotChunkGossip, now time.Time) {
	cutoff := now.Add(-snapshotGossipCacheTTL).Unix()
	for key, msg := range cache {
		if msg.Timestamp > 0 && msg.Timestamp < cutoff {
			delete(cache, key)
		}
	}
}

func evictOldestSnapshotMetaGossipLocked(cache map[string]SnapshotMetaGossip) {
	oldestKey := ""
	oldestTimestamp := int64(0)
	for key, msg := range cache {
		if oldestKey == "" || msg.Timestamp < oldestTimestamp ||
			(msg.Timestamp == oldestTimestamp && key < oldestKey) {
			oldestKey = key
			oldestTimestamp = msg.Timestamp
		}
	}
	if oldestKey != "" {
		delete(cache, oldestKey)
	}
}

func evictOldestSnapshotChunkGossipLocked(cache map[string]SnapshotChunkGossip) {
	oldestKey := ""
	oldestTimestamp := int64(0)
	for key, msg := range cache {
		if oldestKey == "" || msg.Timestamp < oldestTimestamp ||
			(msg.Timestamp == oldestTimestamp && key < oldestKey) {
			oldestKey = key
			oldestTimestamp = msg.Timestamp
		}
	}
	if oldestKey != "" {
		delete(cache, oldestKey)
	}
}

// recordSnapshotMetaGossip implements the record snapshot meta gossip helper.
func (n *Node) recordSnapshotMetaGossip(msg SnapshotMetaGossip) bool {
	if n == nil || msg.Height == 0 || !msg.Meta.Available || strings.TrimSpace(msg.From) == "" ||
		len(msg.From) > maxSnapshotManifestFieldBytes {
		return false
	}
	manifest := snapshotManifestFromMeta(&msg.Meta)
	if !snapshotManifestBasicValid(manifest, msg.Height, 0) || manifest.Height != msg.Height {
		return false
	}
	if msg.Manifest != nil && !snapshotManifestMatches(manifest, msg.Manifest) {
		return false
	}
	if raw, err := json.Marshal(msg); err != nil || len(raw) > maxSnapshotMetaGossipCacheBytes {
		return false
	}
	now := time.Now()
	msg.Timestamp = now.Unix()
	// `key` stores the key used to access the related value.
	key := snapshotMetaGossipKey(msg.Height, msg.From, msg.Meta.SnapshotHash)
	n.snapshotGossipMu.Lock()
	defer n.snapshotGossipMu.Unlock()
	if n.snapshotMetaGossipCache == nil {
		n.snapshotMetaGossipCache = make(map[string]SnapshotMetaGossip)
	}
	pruneSnapshotMetaGossipCacheLocked(n.snapshotMetaGossipCache, now)
	if _, exists := n.snapshotMetaGossipCache[key]; !exists {
		for len(n.snapshotMetaGossipCache) >= maxSnapshotMetaGossipCacheEntries {
			evictOldestSnapshotMetaGossipLocked(n.snapshotMetaGossipCache)
		}
	}
	n.snapshotMetaGossipCache[key] = msg
	return true
}

// recordSnapshotChunkGossip implements the record snapshot chunk gossip helper.
func (n *Node) recordSnapshotChunkGossip(msg SnapshotChunkGossip) bool {
	if n == nil || msg.Height == 0 || strings.TrimSpace(msg.From) == "" ||
		strings.TrimSpace(msg.SnapshotHash) == "" ||
		len(msg.From) > maxSnapshotManifestFieldBytes ||
		len(msg.SnapshotHash) > maxSnapshotManifestFieldBytes ||
		!snapshotChunkLayoutValid(msg.ChunkSize, msg.ChunkCount, 0) {
		return false
	}
	now := time.Now()
	msg.Timestamp = now.Unix()
	// `key` stores the key used to access the related value.
	key := snapshotChunkGossipKey(msg.Height, msg.From, msg.SnapshotHash)
	n.snapshotGossipMu.Lock()
	defer n.snapshotGossipMu.Unlock()
	if n.snapshotChunkGossipCache == nil {
		n.snapshotChunkGossipCache = make(map[string]SnapshotChunkGossip)
	}
	pruneSnapshotChunkGossipCacheLocked(n.snapshotChunkGossipCache, now)
	if _, exists := n.snapshotChunkGossipCache[key]; !exists {
		for len(n.snapshotChunkGossipCache) >= maxSnapshotChunkGossipCacheEntries {
			evictOldestSnapshotChunkGossipLocked(n.snapshotChunkGossipCache)
		}
	}
	n.snapshotChunkGossipCache[key] = msg
	return true
}

// cachedSnapshotMetaAvailabilities implements the cached snapshot meta availabilities helper.
func (n *Node) cachedSnapshotMetaAvailabilities(targetHeight uint64, minHeight uint64, validatorProviders map[string]string) []strictSnapshotMetaAvailability {
	if n == nil || len(validatorProviders) == 0 {
		return nil
	}
	n.snapshotGossipMu.Lock()
	pruneSnapshotMetaGossipCacheLocked(n.snapshotMetaGossipCache, time.Now())
	// `cached` stores the value produced by this operation.
	cached := make([]SnapshotMetaGossip, 0, len(n.snapshotMetaGossipCache))
	// `msg` tracks the current values while iterating.
	for _, msg := range n.snapshotMetaGossipCache {
		cached = append(cached, msg)
	}
	n.snapshotGossipMu.Unlock()
	if len(cached) == 0 {
		return nil
	}
	// `bestByValidator` stores the value produced by this operation.
	bestByValidator := make(map[string]strictSnapshotMetaAvailability, len(validatorProviders))
	// `msg` tracks the current values while iterating.
	for _, msg := range cached {
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := normalizeValidatorID(msg.From)
		if validatorID == "" {
			continue
		}
		// `provider` stores the value produced by this operation.
		provider := strings.TrimSpace(validatorProviders[validatorID])
		if provider == "" {
			continue
		}
		// `meta` stores the value produced by this operation.
		meta := msg.Meta
		if !meta.Available {
			continue
		}
		// `observation` stores the value produced by this operation.
		observation, _ := n.strictSnapshotMetaObservationForTarget(&meta, provider, validatorID, targetHeight, minHeight, false)
		if observation == nil || observation.Candidate == nil {
			continue
		}
		// `availability` stores the value produced by this operation.
		availability := strictSnapshotMetaAvailability{
			Provider:    provider,
			ValidatorID: validatorID,
			Meta:        &meta,
			Candidate:   observation.Candidate,
		}
		// `existing` and `ok` store whether the related condition is satisfied.
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
	// `out` stores the result produced by this operation.
	out := make([]strictSnapshotMetaAvailability, 0, len(bestByValidator))
	// `availability` tracks the current values while iterating.
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

// snapshotChunkGossipProviderScore implements the snapshot chunk gossip provider score helper.
func (n *Node) snapshotChunkGossipProviderScore(height uint64, snapshotHash string, provider string) int {
	if n == nil || height == 0 || strings.TrimSpace(snapshotHash) == "" || strings.TrimSpace(provider) == "" {
		return 0
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := strings.ToLower(strings.TrimSpace(snapshotHash))
	provider = strings.TrimSpace(provider)
	// `score` stores the value produced by this operation.
	score := 0
	n.snapshotGossipMu.Lock()
	pruneSnapshotChunkGossipCacheLocked(n.snapshotChunkGossipCache, time.Now())
	// `msg` tracks the current values while iterating.
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

// handleSnapshotMetaGossipMessage handles snapshot meta gossip message.
func (n *Node) handleSnapshotMetaGossipMessage(msg SnapshotMetaGossip) {
	if n == nil {
		return
	}
	if msg.Meta.Available {
		if !n.recordSnapshotMetaGossip(msg) {
			return
		}
		// `provider` stores the value produced by this operation.
		provider := normalizeValidatorID(msg.From)
		if provider == "" {
			provider = strings.TrimSpace(msg.From)
		}
		// `chunkCount` stores the measured quantity used by this operation.
		chunkCount := msg.Meta.TotalChunks
		if chunkCount == 0 && msg.Manifest != nil {
			chunkCount = msg.Manifest.ChunkCount
		}
		n.updateSnapshotCatalogMeta(msg.Height, strings.TrimSpace(msg.Meta.StateRoot), chunkCount, []string{provider})
		n.updateSnapshotCatalogProviders(msg.Height, []string{provider})
	}
}

// handleSnapshotChunkGossipMessage handles snapshot chunk gossip message.
func (n *Node) handleSnapshotChunkGossipMessage(msg SnapshotChunkGossip) {
	if n == nil {
		return
	}
	if !n.recordSnapshotChunkGossip(msg) {
		return
	}
	// `provider` stores the value produced by this operation.
	provider := normalizeValidatorID(msg.From)
	if provider == "" {
		provider = strings.TrimSpace(msg.From)
	}
	n.updateSnapshotCatalogMeta(msg.Height, "", msg.ChunkCount, []string{provider})
	n.updateSnapshotCatalogProviders(msg.Height, []string{provider})
}

// handleSnapshotMetaGossip handles snapshot meta gossip.
func (n *Node) handleSnapshotMetaGossip(sub *pubsub.Subscription) {
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
		if n.handleConsensusEnvelope(msg.Data) {
			continue
		}
		// `payload` stores the value used by this operation.
		var payload SnapshotMetaGossip
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			continue
		}
		n.handleSnapshotMetaGossipMessage(payload)
	}
}

// handleSnapshotChunkGossip handles snapshot chunk gossip.
func (n *Node) handleSnapshotChunkGossip(sub *pubsub.Subscription) {
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
		if n.handleConsensusEnvelope(msg.Data) {
			continue
		}
		// `payload` stores the value used by this operation.
		var payload SnapshotChunkGossip
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			continue
		}
		n.handleSnapshotChunkGossipMessage(payload)
	}
}

// publishSnapshotMetaGossip implements the publish snapshot meta gossip helper.
func (n *Node) publishSnapshotMetaGossip(snapshot *StateSnapshot) bool {
	return n.publishSnapshotMetaGossipInternal(snapshot, false)
}

// publishSnapshotMetaGossipForce implements the publish snapshot meta gossip force helper.
func (n *Node) publishSnapshotMetaGossipForce(snapshot *StateSnapshot) bool {
	return n.publishSnapshotMetaGossipInternal(snapshot, true)
}

// snapshotPublishCooldown implements the snapshot publish cooldown helper.
func (n *Node) snapshotPublishCooldown() time.Duration {
	if n == nil {
		return syncSnapshotPublishReannounceCooldown()
	}
	n.snapshotGossipMu.RLock()
	// `until` stores the value produced by this operation.
	until := n.snapshotBoostUntil
	n.snapshotGossipMu.RUnlock()
	if !until.IsZero() && time.Now().Before(until) {
		return 4 * time.Second
	}
	return syncSnapshotPublishReannounceCooldown()
}

// markSnapshotPublishBoost implements the mark snapshot publish boost helper.
func (n *Node) markSnapshotPublishBoost(reason string) {
	if n == nil {
		return
	}
	if strings.TrimSpace(reason) != "new_node_join" {
		return
	}
	// `until` stores the value produced by this operation.
	until := time.Now().Add(15 * time.Second)
	n.snapshotGossipMu.Lock()
	if until.After(n.snapshotBoostUntil) {
		n.snapshotBoostUntil = until
	}
	n.snapshotGossipMu.Unlock()
}

// publishSnapshotMetaGossipInternal implements the publish snapshot meta gossip internal helper.
func (n *Node) publishSnapshotMetaGossipInternal(snapshot *StateSnapshot, allowRepeat bool) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	// `manifest` and `err` store the error produced by this operation.
	manifest, _, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil || manifest == nil {
		return false
	}
	from := n.localConsensusValidatorIDForHeight(snapshot.Height)
	// `msg` stores the value produced by this operation.
	msg := SnapshotMetaGossip{
		From:   from,
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
	if !n.recordSnapshotMetaGossip(msg) {
		return false
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	// `wrapped` and `err` store the error produced by this operation.
	wrapped, err := MarshalP2PMessage(Message{Type: MsgSnapshotMeta, Data: raw})
	if err != nil {
		return false
	}
	// `key` stores the key used to access the related value.
	key := snapshotMetaGossipKey(msg.Height, msg.From, msg.Meta.SnapshotHash)
	// `now` stores the value produced by this operation.
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
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(n.RootContext(), validatorHeartbeatPublishTimeout())
		// `err` stores the error produced by this operation.
		err := n.SnapshotMetaTopic.Publish(ctx, wrapped)
		cancel()
		if err == nil {
			return true
		}
	}
	if n.PubSub != nil {
		// `err` stores the error produced by this operation.
		if err := n.PubSub.Publish(TopicSnapshotMeta, wrapped); err == nil {
			return true
		}
	}
	return false
}

// publishSnapshotChunkGossip implements the publish snapshot chunk gossip helper.
func (n *Node) publishSnapshotChunkGossip(snapshot *StateSnapshot) bool {
	return n.publishSnapshotChunkGossipInternal(snapshot, false)
}

// publishSnapshotChunkGossipForce implements the publish snapshot chunk gossip force helper.
func (n *Node) publishSnapshotChunkGossipForce(snapshot *StateSnapshot) bool {
	return n.publishSnapshotChunkGossipInternal(snapshot, true)
}

// publishSnapshotChunkGossipInternal implements the publish snapshot chunk gossip internal helper.
func (n *Node) publishSnapshotChunkGossipInternal(snapshot *StateSnapshot, allowRepeat bool) bool {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return false
	}
	// `manifest` and `err` store the error produced by this operation.
	manifest, _, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil || manifest == nil {
		return false
	}
	from := n.localConsensusValidatorIDForHeight(snapshot.Height)
	// `msg` stores the value produced by this operation.
	msg := SnapshotChunkGossip{
		From:         from,
		Height:       snapshot.Height,
		SnapshotHash: strings.TrimSpace(snapshot.SnapshotHash),
		ChunkSize:    manifest.ChunkSize,
		ChunkCount:   manifest.ChunkCount,
		Timestamp:    time.Now().Unix(),
	}
	if !n.recordSnapshotChunkGossip(msg) {
		return false
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	// `wrapped` and `err` store the error produced by this operation.
	wrapped, err := MarshalP2PMessage(Message{Type: MsgSnapshotChunk, Data: raw})
	if err != nil {
		return false
	}
	// `key` stores the key used to access the related value.
	key := snapshotChunkGossipKey(msg.Height, msg.From, msg.SnapshotHash)
	// `now` stores the value produced by this operation.
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
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(n.RootContext(), validatorHeartbeatPublishTimeout())
		// `err` stores the error produced by this operation.
		err := n.SnapshotChunkTopic.Publish(ctx, wrapped)
		cancel()
		if err == nil {
			return true
		}
	}
	if n.PubSub != nil {
		// `err` stores the error produced by this operation.
		if err := n.PubSub.Publish(TopicSnapshotChunk, wrapped); err == nil {
			return true
		}
	}
	return false
}
