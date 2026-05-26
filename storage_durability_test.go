package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func latestPebbleWALForTest(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read pebble db dir: %v", err)
	}
	best := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		if entry.Name() > best {
			best = entry.Name()
		}
	}
	if best == "" {
		t.Fatalf("no pebble WAL found in %s", dir)
	}
	return filepath.Join(dir, best)
}

func readPebbleValueForTest(t *testing.T, db *DB, key string) (string, bool) {
	t.Helper()
	out := ""
	found := false
	err := db.View(func(txn *Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			out = string(val)
			return nil
		})
	})
	if err != nil {
		t.Fatalf("read pebble value %s: %v", key, err)
	}
	return out, found
}

func storageDurabilityFinalityBlock(t *testing.T, node *Node, height uint64, prevHash string) Block {
	t.Helper()
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	setRoot := HashStrings(append([]string{"storage-durability-validator-root"}, validators...))
	block := Block{
		ID:                     height,
		Height:                 height,
		Type:                   BlockTypeTime,
		PrevHash:               strings.TrimSpace(prevHash),
		StateRoot:              HashStrings([]string{"storage-durability-state", fmt.Sprint(height)}),
		ValidatorSetHash:       ValidatorSetHash(validators),
		ValidatorSetRoot:       setRoot,
		NextValidatorSetHash:   ValidatorSetHash(validators),
		NextValidatorSetRoot:   setRoot,
		ValidatorRegistryHash:  HashStrings([]string{"storage-durability-registry", fmt.Sprint(height)}),
		ConsensusMode:          "NORMAL",
		QuorumPolicyVersion:    quorumPolicyVersionV1,
		ActiveReadyCount:       len(validators),
		RequiredQuorum:         strictExecSupermajority(len(validators)),
		StrictQuorum:           strictExecSupermajority(len(validators)),
		Signatures:             validators[:strictExecSupermajority(len(validators))],
		NextValidatorSetHeight: height + 1,
		ActivationHeight:       height + 1,
	}
	block.BlockTime = LogicalTimeForEpochTick(height, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	node.attachFinalityCommitments(&block)
	block.BlockHash = HashBlock(block)
	node.attachFinalityCertificate(&block)
	return block
}

func storageDurabilityFinalityPair(t *testing.T) (*Node, Block, Block) {
	t.Helper()
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	block1 := storageDurabilityFinalityBlock(t, node, 1, GenesisHash)
	if err := node.persistFinalityCheckpoint(block1); err != nil {
		t.Fatalf("persist block1 finality checkpoint: %v", err)
	}
	node.Blockchain.ReplaceChain([]Block{block1})
	block2 := storageDurabilityFinalityBlock(t, node, 2, block1.BlockHash)
	return node, block1, block2
}

func TestTornWALWriteRecovery(t *testing.T) {
	dbDir := filepath.Join(t.TempDir(), "state.db")
	db, err := openPebbleDB(dbDir)
	if err != nil {
		t.Fatalf("open pebble db: %v", err)
	}
	if err := db.Update(func(txn *Txn) error {
		return txn.Set([]byte("tx1"), []byte("committed-1"))
	}); err != nil {
		t.Fatalf("write tx1: %v", err)
	}
	if err := db.Update(func(txn *Txn) error {
		return txn.Set([]byte("tx2"), []byte("committed-2"))
	}); err != nil {
		t.Fatalf("write tx2: %v", err)
	}
	walPath := latestPebbleWALForTest(t, dbDir)
	if err := db.Close(); err != nil {
		t.Fatalf("close pebble before WAL tear: %v", err)
	}

	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open WAL for torn tail: %v", err)
	}
	if _, err := f.Write([]byte("incomplete-wal-record-without-valid-trailer")); err != nil {
		_ = f.Close()
		t.Fatalf("append torn WAL tail: %v", err)
	}
	_ = f.Close()

	reopened, err := openPebbleDB(dbDir)
	if err != nil {
		t.Fatalf("reopen should truncate/ignore torn WAL tail without panic: %v", err)
	}
	defer reopened.Close()
	if got, ok := readPebbleValueForTest(t, reopened, "tx1"); !ok || got != "committed-1" {
		t.Fatalf("tx1 was not recovered after torn WAL tail: got=%q ok=%t", got, ok)
	}
	if got, ok := readPebbleValueForTest(t, reopened, "tx2"); !ok || got != "committed-2" {
		t.Fatalf("tx2 was not recovered after torn WAL tail: got=%q ok=%t", got, ok)
	}
	if _, ok := readPebbleValueForTest(t, reopened, "tx3"); ok {
		t.Fatal("incomplete WAL tail materialized as tx3")
	}
}

