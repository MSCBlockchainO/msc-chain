package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

func testExecutionParentLedgers() (Ledger, Ledger) {
	execLedger := NewLedger()
	addBalance(&execLedger, CoinSymbol, "alice", 10)

	runtimeLedger := execLedger.Clone()
	addBalance(&runtimeLedger, CoinSymbol, TREASURY_ADDRESS, 99)
	return execLedger, runtimeLedger
}

func testExecutionParentNode(t *testing.T) (*Node, Block, Ledger, Ledger) {
	t.Helper()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	parent := Block{
		ID:        1,
		BlockHash: "parent-hash",
		BlockTime: LogicalTimeForEpoch(1),
	}
	parent.Timestamp = int64(SystemTimeUnits(parent.BlockTime))
	node.Blockchain.AddBlock(parent)
	node.commitMu.Lock()
	node.committedHeight = parent.ID
	node.commitMu.Unlock()

	execLedger, runtimeLedger := testExecutionParentLedgers()
	node.cacheExecutionSnapshotLedger(parent.ID, execLedger)
	node.Ledger = runtimeLedger.Clone()
	return node, parent, execLedger, runtimeLedger
}

func TestExecuteBlockAndGetStateRootUsesCachedExecutionParentLedger(t *testing.T) {
	node, parent, execLedger, runtimeLedger := testExecutionParentNode(t)

	block := Block{
		ID:        2,
		Type:      BlockTypeTime,
		PrevHash:  parent.BlockHash,
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(2),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	got := node.ExecuteBlockAndGetStateRoot(block)
	want := ComputeExecHashVersioned(block, HashLedger(execLedger), executionStateRootVersionForHeight(block.ID))
	runtimeRoot := ComputeExecHashVersioned(block, HashLedger(runtimeLedger), executionStateRootVersionForHeight(block.ID))

	if got != want {
		t.Fatalf("expected sealed parent execution root, got=%q want=%q", got, want)
	}
	if got == runtimeRoot {
		t.Fatalf("execution root incorrectly used runtime post-effects ledger: got=%q runtime=%q", got, runtimeRoot)
	}
}

func TestBuildLeaderBlockUsesCachedExecutionParentLedger(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)

	block := node.BuildLeaderBlock(node.currentEpoch())

	want := ComputeExecHashVersioned(block, HashLedger(execLedger), executionStateRootVersionForHeight(block.ID))
	runtimeRoot := ComputeExecHashVersioned(block, HashLedger(runtimeLedger), executionStateRootVersionForHeight(block.ID))
	if block.StateRoot != want {
		t.Fatalf("leader block state root mismatch: got=%q want=%q", block.StateRoot, want)
	}
	if block.StateRoot == runtimeRoot {
		t.Fatalf("leader block state root incorrectly used runtime post-effects ledger")
	}
}

func TestBuildDeterministicTxBatchUsesExecutionParentLedger(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)

	runtimeLedger.Nonces["alice"] = 1
	node.Ledger = runtimeLedger.Clone()

	tx := Transaction{
		ID:     "tx-exec-parent",
		From:   "alice",
		To:     "bob",
		Amount: 1,
		Nonce:  1,
		Type:   TxTransfer,
		Coin:   CoinSymbol,
		Expiry: time.Now().Unix() + 300,
	}
	tx.Fee = requiredFeeForTxWithLedger(&execLedger, tx)
	node.Mempool.Transactions = []Transaction{tx}

	txs, _ := node.BuildDeterministicTxBatch(node.currentEpoch())
	if len(txs) != 1 || txs[0].ID != tx.ID {
		t.Fatalf("expected tx batch to use execution parent ledger, got=%v", txs)
	}

	if got := selectTxBatch([]Transaction{tx}, runtimeLedger); len(got) != 0 {
		t.Fatalf("test setup invalid: runtime ledger still selected txs=%v", got)
	}
}

