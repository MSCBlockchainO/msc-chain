package main

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

func TestReceiveBlockNextValidatorSetMismatchEscalatesSnapshotSync(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	prevV3 := ValidatorSetHashV3Height
	ValidatorSetCommitmentV2Height = 1
	ValidatorSetHashV3Height = ^uint64(0)
	t.Cleanup(func() {
		ValidatorSetCommitmentV2Height = prevV2
		ValidatorSetHashV3Height = prevV3
	})

	restoreKeys := testSetGenesisPubKeys(t, map[string]ed25519.PublicKey{
		"A": bytesRepeat(0x11, ed25519.PublicKeySize),
		"B": bytesRepeat(0x22, ed25519.PublicKeySize),
		"C": bytesRepeat(0x33, ed25519.PublicKeySize),
		"D": bytesRepeat(0x44, ed25519.PublicKeySize),
		"G": bytesRepeat(0x55, ed25519.PublicKeySize),
	})
	defer restoreKeys()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D", "G"})
	if node.peerAckHeight == nil {
		node.peerAckHeight = make(map[string]uint64)
	}
	node.peerAckHeight["peer-1"] = 767

	ledger := NewLedger()
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"G": {ID: "G", Stake: 100},
	}
	registryHash := ValidatorRegistrySnapshotHash(registry)
	currentValidators := []string{"A", "B", "C", "D", "G"}
	currentHash := validatorSetHashFromSnapshotForHeight(682, currentValidators, registry)
	currentRoot := ValidatorSetMerkleRoot(682, currentValidators, registry)
	nextHash := "unreconstructable-next-hash"
	nextRoot := "unreconstructable-next-root"

	parent := Block{
		ID:                     681,
		BlockHash:              "parent-681",
		StateRoot:              "state-681",
		ValidatorSetHash:       currentHash,
		ValidatorSetRoot:       currentRoot,
		ValidatorRegistryHash:  registryHash,
		NextValidatorSetHash:   currentHash,
		NextValidatorSetRoot:   currentRoot,
		NextValidatorSetHeight: 682,
		ActivationHeight:       682,
	}
	bc := NewBlockchain()
	bc.Blocks = append(bc.Blocks, parent)
	node.Blockchain = &bc

	storeSnapshotForHeight(t, node.DB, StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   681,
		BlockHash:                parent.BlockHash,
		StateRoot:                parent.StateRoot,
		Ledger:                   ledger,
		LedgerHash:               HashLedger(ledger),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true, "G": true},
		ValidatorRegistry:        registry,
		ValidatorRegistryHash:    registryHash,
		PendingValidatorRemovals: map[string]uint64{"G": 683},
		ValidatorSetHash:         currentHash,
		ValidatorSetRoot:         currentRoot,
		NextValidatorSetHash:     currentHash,
		NextValidatorSetRoot:     currentRoot,
		NextValidatorSetHeight:   682,
		ActivationHeight:         682,
	})
	storeCanonicalValidatorRegistrySnapshotRecord(t, node.DB, 681, registry)

	block := Block{
		ID:                     682,
		PrevHash:               parent.BlockHash,
		StateRoot:              "state-682",
		ValidatorSetHash:       currentHash,
		ValidatorSetRoot:       currentRoot,
		ValidatorRegistryHash:  registryHash,
		NextValidatorSetHash:   nextHash,
		NextValidatorSetRoot:   nextRoot,
		NextValidatorSetHeight: 683,
		ActivationHeight:       683,
		Round:                  0,
	}
	validators := node.freezeValidatorSetForHeight(block.ID, node.GetConsensusValidators(int(block.ID)))
	block.Proposer = node.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	block.BlockHash = HashBlock(block)

	expectedNext, _, source := node.expectedNextValidatorSetCommitmentForBlock(block)
	if expectedNext == "" {
		t.Fatalf("expected a local next-set commitment source, got none")
	}
	if expectedNext == nextHash {
		t.Fatalf("test requires local stale expectation mismatch, got source=%s hash=%s", source, expectedNext)
	}

	err := node.ReceiveBlock(block, node.Blockchain)
	if err == nil {
		t.Fatal("expected next-validator-set mismatch rejection")
	}
	var applyErr *BlockApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("expected BlockApplyError, got %T", err)
	}
	if applyErr.Reason != "next_validator_set_hash_mismatch" {
		t.Fatalf("expected next_validator_set_hash_mismatch, got %q", applyErr.Reason)
	}

	if node.lastSyncAttempt.IsZero() {
		t.Fatal("expected mismatch to trigger snapshot resync attempt")
	}
	stage, _ := node.syncDiagnosticContext()
	if stage != "snapshot_delta" {
		t.Fatalf("expected snapshot_delta stage after escalation, got %q", stage)
	}
}

func TestSnapshotSyncMinHeightOverrideForValidatorSetMismatchUsesNextHeight(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		local    uint64
		target   uint64
		wantMinH uint64
	}{
		{
			name:     "next_hash_mismatch",
			reason:   "next-validator-set-hash-mismatch-autoheal",
			local:    758,
			target:   928,
			wantMinH: 759,
		},
		{
			name:     "next_root_mismatch",
			reason:   "next-validator-set-root-mismatch-autoheal",
			local:    758,
			target:   928,
			wantMinH: 759,
		},
		{
			name:     "state_root_mismatch",
			reason:   "queue_stall_queue_apply_failed_state_root_mismatch",
			local:    1396,
			target:   3195,
			wantMinH: 1397,
		},
		{
			name:     "startup_execution_snapshot",
			reason:   "startup_execution_snapshot_missing",
			local:    758,
			target:   928,
			wantMinH: 1,
		},
		{
			name:     "normal_reason",
			reason:   "heartbeat",
			local:    758,
			target:   928,
			wantMinH: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotSyncMinHeightOverrideForReason(tc.reason, tc.local, tc.target); got != tc.wantMinH {
				t.Fatalf("snapshotSyncMinHeightOverrideForReason(%q, %d, %d) = %d, want %d",
					tc.reason, tc.local, tc.target, got, tc.wantMinH)
			}
		})
	}
}
