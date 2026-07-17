package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	// `storageProfileAuto` defines the constant value used by this package.
	storageProfileAuto      = "auto"
	// `storageProfileValidator` defines the constant value used by this package.
	storageProfileValidator = "validator"
	// `storageProfileFull` defines the constant value used by this package.
	storageProfileFull      = "full"
	// `storageProfileArchive` defines the constant value used by this package.
	storageProfileArchive   = "archive"

	// `stateCheckpointVersion` defines the constant value used by this package.
	stateCheckpointVersion  = "state_checkpoint_v1"
	// `stateCheckpointDomain` defines the constant value used by this package.
	stateCheckpointDomain   = "MSC_STATE_CHECKPOINT_V1"
	// `stateCheckpointDBPrefix` defines the constant value used by this package.
	stateCheckpointDBPrefix = "state_checkpoint:"
)

type StateCheckpoint struct {
	// `Version` stores the value associated with this record.
	Version                   string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain                    string `json:"domain"`
	// `Height` stores the value associated with this record.
	Height                    uint64 `json:"height"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot                 string `json:"state_root"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot              string `json:"finality_root"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash           string `json:"epoch_anchor_hash"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash          string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot          string `json:"validator_set_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash,omitempty"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash                 string `json:"block_hash"`
	// `FinalityCertificateHash` stores the digest used to identify or verify the related data.
	FinalityCertificateHash   string `json:"finality_certificate_hash,omitempty"`
	// `CreatedAtUnix` stores the value associated with this record.
	CreatedAtUnix             int64  `json:"created_at_unix"`
}

type StoragePolicy struct {
	// `Profile` stores the value associated with this record.
	Profile                      string
	// `PruningEnabled` stores whether the related condition is satisfied.
	PruningEnabled               bool
	// `EpochLengthBlocks` stores the value associated with this record.
	EpochLengthBlocks            uint64
	// `RetainedEpochs` stores the value associated with this record.
	RetainedEpochs               uint64
	// `RollbackWindowBlocks` stores the value associated with this record.
	RollbackWindowBlocks         uint64
	// `SnapshotKeepLast` stores the value associated with this record.
	SnapshotKeepLast             uint64
	// `RecentBlockWindow` stores the value associated with this record.
	RecentBlockWindow            uint64
	// `HourlySnapshotRetain` stores the value associated with this record.
	HourlySnapshotRetain         uint64
	// `DailySnapshotRetain` stores the value associated with this record.
	DailySnapshotRetain          uint64
	// `WeeklySnapshotRetain` stores the value associated with this record.
	WeeklySnapshotRetain         uint64
	// `MonthlySnapshotRetain` stores the value associated with this record.
	MonthlySnapshotRetain        uint64
	// `HourlySnapshotIntervalBlocks` stores the value associated with this record.
	HourlySnapshotIntervalBlocks uint64
	// `ColdExportEnabled` stores whether the related condition is satisfied.
	ColdExportEnabled            bool
	// `ColdExportCompression` stores the value associated with this record.
	ColdExportCompression        string
	// `ParallelGCWorkers` stores the value associated with this record.
	ParallelGCWorkers            uint64
	// `StateRentEnabled` stores whether the related condition is satisfied.
	StateRentEnabled             bool
	// `StateRentArchiveEpochs` stores the value associated with this record.
	StateRentArchiveEpochs       uint64
	// `StateLayoutMode` stores the value associated with this record.
	StateLayoutMode              string
}

type StorageManager struct {
	// `Node` stores the value associated with this record.
	Node   *Node
	// `Policy` stores the value associated with this record.
	Policy StoragePolicy
}

type StorageManagerReport struct {
	// `Profile` stores the value associated with this record.
	Profile                string `json:"profile"`
	// `PruningEnabled` stores whether the related condition is satisfied.
	PruningEnabled         bool   `json:"pruning_enabled"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight        uint64 `json:"finalized_height"`
	// `RetainFromHeight` stores the value associated with this record.
	RetainFromHeight       uint64 `json:"retain_from_height"`
	// `HotWindowBlocks` stores the value associated with this record.
	HotWindowBlocks        uint64 `json:"hot_window_blocks"`
	// `SnapshotsPruned` stores the value associated with this record.
	SnapshotsPruned        int    `json:"snapshots_pruned"`
	// `RegistryPruned` stores the value associated with this record.
	RegistryPruned         bool   `json:"registry_pruned"`
	// `ExecutionCachePruned` stores the value associated with this record.
	ExecutionCachePruned   int    `json:"execution_cache_pruned"`
	// `BlockFilesPruned` stores the block data handled by this operation.
	BlockFilesPruned       int    `json:"block_files_pruned"`
	// `ColdStorageExported` stores the value associated with this record.
	ColdStorageExported    int    `json:"cold_storage_exported"`
	// `StateCheckpointHeight` stores the value associated with this record.
	StateCheckpointHeight  uint64 `json:"state_checkpoint_height"`
	// `ArchiveModeSkipped` stores the value associated with this record.
	ArchiveModeSkipped     bool   `json:"archive_mode_skipped"`
	// `PruningDisabledSkipped` stores the value associated with this record.
	PruningDisabledSkipped bool   `json:"pruning_disabled_skipped"`
	// `ParallelGCWorkers` stores the value associated with this record.
	ParallelGCWorkers      uint64 `json:"parallel_gc_workers"`
	// `StateRentEnabled` stores whether the related condition is satisfied.
	StateRentEnabled       bool   `json:"state_rent_enabled"`
	// `StateLayoutMode` stores the value associated with this record.
	StateLayoutMode        string `json:"state_layout_mode"`
}

