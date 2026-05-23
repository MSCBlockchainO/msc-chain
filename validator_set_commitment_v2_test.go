package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"testing"
)

func legacyHashBlockForTest(block Block) string {
	txIDs := make([]string, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		txIDs = append(txIDs, tx.ID)
	}
	sort.Strings(txIDs)
	data := fmt.Sprintf(
		"%d|%s|%s|%d|%s|%s|%s|%s|%s|%s|%x",
		block.ID,
		block.PrevHash,
		strings.Join(txIDs, ","),
		SystemTimeUnits(block.BlockTime),
		block.Proposer,
		block.Type,
		block.StateRoot,
		block.MempoolRoot,
		block.ValidatorSetHash,
		block.Task.TaskID,
		block.ResultHash,
	)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestHashBlockPreForkCompatibility(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = ^uint64(0)
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	block := Block{
		ID:               42,
		PrevHash:         "prev",
		Proposer:         "A",
		Type:             BlockTypeWork,
		StateRoot:        "state",
		MempoolRoot:      "mempool",
		ValidatorSetHash: "vset",
		Task:             Task{TaskID: "task-1"},
		ResultHash:       []byte{0x01, 0x02},
		BlockTime:        LogicalClock{Epoch: 42, Tick: TickExec},
		Transactions: []Transaction{
			{ID: "tx-b"},
			{ID: "tx-a"},
		},
		NextValidatorSetHash:   "ignored",
		NextValidatorSetHeight: 43,
	}

	got := HashBlock(block)
	want := legacyHashBlockForTest(block)
	if got != want {
		t.Fatalf("pre-fork hash mismatch: got=%s want=%s", got, want)
	}
}

func TestHashBlockPostForkIncludesNextCommitment(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 100
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	postFork := Block{
		ID:                     100,
		PrevHash:               "prev",
		Proposer:               "A",
		Type:                   BlockTypeWork,
		StateRoot:              "state",
		MempoolRoot:            "mempool",
		ValidatorSetHash:       "vset",
		ValidatorRegistryHash:  "reg-a",
		NextValidatorSetHash:   "next-a",
		NextValidatorSetHeight: 101,
		ActivationHeight:       101,
		Task:                   Task{TaskID: "task-1"},
		ResultHash:             []byte{0x0A},
		BlockTime:              LogicalClock{Epoch: 100, Tick: TickExec},
	}
	hashA := HashBlock(postFork)
	postFork.NextValidatorSetHash = "next-b"
	hashB := HashBlock(postFork)
	if hashA == hashB {
		t.Fatalf("post-fork hash must include next commitment")
	}

	preFork := postFork
	preFork.ID = 99
	preFork.NextValidatorSetHash = "next-a"
	hashPreA := HashBlock(preFork)
	preFork.NextValidatorSetHash = "next-b"
	hashPreB := HashBlock(preFork)
	if hashPreA != hashPreB {
		t.Fatalf("pre-fork hash must ignore next commitment fields")
	}

	postFork.NextValidatorSetHash = "next-a"
	postFork.NextValidatorSetHeight = 101
	postFork.ActivationHeight = 101
	hashActA := HashBlock(postFork)
	postFork.ActivationHeight = 102
	hashActB := HashBlock(postFork)
	if hashActA == hashActB {
		t.Fatalf("post-fork hash must include activation height alias")
	}

	postFork.ActivationHeight = 101
	postFork.ValidatorRegistryHash = "reg-a"
	hashRegA := HashBlock(postFork)
	postFork.ValidatorRegistryHash = "reg-b"
	hashRegB := HashBlock(postFork)
	if hashRegA == hashRegB {
		t.Fatalf("post-fork hash must include validator registry commitment")
	}

	postFork.ValidatorRegistryHash = "reg-a"
	postFork.ValidatorSetRoot = "root-a"
	hashRootA := HashBlock(postFork)
	postFork.ValidatorSetRoot = "root-b"
	hashRootB := HashBlock(postFork)
	if hashRootA == hashRootB {
		t.Fatalf("post-fork hash must include validator_set_root commitment")
	}

	postFork.ValidatorSetRoot = "root-a"
	postFork.NextValidatorSetRoot = "next-root-a"
	hashNextRootA := HashBlock(postFork)
	postFork.NextValidatorSetRoot = "next-root-b"
	hashNextRootB := HashBlock(postFork)
	if hashNextRootA == hashNextRootB {
		t.Fatalf("post-fork hash must include next_validator_set_root commitment")
	}

	preForkWithRoot := postFork
	preForkWithRoot.ID = 99
	preForkWithRoot.ValidatorSetRoot = "root-a"
	hashPreRootA := HashBlock(preForkWithRoot)
	preForkWithRoot.ValidatorSetRoot = "root-b"
	hashPreRootB := HashBlock(preForkWithRoot)
	if hashPreRootA != hashPreRootB {
		t.Fatalf("pre-fork hash must ignore validator_set_root field")
	}

	preForkWithRoot.NextValidatorSetRoot = "next-root-a"
	hashPreNextRootA := HashBlock(preForkWithRoot)
	preForkWithRoot.NextValidatorSetRoot = "next-root-b"
	hashPreNextRootB := HashBlock(preForkWithRoot)
	if hashPreNextRootA != hashPreNextRootB {
		t.Fatalf("pre-fork hash must ignore next_validator_set_root field")
	}
}

func TestExpectedValidatorSetHashUsesParentCommitmentPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	parent := Block{
		ID:                     10,
		BlockHash:              "h10",
		ValidatorSetHash:       "legacy",
		NextValidatorSetHash:   "parent-next-hash",
		NextValidatorSetHeight: 11,
	}
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}

	got, source := n.expectedValidatorSetHashWithSource(11)
	if got != "parent-next-hash" {
		t.Fatalf("unexpected expected hash: got=%q", got)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected source: got=%q", source)
	}
}