func TestInterruptedFsyncRecovery(t *testing.T) {
	node, block1, block2 := storageDurabilityFinalityPair(t)
	interrupted := errors.New("simulated fsync interruption before finality batch commit")

	if err := node.DB.Meta.Update(func(txn *Txn) error {
		encAnchor, err := encryptDBValue([]byte(block2.EpochAnchorHash))
		if err != nil {
			return err
		}
		if err := txn.Set(finalityAnchorDBKey(block2.ID), encAnchor); err != nil {
			return err
		}
		checkpoint := finalityCheckpointRecordFromBlock(block2)
		rawCheckpoint, err := marshalForStorageDurability(checkpoint)
		if err != nil {
			return err
		}
		encCheckpoint, err := encryptDBValue(rawCheckpoint)
		if err != nil {
			return err
		}
		if err := txn.Set(finalityCheckpointDBKey(block2.ID), encCheckpoint); err != nil {
			return err
		}
		return interrupted
	}); !errors.Is(err, interrupted) {
		t.Fatalf("expected interrupted finality batch error, got %v", err)
	}

	if err := node.verifyPersistedFinalityCheckpoint(block1); err != nil {
		t.Fatalf("previous checkpoint must survive interrupted fsync: %v", err)
	}
	if _, ok, err := node.loadPersistedFinalityCheckpoint(block2.ID); err != nil || ok {
		t.Fatalf("interrupted checkpoint became visible ok=%t err=%v", ok, err)
	}
	if anchor, ok, err := node.loadPersistedFinalityAnchorHash(block2.ID); err != nil || ok || anchor != "" {
		t.Fatalf("interrupted anchor became visible anchor=%q ok=%t err=%v", anchor, ok, err)
	}
}

func TestPartialDatabaseWriteRecovery(t *testing.T) {
	node, block1, block2 := storageDurabilityFinalityPair(t)
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		encAnchor, err := encryptDBValue([]byte(block2.EpochAnchorHash))
		if err != nil {
			return err
		}
		return txn.Set(finalityAnchorDBKey(block2.ID), encAnchor)
	}); err != nil {
		t.Fatalf("write partial finality DB record: %v", err)
	}

	if err := node.verifyPersistedFinalityCheckpoint(block2); err != nil {
		t.Fatalf("future partial finality DB record should be ignored until the block commits: %v", err)
	}
	if err := node.verifyPersistedFinalityCheckpoint(block1); err != nil {
		t.Fatalf("previous valid checkpoint should remain recoverable: %v", err)
	}
}

func TestCrashDuringSnapshotCreation(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	good := storeDurabilitySnapshotForCrashTest(t, node, 30, true)
	if err := node.DB.State.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:31"), []byte("{half-written-snapshot"))
	}); err != nil {
		t.Fatalf("write half snapshot: %v", err)
	}
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), []byte("snapshot:31"))
	}); err != nil {
		t.Fatalf("point latest at half snapshot: %v", err)
	}

	loaded, err := node.LoadBestSnapshot()
	if err != nil {
		t.Fatalf("half snapshot should be ignored: %v", err)
	}
	if loaded.Height != good.Height || loaded.SnapshotHash != good.SnapshotHash {
		t.Fatalf("wrong snapshot after crash recovery: got h=%d hash=%q want h=%d hash=%q", loaded.Height, loaded.SnapshotHash, good.Height, good.SnapshotHash)
	}
}

func TestCheckpointAtomicity(t *testing.T) {
	node, _, block2 := storageDurabilityFinalityPair(t)
	if err := node.DB.Meta.Update(func(txn *Txn) error {
		encAnchor, err := encryptDBValue([]byte(block2.EpochAnchorHash))
		if err != nil {
			return err
		}
		if err := txn.Set(finalityAnchorDBKey(block2.ID), encAnchor); err != nil {
			return err
		}
		encCert, err := encryptDBValue([]byte("{partial-finality-cert"))
		if err != nil {
			return err
		}
		return txn.Set(finalityCertificateDBKey(block2.ID), encCert)
	}); err != nil {
		t.Fatalf("write intentionally partial finality checkpoint: %v", err)
	}

	if err := node.verifyPersistedFinalityCheckpoint(block2); err != nil {
		t.Fatalf("future partial finality checkpoint should be ignored until the block commits: %v", err)
	}
}

