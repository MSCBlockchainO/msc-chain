package main

import (
	"testing"

	peer "github.com/libp2p/go-libp2p/core/peer"
)

func TestSyncCatchupStageThresholds(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldFast := SyncFastBlockSyncMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldThreshold := SyncSnapshotCatchupThresholdBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	oldDeltaState := SyncDeltaStateSyncEnabled
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncFastBlockSyncMaxBlocks = oldFast
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotCatchupThresholdBlocks = oldThreshold
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
		SyncDeltaStateSyncEnabled = oldDeltaState
	})
	SyncDirectGossipMaxBlocks = 128
	SyncFastBlockSyncMaxBlocks = 256
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotCatchupThresholdBlocks = 2000
	SyncSnapshotDeltaMaxBlocks = 10000000
	SyncDeltaStateSyncEnabled = true

	cases := []struct {
		local  uint64
		target uint64
		want   string
	}{
		{100, 101, "direct_gossip"},
		{100, 228, "direct_gossip"},
		{100, 229, "range_fetch"},
		{100, 356, "range_fetch"},
		{100, 357, "delta_replay"},
		{100, 2099, "delta_replay"},
		{100, 2100, "snapshot_delta"},
		{100, 10000100, "snapshot_delta"},
		{100, 10000101, "state_bootstrap"},
		{100, 100000100, "state_bootstrap"},
	}
	for _, tc := range cases {
		if got := syncCatchupStage(tc.local, tc.target); got != tc.want {
			t.Fatalf("syncCatchupStage(%d,%d)=%q want=%q", tc.local, tc.target, got, tc.want)
		}
	}
}

func TestSyncCatchupStageSkipsDeltaWhenDeltaStateSyncDisabled(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldFast := SyncFastBlockSyncMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldThreshold := SyncSnapshotCatchupThresholdBlocks
	oldDeltaState := SyncDeltaStateSyncEnabled
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncFastBlockSyncMaxBlocks = oldFast
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotCatchupThresholdBlocks = oldThreshold
		SyncDeltaStateSyncEnabled = oldDeltaState
	})
	SyncDirectGossipMaxBlocks = 128
	SyncFastBlockSyncMaxBlocks = 256
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotCatchupThresholdBlocks = 2000
	SyncDeltaStateSyncEnabled = false

	if got := syncCatchupStage(100, 600); got != "state_bootstrap" {
		t.Fatalf("expected delta disabled to choose state_bootstrap, got %q", got)
	}
}

func TestSyncReasonPreemptsInFlightStallWatchdog(t *testing.T) {
	preemptReasons := []string{
		"sync_stall_watchdog",
		"queue_stall",
		"parent_mismatch",
		"prev_hash_mismatch",
		"missing_block_recovery",
		"no_progress",
	}
	for _, reason := range preemptReasons {
		if !syncReasonPreemptsInFlight(reason) {
			t.Fatalf("expected reason %q to preempt stale sync", reason)
		}
	}
	if syncReasonPreemptsInFlight("routine_peer_sync") {
		t.Fatalf("routine sync must not preempt an active in-flight sync")
	}
}

func TestSyncCatchupModeCompatibilityMapping(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldFast := SyncFastBlockSyncMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldThreshold := SyncSnapshotCatchupThresholdBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncFastBlockSyncMaxBlocks = oldFast
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotCatchupThresholdBlocks = oldThreshold
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncDirectGossipMaxBlocks = 128
	SyncFastBlockSyncMaxBlocks = 256
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotCatchupThresholdBlocks = 2000
	SyncSnapshotDeltaMaxBlocks = 10000000

	if got := syncCatchupMode(100, 110); got != "blocks" {
		t.Fatalf("expected blocks compatibility mode for direct_gossip, got=%q", got)
	}
	if got := syncCatchupMode(100, 1000); got != "blocks" {
		t.Fatalf("expected blocks compatibility mode for range_fetch, got=%q", got)
	}
	if got := syncCatchupMode(100, 60000); got != "snapshot" {
		t.Fatalf("expected snapshot compatibility mode for snapshot_delta, got=%q", got)
	}
	if got := syncCatchupMode(100, 11000000); got != "warp" {
		t.Fatalf("expected warp compatibility mode for state_bootstrap, got=%q", got)
	}
}

