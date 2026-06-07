package main

import "testing"

func TestSnapshotHasValidMetadataRejectsValidatorSetRootMismatch(t *testing.T) {
	ledger := NewLedger()
	validators := []string{"A", "B", "C", "D"}
	snap := &StateSnapshot{
		Version:            SnapshotVersion,
		Height:             64,
		BlockHash:          "block_hash_64",
		StateRoot:          "state_root_64",
		LedgerHash:         HashLedger(ledger),
		Ledger:             ledger,
		Validators:         map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHeight: 64,
		ValidatorSetHash:   ValidatorSetHash(validators),
		ValidatorSetRoot:   "deadbeef",
	}

	if snapshotHasValidMetadata(snap) {
		t.Fatalf("expected snapshot validator_set_root mismatch to be rejected")
	}
}

func TestSnapshotHasValidMetadataAcceptsMatchingValidatorSetRoot(t *testing.T) {
	ledger := NewLedger()
	validators := []string{"A", "B", "C", "D"}
	root := ValidatorSetMerkleRoot(64, validators, nil)
	snap := &StateSnapshot{
		Version:            SnapshotVersion,
		Height:             64,
		BlockHash:          "block_hash_64",
		StateRoot:          "state_root_64",
		LedgerHash:         HashLedger(ledger),
		Ledger:             ledger,
		Validators:         map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHeight: 64,
		ValidatorSetHash:   ValidatorSetHash(validators),
		ValidatorSetRoot:   root,
	}

	if !snapshotHasValidMetadata(snap) {
		t.Fatalf("expected snapshot with matching validator_set_root to be valid")
	}
}

func TestSnapshotHasValidMetadataRejectsFinalizedHashMismatch(t *testing.T) {
	_, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap.FinalizedEpoch = snap.Height
	snap.FinalizedHeight = snap.Height
	snap.FinalizedHash = "rewritten-finalized-hash"
	snap.FinalizedStateRoot = snap.StateRoot
	snap.FinalizedValidatorSetHash = snapshotValidatorSetHash(&snap)
	snap.EpochAnchorHash = "anchor-5"
	snap.FinalityRoot = "finality-root-5"
	snap.SnapshotHash = snapshotCanonicalHash(&snap)

	if reason := snapshotMetadataRejectReason(&snap); reason != "snapshot_finalized_hash_mismatch" {
		t.Fatalf("unexpected reject reason: got=%q want=%q", reason, "snapshot_finalized_hash_mismatch")
	}
}

func TestSnapshotHasValidMetadataRejectsFinalityCertificateMismatch(t *testing.T) {
	_, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap.FinalizedEpoch = snap.Height
	snap.FinalizedHeight = snap.Height
	snap.FinalizedHash = snap.BlockHash
	snap.FinalizedStateRoot = snap.StateRoot
	snap.FinalizedValidatorSetHash = snapshotValidatorSetHash(&snap)
	snap.EpochAnchorHash = "anchor-5"
	snap.FinalityRoot = "finality-root-5"
	snap.FinalityCertificate = &FinalizedEpochCertificate{
		Epoch:                     snap.FinalizedEpoch,
		Height:                    snap.FinalizedHeight,
		BlockHash:                 snap.FinalizedHash,
		StateRoot:                 snap.FinalizedStateRoot,
		FinalizedValidatorSetHash: "rewritten-validator-set",
		EpochAnchorHash:           snap.EpochAnchorHash,
		FinalityRoot:              snap.FinalityRoot,
	}
	snap.SnapshotHash = snapshotCanonicalHash(&snap)

	if reason := snapshotMetadataRejectReason(&snap); reason != "snapshot_finality_certificate_mismatch" {
		t.Fatalf("unexpected reject reason: got=%q want=%q", reason, "snapshot_finality_certificate_mismatch")
	}
}

func TestSnapshotHasValidMetadataRejectsNextValidatorSetRootMismatch(t *testing.T) {
	ledger := NewLedger()
	validators := []string{"A", "B", "C", "D"}
	root := ValidatorSetMerkleRoot(64, validators, nil)
	setHash := ValidatorSetHash(validators)
	snap := &StateSnapshot{
		Version:              SnapshotVersion,
		Height:               64,
		BlockHash:            "block_hash_64",
		StateRoot:            "state_root_64",
		LedgerHash:           HashLedger(ledger),
		Ledger:               ledger,
		Validators:           map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHeight:   64,
		ValidatorSetHash:     setHash,
		ValidatorSetRoot:     root,
		NextValidatorSetHash: setHash,
		NextValidatorSetRoot: "bad_next_root",
	}

	if snapshotHasValidMetadata(snap) {
		t.Fatalf("expected snapshot next_validator_set_root mismatch to be rejected")
	}
}

