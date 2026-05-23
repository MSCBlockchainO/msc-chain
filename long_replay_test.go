package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type longReplayAccount struct {
	Address string
	PubKey  ed25519.PublicKey
	PrivKey ed25519.PrivateKey
}

type longReplayFixture struct {
	Accounts        []longReplayAccount
	BaseLedger      Ledger
	Blocks          []Block
	FinalLedger     Ledger
	LedgersByHeight map[uint64]Ledger
	Validators      []string
	Registry        map[string]ValidatorRecord
	TxCount         uint64
	FinalStateRoot  string
	FinalBlockHash  string
}

type longReplayResult struct {
	Height          uint64
	Blocks          uint64
	Transactions    uint64
	Duration        time.Duration
	BlocksPerSecond float64
	TxPerSecond     float64
	MemoryBytes     uint64
	PeakMemoryBytes uint64
	StateRoot       string
	BlockHash       string
	LedgerHash      string
}

func TestLongReplayFromGenesisDeterministicAndMeasured(t *testing.T) {
	blocks := longReplayEnvUint("MSC_LONG_REPLAY_BLOCKS", 2048)
	accounts := longReplayEnvUint("MSC_LONG_REPLAY_ACCOUNTS", 64)
	txEvery := longReplayEnvUint("MSC_LONG_REPLAY_TX_EVERY", 1)
	fixture := buildLongReplayFixture(t, blocks, accounts, txEvery, nil)

	var first longReplayResult
	for run := 1; run <= 3; run++ {
		node := newLongReplayTestNode()
		ledger, result, err := replayLongBlocks(
			node,
			fixture.BaseLedger,
			fixture.Blocks,
			0,
			GenesisHash,
			fixture.Validators,
			fixture.Registry,
			false,
		)
		if err != nil {
			t.Fatalf("replay run %d failed: %v", run, err)
		}
		if got, want := HashLedger(ledger), HashLedger(fixture.FinalLedger); got != want {
			t.Fatalf("replay run %d final ledger mismatch: got=%s want=%s", run, got, want)
		}
		if result.StateRoot != fixture.FinalStateRoot {
			t.Fatalf("replay run %d state root mismatch: got=%s want=%s", run, result.StateRoot, fixture.FinalStateRoot)
		}
		if result.BlockHash != fixture.FinalBlockHash {
			t.Fatalf("replay run %d block hash mismatch: got=%s want=%s", run, result.BlockHash, fixture.FinalBlockHash)
		}
		if got := ValidatorRegistrySnapshotHash(fixture.Registry); got == "" {
			t.Fatal("validator registry commitment must be non-empty")
		}
		if got := validatorSetHashFromSnapshotForHeight(result.Height, fixture.Validators, fixture.Registry); got == "" {
			t.Fatal("validator set commitment must be non-empty")
		}
		if run == 1 {
			first = result
			continue
		}
		if result.StateRoot != first.StateRoot || result.BlockHash != first.BlockHash || result.LedgerHash != first.LedgerHash {
			t.Fatalf("replay run %d not deterministic: first=%+v got=%+v", run, first, result)
		}
	}

	if first.Blocks != blocks {
		t.Fatalf("unexpected replay block count: got=%d want=%d", first.Blocks, blocks)
	}
	if first.BlocksPerSecond <= 0 {
		t.Fatalf("expected positive replay throughput, got %+v", first)
	}
	t.Logf("long replay measured height=%d blocks=%d tx=%d duration=%s blocks_per_second=%.2f tx_per_second=%.2f heap=%d peak_alloc=%d",
		first.Height,
		first.Blocks,
		first.Transactions,
		first.Duration,
		first.BlocksPerSecond,
		first.TxPerSecond,
		first.MemoryBytes,
		first.PeakMemoryBytes,
	)
}