func TestWALReplayIdempotent(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	blocks := stateRecoveryBlocks(3)
	for i := range blocks {
		blocks[i].StateRoot = fmt.Sprintf("idempotent-state-root-%d", blocks[i].ID)
	}
	stateRecoverySetCommitTip(t, node, blocks)
	setConsensusSafetyRoundForCrashTest(t, node, 3, 2, "idempotent-lock")
	if err := node.persistConsensusSafetyState("idempotency_baseline"); err != nil {
		t.Fatalf("persist idempotency WAL: %v", err)
	}

	var prevHeight uint64
	var prevHash string
	var prevStateRoot string
	for replay := 0; replay < 2; replay++ {
		node.Consensus = NewConsensusState(1)
		node.commitMu.Lock()
		node.committed = make(map[uint64]string)
		node.committedHeight = 0
		node.finalizedHeight = 0
		node.lastCommitHeight = 0
		node.commitMu.Unlock()
		node.restoreCommittedHeightFromChain()
		if err := node.restoreConsensusSafetyState(); err != nil {
			t.Fatalf("restore replay %d: %v", replay, err)
		}
		height := node.getFinalizedHeight()
		hash, ok := node.getCommittedHash(height)
		if !ok {
			t.Fatalf("missing committed hash after replay %d height=%d", replay, height)
		}
		block, ok := node.Blockchain.GetBlock(height)
		if !ok {
			t.Fatalf("missing block after replay %d height=%d", replay, height)
		}
		if replay > 0 && (height != prevHeight || hash != prevHash || block.StateRoot != prevStateRoot) {
			t.Fatalf("WAL replay was not idempotent: h=%d/%d hash=%q/%q state=%q/%q", height, prevHeight, hash, prevHash, block.StateRoot, prevStateRoot)
		}
		prevHeight, prevHash, prevStateRoot = height, hash, block.StateRoot
	}
}

func TestCrashMatrixSuite(t *testing.T) {
	t.Run("before WAL commit", func(t *testing.T) {
		node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
		blocks := stateRecoveryBlocks(2)
		stateRecoverySetCommitTip(t, node, blocks)
		node.Consensus = NewConsensusState(1)
		node.restoreCommittedHeightFromChain()
		if got := node.getFinalizedHeight(); got != 2 {
			t.Fatalf("chain-derived recovery failed: got=%d want=2", got)
		}
	})
	t.Run("after WAL before DB", func(t *testing.T) {
		node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
		block := stateRecoveryBlocks(1)[0]
		node.StoreBlock(block)
		if err := node.DB.Blocks.Update(func(txn *Txn) error {
			return txn.Set([]byte("block:1"), []byte("{torn-block-db"))
		}); err != nil {
			t.Fatalf("corrupt block db: %v", err)
		}
		if got, ok := node.LoadBlock(1); !ok || got.BlockHash != block.BlockHash {
			t.Fatalf("block file fallback failed: got=%+v ok=%t", got, ok)
		}
	})
	t.Run("after DB before checkpoint", func(t *testing.T) {
		node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
		block := storageDurabilityFinalityBlock(t, node, 1, GenesisHash)
		node.StoreBlock(block)
		if err := node.verifyPersistedFinalityCheckpoint(block); err != nil {
			t.Fatalf("missing optional checkpoint should not break startup: %v", err)
		}
	})
	t.Run("during snapshot", func(t *testing.T) {
		node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
		good := storeDurabilitySnapshotForCrashTest(t, node, 1, true)
		if err := node.DB.State.Update(func(txn *Txn) error {
			return txn.Set([]byte("snapshot:2"), []byte("{partial"))
		}); err != nil {
			t.Fatalf("write partial snapshot: %v", err)
		}
		snapshot, err := node.loadBestReadableSnapshotAtOrBelow(2)
		if err != nil || snapshot.Height != good.Height {
			t.Fatalf("snapshot fallback failed height=%d err=%v", snapshot.Height, err)
		}
	})
	t.Run("during compaction", func(t *testing.T) {
		node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
		block := stateRecoveryBlocks(1)[0]
		node.StoreBlock(block)
		if err := os.WriteFile(filepath.Join(blockStoreDir(node.DataDir, node.ID), "p2p_identity_compaction.tmp"), []byte("{partial"), 0o600); err != nil {
			t.Fatalf("write compaction temp: %v", err)
		}
		if blocks := node.loadBlockFilesFromDisk(); len(blocks) != 1 || blocks[0].BlockHash != block.BlockHash {
			t.Fatalf("compaction temp leaked into block scan: %+v", blocks)
		}
	})
	t.Run("during pruning", func(t *testing.T) {
		node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
		blocks := stateRecoveryBlocks(2)
		for _, block := range blocks {
			node.StoreBlock(block)
		}
		node.pruneBlocksAboveHeight(1)
		if _, ok := node.LoadBlock(1); !ok {
			t.Fatal("retained block was pruned")
		}
		if _, ok := node.LoadBlock(2); ok {
			t.Fatal("future block survived prune")
		}
	})
}

