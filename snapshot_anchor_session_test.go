package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

func cloneSnapshotForTest(in *StateSnapshot) *StateSnapshot {
	if in == nil {
		return nil
	}
	out := *in
	if in.Validators != nil {
		out.Validators = make(map[string]bool, len(in.Validators))
		for k, v := range in.Validators {
			out.Validators[k] = v
		}
	}
	if in.PendingValidators != nil {
		out.PendingValidators = make(map[string]uint64, len(in.PendingValidators))
		for k, v := range in.PendingValidators {
			out.PendingValidators[k] = v
		}
	}
	if in.PendingValidatorRemovals != nil {
		out.PendingValidatorRemovals = make(map[string]uint64, len(in.PendingValidatorRemovals))
		for k, v := range in.PendingValidatorRemovals {
			out.PendingValidatorRemovals[k] = v
		}
	}
	if in.ValidatorRegistry != nil {
		out.ValidatorRegistry = make(map[string]ValidatorRecord, len(in.ValidatorRegistry))
		for k, v := range in.ValidatorRegistry {
			out.ValidatorRegistry[k] = v
		}
	}
	if in.StateValidators != nil {
		out.StateValidators = make(map[string]Validator, len(in.StateValidators))
		for k, v := range in.StateValidators {
			clone := v
			if v.PubKey != nil {
				clone.PubKey = append([]byte{}, v.PubKey...)
			}
			out.StateValidators[k] = clone
		}
	}
	if in.CheckpointProof != nil {
		out.CheckpointProof = make(map[string]string, len(in.CheckpointProof))
		for k, v := range in.CheckpointProof {
			out.CheckpointProof[k] = v
		}
	}
	return &out
}

func enableFreshJoinFallbackBlockReplayForTest(t *testing.T) {
	t.Helper()
	oldEnabled := SyncFreshJoinFallbackBlockReplayEnabled
	SyncFreshJoinFallbackBlockReplayEnabled = true
	t.Cleanup(func() {
		SyncFreshJoinFallbackBlockReplayEnabled = oldEnabled
	})
}

func attachRemoteStartupSampleForTest(t *testing.T, n *Node, validatorID string, height uint64, hash string) {
	t.Helper()
	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	t.Cleanup(func() {
		_ = host.Close()
	})
	n.Host = host

	peerID := "peer" + strings.ToUpper(strings.TrimSpace(validatorID))
	n.peerStateMu.Lock()
	if n.peerRole == nil {
		n.peerRole = make(map[string]string)
	}
	if n.peerHelloOK == nil {
		n.peerHelloOK = make(map[string]bool)
	}
	if n.peerToValidator == nil {
		n.peerToValidator = make(map[string]string)
	}
	if n.peerSetHash == nil {
		n.peerSetHash = make(map[string]string)
	}
	if n.peerAckHeight == nil {
		n.peerAckHeight = make(map[string]uint64)
	}
	n.peerRole[peerID] = "validator"
	n.peerHelloOK[peerID] = true
	n.peerToValidator[peerID] = normalizeValidatorID(validatorID)
	n.peerSetHash[peerID] = strings.TrimSpace(hash)
	n.peerAckHeight[peerID] = height
	n.peerStateMu.Unlock()
}

func TestSnapshotAnchorFreezeIgnoresTargetDrift(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	session := n.startSnapshotSession(170, "large_lag")
	if !session.Active {
		t.Fatalf("expected active snapshot session")
	}
	if session.FreezeHeight != 170 {
		t.Fatalf("unexpected frozen height: got=%d want=170", session.FreezeHeight)
	}
	if session.CheckpointHeight != 160 {
		t.Fatalf("unexpected checkpoint height: got=%d want=160", session.CheckpointHeight)
	}

	// Higher observed targets must not rewrite frozen anchor for this active session.
	n.startSnapshotSession(250, "heartbeat")
	after := n.snapshotSessionSnapshot()
	if after.FreezeHeight != 170 {
		t.Fatalf("expected frozen anchor to remain unchanged: got=%d want=170", after.FreezeHeight)
	}
	if after.CheckpointHeight != 160 {
		t.Fatalf("expected checkpoint anchor to remain unchanged: got=%d want=160", after.CheckpointHeight)
	}
}

func TestSnapshotVotesBoundToFreezeHeight(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	n.DataDir = t.TempDir()
	session := n.startSnapshotSession(170, "large_lag")
	cp := session.CheckpointHeight
	if cp == 0 {
		t.Fatalf("expected non-zero checkpoint height")
	}

	votes, accepted, _ := n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "A",
		Height:           cp + 1,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	})
	if accepted || votes != 0 {
		t.Fatalf("expected vote with wrong height to be ignored, votes=%d accepted=%t", votes, accepted)
	}

	voteA := SnapshotVote{
		ValidatorID:      "A",
		Height:           cp,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	}
	votes, accepted, _ = n.updateSnapshotSessionVote(voteA)
	if !accepted || votes != 1 {
		t.Fatalf("expected first valid vote accepted, votes=%d accepted=%t", votes, accepted)
	}
	if required := n.snapshotSessionSnapshot().Required; required <= 0 {
		t.Fatalf("snapshot vote must initialize required quorum, got=%d", required)
	}

	votes, accepted, _ = n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "B",
		Height:           cp,
		SnapshotHash:     "other-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	})
	if accepted || votes != 1 {
		t.Fatalf("expected mismatched snapshot hash vote to be rejected, votes=%d accepted=%t", votes, accepted)
	}

	votes, accepted, _ = n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "B",
		Height:           cp,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	})
	if !accepted || votes != 2 {
		t.Fatalf("expected second matching vote accepted, votes=%d accepted=%t", votes, accepted)
	}
}

func TestSnapshotVoteAccumulatorNotResetByHeartbeat(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	session := n.startSnapshotSession(170, "large_lag")
	cp := session.CheckpointHeight

	votes, accepted, _ := n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "A",
		Height:           cp,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	})
	if !accepted || votes != 1 {
		t.Fatalf("expected first vote accepted before heartbeat churn, votes=%d accepted=%t", votes, accepted)
	}

	// Simulate heartbeat/status target drift while session is active.
	n.startSnapshotSession(300, "heartbeat")
	after := n.snapshotSessionSnapshot()
	if after.FreezeHeight != 170 {
		t.Fatalf("expected freeze height unchanged across heartbeat churn, got=%d", after.FreezeHeight)
	}
	if len(after.Votes) != 1 {
		t.Fatalf("expected vote accumulator preserved across heartbeat churn, got=%d", len(after.Votes))
	}
}

