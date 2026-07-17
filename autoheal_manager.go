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
	// `autoHealExactCatchupMaxPerTick` caps small-lag exact block recovery so a
	// single auto-heal tick cannot monopolize CPU/network while catching up.
	autoHealExactCatchupMaxPerTick = 8
)

const (
	autoHealRecoveryStageHealthy         = "healthy"
	autoHealRecoveryStageExactBlock      = "exact_block_fetch"
	autoHealRecoveryStageRangeCatchup    = "range_catchup"
	autoHealRecoveryStageTrustedSnapshot = "trusted_snapshot"
	autoHealRecoveryStageExecutionVerify = "execution_verify"
	autoHealRecoveryStageRejoinVoting    = "rejoin_voting"
	autoHealRecoveryStageOperatorAlert   = "operator_alert"
)

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
	// `MempoolPressureDepth` stores the pending transaction depth that requires
	// faster self-heal escalation.
	MempoolPressureDepth int
	// `MempoolPressureRepairAfter` stores how quickly a stalled high-pressure
	// mempool should move from diagnostics to automatic repair.
	MempoolPressureRepairAfter time.Duration
	// `CommitRecoveryCooldown` bounds commit-cache recovery and consensus soft
	// restarts at the same height so recovery gets time to make progress.
	CommitRecoveryCooldown time.Duration
	// `ExactCatchupTimeout` bounds exact missing-block recovery calls so a
	// wedged peer/apply path cannot stop the auto-heal manager loop.
	ExactCatchupTimeout time.Duration
	// `RangeCatchupTimeout` bounds range catch-up calls so a hung peer request
	// cannot prevent snapshot fallback and later auto-heal ticks.
	RangeCatchupTimeout time.Duration
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
	// `MempoolDepth` stores the pending transaction count seen by runtime status.
	MempoolDepth int `json:"mempool_depth"`
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

type AutoHealRecoverySession struct {
	ID            string `json:"id"`
	StartHeight   uint64 `json:"start_height"`
	TargetHeight  uint64 `json:"target_height"`
	Stage         string `json:"stage"`
	RetryCount    uint64 `json:"retry_count"`
	SnapshotHash  string `json:"snapshot_hash,omitempty"`
	Result        string `json:"result"`
	LastError     string `json:"last_error,omitempty"`
	StartedAtUnix int64  `json:"started_at_unix"`
	UpdatedAtUnix int64  `json:"updated_at_unix"`
	RejoinReady   bool   `json:"rejoin_ready"`
}

type AutoHealManager struct {
	// `Node` stores the value associated with this record.
	Node *Node
	// `Config` stores the configuration used by this operation.
	Config AutoHealConfig

	// `now` stores the value associated with this record.
	now func() time.Time
	// `rangeCatchup` optionally overrides small-lag peer block catch-up for tests.
	rangeCatchup func(*Node, uint64, string) bool
	// `missingBlockCatchup` optionally overrides exact missing block recovery for tests.
	missingBlockCatchup func(*Node, uint64, string) bool

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
	// `lastCommitRecoveryAt` stores the last commit-recovery attempt time.
	lastCommitRecoveryAt time.Time
	// `lastCommitRecoveryHeight` stores the height of the last recovery attempt.
	lastCommitRecoveryHeight uint64
	// `recoverySeq` stores the monotonically increasing recovery session number.
	recoverySeq uint64
	// `recoverySession` stores the current staged recovery state.
	recoverySession AutoHealRecoverySession
}

