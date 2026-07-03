package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withLivenessSettings(t *testing.T, ttlSeconds uint64, graceSeconds uint64, driftBlocks uint64) {
	t.Helper()
	oldTTL := ValidatorLivenessHeartbeatTTLSeconds
	oldGrace := ValidatorLivenessGraceSeconds
	oldDrift := ValidatorLivenessMaxHeightDriftBlocks
	ValidatorLivenessHeartbeatTTLSeconds = ttlSeconds
	ValidatorLivenessGraceSeconds = graceSeconds
	ValidatorLivenessMaxHeightDriftBlocks = driftBlocks
	t.Cleanup(func() {
		ValidatorLivenessHeartbeatTTLSeconds = oldTTL
		ValidatorLivenessGraceSeconds = oldGrace
		ValidatorLivenessMaxHeightDriftBlocks = oldDrift
	})
}

func TestValidatorLivenessWithinGraceAndDriftRemainsLive(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		GenesisValidators:     []string{"F"},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	st := &ValidatorStatus{
		LastSeen:            now.Add(-30 * time.Second),
		Active:              true,
		Enabled:             true,
		ConsensusReadyKnown: true,
		ReportedHeight:      124,
		FinalizedHeight:     124,
		ExecEpoch:           129,
		ValidatorSetHeight:  129,
	}
	n.validatorStatus["F"] = st

	live := n.isValidatorLiveForConsensusLocked("F", st, now, 129)
	if !live {
		t.Fatalf("validator should remain live when heartbeat is fresh and drift is within limit")
	}
}

func TestValidatorLivenessBeyondGraceBecomesOffline(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		GenesisValidators:     []string{"F"},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	st := &ValidatorStatus{
		LastSeen:            now.Add(-36 * time.Second),
		Active:              true,
		Enabled:             true,
		ConsensusReadyKnown: true,
		ReportedHeight:      100,
		FinalizedHeight:     100,
		ExecEpoch:           100,
		ValidatorSetHeight:  100,
	}
	n.validatorStatus["F"] = st

	live := n.isValidatorLiveForConsensusLocked("F", st, now, 100)
	if live {
		t.Fatalf("validator should become offline once ttl+grace is exceeded")
	}
}

func TestValidatorLivenessOutOfSetIsOffline(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	st := &ValidatorStatus{
		LastSeen:            now.Add(-5 * time.Second),
		Active:              true,
		Enabled:             true,
		ConsensusReadyKnown: true,
		ReportedHeight:      50,
		FinalizedHeight:     50,
		ExecEpoch:           50,
	}
	n.validatorStatus["F"] = st

	live := n.isValidatorLiveForConsensusLocked("F", st, now, 50)
	if live {
		t.Fatalf("validator must be offline when not present in any heartbeat set")
	}
}

func TestValidatorLivenessCountsConsensusWaitingPeerDuringCommitStall(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		GenesisValidators:     []string{"F"},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
		peerToValidator:       map[string]string{"peerF": "F"},
		connectedPeers:        map[string]bool{"peerF": true},
	}
	n.lastCommitAt = now.Add(-20 * time.Second)
	st := &ValidatorStatus{
		LastSeen:            now.Add(-5 * time.Second),
		Active:              true,
		Enabled:             false,
		ConsensusReadyKnown: true,
		ReportedHeight:      100,
		FinalizedHeight:     100,
		ExecEpoch:           101,
		ValidatorSetHeight:  101,
	}
	n.validatorStatus["F"] = st

	eval := n.evaluateValidatorLivenessLocked("F", st, now, 100)
	if !eval.LiveStrict {
		t.Fatalf("fresh quorum-waiting validator should count live during commit stall, reason=%s", eval.FailReason)
	}
}

func TestValidatorLivenessDeepLagExcluded(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		GenesisValidators:     []string{"F"},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	st := &ValidatorStatus{
		LastSeen:            now.Add(-5 * time.Second),
		Active:              true,
		Enabled:             true,
		ConsensusReadyKnown: true,
		ReportedHeight:      99,
		FinalizedHeight:     99,
		ExecEpoch:           129,
		ValidatorSetHeight:  129,
	}
	n.validatorStatus["F"] = st

	live := n.isValidatorLiveForConsensusLocked("F", st, now, 129)
	if live {
		t.Fatalf("validator should be offline when lag exceeds strict drift limit")
	}
}