func TestCorruptSnapshotManifestRejected(t *testing.T) {
	_, snapshot := makeSnapshotLayerFixture(12, "manifest-prev", NewLedger(), testValidatorSetMaterializationRegistry())
	manifest, payload, err := snapshotManifestFromSnapshot(&snapshot)
	if err != nil {
		t.Fatalf("build snapshot manifest: %v", err)
	}
	if _, err := verifySnapshotPayloadAgainstManifest(payload, manifest, 0); err != nil {
		t.Fatalf("baseline manifest should verify: %v", err)
	}
	corruptChunk := *manifest
	corruptChunk.ChunkHashes = append([]string{}, manifest.ChunkHashes...)
	corruptChunk.ChunkHashes[0] = "bad-checksum"
	if _, err := verifySnapshotPayloadAgainstManifest(payload, &corruptChunk, 0); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected corrupt manifest checksum rejection, got %v", err)
	}
	corruptRoot := *manifest
	corruptRoot.StateRoot = "wrong-state-root"
	if _, err := verifySnapshotPayloadAgainstManifest(payload, &corruptRoot, 0); err == nil || !strings.Contains(err.Error(), "state root") {
		t.Fatalf("expected corrupt manifest state root rejection, got %v", err)
	}
	corruptTail := append(append([]byte{}, payload...), 0x42)
	corruptSize := *manifest
	corruptSize.ChunkSize = uint64(len(payload))
	corruptSize.ChunkCount = 1
	corruptSize.ChunkHashes = []string{snapshotChunkHash(payload)}
	corruptSize.SnapshotSizeBytes = uint64(len(payload))
	if _, err := verifySnapshotPayloadAgainstManifest(corruptTail, &corruptSize, 0); err == nil || !strings.Contains(err.Error(), "payload size") {
		t.Fatalf("expected corrupt manifest payload size rejection, got %v", err)
	}
}

func TestCorruptFECRejected(t *testing.T) {
	node, block1, block2 := storageDurabilityFinalityPair(t)
	if err := node.persistFinalityCheckpoint(block2); err != nil {
		t.Fatalf("persist block2 finality checkpoint: %v", err)
	}
	path := finalityArtifactFilePath(node.DataDir, node.ID, finalityCertificatesDir, block2.ID)
	if err := os.WriteFile(path, []byte("{corrupt-fec"), 0o600); err != nil {
		t.Fatalf("corrupt FEC artifact: %v", err)
	}
	if err := node.verifyFinalityArtifacts(block2); err == nil {
		t.Fatal("corrupt FEC artifact was trusted")
	}
	if err := node.verifyFinalityArtifacts(block1); err != nil {
		t.Fatalf("previous valid FEC should remain usable: %v", err)
	}
}

func TestCorruptAnchorRejected(t *testing.T) {
	node, _, block2 := storageDurabilityFinalityPair(t)
	if err := node.persistFinalityCheckpoint(block2); err != nil {
		t.Fatalf("persist block2 finality checkpoint: %v", err)
	}
	anchor := epochAnchorRecordFromBlock(block2)
	anchor.AnchorHash = "wrong-anchor"
	if err := writeFinalityArtifactJSON(finalityArtifactFilePath(node.DataDir, node.ID, finalityEpochAnchorsDir, block2.ID), anchor); err != nil {
		t.Fatalf("write corrupt anchor: %v", err)
	}
	err := node.verifyFinalityArtifacts(block2)
	if err == nil || !strings.Contains(err.Error(), "irreversible_finality_artifact_conflict") {
		t.Fatalf("expected corrupt anchor rejection, got %v", err)
	}
}

func TestCorruptValidatorCommitmentRejected(t *testing.T) {
	node, _, block2 := storageDurabilityFinalityPair(t)
	if err := node.persistFinalityCheckpoint(block2); err != nil {
		t.Fatalf("persist block2 finality checkpoint: %v", err)
	}
	commitment := validatorCommitmentRecordFromBlock(block2)
	commitment.FinalizedValidatorSetRoot = "wrong-validator-root"
	if err := writeFinalityArtifactJSON(finalityArtifactFilePath(node.DataDir, node.ID, finalityValidatorCommitmentsDir, block2.ID), commitment); err != nil {
		t.Fatalf("write corrupt validator commitment: %v", err)
	}
	err := node.verifyFinalityArtifacts(block2)
	if err == nil || !strings.Contains(err.Error(), "irreversible_finality_artifact_conflict") {
		t.Fatalf("expected corrupt validator commitment rejection, got %v", err)
	}
}

func marshalForStorageDurability(value any) ([]byte, error) {
	return json.Marshal(value)
}