func TestSelectSyncTargetAllowsLaggingSelfCatchupOneBlock(t *testing.T) {
	got := selectSyncTargetHeight(
		100, // local
		100, // strict quorum height is held down by this lagging node
		true,
		101, // two connected peers are already one block ahead
		2,
		3,
	)
	if got != 101 {
		t.Fatalf("expected lagging node to catch up one block with required-1 observed votes, got=%d", got)
	}
}

func TestSelectSyncTargetAllowsLaggingSelfCatchupHugeGap(t *testing.T) {
	got := selectSyncTargetHeight(
		100,
		100,
		true,
		100000100,
		2,
		3,
	)
	if got != 100000100 {
		t.Fatalf("expected lagging node to catch up huge gap with required-1 observed votes, got=%d", got)
	}
}

func TestSelectSyncTargetRejectsSingleMinorityObservedVote(t *testing.T) {
	got := selectSyncTargetHeight(
		100,
		100,
		true,
		101,
		1,
		3,
	)
	if got != 0 {
		t.Fatalf("expected one minority observed vote to be ignored, got=%d", got)
	}
}

func TestSyncRealtimeGossipCompatibilityMapping(t *testing.T) {
	if got := normalizeSyncStage("gossip"); got != "realtime_gossip" {
		t.Fatalf("expected gossip alias to normalize to realtime_gossip, got=%q", got)
	}
	if got := syncModeForStage("realtime_gossip"); got != "gossip" {
		t.Fatalf("expected realtime_gossip stage to map to gossip mode, got=%q", got)
	}
	if got := syncGossipAction("realtime_gossip"); got != "gossip_validate_apply" {
		t.Fatalf("unexpected realtime gossip action: got=%q", got)
	}
}

func TestSyncActionSnapshotReportsStateBootstrapStage(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncDirectGossipMaxBlocks = 128
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotDeltaMaxBlocks = 10000000

	n := &Node{}
	stage, mode, lag, action := n.syncActionSnapshot(10, 11000020, true)
	if stage != "state_bootstrap" {
		t.Fatalf("expected state_bootstrap stage, got=%q", stage)
	}
	if mode != "warp" {
		t.Fatalf("expected warp compatibility mode, got=%q", mode)
	}
	if lag != 11000010 {
		t.Fatalf("unexpected lag: got=%d want=%d", lag, uint64(11000010))
	}
	if action != "state_snapshot_bootstrap" {
		t.Fatalf("unexpected action: got=%q want=state_snapshot_bootstrap", action)
	}
}

func TestSyncActionSnapshotReportsStateBootstrapForHundredMillionGap(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncDirectGossipMaxBlocks = 128
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotDeltaMaxBlocks = 10000000

	n := &Node{}
	stage, mode, lag, action := n.syncActionSnapshot(100, 100000100, true)
	if stage != "state_bootstrap" {
		t.Fatalf("expected state_bootstrap stage for 100M gap, got=%q", stage)
	}
	if mode != "warp" {
		t.Fatalf("expected warp compatibility mode for 100M gap, got=%q", mode)
	}
	if lag != 100000000 {
		t.Fatalf("unexpected lag: got=%d want=100000000", lag)
	}
	if action != "state_snapshot_bootstrap" {
		t.Fatalf("unexpected action: got=%q want=state_snapshot_bootstrap", action)
	}
}

