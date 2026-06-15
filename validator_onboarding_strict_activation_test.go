package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	libp2p "github.com/libp2p/go-libp2p"
)

func withOnboardingStrictActivationGlobals(t *testing.T) func() {
	t.Helper()

	oldDynamic := DynamicValidatorSelectionEnabled
	oldDeterministic := DeterministicValidatorSelection
	oldMode := ValidatorActiveSetMode
	oldMaxCommittee := ValidatorMaxActiveCommittee
	oldLogMult := ValidatorAdaptiveCommitteeLogMult
	oldMinActive := ValidatorMinActiveSet
	oldActivityWindow := ValidatorSelectionActivityWindow
	oldMinSigned := ValidatorSelectionMinSignedBlocks
	oldOnboardingGrace := ValidatorOnboardingGraceBlocks
	oldOnboardingSlots := ValidatorOnboardingMaxNewSlots
	oldOnboardingStrict := ValidatorOnboardingStrictActivation
	oldActivationModelV2Height := ValidatorSetActivationModelV2Height
	oldBootstrapEnabled := ValidatorOnboardingBootstrapLaneEnabled
	oldBootstrapSlots := ValidatorOnboardingBootstrapMaxNewSlots
	oldBootstrapStake := ValidatorOnboardingBootstrapRequireStake
	oldBootstrapJailed := ValidatorOnboardingBootstrapRequireNotJailed
	oldMinStake := ValidatorMinStake
	oldRequireStake := ValidatorRequireStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldAuthRequireWallet := ConfigAuthRequireWallet
	oldGenesisHashExpected := GenesisHashExpected
	oldGenesisHash := GenesisHash
	oldChainID := ChainID
	authMu.Lock()
	oldAuthReady := authReady
	oldAuthNodeID := authNodeID
	oldAuthWalletAddr := authWalletAddr
	oldAuthWalletPub := authWalletPub
	authMu.Unlock()
	oldTTL := ValidatorLivenessHeartbeatTTLSeconds
	oldGrace := ValidatorLivenessGraceSeconds
	oldDrift := ValidatorLivenessMaxHeightDriftBlocks
	oldSyncSnapshotThreshold := SyncSnapshotCatchupThresholdBlocks
	oldSyncDeltaMax := SyncDeltaReplayMaxBlocks
	oldSyncChunkSize := SyncSnapshotChunkSizeBytes
	oldSyncParallel := SyncSnapshotParallelChunks
	oldSyncStall := SyncStallSeconds
	oldSyncPeerTimeout := SyncPeerTimeoutSeconds
	oldSyncHistoryMode := SyncHistoryMode
	oldSyncRequireProof := SyncTrustedSnapshotRequireCheckpointProof
	oldSyncAnchorTimeout := SyncSnapshotAnchorTimeoutSeconds
	oldSyncAnchorMaxRetries := SyncSnapshotAnchorMaxRetries
	oldSyncCheckpointInterval := SyncCheckpointIntervalBlocks
	oldSyncCheckpointDomain := SyncSnapshotCheckpointDomain
	oldSyncCheckpointV2Height := SyncSnapshotCheckpointV2Height
	oldSyncSessionTTL := SyncSnapshotSessionTTLSeconds
	oldSyncQuorumApplyWatchdog := SyncSnapshotQuorumApplyWatchdogSeconds
	oldSyncSessionResetWatchdog := SyncSnapshotSessionResetWatchdogSeconds
	oldSyncInvalidProofQuarantine := SyncSnapshotInvalidProofQuarantineAfter
	oldBarrierRetryMode := TransitionBarrierRetryMode
	oldIsTestnet := IsTestnet
	oldBanned := append([]string{}, ValidatorBannedList...)
	oldRegistry := GlobalValidatorRegistry.Snapshot()

	runtimeCoreValidatorSet.mu.RLock()
	oldRuntimeCore := make([]string, 0, len(runtimeCoreValidatorSet.ids))
	for id := range runtimeCoreValidatorSet.ids {
		oldRuntimeCore = append(oldRuntimeCore, id)
	}
	runtimeCoreValidatorSet.mu.RUnlock()
	sort.Strings(oldRuntimeCore)

	genesisRewardWalletsMu.RLock()
	oldGenesisRewardWallets := make(map[string]string, len(genesisRewardWallets))
	for id, wallet := range genesisRewardWallets {
		oldGenesisRewardWallets[id] = wallet
	}
	genesisRewardWalletsMu.RUnlock()

	genesisBootstrapWalletsMu.RLock()
	oldGenesisBootstrapWallets := make(map[string]genesisBootstrapWalletBinding, len(genesisBootstrapWalletBindings))
	for id, binding := range genesisBootstrapWalletBindings {
		oldGenesisBootstrapWallets[id] = binding
	}
	genesisBootstrapWalletsMu.RUnlock()

	validatorPubKeysMu.RLock()
	oldPub := make(map[string]ed25519.PublicKey, len(ValidatorPubKeys))
	for id, pub := range ValidatorPubKeys {
		oldPub[id] = append(ed25519.PublicKey(nil), pub...)
	}
	oldGenesisPub := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pub := range GenesisValidatorPubKeys {
		oldGenesisPub[id] = append(ed25519.PublicKey(nil), pub...)
	}
	validatorPubKeysMu.RUnlock()

	return func() {
		DynamicValidatorSelectionEnabled = oldDynamic
		DeterministicValidatorSelection = oldDeterministic
		ValidatorActiveSetMode = oldMode
		ValidatorMaxActiveCommittee = oldMaxCommittee
		ValidatorAdaptiveCommitteeLogMult = oldLogMult
		ValidatorMinActiveSet = oldMinActive
		ValidatorSelectionActivityWindow = oldActivityWindow
		ValidatorSelectionMinSignedBlocks = oldMinSigned
		ValidatorOnboardingGraceBlocks = oldOnboardingGrace
		ValidatorOnboardingMaxNewSlots = oldOnboardingSlots
		ValidatorOnboardingStrictActivation = oldOnboardingStrict
		ValidatorSetActivationModelV2Height = oldActivationModelV2Height
		ValidatorOnboardingBootstrapLaneEnabled = oldBootstrapEnabled
		ValidatorOnboardingBootstrapMaxNewSlots = oldBootstrapSlots
		ValidatorOnboardingBootstrapRequireStake = oldBootstrapStake
		ValidatorOnboardingBootstrapRequireNotJailed = oldBootstrapJailed
		ValidatorMinStake = oldMinStake
		ValidatorRequireStake = oldRequireStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		ConfigAuthRequireWallet = oldAuthRequireWallet
		GenesisHashExpected = oldGenesisHashExpected
		GenesisHash = oldGenesisHash
		ChainID = oldChainID
		setGenesisRewardWallets(oldGenesisRewardWallets)
		setGenesisBootstrapWalletBindings(oldGenesisBootstrapWallets)
		authMu.Lock()
		authReady = oldAuthReady
		authNodeID = oldAuthNodeID
		authWalletAddr = oldAuthWalletAddr
		authWalletPub = oldAuthWalletPub
		authMu.Unlock()
		ValidatorLivenessHeartbeatTTLSeconds = oldTTL
		ValidatorLivenessGraceSeconds = oldGrace
		ValidatorLivenessMaxHeightDriftBlocks = oldDrift
		SyncSnapshotCatchupThresholdBlocks = oldSyncSnapshotThreshold
		SyncDeltaReplayMaxBlocks = oldSyncDeltaMax
		SyncSnapshotChunkSizeBytes = oldSyncChunkSize
		SyncSnapshotParallelChunks = oldSyncParallel
		SyncStallSeconds = oldSyncStall
		SyncPeerTimeoutSeconds = oldSyncPeerTimeout
		SyncHistoryMode = oldSyncHistoryMode
		SyncTrustedSnapshotRequireCheckpointProof = oldSyncRequireProof
		SyncSnapshotAnchorTimeoutSeconds = oldSyncAnchorTimeout
		SyncSnapshotAnchorMaxRetries = oldSyncAnchorMaxRetries
		SyncCheckpointIntervalBlocks = oldSyncCheckpointInterval
		SyncSnapshotCheckpointDomain = oldSyncCheckpointDomain
		SyncSnapshotCheckpointV2Height = oldSyncCheckpointV2Height
		SyncSnapshotSessionTTLSeconds = oldSyncSessionTTL
		SyncSnapshotQuorumApplyWatchdogSeconds = oldSyncQuorumApplyWatchdog
		SyncSnapshotSessionResetWatchdogSeconds = oldSyncSessionResetWatchdog
		SyncSnapshotInvalidProofQuarantineAfter = oldSyncInvalidProofQuarantine
		TransitionBarrierRetryMode = oldBarrierRetryMode
		IsTestnet = oldIsTestnet
		setValidatorBannedValidators(oldBanned)
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
		GlobalValidatorRegistry.Load(oldRegistry)

		validatorPubKeysMu.Lock()
		ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldPub))
		for id, pub := range oldPub {
			ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisPub))
		for id, pub := range oldGenesisPub {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		validatorPubKeysMu.Unlock()
	}
}

func strictActivationTestPub(seedByte byte) ed25519.PublicKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = seedByte + byte(i)
	}
	key := ed25519.NewKeyFromSeed(seed)
	pub := key.Public().(ed25519.PublicKey)
	return append(ed25519.PublicKey(nil), pub...)
}

func strictActivationTestValidatorKey(seedByte byte, id string) ValidatorKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = seedByte + byte(i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return ValidatorKey{
		ID:         id,
		PublicKey:  append(ed25519.PublicKey(nil), pub...),
		PrivateKey: append(ed25519.PrivateKey(nil), priv...),
	}
}

func configureStrictActivationDefaults() {
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ValidatorActiveSetMode = "adaptive_committee"
	ValidatorMaxActiveCommittee = 512
	ValidatorAdaptiveCommitteeLogMult = 16
	ValidatorMinActiveSet = 3
	ValidatorSelectionActivityWindow = 64
	ValidatorSelectionMinSignedBlocks = 1
	ValidatorOnboardingGraceBlocks = 64
	ValidatorOnboardingMaxNewSlots = 1
	ValidatorOnboardingStrictActivation = true
	ValidatorSetActivationDelay = 5
	ValidatorSetActivationModelV2Height = 0
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	ValidatorOnboardingBootstrapLaneEnabled = false
	ValidatorOnboardingBootstrapMaxNewSlots = 0
	ValidatorOnboardingBootstrapRequireStake = true
	ValidatorOnboardingBootstrapRequireNotJailed = true
	ValidatorMinStake = 1
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorLivenessHeartbeatTTLSeconds = 25
	ValidatorLivenessGraceSeconds = 10
	ValidatorLivenessMaxHeightDriftBlocks = 8
	setValidatorBannedValidators(nil)
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
}

