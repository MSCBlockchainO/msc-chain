package main

import (
	"strings"
	"testing"
)

func testSnapshotForManifest() *StateSnapshot {
	ledger := NewLedger()
	return &StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   64,
		PrevHash:                 "h63",
		BlockHash:                "h64",
		StateRoot:                "state64",
		GenesisHash:              "genesis",
		Ledger:                   ledger,
		LedgerHash:               HashLedger(ledger),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry:        map[string]ValidatorRecord{"A": {ID: "A", Stake: 400}, "B": {ID: "B", Stake: 300}, "C": {ID: "C", Stake: 200}, "D": {ID: "D", Stake: 100}},
		PendingValidators:        map[string]uint64{"F": 68},
		PendingValidatorRemovals: map[string]uint64{"D": 68},
		NextValidatorSetHeight:   65,
		ActivationHeight:         65,
		CheckpointProof: map[string]string{
			"A": "sig-a",
			"B": "sig-b",
			"C": "sig-c",
		},
	}
}

func TestSnapshotManifestFromSnapshotDeterministic(t *testing.T) {
	snap := testSnapshotForManifest()

	manifestA, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot first call: %v", err)
	}
	manifestB, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot second call: %v", err)
	}
	if !snapshotManifestMatches(manifestA, manifestB) {
		t.Fatalf("expected deterministic manifest, got A=%+v B=%+v", manifestA, manifestB)
	}
	if manifestA.ChunkCount == 0 || len(manifestA.ChunkHashes) == 0 {
		t.Fatalf("expected manifest chunk metadata")
	}
	if manifestA.ChunkCount != uint64(len(manifestA.ChunkHashes)) {
		t.Fatalf("chunk count mismatch: count=%d hashes=%d", manifestA.ChunkCount, len(manifestA.ChunkHashes))
	}
	if manifestA.ValidatorRegistryHash != strings.TrimSpace(snapshotValidatorRegistryHash(snap)) {
		t.Fatalf("manifest must carry validator registry hash: got=%q want=%q", manifestA.ValidatorRegistryHash, snapshotValidatorRegistryHash(snap))
	}
}

func TestSnapshotManifestHashChangesOnRegistryOrChunkOrderMismatch(t *testing.T) {
	snap := testSnapshotForManifest()

	manifest, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}

	mutatedRegistry := *manifest
	mutatedRegistry.ValidatorRegistryHash = "different-registry"
	if snapshotManifestMatches(manifest, &mutatedRegistry) {
		t.Fatalf("expected registry hash mismatch to change manifest identity")
	}

	mutatedChunks := *manifest
	mutatedChunks.ChunkHashes = append([]string{}, manifest.ChunkHashes...)
	if len(mutatedChunks.ChunkHashes) == 0 {
		t.Fatalf("expected at least one chunk hash")
	}
	mutatedChunks.ChunkHashes[0] = "tampered-chunk-hash"
	if snapshotManifestMatches(manifest, &mutatedChunks) {
		t.Fatalf("expected chunk hash mismatch to change manifest identity")
	}
}
