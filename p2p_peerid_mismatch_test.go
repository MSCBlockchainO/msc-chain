package main

import (
	"errors"
	"runtime"
	"testing"
)

func TestRemotePeerIDFromMismatchError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "remote key matches marker",
			err:  errors.New("failed to negotiate security protocol: peer id mismatch: expected 12D3KooA, but remote key matches 12D3KooWDDMahYs6k9Gu61PNkCnkckG6BByJdDnJN2uUJa257PVR"),
			want: "12D3KooWDDMahYs6k9Gu61PNkCnkckG6BByJdDnJN2uUJa257PVR",
		},
		{
			name: "but got marker",
			err:  errors.New("peer ID mismatch: expected 12D3KooA but got 12D3KooWSEQhoUiBNwfWgrv7ygWBTCGWRrL7K8z8ANcu6NXvk4ZA"),
			want: "12D3KooWSEQhoUiBNwfWgrv7ygWBTCGWRrL7K8z8ANcu6NXvk4ZA",
		},
		{
			name: "no marker",
			err:  errors.New("dial backoff"),
			want: "",
		},
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := remotePeerIDFromMismatchError(tc.err)
			if got != tc.want {
				t.Fatalf("remotePeerIDFromMismatchError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPeerAddrWithPeerID(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		peerID string
		want   string
		ok     bool
	}{
		{
			name:   "replace existing p2p id",
			raw:    "/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWOLD",
			peerID: "12D3KooWNEW",
			want:   "/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWNEW",
			ok:     true,
		},
		{
			name:   "append missing p2p id",
			raw:    "/ip4/127.0.0.1/tcp/7002",
			peerID: "12D3KooWNEW",
			want:   "/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWNEW",
			ok:     true,
		},
		{
			name:   "invalid raw address",
			raw:    "127.0.0.1:7002",
			peerID: "12D3KooWNEW",
			want:   "",
			ok:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := peerAddrWithPeerID(tc.raw, tc.peerID)
			if ok != tc.ok {
				t.Fatalf("peerAddrWithPeerID() ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("peerAddrWithPeerID() addr = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRefreshPeerIDMismatchAllowsTrustedPublicPersistentPeer(t *testing.T) {
	oldPersist := PersistPeerIDRefresh
	PersistPeerIDRefresh = false
	t.Cleanup(func() {
		PersistPeerIDRefresh = oldPersist
	})

	oldPeer := "12D3KooWOLD"
	newPeer := "12D3KooWDDMahYs6k9Gu61PNkCnkckG6BByJdDnJN2uUJa257PVR"
	raw := "/ip4/54.80.4.133/tcp/7001/p2p/" + oldPeer
	n := &Node{
		Config:         &NodeConfig{PersistentPeers: []string{raw}},
		allowedPeerIDs: map[string]bool{},
	}
	err := errors.New("peer ID mismatch: expected " + oldPeer + " but got " + newPeer)

	if !n.refreshPeerIDMismatch(raw, oldPeer, err) {
		t.Fatalf("expected trusted public persistent peer-id mismatch to refresh")
	}
	want := "/ip4/54.80.4.133/tcp/7001/p2p/" + newPeer
	if len(n.Config.PersistentPeers) != 1 || n.Config.PersistentPeers[0] != want {
		t.Fatalf("persistent peers = %#v, want %q", n.Config.PersistentPeers, want)
	}
	if !n.allowedPeerIDs[newPeer] {
		t.Fatalf("expected refreshed peer to be allowed")
	}
}

func TestRefreshPeerIDMismatchRejectsUntrustedPublicPeer(t *testing.T) {
	oldPersist := PersistPeerIDRefresh
	PersistPeerIDRefresh = false
	t.Cleanup(func() {
		PersistPeerIDRefresh = oldPersist
	})

	raw := "/ip4/203.0.113.10/tcp/7001/p2p/12D3KooWOLD"
	n := &Node{
		Config:         &NodeConfig{},
		allowedPeerIDs: map[string]bool{},
	}
	err := errors.New("peer ID mismatch: expected 12D3KooWOLD but got 12D3KooWSEQhoUiBNwfWgrv7ygWBTCGWRrL7K8z8ANcu6NXvk4ZA")

	if n.refreshPeerIDMismatch(raw, "12D3KooWOLD", err) {
		t.Fatalf("untrusted public peer-id mismatch should not refresh")
	}
}

func TestPubsubValidateWorkersFollowGOMAXPROCS(t *testing.T) {
	oldMaxProcs := runtime.GOMAXPROCS(2)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(oldMaxProcs)
	})
	if got := pubsubValidateWorkerCount(); got != 2 {
		t.Fatalf("pubsub workers with GOMAXPROCS=2 = %d, want 2", got)
	}
	runtime.GOMAXPROCS(1)
	if got := pubsubValidateWorkerCount(); got != 1 {
		t.Fatalf("pubsub workers with GOMAXPROCS=1 = %d, want 1", got)
	}
}