func TestSnapshotRestoreAtScaleAndReplayRemaining(t *testing.T) {
	scales := longReplaySnapshotScales()
	maxScale := uint64(0)
	checkpoints := map[uint64]struct{}{0: {}}
	for _, scale := range scales {
		if scale > maxScale {
			maxScale = scale
		}
		snapshotAt := scale / 2
		if snapshotAt == 0 {
			snapshotAt = 1
		}
		checkpoints[snapshotAt] = struct{}{}
		checkpoints[scale] = struct{}{}
	}
	fixture := buildLongReplayFixture(t, maxScale, longReplayEnvUint("MSC_LONG_REPLAY_ACCOUNTS", 64), 1, checkpoints)

	for _, scale := range scales {
		snapshotAt := scale / 2
		if snapshotAt == 0 {
			snapshotAt = 1
		}
		snapshotLedger, ok := fixture.LedgersByHeight[snapshotAt]
		if !ok {
			t.Fatalf("missing snapshot ledger at height %d", snapshotAt)
		}
		expectedLedger, ok := fixture.LedgersByHeight[scale]
		if !ok {
			t.Fatalf("missing expected ledger at height %d", scale)
		}
		snapshot := makeLongReplaySnapshot(t, fixture, snapshotAt, snapshotLedger)
		restoreNode, restoredLedger := restoreLongReplaySnapshotForTest(t, snapshot, fixture)
		remaining := fixture.Blocks[snapshotAt:scale]
		ledger, result, err := replayLongBlocks(
			restoreNode,
			restoredLedger,
			remaining,
			snapshotAt,
			snapshot.BlockHash,
			fixture.Validators,
			fixture.Registry,
			false,
		)
		if err != nil {
			t.Fatalf("snapshot scale %d replay after restore failed: %v", scale, err)
		}
		if got, want := HashLedger(ledger), HashLedger(expectedLedger); got != want {
			t.Fatalf("snapshot scale %d final ledger mismatch: got=%s want=%s", scale, got, want)
		}
		t.Logf("snapshot restore scale=%d snapshot_at=%d remaining=%d restore_replay=%s bps=%.2f final_root=%s",
			scale,
			snapshotAt,
			len(remaining),
			result.Duration,
			result.BlocksPerSecond,
			result.StateRoot,
		)
	}
}

func TestReplayCrashResumeAndIdempotencyGuards(t *testing.T) {
	crashAt := uint64(128)
	total := uint64(384)
	fixture := buildLongReplayFixture(t, total, 32, 1, map[uint64]struct{}{crashAt: {}})

	crashedNode := newLongReplayTestNode()
	partialLedger, _, err := replayLongBlocks(crashedNode, fixture.BaseLedger, fixture.Blocks[:crashAt], 0, GenesisHash, fixture.Validators, fixture.Registry, false)
	if err != nil {
		t.Fatalf("initial replay before simulated crash failed: %v", err)
	}
	if got, want := HashLedger(partialLedger), HashLedger(fixture.LedgersByHeight[crashAt]); got != want {
		t.Fatalf("crash checkpoint ledger mismatch: got=%s want=%s", got, want)
	}

	resumeNode := newLongReplayTestNode()
	finalLedger, _, err := replayLongBlocks(resumeNode, partialLedger, fixture.Blocks[crashAt:], crashAt, fixture.Blocks[crashAt-1].BlockHash, fixture.Validators, fixture.Registry, false)
	if err != nil {
		t.Fatalf("resume replay failed: %v", err)
	}
	if got, want := HashLedger(finalLedger), HashLedger(fixture.FinalLedger); got != want {
		t.Fatalf("resume final ledger mismatch: got=%s want=%s", got, want)
	}

	alreadyApplied := partialLedger.Clone()
	alreadyApplied, err = ApplyBlockStateWithNode(resumeNode, alreadyApplied, fixture.Blocks[crashAt])
	if err != nil {
		t.Fatalf("apply next block once for idempotency setup: %v", err)
	}
	if _, err := ApplyBlockStateWithNode(resumeNode, alreadyApplied, fixture.Blocks[crashAt]); err == nil {
		t.Fatal("expected duplicate replay of the same block to be rejected by nonce/idempotency guard")
	}
}

