package main

import "testing"

func TestApplyP2PCLIListenAddrNormalizesWildcardHostPort(t *testing.T) {
	addr, port, err := applyP2PCLIListenAddr("0.0.0.0:7001")
	if err != nil {
		t.Fatalf("apply p2p listen addr: %v", err)
	}
	if addr != "/ip4/0.0.0.0/tcp/7001" {
		t.Fatalf("addr mismatch: got %q", addr)
	}
	if port != 7001 {
		t.Fatalf("port mismatch: got %d", port)
	}
}

func TestApplyP2PCLIListenAddrNormalizesColonPort(t *testing.T) {
	addr, port, err := applyP2PCLIListenAddr(":7002")
	if err != nil {
		t.Fatalf("apply p2p listen addr: %v", err)
	}
	if addr != "/ip4/0.0.0.0/tcp/7002" {
		t.Fatalf("addr mismatch: got %q", addr)
	}
	if port != 7002 {
		t.Fatalf("port mismatch: got %d", port)
	}
}

func TestApplyP2PCLIListenAddrStripsPeerID(t *testing.T) {
	addr, port, err := applyP2PCLIListenAddr("/ip4/0.0.0.0/tcp/7003/p2p/12D3KooWQyDzYWfQphFam7oxte6PgPAKPPzWWWXwXMUzjYqXxBLk")
	if err != nil {
		t.Fatalf("apply p2p listen addr: %v", err)
	}
	if addr != "/ip4/0.0.0.0/tcp/7003" {
		t.Fatalf("addr mismatch: got %q", addr)
	}
	if port != 7003 {
		t.Fatalf("port mismatch: got %d", port)
	}
}