func TestExpectedValidatorSetHashPostForkDoesNotUseRuntimeFallback(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	n := &Node{
		validatorSetHeight: 11,
	}

	got, source := n.expectedValidatorSetHashWithSource(11)
	if got != "" {
		t.Fatalf("expected empty hash without chain commitment, got=%q", got)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected source: got=%q", source)
	}
}

func TestExpectedValidatorSetHashPostForkIgnoresCurrentValidatorsCache(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	parent := Block{
		ID:                     10,
		BlockHash:              "h10",
		NextValidatorSetHash:   "parent-next-hash",
		NextValidatorSetHeight: 11,
	}
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}

	gotBefore, sourceBefore := n.expectedValidatorSetHashWithSource(11)
	n.validatorSetMu.Lock()
	n.currentValidators = []string{"X", "Y", "Z"}
	n.validatorSetMu.Unlock()
	gotAfter, sourceAfter := n.expectedValidatorSetHashWithSource(11)

	if gotBefore != "parent-next-hash" || gotAfter != "parent-next-hash" {
		t.Fatalf("expected parent commitment hash to remain authoritative, before=%q after=%q", gotBefore, gotAfter)
	}
	if sourceBefore != "chain_parent_commitment" || sourceAfter != "chain_parent_commitment" {
		t.Fatalf("unexpected source transition before=%q after=%q", sourceBefore, sourceAfter)
	}
}

func TestRuntimeStatusSnapshotExposesParentCommitmentAuthorityPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	parent := Block{
		ID:                     10,
		BlockHash:              "h10",
		NextValidatorSetHash:   "parent-next-hash",
		NextValidatorSetHeight: 11,
		ActivationHeight:       11,
	}
	n := &Node{
		Role:       "validator",
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}

	status := n.runtimeStatusSnapshot()
	if status.ExpectedVsetHash != "parent-next-hash" {
		t.Fatalf("unexpected expected current validator hash: got=%q", status.ExpectedVsetHash)
	}
	if status.ExpectedVsetSource != "chain_parent_commitment" {
		t.Fatalf("unexpected expected current validator source: got=%q", status.ExpectedVsetSource)
	}
	if status.ValidatorAuthoritySource != "chain_parent_commitment" {
		t.Fatalf("unexpected validator authority source: got=%q", status.ValidatorAuthoritySource)
	}
	if status.ExpectedNextVsetHash != "parent-next-hash" {
		t.Fatalf("unexpected expected next validator hash: got=%q", status.ExpectedNextVsetHash)
	}
	if status.ExpectedNextVsetSource != "carry_forward" {
		t.Fatalf("unexpected expected next validator source: got=%q", status.ExpectedNextVsetSource)
	}
}