func TestSnapshotSessionVoteConflictFlag(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	session := n.startSnapshotSession(170, "large_lag")
	cp := session.CheckpointHeight

	if _, accepted, conflict := n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "A",
		Height:           cp,
		SnapshotHash:     "snap-hash-1",
		StateRoot:        "state-root-1",
		ValidatorSetHash: "vset-hash-1",
	}); !accepted || conflict {
		t.Fatalf("expected first vote accepted without conflict")
	}

	if _, accepted, conflict := n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "B",
		Height:           cp,
		SnapshotHash:     "snap-hash-2",
		StateRoot:        "state-root-2",
		ValidatorSetHash: "vset-hash-2",
	}); accepted || !conflict {
		t.Fatalf("expected conflicting vote to be rejected with conflict flag")
	}
}

func TestSnapshotSessionHasQuorumForSnapshot(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	session := n.startSnapshotSession(170, "large_lag")
	cp := session.CheckpointHeight

	for _, id := range []string{"A", "B", "C"} {
		votes, accepted, _ := n.updateSnapshotSessionVote(SnapshotVote{
			ValidatorID:      id,
			Height:           cp,
			SnapshotHash:     "snap-hash",
			StateRoot:        "state-root",
			ValidatorSetHash: "vset-hash",
		})
		if !accepted || votes == 0 {
			t.Fatalf("expected vote accepted for %s", id)
		}
	}

	snap := &StateSnapshot{
		Height:           170,
		CheckpointHeight: cp,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	}
	if !n.snapshotSessionHasQuorumForSnapshot(snap, 3) {
		t.Fatalf("expected quorum match for snapshot")
	}
	if n.snapshotSessionHasQuorumForSnapshot(snap, 4) {
		t.Fatalf("expected quorum mismatch when required votes exceed collected votes")
	}

	bad := cloneSnapshotForTest(snap)
	bad.SnapshotHash = "other-hash"
	if n.snapshotSessionHasQuorumForSnapshot(bad, 3) {
		t.Fatalf("expected mismatched snapshot hash to fail session quorum match")
	}
}

func TestCheckpointHeightDeterministicInterval32(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	SyncCheckpointIntervalBlocks = 32
	tests := []struct {
		height uint64
		want   uint64
	}{
		{0, 0},
		{1, 0},
		{31, 0},
		{32, 32},
		{33, 32},
		{63, 32},
		{64, 64},
		{95, 64},
		{96, 96},
	}
	for _, tc := range tests {
		if got := snapshotCheckpointHeightFor(tc.height); got != tc.want {
			t.Fatalf("checkpoint height mismatch for %d: got=%d want=%d", tc.height, got, tc.want)
		}
	}
}

func TestCheckpointProofDomainSeparatedVerification(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncSnapshotCheckpointDomain = "MSC_SNAPSHOT_V1"
	SyncSnapshotCheckpointV2Height = 1 // enforce domain-separated V1

	key := strictActivationTestValidatorKey(101, "F")
	n := makeStrictActivationNode(64)
	n.ID = "F"
	n.ValidatorKey = key

	validatorPubKeysMu.Lock()
	ValidatorPubKeys["F"] = append(ed25519.PublicKey(nil), key.PublicKey...)
	validatorPubKeysMu.Unlock()

	snapshot := &StateSnapshot{
		Version:            SnapshotVersion,
		Height:             64,
		BlockHash:          "block_hash_64",
		StateRoot:          "state_root_64",
		LedgerHash:         HashLedger(n.Ledger),
		GenesisHash:        GenesisHash,
		PrevHash:           "prev_hash_63",
		Ledger:             n.Ledger,
		Validators:         map[string]bool{"A": true, "B": true, "C": true, "D": true, "F": true},
		ValidatorSetHeight: 64,
	}
	snapshot.ValidatorSetHash = ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	populateSnapshotDerivedFields(snapshot)
	n.attachSnapshotCheckpointProof(snapshot)

	if !n.verifySnapshotCheckpointProofForValidator(snapshot, "F") {
		t.Fatalf("expected valid domain-separated proof to verify")
	}

	wrongDomain := cloneSnapshotForTest(snapshot)
	wrongDomain.CheckpointDomain = "MSC_SNAPSHOT_V0"
	if n.verifySnapshotCheckpointProofForValidator(wrongDomain, "F") {
		t.Fatalf("expected wrong checkpoint domain to fail verification")
	}

	wrongCheckpoint := cloneSnapshotForTest(snapshot)
	wrongCheckpoint.CheckpointHeight = snapshot.CheckpointHeight - 32
	if n.verifySnapshotCheckpointProofForValidator(wrongCheckpoint, "F") {
		t.Fatalf("expected wrong checkpoint height to fail verification")
	}

	oldChainID := ChainID
	ChainID = oldChainID + "_wrong"
	if n.verifySnapshotCheckpointProofForValidator(snapshot, "F") {
		t.Fatalf("expected wrong chain id to fail verification")
	}
	ChainID = oldChainID

	wrongSignature := cloneSnapshotForTest(snapshot)
	wrongSignature.CheckpointProof["F"] = strings.Repeat("00", ed25519.SignatureSize)
	if n.verifySnapshotCheckpointProofForValidator(wrongSignature, "F") {
		t.Fatalf("expected wrong signature to fail verification")
	}
}

func TestSnapshotSessionTimeoutRotatesProviderSameAnchor(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	providerA, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create providerA host: %v", err)
	}
	defer providerA.Close()

	providerB, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create providerB host: %v", err)
	}
	defer providerB.Close()

	if err := localHost.Connect(ctx, peer.AddrInfo{ID: providerA.ID(), Addrs: providerA.Addrs()}); err != nil {
		t.Fatalf("failed to connect providerA: %v", err)
	}
	if err := localHost.Connect(ctx, peer.AddrInfo{ID: providerB.ID(), Addrs: providerB.Addrs()}); err != nil {
		t.Fatalf("failed to connect providerB: %v", err)
	}

	n := makeStrictActivationNode(50)
	n.Host = localHost
	n.peerStateMu.Lock()
	if n.peerToValidator == nil {
		n.peerToValidator = make(map[string]string)
	}
	n.peerToValidator[providerA.ID().String()] = "B"
	n.peerToValidator[providerB.ID().String()] = "C"
	n.peerStateMu.Unlock()

	n.validatorMu.Lock()
	n.validatorStatus["B"] = &ValidatorStatus{FinalizedHeight: 500, ReportedHeight: 500}
	n.validatorStatus["C"] = &ValidatorStatus{FinalizedHeight: 500, ReportedHeight: 500}
	n.validatorMu.Unlock()

	session := n.startSnapshotSession(170, "large_lag")
	n.updateSnapshotSessionProviders([]string{providerA.ID().String(), providerB.ID().String()})
	n.setSnapshotSessionProvider(providerA.ID().String())
	if _, accepted, _ := n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "B",
		Height:           session.CheckpointHeight,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	}); !accepted {
		t.Fatalf("expected initial session vote accepted")
	}

	if ok := n.snapshotSessionMarkFailure("snapshot_anchor_timeout"); !ok {
		t.Fatalf("expected snapshot session retry to stay active")
	}
	nextProvider := n.rotateSnapshotProvider()
	if strings.TrimSpace(nextProvider) == "" {
		t.Fatalf("expected provider rotation to choose an alternate peer")
	}
	if nextProvider == providerA.ID().String() {
		t.Fatalf("expected provider rotation to avoid failed provider")
	}

	after := n.snapshotSessionSnapshot()
	if after.FreezeHeight != session.FreezeHeight {
		t.Fatalf("expected freeze height unchanged across timeout retry: got=%d want=%d", after.FreezeHeight, session.FreezeHeight)
	}
	if after.CheckpointHeight != session.CheckpointHeight {
		t.Fatalf("expected checkpoint unchanged across timeout retry: got=%d want=%d", after.CheckpointHeight, session.CheckpointHeight)
	}
	if len(after.Votes) != 0 {
		t.Fatalf("expected votes cleared on timeout retry, got=%d", len(after.Votes))
	}
	if after.RetryCount == 0 {
		t.Fatalf("expected retry count incremented on timeout")
	}
}

