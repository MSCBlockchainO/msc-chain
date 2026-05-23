package main

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func withAutohealHardeningGlobals(t *testing.T) func() {
	t.Helper()
	oldCore := append([]string{}, ConfigAuthCoreValidators...)
	oldMode := ValidatorSetAutohealMode
	oldTTL := ValidatorLivenessHeartbeatTTLSeconds
	oldGrace := ValidatorLivenessGraceSeconds
	oldAllowRotation := ValidatorAllowIdentityRotationOnExistingChain
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldMinStake := ValidatorMinStake
	oldMismatchThreshold := ValidatorSetMismatchResyncThreshold
	oldNearTipForce := ValidatorSetAutohealNearTipForceAfter
	oldBanned := append([]string{}, ValidatorBannedList...)
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	runtimeCoreValidatorSet.mu.RLock()
	oldRuntimeCore := make([]string, 0, len(runtimeCoreValidatorSet.ids))
	for id := range runtimeCoreValidatorSet.ids {
		oldRuntimeCore = append(oldRuntimeCore, id)
	}
	runtimeCoreValidatorSet.mu.RUnlock()
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
		ConfigAuthCoreValidators = oldCore
		ValidatorSetAutohealMode = oldMode
		ValidatorLivenessHeartbeatTTLSeconds = oldTTL
		ValidatorLivenessGraceSeconds = oldGrace
		ValidatorAllowIdentityRotationOnExistingChain = oldAllowRotation
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ValidatorMinStake = oldMinStake
		ValidatorSetMismatchResyncThreshold = oldMismatchThreshold
		ValidatorSetAutohealNearTipForceAfter = oldNearTipForce
		setValidatorBannedValidators(oldBanned)
		GlobalValidatorRegistry.Load(oldRegistry)
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
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

func testDeterministicPub(seedByte byte) ed25519.PublicKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = seedByte + byte(i)
	}
	key := ed25519.NewKeyFromSeed(seed)
	pub := key.Public().(ed25519.PublicKey)
	return append(ed25519.PublicKey(nil), pub...)
}

func TestCoreQuorumVotesIncludesSelfWhenExpectedHashMatches(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5

	expected := "a006925f"
	now := time.Now()
	n := &Node{
		ID: "B",
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 130, BlockHash: "h130", ValidatorSetHash: expected},
			},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"A": {LastSeen: now, ValidatorSetHash: expected},
			"C": {LastSeen: now, ValidatorSetHash: expected},
			"D": {LastSeen: now, ValidatorSetHash: "04f4a87c"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			130: expected,
		},
		frozenValidatorsByHeight: map[uint64][]string{
			130: {"A", "B", "C", "D"},
		},
	}

	votes, required := n.coreQuorumVotesForExpectedHash(130, expected)
	if required != 3 {
		t.Fatalf("unexpected required quorum: got=%d want=3", required)
	}
	if votes != 3 {
		t.Fatalf("self+2 peer quorum expected votes=3, got=%d", votes)
	}
}

func TestShouldTreatValidatorSetMismatchAsPeerDriftWhenCoreQuorumConfirmsExpected(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5

	expected := "a006925f"
	got := "04f4a87c"
	now := time.Now()
	n := &Node{
		ID: "B",
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 130, BlockHash: "h130", ValidatorSetHash: expected},
			},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"A": {LastSeen: now, ValidatorSetHash: expected},
			"C": {LastSeen: now, ValidatorSetHash: expected},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			130: expected,
		},
		frozenValidatorsByHeight: map[uint64][]string{
			130: {"A", "B", "C", "D"},
		},
	}

	if !n.shouldTreatValidatorSetMismatchAsPeerDrift(130, expected, got) {
		t.Fatalf("expected mismatch to be classified as peer drift when expected hash has validator quorum")
	}
	n.validatorSetMismatchMu.Lock()
	reason := n.validatorAutohealLastReason
	n.validatorSetMismatchMu.Unlock()
	if !strings.HasPrefix(reason, "peer_drift_validator_quorum_") {
		t.Fatalf("expected peer drift reason to be recorded, got=%q", reason)
	}
}

func TestShouldTreatValidatorSetMismatchAsPeerDriftFalseWithoutCoreQuorum(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5

	expected := "a006925f"
	got := "04f4a87c"
	now := time.Now()
	n := &Node{
		ID: "B",
		validatorStatus: map[string]*ValidatorStatus{
			"A": {LastSeen: now, ValidatorSetHash: expected},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			130: expected,
		},
		frozenValidatorsByHeight: map[uint64][]string{
			130: {"A", "B", "C", "D"},
		},
	}

	if n.shouldTreatValidatorSetMismatchAsPeerDrift(130, expected, got) {
		t.Fatalf("expected false without validator quorum support for expected hash")
	}
}

func TestTryRepairValidatorSetHashDefersWhenStrictCoreQuorumUnavailable(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5

	expected := ValidatorSetHash([]string{"A", "B", "C", "D"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	n := &Node{
		ID: "F",
		frozenValidatorsByHeight: map[uint64][]string{
			495: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			495: expected,
		},
	}

	if n.tryRepairValidatorSetHash(495, got) {
		t.Fatalf("expected repair to defer when validator quorum evidence is unavailable")
	}
	if n.consensusRecomputePauseActive() {
		t.Fatalf("expected no recompute pause while validator quorum evidence is unavailable")
	}

	_, reason, mismatchHeight, _, _, _ := n.validatorAutohealStatusSnapshot()
	if mismatchHeight != 495 {
		t.Fatalf("expected mismatch height to be tracked at 495, got=%d", mismatchHeight)
	}
	if !strings.HasPrefix(reason, "strict_autofix_guard_validator_quorum_") {
		t.Fatalf("expected validator quorum wait reason, got=%q", reason)
	}
}

func TestTryRepairValidatorSetHashDefersDuringSnapshotSession(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	expected := ValidatorSetHash([]string{"A", "B", "C", "D"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 75, BlockHash: "h75", ValidatorSetHash: expected},
			},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			75: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			75: expected,
		},
	}

	n.startSnapshotSession(120, "test_snapshot_lock")
	if n.tryRepairValidatorSetHash(75, got) {
		t.Fatalf("expected repair to defer while snapshot session is active")
	}

	_, reason, mismatchHeight, trackedExpected, trackedGot, _ := n.validatorAutohealStatusSnapshot()
	if reason != "snapshot_session_active" {
		t.Fatalf("expected snapshot session blocker reason, got=%q", reason)
	}
	if mismatchHeight != 75 {
		t.Fatalf("expected mismatch height tracked at 75, got=%d", mismatchHeight)
	}
	if trackedExpected != expected || trackedGot != got {
		t.Fatalf("expected tracked hashes updated, got expected=%q got=%q", trackedExpected, trackedGot)
	}
}

