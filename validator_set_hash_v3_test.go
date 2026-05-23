package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"testing"
)

func testSetGenesisPubKeys(t *testing.T, keys map[string]ed25519.PublicKey) func() {
	t.Helper()
	validatorPubKeysMu.RLock()
	old := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pk := range GenesisValidatorPubKeys {
		old[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.RUnlock()

	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(keys))
	for id, pk := range keys {
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.Unlock()

	return func() {
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(old))
		for id, pk := range old {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
		}
		validatorPubKeysMu.Unlock()
	}
}

func storeSnapshotForHeight(t *testing.T, db *NodeDB, snapshot StateSnapshot) {
	t.Helper()
	key := []byte(fmt.Sprintf("snapshot:%d", snapshot.Height))
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	store := db.SnapshotStore()
	if store == nil {
		t.Fatalf("snapshot store not initialized")
	}
	if err := store.Update(func(txn *Txn) error {
		return txn.Set(key, raw)
	}); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
}

func openNodeDBForTest(t *testing.T) (*NodeDB, func()) {
	t.Helper()
	db := OpenNodeDB(t.TempDir())
	cleanup := func() {
		if db != nil {
			_ = db.Close()
		}
	}
	return db, cleanup
}

func TestValidatorSetHashFromSnapshotForHeightRespectsRollout(t *testing.T) {
	oldV3 := ValidatorSetHashV3Height
	ValidatorSetHashV3Height = 100
	defer func() { ValidatorSetHashV3Height = oldV3 }()

	restoreKeys := testSetGenesisPubKeys(t, map[string]ed25519.PublicKey{
		"A": bytesRepeat(0x11, ed25519.PublicKeySize),
		"B": bytesRepeat(0x22, ed25519.PublicKeySize),
	})
	defer restoreKeys()

	stakeSnapshot := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 200},
		"B": {ID: "B", Stake: 100},
	}
	validators := []string{"B", "A"}

	legacy := validatorSetHashFromSnapshotLegacy(validators, stakeSnapshot)
	gotPre := validatorSetHashFromSnapshotForHeight(99, validators, stakeSnapshot)
	if gotPre != legacy {
		t.Fatalf("pre-v3 hash must remain legacy: got=%q want=%q", gotPre, legacy)
	}

	gotV3 := validatorSetHashFromSnapshotForHeight(100, validators, stakeSnapshot)
	if gotV3 == "" {
		t.Fatalf("v3 hash must not be empty")
	}
	if gotV3 == legacy {
		t.Fatalf("v3 hash must differ from legacy hash when gate is active")
	}
}

func TestValidatorSetHashFromSnapshotForHeightCanonicalDeterministic(t *testing.T) {
	oldV3 := ValidatorSetHashV3Height
	ValidatorSetHashV3Height = 1
	defer func() { ValidatorSetHashV3Height = oldV3 }()

	restoreKeys := testSetGenesisPubKeys(t, map[string]ed25519.PublicKey{
		"A": bytesRepeat(0x11, ed25519.PublicKeySize),
		"B": bytesRepeat(0x22, ed25519.PublicKeySize),
		"C": bytesRepeat(0x33, ed25519.PublicKeySize),
	})
	defer restoreKeys()

	stakeSnapshot := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 50},
	}

	hashA := validatorSetHashFromSnapshotForHeight(10, []string{"B", "C", "A"}, stakeSnapshot)
	hashB := validatorSetHashFromSnapshotForHeight(10, []string{"A", "B", "C"}, stakeSnapshot)
	if hashA == "" || hashB == "" {
		t.Fatalf("canonical hash must not be empty")
	}
	if hashA != hashB {
		t.Fatalf("canonical hash must be input-order independent: %q vs %q", hashA, hashB)
	}

	stakeSnapshot["B"] = ValidatorRecord{ID: "B", Stake: 150}
	hashC := validatorSetHashFromSnapshotForHeight(10, []string{"A", "B", "C"}, stakeSnapshot)
	if hashC == hashA {
		t.Fatalf("canonical hash must change when stake changes")
	}
}

func TestValidatorSetHashFromFinalizedSnapshotPrefersSnapshotRegistry(t *testing.T) {
	oldV3 := ValidatorSetHashV3Height
	ValidatorSetHashV3Height = 1
	defer func() { ValidatorSetHashV3Height = oldV3 }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	snapshotRegistry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 900},
		"B": {ID: "B", Stake: 200},
	}
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:           SnapshotVersion,
		Height:            10,
		Validators:        map[string]bool{"A": true, "B": true},
		ValidatorRegistry: snapshotRegistry,
	})

	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 1},
		"B": {ID: "B", Stake: 1},
	})

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 10, BlockHash: "h10"}},
		},
	}
	validators := []string{"A", "B"}

	got := n.validatorSetHashFromFinalizedSnapshot(11, validators)
	want := validatorSetHashFromSnapshotForHeight(11, validators, snapshotRegistry)
	if got != want {
		t.Fatalf("expected hash from finalized snapshot registry: got=%q want=%q", got, want)
	}
}

func TestConsensusLeaderForHeightRoundUsesSnapshotRegistry(t *testing.T) {
	oldV3 := ValidatorSetHashV3Height
	ValidatorSetHashV3Height = 1
	defer func() { ValidatorSetHashV3Height = oldV3 }()

	restoreKeys := testSetGenesisPubKeys(t, map[string]ed25519.PublicKey{
		"A": bytesRepeat(0x11, ed25519.PublicKeySize),
		"B": bytesRepeat(0x22, ed25519.PublicKeySize),
		"C": bytesRepeat(0x33, ed25519.PublicKeySize),
	})
	defer restoreKeys()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()
	snapshotRegistry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 1000},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 50},
	}
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:           SnapshotVersion,
		Height:            10,
		Validators:        map[string]bool{"A": true, "B": true, "C": true},
		ValidatorRegistry: snapshotRegistry,
	})

	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 1},
		"B": {ID: "B", Stake: 2000},
		"C": {ID: "C", Stake: 2000},
	})

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 10, BlockHash: "h10"}},
		},
	}

	validators := []string{"A", "B", "C"}
	got := n.consensusLeaderForHeightRound(11, 0, validators)
	want := LeaderForHeightFromSnapshot(11, validators, snapshotRegistry)
	if got != want {
		t.Fatalf("consensus leader must be derived from finalized snapshot registry: got=%q want=%q", got, want)
	}
}
