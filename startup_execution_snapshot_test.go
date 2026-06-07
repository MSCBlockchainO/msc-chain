package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeStartupExecutionSnapshotGenesis(t *testing.T, dir string, genesis Genesis) string {
	t.Helper()
	data, err := json.Marshal(genesis)
	if err != nil {
		t.Fatalf("marshal genesis: %v", err)
	}
	path := filepath.Join(dir, "genesis.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write genesis: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func setupStartupExecutionSnapshotNode(t *testing.T, validators []string) (*Node, Block, Ledger, string) {
	return setupStartupExecutionSnapshotNodeWithGenesis(t, validators, nil)
}

func setupStartupExecutionSnapshotNodeWithGenesis(t *testing.T, validators []string, configure func(*Genesis)) (*Node, Block, Ledger, string) {
	t.Helper()

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.Role = "validator"
	genesis := Genesis{
		ChainID:    ChainID,
		Validators: make(map[string]string, len(validators)),
	}
	for idx, id := range validators {
		genesis.Validators[id] = hex.EncodeToString(strictActivationTestPub(byte(idx + 11)))
	}
	if configure != nil {
		configure(&genesis)
	}
	genesisHash := writeStartupExecutionSnapshotGenesis(t, node.DataDir, genesis)

	genesisLedger, err := buildExecutionGenesisLedgerFromGenesis(&genesis)
	if err != nil {
		t.Fatalf("build genesis ledger: %v", err)
	}
	setHash := strings.TrimSpace(ValidatorSetHash(validators))
	if setHash == "" {
		t.Fatalf("empty validator set hash")
	}

	block := Block{
		ID:                   1,
		Type:                 BlockTypeTime,
		PrevHash:             "genesis",
		BlockHash:            "block-1",
		Proposer:             validators[0],
		ValidatorSetHash:     setHash,
		NextValidatorSetHash: setHash,
		BlockTime:            LogicalTimeForEpoch(1),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.StateRoot = ComputeExecHash(block, HashLedger(genesisLedger))

	node.Blockchain.AddBlock(block)
	node.commitMu.Lock()
	node.committed[block.ID] = block.BlockHash
	node.committedHeight = block.ID
	node.commitMu.Unlock()

	return node, block, genesisLedger, genesisHash
}

func appendStartupExecutionSnapshotBlock(t *testing.T, node *Node, parent Block, parentLedger Ledger, proposer string) (Block, Ledger) {
	t.Helper()
	if node == nil {
		t.Fatalf("node unavailable")
	}
	setHash := strings.TrimSpace(parent.NextValidatorSetHash)
	if setHash == "" {
		setHash = strings.TrimSpace(parent.ValidatorSetHash)
	}
	next := Block{
		ID:                   parent.ID + 1,
		Type:                 BlockTypeTime,
		PrevHash:             parent.BlockHash,
		BlockHash:            "block-" + strconv.FormatUint(parent.ID+1, 10),
		Proposer:             proposer,
		ValidatorSetHash:     setHash,
		NextValidatorSetHash: setHash,
		BlockTime:            LogicalTimeForEpoch(parent.ID + 1),
	}
	next.Timestamp = int64(SystemTimeUnits(next.BlockTime))
	next.StateRoot = ComputeExecHash(next, HashLedger(parentLedger))
	node.Blockchain.AddBlock(next)
	node.commitMu.Lock()
	node.committed[next.ID] = next.BlockHash
	node.committedHeight = next.ID
	node.commitMu.Unlock()
	nextLedger, err := ApplyBlockStateWithNode(node, parentLedger, next)
	if err != nil {
		t.Fatalf("apply block %d: %v", next.ID, err)
	}
	return next, nextLedger
}

func storeExecutionSnapshotForTest(t *testing.T, node *Node, block Block, ledger Ledger, version uint32, stage string) {
	t.Helper()
	if node == nil || node.DB == nil {
		t.Fatalf("node db unavailable")
	}
	validators := node.consensusValidatorsForHeight(block.ID + 1)
	validatorMap := make(map[string]bool, len(validators))
	for _, id := range validators {
		validatorMap[id] = true
	}
	snapshot := StateSnapshot{
		Version:         version,
		Height:          block.ID,
		BlockHash:       block.BlockHash,
		StateRoot:       block.StateRoot,
		StateMerkleRoot: LedgerStateMerkleRoot(ledger),
		LedgerHash:      HashLedger(ledger),
		LedgerStage:     stage,
		GenesisHash:     GenesisHash,
		PrevHash:        block.PrevHash,
		Ledger:          ledger.Clone(),
		Validators:      validatorMap,
		ValidatorSetHash: func() string {
			if next := strings.TrimSpace(block.NextValidatorSetHash); next != "" {
				return next
			}
			return strings.TrimSpace(block.ValidatorSetHash)
		}(),
	}
	populateSnapshotDerivedFields(&snapshot)
	storeSnapshotForHeight(t, node.DB, snapshot)
}

func TestCurrentExecutionLedgerCloneRejectsRootMismatchedSnapshot(t *testing.T) {
	node, block, genesisLedger, _ := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})

	wrongLedger := genesisLedger.Clone()
	addBalance(&wrongLedger, CoinSymbol, "stale-snapshot-wallet", 99)
	if got := ComputeExecHashVersioned(block, HashLedger(wrongLedger), executionStateRootVersionForHeight(block.ID)); got == block.StateRoot {
		t.Fatalf("test setup did not create a mismatched snapshot ledger")
	}
	node.cacheExecutionSnapshotLedger(block.ID, wrongLedger)
	node.ExecutionLedger = Ledger{}

	correctPostCommit := node.replayPostBlockEffectsToLedger(block, genesisLedger)
	node.Ledger = correctPostCommit.Clone()

	if _, ok := node.committedTipLedgerFromExecutionSnapshot(block.ID); ok {
		t.Fatalf("mismatched execution snapshot was accepted as post-commit ledger")
	}
	if node.restoreLedgersFromAuthoritativeExecution(block.ID, "test") {
		t.Fatalf("mismatched execution snapshot restored authoritative runtime ledger")
	}
	got := node.currentExecutionLedgerClone()
	if HashLedger(got) == HashLedger(wrongLedger) {
		t.Fatalf("current execution ledger used mismatched cached execution snapshot")
	}
	if HashLedger(got) != HashLedger(correctPostCommit) {
		t.Fatalf("current execution ledger = %s, want runtime fallback %s", ShortHash(HashLedger(got)), ShortHash(HashLedger(correctPostCommit)))
	}
}

