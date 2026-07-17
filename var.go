package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/time/rate"
)

type PeerInfo struct {
	// `NodeID` stores the value associated with this record.
	NodeID string
	// `Addr` stores the address used by this operation.
	Addr string
	// `LastSeen` stores the value associated with this record.
	LastSeen time.Time
	// `LastConnectAttempt` stores the value associated with this record.
	LastConnectAttempt time.Time
	// `Alive` stores the value associated with this record.
	Alive bool
	// `Failures` stores the result produced by this operation.
	Failures int
	// `Height` stores the value associated with this record.
	Height uint64
	// `HelloSent` stores the value associated with this record.
	HelloSent bool
	// `Connecting` stores the value associated with this record.
	Connecting bool
	// `Conn` stores the value associated with this record.
	Conn net.Conn
}
type StateSnapshot struct {
	// `Version` stores the value associated with this record.
	Version uint32 `json:"version"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string `json:"state_merkle_root,omitempty"`
	// `LedgerHash` stores the digest used to identify or verify the related data.
	LedgerHash string `json:"ledger_hash"`
	// `LedgerStage` stores the value associated with this record.
	LedgerStage string `json:"ledger_stage,omitempty"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash,omitempty"`
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string `json:"prev_hash"`
	// BlockProposer preserves the canonical block proposer used by execution root verification.
	BlockProposer string `json:"block_proposer,omitempty"`
	// BlockMempoolRoot preserves the canonical mempool root used by execution root verification.
	BlockMempoolRoot string `json:"block_mempool_root,omitempty"`
	// BlockEpoch preserves the canonical logical epoch used by execution root verification.
	BlockEpoch uint64 `json:"block_epoch,omitempty"`
	// `Ledger` stores the value associated with this record.
	Ledger Ledger `json:"ledger"`
	// `Validators` stores whether the related condition is satisfied.
	Validators map[string]bool `json:"validators"`
	// ValidatorSetHash freezes the deterministic validator-set hash at snapshot height.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// ValidatorSetSource records where the authoritative validator set came from.
	ValidatorSetSource string `json:"validator_set_source,omitempty"`
	// ValidatorSetRoot commits ordered validator leaves (stake + canonical validator hash).
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// Pending validator transitions are required for deterministic set-hash
	// reconstruction after late-join snapshot restore.
	PendingValidators map[string]uint64 `json:"pending_validators,omitempty"`
	// `PendingValidatorRemovals` stores the value associated with this record.
	PendingValidatorRemovals map[string]uint64 `json:"pending_validator_removals,omitempty"`
	// `ValidatorSetHeight` stores whether the related condition is satisfied.
	ValidatorSetHeight uint64 `json:"validator_set_height,omitempty"`
	// NextValidatorSetHash commits validator set hash for height+1.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// NextValidatorSetSource records where the next validator set commitment came from.
	NextValidatorSetSource string `json:"next_validator_set_source,omitempty"`
	// NextValidatorSetRoot commits ordered validator leaves for height+1 set.
	NextValidatorSetRoot string `json:"next_validator_set_root,omitempty"`
	// NextValidatorSetHeight pins activation height for NextValidatorSetHash.
	NextValidatorSetHeight uint64 `json:"next_validator_set_height,omitempty"`
	// ActivationHeight is an alias of NextValidatorSetHeight for v2 commitment APIs.
	ActivationHeight uint64 `json:"activation_height,omitempty"`
	// ValidatorRegistry carries dynamic validator engine state.
	ValidatorRegistry map[string]ValidatorRecord `json:"validator_registry,omitempty"`
	// StateValidators is the canonical on-chain validator registry snapshot.
	// Key is normalized validator ID/address.
	StateValidators map[string]Validator `json:"state_validators,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// PromotionWindowRecords carries chain-committed hybrid validator-pool
	// performance-slot freezes. Local caches may mirror these records, but this
	// snapshot field is part of the replayable consensus state.
	PromotionWindowRecords map[uint64]PromotionWindowRecord `json:"promotion_window_records,omitempty"`
	// `PromotionWindowReplacements` stores the value associated with this record.
	PromotionWindowReplacements map[uint64][]PromotionWindowReplacementRecord `json:"promotion_window_replacements,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// CheckpointProof stores validator signatures over deterministic snapshot checkpoint bytes.
	CheckpointProof map[string]string `json:"checkpoint_proof,omitempty"`
	// CheckpointHeight pins deterministic proof anchor (for quorum accumulation).
	CheckpointHeight uint64 `json:"checkpoint_height,omitempty"`
	// CheckpointDomain is the signature domain for checkpoint proof bytes.
	CheckpointDomain string `json:"checkpoint_domain,omitempty"`
	// Finality anchors bind snapshots to irreversible epoch checkpoints.
	FinalizedEpoch uint64 `json:"finalized_epoch,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash,omitempty"`
	// `FinalizedStateRoot` stores the digest used to identify or verify the related data.
	FinalizedStateRoot string `json:"finalized_state_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash,omitempty"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `PreviousEpochAnchorHash` stores the digest used to identify or verify the related data.
	PreviousEpochAnchorHash string `json:"previous_epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `FinalityCertificate` stores the value associated with this record.
	FinalityCertificate *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
	// SnapshotHash is the canonical deterministic hash of snapshot metadata.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp"`
}

// ExecPoolSnapshot carries execution-hash convergence for late joiners.
// Hashes map execHash -> signers, TxMerkle map execHash -> txMerkle.
type ExecPoolSnapshot struct {
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `ProposalKey` stores the key used to access the related value.
	ProposalKey string `json:"proposal_key,omitempty"`
	// `Hashes` stores the digest used to identify or verify the related data.
	Hashes map[string][]string `json:"hashes"`
	// `TxMerkle` stores the transaction data handled by this operation.
	TxMerkle map[string]string `json:"tx_merkle"`
}

// LogicalClock tracks deterministic chain time (no wall clock).
type LogicalClock struct {
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Tick` stores the value associated with this record.
	Tick uint64 `json:"tick"`
}

// `SnapshotVersion` defines the current snapshot wire format. Version 5 binds
// every field that can change future validator authority (the registry and
// queued validator transitions) into SnapshotHash.
const SnapshotVersion = 5

// Snapshot v4 first introduced an execution-stage ledger. Keep recognizing it
// for passive historical storage, but only v5 has a complete authority-bound
// canonical identity and may become live chain state.
const snapshotExecutionLedgerVersion = 4
const snapshotAuthorityBindingVersion = 5

// `snapshotLedgerStageExecution` defines the constant value used by this package.
const snapshotLedgerStageExecution = "execution"

// SnapshotRetention keeps the last N finalized snapshots available for joiners.
const SnapshotRetention = 1000

var (
	// `DebugConsensus` stores the value used by this operation.
	DebugConsensus = envBool("MSC_DEBUG_CONSENSUS")
	// `DebugNet` stores the value used by this operation.
	DebugNet = envBool("MSC_DEBUG_NET")
	// `DebugSync` stores the value used by this operation.
	DebugSync = envBool("MSC_DEBUG_SYNC")
	// `ExecutionDeterminismGuardEnabled` stores whether the related condition is satisfied.
	ExecutionDeterminismGuardEnabled = true
	// `ResultGossipOnly` stores the result produced by this operation.
	ResultGossipOnly = true
	// `DisableDHT` stores the value used by this operation.
	DisableDHT = false
	// `BlockPublicPeers` stores the block data handled by this operation.
	BlockPublicPeers = false
	// `PeerDiversityEnabled` stores whether the related condition is satisfied.
	PeerDiversityEnabled = true
	// `PeerDiversityIPv4Prefix` stores the value used by this operation.
	PeerDiversityIPv4Prefix = 24
	// `PeerDiversityIPv6Prefix` stores the value used by this operation.
	PeerDiversityIPv6Prefix = 64
	// `PeerDiversityMaxPerSubnet` stores the value used by this operation.
	PeerDiversityMaxPerSubnet = 8
	// `PeerDiversityMaxOutboundPerSubnet` stores the value used by this operation.
	PeerDiversityMaxOutboundPerSubnet = 4
	// `PeerDiversityMaxPerASN` stores the value used by this operation.
	PeerDiversityMaxPerASN = 12
	// `PeerDiversityMaxOutboundPerASN` stores the value used by this operation.
	PeerDiversityMaxOutboundPerASN = 6
	// `PeerMemoryQuotaBytes` stores the value used by this operation.
	PeerMemoryQuotaBytes = 16 << 20
	// `PeerBandwidthQuotaBytesPerMinute` stores the value used by this operation.
	PeerBandwidthQuotaBytesPerMinute = 64 << 20
	// `PeerMempoolTxPerMinute` stores the value used by this operation.
	PeerMempoolTxPerMinute = 240
	// `PeerBlockRequestsPerMinute` stores the value used by this operation.
	PeerBlockRequestsPerMinute = 120
	// `PeerConnectionFloodMaxPerWindow` stores the value used by this operation.
	PeerConnectionFloodMaxPerWindow = 20
	// `PeerDiscoveryMaxAddrs` stores the value used by this operation.
	PeerDiscoveryMaxAddrs = 16
	// `PeerResourceWindowDuration` stores the value used by this operation.
	PeerResourceWindowDuration = time.Minute
	// `EnableMDNS` stores the value used by this operation.
	EnableMDNS = true
	// `SelfHealEnabled` stores whether the related condition is satisfied.
	SelfHealEnabled = false
	// `SelfHealInterval` stores the value currently being processed.
	SelfHealInterval = 15 * time.Second
	// `SelfHealMinPeers` stores the value used by this operation.
	SelfHealMinPeers = MinimumPeers
	// `SelfHealStallSeconds` stores the value used by this operation.
	SelfHealStallSeconds uint64 = 45
	// `EnableAutoNAT` stores the value used by this operation.
	EnableAutoNAT = false
	// `EnableRelay` stores the value used by this operation.
	EnableRelay = false
	// `EnableHolePunch` stores the value used by this operation.
	EnableHolePunch = false
)

const (
	// `MinimumPeers` is the production emergency floor. Local configuration
	// cannot reduce discovery/self-heal below this safety value.
	MinimumPeers = 8
	// `TargetPeers` is the healthy sparse-network target. Nodes discover and
	// dial toward this value without requiring a validator full mesh.
	TargetPeers = 16
	// `MaxPeers` defines the constant value used by this package.
	MaxPeers = 32
	// `MaxInboundPeers` defines the constant value used by this package.
	MaxInboundPeers = 24
	// `MaxOutboundPeers` defines the constant value used by this package.
	MaxOutboundPeers = 16
	// `MaxConnections` defines the constant value used by this package.
	MaxConnections = 64
	// `MaxPendingConn` defines the constant value used by this package.
	MaxPendingConn = 10
	// `MaxPeerOutboundQueue` defines the constant value used by this package.
	MaxPeerOutboundQueue = 512
	// `MaxValidateQueue` defines the constant value used by this package.
	MaxValidateQueue = 128
	// `MaxValidateWorkers` defines the constant value used by this package.
	MaxValidateWorkers = 8
	// `MaxTxPerSecondPerSender` defines the constant value used by this package.
	MaxTxPerSecondPerSender = 20
	// `GoroutineWarnThreshold` defines the constant value used by this package.
	GoroutineWarnThreshold = 6000
	// `ExecResultsMaxEntries` defines the constant value used by this package.
	ExecResultsMaxEntries = 20000
	// `PendingBlocksMaxEntries` defines the constant value used by this package.
	PendingBlocksMaxEntries = 2000
	// `QueuedExecVotesMaxKeys` defines the constant value used by this package.
	QueuedExecVotesMaxKeys = 2000
	// `AcceptedProposalMaxKeys` defines the constant value used by this package.
	AcceptedProposalMaxKeys = 2000
	// `ExecBroadcastedMaxEpoch` defines the constant value used by this package.
	ExecBroadcastedMaxEpoch = 1024
	// `ExecSignerSeenMaxEpoch` defines the constant value used by this package.
	ExecSignerSeenMaxEpoch = 1024
	// `ExecBroadcastedByValMax` defines the constant value used by this package.
	ExecBroadcastedByValMax = 1024
	// `ExecVoteReplayMaxKeys` defines the constant value used by this package.
	ExecVoteReplayMaxKeys = 5000
	// `ExecMismatchTrackMax` defines the constant value used by this package.
	ExecMismatchTrackMax = 2048
)

const (
	// `TxLimiterIdleTTL` defines the transaction data handled by this operation.
	TxLimiterIdleTTL = 10 * time.Minute
	// `GoroutineWarnInterval` defines the value currently being processed.
	GoroutineWarnInterval = 10 * time.Second
	// `MapStatsInterval` defines the value currently being processed.
	MapStatsInterval = 60 * time.Second
)

// Validator-set updates activate after N finalized blocks (decision height + delay).
var ValidatorSetActivationDelay uint64 = 5

// `ValidatorSetActivationModelV2Height` stores whether the related condition is satisfied.
var ValidatorSetActivationModelV2Height uint64 = 0

// `ValidatorSetCommitmentV2Height` stores whether the related condition is satisfied.
var ValidatorSetCommitmentV2Height uint64 = 0

// `PromotionWindowRecordV1Height` stores the value used by this operation.
var PromotionWindowRecordV1Height uint64 = 0

// `ValidatorSetHashV3Height` stores whether the related condition is satisfied.
var ValidatorSetHashV3Height uint64 = 0

const (
	// `activationDelayModelV1DoubleHold` defines the constant value used by this package.
	activationDelayModelV1DoubleHold = "v1_double_hold"
	// `activationDelayModelV2Single` defines the constant value used by this package.
	activationDelayModelV2Single = "v2_single_phase"
)

// validatorSetActivationDelayBlocks implements the validator set activation delay blocks helper.
func validatorSetActivationDelayBlocks() uint64 {
	if ValidatorSetActivationDelay == 0 {
		return 5
	}
	return ValidatorSetActivationDelay
}

// validatorSetCommitmentV2EnabledAt implements the validator set commitment v2 enabled at helper.
func validatorSetCommitmentV2EnabledAt(height uint64) bool {
	if height == 0 {
		return false
	}
	if ValidatorSetCommitmentV2Height == 0 {
		// Treat 0 as "enabled from genesis" to ensure validator_set_root is always committed.
		return true
	}
	// Special-case max value to allow explicit disable in tests or local configs.
	if ValidatorSetCommitmentV2Height == ^uint64(0) {
		return false
	}
	return height >= ValidatorSetCommitmentV2Height
}

// promotionWindowRecordV1EnabledAt implements the promotion window record v1 enabled at helper.
func promotionWindowRecordV1EnabledAt(height uint64) bool {
	if height == 0 || PromotionWindowRecordV1Height == 0 || PromotionWindowRecordV1Height == ^uint64(0) {
		return false
	}
	return height >= PromotionWindowRecordV1Height
}

// validatorSetHashV3EnabledAt implements the validator set hash v3 enabled at helper.
func validatorSetHashV3EnabledAt(height uint64) bool {
	if height == 0 || ValidatorSetHashV3Height == 0 {
		return false
	}
	return height >= ValidatorSetHashV3Height
}

// validatorSetTransitionHoldBlocks adds a second deterministic hold window
// between "scheduled" and "active" validator-set phases.
func validatorSetTransitionHoldBlocks() uint64 {
	return validatorSetActivationDelayBlocks()
}

// alignHeightToCheckpointBoundary implements the align height to checkpoint boundary helper.
func alignHeightToCheckpointBoundary(height uint64, interval uint64) uint64 {
	if height == 0 {
		return 0
	}
	if interval <= 1 {
		return height
	}
	// `rem` stores the value produced by this operation.
	rem := height % interval
	if rem == 0 {
		return height
	}
	// `step` stores the value produced by this operation.
	step := interval - rem
	// `maxU64` stores the value produced by this operation.
	maxU64 := ^uint64(0)
	if height > maxU64-step {
		return maxU64
	}
	return height + step
}

// effectiveValidatorSetActivationHeight converts a scheduled transition height
// into the actual height when the validator set becomes active.
func effectiveValidatorSetActivationHeight(scheduledHeight uint64) uint64 {
	if scheduledHeight == 0 {
		return 0
	}
	// `hold` stores the value produced by this operation.
	hold := validatorSetTransitionHoldBlocks()
	if hold == 0 {
		return scheduledHeight
	}
	// `maxU64` stores the value produced by this operation.
	maxU64 := ^uint64(0)
	if scheduledHeight > maxU64-hold {
		return maxU64
	}
	return scheduledHeight + hold
}

// validatorSetActivationDelayModelAtHeight implements the validator set activation delay model at height helper.
func validatorSetActivationDelayModelAtHeight(evalHeight uint64) string {
	// `switchHeight` stores the value produced by this operation.
	switchHeight := ValidatorSetActivationModelV2Height
	if switchHeight > 0 && evalHeight >= switchHeight {
		return activationDelayModelV2Single
	}
	return activationDelayModelV1DoubleHold
}

// effectiveValidatorSetActivationHeightAt returns effective validator set activation height at.
func effectiveValidatorSetActivationHeightAt(scheduledHeight uint64, evalHeight uint64) uint64 {
	if scheduledHeight == 0 {
		return 0
	}
	// `effective` stores the value produced by this operation.
	effective := uint64(0)
	// `singlePhase` stores the value produced by this operation.
	singlePhase := validatorSetActivationDelayModelAtHeight(evalHeight) == activationDelayModelV2Single
	if singlePhase {
		// v2 uses a single-phase interpretation: queue height is the effective height.
		effective = scheduledHeight
	} else {
		effective = effectiveValidatorSetActivationHeight(scheduledHeight)
	}
	if singlePhase {
		// v2 still activates on checkpoint boundaries; only the interpretation of
		// the queued height changes from double-hold to single-phase.
	}
	if validatorEpochSetV1EnabledAt(effective) {
		return nextValidatorEpochBoundaryAtOrAfter(effective)
	}
	// Mainnet-safe deterministic activation: apply validator-set transitions
	// only at checkpoint boundaries across the network.
	interval := SyncCheckpointIntervalBlocks
	if interval == 0 {
		interval = 32
	}
	return alignHeightToCheckpointBoundary(effective, interval)
}

// validatorSetTransitionUsesStrictParentCommitment implements the validator set transition uses strict parent commitment helper.
func validatorSetTransitionUsesStrictParentCommitment(scheduledHeight uint64) bool {
	if scheduledHeight == 0 {
		return false
	}
	return validatorSetCommitmentV2EnabledAt(scheduledHeight)
}

// validatorSetTransitionActivationHeightAt implements the validator set transition activation height at helper.
func validatorSetTransitionActivationHeightAt(scheduledHeight uint64, evalHeight uint64) uint64 {
	if scheduledHeight == 0 {
		return 0
	}
	if validatorSetTransitionUsesStrictParentCommitment(scheduledHeight) {
		return validatorEpochTransitionHeight(scheduledHeight)
	}
	return effectiveValidatorSetActivationHeightAt(scheduledHeight, evalHeight)
}

// validatorSetTransitionVisibleInChildSetAt implements the validator set transition visible in child set at helper.
func validatorSetTransitionVisibleInChildSetAt(scheduledHeight uint64, childHeight uint64) bool {
	if scheduledHeight == 0 || childHeight == 0 {
		return false
	}
	if validatorSetTransitionUsesStrictParentCommitment(scheduledHeight) {
		activationHeight := validatorEpochTransitionHeight(scheduledHeight)
		return activationHeight > 0 && activationHeight <= childHeight
	}
	// `effectiveActivation` stores the value produced by this operation.
	effectiveActivation := effectiveValidatorSetActivationHeightAt(scheduledHeight, childHeight)
	return effectiveActivation > 0 && effectiveActivation <= childHeight
}

// ValidatorInactiveBlocks defines how many blocks a validator can stay inactive before removal.
// 0 disables automatic removal.
var ValidatorInactiveBlocks uint64 = 0

// ValidatorSetRotationWindow defines how many blocks between validator set rotations.
// 0 disables rotation.
var ValidatorSetRotationWindow uint64 = 0

// ValidatorInactivePermanentRemove controls whether inactive validators are permanently exited.
var ValidatorInactivePermanentRemove bool = false

// ValidatorMinActiveSet is the minimum validator count that must remain active.
// Validator-set removals are blocked if they would reduce active validators below this floor.
var ValidatorMinActiveSet int = 4

// minActiveValidatorsFloor returns the minimum active validators floor.
func minActiveValidatorsFloor() int {
	return protocolValidatorMinActiveSetValue()
}

// ValidatorBannedList contains validator IDs that are explicitly excluded from selection.
var ValidatorBannedList []string

// `validatorBannedSet` stores whether the related condition is satisfied.
var validatorBannedSet = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `m` stores the value associated with this record.
	m map[string]struct{}
}{
	m: make(map[string]struct{}),
}

// ValidatorSetMismatchResyncThreshold triggers a forced resync after repeated validator-set mismatches.
// 0 disables auto-heal.
var ValidatorSetMismatchResyncThreshold int = 5

// ValidatorSetMismatchResyncWindow is the time window for mismatch counting.
var ValidatorSetMismatchResyncWindow = 30 * time.Second

// ConsensusMinBlockInterval enforces a minimum wall-clock delay between
// consecutive block heights on each node.
var ConsensusMinBlockInterval = 4 * time.Second

// ConsensusRecomputePause pauses proposal/finalization briefly when validator
// set membership changes or a validator-set mismatch is detected.
var ConsensusRecomputePause = 4 * time.Second

// ConsensusDetectorDegradedAfter controls when CMD marks sustained block/finality
// slowness as DEGRADED. It is intentionally above normal 4-6s cadence.
var ConsensusDetectorDegradedAfter = 15 * time.Second

// ConsensusDetectorHaltedAfter controls when CMD reports finality timeout as HALTED.
var ConsensusDetectorHaltedAfter = 60 * time.Second

// ConsensusDetectorRecoveryValidatorLagBlocks sends large lag into RECOVERY
// instead of DEGRADED because catch-up behavior should be explicit.
var ConsensusDetectorRecoveryValidatorLagBlocks uint64 = 100

// WeakSubjectivityDepth rejects blocks that are too far behind local finalized
// history to mitigate long-range rewrite attacks. 0 disables the guard.
var WeakSubjectivityDepth uint64 = 2048

// MaxFutureBlockGap bounds queued future blocks to reduce eclipse/sybil DoS
// memory pressure from very large height jumps.
var MaxFutureBlockGap uint64 = 256

// EnforceDeterministicTxOrder is retained for configuration/telemetry
// compatibility. Work-block verification always enforces canonical
// fee/expiry/id ordering as a protocol rule and cannot be disabled locally.
var EnforceDeterministicTxOrder = true

// ValidatorSetRepairCooldown limits repeated local repair attempts for the
// same validator-set mismatch tuple (height, expected, got).
var ValidatorSetRepairCooldown = 3 * time.Second

// ValidatorSetRepairMaxAttempts bounds local repair attempts in one window.
// Exceeding this enters temporary backoff.
var ValidatorSetRepairMaxAttempts = 6

// ValidatorSetRepairBackoff pauses local repair attempts after repeated
// failures for the same mismatch tuple.
var ValidatorSetRepairBackoff = 20 * time.Second

// SyncSnapshotCatchupThresholdBlocks forces trusted snapshot catch-up when
// local node lag exceeds this block gap.
var SyncSnapshotCatchupThresholdBlocks uint64 = 2000

// `SyncFastBlockSyncMaxBlocks` stores the value used by this operation.
var SyncFastBlockSyncMaxBlocks uint64 = 256

// `SyncRestartSnapshotGraceBlocks` stores the value used by this operation.
var SyncRestartSnapshotGraceBlocks uint64 = 256

// `SyncDirectGossipMaxBlocks` stores the value used by this operation.
var SyncDirectGossipMaxBlocks uint64 = 128

// `SyncRangeFetchMaxBlocks` stores the value used by this operation.
var SyncRangeFetchMaxBlocks uint64 = 50000

// `SyncBlockRangeReplicationFactor` stores the value used by this operation.
var SyncBlockRangeReplicationFactor = 2

// `SyncSnapshotDeltaMaxBlocks` stores the value used by this operation.
var SyncSnapshotDeltaMaxBlocks uint64 = 10000000

// `SyncDeltaReplayMaxBlocks` stores the value used by this operation.
var SyncDeltaReplayMaxBlocks uint64 = 50000

// `SyncDeltaReplayBatchBlocks` stores the value used by this operation.
var SyncDeltaReplayBatchBlocks uint64 = 1024

// `SyncDeltaReplayVerifyWorkers` stores the value used by this operation.
var SyncDeltaReplayVerifyWorkers = 8

// `SyncDeltaStateSyncEnabled` stores whether the related condition is satisfied.
var SyncDeltaStateSyncEnabled = true

// `SyncEd25519BatchVerifyWorkers` stores the value used by this operation.
var SyncEd25519BatchVerifyWorkers = 8

// `SyncSnapshotDistributionEnabled` stores whether the related condition is satisfied.
var SyncSnapshotDistributionEnabled = true

// `SyncSnapshotMultiPeerChunkFetch` stores the value used by this operation.
var SyncSnapshotMultiPeerChunkFetch = true

// `SyncSnapshotCompression` stores the value used by this operation.
var SyncSnapshotCompression = "zstd"

// `SyncSnapshotChunkSizeBytes` stores the value used by this operation.
var SyncSnapshotChunkSizeBytes uint64 = 1024 * 1024

// `SyncSnapshotParallelChunks` stores the value used by this operation.
var SyncSnapshotParallelChunks = 8

// `SyncSnapshotChunkReplicationFactor` stores the value used by this operation.
var SyncSnapshotChunkReplicationFactor = 2

// `SyncStallSeconds` stores the value used by this operation.
var SyncStallSeconds uint64 = 180

// `SyncPeerTimeoutSeconds` stores the value used by this operation.
var SyncPeerTimeoutSeconds uint64 = 15

// `SyncHeaderBatchSize` stores the measured quantity used by this operation.
var SyncHeaderBatchSize uint64 = 256

// `SyncHeaderCommonAncestorDepth` stores the value used by this operation.
var SyncHeaderCommonAncestorDepth uint64 = 2048

// `SyncHistoryMode` stores the value used by this operation.
var SyncHistoryMode = "background"

// `SyncFreshJoinFallbackBlockReplayEnabled` stores whether the related condition is satisfied.
var SyncFreshJoinFallbackBlockReplayEnabled = false

// `SyncSnapshotPublishNewNodeThresholdBlocks` stores the value used by this operation.
var SyncSnapshotPublishNewNodeThresholdBlocks uint64 = 50

// `SyncSnapshotPublishLagThresholdBlocks` stores the value used by this operation.
var SyncSnapshotPublishLagThresholdBlocks uint64 = 20

// `SyncSnapshotPublishReannounceCooldownSeconds` stores the value used by this operation.
var SyncSnapshotPublishReannounceCooldownSeconds uint64 = 15

// `SyncTrustedSnapshotRequireCheckpointProof` stores the value used by this operation.
var SyncTrustedSnapshotRequireCheckpointProof = true

// `SyncSnapshotAnchorTimeoutSeconds` stores the value used by this operation.
var SyncSnapshotAnchorTimeoutSeconds uint64 = 10

// `SyncSnapshotAnchorMaxRetries` stores the value used by this operation.
var SyncSnapshotAnchorMaxRetries uint64 = 6

// `SyncCheckpointIntervalBlocks` stores the value used by this operation.
var SyncCheckpointIntervalBlocks uint64 = 32

// `SyncSnapshotCheckpointDomain` stores the value used by this operation.
var SyncSnapshotCheckpointDomain = "MSC_SNAPSHOT_V1"

// `SyncSnapshotCheckpointV2Height` stores the value used by this operation.
var SyncSnapshotCheckpointV2Height uint64 = 0

// `SupplyCapV2ActivationHeight` records the protocol gate used for any
// governance-approved max supply change.
var SupplyCapV2ActivationHeight uint64 = 0

// `SyncSnapshotSessionTTLSeconds` stores the value used by this operation.
var SyncSnapshotSessionTTLSeconds uint64 = 0

// `SyncSnapshotQuorumApplyWatchdogSeconds` stores the value used by this operation.
var SyncSnapshotQuorumApplyWatchdogSeconds uint64 = 20

// `SyncSnapshotSessionResetWatchdogSeconds` stores the value used by this operation.
var SyncSnapshotSessionResetWatchdogSeconds uint64 = 60

// `SyncSnapshotInvalidProofQuarantineAfter` stores the value used by this operation.
var SyncSnapshotInvalidProofQuarantineAfter uint64 = 3

// `SyncUsableHeadFastBootstrapEnabled` stores whether the related condition is satisfied.
var SyncUsableHeadFastBootstrapEnabled = true

// `SyncUsableHeadRoles` stores the value used by this operation.
var SyncUsableHeadRoles = []string{"full", "archive"}

// `SyncUsableHeadMinGapBlocks` stores the value used by this operation.
var SyncUsableHeadMinGapBlocks uint64 = 2000

// `SyncUsableHeadTargetSeconds` stores the value used by this operation.
var SyncUsableHeadTargetSeconds uint64 = 3

// `SyncUsableHeadRequireCheckpointProof` stores the value used by this operation.
var SyncUsableHeadRequireCheckpointProof = true

// `SyncUsableHeadBackgroundHistory` stores the value used by this operation.
var SyncUsableHeadBackgroundHistory = true

// `SyncUsableHeadRecentReplayWindowBlocks` stores the value used by this operation.
var SyncUsableHeadRecentReplayWindowBlocks uint64 = 2048

// `SyncSnapshotReplicationMinCopies` stores the value used by this operation.
var SyncSnapshotReplicationMinCopies = 3

// `SyncSnapshotWarmupBlocks` stores the value used by this operation.
var SyncSnapshotWarmupBlocks uint64 = 5

// `SyncSnapshotWarmupSeconds` stores the value used by this operation.
var SyncSnapshotWarmupSeconds uint64 = 10

// `StorageEpochLengthBlocks` stores the value used by this operation.
var StorageEpochLengthBlocks uint64 = 100

// `StorageHistoryProfile` stores the value used by this operation.
var StorageHistoryProfile = "auto"

// `StorageStatePruningEnabled` stores whether the related condition is satisfied.
var StorageStatePruningEnabled = true

// `StorageValidatorRetainedEpochs` stores the value used by this operation.
var StorageValidatorRetainedEpochs uint64 = 10

// `StorageValidatorRollbackWindowBlocks` stores the value used by this operation.
var StorageValidatorRollbackWindowBlocks uint64 = 256

// `StorageValidatorSnapshotKeepLast` stores the value used by this operation.
var StorageValidatorSnapshotKeepLast uint64 = 3

// `StorageValidatorRecentBlockWindow` stores the value used by this operation.
var StorageValidatorRecentBlockWindow uint64 = 2048

// `StorageFullNodeHistoryBlocks` stores the value used by this operation.
var StorageFullNodeHistoryBlocks uint64 = 5256000

// `StorageHourlySnapshotRetain` stores the value used by this operation.
var StorageHourlySnapshotRetain uint64 = 24

// `StorageDailySnapshotRetain` stores the value used by this operation.
var StorageDailySnapshotRetain uint64 = 30

// `StorageWeeklySnapshotRetain` stores the value used by this operation.
var StorageWeeklySnapshotRetain uint64 = 12

// `StorageMonthlySnapshotRetain` stores the value used by this operation.
var StorageMonthlySnapshotRetain uint64 = 24

// `StorageHourlySnapshotIntervalBlocks` stores the value used by this operation.
var StorageHourlySnapshotIntervalBlocks uint64 = 3600

// `StorageColdExportEnabled` stores whether the related condition is satisfied.
var StorageColdExportEnabled = true

// `StorageColdExportCompression` stores the value used by this operation.
var StorageColdExportCompression = "zstd"

// `StorageParallelGCWorkers` stores the value used by this operation.
var StorageParallelGCWorkers uint64 = 4

// `StorageStateRentEnabled` stores whether the related condition is satisfied.
var StorageStateRentEnabled = false

// `StorageStateRentArchiveInactiveAfterEpochs` stores the value used by this operation.
var StorageStateRentArchiveInactiveAfterEpochs uint64 = 0

// `StorageStateLayoutMode` stores the value used by this operation.
var StorageStateLayoutMode = "merkle"

const (
	// `SyncHistoryModeNone` defines the constant value used by this package.
	SyncHistoryModeNone = "none"
	// `SyncHistoryModeBackground` defines the constant value used by this package.
	SyncHistoryModeBackground = "background"
	// `SyncHistoryModeArchive` defines the constant value used by this package.
	SyncHistoryModeArchive = "archive_full"
)

// Hybrid validator liveness controls.
var ValidatorLivenessHeartbeatTTLSeconds uint64 = 25

// `ValidatorLivenessGraceSeconds` stores whether the related condition is satisfied.
var ValidatorLivenessGraceSeconds uint64 = 10

// `ValidatorLivenessMaxHeightDriftBlocks` stores whether the related condition is satisfied.
var ValidatorLivenessMaxHeightDriftBlocks uint64 = 8

// Delayed rejoin controls for previously offline validators.
var ValidatorRejoinRequiredHeartbeats uint16 = 3

// `ValidatorRejoinRequiredSignedBlocks` stores whether the related condition is satisfied.
var ValidatorRejoinRequiredSignedBlocks uint64 = 1

// `ValidatorRejoinWindowBlocks` stores whether the related condition is satisfied.
var ValidatorRejoinWindowBlocks uint64 = 16

// Validator startup safety controls.
var ValidatorFailOnKeyUnavailable = true

// `ValidatorAllowIdentityRotationOnExistingChain` stores whether the related condition is satisfied.
var ValidatorAllowIdentityRotationOnExistingChain = false

// `ValidatorRequiredKeyFingerprint` stores whether the related condition is satisfied.
var ValidatorRequiredKeyFingerprint = ""

// `ValidatorKeyBackupRequired` stores whether the related condition is satisfied.
var ValidatorKeyBackupRequired = true

// `ValidatorKeyBackupDir` stores whether the related condition is satisfied.
var ValidatorKeyBackupDir = "secure-backups"

// `ValidatorKeyBackupMaxAgeHours` stores whether the related condition is satisfied.
var ValidatorKeyBackupMaxAgeHours uint64 = 24

// `ValidatorKeyRestoreAllowedOnMissing` stores whether the related condition is satisfied.
var ValidatorKeyRestoreAllowedOnMissing = true

// `ValidatorAllowEnvPasswordInProduction` stores whether the related condition is satisfied.
var ValidatorAllowEnvPasswordInProduction = false

// `ValidatorCoreEnvPasswordAllowed` stores whether the related condition is satisfied.
var ValidatorCoreEnvPasswordAllowed = false

// `ValidatorCorePasswordFile` stores whether the related condition is satisfied.
var ValidatorCorePasswordFile = ""

// `ValidatorPasswordMode` stores whether the related condition is satisfied.
var ValidatorPasswordMode = "file_or_prompt"

// Genesis runtime locks are set from production genesis during node startup.
// A frozen genesis validator set may observe candidates, but it must not admit
// them into the consensus set without an explicit protocol/governance change.
var GenesisRuntimeLocked = false

// `GenesisValidatorSetFrozen` stores the value used by this operation.
var GenesisValidatorSetFrozen = false

// `GenesisFrozenValidatorSetSize` stores the measured quantity used by this operation.
var GenesisFrozenValidatorSetSize = 0

// Round-based proposer failover controls.
var ProposerRoundTimeout = 15 * time.Second

// ConsensusFastProposerFailoverEnabled lets a healthy network replace a stalled
// proposer using proposer_round_timeout_seconds without silently flooring it to
// min_block_interval. It does not change quorum/finality requirements.
var ConsensusFastProposerFailoverEnabled = true

// `ConsensusFastProposerFailoverMin` stores the value used by this operation.
var ConsensusFastProposerFailoverMin = 1 * time.Second

// Zero keeps round recovery uncapped unless an operator explicitly sets a limit.
var ProposerRoundMax uint32 = 0

// `ProposerRoundMaxSkew` stores the value used by this operation.
var ProposerRoundMaxSkew uint32 = 1

// `ConsensusProposalDeadlineGuard` stores the value used by this operation.
var ConsensusProposalDeadlineGuard = 200 * time.Millisecond

// Transition barrier relax controls during sustained stalls.
var TransitionBarrierRelaxTimeout = 60 * time.Second

// `TransitionBarrierMaxDrop` stores the value used by this operation.
var TransitionBarrierMaxDrop = 1

const (
	// `transitionBarrierRetryModeHybrid` defines the constant value used by this package.
	transitionBarrierRetryModeHybrid = "hybrid"
	// `transitionBarrierRetryModePerBlock` defines the synchronization state protecting shared data.
	transitionBarrierRetryModePerBlock = "per_block"
)

// `TransitionBarrierRetryMode` stores the value used by this operation.
var TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock

// normalizeTransitionBarrierRetryMode normalizes transition barrier retry mode.
func normalizeTransitionBarrierRetryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", transitionBarrierRetryModePerBlock:
		return transitionBarrierRetryModePerBlock
	case transitionBarrierRetryModeHybrid:
		return transitionBarrierRetryModeHybrid
	default:
		return transitionBarrierRetryModePerBlock
	}
}

// Explicit gate for emergency execution quorum relaxation.
var ExecQuorumEmergencyEnabled = true

// Consensus safety policy controls.
var ConsensusPenaltyEnforceMode = "converged_only"

// `ConsensusInvalidProposerQuarantineAfter` stores the value used by this operation.
var ConsensusInvalidProposerQuarantineAfter = 4

// `ConsensusExecMismatchQuarantineAfter` stores the value used by this operation.
var ConsensusExecMismatchQuarantineAfter = 3

// `ConsensusExecMismatchSlashAfter` stores the value used by this operation.
var ConsensusExecMismatchSlashAfter = 5

// `ConsensusProposeRequiresSyncReady` stores the value used by this operation.
var ConsensusProposeRequiresSyncReady = true

// `ConsensusCorePendingExcludedFromProposer` stores the value used by this operation.
var ConsensusCorePendingExcludedFromProposer = true

// `ConsensusCoreActivationEffectiveHeightBuffer` stores the value used by this operation.
var ConsensusCoreActivationEffectiveHeightBuffer uint64 = 64

// `ConsensusPostBlockSafeModeEnabled` stores whether the related condition is satisfied.
var ConsensusPostBlockSafeModeEnabled = true

// `ConsensusPostBlockSafeModeMin` stores the value used by this operation.
var ConsensusPostBlockSafeModeMin = 5 * time.Second

// `ConsensusPostBlockSafeModeMax` stores the value used by this operation.
var ConsensusPostBlockSafeModeMax = 8 * time.Second

// `ConsensusPostBlockSafeModeHistoryBlocks` stores the value used by this operation.
var ConsensusPostBlockSafeModeHistoryBlocks uint64 = 32

// `ConsensusPostBlockSafeModeLiveQuorumBPS` stores the value used by this operation.
var ConsensusPostBlockSafeModeLiveQuorumBPS uint64 = 6700

// Signed core registry controls.
var CoreRegistryPath = "core_validators.json"

// `CoreRegistryEnforcementMode` stores the value used by this operation.
var CoreRegistryEnforcementMode = "warn"

// `CoreRegistryMinSignatures` stores the result produced by this operation.
var CoreRegistryMinSignatures = 0

// `CoreRegistryReloadSeconds` stores the value used by this operation.
var CoreRegistryReloadSeconds uint64 = 10

// Peer identity auto-heal persistence controls.
var PersistPeerIDRefresh = true

// Localhost dial-refused pruning controls.
var PruneLocalhostOnRefused = true

// `PruneLocalhostRefusedFailures` stores the result produced by this operation.
var PruneLocalhostRefusedFailures = 3

// `MapStatsEnabled` stores whether the related condition is satisfied.
var MapStatsEnabled = true

// `FailedHeights` stores the value used by this operation.
var FailedHeights = make(map[uint64]bool)

type ConsensusPhase uint8

// Add in global variables
var (
	// `connectionLimiter` stores the value used by this operation.
	connectionLimiter = rate.NewLimiter(10, 20) // 10 conn/sec, burst 20
	// `messageLimiter` stores the value used by this operation.
	messageLimiter = make(map[string]*rate.Limiter)
	// `messageLimiterLastSeen` stores the value used by this operation.
	messageLimiterLastSeen = make(map[string]time.Time)
	// `limiterMu` stores the synchronization state protecting shared data.
	limiterMu sync.Mutex
)

// `ValidatorAddrBook` stores whether the related condition is satisfied.
var ValidatorAddrBook = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `m` stores the value associated with this record.
	m map[string]string // validatorID -> p2pAddr
}{
	m: make(map[string]string),
}

type ConsensusState struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.Mutex
	// `Height` stores the value associated with this record.
	Height uint64
	// `Votes` stores the value associated with this record.
	Votes map[uint64]map[string]BlockVote
	// `Proposals` stores the value associated with this record.
	Proposals map[uint64]Block // ✅ FIXED (Block, not string)
	// `LastCleanedHeight` stores the value associated with this record.
	LastCleanedHeight uint64
	// `Round` stores the value associated with this record.
	Round uint32
	// `Phase` stores the value associated with this record.
	Phase ConsensusPhase
	// `RoundStart` stores the value associated with this record.
	RoundStart time.Time
	// `Timeout` stores the result produced by this operation.
	Timeout time.Duration
	// `LastFinalized` stores the value associated with this record.
	LastFinalized uint64
	// `Validators` stores whether the related condition is satisfied.
	Validators []string
	// LockedBlock is the active-height consensus lock keyed by block hash.
	LockedBlock string
	// ExecVotes is the active-height execution vote view keyed by block hash -> validator -> vote.
	ExecVotes map[string]map[string]ExecutionResult
	// Committed reports whether the current active height has observed a commit barrier.
	Committed bool
	// LockedBlockHash is kept for compatibility with older call sites.
	LockedBlockHash string
	// `LockedRound` stores the synchronization state protecting shared data.
	LockedRound uint32
	// `LastProposedHeight` stores the value associated with this record.
	LastProposedHeight uint64
	// `LastProposedRound` stores the value associated with this record.
	LastProposedRound uint32
	// `Paused` stores the value associated with this record.
	Paused bool
	// `Syncing` stores the value associated with this record.
	Syncing bool
	// `SyncTarget` stores the value associated with this record.
	SyncTarget uint64
	// `syncInFlight` stores the value associated with this record.
	syncInFlight bool

	// MODEL-2 / MODEL-3
	Executed bool
	// `Finalized` stores the value associated with this record.
	Finalized bool
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
}

// =====================================================
// 🔐 PROPOSAL VOTE REGISTRY (GLOBAL, DETERMINISTIC)
// =====================================================
var ProposalVotes = struct {
	// `Mutex` stores the synchronization state protecting shared data.
	sync.Mutex
	// `Votes` stores the value associated with this record.
	Votes map[int]map[string][]byte // height -> validator -> signature
}{
	Votes: make(map[int]map[string][]byte),
}

const (
	// `PhasePropose` defines the constant value used by this package.
	PhasePropose ConsensusPhase = iota
	// `PhaseVote` defines the constant value used by this package.
	PhaseVote
	// `PhaseFinalize` defines the constant value used by this package.
	PhaseFinalize
)
const (
	// `rateWindow` defines the constant value used by this package.
	rateWindow = 10 * time.Second
	// `maxRequestsIP` defines the constant value used by this package.
	maxRequestsIP = 20
)

type ConsensusRound struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `Leader` stores the value associated with this record.
	Leader string
	// `Block` stores the synchronization state protecting shared data.
	Block *Block
	// `Votes` stores the value associated with this record.
	Votes map[string]BlockVote
	// `Phase` stores the value associated with this record.
	Phase ConsensusPhase
	// `Deadline` stores the value associated with this record.
	Deadline int64
}
type EvidenceSummary struct {
	// `Witnesses` stores the value associated with this record.
	Witnesses int64
	// `LastSeen` stores the value associated with this record.
	LastSeen int64
}

// `pendingSpendMu` stores the synchronization state protecting shared data.
var pendingSpendMu sync.Mutex

// `PendingSpend` stores the value used by this operation.
var PendingSpend = map[string]int{}

// `CensorshipSlashThreshold` defines the constant value used by this package.
const CensorshipSlashThreshold = 2.5

// `CensorshipEvidencePool` stores the value used by this operation.
var CensorshipEvidencePool = map[EvidenceKey][]CensorshipEvidence{}

const (
	// `MinCensorshipWitnesses` defines the constant value used by this package.
	MinCensorshipWitnesses = 2 // stake-less threshold
	// `MaxEvidencePerHeight` defines the constant value used by this package.
	MaxEvidencePerHeight = 20 // anti-spam
)

type GossipMessage struct {
	// `Type` stores the value associated with this record.
	Type string // "block" | "censorship"
	// `Data` stores the value associated with this record.
	Data []byte
}

const (
	// `EvidenceTTLBlocks` defines the constant value used by this package.
	EvidenceTTLBlocks = 200 // evidence expires after 200 blocks
	// `EvidenceDecayFactor` defines the constant value used by this package.
	EvidenceDecayFactor = 0.85 // exponential decay per window
	// `EvidenceDecayWindow` defines the constant value used by this package.
	EvidenceDecayWindow = 20 // blocks per decay step
	// `MaxEvidencePerValidator` defines the constant value used by this package.
	MaxEvidencePerValidator = 50
)

const (
	// `TickExec` defines the constant value used by this package.
	TickExec uint64 = 1
	// `TickVote` defines the constant value used by this package.
	TickVote uint64 = 2
	// `TickFinalize` defines the constant value used by this package.
	TickFinalize uint64 = 3
	// `ConsensusTicksPerEpoch` defines the constant value used by this package.
	ConsensusTicksPerEpoch uint64 = 100
	// `ConsensusMaxTxPerBlock` defines the synchronization state protecting shared data.
	ConsensusMaxTxPerBlock = 250
	// `ConsensusMinFeeBPS` defines the constant value used by this package.
	ConsensusMinFeeBPS = 20
	// `ConsensusMaxFeeBPS` defines the constant value used by this package.
	ConsensusMaxFeeBPS = 300
	// `ConsensusFeeFloorAmount` defines the constant value used by this package.
	ConsensusFeeFloorAmount = 200
	// `ConsensusFeeCeilAmount` defines the constant value used by this package.
	ConsensusFeeCeilAmount = 100000
	// `ConsensusExecQuorumPercent` defines the constant value used by this package.
	ConsensusExecQuorumPercent = 60
	// `ConsensusEpochDuration` defines the constant value used by this package.
	ConsensusEpochDuration = 5 * time.Minute
	// `ConsensusValidatorMinStake` defines the constant value used by this package.
	ConsensusValidatorMinStake int64 = 100
)

type Misbehavior struct {
	// `Validator` stores whether the related condition is satisfied.
	Validator string
	// `Reason` stores the value associated with this record.
	Reason string
	// `Height` stores the value associated with this record.
	Height int
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Timestamp` stores the value associated with this record.
	Timestamp int64
}

