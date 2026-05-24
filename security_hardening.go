package main

import (
	"strings"
	"time"
)

const (
	peerSecurityFaultQuarantineAfter uint64 = 3
	peerRateLimitDropQuarantineAfter uint64 = 3
)

func (n *Node) ensurePeerIsolationMaps() {
	if n == nil {
		return
	}
	n.peerStateMu.Lock()
	if n.quarantineUntil == nil {
		n.quarantineUntil = make(map[string]time.Time)
	}
	if n.peerDialFailures == nil {
		n.peerDialFailures = make(map[string]int)
	}
	if n.peerDialNext == nil {
		n.peerDialNext = make(map[string]time.Time)
	}
	if n.peerSubnet == nil {
		n.peerSubnet = make(map[string]string)
	}
	if n.peerASN == nil {
		n.peerASN = make(map[string]string)
	}
	if n.peerOutbound == nil {
		n.peerOutbound = make(map[string]bool)
	}
	if n.peerHelloNonces == nil {
		n.peerHelloNonces = make(map[string]time.Time)
	}
	if n.peerResourceWindows == nil {
		n.peerResourceWindows = make(map[string]PeerResourceWindow)
	}
	if n.peerConnectWindows == nil {
		n.peerConnectWindows = make(map[string]PeerResourceWindow)
	}
	if n.peerHelloOK == nil {
		n.peerHelloOK = make(map[string]bool)
	}
	if n.peerSuspectAt == nil {
		n.peerSuspectAt = make(map[string]time.Time)
	}
	if n.peerHashMatch == nil {
		n.peerHashMatch = make(map[string]bool)
	}
	if n.peerToValidator == nil {
		n.peerToValidator = make(map[string]string)
	}
	if n.validatorToPeer == nil {
		n.validatorToPeer = make(map[string]string)
	}
	if n.peerRole == nil {
		n.peerRole = make(map[string]string)
	}
	if n.peerSetHash == nil {
		n.peerSetHash = make(map[string]string)
	}
	if n.peerTipHash == nil {
		n.peerTipHash = make(map[string]string)
	}
	if n.peerAckHeight == nil {
		n.peerAckHeight = make(map[string]uint64)
	}
	if n.peerSyncOnlyUntil == nil {
		n.peerSyncOnlyUntil = make(map[string]time.Time)
	}
	if n.peerSyncOnlyClass == nil {
		n.peerSyncOnlyClass = make(map[string]string)
	}
	if n.peerFlapTimes == nil {
		n.peerFlapTimes = make(map[string][]time.Time)
	}
	if n.connectedPeers == nil {
		n.connectedPeers = make(map[string]bool)
	}
	if n.connectingPeers == nil {
		n.connectingPeers = make(map[string]bool)
	}
	if n.peerConnectedAt == nil {
		n.peerConnectedAt = make(map[string]time.Time)
	}
	if n.allowedPeerIDs == nil {
		n.allowedPeerIDs = make(map[string]bool)
	}
	if n.peerDriftState == nil {
		n.peerDriftState = make(map[string]PeerDriftState)
	}
	if n.peerSyncOnlyLastDropLog == nil {
		n.peerSyncOnlyLastDropLog = make(map[string]time.Time)
	}
	n.peerStateMu.Unlock()
}

func (n *Node) recordPeerSecurityFault(peerID string, reason string) uint64 {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return 0
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "security_fault"
	}

	n.syncPeerScoreMu.Lock()
	if n.syncPeerScores == nil {
		n.syncPeerScores = make(map[string]*SyncPeerScore)
	}
	score := n.syncPeerScores[peerID]
	if score == nil {
		score = &SyncPeerScore{}
		n.syncPeerScores[peerID] = score
	}
	score.SecurityFaultCount++
	score.InvalidProofCount++
	score.BlockBatchFail++
	score.UpdatedAt = time.Now()
	count := score.SecurityFaultCount
	n.syncPeerScoreMu.Unlock()
	n.savePeerReputation()

	if count >= peerSecurityFaultQuarantineAfter {
		n.ensurePeerIsolationMaps()
		n.disconnectPeerID(peerID, "security_"+reason)
	}
	return count
}

func (n *Node) recordPeerRateLimitDrop(peerID string, msgType string) uint64 {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return 0
	}
	msgType = strings.TrimSpace(msgType)
	if msgType == "" {
		msgType = "unknown"
	}

	n.syncPeerScoreMu.Lock()
	if n.syncPeerScores == nil {
		n.syncPeerScores = make(map[string]*SyncPeerScore)
	}
	score := n.syncPeerScores[peerID]
	if score == nil {
		score = &SyncPeerScore{}
		n.syncPeerScores[peerID] = score
	}
	score.RateLimitDropCount++
	score.BlockBatchFail++
	score.UpdatedAt = time.Now()
	count := score.RateLimitDropCount
	n.syncPeerScoreMu.Unlock()
	n.savePeerReputation()

	if count >= peerRateLimitDropQuarantineAfter {
		n.ensurePeerIsolationMaps()
		n.disconnectPeerID(peerID, "rate_limit_"+msgType)
	}
	return count
}