func TestBuildLeaderBlockReceiptsUseExecutionParentLedger(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)

	runtimeLedger.Nonces["alice"] = 1
	node.Ledger = runtimeLedger.Clone()

	tx := Transaction{
		ID:     "tx-exec-receipt",
		From:   "alice",
		To:     "bob",
		Amount: 1,
		Nonce:  1,
		Type:   TxTransfer,
		Coin:   CoinSymbol,
		Expiry: time.Now().Unix() + 300,
	}
	tx.Fee = requiredFeeForTxWithLedger(&execLedger, tx)
	node.Mempool.Transactions = []Transaction{tx}

	block := node.BuildLeaderBlock(node.currentEpoch())
	if len(block.Receipts) != 1 {
		t.Fatalf("expected one receipt, got=%d", len(block.Receipts))
	}

	if got, want := block.Receipts[0].PreStateHash, HashLedger(execLedger); got != want {
		t.Fatalf("receipt pre-state should use execution parent ledger: got=%q want=%q", got, want)
	}
	if got, runtime := block.Receipts[0].PreStateHash, HashLedger(runtimeLedger); got == runtime {
		t.Fatalf("receipt pre-state incorrectly used runtime ledger")
	}
}

func TestProduceWorkBlockUsesExecutionAuthorityLedger(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)

	runtimeLedger.Nonces["alice"] = 1
	node.Ledger = runtimeLedger.Clone()
	node.ExecutionLedger = execLedger.Clone()

	tx := Transaction{
		ID:     "tx-produce-work-exec-authority",
		From:   "alice",
		To:     "bob",
		Amount: 1,
		Nonce:  1,
		Type:   TxTransfer,
		Coin:   CoinSymbol,
		Expiry: time.Now().Unix() + 300,
	}
	tx.Fee = requiredFeeForTxWithLedger(&execLedger, tx)
	node.Mempool.Transactions = []Transaction{tx}

	block := node.ProduceWorkBlock(node.Blockchain)
	if len(block.Transactions) != 1 || block.Transactions[0].ID != tx.ID {
		t.Fatalf("expected ProduceWorkBlock to select tx using execution authority ledger, got=%v", block.Transactions)
	}
	if len(block.Receipts) != 1 {
		t.Fatalf("expected ProduceWorkBlock to emit one receipt, got=%d", len(block.Receipts))
	}
	if got, want := block.Receipts[0].PreStateHash, HashLedger(execLedger); got != want {
		t.Fatalf("ProduceWorkBlock receipt pre-state should use execution ledger: got=%q want=%q", got, want)
	}
	if got, runtime := block.Receipts[0].PreStateHash, HashLedger(runtimeLedger); got == runtime {
		t.Fatalf("ProduceWorkBlock receipt pre-state incorrectly used runtime ledger")
	}
}

func TestExecutionParentLedgerForBlockPrefersLiveExecutionTipAtChainHead(t *testing.T) {
	node, parent, cachedExecLedger, _ := testExecutionParentNode(t)

	liveExecutionLedger := cachedExecLedger.Clone()
	addBalance(&liveExecutionLedger, CoinSymbol, TREASURY_ADDRESS, 25)
	node.ExecutionLedger = liveExecutionLedger.Clone()

	block := Block{
		ID:        2,
		Type:      BlockTypeTime,
		PrevHash:  parent.BlockHash,
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(2),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	parentLedger, ctx, ok := node.executionParentLedgerForBlock(block)
	if !ok {
		t.Fatalf("expected chain-head parent ledger resolution to succeed")
	}
	if ctx.ParentSource != "live_execution_tip" {
		t.Fatalf("expected live execution tip parent source, got %q", ctx.ParentSource)
	}
	if got, want := HashLedger(parentLedger), HashLedger(liveExecutionLedger); got != want {
		t.Fatalf("expected live execution tip ledger hash, got=%s want=%s", got, want)
	}
}

func TestReceiveTransactionWithReasonUsesExecutionAuthorityLedger(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)

	runtimeLedger.Nonces["alice"] = 1
	node.Ledger = runtimeLedger.Clone()
	node.ExecutionLedger = execLedger.Clone()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	from := AddressFromPublicKey(pub)
	addBalance(&node.ExecutionLedger, CoinSymbol, from, 10)

	tx := Transaction{
		From:      from,
		To:        "bob",
		Amount:    1,
		Nonce:     1,
		Type:      TxTransfer,
		Coin:      CoinSymbol,
		Expiry:    time.Now().Unix() + 300,
		ChainID:   ChainID,
		PublicKey: hex.EncodeToString(pub),
	}
	tx.Fee = requiredFeeForTxWithLedger(&execLedger, tx)
	tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
	tx.ID = ComputeTxID(tx)

	if err := node.Mempool.ValidateTransaction(tx, &runtimeLedger); err == nil {
		t.Fatalf("test setup invalid: runtime ledger unexpectedly accepts tx")
	}
	if ok, reason := node.ReceiveTransactionWithReason(tx); !ok {
		t.Fatalf("expected tx ingress to use execution authority ledger, reason=%s", reason)
	}
	if len(node.Mempool.Transactions) != 1 || node.Mempool.Transactions[0].ID != tx.ID {
		t.Fatalf("expected accepted tx in mempool, got=%v", node.Mempool.Transactions)
	}
}