// DefaultAutoHealConfig returns the default auto heal config.
func DefaultAutoHealConfig() AutoHealConfig {
	return AutoHealConfig{
		TickInterval:               2 * time.Second,
		NormalSyncLagBlocks:        50,
		SnapshotSyncLagBlocks:      500,
		DiagnosticsStuckAfter:      5 * time.Minute,
		AutomaticRepairAfter:       10 * time.Minute,
		SnapshotStrictCoreMode:     true,
		SnapshotAllowSameHeight:    true,
		SnapshotSeedInterval:       5 * time.Second,
		MempoolPressureDepth:       mempoolPressureDepth(),
		MempoolPressureRepairAfter: blockProductionStaleThreshold(),
		CommitRecoveryCooldown:     blockProductionStaleThreshold(),
		ExactCatchupTimeout:        8 * time.Second,
		RangeCatchupTimeout:        12 * time.Second,
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
	if config.MempoolPressureDepth <= 0 {
		config.MempoolPressureDepth = defaults.MempoolPressureDepth
	}
	if config.MempoolPressureRepairAfter <= 0 {
		config.MempoolPressureRepairAfter = defaults.MempoolPressureRepairAfter
	}
	if config.CommitRecoveryCooldown <= 0 {
		config.CommitRecoveryCooldown = defaults.CommitRecoveryCooldown
	}
	if config.ExactCatchupTimeout <= 0 {
		config.ExactCatchupTimeout = defaults.ExactCatchupTimeout
	}
	if config.RangeCatchupTimeout <= 0 {
		config.RangeCatchupTimeout = defaults.RangeCatchupTimeout
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
	if m.tryTickExactCatchup(runtime) {
		runtime = m.Node.runtimeStatusSnapshot()
	}
	m.maybeCompleteRecoveryFromRuntime(runtime)
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

func (m *AutoHealManager) tryTickExactCatchup(runtime RuntimeStatusSnapshot) bool {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil || !runtime.Syncing || runtime.Height == 0 {
		return false
	}
	target := runtime.SyncTarget
	if runtime.NetworkBestHeight > target {
		target = runtime.NetworkBestHeight
	}
	if target <= runtime.Height {
		return false
	}
	lag := target - runtime.Height
	if lag == 0 {
		return false
	}
	if runtime.Peers <= 0 {
		return false
	}
	if runtime.NetworkQuorumVotes == 0 && runtime.NetworkBestHeightVotes == 0 {
		return false
	}
	decision := AutoHealDecision{
		Level:          AutoHealLevel2,
		Action:         AutoHealActionSnapshotSync,
		Reason:         "tick_syncing_exact_catchup",
		LocalHeight:    runtime.Height,
		TargetHeight:   target,
		LagBlocks:      lag,
		Peers:          runtime.Peers,
		QuorumVotes:    runtime.NetworkQuorumVotes,
		QuorumRequired: runtime.NetworkQuorumRequired,
	}
	if m.deferDirectCatchupForTrustedSnapshot(decision, runtime, "tick_syncing_exact_catchup") {
		return false
	}
	before := m.Node.Blockchain.Height()
	reached := m.trySmallLagMissingBlockCatchup(decision, "tick_syncing_exact_catchup")
	if reached || m.Node.Blockchain.Height() > before {
		return true
	}
	if decision.LagBlocks > 0 && decision.LagBlocks <= m.rangeCatchupLimit() {
		if m.tryRangeCatchupOnly(decision, "tick_syncing_range_catchup") {
			return true
		}
		if m.trySmallLagSyncStateResetCatchup(decision, "tick_syncing_state_reset") {
			return true
		}
		if m.shouldEscalateTickDirectCatchupToSnapshot(runtime, decision) {
			if err := m.forceSmallLagSnapshotSync(decision, "tick_syncing_snapshot_escalation"); err != nil {
				log.Printf("[AUTOHEAL-MANAGER] tick_snapshot_escalation failed local=%d target=%d err=%v",
					decision.LocalHeight, decision.TargetHeight, err)
				return false
			}
			return true
		}
	}
	return false
}

func autoHealRecoveryDirectCatchupStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case autoHealRecoveryStageExactBlock, autoHealRecoveryStageRangeCatchup:
		return true
	default:
		return false
	}
}

func autoHealSnapshotStatusVerified(stage string) bool {
	stage = strings.ToLower(strings.TrimSpace(stage))
	return stage == "verified" || stage == "snapshot_verified"
}

func autoHealTrustedSnapshotBaselineReady(local uint64, runtime RuntimeStatusSnapshot, session AutoHealRecoverySession) bool {
	if local == 0 || runtime.SnapshotHeight == 0 || runtime.SnapshotHeight > local {
		return false
	}
	if !autoHealSnapshotStatusVerified(runtime.SnapshotVerifyStage) &&
		!autoHealSnapshotStatusVerified(runtime.SnapshotResumeState) {
		return false
	}
	expectedHash := strings.TrimSpace(session.SnapshotHash)
	if expectedHash == "" {
		return true
	}
	return strings.EqualFold(expectedHash, strings.TrimSpace(runtime.SnapshotHash))
}

func (m *AutoHealManager) trustedSnapshotRecoveryPending(decision AutoHealDecision, runtime RuntimeStatusSnapshot) (bool, RuntimeStatusSnapshot) {
	if m == nil || m.Node == nil {
		return false, runtime
	}
	if runtime.Height == 0 && runtime.SyncTarget == 0 && runtime.NetworkBestHeight == 0 &&
		runtime.SnapshotHeight == 0 && strings.TrimSpace(runtime.SnapshotVerifyStage) == "" &&
		strings.TrimSpace(runtime.SnapshotResumeState) == "" {
		runtime = m.Node.runtimeStatusSnapshot()
	}
	local := decision.LocalHeight
	if m.Node.Blockchain != nil {
		if height := m.Node.Blockchain.Height(); height > 0 {
			local = height
		}
	}
	if runtime.Height > local {
		local = runtime.Height
	}
	target := decision.TargetHeight
	if runtime.SyncTarget > target {
		target = runtime.SyncTarget
	}
	if runtime.NetworkBestHeight > target {
		target = runtime.NetworkBestHeight
	}
	if target == 0 || target <= local {
		return false, runtime
	}
	session := m.RecoverySession()
	if strings.EqualFold(session.Result, "running") &&
		strings.EqualFold(session.Stage, autoHealRecoveryStageTrustedSnapshot) {
		if autoHealTrustedSnapshotBaselineReady(local, runtime, session) {
			return false, runtime
		}
		return true, runtime
	}
	if runtime.SnapshotHeight > local &&
		(autoHealSnapshotStatusVerified(runtime.SnapshotVerifyStage) ||
			autoHealSnapshotStatusVerified(runtime.SnapshotResumeState)) {
		return true, runtime
	}
	return false, runtime
}

func (m *AutoHealManager) deferDirectCatchupForTrustedSnapshot(decision AutoHealDecision, runtime RuntimeStatusSnapshot, reason string) bool {
	pending, runtime := m.trustedSnapshotRecoveryPending(decision, runtime)
	if m == nil || m.Node == nil {
		return false
	}
	if !pending {
		return false
	}
	local := decision.LocalHeight
	if m.Node.Blockchain != nil {
		if height := m.Node.Blockchain.Height(); height > 0 {
			local = height
		}
	}
	target := decision.TargetHeight
	if runtime.SyncTarget > target {
		target = runtime.SyncTarget
	}
	if runtime.NetworkBestHeight > target {
		target = runtime.NetworkBestHeight
	}
	if target <= local {
		return false
	}
	decision.LocalHeight = local
	decision.TargetHeight = target
	decision.LagBlocks = target - local
	m.beginRecoveryStage(decision, autoHealRecoveryStageTrustedSnapshot, "direct_catchup_deferred_"+strings.TrimSpace(reason))
	m.Node.setSyncAction("autoheal_snapshot_sync", decision.LagBlocks, "trusted_snapshot_pending")
	session := m.RecoverySession()
	log.Printf("[AUTOHEAL-MANAGER] direct_catchup_deferred local=%d target=%d recovery_stage=%s snapshot_height=%d snapshot_verify=%s snapshot_resume=%s reason=%s",
		local,
		target,
		strings.TrimSpace(session.Stage),
		runtime.SnapshotHeight,
		strings.TrimSpace(runtime.SnapshotVerifyStage),
		strings.TrimSpace(runtime.SnapshotResumeState),
		strings.TrimSpace(reason),
	)
	return true
}

func (m *AutoHealManager) deferNormalSyncForTrustedSnapshot(decision AutoHealDecision, reason string) bool {
	if decision.TargetHeight <= decision.LocalHeight || decision.LagBlocks == 0 {
		return false
	}
	if !m.deferDirectCatchupForTrustedSnapshot(decision, RuntimeStatusSnapshot{}, reason) {
		return false
	}
	if m != nil && m.Node != nil {
		m.Node.forceSnapshotSyncToHeight(decision.TargetHeight, "autoheal_normal_sync_trusted_snapshot")
		m.noteRecoverySnapshotHash(m.Node.runtimeStatusSnapshot().SnapshotHash)
	}
	return true
}

func (m *AutoHealManager) shouldEscalateTickDirectCatchupToSnapshot(runtime RuntimeStatusSnapshot, decision AutoHealDecision) bool {
	if m == nil || m.Node == nil {
		return false
	}
	decision = m.refreshDecisionTarget(decision)
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight ||
		decision.LagBlocks == 0 || decision.LagBlocks > m.Config.NormalSyncLagBlocks {
		return false
	}
	if runtime.Peers <= 0 {
		return false
	}
	if runtime.NetworkQuorumRequired > 0 && runtime.NetworkQuorumVotes > 0 &&
		runtime.NetworkQuorumVotes < runtime.NetworkQuorumRequired {
		return false
	}
	if runtime.ExecutionDivergence ||
		strings.EqualFold(strings.TrimSpace(runtime.ExecutionDivergenceReason), "state_root_mismatch") {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(runtime.BlockProductionStatus + " " + runtime.NetworkHealth))
	if strings.Contains(status, "stalled") || strings.Contains(status, "ahead") || strings.Contains(status, "syncing") {
		return true
	}
	stuckAfter := uint64(20)
	if m.Config.DiagnosticsStuckAfter > 0 {
		if seconds := uint64(m.Config.DiagnosticsStuckAfter.Seconds() / 4); seconds > stuckAfter {
			stuckAfter = seconds
		}
	}
	return runtime.LastBlockAgeSeconds >= stuckAfter
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

	// `quorumLoss` stores whether this node cannot observe enough consensus
	// participants. Zero observed votes is still a quorum loss; requiring a
	// positive vote count hid isolated/stale-peer nodes as healthy.
	quorumLoss := (runtime.NetworkQuorumRequired > 0 && runtime.NetworkQuorumVotes < runtime.NetworkQuorumRequired) ||
		(runtime.RequiredQuorum > 0 && runtime.LiveValidators > 0 && runtime.LiveValidators < runtime.RequiredQuorum)
	// `networkQuorumVisible` stores whether this node can still observe a
	// usable finalized chain ahead of its local tip. A locally lagging validator
	// can report too few live validators for its own height while the network
	// quorum is healthy; that is catch-up, not true peer isolation.
	networkQuorumVisible := runtime.NetworkQuorumRequired == 0 || runtime.NetworkQuorumVotes >= runtime.NetworkQuorumRequired
	behindWithVisibleQuorum := target > runtime.Height && lag > 0 && runtime.Peers > 0 && networkQuorumVisible
	// `detectorMode` stores the normalized consensus detector mode.
	detectorMode := autoHealDetectorMode(runtime)
	// `detectorUnhealthy` stores whether the consensus detector has escalated.
	detectorUnhealthy := autoHealDetectorUnhealthy(detectorMode, runtime)
	// `isolated` stores the current position in the related collection.
	isolated := runtime.Peers == 0 || quorumLoss || detectorMode == string(ConsensusDetectorPartition)
	// `stalled` stores the value produced by this operation.
	stalled := stuckFor >= m.Config.DiagnosticsStuckAfter ||
		strings.EqualFold(runtime.BlockProductionStatus, "stalled") ||
		strings.EqualFold(runtime.NetworkHealth, "block_stalled") ||
		detectorUnhealthy
	// `mempoolPressure` stores whether the transaction lane is deep enough
	// that a stalled block should self-heal before the generic 10 minute repair
	// window. This matches live incidents where the chain was healthy enough to
	// restart but stopped consuming already accepted transactions.
	mempoolPressure := runtime.MempoolDepth >= m.Config.MempoolPressureDepth
	mempoolPressureStalled := mempoolPressure && stalled
	// `corruptionReason` stores the detected corruption repair reason.
	corruptionReason := autoHealCorruptionReason(runtime)

	switch {
	case behindWithVisibleQuorum && stuckFor >= m.Config.AutomaticRepairAfter:
		decision.Level = AutoHealLevel4
		decision.Action = AutoHealActionAutomaticRepair
		decision.Reason = "height_stuck_automatic_repair"
	case behindWithVisibleQuorum &&
		stuckFor >= m.Config.DiagnosticsStuckAfter &&
		(stalled || strings.EqualFold(runtime.BlockProductionStatus, "network_ahead") || strings.EqualFold(runtime.NetworkHealth, "behind")):
		decision.Level = AutoHealLevel2
		decision.Action = AutoHealActionSnapshotSync
		decision.Reason = "stuck_network_ahead_snapshot_sync"
	case behindWithVisibleQuorum &&
		lag > m.Config.SnapshotSyncLagBlocks &&
		(strings.EqualFold(runtime.BlockProductionStatus, "network_ahead") || strings.EqualFold(runtime.NetworkHealth, "behind")):
		decision.Level = AutoHealLevel2
		decision.Action = AutoHealActionSnapshotSync
		decision.Reason = "network_ahead_snapshot_sync"
	case behindWithVisibleQuorum && runtime.Syncing && lag > m.Config.NormalSyncLagBlocks:
		decision.Level = AutoHealLevel2
		decision.Action = AutoHealActionSnapshotSync
		decision.Reason = "syncing_network_ahead_snapshot_sync"
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
	case mempoolPressureStalled && stuckFor >= m.Config.MempoolPressureRepairAfter:
		decision.Level = AutoHealLevel4
		decision.Action = AutoHealActionAutomaticRepair
		decision.Reason = "mempool_pressure_block_stall"
	case stuckFor >= m.Config.AutomaticRepairAfter:
		decision.Level = AutoHealLevel4
		decision.Action = AutoHealActionAutomaticRepair
		decision.Reason = "height_stuck_automatic_repair"
	case lag > m.Config.SnapshotSyncLagBlocks:
		decision.Level = AutoHealLevel2
		decision.Action = AutoHealActionSnapshotSync
		decision.Reason = "lag_exceeds_snapshot_threshold"
	case stalled:
		decision.Level = AutoHealLevel3
		decision.Action = AutoHealActionDiagnostics
		if mempoolPressureStalled {
			decision.Reason = "mempool_pressure_diagnostics"
		} else if detectorUnhealthy {
			decision.Reason = "consensus_detector_" + strings.ToLower(detectorMode)
		} else {
			decision.Reason = "block_production_stalled"
		}
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

func autoHealDetectorMode(runtime RuntimeStatusSnapshot) string {
	return strings.ToUpper(strings.TrimSpace(runtime.ConsensusDetectorMode))
}

func autoHealDetectorUnhealthy(mode string, runtime RuntimeStatusSnapshot) bool {
	switch mode {
	case string(ConsensusDetectorHalted), string(ConsensusDetectorEmergency), string(ConsensusDetectorPartition), string(ConsensusDetectorAttack):
		return true
	default:
		return runtime.ConsensusDetectorPartitionRisk || runtime.ConsensusDetectorAttack
	}
}

// Execute implements the execute helper.
func (m *AutoHealManager) Execute(decision AutoHealDecision) error {
	if m == nil || m.Node == nil {
		return errors.New("autoheal manager unavailable")
	}
	if decision.Action != AutoHealActionNone {
		m.Node.observeAutoHealAction()
	}
	// `execute` stores the value produced by this operation.
	execute := func() error {
		switch decision.Action {
		case AutoHealActionNone:
			return nil
		case AutoHealActionNormalSync:
			if m.deferNormalSyncForTrustedSnapshot(decision, "autoheal_normal_sync") {
				return nil
			}
			m.Node.setSyncAction("autoheal_normal_sync", decision.LagBlocks, "normal_sync")
			if decision.TargetHeight > decision.LocalHeight {
				_ = m.runCommitCacheRecoveryIfStalled(decision)
				m.Node.maybeSyncToBestObservedHeight("autoheal_normal_sync")
			}
			return nil
		case AutoHealActionSnapshotSync:
			if decision.TargetHeight > decision.LocalHeight && decision.LagBlocks <= m.Config.NormalSyncLagBlocks {
				_ = m.runCommitCacheRecoveryIfStalled(decision)
				if m.commitRecoveryReachedTarget(decision) {
					return nil
				}
				if m.trySmallLagSyncStateResetCatchup(decision, "autoheal_small_lag_normal_sync") {
					return nil
				}
				if m.shouldEscalateSmallLagSnapshot(decision, "autoheal_small_lag_normal_sync") {
					return m.forceSmallLagSnapshotSync(decision, "autoheal_small_lag_snapshot_escalation")
				}
				m.Node.closeSnapshotSession(false, "small_lag_normal_sync_policy")
				m.Node.setSyncAction("autoheal_normal_sync", decision.LagBlocks, "normal_sync")
				m.Node.maybeSyncToBestObservedHeight("autoheal_small_lag_normal_sync")
				return nil
			}
			return m.startSnapshotSync(decision, "autoheal_lag_snapshot")
		case AutoHealActionDiagnostics:
			m.logDiagnostics(decision)
			_ = m.runCommitCacheRecoveryIfStalled(decision)
			if decision.TargetHeight > decision.LocalHeight && decision.LagBlocks <= m.Config.NormalSyncLagBlocks {
				if !m.trySmallLagMissingBlockCatchup(decision, "autoheal_diagnostics_catchup") {
					if !m.trySmallLagSyncStateResetCatchup(decision, "autoheal_diagnostics_small_lag") &&
						m.shouldEscalateSmallLagSnapshot(decision, "autoheal_diagnostics_small_lag") {
						return m.forceSmallLagSnapshotSync(decision, "autoheal_diagnostics_small_lag_snapshot")
					}
				}
			}
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
	if autoHealActionMutatesChainData(decision.Action) || m.peerIsolationNeedsSnapshot(decision) {
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
	decision = m.refreshDecisionTarget(decision)
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight {
		return fmt.Errorf("snapshot target unavailable: local=%d target=%d", decision.LocalHeight, decision.TargetHeight)
	}
	if m.trySmallLagRangeCatchup(decision, reason) {
		return nil
	}
	if decision.LagBlocks > 0 && decision.LagBlocks <= m.Config.NormalSyncLagBlocks &&
		m.trySmallLagSyncStateResetCatchup(decision, reason+"_state_reset") {
		return nil
	}
	if decision.LagBlocks > 0 && decision.LagBlocks <= m.Config.NormalSyncLagBlocks {
		if m.shouldEscalateSmallLagSnapshot(decision, reason) {
			return m.forceSmallLagSnapshotSync(decision, reason+"_small_lag_snapshot")
		}
		m.Node.closeSnapshotSession(false, "small_lag_normal_sync_policy")
		m.Node.setSyncAction("autoheal_normal_sync", decision.LagBlocks, "normal_sync")
		m.Node.maybeSyncToBestObservedHeight("autoheal_small_lag_normal_sync")
		return nil
	}
	m.beginRecoveryStage(decision, autoHealRecoveryStageTrustedSnapshot, reason)
	m.Node.setSyncAction("autoheal_snapshot_sync", decision.LagBlocks, "snapshot_sync")
	m.Node.forceSnapshotSyncToHeight(decision.TargetHeight, reason)
	m.noteRecoverySnapshotHash(m.Node.runtimeStatusSnapshot().SnapshotHash)
	return nil
}

func (m *AutoHealManager) shouldEscalateSmallLagSnapshot(decision AutoHealDecision, reason string) bool {
	if m == nil || m.Node == nil || decision.TargetHeight <= decision.LocalHeight ||
		decision.LagBlocks == 0 || decision.LagBlocks > m.Config.NormalSyncLagBlocks {
		return false
	}
	if pending, _ := m.trustedSnapshotRecoveryPending(decision, RuntimeStatusSnapshot{}); pending {
		return true
	}
	reasonLower := strings.ToLower(strings.TrimSpace(reason + " " + decision.Reason))
	if strings.Contains(reasonLower, "snapshot_escalation") ||
		strings.Contains(reasonLower, "state_root_mismatch") ||
		strings.Contains(reasonLower, "execution_snapshot_ledger_unavailable") ||
		strings.Contains(reasonLower, "stuck_network_ahead") {
		return true
	}
	if decision.StuckFor >= m.Config.DiagnosticsStuckAfter {
		return true
	}
	runtime := m.Node.runtimeStatusSnapshot()
	return runtime.Syncing &&
		(strings.EqualFold(runtime.BlockProductionStatus, "stalled") ||
			strings.EqualFold(runtime.NetworkHealth, "block_stalled") ||
			strings.EqualFold(runtime.NetworkHealth, "behind") ||
			autoHealDetectorUnhealthy(autoHealDetectorMode(runtime), runtime))
}

func (m *AutoHealManager) forceSmallLagSnapshotSync(decision AutoHealDecision, reason string) error {
	if m == nil || m.Node == nil {
		return errors.New("autoheal manager unavailable")
	}
	decision = m.refreshDecisionTarget(decision)
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_small_lag_snapshot_escalation"
	}
	log.Printf("[AUTOHEAL-MANAGER] small_lag_snapshot_escalation local=%d target=%d lag=%d reason=%s",
		decision.LocalHeight, decision.TargetHeight, decision.LagBlocks, reason)
	m.beginRecoveryStage(decision, autoHealRecoveryStageTrustedSnapshot, reason)
	m.Node.setSyncAction("autoheal_snapshot_sync", decision.LagBlocks, "snapshot_sync")
	m.Node.forceSnapshotSyncToHeight(decision.TargetHeight, reason)
	m.noteRecoverySnapshotHash(m.Node.runtimeStatusSnapshot().SnapshotHash)
	return nil
}

func (m *AutoHealManager) refreshDecisionTarget(decision AutoHealDecision) AutoHealDecision {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return decision
	}
	localHeight := m.Node.Blockchain.Height()
	if localHeight > 0 {
		decision.LocalHeight = localHeight
	}
	target := decision.TargetHeight
	runtime := m.Node.runtimeStatusSnapshot()
	if runtime.NetworkBestHeight > target {
		target = runtime.NetworkBestHeight
	}
	if runtime.SyncTarget > target {
		target = runtime.SyncTarget
	}
	if target > decision.TargetHeight {
		decision.TargetHeight = target
	}
	if decision.TargetHeight > decision.LocalHeight {
		decision.LagBlocks = decision.TargetHeight - decision.LocalHeight
	} else {
		decision.LagBlocks = 0
	}
	return decision
}

func (m *AutoHealManager) trySmallLagRangeCatchup(decision AutoHealDecision, reason string) bool {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return false
	}
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight {
		return false
	}
	if decision.LagBlocks == 0 || decision.LagBlocks > m.rangeCatchupLimit() {
		return false
	}
	localHeight := m.Node.Blockchain.Height()
	if localHeight >= decision.TargetHeight {
		return m.finishSmallLagRangeCatchup(decision.TargetHeight, reason)
	}
	if m.trySmallLagMissingBlockCatchup(decision, reason) {
		return true
	}
	return m.tryRangeCatchupOnly(decision, reason)
}

func (m *AutoHealManager) rangeCatchupLimit() uint64 {
	if m == nil {
		return DefaultAutoHealConfig().SnapshotSyncLagBlocks
	}
	limit := m.Config.SnapshotSyncLagBlocks
	if limit < m.Config.NormalSyncLagBlocks {
		limit = m.Config.NormalSyncLagBlocks
	}
	if limit == 0 {
		limit = DefaultAutoHealConfig().SnapshotSyncLagBlocks
	}
	return limit
}

func (m *AutoHealManager) tryRangeCatchupOnly(decision AutoHealDecision, reason string) bool {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return false
	}
	if decision.TargetHeight == 0 {
		return false
	}
	localHeight := m.Node.Blockchain.Height()
	if localHeight >= decision.TargetHeight {
		return m.finishSmallLagRangeCatchup(decision.TargetHeight, reason)
	}
	decision.LocalHeight = localHeight
	decision.LagBlocks = decision.TargetHeight - localHeight
	if m.deferDirectCatchupForTrustedSnapshot(decision, RuntimeStatusSnapshot{}, reason) {
		return false
	}
	m.beginRecoveryStage(decision, autoHealRecoveryStageRangeCatchup, reason)
	m.Node.setSyncAction("autoheal_range_fetch", decision.TargetHeight-localHeight, "range_fetch")
	log.Printf("[AUTOHEAL-MANAGER] range_catchup start local=%d target=%d lag=%d reason=%s",
		localHeight, decision.TargetHeight, decision.TargetHeight-localHeight, strings.TrimSpace(reason))
	catchup := m.rangeCatchup
	if catchup == nil {
		catchup = func(node *Node, target uint64, stage string) bool {
			return node.syncBlocksFromPeersWithStage(target, syncRangeFetchMaxBlocks(), false, stage)
		}
	}
	reached, timedOut := m.runRangeCatchupWithTimeout(catchup, decision.TargetHeight, "autoheal_range_fetch", decision.TargetHeight-localHeight)
	if timedOut {
		if _, provider := m.Node.syncDiagnosticContext(); strings.TrimSpace(provider) != "" {
			m.Node.setSyncAvoidProviderOnce(provider)
		}
		log.Printf("[AUTOHEAL-MANAGER] range_catchup timeout local=%d target=%d timeout=%s reason=%s",
			m.Node.Blockchain.Height(), decision.TargetHeight, m.rangeCatchupTimeout(decision.TargetHeight-localHeight), strings.TrimSpace(reason))
		m.failRecoveryStage(autoHealRecoveryStageRangeCatchup, "timeout_"+reason)
		return false
	}
	if !reached {
		log.Printf("[AUTOHEAL-MANAGER] range_catchup failed local=%d target=%d reason=%s",
			m.Node.Blockchain.Height(), decision.TargetHeight, strings.TrimSpace(reason))
		m.failRecoveryStage(autoHealRecoveryStageRangeCatchup, "failed_"+reason)
		return false
	}
	if m.Node.Blockchain.Height() < decision.TargetHeight {
		log.Printf("[AUTOHEAL-MANAGER] range_catchup incomplete local=%d target=%d reason=%s",
			m.Node.Blockchain.Height(), decision.TargetHeight, strings.TrimSpace(reason))
		m.failRecoveryStage(autoHealRecoveryStageRangeCatchup, "incomplete_"+reason)
		return false
	}
	return m.finishSmallLagRangeCatchup(decision.TargetHeight, reason)
}

func (m *AutoHealManager) trySmallLagSyncStateResetCatchup(decision AutoHealDecision, reason string) bool {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return false
	}
	decision = m.refreshDecisionTarget(decision)
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight ||
		decision.LagBlocks == 0 || decision.LagBlocks > m.Config.NormalSyncLagBlocks {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_small_lag_sync_state_reset"
	}
	if m.deferDirectCatchupForTrustedSnapshot(decision, RuntimeStatusSnapshot{}, reason) {
		return false
	}
	if !m.Node.resetSmallLagSyncStall(decision.TargetHeight, reason) {
		return false
	}
	decision = m.refreshDecisionTarget(decision)
	if decision.TargetHeight <= decision.LocalHeight || decision.LagBlocks == 0 {
		return m.finishSmallLagRangeCatchup(decision.TargetHeight, reason)
	}
	if m.trySmallLagMissingBlockCatchup(decision, reason+"_exact") {
		return true
	}
	return m.tryRangeCatchupOnly(decision, reason+"_range")
}

func (m *AutoHealManager) rangeCatchupTimeout(lag uint64) time.Duration {
	if m == nil {
		return DefaultAutoHealConfig().RangeCatchupTimeout
	}
	timeout := m.Config.RangeCatchupTimeout
	if timeout <= 0 {
		timeout = DefaultAutoHealConfig().RangeCatchupTimeout
	}
	if lag > 128 {
		timeout += 8 * time.Second
	}
	return timeout
}

func (m *AutoHealManager) runRangeCatchupWithTimeout(
	catchup func(*Node, uint64, string) bool,
	target uint64,
	stage string,
	lag uint64,
) (bool, bool) {
	if m == nil || m.Node == nil || catchup == nil {
		return false, false
	}
	resultCh := make(chan bool, 1)
	go func() {
		resultCh <- catchup(m.Node, target, stage)
	}()
	timer := time.NewTimer(m.rangeCatchupTimeout(lag))
	defer timer.Stop()
	select {
	case ok := <-resultCh:
		return ok, false
	case <-timer.C:
		return false, true
	}
}

func (m *AutoHealManager) trySmallLagMissingBlockCatchup(decision AutoHealDecision, reason string) bool {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return false
	}
	if decision.TargetHeight == 0 || decision.TargetHeight <= decision.LocalHeight ||
		decision.LagBlocks == 0 {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_missing_block"
	}
	if m.deferDirectCatchupForTrustedSnapshot(decision, RuntimeStatusSnapshot{}, reason) {
		return false
	}
	catchup := m.missingBlockCatchup
	m.beginRecoveryStage(decision, autoHealRecoveryStageExactBlock, reason)
	recovered := 0
	for recovered < autoHealExactCatchupMaxPerTick {
		localHeight := m.Node.Blockchain.Height()
		if localHeight >= decision.TargetHeight {
			return m.finishSmallLagRangeCatchup(decision.TargetHeight, reason)
		}
		nextHeight := localHeight + 1
		if nextHeight == 0 || nextHeight > decision.TargetHeight {
			return false
		}
		m.Node.setSyncAction("autoheal_missing_block", decision.TargetHeight-localHeight, "missing_block")
		m.Node.clearStalledConsensusScratch(nextHeight, reason+"_pre_exact_missing")
		log.Printf("[AUTOHEAL-MANAGER] missing_block_catchup start local=%d next=%d target=%d reason=%s",
			localHeight, nextHeight, decision.TargetHeight, reason)
		advanced, timedOut := m.runMissingBlockCatchupWithTimeout(catchup, nextHeight, reason+"_missing_block")
		if timedOut {
			if _, provider := m.Node.syncDiagnosticContext(); strings.TrimSpace(provider) != "" {
				m.Node.setSyncAvoidProviderOnce(provider)
			}
			log.Printf("[AUTOHEAL-MANAGER] missing_block_catchup timeout local=%d next=%d target=%d timeout=%s reason=%s",
				localHeight, nextHeight, decision.TargetHeight, m.Config.ExactCatchupTimeout, reason)
			m.failRecoveryStage(autoHealRecoveryStageExactBlock, "timeout_"+reason)
			return false
		}
		m.Node.ProcessQueuedBlocks()
		_ = m.Node.recoverSignedCommitQuorumAtCurrentHeight(reason + "_signed_commit")
		if m.Node.Blockchain.Height() <= localHeight {
			log.Printf("[AUTOHEAL-MANAGER] missing_block_catchup no_progress local=%d next=%d target=%d advanced=%t reason=%s",
				localHeight, nextHeight, decision.TargetHeight, advanced, reason)
			m.failRecoveryStage(autoHealRecoveryStageExactBlock, "no_progress_"+reason)
			return false
		}
		log.Printf("[AUTOHEAL-MANAGER] missing_block_catchup advanced local=%d target=%d reason=%s",
			m.Node.Blockchain.Height(), decision.TargetHeight, reason)
		recovered++
	}
	log.Printf("[AUTOHEAL-MANAGER] missing_block_catchup paused local=%d target=%d recovered=%d reason=%s",
		m.Node.Blockchain.Height(), decision.TargetHeight, recovered, reason)
	return m.Node.Blockchain.Height() >= decision.TargetHeight &&
		m.finishSmallLagRangeCatchup(decision.TargetHeight, reason)
}

