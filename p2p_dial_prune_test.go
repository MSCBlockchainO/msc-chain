package main

import (
	"errors"
	"testing"
	"time"
)

func TestIsLocalhostPeerAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"/ip4/127.0.0.1/tcp/7001/p2p/abc", true},
		{"/ip6/::1/tcp/7001/p2p/abc", true},
		{"127.0.0.1:7001", true},
		{"/dns/localhost/tcp/7001/p2p/abc", true},
		{"/ip4/10.0.0.1/tcp/7001/p2p/abc", false},
		{"/ip4/192.168.1.10/tcp/7001/p2p/abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLocalhostPeerAddr(c.addr); got != c.want {
			t.Fatalf("isLocalhostPeerAddr(%q)=%t want=%t", c.addr, got, c.want)
		}
	}
}

func TestIsDialRefusedError(t *testing.T) {
	if !isDialRefusedError(errors.New("connectex: No connection could be made because the target machine actively refused it.")) {
		t.Fatalf("expected actively refused to match")
	}
	if !isDialRefusedError(errors.New("dial tcp 127.0.0.1:7005: connection refused")) {
		t.Fatalf("expected connection refused to match")
	}
	if isDialRefusedError(errors.New("i/o timeout")) {
		t.Fatalf("did not expect timeout to match")
	}
}

func TestShouldPruneLocalhostDialRefused(t *testing.T) {
	oldEnabled := PruneLocalhostOnRefused
	oldThreshold := PruneLocalhostRefusedFailures
	defer func() {
		PruneLocalhostOnRefused = oldEnabled
		PruneLocalhostRefusedFailures = oldThreshold
	}()

	PruneLocalhostOnRefused = true
	PruneLocalhostRefusedFailures = 3

	n := &Node{
		peerDialFailures: make(map[string]int),
		peerDialNext:     make(map[string]time.Time),
	}
	n.peerDialFailures["peer"] = 2
	n.recordDialFailure("peer")

	addr := "/ip4/127.0.0.1/tcp/7005/p2p/peer"
	if !shouldPruneLocalhostDialRefused(n, "peer", addr, errors.New("connection refused")) {
		t.Fatalf("expected localhost prune after threshold")
	}
	if shouldPruneLocalhostDialRefused(n, "peer", "/ip4/10.0.0.1/tcp/7005/p2p/peer", errors.New("connection refused")) {
		t.Fatalf("did not expect prune for non-localhost")
	}
	if shouldPruneLocalhostDialRefused(n, "peer", addr, errors.New("i/o timeout")) {
		t.Fatalf("did not expect prune for non-refused error")
	}
	PruneLocalhostOnRefused = false
	if shouldPruneLocalhostDialRefused(n, "peer", addr, errors.New("connection refused")) {
		t.Fatalf("did not expect prune when disabled")
	}
}

func TestValidatePeerHelloClearsQuarantineOnSuccess(t *testing.T) {
	peerID := "12D3KooWTestPeer"
	n := &Node{
		peerHelloOK:     make(map[string]bool),
		quarantineUntil: make(map[string]time.Time),
	}
	n.quarantineUntil[peerID] = time.Now().Add(30 * time.Minute)

	hello := PeerHello{
		ChainID:       ChainID,
		GenesisHash:   GenesisHash,
		Version:       Version,
		ConsensusHash: consensusParamsHash(),
	}
	if !n.validatePeerHello(peerID, hello) {
		t.Fatalf("expected peer hello validation success")
	}
	if n.isPeerQuarantined(peerID) {
		t.Fatalf("expected quarantine to clear after successful peer hello")
	}
	if !n.isPeerHelloOK(peerID) {
		t.Fatalf("expected peer hello to be marked verified")
	}
}

func TestQuarantineDurationForPeerInfoProtocolMismatch(t *testing.T) {
	got := quarantineDurationFor("peerinfo_protocol_mismatch")
	if got != peerQuarantineForPeerInfoStream {
		t.Fatalf("unexpected quarantine duration: got=%s want=%s", got, peerQuarantineForPeerInfoStream)
	}
}

func TestQuarantineDurationForPeerFlapIsShort(t *testing.T) {
	got := quarantineDurationFor("peer_flap")
	if got != peerQuarantineForFlap {
		t.Fatalf("unexpected peer flap quarantine duration: got=%s want=%s", got, peerQuarantineForFlap)
	}
}
