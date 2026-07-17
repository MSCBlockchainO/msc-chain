package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// `syncControllerQueueCapacity` defines the constant value used by this package.
	syncControllerQueueCapacity = 15000
	// `syncControllerLoopInterval` defines the value currently being processed.
	syncControllerLoopInterval = 300 * time.Millisecond
	// `syncControllerStallTimeout` defines the result produced by this operation.
	syncControllerStallTimeout = 30 * time.Second
	// `syncControllerFailureBudget` defines the constant value used by this package.
	syncControllerFailureBudget = 5
)

// SyncController coordinates download/apply orchestration while delegating
// execution, validation, registry persistence, and commit behavior to Node.
type SyncController struct {
	// `node` stores the value associated with this record.
	node *Node

	// `chain` stores the value associated with this record.
	chain *Blockchain
	// `peers` stores the value associated with this record.
	peers []peer.ID
	// `provider` stores the value associated with this record.
	provider peer.ID

	// `blockQueue` stores the block data handled by this operation.
	blockQueue chan Block
	// `applyFailures` stores the result produced by this operation.
	applyFailures map[string]int
	// `lastProgress` stores the value associated with this record.
	lastProgress time.Time
	// `targetHeight` stores the value associated with this record.
	targetHeight uint64

	// `mu` stores the synchronization state protecting shared data.
	mu sync.Mutex
	// `applyOnce` stores the value associated with this record.
	applyOnce sync.Once
	// `downloadInFlight` stores the value associated with this record.
	downloadInFlight bool
}

// NewSyncController creates a new sync controller.
func NewSyncController(node *Node, peers []peer.ID) *SyncController {
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
	return &SyncController{
		node:          node,
		chain:         chain,
		peers:         controllerPeers,
		provider:      provider,
		blockQueue:    make(chan Block, syncControllerQueueCapacity),
		applyFailures: make(map[string]int),
		lastProgress:  time.Now(),
	}
}

// Start implements the start helper.
func (sc *SyncController) Start() {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return
	}
	sc.applyOnce.Do(func() {
		go sc.applyWorker()
	})

	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(syncControllerLoopInterval)
	defer ticker.Stop()

	for {
		// `local` stores the value produced by this operation.
		local := sc.chain.Height()
		// `target` stores the value produced by this operation.
		target := sc.getNetworkHeight()

		sc.mu.Lock()
		sc.targetHeight = target
		sc.mu.Unlock()

		if target == 0 {
			<-ticker.C
			continue
		}
		if local >= target {
			log.Printf("[SYNC-CONTROLLER] completed local=%d target=%d", local, target)
			return
		}

		// `lag` stores the value produced by this operation.
		lag := syncLagBlocks(local, target)
		if sc.detectStall() {
			log.Printf("[SYNC-CONTROLLER] stall detected local=%d target=%d", local, target)
			sc.rotateProvider()
		}
		if sc.maybeSnapshotSync(lag) {
			<-ticker.C
			continue
		}

		// `start` stores the value produced by this operation.
		start := local + 1
		// `batch` stores the value produced by this operation.
		batch := sc.computeBatchSize(lag)
		// `end` stores the value produced by this operation.
		end := start + batch - 1
		if end < start || end > target {
			end = target
		}

		if sc.beginDownload() {
			go sc.downloadBatch(start, end)
		}

		<-ticker.C
	}
}

// computeBatchSize computes batch size.
func (sc *SyncController) computeBatchSize(lag uint64) uint64 {
	switch {
	case lag > 20000:
		return 4096
	case lag > 5000:
		return 2048
	case lag > 1000:
		return 1024
	case lag > 200:
		return 512
	default:
		return 128
	}
}

