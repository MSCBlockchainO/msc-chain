package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatFallbackRegistersValidator(t *testing.T) {
	oldDebug := DebugConsensus
	DebugConsensus = false
	t.Cleanup(func() {
		DebugConsensus = oldDebug
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B"})
	if node.candidates == nil {
		node.candidates = make(map[string]*CandidateStatus)
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	ann := ValidatorAnnouncement{
		NodeID:             "A",
		PubKey:             hex.EncodeToString(pub),
		ReportedHeight:     1,
		FinalizedHeight:    1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		IsValidator:        false,
	}
	data, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}

	node.handleValidatorAnnouncement(data)

	node.validatorMu.RLock()
	st := node.validatorStatus["A"]
	node.validatorMu.RUnlock()
	if st == nil || st.LastSeen.IsZero() {
		t.Fatalf("expected validator status to be registered via fallback")
	}
}

func TestValidatorAnnouncementAcceptsV5EmptyValidatorSetHash(t *testing.T) {
	oldDebug := DebugConsensus
	DebugConsensus = false
	validatorPubKeysMu.Lock()
	oldPubKeys := ValidatorPubKeys
	ValidatorPubKeys = make(map[string]ed25519.PublicKey)
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		DebugConsensus = oldDebug
		validatorPubKeysMu.Lock()
		ValidatorPubKeys = oldPubKeys
		validatorPubKeysMu.Unlock()
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B"})
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}

	ann := ValidatorAnnouncement{
		NodeID:               "A",
		PubKey:               hex.EncodeToString(pub),
		P2PAddr:              "/ip4/127.0.0.1/tcp/19001/p2p/12D3KooWJqFp6YvYxD7SXfJf3y9Li9nZQ5TPPjS7k2qxJbPryK5U",
		ReportedHeight:       0,
		FinalizedHeight:      0,
		ExecEpoch:            1,
		ValidatorSetHeight:   1,
		ActivationHeight:     1,
		ValidatorSetHash:     "",
		NextValidatorSetHash: "",
		NextActivationHeight: 2,
		ConsensusReadySet:    true,
		ConsensusReady:       false,
		IsValidator:          true,
	}
	ann.Signature = hex.EncodeToString(ed25519.Sign(priv, validatorAnnounceSignBytesV5(
		ann.NodeID,
		ann.PubKey,
		ann.P2PAddr,
		ann.ReportedHeight,
		ann.FinalizedHeight,
		ann.ExecEpoch,
		ann.ValidatorSetHeight,
		ann.ValidatorSetHash,
		ann.NextValidatorSetHash,
		ann.NextActivationHeight,
		ann.ConsensusReadySet,
		ann.ConsensusReady,
		ann.IsValidator,
	)))

	data, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}
	node.handleValidatorAnnouncement(data)

	validatorPubKeysMu.RLock()
	got, ok := ValidatorPubKeys["A"]
	validatorPubKeysMu.RUnlock()
	if !ok || !bytes.Equal(got, pub) {
		t.Fatalf("expected empty-hash V5 heartbeat to verify and store pubkey")
	}
}

func TestRegistryBootstrapFallbackEarlyHeight(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	node.Blockchain.AddBlock(Block{ID: 1, BlockHash: "h1"})

	snap := node.validatorRegistrySnapshotForHeight(2)
	rec, ok := snap["A"]
	if !ok || rec.ID != "A" {
		t.Fatalf("expected registry bootstrap fallback to include validator A")
	}
}

func TestEnsureRegistrySnapshotCreates(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	node.ensureRegistrySnapshot(1, "test")

	snap, err := node.loadValidatorRegistrySnapshot(1)
	if err != nil {
		t.Fatalf("expected registry snapshot to be created: %v", err)
	}
	rec, ok := snap["A"]
	if !ok || rec.ID != "A" {
		t.Fatalf("expected registry snapshot to include validator A")
	}
	gotHash := ValidatorRegistrySnapshotHash(snap)
	wantHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	if gotHash != wantHash {
		t.Fatalf("unexpected registry snapshot hash: got=%s want=%s", gotHash, wantHash)
	}
}

func TestEnsureRegistrySnapshotNoOpWhenExists(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	node.ensureRegistrySnapshot(1, "first")

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"B": {ID: "B", Stake: 200, Status: ValidatorActive},
	})

	node.ensureRegistrySnapshot(1, "second")

	snap, err := node.loadValidatorRegistrySnapshot(1)
	if err != nil {
		t.Fatalf("expected registry snapshot to remain: %v", err)
	}
	if len(snap) != 1 {
		t.Fatalf("expected single validator in snapshot, got=%d", len(snap))
	}
	rec, ok := snap["A"]
	if !ok || rec.ID != "A" {
		t.Fatalf("expected registry snapshot to remain unchanged")
	}
}