func loadStrictActivationRegistry(joinHeight uint64) {
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", ConsensusPubKey: strings.ToLower(hex.EncodeToString(strictActivationTestPub(11))), Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", ConsensusPubKey: strings.ToLower(hex.EncodeToString(strictActivationTestPub(12))), Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", ConsensusPubKey: strings.ToLower(hex.EncodeToString(strictActivationTestPub(13))), Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", ConsensusPubKey: strings.ToLower(hex.EncodeToString(strictActivationTestPub(14))), Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"F": {ID: "F", Stake: 100, Reputation: 1, Status: ValidatorPending, JoinHeight: joinHeight},
	})
}

func setStrictActivationPubKeys(includeF bool) {
	validatorPubKeysMu.Lock()
	defer validatorPubKeysMu.Unlock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": strictActivationTestPub(11),
		"B": strictActivationTestPub(12),
		"C": strictActivationTestPub(13),
		"D": strictActivationTestPub(14),
	}
	if includeF {
		ValidatorPubKeys["F"] = strictActivationTestPub(15)
	}
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)

	snapshot := GlobalValidatorRegistry.Snapshot()
	rec := snapshot["F"]
	rec.ID = "F"
	if includeF {
		rec.ConsensusPubKey = strings.ToLower(hex.EncodeToString(strictActivationTestPub(15)))
	} else {
		rec.ConsensusPubKey = ""
	}
	snapshot["F"] = rec
	GlobalValidatorRegistry.Load(snapshot)
}

func makeStrictActivationNode(height uint64) *Node {
	bc := NewBlockchain()
	for i, proposer := range []string{"A", "B", "C", "D", "A", "B", "C", "D"} {
		bc.Blocks = append(bc.Blocks, Block{
			ID:       uint64(i + 1),
			Proposer: proposer,
		})
	}
	currentHeight := bc.Height()
	baseSet := []string{"A", "B", "C", "D"}
	baseHash := ValidatorSetHash(baseSet)
	for i := range bc.Blocks {
		bc.Blocks[i].ValidatorSetHash = baseHash
		bc.Blocks[i].NextValidatorSetHash = baseHash
		bc.Blocks[i].NextValidatorSetHeight = bc.Blocks[i].ID + 1
		bc.Blocks[i].ActivationHeight = bc.Blocks[i].ID + 1
	}

	n := &Node{
		ID:                          "A",
		Role:                        "validator",
		ValidatorKey:                strictActivationTestValidatorKey(21, "A"),
		Blockchain:                  &bc,
		validatorStatus:             make(map[string]*ValidatorStatus),
		epochValidators:             map[uint64][]string{height: {"A", "B", "C", "D", "F"}, currentHeight: append([]string{}, baseSet...), currentHeight + 1: append([]string{}, baseSet...)},
		frozenValidatorsByHeight:    make(map[uint64][]string),
		frozenValidatorHashByHeight: make(map[uint64]string),
		candidates:                  make(map[string]*CandidateStatus),
	}
	n.frozenValidatorsByHeight[currentHeight] = append([]string{}, baseSet...)
	n.frozenValidatorHashByHeight[currentHeight] = baseHash
	n.frozenValidatorsByHeight[currentHeight+1] = append([]string{}, baseSet...)
	n.frozenValidatorHashByHeight[currentHeight+1] = baseHash
	now := time.Now()
	for _, id := range []string{"A", "B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			Active:             true,
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height,
			ValidatorSetHeight: height,
		}
	}
	return n
}

func setStrictActivationObservedHeight(n *Node, height uint64) {
	if n == nil {
		return
	}
	for _, st := range n.validatorStatus {
		if st == nil {
			continue
		}
		st.ReportedHeight = height
		st.FinalizedHeight = height
		st.ExecEpoch = height
		st.ValidatorSetHeight = height
	}
}

func containsValidatorIDInSet(ids []string, target string) bool {
	target = normalizeValidatorID(target)
	for _, id := range ids {
		if normalizeValidatorID(id) == target {
			return true
		}
	}
	return false
}

func TestOnboardingStrictGateExcludesNonReadyCandidate(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)

	t.Run("missing_pubkey", func(t *testing.T) {
		setStrictActivationPubKeys(false)
		n := makeStrictActivationNode(height)
		snapshot := GlobalValidatorRegistry.Snapshot()
		ready, reason := n.onboardingActivationReady("F", height, snapshot)
		if ready {
			t.Fatalf("expected F not ready without pubkey")
		}
		if reason != "awaiting_pubkey" {
			t.Fatalf("unexpected reason: got=%s want=awaiting_pubkey", reason)
		}
		got := n.getEligibleSortedValidatorIDs(height, nil)
		if containsValidatorIDInSet(got, "F") {
			t.Fatalf("expected F to be excluded, got=%v", got)
		}
	})

	t.Run("missing_heartbeat", func(t *testing.T) {
		setStrictActivationPubKeys(true)
		n := makeStrictActivationNode(height)
		snapshot := GlobalValidatorRegistry.Snapshot()
		ready, reason := n.onboardingActivationReady("F", height, snapshot)
		if ready {
			t.Fatalf("expected F not ready without heartbeat")
		}
		if reason != "awaiting_heartbeat" {
			t.Fatalf("unexpected reason: got=%s want=awaiting_heartbeat", reason)
		}
		got := n.getEligibleSortedValidatorIDs(height, nil)
		if containsValidatorIDInSet(got, "F") {
			t.Fatalf("expected F to be excluded, got=%v", got)
		}
	})
}

func TestOnboardingStrictGateAdmitsReadyCandidate(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(height)
	n.candidates["F"] = &CandidateStatus{
		ID:              "F",
		LastHeartbeatAt: time.Now(),
	}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("F", height, snapshot)
	if !ready {
		t.Fatalf("expected F ready, got reason=%s", reason)
	}

	got := n.getEligibleSortedValidatorIDs(height, nil)
	if !containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected F admitted once activation-ready, got=%v", got)
	}
}

func TestAdaptiveCommitteeKeepsActiveSmallSetMember(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	snapshot := GlobalValidatorRegistry.Snapshot()
	rec := snapshot["F"]
	rec.Status = ValidatorActive
	rec.JoinHeight = 1
	rec.ConsensusPubKey = strings.ToLower(hex.EncodeToString(strictActivationTestPub(15)))
	snapshot["F"] = rec
	GlobalValidatorRegistry.Load(snapshot)

	n := makeStrictActivationNode(height)
	got := n.getEligibleSortedValidatorIDs(height, nil)
	if !containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected active staked validator to remain in small active set, got=%v", got)
	}
}

func TestOnboardingStrictGateUsesAnchoredSnapshotPubkeyAfterRestart(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	validatorPubKeysMu.Lock()
	ValidatorPubKeys = make(map[string]ed25519.PublicKey)
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)
	validatorPubKeysMu.Unlock()

	n := makeStrictActivationNode(height)
	n.candidates["F"] = &CandidateStatus{
		ID:              "F",
		LastHeartbeatAt: time.Now(),
	}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("F", height, snapshot)
	if !ready {
		t.Fatalf("expected F ready from anchored snapshot pubkey after restart, got reason=%s", reason)
	}

	got := n.getEligibleSortedValidatorIDs(height, nil)
	if !containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected F admitted from snapshot-backed pubkey after restart, got=%v", got)
	}
}

func TestOnboardingStrictGateRejoinFastpathAdmitsPriorMember(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(height)
	// Keep F out-of-set in validatorStatus evaluation heights (8/9), but prove
	// it was recently part of frozen set for deterministic rejoin safety.
	n.validatorStatus["F"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		ReportedHeight:     8,
		FinalizedHeight:    8,
		ExecEpoch:          9,
		ValidatorSetHeight: 9,
	}
	n.frozenValidatorsByHeight[height-1] = []string{"A", "B", "C", "D", "F"}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("F", height, snapshot)
	if !ready {
		t.Fatalf("expected F rejoin fastpath ready, got reason=%s", reason)
	}
	if reason != "rejoin_validator_heartbeat" {
		t.Fatalf("unexpected reason: got=%s want=rejoin_validator_heartbeat", reason)
	}

	got := n.getEligibleSortedValidatorIDs(height, nil)
	if !containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected F admitted via rejoin fastpath, got=%v", got)
	}
}

func TestOnboardingStrictGateRejoinFastpathRequiresRecentFrozenProof(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(height)
	n.validatorStatus["F"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		ReportedHeight:     8,
		FinalizedHeight:    8,
		ExecEpoch:          9,
		ValidatorSetHeight: 9,
	}
	// Deliberately omit F from recent frozen-set history.
	n.frozenValidatorsByHeight[height-1] = []string{"A", "B", "C", "D"}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("F", height, snapshot)
	if ready {
		t.Fatalf("expected F not ready without rejoin proof")
	}
	if reason != "awaiting_rejoin_proof" {
		t.Fatalf("unexpected reason: got=%s want=awaiting_rejoin_proof", reason)
	}

	got := n.getEligibleSortedValidatorIDs(height, nil)
	if containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected F excluded without recent frozen-set proof, got=%v", got)
	}
}

func TestOnboardingStrictGateRejoinFastpathRejectsStaleHeartbeat(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(height)
	n.validatorStatus["F"] = &ValidatorStatus{
		LastSeen:           time.Now().Add(-2 * time.Minute),
		Active:             true,
		ReportedHeight:     8,
		FinalizedHeight:    8,
		ExecEpoch:          9,
		ValidatorSetHeight: 9,
	}
	n.frozenValidatorsByHeight[height-1] = []string{"A", "B", "C", "D", "F"}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("F", height, snapshot)
	if ready {
		t.Fatalf("expected stale rejoin heartbeat to be rejected")
	}
	if reason != "awaiting_heartbeat" {
		t.Fatalf("unexpected reason: got=%s want=awaiting_heartbeat", reason)
	}

	got := n.getEligibleSortedValidatorIDs(height, nil)
	if containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected F excluded with stale heartbeat, got=%v", got)
	}
}

