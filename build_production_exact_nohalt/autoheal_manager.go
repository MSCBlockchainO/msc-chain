package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type AutoHealLevel string

const (
	// `AutoHealLevelHealthy` defines the constant value used by this package.
	AutoHealLevelHealthy AutoHealLevel = "healthy"
	// `AutoHealLevel1` defines the constant value used by this package.
	AutoHealLevel1 AutoHealLevel = "level_1_normal_sync"
	// `AutoHealLevel2` defines the constant value used by this package.
	AutoHealLevel2 AutoHealLevel = "level_2_snapshot_sync"
	// `AutoHealLevel3` defines the constant value used by this package.
	AutoHealLevel3 AutoHealLevel = "level_3_diagnostics"
	// `AutoHealLevel4` defines the constant value used by this package.
	AutoHealLevel4 AutoHealLevel = "level_4_automatic_repair"
)

type AutoHealAction string

const (
	// `AutoHealActionNone` defines the constant value used by this package.
	AutoHealActionNone AutoHealAction = "none"
	// `AutoHealActionNormalSync` defines the constant value used by this package.
	AutoHealActionNormalSync AutoHealAction = "normal_sync"
	// `AutoHealActionSnapshotSync` defines the constant value used by this package.
	AutoHealActionSnapshotSync AutoHealAction = "snapshot_sync"
	// `AutoHealActionDiagnostics` defines the constant value used by this package.
	AutoHealActionDiagnostics AutoHealAction = "diagnostics"
	// `AutoHealActionAutomaticRepair` defines the constant value used by this package.
	AutoHealActionAutomaticRepair AutoHealAction = "automatic_repair"
	// `AutoHealActionPeerIsolation` defines the constant value used by this package.
	AutoHealActionPeerIsolation AutoHealAction = "peer_isolation_recovery"
	// `AutoHealActionRegistryRepair` defines the constant value used by this package.
	AutoHealActionRegistryRepair AutoHealAction = "registry_repair"
	// `AutoHealActionDBRecovery` defines the constant value used by this package.
	AutoHealActionDBRecovery AutoHealAction = "db_recovery"
)

type AutoHealConfig struct {
	// `TickInterval` stores the value currently being processed.
	TickInterval time.Duration
	// `NormalSyncLagBlocks` stores the value associated with this record.
	NormalSyncLagBlocks uint64
	// `SnapshotSyncLagBlocks` stores the value associated with this record.
	SnapshotSyncLagBlocks uint64
	// `DiagnosticsStuckAfter` stores the value associated with this record.
	DiagnosticsStuckAfter time.Duration
	// `AutomaticRepairAfter` stores the value associated with this record.
	AutomaticRepairAfter time.Duration
	// `SnapshotStrictCoreMode` stores the value associated with this record.
	SnapshotStrictCoreMode bool
	// `SnapshotAllowSameHeight` stores the value associated with this record.
	SnapshotAllowSameHeight bool
	// SnapshotSeedInterval limits forced publication of the latest verified tip.
	SnapshotSeedInterval time.Duration
}

type AutoHealDecision struct {
	// `Level` stores the value associated with this record.
	Level AutoHealLevel `json:"level"`
	// `Action` stores the value associated with this record.
	Action AutoHealAction `json:"action"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason"`
	// `LocalHeight` stores the value associated with this record.
	LocalHeight uint64 `json:"local_height"`
	// `TargetHeight` stores the value associated with this record.
	TargetHeight uint64 `json:"target_height"`
	// `LagBlocks` stores the value associated with this record.
	LagBlocks uint64 `json:"lag_blocks"`
	// `StuckFor` stores the value associated with this record.
	StuckFor time.Duration `json:"stuck_for"`
	// `Peers` stores the value associated with this record.
	Peers int `json:"peers"`
	// `QuorumVotes` stores the value associated with this record.
	QuorumVotes int `json:"quorum_votes"`
	// `QuorumRequired` stores the value associated with this record.
	QuorumRequired int `json:"quorum_required"`
}

