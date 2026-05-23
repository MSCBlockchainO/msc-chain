package main

import (
	"crypto/ed25519"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestApplySnapshotForSyncRestoresCommittedValidatorState(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)

	chain := NewBlockchain()
	node := &Node{
		ID:                          "A",
		Role:                        "validator",
		Blockchain:                  &chain,
		Ledger:                      GenesisLedger(),
		GenesisValidators:           []string{"A", "B", "C", "D"},
		pendingValidators:           map[string]uint64{},
		pendingValidatorRemovals:    map[string]uint64{},
		frozenValidatorsByHeight:    map[uint64][]string{},
		frozenValidatorHashByHeight: map[uint64]string{},
		epochValidators:             map[uint64][]string{},
		committed:                   map[uint64]string{},
	}

	snapshotLedger := NewLedger()
	snapshotLedger.Balances["wallet-f"] = 1234
	snapshotLedger.Stakes["stake|F"] = StakeLock{
		ValidatorID: "F",
		Amount:      500,
	}

	snapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      25,
		PrevHash:    "h24",
		BlockHash:   "h25",
		StateRoot:   "state25",
		GenesisHash: GenesisHash,
		Ledger:      snapshotLedger,
		LedgerHash:  HashLedger(snapshotLedger),
		Validators: map[string]bool{
			"A": true,
			"B": true,
			"C": true,
			"D": true,
		},
		ValidatorRegistry: map[string]ValidatorRecord{
			"A": {ID: "A", Stake: 1000, Status: ValidatorActive},
			"B": {ID: "B", Stake: 900, Status: ValidatorActive},
			"C": {ID: "C", Stake: 800, Status: ValidatorActive},
			"D": {ID: "D", Stake: 700, Status: ValidatorActive},
			"F": {ID: "F", Stake: 500, Status: ValidatorPending},
		},
		PendingValidators: map[string]uint64{
			"F": 26,
		},
		PendingValidatorRemovals: map[string]uint64{
			"D": 26,
		},
		ValidatorSetHeight:     25,
		NextValidatorSetHash:   "next-25",
		NextValidatorSetHeight: 26,
		ActivationHeight:       26,
	}

	node.ApplySnapshotForSync(snapshot)

	if got := node.Blockchain.Height(); got != 25 {
		t.Fatalf("unexpected blockchain height after snapshot apply: got=%d want=25", got)
	}
	if got := node.Blockchain.FinalizedHeight(); got != 25 {
		t.Fatalf("unexpected finalized height after snapshot apply: got=%d want=25", got)
	}
	if got := node.Ledger.Balances["wallet-f"]; got != 1234 {
		t.Fatalf("unexpected restored balance: got=%d want=1234", got)
	}
	if got := node.committedHeight; got != 25 {
		t.Fatalf("unexpected committed height after snapshot apply: got=%d want=25", got)
	}
	if got := node.lastCommitHeight; got != 25 {
		t.Fatalf("unexpected lastCommitHeight after snapshot apply: got=%d want=25", got)
	}

	node.validatorSetMu.RLock()
	gotFrozen := append([]string{}, node.frozenValidatorsByHeight[25]...)
	node.validatorSetMu.RUnlock()

	wantFrozen := []string{"A", "B", "C", "D"}
	if !reflect.DeepEqual(gotFrozen, wantFrozen) {
		t.Fatalf("unexpected frozen validator set: got=%v want=%v", gotFrozen, wantFrozen)
	}

	registrySnapshot := GlobalValidatorRegistry.Snapshot()
	if got := registrySnapshot["F"].Stake; got != 500 {
		t.Fatalf("unexpected restored validator registry stake for F: got=%d want=500", got)
	}
	if got := registrySnapshot["F"].Status; got != ValidatorPending {
		t.Fatalf("unexpected restored validator registry status for F: got=%q want=%q", got, ValidatorPending)
	}

	anchor, ok := node.LoadBlock(25)
	if !ok {
		t.Fatalf("expected anchor block at snapshot height")
	}
	if anchor.BlockHash != "h25" {
		t.Fatalf("unexpected anchor block hash: got=%q want=%q", anchor.BlockHash, "h25")
	}
	if anchor.NextValidatorSetHeight != 26 || anchor.ActivationHeight != 26 {
		t.Fatalf("unexpected anchor activation fields: next=%d activation=%d", anchor.NextValidatorSetHeight, anchor.ActivationHeight)
	}
}

