package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const peerReputationFileName = "peer_reputation.json"

var PeerAdmissionMinReputation = 0.20
var PeerAdmissionMinSamples uint64 = 5
var PeerReputationDecayInterval = 30 * time.Minute

func decayCounterHalf(v uint64) uint64 {
	if v <= 1 {
		return 0
	}
	return v / 2
}

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
	intervals := int(now.Sub(score.DecayedAt) / PeerReputationDecayInterval)
	if intervals <= 0 {
		return
	}
	if intervals > 8 {
		intervals = 8
	}
	for i := 0; i < intervals; i++ {
		score.SnapshotFail = decayCounterHalf(score.SnapshotFail)
		score.BlockBatchFail = decayCounterHalf(score.BlockBatchFail)
		score.DialFailure = decayCounterHalf(score.DialFailure)
		score.TimeoutCount = decayCounterHalf(score.TimeoutCount)
		score.InvalidProofCount = decayCounterHalf(score.InvalidProofCount)
		score.SecurityFaultCount = decayCounterHalf(score.SecurityFaultCount)
		score.RateLimitDropCount = decayCounterHalf(score.RateLimitDropCount)
	}
	score.DecayedAt = score.DecayedAt.Add(time.Duration(intervals) * PeerReputationDecayInterval)
}

func peerReputationValue(score *SyncPeerScore) (float64, uint64) {
	if score == nil {
		return 1, 0
	}
	success := score.SnapshotSuccess + score.BlockBatchSuccess + score.DialSuccess
	failures := score.SnapshotFail + score.BlockBatchFail + score.DialFailure + score.TimeoutCount + score.InvalidProofCount + score.SecurityFaultCount + score.RateLimitDropCount
	total := success + failures
	if total == 0 {
		return 1, 0
	}
	return float64(success) / float64(total), total
}

func (n *Node) peerReputationPath() string {
	if n == nil || n.DataDir == "" {
		return ""
	}
	return filepath.Join(n.DataDir, peerReputationFileName)
}

func (n *Node) loadPeerReputation() {
	path := n.peerReputationPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	loaded := make(map[string]*SyncPeerScore)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	n.syncPeerScoreMu.Lock()
	if n.syncPeerScores == nil {
		n.syncPeerScores = make(map[string]*SyncPeerScore)
	}
	for peerID, score := range loaded {
		if peerID == "" || score == nil {
			continue
		}
		decaySyncPeerScore(score, time.Now())
		n.syncPeerScores[peerID] = score
	}
	n.syncPeerScoreMu.Unlock()
}

func (n *Node) savePeerReputation() {
	path := n.peerReputationPath()
	if path == "" {
		return
	}
	n.syncPeerScoreMu.Lock()
	snapshot := make(map[string]*SyncPeerScore, len(n.syncPeerScores))
	for peerID, score := range n.syncPeerScores {
		if peerID == "" || score == nil {
			continue
		}
		copyScore := *score
		snapshot[peerID] = &copyScore
	}
	n.syncPeerScoreMu.Unlock()
	if len(snapshot) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (n *Node) notePeerDialScore(peerID string, success bool) {
	if n == nil || peerID == "" {
		return
	}
	n.syncPeerScoreMu.Lock()
	if n.syncPeerScores == nil {
		n.syncPeerScores = make(map[string]*SyncPeerScore)
	}
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
	score := n.syncPeerScores[peerID]
	decaySyncPeerScore(score, time.Now())
	reputation, samples := peerReputationValue(score)
	n.syncPeerScoreMu.Unlock()
	if samples >= PeerAdmissionMinSamples && reputation < PeerAdmissionMinReputation {
		n.ensurePeerIsolationMaps()
		n.disconnectPeerID(peerID, "low_peer_reputation")
		return false
	}
	return true
}
