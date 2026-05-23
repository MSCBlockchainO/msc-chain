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
	ValidatorPending ValidatorState = "PENDING"
	ValidatorActive  ValidatorState = "ACTIVE"
	ValidatorJailed  ValidatorState = "JAILED"
	ValidatorExited  ValidatorState = "EXITED"
)

var (
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection  = true
	// Legacy compatibility flag kept for config/runtime compatibility.
	// Deterministic selection now always uses VRF-style scoring after stake gating.
	ValidatorEqualChanceSelection = true
	// Production safety: candidates are observation-only and cannot affect
	// execution voting or validator-set membership directly.
	CandidateIsolationMode = true

	ValidatorMinStake                            int64   = 100
	ValidatorStakeCapPct                         float64 = 0.05
	ValidatorActiveSetSize                       int     = 0
	ValidatorActiveSetMode                       string  = "adaptive_committee"
	ValidatorMaxActiveCommittee                  int     = 512
	ValidatorAdaptiveCommitteeLogMult            int     = 16
	ValidatorCommitteeRotationBlocks             uint64  = 32
	ValidatorSelectionActivityWindow             uint64  = 64
	ValidatorSelectionMinSignedBlocks            uint64  = 1
	ValidatorOnboardingGraceBlocks               uint64  = 64
	ValidatorOnboardingMaxNewSlots               int     = 1
	ValidatorOnboardingStrictActivation          bool    = true
	ValidatorSetAutohealMode                     string  = "validator_quorum"
	ValidatorSetAutohealTrustedOnly              bool    = true
	ValidatorSetAutohealNearTipForceAfter        int     = 2
	ValidatorSetAutohealPauseSeconds             uint64  = 6
	ValidatorOnboardingBootstrapLaneEnabled      bool    = true
	ValidatorOnboardingBootstrapMaxNewSlots      int     = 1
	ValidatorOnboardingBootstrapRequireStake     bool    = true
	ValidatorOnboardingBootstrapRequireNotJailed bool    = true
	ValidatorHeartbeatScope                      string  = "committee_only"
	ValidatorUptimeWindow                        uint64  = 1000
	ValidatorReputationRecoveryThreshold         float64 = 0.30
	ValidatorReputationInitial                   float64 = 0.50
	ValidatorLongUptimeThreshold                 float64 = 0.95
	ValidatorRequireStake                        bool    = false
	ValidatorCoreStakeExempt                     bool    = true

	ReputationCorrectDelta    float64 = 0.001
	ReputationMismatchDelta   float64 = -0.005
	ReputationSlashDelta      float64 = -0.10
	ReputationLongUptimeDelta float64 = 0.002

	PenaltyMissedWeight     float64 = 0.001
	PenaltyBadExecWeight    float64 = 0.05
	PenaltyDoubleSignWeight float64 = 1.0
	PenaltyDisconnectWeight float64 = 0.02

	JailDoubleSignBlocks   uint64 = 10000
	JailBadExecutionBlocks uint64 = 2000
	JailMassMissBlocks     uint64 = 500
	MassMissThreshold      uint64 = 500

	ValidatorInactivityPenaltyEnabled        bool   = true
	ValidatorInactivityPenaltyBurnBPS        uint64 = 100
	ValidatorInactivityPenaltyJailBlocks     uint64 = 500
	ValidatorInactivityPenaltyCooldownBlocks uint64 = 50
)

func validatorPassesStakeGate(id string, stake int64) bool {
	_ = id
	return stake >= ValidatorMinStake
}

func validatorOnboardingGraceBlocks() uint64 {
	return ValidatorOnboardingGraceBlocks
}

func validatorOnboardingMaxNewSlots() int {
	if ValidatorOnboardingMaxNewSlots < 0 {
		return 0
	}
	return ValidatorOnboardingMaxNewSlots
}

func validatorOnboardingStrictActivationEnabled() bool {
	return ValidatorOnboardingStrictActivation
}

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

func validatorSetAutohealStrictCoreQuorum() bool {
	// Compatibility shim: legacy "strict_core_quorum" now resolves to the
	// committed validator-set quorum path.
	return normalizeValidatorSetAutohealMode(ValidatorSetAutohealMode) == "validator_quorum"
}

func validatorSetAutohealTrustedOnlyOnMismatchEnabled() bool {
	return ValidatorSetAutohealTrustedOnly
}

func validatorSetAutohealNearTipForceAfter() int {
	if ValidatorSetAutohealNearTipForceAfter <= 0 {
		return 0
	}
	return ValidatorSetAutohealNearTipForceAfter
}

func validatorSetAutohealPauseDuration() time.Duration {
	if ValidatorSetAutohealPauseSeconds == 0 {
		return 0
	}
	return time.Duration(ValidatorSetAutohealPauseSeconds) * time.Second
}

func validatorOnboardingBootstrapLaneEnabled() bool {
	return ValidatorOnboardingBootstrapLaneEnabled
}

func validatorOnboardingBootstrapMaxNewSlots() int {
	if ValidatorOnboardingBootstrapMaxNewSlots < 0 {
		return 0
	}
	return ValidatorOnboardingBootstrapMaxNewSlots
}

func validatorOnboardingBootstrapRequireStake() bool {
	return ValidatorOnboardingBootstrapRequireStake
}

func validatorOnboardingBootstrapRequireNotJailed() bool {
	return ValidatorOnboardingBootstrapRequireNotJailed
}

