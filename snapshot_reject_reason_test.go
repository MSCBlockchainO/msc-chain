package main

import "testing"

func TestMergeSnapshotVerifyRejectReasonPrefersRemoteWhenLocalAnchorUnavailable(t *testing.T) {
	got := mergeSnapshotVerifyRejectReason("anchor_fetch_failed", "anchor_block_unavailable")
	if got != "anchor_fetch_failed" {
		t.Fatalf("unexpected merged reason: got=%s want=%s", got, "anchor_fetch_failed")
	}
}

func TestMergeSnapshotVerifyRejectReasonUsesLocalWhenSpecific(t *testing.T) {
	got := mergeSnapshotVerifyRejectReason("anchor_fetch_failed", "state_root_mismatch")
	if got != "state_root_mismatch" {
		t.Fatalf("unexpected merged reason: got=%s want=%s", got, "state_root_mismatch")
	}
}

func TestShouldPenalizeSnapshotPeerForRejectReason(t *testing.T) {
	if shouldPenalizeSnapshotPeerForRejectReason("anchor_block_unavailable") {
		t.Fatalf("anchor_block_unavailable should not penalize peer")
	}
	if shouldPenalizeSnapshotPeerForRejectReason("anchor_fetch_failed") {
		t.Fatalf("anchor_fetch_failed should not penalize peer")
	}
	if !shouldPenalizeSnapshotPeerForRejectReason("state_root_mismatch") {
		t.Fatalf("state_root_mismatch should penalize peer")
	}
}
