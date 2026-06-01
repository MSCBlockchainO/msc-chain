package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func storageManagerTestBlocks(count uint64) []Block {
	blocks := make([]Block, 0, count)
	prev := GenesisHash
	for height := uint64(1); height <= count; height++ {
		block := Block{
			ID:                        height,
			Height:                    height,
			PrevHash:                  prev,
			StateRoot:                 HashStrings([]string{"state", strconv.FormatUint(height, 10)}),
			ValidatorSetHash:          "validator-set-hash",
			ValidatorSetRoot:          "validator-set-root",
			FinalizedEpoch:            height,
			FinalizedHeight:           height,
			FinalizedValidatorSetHash: "validator-set-hash",
			FinalizedValidatorSetRoot: "validator-set-root",
			FinalityRoot:              HashStrings([]string{"finality", strconv.FormatUint(height, 10)}),
			EpochAnchorHash:           HashStrings([]string{"anchor", strconv.FormatUint(height, 10)}),
		}
		block.FinalizedStateRoot = block.StateRoot
		block.BlockHash = HashBlock(block)
		prev = block.BlockHash
		blocks = append(blocks, block)
	}
	return blocks
}

func TestStorageManagerValidatorPolicyPrunesAndCheckpoints(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()
	dataDir := t.TempDir()
	blocks := storageManagerTestBlocks(40)
	n := &Node{
		ID:         "A",
		DataDir:    dataDir,
		Role:       "validator",
		DB:         db,
		Blockchain: &Blockchain{Blocks: blocks},
	}
	n.commitMu.Lock()
	n.committedHeight = 40
	n.finalizedHeight = 40
	n.commitMu.Unlock()
	n.snapshotExecutionLedgerByHeight = map[uint64]Ledger{
		1:  NewLedger(),
		36: NewLedger(),
	}
	for h := uint64(1); h <= 40; h++ {
		storeSnapshotForHeight(t, db, StateSnapshot{Height: h, BlockHash: blocks[h-1].BlockHash, StateRoot: blocks[h-1].StateRoot, LedgerHash: HashLedger(NewLedger())})
		if err := n.persistBlockFile(blocks[h-1]); err != nil {
			t.Fatalf("persist block file %d: %v", h, err)
		}
	}
	manager := &StorageManager{Node: n, Policy: StoragePolicy{
		Profile:                      storageProfileValidator,
		PruningEnabled:               true,
		EpochLengthBlocks:            2,
		RetainedEpochs:               3,
		RollbackWindowBlocks:         4,
		SnapshotKeepLast:             3,
		RecentBlockWindow:            6,
		HourlySnapshotIntervalBlocks: 10,
		ColdExportEnabled:            true,
		ColdExportCompression:        "zstd",
		ParallelGCWorkers:            2,
		StateRentEnabled:             true,
		StateRentArchiveEpochs:       2,
		StateLayoutMode:              "merkle",
	}}
	report, err := manager.RunOnce("test")
	if err != nil {
		t.Fatalf("storage manager run: %v", err)
	}
	if report.RetainFromHeight != 35 {
		t.Fatalf("retain_from = %d, want 35", report.RetainFromHeight)
	}
	if !report.PruningEnabled || report.HotWindowBlocks != 6 {
		t.Fatalf("unexpected pruning policy in report: %+v", report)
	}
	if report.StateCheckpointHeight != 40 {
		t.Fatalf("checkpoint height = %d, want 40", report.StateCheckpointHeight)
	}
	if _, err := n.GetSnapshot(1); err == nil {
		t.Fatal("expected old snapshot pruned")
	}
	if _, err := n.GetSnapshot(40); err != nil {
		t.Fatalf("expected latest snapshot retained: %v", err)
	}
	if _, ok := n.snapshotExecutionLedgerByHeight[1]; ok {
		t.Fatal("expected old execution cache pruned")
	}
	if _, ok := n.snapshotExecutionLedgerByHeight[36]; !ok {
		t.Fatal("expected recent execution cache retained")
	}
	if _, err := os.Stat(blockStoreFilePath(dataDir, "A", 1)); !os.IsNotExist(err) {
		t.Fatalf("expected old block file pruned from hot store, err=%v", err)
	}
	if restoredFromCold, ok := n.loadBlockFile(1); !ok || restoredFromCold.ID != 1 {
		t.Fatalf("expected old block file available through cold fallback, ok=%t block=%+v", ok, restoredFromCold)
	}
	if _, ok := n.loadBlockFile(40); !ok {
		t.Fatal("expected recent block file retained")
	}
	cold := coldBlockStoreZstdFilePath(dataDir, "A", 1)
	if _, err := os.Stat(cold); err != nil {
		t.Fatalf("expected cold zstd export: %v", err)
	}
	if ok, err := n.restoreColdBlockFile(1); err != nil || !ok {
		t.Fatalf("expected cold block re-import ok=%t err=%v", ok, err)
	}
	if _, err := os.Stat(blockStoreFilePath(dataDir, "A", 1)); err != nil {
		t.Fatalf("expected restored hot block file: %v", err)
	}
	checkpoint, ok, err := n.loadStateCheckpoint(40)
	if err != nil || !ok {
		t.Fatalf("load checkpoint ok=%t err=%v", ok, err)
	}
	if checkpoint.EpochAnchorHash != blocks[39].EpochAnchorHash || checkpoint.FinalityRoot != blocks[39].FinalityRoot {
		t.Fatalf("checkpoint finality mismatch: %+v", checkpoint)
	}
	marker, ok := n.loadStatePruneMarker()
	if !ok || marker.Profile != storageProfileValidator || !marker.BlockStoreCompacted || !marker.ColdStorageExported || !marker.ExecutionCacheGC {
		t.Fatalf("unexpected storage marker ok=%t marker=%+v", ok, marker)
	}
	if marker.ParallelGCWorkers != 2 || !marker.StateRentEnabled || marker.StateRentArchiveEpochs != 2 || marker.StateLayoutMode != "merkle" {
		t.Fatalf("storage policy marker mismatch: %+v", marker)
	}
	if !marker.PruningEnabled || marker.HotWindowBlocks != 6 {
		t.Fatalf("storage pruning marker mismatch: %+v", marker)
	}
}

