package main

import (
	"fmt"
	"strings"
	"time"
)

type ConsensusDetectorMode string

const (
	ConsensusDetectorNormal    ConsensusDetectorMode = "NORMAL"
	ConsensusDetectorStrict    ConsensusDetectorMode = "STRICT"
	ConsensusDetectorRecovery  ConsensusDetectorMode = "RECOVERY"
	ConsensusDetectorDegraded  ConsensusDetectorMode = "DEGRADED"
	ConsensusDetectorEmergency ConsensusDetectorMode = "EMERGENCY"
	ConsensusDetectorHalted    ConsensusDetectorMode = "HALTED"
	ConsensusDetectorPartition ConsensusDetectorMode = "PARTITION"
	ConsensusDetectorAttack    ConsensusDetectorMode = "ATTACK"
)

type ConsensusDetectorMetrics struct {
	Height                uint64
	FinalizedHeight       uint64
	ValidatorMetricHeight uint64
	NodeRole              string
	TotalValidators       int
	ActiveValidators      int
	Quorum                int
	NetworkQuorumVotes    int
	NetworkQuorumRequired int
	MaxValidatorLag       uint64
	PeerCount             int
	MissedVotes           int
	BlockTimeMS           int64
	DoubleSign            bool
	ForkDetected          bool
	SyncingValidators     int
	LastFinalitySec       int64
	PartitionRisk         bool
	FinalityLagBlocks     uint64

	DegradedAfterSec           int64
	HaltedAfterSec             int64
	RecoveryValidatorLagBlocks uint64
}

type ConsensusDetectorResult struct {
	Mode              ConsensusDetectorMode
	Code              int
	Reason            string
	CandidateMode     ConsensusDetectorMode
	CandidateReason   string
	CandidateSamples  int
	StableModeReason  string
	FinalityLagBlocks uint64
	LastFinalitySec   int64
	PartitionRisk     bool
	Attack            bool

	DegradedAfterSec           int64
	HaltedAfterSec             int64
	RecoveryValidatorLagBlocks uint64
}

func DetectConsensusMode(m ConsensusDetectorMetrics) ConsensusDetectorResult {
	finalityLag := m.FinalityLagBlocks
	if finalityLag == 0 && m.Height > m.FinalizedHeight {
		finalityLag = m.Height - m.FinalizedHeight
	}
	degradedAfterSec := consensusDetectorMetricDegradedAfterSec(m)
	haltedAfterSec := consensusDetectorMetricHaltedAfterSec(m)
	recoveryLagBlocks := consensusDetectorMetricRecoveryValidatorLagBlocks(m)

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

	nodeRole := strings.ToLower(strings.TrimSpace(m.NodeRole))
	localCatchup := m.SyncingValidators > 0 && !strings.EqualFold(nodeRole, "validator")
	materialLocalCatchup := localCatchup &&
		(finalityLag > 0 ||
			m.MaxValidatorLag > consensusDetectorDegradedValidatorLagBlocks() ||
			m.LastFinalitySec >= degradedAfterSec)
	syncingNeedsRecovery := m.SyncingValidators > 0 &&
		(nodeRole == "" || strings.EqualFold(nodeRole, "validator") || materialLocalCatchup)
	quorumVotesOK := m.NetworkQuorumRequired == 0 || m.NetworkQuorumVotes >= m.NetworkQuorumRequired
	networkQuorumLoss := m.NetworkQuorumRequired > 0 && m.NetworkQuorumVotes > 0 && m.NetworkQuorumVotes < m.NetworkQuorumRequired
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
	case m.LastFinalitySec > haltedAfterSec:
		result.Mode = ConsensusDetectorHalted
		result.Reason = "finality_timeout"
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

func consensusDetectorMetricDegradedAfterSec(m ConsensusDetectorMetrics) int64 {
	if m.DegradedAfterSec > 0 {
		return m.DegradedAfterSec
	}
	sec := int64(ConsensusDetectorDegradedAfter / time.Second)
	if sec <= 0 {
		return 12
	}
	return sec
}

func consensusDetectorMetricHaltedAfterSec(m ConsensusDetectorMetrics) int64 {
	if m.HaltedAfterSec > 0 {
		return m.HaltedAfterSec
	}
	sec := int64(ConsensusDetectorHaltedAfter / time.Second)
	if sec <= 0 {
		return 60
	}
	return sec
}

func consensusDetectorMetricRecoveryValidatorLagBlocks(m ConsensusDetectorMetrics) uint64 {
	if m.RecoveryValidatorLagBlocks > 0 {
		return m.RecoveryValidatorLagBlocks
	}
	if ConsensusDetectorRecoveryValidatorLagBlocks == 0 {
		return 100
	}
	return ConsensusDetectorRecoveryValidatorLagBlocks
}

func consensusDetectorDegradedValidatorLagBlocks() uint64 {
	threshold := validatorLivenessMaxHeightDriftBlocks()
	if threshold == 0 {
		return 1
	}
	return threshold
}

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

func (n *Node) consensusDetectorMetricsFromRuntime(runtime RuntimeStatusSnapshot) ConsensusDetectorMetrics {
	validatorMetricHeight := consensusDetectorValidatorMetricHeight(runtime)
	total := len(n.GetConsensusValidators(int(validatorMetricHeight)))
	if total == 0 {
		total = GenesisFrozenValidatorSetSize
	}
	if total == 0 && runtime.RequiredQuorum > 0 {
		total = runtime.RequiredQuorum
	}
	quorum := runtime.RequiredQuorum
	if quorum == 0 {
		quorum = runtime.StrictQuorum
	}
	if quorum == 0 {
		quorum = runtime.NetworkQuorumRequired
	}

	finalityLag := uint64(0)
	if runtime.Height > runtime.FinalizedHeight {
		finalityLag = runtime.Height - runtime.FinalizedHeight
	}
	lastFinalitySec := int64(runtime.LastBlockAgeSeconds)
	if finalityLag > 0 && lastFinalitySec == 0 {
		lastFinalitySec = int64(finalityLag)
	}

	syncingValidators := 0
	if runtime.Syncing || !runtime.SyncComplete {
		syncingValidators = 1
	}
	if strings.Contains(strings.ToLower(runtime.ValidatorState), "sync") ||
		strings.Contains(strings.ToLower(runtime.WaitReason), "sync") ||
		strings.Contains(strings.ToLower(runtime.WaitReason), "snapshot") {
		if syncingValidators < 1 {
			syncingValidators = 1
		}
	}

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

func (n *Node) applyConsensusModeDetector(out *RuntimeStatusSnapshot) {
	if n == nil || out == nil {
		return
	}
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

	held := candidate
	held.Mode = ConsensusDetectorMode(n.consensusDetectorStableMode)
	held.Code = consensusDetectorModeCode(held.Mode)
	held.Reason = n.consensusDetectorStableReason
	held.StableModeReason = fmt.Sprintf("holding_%s_pending_%s", strings.ToLower(n.consensusDetectorStableMode), strings.ToLower(string(candidate.Mode)))
	return held
}

func consensusDetectorHealthierCandidate(mode ConsensusDetectorMode) bool {
	return mode == ConsensusDetectorNormal || mode == ConsensusDetectorStrict
}

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
