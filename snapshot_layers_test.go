package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func makeSnapshotLayerFixture(height uint64, prevHash string, ledger Ledger, registry map[string]ValidatorRecord) (Block, StateSnapshot) {
	if registry == nil {
		registry = testValidatorSetMaterializationRegistry()
	}
	registry = copyValidatorRegistrySnapshot(registry)
	for i, id := range []string{"A", "B", "C", "D", "F"} {
		rec, ok := registry[id]
		if !ok {
			continue
		}
		if rec.ConsensusPubKey == "" {
			key := strictActivationTestValidatorKey(byte(31+i), id)
			rec.ConsensusPubKey = hex.EncodeToString(key.PublicKey)
			registry[id] = rec
		}
	}
	if ledger.Balances == nil {
		ledger = NewLedger()
	}
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	setHash := validatorSetHashFromSnapshotForHeight(height, validators, registry)
	setRoot := ValidatorSetMerkleRoot(height, validators, registry)
	block := Block{
		ID:                     height,
		BlockHash:              fmt.Sprintf("block-%d", height),
		PrevHash:               prevHash,
		ValidatorSetHash:       setHash,
		ValidatorSetRoot:       setRoot,
		NextValidatorSetHash:   setHash,
		NextValidatorSetRoot:   setRoot,
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
		NextValidatorSetHeight: height + 1,
		ActivationHeight:       height + 1,
		Timestamp:              time.Now().Unix(),
	}
	ledgerHash := HashLedger(ledger)
	block.StateRoot = ComputeExecHash(block, ledgerHash)
	snapshot := StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 height,
		BlockHash:              block.BlockHash,
		StateRoot:              block.StateRoot,
		Ledger:                 ledger.Clone(),
		LedgerHash:             ledgerHash,
		GenesisHash:            GenesisHash,
		PrevHash:               prevHash,
		Validators:             map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:       setHash,
		ValidatorSetRoot:       setRoot,
		NextValidatorSetHash:   setHash,
		NextValidatorSetRoot:   setRoot,
		NextValidatorSetHeight: height + 1,
		ActivationHeight:       height + 1,
		ValidatorRegistry:      copyValidatorRegistrySnapshot(registry),
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
		StateValidators:        onChainValidatorsFromRegistrySnapshot(registry, nil, height),
		Timestamp:              block.Timestamp,
	}
	populateSnapshotDerivedFields(&snapshot)
	setRoot = ValidatorSetMerkleRoot(snapshot.Height, validatorsFromSnapshot(&snapshot), snapshot.ValidatorRegistry)
	snapshot.ValidatorSetRoot = setRoot
	snapshot.NextValidatorSetRoot = setRoot
	block.ValidatorSetRoot = setRoot
	block.NextValidatorSetRoot = setRoot
	snapshot.SnapshotHash = snapshotCanonicalHash(&snapshot)
	return block, snapshot
}

func TestSnapshotCanonicalHashIgnoresLiveTransitionBookkeeping(t *testing.T) {
	_, base := makeSnapshotLayerFixture(30, "block-29", NewLedger(), testValidatorSetMaterializationRegistry())
	base.ValidatorSetHeight = 30
	base.PendingValidators = map[string]uint64{
		"E": 41,
	}
	base.PendingValidatorRemovals = map[string]uint64{
		"B": 44,
	}
	populateSnapshotDerivedFields(&base)

	mutated := base
	mutated.ValidatorSetHeight = 999
	mutated.PendingValidators = map[string]uint64{
		"F": 77,
	}
	mutated.PendingValidatorRemovals = map[string]uint64{
		"C": 88,
	}
	populateSnapshotDerivedFields(&mutated)

	want := snapshotCanonicalHash(&base)
	got := snapshotCanonicalHash(&mutated)
	if got != want {
		t.Fatalf("snapshot hash should ignore live transition bookkeeping: got=%q want=%q", got, want)
	}
}

