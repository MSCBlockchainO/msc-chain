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

func TestExpectedNextValidatorSetCarriesActiveSetWithoutVisibleTransition(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	oldMode := ValidatorActiveSetMode
	oldFrozen := GenesisValidatorSetFrozen
	oldFrozenSize := GenesisFrozenValidatorSetSize
	defer func() {
		ValidatorActiveSetMode = oldMode
		GenesisValidatorSetFrozen = oldFrozen
		GenesisFrozenValidatorSetSize = oldFrozenSize
	}()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ValidatorActiveSetMode = "adaptive_committee"
	GenesisValidatorSetFrozen = true
	GenesisFrozenValidatorSetSize = 4
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D"})
	registry := GlobalValidatorRegistry.Snapshot()
	registryHash := ValidatorRegistrySnapshotHash(registry)
	active := []string{"A", "B", "C"}
	activeHash := validatorSetHashFromSnapshotForHeight(10, active, registry)
	if activeHash == "" {
		t.Fatalf("active hash empty")
	}
	fullHash := validatorSetHashFromSnapshotForHeight(11, []string{"A", "B", "C", "D"}, registry)
	if fullHash == "" || strings.EqualFold(activeHash, fullHash) {
		t.Fatalf("test requires distinct active/full hashes active=%s full=%s", activeHash, fullHash)
	}

	n.Blockchain.AddBlock(Block{
		ID:                     9,
		BlockHash:              "parent-9",
		ValidatorSetHash:       activeHash,
		ValidatorRegistryHash:  registryHash,
		NextValidatorSetHash:   activeHash,
		NextValidatorSetRoot:   ValidatorSetMerkleRoot(10, active, registry),
		NextValidatorSetHeight: 10,
		ActivationHeight:       10,
	})

	block := Block{
		ID:                    10,
		PrevHash:              "parent-9",
		BlockHash:             "block-10",
		ValidatorSetHash:      activeHash,
		ValidatorSetRoot:      ValidatorSetMerkleRoot(10, active, registry),
		ValidatorRegistryHash: registryHash,
	}

	nextHash, _, source := n.expectedNextValidatorSetCommitmentForBlock(block)
	if !strings.EqualFold(nextHash, activeHash) {
		t.Fatalf("expected active-set carry-forward, got hash=%s source=%s want=%s", nextHash, source, activeHash)
	}
	if source != "carry_forward" {
		t.Fatalf("expected carry_forward source, got %s", source)
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

func TestValidatorUpdateAddUsesCommittedCommitteeOverRegistryProjection(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	oldMaxActive := ValidatorHybridMaxActiveValidators
	ValidatorHybridMaxActiveValidators = 4
	defer func() { ValidatorHybridMaxActiveValidators = oldMaxActive }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	n := newValidatorUpdateTestNode()
	n.DB = db
	signerKeys := installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D"})
	registry := GlobalValidatorRegistry.Snapshot()
	for id, rec := range registry {
		rec.Reputation = ValidatorReputationInitial
		registry[id] = rec
	}
	GlobalValidatorRegistry.Load(registry)
	parentRegistryHash := ValidatorRegistrySnapshotHash(registry)
	committedCommittee := []string{"B", "C", "D"}
	committedHash := validatorSetHashFromSnapshotForHeight(2, committedCommittee, registry)
	registryProjectionHash := validatorSetHashFromSnapshotForHeight(2, []string{"A", "B", "C", "D"}, registry)
	if committedHash == "" || registryProjectionHash == "" || strings.EqualFold(committedHash, registryProjectionHash) {
		t.Fatalf("test requires distinct committed/projection hashes committed=%s projection=%s", committedHash, registryProjectionHash)
	}

	parent := Block{
		ID:                     1,
		BlockHash:              "block-1",
		StateRoot:              "state-1",
		Signatures:             committedCommittee,
		ValidatorSetHash:       committedHash,
		ValidatorRegistryHash:  parentRegistryHash,
		NextValidatorSetHash:   committedHash,
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
	}
	n.Blockchain = &Blockchain{Blocks: []Block{parent}}
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)
	parentSnapshot := StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 1,
		BlockHash:              parent.BlockHash,
		StateRoot:              parent.StateRoot,
		Ledger:                 n.Ledger.Clone(),
		LedgerHash:             HashLedger(n.Ledger),
		GenesisHash:            GenesisHash,
		Validators:             map[string]bool{"B": true, "C": true, "D": true},
		ValidatorSetHash:       committedHash,
		ValidatorRegistry:      copyValidatorRegistrySnapshot(registry),
		ValidatorRegistryHash:  parentRegistryHash,
		NextValidatorSetHash:   committedHash,
		NextValidatorSetHeight: 2,
		ActivationHeight:       2,
	}
	storeSnapshotForHeight(t, db, parentSnapshot)

	if got := n.plannedValidatorSetForHeightFromChain(2); !containsValidatorID(got, "A") {
		t.Fatalf("test setup expected registry projection to include A, got=%v", got)
	}
	if got := n.consensusValidatorsForHeight(2); !sameStringSlice(got, committedCommittee) {
		t.Fatalf("test setup expected committed committee, got=%v want=%v", got, committedCommittee)
	}
	if got := n.validatorUpdateActiveSetForHeight(2); containsValidatorID(got, "A") || !sameStringSlice(got, committedCommittee) {
		t.Fatalf("validator update active set must use committed committee, got=%v want=%v", got, committedCommittee)
	}

	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}
	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "A", parentRegistryHash, 101, 1, []string{"B", "C", "D"}, signerKeys)
	ctx := n.newValidatorUpdateExecutionContext(2)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	if ctx.activeSetContains("A") {
		t.Fatalf("A must not be treated active before reconciliation add")
	}
	ledger := n.Ledger.Clone()
	if _, err := ExecuteTransactionWithNodeContext(n, ctx, &ledger, tx, 2); err != nil {
		t.Fatalf("expected add:A reconciliation tx to apply, got %v", err)
	}
	if _, ok := ctx.pendingAdds["A"]; !ok {
		t.Fatalf("expected A to be queued as pending add")
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

func TestValidatorUpdateProjectsMissingLedgerCandidateIntoRegistryCommitment(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D"})
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}
	fPub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate F key: %v", err)
	}
	fPubHex := strings.ToLower(hex.EncodeToString(fPub))
	n.Ledger.Stakes[stakeKey("wallet-f", "F")] = StakeLock{
		ValidatorID:     "F",
		ConsensusPubKey: fPubHex,
		Amount:          1000,
		LockedUntil:     1000,
	}

	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	n.Mempool.Transactions = []Transaction{tx}

	block := n.BuildDeterministicBlock(n.Blockchain)
	projected, projectedHash, ok := n.projectedValidatorUpdateRegistrySnapshotForBlock(block)
	if !ok {
		t.Fatalf("expected validator update registry projection")
	}
	if strings.EqualFold(projectedHash, parentRegistryHash) {
		t.Fatalf("projected registry hash should include F, got parent hash %s", projectedHash)
	}
	if block.ValidatorRegistryHash != projectedHash {
		t.Fatalf("block registry hash = %s, want projected %s", block.ValidatorRegistryHash, projectedHash)
	}
	rec, ok := projected["F"]
	if !ok {
		t.Fatalf("projected registry missing F")
	}
	if rec.Status != ValidatorActive {
		t.Fatalf("F projected status = %s, want %s", rec.Status, ValidatorActive)
	}
	if !strings.EqualFold(rec.ConsensusPubKey, fPubHex) {
		t.Fatalf("F projected pubkey mismatch")
	}
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		t.Fatalf("projected registry commitment should validate: %v", err)
	}
	preCommit, source, err := n.deterministicPreCommitRegistrySnapshot(block)
	if err != nil {
		t.Fatalf("precommit registry projection failed: %v", err)
	}
	if source != "block_tx_registry_projection" {
		t.Fatalf("precommit source = %s, want block_tx_registry_projection", source)
	}
	if preCommit["F"].Status != ValidatorActive {
		t.Fatalf("precommit F status = %s, want %s", preCommit["F"].Status, ValidatorActive)
	}
	wantNext := validatorSetHashFromSnapshotForHeight(2, []string{"A", "B", "C", "D", "F"}, projected)
	if block.NextValidatorSetHash != wantNext {
		t.Fatalf("next validator set hash = %s, want %s", block.NextValidatorSetHash, wantNext)
	}
}