func TestRestoreLedgersEvictsBadExecutionCacheAndUsesTrustedSnapshot(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, parentExecutionLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	storeExecutionSnapshotForTest(t, node, parent, parentExecutionLedger, SnapshotVersion, snapshotLedgerStageExecution)

	badExecutionCache := parentExecutionLedger.Clone()
	addBalance(&badExecutionCache, CoinSymbol, "bad-cache-wallet", 99)
	if got := ComputeExecHashVersioned(parent, HashLedger(badExecutionCache), executionStateRootVersionForHeight(parent.ID)); got == parent.StateRoot {
		t.Fatalf("test setup did not create mismatched execution cache")
	}
	node.cacheExecutionSnapshotLedger(parent.ID, badExecutionCache)
	node.cachePostCommitLedger(parent.ID, badExecutionCache)
	node.setExecutionLedger(badExecutionCache)

	if !node.restoreLedgersFromAuthoritativeExecution(parent.ID, "test_bad_cache") {
		t.Fatalf("expected restore to evict bad cache and use trusted snapshot")
	}

	wantPostCommit := node.replayPostBlockEffectsToLedger(parent, parentExecutionLedger)
	if got, want := HashLedger(node.currentExecutionLedgerClone()), HashLedger(wantPostCommit); got != want {
		t.Fatalf("restored execution ledger mismatch: got=%s want=%s", got, want)
	}
	if cached, ok := node.cachedExecutionSnapshotLedger(parent.ID); !ok || HashLedger(cached) != HashLedger(parentExecutionLedger) {
		t.Fatalf("expected good execution snapshot cached after restore")
	}
	if cachedPost, ok := node.cachedPostCommitLedger(parent.ID); !ok || HashLedger(cachedPost) != HashLedger(wantPostCommit) {
		t.Fatalf("expected good post-commit ledger cached after restore")
	}
}

func TestVerifyBlockRepairsStalePostCommitLedgerFromParentExecutionSnapshot(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})
	node, parent, parentExecutionLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash
	node.Role = "full"

	setHash := strings.TrimSpace(parent.NextValidatorSetHash)
	if setHash == "" {
		setHash = strings.TrimSpace(parent.ValidatorSetHash)
	}
	proposer := node.consensusLeaderForHeightRound(parent.ID+1, 0, validators)
	if proposer == "" {
		proposer = validators[0]
	}
	block := Block{
		ID:                   parent.ID + 1,
		Type:                 BlockTypeTime,
		PrevHash:             parent.BlockHash,
		Proposer:             proposer,
		Signatures:           append([]string{}, validators...),
		ValidatorSetHash:     setHash,
		NextValidatorSetHash: setHash,
		BlockTime:            LogicalTimeForEpochTick(parent.ID+1, TickFinalize),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	node.fillBlockNextValidatorSetCommitment(&block)

	correctParentPostCommit := node.replayPostBlockEffectsToLedger(parent, parentExecutionLedger)
	staleParentPostCommit := correctParentPostCommit.Clone()
	addBalance(&staleParentPostCommit, CoinSymbol, "stale-post-commit-wallet", 77)
	if HashLedger(staleParentPostCommit) == HashLedger(correctParentPostCommit) {
		t.Fatalf("test setup did not create stale post-commit ledger")
	}

	node.cacheExecutionSnapshotLedger(parent.ID, parentExecutionLedger)
	node.cachePostCommitLedger(parent.ID, staleParentPostCommit)
	node.setExecutionLedger(staleParentPostCommit)

	block.StateRoot = ComputeExecHashVersioned(block, HashLedger(correctParentPostCommit), executionStateRootVersionForHeight(block.ID))
	block.BlockHash = HashBlock(block)

	if err := node.VerifyBlock(block, node.Blockchain); err != nil {
		t.Fatalf("VerifyBlock should repair stale parent post-commit ledger, got %v", err)
	}
}

func TestCurrentExecutionLedgerCloneReplaysTipPostCommitAfterRestart(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})
	node, parent, parentExecutionLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	correctParentPostCommit := node.replayPostBlockEffectsToLedger(parent, parentExecutionLedger)
	staleTipLedger := parentExecutionLedger.Clone()
	addBalance(&staleTipLedger, CoinSymbol, "stale-restart-ledger", 99)

	node.cacheExecutionSnapshotLedger(parent.ID, parentExecutionLedger)
	node.ExecutionLedger = staleTipLedger.Clone()
	node.Ledger = staleTipLedger.Clone()

	got := node.currentExecutionLedgerClone()
	if gotHash, want := HashLedger(got), HashLedger(correctParentPostCommit); gotHash != want {
		t.Fatalf("current execution ledger should replay committed tip post-effects after restart: got=%s want=%s", gotHash, want)
	}
}

func TestPostBlockEffectsUseCommittedExecutionSnapshotOnce(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	oldWorkReward := WorkBlockRewardEnabled
	oldWorkBase := WorkBlockBaseReward
	oldEmissionEnabled := EmissionRewardEnabled
	oldEmissionMin := EmissionMinReward
	oldEmissionMax := EmissionMaxReward
	oldEmissionBaseChance := EmissionBaseChanceBPS
	oldEmissionTreasury := EmissionTreasuryBPS
	oldEmissionValidator := EmissionValidatorBPS
	oldEmissionBurn := EmissionBurnBPS
	oldUnified := UnifiedTeamRewardEnabled
	oldUnifiedTreasury := UnifiedTeamRewardTreasuryBPS
	oldUnifiedProposer := UnifiedTeamRewardProposerBPS
	oldUnifiedValidator := UnifiedTeamRewardValidatorBPS
	t.Cleanup(func() {
		WorkBlockRewardEnabled = oldWorkReward
		WorkBlockBaseReward = oldWorkBase
		EmissionRewardEnabled = oldEmissionEnabled
		EmissionMinReward = oldEmissionMin
		EmissionMaxReward = oldEmissionMax
		EmissionBaseChanceBPS = oldEmissionBaseChance
		EmissionTreasuryBPS = oldEmissionTreasury
		EmissionValidatorBPS = oldEmissionValidator
		EmissionBurnBPS = oldEmissionBurn
		UnifiedTeamRewardEnabled = oldUnified
		UnifiedTeamRewardTreasuryBPS = oldUnifiedTreasury
		UnifiedTeamRewardProposerBPS = oldUnifiedProposer
		UnifiedTeamRewardValidatorBPS = oldUnifiedValidator
	})
	WorkBlockRewardEnabled = true
	WorkBlockBaseReward = 8
	EmissionRewardEnabled = true
	EmissionMinReward = 4
	EmissionMaxReward = 4
	EmissionBaseChanceBPS = 10000
	EmissionTreasuryBPS = 2500
	EmissionValidatorBPS = 7500
	EmissionBurnBPS = 0
	UnifiedTeamRewardEnabled = true
	UnifiedTeamRewardTreasuryBPS = 2500
	UnifiedTeamRewardProposerBPS = 2500
	UnifiedTeamRewardValidatorBPS = 5000

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.Role = "validator"
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	if err := node.ReceiveBlock(block, node.Blockchain); err != nil {
		t.Fatalf("receive block: %v", err)
	}
	executionLedger, ok := node.cachedExecutionSnapshotLedger(block.ID)
	if !ok {
		t.Fatalf("expected committed execution snapshot for height %d", block.ID)
	}
	singlePostCommit := node.replayPostBlockEffectsToLedger(block, executionLedger)
	doublePostCommit := node.replayPostBlockEffectsToLedger(block, singlePostCommit)
	if HashLedger(singlePostCommit) == HashLedger(doublePostCommit) {
		t.Fatalf("test setup invalid: post-block effects did not change ledger")
	}

	cachedPostCommit, ok := node.cachedPostCommitLedger(block.ID)
	if !ok {
		t.Fatalf("expected post-commit cache for height %d", block.ID)
	}
	if got, want := HashLedger(cachedPostCommit), HashLedger(singlePostCommit); got != want {
		t.Fatalf("post-commit cache applied effects more than once: got=%s want=%s", ShortHash(got), ShortHash(want))
	}
	if got, bad := HashLedger(cachedPostCommit), HashLedger(doublePostCommit); got == bad {
		t.Fatalf("post-commit cache contains double-applied post-block effects")
	}
	if got, want := HashLedger(node.currentExecutionLedgerClone()), HashLedger(singlePostCommit); got != want {
		t.Fatalf("current execution ledger mismatch after commit: got=%s want=%s", ShortHash(got), ShortHash(want))
	}
}

