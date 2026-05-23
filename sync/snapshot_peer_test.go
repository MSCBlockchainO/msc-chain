package syncpipeline

import "testing"

func TestSelectSnapshotReplicationPeersPrioritizesPolicy(t *testing.T) {
	peers := []Peer{
		{ID: "p1", IsValidator: true, Score: 10},
		{ID: "p2", IsArchival: true, Score: 8},
		{ID: "p3", Score: 6},
		{ID: "p4", Score: 4},
	}

	selected := SelectSnapshotReplicationPeers(peers, 3, "snapshot-448")
	if len(selected) != 3 {
		t.Fatalf("expected 3 replication peers, got %d", len(selected))
	}

	hasValidator := false
	hasArchival := false
	seen := make(map[string]struct{}, len(selected))
	for _, peer := range selected {
		if _, ok := seen[peer.ID]; ok {
			t.Fatalf("duplicate peer selected: %s", peer.ID)
		}
		seen[peer.ID] = struct{}{}
		if peer.IsValidator {
			hasValidator = true
		}
		if peer.IsArchival {
			hasArchival = true
		}
	}
	if !hasValidator {
		t.Fatalf("expected at least one validator replication target")
	}
	if !hasArchival {
		t.Fatalf("expected at least one archival replication target")
	}
}

func TestSelectSnapshotReplicationPeersDeterministicForSeed(t *testing.T) {
	peers := []Peer{
		{ID: "v1", IsValidator: true, Score: 10},
		{ID: "a1", IsArchival: true, Score: 9},
		{ID: "r1", Score: 8},
		{ID: "r2", Score: 7},
		{ID: "r3", Score: 6},
	}

	left := SelectSnapshotReplicationPeers(peers, 4, "same-seed")
	right := SelectSnapshotReplicationPeers(peers, 4, "same-seed")
	if len(left) != len(right) {
		t.Fatalf("determinism mismatch: left=%d right=%d", len(left), len(right))
	}
	for i := range left {
		if left[i].ID != right[i].ID {
			t.Fatalf("determinism mismatch at %d: %s != %s", i, left[i].ID, right[i].ID)
		}
	}
}

