package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func setConsensusSafetyRoundForCrashTest(t *testing.T, node *Node, height uint64, round uint32, lockedHash string) {
	t.Helper()
	if node.Consensus == nil {
		node.Consensus = NewConsensusState(height)
	}
	node.Consensus.mu.Lock()
	node.Consensus.Height = height
	node.Consensus.Round = round
	node.Consensus.Phase = PhaseFinalize
	node.Consensus.LockedBlockHash = lockedHash
	node.Consensus.LockedBlock = lockedHash
	node.Consensus.LockedRound = round
	node.Consensus.LastFinalized = height
	node.Consensus.mu.Unlock()
}

func consensusSafetyRoundAndLockForCrashTest(t *testing.T, node *Node) (uint32, string) {
	t.Helper()
	node.Consensus.mu.Lock()
	defer node.Consensus.mu.Unlock()
	return node.Consensus.Round, node.Consensus.LockedBlockHash
}

func TestCrashDuringWALFsyncTornLatestSafetyRecordFallsBackToPrevious(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	blocks := stateRecoveryBlocks(3)
	stateRecoverySetCommitTip(t, node, blocks)

	setConsensusSafetyRoundForCrashTest(t, node, 3, 1, "safe-lock-round-1")
	if err := node.persistConsensusSafetyState("before_torn_record"); err != nil {
		t.Fatalf("persist first safety state: %v", err)
	}
	seq1 := node.latestConsensusSafetyJournalSeq()
	if seq1 == 0 {
		t.Fatal("expected first safety journal sequence")
	}

	setConsensusSafetyRoundForCrashTest(t, node, 3, 2, "torn-lock-round-2")
	if err := node.persistConsensusSafetyState("torn_record_candidate"); err != nil {
		t.Fatalf("persist second safety state: %v", err)
	}
	seq2 := node.latestConsensusSafetyJournalSeq()
	if seq2 <= seq1 {
		t.Fatalf("expected newer journal sequence, got seq1=%d seq2=%d", seq1, seq2)
	}

	if err := node.DB.Meta.Update(func(txn *Txn) error {
		if err := txn.Set(consensusSafetyJournalRecordKey(seq2), []byte("{torn-journal-record")); err != nil {
			return err
		}
		if err := txn.Set([]byte(consensusSafetyJournalLatestKey), []byte("{torn-latest-pointer")); err != nil {
			return err
		}
		return txn.Set([]byte(consensusSafetyDBKey), []byte("{torn-legacy-safety"))
	}); err != nil {
		t.Fatalf("simulate torn journal record: %v", err)
	}

	node.Consensus = NewConsensusState(1)
	node.commitMu.Lock()
	node.committed = make(map[uint64]string)
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()

	node.restoreCommittedHeightFromChain()
	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore should skip torn latest record and recover previous complete record: %v", err)
	}
	round, locked := consensusSafetyRoundAndLockForCrashTest(t, node)
	if round != 1 || locked != "safe-lock-round-1" {
		t.Fatalf("expected previous complete safety state, round=%d lock=%q", round, locked)
	}
	if got := node.getFinalizedHeight(); got != 3 {
		t.Fatalf("torn safety restore must not roll finalized height back: got=%d want=3", got)
	}
}

