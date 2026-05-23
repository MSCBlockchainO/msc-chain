package main

import (
	"testing"
	"time"
)

func TestP2PPeerObservabilityTracksLatencyAndDisconnectRate(t *testing.T) {
	node := &Node{}

	node.recordSyncPeerBlockResult("peer-fast", true, 100*time.Millisecond, 1024, false)
	node.recordSyncPeerSnapshotResult("peer-slow", false, 300*time.Millisecond, 2048, true)
	node.recordPeerRateLimitDrop("peer-slow", "block")
	node.recordPeerSecurityFault("peer-slow", "malformed_block")
	node.observePeerDisconnect("unit_disconnect")
	node.observePeerDisconnect("unit_disconnect")

	peerObs := node.peerObservabilitySnapshot()
	if peerObs.AverageLatencyMs <= 0 {
		t.Fatalf("expected peer latency metric to be populated: %+v", peerObs)
	}
	if peerObs.MaxLatencyMs < peerObs.AverageLatencyMs {
		t.Fatalf("max latency should be at least average latency: %+v", peerObs)
	}
	if peerObs.RateLimitDropsTotal != 1 {
		t.Fatalf("unexpected rate-limit drops: got=%d want=1", peerObs.RateLimitDropsTotal)
	}
	if peerObs.SecurityFaultsTotal != 1 {
		t.Fatalf("unexpected security faults: got=%d want=1", peerObs.SecurityFaultsTotal)
	}

	obs := node.observabilityStatsSnapshot()
	if obs.PeerDisconnectTotal != 2 {
		t.Fatalf("unexpected disconnect count: got=%d want=2", obs.PeerDisconnectTotal)
	}
	if rate := peerDisconnectRatePerMinute(obs); rate <= 0 {
		t.Fatalf("expected positive disconnect rate, got=%f", rate)
	}
}

func TestP2PBlockPropagationObservabilityTracksGossipTiming(t *testing.T) {
	node := &Node{}
	block := Block{ID: 77, BlockHash: "block-77", PrevHash: "block-76"}

	node.observeBlockPropagation(block, time.Now().Add(-125*time.Millisecond))

	obs := node.observabilityStatsSnapshot()
	if obs.BlockGossipReceivedTotal != 1 {
		t.Fatalf("unexpected block propagation total: got=%d want=1", obs.BlockGossipReceivedTotal)
	}
	if obs.BlockPropagationHeight != block.ID {
		t.Fatalf("unexpected block propagation height: got=%d want=%d", obs.BlockPropagationHeight, block.ID)
	}
	if obs.BlockPropagationLastMs == 0 || obs.BlockPropagationMaxMs == 0 {
		t.Fatalf("expected non-zero block propagation timing: last=%d max=%d", obs.BlockPropagationLastMs, obs.BlockPropagationMaxMs)
	}
}