func TestSnapshotAnchorUnreachableKeepsFreshNodeSnapshotForced(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	enableFreshJoinFallbackBlockReplayForTest(t)

	oldMaxRetries := SyncSnapshotAnchorMaxRetries
	SyncSnapshotAnchorMaxRetries = 6
	t.Cleanup(func() {
		SyncSnapshotAnchorMaxRetries = oldMaxRetries
	})

	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	session := n.startSnapshotSession(87, "late_join")
	if !session.Active {
		t.Fatalf("expected snapshot session active")
	}

	if ok := n.snapshotSessionMarkFailure("snapshot_anchor_unreachable"); !ok {
		t.Fatalf("expected first anchor-unreachable failure to remain retriable")
	}
	if n.allowFreshNodeBlockCatchup(0, 87) {
		t.Fatalf("fresh-node block catchup must stay disabled")
	}

	if ok := n.snapshotSessionMarkFailure("snapshot_anchor_unreachable"); !ok {
		t.Fatalf("expected repeated anchor-unreachable failure to remain retriable until max retries")
	}
	after := n.snapshotSessionSnapshot()
	if !after.Active {
		t.Fatalf("expected snapshot session to remain active without fallback")
	}
	if after.RetryCount != 2 {
		t.Fatalf("unexpected retry count: got=%d want=2", after.RetryCount)
	}
	if n.allowFreshNodeBlockCatchup(0, 87) {
		t.Fatalf("fresh-node block catchup must remain disabled after repeated anchor failures")
	}
	if !n.shouldForceSnapshotForFreshNode(0, 87) {
		t.Fatalf("snapshot must remain forced for fresh node after repeated anchor failures")
	}
}

func TestSnapshotAnchorUnreachableEnablesFreshNodeBlockCatchupAfterThreshold(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	enableFreshJoinFallbackBlockReplayForTest(t)

	oldMaxRetries := SyncSnapshotAnchorMaxRetries
	SyncSnapshotAnchorMaxRetries = 6
	t.Cleanup(func() {
		SyncSnapshotAnchorMaxRetries = oldMaxRetries
	})

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(0, hash)
	n.DataDir = t.TempDir()
	attachRemoteStartupSampleForTest(t, n, "B", 87, hash)
	attachRemoteStartupSampleForTest(t, n, "C", 87, hash)
	session := n.startSnapshotSession(87, "late_join")
	if !session.Active {
		t.Fatalf("expected snapshot session active")
	}
	for i := 0; i < 3; i++ {
		if ok := n.snapshotSessionMarkFailure("snapshot_anchor_unreachable"); !ok {
			t.Fatalf("expected anchor-unreachable failure #%d to remain retriable", i+1)
		}
	}
	if !n.allowFreshNodeBlockCatchup(0, 87) {
		t.Fatalf("expected fresh-node block catchup once guarded fallback conditions are met")
	}
	if !n.preferBlockRangeCatchup(0, 87) {
		t.Fatalf("expected block-range catchup preference after guarded fallback unlocks")
	}
	if n.shouldForceSnapshotForFreshNode(0, 87) {
		t.Fatalf("trusted snapshot should no longer be forced once fallback is unlocked")
	}
}

func TestFreshNodeBlockCatchupEnablesAfterGenericSnapshotRetries(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	enableFreshJoinFallbackBlockReplayForTest(t)

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(0, hash)
	n.DataDir = t.TempDir()
	attachRemoteStartupSampleForTest(t, n, "B", 87, hash)
	attachRemoteStartupSampleForTest(t, n, "C", 87, hash)
	session := n.startSnapshotSession(87, "late_join")
	if !session.Active {
		t.Fatalf("expected snapshot session active")
	}
	for i := 0; i < 3; i++ {
		if ok := n.snapshotSessionMarkFailure("meta_conflict"); !ok {
			t.Fatalf("expected generic retry #%d to remain retriable", i+1)
		}
	}
	if !n.allowFreshNodeBlockCatchup(0, 87) {
		t.Fatalf("expected fresh-node block catchup after generic snapshot retries with a quorum-backed sample")
	}
}

func TestFreshNodeBlockCatchupEnablesFromPersistedFailureCount(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	enableFreshJoinFallbackBlockReplayForTest(t)

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(0, hash)
	n.DataDir = t.TempDir()
	attachRemoteStartupSampleForTest(t, n, "B", 87, hash)
	attachRemoteStartupSampleForTest(t, n, "C", 87, hash)
	n.syncMu.Lock()
	n.syncSnapshotSessionFailures = syncSnapshotSessionFailureDegradeThreshold()
	n.syncMu.Unlock()
	if n.allowFreshNodeBlockCatchup(0, 87) {
		t.Fatalf("stale failure counters without an active degrade window must not unlock fresh-node block catchup")
	}
}

func TestSnapshotProviderScoringPenalizesInvalidProof(t *testing.T) {
	n := &Node{}
	n.recordSyncPeerSnapshotResult("good-peer", true, 20*time.Millisecond, 2048, false)
	n.recordSyncPeerSnapshotResult("good-peer", true, 25*time.Millisecond, 2048, false)
	n.recordSyncPeerSnapshotResult("bad-peer", true, 20*time.Millisecond, 2048, false)
	n.recordSyncPeerInvalidProof("bad-peer")

	goodScore := n.syncPeerScoreValue("good-peer")
	badScore := n.syncPeerScoreValue("bad-peer")
	if goodScore <= badScore {
		t.Fatalf("expected invalid-proof peer score lower than successful peer: good=%f bad=%f", goodScore, badScore)
	}
}

