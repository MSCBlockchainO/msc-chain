package main

import "testing"

func TestRuntimeStatusSnapshotDisplaysRegistryVerifiedSourceLabel(t *testing.T) {
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

	n := &Node{
		DB:         db,
		Ledger:     NewLedger(),
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "block-1"}, {ID: 2, BlockHash: "block-2", PrevHash: "block-1", ValidatorSetHash: targetHash, NextValidatorSetHash: targetHash, ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry)}}},
	}
	if err := n.storeValidatorRegistrySnapshotRecord(2, registry); err != nil {
		t.Fatalf("store validator registry snapshot: %v", err)
	}

	runtime := n.runtimeStatusSnapshot()
	if runtime.ResolvedVsetSource != "registry_snapshot_verified" {
		t.Fatalf("unexpected resolved source label: got=%q want=registry_snapshot_verified", runtime.ResolvedVsetSource)
	}
	if runtime.ValidatorAuthoritySource != "chain_parent_commitment" {
		t.Fatalf("unexpected authority source label: got=%q want=chain_parent_commitment", runtime.ValidatorAuthoritySource)
	}
}

func TestSnapshotMetaResponseDisplaysAuthoritySourceLabels(t *testing.T) {
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
	}
	snapshot := &StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 5,
		BlockHash:              "block-5",
		StateRoot:              "state-5",
		LedgerHash:             "ledger-5",
		Validators:             map[string]bool{"A": true},
		ValidatorSetHash:       "validator-hash",
		ValidatorSetSource:     "registry_verified",
		ValidatorRegistry:      registry,
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
		NextValidatorSetHash:   "next-hash",
		NextValidatorSetSource: "chain_parent_commitment",
		NextValidatorSetHeight: 6,
	}
	meta := &SnapshotMetaRecord{
		Height:                 5,
		SnapshotHash:           "snapshot-hash",
		StateRoot:              "state-5",
		ValidatorSetHash:       "validator-hash",
		ValidatorSetSource:     "registry_verified",
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
		NextValidatorSetHash:   "next-hash",
		NextValidatorSetSource: "chain_parent_commitment",
		NextValidatorSetHeight: 6,
		Source:                 "trusted_snapshot_download",
	}

	resp := snapshotMetaResponse(snapshot, meta, "trusted_snapshot_download")
	if got := resp["source"]; got != "trusted_snapshot_anchor" {
		t.Fatalf("unexpected response source: got=%v want=trusted_snapshot_anchor", got)
	}
	if got := resp["meta_source"]; got != "trusted_snapshot_anchor" {
		t.Fatalf("unexpected meta source: got=%v want=trusted_snapshot_anchor", got)
	}
	if got := resp["validator_set_source"]; got != "registry_snapshot_verified" {
		t.Fatalf("unexpected validator set source: got=%v want=registry_snapshot_verified", got)
	}
	if got := resp["next_validator_set_source"]; got != "chain_parent_commitment" {
		t.Fatalf("unexpected next validator set source: got=%v want=chain_parent_commitment", got)
	}
	if got := resp["source_raw"]; got != "trusted_snapshot_download" {
		t.Fatalf("unexpected raw source: got=%v want=trusted_snapshot_download", got)
	}
}