func TestSnapshotReplayRejectsMissingDuplicateOutOfOrderAndCorruptBlocks(t *testing.T) {
	fixture := buildLongReplayFixture(t, 96, 16, 1, nil)

	cases := []struct {
		name   string
		blocks []Block
	}{
		{
			name:   "missing block",
			blocks: append(append([]Block{}, fixture.Blocks[:9]...), fixture.Blocks[10:]...),
		},
		{
			name:   "duplicate block",
			blocks: append(append([]Block{}, fixture.Blocks[:10]...), fixture.Blocks[9:]...),
		},
		{
			name: "out of order block",
			blocks: func() []Block {
				out := append([]Block{}, fixture.Blocks...)
				out[9], out[10] = out[10], out[9]
				return out
			}(),
		},
		{
			name: "corrupt prev hash",
			blocks: func() []Block {
				out := append([]Block{}, fixture.Blocks...)
				out[12].PrevHash = "bad-parent"
				return out
			}(),
		},
		{
			name: "corrupt state root",
			blocks: func() []Block {
				out := append([]Block{}, fixture.Blocks...)
				out[15].StateRoot = "bad-state-root"
				return out
			}(),
		},
		{
			name: "wrong validator set",
			blocks: func() []Block {
				out := append([]Block{}, fixture.Blocks...)
				out[20].ValidatorSetHash = "bad-validator-set"
				return out
			}(),
		},
		{
			name: "invalid signature",
			blocks: func() []Block {
				out := append([]Block{}, fixture.Blocks...)
				tx := out[25].Transactions[0]
				tx.Signature = strings.Repeat("0", ed25519.SignatureSize*2)
				out[25].Transactions[0] = tx
				return out
			}(),
		},
	}

	for _, tc := range cases {
		_, _, err := replayLongBlocks(newLongReplayTestNode(), fixture.BaseLedger, tc.blocks, 0, GenesisHash, fixture.Validators, fixture.Registry, true)
		if err == nil {
			t.Fatalf("expected %s replay to fail", tc.name)
		}
		t.Logf("rejected %s with %v", tc.name, err)
	}
}

func TestHistoricalUpgradeReplayHonorsActivationHeights(t *testing.T) {
	oldCheckpoint := SyncSnapshotCheckpointV2Height
	oldDTLV2 := ConfigDTLV2ActivationHeight
	defer func() {
		SyncSnapshotCheckpointV2Height = oldCheckpoint
		ConfigDTLV2ActivationHeight = oldDTLV2
	}()
	SyncSnapshotCheckpointV2Height = 0
	ConfigDTLV2ActivationHeight = 0

	fixture := buildLongReplayFixture(t, 128, 24, 1, nil)
	manager := NewProtocolUpgradeManager()
	manager.MinActivationDelay = 0
	if err := manager.Schedule(ProtocolUpgrade{
		Name:             "checkpoint-v2",
		Version:          "v2",
		ActivationHeight: 64,
		ProtocolChanges: map[string]uint64{
			ProtocolGateSyncSnapshotCheckpointV2: 64,
		},
	}, 1); err != nil {
		t.Fatalf("schedule checkpoint upgrade: %v", err)
	}
	if err := manager.Schedule(ProtocolUpgrade{
		Name:             "dtl-v2",
		Version:          "v3",
		ActivationHeight: 96,
		ProtocolChanges: map[string]uint64{
			ProtocolGateDTLV2: 96,
		},
	}, 1); err != nil {
		t.Fatalf("schedule dtl upgrade: %v", err)
	}

	node := newLongReplayTestNode()
	ledger := fixture.BaseLedger.Clone()
	prevHash := GenesisHash
	for _, block := range fixture.Blocks {
		if block.ID < 64 && SyncSnapshotCheckpointV2Height != 0 {
			t.Fatalf("checkpoint gate activated too early at block %d", block.ID)
		}
		if block.ID < 96 && ConfigDTLV2ActivationHeight != 0 {
			t.Fatalf("dtl gate activated too early at block %d", block.ID)
		}
		if _, err := manager.ActivateDue(block.ID); err != nil {
			t.Fatalf("activate upgrades at height %d: %v", block.ID, err)
		}
		next, result, err := replayLongBlocks(node, ledger, []Block{block}, block.ID-1, prevHash, fixture.Validators, fixture.Registry, false)
		if err != nil {
			t.Fatalf("historical replay block %d failed: result=%+v err=%v", block.ID, result, err)
		}
		ledger = next
		prevHash = block.BlockHash
	}
	if SyncSnapshotCheckpointV2Height != 64 {
		t.Fatalf("checkpoint upgrade activation mismatch: got=%d want=64", SyncSnapshotCheckpointV2Height)
	}
	if ConfigDTLV2ActivationHeight != 96 {
		t.Fatalf("dtl upgrade activation mismatch: got=%d want=96", ConfigDTLV2ActivationHeight)
	}
	if got, want := HashLedger(ledger), HashLedger(fixture.FinalLedger); got != want {
		t.Fatalf("historical upgrade replay final ledger mismatch: got=%s want=%s", got, want)
	}
}