func TestSnapshotProofCacheSeedsSessionQuorum(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	oldRequireProof := SyncTrustedSnapshotRequireCheckpointProof
	oldCheckpointV2Height := SyncSnapshotCheckpointV2Height
	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncSnapshotCheckpointV2Height = 1
	t.Cleanup(func() {
		SyncTrustedSnapshotRequireCheckpointProof = oldRequireProof
		SyncSnapshotCheckpointV2Height = oldCheckpointV2Height
	})

	n := makeStrictActivationNode(96)
	n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)
	n.snapshotSession = SnapshotSession{Stage: SnapshotSyncStageIdle, Votes: make(map[string]SnapshotVote)}

	validatorPubKeysMu.Lock()
	prevKeys := make(map[string]ed25519.PublicKey)
	for _, id := range []string{"A", "B", "C"} {
		if existing, ok := ValidatorPubKeys[id]; ok {
			prevKeys[id] = append(ed25519.PublicKey(nil), existing...)
		}
	}
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		for _, id := range []string{"A", "B", "C"} {
			if prev, ok := prevKeys[id]; ok {
				ValidatorPubKeys[id] = prev
			} else {
				delete(ValidatorPubKeys, id)
			}
		}
	})

	keys := map[string]ValidatorKey{
		"A": strictActivationTestValidatorKey(41, "A"),
		"B": strictActivationTestValidatorKey(42, "B"),
		"C": strictActivationTestValidatorKey(43, "C"),
	}
	validatorPubKeysMu.Lock()
	for id, key := range keys {
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
	validatorPubKeysMu.Unlock()

	session := n.startSnapshotSession(96, "late_join")
	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                96,
		CheckpointHeight:      session.CheckpointHeight,
		CheckpointDomain:      syncSnapshotCheckpointDomain(),
		BlockHash:             "block-96",
		SnapshotHash:          "snapshot-hash-96",
		StateRoot:             "state-root-96",
		StateMerkleRoot:       "state-merkle-root-96",
		LedgerHash:            "ledger-hash-96",
		ValidatorSetHash:      "validator-set-hash-96",
		ValidatorRegistryHash: "validator-registry-hash-96",
	}

	for _, id := range []string{"A", "B", "C"} {
		clone := cloneSnapshotForTest(snapshot)
		clone.CheckpointProof = map[string]string{
			id: hex.EncodeToString(ed25519.Sign(keys[id].PrivateKey, snapshotCheckpointSignBytes(clone))),
		}
		proof := snapshotProofFromSnapshot(id, clone)
		votes, ok := n.recordSnapshotProof(proof)
		if !ok {
			t.Fatalf("expected proof from %s to be accepted", id)
		}
		if votes == 0 {
			t.Fatalf("expected non-zero cached vote count for %s", id)
		}
	}

	validatorProviders := map[string]string{
		"A": "peer-a",
		"B": "peer-b",
		"C": "peer-c",
	}
	observations, votes := n.cachedSnapshotProofObservations(96, 64, validatorProviders, 3)
	if len(observations) != 3 {
		t.Fatalf("unexpected proof observations: got=%d want=3", len(observations))
	}
	if len(votes) != 3 {
		t.Fatalf("unexpected proof vote count: got=%d want=3", len(votes))
	}
	quorum, best := selectStrictSnapshotMetaCandidate(observations, 3)
	if quorum == nil || best == nil {
		t.Fatalf("expected cached proof quorum candidate")
	}
	if quorum.Height != 96 || quorum.CheckpointHeight != session.CheckpointHeight {
		t.Fatalf("unexpected quorum candidate: %+v", quorum)
	}
	if cache, ok := n.cachedSnapshotAnchorLocked(96, 64); !ok || cache.Votes != 3 {
		t.Fatalf("expected cached anchor quorum entry, got=%+v ok=%t", cache, ok)
	}
	after := n.snapshotSessionSnapshot()
	if len(after.Votes) != 3 {
		t.Fatalf("expected session votes seeded from proof cache, got=%d", len(after.Votes))
	}
}

func TestSnapshotProofGossipLearnsValidatorPeerMapping(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	oldRequireProof := SyncTrustedSnapshotRequireCheckpointProof
	oldCheckpointV2Height := SyncSnapshotCheckpointV2Height
	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncSnapshotCheckpointV2Height = 1
	t.Cleanup(func() {
		SyncTrustedSnapshotRequireCheckpointProof = oldRequireProof
		SyncSnapshotCheckpointV2Height = oldCheckpointV2Height
	})

	n := makeStrictActivationNode(96)
	n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)
	n.snapshotSession = SnapshotSession{Stage: SnapshotSyncStageIdle, Votes: make(map[string]SnapshotVote)}

	key := strictActivationTestValidatorKey(51, "A")
	validatorPubKeysMu.Lock()
	prev, hadPrev := ValidatorPubKeys["A"]
	ValidatorPubKeys["A"] = append(ed25519.PublicKey(nil), key.PublicKey...)
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		if hadPrev {
			ValidatorPubKeys["A"] = prev
		} else {
			delete(ValidatorPubKeys, "A")
		}
	})

	session := n.startSnapshotSession(96, "late_join")
	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                96,
		CheckpointHeight:      session.CheckpointHeight,
		CheckpointDomain:      syncSnapshotCheckpointDomain(),
		BlockHash:             "block-96",
		SnapshotHash:          "snapshot-hash-96",
		StateRoot:             "state-root-96",
		StateMerkleRoot:       "state-merkle-root-96",
		LedgerHash:            "ledger-hash-96",
		ValidatorSetHash:      "validator-set-hash-96",
		ValidatorRegistryHash: "validator-registry-hash-96",
	}
	snapshot.CheckpointProof = map[string]string{
		"A": hex.EncodeToString(ed25519.Sign(key.PrivateKey, snapshotCheckpointSignBytes(snapshot))),
	}
	proof := snapshotProofFromSnapshot("A", snapshot)
	n.handleSnapshotProofFromPeer(proof, "peer-a")

	candidate := snapshotProofCandidateFromProof(&proof)
	providerKey := strictSnapshotMetaCandidateKey(candidate)
	n.snapshotProofMu.RLock()
	gotProvider := n.snapshotProofProviders[providerKey]["A"]
	n.snapshotProofMu.RUnlock()
	if gotProvider != "peer-a" {
		t.Fatalf("expected snapshot proof provider peer-a, got %q", gotProvider)
	}
	n.peerStateMu.Lock()
	got := n.peerToValidator["peer-a"]
	n.peerStateMu.Unlock()
	if got != "" {
		t.Fatalf("snapshot proof must not mutate peer validator identity, got %q", got)
	}
	observations, votes := n.cachedSnapshotProofObservations(96, 64, nil, 1)
	if len(observations) != 1 || len(votes) != 1 {
		t.Fatalf("expected cached proof observation from gossip peer, observations=%d votes=%d", len(observations), len(votes))
	}
	if observations[0].Provider != "peer-a" {
		t.Fatalf("expected proof observation provider peer-a, got %q", observations[0].Provider)
	}
}

