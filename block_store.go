package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type BlockFileRecord struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `BlockHeader` stores the block data handled by this operation.
	BlockHeader BlockHeader `json:"block_header"`
	// `Transactions` stores the transaction data handled by this operation.
	Transactions []Transaction `json:"transactions"`
	// `ValidatorSet` stores whether the related condition is satisfied.
	ValidatorSet []string `json:"validator_set"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `Block` stores the synchronization state protecting shared data.
	Block Block `json:"block"`
}

// blockStoreDir implements the block store dir helper.
func blockStoreDir(dataDir, nodeID string) string {
	// `base` stores the value produced by this operation.
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = "."
	}
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(nodeID)
	if id == "" {
		id = "node"
	}
	return filepath.Join(base, "node_"+id, "blocks")
}

// blockStoreFilePath implements the block store file path helper.
func blockStoreFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(blockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%06d.json", height))
}

// blockStoreProtoFilePath implements the block store proto file path helper.
func blockStoreProtoFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(blockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%06d.mpb", height))
}

// blockStoreFileExists implements the block store file exists helper.
func blockStoreFileExists(dataDir, nodeID string, height uint64) bool {
	if height == 0 {
		return false
	}
	// `err` stores the error produced by this operation.
	if _, err := os.Stat(blockStoreProtoFilePath(dataDir, nodeID, height)); err == nil {
		return true
	}
	// `err` stores the error produced by this operation.
	if _, err := os.Stat(blockStoreFilePath(dataDir, nodeID, height)); err == nil {
		return true
	}
	return false
}

// coldBlockStoreDir implements the cold block store dir helper.
func coldBlockStoreDir(dataDir, nodeID string) string {
	return filepath.Join(nodeDataPath(dataDir, nodeID), "cold-storage", "blocks")
}

// coldBlockStoreZstdFilePath implements the cold block store zstd file path helper.
func coldBlockStoreZstdFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(coldBlockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%020d.json.zst", height))
}

// coldBlockStoreRawFilePath implements the cold block store raw file path helper.
func coldBlockStoreRawFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(coldBlockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%020d.json", height))
}

// blockStorageHeight implements the block storage height helper.
func blockStorageHeight(block Block) uint64 {
	if block.ID > 0 {
		return block.ID
	}
	return block.Height
}

// persistBlockFile implements the persist block file helper.
func (n *Node) persistBlockFile(block Block) error {
	if n == nil {
		return fmt.Errorf("node not initialized")
	}
	// `height` stores the value produced by this operation.
	height := blockStorageHeight(block)
	if height == 0 {
		return nil
	}
	// `dir` stores the value produced by this operation.
	dir := blockStoreDir(n.DataDir, n.ID)
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create block store dir: %w", err)
	}

	// `storedBlock` stores the synchronization state protecting shared data.
	storedBlock := block
	if storedBlock.ID == 0 {
		storedBlock.ID = height
	}
	if storedBlock.Height == 0 {
		storedBlock.Height = height
	}

	// `record` stores the value produced by this operation.
	record := BlockFileRecord{
		Height:       height,
		BlockHash:    strings.TrimSpace(storedBlock.BlockHash),
		BlockHeader:  buildBlockFileHeader(storedBlock, height),
		Transactions: append([]Transaction{}, storedBlock.Transactions...),
		ValidatorSet: n.blockFileValidatorSet(height, storedBlock),
		StateRoot:    storedBlock.StateRoot,
		Block:        storedBlock,
	}

	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal block file: %w", err)
	}
	// `err` stores the error produced by this operation.
	if err := writeFileAtomic(blockStoreFilePath(n.DataDir, n.ID, height), raw, 0o600); err != nil {
		return fmt.Errorf("persist block file: %w", err)
	}
	// `protoRaw` and `err` store the error produced by this operation.
	if protoRaw, err := MarshalBlockFileRecordProtobuf(record); err == nil {
		// `err` stores the error produced by this operation.
		if err := writeFileAtomic(blockStoreProtoFilePath(n.DataDir, n.ID, height), protoRaw, 0o600); err != nil {
			return fmt.Errorf("persist protobuf block file: %w", err)
		}
	} else {
		return fmt.Errorf("marshal protobuf block file: %w", err)
	}
	return nil
}

// loadBlockFile implements the load block file helper.
func (n *Node) loadBlockFile(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(blockStoreProtoFilePath(n.DataDir, n.ID, height)); err == nil {
		return decodeBlockFileRecord(raw, height)
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(blockStoreFilePath(n.DataDir, n.ID, height))
	if err == nil {
		return decodeBlockFileRecord(raw, height)
	}
	return n.loadColdBlockFile(height)
}

// decodeBlockFileRecord implements the decode block file record helper.
func decodeBlockFileRecord(raw []byte, height uint64) (Block, bool) {
	// `record` stores the value used by this operation.
	var record BlockFileRecord
	// `err` stores the error produced by this operation.
	if bytes.HasPrefix(raw, []byte(blockFileProtoMagic)) {
		// `err` stores the error produced by this operation.
		if err := UnmarshalBlockFileRecordProtobuf(raw, &record); err != nil {
			return Block{}, false
		}
	} else if err := json.Unmarshal(raw, &record); err != nil {
		return Block{}, false
	}
	// `block` stores the synchronization state protecting shared data.
	block := record.Block
	if block.ID == 0 {
		block.ID = record.Height
	}
	if block.Height == 0 {
		block.Height = record.Height
	}
	if block.ID != height || strings.TrimSpace(block.BlockHash) == "" {
		return Block{}, false
	}
	return block, true
}

// loadColdBlockFile implements the load cold block file helper.
func (n *Node) loadColdBlockFile(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(coldBlockStoreZstdFilePath(n.DataDir, n.ID, height)); err == nil {
		// `decoder` and `err` store the error produced by this operation.
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return Block{}, false
		}
		defer decoder.Close()
		// `decoded` and `err` store the error produced by this operation.
		decoded, err := decoder.DecodeAll(raw, nil)
		if err != nil {
			return Block{}, false
		}
		return decodeBlockFileRecord(decoded, height)
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(coldBlockStoreRawFilePath(n.DataDir, n.ID, height)); err == nil {
		return decodeBlockFileRecord(raw, height)
	}
	return Block{}, false
}

// restoreColdBlockFile implements the restore cold block file helper.
func (n *Node) restoreColdBlockFile(height uint64) (bool, error) {
	if n == nil || height == 0 {
		return false, nil
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.loadColdBlockFile(height)
	if !ok {
		return false, nil
	}
	// `err` stores the error produced by this operation.
	if err := n.persistBlockFile(block); err != nil {
		return false, err
	}
	return true, nil
}

// loadBlockFilesFromDisk implements the load block files from disk helper.
func (n *Node) loadBlockFilesFromDisk() []Block {
	if n == nil {
		return nil
	}
	// `dir` stores the value produced by this operation.
	dir := blockStoreDir(n.DataDir, n.ID)
	// `entries` and `err` store the error produced by this operation.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[uint64]struct{}, len(entries))
	// `heights` stores the value produced by this operation.
	heights := make([]uint64, 0, len(entries))
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// `name` stores the value produced by this operation.
		name := entry.Name()
		if !strings.HasPrefix(name, "block_") || (!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".mpb")) {
			continue
		}
		// `rawHeight` stores the value produced by this operation.
		rawHeight := strings.TrimPrefix(name, "block_")
		rawHeight = strings.TrimSuffix(strings.TrimSuffix(rawHeight, ".json"), ".mpb")
		// `height` and `err` store the error produced by this operation.
		height, err := strconv.ParseUint(rawHeight, 10, 64)
		if err == nil && height > 0 {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := seen[height]; ok {
				continue
			}
			seen[height] = struct{}{}
			heights = append(heights, height)
		}
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	// `blocks` stores the block data handled by this operation.
	blocks := make([]Block, 0, len(heights))
	// `height` tracks the current values while iterating.
	for _, height := range heights {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := n.loadBlockFile(height); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// buildBlockFileHeader builds block file header.
func buildBlockFileHeader(block Block, height uint64) BlockHeader {
	return BlockHeader{
		Height:                    height,
		PrevHash:                  block.PrevHash,
		StateRoot:                 block.StateRoot,
		ReceiptsRoot:              executionCommitmentsFromBlock(block).ReceiptsRoot,
		EventRoot:                 block.EventRoot,
		ExecutionHash:             block.ExecutionHash,
		FeeRoot:                   block.FeeRoot,
		DTLStateRoot:              block.DTLStateRoot,
		DTLReceiptsRoot:           block.DTLReceiptsRoot,
		ProtocolVersion:           block.ProtocolVersion,
		FeatureBitmap:             block.FeatureBitmap,
		DTLV2ActivationHeight:     block.DTLV2ActivationHeight,
		ValidatorSetVersion:       block.ValidatorSetVersion,
		CommitteeHash:             block.CommitteeHash,
		TxRoot:                    block.MempoolRoot,
		ValidatorSetHash:          block.ValidatorSetHash,
		ValidatorSetRoot:          block.ValidatorSetRoot,
		ValidatorRegistryHash:     block.ValidatorRegistryHash,
		PromotionWindowHash:       block.PromotionWindowHash,
		NextValidatorSetHash:      block.NextValidatorSetHash,
		NextValidatorSetRoot:      block.NextValidatorSetRoot,
		FinalityRoot:              block.FinalityRoot,
		EpochAnchorHash:           block.EpochAnchorHash,
		FinalizedHeight:           block.FinalizedHeight,
		FinalizedStateRoot:        block.FinalizedStateRoot,
		FinalizedValidatorSetHash: block.FinalizedValidatorSetHash,
		FinalizedValidatorSetRoot: block.FinalizedValidatorSetRoot,
		Proposer:                  block.Proposer,
		Timestamp:                 block.Timestamp,
	}
}

// blockFileValidatorSet implements the block file validator set helper.
func (n *Node) blockFileValidatorSet(height uint64, block Block) []string {
	if n == nil || height == 0 {
		return nil
	}
	// `validators` stores whether the related condition is satisfied.
	if validators := n.freezeValidatorSetForHeight(height, n.GetConsensusValidators(int(height))); len(validators) > 0 {
		return canonicalValidatorIDs(validators)
	}
	// `validators` and `ok` store whether the related condition is satisfied.
	if validators, ok := n.blockValidatorSetFromSignatures(block); ok && len(validators) > 0 {
		return canonicalValidatorIDs(validators)
	}
	return canonicalValidatorIDs(block.Signatures)
}

// backfillBlockFiles implements the backfill block files helper.
func (n *Node) backfillBlockFiles(blocks []Block) {
	if n == nil || len(blocks) == 0 {
		return
	}
	// `written` stores the value produced by this operation.
	written := 0
	// `skipped` stores the value produced by this operation.
	skipped := 0
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range blocks {
		// `height` stores the value produced by this operation.
		height := blockStorageHeight(block)
		if height == 0 {
			continue
		}
		if blockStoreFileExists(n.DataDir, n.ID, height) {
			skipped++
			continue
		}
		// `err` stores the error produced by this operation.
		if err := n.persistBlockFile(block); err != nil {
			log.Printf("store block file failed during backfill (height=%d): %v", height, err)
			continue
		}
		written++
	}
	if written > 0 {
		log.Printf("[BLOCK-FILE-BACKFILL] written=%d skipped_existing=%d", written, skipped)
	}
}

// deleteBlockFilesAboveHeight implements the delete block files above height helper.
func deleteBlockFilesAboveHeight(dataDir, nodeID string, height uint64) error {
	// `dir` stores the value produced by this operation.
	dir := blockStoreDir(dataDir, nodeID)
	// `entries` and `err` store the error produced by this operation.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// `name` stores the value produced by this operation.
		name := entry.Name()
		if !strings.HasPrefix(name, "block_") || (!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".mpb")) {
			continue
		}
		// `rawHeight` stores the value produced by this operation.
		rawHeight := strings.TrimPrefix(name, "block_")
		rawHeight = strings.TrimSuffix(strings.TrimSuffix(rawHeight, ".json"), ".mpb")
		// `blockHeight` and `err` store the error produced by this operation.
		blockHeight, err := strconv.ParseUint(rawHeight, 10, 64)
		if err != nil {
			continue
		}
		if blockHeight <= height {
			continue
		}
		// `err` stores the error produced by this operation.
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