func buildLongReplayFixture(t *testing.T, blocks uint64, accountCount uint64, txEvery uint64, checkpoints map[uint64]struct{}) *longReplayFixture {
	t.Helper()
	if blocks == 0 {
		t.Fatal("long replay fixture requires at least one block")
	}
	if accountCount < 2 {
		accountCount = 2
	}
	if txEvery == 0 {
		txEvery = 1
	}
	validators := []string{"A", "B", "C", "D"}
	registry := longReplayValidatorRegistry(validators)
	accounts := longReplayAccounts(accountCount)
	ledger := NewLedger()
	for _, account := range accounts {
		addBalance(&ledger, CoinSymbol, account.Address, 1_000_000_000)
	}
	for i, validator := range validators {
		wallet := accounts[i%len(accounts)].Address
		ledger.ValidatorRewardWallets[normalizeValidatorID(validator)] = wallet
		ledger.Stakes[stakeKey(wallet, normalizeValidatorID(validator))] = StakeLock{
			ValidatorID: normalizeValidatorID(validator),
			Amount:      250_000,
			LockedUntil: blocks + DefaultStakeLockEpochs,
		}
	}
	ledger.EVMState["contract-long-replay"] = strings.Repeat("ab", 32)
	ledger.EVMCode["contract-long-replay"] = strings.Repeat("01", 64)
	ledger.EVMStorage["contract-long-replay"] = make(map[string]string)
	for i := 0; i < int(longReplayEnvUint("MSC_LONG_REPLAY_STORAGE_SLOTS", 128)); i++ {
		slot := fmt.Sprintf("%064x", i)
		value := sha256.Sum256([]byte(fmt.Sprintf("long-replay-slot-%d", i)))
		ledger.EVMStorage["contract-long-replay"][slot] = hex.EncodeToString(value[:])
	}

	out := &longReplayFixture{
		Accounts:        accounts,
		BaseLedger:      ledger.Clone(),
		Blocks:          make([]Block, 0, blocks),
		LedgersByHeight: make(map[uint64]Ledger),
		Validators:      validators,
		Registry:        registry,
	}
	if _, ok := checkpoints[0]; ok {
		out.LedgersByHeight[0] = ledger.Clone()
	}

	sealNode := newLongReplayTestNode()
	prevHash := GenesisHash
	for height := uint64(1); height <= blocks; height++ {
		txs := make([]Transaction, 0, 1)
		if height%txEvery == 0 {
			sender := accounts[(height-1)%uint64(len(accounts))]
			receiver := accounts[height%uint64(len(accounts))]
			tx := Transaction{
				From:      sender.Address,
				To:        receiver.Address,
				Amount:    int(height%7) + 1,
				Nonce:     getNonce(ledger, sender.Address) + 1,
				PublicKey: hex.EncodeToString(sender.PubKey),
				Expiry:    4_102_444_800,
				Type:      TxTransfer,
				ChainID:   ChainID,
				Coin:      CoinSymbol,
			}
			tx.Fee = requiredFeeForTxWithLedger(&ledger, tx)
			tx.Signature = hex.EncodeToString(ed25519.Sign(sender.PrivKey, TxPayload(tx)))
			tx.ID = ComputeTxID(tx)
			txs = append(txs, tx)
			out.TxCount++
		}

		block := Block{
			ID:                     height,
			Height:                 height,
			PrevHash:               prevHash,
			Proposer:               validators[(height-1)%uint64(len(validators))],
			Type:                   BlockTypeWork,
			Transactions:           txs,
			BlockTime:              LogicalTimeForEpochTick(height, TickFinalize),
			MempoolRoot:            ComputeMempoolRoot(txs),
			ValidatorSetHash:       validatorSetHashFromSnapshotForHeight(height, validators, registry),
			ValidatorSetRoot:       ValidatorSetMerkleRoot(height, validators, registry),
			ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
			NextValidatorSetHash:   validatorSetHashFromSnapshotForHeight(height+1, validators, registry),
			NextValidatorSetRoot:   ValidatorSetMerkleRoot(height+1, validators, registry),
			NextValidatorSetHeight: height + 1,
			ActivationHeight:       height + 1,
			ConsensusMode:          "NORMAL",
			QuorumPolicyVersion:    quorumPolicyVersionV1,
			ActiveReadyCount:       len(validators),
			RequiredQuorum:         execQuorumRequired(len(validators)),
			StrictQuorum:           execQuorumRequired(len(validators)),
		}
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		nextLedger, err := ApplyBlockStateWithNode(sealNode, ledger, block)
		if err != nil {
			t.Fatalf("seal block %d: %v", height, err)
		}
		block.StateRoot = ComputeExecHashVersioned(block, HashLedger(nextLedger), executionStateRootVersionForHeight(block.ID))
		if block.StateRoot == "" {
			t.Fatalf("empty state root at height %d", height)
		}
		block.BlockHash = HashBlock(block)
		if block.BlockHash == "" {
			t.Fatalf("empty block hash at height %d", height)
		}

		ledger = nextLedger
		prevHash = block.BlockHash
		out.Blocks = append(out.Blocks, block)
		if _, ok := checkpoints[height]; ok {
			out.LedgersByHeight[height] = ledger.Clone()
		}
	}
	out.FinalLedger = ledger.Clone()
	out.FinalStateRoot = out.Blocks[len(out.Blocks)-1].StateRoot
	out.FinalBlockHash = out.Blocks[len(out.Blocks)-1].BlockHash
	return out
}

