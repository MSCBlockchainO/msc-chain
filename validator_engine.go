package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ValidatorState string

const (
	// ValidatorInactive is a governance-controlled suspension state. It is not
	// eligible for consensus, but it can be reactivated by a certified update.
	ValidatorInactive ValidatorState = "INACTIVE"
	// `ValidatorPending` defines whether the related condition is satisfied.
	// Kept for replay compatibility with registry snapshots created before the
	// explicit INACTIVE lifecycle state was introduced.
	ValidatorPending ValidatorState = "PENDING"
	// `ValidatorActive` defines whether the related condition is satisfied.
	ValidatorActive ValidatorState = "ACTIVE"
	// `ValidatorJailed` defines whether the related condition is satisfied.
	ValidatorJailed ValidatorState = "JAILED"
	// ValidatorRemoved is a terminal governance-controlled lifecycle state.
	ValidatorRemoved ValidatorState = "REMOVED"
	// `ValidatorExited` defines whether the related condition is satisfied.
	// Kept for replay compatibility with legacy snapshots.
	ValidatorExited ValidatorState = "EXITED"
)

const (
	// `protocolDynamicValidatorSelectionEnabled` defines whether the related condition is satisfied.
	protocolDynamicValidatorSelectionEnabled = true
	// `protocolDeterministicValidatorSelection` defines the constant value used by this package.
	protocolDeterministicValidatorSelection = true
	// `protocolCandidateIsolationMode` defines the constant value used by this package.
	protocolCandidateIsolationMode = true
	// `protocolValidatorActiveSetMode` defines the constant value used by this package.
	protocolValidatorActiveSetMode = "adaptive_committee"
	// `protocolValidatorActiveSetSize` defines the measured quantity used by this operation.
	protocolValidatorActiveSetSize = 0
	// `protocolValidatorMaxActiveCommittee` defines the constant value used by this package.
	protocolValidatorMaxActiveCommittee  = 25
	protocolValidatorRegistrySmallSetMax = 25
	// `protocolValidatorAdaptiveCommitteeLogMult` defines the constant value used by this package.
	protocolValidatorAdaptiveCommitteeLogMult = 16
	// `protocolValidatorCommitteeRotationBlocks` defines the constant value used by this package.
	protocolValidatorCommitteeRotationBlocks  uint64 = 32
	protocolValidatorSelectionActivityWindow  uint64 = 64
	protocolValidatorSelectionMinSignedBlocks uint64 = 1
	// `protocolValidatorSetRotationWindow` defines the constant value used by this package.
	protocolValidatorSetRotationWindow uint64 = 50
	// `protocolValidatorMinActiveSet` defines the constant value used by this package.
	protocolValidatorMinActiveSet                       = 4
	protocolValidatorInactiveBlocks              uint64 = 4
	protocolValidatorInactivePermanentRemove            = false
	protocolValidatorStakeCapPct                        = 0.05
	protocolValidatorUptimeWindow                uint64 = 1000
	protocolValidatorReputationRecoveryThreshold        = 0.30
	protocolValidatorReputationInitial                  = 0.50
	protocolValidatorLongUptimeThreshold                = 0.95
	protocolReputationCorrectDelta                      = 0.001
	protocolReputationMismatchDelta                     = -0.005
	protocolReputationLongUptimeDelta                   = 0.002
	protocolPenaltyMissedWeight                         = 0.001
	protocolPenaltyBadExecWeight                        = 0.05
	protocolPenaltyDoubleSignWeight                     = 1.0
	protocolPenaltyDisconnectWeight                     = 0.02
	protocolMassMissThreshold                    uint64 = 500

	// `protocolReputationSlashDelta` defines the constant value used by this package.
	protocolReputationSlashDelta = -0.10

	// `protocolJailDoubleSignBlocks` defines the constant value used by this package.
	protocolJailDoubleSignBlocks uint64 = 10000
	// `protocolJailBadExecutionBlocks` defines the constant value used by this package.
	protocolJailBadExecutionBlocks uint64 = 2000
	// `protocolJailMassMissBlocks` defines the constant value used by this package.
	protocolJailMassMissBlocks uint64 = 500

	// `protocolValidatorInactivityPenaltyEnabled` defines whether the related condition is satisfied.
	protocolValidatorInactivityPenaltyEnabled = true
	// `protocolValidatorInactivityPenaltyBurnBPS` defines the constant value used by this package.
	protocolValidatorInactivityPenaltyBurnBPS uint64 = 100
	// `protocolValidatorInactivityPenaltyJailBlocks` defines the constant value used by this package.
	protocolValidatorInactivityPenaltyJailBlocks uint64 = 500
	// `protocolValidatorInactivityPenaltyCooldownBlocks` defines the constant value used by this package.
	protocolValidatorInactivityPenaltyCooldownBlocks uint64 = 50
)

var (
	// `DynamicValidatorSelectionEnabled` stores whether the related condition is satisfied.
	DynamicValidatorSelectionEnabled = protocolDynamicValidatorSelectionEnabled
	// `DeterministicValidatorSelection` stores the value used by this operation.
	DeterministicValidatorSelection = protocolDeterministicValidatorSelection
	// Legacy compatibility flag kept for config/runtime compatibility.
	// Deterministic selection now always uses VRF-style scoring after stake gating.
	ValidatorEqualChanceSelection = true
	// Production safety: candidates are observation-only and cannot affect
	// execution voting or validator-set membership directly.
	CandidateIsolationMode = protocolCandidateIsolationMode

	// `ValidatorMinStake` stores whether the related condition is satisfied.
	ValidatorMinStake int64 = ConsensusValidatorMinStake
	// `ValidatorStakeCapPct` stores whether the related condition is satisfied.
	ValidatorStakeCapPct float64 = protocolValidatorStakeCapPct
	// `ValidatorActiveSetSize` stores whether the related condition is satisfied.
	ValidatorActiveSetSize int = protocolValidatorActiveSetSize
	// `ValidatorActiveSetMode` stores whether the related condition is satisfied.
	ValidatorActiveSetMode string = protocolValidatorActiveSetMode
	// `ValidatorMaxActiveCommittee` stores whether the related condition is satisfied.
	ValidatorMaxActiveCommittee int = protocolValidatorMaxActiveCommittee
	// `ValidatorHybridMaxActiveValidators` stores whether the related condition is satisfied.
	ValidatorHybridMaxActiveValidators int = protocolValidatorMaxActiveCommittee
	// `ValidatorHybridPerformanceSlots` stores whether the related condition is satisfied.
	ValidatorHybridPerformanceSlots int = 19
	// `ValidatorHybridRotationSlots` stores whether the related condition is satisfied.
	ValidatorHybridRotationSlots int = 6
	// `ValidatorHybridEffectiveStakeCap` stores whether the related condition is satisfied.
	ValidatorHybridEffectiveStakeCap int64 = 5_000_000
	// `ValidatorHybridEpochBlocks` stores whether the related condition is satisfied.
	ValidatorHybridEpochBlocks uint64 = 10_000
	// `ValidatorHybridStakeWeight` stores whether the related condition is satisfied.
	ValidatorHybridStakeWeight int = 40
	// `ValidatorHybridUptimeWeight` stores whether the related condition is satisfied.
	ValidatorHybridUptimeWeight int = 35
	// `ValidatorHybridPerformanceWeight` stores whether the related condition is satisfied.
	ValidatorHybridPerformanceWeight int = 15
	// `ValidatorHybridDecentralizationWeight` stores whether the related condition is satisfied.
	ValidatorHybridDecentralizationWeight int = 10
	// `ValidatorHybridPerformanceMinSignedBPS` stores whether the related condition is satisfied.
	ValidatorHybridPerformanceMinSignedBPS int = 9000
	// `ValidatorHybridPromotionWindowEpochs` stores whether the related condition is satisfied.
	ValidatorHybridPromotionWindowEpochs uint64 = 10
	// `ValidatorHybridMinimumPerformanceAgeEpochs` stores whether the related condition is satisfied.
	ValidatorHybridMinimumPerformanceAgeEpochs uint64 = 10
	// `ValidatorHybridMinimumOnlineWhenFull` stores whether the related condition is satisfied.
	ValidatorHybridMinimumOnlineWhenFull int = 15
	// `ValidatorHybridDiversityASNWeight` stores whether the related condition is satisfied.
	ValidatorHybridDiversityASNWeight int = 40
	// `ValidatorHybridDiversityRegionWeight` stores whether the related condition is satisfied.
	ValidatorHybridDiversityRegionWeight int = 30
	// `ValidatorHybridDiversityProviderWeight` stores whether the related condition is satisfied.
	ValidatorHybridDiversityProviderWeight int = 20
	// `ValidatorHybridDiversityHomePCWeight` stores whether the related condition is satisfied.
	ValidatorHybridDiversityHomePCWeight int = 10
	// `ValidatorAdaptiveCommitteeLogMult` stores whether the related condition is satisfied.
	ValidatorAdaptiveCommitteeLogMult int = protocolValidatorAdaptiveCommitteeLogMult
	// `ValidatorCommitteeRotationBlocks` stores whether the related condition is satisfied.
	ValidatorCommitteeRotationBlocks uint64 = protocolValidatorCommitteeRotationBlocks
	// `ValidatorSelectionActivityWindow` stores whether the related condition is satisfied.
	ValidatorSelectionActivityWindow uint64 = 64
	// `ValidatorSelectionMinSignedBlocks` stores whether the related condition is satisfied.
	ValidatorSelectionMinSignedBlocks uint64 = 1
	// `ValidatorOnboardingGraceBlocks` stores whether the related condition is satisfied.
	ValidatorOnboardingGraceBlocks uint64 = 64
	// `ValidatorOnboardingMaxNewSlots` stores whether the related condition is satisfied.
	ValidatorOnboardingMaxNewSlots int = 1
	// `ValidatorOnboardingStrictActivation` stores whether the related condition is satisfied.
	ValidatorOnboardingStrictActivation bool = true
	// `ValidatorSetAutohealMode` stores whether the related condition is satisfied.
	ValidatorSetAutohealMode string = "validator_quorum"
	// `ValidatorSetAutohealTrustedOnly` stores whether the related condition is satisfied.
	ValidatorSetAutohealTrustedOnly bool = true
	// `ValidatorSetAutohealNearTipForceAfter` stores whether the related condition is satisfied.
	ValidatorSetAutohealNearTipForceAfter int = 2
	// `ValidatorSetAutohealPauseSeconds` stores whether the related condition is satisfied.
	ValidatorSetAutohealPauseSeconds uint64 = 6
	// `ValidatorOnboardingBootstrapLaneEnabled` stores whether the related condition is satisfied.
	ValidatorOnboardingBootstrapLaneEnabled bool = true
	// `ValidatorOnboardingBootstrapMaxNewSlots` stores whether the related condition is satisfied.
	ValidatorOnboardingBootstrapMaxNewSlots int = 1
	// `ValidatorOnboardingBootstrapRequireStake` stores whether the related condition is satisfied.
	ValidatorOnboardingBootstrapRequireStake bool = true
	// `ValidatorOnboardingBootstrapRequireNotJailed` stores whether the related condition is satisfied.
	ValidatorOnboardingBootstrapRequireNotJailed bool = true
	// `ValidatorHeartbeatScope` stores whether the related condition is satisfied.
	ValidatorHeartbeatScope string = "committee_only"
	// `ValidatorUptimeWindow` stores whether the related condition is satisfied.
	ValidatorUptimeWindow uint64 = protocolValidatorUptimeWindow
	// `ValidatorReputationRecoveryThreshold` stores whether the related condition is satisfied.
	ValidatorReputationRecoveryThreshold float64 = protocolValidatorReputationRecoveryThreshold
	// `ValidatorReputationInitial` stores whether the related condition is satisfied.
	ValidatorReputationInitial float64 = protocolValidatorReputationInitial
	// `ValidatorLongUptimeThreshold` stores whether the related condition is satisfied.
	ValidatorLongUptimeThreshold float64 = protocolValidatorLongUptimeThreshold
	// `ValidatorRequireStake` stores whether the related condition is satisfied.
	ValidatorRequireStake bool = false
	// `ValidatorCoreStakeExempt` stores whether the related condition is satisfied.
	ValidatorCoreStakeExempt bool = true

	// `ReputationCorrectDelta` stores the value used by this operation.
	ReputationCorrectDelta float64 = protocolReputationCorrectDelta
	// `ReputationMismatchDelta` stores the value used by this operation.
	ReputationMismatchDelta float64 = protocolReputationMismatchDelta
	// `ReputationSlashDelta` stores the value used by this operation.
	ReputationSlashDelta float64 = protocolReputationSlashDelta
	// `ReputationLongUptimeDelta` stores the value used by this operation.
	ReputationLongUptimeDelta float64 = protocolReputationLongUptimeDelta

	// `PenaltyMissedWeight` stores the value used by this operation.
	PenaltyMissedWeight float64 = protocolPenaltyMissedWeight
	// `PenaltyBadExecWeight` stores the value used by this operation.
	PenaltyBadExecWeight float64 = protocolPenaltyBadExecWeight
	// `PenaltyDoubleSignWeight` stores the value used by this operation.
	PenaltyDoubleSignWeight float64 = protocolPenaltyDoubleSignWeight
	// `PenaltyDisconnectWeight` stores the value used by this operation.
	PenaltyDisconnectWeight float64 = protocolPenaltyDisconnectWeight

	// `JailDoubleSignBlocks` stores the current position in the related collection.
	JailDoubleSignBlocks uint64 = protocolJailDoubleSignBlocks
	// `JailBadExecutionBlocks` stores the current position in the related collection.
	JailBadExecutionBlocks uint64 = protocolJailBadExecutionBlocks
	// `JailMassMissBlocks` stores the current position in the related collection.
	JailMassMissBlocks uint64 = protocolJailMassMissBlocks
	// `MassMissThreshold` stores the value used by this operation.
	MassMissThreshold uint64 = protocolMassMissThreshold

	// Mutable compatibility knobs retained for config/status/UI only. Consensus
	// penalty state transitions must use the protocol helpers below.
	ValidatorInactivityPenaltyEnabled bool = protocolValidatorInactivityPenaltyEnabled
	// `ValidatorInactivityPenaltyBurnBPS` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyBurnBPS uint64 = protocolValidatorInactivityPenaltyBurnBPS
	// `ValidatorInactivityPenaltyJailBlocks` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyJailBlocks uint64 = protocolValidatorInactivityPenaltyJailBlocks
	// `ValidatorInactivityPenaltyCooldownBlocks` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyCooldownBlocks uint64 = protocolValidatorInactivityPenaltyCooldownBlocks
)

func protocolDynamicValidatorSelectionEnabledFlag() bool {
	return protocolDynamicValidatorSelectionEnabled
}

func protocolDeterministicValidatorSelectionEnabled() bool {
	return protocolDeterministicValidatorSelection
}

func protocolCandidateIsolationEnabled() bool {
	return protocolCandidateIsolationMode
}

func protocolValidatorActiveSetModeValue() string {
	return protocolValidatorActiveSetMode
}

func protocolValidatorActiveSetSizeValue() int {
	return protocolValidatorActiveSetSize
}

func protocolValidatorMaxActiveCommitteeValue() int {
	return protocolValidatorMaxActiveCommittee
}

func protocolValidatorRegistrySmallSetMaxValue() int {
	return protocolValidatorRegistrySmallSetMax
}

func protocolValidatorAdaptiveCommitteeLogMultValue() int {
	return protocolValidatorAdaptiveCommitteeLogMult
}

func protocolValidatorCommitteeRotationBlocksValue() uint64 {
	return protocolValidatorCommitteeRotationBlocks
}

