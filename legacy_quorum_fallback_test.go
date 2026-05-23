package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func withLegacyQuorumFallbackGlobals(t *testing.T) func() {
	t.Helper()

	oldCommitmentV2Height := ValidatorSetCommitmentV2Height
	oldHashV3Height := ValidatorSetHashV3Height
	oldRequireStake := ValidatorRequireStake
	oldRequireWallet := ConfigAuthRequireWallet
	oldTTL := ValidatorLivenessHeartbeatTTLSeconds
	oldGrace := ValidatorLivenessGraceSeconds
	oldDrift := ValidatorLivenessMaxHeightDriftBlocks
	oldConsensusStarted := consensusStarted.Load()

	return func() {
		ValidatorSetCommitmentV2Height = oldCommitmentV2Height
		ValidatorSetHashV3Height = oldHashV3Height
		ValidatorRequireStake = oldRequireStake
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorLivenessHeartbeatTTLSeconds = oldTTL
		ValidatorLivenessGraceSeconds = oldGrace
		ValidatorLivenessMaxHeightDriftBlocks = oldDrift
		consensusStarted.Store(oldConsensusStarted)
	}
}

func newLegacyFrozenQuorumNode(hash string) *Node {
	validators := []string{"A", "B", "C", "D"}
	return &Node{
		ID:                    "A",
		Role:                  "validator",
		Blockchain:            &Blockchain{Blocks: []Block{{ID: 947, ValidatorSetHash: hash}}},
		ValidatorKey:          strictActivationTestValidatorKey(1, "A"),
		GenesisValidators:     append([]string{}, validators...),
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: map[string]time.Time{},
		validatorRejoin:       make(map[string]ValidatorRejoinState),
		frozenValidatorsByHeight: map[uint64][]string{
			947: append([]string{}, validators...),
			948: append([]string{}, validators...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			947: hash,
			948: hash,
		},
	}
}

func legacyFrozenCanonicalHash() string {
	return ValidatorSetHash([]string{"A", "B", "C", "D"})
}

func registerLegacyFrozenHeartbeat(node *Node, id string, height uint64, hash string) {
	node.RegisterValidator(id, height, height, height+1, height+1, hash)
}

func newExistingChainUnresolvedNode() *Node {
	const frozenHash = "04f4a87cfeedbeef"
	validators := []string{"A", "B", "C", "D"}
	return &Node{
		ID:                    "A",
		Role:                  "validator",
		Blockchain:            &Blockchain{Blocks: []Block{{ID: 992, ValidatorSetHash: frozenHash, NextValidatorSetHash: "deadbeef"}}},
		ValidatorKey:          strictActivationTestValidatorKey(1, "A"),
		GenesisValidators:     append([]string{}, validators...),
		validatorStatus:       make(map[string]*ValidatorStatus),
		validatorOfflineSince: map[string]time.Time{},
		validatorRejoin:       make(map[string]ValidatorRejoinState),
		frozenValidatorsByHeight: map[uint64][]string{
			992: append([]string{}, validators...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			992: frozenHash,
		},
	}
}

func registerExistingChainLiveHeartbeat(node *Node, id string) {
	normID := normalizeValidatorID(id)
	node.validatorMu.Lock()
	defer node.validatorMu.Unlock()
	node.validatorStatus[normID] = &ValidatorStatus{
		Height:             992,
		ReportedHeight:     992,
		FinalizedHeight:    992,
		ExecEpoch:          993,
		ValidatorSetHeight: 993,
		LastSeen:           time.Now(),
		Active:             true,
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()
	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out)
}

func TestLegacyFrozenValidatorInAnyHeartbeatSetAtNextHeight(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	n := newLegacyFrozenQuorumNode("04f4a87cfeedbeef")
	if got := n.validatorInAnyHeartbeatSet("A", 947, 947, 948, 948); !got {
		t.Fatalf("expected validator heartbeat set lookup to use frozen next-height authority")
	}
}

func TestLegacyFrozenRegisterValidatorAtNextHeight(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	frozenHash := legacyFrozenCanonicalHash()
	n := newLegacyFrozenQuorumNode(frozenHash)
	registerLegacyFrozenHeartbeat(n, "A", 947, frozenHash)

	st := n.validatorStatus["A"]
	if st == nil {
		t.Fatalf("expected validator status to be recorded")
	}
	if !st.Active {
		t.Fatalf("expected validator to remain active after frozen-set admission")
	}
	if st.ValidatorSetHeight != 948 {
		t.Fatalf("unexpected validator set height: got=%d want=948", st.ValidatorSetHeight)
	}
	if st.FinalizedHeight != 947 {
		t.Fatalf("unexpected finalized height: got=%d want=947", st.FinalizedHeight)
	}
}

func TestLegacyExpectedValidatorSetHashFallsBackToFrozen(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	frozenHash := legacyFrozenCanonicalHash()
	n := newLegacyFrozenQuorumNode(frozenHash)

	got, source := n.expectedValidatorSetHashWithSource(948)
	if got != frozenHash {
		t.Fatalf("unexpected expected validator hash: got=%q want=%q", got, frozenHash)
	}
	if source != "frozen" {
		t.Fatalf("unexpected expected validator source: got=%q want=frozen", source)
	}
}

func TestLegacyConsensusValidatorsForNextHeightFallsBackToFrozen(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	n := newLegacyFrozenQuorumNode(legacyFrozenCanonicalHash())
	got := n.consensusValidatorsForHeight(948)
	if len(got) != 4 {
		t.Fatalf("unexpected validator count: got=%d want=4", len(got))
	}
	if got[0] != "A" || got[1] != "B" || got[2] != "C" || got[3] != "D" {
		t.Fatalf("unexpected validators: got=%v", got)
	}
}

func TestLegacySelfActiveValidatorAtNextHeightUsesFrozenCarryForward(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false

	n := newLegacyFrozenQuorumNode(legacyFrozenCanonicalHash())
	active, reason := n.selfActiveValidatorAt(948)
	if !active {
		t.Fatalf("expected self to remain active on legacy carry-forward set, reason=%q", reason)
	}
	if reason != "active" {
		t.Fatalf("unexpected self-active reason: got=%q want=active", reason)
	}
}

func TestLegacySnapshotEpochValidatorsDoesNotDeadlockOnFrozenAuthority(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	n := newLegacyFrozenQuorumNode(legacyFrozenCanonicalHash())
	done := make(chan struct{})
	go func() {
		n.snapshotEpochValidators(948)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("snapshotEpochValidators deadlocked while resolving legacy frozen authority")
	}

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	got := n.epochValidators[948]
	if len(got) != 4 {
		t.Fatalf("unexpected snapshot validator count: got=%d want=4", len(got))
	}
	if got[0] != "A" || got[1] != "B" || got[2] != "C" || got[3] != "D" {
		t.Fatalf("unexpected snapshot validators: got=%v", got)
	}
}

func TestLegacyChainHashSyncBackfillsContiguousFrozenValidatorList(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	frozenHash := legacyFrozenCanonicalHash()
	n := &Node{
		ID:                "A",
		Role:              "validator",
		Blockchain:        &Blockchain{Blocks: []Block{{ID: 953, ValidatorSetHash: frozenHash}, {ID: 954, ValidatorSetHash: frozenHash}}},
		GenesisValidators: []string{"A", "B", "C", "D"},
		frozenValidatorsByHeight: map[uint64][]string{
			953: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			953: frozenHash,
		},
	}

	applied := n.syncFrozenValidatorSetHashesFromChain()
	if applied == 0 {
		t.Fatalf("expected chain hash sync to apply at least one height")
	}
	if got := canonicalValidatorIDs(n.frozenValidatorsByHeight[954]); len(got) != 4 {
		t.Fatalf("expected carried frozen validators at 954, got=%v", got)
	}
	validators := n.consensusValidatorsForHeight(955)
	if len(validators) != 4 {
		t.Fatalf("expected next-height consensus validators after hash-only backfill, got=%v", validators)
	}
	if hash, source := n.expectedValidatorSetHashWithSource(955); hash != frozenHash || source != "carry_forward" {
		t.Fatalf("unexpected next-height authority after backfill: hash=%q source=%q", hash, source)
	}
}

func TestLegacySyncReadyForConsensusUsesFrozenNextHeightAuthority(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0

	frozenHash := legacyFrozenCanonicalHash()
	n := newLegacyFrozenQuorumNode(frozenHash)
	registerLegacyFrozenHeartbeat(n, "B", 947, frozenHash)
	registerLegacyFrozenHeartbeat(n, "C", 947, frozenHash)
	registerLegacyFrozenHeartbeat(n, "D", 947, frozenHash)

	ready, reason := n.syncReadyForConsensus(948)
	if !ready {
		t.Fatalf("expected consensus readiness with frozen next-height authority, reason=%q", reason)
	}
}

func TestLegacyRuntimeStatusUsesFrozenAuthorityForQuorum(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorSetHashV3Height = 0
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false
	ValidatorLivenessHeartbeatTTLSeconds = 25
	ValidatorLivenessGraceSeconds = 10
	ValidatorLivenessMaxHeightDriftBlocks = 8
	consensusStarted.Store(false)

	frozenHash := legacyFrozenCanonicalHash()
	n := newLegacyFrozenQuorumNode(frozenHash)
	registerLegacyFrozenHeartbeat(n, "B", 947, frozenHash)
	registerLegacyFrozenHeartbeat(n, "C", 947, frozenHash)
	registerLegacyFrozenHeartbeat(n, "D", 947, frozenHash)

	status := n.runtimeStatusSnapshot()
	if status.ExpectedVsetHash != frozenHash {
		t.Fatalf("unexpected expected validator hash: got=%q want=%q", status.ExpectedVsetHash, frozenHash)
	}
	if status.ExpectedVsetSource != "frozen" {
		t.Fatalf("unexpected expected validator source: got=%q want=frozen", status.ExpectedVsetSource)
	}
	if status.ValidatorAuthoritySource != "frozen" {
		t.Fatalf("unexpected authority source: got=%q want=frozen", status.ValidatorAuthoritySource)
	}
	if status.LiveValidators < status.RequiredQuorum {
		t.Fatalf("expected live validators to satisfy quorum: live=%d required=%d", status.LiveValidators, status.RequiredQuorum)
	}
	if status.LiveStrictCount == 0 || status.LiveHeartbeatCount == 0 {
		t.Fatalf("expected non-zero live validator counts, got strict=%d heartbeat=%d", status.LiveStrictCount, status.LiveHeartbeatCount)
	}
	if status.WaitReason == "waiting_quorum_0_of_3" {
		t.Fatalf("unexpected legacy quorum collapse wait reason: %+v", status)
	}
}

func TestStrictParentCommitmentDoesNotFallbackToFrozen(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = 947

	n := newLegacyFrozenQuorumNode(legacyFrozenCanonicalHash())

	got, source := n.expectedValidatorSetHashWithSource(948)
	if got != "" {
		t.Fatalf("expected no frozen fallback on strict parent-commitment path, got=%q", got)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected strict source: got=%q want=chain_parent_commitment", source)
	}
}

func TestLegacyExecutionQuorumTotalDoesNotFallbackToLiveCountOnExistingChain(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	n := newExistingChainUnresolvedNode()
	registerExistingChainLiveHeartbeat(n, "B")
	registerExistingChainLiveHeartbeat(n, "C")
	registerExistingChainLiveHeartbeat(n, "D")

	if live := n.countLiveValidators(); live == 0 {
		t.Fatalf("expected non-zero live validators for unresolved existing-chain test")
	}
	if total := n.executionQuorumTotal(993); total != 0 {
		t.Fatalf("expected unresolved existing-chain quorum to fail closed, got total=%d", total)
	}
}

func TestLegacyRuntimeStatusFailsClosedWhenNextHeightAuthorityUnresolved(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ValidatorRequireStake = false
	ConfigAuthRequireWallet = false
	consensusStarted.Store(true)

	n := newExistingChainUnresolvedNode()
	registerExistingChainLiveHeartbeat(n, "B")
	registerExistingChainLiveHeartbeat(n, "C")
	registerExistingChainLiveHeartbeat(n, "D")

	status := n.runtimeStatusSnapshot()
	if status.Ready || status.ConsensusReady {
		t.Fatalf("expected unresolved next-height authority to fail closed, status=%+v", status)
	}
	if strings.HasPrefix(status.WaitReason, "waiting_quorum_") || status.WaitReason == "ready" {
		t.Fatalf("expected unresolved authority wait reason, got=%q status=%+v", status.WaitReason, status)
	}
	if status.ValidatorAuthoritySource != "none" {
		t.Fatalf("expected no authority source for unresolved next height, got=%q", status.ValidatorAuthoritySource)
	}
	if status.LiveValidators != 0 || status.RequiredQuorum != 0 {
		t.Fatalf("expected no synthetic live-count quorum on unresolved authority, live=%d required=%d", status.LiveValidators, status.RequiredQuorum)
	}
}

func TestLegacySyncReadyForConsensusFailsClosedWithoutNextHeightAuthority(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	n := newExistingChainUnresolvedNode()
	registerExistingChainLiveHeartbeat(n, "B")
	registerExistingChainLiveHeartbeat(n, "C")
	registerExistingChainLiveHeartbeat(n, "D")

	ready, reason := n.syncReadyForConsensus(993)
	if ready {
		t.Fatalf("expected sync readiness to fail closed on unresolved next-height authority")
	}
	if reason != "validator_set_unresolved" {
		t.Fatalf("unexpected readiness reason: got=%q want=validator_set_unresolved", reason)
	}
}

func TestReceiveBlockLogsImmediateCatchUpRejectReason(t *testing.T) {
	defer withLegacyQuorumFallbackGlobals(t)()
	ValidatorSetCommitmentV2Height = ^uint64(0)

	n := newExistingChainUnresolvedNode()
	block := Block{
		ID:               993,
		BlockHash:        "block-993",
		PrevHash:         "block-992",
		ValidatorSetHash: "04f4a87cfeedbeef",
		Proposer:         "B",
	}

	out := captureStdout(t, func() {
		_ = n.ReceiveBlock(block, n.Blockchain)
	})
	if !strings.Contains(out, "[CATCH-UP-REJECT]") {
		t.Fatalf("expected immediate catch-up reject log, got=%q", out)
	}
	if !strings.Contains(out, "reason=validator_set_unresolved") {
		t.Fatalf("expected validator_set_unresolved reject reason, got=%q", out)
	}
}
