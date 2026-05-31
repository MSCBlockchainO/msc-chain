package main

import (
	"strings"
	"testing"
)

func finalityLayerTestBlock(t *testing.T, node *Node, validators []string) Block {
	t.Helper()
	block := verificationTestFinalBlock(t, node)
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = len(validators)
	block.RequiredQuorum = strictExecSupermajority(len(validators))
	block.StrictQuorum = strictExecSupermajority(len(validators))
	block.Signatures = canonicalValidatorIDs(validators[:block.RequiredQuorum])
	if strings.TrimSpace(block.ValidatorSetHash) == "" {
		block.ValidatorSetHash = ValidatorSetHash(validators)
	}
	if strings.TrimSpace(block.ValidatorSetRoot) == "" {
		block.ValidatorSetRoot = HashStrings(append([]string{"validator-set-root"}, validators...))
	}
	if strings.TrimSpace(block.NextValidatorSetHash) == "" {
		block.NextValidatorSetHash = block.ValidatorSetHash
	}
	node.attachFinalityCommitments(&block)
	block.BlockHash = HashBlock(block)
	node.attachFinalityCertificate(&block)
	return block
}

func TestFinalityCommitmentsAttachEpochAnchorAndValidatorSet(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)

	if strings.TrimSpace(block.FinalityRoot) == "" {
		t.Fatal("finality root missing")
	}
	if strings.TrimSpace(block.EpochAnchorHash) == "" {
		t.Fatal("epoch anchor missing")
	}
	if block.FinalityCertificate == nil {
		t.Fatal("finality certificate missing")
	}
	if got, want := block.FinalizedValidatorSetHash, block.ValidatorSetHash; got != want {
		t.Fatalf("finalized validator set hash = %q, want %q", got, want)
	}
	if got, want := block.FinalizedValidatorSetRoot, block.ValidatorSetRoot; got != want {
		t.Fatalf("finalized validator set root = %q, want %q", got, want)
	}
	if got, want := block.FinalityCertificate.FinalizedValidatorSetRoot, block.ValidatorSetRoot; got != want {
		t.Fatalf("certificate finalized validator set root = %q, want %q", got, want)
	}
	if err := node.verifyFinalityCommitments(block, validators); err != nil {
		t.Fatalf("verify finality commitments: %v", err)
	}
}

func TestFinalityBlockHashIgnoresEquivalentQuorumSubset(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	build := func(signers []string) Block {
		block := verificationTestFinalBlock(t, node)
		block.ConsensusMode = "NORMAL"
		block.QuorumPolicyVersion = quorumPolicyVersionV1
		block.ActiveReadyCount = len(validators)
		block.RequiredQuorum = strictExecSupermajority(len(validators))
		block.StrictQuorum = strictExecSupermajority(len(validators))
		block.Signatures = canonicalValidatorIDs(signers)
		block.ValidatorSetHash = ValidatorSetHash(validators)
		block.ValidatorSetRoot = HashStrings(append([]string{"validator-set-root"}, validators...))
		block.NextValidatorSetHash = block.ValidatorSetHash
		node.attachFinalityCommitments(&block)
		block.BlockHash = HashBlock(block)
		node.attachFinalityCertificate(&block)
		return block
	}

	left := build([]string{"A", "C", "D"})
	right := build([]string{"B", "C", "D"})
	if left.BlockHash != right.BlockHash {
		t.Fatalf("equivalent quorum subsets changed finalized block hash: left=%s right=%s", left.BlockHash, right.BlockHash)
	}
	if err := node.verifyFinalityCommitments(left, validators); err != nil {
		t.Fatalf("left finality commitments: %v", err)
	}
	if err := node.verifyFinalityCommitments(right, validators); err != nil {
		t.Fatalf("right finality commitments: %v", err)
	}
}

func TestVerifyBlockRejectsFinalityCertificateValidatorSetMismatch(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	block.FinalityCertificate.FinalizedValidatorSetHash = "bad-validator-set"

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "finality_certificate_validator_set_mismatch" {
		t.Fatalf("expected certificate validator-set mismatch, got %v", err)
	}
}

func TestVerifyBlockRejectsFinalityCertificateValidatorSetRootMismatch(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	block.FinalityCertificate.FinalizedValidatorSetRoot = "bad-validator-set-root"

	err := node.VerifyBlock(block, node.Blockchain)
	if err == nil || err.Error() != "finality_certificate_validator_set_root_mismatch" {
		t.Fatalf("expected certificate validator-set root mismatch, got %v", err)
	}
}