func TestVerifyBlockAcceptsLegacyExecutionSnapshotParentWhenPostCommitParentWouldMismatch(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldResultGossipOnly := ResultGossipOnly
	oldValidatorSetCommitmentV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ResultGossipOnly = oldResultGossipOnly
		ValidatorSetCommitmentV2Height = oldValidatorSetCommitmentV2Height
	})
	ResultGossipOnly = false
	ValidatorSetCommitmentV2Height = ^uint64(0)
	node, parent, parentExecutionLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash
	node.Role = "full"

	postCommitParent := parentExecutionLedger.Clone()
	addBalance(&postCommitParent, CoinSymbol, "post-commit-only", 123)
	if HashLedger(postCommitParent) == HashLedger(parentExecutionLedger) {
		t.Fatalf("test setup invalid: post-commit parent did not diverge")
	}

	setHash := strings.TrimSpace(parent.NextValidatorSetHash)
	if setHash == "" {
		setHash = strings.TrimSpace(parent.ValidatorSetHash)
	}
	proposer := node.consensusLeaderForHeightRound(parent.ID+1, 0, validators)
	if proposer == "" {
		proposer = validators[0]
	}
	block := Block{
		ID:                   parent.ID + 1,
		Type:                 BlockTypeTime,
		PrevHash:             parent.BlockHash,
		Proposer:             proposer,
		Signatures:           append([]string{}, validators...),
		ValidatorSetHash:     setHash,
		NextValidatorSetHash: setHash,
		BlockTime:            LogicalTimeForEpochTick(parent.ID+1, TickFinalize),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	node.fillBlockNextValidatorSetCommitment(&block)
	block.StateRoot = ComputeExecHashVersioned(block, HashLedger(parentExecutionLedger), executionStateRootVersionForHeight(block.ID))
	block.BlockHash = HashBlock(block)

	node.validatorSetMu.Lock()
	if node.frozenValidatorsByHeight == nil {
		node.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if node.frozenValidatorHashByHeight == nil {
		node.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	if node.committeeHashByHeight == nil {
		node.committeeHashByHeight = make(map[uint64]string)
	}
	node.frozenValidatorsByHeight[block.ID] = append([]string{}, validators...)
	node.frozenValidatorHashByHeight[block.ID] = ValidatorSetHash(validators)
	node.committeeHashByHeight[block.ID] = ValidatorSetHash(validators)
	node.validatorSetMu.Unlock()

	node.cacheExecutionSnapshotLedger(parent.ID, parentExecutionLedger)
	node.cachePostCommitLedger(parent.ID, postCommitParent)
	node.setExecutionLedger(postCommitParent)

	if err := node.VerifyBlock(block, node.Blockchain); err != nil {
		t.Fatalf("VerifyBlock should accept quorum-verified legacy execution parent, got %v", err)
	}
	appliedLedger, _, ok := node.executionLedgerForBlock(block)
	if !ok {
		t.Fatalf("execution ledger should be available after legacy parent verification")
	}
	gotRoot := ComputeExecHashVersioned(block, HashLedger(appliedLedger), executionStateRootVersionForHeight(block.ID))
	if gotRoot != block.StateRoot {
		t.Fatalf("ProcessBlock parent override not preserved: got root=%s want=%s", gotRoot, block.StateRoot)
	}
}

func captureStartupExecutionStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out)
}

func TestExecutionParentLedgerRejectsLegacySnapshotForExecutionUse(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})
	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	storeExecutionSnapshotForTest(t, node, parent, execLedger, 3, "")

	runtimeLedger := execLedger.Clone()
	addBalance(&runtimeLedger, CoinSymbol, TREASURY_ADDRESS, 77)
	node.Ledger = runtimeLedger

	block := Block{
		ID:        2,
		Type:      BlockTypeTime,
		PrevHash:  parent.BlockHash,
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(2),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	if got := node.ExecuteBlockAndGetStateRoot(block); got != "" {
		t.Fatalf("expected legacy snapshot to be rejected for execution use, got=%q", got)
	}
}

func TestExecutionParentLedgerUsesTrustedPersistedSnapshotAfterRestart(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})
	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	storeExecutionSnapshotForTest(t, node, parent, execLedger, SnapshotVersion, snapshotLedgerStageExecution)

	runtimeLedger := execLedger.Clone()
	addBalance(&runtimeLedger, CoinSymbol, TREASURY_ADDRESS, 99)
	node.Ledger = runtimeLedger

	block := Block{
		ID:        2,
		Type:      BlockTypeTime,
		PrevHash:  parent.BlockHash,
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(2),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	got := node.ExecuteBlockAndGetStateRoot(block)
	want := ComputeExecHash(block, HashLedger(execLedger))
	runtimeRoot := ComputeExecHash(block, HashLedger(runtimeLedger))
	if got != want {
		t.Fatalf("trusted persisted snapshot root mismatch: got=%q want=%q", got, want)
	}
	if got == runtimeRoot {
		t.Fatalf("execution root incorrectly used runtime ledger after restart")
	}
}

func TestStartupExecutionSnapshotRebuildKeepsGateClosedUntilTrustedSnapshotExists(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetCommitmentV2Height = oldV2Height
	})
	ValidatorSetCommitmentV2Height = 1

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); ok {
		t.Fatalf("expected trusted snapshot storage to be empty before rebuild")
	}

	ok, reason := node.startupValidatorSetSelfCheck()
	if ok {
		t.Fatalf("expected startup gate to remain closed while execution snapshots rebuild")
	}
	if want := "startup_execution_snapshot_rebuilt_h_1"; reason != want {
		t.Fatalf("unexpected rebuild reason: got=%q want=%q", reason, want)
	}

	snapshot, err := node.GetSnapshot(parent.ID)
	if err != nil {
		t.Fatalf("get rebuilt snapshot: %v", err)
	}
	if !snapshotHasTrustedExecutionLedger(snapshot) {
		t.Fatalf("expected rebuilt snapshot to be marked as trusted execution snapshot")
	}
	if got, want := snapshot.LedgerHash, HashLedger(execLedger); got != want {
		t.Fatalf("expected rebuilt snapshot ledger to match execution replay: got=%q want=%q", got, want)
	}

	ok, reason = node.startupValidatorSetSelfCheck()
	if !ok || reason != "ready" {
		t.Fatalf("expected startup gate to open after rebuild, got ok=%v reason=%q", ok, reason)
	}
}

