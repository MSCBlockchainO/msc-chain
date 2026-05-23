package main

import "testing"

func TestLedgerStateMerkleRootDeterministicAcrossMapOrder(t *testing.T) {
	ledgerA := NewLedger()
	setBalance(&ledgerA, CoinSymbol, "addr1", 100)
	setBalance(&ledgerA, CoinSymbol, "addr2", 50)
	ledgerA.Stakes["wallet1|A"] = StakeLock{Amount: 25, LockedUntil: 10}
	ledgerA.Stakes["wallet2|B"] = StakeLock{Amount: 15, LockedUntil: 12}
	ledgerA.ValidatorRewardWallets["A"] = "rewardA"
	ledgerA.ValidatorRewardWallets["B"] = "rewardB"
	ledgerA.EVMState["k2"] = "v2"
	ledgerA.EVMState["k1"] = "v1"
	ledgerA.EVMCode["0xabc"] = "0x00aa"
	ledgerA.EVMStorage["0xabc"] = map[string]string{
		"0x02": "0x20",
		"0x01": "0x10",
	}

	ledgerB := NewLedger()
	setBalance(&ledgerB, CoinSymbol, "addr2", 50)
	setBalance(&ledgerB, CoinSymbol, "addr1", 100)
	ledgerB.Stakes["wallet2|B"] = StakeLock{Amount: 15, LockedUntil: 12}
	ledgerB.Stakes["wallet1|A"] = StakeLock{Amount: 25, LockedUntil: 10}
	ledgerB.ValidatorRewardWallets["B"] = "rewardB"
	ledgerB.ValidatorRewardWallets["A"] = "rewardA"
	ledgerB.EVMState["k1"] = "v1"
	ledgerB.EVMState["k2"] = "v2"
	ledgerB.EVMCode["0xabc"] = "0x00aa"
	ledgerB.EVMStorage["0xabc"] = map[string]string{
		"0x01": "0x10",
		"0x02": "0x20",
	}

	hashA := HashLedger(ledgerA)
	hashB := HashLedger(ledgerB)
	if hashA != hashB {
		t.Fatalf("ledger hash must be deterministic across map order: got=%s want=%s", hashA, hashB)
	}

	rootA := LedgerStateMerkleRoot(ledgerA)
	rootB := LedgerStateMerkleRoot(ledgerB)
	if rootA == "" || rootB == "" {
		t.Fatalf("state merkle root must not be empty: a=%q b=%q", rootA, rootB)
	}
	if rootA != rootB {
		t.Fatalf("state merkle root must be deterministic across map order: got=%s want=%s", rootA, rootB)
	}
}

func TestSnapshotManifestCarriesStateMerkleRoot(t *testing.T) {
	ledger := NewLedger()
	setBalance(&ledger, CoinSymbol, "addr1", 42)
	setBalance(&ledger, CoinSymbol, "addr2", 99)
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	}

	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                5,
		BlockHash:             "block-5",
		PrevHash:              "block-4",
		StateRoot:             "state-root-5",
		LedgerHash:            HashLedger(ledger),
		Ledger:                ledger,
		Validators:            map[string]bool{"A": true},
		ValidatorSetHash:      ValidatorSetHash([]string{"A"}),
		NextValidatorSetHash:  ValidatorSetHash([]string{"A"}),
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
	}
	populateSnapshotDerivedFields(snapshot)

	manifest, _, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot failed: %v", err)
	}
	if manifest.StateMerkleRoot == "" {
		t.Fatalf("expected non-empty state merkle root in manifest")
	}
	if manifest.StateMerkleRoot != LedgerStateMerkleRoot(ledger) {
		t.Fatalf("unexpected state merkle root in manifest: got=%s want=%s", manifest.StateMerkleRoot, LedgerStateMerkleRoot(ledger))
	}

	meta := &SnapshotMetaResponse{
		Available:       true,
		Height:          manifest.Height,
		SnapshotHash:    manifest.SnapshotHash,
		StateRoot:       manifest.StateRoot,
		StateMerkleRoot: manifest.StateMerkleRoot,
		Manifest:        manifest,
	}
	roundTrip := snapshotManifestFromMeta(meta)
	if roundTrip == nil {
		t.Fatalf("expected manifest to round-trip from meta")
	}
	if roundTrip.StateMerkleRoot != manifest.StateMerkleRoot {
		t.Fatalf("state merkle root not propagated through meta: got=%s want=%s", roundTrip.StateMerkleRoot, manifest.StateMerkleRoot)
	}
}

func TestExecutionDeterminismGuardAllowsDeterministicFallbackBlock(t *testing.T) {
	oldGuard := ExecutionDeterminismGuardEnabled
	ExecutionDeterminismGuardEnabled = true
	t.Cleanup(func() {
		ExecutionDeterminismGuardEnabled = oldGuard
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildFallbackBlock(node.currentEpoch())
	if block.StateRoot == "" {
		t.Fatalf("expected non-empty state root with determinism guard enabled")
	}
}

func TestVerifyReceiptRootBackwardCompatibleWhenMissing(t *testing.T) {
	block := Block{
		Receipts: []StateReceipt{
			{TxHash: "tx-1", PreStateHash: "pre-1", PostStateHash: "post-1"},
		},
		ReceiptRoot: "",
	}
	if err := VerifyReceiptRoot(block); err != nil {
		t.Fatalf("expected compatibility when receipt_root is missing, got error: %v", err)
	}
}

func TestVerifyReceiptRootMismatchDetected(t *testing.T) {
	block := Block{
		Receipts: []StateReceipt{
			{TxHash: "tx-1", PreStateHash: "pre-1", PostStateHash: "post-1"},
			{TxHash: "tx-2", PreStateHash: "pre-2", PostStateHash: "post-2"},
		},
		ReceiptRoot: "deadbeef",
	}
	if err := VerifyReceiptRoot(block); err == nil {
		t.Fatalf("expected receipt_root mismatch to be detected")
	}
}
