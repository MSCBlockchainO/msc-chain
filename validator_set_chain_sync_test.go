package main

import (
	"strings"
	"testing"
	"time"
)

func TestApplyValidatorUpdatesFromBlockUsesCommittedSignatureSet(t *testing.T) {
	set := canonicalValidatorIDs([]string{"D", "B", "A", "C"})
	setHash := ValidatorSetHash(set)

	n := &Node{}
	n.applyValidatorUpdatesFromBlock(Block{
		ID:                     25,
		Signatures:             append([]string{}, set...),
		ValidatorSetHash:       setHash,
		NextValidatorSetHash:   setHash,
		NextValidatorSetHeight: 26,
	})

	got := n.currentValidatorSetSnapshot()
	if !sameStringSlice(got, set) {
		t.Fatalf("unexpected current validator set: got=%v want=%v", got, set)
	}
	if n.validatorSetHeight != 25 {
		t.Fatalf("unexpected validatorSetHeight: got=%d want=%d", n.validatorSetHeight, 25)
	}
	if frozenHash, ok := n.frozenValidatorSetHash(25); !ok || !strings.EqualFold(frozenHash, setHash) {
		t.Fatalf("unexpected frozen hash at 25: ok=%t hash=%q want=%q", ok, frozenHash, setHash)
	}
	if frozenHash, ok := n.frozenValidatorSetHash(26); !ok || !strings.EqualFold(frozenHash, setHash) {
		t.Fatalf("unexpected frozen hash at 26: ok=%t hash=%q want=%q", ok, frozenHash, setHash)
	}
}

func TestApplyValidatorUpdatesFromBlockMaterializesReconstructedNextSet(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	registry := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 100},
		"D": {ID: "D", Stake: 100},
		"G": {ID: "G", Stake: 100},
	}
	GlobalValidatorRegistry.Load(registry)

	current := canonicalValidatorIDs([]string{"A", "B", "C", "D", "G"})
	next := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	currentHash := validatorSetHashFromSnapshotForHeight(25, current, registry)
	nextHash := validatorSetHashFromSnapshotForHeight(26, next, registry)

	n := &Node{}
	n.applyValidatorUpdatesFromBlock(Block{
		ID:                     25,
		Signatures:             append([]string{}, current...),
		ValidatorSetHash:       currentHash,
		NextValidatorSetHash:   nextHash,
		NextValidatorSetHeight: 26,
		ActivationHeight:       26,
	})

	if got := canonicalValidatorIDs(n.frozenValidatorsForHeight(25)); !sameStringSlice(got, current) {
		t.Fatalf("unexpected current frozen set: got=%v want=%v", got, current)
	}
	if got := canonicalValidatorIDs(n.frozenValidatorsForHeight(26)); !sameStringSlice(got, next) {
		t.Fatalf("unexpected next frozen set: got=%v want=%v", got, next)
	}
	if frozenHash, ok := n.frozenValidatorSetHash(26); !ok || !strings.EqualFold(frozenHash, nextHash) {
		t.Fatalf("unexpected frozen hash at 26: ok=%t hash=%q want=%q", ok, frozenHash, nextHash)
	}
}

func TestConsensusValidatorsForHeightFallsBackToCommittedBlockSet(t *testing.T) {
	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHash(set)
	bc := &Blockchain{
		Blocks: []Block{
			{
				ID:               1,
				Signatures:       append([]string{}, set...),
				ValidatorSetHash: hash,
			},
		},
	}
	n := &Node{
		Blockchain: bc,
	}

	got := n.consensusValidatorsForHeight(1)
	if !sameStringSlice(got, set) {
		t.Fatalf("unexpected validators at height 1: got=%v want=%v", got, set)
	}
}

func TestConsensusValidatorsForHeightRecoversExecutionResultSignerSubset(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHash(set)
	block := Block{
		ID:                   1,
		Signatures:           []string{"A", "B", "C"}, // quorum subset from finalized execution block
		ValidatorSetHash:     hash,
		NextValidatorSetHash: hash,
		ExecutionResults:     []ExecutionResult{{Signer: "A"}, {Signer: "B"}, {Signer: "C"}},
	}

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			1: append([]string{}, set...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			1: hash,
		},
	}

	got, ok := n.blockValidatorSetFromSignatures(block)
	if !ok || !sameStringSlice(got, set) {
		t.Fatalf("expected block validator-set recovery from frozen authority, ok=%t got=%v want=%v", ok, got, set)
	}

	next := n.consensusValidatorsForHeight(2)
	if !sameStringSlice(next, set) {
		t.Fatalf("unexpected validators at height 2: got=%v want=%v", next, set)
	}
}

