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
	NodeID             string
	Addr               string
	LastSeen           time.Time
	LastConnectAttempt time.Time
	Alive              bool
	Failures           int
	Height             uint64
	HelloSent          bool
	Connecting         bool
	Conn               net.Conn
}
type StateSnapshot struct {
	Version         uint32          `json:"version"`
	Height          uint64          `json:"height"`
	BlockHash       string          `json:"block_hash"`
	StateRoot       string          `json:"state_root"`
	StateMerkleRoot string          `json:"state_merkle_root,omitempty"`
	LedgerHash      string          `json:"ledger_hash"`
	LedgerStage     string          `json:"ledger_stage,omitempty"`
	GenesisHash     string          `json:"genesis_hash,omitempty"`
	PrevHash        string          `json:"prev_hash"`
	Ledger          Ledger          `json:"ledger"`
	Validators      map[string]bool `json:"validators"`
	// ValidatorSetHash freezes the deterministic validator-set hash at snapshot height.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// ValidatorSetSource records where the authoritative validator set came from.
	ValidatorSetSource string `json:"validator_set_source,omitempty"`
	// ValidatorSetRoot commits ordered validator leaves (stake + canonical validator hash).
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// Pending validator transitions are required for deterministic set-hash
	// reconstruction after late-join snapshot restore.
	PendingValidators        map[string]uint64 `json:"pending_validators,omitempty"`
	PendingValidatorRemovals map[string]uint64 `json:"pending_validator_removals,omitempty"`
	ValidatorSetHeight       uint64            `json:"validator_set_height,omitempty"`
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
	StateValidators       map[string]Validator `json:"state_validators,omitempty"`
	ValidatorRegistryHash string               `json:"validator_registry_hash,omitempty"`
	// CheckpointProof stores validator signatures over deterministic snapshot checkpoint bytes.
	CheckpointProof map[string]string `json:"checkpoint_proof,omitempty"`
	// CheckpointHeight pins deterministic proof anchor (for quorum accumulation).
	CheckpointHeight uint64 `json:"checkpoint_height,omitempty"`
	// CheckpointDomain is the signature domain for checkpoint proof bytes.
	CheckpointDomain string `json:"checkpoint_domain,omitempty"`
	// Finality anchors bind snapshots to irreversible epoch checkpoints.
	FinalizedEpoch            uint64                     `json:"finalized_epoch,omitempty"`
	FinalizedHeight           uint64                     `json:"finalized_height,omitempty"`
	FinalizedHash             string                     `json:"finalized_hash,omitempty"`
	FinalizedStateRoot        string                     `json:"finalized_state_root,omitempty"`
	FinalizedValidatorSetHash string                     `json:"finalized_validator_set_hash,omitempty"`
	FinalizedValidatorSetRoot string                     `json:"finalized_validator_set_root,omitempty"`
	EpochAnchorHash           string                     `json:"epoch_anchor_hash,omitempty"`
	PreviousEpochAnchorHash   string                     `json:"previous_epoch_anchor_hash,omitempty"`
	FinalityRoot              string                     `json:"finality_root,omitempty"`
	FinalityCertificate       *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
	// SnapshotHash is the canonical deterministic hash of snapshot metadata.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

// ExecPoolSnapshot carries execution-hash convergence for late joiners.
// Hashes map execHash -> signers, TxMerkle map execHash -> txMerkle.
type ExecPoolSnapshot struct {
	Epoch       uint64              `json:"epoch"`
	ProposalKey string              `json:"proposal_key,omitempty"`
	Hashes      map[string][]string `json:"hashes"`
	TxMerkle    map[string]string   `json:"tx_merkle"`
}

// LogicalClock tracks deterministic chain time (no wall clock).
type LogicalClock struct {
	Epoch uint64 `json:"epoch"`
	Tick  uint64 `json:"tick"`
}

const SnapshotVersion = 4
const snapshotLedgerStageExecution = "execution"

// SnapshotRetention keeps the last N finalized snapshots available for joiners.
const SnapshotRetention = 1000

var (
	DebugConsensus                           = true
	DebugNet                                 = true
	DebugSync                                = false
	ExecutionDeterminismGuardEnabled         = true
	ResultGossipOnly                         = true
	DisableDHT                               = false
	BlockPublicPeers                         = false
	PeerDiversityEnabled                     = true
	PeerDiversityIPv4Prefix                  = 24
	PeerDiversityIPv6Prefix                  = 64
	PeerDiversityMaxPerSubnet                = 8
	PeerDiversityMaxOutboundPerSubnet        = 4
	PeerDiversityMaxPerASN                   = 12
	PeerDiversityMaxOutboundPerASN           = 6
	PeerMemoryQuotaBytes                     = 16 << 20
	PeerBandwidthQuotaBytesPerMinute         = 64 << 20
	PeerMempoolTxPerMinute                   = 240
	PeerBlockRequestsPerMinute               = 120
	PeerConnectionFloodMaxPerWindow          = 20
	PeerDiscoveryMaxAddrs                    = 16
	PeerResourceWindowDuration               = time.Minute
	EnableMDNS                               = true
	SelfHealEnabled                          = false
	SelfHealInterval                         = 15 * time.Second
	SelfHealMinPeers                         = 4
	SelfHealStallSeconds              uint64 = 45
	EnableAutoNAT                            = false
	EnableRelay                              = false
	EnableHolePunch                          = false
)

const (
	MaxPeers                = 50
	MaxInboundPeers         = 30
	MaxOutboundPeers        = 20
	MaxConnections          = 100
	MaxPendingConn          = 10
	MaxPeerOutboundQueue    = 512
	MaxValidateQueue        = 128
	MaxValidateWorkers      = 8
	MaxTxPerSecondPerSender = 20
	GoroutineWarnThreshold  = 6000
	ExecResultsMaxEntries   = 20000
	PendingBlocksMaxEntries = 2000
	QueuedExecVotesMaxKeys  = 2000
	AcceptedProposalMaxKeys = 2000
	ExecBroadcastedMaxEpoch = 1024
	ExecSignerSeenMaxEpoch  = 1024
	ExecBroadcastedByValMax = 1024
	ExecVoteReplayMaxKeys   = 5000
	ExecMismatchTrackMax    = 2048
)

const (
	TxLimiterIdleTTL      = 10 * time.Minute
	GoroutineWarnInterval = 10 * time.Second
	MapStatsInterval      = 60 * time.Second
)

// Validator-set updates activate after N finalized blocks (decision height + delay).
var ValidatorSetActivationDelay uint64 = 5
var ValidatorSetActivationModelV2Height uint64 = 0
var ValidatorSetCommitmentV2Height uint64 = 0
var ValidatorSetHashV3Height uint64 = 0

const (
	activationDelayModelV1DoubleHold = "v1_double_hold"
	activationDelayModelV2Single     = "v2_single_phase"
)

func validatorSetActivationDelayBlocks() uint64 {
	if ValidatorSetActivationDelay == 0 {
		return 5
	}
	return ValidatorSetActivationDelay
}

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

func alignHeightToCheckpointBoundary(height uint64, interval uint64) uint64 {
	if height == 0 {
		return 0
	}
	if interval <= 1 {
		return height
	}
	rem := height % interval
	if rem == 0 {
		return height
	}
	step := interval - rem
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
	hold := validatorSetTransitionHoldBlocks()
	if hold == 0 {
		return scheduledHeight
	}
	maxU64 := ^uint64(0)
	if scheduledHeight > maxU64-hold {
		return maxU64
	}
	return scheduledHeight + hold
}

func validatorSetActivationDelayModelAtHeight(evalHeight uint64) string {
	switchHeight := ValidatorSetActivationModelV2Height
	if switchHeight > 0 && evalHeight >= switchHeight {
		return activationDelayModelV2Single
	}
	return activationDelayModelV1DoubleHold
}

func effectiveValidatorSetActivationHeightAt(scheduledHeight uint64, evalHeight uint64) uint64 {
	if scheduledHeight == 0 {
		return 0
	}
	effective := uint64(0)
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
	// Mainnet-safe deterministic activation: apply validator-set transitions
	// only at checkpoint boundaries across the network.
	interval := SyncCheckpointIntervalBlocks
	if interval == 0 {
		interval = 32
	}
	return alignHeightToCheckpointBoundary(effective, interval)
}

func validatorSetTransitionUsesStrictParentCommitment(scheduledHeight uint64) bool {
	if scheduledHeight == 0 {
		return false
	}
	return validatorSetCommitmentV2EnabledAt(scheduledHeight)
}

func validatorSetTransitionActivationHeightAt(scheduledHeight uint64, evalHeight uint64) uint64 {
	if scheduledHeight == 0 {
		return 0
	}
	if validatorSetTransitionUsesStrictParentCommitment(scheduledHeight) {
		maxU64 := ^uint64(0)
		if scheduledHeight == maxU64 {
			return maxU64
		}
		return scheduledHeight + 1
	}
	return effectiveValidatorSetActivationHeightAt(scheduledHeight, evalHeight)
}

func validatorSetTransitionVisibleInChildSetAt(scheduledHeight uint64, childHeight uint64) bool {
	if scheduledHeight == 0 || childHeight == 0 {
		return false
	}
	parentHeight := childHeight - 1
	if validatorSetTransitionUsesStrictParentCommitment(scheduledHeight) {
		return scheduledHeight <= parentHeight
	}
	effectiveActivation := effectiveValidatorSetActivationHeightAt(scheduledHeight, childHeight)
	return effectiveActivation > 0 && effectiveActivation <= parentHeight
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

func minActiveValidatorsFloor() int {
	if ValidatorMinActiveSet <= 0 {
		return 1
	}
	return ValidatorMinActiveSet
}

// ValidatorBannedList contains validator IDs that are explicitly excluded from selection.
var ValidatorBannedList []string
var validatorBannedSet = struct {
	mu sync.RWMutex
	m  map[string]struct{}
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

// WeakSubjectivityDepth rejects blocks that are too far behind local finalized
// history to mitigate long-range rewrite attacks. 0 disables the guard.
var WeakSubjectivityDepth uint64 = 2048

// MaxFutureBlockGap bounds queued future blocks to reduce eclipse/sybil DoS
// memory pressure from very large height jumps.
var MaxFutureBlockGap uint64 = 256

// EnforceDeterministicTxOrder rejects blocks where tx order deviates from
// canonical fee/expiry/id sorting, reducing proposer-side MEV reordering.
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
var SyncFastBlockSyncMaxBlocks uint64 = 256
var SyncRestartSnapshotGraceBlocks uint64 = 256
var SyncDirectGossipMaxBlocks uint64 = 128
var SyncRangeFetchMaxBlocks uint64 = 50000
var SyncBlockRangeReplicationFactor = 2
var SyncSnapshotDeltaMaxBlocks uint64 = 10000000
var SyncDeltaReplayMaxBlocks uint64 = 50000
var SyncDeltaReplayBatchBlocks uint64 = 1024
var SyncDeltaReplayVerifyWorkers = 8
var SyncSnapshotChunkSizeBytes uint64 = 1024 * 1024
var SyncSnapshotParallelChunks = 8
var SyncSnapshotChunkReplicationFactor = 2
var SyncStallSeconds uint64 = 180
var SyncPeerTimeoutSeconds uint64 = 15
var SyncHeaderBatchSize uint64 = 256
var SyncHeaderCommonAncestorDepth uint64 = 2048
var SyncHistoryMode = "background"
var SyncFreshJoinFallbackBlockReplayEnabled = false
var SyncSnapshotPublishNewNodeThresholdBlocks uint64 = 50
var SyncSnapshotPublishLagThresholdBlocks uint64 = 20
var SyncSnapshotPublishReannounceCooldownSeconds uint64 = 15
var SyncTrustedSnapshotRequireCheckpointProof = true
var SyncSnapshotAnchorTimeoutSeconds uint64 = 10
var SyncSnapshotAnchorMaxRetries uint64 = 6
var SyncCheckpointIntervalBlocks uint64 = 32
var SyncSnapshotCheckpointDomain = "MSC_SNAPSHOT_V1"
var SyncSnapshotCheckpointV2Height uint64 = 0
var SyncSnapshotSessionTTLSeconds uint64 = 0
var SyncSnapshotQuorumApplyWatchdogSeconds uint64 = 20
var SyncSnapshotSessionResetWatchdogSeconds uint64 = 60
var SyncSnapshotInvalidProofQuarantineAfter uint64 = 3
var SyncSnapshotReplicationMinCopies = 3
var SyncSnapshotWarmupBlocks uint64 = 5
var SyncSnapshotWarmupSeconds uint64 = 10

var StorageEpochLengthBlocks uint64 = 100
var StorageValidatorRetainedEpochs uint64 = 10
var StorageValidatorRollbackWindowBlocks uint64 = 256
var StorageValidatorSnapshotKeepLast uint64 = 3
var StorageValidatorRecentBlockWindow uint64 = 2048
var StorageFullNodeHistoryBlocks uint64 = 5256000
var StorageHourlySnapshotRetain uint64 = 24
var StorageDailySnapshotRetain uint64 = 30
var StorageWeeklySnapshotRetain uint64 = 12
var StorageMonthlySnapshotRetain uint64 = 24
var StorageHourlySnapshotIntervalBlocks uint64 = 3600
var StorageColdExportEnabled = true
var StorageColdExportCompression = "zstd"
var StorageParallelGCWorkers uint64 = 4
var StorageStateRentEnabled = false
var StorageStateRentArchiveInactiveAfterEpochs uint64 = 0
var StorageStateLayoutMode = "merkle"

const (
	SyncHistoryModeNone       = "none"
	SyncHistoryModeBackground = "background"
	SyncHistoryModeArchive    = "archive_full"
)

// Hybrid validator liveness controls.
var ValidatorLivenessHeartbeatTTLSeconds uint64 = 25
var ValidatorLivenessGraceSeconds uint64 = 10
var ValidatorLivenessMaxHeightDriftBlocks uint64 = 8

// Delayed rejoin controls for previously offline validators.
var ValidatorRejoinRequiredHeartbeats uint16 = 3
var ValidatorRejoinRequiredSignedBlocks uint64 = 1
var ValidatorRejoinWindowBlocks uint64 = 16

// Validator startup safety controls.
var ValidatorFailOnKeyUnavailable = true
var ValidatorAllowIdentityRotationOnExistingChain = false
var ValidatorRequiredKeyFingerprint = ""
var ValidatorKeyBackupRequired = true
var ValidatorKeyBackupDir = "secure-backups"
var ValidatorKeyBackupMaxAgeHours uint64 = 24
var ValidatorKeyRestoreAllowedOnMissing = true
var ValidatorAllowEnvPasswordInProduction = false
var ValidatorCoreEnvPasswordAllowed = false
var ValidatorCorePasswordFile = ""
var ValidatorPasswordMode = "file_or_prompt"

// Genesis runtime locks are set from production genesis during node startup.
// A frozen genesis validator set may observe candidates, but it must not admit
// them into the consensus set without an explicit protocol/governance change.
var GenesisRuntimeLocked = false
var GenesisValidatorSetFrozen = false
var GenesisFrozenValidatorSetSize = 0

// Round-based proposer failover controls.
var ProposerRoundTimeout = 15 * time.Second

// Zero keeps round recovery uncapped unless an operator explicitly sets a limit.
var ProposerRoundMax uint32 = 0
var ProposerRoundMaxSkew uint32 = 1
var ConsensusProposalDeadlineGuard = 200 * time.Millisecond

// Transition barrier relax controls during sustained stalls.
var TransitionBarrierRelaxTimeout = 60 * time.Second
var TransitionBarrierMaxDrop = 1

const (
	transitionBarrierRetryModeHybrid   = "hybrid"
	transitionBarrierRetryModePerBlock = "per_block"
)

var TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock

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
var ConsensusInvalidProposerQuarantineAfter = 4
var ConsensusExecMismatchQuarantineAfter = 3
var ConsensusExecMismatchSlashAfter = 5
var ConsensusProposeRequiresSyncReady = true
var ConsensusCorePendingExcludedFromProposer = true
var ConsensusCoreActivationEffectiveHeightBuffer uint64 = 64
var ConsensusPostBlockSafeModeEnabled = true
var ConsensusPostBlockSafeModeMin = 5 * time.Second
var ConsensusPostBlockSafeModeMax = 8 * time.Second
var ConsensusPostBlockSafeModeHistoryBlocks uint64 = 32
var ConsensusPostBlockSafeModeLiveQuorumBPS uint64 = 6700

// Signed core registry controls.
var CoreRegistryPath = "core_validators.json"
var CoreRegistryEnforcementMode = "warn"
var CoreRegistryMinSignatures = 0
var CoreRegistryReloadSeconds uint64 = 10

// Peer identity auto-heal persistence controls.
var PersistPeerIDRefresh = true

// Localhost dial-refused pruning controls.
var PruneLocalhostOnRefused = true
var PruneLocalhostRefusedFailures = 3

var MapStatsEnabled = true

var FailedHeights = make(map[uint64]bool)

type ConsensusPhase uint8

// Add in global variables
var (
	connectionLimiter      = rate.NewLimiter(10, 20) // 10 conn/sec, burst 20
	messageLimiter         = make(map[string]*rate.Limiter)
	messageLimiterLastSeen = make(map[string]time.Time)
	limiterMu              sync.Mutex
)

var ValidatorAddrBook = struct {
	mu sync.RWMutex
	m  map[string]string // validatorID -> p2pAddr
}{
	m: make(map[string]string),
}

type ConsensusState struct {
	mu                sync.Mutex
	Height            uint64
	Votes             map[uint64]map[string]BlockVote
	Proposals         map[uint64]Block // ✅ FIXED (Block, not string)
	LastCleanedHeight uint64
	Round             uint32
	Phase             ConsensusPhase
	RoundStart        time.Time
	Timeout           time.Duration
	LastFinalized     uint64
	Validators        []string
	// LockedBlock is the active-height consensus lock keyed by block hash.
	LockedBlock string
	// ExecVotes is the active-height execution vote view keyed by block hash -> validator -> vote.
	ExecVotes map[string]map[string]ExecutionResult
	// Committed reports whether the current active height has observed a commit barrier.
	Committed bool
	// LockedBlockHash is kept for compatibility with older call sites.
	LockedBlockHash    string
	LockedRound        uint32
	LastProposedHeight uint64
	LastProposedRound  uint32
	Paused             bool
	Syncing            bool
	SyncTarget         uint64
	syncInFlight       bool

	// MODEL-2 / MODEL-3
	Executed  bool
	Finalized bool
	BlockHash string
}

// =====================================================
// 🔐 PROPOSAL VOTE REGISTRY (GLOBAL, DETERMINISTIC)
// =====================================================
var ProposalVotes = struct {
	sync.Mutex
	Votes map[int]map[string][]byte // height -> validator -> signature
}{
	Votes: make(map[int]map[string][]byte),
}

const (
	PhasePropose ConsensusPhase = iota
	PhaseVote
	PhaseFinalize
)
const (
	rateWindow    = 10 * time.Second
	maxRequestsIP = 20
)

type ConsensusRound struct {
	Height   uint64
	Leader   string
	Block    *Block
	Votes    map[string]BlockVote
	Phase    ConsensusPhase
	Deadline int64
}
type EvidenceSummary struct {
	Witnesses int64
	LastSeen  int64
}

var pendingSpendMu sync.Mutex
var PendingSpend = map[string]int{}

const CensorshipSlashThreshold = 2.5

var CensorshipEvidencePool = map[EvidenceKey][]CensorshipEvidence{}

const (
	MinCensorshipWitnesses = 2  // stake-less threshold
	MaxEvidencePerHeight   = 20 // anti-spam
)

type GossipMessage struct {
	Type string // "block" | "censorship"
	Data []byte
}

const (
	EvidenceTTLBlocks       = 200  // evidence expires after 200 blocks
	EvidenceDecayFactor     = 0.85 // exponential decay per window
	EvidenceDecayWindow     = 20   // blocks per decay step
	MaxEvidencePerValidator = 50
)

const (
	TickExec     uint64 = 1
	TickVote     uint64 = 2
	TickFinalize uint64 = 3
)

type Misbehavior struct {
	Validator string
	Reason    string
	Height    int
	BlockHash string
	Timestamp int64
}

type ForkScore struct {
	Block Block

	MempoolDistance int
	SignatureWeight int

	ExecutionScore  int
	StateScore      int
	CensorshipScore int
	FreshnessScore  int
	TotalScore      int

	BlockHash string
	Timestamp int64
}
type CensorshipEvidence struct {
	Height      uint64 // Changed from int to uint64
	BlockHash   string
	Leader      string
	TxID        string
	TxFee       int
	MempoolRoot string
	Observer    string
	ObserverSig []byte
	ObservedAt  uint64 // Changed from int to uint64

	Fee int

	Timestamp int64
}

var GlobalMempoolSnapshot []Transaction

const MsgCensorshipProof = "censorship_proof"

var CensorshipScore = map[string]int{}

type CensorshipProof struct {
	// --- Block context ---
	BlockHeight uint64 // Changed from int to uint64
	BlockHash   string
	Proposer    string
	MempoolRoot string
	// --- Transaction evidence ---
	TxID      string
	TxFee     int
	TxPayload []byte // canonical payload hash source
	// --- Reporter ---
	Reporter  string
	Signature []byte
	Timestamp int64
}
type TxAbuseRecord struct {
	Attempts   int
	BannedTill time.Time
	Permanent  bool
}
type PeerHello struct {
	ChainID              string `json:"chain_id"`
	GenesisHash          string `json:"genesis_hash"`
	Version              string `json:"version"`
	ConsensusHash        string `json:"consensus_hash"`
	Role                 string `json:"role,omitempty"`
	ValidatorID          string `json:"validator_id"`
	ValidatorPubKey      string `json:"validator_pubkey,omitempty"`
	P2PAddr              string `json:"p2p_addr"`
	ValidatorSetHash     string `json:"validator_set_hash"`
	ValidatorSetHeight   uint64 `json:"validator_set_height,omitempty"`
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	ActivationHeight     uint64 `json:"activation_height,omitempty"`
	Height               uint64 `json:"height"`
	TipHash              string `json:"tip_hash,omitempty"`
	Timestamp            int64  `json:"timestamp,omitempty"`
	Nonce                string `json:"nonce,omitempty"`
	SignatureHex         string `json:"signature_hex,omitempty"`
}

type consensusParamsSnapshot struct {
	ChainID                               string   `json:"chain_id"`
	GenesisHash                           string   `json:"genesis_hash"`
	Version                               string   `json:"version"`
	DynamicSelection                      bool     `json:"dynamic_selection"`
	DeterministicSelection                bool     `json:"deterministic_selection"`
	ActiveSetSize                         int      `json:"active_set_size"`
	ActiveSetMode                         string   `json:"active_set_mode"`
	MaxActiveCommittee                    int      `json:"max_active_committee"`
	AdaptiveCommitteeLogMultiplier        int      `json:"adaptive_committee_log_multiplier"`
	CommitteeRotationBlocks               uint64   `json:"committee_rotation_blocks"`
	SelectionActivityWindowBlocks         uint64   `json:"selection_activity_window_blocks"`
	SelectionMinSignedBlocks              uint64   `json:"selection_min_signed_blocks"`
	OnboardingGraceBlocks                 uint64   `json:"onboarding_grace_blocks"`
	OnboardingMaxNewSlots                 int      `json:"onboarding_max_new_slots"`
	OnboardingStrictActivation            bool     `json:"onboarding_strict_activation"`
	OnboardingBootstrapLaneEnabled        bool     `json:"onboarding_bootstrap_lane_enabled"`
	OnboardingBootstrapMaxNewSlots        int      `json:"onboarding_bootstrap_max_new_slots"`
	OnboardingBootstrapRequireStake       bool     `json:"onboarding_bootstrap_require_stake"`
	OnboardingBootstrapRequireNotJailed   bool     `json:"onboarding_bootstrap_require_not_jailed"`
	ValidatorSetAutohealMode              string   `json:"validator_set_autoheal_mode"`
	ValidatorSetAutohealTrustedOnly       bool     `json:"validator_set_autoheal_trusted_only_on_mismatch"`
	ValidatorSetAutohealNearTipForceAfter int      `json:"validator_set_autoheal_near_tip_force_after"`
	ValidatorSetAutohealPauseSeconds      uint64   `json:"validator_set_autoheal_pause_seconds"`
	HeartbeatScope                        string   `json:"heartbeat_scope"`
	MinStake                              int64    `json:"min_stake"`
	StakeCapPct                           string   `json:"stake_cap_pct"`
	UptimeWindow                          uint64   `json:"uptime_window"`
	LivenessMaxHeightDriftBlocks          uint64   `json:"liveness_max_height_drift_blocks"`
	ReputationRecoveryThreshold           string   `json:"reputation_recovery_threshold"`
	LongUptimeThreshold                   string   `json:"long_uptime_threshold"`
	RequireStake                          bool     `json:"require_stake"`
	CoreStakeExempt                       bool     `json:"core_stake_exempt"`
	CandidateObservationEpochs            uint64   `json:"candidate_observation_epochs"`
	CandidateDCSMin                       string   `json:"candidate_dcs_min"`
	CandidateUptimeMin                    string   `json:"candidate_uptime_min"`
	CandidateDiversityPctMin              string   `json:"candidate_diversity_pct_min"`
	CandidateDiversityEpochs              uint64   `json:"candidate_diversity_epochs"`
	MaxPromotionsPerWindow                int      `json:"max_promotions_per_window"`
	PromotionWindowSize                   uint64   `json:"promotion_window_size"`
	CandidateSRPMin                       string   `json:"candidate_srp_min"`
	CandidateSRPAlpha                     string   `json:"candidate_srp_alpha"`
	CandidateWarningLimit                 int      `json:"candidate_warning_limit"`
	TestingRelaxedPromotion               bool     `json:"testing_relaxed_promotion"`
	ValidatorSetActivationDelay           uint64   `json:"validator_set_activation_delay"`
	ValidatorSetActivationModelV2Height   uint64   `json:"validator_set_activation_model_v2_height"`
	ValidatorSetCommitmentV2Height        uint64   `json:"validator_set_commitment_v2_height"`
	ValidatorSetHashV3Height              uint64   `json:"validator_set_hash_v3_height"`
	ValidatorInactiveBlocks               uint64   `json:"validator_inactive_blocks"`
	ValidatorSetRotationWindow            uint64   `json:"validator_set_rotation_window"`
	ValidatorMinActiveSet                 int      `json:"validator_min_active_set"`
	ValidatorInactivityPenaltyEnabled     bool     `json:"validator_inactivity_penalty_enabled"`
	ValidatorInactivityPenaltyBurnBPS     uint64   `json:"validator_inactivity_penalty_burn_bps"`
	ValidatorInactivityPenaltyJail        uint64   `json:"validator_inactivity_penalty_jail_blocks"`
	ValidatorInactivityPenaltyCooldown    uint64   `json:"validator_inactivity_penalty_cooldown_blocks"`
	ValidatorInactivePermanent            bool     `json:"validator_inactive_permanent"`
	PostBlockSafeModeEnabled              bool     `json:"post_block_safe_mode_enabled"`
	PostBlockSafeModeMinMs                uint64   `json:"post_block_safe_mode_min_ms"`
	PostBlockSafeModeMaxMs                uint64   `json:"post_block_safe_mode_max_ms"`
	PostBlockSafeModeHistoryBlocks        uint64   `json:"post_block_safe_mode_history_blocks"`
	PostBlockSafeModeLiveQuorumBPS        uint64   `json:"post_block_safe_mode_live_quorum_bps"`
	TransitionBarrierRetryMode            string   `json:"transition_barrier_retry_mode"`
	BannedValidators                      []string `json:"banned_validators"`
}

func consensusParamsHash() string {
	snap := consensusParamsSnapshot{
		ChainID:                               ChainID,
		GenesisHash:                           GenesisHash,
		Version:                               Version,
		DynamicSelection:                      DynamicValidatorSelectionEnabled,
		DeterministicSelection:                DeterministicValidatorSelection,
		ActiveSetSize:                         ValidatorActiveSetSize,
		ActiveSetMode:                         normalizeActiveSetMode(ValidatorActiveSetMode),
		MaxActiveCommittee:                    ValidatorMaxActiveCommittee,
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
	raw, _ := json.Marshal(snap)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 8, 64)
}

func normalizeValidatorID(id string) string {
	return strings.ToUpper(strings.TrimSpace(id))
}

func setValidatorBannedValidators(list []string) {
	validatorBannedSet.mu.Lock()
	defer validatorBannedSet.mu.Unlock()

	validatorBannedSet.m = make(map[string]struct{})
	ValidatorBannedList = ValidatorBannedList[:0]

	for _, raw := range list {
		id := normalizeValidatorID(raw)
		if id == "" {
			continue
		}
		if _, exists := validatorBannedSet.m[id]; exists {
			continue
		}
		validatorBannedSet.m[id] = struct{}{}
		ValidatorBannedList = append(ValidatorBannedList, id)
	}

	sort.Strings(ValidatorBannedList)
}

func isValidatorBanned(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	validatorBannedSet.mu.RLock()
	_, ok := validatorBannedSet.m[id]
	validatorBannedSet.mu.RUnlock()
	return ok
}

var CensorshipCount = map[string]int{}
var TxAbuse = map[string]*TxAbuseRecord{}
var FakeTxAttempts = map[string]int{}
var TxBanUntil = map[string]time.Time{}

type TxExecutionProof struct {
	TxID          string
	PreStateHash  string
	PostStateHash string
}

var consensusStarted atomic.Bool
var leader string

const (
	MsgFinalBlock = "final_block"

	MsgLeaderBlock        = "leader_block"
	MsgPeerHello          = "peer_hello"
	MsgCensorshipEvidence = "censorship_evidence"
	MsgValidatorAnnounce  = "validator_announce"
	MsgValidatorSetUpdate = "validator_set_update"
	MsgSnapshotOffer      = "snapshot_offer"
	MsgSnapshotProof      = "snapshot_proof"
	MsgSnapshotMeta       = "snapshot_meta"
	MsgSnapshotChunk      = "snapshot_chunk"
	MsgTx                 = "tx"
	MsgBlock              = "block"
	MsgGetBlocks          = "get_blocks"
	MsgBlocksBatch        = "blocks_batch"
	MsgPeers              = "peers"
	MsgPing               = "ping"
	MsgPong               = "pong"
	MsgExecutionResult    = "execution_result"
	MsgCommit             = "commit"
	MsgBlockAck           = "block_ack"
)

type BlockVote struct {
	Height    uint64
	BlockHash string
	Validator string
	Signature []byte
}

type BlockAck struct {
	Height uint64 `json:"height"`
}

type SnapshotOffer struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Height      uint64 `json:"height"`
	BlockHash   string `json:"block_hash"`
	StateRoot   string `json:"state_root"`
	GenesisHash string `json:"genesis_hash"`
}

type SnapshotProof struct {
	Height                uint64 `json:"height"`
	CheckpointHeight      uint64 `json:"checkpoint_height,omitempty"`
	BlockHash             string `json:"block_hash,omitempty"`
	SnapshotHash          string `json:"snapshot_hash"`
	StateRoot             string `json:"state_root"`
	StateMerkleRoot       string `json:"state_merkle_root,omitempty"`
	LedgerHash            string `json:"ledger_hash,omitempty"`
	ValidatorSetHash      string `json:"validator_set_hash"`
	ValidatorSetRoot      string `json:"validator_set_root,omitempty"`
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	CheckpointDomain      string `json:"checkpoint_domain,omitempty"`
	Validator             string `json:"validator"`
	SignatureHex          string `json:"signature_hex"`
	Timestamp             int64  `json:"timestamp,omitempty"`
}

type SnapshotMetaGossip struct {
	From      string               `json:"from,omitempty"`
	Height    uint64               `json:"height"`
	Meta      SnapshotMetaResponse `json:"meta"`
	Manifest  *SnapshotManifest    `json:"manifest,omitempty"`
	Timestamp int64                `json:"timestamp,omitempty"`
}

type SnapshotChunkGossip struct {
	From         string `json:"from,omitempty"`
	Height       uint64 `json:"height"`
	SnapshotHash string `json:"snapshot_hash"`
	ChunkSize    uint64 `json:"chunk_size"`
	ChunkCount   uint64 `json:"chunk_count"`
	Timestamp    int64  `json:"timestamp,omitempty"`
}

type SnapshotAnchorCache struct {
	CandidateKey          string    `json:"candidate_key,omitempty"`
	Height                uint64    `json:"height"`
	CheckpointHeight      uint64    `json:"checkpoint_height,omitempty"`
	SnapshotHash          string    `json:"snapshot_hash,omitempty"`
	StateRoot             string    `json:"state_root,omitempty"`
	StateMerkleRoot       string    `json:"state_merkle_root,omitempty"`
	ValidatorSetHash      string    `json:"validator_set_hash,omitempty"`
	ValidatorRegistryHash string    `json:"validator_registry_hash,omitempty"`
	Votes                 int       `json:"votes"`
	Validators            []string  `json:"validators,omitempty"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ValidatorSetUpdate struct {
	From        string   `json:"from"`
	Height      uint64   `json:"height"`
	Validators  []string `json:"validators"`
	Hash        string   `json:"hash"`
	GenesisHash string   `json:"genesis_hash"`
}

var ActiveValidators = map[string]bool{}
var ValidatorLastSeen = map[string]time.Time{}
var (
	participationMu  sync.RWMutex
	validatorsMu     sync.RWMutex
	blocksMu         sync.RWMutex
	noncesMu         sync.RWMutex
	proposedBlocksMu sync.Mutex
	txAbuseMu        sync.Mutex // 🔒 ADD THIS
	TxAbuseMu        sync.Mutex
	CensorshipMu     sync.Mutex
)

var LastWorkBlockTime int64 = 0
var LastBlockTime int64 = 0
var IsSynced = false

const LeaderCooldownBlocks = 100 // or 1000 (configurable)
type ValidatorAnnounce struct {
	NodeID    string
	PubKey    string
	Height    int
	Signature string
}

type NodeDB struct {
	State    *DB
	Blocks   *DB
	Snapshot *DB
	Tx       *DB
	Meta     *DB
}
type ParticipationScore struct {
	ValidBlocks   int
	InvalidBlocks int
	LastSeen      time.Time
	CooldownUntil uint64
	Reputation    int
}

var Participation = make(map[string]*ParticipationScore)
var PeerInteractions = map[string]map[string]int{}
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
	PublicKey string       `json:"publicKey"`
	Crypto    EncryptedKey `json:"crypto"`
}
type ValidatorKey struct {
	ID         string // validator ID (node ID)
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey // NEVER exposed to users
}

var ProtocolFrozen = true

// validatorPubKeysMu guards ValidatorPubKeys and GenesisValidatorPubKeys.
var validatorPubKeysMu sync.RWMutex

var ValidatorPubKeys = map[string]ed25519.PublicKey{}

// Immutable genesis validator keys used as a historical verification fallback.
var GenesisValidatorPubKeys = map[string]ed25519.PublicKey{}

// ExecPool is a global execution vote pool shared inside the process.
// Votes are block-scoped across round churn, not tied to only the current round.
var ExecPool = struct {
	mu          sync.Mutex
	pool        map[uint64]map[string]map[string]ExecutionResult // epoch -> blockScopedExecKey -> signer -> result
	txMerkle    map[uint64]map[string]string                     // epoch -> blockScopedExecKey -> txMerkle
	frozen      map[uint64]map[string]string                     // epoch -> blockScopeKey -> execHash (frozen after quorum)
	signers     map[uint64]map[string]map[string]bool            // epoch -> blockScopeKey -> signer -> seen
	choice      map[uint64]map[string]map[string]string          // epoch -> blockScopeKey -> signer -> execHash:txMerkle
	epochChoice map[uint64]map[string]string                     // epoch -> signer -> blockScopeKey|execHash:txMerkle
}{
	pool:        make(map[uint64]map[string]map[string]ExecutionResult),
	txMerkle:    make(map[uint64]map[string]string),
	frozen:      make(map[uint64]map[string]string),
	signers:     make(map[uint64]map[string]map[string]bool),
	choice:      make(map[uint64]map[string]map[string]string),
	epochChoice: make(map[uint64]map[string]string),
}

// ================================
// MSC COIN PROTOCOL CONSTANTS
// ================================
const (
	CoinSymbol       = "MSC"
	CoinName         = "Mythical system coin"
	CoinDecimals     = 18
	FixedTotalSupply = int64(9193823602)
)

const AltCoinSymbol = "MSCX"

var AllowedCoins = map[string]bool{
	CoinSymbol:    true,
	AltCoinSymbol: true,
}

// ================================
// ADAPTIVE CONSENSUS STATE
// ================================
var TotalMintedMSC int64 = 0
var CurrentTimeoutHours = 10  // initial fallback window
const MaxTimeoutHours = 72    // safety cap
const TimeoutIncreaseStep = 2 // +2 hours each fallback
// block classification

const (
	RewardUser       = 10
	RewardProposer   = 40
	RewardValidators = 40
	RewardOwner      = 10
)

const (
	FeeSplitUser       = 0
	FeeSplitProposer   = 50
	FeeSplitValidators = 50
	FeeSplitOwner      = 0
)

// RandomUserRewardEnabled controls whether user-pool minting is emitted only on
// deterministic pseudo-random block slots.
var RandomUserRewardEnabled = true

// RandomUserRewardChanceBPS is the per-block chance (in basis points) that
// USER_REWARD_POOL receives its mint share.
// Example: 2500 = 25% chance.
var RandomUserRewardChanceBPS = 2500

// Scheduled emission reward (independent from tx-fee-derived reward).
// Policy goals:
// - deterministic random trigger on eligible blocks
// - min/max reward bounds
// - yearly halving by block interval
// - fixed split: treasury / validators / burn
var EmissionRewardEnabled = true
var EmissionMinReward int64 = 2
var EmissionMaxReward int64 = 4
var EmissionJackpotChanceBPS = 100
var EmissionBaseChanceBPS = 10000
var EmissionHighChanceAfterBlocks uint64 = 1105840
var EmissionHighChanceBPS = 10000
var EmissionHalvingIntervalBlocks uint64 = 1105840
var EmissionTreasuryBPS = 2000
var EmissionValidatorBPS = 7200
var EmissionBurnBPS = 800

// Work-block base reward (added on top of fee-derived reward).
// This helps ensure productive tx blocks always pay a deterministic minimum.
var WorkBlockRewardEnabled = true
var WorkBlockBaseReward int64 = 2

// Burn stops permanently once effective supply reaches this floor.
// 0 means disabled unless configured via config.toml.
var BurnStopSupply int64 = 0
var EmissionIntervalMode = false
var EmissionGapMinBlocks uint64 = 4
var EmissionGapMaxBlocks uint64 = 6
var EmissionValidatorToProposer = true

// UnifiedTeamRewardEnabled merges base block reward distribution for both
// work/task and time blocks into proposer/validators/treasury splits.
// Disabled by default to preserve legacy behavior.
var UnifiedTeamRewardEnabled = false
var UnifiedTeamRewardTreasuryBPS = 2000
var UnifiedTeamRewardProposerBPS = 3500
var UnifiedTeamRewardValidatorBPS = 4500

const (
	SlashDoubleProposal = 50
	SlashInvalidBlock   = 30
	SlashFinalityBreak  = 100
	// 1000 bps = 10% stake burn on slash events.
	SlashStakeBurnBPS    = 1000
	SevereSlashExitAfter = 3
)

var ProposedBlocks = map[uint64]map[uint32]map[string]string{} // height -> round -> proposer -> block hash
var OwnerAddress = "MSC_OWNER_TREASURY_ADDRESS"

const OWNER_ADDRESS = "MSC_OWNER_ACCOUNT"
const TREASURY_ADDRESS = "MSC_TREASURY"
const FOUNDATION_ADDRESS = "MSC_FOUNDATION"
const VALIDATOR_BOOTSTRAP_POOL = "MSC_VALIDATOR_BOOTSTRAP"
const COMMUNITY_POOL = "MSC_COMMUNITY_POOL"
const USER_REWARD_POOL = "USER_REWARD_POOL"
const MaxTxPerAddress = 100
const MaxMempoolSize = 5000
const MaxTxPerAccountPerBlock = 20
const MaxTxTTLSeconds = 600
const MaxTxRequestBodyBytes int64 = 64 * 1024
const MaxTxGossipMessageBytes = 64 * 1024
const MaxTxIDHexLen = 64
const MaxTxPubKeyHexLen = 64
const MaxTxSignatureHexLen = 128
const MaxTxAddressLen = 128
const MaxTxCoinLen = 16
const MaxTxChainIDLen = 64
const MaxTxEVMCodeHexLen = 256 * 1024
const MaxTxEVMInputHexLen = 128 * 1024
const MaxTxEVMRawHexLen = 256 * 1024
const MaxTxEVMHashHexLen = 64
const MaxTxDTLTypeLen = 32
const MaxTxDTLTokenIDLen = 128
const MaxTxDTLPayloadLen = 48 * 1024
const MaxTxDTLGCertLen = 24 * 1024

var TxGossipGlobalRatePerSecond = 1000
var TxGossipPeerRatePerSecond = 100
var TxGossipPeerBurst = 50
var TxGossipPeerLimiterTTL = 10 * time.Minute

// 23 months at ~3s/epoch => 19,872,000 epochs.
const DefaultStakeLockEpochs uint64 = 19872000
const MinUnstakeMonths = 23
const DaysPerMonth = 30

var ActiveProposals []Proposal
var ValidatorLastVote = map[string]uint64{} // Changed from int to uint64
type Message struct {
	Type string `json:"type"` // "tx" | "block"
	Data []byte `json:"data"`
}

var (
	FinalityVotes = map[uint64]map[string]bool{} // height -> validatorID -> voted // Changed from int to uint64
)

const (
	Version = "0.1.0"
)

var ChainID = "91938"

type ValidatorCandidate struct {
	Address    string
	PubKey     []byte
	Reputation int64
}
type VRFProof struct {
	Output []byte
	Proof  []byte
}
type BlockHeader struct {
	Height                    uint64
	PrevHash                  string
	StateRoot                 string
	TxRoot                    string
	ValidatorSetHash          string
	ValidatorSetRoot          string
	ValidatorRegistryHash     string
	NextValidatorSetHash      string
	NextValidatorSetRoot      string
	FinalityRoot              string
	EpochAnchorHash           string
	FinalizedHeight           uint64
	FinalizedStateRoot        string
	FinalizedValidatorSetHash string
	FinalizedValidatorSetRoot string
	RandomSeed                string
	Proposer                  string
	Timestamp                 int64
}
type FinalityCert struct {
	BlockHash  string
	Signatures map[string][]byte // validator → sig
}

type ValidatorSignature struct {
	Validator string `json:"validator"`
	Signature string `json:"signature"`
}

type FinalizedEpochCertificate struct {
	Version                   string               `json:"version"`
	Domain                    string               `json:"domain"`
	Epoch                     uint64               `json:"epoch"`
	Height                    uint64               `json:"height"`
	BlockHash                 string               `json:"block_hash"`
	StateRoot                 string               `json:"state_root"`
	ValidatorSetHash          string               `json:"validator_set_hash"`
	ValidatorSetRoot          string               `json:"validator_set_root,omitempty"`
	EpochAnchorHash           string               `json:"epoch_anchor_hash"`
	PreviousEpochAnchorHash   string               `json:"previous_epoch_anchor_hash,omitempty"`
	FinalityRoot              string               `json:"finality_root"`
	ConsensusMode             string               `json:"consensus_mode,omitempty"`
	QuorumPolicyVersion       string               `json:"quorum_policy_version,omitempty"`
	ActiveReadyCount          int                  `json:"active_ready_count,omitempty"`
	RequiredQuorum            int                  `json:"required_quorum,omitempty"`
	StrictQuorum              int                  `json:"strict_quorum,omitempty"`
	FinalizedValidatorSetHash string               `json:"finalized_validator_set_hash"`
	FinalizedValidatorSetRoot string               `json:"finalized_validator_set_root,omitempty"`
	Signers                   []string             `json:"signers,omitempty"`
	Signatures                []ValidatorSignature `json:"signatures,omitempty"`
	ExecutionResultSignatures map[string]string    `json:"execution_result_signatures,omitempty"`
}
type Handshake struct {
	NodeID          string   `json:"node_id"`
	ChainID         string   `json:"chain_id"`
	Version         string   `json:"version"`
	Address         string   `json:"address"`          // ✅ ADD
	PubKey          string   `json:"pubkey"`           // ✅ ADD THIS
	Height          uint64   `json:"height"`           // Changed from int to uint64
	FinalizedHeight uint64   `json:"finalized_height"` // Changed from int to uint64
	GenesisHash     string   `json:"genesis_hash"`
	IsValidator     bool     `json:"is_validator"`
	Signature       string   `json:"sig"` // 🔐 ADD
	Peers           []string `json:"peers"`
}

var (
	apiToken       = strings.TrimSpace(os.Getenv("MSC_RPC_TOKEN"))
	apiReadToken   = strings.TrimSpace(os.Getenv("MSC_RPC_READ_TOKEN"))
	apiSubmitToken = strings.TrimSpace(os.Getenv("MSC_RPC_SUBMIT_TOKEN"))
	rateMu         sync.Mutex
	rateMap        = make(map[string][]time.Time)
)

type Server struct {
	Node *Node
}

// AuthSession captures wallet-auth details for node startup gating and RPC access.
type AuthSession struct {
	SessionID     string
	NodeID        string
	ChainID       string
	Nonce         string
	Message       string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	WalletAddr    string
	WalletPubKey  string
	Token         string
	TokenExpires  time.Time
	ValidatorOK   bool
	ValidatorNote string
}

var (
	authMu         sync.Mutex
	authSessions   = make(map[string]*AuthSession)
	authTokens     = make(map[string]*AuthSession)
	authReady      bool
	authNodeID     string
	authWalletAddr string
	authWalletPub  string
)

// ProtocolConfig holds live blockchain rules
type ProtocolConfig struct {
	MaxPeers      int
	MinValidators int
	SlashMinor    int
	SlashMajor    int
	MaxTxPerBlock int
	// BlockTime is logical time per epoch (system time, not wall clock).
	BlockTime     time.Duration
	RealTick      time.Duration
	TicksPerEpoch uint64
	MinExecutors  int // 🔥 REQUIRED FOR MODEL-3
	FeeBps        int // legacy fixed fee in basis points (e.g. 20 = 0.2%)
	// Dynamic fee parameters
	MinFeeBps      int // min fee (bps)
	MaxFeeBps      int // max fee (bps)
	FeeFloorAmount int // amount at which min fee applies
	FeeCeilAmount  int // amount at which max fee applies
	// DTL fee model (base by tx-type + optional payload component).
	DTLCreateBaseFee    int // TOKEN_CREATE base fee
	DTLTransferBaseFee  int // TOKEN_TRANSFER base fee
	DTLMintBaseFee      int // TOKEN_MINT base fee
	DTLBurnBaseFee      int // TOKEN_BURN base fee
	DTLPayloadFeePerKB  int // additional fee per started 1KiB payload
	DTLFeeMaxMultiplier int // max accepted fee multiplier vs required (anti-fat-finger)
	// Exec-hash quorum percent (e.g. 60)
	ExecQuorumPct int
	// Leader rotation slot (real-time seconds)
	LeaderSlotSeconds int64
}

// GlobalConfig is the active protocol config
var GlobalConfig = ProtocolConfig{
	BlockTime:           5 * time.Minute,
	RealTick:            2 * time.Second,
	TicksPerEpoch:       100,
	MaxPeers:            50,
	MinValidators:       3,
	SlashMinor:          10,
	SlashMajor:          20,
	MaxTxPerBlock:       250,
	MinExecutors:        2, // production: 3–5
	FeeBps:              20,
	MinFeeBps:           20,
	MaxFeeBps:           300,
	FeeFloorAmount:      200,
	FeeCeilAmount:       100000,
	DTLCreateBaseFee:    1,
	DTLTransferBaseFee:  1,
	DTLMintBaseFee:      1,
	DTLBurnBaseFee:      1,
	DTLPayloadFeePerKB:  0,
	DTLFeeMaxMultiplier: 10,
	ExecQuorumPct:       60,
	LeaderSlotSeconds:   240,
}

// LeaderStallTimeout triggers fallback leader blocks when the expected leader stalls.
// Keep this above the network RTT to avoid unnecessary fallback.
var LeaderStallTimeout = 15 * time.Second

type Proposal struct {
	ID      string
	Votes   map[string]bool
	Applied bool
}
type Mempool struct {
	mu                   sync.Mutex
	Transactions         []Transaction
	SeenTxIDs            map[string]bool
	txByID               map[string]struct{}
	txBySenderNonce      map[string]string
	pendingCountBySender map[string]int
	nextNonceBySender    map[string]int
}

var GenesisHashExpected = "d6d7d96ea1a70d2aca31389ce7ef7953794ce77b4c933828295269702768fa3c"

var GenesisHash = GenesisHashExpected

// Permissionless validator admission (no-stake)
var CandidateObservationEpochs uint64 = 50
var CandidateDCSMin float64 = 0.999
var CandidateUptimeMin float64 = 0.99
var CandidateDiversityPctMin float64 = 0.60
var CandidateDiversityEpochs uint64 = 50
var MaxPromotionsPerWindow int = 1
var PromotionWindowSize uint64 = 100
var CandidateSRPMin float64 = 0.999
var CandidateSRPAlpha float64 = 0.20

// After promotion we allow a small number of SRP warnings before scheduling removal.
var CandidateWarningLimit int = 3
var TestingRelaxedPromotion bool = false

const CandidateBanFirst = 370
const CandidateBanSecond = 1000

//	var GenesisValidators = []string{
//	    "12D3KooWM1yVV5Rovap6i9sepWuJbgz9VDDLnBdu2ioRfsXXri2Y", // A
//	    "12D3KooWS47891wJZbiWCRUFx5BGoMZNBCQmNBG94ogVC39fKWeY", // B
//	    "12D3KooWGg4V4nhsLvn4FXpCjzW62qUYLfex1pbRQSXdM5ovVGeQ", // C
//	}
type ValidatorAnnouncement struct {
	NodeID             string `json:"node_id"`
	PubKey             string `json:"pubkey"`
	P2PAddr            string `json:"p2p_addr"`
	Height             uint64 `json:"height"` // legacy: reported height
	ReportedHeight     uint64 `json:"reported_height"`
	FinalizedHeight    uint64 `json:"finalized_height"`
	ExecEpoch          uint64 `json:"exec_epoch"`
	ValidatorSetHeight uint64 `json:"validator_set_height,omitempty"`
	ValidatorSetHash   string `json:"validator_set_hash,omitempty"`
	ActivationHeight   uint64 `json:"activation_height,omitempty"`
	// NextValidatorSetHash is diagnostic-only: peers can observe upcoming
	// deterministic transition commitment without treating it as authority.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// NextActivationHeight mirrors the target activation height for
	// NextValidatorSetHash (usually validator_set_height+1).
	NextActivationHeight uint64 `json:"next_activation_height,omitempty"`
	// ConsensusReady means this heartbeat sender is currently able to vote for
	// execution consensus, not only connected and height-live.
	ConsensusReady    bool   `json:"consensus_ready,omitempty"`
	ConsensusReadySet bool   `json:"consensus_ready_set,omitempty"`
	IsValidator       bool   `json:"is_validator"`
	Signature         string `json:"signature"`
}

type ValidatorRejoinState struct {
	Pending          bool
	Heartbeats       uint16
	LastHeartbeat    time.Time
	LastSignedHeight uint64
}

type ValidatorKeyHealth struct {
	Loaded           bool
	Fingerprint      string
	Expected         string
	Match            bool
	IntegrityOK      bool
	BackupPresent    bool
	BackupAgeSeconds uint64
	Source           string
	Mode             string
}

type CoreRegistryEntry struct {
	ID                     string `json:"id"`
	RequiredKeyFingerprint string `json:"required_key_fingerprint"`
	ConsensusPubKey        string `json:"consensus_pubkey"`
	P2PSeed                string `json:"p2p_seed"`
	Status                 string `json:"status"`
}

type CoreRegistrySignature struct {
	SignerID     string `json:"signer_id"`
	SignerPubKey string `json:"signer_pubkey"`
	SigHex       string `json:"sig_hex"`
}

type CoreRegistry struct {
	ChainID              string                  `json:"chain_id"`
	Version              uint64                  `json:"version"`
	Epoch                uint64                  `json:"epoch"`
	EffectiveHeight      uint64                  `json:"effective_height"`
	PreviousRegistryHash string                  `json:"previous_registry_hash"`
	Validators           []CoreRegistryEntry     `json:"validators"`
	Signatures           []CoreRegistrySignature `json:"signatures"`
	PayloadHash          string                  `json:"payload_hash"`
}

type CoreRegistryState struct {
	Hash            string
	Epoch           uint64
	EffectiveHeight uint64
	Verified        bool
	ActiveCoreSet   []string
	PendingCoreSet  []string
	LastReloadAt    time.Time
	EnforcementMode string
}

type CoreActivationStatus struct {
	NodeID         string `json:"node_id"`
	Status         string `json:"status"`
	EligibleHeight uint64 `json:"eligible_height"`
	Reason         string `json:"reason"`
}

type ValidatorInfo struct {
	ID     string
	Height uint64
}

const (
	TopicBlock         = "msc-block"
	TopicTx            = "msc-tx"
	TopicConsensus     = "msc-consensus"
	TopicValidator     = "msc-validator"
	TopicSnapshotMeta  = "msc-snapshot-meta"
	TopicSnapshotChunk = "msc-snapshot-chunk"
	TopicSnapshotProof = "msc-snapshot-proof"

	TopicBlocksLegacy       = "msc-blocks"
	TopicTransactionsLegacy = "msc-transactions"
	TopicValidatorsLegacy   = "msc-validators"
)

// Backward-compatible alias for older references.
const ValidatorTopic = TopicValidator

var PeerLastSeen = map[string]time.Time{}
var peerLastSeenMu sync.Mutex

type GenesisFile struct {
	ChainID    string            `json:"chain_id"`
	Validators map[string]string `json:"validators"` // id -> pubkey hex
}

type Node struct {

	// Canonical validator set

	PeersLibp2p     []peer.ID // 🔥 FIXED: Changed from PeersLib2p to PeersLibp2p
	CensorshipTopic *pubsub.Topic
	// 🔥 NAYA LIBP2P FIELDS

	ProposalTopic      *pubsub.Topic
	VoteTopic          *pubsub.Topic
	BlockTopic         *pubsub.Topic
	TxTopic            *pubsub.Topic
	Libp2pHost         host.Host
	PubSub             *pubsub.PubSub
	DHT                *dht.IpfsDHT
	PeersLib2p         []peer.ID // 🔥 ADD THIS LINE - keep track of libp2p peers
	BlockSubscription  *pubsub.Subscription
	peerMu             sync.RWMutex
	MisbehaviorLog     map[string][]SlashEvidence
	misbehaviorMu      sync.Mutex
	TxSubscription     *pubsub.Subscription
	ConsensusTopic     *pubsub.Topic
	ConsensusSub       *pubsub.Subscription
	SnapshotMetaTopic  *pubsub.Topic
	SnapshotMetaSub    *pubsub.Subscription
	SnapshotChunkTopic *pubsub.Topic
	SnapshotChunkSub   *pubsub.Subscription
	SnapshotProofTopic *pubsub.Topic
	SnapshotProofSub   *pubsub.Subscription
	TopicBlocks        *pubsub.Topic
	Host               host.Host // Libp2p host
	streamManager      streamOpener

	TopicProposal *pubsub.Topic
	TopicVote     *pubsub.Topic
	// 🔥 BOOTSTRAP LIFECYCLE FLAG (AUTHORITATIVE)
	BootstrapDone                    bool
	shutdownCh                       chan struct{}
	shutdownOnce                     sync.Once
	rootCtx                          context.Context
	rootCancel                       context.CancelFunc
	ID                               string
	Ledger                           Ledger
	ExecutionLedger                  Ledger
	Wallet                           Wallet
	DataDir                          string
	Peers                            []*Node
	Mempool                          Mempool
	SeenTxIDs                        map[string]bool
	seenTxMu                         sync.Mutex
	seenTxQueue                      []string
	seenTxHead                       int
	SeenBlockHashes                  map[string]bool // 👈 REQUIRED
	seenBlockMu                      sync.Mutex
	seenBlockQueue                   []string
	seenBlockHead                    int
	ForkBlocks                       map[uint64][]Block // 👈 ADD THIS
	forkMu                           sync.RWMutex
	PeersTCP                         []string // e.g. ["127.0.0.1:7001"]
	Blockchain                       *Blockchain
	DB                               *NodeDB // 🔥 ADD THIS
	closeChan                        chan struct{}
	wg                               sync.WaitGroup
	threadInitOnce                   sync.Once
	immediateRoundStartMu            sync.Mutex
	immediateRoundStartPendingHeight uint64
	immediateRoundStartStartedHeight uint64

	ConsensusThread *NodeTaskThread
	ExecutionThread *NodeTaskThread
	SyncThread      *NodeTaskThread

	ProposalHistory map[uint64]string // height → proposer
	SelfAddr        string            // 🔥 REQUIRED: this node’s real P2P address
	ValidatorKey    ValidatorKey      // consensus only

	// 🔥 VALIDATOR OWNERSHIP (FINAL)

	validatorMu   sync.RWMutex
	PeerDiscovery *PeerDiscovery // 🔥 NEW: Auto-discovery

	Consensus *ConsensusState
	Config    *NodeConfig // 🔥 NEW: Configuration
	configMu  sync.RWMutex

	// LibP2P

	// Validator gossip
	ValidatorTopic *pubsub.Topic
	ValidatorSub   *pubsub.Subscription

	sync.RWMutex

	// ================= LIBP2P =================

	// Runtime status
	validatorStatus       map[string]*ValidatorStatus
	validatorOfflineSince map[string]time.Time
	validatorRejoin       map[string]ValidatorRejoinState

	// Genesis-authoritative validator set (consensus snapshot baseline)
	GenesisValidators []string

	// Node role: validator | full | light
	Role string

	// Peer state for gossip quiet mode + flapping protection
	peerStateMu             sync.Mutex
	peerSetHash             map[string]string // peerID -> validator set hash
	peerTipHash             map[string]string // peerID -> advertised chain tip hash
	peerHashMatch           map[string]bool   // peerID -> hash match
	peerHelloOK             map[string]bool   // peerID -> handshake complete
	peerToValidator         map[string]string // peerID -> validator ID
	validatorToPeer         map[string]string // validator ID -> peerID
	peerRole                map[string]string // peerID -> role (validator|full|light)
	peerAckHeight           map[string]uint64 // peerID -> last acknowledged height
	peerDriftState          map[string]PeerDriftState
	peerSyncOnlyUntil       map[string]time.Time
	peerSyncOnlyClass       map[string]string
	peerSyncOnlyLastDropLog map[string]time.Time
	noBlockLogMu            sync.Mutex
	noBlockLogAt            map[string]time.Time // peerID:from:to:tip -> last log time
	peerSuspectAt           map[string]time.Time
	peerHelloSentAt         map[string]time.Time
	peerConnectedAt         map[string]time.Time
	peerFlapTimes           map[string][]time.Time
	peerGraftAt             map[string]time.Time
	quarantineUntil         map[string]time.Time
	peerDialFailures        map[string]int
	peerDialNext            map[string]time.Time
	peerSubnet              map[string]string
	peerASN                 map[string]string
	peerOutbound            map[string]bool
	peerHelloNonces         map[string]time.Time
	peerResourceWindows     map[string]PeerResourceWindow
	peerConnectWindows      map[string]PeerResourceWindow
	gossipQuiet             bool

	connectedPeers   map[string]bool
	connectingPeers  map[string]bool
	validatorSuspect map[string]time.Time
	allowedPeerIDs   map[string]bool
	dialSlots        chan struct{}
	dialSlotsMu      sync.Once

	// Validator set management (next-height activation)
	validatorSetMu sync.RWMutex
	// Deprecated cache-only field. Do not use as consensus authority; derive
	// validator sets from finalized chain/snapshot commitments.
	currentValidators                         []string
	pendingValidators                         map[string]uint64 // validatorID -> activateEpoch
	pendingValidatorRemovals                  map[string]uint64 // validatorID -> deactivateEpoch
	epochValidators                           map[uint64][]string
	frozenValidatorsByHeight                  map[uint64][]string
	frozenValidatorHashByHeight               map[uint64]string
	committeeByHeight                         map[uint64][]string
	committeeHashByHeight                     map[uint64]string
	committeeLiveByHeight                     map[uint64]map[string]bool
	safeModeGateActive                        int32
	safeModeGateHeight                        uint64
	safeModeUntilByHeight                     map[uint64]time.Time
	safeModeWindowByHeight                    map[uint64]time.Duration
	safeModeObservedDelays                    []time.Duration
	eligibleIndexVersion                      uint64
	eligibleSortedValidators                  []string
	queuedValidatorSetUpdates                 map[uint64]ValidatorSetUpdate
	validatorSetHeight                        uint64
	candidateMu                               sync.RWMutex
	candidates                                map[string]*CandidateStatus
	promotionWindowIdx                        uint64
	promotionsInWindow                        int
	validatorSetMismatchMu                    sync.Mutex
	validatorSetMismatchCnt                   int
	validatorSetMismatchSince                 time.Time
	validatorSetMismatchHeight                uint64
	validatorSetMismatchExpected              string
	validatorSetMismatchGot                   string
	validatorSetRepairKey                     string
	validatorSetRepairAt                      time.Time
	validatorSetRepairWindow                  time.Time
	validatorSetRepairAttempts                int
	validatorSetRepairBackoffTil              time.Time
	validatorSetSyncOverrideKey               string
	validatorSetSyncOverrideAt                time.Time
	validatorAutohealState                    string
	validatorAutohealLastReason               string
	validatorAutohealLastSuccessHeight        uint64
	peerDriftTupleCount                       map[string]uint64
	peerDriftTupleLastSeen                    map[string]time.Time
	peerDriftTupleLastLog                     map[string]time.Time
	livenessReasonLogMu                       sync.Mutex
	livenessReasonLogLast                     map[string]time.Time
	validatorStartupCheckOK                   bool
	validatorStartupCheckReason               string
	validatorStartupCheckHeight               uint64
	validatorStartupCheckExpected             string
	validatorStartupCheckGot                  string
	validatorStartupCheckAt                   time.Time
	postCommitEffectsMu                       sync.Mutex
	executionSnapshotRebuildMu                sync.Mutex
	executionSnapshotRebuildReadyHeight       uint64
	executionSnapshotRebuildLastErr           string
	executionSnapshotRebuildFailedHeight      uint64
	executionSnapshotRebuildTarget            uint64
	executionSnapshotRebuildLastScheduleAt    time.Time
	registryHistoryRebuildMu                  sync.Mutex
	registryHistoryRebuildTarget              uint64
	registryHistoryRebuildReadyHeight         uint64
	registryHistoryRebuildLastErr             string
	registryHistoryRebuildFailedHeight        uint64
	registryHistoryRebuildLastScheduledHeight uint64
	registryHistoryRebuildLastScheduleAt      time.Time
	freezeJournalLastHeight                   uint64
	freezeJournalLastHash                     string
	recomputePauseMu                          sync.Mutex
	recomputePauseUntil                       time.Time
	recomputePauseHeight                      uint64
	recomputePauseReason                      string
	recomputePauseLastLog                     time.Time
	recomputePauseApplied                     bool
	transitionBarrierPauseMu                  sync.Mutex
	transitionBarrierPauseLast                map[string]time.Time
	transitionBarrierRetryMu                  sync.Mutex
	transitionBarrierRetryStateByKey          map[transitionBarrierRetryKey]transitionBarrierRetryState
	transitionPlan                            validatorSetTransitionPlan
	selfActivationPendingMu                   sync.Mutex
	selfActivationPendingSince                uint64
	selfActivationPendingStableSince          uint64
	selfActivationPendingStableHash           string
	selfActivationPendingLastWarnAt           time.Time
	selfActivationPendingLastReconcile        uint64
	onboardingTrackerMu                       sync.RWMutex
	onboardingTracker                         map[string]ValidatorActivationTracker
	onboardingLogMu                           sync.Mutex
	onboardingLogLast                         map[string]time.Time
	invalidProposerMu                         sync.Mutex
	invalidProposerSeen                       map[uint64]map[string]int // height -> "expected->got" -> count
	invalidProposerEvidenceSeen               map[string]time.Time      // "height|round|expected|got|block_hash" -> first seen at
	invalidProposerStrikes                    map[string]ExecMismatchTracker
	invalidProposerPeerStrikes                map[string]ExecMismatchTracker
	doubleProposalMu                          sync.Mutex
	doubleProposalEvidenceSeen                map[string]time.Time // "height|round|proposer|prev_hash|got_hash" -> first seen at

	execResultsMu               sync.Mutex
	execResults                 map[string]map[string]ExecutionResult // key -> executor -> result
	execBroadcasted             map[uint64]map[string]bool            // epoch -> exec_hash:tx_merkle broadcasted
	pendingBlocks               map[string]Block
	queuedExecVotes             map[string][]ExecutionResultMsg
	acceptedProposal            map[string]string
	acceptedProposalBlocks      map[string]Block
	quorumLockedProposal        map[string]string
	execVoteGuardMu             sync.Mutex
	execVoteIngressSeen         map[string]time.Time
	execVoteStaleIngressSeen    map[string]time.Time
	execVoteSeen                map[string]time.Time
	execVoteLimiter             map[string]*rate.Limiter
	execMismatch                map[string]ExecMismatchTracker
	localExecMismatchMu         sync.Mutex
	localExecMismatchCount      int
	localExecMismatchHeight     uint64
	localExecMismatchBlockHash  string
	leaderMu                    sync.Mutex
	leaderBlocks                map[uint64]Block
	queuedFutureLeaderBlocks    map[uint64][]Block
	leaderConflictReplaceCount  map[uint64]uint32
	lastProposedRoundByHeight   map[uint64]uint32
	lastProposedRoundAtByHeight map[uint64]time.Time
	lastLeaderSlot              int64
	lastLeaderEpoch             uint64
	lastLeaderRound             uint32

	commitMu         sync.Mutex
	applyMu          sync.Mutex
	committed        map[uint64]string                         // height -> hash
	committedHeight  uint64                                    // monotonic finalized height (idempotent barrier)
	lastCommitHeight uint64                                    // last committed height observed locally
	lastCommitAt     time.Time                                 // wall-clock time of last committed-height progress
	finalizedHeight  uint64                                    // supermajority-finalized height (can lag committed)
	commitVotes      map[uint64]map[string]map[string]struct{} // height -> hash -> voter
	commitVoted      map[uint64]map[string]string              // height -> voter -> hash (one commit vote per height)
	commitInFlight   map[uint64]string                         // height -> hash currently being applied

	heartbeatMu            sync.Mutex
	lastHeartbeatReported  uint64
	lastHeartbeatFinalized uint64
	lastHeartbeatEpoch     uint64
	lastHeartbeatAt        time.Time
	lastHeartbeatGateLogAt time.Time
	heartbeatSignalCh      chan struct{}
	heartbeatForcePending  bool
	heartbeatLoopActive    bool

	startupRecoveryApplied  bool
	validatorKeyFingerprint string
	lastValidatorKeyHealth  ValidatorKeyHealth
	lastKeyAuditAt          time.Time
	coreRegistryMu          sync.RWMutex
	coreRegistryState       CoreRegistryState
	coreRegistryEntries     map[string]CoreRegistryEntry
	coreActivationStatus    CoreActivationStatus

	execSignerSeen             map[uint64]map[string]map[string]bool // epoch -> blockScopeKey -> signer -> seen
	execBroadcastedByValidator map[uint64]map[string]map[string]bool // epoch -> blockScopeKey -> signer -> broadcasted
	localExecVoteByRound       map[uint64]map[uint32]string          // epoch -> round -> proposalKey
	execRebroadcastMu          sync.Mutex
	execRebroadcastAt          map[uint64]time.Time // epoch -> last execution-vote broadcast activity
	execRebroadcastState       map[uint64]execVoteRebroadcastState

	syncMu                           sync.Mutex
	lastSyncAttempt                  time.Time
	lastSyncQueueLogAt               time.Time
	lastSyncQueueLogTarget           uint64
	lastQueueForceSyncAt             time.Time
	lastQueueForceSyncTarget         uint64
	lastMissingBlockRequestAt        time.Time
	lastMissingBlockHeight           uint64
	missingBlockRecoveryInFlight     bool
	syncMode                         string
	syncStage                        string
	syncPipelineStage                string
	syncLagBlocks                    uint64
	syncAction                       string
	syncActionAt                     time.Time
	syncPipelineStageAt              time.Time
	syncProvider                     string
	syncSnapshotHeight               uint64
	syncSnapshotHash                 string
	syncDownloadedChunks             uint64
	syncTotalChunks                  uint64
	syncChunkLastDownloaded          uint64
	syncChunkLastProgressAt          time.Time
	syncChunkProviders               []string
	syncVerifyStage                  string
	syncResumeState                  string
	syncLastProgressAt               time.Time
	syncLastObservedHeight           uint64
	syncStallSeconds                 uint64
	syncStrategy                     string
	syncResumeTarget                 uint64
	syncResumePending                bool
	syncProgressRate                 float64
	syncProgressSampleHeight         uint64
	syncProgressSampleAt             time.Time
	syncWarmupJoinHeight             uint64
	syncAvoidProvider                string
	syncAvoidProviderOnce            bool
	syncApplyFailureHeight           uint64
	syncApplyFailureLocalHeight      uint64
	syncApplyFailureBlockHash        string
	syncApplyFailurePrevHash         string
	syncApplyFailureReason           string
	syncApplyFailureProposer         string
	syncApplyFailureCount            uint64
	syncApplyFailureAt               time.Time
	syncRangeUnavailableStreak       uint64
	syncSnapshotSessionFailures      uint64
	syncSnapshotSessionLastFailAt    time.Time
	syncSnapshotSessionDegradedUntil time.Time
	syncPeerScoreMu                  sync.Mutex
	syncPeerScores                   map[string]*SyncPeerScore
	syncBackfillMu                   sync.Mutex
	syncBackfillWatermark            uint64
	syncApplyWorkerOnce              sync.Once
	syncBlockQueue                   chan syncApplyTask
	syncWarmupStartAt                time.Time
	syncWarmupLastHeight             uint64
	syncWarmupLastHeightAt           time.Time
	syncWarmupQuorumHash             string
	syncWarmupQuorumVotes            int
	syncWarmupQuorumSince            time.Time
	syncWarmupEligible               bool
	snapshotSessionMu                sync.Mutex
	snapshotSession                  SnapshotSession
	lateJoinAuthorityMu              sync.Mutex
	lateJoinAuthority                LateJoinAuthorityState

	logicalMu    sync.Mutex
	logicalClock LogicalClock

	heightReportMu        sync.Mutex
	heightReports         map[uint64]map[string]time.Time // height -> validator -> last report time
	validatorReportHeight map[string]uint64               // validator -> last reported height

	snapshotOfferMu     sync.Mutex
	snapshotOfferSent   map[string]uint64    // validator -> last offered height
	snapshotOfferSentAt map[string]time.Time // validator -> last offer wall time

	snapshotExecutionLedgerMu       sync.Mutex
	snapshotExecutionLedgerByHeight map[uint64]Ledger
	postCommitLedgerMu              sync.Mutex
	postCommitLedgerByHeight        map[uint64]Ledger

	snapshotGossipMu               sync.RWMutex
	snapshotMetaGossipCache        map[string]SnapshotMetaGossip
	snapshotChunkGossipCache       map[string]SnapshotChunkGossip
	snapshotMetaLastPublished      string
	snapshotMetaLastPublishedAt    time.Time
	snapshotChunkLastPublished     string
	snapshotChunkLastPublishedAt   time.Time
	snapshotBoostUntil             time.Time
	snapshotProofMu                sync.RWMutex
	snapshotProofs                 map[string]map[string]SnapshotProof // candidateKey -> validator -> proof
	snapshotProofProviders         map[string]map[string]string        // candidateKey -> validator -> peerID that supplied proof
	snapshotAnchorCache            map[uint64]SnapshotAnchorCache      // checkpointHeight -> best cached anchor
	snapshotProofLastPublished     string
	snapshotProofLastPublishedAt   time.Time
	validatorSnapshotPublishHeight uint64
	validatorSnapshotPublishHash   string
	validatorSnapshotPublishAt     time.Time
	validatorSnapshotPublishError  string
	validatorSnapshotPublished     *StateSnapshot
	snapshotCatalogMu              sync.RWMutex
	snapshotCatalog                map[uint64]SnapshotCatalogEntry

	// Snapshot trust cache (quorum across time)
	snapshotTrustMu sync.Mutex
	snapshotVotes   map[string]map[string]struct{} // trustKey -> validator IDs
	snapshotCache   map[string]*StateSnapshot      // trustKey -> snapshot

	observabilityMu sync.RWMutex
	observability   observabilityStats
}

type MapStats struct {
	SeenBlockHashes             int
	SeenTxIDs                   int
	ForkBlocksHeights           int
	ForkBlocksTotal             int
	ExecResultsKeys             int
	ExecResultsTotal            int
	PendingBlocks               int
	QueuedExecVotesKeys         int
	QueuedExecVotesTotal        int
	AcceptedProposal            int
	ExecBroadcastedEpochs       int
	ExecBroadcastedKeys         int
	ExecSignerSeenEpochs        int
	ExecSignerSeenTotal         int
	ExecBroadcastedByValEpochs  int
	ExecBroadcastedByValTotal   int
	ExecRebroadcastAtEpochs     int
	ValidatorStatusCount        int
	CandidateCount              int
	PeerStateCount              int
	PeerConnectedCount          int
	PeerConnectingCount         int
	PeerSuspectCount            int
	PeerQuarantinedCount        int
	AllowedPeerCount            int
	ValidatorSetPendingCount    int
	ValidatorSetRemovalCount    int
	QueuedValidatorSetUpdates   int
	HeightReportsEpochs         int
	HeightReportValidatorsTotal int
	MisbehaviorValidators       int
	MisbehaviorEventsTotal      int
	ExecMismatchTracked         int
	ExecVoteReplayCache         int
}

type SyncPeerScore struct {
	SnapshotSuccess    uint64
	SnapshotFail       uint64
	BlockBatchSuccess  uint64
	BlockBatchFail     uint64
	DialSuccess        uint64
	DialFailure        uint64
	TimeoutCount       uint64
	InvalidProofCount  uint64
	SecurityFaultCount uint64
	RateLimitDropCount uint64
	DecayedAt          time.Time
	AvgLatencyMs       float64
	LastBytesPerSec    float64
	UpdatedAt          time.Time
}

type PeerResourceWindow struct {
	StartedAt     time.Time
	Bytes         uint64
	Messages      uint64
	TxMessages    uint64
	BlockRequests uint64
	Connections   uint64
}

type WireMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ValidatorStatus struct {
	Height             uint64
	ReportedHeight     uint64
	FinalizedHeight    uint64
	ExecEpoch          uint64
	ValidatorSetHeight uint64
	ValidatorSetHash   string
	LastSeen           time.Time
	Active             bool

	ID     string
	PubKey ed25519.PublicKey

	Enabled             bool
	ConsensusReadyKnown bool
}

type CommitteeLivenessSnapshot struct {
	Height        uint64
	CommitteeSize int
	Live          int
	HeartbeatLive int
	OutOfDrift    int
	Offline       int
	WindowMs      uint64
	SafeUntil     time.Time
}

type CandidateStatus struct {
	ID                     string
	PubKey                 ed25519.PublicKey
	FirstSeenHeight        uint64
	ObservationStartHeight uint64
	LastObservedHeight     uint64
	ObservedEpochs         uint64
	MatchedEpochs          uint64
	HeartbeatGood          uint64
	HeartbeatTotal         uint64
	LastReportedHeight     uint64
	LastFinalizedHeight    uint64
	LastHeartbeatEpoch     uint64
	LastValidatorSetHeight uint64
	LastValidatorSetHash   string
	LastHeartbeatAt        time.Time
	ExecHashes             map[uint64]string // epoch -> exec hash
	PendingMatch           map[uint64]bool   // epoch -> awaiting exec hash
	Strikes                int
	BanUntil               uint64
	PermanentBan           bool
	Promoted               bool
	DiversityConsecutive   uint64
	LastDiversityRatio     float64
	DiversitySum           float64
	GossipTimely           uint64
	GossipTotal            uint64
	GossipMissing          uint64
	SRPScore               float64
}

type ExecMismatchTracker struct {
	Count     int
	LastEpoch uint64
	LastAt    time.Time
}

type transitionBarrierRetryKey struct {
	Reason       string
	UpdateHeight uint64
	NextSetHash  string
}

type transitionBarrierRetryState struct {
	NextRetryHeight uint64
	LastFailTuple   string
	LastUpdatedAt   time.Time
}

type validatorSetTransitionPlan struct {
	Active           bool
	UpdateHeight     uint64
	NextSetHash      string
	NextValidators   []string
	ProcessedAdds    []string
	ProcessedRemoves []string
	LockedAt         time.Time
	Reports          int
	Matches          int
	Required         int
}

type SnapshotSyncStage string

const (
	SnapshotSyncStageIdle           SnapshotSyncStage = "IDLE"
	SnapshotSyncStageDetectLag      SnapshotSyncStage = "DETECT_LAG"
	SnapshotSyncStageFreezeAnchor   SnapshotSyncStage = "FREEZE_ANCHOR"
	SnapshotSyncStageCheckAnchor    SnapshotSyncStage = "CHECK_ANCHOR"
	SnapshotSyncStageCollectProofs  SnapshotSyncStage = "COLLECT_PROOFS"
	SnapshotSyncStageVerifyQuorum   SnapshotSyncStage = "VERIFY_QUORUM"
	SnapshotSyncStageProviderRotate SnapshotSyncStage = "PROVIDER_ROTATE"
	SnapshotSyncStageApplySnapshot  SnapshotSyncStage = "APPLY_SNAPSHOT"
	SnapshotSyncStageDeltaReplay    SnapshotSyncStage = "DELTA_REPLAY"
	SnapshotSyncStageSyncComplete   SnapshotSyncStage = "SYNC_COMPLETE"
)

type SnapshotVote struct {
	ValidatorID           string `json:"validator_id"`
	Height                uint64 `json:"height"`
	SnapshotHash          string `json:"snapshot_hash"`
	StateRoot             string `json:"state_root"`
	ValidatorSetHash      string `json:"validator_set_hash"`
	ValidatorSetRoot      string `json:"validator_set_root,omitempty"`
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	SignatureHex          string `json:"signature_hex"`
}

type SnapshotSession struct {
	Active                      bool                         `json:"active"`
	SessionID                   string                       `json:"session_id,omitempty"`
	Stage                       SnapshotSyncStage            `json:"stage"`
	FreezeHeight                uint64                       `json:"freeze_height"`
	CheckpointHeight            uint64                       `json:"checkpoint_height"`
	CandidateHeight             uint64                       `json:"candidate_height,omitempty"`
	CandidateCheckpointHeight   uint64                       `json:"candidate_checkpoint_height,omitempty"`
	StartedAt                   time.Time                    `json:"started_at"`
	LastVoteAt                  time.Time                    `json:"last_vote_at"`
	CanonicalHash               string                       `json:"canonical_hash"`
	CheckpointHash              string                       `json:"checkpoint_hash"`
	FreezeStateRoot             string                       `json:"freeze_state_root"`
	FreezeVsetHash              string                       `json:"freeze_vset_hash"`
	FreezeRegistryHash          string                       `json:"freeze_registry_hash,omitempty"`
	FreezeSnapHash              string                       `json:"freeze_snapshot_hash"`
	Votes                       map[string]SnapshotVote      `json:"votes"`
	Required                    int                          `json:"required"`
	RequiredVotes               int                          `json:"required_votes"`
	Applied                     bool                         `json:"applied"`
	Completed                   bool                         `json:"completed"`
	ProviderSet                 []string                     `json:"provider_set"`
	CurrentProvider             string                       `json:"current_provider"`
	Deadline                    time.Time                    `json:"deadline"`
	LastTriggerTime             time.Time                    `json:"last_trigger_time"`
	RetryCount                  uint64                       `json:"retry_count"`
	LastError                   string                       `json:"last_error"`
	LastAppliedSnapshotHash     string                       `json:"last_applied_snapshot_hash,omitempty"`
	LastAppliedSnapshotAttempts uint64                       `json:"last_applied_snapshot_attempts,omitempty"`
	LastRejectReason            string                       `json:"last_reject_reason,omitempty"`
	StrictReasonCounts          map[string]uint64            `json:"strict_reason_counts,omitempty"`
	StrictProviderResults       map[string]map[string]uint64 `json:"strict_provider_results,omitempty"`
	RelaxProofs                 bool                         `json:"relax_proofs,omitempty"`
}

type LateJoinAuthorityState struct {
	Active                bool   `json:"active"`
	Authoritative         bool   `json:"authoritative"`
	Source                string `json:"source"`
	Height                uint64 `json:"height"`
	ValidatorSetHash      string `json:"validator_set_hash,omitempty"`
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	SnapshotHash          string `json:"snapshot_hash,omitempty"`
	AnchorBlockHash       string `json:"anchor_block_hash,omitempty"`
}

type OnboardingState string

const (
	OnboardingStateCandidateDetected   OnboardingState = "candidate_detected"
	OnboardingStateAwaitingPubKey      OnboardingState = "awaiting_pubkey"
	OnboardingStateAwaitingHeartbeat   OnboardingState = "awaiting_heartbeat"
	OnboardingStateAwaitingRejoinProof OnboardingState = "awaiting_rejoin_proof"
	OnboardingStateAwaitingSync        OnboardingState = "awaiting_sync"
	OnboardingStateScheduled           OnboardingState = "scheduled"
	OnboardingStateActivating          OnboardingState = "activating"
	OnboardingStateActive              OnboardingState = "active"
	OnboardingStateBlocked             OnboardingState = "blocked"
)

type ValidatorActivationTracker struct {
	State           OnboardingState `json:"state"`
	LastReason      string          `json:"last_reason"`
	ScheduledHeight uint64          `json:"scheduled_height"`
	EffectiveHeight uint64          `json:"effective_height"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type Genesis struct {
	ChainID            string                  `json:"chain_id"`
	Decimals           int                     `json:"decimals,omitempty"`
	GenesisLocked      bool                    `json:"genesis_locked,omitempty"`
	ValidatorSetFrozen bool                    `json:"validator_set_frozen,omitempty"`
	Validators         map[string]string       `json:"validators"` // nodeID -> pubkey(hex)
	Balances           map[string]int          `json:"balances,omitempty"`
	RewardWallets      map[string]string       `json:"reward_wallets,omitempty"` // validatorID -> wallet address
	Foundation         GenesisAllocation       `json:"foundation,omitempty"`
	Treasury           GenesisAllocation       `json:"treasury,omitempty"`
	GenesisStakes      map[string]GenesisStake `json:"genesis_stakes,omitempty"`
}

type GenesisStake struct {
	Wallet       string `json:"wallet,omitempty"`
	WalletPubKey string `json:"wallet_pubkey,omitempty"`
	Amount       int    `json:"amount"`
	LockEpochs   uint64 `json:"lock_epochs,omitempty"`
}

type GenesisAllocation struct {
	Wallet         string `json:"wallet,omitempty"`
	Allocation     int    `json:"allocation,omitempty"`
	Locked         bool   `json:"locked,omitempty"`
	LockEpochs     uint64 `json:"lock_epochs,omitempty"`
	GovernanceOnly bool   `json:"governance_only,omitempty"`
}

type genesisBootstrapWalletBinding struct {
	WalletAddr   string
	WalletPubKey string
}

var (
	genesisRewardWalletsMu         sync.RWMutex
	genesisRewardWallets           = make(map[string]string) // normalized validatorID -> wallet
	genesisBootstrapWalletsMu      sync.RWMutex
	genesisBootstrapWalletBindings = make(map[string]genesisBootstrapWalletBinding) // normalized validatorID -> genesis wallet binding
)

func setGenesisRewardWallets(m map[string]string) {
	genesisRewardWalletsMu.Lock()
	defer genesisRewardWalletsMu.Unlock()
	genesisRewardWallets = make(map[string]string)
	for vid, wallet := range m {
		id := strings.TrimSpace(strings.ToUpper(vid))
		addr := strings.TrimSpace(wallet)
		if id == "" || addr == "" {
			continue
		}
		genesisRewardWallets[id] = addr
	}
}

func genesisRewardWallet(validatorID string) (string, bool) {
	id := strings.TrimSpace(strings.ToUpper(validatorID))
	if id == "" {
		return "", false
	}
	genesisRewardWalletsMu.RLock()
	addr, ok := genesisRewardWallets[id]
	genesisRewardWalletsMu.RUnlock()
	if !ok || strings.TrimSpace(addr) == "" {
		return "", false
	}
	return addr, true
}

func setGenesisBootstrapWalletBindings(m map[string]genesisBootstrapWalletBinding) {
	genesisBootstrapWalletsMu.Lock()
	defer genesisBootstrapWalletsMu.Unlock()
	genesisBootstrapWalletBindings = make(map[string]genesisBootstrapWalletBinding)
	for vid, binding := range m {
		id := strings.TrimSpace(strings.ToUpper(vid))
		addr := strings.TrimSpace(binding.WalletAddr)
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

func genesisBootstrapWalletBindingForValidator(validatorID string) (genesisBootstrapWalletBinding, bool) {
	id := strings.TrimSpace(strings.ToUpper(validatorID))
	if id == "" {
		return genesisBootstrapWalletBinding{}, false
	}
	genesisBootstrapWalletsMu.RLock()
	binding, ok := genesisBootstrapWalletBindings[id]
	genesisBootstrapWalletsMu.RUnlock()
	if !ok || strings.TrimSpace(binding.WalletAddr) == "" || strings.TrimSpace(binding.WalletPubKey) == "" {
		return genesisBootstrapWalletBinding{}, false
	}
	return binding, true
}

func trustedGenesisWalletBindingForValidator(validatorID string) (genesisBootstrapWalletBinding, string, bool) {
	id := normalizeValidatorID(validatorID)
	if id == "" {
		return genesisBootstrapWalletBinding{}, "", false
	}
	binding, ok := genesisBootstrapWalletBindingForValidator(id)
	if !ok {
		return genesisBootstrapWalletBinding{}, "", false
	}
	rewardWallet, ok := genesisRewardWallet(id)
	if !ok || !addressesEqual(rewardWallet, binding.WalletAddr) {
		return genesisBootstrapWalletBinding{}, "", false
	}
	validatorPubKeysMu.RLock()
	_, knownGenesisValidator := GenesisValidatorPubKeys[id]
	hasGenesisValidators := len(GenesisValidatorPubKeys) > 0
	validatorPubKeysMu.RUnlock()
	if hasGenesisValidators && !knownGenesisValidator {
		return genesisBootstrapWalletBinding{}, "", false
	}
	return binding, rewardWallet, true
}

func genesisWalletAuthExemptValidator(validatorID string) bool {
	_, _, ok := trustedGenesisWalletBindingForValidator(validatorID)
	return ok
}

type EvidenceKey struct {
	Leaderme string
	Height   uint64

	Leader string
}

var (
	NetworkName      = "testnet" // "testnet" | "mainnet"
	IsTestnet        = true
	AllowTreasuryOps = false
)

var (
	ConfigDTLSolidityLikeRuntimeEnabled   = false
	ConfigDTLContractsV2Enabled           = false
	ConfigDTLCompatRPCSubsetEnabled       = false
	ConfigDTLV2ActivationHeight           uint64
	ConfigDTLLogsIndexEnabled                    = true
	ConfigDTLOracleMinSigners             uint16 = 3
	ConfigDTLOracleMaxStalenessBlocks     uint64 = 120
	ConfigDTLLendingAccrualIntervalBlocks uint64 = 1
	ConfigDTLGameBeaconDelayBlocks        uint64 = 8
	ConfigDTLMaxContractCallDepth         uint16 = 2
	ConfigDTLBytecodeRuntimeEnabled              = false
	ConfigDTLBytecodeActivationHeight     uint64
	ConfigDTLBytecodeMaxSize              uint64 = DTLDefaultBytecodeMaxSize
	ConfigDTLBytecodeRequireCanonical            = true
	ConfigDTLRouterEnabled                       = true
	ConfigDTLRouterMaxHops                uint16 = DTLDefaultRouterMaxHops
	ConfigDTLRouterDeadlineMaxBlocks      uint64 = DTLDefaultRouterDeadlineMaxBlocks
	ConfigDTLRouterMaxPriceImpactBPS      uint16 = DTLDefaultRouterMaxPriceImpactBPS
	ConfigDTLRouterQuoteMaxPaths          uint16 = DTLDefaultRouterQuoteMaxPaths
	ConfigDTLDeFiFarmEnabled                     = true
	ConfigDTLGameFiSeasonEnabled                 = true
	ConfigDTLGameFiRewardToken                   = CoinSymbol
	ConfigDTLGameFiSeasonLengthBlocks     uint64 = DTLDefaultGameFiSeasonLengthBlocks
	ConfigDTLGameFiClaimGraceBlocks       uint64 = DTLDefaultGameFiClaimGraceBlocks
	ConfigDTLGameFiFeeShareFromPoolBPS    uint16 = DTLDefaultGameFiFeeSharePoolBPS
	ConfigDTLGameFiFeeShareFromLendingBPS uint16 = DTLDefaultGameFiFeeShareLendingBPS
	ConfigDTLGameFiDuelWinPoints          uint64 = DTLDefaultGameFiDuelWinPoints
	ConfigDTLGameFiTournamentWinPoints    uint64 = DTLDefaultGameFiTournamentWinPoints
	ConfigDTLGameFiTournamentPartPoints   uint64 = DTLDefaultGameFiTournamentPartPoints
	ConfigDTLFarmMinStakeBlocks           uint64 = DTLDefaultFarmMinStakeBlocks
	ConfigDTLFarmLPPointsPerBlock         uint64 = DTLDefaultFarmLPPointsPerBlock
	ConfigDTLFarmMaxMultiplierBPS         uint16 = DTLDefaultFarmMaxMultiplierBPS
	ConfigDTLGameFiMaxRewardPerSeason     uint64 = DTLDefaultGameFiMaxRewardPerSeason
)

func dtlSolidityLikeRuntimeEnabled() bool {
	return false
}

func dtlV2EnabledAtHeight(height uint64) bool {
	if !ConfigDTLContractsV2Enabled {
		return false
	}
	if ConfigDTLV2ActivationHeight == 0 {
		return true
	}
	return height >= ConfigDTLV2ActivationHeight
}

func dtlCompatRPCSubsetEnabled() bool {
	return ConfigDTLCompatRPCSubsetEnabled
}

func dtlMaxContractCallDepth() int {
	v := int(ConfigDTLMaxContractCallDepth)
	if v < 1 {
		return 1
	}
	return v
}

func dtlBeaconDelayAtHeight(height uint64) uint64 {
	if !dtlV2EnabledAtHeight(height) {
		return 0
	}
	return ConfigDTLGameBeaconDelayBlocks
}

func dtlBytecodeEnabledAtHeight(_ uint64) bool {
	return false
}

func dtlRouterEnabled() bool {
	return ConfigDTLRouterEnabled
}

func dtlRouterMaxHops() int {
	v := int(ConfigDTLRouterMaxHops)
	if v < 1 {
		return 1
	}
	if v > 16 {
		return 16
	}
	return v
}

func dtlRouterDeadlineMaxBlocks() uint64 {
	if ConfigDTLRouterDeadlineMaxBlocks == 0 {
		return DTLDefaultRouterDeadlineMaxBlocks
	}
	return ConfigDTLRouterDeadlineMaxBlocks
}

func dtlRouterMaxPriceImpactBPS() uint16 {
	v := ConfigDTLRouterMaxPriceImpactBPS
	if v == 0 {
		return DTLDefaultRouterMaxPriceImpactBPS
	}
	if v > DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	return v
}

func dtlRouterQuoteMaxPaths() int {
	v := int(ConfigDTLRouterQuoteMaxPaths)
	if v < 1 {
		return int(DTLDefaultRouterQuoteMaxPaths)
	}
	if v > 64 {
		return 64
	}
	return v
}

func dtlDeFiFarmEnabled() bool {
	return ConfigDTLDeFiFarmEnabled
}

func dtlGameFiSeasonEnabled() bool {
	return ConfigDTLGameFiSeasonEnabled
}

func dtlGameFiRewardTokenRef() string {
	return strings.TrimSpace(ConfigDTLGameFiRewardToken)
}

func dtlGameFiSeasonLengthBlocks() uint64 {
	if ConfigDTLGameFiSeasonLengthBlocks == 0 {
		return DTLDefaultGameFiSeasonLengthBlocks
	}
	return ConfigDTLGameFiSeasonLengthBlocks
}

func dtlGameFiClaimGraceBlocks() uint64 {
	if ConfigDTLGameFiClaimGraceBlocks == 0 {
		return DTLDefaultGameFiClaimGraceBlocks
	}
	return ConfigDTLGameFiClaimGraceBlocks
}

func dtlGameFiFeeShareFromPoolBPS() uint16 {
	v := ConfigDTLGameFiFeeShareFromPoolBPS
	if v > DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	return v
}

func dtlGameFiFeeShareFromLendingBPS() uint16 {
	v := ConfigDTLGameFiFeeShareFromLendingBPS
	if v > DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	return v
}

func dtlFarmMaxMultiplierBPS() uint16 {
	v := ConfigDTLFarmMaxMultiplierBPS
	if v < DTLMaxTaxBPS {
		return DTLMaxTaxBPS
	}
	if v > ^uint16(0) {
		return ^uint16(0)
	}
	return v
}

func dtlFarmMinStakeBlocks() uint64 {
	return ConfigDTLFarmMinStakeBlocks
}

func dtlFarmLPPointsPerBlock() uint64 {
	if ConfigDTLFarmLPPointsPerBlock == 0 {
		return 1
	}
	return ConfigDTLFarmLPPointsPerBlock
}

type BlockRequest struct {
	From           uint64 `json:"from"`
	To             uint64 `json:"to"`
	WantSnapshot   bool   `json:"want_snapshot"`
	SnapshotHeight uint64 `json:"snapshot_height,omitempty"`
	BypassAck      bool   `json:"bypass_ack,omitempty"`
}

const (
	BlockSyncProtocol     = "/msc/blocksync/1.0.0"
	HeaderSyncProtocol    = "/msc/headersync/1.0.0"
	SnapshotMetaProtocol  = "/msc/snapshot-meta/1.0.0"
	SnapshotChunkProtocol = "/msc/snapshot-chunk/1.0.0"
)

type BlockResponse struct {
	Blocks   []Block           `json:"blocks"`
	Snapshot *StateSnapshot    `json:"snapshot,omitempty"`
	ExecPool *ExecPoolSnapshot `json:"exec_pool,omitempty"`
}

type SyncBlockHeader struct {
	Height               uint64 `json:"height"`
	BlockHash            string `json:"block_hash"`
	PrevHash             string `json:"prev_hash"`
	StateRoot            string `json:"state_root,omitempty"`
	ValidatorSetHash     string `json:"validator_set_hash,omitempty"`
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	Timestamp            int64  `json:"timestamp,omitempty"`
}

type HeaderSyncLocator struct {
	Height    uint64 `json:"height"`
	BlockHash string `json:"block_hash"`
}

type HeaderSyncRequest struct {
	From     uint64              `json:"from,omitempty"`
	To       uint64              `json:"to,omitempty"`
	Locators []HeaderSyncLocator `json:"locators,omitempty"`
}

type HeaderSyncResponse struct {
	Headers      []SyncBlockHeader `json:"headers,omitempty"`
	CommonHeight uint64            `json:"common_height,omitempty"`
	CommonHash   string            `json:"common_hash,omitempty"`
}

type SnapshotMetaRequest struct {
	Height uint64 `json:"height"`
}

type SnapshotManifest struct {
	Height                uint64            `json:"height"`
	SnapshotHash          string            `json:"snapshot_hash"`
	StateRoot             string            `json:"state_root"`
	StateMerkleRoot       string            `json:"state_merkle_root,omitempty"`
	ValidatorSetHash      string            `json:"validator_set_hash"`
	ValidatorRegistryHash string            `json:"validator_registry_hash"`
	FinalizedHeight       uint64            `json:"finalized_height,omitempty"`
	FinalizedHash         string            `json:"finalized_hash,omitempty"`
	EpochAnchorHash       string            `json:"epoch_anchor_hash,omitempty"`
	FinalityRoot          string            `json:"finality_root,omitempty"`
	ChunkSize             uint64            `json:"chunk_size"`
	ChunkCount            uint64            `json:"chunk_count"`
	ChunkHashes           []string          `json:"chunk_hashes,omitempty"`
	CheckpointProof       map[string]string `json:"checkpoint_proof,omitempty"`
}

type SnapshotMetaResponse struct {
	Height                uint64            `json:"height"`
	SnapshotHash          string            `json:"snapshot_hash"`
	StateRoot             string            `json:"state_root,omitempty"`
	StateMerkleRoot       string            `json:"state_merkle_root,omitempty"`
	ValidatorSetHash      string            `json:"validator_set_hash,omitempty"`
	ValidatorRegistryHash string            `json:"validator_registry_hash,omitempty"`
	FinalizedHeight       uint64            `json:"finalized_height,omitempty"`
	FinalizedHash         string            `json:"finalized_hash,omitempty"`
	EpochAnchorHash       string            `json:"epoch_anchor_hash,omitempty"`
	FinalityRoot          string            `json:"finality_root,omitempty"`
	ChunkSize             uint64            `json:"chunk_size"`
	TotalChunks           uint64            `json:"total_chunks"`
	ChunkHashes           []string          `json:"chunk_hashes,omitempty"`
	CheckpointProof       map[string]string `json:"checkpoint_proof,omitempty"`
	Manifest              *SnapshotManifest `json:"manifest,omitempty"`
	Available             bool              `json:"available"`
}

type SnapshotChunkRequest struct {
	Height uint64 `json:"height"`
	Index  uint64 `json:"index"`
}

type SnapshotChunkResponse struct {
	Height       uint64 `json:"height"`
	Index        uint64 `json:"index"`
	ChunkHash    string `json:"chunk_hash"`
	SnapshotHash string `json:"snapshot_hash"`
	Data         []byte `json:"data"`
}

type PeerDiscovery struct {

	// knownPeers   map[string]*PeerInfo
	seedNodes    []string
	bootstrapURL string
	isRunning    bool
}
type NodeConfig struct {
	ChainID         string   `json:"chain_id"`
	Moniker         string   `json:"moniker"`
	Seeds           []string `json:"seeds"`
	PersistentPeers []string `json:"persistent_peers"`
	MinPeers        int      `json:"min_peers"`
	MaxPeers        int      `json:"max_peers"`

	MinValidators int
}
type SlashEvidence struct {
	ValidatorID string
	Reason      string

	BlockHash string
	Timestamp int64
	Reporter  string
	Height    uint64 // 👈 uint64
	Validator string
}

var KnownValidators = map[string]uint64{} // validator → last seen height
var ValidatorMu sync.Mutex

type StateReceipt struct {
	TxHash             string        `json:"tx_hash"`
	PreStateHash       string        `json:"pre_state_hash"`
	PostStateHash      string        `json:"post_state_hash"`
	DTLTxType          string        `json:"dtl_tx_type,omitempty"`
	ContractID         string        `json:"contract_id,omitempty"`
	RuntimeMode        string        `json:"runtime_mode,omitempty"`
	ContractStandard   string        `json:"contract_standard,omitempty"`
	ContractInterfaces []string      `json:"contract_interfaces,omitempty"`
	ABIHash            string        `json:"abi_hash,omitempty"`
	Upgradeable        bool          `json:"upgradeable,omitempty"`
	ProxyTarget        string        `json:"proxy_target,omitempty"`
	OracleFeedID       string        `json:"oracle_feed_id,omitempty"`
	HealthFactor       uint64        `json:"health_factor,omitempty"`
	RouteHops          uint64        `json:"route_hops,omitempty"`
	RouteTokenIn       string        `json:"route_token_in,omitempty"`
	RouteTokenOut      string        `json:"route_token_out,omitempty"`
	BytecodeFormat     string        `json:"bytecode_format,omitempty"`
	BytecodeHash       string        `json:"bytecode_hash,omitempty"`
	BytecodeSize       uint64        `json:"bytecode_size,omitempty"`
	Compiler           string        `json:"compiler,omitempty"`
	SourceHash         string        `json:"source_hash,omitempty"`
	Logs               []DTLEventLog `json:"logs,omitempty"`
}
type Transaction struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Amount    int    `json:"amount"`
	Nonce     int    `json:"nonce"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	Fee       int    `json:"fee"`

	Expiry   int64 `json:"expiry"` // 🔥 ADD (unix timestamp)
	GasLimit uint64
	// StakeEpochs defines the lock duration (in epochs) for stake operations.
	StakeEpochs uint64 `json:"stake_epochs"`
	// ValidatorPubKey anchors the validator consensus pubkey for TxStake onboarding.
	ValidatorPubKey string `json:"validator_pubkey,omitempty"`
	// EVM sandbox payload (hex encoded; 0x prefix optional).
	EVMCode     string `json:"evm_code,omitempty"`
	EVMInput    string `json:"evm_input,omitempty"`
	EVMGasLimit uint64 `json:"evm_gas_limit,omitempty"`
	// Optional Ethereum-compatible raw transaction envelope for TxEVM.
	EVMRawTx string `json:"evm_raw_tx,omitempty"`
	// Ethereum tx hash (Keccak256 of signed raw tx) for RPC compatibility.
	EVMTxHash string `json:"evm_tx_hash,omitempty"`
	// DTL (Decentralized Token Ledger) envelope for TxDTL.
	DTLTxType         string `json:"dtl_tx_type,omitempty"`
	DTLTokenID        string `json:"dtl_token_id,omitempty"`
	DTLPayload        string `json:"dtl_payload,omitempty"`
	DTLGovernanceCert string `json:"dtl_governance_cert,omitempty"`
	// ValidatorUpdateCert carries threshold-governance approval for
	// validator add/remove transactions.
	ValidatorUpdateCert *ValidatorUpdateCertificate `json:"validator_update_cert,omitempty"`

	// MODEL-3
	TaskID string
	Input  int

	Type TxType

	ChainID string // 🔥 ADD THIS
	Coin    string // e.g. MSC, MSCX

}
type TxType uint8

const (
	TxTransfer TxType = 0
	TxTask     TxType = 1
	TxStake    TxType = 2
	TxVote     TxType = 3
	// TxValidatorUpdate updates validator set via governance tx.
	TxValidatorUpdate TxType = 4
	// TxFaucet issues testnet funds from the reward pool.
	TxFaucet TxType = 5
	// TxUnstake unlocks previously locked stake.
	TxUnstake TxType = 6
	// TxEVM executes Ethereum bytecode in custom sandbox mode.
	TxEVM TxType = 7
	// TxDTL executes native Decentralized Token Ledger operations.
	TxDTL TxType = 8
)

const (
	validatorUpdateAddPrefix    = "add:"
	validatorUpdateRemovePrefix = "remove:"
)

type ValidatorUpdateCertSignature struct {
	SignerID string `json:"signer_id"`
	SigHex   string `json:"sig_hex"`
}

type ValidatorUpdateCertificate struct {
	ParentRegistryHash string                         `json:"parent_registry_hash"`
	ProposalNonce      uint64                         `json:"proposal_nonce"`
	ExpiryHeight       uint64                         `json:"expiry_height"`
	Signatures         []ValidatorUpdateCertSignature `json:"signatures,omitempty"`
}

func parseValidatorUpdateTarget(to string) (action string, id string, ok bool) {
	if strings.HasPrefix(to, validatorUpdateAddPrefix) {
		id = strings.TrimPrefix(to, validatorUpdateAddPrefix)
		if id == "" {
			return "", "", false
		}
		return "add", id, true
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
	Balances map[string]int
	Nonces   map[string]int
	Stakes   map[string]StakeLock
	// ValidatorRewardWallets pins validator rewards to a deterministic wallet.
	// Key is normalized validator ID (uppercase), value is wallet address.
	ValidatorRewardWallets map[string]string `json:"validator_reward_wallets,omitempty"`
	// EVMState stores last deterministic EVM sandbox output hash by logical key.
	EVMState map[string]string `json:"evm_state,omitempty"`
	// EVMCode stores deployed contract bytecode by EVM address (lowercase hex).
	EVMCode map[string]string `json:"evm_code,omitempty"`
	// EVMStorage stores contract storage slots by EVM address.
	EVMStorage map[string]map[string]string `json:"evm_storage,omitempty"`
	// DTL stores native decentralized token ledger state.
	DTL *DTLState `json:"dtl,omitempty"`
	// UsedValidatorUpdateCerts tracks consumed validator-update certificate
	// message hashes so replay is rejected deterministically after restart/sync.
	UsedValidatorUpdateCerts map[string]uint64 `json:"used_validator_update_certs,omitempty"`
}

// StakeLock tracks locked stake for a delegator -> validator pair.
type StakeLock struct {
	ValidatorID string `json:"validator_id"`
	Amount      int    `json:"amount"`
	LockedUntil uint64 `json:"locked_until_epoch"`
	Burned      int    `json:"burned_amount,omitempty"`
}

type Reward struct {
	Worker     int
	Owner      int
	User       int
	Validators int
}
type Validator struct {
	Address          string `json:"address"`
	PubKey           []byte `json:"pubkey"`
	Stake            uint64 `json:"stake"`
	VotingPower      uint64 `json:"voting_power"`
	Status           string `json:"status"`
	JoinHeight       uint64 `json:"join_height"`
	ActivationHeight uint64 `json:"activation_height"`
}
type Task struct {
	TaskID string
	Input  int
}
type Receipt struct {
	TaskID string
	input  int
	Output int
	Hash   string
}
type Block struct {
	Task   Task
	Result int

	Hash string

	Type                  BlockType // 🔒 NOT string
	Signature             []byte
	ID                    uint64
	Round                 uint32
	BlockHash             string
	ExecutionResults      []ExecutionResult
	Height                uint64
	Tasks                 []Task
	Results               []TaskResult
	ResultHash            []byte
	Proposer              string
	Payload               []byte // 👈 यही use होगा
	Transactions          []Transaction
	Receipts              []StateReceipt
	PrevHash              string
	Signatures            []string // validator IDs
	Timestamp             int64
	BlockTime             LogicalClock `json:"block_time"`
	MempoolRoot           string
	ReceiptRoot           string `json:"receipt_root,omitempty"`
	StateRoot             string // 🔐 REQUIRED
	ValidatorSetHash      string `json:"validator_set_hash"`
	ValidatorSetRoot      string `json:"validator_set_root,omitempty"`
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
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
	ConsensusMode       string `json:"consensus_mode,omitempty"`
	QuorumPolicyVersion string `json:"quorum_policy_version,omitempty"`
	ActiveReadyCount    int    `json:"active_ready_count,omitempty"`
	RequiredQuorum      int    `json:"required_quorum,omitempty"`
	StrictQuorum        int    `json:"strict_quorum,omitempty"`

	// Finality commitments bind finalized roots and validator-set commitments
	// into the block header hash. The certificate carries observable quorum
	// evidence for RPC/explorer/wallet clients.
	FinalizedEpoch            uint64                     `json:"finalized_epoch,omitempty"`
	FinalizedHeight           uint64                     `json:"finalized_height,omitempty"`
	FinalizedStateRoot        string                     `json:"finalized_state_root,omitempty"`
	FinalizedValidatorSetHash string                     `json:"finalized_validator_set_hash,omitempty"`
	FinalizedValidatorSetRoot string                     `json:"finalized_validator_set_root,omitempty"`
	EpochAnchorHash           string                     `json:"epoch_anchor_hash,omitempty"`
	PreviousEpochAnchorHash   string                     `json:"previous_epoch_anchor_hash,omitempty"`
	FinalityRoot              string                     `json:"finality_root,omitempty"`
	FinalityCertificate       *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
}

func canonicalActivationHeight(nextValidatorSetHeight uint64, activationHeight uint64) uint64 {
	if activationHeight > 0 {
		return activationHeight
	}
	return nextValidatorSetHeight
}

func blockActivationHeight(block Block) uint64 {
	return canonicalActivationHeight(block.NextValidatorSetHeight, block.ActivationHeight)
}

func snapshotActivationHeight(snapshot *StateSnapshot) uint64 {
	if snapshot == nil {
		return 0
	}
	return canonicalActivationHeight(snapshot.NextValidatorSetHeight, snapshot.ActivationHeight)
}

func peerHelloActivationHeight(hello PeerHello) uint64 {
	return canonicalActivationHeight(hello.ValidatorSetHeight, hello.ActivationHeight)
}

func validatorAnnouncementActivationHeight(ann ValidatorAnnouncement) uint64 {
	return canonicalActivationHeight(ann.ValidatorSetHeight, ann.ActivationHeight)
}

type TaskResult struct {
	TaskID        string // task identifier
	Output        int    // computed output
	ResultHash    string // hash(Output)
	ExecutorID    string // who computed
	Signature     []byte // executor signature
	ExecutedAt    int64  // unix time
	ExecutionTime int64  // ms (optional metrics)

	Hash []byte
}

type MintResult struct {
	To             string
	Amount         int64
	NewTotalSupply int64
}

type ExecutionResult struct {
	Height     uint64
	Round      uint32 `json:"round,omitempty"`
	BlockHash  string
	Signer     string
	ResultHash string
	TxMerkle   string
	Signature  string `json:"sig,omitempty"`
}

type ExecutionResultMsg struct {
	HeightHint    uint64 `json:"height_hint"`
	RoundHint     uint32 `json:"round_hint,omitempty"`
	BlockHashHint string `json:"block_hash_hint,omitempty"`
	SigVersion    uint8  `json:"sig_v,omitempty"`
	ExecHash      string `json:"exec_hash"`
	TxMerkle      string `json:"tx_merkle"`
	Signer        string `json:"signer"`
	Signature     string `json:"sig"`
}

type CommitMsg struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
	Block  Block  `json:"block"`
	From   string `json:"from"`
}

type FraudProof struct {
	BlockID      uint64
	Proposer     string
	ExpectedHash []byte
	ActualHash   []byte
	Reporter     string
}
type BlockType uint8

const (
	BlockTypeGenesis BlockType = iota
	BlockTypeWork              // normal tx execution
	BlockTypeTime              // empty / heartbeat block
	BlockTypeTask              // 🔥 execution / compute block
	BlockTypeReceipt           // execution receipt block (ERB)
)

const (
	BlockTypeWorkk = "work"
	BlockTypeTimek = "time"
)

type Blockchain struct {
	mu     sync.RWMutex
	Blocks []Block
}
type Wallet struct {
	PublicKey ed25519.PublicKey
	Address   string
}

type GenesisValidatorKey struct {
	Address   string `json:"address"`
	PublicKey string `json:"publicKey"`
}

type SecureWallet struct {
	Address   string        `json:"address"`
	PublicKey string        `json:"publicKey"`
	Crypto    EncryptedKey  `json:"crypto"`
	HD        *HDWalletMeta `json:"hd,omitempty"`
}

type HDWalletMeta struct {
	Scheme   string `json:"scheme,omitempty"`
	Path     string `json:"path,omitempty"`
	Purpose  uint32 `json:"purpose,omitempty"`
	CoinType uint32 `json:"coin_type,omitempty"`
	Account  uint32 `json:"account,omitempty"`
	Change   uint32 `json:"change,omitempty"`
	Index    uint32 `json:"index,omitempty"`
}
type EncryptedKey struct {
	Ciphertext string `json:"ciphertext"`
	Nonce      string `json:"nonce"`
	Salt       string `json:"salt"`
	Version    int    `json:"version,omitempty"`
	KDF        string `json:"kdf,omitempty"`
	// Argon2id parameters (v2 encryption format).
	Argon2Time      uint32 `json:"argon2_time,omitempty"`
	Argon2MemoryKiB uint32 `json:"argon2_memory_kib,omitempty"`
	Argon2Threads   uint8  `json:"argon2_threads,omitempty"`
}
type validatorAddrState struct {
	mu   sync.RWMutex
	addr map[string]string // validatorID -> p2p addr
}

var ValidatorAddr = &validatorAddrState{
	addr: make(map[string]string),
}