func protocolValidatorSelectionActivityWindowValue() uint64 {
	return protocolValidatorSelectionActivityWindow
}

func protocolValidatorSelectionMinSignedBlocksValue() uint64 {
	return protocolValidatorSelectionMinSignedBlocks
}

func protocolValidatorSetRotationWindowValue() uint64 {
	return protocolValidatorSetRotationWindow
}

func protocolValidatorMinActiveSetValue() int {
	return protocolValidatorMinActiveSet
}

func protocolValidatorInactiveBlocksValue() uint64 {
	return protocolValidatorInactiveBlocks
}

func protocolValidatorInactivePermanentRemoveEnabled() bool {
	return protocolValidatorInactivePermanentRemove
}

func protocolValidatorStakeCapPctValue() float64 {
	return protocolValidatorStakeCapPct
}

func protocolValidatorUptimeWindowValue() uint64 {
	return protocolValidatorUptimeWindow
}

func protocolValidatorReputationRecoveryThresholdValue() float64 {
	return protocolValidatorReputationRecoveryThreshold
}

func protocolValidatorReputationInitialValue() float64 {
	return protocolValidatorReputationInitial
}

func protocolValidatorLongUptimeThresholdValue() float64 {
	return protocolValidatorLongUptimeThreshold
}

func protocolReputationCorrectDeltaValue() float64 {
	return protocolReputationCorrectDelta
}

func protocolReputationMismatchDeltaValue() float64 {
	return protocolReputationMismatchDelta
}

func protocolReputationLongUptimeDeltaValue() float64 {
	return protocolReputationLongUptimeDelta
}

func protocolPenaltyWeights() (float64, float64, float64, float64) {
	return protocolPenaltyMissedWeight, protocolPenaltyBadExecWeight, protocolPenaltyDoubleSignWeight, protocolPenaltyDisconnectWeight
}

func protocolMassMissThresholdValue() uint64 {
	return protocolMassMissThreshold
}

// protocolReputationSlashDeltaValue implements the protocol reputation slash delta helper.
func protocolReputationSlashDeltaValue() float64 {
	return protocolReputationSlashDelta
}

// protocolJailDoubleSignBlocksValue implements the protocol double-sign jail blocks helper.
func protocolJailDoubleSignBlocksValue() uint64 {
	return protocolJailDoubleSignBlocks
}

// protocolJailBadExecutionBlocksValue implements the protocol bad-execution jail blocks helper.
func protocolJailBadExecutionBlocksValue() uint64 {
	return protocolJailBadExecutionBlocks
}

// protocolJailMassMissBlocksValue implements the protocol mass-miss jail blocks helper.
func protocolJailMassMissBlocksValue() uint64 {
	return protocolJailMassMissBlocks
}

// protocolValidatorInactivityPenaltyEnabledFlag implements the protocol inactivity penalty enabled helper.
func protocolValidatorInactivityPenaltyEnabledFlag() bool {
	return protocolValidatorInactivityPenaltyEnabled
}

// protocolValidatorInactivityPenaltyBurnBPSValue implements the protocol inactivity penalty burn bps helper.
func protocolValidatorInactivityPenaltyBurnBPSValue() uint64 {
	if protocolValidatorInactivityPenaltyBurnBPS > 10000 {
		return 10000
	}
	return protocolValidatorInactivityPenaltyBurnBPS
}

// protocolValidatorInactivityPenaltyJailBlocksValue implements the protocol inactivity penalty jail blocks helper.
func protocolValidatorInactivityPenaltyJailBlocksValue() uint64 {
	if protocolValidatorInactivityPenaltyJailBlocks == 0 {
		return protocolJailMassMissBlocksValue()
	}
	return protocolValidatorInactivityPenaltyJailBlocks
}

// protocolValidatorInactivityPenaltyCooldownBlocksValue implements the protocol inactivity penalty cooldown helper.
func protocolValidatorInactivityPenaltyCooldownBlocksValue() uint64 {
	if protocolValidatorInactivityPenaltyCooldownBlocks == 0 {
		return 1
	}
	return protocolValidatorInactivityPenaltyCooldownBlocks
}

// validatorPassesStakeGate checks whether a validator's stake satisfies protocol eligibility.
func validatorPassesStakeGate(id string, stake int64) bool {
	_ = id
	return stake >= ConsensusValidatorMinStake
}

// validatorOnboardingGraceBlocks implements the validator onboarding grace blocks helper.
func validatorOnboardingGraceBlocks() uint64 {
	return ValidatorOnboardingGraceBlocks
}

// validatorOnboardingMaxNewSlots implements the validator onboarding max new slots helper.
func validatorOnboardingMaxNewSlots() int {
	if ValidatorOnboardingMaxNewSlots < 0 {
		return 0
	}
	return ValidatorOnboardingMaxNewSlots
}

// validatorOnboardingStrictActivationEnabled implements the validator onboarding strict activation enabled helper.
func validatorOnboardingStrictActivationEnabled() bool {
	return ValidatorOnboardingStrictActivation
}

// normalizeValidatorSetAutohealMode normalizes validator set autoheal mode.
func normalizeValidatorSetAutohealMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "validator_quorum":
		return "validator_quorum"
	case "strict_core_quorum":
		return "validator_quorum"
	default:
		return "validator_quorum"
	}
}

// validatorSetAutohealStrictCoreQuorum implements the validator set autoheal strict core quorum helper.
func validatorSetAutohealStrictCoreQuorum() bool {
	// Compatibility shim: legacy "strict_core_quorum" now resolves to the
	// committed validator-set quorum path.
	return normalizeValidatorSetAutohealMode(ValidatorSetAutohealMode) == "validator_quorum"
}

// validatorSetAutohealTrustedOnlyOnMismatchEnabled implements the validator set autoheal trusted only on mismatch enabled helper.
func validatorSetAutohealTrustedOnlyOnMismatchEnabled() bool {
	return ValidatorSetAutohealTrustedOnly
}

// validatorSetAutohealNearTipForceAfter implements the validator set autoheal near tip force after helper.
func validatorSetAutohealNearTipForceAfter() int {
	if ValidatorSetAutohealNearTipForceAfter <= 0 {
		return 0
	}
	return ValidatorSetAutohealNearTipForceAfter
}

// validatorSetAutohealPauseDuration implements the validator set autoheal pause duration helper.
func validatorSetAutohealPauseDuration() time.Duration {
	if ValidatorSetAutohealPauseSeconds == 0 {
		return 0
	}
	return time.Duration(ValidatorSetAutohealPauseSeconds) * time.Second
}

// validatorOnboardingBootstrapLaneEnabled implements the validator onboarding bootstrap lane enabled helper.
func validatorOnboardingBootstrapLaneEnabled() bool {
	return ValidatorOnboardingBootstrapLaneEnabled
}

// validatorOnboardingBootstrapMaxNewSlots implements the validator onboarding bootstrap max new slots helper.
func validatorOnboardingBootstrapMaxNewSlots() int {
	if ValidatorOnboardingBootstrapMaxNewSlots < 0 {
		return 0
	}
	return ValidatorOnboardingBootstrapMaxNewSlots
}

// validatorOnboardingBootstrapRequireStake implements the validator onboarding bootstrap require stake helper.
func validatorOnboardingBootstrapRequireStake() bool {
	return ValidatorOnboardingBootstrapRequireStake
}

// validatorOnboardingBootstrapRequireNotJailed implements the validator onboarding bootstrap require not jailed helper.
func validatorOnboardingBootstrapRequireNotJailed() bool {
	return ValidatorOnboardingBootstrapRequireNotJailed
}

// validatorStakeGateTransition implements the validator stake gate transition helper.
func validatorStakeGateTransition(id string, oldStake int64, newStake int64) (lost bool, gained bool) {
	// `oldPass` stores the value produced by this operation.
	oldPass := validatorPassesStakeGate(id, oldStake)
	// `newPass` stores the value produced by this operation.
	newPass := validatorPassesStakeGate(id, newStake)
	return oldPass && !newPass, !oldPass && newPass
}

type ValidatorRecord struct {
	// `ID` stores the current position in the related collection.
	ID string `json:"id"`
	// `ConsensusPubKey` stores the key used to access the related value.
	ConsensusPubKey string `json:"consensus_pubkey,omitempty"`
	// `GovernanceSigner` stores the value associated with this record.
	GovernanceSigner bool `json:"governance_signer,omitempty"`
	// `Stake` stores the value associated with this record.
	Stake int64 `json:"validator_stake"`
	// VotingPower is the equal-vote consensus weight exposed by the committed
	// registry. Membership still requires Status == ACTIVE.
	VotingPower uint64 `json:"validator_voting_power"`
	// `Reputation` stores the value associated with this record.
	Reputation float64 `json:"validator_reputation"`
	// `LastActive` stores the value associated with this record.
	LastActive uint64 `json:"validator_last_active"`
	// LastSeenHeight is the latest finalized height carrying canonical activity
	// evidence for this validator.
	LastSeenHeight uint64 `json:"validator_last_seen_height"`
	// LastVoteHeight is the latest finalized height signed by this validator.
	LastVoteHeight uint64 `json:"validator_last_vote_height"`
	// HeartbeatHeight is a deterministic, finalized heartbeat indicator. Local
	// wall-clock heartbeat telemetry is intentionally not consensus state.
	HeartbeatHeight uint64 `json:"validator_heartbeat_height"`
	// SuspensionRecommended is deterministic evidence for operators and
	// governance. It never removes a validator from consensus by itself.
	SuspensionRecommended       bool   `json:"validator_suspension_recommended,omitempty"`
	SuspensionRecommendedHeight uint64 `json:"validator_suspension_recommended_height,omitempty"`
	// `MissedBlocks` stores the value associated with this record.
	MissedBlocks uint64 `json:"validator_missed_blocks"`
	// `MissedBlocksWindow` stores the value associated with this record.
	MissedBlocksWindow uint64 `json:"validator_missed_blocks_window"`
	// `BadExecution` stores the value associated with this record.
	BadExecution uint64 `json:"validator_bad_execution"`
	// `DoubleSign` stores the value associated with this record.
	DoubleSign uint64 `json:"validator_double_sign"`
	// `DisconnectPattern` stores the value associated with this record.
	DisconnectPattern uint64 `json:"validator_disconnect_pattern"`
	// `Status` stores the value associated with this record.
	Status ValidatorState `json:"validator_status"`
	// `JailUntilHeight` stores the current position in the related collection.
	JailUntilHeight uint64 `json:"validator_jail_until_height"`
	// `TotalSlashes` stores the measured quantity used by this operation.
	TotalSlashes uint64 `json:"validator_total_slashes"`
	// `JoinHeight` stores the current position in the related collection.
	JoinHeight uint64 `json:"validator_join_height"`
	// `LastScore` stores the value associated with this record.
	LastScore float64 `json:"validator_last_score"`
	// `UptimeWindowCounter` stores the value associated with this record.
	UptimeWindowCounter uint64 `json:"validator_uptime_window_counter"`
	// `InactivityPenalties` stores the current position in the related collection.
	InactivityPenalties uint64 `json:"validator_inactivity_penalties"`
	// `LastInactivityPenaltyHeight` stores the value associated with this record.
	LastInactivityPenaltyHeight uint64 `json:"validator_last_inactivity_penalty_height"`
	// `ActiveHeights` stores the value associated with this record.
	ActiveHeights []uint64 `json:"validator_active_heights,omitempty"`
	// `SignedHeights` stores the value associated with this record.
	SignedHeights []uint64 `json:"validator_signed_heights,omitempty"`
}

type ValidatorRegistry struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `records` stores the value associated with this record.
	records map[string]*ValidatorRecord
}

// NewValidatorRegistry creates a new validator registry.
func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{records: make(map[string]*ValidatorRecord)}
}

// `GlobalValidatorRegistry` stores the value used by this operation.
var GlobalValidatorRegistry = NewValidatorRegistry()

// Ensure implements the ensure helper.
func (r *ValidatorRegistry) Ensure(id string, height uint64) *ValidatorRecord {
	if id == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// `rec` and `ok` store whether the related condition is satisfied.
	if rec, ok := r.records[id]; ok {
		return rec
	}
	// `rec` stores the value produced by this operation.
	rec := &ValidatorRecord{
		ID:            id,
		Stake:         0,
		Reputation:    protocolValidatorReputationInitialValue(),
		Status:        ValidatorPending,
		JoinHeight:    height,
		ActiveHeights: make([]uint64, 0),
		SignedHeights: make([]uint64, 0),
	}
	r.records[id] = rec
	return rec
}

// Get returns a cloned validator record by normalized validator ID.
func (r *ValidatorRegistry) Get(id string) (*ValidatorRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// `rec` and `ok` store whether the related condition is satisfied.
	rec, ok := r.records[id]
	return rec, ok
}

// All implements the all helper.
func (r *ValidatorRegistry) All() []*ValidatorRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// `out` stores the result produced by this operation.
	out := make([]*ValidatorRecord, 0, len(r.records))
	// `rec` tracks the current values while iterating.
	for _, rec := range r.records {
		out = append(out, rec)
	}
	return out
}

// Count returns the number of registered validators without cloning the
// registry. Status endpoints use this lightweight path while a node is syncing.
func (r *ValidatorRegistry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.records)
}

// Snapshot returns a cloned point-in-time copy of the validator registry.
func (r *ValidatorRegistry) Snapshot() map[string]ValidatorRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// `snap` stores the value produced by this operation.
	snap := make(map[string]ValidatorRecord, len(r.records))
	// `id` and `rec` track the current position in the related collection.
	for id, rec := range r.records {
		snap[id] = cloneValidatorRecord(rec)
	}
	return snap
}

// ValidatorRegistrySnapshotHash hashes a canonical validator registry snapshot for commitment checks.
func ValidatorRegistrySnapshotHash(snapshot map[string]ValidatorRecord) string {
	if len(snapshot) == 0 {
		return ""
	}
	// `canonical` stores the value produced by this operation.
	canonical := make(map[string]string, len(snapshot))
	// `key` and `rec` track the key used to access the related value.
	for key, rec := range snapshot {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			id = normalizeValidatorID(key)
		}
		if id == "" {
			continue
		}
		// `entry` stores the value produced by this operation.
		entry := fmt.Sprintf("%s|%s|%t|%d|%s|%d|%d|%d|%d",
			id,
			strings.ToLower(strings.TrimSpace(rec.ConsensusPubKey)),
			rec.GovernanceSigner,
			rec.Stake,
			strings.ToUpper(strings.TrimSpace(string(rec.Status))),
			rec.JailUntilHeight,
			rec.JoinHeight,
			rec.TotalSlashes,
			rec.InactivityPenalties,
		)
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := canonical[id]; !ok || entry < existing {
			canonical[id] = entry
		}
	}
	if len(canonical) == 0 {
		return ""
	}
	// `parts` stores the value produced by this operation.
	parts := make([]string, 0, len(canonical))
	// `entry` tracks the current values while iterating.
	for _, entry := range canonical {
		parts = append(parts, entry)
	}
	sort.Strings(parts)
	return HashStrings(parts)
}

// Load replaces the runtime validator registry with a cloned snapshot.
func (r *ValidatorRegistry) Load(snapshot map[string]ValidatorRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = make(map[string]*ValidatorRecord, len(snapshot))
	// `id` and `rec` track the current position in the related collection.
	for id, rec := range snapshot {
		// `cloned` stores the value produced by this operation.
		cloned := rec
		cloned.ActiveHeights = append([]uint64{}, rec.ActiveHeights...)
		cloned.SignedHeights = append([]uint64{}, rec.SignedHeights...)
		r.records[id] = &cloned
	}
}