func TestSnapshotProofEnvelopePreservesPeerMapping(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	oldRequireProof := SyncTrustedSnapshotRequireCheckpointProof
	oldCheckpointV2Height := SyncSnapshotCheckpointV2Height
	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncSnapshotCheckpointV2Height = 1
	t.Cleanup(func() {
		SyncTrustedSnapshotRequireCheckpointProof = oldRequireProof
		SyncSnapshotCheckpointV2Height = oldCheckpointV2Height
	})

	n := makeStrictActivationNode(96)
	n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	n.snapshotProofProviders = make(map[string]map[string]string)
	n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)
	n.snapshotSession = SnapshotSession{Stage: SnapshotSyncStageIdle, Votes: make(map[string]SnapshotVote)}

	key := strictActivationTestValidatorKey(51, "A")
	validatorPubKeysMu.Lock()
	prev, hadPrev := ValidatorPubKeys["A"]
	ValidatorPubKeys["A"] = append(ed25519.PublicKey(nil), key.PublicKey...)
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		if hadPrev {
			ValidatorPubKeys["A"] = prev
		} else {
			delete(ValidatorPubKeys, "A")
		}
	})

	session := n.startSnapshotSession(96, "late_join")
	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                96,
		CheckpointHeight:      session.CheckpointHeight,
		CheckpointDomain:      syncSnapshotCheckpointDomain(),
		BlockHash:             "block-96",
		SnapshotHash:          "snapshot-hash-96",
		StateRoot:             "state-root-96",
		StateMerkleRoot:       "state-merkle-root-96",
		LedgerHash:            "ledger-hash-96",
		ValidatorSetHash:      "validator-set-hash-96",
		ValidatorRegistryHash: "validator-registry-hash-96",
	}
	snapshot.CheckpointProof = map[string]string{
		"A": hex.EncodeToString(ed25519.Sign(key.PrivateKey, snapshotCheckpointSignBytes(snapshot))),
	}
	proof := snapshotProofFromSnapshot("A", snapshot)
	wrapped := MustJSON(Message{Type: MsgSnapshotProof, Data: MustJSON(proof)})
	if !n.handleConsensusEnvelopeFromPeer(wrapped, "peer-a") {
		t.Fatalf("expected snapshot proof envelope to be handled")
	}

	observations, votes := n.cachedSnapshotProofObservations(96, 64, nil, 1)
	if len(observations) != 1 || len(votes) != 1 {
		t.Fatalf("expected cached proof observation from envelope peer, observations=%d votes=%d", len(observations), len(votes))
	}
	if observations[0].Provider != "peer-a" {
		t.Fatalf("expected envelope proof provider peer-a, got %q", observations[0].Provider)
	}
	n.peerStateMu.Lock()
	got := n.peerToValidator["peer-a"]
	n.peerStateMu.Unlock()
	if got != "" {
		t.Fatalf("snapshot proof envelope must not mutate peer validator identity, got %q", got)
	}
}

func TestRecordSnapshotProofDedupesValidatorCandidateTuple(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	oldRequireProof := SyncTrustedSnapshotRequireCheckpointProof
	oldCheckpointV2Height := SyncSnapshotCheckpointV2Height
	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncSnapshotCheckpointV2Height = 1
	t.Cleanup(func() {
		SyncTrustedSnapshotRequireCheckpointProof = oldRequireProof
		SyncSnapshotCheckpointV2Height = oldCheckpointV2Height
	})

	n := makeStrictActivationNode(64)
	n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)

	key := strictActivationTestValidatorKey(77, "A")
	validatorPubKeysMu.Lock()
	prev, hadPrev := ValidatorPubKeys["A"]
	ValidatorPubKeys["A"] = append(ed25519.PublicKey(nil), key.PublicKey...)
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		if hadPrev {
			ValidatorPubKeys["A"] = prev
		} else {
			delete(ValidatorPubKeys, "A")
		}
	})

	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                64,
		CheckpointHeight:      64,
		CheckpointDomain:      syncSnapshotCheckpointDomain(),
		BlockHash:             "block-64",
		SnapshotHash:          "snapshot-hash-64",
		StateRoot:             "state-root-64",
		StateMerkleRoot:       "state-merkle-root-64",
		LedgerHash:            "ledger-hash-64",
		ValidatorSetHash:      "validator-set-hash-64",
		ValidatorRegistryHash: "validator-registry-hash-64",
	}
	snapshot.CheckpointProof = map[string]string{
		"A": hex.EncodeToString(ed25519.Sign(key.PrivateKey, snapshotCheckpointSignBytes(snapshot))),
	}
	proof := snapshotProofFromSnapshot("A", snapshot)

	votes, ok := n.recordSnapshotProof(proof)
	if !ok || votes != 1 {
		t.Fatalf("expected first proof to be recorded, votes=%d ok=%t", votes, ok)
	}
	votes, ok = n.recordSnapshotProof(proof)
	if ok {
		t.Fatalf("expected duplicate proof to be ignored")
	}
	if votes != 1 {
		t.Fatalf("duplicate proof should not increase votes, got=%d want=1", votes)
	}
}

func TestSnapshotProofQuorumPromotesHigherSessionCandidate(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	oldRequireProof := SyncTrustedSnapshotRequireCheckpointProof
	oldCheckpointV2Height := SyncSnapshotCheckpointV2Height
	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncSnapshotCheckpointV2Height = 1
	t.Cleanup(func() {
		SyncTrustedSnapshotRequireCheckpointProof = oldRequireProof
		SyncSnapshotCheckpointV2Height = oldCheckpointV2Height
	})

	n := makeStrictActivationNode(64)
	n.DataDir = t.TempDir()
	n.snapshotProofs = make(map[string]map[string]SnapshotProof)
	n.snapshotAnchorCache = make(map[uint64]SnapshotAnchorCache)
	n.snapshotSession = SnapshotSession{Stage: SnapshotSyncStageIdle, Votes: make(map[string]SnapshotVote)}

	validatorPubKeysMu.Lock()
	prevKeys := make(map[string]ed25519.PublicKey)
	for _, id := range []string{"A", "B", "C"} {
		if existing, ok := ValidatorPubKeys[id]; ok {
			prevKeys[id] = append(ed25519.PublicKey(nil), existing...)
		}
	}
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		for _, id := range []string{"A", "B", "C"} {
			if prev, ok := prevKeys[id]; ok {
				ValidatorPubKeys[id] = prev
			} else {
				delete(ValidatorPubKeys, id)
			}
		}
	})

	keys := map[string]ValidatorKey{
		"A": strictActivationTestValidatorKey(81, "A"),
		"B": strictActivationTestValidatorKey(82, "B"),
		"C": strictActivationTestValidatorKey(83, "C"),
	}
	validatorPubKeysMu.Lock()
	for id, key := range keys {
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
	validatorPubKeysMu.Unlock()

	session := n.startSnapshotSession(95, "late_join")
	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                96,
		CheckpointHeight:      session.CheckpointHeight,
		CheckpointDomain:      syncSnapshotCheckpointDomain(),
		BlockHash:             "block-96",
		SnapshotHash:          "snapshot-hash-96",
		StateRoot:             "state-root-96",
		StateMerkleRoot:       "state-merkle-root-96",
		LedgerHash:            "ledger-hash-96",
		ValidatorSetHash:      "validator-set-hash-96",
		ValidatorRegistryHash: "validator-registry-hash-96",
	}

	for _, id := range []string{"A", "B", "C"} {
		clone := cloneSnapshotForTest(snapshot)
		clone.CheckpointProof = map[string]string{
			id: hex.EncodeToString(ed25519.Sign(keys[id].PrivateKey, snapshotCheckpointSignBytes(clone))),
		}
		proof := snapshotProofFromSnapshot(id, clone)
		if _, ok := n.recordSnapshotProof(proof); !ok {
			t.Fatalf("expected proof from %s to be accepted", id)
		}
	}

	after := n.snapshotSessionSnapshot()
	if after.CandidateHeight != 96 {
		t.Fatalf("expected proof quorum to promote snapshot candidate height, got=%d", after.CandidateHeight)
	}
	if target, ok := n.snapshotSessionFrozenTarget(n.Blockchain.Height()); !ok || target != 96 {
		t.Fatalf("expected frozen target to use promoted candidate, target=%d ok=%t", target, ok)
	}
}

