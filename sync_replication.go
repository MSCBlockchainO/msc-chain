package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// syncSnapshotChunkReplicationFactor implements the sync snapshot chunk replication factor helper.
func syncSnapshotChunkReplicationFactor() int {
	if SyncSnapshotChunkReplicationFactor <= 0 {
		return 2
	}
	if SyncSnapshotChunkReplicationFactor > 3 {
		return 3
	}
	return SyncSnapshotChunkReplicationFactor
}

// syncBlockRangeReplicationFactor implements the sync block range replication factor helper.
func syncBlockRangeReplicationFactor() int {
	if SyncBlockRangeReplicationFactor <= 0 {
		return 2
	}
	if SyncBlockRangeReplicationFactor > 3 {
		return 3
	}
	return SyncBlockRangeReplicationFactor
}

// snapshotChunkReplicaProviders implements the snapshot chunk replica providers helper.
func snapshotChunkReplicaProviders(providers []peer.ID, idx uint64, batchStart int, replicationFactor int) []peer.ID {
	if len(providers) == 0 || replicationFactor <= 0 {
		return nil
	}
	if replicationFactor > len(providers) {
		replicationFactor = len(providers)
	}
	// `out` stores the result produced by this operation.
	out := make([]peer.ID, 0, replicationFactor)
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, replicationFactor)
	// `offset` stores the value produced by this operation.
	for offset := 0; offset < len(providers) && len(out) < replicationFactor; offset++ {
		// `pos` stores the value produced by this operation.
		pos := (int(idx) + batchStart + offset) % len(providers)
		// `pid` stores the value produced by this operation.
		pid := providers[pos]
		// `key` stores the key used to access the related value.
		key := strings.TrimSpace(pid.String())
		if key == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pid)
	}
	return out
}

type snapshotChunkReplicaResult struct {
	// `pid` stores the value associated with this record.
	pid peer.ID
	// `resp` stores the response produced by this operation.
	resp *SnapshotChunkResponse
	// `err` stores the error produced by this operation.
	err error
	// `elapsed` stores the value associated with this record.
	elapsed time.Duration
	// `timeout` stores the result produced by this operation.
	timeout bool
}

