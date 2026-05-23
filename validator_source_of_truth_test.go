package main

import (
	"strings"
	"testing"
)

func TestStartupValidatorSetSelfCheckV2RejectsStaleRestartSnapshotSet(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetCommitmentV2Height = oldV2Height
	})
	ValidatorSetCommitmentV2Height = 1

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
	}
	targetSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	targetHash := validatorSetHashFromSnapshotForHeight(2, targetSet, registry)
	parent.ValidatorRegistryHash = ValidatorRegistrySnapshotHash(registry)
	parent.NextValidatorSetHash = targetHash
	parent.NextValidatorSetHeight = 2
	parent.ActivationHeight = 2
	node.Blockchain.Blocks[0] = parent
	storeCanonicalValidatorRegistrySnapshotRecord(t, node.DB, 1, registry)

	staleSnapshot := StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 1,
		BlockHash:              parent.BlockHash,
		StateRoot:              parent.StateRoot,
		Ledger:                 execLedger.Clone(),
		LedgerHash:             HashLedger(execLedger),
		LedgerStage:            snapshotLedgerStageExecution,
		GenesisHash:            genesisHash,
		PrevHash:               parent.PrevHash,
		Validators:             map[string]bool{"A": true, "B": true, "C": true},
		ValidatorSetHash:       targetHash,
		ValidatorSetSource:     "snapshot_committed",
		ValidatorRegistry:      copyValidatorRegistrySnapshot(registry),
		ValidatorRegistryHash:  parent.ValidatorRegistryHash,
		NextValidatorSetHash:   targetHash,
		NextValidatorSetSource: "chain_parent_commitment",
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
		Timestamp:              parent.Timestamp,
	}
	populateSnapshotDerivedFields(&staleSnapshot)
	storeSnapshotForHeight(t, node.DB, staleSnapshot)

	ok, reason := node.startupValidatorSetSelfCheck()
	if ok {
		t.Fatalf("expected stale restart snapshot to keep startup gate closed")
	}
	if !strings.HasPrefix(reason, "startup_validator_set_mismatch_h_2") {
		t.Fatalf("unexpected mismatch reason: got=%q", reason)
	}
}

func TestRuntimeStatusSnapshotReportsCommittedSnapshotSourceForRestart(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetCommitmentV2Height = oldV2Height
	})
	ValidatorSetCommitmentV2Height = 1

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
	}
	targetSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	targetHash := validatorSetHashFromSnapshotForHeight(2, targetSet, registry)
	parent.ValidatorRegistryHash = ValidatorRegistrySnapshotHash(registry)
	parent.NextValidatorSetHash = targetHash
	parent.NextValidatorSetHeight = 2
	parent.ActivationHeight = 2
	node.Blockchain.Blocks[0] = parent
	storeCanonicalValidatorRegistrySnapshotRecord(t, node.DB, 1, registry)

	snapshot := StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 1,
		BlockHash:              parent.BlockHash,
		StateRoot:              parent.StateRoot,
		Ledger:                 execLedger.Clone(),
		LedgerHash:             HashLedger(execLedger),
		LedgerStage:            snapshotLedgerStageExecution,
		GenesisHash:            genesisHash,
		PrevHash:               parent.PrevHash,
		Validators:             map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:       targetHash,
		ValidatorSetSource:     "snapshot_committed",
		ValidatorRegistry:      copyValidatorRegistrySnapshot(registry),
		ValidatorRegistryHash:  parent.ValidatorRegistryHash,
		NextValidatorSetHash:   targetHash,
		NextValidatorSetSource: "chain_parent_commitment",
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
		Timestamp:              parent.Timestamp,
	}
	populateSnapshotDerivedFields(&snapshot)
	storeSnapshotForHeight(t, node.DB, snapshot)

	runtime := node.runtimeStatusSnapshot()
	if runtime.CommittedSnapshotSource != "snapshot_committed" {
		t.Fatalf("unexpected committed snapshot source: got=%q want=snapshot_committed", runtime.CommittedSnapshotSource)
	}
	if runtime.ExpectedVsetSource != "chain_parent_commitment" {
		t.Fatalf("unexpected expected validator-set source: got=%q want=chain_parent_commitment", runtime.ExpectedVsetSource)
	}
}

func TestResolveCommittedValidatorSetForHeightUsesRegistryVerifiedCandidate(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive},
	}
	parentSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	targetHash := validatorSetHashFromSnapshotForHeight(3, parentSet, registry)

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
		t.Fatalf("expected committed validator set to resolve from verified registry snapshot")
	}
	if !sameStringSlice(got, parentSet) {
		t.Fatalf("unexpected resolved validator set: got=%v want=%v", got, parentSet)
	}
	if !strings.EqualFold(strings.TrimSpace(resolvedHash), targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "registry_verified" {
		t.Fatalf("unexpected resolver source: got=%q want=registry_verified", source)
	}
}
