package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

const multiArchReplayVectorVersion = "msc-multi-arch-replay-v1"

// multiArchReplayGoldenDigest is the public validator determinism canary. It is
// intentionally independent of runtime.GOOS/runtime.GOARCH; Linux, Windows,
// amd64, and arm64 must all produce this exact digest for the same replay vector.
const multiArchReplayGoldenDigest = "e2f226d9dbd39d79e982a523553ddc885cf39bf52101d000e49af32b8688ed48"

type multiArchReplayVector struct {
	Digest             string
	FinalHeight        uint64
	FinalLedgerHash    string
	FinalBlockHash     string
	FinalStateRoot     string
	SnapshotHash       string
	SnapshotManifest   string
	BlockHashes        []string
	StateRoots         []string
	EpochAnchors       []string
	ValidatorRoots     []string
	MempoolRoots       []string
	TransactionIDs     []string
	ReceiptRoots       []string
	FinalityRoots      []string
	ValidatorSetHashes []string
}

func fixedEd25519KeyForMultiArchReplay(label string) (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := sha256.Sum256([]byte("msc-multi-arch-determinism|" + label))
	priv := ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv
}

func multiArchReplayDigest(parts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func buildMultiArchReplayVector(t *testing.T) multiArchReplayVector {
	t.Helper()

	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	pubA, privA := fixedEd25519KeyForMultiArchReplay("sender-a")
	pubB, privB := fixedEd25519KeyForMultiArchReplay("sender-b")
	addrA := AddressFromPublicKey(pubA)
	addrB := AddressFromPublicKey(pubB)

	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, addrA, 1_000_000)
	addBalance(&ledger, CoinSymbol, addrB, 1_000_000)
	setValidatorRewardWallet(&ledger, "A", "reward-wallet-a")
	setValidatorRewardWallet(&ledger, "B", "reward-wallet-b")
	setValidatorRewardWallet(&ledger, "C", "reward-wallet-c")
	setValidatorRewardWallet(&ledger, "D", "reward-wallet-d")

	prevHash := GenesisHash
	setHash := ValidatorSetHash(validators)
	setRoot := HashStrings(append([]string{"multi-arch-validator-root"}, validators...))
	registryHash := ValidatorRegistrySnapshotHash(testValidatorSetMaterializationRegistry())

	vector := multiArchReplayVector{}
	material := []string{multiArchReplayVectorVersion, ChainID, GenesisHash, setHash, setRoot, registryHash}

	for height := uint64(1); height <= 8; height++ {
		from, to := addrA, addrB
		pub, priv := pubA, privA
		if height%2 == 0 {
			from, to = addrB, addrA
			pub, priv = pubB, privB
		}
		tx := Transaction{
			From:      from,
			To:        fmt.Sprintf("%s-receiver-%02d", to, height),
			Amount:    int(7 + height),
			Nonce:     int((height + 1) / 2),
			PublicKey: hex.EncodeToString(pub),
			Expiry:    4_102_444_800 + int64(height),
			Type:      TxTransfer,
			ChainID:   ChainID,
			Coin:      CoinSymbol,
		}
		tx.Fee = requiredFeeForTxWithLedger(&ledger, tx)
		tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
		tx.ID = ComputeTxID(tx)

		block := Block{
			ID:                     height,
			Height:                 height,
			PrevHash:               prevHash,
			Proposer:               validators[int((height-1)%uint64(len(validators)))],
			Type:                   BlockTypeWork,
			Transactions:           []Transaction{tx},
			BlockTime:              LogicalTimeForEpochTick(height, TickFinalize),
			MempoolRoot:            ComputeMempoolRoot([]Transaction{tx}),
			ValidatorSetHash:       setHash,
			ValidatorSetRoot:       setRoot,
			NextValidatorSetHash:   setHash,
			NextValidatorSetRoot:   setRoot,
			ValidatorRegistryHash:  registryHash,
			NextValidatorSetHeight: height + 1,
			ActivationHeight:       height + 1,
			ConsensusMode:          "NORMAL",
			QuorumPolicyVersion:    quorumPolicyVersionV1,
			ActiveReadyCount:       len(validators),
			RequiredQuorum:         strictExecSupermajority(len(validators)),
			StrictQuorum:           strictExecSupermajority(len(validators)),
			Signatures:             append([]string{}, validators[:strictExecSupermajority(len(validators))]...),
		}
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))

		preHash := HashLedger(ledger)
		nextLedger, err := ApplyBlockStateWithNode(node, ledger, block)
		if err != nil {
			t.Fatalf("apply multi-arch replay block %d: %v", height, err)
		}
		postHash := HashLedger(nextLedger)
		block.Receipts = []StateReceipt{{
			TxHash:        tx.ID,
			PreStateHash:  preHash,
			PostStateHash: postHash,
		}}
		block.ReceiptRoot = ComputeReceiptRoot(block.Receipts)
		block.StateRoot = ComputeExecHashVersioned(block, postHash, executionStateRootVersionForHeight(block.ID))
		node.attachFinalityCommitments(&block)
		block.BlockHash = HashBlock(block)
		node.attachFinalityCertificate(&block)

		replayLedger, err := ApplyBlockStateWithNode(node, ledger, block)
		if err != nil {
			t.Fatalf("replay multi-arch block %d: %v", height, err)
		}
		replayRoot := ComputeExecHashVersioned(block, HashLedger(replayLedger), executionStateRootVersionForHeight(block.ID))
		if replayRoot != block.StateRoot {
			t.Fatalf("multi-arch replay state root mismatch height=%d got=%s want=%s", height, replayRoot, block.StateRoot)
		}
		if replayHash := HashBlock(block); replayHash != block.BlockHash {
			t.Fatalf("multi-arch replay block hash mismatch height=%d got=%s want=%s", height, replayHash, block.BlockHash)
		}

		node.Blockchain.AddBlock(block)
		node.commitMu.Lock()
		node.committed[block.ID] = block.BlockHash
		node.committedHeight = block.ID
		node.finalizedHeight = block.ID
		node.lastCommitHeight = block.ID
		node.commitMu.Unlock()

		vector.BlockHashes = append(vector.BlockHashes, block.BlockHash)
		vector.StateRoots = append(vector.StateRoots, block.StateRoot)
		vector.EpochAnchors = append(vector.EpochAnchors, block.EpochAnchorHash)
		vector.ValidatorRoots = append(vector.ValidatorRoots, block.ValidatorSetRoot)
		vector.MempoolRoots = append(vector.MempoolRoots, block.MempoolRoot)
		vector.TransactionIDs = append(vector.TransactionIDs, tx.ID)
		vector.ReceiptRoots = append(vector.ReceiptRoots, block.ReceiptRoot)
		vector.FinalityRoots = append(vector.FinalityRoots, block.FinalityRoot)
		vector.ValidatorSetHashes = append(vector.ValidatorSetHashes, block.ValidatorSetHash)

		material = append(material,
			fmt.Sprintf("height=%d", block.ID),
			"tx="+tx.ID,
			"pre="+preHash,
			"post="+postHash,
			"state="+block.StateRoot,
			"block="+block.BlockHash,
			"mempool="+block.MempoolRoot,
			"receipt="+block.ReceiptRoot,
			"vset="+block.ValidatorSetHash,
			"vroot="+block.ValidatorSetRoot,
			"anchor="+block.EpochAnchorHash,
			"finality="+block.FinalityRoot,
		)

		ledger = nextLedger
		prevHash = block.BlockHash
	}

	finalHeight := uint64(len(vector.StateRoots))
	finalSnapshot := StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 finalHeight,
		BlockHash:              prevHash,
		StateRoot:              vector.StateRoots[len(vector.StateRoots)-1],
		Ledger:                 ledger.Clone(),
		LedgerHash:             HashLedger(ledger),
		StateMerkleRoot:        LedgerStateMerkleRoot(ledger),
		GenesisHash:            GenesisHash,
		Validators:             map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorSetHash:       setHash,
		ValidatorSetRoot:       setRoot,
		NextValidatorSetHash:   setHash,
		NextValidatorSetRoot:   setRoot,
		NextValidatorSetHeight: finalHeight + 1,
		ActivationHeight:       finalHeight + 1,
		ValidatorRegistry:      testValidatorSetMaterializationRegistry(),
		ValidatorRegistryHash:  registryHash,
		FinalizedHeight:        finalHeight,
		FinalizedHash:          prevHash,
		EpochAnchorHash:        vector.EpochAnchors[len(vector.EpochAnchors)-1],
		FinalityRoot:           vector.FinalityRoots[len(vector.FinalityRoots)-1],
	}
	populateSnapshotDerivedFields(&finalSnapshot)
	manifest, payload, err := snapshotManifestFromSnapshot(&finalSnapshot)
	if err != nil {
		t.Fatalf("build multi-arch snapshot manifest: %v", err)
	}
	if _, err := verifySnapshotPayloadAgainstManifest(payload, manifest, 0); err != nil {
		t.Fatalf("multi-arch snapshot manifest verify: %v", err)
	}
	manifestHash := snapshotManifestHash(manifest)

	vector.FinalHeight = finalSnapshot.Height
	vector.FinalLedgerHash = HashLedger(ledger)
	vector.FinalBlockHash = prevHash
	vector.FinalStateRoot = finalSnapshot.StateRoot
	vector.SnapshotHash = finalSnapshot.SnapshotHash
	vector.SnapshotManifest = manifestHash
	material = append(material,
		"final_height="+fmt.Sprint(vector.FinalHeight),
		"final_ledger="+vector.FinalLedgerHash,
		"final_block="+vector.FinalBlockHash,
		"final_state="+vector.FinalStateRoot,
		"snapshot="+vector.SnapshotHash,
		"manifest="+vector.SnapshotManifest,
	)
	vector.Digest = multiArchReplayDigest(material)
	return vector
}

