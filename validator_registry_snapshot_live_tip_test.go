package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func withValidatorRegistryTestState(t *testing.T, registry map[string]ValidatorRecord) {
	t.Helper()
	old := GlobalValidatorRegistry.Snapshot()
	GlobalValidatorRegistry.Load(registry)
	t.Cleanup(func() { GlobalValidatorRegistry.Load(old) })
}

func testNodeWithRegistryBlocks(db *NodeDB, blocks []Block) *Node {
	return &Node{
		DB:         db,
		Ledger:     NewLedger(),
		Blockchain: &Blockchain{Blocks: blocks},
	}
}

func storeLegacyValidatorRegistrySnapshotRecord(t *testing.T, db *NodeDB, height uint64, registry map[string]ValidatorRecord) {
	t.Helper()
	record := validatorRegistrySnapshotRecord{
		Height:   height,
		Registry: copyValidatorRegistrySnapshot(registry),
		Hash:     ValidatorRegistrySnapshotHash(registry),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal legacy registry snapshot: %v", err)
	}
	if err := db.State.Update(func(txn *Txn) error {
		return txn.Set(validatorRegistrySnapshotLegacyKey(height), raw)
	}); err != nil {
		t.Fatalf("store legacy registry snapshot: %v", err)
	}
}

func storeCanonicalValidatorRegistrySnapshotRecord(t *testing.T, db *NodeDB, height uint64, registry map[string]ValidatorRecord) {
	t.Helper()
	record := validatorRegistrySnapshotRecord{
		Height:   height,
		Registry: copyValidatorRegistrySnapshot(registry),
		Hash:     ValidatorRegistrySnapshotHash(registry),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal canonical registry snapshot: %v", err)
	}
	if err := db.State.Update(func(txn *Txn) error {
		return txn.Set(validatorRegistrySnapshotCanonicalKey(height), raw)
	}); err != nil {
		t.Fatalf("store canonical registry snapshot: %v", err)
	}
}

func hasStateKey(t *testing.T, db *NodeDB, key []byte) bool {
	t.Helper()
	err := db.State.View(func(txn *Txn) error {
		_, err := txn.Get(key)
		return err
	})
	return err == nil
}

func TestResolveCommittedValidatorRegistrySnapshotPrefersPersistedSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	n := testNodeWithRegistryBlocks(db, []Block{{ID: 2, BlockHash: "block-2"}})
	if err := n.storeValidatorRegistrySnapshotRecord(2, registry); err != nil {
		t.Fatalf("store registry snapshot: %v", err)
	}

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2)
	if !ok {
		t.Fatalf("expected persisted registry snapshot to resolve")
	}
	if source != "registry_snapshot" {
		t.Fatalf("unexpected source: got=%q want=registry_snapshot", source)
	}
	wantHash := ValidatorRegistrySnapshotHash(registry)
	if !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, wantHash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected registry size: got=%d want=%d", len(got), len(registry))
	}
	if !hasStateKey(t, db, validatorRegistrySnapshotCanonicalKey(2)) {
		t.Fatalf("expected canonical registry snapshot key to exist")
	}
	if !hasStateKey(t, db, validatorRegistrySnapshotLegacyKey(2)) {
		t.Fatalf("expected legacy registry snapshot key to exist")
	}
}

func TestResolveCommittedValidatorRegistrySnapshotReadsLegacyKeyFallback(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	storeLegacyValidatorRegistrySnapshotRecord(t, db, 2, registry)
	n := testNodeWithRegistryBlocks(db, []Block{{ID: 2, BlockHash: "block-2"}})

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2)
	if !ok {
		t.Fatalf("expected legacy registry key fallback to resolve")
	}
	if source != "registry_snapshot" {
		t.Fatalf("unexpected source: got=%q want=registry_snapshot", source)
	}
	hash := ValidatorRegistrySnapshotHash(registry)
	if !strings.EqualFold(gotHash, hash) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, hash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected registry size: got=%d want=%d", len(got), len(registry))
	}
}

