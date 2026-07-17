package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// `peerReputationFileName` defines the constant value used by this package.
const peerReputationFileName = "peer_reputation.json"

// `PeerAdmissionMinReputation` stores the value used by this operation.
var PeerAdmissionMinReputation = 0.20

// `PeerAdmissionMinSamples` stores the value used by this operation.
var PeerAdmissionMinSamples uint64 = 5

// `PeerReputationDecayInterval` stores the value currently being processed.
var PeerReputationDecayInterval = 30 * time.Minute

// decayCounterHalf implements the decay counter half helper.
func decayCounterHalf(v uint64) uint64 {
	if v <= 1 {
		return 0
	}
	return v / 2
}

// decaySyncPeerScore implements the decay sync peer score helper.
func decaySyncPeerScore(score *SyncPeerScore, now time.Time) {
	if score == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if PeerReputationDecayInterval <= 0 {
		PeerReputationDecayInterval = 30 * time.Minute
	}
	if score.DecayedAt.IsZero() {
		score.DecayedAt = now
		return
	}
	// `intervals` stores the current position in the related collection.
	intervals := int(now.Sub(score.DecayedAt) / PeerReputationDecayInterval)
	if intervals <= 0 {
		return
	}
	if intervals > 8 {
		intervals = 8
	}
	// `i` stores the current position in the related collection.
	for i := 0; i < intervals; i++ {
		score.SnapshotFail = decayCounterHalf(score.SnapshotFail)
		score.BlockBatchFail = decayCounterHalf(score.BlockBatchFail)
		score.DialFailure = decayCounterHalf(score.DialFailure)
		score.TimeoutCount = decayCounterHalf(score.TimeoutCount)
		score.InvalidProofCount = decayCounterHalf(score.InvalidProofCount)
		score.SecurityFaultCount = decayCounterHalf(score.SecurityFaultCount)
		score.RateLimitDropCount = decayCounterHalf(score.RateLimitDropCount)
		score.TrustedSecurityFaultCount = decayCounterHalf(score.TrustedSecurityFaultCount)
		score.TrustedRateLimitDropCount = decayCounterHalf(score.TrustedRateLimitDropCount)
	}
	score.DecayedAt = score.DecayedAt.Add(time.Duration(intervals) * PeerReputationDecayInterval)
}

// peerReputationValue implements the peer reputation value helper.
func peerReputationValue(score *SyncPeerScore) (float64, uint64) {
	if score == nil {
		return 1, 0
	}
	// Correct block batches carry the strongest positive weight. Invalid
	// proofs/security faults carry a decisive penalty, and timeouts are more
	// expensive than ordinary transport failures.
	successSamples := score.SnapshotSuccess + score.BlockBatchSuccess + score.DialSuccess
	failureSamples := score.SnapshotFail + score.BlockBatchFail + score.DialFailure + score.TimeoutCount + score.InvalidProofCount + score.SecurityFaultCount + score.RateLimitDropCount
	total := successSamples + failureSamples
	if total == 0 {
		return 1, 0
	}
	positive := float64(score.DialSuccess)*10 +
		float64(score.SnapshotSuccess)*10 +
		float64(score.BlockBatchSuccess)*20
	negative := float64(score.DialFailure)*10 +
		float64(score.SnapshotFail)*20 +
		float64(score.BlockBatchFail)*20 +
		float64(score.TimeoutCount)*20 +
		float64(score.InvalidProofCount)*100 +
		float64(score.SecurityFaultCount)*100 +
		float64(score.RateLimitDropCount)*20
	return positive / (positive + negative), total
}

// peerReputationPath implements the peer reputation path helper.
func (n *Node) peerReputationPath() string {
	if n == nil || n.DataDir == "" {
		return ""
	}
	return filepath.Join(n.DataDir, peerReputationFileName)
}