func (m *AutoHealManager) runMissingBlockCatchupWithTimeout(
	catchup func(*Node, uint64, string) bool,
	height uint64,
	trigger string,
) (bool, bool) {
	if m == nil || m.Node == nil {
		return false, false
	}
	timeout := m.Config.ExactCatchupTimeout
	if timeout <= 0 {
		timeout = DefaultAutoHealConfig().ExactCatchupTimeout
	}
	if catchup == nil {
		return m.Node.recoverMissingBlockFromPeersBounded(height, trigger, timeout)
	}
	resultCh := make(chan bool, 1)
	go func() {
		resultCh <- catchup(m.Node, height, trigger)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ok := <-resultCh:
		return ok, false
	case <-timer.C:
		return false, true
	}
}

func (m *AutoHealManager) finishSmallLagRangeCatchup(targetHeight uint64, reason string) bool {
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return false
	}
	localHeight := m.Node.Blockchain.Height()
	if targetHeight == 0 || localHeight < targetHeight {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_range_fetch"
	}
	decision := AutoHealDecision{LocalHeight: localHeight, TargetHeight: targetHeight, LagBlocks: 0}
	m.beginRecoveryStage(decision, autoHealRecoveryStageExecutionVerify, reason)
	gate := m.Node.recoveryVotingRejoinGate(localHeight)
	if !gate.Ready {
		m.Node.setSyncAction("rejoin_verify_failed", 0, gate.Reason)
		m.failRecoveryStage(autoHealRecoveryStageExecutionVerify, gate.Reason)
		log.Printf("[AUTOHEAL-REJOIN-GATE] status=blocked height=%d target=%d reason=%s runtime_ledger=%s execution_ledger=%s state_root=%s registry_hash=%s parent=%s tip=%s",
			localHeight,
			targetHeight,
			strings.TrimSpace(gate.Reason),
			ShortHash(gate.RuntimeLedgerHash),
			ShortHash(gate.ExecutionLedgerHash),
			ShortHash(gate.StateRoot),
			ShortHash(gate.RegistryHash),
			ShortHash(gate.ParentHash),
			ShortHash(gate.TipHash),
		)
		return false
	}
	m.completeRecoveryStage(autoHealRecoveryStageExecutionVerify, reason)
	_ = m.Node.maybeExitSyncMode(reason + "_range_fetch_complete")
	_ = m.Node.advanceConsensusToCommittedTip(reason + "_range_fetch_complete")
	m.Node.setSyncAction("idle", 0, "up_to_date")
	m.Node.setSyncProvider("")
	m.Node.clearSyncResumeState()
	m.Node.persistDurableSyncAnchorAsync(localHeight, reason+"_range_fetch_complete")
	m.Node.replayQueuedExecutionVotes()
	if m.Node.Host != nil {
		m.Node.requestHeartbeatBroadcast(true)
	}
	m.beginRecoveryStage(decision, autoHealRecoveryStageRejoinVoting, reason)
	m.completeRecoveryStage(autoHealRecoveryStageRejoinVoting, reason)
	log.Printf("[AUTOHEAL-MANAGER] range_catchup complete local=%d target=%d reason=%s",
		localHeight, targetHeight, reason)
	return true
}