func TestDeterministicNextValidatorSetHashWithSourceProjectsWithoutFutureRegistryPending(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"F": {ID: "F", Stake: 100},
	}
	activeValidators := []string{"A", "B", "C", "D"}
	currentHash := validatorSetHashFromSnapshotForHeight(747, activeValidators, registry)
	projectedValidators := []string{"A", "B", "C", "D", "F"}
	projectedHash := validatorSetHashFromSnapshotForHeight(748, projectedValidators, registry)

	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   746,
		BlockHash:                "h746",
		StateRoot:                "state746",
		Ledger:                   NewLedger(),
		LedgerHash:               HashLedger(NewLedger()),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry:        registry,
		ValidatorRegistryHash:    ValidatorRegistrySnapshotHash(registry),
		PendingValidators:        map[string]uint64{"F": 747},
		PendingValidatorRemovals: map[string]uint64{},
		NextValidatorSetHash:     currentHash,
		NextValidatorSetHeight:   747,
		ActivationHeight:         747,
	})

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{
			{
				ID:                     746,
				BlockHash:              "h746",
				ValidatorRegistryHash:  ValidatorRegistrySnapshotHash(registry),
				NextValidatorSetHash:   currentHash,
				NextValidatorSetHeight: 747,
				ActivationHeight:       747,
			},
		}},
	}

	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
	})

	got, source := n.deterministicNextValidatorSetHashWithSource(747, currentHash)
	if got != projectedHash {
		t.Fatalf("unexpected projected next hash: got=%q want=%q", got, projectedHash)
	}
	if source != "chain_planned_transition" {
		t.Fatalf("unexpected projected source: got=%q want=chain_planned_transition", source)
	}
	if strings.Contains(buf.String(), "[REGISTRY-PENDING]") {
		t.Fatalf("expected no future registry pending log, got: %s", buf.String())
	}
}

func TestPlannedValidatorSetForHeightStrictIgnoresRuntimePendingMaps(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   10,
		BlockHash:                "h10",
		StateRoot:                "state10",
		Ledger:                   NewLedger(),
		LedgerHash:               HashLedger(NewLedger()),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry:        map[string]ValidatorRecord{"A": {ID: "A", Stake: 100}, "B": {ID: "B", Stake: 100}, "C": {ID: "C", Stake: 100}, "D": {ID: "D", Stake: 100}, "F": {ID: "F", Stake: 100}},
		PendingValidators:        map[string]uint64{},
		PendingValidatorRemovals: map[string]uint64{},
	})

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 10, BlockHash: "h10"}},
		},
		pendingValidators: map[string]uint64{
			"F": 1,
		},
		pendingValidatorRemovals: map[string]uint64{
			"B": 1,
		},
	}

	got := n.plannedValidatorSetForHeight(11)
	want := []string{"A", "B", "C", "D"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("strict planned validator set must ignore runtime pending maps: got=%v want=%v", got, want)
	}
}

func TestPlannedValidatorSetForHeightStrictUsesCommittedPendingTransitionsAtChildHeight(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger := NewLedger()
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   10,
		BlockHash:                "h10",
		StateRoot:                "state10",
		Ledger:                   ledger,
		LedgerHash:               HashLedger(ledger),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry:        map[string]ValidatorRecord{"A": {ID: "A", Stake: 100}, "B": {ID: "B", Stake: 100}, "C": {ID: "C", Stake: 100}, "D": {ID: "D", Stake: 100}, "F": {ID: "F", Stake: 100}},
		PendingValidators:        map[string]uint64{"F": 10},
		PendingValidatorRemovals: map[string]uint64{"B": 10},
	})

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 10, BlockHash: "h10"}},
		},
	}

	got := n.plannedValidatorSetForHeightFromChain(11)
	want := []string{"A", "C", "D", "F"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("strict committed pending transitions must apply exactly at child height: got=%v want=%v", got, want)
	}
}

func TestValidateBlockNextValidatorSetCommitmentProjectsOneBlockBehindCommittedSnapshot(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger := NewLedger()
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"G": {ID: "G", Stake: 100},
	}
	currentValidators := []string{"A", "B", "C", "D"}
	nextValidators := []string{"A", "B", "C", "D", "G"}
	currentHash := validatorSetHashFromSnapshotForHeight(669, currentValidators, registry)
	currentRoot := ValidatorSetMerkleRoot(669, currentValidators, registry)
	nextHash := validatorSetHashFromSnapshotForHeight(670, nextValidators, registry)
	nextRoot := ValidatorSetMerkleRoot(670, nextValidators, registry)

	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:           SnapshotVersion,
		Height:            668,
		BlockHash:         "h668",
		StateRoot:         "state668",
		Ledger:            ledger,
		LedgerHash:        HashLedger(ledger),
		Validators:        map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry: registry,
		PendingValidators: map[string]uint64{"G": 669},
		ValidatorSetHash:  currentHash,
		ValidatorSetRoot:  currentRoot,
	})

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                   668,
				BlockHash:            "h668",
				ValidatorSetHash:     currentHash,
				ValidatorSetRoot:     currentRoot,
				NextValidatorSetHash: currentHash,
				NextValidatorSetRoot: currentRoot,
				ActivationHeight:     669,
			}},
		},
	}

	if got := n.plannedValidatorSetForHeightFromChain(670); strings.Join(got, ",") != strings.Join(nextValidators, ",") {
		t.Fatalf("projected validator set mismatch: got=%v want=%v", got, nextValidators)
	}
	if got := ValidatorRegistrySnapshotHash(n.validatorRegistrySnapshotForHeight(670)); !strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(ValidatorRegistrySnapshotHash(registry))) {
		t.Fatalf("projected registry hash mismatch: got=%q want=%q", got, ValidatorRegistrySnapshotHash(registry))
	}

	block := Block{
		ID:                     669,
		BlockHash:              "h669",
		PrevHash:               "h668",
		ValidatorSetHash:       currentHash,
		ValidatorSetRoot:       currentRoot,
		NextValidatorSetHash:   nextHash,
		NextValidatorSetRoot:   nextRoot,
		NextValidatorSetHeight: 670,
		ActivationHeight:       670,
	}

	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		t.Fatalf("validate next validator set commitment: %v", err)
	}
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		t.Fatalf("validate next validator set root commitment: %v", err)
	}
}

