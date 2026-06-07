package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func stateRecoveryBlocks(count int) []Block {
	blocks := make([]Block, 0, count)
	prev := GenesisHash
	for i := 1; i <= count; i++ {
		block := Block{
			ID:        uint64(i),
			Height:    uint64(i),
			PrevHash:  prev,
			BlockHash: fmt.Sprintf("state-recovery-hash-%d", i),
			Proposer:  "A",
			Type:      BlockTypeTime,
			BlockTime: LogicalTimeForEpoch(uint64(i)),
		}
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		blocks = append(blocks, block)
		prev = block.BlockHash
	}
	return blocks
}

func stateRecoverySetCommitTip(t *testing.T, node *Node, blocks []Block) {
	t.Helper()
	node.Blockchain.ReplaceChain(blocks)
	node.restoreCommittedHeightFromChain()
	tip := blocks[len(blocks)-1]
	if got := node.getFinalizedHeight(); got != tip.ID {
		t.Fatalf("expected finalized tip %d, got %d", tip.ID, got)
	}
	if got, ok := node.getCommittedHash(tip.ID); !ok || got != tip.BlockHash {
		t.Fatalf("expected committed tip hash %q, got=%q ok=%t", tip.BlockHash, got, ok)
	}
}

func TestStateCorruptionRecoveryCorruptSafetyWALDoesNotRollback(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	blocks := stateRecoveryBlocks(3)
	stateRecoverySetCommitTip(t, node, blocks)

	if err := node.persistConsensusSafetyState("clean_before_corruption"); err != nil {
		t.Fatalf("persist clean consensus safety state: %v", err)
	}
	var safetyJournalKeys [][]byte
	if err := node.DB.Meta.View(func(txn *Txn) error {
		prefix := []byte(consensusSafetyJournalRecordPrefix)
		it := txn.NewIterator(IteratorOptions{Prefix: prefix})
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := append([]byte{}, it.Item().Key()...)
			safetyJournalKeys = append(safetyJournalKeys, key)
		}
		return nil
	}); err != nil {
		t.Fatalf("enumerate consensus safety journal: %v", err)
	}
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		for _, key := range safetyJournalKeys {
			if err := txn.Set(key, []byte("{corrupt-journal-record")); err != nil {
				return err
			}
		}
		if err := txn.Set([]byte(consensusSafetyJournalLatestKey), []byte("{corrupt-journal-latest")); err != nil {
			return err
		}
		return txn.Set([]byte(consensusSafetyDBKey), []byte("{corrupt-wal"))
	}); err != nil {
		t.Fatalf("corrupt consensus safety state: %v", err)
	}

	node.Consensus = NewConsensusState(0)
	node.commitMu.Lock()
	node.committed = make(map[uint64]string)
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()

	node.restoreCommittedHeightFromChain()
	err := node.restoreConsensusSafetyState()
	if err == nil {
		t.Fatal("expected corrupt consensus safety WAL to be detected")
	}
	if got := node.getFinalizedHeight(); got != 3 {
		t.Fatalf("corrupt WAL must not roll finalized height back: got=%d want=3 err=%v", got, err)
	}
	if got, ok := node.getCommittedHash(3); !ok || got != blocks[2].BlockHash {
		t.Fatalf("corrupt WAL must preserve block-derived committed tip: got=%q ok=%t want=%q", got, ok, blocks[2].BlockHash)
	}
	if node.hasCommittedDifferentHash(3, "fork-after-wal-corruption") == false {
		t.Fatal("finalized hash invariant must still reject same-height forks after WAL corruption")
	}
}

func TestStateCorruptionRecoveryCorruptSnapshotSkippedWithoutRollback(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, block, goodLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash
	node.commitMu.Lock()
	node.finalizedHeight = block.ID
	node.lastCommitHeight = block.ID
	node.commitMu.Unlock()

	badLedger := goodLedger.Clone()
	addBalance(&badLedger, CoinSymbol, "corrupt-snapshot-wallet", 77)
	storeExecutionSnapshotForTest(t, node, block, badLedger, SnapshotVersion, snapshotLedgerStageExecution)

	scrubbed, err := node.scrubInvalidStoredSnapshots(block.ID)
	if err != nil {
		t.Fatalf("scrub corrupt protected snapshot: %v", err)
	}
	if scrubbed != 0 {
		t.Fatalf("recent protected snapshot should be kept but gated, scrubbed=%d", scrubbed)
	}
	snapshot, err := node.GetSnapshot(block.ID)
	if err != nil {
		t.Fatalf("load corrupt snapshot: %v", err)
	}
	if applied := node.applyStartupBestSnapshot(snapshot, false); applied {
		t.Fatal("corrupt same-height snapshot metadata was applied during startup")
	}
	if got := node.getFinalizedHeight(); got != block.ID {
		t.Fatalf("corrupt snapshot must not roll finalized height back: got=%d want=%d", got, block.ID)
	}
	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block.ID); ok {
		t.Fatal("corrupt snapshot became a trusted execution snapshot")
	}
}