func TestResolveCommittedStateSnapshotRepairsTipSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, testValidatorSetMaterializationRegistry())

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeTipSnapshotRecords(&snap2, "test_tip"); err != nil {
		t.Fatalf("store tip snapshot: %v", err)
	}

	got, gotHash, source, ok := n.ResolveCommittedStateSnapshot(2)
	if !ok || got == nil {
		t.Fatalf("expected committed state snapshot repair at tip")
	}
	if source != "tip_snapshot_repair" {
		t.Fatalf("unexpected source: got=%q want=tip_snapshot_repair", source)
	}
	if gotHash != snap2.SnapshotHash {
		t.Fatalf("unexpected snapshot hash: got=%q want=%q", gotHash, snap2.SnapshotHash)
	}
	if _, err := n.GetSnapshot(2); err != nil {
		t.Fatalf("expected repaired committed snapshot persisted: %v", err)
	}
	if meta, err := n.loadSnapshotMetaRecord(2); err != nil || meta == nil {
		t.Fatalf("expected committed snapshot meta persisted, err=%v", err)
	}
}

func TestResolveCommittedStateSnapshotDoesNotUseTipForHistoricalHeight(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, NewLedger(), testValidatorSetMaterializationRegistry())

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeTipSnapshotRecords(&snap2, "test_tip"); err != nil {
		t.Fatalf("store tip snapshot: %v", err)
	}

	if got, gotHash, source, ok := n.ResolveCommittedStateSnapshot(1); ok || got != nil || gotHash != "" || source != "none" {
		t.Fatalf("expected no historical tip fallback, got=%v hash=%q source=%q ok=%t", got, gotHash, source, ok)
	}
}

func TestResolveCommittedStateSnapshotMaterializesTipByCreate(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.Signatures = append([]string{}, set...)
	block2.StateRoot = ComputeExecHash(block2, HashLedger(ledger))

	n := &Node{
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
	}

	got, gotHash, source, ok := n.ResolveCommittedStateSnapshot(2)
	if !ok || got == nil {
		t.Fatalf("expected tip materialization via create path")
	}
	if source != "tip_create_snapshot_repair" {
		t.Fatalf("unexpected source: got=%q want=tip_create_snapshot_repair", source)
	}
	if got.Height != 2 || gotHash == "" {
		t.Fatalf("unexpected resolved snapshot: height=%d hash=%q", got.Height, gotHash)
	}
	if _, err := n.GetSnapshot(2); err != nil {
		t.Fatalf("expected committed snapshot persisted at tip: %v", err)
	}
}

func TestEnsureCommittedTipStateSnapshotDefersOutsideCheckpointInterval(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	prevInterval := SyncCheckpointIntervalBlocks
	SyncCheckpointIntervalBlocks = 32
	defer func() { SyncCheckpointIntervalBlocks = prevInterval }()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block3, _ := makeSnapshotLayerFixture(3, block2.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block3.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})

	n := &Node{
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2, block3}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
	}

	source, ok := n.ensureCommittedTipStateSnapshot(3, "test_checkpoint_defer")
	if !ok {
		t.Fatalf("expected checkpoint deferral to be non-fatal")
	}
	if source != "checkpoint_interval_deferred" {
		t.Fatalf("unexpected source: got=%q want=%q", source, "checkpoint_interval_deferred")
	}
	if _, err := n.GetSnapshot(3); err == nil {
		t.Fatalf("expected no tip snapshot at non-checkpoint height")
	}
}

func TestEnsureCommittedTipStateSnapshotMaterializesTipForResolverReason(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	prevInterval := SyncCheckpointIntervalBlocks
	SyncCheckpointIntervalBlocks = 32
	defer func() { SyncCheckpointIntervalBlocks = prevInterval }()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block3, _ := makeSnapshotLayerFixture(3, block2.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block3.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block3.StateRoot = ComputeExecHash(block3, HashLedger(ledger))

	n := &Node{
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2, block3}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
	}

	source, ok := n.ensureCommittedTipStateSnapshot(3, "resolver_tip_missing")
	if !ok {
		t.Fatalf("expected resolver-triggered tip materialization to succeed")
	}
	if source != "tip_create_snapshot_repair" {
		t.Fatalf("unexpected source: got=%q want=%q", source, "tip_create_snapshot_repair")
	}
	if _, err := n.GetSnapshot(3); err != nil {
		t.Fatalf("expected tip snapshot persisted after resolver materialization: %v", err)
	}
}

func TestShouldPeerFetchCommittedTipSnapshotReasonGating(t *testing.T) {
	if !shouldPeerFetchCommittedTipSnapshot("resolver_tip_missing") {
		t.Fatalf("expected resolver reason to allow peer fetch")
	}
	if !shouldPeerFetchCommittedTipSnapshot("snapshot_create_worker") {
		t.Fatalf("expected snapshot worker reason to allow peer fetch")
	}
	if shouldPeerFetchCommittedTipSnapshot("test_checkpoint_defer") {
		t.Fatalf("unexpected peer fetch allowance for unrelated reason")
	}
}

