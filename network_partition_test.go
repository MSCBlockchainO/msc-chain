package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func withNetworkPartitionMainnetGlobals(t *testing.T) {
	t.Helper()
	oldIsTestnet := IsTestnet
	oldResultGossipOnly := ResultGossipOnly
	oldCommitmentV2 := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		IsTestnet = oldIsTestnet
		ResultGossipOnly = oldResultGossipOnly
		ValidatorSetCommitmentV2Height = oldCommitmentV2
	})
	IsTestnet = false
	ResultGossipOnly = true
	ValidatorSetCommitmentV2Height = 1_000_000
}

func installNetworkPartitionValidatorKeys(t *testing.T, validators []string) map[string]ValidatorKey {
	t.Helper()

	validatorPubKeysMu.RLock()
	oldRuntime := make(map[string]ed25519.PublicKey, len(ValidatorPubKeys))
	for id, pub := range ValidatorPubKeys {
		oldRuntime[id] = append(ed25519.PublicKey(nil), pub...)
	}
	oldGenesis := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pub := range GenesisValidatorPubKeys {
		oldGenesis[id] = append(ed25519.PublicKey(nil), pub...)
	}
	validatorPubKeysMu.RUnlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldRuntime))
		for id, pub := range oldRuntime {
			ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesis))
		for id, pub := range oldGenesis {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		}
		validatorPubKeysMu.Unlock()
	})

	keys := make(map[string]ValidatorKey, len(validators))
	validatorPubKeysMu.Lock()
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	for i, id := range validators {
		key := strictActivationTestValidatorKey(byte(120+i), id)
		keys[id] = key
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
	validatorPubKeysMu.Unlock()
	return keys
}

func registerNetworkPartitionValidatorKeys(keys map[string]ValidatorKey) {
	validatorPubKeysMu.Lock()
	defer validatorPubKeysMu.Unlock()
	for id, key := range keys {
		id = normalizeValidatorID(id)
		if id == "" {
			continue
		}
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), key.PublicKey...)
	}
}

func newNetworkPartitionNode(t *testing.T, name string, validators []string, keys map[string]ValidatorKey, parent Block) *Node {
	t.Helper()
	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), name), validators)
	node.ID = normalizeValidatorID(name)
	node.Role = "validator"
	node.GenesisValidators = append([]string{}, validators...)
	if key, ok := keys[node.ID]; ok {
		node.ValidatorKey = key
	} else {
		node.ValidatorKey = keys[validators[0]]
	}
	node.Consensus = NewConsensusState(parent.ID + 1)
	node.Blockchain.AddBlock(parent)
	seedCrashRecoveryFrozenSet(node, parent.ID+1, validators)
	return node
}

func networkPartitionParent(validators []string) Block {
	parent := Block{
		ID:               1,
		Height:           1,
		PrevHash:         GenesisHash,
		Proposer:         validators[0],
		Type:             BlockTypeTime,
		BlockTime:        LogicalTimeForEpochTick(1, TickFinalize),
		ValidatorSetHash: ValidatorSetHash(validators),
		StateRoot:        strings.Repeat("1", 64),
	}
	parent.Timestamp = int64(SystemTimeUnits(parent.BlockTime))
	parent.BlockHash = HashBlock(parent)
	return parent
}

func networkPartitionRoundForProposer(t *testing.T, node *Node, height uint64, proposer string, validators []string) uint32 {
	t.Helper()
	for round := uint32(0); round < 128; round++ {
		if normalizeValidatorID(node.consensusLeaderForHeightRound(height, round, validators)) == normalizeValidatorID(proposer) {
			return round
		}
	}
	t.Fatalf("no round found for proposer %s at height %d", proposer, height)
	return 0
}