func TestEnsureStartupTrustedExecutionSnapshotsUsesAuthoritativeLiveCache(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	node.cacheExecutionSnapshotLedger(parent.ID, execLedger)
	runtimeLedger := execLedger.Clone()
	addBalance(&runtimeLedger, CoinSymbol, TREASURY_ADDRESS, 55)
	node.Ledger = runtimeLedger

	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); ok {
		t.Fatalf("expected trusted snapshot storage to be empty before readiness check")
	}

	ok, reason := node.ensureStartupTrustedExecutionSnapshots(parent.ID)
	if !ok || reason != "ready" {
		t.Fatalf("expected authoritative live cache to satisfy startup readiness, got ok=%v reason=%q", ok, reason)
	}
	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); ok {
		t.Fatalf("startup readiness should not rebuild/persist when authoritative live cache already exists")
	}
	if got := node.executionSnapshotRebuildReadyHeight; got != parent.ID {
		t.Fatalf("unexpected ready height: got=%d want=%d", got, parent.ID)
	}
}

func TestEnsureStartupTrustedExecutionSnapshotsUsesCurrentProcessLiveCommitMarker(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, _, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); ok {
		t.Fatalf("expected trusted snapshot storage to be empty before readiness check")
	}

	node.markLiveCommittedExecutionSnapshotReadyHeight(parent.ID)
	ok, reason := node.ensureStartupTrustedExecutionSnapshots(parent.ID)
	if !ok || reason != "ready" {
		t.Fatalf("expected current-process live commit marker to satisfy readiness, got ok=%v reason=%q", ok, reason)
	}
	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); ok {
		t.Fatalf("live commit readiness should not rebuild a persistent startup snapshot")
	}
}

func TestRebuildTrustedExecutionSnapshotsPreservesLiveCache(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	liveCacheLedger := execLedger.Clone()
	addBalance(&liveCacheLedger, CoinSymbol, TREASURY_ADDRESS, 777)
	node.cacheExecutionSnapshotLedger(parent.ID, liveCacheLedger)

	if err := node.rebuildTrustedExecutionSnapshotsUpTo(parent.ID); err != nil {
		t.Fatalf("rebuild trusted snapshots: %v", err)
	}

	cached, ok := node.cachedExecutionSnapshotLedger(parent.ID)
	if !ok {
		t.Fatalf("expected live execution cache entry to remain available")
	}
	if got, want := HashLedger(cached), HashLedger(liveCacheLedger); got != want {
		t.Fatalf("live cache was overwritten by rebuild: got=%q want=%q", got, want)
	}

	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID)
	if !ok || snapshot == nil {
		t.Fatalf("expected trusted execution snapshot to be rebuilt in storage")
	}
	if got, want := snapshot.LedgerHash, HashLedger(execLedger); got != want {
		t.Fatalf("rebuilt snapshot ledger mismatch: got=%q want=%q", got, want)
	}
	if snapshot.LedgerHash == HashLedger(liveCacheLedger) {
		t.Fatalf("rebuild incorrectly used the live in-memory cache ledger")
	}
}

func TestTrustedExecutionSnapshotRejectsLedgerStateRootMismatch(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	badLedger := execLedger.Clone()
	addBalance(&badLedger, CoinSymbol, TREASURY_ADDRESS, 999)
	storeExecutionSnapshotForTest(t, node, parent, badLedger, SnapshotVersion, snapshotLedgerStageExecution)

	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); ok {
		t.Fatalf("expected trusted snapshot with ledger/state-root mismatch to be rejected")
	}

	node.ExecutionLedger = Ledger{}
	node.Ledger = NewLedger()
	addBalance(&node.Ledger, CoinSymbol, "runtime-mismatch-wallet", 1)

	ok, reason := node.ensureStartupTrustedExecutionSnapshots(parent.ID)
	if ok {
		if reason != "ready" {
			t.Fatalf("unexpected ready reason: got=%q", reason)
		}
	} else if want := "startup_execution_snapshot_rebuilt_h_1"; reason != want {
		t.Fatalf("unexpected rebuild reason: got=%q want=%q", reason, want)
	}

	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID)
	if ok && snapshot != nil {
		if got, want := snapshot.LedgerHash, HashLedger(execLedger); got != want {
			t.Fatalf("rebuilt trusted snapshot ledger mismatch: got=%q want=%q", got, want)
		}
		return
	}
	if !ok && reason == "startup_execution_snapshot_rebuilt_h_1" {
		t.Fatalf("expected rebuilt trusted execution snapshot to be available")
	}
	if _, _, badOK := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID); badOK {
		t.Fatalf("mismatched trusted snapshot became available after readiness check")
	}
}

func TestResolveTrustedExecutionSnapshotPromotesValidCommittedSnapshot(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	storeExecutionSnapshotForTest(t, node, parent, execLedger, SnapshotVersion, "")

	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(parent.ID)
	if !ok || snapshot == nil {
		t.Fatalf("expected valid committed snapshot to be promoted to trusted execution snapshot")
	}
	if !snapshotHasTrustedExecutionLedger(snapshot) {
		t.Fatalf("expected promoted snapshot to be marked as trusted execution snapshot")
	}
	loaded, err := node.GetSnapshot(parent.ID)
	if err != nil {
		t.Fatalf("get promoted snapshot: %v", err)
	}
	if !snapshotHasTrustedExecutionLedger(loaded) {
		t.Fatalf("expected stored snapshot to persist trusted execution ledger stage after promotion")
	}
}

func TestEnsureStartupTrustedExecutionSnapshotsRecoversAfterPriorFailureFromCommittedSnapshot(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	storeExecutionSnapshotForTest(t, node, parent, execLedger, SnapshotVersion, "")
	node.executionSnapshotRebuildFailedHeight = parent.ID
	node.executionSnapshotRebuildLastErr = "historical_replay_failed"

	ok, reason := node.ensureStartupTrustedExecutionSnapshots(parent.ID)
	if !ok || reason != "ready" {
		t.Fatalf("expected committed snapshot recovery after prior failure, got ok=%v reason=%q", ok, reason)
	}
	if got := node.executionSnapshotRebuildFailedHeight; got != 0 {
		t.Fatalf("expected rebuild failed height to be cleared, got=%d", got)
	}
	if got := node.executionSnapshotRebuildLastErr; got != "" {
		t.Fatalf("expected rebuild error to be cleared, got=%q", got)
	}
	if _, ok := node.cachedExecutionSnapshotLedger(parent.ID); !ok {
		t.Fatalf("expected recovered execution snapshot ledger to be cached")
	}
}

