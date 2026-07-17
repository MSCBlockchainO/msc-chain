package main

import (
	"fmt"
	"strings"
	"time"
)

// `tipHashConvergenceLogCooldown` defines the constant value used by this package.
const tipHashConvergenceLogCooldown = 10 * time.Second

// maybeConvergeTipFromPeerHello implements the maybe converge tip from peer hello helper.
func (n *Node) maybeConvergeTipFromPeerHello(peerAddr string, hello PeerHello) {
	if n == nil || n.Blockchain == nil || hello.Height == 0 {
		return
	}
	// `peerTip` stores the value produced by this operation.
	peerTip := strings.TrimSpace(hello.TipHash)
	if peerTip == "" {
		return
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := n.Blockchain.Height()
	if hello.Height != localHeight || localHeight <= 1 {
		return
	}
	// `localTip` stores the value produced by this operation.
	localTip := strings.TrimSpace(n.Blockchain.LastBlock().BlockHash)
	if localTip == "" || strings.EqualFold(localTip, peerTip) {
		return
	}

	type voteInfo struct {
		// `votes` stores the value associated with this record.
		votes int
		// `peers` stores the value associated with this record.
		peers []string
	}
	// `counts` stores the measured quantity used by this operation.
	counts := make(map[string]*voteInfo)
	// `totalSamples` stores the measured quantity used by this operation.
	totalSamples := 1
	counts[localTip] = &voteInfo{votes: 1, peers: []string{"self"}}

	n.peerStateMu.Lock()
	// `pid` and `height` track the current values while iterating.
	for pid, height := range n.peerAckHeight {
		if height != localHeight || !n.peerHelloOK[pid] {
			continue
		}
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.TrimSpace(n.peerTipHash[pid])
		if hash == "" {
			continue
		}
		// `info` stores the current position in the related collection.
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

	// `bestHash` stores the digest used to identify or verify the related data.
	bestHash := ""
	// `bestVotes` stores the value produced by this operation.
	bestVotes := 0
	// `bestPeers` stores the value produced by this operation.
	bestPeers := []string(nil)
	// `localVotes` stores the value produced by this operation.
	localVotes := counts[localTip].votes
	// `hash` and `info` track the digest used to identify or verify the related data.
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

	// `key` stores the key used to access the related value.
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

// maybeConvergeTipFromLeaderPrev implements the maybe converge tip from leader prev helper.
func (n *Node) maybeConvergeTipFromLeaderPrev(sourcePeer string, block Block, reason string) {
	if n == nil || n.Blockchain == nil || block.ID == 0 {
		return
	}
	// `prevHash` stores the digest used to identify or verify the related data.
	prevHash := strings.TrimSpace(block.PrevHash)
	if prevHash == "" {
		return
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := n.Blockchain.Height()
	if block.ID != localHeight+1 || localHeight <= 1 {
		return
	}
	// `localTip` stores the value produced by this operation.
	localTip := strings.TrimSpace(n.Blockchain.LastBlock().BlockHash)
	if localTip == "" || strings.EqualFold(localTip, prevHash) {
		return
	}

	// `peerKey` stores the key used to access the related value.
	peerKey := strings.TrimSpace(sourcePeer)
	if peerKey == "" {
		// `proposer` stores the value produced by this operation.
		proposer := normalizeValidatorID(block.Proposer)
		if proposer != "" {
			n.peerStateMu.Lock()
			// `existingPeer` and `validatorID` track whether the related condition is satisfied.
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
	// `key` stores the key used to access the related value.
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