// cloneValidatorRecord clones validator record.
func cloneValidatorRecord(rec *ValidatorRecord) ValidatorRecord {
	if rec == nil {
		return ValidatorRecord{}
	}
	// `out` stores the result produced by this operation.
	out := *rec
	out.ActiveHeights = append([]uint64{}, rec.ActiveHeights...)
	out.SignedHeights = append([]uint64{}, rec.SignedHeights...)
	return out
}

// normalizeOnChainValidatorStatus normalizes on chain validator status.
func normalizeOnChainValidatorStatus(raw string) ValidatorState {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(ValidatorActive):
		return ValidatorActive
	case string(ValidatorInactive):
		return ValidatorInactive
	case string(ValidatorJailed):
		return ValidatorJailed
	case string(ValidatorRemoved):
		return ValidatorRemoved
	case string(ValidatorExited):
		return ValidatorExited
	case string(ValidatorPending):
		return ValidatorPending
	default:
		return ValidatorPending
	}
}

func validatorStateIsInactive(status ValidatorState) bool {
	return status == ValidatorInactive || status == ValidatorPending
}

func validatorStateIsRemoved(status ValidatorState) bool {
	return status == ValidatorRemoved || status == ValidatorExited
}

func validatorRecordIsConsensusActive(rec ValidatorRecord, height uint64) bool {
	if rec.Status != ValidatorActive || validatorStateIsRemoved(rec.Status) {
		return false
	}
	if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
		return false
	}
	if rec.JoinHeight > 0 && height < rec.JoinHeight {
		return false
	}
	return validatorPassesStakeGate(rec.ID, rec.Stake)
}

// stakeInt64FromUint64 implements the stake int64 from uint64 helper.
func stakeInt64FromUint64(v uint64) int64 {
	if v > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(v)
}

// onChainValidatorPubKeyForID implements the on chain validator pub key for id helper.
func onChainValidatorPubKeyForID(id string) []byte {
	id = normalizeValidatorID(id)
	if id == "" {
		return nil
	}
	validatorPubKeysMu.RLock()
	defer validatorPubKeysMu.RUnlock()
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := GenesisValidatorPubKeys[id]
	if !ok || len(pub) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]byte, len(pub))
	copy(out, pub)
	return out
}

// decodeConsensusPubKeyHex implements the decode consensus pub key hex helper.
func decodeConsensusPubKeyHex(raw string) ([]byte, error) {
	raw = strings.ToLower(strings.TrimSpace(stripHexPrefix(raw)))
	if raw == "" {
		return nil, nil
	}
	// `pub` and `err` store the error produced by this operation.
	pub, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid validator_pubkey")
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid validator_pubkey length")
	}
	return pub, nil
}

// normalizeConsensusPubKeyHex normalizes consensus pub key hex.
func normalizeConsensusPubKeyHex(raw string) string {
	// `pub` and `err` store the error produced by this operation.
	pub, err := decodeConsensusPubKeyHex(raw)
	if err != nil || len(pub) == 0 {
		return ""
	}
	return strings.ToLower(hex.EncodeToString(pub))
}

// validatorRecordConsensusPubKeyHex implements the validator record consensus pub key hex helper.
func validatorRecordConsensusPubKeyHex(rec ValidatorRecord, fallbackID string) string {
	// `normalized` stores the value produced by this operation.
	if normalized := normalizeConsensusPubKeyHex(rec.ConsensusPubKey); normalized != "" {
		return normalized
	}
	if fallbackID == "" {
		return ""
	}
	return consensusPubKeyHexForValidatorID(fallbackID)
}

// validatorRecordPubKeyBytes implements the validator record pub key bytes helper.
func validatorRecordPubKeyBytes(rec ValidatorRecord, fallbackID string) []byte {
	// `raw` stores the value produced by this operation.
	raw := validatorRecordConsensusPubKeyHex(rec, fallbackID)
	if raw == "" {
		return nil
	}
	// `pub` and `err` store the error produced by this operation.
	pub, err := hex.DecodeString(raw)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil
	}
	return append([]byte(nil), pub...)
}

// validatorConsensusPubKeyHexFromSnapshot implements the validator consensus pub key hex from snapshot helper.
func validatorConsensusPubKeyHexFromSnapshot(snapshot map[string]ValidatorRecord, id string) string {
	id = normalizeValidatorID(id)
	if id == "" {
		return ""
	}
	// `rec` and `ok` store whether the related condition is satisfied.
	if rec, ok := validatorRecordFromStakeSnapshot(snapshot, id); ok {
		// `normalized` stores the value produced by this operation.
		if normalized := validatorRecordConsensusPubKeyHex(rec, ""); normalized != "" {
			return normalized
		}
	}
	return consensusPubKeyHexForValidatorID(id)
}

// validatorConsensusPubKeyAnchorSource implements the validator consensus pub key anchor source helper.
func validatorConsensusPubKeyAnchorSource(snapshot map[string]ValidatorRecord, id string) string {
	id = normalizeValidatorID(id)
	if id == "" {
		return ""
	}
	// `rec` and `ok` store whether the related condition is satisfied.
	if rec, ok := validatorRecordFromStakeSnapshot(snapshot, id); ok {
		if normalizeConsensusPubKeyHex(rec.ConsensusPubKey) != "" {
			return "registry_snapshot"
		}
	}
	if consensusPubKeyHexForValidatorID(id) != "" {
		return "genesis"
	}
	return ""
}

// validatorHasAnchoredConsensusPubKeyFromSnapshot implements the validator has anchored consensus pub key from snapshot helper.
func validatorHasAnchoredConsensusPubKeyFromSnapshot(snapshot map[string]ValidatorRecord, id string) bool {
	return validatorConsensusPubKeyAnchorSource(snapshot, id) != ""
}

// validateStakeConsensusPubKey validates stake consensus pub key.
func validateStakeConsensusPubKey(tx Transaction, snapshot map[string]ValidatorRecord) (string, error) {
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := normalizeValidatorID(tx.To)
	if validatorID == "" {
		return "", nil
	}
	// `anchored` stores the value produced by this operation.
	anchored := ""
	if snapshot != nil {
		if rec, ok := validatorRecordFromStakeSnapshot(snapshot, validatorID); ok {
			anchored = validatorRecordConsensusPubKeyHex(rec, "")
		}
	} else {
		// Local transaction simulation may still consult the runtime/genesis
		// compatibility key maps. Consensus replay always supplies a non-nil
		// committed snapshot and therefore cannot reach this fallback.
		anchored = validatorConsensusPubKeyHexFromSnapshot(nil, validatorID)
	}
	// `provided` and `err` store the error produced by this operation.
	provided, err := decodeConsensusPubKeyHex(tx.ValidatorPubKey)
	if err != nil {
		return "", err
	}
	// `providedHex` stores the value produced by this operation.
	providedHex := ""
	if len(provided) == ed25519.PublicKeySize {
		providedHex = strings.ToLower(hex.EncodeToString(provided))
	}
	// `walletPubHex` stores the value produced by this operation.
	walletPubHex := normalizeConsensusPubKeyHex(tx.PublicKey)
	if providedHex != "" && walletPubHex != "" && strings.EqualFold(providedHex, walletPubHex) {
		return "", errors.New("validator_pubkey must be validator consensus key, not wallet public key")
	}
	if anchored != "" {
		if providedHex != "" && !strings.EqualFold(providedHex, anchored) {
			return "", fmt.Errorf("validator_pubkey conflicts with anchored consensus pubkey")
		}
		return anchored, nil
	}
	if snapshot == nil && validatorRecordBootstrapGovernanceSeed(validatorID) {
		return providedHex, nil
	}
	if providedHex == "" {
		return "", errors.New("validator_pubkey required for first non-core stake")
	}
	return providedHex, nil
}

// anchorConsensusPubKeyOnValidatorRecord implements the anchor consensus pub key on validator record helper.
func anchorConsensusPubKeyOnValidatorRecord(rec *ValidatorRecord, id string, provided string) {
	if rec == nil {
		return
	}
	// `normalized` stores the value produced by this operation.
	if normalized := normalizeConsensusPubKeyHex(rec.ConsensusPubKey); normalized != "" {
		rec.ConsensusPubKey = normalized
		return
	}
	// `normalized` stores the value produced by this operation.
	if normalized := normalizeConsensusPubKeyHex(provided); normalized != "" {
		rec.ConsensusPubKey = normalized
		return
	}
	// `trusted` stores the value produced by this operation.
	if trusted := consensusPubKeyHexForValidatorID(id); trusted != "" {
		rec.ConsensusPubKey = trusted
	}
}

// onChainValidatorsFromRegistrySnapshot implements the on chain validators from registry snapshot helper.
func onChainValidatorsFromRegistrySnapshot(snapshot map[string]ValidatorRecord, pendingAdds map[string]uint64, height uint64) map[string]Validator {
	if len(snapshot) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]Validator, len(snapshot))
	// `key` and `rec` track the key used to access the related value.
	for key, rec := range snapshot {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			id = normalizeValidatorID(key)
		}
		if id == "" {
			continue
		}
		// `stake` stores the value produced by this operation.
		stake := canonicalVotingPowerFromStake(rec.Stake)
		// `status` stores the value produced by this operation.
		status := strings.ToUpper(strings.TrimSpace(string(rec.Status)))
		if status == "" {
			status = string(ValidatorPending)
		}
		// `activationHeight` stores the value produced by this operation.
		activationHeight := rec.JoinHeight
		// `act` and `ok` store whether the related condition is satisfied.
		if act, ok := pendingAdds[id]; ok && act > 0 {
			activationHeight = act
		} else if rec.LastActive > 0 {
			activationHeight = rec.LastActive
		} else if rec.Status == ValidatorActive {
			activationHeight = height
		}
		// `pubKey` stores the key used to access the related value.
		pubKey := validatorRecordPubKeyBytes(rec, id)
		// `address` stores the address used by this operation.
		address := canonicalValidatorAddressForID(id)
		if len(pubKey) == ed25519.PublicKeySize {
			address = canonicalAddressFromPubKey(ed25519.PublicKey(pubKey))
		}
		if address == "" {
			address = strings.ToLower(id)
		}
		out[id] = Validator{
			Address:          address,
			PubKey:           pubKey,
			Stake:            stake,
			VotingPower:      stake,
			Status:           status,
			JoinHeight:       rec.JoinHeight,
			ActivationHeight: activationHeight,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validatorRegistrySnapshotFromOnChainValidators implements the validator registry snapshot from on chain validators helper.
func validatorRegistrySnapshotFromOnChainValidators(state map[string]Validator) map[string]ValidatorRecord {
	if len(state) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]ValidatorRecord, len(state))
	// `key` and `val` track the key used to access the related value.
	for key, val := range state {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(key)
		if id == "" {
			id = normalizeValidatorID(val.Address)
		}
		if id == "" {
			continue
		}
		// `stake` stores the value produced by this operation.
		stake := val.Stake
		if stake == 0 && val.VotingPower > 0 {
			stake = val.VotingPower
		}
		// `rec` stores the value produced by this operation.
		rec := ValidatorRecord{
			ID:               id,
			ConsensusPubKey:  strings.ToLower(hex.EncodeToString(val.PubKey)),
			GovernanceSigner: containsValidatorID(ConfigAuthCoreValidators, id),
			Stake:            stakeInt64FromUint64(stake),
			Reputation:       protocolValidatorReputationInitialValue(),
			Status:           normalizeOnChainValidatorStatus(val.Status),
			JoinHeight:       val.JoinHeight,
			ActiveHeights:    make([]uint64, 0),
			SignedHeights:    make([]uint64, 0),
		}
		if rec.Status == ValidatorActive {
			rec.LastActive = val.ActivationHeight
			if rec.LastActive == 0 {
				rec.LastActive = rec.JoinHeight
			}
		}
		out[id] = rec
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// trimHeights implements the trim heights helper.
func trimHeights(heights []uint64, minHeight uint64) []uint64 {
	if len(heights) == 0 {
		return heights
	}
	// `idx` stores the current position in the related collection.
	idx := 0
	for idx < len(heights) && heights[idx] < minHeight {
		idx++
	}
	if idx == 0 {
		return heights
	}
	// `trimmed` stores the value produced by this operation.
	trimmed := heights[idx:]
	// `out` stores the result produced by this operation.
	out := make([]uint64, len(trimmed))
	copy(out, trimmed)
	return out
}

// inactivityPenaltyJailBlocks implements the inactivity penalty jail blocks helper.
func inactivityPenaltyJailBlocks() uint64 {
	return protocolValidatorInactivityPenaltyJailBlocksValue()
}

// inactivityPenaltyTier implements the inactivity penalty tier helper.
func inactivityPenaltyTier(offenses uint64) uint64 {
	switch {
	case offenses <= 1:
		return 1
	case offenses == 2:
		return 2
	default:
		return 3
	}
}

// inactivityPenaltyBurnBPSForCount implements the inactivity penalty burn bps for count helper.
func inactivityPenaltyBurnBPSForCount(offenses uint64) uint64 {
	// `base` stores the value produced by this operation.
	base := protocolValidatorInactivityPenaltyBurnBPSValue()
	if base > 10000 {
		base = 10000
	}
	if base == 0 {
		return 0
	}
	switch inactivityPenaltyTier(offenses) {
	case 1:
		// `burn` stores the value produced by this operation.
		burn := base / 2
		if burn == 0 {
			burn = 1
		}
		return burn
	case 2:
		return base
	default:
		if base > 5000 {
			return 10000
		}
		return base * 2
	}
}

// inactivityPenaltyJailBlocksForCount implements the inactivity penalty jail blocks for count helper.
func inactivityPenaltyJailBlocksForCount(offenses uint64) uint64 {
	// `base` stores the value produced by this operation.
	base := inactivityPenaltyJailBlocks()
	switch inactivityPenaltyTier(offenses) {
	case 1:
		return base
	case 2:
		return base * 2
	default:
		return base * 3
	}
}

// inactivityPenaltyCooldownBlocks implements the inactivity penalty cooldown blocks helper.
func inactivityPenaltyCooldownBlocks() uint64 {
	return protocolValidatorInactivityPenaltyCooldownBlocksValue()
}

// MarkValidatorInactivityPenalty marks validator inactivity penalty.
func MarkValidatorInactivityPenalty(id string, height uint64) bool {
	id = normalizeValidatorID(id)
	if id == "" || height == 0 || !protocolValidatorInactivityPenaltyEnabledFlag() {
		return false
	}

	// `cooldown` stores the value produced by this operation.
	cooldown := inactivityPenaltyCooldownBlocks()

	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()

	// `rec` stores the value produced by this operation.
	rec := ensureValidatorRecordLocked(id, height)
	if rec == nil {
		return false
	}
	if validatorStateIsRemoved(rec.Status) {
		return false
	}
	if cooldown > 0 && rec.LastInactivityPenaltyHeight > 0 &&
		height < rec.LastInactivityPenaltyHeight+cooldown {
		return false
	}

	rec.LastInactivityPenaltyHeight = height
	rec.InactivityPenalties++
	return true
}

// applyReputationDelta applies reputation delta.
func (r *ValidatorRecord) applyReputationDelta(delta float64) {
	r.Reputation += delta
	if r.Reputation < 0 {
		r.Reputation = 0
	}
	if r.Reputation > 1 {
		r.Reputation = 1
	}
}

// recordActivity implements the record activity helper.
func (r *ValidatorRecord) recordActivity(height uint64, signed bool) {
	if height == 0 {
		return
	}
	r.ActiveHeights = append(r.ActiveHeights, height)
	if signed {
		r.SignedHeights = append(r.SignedHeights, height)
		r.LastActive = height
		r.LastSeenHeight = height
		r.LastVoteHeight = height
		r.HeartbeatHeight = height
	}
	// `minHeight` stores the value produced by this operation.
	minHeight := uint64(0)
	uptimeWindow := protocolValidatorUptimeWindowValue()
	if uptimeWindow > 0 && height >= uptimeWindow {
		minHeight = height - uptimeWindow + 1
	}
	r.ActiveHeights = trimHeights(r.ActiveHeights, minHeight)
	r.SignedHeights = trimHeights(r.SignedHeights, minHeight)
	// `activeCount` stores the measured quantity used by this operation.
	activeCount := uint64(len(r.ActiveHeights))
	// `signedCount` stores the measured quantity used by this operation.
	signedCount := uint64(len(r.SignedHeights))
	r.UptimeWindowCounter = activeCount
	if activeCount >= signedCount {
		r.MissedBlocksWindow = activeCount - signedCount
	} else {
		r.MissedBlocksWindow = 0
	}
}

// UptimeScore implements the uptime score helper.
func (r *ValidatorRecord) UptimeScore(height uint64) float64 {
	_ = height
	// `activeCount` stores the measured quantity used by this operation.
	activeCount := uint64(len(r.ActiveHeights))
	if activeCount == 0 {
		return 0
	}
	// `signedCount` stores the measured quantity used by this operation.
	signedCount := uint64(len(r.SignedHeights))
	return float64(signedCount) / float64(activeCount)
}

// PenaltyScore implements the penalty score helper.
func (r *ValidatorRecord) PenaltyScore() float64 {
	// `missed` stores the value produced by this operation.
	missedWeight, badExecWeight, doubleSignWeight, disconnectWeight := protocolPenaltyWeights()
	missed := float64(r.MissedBlocksWindow) * missedWeight
	// `badExec` stores the value produced by this operation.
	badExec := float64(r.BadExecution) * badExecWeight
	// `doubleSign` stores the value produced by this operation.
	doubleSign := float64(r.DoubleSign) * doubleSignWeight
	// `disconnect` stores the value produced by this operation.
	disconnect := float64(r.DisconnectPattern) * disconnectWeight
	return missed + badExec + doubleSign + disconnect
}

type ValidatorStateMachine struct{}

type ValidatorScoreEngine struct{}

type DynamicValidatorSelector struct {
	// `ScoreEngine` stores the value associated with this record.
	ScoreEngine ValidatorScoreEngine
	// `StateMachine` stores the value associated with this record.
	StateMachine ValidatorStateMachine
}

// `ValidatorSelector` stores whether the related condition is satisfied.
var ValidatorSelector = DynamicValidatorSelector{
	ScoreEngine:  ValidatorScoreEngine{},
	StateMachine: ValidatorStateMachine{},
}

// ensureValidatorRecordLocked implements the ensure validator record locked helper.
func ensureValidatorRecordLocked(id string, height uint64) *ValidatorRecord {
	if id == "" {
		return nil
	}
	// `rec` and `ok` store whether the related condition is satisfied.
	if rec, ok := GlobalValidatorRegistry.records[id]; ok {
		return rec
	}
	// `rec` stores the value produced by this operation.
	rec := &ValidatorRecord{
		ID:              id,
		ConsensusPubKey: consensusPubKeyHexForValidatorID(id),
		Stake:           0,
		Reputation:      protocolValidatorReputationInitialValue(),
		Status:          ValidatorPending,
		JoinHeight:      height,
		ActiveHeights:   make([]uint64, 0),
		SignedHeights:   make([]uint64, 0),
	}
	GlobalValidatorRegistry.records[id] = rec
	return rec
}

// Update transitions one validator record between active, jailed, pending, and exited states.
func (ValidatorStateMachine) Update(rec *ValidatorRecord, height uint64) {
	if rec == nil {
		return
	}
	defer func() {
		if rec.Status == ValidatorActive {
			rec.VotingPower = 1
		} else {
			rec.VotingPower = 0
		}
	}()
	if validatorStateIsRemoved(rec.Status) {
		return
	}
	if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
		rec.Status = ValidatorJailed
		return
	}
	if rec.JoinHeight > 0 && height < rec.JoinHeight {
		if rec.Status != ValidatorInactive {
			rec.Status = ValidatorPending
		}
		return
	}
	if rec.Status == ValidatorJailed {
		if height >= rec.JailUntilHeight && rec.Reputation >= protocolValidatorReputationRecoveryThresholdValue() {
			rec.Status = ValidatorActive
			rec.JailUntilHeight = 0
		}
		return
	}
	// ACTIVE, PENDING, and INACTIVE are committed lifecycle states. Local
	// stake, reputation, or heartbeat evaluation may recommend a transition but
	// cannot rewrite consensus membership.
	if rec.Status == ValidatorActive {
		return
	}
	if validatorStateIsInactive(rec.Status) {
		return
	}
	if !validatorPassesStakeGate(rec.ID, rec.Stake) {
		if rec.Status != ValidatorInactive {
			rec.Status = ValidatorPending
		}
		return
	}
	if rec.Reputation < protocolValidatorReputationRecoveryThresholdValue() {
		if rec.Status != ValidatorInactive {
			rec.Status = ValidatorPending
		}
		return
	}
	rec.Status = ValidatorActive
}