// fetchReplicatedSnapshotChunk implements the fetch replicated snapshot chunk helper.
func (n *Node) fetchReplicatedSnapshotChunk(manifest *SnapshotManifest, providers []peer.ID, idx uint64) ([]byte, peer.ID, error) {
	if n == nil || manifest == nil {
		return nil, "", fmt.Errorf("snapshot manifest unavailable")
	}
	if idx >= uint64(len(manifest.ChunkHashes)) {
		return nil, "", fmt.Errorf("snapshot chunk hash unavailable index=%d", idx)
	}
	// `replicationFactor` stores the value produced by this operation.
	replicationFactor := syncSnapshotChunkReplicationFactor()
	if !syncSnapshotMultiPeerChunkFetchEnabled() {
		replicationFactor = 1
	}
	if replicationFactor > len(providers) {
		replicationFactor = len(providers)
	}
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	// `expectedHash` stores the digest used to identify or verify the related data.
	expectedHash := strings.TrimSpace(manifest.ChunkHashes[idx])
	// `batchStart` stores the value produced by this operation.
	for batchStart := 0; batchStart < len(providers); batchStart += replicationFactor {
		// `replicas` stores the value produced by this operation.
		replicas := snapshotChunkReplicaProviders(providers, idx, batchStart, replicationFactor)
		if len(replicas) == 0 {
			continue
		}
		// `resultsCh` stores the result produced by this operation.
		resultsCh := make(chan snapshotChunkReplicaResult, len(replicas))
		// `pid` tracks the current values while iterating.
		for _, pid := range replicas {
			// `pid` stores the value produced by this operation.
			pid := pid
			go func() {
				// `started` stores the value produced by this operation.
				started := time.Now()
				// `resp` and `err` store the error produced by this operation.
				resp, err := n.requestSnapshotChunkFromPeer(pid, manifest.Height, idx)
				resultsCh <- snapshotChunkReplicaResult{
					pid:     pid,
					resp:    resp,
					err:     err,
					elapsed: time.Since(started),
					timeout: isSyncTimeoutError(err),
				}
			}()
		}

		var (
			// `winner` stores the value used by this operation.
			winner peer.ID
			// `data` stores the value used by this operation.
			data []byte
		)
		for range replicas {
			// `result` stores the result produced by this operation.
			result := <-resultsCh
			if result.err != nil {
				n.recordSyncPeerSnapshotResult(result.pid.String(), false, result.elapsed, 0, result.timeout)
				continue
			}
			if result.resp == nil || result.resp.Height != manifest.Height || result.resp.Index != idx {
				n.recordSyncPeerSnapshotResult(result.pid.String(), false, result.elapsed, 0, false)
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(result.resp.SnapshotHash), strings.TrimSpace(manifest.SnapshotHash)) {
				n.recordSyncPeerInvalidProof(result.pid.String())
				continue
			}
			if !snapshotManifestChunkDataValid(manifest, idx, result.resp.Data) {
				n.recordSyncPeerInvalidProof(result.pid.String())
				continue
			}
			// `actualHash` stores the digest used to identify or verify the related data.
			actualHash := snapshotChunkHash(result.resp.Data)
			if !strings.EqualFold(strings.TrimSpace(result.resp.ChunkHash), expectedHash) || !strings.EqualFold(actualHash, expectedHash) {
				n.recordSyncPeerInvalidProof(result.pid.String())
				if DebugSync || DebugConsensus {
					fmt.Printf("[SNAPSHOT-CHUNK] reject height=%d idx=%d provider=%s expected=%s got=%s\n",
						manifest.Height, idx, result.pid.String(), ShortHash(expectedHash), ShortHash(actualHash))
				}
				continue
			}
			if data == nil {
				data = append([]byte(nil), result.resp.Data...)
				winner = result.pid
				n.recordSyncPeerSnapshotResult(result.pid.String(), true, result.elapsed, len(result.resp.Data), false)
				continue
			}
			if bytes.Equal(data, result.resp.Data) {
				n.recordSyncPeerSnapshotResult(result.pid.String(), true, result.elapsed, len(result.resp.Data), false)
				continue
			}
			n.recordSyncPeerInvalidProof(result.pid.String())
		}
		if data != nil {
			return data, winner, nil
		}
	}
	return nil, "", fmt.Errorf("snapshot chunk unavailable index=%d", idx)
}

type syncReplicatedRangeAssignment struct {
	// `From` stores the value associated with this record.
	From uint64
	// `To` stores the value associated with this record.
	To uint64
	// `Peers` stores the value associated with this record.
	Peers []peer.ID
}

type syncReplicatedRangeResult struct {
	// `assignment` stores the value associated with this record.
	assignment syncReplicatedRangeAssignment
	// `blocks` stores the block data handled by this operation.
	blocks []Block
	// `failedPeers` stores the value associated with this record.
	failedPeers map[peer.ID]struct{}
	// `err` stores the error produced by this operation.
	err error
}

type syncHeaderFetchResult struct {
	// `pid` stores the value associated with this record.
	pid peer.ID
	// `headers` stores the block data handled by this operation.
	headers []SyncBlockHeader
	// `err` stores the error produced by this operation.
	err error
	// `elapsed` stores the value associated with this record.
	elapsed time.Duration
	// `timeout` stores the result produced by this operation.
	timeout bool
}

type syncBlockFetchResult struct {
	// `pid` stores the value associated with this record.
	pid peer.ID
	// `blocks` stores the block data handled by this operation.
	blocks []Block
	// `err` stores the error produced by this operation.
	err error
	// `elapsed` stores the value associated with this record.
	elapsed time.Duration
	// `timeout` stores the result produced by this operation.
	timeout bool
}

// syncBlocksEquivalent implements the sync blocks equivalent helper.
func syncBlocksEquivalent(left []Block, right []Block) bool {
	if len(left) != len(right) {
		return false
	}
	// `idx` tracks the current position in the related collection.
	for idx := range left {
		if left[idx].ID != right[idx].ID {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(left[idx].BlockHash), strings.TrimSpace(right[idx].BlockHash)) {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(left[idx].PrevHash), strings.TrimSpace(right[idx].PrevHash)) {
			return false
		}
	}
	return true
}