func TestEnsureStartupTrustedExecutionSnapshotsPrefersLocalRebuildWhenHistoryAvailable(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldSeeds := append([]string{}, ConfigSeeds...)
	oldPersistentPeers := append([]string{}, ConfigPersistentPeers...)
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ConfigSeeds = oldSeeds
		ConfigPersistentPeers = oldPersistentPeers
	})

	ConfigSeeds = []string{"/ip4/127.0.0.1/tcp/7001/p2p/test-peer"}
	ConfigPersistentPeers = nil

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	badRuntime := execLedger.Clone()
	addBalance(&badRuntime, CoinSymbol, TREASURY_ADDRESS, 55)
	node.Ledger = badRuntime
	node.ExecutionLedger = Ledger{}

	ok, reason := node.ensureStartupTrustedExecutionSnapshots(parent.ID)
	if ok {
		t.Fatalf("expected startup execution snapshot recovery to rebuild before becoming ready")
	}
	if want := "startup_execution_snapshot_rebuilt_h_1"; reason != want {
		t.Fatalf("unexpected rebuild reason: got=%q want=%q", reason, want)
	}
	if got := node.executionSnapshotRebuildFailedHeight; got != 0 {
		t.Fatalf("expected no rebuild failure to be recorded while rebuilding locally, got=%d", got)
	}
}

func TestEnsureStartupTrustedExecutionSnapshotsWaitsForPeersWhenLocalHistoryIncomplete(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldSeeds := append([]string{}, ConfigSeeds...)
	oldPersistentPeers := append([]string{}, ConfigPersistentPeers...)
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ConfigSeeds = oldSeeds
		ConfigPersistentPeers = oldPersistentPeers
	})

	ConfigSeeds = []string{"/ip4/127.0.0.1/tcp/7001/p2p/test-peer"}
	ConfigPersistentPeers = nil

	validators := []string{"A", "B", "C", "D"}
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	block2, _ := appendStartupExecutionSnapshotBlock(t, node, block1, genesisLedger, validators[1])
	node.DB = nil
	node.DataDir = t.TempDir()
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{block2}
	node.Blockchain.mu.Unlock()

	badRuntime := genesisLedger.Clone()
	addBalance(&badRuntime, CoinSymbol, TREASURY_ADDRESS, 77)
	node.Ledger = badRuntime
	node.ExecutionLedger = Ledger{}

	ok, reason := node.ensureStartupTrustedExecutionSnapshots(block2.ID)
	if ok {
		t.Fatalf("expected startup execution snapshot recovery to wait for peers when local history is incomplete")
	}
	if want := startupExecutionSnapshotWaitingReason(block2.ID); reason != want {
		t.Fatalf("unexpected wait reason: got=%q want=%q", reason, want)
	}
	if got := node.executionSnapshotRebuildFailedHeight; got != 0 {
		t.Fatalf("expected no rebuild failure while waiting for peer recovery, got=%d", got)
	}
}

func TestRuntimeStatusSeparatesExecutionReadyFromSyncComplete(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldSeeds := append([]string{}, ConfigSeeds...)
	oldPersistentPeers := append([]string{}, ConfigPersistentPeers...)
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ConfigSeeds = oldSeeds
		ConfigPersistentPeers = oldPersistentPeers
	})

	ConfigSeeds = []string{"/ip4/127.0.0.1/tcp/7001/p2p/test-peer"}
	ConfigPersistentPeers = nil

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	badRuntime := execLedger.Clone()
	addBalance(&badRuntime, CoinSymbol, TREASURY_ADDRESS, 66)
	node.Ledger = badRuntime
	node.ExecutionLedger = Ledger{}

	runtime := node.runtimeStatusSnapshot()
	if !runtime.SyncComplete {
		t.Fatalf("expected sync to be complete for local tip")
	}
	if runtime.ExecutionReady {
		t.Fatalf("expected execution readiness to remain false while waiting for trusted startup snapshot recovery")
	}
	if want := startupExecutionSnapshotRebuildPendingReason(parent.ID); runtime.ExecutionWaitReason != want {
		t.Fatalf("unexpected execution wait reason: got=%q want=%q", runtime.ExecutionWaitReason, want)
	}
}

func TestStartupExecutionSnapshotCanRebuildLocallyRequiresFullHistory(t *testing.T) {
	node, parent, _, _ := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})

	block3 := Block{
		ID:        3,
		Type:      BlockTypeTime,
		PrevHash:  "missing-block-2",
		BlockHash: "block-3",
		Proposer:  "B",
		BlockTime: LogicalTimeForEpoch(3),
	}
	block3.Timestamp = int64(SystemTimeUnits(block3.BlockTime))
	block3.StateRoot = parent.StateRoot
	node.Blockchain.AddBlock(block3)

	if node.startupExecutionSnapshotHasFullHistory(3) {
		t.Fatalf("expected full history check to fail when block 2 is missing")
	}
	if node.startupExecutionSnapshotCanRebuildLocally(3) {
		t.Fatalf("expected local rebuild to be blocked without full history")
	}
}

func TestStartupExecutionSnapshotRebuildBootstrapsHistoricalRegistrySnapshots(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetCommitmentV2Height = oldV2Height
	})
	ValidatorSetCommitmentV2Height = 1

	validators := []string{"A", "B", "C", "D"}
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	_, _ = appendStartupExecutionSnapshotBlock(t, node, block1, ledgerAfter1, validators[1])

	if ok, reason := node.ensureStartupTrustedExecutionSnapshots(2); ok || reason != "startup_execution_snapshot_rebuilt_h_2" {
		t.Fatalf("expected rebuild-first response for height 2, got ok=%v reason=%q", ok, reason)
	}
	if _, err := node.loadValidatorRegistrySnapshot(2); err != nil {
		t.Fatalf("expected rebuilt registry snapshot for height 2: %v", err)
	}
	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(2); !ok {
		t.Fatalf("expected trusted execution snapshot for height 2 after rebuild")
	}
	if ok, reason := node.ensureStartupTrustedExecutionSnapshots(2); !ok || reason != "ready" {
		t.Fatalf("expected ready after height 2 rebuild, got ok=%v reason=%q", ok, reason)
	}
}

func TestRebuildTrustedExecutionSnapshotsUsesPostCommitLedgerBetweenBlocks(t *testing.T) {
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
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNodeWithGenesis(t, validators, func(genesis *Genesis) {
		genesis.Balances = map[string]int{
			TREASURY_ADDRESS: 1000,
		}
	})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	postCommitLedger1 := node.applyPostBlockEffectsToLedger(block1, ledgerAfter1)
	if got, wantNot := HashLedger(postCommitLedger1), HashLedger(ledgerAfter1); got == wantNot {
		t.Fatalf("test setup ineffective: post-commit effects did not change parent ledger")
	}

	block2, _ := appendStartupExecutionSnapshotBlock(t, node, block1, postCommitLedger1, validators[1])
	if err := node.rebuildTrustedExecutionSnapshotsUpTo(block2.ID); err != nil {
		t.Fatalf("rebuild trusted snapshots with post-commit parent: %v", err)
	}
	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block2.ID)
	if !ok || snapshot == nil {
		t.Fatalf("expected trusted execution snapshot for height %d", block2.ID)
	}
	if got, want := snapshot.LedgerHash, HashLedger(postCommitLedger1); got != want {
		t.Fatalf("height 2 snapshot should use post-commit parent ledger: got=%q want=%q", got, want)
	}
}