// normalizeStorageHistoryProfile normalizes storage history profile.
func normalizeStorageHistoryProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", storageProfileAuto:
		return storageProfileAuto
	case storageProfileValidator, "validator_node":
		return storageProfileValidator
	case storageProfileFull, "full_node":
		return storageProfileFull
	case storageProfileArchive, "archive_node":
		return storageProfileArchive
	default:
		return storageProfileAuto
	}
}

// storageProfileForNode implements the storage profile for node helper.
func storageProfileForNode(n *Node) string {
	// `configured` stores the configuration used by this operation.
	configured := normalizeStorageHistoryProfile(StorageHistoryProfile)
	if configured == storageProfileArchive || normalizeSyncHistoryMode(SyncHistoryMode) == SyncHistoryModeArchive {
		return storageProfileArchive
	}
	if configured == storageProfileValidator || configured == storageProfileFull {
		return configured
	}
	if n != nil && strings.EqualFold(strings.TrimSpace(n.Role), "full") {
		return storageProfileFull
	}
	return storageProfileValidator
}

// defaultStoragePolicyForNode returns the default storage policy for node.
func defaultStoragePolicyForNode(n *Node) StoragePolicy {
	// `profile` stores the value produced by this operation.
	profile := storageProfileForNode(n)
	// `policy` stores the value produced by this operation.
	policy := StoragePolicy{
		Profile:                      profile,
		PruningEnabled:               StorageStatePruningEnabled,
		EpochLengthBlocks:            StorageEpochLengthBlocks,
		RetainedEpochs:               StorageValidatorRetainedEpochs,
		RollbackWindowBlocks:         StorageValidatorRollbackWindowBlocks,
		SnapshotKeepLast:             StorageValidatorSnapshotKeepLast,
		RecentBlockWindow:            StorageValidatorRecentBlockWindow,
		HourlySnapshotRetain:         StorageHourlySnapshotRetain,
		DailySnapshotRetain:          StorageDailySnapshotRetain,
		WeeklySnapshotRetain:         StorageWeeklySnapshotRetain,
		MonthlySnapshotRetain:        StorageMonthlySnapshotRetain,
		HourlySnapshotIntervalBlocks: StorageHourlySnapshotIntervalBlocks,
		ColdExportEnabled:            StorageColdExportEnabled,
		ColdExportCompression:        StorageColdExportCompression,
		ParallelGCWorkers:            StorageParallelGCWorkers,
		StateRentEnabled:             StorageStateRentEnabled,
		StateRentArchiveEpochs:       StorageStateRentArchiveInactiveAfterEpochs,
		StateLayoutMode:              StorageStateLayoutMode,
	}
	if policy.EpochLengthBlocks == 0 {
		policy.EpochLengthBlocks = 100
	}
	if policy.HourlySnapshotIntervalBlocks == 0 {
		policy.HourlySnapshotIntervalBlocks = policy.EpochLengthBlocks
	}
	if profile == storageProfileFull {
		policy.RetainedEpochs = 0
		policy.RollbackWindowBlocks = StorageFullNodeHistoryBlocks
		policy.SnapshotKeepLast = maxUint64Value(policy.SnapshotKeepLast, 24)
		policy.RecentBlockWindow = StorageFullNodeHistoryBlocks
	}
	if profile == storageProfileArchive {
		policy.ColdExportEnabled = false
		policy.PruningEnabled = false
	}
	return policy
}