func TestApplySnapshotForSyncPersistsCrashRecoveryEnvelope(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	node.Consensus = nil
	ledger := GenesisLedger()
	snapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      42,
		PrevHash:    "hash-41",
		BlockHash:   "hash-42",
		StateRoot:   "state-42",
		GenesisHash: GenesisHash,
		Ledger:      ledger,
		LedgerHash:  HashLedger(ledger),
		Validators:  map[string]bool{"A": true, "B": true, "C": true},
	}

	node.ApplySnapshotForSync(snapshot)
	assertSnapshotCommitSafetyPersisted(t, node, snapshot.Height, snapshot.BlockHash)
}

func TestApplySnapshotForRecoveryPersistsCrashRecoveryEnvelope(t *testing.T) {
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C"})
	node.Consensus = nil
	ledger := GenesisLedger()
	snapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      44,
		PrevHash:    "hash-43",
		BlockHash:   "hash-44",
		StateRoot:   "state-44",
		GenesisHash: GenesisHash,
		Ledger:      ledger,
		LedgerHash:  HashLedger(ledger),
		Validators:  map[string]bool{"A": true, "B": true, "C": true},
	}

	node.ApplySnapshotForRecovery(snapshot)
	assertSnapshotCommitSafetyPersisted(t, node, snapshot.Height, snapshot.BlockHash)
}

func TestApplySnapshotForRecoverySameHeightRealignsPostCommitLedger(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldEmissionEnabled := EmissionRewardEnabled
	oldEmissionMin := EmissionMinReward
	oldEmissionMax := EmissionMaxReward
	oldEmissionBaseChance := EmissionBaseChanceBPS
	oldEmissionHighChance := EmissionHighChanceBPS
	oldEmissionValidatorToProposer := EmissionValidatorToProposer
	oldBurnStopSupply := BurnStopSupply
	oldMinted := TotalMintedMSC
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		EmissionRewardEnabled = oldEmissionEnabled
		EmissionMinReward = oldEmissionMin
		EmissionMaxReward = oldEmissionMax
		EmissionBaseChanceBPS = oldEmissionBaseChance
		EmissionHighChanceBPS = oldEmissionHighChance
		EmissionValidatorToProposer = oldEmissionValidatorToProposer
		BurnStopSupply = oldBurnStopSupply
		TotalMintedMSC = oldMinted
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	EmissionRewardEnabled = true
	EmissionMinReward = 2
	EmissionMaxReward = 2
	EmissionBaseChanceBPS = 10000
	EmissionHighChanceBPS = 10000
	EmissionValidatorToProposer = false
	BurnStopSupply = 0
	TotalMintedMSC = 0

	validators := []string{"A", "B", "C", "D"}
	bootstrapValidatorRegistry(validators, 1)
	node, block, executionLedger, genesisHash := setupStartupExecutionSnapshotNodeWithGenesis(t, validators, func(genesis *Genesis) {
		genesis.Balances = map[string]int{
			TREASURY_ADDRESS: 1000,
		}
	})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	postCommitLedger := node.applyPostBlockEffectsToLedger(block, executionLedger)
	if HashLedger(postCommitLedger) == HashLedger(executionLedger) {
		t.Fatalf("test setup ineffective: post-commit effects did not change ledger")
	}

	staleLedger := NewLedger()
	staleLedger.Balances["MSC|stale"] = 99
	node.setExecutionLedger(staleLedger)

	validatorMap := make(map[string]bool, len(validators))
	for _, id := range validators {
		validatorMap[id] = true
	}
	snapshot := StateSnapshot{
		Version:              SnapshotVersion,
		Height:               block.ID,
		PrevHash:             block.PrevHash,
		BlockHash:            block.BlockHash,
		StateRoot:            block.StateRoot,
		GenesisHash:          GenesisHash,
		Ledger:               executionLedger,
		LedgerHash:           HashLedger(executionLedger),
		LedgerStage:          snapshotLedgerStageExecution,
		Validators:           validatorMap,
		ValidatorRegistry:    GlobalValidatorRegistry.Snapshot(),
		ValidatorSetHash:     block.ValidatorSetHash,
		NextValidatorSetHash: block.NextValidatorSetHash,
	}

	node.ApplySnapshotForRecovery(snapshot)

	if got, want := HashLedger(node.currentExecutionLedgerClone()), HashLedger(postCommitLedger); got != want {
		t.Fatalf("same-height recovery should restore post-commit parent ledger: got=%q want=%q", got, want)
	}
	cachedExecutionLedger, ok := node.cachedExecutionSnapshotLedger(block.ID)
	if !ok {
		t.Fatalf("expected raw execution snapshot ledger to remain cached")
	}
	if got, want := HashLedger(cachedExecutionLedger), HashLedger(executionLedger); got != want {
		t.Fatalf("raw execution snapshot cache changed: got=%q want=%q", got, want)
	}
}