func TestOnboardingStrictGateDisabledKeepsLegacyAdmission(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	ValidatorOnboardingStrictActivation = false
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(height)
	got := n.getEligibleSortedValidatorIDs(height, nil)
	if !containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected F admitted when strict activation gate is disabled, got=%v", got)
	}
}

func TestDeterministicEligibilitySafetyPassDropsUnknownPubKey(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	ValidatorOnboardingStrictActivation = false
	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(false) // F intentionally has no known pubkey.

	n := makeStrictActivationNode(height)
	got := n.getEligibleSortedValidatorIDs(height, nil)

	if containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected deterministic eligibility safety pass to remove unknown-pubkey F, got=%v", got)
	}
	if len(got) == 0 {
		t.Fatalf("eligible committee must remain non-empty after deterministic safety pass")
	}
}

func TestValidatorConfigParsesOnboardingStrictActivation(t *testing.T) {
	var cfg ConfigFile
	if _, err := toml.Decode("[validators]\nonboarding_strict_activation = false\n", &cfg); err != nil {
		t.Fatalf("decode false strict-activation flag: %v", err)
	}
	if cfg.Validators.OnboardingStrictActivation {
		t.Fatalf("expected parsed onboarding_strict_activation=false")
	}

	if _, err := toml.Decode("[validators]\nonboarding_strict_activation = true\n", &cfg); err != nil {
		t.Fatalf("decode true strict-activation flag: %v", err)
	}
	if !cfg.Validators.OnboardingStrictActivation {
		t.Fatalf("expected parsed onboarding_strict_activation=true")
	}
}

func TestConsensusParamsHashIncludesOnboardingStrictActivation(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ValidatorOnboardingStrictActivation = true
	hashTrue := consensusParamsHash()
	ValidatorOnboardingStrictActivation = false
	hashFalse := consensusParamsHash()
	if hashTrue == hashFalse {
		t.Fatalf("consensus params hash must change when onboarding_strict_activation changes")
	}
}

func TestStrictActivationPeerHelloIdentityAndActivationFlip(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	epoch := n.currentEpoch()
	inactiveSet := []string{"B", "C", "D"}
	n.epochValidators[epoch] = append([]string{}, inactiveSet...)
	n.frozenValidatorsByHeight[epoch] = append([]string{}, inactiveSet...)
	n.frozenValidatorHashByHeight[epoch] = ValidatorSetHash(inactiveSet)
	if epoch > 1 {
		parent := &n.Blockchain.Blocks[epoch-2]
		parent.NextValidatorSetHash = ValidatorSetHash(inactiveSet)
		parent.NextValidatorSetHeight = epoch
		parent.ActivationHeight = epoch
	}

	active, reason := n.selfActiveValidatorAt(epoch)
	if active {
		t.Fatalf("expected inactive before frozen-set inclusion, got active reason=%s", reason)
	}
	if reason != "activation_pending_not_in_frozen_set" {
		t.Fatalf("unexpected inactive reason: got=%s want=activation_pending_not_in_frozen_set", reason)
	}

	hello := n.outboundPeerHello()
	if hello.Role != "full" {
		t.Fatalf("expected grace peer role=full, got=%s", hello.Role)
	}
	if hello.ValidatorID != "" || hello.ValidatorPubKey != "" {
		t.Fatalf("expected no validator identity during grace, got id=%q pub=%q", hello.ValidatorID, hello.ValidatorPubKey)
	}
	if hello.ValidatorSetHash != n.stableFrozenHashForAdvertise(epoch) {
		t.Fatalf("expected stable frozen hash advertisement during grace")
	}

	activeSet := []string{"A", "B", "C", "D"}
	n.epochValidators[epoch] = append([]string{}, activeSet...)
	n.frozenValidatorsByHeight[epoch] = append([]string{}, activeSet...)
	n.frozenValidatorHashByHeight[epoch] = ValidatorSetHash(activeSet)
	if epoch > 1 {
		parent := &n.Blockchain.Blocks[epoch-2]
		parent.NextValidatorSetHash = ValidatorSetHash(activeSet)
		parent.NextValidatorSetHeight = epoch
		parent.ActivationHeight = epoch
	}

	active, reason = n.selfActiveValidatorAt(epoch)
	if !active {
		t.Fatalf("expected active after frozen-set inclusion, got reason=%s", reason)
	}
	if reason != "active" {
		t.Fatalf("unexpected active reason: got=%s want=active", reason)
	}

	hello = n.outboundPeerHello()
	if hello.Role != "validator" {
		t.Fatalf("expected active peer role=validator, got=%s", hello.Role)
	}
	if hello.ValidatorID != n.ID {
		t.Fatalf("expected validator id %s after activation, got=%s", n.ID, hello.ValidatorID)
	}
	if hello.ValidatorPubKey == "" {
		t.Fatalf("expected validator pubkey after activation")
	}
}

func TestSelfActiveValidatorAtFallsBackToCommittedValidatorSetWhenSnapshotMissing(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	withValidatorSetCommitmentV2AtHeight(t, 1)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := GlobalValidatorRegistry.Snapshot()
	activeSet := canonicalValidatorIDs([]string{"A", "B", "C", "D", "F"})
	staleSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	committedHash := validatorSetHashFromSnapshotForHeight(2, activeSet, registry)

	n := &Node{
		ID:                          "F",
		Role:                        "validator",
		ValidatorKey:                strictActivationTestValidatorKey(15, "F"),
		DB:                          db,
		Blockchain:                  &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "block-1"}, {ID: 2, BlockHash: "block-2", Signatures: append([]string{}, activeSet...), ValidatorSetHash: committedHash, ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry)}}},
		epochValidators:             map[uint64][]string{2: append([]string{}, staleSet...)},
		frozenValidatorsByHeight:    map[uint64][]string{2: append([]string{}, staleSet...)},
		frozenValidatorHashByHeight: map[uint64]string{2: ValidatorSetHash(staleSet)},
	}
	if err := n.storeValidatorRegistrySnapshotRecord(1, registry); err != nil {
		t.Fatalf("store validator registry snapshot: %v", err)
	}

	epoch := uint64(2)
	if !n.missingPersistedCommittedSnapshotForHeight(1) {
		t.Fatalf("expected committed snapshot at height 1 to be missing")
	}

	if resolved, _, _, ok := n.resolveCommittedValidatorSetForHeight(epoch); !ok || !sameStringSlice(resolved, activeSet) {
		t.Fatalf("expected committed validator-set resolution, ok=%t got=%v want=%v", ok, resolved, activeSet)
	}

	active, reason := n.selfActiveValidatorAt(epoch)
	if !active {
		t.Fatalf("expected committed validator-set fallback to activate self, got reason=%s", reason)
	}
	if reason != "active_chain_derived_fallback" {
		t.Fatalf("unexpected active reason: got=%s want=active_chain_derived_fallback", reason)
	}
}

func TestSelfActiveValidatorAtDoesNotUseTipSnapshotRepairWhenCommittedSnapshotMissing(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	withValidatorSetCommitmentV2AtHeight(t, 1)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := GlobalValidatorRegistry.Snapshot()
	activeSet := canonicalValidatorIDs([]string{"A", "B", "C", "D", "F"})
	staleSet := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	committedHash := validatorSetHashFromSnapshotForHeight(2, activeSet, registry)

	block1 := Block{
		ID:                     1,
		BlockHash:              "block-1",
		ValidatorSetHash:       committedHash,
		NextValidatorSetHash:   committedHash,
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
	}
	snapshot := &StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 1,
		BlockHash:              block1.BlockHash,
		StateRoot:              "state-root-1",
		Ledger:                 NewLedger(),
		LedgerHash:             HashLedger(NewLedger()),
		GenesisHash:            GenesisHash,
		PrevHash:               "",
		Validators:             map[string]bool{"A": true, "B": true, "C": true, "D": true, "F": true},
		ValidatorSetHash:       committedHash,
		NextValidatorSetHash:   committedHash,
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
		ValidatorRegistry:      copyValidatorRegistrySnapshot(registry),
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
		StateValidators:        onChainValidatorsFromRegistrySnapshot(registry, nil, 1),
	}
	populateSnapshotDerivedFields(snapshot)

	n := &Node{
		ID:                          "F",
		Role:                        "validator",
		ValidatorKey:                strictActivationTestValidatorKey(15, "F"),
		DB:                          db,
		Blockchain:                  &Blockchain{Blocks: []Block{block1}},
		epochValidators:             map[uint64][]string{2: append([]string{}, staleSet...)},
		frozenValidatorsByHeight:    map[uint64][]string{2: append([]string{}, staleSet...)},
		frozenValidatorHashByHeight: map[uint64]string{2: ValidatorSetHash(staleSet)},
	}
	if err := n.storeValidatorRegistrySnapshotRecord(1, registry); err != nil {
		t.Fatalf("store validator registry snapshot: %v", err)
	}
	if err := n.storeTipSnapshotRecords(snapshot, "test_tip"); err != nil {
		t.Fatalf("store tip snapshot: %v", err)
	}

	epoch := uint64(2)
	if !n.missingPersistedCommittedSnapshotForHeight(1) {
		t.Fatalf("expected committed snapshot at height 1 to be missing")
	}

	derived, source, ok := n.chainDerivedValidatorAuthorityForHeight(epoch)
	if !ok || !sameStringSlice(derived, activeSet) {
		t.Fatalf("expected chain-derived authority to resolve committed validator set without tip repair, ok=%t got=%v want=%v", ok, derived, activeSet)
	}
	if normalizeCommitteeAuthoritySource(source) == "tip_snapshot_repair" {
		t.Fatalf("expected activation authority source to avoid tip snapshot repair, got=%s", source)
	}
	if !n.missingPersistedCommittedSnapshotForHeight(1) {
		t.Fatalf("expected activation authority resolution to avoid materializing tip snapshot repair")
	}

	active, reason := n.selfActiveValidatorAt(epoch)
	if !active {
		t.Fatalf("expected committed validator-set fallback to activate self, got reason=%s", reason)
	}
	if reason != "active_chain_derived_fallback" {
		t.Fatalf("unexpected active reason: got=%s want=active_chain_derived_fallback", reason)
	}
	if !n.missingPersistedCommittedSnapshotForHeight(1) {
		t.Fatalf("expected self activation to avoid repairing committed snapshot from tip snapshot")
	}
}