func TestConsensusValidatorsForHeightCarriesForwardParentFrozenSetWhenCommitmentMatches(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHash(set)
	nextHash := "snapshot-commitment-for-1632"
	block := Block{
		ID:                     1631,
		Signatures:             []string{"A", "B", "C"},
		ValidatorSetHash:       hash,
		NextValidatorSetHash:   nextHash,
		NextValidatorSetHeight: 1632,
	}
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{block}},
		frozenValidatorsByHeight: map[uint64][]string{
			1631: append([]string{}, set...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			1631: nextHash,
		},
	}

	got := n.consensusValidatorsForHeight(1632)
	if !sameStringSlice(got, set) {
		t.Fatalf("expected parent frozen set carry-forward at height 1632: got=%v want=%v", got, set)
	}
	resolved, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(1632)
	if !ok || !sameStringSlice(resolved, set) {
		t.Fatalf("expected resolver carry-forward, ok=%t got=%v want=%v", ok, resolved, set)
	}
	if !strings.EqualFold(resolvedHash, nextHash) {
		t.Fatalf("unexpected carry-forward hash: got=%q want=%q", resolvedHash, nextHash)
	}
	if source != "chain_parent_commitment_carry_forward" {
		t.Fatalf("unexpected carry-forward source: got=%q", source)
	}
}

func TestCommittedFrozenValidatorSetCandidateRequiresExactCommittedHash(t *testing.T) {
	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	targetHash := "chain-committed-validator-set-hash"
	n := &Node{
		frozenValidatorsByHeight: map[uint64][]string{
			10: append([]string{}, set...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			10: targetHash,
		},
	}

	got, ok := n.committedFrozenValidatorSetCandidate(targetHash, 10)
	if !ok || !sameStringSlice(got, set) {
		t.Fatalf("expected exact committed frozen set, ok=%t got=%v want=%v", ok, got, set)
	}
	if got, ok := n.committedFrozenValidatorSetCandidate("different-committed-hash", 10); ok || len(got) != 0 {
		t.Fatalf("expected committed hash mismatch to reject frozen set, ok=%t got=%v", ok, got)
	}
}

func TestResolveCommittedValidatorSetFrozenFastPathAvoidsSnapshotLoad(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	targetHash := "chain-committed-validator-set-hash"
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:                     10,
			BlockHash:              "block-10",
			NextValidatorSetHash:   targetHash,
			NextValidatorSetHeight: 11,
			ActivationHeight:       11,
		}}},
		frozenValidatorsByHeight: map[uint64][]string{
			10: append([]string{}, set...),
		},
		frozenValidatorHashByHeight: map[uint64]string{
			10: targetHash,
		},
	}

	before := n.observabilityStatsSnapshot().SnapshotLoadTotal
	got, resolvedHash, source, ok := n.resolveCommittedValidatorSetForHeight(11)
	after := n.observabilityStatsSnapshot().SnapshotLoadTotal

	if !ok || !sameStringSlice(got, set) {
		t.Fatalf("expected committed frozen fast path, ok=%t got=%v want=%v", ok, got, set)
	}
	if !strings.EqualFold(resolvedHash, targetHash) {
		t.Fatalf("unexpected resolved hash: got=%q want=%q", resolvedHash, targetHash)
	}
	if source != "chain_parent_commitment_carry_forward" {
		t.Fatalf("unexpected fast-path source: got=%q", source)
	}
	if after != before {
		t.Fatalf("fast path loaded full snapshot: before=%d after=%d", before, after)
	}
}

func TestExpectedValidatorSetHashParentCommitmentAvoidsSnapshotLoad(t *testing.T) {
	withValidatorSetCommitmentV2AtHeight(t, 1)

	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	targetHash := "chain-parent-commitment"
	n := &Node{
		DB: db,
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:                     10,
			BlockHash:              "block-10",
			NextValidatorSetHash:   targetHash,
			NextValidatorSetHeight: 11,
			ActivationHeight:       11,
		}}},
	}

	before := n.observabilityStatsSnapshot().SnapshotLoadTotal
	got, source := n.expectedValidatorSetHashWithSource(11)
	after := n.observabilityStatsSnapshot().SnapshotLoadTotal

	if got != targetHash || source != "chain_parent_commitment" {
		t.Fatalf("unexpected expected validator-set hash: got=%q source=%q", got, source)
	}
	if after != before {
		t.Fatalf("chain parent commitment path loaded full snapshot: before=%d after=%d", before, after)
	}
}

func TestConsensusValidatorsForHeightUsesHeartbeatSetDuringTipSync(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHash(set)
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:                     1712,
			Signatures:             []string{"A", "B", "C"},
			ValidatorSetHash:       hash,
			NextValidatorSetHash:   hash,
			NextValidatorSetHeight: 1713,
		}}},
		validatorStatus: map[string]*ValidatorStatus{},
		syncStage:       "direct_gossip",
	}
	now := time.Now()
	for _, id := range set {
		n.validatorStatus[id] = &ValidatorStatus{
			ID:              id,
			Active:          true,
			LastSeen:        now,
			ReportedHeight:  1713,
			FinalizedHeight: 1713,
		}
	}

	got := n.consensusValidatorsForHeight(1713)
	if !sameStringSlice(got, set) {
		t.Fatalf("expected heartbeat validator-set recovery at tip sync: got=%v want=%v", got, set)
	}
}