func validatorStakeGateTransition(id string, oldStake int64, newStake int64) (lost bool, gained bool) {
	oldPass := validatorPassesStakeGate(id, oldStake)
	newPass := validatorPassesStakeGate(id, newStake)
	return oldPass && !newPass, !oldPass && newPass
}

type ValidatorRecord struct {
	ID                          string         `json:"id"`
	ConsensusPubKey             string         `json:"consensus_pubkey,omitempty"`
	GovernanceSigner            bool           `json:"governance_signer,omitempty"`
	Stake                       int64          `json:"validator_stake"`
	Reputation                  float64        `json:"validator_reputation"`
	LastActive                  uint64         `json:"validator_last_active"`
	MissedBlocks                uint64         `json:"validator_missed_blocks"`
	MissedBlocksWindow          uint64         `json:"validator_missed_blocks_window"`
	BadExecution                uint64         `json:"validator_bad_execution"`
	DoubleSign                  uint64         `json:"validator_double_sign"`
	DisconnectPattern           uint64         `json:"validator_disconnect_pattern"`
	Status                      ValidatorState `json:"validator_status"`
	JailUntilHeight             uint64         `json:"validator_jail_until_height"`
	TotalSlashes                uint64         `json:"validator_total_slashes"`
	JoinHeight                  uint64         `json:"validator_join_height"`
	LastScore                   float64        `json:"validator_last_score"`
	UptimeWindowCounter         uint64         `json:"validator_uptime_window_counter"`
	InactivityPenalties         uint64         `json:"validator_inactivity_penalties"`
	LastInactivityPenaltyHeight uint64         `json:"validator_last_inactivity_penalty_height"`
	ActiveHeights               []uint64       `json:"validator_active_heights,omitempty"`
	SignedHeights               []uint64       `json:"validator_signed_heights,omitempty"`
}

type ValidatorRegistry struct {
	mu      sync.RWMutex
	records map[string]*ValidatorRecord
}

func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{records: make(map[string]*ValidatorRecord)}
}

var GlobalValidatorRegistry = NewValidatorRegistry()

func (r *ValidatorRegistry) Ensure(id string, height uint64) *ValidatorRecord {
	if id == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.records[id]; ok {
		return rec
	}
	rec := &ValidatorRecord{
		ID:            id,
		Stake:         0,
		Reputation:    ValidatorReputationInitial,
		Status:        ValidatorPending,
		JoinHeight:    height,
		ActiveHeights: make([]uint64, 0),
		SignedHeights: make([]uint64, 0),
	}
	r.records[id] = rec
	return rec
}

func (r *ValidatorRegistry) Get(id string) (*ValidatorRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.records[id]
	return rec, ok
}

func (r *ValidatorRegistry) All() []*ValidatorRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ValidatorRecord, 0, len(r.records))
	for _, rec := range r.records {
		out = append(out, rec)
	}
	return out
}

func (r *ValidatorRegistry) Snapshot() map[string]ValidatorRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]ValidatorRecord, len(r.records))
	for id, rec := range r.records {
		snap[id] = cloneValidatorRecord(rec)
	}
	return snap
}

func ValidatorRegistrySnapshotHash(snapshot map[string]ValidatorRecord) string {
	if len(snapshot) == 0 {
		return ""
	}
	canonical := make(map[string]string, len(snapshot))
	for key, rec := range snapshot {
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			id = normalizeValidatorID(key)
		}
		if id == "" {
			continue
		}
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
		if existing, ok := canonical[id]; !ok || entry < existing {
			canonical[id] = entry
		}
	}
	if len(canonical) == 0 {
		return ""
	}
	parts := make([]string, 0, len(canonical))
	for _, entry := range canonical {
		parts = append(parts, entry)
	}
	sort.Strings(parts)
	return HashStrings(parts)
}

func (r *ValidatorRegistry) Load(snapshot map[string]ValidatorRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = make(map[string]*ValidatorRecord, len(snapshot))
	for id, rec := range snapshot {
		cloned := rec
		cloned.ActiveHeights = append([]uint64{}, rec.ActiveHeights...)
		cloned.SignedHeights = append([]uint64{}, rec.SignedHeights...)
		r.records[id] = &cloned
	}
}

func cloneValidatorRecord(rec *ValidatorRecord) ValidatorRecord {
	if rec == nil {
		return ValidatorRecord{}
	}
	out := *rec
	out.ActiveHeights = append([]uint64{}, rec.ActiveHeights...)
	out.SignedHeights = append([]uint64{}, rec.SignedHeights...)
	return out
}

func normalizeOnChainValidatorStatus(raw string) ValidatorState {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(ValidatorActive):
		return ValidatorActive
	case string(ValidatorJailed):
		return ValidatorJailed
	case string(ValidatorExited):
		return ValidatorExited
	default:
		return ValidatorPending
	}
}

func stakeInt64FromUint64(v uint64) int64 {
	if v > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(v)
}

func onChainValidatorPubKeyForID(id string) []byte {
	id = normalizeValidatorID(id)
	if id == "" {
		return nil
	}
	validatorPubKeysMu.RLock()
	defer validatorPubKeysMu.RUnlock()
	pub, ok := GenesisValidatorPubKeys[id]
	if !ok || len(pub) == 0 {
		return nil
	}
	out := make([]byte, len(pub))
	copy(out, pub)
	return out
}

func decodeConsensusPubKeyHex(raw string) ([]byte, error) {
	raw = strings.ToLower(strings.TrimSpace(stripHexPrefix(raw)))
	if raw == "" {
		return nil, nil
	}
	pub, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid validator_pubkey")
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid validator_pubkey length")
	}
	return pub, nil
}