func TestExecutionLedgerForBlockResetsRuntimeLedgerOnPreExecutionDrift(t *testing.T) {
	node, parent, execLedger, runtimeLedger := testExecutionParentNode(t)
	node.ExecutionLedger = execLedger.Clone()
	if HashLedger(runtimeLedger) == HashLedger(execLedger) {
		t.Fatalf("test setup invalid: runtime and execution ledgers unexpectedly match")
	}

	block := Block{
		ID:        2,
		Type:      BlockTypeTime,
		PrevHash:  parent.BlockHash,
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(2),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	if _, _, ok := node.executionLedgerForBlock(block); !ok {
		t.Fatalf("expected execution ledger resolution to succeed after runtime reset")
	}
	if got, want := HashLedger(node.Ledger), HashLedger(execLedger); got != want {
		t.Fatalf("expected runtime ledger reset to execution ledger, got=%q want=%q", got, want)
	}
}

func TestDeterministicEnsureExecutionLedgerAlignedRepairsFromAuthoritativeSnapshot(t *testing.T) {
	node, parent, authoritativeLedger, _ := testExecutionParentNode(t)

	driftedRuntime := NewLedger()
	addBalance(&driftedRuntime, CoinSymbol, "bob", 33)
	driftedExecution := NewLedger()
	addBalance(&driftedExecution, CoinSymbol, "carol", 44)
	node.Ledger = driftedRuntime.Clone()
	node.ExecutionLedger = driftedExecution.Clone()
	node.cacheExecutionSnapshotLedger(parent.ID, authoritativeLedger)

	ctx := &executionRootContext{
		ParentHeight:        parent.ID,
		RuntimeLedgerHash:   HashLedger(driftedRuntime),
		ExecutionLedgerHash: HashLedger(driftedExecution),
	}
	if err := node.deterministicEnsureExecutionLedgerAligned(parent.ID+1, ctx); err != nil {
		t.Fatalf("expected authoritative snapshot repair, got err=%v", err)
	}

	want := HashLedger(authoritativeLedger)
	if got := HashLedger(node.Ledger.Clone()); got != want {
		t.Fatalf("runtime ledger was not repaired from authoritative snapshot: got=%s want=%s", got, want)
	}
	if got := HashLedger(node.currentExecutionLedgerClone()); got != want {
		t.Fatalf("execution ledger was not repaired from authoritative snapshot: got=%s want=%s", got, want)
	}
	if ctx.RuntimeLedgerHash != want || ctx.ExecutionLedgerHash != want {
		t.Fatalf("execution context hashes were not updated after repair: runtime=%s execution=%s want=%s", ctx.RuntimeLedgerHash, ctx.ExecutionLedgerHash, want)
	}
}

func TestCurrentExecutionLedgerClonePrefersCachedSnapshotOverRuntimeFallback(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)
	node.ExecutionLedger = Ledger{}
	node.Ledger = runtimeLedger.Clone()

	got := node.currentExecutionLedgerClone()
	if gotHash, want := HashLedger(got), HashLedger(execLedger); gotHash != want {
		t.Fatalf("expected currentExecutionLedgerClone to prefer cached execution snapshot: got=%s want=%s", gotHash, want)
	}
	if gotHash, runtime := HashLedger(got), HashLedger(runtimeLedger); gotHash == runtime {
		t.Fatalf("currentExecutionLedgerClone incorrectly fell back to runtime ledger")
	}
}

