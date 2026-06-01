package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHandleMetricsExposesCoreMetrics(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldAPIToken := apiToken
	defer func() {
		ConfigAuthRequireWallet = oldRequireWallet
		apiToken = oldAPIToken
	}()

	ConfigAuthRequireWallet = false
	apiToken = ""

	s := NewServer(&Node{
		ID:   "TEST",
		Role: "validator",
		Blockchain: &Blockchain{
			Blocks: []Block{},
		},
		Ledger: Ledger{
			Balances:               map[string]int{},
			Nonces:                 map[string]int{},
			Stakes:                 map[string]StakeLock{},
			ValidatorRewardWallets: map[string]string{},
		},
	})
	s.Node.syncMu.Lock()
	s.Node.syncSnapshotHeight = 77
	s.Node.syncSnapshotHash = "snap-77"
	s.Node.syncDownloadedChunks = 5
	s.Node.syncTotalChunks = 8
	s.Node.syncChunkProviders = []string{"p1", "p2"}
	s.Node.syncVerifyStage = "download"
	s.Node.syncResumeState = "restored"
	s.Node.syncMu.Unlock()
	s.Node.observeSnapshotOperation("create", 77, 12*time.Millisecond, true)
	s.Node.observeSnapshotOperation("load", 77, 3*time.Millisecond, true)
	s.Node.observeSnapshotOperation("apply", 77, 8*time.Millisecond, true)
	s.Node.observeReplayOperation(77, 4, 21*time.Millisecond, true)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, name := range []string{
		"msc_node_height",
		"msc_node_finalized_height",
		"msc_node_peers",
		"msc_block_height",
		"msc_finalized_height",
		"msc_finality_gap",
		"msc_blocks_produced_total",
		"msc_blocks_verified_total",
		"msc_node_ready",
		"msc_node_live_validators",
		"msc_node_snapshot_height",
		"msc_node_snapshot_downloaded_chunks",
		"msc_node_snapshot_total_chunks",
		"msc_node_snapshot_providers",
		"msc_consensus_lag_blocks",
		"msc_consensus_lag_seconds",
		"msc_network_best_height",
		"msc_local_height",
		"msc_consensus_finality_lag_blocks",
		"msc_consensus_last_block_age_seconds",
		"msc_consensus_mode",
		"msc_consensus_detector_mode",
		"msc_consensus_detector_finality_lag_blocks",
		"msc_consensus_detector_last_finality_seconds",
		"msc_consensus_detector_partition_risk",
		"msc_consensus_detector_attack_signal",
		"msc_consensus_block_production_status",
		"msc_consensus_tx_lane_status",
		"msc_quorum_required",
		"msc_quorum_observed",
		"msc_validator_votes_received",
		"msc_quorum_failures_total",
		"msc_mempool_depth",
		"msc_mempool_capacity",
		"msc_mempool_utilization_ratio",
		"msc_mempool_queued_exec_votes",
		"msc_mempool_size",
		"msc_mempool_bytes",
		"msc_mempool_oldest_tx_age",
		"msc_mempool_rejected_total",
		"msc_mempool_rate_limited_total",
		"msc_tx_received_total",
		"msc_tx_processed_total",
		"msc_tx_confirmed_total",
		"msc_tps",
		"msc_validator_health_live",
		"msc_validator_health_offline",
		"msc_validator_health_ratio",
		"msc_validator_health_required_quorum",
		"msc_validator_online",
		"msc_validator_ready",
		"msc_validator_vote_latency_ms",
		"msc_validator_last_vote_height",
		"msc_validator_last_seen_seconds",
		"msc_validator_missed_votes",
		"msc_validator_double_sign_events",
		"msc_validator_censorship_events",
		"msc_validator_slash_count",
		"msc_snapshot_create_total",
		"msc_snapshot_load_total",
		"msc_snapshot_apply_total",
		"msc_snapshot_created_total",
		"msc_snapshot_failures_total",
		"msc_snapshot_create_last_duration_ms",
		"msc_snapshot_load_last_duration_ms",
		"msc_snapshot_apply_last_duration_ms",
		"msc_snapshot_creation_seconds",
		"msc_snapshot_apply_seconds",
		"msc_snapshot_download_seconds",
		"msc_snapshot_restore_seconds",
		"msc_snapshot_size_bytes",
		"msc_snapshot_bootstrap_total",
		"msc_snapshot_bootstrap_success_total",
		"msc_replay_operation_total",
		"msc_replay_last_duration_ms",
		"msc_replay_last_blocks",
		"msc_replay_blocks_total",
		"msc_replay_duration_seconds",
		"msc_replay_blocks_per_second",
		"msc_replay_tx_per_second",
		"msc_replay_memory_bytes",
		"msc_replay_peak_memory_bytes",
		"msc_replay_state_rebuild_total",
		"msc_replay_failures_total",
		"msc_replay_digest_match",
		"msc_replay_digest_mismatch_total",
		"msc_storage_size_bytes",
		"msc_disk_usage_percent",
		"msc_storage_disk_usage_percent",
		"msc_storage_pruned_states_total",
		"msc_storage_pruned_snapshots_total",
		"msc_storage_gc_cycles_total",
		"msc_storage_gc_duration_seconds",
		"msc_cold_exports_total",
		"msc_cold_imports_total",
		"msc_cold_storage_size_bytes",
		"msc_finalized_epoch",
		"msc_finality_certificates_total",
		"msc_epoch_anchor_total",
		"msc_irreversible_root_total",
		"msc_finalized_validator_set_commitments_total",
		"msc_finality_conflicts_total",
		"msc_long_range_attack_reject_total",
		"msc_checkpoint_conflicts_total",
		"msc_governance_proposals_total",
		"msc_governance_proposals_active",
		"msc_governance_proposals_approved",
		"msc_governance_proposals_rejected",
		"msc_governance_proposals_scheduled",
		"msc_governance_proposals_applied",
		"msc_governance_treasury_balance",
		"msc_protocol_upgrades_scheduled",
		"msc_protocol_upgrades_activated",
		"msc_protocol_gate_validator_set_activation_model_v2_height",
		"msc_protocol_gate_validator_set_commitment_v2_height",
		"msc_protocol_gate_validator_set_hash_v3_height",
		"msc_protocol_gate_sync_snapshot_checkpoint_v2_height",
		"msc_protocol_gate_dtl_v2_activation_height",
		"msc_sync_mode",
		"msc_sync_target_height",
		"msc_sync_lag_blocks",
		"msc_sync_duration_seconds",
		"msc_sync_mode_switch_total",
		"msc_peers_connected",
		"msc_peers_trusted",
		"msc_peers_quarantined",
		"msc_peer_disconnect_total",
		"msc_peer_disconnect_rate_per_minute",
		"msc_peer_reputation_average",
		"msc_peer_latency_ms",
		"msc_peer_latency_max_ms",
		"msc_peer_subnet_buckets",
		"msc_peer_asn_buckets",
		"msc_peer_outbound_subnet_buckets",
		"msc_peer_outbound_asn_buckets",
		"msc_peer_diversity_rejections_total",
		"msc_peer_outbound_diversity_rejections_total",
		"msc_validator_country_buckets",
		"msc_validator_asn_buckets",
		"msc_validator_cloud_buckets",
		"msc_validator_region_buckets",
		"msc_validator_diversity_missing_metadata",
		"msc_validator_diversity_violations",
		"msc_validator_diversity_healthy",
		"msc_validator_diversity_max_country_pct",
		"msc_validator_diversity_max_asn_pct",
		"msc_validator_diversity_max_cloud_pct",
		"msc_peer_resource_drops_total",
		"msc_peer_connection_flood_total",
		"msc_peer_count_drop",
		"msc_partition_height_divergence_blocks",
		"msc_partition_majority_peer_loss",
		"msc_partition_risk",
		"msc_rate_limit_drops_total",
		"msc_invalid_messages_total",
		"msc_bad_snapshot_proofs_total",
		"msc_block_propagation_seconds",
		"msc_block_propagation_max_seconds",
		"msc_block_propagation_total",
		"msc_rpc_requests_total",
		"msc_rpc_rate_limited_total",
		"msc_rpc_body_rejected_total",
		"msc_rpc_concurrent_rejected_total",
		"msc_rpc_unauthorized_total",
		"msc_rpc_inflight",
		"msc_rpc_max_request_body_bytes",
		"msc_rpc_read_rate_limit_per_minute",
		"msc_rpc_write_rate_limit_per_minute",
		"msc_rpc_max_concurrent_requests",
		"msc_supply_current",
		"msc_tokenomics_work_block_reward_enabled",
		"msc_validator_signer_mode_code",
		"msc_validator_signer_ready",
		"msc_validator_signer_external_signer_configured",
		"msc_validator_signer_threshold",
		"msc_validator_signer_participants",
		"msc_validator_hsm_enabled",
		"msc_validator_hsm_ready",
		"msc_validator_hsm_external_signer_configured",
		"msc_validator_hsm_timeout_ms",
		"msc_map_seen_blocks",
		"msc_security_validator_lifecycle_pending_total",
		"msc_security_validator_lifecycle_active_total",
		"msc_security_validator_lifecycle_inactive_total",
		"msc_security_validator_lifecycle_slashed_total",
		"msc_security_validator_lifecycle_removed_total",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("expected metric %q in output", name)
		}
	}
	for _, want := range []string{
		`msc_snapshot_create_last_duration_ms{chain_id=`,
		`msc_replay_last_blocks{chain_id=`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected metrics output to contain %q", want)
		}
	}
	helpSeen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "# HELP ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatalf("malformed HELP line: %q", line)
		}
		if helpSeen[fields[2]] {
			t.Fatalf("duplicate HELP line for metric %q", fields[2])
		}
		helpSeen[fields[2]] = true
	}
}