// syncBlocksMatchHeaders implements the sync blocks match headers helper.
func syncBlocksMatchHeaders(blocks []Block, headers []SyncBlockHeader) bool {
	if len(blocks) != len(headers) {
		return false
	}
	// `idx` tracks the current position in the related collection.
	for idx := range blocks {
		if blocks[idx].ID != headers[idx].Height {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(blocks[idx].BlockHash), strings.TrimSpace(headers[idx].BlockHash)) {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(blocks[idx].PrevHash), strings.TrimSpace(headers[idx].PrevHash)) {
			return false
		}
	}
	return true
}

// syncReplicatedAssignmentPeerCount implements the sync replicated assignment peer count helper.
func syncReplicatedAssignmentPeerCount(assignments []syncReplicatedRangeAssignment) int {
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(assignments))
	// `assignment` tracks the current values while iterating.
	for _, assignment := range assignments {
		// `pid` tracks the current values while iterating.
		for _, pid := range assignment.Peers {
			// `key` stores the key used to access the related value.
			key := strings.TrimSpace(pid.String())
			if key == "" {
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return len(seen)
}

// planReplicatedSyncRangeAssignments implements the plan replicated sync range assignments helper.
func planReplicatedSyncRangeAssignments(start uint64, targetHeight uint64, window uint64, peers []peer.ID, replicationFactor int) []syncReplicatedRangeAssignment {
	// `base` stores the value produced by this operation.
	base := planSyncRangeAssignments(start, targetHeight, window, peers)
	if len(base) == 0 {
		return nil
	}
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	if replicationFactor > len(peers) {
		replicationFactor = len(peers)
	}
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	// `assignments` stores the value produced by this operation.
	assignments := make([]syncReplicatedRangeAssignment, 0, len(base))
	// `idx` and `assignment` track the current position in the related collection.
	for idx, assignment := range base {
		// `group` stores the value produced by this operation.
		group := make([]peer.ID, 0, replicationFactor)
		group = append(group, assignment.Peer)
		// `seen` stores the value produced by this operation.
		seen := map[string]struct{}{
			strings.TrimSpace(assignment.Peer.String()): {},
		}
		// `offset` stores the value produced by this operation.
		for offset := 1; offset < len(peers) && len(group) < replicationFactor; offset++ {
			// `pid` stores the value produced by this operation.
			pid := peers[(idx+offset)%len(peers)]
			// `key` stores the key used to access the related value.
			key := strings.TrimSpace(pid.String())
			if key == "" {
				continue
			}
			// `ok` stores whether the related condition is satisfied.
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			group = append(group, pid)
		}
		assignments = append(assignments, syncReplicatedRangeAssignment{
			From:  assignment.From,
			To:    assignment.To,
			Peers: group,
		})
	}
	return assignments
}

// syncExpectedPrevHashForRange implements the sync expected prev hash for range helper.
func (n *Node) syncExpectedPrevHashForRange(from uint64) string {
	if n == nil || from <= 1 || n.Blockchain == nil {
		return ""
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := n.Blockchain.Height()
	if from != localHeight+1 {
		return ""
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.LoadBlock(int(localHeight))
	if !ok {
		return ""
	}
	return strings.TrimSpace(block.BlockHash)
}

// buildCommonAncestorLocators builds common ancestor locators.
func (n *Node) buildCommonAncestorLocators(localHeight uint64, maxDepth uint64) []HeaderSyncLocator {
	if n == nil || localHeight == 0 {
		return nil
	}
	if maxDepth == 0 {
		maxDepth = syncHeaderCommonAncestorDepth()
	}
	// `locators` stores the value produced by this operation.
	locators := make([]HeaderSyncLocator, 0, 16)
	// `seen` stores the value produced by this operation.
	seen := make(map[uint64]struct{}, 16)
	// `step` stores the value produced by this operation.
	step := uint64(1)
	// `height` stores the value produced by this operation.
	for height := localHeight; height > 0; {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[height]; !ok {
			// `block` and `ok` store whether the related condition is satisfied.
			if block, ok := n.LoadBlock(int(height)); ok && strings.TrimSpace(block.BlockHash) != "" {
				locators = append(locators, HeaderSyncLocator{
					Height:    height,
					BlockHash: strings.TrimSpace(block.BlockHash),
				})
				seen[height] = struct{}{}
			}
		}
		if height == 1 {
			break
		}
		if localHeight-height >= maxDepth {
			height = 1
			continue
		}
		if len(locators) < 8 {
			height--
			continue
		}
		if height <= step {
			height = 1
			continue
		}
		height -= step
		if step < 1024 {
			step *= 2
		}
	}
	return locators
}

// findCommonAncestorWithPeer implements the find common ancestor with peer helper.
func (n *Node) findCommonAncestorWithPeer(pid peer.ID, localHeight uint64) (uint64, string, error) {
	// `locators` stores the value produced by this operation.
	locators := n.buildCommonAncestorLocators(localHeight, syncHeaderCommonAncestorDepth())
	if len(locators) == 0 {
		return 0, "", fmt.Errorf("common ancestor locators unavailable")
	}
	return n.requestCommonAncestorFromPeer(pid, locators)
}

// headerSyncFallbackEligible implements the header sync fallback eligible helper.
func headerSyncFallbackEligible(err error) bool {
	if err == nil {
		return false
	}
	// `msg` stores the value produced by this operation.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "protocol") ||
		strings.Contains(msg, "empty header response") ||
		strings.Contains(msg, "stream reset") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "nil stream") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded")
}

// fetchCanonicalHeadersForRange implements the fetch canonical headers for range helper.
func (n *Node) fetchCanonicalHeadersForRange(assign syncReplicatedRangeAssignment) ([]SyncBlockHeader, []peer.ID, map[peer.ID]struct{}, bool, error) {
	if n == nil || len(assign.Peers) == 0 {
		return nil, nil, nil, false, fmt.Errorf("header_sync_peers_unavailable")
	}
	// `expectedPrevHash` stores the digest used to identify or verify the related data.
	expectedPrevHash := n.syncExpectedPrevHashForRange(assign.From)
	// `failedPeers` stores the value produced by this operation.
	failedPeers := make(map[peer.ID]struct{})
	// `matchingPeers` stores the value produced by this operation.
	matchingPeers := make([]peer.ID, 0, len(assign.Peers))
	// `fallbackPeers` stores the value produced by this operation.
	fallbackPeers := make([]peer.ID, 0, len(assign.Peers))
	// `fallbackSeen` stores the value produced by this operation.
	fallbackSeen := make(map[string]struct{}, len(assign.Peers))
	// `canonical` stores the value used by this operation.
	var canonical []SyncBlockHeader
	// `headerPrevMismatchNoCommonAncestor` tracks whether every peer view may
	// be ahead of a local fork whose previous hash is no longer canonical.
	headerPrevMismatchNoCommonAncestor := false

	// `pid` tracks the current values while iterating.
	for _, pid := range assign.Peers {
		// `started` stores the value produced by this operation.
		started := time.Now()
		// `headers` and `err` store the error produced by this operation.
		headers, err := n.requestHeadersFromPeer(pid, assign.From, assign.To)
		// `result` stores the result produced by this operation.
		result := syncHeaderFetchResult{
			pid:     pid,
			headers: headers,
			err:     err,
			elapsed: time.Since(started),
			timeout: isSyncTimeoutError(err),
		}
		if result.err != nil {
			if headerSyncFallbackEligible(result.err) {
				// `key` stores the key used to access the related value.
				key := strings.TrimSpace(result.pid.String())
				if key != "" {
					// `ok` stores whether the related condition is satisfied.
					if _, ok := fallbackSeen[key]; !ok {
						fallbackSeen[key] = struct{}{}
						fallbackPeers = append(fallbackPeers, result.pid)
					}
				}
				continue
			}
			n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, 0, result.timeout)
			failedPeers[result.pid] = struct{}{}
			continue
		}
		// `err` stores the error produced by this operation.
		if err := validateSyncHeaderBatch(assign.From, result.headers, expectedPrevHash); err != nil {
			n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, 0, false)
			failedPeers[result.pid] = struct{}{}
			if strings.Contains(err.Error(), "header_first_prev_hash_mismatch") && n.Blockchain != nil {
				// `commonHeight`, `commonHash`, and `commonErr` store the error produced by this operation.
				commonHeight, commonHash, commonErr := n.findCommonAncestorWithPeer(result.pid, n.Blockchain.Height())
				if commonErr == nil {
					fmt.Printf("[SYNC-FORK] peer=%s local=%d requested=%d-%d common_height=%d common_hash=%s reason=header_prev_mismatch\n",
						ShortID(result.pid.String()),
						n.Blockchain.Height(),
						assign.From,
						assign.To,
						commonHeight,
						ShortHash(commonHash),
					)
					if commonHeight > 0 && commonHeight < n.Blockchain.Height() && commonHeight+1 < assign.From {
						if n.rewindLocalChainToHeight(commonHeight, "header_prev_mismatch") {
							return nil, nil, failedPeers, false, fmt.Errorf("local_rewind_to_common_ancestor")
						}
					}
				} else {
					headerPrevMismatchNoCommonAncestor = true
				}
			}
			continue
		}
		if canonical == nil {
			canonical = append([]SyncBlockHeader{}, result.headers...)
			matchingPeers = append(matchingPeers, result.pid)
			continue
		}
		if syncHeaderBatchesEqual(canonical, result.headers) {
			matchingPeers = append(matchingPeers, result.pid)
			continue
		}
		n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, 0, false)
		failedPeers[result.pid] = struct{}{}
	}

	if len(canonical) > 0 && len(matchingPeers) > 0 {
		return canonical, matchingPeers, failedPeers, false, nil
	}
	if len(fallbackPeers) > 0 {
		return nil, fallbackPeers, failedPeers, true, nil
	}
	if headerPrevMismatchNoCommonAncestor && n.Blockchain != nil && assign.To > n.Blockchain.Height() {
		localHeight := n.Blockchain.Height()
		fmt.Printf("[SYNC-FORK] action=snapshot_resync local=%d requested=%d-%d reason=header_prev_mismatch_no_common_ancestor\n",
			localHeight, assign.From, assign.To)
		go n.forceSnapshotResyncNow(assign.To, "header_prev_mismatch_no_common_ancestor")
		return nil, nil, failedPeers, false, fmt.Errorf("snapshot_resync_scheduled_header_prev_mismatch")
	}
	return nil, nil, failedPeers, false, fmt.Errorf("header_sync_no_valid_headers")
}

