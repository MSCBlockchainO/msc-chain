package main

import (
	"fmt"
	"strings"
	"time"
)

const tipHashConvergenceLogCooldown = 10 * time.Second

type forkAncestorSample struct {
	height uint64
	hash   string
	peer   string
}

func forkAncestorRollbackWindow() uint64 {
	if StorageValidatorRollbackWindowBlocks == 0 {
		return 256
	}
	return StorageValidatorRollbackWindowBlocks
}

func chooseForkAncestorRewind(localHeight uint64, samples []forkAncestorSample, requiredVotes int, rollbackWindow uint64) (uint64, string, int, int, bool) {
	if localHeight <= 1 || len(samples) == 0 {
		return 0, "", 0, 0, false
	}
	if requiredVotes <= 0 {
		requiredVotes = 1
	}
	if rollbackWindow == 0 {
		rollbackWindow = 1
	}

	type voteInfo struct {
		height uint64
		hash   string
		votes  int
	}
	counts := make(map[string]*voteInfo)
	total := 0
	for _, sample := range samples {
		hash := strings.TrimSpace(sample.hash)
		if sample.height == 0 || sample.height >= localHeight || hash == "" {
			continue
		}
		if localHeight-sample.height > rollbackWindow {
			continue
		}
		key := fmt.Sprintf("%d:%s", sample.height, hash)
		info := counts[key]
		if info == nil {
			info = &voteInfo{height: sample.height, hash: hash}
			counts[key] = info
		}
		info.votes++
		total++
	}
	if total == 0 {
		return 0, "", 0, 0, false
	}

	var best *voteInfo
	for _, info := range counts {
		if best == nil ||
			info.votes > best.votes ||
			(info.votes == best.votes && info.height > best.height) ||
			(info.votes == best.votes && info.height == best.height && info.hash < best.hash) {
			best = info
		}
	}
	if best == nil || best.votes < requiredVotes {
		return 0, "", 0, total, false
	}
	return best.height, best.hash, best.votes, total, true
}

func (n *Node) maybeConvergeTipFromPeerHello(peerAddr string, hello PeerHello) {
	if n == nil || n.Blockchain == nil || hello.Height == 0 {
		return
	}
	peerTip := strings.TrimSpace(hello.TipHash)
	if peerTip == "" {
		return
	}
	localHeight := n.Blockchain.Height()
	if hello.Height != localHeight || localHeight <= 1 {
		return
	}
	localTip := strings.TrimSpace(n.Blockchain.LastBlock().BlockHash)
	if localTip == "" || strings.EqualFold(localTip, peerTip) {
		return
	}

	type voteInfo struct {
		votes int
		peers []string
	}
	counts := make(map[string]*voteInfo)
	totalSamples := 1
	counts[localTip] = &voteInfo{votes: 1, peers: []string{"self"}}

	n.peerStateMu.Lock()
	for pid, height := range n.peerAckHeight {
		if height != localHeight || !n.peerHelloOK[pid] {
			continue
		}
		hash := strings.TrimSpace(n.peerTipHash[pid])
		if hash == "" {
			continue
		}
		info := counts[hash]
		if info == nil {
			info = &voteInfo{}
			counts[hash] = info
		}
		info.votes++
		info.peers = append(info.peers, pid)
		totalSamples++
	}
	n.peerStateMu.Unlock()

	bestHash := ""
	bestVotes := 0
	bestPeers := []string(nil)
	localVotes := counts[localTip].votes
	for hash, info := range counts {
		if strings.EqualFold(hash, localTip) {
			continue
		}
		if info.votes > bestVotes || (info.votes == bestVotes && hash < bestHash) {
			bestHash = hash
			bestVotes = info.votes
			bestPeers = info.peers
		}
	}
	if bestHash == "" || bestVotes <= localVotes || bestVotes*2 <= totalSamples {
		return
	}

	key := fmt.Sprintf("tip_hash_convergence:%d:%s", localHeight, bestHash)
	if !n.shouldLogLivenessReason(key, tipHashConvergenceLogCooldown) {
		return
	}
	fmt.Printf("[FORK-CONVERGE] local=%d local_tip=%s local_votes=%d best_tip=%s best_votes=%d samples=%d trigger_peer=%s peers=%d action=rewind_and_sync\n",
		localHeight,
		ShortHash(localTip),
		localVotes,
		ShortHash(bestHash),
		bestVotes,
		totalSamples,
		ShortID(peerAddr),
		len(bestPeers),
	)

	if n.rewindLocalChainToHeight(localHeight-1, "tip_hash_majority") {
		n.maybeSyncToBestObservedHeight("tip_hash_majority_rewind")
		n.maybeRecoverMissingBlock(localHeight, "tip_hash_majority_rewind")
	}
}