func TestCurrentExecutionLedgerCloneReplaysPostEffectsWithoutMintedDrift(t *testing.T) {
	oldEmissionEnabled := EmissionRewardEnabled
	oldEmissionMin := EmissionMinReward
	oldEmissionMax := EmissionMaxReward
	oldEmissionBaseChance := EmissionBaseChanceBPS
	oldEmissionHighChance := EmissionHighChanceBPS
	oldEmissionValidatorToProposer := EmissionValidatorToProposer
	oldBurnStopSupply := BurnStopSupply
	oldMinted := TotalMintedMSC
	t.Cleanup(func() {
		EmissionRewardEnabled = oldEmissionEnabled
		EmissionMinReward = oldEmissionMin
		EmissionMaxReward = oldEmissionMax
		EmissionBaseChanceBPS = oldEmissionBaseChance
		EmissionHighChanceBPS = oldEmissionHighChance
		EmissionValidatorToProposer = oldEmissionValidatorToProposer
		BurnStopSupply = oldBurnStopSupply
		TotalMintedMSC = oldMinted
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
	node, block1, genesisLedger, _ := setupStartupExecutionSnapshotNodeWithGenesis(t, validators, func(genesis *Genesis) {
		genesis.Balances = map[string]int{
			TREASURY_ADDRESS: 1000,
		}
	})
	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	node.cacheExecutionSnapshotLedger(block1.ID, ledgerAfter1)
	node.ExecutionLedger = Ledger{}
	node.Ledger = NewLedger()

	before := TotalMintedMSC
	got1 := node.currentExecutionLedgerClone()
	afterFirst := TotalMintedMSC
	got2 := node.currentExecutionLedgerClone()
	afterSecond := TotalMintedMSC

	if afterFirst != before || afterSecond != before {
		t.Fatalf("currentExecutionLedgerClone mutated total minted: before=%d after_first=%d after_second=%d", before, afterFirst, afterSecond)
	}
	if got1Hash, got2Hash := HashLedger(got1), HashLedger(got2); got1Hash != got2Hash {
		t.Fatalf("cached post-commit ledger changed across reads: first=%s second=%s", got1Hash, got2Hash)
	}
	if got, raw := HashLedger(got1), HashLedger(ledgerAfter1); got == raw {
		t.Fatalf("expected committed-tip ledger to include post-block effects, got raw execution snapshot %s", got)
	}
}

func TestRebuildTrustedExecutionSnapshotsResumesFromTrustedBase(t *testing.T) {
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
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNodeWithGenesis(t, validators, func(genesis *Genesis) {
		genesis.Balances = map[string]int{
			TREASURY_ADDRESS: 1000,
		}
	})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	storeExecutionSnapshotForTest(t, node, block1, ledgerAfter1, SnapshotVersion, snapshotLedgerStageExecution)
	postCommitLedger1 := node.applyPostBlockEffectsToLedger(block1, ledgerAfter1)
	block2, _ := appendStartupExecutionSnapshotBlock(t, node, block1, postCommitLedger1, validators[1])

	if err := os.WriteFile(resolveGenesisPath(node.DataDir), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("poison genesis: %v", err)
	}
	if err := node.rebuildTrustedExecutionSnapshotsUpTo(block2.ID); err != nil {
		t.Fatalf("rebuild should resume from trusted base without reading genesis: %v", err)
	}
	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block2.ID)
	if !ok || snapshot == nil {
		t.Fatalf("expected trusted execution snapshot for height %d", block2.ID)
	}
	if got, want := snapshot.LedgerHash, HashLedger(postCommitLedger1); got != want {
		t.Fatalf("height 2 snapshot should resume from base post-commit ledger: got=%q want=%q", got, want)
	}
}

func TestRebuildTrustedExecutionSnapshotsResumesFromLegacyRawParentBase(t *testing.T) {
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
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNodeWithGenesis(t, validators, func(genesis *Genesis) {
		genesis.Balances = map[string]int{
			TREASURY_ADDRESS: 1000,
		}
	})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	storeExecutionSnapshotForTest(t, node, block1, ledgerAfter1, SnapshotVersion, snapshotLedgerStageExecution)
	postCommitLedger1 := node.applyPostBlockEffectsToLedger(block1, ledgerAfter1)
	if got, wantNot := HashLedger(postCommitLedger1), HashLedger(ledgerAfter1); got == wantNot {
		t.Fatalf("test setup ineffective: post-commit effects did not change parent ledger")
	}
	block2, _ := appendStartupExecutionSnapshotBlock(t, node, block1, ledgerAfter1, validators[1])

	if err := os.WriteFile(resolveGenesisPath(node.DataDir), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("poison genesis: %v", err)
	}
	if err := node.rebuildTrustedExecutionSnapshotsUpTo(block2.ID); err != nil {
		t.Fatalf("rebuild should resume from legacy raw-parent base: %v", err)
	}
	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block2.ID)
	if !ok || snapshot == nil {
		t.Fatalf("expected trusted execution snapshot for height %d", block2.ID)
	}
	if got, want := snapshot.LedgerHash, HashLedger(ledgerAfter1); got != want {
		t.Fatalf("height 2 snapshot should use legacy raw parent ledger: got=%q want=%q", got, want)
	}
}

func TestRebuildTrustedExecutionSnapshotsSkipsEarlyEmptyLegacyRootMismatch(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	validators := []string{"A", "B", "C", "D"}
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	block2 := Block{
		ID:                   2,
		Type:                 BlockTypeTime,
		PrevHash:             block1.BlockHash,
		Proposer:             validators[1],
		ValidatorSetHash:     block1.ValidatorSetHash,
		NextValidatorSetHash: block1.NextValidatorSetHash,
		BlockTime:            LogicalTimeForEpoch(2),
		StateRoot:            strings.Repeat("a", 64),
	}
	block2.Timestamp = int64(SystemTimeUnits(block2.BlockTime))
	block2.BlockHash = HashBlock(block2)
	node.Blockchain.AddBlock(block2)
	node.commitMu.Lock()
	node.committed[block2.ID] = block2.BlockHash
	node.committedHeight = block2.ID
	node.commitMu.Unlock()

	block3 := Block{
		ID:                   3,
		Type:                 BlockTypeTime,
		PrevHash:             block2.BlockHash,
		Proposer:             validators[2],
		ValidatorSetHash:     block1.ValidatorSetHash,
		NextValidatorSetHash: block1.NextValidatorSetHash,
		BlockTime:            LogicalTimeForEpoch(3),
	}
	block3.Timestamp = int64(SystemTimeUnits(block3.BlockTime))
	block3.StateRoot = ComputeExecHash(block3, HashLedger(ledgerAfter1))
	block3.BlockHash = HashBlock(block3)
	node.Blockchain.AddBlock(block3)
	node.commitMu.Lock()
	node.committed[block3.ID] = block3.BlockHash
	node.committedHeight = block3.ID
	node.commitMu.Unlock()

	if err := node.rebuildTrustedExecutionSnapshotsUpTo(block3.ID); err != nil {
		t.Fatalf("rebuild should skip early empty legacy mismatch and converge: %v", err)
	}
	if _, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block2.ID); ok {
		t.Fatalf("did not expect mismatched legacy height to be promoted as trusted execution snapshot")
	}
	snapshot, _, ok := node.resolveTrustedExecutionSnapshotFromStorage(block3.ID)
	if !ok || snapshot == nil {
		t.Fatalf("expected trusted execution snapshot after replay convergence")
	}
	if got, want := snapshot.LedgerHash, HashLedger(ledgerAfter1); got != want {
		t.Fatalf("converged snapshot ledger mismatch: got=%q want=%q", got, want)
	}
}