func TestResolveCommittedValidatorRegistrySnapshotPrefersCanonicalOverLegacy(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	legacy := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
	}
	canonical := map[string]ValidatorRecord{
		"B": {ID: "B", Stake: 250},
	}
	storeLegacyValidatorRegistrySnapshotRecord(t, db, 2, legacy)
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 2, canonical)
	n := testNodeWithRegistryBlocks(db, []Block{{ID: 2, BlockHash: "block-2"}})

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2)
	if !ok {
		t.Fatalf("expected canonical registry snapshot to resolve")
	}
	if source != "registry_snapshot" {
		t.Fatalf("unexpected source: got=%q want=registry_snapshot", source)
	}
	wantHash := ValidatorRegistrySnapshotHash(canonical)
	if !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, wantHash)
	}
	if _, ok := got["B"]; !ok || len(got) != 1 {
		t.Fatalf("expected canonical registry payload, got=%v", got)
	}
}

func TestResolveCommittedValidatorRegistrySnapshotRepairsFromCommittedHistoricalSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	validatorSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	validatorSetHash := validatorSetHashFromSnapshotForHeight(2, validatorSet, registry)
	ledger := NewLedger()
	block2 := Block{
		ID:                    2,
		BlockHash:             "block-2",
		PrevHash:              "block-1",
		Type:                  BlockTypeTime,
		Proposer:              "A",
		ValidatorSetHash:      validatorSetHash,
		ValidatorRegistryHash: hash,
		NextValidatorSetHash:  validatorSetHash,
		BlockTime:             LogicalTimeForEpoch(2),
	}
	block2.Timestamp = int64(SystemTimeUnits(block2.BlockTime))
	block2.StateRoot = ComputeExecHashVersioned(block2, HashLedger(ledger), executionStateRootVersionForHeight(block2.ID))
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:               SnapshotVersion,
		Height:                2,
		BlockHash:             block2.BlockHash,
		StateRoot:             block2.StateRoot,
		StateMerkleRoot:       LedgerStateMerkleRoot(ledger),
		LedgerHash:            HashLedger(ledger),
		Ledger:                ledger.Clone(),
		Validators:            map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:      validatorSetHash,
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: hash,
		GenesisHash:           GenesisHash,
	})
	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		block2,
		{ID: 3, BlockHash: "block-3"},
	})

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2)
	if !ok {
		t.Fatalf("expected historical committed snapshot repair to resolve")
	}
	if source != "committed_snapshot_repair" {
		t.Fatalf("unexpected source: got=%q want=committed_snapshot_repair", source)
	}
	if !strings.EqualFold(gotHash, hash) {
		t.Fatalf("unexpected repaired hash: got=%q want=%q", gotHash, hash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected repaired registry size: got=%d want=%d", len(got), len(registry))
	}
	if !n.registrySnapshotExists(2) {
		t.Fatalf("expected historical committed snapshot repair to persist registry snapshot")
	}
}

func TestResolveCommittedValidatorRegistrySnapshotRepairsLiveTipFromMatchingRuntime(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	withValidatorRegistryTestState(t, registry)

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2", ValidatorRegistryHash: hash},
	})

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2)
	if !ok {
		t.Fatalf("expected live-tip repair to resolve")
	}
	if source != "live_tip_runtime_repair" {
		t.Fatalf("unexpected source: got=%q want=live_tip_runtime_repair", source)
	}
	if !strings.EqualFold(gotHash, hash) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, hash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected registry size: got=%d want=%d", len(got), len(registry))
	}
	if !n.registrySnapshotExists(2) {
		t.Fatalf("expected repaired registry snapshot to be persisted at height 2")
	}
}