func normalizeConsensusPubKeyHex(raw string) string {
	pub, err := decodeConsensusPubKeyHex(raw)
	if err != nil || len(pub) == 0 {
		return ""
	}
	return strings.ToLower(hex.EncodeToString(pub))
}

func validatorRecordConsensusPubKeyHex(rec ValidatorRecord, fallbackID string) string {
	if normalized := normalizeConsensusPubKeyHex(rec.ConsensusPubKey); normalized != "" {
		return normalized
	}
	if fallbackID == "" {
		return ""
	}
	return consensusPubKeyHexForValidatorID(fallbackID)
}

func validatorRecordPubKeyBytes(rec ValidatorRecord, fallbackID string) []byte {
	raw := validatorRecordConsensusPubKeyHex(rec, fallbackID)
	if raw == "" {
		return nil
	}
	pub, err := hex.DecodeString(raw)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil
	}
	return append([]byte(nil), pub...)
}

func validatorConsensusPubKeyHexFromSnapshot(snapshot map[string]ValidatorRecord, id string) string {
	id = normalizeValidatorID(id)
	if id == "" {
		return ""
	}
	if rec, ok := validatorRecordFromStakeSnapshot(snapshot, id); ok {
		if normalized := validatorRecordConsensusPubKeyHex(rec, ""); normalized != "" {
			return normalized
		}
	}
	return consensusPubKeyHexForValidatorID(id)
}