func TestCrashDuringWALFsyncInterruptedSafetyPersistenceUsesCompleteRecordWithoutLatestPointer(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	blocks := stateRecoveryBlocks(3)
	stateRecoverySetCommitTip(t, node, blocks)

	setConsensusSafetyRoundForCrashTest(t, node, 3, 1, "old-lock")
	if err := node.persistConsensusSafetyState("before_interrupted_pointer"); err != nil {
		t.Fatalf("persist first safety state: %v", err)
	}
	seq1 := node.latestConsensusSafetyJournalSeq()
	if seq1 == 0 {
		t.Fatal("expected first safety journal sequence")
	}

	setConsensusSafetyRoundForCrashTest(t, node, 3, 3, "complete-record-without-pointer")
	snap := node.snapshotConsensusSafetyState("interrupted_after_record_before_pointer")
	payload, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal interrupted safety snapshot: %v", err)
	}
	record, err := encodeConsensusSafetyJournalRecord(seq1+1, payload)
	if err != nil {
		t.Fatalf("encode interrupted safety journal record: %v", err)
	}
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		enc, err := encryptDBValue(record)
		if err != nil {
			return err
		}
		if err := txn.Set(consensusSafetyJournalRecordKey(seq1+1), enc); err != nil {
			return err
		}
		return txn.Set([]byte(consensusSafetyDBKey), []byte("{stale-legacy-after-interrupted-record"))
	}); err != nil {
		t.Fatalf("simulate interrupted safety persistence: %v", err)
	}

	node.Consensus = NewConsensusState(1)
	node.restoreCommittedHeightFromChain()
	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore should scan complete journal records even when latest pointer is stale: %v", err)
	}
	round, locked := consensusSafetyRoundAndLockForCrashTest(t, node)
	if round != 3 || locked != "complete-record-without-pointer" {
		t.Fatalf("expected newest complete unpointed journal record, round=%d lock=%q", round, locked)
	}
}

func TestCrashDuringBlockCommitTornDBRecordFallsBackToFsyncedBlockFile(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	block := stateRecoveryBlocks(1)[0]
	node.StoreBlock(block)

	if err := node.DB.Blocks.Update(func(txn *Txn) error {
		return txn.Set([]byte("block:1"), []byte("{partial-block-db-record"))
	}); err != nil {
		t.Fatalf("simulate torn block db record: %v", err)
	}

	loaded, ok := node.LoadBlock(1)
	if !ok {
		t.Fatal("expected block file fallback after torn db record")
	}
	if loaded.BlockHash != block.BlockHash || loaded.ID != block.ID {
		t.Fatalf("unexpected fallback block: got h=%d hash=%q want h=%d hash=%q", loaded.ID, loaded.BlockHash, block.ID, block.BlockHash)
	}
}

func TestCrashDuringStartupLoadTornBlockDBRecordFallsBackToFsyncedBlockFile(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	block := stateRecoveryBlocks(1)[0]
	node.StoreBlock(block)

	if err := node.DB.Blocks.Update(func(txn *Txn) error {
		return txn.Set([]byte("block:1"), []byte("{partial-startup-block-record"))
	}); err != nil {
		t.Fatalf("simulate torn startup block db record: %v", err)
	}

	loaded := node.LoadBlocksFromDB()
	if len(loaded) != 1 {
		t.Fatalf("expected startup load to recover one block from file fallback, got %d", len(loaded))
	}
	if loaded[0].ID != block.ID || loaded[0].BlockHash != block.BlockHash {
		t.Fatalf("unexpected recovered startup block: got h=%d hash=%q want h=%d hash=%q", loaded[0].ID, loaded[0].BlockHash, block.ID, block.BlockHash)
	}
}

func storeDurabilitySnapshotForCrashTest(t *testing.T, node *Node, height uint64, latest bool) StateSnapshot {
	t.Helper()
	snapshot := StateSnapshot{
		Version:      SnapshotVersion,
		Height:       height,
		BlockHash:    fmt.Sprintf("durability-snapshot-block-%d", height),
		StateRoot:    fmt.Sprintf("durability-snapshot-state-%d", height),
		Ledger:       NewLedger(),
		GenesisHash:  GenesisHash,
		Validators:   map[string]bool{"A": true, "B": true, "C": true, "D": true},
		SnapshotHash: fmt.Sprintf("durability-snapshot-hash-%d", height),
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal durability snapshot: %v", err)
	}
	enc, err := encryptDBValue(payload)
	if err != nil {
		t.Fatalf("encrypt durability snapshot: %v", err)
	}
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	if err := node.DB.State.Update(func(txn *Txn) error {
		return txn.Set(key, enc)
	}); err != nil {
		t.Fatalf("store durability snapshot: %v", err)
	}
	if latest {
		if err := node.DB.Meta.Update(func(txn *Txn) error {
			return txn.Set([]byte("snapshot:latest"), key)
		}); err != nil {
			t.Fatalf("store durability snapshot latest pointer: %v", err)
		}
	}
	return snapshot
}

