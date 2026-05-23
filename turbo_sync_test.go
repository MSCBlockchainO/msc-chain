package main

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestNewTurboSyncInitializesDefaults(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peers := []peer.ID{"peer-a", "peer-b"}

	turbo := NewTurboSync(node, peers)
	if turbo == nil {
		t.Fatal("expected turbo sync controller")
	}
	if turbo.chain != node.Blockchain {
		t.Fatal("expected controller to use node blockchain")
	}
	if turbo.provider != peers[0] {
		t.Fatalf("expected provider %q got %q", peers[0], turbo.provider)
	}
	if cap(turbo.blockQueue) != turboSyncQueueCapacity {
		t.Fatalf("expected queue capacity %d got %d", turboSyncQueueCapacity, cap(turbo.blockQueue))
	}
}

func TestTurboSyncDecideModeThreshold(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	turbo := NewTurboSync(node, []peer.ID{"peer-a"})
	turbo.targetHeight = turbo.chain.Height() + turboSyncReplayThreshold + 1

	turbo.decideMode()

	if !turbo.isTurboMode() {
		t.Fatal("expected turbo mode to be enabled")
	}
}

func TestPlanTurboAssignmentsSplitsRanges(t *testing.T) {
	assignments := planTurboAssignments(100, 199, []peer.ID{"peer-a", "peer-b", "peer-c"})
	if len(assignments) != 3 {
		t.Fatalf("expected 3 assignments got %d", len(assignments))
	}
	if assignments[0].From != 100 {
		t.Fatalf("expected first assignment to start at 100 got %d", assignments[0].From)
	}
	if assignments[len(assignments)-1].To != 199 {
		t.Fatalf("expected last assignment to end at 199 got %d", assignments[len(assignments)-1].To)
	}
}

func TestTurboSyncSelectPeersFiltersFailedPeers(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerA := peer.ID("peer-a")
	peerB := peer.ID("peer-b")
	if node.connectedPeers == nil {
		node.connectedPeers = make(map[string]bool)
	}
	node.connectedPeers[peerA.String()] = true
	node.connectedPeers[peerB.String()] = true

	turbo := NewTurboSync(node, []peer.ID{peerA, peerB})
	turbo.peerFailures[peerA.String()] = turboSyncPeerFailBudget

	turbo.selectPeers()

	active := turbo.currentActivePeers()
	if len(active) != 1 || active[0] != peerB {
		t.Fatalf("expected only peer-b to remain active, got %v", active)
	}
}

func TestTurboSyncDetectStall(t *testing.T) {
	turbo := &TurboSync{lastProgress: time.Now().Add(-turboSyncStallTimeout - time.Second)}
	if !turbo.detectStall() {
		t.Fatal("expected stall detection")
	}
	turbo.recordProgress()
	if turbo.detectStall() {
		t.Fatal("expected fresh progress to clear stall")
	}
}
