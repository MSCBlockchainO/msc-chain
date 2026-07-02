package main

import "testing"

func TestChooseForkAncestorRewindRequiresAgreeingPeers(t *testing.T) {
	height, hash, votes, total, ok := chooseForkAncestorRewind(100, []forkAncestorSample{
		{height: 99, hash: "ancestor-a", peer: "p1"},
		{height: 99, hash: "ancestor-a", peer: "p2"},
		{height: 98, hash: "ancestor-b", peer: "p3"},
	}, 2, 8)
	if !ok {
		t.Fatal("expected agreeing peers to trigger rewind")
	}
	if height != 99 || hash != "ancestor-a" || votes != 2 || total != 3 {
		t.Fatalf("unexpected decision height=%d hash=%q votes=%d total=%d", height, hash, votes, total)
	}
}

func TestChooseForkAncestorRewindRejectsSinglePeerAndDeepRollback(t *testing.T) {
	if _, _, _, _, ok := chooseForkAncestorRewind(100, []forkAncestorSample{
		{height: 99, hash: "ancestor-a", peer: "p1"},
	}, 2, 8); ok {
		t.Fatal("single peer must not trigger a multi-peer rewind")
	}
	if _, _, _, _, ok := chooseForkAncestorRewind(100, []forkAncestorSample{
		{height: 10, hash: "ancestor-a", peer: "p1"},
		{height: 10, hash: "ancestor-a", peer: "p2"},
	}, 2, 8); ok {
		t.Fatal("deep rollback outside window must be rejected")
	}
}