func buildNetworkPartitionBlock(
	t *testing.T,
	node *Node,
	parent Block,
	validators []string,
	keys map[string]ValidatorKey,
	proposer string,
	mode string,
	activeReady int,
	required int,
	strict int,
	signers []string,
	stateSalt string,
) Block {
	t.Helper()
	height := parent.ID + 1
	round := networkPartitionRoundForProposer(t, node, height, proposer, validators)
	block := Block{
		ID:                  height,
		Height:              height,
		Round:               round,
		PrevHash:            parent.BlockHash,
		Proposer:            normalizeValidatorID(proposer),
		Type:                BlockTypeTime,
		BlockTime:           LogicalTimeForEpochTick(height, TickFinalize),
		ValidatorSetHash:    ValidatorSetHash(validators),
		ConsensusMode:       mode,
		QuorumPolicyVersion: quorumPolicyVersionV1,
		ActiveReadyCount:    activeReady,
		RequiredQuorum:      required,
		StrictQuorum:        strict,
		Signatures:          canonicalValidatorIDs(signers),
	}
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.MempoolRoot = ""
	block.StateRoot = node.ExecuteBlockAndGetStateRoot(block)
	if block.StateRoot == "" {
		t.Fatal("state root unavailable for partition test block")
	}
	if stateSalt != "" {
		block.StateRoot = HashStrings([]string{block.StateRoot, stateSalt})
	}
	proposerKey, ok := keys[normalizeValidatorID(proposer)]
	if !ok {
		t.Fatalf("missing proposer key for %s", proposer)
	}
	block.BlockHash = HashBlock(block)

	for _, signer := range canonicalValidatorIDs(signers) {
		key, ok := keys[signer]
		if !ok {
			t.Fatalf("missing signer key for %s", signer)
		}
		sig := ed25519.Sign(key.PrivateKey, execResultSignBytesV2(block.ID, block.Round, block.BlockHash, block.StateRoot, block.MempoolRoot))
		block.ExecutionResults = append(block.ExecutionResults, ExecutionResult{
			Height:     block.ID,
			BlockHash:  block.BlockHash,
			Signer:     signer,
			ResultHash: block.StateRoot,
			TxMerkle:   block.MempoolRoot,
			Signature:  hex.EncodeToString(sig),
		})
	}
	registerNetworkPartitionValidatorKeys(keys)
	block.BlockHash = HashBlock(block)
	block.Signature = ed25519.Sign(proposerKey.PrivateKey, []byte(block.BlockHash))
	if canonical := HashBlock(block); canonical != block.BlockHash {
		t.Fatalf("partition block hash drift proposer=%s height=%d got=%s want=%s", block.Proposer, block.ID, ShortHash(canonical), ShortHash(block.BlockHash))
	}
	if !ed25519.Verify(proposerKey.PublicKey, []byte(block.BlockHash), block.Signature) {
		t.Fatalf("partition block direct signature failed proposer=%s height=%d hash=%s", block.Proposer, block.ID, ShortHash(block.BlockHash))
	}
	if !VerifyBlockSignature(block) {
		t.Fatalf("test block signature failed local verification proposer=%s height=%d hash=%s", block.Proposer, block.ID, ShortHash(block.BlockHash))
	}
	return block
}

func finalizePartitionBlockForTest(t *testing.T, node *Node, block Block) {
	t.Helper()
	node.Blockchain.AddBlock(block)
	node.commitMu.Lock()
	if node.committed == nil {
		node.committed = make(map[uint64]string)
	}
	node.committed[block.ID] = block.BlockHash
	node.committedHeight = block.ID
	node.finalizedHeight = block.ID
	node.lastCommitHeight = block.ID
	node.commitMu.Unlock()
	if err := node.persistFinalizedHashInvariant(block); err != nil {
		t.Fatalf("persist finalized hash invariant: %v", err)
	}
}

