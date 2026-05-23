package main

import (
	"testing"
	"time"
)

func makeFreshLateJoinTestNode() *Node {
	return &Node{
		ID:                "F",
		Role:              "validator",
		Blockchain:        &Blockchain{},
		GenesisValidators: []string{"A", "B", "C", "D"},
		validatorStatus:   make(map[string]*ValidatorStatus),
		peerToValidator:   make(map[string]string),
		peerRole:          make(map[string]string),
		peerSetHash:       make(map[string]string),
		peerHashMatch:     make(map[string]bool),
		peerAckHeight:     make(map[string]uint64),
		validatorSuspect:  make(map[string]time.Time),
	}
}

func TestFreshLateJoinExpectedSourcesUsePendingAuthorityState(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeFreshLateJoinTestNode()
	n.commitMu.Lock()
	n.finalizedHeight = 406
	n.commitMu.Unlock()
	n.noteLateJoinAuthoritySample(406, "04f4a87cfeedbeef")

	if got, source := n.expectedValidatorSetHashWithSource(406); got != "" || source != "chain_sample_pending_snapshot" {
		t.Fatalf("unexpected pending validator-set source: got_hash=%q source=%q", got, source)
	}
	if got, source := n.expectedValidatorRegistryHashWithSource(406); got != "" || source != "chain_sample_pending_snapshot" {
		t.Fatalf("unexpected pending validator-registry source: got_hash=%q source=%q", got, source)
	}
}

func TestFreshLateJoinTrackingIgnoresObservedFinalizedHeight(t *testing.T) {
	n := makeFreshLateJoinTestNode()
	n.commitMu.Lock()
	n.finalizedHeight = 622
	n.commitMu.Unlock()

	if got := n.localCommittedHistoryHeight(); got != 0 {
		t.Fatalf("expected no local committed history, got=%d", got)
	}
	if !n.shouldTrackLateJoinAuthority() {
		t.Fatalf("fresh late join must keep tracking authority while only observed finalized height is non-zero")
	}
}

func TestGenesisObservedHashDoesNotForceLateJoinSnapshotPending(t *testing.T) {
	n := makeFreshLateJoinTestNode()
	now := time.Now()

	n.validatorMu.Lock()
	n.validatorStatus["B"] = &ValidatorStatus{
		ID:               "B",
		LastSeen:         now,
		ValidatorSetHash: "04f4a87cfeedbeef",
		FinalizedHeight:  0,
		ReportedHeight:   0,
	}
	n.validatorStatus["C"] = &ValidatorStatus{
		ID:               "C",
		LastSeen:         now,
		ValidatorSetHash: "04f4a87cfeedbeef",
		FinalizedHeight:  0,
		ReportedHeight:   0,
	}
	n.validatorMu.Unlock()

	n.maybeSyncToBestObservedHeight("startup")

	if lateJoin := n.lateJoinAuthoritySnapshot(); lateJoin.Active {
		t.Fatalf("genesis observed hash must not trigger late-join pending authority: %+v", lateJoin)
	}
	if ok, reason := n.startupValidatorSetSelfCheck(); !ok || reason != "ready" {
		t.Fatalf("unexpected startup gate status: ok=%t reason=%q", ok, reason)
	}
}