func TestStrictActivationDisabledKeepsLegacyPeerHelloIdentity(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	ValidatorOnboardingStrictActivation = false

	n := makeStrictActivationNode(50)
	epoch := n.currentEpoch()
	inactiveSet := []string{"B", "C", "D"}
	n.frozenValidatorsByHeight[epoch] = append([]string{}, inactiveSet...)
	n.frozenValidatorHashByHeight[epoch] = ValidatorSetHash(inactiveSet)

	hello := n.outboundPeerHello()
	if hello.Role != "validator" {
		t.Fatalf("expected legacy role=validator when strict activation disabled, got=%s", hello.Role)
	}
	if hello.ValidatorID != n.ID {
		t.Fatalf("expected legacy validator identity when strict activation disabled, got id=%q", hello.ValidatorID)
	}
}

func TestSelfActiveValidatorAtReconcilesStaleFrozenSetWhenCurrentSetIncludesSelf(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	epoch := n.currentEpoch()

	n.validatorSetMu.Lock()
	n.epochValidators[epoch] = []string{"A", "B", "C", "D", "F"}
	n.validatorSetHeight = epoch
	n.frozenValidatorsByHeight[epoch] = []string{"B", "C", "D", "F"}
	n.frozenValidatorHashByHeight[epoch] = ValidatorSetHash([]string{"B", "C", "D", "F"})
	n.validatorSetMu.Unlock()

	active, reason := n.selfActiveValidatorAt(epoch)
	if !active {
		t.Fatalf("expected reconcile path to restore self activation, got reason=%s", reason)
	}
	if reason != "active" {
		t.Fatalf("unexpected reason: got=%s want=active", reason)
	}

	frozen := n.frozenValidatorsForHeight(epoch)
	if !containsValidatorIDInSet(frozen, n.ID) {
		t.Fatalf("expected reconciled frozen set to include self %s, got=%v", n.ID, frozen)
	}
}

func TestStrictActivationPreAuthSelfCandidateRefreshesLocalEvidence(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ValidatorRequireStake = false
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	height := uint64(50)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", Stake: 100, Reputation: 1, Status: ValidatorActive, JoinHeight: 1},
		"N": {ID: "N", Stake: 100, Reputation: 1, Status: ValidatorPending, JoinHeight: 1},
	})

	validatorPubKeysMu.Lock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": strictActivationTestPub(11),
		"B": strictActivationTestPub(12),
		"C": strictActivationTestPub(13),
		"D": strictActivationTestPub(14),
		"N": strictActivationTestPub(31),
	}
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)
	validatorPubKeysMu.Unlock()

	n := makeStrictActivationNode(height)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(41, "N")
	n.candidates = make(map[string]*CandidateStatus)
	n.validatorStatus = make(map[string]*ValidatorStatus)
	n.epochValidators = map[uint64][]string{
		height: {"A", "B", "C", "D", "N"},
	}
	n.frozenValidatorsByHeight[height] = []string{"A", "B", "C", "D"}
	n.frozenValidatorHashByHeight[height] = ValidatorSetHash([]string{"A", "B", "C", "D"})

	authMu.Lock()
	authReady = false
	authWalletAddr = ""
	authNodeID = ""
	authMu.Unlock()

	if n.canPublishValidatorHeartbeatNow() {
		t.Fatalf("expected publish gate blocked while wallet auth is pending")
	}

	n.broadcastValidatorInfoInternal(true)

	n.candidateMu.RLock()
	cand := n.candidates["N"]
	n.candidateMu.RUnlock()
	if cand == nil || cand.LastHeartbeatAt.IsZero() {
		t.Fatalf("expected local self-candidate heartbeat evidence to be refreshed pre-auth")
	}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("N", height, snapshot)
	if !ready {
		t.Fatalf("expected candidate heartbeat evidence to satisfy strict readiness, got reason=%s", reason)
	}
}

func TestCanPublishValidatorHeartbeatNowRespectsWalletAuthState(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(51, "N")

	authMu.Lock()
	authReady = false
	authWalletAddr = ""
	authNodeID = ""
	authMu.Unlock()
	if n.canPublishValidatorHeartbeatNow() {
		t.Fatalf("expected publish gate closed while auth is pending")
	}

	authMu.Lock()
	authReady = true
	authWalletAddr = "MSC01authwallet"
	authNodeID = "N"
	authMu.Unlock()
	if !n.canPublishValidatorHeartbeatNow() {
		t.Fatalf("expected publish gate open once auth is ready for self node")
	}
}

func TestOnboardingGateSuppressesOfflineAwaitingPubkeyLogSpam(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(false)

	n := makeStrictActivationNode(height)
	n.peerToValidator = map[string]string{"peer-f": "F"}
	n.peerDialNext = map[string]time.Time{"peer-f": time.Now().Add(30 * time.Second)}
	n.connectedPeers = map[string]bool{}
	n.connectingPeers = map[string]bool{}
	n.peerHelloOK = map[string]bool{}

	if !n.shouldLogOnboardingGateDecision("F", "awaiting_pubkey", height) {
		t.Fatalf("expected first offline awaiting_pubkey gate decision to log")
	}
	if n.shouldLogOnboardingGateDecision("F", "awaiting_pubkey", height) {
		t.Fatalf("expected repeated offline awaiting_pubkey decision to be suppressed within cooldown")
	}

	snapshot := GlobalValidatorRegistry.Snapshot()
	ready, reason := n.onboardingActivationReady("F", height, snapshot)
	if ready || reason != "awaiting_pubkey" {
		t.Fatalf("eligibility semantics must remain unchanged, ready=%t reason=%s", ready, reason)
	}
	got := n.getEligibleSortedValidatorIDs(height, nil)
	if containsValidatorIDInSet(got, "F") {
		t.Fatalf("expected offline awaiting_pubkey candidate to remain ineligible, got=%v", got)
	}
}

func TestWalletLoginGateIsIndependentFromStakeGate(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true
	ValidatorMinStake = 100
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(61, "N")
	n.Ledger = GenesisLedger()

	// No auth yet => wallet-login gate should block.
	authMu.Lock()
	authWalletAddr = ""
	authNodeID = ""
	authReady = false
	authMu.Unlock()
	if n.WaitForWalletLogin(context.Background(), 20*time.Millisecond) {
		t.Fatalf("expected wallet login gate to block when auth is missing")
	}

	// Auth completed => wallet-login gate should pass even without stake.
	authMu.Lock()
	authWalletAddr = "MSC01testwallet"
	authNodeID = "N"
	authReady = true
	authMu.Unlock()
	if !n.WaitForWalletLogin(context.Background(), 20*time.Millisecond) {
		t.Fatalf("expected wallet login gate to pass once auth is complete")
	}
	if n.WaitForWalletAuth(context.Background(), 20*time.Millisecond) {
		t.Fatalf("expected stake gate to remain blocked without required validator stake")
	}
}

func TestRuntimeStatusUsesValidatorStakeRequiredWaitReasonWhenUnstaked(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = true
	ValidatorMinStake = 100
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(62, "N")
	n.Ledger = GenesisLedger()

	status := n.runtimeStatusSnapshot()
	if status.WaitReason != "validator_stake_required" {
		t.Fatalf("expected wait reason validator_stake_required for unstaked non-core validator, got=%s", status.WaitReason)
	}

	// Add stake and verify stake gate reason clears.
	n.Ledger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      100,
		LockedUntil: n.Blockchain.Height() + 100,
	}
	status = n.runtimeStatusSnapshot()
	if status.WaitReason == "validator_stake_required" {
		t.Fatalf("expected wait reason to move past validator_stake_required once stake is present")
	}
}

func TestValidatorStakeGatePersistsAfterLockMaturesUntilUnstake(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = true
	ValidatorMinStake = 100

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(63, "N")
	n.Ledger = GenesisLedger()
	n.Ledger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      100,
		LockedUntil: 10,
	}

	if !n.hasRequiredValidatorStake() {
		t.Fatalf("expected matured but still-staked validator stake to satisfy rejoin gate")
	}
	_, lockedUntil, eligible, reason := validatorStakeStatus(n, "wallet-n", "N")
	if !eligible {
		t.Fatalf("expected wallet stake status eligible after lock maturity until unstake, locked_until=%d reason=%q", lockedUntil, reason)
	}

	n.Ledger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      99,
		LockedUntil: 10,
	}
	if n.hasRequiredValidatorStake() {
		t.Fatalf("expected validator stake gate to fail after unstake drops amount below minimum")
	}
}

func TestValidatorStakeGateUsesExecutionLedgerAfterRestart(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = true
	ValidatorMinStake = 100

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(64, "N")
	n.Ledger = GenesisLedger()
	executionLedger := GenesisLedger()
	executionLedger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      100,
		LockedUntil: 10,
	}
	n.ExecutionLedger = executionLedger.Clone()

	if !n.hasRequiredValidatorStake() {
		t.Fatalf("expected restart stake gate to use persisted execution ledger stake")
	}

	executionLedger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      99,
		LockedUntil: 10,
	}
	n.setExecutionLedger(executionLedger)
	if n.hasRequiredValidatorStake() {
		t.Fatalf("expected stake gate to fail after unstake reduces execution ledger stake below minimum")
	}
}

func TestRuntimeStatusSeparatesLocalKeyLoadFromRegistryPubkeyAnchoring(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	height := uint64(50)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(false)

	n := makeStrictActivationNode(height)
	n.ID = "F"
	n.ValidatorKey = strictActivationTestValidatorKey(15, "F")

	status := n.runtimeStatusSnapshot()
	if status.ValidatorConsensusPubKeyAnchored {
		t.Fatalf("expected missing registry pubkey anchor for F")
	}
	if status.ValidatorConsensusPubKeySource != "none" {
		t.Fatalf("expected validator consensus pubkey source none, got=%s", status.ValidatorConsensusPubKeySource)
	}
	if status.ActivationBlockerReason != "awaiting_pubkey" {
		t.Fatalf("expected activation blocker awaiting_pubkey, got=%s", status.ActivationBlockerReason)
	}
	if status.WaitReason == "validator_key_unavailable" {
		t.Fatalf("expected loaded local key to be reported separately from registry pubkey anchoring")
	}

	n.ValidatorKey = ValidatorKey{}
	status = n.runtimeStatusSnapshot()
	if status.WaitReason != "validator_key_unavailable" {
		t.Fatalf("expected validator_key_unavailable when local key is missing, got=%s", status.WaitReason)
	}
	if status.ValidatorConsensusPubKeyAnchored {
		t.Fatalf("expected registry pubkey anchor to remain absent when local key is missing")
	}
}