func TestValidateBlockNextValidatorSetCommitmentAcceptsAuthoritativeCandidateHash(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	prevV3 := ValidatorSetHashV3Height
	ValidatorSetHashV3Height = ^uint64(0)
	defer func() { ValidatorSetHashV3Height = prevV3 }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger := NewLedger()
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"F": {ID: "F", Stake: 200},
	}
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   10,
		BlockHash:                "h10",
		StateRoot:                "state10",
		Ledger:                   ledger,
		LedgerHash:               HashLedger(ledger),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry:        registry,
		PendingValidators:        map[string]uint64{"F": 11},
		PendingValidatorRemovals: map[string]uint64{"B": 11},
	})
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 10, registry)

	registryHash := ValidatorRegistrySnapshotHash(registry)
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                    10,
				BlockHash:             "h10",
				ValidatorRegistryHash: registryHash,
			}},
		},
	}

	planned := []string{"A", "C", "D", "F"}
	snapshotHash := validatorSetHashFromSnapshotForHeight(12, planned, registry)
	plainHash := ValidatorSetHash(planned)
	if snapshotHash == "" || plainHash == "" {
		t.Fatalf("expected non-empty planned validator hashes")
	}
	if snapshotHash == plainHash {
		t.Fatalf("test requires distinct preferred and candidate hashes")
	}

	block := Block{
		ID:                     11,
		ValidatorSetHash:       ValidatorSetHash([]string{"A", "B", "C", "D"}),
		NextValidatorSetHash:   plainHash,
		NextValidatorSetHeight: 12,
		ActivationHeight:       12,
	}
	if got, _, source := n.expectedNextValidatorSetCommitmentForBlock(block); got != snapshotHash || source != "block_tx_plan" {
		t.Fatalf("unexpected preferred next commitment: got=(%q,%q) want=(%q,%q)", got, source, snapshotHash, "block_tx_plan")
	}
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		t.Fatalf("expected authoritative candidate next hash acceptance, got err=%v", err)
	}
}

func TestValidateBlockNextValidatorSetCommitmentAcceptsReconstructedRegistrySubset(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	prevV3 := ValidatorSetHashV3Height
	ValidatorSetHashV3Height = ^uint64(0)
	defer func() { ValidatorSetHashV3Height = prevV3 }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	ledger := NewLedger()
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"G": {ID: "G", Stake: 100},
	}
	registryHash := ValidatorRegistrySnapshotHash(registry)
	currentValidators := []string{"A", "B", "C", "D", "G"}
	nextValidators := []string{"A", "B", "C", "D"}
	currentHash := validatorSetHashFromSnapshotForHeight(11, currentValidators, registry)
	currentRoot := ValidatorSetMerkleRoot(11, currentValidators, registry)
	nextHash := validatorSetHashFromSnapshotForHeight(12, nextValidators, registry)
	nextRoot := ValidatorSetMerkleRoot(12, nextValidators, registry)
	storeSnapshotForHeight(t, db, StateSnapshot{
		Version:                SnapshotVersion,
		Height:                 10,
		BlockHash:              "h10",
		StateRoot:              "state10",
		Ledger:                 ledger,
		LedgerHash:             HashLedger(ledger),
		Validators:             map[string]bool{"A": true, "B": true, "C": true, "D": true, "G": true},
		ValidatorRegistry:      registry,
		ValidatorRegistryHash:  registryHash,
		ValidatorSetHash:       currentHash,
		ValidatorSetRoot:       currentRoot,
		NextValidatorSetHash:   currentHash,
		NextValidatorSetRoot:   currentRoot,
		NextValidatorSetHeight: 11,
		ActivationHeight:       11,
	})
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 10, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{
			Blocks: []Block{{
				ID:                    10,
				BlockHash:             "h10",
				StateRoot:             "state10",
				ValidatorSetHash:      currentHash,
				ValidatorSetRoot:      currentRoot,
				ValidatorRegistryHash: registryHash,
				NextValidatorSetHash:  currentHash,
				NextValidatorSetRoot:  currentRoot,
				ActivationHeight:      11,
			}},
		},
	}

	block := Block{
		ID:                     11,
		BlockHash:              "h11",
		PrevHash:               "h10",
		ValidatorSetHash:       currentHash,
		ValidatorSetRoot:       currentRoot,
		ValidatorRegistryHash:  registryHash,
		NextValidatorSetHash:   nextHash,
		NextValidatorSetRoot:   nextRoot,
		NextValidatorSetHeight: 12,
		ActivationHeight:       12,
	}

	if got, _, source := n.expectedNextValidatorSetCommitmentForBlock(block); got == nextHash {
		t.Fatalf("test requires stale local next-set expectation, got source=%q hash=%q", source, got)
	}
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		t.Fatalf("expected reconstructed next hash acceptance, got err=%v", err)
	}
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		t.Fatalf("expected reconstructed next root acceptance, got err=%v", err)
	}
}