func TestNetworkPartitionThreeTwoSplitNoDoubleFinalization(t *testing.T) {
	withNetworkPartitionMainnetGlobals(t)
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D", "E"})
	keys := installNetworkPartitionValidatorKeys(t, validators)
	parent := networkPartitionParent(validators)

	majority := newNetworkPartitionNode(t, "A", validators, keys, parent)
	minority := newNetworkPartitionNode(t, "D", validators, keys, parent)

	normalThreeOfFive := buildNetworkPartitionBlock(
		t,
		majority,
		parent,
		validators,
		keys,
		"A",
		"NORMAL",
		3,
		3,
		4,
		[]string{"A", "B", "C"},
		"",
	)
	if err := majority.VerifyBlock(normalThreeOfFive, majority.Blockchain); err == nil || !strings.Contains(err.Error(), "quorum_metadata_weak_normal") {
		t.Fatalf("expected NORMAL 3/5 partition quorum rejection, got %v", err)
	}

	majorityBlock := buildNetworkPartitionBlock(
		t,
		majority,
		parent,
		validators,
		keys,
		"A",
		"DEGRADED",
		4,
		4,
		4,
		[]string{"A", "B", "C", "D"},
		"",
	)
	if err := majority.VerifyBlock(majorityBlock, majority.Blockchain); err != nil {
		t.Fatalf("expected 4/5 degraded recovery block to verify: %v", err)
	}

	minorityFork := buildNetworkPartitionBlock(
		t,
		minority,
		parent,
		validators,
		keys,
		"D",
		"DEGRADED",
		2,
		2,
		4,
		[]string{"D", "E"},
		"minority-fork",
	)
	if err := minority.VerifyBlock(minorityFork, minority.Blockchain); err == nil || !strings.Contains(err.Error(), "quorum_metadata_below_strict") {
		t.Fatalf("expected 2/5 degraded partition quorum rejection, got %v", err)
	}
	if got := minority.Blockchain.Height(); got != parent.ID {
		t.Fatalf("minority partition must not finalize rejected fork: got height=%d want=%d", got, parent.ID)
	}

	finalizePartitionBlockForTest(t, majority, majorityBlock)
	registerNetworkPartitionValidatorKeys(keys)
	if err := minority.ReceiveBlock(majorityBlock, minority.Blockchain); err != nil {
		t.Fatalf("minority side should deterministically recover to majority block on heal: %v", err)
	}
	if got := minority.Blockchain.LastBlock().BlockHash; got != majorityBlock.BlockHash {
		t.Fatalf("minority recovery selected wrong tip: got=%s want=%s", got, majorityBlock.BlockHash)
	}

	registerNetworkPartitionValidatorKeys(keys)
	err := majority.ReceiveBlock(minorityFork, majority.Blockchain)
	if err == nil || !strings.Contains(err.Error(), "committed_different_hash") {
		t.Fatalf("expected healed minority fork to be rejected against finalized majority hash, got %v", err)
	}
	if got := majority.Blockchain.LastBlock().BlockHash; got != majorityBlock.BlockHash {
		t.Fatalf("majority finalized tip changed after partition heal: got=%s want=%s", got, majorityBlock.BlockHash)
	}
	if majority.getFinalizedHeight() != majorityBlock.ID || minority.getFinalizedHeight() != majorityBlock.ID {
		t.Fatalf("expected both sides finalized at height %d after heal: majority=%d minority=%d",
			majorityBlock.ID, majority.getFinalizedHeight(), minority.getFinalizedHeight())
	}
	if majority.getFinalizedHeight() == minorityFork.ID && majority.Blockchain.LastBlock().BlockHash == minorityFork.BlockHash {
		t.Fatal("minority fork was double-finalized")
	}
	if !hasConsensusEvidenceForTest(t, majority, "finalized_hash_conflict", minorityFork.ID, majorityBlock.BlockHash, minorityFork.BlockHash) {
		t.Fatal("expected finalized fork evidence after partition heal")
	}

	if minority.QueueFutureBlock(minorityFork) {
		t.Fatal("recovered node must not queue its rejected minority fork after heal")
	}
	minority.SwitchToFork(minorityFork)
	if got := minority.Blockchain.LastBlock().BlockHash; got != majorityBlock.BlockHash {
		t.Fatalf("recovered node switched to finalized fork: got=%s want=%s", got, majorityBlock.BlockHash)
	}
}