func TestRuntimeStatusReportsObserverConsensusLoopWhenParticipationBlocked(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = true
	ValidatorMinStake = 100

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(65, "N")
	n.Ledger = GenesisLedger()

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if !status.ConsensusRunning {
		t.Fatalf("expected consensus_running=true while loop is active in observer mode")
	}
	if status.ConsensusMode != "observer" {
		t.Fatalf("expected consensus_mode=observer when participation gate is blocked, got=%s", status.ConsensusMode)
	}
	if status.VoteEnabled || status.ProposeEnabled {
		t.Fatalf("expected vote/propose disabled in observer mode, vote=%t propose=%t", status.VoteEnabled, status.ProposeEnabled)
	}
	if status.WaitReason != "validator_stake_required" {
		t.Fatalf("expected wait reason validator_stake_required when unstaked, got=%s", status.WaitReason)
	}
}

func TestRuntimeStatusReportsValidatorConsensusModeWhenParticipationReady(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.Ledger = GenesisLedger()

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if !status.ConsensusRunning {
		t.Fatalf("expected consensus_running=true while loop is active")
	}
	if status.ConsensusMode != "validator" {
		t.Fatalf("expected consensus_mode=validator when participation gate is ready, got=%s", status.ConsensusMode)
	}
	if !status.VoteEnabled || !status.ProposeEnabled {
		t.Fatalf("expected vote/propose enabled in validator mode, vote=%t propose=%t", status.VoteEnabled, status.ProposeEnabled)
	}
}

func TestRuntimeStatusReportsActivatingWhenStrictActivationPending(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(50)
	n.ID = "F"
	n.ValidatorKey = strictActivationTestValidatorKey(15, "F")
	n.Ledger = GenesisLedger()
	n.candidates["F"] = &CandidateStatus{
		ID:              "F",
		LastHeartbeatAt: time.Now(),
	}

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if status.ConsensusMode != "observer" {
		t.Fatalf("expected consensus_mode=observer while strict activation is pending, got=%s", status.ConsensusMode)
	}
	if status.VoteEnabled || status.ProposeEnabled {
		t.Fatalf("expected vote/propose disabled while strict activation is pending, vote=%t propose=%t", status.VoteEnabled, status.ProposeEnabled)
	}
	if status.ValidatorState != "activating" {
		t.Fatalf("expected validator_state=activating while strict activation is pending, got=%s", status.ValidatorState)
	}
	if status.WaitReason != "activation_pending_not_in_frozen_set" {
		t.Fatalf("expected wait reason activation_pending_not_in_frozen_set, got=%s", status.WaitReason)
	}
	if status.SelfActiveReasonNext != "activation_pending_not_in_frozen_set" {
		t.Fatalf("expected self_active_reason_next activation_pending_not_in_frozen_set, got=%s", status.SelfActiveReasonNext)
	}
	if status.OnboardingState != string(OnboardingStateActivating) {
		t.Fatalf("expected onboarding_state activating while strict activation is pending, got=%s", status.OnboardingState)
	}
}

func TestRuntimeStatusReportsActivatingWhenStrictActivationPendingWithTransport(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()

	n := makeStrictActivationNode(50)
	n.Host = host
	n.ID = "F"
	n.ValidatorKey = strictActivationTestValidatorKey(15, "F")
	n.Ledger = GenesisLedger()
	n.candidates["F"] = &CandidateStatus{
		ID:              "F",
		LastHeartbeatAt: time.Now(),
	}

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	status := n.runtimeStatusSnapshot()
	if status.ValidatorState != "activating" {
		t.Fatalf("expected validator_state=activating while strict activation is pending, got=%s", status.ValidatorState)
	}
	if status.WaitReason != "activation_pending_not_in_frozen_set" {
		t.Fatalf("expected wait reason activation_pending_not_in_frozen_set, got=%s", status.WaitReason)
	}
	if status.SelfActiveReasonNext != "activation_pending_not_in_frozen_set" {
		t.Fatalf("expected self_active_reason_next activation_pending_not_in_frozen_set, got=%s", status.SelfActiveReasonNext)
	}
	if status.ConsensusMode != "observer" {
		t.Fatalf("expected consensus_mode=observer while strict activation is pending, got=%s", status.ConsensusMode)
	}
	if status.GossipPipeline == "gossip_wait_remote_sample" {
		t.Fatalf("expected strict-activation pending node to avoid remote-sample startup gate")
	}
}

func TestCanParticipateInConsensusNowRequiresWalletAndStakeForNonCore(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true
	ValidatorMinStake = 100
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(63, "N")
	n.Ledger = GenesisLedger()

	// no auth + no stake
	authMu.Lock()
	authWalletAddr = ""
	authNodeID = ""
	authReady = false
	authMu.Unlock()
	if n.canParticipateInConsensusNow() {
		t.Fatalf("expected consensus participation gate closed without auth and stake")
	}

	// auth only
	authMu.Lock()
	authWalletAddr = "MSC01authonly"
	authNodeID = "N"
	authReady = true
	authMu.Unlock()
	if n.canParticipateInConsensusNow() {
		t.Fatalf("expected consensus participation gate closed with auth only and no stake")
	}

	// stake only
	authMu.Lock()
	authWalletAddr = ""
	authNodeID = ""
	authReady = false
	authMu.Unlock()
	n.Ledger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      100,
		LockedUntil: n.Blockchain.Height() + 100,
	}
	if !n.canParticipateAsValidator() {
		t.Fatalf("expected stake/key gate to pass with stake-only (wallet gate still pending)")
	}
	if n.canParticipateInConsensusNow() {
		t.Fatalf("expected consensus participation gate closed with stake only and no auth")
	}

	// auth + stake
	authMu.Lock()
	authWalletAddr = "MSC01authplusstake"
	authNodeID = "N"
	authReady = true
	authMu.Unlock()
	if !n.canParticipateInConsensusNow() {
		t.Fatalf("expected consensus participation gate open with both auth and stake")
	}
}

func TestRuntimeStatusUsesEffectiveRoleAndWalletAuthWaitReason(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true
	ValidatorMinStake = 100
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorOnboardingStrictActivation = false

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(64, "N")
	n.Ledger = GenesisLedger()

	// no auth + no stake => effective observer role and wallet gate reason first.
	authMu.Lock()
	authWalletAddr = ""
	authNodeID = ""
	authReady = false
	authMu.Unlock()
	status := n.runtimeStatusSnapshot()
	if status.Role != "full" || status.IsValidator {
		t.Fatalf("expected effective observer role pre-auth/stake, got role=%s is_validator=%t", status.Role, status.IsValidator)
	}
	if status.WaitReason != "wallet_auth_required" {
		t.Fatalf("expected wait reason wallet_auth_required before stake gate, got=%s", status.WaitReason)
	}

	// stake only => still wallet gate.
	n.Ledger.Stakes[stakeKey("wallet-n", "N")] = StakeLock{
		ValidatorID: "N",
		Amount:      100,
		LockedUntil: n.Blockchain.Height() + 100,
	}
	status = n.runtimeStatusSnapshot()
	if status.Role != "full" || status.IsValidator {
		t.Fatalf("expected effective observer role with stake-only pre-auth, got role=%s is_validator=%t", status.Role, status.IsValidator)
	}
	if status.WaitReason != "wallet_auth_required" {
		t.Fatalf("expected wallet_auth_required with stake-only pre-auth, got=%s", status.WaitReason)
	}

	// auth + stake => effective validator role.
	authMu.Lock()
	authWalletAddr = "MSC01readywallet"
	authNodeID = "N"
	authReady = true
	authMu.Unlock()
	status = n.runtimeStatusSnapshot()
	if status.Role != "validator" || !status.IsValidator {
		t.Fatalf("expected effective validator role after auth+stake, got role=%s is_validator=%t", status.Role, status.IsValidator)
	}
	if status.WaitReason == "wallet_auth_required" || status.WaitReason == "validator_stake_required" {
		t.Fatalf("expected wait reason to move past auth/stake gates once satisfied, got=%s", status.WaitReason)
	}
}

func TestValidatorParticipationRequiresCommittedConsensusSigningKey(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false

	n := makeStrictActivationNode(50)
	n.ID = "F"
	n.ValidatorKey = strictActivationTestValidatorKey(15, "F")
	height := n.currentEpoch()
	committee := []string{"A", "B", "C", "D", "F"}
	n.epochValidators[height] = append([]string{}, committee...)
	n.frozenValidatorsByHeight[height] = append([]string{}, committee...)
	n.frozenValidatorHashByHeight[height] = ValidatorSetHash(committee)

	registry := GlobalValidatorRegistry.Snapshot()
	rec := registry["F"]
	rec.ID = "F"
	rec.Status = ValidatorActive
	rec.ConsensusPubKey = ""
	registry["F"] = rec
	GlobalValidatorRegistry.Load(registry)
	GenesisValidatorPubKeys["F"] = append(ed25519.PublicKey(nil), n.ValidatorKey.PublicKey...)
	n.Blockchain.mu.Lock()
	n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1].ValidatorRegistryHash = ValidatorRegistrySnapshotHash(registry)
	n.Blockchain.mu.Unlock()

	if ready, reason := n.validatorParticipationGateStatus(height); ready || reason != "validator_consensus_pubkey_unanchored" {
		t.Fatalf("unanchored active validator must not participate, ready=%t reason=%q", ready, reason)
	}
	unsigned := Block{ID: height, Proposer: "F", BlockTime: LogicalTimeForEpoch(height)}
	n.SignBlock(&unsigned)
	if len(unsigned.Signature) != 0 {
		t.Fatalf("unanchored active validator must not sign proposer blocks")
	}
	status := n.runtimeStatusSnapshot()
	if status.ProposeEnabled || status.VoteEnabled {
		t.Fatalf("runtime status must keep unanchored validator from proposing or voting, propose=%t vote=%t wait=%q",
			status.ProposeEnabled, status.VoteEnabled, status.WaitReason)
	}

	registry = GlobalValidatorRegistry.Snapshot()
	rec = registry["F"]
	rec.ConsensusPubKey = strings.ToLower(hex.EncodeToString(strictActivationTestPub(16)))
	registry["F"] = rec
	GlobalValidatorRegistry.Load(registry)
	if ready, reason := n.validatorParticipationGateStatus(height); ready || reason != "validator_consensus_pubkey_mismatch" {
		t.Fatalf("mismatched active validator must not participate, ready=%t reason=%q", ready, reason)
	}

	registry = GlobalValidatorRegistry.Snapshot()
	rec = registry["F"]
	rec.ConsensusPubKey = strings.ToLower(hex.EncodeToString(n.ValidatorKey.PublicKey))
	registry["F"] = rec
	GlobalValidatorRegistry.Load(registry)
	if ready, reason := n.validatorParticipationGateStatus(height); !ready {
		t.Fatalf("matching committed signing key must restore participation, reason=%q", reason)
	}
	signed := Block{ID: height, Proposer: "F", BlockTime: LogicalTimeForEpoch(height)}
	n.SignBlock(&signed)
	if len(signed.Signature) != ed25519.SignatureSize {
		t.Fatalf("matching committed signing key must restore proposer signing")
	}
}

