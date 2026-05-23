package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const statePruneMarkerVersion = 1

func statePruneMarkerKey() []byte {
	return []byte("state_prune_marker")
}

// StatePruneMarker is the durable boundary between hot state and archive
// history. Heights at or below PrunedThroughHeight were intentionally compacted.
type StatePruneMarker struct {
	Version                int    `json:"version"`
	Mode                   string `json:"mode"`
	Profile                string `json:"profile,omitempty"`
	FinalizedHeight        uint64 `json:"finalized_height"`
	RetainFromHeight       uint64 `json:"retain_from_height"`
	PrunedThroughHeight    uint64 `json:"pruned_through_height"`
	SnapshotRetention      uint64 `json:"snapshot_retention"`
	BlockPrunedThrough     uint64 `json:"block_pruned_through,omitempty"`
	LastCheckpointHeight   uint64 `json:"last_checkpoint_height,omitempty"`
	ColdExportedThrough    uint64 `json:"cold_exported_through,omitempty"`
	SnapshotCompacted      bool   `json:"snapshot_compacted"`
	RegistryCompacted      bool   `json:"registry_compacted"`
	SnapshotMetaCompacted  bool   `json:"snapshot_meta_compacted"`
	SnapshotDeltaCompacted bool   `json:"snapshot_delta_compacted"`
	ExecutionCacheGC       bool   `json:"execution_cache_gc,omitempty"`
	BlockStoreCompacted    bool   `json:"block_store_compacted,omitempty"`
	ColdStorageExported    bool   `json:"cold_storage_exported,omitempty"`
	ParallelGCWorkers      uint64 `json:"parallel_gc_workers,omitempty"`
	StateRentEnabled       bool   `json:"state_rent_enabled,omitempty"`
	StateRentArchiveEpochs uint64 `json:"state_rent_archive_inactive_after_epochs,omitempty"`
	StateLayoutMode        string `json:"state_layout_mode,omitempty"`
	UpdatedAtUnix          int64  `json:"updated_at_unix"`
}

func maxUint64Value(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func (n *Node) statePruningArchiveMode() bool {
	return normalizeSyncHistoryMode(SyncHistoryMode) == SyncHistoryModeArchive
}

func (n *Node) loadStatePruneMarker() (StatePruneMarker, bool) {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return StatePruneMarker{}, false
	}
	var marker StatePruneMarker
	err := n.DB.Meta.View(func(txn *Txn) error {
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

func (n *Node) recordStatePruneMarker(kind string, finalizedHeight uint64, retainFromHeight uint64, keepLast uint64) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || retainFromHeight == 0 {
		return nil
	}
	if n.statePruningArchiveMode() {
		return nil
	}
	prunedThrough := retainFromHeight - 1
	kind = strings.ToLower(strings.TrimSpace(kind))
	return n.DB.Meta.Update(func(txn *Txn) error {
		marker := StatePruneMarker{}
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
		policy := defaultStoragePolicyForNode(n)
		marker.Profile = strings.TrimSpace(policy.Profile)
		marker.FinalizedHeight = maxUint64Value(marker.FinalizedHeight, finalizedHeight)
		marker.RetainFromHeight = maxUint64Value(marker.RetainFromHeight, retainFromHeight)
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
		raw, err := json.Marshal(marker)
		if err != nil {
			return fmt.Errorf("marshal_state_prune_marker: %w", err)
		}
		return txn.Set(statePruneMarkerKey(), raw)
	})
}

func (n *Node) stateHistoryPrunedForHeight(height uint64) bool {
	if height == 0 {
		return false
	}
	marker, ok := n.loadStatePruneMarker()
	if !ok {
		return false
	}
	return marker.PrunedThroughHeight > 0 && height <= marker.PrunedThroughHeight
}