func (m *AutoHealManager) maybeCompleteRecoveryFromRuntime(runtime RuntimeStatusSnapshot) {
	if m == nil || m.Node == nil || runtime.Height == 0 {
		return
	}
	session := m.RecoverySession()
	if strings.TrimSpace(session.ID) == "" || !strings.EqualFold(session.Result, "running") {
		return
	}
	if session.TargetHeight == 0 || runtime.Height < session.TargetHeight {
		return
	}
	decision := AutoHealDecision{
		LocalHeight:  runtime.Height,
		TargetHeight: session.TargetHeight,
		LagBlocks:    0,
		Reason:       "runtime_target_reached",
	}
	m.beginRecoveryStage(decision, autoHealRecoveryStageExecutionVerify, "runtime_target_reached")
	gate := m.Node.recoveryVotingRejoinGate(runtime.Height)
	if !gate.Ready {
		m.Node.setSyncAction("rejoin_verify_failed", 0, gate.Reason)
		m.failRecoveryStage(autoHealRecoveryStageExecutionVerify, gate.Reason)
		log.Printf("[AUTOHEAL-REJOIN-GATE] status=blocked height=%d target=%d reason=%s runtime_ledger=%s execution_ledger=%s state_root=%s registry_hash=%s parent=%s tip=%s",
			runtime.Height,
			session.TargetHeight,
			strings.TrimSpace(gate.Reason),
			ShortHash(gate.RuntimeLedgerHash),
			ShortHash(gate.ExecutionLedgerHash),
			ShortHash(gate.StateRoot),
			ShortHash(gate.RegistryHash),
			ShortHash(gate.ParentHash),
			ShortHash(gate.TipHash),
		)
		return
	}
	m.completeRecoveryStage(autoHealRecoveryStageExecutionVerify, "runtime_target_reached")
	m.beginRecoveryStage(decision, autoHealRecoveryStageRejoinVoting, "runtime_target_reached")
	m.completeRecoveryStage(autoHealRecoveryStageRejoinVoting, "runtime_target_reached")
}