func TestValidatorLivenessAheadLagExcluded(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		GenesisValidators:     []string{"F"},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	st := &ValidatorStatus{
		LastSeen:            now.Add(-5 * time.Second),
		Active:              true,
		Enabled:             true,
		ConsensusReadyKnown: true,
		ReportedHeight:      129,
		FinalizedHeight:     129,
		ExecEpoch:           99,
		ValidatorSetHeight:  99,
	}
	n.validatorStatus["F"] = st

	live := n.isValidatorLiveForConsensusLocked("F", st, now, 99)
	if live {
		t.Fatalf("validator should be offline when ahead-lag exceeds strict drift limit")
	}
}

func TestRuntimeStatusLiteUsesObservedNetworkHeight(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	wasStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(wasStarted)
	})

	validators := []string{"A", "B", "C", "D"}
	setHash := ValidatorSetHash(validators)
	bc := &Blockchain{Blocks: []Block{{
		ID:                     100,
		Signatures:             validators,
		ValidatorSetHash:       setHash,
		NextValidatorSetHash:   setHash,
		NextValidatorSetHeight: 101,
	}}}
	n := &Node{
		ID:                    "D",
		Role:                  "validator",
		Blockchain:            bc,
		Consensus:             &ConsensusState{},
		GenesisValidators:     validators,
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	n.validatorStatus["B"] = &ValidatorStatus{
		LastSeen:        time.Now(),
		Active:          true,
		Enabled:         true,
		ReportedHeight:  116,
		FinalizedHeight: 116,
	}

	status := n.runtimeStatusSnapshotLite()
	if !status.Syncing || status.SyncComplete || status.Ready || status.ExecutionReady {
		t.Fatalf("expected lite status to gate lagging node, status=%+v", status)
	}
	if status.SyncTarget != 116 || status.NetworkBestHeight != 116 || status.NetworkLagBlocks != 16 {
		t.Fatalf("unexpected observed target/lag: target=%d best=%d lag=%d", status.SyncTarget, status.NetworkBestHeight, status.NetworkLagBlocks)
	}
	if status.WaitReason != "syncing" || status.ValidatorState != "syncing" {
		t.Fatalf("expected syncing wait/state, got wait=%q state=%q", status.WaitReason, status.ValidatorState)
	}
}

func TestRuntimeStatusLiteDoesNotChaseMinorityTipWithQuorumAtLocalHeight(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	wasStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(wasStarted)
	})

	validators := []string{"A", "B", "C", "D"}
	setHash := ValidatorSetHash(validators)
	bc := &Blockchain{Blocks: []Block{{
		ID:                     100,
		Signatures:             validators,
		ValidatorSetHash:       setHash,
		NextValidatorSetHash:   setHash,
		NextValidatorSetHeight: 101,
	}}}
	n := &Node{
		ID:                    "D",
		Role:                  "validator",
		Blockchain:            bc,
		Consensus:             &ConsensusState{Syncing: true, SyncTarget: 100},
		GenesisValidators:     validators,
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	now := time.Now()
	for _, sample := range []struct {
		id     string
		height uint64
	}{
		{id: "A", height: 100},
		{id: "B", height: 100},
		{id: "C", height: 101},
	} {
		n.validatorStatus[sample.id] = &ValidatorStatus{
			LastSeen:        now,
			Active:          true,
			Enabled:         true,
			ReportedHeight:  sample.height,
			FinalizedHeight: sample.height,
		}
	}

	status := n.runtimeStatusSnapshotLite()
	if status.Syncing {
		t.Fatalf("minority tip must not keep validator in sync mode, status=%+v", status)
	}
	if status.SyncTarget != 100 {
		t.Fatalf("minority tip must not extend validator sync target, got=%d", status.SyncTarget)
	}
	if status.NetworkBestHeight != 101 || status.NetworkBestHeightVotes != 1 {
		t.Fatalf("minority tip should remain visible as telemetry, best=%d votes=%d",
			status.NetworkBestHeight, status.NetworkBestHeightVotes)
	}
}