func TestApplyDownloadedSnapshotRejectsLowerSnapshotRegression(t *testing.T) {
	chain := NewBlockchain()
	node := &Node{
		ID:                          "A",
		Role:                        "validator",
		Blockchain:                  &chain,
		Ledger:                      GenesisLedger(),
		ExecutionLedger:             GenesisLedger(),
		GenesisValidators:           []string{"A", "B", "C", "D"},
		pendingValidators:           map[string]uint64{},
		pendingValidatorRemovals:    map[string]uint64{},
		frozenValidatorsByHeight:    map[uint64][]string{},
		frozenValidatorHashByHeight: map[uint64]string{},
		epochValidators:             map[uint64][]string{},
		committed:                   map[uint64]string{},
	}

	localSnapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      25,
		PrevHash:    "h24",
		BlockHash:   "h25",
		StateRoot:   "state25",
		GenesisHash: GenesisHash,
		Ledger:      GenesisLedger(),
		LedgerHash:  HashLedger(GenesisLedger()),
	}
	node.ApplySnapshotForSync(localSnapshot)

	olderLedger := NewLedger()
	olderLedger.Balances["rollback"] = 77
	recoverySnapshot := StateSnapshot{
		Version:     SnapshotVersion,
		Height:      20,
		PrevHash:    "h19",
		BlockHash:   "h20",
		StateRoot:   "state20",
		GenesisHash: GenesisHash,
		Ledger:      olderLedger,
		LedgerHash:  HashLedger(olderLedger),
		Validators: map[string]bool{
			"A": true,
			"B": true,
			"C": true,
			"D": true,
		},
	}

	if applied := node.applyDownloadedSnapshot(&recoverySnapshot, true); applied {
		t.Fatalf("lower snapshot must not be reported as applied")
	}
	if got := node.Blockchain.Height(); got != 25 {
		t.Fatalf("lower snapshot must not roll back chain height: got=%d want=25", got)
	}
	if got := node.Ledger.Balances["rollback"]; got != 0 {
		t.Fatalf("lower snapshot ledger payload must not be applied: got=%d", got)
	}
}