func TestVerifySnapshotStateAndValidatorRootsDetailedRejectsGenesisHashMismatch(t *testing.T) {
	prevGenesisHash := GenesisHash
	GenesisHash = "expected-genesis-hash"
	defer func() { GenesisHash = prevGenesisHash }()

	_, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap.GenesisHash = "wrong-genesis-hash"

	n := &Node{}
	ok, reason := n.verifySnapshotStateAndValidatorRootsDetailed("", &snap)
	if ok {
		t.Fatalf("expected genesis hash mismatch to be rejected")
	}
	if reason != "genesis_hash_mismatch" {
		t.Fatalf("unexpected reject reason: got=%q want=%q", reason, "genesis_hash_mismatch")
	}
}

func TestVerifySnapshotStateAndValidatorRootsDetailedRejectsRegistryHashMismatch(t *testing.T) {
	prevGenesisHash := GenesisHash
	GenesisHash = "expected-genesis-hash"
	defer func() { GenesisHash = prevGenesisHash }()

	block, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap.GenesisHash = GenesisHash
	nextBlock := Block{
		ID:                    6,
		BlockHash:             "block-6",
		ValidatorRegistryHash: "expected-registry-hash",
	}

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block, nextBlock},
		},
	}

	ok, reason := n.verifySnapshotStateAndValidatorRootsDetailed("", &snap)
	if ok {
		t.Fatalf("expected registry hash mismatch to be rejected")
	}
	if reason != "registry_hash_mismatch" {
		t.Fatalf("unexpected reject reason: got=%q want=%q", reason, "registry_hash_mismatch")
	}
}

func TestVerifySnapshotAgainstLocalBlockDetailedRejectsValidatorSetHashMismatch(t *testing.T) {
	prevGenesisHash := GenesisHash
	GenesisHash = "expected-genesis-hash"
	defer func() { GenesisHash = prevGenesisHash }()

	block, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap.GenesisHash = GenesisHash
	block.NextValidatorSetHash = "different-validator-set-hash"

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block},
		},
	}

	ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(&snap)
	if ok {
		t.Fatalf("expected validator set hash mismatch to be rejected")
	}
	if reason != "validator_set_hash_mismatch" {
		t.Fatalf("unexpected reject reason: got=%q want=%q", reason, "validator_set_hash_mismatch")
	}
}

func TestVerifySnapshotAgainstLocalBlockDetailedRejectsStateRootMismatch(t *testing.T) {
	prevGenesisHash := GenesisHash
	GenesisHash = "expected-genesis-hash"
	defer func() { GenesisHash = prevGenesisHash }()

	block, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	snap.GenesisHash = GenesisHash
	block.StateRoot = "different-state-root"

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block},
		},
	}

	ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(&snap)
	if ok {
		t.Fatalf("expected state root mismatch to be rejected")
	}
	if reason != "state_root_mismatch" {
		t.Fatalf("unexpected reject reason: got=%q want=%q", reason, "state_root_mismatch")
	}
}

func TestSnapshotVerifierRejectsSnapshotBelowLocalFinalizedHeight(t *testing.T) {
	_, snap := makeAnchoredSnapshotFixture(9, "34a93d19feedbeef")
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.commitMu.Lock()
	node.committedHeight = 10
	node.finalizedHeight = 10
	node.commitMu.Unlock()
	node.Blockchain.Blocks = []Block{{ID: 10, Height: 10, BlockHash: "h10"}}

	err := (SnapshotVerifier{Node: node}).Verify(&snap)
	if err == nil || err.Error() != "snapshot_below_finalized_height" {
		t.Fatalf("expected below-finalized snapshot rejection, got %v", err)
	}
}

func TestSnapshotVerifierIgnoresRemoteFinalityAheadOfLocalAnchor(t *testing.T) {
	_, snap := makeAnchoredSnapshotFixture(25, "34a93d19feedbeef")
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.commitMu.Lock()
	node.committedHeight = 10
	node.finalizedHeight = 30
	node.commitMu.Unlock()
	node.Blockchain.Blocks = []Block{{ID: 10, Height: 10, BlockHash: "h10"}}

	if reason := node.snapshotLocalFinalityRejectReason(&snap); reason != "" {
		t.Fatalf("remote-observed finality ahead of local chain rejected catch-up snapshot: %s", reason)
	}
}

func TestSnapshotVerifierRejectsIrreversibleHashConflict(t *testing.T) {
	_, snap := makeAnchoredSnapshotFixture(5, "34a93d19feedbeef")
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.commitMu.Lock()
	node.committedHeight = 5
	node.finalizedHeight = 5
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(Block{ID: 5, Height: 5, BlockHash: "canonical-finalized-hash"}); err != nil {
		t.Fatalf("persist finalized invariant: %v", err)
	}
	snap.BlockHash = "conflicting-snapshot-hash"
	snap.SnapshotHash = snapshotCanonicalHash(&snap)

	err := (SnapshotVerifier{Node: node}).Verify(&snap)
	if err == nil || err.Error() != "snapshot_irreversible_hash_conflict" {
		t.Fatalf("expected irreversible snapshot conflict, got %v", err)
	}
}