func TestRecordValidatorSetMismatchDeferredWhileSyncingLargeLag(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100, BlockHash: "h100"}},
		},
		Consensus: &ConsensusState{
			Syncing: true,
		},
	}

	if n.recordValidatorSetMismatchWithLocal(100, 120, "exp", "got") {
		t.Fatalf("expected mismatch trigger threshold not reached on first hit")
	}

	_, reason, mismatchHeight, expected, got, _ := n.validatorAutohealStatusSnapshot()
	if mismatchHeight != 120 {
		t.Fatalf("expected deferred mismatch height tracked at 120, got=%d", mismatchHeight)
	}
	if reason != "validator_set_hash_mismatch" {
		t.Fatalf("expected direct mismatch reason in snapshot-first mode, got=%q", reason)
	}
	if expected != "exp" || got != "got" {
		t.Fatalf("expected mismatch hashes retained in status snapshot, got expected=%q got=%q", expected, got)
	}
}

func TestRecordPeerDriftTupleThrottlesLogs(t *testing.T) {
	n := &Node{}
	height := uint64(130)
	expected := "a006925f"
	got := "04f4a87c"
	cooldown := 3 * time.Second

	count, shouldLog := n.recordPeerDriftTuple(height, expected, got, cooldown)
	if count != 1 || !shouldLog {
		t.Fatalf("expected first tuple hit to log, got count=%d shouldLog=%t", count, shouldLog)
	}

	count, shouldLog = n.recordPeerDriftTuple(height, expected, got, cooldown)
	if count != 2 || shouldLog {
		t.Fatalf("expected immediate duplicate tuple hit to be throttled, got count=%d shouldLog=%t", count, shouldLog)
	}

	key := peerDriftTupleKey(height, expected, got)
	n.validatorSetMismatchMu.Lock()
	n.peerDriftTupleLastLog[key] = time.Now().Add(-4 * time.Second)
	n.validatorSetMismatchMu.Unlock()

	count, shouldLog = n.recordPeerDriftTuple(height, expected, got, cooldown)
	if count != 3 || !shouldLog {
		t.Fatalf("expected log after cooldown, got count=%d shouldLog=%t", count, shouldLog)
	}

	n.validatorSetMismatchMu.Lock()
	n.peerDriftTupleCount[key] = 24
	n.peerDriftTupleLastLog[key] = time.Now()
	n.validatorSetMismatchMu.Unlock()

	count, shouldLog = n.recordPeerDriftTuple(height, expected, got, cooldown)
	if count != 25 || !shouldLog {
		t.Fatalf("expected periodic summary log at count=25, got count=%d shouldLog=%t", count, shouldLog)
	}
}

func TestShouldTriggerValidatorSetSyncOverrideDedupesTuple(t *testing.T) {
	n := &Node{}
	height := uint64(21)
	expected := "a006925f"
	got := "04f4a87c"

	if !n.shouldTriggerValidatorSetSyncOverride(height, expected, got) {
		t.Fatalf("expected first tuple to trigger sync override")
	}
	if n.shouldTriggerValidatorSetSyncOverride(height, expected, got) {
		t.Fatalf("expected duplicate tuple inside cooldown to be deduped")
	}

	n.validatorSetMismatchMu.Lock()
	n.validatorSetSyncOverrideAt = time.Now().Add(-validatorSetSyncOverrideCooldown - time.Second)
	n.validatorSetMismatchMu.Unlock()

	if !n.shouldTriggerValidatorSetSyncOverride(height, expected, got) {
		t.Fatalf("expected tuple to trigger again after cooldown expiry")
	}
}

func TestRecordValidatorSetMismatchWithLocalStrictCoreQuorumCoalescesTrigger(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorSetMismatchResyncThreshold = 2
	ValidatorSetAutohealNearTipForceAfter = 0

	n := &Node{}
	localHeight := uint64(20)
	mismatchHeight := uint64(21)
	expected := "a006925f"
	got := "04f4a87c"

	if n.recordValidatorSetMismatchWithLocal(localHeight, mismatchHeight, expected, got) {
		t.Fatalf("expected first hit to only record mismatch")
	}
	if !n.recordValidatorSetMismatchWithLocal(localHeight, mismatchHeight, expected, got) {
		t.Fatalf("expected second hit to trigger repair threshold")
	}
	if n.recordValidatorSetMismatchWithLocal(localHeight, mismatchHeight, expected, got) {
		t.Fatalf("expected immediate post-trigger hit to be coalesced")
	}
	if n.recordValidatorSetMismatchWithLocal(localHeight, mismatchHeight, expected, got) {
		t.Fatalf("expected duplicate trigger inside cooldown to be coalesced")
	}
}