// downloadBatch implements the download batch helper.
func (sc *SyncController) downloadBatch(start uint64, end uint64) {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return
	}
	defer sc.finishDownload()
	if start == 0 || end < start {
		return
	}

	// `perRequestMax` stores the value produced by this operation.
	perRequestMax := syncBlockRequestMaxBlocks(0)
	if perRequestMax == 0 {
		return
	}

	// `cursor` stores the value produced by this operation.
	cursor := start
	for cursor <= end {
		// `chunkEnd` stores the value produced by this operation.
		chunkEnd := cursor + perRequestMax - 1
		if chunkEnd < cursor || chunkEnd > end {
			chunkEnd = end
		}
		// `provider` stores the value produced by this operation.
		provider := sc.providerForRange(chunkEnd)
		if provider == "" {
			sc.rotateProvider()
			provider = sc.providerForRange(chunkEnd)
			if provider == "" {
				return
			}
		}

		// `started` stores the value produced by this operation.
		started := time.Now()
		sc.node.setSyncProvider(provider.String())
		// `blocks` and `err` store the error produced by this operation.
		blocks, _, _, err := sc.node.requestBlocksFromPeer(provider, cursor, chunkEnd, false, 0)
		if err != nil || len(blocks) == 0 {
			sc.node.recordSyncPeerBlockResult(provider.String(), false, time.Since(started), 0, isSyncTimeoutError(err))
			log.Printf("[SYNC-CONTROLLER] download failed provider=%s range=%d-%d err=%v",
				ShortID(provider.String()), cursor, chunkEnd, err)
			sc.rotateProvider()
			return
		}

		sc.node.recordSyncPeerBlockResult(provider.String(), true, time.Since(started), len(MustJSON(blocks)), false)
		// `block` tracks the synchronization state protecting shared data.
		for _, block := range blocks {
			sc.blockQueue <- block
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
func (sc *SyncController) applyWorker() {
	if sc == nil || sc.chain == nil {
		return
	}
	// `block` tracks the synchronization state protecting shared data.
	for block := range sc.blockQueue {
		// `err` stores the error produced by this operation.
		err := sc.applyBlock(block)
		if err != nil {
			log.Printf("[SYNC-CONTROLLER] apply failed: %v", err)
			if sc.recordFailure(strings.TrimSpace(block.BlockHash)) {
				log.Printf("[SYNC-CONTROLLER] repeated failure h=%d hash=%s action=provider_rotate",
					block.ID, ShortHash(block.BlockHash))
				sc.rotateProvider()
			}
			continue
		}

		sc.clearFailure(strings.TrimSpace(block.BlockHash))
		sc.updateTip(sc.chain.Height())
		sc.recordProgress()
	}
}

// applyBlock applies block.
func (sc *SyncController) applyBlock(block Block) error {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return errors.New("sync_controller_uninitialized")
	}

	// `expectedHeight` stores the value produced by this operation.
	expectedHeight := sc.chain.Height() + 1
	if block.ID != expectedHeight {
		return newBlockApplyError(block, "invalid_height",
			fmt.Errorf("expected_next=%d got=%d", expectedHeight, block.ID))
	}

	// `last` stores the value produced by this operation.
	last := sc.chain.LastBlock()
	if last.ID > 0 &&
		strings.TrimSpace(last.BlockHash) != "" &&
		!strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
		return newBlockApplyError(block, "parent_mismatch", nil)
	}

	sc.node.applyMu.Lock()
	// `err` stores the error produced by this operation.
	err := sc.node.ReceiveBlock(block, sc.chain)
	sc.node.ProcessQueuedBlocks()
	// `appliedHeight` stores the value produced by this operation.
	appliedHeight := sc.chain.Height()
	sc.node.applyMu.Unlock()

	if err != nil {
		return err
	}
	if appliedHeight < block.ID {
		return newBlockApplyError(block, "execution_failed", errors.New("commit_incomplete"))
	}

	if shouldAutoCreateSnapshotAtHeight(appliedHeight) {
		sc.createSnapshot(appliedHeight)
	}
	return nil
}

// updateTip implements the update tip helper.
func (sc *SyncController) updateTip(height uint64) {
	if sc == nil {
		return
	}
	if sc.node != nil && height > 0 {
		sc.node.noteSyncProgress(height)
	}
	// `nodeID` stores the value produced by this operation.
	nodeID := ""
	if sc.node != nil {
		nodeID = ShortID(sc.node.ID)
	}
	log.Printf("[HEIGHT] node=%s updated -> %d", nodeID, height)
}