func replayLongBlocks(
	node *Node,
	base Ledger,
	blocks []Block,
	startHeight uint64,
	prevHash string,
	validators []string,
	registry map[string]ValidatorRecord,
	validateEnvelope bool,
) (Ledger, longReplayResult, error) {
	if node == nil {
		node = newLongReplayTestNode()
	}
	started := time.Now()
	ledger := base.Clone()
	result := longReplayResult{Height: startHeight, Blocks: uint64(len(blocks))}
	success := false
	defer func() {
		node.observeReplayOperation(result.Height, result.Blocks, time.Since(started), success)
	}()

	for i, block := range blocks {
		expectedHeight := startHeight + 1 + uint64(i)
		if block.ID != expectedHeight {
			return ledger, result, fmt.Errorf("height continuity mismatch at index %d: got=%d want=%d", i, block.ID, expectedHeight)
		}
		if block.Height != 0 && block.Height != block.ID {
			return ledger, result, fmt.Errorf("block height field mismatch at height %d: got=%d", block.ID, block.Height)
		}
		if !strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(prevHash)) {
			return ledger, result, fmt.Errorf("prev hash mismatch at height %d: got=%s want=%s", block.ID, block.PrevHash, prevHash)
		}
		if got := ComputeMempoolRoot(block.Transactions); got != block.MempoolRoot {
			return ledger, result, fmt.Errorf("mempool root mismatch at height %d: got=%s want=%s", block.ID, got, block.MempoolRoot)
		}
		if got := validatorSetHashFromSnapshotForHeight(block.ID, validators, registry); got != "" && !strings.EqualFold(got, block.ValidatorSetHash) {
			return ledger, result, fmt.Errorf("validator set hash mismatch at height %d: got=%s want=%s", block.ID, block.ValidatorSetHash, got)
		}
		if got := ValidatorSetMerkleRoot(block.ID, validators, registry); got != "" && !strings.EqualFold(got, block.ValidatorSetRoot) {
			return ledger, result, fmt.Errorf("validator set root mismatch at height %d: got=%s want=%s", block.ID, block.ValidatorSetRoot, got)
		}
		if got := ValidatorRegistrySnapshotHash(registry); got != "" && !strings.EqualFold(got, block.ValidatorRegistryHash) {
			return ledger, result, fmt.Errorf("validator registry hash mismatch at height %d: got=%s want=%s", block.ID, block.ValidatorRegistryHash, got)
		}
		if validateEnvelope {
			for _, tx := range block.Transactions {
				if err := validateLongReplayTxEnvelope(tx); err != nil {
					return ledger, result, fmt.Errorf("tx envelope invalid at height %d tx=%s: %w", block.ID, tx.ID, err)
				}
			}
		}
		nextLedger, err := ApplyBlockStateWithNode(node, ledger, block)
		if err != nil {
			return ledger, result, fmt.Errorf("apply block %d: %w", block.ID, err)
		}
		stateRoot := ComputeExecHashVersioned(block, HashLedger(nextLedger), executionStateRootVersionForHeight(block.ID))
		if !strings.EqualFold(stateRoot, block.StateRoot) {
			return ledger, result, fmt.Errorf("state root mismatch at height %d: got=%s want=%s", block.ID, stateRoot, block.StateRoot)
		}
		blockHash := HashBlock(block)
		if !strings.EqualFold(blockHash, block.BlockHash) {
			return ledger, result, fmt.Errorf("block hash mismatch at height %d: got=%s want=%s", block.ID, blockHash, block.BlockHash)
		}
		ledger = nextLedger
		prevHash = block.BlockHash
		result.Height = block.ID
		result.Transactions += uint64(len(block.Transactions))
		result.StateRoot = stateRoot
		result.BlockHash = blockHash
		if node.Blockchain != nil {
			node.Blockchain.AddBlock(block)
		}
	}

	result.Duration = time.Since(started)
	if result.Duration <= 0 {
		result.Duration = time.Nanosecond
	}
	result.BlocksPerSecond = float64(result.Blocks) / result.Duration.Seconds()
	result.TxPerSecond = float64(result.Transactions) / result.Duration.Seconds()
	result.LedgerHash = HashLedger(ledger)
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	result.MemoryBytes = mem.Alloc
	result.PeakMemoryBytes = mem.TotalAlloc
	success = true
	return ledger, result, nil
}

