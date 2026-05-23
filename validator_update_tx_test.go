package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func withValidatorUpdateTestGlobals(t *testing.T) func() {
	t.Helper()

	oldV2 := ValidatorSetCommitmentV2Height
	oldDelay := ValidatorSetActivationDelay
	oldDyn := DynamicValidatorSelectionEnabled
	oldDet := DeterministicValidatorSelection
	oldCore := append([]string{}, ConfigAuthCoreValidators...)
	oldRegistry := GlobalValidatorRegistry.Snapshot()

	validatorPubKeysMu.RLock()
	oldPub := make(map[string]ed25519.PublicKey, len(ValidatorPubKeys))
	for id, pub := range ValidatorPubKeys {
		oldPub[id] = append(ed25519.PublicKey(nil), pub...)
	}
	oldGenesisPub := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pub := range GenesisValidatorPubKeys {
		oldGenesisPub[id] = append(ed25519.PublicKey(nil), pub...)
	}
	validatorPubKeysMu.RUnlock()

	return func() {
		ValidatorSetCommitmentV2Height = oldV2
		ValidatorSetActivationDelay = oldDelay
		DynamicValidatorSelectionEnabled = oldDyn
		DeterministicValidatorSelection = oldDet
		ConfigAuthCoreValidators = oldCore
		GlobalValidatorRegistry.Load(oldRegistry)

		validatorPubKeysMu.Lock()
		ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldPub))
		for id, pub := range oldPub {
			ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisPub))
		for id, pub := range oldGenesisPub {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		validatorPubKeysMu.Unlock()
	}
}

func newValidatorUpdateTestNode() *Node {
	bc := NewBlockchain()
	n := &Node{
		ID:                          "A",
		Blockchain:                  &bc,
		Ledger:                      GenesisLedger(),
		GenesisValidators:           []string{"A", "B", "C", "D"},
		Mempool:                     Mempool{SeenTxIDs: make(map[string]bool), txByID: make(map[string]struct{}), txBySenderNonce: make(map[string]string), pendingCountBySender: make(map[string]int), nextNonceBySender: make(map[string]int)},
		validatorStatus:             make(map[string]*ValidatorStatus),
		epochValidators:             make(map[uint64][]string),
		frozenValidatorsByHeight:    make(map[uint64][]string),
		frozenValidatorHashByHeight: make(map[uint64]string),
		pendingValidators:           make(map[string]uint64),
		pendingValidatorRemovals:    make(map[string]uint64),
	}
	_ = n.freezeValidatorSetForHeight(1, n.GenesisValidators)
	return n
}

func installValidatorUpdateRegistry(t *testing.T) map[string]ed25519.PrivateKey {
	t.Helper()

	return installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D", "F"})
}

func installValidatorUpdateRegistryForIDs(t *testing.T, ids []string) map[string]ed25519.PrivateKey {
	t.Helper()

	keys := make(map[string]ed25519.PrivateKey)
	snapshot := make(map[string]ValidatorRecord)
	for _, id := range ids {
		pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
		if err != nil {
			t.Fatalf("generate validator key %s: %v", id, err)
		}
		keys[id] = priv
		snapshot[id] = ValidatorRecord{
			ID:               id,
			Stake:            1000,
			Status:           ValidatorActive,
			ConsensusPubKey:  strings.ToLower(hex.EncodeToString(pub)),
			GovernanceSigner: containsValidatorID([]string{"A", "B", "C", "D"}, id),
		}
		validatorPubKeysMu.Lock()
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		validatorPubKeysMu.Unlock()
	}
	GlobalValidatorRegistry.Load(snapshot)
	return keys
}