// Score implements the score helper.
func (ValidatorScoreEngine) Score(rec *ValidatorRecord, maxStakeScore float64, maxPenalty float64) float64 {
	if rec == nil {
		return 0
	}
	// `capStake` stores the value produced by this operation.
	capStake := int64(float64(FixedTotalSupply) * protocolValidatorStakeCapPctValue())
	// `effective` stores the value produced by this operation.
	effective := rec.Stake
	if capStake > 0 && effective > capStake {
		effective = capStake
	}
	// `stakeScore` stores the value produced by this operation.
	stakeScore := math.Log(float64(effective) + 1)
	// `stakeNorm` stores the value produced by this operation.
	stakeNorm := normalizeScore(stakeScore, maxStakeScore)
	// `uptime` stores the value produced by this operation.
	uptime := rec.UptimeScore(rec.LastActive)
	// `penalty` stores the value produced by this operation.
	penalty := normalizeScore(rec.PenaltyScore(), maxPenalty)

	// `finalScore` stores the value produced by this operation.
	finalScore := 0.45*stakeNorm + 0.25*rec.Reputation + 0.20*uptime - 0.10*penalty
	if finalScore < 0 {
		finalScore = 0
	}
	return finalScore
}

// normalizeScore normalizes score.
func normalizeScore(value float64, max float64) float64 {
	if max <= 0 {
		return 0
	}
	if value <= 0 {
		return 0
	}
	return value / max
}

// Select returns the deterministic active validator rotation for a height and target size.
func (s DynamicValidatorSelector) Select(height uint64, target int) []string {
	// `unlimited` stores the value produced by this operation.
	unlimited := false
	if target < 0 {
		unlimited = true
	} else if target == 0 {
		target = 1
	}

	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	// Hold the lock while scoring to keep registry updates consistent.

	records := make([]*ValidatorRecord, 0, len(GlobalValidatorRegistry.records))
	// `rec` tracks the current values while iterating.
	for _, rec := range GlobalValidatorRegistry.records {
		s.StateMachine.Update(rec, height)
		if isProtocolValidatorBanned(rec.ID) {
			continue
		}
		if validatorStateIsRemoved(rec.Status) {
			continue
		}
		if rec.JoinHeight > 0 && height < rec.JoinHeight {
			continue
		}
		records = append(records, rec)
	}

	if len(records) == 0 {
		return nil
	}

	// `maxStakeScore` stores the value produced by this operation.
	maxStakeScore := 0.0
	// `maxPenalty` stores the value produced by this operation.
	maxPenalty := 0.0
	// `capStake` stores the value produced by this operation.
	capStake := int64(float64(FixedTotalSupply) * protocolValidatorStakeCapPctValue())
	// `rec` tracks the current values while iterating.
	for _, rec := range records {
		// `effective` stores the value produced by this operation.
		effective := rec.Stake
		if capStake > 0 && effective > capStake {
			effective = capStake
		}
		// `stakeScore` stores the value produced by this operation.
		stakeScore := math.Log(float64(effective) + 1)
		if stakeScore > maxStakeScore {
			maxStakeScore = stakeScore
		}
		// `penalty` stores the value produced by this operation.
		penalty := rec.PenaltyScore()
		if penalty > maxPenalty {
			maxPenalty = penalty
		}
	}

	// `scored` stores the value produced by this operation.
	scored := make([]*ValidatorRecord, 0, len(records))
	// `rec` tracks the current values while iterating.
	for _, rec := range records {
		if rec.Status != ValidatorActive {
			continue
		}
		rec.LastScore = s.ScoreEngine.Score(rec, maxStakeScore, maxPenalty)
		scored = append(scored, rec)
	}

	if len(scored) == 0 {
		return nil
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].LastScore == scored[j].LastScore {
			if scored[i].Stake == scored[j].Stake {
				return scored[i].ID < scored[j].ID
			}
			return scored[i].Stake > scored[j].Stake
		}
		return scored[i].LastScore > scored[j].LastScore
	})

	if unlimited {
		target = len(scored)
	}
	if len(scored) > target {
		scored = scored[:target]
	}

	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(scored))
	// `rec` tracks the current values while iterating.
	for _, rec := range scored {
		out = append(out, rec.ID)
	}
	sort.Strings(out)
	return out
}

// ApplyValidatorStake updates a validator's recorded stake balance at a given height.
func ApplyValidatorStake(id string, amount int64, height uint64) {
	if id == "" || amount == 0 {
		return
	}
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	// `rec` stores the value produced by this operation.
	rec := ensureValidatorRecordLocked(id, height)
	if rec == nil {
		return
	}
	rec.Stake += amount
	if rec.Stake < 0 {
		rec.Stake = 0
	}
}

// ApplyValidatorPenalty records slash evidence, adjusts reputation, and jails or exits validators when required.
func ApplyValidatorPenalty(id string, reason string, height uint64) {
	if id == "" {
		return
	}
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	// `rec` stores the value produced by this operation.
	rec := ensureValidatorRecordLocked(id, height)
	if rec == nil {
		return
	}
	rec.TotalSlashes++
	rec.applyReputationDelta(protocolReputationSlashDeltaValue())

	switch reason {
	case "double_proposal", "double_sign":
		rec.DoubleSign++
		rec.JailUntilHeight = height + protocolJailDoubleSignBlocksValue()
		rec.Status = ValidatorJailed
	case "invalid_block", "invalid_proposer", "fake_tx_execution", "exec_mismatch", "exec_equivocation":
		rec.BadExecution++
		rec.JailUntilHeight = height + protocolJailBadExecutionBlocksValue()
		rec.Status = ValidatorJailed
	case "censorship_threshold_exceeded", "verified_censorship", "systematic_censorship":
		rec.BadExecution++
		if rec.JailUntilHeight < height+protocolJailBadExecutionBlocksValue() {
			rec.JailUntilHeight = height + protocolJailBadExecutionBlocksValue()
			rec.Status = ValidatorJailed
		}
	case "offline_inactive", "inactive":
		rec.DisconnectPattern++
		rec.SuspensionRecommended = true
		rec.SuspensionRecommendedHeight = height
	}
	// Even repeated severe evidence can only jail. Permanent removal is a
	// certified on-chain registry transition, never a local penalty side effect.
}

// UpdateValidatorStates advances every registry record through the validator state machine for the height.
func UpdateValidatorStates(height uint64) {
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	// `rec` tracks the current values while iterating.
	for _, rec := range GlobalValidatorRegistry.records {
		ValidatorStateMachine{}.Update(rec, height)
	}
}

// UpdateValidatorMetricsFromBlock updates validator metrics from block.
func (n *Node) UpdateValidatorMetricsFromBlock(block Block) {
	n.updateValidatorMetricsFromBlockWithRegistry(
		block,
		GlobalValidatorRegistry.Snapshot(),
	)
}

// updateValidatorMetricsFromBlockWithRegistry applies validator registry
// transitions from one explicit committed parent view. Quorum witness subsets
// are deliberately excluded: they are valid certificate evidence but are not
// canonical block identity and may differ between honest nodes.
func (n *Node) updateValidatorMetricsFromBlockWithRegistry(
	block Block,
	validatorRegistry map[string]ValidatorRecord,
) {
	// `height` stores the value produced by this operation.
	height := block.ID
	if height == 0 {
		return
	}
	validatorRegistry = copyValidatorRegistrySnapshot(validatorRegistry)
	if len(validatorRegistry) > 0 {
		GlobalValidatorRegistry.Load(validatorRegistry)
	}

	// `activeSet` stores the value produced by this operation.
	activeSet := protocolRewardValidatorsFromRegistrySnapshot(height, validatorRegistry)
	// `activeMap` stores the value produced by this operation.
	activeMap := make(map[string]struct{}, len(activeSet))
	// `id` tracks the current position in the related collection.
	for _, id := range activeSet {
		activeMap[id] = struct{}{}
	}

	// A finalized block proves committee quorum, but its exact witness subset is
	// not canonical. Advance every deterministic active record identically.
	signerMap := make(map[string]struct{}, len(activeSet))
	for _, id := range activeSet {
		signerMap[id] = struct{}{}
	}

	// `pendingAdds` stores the value produced by this operation.
	pendingAdds := make(map[string]uint64)
	// `pendingDrops` stores the value produced by this operation.
	pendingDrops := make(map[string]uint64)

	GlobalValidatorRegistry.mu.Lock()
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range block.Transactions {
		if tx.Type != TxStake && tx.Type != TxUnstake {
			continue
		}
		// `vid` stores the value produced by this operation.
		vid := strings.TrimSpace(tx.To)
		if vid == "" {
			continue
		}
		// `rec` stores the value produced by this operation.
		rec := ensureValidatorRecordLocked(vid, height)
		if rec == nil {
			continue
		}
		// `hadConsensusPubKey` stores the key used to access the related value.
		hadConsensusPubKey := normalizeConsensusPubKeyHex(rec.ConsensusPubKey) != ""
		if tx.Type == TxStake {
			anchorConsensusPubKeyOnValidatorRecord(rec, vid, tx.ValidatorPubKey)
		}
		// `hasConsensusPubKey` stores the key used to access the related value.
		hasConsensusPubKey := normalizeConsensusPubKeyHex(rec.ConsensusPubKey) != ""
		// `oldStake` stores the value produced by this operation.
		oldStake := rec.Stake
		// `delta` stores the value produced by this operation.
		delta := int64(tx.Amount)
		if tx.Type == TxUnstake {
			delta = -delta
		}
		rec.Stake += delta
		if rec.Stake < 0 {
			rec.Stake = 0
		}
		// `lostStakeGate` and `gainedStakeGate` store the value produced by this operation.
		lostStakeGate, gainedStakeGate := validatorStakeGateTransition(vid, oldStake, rec.Stake)
		if lostStakeGate {
			rec.JoinHeight = 0
			pendingDrops[vid] = height + validatorSetActivationDelayBlocks()
		} else if gainedStakeGate {
			// `activationHeight` stores the value produced by this operation.
			activationHeight := height + validatorSetActivationDelayBlocks()
			if rec.JoinHeight == 0 || rec.JoinHeight <= height || activationHeight < rec.JoinHeight {
				rec.JoinHeight = activationHeight
			}
			pendingAdds[vid] = rec.JoinHeight
		} else if tx.Type == TxStake && validatorPassesStakeGate(vid, rec.Stake) && !hadConsensusPubKey && hasConsensusPubKey {
			// `activationHeight` stores the value produced by this operation.
			activationHeight := height + validatorSetActivationDelayBlocks()
			if rec.JoinHeight == 0 || rec.JoinHeight <= height || activationHeight < rec.JoinHeight {
				rec.JoinHeight = activationHeight
			}
			pendingAdds[vid] = rec.JoinHeight
		}
	}

	// `id` tracks the current position in the related collection.
	for _, id := range activeSet {
		// `rec` stores the value produced by this operation.
		rec := ensureValidatorRecordLocked(id, height)
		if rec == nil {
			continue
		}
		// `signed` stores the value produced by this operation.
		_, signed := signerMap[id]
		rec.recordActivity(height, signed)
		if signed {
			rec.applyReputationDelta(protocolReputationCorrectDeltaValue())
		} else {
			rec.MissedBlocks++
			rec.applyReputationDelta(protocolReputationMismatchDeltaValue())
		}
		if rec.UptimeScore(height) >= protocolValidatorLongUptimeThresholdValue() {
			rec.applyReputationDelta(protocolReputationLongUptimeDeltaValue())
		}
		if rec.MissedBlocksWindow >= protocolMassMissThresholdValue() {
			rec.SuspensionRecommended = true
			if rec.SuspensionRecommendedHeight == 0 {
				rec.SuspensionRecommendedHeight = height
			}
		}
		// In deterministic mode, inactivity removals are derived from finalized
		// chain data in queueDeterministicInactiveRemovals to keep all nodes identical.
		inactiveBlocks := protocolValidatorInactiveBlocksValue()
		if !protocolDeterministicValidatorSelectionEnabled() && inactiveBlocks > 0 && rec.MissedBlocksWindow >= inactiveBlocks {
			// `activationHeight` stores the value produced by this operation.
			activationHeight := height + validatorSetActivationDelayBlocks()
			// `existing` and `ok` store whether the related condition is satisfied.
			if existing, ok := pendingDrops[id]; !ok || activationHeight < existing {
				pendingDrops[id] = activationHeight
			}
			if protocolValidatorInactivePermanentRemoveEnabled() {
				rec.Status = ValidatorRemoved
				rec.JailUntilHeight = 0
				rec.JoinHeight = 0
			}
		}
		ValidatorStateMachine{}.Update(rec, height)
	}

	// `signer` tracks the current values while iterating.
	for signer := range signerMap {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := activeMap[signer]; ok {
			continue
		}
		// `rec` stores the value produced by this operation.
		rec := ensureValidatorRecordLocked(signer, height)
		if rec == nil {
			continue
		}
		rec.recordActivity(height, true)
		rec.applyReputationDelta(protocolReputationCorrectDeltaValue())
		ValidatorStateMachine{}.Update(rec, height)
	}

	// `rec` tracks the current values while iterating.
	for _, rec := range GlobalValidatorRegistry.records {
		ValidatorStateMachine{}.Update(rec, height)
	}
	GlobalValidatorRegistry.mu.Unlock()

	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDsFromMapKeys(pendingAdds) {
		n.queuePendingValidator(id, pendingAdds[id])
	}
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDsFromMapKeys(pendingDrops) {
		n.queuePendingValidatorRemoval(id, pendingDrops[id])
	}
}

