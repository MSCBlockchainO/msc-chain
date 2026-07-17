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
	// `turboSyncQueueCapacity` defines the constant value used by this package.
	turboSyncQueueCapacity     = 10000
	// `turboSyncLoopInterval` defines the value currently being processed.
	turboSyncLoopInterval      = 300 * time.Millisecond
	// `turboSyncStallTimeout` defines the result produced by this operation.
	turboSyncStallTimeout      = 30 * time.Second
	// `turboSyncApplyFailBudget` defines the constant value used by this package.
	turboSyncApplyFailBudget   = 5
	// `turboSyncPeerFailBudget` defines the constant value used by this package.
	turboSyncPeerFailBudget    = 3
	// `turboSyncReplayThreshold` defines the constant value used by this package.
	turboSyncReplayThreshold   = 2000
	// `turboSyncSnapshotThreshold` defines the constant value used by this package.
	turboSyncSnapshotThreshold = 10000
)

type TurboSync struct {
	// `node` stores the value associated with this record.
	node *Node

	// `chain` stores the value associated with this record.
	chain       *Blockchain
	// `peers` stores the value associated with this record.
	peers       []peer.ID
	// `activePeers` stores the value associated with this record.
	activePeers []peer.ID
	// `provider` stores the value associated with this record.
	provider    peer.ID
	// `blockQueue` stores the block data handled by this operation.
	blockQueue  chan Block

	// `targetHeight` stores the value associated with this record.
	targetHeight  uint64
	// `turboMode` stores the value associated with this record.
	turboMode     bool
	// `lastProgress` stores the value associated with this record.
	lastProgress  time.Time
	// `lastTipHeight` stores the value associated with this record.
	lastTipHeight uint64

	// `applyFailures` stores the result produced by this operation.
	applyFailures map[string]int
	// `peerFailures` stores the result produced by this operation.
	peerFailures  map[string]int

	// `mu` stores the synchronization state protecting shared data.
	mu               sync.Mutex
	// `applyOnce` stores the value associated with this record.
	applyOnce        sync.Once
	// `downloadInFlight` stores the value associated with this record.
	downloadInFlight bool
}