func TestSyncActionSnapshotReportsSnapshotDeltaStage(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncDirectGossipMaxBlocks = 128
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotDeltaMaxBlocks = 10000000

	n := &Node{}
	stage, mode, lag, action := n.syncActionSnapshot(0, 3000000, true)
	if stage != "snapshot_delta" {
		t.Fatalf("expected snapshot_delta stage, got=%q", stage)
	}
	if mode != "snapshot" {
		t.Fatalf("expected snapshot compatibility mode, got=%q", mode)
	}
	if lag != 3000000 {
		t.Fatalf("unexpected lag: got=%d want=%d", lag, uint64(3000000))
	}
	if action != "snapshot_delta" {
		t.Fatalf("unexpected action: got=%q want=snapshot_delta", action)
	}
}

func TestSyncActionSnapshotReportsDirectGossipStage(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
	})
	SyncDirectGossipMaxBlocks = 128

	n := &Node{}
	stage, mode, lag, action := n.syncActionSnapshot(100, 120, true)
	if stage != "direct_gossip" {
		t.Fatalf("expected direct_gossip stage, got=%q", stage)
	}
	if mode != "blocks" {
		t.Fatalf("expected blocks compatibility mode, got=%q", mode)
	}
	if lag != 20 {
		t.Fatalf("unexpected lag: got=%d want=20", lag)
	}
	if action != "direct_gossip" {
		t.Fatalf("unexpected action: got=%q want=direct_gossip", action)
	}
}

func TestSyncActionSnapshotReportsNearTipLagWhenConsensusNotPaused(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
	})
	SyncDirectGossipMaxBlocks = 128

	n := &Node{}
	stage, mode, lag, action := n.syncActionSnapshot(9635, 9641, false)
	if stage != "direct_gossip" {
		t.Fatalf("expected near-tip lag to report direct_gossip stage, got=%q", stage)
	}
	if mode != "blocks" {
		t.Fatalf("expected blocks mode, got=%q", mode)
	}
	if lag != 6 {
		t.Fatalf("unexpected lag: got=%d want=6", lag)
	}
	if action != "direct_gossip" {
		t.Fatalf("unexpected action: got=%q want=direct_gossip", action)
	}
}

func TestSyncCatchupPlanSelectsExecutionMethodAndBatch(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldFast := SyncFastBlockSyncMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldThreshold := SyncSnapshotCatchupThresholdBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncFastBlockSyncMaxBlocks = oldFast
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotCatchupThresholdBlocks = oldThreshold
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncDirectGossipMaxBlocks = 128
	SyncFastBlockSyncMaxBlocks = 256
	SyncRangeFetchMaxBlocks = 50000
	SyncSnapshotCatchupThresholdBlocks = 2000
	SyncSnapshotDeltaMaxBlocks = 10000000

	cases := []struct {
		name        string
		local       uint64
		target      uint64
		stage       string
		mode        string
		action      string
		batch       uint64
		blockRange  bool
		deltaReplay bool
		snapshot    bool
	}{
		{"one_block", 100, 101, "direct_gossip", "blocks", "direct_gossip", 128, true, false, false},
		{"range", 100, 356, "range_fetch", "blocks", "range_fetch", 50000, true, false, false},
		{"delta", 100, 357, "delta_replay", "blocks", "delta_replay", 1024, false, true, false},
		{"snapshot", 100, 2100, "snapshot_delta", "snapshot", "snapshot_delta", 0, false, false, true},
		{"huge", 100, 100000100, "state_bootstrap", "warp", "state_snapshot_bootstrap", 0, false, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := syncCatchupPlanForLag(tc.local, tc.target)
			if plan.Stage != tc.stage || plan.Mode != tc.mode || plan.Action != tc.action {
				t.Fatalf("unexpected plan: stage=%q mode=%q action=%q", plan.Stage, plan.Mode, plan.Action)
			}
			if plan.BatchMax != tc.batch {
				t.Fatalf("unexpected batch max: got=%d want=%d", plan.BatchMax, tc.batch)
			}
			if plan.BlockRange != tc.blockRange || plan.DeltaReplay != tc.deltaReplay || plan.Snapshot != tc.snapshot {
				t.Fatalf("unexpected method flags: block=%t delta=%t snapshot=%t", plan.BlockRange, plan.DeltaReplay, plan.Snapshot)
			}
		})
	}
}