// applySnapshotValidatorRegistry applies snapshot validator registry.
func (n *Node) applySnapshotValidatorRegistry(snapshot StateSnapshot) {
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := snapshot.ValidatorRegistry
	if len(registrySnapshot) == 0 && len(snapshot.StateValidators) > 0 {
		registrySnapshot = validatorRegistrySnapshotFromOnChainValidators(snapshot.StateValidators)
	}
	if len(registrySnapshot) > 0 {
		GlobalValidatorRegistry.Load(registrySnapshot)
	} else {
		// Fallback: rebuild from snapshot validators.
		GlobalValidatorRegistry.mu.Lock()
		GlobalValidatorRegistry.records = make(map[string]*ValidatorRecord)
		GlobalValidatorRegistry.mu.Unlock()
		if len(snapshot.Validators) > 0 {
			// `ids` stores the current position in the related collection.
			ids := canonicalValidatorIDsFromMapKeys(snapshot.Validators)
			bootstrapValidatorRegistry(ids, snapshot.Height)
		}
	}

	// Snapshot validator_registry may be stale around stake transitions.
	// Reconcile stake from ledger locks to avoid onboarding starvation ("no_stake")
	// when ledger already contains valid stake proofs.
	n.reconcileSnapshotRegistryStakeFromLedger(snapshot.Ledger, snapshot.Height)
}

// reconcileSnapshotRegistryStakeFromLedger implements the reconcile snapshot registry stake from ledger helper.
func (n *Node) reconcileSnapshotRegistryStakeFromLedger(ledger Ledger, height uint64) {
	if len(ledger.Stakes) == 0 {
		return
	}
	// `stakeTotals` stores the value produced by this operation.
	stakeTotals := make(map[string]int64)
	// `consensusPubKeys` stores the value produced by this operation.
	consensusPubKeys := make(map[string]string)
	// `key` and `rec` track the key used to access the related value.
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		// `keyID` stores the key used to access the related value.
		keyID := normalizeValidatorID(parts[1])
		// `recID` stores the value produced by this operation.
		recID := normalizeValidatorID(rec.ValidatorID)
		if keyID == "" || recID == "" || keyID != recID {
			continue
		}
		stakeTotals[keyID] += int64(rec.Amount)
		// `pubKey` stores the key used to access the related value.
		if pubKey := normalizeConsensusPubKeyHex(rec.ConsensusPubKey); pubKey != "" {
			consensusPubKeys[keyID] = pubKey
		}
	}
	if len(stakeTotals) == 0 {
		return
	}

	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	// `id` and `total` track the measured quantity used by this operation.
	for id, total := range stakeTotals {
		// `rec` and `ok` store whether the related condition is satisfied.
		rec, ok := GlobalValidatorRegistry.records[id]
		if !ok || rec == nil {
			// `consensusPubKey` stores the key used to access the related value.
			consensusPubKey := consensusPubKeyHexForValidatorID(id)
			// `pubKey` stores the key used to access the related value.
			if pubKey := consensusPubKeys[id]; pubKey != "" {
				consensusPubKey = pubKey
			}
			rec = &ValidatorRecord{
				ID:              id,
				ConsensusPubKey: consensusPubKey,
				Stake:           0,
				Reputation:      protocolValidatorReputationInitialValue(),
				Status:          ValidatorPending,
				JoinHeight:      height,
				ActiveHeights:   make([]uint64, 0),
				SignedHeights:   make([]uint64, 0),
			}
			GlobalValidatorRegistry.records[id] = rec
		}
		if rec.JoinHeight == 0 {
			rec.JoinHeight = height
		}
		if rec.ConsensusPubKey == "" {
			rec.ConsensusPubKey = consensusPubKeys[id]
		}
		if rec.Stake != total {
			rec.Stake = total
		}
		ValidatorStateMachine{}.Update(rec, height)
	}
}

// bootstrapValidatorRegistry implements the bootstrap validator registry helper.
func bootstrapValidatorRegistry(ids []string, height uint64) {
	if len(ids) == 0 {
		return
	}
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		if id == "" {
			continue
		}
		// `rec` and `ok` store whether the related condition is satisfied.
		rec, ok := GlobalValidatorRegistry.records[id]
		if !ok {
			rec = &ValidatorRecord{
				ID:               id,
				ConsensusPubKey:  consensusPubKeyHexForValidatorID(id),
				GovernanceSigner: validatorRecordBootstrapGovernanceSeed(id),
				Stake:            0,
				Reputation:       protocolValidatorReputationInitialValue(),
				Status:           ValidatorPending,
				JoinHeight:       height,
				ActiveHeights:    make([]uint64, 0),
				SignedHeights:    make([]uint64, 0),
			}
			GlobalValidatorRegistry.records[id] = rec
		}
		if rec.ConsensusPubKey == "" {
			rec.ConsensusPubKey = consensusPubKeyHexForValidatorID(id)
		}
		if validatorRecordBootstrapGovernanceSeed(id) {
			rec.GovernanceSigner = true
		}
		if !validatorPassesStakeGate(rec.ID, rec.Stake) {
			rec.Stake = ConsensusValidatorMinStake
		}
		if !validatorStateIsRemoved(rec.Status) {
			rec.Status = ValidatorActive
			rec.VotingPower = 1
		}
	}
}

// validatorRecordBootstrapGovernanceSeed implements the validator record bootstrap governance seed helper.
func validatorRecordBootstrapGovernanceSeed(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	// `seeds` stores the value produced by this operation.
	seeds := canonicalValidatorIDs(ConfigAuthCoreValidators)
	return containsValidatorID(seeds, id)
}

// consensusPubKeyHexForValidatorID implements the consensus pub key hex for validator id helper.
func consensusPubKeyHexForValidatorID(id string) string {
	id = normalizeValidatorID(id)
	if id == "" {
		return ""
	}
	// `pub` stores the value produced by this operation.
	pub := onChainValidatorPubKeyForID(id)
	if len(pub) != ed25519.PublicKeySize {
		return ""
	}
	return strings.ToLower(hex.EncodeToString(pub))
}

// activeSetTarget implements the active set target helper.
func (n *Node) activeSetTarget() int {
	if n == nil {
		return minActiveValidatorsFloor()
	}
	if activeSetModeHybridScoreRotation() {
		return validatorHybridMaxActiveValidators()
	}
	// `configured` stores the configuration used by this operation.
	configured := protocolValidatorActiveSetSizeValue()
	if configured < 0 {
		return configured
	}
	// `floor` stores the value produced by this operation.
	floor := minActiveValidatorsFloor()
	if configured > 0 {
		if configured < floor {
			return floor
		}
		return configured
	}
	if len(n.GenesisValidators) > 0 {
		if len(n.GenesisValidators) < floor {
			return floor
		}
		return len(n.GenesisValidators)
	}
	return floor
}

// normalizeActiveSetMode normalizes active set mode.
func normalizeActiveSetMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "adaptive_committee":
		return "adaptive_committee"
	case "hybrid_score_rotation", "hybrid", "score_rotation":
		return "hybrid_score_rotation"
	default:
		return "adaptive_committee"
	}
}

// activeSetModeHybridScoreRotation implements the active set mode hybrid score rotation helper.
func activeSetModeHybridScoreRotation() bool {
	return normalizeActiveSetMode(protocolValidatorActiveSetModeValue()) == "hybrid_score_rotation"
}

// validatorHybridMaxActiveValidators implements the validator hybrid max active validators helper.
func validatorHybridMaxActiveValidators() int {
	// `maxActive` stores the value produced by this operation.
	maxActive := ValidatorHybridMaxActiveValidators
	if maxActive <= 0 {
		maxActive = protocolValidatorMaxActiveCommitteeValue()
	}
	if protocolMax := protocolValidatorMaxActiveCommitteeValue(); protocolMax > 0 && maxActive > protocolMax {
		maxActive = protocolMax
	}
	// `floor` stores the value produced by this operation.
	if floor := minActiveValidatorsFloor(); maxActive < floor {
		maxActive = floor
	}
	return maxActive
}

// validatorHybridPerformanceSlots implements the validator hybrid performance slots helper.
func validatorHybridPerformanceSlots() int {
	// `maxActive` stores the value produced by this operation.
	maxActive := validatorHybridMaxActiveValidators()
	// `slots` stores the value produced by this operation.
	slots := ValidatorHybridPerformanceSlots
	if slots <= 0 {
		slots = 15
	}
	if slots > maxActive {
		slots = maxActive
	}
	return slots
}

// validatorHybridRotationSlots implements the validator hybrid rotation slots helper.
func validatorHybridRotationSlots() int {
	// `maxActive` stores the value produced by this operation.
	maxActive := validatorHybridMaxActiveValidators()
	// `performanceSlots` stores the value produced by this operation.
	performanceSlots := validatorHybridPerformanceSlots()
	// `slots` stores the value produced by this operation.
	slots := ValidatorHybridRotationSlots
	if slots <= 0 {
		slots = maxActive - performanceSlots
	}
	if slots < 0 {
		slots = 0
	}
	if performanceSlots+slots > maxActive {
		slots = maxActive - performanceSlots
	}
	if slots < 0 {
		return 0
	}
	return slots
}

// validatorHybridEffectiveStakeCap implements the validator hybrid effective stake cap helper.
func validatorHybridEffectiveStakeCap() int64 {
	if ValidatorHybridEffectiveStakeCap > 0 {
		return ValidatorHybridEffectiveStakeCap
	}
	if protocolValidatorStakeCapPctValue() > 0 {
		return int64(float64(FixedTotalSupply) * protocolValidatorStakeCapPctValue())
	}
	return 5_000_000
}

// validatorHybridEpochBlocks implements the validator hybrid epoch blocks helper.
func validatorHybridEpochBlocks() uint64 {
	if ValidatorHybridEpochBlocks == 0 {
		return 10_000
	}
	return ValidatorHybridEpochBlocks
}

// validatorHybridScoreWeights implements the validator hybrid score weights helper.
func validatorHybridScoreWeights() (float64, float64, float64, float64) {
	// `stake` stores the value produced by this operation.
	stake := ValidatorHybridStakeWeight
	// `uptime` stores the value produced by this operation.
	uptime := ValidatorHybridUptimeWeight
	// `performance` stores the value produced by this operation.
	performance := ValidatorHybridPerformanceWeight
	// `decentralization` stores the value produced by this operation.
	decentralization := ValidatorHybridDecentralizationWeight
	// `total` stores the measured quantity used by this operation.
	total := stake + uptime + performance + decentralization
	if total <= 0 {
		stake, uptime, performance, decentralization, total = 40, 35, 15, 10, 100
	}
	return float64(stake) / float64(total), float64(uptime) / float64(total), float64(performance) / float64(total), float64(decentralization) / float64(total)
}

// validatorHybridDiversityWeights implements the validator hybrid diversity weights helper.
func validatorHybridDiversityWeights() (float64, float64, float64, float64) {
	// `asn` stores the value produced by this operation.
	asn := ValidatorHybridDiversityASNWeight
	// `region` stores the value produced by this operation.
	region := ValidatorHybridDiversityRegionWeight
	// `provider` stores the value produced by this operation.
	provider := ValidatorHybridDiversityProviderWeight
	// `homePC` stores the value produced by this operation.
	homePC := ValidatorHybridDiversityHomePCWeight
	// `total` stores the measured quantity used by this operation.
	total := asn + region + provider + homePC
	if total <= 0 {
		asn, region, provider, homePC, total = 40, 30, 20, 10, 100
	}
	return float64(asn) / float64(total), float64(region) / float64(total), float64(provider) / float64(total), float64(homePC) / float64(total)
}

// validatorHybridPerformanceMinSignedBPS implements the validator hybrid performance min signed bps helper.
func validatorHybridPerformanceMinSignedBPS() int {
	// `v` stores the value produced by this operation.
	v := ValidatorHybridPerformanceMinSignedBPS
	if v <= 0 {
		return 9000
	}
	if v > 10000 {
		return 10000
	}
	return v
}

// validatorHybridPromotionWindowEpochs implements the validator hybrid promotion window epochs helper.
func validatorHybridPromotionWindowEpochs() uint64 {
	if ValidatorHybridPromotionWindowEpochs == 0 {
		return 10
	}
	return ValidatorHybridPromotionWindowEpochs
}

// validatorHybridMinimumPerformanceAgeEpochs implements the validator hybrid minimum performance age epochs helper.
func validatorHybridMinimumPerformanceAgeEpochs() uint64 {
	return ValidatorHybridMinimumPerformanceAgeEpochs
}

// validatorHybridMinimumPerformanceAgeBlocks implements the validator hybrid minimum performance age blocks helper.
func validatorHybridMinimumPerformanceAgeBlocks() uint64 {
	return validatorHybridMinimumPerformanceAgeEpochs() * validatorHybridEpochBlocks()
}

