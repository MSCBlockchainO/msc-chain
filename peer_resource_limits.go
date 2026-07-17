package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	ma "github.com/multiformats/go-multiaddr"
)

// peerResourceWindowDuration implements the peer resource window duration helper.
func peerResourceWindowDuration() time.Duration {
	if PeerResourceWindowDuration <= 0 {
		return time.Minute
	}
	return PeerResourceWindowDuration
}

// resetPeerResourceWindowIfNeeded implements the reset peer resource window if needed helper.
func resetPeerResourceWindowIfNeeded(w PeerResourceWindow, now time.Time) PeerResourceWindow {
	if w.StartedAt.IsZero() || now.Sub(w.StartedAt) >= peerResourceWindowDuration() {
		return PeerResourceWindow{StartedAt: now}
	}
	return w
}

// allowPeerResource implements the allow peer resource helper.
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
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.peerStateMu.Lock()
	// `w` stores the value produced by this operation.
	w := resetPeerResourceWindowIfNeeded(n.peerResourceWindows[peerID], now)
	w.Messages++
	w.Bytes += uint64(payloadBytes)
	switch msgType {
	case MsgTx:
		w.TxMessages++
	case MsgGetBlocks:
		w.BlockRequests++
	}
	// `allowed` stores whether the related condition is satisfied.
	allowed := true
	// `reason` stores the value produced by this operation.
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

// peerConnectionFloodKey implements the peer connection flood key helper.
func peerConnectionFloodKey(conn network.Conn) string {
	if conn == nil {
		return ""
	}
	// `key` stores the key used to access the related value.
	if key := peerSubnetKeyFromMultiaddr(conn.RemoteMultiaddr()); key != "" {
		return "subnet:" + key
	}
	// `pid` stores the value produced by this operation.
	if pid := strings.TrimSpace(conn.RemotePeer().String()); pid != "" {
		return "peer:" + pid
	}
	return ""
}

// allowPeerConnectionFloodForPeer implements the allow peer connection flood for peer helper.
func (n *Node) allowPeerConnectionFloodForPeer(peerID string, key string) bool {
	peerID = strings.TrimSpace(peerID)
	key = strings.TrimSpace(key)
	if n == nil || key == "" || PeerConnectionFloodMaxPerWindow <= 0 {
		return true
	}
	if peerID != "" && n.isValidatorOrPersistentPeerID(peerID) {
		return true
	}
	n.ensurePeerIsolationMaps()
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.peerStateMu.Lock()
	// `w` stores the value produced by this operation.
	w := resetPeerResourceWindowIfNeeded(n.peerConnectWindows[key], now)
	w.Connections++
	// `allowed` stores whether the related condition is satisfied.
	allowed := w.Connections <= uint64(PeerConnectionFloodMaxPerWindow)
	n.peerConnectWindows[key] = w
	n.peerStateMu.Unlock()
	if !allowed {
		n.observePeerConnectionFlood("connection_flood")
		return false
	}
	return true
}

// allowPeerConnectionFloodKey implements the allow peer connection flood key helper.
func (n *Node) allowPeerConnectionFloodKey(key string) bool {
	// `peerID` stores the value produced by this operation.
	peerID := ""
	if strings.HasPrefix(strings.TrimSpace(key), "peer:") {
		peerID = strings.TrimPrefix(strings.TrimSpace(key), "peer:")
	}
	return n.allowPeerConnectionFloodForPeer(peerID, key)
}

// allowPeerConnectionFlood implements the allow peer connection flood helper.
func (n *Node) allowPeerConnectionFlood(conn network.Conn) bool {
	// `peerID` stores the value produced by this operation.
	peerID := ""
	if conn != nil {
		peerID = strings.TrimSpace(conn.RemotePeer().String())
	}
	return n.allowPeerConnectionFloodForPeer(peerID, peerConnectionFloodKey(conn))
}

// isRoutableDiscoveryIP implements the is routable discovery ip helper.
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

// validDiscoveredAddr implements the valid discovered addr helper.
func validDiscoveredAddr(addr ma.Multiaddr) bool {
	if addr == nil {
		return false
	}
	// `ip4` and `err` store the error produced by this operation.
	if ip4, err := addr.ValueForProtocol(ma.P_IP4); err == nil && ip4 != "" {
		return isRoutableDiscoveryIP(net.ParseIP(ip4))
	}
	// `ip6` and `err` store the error produced by this operation.
	if ip6, err := addr.ValueForProtocol(ma.P_IP6); err == nil && ip6 != "" {
		return isRoutableDiscoveryIP(net.ParseIP(ip6))
	}
	// `host` and `err` store the error produced by this operation.
	if host, err := addr.ValueForProtocol(ma.P_DNS); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	// `host` and `err` store the error produced by this operation.
	if host, err := addr.ValueForProtocol(ma.P_DNS4); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	// `host` and `err` store the error produced by this operation.
	if host, err := addr.ValueForProtocol(ma.P_DNS6); err == nil && strings.TrimSpace(host) != "" {
		return true
	}
	return false
}

// sanitizeDiscoveredAddrs implements the sanitize discovered addrs helper.
func sanitizeDiscoveredAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	if len(addrs) == 0 {
		return nil
	}
	// `limit` stores the value produced by this operation.
	limit := PeerDiscoveryMaxAddrs
	if limit <= 0 {
		limit = 16
	}
	// `out` stores the result produced by this operation.
	out := make([]ma.Multiaddr, 0, minInt(len(addrs), limit))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(addrs))
	// `addr` tracks the address used by this operation.
	for _, addr := range addrs {
		if !validDiscoveredAddr(addr) {
			continue
		}
		// `key` stores the key used to access the related value.
		key := addr.String()
		// `ok` stores whether the related condition is satisfied.
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

// minInt returns the minimum int.
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// peerResourceLimitString implements the peer resource limit string helper.
func peerResourceLimitString() string {
	return fmt.Sprintf("memory=%d bandwidth_per_min=%d tx_per_min=%d block_req_per_min=%d conn_window=%d",
		PeerMemoryQuotaBytes,
		PeerBandwidthQuotaBytesPerMinute,
		PeerMempoolTxPerMinute,
		PeerBlockRequestsPerMinute,
		PeerConnectionFloodMaxPerWindow,
	)
}