func TestHandleStatusExposesSnapshotSyncFields(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldAPIToken := apiToken
	defer func() {
		ConfigAuthRequireWallet = oldRequireWallet
		apiToken = oldAPIToken
	}()

	ConfigAuthRequireWallet = false
	apiToken = ""

	node := &Node{
		ID:   "TEST",
		Role: "validator",
		Blockchain: &Blockchain{
			Blocks: []Block{},
		},
		Ledger: Ledger{
			Balances:               map[string]int{},
			Nonces:                 map[string]int{},
			Stakes:                 map[string]StakeLock{},
			ValidatorRewardWallets: map[string]string{},
		},
	}
	node.syncMu.Lock()
	node.syncSnapshotHeight = 91
	node.syncSnapshotHash = "snap-91"
	node.syncDownloadedChunks = 7
	node.syncTotalChunks = 9
	node.syncChunkProviders = []string{"peer-a", "peer-b"}
	node.syncVerifyStage = "verified"
	node.syncResumeState = "restored"
	node.syncMu.Unlock()

	s := NewServer(node)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := int(payload["snapshot_height"].(float64)); got != 91 {
		t.Fatalf("unexpected snapshot_height: got=%d", got)
	}
	if got := payload["snapshot_hash"].(string); got != "snap-91" {
		t.Fatalf("unexpected snapshot_hash: got=%q", got)
	}
	if got := int(payload["snapshot_downloaded_chunks"].(float64)); got != 7 {
		t.Fatalf("unexpected snapshot_downloaded_chunks: got=%d", got)
	}
	if got := int(payload["snapshot_total_chunks"].(float64)); got != 9 {
		t.Fatalf("unexpected snapshot_total_chunks: got=%d", got)
	}
	if got := payload["snapshot_verify_stage"].(string); got != "verified" {
		t.Fatalf("unexpected snapshot_verify_stage: got=%q", got)
	}
	if got := payload["snapshot_resume_state"].(string); got != "restored" {
		t.Fatalf("unexpected snapshot_resume_state: got=%q", got)
	}
	providers, ok := payload["snapshot_providers"].([]any)
	if !ok || len(providers) != 2 {
		t.Fatalf("unexpected snapshot_providers payload: %#v", payload["snapshot_providers"])
	}
}