func TestTransitionBarrierPauseTupleDedupe(t *testing.T) {
	n := &Node{}
	reason := "validator_set_transition_barrier"
	height := uint64(105)
	hash := "e4733c74"

	if !n.shouldRequestTransitionBarrierPause(reason, height, hash, 1, 5, 4) {
		t.Fatalf("expected first transition-barrier tuple to trigger pause request")
	}
	if n.shouldRequestTransitionBarrierPause(reason, height, hash, 1, 5, 4) {
		t.Fatalf("expected duplicate tuple inside cooldown to be deduped")
	}
	if !n.shouldRequestTransitionBarrierPause(reason, height, hash, 2, 5, 4) {
		t.Fatalf("expected changed tuple evidence to bypass dedupe")
	}

	key := transitionBarrierPauseTupleKey(reason, height, hash, 1, 5, 4)
	n.transitionBarrierPauseMu.Lock()
	if n.transitionBarrierPauseLast == nil {
		n.transitionBarrierPauseLast = make(map[string]time.Time)
	}
	n.transitionBarrierPauseLast[key] = time.Now().Add(-transitionBarrierPauseDedupeCooldown - time.Second)
	n.transitionBarrierPauseMu.Unlock()

	if !n.shouldRequestTransitionBarrierPause(reason, height, hash, 1, 5, 4) {
		t.Fatalf("expected tuple to trigger pause again after cooldown expiry")
	}
}

func TestTransitionBarrierRetryAttemptsEveryBlockForUnchangedTuple(t *testing.T) {
	prevDelay := ValidatorSetActivationDelay
	prevMode := TransitionBarrierRetryMode
	ValidatorSetActivationDelay = 5
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	defer func() {
		ValidatorSetActivationDelay = prevDelay
		TransitionBarrierRetryMode = prevMode
	}()

	n := &Node{}
	reason := "validator_set_transition_barrier"
	updateHeight := uint64(105)
	hash := "e4733c74"

	attempt, tupleChanged, nextRetry := n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 200, hash, 1, 5, 4)
	if !attempt || tupleChanged {
		t.Fatalf("expected first failure to trigger retry registration, attempt=%t tupleChanged=%t", attempt, tupleChanged)
	}
	if nextRetry != 224 {
		t.Fatalf("unexpected first next retry height: got=%d want=224", nextRetry)
	}

	attempt, tupleChanged, nextRetry = n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 201, hash, 1, 5, 4)
	if attempt || tupleChanged {
		t.Fatalf("expected unchanged tuple to wait for checkpoint retry, attempt=%t tupleChanged=%t", attempt, tupleChanged)
	}
	if nextRetry != 224 {
		t.Fatalf("unexpected rolled retry height: got=%d want=224", nextRetry)
	}

	attempt, tupleChanged, nextRetry = n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 202, hash, 1, 5, 4)
	if attempt || tupleChanged {
		t.Fatalf("expected deterministic checkpoint wait without tuple change, attempt=%t tupleChanged=%t", attempt, tupleChanged)
	}
	if nextRetry != 224 {
		t.Fatalf("unexpected deterministic retry height: got=%d want=224", nextRetry)
	}
}

func TestTransitionBarrierRetryTupleChangeStillFlaggedInPerBlockMode(t *testing.T) {
	prevDelay := ValidatorSetActivationDelay
	prevMode := TransitionBarrierRetryMode
	ValidatorSetActivationDelay = 5
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	defer func() {
		ValidatorSetActivationDelay = prevDelay
		TransitionBarrierRetryMode = prevMode
	}()

	n := &Node{}
	reason := "validator_set_transition_barrier"
	updateHeight := uint64(105)
	hash := "e4733c74"

	_, _, nextRetry := n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 200, hash, 1, 5, 4)
	if nextRetry != 224 {
		t.Fatalf("unexpected initial retry height: got=%d want=224", nextRetry)
	}

	attempt, tupleChanged, nextRetry := n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 201, hash, 2, 5, 4)
	if attempt || !tupleChanged {
		t.Fatalf("expected tuple-change marker while still waiting for checkpoint, attempt=%t tupleChanged=%t", attempt, tupleChanged)
	}
	if nextRetry != 224 {
		t.Fatalf("unexpected retry height after tuple-change update: got=%d want=224", nextRetry)
	}
}

func TestTransitionBarrierRetryUsesRollingDelayCadence(t *testing.T) {
	prevDelay := ValidatorSetActivationDelay
	prevMode := TransitionBarrierRetryMode
	ValidatorSetActivationDelay = 5
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	defer func() {
		ValidatorSetActivationDelay = prevDelay
		TransitionBarrierRetryMode = prevMode
	}()

	n := &Node{}
	reason := "validator_set_transition_barrier"
	updateHeight := uint64(105)
	hash := "e4733c74"

	_, _, nextRetry := n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 200, hash, 1, 5, 4)
	if nextRetry != 224 {
		t.Fatalf("unexpected initial retry height: got=%d want=224", nextRetry)
	}

	attempt, tupleChanged, nextRetry := n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 205, hash, 1, 5, 4)
	if attempt || tupleChanged {
		t.Fatalf("expected deterministic checkpoint wait without tuple change, attempt=%t tupleChanged=%t", attempt, tupleChanged)
	}
	if nextRetry != 224 {
		t.Fatalf("unexpected second retry height: got=%d want=224", nextRetry)
	}

	attempt, tupleChanged, nextRetry = n.shouldAttemptTransitionBarrierRetry(reason, updateHeight, 206, hash, 1, 5, 4)
	if attempt || tupleChanged {
		t.Fatalf("expected checkpoint-bound retry cadence without tuple change, attempt=%t tupleChanged=%t", attempt, tupleChanged)
	}
	if nextRetry != 224 {
		t.Fatalf("unexpected third retry height: got=%d want=224", nextRetry)
	}
}

