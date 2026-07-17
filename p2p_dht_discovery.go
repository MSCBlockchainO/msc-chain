package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
)

// tryConnectDHTPeer attempts one discovered peer without bypassing diversity,
// reputation, connection, or rate limits.
func (n *Node) tryConnectDHTPeer(ctx context.Context, addrInfo peer.AddrInfo) {
	if n == nil || n.Host == nil || addrInfo.ID == "" || addrInfo.ID == n.Host.ID() {
		return
	}
	peerID := addrInfo.ID.String()
	if !n.canDialPeerID(peerID) || len(n.Host.Network().ConnsToPeer(addrInfo.ID)) > 0 {
		return
	}
	addrs := addrInfo.Addrs
	if len(addrs) == 0 {
		addrs = n.Host.Peerstore().Addrs(addrInfo.ID)
	}
	addrs = sanitizeDiscoveredAddrs(addrs)
	if len(addrs) == 0 {
		n.recordDialFailure(peerID)
		return
	}
	if BlockPublicPeers {
		addrs = filterPrivateAddrs(addrs)
		if len(addrs) == 0 {
			return
		}
	}
	addrInfo.Addrs = addrs
	if !n.allowDiscoveredPeer(addrInfo) || !connectionLimiter.Allow() || !n.canDialPeer() {
		return
	}
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := n.Host.Connect(connectCtx, addrInfo); err == nil {
		if DebugNet {
			fmt.Printf("DHT discovered and connected to peer: %s\n", addrInfo.ID)
		}
		if cm := n.Host.ConnManager(); cm != nil {
			cm.TagPeer(addrInfo.ID, "dht-discovered", 50)
		}
		n.peerMu.Lock()
		n.PeersLibp2p = append(n.PeersLibp2p, addrInfo.ID)
		n.peerMu.Unlock()
		n.rememberPeerDiversityAddr(peerID, addrInfo.Addrs, true)
		n.recordDialSuccess(peerID)
		return
	} else {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "peer id mismatch") || strings.Contains(errLower, "dial to self attempted") {
			rawAddr := ""
			if len(addrs) > 0 {
				rawAddr = fmt.Sprintf("%s/p2p/%s", addrs[0].String(), peerID)
			}
			if n.refreshPeerIDMismatch(rawAddr, peerID, err) {
				return
			}
			n.forgetPeer(peerID, "peer_id_mismatch")
		}
		n.recordDialFailure(peerID)
	}
}

// discoverDHTProvidersOnce advertises this chain and scans enough providers to
// fill the sparse target even when some candidates fail policy checks.
func (n *Node) discoverDHTProvidersOnce(ctx context.Context, dhtInst *dht.IpfsDHT) {
	if n == nil || n.Host == nil || dhtInst == nil || DisableDHT {
		return
	}
	networkKey := fmt.Sprintf("msc-chain-network-%s", protocolChainID())
	mh, err := multihash.Sum([]byte(networkKey), multihash.SHA2_256, -1)
	if err != nil {
		return
	}
	chainCID := cid.NewCidV1(cid.Raw, mh)
	providersCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	_ = dhtInst.Provide(providersCtx, chainCID, true)
	providers := dhtInst.FindProvidersAsync(providersCtx, chainCID, MaxPeers)
	for provider := range providers {
		n.tryConnectDHTPeer(providersCtx, provider)
		if len(n.Host.Network().Peers()) >= TargetPeers {
			return
		}
	}
}

// triggerDHTPeerDiscovery coalesces discovery scans requested by self-heal.
func (n *Node) triggerDHTPeerDiscovery(ctx context.Context, dhtInst *dht.IpfsDHT) {
	if n == nil || dhtInst == nil || DisableDHT || !n.dhtDiscoveryRunning.CompareAndSwap(false, true) {
		return
	}
	n.SafeGo("dht_provider_discovery", func() {
		defer n.dhtDiscoveryRunning.Store(false)
		n.discoverDHTProvidersOnce(ctx, dhtInst)
	})
}