func makeLongReplaySnapshot(t *testing.T, fixture *longReplayFixture, height uint64, ledger Ledger) StateSnapshot {
	t.Helper()
	if height == 0 || height > uint64(len(fixture.Blocks)) {
		t.Fatalf("invalid snapshot height %d", height)
	}
	block := fixture.Blocks[height-1]
	validators := make(map[string]bool, len(fixture.Validators))
	for _, id := range fixture.Validators {
		validators[id] = true
	}
	snapshot := StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 height,
		BlockHash:              block.BlockHash,
		StateRoot:              block.StateRoot,
		StateMerkleRoot:        LedgerStateMerkleRoot(ledger),
		LedgerHash:             HashLedger(ledger),
		LedgerStage:            snapshotLedgerStageExecution,
		GenesisHash:            GenesisHash,
		PrevHash:               block.PrevHash,
		Ledger:                 ledger.Clone(),
		Validators:             validators,
		ValidatorSetHash:       block.ValidatorSetHash,
		ValidatorSetRoot:       block.ValidatorSetRoot,
		NextValidatorSetHash:   block.NextValidatorSetHash,
		NextValidatorSetRoot:   block.NextValidatorSetRoot,
		NextValidatorSetHeight: block.NextValidatorSetHeight,
		ActivationHeight:       block.ActivationHeight,
		ValidatorRegistry:      cloneLongReplayRegistry(fixture.Registry),
		ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(fixture.Registry),
		Timestamp:              time.Now().Unix(),
	}
	populateSnapshotDerivedFields(&snapshot)
	snapshot.SnapshotHash = snapshotCanonicalHash(&snapshot)
	return snapshot
}