func TestResolveCommittedValidatorRegistrySnapshotRejectsHashMismatch(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2", ValidatorRegistryHash: "different-registry-hash"},
	})

	if got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2); ok {
		t.Fatalf("expected hash mismatch to reject repair, got=%v hash=%q source=%q", got, gotHash, source)
	}
	if n.registrySnapshotExists(2) {
		t.Fatalf("expected no registry snapshot to be persisted on mismatch")
	}
}

func TestResolveCommittedValidatorRegistrySnapshotCarriesForwardCommittedMatch(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 2, registry)

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2"},
		{ID: 3, BlockHash: "block-3", ValidatorRegistryHash: hash},
	})

	before := n.observabilityStatsSnapshot().SnapshotLoadTotal
	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(3)
	after := n.observabilityStatsSnapshot().SnapshotLoadTotal
	if !ok {
		t.Fatalf("expected committed carry-forward repair to resolve")
	}
	if source != "committed_carry_forward_repair" {
		t.Fatalf("unexpected source: got=%q want=committed_carry_forward_repair", source)
	}
	if !strings.EqualFold(strings.TrimSpace(gotHash), strings.TrimSpace(hash)) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, hash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected registry size: got=%d want=%d", len(got), len(registry))
	}
	if !n.registrySnapshotExists(3) {
		t.Fatalf("expected carried-forward snapshot to be persisted at height 3")
	}
	if after != before {
		t.Fatalf("carry-forward fast path loaded full snapshot: before=%d after=%d", before, after)
	}
}

func TestCatchupRegistryCarryForwardSkipsDuplicateHeightWrite(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 2, registry)

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2"},
		{ID: 3, BlockHash: "block-3", ValidatorRegistryHash: hash},
	})
	n.Consensus = &ConsensusState{Syncing: true}

	if err := n.deterministicPersistRegistrySnapshot(3, registry, hash); err != nil {
		t.Fatalf("deterministic persist during catch-up: %v", err)
	}
	if n.registrySnapshotExists(3) {
		t.Fatalf("catch-up persist should avoid duplicate carry-forward snapshot write")
	}

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(3)
	if !ok {
		t.Fatalf("expected carry-forward resolver to work without duplicate write")
	}
	if source != "committed_carry_forward_repair" {
		t.Fatalf("unexpected source: got=%q want=committed_carry_forward_repair", source)
	}
	if !strings.EqualFold(strings.TrimSpace(gotHash), strings.TrimSpace(hash)) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, hash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected registry size: got=%d want=%d", len(got), len(registry))
	}
	if n.registrySnapshotExists(3) {
		t.Fatalf("catch-up resolver should avoid duplicate carry-forward snapshot write")
	}
}

func TestResolveCommittedValidatorRegistrySnapshotMarksFutureBlockPending(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2"},
	})

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(3)
	if ok {
		t.Fatalf("expected future registry lookup to remain unresolved, got=%v hash=%q source=%q", got, gotHash, source)
	}
	if source != "pending_future_block" {
		t.Fatalf("unexpected source: got=%q want=pending_future_block", source)
	}
	if gotHash != "" {
		t.Fatalf("expected empty hash for pending future block, got=%q", gotHash)
	}
	if len(got) != 0 {
		t.Fatalf("expected no registry snapshot for pending future block, got=%v", got)
	}
}

func TestValidatorRegistrySnapshotForHeightRepairsCommittedParentAtLiveTip(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	withValidatorRegistryTestState(t, registry)

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2", ValidatorRegistryHash: hash},
	})

	got := n.validatorRegistrySnapshotForHeight(3)
	if len(got) != len(registry) {
		t.Fatalf("expected parent registry repair for height 3 lookup: got=%d want=%d", len(got), len(registry))
	}
	if !n.registrySnapshotExists(2) {
		t.Fatalf("expected height 2 registry snapshot to be backfilled")
	}
}

