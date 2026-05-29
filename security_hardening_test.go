package main

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestConsensusMetadataCoveredByBlockSignature(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	priv := installValidatorPubKeyForTest(t, node, block.Proposer)

	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = 3
	block.RequiredQuorum = 3
	block.StrictQuorum = 3
	signBlockHashForTest(&block, priv)

	if !VerifyBlockSignature(block) {
		t.Fatalf("expected signed consensus metadata to verify")
	}
	block.RequiredQuorum = 2
	if VerifyBlockSignature(block) {
		t.Fatalf("consensus metadata tamper should invalidate block signature")
	}
}

func TestInvalidSnapshotProofPenalizesSnapshotProviderWithoutQuarantine(t *testing.T) {
	oldThreshold := SyncSnapshotInvalidProofQuarantineAfter
	SyncSnapshotInvalidProofQuarantineAfter = 2
	defer func() { SyncSnapshotInvalidProofQuarantineAfter = oldThreshold }()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerID := "peer-bad-snapshot-proof"
	proof := SnapshotProof{
		Height:                64,
		CheckpointHeight:      snapshotCheckpointHeightFor(64),
		SnapshotHash:          "bad-snapshot-hash",
		StateRoot:             "state-root",
		ValidatorSetHash:      "validator-set-hash",
		ValidatorRegistryHash: "validator-registry-hash",
		Validator:             "A",
		SignatureHex:          strings.Repeat("00", ed25519.SignatureSize),
	}

	node.handleSnapshotProofFromPeer(proof, peerID)
	score, ok := node.syncPeerScoreSnapshot(peerID)
	if !ok || score.InvalidProofCount != 1 {
		t.Fatalf("expected invalid snapshot proof reputation strike, ok=%t score=%+v", ok, score)
	}
	if node.isPeerQuarantined(peerID) {
		t.Fatalf("peer should not be quarantined before threshold")
	}

	node.handleSnapshotProofFromPeer(proof, peerID)
	if node.isPeerQuarantined(peerID) {
		t.Fatalf("peer should not be transport-quarantined for snapshot proof failures")
	}
	if class := node.syncPeerReputationClass(peerID); class != "avoid" {
		t.Fatalf("expected invalid snapshot provider to be avoided, got class=%q", class)
	}
}

func TestInvalidSnapshotProofPubsubDoesNotQuarantineRelayPeer(t *testing.T) {
	oldThreshold := SyncSnapshotInvalidProofQuarantineAfter
	SyncSnapshotInvalidProofQuarantineAfter = 2
	defer func() { SyncSnapshotInvalidProofQuarantineAfter = oldThreshold }()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerID := "peer-relayed-snapshot-proof"
	proof := SnapshotProof{
		Height:                64,
		CheckpointHeight:      snapshotCheckpointHeightFor(64),
		SnapshotHash:          "bad-snapshot-hash",
		StateRoot:             "state-root",
		ValidatorSetHash:      "validator-set-hash",
		ValidatorRegistryHash: "validator-registry-hash",
		Validator:             "A",
		SignatureHex:          strings.Repeat("00", ed25519.SignatureSize),
	}

	node.handleSnapshotProofFromGossip(proof, peerID)
	node.handleSnapshotProofFromGossip(proof, peerID)
	if score, ok := node.syncPeerScoreSnapshot(peerID); ok && score.InvalidProofCount != 0 {
		t.Fatalf("relay peer should not receive invalid proof strikes, score=%+v", score)
	}
	if node.isPeerQuarantined(peerID) {
		t.Fatalf("relay peer should not be quarantined for relayed invalid snapshot proof")
	}
}

func TestPeerMessageRateLimitFeedsReputationAndQuarantine(t *testing.T) {
	limiterMu.Lock()
	oldLimiters := messageLimiter
	oldLastSeen := messageLimiterLastSeen
	messageLimiter = make(map[string]*rate.Limiter)
	messageLimiterLastSeen = make(map[string]time.Time)
	limiterMu.Unlock()
	defer func() {
		limiterMu.Lock()
		messageLimiter = oldLimiters
		messageLimiterLastSeen = oldLastSeen
		limiterMu.Unlock()
	}()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerID := "peer-spam-rate-limit"
	node.ensurePeerIsolationMaps()
	node.peerStateMu.Lock()
	node.peerHelloOK[peerID] = true
	node.peerStateMu.Unlock()

	for i := 0; i < 80 && !node.isPeerQuarantined(peerID); i++ {
		node.handleMessage(Message{Type: MsgPing}, peerID)
	}

	score, ok := node.syncPeerScoreSnapshot(peerID)
	if !ok || score.RateLimitDropCount < peerRateLimitDropQuarantineAfter {
		t.Fatalf("expected rate-limit drops to be scored, ok=%t score=%+v", ok, score)
	}
	if !node.isPeerQuarantined(peerID) {
		t.Fatalf("rate-limited spam peer should be quarantined")
	}
	if rep := node.syncPeerReputationValue(peerID); rep >= 0.5 {
		t.Fatalf("expected spam peer reputation below neutral, got=%f", rep)
	}
}

func TestInvalidLeaderBlockSignatureFeedsSecurityReputation(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerID := "peer-bad-leader"
	height := node.currentEpoch()
	block := Block{
		ID:        height,
		Height:    height,
		PrevHash:  node.Blockchain.LastBlock().BlockHash,
		Proposer:  "A",
		Type:      BlockTypeTime,
		StateRoot: strings.Repeat("a", 64),
		BlockTime: LogicalTimeForEpoch(height),
		Signature: make([]byte, ed25519.SignatureSize),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	for i := uint64(0); i < peerSecurityFaultQuarantineAfter; i++ {
		if node.verifyLeaderBlock(block, peerID) {
			t.Fatalf("invalid leader block signature should be rejected")
		}
	}
	score, ok := node.syncPeerScoreSnapshot(peerID)
	if !ok || score.SecurityFaultCount < peerSecurityFaultQuarantineAfter {
		t.Fatalf("expected security faults to be scored, ok=%t score=%+v", ok, score)
	}
	if !node.isPeerQuarantined(peerID) {
		t.Fatalf("repeated invalid leader blocks should quarantine peer")
	}
}