func TestFetchCommittedTipSnapshotFromPeersSkipsWithoutHost(t *testing.T) {
	n := &Node{}
	source, ok := n.fetchCommittedTipSnapshotFromPeers(100, "resolver_tip_missing")
	if ok {
		t.Fatalf("expected no peer fetch without host")
	}
	if source != "none" {
		t.Fatalf("unexpected source: got=%q want=none", source)
	}
}

func TestCreateCommittedTipSnapshotForceCreatesTip(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.StateRoot = ComputeExecHash(block2, HashLedger(ledger))

	n := &Node{
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
	}

	snapshot, meta, source, err := n.createCommittedTipSnapshot("test_force_create", true)
	if err != nil {
		t.Fatalf("create committed tip snapshot: %v", err)
	}
	if snapshot == nil || meta == nil {
		t.Fatalf("expected snapshot + meta, got snapshot=%v meta=%v", snapshot, meta)
	}
	if source != "create_snapshot" {
		t.Fatalf("unexpected source: got=%q want=create_snapshot", source)
	}
	if snapshot.Height != 2 || meta.Height != 2 {
		t.Fatalf("unexpected snapshot height: snapshot=%d meta=%d", snapshot.Height, meta.Height)
	}
	if snapshot.ValidatorSetSource == "" {
		t.Fatalf("expected snapshot validator-set source to be populated")
	}
	if meta.ValidatorSetSource != snapshot.ValidatorSetSource {
		t.Fatalf("expected snapshot meta validator-set source to mirror snapshot: got=%q want=%q", meta.ValidatorSetSource, snapshot.ValidatorSetSource)
	}
	if _, err := n.GetSnapshot(2); err != nil {
		t.Fatalf("expected created snapshot persisted: %v", err)
	}
}

func TestCreateSnapshotUsesCachedExecutionLedgerForAnchorState(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	execLedger := NewLedger()
	execLedger.Balances["alice"] = 10
	runtimeLedger := execLedger.Clone()
	runtimeLedger.Balances["treasury"] = 999 // Simulate post-commit side effects.

	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, execLedger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.StateRoot = ComputeExecHash(block2, HashLedger(execLedger))

	n := &Node{
		DB:                db,
		Ledger:            runtimeLedger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
	}
	n.cacheExecutionSnapshotLedger(2, execLedger)

	if err := n.CreateSnapshot(2, block2.BlockHash); err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	snapshot, err := n.GetSnapshot(2)
	if err != nil || snapshot == nil {
		t.Fatalf("GetSnapshot failed: %v", err)
	}
	if snapshot.StateRoot != block2.StateRoot {
		t.Fatalf("state root mismatch: got=%q want=%q", snapshot.StateRoot, block2.StateRoot)
	}
	if snapshot.LedgerHash != HashLedger(execLedger) {
		t.Fatalf("expected cached execution ledger hash, got=%q want=%q", snapshot.LedgerHash, HashLedger(execLedger))
	}
	if snapshot.LedgerHash == HashLedger(runtimeLedger) {
		t.Fatalf("snapshot incorrectly used runtime post-effects ledger")
	}
}

func TestLatestCommittedSnapshotMetaReturnsLatestStoredSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	block1, snap1 := makeSnapshotLayerFixture(1, "", NewLedger(), registry)
	ledger2 := NewLedger()
	ledger2.Balances["bob"] = 7
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, registry)

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test_commit_1"); err != nil {
		t.Fatalf("store committed snapshot 1: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap2, "test_commit_2"); err != nil {
		t.Fatalf("store committed snapshot 2: %v", err)
	}

	snapshot, meta, source, err := n.latestCommittedSnapshotMeta()
	if err != nil {
		t.Fatalf("latest committed snapshot meta: %v", err)
	}
	if snapshot == nil || meta == nil {
		t.Fatalf("expected latest snapshot + meta, got snapshot=%v meta=%v", snapshot, meta)
	}
	if snapshot.Height != 2 || meta.Height != 2 {
		t.Fatalf("unexpected latest height: snapshot=%d meta=%d", snapshot.Height, meta.Height)
	}
	if source != "test_commit_2" {
		t.Fatalf("unexpected latest source: got=%q want=test_commit_2", source)
	}
	if meta.SnapshotHash != snap2.SnapshotHash {
		t.Fatalf("unexpected latest snapshot hash: got=%q want=%q", meta.SnapshotHash, snap2.SnapshotHash)
	}
}