func TestObserveCandidatesOnCommitQueuesPendingValidatorInDeterministicMode(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)

	n := makeStrictActivationNode(50)
	n.candidates["F"] = &CandidateStatus{
		ID:              "F",
		LastHeartbeatAt: time.Now(),
		PubKey:          strictActivationTestPub(15),
	}

	block, ok := n.Blockchain.GetBlock(n.Blockchain.Height())
	if !ok {
		t.Fatalf("expected committed tip block")
	}

	n.observeCandidatesOnCommit(block)

	got := n.onboardingPendingAddHeight("F")
	want := block.ID + validatorSetActivationDelayBlocks()
	if got != want {
		t.Fatalf("expected deterministic onboarding queue height %d, got=%d", want, got)
	}
	if _, ok := n.pendingValidators["F"]; !ok {
		t.Fatalf("expected pending add entry for F after deterministic onboarding observation")
	}
	n.candidateMu.RLock()
	cand := n.candidates["F"]
	n.candidateMu.RUnlock()
	if cand == nil || !cand.Promoted {
		t.Fatalf("expected deterministic onboarding path to mark candidate F as promoted")
	}
}

func TestRuntimeStatusUsesObserverRoleAndSnapshotModeWhileSyncing(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	SyncSnapshotCatchupThresholdBlocks = 64

	n := makeStrictActivationNode(50)
	n.Consensus = &ConsensusState{
		Syncing:    true,
		SyncTarget: n.Blockchain.Height() + 100,
	}

	status := n.runtimeStatusSnapshot()
	if status.Role != "full" || status.IsValidator {
		t.Fatalf("expected effective observer role while syncing, got role=%s is_validator=%t", status.Role, status.IsValidator)
	}
	if status.WaitReason != "syncing" {
		t.Fatalf("expected syncing wait reason while catch-up is in progress, got=%s", status.WaitReason)
	}
	if status.SyncMode != "snapshot" {
		t.Fatalf("expected snapshot sync mode for large lag, got=%s", status.SyncMode)
	}
	if status.SyncLagBlocks < 64 {
		t.Fatalf("expected sync lag to reflect large catch-up gap, got=%d", status.SyncLagBlocks)
	}
}

func TestRuntimeStatusPrefersSnapshotModeForFreshNodeCatchUp(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	SyncSnapshotCatchupThresholdBlocks = 64

	n := makeStrictActivationNode(0)
	empty := NewBlockchain()
	n.Blockchain = &empty
	n.Consensus = &ConsensusState{
		Syncing:    true,
		SyncTarget: 10,
	}

	status := n.runtimeStatusSnapshot()
	if status.SyncMode != "snapshot" {
		t.Fatalf("expected fresh-node catch-up to prefer snapshot mode, got=%s", status.SyncMode)
	}
	if status.SyncAction == "" {
		t.Fatalf("expected sync action to be populated for fresh-node catch-up")
	}
}

func TestRuntimeStatusIncludesSelfActivationObservabilityFields(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	nextHeight := n.Blockchain.Height() + 1
	n.validatorSetMu.Lock()
	n.frozenValidatorsByHeight[nextHeight] = []string{"A", "B", "C", "D"}
	n.frozenValidatorHashByHeight[nextHeight] = ValidatorSetHash([]string{"A", "B", "C", "D"})
	if n.pendingValidators == nil {
		n.pendingValidators = make(map[string]uint64)
	}
	n.pendingValidators["A"] = nextHeight + 10
	n.validatorSetMu.Unlock()

	status := n.runtimeStatusSnapshot()
	if !status.SelfInFrozenSetNext {
		t.Fatalf("expected self_in_frozen_set_next=true")
	}
	if status.SelfActiveReasonNext == "" {
		t.Fatalf("expected self_active_reason_next to be populated")
	}
	if status.SelfPendingAddHeight != nextHeight+10 {
		t.Fatalf("unexpected self_pending_add_height: got=%d want=%d", status.SelfPendingAddHeight, nextHeight+10)
	}
	if status.ScheduledHeight != nextHeight+10 {
		t.Fatalf("unexpected scheduled_height: got=%d want=%d", status.ScheduledHeight, nextHeight+10)
	}
	if status.ActivationDelayModel == "" {
		t.Fatalf("expected activation_delay_model to be populated")
	}
	if status.BarrierRetryMode != transitionBarrierRetryModePerBlock {
		t.Fatalf("unexpected barrier_retry_mode: got=%q want=%q", status.BarrierRetryMode, transitionBarrierRetryModePerBlock)
	}
	if status.OnboardingState == "" {
		t.Fatalf("expected onboarding_state to be populated")
	}
}

func TestActivationDelayModelSwitchHeightDeterminism(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ValidatorSetActivationDelay = 5
	ValidatorSetActivationModelV2Height = 120
	SyncCheckpointIntervalBlocks = 32

	if got, want := effectiveValidatorSetActivationHeightAt(110, 119), uint64(128); got != want {
		t.Fatalf("unexpected v1 effective height before switch: got=%d want=%d", got, want)
	}
	if got, want := validatorSetActivationDelayModelAtHeight(119), activationDelayModelV1DoubleHold; got != want {
		t.Fatalf("unexpected activation model before switch: got=%s want=%s", got, want)
	}
	if got, want := effectiveValidatorSetActivationHeightAt(110, 120), uint64(128); got != want {
		t.Fatalf("unexpected v2 effective height at switch: got=%d want=%d", got, want)
	}
	if got, want := validatorSetActivationDelayModelAtHeight(120), activationDelayModelV2Single; got != want {
		t.Fatalf("unexpected activation model at switch: got=%s want=%s", got, want)
	}
}

func TestScheduleWhileSyncApplyAfterSync(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	DeterministicValidatorSelection = true
	ValidatorSetActivationModelV2Height = 1
	prevCommit := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	SyncCheckpointIntervalBlocks = 8
	t.Cleanup(func() { ValidatorSetCommitmentV2Height = prevCommit })

	n := makeStrictActivationNode(8)
	n.epochValidators[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorsByHeight[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorHashByHeight[8] = ValidatorSetHash([]string{"A", "B", "C", "D"})
	n.pendingValidators = map[string]uint64{"F": 1}
	n.pendingValidatorRemovals = make(map[string]uint64)
	n.Consensus = &ConsensusState{Syncing: true}

	expectedNextHash := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	now := time.Now()
	for _, id := range []string{"B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			FinalizedHeight:    8,
			ReportedHeight:     8,
			ExecEpoch:          9,
			ValidatorSetHeight: 9,
			ValidatorSetHash:   expectedNextHash,
		}
	}

	n.applyScheduledValidatorUpdates(8)
	if containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected validator F to remain unapplied while syncing")
	}
	if _, ok := n.pendingValidators["F"]; !ok {
		t.Fatalf("expected pending add for F to remain while syncing")
	}

	n.Consensus.mu.Lock()
	n.Consensus.Syncing = false
	n.Consensus.mu.Unlock()

	n.applyScheduledValidatorUpdates(8)
	if !containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected validator F to apply after sync completion")
	}
	if _, ok := n.pendingValidators["F"]; !ok {
		t.Fatalf("expected pending add for F to remain until chain commit applies transition")
	}
}

func TestPendingEndpointIncludesBlockerReason(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	n.pendingValidators = map[string]uint64{"F": 60}
	n.pendingValidatorRemovals = map[string]uint64{"C": 70}
	s := &Server{Node: n}

	height, adds, removes := s.collectPendingValidators()
	if height != n.Blockchain.FinalizedHeight() {
		t.Fatalf("unexpected pending height: got=%d want=%d", height, n.Blockchain.FinalizedHeight())
	}
	if len(adds) != 1 || len(removes) != 1 {
		t.Fatalf("unexpected pending lengths: adds=%d removes=%d", len(adds), len(removes))
	}
	if adds[0].ScheduledHeight == 0 || adds[0].EffectiveHeight == 0 {
		t.Fatalf("expected pending_add scheduling fields, got=%+v", adds[0])
	}
	if adds[0].BlockerReason == "" || adds[0].BlockerClass == "" {
		t.Fatalf("expected pending_add blocker diagnostics, got=%+v", adds[0])
	}
	if removes[0].ScheduledHeight == 0 || removes[0].EffectiveHeight == 0 {
		t.Fatalf("expected pending_remove scheduling fields, got=%+v", removes[0])
	}
}