func TestMultiArchitectureReplayVectorMatchesGolden(t *testing.T) {
	vector := buildMultiArchReplayVector(t)
	t.Logf("multi-arch replay runtime=%s/%s digest=%s final_height=%d final_block=%s final_ledger=%s manifest=%s",
		runtime.GOOS,
		runtime.GOARCH,
		vector.Digest,
		vector.FinalHeight,
		vector.FinalBlockHash,
		vector.FinalLedgerHash,
		vector.SnapshotManifest,
	)
	if multiArchReplayGoldenDigest == "" {
		t.Fatalf("multiArchReplayGoldenDigest is empty; set it to %q after intentional replay-vector changes", vector.Digest)
	}
	if vector.Digest != multiArchReplayGoldenDigest {
		t.Fatalf("multi-architecture replay digest mismatch on %s/%s: got=%s want=%s", runtime.GOOS, runtime.GOARCH, vector.Digest, multiArchReplayGoldenDigest)
	}
}

func TestMultiArchitectureReplayVectorIsStableAcrossRepeatedRuns(t *testing.T) {
	first := buildMultiArchReplayVector(t)
	second := buildMultiArchReplayVector(t)
	if first.Digest != second.Digest ||
		first.FinalLedgerHash != second.FinalLedgerHash ||
		first.FinalBlockHash != second.FinalBlockHash ||
		first.SnapshotManifest != second.SnapshotManifest {
		t.Fatalf("multi-arch replay vector is not stable inside one runtime: first=%+v second=%+v", first, second)
	}
}