func TestInterruptedBlockDBBatchDoesNotExposePartialWrite(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	block := stateRecoveryBlocks(1)[0]
	node.StoreBlock(block)

	interrupted := errors.New("simulated fsync interruption before block db batch commit")
	if err := node.DB.Blocks.Update(func(txn *Txn) error {
		if err := txn.Set([]byte("block:1"), []byte("{partial-block-batch-write")); err != nil {
			return err
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		t.Fatalf("expected simulated interrupted batch error, got %v", err)
	}

	loaded, ok := node.LoadBlock(1)
	if !ok {
		t.Fatal("expected original block to remain visible after interrupted db batch")
	}
	if loaded.ID != block.ID || loaded.BlockHash != block.BlockHash {
		t.Fatalf("interrupted batch exposed partial block: got h=%d hash=%q want h=%d hash=%q", loaded.ID, loaded.BlockHash, block.ID, block.BlockHash)
	}
}

func TestCrashDuringBlockFileAtomicWriteIgnoresOrphanTempFile(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	block := stateRecoveryBlocks(1)[0]
	node.StoreBlock(block)

	dir := blockStoreDir(node.DataDir, node.ID)
	if err := os.WriteFile(filepath.Join(dir, "p2p_identity_orphan.tmp"), []byte("{torn-temp-block-file"), 0o600); err != nil {
		t.Fatalf("write orphan temp block file: %v", err)
	}

	loaded := node.loadBlockFilesFromDisk()
	if len(loaded) != 1 {
		t.Fatalf("expected orphan temp file to be ignored by block file scan, got %d blocks", len(loaded))
	}
	if loaded[0].ID != block.ID || loaded[0].BlockHash != block.BlockHash {
		t.Fatalf("unexpected block after orphan temp file: got h=%d hash=%q want h=%d hash=%q", loaded[0].ID, loaded[0].BlockHash, block.ID, block.BlockHash)
	}
}

func TestCrashDuringSnapshotCreationMissingBodyFallsBackToPreviousSnapshot(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	good := storeDurabilitySnapshotForCrashTest(t, node, 10, true)

	if err := node.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), []byte("snapshot:11"))
	}); err != nil {
		t.Fatalf("simulate snapshot latest pointer persisted before body: %v", err)
	}

	loaded, err := node.LoadBestSnapshot()
	if err != nil {
		t.Fatalf("expected snapshot recovery to fall back below missing latest body: %v", err)
	}
	if loaded.Height != good.Height || loaded.SnapshotHash != good.SnapshotHash {
		t.Fatalf("unexpected fallback snapshot: got h=%d hash=%q want h=%d hash=%q", loaded.Height, loaded.SnapshotHash, good.Height, good.SnapshotHash)
	}
}

func TestPartialSnapshotDBWriteSkippedDuringBestSnapshotScan(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	good := storeDurabilitySnapshotForCrashTest(t, node, 20, true)

	if err := node.DB.State.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:21"), []byte("{partial-snapshot-db-write"))
	}); err != nil {
		t.Fatalf("store partial snapshot db write: %v", err)
	}
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), []byte("snapshot:21"))
	}); err != nil {
		t.Fatalf("point latest at partial snapshot db write: %v", err)
	}

	loaded, err := node.LoadBestSnapshot()
	if err != nil {
		t.Fatalf("expected partial latest snapshot to be skipped: %v", err)
	}
	if loaded.Height != good.Height || loaded.SnapshotHash != good.SnapshotHash {
		t.Fatalf("unexpected fallback snapshot after partial write: got h=%d hash=%q want h=%d hash=%q", loaded.Height, loaded.SnapshotHash, good.Height, good.SnapshotHash)
	}
}
