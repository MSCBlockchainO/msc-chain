package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"strings"
	"testing"
)

func TestResolveSnapshotValidatorListPrefersCommittedBlockSetMatchingAnchor(t *testing.T) {
	set := canonicalValidatorIDs([]string{"D", "B", "A", "C"})
	setHash := ValidatorSetHash(set)
	block := Block{
		ID:                     5,
		Signatures:             append([]string{}, set...),
		ValidatorSetHash:       setHash,
		NextValidatorSetHash:   setHash,
		NextValidatorSetHeight: 6,
		ActivationHeight:       6,
	}
	n := &Node{
		Blockchain:        &Blockchain{Blocks: []Block{block}},
		GenesisValidators: []string{"X", "Y", "Z"},
	}

	got, err := n.resolveSnapshotValidatorList(6, block)
	if err != nil {
		t.Fatalf("resolveSnapshotValidatorList failed: %v", err)
	}
	if !sameStringSlice(got, set) {
		t.Fatalf("unexpected validator set: got=%v want=%v", got, set)
	}
}

func TestResolveSnapshotValidatorListRejectsAnchorMismatch(t *testing.T) {
	set := canonicalValidatorIDs([]string{"D", "B", "A", "C"})
	block := Block{
		ID:                     5,
		Signatures:             append([]string{}, set...),
		ValidatorSetHash:       ValidatorSetHash(set),
		NextValidatorSetHash:   ValidatorSetHash([]string{"A", "B", "C", "D", "F"}),
		NextValidatorSetHeight: 6,
		ActivationHeight:       6,
	}
	n := &Node{
		Blockchain:        &Blockchain{Blocks: []Block{block}},
		GenesisValidators: []string{"A", "B", "C", "D"},
	}

	got, err := n.resolveSnapshotValidatorList(6, block)
	if err == nil {
		t.Fatalf("expected unresolved error, got set=%v", got)
	}
	if !strings.Contains(err.Error(), "snapshot_validator_set_unresolved") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSnapshotValidatorListNoGenesisFallbackOnExistingChain(t *testing.T) {
	block := Block{
		ID:                     5,
		NextValidatorSetHash:   "different-next-hash",
		NextValidatorSetHeight: 6,
		ActivationHeight:       6,
	}
	n := &Node{
		Blockchain:        &Blockchain{Blocks: []Block{block}},
		GenesisValidators: []string{"A", "B", "C", "D"},
	}

	got, err := n.resolveSnapshotValidatorList(6, block)
	if err == nil {
		t.Fatalf("expected unresolved error, got set=%v", got)
	}
	if !strings.Contains(err.Error(), "snapshot_validator_set_unresolved") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSnapshotValidatorListAllowsGenesisBootstrap(t *testing.T) {
	n := &Node{
		Blockchain:        &Blockchain{},
		GenesisValidators: []string{"D", "A", "C", "B"},
	}
	got, err := n.resolveSnapshotValidatorList(1, Block{ID: 0})
	if err != nil {
		t.Fatalf("resolveSnapshotValidatorList failed: %v", err)
	}
	want := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected bootstrap set: got=%v want=%v", got, want)
	}
}

func TestResolveSnapshotValidatorListUsesStrictTxPlannedNextSet(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 5
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	n.Mempool.Transactions = []Transaction{tx}

	block := n.BuildDeterministicBlock(n.Blockchain)
	got, err := n.resolveSnapshotValidatorList(2, block)
	if err != nil {
		t.Fatalf("resolveSnapshotValidatorList failed: %v", err)
	}
	want := []string{"A", "B", "C", "D", "F"}
	if !sameStringSlice(got, want) {
		t.Fatalf("strict snapshot validator resolver must use tx-planned next set: got=%v want=%v", got, want)
	}
}

func TestResolveSnapshotValidatorListUsesCommittedParentRegistryCandidate(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	parentRegistry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive},
	}
	parentSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	targetHash := validatorSetHashFromSnapshotForHeight(3, parentSet, parentRegistry)

	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2 := Block{
		ID:                    2,
		BlockHash:             "block-2",
		PrevHash:              "block-1",
		Signatures:            []string{"A", "B", "C"}, // intentionally mismatched subset
		ValidatorSetHash:      targetHash,
		NextValidatorSetHash:  targetHash,
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(parentRegistry),
	}

	n := &Node{
		DB:         db,
		Ledger:     NewLedger(),
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeValidatorRegistrySnapshotRecord(2, parentRegistry); err != nil {
		t.Fatalf("store parent registry snapshot: %v", err)
	}

	got, err := n.resolveSnapshotValidatorList(3, block2)
	if err != nil {
		t.Fatalf("resolveSnapshotValidatorList failed: %v", err)
	}
	if !sameStringSlice(got, parentSet) {
		t.Fatalf("expected parent-registry-derived validator set: got=%v want=%v", got, parentSet)
	}
}

func TestExpectedValidatorSetHashUsesSnapshotCommittedHashOnExistingChain(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
	}
	ledger := NewLedger()
	snapshot := StateSnapshot{
		Version:               SnapshotVersion,
		Height:                622,
		BlockHash:             "block-622",
		StateRoot:             "state-622",
		Ledger:                ledger,
		LedgerHash:            HashLedger(ledger),
		GenesisHash:           GenesisHash,
		Validators:            map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:      "04f4a87cfeedbeef",
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
		ActivationHeight:      623,
	}
	populateSnapshotDerivedFields(&snapshot)
	storeSnapshotForHeight(t, db, snapshot)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 622, BlockHash: "block-622"}},
		},
		DB: db,
	}

	got, source := n.expectedValidatorSetHashWithSource(623)
	if got != "04f4a87cfeedbeef" {
		t.Fatalf("unexpected committed hash: got=%q want=%q", got, "04f4a87cfeedbeef")
	}
	if source != "snapshot_parent" {
		t.Fatalf("unexpected committed hash source: got=%q want=snapshot_parent", source)
	}
}
