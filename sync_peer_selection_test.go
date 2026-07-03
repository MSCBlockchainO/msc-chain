package main

import (
	"context"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPickSyncPeersPrefersHigherAckHeightOverStaleMappedPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	stalePeer, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create stale peer host: %v", err)
	}
	defer stalePeer.Close()

	freshPeer, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create fresh peer host: %v", err)
	}
	defer freshPeer.Close()

	if err := localHost.Connect(ctx, peer.AddrInfo{ID: stalePeer.ID(), Addrs: stalePeer.Addrs()}); err != nil {
		t.Fatalf("failed to connect stale peer: %v", err)
	}
	if err := localHost.Connect(ctx, peer.AddrInfo{ID: freshPeer.ID(), Addrs: freshPeer.Addrs()}); err != nil {
		t.Fatalf("failed to connect fresh peer: %v", err)
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D", "G"})
	node.Host = localHost

	node.peerStateMu.Lock()
	if node.peerAckHeight == nil {
		node.peerAckHeight = make(map[string]uint64)
	}
	node.peerToValidator[stalePeer.ID().String()] = "G"
	node.peerAckHeight[freshPeer.ID().String()] = 701
	node.peerStateMu.Unlock()

	node.validatorMu.Lock()
	node.validatorStatus["G"] = &ValidatorStatus{
		ReportedHeight:  668,
		FinalizedHeight: 668,
		LastSeen:        time.Now(),
		Active:          true,
	}
	node.validatorMu.Unlock()

	peers := node.pickSyncPeers(700, map[peer.ID]struct{}{}, 2)
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0] != freshPeer.ID() {
		t.Fatalf("expected fresh peer first, got %s want %s", peers[0], freshPeer.ID())
	}
	if peers[1] != stalePeer.ID() {
		t.Fatalf("expected stale peer second, got %s want %s", peers[1], stalePeer.ID())
	}
}

func TestPickSyncPeersPrefersFastReadyPeerOverTallerPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	tallerPeer, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create taller peer host: %v", err)
	}
	defer tallerPeer.Close()

	fastPeer, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create fast peer host: %v", err)
	}
	defer fastPeer.Close()

	if err := localHost.Connect(ctx, peer.AddrInfo{ID: tallerPeer.ID(), Addrs: tallerPeer.Addrs()}); err != nil {
		t.Fatalf("failed to connect taller peer: %v", err)
	}
	if err := localHost.Connect(ctx, peer.AddrInfo{ID: fastPeer.ID(), Addrs: fastPeer.Addrs()}); err != nil {
		t.Fatalf("failed to connect fast peer: %v", err)
	}

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.Host = localHost

	node.peerStateMu.Lock()
	if node.peerAckHeight == nil {
		node.peerAckHeight = make(map[string]uint64)
	}
	node.peerAckHeight[tallerPeer.ID().String()] = 725
	node.peerAckHeight[fastPeer.ID().String()] = 700
	node.peerStateMu.Unlock()

	node.syncPeerScoreMu.Lock()
	node.syncPeerScores = map[string]*SyncPeerScore{
		tallerPeer.ID().String(): {
			BlockBatchSuccess: 1,
			AvgLatencyMs:      1800,
			LastBytesPerSec:   1024,
			UpdatedAt:         time.Now(),
		},
		fastPeer.ID().String(): {
			BlockBatchSuccess: 12,
			AvgLatencyMs:      25,
			LastBytesPerSec:   2 * 1024 * 1024,
			UpdatedAt:         time.Now(),
		},
	}
	node.syncPeerScoreMu.Unlock()

	peers := node.pickSyncPeers(700, map[peer.ID]struct{}{}, 2)
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0] != fastPeer.ID() {
		t.Fatalf("expected fast ready peer first, got %s want %s", peers[0], fastPeer.ID())
	}
}