func TestRuntimeStatusIncludesSyncHardeningFields(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	SyncSnapshotCatchupThresholdBlocks = 64

	n := makeStrictActivationNode(50)
	n.Consensus = &ConsensusState{
		Syncing:    true,
		SyncTarget: n.Blockchain.Height() + 200,
	}
	n.syncMu.Lock()
	n.syncProvider = "12D3KooWProvider"
	n.syncSnapshotHeight = 120
	n.syncStallSeconds = 33
	n.syncMu.Unlock()

	status := n.runtimeStatusSnapshot()
	if status.SyncProvider != "12D3KooWProvider" {
		t.Fatalf("unexpected sync provider: got=%q", status.SyncProvider)
	}
	if status.SnapshotHeightApplied != 120 {
		t.Fatalf("unexpected snapshot height applied: got=%d want=120", status.SnapshotHeightApplied)
	}
	if status.SyncStallSeconds != 33 {
		t.Fatalf("unexpected sync stall seconds: got=%d want=33", status.SyncStallSeconds)
	}
	if status.DeltaRemainingBlocks == 0 {
		t.Fatalf("expected non-zero delta remaining blocks while syncing")
	}
	if status.MeshMode != "active_only" {
		t.Fatalf("unexpected mesh mode: got=%q want=active_only", status.MeshMode)
	}
	if status.MeshReconcileIntervalSeconds != 8 {
		t.Fatalf("unexpected mesh reconcile interval: got=%d want=8", status.MeshReconcileIntervalSeconds)
	}
	if status.CoreRegistryGate != "open" {
		t.Fatalf("unexpected core registry gate on testnet: got=%q want=open", status.CoreRegistryGate)
	}
}

func TestRuntimeStatusDoesNotMarkNearTipTargetComplete(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.ID = "G"
	n.ValidatorKey = strictActivationTestValidatorKey(22, "G")
	n.Consensus = &ConsensusState{
		Syncing:    false,
		SyncTarget: n.Blockchain.Height() + 1,
	}
	setStrictActivationObservedHeight(n, n.Blockchain.Height()+1)

	status := n.runtimeStatusSnapshot()
	if status.SyncComplete {
		t.Fatalf("expected sync to remain incomplete while target is one block ahead")
	}
	if !status.Syncing {
		t.Fatalf("expected status to report syncing while target is one block ahead")
	}
	if status.WaitReason != "syncing" {
		t.Fatalf("expected syncing wait reason, got=%q", status.WaitReason)
	}
	if status.DeltaRemainingBlocks != 1 {
		t.Fatalf("unexpected delta remaining: got=%d want=1", status.DeltaRemainingBlocks)
	}
}

func TestRuntimeStatusTreatsNearTipTargetCompleteAfterConsensusStarted(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	n := makeStrictActivationNode(50)
	n.Consensus = &ConsensusState{
		Syncing:    false,
		SyncTarget: n.Blockchain.Height() + 1,
	}
	setStrictActivationObservedHeight(n, n.Blockchain.Height()+1)

	status := n.runtimeStatusSnapshot()
	if status.Syncing {
		t.Fatalf("expected near-tip status not to report syncing after consensus started")
	}
	if !status.SyncComplete {
		t.Fatalf("expected near-tip status to remain complete after consensus started")
	}
	if status.WaitReason == "syncing" {
		t.Fatalf("expected near-tip wait reason not to be syncing")
	}
	if status.DeltaRemainingBlocks != 1 {
		t.Fatalf("unexpected delta remaining: got=%d want=1", status.DeltaRemainingBlocks)
	}
}

func TestRuntimeStatusClearsStaleRawSyncingAtTarget(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.Consensus = &ConsensusState{
		Syncing:    true,
		SyncTarget: n.Blockchain.Height(),
	}
	setStrictActivationObservedHeight(n, n.Blockchain.Height())

	status := n.runtimeStatusSnapshot()
	if status.Syncing {
		t.Fatalf("expected status to clear stale raw syncing once target is reached")
	}
	if !status.SyncComplete {
		t.Fatalf("expected status to remain sync-complete once target is reached")
	}
	if status.WaitReason == "syncing" {
		t.Fatalf("expected wait reason not to be syncing after target is reached")
	}
}

func TestRuntimeStatusKeepsMultiBlockLagSyncingAfterConsensusStarted(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	n := makeStrictActivationNode(50)
	n.Consensus = &ConsensusState{
		Syncing:    false,
		SyncTarget: n.Blockchain.Height() + 2,
	}
	setStrictActivationObservedHeight(n, n.Blockchain.Height()+2)

	status := n.runtimeStatusSnapshot()
	if !status.Syncing {
		t.Fatalf("expected multi-block lag to remain syncing after consensus started")
	}
	if status.SyncComplete {
		t.Fatalf("expected multi-block lag to remain incomplete")
	}
	if status.WaitReason != "syncing" {
		t.Fatalf("expected syncing wait reason, got=%q", status.WaitReason)
	}
	if status.DeltaRemainingBlocks != 2 {
		t.Fatalf("unexpected delta remaining: got=%d want=2", status.DeltaRemainingBlocks)
	}
}

func TestRuntimeStatusFullNodeLiveTailDoesNotFlapSyncing(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	n := makeStrictActivationNode(50)
	n.Role = "full"
	n.Consensus = &ConsensusState{
		Syncing:    false,
		SyncTarget: n.Blockchain.Height() + validatorLivenessMaxHeightDriftBlocks(),
	}
	setStrictActivationObservedHeight(n, n.Blockchain.Height()+validatorLivenessMaxHeightDriftBlocks())

	status := n.runtimeStatusSnapshot()
	if status.Syncing {
		t.Fatalf("expected full node live-tail lag not to report syncing")
	}
	if !status.SyncComplete {
		t.Fatalf("expected full node live-tail lag to remain sync-complete")
	}
	if status.WaitReason == "syncing" {
		t.Fatalf("expected full node wait reason not to be syncing")
	}
	if status.SyncLagBlocks == 0 {
		t.Fatalf("expected live-tail lag to remain visible")
	}
}

func TestRuntimeStatusFullNodeLargeLagStillSyncing(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	lag := validatorLivenessMaxHeightDriftBlocks() + 1
	n := makeStrictActivationNode(50)
	n.Role = "full"
	n.Consensus = &ConsensusState{
		Syncing:    false,
		SyncTarget: n.Blockchain.Height() + lag,
	}
	setStrictActivationObservedHeight(n, n.Blockchain.Height()+lag)

	status := n.runtimeStatusSnapshot()
	if !status.Syncing {
		t.Fatalf("expected full node large lag to report syncing")
	}
	if status.SyncComplete {
		t.Fatalf("expected full node large lag to remain incomplete")
	}
	if status.WaitReason != "syncing" {
		t.Fatalf("expected syncing wait reason, got=%q", status.WaitReason)
	}
}

func TestRuntimeStatusFastPathKeepsRecentDegradedPolicyVisible(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	prevStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(prevStarted)
	})

	n := makeStrictActivationNode(50)
	localHeight := n.Blockchain.Height()
	n.Consensus = &ConsensusState{
		Syncing:    false,
		SyncTarget: localHeight + 2,
	}
	n.Blockchain.mu.Lock()
	last := &n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1]
	last.ConsensusMode = "DEGRADED"
	last.QuorumPolicyVersion = quorumPolicyVersionV1
	last.ActiveReadyCount = 2
	last.RequiredQuorum = 2
	last.StrictQuorum = 3
	n.Blockchain.mu.Unlock()

	now := time.Now()
	n.validatorMu.Lock()
	for _, id := range []string{"A", "B"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      localHeight,
			FinalizedHeight:     localHeight,
			ExecEpoch:           localHeight + 1,
			ValidatorSetHeight:  localHeight + 1,
		}
	}
	for _, id := range []string{"C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now.Add(-2 * validatorLivenessHeartbeatTTL()),
			Active:              true,
			Enabled:             false,
			ConsensusReadyKnown: true,
			ReportedHeight:      localHeight,
			FinalizedHeight:     localHeight,
			ExecEpoch:           localHeight,
			ValidatorSetHeight:  localHeight + 1,
		}
	}
	n.validatorMu.Unlock()

	n.commitMu.Lock()
	n.lastCommitAt = now
	n.commitMu.Unlock()

	status := n.runtimeStatusSnapshot()
	if status.QuorumPolicyMode != "NORMAL" || status.RequiredQuorum != 3 {
		t.Fatalf("expected status fast path to keep strict quorum visible, got mode=%s required=%d status=%+v", status.QuorumPolicyMode, status.RequiredQuorum, status)
	}
	if status.StrictQuorum != 3 || status.ActiveReadyCount != 2 {
		t.Fatalf("unexpected quorum metadata: strict=%d active=%d", status.StrictQuorum, status.ActiveReadyCount)
	}
}

func TestRuntimeStatusIgnoresReachedSnapshotAnchorSession(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.snapshotSession = SnapshotSession{
		Active:       true,
		Stage:        SnapshotSyncStageVerifyQuorum,
		FreezeHeight: n.Blockchain.Height(),
		LastError:    "snapshot_anchor_unreachable",
	}

	status := n.runtimeStatusSnapshot()
	if status.SyncAnchorActive {
		t.Fatalf("expected reached snapshot anchor session to be hidden from active status")
	}
	if status.WaitReason == "snapshot_session_active" {
		t.Fatalf("expected reached snapshot anchor session not to block status")
	}
}

func TestMainnetRegistryVerificationDoesNotGateExistingChainParticipation(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	IsTestnet = false
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(74, "N")
	n.coreRegistryState = CoreRegistryState{
		Verified: false,
	}

	if !n.canParticipateInConsensusNow() {
		t.Fatalf("expected existing-chain participation to ignore core registry verification")
	}

	status := n.runtimeStatusSnapshot()
	if status.CoreRegistryGate != "open" {
		t.Fatalf("expected core registry gate open, got=%q", status.CoreRegistryGate)
	}
	if status.WaitReason == "core_registry_unverified" {
		t.Fatalf("did not expect wait reason core_registry_unverified on existing chain")
	}
}

func TestMainnetRegistryVerificationStateDoesNotChangeParticipationGate(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	IsTestnet = false
	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.ValidatorKey = strictActivationTestValidatorKey(75, "N")
	n.coreRegistryState = CoreRegistryState{
		Verified: true,
	}

	if !n.canParticipateInConsensusNow() {
		t.Fatalf("expected consensus participation gate open on existing chain")
	}
	status := n.runtimeStatusSnapshot()
	if status.CoreRegistryGate != "open" {
		t.Fatalf("expected core registry gate open, got=%q", status.CoreRegistryGate)
	}
	if status.CoreAuthoritySource != "on_chain_validator_state" {
		t.Fatalf("expected on_chain_validator_state source, got=%q", status.CoreAuthoritySource)
	}
}