func TestSafeModeDefersTransitionApply(t *testing.T) {
	prevDelay := ValidatorSetActivationDelay
	prevMode := TransitionBarrierRetryMode
	prevV2 := ValidatorSetActivationModelV2Height
	prevCommit := ValidatorSetCommitmentV2Height
	prevCheckpoint := SyncCheckpointIntervalBlocks
	ValidatorSetActivationDelay = 5
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	ValidatorSetActivationModelV2Height = 1
	ValidatorSetCommitmentV2Height = 1
	SyncCheckpointIntervalBlocks = 8
	defer func() {
		ValidatorSetActivationDelay = prevDelay
		TransitionBarrierRetryMode = prevMode
		ValidatorSetActivationModelV2Height = prevV2
		ValidatorSetCommitmentV2Height = prevCommit
		SyncCheckpointIntervalBlocks = prevCheckpoint
	}()

	n := makeStrictActivationNode(8)
	n.epochValidators[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorsByHeight[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorHashByHeight[8] = ValidatorSetHash([]string{"A", "B", "C", "D"})
	n.pendingValidators = map[string]uint64{"F": 1}
	n.pendingValidatorRemovals = make(map[string]uint64)
	if n.safeModeUntilByHeight == nil {
		n.safeModeUntilByHeight = make(map[uint64]time.Time)
	}
	n.safeModeUntilByHeight[8] = time.Now().Add(5 * time.Second)

	nextHash := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	now := time.Now()
	for _, id := range []string{"B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			FinalizedHeight:    8,
			ReportedHeight:     8,
			ExecEpoch:          9,
			ValidatorSetHeight: 9,
			ValidatorSetHash:   nextHash,
		}
	}

	n.applyScheduledValidatorUpdates(8)
	if containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected transition apply to defer while safe mode is active")
	}

	n.safeModeUntilByHeight[8] = time.Now().Add(-time.Second)
	n.applyScheduledValidatorUpdates(8)
	if !containsValidatorIDInSet(n.frozenValidatorsForHeight(8), "F") {
		t.Fatalf("expected transition apply to resume after safe mode ends")
	}
}

func TestDeterministicTransitionApplyIgnoresCurrentValidatorsCache(t *testing.T) {
	prevDelay := ValidatorSetActivationDelay
	prevMode := TransitionBarrierRetryMode
	prevV2 := ValidatorSetActivationModelV2Height
	prevCommit := ValidatorSetCommitmentV2Height
	prevCheckpoint := SyncCheckpointIntervalBlocks
	ValidatorSetActivationDelay = 5
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	ValidatorSetActivationModelV2Height = 1
	ValidatorSetCommitmentV2Height = 1
	SyncCheckpointIntervalBlocks = 8
	defer func() {
		ValidatorSetActivationDelay = prevDelay
		TransitionBarrierRetryMode = prevMode
		ValidatorSetActivationModelV2Height = prevV2
		ValidatorSetCommitmentV2Height = prevCommit
		SyncCheckpointIntervalBlocks = prevCheckpoint
	}()

	n := makeStrictActivationNode(8)
	n.currentValidators = []string{"Z"}
	n.epochValidators[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorsByHeight[8] = []string{"A", "B", "C", "D"}
	n.frozenValidatorHashByHeight[8] = ValidatorSetHash([]string{"A", "B", "C", "D"})
	n.pendingValidators = map[string]uint64{"F": 1}
	n.pendingValidatorRemovals = make(map[string]uint64)

	nextHash := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	now := time.Now()
	for _, id := range []string{"B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:           now,
			FinalizedHeight:    8,
			ReportedHeight:     8,
			ExecEpoch:          9,
			ValidatorSetHeight: 9,
			ValidatorSetHash:   nextHash,
		}
	}

	n.applyScheduledValidatorUpdates(8)
	frozen := n.frozenValidatorsForHeight(8)
	if !containsValidatorIDInSet(frozen, "F") {
		t.Fatalf("expected transition apply derived from committed set, got=%v", frozen)
	}
	if containsValidatorIDInSet(frozen, "Z") {
		t.Fatalf("stale currentValidators cache must not influence deterministic transition, got=%v", frozen)
	}
}

func TestTransitionBarrierRetryPruneRemovesStaleState(t *testing.T) {
	n := &Node{
		transitionBarrierRetryStateByKey: map[transitionBarrierRetryKey]transitionBarrierRetryState{
			transitionBarrierRetryStateKey("validator_set_transition_barrier", 105, "e4733c74"): {
				NextRetryHeight: 205,
				LastFailTuple:   "1|5|4",
				LastUpdatedAt:   time.Now(),
			},
			transitionBarrierRetryStateKey("validator_set_transition_barrier", 120, "a006925f"): {
				NextRetryHeight: 220,
				LastFailTuple:   "2|5|4",
				LastUpdatedAt:   time.Now(),
			},
		},
	}

	activeHeights := map[uint64]struct{}{
		120: {},
	}
	n.pruneTransitionBarrierRetry(activeHeights)

	if len(n.transitionBarrierRetryStateByKey) != 1 {
		t.Fatalf("expected stale retry state to be pruned, got=%d entries", len(n.transitionBarrierRetryStateByKey))
	}
	if _, ok := n.transitionBarrierRetryStateByKey[transitionBarrierRetryStateKey("validator_set_transition_barrier", 120, "a006925f")]; !ok {
		t.Fatalf("expected active retry state at height=120 to remain")
	}
}

func TestApplyStartupConsensusRecoveryPreservesReplayedFreezeMaps(t *testing.T) {
	existingHash := ValidatorSetHash([]string{"A", "B"})
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 1, BlockHash: "h1"},
			},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			42: {"A", "B"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			42: existingHash,
		},
	}

	n.applyStartupConsensusRecovery()

	if _, ok := n.frozenValidatorsByHeight[42]; !ok {
		t.Fatalf("expected existing replayed frozen validator set at height 42 to be preserved")
	}
	if got := strings.TrimSpace(n.frozenValidatorHashByHeight[42]); got != existingHash {
		t.Fatalf("expected replayed frozen hash to be preserved, got=%q want=%q", got, existingHash)
	}
}