func TestFinalityCheckpointPersistenceRejectsConflictingAnchor(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	if err := node.persistFinalityCheckpoint(block); err != nil {
		t.Fatalf("persist finality checkpoint: %v", err)
	}
	node.Blockchain.ReplaceChain([]Block{block})

	conflict := block
	conflict.StateRoot = "different-finalized-state-root"
	conflict.FinalizedStateRoot = conflict.StateRoot
	conflict.FinalityRoot = computeFinalityRoot(conflict, finalitySignersForBlock(conflict))
	conflict.EpochAnchorHash = computeEpochAnchorHash(conflict, conflict.PreviousEpochAnchorHash, finalitySignersForBlock(conflict))
	conflict.BlockHash = HashBlock(conflict)
	node.attachFinalityCertificate(&conflict)

	err := node.persistFinalityCheckpoint(conflict)
	if err == nil || !strings.Contains(err.Error(), "irreversible") {
		t.Fatalf("expected irreversible checkpoint rejection, got %v", err)
	}
}

func TestVerifyFinalityCommitmentsRejectPersistedIrreversibleRootMismatch(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	if err := node.persistFinalityCheckpoint(block); err != nil {
		t.Fatalf("persist finality checkpoint: %v", err)
	}
	node.Blockchain.ReplaceChain([]Block{block})

	conflict := block
	conflict.ValidatorSetRoot = "different-validator-set-root"
	conflict.FinalizedValidatorSetRoot = conflict.ValidatorSetRoot
	conflict.FinalityRoot = computeFinalityRoot(conflict, finalitySignersForBlock(conflict))
	conflict.EpochAnchorHash = computeEpochAnchorHash(conflict, conflict.PreviousEpochAnchorHash, finalitySignersForBlock(conflict))
	conflict.BlockHash = HashBlock(conflict)
	node.attachFinalityCertificate(&conflict)

	err := node.verifyFinalityCommitments(conflict, validators)
	if err == nil || !strings.Contains(err.Error(), "irreversible_finality_checkpoint_conflict") {
		t.Fatalf("expected persisted irreversible checkpoint rejection, got %v", err)
	}
}

func TestFinalityCheckpointRecordPersistsIrreversibleRoots(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	if err := node.persistFinalityCheckpoint(block); err != nil {
		t.Fatalf("persist finality checkpoint: %v", err)
	}
	record, ok, err := node.loadPersistedFinalityCheckpoint(block.ID)
	if err != nil || !ok {
		t.Fatalf("load finality checkpoint ok=%t err=%v", ok, err)
	}
	if record.BlockHash != block.BlockHash {
		t.Fatalf("checkpoint block hash = %q, want %q", record.BlockHash, block.BlockHash)
	}
	if record.StateRoot != block.FinalizedStateRoot {
		t.Fatalf("checkpoint state root = %q, want %q", record.StateRoot, block.FinalizedStateRoot)
	}
	if record.FinalizedValidatorSetHash != block.FinalizedValidatorSetHash {
		t.Fatalf("checkpoint validator set hash = %q, want %q", record.FinalizedValidatorSetHash, block.FinalizedValidatorSetHash)
	}
	if record.FinalizedValidatorSetRoot != block.FinalizedValidatorSetRoot {
		t.Fatalf("checkpoint validator set root = %q, want %q", record.FinalizedValidatorSetRoot, block.FinalizedValidatorSetRoot)
	}
	if record.EpochAnchorHash != block.EpochAnchorHash {
		t.Fatalf("checkpoint anchor = %q, want %q", record.EpochAnchorHash, block.EpochAnchorHash)
	}
}