func TestValidateBlockNextValidatorSetCommitmentPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 50
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	n := &Node{
		frozenValidatorHashByHeight: map[uint64]string{
			51: "expected-next-hash",
		},
		frozenValidatorsByHeight: map[uint64][]string{
			51: {"A", "B", "C"},
		},
	}

	blockMissing := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "",
		NextValidatorSetHeight: 51,
	}
	if err := n.validateBlockNextValidatorSetCommitment(blockMissing); err == nil {
		t.Fatalf("expected missing next hash error")
	}

	blockBadHeight := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "active-hash",
		NextValidatorSetHeight: 99,
	}
	if err := n.validateBlockNextValidatorSetCommitment(blockBadHeight); err == nil {
		t.Fatalf("expected invalid next height error")
	}

	blockAliasMismatch := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "active-hash",
		NextValidatorSetHeight: 51,
		ActivationHeight:       52,
	}
	if err := n.validateBlockNextValidatorSetCommitment(blockAliasMismatch); err == nil {
		t.Fatalf("expected activation alias mismatch error")
	}

	blockMismatch := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "different-hash",
		NextValidatorSetHeight: 51,
	}
	if _, _, source := n.expectedNextValidatorSetCommitmentForBlock(blockMismatch); source != "carry_forward" {
		t.Fatalf("unexpected source: got=%q want=carry_forward", source)
	}
	if err := n.validateBlockNextValidatorSetCommitment(blockMismatch); err == nil {
		t.Fatalf("expected carry-forward next hash mismatch rejection")
	}

	parent := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "chain-next-hash",
		NextValidatorSetHeight: 51,
	}
	nStrong := &Node{
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}
	blockStrongMismatch := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "different-hash",
		NextValidatorSetHeight: 51,
	}
	if err := nStrong.validateBlockNextValidatorSetCommitment(blockStrongMismatch); err == nil {
		t.Fatalf("expected next hash mismatch error for chain-anchored source")
	}
}

func TestValidateBlockNextValidatorSetCommitmentEnforcesParentChildHash(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 50
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	parent := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		NextValidatorSetHash:   "child-commit-hash",
		NextValidatorSetHeight: 51,
		ActivationHeight:       51,
	}
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}

	blockMismatch := Block{
		ID:                     51,
		ValidatorSetHash:       "wrong-child-hash",
		NextValidatorSetHash:   "wrong-child-hash",
		NextValidatorSetHeight: 52,
		ActivationHeight:       52,
	}
	if err := n.validateBlockNextValidatorSetCommitment(blockMismatch); err == nil {
		t.Fatalf("expected strict parent->child validator_set_hash mismatch rejection")
	}

	blockMatch := Block{
		ID:                     51,
		ValidatorSetHash:       "child-commit-hash",
		NextValidatorSetHash:   "child-commit-hash",
		NextValidatorSetHeight: 52,
		ActivationHeight:       52,
	}
	if err := n.validateBlockNextValidatorSetCommitment(blockMatch); err != nil {
		t.Fatalf("expected parent->child validator_set_hash commitment acceptance, got err=%v", err)
	}
}

