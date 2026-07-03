package main

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	turboSyncQueueCapacity     = 10000
	turboSyncLoopInterval      = 300 * time.Millisecond
	turboSyncStableLoopDelay   = 1 * time.Second
	turboSyncStallTimeout      = 30 * time.Second
	turboSyncApplyFailBudget   = 5
	turboSyncPeerFailBudget    = 3
	turboSyncReplayThreshold   = 2000
	turboSyncSnapshotThreshold = 10000
)

type TurboSync struct {
	node *Node

	chain       *Blockchain
	peers       []peer.ID
	activePeers []peer.ID
	provider    peer.ID
	blockQueue  chan Block

	targetHeight  uint64
	turboMode     bool
	lastProgress  time.Time
	lastTipHeight uint64

	applyFailures map[string]int
	peerFailures  map[string]int

	mu               sync.Mutex
	applyOnce        sync.Once
	downloadInFlight bool
}

func NewTurboSync(node *Node, peers []peer.ID) *TurboSync {
	var chain *Blockchain
	if node != nil {
		chain = node.Blockchain
	}
	controllerPeers := append([]peer.ID{}, peers...)
	provider := peer.ID("")
	if len(controllerPeers) > 0 {
		provider = controllerPeers[0]
	}
	initialHeight := uint64(0)
	if chain != nil {
		initialHeight = chain.Height()
	}
	return &TurboSync{
		node:          node,
		chain:         chain,
		peers:         controllerPeers,
		provider:      provider,
		blockQueue:    make(chan Block, turboSyncQueueCapacity),
		lastProgress:  time.Now(),
		lastTipHeight: initialHeight,
		applyFailures: make(map[string]int),
		peerFailures:  make(map[string]int),
	}
}

func (ts *TurboSync) workerCap() int {
	if ts == nil || ts.node == nil {
		return runtimeWorkerBudget()
	}
	return runtimeSyncWorkerCap(ts.node.Role)
}

func (ts *TurboSync) loopInterval() time.Duration {
	if truthyEnv("MSC_TURBO_SYNC_FAST") {
		return turboSyncLoopInterval
	}
	return turboSyncStableLoopDelay
}

func (ts *TurboSync) Start() {
	if ts == nil || ts.node == nil || ts.chain == nil {
		return
	}
	ts.applyOnce.Do(func() {
		go ts.applyWorker()
	})

	ticker := time.NewTicker(ts.loopInterval())
	defer ticker.Stop()

	for {
		local := ts.chain.Height()
		target := ts.getNetworkHeight()

		ts.mu.Lock()
		ts.targetHeight = target
		ts.mu.Unlock()

		if target == 0 {
			<-ticker.C
			continue
		}
		if local >= target {
			log.Printf("[SYNC-TURBO] completed local=%d target=%d", local, target)
			return
		}

		if ts.detectStall() {
			log.Printf("[SYNC-TURBO] stall detected local=%d target=%d", local, target)
			ts.rotateProvider()
		}

		lag := syncLagBlocks(local, target)
		if lag > turboSyncSnapshotThreshold {
			ts.snapshotSync()
			<-ticker.C
			continue
		}

		ts.decideMode()
		ts.selectPeers()

		start := local + 1
		if ts.beginDownload() {
			if ts.isTurboMode() {
				end := start + ts.computeTurboWindow(lag) - 1
				if end < start || end > target {
					end = target
				}
				go ts.parallelDownload(start, end)
			} else {
				end := start + syncBlockRequestMaxBlocks(0) - 1
				if end < start || end > target {
					end = target
				}
				go ts.downloadRange(ts.currentProvider(), start, end)
			}
		}

		<-ticker.C
	}
}

func (ts *TurboSync) decideMode() {
	if ts == nil || ts.chain == nil {
		return
	}
	lag := syncLagBlocks(ts.chain.Height(), ts.currentTargetHeight())
	enableTurbo := lag > turboSyncReplayThreshold

	ts.mu.Lock()
	changed := ts.turboMode != enableTurbo
	ts.turboMode = enableTurbo
	ts.mu.Unlock()

	if changed && enableTurbo {
		log.Println("[SYNC-TURBO] turbo mode enabled")
	}
}