type AutoHealDiagnostics struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height"`
	// `NetworkBestHeight` stores the value associated with this record.
	NetworkBestHeight uint64 `json:"network_best_height"`
	// `NetworkLagBlocks` stores the value associated with this record.
	NetworkLagBlocks uint64 `json:"network_lag_blocks"`
	// `LastBlockAgeSeconds` stores the value associated with this record.
	LastBlockAgeSeconds uint64 `json:"last_block_age_seconds"`
	// `LastCommitHeight` stores the value associated with this record.
	LastCommitHeight uint64 `json:"last_commit_height"`
	// `Peers` stores the value associated with this record.
	Peers int `json:"peers"`
	// `NetworkQuorumVotes` stores the value associated with this record.
	NetworkQuorumVotes int `json:"network_quorum_votes"`
	// `NetworkQuorumRequired` stores the value associated with this record.
	NetworkQuorumRequired int `json:"network_quorum_required"`
	// `BlockProductionStatus` stores the block data handled by this operation.
	BlockProductionStatus string `json:"block_production_status"`
	// `NetworkHealth` stores the value associated with this record.
	NetworkHealth string `json:"network_health"`
	// `SyncStage` stores the value associated with this record.
	SyncStage string `json:"sync_stage"`
	// `SnapshotVerifyStage` stores the snapshot verification stage.
	SnapshotVerifyStage string `json:"snapshot_verify_stage"`
	// `SnapshotHeight` stores the value associated with this record.
	SnapshotHeight uint64 `json:"snapshot_height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `SyncAnchorLastError` stores the last sync anchor error.
	SyncAnchorLastError string `json:"sync_anchor_last_error"`
	// `SyncAnchorLastRejectReason` stores the last sync anchor reject reason.
	SyncAnchorLastRejectReason string `json:"sync_anchor_last_reject_reason"`
	// `ValidatorSnapshotLastError` stores the last validator snapshot publish error.
	ValidatorSnapshotLastError string `json:"validator_snapshot_last_error"`
}

type AutoHealManager struct {
	// `Node` stores the value associated with this record.
	Node *Node
	// `Config` stores the configuration used by this operation.
	Config AutoHealConfig

	// `now` stores the value associated with this record.
	now func() time.Time

	// `mu` stores the synchronization state protecting shared data.
	mu sync.Mutex
	// `lastHeight` stores the value associated with this record.
	lastHeight uint64
	// `heightObservedAt` stores the value associated with this record.
	heightObservedAt time.Time
	// `lastCommitHeight` stores the value associated with this record.
	lastCommitHeight uint64
	// `commitObservedAt` stores the value associated with this record.
	commitObservedAt time.Time
	// `repairInFlight` stores the value associated with this record.
	repairInFlight bool
	// `lastDecision` stores the value associated with this record.
	lastDecision AutoHealDecision
	// `lastDiagnostics` stores the value associated with this record.
	lastDiagnostics AutoHealDiagnostics
	// `lastExecutionError` stores the error produced by this operation.
	lastExecutionError string
	// `lastSnapshotSeedAt` stores the last successful seeder publication time.
	lastSnapshotSeedAt time.Time
	// `lastSnapshotSeedHeight` stores the last successfully seeded height.
	lastSnapshotSeedHeight uint64
}

// DefaultAutoHealConfig returns the default auto heal config.
func DefaultAutoHealConfig() AutoHealConfig {
	return AutoHealConfig{
		TickInterval:            5 * time.Second,
		NormalSyncLagBlocks:     50,
		SnapshotSyncLagBlocks:   1000,
		DiagnosticsStuckAfter:   5 * time.Minute,
		AutomaticRepairAfter:    10 * time.Minute,
		SnapshotStrictCoreMode:  true,
		SnapshotAllowSameHeight: true,
		SnapshotSeedInterval:    5 * time.Second,
	}
}

// NewAutoHealManager creates a new auto heal manager.
func NewAutoHealManager(node *Node, config AutoHealConfig) *AutoHealManager {
	config = normalizeAutoHealConfig(config)
	return &AutoHealManager{
		Node:   node,
		Config: config,
		now:    time.Now,
	}
}

// normalizeAutoHealConfig normalizes auto heal config.
func normalizeAutoHealConfig(config AutoHealConfig) AutoHealConfig {
	// `defaults` stores the value produced by this operation.
	defaults := DefaultAutoHealConfig()
	if config.TickInterval <= 0 {
		config.TickInterval = defaults.TickInterval
	}
	if config.NormalSyncLagBlocks == 0 {
		config.NormalSyncLagBlocks = defaults.NormalSyncLagBlocks
	}
	if config.SnapshotSyncLagBlocks == 0 {
		config.SnapshotSyncLagBlocks = defaults.SnapshotSyncLagBlocks
	}
	if config.DiagnosticsStuckAfter <= 0 {
		config.DiagnosticsStuckAfter = defaults.DiagnosticsStuckAfter
	}
	if config.AutomaticRepairAfter <= 0 {
		config.AutomaticRepairAfter = defaults.AutomaticRepairAfter
	}
	if config.AutomaticRepairAfter < config.DiagnosticsStuckAfter {
		config.AutomaticRepairAfter = config.DiagnosticsStuckAfter
	}
	if config.SnapshotSeedInterval <= 0 {
		config.SnapshotSeedInterval = defaults.SnapshotSeedInterval
	}
	return config
}

// Run implements the run helper.
func (m *AutoHealManager) Run(ctx context.Context) {
	if m == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// `interval` stores the value currently being processed.
	interval := m.Config.TickInterval
	if interval <= 0 {
		interval = DefaultAutoHealConfig().TickInterval
	}
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.RunOnce()
		}
	}
}

// RunOnce runs once.
func (m *AutoHealManager) RunOnce() AutoHealDecision {
	if m == nil || m.Node == nil {
		return AutoHealDecision{Level: AutoHealLevelHealthy, Action: AutoHealActionNone, Reason: "node_unavailable"}
	}
	_ = m.Node.recoverSignedCommitQuorumAtCurrentHeight("autoheal_tick")
	// `runtime` stores the value produced by this operation.
	runtime := m.Node.runtimeStatusSnapshot()
	// `decision` stores the value produced by this operation.
	decision := m.Evaluate(runtime)
	// `actionErr` stores the error produced by this operation.
	actionErr := m.Execute(decision)
	if actionErr != nil {
		log.Printf("[AUTOHEAL-MANAGER] action=%s level=%s reason=%s err=%v",
			decision.Action, decision.Level, decision.Reason, actionErr)
	}
	// `seedErr` stores the error produced by this operation.
	seedErr := m.seedLatestHealthySnapshot(runtime, decision)
	if seedErr != nil {
		log.Printf("[AUTOHEAL-SEEDER] height=%d status=failed err=%v", runtime.Height, seedErr)
	}
	m.mu.Lock()
	switch {
	case actionErr != nil && seedErr != nil:
		m.lastExecutionError = errors.Join(actionErr, seedErr).Error()
	case actionErr != nil:
		m.lastExecutionError = actionErr.Error()
	case seedErr != nil:
		m.lastExecutionError = seedErr.Error()
	default:
		m.lastExecutionError = ""
	}
	m.mu.Unlock()
	return decision
}

func (m *AutoHealManager) seedLatestHealthySnapshot(runtime RuntimeStatusSnapshot, decision AutoHealDecision) error {
	if m == nil || m.Node == nil || normalizeNodeRole(m.Node.Role) != "validator" || runtime.Height == 0 {
		return nil
	}
	if runtime.Syncing || runtime.NetworkLagBlocks > m.Config.NormalSyncLagBlocks ||
		(decision.Action != AutoHealActionNone && decision.Action != AutoHealActionNormalSync) ||
		!runtime.Ready || !runtime.ConsensusRunning ||
		(runtime.NetworkQuorumRequired > 0 && runtime.NetworkQuorumVotes < runtime.NetworkQuorumRequired) {
		return nil
	}
	publishedHeight, _, _, publishErr := m.Node.validatorSnapshotPublicationState()
	if publishedHeight >= runtime.Height && strings.TrimSpace(publishErr) == "" {
		return nil
	}
	now := m.now()
	m.mu.Lock()
	if m.lastSnapshotSeedHeight >= runtime.Height ||
		(!m.lastSnapshotSeedAt.IsZero() && now.Sub(m.lastSnapshotSeedAt) < m.Config.SnapshotSeedInterval) {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	snapshot, err := m.Node.publishRequiredValidatorSnapshot("autoheal_snapshot_seeder", true)
	if err != nil {
		return err
	}
	if snapshot == nil || snapshot.Height != runtime.Height {
		return fmt.Errorf("latest verified snapshot unavailable: tip=%d", runtime.Height)
	}
	if ok, reason := m.Node.snapshotMatchesLocalAnchorDetailed(snapshot); !ok {
		return fmt.Errorf("latest snapshot anchor verification failed: %s", strings.TrimSpace(reason))
	}
	m.mu.Lock()
	m.lastSnapshotSeedAt = now
	m.lastSnapshotSeedHeight = snapshot.Height
	m.mu.Unlock()
	log.Printf("[AUTOHEAL-SEEDER] height=%d hash=%s status=published", snapshot.Height, ShortHash(snapshot.SnapshotHash))
	return nil
}

// Evaluate implements the evaluate helper.
func (m *AutoHealManager) Evaluate(runtime RuntimeStatusSnapshot) AutoHealDecision {
	if m == nil {
		return AutoHealDecision{Level: AutoHealLevelHealthy, Action: AutoHealActionNone, Reason: "manager_unavailable"}
	}
	// `now` stores the value produced by this operation.
	now := m.now()
	if now.IsZero() {
		now = time.Now()
	}

	// `lag` stores the value produced by this operation.
	lag := maxAutoHealLag(runtime)
	// `target` stores the value produced by this operation.
	target := runtime.NetworkBestHeight
	if runtime.SyncTarget > target {
		target = runtime.SyncTarget
	}
	if target < runtime.Height {
		target = runtime.Height
	}

	m.mu.Lock()
	if m.heightObservedAt.IsZero() || runtime.Height != m.lastHeight {
		m.lastHeight = runtime.Height
		m.heightObservedAt = now
	}
	if m.commitObservedAt.IsZero() || runtime.LastCommitHeight != m.lastCommitHeight {
		m.lastCommitHeight = runtime.LastCommitHeight
		m.commitObservedAt = now
	}
	// `stuckFor` stores the value produced by this operation.
	stuckFor := now.Sub(m.heightObservedAt)
	if runtime.LastBlockAgeSeconds > 0 {
		// `age` stores the value produced by this operation.
		age := time.Duration(runtime.LastBlockAgeSeconds) * time.Second
		if age > stuckFor {
			stuckFor = age
		}
	}
	if stuckFor < 0 {
		stuckFor = 0
	}
	m.mu.Unlock()

	// `decision` stores the value produced by this operation.
	decision := AutoHealDecision{
		Level:          AutoHealLevelHealthy,
		Action:         AutoHealActionNone,
		Reason:         "healthy",
		LocalHeight:    runtime.Height,
		TargetHeight:   target,
		LagBlocks:      lag,
		StuckFor:       stuckFor,
		Peers:          runtime.Peers,
		QuorumVotes:    runtime.NetworkQuorumVotes,
		QuorumRequired: runtime.NetworkQuorumRequired,
	}

	// `isolated` stores the current position in the related collection.
	isolated := runtime.Peers == 0 ||
		(runtime.NetworkQuorumRequired > 0 && runtime.NetworkQuorumVotes > 0 && runtime.NetworkQuorumVotes < runtime.NetworkQuorumRequired)
	// `stalled` stores the value produced by this operation.
	stalled := stuckFor >= m.Config.DiagnosticsStuckAfter ||
		strings.EqualFold(runtime.BlockProductionStatus, "stalled") ||
		strings.EqualFold(runtime.NetworkHealth, "block_stalled")
	// `corruptionReason` stores the detected corruption repair reason.
	corruptionReason := autoHealCorruptionReason(runtime)

	switch {
	case isolated && (lag > 0 || stalled):
		decision.Level = AutoHealLevel3
		if stuckFor >= m.Config.AutomaticRepairAfter {
			decision.Level = AutoHealLevel4
		}
		decision.Action = AutoHealActionPeerIsolation
		decision.Reason = "peer_isolation_or_quorum_loss"
	case corruptionReason != "" && target > runtime.Height:
		decision.Level = AutoHealLevel2
		decision.Action = AutoHealActionSnapshotSync
		decision.Reason = corruptionReason
	case corruptionReason != "":
		decision.Level = AutoHealLevel4
		decision.Action = AutoHealActionAutomaticRepair
		decision.Reason = corruptionReason
	case stuckFor >= m.Config.AutomaticRepairAfter:
		decision.Level = AutoHealLevel4
		decision.Action = AutoHealActionAutomaticRepair
		decision.Reason = "height_stuck_automatic_repair"
	case lag > m.Config.SnapshotSyncLagBlocks:
		decision.Level = AutoHealLevel2
		decision.Action = AutoHealActionSnapshotSync
		decision.Reason = "lag_exceeds_snapshot_threshold"
	case stuckFor >= m.Config.DiagnosticsStuckAfter:
		decision.Level = AutoHealLevel3
		decision.Action = AutoHealActionDiagnostics
		decision.Reason = "height_stuck_diagnostics"
	case lag > 0:
		decision.Level = AutoHealLevel1
		decision.Action = AutoHealActionNormalSync
		if lag < m.Config.NormalSyncLagBlocks {
			decision.Reason = "lag_below_normal_sync_threshold"
		} else {
			decision.Reason = "moderate_lag_normal_sync"
		}
	}

	m.mu.Lock()
	m.lastDecision = decision
	m.lastDiagnostics = autoHealDiagnosticsFromRuntime(runtime)
	m.mu.Unlock()
	return decision
}

// Execute implements the execute helper.
func (m *AutoHealManager) Execute(decision AutoHealDecision) error {
	if m == nil || m.Node == nil {
		return errors.New("autoheal manager unavailable")
	}
	// `execute` stores the value produced by this operation.
	execute := func() error {
		switch decision.Action {
		case AutoHealActionNone:
			return nil
		case AutoHealActionNormalSync:
			m.Node.setSyncAction("autoheal_normal_sync", decision.LagBlocks, "normal_sync")
			if decision.TargetHeight > decision.LocalHeight {
				m.Node.maybeSyncToBestObservedHeight("autoheal_normal_sync")
			}
			return nil
		case AutoHealActionSnapshotSync:
			return m.startSnapshotSync(decision, "autoheal_lag_snapshot")
		case AutoHealActionDiagnostics:
			m.logDiagnostics(decision)
			return nil
		case AutoHealActionPeerIsolation:
			return m.recoverPeerIsolation(decision)
		case AutoHealActionAutomaticRepair:
			return m.runAutomaticRepair(decision)
		case AutoHealActionRegistryRepair:
			return m.runRegistryRepair(decision.LocalHeight)
		case AutoHealActionDBRecovery:
			return m.runDBRecovery(decision.LocalHeight)
		default:
			return fmt.Errorf("unknown autoheal action: %s", decision.Action)
		}
	}
	if autoHealActionMutatesChainData(decision.Action) {
		return m.withProtectedIdentityInvariant(execute)
	}
	return execute()
}

// autoHealActionMutatesChainData reports actions that may apply or rebuild chain state.
func autoHealActionMutatesChainData(action AutoHealAction) bool {
	switch action {
	case AutoHealActionSnapshotSync, AutoHealActionAutomaticRepair, AutoHealActionRegistryRepair, AutoHealActionDBRecovery:
		return true
	default:
		return false
	}
}

// startSnapshotSync implements the start snapshot sync helper.
func (m *AutoHealManager) startSnapshotSync(decision AutoHealDecision, reason string) error {
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight {
		return fmt.Errorf("snapshot target unavailable: local=%d target=%d", decision.LocalHeight, decision.TargetHeight)
	}
	m.Node.setSyncAction("autoheal_snapshot_sync", decision.LagBlocks, "snapshot_sync")
	m.Node.forceSnapshotSyncToHeight(decision.TargetHeight, reason)
	return nil
}

// runAutomaticRepair implements the run automatic repair helper.
func (m *AutoHealManager) runAutomaticRepair(decision AutoHealDecision) error {
	m.mu.Lock()
	if m.repairInFlight {
		m.mu.Unlock()
		return nil
	}
	m.repairInFlight = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.repairInFlight = false
		m.mu.Unlock()
	}()

	m.logDiagnostics(decision)
	if decision.TargetHeight > decision.LocalHeight {
		// Use the complete trusted sync state machine here. It applies the
		// quorum snapshot, delta-replays to the target, clears sync mode, and
		// arms validator rejoin warmup only after catch-up is complete.
		return m.startSnapshotSync(decision, "autoheal_automatic_repair_snapshot")
	}
	if m.Config.SnapshotAllowSameHeight && decision.LocalHeight > 0 {
		manager := NewSnapshotManager(m.Node, decision.LocalHeight, decision.LocalHeight, m.Config.SnapshotStrictCoreMode)
		if _, _, err := manager.StartSession(true); err == nil {
			_ = m.Node.advanceConsensusToCommittedTip("autoheal_same_height_snapshot_repair")
			return nil
		}
	}
	// `err` stores the error produced by this operation.
	if err := m.runRegistryRepair(decision.LocalHeight); err == nil {
		_ = m.Node.advanceConsensusToCommittedTip("autoheal_registry_repair")
		return nil
	}
	return m.runDBRecovery(decision.LocalHeight)
}

// runRegistryRepair implements the run registry repair helper.
func (m *AutoHealManager) runRegistryRepair(height uint64) error {
	if m == nil || m.Node == nil || height == 0 {
		return fmt.Errorf("registry repair height unavailable")
	}
	// `registry`, `hash`, `source`, and `ok` store whether the related condition is satisfied.
	if registry, hash, source, ok := m.Node.ResolveCommittedRegistrySnapshot(height); ok && len(registry) > 0 {
		log.Printf("[AUTOHEAL-MANAGER] registry repair verified height=%d hash=%s source=%s validators=%d",
			height, ShortHash(hash), strings.TrimSpace(source), len(registry))
		return nil
	}
	// `source` and `ok` store whether the related condition is satisfied.
	if source, ok := m.Node.ensureCommittedTipStateSnapshot(height, "autoheal_registry_repair"); ok {
		log.Printf("[AUTOHEAL-MANAGER] registry repair materialized height=%d source=%s", height, strings.TrimSpace(source))
		return nil
	}
	return fmt.Errorf("registry repair unresolved height=%d", height)
}

// runDBRecovery implements the run db recovery helper.
func (m *AutoHealManager) runDBRecovery(height uint64) error {
	if m == nil || m.Node == nil || height == 0 {
		return fmt.Errorf("db recovery height unavailable")
	}
	// `rebuilt` and `err` store the error produced by this operation.
	rebuilt, err := m.Node.rebuildCommittedSnapshotHeight(height)
	if err == nil && rebuilt {
		log.Printf("[AUTOHEAL-MANAGER] db recovery rebuilt committed snapshot height=%d", height)
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("db recovery had no rebuild candidate height=%d", height)
}

// recoverPeerIsolation implements the recover peer isolation helper.
func (m *AutoHealManager) recoverPeerIsolation(decision AutoHealDecision) error {
	if m == nil || m.Node == nil {
		return errors.New("autoheal manager unavailable")
	}
	m.logDiagnostics(decision)
	m.Node.peerStateMu.Lock()
	// `peerID` tracks the current values while iterating.
	for peerID := range m.Node.peerDialFailures {
		delete(m.Node.peerDialFailures, peerID)
		delete(m.Node.peerDialNext, peerID)
	}
	// `peerID` tracks the current values while iterating.
	for peerID := range m.Node.connectingPeers {
		delete(m.Node.connectingPeers, peerID)
	}
	m.Node.peerStateMu.Unlock()
	if decision.TargetHeight > decision.LocalHeight {
		m.Node.maybeSyncToBestObservedHeight("autoheal_peer_isolation")
	}
	return nil
}

// logDiagnostics implements the log diagnostics helper.
func (m *AutoHealManager) logDiagnostics(decision AutoHealDecision) {
	if m == nil || m.Node == nil {
		return
	}
	m.mu.Lock()
	// `diag` stores the value produced by this operation.
	diag := m.lastDiagnostics
	m.mu.Unlock()
	log.Printf("[AUTOHEAL-MANAGER] level=%s action=%s reason=%s height=%d finalized=%d target=%d lag=%d stuck=%s peers=%d quorum=%d/%d block_status=%s network=%s sync_stage=%s snapshot=%d:%s",
		decision.Level,
		decision.Action,
		decision.Reason,
		diag.Height,
		diag.FinalizedHeight,
		diag.NetworkBestHeight,
		diag.NetworkLagBlocks,
		decision.StuckFor,
		diag.Peers,
		diag.NetworkQuorumVotes,
		diag.NetworkQuorumRequired,
		diag.BlockProductionStatus,
		diag.NetworkHealth,
		diag.SyncStage,
		diag.SnapshotHeight,
		ShortHash(diag.SnapshotHash),
	)
}

// LastDecision implements the last decision helper.
func (m *AutoHealManager) LastDecision() (AutoHealDecision, AutoHealDiagnostics, string) {
	if m == nil {
		return AutoHealDecision{}, AutoHealDiagnostics{}, ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastDecision, m.lastDiagnostics, m.lastExecutionError
}

// autoHealDiagnosticsFromRuntime implements the auto heal diagnostics from runtime helper.
func autoHealDiagnosticsFromRuntime(runtime RuntimeStatusSnapshot) AutoHealDiagnostics {
	return AutoHealDiagnostics{
		Height:                     runtime.Height,
		FinalizedHeight:            runtime.FinalizedHeight,
		NetworkBestHeight:          runtime.NetworkBestHeight,
		NetworkLagBlocks:           runtime.NetworkLagBlocks,
		LastBlockAgeSeconds:        runtime.LastBlockAgeSeconds,
		LastCommitHeight:           runtime.LastCommitHeight,
		Peers:                      runtime.Peers,
		NetworkQuorumVotes:         runtime.NetworkQuorumVotes,
		NetworkQuorumRequired:      runtime.NetworkQuorumRequired,
		BlockProductionStatus:      runtime.BlockProductionStatus,
		NetworkHealth:              runtime.NetworkHealth,
		SyncStage:                  runtime.SyncStage,
		SnapshotVerifyStage:        runtime.SnapshotVerifyStage,
		SnapshotHeight:             runtime.SnapshotHeight,
		SnapshotHash:               runtime.SnapshotHash,
		SyncAnchorLastError:        runtime.SyncAnchorLastError,
		SyncAnchorLastRejectReason: runtime.SyncAnchorLastRejectReason,
		ValidatorSnapshotLastError: runtime.ValidatorSnapshotLastError,
	}
}

// maxAutoHealLag returns the maximum auto heal lag.
func maxAutoHealLag(runtime RuntimeStatusSnapshot) uint64 {
	// `lag` stores the value produced by this operation.
	lag := runtime.NetworkLagBlocks
	if runtime.SyncLagBlocks > lag {
		lag = runtime.SyncLagBlocks
	}
	if runtime.SyncTarget > runtime.Height && runtime.SyncTarget-runtime.Height > lag {
		lag = runtime.SyncTarget - runtime.Height
	}
	if runtime.NetworkBestHeight > runtime.Height && runtime.NetworkBestHeight-runtime.Height > lag {
		lag = runtime.NetworkBestHeight - runtime.Height
	}
	return lag
}

// startAutoHealManager implements the start auto heal manager helper.
func (n *Node) startAutoHealManager(ctx context.Context) {
	if n == nil {
		return
	}
	// `manager` stores the value produced by this operation.
	manager := n.ensureAutoHealManager()
	manager.Run(ctx)
}

// ensureAutoHealManager returns the retained node-level auto-heal manager.
func (n *Node) ensureAutoHealManager() *AutoHealManager {
	if n == nil {
		return nil
	}
	n.autoHealMu.Lock()
	defer n.autoHealMu.Unlock()
	if n.autoHealManager == nil {
		n.autoHealManager = NewAutoHealManager(n, DefaultAutoHealConfig())
	}
	return n.autoHealManager
}

// autoHealManagerSnapshot returns the current node-level auto-heal manager.
func (n *Node) autoHealManagerSnapshot() *AutoHealManager {
	if n == nil {
		return nil
	}
	n.autoHealMu.RLock()
	defer n.autoHealMu.RUnlock()
	return n.autoHealManager
}

// applyAutoHealRuntimeStatus attaches the last auto-heal decision to runtime status.
func (n *Node) applyAutoHealRuntimeStatus(out *RuntimeStatusSnapshot) {
	if out == nil {
		return
	}
	out.AutoHealLevel = string(AutoHealLevelHealthy)
	out.AutoHealAction = string(AutoHealActionNone)
	out.AutoHealReason = "manager_not_started"
	if n == nil {
		return
	}
	manager := n.autoHealManagerSnapshot()
	if manager == nil {
		return
	}
	decision, _, lastErr := manager.LastDecision()
	if decision.Level == "" {
		out.AutoHealReason = "waiting_first_tick"
		return
	}
	out.AutoHealLevel = string(decision.Level)
	out.AutoHealAction = string(decision.Action)
	out.AutoHealReason = strings.TrimSpace(decision.Reason)
	out.AutoHealTargetHeight = decision.TargetHeight
	out.AutoHealLagBlocks = decision.LagBlocks
	if decision.StuckFor > 0 {
		out.AutoHealStuckSeconds = uint64(decision.StuckFor / time.Second)
	}
	out.AutoHealLastError = strings.TrimSpace(lastErr)
}

// autoHealCorruptionReason returns a repair reason when runtime status reports corrupt chain data.
func autoHealCorruptionReason(runtime RuntimeStatusSnapshot) string {
	checks := []struct {
		value  string
		reason string
	}{
		{runtime.SyncAnchorLastRejectReason, "snapshot_reject_repair_required"},
		{runtime.SyncAnchorLastError, "snapshot_error_repair_required"},
		{runtime.ValidatorSnapshotLastError, "validator_snapshot_repair_required"},
		{runtime.SnapshotVerifyStage, "snapshot_verify_repair_required"},
	}
	for _, check := range checks {
		if autoHealLooksCorrupt(check.value) {
			return check.reason
		}
	}
	return ""
}

// autoHealLooksCorrupt classifies corruption-style errors from snapshot/registry paths.
func autoHealLooksCorrupt(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	corruptionTokens := []string{
		"corrupt",
		"hash_mismatch",
		"hash mismatch",
		"registry_snapshot_hash_mismatch",
		"snapshot_hash_mismatch",
		"snapshot metadata hash mismatch",
		"snapshot_finalized_hash_mismatch",
		"snapshot_irreversible_hash_conflict",
		"snapshot_finalized_hash_conflict",
		"quorum_verification_failed",
		"verification_failed",
		"verify_failed",
		"state_root_mismatch",
		"merkle_root_mismatch",
	}
	for _, token := range corruptionTokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

type autoHealIdentityFingerprint struct {
	Exists bool
	Mode   os.FileMode
	Size   int64
	Hash   string
}

// withProtectedIdentityInvariant fails closed if a chain repair changes key or identity material.
func (m *AutoHealManager) withProtectedIdentityInvariant(repair func() error) error {
	if m == nil || m.Node == nil {
		return errors.New("autoheal manager unavailable")
	}
	before, err := captureAutoHealIdentityManifest(m.Node)
	if err != nil {
		return fmt.Errorf("autoheal identity preflight failed: %w", err)
	}
	repairErr := repair()
	after, verifyErr := captureAutoHealIdentityManifest(m.Node)
	if verifyErr != nil {
		verifyErr = fmt.Errorf("autoheal identity postflight failed: %w", verifyErr)
	} else {
		verifyErr = compareAutoHealIdentityManifests(before, after)
	}
	if verifyErr != nil {
		if repairErr != nil {
			return errors.Join(repairErr, verifyErr)
		}
		return verifyErr
	}
	return repairErr
}

// captureAutoHealIdentityManifest fingerprints active identity and key locations only.
func captureAutoHealIdentityManifest(node *Node) (map[string]autoHealIdentityFingerprint, error) {
	if node == nil {
		return nil, errors.New("node unavailable")
	}
	dataRoot := strings.TrimSpace(node.DataDir)
	if dataRoot == "" {
		dataRoot = "."
	}
	nodeRoot := strings.TrimSpace(node.recoveryNodeRoot())
	if nodeRoot == "" {
		return nil, errors.New("node identity root unavailable")
	}
	dataRoot, err := filepath.Abs(filepath.Clean(dataRoot))
	if err != nil {
		return nil, err
	}
	nodeRoot, err = filepath.Abs(filepath.Clean(nodeRoot))
	if err != nil {
		return nil, err
	}

	manifest := make(map[string]autoHealIdentityFingerprint)
	// `record` stores the value produced by this operation.
	record := func(path string) error {
		path, err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			return err
		}
		key := filepath.ToSlash(path)
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			manifest[key] = autoHealIdentityFingerprint{}
			return nil
		}
		if statErr != nil {
			return statErr
		}
		fingerprint := autoHealIdentityFingerprint{Exists: true, Mode: info.Mode(), Size: info.Size()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return readErr
			}
			fingerprint.Hash = checksumBytes([]byte(target))
		case info.Mode().IsRegular():
			hash, size, readErr := checksumFile(path)
			if readErr != nil {
				return readErr
			}
			fingerprint.Hash = hash
			fingerprint.Size = size
		}
		manifest[key] = fingerprint
		return nil
	}

	exactNames := []string{
		"validator.sec", "validator.pub", "validator.key", "validator_key.json", "validator.meta.json",
		"consensus.sec", "consensus.pub", "consensus.key", "consensus_key.json",
		"priv_validator_key.json", "priv_validator_state.json", "node_key.json", "p2p_identity.key",
		"mpc.key", "fingerprint.lock", "validator_identity.lock",
	}
	for _, root := range []string{dataRoot, nodeRoot} {
		for _, name := range exactNames {
			if err := record(filepath.Join(root, name)); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range []string{"config.toml", "config.mpc.toml", "genesis.json", "peers.json", "storage_layout.json"} {
		if err := record(filepath.Join(dataRoot, name)); err != nil {
			return nil, err
		}
	}

	protectedDirs := []string{"mpc", "keys", "validator_keys", "consensus_keys", "hsm", "secure-backups", "key-backups", "keystore", "wallets"}
	seenDirs := make(map[string]struct{})
	for _, root := range []string{dataRoot, nodeRoot} {
		for _, name := range protectedDirs {
			dir := filepath.Join(root, name)
			absDir, absErr := filepath.Abs(filepath.Clean(dir))
			if absErr != nil {
				return nil, absErr
			}
			if _, duplicate := seenDirs[absDir]; duplicate {
				continue
			}
			seenDirs[absDir] = struct{}{}
			if err := record(absDir); err != nil {
				return nil, err
			}
			if info, statErr := os.Lstat(absDir); statErr == nil && info.IsDir() {
				if walkErr := filepath.Walk(absDir, func(path string, info os.FileInfo, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if filepath.Clean(path) == filepath.Clean(absDir) {
						return nil
					}
					return record(path)
				}); walkErr != nil {
					return nil, walkErr
				}
			}
		}
	}
	return manifest, nil
}

// compareAutoHealIdentityManifests reports the first protected identity mutation.
func compareAutoHealIdentityManifests(before map[string]autoHealIdentityFingerprint, after map[string]autoHealIdentityFingerprint) error {
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		left, leftOK := before[path]
		right, rightOK := after[path]
		if !leftOK || !rightOK || left != right {
			return fmt.Errorf("autoheal protected identity changed: %s", path)
		}
	}
	return nil
}

// ValidateRepairPath validates repair path.
func (m *AutoHealManager) ValidateRepairPath(path string) error {
	if m == nil || m.Node == nil {
		return errors.New("autoheal manager unavailable")
	}
	// `root` stores the digest used to identify or verify the related data.
	root := m.Node.recoveryNodeRoot()
	// `rel` and `err` store the error produced by this operation.
	rel, err := autoHealRelativePath(root, path)
	if err != nil {
		return err
	}
	if autoHealProtectedPath(rel) {
		return fmt.Errorf("autoheal protected path rejected: %s", rel)
	}
	if !autoHealRepairablePath(rel) {
		return fmt.Errorf("autoheal path is not in repairable chain-data set: %s", rel)
	}
	return nil
}

// autoHealRelativePath implements the auto heal relative path helper.
func autoHealRelativePath(root string, path string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	path = filepath.Clean(strings.TrimSpace(path))
	if root == "." || root == "" || path == "." || path == "" {
		return "", errors.New("invalid autoheal path")
	}
	// `absRoot` and `err` store the error produced by this operation.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// `absPath` and `err` store the error produced by this operation.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// `rel` and `err` store the error produced by this operation.
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return "", errors.New("node root is not a repair target")
	}
	if strings.HasPrefix(rel, "../") || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("autoheal path escapes node root: %s", path)
	}
	return rel, nil
}

// autoHealProtectedPath implements the auto heal protected path helper.
func autoHealProtectedPath(rel string) bool {
	rel = strings.Trim(strings.ToLower(filepath.ToSlash(filepath.Clean(rel))), "/")
	if rel == "" || rel == "." {
		return true
	}
	// `protectedExact` stores the value produced by this operation.
	protectedExact := map[string]struct{}{
		"validator.sec":             {},
		"validator.pub":             {},
		"validator.key":             {},
		"validator_key.json":        {},
		"validator.meta.json":       {},
		"consensus.sec":             {},
		"consensus.pub":             {},
		"consensus.key":             {},
		"consensus_key.json":        {},
		"mpc.key":                   {},
		"priv_validator_key.json":   {},
		"priv_validator_state.json": {},
		"node_key.json":             {},
		"p2p_identity.key":          {},
		"fingerprint.lock":          {},
		"config.toml":               {},
		"config.mpc.toml":           {},
		"peers.json":                {},
		"wallet.key":                {},
		"wallet.sec":                {},
		"genesis.json":              {},
		"storage_layout.json":       {},
		"validator_identity.lock":   {},
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := protectedExact[rel]; ok {
		return true
	}
	// `protectedPrefixes` stores the value produced by this operation.
	protectedPrefixes := []string{
		"mpc/",
		"keys/",
		"validator_keys/",
		"consensus_keys/",
		"hsm/",
		"secure-backups/",
		"key-backups/",
		"keystore/",
		"wallets/",
	}
	// `prefix` tracks the current values while iterating.
	for _, prefix := range protectedPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// autoHealRepairablePath implements the auto heal repairable path helper.
func autoHealRepairablePath(rel string) bool {
	rel = strings.Trim(strings.ToLower(filepath.ToSlash(filepath.Clean(rel))), "/")
	if rel == "" || rel == "." || autoHealProtectedPath(rel) {
		return false
	}
	// `repairableExact` stores the value produced by this operation.
	repairableExact := map[string]struct{}{
		"state.db":                     {},
		"blocks.db":                    {},
		"snapshot.db":                  {},
		"tx.db":                        {},
		"meta.db":                      {},
		"blocks":                       {},
		"snapshots":                    {},
		"state_checkpoints":            {},
		"cold-storage":                 {},
		"epoch_anchor_hashes":          {},
		"finalized_epoch_certificates": {},
		"irreversible_roots":           {},
		"validator_commitments":        {},
		"validators.json":              {},
		"ledger.json":                  {},
		"ledger.json.bak":              {},
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := repairableExact[rel]; ok {
		return true
	}
	// `repairablePrefixes` stores the value produced by this operation.
	repairablePrefixes := []string{
		"state.db/",
		"blocks.db/",
		"snapshot.db/",
		"tx.db/",
		"meta.db/",
		"blocks/",
		"snapshots/",
		"state_checkpoints/",
		"cold-storage/",
		"epoch_anchor_hashes/",
		"finalized_epoch_certificates/",
		"irreversible_roots/",
		"validator_commitments/",
	}
	// `prefix` tracks the current values while iterating.
	for _, prefix := range repairablePrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// autoHealEnsureRepairPath implements the auto heal ensure repair path helper.
func autoHealEnsureRepairPath(path string, mode os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("empty autoheal repair path")
	}
	return os.MkdirAll(path, mode)
}
