package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	ma "github.com/multiformats/go-multiaddr"
)

func peerResourceWindowDuration() time.Duration {
	if PeerResourceWindowDuration <= 0 {
		return time.Minute
	}
	return PeerResourceWindowDuration
}

func resetPeerResourceWindowIfNeeded(w PeerResourceWindow, now time.Time) PeerResourceWindow {
	if w.StartedAt.IsZero() || now.Sub(w.StartedAt) >= peerResourceWindowDuration() {
		return PeerResourceWindow{StartedAt: now}
	}
	return w
}

func (n *Node) allowPeerResource(peerID string, msgType string, payloadBytes int) bool {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return true
	}
	if payloadBytes < 0 {
		payloadBytes = 0
	}
	if PeerMemoryQuotaBytes > 0 && payloadBytes > PeerMemoryQuotaBytes {
		n.observePeerResourceDrop("payload_too_large")
		n.recordPeerRateLimitDrop(peerID, "payload_too_large")
		return false
	}

	n.ensurePeerIsolationMaps()
	now := time.Now()
	n.peerStateMu.Lock()
	w := resetPeerResourceWindowIfNeeded(n.peerResourceWindows[peerID], now)
	w.Messages++
	w.Bytes += uint64(payloadBytes)
	switch msgType {
	case MsgTx:
		w.TxMessages++
	case MsgGetBlocks:
		w.BlockRequests++
	}
	allowed := true
	reason := ""
	if PeerBandwidthQuotaBytesPerMinute > 0 && w.Bytes > uint64(PeerBandwidthQuotaBytesPerMinute) {
		allowed = false
		reason = "bandwidth_quota"
	}
	if allowed && PeerMempoolTxPerMinute > 0 && w.TxMessages > uint64(PeerMempoolTxPerMinute) {
		allowed = false
		reason = "mempool_tx_quota"
	}
	if allowed && PeerBlockRequestsPerMinute > 0 && w.BlockRequests > uint64(PeerBlockRequestsPerMinute) {
		allowed = false
		reason = "block_request_quota"
	}
	n.peerResourceWindows[peerID] = w
	n.peerStateMu.Unlock()
	if !allowed {
		n.observePeerResourceDrop(reason)
		n.recordPeerRateLimitDrop(peerID, reason)
		return false
	}
	return true
}

func peerConnectionFloodKey(conn network.Conn) string {
	if conn == nil {
		return ""
	}
	if key := peerSubnetKeyFromMultiaddr(conn.RemoteMultiaddr()); key != "" {
		return "subnet:" + key
	}
	if pid := strings.TrimSpace(conn.RemotePeer().String()); pid != "" {
		return "peer:" + pid
	}
	return ""
}

func (n *Node) allowPeerConnectionFloodKey(key string) bool {
	key = strings.TrimSpace(key)
	if n == nil || key == "" || PeerConnectionFloodMaxPerWindow <= 0 {
		return true
	}
	n.ensurePeerIsolationMaps()
	now := time.Now()
	n.peerStateMu.Lock()
	w := resetPeerResourceWindowIfNeeded(n.peerConnectWindows[key], now)
	w.Connections++
	allowed := w.Connections <= uint64(PeerConnectionFloodMaxPerWindow)
	n.peerConnectWindows[key] = w
	n.peerStateMu.Unlock()
	if !allowed {
		n.observePeerConnectionFlood("connection_flood")
		return false
	}
	return true
}

func (n *Node) allowPeerConnectionFlood(conn network.Conn) bool {
	return n.allowPeerConnectionFloodKey(peerConnectionFloodKey(conn))
}

func isRoutableDiscoveryIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.IsLoopback() {
		return !BlockPublicPeers
	}
	return true
}

func validDiscoveredAddr(addr ma.Multiaddr) bool {
	if addr == nil {
		return false
	}
	if ip4, err := addr.ValueForProtocol(ma.P_IP4); err == nil && ip4 != "" {
		return isRoutableDiscoveryIP(net.ParseIP(ip4))
	}
	if ip6, err := addr.ValueForProtocol(ma.P_IP6); err == nil && ip6 != "" {
		return isRoutableDiscoveryIP(net.ParseIP(ip6))
	}
	if host, err := addr.ValueForProtocol(ma.P_DNS); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	if host, err := addr.ValueForProtocol(ma.P_DNS4); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	if host, err := addr.ValueForProtocol(ma.P_DNS6); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	return false
}

func sanitizeDiscoveredAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	if len(addrs) == 0 {
		return nil
	}
	limit := PeerDiscoveryMaxAddrs
	if limit <= 0 {
		limit = 16
	}
	out := make([]ma.Multiaddr, 0, minInt(len(addrs), limit))
	seen := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		if !validDiscoveredAddr(addr) {
			continue
		}
		key := addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, addr)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func peerResourceLimitString() string {
	return fmt.Sprintf("memory=%d bandwidth_per_min=%d tx_per_min=%d block_req_per_min=%d conn_window=%d",
		PeerMemoryQuotaBytes,
		PeerBandwidthQuotaBytesPerMinute,
		PeerMempoolTxPerMinute,
		PeerBlockRequestsPerMinute,
		PeerConnectionFloodMaxPerWindow,
	)
}