func (ts *TurboSync) selectPeers() {
	if ts == nil {
		return
	}

	ts.mu.Lock()
	peers := append([]peer.ID{}, ts.peers...)
	peerFailures := make(map[string]int, len(ts.peerFailures))
	for key, value := range ts.peerFailures {
		peerFailures[key] = value
	}
	ts.mu.Unlock()

	active := make([]peer.ID, 0, len(peers))
	for _, pid := range peers {
		peerID := strings.TrimSpace(pid.String())
		if peerID == "" {
			continue
		}
		if peerFailures[peerID] >= turboSyncPeerFailBudget {
			continue
		}
		if ts.node != nil {
			score := ts.node.syncPeerScoreValue(peerID)
			connected := ts.node.isPeerConnected(peerID)
			if !connected && score < 0 {
				continue
			}
		}
		active = append(active, pid)
	}
	if len(active) == 0 {
		active = peers
	}
	if cap := ts.workerCap(); cap > 0 && len(active) > cap {
		active = active[:cap]
	}

	ts.mu.Lock()
	ts.activePeers = append([]peer.ID{}, active...)
	if len(ts.activePeers) > 0 {
		found := false
		for _, pid := range ts.activePeers {
			if pid == ts.provider {
				found = true
				break
			}
		}
		if !found {
			ts.provider = ts.activePeers[0]
		}
	}
	ts.mu.Unlock()
}

func (ts *TurboSync) parallelDownload(start uint64, end uint64) {
	defer ts.finishDownload()
	if ts == nil || ts.node == nil || ts.chain == nil || start == 0 || end < start {
		return
	}

	assignments := planTurboAssignments(start, end, ts.currentActivePeers())
	if len(assignments) == 0 {
		return
	}
	if cap := ts.workerCap(); cap > 0 && len(assignments) > cap {
		assignments = assignments[:cap]
	}

	var wg sync.WaitGroup
	for _, assignment := range assignments {
		wg.Add(1)
		assignment := assignment
		go func() {
			defer wg.Done()
			ts.downloadRange(assignment.Peer, assignment.From, assignment.To)
		}()
	}
	wg.Wait()
}

func (ts *TurboSync) downloadRange(provider peer.ID, start uint64, end uint64) {
	if ts == nil || ts.node == nil || start == 0 || end < start {
		return
	}

	perRequestMax := syncBlockRequestMaxBlocks(0)
	if perRequestMax == 0 {
		return
	}
	if provider == "" {
		ts.rotateProvider()
		provider = ts.currentProvider()
		if provider == "" {
			return
		}
	}

	for cursor := start; cursor <= end; {
		chunkEnd := cursor + perRequestMax - 1
		if chunkEnd < cursor || chunkEnd > end {
			chunkEnd = end
		}

		started := time.Now()
		ts.node.setSyncProvider(provider.String())
		blocks, _, _, err := ts.node.requestBlocksFromPeer(provider, cursor, chunkEnd, false, 0)
		if err != nil || len(blocks) == 0 {
			ts.node.recordSyncPeerBlockResult(provider.String(), false, time.Since(started), 0, isSyncTimeoutError(err))
			failCount := ts.recordPeerFailure(provider.String())
			log.Printf("[SYNC-TURBO] download failed provider=%s range=%d-%d fail_count=%d err=%v",
				ShortID(provider.String()), cursor, chunkEnd, failCount, err)
			if failCount >= turboSyncPeerFailBudget {
				ts.rotateProvider()
			}
			return
		}

		ts.clearPeerFailure(provider.String())
		ts.node.recordSyncPeerBlockResult(provider.String(), true, time.Since(started), len(MustJSON(blocks)), false)
		for _, block := range blocks {
			ts.blockQueue <- block
		}

		lastHeight := blocks[len(blocks)-1].ID
		if lastHeight < cursor {
			return
		}
		cursor = lastHeight + 1
	}
}

func (ts *TurboSync) applyWorker() {
	if ts == nil || ts.chain == nil {
		return
	}

	pending := make(map[uint64]Block)
	for block := range ts.blockQueue {
		if block.ID == 0 {
			continue
		}
		pending[block.ID] = block

		for {
			next := ts.chain.Height() + 1
			candidate, ok := pending[next]
			if !ok {
				break
			}

			err := ts.applyBlock(candidate)
			if err != nil {
				log.Printf("[SYNC-TURBO] apply error: %v", err)
				if ts.detectFork(candidate) {
					ts.recoverFork()
				}
				if ts.recordFailure(strings.TrimSpace(candidate.BlockHash)) {
					log.Printf("[SYNC-TURBO] repeated apply failure h=%d hash=%s action=provider_rotate",
						candidate.ID, ShortHash(candidate.BlockHash))
					ts.rotateProvider()
					if syncLagBlocks(ts.chain.Height(), ts.currentTargetHeight()) > turboSyncSnapshotThreshold {
						ts.snapshotSync()
					}
				}
				break
			}

			delete(pending, next)
			ts.clearFailure(strings.TrimSpace(candidate.BlockHash))
		}
	}
}

