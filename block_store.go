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
	Height       uint64        `json:"height"`
	BlockHash    string        `json:"block_hash"`
	BlockHeader  BlockHeader   `json:"block_header"`
	Transactions []Transaction `json:"transactions"`
	ValidatorSet []string      `json:"validator_set"`
	StateRoot    string        `json:"state_root"`
	Block        Block         `json:"block"`
}

func blockStoreDir(dataDir, nodeID string) string {
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = "."
	}
	id := strings.TrimSpace(nodeID)
	if id == "" {
		id = "node"
	}
	return filepath.Join(base, "node_"+id, "blocks")
}

func blockStoreFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(blockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%06d.json", height))
}

func blockStoreProtoFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(blockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%06d.mpb", height))
}

func coldBlockStoreDir(dataDir, nodeID string) string {
	return filepath.Join(nodeDataPath(dataDir, nodeID), "cold-storage", "blocks")
}

func coldBlockStoreZstdFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(coldBlockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%020d.json.zst", height))
}

func coldBlockStoreRawFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(coldBlockStoreDir(dataDir, nodeID), fmt.Sprintf("block_%020d.json", height))
}

func blockStorageHeight(block Block) uint64 {
	if block.ID > 0 {
		return block.ID
	}
	return block.Height
}

func (n *Node) persistBlockFile(block Block) error {
	if n == nil {
		return fmt.Errorf("node not initialized")
	}
	height := blockStorageHeight(block)
	if height == 0 {
		return nil
	}
	dir := blockStoreDir(n.DataDir, n.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create block store dir: %w", err)
	}

	storedBlock := block
	if storedBlock.ID == 0 {
		storedBlock.ID = height
	}
	if storedBlock.Height == 0 {
		storedBlock.Height = height
	}

	record := BlockFileRecord{
		Height:       height,
		BlockHash:    strings.TrimSpace(storedBlock.BlockHash),
		BlockHeader:  buildBlockFileHeader(storedBlock, height),
		Transactions: append([]Transaction{}, storedBlock.Transactions...),
		ValidatorSet: n.blockFileValidatorSet(height, storedBlock),
		StateRoot:    storedBlock.StateRoot,
		Block:        storedBlock,
	}

	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal block file: %w", err)
	}
	if err := writeFileAtomic(blockStoreFilePath(n.DataDir, n.ID, height), raw, 0o600); err != nil {
		return fmt.Errorf("persist block file: %w", err)
	}
	if protoRaw, err := MarshalBlockFileRecordProtobuf(record); err == nil {
		if err := writeFileAtomic(blockStoreProtoFilePath(n.DataDir, n.ID, height), protoRaw, 0o600); err != nil {
			return fmt.Errorf("persist protobuf block file: %w", err)
		}
	} else {
		return fmt.Errorf("marshal protobuf block file: %w", err)
	}
	return nil
}

func (n *Node) loadBlockFile(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	if raw, err := os.ReadFile(blockStoreProtoFilePath(n.DataDir, n.ID, height)); err == nil {
		return decodeBlockFileRecord(raw, height)
	}
	raw, err := os.ReadFile(blockStoreFilePath(n.DataDir, n.ID, height))
	if err == nil {
		return decodeBlockFileRecord(raw, height)
	}
	return n.loadColdBlockFile(height)
}

func decodeBlockFileRecord(raw []byte, height uint64) (Block, bool) {
	var record BlockFileRecord
	if bytes.HasPrefix(raw, []byte(blockFileProtoMagic)) {
		if err := UnmarshalBlockFileRecordProtobuf(raw, &record); err != nil {
			return Block{}, false
		}
	} else if err := json.Unmarshal(raw, &record); err != nil {
		return Block{}, false
	}
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

func (n *Node) loadColdBlockFile(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	if raw, err := os.ReadFile(coldBlockStoreZstdFilePath(n.DataDir, n.ID, height)); err == nil {
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return Block{}, false
		}
		defer decoder.Close()
		decoded, err := decoder.DecodeAll(raw, nil)
		if err != nil {
			return Block{}, false
		}
		return decodeBlockFileRecord(decoded, height)
	}
	if raw, err := os.ReadFile(coldBlockStoreRawFilePath(n.DataDir, n.ID, height)); err == nil {
		return decodeBlockFileRecord(raw, height)
	}
	return Block{}, false
}

func (n *Node) restoreColdBlockFile(height uint64) (bool, error) {
	if n == nil || height == 0 {
		return false, nil
	}
	block, ok := n.loadColdBlockFile(height)
	if !ok {
		return false, nil
	}
	if err := n.persistBlockFile(block); err != nil {
		return false, err
	}
	return true, nil
}

func (n *Node) loadBlockFilesFromDisk() []Block {
	if n == nil {
		return nil
	}
	dir := blockStoreDir(n.DataDir, n.ID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := make(map[uint64]struct{}, len(entries))
	heights := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "block_") || (!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".mpb")) {
			continue
		}
		rawHeight := strings.TrimPrefix(name, "block_")
		rawHeight = strings.TrimSuffix(strings.TrimSuffix(rawHeight, ".json"), ".mpb")
		height, err := strconv.ParseUint(rawHeight, 10, 64)
		if err == nil && height > 0 {
			if _, ok := seen[height]; ok {
				continue
			}
			seen[height] = struct{}{}
			heights = append(heights, height)
		}
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	blocks := make([]Block, 0, len(heights))
	for _, height := range heights {
		if block, ok := n.loadBlockFile(height); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func buildBlockFileHeader(block Block, height uint64) BlockHeader {
	return BlockHeader{
		Height:                    height,
		PrevHash:                  block.PrevHash,
		StateRoot:                 block.StateRoot,
		TxRoot:                    block.MempoolRoot,
		ValidatorSetHash:          block.ValidatorSetHash,
		ValidatorSetRoot:          block.ValidatorSetRoot,
		ValidatorRegistryHash:     block.ValidatorRegistryHash,
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

func (n *Node) blockFileValidatorSet(height uint64, block Block) []string {
	if n == nil || height == 0 {
		return nil
	}
	if validators := n.freezeValidatorSetForHeight(height, n.GetConsensusValidators(int(height))); len(validators) > 0 {
		return canonicalValidatorIDs(validators)
	}
	if validators, ok := n.blockValidatorSetFromSignatures(block); ok && len(validators) > 0 {
		return canonicalValidatorIDs(validators)
	}
	return canonicalValidatorIDs(block.Signatures)
}

func (n *Node) backfillBlockFiles(blocks []Block) {
	if n == nil || len(blocks) == 0 {
		return
	}
	for _, block := range blocks {
		height := blockStorageHeight(block)
		if height == 0 {
			continue
		}
		if err := n.persistBlockFile(block); err != nil {
			log.Printf("store block file failed during backfill (height=%d): %v", height, err)
		}
	}
}

func deleteBlockFilesAboveHeight(dataDir, nodeID string, height uint64) error {
	dir := blockStoreDir(dataDir, nodeID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "block_") || (!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".mpb")) {
			continue
		}
		rawHeight := strings.TrimPrefix(name, "block_")
		rawHeight = strings.TrimSuffix(strings.TrimSuffix(rawHeight, ".json"), ".mpb")
		blockHeight, err := strconv.ParseUint(rawHeight, 10, 64)
		if err != nil {
			continue
		}
		if blockHeight <= height {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