func TestSnapshotValidatorListFallsBackToGenesisWhenSignersArePartial(t *testing.T) {
	validatorPubKeysMu.Lock()
	oldGenesisPubKeys := GenesisValidatorPubKeys
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{}
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = oldGenesisPubKeys
		validatorPubKeysMu.Unlock()
	})

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.GenesisValidators = nil
	validatorPubKeysMu.Lock()
	for idx, id := range validators {
		GenesisValidatorPubKeys[id] = strictActivationTestPub(byte(idx + 31))
	}
	validatorPubKeysMu.Unlock()
	targetHash := ValidatorSetHash(validators)
	block := Block{
		ID:                   56,
		ValidatorSetHash:     targetHash,
		NextValidatorSetHash: targetHash,
		Signatures:           []string{"A", "C", "D"},
	}

	got, source, err := node.resolveSnapshotValidatorListWithSource(57, block)
	if err != nil {
		t.Fatalf("resolve snapshot validator list: %v", err)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected source: got=%q", source)
	}
	if strings.Join(got, ",") != strings.Join(validators, ",") {
		t.Fatalf("unexpected validator list: got=%v want=%v", got, validators)
	}
}

func TestReplayValidatorFreezeJournalAcceptsRegistryHash(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV3Height := ValidatorSetHashV3Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetHashV3Height = oldV3Height
	})
	ValidatorSetHashV3Height = 1

	validators := []string{"A", "B", "C", "D"}
	node, _, _, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	registryHash := strings.TrimSpace(node.validatorSetHashFromFinalizedSnapshot(2, validators))
	if registryHash == "" {
		t.Fatalf("expected registry-backed validator-set hash")
	}
	if strings.EqualFold(registryHash, ValidatorSetHash(validators)) {
		t.Fatalf("test setup ineffective: registry hash unexpectedly equals legacy hash")
	}
	if err := appendValidatorFreezeJournal(node.DataDir, node.ID, ValidatorFreezeJournalEntry{
		Height:           2,
		ValidatorSetHash: registryHash,
		Validators:       validators,
	}); err != nil {
		t.Fatalf("append freeze journal: %v", err)
	}

	node.validatorSetMu.Lock()
	node.frozenValidatorsByHeight = make(map[uint64][]string)
	node.frozenValidatorHashByHeight = make(map[uint64]string)
	node.validatorSetMu.Unlock()

	if applied := node.replayValidatorFreezeJournal(); applied != 1 {
		t.Fatalf("expected one replayed registry-hash freeze entry, got %d", applied)
	}
	if got := node.frozenValidatorsForHeight(2); strings.Join(got, ",") != strings.Join(validators, ",") {
		t.Fatalf("unexpected replayed validators: got=%v want=%v", got, validators)
	}
	if got, ok := node.frozenValidatorSetHash(2); !ok || !strings.EqualFold(got, registryHash) {
		t.Fatalf("unexpected replayed hash: got=%q ok=%v want=%q", got, ok, registryHash)
	}
}

func TestSyncFrozenValidatorSetHashesCarriesRegistryHashHistory(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV3Height := ValidatorSetHashV3Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetHashV3Height = oldV3Height
	})
	ValidatorSetHashV3Height = 1

	validators := []string{"A", "B", "C", "D"}
	node, block1, _, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	registryHash := strings.TrimSpace(node.validatorSetHashFromFinalizedSnapshot(2, validators))
	if registryHash == "" {
		t.Fatalf("expected registry-backed validator-set hash")
	}
	prev := block1
	for h := uint64(2); h <= 4; h++ {
		block := Block{
			ID:                   h,
			Type:                 BlockTypeTime,
			PrevHash:             prev.BlockHash,
			BlockHash:            fmt.Sprintf("block-%d", h),
			ValidatorSetHash:     registryHash,
			NextValidatorSetHash: registryHash,
			BlockTime:            LogicalTimeForEpoch(h),
		}
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		node.Blockchain.AddBlock(block)
		prev = block
	}

	node.validatorSetMu.Lock()
	node.frozenValidatorsByHeight = map[uint64][]string{2: canonicalValidatorIDs(validators)}
	node.frozenValidatorHashByHeight = map[uint64]string{2: registryHash}
	node.validatorSetMu.Unlock()

	node.syncFrozenValidatorSetHashesFromChain()
	for h := uint64(2); h <= 4; h++ {
		if got := node.frozenValidatorsForHeight(h); strings.Join(got, ",") != strings.Join(validators, ",") {
			t.Fatalf("height %d validators not carried: got=%v want=%v", h, got, validators)
		}
		if got, ok := node.frozenValidatorSetHash(h); !ok || !strings.EqualFold(got, registryHash) {
			t.Fatalf("height %d hash not carried: got=%q ok=%v want=%q", h, got, ok, registryHash)
		}
	}
}

func TestSnapshotValidatorListUsesFrozenHashForHistoricalRegistryCommitment(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV3Height := ValidatorSetHashV3Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetHashV3Height = oldV3Height
	})
	ValidatorSetHashV3Height = 1

	validators := []string{"A", "B", "C", "D"}
	node, _, _, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	registryHash := strings.TrimSpace(node.validatorSetHashFromFinalizedSnapshot(2, validators))
	if registryHash == "" {
		t.Fatalf("expected registry-backed validator-set hash")
	}
	node.validatorSetMu.Lock()
	node.frozenValidatorsByHeight = map[uint64][]string{2: canonicalValidatorIDs(validators)}
	node.frozenValidatorHashByHeight = map[uint64]string{2: registryHash}
	node.validatorSetMu.Unlock()

	block := Block{
		ID:                   56,
		ValidatorSetHash:     registryHash,
		NextValidatorSetHash: registryHash,
		Signatures:           []string{"A", "C", "D"},
	}

	got, source, err := node.resolveSnapshotValidatorListWithSource(57, block)
	if err != nil {
		t.Fatalf("resolve snapshot validator list: %v", err)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected source: got=%q", source)
	}
	if strings.Join(got, ",") != strings.Join(validators, ",") {
		t.Fatalf("unexpected validator list: got=%v want=%v", got, validators)
	}
}