func TestValidatorRegistrySnapshotForHeightDoesNotRepairHistoricalParent(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	withValidatorRegistryTestState(t, registry)
	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2", ValidatorRegistryHash: hash},
		{ID: 3, BlockHash: "block-3"},
	})

	got := n.validatorRegistrySnapshotForHeight(3)
	if len(got) != 0 {
		t.Fatalf("expected no historical runtime repair for parent height 2, got=%v", got)
	}
	if n.registrySnapshotExists(2) {
		t.Fatalf("expected no backfill for historical parent height")
	}
}

func TestDeterministicPreCommitRegistrySnapshotAllowsHeaderContinuityWithoutPersistingRuntime(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	committed := testValidatorSetMaterializationRegistry()
	committedHash := ValidatorRegistrySnapshotHash(committed)
	runtime := copyValidatorRegistrySnapshot(committed)
	rec := runtime["A"]
	rec.Stake++
	runtime["A"] = rec
	runtimeHash := ValidatorRegistrySnapshotHash(runtime)
	if strings.EqualFold(runtimeHash, committedHash) {
		t.Fatalf("test fixture must have mismatched runtime and committed registry hashes")
	}
	withValidatorRegistryTestState(t, runtime)

	parent := Block{ID: 93000, BlockHash: "block-93000", ValidatorRegistryHash: committedHash}
	child := Block{ID: 93001, BlockHash: "block-93001", PrevHash: parent.BlockHash, ValidatorRegistryHash: committedHash}
	n := testNodeWithRegistryBlocks(db, []Block{parent})

	got, source, err := n.deterministicPreCommitRegistrySnapshot(child)
	if err != nil {
		t.Fatalf("expected header-continuity precommit repair, got error: %v", err)
	}
	if source != preCommitRegistrySourceChainHeaderContinuity {
		t.Fatalf("unexpected source: got=%q want=%q", source, preCommitRegistrySourceChainHeaderContinuity)
	}
	if gotHash := ValidatorRegistrySnapshotHash(got); !strings.EqualFold(gotHash, runtimeHash) {
		t.Fatalf("expected runtime registry to remain unadopted, got hash=%q want runtime=%q", gotHash, runtimeHash)
	}
	if !shouldSkipPreCommitRegistryPersistence(source, got, committedHash) {
		t.Fatalf("expected mismatched continuity source to skip durable registry persistence")
	}
	if n.registrySnapshotExists(child.ID) {
		t.Fatalf("continuity repair must not persist a mismatched runtime registry")
	}
}

func TestDeterministicPreCommitRegistrySnapshotRejectsHeaderContinuityWithRegistryMutation(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	committed := testValidatorSetMaterializationRegistry()
	committedHash := ValidatorRegistrySnapshotHash(committed)
	runtime := copyValidatorRegistrySnapshot(committed)
	rec := runtime["A"]
	rec.Stake++
	runtime["A"] = rec
	withValidatorRegistryTestState(t, runtime)

	parent := Block{ID: 93000, BlockHash: "block-93000", ValidatorRegistryHash: committedHash}
	child := Block{
		ID:                    93001,
		BlockHash:             "block-93001",
		PrevHash:              parent.BlockHash,
		ValidatorRegistryHash: committedHash,
		Transactions:          []Transaction{{Type: TxStake, To: "A", Amount: 1}},
	}
	n := testNodeWithRegistryBlocks(db, []Block{parent})

	if got, source, err := n.deterministicPreCommitRegistrySnapshot(child); err == nil {
		t.Fatalf("expected registry mutation to reject header-continuity shortcut, got=%v source=%q", got, source)
	}
}

func TestResolveCommittedValidatorRegistrySnapshotFallsBackToStoredBlock(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	n := testNodeWithRegistryBlocks(db, []Block{{ID: 21, BlockHash: "tip-21"}})
	if err := n.storeValidatorRegistrySnapshotRecord(10, registry); err != nil {
		t.Fatalf("store carry-forward registry snapshot: %v", err)
	}
	block20 := Block{ID: 20, BlockHash: "block-20", ValidatorRegistryHash: hash}
	if err := db.StoreBlock(block20); err != nil {
		t.Fatalf("store block 20: %v", err)
	}

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(20)
	if !ok {
		t.Fatalf("expected registry snapshot resolved from stored block commitment")
	}
	if source != "committed_carry_forward_repair" {
		t.Fatalf("unexpected source: got=%q", source)
	}
	if !strings.EqualFold(gotHash, hash) {
		t.Fatalf("unexpected hash: got=%q want=%q", gotHash, hash)
	}
	if len(got) != len(registry) {
		t.Fatalf("unexpected registry size: got=%d want=%d", len(got), len(registry))
	}
}