func TestFinalityArtifactsPersistSeparateMainnetAnchors(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	if err := node.persistFinalityCheckpoint(block); err != nil {
		t.Fatalf("persist finality checkpoint: %v", err)
	}

	var cert FinalizedEpochCertificate
	if ok, err := loadFinalityArtifactJSON(finalityArtifactFilePath(node.DataDir, node.ID, finalityCertificatesDir, block.ID), &cert); err != nil || !ok {
		t.Fatalf("load finality certificate artifact ok=%t err=%v", ok, err)
	}
	if cert.BlockHash != block.BlockHash || cert.FinalizedValidatorSetRoot != block.FinalizedValidatorSetRoot {
		t.Fatalf("unexpected certificate artifact: %+v", cert)
	}

	var anchor EpochAnchorRecord
	if ok, err := loadFinalityArtifactJSON(finalityArtifactFilePath(node.DataDir, node.ID, finalityEpochAnchorsDir, block.ID), &anchor); err != nil || !ok {
		t.Fatalf("load epoch anchor artifact ok=%t err=%v", ok, err)
	}
	if anchor.AnchorHash != block.EpochAnchorHash || anchor.FinalizedHash != block.BlockHash {
		t.Fatalf("unexpected epoch anchor artifact: %+v", anchor)
	}

	var commitment ValidatorCommitmentRecord
	if ok, err := loadFinalityArtifactJSON(finalityArtifactFilePath(node.DataDir, node.ID, finalityValidatorCommitmentsDir, block.ID), &commitment); err != nil || !ok {
		t.Fatalf("load validator commitment artifact ok=%t err=%v", ok, err)
	}
	if commitment.FinalizedValidatorSetHash != block.FinalizedValidatorSetHash || commitment.FinalizedValidatorSetRoot != block.FinalizedValidatorSetRoot {
		t.Fatalf("unexpected validator commitment artifact: %+v", commitment)
	}

	var root IrreversibleRoot
	if ok, err := loadFinalityArtifactJSON(finalityArtifactFilePath(node.DataDir, node.ID, finalityIrreversibleRootsDir, block.ID), &root); err != nil || !ok {
		t.Fatalf("load irreversible root artifact ok=%t err=%v", ok, err)
	}
	if root.FinalizedHash != block.BlockHash || root.StateRoot != block.FinalizedStateRoot || root.EpochAnchorHash != block.EpochAnchorHash {
		t.Fatalf("unexpected irreversible root artifact: %+v", root)
	}
}

func TestFullNodeSyncThrottlesFinalityArtifactFiles(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.Role = "full"
	node.Consensus = &ConsensusState{Syncing: true, Paused: true, SyncTarget: 100}

	block := finalityLayerTestBlock(t, node, validators)
	block.ID = 7
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	clearFinalityCommitments(&block)
	node.attachFinalityCommitments(&block)
	block.BlockHash = HashBlock(block)
	node.attachFinalityCertificate(&block)

	if finalityArtifactCheckpointBoundary(block.ID) {
		t.Fatalf("test block unexpectedly lands on finality artifact boundary: %d", block.ID)
	}
	if err := node.persistFinalityCheckpoint(block); err != nil {
		t.Fatalf("persist finality checkpoint: %v", err)
	}
	if _, ok, err := node.loadPersistedFinalityCheckpoint(block.ID); err != nil || !ok {
		t.Fatalf("db finality checkpoint missing ok=%t err=%v", ok, err)
	}
	if count := node.finalityArtifactFileCount(block.ID); count != 0 {
		t.Fatalf("syncing full node wrote %d finality artifact files, want 0", count)
	}
}

func TestFinalityArtifactsRejectDiskOnlyAnchorRewrite(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	anchor := epochAnchorRecordFromBlock(block)
	anchor.AnchorHash = "rewritten-anchor"
	path := finalityArtifactFilePath(node.DataDir, node.ID, finalityEpochAnchorsDir, block.ID)
	if err := writeFinalityArtifactJSON(path, anchor); err != nil {
		t.Fatalf("write conflicting anchor artifact: %v", err)
	}

	err := node.verifyFinalityArtifacts(block)
	if err == nil || !strings.Contains(err.Error(), "irreversible_finality_artifact_conflict") {
		t.Fatalf("expected disk artifact conflict, got %v", err)
	}
}

func TestFinalityCertificateTypedSignatures(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	block.ExecutionResults = []ExecutionResult{
		{Signer: "A", Signature: "sig-a"},
		{Signer: "B", Signature: "sig-b"},
		{Signer: "C", Signature: "sig-c"},
	}
	node.attachFinalityCertificate(&block)
	if got, want := len(block.FinalityCertificate.Signatures), block.RequiredQuorum; got != want {
		t.Fatalf("typed signature count = %d, want %d", got, want)
	}
	if err := verifyFinalityCertificate(block, finalitySignersForBlock(block), block.RequiredQuorum); err != nil {
		t.Fatalf("verify typed finality certificate: %v", err)
	}
}