// fetchReplicatedBlocksForRange implements the fetch replicated blocks for range helper.
func (n *Node) fetchReplicatedBlocksForRange(assign syncReplicatedRangeAssignment, headers []SyncBlockHeader, peers []peer.ID) ([]Block, map[peer.ID]struct{}, error) {
	if n == nil || len(peers) == 0 {
		return nil, nil, fmt.Errorf("block_sync_peers_unavailable")
	}
	// `resultsCh` stores the result produced by this operation.
	resultsCh := make(chan syncBlockFetchResult, len(peers))
	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		// `pid` stores the value produced by this operation.
		pid := pid
		go func() {
			// `started` stores the value produced by this operation.
			started := time.Now()
			// `blocks` and `err` store the error produced by this operation.
			blocks, _, _, err := n.requestBlocksFromPeer(pid, assign.From, assign.To, false, 0)
			resultsCh <- syncBlockFetchResult{
				pid:     pid,
				blocks:  blocks,
				err:     err,
				elapsed: time.Since(started),
				timeout: isSyncTimeoutError(err),
			}
		}()
	}

	// `failedPeers` stores the value produced by this operation.
	failedPeers := make(map[peer.ID]struct{})
	// `winner` stores the value used by this operation.
	var winner []Block
	for range peers {
		// `result` stores the result produced by this operation.
		result := <-resultsCh
		if result.err != nil || len(result.blocks) == 0 {
			n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, 0, result.timeout)
			failedPeers[result.pid] = struct{}{}
			continue
		}
		if result.blocks[0].ID != assign.From || result.blocks[len(result.blocks)-1].ID != assign.To {
			n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, len(MustJSON(result.blocks)), false)
			failedPeers[result.pid] = struct{}{}
			continue
		}
		// `err` stores the error produced by this operation.
		if err := preflightDeltaReplayBatch(assign.From, result.blocks); err != nil {
			n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, len(MustJSON(result.blocks)), false)
			failedPeers[result.pid] = struct{}{}
			continue
		}
		if len(headers) > 0 && !syncBlocksMatchHeaders(result.blocks, headers) {
			n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, len(MustJSON(result.blocks)), false)
			failedPeers[result.pid] = struct{}{}
			continue
		}
		if winner == nil {
			winner = append([]Block{}, result.blocks...)
			n.recordSyncPeerBlockResult(result.pid.String(), true, result.elapsed, len(MustJSON(result.blocks)), false)
			continue
		}
		if syncBlocksEquivalent(winner, result.blocks) {
			n.recordSyncPeerBlockResult(result.pid.String(), true, result.elapsed, len(MustJSON(result.blocks)), false)
			continue
		}
		n.recordSyncPeerBlockResult(result.pid.String(), false, result.elapsed, len(MustJSON(result.blocks)), false)
		failedPeers[result.pid] = struct{}{}
	}
	if winner == nil {
		return nil, failedPeers, fmt.Errorf("replicated_block_range_unavailable")
	}
	return winner, failedPeers, nil
}

