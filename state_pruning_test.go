package main

import (
	"strings"
	"testing"
)

func TestPruneSnapshotsRecordsStatePruneMarker(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}},
	}
	n.commitMu.Lock()
	n.committedHeight = 5
	n.finalizedHeight = 5
	n.commitMu.Unlock()

	for h := uint64(1); h <= 5; h++ {
		storeSnapshotForHeight(t, db, StateSnapshot{Height: h, BlockHash: "block"})
	}
	n.snapshotExecutionLedgerByHeight = map[uint64]Ledger{
		1: NewLedger(),
		4: NewLedger(),
	}

	if err := n.PruneSnapshots(2); err != nil {
		t.Fatalf("prune snapshots: %v", err)
	}
	if _, err := n.GetSnapshot(1); err == nil {
		t.Fatal("expected old snapshot height 1 to be pruned")
	}
	if _, err := n.GetSnapshot(3); err != nil {
		t.Fatalf("expected retained snapshot height 3: %v", err)
	}
	if !n.stateHistoryPrunedForHeight(2) {
		t.Fatal("expected prune marker to cover height 2")
	}
	if n.stateHistoryPrunedForHeight(3) {
		t.Fatal("did not expect retained height 3 to be marked pruned")
	}
	marker, ok := n.loadStatePruneMarker()
	if !ok {
		t.Fatal("expected state prune marker")
	}
	if marker.PrunedThroughHeight != 2 || !marker.SnapshotCompacted || !marker.SnapshotMetaCompacted || !marker.SnapshotDeltaCompacted {
		t.Fatalf("unexpected marker after snapshot prune: %+v", marker)
	}
	if _, ok := n.snapshotExecutionLedgerByHeight[1]; ok {
		t.Fatal("expected legacy prune to remove old execution cache")
	}
	if _, ok := n.snapshotExecutionLedgerByHeight[4]; !ok {
		t.Fatal("expected legacy prune to retain recent execution cache")
	}
	if marker.Profile == "" || marker.StateLayoutMode == "" || marker.ParallelGCWorkers == 0 || !marker.ExecutionCacheGC {
		t.Fatalf("expected merged storage policy marker fields: %+v", marker)
	}

	registry := testValidatorSetMaterializationRegistry()
	for h := uint64(1); h <= 5; h++ {
		if err := n.storeValidatorRegistrySnapshotRecord(h, registry); err != nil {
			t.Fatalf("store registry snapshot %d: %v", h, err)
		}
	}
	if err := n.PruneValidatorRegistrySnapshots(2); err != nil {
		t.Fatalf("prune registry snapshots: %v", err)
	}
	if _, err := n.loadValidatorRegistrySnapshot(1); err == nil {
		t.Fatal("expected old registry snapshot height 1 to be pruned")
	}
	marker, ok = n.loadStatePruneMarker()
	if !ok || !marker.RegistryCompacted {
		t.Fatalf("expected registry compaction marker, ok=%t marker=%+v", ok, marker)
	}
}

func TestArchiveHistoryModeSkipsStatePrune(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	oldMode := SyncHistoryMode
	SyncHistoryMode = SyncHistoryModeArchive
	t.Cleanup(func() { SyncHistoryMode = oldMode })

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1}, {ID: 2}, {ID: 3}}},
	}
	n.commitMu.Lock()
	n.committedHeight = 3
	n.finalizedHeight = 3
	n.commitMu.Unlock()
	storeSnapshotForHeight(t, db, StateSnapshot{Height: 1, BlockHash: "block"})

	if err := n.PruneSnapshots(1); err != nil {
		t.Fatalf("archive prune snapshots: %v", err)
	}
	if _, err := n.GetSnapshot(1); err != nil {
		t.Fatalf("archive mode must retain full snapshot history: %v", err)
	}
	if _, ok := n.loadStatePruneMarker(); ok {
		t.Fatal("archive mode should not write a prune marker")
	}
}

func TestConsensusValidatorsUseFrozenHashAfterStatePruning(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	active := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := "chain-pruned-validator-set-hash"

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{
			{
				ID:                     1,
				BlockHash:              "block-1",
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
				ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
			},
			{
				ID:                     2,
				BlockHash:              "block-2",
				ValidatorSetHash:       targetHash,
				ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 3,
				ActivationHeight:       3,
			},
		}},
		frozenValidatorsByHeight: map[uint64][]string{
			2: active,
		},
		frozenValidatorHashByHeight: map[uint64]string{
			2: targetHash,
		},
	}
	if err := n.recordStatePruneMarker("snapshot", 3, 2, 1); err != nil {
		t.Fatalf("record prune marker: %v", err)
	}

	got := n.consensusValidatorsForHeight(2)
	if !sameStringSlice(got, active) {
		t.Fatalf("expected frozen pruned validator set, got=%v want=%v", got, active)
	}
	resolved, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(2)
	if !ok || !sameStringSlice(resolved, active) {
		t.Fatalf("expected resolver to use frozen pruned set, ok=%t source=%q got=%v", ok, source, resolved)
	}
	if !strings.EqualFold(strings.TrimSpace(resolvedHash), targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "chain_pruned_frozen" {
		t.Fatalf("unexpected source: got=%q want=chain_pruned_frozen", source)
	}
}

func TestChainHashSyncPreservesPrunedFrozenValidatorIDs(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	active := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	legacyHash := ValidatorSetHash(active)
	chainHash := "registry-bound-chain-hash"
	if strings.EqualFold(legacyHash, chainHash) {
		t.Fatal("test setup invalid")
	}

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:               1,
			BlockHash:        "block-1",
			ValidatorSetHash: chainHash,
		}}},
		frozenValidatorsByHeight: map[uint64][]string{
			1: active,
		},
		frozenValidatorHashByHeight: map[uint64]string{
			1: legacyHash,
		},
	}
	if err := n.recordStatePruneMarker("snapshot", 2, 2, 0); err != nil {
		t.Fatalf("record prune marker: %v", err)
	}

	applied := n.syncFrozenValidatorSetHashesFromChain()
	if applied != 1 {
		t.Fatalf("expected one chain hash update, got %d", applied)
	}
	got := n.frozenValidatorsForHeight(1)
	if !sameStringSlice(got, active) {
		t.Fatalf("expected pruned frozen validator IDs to be preserved, got=%v want=%v", got, active)
	}
	gotHash, ok := n.frozenValidatorSetHash(1)
	if !ok || !strings.EqualFold(gotHash, chainHash) {
		t.Fatalf("expected chain hash rebound, ok=%t got=%q want=%q", ok, gotHash, chainHash)
	}
}