func TestHandleStatusLiteIncludesRuntimeHealth(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldAPIToken := apiToken
	defer func() {
		ConfigAuthRequireWallet = oldRequireWallet
		apiToken = oldAPIToken
	}()

	ConfigAuthRequireWallet = false
	apiToken = ""

	node := &Node{
		ID:   "TEST",
		Role: "validator",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 1}, {ID: 2}},
		},
		Mempool: Mempool{
			Transactions: []Transaction{},
		},
		Ledger: Ledger{
			Balances:               map[string]int{},
			Nonces:                 map[string]int{},
			Stakes:                 map[string]StakeLock{},
			ValidatorRewardWallets: map[string]string{},
		},
		lastCommitHeight: 2,
		lastCommitAt:     time.Now().Add(-2 * time.Second),
	}

	s := NewServer(node)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["block_production_status"].(string); got != "producing" {
		t.Fatalf("expected producing block status, got=%q payload=%v", got, payload)
	}
	if got := payload["tx_lane_status"].(string); got != "clear" {
		t.Fatalf("expected clear tx lane, got=%q", got)
	}
	if got := int(payload["last_commit_height"].(float64)); got != 2 {
		t.Fatalf("unexpected last_commit_height: got=%d", got)
	}
	if got := int(payload["network_best_height"].(float64)); got != 2 {
		t.Fatalf("unexpected network_best_height: got=%d", got)
	}
	if got := int(payload["network_lag_blocks"].(float64)); got != 0 {
		t.Fatalf("unexpected network_lag_blocks: got=%d", got)
	}
	if got := payload["network_health_summary"].(string); strings.Contains(got, "unknown") || !strings.Contains(got, "blocks=producing") {
		t.Fatalf("unexpected network health summary: %q", got)
	}
}