func TestChainParentCommittedValidatorSetHashFallsBackToStoredBlock(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	parentHash := strings.Repeat("a", 64)
	parentNextHash := strings.Repeat("b", 64)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{
			{ID: 1, BlockHash: "genesis"},
		}},
	}
	n.StoreBlock(Block{
		ID:                   90553,
		Height:               90553,
		BlockHash:            parentHash,
		PrevHash:             strings.Repeat("c", 64),
		ValidatorSetHash:     strings.Repeat("d", 64),
		NextValidatorSetHash: parentNextHash,
	})

	got, ok := n.chainParentCommittedValidatorSetHash(90554)
	if !ok {
		t.Fatal("expected parent commitment from stored block")
	}
	if got != parentNextHash {
		t.Fatalf("unexpected parent commitment: got=%q want=%q", got, parentNextHash)
	}
}

func TestValidatorRegistrySnapshotForHeightDoesNotScheduleFutureParentRebuild(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2"},
	})

	got := n.validatorRegistrySnapshotForHeight(10)
	if len(got) != 0 {
		t.Fatalf("expected unresolved future parent registry, got=%v", got)
	}

	n.registryHistoryRebuildMu.Lock()
	scheduled := n.registryHistoryRebuildLastScheduledHeight
	target := n.registryHistoryRebuildTarget
	n.registryHistoryRebuildMu.Unlock()
	if scheduled != 0 || target != 0 {
		t.Fatalf("future parent lookup scheduled registry rebuild: scheduled=%d target=%d", scheduled, target)
	}
}

func TestValidatorRegistrySnapshotForHeightRepairsHistoricalParentFromCommittedSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	validatorSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	validatorSetHash := validatorSetHashFromSnapshotForHeight(2, validatorSet, registry)
	ledger := NewLedger()
	block2 := Block{
		ID:                    2,
		BlockHash:             "block-2",
		PrevHash:              "block-1",
		Type:                  BlockTypeTime,
		Proposer:              "A",
		ValidatorSetHash:      validatorSetHash,
		ValidatorRegistryHash: hash,
		NextValidatorSetHash:  validatorSetHash,
		BlockTime:             LogicalTimeForEpoch(2),
	}
	block2.Timestamp = int64(SystemTimeUnits(block2.BlockTime))
	block2.StateRoot = ComputeExecHashVersioned(block2, HashLedger(ledger), executionStateRootVersionForHeight(block2.ID))
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:               SnapshotVersion,
		Height:                2,
		BlockHash:             block2.BlockHash,
		StateRoot:             block2.StateRoot,
		StateMerkleRoot:       LedgerStateMerkleRoot(ledger),
		LedgerHash:            HashLedger(ledger),
		Ledger:                ledger.Clone(),
		Validators:            map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:      validatorSetHash,
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: hash,
		GenesisHash:           GenesisHash,
	})

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		block2,
		{ID: 3, BlockHash: "block-3"},
	})

	got := n.validatorRegistrySnapshotForHeight(3)
	if len(got) != len(registry) {
		t.Fatalf("expected committed historical snapshot repair for parent height: got=%d want=%d", len(got), len(registry))
	}
	if !n.registrySnapshotExists(2) {
		t.Fatalf("expected historical committed snapshot repair to backfill parent height")
	}
}

