package main

import "testing"

func TestBuildAndCommitTwoHundredBlocks(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	for i := 0; i < 200; i++ {
		block := node.BuildLeaderBlock(node.currentEpoch())
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)

		if err := node.ReceiveBlock(block, node.Blockchain); err != nil {
			t.Fatalf("block %d rejected: %v", block.ID, err)
		}
		if got := node.Blockchain.Height(); got != block.ID {
			t.Fatalf("height after block %d = %d", block.ID, got)
		}
	}

	if got := node.Blockchain.Height(); got != 200 {
		t.Fatalf("final height = %d, want 200", got)
	}
}