func validatorHybridEpochBucket(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	if validatorEpochSetV1EnabledAt(height) {
		return (height - 1) / validatorEpochLengthBlocks()
	}
	epochBlocks := validatorHybridEpochBlocks()
	if epochBlocks == 0 {
		return 0
	}
	return height / epochBlocks
}

// validatorHybridPromotionWindowBucket implements the validator hybrid promotion window bucket helper.
func validatorHybridPromotionWindowBucket(height uint64) uint64 {
	return validatorHybridEpochBucket(height) / validatorHybridPromotionWindowEpochs()
}

// validatorHybridMinimumOnlineForActiveCount implements the validator hybrid minimum online for active count helper.
func validatorHybridMinimumOnlineForActiveCount(activeCount int) int {
	// `maxActive` stores the value produced by this operation.
	maxActive := validatorHybridMaxActiveValidators()
	if activeCount < maxActive || maxActive <= 0 {
		return 0
	}
	// `required` stores the request data being processed.
	required := ValidatorHybridMinimumOnlineWhenFull
	if required <= 0 {
		required = 15
	}
	if required > activeCount {
		required = activeCount
	}
	return required
}

// validatorHybridMinimumOnlineOK implements the validator hybrid minimum online ok helper.
func validatorHybridMinimumOnlineOK(activeCount int, onlineCount int) bool {
	// `required` stores the request data being processed.
	required := validatorHybridMinimumOnlineForActiveCount(activeCount)
	return required == 0 || onlineCount >= required
}

// clampUnit implements the clamp unit helper.
func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type ValidatorPoolEntry struct {
	// `ID` stores the current position in the related collection.
	ID string `json:"id"`
	// `SlotType` stores the value associated with this record.
	SlotType string `json:"slot_type"`
	// `Active` stores the value associated with this record.
	Active bool `json:"active"`
	// `FinalScore` stores the value associated with this record.
	FinalScore float64 `json:"final_score"`
	// `StakeScore` stores the value associated with this record.
	StakeScore float64 `json:"stake_score"`
	// `UptimeScore` stores the value associated with this record.
	UptimeScore float64 `json:"uptime_score"`
	// `PerformanceScore` stores the value associated with this record.
	PerformanceScore float64 `json:"performance_score"`
	// `DecentralizationScore` stores the value associated with this record.
	DecentralizationScore float64 `json:"decentralization_score"`
	// `ASNScore` stores the value associated with this record.
	ASNScore float64 `json:"asn_score"`
	// `RegionScore` stores the value associated with this record.
	RegionScore float64 `json:"region_score"`
	// `ProviderScore` stores the value associated with this record.
	ProviderScore float64 `json:"provider_score"`
	// `HomePCScore` stores the value associated with this record.
	HomePCScore float64 `json:"home_pc_score"`
	// `OperatorScore` stores the value associated with this record.
	OperatorScore float64 `json:"operator_score"`
	// `SignedRatioBPS` stores the value associated with this record.
	SignedRatioBPS int `json:"signed_ratio_bps"`
	// `PerformanceEligible` stores the value associated with this record.
	PerformanceEligible bool `json:"performance_eligible"`
	// `PerformanceAgeEligible` stores the value associated with this record.
	PerformanceAgeEligible bool `json:"performance_age_eligible"`
	// `PerformanceIneligibleReason` stores the value associated with this record.
	PerformanceIneligibleReason string `json:"performance_ineligible_reason,omitempty"`
	// `ValidatorAgeBlocks` stores whether the related condition is satisfied.
	ValidatorAgeBlocks uint64 `json:"validator_age_blocks"`
	// `ValidatorAgeEpochs` stores whether the related condition is satisfied.
	ValidatorAgeEpochs uint64 `json:"validator_age_epochs"`
	// `MinimumAgeForPerformanceSlotEpochs` stores the value associated with this record.
	MinimumAgeForPerformanceSlotEpochs uint64 `json:"minimum_age_for_performance_slot_epochs"`
	// `PromotionWindowBucket` stores the value associated with this record.
	PromotionWindowBucket uint64 `json:"promotion_window_bucket"`
	// `PromotionRank` stores the value associated with this record.
	PromotionRank int `json:"promotion_rank"`
	// `EffectiveStake` stores the value associated with this record.
	EffectiveStake int64 `json:"effective_stake"`
	// `ActualStake` stores the value associated with this record.
	ActualStake int64 `json:"actual_stake"`
	// `ReplacementReason` stores the value associated with this record.
	ReplacementReason string `json:"replacement_reason,omitempty"`
}

type ValidatorPoolSnapshot struct {
	// `Mode` stores the value associated with this record.
	Mode string `json:"mode"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `EpochBucket` stores the value associated with this record.
	EpochBucket uint64 `json:"epoch_bucket"`
	// `PromotionWindowBucket` stores the value associated with this record.
	PromotionWindowBucket uint64 `json:"promotion_window_bucket"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `PromotionWindowSource` stores the value associated with this record.
	PromotionWindowSource string `json:"promotion_window_source,omitempty"`
	// `PromotionWindowFrozen` stores the value associated with this record.
	PromotionWindowFrozen bool `json:"promotion_window_frozen"`
	// `PromotionWindowValidators` stores the value associated with this record.
	PromotionWindowValidators []string `json:"promotion_window_validators,omitempty"`
	// `PromotionWindowReplacements` stores the value associated with this record.
	PromotionWindowReplacements int `json:"promotion_window_replacements,omitempty"`
	// `MaxActiveValidators` stores the value associated with this record.
	MaxActiveValidators int `json:"max_active_validators"`
	// `PerformanceSlots` stores the value associated with this record.
	PerformanceSlots int `json:"performance_slots"`
	// `RotationSlots` stores the value associated with this record.
	RotationSlots int `json:"rotation_slots"`
	// `ActiveCount` stores the measured quantity used by this operation.
	ActiveCount int `json:"active_count"`
	// `StandbyCount` stores the measured quantity used by this operation.
	StandbyCount int `json:"standby_count"`
	// `PerformanceActiveCount` stores the measured quantity used by this operation.
	PerformanceActiveCount int `json:"performance_active_count"`
	// `RotationActiveCount` stores the measured quantity used by this operation.
	RotationActiveCount int `json:"rotation_active_count"`
	// `EmergencyReplacementCount` stores the measured quantity used by this operation.
	EmergencyReplacementCount int `json:"emergency_replacement_count"`
	// `MinimumOnlineValidators` stores the value associated with this record.
	MinimumOnlineValidators int `json:"minimum_online_validators"`
	// `Entries` stores the value associated with this record.
	Entries []ValidatorPoolEntry `json:"entries,omitempty"`
}

type validatorHybridScoreContext struct {
	// `asnCounts` stores the value associated with this record.
	asnCounts map[string]int
	// `regionCounts` stores the value associated with this record.
	regionCounts map[string]int
	// `providerCounts` stores the value associated with this record.
	providerCounts map[string]int
	// `operatorCounts` stores the value associated with this record.
	operatorCounts map[string]int
}

// validatorHybridDiversityInfoForScoring implements the validator hybrid diversity info for scoring helper.
func validatorHybridDiversityInfoForScoring(id string) ValidatorDiversityInfo {
	// `info` and `ok` store whether the related condition is satisfied.
	info, ok := validatorDiversityMetadataForID(id)
	if !ok {
		info = ValidatorDiversityInfo{ValidatorID: normalizeValidatorID(id)}
	}
	if info.ValidatorID == "" {
		info.ValidatorID = normalizeValidatorID(id)
	}
	if info.OperatorID == "" {
		info.OperatorID = normalizeValidatorDiversityOperator(info.ValidatorID)
	}
	return info
}

// validatorHybridScoreContextForRecords implements the validator hybrid score context for records helper.
func validatorHybridScoreContextForRecords(records []ValidatorRecord) validatorHybridScoreContext {
	// `ctx` stores the context controlling this operation.
	ctx := validatorHybridScoreContext{
		asnCounts:      make(map[string]int),
		regionCounts:   make(map[string]int),
		providerCounts: make(map[string]int),
		operatorCounts: make(map[string]int),
	}
	// `rec` tracks the current values while iterating.
	for _, rec := range records {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			continue
		}
		// `info` stores the current position in the related collection.
		info := validatorHybridDiversityInfoForScoring(id)
		if info.ASN != "" {
			ctx.asnCounts[info.ASN]++
		}
		if info.Region != "" {
			ctx.regionCounts[info.Region]++
		}
		if info.Cloud != "" {
			ctx.providerCounts[info.Cloud]++
		}
		if info.OperatorID != "" {
			ctx.operatorCounts[info.OperatorID]++
		}
	}
	return ctx
}

// validatorHybridOperatorScore implements the validator hybrid operator score helper.
func validatorHybridOperatorScore(operatorCount int) float64 {
	switch {
	case operatorCount <= 1:
		return 1
	case operatorCount == 2:
		return 0.5
	default:
		return 0
	}
}

// validatorHybridSignedRatioBPS implements the validator hybrid signed ratio bps helper.
func validatorHybridSignedRatioBPS(rec *ValidatorRecord) int {
	if rec == nil || len(rec.ActiveHeights) == 0 {
		return 0
	}
	// `ratio` stores the value produced by this operation.
	ratio := rec.UptimeScore(rec.LastActive)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return int(math.Round(ratio * 10000))
}

// validatorHybridScoreWithContext implements the validator hybrid score with context helper.
func validatorHybridScoreWithContext(rec *ValidatorRecord, ctx *validatorHybridScoreContext) ValidatorPoolEntry {
	// `entry` stores the value produced by this operation.
	entry := ValidatorPoolEntry{}
	if rec == nil {
		return entry
	}
	// `capStake` stores the value produced by this operation.
	capStake := validatorHybridEffectiveStakeCap()
	// `effective` stores the value produced by this operation.
	effective := rec.Stake
	if capStake > 0 && effective > capStake {
		effective = capStake
	}
	if effective < 0 {
		effective = 0
	}
	// `stakeScore` stores the value produced by this operation.
	stakeScore := float64(0)
	if capStake > 0 {
		stakeScore = clampUnit(float64(effective) / float64(capStake))
	}
	// `uptimeScore` stores the value produced by this operation.
	uptimeScore := clampUnit(rec.UptimeScore(rec.LastActive))
	// `performanceScore` stores the value produced by this operation.
	performanceScore := clampUnit(rec.Reputation - rec.PenaltyScore())
	// `signedRatioBPS` stores the value produced by this operation.
	signedRatioBPS := validatorHybridSignedRatioBPS(rec)
	// `performanceEligible` stores the value produced by this operation.
	performanceEligible := signedRatioBPS >= validatorHybridPerformanceMinSignedBPS()
	// `performanceReason` stores the value produced by this operation.
	performanceReason := ""
	if !performanceEligible {
		performanceReason = "signed_ratio_below_minimum"
	}
	// `asnScore`, `regionScore`, `providerScore`, `homePCScore`, and `operatorScore` store the value produced by this operation.
	asnScore, regionScore, providerScore, homePCScore, operatorScore := float64(0), float64(0), float64(0), float64(0), float64(1)
	if ctx != nil {
		// `info` stores the current position in the related collection.
		info := validatorHybridDiversityInfoForScoring(rec.ID)
		if info.ASN != "" && ctx.asnCounts[info.ASN] == 1 {
			asnScore = 1
		}
		if info.Region != "" && ctx.regionCounts[info.Region] == 1 {
			regionScore = 1
		}
		if info.Cloud != "" && ctx.providerCounts[info.Cloud] == 1 {
			providerScore = 1
		}
		if info.HomePC && uptimeScore >= 0.95 && rec.TotalSlashes == 0 {
			homePCScore = 1
		}
		operatorScore = validatorHybridOperatorScore(ctx.operatorCounts[info.OperatorID])
	}
	// `asnWeight`, `regionWeight`, `providerWeight`, and `homePCWeight` store the value produced by this operation.
	asnWeight, regionWeight, providerWeight, homePCWeight := validatorHybridDiversityWeights()
	// `decentralizationScore` stores the value produced by this operation.
	decentralizationScore := clampUnit((asnWeight*asnScore + regionWeight*regionScore + providerWeight*providerScore + homePCWeight*homePCScore) * operatorScore)
	// `stakeWeight`, `uptimeWeight`, `performanceWeight`, and `decentralizationWeight` store the value produced by this operation.
	stakeWeight, uptimeWeight, performanceWeight, decentralizationWeight := validatorHybridScoreWeights()
	// `finalScore` stores the value produced by this operation.
	finalScore := stakeWeight*stakeScore + uptimeWeight*uptimeScore + performanceWeight*performanceScore + decentralizationWeight*decentralizationScore
	return ValidatorPoolEntry{
		ID:                                 normalizeValidatorID(rec.ID),
		FinalScore:                         clampUnit(finalScore),
		StakeScore:                         stakeScore,
		UptimeScore:                        uptimeScore,
		PerformanceScore:                   performanceScore,
		DecentralizationScore:              decentralizationScore,
		ASNScore:                           asnScore,
		RegionScore:                        regionScore,
		ProviderScore:                      providerScore,
		HomePCScore:                        homePCScore,
		OperatorScore:                      operatorScore,
		SignedRatioBPS:                     signedRatioBPS,
		PerformanceEligible:                performanceEligible,
		PerformanceAgeEligible:             true,
		PerformanceIneligibleReason:        performanceReason,
		MinimumAgeForPerformanceSlotEpochs: validatorHybridMinimumPerformanceAgeEpochs(),
		EffectiveStake:                     effective,
		ActualStake:                        rec.Stake,
	}
}

// validatorHybridScore implements the validator hybrid score helper.
func validatorHybridScore(rec *ValidatorRecord) ValidatorPoolEntry {
	return validatorHybridScoreWithContext(rec, nil)
}

// validatorHybridAgeBlocksAt implements the validator hybrid age blocks at helper.
func validatorHybridAgeBlocksAt(height uint64, joinHeight uint64) uint64 {
	if height == 0 {
		return 0
	}
	if joinHeight == 0 {
		return height
	}
	if height <= joinHeight {
		return 0
	}
	return height - joinHeight
}

// validatorHybridApplyPerformanceAgeGate implements the validator hybrid apply performance age gate helper.
func validatorHybridApplyPerformanceAgeGate(height uint64, rec *ValidatorRecord, entry *ValidatorPoolEntry) {
	if entry == nil {
		return
	}
	// `minAgeEpochs` stores the value produced by this operation.
	minAgeEpochs := validatorHybridMinimumPerformanceAgeEpochs()
	// `minAgeBlocks` stores the value produced by this operation.
	minAgeBlocks := validatorHybridMinimumPerformanceAgeBlocks()
	// `ageBlocks` stores the value produced by this operation.
	ageBlocks := uint64(0)
	if rec != nil {
		ageBlocks = validatorHybridAgeBlocksAt(height, rec.JoinHeight)
	}
	// `ageEpochs` stores the value produced by this operation.
	ageEpochs := uint64(0)
	// `epochBlocks` stores the value produced by this operation.
	if epochBlocks := validatorHybridEpochBlocks(); epochBlocks > 0 {
		ageEpochs = ageBlocks / epochBlocks
	}
	entry.ValidatorAgeBlocks = ageBlocks
	entry.ValidatorAgeEpochs = ageEpochs
	entry.MinimumAgeForPerformanceSlotEpochs = minAgeEpochs
	// `legacyOrGenesisValidator` stores the value produced by this operation.
	legacyOrGenesisValidator := rec != nil && rec.JoinHeight == 0
	entry.PerformanceAgeEligible = legacyOrGenesisValidator || minAgeBlocks == 0 || ageBlocks >= minAgeBlocks
	if !entry.PerformanceAgeEligible {
		if entry.PerformanceIneligibleReason != "" {
			entry.PerformanceIneligibleReason += ","
		}
		entry.PerformanceIneligibleReason += "age_below_minimum"
		entry.PerformanceEligible = false
	}
}