type ForkScore struct {
	// `Block` stores the synchronization state protecting shared data.
	Block Block

	// `MempoolDistance` stores the value associated with this record.
	MempoolDistance int
	// `SignatureWeight` stores the value associated with this record.
	SignatureWeight int

	// `ExecutionScore` stores the value associated with this record.
	ExecutionScore int
	// `StateScore` stores the value associated with this record.
	StateScore int
	// `CensorshipScore` stores the value associated with this record.
	CensorshipScore int
	// `FreshnessScore` stores the value associated with this record.
	FreshnessScore int
	// `TotalScore` stores the measured quantity used by this operation.
	TotalScore int

	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Timestamp` stores the value associated with this record.
	Timestamp int64
}
type CensorshipEvidence struct {
	// `Height` stores the value associated with this record.
	Height uint64 // Changed from int to uint64
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Leader` stores the value associated with this record.
	Leader string
	// `TxID` stores the transaction data handled by this operation.
	TxID string
	// `TxFee` stores the transaction data handled by this operation.
	TxFee int
	// `MempoolRoot` stores the digest used to identify or verify the related data.
	MempoolRoot string
	// `Observer` stores the value associated with this record.
	Observer string
	// `ObserverSig` stores the value associated with this record.
	ObserverSig []byte
	// `ObservedAt` stores the value associated with this record.
	ObservedAt uint64 // Changed from int to uint64

	// `Fee` stores the value associated with this record.
	Fee int

	// `Timestamp` stores the value associated with this record.
	Timestamp int64
}

// `GlobalMempoolSnapshot` stores the value used by this operation.
var GlobalMempoolSnapshot []Transaction

// `MsgCensorshipProof` defines the constant value used by this package.
const MsgCensorshipProof = "censorship_proof"

// `CensorshipScore` stores the value used by this operation.
var CensorshipScore = map[string]int{}