func TestStoreCommittedStateSnapshotRecordWritesMetaAndTip(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block1, snap1 := makeSnapshotLayerFixture(1, "", NewLedger(), testValidatorSetMaterializationRegistry())
	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test_commit"); err != nil {
		t.Fatalf("store committed snapshot: %v", err)
	}

	meta, err := n.loadSnapshotMetaRecord(1)
	if err != nil {
		t.Fatalf("load snapshot meta: %v", err)
	}
	if meta.SnapshotHash != snap1.SnapshotHash || meta.StateRoot != snap1.StateRoot {
		t.Fatalf("snapshot meta mismatch: got=%+v snapshot_hash=%q state_root=%q", meta, snap1.SnapshotHash, snap1.StateRoot)
	}
	tipState, err := n.loadTipSnapshotState()
	if err != nil {
		t.Fatalf("load tip state: %v", err)
	}
	if tipState.Height != 1 || tipState.Snapshot == nil || tipState.Snapshot.SnapshotHash != snap1.SnapshotHash {
		t.Fatalf("unexpected tip state record: %+v", tipState)
	}
}

func TestStoreCommittedStateSnapshotRecordExportsArtifacts(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	dataDir := t.TempDir()
	block1, snap1 := makeSnapshotLayerFixture(1, "", NewLedger(), testValidatorSetMaterializationRegistry())
	n := &Node{
		DB:         db,
		DataDir:    dataDir,
		Blockchain: &Blockchain{Blocks: []Block{block1}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test_export"); err != nil {
		t.Fatalf("store committed snapshot: %v", err)
	}

	exportDir := filepath.Join(dataDir, "snapshots", fmt.Sprintf("%020d", snap1.Height))
	if _, err := os.Stat(filepath.Join(exportDir, "meta.json")); err != nil {
		t.Fatalf("expected exported snapshot meta.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exportDir, "chunk_0000")); err != nil {
		t.Fatalf("expected exported snapshot chunk file: %v", err)
	}
}

func TestPublishRequiredValidatorSnapshotMarksValidatorHealthy(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.StateRoot = ComputeExecHash(block2, HashLedger(ledger))

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()

	ps, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		t.Fatalf("create pubsub: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}

	n := &Node{
		ID:                "A",
		Role:              "validator",
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
		Host:              host,
		PubSub:            ps,
		ValidatorKey: ValidatorKey{
			PublicKey:  pub,
			PrivateKey: priv,
		},
	}
	if err := n.initPubSubTopics(); err != nil {
		t.Fatalf("init pubsub topics: %v", err)
	}

	snapshot, err := n.publishRequiredValidatorSnapshot("test_validator_required", true)
	if err != nil {
		t.Fatalf("publish required validator snapshot: %v", err)
	}
	if snapshot == nil || snapshot.Height != 2 {
		t.Fatalf("unexpected snapshot published: %+v", snapshot)
	}

	publishedHeight, publishedHash, publishedAt, publishErr := n.validatorSnapshotPublicationState()
	if publishedHeight != 2 {
		t.Fatalf("unexpected published height: got=%d want=2", publishedHeight)
	}
	if publishedHash == "" {
		t.Fatalf("expected published snapshot hash to be recorded")
	}
	if publishedAt.IsZero() {
		t.Fatalf("expected published snapshot time to be recorded")
	}
	if publishErr != "" {
		t.Fatalf("expected empty publication error, got=%q", publishErr)
	}

	proofGroups, proofEntries, proofBestVotes := n.snapshotProofStats()
	if proofGroups < 1 || proofEntries < 1 || proofBestVotes < 1 {
		t.Fatalf("expected local proof cache populated, got groups=%d entries=%d best_votes=%d", proofGroups, proofEntries, proofBestVotes)
	}

	status := n.runtimeStatusSnapshot()
	if !status.ValidatorSnapshotRequired {
		t.Fatalf("expected validator snapshot publication to be required")
	}
	if !status.ValidatorSnapshotPublishHealthy {
		t.Fatalf("expected validator snapshot publication to be healthy, status=%+v", status)
	}
	if status.ValidatorSnapshotPublishedHeight != 2 {
		t.Fatalf("unexpected runtime published height: got=%d want=2", status.ValidatorSnapshotPublishedHeight)
	}
	if status.ValidatorSnapshotLastError != "" {
		t.Fatalf("unexpected runtime last error: %q", status.ValidatorSnapshotLastError)
	}
}

func TestPublishRequiredValidatorSnapshotForceBypassesCheckpointInterval(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.StateRoot = ComputeExecHash(block2, HashLedger(ledger))
	block3, _ := makeSnapshotLayerFixture(3, block2.BlockHash, ledger, registry)
	block3.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block3.StateRoot = ComputeExecHash(block3, HashLedger(ledger))

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()

	ps, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		t.Fatalf("create pubsub: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}

	n := &Node{
		ID:                "A",
		Role:              "validator",
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2, block3}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
		Host:              host,
		PubSub:            ps,
		ValidatorKey: ValidatorKey{
			PublicKey:  pub,
			PrivateKey: priv,
		},
	}
	if err := n.initPubSubTopics(); err != nil {
		t.Fatalf("init pubsub topics: %v", err)
	}

	deferred, err := n.publishRequiredValidatorSnapshot("test_validator_required_interval", false)
	if err != nil {
		t.Fatalf("publish required validator snapshot without force: %v", err)
	}
	if deferred != nil {
		t.Fatalf("expected checkpoint-gated publish to defer, got %+v", deferred)
	}

	snapshot, err := n.publishRequiredValidatorSnapshot("test_validator_required_force", true)
	if err != nil {
		t.Fatalf("publish required validator snapshot with force: %v", err)
	}
	if snapshot == nil || snapshot.Height != 3 {
		t.Fatalf("unexpected forced snapshot published: %+v", snapshot)
	}
}

func TestPublishRequiredValidatorSnapshotAutoReasonThrottlesOutsideInterval(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.StateRoot = ComputeExecHash(block2, HashLedger(ledger))
	block3, _ := makeSnapshotLayerFixture(3, block2.BlockHash, ledger, registry)
	block3.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block3.StateRoot = ComputeExecHash(block3, HashLedger(ledger))

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()

	ps, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		t.Fatalf("create pubsub: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}

	n := &Node{
		ID:                "A",
		Role:              "validator",
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2, block3}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
		Host:              host,
		PubSub:            ps,
		ValidatorKey: ValidatorKey{
			PublicKey:  pub,
			PrivateKey: priv,
		},
	}
	if err := n.initPubSubTopics(); err != nil {
		t.Fatalf("init pubsub topics: %v", err)
	}

	snapshot, err := n.publishRequiredValidatorSnapshot("snapshot_proof_signer", true)
	if err != nil {
		t.Fatalf("publish required validator snapshot with auto reason: %v", err)
	}
	if snapshot != nil {
		t.Fatalf("expected auto snapshot publish to throttle outside height interval, got %+v", snapshot)
	}
}

func TestAdaptiveSnapshotPublishReasonClassifiedForIntervalBypass(t *testing.T) {
	reason := "adaptive_snapshot_proof_signer_lagging_peer"
	if !isAutomaticValidatorSnapshotPublishReason(reason) {
		t.Fatalf("expected adaptive reason to remain an automatic publish reason")
	}
	if !isAdaptiveValidatorSnapshotPublishReason(reason) {
		t.Fatalf("expected adaptive reason to bypass automatic height interval when forced")
	}
	if isAdaptiveValidatorSnapshotPublishReason("snapshot_proof_signer") {
		t.Fatalf("plain snapshot proof signer should remain interval-gated")
	}
}

func TestPublishRequiredValidatorSnapshotAutoReasonPublishesOnIntervalBoundary(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	blocks := make([]Block, 0, 10)
	prevHash := ""
	for height := uint64(1); height <= 10; height++ {
		block, _ := makeSnapshotLayerFixture(height, prevHash, ledger, registry)
		block.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
		block.StateRoot = ComputeExecHash(block, HashLedger(ledger))
		blocks = append(blocks, block)
		prevHash = block.BlockHash
	}

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()

	ps, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		t.Fatalf("create pubsub: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}

	n := &Node{
		ID:                "A",
		Role:              "validator",
		DB:                db,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: blocks},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
		Host:              host,
		PubSub:            ps,
		ValidatorKey: ValidatorKey{
			PublicKey:  pub,
			PrivateKey: priv,
		},
	}
	if err := n.initPubSubTopics(); err != nil {
		t.Fatalf("init pubsub topics: %v", err)
	}

	snapshot, err := n.publishRequiredValidatorSnapshot("snapshot_proof_signer", true)
	if err != nil {
		t.Fatalf("publish required validator snapshot on interval boundary: %v", err)
	}
	if snapshot == nil || snapshot.Height != 10 {
		t.Fatalf("expected auto snapshot publish on interval boundary, got %+v", snapshot)
	}
}

func TestValidatorSnapshotAdaptivePublishDecisionPrefersNewNodeThenLaggingPeer(t *testing.T) {
	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	n.Role = "validator"
	n.Blockchain = &Blockchain{Blocks: []Block{{ID: 170, BlockHash: "block-170"}}}

	n.peerStateMu.Lock()
	if n.peerAckHeight == nil {
		n.peerAckHeight = make(map[string]uint64)
	}
	n.peerAckHeight["fresh-peer"] = 0
	n.peerAckHeight["lagging-peer"] = 145
	n.peerStateMu.Unlock()

	force, reason := n.validatorSnapshotAdaptivePublishDecision(170)
	if !force || reason != "new_node_join" {
		t.Fatalf("expected new node join to take priority, got force=%t reason=%q", force, reason)
	}

	n.peerStateMu.Lock()
	delete(n.peerAckHeight, "fresh-peer")
	n.peerStateMu.Unlock()

	force, reason = n.validatorSnapshotAdaptivePublishDecision(170)
	if !force || reason != "lagging_peer" {
		t.Fatalf("expected lagging peer publish trigger, got force=%t reason=%q", force, reason)
	}
}

func TestStateDeltaSnapshotReplayReproducesExactHashes(t *testing.T) {
	registry1 := testValidatorSetMaterializationRegistry()
	registry2 := copyValidatorRegistrySnapshot(registry1)
	recA := registry2["A"]
	recA.Stake = 250
	registry2["A"] = recA

	ledger1 := NewLedger()
	ledger1.Balances["alice"] = 10
	ledger2 := ledger1.Clone()
	ledger2.Balances["alice"] = 15
	ledger2.Nonces["alice"] = 2
	ledger2.Stakes["alice->A"] = StakeLock{ValidatorID: "A", Amount: 30, LockedUntil: 9}
	ledger2.ValidatorRewardWallets["A"] = "wallet-a"

	block1, snap1 := makeSnapshotLayerFixture(1, "", ledger1, registry1)
	_, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, registry2)
	snap2.FinalizedEpoch = 2
	snap2.FinalizedHeight = 2
	snap2.FinalizedHash = "finalized-block-hash"
	snap2.FinalizedStateRoot = snap2.StateRoot
	snap2.FinalizedValidatorSetHash = snap2.ValidatorSetHash
	snap2.FinalizedValidatorSetRoot = snap2.ValidatorSetRoot
	snap2.EpochAnchorHash = "epoch-anchor"
	snap2.PreviousEpochAnchorHash = "previous-epoch-anchor"
	snap2.FinalityRoot = "finality-root"
	snap2.FinalityCertificate = &FinalizedEpochCertificate{
		Version:                   finalityCertificateVersionV1,
		Domain:                    finalityCertificateDomainV1,
		Epoch:                     snap2.FinalizedEpoch,
		Height:                    snap2.FinalizedHeight,
		BlockHash:                 snap2.FinalizedHash,
		StateRoot:                 snap2.FinalizedStateRoot,
		ValidatorSetHash:          snap2.ValidatorSetHash,
		ValidatorSetRoot:          snap2.ValidatorSetRoot,
		FinalizedValidatorSetHash: snap2.FinalizedValidatorSetHash,
		FinalizedValidatorSetRoot: snap2.FinalizedValidatorSetRoot,
		EpochAnchorHash:           snap2.EpochAnchorHash,
		PreviousEpochAnchorHash:   snap2.PreviousEpochAnchorHash,
		FinalityRoot:              snap2.FinalityRoot,
		Signers:                   []string{"A", "B", "C"},
	}
	snap2.SnapshotHash = snapshotCanonicalHash(&snap2)

	delta, err := buildStateDeltaSnapshot(&snap1, &snap2)
	if err != nil {
		t.Fatalf("build state delta: %v", err)
	}
	rebuilt, err := applyStateDeltaSnapshot(&snap1, delta)
	if err != nil {
		t.Fatalf("apply state delta: %v", err)
	}
	if rebuilt.SnapshotHash != snap2.SnapshotHash {
		t.Fatalf("snapshot hash mismatch: got=%q want=%q", rebuilt.SnapshotHash, snap2.SnapshotHash)
	}
	if rebuilt.StateRoot != snap2.StateRoot {
		t.Fatalf("state root mismatch: got=%q want=%q", rebuilt.StateRoot, snap2.StateRoot)
	}
	if snapshotValidatorRegistryHash(rebuilt) != snapshotValidatorRegistryHash(&snap2) {
		t.Fatalf("registry hash mismatch: got=%q want=%q", snapshotValidatorRegistryHash(rebuilt), snapshotValidatorRegistryHash(&snap2))
	}
	if rebuilt.EpochAnchorHash != snap2.EpochAnchorHash || rebuilt.FinalityRoot != snap2.FinalityRoot || rebuilt.FinalizedValidatorSetRoot != snap2.FinalizedValidatorSetRoot {
		t.Fatalf("finality metadata mismatch after delta replay: got anchor=%q root=%q vset=%q", rebuilt.EpochAnchorHash, rebuilt.FinalityRoot, rebuilt.FinalizedValidatorSetRoot)
	}
	if rebuilt.FinalityCertificate == nil || rebuilt.FinalityCertificate.Epoch != snap2.FinalityCertificate.Epoch {
		t.Fatalf("finality certificate missing after delta replay: %+v", rebuilt.FinalityCertificate)
	}
}

func TestProcessPendingSnapshotDeltaWorkWritesDeltaRecord(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger1 := NewLedger()
	ledger2 := ledger1.Clone()
	ledger2.Balances["bob"] = 7
	block1, snap1 := makeSnapshotLayerFixture(1, "", ledger1, testValidatorSetMaterializationRegistry())
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, testValidatorSetMaterializationRegistry())

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test"); err != nil {
		t.Fatalf("store snapshot 1: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap2, "test"); err != nil {
		t.Fatalf("store snapshot 2: %v", err)
	}

	processed, err := n.processPendingSnapshotDeltaWork(10)
	if err != nil {
		t.Fatalf("process pending snapshot delta work: %v", err)
	}
	if processed != 1 {
		t.Fatalf("unexpected processed count: got=%d want=1", processed)
	}
	delta, err := n.loadStateDeltaSnapshot(2)
	if err != nil {
		t.Fatalf("load snapshot delta: %v", err)
	}
	if delta.Height != 2 || delta.BaseHeight != 1 {
		t.Fatalf("unexpected delta record: %+v", delta)
	}
}

func TestVerifySnapshotIntegrityDepthRebuildsFromDelta(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger1 := NewLedger()
	ledger2 := ledger1.Clone()
	ledger2.Balances["carol"] = 21
	block1, snap1 := makeSnapshotLayerFixture(1, "", ledger1, testValidatorSetMaterializationRegistry())
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, testValidatorSetMaterializationRegistry())

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test"); err != nil {
		t.Fatalf("store snapshot 1: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap2, "test"); err != nil {
		t.Fatalf("store snapshot 2: %v", err)
	}
	if _, err := n.processPendingSnapshotDeltaWork(10); err != nil {
		t.Fatalf("build deltas: %v", err)
	}
	if err := n.clearTipSnapshotRecords(); err != nil {
		t.Fatalf("clear tip snapshot: %v", err)
	}
	if err := n.deleteStoredSnapshotHeight(2); err != nil {
		t.Fatalf("delete committed snapshot: %v", err)
	}

	if err := n.verifySnapshotIntegrityDepth(4); err != nil {
		t.Fatalf("verify snapshot integrity: %v", err)
	}
	rebuilt, err := n.GetSnapshot(2)
	if err != nil {
		t.Fatalf("expected rebuilt snapshot at height 2: %v", err)
	}
	if rebuilt.SnapshotHash != snap2.SnapshotHash {
		t.Fatalf("rebuilt snapshot hash mismatch: got=%q want=%q", rebuilt.SnapshotHash, snap2.SnapshotHash)
	}
}