// NewStorageManager creates a new storage manager.
func NewStorageManager(n *Node) *StorageManager {
	return &StorageManager{Node: n, Policy: defaultStoragePolicyForNode(n)}
}

// storageManagerFinalizedEpochBoundary implements the storage manager finalized epoch boundary helper.
func storageManagerFinalizedEpochBoundary(height uint64, policy StoragePolicy) bool {
	if height == 0 {
		return false
	}
	// `interval` stores the value currently being processed.
	interval := policy.EpochLengthBlocks
	if interval == 0 {
		interval = 100
	}
	return height%interval == 0
}

// beginStorageManagerRun implements the begin storage manager run helper.
func (n *Node) beginStorageManagerRun(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	n.storageManagerMu.Lock()
	defer n.storageManagerMu.Unlock()
	if n.storageManagerRunning || height <= n.storageManagerLastScheduledHeight {
		return false
	}
	n.storageManagerRunning = true
	n.storageManagerLastScheduledHeight = height
	return true
}

// finishStorageManagerRun implements the finish storage manager run helper.
func (n *Node) finishStorageManagerRun() {
	if n == nil {
		return
	}
	n.storageManagerMu.Lock()
	n.storageManagerRunning = false
	n.storageManagerMu.Unlock()
}

// runStorageManagerAfterFinalizedEpoch implements the run storage manager after finalized epoch helper.
func (n *Node) runStorageManagerAfterFinalizedEpoch(block Block, reason string) {
	if n == nil || !blockFinalityCommitmentsPresent(block) {
		return
	}
	// `manager` stores the value produced by this operation.
	manager := NewStorageManager(n)
	manager.Policy = manager.normalizePolicy()
	if !storageManagerFinalizedEpochBoundary(block.ID, manager.Policy) {
		return
	}
	if !n.beginStorageManagerRun(block.ID) {
		return
	}
	n.SafeGo(fmt.Sprintf("storage_manager_%d", block.ID), func() {
		defer n.finishStorageManagerRun()
		// `report` and `err` store the error produced by this operation.
		report, err := manager.RunOnce(reason)
		if err != nil {
			log.Printf("[STORAGE-MANAGER] height=%d status=failed err=%v", block.ID, err)
			return
		}
		log.Printf("[STORAGE-MANAGER] height=%d profile=%s retain_from=%d snapshots_pruned=%d blocks_pruned=%d cold_exports=%d checkpoint=%d archive_skip=%t gc_workers=%d layout=%s",
			block.ID,
			report.Profile,
			report.RetainFromHeight,
			report.SnapshotsPruned,
			report.BlockFilesPruned,
			report.ColdStorageExported,
			report.StateCheckpointHeight,
			report.ArchiveModeSkipped,
			report.ParallelGCWorkers,
			report.StateLayoutMode,
		)
	})
}