func buildValidatorUpdateTestTx(
	t *testing.T,
	relayerPriv ed25519.PrivateKey,
	action string,
	target string,
	parentRegistryHash string,
	proposalNonce uint64,
	outerNonce int,
	signerIDs []string,
	signerKeys map[string]ed25519.PrivateKey,
) Transaction {
	t.Helper()

	relayerPub := relayerPriv.Public().(ed25519.PublicKey)
	targetID := normalizeValidatorID(target)
	tx := Transaction{
		From:      AddressFromPublicKey(relayerPub),
		To:        action + ":" + targetID,
		Amount:    0,
		Nonce:     outerNonce,
		PublicKey: hex.EncodeToString(relayerPub),
		Fee:       0,
		Expiry:    time.Now().Add(5 * time.Minute).Unix(),
		ChainID:   ChainID,
		Coin:      CoinSymbol,
		Type:      TxValidatorUpdate,
	}

	cert := &ValidatorUpdateCertificate{
		ParentRegistryHash: parentRegistryHash,
		ProposalNonce:      proposalNonce,
		ExpiryHeight:       100,
	}
	payload := validatorUpdateCertSigningPayload(tx.ChainID, action, targetID, parentRegistryHash, proposalNonce, cert.ExpiryHeight)
	for _, signerID := range signerIDs {
		priv, ok := signerKeys[signerID]
		if !ok {
			t.Fatalf("missing signer key for %s", signerID)
		}
		cert.Signatures = append(cert.Signatures, ValidatorUpdateCertSignature{
			SignerID: signerID,
			SigHex:   strings.ToLower(hex.EncodeToString(ed25519.Sign(priv, payload))),
		})
	}
	if err := AttachValidatorUpdateCertificate(&tx, cert, relayerPriv); err != nil {
		t.Fatalf("attach validator update cert: %v", err)
	}
	return tx
}

func TestValidatorUpdateTxAcceptedWithThresholdCert(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	ok, reason := n.ReceiveTransactionWithReason(tx)
	if !ok {
		t.Fatalf("expected validator update tx accepted, reason=%s", reason)
	}
	if len(n.Mempool.Transactions) != 1 {
		t.Fatalf("expected tx in mempool, got %d", len(n.Mempool.Transactions))
	}
}

func TestValidatorUpdateTxRejectsInsufficientGovernanceSignatures(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B"}, signerKeys)
	ok, reason := n.ReceiveTransactionWithReason(tx)
	if ok {
		t.Fatalf("expected validator update tx rejected")
	}
	if !strings.Contains(reason, "insufficient") {
		t.Fatalf("unexpected reject reason: %s", reason)
	}
}

func TestValidatorUpdateCertificateReplayRejected(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	tx1 := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 7, 1, []string{"A", "B", "C"}, signerKeys)
	tx2 := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 7, 2, []string{"A", "B", "C"}, signerKeys)

	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	ledger := n.Ledger.Clone()
	updatedLedger, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, tx1, 1)
	if err != nil {
		t.Fatalf("first validator update execution failed: %v", err)
	}
	ledger = updatedLedger
	if _, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, tx2, 1); err == nil {
		t.Fatalf("expected replay rejection")
	} else if !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("unexpected replay error: %v", err)
	}
}

func TestValidatorUpdateRemoveCancelsPendingAdd(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	txAdd := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 10, 1, []string{"A", "B", "C"}, signerKeys)
	txRemove := buildValidatorUpdateTestTx(t, relayerPriv, "remove", "F", parentRegistryHash, 11, 2, []string{"A", "B", "C"}, signerKeys)

	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	ledger := n.Ledger.Clone()
	updatedLedger, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, txAdd, 1)
	if err != nil {
		t.Fatalf("add validator update failed: %v", err)
	}
	ledger = updatedLedger
	if _, ok := ctx.pendingAdds["F"]; !ok {
		t.Fatalf("expected pending add for F")
	}

	if _, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, txRemove, 1); err != nil {
		t.Fatalf("remove validator update failed: %v", err)
	}
	if _, ok := ctx.pendingAdds["F"]; ok {
		t.Fatalf("expected pending add canceled")
	}
	if _, ok := ctx.pendingRemovals["F"]; ok {
		t.Fatalf("expected no pending removal when canceling pending add")
	}
}

func TestValidatorUpdateAddCancelsPendingRemoval(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	txRemove := buildValidatorUpdateTestTx(t, relayerPriv, "remove", "A", parentRegistryHash, 20, 1, []string{"A", "B", "C"}, signerKeys)
	txAdd := buildValidatorUpdateTestTx(t, relayerPriv, "add", "A", parentRegistryHash, 21, 2, []string{"A", "B", "C"}, signerKeys)

	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	ledger := n.Ledger.Clone()
	updatedLedger, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, txRemove, 1)
	if err != nil {
		t.Fatalf("remove validator update failed: %v", err)
	}
	ledger = updatedLedger
	if _, ok := ctx.pendingRemovals["A"]; !ok {
		t.Fatalf("expected pending removal for A")
	}

	if _, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, txAdd, 1); err != nil {
		t.Fatalf("add validator rollback failed: %v", err)
	}
	if _, ok := ctx.pendingRemovals["A"]; ok {
		t.Fatalf("expected pending removal canceled")
	}
	if !ctx.activeSetContains("A") {
		t.Fatalf("A should remain part of active baseline after rollback")
	}
}