func TestStartupSelfCheckReconcilesStaleFrozenHashWhenValidatorListMatches(t *testing.T) {
	expected := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	stale := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 325, BlockHash: "h325", ValidatorSetHash: expected},
			},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			325: {"A", "B", "C", "D", "F"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			325: stale,
		},
	}

	ok, reason := n.startupValidatorSetSelfCheck()
	if !ok || reason != "ready" {
		t.Fatalf("expected startup self-check to recover stale frozen hash, ok=%t reason=%q", ok, reason)
	}
	hash, okHash := n.frozenValidatorSetHash(325)
	if !okHash || !strings.EqualFold(strings.TrimSpace(hash), expected) {
		t.Fatalf("expected frozen hash reconciled to %q, got %q", expected, hash)
	}
	statusOK, statusHeight, statusExpected, statusGot, statusReason := n.validatorStartupCheckStatusSnapshot()
	if !statusOK {
		t.Fatalf("expected startup check status ok after reconciliation")
	}
	if statusHeight != 325 {
		t.Fatalf("expected startup status height 325, got %d", statusHeight)
	}
	if !strings.EqualFold(strings.TrimSpace(statusExpected), expected) || !strings.EqualFold(strings.TrimSpace(statusGot), expected) {
		t.Fatalf("expected startup status hashes to match expected=%q got expected=%q got=%q", expected, statusExpected, statusGot)
	}
	if statusReason != "startup_validator_set_hash_reconciled" {
		t.Fatalf("expected reconciled startup reason, got %q", statusReason)
	}
}

func TestStartupSelfCheckDefersDuringFreshLateJoinSnapshotAnchor(t *testing.T) {
	n := &Node{
		ID:         "F",
		Role:       "validator",
		Blockchain: &Blockchain{},
	}
	n.noteLateJoinAuthoritySample(623, "04f4a87cfeedbeef")
	n.commitMu.Lock()
	n.finalizedHeight = 622
	n.commitMu.Unlock()

	ok, reason := n.startupValidatorSetSelfCheck()
	if ok || reason != "late_join_snapshot_anchor_pending" {
		t.Fatalf("expected startup self-check to defer during fresh late join, ok=%t reason=%q", ok, reason)
	}
	statusOK, statusHeight, _, _, statusReason := n.validatorStartupCheckStatusSnapshot()
	if statusOK {
		t.Fatalf("expected deferred startup status to remain not ready")
	}
	if statusHeight != 623 {
		t.Fatalf("expected deferred startup height 623, got %d", statusHeight)
	}
	if statusReason != "late_join_snapshot_anchor_pending" {
		t.Fatalf("unexpected deferred startup reason: %q", statusReason)
	}
}

func TestExpectedValidatorSetHashPrefersChainBlockHashOverFrozen(t *testing.T) {
	chainHash := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	staleHash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	if strings.EqualFold(chainHash, staleHash) {
		t.Fatalf("test setup invalid: expected different hashes")
	}

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 325, BlockHash: "h325", ValidatorSetHash: chainHash},
			},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			325: staleHash,
		},
	}

	got := n.expectedValidatorSetHash(325)
	if !strings.EqualFold(strings.TrimSpace(got), chainHash) {
		t.Fatalf("expected chain hash %q, got %q", chainHash, got)
	}
}

func TestExpectedValidatorSetHashSuppressesDerivedWhileSyncing(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 100, BlockHash: "h100"},
			},
		},
		Consensus: &ConsensusState{
			Syncing: true,
		},
		epochValidators: map[uint64][]string{
			101: {"A", "B", "C", "D"},
		},
		validatorSetHeight: 101,
	}

	if got := strings.TrimSpace(n.expectedValidatorSetHash(101)); got != "" {
		t.Fatalf("expected derived hash to be suppressed while syncing, got=%q", got)
	}
	if _, ok := n.frozenValidatorSetHash(101); ok {
		t.Fatalf("expected syncing path to avoid freezing speculative hash")
	}

	n.Consensus.mu.Lock()
	n.Consensus.Syncing = false
	n.Consensus.mu.Unlock()

	got := strings.TrimSpace(n.expectedValidatorSetHash(101))
	if got != "" {
		t.Fatalf("expected no runtime-derived hash after sync clears, got %q", got)
	}
}

func TestConsensusValidatorsForHeightNoGenesisFallbackOnExistingChain(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 88, BlockHash: "h88"},
			},
		},
		GenesisValidators:  []string{"A", "B", "C", "D"},
		ValidatorKey:       ValidatorKey{ID: "A"},
		validatorSetHeight: 88,
	}

	if got := n.consensusValidatorsForHeight(89); len(got) != 0 {
		t.Fatalf("expected unresolved validator set on existing chain, got=%v", got)
	}
}

func TestSnapshotEpochValidatorsSkipsDynamicFallbackOnExistingChain(t *testing.T) {
	oldDynamic := DynamicValidatorSelectionEnabled
	oldDeterministic := DeterministicValidatorSelection
	t.Cleanup(func() {
		DynamicValidatorSelectionEnabled = oldDynamic
		DeterministicValidatorSelection = oldDeterministic
	})
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 10, BlockHash: "h10"},
			},
		},
		epochValidators:    make(map[uint64][]string),
		validatorSetHeight: 10,
		GenesisValidators:  []string{"A", "B", "C", "D"},
		ValidatorKey:       ValidatorKey{ID: "A"},
	}

	n.snapshotEpochValidators(11)
	if got := n.epochValidators[11]; len(got) != 0 {
		t.Fatalf("expected no speculative snapshot validators on existing chain, got=%v", got)
	}
	if got := n.GetConsensusValidators(11); len(got) != 0 {
		t.Fatalf("expected consensus validators to remain unresolved, got=%v", got)
	}
}

func TestStartupSelfCheckUsesChainAuthoritativeHashWhenFrozenStateIsStale(t *testing.T) {
	chainHash := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	staleHash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	if strings.EqualFold(chainHash, staleHash) {
		t.Fatalf("test setup invalid: expected different hashes")
	}

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 410, BlockHash: "h410", ValidatorSetHash: chainHash},
			},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			410: staleHash,
		},
	}

	ok, reason := n.startupValidatorSetSelfCheck()
	if !ok || reason != "ready" {
		t.Fatalf("expected startup self-check ready with chain-authoritative hash, ok=%t reason=%q", ok, reason)
	}
	statusOK, statusHeight, statusExpected, statusGot, statusReason := n.validatorStartupCheckStatusSnapshot()
	if !statusOK || statusHeight != 410 {
		t.Fatalf("unexpected startup status: ok=%t height=%d", statusOK, statusHeight)
	}
	if !strings.EqualFold(strings.TrimSpace(statusExpected), chainHash) || !strings.EqualFold(strings.TrimSpace(statusGot), chainHash) {
		t.Fatalf("expected status hashes to use chain hash %q, got expected=%q got=%q", chainHash, statusExpected, statusGot)
	}
	if statusReason != "startup_validator_set_chain_authoritative" {
		t.Fatalf("expected chain-authoritative startup reason, got %q", statusReason)
	}
}