func TestRuntimeStatusLiteDoesNotMarkInactiveValidatorReady(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	wasStarted := consensusStarted.Load()
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false
	consensusStarted.Store(true)
	t.Cleanup(func() {
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
		consensusStarted.Store(wasStarted)
	})

	validators := []string{"A", "B", "C", "D"}
	setHash := ValidatorSetHash(validators)
	bc := &Blockchain{Blocks: []Block{{
		ID:                     100,
		Signatures:             validators,
		ValidatorSetHash:       setHash,
		NextValidatorSetHash:   setHash,
		NextValidatorSetHeight: 101,
	}}}
	n := &Node{
		ID:                    "F",
		Role:                  "validator",
		ValidatorKey:          strictActivationTestValidatorKey(91, "F"),
		Blockchain:            bc,
		Consensus:             &ConsensusState{},
		GenesisValidators:     validators,
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	now := time.Now()
	for _, id := range []string{"A", "B", "C"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      100,
			FinalizedHeight:     100,
			ExecEpoch:           101,
			ValidatorSetHeight:  101,
		}
	}

	status := n.runtimeStatusSnapshotLite()
	if status.Ready || status.ConsensusReady || status.VoteEnabled || status.ProposeEnabled {
		t.Fatalf("inactive validator must not be marked ready by lite status: %+v", status)
	}
	if status.WaitReason != "activation_pending_not_in_frozen_set" || status.ValidatorState != "activating" {
		t.Fatalf("expected activation wait in lite status, got wait=%q state=%q", status.WaitReason, status.ValidatorState)
	}
}

func TestRuntimeStatusDoesNotSelfDisableOnObservedQuorumCollapse(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	wasStarted := consensusStarted.Load()
	consensusStarted.Store(true)
	t.Cleanup(func() {
		consensusStarted.Store(wasStarted)
	})

	n := makeStrictActivationNode(8)
	setHash := ValidatorSetHash([]string{"A", "B", "C", "D"})
	n.setValidatorStartupCheckStatus(true, n.Blockchain.Height()+1, setHash, setHash, "startup_validator_set_ok")
	for _, st := range n.validatorStatus {
		st.Enabled = false
		st.ConsensusReadyKnown = true
	}

	lite := n.runtimeStatusSnapshotLite()
	if lite.RequiredQuorum <= 0 || lite.LiveValidators >= lite.RequiredQuorum {
		t.Fatalf("test setup must simulate observed quorum collapse, live=%d required=%d", lite.LiveValidators, lite.RequiredQuorum)
	}
	if !lite.ConsensusReady || !lite.Ready || lite.WaitReason != "ready" {
		t.Fatalf("lite status should keep self readiness independent from observed quorum: %+v", lite)
	}

	full := n.runtimeStatusSnapshot()
	if full.RequiredQuorum <= 0 || full.LiveValidators >= full.RequiredQuorum {
		t.Fatalf("test setup must simulate full observed quorum collapse, live=%d required=%d", full.LiveValidators, full.RequiredQuorum)
	}
	if !full.ConsensusReady || !full.Ready || full.WaitReason != "ready" {
		t.Fatalf("full status should keep self readiness independent from observed quorum: %+v", full)
	}
}

func TestValidatorLivenessRequiresConsensusReady(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	now := time.Now()
	bc := NewBlockchain()
	n := &Node{
		Blockchain:            &bc,
		GenesisValidators:     []string{"F"},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
	}
	st := &ValidatorStatus{
		LastSeen:            now.Add(-2 * time.Second),
		Active:              true,
		Enabled:             false,
		ConsensusReadyKnown: true,
		ReportedHeight:      100,
		FinalizedHeight:     100,
		ExecEpoch:           101,
		ValidatorSetHeight:  101,
	}
	n.validatorStatus["F"] = st

	if n.isValidatorLiveForConsensusLocked("F", st, now, 100) {
		t.Fatalf("fresh heartbeat must not count when consensus_ready=false")
	}
	if live := n.countRecentHeartbeatValidatorsInSet(101, validatorLivenessHeartbeatTTL()); live != 0 {
		t.Fatalf("consensus-ready live count = %d, want 0", live)
	}
}

