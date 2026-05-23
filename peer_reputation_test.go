package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestSnapshotProviderPreferenceValuePrefersReputationOverChunkNoise(t *testing.T) {
	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	n.recordSyncPeerSnapshotResult("good-peer", true, 20*time.Millisecond, 4096, false)
	n.recordSyncPeerSnapshotResult("good-peer", true, 22*time.Millisecond, 4096, false)
	n.recordSyncPeerSnapshotResult("noisy-peer", true, 20*time.Millisecond, 4096, false)
	n.recordSyncPeerInvalidProof("noisy-peer")

	n.snapshotGossipMu.Lock()
	if n.snapshotChunkGossipCache == nil {
		n.snapshotChunkGossipCache = make(map[string]SnapshotChunkGossip)
	}
	for i := 0; i < 8; i++ {
		n.snapshotChunkGossipCache[fmt.Sprintf("noise-%d", i)] = SnapshotChunkGossip{
			From:         "noisy-peer",
			Height:       448,
			SnapshotHash: "0xabc123",
		}
	}
	n.snapshotChunkGossipCache["good"] = SnapshotChunkGossip{
		From:         "good-peer",
		Height:       448,
		SnapshotHash: "0xabc123",
	}
	n.snapshotGossipMu.Unlock()

	goodPreference := n.snapshotProviderPreferenceValue(448, "0xabc123", "good-peer")
	noisyPreference := n.snapshotProviderPreferenceValue(448, "0xabc123", "noisy-peer")
	if goodPreference <= noisyPreference {
		t.Fatalf("expected higher-reputation provider to outrank chunk-noisy peer: good=%f noisy=%f", goodPreference, noisyPreference)
	}
}

func TestSelectSnapshotPeersPrefersHigherReputation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	peerA, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create peerA host: %v", err)
	}
	defer peerA.Close()

	peerB, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create peerB host: %v", err)
	}
	defer peerB.Close()

	if err := localHost.Connect(ctx, peer.AddrInfo{ID: peerA.ID(), Addrs: peerA.Addrs()}); err != nil {
		t.Fatalf("failed to connect peerA: %v", err)
	}
	if err := localHost.Connect(ctx, peer.AddrInfo{ID: peerB.ID(), Addrs: peerB.Addrs()}); err != nil {
		t.Fatalf("failed to connect peerB: %v", err)
	}

	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	n.Host = localHost
	n.validatorSetMu.Lock()
	n.frozenValidatorsByHeight = map[uint64][]string{168: {"A", "B"}}
	n.frozenValidatorHashByHeight = map[uint64]string{168: ValidatorSetHash([]string{"A", "B"})}
	n.validatorSetMu.Unlock()

	n.peerStateMu.Lock()
	n.peerToValidator[peerA.ID().String()] = "A"
	n.peerToValidator[peerB.ID().String()] = "B"
	n.peerStateMu.Unlock()

	n.recordSyncPeerSnapshotResult(peerA.ID().String(), true, 20*time.Millisecond, 4096, false)
	n.recordSyncPeerSnapshotResult(peerA.ID().String(), true, 21*time.Millisecond, 4096, false)
	n.recordSyncPeerSnapshotResult(peerB.ID().String(), true, 20*time.Millisecond, 4096, false)
	n.recordSyncPeerInvalidProof(peerB.ID().String())

	selected := n.SelectSnapshotPeers(169, true)
	if len(selected) != 2 {
		t.Fatalf("expected both authority peers, got=%d", len(selected))
	}
	if selected[0].PeerID != peerA.ID() {
		t.Fatalf("expected higher-reputation peer first, got=%s want=%s", selected[0].PeerID, peerA.ID())
	}
}

func TestCollectExplorerPeersIncludesReputation(t *testing.T) {
	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	peerID := "peer-a"
	n.peerStateMu.Lock()
	if n.peerToValidator == nil {
		n.peerToValidator = make(map[string]string)
	}
	if n.peerRole == nil {
		n.peerRole = make(map[string]string)
	}
	if n.peerHelloOK == nil {
		n.peerHelloOK = make(map[string]bool)
	}
	if n.connectedPeers == nil {
		n.connectedPeers = make(map[string]bool)
	}
	n.peerToValidator[peerID] = "A"
	n.peerRole[peerID] = "validator"
	n.peerHelloOK[peerID] = true
	n.connectedPeers[peerID] = true
	n.peerStateMu.Unlock()

	n.recordSyncPeerSnapshotResult(peerID, true, 20*time.Millisecond, 4096, false)

	s := &Server{Node: n}
	peers := s.collectExplorerPeers()
	if len(peers) != 1 {
		t.Fatalf("expected one explorer peer entry, got=%d", len(peers))
	}
	if peers[0].Reputation <= 0 {
		t.Fatalf("expected reputation to be exposed, got=%f", peers[0].Reputation)
	}
	if peers[0].ReputationClass == "" {
		t.Fatalf("expected reputation class to be exposed")
	}
	if peers[0].SyncScore <= 0 {
		t.Fatalf("expected sync score to be exposed, got=%f", peers[0].SyncScore)
	}
}

func TestRuntimeStatusSnapshotIncludesSnapshotProviderReputations(t *testing.T) {
	emptyChain := NewBlockchain()
	n := &Node{
		Role:           "full",
		Blockchain:     &emptyChain,
		syncPeerScores: make(map[string]*SyncPeerScore),
	}
	n.recordSyncPeerSnapshotResult("peer-provider", true, 20*time.Millisecond, 4096, false)

	n.syncMu.Lock()
	n.syncProvider = "peer-provider"
	n.syncChunkProviders = []string{"peer-provider"}
	n.syncMu.Unlock()

	status := n.runtimeStatusSnapshot()
	if status.SyncProviderReputation <= 0 {
		t.Fatalf("expected sync provider reputation, got=%f", status.SyncProviderReputation)
	}
	if status.SyncProviderClass == "" {
		t.Fatalf("expected sync provider class")
	}
	if len(status.SnapshotProviderReputations) != 1 {
		t.Fatalf("expected snapshot provider reputation map, got=%v", status.SnapshotProviderReputations)
	}
	if got := status.SnapshotProviderReputations["peer-provider"]; got <= 0 {
		t.Fatalf("expected snapshot provider reputation entry, got=%f", got)
	}
	if got := status.SnapshotProviderClasses["peer-provider"]; got == "" {
		t.Fatalf("expected snapshot provider class entry")
	}
}