// normalizePolicy normalizes policy.
func (m *StorageManager) normalizePolicy() StoragePolicy {
	if m == nil {
		return defaultStoragePolicyForNode(nil)
	}
	// `policy` stores the value produced by this operation.
	policy := m.Policy
	if strings.TrimSpace(policy.Profile) == "" {
		policy = defaultStoragePolicyForNode(m.Node)
	}
	policy.Profile = normalizeStorageHistoryProfile(policy.Profile)
	if policy.Profile == storageProfileAuto {
		policy.Profile = storageProfileForNode(m.Node)
	}
	if policy.EpochLengthBlocks == 0 {
		policy.EpochLengthBlocks = 100
	}
	if policy.HourlySnapshotIntervalBlocks == 0 {
		policy.HourlySnapshotIntervalBlocks = policy.EpochLengthBlocks
	}
	if policy.SnapshotKeepLast == 0 && policy.Profile != storageProfileArchive {
		policy.SnapshotKeepLast = 1
	}
	if strings.TrimSpace(policy.ColdExportCompression) == "" {
		policy.ColdExportCompression = "zstd"
	}
	if policy.ParallelGCWorkers == 0 {
		policy.ParallelGCWorkers = 1
	}
	if strings.TrimSpace(policy.StateLayoutMode) == "" {
		policy.StateLayoutMode = "merkle"
	}
	if policy.Profile == storageProfileArchive {
		policy.PruningEnabled = false
	}
	return policy
}

// RunOnce runs once.
func (m *StorageManager) RunOnce(reason string) (report StorageManagerReport, err error) {
	// `started` stores the value produced by this operation.
	started := time.Now()
	defer func() {
		if m != nil && m.Node != nil {
			m.Node.observeStorageManagerRun(report, time.Since(started), err == nil)
		}
	}()
	if m == nil || m.Node == nil {
		return report, errors.New("storage_manager_unavailable")
	}
	// `n` stores the value produced by this operation.
	n := m.Node
	// `policy` stores the value produced by this operation.
	policy := m.normalizePolicy()
	report.Profile = policy.Profile
	report.PruningEnabled = policy.PruningEnabled
	report.ParallelGCWorkers = policy.ParallelGCWorkers
	report.StateRentEnabled = policy.StateRentEnabled
	report.StateLayoutMode = policy.StateLayoutMode
	// `finalized` stores the value produced by this operation.
	finalized := n.getFinalizedHeight()
	if finalized == 0 && n.Blockchain != nil {
		finalized = n.Blockchain.FinalizedHeight()
	}
	if finalized == 0 && n.Blockchain != nil {
		finalized = n.Blockchain.Height()
	}
	report.FinalizedHeight = finalized
	if finalized == 0 {
		return report, nil
	}
	if policy.Profile == storageProfileArchive || n.statePruningArchiveMode() {
		report.ArchiveModeSkipped = true
		return report, nil
	}
	if !policy.PruningEnabled {
		report.PruningDisabledSkipped = true
		// `checkpoint` and `err` store the error produced by this operation.
		if checkpoint, err := n.persistStateCheckpoint(finalized); err != nil {
			return report, err
		} else {
			report.StateCheckpointHeight = checkpoint.Height
		}
		return report, nil
	}
	// `retainFrom` stores the value produced by this operation.
	retainFrom := storageRetainFromHeight(finalized, policy)
	report.RetainFromHeight = retainFrom
	report.HotWindowBlocks = storageHotWindowBlocks(finalized, retainFrom)

	// `checkpoint` and `err` store the error produced by this operation.
	if checkpoint, err := n.persistStateCheckpoint(finalized); err != nil {
		return report, err
	} else {
		report.StateCheckpointHeight = checkpoint.Height
	}
	m.runAutomaticBackupBestEffort("storage_manager:" + strings.TrimSpace(reason))
	// `pruned` and `err` store the error produced by this operation.
	if pruned, err := n.compactSnapshotsByStoragePolicy(policy, finalized); err != nil {
		return report, err
	} else {
		report.SnapshotsPruned = pruned
	}
	// `err` stores the error produced by this operation.
	if err := n.PruneValidatorRegistrySnapshots(maxUint64Value(finalized-retainFrom+1, 1)); err != nil {
		return report, err
	}
	report.RegistryPruned = true
	if policy.ParallelGCWorkers > 1 {
		type gcResult struct {
			// `executionPruned` stores the value associated with this record.
			executionPruned int
			// `blockPruned` stores the block data handled by this operation.
			blockPruned     int
			// `coldExported` stores the value associated with this record.
			coldExported    int
			// `err` stores the error produced by this operation.
			err             error
		}
		// `results` stores the result produced by this operation.
		results := make(chan gcResult, 2)
		go func() {
			results <- gcResult{executionPruned: n.pruneExecutionSnapshotCacheBefore(retainFrom)}
		}()
		go func() {
			// `pruned`, `exported`, and `err` store the error produced by this operation.
			pruned, exported, err := n.pruneBlockFilesBefore(retainFrom, policy)
			results <- gcResult{blockPruned: pruned, coldExported: exported, err: err}
		}()
		// `i` stores the current position in the related collection.
		for i := 0; i < 2; i++ {
			// `result` stores the result produced by this operation.
			result := <-results
			if result.err != nil {
				return report, result.err
			}
			report.ExecutionCachePruned += result.executionPruned
			report.BlockFilesPruned += result.blockPruned
			report.ColdStorageExported += result.coldExported
		}
	} else {
		report.ExecutionCachePruned = n.pruneExecutionSnapshotCacheBefore(retainFrom)
		// `pruned`, `exported`, and `err` store the error produced by this operation.
		if pruned, exported, err := n.pruneBlockFilesBefore(retainFrom, policy); err != nil {
			return report, err
		} else {
			report.BlockFilesPruned = pruned
			report.ColdStorageExported = exported
		}
	}
	// `err` stores the error produced by this operation.
	if err := n.recordStorageManagerMarker(policy, report, strings.TrimSpace(reason)); err != nil {
		return report, err
	}
	return report, nil
}