func TestUsableHeadFastBootstrapRoleGate(t *testing.T) {
	oldEnabled := SyncUsableHeadFastBootstrapEnabled
	oldRoles := append([]string{}, SyncUsableHeadRoles...)
	oldMinGap := SyncUsableHeadMinGapBlocks
	oldCheckpoint := SyncUsableHeadRequireCheckpointProof
	oldBackground := SyncUsableHeadBackgroundHistory
	oldRecentWindow := SyncUsableHeadRecentReplayWindowBlocks
	oldTargetSeconds := SyncUsableHeadTargetSeconds
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncUsableHeadFastBootstrapEnabled = oldEnabled
		SyncUsableHeadRoles = oldRoles
		SyncUsableHeadMinGapBlocks = oldMinGap
		SyncUsableHeadRequireCheckpointProof = oldCheckpoint
		SyncUsableHeadBackgroundHistory = oldBackground
		SyncUsableHeadRecentReplayWindowBlocks = oldRecentWindow
		SyncUsableHeadTargetSeconds = oldTargetSeconds
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncUsableHeadFastBootstrapEnabled = true
	SyncUsableHeadRoles = []string{"full", "archive"}
	SyncUsableHeadMinGapBlocks = 2000
	SyncUsableHeadRequireCheckpointProof = true
	SyncUsableHeadBackgroundHistory = true
	SyncUsableHeadRecentReplayWindowBlocks = 2048
	SyncUsableHeadTargetSeconds = 3
	SyncSnapshotDeltaMaxBlocks = 10000000

	fullPlan := syncCatchupPlanForRole(100, 100000100, "full", false)
	if !fullPlan.UsableHead || !fullPlan.StateBootstrap || fullPlan.HeadStage != "checkpoint_verify" {
		t.Fatalf("expected full node usable-head state bootstrap plan, got %+v", fullPlan)
	}
	if !fullPlan.RequireCheckpointProof || !fullPlan.BackgroundHistory || fullPlan.RecentReplayWindow != 2048 || fullPlan.TargetSeconds != 3 {
		t.Fatalf("unexpected usable-head policy in plan: %+v", fullPlan)
	}

	archivePlan := syncCatchupPlanForRole(100, 100000100, "full", true)
	if !archivePlan.UsableHead {
		t.Fatalf("expected archive-mode full node to be usable-head eligible")
	}

	validatorPlan := syncCatchupPlanForRole(100, 100000100, "validator", false)
	if validatorPlan.UsableHead {
		t.Fatalf("validator must not be usable-head eligible: %+v", validatorPlan)
	}

	smallGapPlan := syncCatchupPlanForRole(100, 101, "full", false)
	if smallGapPlan.UsableHead || smallGapPlan.Stage != "direct_gossip" {
		t.Fatalf("small gaps must keep normal block sync, got %+v", smallGapPlan)
	}
}