type CensorshipProof struct {
	// --- Block context ---
	BlockHeight uint64 // Changed from int to uint64
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Proposer` stores the value associated with this record.
	Proposer string
	// `MempoolRoot` stores the digest used to identify or verify the related data.
	MempoolRoot string
	// --- Transaction evidence ---
	TxID string
	// `TxFee` stores the transaction data handled by this operation.
	TxFee int
	// `TxPayload` stores the transaction data handled by this operation.
	TxPayload []byte // canonical payload hash source
	// --- Reporter ---
	Reporter string
	// `Signature` stores the value associated with this record.
	Signature []byte
	// `Timestamp` stores the value associated with this record.
	Timestamp int64
}
type TxAbuseRecord struct {
	// `Attempts` stores the value associated with this record.
	Attempts int
	// `BannedTill` stores the value associated with this record.
	BannedTill time.Time
	// `Permanent` stores the value associated with this record.
	Permanent bool
}
type PeerHello struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `ConsensusHash` stores the digest used to identify or verify the related data.
	ConsensusHash string `json:"consensus_hash"`
	// `Role` stores the value associated with this record.
	Role string `json:"role,omitempty"`
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id,omitempty"`
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string `json:"validator_id"`
	// `ValidatorPubKey` stores whether the related condition is satisfied.
	ValidatorPubKey string `json:"validator_pubkey,omitempty"`
	// `P2PAddr` stores the address used by this operation.
	P2PAddr string `json:"p2p_addr"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetHeight` stores whether the related condition is satisfied.
	ValidatorSetHeight uint64 `json:"validator_set_height,omitempty"`
	// `NextValidatorSetHash` stores the digest used to identify or verify the related data.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// `ActivationHeight` stores the value associated with this record.
	ActivationHeight uint64 `json:"activation_height,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `TipHash` stores the digest used to identify or verify the related data.
	TipHash string `json:"tip_hash,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp,omitempty"`
	// `Nonce` stores the value associated with this record.
	Nonce string `json:"nonce,omitempty"`
	// `SignatureHex` stores the value associated with this record.
	SignatureHex string `json:"signature_hex,omitempty"`
}

type consensusParamsSnapshot struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `DynamicSelection` stores the value associated with this record.
	DynamicSelection bool `json:"dynamic_selection"`
	// `DeterministicSelection` stores the value associated with this record.
	DeterministicSelection bool `json:"deterministic_selection"`
	// `ActiveSetSize` stores the measured quantity used by this operation.
	ActiveSetSize int `json:"active_set_size"`
	// `ActiveSetMode` stores the value associated with this record.
	ActiveSetMode string `json:"active_set_mode"`
	// `MaxActiveCommittee` stores the value associated with this record.
	MaxActiveCommittee int `json:"max_active_committee"`
	// `MaxActiveValidators` stores the value associated with this record.
	MaxActiveValidators int `json:"max_active_validators"`
	// `PerformanceSlots` stores the value associated with this record.
	PerformanceSlots int `json:"performance_slots"`
	// `RotationSlots` stores the value associated with this record.
	RotationSlots int `json:"rotation_slots"`
	// `EffectiveStakeCap` stores the value associated with this record.
	EffectiveStakeCap int64 `json:"effective_stake_cap"`
	// `ValidatorEpochBlocks` stores whether the related condition is satisfied.
	ValidatorEpochBlocks uint64 `json:"validator_epoch_blocks"`
	// `ScoreWeightStake` stores the value associated with this record.
	ScoreWeightStake int `json:"score_weight_stake"`
	// `ScoreWeightUptime` stores the value associated with this record.
	ScoreWeightUptime int `json:"score_weight_uptime"`
	// `ScoreWeightPerformance` stores the value associated with this record.
	ScoreWeightPerformance int `json:"score_weight_performance"`
	// `ScoreWeightDecentralization` stores the value associated with this record.
	ScoreWeightDecentralization int `json:"score_weight_decentralization"`
	// `PerformanceMinSignedBPS` stores the value associated with this record.
	PerformanceMinSignedBPS int `json:"performance_min_signed_bps"`
	// `PromotionWindowEpochs` stores the value associated with this record.
	PromotionWindowEpochs uint64 `json:"promotion_window_epochs"`
	// `MinimumAgeForPerformanceSlotEpochs` stores the value associated with this record.
	MinimumAgeForPerformanceSlotEpochs uint64 `json:"minimum_age_for_performance_slot_epochs"`
	// `MinimumOnlineValidatorsWhenFull` stores the value associated with this record.
	MinimumOnlineValidatorsWhenFull int `json:"minimum_online_validators_when_full"`
	// `DiversityWeightASN` stores the value associated with this record.
	DiversityWeightASN int `json:"diversity_weight_asn"`
	// `DiversityWeightRegion` stores the value associated with this record.
	DiversityWeightRegion int `json:"diversity_weight_region"`
	// `DiversityWeightProvider` stores the value associated with this record.
	DiversityWeightProvider int `json:"diversity_weight_provider"`
	// `DiversityWeightHomePC` stores the value associated with this record.
	DiversityWeightHomePC int `json:"diversity_weight_home_pc"`
	// `ValidatorDiversityMetadataHash` stores whether the related condition is satisfied.
	ValidatorDiversityMetadataHash string `json:"validator_diversity_metadata_hash"`
	// `AdaptiveCommitteeLogMultiplier` stores the value associated with this record.
	AdaptiveCommitteeLogMultiplier int `json:"adaptive_committee_log_multiplier"`
	// `CommitteeRotationBlocks` stores the value associated with this record.
	CommitteeRotationBlocks uint64 `json:"committee_rotation_blocks"`
	// `SelectionActivityWindowBlocks` stores the value associated with this record.
	SelectionActivityWindowBlocks uint64 `json:"selection_activity_window_blocks"`
	// `SelectionMinSignedBlocks` stores the value associated with this record.
	SelectionMinSignedBlocks uint64 `json:"selection_min_signed_blocks"`
	// `OnboardingGraceBlocks` stores the value associated with this record.
	OnboardingGraceBlocks uint64 `json:"onboarding_grace_blocks"`
	// `OnboardingMaxNewSlots` stores the value associated with this record.
	OnboardingMaxNewSlots int `json:"onboarding_max_new_slots"`
	// `OnboardingStrictActivation` stores the value associated with this record.
	OnboardingStrictActivation bool `json:"onboarding_strict_activation"`
	// `OnboardingBootstrapLaneEnabled` stores whether the related condition is satisfied.
	OnboardingBootstrapLaneEnabled bool `json:"onboarding_bootstrap_lane_enabled"`
	// `OnboardingBootstrapMaxNewSlots` stores the value associated with this record.
	OnboardingBootstrapMaxNewSlots int `json:"onboarding_bootstrap_max_new_slots"`
	// `OnboardingBootstrapRequireStake` stores the value associated with this record.
	OnboardingBootstrapRequireStake bool `json:"onboarding_bootstrap_require_stake"`
	// `OnboardingBootstrapRequireNotJailed` stores the value associated with this record.
	OnboardingBootstrapRequireNotJailed bool `json:"onboarding_bootstrap_require_not_jailed"`
	// `ValidatorSetAutohealMode` stores whether the related condition is satisfied.
	ValidatorSetAutohealMode string `json:"validator_set_autoheal_mode"`
	// `ValidatorSetAutohealTrustedOnly` stores whether the related condition is satisfied.
	ValidatorSetAutohealTrustedOnly bool `json:"validator_set_autoheal_trusted_only_on_mismatch"`
	// `ValidatorSetAutohealNearTipForceAfter` stores whether the related condition is satisfied.
	ValidatorSetAutohealNearTipForceAfter int `json:"validator_set_autoheal_near_tip_force_after"`
	// `ValidatorSetAutohealPauseSeconds` stores whether the related condition is satisfied.
	ValidatorSetAutohealPauseSeconds uint64 `json:"validator_set_autoheal_pause_seconds"`
	// `HeartbeatScope` stores the value associated with this record.
	HeartbeatScope string `json:"heartbeat_scope"`
	// `MinStake` stores the value associated with this record.
	MinStake int64 `json:"min_stake"`
	// `StakeCapPct` stores the value associated with this record.
	StakeCapPct string `json:"stake_cap_pct"`
	// `UptimeWindow` stores the value associated with this record.
	UptimeWindow uint64 `json:"uptime_window"`
	// `LivenessMaxHeightDriftBlocks` stores the value associated with this record.
	LivenessMaxHeightDriftBlocks uint64 `json:"liveness_max_height_drift_blocks"`
	// `ReputationRecoveryThreshold` stores the value associated with this record.
	ReputationRecoveryThreshold string `json:"reputation_recovery_threshold"`
	// `LongUptimeThreshold` stores the value associated with this record.
	LongUptimeThreshold string `json:"long_uptime_threshold"`
	// `RequireStake` stores the request data being processed.
	RequireStake bool `json:"require_stake"`
	// `CoreStakeExempt` stores the value associated with this record.
	CoreStakeExempt bool `json:"core_stake_exempt"`
	// `CandidateObservationEpochs` stores the value associated with this record.
	CandidateObservationEpochs uint64 `json:"candidate_observation_epochs"`
	// `CandidateDCSMin` stores the value associated with this record.
	CandidateDCSMin string `json:"candidate_dcs_min"`
	// `CandidateUptimeMin` stores the value associated with this record.
	CandidateUptimeMin string `json:"candidate_uptime_min"`
	// `CandidateDiversityPctMin` stores the value associated with this record.
	CandidateDiversityPctMin string `json:"candidate_diversity_pct_min"`
	// `CandidateDiversityEpochs` stores the value associated with this record.
	CandidateDiversityEpochs uint64 `json:"candidate_diversity_epochs"`
	// `MaxPromotionsPerWindow` stores the value associated with this record.
	MaxPromotionsPerWindow int `json:"max_promotions_per_window"`
	// `PromotionWindowSize` stores the measured quantity used by this operation.
	PromotionWindowSize uint64 `json:"promotion_window_size"`
	// `CandidateSRPMin` stores the value associated with this record.
	CandidateSRPMin string `json:"candidate_srp_min"`
	// `CandidateSRPAlpha` stores the value associated with this record.
	CandidateSRPAlpha string `json:"candidate_srp_alpha"`
	// `CandidateWarningLimit` stores the value associated with this record.
	CandidateWarningLimit int `json:"candidate_warning_limit"`
	// `TestingRelaxedPromotion` stores the value associated with this record.
	TestingRelaxedPromotion bool `json:"testing_relaxed_promotion"`
	// `ValidatorSetActivationDelay` stores whether the related condition is satisfied.
	ValidatorSetActivationDelay uint64 `json:"validator_set_activation_delay"`
	// `ValidatorSetActivationModelV2Height` stores whether the related condition is satisfied.
	ValidatorSetActivationModelV2Height uint64 `json:"validator_set_activation_model_v2_height"`
	// `ValidatorSetCommitmentV2Height` stores whether the related condition is satisfied.
	ValidatorSetCommitmentV2Height uint64 `json:"validator_set_commitment_v2_height"`
	// `PromotionWindowRecordV1Height` stores the value associated with this record.
	PromotionWindowRecordV1Height uint64 `json:"promotion_window_record_v1_height"`
	// `ValidatorSetHashV3Height` stores whether the related condition is satisfied.
	ValidatorSetHashV3Height uint64 `json:"validator_set_hash_v3_height"`
	// `ValidatorInactiveBlocks` stores whether the related condition is satisfied.
	ValidatorInactiveBlocks uint64 `json:"validator_inactive_blocks"`
	// `ValidatorSetRotationWindow` stores whether the related condition is satisfied.
	ValidatorSetRotationWindow uint64 `json:"validator_set_rotation_window"`
	// `ValidatorMinActiveSet` stores whether the related condition is satisfied.
	ValidatorMinActiveSet int `json:"validator_min_active_set"`
	// `ValidatorInactivityPenaltyEnabled` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyEnabled bool `json:"validator_inactivity_penalty_enabled"`
	// `ValidatorInactivityPenaltyBurnBPS` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyBurnBPS uint64 `json:"validator_inactivity_penalty_burn_bps"`
	// `ValidatorInactivityPenaltyJail` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyJail uint64 `json:"validator_inactivity_penalty_jail_blocks"`
	// `ValidatorInactivityPenaltyCooldown` stores whether the related condition is satisfied.
	ValidatorInactivityPenaltyCooldown uint64 `json:"validator_inactivity_penalty_cooldown_blocks"`
	// `ValidatorInactivePermanent` stores whether the related condition is satisfied.
	ValidatorInactivePermanent bool `json:"validator_inactive_permanent"`
	// `PostBlockSafeModeEnabled` stores whether the related condition is satisfied.
	PostBlockSafeModeEnabled bool `json:"post_block_safe_mode_enabled"`
	// `PostBlockSafeModeMinMs` stores the value associated with this record.
	PostBlockSafeModeMinMs uint64 `json:"post_block_safe_mode_min_ms"`
	// `PostBlockSafeModeMaxMs` stores the value associated with this record.
	PostBlockSafeModeMaxMs uint64 `json:"post_block_safe_mode_max_ms"`
	// `PostBlockSafeModeHistoryBlocks` stores the value associated with this record.
	PostBlockSafeModeHistoryBlocks uint64 `json:"post_block_safe_mode_history_blocks"`
	// `PostBlockSafeModeLiveQuorumBPS` stores the value associated with this record.
	PostBlockSafeModeLiveQuorumBPS uint64 `json:"post_block_safe_mode_live_quorum_bps"`
	// `TransitionBarrierRetryMode` stores the value associated with this record.
	TransitionBarrierRetryMode string `json:"transition_barrier_retry_mode"`
	// `BannedValidators` stores the value associated with this record.
	BannedValidators []string `json:"banned_validators"`
}

// consensusParamsHash implements the consensus params hash helper.
func consensusParamsHash() string {
	// `snap` stores the value produced by this operation.
	snap := consensusParamsSnapshot{
		ChainID:                               protocolChainID(),
		GenesisHash:                           GenesisHash,
		Version:                               Version,
		DynamicSelection:                      DynamicValidatorSelectionEnabled,
		DeterministicSelection:                DeterministicValidatorSelection,
		ActiveSetSize:                         ValidatorActiveSetSize,
		ActiveSetMode:                         normalizeActiveSetMode(ValidatorActiveSetMode),
		MaxActiveCommittee:                    ValidatorMaxActiveCommittee,
		MaxActiveValidators:                   validatorHybridMaxActiveValidators(),
		PerformanceSlots:                      validatorHybridPerformanceSlots(),
		RotationSlots:                         validatorHybridRotationSlots(),
		EffectiveStakeCap:                     validatorHybridEffectiveStakeCap(),
		ValidatorEpochBlocks:                  validatorHybridEpochBlocks(),
		ScoreWeightStake:                      ValidatorHybridStakeWeight,
		ScoreWeightUptime:                     ValidatorHybridUptimeWeight,
		ScoreWeightPerformance:                ValidatorHybridPerformanceWeight,
		ScoreWeightDecentralization:           ValidatorHybridDecentralizationWeight,
		PerformanceMinSignedBPS:               validatorHybridPerformanceMinSignedBPS(),
		PromotionWindowEpochs:                 validatorHybridPromotionWindowEpochs(),
		MinimumAgeForPerformanceSlotEpochs:    validatorHybridMinimumPerformanceAgeEpochs(),
		MinimumOnlineValidatorsWhenFull:       ValidatorHybridMinimumOnlineWhenFull,
		DiversityWeightASN:                    ValidatorHybridDiversityASNWeight,
		DiversityWeightRegion:                 ValidatorHybridDiversityRegionWeight,
		DiversityWeightProvider:               ValidatorHybridDiversityProviderWeight,
		DiversityWeightHomePC:                 ValidatorHybridDiversityHomePCWeight,
		ValidatorDiversityMetadataHash:        ValidatorDiversityMetadataHash(),
		AdaptiveCommitteeLogMultiplier:        ValidatorAdaptiveCommitteeLogMult,
		CommitteeRotationBlocks:               committeeRotationBlocks(),
		SelectionActivityWindowBlocks:         validatorSelectionActivityWindowBlocks(),
		SelectionMinSignedBlocks:              validatorSelectionMinSignedBlocks(),
		OnboardingGraceBlocks:                 validatorOnboardingGraceBlocks(),
		OnboardingMaxNewSlots:                 validatorOnboardingMaxNewSlots(),
		OnboardingStrictActivation:            validatorOnboardingStrictActivationEnabled(),
		OnboardingBootstrapLaneEnabled:        validatorOnboardingBootstrapLaneEnabled(),
		OnboardingBootstrapMaxNewSlots:        validatorOnboardingBootstrapMaxNewSlots(),
		OnboardingBootstrapRequireStake:       validatorOnboardingBootstrapRequireStake(),
		OnboardingBootstrapRequireNotJailed:   validatorOnboardingBootstrapRequireNotJailed(),
		ValidatorSetAutohealMode:              normalizeValidatorSetAutohealMode(ValidatorSetAutohealMode),
		ValidatorSetAutohealTrustedOnly:       validatorSetAutohealTrustedOnlyOnMismatchEnabled(),
		ValidatorSetAutohealNearTipForceAfter: validatorSetAutohealNearTipForceAfter(),
		ValidatorSetAutohealPauseSeconds:      ValidatorSetAutohealPauseSeconds,
		HeartbeatScope:                        normalizeHeartbeatScope(ValidatorHeartbeatScope),
		MinStake:                              ValidatorMinStake,
		StakeCapPct:                           formatFloat(ValidatorStakeCapPct),
		UptimeWindow:                          ValidatorUptimeWindow,
		LivenessMaxHeightDriftBlocks:          validatorLivenessMaxHeightDriftBlocks(),
		ReputationRecoveryThreshold:           formatFloat(ValidatorReputationRecoveryThreshold),
		LongUptimeThreshold:                   formatFloat(ValidatorLongUptimeThreshold),
		RequireStake:                          ValidatorRequireStake,
		CoreStakeExempt:                       ValidatorCoreStakeExempt,
		CandidateObservationEpochs:            CandidateObservationEpochs,
		CandidateDCSMin:                       formatFloat(CandidateDCSMin),
		CandidateUptimeMin:                    formatFloat(CandidateUptimeMin),
		CandidateDiversityPctMin:              formatFloat(CandidateDiversityPctMin),
		CandidateDiversityEpochs:              CandidateDiversityEpochs,
		MaxPromotionsPerWindow:                MaxPromotionsPerWindow,
		PromotionWindowSize:                   PromotionWindowSize,
		CandidateSRPMin:                       formatFloat(CandidateSRPMin),
		CandidateSRPAlpha:                     formatFloat(CandidateSRPAlpha),
		CandidateWarningLimit:                 CandidateWarningLimit,
		TestingRelaxedPromotion:               TestingRelaxedPromotion,
		ValidatorSetActivationDelay:           ValidatorSetActivationDelay,
		ValidatorSetActivationModelV2Height:   ValidatorSetActivationModelV2Height,
		ValidatorSetCommitmentV2Height:        ValidatorSetCommitmentV2Height,
		PromotionWindowRecordV1Height:         PromotionWindowRecordV1Height,
		ValidatorSetHashV3Height:              ValidatorSetHashV3Height,
		ValidatorInactiveBlocks:               ValidatorInactiveBlocks,
		ValidatorSetRotationWindow:            ValidatorSetRotationWindow,
		ValidatorMinActiveSet:                 ValidatorMinActiveSet,
		ValidatorInactivityPenaltyEnabled:     ValidatorInactivityPenaltyEnabled,
		ValidatorInactivityPenaltyBurnBPS:     ValidatorInactivityPenaltyBurnBPS,
		ValidatorInactivityPenaltyJail:        ValidatorInactivityPenaltyJailBlocks,
		ValidatorInactivityPenaltyCooldown:    ValidatorInactivityPenaltyCooldownBlocks,
		ValidatorInactivePermanent:            ValidatorInactivePermanentRemove,
		PostBlockSafeModeEnabled:              ConsensusPostBlockSafeModeEnabled,
		PostBlockSafeModeMinMs:                uint64(ConsensusPostBlockSafeModeMin / time.Millisecond),
		PostBlockSafeModeMaxMs:                uint64(ConsensusPostBlockSafeModeMax / time.Millisecond),
		PostBlockSafeModeHistoryBlocks:        ConsensusPostBlockSafeModeHistoryBlocks,
		PostBlockSafeModeLiveQuorumBPS:        ConsensusPostBlockSafeModeLiveQuorumBPS,
		TransitionBarrierRetryMode:            normalizeTransitionBarrierRetryMode(TransitionBarrierRetryMode),
		BannedValidators:                      ValidatorBannedList,
	}
	// `raw` stores the value produced by this operation.
	raw, _ := json.Marshal(snap)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// formatFloat implements the format float helper.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 8, 64)
}

// normalizeValidatorID normalizes validator id.
func normalizeValidatorID(id string) string {
	return strings.ToUpper(strings.TrimSpace(id))
}

// setValidatorBannedValidators implements the set validator banned validators helper.
func setValidatorBannedValidators(list []string) {
	validatorBannedSet.mu.Lock()
	defer validatorBannedSet.mu.Unlock()

	validatorBannedSet.m = make(map[string]struct{})
	ValidatorBannedList = ValidatorBannedList[:0]

	// `raw` tracks the current values while iterating.
	for _, raw := range list {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(raw)
		if id == "" {
			continue
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := validatorBannedSet.m[id]; exists {
			continue
		}
		validatorBannedSet.m[id] = struct{}{}
		ValidatorBannedList = append(ValidatorBannedList, id)
	}

	sort.Strings(ValidatorBannedList)
}

// isValidatorBanned implements the is validator banned helper.
func isValidatorBanned(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	validatorBannedSet.mu.RLock()
	// `ok` stores whether the related condition is satisfied.
	_, ok := validatorBannedSet.m[id]
	validatorBannedSet.mu.RUnlock()
	return ok
}

// isProtocolValidatorBanned is the consensus ban authority. Validator bans
// must be committed in protocol state (for example as JAILED/EXITED registry
// status); a node-local config list cannot change committee membership.
func isProtocolValidatorBanned(id string) bool {
	_ = id
	return false
}

// `CensorshipCount` stores the measured quantity used by this operation.
var CensorshipCount = map[string]int{}

// `TxAbuse` stores the transaction data handled by this operation.
var TxAbuse = map[string]*TxAbuseRecord{}

// `FakeTxAttempts` stores the value used by this operation.
var FakeTxAttempts = map[string]int{}

// `TxBanUntil` stores the transaction data handled by this operation.
var TxBanUntil = map[string]time.Time{}

type TxExecutionProof struct {
	// `TxID` stores the transaction data handled by this operation.
	TxID string
	// `PreStateHash` stores the digest used to identify or verify the related data.
	PreStateHash string
	// `PostStateHash` stores the digest used to identify or verify the related data.
	PostStateHash string
}

// `consensusStarted` stores the value used by this operation.
var consensusStarted atomic.Bool

// `leader` stores the value used by this operation.
var leader string

const (
	// `MsgFinalBlock` defines the synchronization state protecting shared data.
	MsgFinalBlock = "final_block"

	// `MsgLeaderBlock` defines the synchronization state protecting shared data.
	MsgLeaderBlock = "leader_block"
	// `MsgPeerHello` defines the constant value used by this package.
	MsgPeerHello = "peer_hello"
	// `MsgCensorshipEvidence` defines the constant value used by this package.
	MsgCensorshipEvidence = "censorship_evidence"
	// `MsgValidatorAnnounce` defines the constant value used by this package.
	MsgValidatorAnnounce = "validator_announce"
	// `MsgValidatorSetUpdate` defines the constant value used by this package.
	MsgValidatorSetUpdate = "validator_set_update"
	// `MsgSnapshotOffer` defines the constant value used by this package.
	MsgSnapshotOffer = "snapshot_offer"
	// `MsgSnapshotProof` defines the constant value used by this package.
	MsgSnapshotProof = "snapshot_proof"
	// `MsgSnapshotMeta` defines the constant value used by this package.
	MsgSnapshotMeta = "snapshot_meta"
	// `MsgSnapshotChunk` defines the constant value used by this package.
	MsgSnapshotChunk = "snapshot_chunk"
	// `MsgTx` defines the transaction data handled by this operation.
	MsgTx = "tx"
	// `MsgBlock` defines the synchronization state protecting shared data.
	MsgBlock = "block"
	// `MsgGetBlocks` defines the constant value used by this package.
	MsgGetBlocks = "get_blocks"
	// `MsgBlocksBatch` defines the constant value used by this package.
	MsgBlocksBatch = "blocks_batch"
	// `MsgPeers` defines the constant value used by this package.
	MsgPeers = "peers"
	// `MsgPing` defines the constant value used by this package.
	MsgPing = "ping"
	// `MsgPong` defines the constant value used by this package.
	MsgPong = "pong"
	// `MsgExecutionResult` defines the result produced by this operation.
	MsgExecutionResult = "execution_result"
	// `MsgCommit` defines the constant value used by this package.
	MsgCommit = "commit"
	// `MsgBlockAck` defines the constant value used by this package.
	MsgBlockAck = "block_ack"
)

type BlockVote struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Validator` stores whether the related condition is satisfied.
	Validator string
	// `Signature` stores the value associated with this record.
	Signature []byte
}

type BlockAck struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
}

type SnapshotOffer struct {
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
}

type SnapshotProof struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64 `json:"checkpoint_height,omitempty"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash,omitempty"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string `json:"state_merkle_root,omitempty"`
	// `LedgerHash` stores the digest used to identify or verify the related data.
	LedgerHash string `json:"ledger_hash,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// ValidatorRegistry carries the committed validator authority needed to
	// verify V1 checkpoint proofs without consulting mutable local key caches.
	ValidatorRegistry map[string]ValidatorRecord `json:"validator_registry,omitempty"`
	// `CheckpointDomain` stores the value associated with this record.
	CheckpointDomain string `json:"checkpoint_domain,omitempty"`
	// `Validator` stores whether the related condition is satisfied.
	Validator string `json:"validator"`
	// `SignatureHex` stores the value associated with this record.
	SignatureHex string `json:"signature_hex"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp,omitempty"`
}

type SnapshotMetaGossip struct {
	// `From` stores the value associated with this record.
	From string `json:"from,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Meta` stores the value associated with this record.
	Meta SnapshotMetaResponse `json:"meta"`
	// `Manifest` stores the value associated with this record.
	Manifest *SnapshotManifest `json:"manifest,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp,omitempty"`
}

type SnapshotChunkGossip struct {
	// `From` stores the value associated with this record.
	From string `json:"from,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `ChunkSize` stores the measured quantity used by this operation.
	ChunkSize uint64 `json:"chunk_size"`
	// `ChunkCount` stores the measured quantity used by this operation.
	ChunkCount uint64 `json:"chunk_count"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp,omitempty"`
}