func (n *Node) maybeRewindForkedTipFromAheadPeers(targetHeight uint64, reason string) bool {
	if n == nil || n.Host == nil || n.Blockchain == nil {
		return false
	}
	localHeight := n.Blockchain.Height()
	if localHeight <= 1 || targetHeight <= localHeight {
		return false
	}
	peers := n.pickSyncPeers(targetHeight, nil, 4)
	if len(peers) == 0 {
		return false
	}

	samples := make([]forkAncestorSample, 0, len(peers))
	for _, pid := range peers {
		commonHeight, commonHash, err := n.findCommonAncestorWithPeer(pid, localHeight)
		if err != nil {
			continue
		}
		samples = append(samples, forkAncestorSample{
			height: commonHeight,
			hash:   commonHash,
			peer:   pid.String(),
		})
	}

	requiredVotes := 2
	if len(peers) == 1 {
		requiredVotes = 1
	}
	commonHeight, commonHash, votes, total, ok := chooseForkAncestorRewind(localHeight, samples, requiredVotes, forkAncestorRollbackWindow())
	if !ok {
		return false
	}

	key := fmt.Sprintf("fork_ancestor_rewind:%d:%d:%s", localHeight, commonHeight, commonHash)
	if !n.shouldLogLivenessReason(key, tipHashConvergenceLogCooldown) {
		return false
	}
	fmt.Printf("[FORK-CONVERGE] local=%d target=%d common_height=%d common_hash=%s votes=%d samples=%d reason=%s action=rewind_and_sync\n",
		localHeight,
		targetHeight,
		commonHeight,
		ShortHash(commonHash),
		votes,
		total,
		strings.TrimSpace(reason),
	)
	if !n.rewindLocalChainToHeight(commonHeight, "fork_ancestor_"+strings.TrimSpace(reason)) {
		return false
	}
	n.maybeSyncToBestObservedHeight("fork_ancestor_rewind")
	n.maybeRecoverMissingBlock(commonHeight+1, "fork_ancestor_rewind")
	return true
}

func (n *Node) maybeConvergeTipFromLeaderPrev(sourcePeer string, block Block, reason string) {
	if n == nil || n.Blockchain == nil || block.ID == 0 {
		return
	}
	prevHash := strings.TrimSpace(block.PrevHash)
	if prevHash == "" {
		return
	}
	localHeight := n.Blockchain.Height()
	if block.ID != localHeight+1 || localHeight <= 1 {
		return
	}
	localTip := strings.TrimSpace(n.Blockchain.LastBlock().BlockHash)
	if localTip == "" || strings.EqualFold(localTip, prevHash) {
		return
	}

	peerKey := strings.TrimSpace(sourcePeer)
	if peerKey == "" {
		proposer := normalizeValidatorID(block.Proposer)
		if proposer != "" {
			n.peerStateMu.Lock()
			for existingPeer, validatorID := range n.peerToValidator {
				if normalizeValidatorID(validatorID) != proposer {
					continue
				}
				if n.peerAckHeight[existingPeer] == localHeight && strings.EqualFold(strings.TrimSpace(n.peerTipHash[existingPeer]), prevHash) {
					n.peerStateMu.Unlock()
					n.maybeConvergeTipFromPeerHello(existingPeer, PeerHello{
						Height:  localHeight,
						TipHash: prevHash,
					})
					return
				}
			}
			n.peerStateMu.Unlock()
		}
		peerKey = fmt.Sprintf("proposal:%s:%d:%d:%s", proposer, block.ID, block.Round, strings.TrimSpace(block.BlockHash))
	}
	n.peerStateMu.Lock()
	if n.peerTipHash == nil {
		n.peerTipHash = make(map[string]string)
	}
	if n.peerAckHeight == nil {
		n.peerAckHeight = make(map[string]uint64)
	}
	if n.peerHelloOK == nil {
		n.peerHelloOK = make(map[string]bool)
	}
	n.peerTipHash[peerKey] = prevHash
	n.peerAckHeight[peerKey] = localHeight
	n.peerHelloOK[peerKey] = true
	n.peerStateMu.Unlock()

	if reason = strings.TrimSpace(reason); reason == "" {
		reason = "leader_prev_mismatch"
	}
	key := fmt.Sprintf("tip_hash_observed_prev:%d:%s:%s", localHeight, prevHash, reason)
	if n.shouldLogLivenessReason(key, tipHashConvergenceLogCooldown) {
		fmt.Printf("[FORK-CONVERGE] local=%d local_tip=%s observed_tip=%s source=%s proposer=%s block=%s reason=%s action=sample\n",
			localHeight,
			ShortHash(localTip),
			ShortHash(prevHash),
			ShortID(sourcePeer),
			ShortID(block.Proposer),
			ShortHash(block.BlockHash),
			reason,
		)
	}
	n.maybeConvergeTipFromPeerHello(peerKey, PeerHello{
		Height:  localHeight,
		TipHash: prevHash,
	})
}