func TestUsableHeadRuntimeStatusRequiresAllowedRole(t *testing.T) {
	oldEnabled := SyncUsableHeadFastBootstrapEnabled
	oldRoles := append([]string{}, SyncUsableHeadRoles...)
	oldHistoryMode := SyncHistoryMode
	t.Cleanup(func() {
		SyncUsableHeadFastBootstrapEnabled = oldEnabled
		SyncUsableHeadRoles = oldRoles
		SyncHistoryMode = oldHistoryMode
	})
	SyncUsableHeadFastBootstrapEnabled = true
	SyncUsableHeadRoles = []string{"full", "archive"}
	SyncHistoryMode = SyncHistoryModeBackground

	fullNode := &Node{Role: "full"}
	fullStatus := RuntimeStatusSnapshot{
		Role:                  "full",
		Height:                100000100,
		SyncTarget:            100000100,
		SyncStage:             "state_bootstrap",
		SyncAction:            "state_snapshot_applied",
		SnapshotHeightApplied: 100000100,
		SyncComplete:          true,
	}
	fullNode.applyUsableHeadRuntimeStatus(&fullStatus, "full", 100000100)
	if !fullStatus.UsableHead || !fullStatus.HeadSynced {
		t.Fatalf("expected full node usable head after state snapshot apply, got %+v", fullStatus)
	}
	if fullStatus.HistoryBackfillPending {
		t.Fatalf("history should not be pending when target height is reached: %+v", fullStatus)
	}

	validatorNode := &Node{Role: "validator"}
	validatorStatus := fullStatus
	validatorStatus.Role = "validator"
	validatorStatus.UsableHead = false
	validatorStatus.HeadSynced = false
	validatorNode.applyUsableHeadRuntimeStatus(&validatorStatus, "validator", 100000100)
	if validatorStatus.UsableHead {
		t.Fatalf("validator must not be marked usable-head through fast bootstrap: %+v", validatorStatus)
	}
}

func TestSyncActionSnapshotReplansStaleCachedStage(t *testing.T) {
	oldDirect := SyncDirectGossipMaxBlocks
	oldRange := SyncRangeFetchMaxBlocks
	oldSnapshotDelta := SyncSnapshotDeltaMaxBlocks
	t.Cleanup(func() {
		SyncDirectGossipMaxBlocks = oldDirect
		SyncRangeFetchMaxBlocks = oldRange
		SyncSnapshotDeltaMaxBlocks = oldSnapshotDelta
	})
	SyncDirectGossipMaxBlocks = 10
	SyncRangeFetchMaxBlocks = 1000
	SyncSnapshotDeltaMaxBlocks = 10000000

	n := &Node{}
	n.syncStage = "direct_gossip"
	n.syncMode = "blocks"
	n.syncAction = "direct_gossip"

	stage, mode, lag, action := n.syncActionSnapshot(100, 250, true)
	if stage != "range_fetch" {
		t.Fatalf("expected stale direct_gossip cache to replan to range_fetch, got=%q", stage)
	}
	if mode != "blocks" {
		t.Fatalf("expected blocks mode, got=%q", mode)
	}
	if lag != 150 {
		t.Fatalf("unexpected lag: got=%d want=150", lag)
	}
	if action != "range_fetch" {
		t.Fatalf("expected range_fetch action, got=%q", action)
	}
}

func TestSyncStagesUseExplicitBlockRangeFetch(t *testing.T) {
	if !syncStageUsesBlockRangeFetch("direct_gossip") {
		t.Fatalf("expected direct_gossip to use explicit block range fetch")
	}
	if !syncStageUsesBlockRangeFetch("range_fetch") {
		t.Fatalf("expected range_fetch to use explicit block range fetch")
	}
	if syncStageUsesBlockRangeFetch("snapshot_delta") {
		t.Fatalf("snapshot_delta must not use direct block range fetch stage helper")
	}
	if syncStageUsesBlockRangeFetch("state_bootstrap") {
		t.Fatalf("state_bootstrap must not use direct block range fetch stage helper")
	}
	if got := syncBlockRangeAction("direct_gossip"); got != "block_range_fetch" {
		t.Fatalf("unexpected direct_gossip action: got=%q want=block_range_fetch", got)
	}
	if got := syncBlockRangeAction("range_fetch"); got != "block_range_fetch" {
		t.Fatalf("unexpected range_fetch action: got=%q want=block_range_fetch", got)
	}
	if got := syncBlockRangeAction("snapshot_delta"); got != "" {
		t.Fatalf("unexpected snapshot_delta action: got=%q want empty", got)
	}
}