func TestStateCorruptionRecoveryLoadBestSnapshotSkipsCorruptLatest(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, block, ledger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash
	storeExecutionSnapshotForTest(t, node, block, ledger, SnapshotVersion, snapshotLedgerStageExecution)
	if err := node.DB.State.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:2"), []byte("{corrupt-snapshot"))
	}); err != nil {
		t.Fatalf("store corrupt latest snapshot: %v", err)
	}
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), []byte("snapshot:2"))
	}); err != nil {
		t.Fatalf("point latest at corrupt snapshot: %v", err)
	}

	snapshot, err := node.LoadBestSnapshot()
	if err != nil {
		t.Fatalf("LoadBestSnapshot should fall back below corrupt latest: %v", err)
	}
	if snapshot.Height != block.ID {
		t.Fatalf("expected fallback snapshot height %d, got %d", block.ID, snapshot.Height)
	}
}

func TestStateCorruptionRecoveryCorruptTrieRebuildsTrustedExecutionSnapshotWithoutRollback(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, block, goodLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash
	node.commitMu.Lock()
	node.finalizedHeight = block.ID
	node.lastCommitHeight = block.ID
	node.commitMu.Unlock()

	badLedger := goodLedger.Clone()
	addBalance(&badLedger, CoinSymbol, TREASURY_ADDRESS, 99)
	if got := ComputeExecHashVersioned(block, HashLedger(badLedger), executionStateRootVersionForHeight(block.ID)); got == block.StateRoot {
		t.Fatal("test setup did not create a trie/state-root mismatch")
	}
	storeExecutionSnapshotForTest(t, node, block, badLedger, SnapshotVersion, snapshotLedgerStageExecution)
	node.snapshotExecutionLedgerMu.Lock()
	node.snapshotExecutionLedgerByHeight = make(map[uint64]Ledger)
	node.snapshotExecutionLedgerMu.Unlock()
	node.postCommitLedgerMu.Lock()
	node.postCommitLedgerByHeight = make(map[uint64]Ledger)
	node.postCommitLedgerMu.Unlock()
	node.ExecutionLedger = Ledger{}
	node.Ledger = NewLedger()
	addBalance(&node.Ledger, CoinSymbol, "runtime-drift", 1)

	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block.ID); ok {
		t.Fatal("corrupt trie snapshot should not be trusted before rebuild")
	}
	if node.restoreLedgersFromAuthoritativeExecution(block.ID, "corrupt_trie") {
		t.Fatal("corrupt trie snapshot should not restore authoritative execution state")
	}
	if err := node.rebuildTrustedExecutionSnapshotsUpTo(block.ID); err != nil {
		t.Fatalf("rebuild trusted execution snapshot after trie corruption: %v", err)
	}
	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block.ID)
	if !ok || snapshot == nil {
		t.Fatal("expected trusted execution snapshot after local rebuild")
	}
	if got, want := HashLedger(snapshot.Ledger), HashLedger(goodLedger); got != want {
		t.Fatalf("rebuilt trie snapshot ledger mismatch: got=%s want=%s", got, want)
	}
	if got := node.getFinalizedHeight(); got != block.ID {
		t.Fatalf("trie rebuild must not roll finalized height back: got=%d want=%d", got, block.ID)
	}
}

func TestStateCorruptionRecoveryCorruptBlockIndexGapPrunesFutureWithoutRollback(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	blocks := stateRecoveryBlocks(4)
	for _, block := range []Block{blocks[0], blocks[1], blocks[3]} {
		if err := node.DB.StoreBlock(block); err != nil {
			t.Fatalf("store block %d: %v", block.ID, err)
		}
	}
	if err := node.DB.Blocks.Update(func(txn *Txn) error {
		return txn.Set([]byte("block:3"), []byte("{corrupt-block-index"))
	}); err != nil {
		t.Fatalf("corrupt block index height 3: %v", err)
	}

	loaded := node.LoadBlocksFromDB()
	sanitized, tip, err := sanitizeContiguousLoadedBlocks(loaded)
	if err == nil || !strings.Contains(err.Error(), "height_gap_2_to_4") || tip != 2 {
		t.Fatalf("expected corrupt block index gap at 2->4, tip=%d err=%v loaded=%d", tip, err, len(loaded))
	}
	node.Blockchain.ReplaceChain(sanitized)
	node.restoreCommittedHeightFromChain()
	if err := node.persistFinalizedHashInvariant(blocks[3]); err != nil {
		t.Fatalf("persist corrupt future finalized invariant: %v", err)
	}
	node.pruneBlocksAboveHeight(tip)
	if err := node.pruneFinalizedHashInvariantsAboveHeight(tip); err != nil {
		t.Fatalf("prune finalized hash invariants above sanitized tip: %v", err)
	}

	if got := node.getFinalizedHeight(); got != 2 {
		t.Fatalf("block index recovery must finalize last contiguous height: got=%d want=2", got)
	}
	if got, ok := node.getCommittedHash(2); !ok || got != blocks[1].BlockHash {
		t.Fatalf("expected committed hash at sanitized tip: got=%q ok=%t want=%q", got, ok, blocks[1].BlockHash)
	}
	if _, ok := node.LoadBlock(4); ok {
		t.Fatal("future sparse block survived block-index prune")
	}
	if got, found, err := node.loadFinalizedHashInvariant(4); err != nil || found || got != "" {
		t.Fatalf("future finalized invariant should be pruned: got=%q found=%t err=%v", got, found, err)
	}
}