func TestApplySnapshotForSyncStartsNextRoundImmediately(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldRequireStake := ValidatorRequireStake
	oldStrictActivation := ValidatorOnboardingStrictActivation
	oldSafeModeEnabled := ConsensusPostBlockSafeModeEnabled
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	oldResultGossipOnly := ResultGossipOnly
	oldValidatorSetHashV3Height := ValidatorSetHashV3Height
	oldValidatorSetCommitmentV2Height := ValidatorSetCommitmentV2Height
	oldConsensusProposeRequiresSyncReady := ConsensusProposeRequiresSyncReady
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		ValidatorRequireStake = oldRequireStake
		ValidatorOnboardingStrictActivation = oldStrictActivation
		ConsensusPostBlockSafeModeEnabled = oldSafeModeEnabled
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		GlobalValidatorRegistry.Load(oldRegistry)
		ResultGossipOnly = oldResultGossipOnly
		ValidatorSetHashV3Height = oldValidatorSetHashV3Height
		ValidatorSetCommitmentV2Height = oldValidatorSetCommitmentV2Height
		ConsensusProposeRequiresSyncReady = oldConsensusProposeRequiresSyncReady
	})

	ConfigAuthRequireWallet = false
	ValidatorRequireStake = false
	ValidatorOnboardingStrictActivation = false
	ConsensusPostBlockSafeModeEnabled = false
	ResultGossipOnly = true
	ValidatorSetHashV3Height = ^uint64(0)
	ValidatorSetCommitmentV2Height = ^uint64(0)
	ConsensusProposeRequiresSyncReady = false

	validators := []string{"A"}
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{})
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.Role = "validator"
	node.ValidatorKey = strictActivationTestValidatorKey(74, "A")
	node.Consensus = NewConsensusState(1)
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), node.ValidatorKey.PublicKey...),
	}
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{
		LastSeen:           time.Now(),
		Active:             true,
		FinalizedHeight:    1,
		ReportedHeight:     1,
		ExecEpoch:          2,
		ValidatorSetHeight: 2,
		ValidatorSetHash:   ValidatorSetHash(validators),
	}
	node.validatorMu.Unlock()

	snapshotLedger := GenesisLedger()
	setHash := ValidatorSetHash(validators)
	node.ApplySnapshotForSync(StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 1,
		PrevHash:               "genesis",
		BlockHash:              "snapshot-1",
		StateRoot:              "state-1",
		GenesisHash:            GenesisHash,
		Ledger:                 snapshotLedger,
		LedgerHash:             HashLedger(snapshotLedger),
		Validators:             map[string]bool{"A": true},
		ValidatorRegistry:      map[string]ValidatorRecord{"A": {ID: "A", Stake: 1000, Status: ValidatorActive}},
		ValidatorSetHeight:     1,
		ValidatorSetHash:       setHash,
		NextValidatorSetHash:   setHash,
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
	})

	node.heartbeatMu.Lock()
	forcePending := node.heartbeatForcePending
	node.heartbeatMu.Unlock()
	if !forcePending {
		t.Fatalf("expected snapshot sync apply to queue an immediate heartbeat refresh")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if block, ok := node.getLeaderBlock(2); ok && block.Round == 0 && normalizeValidatorID(block.Proposer) == "A" {
			return
		}
		if node.Blockchain.Height() >= 2 && node.proposedRoundForHeight(2) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	consensusHeight := uint64(0)
	consensusRound := uint32(0)
	consensusSyncing := false
	consensusPaused := false
	if node.Consensus != nil {
		node.Consensus.mu.Lock()
		consensusHeight = node.Consensus.Height
		consensusRound = node.Consensus.Round
		consensusSyncing = node.Consensus.Syncing
		consensusPaused = node.Consensus.Paused
		node.Consensus.mu.Unlock()
	}
	participationReady, participationReason := node.validatorParticipationGateStatus(2)
	syncBlocked, syncReason, syncTarget := node.consensusSyncGateForHeight(2)
	node.immediateRoundStartMu.Lock()
	pendingImmediate := node.immediateRoundStartPendingHeight
	startedImmediate := node.immediateRoundStartStartedHeight
	node.immediateRoundStartMu.Unlock()
	t.Fatalf("expected snapshot sync apply to start next-height round 0 immediately: chain=%d current_epoch=%d consensus=%d/%d syncing=%t paused=%t participation=%t/%s sync_blocked=%t/%s target=%d leader=%t proposed_round=%d immediate_pending=%d immediate_started=%d",
		node.Blockchain.Height(),
		node.currentEpoch(),
		consensusHeight,
		consensusRound,
		consensusSyncing,
		consensusPaused,
		participationReady,
		participationReason,
		syncBlocked,
		syncReason,
		syncTarget,
		node.isRoundLeader(2, 0),
		node.proposedRoundForHeight(2),
		pendingImmediate,
		startedImmediate,
	)
}

func assertSnapshotCommitSafetyPersisted(t *testing.T, node *Node, height uint64, hash string) {
	t.Helper()
	if got, found, err := node.loadFinalizedHashInvariant(height); err != nil || !found || got != hash {
		t.Fatalf("expected snapshot finalized invariant: got=%q found=%t err=%v", got, found, err)
	}
	node.commitMu.Lock()
	node.committed = map[uint64]string{}
	node.committedHeight = 0
	node.finalizedHeight = 0
	node.lastCommitHeight = 0
	node.commitMu.Unlock()
	if err := node.restoreConsensusSafetyState(); err != nil {
		t.Fatalf("restore consensus safety after snapshot crash: %v", err)
	}
	node.commitMu.Lock()
	committedHash := node.committed[height]
	committedHeight := node.committedHeight
	finalizedHeight := node.finalizedHeight
	lastCommitHeight := node.lastCommitHeight
	node.commitMu.Unlock()
	if committedHash != hash || committedHeight != height || finalizedHeight != height || lastCommitHeight != height {
		t.Fatalf("snapshot commit safety not restored: hash=%q committed=%d finalized=%d last=%d",
			committedHash, committedHeight, finalizedHeight, lastCommitHeight)
	}
	if !node.hasCommittedDifferentHash(height, "fork-"+hash) {
		t.Fatal("snapshot anchor fork must be rejected after crash restore")
	}
}
