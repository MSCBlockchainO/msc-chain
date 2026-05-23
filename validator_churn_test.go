package main

import (
	"crypto/ed25519"
	crypto_rand "crypto/rand"
	"fmt"
	"io"
	"path/filepath"
	"testing"
)

type validatorChurnOp struct {
	action    string
	validator string
}

func TestValidatorChurnJoinLeaveRestartActivationPipelineStable(t *testing.T) {
	restoreGlobals := withValidatorUpdateTestGlobals(t)
	defer restoreGlobals()

	oldActivationModel := ValidatorSetActivationModelV2Height
	oldCommitmentV2 := ValidatorSetCommitmentV2Height
	oldActivationDelay := ValidatorSetActivationDelay
	oldRetryMode := TransitionBarrierRetryMode
	oldCheckpointInterval := SyncCheckpointIntervalBlocks
	oldSafeMode := ConsensusPostBlockSafeModeEnabled
	oldDynamicSelection := DynamicValidatorSelectionEnabled
	oldDeterministicSelection := DeterministicValidatorSelection
	oldAuthCore := append([]string(nil), ConfigAuthCoreValidators...)
	oldScheduledTraceOutput := scheduledUpdateTraceOutput
	defer func() {
		ValidatorSetActivationModelV2Height = oldActivationModel
		ValidatorSetCommitmentV2Height = oldCommitmentV2
		ValidatorSetActivationDelay = oldActivationDelay
		TransitionBarrierRetryMode = oldRetryMode
		SyncCheckpointIntervalBlocks = oldCheckpointInterval
		ConsensusPostBlockSafeModeEnabled = oldSafeMode
		DynamicValidatorSelectionEnabled = oldDynamicSelection
		DeterministicValidatorSelection = oldDeterministicSelection
		ConfigAuthCoreValidators = oldAuthCore
		scheduledUpdateTraceOutput = oldScheduledTraceOutput
	}()

	scheduledUpdateTraceOutput = io.Discard
	ValidatorSetActivationModelV2Height = 1
	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	TransitionBarrierRetryMode = transitionBarrierRetryModePerBlock
	SyncCheckpointIntervalBlocks = 8
	ConsensusPostBlockSafeModeEnabled = false
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	ids := []string{"A", "B", "C", "D", "F", "G", "H", "I", "J"}
	signerKeys := installValidatorUpdateRegistryForIDs(t, ids)
	registry := GlobalValidatorRegistry.Snapshot()

	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.Role = "validator"
	node.GenesisValidators = []string{"A", "B", "C", "D"}
	node.currentValidators = []string{"A", "B", "C", "D"}

	_, relayerPriv, err := ed25519.GenerateKey(crypto_rand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}
	ledger := node.Ledger.Clone()
	outerNonce := 1
	proposalNonce := uint64(1000)
	parentRegistryHash := ValidatorRegistrySnapshotHash(registry)

	active := []string{"A", "B", "C", "D"}
	for height := uint64(1); height <= 7; height++ {
		validatorChurnSeedCommittedHeight(t, node, height, active, registry)
	}

	steps := []struct {
		txHeight uint64
		ops      []validatorChurnOp
		want     []string
	}{
		{txHeight: 8, ops: []validatorChurnOp{{action: "add", validator: "F"}}, want: []string{"A", "B", "C", "D", "F"}},
		{txHeight: 9, ops: []validatorChurnOp{{action: "add", validator: "G"}, {action: "remove", validator: "B"}}, want: []string{"A", "C", "D", "F", "G"}},
		{txHeight: 10, ops: []validatorChurnOp{{action: "add", validator: "H"}, {action: "remove", validator: "F"}}, want: []string{"A", "C", "D", "G", "H"}},
		{txHeight: 11, ops: []validatorChurnOp{{action: "add", validator: "I"}, {action: "remove", validator: "C"}}, want: []string{"A", "D", "G", "H", "I"}},
		{txHeight: 12, ops: []validatorChurnOp{{action: "add", validator: "J"}, {action: "remove", validator: "D"}}, want: []string{"A", "G", "H", "I", "J"}},
	}

	for _, step := range steps {
		activationHeight := step.txHeight + validatorSetActivationDelayBlocks()
		validatorChurnSeedCommittedHeight(t, node, step.txHeight, active, registry)
		validatorChurnApplyUpdateTxs(t, node, &ledger, step.txHeight, step.ops, relayerPriv, signerKeys, parentRegistryHash, &proposalNonce, &outerNonce)

		validatorChurnSeedCommittedHeight(t, node, activationHeight, active, registry)
		node.applyScheduledValidatorUpdates(activationHeight)
		validatorChurnAssertHeightSet(t, node, activationHeight, step.want)
		validatorChurnAssertNoStaleValidator(t, node, activationHeight, "STALE")

		validatorChurnSimulateRestart(t, node, activationHeight)
		if !node.hasDueValidatorTransitionAtStartup(activationHeight) {
			t.Fatalf("height %d expected due validator transition after restart", activationHeight)
		}
		if !node.recoverDueValidatorTransitionsAtStartup(activationHeight) {
			t.Fatalf("height %d failed to recover due validator transition after restart", activationHeight)
		}
		validatorChurnAssertHeightSet(t, node, activationHeight, step.want)
		validatorChurnAssertNoStaleValidator(t, node, activationHeight, "STALE")

		validatorChurnAssertPendingMatchesOps(t, node, step.ops, true)
		node.clearPendingValidatorUpdatesUpTo(activationHeight)
		validatorChurnAssertPendingMatchesOps(t, node, step.ops, false)

		active = append([]string(nil), step.want...)
	}
}

