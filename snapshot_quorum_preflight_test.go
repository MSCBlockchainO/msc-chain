package main

import (
	"fmt"
	"testing"
)

func strictCandidateObs(height uint64, hash string, validator string, provider string) strictSnapshotMetaObservation {
	return strictSnapshotMetaObservation{
		Provider:    provider,
		ValidatorID: validator,
		Candidate: &strictSnapshotMetaCandidate{
			Height:                height,
			CheckpointHeight:      snapshotCheckpointHeightFor(height),
			SnapshotHash:          hash,
			StateRoot:             "state-" + hash,
			ValidatorSetHash:      "vset-" + hash,
			ValidatorRegistryHash: "registry-" + hash,
			Validators:            make(map[string]struct{}),
		},
	}
}

func strictCandidateObsWithMeta(height uint64, snapshotHash string, stateRoot string, validatorSetHash string, registryHash string, validator string, provider string) strictSnapshotMetaObservation {
	return strictSnapshotMetaObservation{
		Provider:    provider,
		ValidatorID: validator,
		Candidate: &strictSnapshotMetaCandidate{
			Height:                height,
			CheckpointHeight:      snapshotCheckpointHeightFor(height),
			SnapshotHash:          snapshotHash,
			StateRoot:             stateRoot,
			ValidatorSetHash:      validatorSetHash,
			ValidatorRegistryHash: registryHash,
			Validators:            make(map[string]struct{}),
		},
	}
}

func TestSelectStrictSnapshotMetaCandidateChoosesHighestQuorumReachable(t *testing.T) {
	observations := []strictSnapshotMetaObservation{
		strictCandidateObs(956, "h956", "B", "peer-b"),
		strictCandidateObs(956, "h956", "C", "peer-c"),
		strictCandidateObs(956, "h956", "D", "peer-d"),
		strictCandidateObs(953, "h953", "B", "peer-b-old"),
		strictCandidateObs(953, "h953", "C", "peer-c-old"),
	}

	quorum, best := selectStrictSnapshotMetaCandidate(observations, 3)
	if quorum == nil {
		t.Fatalf("expected quorum candidate")
	}
	if quorum.Height != 956 {
		t.Fatalf("unexpected quorum candidate height: got=%d want=956", quorum.Height)
	}
	if len(quorum.Validators) != 3 {
		t.Fatalf("unexpected quorum candidate validator count: got=%d want=3", len(quorum.Validators))
	}
	if best == nil || best.Height != 956 {
		t.Fatalf("unexpected best candidate height: got=%v", best)
	}
}

func TestSelectStrictSnapshotMetaCandidateKeepsLowerQuorumWhenHigherIsPartial(t *testing.T) {
	observations := []strictSnapshotMetaObservation{
		strictCandidateObs(956, "h956", "B", "peer-b"),
		strictCandidateObs(956, "h956", "C", "peer-c"),
		strictCandidateObs(953, "h953", "B", "peer-b-old"),
		strictCandidateObs(953, "h953", "C", "peer-c-old"),
		strictCandidateObs(953, "h953", "D", "peer-d-old"),
	}

	quorum, best := selectStrictSnapshotMetaCandidate(observations, 3)
	if quorum == nil {
		t.Fatalf("expected quorum candidate")
	}
	if quorum.Height != 953 {
		t.Fatalf("unexpected quorum candidate height: got=%d want=953", quorum.Height)
	}
	if best == nil || best.Height != 956 {
		t.Fatalf("unexpected best partial candidate height: got=%v", best)
	}
}

func TestSelectStrictSnapshotMetaCandidateRequiresMatchingMetadataIdentity(t *testing.T) {
	observations := []strictSnapshotMetaObservation{
		strictCandidateObsWithMeta(995, "snap-a", "state-a", "vset-a", "reg-a", "B", "peer-b"),
		strictCandidateObsWithMeta(995, "snap-a", "state-a", "vset-a", "reg-a", "C", "peer-c"),
		strictCandidateObsWithMeta(995, "snap-a", "state-a", "vset-a", "reg-b", "D", "peer-d"),
	}

	quorum, best := selectStrictSnapshotMetaCandidate(observations, 3)
	if quorum != nil {
		t.Fatalf("expected no quorum candidate when registry hash differs, got=%+v", quorum)
	}
	if best == nil {
		t.Fatalf("expected best partial candidate")
	}
	if len(best.Validators) != 2 {
		t.Fatalf("unexpected best partial vote count: got=%d want=2", len(best.Validators))
	}
	if best.ValidatorRegistryHash != "reg-a" {
		t.Fatalf("unexpected best partial registry hash: got=%q want=reg-a", best.ValidatorRegistryHash)
	}
}

