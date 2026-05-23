package main

import "testing"

func TestExplorerAvailableFinalizedHeightClampsToLocalTip(t *testing.T) {
	server := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{
				{ID: 250, BlockHash: "block-250"},
				{ID: 251, BlockHash: "block-251"},
			}},
			finalizedHeight: 265,
		},
	}

	if got := server.explorerFinalizedHeight(); got != 265 {
		t.Fatalf("unexpected best-known finalized height: got=%d want=265", got)
	}
	if got := server.explorerAvailableFinalizedHeight(); got != 251 {
		t.Fatalf("unexpected available finalized height: got=%d want=251", got)
	}
}

func TestBuildTxStatusSnapshotUsesAvailableFinalizedHeight(t *testing.T) {
	server := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{
				{
					ID:        251,
					BlockHash: "block-251",
					Transactions: []Transaction{
						{ID: "tx-251"},
					},
				},
			}},
			finalizedHeight: 265,
		},
	}

	snap := server.buildTxStatusSnapshot("tx-251")
	if snap.State != "confirmed" {
		t.Fatalf("unexpected state: got=%q want=confirmed", snap.State)
	}
	if snap.Height != 251 {
		t.Fatalf("unexpected tx height: got=%d want=251", snap.Height)
	}
	if snap.LatestHeight != 251 {
		t.Fatalf("unexpected latest height: got=%d want=251", snap.LatestHeight)
	}
	if snap.FinalizedHeight != 251 {
		t.Fatalf("unexpected finalized height: got=%d want=251", snap.FinalizedHeight)
	}
	if snap.Confirmations != 1 {
		t.Fatalf("unexpected confirmations: got=%d want=1", snap.Confirmations)
	}
	if !snap.IsFinalized {
		t.Fatalf("expected tx to be finalized")
	}
}