func validatorChurnApplyUpdateTxs(
	t *testing.T,
	node *Node,
	ledger *Ledger,
	height uint64,
	ops []validatorChurnOp,
	relayerPriv ed25519.PrivateKey,
	signerKeys map[string]ed25519.PrivateKey,
	parentRegistryHash string,
	proposalNonce *uint64,
	outerNonce *int,
) {
	t.Helper()

	ctx := node.newValidatorUpdateExecutionContext(height)
	if ctx == nil {
		t.Fatalf("height %d expected validator update execution context", height)
	}
	for _, op := range ops {
		if op.action != "add" && op.action != "remove" {
			t.Fatalf("unknown churn action %q", op.action)
		}
		tx := buildValidatorUpdateTestTx(t, relayerPriv, op.action, op.validator, parentRegistryHash, *proposalNonce, *outerNonce, []string{"A", "B", "C"}, signerKeys)
		(*proposalNonce)++
		(*outerNonce)++
		updatedLedger, err := ExecuteTransactionWithNodeContext(node, ctx, ledger, tx, int(height))
		if err != nil {
			t.Fatalf("height %d %s %s validator update failed: %v", height, op.action, op.validator, err)
		}
		*ledger = updatedLedger
	}
	node.validatorSetMu.Lock()
	node.pendingValidators = copyUint64Map(ctx.pendingAdds)
	node.pendingValidatorRemovals = copyUint64Map(ctx.pendingRemovals)
	node.validatorSetMu.Unlock()
}