func TestValidateBlockValidatorRegistryCommitmentPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 50
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {
			ID:     "A",
			Stake:  100,
			Status: ValidatorActive,
		},
	})

	blockMissing := Block{
		ID:                    50,
		ValidatorSetHash:      "active-hash",
		ValidatorRegistryHash: "",
	}
	nWeak := &Node{}
	if err := nWeak.validateBlockValidatorRegistryCommitment(blockMissing); err != nil {
		t.Fatalf("expected legacy missing registry hash acceptance, got err=%v", err)
	}

	blockWeakMismatch := Block{
		ID:                    50,
		ValidatorSetHash:      "active-hash",
		ValidatorRegistryHash: "different-hash",
	}
	if err := nWeak.validateBlockValidatorRegistryCommitment(blockWeakMismatch); err != nil {
		t.Fatalf("expected weak-source mismatch acceptance, got err=%v", err)
	}

	parent := Block{
		ID:                    49,
		ValidatorRegistryHash: "chain-registry-hash",
	}
	nRequired := &Node{
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}
	if err := nRequired.validateBlockValidatorRegistryCommitment(blockMissing); err == nil {
		t.Fatalf("expected missing registry hash error when parent commitment is anchored")
	}
	nStrong := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:                    50,
			ValidatorRegistryHash: "chain-registry-hash",
		}}},
	}
	blockStrongMismatch := Block{
		ID:                    50,
		ValidatorRegistryHash: "different-hash",
	}
	if err := nStrong.validateBlockValidatorRegistryCommitment(blockStrongMismatch); err == nil {
		t.Fatalf("expected registry hash mismatch error for chain-anchored source")
	}
}

func TestValidateBlockValidatorSetRootCommitmentPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 50
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	nRequired := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:               49,
			ValidatorSetRoot: "parent-root",
		}}},
	}
	blockMissing := Block{
		ID:               50,
		ValidatorSetHash: "active-hash",
	}
	if err := nRequired.validateBlockValidatorSetRootCommitment(blockMissing); err == nil {
		t.Fatalf("expected missing validator_set_root error when parent commitment is anchored")
	}

	nWeak := &Node{}
	blockWeak := Block{
		ID:               50,
		ValidatorSetHash: "active-hash",
		ValidatorSetRoot: "root-any",
	}
	if err := nWeak.validateBlockValidatorSetRootCommitment(blockWeak); err != nil {
		t.Fatalf("expected weak-source acceptance for validator_set_root, got err=%v", err)
	}

	nStrong := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:               50,
			ValidatorSetRoot: "chain-root",
		}}},
	}
	blockStrongMismatch := Block{
		ID:               50,
		ValidatorSetHash: "active-hash",
		ValidatorSetRoot: "other-root",
	}
	if err := nStrong.validateBlockValidatorSetRootCommitment(blockStrongMismatch); err == nil {
		t.Fatalf("expected validator_set_root mismatch error for chain-anchored source")
	}
}

func TestExpectedValidatorRegistryHashUsesParentCommitmentPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	parent := Block{
		ID:                    10,
		BlockHash:             "h10",
		ValidatorRegistryHash: "parent-registry-hash",
	}
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{parent}},
	}

	got, source := n.expectedValidatorRegistryHashWithSource(11)
	if got != "parent-registry-hash" {
		t.Fatalf("unexpected expected registry hash: got=%q", got)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected source: got=%q", source)
	}
}

func TestExpectedValidatorRegistryHashIgnoresUnanchoredParentSnapshot(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1,
			BlockHash: "block-1",
		}}},
	}

	got, source := n.expectedValidatorRegistryHashWithSource(2)
	if got != "" {
		t.Fatalf("expected empty registry hash when parent block has no anchored commitment, got=%q", got)
	}
	if source != "none" {
		t.Fatalf("unexpected source: got=%q want=none", source)
	}
}

func TestFillBlockNextValidatorSetCommitmentReanchorsRegistryHashFromCommittedSnapshot(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	wantHash := ValidatorRegistrySnapshotHash(registry)
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1,
			BlockHash: "block-1",
		}}},
	}

	block := Block{
		ID:               2,
		BlockHash:        "block-2",
		ValidatorSetHash: "validator-set-hash",
	}
	n.fillBlockNextValidatorSetCommitment(&block)

	if !strings.EqualFold(strings.TrimSpace(block.ValidatorRegistryHash), wantHash) {
		t.Fatalf("unexpected registry hash: got=%q want=%q", block.ValidatorRegistryHash, wantHash)
	}

	n.Blockchain.AddBlock(block)

	got, source := n.expectedValidatorRegistryHashWithSource(3)
	if !strings.EqualFold(strings.TrimSpace(got), wantHash) {
		t.Fatalf("unexpected parent-committed registry hash: got=%q want=%q", got, wantHash)
	}
	if source != "chain_parent_commitment" {
		t.Fatalf("unexpected source: got=%q want=chain_parent_commitment", source)
	}
}