func TestEnsureRegistrySnapshotSkipsEmptyRegistry(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{})
	node.ensureRegistrySnapshot(1, "empty")

	if node.registrySnapshotExists(1) {
		t.Fatalf("expected registry snapshot to be skipped for empty registry")
	}
}

func TestPersistRegistrySnapshotOverwrites(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	node.persistValidatorRegistrySnapshot(2)

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"B": {ID: "B", Stake: 200, Status: ValidatorActive},
	})

	node.persistValidatorRegistrySnapshot(2)

	snap, err := node.loadValidatorRegistrySnapshot(2)
	if err != nil {
		t.Fatalf("expected registry snapshot to be persisted: %v", err)
	}
	if len(snap) != 1 {
		t.Fatalf("expected single validator in snapshot, got=%d", len(snap))
	}
	rec, ok := snap["B"]
	if !ok || rec.ID != "B" {
		t.Fatalf("expected registry snapshot to be overwritten with validator B")
	}
}

func TestPersistRegistrySnapshotSkipsEmptyRegistry(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{})
	node.persistValidatorRegistrySnapshot(2)

	if node.registrySnapshotExists(2) {
		t.Fatalf("expected registry snapshot to be skipped for empty registry")
	}
}

func TestPersistRegistrySnapshotUsesCommittedSourceSnapshot(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	committed := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	}
	advancedRuntime := map[string]ValidatorRecord{
		"B": {ID: "B", Stake: 200, Status: ValidatorActive},
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	expectedHash := ValidatorRegistrySnapshotHash(committed)
	node.Blockchain.AddBlock(Block{ID: 1, ValidatorRegistryHash: expectedHash})

	// Simulate runtime mutation after the committed snapshot was captured.
	GlobalValidatorRegistry.Load(advancedRuntime)
	node.persistValidatorRegistrySnapshotFromSource(1, committed)

	snap, err := node.loadValidatorRegistrySnapshot(1)
	if err != nil {
		t.Fatalf("expected committed-source snapshot to persist: %v", err)
	}
	if got := ValidatorRegistrySnapshotHash(snap); got != expectedHash {
		t.Fatalf("unexpected persisted hash: got=%s want=%s", got, expectedHash)
	}
	if len(snap) != 1 || snap["A"].ID != "A" {
		t.Fatalf("expected committed snapshot content, got=%v", snap)
	}
}

func TestPersistRegistrySnapshotCarryForwardOnSourceMismatch(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	committed := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
	}
	mismatched := map[string]ValidatorRecord{
		"Z": {ID: "Z", Stake: 300, Status: ValidatorActive},
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	expectedHash := ValidatorRegistrySnapshotHash(committed)
	node.Blockchain.AddBlock(Block{ID: 1, ValidatorRegistryHash: expectedHash})
	node.Blockchain.AddBlock(Block{ID: 2, ValidatorRegistryHash: expectedHash})

	// Height 1 stores the authoritative committed snapshot.
	node.persistValidatorRegistrySnapshotFromSource(1, committed)

	// Height 2 receives a mismatched source; persist path must carry forward
	// the previously committed hash-matching snapshot.
	node.persistValidatorRegistrySnapshotFromSource(2, mismatched)

	snap, err := node.loadValidatorRegistrySnapshot(2)
	if err != nil {
		t.Fatalf("expected carry-forward snapshot at height 2: %v", err)
	}
	if got := ValidatorRegistrySnapshotHash(snap); got != expectedHash {
		t.Fatalf("unexpected carry-forward hash: got=%s want=%s", got, expectedHash)
	}
	if len(snap) != 1 || snap["A"].ID != "A" {
		t.Fatalf("expected carry-forward snapshot content, got=%v", snap)
	}
}

