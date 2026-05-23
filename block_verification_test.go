package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

func verificationTestFinalBlock(t *testing.T, node *Node) Block {
	t.Helper()
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)
	return block
}

func installValidatorPubKeyForTest(t *testing.T, node *Node, validatorID string) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}
	id := normalizeValidatorID(validatorID)
	validatorPubKeysMu.Lock()
	if ValidatorPubKeys == nil {
		ValidatorPubKeys = make(map[string]ed25519.PublicKey)
	}
	if GenesisValidatorPubKeys == nil {
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)
	}
	oldRuntime, hadRuntime := ValidatorPubKeys[id]
	oldGenesis, hadGenesis := GenesisValidatorPubKeys[id]
	ValidatorPubKeys[id] = pub
	GenesisValidatorPubKeys[id] = pub
	validatorPubKeysMu.Unlock()
	if node != nil {
		node.ValidatorKey = ValidatorKey{ID: id, PublicKey: pub, PrivateKey: priv}
	}
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		if hadRuntime {
			ValidatorPubKeys[id] = oldRuntime
		} else {
			delete(ValidatorPubKeys, id)
		}
		if hadGenesis {
			GenesisValidatorPubKeys[id] = oldGenesis
		} else {
			delete(GenesisValidatorPubKeys, id)
		}
		validatorPubKeysMu.Unlock()
	})
	return priv
}

func signBlockHashForTest(block *Block, priv ed25519.PrivateKey) {
	block.BlockHash = HashBlock(*block)
	block.Signature = ed25519.Sign(priv, []byte(block.BlockHash))
}