func TestPromoteLateJoinAuthorityFromSnapshotPromotesExpectedSourceAndStatus(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeFreshLateJoinTestNode()
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
	}
	snap := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                405,
		BlockHash:             "block-405",
		StateRoot:             "state-405",
		Ledger:                NewLedger(),
		LedgerHash:            HashLedger(NewLedger()),
		GenesisHash:           GenesisHash,
		Validators:            map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:      ValidatorSetHash([]string{"A", "B", "C", "D"}),
		ValidatorRegistry:     registry,
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
		SnapshotHash:          "snapshot-405",
		ActivationHeight:      406,
	}
	populateSnapshotDerivedFields(snap)

	n.promoteLateJoinAuthorityFromSnapshot(snap)

	if got, source := n.expectedValidatorSetHashWithSource(406); got != snap.ValidatorSetHash || source != "snapshot_anchor" {
		t.Fatalf("unexpected promoted validator-set source: got_hash=%q source=%q", got, source)
	}
	if got, source := n.expectedValidatorRegistryHashWithSource(406); got != snap.ValidatorRegistryHash || source != "snapshot_anchor" {
		t.Fatalf("unexpected promoted validator-registry source: got_hash=%q source=%q", got, source)
	}

	status := n.runtimeStatusSnapshot()
	if status.ExpectedVsetSource != "snapshot_anchor" {
		t.Fatalf("unexpected runtime expected source after promotion: got=%q", status.ExpectedVsetSource)
	}
	if status.ValidatorAuthoritySource != "snapshot_anchor" {
		t.Fatalf("unexpected runtime authority source after promotion: got=%q", status.ValidatorAuthoritySource)
	}
	if status.ExpectedVsetHash != snap.ValidatorSetHash {
		t.Fatalf("unexpected runtime expected hash after promotion: got=%q want=%q", status.ExpectedVsetHash, snap.ValidatorSetHash)
	}
}

func TestApplyPeerInfoFreshLateJoinBootstrapSupersededRecordsPendingAuthority(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeFreshLateJoinTestNode()
	hello := PeerHello{
		ChainID:          ChainID,
		GenesisHash:      GenesisHash,
		Role:             "validator",
		ValidatorID:      "A",
		ValidatorSetHash: "04f4a87cfeedbeef",
		Height:           405,
	}

	n.applyPeerInfo("peer-a", hello)

	lateJoin := n.lateJoinAuthoritySnapshot()
	if !lateJoin.Active || lateJoin.Authoritative {
		t.Fatalf("expected pending late-join authority after bootstrap supersede, got=%+v", lateJoin)
	}
	if lateJoin.Source != "chain_sample_pending_snapshot" {
		t.Fatalf("unexpected late-join source: got=%q", lateJoin.Source)
	}
	if lateJoin.Height != 406 {
		t.Fatalf("unexpected late-join height: got=%d want=406", lateJoin.Height)
	}
	if lateJoin.ValidatorSetHash != hello.ValidatorSetHash {
		t.Fatalf("unexpected late-join hash: got=%q want=%q", lateJoin.ValidatorSetHash, hello.ValidatorSetHash)
	}
}

func TestFreshLateJoinTryRepairDefersUntilSnapshotAnchor(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeFreshLateJoinTestNode()
	n.noteLateJoinAuthoritySample(406, "04f4a87cfeedbeef")

	if repaired := n.tryRepairValidatorSetHash(406, "deadbeef"); repaired {
		t.Fatalf("expected repair to be deferred while late-join authority is pending")
	}
	_, reason, mismatchHeight, _, got, _ := n.validatorAutohealStatusSnapshot()
	if reason != "late_join_snapshot_anchor_pending" {
		t.Fatalf("unexpected autoheal wait reason: got=%q", reason)
	}
	if mismatchHeight != 406 {
		t.Fatalf("unexpected mismatch height: got=%d want=406", mismatchHeight)
	}
	if got != "deadbeef" {
		t.Fatalf("unexpected recorded mismatch hash: got=%q", got)
	}
}

func TestRuntimeAdvertisedValidatorSetHashPrefersFrozenHashOnExistingChain(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	bc := Blockchain{
		Blocks: []Block{{ID: 405}},
	}
	n := &Node{
		Blockchain: &bc,
		frozenValidatorsByHeight: map[uint64][]string{
			406: {"A", "B", "C", "D"},
		},
		frozenValidatorHashByHeight: map[uint64]string{
			406: "34a93d19feedbeef",
		},
		validatorStatus: make(map[string]*ValidatorStatus),
	}

	hash, source := n.runtimeAdvertisedValidatorSetHash(406)
	if hash != "34a93d19feedbeef" {
		t.Fatalf("unexpected advertised hash: got=%q want=%q", hash, "34a93d19feedbeef")
	}
	if source != "frozen" {
		t.Fatalf("unexpected advertised source: got=%q want=frozen", source)
	}
}
