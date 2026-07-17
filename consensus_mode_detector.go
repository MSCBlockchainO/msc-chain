package main

import (
	"fmt"
	"strings"
	"time"
)

type ConsensusDetectorMode string

const (
	// `ConsensusDetectorNormal` defines the constant value used by this package.
	ConsensusDetectorNormal ConsensusDetectorMode = "NORMAL"
	// `ConsensusDetectorStrict` defines the constant value used by this package.
	ConsensusDetectorStrict ConsensusDetectorMode = "STRICT"
	// `ConsensusDetectorRecovery` defines the constant value used by this package.
	ConsensusDetectorRecovery ConsensusDetectorMode = "RECOVERY"
	// `ConsensusDetectorDegraded` defines the constant value used by this package.
	ConsensusDetectorDegraded ConsensusDetectorMode = "DEGRADED"
	// `ConsensusDetectorEmergency` defines the constant value used by this package.
	ConsensusDetectorEmergency ConsensusDetectorMode = "EMERGENCY"
	// `ConsensusDetectorHalted` defines the constant value used by this package.
	ConsensusDetectorHalted ConsensusDetectorMode = "HALTED"
	// `ConsensusDetectorPartition` defines the constant value used by this package.
	ConsensusDetectorPartition ConsensusDetectorMode = "PARTITION"
	// `ConsensusDetectorAttack` defines the constant value used by this package.
	ConsensusDetectorAttack ConsensusDetectorMode = "ATTACK"
)

type ConsensusDetectorMetrics struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64
	// `ValidatorMetricHeight` stores whether the related condition is satisfied.
	ValidatorMetricHeight uint64
	// `NodeRole` stores the value associated with this record.
	NodeRole string
	// `TotalValidators` stores the measured quantity used by this operation.
	TotalValidators int
	// `ActiveValidators` stores the value associated with this record.
	ActiveValidators int
	// `Quorum` stores the value associated with this record.
	Quorum int
	// `NetworkQuorumVotes` stores the value associated with this record.
	NetworkQuorumVotes int
	// `NetworkQuorumRequired` stores the value associated with this record.
	NetworkQuorumRequired int
	// `MaxValidatorLag` stores the value associated with this record.
	MaxValidatorLag uint64
	// `PeerCount` stores the measured quantity used by this operation.
	PeerCount int
	// `MissedVotes` stores the value associated with this record.
	MissedVotes int
	// `BlockTimeMS` stores the block data handled by this operation.
	BlockTimeMS int64
	// `DoubleSign` stores the value associated with this record.
	DoubleSign bool
	// `ForkDetected` stores the value associated with this record.
	ForkDetected bool
	// `SyncingValidators` stores the value associated with this record.
	SyncingValidators int
	// `LastFinalitySec` stores the value associated with this record.
	LastFinalitySec int64
	// `PartitionRisk` stores the value associated with this record.
	PartitionRisk bool
	// `FinalityLagBlocks` stores the value associated with this record.
	FinalityLagBlocks uint64

	// `DegradedAfterSec` stores the value associated with this record.
	DegradedAfterSec int64
	// `HaltedAfterSec` stores the value associated with this record.
	HaltedAfterSec int64
	// `RecoveryValidatorLagBlocks` stores the value associated with this record.
	RecoveryValidatorLagBlocks uint64
}

type ConsensusDetectorResult struct {
	// `Mode` stores the value associated with this record.
	Mode ConsensusDetectorMode
	// `Code` stores the value associated with this record.
	Code int
	// `Reason` stores the value associated with this record.
	Reason string
	// `CandidateMode` stores the value associated with this record.
	CandidateMode ConsensusDetectorMode
	// `CandidateReason` stores the value associated with this record.
	CandidateReason string
	// `CandidateSamples` stores the value associated with this record.
	CandidateSamples int
	// `StableModeReason` stores the value associated with this record.
	StableModeReason string
	// `FinalityLagBlocks` stores the value associated with this record.
	FinalityLagBlocks uint64
	// `LastFinalitySec` stores the value associated with this record.
	LastFinalitySec int64
	// `PartitionRisk` stores the value associated with this record.
	PartitionRisk bool
	// `Attack` stores the value associated with this record.
	Attack bool

	// `DegradedAfterSec` stores the value associated with this record.
	DegradedAfterSec int64
	// `HaltedAfterSec` stores the value associated with this record.
	HaltedAfterSec int64
	// `RecoveryValidatorLagBlocks` stores the value associated with this record.
	RecoveryValidatorLagBlocks uint64
}