func restoreLongReplaySnapshotForTest(t *testing.T, snapshot StateSnapshot, fixture *longReplayFixture) (*Node, Ledger) {
	t.Helper()
	started := time.Now()
	populateSnapshotDerivedFields(&snapshot)
	if got, want := snapshot.SnapshotHash, snapshotCanonicalHash(&snapshot); got != want {
		t.Fatalf("snapshot hash mismatch: got=%s want=%s", got, want)
	}
	if reason := snapshotMetadataRejectReason(&snapshot); reason != "" {
		t.Fatalf("snapshot metadata rejected: %s", reason)
	}
	block := fixture.Blocks[snapshot.Height-1]
	computedRoot := ComputeExecHashVersioned(block, HashLedger(snapshot.Ledger), executionStateRootVersionForHeight(block.ID))
	if computedRoot != snapshot.StateRoot {
		t.Fatalf("snapshot state root mismatch: got=%s want=%s", computedRoot, snapshot.StateRoot)
	}
	node := newLongReplayTestNode()
	node.Ledger = snapshot.Ledger.Clone()
	node.ExecutionLedger = snapshot.Ledger.Clone()
	node.observeSnapshotOperation("apply", snapshot.Height, time.Since(started), true)
	return node, snapshot.Ledger.Clone()
}

func validateLongReplayTxEnvelope(tx Transaction) error {
	if tx.ChainID != ChainID {
		return fmt.Errorf("invalid chain id: %s", tx.ChainID)
	}
	if tx.ID == "" || !strings.EqualFold(tx.ID, ComputeTxID(tx)) {
		return fmt.Errorf("tx id mismatch")
	}
	if tx.Type == TxFaucet {
		return nil
	}
	pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key")
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)
	if !AddressMatchesPublicKey(tx.From, pubKey) {
		return fmt.Errorf("address/public key mismatch")
	}
	sig, err := hex.DecodeString(tx.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature encoding")
	}
	if !ed25519.Verify(pubKey, TxPayload(tx), sig) && !ed25519.Verify(pubKey, TxPayloadLegacy(tx), sig) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

