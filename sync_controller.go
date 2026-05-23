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
	syncControllerQueueCapacity = 15000
	syncControllerLoopInterval  = 300 * time.Millisecond
	syncControllerStallTimeout  = 30 * time.Second
	syncControllerFailureBudget = 5
)

// SyncController coordinates download/apply orchestration while delegating
// execution, validation, registry persistence, and commit behavior to Node.
type SyncController struct {
	node *Node

	chain    *Blockchain
	peers    []peer.ID
	provider peer.ID

	blockQueue    chan Block
	applyFailures map[string]int
	lastProgress  time.Time
	targetHeight  uint64

	mu               sync.Mutex
	applyOnce        sync.Once
	downloadInFlight bool
}

func NewSyncController(node *Node, peers []peer.ID) *SyncController {
	var chain *Blockchain
	if node != nil {
		chain = node.Blockchain
	}
	controllerPeers := append([]peer.ID{}, peers...)
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

func (sc *SyncController) Start() {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return
	}
	sc.applyOnce.Do(func() {
		go sc.applyWorker()
	})

	ticker := time.NewTicker(syncControllerLoopInterval)
	defer ticker.Stop()

	for {
		local := sc.chain.Height()
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

		lag := syncLagBlocks(local, target)
		if sc.detectStall() {
			log.Printf("[SYNC-CONTROLLER] stall detected local=%d target=%d", local, target)
			sc.rotateProvider()
		}
		if sc.maybeSnapshotSync(lag) {
			<-ticker.C
			continue
		}

		start := local + 1
		batch := sc.computeBatchSize(lag)
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

func (sc *SyncController) downloadBatch(start uint64, end uint64) {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return
	}
	defer sc.finishDownload()
	if start == 0 || end < start {
		return
	}

	perRequestMax := syncBlockRequestMaxBlocks(0)
	if perRequestMax == 0 {
		return
	}

	cursor := start
	for cursor <= end {
		provider := sc.currentProvider()
		if provider == "" {
			sc.rotateProvider()
			provider = sc.currentProvider()
			if provider == "" {
				return
			}
		}

		chunkEnd := cursor + perRequestMax - 1
		if chunkEnd < cursor || chunkEnd > end {
			chunkEnd = end
		}

		started := time.Now()
		sc.node.setSyncProvider(provider.String())
		blocks, _, _, err := sc.node.requestBlocksFromPeer(provider, cursor, chunkEnd, false, 0)
		if err != nil || len(blocks) == 0 {
			sc.node.recordSyncPeerBlockResult(provider.String(), false, time.Since(started), 0, isSyncTimeoutError(err))
			log.Printf("[SYNC-CONTROLLER] download failed provider=%s range=%d-%d err=%v",
				ShortID(provider.String()), cursor, chunkEnd, err)
			sc.rotateProvider()
			return
		}

		sc.node.recordSyncPeerBlockResult(provider.String(), true, time.Since(started), len(MustJSON(blocks)), false)
		for _, block := range blocks {
			sc.blockQueue <- block
		}

		lastHeight := blocks[len(blocks)-1].ID
		if lastHeight < cursor {
			return
		}
		cursor = lastHeight + 1
	}
}

func (sc *SyncController) applyWorker() {
	if sc == nil || sc.chain == nil {
		return
	}
	for block := range sc.blockQueue {
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

func (sc *SyncController) applyBlock(block Block) error {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return errors.New("sync_controller_uninitialized")
	}

	expectedHeight := sc.chain.Height() + 1
	if block.ID != expectedHeight {
		return newBlockApplyError(block, "invalid_height",
			fmt.Errorf("expected_next=%d got=%d", expectedHeight, block.ID))
	}

	last := sc.chain.LastBlock()
	if last.ID > 0 &&
		strings.TrimSpace(last.BlockHash) != "" &&
		!strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
		return newBlockApplyError(block, "parent_mismatch", nil)
	}

	sc.node.applyMu.Lock()
	err := sc.node.ReceiveBlock(block, sc.chain)
	sc.node.ProcessQueuedBlocks()
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

func (sc *SyncController) updateTip(height uint64) {
	if sc == nil {
		return
	}
	if sc.node != nil && height > 0 {
		sc.node.noteSyncProgress(height)
	}
	nodeID := ""
	if sc.node != nil {
		nodeID = ShortID(sc.node.ID)
	}
	log.Printf("[HEIGHT] node=%s updated -> %d", nodeID, height)
}

func (sc *SyncController) recordFailure(hash string) bool {
	if sc == nil || strings.TrimSpace(hash) == "" {
		return false
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.applyFailures[hash]++
	return sc.applyFailures[hash] > syncControllerFailureBudget
}

func (sc *SyncController) clearFailure(hash string) {
	if sc == nil || strings.TrimSpace(hash) == "" {
		return
	}
	sc.mu.Lock()
	delete(sc.applyFailures, hash)
	sc.mu.Unlock()
}

func (sc *SyncController) rotateProvider() {
	if sc == nil {
		return
	}

	sc.mu.Lock()
	current := sc.provider
	peers := append([]peer.ID{}, sc.peers...)
	sc.mu.Unlock()

	if len(peers) == 0 {
		return
	}

	best := current
	bestScore := math.Inf(-1)
	bestReputation := math.Inf(-1)
	bestConnected := false
	found := false

	for _, pid := range peers {
		if strings.TrimSpace(pid.String()) == "" {
			continue
		}
		if pid == current && len(peers) > 1 {
			continue
		}

		connected := true
		reputation := 0.5
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

func (sc *SyncController) maybeSnapshotSync(lag uint64) bool {
	if sc == nil || lag <= 5000 {
		return false
	}
	log.Printf("[SYNC-CONTROLLER] escalating to snapshot sync local=%d target=%d lag=%d",
		sc.chain.Height(), sc.currentTargetHeight(), lag)
	sc.startSnapshotSync()
	return true
}

func (sc *SyncController) startSnapshotSync() {
	if sc == nil || sc.node == nil || sc.chain == nil {
		return
	}
	target := sc.currentTargetHeight()
	if target == 0 || target <= sc.chain.Height() {
		return
	}
	sc.node.forceSnapshotSyncToHeight(target, "sync_controller_snapshot")
	sc.recordProgress()
}

func (sc *SyncController) createSnapshot(height uint64) {
	if sc == nil || sc.node == nil || sc.chain == nil || height == 0 {
		return
	}
	if snap, err := sc.node.GetSnapshot(height); err == nil && snap != nil {
		return
	}
	block, ok := sc.chain.GetBlock(height)
	if !ok || strings.TrimSpace(block.BlockHash) == "" {
		return
	}
	if err := sc.node.CreateSnapshot(height, strings.TrimSpace(block.BlockHash)); err != nil {
		log.Printf("[SYNC-CONTROLLER] snapshot create failed height=%d err=%v", height, err)
		return
	}
	log.Printf("[SNAPSHOT] created height=%d", height)
}

func (sc *SyncController) recordProgress() {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	sc.lastProgress = time.Now()
	sc.mu.Unlock()
}

func (sc *SyncController) detectStall() bool {
	if sc == nil {
		return false
	}
	sc.mu.Lock()
	last := sc.lastProgress
	sc.mu.Unlock()
	return time.Since(last) > syncControllerStallTimeout
}

func (sc *SyncController) getNetworkHeight() uint64 {
	if sc == nil || sc.chain == nil {
		return 0
	}
	localHeight := sc.chain.Height()
	if sc.node == nil {
		return localHeight
	}

	quorumHeight, _, required, quorumOK := sc.node.majorityHeartbeatHeight()
	observedHeight, observedVotes := sc.node.bestObservedSyncHeight()
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

func (sc *SyncController) currentTargetHeight() uint64 {
	if sc == nil {
		return 0
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.targetHeight
}

func (sc *SyncController) currentProvider() peer.ID {
	if sc == nil {
		return ""
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.provider
}

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

func (sc *SyncController) finishDownload() {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	sc.downloadInFlight = false
	sc.mu.Unlock()
}
