package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// `statePruneMarkerVersion` defines the constant value used by this package.
const statePruneMarkerVersion = 1

// statePruneMarkerKey implements the state prune marker key helper.
func statePruneMarkerKey() []byte {
	return []byte("state_prune_marker")
}

// StatePruneMarker is the durable boundary between hot state and archive
// history. Heights at or below PrunedThroughHeight were intentionally compacted.
type StatePruneMarker struct {
	// `Version` stores the value associated with this record.
	Version                int    `json:"version"`
	// `Mode` stores the value associated with this record.
	Mode                   string `json:"mode"`
	// `Profile` stores the value associated with this record.
	Profile                string `json:"profile,omitempty"`
	// `PruningEnabled` stores whether the related condition is satisfied.
	PruningEnabled         bool   `json:"pruning_enabled"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight        uint64 `json:"finalized_height"`
	// `RetainFromHeight` stores the value associated with this record.
	RetainFromHeight       uint64 `json:"retain_from_height"`
	// `HotWindowBlocks` stores the value associated with this record.
	HotWindowBlocks        uint64 `json:"hot_window_blocks,omitempty"`
	// `PrunedThroughHeight` stores the value associated with this record.
	PrunedThroughHeight    uint64 `json:"pruned_through_height"`
	// `SnapshotRetention` stores the value associated with this record.
	SnapshotRetention      uint64 `json:"snapshot_retention"`
	// `BlockPrunedThrough` stores the block data handled by this operation.
	BlockPrunedThrough     uint64 `json:"block_pruned_through,omitempty"`
	// `LastCheckpointHeight` stores the value associated with this record.
	LastCheckpointHeight   uint64 `json:"last_checkpoint_height,omitempty"`
	// `ColdExportedThrough` stores the value associated with this record.
	ColdExportedThrough    uint64 `json:"cold_exported_through,omitempty"`
	// `SnapshotCompacted` stores the value associated with this record.
	SnapshotCompacted      bool   `json:"snapshot_compacted"`
	// `RegistryCompacted` stores the value associated with this record.
	RegistryCompacted      bool   `json:"registry_compacted"`
	// `SnapshotMetaCompacted` stores the value associated with this record.
	SnapshotMetaCompacted  bool   `json:"snapshot_meta_compacted"`
	// `SnapshotDeltaCompacted` stores the value associated with this record.
	SnapshotDeltaCompacted bool   `json:"snapshot_delta_compacted"`
	// `ExecutionCacheGC` stores the value associated with this record.
	ExecutionCacheGC       bool   `json:"execution_cache_gc,omitempty"`
	// `BlockStoreCompacted` stores the block data handled by this operation.
	BlockStoreCompacted    bool   `json:"block_store_compacted,omitempty"`
	// `ColdStorageExported` stores the value associated with this record.
	ColdStorageExported    bool   `json:"cold_storage_exported,omitempty"`
	// `ParallelGCWorkers` stores the value associated with this record.
	ParallelGCWorkers      uint64 `json:"parallel_gc_workers,omitempty"`
	// `StateRentEnabled` stores whether the related condition is satisfied.
	StateRentEnabled       bool   `json:"state_rent_enabled,omitempty"`
	// `StateRentArchiveEpochs` stores the value associated with this record.
	StateRentArchiveEpochs uint64 `json:"state_rent_archive_inactive_after_epochs,omitempty"`
	// `StateLayoutMode` stores the value associated with this record.
	StateLayoutMode        string `json:"state_layout_mode,omitempty"`
	// `UpdatedAtUnix` stores the value associated with this record.
	UpdatedAtUnix          int64  `json:"updated_at_unix"`
}

// maxUint64Value returns the maximum uint64 value.
func maxUint64Value(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// statePruningArchiveMode implements the state pruning archive mode helper.
func (n *Node) statePruningArchiveMode() bool {
	return normalizeSyncHistoryMode(SyncHistoryMode) == SyncHistoryModeArchive
}

// loadStatePruneMarker implements the load state prune marker helper.
func (n *Node) loadStatePruneMarker() (StatePruneMarker, bool) {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return StatePruneMarker{}, false
	}
	// `marker` stores the value used by this operation.
	var marker StatePruneMarker
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(statePruneMarkerKey())
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			if len(val) == 0 {
				return errors.New("empty_state_prune_marker")
			}
			return json.Unmarshal(val, &marker)
		})
	})
	if err != nil || marker.PrunedThroughHeight == 0 {
		return StatePruneMarker{}, false
	}
	if marker.Version == 0 {
		marker.Version = statePruneMarkerVersion
	}
	return marker, true
}

// recordStatePruneMarker implements the record state prune marker helper.
func (n *Node) recordStatePruneMarker(kind string, finalizedHeight uint64, retainFromHeight uint64, keepLast uint64) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || retainFromHeight == 0 {
		return nil
	}
	if n.statePruningArchiveMode() {
		return nil
	}
	// `prunedThrough` stores the value produced by this operation.
	prunedThrough := retainFromHeight - 1
	kind = strings.ToLower(strings.TrimSpace(kind))
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
		// `policy` stores the value produced by this operation.
		policy := defaultStoragePolicyForNode(n)
		marker.Profile = strings.TrimSpace(policy.Profile)
		marker.PruningEnabled = policy.PruningEnabled
		marker.FinalizedHeight = maxUint64Value(marker.FinalizedHeight, finalizedHeight)
		marker.RetainFromHeight = maxUint64Value(marker.RetainFromHeight, retainFromHeight)
		marker.HotWindowBlocks = storageHotWindowBlocks(marker.FinalizedHeight, marker.RetainFromHeight)
		marker.PrunedThroughHeight = maxUint64Value(marker.PrunedThroughHeight, prunedThrough)
		marker.SnapshotRetention = keepLast
		marker.ParallelGCWorkers = maxUint64Value(marker.ParallelGCWorkers, policy.ParallelGCWorkers)
		marker.StateRentEnabled = marker.StateRentEnabled || policy.StateRentEnabled
		marker.StateRentArchiveEpochs = maxUint64Value(marker.StateRentArchiveEpochs, policy.StateRentArchiveEpochs)
		marker.StateLayoutMode = strings.TrimSpace(policy.StateLayoutMode)
		marker.UpdatedAtUnix = time.Now().Unix()
		switch kind {
		case "snapshot":
			marker.SnapshotCompacted = true
			marker.SnapshotMetaCompacted = true
			marker.SnapshotDeltaCompacted = true
		case "registry":
			marker.RegistryCompacted = true
		case "execution_cache":
			marker.ExecutionCacheGC = true
		}
		// `raw` and `err` store the error produced by this operation.
		raw, err := json.Marshal(marker)
		if err != nil {
			return fmt.Errorf("marshal_state_prune_marker: %w", err)
		}
		return txn.Set(statePruneMarkerKey(), raw)
	})
}

// stateHistoryPrunedForHeight implements the state history pruned for height helper.
func (n *Node) stateHistoryPrunedForHeight(height uint64) bool {
	if height == 0 {
		return false
	}
	// `marker` and `ok` store whether the related condition is satisfied.
	marker, ok := n.loadStatePruneMarker()
	if !ok {
		return false
	}
	return marker.PrunedThroughHeight > 0 && height <= marker.PrunedThroughHeight
}