// loadPeerReputation implements the load peer reputation helper.
func (n *Node) loadPeerReputation() {
	// `path` stores the value produced by this operation.
	path := n.peerReputationPath()
	if path == "" {
		return
	}
	// `data` and `err` store the error produced by this operation.
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	// `loaded` stores the value produced by this operation.
	loaded := make(map[string]*SyncPeerScore)
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	n.syncPeerScoreMu.Lock()
	if n.syncPeerScores == nil {
		n.syncPeerScores = make(map[string]*SyncPeerScore)
	}
	// `peerID` and `score` track the current values while iterating.
	for peerID, score := range loaded {
		if peerID == "" || score == nil {
			continue
		}
		decaySyncPeerScore(score, time.Now())
		n.syncPeerScores[peerID] = score
	}
	n.syncPeerScoreMu.Unlock()
}

// savePeerReputation implements the save peer reputation helper.
func (n *Node) savePeerReputation() {
	// `path` stores the value produced by this operation.
	path := n.peerReputationPath()
	if path == "" {
		return
	}
	n.syncPeerScoreMu.Lock()
	// `snapshot` stores the value produced by this operation.
	snapshot := make(map[string]*SyncPeerScore, len(n.syncPeerScores))
	// `peerID` and `score` track the current values while iterating.
	for peerID, score := range n.syncPeerScores {
		if peerID == "" || score == nil {
			continue
		}
		// `copyScore` stores the value produced by this operation.
		copyScore := *score
		snapshot[peerID] = &copyScore
	}
	n.syncPeerScoreMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// `data` and `err` store the error produced by this operation.
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	// `tmp` stores the value produced by this operation.
	tmp := path + ".tmp"
	// `err` stores the error produced by this operation.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// notePeerDialScore implements the note peer dial score helper.
func (n *Node) notePeerDialScore(peerID string, success bool) {
	if n == nil || peerID == "" {
		return
	}
	n.syncPeerScoreMu.Lock()
	if n.syncPeerScores == nil {
		n.syncPeerScores = make(map[string]*SyncPeerScore)
	}
	// `score` stores the value produced by this operation.
	score := n.syncPeerScores[peerID]
	if score == nil {
		score = &SyncPeerScore{}
		n.syncPeerScores[peerID] = score
	}
	if success {
		score.DialSuccess++
	} else {
		score.DialFailure++
		score.TimeoutCount++
	}
	score.UpdatedAt = time.Now()
	if score.DecayedAt.IsZero() {
		score.DecayedAt = score.UpdatedAt
	}
	n.syncPeerScoreMu.Unlock()
	n.savePeerReputation()
}

// peerAdmissionAllowed implements the peer admission allowed helper.
func (n *Node) peerAdmissionAllowed(peerID string) bool {
	if n == nil || peerID == "" {
		return true
	}
	if n.isValidatorOrPersistentPeerID(peerID) {
		return true
	}
	if n.isPeerQuarantined(peerID) {
		return false
	}
	n.syncPeerScoreMu.Lock()
	// `score` stores the value produced by this operation.
	score := n.syncPeerScores[peerID]
	decaySyncPeerScore(score, time.Now())
	// `reputation` and `samples` store the value produced by this operation.
	reputation, samples := peerReputationValue(score)
	n.syncPeerScoreMu.Unlock()
	if samples >= PeerAdmissionMinSamples && reputation < PeerAdmissionMinReputation {
		n.ensurePeerIsolationMaps()
		n.disconnectPeerID(peerID, "low_peer_reputation")
		return false
	}
	return true
}

// enforceConnectedPeerReputation applies the admission policy to peers that
// degraded after connecting. This closes the gap where a low-score peer could
// otherwise remain connected indefinitely.
func (n *Node) enforceConnectedPeerReputation() int {
	if n == nil || n.Host == nil {
		return 0
	}
	disconnected := 0
	for _, peerID := range n.Host.Network().Peers() {
		if !n.peerAdmissionAllowed(peerID.String()) {
			disconnected++
		}
	}
	return disconnected
}
