package main

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestNewSyncControllerInitializesDefaults(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peers := []peer.ID{"peer-a", "peer-b"}

	controller := NewSyncController(node, peers)
	if controller == nil {
		t.Fatal("expected controller")
	}
	if controller.chain != node.Blockchain {
		t.Fatal("expected controller to use node blockchain")
	}
	if controller.provider != peers[0] {
		t.Fatalf("expected initial provider %q, got %q", peers[0], controller.provider)
	}
	if cap(controller.blockQueue) != syncControllerQueueCapacity {
		t.Fatalf("expected queue capacity %d, got %d", syncControllerQueueCapacity, cap(controller.blockQueue))
	}
}

func TestSyncControllerComputeBatchSize(t *testing.T) {
	controller := &SyncController{}

	tests := []struct {
		lag  uint64
		want uint64
	}{
		{lag: 50, want: 128},
		{lag: 201, want: 512},
		{lag: 1001, want: 1024},
		{lag: 5001, want: 2048},
	}

	for _, tc := range tests {
		if got := controller.computeBatchSize(tc.lag); got != tc.want {
			t.Fatalf("lag=%d expected batch=%d got=%d", tc.lag, tc.want, got)
		}
	}
}

func TestSyncControllerRecordFailureThreshold(t *testing.T) {
	controller := &SyncController{applyFailures: make(map[string]int)}

	for attempt := 1; attempt <= syncControllerFailureBudget; attempt++ {
		if controller.recordFailure("hash-a") {
			t.Fatalf("unexpected threshold trigger at attempt=%d", attempt)
		}
	}
	if !controller.recordFailure("hash-a") {
		t.Fatal("expected failure budget trigger after threshold")
	}
	controller.clearFailure("hash-a")
	if controller.recordFailure("hash-a") {
		t.Fatal("expected cleared failure budget to reset counter")
	}
}

func TestSyncControllerRotateProviderPrefersConnectedHighScorePeer(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerA := peer.ID("peer-a")
	peerB := peer.ID("peer-b")
	peerC := peer.ID("peer-c")

	if node.connectedPeers == nil {
		node.connectedPeers = make(map[string]bool)
	}
	if node.syncPeerScores == nil {
		node.syncPeerScores = make(map[string]*SyncPeerScore)
	}
	node.connectedPeers[peerA.String()] = true
	node.connectedPeers[peerB.String()] = true
	node.connectedPeers[peerC.String()] = false
	node.syncPeerScores[peerA.String()] = &SyncPeerScore{BlockBatchSuccess: 1}
	node.syncPeerScores[peerB.String()] = &SyncPeerScore{BlockBatchSuccess: 5}
	node.syncPeerScores[peerC.String()] = &SyncPeerScore{BlockBatchSuccess: 50}

	controller := NewSyncController(node, []peer.ID{peerA, peerB, peerC})
	controller.provider = peerA

	controller.rotateProvider()

	if controller.currentProvider() != peerB {
		t.Fatalf("expected provider rotation to choose connected higher-score peer %q, got %q", peerB, controller.currentProvider())
	}
}

func TestSyncControllerDetectStall(t *testing.T) {
	controller := &SyncController{lastProgress: time.Now().Add(-syncControllerStallTimeout - time.Second)}
	if !controller.detectStall() {
		t.Fatal("expected stall detection to trigger")
	}
	controller.recordProgress()
	if controller.detectStall() {
		t.Fatal("expected fresh progress to clear stall state")
	}
}