func TestValidatorUpdateRejectsCandidateWithoutConsensusPubKey(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D"})
	parentRegistryHash := ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}
	n.Ledger.Stakes[stakeKey("wallet-f", "F")] = StakeLock{
		ValidatorID: "F",
		Amount:      1000,
		LockedUntil: 1000,
	}

	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	err = ctx.validateAndApply(tx, nil)
	if err == nil || err.Error() != "validator_update_missing_consensus_pubkey" {
		t.Fatalf("expected missing consensus pubkey rejection, got=%v", err)
	}
}

func TestValidatorUpdateRepairsActiveMissingConsensusPubKeyFromLedger(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	ValidatorSetCommitmentV2Height = 1
	ValidatorSetActivationDelay = 1
	DynamicValidatorSelectionEnabled = true
	DeterministicValidatorSelection = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	n := newValidatorUpdateTestNode()
	signerKeys := installValidatorUpdateRegistryForIDs(t, []string{"A", "B", "C", "D", "F"})
	parent := GlobalValidatorRegistry.Snapshot()
	fRecord := parent["F"]
	fRecord.ConsensusPubKey = ""
	parent["F"] = fRecord
	GlobalValidatorRegistry.Load(parent)
	parentRegistryHash := ValidatorRegistrySnapshotHash(parent)

	fPub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate F key: %v", err)
	}
	fPubHex := strings.ToLower(hex.EncodeToString(fPub))
	n.Ledger.Stakes[stakeKey("wallet-f", "F")] = StakeLock{
		ValidatorID:     "F",
		ConsensusPubKey: fPubHex,
		Amount:          1000,
		LockedUntil:     1000,
	}
	n.GenesisValidators = []string{"A", "B", "C", "D", "F"}
	n.frozenValidatorsByHeight[1] = append([]string{}, n.GenesisValidators...)
	n.frozenValidatorHashByHeight[1] = ValidatorSetHash(n.GenesisValidators)

	_, relayerPriv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate relayer key: %v", err)
	}
	tx := buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", parentRegistryHash, 1, 1, []string{"A", "B", "C"}, signerKeys)
	ctx := n.newValidatorUpdateExecutionContext(1)
	if ctx == nil {
		t.Fatalf("expected validator update execution context")
	}
	if err := ctx.validateAndApply(tx, nil); err != nil {
		t.Fatalf("active missing-key repair should validate: %v", err)
	}
	repaired := ctx.registrySnapshot["F"]
	if !strings.EqualFold(repaired.ConsensusPubKey, fPubHex) {
		t.Fatalf("repaired F pubkey = %q, want %q", repaired.ConsensusPubKey, fPubHex)
	}
	if got := ctx.projectedRegistryHash(); got == "" || strings.EqualFold(got, parentRegistryHash) {
		t.Fatalf("repair must produce a new registry commitment, got=%q parent=%q", got, parentRegistryHash)
	}

	GlobalValidatorRegistry.Load(ctx.registrySnapshot)
	second := n.newValidatorUpdateExecutionContext(1)
	if second == nil {
		t.Fatalf("expected second validator update execution context")
	}
	tx = buildValidatorUpdateTestTx(t, relayerPriv, "add", "F", second.expectedRegistryHash, 2, 2, []string{"A", "B", "C"}, signerKeys)
	if err := second.validateAndApply(tx, nil); err == nil || err.Error() != "validator_update_already_active" {
		t.Fatalf("active validator with anchored key must not be re-added, got=%v", err)
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

func TestExpectedValidatorSetRootUsesParentCommittedNextRoot(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D", "F"})
	parent := Block{
		ID:                   1,
		BlockHash:            "parent-block",
		ValidatorSetHash:     "parent-set",
		ValidatorSetRoot:     "parent-root",
		NextValidatorSetHash: "child-set",
		NextValidatorSetRoot: "child-root",
	}
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{{ID: 0, BlockHash: "genesis"}, parent}
	node.Blockchain.mu.Unlock()

	got, source := node.expectedValidatorSetRootWithSource(2)
	if got != parent.NextValidatorSetRoot || source != "chain_parent_commitment" {
		t.Fatalf("expected parent-committed child root, got root=%q source=%q", got, source)
	}
}

func TestCarryForwardNextValidatorSetIgnoresLocalPendingRemoval(t *testing.T) {
	node := &Node{
		pendingValidatorRemovals: map[string]uint64{"F": 3},
	}
	block := Block{
		ID:               2,
		ValidatorSetHash: "active-set",
		ValidatorSetRoot: "active-root",
	}

	hash, root, source := node.carryForwardNextValidatorSetCommitmentForBlock(block)
	if hash != block.ValidatorSetHash || root != block.ValidatorSetRoot || source != "carry_forward" {
		t.Fatalf("expected strict carry-forward without on-chain update, got hash=%q root=%q source=%q", hash, root, source)
	}
}