// fetchSingleProviderBlocksForRange implements the fetch single provider blocks for range helper.
func (n *Node) fetchSingleProviderBlocksForRange(assign syncReplicatedRangeAssignment, reason string) ([]Block, peer.ID, map[peer.ID]struct{}, error) {
	// `failedPeers` stores the value produced by this operation.
	failedPeers := make(map[peer.ID]struct{})
	if n == nil || n.Blockchain == nil || len(assign.Peers) == 0 || assign.From == 0 || assign.To < assign.From {
		return nil, "", failedPeers, fmt.Errorf("single_provider_range_unavailable")
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := n.Blockchain.Height()
	if assign.From != localHeight+1 {
		return nil, "", failedPeers, fmt.Errorf("single_provider_range_not_next local=%d from=%d", localHeight, assign.From)
	}
	// `localTip` stores the value produced by this operation.
	localTip := n.Blockchain.LastBlock()
	// `pid` tracks the current values while iterating.
	for _, pid := range assign.Peers {
		// `started` stores the value produced by this operation.
		started := time.Now()
		// `blocks` and `err` store the error produced by this operation.
		blocks, _, _, err := n.requestBlocksFromPeerDirect(pid, assign.From, assign.To, false, 0)
		// `elapsed` stores the value produced by this operation.
		elapsed := time.Since(started)
		if err != nil || len(blocks) == 0 {
			n.recordSyncPeerBlockResult(pid.String(), false, elapsed, 0, isSyncTimeoutError(err))
			failedPeers[pid] = struct{}{}
			continue
		}
		if blocks[0].ID != assign.From || blocks[len(blocks)-1].ID != assign.To {
			n.recordSyncPeerBlockResult(pid.String(), false, elapsed, len(MustJSON(blocks)), false)
			failedPeers[pid] = struct{}{}
			continue
		}
		if strings.TrimSpace(localTip.BlockHash) != "" && blocks[0].PrevHash != localTip.BlockHash {
			n.recordSyncPeerBlockResult(pid.String(), false, elapsed, len(MustJSON(blocks)), false)
			failedPeers[pid] = struct{}{}
			continue
		}
		// `err` stores the error produced by this operation.
		if err := preflightDeltaReplayBatch(assign.From, blocks); err != nil {
			n.recordSyncPeerBlockResult(pid.String(), false, elapsed, len(MustJSON(blocks)), false)
			failedPeers[pid] = struct{}{}
			continue
		}
		n.recordSyncPeerBlockResult(pid.String(), true, elapsed, len(MustJSON(blocks)), false)
		fmt.Printf("[SYNC-RANGE-FALLBACK] range=%d-%d provider=%s reason=%s action=single_provider_verified count=%d\n",
			assign.From, assign.To, ShortID(pid.String()), strings.TrimSpace(reason), len(blocks))
		return append([]Block{}, blocks...), pid, failedPeers, nil
	}
	return nil, "", failedPeers, fmt.Errorf("single_provider_range_unavailable")
}

// requestReplicatedBlockRange implements the request replicated block range helper.
func (n *Node) requestReplicatedBlockRange(assign syncReplicatedRangeAssignment) ([]Block, map[peer.ID]struct{}, error) {
	// `headers`, `peers`, `failedPeers`, `fallbackMode`, and `err` store the error produced by this operation.
	headers, peers, failedPeers, fallbackMode, err := n.fetchCanonicalHeadersForRange(assign)
	if err != nil {
		// `blocks`, `singleFailedPeers`, and `singleErr` store the error produced by this operation.
		blocks, _, singleFailedPeers, singleErr := n.fetchSingleProviderBlocksForRange(assign, "header_"+err.Error())
		if failedPeers == nil {
			failedPeers = make(map[peer.ID]struct{})
		}
		// `pid` tracks the current values while iterating.
		for pid := range singleFailedPeers {
			failedPeers[pid] = struct{}{}
		}
		if singleErr == nil {
			return blocks, failedPeers, nil
		}
		return nil, failedPeers, err
	}
	if fallbackMode {
		headers = nil
		peers = append([]peer.ID{}, assign.Peers...)
	}
	// `blocks`, `blockFailedPeers`, and `err` store the error produced by this operation.
	blocks, blockFailedPeers, err := n.fetchReplicatedBlocksForRange(assign, headers, peers)
	if len(blockFailedPeers) > 0 {
		if failedPeers == nil {
			failedPeers = make(map[peer.ID]struct{}, len(blockFailedPeers))
		}
		// `pid` tracks the current values while iterating.
		for pid := range blockFailedPeers {
			failedPeers[pid] = struct{}{}
		}
	}
	if err != nil {
		// `blocks`, `singleFailedPeers`, and `singleErr` store the error produced by this operation.
		blocks, _, singleFailedPeers, singleErr := n.fetchSingleProviderBlocksForRange(assign, "replicated_"+err.Error())
		// `pid` tracks the current values while iterating.
		for pid := range singleFailedPeers {
			failedPeers[pid] = struct{}{}
		}
		if singleErr == nil {
			return blocks, failedPeers, nil
		}
	}
	return blocks, failedPeers, err
}

// requestParallelReplicatedBlockRanges implements the request parallel replicated block ranges helper.
func (n *Node) requestParallelReplicatedBlockRanges(assignments []syncReplicatedRangeAssignment) ([]Block, map[peer.ID]struct{}, error) {
	if n == nil || len(assignments) == 0 {
		return nil, nil, fmt.Errorf("replicated_parallel_sync_assignments_empty")
	}
	// `resultsCh` stores the result produced by this operation.
	resultsCh := make(chan syncReplicatedRangeResult, len(assignments))
	// `assignment` tracks the current values while iterating.
	for _, assignment := range assignments {
		// `assignment` stores the value produced by this operation.
		assignment := assignment
		go func() {
			// `blocks`, `failedPeers`, and `err` store the error produced by this operation.
			blocks, failedPeers, err := n.requestReplicatedBlockRange(assignment)
			resultsCh <- syncReplicatedRangeResult{
				assignment:  assignment,
				blocks:      blocks,
				failedPeers: failedPeers,
				err:         err,
			}
		}()
	}

	// `results` stores the result produced by this operation.
	results := make([]syncReplicatedRangeResult, 0, len(assignments))
	// `failedPeers` stores the value produced by this operation.
	failedPeers := make(map[peer.ID]struct{})
	for range assignments {
		// `result` stores the result produced by this operation.
		result := <-resultsCh
		// `pid` tracks the current values while iterating.
		for pid := range result.failedPeers {
			failedPeers[pid] = struct{}{}
		}
		if result.err != nil || len(result.blocks) == 0 {
			return nil, failedPeers, result.err
		}
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].assignment.From < results[j].assignment.From
	})
	// `merged` stores the value produced by this operation.
	merged := make([]Block, 0)
	// `result` tracks the result produced by this operation.
	for _, result := range results {
		merged = append(merged, result.blocks...)
	}
	if len(merged) == 0 {
		return nil, failedPeers, fmt.Errorf("replicated_parallel_sync_fetch_empty")
	}
	// `err` stores the error produced by this operation.
	if err := preflightDeltaReplayBatch(assignments[0].From, merged); err != nil {
		return nil, failedPeers, err
	}
	return merged, failedPeers, nil
}