func newLongReplayTestNode() *Node {
	bc := NewBlockchain()
	return &Node{
		ID:              "LONG_REPLAY",
		Role:            "full",
		Ledger:          NewLedger(),
		ExecutionLedger: NewLedger(),
		Blockchain:      &bc,
		SeenTxIDs:       make(map[string]bool),
		SeenBlockHashes: make(map[string]bool),
		ForkBlocks:      make(map[uint64][]Block),
		Mempool: Mempool{
			SeenTxIDs: make(map[string]bool),
		},
		validatorStatus:             make(map[string]*ValidatorStatus),
		pendingValidators:           make(map[string]uint64),
		connectedPeers:              make(map[string]bool),
		connectingPeers:             make(map[string]bool),
		validatorSuspect:            make(map[string]time.Time),
		allowedPeerIDs:              make(map[string]bool),
		ProposalHistory:             make(map[uint64]string),
		MisbehaviorLog:              make(map[string][]SlashEvidence),
		committed:                   make(map[uint64]string),
		commitVotes:                 make(map[uint64]map[string]map[string]struct{}),
		commitVoted:                 make(map[uint64]map[string]string),
		quarantineUntil:             make(map[string]time.Time),
		peerSetHash:                 make(map[string]string),
		peerHashMatch:               make(map[string]bool),
		peerToValidator:             make(map[string]string),
		peerSuspectAt:               make(map[string]time.Time),
		peerHelloSentAt:             make(map[string]time.Time),
		peerFlapTimes:               make(map[string][]time.Time),
		syncPeerScores:              make(map[string]*SyncPeerScore),
		epochValidators:             make(map[uint64][]string),
		frozenValidatorsByHeight:    make(map[uint64][]string),
		frozenValidatorHashByHeight: make(map[uint64]string),
	}
}

func longReplayAccounts(count uint64) []longReplayAccount {
	out := make([]longReplayAccount, 0, count)
	for i := uint64(0); i < count; i++ {
		seed := sha256.Sum256([]byte(fmt.Sprintf("msc-long-replay-account-%d", i)))
		priv := ed25519.NewKeyFromSeed(seed[:])
		pub := priv.Public().(ed25519.PublicKey)
		out = append(out, longReplayAccount{
			Address: AddressFromPublicKey(pub),
			PubKey:  pub,
			PrivKey: priv,
		})
	}
	return out
}

func longReplayValidatorRegistry(validators []string) map[string]ValidatorRecord {
	registry := make(map[string]ValidatorRecord, len(validators))
	for i, id := range validators {
		norm := normalizeValidatorID(id)
		pubSeed := sha256.Sum256([]byte("msc-long-replay-validator-" + norm))
		registry[norm] = ValidatorRecord{
			ID:               norm,
			ConsensusPubKey:  hex.EncodeToString(pubSeed[:]),
			GovernanceSigner: true,
			Stake:            int64(250_000 + i),
			Reputation:       ValidatorReputationInitial,
			LastActive:       1,
			Status:           ValidatorActive,
			JoinHeight:       1,
			ActiveHeights:    []uint64{1},
		}
	}
	return registry
}

func cloneLongReplayRegistry(src map[string]ValidatorRecord) map[string]ValidatorRecord {
	out := make(map[string]ValidatorRecord, len(src))
	for id, rec := range src {
		rec.ActiveHeights = append([]uint64{}, rec.ActiveHeights...)
		rec.SignedHeights = append([]uint64{}, rec.SignedHeights...)
		out[id] = rec
	}
	return out
}

func longReplayEnvUint(key string, fallback uint64) uint64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return fallback
	}
	return value
}

func longReplaySnapshotScales() []uint64 {
	if raw := strings.TrimSpace(os.Getenv("MSC_SNAPSHOT_RESTORE_SCALES")); raw != "" {
		parts := strings.Split(raw, ",")
		out := make([]uint64, 0, len(parts))
		for _, part := range parts {
			value, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
			if err == nil && value > 1 {
				out = append(out, value)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	longBlocks := longReplayEnvUint("MSC_LONG_REPLAY_BLOCKS", 0)
	switch {
	case longBlocks >= 1_000_000:
		return []uint64{10_000, 100_000, 500_000, 1_000_000}
	case longBlocks >= 100_000:
		return []uint64{10_000, 50_000, 100_000}
	case longBlocks >= 10_000:
		return []uint64{1_000, 5_000, 10_000}
	default:
		return []uint64{256, 512, 1024}
	}
}
