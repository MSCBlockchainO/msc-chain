package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
)

func TestCoreValidatorParticipationPersistsAfterFirstCommitWithoutStakeOrWallet(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldRuntimeCore := runtimeCoreValidatorIDs()
	authMu.Lock()
	oldAuthNodeID := authNodeID
	oldAuthWalletAddr := authWalletAddr
	authMu.Unlock()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(oldRuntimeCore)
		authMu.Lock()
		authNodeID = oldAuthNodeID
		authWalletAddr = oldAuthWalletAddr
		authMu.Unlock()
	})

	ConfigAuthRequireWallet = true
	ValidatorRequireStake = true
	ValidatorCoreStakeExempt = true
	ConfigAuthCoreValidators = []string{"T", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	authMu.Lock()
	authNodeID = ""
	authWalletAddr = ""
	authMu.Unlock()

	validators := []string{"T", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "T"
	node.Role = "validator"
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	node.ValidatorKey = ValidatorKey{
		ID:         "T",
		PublicKey:  pub,
		PrivateKey: priv,
	}
	node.coreRegistryMu.Lock()
	node.applyLegacyCoreRegistryLocked(time.Now(), "warn")
	node.coreRegistryMu.Unlock()

	if ok, reason := node.validatorParticipationGateStatus(1); !ok {
		t.Fatalf("expected participation at height 1, got reason=%s", reason)
	}

	setHash := strings.TrimSpace(ValidatorSetHash(validators))
	if setHash == "" {
		t.Fatalf("empty validator set hash")
	}
	block1 := Block{
		ID:                   1,
		BlockHash:            "h1",
		ValidatorSetHash:     setHash,
		NextValidatorSetHash: setHash,
	}
	node.Blockchain.AddBlock(block1)
	node.commitMu.Lock()
	if node.committed == nil {
		node.committed = make(map[uint64]string)
	}
	node.committed[1] = block1.BlockHash
	node.committedHeight = 1
	node.commitMu.Unlock()

	if ok, reason := node.validatorParticipationGateStatus(2); !ok {
		t.Fatalf("expected participation at height 2 after first commit, got reason=%s", reason)
	}
}

func TestExpectedValidatorSetHashUsesBootstrapParentFallbackAtHeightTwo(t *testing.T) {
	oldV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		ValidatorSetCommitmentV2Height = oldV2Height
	})
	ValidatorSetCommitmentV2Height = 1

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	setHash := strings.TrimSpace(ValidatorSetHash(validators))
	if setHash == "" {
		t.Fatalf("empty validator set hash")
	}

	// Parent commitment intentionally absent to verify guarded bootstrap fallback.
	block1 := Block{
		ID:               1,
		BlockHash:        "h1",
		ValidatorSetHash: setHash,
	}
	node.Blockchain.AddBlock(block1)

	got, source := node.expectedValidatorSetHashWithSource(2)
	if !strings.EqualFold(strings.TrimSpace(got), setHash) {
		t.Fatalf("fallback hash mismatch: got=%s want=%s source=%s", got, setHash, source)
	}
	if normalizeExpectedValidatorSetSource(source) != "bootstrap_parent_fallback" {
		t.Fatalf("unexpected fallback source: %s", source)
	}
}

func TestStartupNetworkValidatorSetSampleStatusCountsLocalValidatorTowardQuorum(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "B"

	host, err := libp2p.New()
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer host.Close()
	node.Host = host

	now := time.Now()
	setHash := strings.TrimSpace(ValidatorSetHash([]string{"A", "B", "C", "D"}))
	node.validatorStatus["A"] = &ValidatorStatus{
		ID:               "A",
		FinalizedHeight:  339,
		ReportedHeight:   339,
		ValidatorSetHash: setHash,
		LastSeen:         now,
	}
	node.validatorStatus["D"] = &ValidatorStatus{
		ID:               "D",
		FinalizedHeight:  339,
		ReportedHeight:   339,
		ValidatorSetHash: setHash,
		LastSeen:         now,
	}

	ready, reason, networkHeight, votes, gotHash := node.startupNetworkValidatorSetSampleStatus(106)
	if !ready {
		t.Fatalf("expected startup sample to use local validator plus two remotes as quorum")
	}
	if reason != "network_validator_set_sample" {
		t.Fatalf("unexpected ready reason: %s", reason)
	}
	if votes != 2 {
		t.Fatalf("votes = %d, want 2", votes)
	}
	if networkHeight != 339 {
		t.Fatalf("network height = %d, want 339", networkHeight)
	}
	if gotHash != strings.ToLower(setHash) {
		t.Fatalf("hash = %q, want %q", gotHash, strings.ToLower(setHash))
	}

	delete(node.validatorStatus, "D")

	ready, reason, _, votes, _ = node.startupNetworkValidatorSetSampleStatus(106)
	if ready {
		t.Fatalf("expected startup sample to wait with only one healthy remote peer")
	}
	if reason != "waiting_network_validator_set_sample" {
		t.Fatalf("unexpected wait reason: %s", reason)
	}
	if votes != 1 {
		t.Fatalf("votes = %d, want 1", votes)
	}
}

func TestStartupNetworkValidatorSetSampleStatusSkipsQuorumForInactiveCandidate(t *testing.T) {
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

	node := makeStrictActivationNode(50)
	node.Host = host
	node.ID = "F"
	node.ValidatorKey = strictActivationTestValidatorKey(15, "F")
	node.Ledger = GenesisLedger()
	node.candidates["F"] = &CandidateStatus{
		ID:              "F",
		LastHeartbeatAt: time.Now(),
	}

	ready, reason, networkHeight, votes, gotHash := node.startupNetworkValidatorSetSampleStatus(50)
	if !ready {
		t.Fatalf("expected inactive onboarding candidate to bypass startup network sample gate")
	}
	if reason != "network_validator_set_sample_not_required_inactive_candidate" {
		t.Fatalf("unexpected reason: %s", reason)
	}
	if votes != 0 {
		t.Fatalf("votes = %d, want 0", votes)
	}
	if gotHash != "" {
		t.Fatalf("hash = %q, want empty", gotHash)
	}
	if networkHeight > 50 {
		t.Fatalf("network height = %d, want no higher than local height without remote sample", networkHeight)
	}
}