func TestVerifySnapshotIntegrityDepthSkipsHistoricalMissingAsMaterializing(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()

	ledger1 := NewLedger()
	ledger2 := ledger1.Clone()
	ledger2.Balances["alice"] = 5
	ledger3 := ledger2.Clone()
	ledger3.Balances["alice"] = 11

	block1, snap1 := makeSnapshotLayerFixture(1, "", ledger1, registry)
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, registry)
	block3, snap3 := makeSnapshotLayerFixture(3, block2.BlockHash, ledger3, registry)

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2, block3}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test"); err != nil {
		t.Fatalf("store snapshot 1: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap2, "test"); err != nil {
		t.Fatalf("store snapshot 2: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap3, "test"); err != nil {
		t.Fatalf("store snapshot 3: %v", err)
	}
	if _, err := n.processPendingSnapshotDeltaWork(10); err != nil {
		t.Fatalf("build deltas: %v", err)
	}
	if err := n.deleteStoredSnapshotHeight(2); err != nil {
		t.Fatalf("delete snapshot 2: %v", err)
	}

	if err := n.verifySnapshotIntegrityDepth(10); err != nil {
		t.Fatalf("verify snapshot integrity: %v", err)
	}
	if _, err := n.GetSnapshot(2); err == nil {
		t.Fatalf("expected historical missing snapshot to remain materializing")
	}
	if _, err := n.GetSnapshot(3); err != nil {
		t.Fatalf("expected tip snapshot to remain available: %v", err)
	}
}