func TestValidatorRegistrySnapshotForHeightUsesCurrentSparseAnchorSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	vhash := ValidatorSetHash(validators)
	ledger := NewLedger()
	anchor := Block{
		ID:                    10,
		Height:                10,
		BlockHash:             "",
		PrevHash:              "block-9",
		Type:                  BlockTypeTime,
		Proposer:              "A",
		ValidatorSetHash:      vhash,
		NextValidatorSetHash:  vhash,
		ValidatorRegistryHash: hash,
		BlockTime:             LogicalTimeForEpoch(10),
	}
	anchor.Timestamp = int64(SystemTimeUnits(anchor.BlockTime))
	anchor.StateRoot = ComputeExecHashVersioned(anchor, HashLedger(ledger), executionStateRootVersionForHeight(anchor.ID))
	anchor.BlockHash = HashBlock(anchor)
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:               SnapshotVersion,
		Height:                anchor.ID,
		BlockHash:             anchor.BlockHash,
		PrevHash:              anchor.PrevHash,
		StateRoot:             anchor.StateRoot,
		StateMerkleRoot:       LedgerStateMerkleRoot(ledger),
		LedgerHash:            HashLedger(ledger),
		LedgerStage:           snapshotLedgerStageExecution,
		Ledger:                ledger.Clone(),
		Validators:            map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:      vhash,
		NextValidatorSetHash:  vhash,
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: hash,
		GenesisHash:           GenesisHash,
	})
	staleParent := copyValidatorRegistrySnapshot(registry)
	stale := staleParent["A"]
	stale.Stake++
	staleParent["A"] = stale
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, anchor.ID-1, staleParent)

	n := testNodeWithRegistryBlocks(db, []Block{anchor})
	got := n.validatorRegistrySnapshotForHeight(anchor.ID)
	if gotHash := ValidatorRegistrySnapshotHash(got); !strings.EqualFold(gotHash, hash) {
		t.Fatalf("expected sparse anchor registry hash %q, got %q", hash, gotHash)
	}
	if gotHash := ValidatorRegistrySnapshotHash(got); strings.EqualFold(gotHash, ValidatorRegistrySnapshotHash(staleParent)) {
		t.Fatalf("sparse anchor fallback must reject an unprovable persisted parent registry snapshot")
	}
	parent, parentHash, parentSource, ok := n.resolveCommittedValidatorRegistrySnapshot(anchor.ID - 1)
	if !ok {
		t.Fatalf("expected sparse anchor parent projection to resolve")
	}
	if parentSource != "sparse_anchor_parent_projection" {
		t.Fatalf("unexpected parent projection source: got=%q", parentSource)
	}
	if !strings.EqualFold(parentHash, hash) {
		t.Fatalf("unexpected parent projection hash: got=%q want=%q", parentHash, hash)
	}
	if gotHash := ValidatorRegistrySnapshotHash(parent); strings.EqualFold(gotHash, ValidatorRegistrySnapshotHash(staleParent)) {
		t.Fatalf("sparse anchor parent projection must reject stale persisted parent registry snapshot")
	}
	stillStale, err := n.loadValidatorRegistrySnapshot(anchor.ID - 1)
	if err != nil {
		t.Fatalf("expected stale parent fixture to remain loadable: %v", err)
	}
	if gotHash := ValidatorRegistrySnapshotHash(stillStale); !strings.EqualFold(gotHash, ValidatorRegistrySnapshotHash(staleParent)) {
		t.Fatalf("sparse anchor parent projection must not overwrite parent snapshot: got=%q want=%q", gotHash, ValidatorRegistrySnapshotHash(staleParent))
	}
	next := Block{
		ID:                    anchor.ID + 1,
		Height:                anchor.ID + 1,
		BlockHash:             "block-11",
		PrevHash:              anchor.BlockHash,
		ValidatorSetHash:      vhash,
		NextValidatorSetHash:  vhash,
		ValidatorRegistryHash: hash,
	}
	n.Blockchain.Blocks = []Block{anchor, next}
	advancedParent, advancedHash, advancedSource, ok := n.resolveCommittedValidatorRegistrySnapshot(anchor.ID - 1)
	if !ok {
		t.Fatalf("expected advanced sparse anchor parent projection to resolve")
	}
	if advancedSource != "sparse_anchor_parent_projection" {
		t.Fatalf("unexpected advanced parent projection source: got=%q", advancedSource)
	}
	if !strings.EqualFold(advancedHash, hash) {
		t.Fatalf("unexpected advanced parent projection hash: got=%q want=%q", advancedHash, hash)
	}
	if gotHash := ValidatorRegistrySnapshotHash(advancedParent); strings.EqualFold(gotHash, ValidatorRegistrySnapshotHash(staleParent)) {
		t.Fatalf("advanced sparse anchor parent projection must reject stale persisted parent registry snapshot")
	}
}