type SnapshotAnchorCache struct {
	// `CandidateKey` stores the key used to access the related value.
	CandidateKey string `json:"candidate_key,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64 `json:"checkpoint_height,omitempty"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root,omitempty"`
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string `json:"state_merkle_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `Votes` stores the value associated with this record.
	Votes int `json:"votes"`
	// `Validators` stores whether the related condition is satisfied.
	Validators []string `json:"validators,omitempty"`
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt time.Time `json:"updated_at"`
}

type ValidatorSetUpdate struct {
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Validators` stores whether the related condition is satisfied.
	Validators []string `json:"validators"`
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string `json:"hash"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
}

// `ActiveValidators` stores the value used by this operation.
var ActiveValidators = map[string]bool{}

// `ValidatorLastSeen` stores whether the related condition is satisfied.
var ValidatorLastSeen = map[string]time.Time{}
var (
	// `participationMu` stores the synchronization state protecting shared data.
	participationMu sync.RWMutex
	// `validatorsMu` stores whether the related condition is satisfied.
	validatorsMu sync.RWMutex
	// `blocksMu` stores the synchronization state protecting shared data.
	blocksMu sync.RWMutex
	// `noncesMu` stores the synchronization state protecting shared data.
	noncesMu sync.RWMutex
	// `proposedBlocksMu` stores the synchronization state protecting shared data.
	proposedBlocksMu sync.Mutex
	// `txAbuseMu` stores the synchronization state protecting shared data.
	txAbuseMu sync.Mutex // 🔒 ADD THIS
	// `TxAbuseMu` stores the synchronization state protecting shared data.
	TxAbuseMu sync.Mutex
	// `CensorshipMu` stores the synchronization state protecting shared data.
	CensorshipMu sync.Mutex
)

// `LastWorkBlockTime` stores the value used by this operation.
var LastWorkBlockTime int64 = 0

// `LastBlockTime` stores the value used by this operation.
var LastBlockTime int64 = 0

// `IsSynced` stores the current position in the related collection.
var IsSynced = false

// `LeaderCooldownBlocks` defines the constant value used by this package.
const LeaderCooldownBlocks = 100 // or 1000 (configurable)
type ValidatorAnnounce struct {
	// `NodeID` stores the value associated with this record.
	NodeID string
	// `PubKey` stores the key used to access the related value.
	PubKey string
	// `Height` stores the value associated with this record.
	Height int
	// `Signature` stores the value associated with this record.
	Signature string
}

type NodeDB struct {
	// `State` stores the value associated with this record.
	State *DB
	// `Blocks` stores the block data handled by this operation.
	Blocks *DB
	// `Snapshot` stores the value associated with this record.
	Snapshot *DB
	// `Tx` stores the transaction data handled by this operation.
	Tx *DB
	// `Meta` stores the value associated with this record.
	Meta *DB
}
type ParticipationScore struct {
	// `ValidBlocks` stores whether the related condition is satisfied.
	ValidBlocks int
	// `InvalidBlocks` stores the current position in the related collection.
	InvalidBlocks int
	// `LastSeen` stores the value associated with this record.
	LastSeen time.Time
	// `CooldownUntil` stores the value associated with this record.
	CooldownUntil uint64
	// `Reputation` stores the value associated with this record.
	Reputation int
}

// `Participation` stores the value used by this operation.
var Participation = make(map[string]*ParticipationScore)

// `PeerInteractions` stores the value used by this operation.
var PeerInteractions = map[string]map[string]int{}

// `ValidatorCooldown` stores whether the related condition is satisfied.
var ValidatorCooldown = map[string]int{}

// type PeerInfo struct {
// 	NodeID             string
// 	Addr               string // FIXED p2p addr: 127.0.0.1:700X
// 	LastSeen           time.Time
// 	LastConnectAttempt time.Time
// 	Alive              bool
// 	Failures           int
// 	Height             uint64 // Changed from int to uint64
// 	HelloSent          bool
// 	Connecting         bool     // 🔥 NEW
// 	Conn               net.Conn // SINGLE live connection

// }
type EncryptedValidatorKey struct {
	// `PublicKey` stores the key used to access the related value.
	PublicKey string `json:"publicKey"`
	// `Crypto` stores the value associated with this record.
	Crypto EncryptedKey `json:"crypto"`
}
type ValidatorKey struct {
	// `ID` stores the current position in the related collection.
	ID string // validator ID (node ID)
	// `PublicKey` stores the key used to access the related value.
	PublicKey ed25519.PublicKey
	// `PrivateKey` stores the key used to access the related value.
	PrivateKey ed25519.PrivateKey // NEVER exposed to users
}

// `ProtocolFrozen` stores the value used by this operation.
var ProtocolFrozen = true

// validatorPubKeysMu guards ValidatorPubKeys and GenesisValidatorPubKeys.
var validatorPubKeysMu sync.RWMutex

// `ValidatorPubKeys` stores whether the related condition is satisfied.
var ValidatorPubKeys = map[string]ed25519.PublicKey{}

// Immutable genesis validator keys used as a historical verification fallback.
var GenesisValidatorPubKeys = map[string]ed25519.PublicKey{}

// ExecPool is a global execution vote pool shared inside the process.
// Votes are block-scoped across round churn, not tied to only the current round.
var ExecPool = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.Mutex
	// `pool` stores the value associated with this record.
	pool map[uint64]map[string]map[string]ExecutionResult // epoch -> blockScopedExecKey -> signer -> result
	// `txMerkle` stores the transaction data handled by this operation.
	txMerkle map[uint64]map[string]string // epoch -> blockScopedExecKey -> txMerkle
	// `frozen` stores the value associated with this record.
	frozen map[uint64]map[string]string // epoch -> blockScopeKey -> execHash (frozen after quorum)
	// `signers` stores the value associated with this record.
	signers map[uint64]map[string]map[string]bool // epoch -> blockScopeKey -> signer -> seen
	// `choice` stores the value associated with this record.
	choice map[uint64]map[string]map[string]string // epoch -> blockScopeKey -> signer -> execHash:txMerkle
	// `epochChoice` stores the value associated with this record.
	epochChoice map[uint64]map[string]string // epoch -> signer -> blockScopeKey|execHash:txMerkle
	// `commitChoice` stores the value associated with this record.
	commitChoice map[uint64]map[string]string // epoch -> signer -> blockScopeKey
}{
	pool:         make(map[uint64]map[string]map[string]ExecutionResult),
	txMerkle:     make(map[uint64]map[string]string),
	frozen:       make(map[uint64]map[string]string),
	signers:      make(map[uint64]map[string]map[string]bool),
	choice:       make(map[uint64]map[string]map[string]string),
	epochChoice:  make(map[uint64]map[string]string),
	commitChoice: make(map[uint64]map[string]string),
}

// ================================
// MSC COIN PROTOCOL CONSTANTS
// ================================
const (
	// `CoinSymbol` defines the constant value used by this package.
	CoinSymbol = "MSC"
	// `CoinName` defines the constant value used by this package.
	CoinName = "Mythical system coin"
	// `CoinDecimals` defines the constant value used by this package.
	CoinDecimals = 18
	// `FixedTotalSupply` defines the constant value used by this package.
	FixedTotalSupply = int64(9193823602)
)

// `AltCoinSymbol` defines the constant value used by this package.
const AltCoinSymbol = "MSCX"

// AllowedCoins is retained for presentation/API-list compatibility only.
// Consensus, mempool, and request validation must use isProtocolCoinAllowed.
var AllowedCoins = map[string]bool{
	CoinSymbol:    true,
	AltCoinSymbol: true,
}

// ================================
// ADAPTIVE CONSENSUS STATE
// ================================
var TotalMintedMSC int64 = 0

// `CurrentTimeoutHours` stores the value used by this operation.
var CurrentTimeoutHours = 10 // initial fallback window
// `MaxTimeoutHours` defines the constant value used by this package.
const MaxTimeoutHours = 72 // safety cap
// `TimeoutIncreaseStep` defines the constant value used by this package.
const TimeoutIncreaseStep = 2 // +2 hours each fallback
// block classification

const (
	// `RewardUser` defines the constant value used by this package.
	RewardUser = 10
	// `RewardProposer` defines the constant value used by this package.
	RewardProposer = 40
	// `RewardValidators` defines the constant value used by this package.
	RewardValidators = 40
	// `RewardOwner` defines the constant value used by this package.
	RewardOwner = 10
)

const (
	// `FeeSplitUser` defines the constant value used by this package.
	FeeSplitUser = 0
	// `FeeSplitProposer` defines the constant value used by this package.
	FeeSplitProposer = 50
	// `FeeSplitValidators` defines the constant value used by this package.
	FeeSplitValidators = 50
	// `FeeSplitOwner` defines the constant value used by this package.
	FeeSplitOwner = 0
)

// RandomUserRewardEnabled controls local/status configuration only.
// Consensus reward minting must use protocolRandomUserRewardEnabled() so local
// tokenomics config cannot change replay state roots.
var RandomUserRewardEnabled = true

// RandomUserRewardChanceBPS is retained for local/status compatibility.
// Consensus reward minting must use protocolRandomUserRewardChanceBPS().
// Example: 2500 = 25% chance.
var RandomUserRewardChanceBPS = 2500

// Scheduled emission reward (independent from tx-fee-derived reward).
// Policy goals:
// - deterministic random trigger on eligible blocks
// - min/max reward bounds
// - yearly halving by block interval
// - fixed split: treasury / validators / burn
var EmissionRewardEnabled = true

// `EmissionMinReward` stores the value used by this operation.
var EmissionMinReward int64 = 2

// `EmissionMaxReward` stores the value used by this operation.
var EmissionMaxReward int64 = 4

// `EmissionJackpotChanceBPS` stores the value used by this operation.
var EmissionJackpotChanceBPS = 100

// `EmissionBaseChanceBPS` stores the value used by this operation.
var EmissionBaseChanceBPS = 10000

// `EmissionHighChanceAfterBlocks` stores the value used by this operation.
var EmissionHighChanceAfterBlocks uint64 = 1105840

// `EmissionHighChanceBPS` stores the value used by this operation.
var EmissionHighChanceBPS = 10000

// `EmissionHalvingIntervalBlocks` stores the value used by this operation.
var EmissionHalvingIntervalBlocks uint64 = 1105840

// `EmissionTreasuryBPS` stores the value used by this operation.
var EmissionTreasuryBPS = 2000

// `EmissionValidatorBPS` stores the value used by this operation.
var EmissionValidatorBPS = 7200

// `EmissionBurnBPS` stores the value used by this operation.
var EmissionBurnBPS = 800

// Work-block base reward (added on top of fee-derived reward).
// This helps ensure productive tx blocks always pay a deterministic minimum.
var WorkBlockRewardEnabled = true

// `WorkBlockBaseReward` stores the value used by this operation.
var WorkBlockBaseReward int64 = 2

// Burn stops permanently once effective supply reaches this floor.
// 0 means disabled unless configured via config.toml.
var BurnStopSupply int64 = 0

// `EmissionIntervalMode` stores the value used by this operation.
var EmissionIntervalMode = false

// `EmissionGapMinBlocks` stores the value used by this operation.
var EmissionGapMinBlocks uint64 = 4

// `EmissionGapMaxBlocks` stores the value used by this operation.
var EmissionGapMaxBlocks uint64 = 6

// `EmissionValidatorToProposer` stores the value used by this operation.
var EmissionValidatorToProposer = true

// UnifiedTeamRewardEnabled merges base block reward distribution for both
// work/task and time blocks into proposer/validators/treasury splits.
// Disabled by default to preserve legacy behavior.
var UnifiedTeamRewardEnabled = false

// `UnifiedTeamRewardTreasuryBPS` stores the value used by this operation.
var UnifiedTeamRewardTreasuryBPS = 2000

// `UnifiedTeamRewardProposerBPS` stores the value used by this operation.
var UnifiedTeamRewardProposerBPS = 3500

// `UnifiedTeamRewardValidatorBPS` stores the value used by this operation.
var UnifiedTeamRewardValidatorBPS = 4500

const (
	// `protocolRandomUserRewardEnabled` defines whether the related condition is satisfied.
	protocolRandomUserRewardEnabled = true
	// `protocolRandomUserRewardChanceBPS` defines the constant value used by this package.
	protocolRandomUserRewardChanceBPS = 2500

	// `protocolEmissionRewardEnabled` defines whether the related condition is satisfied.
	protocolEmissionRewardEnabled = true
	// `protocolEmissionMinReward` defines the constant value used by this package.
	protocolEmissionMinReward int64 = 2
	// `protocolEmissionMaxReward` defines the constant value used by this package.
	protocolEmissionMaxReward int64 = 4
	// `protocolEmissionJackpotChanceBPS` defines the constant value used by this package.
	protocolEmissionJackpotChanceBPS = 100
	// `protocolEmissionBaseChanceBPS` defines the constant value used by this package.
	protocolEmissionBaseChanceBPS = 10000
	// `protocolEmissionHighChanceAfterBlocks` defines the constant value used by this package.
	protocolEmissionHighChanceAfterBlocks = uint64(1105840)
	// `protocolEmissionHighChanceBPS` defines the constant value used by this package.
	protocolEmissionHighChanceBPS = 10000
	// `protocolEmissionHalvingIntervalBlocks` defines the constant value used by this package.
	protocolEmissionHalvingIntervalBlocks = uint64(1105840)
	// `protocolEmissionTreasuryBPS` defines the constant value used by this package.
	protocolEmissionTreasuryBPS = 2000
	// `protocolEmissionValidatorBPS` defines the constant value used by this package.
	protocolEmissionValidatorBPS = 7200
	// `protocolEmissionBurnBPS` defines the constant value used by this package.
	protocolEmissionBurnBPS = 800
	// `protocolEmissionIntervalMode` defines the constant value used by this package.
	protocolEmissionIntervalMode = false
	// `protocolEmissionGapMinBlocks` defines the constant value used by this package.
	protocolEmissionGapMinBlocks = uint64(4)
	// `protocolEmissionGapMaxBlocks` defines the constant value used by this package.
	protocolEmissionGapMaxBlocks = uint64(6)
	// `protocolEmissionValidatorToProposer` defines the constant value used by this package.
	protocolEmissionValidatorToProposer = true

	// `protocolWorkBlockRewardEnabled` defines whether the related condition is satisfied.
	protocolWorkBlockRewardEnabled = true
	// `protocolWorkBlockBaseReward` defines the constant value used by this package.
	protocolWorkBlockBaseReward int64 = 2
	// `protocolBurnStopSupply` defines the constant value used by this package.
	protocolBurnStopSupply int64 = 0

	// `protocolUnifiedTeamRewardEnabled` defines whether the related condition is satisfied.
	protocolUnifiedTeamRewardEnabled = false
	// `protocolUnifiedTeamRewardTreasuryBPS` defines the constant value used by this package.
	protocolUnifiedTeamRewardTreasuryBPS = 2000
	// `protocolUnifiedTeamRewardProposerBPS` defines the constant value used by this package.
	protocolUnifiedTeamRewardProposerBPS = 3500
	// `protocolUnifiedTeamRewardValidatorBPS` defines the constant value used by this package.
	protocolUnifiedTeamRewardValidatorBPS = 4500
)

// protocolRandomUserRewardEnabledFlag implements the protocol random user reward enabled flag helper.
func protocolRandomUserRewardEnabledFlag() bool {
	return protocolRandomUserRewardEnabled
}

// protocolRandomUserRewardChanceBPSValue implements the protocol random user reward chance bps value helper.
func protocolRandomUserRewardChanceBPSValue() int {
	return protocolRandomUserRewardChanceBPS
}

// protocolEmissionRewardEnabledFlag implements the protocol emission reward enabled flag helper.
func protocolEmissionRewardEnabledFlag() bool {
	return protocolEmissionRewardEnabled
}

// protocolEmissionRewardBounds implements the protocol emission reward bounds helper.
func protocolEmissionRewardBounds() (int64, int64) {
	return protocolEmissionMinReward, protocolEmissionMaxReward
}

// protocolEmissionJackpotChanceBPSValue implements the protocol emission jackpot chance bps value helper.
func protocolEmissionJackpotChanceBPSValue() int {
	return protocolEmissionJackpotChanceBPS
}

// protocolEmissionBaseChanceBPSValue implements the protocol emission base chance bps value helper.
func protocolEmissionBaseChanceBPSValue() int {
	return protocolEmissionBaseChanceBPS
}

// protocolEmissionHighChanceThreshold implements the protocol emission high chance threshold helper.
func protocolEmissionHighChanceThreshold() (uint64, int) {
	return protocolEmissionHighChanceAfterBlocks, protocolEmissionHighChanceBPS
}

// protocolEmissionHalvingIntervalBlocksValue implements the protocol emission halving interval blocks value helper.
func protocolEmissionHalvingIntervalBlocksValue() uint64 {
	return protocolEmissionHalvingIntervalBlocks
}

// protocolEmissionSplitBPS implements the protocol emission split bps helper.
func protocolEmissionSplitBPS() (int, int, int) {
	return protocolEmissionTreasuryBPS, protocolEmissionValidatorBPS, protocolEmissionBurnBPS
}

// protocolEmissionIntervalModeEnabled implements the protocol emission interval mode enabled helper.
func protocolEmissionIntervalModeEnabled() bool {
	return protocolEmissionIntervalMode
}

// protocolEmissionGapBounds implements the protocol emission gap bounds helper.
func protocolEmissionGapBounds() (uint64, uint64) {
	return protocolEmissionGapMinBlocks, protocolEmissionGapMaxBlocks
}

// protocolEmissionValidatorRewardToProposer implements the protocol emission validator reward to proposer helper.
func protocolEmissionValidatorRewardToProposer() bool {
	return protocolEmissionValidatorToProposer
}

// protocolWorkBlockRewardEnabledFlag implements the protocol work block reward enabled flag helper.
func protocolWorkBlockRewardEnabledFlag() bool {
	return protocolWorkBlockRewardEnabled
}

// protocolWorkBlockBaseRewardValue implements the protocol work block base reward value helper.
func protocolWorkBlockBaseRewardValue() int64 {
	return protocolWorkBlockBaseReward
}

// protocolBurnStopSupplyValue implements the protocol burn stop supply value helper.
func protocolBurnStopSupplyValue() int64 {
	return protocolBurnStopSupply
}

// protocolUnifiedTeamRewardEnabledFlag implements the protocol unified team reward enabled flag helper.
func protocolUnifiedTeamRewardEnabledFlag() bool {
	return protocolUnifiedTeamRewardEnabled
}

// protocolUnifiedTeamRewardSplitBPS implements the protocol unified team reward split bps helper.
func protocolUnifiedTeamRewardSplitBPS() (int, int, int) {
	return protocolUnifiedTeamRewardTreasuryBPS, protocolUnifiedTeamRewardProposerBPS, protocolUnifiedTeamRewardValidatorBPS
}

const (
	// `SlashDoubleProposal` defines the constant value used by this package.
	SlashDoubleProposal = 50
	// `SlashInvalidBlock` defines the synchronization state protecting shared data.
	SlashInvalidBlock = 30
	// `SlashFinalityBreak` defines the constant value used by this package.
	SlashFinalityBreak = 100
	// 1000 bps = 10% stake burn on slash events.
	SlashStakeBurnBPS = 1000
	// `SevereSlashExitAfter` defines the constant value used by this package.
	SevereSlashExitAfter = 3
)

// `ProposedBlocks` stores the value used by this operation.
var ProposedBlocks = map[uint64]map[uint32]map[string]string{} // height -> round -> proposer -> block hash
// `OwnerAddress` stores the address used by this operation.
var OwnerAddress = "MSC_OWNER_TREASURY_ADDRESS"

// `OWNER_ADDRESS` defines the address used by this operation.
const OWNER_ADDRESS = "MSC_OWNER_ACCOUNT"

// `TREASURY_ADDRESS` defines the address used by this operation.
const TREASURY_ADDRESS = "MSC_TREASURY"

// `FOUNDATION_ADDRESS` defines whether the related condition is satisfied.
const FOUNDATION_ADDRESS = "MSC_FOUNDATION"

// `VALIDATOR_BOOTSTRAP_POOL` defines whether the related condition is satisfied.
const VALIDATOR_BOOTSTRAP_POOL = "MSC_VALIDATOR_BOOTSTRAP"

// `COMMUNITY_POOL` defines the constant value used by this package.
const COMMUNITY_POOL = "MSC_COMMUNITY_POOL"

// `USER_REWARD_POOL` defines the constant value used by this package.
const USER_REWARD_POOL = "USER_REWARD_POOL"

// `MaxTxPerAddress` defines the address used by this operation.
const MaxTxPerAddress = 100

// `MaxMempoolSize` defines the measured quantity used by this operation.
const MaxMempoolSize = 5000

// `MaxTxPerAccountPerBlock` defines the synchronization state protecting shared data.
const MaxTxPerAccountPerBlock = 20

// `MaxTxTTLSeconds` defines the constant value used by this package.
const MaxTxTTLSeconds = 600

// `MaxTxRequestBodyBytes` defines the constant value used by this package.
const MaxTxRequestBodyBytes int64 = 64 * 1024

// `MaxTxGossipMessageBytes` defines the constant value used by this package.
const MaxTxGossipMessageBytes = 64 * 1024

// `MaxTxIDHexLen` defines the measured quantity used by this operation.
const MaxTxIDHexLen = 64

// `MaxTxPubKeyHexLen` defines the measured quantity used by this operation.
const MaxTxPubKeyHexLen = 64

// `MaxTxSignatureHexLen` defines the measured quantity used by this operation.
const MaxTxSignatureHexLen = 128

// `MaxTxAddressLen` defines the measured quantity used by this operation.
const MaxTxAddressLen = 128

// `MaxTxCoinLen` defines the measured quantity used by this operation.
const MaxTxCoinLen = 16

// `MaxTxChainIDLen` defines the measured quantity used by this operation.
const MaxTxChainIDLen = 64

// `MaxTxDTLTypeLen` defines the measured quantity used by this operation.
const MaxTxDTLTypeLen = 32

// `MaxTxDTLTokenIDLen` defines the measured quantity used by this operation.
const MaxTxDTLTokenIDLen = 128

// `MaxTxDTLPayloadLen` defines the measured quantity used by this operation.
const MaxTxDTLPayloadLen = 48 * 1024

// `MaxTxDTLGCertLen` defines the measured quantity used by this operation.
const MaxTxDTLGCertLen = 24 * 1024

// `TxGossipGlobalRatePerSecond` stores the transaction data handled by this operation.
var TxGossipGlobalRatePerSecond = 1000

// `TxGossipPeerRatePerSecond` stores the transaction data handled by this operation.
var TxGossipPeerRatePerSecond = 100

// `TxGossipPeerBurst` stores the transaction data handled by this operation.
var TxGossipPeerBurst = 50

// `TxGossipPeerLimiterTTL` stores the transaction data handled by this operation.
var TxGossipPeerLimiterTTL = 10 * time.Minute

// 23 months at ~3s/epoch => 19,872,000 epochs.
const DefaultStakeLockEpochs uint64 = 19872000

// `MinUnstakeMonths` defines the constant value used by this package.
const MinUnstakeMonths = 23

// `DaysPerMonth` defines the constant value used by this package.
const DaysPerMonth = 30

// `ActiveProposals` stores the value used by this operation.
var ActiveProposals []Proposal

// `ValidatorLastVote` stores whether the related condition is satisfied.
var ValidatorLastVote = map[string]uint64{} // Changed from int to uint64
type Message struct {
	// `Type` stores the value associated with this record.
	Type string `json:"type"` // "tx" | "block"
	// `Data` stores the value associated with this record.
	Data []byte `json:"data"`
}

var (
	// `FinalityVotes` stores the value used by this operation.
	FinalityVotes = map[uint64]map[string]bool{} // height -> validatorID -> voted // Changed from int to uint64
)

const (
	// `Version` defines the constant value used by this package.
	Version = "0.1.0"
)

// ChainID is retained for configuration/status compatibility. Protocol,
// consensus, DTL, signing, and address authority must use protocolChainID().
var ChainID = "91938"

type ValidatorCandidate struct {
	// `Address` stores the address used by this operation.
	Address string
	// `PubKey` stores the key used to access the related value.
	PubKey []byte
	// `Reputation` stores the value associated with this record.
	Reputation int64
}
type VRFProof struct {
	// `Output` stores the result produced by this operation.
	Output []byte
	// `Proof` stores the value associated with this record.
	Proof []byte
}
type BlockHeader struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string
	// `ReceiptsRoot` commits ordered execution receipts.
	ReceiptsRoot string
	// `EventRoot` commits ordered execution events.
	EventRoot string
	// `ExecutionHash` commits the raw post-execution state.
	ExecutionHash string
	// `FeeRoot` commits ordered fee effects.
	FeeRoot string
	// `DTLStateRoot` commits DTL-owned post-state independently.
	DTLStateRoot string
	// `DTLReceiptsRoot` commits DTL-only receipts independently.
	DTLReceiptsRoot string
	// `ProtocolVersion` selects deterministic execution rules.
	ProtocolVersion uint32
	// `FeatureBitmap` selects protocol features committed by the block.
	FeatureBitmap uint64
	// `DTLV2ActivationHeight` commits the exact V1 -> V2 transition height.
	DTLV2ActivationHeight uint64
	// `ValidatorSetVersion` identifies the independent registry view.
	ValidatorSetVersion uint64
	// `CommitteeHash` commits the ordered consensus committee.
	CommitteeHash string
	// `TxRoot` stores the transaction data handled by this operation.
	TxRoot string
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string
	// `NextValidatorSetHash` stores the digest used to identify or verify the related data.
	NextValidatorSetHash string
	// `NextValidatorSetRoot` stores the digest used to identify or verify the related data.
	NextValidatorSetRoot string
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64
	// `FinalizedStateRoot` stores the digest used to identify or verify the related data.
	FinalizedStateRoot string
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string
	// `RandomSeed` stores the value associated with this record.
	RandomSeed string
	// `Proposer` stores the value associated with this record.
	Proposer string
	// `Timestamp` stores the value associated with this record.
	Timestamp int64
}
type FinalityCert struct {
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Signatures` stores the result produced by this operation.
	Signatures map[string][]byte // validator → sig
}

type ValidatorSignature struct {
	// `Validator` stores whether the related condition is satisfied.
	Validator string `json:"validator"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature"`
}

type FinalizedEpochCertificate struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash"`
	// `PreviousEpochAnchorHash` stores the digest used to identify or verify the related data.
	PreviousEpochAnchorHash string `json:"previous_epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root"`
	// `ConsensusMode` stores the value associated with this record.
	ConsensusMode string `json:"consensus_mode,omitempty"`
	// `QuorumPolicyVersion` stores the value associated with this record.
	QuorumPolicyVersion string `json:"quorum_policy_version,omitempty"`
	// `ActiveReadyCount` stores the measured quantity used by this operation.
	ActiveReadyCount int `json:"active_ready_count,omitempty"`
	// `RequiredQuorum` stores the request data being processed.
	RequiredQuorum int `json:"required_quorum,omitempty"`
	// `StrictQuorum` stores the value associated with this record.
	StrictQuorum int `json:"strict_quorum,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `Signers` stores the value associated with this record.
	Signers []string `json:"signers,omitempty"`
	// `Signatures` stores the result produced by this operation.
	Signatures []ValidatorSignature `json:"signatures,omitempty"`
	// `ExecutionResultSignatures` stores the result produced by this operation.
	ExecutionResultSignatures map[string]string `json:"execution_result_signatures,omitempty"`
	// `CommitVoteProposalHash` stores the digest used to identify or verify the related data.
	CommitVoteProposalHash string `json:"commit_vote_proposal_hash,omitempty"`
	// ExecutionCommitmentHash is the separately signed hash of StateRoot,
	// ReceiptsRoot, EventRoot, ExecutionHash, FeeRoot and the dedicated DTL roots.
	ExecutionCommitmentHash string `json:"execution_commitment_hash,omitempty"`
	// `CommitVoteSignatures` stores the result produced by this operation.
	CommitVoteSignatures map[string]string `json:"commit_vote_signatures,omitempty"`
}
type Handshake struct {
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Address` stores the address used by this operation.
	Address string `json:"address"` // ✅ ADD
	// `PubKey` stores the key used to access the related value.
	PubKey string `json:"pubkey"` // ✅ ADD THIS
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"` // Changed from int to uint64
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height"` // Changed from int to uint64
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
	// `IsValidator` stores the current position in the related collection.
	IsValidator bool `json:"is_validator"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"sig"` // 🔐 ADD
	// `Peers` stores the value associated with this record.
	Peers []string `json:"peers"`
}

var (
	// `apiToken` stores the value used by this operation.
	apiToken = strings.TrimSpace(os.Getenv("MSC_RPC_TOKEN"))
	// `apiReadToken` stores the value used by this operation.
	apiReadToken = strings.TrimSpace(os.Getenv("MSC_RPC_READ_TOKEN"))
	// `apiSubmitToken` stores the value used by this operation.
	apiSubmitToken = strings.TrimSpace(os.Getenv("MSC_RPC_SUBMIT_TOKEN"))
	// `rateMu` stores the synchronization state protecting shared data.
	rateMu sync.Mutex
	// `rateMap` stores the value used by this operation.
	rateMap = make(map[string][]time.Time)
	// `faucetMu` stores the synchronization state protecting shared data.
	faucetMu sync.Mutex
	// `faucetLastByAddress` stores the address used by this operation.
	faucetLastByAddress = make(map[string]time.Time)
)

type Server struct {
	// `Node` stores the value associated with this record.
	Node         *Node
	bridgeMu     sync.RWMutex
	bridgeLoaded bool
	bridgeState  BridgeGatewayState
}

// AuthSession captures wallet-auth details for node startup gating and RPC access.
type AuthSession struct {
	// `SessionID` stores the value associated with this record.
	SessionID string
	// `NodeID` stores the value associated with this record.
	NodeID string
	// `ValidatorID` stores the consensus identity associated with this session.
	ValidatorID string
	// `ChainID` stores the value associated with this record.
	ChainID string
	// `Nonce` stores the value associated with this record.
	Nonce string
	// `Message` stores the value associated with this record.
	Message string
	// `CreatedAt` stores the value associated with this record.
	CreatedAt time.Time
	// `ExpiresAt` stores the value associated with this record.
	ExpiresAt time.Time
	// `WalletAddr` stores the address used by this operation.
	WalletAddr string
	// `WalletPubKey` stores the key used to access the related value.
	WalletPubKey string
	// `Token` stores the value associated with this record.
	Token string
	// `TokenExpires` stores the result produced by this operation.
	TokenExpires time.Time
	// `ValidatorOK` stores whether the related condition is satisfied.
	ValidatorOK bool
	// `ValidatorNote` stores whether the related condition is satisfied.
	ValidatorNote string
}

var (
	// `authMu` stores the synchronization state protecting shared data.
	authMu sync.Mutex
	// `authSessions` stores the value used by this operation.
	authSessions = make(map[string]*AuthSession)
	// `authTokens` stores the value used by this operation.
	authTokens = make(map[string]*AuthSession)
	// `authReady` stores the value used by this operation.
	authReady bool
	// `authNodeID` stores the value used by this operation.
	authNodeID string
	// `authWalletAddr` stores the address used by this operation.
	authWalletAddr string
	// `authWalletPub` stores the value used by this operation.
	authWalletPub string
)

// ProtocolConfig holds live blockchain rules
type ProtocolConfig struct {
	// `MaxPeers` stores the value associated with this record.
	MaxPeers int
	// `MinValidators` stores the value associated with this record.
	MinValidators int
	// `SlashMinor` stores the value associated with this record.
	SlashMinor int
	// `SlashMajor` stores the value associated with this record.
	SlashMajor int
	// `MaxTxPerBlock` stores the synchronization state protecting shared data.
	MaxTxPerBlock int
	// BlockTime is logical time per epoch (system time, not wall clock).
	BlockTime time.Duration
	// `RealTick` stores the value associated with this record.
	RealTick time.Duration
	// `TicksPerEpoch` stores the value associated with this record.
	TicksPerEpoch uint64
	// `MinExecutors` stores the value associated with this record.
	MinExecutors int // 🔥 REQUIRED FOR MODEL-3
	// `FeeBps` stores the value associated with this record.
	FeeBps int // legacy fixed fee in basis points (e.g. 20 = 0.2%)
	// Dynamic fee parameters
	MinFeeBps int // min fee (bps)
	// `MaxFeeBps` stores the value associated with this record.
	MaxFeeBps int // max fee (bps)
	// `FeeFloorAmount` stores the value associated with this record.
	FeeFloorAmount int // amount at which min fee applies
	// `FeeCeilAmount` stores the value associated with this record.
	FeeCeilAmount int // amount at which max fee applies
	// DTL fee model (base by tx-type + optional payload component).
	DTLCreateBaseFee int // TOKEN_CREATE base fee
	// `DTLTransferBaseFee` stores the value associated with this record.
	DTLTransferBaseFee int // TOKEN_TRANSFER base fee
	// `DTLMintBaseFee` stores the value associated with this record.
	DTLMintBaseFee int // TOKEN_MINT base fee
	// `DTLBurnBaseFee` stores the value associated with this record.
	DTLBurnBaseFee int // TOKEN_BURN base fee
	// `DTLPayloadFeePerKB` stores the value associated with this record.
	DTLPayloadFeePerKB int // additional fee per started 1KiB payload
	// `DTLFeeMaxMultiplier` stores the value associated with this record.
	DTLFeeMaxMultiplier int // max accepted fee multiplier vs required (anti-fat-finger)
	// Exec-hash quorum percent (e.g. 60)
	ExecQuorumPct int
	// Leader rotation slot (real-time seconds)
	LeaderSlotSeconds int64
}

// GlobalConfig is the active protocol config
var GlobalConfig = ProtocolConfig{
	BlockTime:           ConsensusEpochDuration,
	RealTick:            2 * time.Second,
	TicksPerEpoch:       ConsensusTicksPerEpoch,
	MaxPeers:            MaxPeers,
	MinValidators:       3,
	SlashMinor:          10,
	SlashMajor:          20,
	MaxTxPerBlock:       250,
	MinExecutors:        2, // production: 3–5
	FeeBps:              20,
	MinFeeBps:           ConsensusMinFeeBPS,
	MaxFeeBps:           ConsensusMaxFeeBPS,
	FeeFloorAmount:      ConsensusFeeFloorAmount,
	FeeCeilAmount:       ConsensusFeeCeilAmount,
	DTLCreateBaseFee:    DTLDefaultCreateBaseFee,
	DTLTransferBaseFee:  DTLDefaultTransferBaseFee,
	DTLMintBaseFee:      DTLDefaultMintBaseFee,
	DTLBurnBaseFee:      DTLDefaultBurnBaseFee,
	DTLPayloadFeePerKB:  DTLDefaultPayloadFeePerKB,
	DTLFeeMaxMultiplier: DTLDefaultFeeMaxMultiplier,
	ExecQuorumPct:       ConsensusExecQuorumPercent,
	LeaderSlotSeconds:   240,
}

// LeaderStallTimeout triggers fallback leader blocks when the expected leader stalls.
// Keep this above the network RTT to avoid unnecessary fallback.
var LeaderStallTimeout = 15 * time.Second

type Proposal struct {
	// `ID` stores the current position in the related collection.
	ID string
	// `Votes` stores the value associated with this record.
	Votes map[string]bool
	// `Applied` stores the value associated with this record.
	Applied bool
}
type Mempool struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.Mutex
	// `Transactions` stores the transaction data handled by this operation.
	Transactions []Transaction
	// `SeenTxIDs` stores the value associated with this record.
	SeenTxIDs map[string]bool
	// `txByID` stores the transaction data handled by this operation.
	txByID map[string]struct{}
	// `txBySenderNonce` stores the transaction data handled by this operation.
	txBySenderNonce map[string]string
	// `pendingCountBySender` stores the value associated with this record.
	pendingCountBySender map[string]int
	// `nextNonceBySender` stores the value associated with this record.
	nextNonceBySender map[string]int
}

// `GenesisHashExpected` stores the value used by this operation.
var GenesisHashExpected = "f6230e42861022d676ca43c57d0f3b6c0984c3cd17bfcd932059c353ab46821c"

// `GenesisHash` stores the digest used to identify or verify the related data.
var GenesisHash = GenesisHashExpected

// Permissionless validator admission (no-stake)
var CandidateObservationEpochs uint64 = 50

// `CandidateDCSMin` stores the value used by this operation.
var CandidateDCSMin float64 = 0.999

// `CandidateUptimeMin` stores the value used by this operation.
var CandidateUptimeMin float64 = 0.99

// `CandidateDiversityPctMin` stores the value used by this operation.
var CandidateDiversityPctMin float64 = 0.60

// `CandidateDiversityEpochs` stores the value used by this operation.
var CandidateDiversityEpochs uint64 = 50

// `MaxPromotionsPerWindow` stores the value used by this operation.
var MaxPromotionsPerWindow int = 1

// `PromotionWindowSize` stores the measured quantity used by this operation.
var PromotionWindowSize uint64 = 100

// `CandidateSRPMin` stores the value used by this operation.
var CandidateSRPMin float64 = 0.999

// `CandidateSRPAlpha` stores the value used by this operation.
var CandidateSRPAlpha float64 = 0.20

// After promotion we allow a small number of SRP warnings before scheduling removal.
var CandidateWarningLimit int = 3

// `TestingRelaxedPromotion` stores the value used by this operation.
var TestingRelaxedPromotion bool = false

// `CandidateBanFirst` defines the constant value used by this package.
const CandidateBanFirst = 370

// `CandidateBanSecond` defines the constant value used by this package.
const CandidateBanSecond = 1000

//	var GenesisValidators = []string{
//	    "12D3KooWM1yVV5Rovap6i9sepWuJbgz9VDDLnBdu2ioRfsXXri2Y", // A
//	    "12D3KooWS47891wJZbiWCRUFx5BGoMZNBCQmNBG94ogVC39fKWeY", // B
//	    "12D3KooWGg4V4nhsLvn4FXpCjzW62qUYLfex1pbRQSXdM5ovVGeQ", // C
//	}
type ValidatorAnnouncement struct {
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `PubKey` stores the key used to access the related value.
	PubKey string `json:"pubkey"`
	// `P2PAddr` stores the address used by this operation.
	P2PAddr string `json:"p2p_addr"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"` // legacy: reported height
	// `ReportedHeight` stores the value associated with this record.
	ReportedHeight uint64 `json:"reported_height"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height"`
	// `ExecEpoch` stores the value associated with this record.
	ExecEpoch uint64 `json:"exec_epoch"`
	// `ValidatorSetHeight` stores whether the related condition is satisfied.
	ValidatorSetHeight uint64 `json:"validator_set_height,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `ActivationHeight` stores the value associated with this record.
	ActivationHeight uint64 `json:"activation_height,omitempty"`
	// NextValidatorSetHash is diagnostic-only: peers can observe upcoming
	// deterministic transition commitment without treating it as authority.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// NextActivationHeight mirrors the target activation height for
	// NextValidatorSetHash (usually validator_set_height+1).
	NextActivationHeight uint64 `json:"next_activation_height,omitempty"`
	// ConsensusReady means this heartbeat sender is currently able to vote for
	// execution consensus, not only connected and height-live.
	ConsensusReady bool `json:"consensus_ready,omitempty"`
	// `ConsensusReadySet` stores the value associated with this record.
	ConsensusReadySet bool `json:"consensus_ready_set,omitempty"`
	// `IsValidator` stores the current position in the related collection.
	IsValidator bool `json:"is_validator"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature"`
}

type ValidatorRejoinState struct {
	// `Pending` stores the value associated with this record.
	Pending bool
	// `Heartbeats` stores the value associated with this record.
	Heartbeats uint16
	// `LastHeartbeat` stores the value associated with this record.
	LastHeartbeat time.Time
	// `LastSignedHeight` stores the value associated with this record.
	LastSignedHeight uint64
}

type ValidatorKeyHealth struct {
	// `Loaded` stores the value associated with this record.
	Loaded bool
	// `Fingerprint` stores the value associated with this record.
	Fingerprint string
	// `Expected` stores the value associated with this record.
	Expected string
	// `Match` stores the value associated with this record.
	Match bool
	// `IntegrityOK` stores whether the related condition is satisfied.
	IntegrityOK bool
	// `BackupPresent` stores the value associated with this record.
	BackupPresent bool
	// `BackupAgeSeconds` stores the value associated with this record.
	BackupAgeSeconds uint64
	// `Source` stores the value associated with this record.
	Source string
	// `Mode` stores the value associated with this record.
	Mode string
}

type CoreRegistryEntry struct {
	// `ID` stores the current position in the related collection.
	ID string `json:"id"`
	// `RequiredKeyFingerprint` stores the request data being processed.
	RequiredKeyFingerprint string `json:"required_key_fingerprint"`
	// `ConsensusPubKey` stores the key used to access the related value.
	ConsensusPubKey string `json:"consensus_pubkey"`
	// `P2PSeed` stores the value associated with this record.
	P2PSeed string `json:"p2p_seed"`
	// `Status` stores the value associated with this record.
	Status string `json:"status"`
}

type CoreRegistrySignature struct {
	// `SignerID` stores the value associated with this record.
	SignerID string `json:"signer_id"`
	// `SignerPubKey` stores the key used to access the related value.
	SignerPubKey string `json:"signer_pubkey"`
	// `SigHex` stores the value associated with this record.
	SigHex string `json:"sig_hex"`
}

type CoreRegistry struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Version` stores the value associated with this record.
	Version uint64 `json:"version"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `EffectiveHeight` stores the value associated with this record.
	EffectiveHeight uint64 `json:"effective_height"`
	// `PreviousRegistryHash` stores the digest used to identify or verify the related data.
	PreviousRegistryHash string `json:"previous_registry_hash"`
	// `Validators` stores whether the related condition is satisfied.
	Validators []CoreRegistryEntry `json:"validators"`
	// `Signatures` stores the result produced by this operation.
	Signatures []CoreRegistrySignature `json:"signatures"`
	// `PayloadHash` stores the digest used to identify or verify the related data.
	PayloadHash string `json:"payload_hash"`
}