// NewTurboSync creates a new turbo sync.
func NewTurboSync(node *Node, peers []peer.ID) *TurboSync {
	// `chain` stores the value used by this operation.
	var chain *Blockchain
	if node != nil {
		chain = node.Blockchain
	}
	// `controllerPeers` stores the value produced by this operation.
	controllerPeers := append([]peer.ID{}, peers...)
	// `provider` stores the value produced by this operation.
	provider := peer.ID("")
	if len(controllerPeers) > 0 {
		provider = controllerPeers[0]
	}
	// `initialHeight` stores the current position in the related collection.
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

// Start implements the start helper.
func (ts *TurboSync) Start() {
	if ts == nil || ts.node == nil || ts.chain == nil {
		return
	}
	ts.applyOnce.Do(func() {
		go ts.applyWorker()
	})

	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(turboSyncLoopInterval)
	defer ticker.Stop()

	for {
		// `local` stores the value produced by this operation.
		local := ts.chain.Height()
		// `target` stores the value produced by this operation.
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

		// `lag` stores the value produced by this operation.
		lag := syncLagBlocks(local, target)
		if lag > turboSyncSnapshotThreshold {
			ts.snapshotSync()
			<-ticker.C
			continue
		}

		ts.decideMode()
		ts.selectPeers()

		// `start` stores the value produced by this operation.
		start := local + 1
		if ts.beginDownload() {
			if ts.isTurboMode() {
				// `end` stores the value produced by this operation.
				end := start + ts.computeTurboWindow(lag) - 1
				if end < start || end > target {
					end = target
				}
				go ts.parallelDownload(start, end)
			} else {
				// `end` stores the value produced by this operation.
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

// decideMode implements the decide mode helper.
func (ts *TurboSync) decideMode() {
	if ts == nil || ts.chain == nil {
		return
	}
	// `lag` stores the value produced by this operation.
	lag := syncLagBlocks(ts.chain.Height(), ts.currentTargetHeight())
	// `enableTurbo` stores the value produced by this operation.
	enableTurbo := lag > turboSyncReplayThreshold

	ts.mu.Lock()
	// `changed` stores the value produced by this operation.
	changed := ts.turboMode != enableTurbo
	ts.turboMode = enableTurbo
	ts.mu.Unlock()

	if changed && enableTurbo {
		log.Println("[SYNC-TURBO] turbo mode enabled")
	}
}

// selectPeers implements the select peers helper.
func (ts *TurboSync) selectPeers() {
	if ts == nil {
		return
	}

	ts.mu.Lock()
	// `peers` stores the value produced by this operation.
	peers := append([]peer.ID{}, ts.peers...)
	// `peerFailures` stores the result produced by this operation.
	peerFailures := make(map[string]int, len(ts.peerFailures))
	// `key` and `value` track the key used to access the related value.
	for key, value := range ts.peerFailures {
		peerFailures[key] = value
	}
	ts.mu.Unlock()

	// `active` stores the value produced by this operation.
	active := make([]peer.ID, 0, len(peers))
	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		// `peerID` stores the value produced by this operation.
		peerID := strings.TrimSpace(pid.String())
		if peerID == "" {
			continue
		}
		if peerFailures[peerID] >= turboSyncPeerFailBudget {
			continue
		}
		if ts.node != nil {
			// `score` stores the value produced by this operation.
			score := ts.node.syncPeerScoreValue(peerID)
			// `connected` stores the value produced by this operation.
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

	ts.mu.Lock()
	ts.activePeers = append([]peer.ID{}, active...)
	if len(ts.activePeers) > 0 {
		// `found` stores whether the related condition is satisfied.
		found := false
		// `pid` tracks the current values while iterating.
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

// parallelDownload implements the parallel download helper.
func (ts *TurboSync) parallelDownload(start uint64, end uint64) {
	defer ts.finishDownload()
	if ts == nil || ts.node == nil || ts.chain == nil || start == 0 || end < start {
		return
	}

	// `assignments` stores the value produced by this operation.
	assignments := planTurboAssignments(start, end, ts.currentActivePeers())
	if len(assignments) == 0 {
		return
	}

	// `wg` stores the value used by this operation.
	var wg sync.WaitGroup
	// `assignment` tracks the current values while iterating.
	for _, assignment := range assignments {
		wg.Add(1)
		// `assignment` stores the value produced by this operation.
		assignment := assignment
		go func() {
			defer wg.Done()
			ts.downloadRange(assignment.Peer, assignment.From, assignment.To)
		}()
	}
	wg.Wait()
}

// downloadRange implements the download range helper.
func (ts *TurboSync) downloadRange(provider peer.ID, start uint64, end uint64) {
	if ts == nil || ts.node == nil || start == 0 || end < start {
		return
	}

	// `perRequestMax` stores the value produced by this operation.
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

	// `cursor` stores the value produced by this operation.
	for cursor := start; cursor <= end; {
		// `chunkEnd` stores the value produced by this operation.
		chunkEnd := cursor + perRequestMax - 1
		if chunkEnd < cursor || chunkEnd > end {
			chunkEnd = end
		}

		// `started` stores the value produced by this operation.
		started := time.Now()
		ts.node.setSyncProvider(provider.String())
		// `blocks` and `err` store the error produced by this operation.
		blocks, _, _, err := ts.node.requestBlocksFromPeer(provider, cursor, chunkEnd, false, 0)
		if err != nil || len(blocks) == 0 {
			ts.node.recordSyncPeerBlockResult(provider.String(), false, time.Since(started), 0, isSyncTimeoutError(err))
			// `failCount` stores the measured quantity used by this operation.
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
		// `block` tracks the synchronization state protecting shared data.
		for _, block := range blocks {
			ts.blockQueue <- block
		}

		// `lastHeight` stores the value produced by this operation.
		lastHeight := blocks[len(blocks)-1].ID
		if lastHeight < cursor {
			return
		}
		cursor = lastHeight + 1
	}
}

// applyWorker applies worker.
func (ts *TurboSync) applyWorker() {
	if ts == nil || ts.chain == nil {
		return
	}

	// `pending` stores the value produced by this operation.
	pending := make(map[uint64]Block)
	// `block` tracks the synchronization state protecting shared data.
	for block := range ts.blockQueue {
		if block.ID == 0 {
			continue
		}
		pending[block.ID] = block

		for {
			// `next` stores the value produced by this operation.
			next := ts.chain.Height() + 1
			// `candidate` and `ok` store whether the related condition is satisfied.
			candidate, ok := pending[next]
			if !ok {
				break
			}

			// `err` stores the error produced by this operation.
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

// applyBlock applies block.
func (ts *TurboSync) applyBlock(block Block) error {
	if ts == nil || ts.node == nil || ts.chain == nil {
		return errors.New("turbo_sync_uninitialized")
	}
	if ts.detectFork(block) {
		return newBlockApplyError(block, "parent_mismatch", nil)
	}

	// `beforeHeight` stores the value produced by this operation.
	beforeHeight := ts.chain.Height()
	ts.node.applyMu.Lock()
	// `err` stores the error produced by this operation.
	err := ts.node.ReceiveBlock(block, ts.chain)
	ts.node.ProcessQueuedBlocks()
	// `afterHeight` stores the value produced by this operation.
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

// updateTip implements the update tip helper.
func (ts *TurboSync) updateTip(height uint64) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	// `prev` stores the value produced by this operation.
	prev := ts.lastTipHeight
	ts.lastTipHeight = height
	ts.mu.Unlock()
	log.Printf("[HEIGHT] %d -> %d", prev, height)
	if ts.node != nil {
		ts.node.noteSyncProgress(height)
	}
}

// detectFork implements the detect fork helper.
func (ts *TurboSync) detectFork(block Block) bool {
	if ts == nil || ts.chain == nil {
		return false
	}
	// `last` stores the value produced by this operation.
	last := ts.chain.LastBlock()
	// `expectedPrev` stores the value produced by this operation.
	expectedPrev := strings.TrimSpace(last.BlockHash)
	// `gotPrev` stores the value produced by this operation.
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

// recoverFork implements the recover fork helper.
func (ts *TurboSync) recoverFork() {
	if ts == nil || ts.chain == nil {
		return
	}
	log.Printf("[SYNC-TURBO] fork recovery local=%d target=%d action=provider_rotate_snapshot",
		ts.chain.Height(), ts.currentTargetHeight())
	ts.rotateProvider()
	ts.snapshotSync()
}

// snapshotSync implements the snapshot sync helper.
func (ts *TurboSync) snapshotSync() {
	if ts == nil || ts.node == nil || ts.chain == nil {
		return
	}
	// `target` stores the value produced by this operation.
	target := ts.currentTargetHeight()
	if target == 0 || target <= ts.chain.Height() {
		return
	}
	log.Printf("[SYNC-TURBO] snapshot sync target=%d local=%d", target, ts.chain.Height())
	ts.node.forceSnapshotSyncToHeight(target, "turbo_sync_snapshot")
	ts.recordProgress()
}

// createSnapshot implements the create snapshot helper.
func (ts *TurboSync) createSnapshot(height uint64) {
	if ts == nil || ts.node == nil || height == 0 {
		return
	}
	// `snap` and `err` store the error produced by this operation.
	if snap, err := ts.node.GetSnapshot(height); err == nil && snap != nil {
		return
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := ts.chain.GetBlock(height)
	if !ok || strings.TrimSpace(block.BlockHash) == "" {
		return
	}
	// `err` stores the error produced by this operation.
	if err := ts.node.CreateSnapshot(height, strings.TrimSpace(block.BlockHash)); err != nil {
		log.Printf("[SYNC-TURBO] snapshot create failed height=%d err=%v", height, err)
		return
	}
	log.Printf("[SNAPSHOT] created height=%d", height)
}

// recordProgress implements the record progress helper.
func (ts *TurboSync) recordProgress() {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	ts.lastProgress = time.Now()
	ts.mu.Unlock()
}

// detectStall implements the detect stall helper.
func (ts *TurboSync) detectStall() bool {
	if ts == nil {
		return false
	}
	ts.mu.Lock()
	// `last` stores the value produced by this operation.
	last := ts.lastProgress
	ts.mu.Unlock()
	return time.Since(last) > turboSyncStallTimeout
}

// recordFailure implements the record failure helper.
func (ts *TurboSync) recordFailure(hash string) bool {
	if ts == nil || strings.TrimSpace(hash) == "" {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.applyFailures[hash]++
	return ts.applyFailures[hash] > turboSyncApplyFailBudget
}

// clearFailure implements the clear failure helper.
func (ts *TurboSync) clearFailure(hash string) {
	if ts == nil || strings.TrimSpace(hash) == "" {
		return
	}
	ts.mu.Lock()
	delete(ts.applyFailures, hash)
	ts.mu.Unlock()
}

// recordPeerFailure implements the record peer failure helper.
func (ts *TurboSync) recordPeerFailure(peerID string) int {
	if ts == nil || strings.TrimSpace(peerID) == "" {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.peerFailures[peerID]++
	return ts.peerFailures[peerID]
}

// clearPeerFailure implements the clear peer failure helper.
func (ts *TurboSync) clearPeerFailure(peerID string) {
	if ts == nil || strings.TrimSpace(peerID) == "" {
		return
	}
	ts.mu.Lock()
	delete(ts.peerFailures, peerID)
	ts.mu.Unlock()
}

// rotateProvider implements the rotate provider helper.
func (ts *TurboSync) rotateProvider() {
	if ts == nil {
		return
	}

	// `active` stores the value produced by this operation.
	active := ts.currentActivePeers()
	// `current` stores the value produced by this operation.
	current := ts.currentProvider()
	if len(active) == 0 {
		ts.selectPeers()
		active = ts.currentActivePeers()
	}
	if len(active) == 0 {
		return
	}

	// `best` stores the value produced by this operation.
	best := current
	// `bestScore` stores the value produced by this operation.
	bestScore := -1e18
	// `bestReputation` stores the value produced by this operation.
	bestReputation := -1.0
	// `found` stores whether the related condition is satisfied.
	found := false
	// `pid` tracks the current values while iterating.
	for _, pid := range active {
		if pid == "" {
			continue
		}
		if current != "" && pid == current && len(active) > 1 {
			continue
		}
		// `reputation` stores the value produced by this operation.
		reputation := 0.5
		// `score` stores the value produced by this operation.
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

// getNetworkHeight implements the get network height helper.
func (ts *TurboSync) getNetworkHeight() uint64 {
	if ts == nil || ts.chain == nil {
		return 0
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := ts.chain.Height()
	if ts.node == nil {
		return localHeight
	}
	// `quorumHeight`, `required`, and `quorumOK` store whether the related condition is satisfied.
	quorumHeight, _, required, quorumOK := ts.node.majorityHeartbeatHeight()
	// `observedHeight` and `observedVotes` store the value produced by this operation.
	observedHeight, observedVotes := ts.node.bestObservedSyncHeight()
	// `target` stores the value produced by this operation.
	target := selectSyncTargetHeight(localHeight, quorumHeight, quorumOK, observedHeight, observedVotes, required)
	if target < localHeight {
		target = localHeight
	}
	return target
}

// computeTurboWindow computes turbo window.
func (ts *TurboSync) computeTurboWindow(lag uint64) uint64 {
	// `window` stores the value produced by this operation.
	window := computeAdaptiveSyncFetchWindow(lag)
	if window == 0 {
		window = syncBlockRequestMaxBlocks(0)
	}
	// `activeCount` stores the measured quantity used by this operation.
	activeCount := uint64(len(ts.currentActivePeers()))
	if activeCount > 1 {
		// `minParallel` stores the value produced by this operation.
		minParallel := activeCount * syncBlockRequestMaxBlocks(0)
		if window < minParallel {
			window = minParallel
		}
	}
	return window
}

// beginDownload implements the begin download helper.
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

// finishDownload implements the finish download helper.
func (ts *TurboSync) finishDownload() {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	ts.downloadInFlight = false
	ts.mu.Unlock()
}

// currentProvider returns current provider.
func (ts *TurboSync) currentProvider() peer.ID {
	if ts == nil {
		return ""
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.provider
}

// currentActivePeers returns current active peers.
func (ts *TurboSync) currentActivePeers() []peer.ID {
	if ts == nil {
		return nil
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]peer.ID{}, ts.activePeers...)
}

// currentTargetHeight returns current target height.
func (ts *TurboSync) currentTargetHeight() uint64 {
	if ts == nil {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.targetHeight
}

// isTurboMode implements the is turbo mode helper.
func (ts *TurboSync) isTurboMode() bool {
	if ts == nil {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.turboMode
}

// currentPeerFailures returns current peer failures.
func (ts *TurboSync) currentPeerFailures(peerID string) int {
	if ts == nil || strings.TrimSpace(peerID) == "" {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.peerFailures[peerID]
}

type turboRangeAssignment struct {
	// `Peer` stores the value associated with this record.
	Peer peer.ID
	// `From` stores the value associated with this record.
	From uint64
	// `To` stores the value associated with this record.
	To   uint64
}

// planTurboAssignments implements the plan turbo assignments helper.
func planTurboAssignments(start uint64, end uint64, peers []peer.ID) []turboRangeAssignment {
	if start == 0 || end < start || len(peers) == 0 {
		return nil
	}

	// `total` stores the measured quantity used by this operation.
	total := end - start + 1
	// `chunk` stores the value produced by this operation.
	chunk := total / uint64(len(peers))
	// `remainder` stores the value produced by this operation.
	remainder := total % uint64(len(peers))
	if chunk == 0 {
		chunk = 1
	}

	// `assignments` stores the value produced by this operation.
	assignments := make([]turboRangeAssignment, 0, len(peers))
	// `cursor` stores the value produced by this operation.
	cursor := start
	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		if cursor > end {
			break
		}
		// `size` stores the measured quantity used by this operation.
		size := chunk
		if remainder > 0 {
			size++
			remainder--
		}
		// `to` stores the value produced by this operation.
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