func TestRuntimeStatusWaitsForNetworkValidatorSetSampleWhenTransportReady(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	n := makeStrictActivationNode(50)
	n.Host = host

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if status.WaitReason != "waiting_network_validator_set_sample" {
		t.Fatalf("expected wait reason waiting_network_validator_set_sample, got=%s", status.WaitReason)
	}
	if status.ConsensusMode != "observer" {
		t.Fatalf("expected observer mode while startup sample is missing, got=%s", status.ConsensusMode)
	}
	if status.VoteEnabled || status.ProposeEnabled {
		t.Fatalf("expected vote/propose disabled while startup sample is missing, vote=%t propose=%t", status.VoteEnabled, status.ProposeEnabled)
	}
	if status.Role != "full" || status.IsValidator {
		t.Fatalf("expected effective observer role while startup sample is missing, got role=%s is_validator=%t", status.Role, status.IsValidator)
	}
}

func TestRuntimeStatusKeepsCommittedValidatorEnabledWithoutStartupSample(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(50, hash)
	n.Host = host
	n.DataDir = t.TempDir()
	now := time.Now()
	for _, id := range []string{"A", "B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			ID:                 id,
			ReportedHeight:     50,
			FinalizedHeight:    50,
			ExecEpoch:          51,
			ValidatorSetHeight: 51,
			ValidatorSetHash:   hash,
			LastSeen:           now,
			Active:             true,
		}
	}
	n.committedHeight = 50
	n.lastCommitHeight = n.committedHeight
	n.setValidatorStartupCheckStatus(true, 51, hash, hash, "startup_validator_set_ok")

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if status.WaitReason == "waiting_network_validator_set_sample" {
		t.Fatalf("committed steady-state validator must not wait on startup network sample")
	}
	if status.ConsensusMode != "validator" || !status.VoteEnabled || !status.ProposeEnabled {
		t.Fatalf("expected committed validator to remain enabled, status=%+v", status)
	}
	if status.Role != "validator" || !status.IsValidator {
		t.Fatalf("expected validator role to remain effective, got role=%s is_validator=%t", status.Role, status.IsValidator)
	}
}

func newStartupSampleNode(localHeight uint64, hash string) *Node {
	validators := []string{"A", "B", "C", "D"}
	return &Node{
		ID:                    "A",
		Role:                  "validator",
		Blockchain:            &Blockchain{Blocks: []Block{{ID: localHeight, ValidatorSetHash: hash, NextValidatorSetHash: hash}}},
		ValidatorKey:          strictActivationTestValidatorKey(1, "A"),
		GenesisValidators:     append([]string{}, validators...),
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: map[string]time.Time{},
		validatorRejoin:       make(map[string]ValidatorRejoinState),
		frozenValidatorsByHeight: map[uint64][]string{
			localHeight:     append([]string{}, validators...),
			localHeight + 1: append([]string{}, validators...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			localHeight:     hash,
			localHeight + 1: hash,
		},
		peerRole:        make(map[string]string),
		peerHelloOK:     make(map[string]bool),
		peerToValidator: make(map[string]string),
		peerSetHash:     make(map[string]string),
		peerAckHeight:   make(map[string]uint64),
	}
}

func TestStartupNetworkValidatorSetSampleStatusIgnoresSelfOnlySample(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(956, hash)
	n.DataDir = t.TempDir()
	n.Host = host
	n.validatorStatus["A"] = &ValidatorStatus{
		ReportedHeight:     956,
		FinalizedHeight:    956,
		ExecEpoch:          957,
		ValidatorSetHeight: 957,
		ValidatorSetHash:   hash,
		LastSeen:           time.Now(),
		Active:             true,
	}

	ready, reason, networkHeight, votes, networkHash := n.startupNetworkValidatorSetSampleStatus(956)
	if ready {
		t.Fatalf("expected self-only startup sample to be rejected")
	}
	if reason != "waiting_network_validator_set_sample" {
		t.Fatalf("unexpected reason: got=%s", reason)
	}
	if networkHeight != 0 || votes != 0 || networkHash != "" {
		t.Fatalf("expected no remote-backed sample, got height=%d votes=%d hash=%q", networkHeight, votes, networkHash)
	}
}

func TestStartupNetworkValidatorSetSampleStatusUsesValidatedRemotePeerSample(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(956, hash)
	n.DataDir = t.TempDir()
	n.Host = host
	n.peerStateMu.Lock()
	n.peerRole["peerB"] = "validator"
	n.peerHelloOK["peerB"] = true
	n.peerToValidator["peerB"] = "B"
	n.peerSetHash["peerB"] = hash
	n.peerAckHeight["peerB"] = 994
	n.peerStateMu.Unlock()

	ready, reason, networkHeight, votes, networkHash := n.startupNetworkValidatorSetSampleStatus(956)
	if !ready {
		t.Fatalf("expected validated remote startup sample to be accepted, reason=%s", reason)
	}
	if reason != "network_validator_set_sample" {
		t.Fatalf("unexpected reason: got=%s", reason)
	}
	if networkHeight != 994 || votes != 1 || networkHash != hash {
		t.Fatalf("unexpected remote sample: height=%d votes=%d hash=%q", networkHeight, votes, networkHash)
	}
}

func TestRuntimeStatusRemainsSyncingWhenRemoteSampleIsAhead(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()
	remoteHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create remote host: %v", err)
	}
	defer remoteHost.Close()
	if err := localHost.Connect(context.Background(), peer.AddrInfo{ID: remoteHost.ID(), Addrs: remoteHost.Addrs()}); err != nil {
		t.Fatalf("failed to connect hosts: %v", err)
	}

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(956, hash)
	n.DataDir = t.TempDir()
	n.Host = localHost
	peerID := remoteHost.ID().String()
	n.peerStateMu.Lock()
	n.peerRole[peerID] = "validator"
	n.peerHelloOK[peerID] = true
	n.peerToValidator[peerID] = "B"
	n.peerSetHash[peerID] = hash
	n.peerAckHeight[peerID] = 994
	n.peerStateMu.Unlock()

	ready, reason := n.syncReadyForConsensus(0)
	if ready {
		t.Fatalf("expected sync readiness to stay blocked while remote target is ahead")
	}
	if reason != "lagging_local_956_target_994" {
		t.Fatalf("unexpected lag reason: got=%q", reason)
	}

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if status.WaitReason != "syncing" {
		t.Fatalf("expected syncing wait reason, got=%s", status.WaitReason)
	}
	if status.ConsensusMode != "observer" || status.VoteEnabled || status.ProposeEnabled {
		t.Fatalf("expected observer mode while lagging, status=%+v", status)
	}
	if status.SyncComplete {
		t.Fatalf("expected sync to remain incomplete while remote target is ahead")
	}
	if status.GossipRealtime {
		t.Fatalf("expected realtime gossip pipeline to remain disabled while lagging")
	}
}