func TestGenesisBootstrapCoreAuthorityAllowsBootstrapPrivileges(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	IsTestnet = false
	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true

	bc := NewBlockchain()
	n := &Node{
		ID:         "A",
		Role:       "validator",
		Blockchain: &bc,
	}
	setRuntimeCoreValidatorIDs(nil)

	if !n.isCoreValidatorCurrent("A") {
		t.Fatalf("expected bootstrap core membership at genesis")
	}
	if n.requiresWalletAuthCurrent("A") {
		t.Fatalf("expected bootstrap core validator to bypass wallet auth at genesis")
	}
	if !n.hasRequiredValidatorStake() {
		t.Fatalf("expected bootstrap core validator to retain core stake exemption at genesis")
	}
	if !n.isCoreRegistryTrustReadyForValidatorParticipation() {
		t.Fatalf("expected bootstrap genesis authority to satisfy registry trust gate")
	}
	status := n.runtimeStatusSnapshot()
	if status.CoreAuthoritySource != "bootstrap_core" {
		t.Fatalf("expected bootstrap core source, got=%q", status.CoreAuthoritySource)
	}
	if status.CoreMembership != "core" {
		t.Fatalf("expected core membership at genesis, got=%q", status.CoreMembership)
	}
}

func TestExistingChainStaticCoreDoesNotGrantCorePrivileges(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	IsTestnet = false
	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true

	n := makeStrictActivationNode(50)
	n.ID = "A"
	n.coreRegistryState = CoreRegistryState{Verified: false}
	setRuntimeCoreValidatorIDs(nil)

	if n.isCoreValidatorCurrent("A") {
		t.Fatalf("expected static core config to stop granting core membership on existing chain")
	}
	if !n.requiresWalletAuthCurrent("A") {
		t.Fatalf("expected existing-chain static core node to require wallet auth without verified registry")
	}
	if n.hasRequiredValidatorStake() {
		t.Fatalf("expected existing-chain static core node to lose core stake exemption without verified registry")
	}
	status := n.runtimeStatusSnapshot()
	if status.CoreAuthoritySource != "on_chain_validator_state" {
		t.Fatalf("expected on_chain_validator_state source, got=%q", status.CoreAuthoritySource)
	}
	if status.CoreMembership != "non_core" {
		t.Fatalf("expected non-core membership without verified registry, got=%q", status.CoreMembership)
	}
}

func TestVerifiedCoreRegistryPromotionDoesNotGrantPostBootstrapCorePrivileges(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	IsTestnet = false
	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true

	n := makeStrictActivationNode(50)
	n.ID = "F"
	n.ValidatorKey = strictActivationTestValidatorKey(76, "F")
	n.coreRegistryEntries = map[string]CoreRegistryEntry{
		"F": {ID: "F", Status: coreRegistryStatusActive},
	}
	n.coreRegistryState = CoreRegistryState{
		Verified:        true,
		EffectiveHeight: 1,
		ActiveCoreSet:   []string{"F"},
	}
	setRuntimeCoreValidatorIDs([]string{"F"})

	if n.isCoreValidatorCurrent("F") {
		t.Fatalf("expected post-bootstrap validator to remain non-core for participation semantics")
	}
	if !n.requiresWalletAuthCurrent("F") {
		t.Fatalf("expected wallet auth to remain required after bootstrap")
	}
	if n.hasRequiredValidatorStake() {
		t.Fatalf("expected no post-bootstrap core stake exemption without ledger stake")
	}
	status := n.runtimeStatusSnapshot()
	if status.CoreAuthoritySource != "on_chain_validator_state" {
		t.Fatalf("expected on_chain_validator_state source, got=%q", status.CoreAuthoritySource)
	}
	if status.CoreMembership != "non_core" {
		t.Fatalf("expected non-core membership after bootstrap, got=%q", status.CoreMembership)
	}
}

func TestBootstrapLaneDoesNotGrantCoreMembership(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ValidatorOnboardingBootstrapLaneEnabled = true
	ValidatorOnboardingBootstrapMaxNewSlots = 1

	height := uint64(200)
	loadStrictActivationRegistry(1)
	setStrictActivationPubKeys(true)
	setRuntimeCoreValidatorIDs([]string{"A", "B", "C", "D"})

	n := makeStrictActivationNode(height)
	admitted, candidates, _ := computeValidatorBootstrapLaneAdmissions(
		[]string{"F"},
		map[string]uint64{},
		1,
		height,
		GlobalValidatorRegistry.Snapshot(),
		1,
	)
	if !containsNormalizedValidatorID(candidates, "F") {
		t.Fatalf("expected F to be considered for bootstrap lane, got=%v", candidates)
	}
	if !containsNormalizedValidatorID(admitted, "F") {
		t.Fatalf("expected F to be admitted via bootstrap lane, got=%v", admitted)
	}
	if n.isCoreValidatorCurrent("F") {
		t.Fatalf("bootstrap lane must not promote F to core membership")
	}
	if !n.requiresWalletAuthCurrent("F") {
		t.Fatalf("bootstrap lane must not bypass wallet auth for non-core validator")
	}
}

func TestSyncWatchdogProviderRotationExcludesLastFailedPeer(t *testing.T) {
	n := &Node{}
	n.setSyncAvoidProviderOnce("12D3KooWFailedPeer")

	if got := n.consumeSyncAvoidProviderOnce(); got != "12D3KooWFailedPeer" {
		t.Fatalf("expected first consume to return the failed provider, got=%q", got)
	}
	if got := n.consumeSyncAvoidProviderOnce(); got != "" {
		t.Fatalf("expected second consume to clear one-shot avoidance, got=%q", got)
	}
}

func TestSnapshotCheckpointProofVerification(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	SyncTrustedSnapshotRequireCheckpointProof = true

	key := strictActivationTestValidatorKey(73, "F")
	n := makeStrictActivationNode(50)
	n.ID = "F"
	n.ValidatorKey = key

	validatorPubKeysMu.Lock()
	ValidatorPubKeys["F"] = append(ed25519.PublicKey(nil), key.PublicKey...)
	validatorPubKeysMu.Unlock()

	snapshot := &StateSnapshot{
		Version:     SnapshotVersion,
		Height:      50,
		BlockHash:   "aa65563b",
		StateRoot:   "d255adbd",
		LedgerHash:  HashLedger(n.Ledger),
		GenesisHash: GenesisHash,
		Validators: map[string]bool{
			"A": true,
			"B": true,
			"C": true,
			"D": true,
			"F": true,
		},
		ValidatorSetHeight: 50,
	}
	snapshot.ValidatorSetHash = ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	populateSnapshotDerivedFields(snapshot)
	n.attachSnapshotCheckpointProof(snapshot)

	if !n.verifySnapshotCheckpointProofForValidator(snapshot, "F") {
		t.Fatalf("expected valid checkpoint proof verification for self signer")
	}

	snapshot.BlockHash = "tampered_hash"
	if n.verifySnapshotCheckpointProofForValidator(snapshot, "F") {
		t.Fatalf("expected tampered snapshot payload to fail proof verification")
	}
}

func TestWalletAuthBindingRestoresAcrossRestartWithSameDatadirAndKey(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.DataDir = t.TempDir()
	n.ValidatorKey = strictActivationTestValidatorKey(71, "N")

	pub := strictActivationTestPub(91)
	walletAddr := AddressFromPublicKey(pub)
	if walletAddr == "" {
		t.Fatalf("expected deterministic wallet address from test pubkey")
	}
	walletPubHex := hex.EncodeToString(pub)

	n.persistWalletAuthBinding(walletAddr, walletPubHex)

	authMu.Lock()
	authWalletAddr = ""
	authWalletPub = ""
	authNodeID = ""
	authReady = false
	authMu.Unlock()

	if !n.restoreWalletAuthBinding() {
		t.Fatalf("expected wallet auth binding restore for same datadir/key identity")
	}
	if !n.hasWalletLoginForValidator() {
		t.Fatalf("expected wallet login gate satisfied after restore")
	}
	authMu.Lock()
	gotNodeID := authNodeID
	gotWallet := authWalletAddr
	authMu.Unlock()
	if gotNodeID != "N" {
		t.Fatalf("unexpected restored node id: got=%s want=N", gotNodeID)
	}
	if !addressesEqual(gotWallet, walletAddr) {
		t.Fatalf("unexpected restored wallet: got=%s want=%s", gotWallet, walletAddr)
	}
}

func TestWalletAuthBindingInvalidatedOnValidatorKeyRotation(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	ValidatorRequireStake = false

	n := makeStrictActivationNode(50)
	n.ID = "N"
	n.DataDir = t.TempDir()
	n.ValidatorKey = strictActivationTestValidatorKey(72, "N")

	pub := strictActivationTestPub(92)
	walletAddr := AddressFromPublicKey(pub)
	if walletAddr == "" {
		t.Fatalf("expected deterministic wallet address from test pubkey")
	}
	walletPubHex := hex.EncodeToString(pub)
	n.persistWalletAuthBinding(walletAddr, walletPubHex)

	// Simulate validator identity/key rotation.
	n.ValidatorKey = strictActivationTestValidatorKey(73, "N")

	authMu.Lock()
	authWalletAddr = ""
	authWalletPub = ""
	authNodeID = ""
	authReady = false
	authMu.Unlock()

	if n.restoreWalletAuthBinding() {
		t.Fatalf("expected restore to fail after validator key rotation")
	}
	if n.hasWalletLoginForValidator() {
		t.Fatalf("expected wallet login gate blocked after key-rotation invalidation")
	}
}

func TestWaitForWalletLoginNormalizesNodeIDComparison(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ValidatorRequireStake = false
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := makeStrictActivationNode(50)
	n.ID = "Talha"
	n.ValidatorKey = strictActivationTestValidatorKey(74, "Talha")
	n.Ledger = GenesisLedger()

	authMu.Lock()
	authWalletAddr = "MSC01testwallet"
	authNodeID = "TALHA"
	authReady = true
	authMu.Unlock()

	if !n.WaitForWalletLogin(context.Background(), 20*time.Millisecond) {
		t.Fatalf("expected wallet login gate to honor normalized node id match")
	}
	if !n.hasWalletLoginForValidator() {
		t.Fatalf("expected normalized auth node id to satisfy wallet login gate")
	}
}