type CoreRegistryState struct {
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string
	// `Epoch` stores the value associated with this record.
	Epoch uint64
	// `EffectiveHeight` stores the value associated with this record.
	EffectiveHeight uint64
	// `Verified` stores the value associated with this record.
	Verified bool
	// `ActiveCoreSet` stores the value associated with this record.
	ActiveCoreSet []string
	// `PendingCoreSet` stores the value associated with this record.
	PendingCoreSet []string
	// `LastReloadAt` stores the value associated with this record.
	LastReloadAt time.Time
	// `EnforcementMode` stores the value associated with this record.
	EnforcementMode string
}

type CoreActivationStatus struct {
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `Status` stores the value associated with this record.
	Status string `json:"status"`
	// `EligibleHeight` stores the value associated with this record.
	EligibleHeight uint64 `json:"eligible_height"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason"`
}

type ValidatorInfo struct {
	// `ID` stores the current position in the related collection.
	ID string
	// `Height` stores the value associated with this record.
	Height uint64
}

const (
	// `TopicBlock` defines the synchronization state protecting shared data.
	TopicBlock = "msc-block"
	// `TopicTx` defines the context controlling this operation.
	TopicTx = "msc-tx"
	// `TopicConsensus` defines the constant value used by this package.
	TopicConsensus = "msc-consensus"
	// `TopicValidator` defines the constant value used by this package.
	TopicValidator = "msc-validator"
	// `TopicSnapshotMeta` defines the constant value used by this package.
	TopicSnapshotMeta = "msc-snapshot-meta"
	// `TopicSnapshotChunk` defines the constant value used by this package.
	TopicSnapshotChunk = "msc-snapshot-chunk"
	// `TopicSnapshotProof` defines the constant value used by this package.
	TopicSnapshotProof = "msc-snapshot-proof"

	// `TopicBlocksLegacy` defines the constant value used by this package.
	TopicBlocksLegacy = "msc-blocks"
	// `TopicTransactionsLegacy` defines the constant value used by this package.
	TopicTransactionsLegacy = "msc-transactions"
	// `TopicValidatorsLegacy` defines the constant value used by this package.
	TopicValidatorsLegacy = "msc-validators"
)

// Backward-compatible alias for older references.
const ValidatorTopic = TopicValidator

// `PeerLastSeen` stores the value used by this operation.
var PeerLastSeen = map[string]time.Time{}

// `peerLastSeenMu` stores the synchronization state protecting shared data.
var peerLastSeenMu sync.Mutex