func TestConsensusValidatorsForHeightUsesOpaqueHeartbeatSetHashDuringTipSync(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	opaqueHash := "snapshot-backed-validator-set-hash"
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:                     1712,
			Signatures:             []string{"A", "B", "C"},
			ValidatorSetHash:       opaqueHash,
			NextValidatorSetHash:   opaqueHash,
			NextValidatorSetHeight: 1713,
		}}},
		validatorStatus: map[string]*ValidatorStatus{},
		syncStage:       "direct_gossip",
	}
	now := time.Now()
	for _, id := range set {
		n.validatorStatus[id] = &ValidatorStatus{
			ID:               id,
			Active:           true,
			LastSeen:         now,
			ReportedHeight:   1713,
			FinalizedHeight:  1713,
			ValidatorSetHash: opaqueHash,
		}
	}

	got := n.consensusValidatorsForHeight(1713)
	if !sameStringSlice(got, set) {
		t.Fatalf("expected opaque heartbeat set-hash recovery at tip sync: got=%v want=%v", got, set)
	}
}

func TestConsensusValidatorsForHeightUsesPeerAdvertisedOpaqueSetHashDuringTipSync(t *testing.T) {
	prevV2 := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 1
	defer func() { ValidatorSetCommitmentV2Height = prevV2 }()

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	opaqueHash := "snapshot-backed-validator-set-hash"
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:                     1712,
			Signatures:             []string{"A", "B", "C"},
			ValidatorSetHash:       opaqueHash,
			NextValidatorSetHash:   opaqueHash,
			NextValidatorSetHeight: 1713,
		}}},
		syncStage:       "direct_gossip",
		connectedPeers:  map[string]bool{},
		peerHelloOK:     map[string]bool{},
		peerRole:        map[string]string{},
		peerSetHash:     map[string]string{},
		peerToValidator: map[string]string{},
	}
	for _, id := range set {
		peerID := "peer-" + id
		n.connectedPeers[peerID] = true
		n.peerHelloOK[peerID] = true
		n.peerRole[peerID] = "validator"
		n.peerSetHash[peerID] = opaqueHash
		n.peerToValidator[peerID] = id
	}

	got := n.consensusValidatorsForHeight(1713)
	if !sameStringSlice(got, set) {
		t.Fatalf("expected peer-advertised opaque set-hash recovery at tip sync: got=%v want=%v", got, set)
	}
}

func TestSyncContinuityValidatorFallbackUsesDirectTipBlockDuringSync(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1712,
			BlockHash: "parent-1712",
		}}},
		syncStage: "direct_gossip",
	}
	block := Block{
		ID:         1713,
		PrevHash:   "parent-1712",
		Proposer:   "A",
		Signatures: []string{"B", "C"},
	}

	got := n.syncContinuityValidatorFallback(block)
	want := canonicalValidatorIDs([]string{"A", "B", "C"})
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected direct tip sync fallback validators: got=%v want=%v", got, want)
	}
}

func TestSyncContinuityValidatorFallbackUsesExecutionResultSignersAtTip(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1712,
			BlockHash: "parent-1712",
		}}},
		syncStage: "direct_gossip",
	}
	block := Block{
		ID:       1713,
		PrevHash: "parent-1712",
		Proposer: "A",
		ExecutionResults: []ExecutionResult{
			{Signer: "B"},
			{Signer: "C"},
		},
	}

	got := n.syncContinuityValidatorFallback(block)
	want := canonicalValidatorIDs([]string{"A", "B", "C"})
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected direct tip execution-result fallback validators: got=%v want=%v", got, want)
	}
}

func TestSyncExecutionResultQuorumFallbackAcceptsTipQuorumDuringSync(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{
			ID:        1712,
			BlockHash: "parent-1712",
		}}},
		syncStage: "direct_gossip",
	}
	block := Block{
		ID:        1713,
		PrevHash:  "parent-1712",
		BlockHash: "block-1713",
		ExecutionResults: []ExecutionResult{
			{BlockHash: "block-1713", Signer: "A"},
			{BlockHash: "block-1713", Signer: "B"},
			{BlockHash: "block-1713", Signer: "C"},
		},
	}
	if !n.syncExecutionResultQuorumFallback(block, []string{"A", "B", "C"}) {
		t.Fatalf("expected execution-result quorum fallback for direct tip sync")
	}
}

func TestBlockValidatorSetFromSignaturesSubsetWithoutFrozenDoesNotRecurse(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(oldRegistry)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive},
	})

	set := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHashFromSnapshot(set, GlobalValidatorRegistry.Snapshot())
	block := Block{
		ID:               1,
		Signatures:       []string{"A", "B", "C"},
		ValidatorSetHash: hash,
		ExecutionResults: []ExecutionResult{{Signer: "A"}, {Signer: "B"}, {Signer: "C"}},
	}
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{block},
		},
	}

	if got, ok := n.blockValidatorSetFromSignatures(block); ok {
		t.Fatalf("expected no recovery without frozen authority, got=%v", got)
	}
}
