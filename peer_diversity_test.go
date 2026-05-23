package main

import (
	"strings"
	"testing"

	ma "github.com/multiformats/go-multiaddr"
)

func TestPeerSubnetKeyFromMultiaddrIPv4(t *testing.T) {
	oldPrefix := PeerDiversityIPv4Prefix
	defer func() { PeerDiversityIPv4Prefix = oldPrefix }()
	PeerDiversityIPv4Prefix = 24

	maddr, err := ma.NewMultiaddr("/ip4/8.8.8.8/tcp/7001")
	if err != nil {
		t.Fatalf("multiaddr: %v", err)
	}
	got := peerSubnetKeyFromMultiaddr(maddr)
	if got != "8.8.8.0/24" {
		t.Fatalf("unexpected subnet key: %s", got)
	}
}

func TestPeerSubnetKeyFromMultiaddrIPv6(t *testing.T) {
	oldPrefix := PeerDiversityIPv6Prefix
	defer func() { PeerDiversityIPv6Prefix = oldPrefix }()
	PeerDiversityIPv6Prefix = 64

	maddr, err := ma.NewMultiaddr("/ip6/2001:db8::1/tcp/7001")
	if err != nil {
		t.Fatalf("multiaddr: %v", err)
	}
	got := peerSubnetKeyFromMultiaddr(maddr)
	if !strings.HasSuffix(got, "/64") {
		t.Fatalf("expected /64 subnet, got=%s", got)
	}
	if !strings.Contains(got, "2001:db8") {
		t.Fatalf("unexpected ipv6 subnet key: %s", got)
	}
}

func TestPeerSubnetKeyFromMultiaddrLoopbackBypass(t *testing.T) {
	maddr, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/7001")
	if err != nil {
		t.Fatalf("multiaddr: %v", err)
	}
	if got := peerSubnetKeyFromMultiaddr(maddr); got != "" {
		t.Fatalf("loopback should bypass diversity key, got=%s", got)
	}
}

func TestPeerDiversityASNFromEnv(t *testing.T) {
	t.Setenv("MSC_PEER_ASN_MAP", "peer-a=64500;peer-b=AS64501")

	if got := peerDiversityASNFromEnv("peer-a"); got != "AS64500" {
		t.Fatalf("unexpected env ASN for peer-a: %s", got)
	}
	if got := peerDiversityASNFromEnv("peer-b"); got != "AS64501" {
		t.Fatalf("unexpected env ASN for peer-b: %s", got)
	}
}

func TestPeerDiversityASNLimitRejectsKnownASN(t *testing.T) {
	oldEnabled := PeerDiversityEnabled
	oldMaxASN := PeerDiversityMaxPerASN
	oldDebug := DebugNet
	defer func() {
		PeerDiversityEnabled = oldEnabled
		PeerDiversityMaxPerASN = oldMaxASN
		DebugNet = oldDebug
	}()
	PeerDiversityEnabled = true
	PeerDiversityMaxPerASN = 2
	DebugNet = false

	n := &Node{}
	n.ensurePeerIsolationMaps()
	for _, peerID := range []string{"peer-a", "peer-b"} {
		n.setPeerConnected(peerID, true)
		n.setPeerDiversityASN(peerID, "AS64500")
	}
	n.setPeerDiversityASN("candidate", "AS64500")

	if n.allowPeerDiversityASN("candidate", false) {
		t.Fatalf("expected ASN diversity limit to reject third peer in AS64500")
	}
	snap := n.peerDiversitySnapshot()
	if snap.ASNBuckets != 1 {
		t.Fatalf("expected one ASN bucket, got=%d", snap.ASNBuckets)
	}
	if snap.RejectTotal != 1 {
		t.Fatalf("expected one diversity rejection, got=%d", snap.RejectTotal)
	}
}

func TestPeerDiversityOutboundASNLimitRejectsConcentration(t *testing.T) {
	oldEnabled := PeerDiversityEnabled
	oldMaxOutboundASN := PeerDiversityMaxOutboundPerASN
	oldDebug := DebugNet
	defer func() {
		PeerDiversityEnabled = oldEnabled
		PeerDiversityMaxOutboundPerASN = oldMaxOutboundASN
		DebugNet = oldDebug
	}()
	PeerDiversityEnabled = true
	PeerDiversityMaxOutboundPerASN = 1
	DebugNet = false

	n := &Node{}
	n.ensurePeerIsolationMaps()
	n.setPeerConnected("peer-a", true)
	n.setPeerDiversityASN("peer-a", "AS64500")
	n.peerStateMu.Lock()
	n.peerOutbound["peer-a"] = true
	n.peerStateMu.Unlock()
	n.setPeerDiversityASN("candidate", "AS64500")

	if n.allowPeerDiversityASN("candidate", true) {
		t.Fatalf("expected outbound ASN diversity limit to reject concentrated dial")
	}
	snap := n.peerDiversitySnapshot()
	if snap.OutboundASNBuckets != 1 {
		t.Fatalf("expected one outbound ASN bucket, got=%d", snap.OutboundASNBuckets)
	}
	if snap.OutboundRejectTotal != 1 {
		t.Fatalf("expected one outbound diversity rejection, got=%d", snap.OutboundRejectTotal)
	}
}

func TestPeerDiversityOutboundSubnetLimitRejectsDial(t *testing.T) {
	oldEnabled := PeerDiversityEnabled
	oldMaxOutboundSubnet := PeerDiversityMaxOutboundPerSubnet
	oldDebug := DebugNet
	defer func() {
		PeerDiversityEnabled = oldEnabled
		PeerDiversityMaxOutboundPerSubnet = oldMaxOutboundSubnet
		DebugNet = oldDebug
	}()
	PeerDiversityEnabled = true
	PeerDiversityMaxOutboundPerSubnet = 1
	DebugNet = false

	existing, err := ma.NewMultiaddr("/ip4/203.0.113.10/tcp/7001")
	if err != nil {
		t.Fatalf("existing multiaddr: %v", err)
	}
	target, err := ma.NewMultiaddr("/ip4/203.0.113.44/tcp/7002")
	if err != nil {
		t.Fatalf("target multiaddr: %v", err)
	}

	n := &Node{}
	n.ensurePeerIsolationMaps()
	n.setPeerConnected("peer-a", true)
	n.rememberPeerDiversityAddr("peer-a", []ma.Multiaddr{existing}, true)

	if n.allowPeerDiversityDial(target) {
		t.Fatalf("expected outbound subnet diversity limit to reject dial")
	}
	snap := n.peerDiversitySnapshot()
	if snap.OutboundSubnetBuckets != 1 {
		t.Fatalf("expected one outbound subnet bucket, got=%d", snap.OutboundSubnetBuckets)
	}
	if snap.OutboundRejectTotal != 1 {
		t.Fatalf("expected outbound diversity rejection, got=%d", snap.OutboundRejectTotal)
	}
}