func TestStorageManagerArchiveProfileSkipsPruning(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()
	oldMode := SyncHistoryMode
	SyncHistoryMode = SyncHistoryModeArchive
	t.Cleanup(func() { SyncHistoryMode = oldMode })
	blocks := storageManagerTestBlocks(3)
	n := &Node{
		ID:         "A",
		DataDir:    t.TempDir(),
		Role:       "validator",
		DB:         db,
		Blockchain: &Blockchain{Blocks: blocks},
	}
	n.commitMu.Lock()
	n.finalizedHeight = 3
	n.commitMu.Unlock()
	storeSnapshotForHeight(t, db, StateSnapshot{Height: 1, BlockHash: "block"})
	report, err := NewStorageManager(n).RunOnce("archive_test")
	if err != nil {
		t.Fatalf("archive storage manager: %v", err)
	}
	if !report.ArchiveModeSkipped {
		t.Fatalf("expected archive skip, got %+v", report)
	}
	if report.PruningEnabled {
		t.Fatalf("archive mode must disable pruning in report: %+v", report)
	}
	if _, err := n.GetSnapshot(1); err != nil {
		t.Fatalf("archive mode must retain snapshot: %v", err)
	}
}

func TestStoragePolicySnapshotReportsRetention(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()
	oldProfile := StorageHistoryProfile
	oldPruning := StorageStatePruningEnabled
	StorageHistoryProfile = storageProfileAuto
	StorageStatePruningEnabled = true
	t.Cleanup(func() {
		StorageHistoryProfile = oldProfile
		StorageStatePruningEnabled = oldPruning
	})
	blocks := storageManagerTestBlocks(20)
	n := &Node{
		ID:         "A",
		DataDir:    t.TempDir(),
		Role:       "validator",
		DB:         db,
		Blockchain: &Blockchain{Blocks: blocks},
	}
	n.commitMu.Lock()
	n.finalizedHeight = 20
	n.commitMu.Unlock()
	resp := n.storagePolicySnapshot()
	if resp["profile"] != storageProfileValidator {
		t.Fatalf("unexpected profile: %+v", resp)
	}
	if resp["state_pruning_enabled"] != true || resp["archive_mode"] != false {
		t.Fatalf("unexpected pruning flags: %+v", resp)
	}
	if got := resp["retain_from_height"].(uint64); got == 0 {
		t.Fatalf("expected retain_from height in storage policy: %+v", resp)
	}
}

func TestStorageSnapshotTierCompactionKeepsLatestAndTierHeads(t *testing.T) {
	heights := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	policy := StoragePolicy{
		SnapshotKeepLast:             2,
		HourlySnapshotIntervalBlocks: 3,
		HourlySnapshotRetain:         2,
		DailySnapshotRetain:          1,
	}
	protected := storageSnapshotProtectedHeights(heights, 10, policy)
	for _, h := range []uint64{10, 9, 8} {
		if !protected[h] {
			t.Fatalf("expected height %d protected, protected=%v", h, protected)
		}
	}
	if protected[1] || protected[2] || protected[6] {
		t.Fatalf("old low snapshots should not be protected: %v", protected)
	}
}

func TestStateCheckpointFilePersistsFinalityCommitments(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()
	blocks := storageManagerTestBlocks(1)
	n := &Node{
		ID:         "A",
		DataDir:    t.TempDir(),
		DB:         db,
		Blockchain: &Blockchain{Blocks: blocks},
	}
	checkpoint, err := n.persistStateCheckpoint(1)
	if err != nil {
		t.Fatalf("persist checkpoint: %v", err)
	}
	if checkpoint.Height != 1 || checkpoint.EpochAnchorHash == "" || checkpoint.FinalityRoot == "" {
		t.Fatalf("checkpoint missing finality data: %+v", checkpoint)
	}
	raw, err := os.ReadFile(stateCheckpointFilePath(n.DataDir, n.ID, 1))
	if err != nil {
		t.Fatalf("read checkpoint file: %v", err)
	}
	if !strings.Contains(string(raw), stateCheckpointDomain) {
		t.Fatalf("checkpoint file missing domain: %s", raw)
	}
}