type GenesisFile struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Validators` stores whether the related condition is satisfied.
	Validators map[string]string `json:"validators"` // id -> pubkey hex
}

type Node struct {

	// Canonical validator set

	PeersLibp2p []peer.ID // 🔥 FIXED: Changed from PeersLib2p to PeersLibp2p
	// `CensorshipTopic` stores the value associated with this record.
	CensorshipTopic *pubsub.Topic
	// 🔥 NAYA LIBP2P FIELDS

	ProposalTopic *pubsub.Topic
	// `VoteTopic` stores the value associated with this record.
	VoteTopic *pubsub.Topic
	// `BlockTopic` stores the block data handled by this operation.
	BlockTopic *pubsub.Topic
	// `TxTopic` stores the transaction data handled by this operation.
	TxTopic *pubsub.Topic
	// `Libp2pHost` stores the value associated with this record.
	Libp2pHost host.Host
	// `PubSub` stores the value associated with this record.
	PubSub *pubsub.PubSub
	// `DHT` stores the value associated with this record.
	DHT *dht.IpfsDHT
	// `dhtDiscoveryRunning` prevents overlapping provider scans when periodic
	// discovery and self-heal request a refresh at the same time.
	dhtDiscoveryRunning atomic.Bool
	// `PeersLib2p` stores the value associated with this record.
	PeersLib2p []peer.ID // 🔥 ADD THIS LINE - keep track of libp2p peers
	// `BlockSubscription` stores the block data handled by this operation.
	BlockSubscription *pubsub.Subscription
	// `peerMu` stores the synchronization state protecting shared data.
	peerMu sync.RWMutex
	// `MisbehaviorLog` stores the value associated with this record.
	MisbehaviorLog map[string][]SlashEvidence
	// `misbehaviorMu` stores the synchronization state protecting shared data.
	misbehaviorMu sync.Mutex
	// `TxSubscription` stores the transaction data handled by this operation.
	TxSubscription *pubsub.Subscription
	// `ConsensusTopic` stores the value associated with this record.
	ConsensusTopic *pubsub.Topic
	// `ConsensusSub` stores the value associated with this record.
	ConsensusSub *pubsub.Subscription
	// `SnapshotMetaTopic` stores the value associated with this record.
	SnapshotMetaTopic *pubsub.Topic
	// `SnapshotMetaSub` stores the value associated with this record.
	SnapshotMetaSub *pubsub.Subscription
	// `SnapshotChunkTopic` stores the value associated with this record.
	SnapshotChunkTopic *pubsub.Topic
	// `SnapshotChunkSub` stores the value associated with this record.
	SnapshotChunkSub *pubsub.Subscription
	// `SnapshotProofTopic` stores the value associated with this record.
	SnapshotProofTopic *pubsub.Topic
	// `SnapshotProofSub` stores the value associated with this record.
	SnapshotProofSub *pubsub.Subscription
	// `TopicBlocks` stores the value associated with this record.
	TopicBlocks *pubsub.Topic
	// `Host` stores the value associated with this record.
	Host host.Host // Libp2p host
	// `streamManager` stores the value associated with this record.
	streamManager streamOpener

	// `TopicProposal` stores the value associated with this record.
	TopicProposal *pubsub.Topic
	// `TopicVote` stores the value associated with this record.
	TopicVote *pubsub.Topic
	// 🔥 BOOTSTRAP LIFECYCLE FLAG (AUTHORITATIVE)
	BootstrapDone bool
	// `shutdownCh` stores the value associated with this record.
	shutdownCh chan struct{}
	// `shutdownOnce` stores the value associated with this record.
	shutdownOnce sync.Once
	// `rootCtx` stores the context controlling this operation.
	rootCtx context.Context
	// `rootCancel` stores the digest used to identify or verify the related data.
	rootCancel context.CancelFunc
	// `ID` stores the current position in the related collection.
	ID string
	// `NetworkID` stores the node's network identity.
	NetworkID string
	// `ValidatorID` stores the consensus identity derived from the validator public key.
	ValidatorID string
	// `WalletAddress` stores the account identity derived from the node wallet key.
	WalletAddress string
	// `WalletPublicKey` stores the account public key for the node wallet.
	WalletPublicKey string
	// `Ledger` stores the value associated with this record.
	Ledger Ledger
	// `ExecutionLedger` stores the value associated with this record.
	ExecutionLedger Ledger
	// supplyAudit tracks explicit mint and burn transitions committed by this node.
	supplyAuditMu     sync.Mutex
	supplyAudit       SupplyAuditState
	supplyAuditLoaded bool
	// `Wallet` stores the value associated with this record.
	Wallet Wallet
	// `DataDir` stores the value associated with this record.
	DataDir string
	// `Peers` stores the value associated with this record.
	Peers []*Node
	// `Mempool` stores the value associated with this record.
	Mempool Mempool
	// `SeenTxIDs` stores the value associated with this record.
	SeenTxIDs map[string]bool
	// `seenTxMu` stores the synchronization state protecting shared data.
	seenTxMu sync.Mutex
	// `seenTxQueue` stores the value associated with this record.
	seenTxQueue []string
	// `seenTxHead` stores the value associated with this record.
	seenTxHead int
	// `SeenBlockHashes` stores the value associated with this record.
	SeenBlockHashes map[string]bool // 👈 REQUIRED
	// `seenBlockMu` stores the synchronization state protecting shared data.
	seenBlockMu sync.Mutex
	// `seenBlockQueue` stores the value associated with this record.
	seenBlockQueue []string
	// `seenBlockHead` stores the value associated with this record.
	seenBlockHead int
	// `ForkBlocks` stores the value associated with this record.
	ForkBlocks map[uint64][]Block // 👈 ADD THIS
	// `forkMu` stores the synchronization state protecting shared data.
	forkMu sync.RWMutex
	// `PeersTCP` stores the value associated with this record.
	PeersTCP []string // e.g. ["127.0.0.1:7001"]
	// `Blockchain` stores the block data handled by this operation.
	Blockchain *Blockchain
	// `DB` stores the value associated with this record.
	DB *NodeDB // 🔥 ADD THIS
	// `closeChan` stores the value associated with this record.
	closeChan chan struct{}
	// `wg` stores the value associated with this record.
	wg sync.WaitGroup
	// `threadInitOnce` stores the value associated with this record.
	threadInitOnce sync.Once
	// `immediateRoundStartMu` stores the synchronization state protecting shared data.
	immediateRoundStartMu sync.Mutex
	// `immediateRoundStartPendingHeight` stores the current position in the related collection.
	immediateRoundStartPendingHeight uint64
	// `immediateRoundStartStartedHeight` stores the current position in the related collection.
	immediateRoundStartStartedHeight uint64
	// `consensusSafetyPersistMu` stores the synchronization state protecting shared data.
	consensusSafetyPersistMu sync.Mutex
	// `consensusSafetyWritesSincePrune` stores the value associated with this record.
	consensusSafetyWritesSincePrune int
	// `consensusSafetyAsyncMu` stores the synchronization state protecting shared data.
	consensusSafetyAsyncMu sync.Mutex
	// `consensusSafetyAsyncRunning` stores the value associated with this record.
	consensusSafetyAsyncRunning bool
	// `consensusSafetyAsyncPending` stores the value associated with this record.
	consensusSafetyAsyncPending bool
	// `consensusSafetyAsyncReason` stores the value associated with this record.
	consensusSafetyAsyncReason string

	// `ConsensusThread` stores the value associated with this record.
	ConsensusThread *NodeTaskThread
	// `ExecutionThread` stores the value associated with this record.
	ExecutionThread *NodeTaskThread
	// `SyncThread` stores the value associated with this record.
	SyncThread *NodeTaskThread

	// `ProposalHistory` stores the value associated with this record.
	ProposalHistory map[uint64]string // height → proposer
	// `SelfAddr` stores the address used by this operation.
	SelfAddr string // 🔥 REQUIRED: this node’s real P2P address
	// `ValidatorKey` stores whether the related condition is satisfied.
	ValidatorKey ValidatorKey // consensus only

	// 🔥 VALIDATOR OWNERSHIP (FINAL)

	validatorMu sync.RWMutex
	// `PeerDiscovery` stores the value associated with this record.
	PeerDiscovery *PeerDiscovery // 🔥 NEW: Auto-discovery

	// `Consensus` stores the value associated with this record.
	Consensus *ConsensusState
	// `Config` stores the configuration used by this operation.
	Config *NodeConfig // 🔥 NEW: Configuration
	// `configMu` stores the configuration used by this operation.
	configMu sync.RWMutex

	// LibP2P

	// Validator gossip
	ValidatorTopic *pubsub.Topic
	// `ValidatorSub` stores whether the related condition is satisfied.
	ValidatorSub *pubsub.Subscription

	// `RWMutex` stores the synchronization state protecting shared data.
	sync.RWMutex

	// ================= LIBP2P =================

	// Runtime status
	validatorStatus map[string]*ValidatorStatus
	// `validatorOfflineSince` stores whether the related condition is satisfied.
	validatorOfflineSince map[string]time.Time
	// `validatorRejoin` stores whether the related condition is satisfied.
	validatorRejoin map[string]ValidatorRejoinState

	// Genesis-authoritative validator set (consensus snapshot baseline)
	GenesisValidators []string

	// Node role: validator | full | light
	Role string

	// Peer state for gossip quiet mode + flapping protection
	peerStateMu sync.Mutex
	// `peerSetHash` stores the digest used to identify or verify the related data.
	peerSetHash map[string]string // peerID -> validator set hash
	// `peerTipHash` stores the digest used to identify or verify the related data.
	peerTipHash map[string]string // peerID -> advertised chain tip hash
	// `peerHashMatch` stores the value associated with this record.
	peerHashMatch map[string]bool // peerID -> hash match
	// `peerHelloOK` stores whether the related condition is satisfied.
	peerHelloOK map[string]bool // peerID -> handshake complete
	// `nodeIDToPeer` stores the value associated with this record.
	nodeIDToPeer map[string]string // node ID -> peerID
	// `peerToValidator` stores the value associated with this record.
	peerToValidator map[string]string // peerID -> validator ID
	// `validatorToPeer` stores whether the related condition is satisfied.
	validatorToPeer map[string]string // validator ID -> peerID
	// `peerRole` stores the value associated with this record.
	peerRole map[string]string // peerID -> role (validator|full|light)
	// `peerAckHeight` stores the value associated with this record.
	peerAckHeight map[string]uint64 // peerID -> last acknowledged height
	// `peerDriftState` stores the value associated with this record.
	peerDriftState map[string]PeerDriftState
	// `peerSyncOnlyUntil` stores the value associated with this record.
	peerSyncOnlyUntil map[string]time.Time
	// `peerSyncOnlyClass` stores the value associated with this record.
	peerSyncOnlyClass map[string]string
	// `peerSyncOnlyLastDropLog` stores the value associated with this record.
	peerSyncOnlyLastDropLog map[string]time.Time
	// `noBlockLogMu` stores the synchronization state protecting shared data.
	noBlockLogMu sync.Mutex
	// `noBlockLogAt` stores the value associated with this record.
	noBlockLogAt map[string]time.Time // peerID:from:to:tip -> last log time
	// `peerSuspectAt` stores the value associated with this record.
	peerSuspectAt map[string]time.Time
	// `peerHelloSentAt` stores the value associated with this record.
	peerHelloSentAt map[string]time.Time
	// `peerConnectedAt` stores the value associated with this record.
	peerConnectedAt map[string]time.Time
	// `peerFlapTimes` stores the value associated with this record.
	peerFlapTimes map[string][]time.Time
	// `peerGraftAt` stores the value associated with this record.
	peerGraftAt map[string]time.Time
	// `quarantineUntil` stores the value associated with this record.
	quarantineUntil map[string]time.Time
	// `peerDialFailures` stores the result produced by this operation.
	peerDialFailures map[string]int
	// `peerDialNext` stores the value associated with this record.
	peerDialNext map[string]time.Time
	// `peerSubnet` stores the value associated with this record.
	peerSubnet map[string]string
	// `peerASN` stores the value associated with this record.
	peerASN map[string]string
	// `peerOutbound` stores the value associated with this record.
	peerOutbound map[string]bool
	// `peerHelloNonces` stores the value associated with this record.
	peerHelloNonces map[string]time.Time
	// `peerResourceWindows` stores the value associated with this record.
	peerResourceWindows map[string]PeerResourceWindow
	// `peerConnectWindows` stores the value associated with this record.
	peerConnectWindows map[string]PeerResourceWindow
	// `gossipQuiet` stores the value associated with this record.
	gossipQuiet bool

	// `connectedPeers` stores the value associated with this record.
	connectedPeers map[string]bool
	// `connectingPeers` stores the value associated with this record.
	connectingPeers map[string]bool
	// `validatorSuspect` stores whether the related condition is satisfied.
	validatorSuspect map[string]time.Time
	// `allowedPeerIDs` stores whether the related condition is satisfied.
	allowedPeerIDs map[string]bool
	// `dialSlots` stores the value associated with this record.
	dialSlots chan struct{}
	// `dialSlotsMu` stores the synchronization state protecting shared data.
	dialSlotsMu sync.Once

	// Validator set management (next-height activation)
	validatorSetMu sync.RWMutex
	// Deprecated cache-only field. Do not use as consensus authority; derive
	// validator sets from finalized chain/snapshot commitments.
	currentValidators []string
	// `pendingValidators` stores the value associated with this record.
	pendingValidators map[string]uint64 // validatorID -> activateEpoch
	// `pendingValidatorRemovals` stores the value associated with this record.
	pendingValidatorRemovals map[string]uint64 // validatorID -> deactivateEpoch
	// `epochValidators` stores the value associated with this record.
	epochValidators map[uint64][]string
	// `frozenValidatorsByHeight` stores the value associated with this record.
	frozenValidatorsByHeight map[uint64][]string
	// `frozenValidatorHashByHeight` stores the value associated with this record.
	frozenValidatorHashByHeight map[uint64]string
	// `committeeByHeight` stores the value associated with this record.
	committeeByHeight map[uint64][]string
	// `committeeHashByHeight` stores the value associated with this record.
	committeeHashByHeight map[uint64]string
	// `committeeLiveByHeight` stores the value associated with this record.
	committeeLiveByHeight map[uint64]map[string]bool
	// `safeModeGateActive` stores the value associated with this record.
	safeModeGateActive int32
	// `safeModeGateHeight` stores the value associated with this record.
	safeModeGateHeight uint64
	// `safeModeUntilByHeight` stores the value associated with this record.
	safeModeUntilByHeight map[uint64]time.Time
	// `safeModeWindowByHeight` stores the value associated with this record.
	safeModeWindowByHeight map[uint64]time.Duration
	// `safeModeObservedDelays` stores the value associated with this record.
	safeModeObservedDelays []time.Duration
	// `eligibleIndexVersion` stores the value associated with this record.
	eligibleIndexVersion uint64
	// `eligibleSortedValidators` stores the value associated with this record.
	eligibleSortedValidators []string
	// `queuedValidatorSetUpdates` stores the value associated with this record.
	queuedValidatorSetUpdates map[uint64]ValidatorSetUpdate
	// `validatorSetHeight` stores whether the related condition is satisfied.
	validatorSetHeight uint64
	// `candidateMu` stores the synchronization state protecting shared data.
	candidateMu sync.RWMutex
	// `candidates` stores the value associated with this record.
	candidates map[string]*CandidateStatus
	// `promotionWindowIdx` stores the current position in the related collection.
	promotionWindowIdx uint64
	// `promotionsInWindow` stores the value associated with this record.
	promotionsInWindow int
	// `validatorSetMismatchMu` stores whether the related condition is satisfied.
	validatorSetMismatchMu sync.Mutex
	// `validatorSetMismatchCnt` stores whether the related condition is satisfied.
	validatorSetMismatchCnt int
	// `validatorSetMismatchSince` stores whether the related condition is satisfied.
	validatorSetMismatchSince time.Time
	// `validatorSetMismatchHeight` stores whether the related condition is satisfied.
	validatorSetMismatchHeight uint64
	// `validatorSetMismatchExpected` stores whether the related condition is satisfied.
	validatorSetMismatchExpected string
	// `validatorSetMismatchGot` stores whether the related condition is satisfied.
	validatorSetMismatchGot string
	// `validatorSetRepairKey` stores whether the related condition is satisfied.
	validatorSetRepairKey string
	// `validatorSetRepairAt` stores whether the related condition is satisfied.
	validatorSetRepairAt time.Time
	// `validatorSetRepairWindow` stores whether the related condition is satisfied.
	validatorSetRepairWindow time.Time
	// `validatorSetRepairAttempts` stores whether the related condition is satisfied.
	validatorSetRepairAttempts int
	// `validatorSetRepairBackoffTil` stores whether the related condition is satisfied.
	validatorSetRepairBackoffTil time.Time
	// `validatorSetSyncOverrideKey` stores whether the related condition is satisfied.
	validatorSetSyncOverrideKey string
	// `validatorSetSyncOverrideAt` stores whether the related condition is satisfied.
	validatorSetSyncOverrideAt time.Time
	// `validatorAutohealState` stores whether the related condition is satisfied.
	validatorAutohealState string
	// `validatorAutohealLastReason` stores whether the related condition is satisfied.
	validatorAutohealLastReason string
	// `validatorAutohealLastSuccessHeight` stores whether the related condition is satisfied.
	validatorAutohealLastSuccessHeight uint64
	// `peerDriftTupleCount` stores the measured quantity used by this operation.
	peerDriftTupleCount map[string]uint64
	// `peerDriftTupleLastSeen` stores the value associated with this record.
	peerDriftTupleLastSeen map[string]time.Time
	// `peerDriftTupleLastLog` stores the value associated with this record.
	peerDriftTupleLastLog map[string]time.Time
	// `livenessReasonLogMu` stores the synchronization state protecting shared data.
	livenessReasonLogMu sync.Mutex
	// `livenessReasonLogLast` stores the value associated with this record.
	livenessReasonLogLast map[string]time.Time
	// `validatorStartupCheckOK` stores whether the related condition is satisfied.
	validatorStartupCheckOK bool
	// `validatorStartupCheckReason` stores whether the related condition is satisfied.
	validatorStartupCheckReason string
	// `validatorStartupCheckHeight` stores whether the related condition is satisfied.
	validatorStartupCheckHeight uint64
	// `validatorStartupCheckExpected` stores whether the related condition is satisfied.
	validatorStartupCheckExpected string
	// `validatorStartupCheckGot` stores whether the related condition is satisfied.
	validatorStartupCheckGot string
	// `validatorStartupCheckAt` stores whether the related condition is satisfied.
	validatorStartupCheckAt time.Time
	// `postCommitEffectsMu` stores the synchronization state protecting shared data.
	postCommitEffectsMu sync.Mutex
	// `executionSnapshotRebuildMu` stores the synchronization state protecting shared data.
	executionSnapshotRebuildMu sync.Mutex
	// `executionSnapshotRebuildReadyHeight` stores the value associated with this record.
	executionSnapshotRebuildReadyHeight uint64
	// `executionSnapshotLiveCommitHeight` stores the value associated with this record.
	executionSnapshotLiveCommitHeight uint64
	// `executionSnapshotRebuildLastErr` stores the error produced by this operation.
	executionSnapshotRebuildLastErr string
	// `executionSnapshotRebuildFailedHeight` stores the value associated with this record.
	executionSnapshotRebuildFailedHeight uint64
	// `executionSnapshotRebuildTarget` stores the value associated with this record.
	executionSnapshotRebuildTarget uint64
	// `executionSnapshotRebuildLastScheduleAt` stores the value associated with this record.
	executionSnapshotRebuildLastScheduleAt time.Time
	// `registryHistoryRebuildMu` stores the synchronization state protecting shared data.
	registryHistoryRebuildMu sync.Mutex
	// `registryHistoryRebuildTarget` stores the value associated with this record.
	registryHistoryRebuildTarget uint64
	// `registryHistoryRebuildReadyHeight` stores the value associated with this record.
	registryHistoryRebuildReadyHeight uint64
	// `registryHistoryRebuildLastErr` stores the error produced by this operation.
	registryHistoryRebuildLastErr string
	// `registryHistoryRebuildFailedHeight` stores the value associated with this record.
	registryHistoryRebuildFailedHeight uint64
	// `registryHistoryRebuildLastScheduledHeight` stores the value associated with this record.
	registryHistoryRebuildLastScheduledHeight uint64
	// `registryHistoryRebuildLastScheduleAt` stores the value associated with this record.
	registryHistoryRebuildLastScheduleAt time.Time
	// `registryCarryForwardRepairMu` stores the synchronization state protecting shared data.
	registryCarryForwardRepairMu sync.Mutex
	// `registryCarryForwardRepairCache` stores verified carry-forward registry repairs while catchup defers disk writes.
	registryCarryForwardRepairCache map[uint64]validatorRegistryCarryForwardRepairCacheEntry
	// `freezeJournalLastHeight` stores the value associated with this record.
	freezeJournalLastHeight uint64
	// `freezeJournalLastHash` stores the digest used to identify or verify the related data.
	freezeJournalLastHash string
	// `recomputePauseMu` stores the synchronization state protecting shared data.
	recomputePauseMu sync.Mutex
	// `recomputePauseUntil` stores the value associated with this record.
	recomputePauseUntil time.Time
	// `recomputePauseHeight` stores the value associated with this record.
	recomputePauseHeight uint64
	// `recomputePauseReason` stores the value associated with this record.
	recomputePauseReason string
	// `recomputePauseLastLog` stores the value associated with this record.
	recomputePauseLastLog time.Time
	// `recomputePauseApplied` stores the value associated with this record.
	recomputePauseApplied bool
	// `transitionBarrierPauseMu` stores the synchronization state protecting shared data.
	transitionBarrierPauseMu sync.Mutex
	// `transitionBarrierPauseLast` stores the value associated with this record.
	transitionBarrierPauseLast map[string]time.Time
	// `transitionBarrierRetryMu` stores the synchronization state protecting shared data.
	transitionBarrierRetryMu sync.Mutex
	// `transitionBarrierRetryStateByKey` stores the key used to access the related value.
	transitionBarrierRetryStateByKey map[transitionBarrierRetryKey]transitionBarrierRetryState
	// `transitionPlan` stores the value associated with this record.
	transitionPlan validatorSetTransitionPlan
	// `selfActivationPendingMu` stores the synchronization state protecting shared data.
	selfActivationPendingMu sync.Mutex
	// `selfActivationPendingSince` stores the value associated with this record.
	selfActivationPendingSince uint64
	// `selfActivationPendingStableSince` stores the value associated with this record.
	selfActivationPendingStableSince uint64
	// `selfActivationPendingStableHash` stores the digest used to identify or verify the related data.
	selfActivationPendingStableHash string
	// `selfActivationPendingLastWarnAt` stores the value associated with this record.
	selfActivationPendingLastWarnAt time.Time
	// `selfActivationPendingLastReconcile` stores the value associated with this record.
	selfActivationPendingLastReconcile uint64
	// `onboardingTrackerMu` stores the synchronization state protecting shared data.
	onboardingTrackerMu sync.RWMutex
	// `onboardingTracker` stores the value associated with this record.
	onboardingTracker map[string]ValidatorActivationTracker
	// `onboardingLogMu` stores the synchronization state protecting shared data.
	onboardingLogMu sync.Mutex
	// `onboardingLogLast` stores the value associated with this record.
	onboardingLogLast map[string]time.Time
	// `invalidProposerMu` stores the synchronization state protecting shared data.
	invalidProposerMu sync.Mutex
	// `invalidProposerSeen` stores the current position in the related collection.
	invalidProposerSeen map[uint64]map[string]int // height -> "expected->got" -> count
	// `invalidProposerEvidenceSeen` stores the current position in the related collection.
	invalidProposerEvidenceSeen map[string]time.Time // "height|round|expected|got|block_hash" -> first seen at
	// `invalidProposerStrikes` stores the current position in the related collection.
	invalidProposerStrikes map[string]ExecMismatchTracker
	// `invalidProposerPeerStrikes` stores the current position in the related collection.
	invalidProposerPeerStrikes map[string]ExecMismatchTracker
	// `doubleProposalMu` stores the synchronization state protecting shared data.
	doubleProposalMu sync.Mutex
	// `doubleProposalEvidenceSeen` stores the value associated with this record.
	doubleProposalEvidenceSeen map[string]time.Time // "height|round|proposer|prev_hash|got_hash" -> first seen at

	// `execResultsMu` stores the synchronization state protecting shared data.
	execResultsMu sync.Mutex
	// `execResults` stores the value associated with this record.
	execResults map[string]map[string]ExecutionResult // key -> executor -> result
	// `execBroadcasted` stores the value associated with this record.
	execBroadcasted map[uint64]map[string]bool // epoch -> exec_hash:tx_merkle broadcasted
	// `pendingBlocks` stores the value associated with this record.
	pendingBlocks map[string]Block
	// `queuedExecVotes` stores the value associated with this record.
	queuedExecVotes map[string][]ExecutionResultMsg
	// `acceptedProposal` stores the value associated with this record.
	acceptedProposal map[string]string
	// `acceptedProposalBlocks` stores the value associated with this record.
	acceptedProposalBlocks map[string]Block
	// `unsignedExecPoolHints` records unsigned exec-pool snapshots seen for a
	// proposal; these hints never count as votes, but they force signed commit
	// quorum before local finality to prevent stale/poisoned hint races.
	unsignedExecPoolHints map[uint64]map[string]bool
	// `embeddedProposalSeen` stores the value associated with this record.
	embeddedProposalSeen map[uint64]map[string]struct{}
	// `quorumLockedProposal` stores the value associated with this record.
	quorumLockedProposal map[string]string
	// `execVoteGuardMu` stores the synchronization state protecting shared data.
	execVoteGuardMu sync.Mutex
	// `execVoteIngressSeen` stores the value associated with this record.
	execVoteIngressSeen map[string]time.Time
	// `execVoteStaleIngressSeen` stores the value associated with this record.
	execVoteStaleIngressSeen map[string]time.Time
	// `execVoteSeen` stores the value associated with this record.
	execVoteSeen map[string]time.Time
	// `execVoteLimiter` stores the value associated with this record.
	execVoteLimiter map[string]*rate.Limiter
	// `execMismatch` stores the value associated with this record.
	execMismatch map[string]ExecMismatchTracker
	// `localExecMismatchMu` stores the synchronization state protecting shared data.
	localExecMismatchMu sync.Mutex
	// `localExecMismatchCount` stores the measured quantity used by this operation.
	localExecMismatchCount int
	// `localExecMismatchHeight` stores the value associated with this record.
	localExecMismatchHeight uint64
	// `localExecMismatchBlockHash` stores the digest used to identify or verify the related data.
	localExecMismatchBlockHash string
	// `leaderMu` stores the synchronization state protecting shared data.
	leaderMu sync.Mutex
	// `leaderBlocks` stores the value associated with this record.
	leaderBlocks map[uint64]Block
	// `queuedFutureLeaderBlocks` stores the value associated with this record.
	queuedFutureLeaderBlocks map[uint64][]Block
	// `lastProposedRoundByHeight` stores the value associated with this record.
	lastProposedRoundByHeight map[uint64]uint32
	// `lastProposedRoundAtByHeight` stores the value associated with this record.
	lastProposedRoundAtByHeight map[uint64]time.Time
	// `lastLeaderSlot` stores the value associated with this record.
	lastLeaderSlot int64
	// `lastLeaderEpoch` stores the value associated with this record.
	lastLeaderEpoch uint64
	// `lastLeaderRound` stores the value associated with this record.
	lastLeaderRound uint32

	// `commitMu` stores the synchronization state protecting shared data.
	commitMu sync.Mutex
	// `receiveMu` serializes block verification and commit entry.
	receiveMu sync.Mutex
	// `applyMu` stores the synchronization state protecting shared data.
	applyMu sync.Mutex
	// `committed` stores the value associated with this record.
	committed map[uint64]string // height -> hash
	// `committedHeight` stores the value associated with this record.
	committedHeight uint64 // monotonic finalized height (idempotent barrier)
	// `lastCommitHeight` stores the value associated with this record.
	lastCommitHeight uint64 // last committed height observed locally
	// `lastCommitAt` stores the value associated with this record.
	lastCommitAt time.Time // wall-clock time of last committed-height progress
	// `finalizedHeight` stores the value associated with this record.
	finalizedHeight uint64 // supermajority-finalized height (can lag committed)
	// `commitVotes` stores the value associated with this record.
	commitVotes map[uint64]map[string]map[string]struct{} // height -> proposal hash -> voter
	// `commitVoted` stores the value associated with this record.
	commitVoted map[uint64]map[string]string // height -> voter -> proposal hash (one commit vote per height)
	// `commitVoteSignatures` stores the result produced by this operation.
	commitVoteSignatures map[uint64]map[string]map[string]string // height -> proposal hash -> voter -> signature
	// `commitInFlight` stores the value associated with this record.
	commitInFlight map[uint64]string // height -> hash currently being applied

	// `heartbeatMu` stores the synchronization state protecting shared data.
	heartbeatMu sync.Mutex
	// `lastHeartbeatReported` stores the value associated with this record.
	lastHeartbeatReported uint64
	// `lastHeartbeatFinalized` stores the value associated with this record.
	lastHeartbeatFinalized uint64
	// `lastHeartbeatEpoch` stores the value associated with this record.
	lastHeartbeatEpoch uint64
	// `lastHeartbeatAt` stores the value associated with this record.
	lastHeartbeatAt time.Time
	// `lastHeartbeatGateLogAt` stores the value associated with this record.
	lastHeartbeatGateLogAt time.Time
	// `heartbeatSignalCh` stores the value associated with this record.
	heartbeatSignalCh chan struct{}
	// `heartbeatForcePending` stores the value associated with this record.
	heartbeatForcePending bool
	// `heartbeatLoopActive` stores the value associated with this record.
	heartbeatLoopActive bool

	// `startupRecoveryApplied` stores the value associated with this record.
	startupRecoveryApplied bool
	// `validatorKeyFingerprint` stores whether the related condition is satisfied.
	validatorKeyFingerprint string
	// `lastValidatorKeyHealth` stores the value associated with this record.
	lastValidatorKeyHealth ValidatorKeyHealth
	// `lastKeyAuditAt` stores the value associated with this record.
	lastKeyAuditAt time.Time
	// `coreRegistryMu` stores the synchronization state protecting shared data.
	coreRegistryMu sync.RWMutex
	// `coreRegistryState` stores the value associated with this record.
	coreRegistryState CoreRegistryState
	// `coreRegistryEntries` stores the value associated with this record.
	coreRegistryEntries map[string]CoreRegistryEntry
	// `coreActivationStatus` stores the value associated with this record.
	coreActivationStatus CoreActivationStatus

	// `execSignerSeen` stores the value associated with this record.
	execSignerSeen map[uint64]map[string]map[string]bool // epoch -> blockScopeKey -> signer -> seen
	// `execBroadcastedByValidator` stores the value associated with this record.
	execBroadcastedByValidator map[uint64]map[string]map[string]bool // epoch -> blockScopeKey -> signer -> broadcasted
	// `localExecVoteByRound` stores the value associated with this record.
	localExecVoteByRound map[uint64]map[uint32]string // epoch -> round -> proposalKey
	// `execRebroadcastMu` stores the synchronization state protecting shared data.
	execRebroadcastMu sync.Mutex
	// `execRebroadcastAt` stores the value associated with this record.
	execRebroadcastAt map[uint64]time.Time // epoch -> last execution-vote broadcast activity
	// `execRebroadcastState` stores the value associated with this record.
	execRebroadcastState map[uint64]execVoteRebroadcastState

	// `syncMu` stores the synchronization state protecting shared data.
	syncMu sync.Mutex
	// `lastSyncAttempt` stores the value associated with this record.
	lastSyncAttempt time.Time
	// `lastSyncQueueLogAt` stores the value associated with this record.
	lastSyncQueueLogAt time.Time
	// `lastSyncQueueLogTarget` stores the value associated with this record.
	lastSyncQueueLogTarget uint64
	// `lastQueueForceSyncAt` stores the value associated with this record.
	lastQueueForceSyncAt time.Time
	// `lastQueueForceSyncTarget` stores the value associated with this record.
	lastQueueForceSyncTarget uint64
	// `lastMissingBlockRequestAt` stores the value associated with this record.
	lastMissingBlockRequestAt time.Time
	// `lastMissingBlockHeight` stores the value associated with this record.
	lastMissingBlockHeight uint64
	// `missingBlockRecoveryInFlight` stores the value associated with this record.
	missingBlockRecoveryInFlight bool
	// `syncMode` stores the value associated with this record.
	syncMode string
	// `syncStage` stores the value associated with this record.
	syncStage string
	// `syncPipelineStage` stores the value associated with this record.
	syncPipelineStage string
	// `syncLagBlocks` stores the value associated with this record.
	syncLagBlocks uint64
	// `syncAction` stores the value associated with this record.
	syncAction string
	// `syncActionAt` stores the value associated with this record.
	syncActionAt time.Time
	// `syncPipelineStageAt` stores the value associated with this record.
	syncPipelineStageAt time.Time
	// `syncProvider` stores the value associated with this record.
	syncProvider string
	// `syncSnapshotHeight` stores the value associated with this record.
	syncSnapshotHeight uint64
	// `syncSnapshotHash` stores the digest used to identify or verify the related data.
	syncSnapshotHash string
	// `syncDownloadedChunks` stores the value associated with this record.
	syncDownloadedChunks uint64
	// `syncTotalChunks` stores the value associated with this record.
	syncTotalChunks uint64
	// `syncChunkLastDownloaded` stores the value associated with this record.
	syncChunkLastDownloaded uint64
	// `syncChunkLastProgressAt` stores the value associated with this record.
	syncChunkLastProgressAt time.Time
	// `syncChunkProviders` stores the value associated with this record.
	syncChunkProviders []string
	// `syncVerifyStage` stores the value associated with this record.
	syncVerifyStage string
	// `syncResumeState` stores the value associated with this record.
	syncResumeState string
	// `syncLastProgressAt` stores the value associated with this record.
	syncLastProgressAt time.Time
	// `syncLastObservedHeight` stores the value associated with this record.
	syncLastObservedHeight uint64
	// `syncStallSeconds` stores the value associated with this record.
	syncStallSeconds uint64
	// `syncStrategy` stores the value associated with this record.
	syncStrategy string
	// `syncResumeTarget` stores the value associated with this record.
	syncResumeTarget uint64
	// `syncResumePending` stores the value associated with this record.
	syncResumePending bool
	// `syncProgressRate` stores the value associated with this record.
	syncProgressRate float64
	// `syncProgressSampleHeight` stores the value associated with this record.
	syncProgressSampleHeight uint64
	// `syncProgressSampleAt` stores the value associated with this record.
	syncProgressSampleAt time.Time
	// `syncWarmupJoinHeight` stores the value associated with this record.
	syncWarmupJoinHeight uint64
	// `syncAvoidProvider` stores the value associated with this record.
	syncAvoidProvider string
	// `syncAvoidProviderOnce` stores the value associated with this record.
	syncAvoidProviderOnce bool
	// `syncApplyFailureHeight` stores the value associated with this record.
	syncApplyFailureHeight uint64
	// `syncApplyFailureLocalHeight` stores the value associated with this record.
	syncApplyFailureLocalHeight uint64
	// `syncApplyFailureBlockHash` stores the digest used to identify or verify the related data.
	syncApplyFailureBlockHash string
	// `syncApplyFailurePrevHash` stores the digest used to identify or verify the related data.
	syncApplyFailurePrevHash string
	// `syncApplyFailureReason` stores the value associated with this record.
	syncApplyFailureReason string
	// `syncApplyFailureProposer` stores the value associated with this record.
	syncApplyFailureProposer string
	// `syncApplyFailureCount` stores the measured quantity used by this operation.
	syncApplyFailureCount uint64
	// `syncApplyFailureAt` stores the value associated with this record.
	syncApplyFailureAt time.Time
	// `syncRangeUnavailableStreak` stores the value associated with this record.
	syncRangeUnavailableStreak uint64
	// `syncSnapshotSessionFailures` stores the result produced by this operation.
	syncSnapshotSessionFailures uint64
	// `syncSnapshotSessionLastFailAt` stores the value associated with this record.
	syncSnapshotSessionLastFailAt time.Time
	// `syncSnapshotSessionDegradedUntil` stores the value associated with this record.
	syncSnapshotSessionDegradedUntil time.Time
	// `syncPeerScoreMu` stores the synchronization state protecting shared data.
	syncPeerScoreMu sync.Mutex
	// `syncPeerScores` stores the result produced by this operation.
	syncPeerScores map[string]*SyncPeerScore
	// `syncBackfillMu` stores the synchronization state protecting shared data.
	syncBackfillMu sync.Mutex
	// `syncBackfillWatermark` stores the value associated with this record.
	syncBackfillWatermark uint64
	// `syncApplyWorkerOnce` stores the value associated with this record.
	syncApplyWorkerOnce sync.Once
	// `syncBlockQueue` stores the value associated with this record.
	syncBlockQueue chan syncApplyTask
	// `syncWarmupStartAt` stores the value associated with this record.
	syncWarmupStartAt time.Time
	// `syncWarmupLastHeight` stores the value associated with this record.
	syncWarmupLastHeight uint64
	// `syncWarmupLastHeightAt` stores the value associated with this record.
	syncWarmupLastHeightAt time.Time
	// `syncWarmupQuorumHash` stores the digest used to identify or verify the related data.
	syncWarmupQuorumHash string
	// `syncWarmupQuorumVotes` stores the value associated with this record.
	syncWarmupQuorumVotes int
	// `syncWarmupQuorumSince` stores the value associated with this record.
	syncWarmupQuorumSince time.Time
	// `syncWarmupEligible` stores the value associated with this record.
	syncWarmupEligible bool
	// `snapshotSessionMu` stores the synchronization state protecting shared data.
	snapshotSessionMu sync.Mutex
	// `snapshotSession` stores the value associated with this record.
	snapshotSession SnapshotSession
	// `lateJoinAuthorityMu` stores the synchronization state protecting shared data.
	lateJoinAuthorityMu sync.Mutex
	// `lateJoinAuthority` stores the value associated with this record.
	lateJoinAuthority LateJoinAuthorityState
	// `autoHealMu` stores the synchronization state protecting auto-heal manager access.
	autoHealMu sync.RWMutex
	// `autoHealManager` stores the node-level auto-heal manager.
	autoHealManager *AutoHealManager

	// `logicalMu` stores the synchronization state protecting shared data.
	logicalMu sync.Mutex
	// `logicalClock` stores the synchronization state protecting shared data.
	logicalClock LogicalClock

	// `heightReportMu` stores the synchronization state protecting shared data.
	heightReportMu sync.Mutex
	// `heightReports` stores the value associated with this record.
	heightReports map[uint64]map[string]time.Time // height -> validator -> last report time
	// `validatorReportHeight` stores whether the related condition is satisfied.
	validatorReportHeight map[string]uint64 // validator -> last reported height

	// `snapshotOfferMu` stores the synchronization state protecting shared data.
	snapshotOfferMu sync.Mutex
	// `snapshotOfferSent` stores the value associated with this record.
	snapshotOfferSent map[string]uint64 // validator -> last offered height
	// `snapshotOfferSentAt` stores the value associated with this record.
	snapshotOfferSentAt map[string]time.Time // validator -> last offer wall time

	// `snapshotExecutionLedgerMu` stores the synchronization state protecting shared data.
	snapshotExecutionLedgerMu sync.Mutex
	// `snapshotExecutionLedgerByHeight` stores the value associated with this record.
	snapshotExecutionLedgerByHeight map[uint64]Ledger
	// `postCommitLedgerMu` stores the synchronization state protecting shared data.
	postCommitLedgerMu sync.Mutex
	// `postCommitLedgerByHeight` stores the value associated with this record.
	postCommitLedgerByHeight map[uint64]Ledger
	// `executionLedgerConsistencyMu` stores the synchronization state protecting shared data.
	executionLedgerConsistencyMu sync.Mutex
	// `executionLedgerGeneration` stores the value associated with this record.
	executionLedgerGeneration uint64
	// `executionLedgerConsistencyHeight` stores the value associated with this record.
	executionLedgerConsistencyHeight uint64
	// `executionLedgerConsistencyGeneration` stores the value associated with this record.
	executionLedgerConsistencyGeneration uint64
	// `executionLedgerConsistencyCheckingHeight` stores the value associated with this record.
	executionLedgerConsistencyCheckingHeight uint64
	// `executionLedgerConsistencyCheckingGeneration` stores the value associated with this record.
	executionLedgerConsistencyCheckingGeneration uint64
	// `executionLedgerConsistencyChecks` stores the value associated with this record.
	executionLedgerConsistencyChecks uint64
	// `executionLedgerRepairBlockedHeight` stores the value associated with this record.
	executionLedgerRepairBlockedHeight uint64

	// `snapshotGossipMu` stores the synchronization state protecting shared data.
	snapshotGossipMu sync.RWMutex
	// `snapshotMetaGossipCache` stores the value associated with this record.
	snapshotMetaGossipCache map[string]SnapshotMetaGossip
	// `snapshotChunkGossipCache` stores the value associated with this record.
	snapshotChunkGossipCache map[string]SnapshotChunkGossip
	// `snapshotMetaLastPublished` stores the value associated with this record.
	snapshotMetaLastPublished string
	// `snapshotMetaLastPublishedAt` stores the value associated with this record.
	snapshotMetaLastPublishedAt time.Time
	// `snapshotChunkLastPublished` stores the value associated with this record.
	snapshotChunkLastPublished string
	// `snapshotChunkLastPublishedAt` stores the value associated with this record.
	snapshotChunkLastPublishedAt time.Time
	// `snapshotBoostUntil` stores the value associated with this record.
	snapshotBoostUntil time.Time
	// `snapshotProofMu` stores the synchronization state protecting shared data.
	snapshotProofMu sync.RWMutex
	// `validatorSnapshotPublishMu` stores whether the related condition is satisfied.
	validatorSnapshotPublishMu sync.Mutex
	// `snapshotProofs` stores the value associated with this record.
	snapshotProofs map[string]map[string]SnapshotProof // candidateKey -> validator -> proof
	// `snapshotProofProviders` stores the value associated with this record.
	snapshotProofProviders map[string]map[string]string // candidateKey -> validator -> peerID that supplied proof
	// `snapshotAnchorCache` stores the value associated with this record.
	snapshotAnchorCache map[uint64]SnapshotAnchorCache // checkpointHeight -> best cached anchor
	// `snapshotProofLastPublished` stores the value associated with this record.
	snapshotProofLastPublished string
	// `snapshotProofLastPublishedAt` stores the value associated with this record.
	snapshotProofLastPublishedAt time.Time
	// `validatorSnapshotPublishHeight` stores whether the related condition is satisfied.
	validatorSnapshotPublishHeight uint64
	// `validatorSnapshotPublishHash` stores whether the related condition is satisfied.
	validatorSnapshotPublishHash string
	// `validatorSnapshotPublishAt` stores whether the related condition is satisfied.
	validatorSnapshotPublishAt time.Time
	// `validatorSnapshotPublishError` stores the error produced by this operation.
	validatorSnapshotPublishError string
	// `validatorSnapshotPublished` stores whether the related condition is satisfied.
	validatorSnapshotPublished *StateSnapshot
	// `snapshotCatalogMu` stores the synchronization state protecting shared data.
	snapshotCatalogMu sync.RWMutex
	// `snapshotCatalog` stores the value associated with this record.
	snapshotCatalog map[uint64]SnapshotCatalogEntry

	// Snapshot trust cache (quorum across time)
	snapshotTrustMu sync.Mutex
	// `snapshotVotes` stores the value associated with this record.
	snapshotVotes map[string]map[string]struct{} // trustKey -> validator IDs
	// `snapshotCache` stores the value associated with this record.
	snapshotCache map[string]*StateSnapshot // trustKey -> snapshot

	// `observabilityMu` stores the synchronization state protecting shared data.
	observabilityMu sync.RWMutex
	// `observability` stores the value associated with this record.
	observability observabilityStats
	// `storageManagerMu` stores the synchronization state protecting shared data.
	storageManagerMu sync.Mutex
	// `storageManagerRunning` stores the value associated with this record.
	storageManagerRunning bool
	// `storageManagerLastScheduledHeight` stores the value associated with this record.
	storageManagerLastScheduledHeight uint64

	// `consensusDetectorMu` stores the synchronization state protecting shared data.
	consensusDetectorMu sync.Mutex
	// `consensusDetectorCandidateMode` stores the value associated with this record.
	consensusDetectorCandidateMode string
	// `consensusDetectorCandidateReason` stores the value associated with this record.
	consensusDetectorCandidateReason string
	// `consensusDetectorCandidateSamples` stores the value associated with this record.
	consensusDetectorCandidateSamples int
	// `consensusDetectorStableMode` stores the value associated with this record.
	consensusDetectorStableMode string
	// `consensusDetectorStableReason` stores the value associated with this record.
	consensusDetectorStableReason string
}

type MapStats struct {
	// `SeenBlockHashes` stores the value associated with this record.
	SeenBlockHashes int
	// `SeenTxIDs` stores the value associated with this record.
	SeenTxIDs int
	// `ForkBlocksHeights` stores the value associated with this record.
	ForkBlocksHeights int
	// `ForkBlocksTotal` stores the measured quantity used by this operation.
	ForkBlocksTotal int
	// `ExecResultsKeys` stores the value associated with this record.
	ExecResultsKeys int
	// `ExecResultsTotal` stores the measured quantity used by this operation.
	ExecResultsTotal int
	// `PendingBlocks` stores the value associated with this record.
	PendingBlocks int
	// `QueuedExecVotesKeys` stores the value associated with this record.
	QueuedExecVotesKeys int
	// `QueuedExecVotesTotal` stores the measured quantity used by this operation.
	QueuedExecVotesTotal int
	// `AcceptedProposal` stores the value associated with this record.
	AcceptedProposal int
	// `AcceptedProposalBlocks` stores the value associated with this record.
	AcceptedProposalBlocks int
	// `ExecPoolEpochs` stores the value associated with this record.
	ExecPoolEpochs int
	// `ExecPoolPoolEpochs` stores the value associated with this record.
	ExecPoolPoolEpochs int
	// `ExecPoolResults` stores the value associated with this record.
	ExecPoolResults int
	// `ExecPoolResultSigners` stores the value associated with this record.
	ExecPoolResultSigners int
	// `ExecPoolSignerScopes` stores the value associated with this record.
	ExecPoolSignerScopes int
	// `ExecPoolSigners` stores the value associated with this record.
	ExecPoolSigners int
	// `ExecPoolChoiceScopes` stores the value associated with this record.
	ExecPoolChoiceScopes int
	// `ExecPoolChoices` stores the value associated with this record.
	ExecPoolChoices int
	// `ExecPoolFrozenScopes` stores the value associated with this record.
	ExecPoolFrozenScopes int
	// `ExecPoolEpochChoices` stores the value associated with this record.
	ExecPoolEpochChoices int
	// `ExecPoolCommitChoices` stores the value associated with this record.
	ExecPoolCommitChoices int
	// `ExecBroadcastedEpochs` stores the value associated with this record.
	ExecBroadcastedEpochs int
	// `ExecBroadcastedKeys` stores the value associated with this record.
	ExecBroadcastedKeys int
	// `ExecSignerSeenEpochs` stores the value associated with this record.
	ExecSignerSeenEpochs int
	// `ExecSignerSeenTotal` stores the measured quantity used by this operation.
	ExecSignerSeenTotal int
	// `ExecBroadcastedByValEpochs` stores the value associated with this record.
	ExecBroadcastedByValEpochs int
	// `ExecBroadcastedByValTotal` stores the measured quantity used by this operation.
	ExecBroadcastedByValTotal int
	// `ExecRebroadcastAtEpochs` stores the value associated with this record.
	ExecRebroadcastAtEpochs int
	// `ValidatorStatusCount` stores whether the related condition is satisfied.
	ValidatorStatusCount int
	// `CandidateCount` stores the measured quantity used by this operation.
	CandidateCount int
	// `PeerStateCount` stores the measured quantity used by this operation.
	PeerStateCount int
	// `PeerConnectedCount` stores the measured quantity used by this operation.
	PeerConnectedCount int
	// `PeerConnectingCount` stores the measured quantity used by this operation.
	PeerConnectingCount int
	// `PeerSuspectCount` stores the measured quantity used by this operation.
	PeerSuspectCount int
	// `PeerQuarantinedCount` stores the measured quantity used by this operation.
	PeerQuarantinedCount int
	// `AllowedPeerCount` stores whether the related condition is satisfied.
	AllowedPeerCount int
	// `ValidatorSetPendingCount` stores whether the related condition is satisfied.
	ValidatorSetPendingCount int
	// `ValidatorSetRemovalCount` stores whether the related condition is satisfied.
	ValidatorSetRemovalCount int
	// `QueuedValidatorSetUpdates` stores the value associated with this record.
	QueuedValidatorSetUpdates int
	// `HeightReportsEpochs` stores the value associated with this record.
	HeightReportsEpochs int
	// `HeightReportValidatorsTotal` stores the measured quantity used by this operation.
	HeightReportValidatorsTotal int
	// `MisbehaviorValidators` stores the value associated with this record.
	MisbehaviorValidators int
	// `MisbehaviorEventsTotal` stores the measured quantity used by this operation.
	MisbehaviorEventsTotal int
	// `ExecMismatchTracked` stores the value associated with this record.
	ExecMismatchTracked int
	// `ExecVoteReplayCache` stores the value associated with this record.
	ExecVoteReplayCache int
}

type SyncPeerScore struct {
	// `SnapshotSuccess` stores the value associated with this record.
	SnapshotSuccess uint64
	// `SnapshotFail` stores the value associated with this record.
	SnapshotFail uint64
	// `BlockBatchSuccess` stores the block data handled by this operation.
	BlockBatchSuccess uint64
	// `BlockBatchFail` stores the block data handled by this operation.
	BlockBatchFail uint64
	// `DialSuccess` stores the value associated with this record.
	DialSuccess uint64
	// `DialFailure` stores the value associated with this record.
	DialFailure uint64
	// `TimeoutCount` stores the measured quantity used by this operation.
	TimeoutCount uint64
	// `InvalidProofCount` stores the measured quantity used by this operation.
	InvalidProofCount uint64
	// `SecurityFaultCount` stores the measured quantity used by this operation.
	SecurityFaultCount uint64
	// `RateLimitDropCount` stores the measured quantity used by this operation.
	RateLimitDropCount uint64
	// `TrustedSecurityFaultCount` stores trusted-peer security faults that should not poison reputation.
	TrustedSecurityFaultCount uint64
	// `TrustedRateLimitDropCount` stores trusted-peer rate-limit drops that should not poison reputation.
	TrustedRateLimitDropCount uint64
	// `DecayedAt` stores the value associated with this record.
	DecayedAt time.Time
	// `AvgLatencyMs` stores the value associated with this record.
	AvgLatencyMs float64
	// `LastBytesPerSec` stores the value associated with this record.
	LastBytesPerSec float64
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt time.Time
}

type PeerResourceWindow struct {
	// `StartedAt` stores the value associated with this record.
	StartedAt time.Time
	// `Bytes` stores the value associated with this record.
	Bytes uint64
	// `Messages` stores the value associated with this record.
	Messages uint64
	// `TxMessages` stores the transaction data handled by this operation.
	TxMessages uint64
	// `BlockRequests` stores the block data handled by this operation.
	BlockRequests uint64
	// `Connections` stores the value associated with this record.
	Connections uint64
}

type WireMessage struct {
	// `Type` stores the value associated with this record.
	Type string `json:"type"`
	// `Data` stores the value associated with this record.
	Data json.RawMessage `json:"data"`
}

type ValidatorStatus struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `ReportedHeight` stores the value associated with this record.
	ReportedHeight uint64
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64
	// `ExecEpoch` stores the value associated with this record.
	ExecEpoch uint64
	// `ValidatorSetHeight` stores whether the related condition is satisfied.
	ValidatorSetHeight uint64
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string
	// `LastSeen` stores the value associated with this record.
	LastSeen time.Time
	// `Active` stores the value associated with this record.
	Active bool

	// `ID` stores the current position in the related collection.
	ID string
	// `PubKey` stores the key used to access the related value.
	PubKey ed25519.PublicKey

	// `Enabled` stores whether the related condition is satisfied.
	Enabled bool
	// `ConsensusReadyKnown` stores the value associated with this record.
	ConsensusReadyKnown bool
}

type CommitteeLivenessSnapshot struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `CommitteeSize` stores the measured quantity used by this operation.
	CommitteeSize int
	// `Live` stores the value associated with this record.
	Live int
	// `HeartbeatLive` stores the value associated with this record.
	HeartbeatLive int
	// `OutOfDrift` stores the result produced by this operation.
	OutOfDrift int
	// `Offline` stores the value associated with this record.
	Offline int
	// `WindowMs` stores the value associated with this record.
	WindowMs uint64
	// `SafeUntil` stores the value associated with this record.
	SafeUntil time.Time
}

type CandidateStatus struct {
	// `ID` stores the current position in the related collection.
	ID string
	// `PubKey` stores the key used to access the related value.
	PubKey ed25519.PublicKey
	// `FirstSeenHeight` stores the value associated with this record.
	FirstSeenHeight uint64
	// `ObservationStartHeight` stores the value associated with this record.
	ObservationStartHeight uint64
	// `LastObservedHeight` stores the value associated with this record.
	LastObservedHeight uint64
	// `ObservedEpochs` stores the value associated with this record.
	ObservedEpochs uint64
	// `MatchedEpochs` stores the value associated with this record.
	MatchedEpochs uint64
	// `HeartbeatGood` stores the value associated with this record.
	HeartbeatGood uint64
	// `HeartbeatTotal` stores the measured quantity used by this operation.
	HeartbeatTotal uint64
	// `LastReportedHeight` stores the value associated with this record.
	LastReportedHeight uint64
	// `LastFinalizedHeight` stores the value associated with this record.
	LastFinalizedHeight uint64
	// `LastHeartbeatEpoch` stores the value associated with this record.
	LastHeartbeatEpoch uint64
	// `LastValidatorSetHeight` stores the value associated with this record.
	LastValidatorSetHeight uint64
	// `LastValidatorSetHash` stores the digest used to identify or verify the related data.
	LastValidatorSetHash string
	// `LastHeartbeatAt` stores the value associated with this record.
	LastHeartbeatAt time.Time
	// `ExecHashes` stores the value associated with this record.
	ExecHashes map[uint64]string // epoch -> exec hash
	// `PendingMatch` stores the value associated with this record.
	PendingMatch map[uint64]bool // epoch -> awaiting exec hash
	// `Strikes` stores the value associated with this record.
	Strikes int
	// `BanUntil` stores the value associated with this record.
	BanUntil uint64
	// `PermanentBan` stores the value associated with this record.
	PermanentBan bool
	// `Promoted` stores the value associated with this record.
	Promoted bool
	// `DiversityConsecutive` stores the value associated with this record.
	DiversityConsecutive uint64
	// `LastDiversityRatio` stores the value associated with this record.
	LastDiversityRatio float64
	// `DiversitySum` stores the value associated with this record.
	DiversitySum float64
	// `GossipTimely` stores the value associated with this record.
	GossipTimely uint64
	// `GossipTotal` stores the measured quantity used by this operation.
	GossipTotal uint64
	// `GossipMissing` stores the value associated with this record.
	GossipMissing uint64
	// `SRPScore` stores the value associated with this record.
	SRPScore float64
}

type ExecMismatchTracker struct {
	// `Count` stores the measured quantity used by this operation.
	Count int
	// `LastEpoch` stores the value associated with this record.
	LastEpoch uint64
	// `LastAt` stores the value associated with this record.
	LastAt time.Time
}

type transitionBarrierRetryKey struct {
	// `Reason` stores the value associated with this record.
	Reason string
	// `UpdateHeight` stores the value associated with this record.
	UpdateHeight uint64
	// `NextSetHash` stores the digest used to identify or verify the related data.
	NextSetHash string
}

type transitionBarrierRetryState struct {
	// `NextRetryHeight` stores the value associated with this record.
	NextRetryHeight uint64
	// `LastFailTuple` stores the value associated with this record.
	LastFailTuple string
	// `LastUpdatedAt` stores the value associated with this record.
	LastUpdatedAt time.Time
}

type validatorSetTransitionPlan struct {
	// `Active` stores the value associated with this record.
	Active bool
	// `UpdateHeight` stores the value associated with this record.
	UpdateHeight uint64
	// `NextSetHash` stores the digest used to identify or verify the related data.
	NextSetHash string
	// `NextValidators` stores the value associated with this record.
	NextValidators []string
	// `ProcessedAdds` stores the value associated with this record.
	ProcessedAdds []string
	// `ProcessedRemoves` stores the value associated with this record.
	ProcessedRemoves []string
	// `LockedAt` stores the synchronization state protecting shared data.
	LockedAt time.Time
	// `Reports` stores the value associated with this record.
	Reports int
	// `Matches` stores the value associated with this record.
	Matches int
	// `Required` stores the request data being processed.
	Required int
}

type SnapshotSyncStage string

const (
	// `SnapshotSyncStageIdle` defines the constant value used by this package.
	SnapshotSyncStageIdle SnapshotSyncStage = "IDLE"
	// `SnapshotSyncStageDetectLag` defines the constant value used by this package.
	SnapshotSyncStageDetectLag SnapshotSyncStage = "DETECT_LAG"
	// `SnapshotSyncStageFreezeAnchor` defines the constant value used by this package.
	SnapshotSyncStageFreezeAnchor SnapshotSyncStage = "FREEZE_ANCHOR"
	// `SnapshotSyncStageCheckAnchor` defines the constant value used by this package.
	SnapshotSyncStageCheckAnchor SnapshotSyncStage = "CHECK_ANCHOR"
	// `SnapshotSyncStageCollectProofs` defines the constant value used by this package.
	SnapshotSyncStageCollectProofs SnapshotSyncStage = "COLLECT_PROOFS"
	// `SnapshotSyncStageVerifyQuorum` defines the constant value used by this package.
	SnapshotSyncStageVerifyQuorum SnapshotSyncStage = "VERIFY_QUORUM"
	// `SnapshotSyncStageProviderRotate` defines the constant value used by this package.
	SnapshotSyncStageProviderRotate SnapshotSyncStage = "PROVIDER_ROTATE"
	// `SnapshotSyncStageApplySnapshot` defines the constant value used by this package.
	SnapshotSyncStageApplySnapshot SnapshotSyncStage = "APPLY_SNAPSHOT"
	// `SnapshotSyncStageDeltaReplay` defines the constant value used by this package.
	SnapshotSyncStageDeltaReplay SnapshotSyncStage = "DELTA_REPLAY"
	// `SnapshotSyncStageSyncComplete` defines the constant value used by this package.
	SnapshotSyncStageSyncComplete SnapshotSyncStage = "SYNC_COMPLETE"
)

type SnapshotVote struct {
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string `json:"validator_id"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `SignatureHex` stores the value associated with this record.
	SignatureHex string `json:"signature_hex"`
}