// DetectConsensusMode implements the detect consensus mode helper.
func DetectConsensusMode(m ConsensusDetectorMetrics) ConsensusDetectorResult {
	// `finalityLag` stores the value produced by this operation.
	finalityLag := m.FinalityLagBlocks
	if finalityLag == 0 && m.Height > m.FinalizedHeight {
		finalityLag = m.Height - m.FinalizedHeight
	}
	// `degradedAfterSec` stores the value produced by this operation.
	degradedAfterSec := consensusDetectorMetricDegradedAfterSec(m)
	// `haltedAfterSec` stores the value produced by this operation.
	haltedAfterSec := consensusDetectorMetricHaltedAfterSec(m)
	// `emergencyAfterSec` stores the value produced by this operation.
	emergencyAfterSec := consensusDetectorMetricEmergencyAfterSec(degradedAfterSec, haltedAfterSec)
	// `recoveryLagBlocks` stores the value produced by this operation.
	recoveryLagBlocks := consensusDetectorMetricRecoveryValidatorLagBlocks(m)

	// `result` stores the result produced by this operation.
	result := ConsensusDetectorResult{
		Mode:                       ConsensusDetectorNormal,
		Reason:                     "healthy",
		CandidateMode:              ConsensusDetectorNormal,
		CandidateReason:            "healthy",
		CandidateSamples:           1,
		StableModeReason:           "healthy",
		FinalityLagBlocks:          finalityLag,
		LastFinalitySec:            m.LastFinalitySec,
		PartitionRisk:              m.PartitionRisk,
		Attack:                     m.DoubleSign || m.ForkDetected,
		DegradedAfterSec:           degradedAfterSec,
		HaltedAfterSec:             haltedAfterSec,
		RecoveryValidatorLagBlocks: recoveryLagBlocks,
	}

	// `nodeRole` stores the value produced by this operation.
	nodeRole := strings.ToLower(strings.TrimSpace(m.NodeRole))
	// `localCatchup` stores the value produced by this operation.
	localCatchup := m.SyncingValidators > 0 && !strings.EqualFold(nodeRole, "validator")
	// `materialLocalCatchup` stores the value produced by this operation.
	materialLocalCatchup := localCatchup &&
		(finalityLag > 0 ||
			m.MaxValidatorLag > consensusDetectorDegradedValidatorLagBlocks() ||
			m.LastFinalitySec >= degradedAfterSec)
	// `syncingNeedsRecovery` stores the value produced by this operation.
	syncingNeedsRecovery := m.SyncingValidators > 0 &&
		(nodeRole == "" || strings.EqualFold(nodeRole, "validator") || materialLocalCatchup)
	// `quorumVotesOK` stores whether the related condition is satisfied.
	quorumVotesOK := m.NetworkQuorumRequired == 0 || m.NetworkQuorumVotes >= m.NetworkQuorumRequired
	// `networkShowsProgress` stores whether this node can see an external
	// consensus path that is still making progress. A local validator catching
	// up must not label the whole chain halted just because its own live-window
	// view temporarily falls below quorum.
	networkShowsProgress := (m.NetworkQuorumRequired > 0 && m.NetworkQuorumVotes >= m.NetworkQuorumRequired) ||
		(m.NetworkQuorumRequired == 0 && m.NetworkQuorumVotes > 0) ||
		m.PeerCount >= 3
	// `networkQuorumProvesProgress` stores whether signed quorum evidence
	// contradicts a stale local liveness window.
	networkQuorumProvesProgress := m.NetworkQuorumRequired > 0 && m.NetworkQuorumVotes >= m.NetworkQuorumRequired
	// `localValidatorCatchup` stores whether the local validator is in a
	// recoverable sync/catch-up state while the surrounding network is visible.
	localValidatorCatchup := strings.EqualFold(nodeRole, "validator") &&
		m.SyncingValidators > 0 &&
		networkShowsProgress &&
		(m.MaxValidatorLag > 0 || finalityLag > 0 || m.LastFinalitySec >= degradedAfterSec || m.ActiveValidators < m.Quorum)
	// `staleActiveWindow` stores whether recent commit votes prove that the
	// network has quorum even though this node's heartbeat window has not yet
	// caught up.
	staleActiveWindow := m.Quorum > 0 &&
		m.ActiveValidators < m.Quorum &&
		networkQuorumProvesProgress &&
		finalityLag == 0 &&
		m.LastFinalitySec < degradedAfterSec
	// `networkQuorumLoss` stores the value produced by this operation.
	networkQuorumLoss := m.NetworkQuorumRequired > 0 && m.NetworkQuorumVotes > 0 && m.NetworkQuorumVotes < m.NetworkQuorumRequired
	// `softLocalPartition` stores the value produced by this operation.
	softLocalPartition := m.PartitionRisk &&
		localCatchup &&
		finalityLag == 0 &&
		m.Quorum > 0 &&
		m.ActiveValidators >= m.Quorum &&
		quorumVotesOK

	switch {
	case m.DoubleSign || m.ForkDetected:
		result.Mode = ConsensusDetectorAttack
		result.Reason = "attack_signal"
	case materialLocalCatchup:
		result.Mode = ConsensusDetectorRecovery
		result.Reason = "local_sync_catchup"
	case localValidatorCatchup:
		result.Mode = ConsensusDetectorRecovery
		result.Reason = "local_validator_catchup"
	case m.LastFinalitySec > haltedAfterSec:
		result.Mode = ConsensusDetectorHalted
		result.Reason = "finality_timeout"
	case staleActiveWindow:
		result.Mode = ConsensusDetectorDegraded
		result.Reason = "validator_liveness_window_lag"
	case m.Quorum > 0 && m.ActiveValidators < m.Quorum:
		result.Mode = ConsensusDetectorEmergency
		result.Reason = fmt.Sprintf("active_validators_%d_below_quorum_%d", m.ActiveValidators, m.Quorum)
	case networkQuorumLoss:
		result.Mode = ConsensusDetectorPartition
		result.Reason = "network_quorum_loss"
	case m.PartitionRisk && !softLocalPartition:
		result.Mode = ConsensusDetectorPartition
		result.Reason = "partition_risk"
	case syncingNeedsRecovery || m.MaxValidatorLag > recoveryLagBlocks:
		result.Mode = ConsensusDetectorRecovery
		result.Reason = "validator_recovery"
	case m.LastFinalitySec > emergencyAfterSec || m.BlockTimeMS > emergencyAfterSec*1000:
		result.Mode = ConsensusDetectorEmergency
		result.Reason = "block_production_timeout"
	case m.Quorum > 0 && m.ActiveValidators == m.Quorum:
		result.Mode = ConsensusDetectorStrict
		result.Reason = "minimum_quorum_active"
	case finalityLag > 0:
		result.Mode = ConsensusDetectorDegraded
		result.Reason = "finality_lag"
	case m.MaxValidatorLag > consensusDetectorDegradedValidatorLagBlocks():
		result.Mode = ConsensusDetectorDegraded
		result.Reason = "validator_lag"
	case m.PeerCount > 0 && m.PeerCount < 3 && finalityLag > 0:
		result.Mode = ConsensusDetectorDegraded
		result.Reason = "peer_instability"
	case m.MissedVotes > 0 && m.Quorum > 0 && m.ActiveValidators-m.MissedVotes <= m.Quorum:
		result.Mode = ConsensusDetectorDegraded
		result.Reason = "validator_lag"
	case m.LastFinalitySec >= degradedAfterSec || m.BlockTimeMS >= degradedAfterSec*1000:
		result.Mode = ConsensusDetectorDegraded
		result.Reason = "slow_blocks_sustained"
	default:
		result.Mode = ConsensusDetectorNormal
		result.Reason = "healthy"
	}
	result.CandidateMode = result.Mode
	result.CandidateReason = result.Reason
	result.StableModeReason = result.Reason
	result.Code = consensusDetectorModeCode(result.Mode)
	return result
}

