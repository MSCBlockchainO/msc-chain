package main

import (
	"strings"
	"testing"
	"time"

	ma "github.com/multiformats/go-multiaddr"
)

func withResourceLimitGlobals(t *testing.T) {
	t.Helper()
	oldMemory := PeerMemoryQuotaBytes
	oldBandwidth := PeerBandwidthQuotaBytesPerMinute
	oldTx := PeerMempoolTxPerMinute
	oldBlocks := PeerBlockRequestsPerMinute
	oldConn := PeerConnectionFloodMaxPerWindow
	oldWindow := PeerResourceWindowDuration
	oldDiscoveryMax := PeerDiscoveryMaxAddrs
	t.Cleanup(func() {
		PeerMemoryQuotaBytes = oldMemory
		PeerBandwidthQuotaBytesPerMinute = oldBandwidth
		PeerMempoolTxPerMinute = oldTx
		PeerBlockRequestsPerMinute = oldBlocks
		PeerConnectionFloodMaxPerWindow = oldConn
		PeerResourceWindowDuration = oldWindow
		PeerDiscoveryMaxAddrs = oldDiscoveryMax
	})
}

func TestResourceExhaustionPeerQuotas(t *testing.T) {
	withResourceLimitGlobals(t)
	PeerMemoryQuotaBytes = 64
	PeerBandwidthQuotaBytesPerMinute = 128
	PeerMempoolTxPerMinute = 2
	PeerBlockRequestsPerMinute = 1
	PeerResourceWindowDuration = time.Minute

	n := &Node{syncPeerScores: make(map[string]*SyncPeerScore)}
	n.ensurePeerIsolationMaps()
	peerID := "peer-quota"

	if !n.allowPeerResource(peerID, MsgTx, 16) || !n.allowPeerResource(peerID, MsgTx, 16) {
		t.Fatalf("expected first two tx messages within quota")
	}
	if n.allowPeerResource(peerID, MsgTx, 16) {
		t.Fatalf("expected tx quota to reject third tx in window")
	}
	if !n.allowPeerResource("peer-block", MsgGetBlocks, 16) {
		t.Fatalf("expected first block request within quota")
	}
	if n.allowPeerResource("peer-block", MsgGetBlocks, 16) {
		t.Fatalf("expected second block request to be rejected")
	}
	if n.allowPeerResource("peer-big", MsgBlock, 256) {
		t.Fatalf("expected oversized payload to be rejected")
	}

	obs := n.observabilityStatsSnapshot()
	if obs.PeerResourceDropTotal < 3 {
		t.Fatalf("expected resource drops to be recorded, got=%d", obs.PeerResourceDropTotal)
	}
}

func TestConnectionFloodLimitRejectsBurst(t *testing.T) {
	withResourceLimitGlobals(t)
	PeerConnectionFloodMaxPerWindow = 3
	PeerResourceWindowDuration = time.Minute

	n := &Node{}
	n.ensurePeerIsolationMaps()
	key := "subnet:203.0.113.0/24"
	for i := 0; i < 3; i++ {
		if !n.allowPeerConnectionFloodKey(key) {
			t.Fatalf("connection %d unexpectedly rejected", i+1)
		}
	}
	if n.allowPeerConnectionFloodKey(key) {
		t.Fatalf("expected burst connection flood to be rejected")
	}
	if got := n.observabilityStatsSnapshot().PeerConnectionFloodTotal; got != 1 {
		t.Fatalf("expected one connection flood metric, got=%d", got)
	}
}

func TestDiscoveryAddressValidationRejectsPoisonedCache(t *testing.T) {
	withResourceLimitGlobals(t)
	PeerDiscoveryMaxAddrs = 2

	good1, _ := ma.NewMultiaddr("/ip4/203.0.113.10/tcp/7001")
	good2, _ := ma.NewMultiaddr("/dns4/bootstrap.example/tcp/7002")
	unspecified, _ := ma.NewMultiaddr("/ip4/0.0.0.0/tcp/1")
	multicast, _ := ma.NewMultiaddr("/ip4/224.0.0.1/tcp/2")

	out := sanitizeDiscoveredAddrs([]ma.Multiaddr{unspecified, good1, good1, multicast, good2})
	if len(out) != 2 {
		t.Fatalf("expected two sanitized addrs, got=%d", len(out))
	}
	joined := out[0].String() + " " + out[1].String()
	if !strings.Contains(joined, "203.0.113.10") || !strings.Contains(joined, "bootstrap.example") {
		t.Fatalf("unexpected sanitized addrs: %s", joined)
	}
}

func TestPeerScoreDecayAllowsReputationRecovery(t *testing.T) {
	score := &SyncPeerScore{
		DialSuccess:        1,
		DialFailure:        64,
		SecurityFaultCount: 8,
		RateLimitDropCount: 8,
		DecayedAt:          time.Now().Add(-4 * PeerReputationDecayInterval),
	}
	before, _ := peerReputationValue(score)
	decaySyncPeerScore(score, time.Now())
	after, _ := peerReputationValue(score)
	if after <= before {
		t.Fatalf("expected reputation to recover after decay: before=%f after=%f", before, after)
	}
	if score.SecurityFaultCount >= 8 || score.RateLimitDropCount >= 8 {
		t.Fatalf("expected temporary penalties to decay, got security=%d rate=%d", score.SecurityFaultCount, score.RateLimitDropCount)
	}
}

func TestDDoSSimulationResourceQuotas100To1000Peers(t *testing.T) {
	withResourceLimitGlobals(t)
	PeerMemoryQuotaBytes = 256
	PeerBandwidthQuotaBytesPerMinute = 512
	PeerMempoolTxPerMinute = 2
	PeerBlockRequestsPerMinute = 1
	PeerConnectionFloodMaxPerWindow = 50
	PeerResourceWindowDuration = time.Minute

	n := &Node{syncPeerScores: make(map[string]*SyncPeerScore)}
	n.ensurePeerIsolationMaps()

	for _, peers := range []int{100, 500, 1000} {
		startDrops := n.observabilityStatsSnapshot().PeerResourceDropTotal
		for i := 0; i < peers; i++ {
			peerID := "ddos-peer-" + string(rune('a'+(i%26))) + "-" + string(rune('0'+(i%10))) + "-" + string(rune('A'+((i/10)%26)))
			_ = n.allowPeerResource(peerID, MsgTx, 64)
			_ = n.allowPeerResource(peerID, MsgTx, 64)
			if n.allowPeerResource(peerID, MsgTx, 64) {
				t.Fatalf("expected tx flood rejection for peers=%d peer=%s", peers, peerID)
			}
			_ = n.allowPeerResource(peerID, MsgGetBlocks, 32)
			if n.allowPeerResource(peerID, MsgGetBlocks, 32) {
				t.Fatalf("expected request flood rejection for peers=%d peer=%s", peers, peerID)
			}
			if n.allowPeerResource(peerID, MsgBlock, 512) {
				t.Fatalf("expected invalid/oversized block flood rejection for peers=%d peer=%s", peers, peerID)
			}
		}
		endDrops := n.observabilityStatsSnapshot().PeerResourceDropTotal
		if endDrops-startDrops < uint64(peers*3) {
			t.Fatalf("expected drops for each flood vector peers=%d got=%d", peers, endDrops-startDrops)
		}
	}
}