// storageRetainFromHeight implements the storage retain from height helper.
func storageRetainFromHeight(finalized uint64, policy StoragePolicy) uint64 {
	// `window` stores the value produced by this operation.
	window := policy.RollbackWindowBlocks
	// `epochWindow` stores the value produced by this operation.
	if epochWindow := policy.RetainedEpochs * policy.EpochLengthBlocks; epochWindow > window {
		window = epochWindow
	}
	if window == 0 {
		return finalized
	}
	if finalized <= window {
		return 1
	}
	return finalized - window + 1
}

// storageHotWindowBlocks implements the storage hot window blocks helper.
func storageHotWindowBlocks(finalized uint64, retainFrom uint64) uint64 {
	if finalized == 0 || retainFrom == 0 || retainFrom > finalized {
		return 0
	}
	return finalized - retainFrom + 1
}

// stateCheckpointDBKey implements the state checkpoint db key helper.
func stateCheckpointDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", stateCheckpointDBPrefix, height))
}

// stateCheckpointFilePath implements the state checkpoint file path helper.
func stateCheckpointFilePath(dataDir, nodeID string, height uint64) string {
	return filepath.Join(nodeDataPath(dataDir, nodeID), "state_checkpoints", fmt.Sprintf("checkpoint_%020d.json", height))
}

// stateCheckpointFromBlock implements the state checkpoint from block helper.
func stateCheckpointFromBlock(block Block) StateCheckpoint {
	// `certHash` stores the digest used to identify or verify the related data.
	certHash := ""
	if block.FinalityCertificate != nil {
		// `raw` and `err` store the error produced by this operation.
		if raw, err := json.Marshal(block.FinalityCertificate); err == nil {
			certHash = HashStrings([]string{string(raw)})
		}
	}
	return StateCheckpoint{
		Version:                   stateCheckpointVersion,
		Domain:                    stateCheckpointDomain,
		Height:                    block.ID,
		StateRoot:                 strings.TrimSpace(block.StateRoot),
		FinalityRoot:              strings.TrimSpace(block.FinalityRoot),
		EpochAnchorHash:           strings.TrimSpace(block.EpochAnchorHash),
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
		BlockHash:                 strings.TrimSpace(block.BlockHash),
		FinalityCertificateHash:   certHash,
		CreatedAtUnix:             time.Now().Unix(),
	}
}