func TestValidatorUpdateRepeatedActiveAndInactiveChurnLeavesNoPendingState(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	ledger := n.Ledger.Clone()
	outerNonce := 1
	proposalNonce := uint64(100)

	apply := func(action, target string) {
		t.Helper()
		tx := buildValidatorUpdateTestTx(t, relayerPriv, action, target, parentRegistryHash, proposalNonce, outerNonce, []string{"A", "B", "C"}, signerKeys)
		proposalNonce++
		outerNonce++
		updatedLedger, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, tx, 1)
		if err != nil {
			t.Fatalf("%s %s failed: %v", action, target, err)
		}
		ledger = updatedLedger
	}

	for i := 0; i < 20; i++ {
		apply("remove", "A")
		apply("add", "A")
		apply("add", "F")
		apply("remove", "F")
		if len(ctx.pendingAdds) != 0 || len(ctx.pendingRemovals) != 0 {
			t.Fatalf("cycle %d left pending state: adds=%v removals=%v", i, ctx.pendingAdds, ctx.pendingRemovals)
		}
	}
}

func TestValidatorUpdateAddsFiveNewValidatorsStablePlan(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D", "G", "H", "I", "J", "K"})
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	ledger := n.Ledger.Clone()
	for i, id := range []string{"G", "H", "I", "J", "K"} {
		tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", id, parentRegistryHash, uint64(200+i), i+1, []string{"A", "B", "C"}, signerKeys)
		updatedLedger, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, tx, 1)
		if err != nil {
			t.Fatalf("add validator %s failed: %v", id, err)
		}
		ledger = updatedLedger
	}

	if len(ctx.pendingAdds) != 5 {
		t.Fatalf("expected five pending adds, got=%v", ctx.pendingAdds)
	}
	planned := ctx.plannedValidatorsForHeight(2)
	want := []string{"A", "B", "C", "D", "G", "H", "I", "J", "K"}
	if !sameStringSlice(canonicalValidatorIDs(planned), canonicalValidatorIDs(want)) {
		t.Fatalf("planned validator set mismatch: got=%v want=%v", planned, want)
	}
}

func TestValidatorUpdateDelayOneCommitsNextSetHash(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistry(t)
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}

	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	n.Mempool.Transactions = []Transaction{tx}

	block := n.BuildDeterministicBlock(n.Blockchain)
	want := validatorSetHashFromSnapshotForHeight(2, []string{"A", "B", "C", "D", "F"}, GlobalValidatorRegistry.Snapshot())
	if block.NextValidatorSetHash != want {
		t.Fatalf("unexpected next validator set hash: got=%s want=%s", block.NextValidatorSetHash, want)
	}
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		t.Fatalf("next validator set commitment should validate: %v", err)
	}
}

func TestValidatorUpdateCommitmentHeightUsesStrictParentCommitmentPath(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 10
	ValidatorSetActivationDelay = 5

	if got := validatorUpdateCommitmentHeight(10); got != 10 {
		t.Fatalf("strict validator update commitment height must use inclusion height: got=%d want=%d", got, 10)
	}
	if got := validatorUpdateCommitmentHeight(9); got != 13 {
		t.Fatalf("legacy validator update commitment height must retain delay behavior before fork: got=%d want=%d", got, 13)
	}
}

func TestValidatorRegistryHashIncludesGovernanceSignerFlag(t *testing.T) {
	snapshot := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 1000, Status: ValidatorActive, ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize), GovernanceSigner: true},
	}
	base := ValidatorRegistrySnapshotHash(snapshot)
	mutated := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 1000, Status: ValidatorActive, ConsensusPubKey: strings.Repeat("11", ed25519.PublicKeySize), GovernanceSigner: false},
	}
	other := ValidatorRegistrySnapshotHash(mutated)
	if base == other {
		t.Fatalf("expected governance signer flag to affect registry hash")
	}
}