// consensusDetectorMetricDegradedAfterSec implements the consensus detector metric degraded after sec helper.
func consensusDetectorMetricDegradedAfterSec(m ConsensusDetectorMetrics) int64 {
	if m.DegradedAfterSec > 0 {
		return m.DegradedAfterSec
	}
	// `sec` stores the value produced by this operation.
	sec := int64(ConsensusDetectorDegradedAfter / time.Second)
	if sec <= 0 {
		return 12
	}
	return sec
}

// consensusDetectorMetricHaltedAfterSec implements the consensus detector metric halted after sec helper.
func consensusDetectorMetricHaltedAfterSec(m ConsensusDetectorMetrics) int64 {
	if m.HaltedAfterSec > 0 {
		return m.HaltedAfterSec
	}
	// `sec` stores the value produced by this operation.
	sec := int64(ConsensusDetectorHaltedAfter / time.Second)
	if sec <= 0 {
		return 60
	}
	return sec
}

// consensusDetectorMetricEmergencyAfterSec implements the consensus detector metric emergency after sec helper.
func consensusDetectorMetricEmergencyAfterSec(degradedAfterSec, haltedAfterSec int64) int64 {
	// `sec` stores the value produced by this operation.
	sec := degradedAfterSec * 2
	if sec < 30 {
		sec = 30
	}
	if haltedAfterSec > 0 && sec >= haltedAfterSec {
		sec = haltedAfterSec - 1
	}
	if sec < degradedAfterSec {
		return degradedAfterSec
	}
	return sec
}