// persistStateCheckpoint implements the persist state checkpoint helper.
func (n *Node) persistStateCheckpoint(height uint64) (StateCheckpoint, error) {
	if n == nil || height == 0 || n.Blockchain == nil {
		return StateCheckpoint{}, nil
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.Blockchain.GetBlock(height)
	if !ok {
		return StateCheckpoint{}, nil
	}
	if block.ID == 0 {
		block.ID = height
	}
	// `checkpoint` stores the value produced by this operation.
	checkpoint := stateCheckpointFromBlock(block)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return StateCheckpoint{}, err
	}
	if n.DB != nil && n.DB.Meta != nil {
		// `err` stores the error produced by this operation.
		if err := n.DB.Meta.Update(func(txn *Txn) error {
			return txn.Set(stateCheckpointDBKey(height), raw)
		}); err != nil {
			return StateCheckpoint{}, err
		}
	}
	// `err` stores the error produced by this operation.
	if err := writeFinalityArtifactJSON(stateCheckpointFilePath(n.DataDir, n.ID, height), checkpoint); err != nil {
		return StateCheckpoint{}, err
	}
	return checkpoint, nil
}

// recordStorageManagerMarker implements the record storage manager marker helper.
func (n *Node) recordStorageManagerMarker(policy StoragePolicy, report StorageManagerReport, reason string) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || report.RetainFromHeight == 0 {
		return nil
	}
	return n.DB.Meta.Update(func(txn *Txn) error {
		// `marker` stores the value produced by this operation.
		marker := StatePruneMarker{}
		// `item` and `err` store the error produced by this operation.
		if item, err := txn.Get(statePruneMarkerKey()); err == nil && item != nil {
			_ = item.Value(func(val []byte) error {
				_ = json.Unmarshal(val, &marker)
				return nil
			})
		}
		if marker.Version == 0 {
			marker.Version = statePruneMarkerVersion
		}
		marker.Mode = normalizeSyncHistoryMode(SyncHistoryMode)
		marker.Profile = strings.TrimSpace(policy.Profile)
		marker.PruningEnabled = policy.PruningEnabled
		marker.FinalizedHeight = maxUint64Value(marker.FinalizedHeight, report.FinalizedHeight)
		marker.RetainFromHeight = maxUint64Value(marker.RetainFromHeight, report.RetainFromHeight)
		marker.HotWindowBlocks = storageHotWindowBlocks(marker.FinalizedHeight, marker.RetainFromHeight)
		if report.RetainFromHeight > 1 {
			marker.PrunedThroughHeight = maxUint64Value(marker.PrunedThroughHeight, report.RetainFromHeight-1)
		}
		marker.SnapshotRetention = policy.SnapshotKeepLast
		marker.BlockPrunedThrough = maxUint64Value(marker.BlockPrunedThrough, blockPrunedThroughFromReport(report))
		marker.LastCheckpointHeight = maxUint64Value(marker.LastCheckpointHeight, report.StateCheckpointHeight)
		if report.ColdStorageExported > 0 {
			marker.ColdExportedThrough = maxUint64Value(marker.ColdExportedThrough, blockPrunedThroughFromReport(report))
		}
		marker.SnapshotCompacted = marker.SnapshotCompacted || report.SnapshotsPruned > 0
		marker.SnapshotMetaCompacted = marker.SnapshotMetaCompacted || report.SnapshotsPruned > 0
		marker.SnapshotDeltaCompacted = marker.SnapshotDeltaCompacted || report.SnapshotsPruned > 0
		marker.RegistryCompacted = marker.RegistryCompacted || report.RegistryPruned
		marker.ExecutionCacheGC = marker.ExecutionCacheGC || report.ExecutionCachePruned > 0
		marker.BlockStoreCompacted = marker.BlockStoreCompacted || report.BlockFilesPruned > 0
		marker.ColdStorageExported = marker.ColdStorageExported || report.ColdStorageExported > 0
		marker.ParallelGCWorkers = policy.ParallelGCWorkers
		marker.StateRentEnabled = policy.StateRentEnabled
		marker.StateRentArchiveEpochs = policy.StateRentArchiveEpochs
		marker.StateLayoutMode = strings.TrimSpace(policy.StateLayoutMode)
		marker.UpdatedAtUnix = time.Now().Unix()
		_ = reason
		// `raw` and `err` store the error produced by this operation.
		raw, err := json.Marshal(marker)
		if err != nil {
			return err
		}
		return txn.Set(statePruneMarkerKey(), raw)
	})
}