func TestScrubInvalidStoredSnapshotsKeepsProtectedRecentSnapshots(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()

	ledger1 := NewLedger()
	ledger2 := ledger1.Clone()
	ledger2.Balances["alice"] = 5
	ledger3 := ledger2.Clone()
	ledger3.Balances["alice"] = 11
	ledger4 := ledger3.Clone()
	ledger4.Balances["alice"] = 17

	block1, snap1 := makeSnapshotLayerFixture(1, "", ledger1, registry)
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, registry)
	block3, snap3 := makeSnapshotLayerFixture(3, block2.BlockHash, ledger3, registry)
	block4, snap4 := makeSnapshotLayerFixture(4, block3.BlockHash, ledger4, registry)
	snap4.StateRoot = "corrupt-root"
	snap4.SnapshotHash = snapshotCanonicalHash(&snap4)

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2, block3, block4}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test"); err != nil {
		t.Fatalf("store snapshot 1: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap2, "test"); err != nil {
		t.Fatalf("store snapshot 2: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap3, "test"); err != nil {
		t.Fatalf("store snapshot 3: %v", err)
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap4, "test"); err != nil {
		t.Fatalf("store snapshot 4: %v", err)
	}

	removed, err := n.scrubInvalidStoredSnapshots(4)
	if err != nil {
		t.Fatalf("scrub invalid snapshots: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected protected recent snapshots to remain, removed=%d", removed)
	}
	if _, err := n.GetSnapshot(4); err != nil {
		t.Fatalf("expected protected tip snapshot to remain stored: %v", err)
	}
}

