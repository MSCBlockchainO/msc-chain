package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"strings"
	"testing"
)

func withValidatorSetCommitmentV2AtHeight(t *testing.T, height uint64) {
	t.Helper()
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = height
	t.Cleanup(func() { ValidatorSetCommitmentV2Height = prev })
}

func testValidatorSetMaterializationRegistry() map[string]ValidatorRecord {
	return map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"F": {ID: "F", Stake: 100},
	}
}

func installValidatorSetMaterializationRegistry(t *testing.T, n *Node, height uint64, registry map[string]ValidatorRecord) {
	t.Helper()
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	GlobalValidatorRegistry.Load(registry)
	t.Cleanup(func() { GlobalValidatorRegistry.Load(oldRegistry) })
	if n != nil && n.DB != nil {
		if err := n.storeValidatorRegistrySnapshot(height); err != nil {
			t.Fatalf("store validator registry snapshot: %v", err)
		}
	}
}

func TestConsensusValidatorsForHeightResolvesParentCommittedV2SnapshotHash(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	set := canonicalValidatorIDs([]string{"D", "B", "A", "C"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(2, set, registry)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                     1,
				BlockHash:              "block-1",
				Signatures:             append([]string{}, set...),
				ValidatorSetHash:       ValidatorSetHash(set),
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
			}},
		},
	}
	installValidatorSetMaterializationRegistry(t, n, 1, registry)

	got := n.consensusValidatorsForHeight(2)
	if !sameStringSlice(got, set) {
		t.Fatalf("unexpected validators at height 2: got=%v want=%v", got, set)
	}
	resolved, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(2)
	if !ok || !sameStringSlice(resolved, set) {
		t.Fatalf("expected committed validator-set resolver to recover height 2 set, ok=%t got=%v want=%v", ok, resolved, set)
	}
	if !strings.EqualFold(strings.TrimSpace(resolvedHash), targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected resolver source: got=%q want=chain_parent_commitment", source)
	}
}

func TestFreezeValidatorSetRepairsStaleCacheFromParentCommitment(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	staleSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	committedSet := canonicalValidatorIDs([]string{"B", "C", "D"})
	committedHash := ValidatorSetHash(committedSet)
	staleHash := ValidatorSetHash(staleSet)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                     1,
				BlockHash:              "block-1",
				Signatures:             append([]string{}, committedSet...),
				ValidatorSetHash:       committedHash,
				NextValidatorSetHash:   committedHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
			}},
		},
		frozenValidatorsByHeight:    map[uint64][]string{2: append([]string{}, staleSet...)},
		frozenValidatorHashByHeight: map[uint64]string{2: staleHash},
		committeeByHeight:           map[uint64][]string{2: append([]string{}, staleSet...)},
		committeeHashByHeight:       map[uint64]string{2: staleHash},
	}

	got := n.freezeValidatorSetForHeight(2, committedSet)
	if !sameStringSlice(got, committedSet) {
		t.Fatalf("freeze returned stale set: got=%v want=%v", got, committedSet)
	}
	if frozen := n.frozenValidatorsForHeight(2); !sameStringSlice(frozen, committedSet) {
		t.Fatalf("frozen cache not repaired: got=%v want=%v", frozen, committedSet)
	}
	if hash, ok := n.frozenValidatorSetHash(2); !ok || !strings.EqualFold(hash, committedHash) {
		t.Fatalf("frozen hash not repaired: ok=%t got=%q want=%q", ok, hash, committedHash)
	}
	if committee := n.committeeForHeight(2, staleSet); !sameStringSlice(committee, committedSet) {
		t.Fatalf("committee cache not repaired: got=%v want=%v", committee, committedSet)
	}
	if hash, ok := n.committeeHashForHeight(2); !ok || !strings.EqualFold(hash, committedHash) {
		t.Fatalf("committee hash not repaired: ok=%t got=%q want=%q", ok, hash, committedHash)
	}
}

func TestConsensusValidatorsForHeightResolvesParentPlannedNextSetFromBlock(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 5
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := newValidatorUpdateTestNode()
	n.DB = db
	signerKeys := installValidatorUpdateRegistry(t)
	if err := n.storeValidatorRegistrySnapshot(1); err != nil {
		t.Fatalf("store validator registry snapshot: %v", err)
	}
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	n.Mempool.Transactions = []Transaction{tx}

	block := n.BuildDeterministicBlock(n.Blockchain)
	n.Blockchain.ReplaceChain([]Block{block})

	got := n.consensusValidatorsForHeight(2)
	want := canonicalValidatorIDs([]string{"A", "B", "C", "D", "F"})
	if !sameStringSlice(got, want) {
		t.Fatalf("expected parent block plan to resolve height 2 validators: got=%v want=%v", got, want)
	}
}

func TestApplyStartupConsensusRecoverySeedsCommittedTipPlusOneValidatorSet(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(2, set, registry)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                     1,
				BlockHash:              "block-1",
				Signatures:             append([]string{}, set...),
				ValidatorSetHash:       ValidatorSetHash(set),
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
			}},
		},
	}
	installValidatorSetMaterializationRegistry(t, n, 1, registry)

	n.applyStartupConsensusRecovery()

	got := canonicalValidatorIDs(n.frozenValidatorsForHeight(2))
	if !sameStringSlice(got, set) {
		t.Fatalf("expected startup recovery to seed frozen validators at 2: got=%v want=%v", got, set)
	}
	hash, ok := n.frozenValidatorSetHash(2)
	if !ok || !strings.EqualFold(strings.TrimSpace(hash), targetHash) {
		t.Fatalf("expected startup recovery to seed committed hash at 2: ok=%t got=%q want=%q", ok, hash, targetHash)
	}
}

