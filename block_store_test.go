package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func newBlockStoreTestNode(t *testing.T) (*Node, func()) {
	t.Helper()

	baseDir := t.TempDir()
	nodeID := "TEST"
	db := OpenNodeDB(filepath.Join(baseDir, "node_"+nodeID))
	bc := Blockchain{}

	node := &Node{
		ID:                          nodeID,
		DataDir:                     baseDir,
		DB:                          db,
		Blockchain:                  &bc,
		frozenValidatorsByHeight:    make(map[uint64][]string),
		frozenValidatorHashByHeight: make(map[uint64]string),
	}

	cleanup := func() {
		if db == nil {
			return
		}
		_ = db.Close()
	}

	return node, cleanup
}

func setFrozenValidatorSetForTest(n *Node, height uint64, validators []string) {
	n.validatorSetMu.Lock()
	defer n.validatorSetMu.Unlock()
	n.frozenValidatorsByHeight[height] = canonicalValidatorIDs(validators)
	n.frozenValidatorHashByHeight[height] = ValidatorSetHash(validators)
}

func readBlockFileRecord(t *testing.T, path string) BlockFileRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read block file: %v", err)
	}
	var record BlockFileRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode block file: %v", err)
	}
	return record
}

func testBlockAtHeight(height uint64) Block {
	return Block{
		ID:                    height,
		Height:                height,
		BlockHash:             "hash-" + string(rune('0'+height)),
		PrevHash:              "prev-hash",
		StateRoot:             "state-root",
		MempoolRoot:           "tx-root",
		ValidatorSetHash:      "validator-set-hash",
		ValidatorSetRoot:      "validator-root",
		ValidatorRegistryHash: "validator-registry-hash",
		NextValidatorSetHash:  "next-validator-set-hash",
		NextValidatorSetRoot:  "next-validator-root",
		Proposer:              "A",
		Timestamp:             12345,
		Transactions: []Transaction{
			{ID: "tx-1", From: "alice", To: "bob", Amount: 5},
		},
	}
}

func TestPersistBlockFileOverwrite(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	block := testBlockAtHeight(2)
	block.BlockHash = "hash-2"
	setFrozenValidatorSetForTest(node, 2, []string{"B", "A"})

	if err := node.persistBlockFile(block); err != nil {
		t.Fatalf("first persist failed: %v", err)
	}
	if err := node.persistBlockFile(block); err != nil {
		t.Fatalf("second persist failed: %v", err)
	}

	record := readBlockFileRecord(t, blockStoreFilePath(node.DataDir, node.ID, 2))
	if record.Height != 2 {
		t.Fatalf("height = %d, want 2", record.Height)
	}
	if record.BlockHeader.Height != 2 {
		t.Fatalf("header height = %d, want 2", record.BlockHeader.Height)
	}
	if record.BlockHeader.TxRoot != block.MempoolRoot {
		t.Fatalf("tx root = %q, want %q", record.BlockHeader.TxRoot, block.MempoolRoot)
	}
	if record.BlockHeader.ValidatorSetHash != block.ValidatorSetHash {
		t.Fatalf("validator set hash = %q, want %q", record.BlockHeader.ValidatorSetHash, block.ValidatorSetHash)
	}
	if record.BlockHeader.ValidatorRegistryHash != block.ValidatorRegistryHash {
		t.Fatalf("validator registry hash = %q, want %q", record.BlockHeader.ValidatorRegistryHash, block.ValidatorRegistryHash)
	}
	if record.BlockHeader.NextValidatorSetHash != block.NextValidatorSetHash {
		t.Fatalf("next validator set hash = %q, want %q", record.BlockHeader.NextValidatorSetHash, block.NextValidatorSetHash)
	}
	if record.StateRoot != block.StateRoot {
		t.Fatalf("state root = %q, want %q", record.StateRoot, block.StateRoot)
	}
	if len(record.Transactions) != 1 || record.Transactions[0].ID != "tx-1" {
		t.Fatalf("transactions = %+v, want tx-1", record.Transactions)
	}
	if !reflect.DeepEqual(record.ValidatorSet, []string{"A", "B"}) {
		t.Fatalf("validator set = %v, want [A B]", record.ValidatorSet)
	}
	if record.Block.ID != 2 {
		t.Fatalf("block height = %d, want 2", record.Block.ID)
	}
	if record.Block.Height != 2 {
		t.Fatalf("block nested height = %d, want 2", record.Block.Height)
	}
}

