package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPreferBlockRangeCatchupNearGenesis(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	if node.preferBlockRangeCatchup(0, 188) {
		t.Fatalf("fresh join must not prefer block-range catchup on existing chain")
	}
	if node.preferBlockRangeCatchup(0, 5000) {
		t.Fatalf("did not expect block-range preference for huge fresh-node lag")
	}
	if !node.shouldForceSnapshotForFreshNode(0, 188) {
		t.Fatalf("fresh join must keep trusted snapshot mandatory")
	}
}

func TestPreferBlockRangeCatchupOnRestartWhenLagIsSmall(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	if !node.preferBlockRangeCatchup(98, 169) {
		t.Fatalf("expected restart block-range preference for lag below limit")
	}
	if node.preferBlockRangeCatchup(98, 700) {
		t.Fatalf("did not expect restart block-range preference for lag above limit")
	}
}

func TestSnapshotFailureDegradeDoesNotForceLargeRestartLagToBlockRange(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.syncMu.Lock()
	node.syncSnapshotSessionFailures = syncSnapshotSessionFailureDegradeThreshold()
	node.syncSnapshotSessionDegradedUntil = time.Now().Add(time.Minute)
	node.syncMu.Unlock()

	if node.preferBlockRangeCatchup(98, 700) {
		t.Fatalf("large restart lag must keep snapshot catch-up despite previous snapshot failures")
	}
	if !syncPromoteDeltaReplayToSnapshot(98, 700-98) {
		t.Fatalf("expected large restart lag to promote delta replay to snapshot catch-up")
	}
}

func TestSnapshotStageTransitionAllowsCheckAnchor(t *testing.T) {
	if !snapshotStageTransitionAllowed(SnapshotSyncStageFreezeAnchor, SnapshotSyncStageCheckAnchor) {
		t.Fatalf("freeze anchor should transition to check anchor")
	}
	if !snapshotStageTransitionAllowed(SnapshotSyncStageCheckAnchor, SnapshotSyncStageCollectProofs) {
		t.Fatalf("check anchor should transition to collect proofs")
	}
	if snapshotStageTransitionAllowed(SnapshotSyncStageFreezeAnchor, SnapshotSyncStageCollectProofs) {
		t.Fatalf("freeze anchor should not skip check anchor")
	}
}

func TestRotateSnapshotProviderUsesAlternativeConnectedProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	peerA, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create peerA host: %v", err)
	}
	defer peerA.Close()

	peerB, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create peerB host: %v", err)
	}
	defer peerB.Close()

	if err := localHost.Connect(ctx, peer.AddrInfo{ID: peerA.ID(), Addrs: peerA.Addrs()}); err != nil {
		t.Fatalf("failed to connect peerA: %v", err)
	}
	if err := localHost.Connect(ctx, peer.AddrInfo{ID: peerB.ID(), Addrs: peerB.Addrs()}); err != nil {
		t.Fatalf("failed to connect peerB: %v", err)
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Host = localHost
	node.snapshotSessionMu.Lock()
	node.snapshotSession.Active = true
	node.snapshotSession.Stage = SnapshotSyncStageProviderRotate
	node.snapshotSession.FreezeHeight = 188
	node.snapshotSession.CurrentProvider = peerA.ID().String()
	node.snapshotSession.ProviderSet = []string{peerA.ID().String(), peerB.ID().String()}
	node.snapshotSessionMu.Unlock()

	next := node.rotateSnapshotProvider()
	if next != peerB.ID().String() {
		t.Fatalf("unexpected rotated provider: got=%s want=%s", next, peerB.ID().String())
	}
}

func TestSyncCatchupStageUsesDeltaReplayBeforeSnapshot(t *testing.T) {
	oldFast := SyncFastBlockSyncMaxBlocks
	oldThreshold := SyncSnapshotCatchupThresholdBlocks
	t.Cleanup(func() {
		SyncFastBlockSyncMaxBlocks = oldFast
		SyncSnapshotCatchupThresholdBlocks = oldThreshold
	})

	SyncFastBlockSyncMaxBlocks = 256
	SyncSnapshotCatchupThresholdBlocks = 2000

	if got := syncCatchupStage(100, 200); got != "direct_gossip" {
		t.Fatalf("unexpected small-lag stage: got=%s", got)
	}
	if got := syncCatchupStage(100, 400); got != "delta_replay" {
		t.Fatalf("unexpected medium-lag stage: got=%s want=delta_replay", got)
	}
	if got := syncCatchupStage(100, 2500); got != "snapshot_delta" {
		t.Fatalf("unexpected large-lag stage: got=%s want=snapshot_delta", got)
	}
}

func TestPersistSyncResumeStateRoundTrip(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.syncMu.Lock()
	node.syncStage = "delta_replay"
	node.syncAction = "delta_replay"
	node.syncLastObservedHeight = 130
	node.syncResumeTarget = 254
	node.syncMu.Unlock()

	node.persistSyncResumeState("delta_replay", 254, "test_resume")

	state, ok := node.restoreSyncResumeState()
	if !ok || state == nil {
		t.Fatalf("expected persisted sync resume state")
	}
	if state.Stage != "delta_replay" {
		t.Fatalf("unexpected stage: got=%s", state.Stage)
	}
	if state.Strategy != "delta_replay" {
		t.Fatalf("unexpected strategy: got=%s", state.Strategy)
	}
	if state.TargetHeight != 254 {
		t.Fatalf("unexpected target height: got=%d", state.TargetHeight)
	}
	if state.LastApplied != 130 {
		t.Fatalf("unexpected last applied height: got=%d", state.LastApplied)
	}
	if got := node.syncResumeSessionPath(); filepath.Base(got) != "session.json" {
		t.Fatalf("unexpected sync resume path: got=%s", got)
	}
}