func TestPersistRegistrySnapshotRejectsUnrepairableMismatch(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	mismatched := map[string]ValidatorRecord{
		"Z": {ID: "Z", Stake: 300, Status: ValidatorActive},
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Blockchain.AddBlock(Block{ID: 2, ValidatorRegistryHash: "expected-hash"})

	if err := node.persistValidatorRegistrySnapshotFromSource(2, mismatched); err == nil {
		t.Fatalf("expected unrepairable registry mismatch to fail")
	}
	if node.registrySnapshotExists(2) {
		t.Fatalf("expected mismatched registry snapshot to remain unpersisted")
	}
}

func TestDeterministicPreCommitRegistrySnapshotUsesGenesisProjectionForBootstrapBlock(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	// Fresh joiner runtime may not have the committed genesis validators yet.
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"G": {ID: "G", Stake: 100, Status: ValidatorActive},
	})

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Blockchain.AddBlock(Block{ID: 1, BlockHash: "block-1"})

	wantSnapshot, wantHash, ok := node.genesisCommittedValidatorRegistryCandidate(nil)
	if !ok {
		t.Fatalf("expected genesis registry projection")
	}

	got, source, err := node.deterministicPreCommitRegistrySnapshot(Block{
		ID:                    2,
		BlockHash:             "block-2",
		PrevHash:              "block-1",
		ValidatorRegistryHash: wantHash,
	})
	if err != nil {
		t.Fatalf("expected bootstrap registry projection to resolve: %v", err)
	}
	if source != "genesis_bootstrap_projection" {
		t.Fatalf("unexpected source: got=%q want=genesis_bootstrap_projection", source)
	}
	if gotHash := ValidatorRegistrySnapshotHash(got); !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected snapshot hash: got=%q want=%q", gotHash, wantHash)
	}
	if len(got) != len(wantSnapshot) {
		t.Fatalf("unexpected validator count: got=%d want=%d", len(got), len(wantSnapshot))
	}
}

func TestDeterministicPreCommitRegistrySnapshotUsesCommittedParentSnapshotWhenHeaderEmpty(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)

	node := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "block-1"}}},
	}

	got, source, err := node.deterministicPreCommitRegistrySnapshot(Block{
		ID:        2,
		BlockHash: "block-2",
		PrevHash:  "block-1",
	})
	if err != nil {
		t.Fatalf("expected committed parent snapshot to resolve: %v", err)
	}
	if source != "committed_parent_snapshot" {
		t.Fatalf("unexpected source: got=%q want=committed_parent_snapshot", source)
	}
	wantHash := ValidatorRegistrySnapshotHash(registry)
	if gotHash := ValidatorRegistrySnapshotHash(got); !strings.EqualFold(gotHash, wantHash) {
		t.Fatalf("unexpected snapshot hash: got=%q want=%q", gotHash, wantHash)
	}
}

func TestSafeModeHeartbeatFallbackExits(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	height := uint64(1)
	now := time.Now()

	node.validatorMu.Lock()
	for _, id := range validators {
		node.validatorStatus[id] = &ValidatorStatus{
			ID:                 id,
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height,
			ValidatorSetHeight: height,
			LastSeen:           now,
			Active:             true,
		}
	}
	node.validatorMu.Unlock()

	node.commitMu.Lock()
	node.finalizedHeight = 100
	node.committedHeight = 100
	node.commitMu.Unlock()

	node.validatorSetMu.Lock()
	if node.safeModeUntilByHeight == nil {
		node.safeModeUntilByHeight = make(map[uint64]time.Time)
	}
	if node.safeModeWindowByHeight == nil {
		node.safeModeWindowByHeight = make(map[uint64]time.Duration)
	}
	node.safeModeUntilByHeight[height] = now.Add(30 * time.Second)
	node.safeModeWindowByHeight[height] = 2 * time.Second
	node.validatorSetMu.Unlock()

	if ok := node.tryExitPostBlockSafeMode(height); !ok {
		t.Fatalf("expected safe-mode exit using heartbeat fallback")
	}
}