func TestBootstrapRegistryCommitmentIgnoresExtraRuntimeValidatorsDuringFreshSync(t *testing.T) {
	defer withValidatorUpdateTestGlobals(t)()

	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}

	pubkeyHex := func(seed byte) string {
		pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
		for i := range pub {
			pub[i] = seed
		}
		return strings.ToLower(hex.EncodeToString(pub))
	}
	pubkeyBytes := func(seed byte) ed25519.PublicKey {
		decoded, err := hex.DecodeString(pubkeyHex(seed))
		if err != nil {
			t.Fatalf("decode test pubkey: %v", err)
		}
		return ed25519.PublicKey(decoded)
	}

	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": pubkeyBytes(0x11),
		"B": pubkeyBytes(0x22),
		"C": pubkeyBytes(0x33),
		"D": pubkeyBytes(0x44),
	}
	validatorPubKeysMu.Unlock()

	runtime := map[string]ValidatorRecord{
		"A": {ID: "A", ConsensusPubKey: pubkeyHex(0x11), GovernanceSigner: true, Stake: ValidatorMinStake, Status: ValidatorActive},
		"B": {ID: "B", ConsensusPubKey: pubkeyHex(0x22), GovernanceSigner: true, Stake: ValidatorMinStake, Status: ValidatorActive},
		"C": {ID: "C", ConsensusPubKey: pubkeyHex(0x33), GovernanceSigner: true, Stake: ValidatorMinStake, Status: ValidatorActive},
		"D": {ID: "D", ConsensusPubKey: pubkeyHex(0x44), GovernanceSigner: true, Stake: ValidatorMinStake, Status: ValidatorActive},
		"F": {ID: "F", ConsensusPubKey: pubkeyHex(0x55), GovernanceSigner: false, Stake: ValidatorMinStake, Status: ValidatorActive},
	}
	GlobalValidatorRegistry.Load(runtime)

	n := &Node{
		Blockchain:        &Blockchain{},
		GenesisValidators: []string{"A", "B", "C", "D"},
	}

	runtimeHash := ValidatorRegistrySnapshotHash(runtime)
	boot, wantHash, ok := n.genesisCommittedValidatorRegistryCandidate(runtime)
	if !ok || len(boot) != 4 {
		t.Fatalf("expected canonical genesis registry candidate, ok=%t len=%d", ok, len(boot))
	}
	if strings.EqualFold(runtimeHash, wantHash) {
		t.Fatalf("expected extra runtime validator to change runtime hash, runtime=%q boot=%q", runtimeHash, wantHash)
	}

	gotRegistry, gotHash, source := n.deterministicValidatorRegistryCommitmentForHeight(1)
	if len(gotRegistry) != 4 {
		t.Fatalf("expected bootstrap registry projection to ignore extra runtime validators, got=%d", len(gotRegistry))
	}
	if !strings.EqualFold(strings.TrimSpace(gotHash), wantHash) {
		t.Fatalf("unexpected derived bootstrap registry hash: got=%q want=%q source=%s", gotHash, wantHash, source)
	}

	block := Block{
		ID:               1,
		ValidatorSetHash: ValidatorSetHash([]string{"A", "B", "C", "D"}),
	}
	n.fillBlockNextValidatorSetCommitment(&block)
	if !strings.EqualFold(strings.TrimSpace(block.ValidatorRegistryHash), wantHash) {
		t.Fatalf("unexpected block registry hash: got=%q want=%q", block.ValidatorRegistryHash, wantHash)
	}
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		t.Fatalf("expected fresh-sync height 1 registry commitment to validate, got err=%v", err)
	}
}

func TestValidateBlockValidatorRegistryCommitmentRejectsEmptyHashWhenDeterministicSnapshotExists(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1,
			BlockHash: "block-1",
		}}},
	}

	err := n.validateBlockValidatorRegistryCommitment(Block{ID: 2})
	if err == nil || err.Error() != "missing_validator_registry_hash" {
		t.Fatalf("expected missing_validator_registry_hash, got=%v", err)
	}
}

func TestValidateBlockValidatorRegistryCommitmentAcceptsDerivedHashWhenParentUnanchored(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1,
			BlockHash: "block-1",
		}}},
	}

	block := Block{
		ID:                    2,
		ValidatorRegistryHash: ValidatorRegistrySnapshotHash(registry),
	}
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		t.Fatalf("expected derived anchored registry hash to validate, got err=%v", err)
	}
}