func TestValidatorHeartbeatBroadcastMinInterval(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)
	if got := validatorHeartbeatBroadcastMinInterval(true); got != 12500*time.Millisecond {
		t.Fatalf("expected committee interval 12.5s for ttl=25s, got %s", got)
	}
	if got := validatorHeartbeatBroadcastMinInterval(false); got != 2*time.Minute {
		t.Fatalf("expected non-committee interval 2m, got %s", got)
	}

	withLivenessSettings(t, 8, 10, 8)
	if got := validatorHeartbeatBroadcastMinInterval(true); got != 10*time.Second {
		t.Fatalf("expected minimum clamp 10s, got %s", got)
	}

	withLivenessSettings(t, 80, 10, 8)
	if got := validatorHeartbeatBroadcastMinInterval(true); got != 20*time.Second {
		t.Fatalf("expected maximum clamp 20s, got %s", got)
	}
}

func TestSanitizeLivenessMaxHeightDriftBlocks(t *testing.T) {
	if got := sanitizeLivenessMaxHeightDriftBlocks(0); got != 1 {
		t.Fatalf("expected clamp to min 1, got %d", got)
	}
	if got := sanitizeLivenessMaxHeightDriftBlocks(8); got != 8 {
		t.Fatalf("expected unchanged value 8, got %d", got)
	}
	if got := sanitizeLivenessMaxHeightDriftBlocks(5000); got != 1024 {
		t.Fatalf("expected clamp to max 1024, got %d", got)
	}
}