func validatorConsensusPubKeyAnchorSource(snapshot map[string]ValidatorRecord, id string) string {
	id = normalizeValidatorID(id)
	if id == "" {
		return ""
	}
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

func validatorHasAnchoredConsensusPubKeyFromSnapshot(snapshot map[string]ValidatorRecord, id string) bool {
	return validatorConsensusPubKeyAnchorSource(snapshot, id) != ""
}

func validateStakeConsensusPubKey(tx Transaction, snapshot map[string]ValidatorRecord) (string, error) {
	validatorID := normalizeValidatorID(tx.To)
	if validatorID == "" {
		return "", nil
	}
	anchored := validatorConsensusPubKeyHexFromSnapshot(snapshot, validatorID)
	provided, err := decodeConsensusPubKeyHex(tx.ValidatorPubKey)
	if err != nil {
		return "", err
	}
	providedHex := ""
	if len(provided) == ed25519.PublicKeySize {
		providedHex = strings.ToLower(hex.EncodeToString(provided))
	}
	if anchored != "" {
		if providedHex != "" && !strings.EqualFold(providedHex, anchored) {
			return "", fmt.Errorf("validator_pubkey conflicts with anchored consensus pubkey")
		}
		return anchored, nil
	}
	if validatorRecordBootstrapGovernanceSeed(validatorID) {
		return providedHex, nil
	}
	if providedHex == "" {
		return "", errors.New("validator_pubkey required for first non-core stake")
	}
	return providedHex, nil
}

func anchorConsensusPubKeyOnValidatorRecord(rec *ValidatorRecord, id string, provided string) {
	if rec == nil {
		return
	}
	if normalized := normalizeConsensusPubKeyHex(rec.ConsensusPubKey); normalized != "" {
		rec.ConsensusPubKey = normalized
		return
	}
	if normalized := normalizeConsensusPubKeyHex(provided); normalized != "" {
		rec.ConsensusPubKey = normalized
		return
	}
	if trusted := consensusPubKeyHexForValidatorID(id); trusted != "" {
		rec.ConsensusPubKey = trusted
	}
}

func onChainValidatorsFromRegistrySnapshot(snapshot map[string]ValidatorRecord, pendingAdds map[string]uint64, height uint64) map[string]Validator {
	if len(snapshot) == 0 {
		return nil
	}
	out := make(map[string]Validator, len(snapshot))
	for key, rec := range snapshot {
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			id = normalizeValidatorID(key)
		}
		if id == "" {
			continue
		}
		stake := canonicalVotingPowerFromStake(rec.Stake)
		status := strings.ToUpper(strings.TrimSpace(string(rec.Status)))
		if status == "" {
			status = string(ValidatorPending)
		}
		activationHeight := rec.JoinHeight
		if act, ok := pendingAdds[id]; ok && act > 0 {
			activationHeight = act
		} else if rec.LastActive > 0 {
			activationHeight = rec.LastActive
		} else if rec.Status == ValidatorActive {
			activationHeight = height
		}
		pubKey := validatorRecordPubKeyBytes(rec, id)
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

func validatorRegistrySnapshotFromOnChainValidators(state map[string]Validator) map[string]ValidatorRecord {
	if len(state) == 0 {
		return nil
	}
	out := make(map[string]ValidatorRecord, len(state))
	for key, val := range state {
		id := normalizeValidatorID(key)
		if id == "" {
			id = normalizeValidatorID(val.Address)
		}
		if id == "" {
			continue
		}
		stake := val.Stake
		if stake == 0 && val.VotingPower > 0 {
			stake = val.VotingPower
		}
		rec := ValidatorRecord{
			ID:               id,
			ConsensusPubKey:  strings.ToLower(hex.EncodeToString(val.PubKey)),
			GovernanceSigner: containsValidatorID(ConfigAuthCoreValidators, id),
			Stake:            stakeInt64FromUint64(stake),
			Reputation:       ValidatorReputationInitial,
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

func trimHeights(heights []uint64, minHeight uint64) []uint64 {
	if len(heights) == 0 {
		return heights
	}
	idx := 0
	for idx < len(heights) && heights[idx] < minHeight {
		idx++
	}
	if idx == 0 {
		return heights
	}
	trimmed := heights[idx:]
	out := make([]uint64, len(trimmed))
	copy(out, trimmed)
	return out
}

func inactivityPenaltyJailBlocks() uint64 {
	if ValidatorInactivityPenaltyJailBlocks == 0 {
		return JailMassMissBlocks
	}
	return ValidatorInactivityPenaltyJailBlocks
}

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

func inactivityPenaltyBurnBPSForCount(offenses uint64) uint64 {
	base := ValidatorInactivityPenaltyBurnBPS
	if base > 10000 {
		base = 10000
	}
	if base == 0 {
		return 0
	}
	switch inactivityPenaltyTier(offenses) {
	case 1:
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

func inactivityPenaltyJailBlocksForCount(offenses uint64) uint64 {
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

func inactivityPenaltyCooldownBlocks() uint64 {
	if ValidatorInactivityPenaltyCooldownBlocks > 0 {
		return ValidatorInactivityPenaltyCooldownBlocks
	}
	if ValidatorInactiveBlocks > 0 {
		return ValidatorInactiveBlocks
	}
	return 1
}

func MarkValidatorInactivityPenalty(id string, height uint64) bool {
	id = normalizeValidatorID(id)
	if id == "" || height == 0 || !ValidatorInactivityPenaltyEnabled {
		return false
	}

	cooldown := inactivityPenaltyCooldownBlocks()

	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()

	rec := ensureValidatorRecordLocked(id, height)
	if rec == nil {
		return false
	}
	if rec.Status == ValidatorExited {
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

func (r *ValidatorRecord) applyReputationDelta(delta float64) {
	r.Reputation += delta
	if r.Reputation < 0 {
		r.Reputation = 0
	}
	if r.Reputation > 1 {
		r.Reputation = 1
	}
}

func (r *ValidatorRecord) recordActivity(height uint64, signed bool) {
	if height == 0 {
		return
	}
	r.ActiveHeights = append(r.ActiveHeights, height)
	if signed {
		r.SignedHeights = append(r.SignedHeights, height)
		r.LastActive = height
	}
	minHeight := uint64(0)
	if ValidatorUptimeWindow > 0 && height >= ValidatorUptimeWindow {
		minHeight = height - ValidatorUptimeWindow + 1
	}
	r.ActiveHeights = trimHeights(r.ActiveHeights, minHeight)
	r.SignedHeights = trimHeights(r.SignedHeights, minHeight)
	activeCount := uint64(len(r.ActiveHeights))
	signedCount := uint64(len(r.SignedHeights))
	r.UptimeWindowCounter = activeCount
	if activeCount >= signedCount {
		r.MissedBlocksWindow = activeCount - signedCount
	} else {
		r.MissedBlocksWindow = 0
	}
}

func (r *ValidatorRecord) UptimeScore(height uint64) float64 {
	_ = height
	activeCount := uint64(len(r.ActiveHeights))
	if activeCount == 0 {
		return 0
	}
	signedCount := uint64(len(r.SignedHeights))
	return float64(signedCount) / float64(activeCount)
}

func (r *ValidatorRecord) PenaltyScore() float64 {
	missed := float64(r.MissedBlocksWindow) * PenaltyMissedWeight
	badExec := float64(r.BadExecution) * PenaltyBadExecWeight
	doubleSign := float64(r.DoubleSign) * PenaltyDoubleSignWeight
	disconnect := float64(r.DisconnectPattern) * PenaltyDisconnectWeight
	return missed + badExec + doubleSign + disconnect
}

type ValidatorStateMachine struct{}

type ValidatorScoreEngine struct{}

type DynamicValidatorSelector struct {
	ScoreEngine  ValidatorScoreEngine
	StateMachine ValidatorStateMachine
}

var ValidatorSelector = DynamicValidatorSelector{
	ScoreEngine:  ValidatorScoreEngine{},
	StateMachine: ValidatorStateMachine{},
}

func ensureValidatorRecordLocked(id string, height uint64) *ValidatorRecord {
	if id == "" {
		return nil
	}
	if rec, ok := GlobalValidatorRegistry.records[id]; ok {
		return rec
	}
	rec := &ValidatorRecord{
		ID:              id,
		ConsensusPubKey: consensusPubKeyHexForValidatorID(id),
		Stake:           0,
		Reputation:      ValidatorReputationInitial,
		Status:          ValidatorPending,
		JoinHeight:      height,
		ActiveHeights:   make([]uint64, 0),
		SignedHeights:   make([]uint64, 0),
	}
	GlobalValidatorRegistry.records[id] = rec
	return rec
}

func (ValidatorStateMachine) Update(rec *ValidatorRecord, height uint64) {
	if rec == nil {
		return
	}
	if rec.Status == ValidatorExited {
		return
	}
	if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
		rec.Status = ValidatorJailed
		return
	}
	if rec.JoinHeight > 0 && height < rec.JoinHeight {
		rec.Status = ValidatorPending
		return
	}
	if rec.Status == ValidatorJailed {
		if height >= rec.JailUntilHeight && rec.Reputation >= ValidatorReputationRecoveryThreshold {
			rec.Status = ValidatorActive
			rec.JailUntilHeight = 0
		}
		return
	}
	if !validatorPassesStakeGate(rec.ID, rec.Stake) {
		rec.Status = ValidatorPending
		return
	}
	if rec.Reputation < ValidatorReputationRecoveryThreshold {
		rec.Status = ValidatorPending
		return
	}
	rec.Status = ValidatorActive
}

func (ValidatorScoreEngine) Score(rec *ValidatorRecord, maxStakeScore float64, maxPenalty float64) float64 {
	if rec == nil {
		return 0
	}
	capStake := int64(float64(FixedTotalSupply) * ValidatorStakeCapPct)
	effective := rec.Stake
	if capStake > 0 && effective > capStake {
		effective = capStake
	}
	stakeScore := math.Log(float64(effective) + 1)
	stakeNorm := normalizeScore(stakeScore, maxStakeScore)
	uptime := rec.UptimeScore(rec.LastActive)
	penalty := normalizeScore(rec.PenaltyScore(), maxPenalty)

	finalScore := 0.45*stakeNorm + 0.25*rec.Reputation + 0.20*uptime - 0.10*penalty
	if finalScore < 0 {
		finalScore = 0
	}
	return finalScore
}

func normalizeScore(value float64, max float64) float64 {
	if max <= 0 {
		return 0
	}
	if value <= 0 {
		return 0
	}
	return value / max
}

func (s DynamicValidatorSelector) Select(height uint64, target int) []string {
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
	for _, rec := range GlobalValidatorRegistry.records {
		s.StateMachine.Update(rec, height)
		if isValidatorBanned(rec.ID) {
			continue
		}
		if rec.Status == ValidatorExited {
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

	maxStakeScore := 0.0
	maxPenalty := 0.0
	capStake := int64(float64(FixedTotalSupply) * ValidatorStakeCapPct)
	for _, rec := range records {
		effective := rec.Stake
		if capStake > 0 && effective > capStake {
			effective = capStake
		}
		stakeScore := math.Log(float64(effective) + 1)
		if stakeScore > maxStakeScore {
			maxStakeScore = stakeScore
		}
		penalty := rec.PenaltyScore()
		if penalty > maxPenalty {
			maxPenalty = penalty
		}
	}

	scored := make([]*ValidatorRecord, 0, len(records))
	for _, rec := range records {
		if rec.Status != ValidatorActive {
			continue
		}
		rec.LastScore = s.ScoreEngine.Score(rec, maxStakeScore, maxPenalty)
		scored = append(scored, rec)
	}

	if !CandidateIsolationMode && len(scored) < target {
		for _, rec := range records {
			if rec.Status == ValidatorActive {
				continue
			}
			if validatorPassesStakeGate(rec.ID, rec.Stake) && rec.Reputation >= ValidatorReputationRecoveryThreshold {
				rec.LastScore = s.ScoreEngine.Score(rec, maxStakeScore, maxPenalty)
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

	out := make([]string, 0, len(scored))
	for _, rec := range scored {
		out = append(out, rec.ID)
	}
	sort.Strings(out)
	return out
}

func ApplyValidatorStake(id string, amount int64, height uint64) {
	if id == "" || amount == 0 {
		return
	}
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	rec := ensureValidatorRecordLocked(id, height)
	if rec == nil {
		return
	}
	rec.Stake += amount
	if rec.Stake < 0 {
		rec.Stake = 0
	}
}

func ApplyValidatorPenalty(id string, reason string, height uint64) {
	if id == "" {
		return
	}
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	rec := ensureValidatorRecordLocked(id, height)
	if rec == nil {
		return
	}
	rec.TotalSlashes++
	rec.applyReputationDelta(ReputationSlashDelta)

	switch reason {
	case "double_proposal", "double_sign":
		rec.DoubleSign++
		rec.JailUntilHeight = height + JailDoubleSignBlocks
		rec.Status = ValidatorJailed
	case "invalid_block", "invalid_proposer", "fake_tx_execution", "exec_mismatch", "exec_equivocation":
		rec.BadExecution++
		rec.JailUntilHeight = height + JailBadExecutionBlocks
		rec.Status = ValidatorJailed
	case "censorship_threshold_exceeded", "verified_censorship", "systematic_censorship":
		rec.BadExecution++
		if rec.JailUntilHeight < height+JailBadExecutionBlocks {
			rec.JailUntilHeight = height + JailBadExecutionBlocks
			rec.Status = ValidatorJailed
		}
	case "offline_inactive", "inactive":
		rec.DisconnectPattern++
		jail := inactivityPenaltyJailBlocksForCount(rec.InactivityPenalties)
		if jail > 0 && rec.JailUntilHeight < height+jail {
			rec.JailUntilHeight = height + jail
			rec.Status = ValidatorJailed
		}
	}
	severeSlash := reason == "double_proposal" ||
		reason == "double_sign" ||
		reason == "invalid_block" ||
		reason == "invalid_proposer" ||
		reason == "fake_tx_execution" ||
		reason == "exec_mismatch" ||
		reason == "exec_equivocation" ||
		reason == "censorship_threshold_exceeded" ||
		reason == "verified_censorship" ||
		reason == "systematic_censorship"
	// Hard safety: repeated severe slashes permanently eject validator.
	// Offline/inactivity penalties remain recoverable via jail/cooldown.
	if severeSlash && rec.TotalSlashes >= SevereSlashExitAfter {
		rec.Status = ValidatorExited
		rec.JailUntilHeight = 0
		rec.JoinHeight = 0
	}
}

func UpdateValidatorStates(height uint64) {
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	for _, rec := range GlobalValidatorRegistry.records {
		ValidatorStateMachine{}.Update(rec, height)
	}
}

func (n *Node) UpdateValidatorMetricsFromBlock(block Block) {
	height := block.ID
	if height == 0 {
		return
	}

	activeSet := n.GetConsensusValidators(int(height))
	activeMap := make(map[string]struct{}, len(activeSet))
	for _, id := range activeSet {
		activeMap[id] = struct{}{}
	}

	// Use execution-result signers for real liveness.
	// Fallback to block signatures if results are missing (legacy blocks).
	signerMap := make(map[string]struct{})
	if len(block.ExecutionResults) > 0 {
		for _, res := range block.ExecutionResults {
			if res.Signer != "" {
				signerMap[res.Signer] = struct{}{}
			}
		}
	} else {
		for _, signer := range block.Signatures {
			if signer != "" {
				signerMap[signer] = struct{}{}
			}
		}
	}

	pendingAdds := make(map[string]uint64)
	pendingDrops := make(map[string]uint64)

	GlobalValidatorRegistry.mu.Lock()
	for _, tx := range block.Transactions {
		if tx.Type != TxStake && tx.Type != TxUnstake {
			continue
		}
		vid := strings.TrimSpace(tx.To)
		if vid == "" {
			continue
		}
		rec := ensureValidatorRecordLocked(vid, height)
		if rec == nil {
			continue
		}
		if tx.Type == TxStake {
			anchorConsensusPubKeyOnValidatorRecord(rec, vid, tx.ValidatorPubKey)
		}
		oldStake := rec.Stake
		delta := int64(tx.Amount)
		if tx.Type == TxUnstake {
			delta = -delta
		}
		rec.Stake += delta
		if rec.Stake < 0 {
			rec.Stake = 0
		}
		lostStakeGate, gainedStakeGate := validatorStakeGateTransition(vid, oldStake, rec.Stake)
		if lostStakeGate {
			rec.JoinHeight = 0
			pendingDrops[vid] = height + validatorSetActivationDelayBlocks()
		} else if gainedStakeGate {
			activationHeight := height + validatorSetActivationDelayBlocks()
			if rec.JoinHeight == 0 || rec.JoinHeight <= height || activationHeight < rec.JoinHeight {
				rec.JoinHeight = activationHeight
			}
			pendingAdds[vid] = rec.JoinHeight
		}
	}

	for _, id := range activeSet {
		rec := ensureValidatorRecordLocked(id, height)
		if rec == nil {
			continue
		}
		_, signed := signerMap[id]
		rec.recordActivity(height, signed)
		if signed {
			rec.applyReputationDelta(ReputationCorrectDelta)
		} else {
			rec.MissedBlocks++
			rec.applyReputationDelta(ReputationMismatchDelta)
		}
		if rec.UptimeScore(height) >= ValidatorLongUptimeThreshold {
			rec.applyReputationDelta(ReputationLongUptimeDelta)
		}
		if rec.MissedBlocksWindow >= MassMissThreshold {
			rec.JailUntilHeight = height + JailMassMissBlocks
			rec.Status = ValidatorJailed
		}
		// In deterministic mode, inactivity removals are derived from finalized
		// chain data in queueDeterministicInactiveRemovals to keep all nodes identical.
		if !DeterministicValidatorSelection && ValidatorInactiveBlocks > 0 && rec.MissedBlocksWindow >= ValidatorInactiveBlocks {
			activationHeight := height + validatorSetActivationDelayBlocks()
			if existing, ok := pendingDrops[id]; !ok || activationHeight < existing {
				pendingDrops[id] = activationHeight
			}
			if ValidatorInactivePermanentRemove {
				rec.Status = ValidatorExited
				rec.JailUntilHeight = 0
				rec.JoinHeight = 0
			}
		}
		ValidatorStateMachine{}.Update(rec, height)
	}

	for signer := range signerMap {
		if _, ok := activeMap[signer]; ok {
			continue
		}
		rec := ensureValidatorRecordLocked(signer, height)
		if rec == nil {
			continue
		}
		rec.recordActivity(height, true)
		rec.applyReputationDelta(ReputationCorrectDelta)
		ValidatorStateMachine{}.Update(rec, height)
	}

	for _, rec := range GlobalValidatorRegistry.records {
		ValidatorStateMachine{}.Update(rec, height)
	}
	GlobalValidatorRegistry.mu.Unlock()

	for _, id := range canonicalValidatorIDsFromMapKeys(pendingAdds) {
		n.queuePendingValidator(id, pendingAdds[id])
	}
	for _, id := range canonicalValidatorIDsFromMapKeys(pendingDrops) {
		n.queuePendingValidatorRemoval(id, pendingDrops[id])
	}
}

func (n *Node) applySnapshotValidatorRegistry(snapshot StateSnapshot) {
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
			ids := canonicalValidatorIDsFromMapKeys(snapshot.Validators)
			bootstrapValidatorRegistry(ids, snapshot.Height)
		}
	}

	// Snapshot validator_registry may be stale around stake transitions.
	// Reconcile stake from ledger locks to avoid onboarding starvation ("no_stake")
	// when ledger already contains valid stake proofs.
	n.reconcileSnapshotRegistryStakeFromLedger(snapshot.Ledger, snapshot.Height)
}

func (n *Node) reconcileSnapshotRegistryStakeFromLedger(ledger Ledger, height uint64) {
	if len(ledger.Stakes) == 0 {
		return
	}
	stakeTotals := make(map[string]int64)
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		keyID := normalizeValidatorID(parts[1])
		recID := normalizeValidatorID(rec.ValidatorID)
		if keyID == "" || recID == "" || keyID != recID {
			continue
		}
		stakeTotals[keyID] += int64(rec.Amount)
	}
	if len(stakeTotals) == 0 {
		return
	}

	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	for id, total := range stakeTotals {
		rec, ok := GlobalValidatorRegistry.records[id]
		if !ok || rec == nil {
			rec = &ValidatorRecord{
				ID:              id,
				ConsensusPubKey: consensusPubKeyHexForValidatorID(id),
				Stake:           0,
				Reputation:      ValidatorReputationInitial,
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
		if rec.Stake != total {
			rec.Stake = total
		}
		ValidatorStateMachine{}.Update(rec, height)
	}
}

func bootstrapValidatorRegistry(ids []string, height uint64) {
	if len(ids) == 0 {
		return
	}
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()
	for _, id := range ids {
		if id == "" {
			continue
		}
		rec, ok := GlobalValidatorRegistry.records[id]
		if !ok {
			rec = &ValidatorRecord{
				ID:               id,
				ConsensusPubKey:  consensusPubKeyHexForValidatorID(id),
				GovernanceSigner: validatorRecordBootstrapGovernanceSeed(id),
				Stake:            0,
				Reputation:       ValidatorReputationInitial,
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
			rec.Stake = ValidatorMinStake
		}
		ValidatorStateMachine{}.Update(rec, height)
	}
}

func validatorRecordBootstrapGovernanceSeed(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	seeds := canonicalValidatorIDs(ConfigAuthCoreValidators)
	return containsValidatorID(seeds, id)
}

func consensusPubKeyHexForValidatorID(id string) string {
	id = normalizeValidatorID(id)
	if id == "" {
		return ""
	}
	pub := onChainValidatorPubKeyForID(id)
	if len(pub) != ed25519.PublicKeySize {
		return ""
	}
	return strings.ToLower(hex.EncodeToString(pub))
}

func (n *Node) activeSetTarget() int {
	if n == nil {
		return minActiveValidatorsFloor()
	}
	if ValidatorActiveSetSize < 0 {
		return ValidatorActiveSetSize
	}
	floor := minActiveValidatorsFloor()
	if ValidatorActiveSetSize > 0 {
		if ValidatorActiveSetSize < floor {
			return floor
		}
		return ValidatorActiveSetSize
	}
	if len(n.GenesisValidators) > 0 {
		if len(n.GenesisValidators) < floor {
			return floor
		}
		return len(n.GenesisValidators)
	}
	if GlobalConfig.MinValidators > 0 {
		if GlobalConfig.MinValidators < floor {
			return floor
		}
		return GlobalConfig.MinValidators
	}
	return floor
}

func normalizeActiveSetMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "adaptive_committee":
		return "adaptive_committee"
	default:
		return "adaptive_committee"
	}
}

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

func committeeRotationBlocks() uint64 {
	if ValidatorCommitteeRotationBlocks == 0 {
		return 32
	}
	return ValidatorCommitteeRotationBlocks
}

func validatorSelectionActivityWindowBlocks() uint64 {
	if ValidatorSelectionActivityWindow == 0 {
		return 64
	}
	return ValidatorSelectionActivityWindow
}

func validatorSelectionMinSignedBlocks() uint64 {
	if ValidatorSelectionMinSignedBlocks == 0 {
		return 1
	}
	return ValidatorSelectionMinSignedBlocks
}

func (n *Node) adaptiveCommitteeTarget(eligible int) int {
	floor := minActiveValidatorsFloor()
	if eligible <= 0 {
		return floor
	}

	mult := ValidatorAdaptiveCommitteeLogMult
	if mult <= 0 {
		mult = 16
	}
	raw := int(math.Ceil(float64(mult) * math.Log2(float64(eligible+1))))
	if raw < floor {
		raw = floor
	}

	maxCommittee := ValidatorMaxActiveCommittee
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

func (n *Node) buildAdaptiveCommittee(height uint64, eligible []string, target int) []string {
	stakeSnapshot := map[string]ValidatorRecord(nil)
	if n != nil {
		stakeSnapshot = n.validatorRegistrySnapshotForHeight(height)
	}
	if len(stakeSnapshot) == 0 {
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

	rotation := committeeRotationBlocks()
	rotationBucket := uint64(0)
	if rotation > 0 {
		rotationBucket = height / rotation
	}
	prevHash := ""
	if n != nil && height > 0 {
		prevHash = strings.TrimSpace(n.expectedValidatorSetHash(height - 1))
	}
	if prevHash == "" {
		prevHash = ValidatorSetHash(eligible)
	}

	setHash := ValidatorSetHash(eligible)
	seed := HashStrings([]string{
		"committee_vrf_v1",
		ChainID,
		strconv.FormatUint(rotationBucket, 10),
		prevHash,
		setHash,
	})

	type scored struct {
		id    string
		score string
		stake int64
	}
	scoredIDs := make([]scored, 0, len(eligible))
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

	out := make([]string, 0, len(scoredIDs))
	for _, item := range scoredIDs {
		out = append(out, item.id)
	}
	return canonicalValidatorIDs(out)
}

func openValidatorSetEnabled() bool {
	if DeterministicValidatorSelection {
		return false
	}
	return DynamicValidatorSelectionEnabled && TestingRelaxedPromotion
}

func selectAllStakedValidators(height uint64) []string {
	GlobalValidatorRegistry.mu.Lock()
	defer GlobalValidatorRegistry.mu.Unlock()

	out := make([]string, 0, len(GlobalValidatorRegistry.records))
	stakes := make(map[string]ValidatorRecord, len(GlobalValidatorRegistry.records))
	for _, rec := range GlobalValidatorRegistry.records {
		if isValidatorBanned(rec.ID) {
			continue
		}
		ValidatorStateMachine{}.Update(rec, height)
		if rec.Status == ValidatorExited {
			continue
		}
		if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
			continue
		}
		if rec.JoinHeight > 0 && height < rec.JoinHeight {
			continue
		}
		if !validatorPassesStakeGate(rec.ID, rec.Stake) {
			continue
		}
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
		stakes[id] = ValidatorRecord{ID: id, Stake: rec.Stake}
	}
	return deterministicStakeHashOrderedValidatorIDs(out, stakes)
}

func selectAllStakedValidatorsFromSnapshot(height uint64, snapshot map[string]ValidatorRecord) []string {
	if len(snapshot) == 0 {
		return nil
	}
	out := make([]string, 0, len(snapshot))
	for _, rec := range snapshot {
		if isValidatorBanned(rec.ID) {
			continue
		}
		if rec.Status == ValidatorExited {
			continue
		}
		if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
			continue
		}
		if rec.JoinHeight > 0 && height < rec.JoinHeight {
			continue
		}
		if !validatorPassesStakeGate(rec.ID, rec.Stake) {
			continue
		}
		out = append(out, rec.ID)
	}
	return canonicalValidatorIDs(out)
}

func selectDeterministicValidatorsFromSnapshot(height uint64, target int, snapshot map[string]ValidatorRecord) []string {
	if len(snapshot) == 0 {
		return nil
	}
	unlimited := false
	if target < 0 {
		unlimited = true
	} else if target == 0 {
		target = 1
	}

	ids := make([]string, 0, len(snapshot))
	for _, rec := range snapshot {
		if isValidatorBanned(rec.ID) {
			continue
		}
		if rec.Status == ValidatorExited {
			continue
		}
		if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
			continue
		}
		if rec.JoinHeight > 0 && height < rec.JoinHeight {
			continue
		}
		if !validatorPassesStakeGate(rec.ID, rec.Stake) {
			continue
		}
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

	rotationWindow := ValidatorSetRotationWindow
	if rotationWindow == 0 {
		rotationWindow = 1
	}
	rotationBucket := height / rotationWindow
	setHash := ValidatorSetHash(ids)
	seed := HashStrings([]string{
		"validator_vrf_v1",
		ChainID,
		strconv.FormatUint(rotationBucket, 10),
		setHash,
	})

	type scored struct {
		id    string
		score string
	}
	scoredIDs := make([]scored, 0, len(ids))
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

	out := make([]string, 0, len(scoredIDs))
	for _, e := range scoredIDs {
		out = append(out, e.id)
	}
	return canonicalValidatorIDs(out)
}

func (n *Node) GetDynamicValidators(height int) []string {
	if height <= 0 {
		if n != nil && n.Blockchain != nil {
			height = int(n.Blockchain.Height() + 1)
		} else {
			height = 1
		}
	}
	registrySnap := map[string]ValidatorRecord(nil)
	if n != nil {
		registrySnap = n.validatorRegistrySnapshotForHeight(uint64(height))
	}
	if len(registrySnap) == 0 {
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
		if out := selectAllStakedValidatorsFromSnapshot(uint64(height), registrySnap); len(out) > 0 {
			return canonicalValidatorIDs(out)
		}
	}
	target := n.activeSetTarget()
	if DeterministicValidatorSelection {
		if out := SelectValidatorsFromRegistrySnapshot(uint64(height), target, registrySnap); len(out) > 0 {
			return canonicalValidatorIDs(out)
		}
		return nil
	}
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
		if out := selectAllStakedValidatorsFromSnapshot(height, snapshot); len(out) > 0 {
			return canonicalValidatorIDs(out)
		}
	}
	if DeterministicValidatorSelection {
		return canonicalValidatorIDs(selectDeterministicValidatorsFromSnapshot(height, target, snapshot))
	}
	unlimited := false
	if target < 0 {
		unlimited = true
	} else if target == 0 {
		target = 1
	}

	records := make([]*ValidatorRecord, 0, len(snapshot))
	for _, recVal := range snapshot {
		rec := recVal
		rec.ActiveHeights = append([]uint64{}, recVal.ActiveHeights...)
		rec.SignedHeights = append([]uint64{}, recVal.SignedHeights...)
		ValidatorStateMachine{}.Update(&rec, height)
		if isValidatorBanned(rec.ID) {
			continue
		}
		if rec.Status == ValidatorExited {
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

	maxStakeScore := 0.0
	maxPenalty := 0.0
	capStake := int64(float64(FixedTotalSupply) * ValidatorStakeCapPct)
	for _, rec := range records {
		effective := rec.Stake
		if capStake > 0 && effective > capStake {
			effective = capStake
		}
		stakeScore := math.Log(float64(effective) + 1)
		if stakeScore > maxStakeScore {
			maxStakeScore = stakeScore
		}
		penalty := rec.PenaltyScore()
		if penalty > maxPenalty {
			maxPenalty = penalty
		}
	}

	scored := make([]*ValidatorRecord, 0, len(records))
	for _, rec := range records {
		if rec.Status != ValidatorActive {
			continue
		}
		rec.LastScore = ValidatorScoreEngine{}.Score(rec, maxStakeScore, maxPenalty)
		scored = append(scored, rec)
	}

	if !CandidateIsolationMode && len(scored) < target {
		for _, rec := range records {
			if rec.Status == ValidatorActive {
				continue
			}
			if validatorPassesStakeGate(rec.ID, rec.Stake) && rec.Reputation >= ValidatorReputationRecoveryThreshold {
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

	out := make([]string, 0, len(scored))
	for _, rec := range scored {
		out = append(out, rec.ID)
	}
	return canonicalValidatorIDs(out)
}