func validatorChurnSeedCommittedHeight(t *testing.T, node *Node, height uint64, active []string, registry map[string]ValidatorRecord) {
	t.Helper()

	if height > 0 {
		for node.Blockchain.LastBlock().ID+1 < height {
			missing := node.Blockchain.LastBlock().ID + 1
			validatorChurnSeedCommittedHeight(t, node, missing, active, registry)
		}
	}
	if node.Blockchain.LastBlock().ID >= height {
		validatorChurnSeedValidatorSet(t, node, height, active, registry)
		return
	}

	prev := node.Blockchain.LastBlock()
	results := make([]ExecutionResult, 0, len(active))
	signatures := make([]string, 0, len(active))
	for _, id := range canonicalValidatorIDs(active) {
		results = append(results, ExecutionResult{Signer: id})
		signatures = append(signatures, id)
	}
	block := Block{
		ID:                    height,
		Height:                height,
		PrevHash:              prev.BlockHash,
		BlockHash:             fmt.Sprintf("churn-block-%03d-%s", height, ValidatorSetHash(active)),
		Proposer:              canonicalValidatorIDs(active)[int((height-1)%uint64(len(active)))],
		ValidatorSetHash:      validatorSetHashFromSnapshotForHeight(height, active, registry),
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
		ExecutionResults:      results,
		Signatures:            signatures,
	}
	node.Blockchain.AddBlock(block)
	if node.DB != nil {
		node.StoreBlock(block)
		if err := node.storeValidatorRegistrySnapshotRecord(height, registry); err != nil {
			t.Fatalf("store registry snapshot at height %d: %v", height, err)
		}
	}
	node.commitMu.Lock()
	node.committed[height] = block.BlockHash
	node.committedHeight = height
	node.finalizedHeight = height
	node.lastCommitHeight = height
	node.commitMu.Unlock()
	node.persistFinalizedHashInvariant(block)
	validatorChurnSeedValidatorSet(t, node, height, active, registry)
}

func validatorChurnSeedValidatorSet(t *testing.T, node *Node, height uint64, active []string, registry map[string]ValidatorRecord) {
	t.Helper()
	canonical := canonicalValidatorIDs(active)
	hash := validatorSetHashFromSnapshotForHeight(height, canonical, registry)
	node.validatorSetMu.Lock()
	if node.epochValidators == nil {
		node.epochValidators = make(map[uint64][]string)
	}
	if node.frozenValidatorsByHeight == nil {
		node.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if node.frozenValidatorHashByHeight == nil {
		node.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	node.currentValidators = append([]string(nil), canonical...)
	node.epochValidators[height] = append([]string(nil), canonical...)
	node.epochValidators[height+1] = append([]string(nil), canonical...)
	node.frozenValidatorsByHeight[height] = append([]string(nil), canonical...)
	node.frozenValidatorsByHeight[height+1] = append([]string(nil), canonical...)
	node.frozenValidatorHashByHeight[height] = hash
	node.frozenValidatorHashByHeight[height+1] = hash
	node.validatorSetMu.Unlock()
}

func validatorChurnSimulateRestart(t *testing.T, node *Node, height uint64) {
	t.Helper()
	node.validatorSetMu.Lock()
	node.currentValidators = []string{"STALE"}
	delete(node.epochValidators, height)
	delete(node.frozenValidatorsByHeight, height)
	delete(node.frozenValidatorHashByHeight, height)
	node.validatorSetMu.Unlock()
}

func validatorChurnAssertHeightSet(t *testing.T, node *Node, height uint64, want []string) {
	t.Helper()
	got := canonicalValidatorIDs(node.frozenValidatorsForHeight(height))
	want = canonicalValidatorIDs(want)
	if !sameStringSlice(got, want) {
		t.Fatalf("height %d validator set mismatch: got %v want %v", height, got, want)
	}
}

func validatorChurnAssertNoStaleValidator(t *testing.T, node *Node, height uint64, stale string) {
	t.Helper()
	for _, id := range node.frozenValidatorsForHeight(height) {
		if id == stale {
			t.Fatalf("height %d retained stale validator marker after restart", height)
		}
	}
}

func validatorChurnAssertPendingMatchesOps(t *testing.T, node *Node, ops []validatorChurnOp, wantPresent bool) {
	t.Helper()
	node.validatorSetMu.RLock()
	defer node.validatorSetMu.RUnlock()
	for _, op := range ops {
		switch op.action {
		case "add":
			_, ok := node.pendingValidators[op.validator]
			if ok != wantPresent {
				t.Fatalf("pending add %s presence=%v want %v", op.validator, ok, wantPresent)
			}
		case "remove":
			_, ok := node.pendingValidatorRemovals[op.validator]
			if ok != wantPresent {
				t.Fatalf("pending remove %s presence=%v want %v", op.validator, ok, wantPresent)
			}
		}
	}
}