func TestHandleValidatorsIncludesStrictDiagnosticFields(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	bc := NewBlockchain()
	now := time.Now()
	n := &Node{
		Blockchain:        &bc,
		GenesisValidators: []string{"F"},
		validatorStatus: map[string]*ValidatorStatus{
			"F": {
				LastSeen:           now.Add(-5 * time.Second),
				Active:             true,
				ReportedHeight:     1,
				FinalizedHeight:    1,
				ExecEpoch:          1,
				ValidatorSetHeight: 1,
			},
		},
		validatorOfflineSince: make(map[string]time.Time),
		validatorRejoin:       make(map[string]ValidatorRejoinState),
	}
	srv := &Server{Node: n}
	req := httptest.NewRequest(http.MethodGet, "/v1/validators", nil)
	rec := httptest.NewRecorder()
	srv.handleValidators(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		ValidatorLiveness map[string]map[string]any `json:"validator_liveness"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	entry, ok := body.ValidatorLiveness["F"]
	if !ok {
		t.Fatalf("missing validator_liveness entry for F")
	}
	for _, key := range []string{
		"last_seen_unix",
		"age_seconds",
		"in_any_heartbeat_set",
		"height_lag_blocks",
		"live_strict",
		"fail_reason",
	} {
		if _, exists := entry[key]; !exists {
			t.Fatalf("missing diagnostic key %q in validator_liveness entry", key)
		}
	}
}

func TestHandleValidatorsUsesFinalizedActivityForStableOnlineInactiveView(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)
	oldInactive := ValidatorInactiveBlocks
	oldV2 := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		ValidatorInactiveBlocks = oldInactive
		ValidatorSetCommitmentV2Height = oldV2
	})
	ValidatorInactiveBlocks = 4
	ValidatorSetCommitmentV2Height = 1

	validators := []string{"A", "B", "C", "D"}
	setHash := ValidatorSetHash(validators)
	bc := NewBlockchain()
	proposers := []string{"A", "B", "C", "A", "B", "C", "A", "B"}
	for i, proposer := range proposers {
		height := uint64(i + 1)
		bc.AddBlock(Block{
			ID:                   height,
			Proposer:             proposer,
			Signatures:           append([]string{}, validators...),
			ValidatorSetHash:     setHash,
			NextValidatorSetHash: setHash,
		})
	}

	now := time.Now()
	n := &Node{
		Blockchain:        &bc,
		GenesisValidators: append([]string{}, validators...),
		validatorStatus: map[string]*ValidatorStatus{
			"A": {
				LastSeen:           now,
				Active:             true,
				ReportedHeight:     8,
				FinalizedHeight:    8,
				ExecEpoch:          8,
				ValidatorSetHeight: 8,
			},
		},
		validatorOfflineSince: make(map[string]time.Time),
		validatorRejoin:       make(map[string]ValidatorRejoinState),
	}
	srv := &Server{Node: n}
	req := httptest.NewRequest(http.MethodGet, "/validators?height=9", nil)
	rec := httptest.NewRecorder()
	srv.handleValidators(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Online        []string `json:"online_validators"`
		Inactive      []string `json:"inactive_validators"`
		LocalOnline   []string `json:"local_online_validators"`
		LocalInactive []string `json:"local_inactive_validators"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got, want := strings.Join(body.Online, ","), "A,B,C"; got != want {
		t.Fatalf("stable online validators mismatch: got=%s want=%s", got, want)
	}
	if got, want := strings.Join(body.Inactive, ","), "D"; got != want {
		t.Fatalf("stable inactive validators mismatch: got=%s want=%s", got, want)
	}
	if got, want := strings.Join(body.LocalOnline, ","), "A"; got != want {
		t.Fatalf("local online validators mismatch: got=%s want=%s", got, want)
	}
	if got, want := strings.Join(body.LocalInactive, ","), "B,C,D"; got != want {
		t.Fatalf("local inactive validators mismatch: got=%s want=%s", got, want)
	}
}

func TestHandleValidatorsDefaultUsesFinalizedHeightNotFutureSet(t *testing.T) {
	withLivenessSettings(t, 25, 10, 8)

	bc := NewBlockchain()
	for height := uint64(1); height <= 10; height++ {
		bc.AddBlock(Block{
			ID:                   height,
			Proposer:             "A",
			ValidatorSetHash:     ValidatorSetHash([]string{"A", "B", "C", "D"}),
			NextValidatorSetHash: ValidatorSetHash([]string{"A", "B", "C", "D"}),
		})
	}

	n := &Node{
		Blockchain:        &bc,
		GenesisValidators: []string{"A", "B", "C", "D"},
		epochValidators: map[uint64][]string{
			10: []string{"A", "B", "C", "D"},
			11: []string{"A", "B", "C", "D", "G"},
		},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
		validatorRejoin:       make(map[string]ValidatorRejoinState),
		pendingValidators: map[string]uint64{
			"G": 10,
		},
	}
	srv := &Server{Node: n}
	req := httptest.NewRequest(http.MethodGet, "/validators", nil)
	rec := httptest.NewRecorder()
	srv.handleValidators(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Height     int      `json:"height"`
		Validators []string `json:"validators"`
		Inactive   []string `json:"inactive_validators"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Height != 10 {
		t.Fatalf("default validator height should be finalized/current height, got %d", body.Height)
	}
	if strings.Contains(strings.Join(body.Validators, ","), "G") {
		t.Fatalf("pending future validator leaked into default validators: %v", body.Validators)
	}
	if strings.Contains(strings.Join(body.Inactive, ","), "G") {
		t.Fatalf("pending future validator leaked into inactive validators: %v", body.Inactive)
	}
}

func TestHandleValidatorsDefaultCapsRemoteFinalizedAtLocalTip(t *testing.T) {
	bc := NewBlockchain()
	for height := uint64(1); height <= 4; height++ {
		bc.AddBlock(Block{
			ID:                   height,
			Proposer:             "A",
			ValidatorSetHash:     ValidatorSetHash([]string{"A", "B", "C", "D"}),
			NextValidatorSetHash: ValidatorSetHash([]string{"A", "B", "C", "D"}),
		})
	}

	n := &Node{
		Blockchain:        &bc,
		GenesisValidators: []string{"A", "B", "C", "D"},
		epochValidators: map[uint64][]string{
			4:  []string{"A", "B", "C", "D"},
			10: []string{"A", "B", "C", "D", "G"},
		},
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: make(map[string]time.Time),
		validatorRejoin:       make(map[string]ValidatorRejoinState),
	}
	n.commitMu.Lock()
	n.finalizedHeight = 10
	n.committedHeight = 10
	n.commitMu.Unlock()

	srv := &Server{Node: n}
	req := httptest.NewRequest(http.MethodGet, "/validators", nil)
	rec := httptest.NewRecorder()
	srv.handleValidators(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Height     int      `json:"height"`
		Validators []string `json:"validators"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Height != 4 {
		t.Fatalf("default validator height should cap remote finalized at local tip, got %d", body.Height)
	}
	if strings.Contains(strings.Join(body.Validators, ","), "G") {
		t.Fatalf("future validator leaked into syncing node default validators: %v", body.Validators)
	}
}