func TestReconcileFrozenValidatorSetHashUsesCoreAuthorityCandidate(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)

	expected := ValidatorSetHash([]string{"A", "B", "C", "D"})
	stale := ValidatorSetHash([]string{"A", "B", "C"})
	n := &Node{
		frozenValidatorsByHeight: map[uint64][]string{
			326: {"A", "B", "C"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			326: stale,
		},
	}

	if !n.reconcileFrozenValidatorSetHash(326, expected) {
		t.Fatalf("expected reconciliation to succeed using core authority candidate")
	}
	gotHash, ok := n.frozenValidatorSetHash(326)
	if !ok || !strings.EqualFold(strings.TrimSpace(gotHash), expected) {
		t.Fatalf("expected reconciled frozen hash=%q got=%q", expected, gotHash)
	}
	gotSet := n.frozenValidatorsForHeight(326)
	if strings.Join(gotSet, ",") != "A,B,C,D" {
		t.Fatalf("expected reconciled frozen set A,B,C,D, got=%v", gotSet)
	}
}

func TestMaybeAdoptCoreQuorumValidatorSetHash(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorMinStake = 100
	setValidatorBannedValidators(nil)

	expected := ValidatorSetHash([]string{"A", "B", "C"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()

	n := &Node{
		ID: "F",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 326, BlockHash: "h326", ValidatorSetHash: got, Signatures: []string{"A", "B", "C", "D"}}},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"A": {LastSeen: now, ReportedHeight: 326, FinalizedHeight: 326, ValidatorSetHash: got},
			"B": {LastSeen: now, ReportedHeight: 326, FinalizedHeight: 326, ValidatorSetHash: got},
			"C": {LastSeen: now, ReportedHeight: 326, FinalizedHeight: 326, ValidatorSetHash: got},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			326: expected,
		},
		frozenValidatorsByHeight: map[uint64][]string{
			326: {"A", "B", "C"},
		},
		Ledger:              NewLedger(),
		coreRegistryEntries: map[string]CoreRegistryEntry{},
	}

	n.Ledger.Stakes[stakeKey("wallet-a", "A")] = StakeLock{ValidatorID: "A", Amount: 150, LockedUntil: 400}
	n.Ledger.Stakes[stakeKey("wallet-b", "B")] = StakeLock{ValidatorID: "B", Amount: 150, LockedUntil: 400}
	n.Ledger.Stakes[stakeKey("wallet-c", "C")] = StakeLock{ValidatorID: "C", Amount: 150, LockedUntil: 400}
	n.Ledger.Stakes[stakeKey("wallet-d", "D")] = StakeLock{ValidatorID: "D", Amount: 150, LockedUntil: 400}

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
	})

	pubA := testDeterministicPub(11)
	pubB := testDeterministicPub(12)
	pubC := testDeterministicPub(13)
	pubD := testDeterministicPub(14)
	validatorPubKeysMu.Lock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": pubA,
		"B": pubB,
		"C": pubC,
		"D": pubD,
	}
	validatorPubKeysMu.Unlock()
	n.coreRegistryEntries = map[string]CoreRegistryEntry{
		"A": {ID: "A", RequiredKeyFingerprint: validatorKeyFingerprint(pubA)},
		"B": {ID: "B", RequiredKeyFingerprint: validatorKeyFingerprint(pubB)},
		"C": {ID: "C", RequiredKeyFingerprint: validatorKeyFingerprint(pubC)},
		"D": {ID: "D", RequiredKeyFingerprint: validatorKeyFingerprint(pubD)},
	}

	if !n.shouldAdoptCoreQuorumValidatorSetHash(326, expected, got) {
		t.Fatalf("expected validator quorum to prefer got hash")
	}
	if !n.maybeAdoptCoreQuorumValidatorSetHash(326, expected, got) {
		t.Fatalf("expected adoption of got hash to succeed")
	}
	reconciled, ok := n.frozenValidatorSetHash(326)
	if !ok || !strings.EqualFold(strings.TrimSpace(reconciled), got) {
		t.Fatalf("expected reconciled frozen hash=%q, got=%q", got, reconciled)
	}
}