// recordFailure implements the record failure helper.
func (sc *SyncController) recordFailure(hash string) bool {
	if sc == nil || strings.TrimSpace(hash) == "" {
		return false
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.applyFailures[hash]++
	return sc.applyFailures[hash] > syncControllerFailureBudget
}

// clearFailure implements the clear failure helper.
func (sc *SyncController) clearFailure(hash string) {
	if sc == nil || strings.TrimSpace(hash) == "" {
		return
	}
	sc.mu.Lock()
	delete(sc.applyFailures, hash)
	sc.mu.Unlock()
}

// rotateProvider implements the rotate provider helper.
func (sc *SyncController) rotateProvider() {
	if sc == nil {
		return
	}

	sc.mu.Lock()
	// `current` stores the value produced by this operation.
	current := sc.provider
	// `peers` stores the value produced by this operation.
	peers := append([]peer.ID{}, sc.peers...)
	sc.mu.Unlock()

	if len(peers) == 0 {
		return
	}

	// `best` stores the value produced by this operation.
	best := current
	// `bestScore` stores the value produced by this operation.
	bestScore := math.Inf(-1)
	// `bestReputation` stores the value produced by this operation.
	bestReputation := math.Inf(-1)
	// `bestConnected` stores the value produced by this operation.
	bestConnected := false
	// `found` stores whether the related condition is satisfied.
	found := false

	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		if strings.TrimSpace(pid.String()) == "" {
			continue
		}
		if pid == current && len(peers) > 1 {
			continue
		}

		// `connected` stores the value produced by this operation.
		connected := true
		// `reputation` stores the value produced by this operation.
		reputation := 0.5
		// `score` stores the value produced by this operation.
		score := 0.0
		if sc.node != nil {
			connected = sc.node.isPeerConnected(pid.String())
			reputation = sc.node.syncPeerReputationValue(pid.String())
			score = sc.node.syncPeerScoreValue(pid.String())
		}

		if !found ||
			(connected && !bestConnected) ||
			(connected == bestConnected && reputation > bestReputation) ||
			(connected == bestConnected && reputation == bestReputation && score > bestScore) {
			best = pid
			bestReputation = reputation
			bestScore = score
			bestConnected = connected
			found = true
		}
	}

	if !found && current == "" {
		best = peers[0]
	}
	if strings.TrimSpace(best.String()) == "" {
		return
	}

	sc.mu.Lock()
	sc.provider = best
	sc.mu.Unlock()

	if sc.node != nil {
		if strings.TrimSpace(current.String()) != "" && current != best {
			sc.node.setSyncAvoidProviderOnce(current.String())
		}
		sc.node.setSyncProvider(best.String())
	}

	log.Printf("[SYNC-CONTROLLER] provider switched=%s previous=%s",
		ShortID(best.String()), ShortID(current.String()))
}

// maybeSnapshotSync implements the maybe snapshot sync helper.
func (sc *SyncController) maybeSnapshotSync(lag uint64) bool {
	if sc == nil || lag <= 5000 {
		return false
	}
	log.Printf("[SYNC-CONTROLLER] escalating to snapshot sync local=%d target=%d lag=%d",
		sc.chain.Height(), sc.currentTargetHeight(), lag)
	sc.startSnapshotSync()
	return true
}

// startSnapshotSync implements the start snapshot sync helper.
func (sc *SyncController) startSnapshotSync() {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return
	}
	// `target` stores the value produced by this operation.
	target := sc.currentTargetHeight()
	if target == 0 || target <= sc.chain.Height() {
		return
	}
	sc.node.forceSnapshotSyncToHeight(target, "sync_controller_snapshot")
	sc.recordProgress()
}