func TestDeterministicEnsureExecutionLedgerAlignedFailsClosedDuringValidatorConsensusDrift(t *testing.T) {
	node, parent, authoritativeLedger, _ := testExecutionParentNode(t)

	driftedRuntime := NewLedger()
	addBalance(&driftedRuntime, CoinSymbol, "bob", 33)
	driftedExecution := NewLedger()
	addBalance(&driftedExecution, CoinSymbol, "carol", 44)
	node.Role = "validator"
	node.Ledger = driftedRuntime.Clone()
	node.ExecutionLedger = driftedExecution.Clone()
	node.cacheExecutionSnapshotLedger(parent.ID, authoritativeLedger)

	ctx := &executionRootContext{
		ParentHeight:        parent.ID,
		RuntimeLedgerHash:   HashLedger(driftedRuntime),
		ExecutionLedgerHash: HashLedger(driftedExecution),
	}
	if err := node.deterministicEnsureExecutionLedgerAligned(parent.ID+1, ctx); err == nil {
		t.Fatalf("expected live validator consensus drift to fail closed")
	}
	if got, want := HashLedger(node.Ledger.Clone()), HashLedger(driftedRuntime); got != want {
		t.Fatalf("runtime ledger should remain drifted on fail-closed path: got=%s want=%s", got, want)
	}
	if got, want := HashLedger(node.currentExecutionLedgerClone()), HashLedger(driftedExecution); got != want {
		t.Fatalf("execution ledger should remain unchanged on fail-closed path: got=%s want=%s", got, want)
	}
}

func TestDeterministicEnsureExecutionLedgerAlignedAllowsRepairWhileSyncingValidator(t *testing.T) {
	node, parent, authoritativeLedger, _ := testExecutionParentNode(t)

	driftedRuntime := NewLedger()
	addBalance(&driftedRuntime, CoinSymbol, "bob", 33)
	driftedExecution := NewLedger()
	addBalance(&driftedExecution, CoinSymbol, "carol", 44)
	node.Role = "validator"
	node.Consensus.Syncing = true
	node.Consensus.Paused = true
	node.Ledger = driftedRuntime.Clone()
	node.ExecutionLedger = driftedExecution.Clone()
	node.cacheExecutionSnapshotLedger(parent.ID, authoritativeLedger)

	ctx := &executionRootContext{
		ParentHeight:        parent.ID,
		RuntimeLedgerHash:   HashLedger(driftedRuntime),
		ExecutionLedgerHash: HashLedger(driftedExecution),
	}
	if err := node.deterministicEnsureExecutionLedgerAligned(parent.ID+1, ctx); err != nil {
		t.Fatalf("expected syncing validator to repair from authoritative snapshot, got err=%v", err)
	}

	want := HashLedger(authoritativeLedger)
	if got := HashLedger(node.Ledger.Clone()); got != want {
		t.Fatalf("runtime ledger was not repaired during syncing validator recovery: got=%s want=%s", got, want)
	}
	if got := HashLedger(node.currentExecutionLedgerClone()); got != want {
		t.Fatalf("execution ledger was not repaired during syncing validator recovery: got=%s want=%s", got, want)
	}
}