func TestSparseAnchorParentProjectionRepairsFromAppliedRuntimeRegistry(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	hash := ValidatorRegistrySnapshotHash(registry)
	withValidatorRegistryTestState(t, registry)

	anchor := Block{
		ID:                    93000,
		Height:                93000,
		BlockHash:             "snapshot-anchor-93000",
		PrevHash:              "block-92999",
		ValidatorRegistryHash: hash,
	}
	n := testNodeWithRegistryBlocks(db, []Block{anchor})

	parent, parentHash, parentSource, ok := n.resolveCommittedValidatorRegistrySnapshot(anchor.ID - 1)
	if !ok {
		t.Fatalf("expected sparse anchor parent projection to resolve from applied runtime registry")
	}
	if parentSource != "sparse_anchor_parent_projection" {
		t.Fatalf("unexpected parent projection source: got=%q", parentSource)
	}
	if !strings.EqualFold(parentHash, hash) {
		t.Fatalf("unexpected parent projection hash: got=%q want=%q", parentHash, hash)
	}
	if gotHash := ValidatorRegistrySnapshotHash(parent); !strings.EqualFold(gotHash, hash) {
		t.Fatalf("unexpected projected registry hash: got=%q want=%q", gotHash, hash)
	}
	anchored, err := n.loadValidatorRegistrySnapshot(anchor.ID)
	if err != nil {
		t.Fatalf("expected runtime projection to persist anchor registry snapshot: %v", err)
	}
	if gotHash := ValidatorRegistrySnapshotHash(anchored); !strings.EqualFold(gotHash, hash) {
		t.Fatalf("unexpected persisted anchor registry hash: got=%q want=%q", gotHash, hash)
	}
	if _, err := n.loadValidatorRegistrySnapshot(anchor.ID - 1); err == nil {
		t.Fatalf("sparse parent projection must not overwrite the absent parent height")
	}
}

func TestPersistValidatorRegistrySnapshotRepairsGenesisBootstrapProjection(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	installValidatorUpdateRegistry(t)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2"},
	})
	n.GenesisValidators = []string{"A", "B", "C", "D"}

	runtime := GlobalValidatorRegistry.Snapshot()
	want, wantHash, ok := n.genesisCommittedValidatorRegistryCandidate(runtime)
	if !ok {
		t.Fatalf("expected genesis repair candidate")
	}
	if strings.EqualFold(ValidatorRegistrySnapshotHash(runtime), wantHash) {
		t.Fatalf("expected runtime registry hash mismatch to require repair")
	}

	n.Blockchain.Blocks[1].ValidatorRegistryHash = wantHash
	n.persistValidatorRegistrySnapshotFromSource(2, runtime)

	got, err := n.loadValidatorRegistrySnapshot(2)
	if err != nil {
		t.Fatalf("expected repaired registry snapshot at height 2: %v", err)
	}
	if gotHash := ValidatorRegistrySnapshotHash(got); !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected repaired hash: got=%q want=%q", gotHash, wantHash)
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected repaired validator count: got=%d want=%d", len(got), len(want))
	}
	if got["A"].Stake != ValidatorMinStake {
		t.Fatalf("expected canonical repaired stake for A: got=%d want=%d", got["A"].Stake, ValidatorMinStake)
	}
	if got["A"].JoinHeight != 0 {
		t.Fatalf("expected canonical repaired join height for A: got=%d want=0", got["A"].JoinHeight)
	}
}