func TestValidatorRegistrySnapshotForHeightReadsStoredHistoricalSnapshotDuringDefer(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	registry := map[string]ValidatorRecord{
		"A": {
			ID:              "A",
			ConsensusPubKey: strings.Repeat("a", ed25519.PublicKeySize*2),
			Stake:           100,
			Status:          ValidatorActive,
		},
	}
	wantHash := ValidatorRegistrySnapshotHash(registry)
	if err := node.storeValidatorRegistrySnapshotRecord(56, registry); err != nil {
		t.Fatalf("store registry snapshot: %v", err)
	}
	node.Blockchain.AddBlock(Block{ID: 200, BlockHash: "tip-200"})

	got := node.validatorRegistrySnapshotForHeight(57)
	if gotHash := ValidatorRegistrySnapshotHash(got); gotHash != wantHash {
		t.Fatalf("historical stored registry snapshot not loaded: got=%q want=%q", gotHash, wantHash)
	}
}

func TestValidatorRegistrySnapshotForHeightRebuildsHistoricalRegistryFromLocalHistory(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetCommitmentV2Height = oldV2Height
	})
	ValidatorSetCommitmentV2Height = 1

	validators := []string{"A", "B", "C", "D"}
	node, block1, genesisLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash

	ledgerAfter1, err := ApplyBlockStateWithNode(node, genesisLedger, block1)
	if err != nil {
		t.Fatalf("apply block 1: %v", err)
	}
	block2, ledgerAfter2 := appendStartupExecutionSnapshotBlock(t, node, block1, ledgerAfter1, validators[1])
	_, _ = appendStartupExecutionSnapshotBlock(t, node, block2, ledgerAfter2, validators[2])

	if node.registrySnapshotExists(2) {
		t.Fatalf("expected no preexisting registry snapshot at historical height 2")
	}

	got := node.validatorRegistrySnapshotForHeight(3)
	if len(got) == 0 {
		t.Fatalf("expected on-demand historical registry rebuild for height 2 parent")
	}
	if !node.registrySnapshotExists(2) {
		t.Fatalf("expected historical registry rebuild to backfill height 2")
	}
}

func TestShouldSkipStartupFailClosedSnapshotResyncForExecutionSnapshotReasons(t *testing.T) {
	for _, reason := range []string{
		"startup_execution_snapshot_rebuild_failed_h_15",
		"startup_execution_snapshot_syncing_h_15",
		"startup_execution_snapshot_waiting_peers_h_15",
	} {
		if !shouldSkipStartupFailClosedSnapshotResync(reason) {
			t.Fatalf("expected startup fail-closed snapshot resync to skip reason %q", reason)
		}
	}
	if shouldSkipStartupFailClosedSnapshotResync("syncing") {
		t.Fatalf("did not expect generic syncing reason to skip fail-closed snapshot resync")
	}
}

func TestSnapshotSyncMinHeightOverrideForStartupExecutionReason(t *testing.T) {
	if got := snapshotSyncMinHeightOverrideForReason("startup_execution_snapshot_missing", 758, 928); got != 1 {
		t.Fatalf("unexpected startup execution min height override: got=%d want=1", got)
	}
	if got := snapshotSyncMinHeightOverrideForReason("execution_snapshot_ledger_unavailable", 758, 928); got != 1 {
		t.Fatalf("unexpected execution ledger unavailable min height override: got=%d want=1", got)
	}
	if got := snapshotSyncMinHeightOverrideForReason("parent_mismatch", 758, 928); got != 759 {
		t.Fatalf("unexpected parent mismatch min height override: got=%d want=759", got)
	}
	if got := snapshotSyncMinHeightOverrideForReason("queue_prevhash_mismatch", 758, 928); got != 759 {
		t.Fatalf("unexpected queue prevhash mismatch min height override: got=%d want=759", got)
	}
	if got := snapshotSyncMinHeightOverrideForReason("validator-set-hash-mismatch", 758, 758); got != 0 {
		t.Fatalf("unexpected generic mismatch min height override: got=%d want=0", got)
	}
}

func TestStartupCommitmentLogsAreThrottledPerTuple(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldV2Height := ValidatorSetCommitmentV2Height
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		ValidatorSetCommitmentV2Height = oldV2Height
		DebugConsensus = oldDebugConsensus
	})
	ValidatorSetCommitmentV2Height = 1
	DebugConsensus = true

	node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, []string{"A", "B", "C", "D"})
	GenesisHash = genesisHash
	GenesisHashExpected = genesisHash
	storeExecutionSnapshotForTest(t, node, parent, execLedger, SnapshotVersion, snapshotLedgerStageExecution)

	output := captureStartupExecutionStdout(t, func() {
		if ok, reason := node.startupValidatorSetSelfCheck(); !ok || reason != "ready" {
			t.Fatalf("first startup check failed: ok=%v reason=%q", ok, reason)
		}
		if ok, reason := node.startupValidatorSetSelfCheck(); !ok || reason != "ready" {
			t.Fatalf("second startup check failed: ok=%v reason=%q", ok, reason)
		}
	})

	if count := strings.Count(output, "[STARTUP-COMMITMENT]"); count != 1 {
		t.Fatalf("expected one throttled startup commitment log, got %d\noutput=%s", count, output)
	}
	if strings.Contains(output, "[STARTUP-COMMITMENT-FALLBACK]") {
		t.Fatalf("unexpected fallback commitment log for stable tuple\noutput=%s", output)
	}
}

func TestRestartExecutionSnapshotsKeepEmptyBlockRootsDeterministicAcrossNodes(t *testing.T) {
	oldGenesisHash := GenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	t.Cleanup(func() {
		GenesisHash = oldGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
	})

	validators := []string{"A", "B", "C", "D"}
	nodes := make([]*Node, 0, 3)
	for _, id := range []string{"A", "B", "C"} {
		node, parent, execLedger, genesisHash := setupStartupExecutionSnapshotNode(t, validators)
		GenesisHash = genesisHash
		GenesisHashExpected = genesisHash
		node.ID = id
		storeExecutionSnapshotForTest(t, node, parent, execLedger, SnapshotVersion, snapshotLedgerStageExecution)
		runtimeLedger := execLedger.Clone()
		addBalance(&runtimeLedger, CoinSymbol, TREASURY_ADDRESS, len(nodes)+1)
		node.Ledger = runtimeLedger
		nodes = append(nodes, node)
	}

	for _, proposer := range []string{"A", "B", "C"} {
		var want string
		for idx, node := range nodes {
			block := Block{
				ID:        2,
				Type:      BlockTypeTime,
				PrevHash:  "block-1",
				Proposer:  proposer,
				BlockTime: LogicalTimeForEpoch(2),
			}
			block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
			got := node.ExecuteBlockAndGetStateRoot(block)
			if got == "" {
				t.Fatalf("node %d returned empty execution root for proposer %s", idx, proposer)
			}
			if idx == 0 {
				want = got
				continue
			}
			if got != want {
				t.Fatalf("determinism mismatch for proposer %s: got=%q want=%q", proposer, got, want)
			}
		}
	}
}