func TestEnterPostBlockSafeModeAsyncActivates(t *testing.T) {
	oldSafeMode := ConsensusPostBlockSafeModeEnabled
	oldDebug := DebugConsensus
	t.Cleanup(func() {
		ConsensusPostBlockSafeModeEnabled = oldSafeMode
		DebugConsensus = oldDebug
	})

	ConsensusPostBlockSafeModeEnabled = true
	DebugConsensus = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	height := uint64(2)

	node.enterPostBlockSafeModeAsync(height)
	if active, _, _ := node.postBlockSafeModeState(height); !active {
		t.Fatalf("expected synchronous safe-mode gate for height=%d", height)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if active, _, _ := node.postBlockSafeModeState(height); active {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected async safe-mode activation for height=%d", height)
}

func TestEnterPostBlockSafeModeAsyncDoesNotReopenAfterImmediateExit(t *testing.T) {
	oldSafeMode := ConsensusPostBlockSafeModeEnabled
	oldDebug := DebugConsensus
	t.Cleanup(func() {
		ConsensusPostBlockSafeModeEnabled = oldSafeMode
		DebugConsensus = oldDebug
	})

	ConsensusPostBlockSafeModeEnabled = true
	DebugConsensus = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	height := uint64(1)

	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		ID:                 "A",
		ReportedHeight:     height,
		FinalizedHeight:    height,
		ExecEpoch:          height,
		ValidatorSetHeight: height,
		LastSeen:           time.Now(),
		Active:             true,
	}
	node.validatorMu.Unlock()

	node.enterPostBlockSafeModeAsync(height)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if active, _, _ := node.postBlockSafeModeState(height); !active {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if active, _, _ := node.postBlockSafeModeState(height); active {
		t.Fatalf("expected live quorum to skip safe mode activation")
	}
}

func TestSafeModeHeartbeatFallbackRequiresQuorum(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	height := uint64(1)
	now := time.Now()
	ttl := validatorLivenessHeartbeatTTL()
	grace := validatorLivenessGrace()
	stale := now.Add(-(ttl + grace + time.Second))

	node.validatorMu.Lock()
	for i, id := range validators {
		lastSeen := now
		if i >= 2 {
			lastSeen = stale
		}
		node.validatorStatus[id] = &ValidatorStatus{
			ID:                 id,
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height,
			ValidatorSetHeight: height,
			LastSeen:           lastSeen,
			Active:             true,
		}
	}
	node.validatorMu.Unlock()

	node.commitMu.Lock()
	node.finalizedHeight = 100
	node.committedHeight = 100
	node.commitMu.Unlock()

	node.validatorSetMu.Lock()
	if node.safeModeUntilByHeight == nil {
		node.safeModeUntilByHeight = make(map[uint64]time.Time)
	}
	if node.safeModeWindowByHeight == nil {
		node.safeModeWindowByHeight = make(map[uint64]time.Duration)
	}
	node.safeModeUntilByHeight[height] = now.Add(30 * time.Second)
	node.safeModeWindowByHeight[height] = 2 * time.Second
	node.validatorSetMu.Unlock()

	if ok := node.tryExitPostBlockSafeMode(height); ok {
		t.Fatalf("expected safe-mode to remain active without heartbeat quorum")
	}
}

func TestValidatorHeartbeatExitsSafeModeAndBroadcastsLeaderVote(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldDebugConsensus := DebugConsensus
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		DebugConsensus = oldDebugConsensus
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = true
	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(91, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}

	height := node.currentEpoch()
	now := time.Now()
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		ID:                 "A",
		ReportedHeight:     height,
		FinalizedHeight:    height,
		ExecEpoch:          height + 1,
		ValidatorSetHeight: height + 1,
		ValidatorSetHash:   ValidatorSetHash(validators),
		LastSeen:           now,
		Active:             true,
	}
	node.validatorMu.Unlock()

	proposeRound := uint32(0)
	foundRound := false
	for round := uint32(0); round < 32; round++ {
		if node.consensusLeaderForHeightRound(height, round, validators) == node.ID {
			proposeRound = round
			foundRound = true
			break
		}
	}
	if !foundRound {
		t.Fatal("did not find a proposer round for node A")
	}
	node.setProposedRound(height, proposeRound)
	block := node.BuildLeaderBlock(height)
	if !node.storeLeaderBlock(block) {
		t.Fatal("expected leader block to be stored")
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	if proposalKey == "" {
		t.Fatal("expected proposal key for stored leader block")
	}

	node.validatorSetMu.Lock()
	if node.safeModeUntilByHeight == nil {
		node.safeModeUntilByHeight = make(map[uint64]time.Time)
	}
	if node.safeModeWindowByHeight == nil {
		node.safeModeWindowByHeight = make(map[uint64]time.Duration)
	}
	node.safeModeUntilByHeight[height] = now.Add(30 * time.Second)
	node.safeModeWindowByHeight[height] = 2 * time.Second
	node.validatorSetMu.Unlock()

	announce := func(id string, seed byte) {
		t.Helper()
		key := strictActivationTestValidatorKey(seed, id)
		data, err := json.Marshal(ValidatorAnnouncement{
			NodeID:             id,
			PubKey:             hex.EncodeToString(key.PublicKey),
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height + 1,
			ValidatorSetHeight: height + 1,
			ValidatorSetHash:   ValidatorSetHash(validators),
			IsValidator:        true,
		})
		if err != nil {
			t.Fatalf("marshal validator announcement: %v", err)
		}
		node.handleValidatorAnnouncement(data)
	}

	if active, _, _ := node.postBlockSafeModeState(height); !active {
		t.Fatal("expected safe mode to start active")
	}
	if node.hasExecSignerSeenForProposal(height, proposalKey, "A") {
		t.Fatal("unexpected local execution vote before heartbeat quorum")
	}

	announce("B", 92)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		active, _, _ := node.postBlockSafeModeState(height)
		if !active && node.hasExecSignerSeenForProposal(height, proposalKey, "A") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	if active, _, _ := node.postBlockSafeModeState(height); active {
		t.Fatal("expected validator heartbeat quorum to clear safe mode")
	}
	if !node.hasExecSignerSeenForProposal(height, proposalKey, "A") {
		t.Fatal("expected validator heartbeat quorum to trigger the local execution vote")
	}
}

func TestProposerSignatureFallbackUsesCandidatePubKeyOnTestnet(t *testing.T) {
	oldTestnet := IsTestnet
	oldDebug := DebugConsensus
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		IsTestnet = oldTestnet
		DebugConsensus = oldDebug
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisPubKeys
	})

	IsTestnet = true
	DebugConsensus = false
	ValidatorPubKeys = make(map[string]ed25519.PublicKey)
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	if node.candidates == nil {
		node.candidates = make(map[string]*CandidateStatus)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	node.ValidatorKey = ValidatorKey{
		ID:         "A",
		PublicKey:  pub,
		PrivateKey: priv,
	}
	node.candidates["A"] = &CandidateStatus{
		ID:              "A",
		PubKey:          pub,
		LastHeartbeatAt: time.Now(),
	}

	lt := LogicalTimeForEpoch(1)
	prev := node.Blockchain.LastBlock().BlockHash
	block := Block{
		ID:        1,
		Proposer:  "A",
		PrevHash:  prev,
		BlockTime: lt,
		Timestamp: int64(SystemTimeUnits(lt)),
	}
	node.SignBlock(&block)

	if ok := verifyBlockSignatureWithCandidates(block, []ed25519.PublicKey{pub}); !ok {
		t.Fatalf("expected candidate pubkey to verify proposer signature")
	}
	if inSet, _, ok := node.authoritativeHeartbeatMembershipAtHeight("A", 1); !ok || !inSet {
		t.Fatalf("expected proposer to be in authoritative set")
	}
	if node.currentEpoch() != 1 {
		t.Fatalf("unexpected current epoch: got=%d want=1", node.currentEpoch())
	}

	if ok := node.verifyLeaderBlock(block, ""); !ok {
		t.Fatalf("expected proposer signature fallback to accept block on testnet")
	}
	validatorPubKeysMu.RLock()
	got := ValidatorPubKeys["A"]
	validatorPubKeysMu.RUnlock()
	if !bytes.Equal(got, pub) {
		t.Fatalf("expected proposer pubkey to be updated via fallback")
	}
}

func TestProposerSignatureFallbackDisabledOnMainnet(t *testing.T) {
	oldTestnet := IsTestnet
	oldDebug := DebugConsensus
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		IsTestnet = oldTestnet
		DebugConsensus = oldDebug
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisPubKeys
	})

	IsTestnet = false
	DebugConsensus = false
	ValidatorPubKeys = make(map[string]ed25519.PublicKey)
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	if node.candidates == nil {
		node.candidates = make(map[string]*CandidateStatus)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	node.ValidatorKey = ValidatorKey{
		ID:         "A",
		PublicKey:  pub,
		PrivateKey: priv,
	}
	node.candidates["A"] = &CandidateStatus{
		ID:              "A",
		PubKey:          pub,
		LastHeartbeatAt: time.Now(),
	}

	lt := LogicalTimeForEpoch(1)
	prev := node.Blockchain.LastBlock().BlockHash
	block := Block{
		ID:        1,
		Proposer:  "A",
		PrevHash:  prev,
		BlockTime: lt,
		Timestamp: int64(SystemTimeUnits(lt)),
	}
	node.SignBlock(&block)

	if ok := node.verifyLeaderBlock(block, ""); ok {
		t.Fatalf("expected proposer signature fallback to be disabled on mainnet")
	}
	validatorPubKeysMu.RLock()
	_, ok := ValidatorPubKeys["A"]
	validatorPubKeysMu.RUnlock()
	if ok {
		t.Fatalf("unexpected proposer pubkey update on mainnet")
	}
}