func TestVerifiedStoredSnapshotAtOrBelowSkipsProtectedInvalidLatest(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()

	ledger1 := NewLedger()
	ledger2 := ledger1.Clone()
	ledger2.Balances["alice"] = 5
	ledger3 := ledger2.Clone()
	ledger3.Balances["alice"] = 11
	ledger4 := ledger3.Clone()
	ledger4.Balances["alice"] = 17

	block1, snap1 := makeSnapshotLayerFixture(1, "", ledger1, registry)
	block2, snap2 := makeSnapshotLayerFixture(2, block1.BlockHash, ledger2, registry)
	block3, snap3 := makeSnapshotLayerFixture(3, block2.BlockHash, ledger3, registry)
	block4, snap4 := makeSnapshotLayerFixture(4, block3.BlockHash, ledger4, registry)
	snap4.StateRoot = "corrupt-root"
	snap4.SnapshotHash = snapshotCanonicalHash(&snap4)

	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1, block2, block3, block4}},
	}
	for _, snap := range []*StateSnapshot{&snap1, &snap2, &snap3, &snap4} {
		if err := n.storeCommittedStateSnapshotRecord(snap, "test"); err != nil {
			t.Fatalf("store snapshot %d: %v", snap.Height, err)
		}
	}

	got, err := n.verifiedStoredSnapshotAtOrBelow(0)
	if err != nil {
		t.Fatalf("verified stored snapshot lookup: %v", err)
	}
	if got == nil || got.Height != 3 {
		t.Fatalf("expected lookup to skip protected invalid latest snapshot and return height 3, got %+v", got)
	}
	if _, err := n.GetSnapshot(4); err != nil {
		t.Fatalf("expected invalid protected latest snapshot to remain stored: %v", err)
	}
}