func TestResolveCommittedValidatorRegistrySnapshotRepairsFromGenesisBootstrapProjection(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	installValidatorUpdateRegistry(t)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := testNodeWithRegistryBlocks(db, []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, BlockHash: "block-2"},
	})
	n.GenesisValidators = []string{"A", "B", "C", "D"}

	runtime := GlobalValidatorRegistry.Snapshot()
	_, wantHash, ok := n.genesisCommittedValidatorRegistryCandidate(runtime)
	if !ok {
		t.Fatalf("expected genesis repair candidate")
	}
	n.Blockchain.Blocks[1].ValidatorRegistryHash = wantHash

	got, gotHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(2)
	if !ok {
		t.Fatalf("expected genesis bootstrap repair to resolve committed snapshot")
	}
	if source != "genesis_bootstrap_repair" {
		t.Fatalf("unexpected source: got=%q want=genesis_bootstrap_repair", source)
	}
	if !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected repaired hash: got=%q want=%q", gotHash, wantHash)
	}
	if len(got) != 4 {
		t.Fatalf("expected repaired genesis validator set only, got=%d", len(got))
	}
	if !n.registrySnapshotExists(2) {
		t.Fatalf("expected repaired registry snapshot to be persisted")
	}
}

func TestValidatorRegistrySnapshotForHeightUsesGenesisBootstrapProjectionAtEarlyHeight(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	installValidatorUpdateRegistry(t)

	n := &Node{
		Blockchain:        &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "block-1"}, {ID: 2, BlockHash: "block-2"}}},
		GenesisValidators: []string{"A", "B", "C", "D"},
	}

	runtime := GlobalValidatorRegistry.Snapshot()
	_, wantHash, ok := n.genesisCommittedValidatorRegistryCandidate(runtime)
	if !ok {
		t.Fatalf("expected genesis repair candidate")
	}
	n.Blockchain.Blocks[0].ValidatorRegistryHash = wantHash

	got := n.validatorRegistrySnapshotForHeight(2)
	if gotHash := ValidatorRegistrySnapshotHash(got); !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected early-height registry hash: got=%q want=%q", gotHash, wantHash)
	}
	if len(got) != 4 {
		t.Fatalf("expected genesis bootstrap validator set only, got=%d", len(got))
	}
}

func TestCreateSnapshotUsesRepairedCommittedTipRegistry(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
	}
	hash := ValidatorRegistrySnapshotHash(registry)
	withValidatorRegistryTestState(t, registry)

	block1 := Block{
		ID:               1,
		BlockHash:        "block-1",
		Signatures:       append([]string{}, set...),
		ValidatorSetHash: ValidatorSetHash(set),
	}
	block2 := Block{
		ID:                    2,
		BlockHash:             "block-2",
		PrevHash:              "block-1",
		Signatures:            append([]string{}, set...),
		ValidatorRegistryHash: hash,
	}

	n := testNodeWithRegistryBlocks(db, []Block{block1, block2})
	n.GenesisValidators = append([]string{}, set...)

	if err := n.CreateSnapshot(2, block2.BlockHash); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if !n.registrySnapshotExists(2) {
		t.Fatalf("expected committed registry snapshot to exist after snapshot creation")
	}
	snap, err := n.GetSnapshot(2)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if len(snap.ValidatorRegistry) != len(registry) {
		t.Fatalf("expected snapshot registry to be populated: got=%d want=%d", len(snap.ValidatorRegistry), len(registry))
	}
	if !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(snap)), hash) {
		t.Fatalf("unexpected snapshot registry hash: got=%q want=%q", snapshotValidatorRegistryHash(snap), hash)
	}
}