func TestSyncStagesUseExplicitSnapshotTransfer(t *testing.T) {
	if !syncStageUsesSnapshotTransfer("snapshot_delta") {
		t.Fatalf("expected snapshot_delta to use snapshot transfer")
	}
	if !syncStageUsesSnapshotTransfer("state_bootstrap") {
		t.Fatalf("expected state_bootstrap to use snapshot transfer")
	}
	if syncStageUsesSnapshotTransfer("direct_gossip") {
		t.Fatalf("direct_gossip must not use snapshot transfer")
	}
	if syncStageUsesSnapshotTransfer("range_fetch") {
		t.Fatalf("range_fetch must not use snapshot transfer")
	}
	if got := syncSnapshotStageAction("snapshot_delta"); got != "snapshot_delta" {
		t.Fatalf("unexpected snapshot_delta action: got=%q want=snapshot_delta", got)
	}
	if got := syncSnapshotStageAction("state_bootstrap"); got != "state_snapshot_bootstrap" {
		t.Fatalf("unexpected state_bootstrap action: got=%q want=state_snapshot_bootstrap", got)
	}
	if got := syncSnapshotProgressAction("state_bootstrap", "snapshot_fetch_verify"); got != "state_snapshot_bootstrap" {
		t.Fatalf("unexpected state_bootstrap fetch action: got=%q want=state_snapshot_bootstrap", got)
	}
	if got := syncSnapshotProgressAction("state_bootstrap", "snapshot_applied"); got != "state_snapshot_applied" {
		t.Fatalf("unexpected state_bootstrap applied action: got=%q want=state_snapshot_applied", got)
	}
	if got := syncSnapshotProgressAction("snapshot_delta", "snapshot_fetch_verify"); got != "snapshot_fetch_verify" {
		t.Fatalf("unexpected snapshot_delta fetch action: got=%q want=snapshot_fetch_verify", got)
	}
}

func TestSyncPipelineStageSelection(t *testing.T) {
	minPeers := 3
	if got := computeSyncPipelineStage(0, 0, 2, minPeers, false, false, false, false); got != syncPipelinePeerDiscovery {
		t.Fatalf("expected peer_discovery stage, got=%q", got)
	}
	if got := computeSyncPipelineStage(0, 0, 3, minPeers, false, false, false, false); got != syncPipelineHeightSample {
		t.Fatalf("expected height_sampling stage, got=%q", got)
	}
	if got := computeSyncPipelineStage(0, 10, 3, minPeers, false, true, false, false); got != syncPipelineSnapshotSync {
		t.Fatalf("expected snapshot_sync stage, got=%q", got)
	}
	if got := computeSyncPipelineStage(5, 10, 3, minPeers, false, false, false, false); got != syncPipelineBlockCatchup {
		t.Fatalf("expected block_catchup stage, got=%q", got)
	}
	if got := computeSyncPipelineStage(5, 5, 3, minPeers, false, false, false, true); got != syncPipelineLiveConsensus {
		t.Fatalf("expected live_consensus stage, got=%q", got)
	}
}

func TestSnapshotChunkWorkerCountUsesRecommendedBounds(t *testing.T) {
	oldParallel := SyncSnapshotParallelChunks
	t.Cleanup(func() {
		SyncSnapshotParallelChunks = oldParallel
	})

	SyncSnapshotParallelChunks = 0
	if got := snapshotChunkWorkerCount(8192); got != 8 {
		t.Fatalf("expected default worker count 8, got=%d", got)
	}

	SyncSnapshotParallelChunks = 12
	if got := snapshotChunkWorkerCount(8192); got != 12 {
		t.Fatalf("expected explicit worker count 12, got=%d", got)
	}

	SyncSnapshotParallelChunks = 32
	if got := snapshotChunkWorkerCount(8192); got != 16 {
		t.Fatalf("expected clamped worker count 16, got=%d", got)
	}

	SyncSnapshotParallelChunks = 12
	if got := snapshotChunkWorkerCount(3); got != 3 {
		t.Fatalf("expected worker count to clamp to missing chunk count, got=%d", got)
	}
}