func (ts *TurboSync) applyBlock(block Block) error {
	if ts == nil || ts.node == nil || ts.chain == nil {
		return errors.New("turbo_sync_uninitialized")
	}
	if ts.detectFork(block) {
		return newBlockApplyError(block, "parent_mismatch", nil)
	}

	beforeHeight := ts.chain.Height()
	ts.node.applyMu.Lock()
	err := ts.node.ReceiveBlock(block, ts.chain)
	ts.node.ProcessQueuedBlocks()
	afterHeight := ts.chain.Height()
	ts.node.applyMu.Unlock()

	if err != nil {
		return err
	}
	if afterHeight <= beforeHeight {
		return newBlockApplyError(block, "execution_failed", errors.New("commit_incomplete"))
	}

	if shouldAutoCreateSnapshotAtHeight(afterHeight) {
		ts.createSnapshot(afterHeight)
	}
	ts.updateTip(afterHeight)
	ts.recordProgress()
	if DebugSync || DebugConsensus {
		log.Printf("[COMMIT] height=%d", afterHeight)
	}
	return nil
}

func (ts *TurboSync) updateTip(height uint64) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	prev := ts.lastTipHeight
	ts.lastTipHeight = height
	ts.mu.Unlock()
	log.Printf("[HEIGHT] %d -> %d", prev, height)
	if ts.node != nil {
		ts.node.noteSyncProgress(height)
	}
}

func (ts *TurboSync) detectFork(block Block) bool {
	if ts == nil || ts.chain == nil {
		return false
	}
	last := ts.chain.LastBlock()
	expectedPrev := strings.TrimSpace(last.BlockHash)
	gotPrev := strings.TrimSpace(block.PrevHash)
	if expectedPrev == "" {
		return false
	}
	if strings.EqualFold(expectedPrev, gotPrev) {
		return false
	}
	log.Printf("[SYNC-TURBO] fork detected height=%d expected_prev=%s got_prev=%s",
		block.ID, ShortHash(expectedPrev), ShortHash(gotPrev))
	return true
}

func (ts *TurboSync) recoverFork() {
	if ts == nil || ts.chain == nil {
		return
	}
	log.Printf("[SYNC-TURBO] fork recovery local=%d target=%d action=provider_rotate_snapshot",
		ts.chain.Height(), ts.currentTargetHeight())
	ts.rotateProvider()
	ts.snapshotSync()
}

func (ts *TurboSync) snapshotSync() {
	if ts == nil || ts.node == nil || ts.chain == nil {
		return
	}
	target := ts.currentTargetHeight()
	if target == 0 || target <= ts.chain.Height() {
		return
	}
	log.Printf("[SYNC-TURBO] snapshot sync target=%d local=%d", target, ts.chain.Height())
	ts.node.forceSnapshotSyncToHeight(target, "turbo_sync_snapshot")
	ts.recordProgress()
}

func (ts *TurboSync) createSnapshot(height uint64) {
	if ts == nil || ts.node == nil || height == 0 {
		return
	}
	if snap, err := ts.node.GetSnapshot(height); err == nil && snap != nil {
		return
	}
	block, ok := ts.chain.GetBlock(height)
	if !ok || strings.TrimSpace(block.BlockHash) == "" {
		return
	}
	if err := ts.node.CreateSnapshot(height, strings.TrimSpace(block.BlockHash)); err != nil {
		log.Printf("[SYNC-TURBO] snapshot create failed height=%d err=%v", height, err)
		return
	}
	log.Printf("[SNAPSHOT] created height=%d", height)
}

func (ts *TurboSync) recordProgress() {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	ts.lastProgress = time.Now()
	ts.mu.Unlock()
}

func (ts *TurboSync) detectStall() bool {
	if ts == nil {
		return false
	}
	ts.mu.Lock()
	last := ts.lastProgress
	ts.mu.Unlock()
	return time.Since(last) > turboSyncStallTimeout
}