func TestStrictSnapshotCandidateHeightsUseLatestObservedSnapshots(t *testing.T) {
	availabilities := []strictSnapshotMetaAvailability{
		{
			Provider:    "peer-b",
			ValidatorID: "B",
			Candidate:   strictCandidateObs(988, "h988", "B", "peer-b").Candidate,
		},
		{
			Provider:    "peer-c",
			ValidatorID: "C",
			Candidate:   strictCandidateObs(989, "h989", "C", "peer-c").Candidate,
		},
		{
			Provider:    "peer-d",
			ValidatorID: "D",
			Candidate:   strictCandidateObs(989, "h989", "D", "peer-d").Candidate,
		},
	}

	got := strictSnapshotCandidateHeights(availabilities)
	if len(got) != 2 {
		t.Fatalf("unexpected candidate heights len: got=%d want=2 (%v)", len(got), got)
	}
	if got[0] != 989 || got[1] != 988 {
		t.Fatalf("unexpected candidate heights: got=%v want=[989 988]", got)
	}
}

func TestRetargetSnapshotSessionCandidateResetsVoteAccumulator(t *testing.T) {
	n := &Node{}
	session := n.startSnapshotSession(953, "test")
	if !session.Active {
		t.Fatalf("expected active snapshot session")
	}
	n.snapshotSessionMu.Lock()
	n.snapshotSession.CanonicalHash = "old-fingerprint"
	n.snapshotSession.FreezeSnapHash = "old-snapshot"
	n.snapshotSession.FreezeStateRoot = "old-state"
	n.snapshotSession.FreezeVsetHash = "old-vset"
	n.snapshotSession.FreezeRegistryHash = "old-registry"
	n.snapshotSession.CheckpointHash = "old-checkpoint"
	n.snapshotSession.Votes["B"] = SnapshotVote{ValidatorID: "B", Height: snapshotCheckpointHeightFor(953)}
	n.snapshotSession.StrictReasonCounts = map[string]uint64{"meta_conflict": 2}
	n.snapshotSession.StrictProviderResults = map[string]map[string]uint64{"peer-b": {"meta_conflict": 2}}
	n.snapshotSessionMu.Unlock()

	n.retargetSnapshotSessionCandidate(956, snapshotCheckpointHeightFor(956), "snapshot_anchor_retargeted")

	got := n.snapshotSessionSnapshot()
	if got.FreezeHeight != 956 {
		t.Fatalf("unexpected freeze height: got=%d want=956", got.FreezeHeight)
	}
	if got.CheckpointHeight != snapshotCheckpointHeightFor(956) {
		t.Fatalf("unexpected checkpoint height: got=%d", got.CheckpointHeight)
	}
	if got.CandidateHeight != 956 || got.CandidateCheckpointHeight != snapshotCheckpointHeightFor(956) {
		t.Fatalf("unexpected candidate state: %+v", got)
	}
	if len(got.Votes) != 0 {
		t.Fatalf("expected cleared vote accumulator, got=%d votes", len(got.Votes))
	}
	if got.CanonicalHash != "" || got.FreezeSnapHash != "" || got.FreezeStateRoot != "" || got.FreezeVsetHash != "" || got.FreezeRegistryHash != "" {
		t.Fatalf("expected cleared canonical freeze state, got=%+v", got)
	}
	if len(got.StrictReasonCounts) != 0 || len(got.StrictProviderResults) != 0 {
		t.Fatalf("expected cleared strict diagnostics, got counts=%v providers=%v", got.StrictReasonCounts, got.StrictProviderResults)
	}
}