func TestRuntimeObservableQuorumPolicyIgnoresRecentDegradedBelowStrict(t *testing.T) {
	strict := quorumPolicySnapshot{
		Mode:             "NORMAL",
		Version:          quorumPolicyVersionV1,
		Total:            4,
		StrictRequired:   3,
		Required:         3,
		ActiveReadyCount: 2,
	}
	node := &Node{
		Blockchain: &Blockchain{Blocks: []Block{
			{
				ID:                  44,
				ConsensusMode:       "DEGRADED",
				QuorumPolicyVersion: quorumPolicyVersionV1,
				ActiveReadyCount:    2,
				RequiredQuorum:      2,
				StrictQuorum:        3,
			},
		}},
		lastCommitAt: time.Now(),
	}

	got := node.runtimeObservableQuorumPolicy(45, strict)
	if got.Mode != "NORMAL" || got.Required != 3 || got.Relaxed {
		t.Fatalf("expected recent degraded policy below strict to be ignored, got=%+v", got)
	}
	if got.StrictRequired != 3 {
		t.Fatalf("unexpected strict quorum: got=%d want=3", got.StrictRequired)
	}

	node.lastCommitAt = time.Now().Add(-blockProductionStaleThreshold() - time.Second)
	got = node.runtimeObservableQuorumPolicy(45, strict)
	if got.Mode != "NORMAL" || got.Required != 3 || got.Relaxed {
		t.Fatalf("expected stale degraded policy not to override strict status, got=%+v", got)
	}
}

func TestHandleHealthzReturnsServiceUnavailableWhenNotReady(t *testing.T) {
	s := NewServer(&Node{
		ID:   "TEST",
		Role: "validator",
		Blockchain: &Blockchain{
			Blocks: []Block{},
		},
		Ledger: Ledger{
			Balances:               map[string]int{},
			Nonces:                 map[string]int{},
			Stakes:                 map[string]StakeLock{},
			ValidatorRewardWallets: map[string]string{},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 when node not ready, got %d", w.Code)
	}
}

func TestValidatorSetAuditUsesDerivedCurrentValidators(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldAPIToken := apiToken
	defer func() {
		ConfigAuthRequireWallet = oldRequireWallet
		apiToken = oldAPIToken
	}()

	ConfigAuthRequireWallet = false
	apiToken = ""

	node := &Node{
		ID:                "TEST",
		Role:              "validator",
		GenesisValidators: []string{"A", "B", "C", "D"},
		Blockchain: &Blockchain{
			Blocks: []Block{},
		},
		Ledger: Ledger{
			Balances:               map[string]int{},
			Nonces:                 map[string]int{},
			Stakes:                 map[string]StakeLock{},
			ValidatorRewardWallets: map[string]string{},
		},
		pendingValidators:        map[string]uint64{},
		pendingValidatorRemovals: map[string]uint64{},
	}
	node.validatorSetMu.Lock()
	node.currentValidators = []string{"X", "Y"}
	node.validatorSetHeight = 1
	node.validatorSetMu.Unlock()

	s := NewServer(node)
	req := httptest.NewRequest(http.MethodGet, "/validatorset/audit", nil)
	w := httptest.NewRecorder()
	s.handleValidatorSetAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		CurrentValidators []string `json:"current_validators"`
		Validators        []string `json:"validators"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := []string{"A", "B", "C", "D"}
	if !reflect.DeepEqual(payload.CurrentValidators, want) {
		t.Fatalf("expected current_validators derived from consensus set, got=%v want=%v", payload.CurrentValidators, want)
	}
	if !reflect.DeepEqual(payload.Validators, want) {
		t.Fatalf("expected validators derived from consensus set, got=%v want=%v", payload.Validators, want)
	}
}