func TestVerifyBlockRejectsDuplicateCommitSigner(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.Signatures = []string{"A", "A"}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "duplicate_block_signature_signer" {
		t.Fatalf("expected duplicate signer rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsExecutionSignerOutsideFrozenSet(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.ExecutionResults = []ExecutionResult{{
		Height:     block.ID,
		BlockHash:  block.BlockHash,
		Signer:     "Z",
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
	}}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "execution_signer_not_validator") {
		t.Fatalf("expected non-validator execution signer rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsInvalidTimestampEnvelope(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.Timestamp++
	block.BlockHash = HashBlock(block)

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "invalid_timestamp" {
		t.Fatalf("expected invalid timestamp rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsWeakNormalQuorumMetadata(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = 3
	block.RequiredQuorum = 2
	block.StrictQuorum = 3
	block.BlockHash = HashBlock(block)

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "quorum_metadata_weak_normal") {
		t.Fatalf("expected weak NORMAL quorum metadata rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsFakeFinalizedProofWithoutExecutionQuorum(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = 3
	block.RequiredQuorum = 3
	block.StrictQuorum = 3
	block.Signatures = []string{"A", "B", "C"}
	block.BlockHash = HashBlock(block)
	block.ExecutionResults = []ExecutionResult{
		{Height: block.ID, BlockHash: block.BlockHash, Signer: "A", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
		{Height: block.ID, BlockHash: block.BlockHash, Signer: "B", ResultHash: block.StateRoot, TxMerkle: block.MempoolRoot},
	}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "execution_quorum_evidence_shortfall") {
		t.Fatalf("expected fake finalized proof rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsInvalidExecutionResultSignature(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	priv := installValidatorPubKeyForTest(t, nil, "A")
	badSig := ed25519.Sign(priv, execResultSignBytesV2(block.ID, block.Round, block.BlockHash, "different-state", block.MempoolRoot))
	block.ExecutionResults = []ExecutionResult{{
		Height:     block.ID,
		BlockHash:  block.BlockHash,
		Signer:     "A",
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
		Signature:  hex.EncodeToString(badSig),
	}}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "invalid_execution_result_signature" {
		t.Fatalf("expected invalid execution result signature rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsMainnetExecutionResultWithoutSignature(t *testing.T) {
	old := IsTestnet
	IsTestnet = false
	defer func() { IsTestnet = old }()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	priv := installValidatorPubKeyForTest(t, node, block.Proposer)
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = 3
	block.RequiredQuorum = 3
	block.StrictQuorum = 3
	signBlockHashForTest(&block, priv)
	block.ExecutionResults = []ExecutionResult{{
		Height:     block.ID,
		BlockHash:  block.BlockHash,
		Signer:     "A",
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
	}}

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "execution_result_signature_missing" {
		t.Fatalf("expected missing mainnet execution result signature rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsUnsignedMainnetBlock(t *testing.T) {
	old := IsTestnet
	IsTestnet = false
	defer func() { IsTestnet = old }()

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)
	block.Signature = nil

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "invalid_block_signature" {
		t.Fatalf("expected unsigned mainnet block rejection, got %v", err)
	}
}

func TestVerifyBlockRejectsBadTransactionSignature(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := verificationTestFinalBlock(t, node)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tx := Transaction{
		From:      AddressFromPublicKey(pub),
		To:        AddressFromPublicKey(pub),
		Amount:    1,
		Fee:       ComputeTxFee(1),
		Nonce:     1,
		PublicKey: hex.EncodeToString(pub),
		Signature: hex.EncodeToString(make([]byte, ed25519.SignatureSize)),
		Expiry:    time.Now().Add(time.Minute).Unix(),
		ChainID:   ChainID,
		Coin:      CoinSymbol,
	}
	tx.ID = ComputeTxID(tx)
	block.Transactions = []Transaction{tx}
	block.MempoolRoot = ComputeMempoolRoot(block.Transactions)
	block.BlockHash = HashBlock(block)

	err = node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "signature verification failed" {
		t.Fatalf("expected transaction signature rejection, got %v", err)
	}
}

func TestVerifyExecutionResultSignatureAcceptsProposalHashOnFinalBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const signer = "SIGTEST"
	validatorPubKeysMu.Lock()
	oldRuntime, hadRuntime := ValidatorPubKeys[signer]
	oldGenesis, hadGenesis := GenesisValidatorPubKeys[signer]
	ValidatorPubKeys[signer] = pub
	GenesisValidatorPubKeys[signer] = pub
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		if hadRuntime {
			ValidatorPubKeys[signer] = oldRuntime
		} else {
			delete(ValidatorPubKeys, signer)
		}
		if hadGenesis {
			GenesisValidatorPubKeys[signer] = oldGenesis
		} else {
			delete(GenesisValidatorPubKeys, signer)
		}
		validatorPubKeysMu.Unlock()
	})

	block := Block{
		ID:          7,
		Round:       2,
		PrevHash:    "parent",
		Proposer:    "A",
		Type:        BlockTypeTime,
		BlockTime:   LogicalTimeForEpochTick(7, TickFinalize),
		StateRoot:   strings.Repeat("a", 64),
		MempoolRoot: strings.Repeat("b", 64),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)
	proposalHash := executionVoteProposalHashForFinalBlock(block)
	sig := ed25519.Sign(priv, execResultSignBytesV2(block.ID, block.Round, proposalHash, block.StateRoot, block.MempoolRoot))

	result := ExecutionResult{
		Height:     block.ID,
		BlockHash:  block.BlockHash,
		Signer:     signer,
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
		Signature:  hex.EncodeToString(sig),
	}
	if err := verifyBlockExecutionResultSignature(result, block); err != nil {
		t.Fatalf("expected proposal-hash execution signature to verify on final block: %v", err)
	}
}

func TestVerifyExecutionResultSignatureUsesStoredVoteRound(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const signer = "ROUNDTEST"
	validatorPubKeysMu.Lock()
	oldRuntime, hadRuntime := ValidatorPubKeys[signer]
	oldGenesis, hadGenesis := GenesisValidatorPubKeys[signer]
	ValidatorPubKeys[signer] = pub
	GenesisValidatorPubKeys[signer] = pub
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		if hadRuntime {
			ValidatorPubKeys[signer] = oldRuntime
		} else {
			delete(ValidatorPubKeys, signer)
		}
		if hadGenesis {
			GenesisValidatorPubKeys[signer] = oldGenesis
		} else {
			delete(GenesisValidatorPubKeys, signer)
		}
	})

	block := Block{
		ID:          11,
		Round:       40,
		PrevHash:    "parent",
		Proposer:    "C",
		Type:        BlockTypeTime,
		BlockTime:   LogicalTimeForEpochTick(11, TickFinalize),
		StateRoot:   strings.Repeat("c", 64),
		MempoolRoot: strings.Repeat("d", 64),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)
	proposalHash := executionVoteProposalHashForFinalBlock(block)
	const voteRound uint32 = 38
	sig := ed25519.Sign(priv, execResultSignBytesV2(block.ID, voteRound, proposalHash, block.StateRoot, block.MempoolRoot))

	result := ExecutionResult{
		Height:     block.ID,
		Round:      voteRound,
		BlockHash:  proposalHash,
		Signer:     signer,
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
		Signature:  hex.EncodeToString(sig),
	}
	if err := verifyBlockExecutionResultSignature(result, block); err != nil {
		t.Fatalf("expected stored round execution signature to verify: %v", err)
	}
	if err := (&Node{}).verifyBlockConsensusEvidence(block, []string{signer}); err != nil {
		t.Fatalf("expected proposal-hash execution result block reference to pass consensus proof: %v", err)
	}
}

func TestVerifyBlockConsensusEvidenceAcceptsSignedOriginalProposalHash(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const signer = "PROPOSALHASH"
	validatorPubKeysMu.Lock()
	oldRuntime, hadRuntime := ValidatorPubKeys[signer]
	oldGenesis, hadGenesis := GenesisValidatorPubKeys[signer]
	ValidatorPubKeys[signer] = pub
	GenesisValidatorPubKeys[signer] = pub
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		defer validatorPubKeysMu.Unlock()
		if hadRuntime {
			ValidatorPubKeys[signer] = oldRuntime
		} else {
			delete(ValidatorPubKeys, signer)
		}
		if hadGenesis {
			GenesisValidatorPubKeys[signer] = oldGenesis
		} else {
			delete(GenesisValidatorPubKeys, signer)
		}
	})

	block := Block{
		ID:                  12,
		Round:               7,
		PrevHash:            "parent",
		Proposer:            signer,
		Type:                BlockTypeTime,
		BlockTime:           LogicalTimeForEpochTick(12, TickFinalize),
		StateRoot:           strings.Repeat("e", 64),
		MempoolRoot:         strings.Repeat("f", 64),
		ConsensusMode:       "DEGRADED",
		QuorumPolicyVersion: quorumPolicyVersionV1,
		ActiveReadyCount:    1,
		RequiredQuorum:      1,
		StrictQuorum:        1,
		Signatures:          []string{signer},
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)
	originalProposalHash := strings.Repeat("a", 64)
	sig := ed25519.Sign(priv, execResultSignBytesV2(block.ID, block.Round, originalProposalHash, block.StateRoot, block.MempoolRoot))
	block.ExecutionResults = []ExecutionResult{{
		Height:     block.ID,
		Round:      block.Round,
		BlockHash:  originalProposalHash,
		Signer:     signer,
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
		Signature:  hex.EncodeToString(sig),
	}}

	if err := (&Node{}).verifyBlockConsensusEvidence(block, []string{signer}); err != nil {
		t.Fatalf("expected signed original proposal hash to pass consensus proof: %v", err)
	}
}

func TestDeterministicReplaySameBlocksSameStateRootAndFinalizedHash(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate sender key: %v", err)
	}
	from := AddressFromPublicKey(pub)
	base := NewLedger()
	addBalance(&base, CoinSymbol, from, 1000)

	sourceNode := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	mirrorNode := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	sourceLedger := base.Clone()
	mirrorLedger := base.Clone()
	prevHash := GenesisHash
	var sealed []Block

	for i := 1; i <= 5; i++ {
		tx := Transaction{
			From:      from,
			To:        fmt.Sprintf("receiver-%d", i),
			Amount:    10 + i,
			Nonce:     i,
			PublicKey: hex.EncodeToString(pub),
			Expiry:    time.Now().Add(time.Hour).Unix(),
			Type:      TxTransfer,
			ChainID:   ChainID,
			Coin:      CoinSymbol,
		}
		tx.Fee = requiredFeeForTxWithLedger(&sourceLedger, tx)
		tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
		tx.ID = ComputeTxID(tx)

		block := Block{
			ID:           uint64(i),
			Height:       uint64(i),
			PrevHash:     prevHash,
			Proposer:     "A",
			Type:         BlockTypeWork,
			Transactions: []Transaction{tx},
			BlockTime:    LogicalTimeForEpochTick(uint64(i), TickFinalize),
			MempoolRoot:  ComputeMempoolRoot([]Transaction{tx}),
		}
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

		nextSource, err := ApplyBlockStateWithNode(sourceNode, sourceLedger, block)
		if err != nil {
			t.Fatalf("apply block %d on source: %v", i, err)
		}
		nextMirror, err := ApplyBlockStateWithNode(mirrorNode, mirrorLedger, block)
		if err != nil {
			t.Fatalf("apply block %d on mirror: %v", i, err)
		}
		if got, want := HashLedger(nextSource), HashLedger(nextMirror); got != want {
			t.Fatalf("deterministic ledger mismatch while sealing block %d: source=%s mirror=%s", i, got, want)
		}

		sourceRoot := ComputeExecHashVersioned(block, HashLedger(nextSource), executionStateRootVersionForHeight(block.ID))
		mirrorRoot := ComputeExecHashVersioned(block, HashLedger(nextMirror), executionStateRootVersionForHeight(block.ID))
		if sourceRoot != mirrorRoot {
			t.Fatalf("deterministic state root mismatch while sealing block %d: source=%s mirror=%s", i, sourceRoot, mirrorRoot)
		}
		block.StateRoot = sourceRoot
		block.BlockHash = HashBlock(block)
		if block.BlockHash == "" {
			t.Fatalf("empty finalized hash at block %d", i)
		}

		sourceLedger = nextSource
		mirrorLedger = nextMirror
		prevHash = block.BlockHash
		sealed = append(sealed, block)
	}

	replayNode := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	replayLedger := base.Clone()
	replayPrevHash := GenesisHash
	for _, block := range sealed {
		if block.PrevHash != replayPrevHash {
			t.Fatalf("sealed chain continuity broken at height %d: got prev=%s want=%s", block.ID, block.PrevHash, replayPrevHash)
		}
		nextReplay, err := ApplyBlockStateWithNode(replayNode, replayLedger, block)
		if err != nil {
			t.Fatalf("fresh replay block %d: %v", block.ID, err)
		}
		replayRoot := ComputeExecHashVersioned(block, HashLedger(nextReplay), executionStateRootVersionForHeight(block.ID))
		if replayRoot != block.StateRoot {
			t.Fatalf("fresh replay state root mismatch at height %d: got=%s want=%s", block.ID, replayRoot, block.StateRoot)
		}
		replayHash := HashBlock(block)
		if replayHash != block.BlockHash {
			t.Fatalf("fresh replay finalized hash mismatch at height %d: got=%s want=%s", block.ID, replayHash, block.BlockHash)
		}
		replayNode.Blockchain.AddBlock(block)
		replayNode.commitMu.Lock()
		if replayNode.committed == nil {
			replayNode.committed = make(map[uint64]string)
		}
		replayNode.committed[block.ID] = block.BlockHash
		replayNode.committedHeight = block.ID
		replayNode.finalizedHeight = block.ID
		replayNode.lastCommitHeight = block.ID
		replayNode.commitMu.Unlock()
		if err := replayNode.persistFinalizedHashInvariant(block); err != nil {
			t.Fatalf("persist replay finalized hash height %d: %v", block.ID, err)
		}
		if got, found, err := replayNode.loadFinalizedHashInvariant(block.ID); err != nil || !found || got != block.BlockHash {
			t.Fatalf("fresh replay finalized hash invariant height %d got=%q found=%t err=%v want=%q", block.ID, got, found, err, block.BlockHash)
		}
		replayLedger = nextReplay
		replayPrevHash = block.BlockHash
	}

	if got, want := HashLedger(replayLedger), HashLedger(sourceLedger); got != want {
		t.Fatalf("fresh replay final ledger mismatch: got=%s want=%s", got, want)
	}
	final := sealed[len(sealed)-1]
	if replayNode.getFinalizedHeight() != final.ID {
		t.Fatalf("fresh replay finalized height mismatch: got=%d want=%d", replayNode.getFinalizedHeight(), final.ID)
	}
	if !replayNode.hasCommittedDifferentHash(final.ID, "fork-"+final.BlockHash) {
		t.Fatal("fresh replay finalized hash invariant must reject same-height fork")
	}

	replayNode.commitMu.Lock()
	replayNode.committed = make(map[uint64]string)
	replayNode.committedHeight = 0
	replayNode.finalizedHeight = 0
	replayNode.lastCommitHeight = 0
	replayNode.commitMu.Unlock()
	replayNode.restoreCommittedHeightFromChain()
	if got := replayNode.getFinalizedHeight(); got != final.ID {
		t.Fatalf("fresh replay startup restore finalized height mismatch: got=%d want=%d", got, final.ID)
	}
	if got, ok := replayNode.getCommittedHash(final.ID); !ok || got != final.BlockHash {
		t.Fatalf("fresh replay startup restore finalized hash mismatch: got=%q ok=%t want=%q", got, ok, final.BlockHash)
	}
}

func FuzzVerifyBlockQuorumMetadata(f *testing.F) {
	f.Add("NORMAL", quorumPolicyVersionV1, 3, 3, 3, 4)
	f.Add("DEGRADED", quorumPolicyVersionV1, 3, 3, 3, 4)
	f.Add("RECOVERY", quorumPolicyVersionV1, 3, 3, 3, 4)
	f.Add("BROKEN", "", 0, 0, 0, 0)
	f.Fuzz(func(t *testing.T, mode string, version string, activeReady int, required int, strict int, validatorCount int) {
		block := Block{
			ConsensusMode:       mode,
			QuorumPolicyVersion: version,
			ActiveReadyCount:    activeReady,
			RequiredQuorum:      required,
			StrictQuorum:        strict,
		}
		err := verifyBlockQuorumMetadata(block, validatorCount)
		if err == nil {
			normalized := strings.ToUpper(strings.TrimSpace(mode))
			if normalized == "" {
				normalized = "NORMAL"
			}
			if normalized != "NORMAL" && normalized != "DEGRADED" && normalized != "RECOVERY" {
				t.Fatalf("unknown mode passed: %q", mode)
			}
			if strings.TrimSpace(version) == "" || required <= 0 || strict <= 0 {
				t.Fatalf("incomplete quorum metadata passed: mode=%q version=%q required=%d strict=%d", mode, version, required, strict)
			}
			if activeReady > 0 && validatorCount >= 0 && activeReady > validatorCount {
				t.Fatalf("active ready above validator count passed: ready=%d validators=%d", activeReady, validatorCount)
			}
			if required > strict {
				t.Fatalf("required above strict passed: required=%d strict=%d", required, strict)
			}
			if required < strict {
				t.Fatalf("weak quorum passed: mode=%s required=%d strict=%d", normalized, required, strict)
			}
		}
	})
}