// validatorHybridEligibleEntries implements the validator hybrid eligible entries helper.
func validatorHybridEligibleEntries(height uint64, snapshot map[string]ValidatorRecord) []ValidatorPoolEntry {
	if len(snapshot) == 0 {
		return nil
	}
	// `records` stores the value produced by this operation.
	records := make([]ValidatorRecord, 0, len(snapshot))
	// `recVal` tracks the value currently being processed.
	for _, recVal := range snapshot {
		// `rec` stores the value produced by this operation.
		rec := recVal
		rec.ActiveHeights = append([]uint64{}, recVal.ActiveHeights...)
		rec.SignedHeights = append([]uint64{}, recVal.SignedHeights...)
		ValidatorStateMachine{}.Update(&rec, height)
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" || isProtocolValidatorBanned(id) {
			continue
		}
		if rec.Status != ValidatorActive {
			continue
		}
		if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
			continue
		}
		if rec.JoinHeight > 0 && height < rec.JoinHeight {
			continue
		}
		if !validatorPassesStakeGate(id, rec.Stake) {
			continue
		}
		rec.ID = id
		records = append(records, rec)
	}
	// `ctx` stores the context controlling this operation.
	ctx := validatorHybridScoreContextForRecords(records)
	// `entries` stores the value produced by this operation.
	entries := make([]ValidatorPoolEntry, 0, len(records))
	// `i` tracks the current position in the related collection.
	for i := range records {
		// `entry` stores the value produced by this operation.
		entry := validatorHybridScoreWithContext(&records[i], &ctx)
		validatorHybridApplyPerformanceAgeGate(height, &records[i], &entry)
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].FinalScore != entries[j].FinalScore {
			return entries[i].FinalScore > entries[j].FinalScore
		}
		if entries[i].EffectiveStake != entries[j].EffectiveStake {
			return entries[i].EffectiveStake > entries[j].EffectiveStake
		}
		if entries[i].ActualStake != entries[j].ActualStake {
			return entries[i].ActualStake > entries[j].ActualStake
		}
		return entries[i].ID < entries[j].ID
	})
	return entries
}

// validatorHybridWeightedRotation implements the validator hybrid weighted rotation helper.
func validatorHybridWeightedRotation(height uint64, seedAnchor string, candidates []ValidatorPoolEntry, slots int) []ValidatorPoolEntry {
	if slots <= 0 || len(candidates) == 0 {
		return nil
	}
	if slots >= len(candidates) {
		// `out` stores the result produced by this operation.
		out := append([]ValidatorPoolEntry{}, candidates...)
		// `i` tracks the current position in the related collection.
		for i := range out {
			out[i].SlotType = "rotation"
			out[i].Active = true
		}
		return out
	}
	// `epochBucket` stores the value produced by this operation.
	epochBucket := validatorHybridEpochBucket(height)
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, len(candidates))
	// `c` tracks the current values while iterating.
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	if strings.TrimSpace(seedAnchor) == "" {
		seedAnchor = ValidatorSetHash(ids)
	}
	// `seed` stores the value produced by this operation.
	seed := HashStrings([]string{
		"hybrid_score_rotation_v1",
		protocolChainID(),
		strconv.FormatUint(epochBucket, 10),
		strings.TrimSpace(seedAnchor),
		ValidatorSetHash(ids),
	})
	type ranked struct {
		// `entry` stores the value associated with this record.
		entry ValidatorPoolEntry
		// `rank` stores the value associated with this record.
		rank uint64
		// `weight` stores the value associated with this record.
		weight uint64
	}
	// `rankedEntries` stores the value produced by this operation.
	rankedEntries := make([]ranked, 0, len(candidates))
	// `c` tracks the current values while iterating.
	for _, c := range candidates {
		// `weight` stores the value produced by this operation.
		weight := uint64(c.FinalScore*1_000_000) + 1
		// `rawHash` stores the digest used to identify or verify the related data.
		rawHash := HashStrings([]string{seed, c.ID})
		// `raw` stores the value produced by this operation.
		raw := uint64(0)
		if len(rawHash) >= 16 {
			// `parsed` and `err` store the error produced by this operation.
			if parsed, err := strconv.ParseUint(rawHash[:16], 16, 64); err == nil {
				raw = parsed
			}
		}
		if raw == 0 {
			raw = 1
		}
		rankedEntries = append(rankedEntries, ranked{
			entry:  c,
			rank:   raw / weight,
			weight: weight,
		})
	}
	sort.Slice(rankedEntries, func(i, j int) bool {
		if rankedEntries[i].rank != rankedEntries[j].rank {
			return rankedEntries[i].rank < rankedEntries[j].rank
		}
		if rankedEntries[i].weight != rankedEntries[j].weight {
			return rankedEntries[i].weight > rankedEntries[j].weight
		}
		if rankedEntries[i].entry.FinalScore != rankedEntries[j].entry.FinalScore {
			return rankedEntries[i].entry.FinalScore > rankedEntries[j].entry.FinalScore
		}
		return rankedEntries[i].entry.ID < rankedEntries[j].entry.ID
	})
	// `out` stores the result produced by this operation.
	out := make([]ValidatorPoolEntry, 0, slots)
	// `i` stores the current position in the related collection.
	for i := 0; i < len(rankedEntries) && len(out) < slots; i++ {
		// `entry` stores the value produced by this operation.
		entry := rankedEntries[i].entry
		entry.SlotType = "rotation"
		entry.Active = true
		out = append(out, entry)
	}
	return out
}

// validatorHybridPoolSnapshot implements the validator hybrid pool snapshot helper.
func validatorHybridPoolSnapshot(height uint64, snapshot map[string]ValidatorRecord, seedAnchor string) ValidatorPoolSnapshot {
	return validatorHybridPoolSnapshotWithPromotionState(height, snapshot, seedAnchor, nil, nil, "", "disabled")
}

// validatorHybridPoolSnapshotWithPromotionState implements the validator hybrid pool snapshot with promotion state helper.
func validatorHybridPoolSnapshotWithPromotionState(height uint64, snapshot map[string]ValidatorRecord, seedAnchor string, promotionRecord *PromotionWindowRecord, promotionReplacements []PromotionWindowReplacementRecord, promotionHash string, promotionSource string) ValidatorPoolSnapshot {
	// `maxActive` stores the value produced by this operation.
	maxActive := validatorHybridMaxActiveValidators()
	// `performanceSlots` stores the value produced by this operation.
	performanceSlots := validatorHybridPerformanceSlots()
	// `rotationSlots` stores the value produced by this operation.
	rotationSlots := validatorHybridRotationSlots()
	// `epochBucket` stores the value produced by this operation.
	epochBucket := validatorHybridEpochBucket(height)
	// `promotionWindowBucket` stores the value produced by this operation.
	promotionWindowBucket := validatorHybridPromotionWindowBucket(height)
	// `out` stores the result produced by this operation.
	out := ValidatorPoolSnapshot{
		Mode:                  normalizeActiveSetMode(protocolValidatorActiveSetModeValue()),
		Height:                height,
		EpochBucket:           epochBucket,
		PromotionWindowBucket: promotionWindowBucket,
		PromotionWindowHash:   strings.TrimSpace(promotionHash),
		PromotionWindowSource: strings.TrimSpace(promotionSource),
		MaxActiveValidators:   maxActive,
		PerformanceSlots:      performanceSlots,
		RotationSlots:         rotationSlots,
	}
	// `eligible` stores the value produced by this operation.
	eligible := validatorHybridEligibleEntries(height, snapshot)
	if len(eligible) == 0 {
		return out
	}
	// `performanceRank` stores the value produced by this operation.
	performanceRank := 0
	// `i` tracks the current position in the related collection.
	for i := range eligible {
		eligible[i].PromotionWindowBucket = promotionWindowBucket
		if eligible[i].PerformanceEligible {
			performanceRank++
			eligible[i].PromotionRank = performanceRank
		}
	}
	// `entries` stores the value produced by this operation.
	entries := make([]ValidatorPoolEntry, 0, len(eligible))
	// `activeIDs` stores the value produced by this operation.
	activeIDs := make(map[string]struct{}, maxActive)
	if len(eligible) <= maxActive {
		// `entry` tracks the current values while iterating.
		for _, entry := range eligible {
			entry.Active = true
			if entry.PerformanceEligible && out.PerformanceActiveCount < performanceSlots {
				entry.SlotType = "performance"
				out.PerformanceActiveCount++
			} else {
				entry.SlotType = "rotation"
				out.RotationActiveCount++
			}
			activeIDs[entry.ID] = struct{}{}
			entries = append(entries, entry)
		}
		out.ActiveCount = len(entries)
		out.MinimumOnlineValidators = validatorHybridMinimumOnlineForActiveCount(out.ActiveCount)
		out.Entries = entries
		return out
	}
	// `eligibleByID` stores the value produced by this operation.
	eligibleByID := make(map[string]ValidatorPoolEntry, len(eligible))
	// `entry` tracks the current values while iterating.
	for _, entry := range eligible {
		eligibleByID[entry.ID] = entry
	}
	if promotionWindowRecordAppliesAtHeight(promotionRecord, height) {
		out.PromotionWindowFrozen = true
		out.PromotionWindowValidators = promotionWindowEffectivePerformanceIDs(promotionRecord, promotionReplacements)
		out.PromotionWindowReplacements = len(promotionReplacements)
		// `id` tracks the current position in the related collection.
		for _, id := range out.PromotionWindowValidators {
			// `entry` and `ok` store whether the related condition is satisfied.
			entry, ok := eligibleByID[id]
			if !ok {
				continue
			}
			entry.SlotType = "performance"
			entry.Active = true
			entry.ReplacementReason = "promotion_window_locked"
			activeIDs[entry.ID] = struct{}{}
			out.PerformanceActiveCount++
			entries = append(entries, entry)
			if out.PerformanceActiveCount >= performanceSlots {
				break
			}
		}
	} else {
		// `performanceEligible` stores the value produced by this operation.
		performanceEligible := make([]ValidatorPoolEntry, 0, len(eligible))
		// `entry` tracks the current values while iterating.
		for _, entry := range eligible {
			if entry.PerformanceEligible {
				performanceEligible = append(performanceEligible, entry)
			}
		}
		// `performanceCount` stores the measured quantity used by this operation.
		performanceCount := performanceSlots
		if performanceCount > len(performanceEligible) {
			performanceCount = len(performanceEligible)
		}
		// `i` stores the current position in the related collection.
		for i := 0; i < performanceCount; i++ {
			// `entry` stores the value produced by this operation.
			entry := performanceEligible[i]
			entry.SlotType = "performance"
			entry.Active = true
			activeIDs[entry.ID] = struct{}{}
			out.PerformanceActiveCount++
			entries = append(entries, entry)
		}
	}
	// `remaining` stores the value produced by this operation.
	remaining := make([]ValidatorPoolEntry, 0, len(eligible)-out.PerformanceActiveCount)
	// `entry` tracks the current values while iterating.
	for _, entry := range eligible {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := activeIDs[entry.ID]; ok {
			continue
		}
		remaining = append(remaining, entry)
	}
	// `rotationTarget` stores the value produced by this operation.
	rotationTarget := maxActive - out.PerformanceActiveCount
	if rotationTarget < 0 {
		rotationTarget = 0
	}
	// `entry` tracks the current values while iterating.
	for _, entry := range validatorHybridWeightedRotation(height, seedAnchor, remaining, rotationTarget) {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := activeIDs[entry.ID]; ok {
			continue
		}
		activeIDs[entry.ID] = struct{}{}
		out.RotationActiveCount++
		entries = append(entries, entry)
	}
	// `entry` tracks the current values while iterating.
	for _, entry := range eligible {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := activeIDs[entry.ID]; ok {
			continue
		}
		entry.SlotType = "standby"
		entry.Active = false
		out.StandbyCount++
		entries = append(entries, entry)
	}
	out.ActiveCount = len(activeIDs)
	out.MinimumOnlineValidators = validatorHybridMinimumOnlineForActiveCount(out.ActiveCount)
	out.Entries = entries
	return out
}

// selectHybridValidatorsFromRegistrySnapshot implements the select hybrid validators from registry snapshot helper.
func selectHybridValidatorsFromRegistrySnapshot(height uint64, snapshot map[string]ValidatorRecord, seedAnchor string) []string {
	// `pool` stores the value produced by this operation.
	pool := validatorHybridPoolSnapshot(height, snapshot, seedAnchor)
	// `out` stores the result produced by this operation.
	out := make([]string, 0, pool.ActiveCount)
	// `entry` tracks the current values while iterating.
	for _, entry := range pool.Entries {
		if entry.Active {
			out = append(out, entry.ID)
		}
	}
	return canonicalValidatorIDs(out)
}

// selectHybridValidatorsFromRegistrySnapshot implements the select hybrid validators from registry snapshot helper.
func (n *Node) selectHybridValidatorsFromRegistrySnapshot(height uint64, snapshot map[string]ValidatorRecord, _ string) []string {
	if height == 0 {
		height = 1
	}
	anchor := ""
	if n != nil && height > 1 {
		anchor = strings.TrimSpace(n.expectedValidatorSetHash(height - 1))
	}
	if len(snapshot) == 0 && n != nil {
		snapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	eligible := validatorHybridEligibleEntries(height, snapshot)
	record, replacements, hash, source := n.promotionWindowStateForHeight(height, snapshot, anchor, eligible)
	// `pool` stores the value produced by this operation.
	pool := validatorHybridPoolSnapshotWithPromotionState(height, snapshot, anchor, record, replacements, hash, source)
	if strings.EqualFold(strings.TrimSpace(pool.PromotionWindowSource), "missing_committed_record") {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, pool.ActiveCount)
	// `entry` tracks the current values while iterating.
	for _, entry := range pool.Entries {
		if entry.Active {
			out = append(out, entry.ID)
		}
	}
	return canonicalValidatorIDs(out)
}

// validatorPoolSnapshotForHeight implements the validator pool snapshot for height helper.
func (n *Node) validatorPoolSnapshotForHeight(height uint64, snapshot map[string]ValidatorRecord) ValidatorPoolSnapshot {
	if height == 0 {
		height = 1
	}
	// `anchor` stores the value produced by this operation.
	anchor := ""
	if n != nil && height > 1 {
		anchor = strings.TrimSpace(n.expectedValidatorSetHash(height - 1))
	}
	if len(snapshot) == 0 && n != nil {
		snapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if n == nil || !promotionWindowRecordV1EnabledAt(height) || !activeSetModeHybridScoreRotation() {
		return validatorHybridPoolSnapshot(height, snapshot, anchor)
	}
	// `eligible` stores the value produced by this operation.
	eligible := validatorHybridEligibleEntries(height, snapshot)
	// `record`, `replacements`, `hash`, and `source` store the digest used to identify or verify the related data.
	record, replacements, hash, source := n.promotionWindowStateForHeight(height, snapshot, anchor, eligible)
	return validatorHybridPoolSnapshotWithPromotionState(height, snapshot, anchor, record, replacements, hash, source)
}

// normalizeHeartbeatScope normalizes heartbeat scope.
func normalizeHeartbeatScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "committee_only":
		return "committee_only"
	case "all":
		return "all"
	default:
		return "committee_only"
	}
}

// committeeRotationBlocks implements the committee rotation blocks helper.
func committeeRotationBlocks() uint64 {
	return protocolValidatorCommitteeRotationBlocksValue()
}

// validatorSelectionActivityWindowBlocks implements the validator selection activity window blocks helper.
func validatorSelectionActivityWindowBlocks() uint64 {
	return protocolValidatorSelectionActivityWindowValue()
}