func TestValidateBlockValidatorRegistryCommitmentRejectsFallbackHashWhenParentUnanchored(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	storeCanonicalValidatorRegistrySnapshotRecord(t, db, 1, registry)

	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1,
			BlockHash: "block-1",
		}}},
	}

	block := Block{
		ID:                    2,
		ValidatorRegistryHash: "network-registry-hash",
	}
	if err := n.validateBlockValidatorRegistryCommitment(block); err == nil || err.Error() != "validator_registry_hash_mismatch" {
		t.Fatalf("expected validator_registry_hash_mismatch, got=%v", err)
	}
}

func TestValidateBlockNextValidatorSetRootCommitmentPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 50
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	nRequired := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:               49,
			ValidatorSetRoot: "parent-root",
		}}},
	}
	blockMissing := Block{
		ID:                     50,
		ValidatorSetHash:       "active-hash",
		ValidatorSetRoot:       "active-root",
		NextValidatorSetHash:   "active-hash",
		NextValidatorSetHeight: 51,
		ActivationHeight:       51,
	}
	if err := nRequired.validateBlockNextValidatorSetRootCommitment(blockMissing); err == nil {
		t.Fatalf("expected missing next_validator_set_root error")
	}

	blockCarryForward := blockMissing
	blockCarryForward.NextValidatorSetRoot = "active-root"
	if err := nRequired.validateBlockNextValidatorSetRootCommitment(blockCarryForward); err != nil {
		t.Fatalf("expected carry-forward next root acceptance, got err=%v", err)
	}

	blockMismatch := blockCarryForward
	blockMismatch.NextValidatorSetRoot = "different-root"
	_, expectedRoot, source := nRequired.expectedNextValidatorSetCommitmentForBlock(blockMismatch)
	if expectedRoot != "active-root" {
		t.Fatalf("unexpected expected next root: got=%q want=active-root", expectedRoot)
	}
	if source != "carry_forward" {
		t.Fatalf("unexpected source: got=%q want=carry_forward", source)
	}
	if err := nRequired.validateBlockNextValidatorSetRootCommitment(blockMismatch); err == nil {
		t.Fatalf("expected carry-forward next root mismatch rejection")
	}
}

func TestValidateBlockValidatorSetHashHeaderCommitmentAcceptsCanonicalSortedList(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
	})
	block := Block{
		ID:               12,
		Signatures:       []string{"b", "A", "C"},
		ValidatorSetHash: ValidatorSetHashFromSnapshot([]string{"A", "B", "C"}, GlobalValidatorRegistry.Snapshot()),
	}
	if err := validateBlockValidatorSetHashHeaderCommitment(block); err != nil {
		t.Fatalf("expected valid header commitment, got err=%v", err)
	}
}

func TestValidateBlockValidatorSetHashHeaderCommitmentRejectsMismatch(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive},
	})
	block := Block{
		ID:               12,
		Signatures:       []string{"A", "B", "C"},
		ValidatorSetHash: ValidatorSetHashFromSnapshot([]string{"A", "B", "D"}, GlobalValidatorRegistry.Snapshot()),
	}
	if err := validateBlockValidatorSetHashHeaderCommitment(block); err == nil {
		t.Fatalf("expected validator_set_hash_header_mismatch error")
	}
}

func TestValidateBlockValidatorSetHashHeaderCommitmentSkipsExecutionResultBlocks(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive},
	})
	block := Block{
		ID:               12,
		Signatures:       []string{"A", "B"},
		ValidatorSetHash: ValidatorSetHashFromSnapshot([]string{"A", "B", "C", "D"}, GlobalValidatorRegistry.Snapshot()),
		ExecutionResults: []ExecutionResult{{Signer: "A"}, {Signer: "B"}},
	}
	if err := validateBlockValidatorSetHashHeaderCommitment(block); err != nil {
		t.Fatalf("expected execution-result block path to skip strict signature-list check, got err=%v", err)
	}
}

func TestValidateBlockValidatorSetHashHeaderCommitmentRequiresHashPostFork(t *testing.T) {
	prev := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 10
	defer func() { ValidatorSetCommitmentV2Height = prev }()

	block := Block{
		ID:               10,
		Signatures:       nil,
		ValidatorSetHash: "",
		ExecutionResults: []ExecutionResult{{Signer: "A"}},
	}
	if err := validateBlockValidatorSetHashHeaderCommitment(block); err == nil {
		t.Fatalf("expected missing validator_set_hash error post-fork")
	}
}
