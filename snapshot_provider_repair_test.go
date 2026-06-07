package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func storeSnapshotForHeightWithLatest(t *testing.T, db *NodeDB, snapshot StateSnapshot) {
	t.Helper()
	key := []byte(fmt.Sprintf("snapshot:%d", snapshot.Height))
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := db.State.Update(func(txn *Txn) error {
		return txn.Set(key, raw)
	}); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
	if db.Meta == nil {
		t.Fatalf("meta db not initialized")
	}
	if err := db.Meta.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), key)
	}); err != nil {
		t.Fatalf("store latest snapshot: %v", err)
	}
}

func makeAnchoredSnapshotFixture(height uint64, validatorSetHash string) (Block, StateSnapshot) {
	ledger := NewLedger()
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
	}
	block := Block{
		ID:                     height,
		BlockHash:              fmt.Sprintf("block-%d", height),
		ValidatorSetHash:       validatorSetHash,
		NextValidatorSetHash:   validatorSetHash,
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
		NextValidatorSetHeight: height + 1,
		ActivationHeight:       height + 1,
	}
	ledgerHash := HashLedger(ledger)
	block.StateRoot = ComputeExecHash(block, ledgerHash)
	snapshot := StateSnapshot{
		Version:               SnapshotVersion,
		Height:                height,
		BlockHash:             block.BlockHash,
		StateRoot:             block.StateRoot,
		Ledger:                ledger,
		LedgerHash:            ledgerHash,
		GenesisHash:           GenesisHash,
		Validators:            map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:      validatorSetHash,
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: block.ValidatorRegistryHash,
		ActivationHeight:      height + 1,
	}
	populateSnapshotDerivedFields(&snapshot)
	return block, snapshot
}

func TestScrubInvalidStoredSnapshotsRemovesInvalidAndRefreshesLatest(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block4, snap4 := makeAnchoredSnapshotFixture(4, "34a93d19feedbeef")
	block5, snap5 := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap5.StateRoot = "bad-state-root"

	storeSnapshotForHeight(t, db, snap4)
	storeSnapshotForHeightWithLatest(t, db, snap5)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block4, block5},
		},
		DB: db,
	}

	removed, err := n.scrubInvalidStoredSnapshots(5)
	if err != nil {
		t.Fatalf("scrubInvalidStoredSnapshots failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("unexpected removed count: got=%d want=1", removed)
	}
	if _, err := n.GetSnapshot(5); err == nil {
		t.Fatalf("expected invalid snapshot at height 5 to be deleted")
	}
	latest, err := n.GetLatestSnapshot()
	if err != nil {
		t.Fatalf("GetLatestSnapshot failed after scrub: %v", err)
	}
	if latest.Height != 4 {
		t.Fatalf("expected latest snapshot pointer to refresh to height 4, got=%d", latest.Height)
	}
}

func TestSnapshotForSyncRequestSkipsInvalidExactSnapshotAndFallsBackToLowerVerified(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block4, snap4 := makeAnchoredSnapshotFixture(4, "34a93d19feedbeef")
	block5, snap5 := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap5.ValidatorSetHash = "bad-validator-set-hash"
	populateSnapshotDerivedFields(&snap5)

	storeSnapshotForHeight(t, db, snap4)
	storeSnapshotForHeightWithLatest(t, db, snap5)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block4, block5},
		},
		DB: db,
	}

	got := n.snapshotForSyncRequest(5)
	if got == nil {
		t.Fatalf("expected provider to fall back to lower verified snapshot")
	}
	if got.Height != 4 {
		t.Fatalf("expected fallback snapshot height 4, got=%d", got.Height)
	}
	if _, err := n.GetSnapshot(5); err == nil {
		t.Fatalf("expected invalid exact snapshot at height 5 to be purged")
	}
}

func TestLatestSnapshotMetaForSyncRequestReturnsLatestVerifiedStoredSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block40, snap40 := makeAnchoredSnapshotFixture(40, "34a93d19feedbeef")
	block80, snap80 := makeAnchoredSnapshotFixture(80, "34a93d19feedbeef")

	storeSnapshotForHeight(t, db, snap40)
	storeSnapshotForHeightWithLatest(t, db, snap80)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block40, block80},
		},
		DB: db,
	}

	got := n.latestSnapshotMetaForSyncRequest()
	if got == nil {
		t.Fatalf("expected latest snapshot metadata to be available")
	}
	if got.Height != 80 {
		t.Fatalf("unexpected latest snapshot height: got=%d want=80", got.Height)
	}
}

func TestSnapshotForSyncRequestServesPublishedValidatorSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block5, snap5 := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block5},
		},
		DB: db,
	}
	n.markValidatorSnapshotPublishResult(&snap5, nil)

	got := n.snapshotForSyncRequest(5)
	if got == nil {
		t.Fatalf("expected provider to serve last published validator snapshot")
	}
	if got.Height != 5 {
		t.Fatalf("unexpected published snapshot height: got=%d want=5", got.Height)
	}
	got.StateRoot = "mutated"
	again := n.snapshotForSyncRequest(5)
	if again == nil || again.StateRoot != snap5.StateRoot {
		t.Fatalf("expected served snapshot to be cloned from publish cache")
	}
}

func TestSnapshotForSyncRequestWaitsForConcurrentSnapshotCreate(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block5, snap5 := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block5},
		},
		DB: db,
	}

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(snapshotServeRetryBackoff)
		key := []byte(fmt.Sprintf("snapshot:%d", snap5.Height))
		raw, err := json.Marshal(snap5)
		if err != nil {
			errCh <- err
			return
		}
		if err := db.State.Update(func(txn *Txn) error {
			return txn.Set(key, raw)
		}); err != nil {
			errCh <- err
			return
		}
		if err := db.Meta.Update(func(txn *Txn) error {
			return txn.Set([]byte("snapshot:latest"), key)
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	got := n.snapshotForSyncRequest(5)
	if err := <-errCh; err != nil {
		t.Fatalf("concurrent snapshot store failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected provider to wait for concurrent snapshot creation")
	}
	if got.Height != 5 {
		t.Fatalf("unexpected snapshot height: got=%d want=5", got.Height)
	}
}

func TestAuthoritativeCommitteeHashesForHeightPreferCommittedHashOnExistingChain(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 405, BlockHash: "h405"}},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			406: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			406: "34a93d19feedbeef",
		},
		committeeHashByHeight: map[uint64]string{
			406: "04f4a87cfeedbeef",
		},
		validatorStatus: make(map[string]*ValidatorStatus),
	}

	got, source, runtimeHash := n.authoritativeCommitteeHashesForHeight(406)
	if got != "34a93d19feedbeef" {
		t.Fatalf("unexpected authoritative committee hash: got=%q want=%q", got, "34a93d19feedbeef")
	}
	if source != "frozen" {
		t.Fatalf("unexpected authoritative committee source: got=%q want=frozen", source)
	}
	if runtimeHash != "04f4a87cfeedbeef" {
		t.Fatalf("unexpected raw runtime committee hash: got=%q want=%q", runtimeHash, "04f4a87cfeedbeef")
	}
}