// blockPrunedThroughFromReport implements the block pruned through from report helper.
func blockPrunedThroughFromReport(report StorageManagerReport) uint64 {
	if report.RetainFromHeight <= 1 {
		return 0
	}
	return report.RetainFromHeight - 1
}

// listStoredSnapshotHeights implements the list stored snapshot heights helper.
func (n *Node) listStoredSnapshotHeights() ([]uint64, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, nil
	}
	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	// `heights` stores the value produced by this operation.
	heights := make([]uint64, 0, len(keys))
	// `key` tracks the key used to access the related value.
	for _, key := range keys {
		heights = append(heights, key.height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	return heights, err
}

// compactSnapshotsByStoragePolicy implements the compact snapshots by storage policy helper.
func (n *Node) compactSnapshotsByStoragePolicy(policy StoragePolicy, finalized uint64) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	// `heights` and `err` store the error produced by this operation.
	heights, err := n.listStoredSnapshotHeights()
	if err != nil || len(heights) == 0 {
		return 0, err
	}
	// `protect` stores the value produced by this operation.
	protect := storageSnapshotProtectedHeights(heights, finalized, policy)
	// `removed` stores the value produced by this operation.
	removed := 0
	// `height` tracks the current values while iterating.
	for _, height := range heights {
		if protect[height] || n.shouldProtectCommittedSnapshotHeight(height, finalized) {
			continue
		}
		// `err` stores the error produced by this operation.
		if err := n.deleteStoredSnapshotHeight(height); err != nil {
			return removed, err
		}
		removed++
	}
	if removed > 0 {
		// `err` stores the error produced by this operation.
		if err := n.refreshLatestSnapshotPointer(); err != nil {
			return removed, err
		}
		// `retainFrom` stores the value produced by this operation.
		if retainFrom := storageRetainFromHeight(finalized, policy); retainFrom > 0 {
			// `err` stores the error produced by this operation.
			if err := n.pruneSnapshotMetaBelowHeight(retainFrom); err != nil {
				return removed, err
			}
			// `err` stores the error produced by this operation.
			if err := n.pruneSnapshotDeltasBelowHeight(retainFrom); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

// storageSnapshotProtectedHeights implements the storage snapshot protected heights helper.
func storageSnapshotProtectedHeights(heights []uint64, finalized uint64, policy StoragePolicy) map[uint64]bool {
	// `protect` stores the value produced by this operation.
	protect := make(map[uint64]bool)
	if len(heights) == 0 {
		return protect
	}
	// `sorted` stores the value produced by this operation.
	sorted := append([]uint64{}, heights...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	// `i` stores the current position in the related collection.
	for i := len(sorted) - 1; i >= 0 && uint64(len(protect)) < policy.SnapshotKeepLast; i-- {
		protect[sorted[i]] = true
	}
	if finalized > 0 {
		protect[finalized] = true
	}
	// `addTier` stores the value produced by this operation.
	addTier := func(interval uint64, retain uint64) {
		if interval == 0 || retain == 0 {
			return
		}
		// `seen` stores the value produced by this operation.
		seen := uint64(0)
		// `lastBucket` stores the value produced by this operation.
		lastBucket := uint64(^uint64(0))
		// `i` stores the current position in the related collection.
		for i := len(sorted) - 1; i >= 0 && seen < retain; i-- {
			// `h` stores the value produced by this operation.
			h := sorted[i]
			// `bucket` stores the value produced by this operation.
			bucket := h / interval
			if bucket == lastBucket {
				continue
			}
			lastBucket = bucket
			protect[h] = true
			seen++
		}
	}
	// `hourly` stores the value produced by this operation.
	hourly := policy.HourlySnapshotIntervalBlocks
	addTier(hourly, policy.HourlySnapshotRetain)
	addTier(hourly*24, policy.DailySnapshotRetain)
	addTier(hourly*24*7, policy.WeeklySnapshotRetain)
	addTier(hourly*24*30, policy.MonthlySnapshotRetain)
	return protect
}

// pruneExecutionSnapshotCacheBefore implements the prune execution snapshot cache before helper.
func (n *Node) pruneExecutionSnapshotCacheBefore(retainFrom uint64) int {
	if n == nil || retainFrom == 0 {
		return 0
	}
	n.snapshotExecutionLedgerMu.Lock()
	defer n.snapshotExecutionLedgerMu.Unlock()
	// `removed` stores the value produced by this operation.
	removed := 0
	// `height` tracks the current values while iterating.
	for height := range n.snapshotExecutionLedgerByHeight {
		if height < retainFrom {
			delete(n.snapshotExecutionLedgerByHeight, height)
			removed++
		}
	}
	return removed
}

// pruneBlockFilesBefore implements the prune block files before helper.
func (n *Node) pruneBlockFilesBefore(retainFrom uint64, policy StoragePolicy) (int, int, error) {
	if n == nil || retainFrom <= 1 {
		return 0, 0, nil
	}
	// `dir` stores the value produced by this operation.
	dir := blockStoreDir(n.DataDir, n.ID)
	// `entries` and `err` store the error produced by this operation.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	// `pruned` and `exported` store the value produced by this operation.
	pruned, exported := 0, 0
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// `height` and `ok` store whether the related condition is satisfied.
		height, ok := blockHeightFromFileName(entry.Name())
		if !ok || height >= retainFrom {
			continue
		}
		// `path` stores the value produced by this operation.
		path := filepath.Join(dir, entry.Name())
		if policy.ColdExportEnabled {
			// `err` stores the error produced by this operation.
			if err := n.exportColdBlockFile(path, height, policy); err != nil {
				return pruned, exported, err
			}
			exported++
		}
		// `err` stores the error produced by this operation.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return pruned, exported, err
		}
		pruned++
	}
	return pruned, exported, nil
}

// blockHeightFromFileName implements the block height from file name helper.
func blockHeightFromFileName(name string) (uint64, bool) {
	if !strings.HasPrefix(name, "block_") || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	// `height` and `err` store the error produced by this operation.
	height, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, "block_"), ".json"), 10, 64)
	return height, err == nil && height > 0
}