type SnapshotSession struct {
	// `Active` stores the value associated with this record.
	Active bool `json:"active"`
	// `SessionID` stores the value associated with this record.
	SessionID string `json:"session_id,omitempty"`
	// `Stage` stores the value associated with this record.
	Stage SnapshotSyncStage `json:"stage"`
	// `FreezeHeight` stores the value associated with this record.
	FreezeHeight uint64 `json:"freeze_height"`
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64 `json:"checkpoint_height"`
	// `CandidateHeight` stores the value associated with this record.
	CandidateHeight uint64 `json:"candidate_height,omitempty"`
	// `CandidateCheckpointHeight` stores the value associated with this record.
	CandidateCheckpointHeight uint64 `json:"candidate_checkpoint_height,omitempty"`
	// `StartedAt` stores the value associated with this record.
	StartedAt time.Time `json:"started_at"`
	// `LastVoteAt` stores the value associated with this record.
	LastVoteAt time.Time `json:"last_vote_at"`
	// `CanonicalHash` stores the digest used to identify or verify the related data.
	CanonicalHash string `json:"canonical_hash"`
	// `CheckpointHash` stores the digest used to identify or verify the related data.
	CheckpointHash string `json:"checkpoint_hash"`
	// `FreezeStateRoot` stores the digest used to identify or verify the related data.
	FreezeStateRoot string `json:"freeze_state_root"`
	// `FreezeVsetHash` stores the digest used to identify or verify the related data.
	FreezeVsetHash string `json:"freeze_vset_hash"`
	// `FreezeRegistryHash` stores the digest used to identify or verify the related data.
	FreezeRegistryHash string `json:"freeze_registry_hash,omitempty"`
	// `FreezeSnapHash` stores the digest used to identify or verify the related data.
	FreezeSnapHash string `json:"freeze_snapshot_hash"`
	// `Votes` stores the value associated with this record.
	Votes map[string]SnapshotVote `json:"votes"`
	// `Required` stores the request data being processed.
	Required int `json:"required"`
	// `RequiredVotes` stores the request data being processed.
	RequiredVotes int `json:"required_votes"`
	// `Applied` stores the value associated with this record.
	Applied bool `json:"applied"`
	// `Completed` stores the value associated with this record.
	Completed bool `json:"completed"`
	// `ProviderSet` stores the value associated with this record.
	ProviderSet []string `json:"provider_set"`
	// `CurrentProvider` stores the value associated with this record.
	CurrentProvider string `json:"current_provider"`
	// `Deadline` stores the value associated with this record.
	Deadline time.Time `json:"deadline"`
	// `LastTriggerTime` stores the value associated with this record.
	LastTriggerTime time.Time `json:"last_trigger_time"`
	// `RetryCount` stores the measured quantity used by this operation.
	RetryCount uint64 `json:"retry_count"`
	// `LastError` stores the error produced by this operation.
	LastError string `json:"last_error"`
	// `LastAppliedSnapshotHash` stores the digest used to identify or verify the related data.
	LastAppliedSnapshotHash string `json:"last_applied_snapshot_hash,omitempty"`
	// `LastAppliedSnapshotAttempts` stores the value associated with this record.
	LastAppliedSnapshotAttempts uint64 `json:"last_applied_snapshot_attempts,omitempty"`
	// `LastRejectReason` stores the value associated with this record.
	LastRejectReason string `json:"last_reject_reason,omitempty"`
	// `StrictReasonCounts` stores the value associated with this record.
	StrictReasonCounts map[string]uint64 `json:"strict_reason_counts,omitempty"`
	// `StrictProviderResults` stores the value associated with this record.
	StrictProviderResults map[string]map[string]uint64 `json:"strict_provider_results,omitempty"`
	// `RelaxProofs` stores the value associated with this record.
	RelaxProofs bool `json:"relax_proofs,omitempty"`
}

type LateJoinAuthorityState struct {
	// `Active` stores the value associated with this record.
	Active bool `json:"active"`
	// `Authoritative` stores the value associated with this record.
	Authoritative bool `json:"authoritative"`
	// `Source` stores the value associated with this record.
	Source string `json:"source"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
	// `AnchorBlockHash` stores the digest used to identify or verify the related data.
	AnchorBlockHash string `json:"anchor_block_hash,omitempty"`
}

type OnboardingState string

const (
	// `OnboardingStateCandidateDetected` defines the constant value used by this package.
	OnboardingStateCandidateDetected OnboardingState = "candidate_detected"
	// `OnboardingStateAwaitingPubKey` defines the key used to access the related value.
	OnboardingStateAwaitingPubKey OnboardingState = "awaiting_pubkey"
	// `OnboardingStateAwaitingHeartbeat` defines the constant value used by this package.
	OnboardingStateAwaitingHeartbeat OnboardingState = "awaiting_heartbeat"
	// `OnboardingStateAwaitingRejoinProof` defines the constant value used by this package.
	OnboardingStateAwaitingRejoinProof OnboardingState = "awaiting_rejoin_proof"
	// `OnboardingStateAwaitingSync` defines the constant value used by this package.
	OnboardingStateAwaitingSync OnboardingState = "awaiting_sync"
	// `OnboardingStateScheduled` defines the constant value used by this package.
	OnboardingStateScheduled OnboardingState = "scheduled"
	// `OnboardingStateActivating` defines the constant value used by this package.
	OnboardingStateActivating OnboardingState = "activating"
	// `OnboardingStateActive` defines the constant value used by this package.
	OnboardingStateActive OnboardingState = "active"
	// `OnboardingStateBlocked` defines the constant value used by this package.
	OnboardingStateBlocked OnboardingState = "blocked"
)

type ValidatorActivationTracker struct {
	// `State` stores the value associated with this record.
	State OnboardingState `json:"state"`
	// `LastReason` stores the value associated with this record.
	LastReason string `json:"last_reason"`
	// `ScheduledHeight` stores the value associated with this record.
	ScheduledHeight uint64 `json:"scheduled_height"`
	// `EffectiveHeight` stores the value associated with this record.
	EffectiveHeight uint64 `json:"effective_height"`
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt time.Time `json:"updated_at"`
}

type Genesis struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Decimals` stores the value associated with this record.
	Decimals int `json:"decimals,omitempty"`
	// `GenesisLocked` stores the value associated with this record.
	GenesisLocked bool `json:"genesis_locked,omitempty"`
	// `ValidatorSetFrozen` stores whether the related condition is satisfied.
	ValidatorSetFrozen bool `json:"validator_set_frozen,omitempty"`
	// `Validators` stores whether the related condition is satisfied.
	Validators map[string]string `json:"validators"` // nodeID -> pubkey(hex)
	// `Balances` stores the value associated with this record.
	Balances map[string]int `json:"balances,omitempty"`
	// `RewardWallets` stores the value associated with this record.
	RewardWallets map[string]string `json:"reward_wallets,omitempty"` // validatorID -> wallet address
	// `Foundation` stores whether the related condition is satisfied.
	Foundation GenesisAllocation `json:"foundation,omitempty"`
	// `Treasury` stores the value associated with this record.
	Treasury GenesisAllocation `json:"treasury,omitempty"`
	// `GenesisStakes` stores the value associated with this record.
	GenesisStakes map[string]GenesisStake `json:"genesis_stakes,omitempty"`
}

type GenesisStake struct {
	// `Wallet` stores the value associated with this record.
	Wallet string `json:"wallet,omitempty"`
	// `WalletPubKey` stores the key used to access the related value.
	WalletPubKey string `json:"wallet_pubkey,omitempty"`
	// `Amount` stores the value associated with this record.
	Amount int `json:"amount"`
	// `LockEpochs` stores the synchronization state protecting shared data.
	LockEpochs uint64 `json:"lock_epochs,omitempty"`
}

type GenesisAllocation struct {
	// `Wallet` stores the value associated with this record.
	Wallet string `json:"wallet,omitempty"`
	// `Allocation` stores the value associated with this record.
	Allocation int `json:"allocation,omitempty"`
	// `Locked` stores the synchronization state protecting shared data.
	Locked bool `json:"locked,omitempty"`
	// `LockEpochs` stores the synchronization state protecting shared data.
	LockEpochs uint64 `json:"lock_epochs,omitempty"`
	// `GovernanceOnly` stores the value associated with this record.
	GovernanceOnly bool `json:"governance_only,omitempty"`
}

type genesisBootstrapWalletBinding struct {
	// `WalletAddr` stores the address used by this operation.
	WalletAddr string
	// `WalletPubKey` stores the key used to access the related value.
	WalletPubKey string
}

var (
	// `genesisRewardWalletsMu` stores the synchronization state protecting shared data.
	genesisRewardWalletsMu sync.RWMutex
	// `genesisRewardWallets` stores the value used by this operation.
	genesisRewardWallets = make(map[string]string) // normalized validatorID -> wallet
	// `genesisBootstrapWalletsMu` stores the synchronization state protecting shared data.
	genesisBootstrapWalletsMu sync.RWMutex
	// `genesisBootstrapWalletBindings` stores the value used by this operation.
	genesisBootstrapWalletBindings = make(map[string]genesisBootstrapWalletBinding) // normalized validatorID -> genesis wallet binding
)

// setGenesisRewardWallets implements the set genesis reward wallets helper.
func setGenesisRewardWallets(m map[string]string) {
	genesisRewardWalletsMu.Lock()
	defer genesisRewardWalletsMu.Unlock()
	genesisRewardWallets = make(map[string]string)
	// `vid` and `wallet` track the current values while iterating.
	for vid, wallet := range m {
		// `id` stores the current position in the related collection.
		id := strings.TrimSpace(strings.ToUpper(vid))
		// `addr` stores the address used by this operation.
		addr := strings.TrimSpace(wallet)
		if id == "" || addr == "" {
			continue
		}
		genesisRewardWallets[id] = addr
	}
}

// genesisRewardWallet implements the genesis reward wallet helper.
func genesisRewardWallet(validatorID string) (string, bool) {
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(strings.ToUpper(validatorID))
	if id == "" {
		return "", false
	}
	genesisRewardWalletsMu.RLock()
	// `addr` and `ok` store whether the related condition is satisfied.
	addr, ok := genesisRewardWallets[id]
	genesisRewardWalletsMu.RUnlock()
	if !ok || strings.TrimSpace(addr) == "" {
		return "", false
	}
	return addr, true
}

// setGenesisBootstrapWalletBindings implements the set genesis bootstrap wallet bindings helper.
func setGenesisBootstrapWalletBindings(m map[string]genesisBootstrapWalletBinding) {
	genesisBootstrapWalletsMu.Lock()
	defer genesisBootstrapWalletsMu.Unlock()
	genesisBootstrapWalletBindings = make(map[string]genesisBootstrapWalletBinding)
	// `vid` and `binding` track the current values while iterating.
	for vid, binding := range m {
		// `id` stores the current position in the related collection.
		id := strings.TrimSpace(strings.ToUpper(vid))
		// `addr` stores the address used by this operation.
		addr := strings.TrimSpace(binding.WalletAddr)
		// `pub` stores the value produced by this operation.
		pub := strings.TrimSpace(strings.ToLower(binding.WalletPubKey))
		if id == "" || addr == "" || pub == "" {
			continue
		}
		genesisBootstrapWalletBindings[id] = genesisBootstrapWalletBinding{
			WalletAddr:   addr,
			WalletPubKey: pub,
		}
	}
}

// genesisBootstrapWalletBindingForValidator implements the genesis bootstrap wallet binding for validator helper.
func genesisBootstrapWalletBindingForValidator(validatorID string) (genesisBootstrapWalletBinding, bool) {
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(strings.ToUpper(validatorID))
	if id == "" {
		return genesisBootstrapWalletBinding{}, false
	}
	genesisBootstrapWalletsMu.RLock()
	// `binding` and `ok` store whether the related condition is satisfied.
	binding, ok := genesisBootstrapWalletBindings[id]
	genesisBootstrapWalletsMu.RUnlock()
	if !ok || strings.TrimSpace(binding.WalletAddr) == "" || strings.TrimSpace(binding.WalletPubKey) == "" {
		return genesisBootstrapWalletBinding{}, false
	}
	return binding, true
}

// trustedGenesisWalletBindingForValidator implements the trusted genesis wallet binding for validator helper.
func trustedGenesisWalletBindingForValidator(validatorID string) (genesisBootstrapWalletBinding, string, bool) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(validatorID)
	if id == "" {
		return genesisBootstrapWalletBinding{}, "", false
	}
	// `binding` and `ok` store whether the related condition is satisfied.
	binding, ok := genesisBootstrapWalletBindingForValidator(id)
	if !ok {
		return genesisBootstrapWalletBinding{}, "", false
	}
	// `rewardWallet` and `ok` store whether the related condition is satisfied.
	rewardWallet, ok := genesisRewardWallet(id)
	if !ok || !addressesEqual(rewardWallet, binding.WalletAddr) {
		return genesisBootstrapWalletBinding{}, "", false
	}
	validatorPubKeysMu.RLock()
	// `knownGenesisValidator` stores the value produced by this operation.
	_, knownGenesisValidator := GenesisValidatorPubKeys[id]
	// `hasGenesisValidators` stores the value produced by this operation.
	hasGenesisValidators := len(GenesisValidatorPubKeys) > 0
	validatorPubKeysMu.RUnlock()
	if hasGenesisValidators && !knownGenesisValidator {
		return genesisBootstrapWalletBinding{}, "", false
	}
	return binding, rewardWallet, true
}

// genesisWalletAuthExemptValidator implements the genesis wallet auth exempt validator helper.
func genesisWalletAuthExemptValidator(validatorID string) bool {
	// `ok` stores whether the related condition is satisfied.
	_, _, ok := trustedGenesisWalletBindingForValidator(validatorID)
	return ok
}

type EvidenceKey struct {
	// `Leaderme` stores the value associated with this record.
	Leaderme string
	// `Height` stores the value associated with this record.
	Height uint64

	// `Leader` stores the value associated with this record.
	Leader string
}

var (
	// NetworkName/IsTestnet are retained for status/UI/local compatibility.
	// Consensus-sensitive rules must use protocolNetworkName/protocolIsTestnet
	// so local config or test mutation cannot change block/tx validity.
	NetworkName = "testnet" // "testnet" | "mainnet"
	// `IsTestnet` stores the current position in the related collection.
	IsTestnet = true
	// `AllowTreasuryOps` stores the value used by this operation.
	AllowTreasuryOps = false
)

// `protocolNetworkName` defines the constant value used by this package.
const protocolNetworkName = "testnet"

// `protocolTestnet` defines the constant value used by this package.
const protocolTestnet = true

// protocolIsTestnet implements the protocol is testnet helper.
func protocolIsTestnet() bool {
	return protocolTestnet
}

// protocolFaucetEnabled implements the protocol faucet enabled helper.
func protocolFaucetEnabled() bool {
	return protocolIsTestnet()
}

// protocolRequiresStrictConsensusSignatures implements the protocol requires strict consensus signatures helper.
func protocolRequiresStrictConsensusSignatures() bool {
	return !protocolIsTestnet()
}

var (
	// `ConfigDTLContractsV2Enabled` stores whether the related condition is satisfied.
	ConfigDTLContractsV2Enabled = false
	// `ConfigDTLV2ActivationHeight` stores the configuration used by this operation.
	ConfigDTLV2ActivationHeight uint64
	// `ConfigDTLLogsIndexEnabled` stores whether the related condition is satisfied.
	ConfigDTLLogsIndexEnabled = true
	// `ConfigDTLOracleMinSigners` stores the configuration used by this operation.
	ConfigDTLOracleMinSigners uint16 = DTLDefaultOracleMinSigners
	// `ConfigDTLOracleMaxStalenessBlocks` stores the configuration used by this operation.
	ConfigDTLOracleMaxStalenessBlocks uint64 = DTLDefaultOracleMaxStalenessBlocks
	// `ConfigDTLLendingAccrualIntervalBlocks` stores the configuration used by this operation.
	ConfigDTLLendingAccrualIntervalBlocks uint64 = DTLDefaultLendingAccrualIntervalBlocks
	// `ConfigDTLGameBeaconDelayBlocks` stores the configuration used by this operation.
	ConfigDTLGameBeaconDelayBlocks uint64 = DTLDefaultGameBeaconDelayBlocks
	// `ConfigDTLRouterEnabled` stores whether the related condition is satisfied.
	ConfigDTLRouterEnabled = true
	// `ConfigDTLRouterMaxHops` stores the configuration used by this operation.
	ConfigDTLRouterMaxHops uint16 = DTLDefaultRouterMaxHops
	// `ConfigDTLRouterDeadlineMaxBlocks` stores the configuration used by this operation.
	ConfigDTLRouterDeadlineMaxBlocks uint64 = DTLDefaultRouterDeadlineMaxBlocks
	// `ConfigDTLRouterMaxPriceImpactBPS` stores the configuration used by this operation.
	ConfigDTLRouterMaxPriceImpactBPS uint16 = DTLDefaultRouterMaxPriceImpactBPS
	// `ConfigDTLRouterQuoteMaxPaths` stores the configuration used by this operation.
	ConfigDTLRouterQuoteMaxPaths uint16 = DTLDefaultRouterQuoteMaxPaths
	// `ConfigDTLDeFiFarmEnabled` stores whether the related condition is satisfied.
	ConfigDTLDeFiFarmEnabled = true
	// `ConfigDTLGameFiSeasonEnabled` stores whether the related condition is satisfied.
	ConfigDTLGameFiSeasonEnabled = true
	// `ConfigDTLGameFiRewardToken` stores the configuration used by this operation.
	ConfigDTLGameFiRewardToken = CoinSymbol
	// `ConfigDTLGameFiSeasonLengthBlocks` stores the configuration used by this operation.
	ConfigDTLGameFiSeasonLengthBlocks uint64 = DTLDefaultGameFiSeasonLengthBlocks
	// `ConfigDTLGameFiClaimGraceBlocks` stores the configuration used by this operation.
	ConfigDTLGameFiClaimGraceBlocks uint64 = DTLDefaultGameFiClaimGraceBlocks
	// `ConfigDTLGameFiFeeShareFromPoolBPS` stores the configuration used by this operation.
	ConfigDTLGameFiFeeShareFromPoolBPS uint16 = DTLDefaultGameFiFeeSharePoolBPS
	// `ConfigDTLGameFiFeeShareFromLendingBPS` stores the configuration used by this operation.
	ConfigDTLGameFiFeeShareFromLendingBPS uint16 = DTLDefaultGameFiFeeShareLendingBPS
	// `ConfigDTLGameFiDuelWinPoints` stores the configuration used by this operation.
	ConfigDTLGameFiDuelWinPoints uint64 = DTLDefaultGameFiDuelWinPoints
	// `ConfigDTLGameFiTournamentWinPoints` stores the configuration used by this operation.
	ConfigDTLGameFiTournamentWinPoints uint64 = DTLDefaultGameFiTournamentWinPoints
	// `ConfigDTLGameFiTournamentPartPoints` stores the configuration used by this operation.
	ConfigDTLGameFiTournamentPartPoints uint64 = DTLDefaultGameFiTournamentPartPoints
	// `ConfigDTLFarmMinStakeBlocks` stores the configuration used by this operation.
	ConfigDTLFarmMinStakeBlocks uint64 = DTLDefaultFarmMinStakeBlocks
	// `ConfigDTLFarmLPPointsPerBlock` stores the configuration used by this operation.
	ConfigDTLFarmLPPointsPerBlock uint64 = DTLDefaultFarmLPPointsPerBlock
	// `ConfigDTLFarmMaxMultiplierBPS` stores the configuration used by this operation.
	ConfigDTLFarmMaxMultiplierBPS uint16 = DTLDefaultFarmMaxMultiplierBPS
	// `ConfigDTLGameFiMaxRewardPerSeason` stores the configuration used by this operation.
	ConfigDTLGameFiMaxRewardPerSeason uint64 = DTLDefaultGameFiMaxRewardPerSeason
)

// dtlV2EnabledAtHeight implements the dtl v2 enabled at height helper.
func dtlV2EnabledAtHeight(height uint64) bool {
	if !ConfigDTLContractsV2Enabled {
		return false
	}
	if ConfigDTLV2ActivationHeight == 0 {
		return true
	}
	return height >= ConfigDTLV2ActivationHeight
}

// dtlBeaconDelayAtHeight implements the dtl beacon delay at height helper.
func dtlBeaconDelayAtHeight(height uint64) uint64 {
	if !dtlV2EnabledAtHeight(height) {
		return 0
	}
	return ConfigDTLGameBeaconDelayBlocks
}

// dtlRouterEnabled implements the dtl router enabled helper.
func dtlRouterEnabled() bool {
	return ConfigDTLRouterEnabled
}

// dtlRouterMaxHops implements the dtl router max hops helper.
func dtlRouterMaxHops() int {
	// `v` stores the value produced by this operation.
	v := int(ConfigDTLRouterMaxHops)
	if v < 1 {
		return 1
	}
	if v > 16 {
		return 16
	}
	return v
}

// dtlRouterDeadlineMaxBlocks implements the dtl router deadline max blocks helper.
func dtlRouterDeadlineMaxBlocks() uint64 {
	if ConfigDTLRouterDeadlineMaxBlocks == 0 {
		return DTLDefaultRouterDeadlineMaxBlocks
	}
	return ConfigDTLRouterDeadlineMaxBlocks
}

// dtlRouterMaxPriceImpactBPS implements the dtl router max price impact bps helper.
func dtlRouterMaxPriceImpactBPS() uint16 {
	// `v` stores the value produced by this operation.
	v := ConfigDTLRouterMaxPriceImpactBPS
	if v == 0 {
		return DTLDefaultRouterMaxPriceImpactBPS
	}
	if v > DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	return v
}

// dtlRouterQuoteMaxPaths implements the dtl router quote max paths helper.
func dtlRouterQuoteMaxPaths() int {
	// `v` stores the value produced by this operation.
	v := int(ConfigDTLRouterQuoteMaxPaths)
	if v < 1 {
		return int(DTLDefaultRouterQuoteMaxPaths)
	}
	if v > 64 {
		return 64
	}
	return v
}

// dtlDeFiFarmEnabled implements the dtl de fi farm enabled helper.
func dtlDeFiFarmEnabled() bool {
	return ConfigDTLDeFiFarmEnabled
}

// dtlGameFiSeasonEnabled implements the dtl game fi season enabled helper.
func dtlGameFiSeasonEnabled() bool {
	return ConfigDTLGameFiSeasonEnabled
}

// dtlGameFiRewardTokenRef implements the dtl game fi reward token ref helper.
func dtlGameFiRewardTokenRef() string {
	return strings.TrimSpace(ConfigDTLGameFiRewardToken)
}

// dtlGameFiSeasonLengthBlocks implements the dtl game fi season length blocks helper.
func dtlGameFiSeasonLengthBlocks() uint64 {
	if ConfigDTLGameFiSeasonLengthBlocks == 0 {
		return DTLDefaultGameFiSeasonLengthBlocks
	}
	return ConfigDTLGameFiSeasonLengthBlocks
}

// dtlGameFiClaimGraceBlocks implements the dtl game fi claim grace blocks helper.
func dtlGameFiClaimGraceBlocks() uint64 {
	if ConfigDTLGameFiClaimGraceBlocks == 0 {
		return DTLDefaultGameFiClaimGraceBlocks
	}
	return ConfigDTLGameFiClaimGraceBlocks
}

// dtlGameFiFeeShareFromPoolBPS implements the dtl game fi fee share from pool bps helper.
func dtlGameFiFeeShareFromPoolBPS() uint16 {
	// `v` stores the value produced by this operation.
	v := ConfigDTLGameFiFeeShareFromPoolBPS
	if v > DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	return v
}

// dtlGameFiFeeShareFromLendingBPS implements the dtl game fi fee share from lending bps helper.
func dtlGameFiFeeShareFromLendingBPS() uint16 {
	// `v` stores the value produced by this operation.
	v := ConfigDTLGameFiFeeShareFromLendingBPS
	if v > DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	return v
}

// dtlFarmMaxMultiplierBPS implements the dtl farm max multiplier bps helper.
func dtlFarmMaxMultiplierBPS() uint16 {
	// `v` stores the value produced by this operation.
	v := ConfigDTLFarmMaxMultiplierBPS
	if v < DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	if v > ^uint16(0) {
		return ^uint16(0)
	}
	return v
}

// dtlFarmMinStakeBlocks implements the dtl farm min stake blocks helper.
func dtlFarmMinStakeBlocks() uint64 {
	return ConfigDTLFarmMinStakeBlocks
}

// dtlFarmLPPointsPerBlock implements the dtl farm lp points per block helper.
func dtlFarmLPPointsPerBlock() uint64 {
	if ConfigDTLFarmLPPointsPerBlock == 0 {
		return 1
	}
	return ConfigDTLFarmLPPointsPerBlock
}

type BlockRequest struct {
	// `From` stores the value associated with this record.
	From uint64 `json:"from"`
	// `To` stores the value associated with this record.
	To uint64 `json:"to"`
	// `WantSnapshot` stores the value associated with this record.
	WantSnapshot bool `json:"want_snapshot"`
	// `SnapshotHeight` stores the value associated with this record.
	SnapshotHeight uint64 `json:"snapshot_height,omitempty"`
	// `BypassAck` stores the value associated with this record.
	BypassAck bool `json:"bypass_ack,omitempty"`
}

const (
	// `BlockSyncProtocol` defines the block data handled by this operation.
	BlockSyncProtocol = "/msc/blocksync/1.0.0"
	// `HeaderSyncProtocol` defines the block data handled by this operation.
	HeaderSyncProtocol = "/msc/headersync/1.0.0"
	// `SnapshotMetaProtocol` defines the constant value used by this package.
	SnapshotMetaProtocol = "/msc/snapshot-meta/1.0.0"
	// `SnapshotChunkProtocol` defines the constant value used by this package.
	SnapshotChunkProtocol = "/msc/snapshot-chunk/1.0.0"
)

type BlockResponse struct {
	// `Blocks` stores the block data handled by this operation.
	Blocks []Block `json:"blocks"`
	// `Snapshot` stores the value associated with this record.
	Snapshot *StateSnapshot `json:"snapshot,omitempty"`
	// `ExecPool` stores the value associated with this record.
	ExecPool *ExecPoolSnapshot `json:"exec_pool,omitempty"`
}