func (ts *TurboSync) recordFailure(hash string) bool {
	if ts == nil || strings.TrimSpace(hash) == "" {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.applyFailures[hash]++
	return ts.applyFailures[hash] > turboSyncApplyFailBudget
}

func (ts *TurboSync) clearFailure(hash string) {
	if ts == nil || strings.TrimSpace(hash) == "" {
		return
	}
	ts.mu.Lock()
	delete(ts.applyFailures, hash)
	ts.mu.Unlock()
}

func (ts *TurboSync) recordPeerFailure(peerID string) int {
	if ts == nil || strings.TrimSpace(peerID) == "" {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.peerFailures[peerID]++
	return ts.peerFailures[peerID]
}

func (ts *TurboSync) clearPeerFailure(peerID string) {
	if ts == nil || strings.TrimSpace(peerID) == "" {
		return
	}
	ts.mu.Lock()
	delete(ts.peerFailures, peerID)
	ts.mu.Unlock()
}

func (ts *TurboSync) rotateProvider() {
	if ts == nil {
		return
	}

	active := ts.currentActivePeers()
	current := ts.currentProvider()
	if len(active) == 0 {
		ts.selectPeers()
		active = ts.currentActivePeers()
	}
	if len(active) == 0 {
		return
	}

	best := current
	bestScore := -1e18
	bestReputation := -1.0
	found := false
	for _, pid := range active {
		if pid == "" {
			continue
		}
		if current != "" && pid == current && len(active) > 1 {
			continue
		}
		reputation := 0.5
		score := 0.0
		if ts.node != nil {
			reputation = ts.node.syncPeerReputationValue(pid.String())
			score = ts.node.syncPeerScoreValue(pid.String())
			if ts.node.isPeerConnected(pid.String()) {
				score += 1000
			}
		}
		score -= float64(ts.currentPeerFailures(pid.String()) * 100)
		if !found || reputation > bestReputation || (reputation == bestReputation && score > bestScore) {
			best = pid
			bestReputation = reputation
			bestScore = score
			found = true
		}
	}
	if !found {
		best = active[0]
	}

	ts.mu.Lock()
	ts.provider = best
	ts.mu.Unlock()
	if ts.node != nil {
		if strings.TrimSpace(current.String()) != "" && current != best {
			ts.node.setSyncAvoidProviderOnce(current.String())
		}
		ts.node.setSyncProvider(best.String())
	}
	log.Printf("[SYNC-TURBO] provider switched=%s previous=%s", ShortID(best.String()), ShortID(current.String()))
}

func (ts *TurboSync) getNetworkHeight() uint64 {
	if ts == nil || ts.chain == nil {
		return 0
	}
	localHeight := ts.chain.Height()
	if ts.node == nil {
		return localHeight
	}
	quorumHeight, _, required, quorumOK := ts.node.majorityHeartbeatHeight()
	observedHeight, observedVotes := ts.node.bestObservedSyncHeight()
	target := selectSyncTargetHeight(localHeight, quorumHeight, quorumOK, observedHeight, observedVotes, required)
	if target < localHeight {
		target = localHeight
	}
	return target
}

func (ts *TurboSync) computeTurboWindow(lag uint64) uint64 {
	window := computeAdaptiveSyncFetchWindow(lag)
	if window == 0 {
		window = syncBlockRequestMaxBlocks(0)
	}
	activeCount := uint64(len(ts.currentActivePeers()))
	if cap := ts.workerCap(); cap > 0 && activeCount > uint64(cap) {
		activeCount = uint64(cap)
	}
	if activeCount > 1 {
		minParallel := activeCount * syncBlockRequestMaxBlocks(0)
		if window < minParallel {
			window = minParallel
		}
	}
	return window
}

func (ts *TurboSync) beginDownload() bool {
	if ts == nil {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.downloadInFlight {
		return false
	}
	ts.downloadInFlight = true
	return true
}

func (ts *TurboSync) finishDownload() {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	ts.downloadInFlight = false
	ts.mu.Unlock()
}

func (ts *TurboSync) currentProvider() peer.ID {
	if ts == nil {
		return ""
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.provider
}

func (ts *TurboSync) currentActivePeers() []peer.ID {
	if ts == nil {
		return nil
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]peer.ID{}, ts.activePeers...)
}

func (ts *TurboSync) currentTargetHeight() uint64 {
	if ts == nil {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.targetHeight
}

func (ts *TurboSync) isTurboMode() bool {
	if ts == nil {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.turboMode
}

func (ts *TurboSync) currentPeerFailures(peerID string) int {
	if ts == nil || strings.TrimSpace(peerID) == "" {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.peerFailures[peerID]
}

type turboRangeAssignment struct {
	Peer peer.ID
	From uint64
	To   uint64
}

func planTurboAssignments(start uint64, end uint64, peers []peer.ID) []turboRangeAssignment {
	if start == 0 || end < start || len(peers) == 0 {
		return nil
	}

	total := end - start + 1
	chunk := total / uint64(len(peers))
	remainder := total % uint64(len(peers))
	if chunk == 0 {
		chunk = 1
	}

	assignments := make([]turboRangeAssignment, 0, len(peers))
	cursor := start
	for _, pid := range peers {
		if cursor > end {
			break
		}
		size := chunk
		if remainder > 0 {
			size++
			remainder--
		}
		to := cursor + size - 1
		if to < cursor || to > end {
			to = end
		}
		assignments = append(assignments, turboRangeAssignment{
			Peer: pid,
			From: cursor,
			To:   to,
		})
		cursor = to + 1
	}
	return assignments
}