// consensusDetectorMetricRecoveryValidatorLagBlocks implements the consensus detector metric recovery validator lag blocks helper.
func consensusDetectorMetricRecoveryValidatorLagBlocks(m ConsensusDetectorMetrics) uint64 {
	if m.RecoveryValidatorLagBlocks > 0 {
		return m.RecoveryValidatorLagBlocks
	}
	if ConsensusDetectorRecoveryValidatorLagBlocks == 0 {
		return 100
	}
	return ConsensusDetectorRecoveryValidatorLagBlocks
}

// consensusDetectorDegradedValidatorLagBlocks implements the consensus detector degraded validator lag blocks helper.
func consensusDetectorDegradedValidatorLagBlocks() uint64 {
	// `threshold` stores the value produced by this operation.
	threshold := validatorLivenessMaxHeightDriftBlocks()
	if threshold == 0 {
		return 1
	}
	return threshold
}

// consensusDetectorModeCode implements the consensus detector mode code helper.
func consensusDetectorModeCode(mode ConsensusDetectorMode) int {
	switch mode {
	case ConsensusDetectorNormal:
		return 0
	case ConsensusDetectorStrict:
		return 1
	case ConsensusDetectorRecovery:
		return 2
	case ConsensusDetectorDegraded:
		return 3
	case ConsensusDetectorEmergency:
		return 4
	case ConsensusDetectorHalted:
		return 5
	case ConsensusDetectorPartition:
		return 6
	case ConsensusDetectorAttack:
		return 7
	default:
		return -1
	}
}

// consensusDetectorValidatorMetricHeight implements the consensus detector validator metric height helper.
func consensusDetectorValidatorMetricHeight(runtime RuntimeStatusSnapshot) uint64 {
	// RuntimeStatusSnapshot.Height is the committed chain tip, while
	// LiveValidators, RequiredQuorum, and StrictQuorum are computed for the next
	// proposal height in runtimeStatusSnapshot. Keep TotalValidators on that
	// same height so detector metrics do not mix epoch views at transitions.
	if runtime.Height == ^uint64(0) {
		return runtime.Height
	}
	return runtime.Height + 1
}