func TestBackfillBlockFilesFromLoadedBlocks(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	block := testBlockAtHeight(3)
	block.BlockHash = "hash-3"
	setFrozenValidatorSetForTest(node, 3, []string{"C", "A"})

	if err := node.DB.StoreBlock(block); err != nil {
		t.Fatalf("seed block db: %v", err)
	}
	path := blockStoreFilePath(node.DataDir, node.ID, 3)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("block file unexpectedly exists before backfill: %v", err)
	}

	blocks := node.LoadBlocksFromDB()
	node.Blockchain.ReplaceChain(blocks)
	node.backfillBlockFiles(blocks)

	record := readBlockFileRecord(t, path)
	if record.Height != 3 {
		t.Fatalf("height = %d, want 3", record.Height)
	}
	if !reflect.DeepEqual(record.ValidatorSet, []string{"A", "C"}) {
		t.Fatalf("validator set = %v, want [A C]", record.ValidatorSet)
	}
}

func TestBackfillBlockFilesSkipsExistingArchive(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	original := testBlockAtHeight(4)
	original.BlockHash = "hash-original"
	if err := node.persistBlockFile(original); err != nil {
		t.Fatalf("persist original block file: %v", err)
	}

	replacement := testBlockAtHeight(4)
	replacement.BlockHash = "hash-replacement"
	node.backfillBlockFiles([]Block{replacement})

	record := readBlockFileRecord(t, blockStoreFilePath(node.DataDir, node.ID, 4))
	if record.BlockHash != "hash-original" {
		t.Fatalf("backfill overwrote existing block archive: got %q", record.BlockHash)
	}
}

func TestPruneBlockFilesAboveHeight(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	for height := uint64(1); height <= 3; height++ {
		block := testBlockAtHeight(height)
		setFrozenValidatorSetForTest(node, height, []string{"A", "B"})
		if err := node.DB.StoreBlock(block); err != nil {
			t.Fatalf("seed block %d: %v", height, err)
		}
		if err := node.persistBlockFile(block); err != nil {
			t.Fatalf("persist file %d: %v", height, err)
		}
	}

	node.pruneBlocksAboveHeight(2)

	for _, height := range []uint64{1, 2} {
		if _, err := os.Stat(blockStoreFilePath(node.DataDir, node.ID, height)); err != nil {
			t.Fatalf("expected block file %d to remain: %v", height, err)
		}
	}
	if _, err := os.Stat(blockStoreFilePath(node.DataDir, node.ID, 3)); !os.IsNotExist(err) {
		t.Fatalf("expected block file 3 to be pruned, got err=%v", err)
	}
}

func TestStoreBlockWritesDBAndFile(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	block := testBlockAtHeight(1)
	setFrozenValidatorSetForTest(node, 1, []string{"B", "A"})

	node.StoreBlock(block)

	if _, ok := node.LoadBlock(1); !ok {
		t.Fatal("expected block in db after StoreBlock")
	}
	record := readBlockFileRecord(t, blockStoreFilePath(node.DataDir, node.ID, 1))
	if record.Height != 1 {
		t.Fatalf("height = %d, want 1", record.Height)
	}
	if !reflect.DeepEqual(record.ValidatorSet, []string{"A", "B"}) {
		t.Fatalf("validator set = %v, want [A B]", record.ValidatorSet)
	}
}

func TestLoadBlockFallsBackToBlockFileWhenDBMissing(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	block := testBlockAtHeight(5)
	block.BlockHash = "hash-5"
	setFrozenValidatorSetForTest(node, 5, []string{"A", "B"})

	if err := node.persistBlockFile(block); err != nil {
		t.Fatalf("persist block file: %v", err)
	}

	loaded, ok := node.LoadBlock(5)
	if !ok {
		t.Fatal("expected block file fallback to load block")
	}
	if loaded.ID != 5 {
		t.Fatalf("loaded height = %d, want 5", loaded.ID)
	}
	if loaded.BlockHash != block.BlockHash {
		t.Fatalf("loaded block hash = %q, want %q", loaded.BlockHash, block.BlockHash)
	}
}

func TestPersistBlockFileNormalizesNestedHeightWhenOnlyIDIsSet(t *testing.T) {
	node, cleanup := newBlockStoreTestNode(t)
	defer cleanup()

	block := testBlockAtHeight(7)
	block.Height = 0
	setFrozenValidatorSetForTest(node, 7, []string{"A", "B"})

	if err := node.persistBlockFile(block); err != nil {
		t.Fatalf("persist block file: %v", err)
	}

	record := readBlockFileRecord(t, blockStoreFilePath(node.DataDir, node.ID, 7))
	if record.Block.Height != 7 {
		t.Fatalf("nested block height = %d, want 7", record.Block.Height)
	}
	if record.BlockHeader.ValidatorRegistryHash != block.ValidatorRegistryHash {
		t.Fatalf("validator registry hash = %q, want %q", record.BlockHeader.ValidatorRegistryHash, block.ValidatorRegistryHash)
	}
}