func (m *AutoHealManager) beginRecoveryStage(decision AutoHealDecision, stage, reason string) AutoHealRecoverySession {
	if m == nil {
		return AutoHealRecoverySession{}
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = autoHealRecoveryStageHealthy
	}
	now := m.now()
	target := decision.TargetHeight
	local := decision.LocalHeight
	if m.Node != nil && m.Node.Blockchain != nil {
		if height := m.Node.Blockchain.Height(); height > 0 {
			local = height
		}
	}
	runtime := RuntimeStatusSnapshot{}
	if autoHealRecoveryDirectCatchupStage(stage) && m.Node != nil {
		runtime = m.Node.runtimeStatusSnapshot()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.recoverySession
	trustedSnapshotHandoff := strings.EqualFold(session.Result, "running") &&
		strings.EqualFold(session.Stage, autoHealRecoveryStageTrustedSnapshot) &&
		autoHealRecoveryDirectCatchupStage(stage) &&
		autoHealTrustedSnapshotBaselineReady(local, runtime, session)
	if strings.EqualFold(session.Result, "running") &&
		strings.EqualFold(session.Stage, autoHealRecoveryStageTrustedSnapshot) &&
		autoHealRecoveryDirectCatchupStage(stage) &&
		!trustedSnapshotHandoff {
		if target > session.TargetHeight {
			session.TargetHeight = target
		}
		session.UpdatedAtUnix = now.Unix()
		m.recoverySession = session
		log.Printf("[AUTOHEAL-SESSION] transition_deferred id=%s from=%s to=%s start=%d target=%d reason=%s",
			session.ID,
			session.Stage,
			stage,
			session.StartHeight,
			session.TargetHeight,
			strings.TrimSpace(reason),
		)
		return session
	}
	newSession := strings.TrimSpace(session.ID) == "" ||
		!strings.EqualFold(session.Result, "running") ||
		(target > 0 && session.TargetHeight != target)
	if newSession {
		m.recoverySeq++
		session = AutoHealRecoverySession{
			ID:            fmt.Sprintf("%d-%d-%d-%d", now.UnixNano(), m.recoverySeq, local, target),
			StartHeight:   local,
			TargetHeight:  target,
			Stage:         stage,
			RetryCount:    1,
			Result:        "running",
			StartedAtUnix: now.Unix(),
			UpdatedAtUnix: now.Unix(),
		}
	} else {
		if !strings.EqualFold(session.Stage, stage) {
			session.Stage = stage
			session.RetryCount = 1
		} else {
			session.RetryCount++
		}
		if target > session.TargetHeight {
			session.TargetHeight = target
		}
		session.Result = "running"
		session.LastError = ""
		session.RejoinReady = false
		session.UpdatedAtUnix = now.Unix()
	}
	m.recoverySession = session
	if trustedSnapshotHandoff && target > local && m.Node != nil {
		m.Node.resetSmallLagSyncStall(target, "trusted_snapshot_baseline_applied")
	}
	log.Printf("[AUTOHEAL-SESSION] id=%s stage=%s start=%d target=%d retry=%d result=%s reason=%s",
		session.ID,
		session.Stage,
		session.StartHeight,
		session.TargetHeight,
		session.RetryCount,
		session.Result,
		strings.TrimSpace(reason),
	)
	return session
}

func (m *AutoHealManager) failRecoveryStage(stage, reason string) {
	if m == nil {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.recoverySession
	if strings.TrimSpace(session.ID) == "" {
		return
	}
	if strings.TrimSpace(stage) != "" {
		session.Stage = strings.TrimSpace(stage)
	}
	session.Result = "failed"
	session.LastError = strings.TrimSpace(reason)
	session.RejoinReady = false
	session.UpdatedAtUnix = now.Unix()
	m.recoverySession = session
	log.Printf("[AUTOHEAL-SESSION] id=%s stage=%s result=failed error=%s",
		session.ID,
		session.Stage,
		session.LastError,
	)
}

func (m *AutoHealManager) completeRecoveryStage(stage, reason string) {
	if m == nil {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.recoverySession
	if strings.TrimSpace(session.ID) == "" {
		return
	}
	if strings.TrimSpace(stage) != "" {
		session.Stage = strings.TrimSpace(stage)
	}
	if strings.EqualFold(session.Stage, autoHealRecoveryStageRejoinVoting) {
		session.Result = "complete"
	} else {
		session.Result = "running"
	}
	session.LastError = ""
	session.RejoinReady = strings.EqualFold(session.Stage, autoHealRecoveryStageRejoinVoting) || session.RejoinReady
	session.UpdatedAtUnix = now.Unix()
	m.recoverySession = session
	log.Printf("[AUTOHEAL-SESSION] id=%s stage=%s result=%s rejoin_ready=%t reason=%s",
		session.ID,
		session.Stage,
		session.Result,
		session.RejoinReady,
		strings.TrimSpace(reason),
	)
}

func (m *AutoHealManager) noteRecoverySnapshotHash(hash string) {
	if m == nil {
		return
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.recoverySession
	if strings.TrimSpace(session.ID) == "" {
		return
	}
	session.SnapshotHash = hash
	session.UpdatedAtUnix = now.Unix()
	m.recoverySession = session
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
	if m.runCommitCacheRecoveryIfStalled(decision) {
		if m.commitRecoveryReachedTarget(decision) {
			return nil
		}
	}
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
	// A quorum-loss report can be caused by a local node missing an already
	// finalized block. Recover that block before escalating to snapshot or DB
	// repair, even when stale heartbeats do not expose a higher target height.
	if m.runCommitCacheRecoveryIfStalled(decision) {
		if m.commitRecoveryReachedTarget(decision) {
			return nil
		}
	}
	if decision.TargetHeight > decision.LocalHeight {
		if m.peerIsolationNeedsSnapshot(decision) {
			return m.startSnapshotSync(decision, "autoheal_peer_isolation_snapshot")
		}
		if m.trySmallLagMissingBlockCatchup(decision, "autoheal_peer_isolation_catchup") {
			return nil
		}
		m.Node.maybeSyncToBestObservedHeight("autoheal_peer_isolation")
	}
	return nil
}

func (m *AutoHealManager) commitRecoveryReachedTarget(decision AutoHealDecision) bool {
	if decision.TargetHeight <= decision.LocalHeight {
		return true
	}
	if m == nil || m.Node == nil || m.Node.Blockchain == nil {
		return false
	}
	return m.Node.Blockchain.Height() >= decision.TargetHeight
}

func (m *AutoHealManager) peerIsolationNeedsSnapshot(decision AutoHealDecision) bool {
	if m == nil || decision.Action != AutoHealActionPeerIsolation {
		return false
	}
	return decision.TargetHeight > decision.LocalHeight &&
		decision.LagBlocks > m.Config.SnapshotSyncLagBlocks
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
	log.Printf("[AUTOHEAL-MANAGER] level=%s action=%s reason=%s height=%d finalized=%d target=%d lag=%d stuck=%s peers=%d quorum=%d/%d block_status=%s network=%s mempool=%d sync_stage=%s snapshot=%d:%s",
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
		diag.MempoolDepth,
		diag.SyncStage,
		diag.SnapshotHeight,
		ShortHash(diag.SnapshotHash),
	)
}

func (m *AutoHealManager) runCommitCacheRecoveryIfStalled(decision AutoHealDecision) bool {
	if m == nil || m.Node == nil || !autoHealDecisionAllowsCommitRecovery(decision, m.Config) {
		return false
	}
	if !m.claimCommitRecoveryAttempt(decision.LocalHeight) {
		// A prior recovery at this height is still settling. Treat the recovery
		// stage as handled so automatic repair does not race ahead to DB repair.
		return true
	}
	// `reason` stores the value produced by this operation.
	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = string(decision.Action)
	}
	return m.Node.recoverFinalizedCommitOrSoftRestart("autoheal_" + reason)
}

func (m *AutoHealManager) claimCommitRecoveryAttempt(height uint64) bool {
	if m == nil || height == 0 {
		return false
	}
	now := m.now()
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastCommitRecoveryHeight == height &&
		!m.lastCommitRecoveryAt.IsZero() &&
		now.Sub(m.lastCommitRecoveryAt) < m.Config.CommitRecoveryCooldown {
		return false
	}
	m.lastCommitRecoveryHeight = height
	m.lastCommitRecoveryAt = now
	return true
}

func autoHealDecisionAllowsCommitRecovery(decision AutoHealDecision, config AutoHealConfig) bool {
	if decision.LocalHeight == 0 {
		return false
	}
	if decision.Action == AutoHealActionDiagnostics || decision.Action == AutoHealActionAutomaticRepair {
		return true
	}
	if decision.Action == AutoHealActionNormalSync && decision.TargetHeight > decision.LocalHeight {
		if decision.StuckFor >= config.DiagnosticsStuckAfter {
			return true
		}
		return strings.Contains(strings.ToLower(strings.TrimSpace(decision.Reason)), "stalled")
	}
	if decision.Action == AutoHealActionSnapshotSync &&
		decision.TargetHeight > decision.LocalHeight &&
		decision.LagBlocks <= config.NormalSyncLagBlocks {
		return true
	}
	if decision.Action == AutoHealActionPeerIsolation &&
		decision.StuckFor >= config.DiagnosticsStuckAfter &&
		(decision.TargetHeight <= decision.LocalHeight || decision.LagBlocks <= config.NormalSyncLagBlocks) {
		return true
	}
	return false
}

func (n *Node) recoverFinalizedCommitOrSoftRestart(reason string) bool {
	if n == nil || n.Blockchain == nil || n.isShuttingDown() {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_commit_recovery"
	}
	// `before` stores the value produced by this operation.
	before := n.Blockchain.Height()
	if before == 0 {
		return false
	}

	if n.recoverSignedCommitQuorumAtCurrentHeight(reason) {
		if n.Blockchain.Height() > before {
			_ = n.advanceConsensusToCommittedTip(reason + "_signed_commit")
			n.schedulePostCommitConsensusDrain(n.Blockchain.Height())
			return true
		}
	}

	n.ProcessQueuedBlocks()
	if n.Blockchain.Height() > before {
		_ = n.advanceConsensusToCommittedTip(reason + "_queued_block")
		n.schedulePostCommitConsensusDrain(n.Blockchain.Height())
		return true
	}

	// If this node missed the already-finalized next block, fetch exactly the
	// next height and apply it through the normal ReceiveBlock path.
	nextHeight := before + 1
	if nextHeight > before && n.recoverMissingBlockFromPeersWithCooldown(nextHeight, reason) {
		if n.Blockchain.Height() > before {
			_ = n.advanceConsensusToCommittedTip(reason + "_peer_finalized_block")
			n.schedulePostCommitConsensusDrain(n.Blockchain.Height())
			return true
		}
	}

	return n.softRestartConsensusEngine(reason)
}

func (n *Node) recoverMissingBlockFromPeersWithCooldown(missingHeight uint64, reason string) bool {
	if n == nil || n.Host == nil || missingHeight == 0 {
		return false
	}
	if n.Blockchain != nil && n.Blockchain.Height() >= missingHeight {
		return true
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.syncMu.Lock()
	if !shouldStartMissingBlockRecovery(now, n.lastMissingBlockRequestAt, n.missingBlockRecoveryInFlight, n.lastMissingBlockHeight, missingHeight) {
		n.syncMu.Unlock()
		return false
	}
	n.lastMissingBlockRequestAt = now
	n.lastMissingBlockHeight = missingHeight
	n.missingBlockRecoveryInFlight = true
	n.syncMu.Unlock()
	defer func() {
		n.syncMu.Lock()
		if n.lastMissingBlockHeight == missingHeight {
			n.missingBlockRecoveryInFlight = false
		}
		n.syncMu.Unlock()
	}()

	n.clearStalledConsensusScratch(missingHeight, strings.TrimSpace(reason)+"_pre_exact_missing")
	recovered, timedOut := n.recoverMissingBlockFromPeersBounded(missingHeight, reason, DefaultAutoHealConfig().ExactCatchupTimeout)
	if timedOut {
		if _, provider := n.syncDiagnosticContext(); strings.TrimSpace(provider) != "" {
			n.setSyncAvoidProviderOnce(provider)
		}
		log.Printf("[MISSING-BLOCK-RECOVERY] timeout height=%d reason=%s", missingHeight, strings.TrimSpace(reason))
	}
	if recovered {
		return n.Blockchain != nil && n.Blockchain.Height() >= missingHeight
	}
	n.maybeSyncToBestObservedHeight("autoheal_commit_recovery_" + reason)
	return false
}

func (n *Node) softRestartConsensusEngine(reason string) bool {
	if n == nil || n.Blockchain == nil || n.isShuttingDown() || !ResultGossipOnly {
		return false
	}
	if n.consensusRecomputePauseActive() {
		return false
	}
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.Blockchain.Height()
	if committedHeight == 0 {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_soft_restart"
	}

	_ = n.advanceConsensusToCommittedTip(reason + "_advance_tip")
	_ = n.maybeExitSyncMode(reason + "_exit_sync")
	nextHeight := committedHeight + 1
	n.clearStalledConsensusScratch(nextHeight, reason)
	n.hardResetConsensus(nextHeight)
	n.startNextRoundImmediatelyWithReason(nextHeight, n.currentExecutionLedgerClone(), reason+"_soft_restart")
	n.schedulePostCommitConsensusDrain(committedHeight)
	log.Printf("[AUTOHEAL-COMMIT-RECOVERY] action=soft_restart height=%d next=%d reason=%s",
		committedHeight,
		nextHeight,
		reason,
	)
	return true
}

func (n *Node) resetSmallLagSyncStall(targetHeight uint64, reason string) bool {
	if n == nil || n.Blockchain == nil || n.Consensus == nil || targetHeight == 0 {
		return false
	}
	localHeight := n.Blockchain.Height()
	if targetHeight <= localHeight {
		_ = n.maybeExitSyncMode(strings.TrimSpace(reason) + "_already_caught_up")
		return false
	}
	if targetHeight-localHeight > DefaultAutoHealConfig().NormalSyncLagBlocks {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_small_lag_sync_state_reset"
	}

	snapshotActive := n.snapshotSessionActive()
	if snapshotActive {
		n.closeSnapshotSession(false, reason+"_close_stale_snapshot_session")
		n.clearSnapshotSessionState()
	}
	n.clearSyncResumeState()
	n.setSyncAction("autoheal_state_reset", targetHeight-localHeight, "state_reset")
	n.setSyncProvider("")

	n.syncMu.Lock()
	n.lastSyncAttempt = time.Time{}
	n.syncLastProgressAt = time.Now()
	n.syncStallSeconds = 0
	n.syncRangeUnavailableStreak = 0
	n.missingBlockRecoveryInFlight = false
	n.lastMissingBlockRequestAt = time.Time{}
	n.syncAvoidProvider = ""
	n.syncAvoidProviderOnce = false
	n.syncMu.Unlock()

	n.Consensus.mu.Lock()
	wasSyncing := n.Consensus.Syncing
	wasPaused := n.Consensus.Paused
	wasInFlight := n.Consensus.syncInFlight
	previousTarget := n.Consensus.SyncTarget
	n.Consensus.syncInFlight = false
	n.Consensus.Syncing = true
	n.Consensus.Paused = true
	n.Consensus.SyncTarget = targetHeight
	n.Consensus.mu.Unlock()

	n.clearStalledConsensusScratch(localHeight+1, reason+"_scratch")
	log.Printf("[AUTOHEAL-SYNC-RESET] local=%d target=%d reason=%s snapshot_active=%t syncing=%t paused=%t in_flight=%t previous_target=%d",
		localHeight,
		targetHeight,
		reason,
		snapshotActive,
		wasSyncing,
		wasPaused,
		wasInFlight,
		previousTarget,
	)
	return true
}

func (n *Node) clearStalledConsensusScratch(height uint64, reason string) {
	if n == nil || height == 0 {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "autoheal_soft_restart"
	}

	n.clearImmediateRoundStart(height)
	n.clearAcceptedProposal(height)
	n.clearLeaderBlock(height)

	n.seenBlockMu.Lock()
	n.SeenBlockHashes = make(map[string]bool)
	n.seenBlockQueue = nil
	n.seenBlockHead = 0
	n.seenBlockMu.Unlock()

	n.forkMu.Lock()
	if n.ForkBlocks != nil {
		delete(n.ForkBlocks, height)
	}
	n.forkMu.Unlock()

	n.leaderMu.Lock()
	if n.queuedFutureLeaderBlocks != nil {
		delete(n.queuedFutureLeaderBlocks, height)
	}
	if n.lastLeaderEpoch == height {
		n.lastLeaderEpoch = 0
		n.lastLeaderRound = 0
		n.lastLeaderSlot = 0
	}
	n.leaderMu.Unlock()

	n.execResultsMu.Lock()
	heightKey := fmt.Sprintf("%d", height)
	if n.execResults != nil {
		for key := range n.execResults {
			if h, ok := parseHeightPrefix(key); ok && h == height {
				delete(n.execResults, key)
			}
		}
	}
	if n.pendingBlocks != nil {
		for key, block := range n.pendingBlocks {
			if block.ID == height {
				delete(n.pendingBlocks, key)
			}
		}
	}
	if n.queuedExecVotes != nil {
		delete(n.queuedExecVotes, heightKey)
	}
	if n.execBroadcasted != nil {
		delete(n.execBroadcasted, height)
	}
	if n.execSignerSeen != nil {
		delete(n.execSignerSeen, height)
	}
	if n.execBroadcastedByValidator != nil {
		delete(n.execBroadcastedByValidator, height)
	}
	if n.localExecVoteByRound != nil {
		delete(n.localExecVoteByRound, height)
	}
	if n.acceptedProposal != nil {
		delete(n.acceptedProposal, acceptedProposalHeightKey(height))
	}
	if n.quorumLockedProposal != nil {
		delete(n.quorumLockedProposal, acceptedProposalHeightKey(height))
	}
	if n.acceptedProposalBlocks != nil {
		for key, block := range n.acceptedProposalBlocks {
			if block.ID == height {
				delete(n.acceptedProposalBlocks, key)
			}
		}
	}
	n.execResultsMu.Unlock()

	n.execVoteGuardMu.Lock()
	if n.execVoteSeen != nil {
		for key := range n.execVoteSeen {
			if h, ok := parseHeightPrefix(key); ok && h == height {
				delete(n.execVoteSeen, key)
			}
		}
	}
	n.execVoteLimiter = nil
	if n.execMismatch != nil {
		for signer, tracker := range n.execMismatch {
			if tracker.LastEpoch == height {
				delete(n.execMismatch, signer)
			}
		}
	}
	n.execVoteGuardMu.Unlock()

	n.execRebroadcastMu.Lock()
	if n.execRebroadcastAt != nil {
		delete(n.execRebroadcastAt, height)
	}
	if n.execRebroadcastState != nil {
		delete(n.execRebroadcastState, height)
	}
	n.execRebroadcastMu.Unlock()

	n.commitMu.Lock()
	if n.commitVotes != nil {
		delete(n.commitVotes, height)
	}
	if n.commitVoted != nil {
		delete(n.commitVoted, height)
	}
	if n.commitVoteSignatures != nil {
		delete(n.commitVoteSignatures, height)
	}
	n.commitMu.Unlock()

	n.invalidProposerMu.Lock()
	if n.invalidProposerSeen != nil {
		delete(n.invalidProposerSeen, height)
	}
	if n.invalidProposerStrikes != nil {
		for key, tracker := range n.invalidProposerStrikes {
			if tracker.LastEpoch == height {
				delete(n.invalidProposerStrikes, key)
			}
		}
	}
	if n.invalidProposerPeerStrikes != nil {
		for key, tracker := range n.invalidProposerPeerStrikes {
			if tracker.LastEpoch == height {
				delete(n.invalidProposerPeerStrikes, key)
			}
		}
	}
	n.invalidProposerMu.Unlock()

	clearExecPoolAtHeight(height)
	log.Printf("[AUTOHEAL-CONSENSUS-SCRATCH-CLEAR] height=%d reason=%s", height, reason)
}

func clearExecPoolAtHeight(height uint64) {
	if height == 0 {
		return
	}
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()
	delete(ExecPool.pool, height)
	delete(ExecPool.txMerkle, height)
	delete(ExecPool.frozen, height)
	delete(ExecPool.signers, height)
	delete(ExecPool.choice, height)
	delete(ExecPool.epochChoice, height)
	delete(ExecPool.commitChoice, height)
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

func (m *AutoHealManager) RecoverySession() AutoHealRecoverySession {
	if m == nil {
		return AutoHealRecoverySession{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recoverySession
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
		MempoolDepth:               runtime.MempoolDepth,
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
	session := manager.RecoverySession()
	out.RecoverySessionID = strings.TrimSpace(session.ID)
	out.RecoveryStage = strings.TrimSpace(session.Stage)
	out.RecoveryRetryCount = session.RetryCount
	out.RecoveryStartHeight = session.StartHeight
	out.RecoveryTargetHeight = session.TargetHeight
	out.RecoverySnapshotHash = strings.TrimSpace(session.SnapshotHash)
	out.RecoveryResult = strings.TrimSpace(session.Result)
	out.RecoveryLastError = strings.TrimSpace(session.LastError)
	out.RecoveryUpdatedAtUnix = session.UpdatedAtUnix
	out.RecoveryRejoinReady = session.RejoinReady
	// A configured validator remains in the committed validator set while
	// recovery runs, but must behave as an observer until catch-up and state
	// verification reach the explicit rejoin-voting stage.
	if normalizeNodeRole(n.Role) == "validator" && autoHealRecoveryRequiresObserver(session) {
		out.ValidatorState = "recovering"
		out.VoteEnabled = false
		out.ProposeEnabled = false
		out.ConsensusMode = "observer"
		out.Ready = false
		out.WaitReason = "recovering_" + strings.TrimSpace(session.Stage)
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

func autoHealRecoveryRequiresObserver(session AutoHealRecoverySession) bool {
	stage := strings.ToLower(strings.TrimSpace(session.Stage))
	if strings.TrimSpace(session.ID) == "" || stage == "" || stage == autoHealRecoveryStageHealthy {
		return false
	}
	return !(strings.EqualFold(strings.TrimSpace(session.Result), "complete") &&
		session.RejoinReady && stage == autoHealRecoveryStageRejoinVoting)
}

func (n *Node) applyRecoveryRejoinRuntimeStatus(out *RuntimeStatusSnapshot, configuredValidator bool) {
	if out == nil {
		return
	}
	out.VotingRejoinReady = true
	if n == nil || out.Height == 0 {
		out.ExecutionDivergenceReason = "genesis_or_empty"
		return
	}
	gate := n.recoveryVotingRejoinGate(out.Height)
	out.ExecutionLedgerHash = strings.TrimSpace(gate.ExecutionLedgerHash)
	out.RuntimeLedgerHash = strings.TrimSpace(gate.RuntimeLedgerHash)
	out.StateRoot = strings.TrimSpace(gate.StateRoot)
	out.RegistryHash = strings.TrimSpace(gate.RegistryHash)
	out.ParentHash = strings.TrimSpace(gate.ParentHash)
	out.TipHash = strings.TrimSpace(gate.TipHash)
	out.VotingRejoinReady = gate.Ready
	if gate.Ready {
		out.ExecutionDivergence = false
		out.ExecutionDivergenceReason = "verified"
		return
	}
	out.ExecutionDivergence = true
	out.ExecutionDivergenceReason = strings.TrimSpace(gate.Reason)
	if out.ExecutionDivergenceReason == "" {
		out.ExecutionDivergenceReason = "rejoin_gate_failed"
	}
	if configuredValidator {
		out.ExecutionReady = false
		if strings.TrimSpace(out.ExecutionWaitReason) == "" || strings.EqualFold(out.ExecutionWaitReason, "ready") {
			out.ExecutionWaitReason = "rejoin_gate_" + out.ExecutionDivergenceReason
		}
	}
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
