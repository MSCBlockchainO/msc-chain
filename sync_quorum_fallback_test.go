package main

import "testing"

func TestSyncExecutionQuorumFallbackUsesCommittedBlockMetadataWhenLocalSetIsStale(t *testing.T) {
	bc := NewBlockchain()
	parent := Block{ID: 1, PrevHash: GenesisHash, BlockHash: "parent-hash", Proposer: "A"}
	bc.AddBlock(parent)
	node := &Node{Blockchain: &bc}
	node.syncMu.Lock()
	node.syncStage = "range_fetch"
	node.syncMu.Unlock()

	block := Block{
		ID:                  2,
		PrevHash:            parent.BlockHash,
		BlockHash:           "child-hash",
		Proposer:            "F",
		ConsensusMode:       "NORMAL",
		QuorumPolicyVersion: quorumPolicyVersionV1,
		ActiveReadyCount:    3,
		RequiredQuorum:      3,
		StrictQuorum:        3,
		ExecutionResults: []ExecutionResult{
			{Height: 2, BlockHash: "child-hash", Signer: "B"},
			{Height: 2, BlockHash: "child-hash", Signer: "C"},
			{Height: 2, BlockHash: "child-hash", Signer: "F"},
		},
	}

	if !node.syncExecutionResultQuorumFallback(block, []string{"A", "B", "C", "D"}) {
		t.Fatal("expected committed block metadata/signers to allow sync fallback despite stale local validator set")
	}
}

func TestSyncExecutionQuorumFallbackRejectsWeakNormalMetadata(t *testing.T) {
	bc := NewBlockchain()
	parent := Block{ID: 1, PrevHash: GenesisHash, BlockHash: "parent-hash", Proposer: "A"}
	bc.AddBlock(parent)
	node := &Node{Blockchain: &bc}
	node.syncMu.Lock()
	node.syncStage = "range_fetch"
	node.syncMu.Unlock()

	block := Block{
		ID:               2,
		PrevHash:         parent.BlockHash,
		BlockHash:        "child-hash",
		Proposer:         "F",
		ConsensusMode:    "NORMAL",
		ActiveReadyCount: 3,
		RequiredQuorum:   2,
		ExecutionResults: []ExecutionResult{
			{Height: 2, BlockHash: "child-hash", Signer: "B"},
			{Height: 2, BlockHash: "child-hash", Signer: "F"},
		},
	}

	if node.syncExecutionResultQuorumFallback(block, []string{"A", "B", "C", "D"}) {
		t.Fatal("normal mode metadata below strict quorum must not allow sync fallback")
	}
}

func TestCommittedQuorumEvidenceRejectsNormalShortfall(t *testing.T) {
	node := &Node{}
	block := Block{
		ID:               3697,
		BlockHash:        "child-hash",
		ConsensusMode:    "NORMAL",
		ActiveReadyCount: 3,
		RequiredQuorum:   3,
		StrictQuorum:     3,
		Signatures:       []string{"B", "F"},
		ExecutionResults: []ExecutionResult{
			{Height: 3697, BlockHash: "child-hash", Signer: "B"},
			{Height: 3697, BlockHash: "child-hash", Signer: "F"},
		},
	}

	if err := node.validateCommittedBlockQuorumEvidence(block); err == nil {
		t.Fatal("expected NORMAL block with 2-of-3 evidence to be rejected")
	}
}

func TestCommittedQuorumEvidenceRejectsDegradedBelowStrict(t *testing.T) {
	oldTestnet := IsTestnet
	IsTestnet = true
	t.Cleanup(func() { IsTestnet = oldTestnet })

	node := &Node{}
	block := Block{
		ID:               42,
		BlockHash:        "child-hash",
		ConsensusMode:    "DEGRADED",
		ActiveReadyCount: 2,
		RequiredQuorum:   2,
		StrictQuorum:     3,
		Signatures:       []string{"B", "F"},
		ExecutionResults: []ExecutionResult{
			{Height: 42, BlockHash: "child-hash", Signer: "B"},
			{Height: 42, BlockHash: "child-hash", Signer: "F"},
		},
	}

	if err := node.validateCommittedBlockQuorumEvidence(block); err == nil {
		t.Fatalf("expected explicit degraded quorum below strict to fail")
	}
}

func TestSanitizeContiguousLoadedBlocksTruncatesSparseTail(t *testing.T) {
	blocks := []Block{
		{ID: 1, BlockHash: "h1"},
		{ID: 2, PrevHash: "h1", BlockHash: "h2"},
		{ID: 10, PrevHash: "h9", BlockHash: "h10"},
	}

	got, tip, reason := sanitizeContiguousLoadedBlocks(blocks)
	if tip != 2 || len(got) != 2 {
		t.Fatalf("expected truncate at height 2, got tip=%d len=%d reason=%s", tip, len(got), reason)
	}
	if reason == "" {
		t.Fatal("expected sparse-chain reason")
	}
}

func TestSanitizeContiguousLoadedBlocksRejectsUnanchoredSnapshotOnlyTip(t *testing.T) {
	blocks := []Block{
		{ID: 1000, PrevHash: "h999", BlockHash: "h1000"},
	}

	got, tip, reason := sanitizeContiguousLoadedBlocks(blocks)
	if tip != 0 || len(got) != 0 {
		t.Fatalf("expected unanchored tip to be removed, got tip=%d len=%d reason=%s", tip, len(got), reason)
	}
	if reason == "" {
		t.Fatal("expected sparse-chain reason")
	}
}

func TestApplySnapshotForRecoveryRejectsHeightRegression(t *testing.T) {
	bc := NewBlockchain()
	bc.AddBlock(Block{ID: 10, BlockHash: "h10"})
	node := &Node{Blockchain: &bc}

	node.ApplySnapshotForRecovery(StateSnapshot{Height: 9, BlockHash: "h9"})

	if got := node.Blockchain.Height(); got != 10 {
		t.Fatalf("recovery snapshot must not lower chain height, got %d", got)
	}
}

func TestSanitizeContiguousLoadedBlocksTruncatesPrevHashMismatch(t *testing.T) {
	blocks := []Block{
		{ID: 1, BlockHash: "h1"},
		{ID: 2, PrevHash: "not-h1", BlockHash: "h2"},
	}

	got, tip, reason := sanitizeContiguousLoadedBlocks(blocks)
	if tip != 1 || len(got) != 1 {
		t.Fatalf("expected truncate at height 1, got tip=%d len=%d reason=%s", tip, len(got), reason)
	}
	if reason == "" {
		t.Fatal("expected prev-hash reason")
	}
}