// consensusDetectorMetricsFromRuntime implements the consensus detector metrics from runtime helper.
func (n *Node) consensusDetectorMetricsFromRuntime(runtime RuntimeStatusSnapshot) ConsensusDetectorMetrics {
	// `validatorMetricHeight` stores whether the related condition is satisfied.
	validatorMetricHeight := consensusDetectorValidatorMetricHeight(runtime)
	// `total` stores the measured quantity used by this operation.
	total := len(n.GetConsensusValidators(int(validatorMetricHeight)))
	if total == 0 {
		total = GenesisFrozenValidatorSetSize
	}
	if total == 0 && runtime.RequiredQuorum > 0 {
		total = runtime.RequiredQuorum
	}
	// `quorum` stores the value produced by this operation.
	quorum := runtime.RequiredQuorum
	if quorum == 0 {
		quorum = runtime.StrictQuorum
	}
	if quorum == 0 {
		quorum = runtime.NetworkQuorumRequired
	}

	// `finalityLag` stores the value produced by this operation.
	finalityLag := uint64(0)
	if runtime.Height > runtime.FinalizedHeight {
		finalityLag = runtime.Height - runtime.FinalizedHeight
	}
	// `lastFinalitySec` stores the value produced by this operation.
	lastFinalitySec := int64(runtime.LastBlockAgeSeconds)
	if finalityLag > 0 && lastFinalitySec == 0 {
		lastFinalitySec = int64(finalityLag)
	}

	// `syncingValidators` stores the value produced by this operation.
	syncingValidators := 0
	if runtime.Syncing || !runtime.SyncComplete || runtimeStatusIndicatesActiveSync(runtime) {
		syncingValidators = 1
	}
	if strings.Contains(strings.ToLower(runtime.ValidatorState), "sync") ||
		strings.Contains(strings.ToLower(runtime.WaitReason), "sync") ||
		strings.Contains(strings.ToLower(runtime.WaitReason), "snapshot") {
		if syncingValidators < 1 {
			syncingValidators = 1
		}
	}

	// `partitionRisk` stores the value produced by this operation.
	partitionRisk := runtime.NetworkLagBlocks > validatorLivenessMaxHeightDriftBlocks()*2
	if runtime.Peers > 0 && runtime.Peers < 3 && runtime.NetworkBestHeight > runtime.Height {
		partitionRisk = true
	}
	if runtime.NetworkQuorumRequired > 0 && runtime.NetworkQuorumVotes > 0 && runtime.NetworkQuorumVotes < runtime.NetworkQuorumRequired {
		partitionRisk = true
	}

	return ConsensusDetectorMetrics{
		Height:                runtime.Height,
		FinalizedHeight:       runtime.FinalizedHeight,
		ValidatorMetricHeight: validatorMetricHeight,
		NodeRole:              runtime.Role,
		TotalValidators:       total,
		ActiveValidators:      runtime.LiveValidators,
		Quorum:                quorum,
		NetworkQuorumVotes:    runtime.NetworkQuorumVotes,
		NetworkQuorumRequired: runtime.NetworkQuorumRequired,
		MaxValidatorLag:       runtime.NetworkLagBlocks,
		PeerCount:             runtime.Peers,
		MissedVotes:           runtime.LiveOutOfDriftCount,
		BlockTimeMS:           int64(runtime.LastBlockAgeSeconds) * 1000,
		ForkDetected:          runtime.ExecMismatchUniqueSignersCurrentEpoch > 0,
		SyncingValidators:     syncingValidators,
		LastFinalitySec:       lastFinalitySec,
		PartitionRisk:         partitionRisk,
		FinalityLagBlocks:     finalityLag,

		DegradedAfterSec:           int64(ConsensusDetectorDegradedAfter / time.Second),
		HaltedAfterSec:             int64(ConsensusDetectorHaltedAfter / time.Second),
		RecoveryValidatorLagBlocks: ConsensusDetectorRecoveryValidatorLagBlocks,
	}
}

func runtimeStatusIndicatesActiveSync(runtime RuntimeStatusSnapshot) bool {
	// The runtime may be actively catching up while the raw Syncing bit is
	// already cleared near the live tip. Detector metrics still need that
	// intent, otherwise a local validator can be misclassified as halted.
	fields := []string{
		runtime.SyncAction,
		runtime.SyncMode,
		runtime.SyncStage,
		runtime.SyncPipelineStage,
		runtime.FastBootstrapStage,
	}
	for _, field := range fields {
		value := strings.ToLower(strings.TrimSpace(field))
		switch value {
		case "", "idle", "up_to_date", "gossip", "realtime_gossip", "gossip_validate_apply", "sync_complete":
			continue
		}
		if strings.Contains(value, "sync") ||
			strings.Contains(value, "catch") ||
			strings.Contains(value, "snapshot") ||
			strings.Contains(value, "bootstrap") ||
			strings.Contains(value, "delta") ||
			strings.Contains(value, "range_fetch") ||
			strings.Contains(value, "replay") {
			return true
		}
	}
	return runtime.SyncTarget > runtime.Height || runtime.SyncLagBlocks > 0
}