// validatorSelectionMinSignedBlocks implements the validator selection min signed blocks helper.
func validatorSelectionMinSignedBlocks() uint64 {
	return protocolValidatorSelectionMinSignedBlocksValue()
}

// adaptiveCommitteeTarget implements the adaptive committee target helper.
func (n *Node) adaptiveCommitteeTarget(eligible int) int {
	// `floor` stores the value produced by this operation.
	floor := minActiveValidatorsFloor()
	if eligible <= 0 {
		return floor
	}

	// `mult` stores the synchronization state protecting shared data.
	mult := protocolValidatorAdaptiveCommitteeLogMultValue()
	if mult <= 0 {
		mult = 16
	}
	// `raw` stores the value produced by this operation.
	raw := int(math.Ceil(float64(mult) * math.Log2(float64(eligible+1))))
	if raw < floor {
		raw = floor
	}

	// `maxCommittee` stores the value produced by this operation.
	maxCommittee := protocolValidatorMaxActiveCommitteeValue()
	if maxCommittee <= 0 {
		maxCommittee = 512
	}
	if raw > maxCommittee {
		raw = maxCommittee
	}
	if raw > eligible {
		raw = eligible
	}
	if raw < 1 {
		raw = 1
	}
	return raw
}

// buildAdaptiveCommittee builds adaptive committee.
func (n *Node) buildAdaptiveCommittee(height uint64, eligible []string, target int) []string {
	// `stakeSnapshot` stores the value produced by this operation.
	stakeSnapshot := map[string]ValidatorRecord(nil)
	if n != nil {
		stakeSnapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if len(stakeSnapshot) == 0 {
		// `legacyWeakSource` stores the value produced by this operation.
		legacyWeakSource := height <= 1 || !validatorSetCommitmentV2EnabledAt(height-1)
		if legacyWeakSource {
			// Legacy committee scoring fallback; not consensus commitment authority.
			stakeSnapshot = GlobalValidatorRegistry.Snapshot()
		}
	}
	if len(stakeSnapshot) == 0 && (n == nil || n.Blockchain == nil || n.Blockchain.Height() == 0) {
		// Genesis bootstrap only.
		stakeSnapshot = GlobalValidatorRegistry.Snapshot()
	}
	eligible = deterministicStakeHashOrderedValidatorIDs(eligible, stakeSnapshot)
	if len(eligible) == 0 {
		return nil
	}
	if target <= 0 || target > len(eligible) {
		target = len(eligible)
	}

	// `rotation` stores the value produced by this operation.
	rotation := committeeRotationBlocks()
	// `rotationBucket` stores the value produced by this operation.
	rotationBucket := uint64(0)
	if validatorEpochSetV1EnabledAt(height) {
		rotationBucket = validatorEpochNumber(height) - 1
	} else if rotation > 0 {
		rotationBucket = height / rotation
	}
	// `prevHash` stores the digest used to identify or verify the related data.
	prevHash := ""
	if n != nil && height > 0 {
		prevHash = strings.TrimSpace(n.expectedValidatorSetHash(height - 1))
	}
	if prevHash == "" {
		prevHash = ValidatorSetHash(eligible)
	}

	// `setHash` stores the digest used to identify or verify the related data.
	setHash := ValidatorSetHash(eligible)
	// `seed` stores the value produced by this operation.
	seed := HashStrings([]string{
		"committee_vrf_v1",
		protocolChainID(),
		strconv.FormatUint(rotationBucket, 10),
		prevHash,
		setHash,
	})

	type scored struct {
		// `id` stores the current position in the related collection.
		id string
		// `score` stores the value associated with this record.
		score string
		// `stake` stores the value associated with this record.
		stake int64
	}
	// `scoredIDs` stores the value produced by this operation.
	scoredIDs := make([]scored, 0, len(eligible))
	// `id` tracks the current position in the related collection.
	for _, id := range eligible {
		scoredIDs = append(scoredIDs, scored{
			id:    id,
			score: HashStrings([]string{seed, id}),
			stake: validatorStakeFromSnapshot(stakeSnapshot, id),
		})
	}
	sort.Slice(scoredIDs, func(i, j int) bool {
		if scoredIDs[i].score == scoredIDs[j].score {
			if scoredIDs[i].stake != scoredIDs[j].stake {
				return scoredIDs[i].stake > scoredIDs[j].stake
			}
			return scoredIDs[i].id < scoredIDs[j].id
		}
		return scoredIDs[i].score < scoredIDs[j].score
	})
	if len(scoredIDs) > target {
		scoredIDs = scoredIDs[:target]
	}

	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(scoredIDs))
	// `item` tracks the current position in the related collection.
	for _, item := range scoredIDs {
		out = append(out, item.id)
	}
	return canonicalValidatorIDs(out)
}

// openValidatorSetEnabled implements the open validator set enabled helper.
func openValidatorSetEnabled() bool {
	if protocolDeterministicValidatorSelectionEnabled() {
		return false
	}
	return protocolDynamicValidatorSelectionEnabledFlag() && TestingRelaxedPromotion
}

// selectAllStakedValidators implements the select all staked validators helper.
func selectAllStakedValidators(height uint64) []string {
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()

	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(GlobalValidatorRegistry.records))
	// `stakes` stores the value produced by this operation.
	stakes := make(map[string]ValidatorRecord, len(GlobalValidatorRegistry.records))
	// `rec` tracks the current values while iterating.
	for _, rec := range GlobalValidatorRegistry.records {
		if isProtocolValidatorBanned(rec.ID) {
			continue
		}
		ValidatorStateMachine{}.Update(rec, height)
		if !validatorRecordIsConsensusActive(*rec, height) {
			continue
		}
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
		stakes[id] = ValidatorRecord{ID: id, Stake: rec.Stake}
	}
	return deterministicStakeHashOrderedValidatorIDs(out, stakes)
}

// selectAllStakedValidatorsFromSnapshot implements the select all staked validators from snapshot helper.
func selectAllStakedValidatorsFromSnapshot(height uint64, snapshot map[string]ValidatorRecord) []string {
	if len(snapshot) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(snapshot))
	// `rec` tracks the current values while iterating.
	for _, rec := range snapshot {
		if isProtocolValidatorBanned(rec.ID) {
			continue
		}
		if !validatorRecordIsConsensusActive(rec, height) {
			continue
		}
		out = append(out, rec.ID)
	}
	return canonicalValidatorIDs(out)
}

// selectDeterministicValidatorsFromSnapshot implements the select deterministic validators from snapshot helper.
func selectDeterministicValidatorsFromSnapshot(height uint64, target int, snapshot map[string]ValidatorRecord) []string {
	if len(snapshot) == 0 {
		return nil
	}
	// `unlimited` stores the value produced by this operation.
	unlimited := false
	if target < 0 {
		unlimited = true
	} else if target == 0 {
		target = 1
	}

	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, len(snapshot))
	// `rec` tracks the current values while iterating.
	for _, rec := range snapshot {
		if isProtocolValidatorBanned(rec.ID) {
			continue
		}
		if !validatorRecordIsConsensusActive(rec, height) {
			continue
		}
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	ids = canonicalValidatorIDs(ids)
	if len(ids) == 0 {
		return nil
	}

	if unlimited {
		target = len(ids)
	}
	if target <= 0 {
		target = 1
	}
	if target > len(ids) {
		target = len(ids)
	}

	// `rotationWindow` stores the value produced by this operation.
	rotationWindow := protocolValidatorSetRotationWindowValue()
	// `rotationBucket` stores the value produced by this operation.
	rotationBucket := height / rotationWindow
	// `setHash` stores the digest used to identify or verify the related data.
	setHash := ValidatorSetHash(ids)
	// `seed` stores the value produced by this operation.
	seed := HashStrings([]string{
		"validator_vrf_v1",
		protocolChainID(),
		strconv.FormatUint(rotationBucket, 10),
		setHash,
	})

	type scored struct {
		// `id` stores the current position in the related collection.
		id string
		// `score` stores the value associated with this record.
		score string
	}
	// `scoredIDs` stores the value produced by this operation.
	scoredIDs := make([]scored, 0, len(ids))
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		scoredIDs = append(scoredIDs, scored{
			id:    id,
			score: HashStrings([]string{seed, id}),
		})
	}
	sort.Slice(scoredIDs, func(i, j int) bool {
		if scoredIDs[i].score == scoredIDs[j].score {
			return scoredIDs[i].id < scoredIDs[j].id
		}
		return scoredIDs[i].score < scoredIDs[j].score
	})
	if len(scoredIDs) > target {
		scoredIDs = scoredIDs[:target]
	}

	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(scoredIDs))
	// `e` tracks the current values while iterating.
	for _, e := range scoredIDs {
		out = append(out, e.id)
	}
	return canonicalValidatorIDs(out)
}

// GetDynamicValidators returns dynamic validators.
func (n *Node) GetDynamicValidators(height int) []string {
	if height <= 0 {
		if n != nil && n.Blockchain != nil {
			height = int(n.Blockchain.Height() + 1)
		} else {
			height = 1
		}
	}
	// `registrySnap` stores the value produced by this operation.
	registrySnap := map[string]ValidatorRecord(nil)
	if n != nil {
		registrySnap = n.validatorRegistrySnapshotForHeight(uint64(height))
	}
	if len(registrySnap) == 0 {
		// `legacyWeakSource` stores the value produced by this operation.
		legacyWeakSource := height <= 1 || !validatorSetCommitmentV2EnabledAt(uint64(height)-1)
		if legacyWeakSource {
			// Legacy dynamic-selection fallback; not consensus commitment authority.
			registrySnap = GlobalValidatorRegistry.Snapshot()
		}
	}
	if len(registrySnap) == 0 && (n == nil || n.Blockchain == nil || n.Blockchain.Height() == 0) {
		// Genesis bootstrap only.
		registrySnap = GlobalValidatorRegistry.Snapshot()
	}
	if openValidatorSetEnabled() {
		// `out` stores the result produced by this operation.
		if out := selectAllStakedValidatorsFromSnapshot(uint64(height), registrySnap); len(out) > 0 {
			return canonicalValidatorIDs(out)
		}
	}
	// `target` stores the value produced by this operation.
	target := n.activeSetTarget()
	if protocolDeterministicValidatorSelectionEnabled() {
		if activeSetModeHybridScoreRotation() {
			// `anchor` stores the value produced by this operation.
			anchor := ""
			if n != nil && uint64(height) > 1 {
				anchor = strings.TrimSpace(n.expectedValidatorSetHash(uint64(height) - 1))
			}
			if n != nil {
				// `out` stores the result produced by this operation.
				if out := n.selectHybridValidatorsFromRegistrySnapshot(uint64(height), registrySnap, anchor); len(out) > 0 {
					return canonicalValidatorIDs(out)
				}
				return nil
			}
			// `out` stores the result produced by this operation.
			if out := selectHybridValidatorsFromRegistrySnapshot(uint64(height), registrySnap, anchor); len(out) > 0 {
				return canonicalValidatorIDs(out)
			}
			return nil
		}
		// `out` stores the result produced by this operation.
		if out := SelectValidatorsFromRegistrySnapshot(uint64(height), target, registrySnap); len(out) > 0 {
			return canonicalValidatorIDs(out)
		}
		return nil
	}
	// `out` stores the result produced by this operation.
	out := ValidatorSelector.Select(uint64(height), target)
	if len(out) == 0 {
		return nil
	}
	return canonicalValidatorIDs(out)
}

// SelectValidatorsFromRegistrySnapshot deterministically selects validators from a
// snapshot registry without mutating global state. This is used to reconcile
// validator-set mismatches caused by stale snapshot validator lists.
func SelectValidatorsFromRegistrySnapshot(height uint64, target int, snapshot map[string]ValidatorRecord) []string {
	if len(snapshot) == 0 {
		return nil
	}
	if openValidatorSetEnabled() {
		// `out` stores the result produced by this operation.
		if out := selectAllStakedValidatorsFromSnapshot(height, snapshot); len(out) > 0 {
			return canonicalValidatorIDs(out)
		}
	}
	if activeSetModeHybridScoreRotation() {
		return canonicalValidatorIDs(selectHybridValidatorsFromRegistrySnapshot(height, snapshot, ""))
	}
	if protocolDeterministicValidatorSelectionEnabled() {
		return canonicalValidatorIDs(selectDeterministicValidatorsFromSnapshot(height, target, snapshot))
	}
	// `unlimited` stores the value produced by this operation.
	unlimited := false
	if target < 0 {
		unlimited = true
	} else if target == 0 {
		target = 1
	}

	// `records` stores the value produced by this operation.
	records := make([]*ValidatorRecord, 0, len(snapshot))
	// `recVal` tracks the value currently being processed.
	for _, recVal := range snapshot {
		// `rec` stores the value produced by this operation.
		rec := recVal
		rec.ActiveHeights = append([]uint64{}, recVal.ActiveHeights...)
		rec.SignedHeights = append([]uint64{}, recVal.SignedHeights...)
		ValidatorStateMachine{}.Update(&rec, height)
		if isProtocolValidatorBanned(rec.ID) {
			continue
		}
		if validatorStateIsRemoved(rec.Status) {
			continue
		}
		if rec.JoinHeight > 0 && height < rec.JoinHeight {
			continue
		}
		records = append(records, &rec)
	}

	if len(records) == 0 {
		return nil
	}

	// `maxStakeScore` stores the value produced by this operation.
	maxStakeScore := 0.0
	// `maxPenalty` stores the value produced by this operation.
	maxPenalty := 0.0
	// `capStake` stores the value produced by this operation.
	capStake := int64(float64(FixedTotalSupply) * protocolValidatorStakeCapPctValue())
	// `rec` tracks the current values while iterating.
	for _, rec := range records {
		// `effective` stores the value produced by this operation.
		effective := rec.Stake
		if capStake > 0 && effective > capStake {
			effective = capStake
		}
		// `stakeScore` stores the value produced by this operation.
		stakeScore := math.Log(float64(effective) + 1)
		if stakeScore > maxStakeScore {
			maxStakeScore = stakeScore
		}
		// `penalty` stores the value produced by this operation.
		penalty := rec.PenaltyScore()
		if penalty > maxPenalty {
			maxPenalty = penalty
		}
	}

	// `scored` stores the value produced by this operation.
	scored := make([]*ValidatorRecord, 0, len(records))
	// `rec` tracks the current values while iterating.
	for _, rec := range records {
		if rec.Status != ValidatorActive {
			continue
		}
		rec.LastScore = ValidatorScoreEngine{}.Score(rec, maxStakeScore, maxPenalty)
		scored = append(scored, rec)
	}

	if !protocolCandidateIsolationEnabled() && len(scored) < target {
		// `rec` tracks the current values while iterating.
		for _, rec := range records {
			if rec.Status == ValidatorActive {
				continue
			}
			if validatorPassesStakeGate(rec.ID, rec.Stake) && rec.Reputation >= protocolValidatorReputationRecoveryThresholdValue() {
				rec.LastScore = ValidatorScoreEngine{}.Score(rec, maxStakeScore, maxPenalty)
				scored = append(scored, rec)
			}
		}
	}

	if len(scored) == 0 {
		return nil
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].LastScore == scored[j].LastScore {
			if scored[i].Stake == scored[j].Stake {
				return scored[i].ID < scored[j].ID
			}
			return scored[i].Stake > scored[j].Stake
		}
		return scored[i].LastScore > scored[j].LastScore
	})

	if unlimited {
		target = len(scored)
	}
	if len(scored) > target {
		scored = scored[:target]
	}

	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(scored))
	// `rec` tracks the current values while iterating.
	for _, rec := range scored {
		out = append(out, rec.ID)
	}
	return canonicalValidatorIDs(out)
}
