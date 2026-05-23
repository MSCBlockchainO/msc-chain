package main

import (
	"context"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestSelectSnapshotPeersFallsBackToMappedValidatorsWhenAuthorityUnknown(t *testing.T) {
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
	n.Blockchain.mu.Lock()
	n.Blockchain.Blocks = append(n.Blockchain.Blocks, Block{ID: 98, BlockHash: "h98"})
	n.Blockchain.mu.Unlock()

	n.peerStateMu.Lock()
	n.peerToValidator[peerA.ID().String()] = "A"
	n.peerToValidator[peerB.ID().String()] = "B"
	n.peerStateMu.Unlock()

	selected := n.SelectSnapshotPeers(169, true)
	if len(selected) != 2 {
		t.Fatalf("expected mapped validator fallback peers, got=%d", len(selected))
	}

	seen := make(map[string]struct{}, len(selected))
	for _, info := range selected {
		seen[normalizeValidatorID(info.ValidatorID)] = struct{}{}
	}
	if _, ok := seen["A"]; !ok {
		t.Fatalf("expected validator A in selected peers")
	}
	if _, ok := seen["B"]; !ok {
		t.Fatalf("expected validator B in selected peers")
	}
}

func TestSelectSnapshotPeersPreservesAuthorityFilterWhenAvailable(t *testing.T) {
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
	n.Blockchain.mu.Lock()
	n.Blockchain.Blocks = append(n.Blockchain.Blocks, Block{ID: 98, BlockHash: "h98"})
	n.Blockchain.mu.Unlock()

	n.validatorSetMu.Lock()
	n.frozenValidatorsByHeight = map[uint64][]string{168: {"A"}}
	n.frozenValidatorHashByHeight = map[uint64]string{168: ValidatorSetHash([]string{"A"})}
	n.validatorSetMu.Unlock()

	n.peerStateMu.Lock()
	n.peerToValidator[peerA.ID().String()] = "A"
	n.peerToValidator[peerB.ID().String()] = "B"
	n.peerStateMu.Unlock()

	selected := n.SelectSnapshotPeers(169, true)
	if len(selected) != 1 {
		t.Fatalf("expected exactly one authority peer, got=%d", len(selected))
	}
	if got := normalizeValidatorID(selected[0].ValidatorID); got != "A" {
		t.Fatalf("unexpected selected validator: got=%s want=A", got)
	}
}