func TestRetargetSnapshotSessionLowerAvailableImmediately(t *testing.T) {
	n := &Node{}
	session := n.startSnapshotSession(1000000, "test")
	if !session.Active {
		t.Fatalf("expected active snapshot session")
	}
	if !n.retargetSnapshotSessionLowerAvailable(1000000, 980000, snapshotCheckpointHeightFor(980000), "snapshot_anchor_lower_available_retargeted") {
		t.Fatalf("expected lower-available snapshot retarget to apply immediately")
	}
	got := n.snapshotSessionSnapshot()
	if got.FreezeHeight != 980000 {
		t.Fatalf("unexpected freeze height: got=%d want=980000", got.FreezeHeight)
	}
	if got.CandidateHeight != 980000 {
		t.Fatalf("unexpected candidate height: got=%d want=980000", got.CandidateHeight)
	}
	if got.CheckpointHeight != snapshotCheckpointHeightFor(980000) {
		t.Fatalf("unexpected checkpoint height: got=%d", got.CheckpointHeight)
	}
}

func TestSnapshotSessionFrozenTargetPrefersDiscoveredCandidateHeight(t *testing.T) {
	n := &Node{}
	n.startSnapshotSession(988, "test")
	n.setSnapshotSessionCandidate(989, snapshotCheckpointHeightFor(989))

	target, ok := n.snapshotSessionFrozenTarget(0)
	if !ok {
		t.Fatalf("expected active snapshot target")
	}
	if target != 989 {
		t.Fatalf("unexpected session target: got=%d want=989", target)
	}
}

func TestForceSnapshotSyncIgnoresCurrentTipTarget(t *testing.T) {
	bc := NewBlockchain()
	for h := uint64(1); h <= 5; h++ {
		prev := bc.LastBlock()
		block := Block{
			ID:        h,
			Height:    h,
			PrevHash:  prev.BlockHash,
			BlockHash: fmt.Sprintf("snapshot-current-tip-%d", h),
			Type:      BlockTypeTime,
		}
		bc.AddBlock(block)
	}

	n := &Node{
		Blockchain: &bc,
		shutdownCh: make(chan struct{}),
	}
	n.startSnapshotSession(5, "test")

	n.forceSnapshotSyncToHeight(5, "startup_execution_snapshot_missing")

	if n.snapshotSessionActive() {
		t.Fatalf("expected current-tip snapshot force to close stale session")
	}
}

func TestRuntimeStatusReportsSnapshotStrictDiagnostics(t *testing.T) {
	n := makeFreshLateJoinTestNode()
	n.startSnapshotSession(953, "test")
	n.snapshotSessionMu.Lock()
	n.snapshotSession.Stage = SnapshotSyncStageCollectProofs
	n.snapshotSession.CandidateHeight = 956
	n.snapshotSession.CandidateCheckpointHeight = snapshotCheckpointHeightFor(956)
	n.snapshotSession.LastRejectReason = "checkpoint_proof_invalid"
	n.snapshotSession.StrictReasonCounts = map[string]uint64{
		"checkpoint_proof_invalid": 2,
		"meta_conflict":            1,
	}
	n.snapshotSession.StrictProviderResults = map[string]map[string]uint64{
		"peer-b": {"checkpoint_proof_invalid": 1},
		"peer-c": {"checkpoint_proof_invalid": 1, "meta_conflict": 1},
	}
	n.snapshotSessionMu.Unlock()

	status := n.runtimeStatusSnapshot()
	if status.SyncAnchorCandidateHeight != 956 {
		t.Fatalf("unexpected candidate height: got=%d want=956", status.SyncAnchorCandidateHeight)
	}
	if status.SyncAnchorCandidateCheckpointHeight != snapshotCheckpointHeightFor(956) {
		t.Fatalf("unexpected candidate checkpoint height: got=%d", status.SyncAnchorCandidateCheckpointHeight)
	}
	if status.SyncAnchorLastRejectReason != "checkpoint_proof_invalid" {
		t.Fatalf("unexpected last reject reason: got=%q", status.SyncAnchorLastRejectReason)
	}
	if status.SyncAnchorStrictReasonCounts["checkpoint_proof_invalid"] != 2 {
		t.Fatalf("unexpected strict reason counts: %+v", status.SyncAnchorStrictReasonCounts)
	}
	if status.SyncAnchorStrictProviderResults["peer-c"]["meta_conflict"] != 1 {
		t.Fatalf("unexpected provider strict results: %+v", status.SyncAnchorStrictProviderResults)
	}
}