func TestMaybeAdoptCoreQuorumValidatorSetHashDefersDuringFreshLateJoin(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"

	expected := ValidatorSetHash([]string{"A", "B", "C"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := &Node{
		ID:         "A",
		Blockchain: &Blockchain{},
		frozenValidatorHashByHeight: map[uint64]string{
			326: expected,
		},
	}
	n.noteLateJoinAuthoritySample(326, got)
	n.commitMu.Lock()
	n.finalizedHeight = 325
	n.commitMu.Unlock()

	if n.shouldAdoptCoreQuorumValidatorSetHash(326, expected, got) {
		t.Fatalf("did not expect quorum adoption while fresh late join snapshot anchor is pending")
	}
	if n.maybeAdoptCoreQuorumValidatorSetHash(326, expected, got) {
		t.Fatalf("did not expect adoption while fresh late join snapshot anchor is pending")
	}
	reason := ""
	_, reason, mismatchHeight, _, mismatchGot, _ := n.validatorAutohealStatusSnapshot()
	if reason != "late_join_snapshot_anchor_pending" {
		t.Fatalf("unexpected autoheal wait reason: %q", reason)
	}
	if mismatchHeight != 326 {
		t.Fatalf("unexpected mismatch height: got=%d want=326", mismatchHeight)
	}
	if mismatchGot != got {
		t.Fatalf("unexpected mismatch hash: got=%q want=%q", mismatchGot, got)
	}
}

func TestCoreQuorumVotesUsesPeerHelloFallbackWhenStatusHashIsStale(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5

	expected := ValidatorSetHash([]string{"A", "B", "C"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()

	n := &Node{
		ID: "A",
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 471, BlockHash: "h471", ValidatorSetHash: got},
			},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"B": {LastSeen: now, ValidatorSetHash: expected},
			"C": {LastSeen: now, ValidatorSetHash: expected},
			"D": {LastSeen: now, ValidatorSetHash: got},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			471: got,
		},
		frozenValidatorsByHeight: map[uint64][]string{
			471: {"A", "B", "C", "D"},
		},
		peerToValidator: map[string]string{
			"pB": "B",
			"pC": "C",
			"pD": "D",
		},
		peerSetHash: map[string]string{
			"pB": got,
			"pC": got,
			"pD": got,
		},
		peerHelloOK: map[string]bool{
			"pB": true,
			"pC": true,
			"pD": true,
		},
		connectedPeers: map[string]bool{
			"pB": true,
			"pC": true,
			"pD": true,
		},
	}

	votes, required := n.coreQuorumVotesForExpectedHash(471, got)
	if required != 3 {
		t.Fatalf("unexpected required quorum: got=%d want=3", required)
	}
	if votes != 4 {
		t.Fatalf("expected core votes to include self + peer-hello fallback, got=%d want=4", votes)
	}
	if !n.shouldAdoptCoreQuorumValidatorSetHash(471, expected, got) {
		t.Fatalf("expected validator quorum adoption to succeed via peer-hello fallback")
	}
}

func TestCoreQuorumVotesRejectsConflictingPeerHelloFallback(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5

	expected := ValidatorSetHash([]string{"A", "B", "C"})
	got := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()

	n := &Node{
		ID: "A",
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 471, BlockHash: "h471", ValidatorSetHash: got},
			},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"B": {LastSeen: now, ValidatorSetHash: expected},
			"C": {LastSeen: now, ValidatorSetHash: expected},
			"D": {LastSeen: now, ValidatorSetHash: got},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			471: got,
		},
		frozenValidatorsByHeight: map[uint64][]string{
			471: {"A", "B", "C", "D"},
		},
		peerToValidator: map[string]string{
			"pB1": "B",
			"pB2": "B",
			"pD":  "D",
		},
		peerSetHash: map[string]string{
			"pB1": got,
			"pB2": expected,
			"pD":  got,
		},
		peerHelloOK: map[string]bool{
			"pB1": true,
			"pB2": true,
			"pD":  true,
		},
		connectedPeers: map[string]bool{
			"pB1": true,
			"pB2": true,
			"pD":  true,
		},
	}

	votes, required := n.coreQuorumVotesForExpectedHash(471, got)
	if required != 3 {
		t.Fatalf("unexpected required quorum: got=%d want=3", required)
	}
	if votes != 2 {
		t.Fatalf("expected conflicting peer-hello hashes to be ignored, got votes=%d want=2", votes)
	}
	if n.shouldAdoptCoreQuorumValidatorSetHash(471, expected, got) {
		t.Fatalf("did not expect adoption when quorum depends on conflicting peer-hello fallback")
	}
}

func TestValidatorInAnyHeartbeatSetAllowsCoreFallbackInStrictMode(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"

	n := &Node{
		ID:         "F",
		Blockchain: &Blockchain{},
	}

	if !n.validatorInAnyHeartbeatSet("A", 0, 0, 0, 0) {
		t.Fatalf("expected core validator heartbeat to be accepted via strict-core fallback")
	}
	if n.validatorInAnyHeartbeatSet("Z", 0, 0, 0, 0) {
		t.Fatalf("did not expect non-core heartbeat to pass strict-core fallback")
	}
}

func TestReconcileFrozenValidatorSetHashUsesAdjacentHeightCandidate(t *testing.T) {
	expected := ValidatorSetHash([]string{"A", "B", "C", "D", "F"})
	stale := ValidatorSetHash([]string{"A", "B", "C"})

	n := &Node{
		frozenValidatorsByHeight: map[uint64][]string{
			471: {"A", "B", "C"},
			472: {"A", "B", "C", "D", "F"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			471: stale,
			472: expected,
		},
	}

	if !n.reconcileFrozenValidatorSetHash(471, expected) {
		t.Fatalf("expected reconciliation to succeed using adjacent height candidate")
	}
	gotHash, ok := n.frozenValidatorSetHash(471)
	if !ok || !strings.EqualFold(strings.TrimSpace(gotHash), expected) {
		t.Fatalf("expected reconciled hash=%q got=%q", expected, gotHash)
	}
}

func TestAutohealSafetyAllowsMutationHappyPath(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorMinStake = 100
	setValidatorBannedValidators(nil)

	target := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()
	n := &Node{
		ID: "A",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100, BlockHash: "h100", ValidatorSetHash: target, Signatures: []string{"A", "B", "C", "D"}}},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"B": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"C": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"D": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			100: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			100: target,
		},
		Ledger:              NewLedger(),
		coreRegistryEntries: map[string]CoreRegistryEntry{},
	}

	n.Ledger.Stakes[stakeKey("wallet-a", "A")] = StakeLock{ValidatorID: "A", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-b", "B")] = StakeLock{ValidatorID: "B", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-c", "C")] = StakeLock{ValidatorID: "C", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-d", "D")] = StakeLock{ValidatorID: "D", Amount: 150, LockedUntil: 200}

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
	})

	pubA := testDeterministicPub(1)
	pubB := testDeterministicPub(2)
	pubC := testDeterministicPub(3)
	pubD := testDeterministicPub(4)

	validatorPubKeysMu.Lock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": pubA,
		"B": pubB,
		"C": pubC,
		"D": pubD,
	}
	validatorPubKeysMu.Unlock()

	n.coreRegistryEntries = map[string]CoreRegistryEntry{
		"A": {ID: "A", RequiredKeyFingerprint: validatorKeyFingerprint(pubA)},
		"B": {ID: "B", RequiredKeyFingerprint: validatorKeyFingerprint(pubB)},
		"C": {ID: "C", RequiredKeyFingerprint: validatorKeyFingerprint(pubC)},
		"D": {ID: "D", RequiredKeyFingerprint: validatorKeyFingerprint(pubD)},
	}

	ok, reason := n.autohealSafetyAllowsMutation(100, target)
	if !ok {
		t.Fatalf("expected safety guard pass, got reason=%q", reason)
	}
}