func TestVerifyBlockUsesCachedExecutionParentLedgerForWorkBlock(t *testing.T) {
	oldResultGossipOnly := ResultGossipOnly
	oldValidatorSetCommitmentV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		ResultGossipOnly = oldResultGossipOnly
		ValidatorSetCommitmentV2Height = oldValidatorSetCommitmentV2Height
	})
	ResultGossipOnly = false
	ValidatorSetCommitmentV2Height = ^uint64(0)

	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)
	node.ID = "A"
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node.validatorSetMu.Lock()
	if node.frozenValidatorsByHeight == nil {
		node.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if node.frozenValidatorHashByHeight == nil {
		node.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	node.frozenValidatorsByHeight[2] = append([]string{}, validators...)
	node.frozenValidatorHashByHeight[2] = ValidatorSetHash(validators)
	node.validatorSetMu.Unlock()

	block := node.BuildLeaderBlock(node.currentEpoch())
	block.Proposer = node.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	block.Type = BlockTypeWork
	block.ReceiptRoot = ""
	block.Signatures = append([]string{}, validators...)
	block.StateRoot = ComputeExecHashVersioned(block, HashLedger(execLedger), executionStateRootVersionForHeight(block.ID))
	block.BlockHash = HashBlock(block)

	if err := node.VerifyBlock(block, node.Blockchain); err != nil {
		t.Fatalf("expected verify block to use sealed parent ledger, got err=%v", err)
	}
	if block.StateRoot == ComputeExecHashVersioned(block, HashLedger(runtimeLedger), executionStateRootVersionForHeight(block.ID)) {
		t.Fatalf("test setup invalid: work block state root still matches runtime ledger")
	}
}

func TestVerifyBlockRepairsStaleWorkParentLedgerBeforeReceiptCheck(t *testing.T) {
	oldResultGossipOnly := ResultGossipOnly
	oldValidatorSetCommitmentV2Height := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		ResultGossipOnly = oldResultGossipOnly
		ValidatorSetCommitmentV2Height = oldValidatorSetCommitmentV2Height
	})
	ResultGossipOnly = false
	ValidatorSetCommitmentV2Height = ^uint64(0)

	node, parent, execLedger, _ := testExecutionParentNode(t)
	node.ID = "A"
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	from := AddressFromPublicKey(pub)
	addBalance(&execLedger, CoinSymbol, from, 10)
	node.cacheExecutionSnapshotLedger(parent.ID, execLedger)
	staleLedger := execLedger.Clone()
	addBalance(&staleLedger, CoinSymbol, TREASURY_ADDRESS, 99)
	node.ExecutionLedger = staleLedger.Clone()
	node.Ledger = staleLedger.Clone()

	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node.validatorSetMu.Lock()
	if node.frozenValidatorsByHeight == nil {
		node.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if node.frozenValidatorHashByHeight == nil {
		node.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	node.frozenValidatorsByHeight[2] = append([]string{}, validators...)
	node.frozenValidatorHashByHeight[2] = ValidatorSetHash(validators)
	node.validatorSetMu.Unlock()

	tx := Transaction{
		From:      from,
		To:        "bob",
		Amount:    1,
		Nonce:     1,
		Type:      TxTransfer,
		Coin:      CoinSymbol,
		Expiry:    time.Now().Unix() + 300,
		ChainID:   ChainID,
		PublicKey: hex.EncodeToString(pub),
	}
	tx.Fee = requiredFeeForTxWithLedger(&execLedger, tx)
	tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
	tx.ID = ComputeTxID(tx)

	block := Block{
		ID:               2,
		Type:             BlockTypeWork,
		PrevHash:         parent.BlockHash,
		Proposer:         node.consensusLeaderForHeightRound(2, 0, validators),
		Signatures:       append([]string{}, validators...),
		Transactions:     []Transaction{tx},
		MempoolRoot:      ComputeMempoolRoot([]Transaction{tx}),
		ValidatorSetHash: node.expectedValidatorSetHash(2),
		BlockTime:        LogicalTimeForEpoch(2),
	}
	if block.ValidatorSetHash == "" {
		block.ValidatorSetHash = ValidatorSetHash(validators)
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	nextLedger, err := ApplyBlockStateWithNode(node, execLedger, block)
	if err != nil {
		t.Fatalf("apply source work block: %v", err)
	}
	block.Receipts = []StateReceipt{{
		TxHash:        tx.ID,
		PreStateHash:  HashLedger(execLedger),
		PostStateHash: HashLedger(nextLedger),
	}}
	block.ReceiptRoot = ComputeReceiptRoot(block.Receipts)
	block.StateRoot = ComputeExecHashVersioned(block, HashLedger(nextLedger), executionStateRootVersionForHeight(block.ID))
	block.BlockHash = HashBlock(block)

	if VerifyWorkBlockExecutionWithNode(node, block, staleLedger) {
		t.Fatalf("test setup invalid: stale parent ledger unexpectedly verifies work receipts")
	}
	if err := node.VerifyBlock(block, node.Blockchain); err != nil {
		t.Fatalf("expected VerifyBlock to repair stale work parent ledger, got err=%v", err)
	}
	if got, want := HashLedger(node.currentExecutionLedgerClone()), HashLedger(execLedger); got != want {
		t.Fatalf("expected execution ledger repaired to authoritative parent: got=%s want=%s", got, want)
	}
}

func TestExecuteBlockAndGetStateRootRejectsParentHashMismatch(t *testing.T) {
	node, _, _, _ := testExecutionParentNode(t)

	block := Block{
		ID:        2,
		Type:      BlockTypeTime,
		PrevHash:  "wrong-parent",
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(2),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

	if got := node.ExecuteBlockAndGetStateRoot(block); got != "" {
		t.Fatalf("expected parent hash mismatch to reject execution root, got=%q", got)
	}
}

func TestReceiveBlockCachesCommittedExecutionLedgerFromSealedParent(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	block1 := node.BuildLeaderBlock(node.currentEpoch())
	block1.BlockTime = LogicalTimeForEpochTick(block1.ID, TickFinalize)
	block1.Timestamp = int64(SystemTimeUnits(block1.BlockTime))
	block1.BlockHash = HashBlock(block1)
	if err := node.ReceiveBlock(block1, node.Blockchain); err != nil {
		t.Fatalf("receive block1: %v", err)
	}

	if _, ok := node.cachedExecutionSnapshotLedger(block1.ID); !ok {
		t.Fatalf("expected cached execution ledger for height %d", block1.ID)
	}

	// Simulate runtime-only drift between committed execution state and the live
	// post-effects ledger so the next commit path must choose the sealed parent
	// execution ledger explicitly.
	addBalance(&node.Ledger, CoinSymbol, TREASURY_ADDRESS, 77)

	block2 := node.BuildLeaderBlock(node.currentEpoch())
	block2.BlockTime = LogicalTimeForEpochTick(block2.ID, TickFinalize)
	block2.Timestamp = int64(SystemTimeUnits(block2.BlockTime))
	block2.BlockHash = HashBlock(block2)

	expectedExecLedger, _, ok := node.executionLedgerForBlock(block2)
	if !ok {
		t.Fatalf("expected sealed execution ledger for block2")
	}
	if HashLedger(expectedExecLedger) == HashLedger(node.Ledger) {
		t.Fatalf("test setup invalid: sealed execution ledger matches runtime ledger before commit")
	}

	if err := node.ReceiveBlock(block2, node.Blockchain); err != nil {
		t.Fatalf("receive block2: %v", err)
	}

	cachedExecLedger, ok := node.cachedExecutionSnapshotLedger(block2.ID)
	if !ok {
		t.Fatalf("expected cached execution ledger for height %d", block2.ID)
	}
	if got, want := HashLedger(cachedExecLedger), HashLedger(expectedExecLedger); got != want {
		t.Fatalf("committed execution cache mismatch: got=%q want=%q", got, want)
	}
	if got, runtime := HashLedger(cachedExecLedger), HashLedger(node.Ledger); got == runtime {
		t.Fatalf("committed execution cache incorrectly followed runtime ledger")
	}
}

func TestExecutionSafetyLockResetsRuntimeAfterRepeatedMismatch(t *testing.T) {
	node, _, execLedger, runtimeLedger := testExecutionParentNode(t)
	node.ExecutionLedger = execLedger.Clone()
	node.Ledger = runtimeLedger.Clone()
	block := Block{ID: 2, Round: 3, BlockHash: "bad-block", StateRoot: "bad-root"}
	node.storeLeaderBlock(block)
	ctx := executionRootContext{
		ParentLedgerHash:    HashLedger(execLedger),
		RuntimeLedgerHash:   HashLedger(runtimeLedger),
		ExecutionLedgerHash: HashLedger(execLedger),
	}

	for i := 0; i < 3; i++ {
		node.applyLocalExecutionSafetyLock(block, ctx, "expected-root")
	}

	if got, want := HashLedger(node.Ledger), HashLedger(node.currentExecutionLedgerClone()); got != want {
		t.Fatalf("expected safety lock to reset runtime ledger: got=%q want=%q", got, want)
	}
	if _, ok := node.getLeaderBlock(block.ID); ok {
		t.Fatalf("expected safety lock to clear stale leader block")
	}
}

func TestPostBlockEffectsUseCommittedSignersForRewards(t *testing.T) {
	oldWorkEnabled := WorkBlockRewardEnabled
	oldWorkBase := WorkBlockBaseReward
	oldRandomEnabled := RandomUserRewardEnabled
	oldRandomChance := RandomUserRewardChanceBPS
	oldEmissionEnabled := EmissionRewardEnabled
	oldMinted := TotalMintedMSC
	defer func() {
		WorkBlockRewardEnabled = oldWorkEnabled
		WorkBlockBaseReward = oldWorkBase
		RandomUserRewardEnabled = oldRandomEnabled
		RandomUserRewardChanceBPS = oldRandomChance
		EmissionRewardEnabled = oldEmissionEnabled
		TotalMintedMSC = oldMinted
	}()

	WorkBlockRewardEnabled = true
	WorkBlockBaseReward = 0
	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 0
	EmissionRewardEnabled = false
	TotalMintedMSC = 0

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C"})
	ledger := NewLedger()
	setValidatorRewardWallet(&ledger, "A", "wallet-a")
	setValidatorRewardWallet(&ledger, "B", "wallet-b")
	setValidatorRewardWallet(&ledger, "C", "wallet-c")
	block := Block{
		ID:         2,
		Type:       BlockTypeWork,
		Proposer:   "A",
		Signatures: []string{"A", "B"},
		Transactions: []Transaction{
			{ID: "fee-tx", Fee: 1000, Coin: CoinSymbol},
		},
	}

	after := node.applyPostBlockEffectsToLedger(block, ledger)
	if got := getBalance(after, CoinSymbol, "wallet-c"); got != 0 {
		t.Fatalf("non-signing validator received post-block reward: got=%d", got)
	}
	if got := getBalance(after, CoinSymbol, "wallet-a") + getBalance(after, CoinSymbol, "wallet-b"); got == 0 {
		t.Fatalf("expected signer rewards to be distributed")
	}
}

func TestPostBlockEffectsIgnoreProcessGlobalMintedSupply(t *testing.T) {
	oldWorkEnabled := WorkBlockRewardEnabled
	oldWorkBase := WorkBlockBaseReward
	oldRandomEnabled := RandomUserRewardEnabled
	oldRandomChance := RandomUserRewardChanceBPS
	oldEmissionEnabled := EmissionRewardEnabled
	oldMinted := TotalMintedMSC
	defer func() {
		WorkBlockRewardEnabled = oldWorkEnabled
		WorkBlockBaseReward = oldWorkBase
		RandomUserRewardEnabled = oldRandomEnabled
		RandomUserRewardChanceBPS = oldRandomChance
		EmissionRewardEnabled = oldEmissionEnabled
		TotalMintedMSC = oldMinted
	}()

	WorkBlockRewardEnabled = true
	WorkBlockBaseReward = 0
	RandomUserRewardEnabled = true
	RandomUserRewardChanceBPS = 0
	EmissionRewardEnabled = false

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B"})
	base := NewLedger()
	setValidatorRewardWallet(&base, "A", "wallet-a")
	setValidatorRewardWallet(&base, "B", "wallet-b")
	block := Block{
		ID:         3,
		Type:       BlockTypeWork,
		Proposer:   "A",
		Signatures: []string{"A", "B"},
		Transactions: []Transaction{
			{ID: "fee-tx", Fee: 1000, Coin: CoinSymbol},
		},
	}

	TotalMintedMSC = 0
	a := node.applyPostBlockEffectsToLedger(block, base)
	TotalMintedMSC = FixedTotalSupply
	b := node.applyPostBlockEffectsToLedger(block, base)

	if got, want := HashLedger(b), HashLedger(a); got != want {
		t.Fatalf("post-block effects depended on process-global minted supply: got=%s want=%s", got, want)
	}
}

func TestComputeExecHashVersionedDefaultsToV1(t *testing.T) {
	block := Block{
		ID:        9,
		PrevHash:  "prev-hash",
		Proposer:  "A",
		BlockTime: LogicalTimeForEpoch(9),
	}
	ledgerHash := "ledger-hash"

	if got, want := executionStateRootVersionForHeight(block.ID), executionStateRootVersionV1; got != want {
		t.Fatalf("unexpected execution root version: got=%q want=%q", got, want)
	}
	if got, want := ComputeExecHash(block, ledgerHash), ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionV1); got != want {
		t.Fatalf("versioned exec hash mismatch: got=%q want=%q", got, want)
	}
}