func TestSnapshotChunkProviderForIndexDistributesRoundRobin(t *testing.T) {
	providers := []peer.ID{
		peer.ID("peer1"),
		peer.ID("peer2"),
		peer.ID("peer3"),
	}
	cases := []struct {
		idx     uint64
		attempt int
		want    peer.ID
	}{
		{0, 0, peer.ID("peer1")},
		{1, 0, peer.ID("peer2")},
		{2, 0, peer.ID("peer3")},
		{3, 0, peer.ID("peer1")},
		{0, 1, peer.ID("peer2")},
		{0, 2, peer.ID("peer3")},
	}
	for _, tc := range cases {
		got, ok := snapshotChunkProviderForIndex(providers, tc.idx, tc.attempt)
		if !ok {
			t.Fatalf("expected provider for idx=%d attempt=%d", tc.idx, tc.attempt)
		}
		if got != tc.want {
			t.Fatalf("unexpected provider for idx=%d attempt=%d: got=%q want=%q", tc.idx, tc.attempt, got, tc.want)
		}
	}
	if _, ok := snapshotChunkProviderForIndex(providers, 0, len(providers)); ok {
		t.Fatalf("expected out-of-range attempt to fail")
	}
}

func TestDeltaReplayVerifyWorkerCountUsesRecommendedBounds(t *testing.T) {
	oldWorkers := SyncDeltaReplayVerifyWorkers
	t.Cleanup(func() {
		SyncDeltaReplayVerifyWorkers = oldWorkers
	})

	SyncDeltaReplayVerifyWorkers = 0
	if got := deltaReplayVerifyWorkerCount(1000); got != 8 {
		t.Fatalf("expected default delta replay worker count 8, got=%d", got)
	}

	SyncDeltaReplayVerifyWorkers = 12
	if got := deltaReplayVerifyWorkerCount(1000); got != 12 {
		t.Fatalf("expected explicit delta replay worker count 12, got=%d", got)
	}

	SyncDeltaReplayVerifyWorkers = 32
	if got := deltaReplayVerifyWorkerCount(1000); got != 16 {
		t.Fatalf("expected clamped delta replay worker count 16, got=%d", got)
	}

	SyncDeltaReplayVerifyWorkers = 12
	if got := deltaReplayVerifyWorkerCount(3); got != 3 {
		t.Fatalf("expected worker count to clamp to batch size, got=%d", got)
	}
}

func TestPreflightDeltaReplayBatchAcceptsContiguousBlocks(t *testing.T) {
	blocks := []Block{
		{ID: 10, PrevHash: "prev-9", BlockHash: "hash-10"},
		{ID: 11, PrevHash: "hash-10", BlockHash: "hash-11"},
		{ID: 12, PrevHash: "hash-11", BlockHash: "hash-12"},
	}
	if err := preflightDeltaReplayBatch(10, blocks); err != nil {
		t.Fatalf("expected contiguous replay batch to pass preflight, got=%v", err)
	}
}

func TestPreflightDeltaReplayBatchRejectsHeightGap(t *testing.T) {
	blocks := []Block{
		{ID: 10, PrevHash: "prev-9", BlockHash: "hash-10"},
		{ID: 12, PrevHash: "hash-10", BlockHash: "hash-12"},
	}
	if err := preflightDeltaReplayBatch(10, blocks); err == nil {
		t.Fatalf("expected height gap to fail delta replay preflight")
	}
}

func TestPreflightDeltaReplayBatchRejectsPrevHashMismatch(t *testing.T) {
	blocks := []Block{
		{ID: 10, PrevHash: "prev-9", BlockHash: "hash-10"},
		{ID: 11, PrevHash: "wrong-prev", BlockHash: "hash-11"},
	}
	if err := preflightDeltaReplayBatch(10, blocks); err == nil {
		t.Fatalf("expected prev-hash mismatch to fail delta replay preflight")
	}
}