func TestAutohealSafetyRejectsMissingStakeProof(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorCoreStakeExempt = true
	ValidatorMinStake = 100
	setValidatorBannedValidators(nil)

	target := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()
	n := &Node{
		ID: "A",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100, BlockHash: "h100", ValidatorSetHash: target, Signatures: []string{"A", "B", "C", "D"}}},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"B": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"C": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"D": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			100: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			100: target,
		},
		Ledger: NewLedger(),
	}

	n.Ledger.Stakes[stakeKey("wallet-a", "A")] = StakeLock{ValidatorID: "A", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-b", "B")] = StakeLock{ValidatorID: "B", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-c", "C")] = StakeLock{ValidatorID: "C", Amount: 150, LockedUntil: 200}
	// D intentionally missing stake proof

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
	})

	validatorPubKeysMu.Lock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": testDeterministicPub(1),
		"B": testDeterministicPub(2),
		"C": testDeterministicPub(3),
		"D": testDeterministicPub(4),
	}
	validatorPubKeysMu.Unlock()

	ok, reason := n.autohealSafetyAllowsMutation(100, target)
	if ok {
		t.Fatalf("expected safety guard to reject missing stake proof")
	}
	if !strings.Contains(reason, "missing_stake_proof_D") {
		t.Fatalf("expected missing stake proof reason, got=%q", reason)
	}
}

func TestAutohealSafetyRejectsMissingStakeProofForPostBootstrapFormerCoreEvenWhenExempt(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorCoreStakeExempt = true
	ValidatorMinStake = 100
	setValidatorBannedValidators(nil)

	target := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()
	n := &Node{
		ID: "A",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100, BlockHash: "h100", ValidatorSetHash: target, Signatures: []string{"A", "B", "C", "D"}}},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"B": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"C": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"D": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			100: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			100: target,
		},
		Ledger: NewLedger(),
	}

	n.Ledger.Stakes[stakeKey("wallet-b", "B")] = StakeLock{ValidatorID: "B", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-c", "C")] = StakeLock{ValidatorID: "C", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-d", "D")] = StakeLock{ValidatorID: "D", Amount: 150, LockedUntil: 200}
	// A intentionally missing stake proof. Post-bootstrap repair no longer grants
	// any core-based exemption.

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
	})

	validatorPubKeysMu.Lock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": testDeterministicPub(1),
		"B": testDeterministicPub(2),
		"C": testDeterministicPub(3),
		"D": testDeterministicPub(4),
	}
	validatorPubKeysMu.Unlock()

	ok, reason := n.autohealSafetyAllowsMutation(100, target)
	if ok {
		t.Fatalf("expected safety guard rejection for missing stake proof")
	}
	if !strings.Contains(reason, "missing_stake_proof_A") {
		t.Fatalf("expected missing stake proof reason, got=%q", reason)
	}
}

func TestAutohealSafetyRejectsMissingStakeProofForCoreWithoutExempt(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorLivenessHeartbeatTTLSeconds = 30
	ValidatorLivenessGraceSeconds = 5
	ValidatorAllowIdentityRotationOnExistingChain = false
	ValidatorCoreStakeExempt = false
	ValidatorMinStake = 100
	setValidatorBannedValidators(nil)

	target := ValidatorSetHash([]string{"A", "B", "C", "D"})
	now := time.Now()
	n := &Node{
		ID: "A",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100, BlockHash: "h100", ValidatorSetHash: target, Signatures: []string{"A", "B", "C", "D"}}},
		},
		validatorStatus: map[string]*ValidatorStatus{
			"B": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"C": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
			"D": {LastSeen: now, ReportedHeight: 100, FinalizedHeight: 100, ValidatorSetHash: target},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			100: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			100: target,
		},
		Ledger: NewLedger(),
	}

	n.Ledger.Stakes[stakeKey("wallet-b", "B")] = StakeLock{ValidatorID: "B", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-c", "C")] = StakeLock{ValidatorID: "C", Amount: 150, LockedUntil: 200}
	n.Ledger.Stakes[stakeKey("wallet-d", "D")] = StakeLock{ValidatorID: "D", Amount: 150, LockedUntil: 200}
	// A intentionally missing stake proof and should fail regardless of the
	// legacy core exemption toggle on an existing chain.

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"B": {ID: "B", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"C": {ID: "C", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
		"D": {ID: "D", Stake: 150, Status: ValidatorActive, JoinHeight: 1},
	})

	validatorPubKeysMu.Lock()
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": testDeterministicPub(1),
		"B": testDeterministicPub(2),
		"C": testDeterministicPub(3),
		"D": testDeterministicPub(4),
	}
	validatorPubKeysMu.Unlock()

	ok, reason := n.autohealSafetyAllowsMutation(100, target)
	if ok {
		t.Fatalf("expected safety guard rejection for core missing stake proof with exemption disabled")
	}
	if !strings.Contains(reason, "missing_stake_proof_A") {
		t.Fatalf("expected missing stake proof reason for core validator A, got=%q", reason)
	}
}

func TestAutohealSafetyRejectsIdentityRotationBypass(t *testing.T) {
	defer withAutohealHardeningGlobals(t)()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	ValidatorSetAutohealMode = "strict_core_quorum"
	ValidatorAllowIdentityRotationOnExistingChain = true

	target := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n := &Node{}
	ok, reason := n.autohealSafetyAllowsMutation(100, target)
	if ok {
		t.Fatalf("expected safety guard rejection when identity rotation bypass is enabled")
	}
	if reason != "identity_rotation_bypass_enabled" {
		t.Fatalf("expected identity bypass reason, got=%q", reason)
	}
}