// exportColdBlockFile implements the export cold block file helper.
func (n *Node) exportColdBlockFile(path string, height uint64, policy StoragePolicy) error {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// `dir` stores the value produced by this operation.
	dir := coldBlockStoreDir(n.DataDir, n.ID)
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(policy.ColdExportCompression), "zstd") {
		return writeFileAtomic(coldBlockStoreRawFilePath(n.DataDir, n.ID, height), raw, 0o600)
	}
	// `encoder` and `err` store the error produced by this operation.
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return err
	}
	// `compressed` stores the value produced by this operation.
	compressed := encoder.EncodeAll(raw, nil)
	return writeFileAtomic(coldBlockStoreZstdFilePath(n.DataDir, n.ID, height), compressed, 0o600)
}

// loadStateCheckpoint implements the load state checkpoint helper.
func (n *Node) loadStateCheckpoint(height uint64) (StateCheckpoint, bool, error) {
	// `checkpoint` stores the value used by this operation.
	var checkpoint StateCheckpoint
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return checkpoint, false, nil
	}
	// `found` stores whether the related condition is satisfied.
	found := false
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(stateCheckpointDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &checkpoint)
		})
	})
	return checkpoint, found, err
}

// encodeUint64Meta implements the encode uint64 meta helper.
func encodeUint64Meta(value uint64) []byte {
	// `raw` stores the value used by this operation.
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	return raw[:]
}
