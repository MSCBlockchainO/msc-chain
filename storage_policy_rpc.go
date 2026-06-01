package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (n *Node) storagePolicySnapshot() map[string]any {
	policy := NewStorageManager(n).normalizePolicy()
	finalized := uint64(0)
	if n != nil {
		finalized = n.getFinalizedHeight()
		if finalized == 0 && n.Blockchain != nil {
			finalized = n.Blockchain.FinalizedHeight()
		}
		if finalized == 0 && n.Blockchain != nil {
			finalized = n.Blockchain.Height()
		}
	}
	retainFrom := uint64(0)
	hotWindow := uint64(0)
	if finalized > 0 && policy.PruningEnabled && policy.Profile != storageProfileArchive {
		retainFrom = storageRetainFromHeight(finalized, policy)
		hotWindow = storageHotWindowBlocks(finalized, retainFrom)
	}
	archiveMode := policy.Profile == storageProfileArchive
	if n != nil && n.statePruningArchiveMode() {
		archiveMode = true
	}
	resp := map[string]any{
		"profile":                          policy.Profile,
		"configured_profile":               normalizeStorageHistoryProfile(StorageHistoryProfile),
		"sync_history_mode":                normalizeSyncHistoryMode(SyncHistoryMode),
		"state_pruning_enabled":            policy.PruningEnabled && !archiveMode,
		"archive_mode":                     archiveMode,
		"finalized_height":                 finalized,
		"retain_from_height":               retainFrom,
		"hot_window_blocks":                hotWindow,
		"epoch_length_blocks":              policy.EpochLengthBlocks,
		"validator_retained_epochs":        policy.RetainedEpochs,
		"validator_rollback_window_blocks": policy.RollbackWindowBlocks,
		"validator_snapshot_keep_last":     policy.SnapshotKeepLast,
		"validator_recent_block_window":    policy.RecentBlockWindow,
		"full_node_history_blocks":         StorageFullNodeHistoryBlocks,
		"hourly_snapshot_retain":           policy.HourlySnapshotRetain,
		"daily_snapshot_retain":            policy.DailySnapshotRetain,
		"weekly_snapshot_retain":           policy.WeeklySnapshotRetain,
		"monthly_snapshot_retain":          policy.MonthlySnapshotRetain,
		"cold_export_enabled":              policy.ColdExportEnabled,
		"cold_export_compression":          strings.TrimSpace(policy.ColdExportCompression),
		"parallel_gc_workers":              policy.ParallelGCWorkers,
		"state_layout_mode":                strings.TrimSpace(policy.StateLayoutMode),
		"retention_summary":                storagePolicyRetentionSummary(policy),
	}
	if n != nil {
		if marker, ok := n.loadStatePruneMarker(); ok {
			resp["prune_marker"] = marker
			resp["pruned_through_height"] = marker.PrunedThroughHeight
			resp["block_pruned_through"] = marker.BlockPrunedThrough
			resp["cold_exported_through"] = marker.ColdExportedThrough
			resp["last_checkpoint_height"] = marker.LastCheckpointHeight
		}
	}
	return resp
}

func storagePolicyRetentionSummary(policy StoragePolicy) string {
	if policy.Profile == storageProfileArchive {
		return "archive: retain full hot history; pruning disabled"
	}
	if !policy.PruningEnabled {
		return policy.Profile + ": pruning disabled; checkpoints continue"
	}
	if policy.Profile == storageProfileFull {
		return "full: retain configured recent block history; archive node required for full historical state"
	}
	return "validator: retain current/finalized state, last validator epochs, rollback window, compacted snapshots, and cold block exports"
}

func (s *Server) storagePolicySnapshotResponse() (map[string]any, int, string) {
	if s == nil || s.Node == nil {
		return nil, http.StatusServiceUnavailable, "node unavailable"
	}
	return s.Node.storagePolicySnapshot(), http.StatusOK, ""
}

func (s *Server) handleStoragePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resp, status, errMsg := s.storagePolicySnapshotResponse()
	if status != http.StatusOK {
		http.Error(w, errMsg, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) handleV1StoragePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	resp, status, errMsg := s.storagePolicySnapshotResponse()
	if status != http.StatusOK {
		writeV1Error(w, status, "", errMsg)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeV1Data(w, http.StatusOK, resp)
}