// createSnapshot implements the create snapshot helper.
func (sc *SyncController) createSnapshot(height uint64) {
	if sc == nil || sc.node == nil || sc.chain == nil || height == 0 {
		return
	}
	// `snap` and `err` store the error produced by this operation.
	if snap, err := sc.node.GetSnapshot(height); err == nil && snap != nil {
		return
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := sc.chain.GetBlock(height)
	if !ok || strings.TrimSpace(block.BlockHash) == "" {
		return
	}
	// `err` stores the error produced by this operation.
	if err := sc.node.CreateSnapshot(height, strings.TrimSpace(block.BlockHash)); err != nil {
		log.Printf("[SYNC-CONTROLLER] snapshot create failed height=%d err=%v", height, err)
		return
	}
	log.Printf("[SNAPSHOT] created height=%d", height)
}

// recordProgress implements the record progress helper.
func (sc *SyncController) recordProgress() {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	sc.lastProgress = time.Now()
	sc.mu.Unlock()
}

// detectStall implements the detect stall helper.
func (sc *SyncController) detectStall() bool {
	if sc == nil {
		return false
	}
	sc.mu.Lock()
	// `last` stores the value produced by this operation.
	last := sc.lastProgress
	sc.mu.Unlock()
	return time.Since(last) > syncControllerStallTimeout
}

// getNetworkHeight implements the get network height helper.
func (sc *SyncController) getNetworkHeight() uint64 {
	if sc == nil || sc.chain == nil {
		return 0
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := sc.chain.Height()
	if sc.node == nil {
		return localHeight
	}

	// `quorumHeight`, `required`, and `quorumOK` store whether the related condition is satisfied.
	quorumHeight, _, required, quorumOK := sc.node.majorityHeartbeatHeight()
	// `observedHeight` and `observedVotes` store the value produced by this operation.
	observedHeight, observedVotes := sc.node.bestObservedSyncHeight()
	// `target` stores the value produced by this operation.
	target := selectSyncTargetHeight(localHeight, quorumHeight, quorumOK, observedHeight, observedVotes, required)
	if target == 0 {
		sc.mu.Lock()
		if sc.targetHeight > localHeight {
			target = sc.targetHeight
		}
		sc.mu.Unlock()
	}
	if target < localHeight {
		target = localHeight
	}
	return target
}

// currentTargetHeight returns current target height.
func (sc *SyncController) currentTargetHeight() uint64 {
	if sc == nil {
		return 0
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.targetHeight
}

// currentProvider returns current provider.
func (sc *SyncController) currentProvider() peer.ID {
	if sc == nil {
		return ""
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.provider
}

// providerForRange chooses a provider that has advertised enough height for the requested range.
func (sc *SyncController) providerForRange(targetHeight uint64) peer.ID {
	if sc == nil {
		return ""
	}
	sc.mu.Lock()
	current := sc.provider
	peers := append([]peer.ID{}, sc.peers...)
	sc.mu.Unlock()
	if sc.providerCanServe(current, targetHeight) {
		return current
	}
	best := peer.ID("")
	bestAck := uint64(0)
	bestReputation := math.Inf(-1)
	bestScore := math.Inf(-1)
	bestConnected := false
	for _, pid := range peers {
		if strings.TrimSpace(pid.String()) == "" {
			continue
		}
		ack := uint64(0)
		connected := true
		reputation := 0.5
		score := 0.0
		if sc.node != nil {
			ack = sc.node.peerAckHeightFor(pid.String())
			connected = sc.node.isPeerConnected(pid.String())
			reputation = sc.node.syncPeerReputationValue(pid.String())
			score = sc.node.syncPeerScoreValue(pid.String())
		}
		if targetHeight > 0 && ack > 0 && ack < targetHeight {
			continue
		}
		if best == "" ||
			(ack > bestAck) ||
			(ack == bestAck && connected && !bestConnected) ||
			(ack == bestAck && connected == bestConnected && reputation > bestReputation) ||
			(ack == bestAck && connected == bestConnected && reputation == bestReputation && score > bestScore) {
			best = pid
			bestAck = ack
			bestReputation = reputation
			bestScore = score
			bestConnected = connected
		}
	}
	if best != "" && best != current {
		sc.mu.Lock()
		sc.provider = best
		sc.mu.Unlock()
		if sc.node != nil {
			if strings.TrimSpace(current.String()) != "" {
				sc.node.setSyncAvoidProviderOnce(current.String())
			}
			sc.node.setSyncProvider(best.String())
		}
		log.Printf("[SYNC-CONTROLLER] provider switched=%s previous=%s reason=height_capable target=%d",
			ShortID(best.String()), ShortID(current.String()), targetHeight)
	}
	return best
}

// providerCanServe reports whether a provider has advertised enough height for the requested range.
func (sc *SyncController) providerCanServe(provider peer.ID, targetHeight uint64) bool {
	if strings.TrimSpace(provider.String()) == "" {
		return false
	}
	if sc.node == nil || targetHeight == 0 {
		return true
	}
	ack := sc.node.peerAckHeightFor(provider.String())
	return ack == 0 || ack >= targetHeight
}

// beginDownload implements the begin download helper.
func (sc *SyncController) beginDownload() bool {
	if sc == nil {
		return false
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.downloadInFlight {
		return false
	}
	sc.downloadInFlight = true
	return true
}

// finishDownload implements the finish download helper.
func (sc *SyncController) finishDownload() {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	sc.downloadInFlight = false
	sc.mu.Unlock()
}