func TestApplySnapshotValidatorsPreservesCommittedSnapshotHash(t *testing.T) {
	snapshot := StateSnapshot{
		Height:           8,
		ValidatorSetHash: "snapshot-committed-hash",
		Validators:       map[string]bool{"A": true, "B": true, "C": true, "D": true},
	}
	n := &Node{}

	n.applySnapshotValidators(snapshot)

	hash, ok := n.frozenValidatorSetHash(8)
	if !ok || hash != "snapshot-committed-hash" {
		t.Fatalf("expected snapshot validator hash to be preserved, ok=%t got=%q", ok, hash)
	}
}

func TestResolveValidatorSetForHashAtHeightAcceptsSnapshotDerivedHash(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(2, set, registry)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 1, BlockHash: "block-1"}},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			2: append([]string{}, set...),
		},
	}
	installValidatorSetMaterializationRegistry(t, n, 1, registry)

	got, ok := n.resolveValidatorSetForHashAtHeight(2, targetHash)
	if !ok || !sameStringSlice(got, set) {
		t.Fatalf("expected snapshot-derived hash resolution, ok=%t got=%v want=%v", ok, got, set)
	}
}

func TestConsensusValidatorsForHeightAcceptsRegistryVerifiedParentCommitment(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	current := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	next := canonicalValidatorIDs([]string{"A", "B", "C", "D", "F"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(2, next, registry)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                     1,
				BlockHash:              "block-1",
				Signatures:             append([]string{}, current...),
				ValidatorSetHash:       ValidatorSetHash(current),
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
			}},
		},
		GenesisValidators: current,
	}
	installValidatorSetMaterializationRegistry(t, n, 1, registry)

	got := n.consensusValidatorsForHeight(2)
	if !sameStringSlice(got, next) {
		t.Fatalf("expected registry-verified validator set at height 2, got=%v want=%v", got, next)
	}
	resolved, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(2)
	if !ok || !sameStringSlice(resolved, next) {
		t.Fatalf("expected committed resolver to accept registry-verified set, ok=%t got=%v want=%v", ok, resolved, next)
	}
	if !strings.EqualFold(strings.TrimSpace(resolvedHash), targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "registry_verified" {
		t.Fatalf("unexpected resolver source: got=%q want=registry_verified", source)
	}
}

func TestResolveCommittedValidatorSetForHeightReconstructsRegistrySubsetByHash(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	active := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(3, active, registry)

	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2 := Block{
		ID:                    2,
		BlockHash:             "block-2",
		PrevHash:              "block-1",
		Signatures:            []string{"A", "B", "C"},
		ValidatorSetHash:      targetHash,
		NextValidatorSetHash:  targetHash,
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
	}

	n := &Node{
		DB:         db,
		Ledger:     NewLedger(),
		Blockchain: &Blockchain{Blocks: []Block{block1, block2}},
	}
	if err := n.storeValidatorRegistrySnapshotRecord(2, registry); err != nil {
		t.Fatalf("store validator registry snapshot: %v", err)
	}

	got, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(3)
	if !ok {
		t.Fatalf("expected committed validator set reconstruction from registry snapshot")
	}
	if !sameStringSlice(got, active) {
		t.Fatalf("unexpected reconstructed validator set: got=%v want=%v", got, active)
	}
	if !strings.EqualFold(strings.TrimSpace(resolvedHash), targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "registry_verified" {
		t.Fatalf("unexpected resolver source: got=%q want=registry_verified", source)
	}
}

func TestConsensusValidatorsForHeightReconstructsStoredSnapshotValidatorSet(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	active := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(2, active, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                     1,
				BlockHash:              "block-1",
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
				ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
			}},
		},
	}

	snapshot := StateSnapshot{
		Height:                 1,
		BlockHash:              "block-1",
		ValidatorSetHash:       targetHash,
		NextValidatorSetHash:   targetHash,
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
		ValidatorRegistry:      copyValidatorRegistrySnapshot(registry),
	}
	populateSnapshotDerivedFields(&snapshot)
	storeSnapshotForHeight(t, db, snapshot)

	got := n.consensusValidatorsForHeight(2)
	if !sameStringSlice(got, active) {
		t.Fatalf("expected stored snapshot reconstruction, got=%v want=%v", got, active)
	}
	resolved, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(2)
	if !ok || !sameStringSlice(resolved, active) {
		t.Fatalf("expected resolver to use stored snapshot reconstruction, ok=%t got=%v want=%v", ok, resolved, active)
	}
	if !strings.EqualFold(strings.TrimSpace(resolvedHash), targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "snapshot_committed" {
		t.Fatalf("unexpected resolver source: got=%q want=snapshot_committed", source)
	}
}

func TestRestartedHeightOneChainBecomesConsensusReadyAtHeightTwo(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(2, set, registry)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                     1,
				BlockHash:              "block-1",
				Signatures:             append([]string{}, set...),
				ValidatorSetHash:       ValidatorSetHash(set),
				NextValidatorSetHash:   targetHash,
				NextValidatorSetHeight: 2,
				ActivationHeight:       2,
			}},
		},
	}
	installValidatorSetMaterializationRegistry(t, n, 1, registry)

	n.applyStartupConsensusRecovery()

	committee, source, ok := n.deterministicCommitteeValidatorsForHeight(2)
	if !ok || !sameStringSlice(committee, set) {
		t.Fatalf("expected deterministic committee resolution at 2, ok=%t source=%q got=%v want=%v", ok, source, committee, set)
	}
	ready, reason := n.syncReadyForConsensus(2)
	if !ready || reason != "ready" {
		t.Fatalf("expected syncReadyForConsensus at height 2, ready=%t reason=%q", ready, reason)
	}
}