type SyncBlockHeader struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string `json:"prev_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `NextValidatorSetHash` stores the digest used to identify or verify the related data.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp,omitempty"`
}

type HeaderSyncLocator struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
}

type HeaderSyncRequest struct {
	// `From` stores the value associated with this record.
	From uint64 `json:"from,omitempty"`
	// `To` stores the value associated with this record.
	To uint64 `json:"to,omitempty"`
	// `Locators` stores the value associated with this record.
	Locators []HeaderSyncLocator `json:"locators,omitempty"`
}

type HeaderSyncResponse struct {
	// `Headers` stores the block data handled by this operation.
	Headers []SyncBlockHeader `json:"headers,omitempty"`
	// `CommonHeight` stores the value associated with this record.
	CommonHeight uint64 `json:"common_height,omitempty"`
	// `CommonHash` stores the digest used to identify or verify the related data.
	CommonHash string `json:"common_hash,omitempty"`
}

type SnapshotMetaRequest struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
}

type SnapshotManifest struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string `json:"state_merkle_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `SnapshotSizeBytes` stores the value associated with this record.
	SnapshotSizeBytes uint64 `json:"snapshot_size_bytes,omitempty"`
	// `Encoding` stores the value associated with this record.
	Encoding string `json:"encoding,omitempty"`
	// `Compression` stores the value associated with this record.
	Compression string `json:"compression,omitempty"`
	// `ChunkSize` stores the measured quantity used by this operation.
	ChunkSize uint64 `json:"chunk_size"`
	// `ChunkCount` stores the measured quantity used by this operation.
	ChunkCount uint64 `json:"chunk_count"`
	// `ChunkHashes` stores the value associated with this record.
	ChunkHashes []string `json:"chunk_hashes,omitempty"`
	// `CheckpointProof` stores the value associated with this record.
	CheckpointProof map[string]string `json:"checkpoint_proof,omitempty"`
}

type SnapshotMetaResponse struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root,omitempty"`
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string `json:"state_merkle_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `Encoding` stores the value associated with this record.
	Encoding string `json:"encoding,omitempty"`
	// `Compression` stores the value associated with this record.
	Compression string `json:"compression,omitempty"`
	// `ChunkSize` stores the measured quantity used by this operation.
	ChunkSize uint64 `json:"chunk_size"`
	// `TotalChunks` stores the measured quantity used by this operation.
	TotalChunks uint64 `json:"total_chunks"`
	// `ChunkHashes` stores the value associated with this record.
	ChunkHashes []string `json:"chunk_hashes,omitempty"`
	// `CheckpointProof` stores the value associated with this record.
	CheckpointProof map[string]string `json:"checkpoint_proof,omitempty"`
	// `Manifest` stores the value associated with this record.
	Manifest *SnapshotManifest `json:"manifest,omitempty"`
	// `Available` stores the value associated with this record.
	Available bool `json:"available"`
}

type SnapshotChunkRequest struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Index` stores the current position in the related collection.
	Index uint64 `json:"index"`
}

type SnapshotChunkResponse struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Index` stores the current position in the related collection.
	Index uint64 `json:"index"`
	// `ChunkHash` stores the digest used to identify or verify the related data.
	ChunkHash string `json:"chunk_hash"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash"`
	// `Encoding` stores the value associated with this record.
	Encoding string `json:"encoding,omitempty"`
	// `Compression` stores the value associated with this record.
	Compression string `json:"compression,omitempty"`
	// `Data` stores the value associated with this record.
	Data []byte `json:"data"`
}

type PeerDiscovery struct {

	// knownPeers   map[string]*PeerInfo
	seedNodes []string
	// `bootstrapURL` stores the value associated with this record.
	bootstrapURL string
	// `isRunning` stores the current position in the related collection.
	isRunning bool
}
type NodeConfig struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Moniker` stores the value associated with this record.
	Moniker string `json:"moniker"`
	// `Seeds` stores the value associated with this record.
	Seeds []string `json:"seeds"`
	// `PersistentPeers` stores the value associated with this record.
	PersistentPeers []string `json:"persistent_peers"`
	// `MinPeers` stores the value associated with this record.
	MinPeers int `json:"min_peers"`
	// `MaxPeers` stores the value associated with this record.
	MaxPeers int `json:"max_peers"`

	// `MinValidators` stores the value associated with this record.
	MinValidators int
}
type SlashEvidence struct {
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string
	// `Reason` stores the value associated with this record.
	Reason string

	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Timestamp` stores the value associated with this record.
	Timestamp int64
	// `Reporter` stores the value associated with this record.
	Reporter string
	// `Height` stores the value associated with this record.
	Height uint64 // 👈 uint64
	// `Validator` stores whether the related condition is satisfied.
	Validator string
}

// `KnownValidators` stores the value used by this operation.
var KnownValidators = map[string]uint64{} // validator → last seen height
// `ValidatorMu` stores whether the related condition is satisfied.
var ValidatorMu sync.Mutex

type StateReceipt struct {
	// `TxHash` stores the transaction data handled by this operation.
	TxHash string `json:"tx_hash"`
	// `PreStateHash` stores the digest used to identify or verify the related data.
	PreStateHash string `json:"pre_state_hash"`
	// `PostStateHash` stores the digest used to identify or verify the related data.
	PostStateHash string `json:"post_state_hash"`
	// `DTLTxType` stores the value associated with this record.
	DTLTxType string `json:"dtl_tx_type,omitempty"`
	// `ContractID` stores the value associated with this record.
	ContractID string `json:"contract_id,omitempty"`
	// `RuntimeMode` stores the value associated with this record.
	RuntimeMode string `json:"runtime_mode,omitempty"`
	// `ContractStandard` stores the value associated with this record.
	ContractStandard string `json:"contract_standard,omitempty"`
	// `ContractInterfaces` stores the value associated with this record.
	ContractInterfaces []string `json:"contract_interfaces,omitempty"`
	// `ABIHash` stores the digest used to identify or verify the related data.
	ABIHash string `json:"abi_hash,omitempty"`
	// `Upgradeable` stores the value associated with this record.
	Upgradeable bool `json:"upgradeable,omitempty"`
	// `ProxyTarget` stores the value associated with this record.
	ProxyTarget string `json:"proxy_target,omitempty"`
	// `OracleFeedID` stores the value associated with this record.
	OracleFeedID string `json:"oracle_feed_id,omitempty"`
	// `HealthFactor` stores the value associated with this record.
	HealthFactor uint64 `json:"health_factor,omitempty"`
	// `RouteHops` stores the value associated with this record.
	RouteHops uint64 `json:"route_hops,omitempty"`
	// `RouteTokenIn` stores the value associated with this record.
	RouteTokenIn string `json:"route_token_in,omitempty"`
	// `RouteTokenOut` stores the result produced by this operation.
	RouteTokenOut string `json:"route_token_out,omitempty"`
	// `BytecodeFormat` stores the value associated with this record.
	BytecodeFormat string `json:"bytecode_format,omitempty"`
	// `BytecodeHash` stores the digest used to identify or verify the related data.
	BytecodeHash string `json:"bytecode_hash,omitempty"`
	// `BytecodeSize` stores the measured quantity used by this operation.
	BytecodeSize uint64 `json:"bytecode_size,omitempty"`
	// `Compiler` stores the value associated with this record.
	Compiler string `json:"compiler,omitempty"`
	// `SourceHash` stores the digest used to identify or verify the related data.
	SourceHash string `json:"source_hash,omitempty"`
	// `Logs` stores the value associated with this record.
	Logs []DTLEventLog `json:"logs,omitempty"`
	// Deterministic DTL resource accounting committed by DTLReceiptsRoot.
	DTLReads        uint64 `json:"dtl_reads,omitempty"`
	DTLWrites       uint64 `json:"dtl_writes,omitempty"`
	DTLEvents       uint64 `json:"dtl_events,omitempty"`
	DTLSteps        uint64 `json:"dtl_steps,omitempty"`
	DTLStorageBytes uint64 `json:"dtl_storage_bytes,omitempty"`
	DTLResourceFee  uint64 `json:"dtl_resource_fee,omitempty"`
}
type Transaction struct {
	// `ID` stores the current position in the related collection.
	ID string `json:"id"`
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `Amount` stores the value associated with this record.
	Amount int `json:"amount"`
	// `Nonce` stores the value associated with this record.
	Nonce int `json:"nonce"`
	// `PublicKey` stores the key used to access the related value.
	PublicKey string `json:"publicKey"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature"`
	// `Fee` stores the value associated with this record.
	Fee int `json:"fee"`

	// `Expiry` stores the value associated with this record.
	Expiry int64 `json:"expiry"` // 🔥 ADD (unix timestamp)
	// `GasLimit` stores the value associated with this record.
	GasLimit uint64
	// StakeEpochs defines the lock duration (in epochs) for stake operations.
	StakeEpochs uint64 `json:"stake_epochs"`
	// ValidatorPubKey anchors the validator consensus pubkey for TxStake onboarding.
	ValidatorPubKey string `json:"validator_pubkey,omitempty"`
	// DTL (Decentralized Token Ledger) envelope for TxDTL.
	DTLTxType string `json:"dtl_tx_type,omitempty"`
	// `DTLTokenID` stores the value associated with this record.
	DTLTokenID string `json:"dtl_token_id,omitempty"`
	// `DTLPayload` stores the value associated with this record.
	DTLPayload string `json:"dtl_payload,omitempty"`
	// `DTLGovernanceCert` stores the value associated with this record.
	DTLGovernanceCert string `json:"dtl_governance_cert,omitempty"`
	// ValidatorUpdateCert carries threshold-governance approval for
	// validator add/remove transactions.
	ValidatorUpdateCert *ValidatorUpdateCertificate `json:"validator_update_cert,omitempty"`

	// MODEL-3
	TaskID string
	// `Input` stores the current position in the related collection.
	Input int

	// `Type` stores the value associated with this record.
	Type TxType

	// `ChainID` stores the value associated with this record.
	ChainID string // 🔥 ADD THIS
	// `Coin` stores the value associated with this record.
	Coin string // e.g. MSC, MSCX

}
type TxType uint8

const (
	// `TxTransfer` defines the transaction data handled by this operation.
	TxTransfer TxType = 0
	// `TxTask` defines the transaction data handled by this operation.
	TxTask TxType = 1
	// `TxStake` defines the transaction data handled by this operation.
	TxStake TxType = 2
	// `TxVote` defines the transaction data handled by this operation.
	TxVote TxType = 3
	// TxValidatorUpdate updates validator set via governance tx.
	TxValidatorUpdate TxType = 4
	// TxFaucet issues testnet funds from the reward pool.
	TxFaucet TxType = 5
	// TxUnstake unlocks previously locked stake.
	TxUnstake TxType = 6
	// TxDTL executes native Decentralized Token Ledger operations.
	TxDTL TxType = 8
)

const (
	// `validatorUpdateAddPrefix` defines whether the related condition is satisfied.
	validatorUpdateAddPrefix = "add:"
	// `validatorUpdateActivatePrefix` reactivates a governance-suspended validator.
	validatorUpdateActivatePrefix = "activate:"
	// `validatorUpdateSuspendPrefix` marks an active validator INACTIVE without
	// permanently removing its identity or keys from registry history.
	validatorUpdateSuspendPrefix = "suspend:"
	// `validatorUpdateRemovePrefix` defines whether the related condition is satisfied.
	validatorUpdateRemovePrefix = "remove:"
)

type ValidatorUpdateCertSignature struct {
	// `SignerID` stores the value associated with this record.
	SignerID string `json:"signer_id"`
	// `SigHex` stores the value associated with this record.
	SigHex string `json:"sig_hex"`
}

type ValidatorUpdateCertificate struct {
	// `ParentRegistryHash` stores the digest used to identify or verify the related data.
	ParentRegistryHash string `json:"parent_registry_hash"`
	// `ProposalNonce` stores the value associated with this record.
	ProposalNonce uint64 `json:"proposal_nonce"`
	// `ExpiryHeight` stores the value associated with this record.
	ExpiryHeight uint64 `json:"expiry_height"`
	// `Signatures` stores the result produced by this operation.
	Signatures []ValidatorUpdateCertSignature `json:"signatures,omitempty"`
}

// parseValidatorUpdateTarget parses validator update target.
func parseValidatorUpdateTarget(to string) (action string, id string, ok bool) {
	if strings.HasPrefix(to, validatorUpdateAddPrefix) {
		id = strings.TrimPrefix(to, validatorUpdateAddPrefix)
		if id == "" {
			return "", "", false
		}
		return "add", id, true
	}
	if strings.HasPrefix(to, validatorUpdateActivatePrefix) {
		id = strings.TrimPrefix(to, validatorUpdateActivatePrefix)
		if id == "" {
			return "", "", false
		}
		return "activate", id, true
	}
	if strings.HasPrefix(to, validatorUpdateSuspendPrefix) {
		id = strings.TrimPrefix(to, validatorUpdateSuspendPrefix)
		if id == "" {
			return "", "", false
		}
		return "suspend", id, true
	}
	if strings.HasPrefix(to, validatorUpdateRemovePrefix) {
		id = strings.TrimPrefix(to, validatorUpdateRemovePrefix)
		if id == "" {
			return "", "", false
		}
		return "remove", id, true
	}
	return "", "", false
}

type Ledger struct {
	// `Balances` stores the value associated with this record.
	Balances map[string]int
	// `Nonces` stores the value associated with this record.
	Nonces map[string]int
	// `Stakes` stores the value associated with this record.
	Stakes map[string]StakeLock
	// ValidatorRewardWallets pins validator rewards to a deterministic wallet.
	// Key is normalized validator ID (uppercase), value is wallet address.
	ValidatorRewardWallets map[string]string `json:"validator_reward_wallets,omitempty"`
	// DTL stores native decentralized token ledger state.
	DTL *DTLState `json:"dtl,omitempty"`
	// UsedValidatorUpdateCerts tracks consumed validator-update certificate
	// message hashes so replay is rejected deterministically after restart/sync.
	UsedValidatorUpdateCerts map[string]uint64 `json:"used_validator_update_certs,omitempty"`
	// UsedBridgeEvents tracks consumed cross-chain event keys in consensus state.
	UsedBridgeEvents map[string]uint64 `json:"used_bridge_events,omitempty"`
}

// StakeLock tracks locked stake for a delegator -> validator pair.
type StakeLock struct {
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string `json:"validator_id"`
	// `ConsensusPubKey` stores the key used to access the related value.
	ConsensusPubKey string `json:"consensus_pubkey,omitempty"`
	// `Amount` stores the value associated with this record.
	Amount int `json:"amount"`
	// `LockedUntil` stores the synchronization state protecting shared data.
	LockedUntil uint64 `json:"locked_until_epoch"`
	// `Burned` stores the value associated with this record.
	Burned int `json:"burned_amount,omitempty"`
}

type Reward struct {
	// `Worker` stores the value associated with this record.
	Worker int
	// `Owner` stores the value associated with this record.
	Owner int
	// `User` stores the value associated with this record.
	User int
	// `Validators` stores whether the related condition is satisfied.
	Validators int
}
type Validator struct {
	// `Address` stores the address used by this operation.
	Address string `json:"address"`
	// `PubKey` stores the key used to access the related value.
	PubKey []byte `json:"pubkey"`
	// `Stake` stores the value associated with this record.
	Stake uint64 `json:"stake"`
	// `VotingPower` stores the value associated with this record.
	VotingPower uint64 `json:"voting_power"`
	// `Status` stores the value associated with this record.
	Status string `json:"status"`
	// `JoinHeight` stores the current position in the related collection.
	JoinHeight uint64 `json:"join_height"`
	// `ActivationHeight` stores the value associated with this record.
	ActivationHeight uint64 `json:"activation_height"`
}
type Task struct {
	// `TaskID` stores the value associated with this record.
	TaskID string
	// `Input` stores the current position in the related collection.
	Input int
}
type Receipt struct {
	// `TaskID` stores the value associated with this record.
	TaskID string
	// `input` stores the current position in the related collection.
	input int
	// `Output` stores the result produced by this operation.
	Output int
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string
}
type Block struct {
	// `Task` stores the value associated with this record.
	Task Task
	// `Result` stores the result produced by this operation.
	Result int

	// `Hash` stores the digest used to identify or verify the related data.
	Hash string

	// `Type` stores the value associated with this record.
	Type BlockType // 🔒 NOT string
	// `Signature` stores the value associated with this record.
	Signature []byte
	// `ID` stores the current position in the related collection.
	ID uint64
	// `Round` stores the value associated with this record.
	Round uint32
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `ExecutionResults` stores the value associated with this record.
	ExecutionResults []ExecutionResult
	// `Height` stores the value associated with this record.
	Height uint64
	// `Tasks` stores the value associated with this record.
	Tasks []Task
	// `Results` stores the result produced by this operation.
	Results []TaskResult
	// `ResultHash` stores the digest used to identify or verify the related data.
	ResultHash []byte
	// `Proposer` stores the value associated with this record.
	Proposer string
	// `Payload` stores the value associated with this record.
	Payload []byte // 👈 यही use होगा
	// `Transactions` stores the transaction data handled by this operation.
	Transactions []Transaction
	// `Receipts` stores the value associated with this record.
	Receipts []StateReceipt
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string
	// `Signatures` stores the result produced by this operation.
	Signatures []string // validator IDs
	// `Timestamp` stores the value associated with this record.
	Timestamp int64
	// `BlockTime` stores the block data handled by this operation.
	BlockTime LogicalClock `json:"block_time"`
	// `MempoolRoot` stores the digest used to identify or verify the related data.
	MempoolRoot string
	// `ReceiptRoot` stores the digest used to identify or verify the related data.
	ReceiptRoot string `json:"receipt_root,omitempty"`
	// ReceiptsRoot is the canonical plural execution commitment. ReceiptRoot is
	// retained as a wire-compatible alias for historical blocks and clients.
	ReceiptsRoot string `json:"receipts_root,omitempty"`
	// `EventRoot` commits ordered execution event logs.
	EventRoot string `json:"event_root,omitempty"`
	// `ExecutionHash` commits raw post-execution state independently of StateRoot.
	ExecutionHash string `json:"execution_hash,omitempty"`
	// `FeeRoot` commits ordered fee effects.
	FeeRoot string `json:"fee_root,omitempty"`
	// `DTLStateRoot` commits DTL-owned state independently.
	DTLStateRoot string `json:"dtl_state_root,omitempty"`
	// `DTLReceiptsRoot` commits DTL-only receipts independently.
	DTLReceiptsRoot string `json:"dtl_receipts_root,omitempty"`
	// `ProtocolVersion` selects deterministic execution semantics.
	ProtocolVersion uint32 `json:"protocol_version,omitempty"`
	// `FeatureBitmap` enables block-committed protocol features.
	FeatureBitmap uint64 `json:"feature_bitmap,omitempty"`
	// `DTLV2ActivationHeight` commits the exact deterministic V2 gate.
	DTLV2ActivationHeight uint64 `json:"dtl_v2_activation_height,omitempty"`
	// `ValidatorSetVersion` identifies the independent registry snapshot.
	ValidatorSetVersion uint64 `json:"validator_set_version,omitempty"`
	// `CommitteeHash` commits the ordered committee used by consensus.
	CommitteeHash string `json:"committee_hash,omitempty"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string // 🔐 REQUIRED
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// NextValidatorSetHash commits deterministic validator set for ID+1.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// NextValidatorSetRoot commits ordered validator leaves for ID+1 set.
	NextValidatorSetRoot string `json:"next_validator_set_root,omitempty"`
	// NextValidatorSetHeight is expected to be ID+1 when commitment-v2 is active.
	NextValidatorSetHeight uint64 `json:"next_validator_set_height,omitempty"`
	// ActivationHeight is an alias of NextValidatorSetHeight for commitment-v2.
	ActivationHeight uint64 `json:"activation_height,omitempty"`
	// Quorum policy metadata is committed with the block so RPC/explorer/wallet
	// clients can identify normal vs degraded recovery finality.
	ConsensusMode string `json:"consensus_mode,omitempty"`
	// `QuorumPolicyVersion` stores the value associated with this record.
	QuorumPolicyVersion string `json:"quorum_policy_version,omitempty"`
	// `ActiveReadyCount` stores the measured quantity used by this operation.
	ActiveReadyCount int `json:"active_ready_count,omitempty"`
	// `RequiredQuorum` stores the request data being processed.
	RequiredQuorum int `json:"required_quorum,omitempty"`
	// `StrictQuorum` stores the value associated with this record.
	StrictQuorum int `json:"strict_quorum,omitempty"`

	// Finality commitments bind finalized roots and validator-set commitments
	// into the block header hash. The certificate carries observable quorum
	// evidence for RPC/explorer/wallet clients.
	FinalizedEpoch uint64 `json:"finalized_epoch,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedStateRoot` stores the digest used to identify or verify the related data.
	FinalizedStateRoot string `json:"finalized_state_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash,omitempty"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `PreviousEpochAnchorHash` stores the digest used to identify or verify the related data.
	PreviousEpochAnchorHash string `json:"previous_epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `FinalityCertificate` stores the value associated with this record.
	FinalityCertificate *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
}

// canonicalActivationHeight returns canonical activation height.
func canonicalActivationHeight(nextValidatorSetHeight uint64, activationHeight uint64) uint64 {
	if activationHeight > 0 {
		return activationHeight
	}
	return nextValidatorSetHeight
}

// blockActivationHeight implements the block activation height helper.
func blockActivationHeight(block Block) uint64 {
	return canonicalActivationHeight(block.NextValidatorSetHeight, block.ActivationHeight)
}

// snapshotActivationHeight implements the snapshot activation height helper.
func snapshotActivationHeight(snapshot *StateSnapshot) uint64 {
	if snapshot == nil {
		return 0
	}
	return canonicalActivationHeight(snapshot.NextValidatorSetHeight, snapshot.ActivationHeight)
}

// peerHelloActivationHeight implements the peer hello activation height helper.
func peerHelloActivationHeight(hello PeerHello) uint64 {
	return canonicalActivationHeight(hello.ValidatorSetHeight, hello.ActivationHeight)
}

// validatorAnnouncementActivationHeight implements the validator announcement activation height helper.
func validatorAnnouncementActivationHeight(ann ValidatorAnnouncement) uint64 {
	return canonicalActivationHeight(ann.ValidatorSetHeight, ann.ActivationHeight)
}

type TaskResult struct {
	// `TaskID` stores the value associated with this record.
	TaskID string // task identifier
	// `Output` stores the result produced by this operation.
	Output int // computed output
	// `ResultHash` stores the digest used to identify or verify the related data.
	ResultHash string // hash(Output)
	// `ExecutorID` stores the value associated with this record.
	ExecutorID string // who computed
	// `Signature` stores the value associated with this record.
	Signature []byte // executor signature
	// `ExecutedAt` stores the value associated with this record.
	ExecutedAt int64 // unix time
	// `ExecutionTime` stores the value associated with this record.
	ExecutionTime int64 // ms (optional metrics)

	// `Hash` stores the digest used to identify or verify the related data.
	Hash []byte
}

type MintResult struct {
	// `To` stores the value associated with this record.
	To string
	// `Amount` stores the value associated with this record.
	Amount int64
	// `NewTotalSupply` stores the value associated with this record.
	NewTotalSupply int64
}

type ExecutionResult struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `Round` stores the value associated with this record.
	Round uint32 `json:"round,omitempty"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `Signer` stores the value associated with this record.
	Signer string
	// `ResultHash` stores the digest used to identify or verify the related data.
	ResultHash string
	// `TxMerkle` stores the transaction data handled by this operation.
	TxMerkle string
	// `ExecutionResultHash` stores the digest used to identify or verify the related data.
	ExecutionResultHash string `json:"execution_result_hash,omitempty"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"sig,omitempty"`
}

type ExecutionResultMsg struct {
	// `HeightHint` stores the value associated with this record.
	HeightHint uint64 `json:"height_hint"`
	// `RoundHint` stores the value associated with this record.
	RoundHint uint32 `json:"round_hint,omitempty"`
	// `BlockHashHint` stores the block data handled by this operation.
	BlockHashHint string `json:"block_hash_hint,omitempty"`
	// `Block` stores the synchronization state protecting shared data.
	Block *Block `json:"block,omitempty"`
	// `SigVersion` stores the value associated with this record.
	SigVersion uint8 `json:"sig_v,omitempty"`
	// `ExecHash` stores the digest used to identify or verify the related data.
	ExecHash string `json:"exec_hash"`
	// `TxMerkle` stores the transaction data handled by this operation.
	TxMerkle string `json:"tx_merkle"`
	// `ExecutionResultHash` stores the digest used to identify or verify the related data.
	ExecutionResultHash string `json:"execution_result_hash,omitempty"`
	// `Signer` stores the value associated with this record.
	Signer string `json:"signer"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"sig"`
}

type CommitMsg struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string `json:"hash"`
	// `ExecHash` stores the digest used to identify or verify the related data.
	ExecHash string `json:"exec_hash,omitempty"`
	// `TxMerkle` stores the transaction data handled by this operation.
	TxMerkle string `json:"tx_merkle,omitempty"`
	// ExecutionCommitmentHash upgrades the commit signature from a state-root
	// vote to an explicit vote over the complete execution commitment tuple.
	ExecutionCommitmentHash string `json:"execution_commitment_hash,omitempty"`
	// `Block` stores the synchronization state protecting shared data.
	Block Block `json:"block"`
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"sig,omitempty"`
}

type FraudProof struct {
	// `BlockID` stores the block data handled by this operation.
	BlockID uint64
	// `Proposer` stores the value associated with this record.
	Proposer string
	// `ExpectedHash` stores the digest used to identify or verify the related data.
	ExpectedHash []byte
	// `ActualHash` stores the digest used to identify or verify the related data.
	ActualHash []byte
	// `Reporter` stores the value associated with this record.
	Reporter string
}
type BlockType uint8

const (
	// `BlockTypeGenesis` defines the block data handled by this operation.
	BlockTypeGenesis BlockType = iota
	// `BlockTypeWork` defines the block data handled by this operation.
	BlockTypeWork // normal tx execution
	// `BlockTypeTime` defines the block data handled by this operation.
	BlockTypeTime // empty / heartbeat block
	// `BlockTypeTask` defines the block data handled by this operation.
	BlockTypeTask // 🔥 execution / compute block
	// `BlockTypeReceipt` defines the block data handled by this operation.
	BlockTypeReceipt // execution receipt block (ERB)
)

const (
	// `BlockTypeWorkk` defines the block data handled by this operation.
	BlockTypeWorkk = "work"
	// `BlockTypeTimek` defines the block data handled by this operation.
	BlockTypeTimek = "time"
)

type Blockchain struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `Blocks` stores the block data handled by this operation.
	Blocks []Block
}
type Wallet struct {
	// `PublicKey` stores the key used to access the related value.
	PublicKey ed25519.PublicKey
	// `Address` stores the address used by this operation.
	Address string
}

type GenesisValidatorKey struct {
	// `Address` stores the address used by this operation.
	Address string `json:"address"`
	// `PublicKey` stores the key used to access the related value.
	PublicKey string `json:"publicKey"`
}

type SecureWallet struct {
	// `Address` stores the address used by this operation.
	Address string `json:"address"`
	// `PublicKey` stores the key used to access the related value.
	PublicKey string `json:"publicKey"`
	// `Crypto` stores the value associated with this record.
	Crypto EncryptedKey `json:"crypto"`
	// `HD` stores the value associated with this record.
	HD *HDWalletMeta `json:"hd,omitempty"`
}

type HDWalletMeta struct {
	// `Scheme` stores the value associated with this record.
	Scheme string `json:"scheme,omitempty"`
	// `Path` stores the value associated with this record.
	Path string `json:"path,omitempty"`
	// `Purpose` stores the value associated with this record.
	Purpose uint32 `json:"purpose,omitempty"`
	// `CoinType` stores the value associated with this record.
	CoinType uint32 `json:"coin_type,omitempty"`
	// `Account` stores the measured quantity used by this operation.
	Account uint32 `json:"account,omitempty"`
	// `Change` stores the value associated with this record.
	Change uint32 `json:"change,omitempty"`
	// `Index` stores the current position in the related collection.
	Index uint32 `json:"index,omitempty"`
}
type EncryptedKey struct {
	// `Ciphertext` stores the value associated with this record.
	Ciphertext string `json:"ciphertext"`
	// `Nonce` stores the value associated with this record.
	Nonce string `json:"nonce"`
	// `Salt` stores the value associated with this record.
	Salt string `json:"salt"`
	// `Version` stores the value associated with this record.
	Version int `json:"version,omitempty"`
	// `KDF` stores the value associated with this record.
	KDF string `json:"kdf,omitempty"`
	// Argon2id parameters (v2 encryption format).
	Argon2Time uint32 `json:"argon2_time,omitempty"`
	// `Argon2MemoryKiB` stores the value associated with this record.
	Argon2MemoryKiB uint32 `json:"argon2_memory_kib,omitempty"`
	// `Argon2Threads` stores the value associated with this record.
	Argon2Threads uint8 `json:"argon2_threads,omitempty"`
}
type validatorAddrState struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `addr` stores the address used by this operation.
	addr map[string]string // validatorID -> p2p addr
}

// `ValidatorAddr` stores whether the related condition is satisfied.
var ValidatorAddr = &validatorAddrState{
	addr: make(map[string]string),
}