func TestSyncReadyForConsensusUsesFinalizedHeightForRemoteTarget(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()
	remoteHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create remote host: %v", err)
	}
	defer remoteHost.Close()
	if err := localHost.Connect(context.Background(), peer.AddrInfo{ID: remoteHost.ID(), Addrs: remoteHost.Addrs()}); err != nil {
		t.Fatalf("failed to connect hosts: %v", err)
	}

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(956, hash)
	n.DataDir = t.TempDir()
	n.Host = localHost
	peerID := remoteHost.ID().String()
	n.peerStateMu.Lock()
	n.peerRole[peerID] = "validator"
	n.peerHelloOK[peerID] = true
	n.peerToValidator[peerID] = "B"
	n.peerSetHash[peerID] = hash
	n.peerAckHeight[peerID] = 994
	n.peerStateMu.Unlock()
	n.Consensus = NewConsensusState(957)
	n.Consensus.mu.Lock()
	n.Consensus.Syncing = true
	n.Consensus.SyncTarget = 994
	n.Consensus.mu.Unlock()
	n.commitMu.Lock()
	n.finalizedHeight = 994
	n.commitMu.Unlock()

	ready, reason := n.syncReadyForConsensus(957)
	if !ready {
		t.Fatalf("expected finalized height to satisfy sync readiness, reason=%q", reason)
	}
}

func TestRuntimeStatusAllowsGenesisBootstrapWithoutNetworkValidatorSetSample(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	n := makeStrictActivationNode(0)
	empty := NewBlockchain()
	n.Blockchain = &empty
	n.Host = host

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if status.WaitReason == "waiting_network_validator_set_sample" {
		t.Fatalf("expected genesis bootstrap to bypass network sample wait")
	}
	if status.ConsensusMode != "validator" {
		t.Fatalf("expected validator mode for genesis bootstrap, got=%s", status.ConsensusMode)
	}
	if !status.VoteEnabled || !status.ProposeEnabled {
		t.Fatalf("expected vote/propose enabled for genesis bootstrap, vote=%t propose=%t", status.VoteEnabled, status.ProposeEnabled)
	}
}

func TestRuntimeStatusReportsRealtimeGossipPipelineWhenSynced(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()
	remoteHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create remote host: %v", err)
	}
	defer remoteHost.Close()
	if err := localHost.Connect(context.Background(), peer.AddrInfo{ID: remoteHost.ID(), Addrs: remoteHost.Addrs()}); err != nil {
		t.Fatalf("failed to connect hosts: %v", err)
	}

	ps, err := pubsub.NewGossipSub(context.Background(), localHost)
	if err != nil {
		t.Fatalf("failed to create local pubsub: %v", err)
	}

	hash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := newStartupSampleNode(994, hash)
	n.DataDir = t.TempDir()
	n.Host = localHost
	n.PubSub = ps
	if err := n.initPubSubTopics(); err != nil {
		t.Fatalf("failed to initialize pubsub topics: %v", err)
	}

	peerID := remoteHost.ID().String()
	n.peerStateMu.Lock()
	n.peerRole[peerID] = "validator"
	n.peerHelloOK[peerID] = true
	n.peerToValidator[peerID] = "B"
	n.peerSetHash[peerID] = hash
	n.peerAckHeight[peerID] = 994
	n.peerStateMu.Unlock()

	status := n.runtimeStatusSnapshot()
	if !status.GossipRealtime {
		t.Fatalf("expected realtime gossip pipeline to be active, status=%+v", status)
	}
	if status.GossipPipeline != "new_block_gossip_validate_apply" {
		t.Fatalf("unexpected gossip pipeline: got=%q", status.GossipPipeline)
	}
	if !status.BlockGossipActive || !status.TxGossipActive || !status.ValidatorGossipActive {
		t.Fatalf("expected all gossip transports active, status=%+v", status)
	}
	if status.SyncStage != "realtime_gossip" || status.SyncMode != "gossip" || status.SyncAction != "gossip_validate_apply" {
		t.Fatalf("unexpected realtime gossip sync snapshot: stage=%q mode=%q action=%q", status.SyncStage, status.SyncMode, status.SyncAction)
	}
}

func TestRecordSyncPeerInvalidProofAvoidsProviderWithoutTransportQuarantine(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	SyncSnapshotInvalidProofQuarantineAfter = 2
	n := makeStrictActivationNode(10)
	peerID := "peer-bad-proof"
	n.peerStateMu.Lock()
	if n.quarantineUntil == nil {
		n.quarantineUntil = make(map[string]time.Time)
	}
	if n.peerDialFailures == nil {
		n.peerDialFailures = make(map[string]int)
	}
	if n.peerDialNext == nil {
		n.peerDialNext = make(map[string]time.Time)
	}
	n.peerStateMu.Unlock()
	n.recordSyncPeerInvalidProof(peerID)
	if n.isPeerQuarantined(peerID) {
		t.Fatalf("peer should not be quarantined after first invalid proof")
	}
	n.recordSyncPeerInvalidProof(peerID)
	if n.isPeerQuarantined(peerID) {
		t.Fatalf("peer should not be transport-quarantined by snapshot proof score")
	}
	if class := n.syncPeerReputationClass(peerID); class != "avoid" {
		t.Fatalf("expected snapshot provider to be avoided, got class=%q", class)
	}
}

