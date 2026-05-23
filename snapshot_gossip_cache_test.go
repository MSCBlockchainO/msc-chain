package main

import "testing"

func TestSnapshotChunkGossipProviderScoreCountsMatchingAnnouncements(t *testing.T) {
	n := &Node{
		snapshotChunkGossipCache: map[string]SnapshotChunkGossip{
			"a": {From: "peerA", Height: 448, SnapshotHash: "0xabc123"},
			"b": {From: "peerA", Height: 448, SnapshotHash: "0xabc123"},
			"c": {From: "peerB", Height: 448, SnapshotHash: "0xabc123"},
			"d": {From: "peerA", Height: 447, SnapshotHash: "0xabc123"},
			"e": {From: "peerA", Height: 448, SnapshotHash: "0xdef456"},
		},
	}

	score := n.snapshotChunkGossipProviderScore(448, "0xabc123", "peerA")
	if score != 2 {
		t.Fatalf("unexpected provider score: got=%d want=2", score)
	}
}

func TestCachedSnapshotMetaAvailabilitiesRejectsInvalidMeta(t *testing.T) {
	n := &Node{
		snapshotMetaGossipCache: map[string]SnapshotMetaGossip{
			"meta": {
				From:   "A",
				Height: 448,
				Meta: SnapshotMetaResponse{
					Height:                448,
					SnapshotHash:          "0xabc123",
					StateRoot:             "0xstate",
					ValidatorSetHash:      "0xset",
					ValidatorRegistryHash: "0xregistry",
					ChunkSize:             1024,
					TotalChunks:           1,
					ChunkHashes:           []string{"0xchunk"},
					Available:             true,
				},
			},
		},
	}

	validatorProviders := map[string]string{"A": "peerA"}
	availabilities := n.cachedSnapshotMetaAvailabilities(448, 0, validatorProviders)
	if len(availabilities) != 0 {
		t.Fatalf("expected invalid cached metadata to be ignored, got=%d", len(availabilities))
	}
}

func TestHandleSnapshotMetaGossipMessageUpdatesSnapshotCatalog(t *testing.T) {
	n := &Node{}
	n.handleSnapshotMetaGossipMessage(SnapshotMetaGossip{
		From:   "A",
		Height: 448,
		Meta: SnapshotMetaResponse{
			Height:       448,
			SnapshotHash: "0xabc123",
			StateRoot:    "0xstate",
			TotalChunks:  8,
			Available:    true,
		},
	})

	entry, ok := n.snapshotCatalogEntry(448)
	if !ok {
		t.Fatalf("expected snapshot catalog entry to exist")
	}
	if entry.StateRoot != "0xstate" {
		t.Fatalf("unexpected state root: got=%s want=%s", entry.StateRoot, "0xstate")
	}
	if entry.ChunkCount != 8 {
		t.Fatalf("unexpected chunk count: got=%d want=%d", entry.ChunkCount, 8)
	}
	if len(entry.ProviderSet) != 1 || entry.ProviderSet[0] != "A" {
		t.Fatalf("unexpected provider set: %+v", entry.ProviderSet)
	}
	if entry.AvailabilityRatio <= 0 {
		t.Fatalf("expected non-zero availability ratio, got=%f", entry.AvailabilityRatio)
	}
}

func TestHandleSnapshotChunkGossipMessageUpdatesSnapshotCatalog(t *testing.T) {
	n := &Node{}
	n.handleSnapshotChunkGossipMessage(SnapshotChunkGossip{
		From:         "B",
		Height:       512,
		SnapshotHash: "0xchunkhash",
		ChunkCount:   12,
	})
	n.handleSnapshotChunkGossipMessage(SnapshotChunkGossip{
		From:         "B",
		Height:       512,
		SnapshotHash: "0xchunkhash",
		ChunkCount:   12,
	})

	entry, ok := n.snapshotCatalogEntry(512)
	if !ok {
		t.Fatalf("expected snapshot catalog entry to exist")
	}
	if entry.ChunkCount != 12 {
		t.Fatalf("unexpected chunk count: got=%d want=%d", entry.ChunkCount, 12)
	}
	if len(entry.ProviderSet) != 1 || entry.ProviderSet[0] != "B" {
		t.Fatalf("unexpected provider set: %+v", entry.ProviderSet)
	}
	if entry.AvailabilityRatio <= 0 {
		t.Fatalf("expected non-zero availability ratio, got=%f", entry.AvailabilityRatio)
	}
}