// applyConsensusModeDetector applies consensus mode detector.
func (n *Node) applyConsensusModeDetector(out *RuntimeStatusSnapshot) {
	if n == nil || out == nil {
		return
	}
	// `result` stores the result produced by this operation.
	result := n.stabilizeConsensusDetectorResult(DetectConsensusMode(n.consensusDetectorMetricsFromRuntime(*out)))
	out.ConsensusDetectorMode = string(result.Mode)
	out.ConsensusDetectorCode = result.Code
	out.ConsensusDetectorReason = result.Reason
	out.ConsensusDetectorCandidateMode = string(result.CandidateMode)
	out.ConsensusDetectorCandidateReason = result.CandidateReason
	out.ConsensusDetectorCandidateSamples = result.CandidateSamples
	out.ConsensusDetectorStableModeReason = result.StableModeReason
	out.ConsensusDetectorFinalityLagBlocks = result.FinalityLagBlocks
	out.ConsensusDetectorLastFinalitySec = result.LastFinalitySec
	out.ConsensusDetectorPartitionRisk = result.PartitionRisk
	out.ConsensusDetectorAttack = result.Attack
}

// stabilizeConsensusDetectorResult implements the stabilize consensus detector result helper.
func (n *Node) stabilizeConsensusDetectorResult(candidate ConsensusDetectorResult) ConsensusDetectorResult {
	candidate.CandidateMode = candidate.Mode
	candidate.CandidateReason = candidate.Reason
	if n == nil {
		candidate.CandidateSamples = 1
		candidate.StableModeReason = candidate.Reason
		return candidate
	}
	n.consensusDetectorMu.Lock()
	defer n.consensusDetectorMu.Unlock()

	if string(candidate.Mode) == n.consensusDetectorCandidateMode &&
		candidate.Reason == n.consensusDetectorCandidateReason {
		n.consensusDetectorCandidateSamples++
	} else {
		n.consensusDetectorCandidateMode = string(candidate.Mode)
		n.consensusDetectorCandidateReason = candidate.Reason
		n.consensusDetectorCandidateSamples = 1
	}
	candidate.CandidateSamples = n.consensusDetectorCandidateSamples

	if n.consensusDetectorStableMode == "" {
		n.consensusDetectorStableMode = string(candidate.Mode)
		n.consensusDetectorStableReason = candidate.Reason
		candidate.StableModeReason = candidate.Reason
		return candidate
	}

	if consensusDetectorHealthierCandidate(candidate.Mode) &&
		consensusDetectorSoftStableMode(n.consensusDetectorStableMode, n.consensusDetectorStableReason) {
		n.consensusDetectorStableMode = string(candidate.Mode)
		n.consensusDetectorStableReason = candidate.Reason
		candidate.StableModeReason = candidate.Reason
		return candidate
	}

	if candidate.Mode == ConsensusDetectorAttack ||
		candidate.Mode == ConsensusDetectorHalted ||
		candidate.Mode == ConsensusDetectorEmergency ||
		(candidate.Mode == ConsensusDetectorPartition && candidate.Reason == "network_quorum_loss") ||
		n.consensusDetectorCandidateSamples >= 2 ||
		string(candidate.Mode) == n.consensusDetectorStableMode {
		n.consensusDetectorStableMode = string(candidate.Mode)
		n.consensusDetectorStableReason = candidate.Reason
		candidate.StableModeReason = candidate.Reason
		return candidate
	}

	// `held` stores the value produced by this operation.
	held := candidate
	held.Mode = ConsensusDetectorMode(n.consensusDetectorStableMode)
	held.Code = consensusDetectorModeCode(held.Mode)
	held.Reason = n.consensusDetectorStableReason
	held.StableModeReason = fmt.Sprintf("holding_%s_pending_%s", strings.ToLower(n.consensusDetectorStableMode), strings.ToLower(string(candidate.Mode)))
	return held
}

// consensusDetectorHealthierCandidate implements the consensus detector healthier candidate helper.
func consensusDetectorHealthierCandidate(mode ConsensusDetectorMode) bool {
	return mode == ConsensusDetectorNormal || mode == ConsensusDetectorStrict
}

// consensusDetectorSoftStableMode implements the consensus detector soft stable mode helper.
func consensusDetectorSoftStableMode(mode, reason string) bool {
	switch ConsensusDetectorMode(strings.ToUpper(strings.TrimSpace(mode))) {
	case ConsensusDetectorDegraded, ConsensusDetectorRecovery:
		return true
	case ConsensusDetectorPartition:
		return reason != "network_quorum_loss"
	default:
		return false
	}
}