func TestSnapshotAutohealIsolation(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	session := n.startSnapshotSession(170, "large_lag")
	if _, accepted, _ := n.updateSnapshotSessionVote(SnapshotVote{
		ValidatorID:      "A",
		Height:           session.CheckpointHeight,
		SnapshotHash:     "snap-hash",
		StateRoot:        "state-root",
		ValidatorSetHash: "vset-hash",
	}); !accepted {
		t.Fatalf("expected snapshot session vote accepted")
	}
	if len(n.snapshotSessionSnapshot().Votes) != 1 {
		t.Fatalf("expected one session vote recorded")
	}
	n.snapshotTrustMu.Lock()
	if len(n.snapshotVotes) != 0 {
		n.snapshotTrustMu.Unlock()
		t.Fatalf("expected autoheal snapshot vote cache to remain untouched by session votes")
	}
	n.snapshotTrustMu.Unlock()

	plainSnapshot := &StateSnapshot{
		Version:            SnapshotVersion,
		Height:             50,
		BlockHash:          "block_hash_50",
		StateRoot:          "state_root_50",
		LedgerHash:         HashLedger(n.Ledger),
		GenesisHash:        GenesisHash,
		Ledger:             n.Ledger,
		Validators:         map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHeight: 50,
	}
	plainSnapshot.ValidatorSetHash = ValidatorSetHash([]string{"A", "B", "C", "D"})
	populateSnapshotDerivedFields(plainSnapshot)
	if votes, _ := n.recordSnapshotVote("A", plainSnapshot); votes != 1 {
		t.Fatalf("expected one autoheal vote entry, got=%d", votes)
	}
	if len(n.snapshotSessionSnapshot().Votes) != 1 {
		t.Fatalf("expected session vote cache unchanged by autoheal vote path")
	}
}

func TestSnapshotSafetyLockBlocksTransitions(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	DeterministicValidatorSelection = true
	ValidatorSetActivationModelV2Height = 1
	prevCommit := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	SyncCheckpointIntervalBlocks = 8
	t.Cleanup(func() { ValidatorSetCommitmentV2Height = prevCommit })

	n := makeStrictActivationNode(8)
	n.epochValidators[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorsByHeight[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorHashByHeight[8] = ValidatorSetHash([]string{"A", "B", "C", "D"})
	n.pendingValidators = map[string]uint64{"F": 1}
	n.pendingValidatorRemovals = make(map[string]uint64)
	n.Consensus = &ConsensusState{Syncing: false}

	expectedNextHash := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	now := time.Now()
	for _, id := range []string{"B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			FinalizedHeight:    8,
			ReportedHeight:     8,
			ExecEpoch:          9,
			ValidatorSetHeight: 9,
			ValidatorSetHash:   expectedNextHash,
		}
	}

	n.startSnapshotSession(200, "large_lag")
	n.applyScheduledValidatorUpdates(8)
	if containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected transition apply deferred while snapshot session is active")
	}
	if _, ok := n.pendingValidators["F"]; !ok {
		t.Fatalf("expected pending add retained while snapshot session is active")
	}
	if tracker, ok := n.onboardingTrackerSnapshot("F"); !ok {
		t.Fatalf("expected onboarding tracker entry for F while deferred")
	} else {
		if tracker.State != OnboardingStateAwaitingSync {
			t.Fatalf("expected awaiting_sync tracker state, got=%s", tracker.State)
		}
		if tracker.LastReason != "snapshot_session_active" {
			t.Fatalf("expected snapshot_session_active blocker reason, got=%s", tracker.LastReason)
		}
	}

	n.closeSnapshotSession(false, "manual_test_close")
	n.applyScheduledValidatorUpdates(8)
	if !containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected transition apply to resume after snapshot session closes")
	}
	if _, ok := n.pendingValidators["F"]; !ok {
		t.Fatalf("expected pending add retained until chain commit applies transition")
	}
}

func TestSnapshotSafetyLockBlocksRemovalTransitions(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	DeterministicValidatorSelection = true
	ValidatorSetActivationModelV2Height = 1
	prevCommit := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	SyncCheckpointIntervalBlocks = 8
	t.Cleanup(func() { ValidatorSetCommitmentV2Height = prevCommit })

	n := makeStrictActivationNode(8)
	n.epochValidators[8] = []string{"A", "B", "C", "D", "F"}
	n.frozenValidatorsByHeight[8] = []string{"A", "B", "C", "D", "F"}
	n.frozenValidatorHashByHeight[8] = ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	n.pendingValidators = make(map[string]uint64)
	n.pendingValidatorRemovals = map[string]uint64{"F": 1}
	n.Consensus = &ConsensusState{Syncing: false}

	expectedNextHash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()
	for _, id := range []string{"B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			FinalizedHeight:    8,
			ReportedHeight:     8,
			ExecEpoch:          9,
			ValidatorSetHeight: 9,
			ValidatorSetHash:   expectedNextHash,
		}
	}

	n.startSnapshotSession(200, "large_lag")
	n.applyScheduledValidatorUpdates(8)
	if !containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected removal transition deferred while snapshot session is active")
	}
	if _, ok := n.pendingValidatorRemovals["F"]; !ok {
		t.Fatalf("expected pending removal retained while snapshot session is active")
	}

	n.closeSnapshotSession(false, "manual_test_close")
	n.applyScheduledValidatorUpdates(8)
	if containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected pending removal applied after snapshot session closes")
	}
	if _, ok := n.pendingValidatorRemovals["F"]; !ok {
		t.Fatalf("expected pending removal retained until chain commit applies transition")
	}
}

func TestSyncStateMachineNoIllegalTransition(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	if snapshotStageTransitionAllowed(SnapshotSyncStageDetectLag, SnapshotSyncStageApplySnapshot) {
		t.Fatalf("expected detect_lag -> apply_snapshot to be illegal")
	}
	if !snapshotStageTransitionAllowed(SnapshotSyncStageCollectProofs, SnapshotSyncStageVerifyQuorum) {
		t.Fatalf("expected collect_proofs -> verify_quorum to be legal")
	}

	n := makeStrictActivationNode(50)
	n.startSnapshotSession(170, "large_lag")

	n.setSnapshotSessionStage(SnapshotSyncStageApplySnapshot)
	if got := n.snapshotSessionSnapshot().Stage; got != SnapshotSyncStageDetectLag {
		t.Fatalf("expected illegal stage jump ignored, got=%s", got)
	}

	n.setSnapshotSessionStage(SnapshotSyncStageFreezeAnchor)
	n.setSnapshotSessionStage(SnapshotSyncStageCollectProofs)
	n.setSnapshotSessionStage(SnapshotSyncStageVerifyQuorum)
	n.setSnapshotSessionStage(SnapshotSyncStageApplySnapshot)
	n.setSnapshotSessionStage(SnapshotSyncStageDeltaReplay)
	n.setSnapshotSessionStage(SnapshotSyncStageSyncComplete)

	if got := n.snapshotSessionSnapshot().Stage; got != SnapshotSyncStageSyncComplete {
		t.Fatalf("expected valid state progression to sync_complete, got=%s", got)
	}

	n.setSnapshotSessionStage(SnapshotSyncStageApplySnapshot)
	if got := n.snapshotSessionSnapshot().Stage; got != SnapshotSyncStageSyncComplete {
		t.Fatalf("expected illegal post-complete jump ignored, got=%s", got)
	}
}
