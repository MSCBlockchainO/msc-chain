package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	lpcrypto "github.com/libp2p/go-libp2p/core/crypto"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	libp2pyamux "github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	noisesec "github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
	libp2pwebsocket "github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multihash"
	"golang.org/x/time/rate"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	// `peerSuspectTimeout` defines the result produced by this operation.
	peerSuspectTimeout = 2 * time.Minute
	// `peerFlapWindow` defines the constant value used by this package.
	peerFlapWindow = 5 * time.Minute
	// `peerFlapThreshold` defines the constant value used by this package.
	peerFlapThreshold = 5
	// `peerQuarantineFor` defines the constant value used by this package.
	peerQuarantineFor = 5 * time.Minute
	// `peerQuarantineForFlap` defines the constant value used by this package.
	peerQuarantineForFlap = 45 * time.Second
	// `peerQuarantineForProtocol` defines the constant value used by this package.
	peerQuarantineForProtocol = 30 * time.Minute
	// `peerQuarantineForPeerInfoStream` defines the constant value used by this package.
	peerQuarantineForPeerInfoStream = 20 * time.Second
	// `peerQuarantineForMismatch` defines the constant value used by this package.
	peerQuarantineForMismatch = 1 * time.Hour
	// `peerHelloCooldown` defines the constant value used by this package.
	peerHelloCooldown = 30 * time.Second
	// `peerHelloMaxClockSkew` defines the constant value used by this package.
	peerHelloMaxClockSkew = 5 * time.Minute
	// `peerHelloNonceTTL` defines the constant value used by this package.
	peerHelloNonceTTL = 15 * time.Minute
	// `peerHelloNonceSweepInterval` defines the value currently being processed.
	peerHelloNonceSweepInterval = time.Minute
	// `peerHelloNonceStoreFile` defines the constant value used by this package.
	peerHelloNonceStoreFile = "peer_hello_nonces.json"
	// `peerSuspectInterval` defines the value currently being processed.
	peerSuspectInterval = 20 * time.Second
	// `syncCooldown` defines the constant value used by this package.
	syncCooldown = 30 * time.Second
	// `peerMinHold` defines the constant value used by this package.
	peerMinHold = 30 * time.Second
	// `peerGraftCooldown` defines the constant value used by this package.
	peerGraftCooldown = 30 * time.Second
	// `dialBackoffStep1` defines the constant value used by this package.
	dialBackoffStep1 = 5 * time.Second
	// `dialBackoffStep2` defines the constant value used by this package.
	dialBackoffStep2 = 30 * time.Second
	// `dialBackoffStep3` defines the constant value used by this package.
	dialBackoffStep3 = 2 * time.Minute
	// `dialBackoffMax` defines the constant value used by this package.
	dialBackoffMax = 5 * time.Minute
	// `validatorDialBackoffStep1` defines whether the related condition is satisfied.
	validatorDialBackoffStep1 = 5 * time.Second
	// `validatorDialBackoffStep2` defines whether the related condition is satisfied.
	validatorDialBackoffStep2 = 10 * time.Second
	// `validatorDialBackoffStep3` defines whether the related condition is satisfied.
	validatorDialBackoffStep3 = 20 * time.Second
	// `validatorDialBackoffMax` defines whether the related condition is satisfied.
	validatorDialBackoffMax = 40 * time.Second
	// `execVoteReplayTTL` defines the constant value used by this package.
	execVoteReplayTTL = 2 * time.Minute
	// `execVoteStaleIngressTTL` defines the constant value used by this package.
	execVoteStaleIngressTTL = 30 * time.Second
	// `execVoteRebroadcastCooldown` defines the constant value used by this package.
	execVoteRebroadcastCooldown = 5 * time.Second
	// `execVoteRatePerSigner` defines the constant value used by this package.
	execVoteRatePerSigner = 8
	// `execVoteRateBurst` defines the constant value used by this package.
	execVoteRateBurst = 8
	// `execMismatchStrikeWindow` defines the constant value used by this package.
	execMismatchStrikeWindow = 3 * time.Minute
	// `execMismatchQuarantineAt` defines the constant value used by this package.
	execMismatchQuarantineAt = 2
	// `execMismatchSlashAt` defines the constant value used by this package.
	execMismatchSlashAt = 3
	// `invalidProposerStrikeWindow` defines the current position in the related collection.
	invalidProposerStrikeWindow = 3 * time.Minute
	// `invalidProposerStrikeDecayEvery` defines the current position in the related collection.
	invalidProposerStrikeDecayEvery = time.Hour
	// `invalidProposerQuarantineAt` defines the current position in the related collection.
	invalidProposerQuarantineAt = 2
	// `invalidProposerPeerQuarantineAt` defines the current position in the related collection.
	invalidProposerPeerQuarantineAt = 3
	// `execVoteStaleLagBlocks` defines the constant value used by this package.
	execVoteStaleLagBlocks = 2
	// `finalizedDriftThreshold` defines the constant value used by this package.
	finalizedDriftThreshold = 20
	// `finalizedDriftEscalateThreshold` defines the constant value used by this package.
	finalizedDriftEscalateThreshold = 200
	// `finalizedDriftWindow` defines the constant value used by this package.
	finalizedDriftWindow = 10 * time.Minute
	// `finalizedDriftCooldown` defines the constant value used by this package.
	finalizedDriftCooldown = 10 * time.Minute
	// `finalizedDriftDropLogInterval` defines the value currently being processed.
	finalizedDriftDropLogInterval = 30 * time.Second
	// `finalizedDriftNearTipSlack` defines the constant value used by this package.
	finalizedDriftNearTipSlack = 16
	// `finalizedDriftMaxServeRange` defines the constant value used by this package.
	finalizedDriftMaxServeRange = 64
	// `finalizedDriftSnapshotCooldown` defines the constant value used by this package.
	finalizedDriftSnapshotCooldown = 30 * time.Second
	// `blockSyncServeMaxBlocks` defines the block data handled by this operation.
	blockSyncServeMaxBlocks = 512
	// `blockRequestServeMinConcurrency` defines the block data handled by this operation.
	blockRequestServeMinConcurrency = 64
	// `blockRequestServeSlotsPerPeer` defines the block data handled by this operation.
	blockRequestServeSlotsPerPeer = 4
	// `snapshotServeRetryAttempts` defines the constant value used by this package.
	snapshotServeRetryAttempts = 6
	// `snapshotServeRetryBackoff` defines the constant value used by this package.
	snapshotServeRetryBackoff = 25 * time.Millisecond
	// `consensusPublishTimeout` defines the result produced by this operation.
	consensusPublishTimeout = 500 * time.Millisecond
)

// `blockRequestServeSem` stores the block data handled by this operation.
var blockRequestServeSem = make(chan struct{}, blockRequestServeConcurrencyLimit(MaxPeers))

// `syncPeerRequestTimeoutOverride` stores the value used by this operation.
var syncPeerRequestTimeoutOverride time.Duration

// blockRequestServeConcurrencyLimit implements the block request serve concurrency limit helper.
func blockRequestServeConcurrencyLimit(maxPeers int) int {
	if maxPeers <= 0 {
		return blockRequestServeMinConcurrency
	}
	// `limit` stores the value produced by this operation.
	limit := maxPeers * blockRequestServeSlotsPerPeer
	if limit < blockRequestServeMinConcurrency {
		return blockRequestServeMinConcurrency
	}
	return limit
}

type blockRequestPhaseError struct {
	// `Stage` stores the value associated with this record.
	Stage string
	// `Peer` stores the value associated with this record.
	Peer string
	// `From` stores the value associated with this record.
	From uint64
	// `To` stores the value associated with this record.
	To uint64
	// `After` stores the value associated with this record.
	After time.Duration
	// `Timeout` stores the result produced by this operation.
	Timeout bool
	// `Err` stores the error produced by this operation.
	Err error
}

// Error implements the error helper.
func (e *blockRequestPhaseError) Error() string {
	if e == nil {
		return ""
	}
	// `status` stores the value produced by this operation.
	status := "failed"
	if e.Timeout {
		status = "timeout"
	}
	// `msg` stores the value produced by this operation.
	msg := fmt.Sprintf("block_request_%s_%s peer=%s range=%d-%d", strings.TrimSpace(e.Stage), status, strings.TrimSpace(e.Peer), e.From, e.To)
	if e.After > 0 {
		msg += fmt.Sprintf(" after=%s", e.After)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap implements the unwrap helper.
func (e *blockRequestPhaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// newBlockRequestPhaseError implements the new block request phase error helper.
func newBlockRequestPhaseError(stage string, pid peer.ID, from, to uint64, after time.Duration, timeout bool, err error) error {
	return &blockRequestPhaseError{
		Stage:   strings.TrimSpace(stage),
		Peer:    ShortID(pid.String()),
		From:    from,
		To:      to,
		After:   after,
		Timeout: timeout,
		Err:     err,
	}
}

// isNetTimeout implements the is net timeout helper.
func isNetTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	// `netErr` stores the error produced by this operation.
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// syncBlockRequestMaxBlocks implements the sync block request max blocks helper.
func syncBlockRequestMaxBlocks(batchMax uint64) uint64 {
	if batchMax == 0 || batchMax > blockSyncServeMaxBlocks {
		return blockSyncServeMaxBlocks
	}
	return batchMax
}

// clampBlockSyncRangeToServeLimit implements the clamp block sync range to serve limit helper.
func clampBlockSyncRangeToServeLimit(from, to uint64, batchMax uint64) uint64 {
	// `maxBlocks` stores the value produced by this operation.
	maxBlocks := syncBlockRequestMaxBlocks(batchMax)
	if maxBlocks == 0 || to < from {
		return to
	}
	if to-from >= maxBlocks {
		return from + maxBlocks - 1
	}
	return to
}

type PeerDriftClass string

const (
	// `PeerDriftClassStale` defines the constant value used by this package.
	PeerDriftClassStale PeerDriftClass = "STALE_DRIFT"
	// `PeerDriftClassDangerous` defines the constant value used by this package.
	PeerDriftClassDangerous PeerDriftClass = "DANGEROUS_DRIFT"
	// `PeerDriftClassAhead` defines the constant value used by this package.
	PeerDriftClassAhead PeerDriftClass = "AHEAD_DRIFT"
)

type PeerDriftState struct {
	// `Count` stores the measured quantity used by this operation.
	Count int
	// `FirstSeen` stores the value associated with this record.
	FirstSeen time.Time
	// `LastSeen` stores the value associated with this record.
	LastSeen time.Time
	// `From` stores the value associated with this record.
	From uint64
	// `To` stores the value associated with this record.
	To uint64
	// `Expected` stores the value associated with this record.
	Expected string
	// `Got` stores the value associated with this record.
	Got string
	// `RecomputedAt` stores the value associated with this record.
	RecomputedAt time.Time
	// `SyncOnlyUntil` stores the value associated with this record.
	SyncOnlyUntil time.Time
	// `LastClass` stores the value associated with this record.
	LastClass PeerDriftClass
}

// configPeerListsSnapshot implements the config peer lists snapshot helper.
func (n *Node) configPeerListsSnapshot() ([]string, []string) {
	if n == nil || n.Config == nil {
		return nil, nil
	}
	n.configMu.RLock()
	// `persistent` stores the value produced by this operation.
	persistent := append([]string{}, n.Config.PersistentPeers...)
	// `seeds` stores the value produced by this operation.
	seeds := append([]string{}, n.Config.Seeds...)
	n.configMu.RUnlock()
	return persistent, seeds
}

// persistentPeersSnapshot implements the persistent peers snapshot helper.
func (n *Node) persistentPeersSnapshot() []string {
	// `persistent` stores the value produced by this operation.
	persistent, _ := n.configPeerListsSnapshot()
	return persistent
}

// seedsSnapshot implements the seeds snapshot helper.
func (n *Node) seedsSnapshot() []string {
	// `seeds` stores the value produced by this operation.
	_, seeds := n.configPeerListsSnapshot()
	return seeds
}

// setPersistentPeers implements the set persistent peers helper.
func (n *Node) setPersistentPeers(peers []string) {
	if n == nil || n.Config == nil {
		return
	}
	n.configMu.Lock()
	n.Config.PersistentPeers = append([]string{}, peers...)
	n.configMu.Unlock()
}

// setSeedPeers implements the set seed peers helper.
func (n *Node) setSeedPeers(peers []string) {
	if n == nil || n.Config == nil {
		return
	}
	n.configMu.Lock()
	n.Config.Seeds = append([]string{}, peers...)
	n.configMu.Unlock()
}

// setConfigPeerLists implements the set config peer lists helper.
func (n *Node) setConfigPeerLists(persistent []string, seeds []string) {
	if n == nil || n.Config == nil {
		return
	}
	n.configMu.Lock()
	n.Config.PersistentPeers = append([]string{}, persistent...)
	n.Config.Seeds = append([]string{}, seeds...)
	n.configMu.Unlock()
}

// logPubSubStatus implements the log pub sub status helper.
func (n *Node) logPubSubStatus() {
	if n.PubSub == nil {
		fmt.Println("PubSub: Not initialized")
		return
	}

	// `topics` stores the value produced by this operation.
	topics := []string{TopicBlock, TopicTx, TopicConsensus, TopicValidator, TopicSnapshotMeta, TopicSnapshotChunk, TopicSnapshotProof}

	fmt.Println("PubSub Mesh Status:")
	// `hasConnections` stores the value produced by this operation.
	hasConnections := false

	// `topicName` tracks the current values while iterating.
	for _, topicName := range topics {
		// `peers` stores the value produced by this operation.
		peers := n.PubSub.ListPeers(topicName)
		fmt.Printf("  %s: %d peers\n", topicName, len(peers))

		if len(peers) > 0 {
			hasConnections = true
			if DebugConsensus {
				// `i` and `pid` track the current position in the related collection.
				for i, pid := range peers {
					if i >= 3 {
						break
					}
					fmt.Printf("    - %s\n", pid.String()[:8])
				}
				if len(peers) > 3 {
					fmt.Printf("    ... and %d more\n", len(peers)-3)
				}
			}
		}
	}

	if n.Host != nil {
		// `allPeers` stores the value produced by this operation.
		allPeers := n.Host.Network().Peers()
		fmt.Printf("Network: %d total peers connected\n", len(allPeers))
		if !hasConnections && len(allPeers) > 0 {
			fmt.Println("WARNING: Peers connected but not in PubSub mesh")
			fmt.Println("Action: trigger publish, ensure common topics, or restart nodes together")
		}
	}
}

// legacy consensus removed
// Legacy proposal/vote gossip paths were removed with ResultGossipOnly.
func (n *Node) BroadcastLeaderBlock(block Block) {
	if n.BlockTopic == nil && n.TopicBlocks == nil && n.PubSub == nil {
		return
	}
	// Safety rail: never publish a locally-invalid proposal.
	if err := n.preflightOwnLeaderBlock(block); err != nil {
		if DebugConsensus {
			fmt.Printf("Blocked leader broadcast @ epoch %d: %v\n", block.ID, err)
		}
		return
	}
	n.broadcastLeaderBlockUnchecked(block)
}

// publishConsensusTopicWithTimeout implements the publish consensus topic with timeout helper.
func (n *Node) publishConsensusTopicWithTimeout(topic *pubsub.Topic, data []byte) error {
	if topic == nil {
		return nil
	}
	// `ctx` stores the context controlling this operation.
	ctx := context.Background()
	if n != nil {
		// `rootCtx` stores the context controlling this operation.
		if rootCtx := n.RootContext(); rootCtx != nil {
			ctx = rootCtx
		}
	}
	// `publishCtx` and `cancel` store the context controlling this operation.
	publishCtx, cancel := context.WithTimeout(ctx, consensusPublishTimeout)
	defer cancel()
	return topic.Publish(publishCtx, data)
}

// broadcastLeaderBlockUnchecked implements the broadcast leader block unchecked helper.
func (n *Node) broadcastLeaderBlockUnchecked(block Block) {
	if n == nil || (n.BlockTopic == nil && n.TopicBlocks == nil && n.PubSub == nil) {
		return
	}
	// `msg` stores the value produced by this operation.
	msg := Message{
		Type: MsgLeaderBlock,
		Data: MustJSON(block),
	}
	// `data` and `err` store the error produced by this operation.
	data, err := MarshalP2PMessage(msg)
	if err != nil {
		return
	}
	n.fanoutConsensusMessageToPeers(msg)

	// `published` stores the value produced by this operation.
	published := false
	if n.BlockTopic != nil {
		_ = n.publishConsensusTopicWithTimeout(n.BlockTopic, data)
		published = true
	}
	if n.TopicBlocks != nil && n.TopicBlocks != n.BlockTopic {
		_ = n.publishConsensusTopicWithTimeout(n.TopicBlocks, data)
		published = true
	}
	if n.ConsensusTopic != nil && n.ConsensusTopic != n.BlockTopic && n.ConsensusTopic != n.TopicBlocks {
		_ = n.publishConsensusTopicWithTimeout(n.ConsensusTopic, data)
		published = true
	}
	if published {
		return
	}

	n.SafeGo(fmt.Sprintf("leader_block_publish_%d_%d", block.ID, block.Round), func() {
		// `err` stores the error produced by this operation.
		if err := n.PubSub.Publish(TopicBlock, data); err != nil {
			_ = n.PubSub.Publish(TopicBlocksLegacy, data)
		}
		_ = n.PubSub.Publish(TopicConsensus, data)
	})
}

// handleTransactionGossip handles transaction gossip.
func (n *Node) handleTransactionGossip(sub *pubsub.Subscription) {
	for {
		// `err` stores the error produced by this operation.
		_, err := sub.Next(n.RootContext())
		if err != nil {
			return
		}
	}
}

// handleValidatorGossip handles validator gossip.
func (n *Node) handleValidatorGossip(sub *pubsub.Subscription) {
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()
	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		// `peerID` stores the value produced by this operation.
		peerID := ""
		if msg.ReceivedFrom != "" {
			peerID = msg.ReceivedFrom.String()
		}
		if n.handleConsensusEnvelopeFromPeer(msg.Data, peerID) {
			continue
		}

		// `wrapped` stores the value used by this operation.
		var wrapped Message
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &wrapped); err == nil && wrapped.Type != "" {
			switch wrapped.Type {
			case MsgValidatorAnnounce:
				n.handleValidatorAnnouncement(wrapped.Data)
			case MsgValidatorSetUpdate:
				// `update` stores the value used by this operation.
				var update ValidatorSetUpdate
				// `err` stores the error produced by this operation.
				if err := json.Unmarshal(wrapped.Data, &update); err == nil {
					n.handleValidatorSetUpdate(update)
				}
			}
			continue
		}

		// `info` stores the current position in the related collection.
		var info ValidatorInfo
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &info); err != nil {
			continue
		}
		info.ID = normalizeValidatorID(info.ID)
		if info.ID == "" {
			continue
		}

		n.validatorMu.Lock()
		// `st` and `ok` store whether the related condition is satisfied.
		st, ok := n.validatorStatus[info.ID]
		if !ok {
			st = &ValidatorStatus{}
			n.validatorStatus[info.ID] = st
		}
		st.Height = info.Height
		st.LastSeen = time.Now()
		n.validatorMu.Unlock()

		participationMu.Lock()
		// `ok` stores whether the related condition is satisfied.
		if _, ok := Participation[info.ID]; !ok {
			Participation[info.ID] = &ParticipationScore{
				ValidBlocks:   1,
				InvalidBlocks: 0,
				LastSeen:      time.Now(),
				CooldownUntil: 0,
				Reputation:    100,
			}
		}
		participationMu.Unlock()
	}
}

// handleConsensusGossip handles consensus gossip.
func (n *Node) handleConsensusGossip(sub *pubsub.Subscription) {
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()
	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		_ = n.handleConsensusEnvelope(msg.Data)
	}
}

// handleBlockGossip handles block gossip.
func (n *Node) handleBlockGossip(sub *pubsub.Subscription) {
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()

	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := sub.Next(ctx)
		if err != nil {
			if DebugNet {
				fmt.Println("Ã¢ÂÅ’ Block gossip read error:", err)
			}
			return
		}

		// Ignore self-published messages
		if msg.ReceivedFrom == n.Host.ID() {
			continue
		}

		// Ignore non-JSON payloads (e.g., pubsub mesh keepalive)
		if len(msg.Data) == 0 || msg.Data[0] != '{' {
			continue
		}

		// Leader blocks are wrapped in Message
		var wrapped Message
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &wrapped); err == nil && wrapped.Type != "" {
			if wrapped.Type == MsgLeaderBlock {
				// `block` stores the synchronization state protecting shared data.
				var block Block
				// `err` stores the error produced by this operation.
				if err := json.Unmarshal(wrapped.Data, &block); err == nil {
					_ = n.submitLeaderBlockOnConsensusLane(block, msg.ReceivedFrom.String())
				}
			}
			if ResultGossipOnly {
				continue
			}
		}

		if ResultGossipOnly {
			continue
		}

		// `block` stores the synchronization state protecting shared data.
		var block Block
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &block); err != nil {
			if DebugNet {
				fmt.Println("Ã¢ÂÅ’ Invalid block gossip payload")
			}
			continue
		}

		if DebugConsensus {
			fmt.Printf(
				"Ã°Å¸â€œÂ¦ Block received via gossip | height=%d from=%s\n",
				block.ID,
				msg.ReceivedFrom.String(),
			)
		}

		// Pass into blockchain
		_ = n.submitFinalBlockOnConsensusLane(block)
	}
}

// requestBlocksFromPeer implements the request blocks from peer helper.
func (n *Node) requestBlocksFromPeer(
	pid peer.ID,
	from, to uint64,
	wantSnapshot bool,
	snapshotHeight uint64,
) ([]Block, *StateSnapshot, *ExecPoolSnapshot, error) {
	return n.requestBlocksFromPeerWithContext(context.Background(), pid, from, to, wantSnapshot, snapshotHeight)
}

func (n *Node) requestBlocksFromPeerWithContext(
	ctx context.Context,
	pid peer.ID,
	from, to uint64,
	wantSnapshot bool,
	snapshotHeight uint64,
) ([]Block, *StateSnapshot, *ExecPoolSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !wantSnapshot {
		// `blocks` and `ok` store whether the related condition is satisfied.
		if blocks, ok := n.localBlocksForRange(from, to); ok {
			if DebugSync || DebugConsensus {
				fmt.Printf("[SYNC-REQUEST-SKIP] peer=%s range=%d-%d reason=local_blocks_present count=%d\n",
					ShortID(pid.String()), from, to, len(blocks))
			}
			return blocks, nil, nil, nil
		}
	}
	return n.requestBlocksFromPeerDirectWithContext(ctx, pid, from, to, wantSnapshot, snapshotHeight)
}

// runBlockRequestStreamPhase implements the run block request stream phase helper.
func runBlockRequestStreamPhase(s network.Stream, timeout time.Duration, operation func() error) (error, bool) {
	return runBlockRequestStreamPhaseContext(context.Background(), s, timeout, operation)
}

func runBlockRequestStreamPhaseContext(ctx context.Context, s network.Stream, timeout time.Duration, operation func() error) (error, bool) {
	if operation == nil {
		return errors.New("missing stream operation"), false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		select {
		case <-ctx.Done():
			if s != nil {
				_ = s.Reset()
			}
			return ctx.Err(), true
		default:
			return operation(), false
		}
	}
	// `resultCh` stores the result produced by this operation.
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- operation()
	}()
	// `timer` stores the value produced by this operation.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-resultCh:
		return err, false
	case <-ctx.Done():
		if s != nil {
			_ = s.Reset()
		}
		return ctx.Err(), true
	case <-timer.C:
		if s != nil {
			_ = s.Reset()
		}
		return context.DeadlineExceeded, true
	}
}

// requestBlocksFromPeerDirect implements the request blocks from peer direct helper.
func (n *Node) requestBlocksFromPeerDirect(
	pid peer.ID,
	from, to uint64,
	wantSnapshot bool,
	snapshotHeight uint64,
) ([]Block, *StateSnapshot, *ExecPoolSnapshot, error) {
	return n.requestBlocksFromPeerDirectWithContext(context.Background(), pid, from, to, wantSnapshot, snapshotHeight)
}

func (n *Node) requestBlocksFromPeerDirectWithContext(
	ctx context.Context,
	pid peer.ID,
	from, to uint64,
	wantSnapshot bool,
	snapshotHeight uint64,
) ([]Block, *StateSnapshot, *ExecPoolSnapshot, error) {
	if n.Host == nil {
		return nil, nil, nil, fmt.Errorf("host not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// `timeout` stores the result produced by this operation.
	timeout := syncPeerRequestTimeout()
	// `peerLabel` stores the value produced by this operation.
	peerLabel := ShortID(pid.String())
	// `traceRequest` stores the request data being processed.
	traceRequest := DebugSync || DebugConsensus
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-START] peer=%s range=%d-%d snapshot=%t snapshot_height=%d timeout_ms=%d\n",
			peerLabel, from, to, wantSnapshot, snapshotHeight, timeout.Milliseconds())
	}

	// `openCtx` and `cancelOpen` store the context controlling this operation.
	openCtx, cancelOpen := context.WithTimeout(ctx, timeout)
	defer cancelOpen()
	// `openStarted` stores the value produced by this operation.
	openStarted := time.Now()
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-OPEN] peer=%s range=%d-%d\n", peerLabel, from, to)
	}
	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(openCtx, pid, BlockSyncProtocol)
	if err != nil {
		// `wrapped` stores the value produced by this operation.
		wrapped := newBlockRequestPhaseError("open", pid, from, to, time.Since(openStarted), errors.Is(openCtx.Err(), context.DeadlineExceeded), err)
		fmt.Printf("[SYNC-REQUEST-OPEN-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, wrapped)
		return nil, nil, nil, wrapped
	}
	if s == nil {
		// `err` stores the error produced by this operation.
		err := newBlockRequestPhaseError("open", pid, from, to, time.Since(openStarted), false, fmt.Errorf("nil stream"))
		fmt.Printf("[SYNC-REQUEST-OPEN-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, err)
		return nil, nil, nil, err
	}
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-OPEN-OK] peer=%s range=%d-%d duration_ms=%d\n",
			peerLabel, from, to, time.Since(openStarted).Milliseconds())
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()

	// `req` stores the request data being processed.
	req := BlockRequest{
		From:           from,
		To:             to,
		WantSnapshot:   wantSnapshot,
		SnapshotHeight: snapshotHeight,
		BypassAck:      true,
	}
	_ = s.SetWriteDeadline(time.Now().Add(timeout))
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `encodeStarted` stores the value produced by this operation.
	encodeStarted := time.Now()
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-ENCODE] peer=%s range=%d-%d\n", peerLabel, from, to)
	}
	// `err` and `timedOut` store the error produced by this operation.
	if err, timedOut := runBlockRequestStreamPhaseContext(ctx, s, timeout, func() error { return enc.Encode(req) }); err != nil {
		if !timedOut {
			_ = s.Reset()
		}
		s = nil
		// `wrapped` stores the value produced by this operation.
		wrapped := newBlockRequestPhaseError("encode", pid, from, to, time.Since(encodeStarted), timedOut || isNetTimeout(err), err)
		fmt.Printf("[SYNC-REQUEST-ENCODE-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, wrapped)
		return nil, nil, nil, wrapped
	}
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-ENCODE-OK] peer=%s range=%d-%d duration_ms=%d\n",
			peerLabel, from, to, time.Since(encodeStarted).Milliseconds())
	}

	_ = s.SetReadDeadline(time.Now().Add(timeout))
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	// `decodeStarted` stores the value produced by this operation.
	decodeStarted := time.Now()
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-DECODE] peer=%s range=%d-%d\n", peerLabel, from, to)
	}
	// `resp` stores the response produced by this operation.
	var resp BlockResponse
	// `err` and `timedOut` store the error produced by this operation.
	if err, timedOut := runBlockRequestStreamPhaseContext(ctx, s, timeout, func() error { return dec.Decode(&resp) }); err != nil {
		if !timedOut {
			_ = s.Reset()
		}
		s = nil
		// `wrapped` stores the value produced by this operation.
		wrapped := newBlockRequestPhaseError("decode", pid, from, to, time.Since(decodeStarted), timedOut || isNetTimeout(err), err)
		fmt.Printf("[SYNC-REQUEST-DECODE-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, wrapped)
		return nil, nil, nil, wrapped
	}
	if traceRequest {
		fmt.Printf("[SYNC-REQUEST-RESULT] peer=%s range=%d-%d count=%d snapshot=%t duration_ms=%d\n",
			peerLabel, from, to, len(resp.Blocks), resp.Snapshot != nil, time.Since(decodeStarted).Milliseconds())
	}
	return resp.Blocks, resp.Snapshot, resp.ExecPool, nil
}

// localBlocksForRange implements the local blocks for range helper.
func (n *Node) localBlocksForRange(from uint64, to uint64) ([]Block, bool) {
	if n == nil || n.Blockchain == nil || from == 0 || to < from {
		return nil, false
	}
	// `blocks` stores the block data handled by this operation.
	blocks := make([]Block, 0, to-from+1)
	// `height` stores the value produced by this operation.
	for height := from; ; height++ {
		// `block` and `ok` store whether the related condition is satisfied.
		block, ok := n.Blockchain.GetBlock(height)
		if !ok || block.ID != height || strings.TrimSpace(block.BlockHash) == "" {
			return nil, false
		}
		blocks = append(blocks, block)
		if height == to {
			break
		}
	}
	return blocks, true
}

// requestSnapshotMetaFromPeer implements the request snapshot meta from peer helper.
func (n *Node) requestSnapshotMetaFromPeer(pid peer.ID, height uint64) (*SnapshotMetaResponse, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host not initialized")
	}
	// `timeout` stores the result produced by this operation.
	timeout := syncPeerRequestTimeout()
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, SnapshotMetaProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	_ = s.SetDeadline(time.Now().Add(timeout))
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)

	// `req` stores the request data being processed.
	req := SnapshotMetaRequest{Height: height}
	// `err` stores the error produced by this operation.
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	// `resp` stores the response produced by this operation.
	var resp SnapshotMetaResponse
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	if !resp.Available {
		return nil, fmt.Errorf("snapshot metadata unavailable")
	}
	return &resp, nil
}

// requestSnapshotChunkFromPeer implements the request snapshot chunk from peer helper.
func (n *Node) requestSnapshotChunkFromPeer(pid peer.ID, height uint64, index uint64) (*SnapshotChunkResponse, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host not initialized")
	}
	// `timeout` stores the result produced by this operation.
	timeout := syncPeerRequestTimeout()
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, SnapshotChunkProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	_ = s.SetDeadline(time.Now().Add(timeout))
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	// `req` stores the request data being processed.
	req := SnapshotChunkRequest{Height: height, Index: index}
	// `err` stores the error produced by this operation.
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	// `resp` stores the response produced by this operation.
	var resp SnapshotChunkResponse
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Height == 0 || len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty snapshot chunk response")
	}
	if !strings.EqualFold(strings.TrimSpace(resp.ChunkHash), snapshotChunkHash(resp.Data)) {
		return nil, fmt.Errorf("snapshot chunk hash mismatch")
	}
	return &resp, nil
}

// sendBlockAck implements the send block ack helper.
func (n *Node) sendBlockAck(pid peer.ID, height uint64) {
	if n.Host == nil || height == 0 {
		return
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, "/msc/consensus/1.0.0")
	if err != nil {
		n.recordDialFailure(pid.String())
		// `errMsg` stores the error produced by this operation.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "consensus_protocol_mismatch")
		}
		return
	}
	defer s.Close()

	// `msg` stores the value produced by this operation.
	msg := Message{
		Type: MsgBlockAck,
		Data: MustJSON(BlockAck{Height: height}),
	}
	// `data` stores the value produced by this operation.
	data, _ := json.Marshal(msg)
	_, _ = s.Write(append(data, '\n'))
}

// sendConsensusMessage implements the send consensus message helper.
func (n *Node) sendConsensusMessage(pid peer.ID, msg Message) {
	if n == nil || n.Host == nil {
		return
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, "/msc/consensus/1.0.0")
	if err != nil {
		n.recordDialFailure(pid.String())
		// `errMsg` stores the error produced by this operation.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "consensus_protocol_mismatch")
		}
		return
	}
	defer s.Close()

	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_, _ = s.Write(append(data, '\n'))
	n.recordDialSuccess(pid.String())
}

// fanoutConsensusMessageToPeers implements the fanout consensus message to peers helper.
func (n *Node) fanoutConsensusMessageToPeers(msg Message) {
	if n == nil || n.Host == nil || n.isShuttingDown() {
		return
	}
	// `peers` stores the value produced by this operation.
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		return
	}
	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		// `peerID` stores the value produced by this operation.
		peerID := pid
		n.SafeGo(fmt.Sprintf("consensus_fanout_%s_%d", msg.Type, time.Now().UnixNano()), func() {
			n.sendConsensusMessage(peerID, msg)
		})
	}
}

// broadcastExecutionResult implements the broadcast execution result helper.
func (n *Node) broadcastExecutionResult(heightHint uint64, execHash string, txMerkle string) {
	n.broadcastExecutionResultInternal(heightHint, execHash, txMerkle, false)
}

const (
	// `execResultSigVersionV1` defines the constant value used by this package.
	execResultSigVersionV1 uint8 = 1
	// `execResultSigVersionV2` defines the constant value used by this package.
	execResultSigVersionV2 uint8 = 2
	// `execVoteModeDual` defines the constant value used by this package.
	execVoteModeDual = "dual"
	// Keep a wider recent-proposal window so delayed execution votes can still
	// resolve after long round-failover churn at the same height.
	execRecentProposalWindow = 64
	// `execProposalSwitchRoundGap` defines the constant value used by this package.
	execProposalSwitchRoundGap = 4
	// `execProposalStickyVoteThreshold` defines the constant value used by this package.
	execProposalStickyVoteThreshold = 2
	// `maxEmbeddedProposalPerHeight` defines the constant value used by this package.
	maxEmbeddedProposalPerHeight = 32
)

type execProposalSnapshot struct {
	// `Epoch` stores the value associated with this record.
	Epoch uint64
	// `Round` stores the value associated with this record.
	Round uint32
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `TxMerkle` stores the transaction data handled by this operation.
	TxMerkle string
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string
	// `ProposalKey` stores the key used to access the related value.
	ProposalKey string
}

type execBroadcastContext struct {
	// `HeightHint` stores the value associated with this record.
	HeightHint uint64
	// `RoundHint` stores the value associated with this record.
	RoundHint uint32
	// `BlockHashHint` stores the block data handled by this operation.
	BlockHashHint string
	// `ProposalKey` stores the key used to access the related value.
	ProposalKey string
	// `Block` stores the synchronization state protecting shared data.
	Block *Block
	// `ExecHash` stores the digest used to identify or verify the related data.
	ExecHash string
	// `TxMerkle` stores the transaction data handled by this operation.
	TxMerkle string
	// `TxCount` stores the transaction data handled by this operation.
	TxCount int
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string
	// `RuntimeLedgerHash` stores the digest used to identify or verify the related data.
	RuntimeLedgerHash string
	// `ExecutionLedgerHash` stores the digest used to identify or verify the related data.
	ExecutionLedgerHash string
	// `TipHeight` stores the value associated with this record.
	TipHeight uint64
	// `TipHash` stores the digest used to identify or verify the related data.
	TipHash string
}

type execVoteRebroadcastState struct {
	// `ProposalKey` stores the key used to access the related value.
	ProposalKey string
	// `VoteCount` stores the measured quantity used by this operation.
	VoteCount int
	// `LastObservedAt` stores the value associated with this record.
	LastObservedAt time.Time
	// `LastForcedAt` stores the value associated with this record.
	LastForcedAt time.Time
}

// Local execution vote markers must survive short retry churn, but once the
// proposal switch gap is reached a non-quorum marker is stale enough to release
// so validators can join the higher-round failover proposal.
const localExecVoteStaleRoundReleaseGap uint32 = execProposalSwitchRoundGap

// legacyProposalVoteKey implements the legacy proposal vote key helper.
func legacyProposalVoteKey(height uint64) string {
	return fmt.Sprintf("legacy|%d", height)
}

// proposalVoteKey implements the proposal vote key helper.
func proposalVoteKey(height uint64, round uint32, blockHash string, txMerkle string, stateRoot string) string {
	// ProposalID / consensus identity must remain round-aware even when the
	// underlying block hash stays stable across retries.
	//
	// Do not include stateRoot in the identity. A proposal can be observed
	// before local execution fills StateRoot and then later re-observed with a
	// populated root. Including that volatile value splits execution votes for
	// the same proposal across two buckets.
	_ = stateRoot
	return fmt.Sprintf("%d|%d|%s|%s|", height, round, blockHash, txMerkle)
}

// proposalVoteKeyParts implements the proposal vote key parts helper.
func proposalVoteKeyParts(proposalKey string) (uint64, uint32, string, string, string, bool) {
	// `parts` stores the value produced by this operation.
	parts := strings.SplitN(strings.TrimSpace(proposalKey), "|", 5)
	if len(parts) != 5 {
		return 0, 0, "", "", "", false
	}
	// `height` and `err` store the error produced by this operation.
	height, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, "", "", "", false
	}
	// `round64` and `err` store the error produced by this operation.
	round64, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil {
		return 0, 0, "", "", "", false
	}
	return height, uint32(round64), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3]), strings.TrimSpace(parts[4]), true
}

// canonicalExecutionResultHash returns canonical execution result hash.
func canonicalExecutionResultHash(height uint64, blockHash string, execHash string, txMerkle string) string {
	execHash = strings.TrimSpace(execHash)
	if height == 0 || execHash == "" {
		return ""
	}
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf("MSC_EXEC_RESULT_V1\x00%d\x00%s\x00%s\x00%s",
		height,
		strings.TrimSpace(blockHash),
		strings.TrimSpace(txMerkle),
		execHash,
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// executionResultHashFromProposal implements the execution result hash from proposal helper.
func executionResultHashFromProposal(epoch uint64, proposalKey string, blockHashHint string, execHash string, txMerkle string) string {
	// `height` stores the value produced by this operation.
	height := epoch
	// `blockHash` stores the block data handled by this operation.
	blockHash := strings.TrimSpace(blockHashHint)
	// `merkle` stores the value produced by this operation.
	merkle := strings.TrimSpace(txMerkle)
	// `h`, `parsedBlockHash`, `parsedTxMerkle`, and `ok` store whether the related condition is satisfied.
	if h, _, parsedBlockHash, parsedTxMerkle, _, ok := proposalVoteKeyParts(proposalKey); ok {
		if height == 0 {
			height = h
		}
		if blockHash == "" {
			blockHash = parsedBlockHash
		}
		if merkle == "" {
			merkle = parsedTxMerkle
		}
	}
	return canonicalExecutionResultHash(height, blockHash, execHash, merkle)
}

// executionResultHashFromMessage implements the execution result hash from message helper.
func executionResultHashFromMessage(res ExecutionResultMsg) string {
	return canonicalExecutionResultHash(res.HeightHint, res.BlockHashHint, res.ExecHash, res.TxMerkle)
}

// executionResultHashFromBlockResult implements the execution result hash from block result helper.
func executionResultHashFromBlockResult(result ExecutionResult, block Block) string {
	// `height` stores the value produced by this operation.
	height := result.Height
	if height == 0 {
		height = block.ID
	}
	// `blockHash` stores the block data handled by this operation.
	blockHash := strings.TrimSpace(result.BlockHash)
	if blockHash == "" {
		blockHash = executionVoteProposalHashForFinalBlock(block)
	}
	if blockHash == "" {
		blockHash = strings.TrimSpace(block.BlockHash)
	}
	// `txMerkle` stores the transaction data handled by this operation.
	txMerkle := strings.TrimSpace(result.TxMerkle)
	if txMerkle == "" {
		txMerkle = strings.TrimSpace(block.MempoolRoot)
	}
	return canonicalExecutionResultHash(height, blockHash, result.ResultHash, txMerkle)
}

// executionResultHashMatches implements the execution result hash matches helper.
func executionResultHashMatches(claimed string, expected string) bool {
	claimed = strings.TrimSpace(claimed)
	if claimed == "" {
		return true
	}
	expected = strings.TrimSpace(expected)
	return expected != "" && strings.EqualFold(claimed, expected)
}

// proposalVoteKeyRound implements the proposal vote key round helper.
func proposalVoteKeyRound(proposalKey string) (uint32, bool) {
	// `round` and `ok` store whether the related condition is satisfied.
	_, round, _, _, _, ok := proposalVoteKeyParts(proposalKey)
	return round, ok
}

// execEpochChoiceSignerKey implements the exec epoch choice signer key helper.
func execEpochChoiceSignerKey(signer string, proposalKey string) string {
	signer = normalizeValidatorID(signer)
	if signer == "" {
		return ""
	}
	_ = proposalKey
	return signer
}

// proposalExecKey implements the proposal exec key helper.
func proposalExecKey(proposalKey string, execHash string) string {
	if proposalKey == "" {
		return execHash
	}
	return fmt.Sprintf("%s|%s", proposalKey, execHash)
}

// execPoolScopeKey implements the exec pool scope key helper.
func execPoolScopeKey(epoch uint64, proposalKey string) string {
	proposalKey = strings.TrimSpace(proposalKey)
	if proposalKey == "" {
		return legacyProposalVoteKey(epoch)
	}
	if strings.HasPrefix(proposalKey, "legacy|") {
		return proposalKey
	}
	// `parts` stores the value produced by this operation.
	parts := strings.SplitN(proposalKey, "|", 5)
	if len(parts) != 5 {
		return proposalKey
	}
	// `heightPart` stores the value produced by this operation.
	heightPart := strings.TrimSpace(parts[0])
	// `blockHash` stores the block data handled by this operation.
	blockHash := strings.TrimSpace(parts[2])
	if blockHash == "" {
		return proposalKey
	}
	if heightPart == "" {
		heightPart = fmt.Sprintf("%d", epoch)
	}
	return fmt.Sprintf("block|%s|%s", heightPart, blockHash)
}

// execPoolResultScopeKey implements the exec pool result scope key helper.
func execPoolResultScopeKey(epoch uint64, proposalKey string, execHash string) string {
	// `scope` stores the value produced by this operation.
	scope := execPoolScopeKey(epoch, proposalKey)
	// `execResultHash` stores the digest used to identify or verify the related data.
	execResultHash := executionResultHashFromProposal(epoch, proposalKey, "", execHash, "")
	if execResultHash == "" {
		execResultHash = strings.TrimSpace(execHash)
	}
	if scope == "" || execResultHash == "" {
		return proposalExecKey(scope, execResultHash)
	}
	return fmt.Sprintf("%s|%s", scope, execResultHash)
}

// commitVoteScopeKey implements the commit vote scope key helper.
func commitVoteScopeKey(height uint64, proposalHash string) string {
	proposalHash = strings.TrimSpace(proposalHash)
	if height == 0 || proposalHash == "" {
		return ""
	}
	return fmt.Sprintf("block|%d|%s", height, proposalHash)
}

// execPoolResultKey implements the exec pool result key helper.
func execPoolResultKey(epoch uint64, proposalKey string, execHash string) string {
	return execPoolResultScopeKey(epoch, proposalKey, execHash)
}

// proposalSnapshotFromBlock implements the proposal snapshot from block helper.
func proposalSnapshotFromBlock(block Block) execProposalSnapshot {
	return execProposalSnapshot{
		Epoch:       block.ID,
		Round:       block.Round,
		BlockHash:   block.BlockHash,
		TxMerkle:    block.MempoolRoot,
		StateRoot:   block.StateRoot,
		ProposalKey: proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot),
	}
}

// acceptedProposalHeightKey implements the accepted proposal height key helper.
func acceptedProposalHeightKey(height uint64) string {
	return fmt.Sprintf("%d", height)
}

// acceptedProposalVoteCountLocked implements the accepted proposal vote count locked helper.
func (n *Node) acceptedProposalVoteCountLocked(epoch uint64, proposalKey string) int {
	if n == nil || epoch == 0 || proposalKey == "" {
		return 0
	}
	// ExecPool is the authoritative execution-vote state machine. The node-local
	// signer and Consensus.ExecVotes maps are mirrors for diagnostics/rebroadcast
	// only; counting them here lets stale mirrors fabricate a proposal lock.
	return getExecCountForProposalScopeGlobal(epoch, proposalKey, "")
}

// execVoteCreditedGlobal implements the exec vote credited global helper.
func execVoteCreditedGlobal(epoch uint64, proposalKey string, signer string, execHash string, txMerkle string) bool {
	signer = normalizeValidatorID(signer)
	if epoch == 0 || proposalKey == "" || signer == "" || execHash == "" {
		return false
	}
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()
	return execVoteCreditedGlobalLocked(epoch, proposalKey, signer, execHash, txMerkle)
}

// execVoteCreditedGlobalLocked implements the exec vote credited global locked helper.
func execVoteCreditedGlobalLocked(epoch uint64, proposalKey string, signer string, execHash string, txMerkle string) bool {
	signer = normalizeValidatorID(signer)
	if epoch == 0 || proposalKey == "" || signer == "" || execHash == "" {
		return false
	}
	// `poolScopeKey` stores the key used to access the related value.
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	// `scopedExecKey` stores the key used to access the related value.
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)
	// `choice` stores the value produced by this operation.
	choice := execBroadcastKey(execHash, txMerkle)

	// `byHash` and `ok` store whether the related condition is satisfied.
	if byHash, ok := ExecPool.pool[epoch]; ok {
		// `bySigner` and `ok` store whether the related condition is satisfied.
		if bySigner, ok := byHash[scopedExecKey]; ok {
			// `existing` and `ok` store whether the related condition is satisfied.
			if existing, ok := bySigner[signer]; ok {
				if strings.TrimSpace(txMerkle) == "" || strings.TrimSpace(existing.TxMerkle) == strings.TrimSpace(txMerkle) {
					return true
				}
			}
		}
	}
	// `byScope` and `ok` store whether the related condition is satisfied.
	if byScope, ok := ExecPool.signers[epoch]; ok {
		// `signers` stores the value produced by this operation.
		if signers := byScope[poolScopeKey]; signers != nil && signers[signer] {
			// `byChoice` and `ok` store whether the related condition is satisfied.
			if byChoice, ok := ExecPool.choice[epoch]; ok {
				// `choices` stores the value produced by this operation.
				if choices := byChoice[poolScopeKey]; choices != nil && strings.TrimSpace(choices[signer]) == choice {
					return true
				}
			}
		}
	}
	return false
}

// proposalVoteCount implements the proposal vote count helper.
func (n *Node) proposalVoteCount(block Block) int {
	if n == nil || block.ID == 0 {
		return 0
	}
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		return 0
	}
	// `proposalKey` stores the key used to access the related value.
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, execHash)
	if proposalKey == "" {
		return 0
	}
	return getExecCountGlobal(block.ID, proposalKey, execHash, block.MempoolRoot)
}

// proposalHasExecutionQuorum implements the proposal has execution quorum helper.
func (n *Node) proposalHasExecutionQuorum(block Block) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(block.ID)
	if required == 0 {
		return false
	}
	return n.proposalVoteCount(block) >= required
}

// proposalHasSignedCommitQuorum implements the proposal has signed commit quorum helper.
func (n *Node) proposalHasSignedCommitQuorum(block Block) (int, int, bool) {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return 0, 0, false
	}
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := strings.TrimSpace(block.StateRoot)
	// `count` and `required` store the measured quantity used by this operation.
	var count, required int
	if execHash != "" {
		_, _, count, required = n.commitVoteEvidenceForResult(block.ID, block.BlockHash, execHash, block.MempoolRoot)
	} else {
		_, _, count, required = n.commitVoteEvidence(block.ID, block.BlockHash)
	}
	return count, required, required > 0 && count >= required
}

// proposalHasLocalSignedExecutionQuorum reports whether this node has already
// signed a commit for a proposal that has enough local execution votes.
func (n *Node) proposalHasLocalSignedExecutionQuorum(block Block, voteCount int) bool {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return false
	}
	required := n.executionQuorumRequiredForEpoch(block.ID)
	if required == 0 || voteCount < required {
		return false
	}
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		return false
	}
	scope := commitVoteResultScopeKey(block.ID, block.BlockHash, execHash, block.MempoolRoot)
	return scope != "" && n.hasLocalSignedCommitScope(block.ID, scope)
}

// proposalAlreadySeenLocked implements the proposal already seen locked helper.
func (n *Node) proposalAlreadySeenLocked(block Block) bool {
	if n == nil || block.ID == 0 || n.acceptedProposalBlocks == nil {
		return false
	}
	// `snap` stores the value produced by this operation.
	snap := proposalSnapshotFromBlock(block)
	if snap.ProposalKey == "" {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	_, ok := n.acceptedProposalBlocks[snap.ProposalKey]
	return ok
}

// proposalShouldStayLocked implements the proposal should stay locked helper.
func (n *Node) proposalShouldStayLocked(block Block, voteCount int) (bool, string) {
	// `committed` stores the value produced by this operation.
	if _, _, committed := n.proposalHasSignedCommitQuorum(block); committed {
		return true, "signed_commit_quorum_locked"
	}
	if n.proposalHasLocalSignedExecutionQuorum(block, voteCount) {
		return true, "local_signed_execution_quorum_locked"
	}
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(block.ID)
	if required > 0 && voteCount >= required {
		return true, "execution_quorum_locked"
	}
	return false, ""
}

// proposalMatchesLocalExecution implements the proposal matches local execution helper.
func (n *Node) proposalMatchesLocalExecution(block Block) (bool, string) {
	if n == nil || block.ID == 0 {
		return false, ""
	}
	// `expected` stores the value produced by this operation.
	expected := strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	if expected == "" {
		return false, ""
	}
	// `actual` stores the value produced by this operation.
	actual := strings.TrimSpace(block.StateRoot)
	return actual != "" && strings.EqualFold(actual, expected), expected
}

// acceptedProposalVoteLockForRound implements the accepted proposal vote lock for round helper.
func (n *Node) acceptedProposalVoteLockForRound(epoch uint64, incomingRound uint32) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	// `heightKey` stores the key used to access the related value.
	heightKey := acceptedProposalHeightKey(epoch)
	// `currentKey` stores the key used to access the related value.
	currentKey := strings.TrimSpace(n.acceptedProposal[heightKey])
	if currentKey == "" {
		return Block{}, 0, false, ""
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.acceptedProposalBlocks[currentKey]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	if incomingRound <= block.Round {
		return Block{}, 0, false, ""
	}
	// `voteCount` stores the measured quantity used by this operation.
	voteCount := n.acceptedProposalVoteCountLocked(epoch, currentKey)
	// `keep` and `reason` store the value produced by this operation.
	keep, reason := n.proposalShouldHoldAgainstIncomingLocked(epoch, currentKey, block, voteCount, incomingRound)
	if !keep {
		return Block{}, 0, false, ""
	}
	return block, voteCount, true, reason
}

// executionQuorumRequiredForEpoch implements the execution quorum required for epoch helper.
func (n *Node) executionQuorumRequiredForEpoch(epoch uint64) int {
	if n == nil || epoch == 0 {
		return 0
	}
	// `validators` and `ok` store whether the related condition is satisfied.
	validators, _, ok := n.deterministicCommitteeValidatorsForHeight(epoch)
	if !ok || len(validators) == 0 {
		return 0
	}
	return execQuorumRequired(len(validators))
}

// proposalShouldHoldAgainstIncomingLocked implements the proposal should hold against incoming locked helper.
func (n *Node) proposalShouldHoldAgainstIncomingLocked(_ uint64, _ string, block Block, voteCount int, _ uint32) (bool, string) {
	return n.proposalShouldStayLocked(block, voteCount)
}

// acceptedProposalLockState implements the accepted proposal lock state helper.
func (n *Node) acceptedProposalLockState(epoch uint64) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, 0, false, ""
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	// `voteCount` stores the measured quantity used by this operation.
	voteCount := n.acceptedProposalVoteCountLocked(epoch, key)
	// `keep` and `reason` store the value produced by this operation.
	keep, reason := n.proposalShouldHoldAgainstIncomingLocked(epoch, key, block, voteCount, 0)
	return block, voteCount, keep, reason
}

// acceptedProposalHoldStateForIncomingRound implements the accepted proposal hold state for incoming round helper.
func (n *Node) acceptedProposalHoldStateForIncomingRound(epoch uint64, incomingRound uint32) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, 0, false, ""
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	// `voteCount` stores the measured quantity used by this operation.
	voteCount := n.acceptedProposalVoteCountLocked(epoch, key)
	// `keep` and `reason` store the value produced by this operation.
	keep, reason := n.proposalShouldHoldAgainstIncomingLocked(epoch, key, block, voteCount, incomingRound)
	return block, voteCount, keep, reason
}

// quorumLockedProposalLockState implements the quorum locked proposal lock state helper.
func (n *Node) quorumLockedProposalLockState(epoch uint64) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	block, ok := n.quorumLockedProposalBlockLocked(epoch)
	n.execResultsMu.Unlock()
	if !ok {
		return Block{}, 0, false, ""
	}
	// `commitVotes` and `committed` store the value produced by this operation.
	commitVotes, _, committed := n.proposalHasSignedCommitQuorum(block)
	if !committed {
		return block, commitVotes, false, ""
	}
	return block, commitVotes, true, "signed_commit_quorum_locked"
}

// quorumLockedProposalBlockLocked reads the quorum-locked block while execResultsMu is held.
func (n *Node) quorumLockedProposalBlockLocked(epoch uint64) (Block, bool) {
	if n == nil || epoch == 0 || n.quorumLockedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, false
	}
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(n.quorumLockedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, false
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, false
	}
	return block, true
}

// quorumLockedProposalHoldStateForIncomingRound implements the quorum locked proposal hold state for incoming round helper.
func (n *Node) quorumLockedProposalHoldStateForIncomingRound(epoch uint64, incoming Block, _ int) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	// `block`, `voteCount`, `keep`, and `reason` store the synchronization state protecting shared data.
	block, voteCount, keep, reason := n.quorumLockedProposalLockState(epoch)
	if !keep {
		return Block{}, 0, false, ""
	}
	if incoming.ID == 0 {
		return block, voteCount, true, reason
	}
	return block, voteCount, true, reason
}

// proposalConflictsWithAcceptedLock implements the proposal conflicts with accepted lock helper.
func proposalConflictsWithAcceptedLock(locked Block, incoming Block) bool {
	if locked.ID == 0 || incoming.ID == 0 {
		return false
	}
	// `lockedHash` stores the synchronization state protecting shared data.
	lockedHash := strings.TrimSpace(locked.BlockHash)
	// `incomingHash` stores the digest used to identify or verify the related data.
	incomingHash := strings.TrimSpace(incoming.BlockHash)
	if lockedHash == "" || incomingHash == "" {
		return false
	}
	return !strings.EqualFold(lockedHash, incomingHash)
}

// commitPinnedProposalHashesForHeight implements the commit pinned proposal hashes for height helper.
func (n *Node) commitPinnedProposalHashesForHeight(height uint64) map[string]struct{} {
	if n == nil || height == 0 {
		return nil
	}
	n.commitMu.Lock()
	defer n.commitMu.Unlock()
	// `byScope` stores the value produced by this operation.
	byScope := n.commitVoteSignatures[height]
	if len(byScope) == 0 {
		return nil
	}
	// `pinned` stores the value produced by this operation.
	pinned := make(map[string]struct{}, len(byScope))
	// `scope` and `bySigner` track the current values while iterating.
	for scope, bySigner := range byScope {
		// `hasSignature` stores the value produced by this operation.
		hasSignature := false
		// `sig` tracks the current values while iterating.
		for _, sig := range bySigner {
			if strings.TrimSpace(sig) != "" {
				hasSignature = true
				break
			}
		}
		if !hasSignature {
			continue
		}
		// `proposalHash` stores the digest used to identify or verify the related data.
		if proposalHash := commitVoteProposalHashFromScope(height, scope); proposalHash != "" {
			pinned[proposalHash] = struct{}{}
		}
	}
	return pinned
}

// pruneAcceptedProposalBlocksForEpochLocked implements the prune accepted proposal blocks for epoch locked helper.
func (n *Node) pruneAcceptedProposalBlocksForEpochLocked(epoch uint64) {
	if n == nil || epoch == 0 || n.acceptedProposalBlocks == nil {
		return
	}
	type proposalEntry struct {
		// `key` stores the key used to access the related value.
		key string
		// `blockHash` stores the block data handled by this operation.
		blockHash string
		// `round` stores the value associated with this record.
		round uint32
	}
	// `entries` stores the value produced by this operation.
	entries := make([]proposalEntry, 0, len(n.acceptedProposalBlocks))
	// `currentKey` stores the key used to access the related value.
	currentKey := ""
	// `quorumLockedKey` stores the key used to access the related value.
	quorumLockedKey := ""
	// `commitPinnedHashes` stores the value produced by this operation.
	commitPinnedHashes := n.commitPinnedProposalHashesForHeight(epoch)
	if n.acceptedProposal != nil {
		currentKey = strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	}
	if n.quorumLockedProposal != nil {
		quorumLockedKey = strings.TrimSpace(n.quorumLockedProposal[acceptedProposalHeightKey(epoch)])
	}
	// `key` and `block` track the synchronization state protecting shared data.
	for key, block := range n.acceptedProposalBlocks {
		if block.ID != epoch {
			continue
		}
		entries = append(entries, proposalEntry{key: key, blockHash: strings.TrimSpace(block.BlockHash), round: block.Round})
	}
	if len(entries) <= execRecentProposalWindow {
		return
	}
	// `isCommitPinned` stores the current position in the related collection.
	isCommitPinned := func(entry proposalEntry) bool {
		if len(commitPinnedHashes) == 0 || entry.blockHash == "" {
			return false
		}
		// `ok` stores whether the related condition is satisfied.
		_, ok := commitPinnedHashes[entry.blockHash]
		return ok
	}
	sort.Slice(entries, func(i, j int) bool {
		// `iLocked` stores the current position in the related collection.
		iLocked := entries[i].key == quorumLockedKey
		// `jLocked` stores the current position in the related collection.
		jLocked := entries[j].key == quorumLockedKey
		if iLocked != jLocked {
			return iLocked
		}
		// `iCurrent` stores the current position in the related collection.
		iCurrent := entries[i].key == currentKey
		// `jCurrent` stores the current position in the related collection.
		jCurrent := entries[j].key == currentKey
		if iCurrent != jCurrent {
			return iCurrent
		}
		// `iCommitPinned` stores the current position in the related collection.
		iCommitPinned := isCommitPinned(entries[i])
		// `jCommitPinned` stores the current position in the related collection.
		jCommitPinned := isCommitPinned(entries[j])
		if iCommitPinned != jCommitPinned {
			return iCommitPinned
		}
		if entries[i].round != entries[j].round {
			return entries[i].round > entries[j].round
		}
		return entries[i].key < entries[j].key
	})
	// `keep` stores the value produced by this operation.
	keep := make(map[string]bool, execRecentProposalWindow+1)
	// `i` stores the current position in the related collection.
	for i := 0; i < len(entries) && i < execRecentProposalWindow; i++ {
		keep[entries[i].key] = true
	}
	if currentKey != "" {
		keep[currentKey] = true
	}
	if quorumLockedKey != "" {
		keep[quorumLockedKey] = true
	}
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if isCommitPinned(entry) {
			keep[entry.key] = true
		}
	}
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if !keep[entry.key] {
			delete(n.acceptedProposalBlocks, entry.key)
		}
	}
}

// registerAcceptedProposalBlockLocked implements the register accepted proposal block locked helper.
func (n *Node) registerAcceptedProposalBlockLocked(block Block) execProposalSnapshot {
	// `snap` stores the value produced by this operation.
	snap := proposalSnapshotFromBlock(block)
	if snap.ProposalKey == "" {
		return snap
	}
	if n.acceptedProposalBlocks == nil {
		n.acceptedProposalBlocks = make(map[string]Block)
	}
	n.acceptedProposalBlocks[snap.ProposalKey] = block
	n.pruneAcceptedProposalBlocksForEpochLocked(block.ID)
	return snap
}

// setAcceptedProposalLocked implements the set accepted proposal locked helper.
func (n *Node) setAcceptedProposalLocked(block Block, reason string, force bool) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	// `snap` stores the value produced by this operation.
	snap := n.registerAcceptedProposalBlockLocked(block)
	if snap.ProposalKey == "" {
		return false
	}
	if n.acceptedProposal == nil {
		n.acceptedProposal = make(map[string]string)
	}
	// `heightKey` stores the key used to access the related value.
	heightKey := acceptedProposalHeightKey(block.ID)
	// `currentKey` stores the key used to access the related value.
	currentKey := strings.TrimSpace(n.acceptedProposal[heightKey])
	if currentKey == snap.ProposalKey {
		return false
	}
	// `prevRound` stores the value produced by this operation.
	prevRound := uint32(0)
	// `prevBlockHash` stores the digest used to identify or verify the related data.
	prevBlockHash := ""
	// `prevVotes` stores the value produced by this operation.
	prevVotes := 0
	// `prevBlock` stores the synchronization state protecting shared data.
	prevBlock := Block{}
	if currentKey != "" {
		// `currentBlock` and `ok` store whether the related condition is satisfied.
		if currentBlock, ok := n.acceptedProposalBlocks[currentKey]; ok {
			prevBlock = currentBlock
			prevRound = currentBlock.Round
			prevBlockHash = currentBlock.BlockHash
		}
		prevVotes = n.acceptedProposalVoteCountLocked(block.ID, currentKey)
		if block.Round <= prevRound {
			return false
		}
		if prevBlock.ID == block.ID && proposalConflictsWithAcceptedLock(prevBlock, block) {
			// `keepCurrent` and `keepReason` store the value produced by this operation.
			if keepCurrent, keepReason := n.proposalShouldHoldAgainstIncomingLocked(block.ID, currentKey, prevBlock, prevVotes, block.Round); keepCurrent {
				if DebugConsensus {
					commitVotes, required, _ := n.proposalHasSignedCommitQuorum(prevBlock)
					if keepReason == "local_signed_execution_quorum_locked" {
						commitVotes = prevVotes
						required = n.executionQuorumRequiredForEpoch(prevBlock.ID)
					}
					fmt.Printf("[EXEC-PROPOSAL-KEEP] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d required=%d reason=%s\n",
						block.ID,
						prevRound,
						ShortHash(prevBlockHash),
						block.Round,
						ShortHash(block.BlockHash),
						commitVotes,
						required,
						keepReason,
					)
				}
				return false
			}
		}
	}
	n.acceptedProposal[heightKey] = snap.ProposalKey
	log.Printf("[EXEC-PROPOSAL-SELECT] height=%d reason=%s prev_round=%d prev_block=%s round=%d block=%s voters=%d force=%t",
		block.ID,
		strings.TrimSpace(reason),
		prevRound,
		ShortHash(prevBlockHash),
		block.Round,
		ShortHash(block.BlockHash),
		prevVotes,
		force,
	)
	n.persistConsensusSafetyStateAsync("accepted_proposal")
	n.emitConsensusTelemetry(consensusTelemetryEvent{
		Type:      "accepted_proposal",
		Reason:    strings.TrimSpace(reason),
		Height:    block.ID,
		Round:     block.Round,
		BlockHash: block.BlockHash,
		Fields: map[string]interface{}{
			"previous_round": prevRound,
			"previous_block": prevBlockHash,
			"force":          force,
		},
	})
	return true
}

// setQuorumLockedProposalLocked implements the set quorum locked proposal locked helper.
func (n *Node) setQuorumLockedProposalLocked(block Block, reason string, voteCount int, required int) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	// Callers must pass the signed commit vote count for this exact proposal.
	if required <= 0 || voteCount < required {
		return false
	}
	// `snap` stores the value produced by this operation.
	snap := n.registerAcceptedProposalBlockLocked(block)
	if snap.ProposalKey == "" {
		return false
	}
	if n.quorumLockedProposal == nil {
		n.quorumLockedProposal = make(map[string]string)
	}
	// `heightKey` stores the key used to access the related value.
	heightKey := acceptedProposalHeightKey(block.ID)
	// `currentKey` stores the key used to access the related value.
	currentKey := strings.TrimSpace(n.quorumLockedProposal[heightKey])
	if currentKey == snap.ProposalKey {
		return false
	}
	// `prevRound` stores the value produced by this operation.
	prevRound := uint32(0)
	// `prevBlockHash` stores the digest used to identify or verify the related data.
	prevBlockHash := ""
	if currentKey != "" {
		// `currentBlock` and `ok` store whether the related condition is satisfied.
		if currentBlock, ok := n.acceptedProposalBlocks[currentKey]; ok {
			prevRound = currentBlock.Round
			prevBlockHash = currentBlock.BlockHash
		}
	}
	n.quorumLockedProposal[heightKey] = snap.ProposalKey
	log.Printf("[EXEC-PRECOMMIT] height=%d reason=%s prev_round=%d prev_block=%s round=%d block=%s votes=%d required=%d",
		block.ID,
		strings.TrimSpace(reason),
		prevRound,
		ShortHash(prevBlockHash),
		block.Round,
		ShortHash(block.BlockHash),
		voteCount,
		required,
	)
	n.syncConsensusLockedBlock(block)
	return true
}

// noteObservedProposal implements the note observed proposal helper.
func (n *Node) noteObservedProposal(block Block) {
	if n == nil || block.ID == 0 {
		return
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	n.registerAcceptedProposalBlockLocked(block)
	_ = n.setAcceptedProposalLocked(block, "observed", false)
}

// maybeAdoptProposalOnExecutionVote implements the maybe adopt proposal on execution vote helper.
func (n *Node) maybeAdoptProposalOnExecutionVote(block Block) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil {
		n.acceptedProposal = make(map[string]string)
	}
	n.registerAcceptedProposalBlockLocked(block)
	// `heightKey` stores the key used to access the related value.
	heightKey := acceptedProposalHeightKey(block.ID)
	// `currentKey` stores the key used to access the related value.
	currentKey := strings.TrimSpace(n.acceptedProposal[heightKey])
	if currentKey == "" {
		return n.setAcceptedProposalLocked(block, "vote_observed", true)
	}
	// `nextKey` stores the key used to access the related value.
	nextKey := proposalSnapshotFromBlock(block).ProposalKey
	if currentKey == nextKey {
		return false
	}
	// `currentVotes` stores the value produced by this operation.
	currentVotes := n.acceptedProposalVoteCountLocked(block.ID, currentKey)
	// `currentBlock` stores the synchronization state protecting shared data.
	currentBlock := n.acceptedProposalBlocks[currentKey]
	// `keepCurrent` stores the value produced by this operation.
	if keepCurrent, _ := n.proposalShouldStayLocked(currentBlock, currentVotes); keepCurrent {
		return false
	}
	if block.Round <= currentBlock.Round {
		return false
	}
	return n.setAcceptedProposalLocked(block, "vote_observed", true)
}

// clearAcceptedProposal implements the clear accepted proposal helper.
func (n *Node) clearAcceptedProposal(epoch uint64) {
	if n == nil || epoch == 0 {
		return
	}
	n.execResultsMu.Lock()
	if n.acceptedProposal != nil {
		delete(n.acceptedProposal, acceptedProposalHeightKey(epoch))
	}
	if n.quorumLockedProposal != nil {
		delete(n.quorumLockedProposal, acceptedProposalHeightKey(epoch))
	}
	if n.acceptedProposalBlocks != nil {
		// `key` and `block` track the synchronization state protecting shared data.
		for key, block := range n.acceptedProposalBlocks {
			if block.ID == epoch {
				delete(n.acceptedProposalBlocks, key)
			}
		}
	}
	n.execResultsMu.Unlock()
	n.clearConsensusLockedBlock(epoch)
	n.persistConsensusSafetyStateAsync("accepted_proposal_cleared")
}

// clearAcceptedProposalIfBlock implements the clear accepted proposal if block helper.
func (n *Node) clearAcceptedProposalIfBlock(epoch uint64, block Block, reason string) bool {
	if n == nil || epoch == 0 {
		return false
	}
	// `snap` stores the value produced by this operation.
	snap := proposalSnapshotFromBlock(block)
	// `proposalKey` stores the key used to access the related value.
	proposalKey := strings.TrimSpace(snap.ProposalKey)
	if proposalKey == "" {
		return false
	}
	// `heightKey` stores the key used to access the related value.
	heightKey := acceptedProposalHeightKey(epoch)
	// `cleared` stores the value produced by this operation.
	cleared := false
	n.execResultsMu.Lock()
	if n.acceptedProposal != nil && strings.TrimSpace(n.acceptedProposal[heightKey]) == proposalKey {
		delete(n.acceptedProposal, heightKey)
		cleared = true
	}
	if n.quorumLockedProposal != nil && strings.TrimSpace(n.quorumLockedProposal[heightKey]) == proposalKey {
		delete(n.quorumLockedProposal, heightKey)
		cleared = true
	}
	if n.acceptedProposalBlocks != nil {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := n.acceptedProposalBlocks[proposalKey]; ok {
			delete(n.acceptedProposalBlocks, proposalKey)
			cleared = true
		}
		// `key` and `accepted` track the key used to access the related value.
		for key, accepted := range n.acceptedProposalBlocks {
			if accepted.ID == epoch && strings.EqualFold(strings.TrimSpace(accepted.BlockHash), strings.TrimSpace(block.BlockHash)) {
				delete(n.acceptedProposalBlocks, key)
				cleared = true
			}
		}
	}
	n.execResultsMu.Unlock()
	if !cleared {
		return false
	}
	n.clearConsensusLockedBlock(epoch)
	n.persistConsensusSafetyStateAsync("accepted_proposal_invalidated")
	if DebugConsensus {
		fmt.Printf("[EXEC-PROPOSAL-CLEAR] height=%d round=%d block=%s reason=%s\n",
			epoch, block.Round, ShortHash(block.BlockHash), strings.TrimSpace(reason))
	}
	return true
}

// executionCommitFailureInvalidatesProposal implements the execution commit failure invalidates proposal helper.
func executionCommitFailureInvalidatesProposal(err error) bool {
	if err == nil {
		return false
	}
	// `reason` stores the value produced by this operation.
	reason := err.Error()
	switch {
	case strings.Contains(reason, "quorum_metadata_"):
		return true
	case strings.Contains(reason, "required_quorum_"):
		return true
	case strings.Contains(reason, "active_ready_"):
		return true
	case strings.Contains(reason, "strict_quorum_"):
		return true
	case strings.Contains(reason, "execution_quorum_evidence_shortfall"):
		return true
	case strings.Contains(reason, "invalid_execution_result_signature"):
		return true
	case strings.Contains(reason, "execution_result_block_mismatch"):
		return true
	default:
		return false
	}
}

// clearLocalExecutionVoteMarkerForProposal implements the clear local execution vote marker for proposal helper.
func (n *Node) clearLocalExecutionVoteMarkerForProposal(epoch uint64, proposalKey string) bool {
	if n == nil || epoch == 0 || strings.TrimSpace(proposalKey) == "" {
		return false
	}
	// `scopedKey` stores the key used to access the related value.
	scopedKey := execPoolScopeKey(epoch, proposalKey)
	// `cleared` stores the value produced by this operation.
	cleared := false
	n.execResultsMu.Lock()
	// `byRound` stores the value produced by this operation.
	if byRound := n.localExecVoteByRound[epoch]; len(byRound) > 0 {
		// `round` and `key` track the key used to access the related value.
		for round, key := range byRound {
			key = strings.TrimSpace(key)
			if key == proposalKey || execPoolScopeKey(epoch, key) == scopedKey {
				delete(byRound, round)
				cleared = true
			}
		}
		if len(byRound) == 0 {
			delete(n.localExecVoteByRound, epoch)
		}
	}
	n.execResultsMu.Unlock()
	return cleared
}

// invalidateExecutionProposalAfterCommitFailure implements the invalidate execution proposal after commit failure helper.
func (n *Node) invalidateExecutionProposalAfterCommitFailure(epoch uint64, proposal Block, err error) {
	if n == nil || epoch == 0 || proposal.ID != epoch || !executionCommitFailureInvalidatesProposal(err) {
		return
	}
	// `reason` stores the value produced by this operation.
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		reason = "commit_verification_failed"
	}
	// `proposalKey` stores the key used to access the related value.
	proposalKey := proposalVoteKey(proposal.ID, proposal.Round, proposal.BlockHash, proposal.MempoolRoot, proposal.StateRoot)
	if proposalKey == "" {
		proposalKey = proposalSnapshotFromBlock(proposal).ProposalKey
	}
	// `clearedProposal` stores the value produced by this operation.
	clearedProposal := n.clearAcceptedProposalIfBlock(epoch, proposal, reason)
	// `clearedLeader` stores the value produced by this operation.
	clearedLeader := n.clearLeaderBlockIfBlock(epoch, proposal)
	// `clearedMarker` stores the value produced by this operation.
	clearedMarker := n.clearLocalExecutionVoteMarkerForProposal(epoch, proposalKey)
	if proposalKey != "" {
		clearExecPoolProposal(epoch, proposalKey)
	}
	log.Printf("[EXEC-PROPOSAL-INVALIDATE] height=%d round=%d block=%s reason=%s cleared_proposal=%t cleared_leader=%t cleared_local_vote=%t",
		epoch,
		proposal.Round,
		ShortHash(proposal.BlockHash),
		reason,
		clearedProposal,
		clearedLeader,
		clearedMarker,
	)
}

// acceptedProposalBlock implements the accepted proposal block helper.
func (n *Node) acceptedProposalBlock(epoch uint64) (Block, bool) {
	if n == nil || epoch == 0 {
		return Block{}, false
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	return n.acceptedProposalBlockLocked(epoch)
}

// acceptedProposalBlockLocked reads the accepted proposal block while execResultsMu is held.
func (n *Node) acceptedProposalBlockLocked(epoch uint64) (Block, bool) {
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, false
	}
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, false
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, false
	}
	return block, true
}

// executionVoteTargetBlock implements the execution vote target block helper.
func (n *Node) executionVoteTargetBlock(epoch uint64) (Block, bool) {
	// `block` and `keep` store the synchronization state protecting shared data.
	if block, _, keep, _ := n.quorumLockedProposalLockState(epoch); keep {
		return block, true
	}
	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := n.acceptedProposalBlock(epoch); ok {
		return block, true
	}
	return n.getLeaderBlock(epoch)
}

// candidateProposalBlocksForEpoch implements the candidate proposal blocks for epoch helper.
func (n *Node) candidateProposalBlocksForEpoch(epoch uint64) []Block {
	if n == nil || epoch == 0 {
		return nil
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]bool)
	// `blocks` stores the block data handled by this operation.
	blocks := make([]Block, 0, execRecentProposalWindow+1)
	// `lockedBlock`, `lockedOK`, `acceptedBlock`, and `acceptedOK` capture proposal state.
	var lockedBlock, acceptedBlock Block
	var lockedOK, acceptedOK bool
	// `extras` stores the value produced by this operation.
	extras := make([]Block, 0)
	n.execResultsMu.Lock()
	lockedBlock, lockedOK = n.quorumLockedProposalBlockLocked(epoch)
	acceptedBlock, acceptedOK = n.acceptedProposalBlockLocked(epoch)
	if n.acceptedProposalBlocks != nil {
		extras = make([]Block, 0, len(n.acceptedProposalBlocks))
		// `block` tracks the synchronization state protecting shared data.
		for _, block := range n.acceptedProposalBlocks {
			if block.ID == epoch {
				extras = append(extras, block)
			}
		}
	}
	n.execResultsMu.Unlock()

	addCandidate := func(block Block, requireSignedCommit bool) {
		if block.ID != epoch {
			return
		}
		if requireSignedCommit {
			if _, _, keep := n.proposalHasSignedCommitQuorum(block); !keep {
				return
			}
		}
		// `snap` stores the value produced by this operation.
		snap := proposalSnapshotFromBlock(block)
		if snap.ProposalKey == "" || seen[snap.ProposalKey] {
			return
		}
		seen[snap.ProposalKey] = true
		blocks = append(blocks, block)
	}

	if lockedOK {
		addCandidate(lockedBlock, true)
	}
	if acceptedOK {
		addCandidate(acceptedBlock, false)
	}

	// `committedBlock` and `ok` store whether the related condition is satisfied.
	if n.Blockchain != nil {
		if committedBlock, ok := n.Blockchain.GetBlock(epoch); ok && committedBlock.ID == epoch {
			// `snap` stores the value produced by this operation.
			snap := proposalSnapshotFromBlock(committedBlock)
			if snap.ProposalKey != "" && !seen[snap.ProposalKey] {
				extras = append(extras, committedBlock)
			}
		}
	}

	// `leaderBlock` and `ok` store whether the related condition is satisfied.
	if leaderBlock, ok := n.getLeaderBlock(epoch); ok && leaderBlock.ID == epoch {
		// `snap` stores the value produced by this operation.
		snap := proposalSnapshotFromBlock(leaderBlock)
		if snap.ProposalKey != "" && !seen[snap.ProposalKey] {
			extras = append(extras, leaderBlock)
		}
	}

	sort.Slice(extras, func(i, j int) bool {
		if extras[i].Round != extras[j].Round {
			return extras[i].Round > extras[j].Round
		}
		return proposalSnapshotFromBlock(extras[i]).ProposalKey < proposalSnapshotFromBlock(extras[j]).ProposalKey
	})
	for _, block := range extras {
		// `snap` stores the value produced by this operation.
		snap := proposalSnapshotFromBlock(block)
		if snap.ProposalKey != "" && !seen[snap.ProposalKey] {
			seen[snap.ProposalKey] = true
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// resolveExecutionVoteProposal implements the resolve execution vote proposal helper.
func (n *Node) resolveExecutionVoteProposal(height uint64, res ExecutionResultMsg) (Block, execProposalSnapshot, bool) {
	// `candidates` stores the value produced by this operation.
	candidates := n.candidateProposalBlocksForEpoch(height)
	if len(candidates) == 0 {
		return Block{}, execProposalSnapshot{}, false
	}
	if strings.TrimSpace(res.BlockHashHint) == "" && res.RoundHint == 0 && strings.TrimSpace(res.TxMerkle) == "" {
		// `snap` stores the value produced by this operation.
		snap := proposalSnapshotFromBlock(candidates[0])
		return candidates[0], snap, snap.ProposalKey != ""
	}
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range candidates {
		// `snap` stores the value produced by this operation.
		snap := proposalSnapshotFromBlock(block)
		if snap.ProposalKey == "" {
			continue
		}
		if voteBelongsToCurrentProposal(res, snap) {
			return block, snap, true
		}
	}
	return Block{}, execProposalSnapshot{}, false
}

// normalizeEmbeddedExecutionVoteHints normalizes embedded execution vote hints.
func normalizeEmbeddedExecutionVoteHints(res ExecutionResultMsg) ExecutionResultMsg {
	if res.Block == nil {
		return res
	}
	// `block` stores the synchronization state protecting shared data.
	block := *res.Block
	if res.BlockHashHint == "" {
		res.BlockHashHint = strings.TrimSpace(block.BlockHash)
	}
	if res.RoundHint == 0 && block.Round != 0 {
		res.RoundHint = block.Round
	}
	if res.TxMerkle == "" {
		res.TxMerkle = strings.TrimSpace(block.MempoolRoot)
	}
	return res
}

// stripEmbeddedExecutionVoteBlockForQueue implements the strip embedded execution vote block for queue helper.
func stripEmbeddedExecutionVoteBlockForQueue(res ExecutionResultMsg) ExecutionResultMsg {
	res = normalizeEmbeddedExecutionVoteHints(res)
	res.Block = nil
	return res
}

// embeddedExecutionVoteProposalKnown implements the embedded execution vote proposal known helper.
func (n *Node) embeddedExecutionVoteProposalKnown(block Block) bool {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return false
	}
	// `snap` stores the value produced by this operation.
	snap := proposalSnapshotFromBlock(block)
	if snap.ProposalKey != "" {
		n.execResultsMu.Lock()
		// `ok` stores whether the related condition is satisfied.
		_, ok := n.acceptedProposalBlocks[snap.ProposalKey]
		n.execResultsMu.Unlock()
		if ok {
			return true
		}
	}
	// `leader` and `ok` store whether the related condition is satisfied.
	if leader, ok := n.getLeaderBlock(block.ID); ok &&
		strings.EqualFold(strings.TrimSpace(leader.BlockHash), strings.TrimSpace(block.BlockHash)) {
		return true
	}
	return false
}

// allowEmbeddedExecutionVoteProposal implements the allow embedded execution vote proposal helper.
func (n *Node) allowEmbeddedExecutionVoteProposal(block Block) bool {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return false
	}
	if n.embeddedExecutionVoteProposalKnown(block) {
		return true
	}
	if maxEmbeddedProposalPerHeight <= 0 {
		return true
	}
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedReplayFenceHeight()
	// `hash` stores the digest used to identify or verify the related data.
	hash := strings.TrimSpace(block.BlockHash)

	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.embeddedProposalSeen == nil {
		n.embeddedProposalSeen = make(map[uint64]map[string]struct{})
	}
	// `height` tracks the current values while iterating.
	for height := range n.embeddedProposalSeen {
		if height <= committedHeight || height+execRecentProposalWindow < block.ID {
			delete(n.embeddedProposalSeen, height)
		}
	}
	if n.embeddedProposalSeen[block.ID] == nil {
		n.embeddedProposalSeen[block.ID] = make(map[string]struct{})
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.embeddedProposalSeen[block.ID][hash]; ok {
		return true
	}
	if len(n.embeddedProposalSeen[block.ID]) >= maxEmbeddedProposalPerHeight {
		return false
	}
	n.embeddedProposalSeen[block.ID][hash] = struct{}{}
	return true
}

// embeddedExecutionVoteParentHash implements the embedded execution vote parent hash helper.
func (n *Node) embeddedExecutionVoteParentHash(block Block) (string, bool) {
	if n == nil || n.Blockchain == nil || block.ID == 0 {
		return "", false
	}
	if block.ID == 1 {
		// `last` stores the value produced by this operation.
		if last := n.Blockchain.LastBlock(); last.ID == 0 && strings.TrimSpace(last.BlockHash) != "" {
			return strings.TrimSpace(last.BlockHash), true
		}
		if strings.TrimSpace(GenesisHash) != "" {
			return strings.TrimSpace(GenesisHash), true
		}
		return "", false
	}
	// `parentHeight` stores the value produced by this operation.
	parentHeight := block.ID - 1
	// `parent` and `ok` store whether the related condition is satisfied.
	if parent, ok := n.Blockchain.GetBlock(parentHeight); ok && strings.TrimSpace(parent.BlockHash) != "" {
		return strings.TrimSpace(parent.BlockHash), true
	}
	// `parent` and `ok` store whether the related condition is satisfied.
	if parent, ok := n.LoadBlock(int(parentHeight)); ok && strings.TrimSpace(parent.BlockHash) != "" {
		return strings.TrimSpace(parent.BlockHash), true
	}
	// `last` stores the value produced by this operation.
	if last := n.Blockchain.LastBlock(); last.ID == parentHeight && strings.TrimSpace(last.BlockHash) != "" {
		return strings.TrimSpace(last.BlockHash), true
	}
	return "", false
}

// validateEmbeddedExecutionVoteProposal validates embedded execution vote proposal.
func (n *Node) validateEmbeddedExecutionVoteProposal(res ExecutionResultMsg, block Block) (bool, string) {
	if block.ID == 0 || res.HeightHint == 0 || block.ID != res.HeightHint {
		return false, "height_mismatch"
	}
	if block.Height != 0 && block.Height != block.ID {
		return false, "height_alias_mismatch"
	}
	// `blockHash` stores the block data handled by this operation.
	blockHash := strings.TrimSpace(block.BlockHash)
	if blockHash == "" {
		return false, "missing_block_hash"
	}
	// `hashHint` stores the digest used to identify or verify the related data.
	if hashHint := strings.TrimSpace(res.BlockHashHint); hashHint == "" || !strings.EqualFold(blockHash, hashHint) {
		return false, "block_hash_hint_mismatch"
	}
	if block.Round != res.RoundHint {
		return false, "round_mismatch"
	}
	if strings.TrimSpace(block.StateRoot) == "" || !strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(res.ExecHash)) {
		return false, "state_root_mismatch"
	}
	if strings.TrimSpace(block.MempoolRoot) != strings.TrimSpace(res.TxMerkle) {
		return false, "tx_merkle_mismatch"
	}
	// `expectedResultHash` stores the digest used to identify or verify the related data.
	expectedResultHash := executionResultHashFromProposal(
		res.HeightHint,
		proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot),
		block.BlockHash,
		res.ExecHash,
		res.TxMerkle,
	)
	if strings.TrimSpace(res.ExecutionResultHash) == "" || !executionResultHashMatches(res.ExecutionResultHash, expectedResultHash) {
		return false, "execution_result_hash_mismatch"
	}
	// `parentHash` and `ok` store whether the related condition is satisfied.
	if parentHash, ok := n.embeddedExecutionVoteParentHash(block); ok &&
		!strings.EqualFold(strings.TrimSpace(block.PrevHash), parentHash) {
		return false, "parent_hash_mismatch"
	}
	if validatorSetCommitmentV2EnabledAt(block.ID) && strings.TrimSpace(block.ValidatorSetHash) == "" {
		return false, "missing_validator_set_hash"
	}
	// `expectedHash` and `source` store the digest used to identify or verify the related data.
	if expectedHash, source := n.expectedValidatorSetHashWithSource(block.ID); validatorSetSourceIsChainAuthoritative(source) && strings.TrimSpace(expectedHash) == "" {
		return false, "validator_set_expected_missing"
	} else if strings.TrimSpace(expectedHash) != "" && !strings.EqualFold(strings.TrimSpace(expectedHash), strings.TrimSpace(block.ValidatorSetHash)) {
		return false, "validator_set_hash_mismatch"
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorSetHashHeaderCommitment(block); err != nil {
		return false, "validator_set_header_" + err.Error()
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		return false, "next_validator_set_" + err.Error()
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		return false, "next_validator_set_root_" + err.Error()
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorSetRootCommitment(block); err != nil {
		return false, "validator_set_root_" + err.Error()
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		return false, "validator_registry_" + err.Error()
	}
	if !strings.EqualFold(strings.TrimSpace(HashBlock(block)), blockHash) {
		return false, "block_hash_mismatch"
	}
	// `err` stores the error produced by this operation.
	if err := VerifyMempoolRoot(block); err != nil {
		return false, "mempool_root_" + err.Error()
	}
	// `err` stores the error produced by this operation.
	if err := VerifyReceiptRoot(block); err != nil {
		return false, "receipt_root_" + err.Error()
	}
	return true, ""
}

// observeExecutionVoteProposalBlock implements the observe execution vote proposal block helper.
func (n *Node) observeExecutionVoteProposalBlock(res ExecutionResultMsg) (bool, string) {
	if n == nil || res.Block == nil || res.HeightHint == 0 {
		return false, "missing_block"
	}
	res = normalizeEmbeddedExecutionVoteHints(res)
	// `block` stores the synchronization state protecting shared data.
	block := *res.Block
	if block.ID != res.HeightHint || strings.TrimSpace(block.BlockHash) == "" {
		return false, "height_or_hash_missing"
	}
	// `hashHint` stores the digest used to identify or verify the related data.
	if hashHint := strings.TrimSpace(res.BlockHashHint); hashHint == "" || !strings.EqualFold(strings.TrimSpace(block.BlockHash), hashHint) {
		return false, "block_hash_hint_mismatch"
	}
	if !n.allowEmbeddedExecutionVoteProposal(block) {
		return false, "embedded_proposal_limit"
	}
	// `ok` and `reason` store whether the related condition is satisfied.
	if ok, reason := n.validateEmbeddedExecutionVoteProposal(res, block); !ok {
		return false, reason
	}
	if n.embeddedExecutionVoteProposalKnown(block) {
		return true, ""
	}
	if !n.verifyLeaderBlock(block, "") {
		return false, "leader_block_verification_failed"
	}
	if !n.storeLeaderBlock(block) {
		n.noteObservedProposal(block)
	}
	return true, ""
}

// proposalSnapshotForEpoch implements the proposal snapshot for epoch helper.
func (n *Node) proposalSnapshotForEpoch(height uint64) (execProposalSnapshot, bool) {
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.executionVoteTargetBlock(height)
	if !ok || block.ID != height {
		return execProposalSnapshot{}, false
	}
	// `snap` stores the value produced by this operation.
	snap := proposalSnapshotFromBlock(block)
	if strings.TrimSpace(snap.ProposalKey) == "" {
		return execProposalSnapshot{}, false
	}
	return snap, true
}

// prepareExecutionBroadcastForBlock implements the prepare execution broadcast for block helper.
func (n *Node) prepareExecutionBroadcastForBlock(block Block, execHash string, txMerkle string) (execBroadcastContext, bool) {
	// `heightHint` stores the value produced by this operation.
	heightHint := block.ID
	// `currentRuntimeLedgerHash`, `currentExecutionLedgerHash`, `tipHeight`, and `tipHash` store the digest used to identify or verify the related data.
	currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
	// `ctx` stores the context controlling this operation.
	ctx := execBroadcastContext{
		HeightHint:          heightHint,
		ExecHash:            strings.TrimSpace(execHash),
		TxMerkle:            strings.TrimSpace(txMerkle),
		RuntimeLedgerHash:   currentRuntimeLedgerHash,
		ExecutionLedgerHash: currentExecutionLedgerHash,
		TipHeight:           tipHeight,
		TipHash:             tipHash,
	}
	// `logDefer` stores the value produced by this operation.
	logDefer := func(reason string) (execBroadcastContext, bool) {
		log.Printf("[EXEC-BROADCAST-DEFER] validator=%s height=%d reason=%s exec=%s tx_merkle=%s current_runtime=%s current_execution=%s current_tip=%d/%s proposal=%s block=%s prev=%s",
			ShortID(n.ID),
			ctx.HeightHint,
			reason,
			ShortHash(ctx.ExecHash),
			ShortHash(ctx.TxMerkle),
			ShortHash(ctx.RuntimeLedgerHash),
			ShortHash(ctx.ExecutionLedgerHash),
			ctx.TipHeight,
			ShortHash(ctx.TipHash),
			ctx.ProposalKey,
			ShortHash(ctx.BlockHashHint),
			ShortHash(ctx.PrevHash),
		)
		return ctx, false
	}

	// `snap` stores the value produced by this operation.
	snap := proposalSnapshotFromBlock(block)
	if strings.TrimSpace(snap.ProposalKey) == "" {
		return logDefer("missing_proposal")
	}
	ctx.RoundHint = snap.Round
	ctx.BlockHashHint = strings.TrimSpace(snap.BlockHash)
	ctx.Block = &block
	ctx.TxCount = len(block.Transactions)
	ctx.PrevHash = strings.TrimSpace(block.PrevHash)
	ctx.TxMerkle = strings.TrimSpace(block.MempoolRoot)
	// `proposalStateRoot` stores the digest used to identify or verify the related data.
	proposalStateRoot := strings.TrimSpace(snap.StateRoot)
	if proposalStateRoot == "" {
		proposalStateRoot = strings.TrimSpace(block.StateRoot)
	}
	ctx.ProposalKey = proposalVoteKey(heightHint, snap.Round, snap.BlockHash, ctx.TxMerkle, proposalStateRoot)
	if ctx.TipHeight+1 != heightHint {
		return logDefer("tip_height_not_parent")
	}
	if ctx.TipHash == "" || !strings.EqualFold(ctx.PrevHash, ctx.TipHash) {
		return logDefer("tip_hash_mismatch")
	}
	// `currentExec` stores the value produced by this operation.
	currentExec := strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	if currentExec == "" {
		return logDefer("exec_unavailable")
	}
	if ctx.ExecHash != "" && !strings.EqualFold(ctx.ExecHash, currentExec) {
		return logDefer("exec_changed")
	}
	ctx.ExecHash = currentExec
	return ctx, true
}

// prepareExecutionBroadcast implements the prepare execution broadcast helper.
func (n *Node) prepareExecutionBroadcast(heightHint uint64, execHash string, txMerkle string) (execBroadcastContext, bool) {
	if heightHint == 0 {
		heightHint = n.currentEpoch()
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.executionVoteTargetBlock(heightHint)
	if !ok || block.ID != heightHint {
		// `currentRuntimeLedgerHash`, `currentExecutionLedgerHash`, `tipHeight`, and `tipHash` store the digest used to identify or verify the related data.
		currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
		// `ctx` stores the context controlling this operation.
		ctx := execBroadcastContext{
			HeightHint:          heightHint,
			ExecHash:            strings.TrimSpace(execHash),
			TxMerkle:            strings.TrimSpace(txMerkle),
			RuntimeLedgerHash:   currentRuntimeLedgerHash,
			ExecutionLedgerHash: currentExecutionLedgerHash,
			TipHeight:           tipHeight,
			TipHash:             tipHash,
		}
		log.Printf("[EXEC-BROADCAST-DEFER] validator=%s height=%d reason=missing_proposal exec=%s tx_merkle=%s current_runtime=%s current_execution=%s current_tip=%d/%s proposal=%s block=%s prev=%s",
			ShortID(n.ID),
			ctx.HeightHint,
			ShortHash(ctx.ExecHash),
			ShortHash(ctx.TxMerkle),
			ShortHash(ctx.RuntimeLedgerHash),
			ShortHash(ctx.ExecutionLedgerHash),
			ctx.TipHeight,
			ShortHash(ctx.TipHash),
			ctx.ProposalKey,
			ShortHash(ctx.BlockHashHint),
			ShortHash(ctx.PrevHash),
		)
		return ctx, false
	}
	return n.prepareExecutionBroadcastForBlock(block, execHash, txMerkle)
}

// currentProposalVoteKey returns current proposal vote key.
func (n *Node) currentProposalVoteKey(height uint64) string {
	// `snap` and `ok` store whether the related condition is satisfied.
	if snap, ok := n.proposalSnapshotForEpoch(height); ok && snap.ProposalKey != "" {
		return snap.ProposalKey
	}
	return legacyProposalVoteKey(height)
}

// voteProposalKey implements the vote proposal key helper.
func voteProposalKey(res ExecutionResultMsg) string {
	if res.HeightHint == 0 {
		return ""
	}
	if strings.TrimSpace(res.BlockHashHint) != "" {
		return proposalVoteKey(res.HeightHint, res.RoundHint, res.BlockHashHint, res.TxMerkle, res.ExecHash)
	}
	return legacyProposalVoteKey(res.HeightHint)
}

// voteBelongsToCurrentProposal implements the vote belongs to current proposal helper.
func voteBelongsToCurrentProposal(res ExecutionResultMsg, snap execProposalSnapshot) bool {
	if res.HeightHint == 0 || snap.Epoch == 0 {
		return false
	}
	if res.HeightHint != snap.Epoch {
		return false
	}
	// `bh` stores the value produced by this operation.
	if bh := strings.TrimSpace(res.BlockHashHint); bh != "" {
		if bh != snap.BlockHash {
			return false
		}
		if res.RoundHint != snap.Round {
			return false
		}
	} else if res.RoundHint > 0 && res.RoundHint != snap.Round {
		return false
	}
	if res.TxMerkle != "" && snap.TxMerkle != "" && res.TxMerkle != snap.TxMerkle {
		return false
	}
	return true
}

// localExecutionBroadcastReady implements the local execution broadcast ready helper.
func (n *Node) localExecutionBroadcastReady(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	if n.consensusRecomputePauseActive() {
		return false, "recompute_pause_active"
	}
	// `blocked` and `reason` store the block data handled by this operation.
	if blocked, reason, _ := n.consensusSyncGateForHeight(height); blocked {
		if reason == "" {
			reason = "syncing"
		}
		return false, reason
	}
	// `ready` and `reason` store the value produced by this operation.
	if ready, reason := n.validatorParticipationGateStatus(height); !ready {
		return false, reason
	}
	return true, ""
}

// localExecutionFinalityReady implements the local execution finality ready helper.
func (n *Node) localExecutionFinalityReady(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	if n.consensusRecomputePauseActive() {
		return false, "recompute_pause_active"
	}
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		// `syncing` stores the value produced by this operation.
		syncing := n.Consensus.Syncing
		// `syncInFlight` stores the value produced by this operation.
		syncInFlight := n.Consensus.syncInFlight
		// `paused` stores the value produced by this operation.
		paused := n.Consensus.Paused
		// `target` stores the value produced by this operation.
		target := n.Consensus.SyncTarget
		n.Consensus.mu.Unlock()
		if syncInFlight {
			return false, "sync_in_progress"
		}
		if syncing {
			return false, "syncing"
		}
		if target > 0 && n.Blockchain != nil {
			// `localHeight` stores the value produced by this operation.
			localHeight := n.Blockchain.Height()
			if target > localHeight && !nearSyncTip(localHeight, target) {
				return false, fmt.Sprintf("lagging_local_%d_target_%d", localHeight, target)
			}
		}
		if paused {
			return false, "consensus_paused"
		}
	}
	// `blocked` and `reason` store the block data handled by this operation.
	if blocked, reason, _ := n.consensusSyncGateForHeight(height); blocked {
		if reason == "" {
			reason = "syncing"
		}
		return false, reason
	}
	return true, "ready"
}

// maybeBroadcastExecutionVoteForBlock implements the maybe broadcast execution vote for block helper.
func (n *Node) maybeBroadcastExecutionVoteForBlock(block Block, trigger string) bool {
	return n.broadcastExecutionVoteForBlock(block, trigger, false, false)
}

// maybeRebroadcastExecutionVoteForBlock implements the maybe rebroadcast execution vote for block helper.
func (n *Node) maybeRebroadcastExecutionVoteForBlock(block Block, trigger string) bool {
	return n.broadcastExecutionVoteForBlock(block, trigger, true, true)
}

// broadcastExecutionVoteForBlock implements the broadcast execution vote for block helper.
func (n *Node) broadcastExecutionVoteForBlock(block Block, trigger string, force bool, requirePriorLocalVote bool) bool {
	if n == nil || block.ID == 0 || n.isShuttingDown() {
		return false
	}
	trigger = strings.TrimSpace(trigger)
	// `priorSnap` stores the value produced by this operation.
	priorSnap := execProposalSnapshot{}
	if requirePriorLocalVote {
		priorSnap = proposalSnapshotFromBlock(block)
		if priorSnap.ProposalKey == "" {
			return false
		}
	}
	// `ready` and `reason` store the value produced by this operation.
	if ready, reason := n.localExecutionBroadcastReady(block.ID); !ready {
		log.Printf("[EXEC-BROADCAST-DEFER] validator=%s height=%d reason=%s trigger=%s block=%s prev=%s",
			ShortID(n.ID),
			block.ID,
			reason,
			trigger,
			ShortHash(block.BlockHash),
			ShortHash(block.PrevHash),
		)
		return false
	}
	if block.ID != n.currentEpoch() {
		log.Printf("[EXEC-BROADCAST-DEFER] validator=%s height=%d reason=epoch_not_current trigger=%s block=%s prev=%s",
			ShortID(n.ID),
			block.ID,
			trigger,
			ShortHash(block.BlockHash),
			ShortHash(block.PrevHash),
		)
		return false
	}
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		log.Printf("[EXEC-BROADCAST-DEFER] validator=%s height=%d reason=exec_unavailable trigger=%s block=%s prev=%s",
			ShortID(n.ID),
			block.ID,
			trigger,
			ShortHash(block.BlockHash),
			ShortHash(block.PrevHash),
		)
		return false
	}
	if requirePriorLocalVote {
		if !n.hasExecBroadcastedByValidatorForResult(block.ID, priorSnap.ProposalKey, execHash, n.ID) {
			return false
		}
		if !n.shouldRebroadcastExec(block.ID, execVoteRebroadcastCooldown) {
			return false
		}
	}
	n.setLogicalTick(block.ID, TickExec)
	n.broadcastExecutionResultForBlockInternal(block, execHash, block.MempoolRoot, force)
	return true
}

// maybeBroadcastCurrentLeaderExecutionVote implements the maybe broadcast current leader execution vote helper.
func (n *Node) maybeBroadcastCurrentLeaderExecutionVote(trigger string) bool {
	if n == nil || n.isShuttingDown() {
		return false
	}
	// `epoch` stores the value produced by this operation.
	epoch := n.currentEpoch()
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.executionVoteTargetBlock(epoch)
	if !ok || block.ID != epoch {
		return false
	}
	return n.maybeBroadcastExecutionVoteForBlock(block, trigger)
}

// onLeaderProposalReplaced implements the on leader proposal replaced helper.
func (n *Node) onLeaderProposalReplaced(epoch uint64, oldBlock Block, newBlock Block) {
	if n == nil || epoch == 0 {
		return
	}
	// `oldKey` stores the key used to access the related value.
	oldKey := proposalVoteKey(epoch, oldBlock.Round, oldBlock.BlockHash, oldBlock.MempoolRoot, oldBlock.StateRoot)
	// `newKey` stores the key used to access the related value.
	newKey := proposalVoteKey(epoch, newBlock.Round, newBlock.BlockHash, newBlock.MempoolRoot, newBlock.StateRoot)
	if oldKey == "" || oldKey == newKey {
		return
	}
	if DebugConsensus {
		fmt.Printf("[EXEC-PROPOSAL] epoch=%d round=%d block=%s state=%s\n",
			epoch, newBlock.Round, ShortHash(newBlock.BlockHash), ShortHash(newBlock.StateRoot))
	}
}

// execResultSignBytes implements the exec result sign bytes helper.
func execResultSignBytes(heightHint uint64, execHash string, txMerkle string) []byte {
	return []byte(fmt.Sprintf("%d|%s|%s", heightHint, execHash, txMerkle))
}

// execResultSignBytesV2 implements the exec result sign bytes v2 helper.
func execResultSignBytesV2(heightHint uint64, roundHint uint32, blockHashHint string, execHash string, txMerkle string) []byte {
	return []byte(fmt.Sprintf("%d|%d|%s|%s|%s", heightHint, roundHint, blockHashHint, execHash, txMerkle))
}

// verifyExecutionResultSignature verifies execution result signature.
func verifyExecutionResultSignature(res ExecutionResultMsg, candidates []ed25519.PublicKey, sig []byte) bool {
	if len(candidates) == 0 || len(sig) == 0 {
		return false
	}
	// `isV2` stores the current position in the related collection.
	isV2 := res.SigVersion == execResultSigVersionV2 || strings.TrimSpace(res.BlockHashHint) != ""
	if isV2 {
		// `signBytes` stores the value produced by this operation.
		signBytes := execResultSignBytesV2(res.HeightHint, res.RoundHint, res.BlockHashHint, res.ExecHash, res.TxMerkle)
		// `pub` tracks the current values while iterating.
		for _, pub := range candidates {
			if ed25519.Verify(pub, signBytes, sig) {
				return true
			}
		}
		return false
	}

	// `signBytes` stores the value produced by this operation.
	signBytes := execResultSignBytes(res.HeightHint, res.ExecHash, res.TxMerkle)
	// `pub` tracks the current values while iterating.
	for _, pub := range candidates {
		if ed25519.Verify(pub, signBytes, sig) {
			return true
		}
	}
	return false
}

// commitVoteSignBytes implements the commit vote sign bytes helper.
func commitVoteSignBytes(height uint64, proposalHash string, execHash string, txMerkle string) []byte {
	return []byte(fmt.Sprintf("MSC_COMMIT_V1\x00%d\x00%s\x00%s\x00%s",
		height,
		strings.TrimSpace(proposalHash),
		strings.TrimSpace(execHash),
		strings.TrimSpace(txMerkle),
	))
}

func commitVoteSignBytesV2(height uint64, proposalHash string, execHash string, txMerkle string, executionCommitmentHash string) []byte {
	return []byte(fmt.Sprintf("MSC_COMMIT_V2\x00%d\x00%s\x00%s\x00%s\x00%s",
		height,
		strings.TrimSpace(proposalHash),
		strings.TrimSpace(execHash),
		strings.TrimSpace(txMerkle),
		strings.TrimSpace(executionCommitmentHash),
	))
}

func commitVoteSignBytesForMessage(cm CommitMsg) []byte {
	if strings.TrimSpace(cm.ExecutionCommitmentHash) != "" {
		return commitVoteSignBytesV2(cm.Height, cm.Hash, cm.ExecHash, cm.TxMerkle, cm.ExecutionCommitmentHash)
	}
	return commitVoteSignBytes(cm.Height, cm.Hash, cm.ExecHash, cm.TxMerkle)
}

// commitVoteResultScopeKey implements the commit vote result scope key helper.
func commitVoteResultScopeKey(height uint64, proposalHash string, execHash string, txMerkle string) string {
	// `scope` stores the value produced by this operation.
	scope := commitVoteScopeKey(height, proposalHash)
	// `resultHash` stores the digest used to identify or verify the related data.
	resultHash := canonicalExecutionResultHash(height, proposalHash, execHash, txMerkle)
	if scope == "" || resultHash == "" {
		return scope
	}
	return fmt.Sprintf("%s|%s", scope, resultHash)
}

// commitVoteProposalHashFromScope implements the commit vote proposal hash from scope helper.
func commitVoteProposalHashFromScope(height uint64, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	// `parts` stores the value produced by this operation.
	parts := strings.Split(key, "|")
	if len(parts) >= 3 && parts[0] == "block" {
		if height == 0 || strings.TrimSpace(parts[1]) == fmt.Sprintf("%d", height) {
			return strings.TrimSpace(parts[2])
		}
	}
	return key
}

// commitVoteScopeMatchesProposal implements the commit vote scope matches proposal helper.
func commitVoteScopeMatchesProposal(height uint64, key string, proposalHash string) bool {
	proposalHash = strings.TrimSpace(proposalHash)
	if proposalHash == "" {
		return false
	}
	key = strings.TrimSpace(key)
	return key == proposalHash ||
		key == commitVoteScopeKey(height, proposalHash) ||
		commitVoteProposalHashFromScope(height, key) == proposalHash
}

// verifyCommitVoteSignature verifies commit vote signature.
func verifyCommitVoteSignatureWithCandidates(cm CommitMsg, candidates []ed25519.PublicKey) bool {
	cm.From = normalizeValidatorID(cm.From)
	cm.Hash = strings.TrimSpace(cm.Hash)
	cm.ExecHash = strings.TrimSpace(cm.ExecHash)
	cm.TxMerkle = strings.TrimSpace(cm.TxMerkle)
	cm.ExecutionCommitmentHash = strings.TrimSpace(cm.ExecutionCommitmentHash)
	if cm.Height == 0 || cm.From == "" || cm.Hash == "" || cm.ExecHash == "" || strings.TrimSpace(cm.Signature) == "" {
		return false
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(strings.TrimSpace(cm.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	// `pub` tracks the current values while iterating.
	for _, pub := range candidates {
		if ed25519.Verify(pub, commitVoteSignBytesForMessage(cm), sig) {
			return true
		}
	}
	return false
}

// verifyCommitVoteSignature verifies commit vote signature.
func verifyCommitVoteSignature(cm CommitMsg) bool {
	return verifyCommitVoteSignatureWithCandidates(cm, execResultPubKeyCandidates(cm.From))
}

// verifyCommitVoteSignatureForHeight verifies commit votes with the same
// height-aware registry/core pubkey candidates used for execution-result votes.
func (n *Node) verifyCommitVoteSignatureForHeight(cm CommitMsg) bool {
	if n != nil {
		return verifyCommitVoteSignatureWithCandidates(cm, n.execResultPubKeyCandidatesForHeight(cm.From, cm.Height))
	}
	return verifyCommitVoteSignatureWithCandidates(cm, execResultPubKeyCandidates(cm.From))
}

// commitVoteEvidence implements the commit vote evidence helper.
func (n *Node) commitVoteEvidence(height uint64, proposalHash string) ([]string, []ValidatorSignature, int, int) {
	if n == nil || height == 0 || strings.TrimSpace(proposalHash) == "" {
		return nil, nil, 0, 0
	}
	proposalHash = strings.TrimSpace(proposalHash)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(height)
	n.commitMu.Lock()
	defer n.commitMu.Unlock()
	// `bySigner` stores the value used by this operation.
	var bySigner map[string]string
	// `key` and `candidate` track the key used to access the related value.
	for key, candidate := range n.commitVoteSignatures[height] {
		if !commitVoteScopeMatchesProposal(height, key, proposalHash) {
			continue
		}
		if len(candidate) > len(bySigner) {
			bySigner = candidate
		}
	}
	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(bySigner))
	// `signer` and `signature` track the current values while iterating.
	for signer, signature := range bySigner {
		if normalizeValidatorID(signer) == "" || strings.TrimSpace(signature) == "" {
			continue
		}
		signers = append(signers, normalizeValidatorID(signer))
	}
	signers = canonicalValidatorIDs(signers)
	// `witnesses` stores the value produced by this operation.
	witnesses := make([]ValidatorSignature, 0, len(signers))
	// `signer` tracks the current values while iterating.
	for _, signer := range signers {
		witnesses = append(witnesses, ValidatorSignature{
			Validator: signer,
			Signature: strings.TrimSpace(bySigner[signer]),
		})
	}
	return signers, witnesses, len(signers), required
}

// commitVoteEvidenceForResult implements the commit vote evidence for result helper.
func (n *Node) commitVoteEvidenceForResult(height uint64, proposalHash string, execHash string, txMerkle string) ([]string, []ValidatorSignature, int, int) {
	if n == nil || height == 0 || strings.TrimSpace(proposalHash) == "" || strings.TrimSpace(execHash) == "" {
		return nil, nil, 0, 0
	}
	// `key` stores the key used to access the related value.
	key := commitVoteResultScopeKey(height, proposalHash, execHash, txMerkle)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(height)
	n.commitMu.Lock()
	defer n.commitMu.Unlock()
	// `bySigner` stores the value produced by this operation.
	bySigner := n.commitVoteSignatures[height][key]
	if len(bySigner) == 0 {
		// Backward compatibility for safety journals written before commit
		// votes became result-scoped. Only exact legacy keys are accepted here;
		// result-scoped buckets for a different execution outcome are never
		// aggregated into this result.
		if legacy := n.commitVoteSignatures[height][strings.TrimSpace(proposalHash)]; len(legacy) > 0 {
			bySigner = legacy
		} else if legacy := n.commitVoteSignatures[height][commitVoteScopeKey(height, proposalHash)]; len(legacy) > 0 {
			bySigner = legacy
		}
	}
	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(bySigner))
	// `signer` and `signature` track the current values while iterating.
	for signer, signature := range bySigner {
		if normalizeValidatorID(signer) == "" || strings.TrimSpace(signature) == "" {
			continue
		}
		signers = append(signers, normalizeValidatorID(signer))
	}
	signers = canonicalValidatorIDs(signers)
	// `witnesses` stores the value produced by this operation.
	witnesses := make([]ValidatorSignature, 0, len(signers))
	// `signer` tracks the current values while iterating.
	for _, signer := range signers {
		witnesses = append(witnesses, ValidatorSignature{
			Validator: signer,
			Signature: strings.TrimSpace(bySigner[signer]),
		})
	}
	return signers, witnesses, len(signers), required
}

// localSignedCommitChoice implements the local signed commit choice helper.
func (n *Node) localSignedCommitChoice(height uint64) string {
	// `choice` stores the value produced by this operation.
	choice := n.localSignedCommitChoiceSnapshot(height)
	return choice.ProposalHash
}

// localSignedCommitChoiceScope implements the local signed commit choice scope helper.
func (n *Node) localSignedCommitChoiceScope(height uint64) string {
	// `choice` stores the value produced by this operation.
	choice := n.localSignedCommitChoiceSnapshot(height)
	return choice.Scope
}

type localSignedCommitChoiceSnapshot struct {
	// `Scope` stores the value associated with this record.
	Scope string
	// `ProposalHash` stores the digest used to identify or verify the related data.
	ProposalHash string
	// `Round` stores the value associated with this record.
	Round uint32
	// `RoundKnown` stores the value associated with this record.
	RoundKnown bool
	// `Count` stores the measured quantity used by this operation.
	Count int
	// `Quorum` stores the value associated with this record.
	Quorum bool
}

// localSignedCommitChoiceSnapshot implements the local signed commit choice snapshot helper.
func (n *Node) localSignedCommitChoiceSnapshot(height uint64) localSignedCommitChoiceSnapshot {
	if n == nil || height == 0 {
		return localSignedCommitChoiceSnapshot{}
	}
	// `localID` stores the local consensus identity selected for this height.
	localID := n.localConsensusValidatorIDForHeight(height)
	if localID == "" {
		return localSignedCommitChoiceSnapshot{}
	}
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(height)
	// `candidates` stores the value produced by this operation.
	candidates := make([]localSignedCommitChoiceSnapshot, 0, 2)
	n.commitMu.Lock()
	// `proposalKey` and `bySigner` track the key used to access the related value.
	for proposalKey, bySigner := range n.commitVoteSignatures[height] {
		if strings.TrimSpace(bySigner[localID]) != "" {
			// `count` stores the measured quantity used by this operation.
			count := 0
			// `signature` tracks the current values while iterating.
			for _, signature := range bySigner {
				if strings.TrimSpace(signature) != "" {
					count++
				}
			}
			// `scope` stores the value produced by this operation.
			scope := strings.TrimSpace(proposalKey)
			candidates = append(candidates, localSignedCommitChoiceSnapshot{
				Scope:        scope,
				ProposalHash: commitVoteProposalHashFromScope(height, scope),
				Count:        count,
				Quorum:       required > 0 && count >= required,
			})
		}
	}
	n.commitMu.Unlock()
	if len(candidates) == 0 {
		return localSignedCommitChoiceSnapshot{}
	}
	// `i` tracks the current position in the related collection.
	for i := range candidates {
		// `round` and `ok` store whether the related condition is satisfied.
		if round, ok := n.proposalRoundForHash(height, candidates[i].ProposalHash); ok {
			candidates[i].Round = round
			candidates[i].RoundKnown = true
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		// `a` stores the value produced by this operation.
		a := candidates[i]
		// `b` stores the value produced by this operation.
		b := candidates[j]
		if a.Quorum != b.Quorum {
			return a.Quorum
		}
		if a.RoundKnown != b.RoundKnown {
			return a.RoundKnown
		}
		if a.Round != b.Round {
			return a.Round > b.Round
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Scope < b.Scope
	})
	return candidates[0]
}

func (n *Node) signedCommitQuorumChoiceSnapshot(height uint64) localSignedCommitChoiceSnapshot {
	if n == nil || height == 0 {
		return localSignedCommitChoiceSnapshot{}
	}
	required := n.executionQuorumRequiredForEpoch(height)
	if required <= 0 {
		return localSignedCommitChoiceSnapshot{}
	}
	candidates := make([]localSignedCommitChoiceSnapshot, 0, 2)
	n.commitMu.Lock()
	for scope, bySigner := range n.commitVoteSignatures[height] {
		count := 0
		for signer, signature := range bySigner {
			if normalizeValidatorID(signer) != "" && strings.TrimSpace(signature) != "" {
				count++
			}
		}
		if count < required {
			continue
		}
		proposalHash := commitVoteProposalHashFromScope(height, scope)
		if proposalHash == "" {
			continue
		}
		candidates = append(candidates, localSignedCommitChoiceSnapshot{
			Scope:        strings.TrimSpace(scope),
			ProposalHash: proposalHash,
			Count:        count,
			Quorum:       true,
		})
	}
	n.commitMu.Unlock()
	if len(candidates) == 0 {
		return localSignedCommitChoiceSnapshot{}
	}
	for i := range candidates {
		if round, ok := n.proposalRoundForHash(height, candidates[i].ProposalHash); ok {
			candidates[i].Round = round
			candidates[i].RoundKnown = true
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a := candidates[i]
		b := candidates[j]
		if a.RoundKnown != b.RoundKnown {
			return a.RoundKnown
		}
		if a.Round != b.Round {
			return a.Round > b.Round
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Scope < b.Scope
	})
	return candidates[0]
}

// hasLocalSignedCommitScope implements the has local signed commit scope helper.
func (n *Node) hasLocalSignedCommitScope(height uint64, scope string) bool {
	// `signature` and `ok` store whether the related condition is satisfied.
	signature, ok := n.localSignedCommitSignature(height, scope)
	return ok && strings.TrimSpace(signature) != ""
}

// localSignedCommitSignature implements the local signed commit signature helper.
func (n *Node) localSignedCommitSignature(height uint64, scope string) (string, bool) {
	if n == nil || height == 0 {
		return "", false
	}
	// `localID` stores the local consensus identity selected for this height.
	localID := n.localConsensusValidatorIDForHeight(height)
	scope = strings.TrimSpace(scope)
	if localID == "" || scope == "" {
		return "", false
	}
	n.commitMu.Lock()
	defer n.commitMu.Unlock()
	// `signature` stores the value produced by this operation.
	signature := strings.TrimSpace(n.commitVoteSignatures[height][scope][localID])
	return signature, signature != ""
}

// signedCommitQuorumForHash implements the signed commit quorum for hash helper.
func (n *Node) signedCommitQuorumForHash(height uint64, proposalHash string) bool {
	// `count` and `required` store the measured quantity used by this operation.
	_, _, count, required := n.commitVoteEvidence(height, proposalHash)
	return required > 0 && count >= required
}

// proposalRoundForHash implements the proposal round for hash helper.
func (n *Node) proposalRoundForHash(height uint64, proposalHash string) (uint32, bool) {
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.proposalBlockByHash(height, proposalHash)
	if !ok {
		return 0, false
	}
	return block.Round, true
}

// shouldFollowCommitEvidence implements the should follow commit evidence helper.
func (n *Node) shouldFollowCommitEvidence(height uint64, proposalHash string, count int, required int) bool {
	if n == nil || height == 0 || strings.TrimSpace(proposalHash) == "" || required <= 1 {
		return false
	}
	if height != n.currentEpoch() || count <= 0 || count >= required {
		return false
	}
	// `localChoice` stores the value produced by this operation.
	localChoice := n.localSignedCommitChoice(height)
	if localChoice == "" {
		// `accepted` and `ok` store whether the related condition is satisfied.
		if accepted, ok := n.acceptedProposalBlock(height); ok && !strings.EqualFold(strings.TrimSpace(accepted.BlockHash), strings.TrimSpace(proposalHash)) {
			// `incomingRound` and `incomingKnown` store the current position in the related collection.
			incomingRound, incomingKnown := n.proposalRoundForHash(height, proposalHash)
			return incomingKnown && incomingRound > accepted.Round
		}
		return true
	}
	if strings.EqualFold(strings.TrimSpace(localChoice), strings.TrimSpace(proposalHash)) {
		return true
	}
	if n.signedCommitQuorumForHash(height, localChoice) {
		return false
	}
	// `incomingRound` and `incomingKnown` store the current position in the related collection.
	incomingRound, incomingKnown := n.proposalRoundForHash(height, proposalHash)
	// `localRound` and `localKnown` store the value produced by this operation.
	localRound, localKnown := n.proposalRoundForHash(height, localChoice)
	return incomingKnown && localKnown && incomingRound > localRound
}

// recordVerifiedCommitVote implements the record verified commit vote helper.
func (n *Node) recordVerifiedCommitVote(cm CommitMsg) (int, int, bool) {
	if n == nil || !n.verifyCommitVoteSignatureForHeight(cm) {
		return 0, 0, false
	}
	cm.From = normalizeValidatorID(cm.From)
	cm.Hash = strings.TrimSpace(cm.Hash)
	cm.Signature = strings.TrimSpace(cm.Signature)
	// `validators` and `ok` store whether the related condition is satisfied.
	validators, _, ok := n.deterministicCommitteeValidatorsForHeight(cm.Height)
	if !ok || len(validators) == 0 || !containsValidatorID(validators, cm.From) {
		return 0, n.executionQuorumRequiredForEpoch(cm.Height), false
	}
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(cm.Height)
	if required == 0 {
		return 0, 0, false
	}

	// `proposalRounds` stores the value produced by this operation.
	proposalRounds := make(map[string]uint32)
	if cm.Block.ID == cm.Height && strings.EqualFold(strings.TrimSpace(cm.Block.BlockHash), cm.Hash) {
		proposalRounds[cm.Hash] = cm.Block.Round
	}
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range n.candidateProposalBlocksForEpoch(cm.Height) {
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.TrimSpace(block.BlockHash)
		if block.ID == cm.Height && hash != "" {
			proposalRounds[hash] = block.Round
		}
	}
	// `incomingRound` and `incomingRoundKnown` store the current position in the related collection.
	incomingRound, incomingRoundKnown := proposalRounds[cm.Hash]
	// `commitScope` stores the value produced by this operation.
	commitScope := commitVoteResultScopeKey(cm.Height, cm.Hash, cm.ExecHash, cm.TxMerkle)
	if commitScope == "" {
		return 0, required, false
	}

	n.commitMu.Lock()
	if n.commitVoteSignatures == nil {
		n.commitVoteSignatures = make(map[uint64]map[string]map[string]string)
	}
	if n.commitVoteSignatures[cm.Height] == nil {
		n.commitVoteSignatures[cm.Height] = make(map[string]map[string]string)
	}
	if n.commitVoteSignatures[cm.Height][commitScope] == nil {
		n.commitVoteSignatures[cm.Height][commitScope] = make(map[string]string)
	}
	// `replacedPrior` stores the value produced by this operation.
	replacedPrior := false
	// `proposalKey` and `bySigner` track the key used to access the related value.
	for proposalKey, bySigner := range n.commitVoteSignatures[cm.Height] {
		if proposalKey != commitScope && strings.TrimSpace(bySigner[cm.From]) != "" {
			// `proposalHash` stores the digest used to identify or verify the related data.
			proposalHash := commitVoteProposalHashFromScope(cm.Height, proposalKey)
			// `existingRound` and `existingRoundKnown` store the value produced by this operation.
			existingRound, existingRoundKnown := proposalRounds[proposalHash]
			// `existingCount` stores the measured quantity used by this operation.
			existingCount := 0
			// `signature` tracks the current values while iterating.
			for _, signature := range bySigner {
				if strings.TrimSpace(signature) != "" {
					existingCount++
				}
			}
			if existingCount >= required || !incomingRoundKnown || !existingRoundKnown || incomingRound <= existingRound {
				n.commitMu.Unlock()
				return 0, required, false
			}
			delete(bySigner, cm.From)
			if len(bySigner) == 0 {
				delete(n.commitVoteSignatures[cm.Height], proposalKey)
			}
			// `byHeight` stores the value produced by this operation.
			if byHeight := n.commitVotes[cm.Height]; byHeight != nil {
				// `signers` stores the value produced by this operation.
				if signers := byHeight[proposalKey]; signers != nil {
					delete(signers, cm.From)
					if len(signers) == 0 {
						delete(byHeight, proposalKey)
					}
				}
			}
			replacedPrior = true
		}
	}
	// `existing` stores the value produced by this operation.
	existing := strings.TrimSpace(n.commitVoteSignatures[cm.Height][commitScope][cm.From])
	n.commitVoteSignatures[cm.Height][commitScope][cm.From] = cm.Signature
	if n.commitVoted == nil {
		n.commitVoted = make(map[uint64]map[string]string)
	}
	if n.commitVoted[cm.Height] == nil {
		n.commitVoted[cm.Height] = make(map[string]string)
	}
	n.commitVoted[cm.Height][cm.From] = commitScope
	if n.commitVotes == nil {
		n.commitVotes = make(map[uint64]map[string]map[string]struct{})
	}
	if n.commitVotes[cm.Height] == nil {
		n.commitVotes[cm.Height] = make(map[string]map[string]struct{})
	}
	if n.commitVotes[cm.Height][commitScope] == nil {
		n.commitVotes[cm.Height][commitScope] = make(map[string]struct{})
	}
	n.commitVotes[cm.Height][commitScope][cm.From] = struct{}{}
	// `count` stores the measured quantity used by this operation.
	count := len(n.commitVoteSignatures[cm.Height][commitScope])
	n.commitMu.Unlock()
	if existing == "" || replacedPrior {
		n.persistConsensusSafetyStateAsync("signed_commit_vote")
	}
	// `scope` stores the value produced by this operation.
	scope := commitScope
	ExecPool.mu.Lock()
	if ExecPool.commitChoice == nil {
		ExecPool.commitChoice = make(map[uint64]map[string]string)
	}
	if ExecPool.commitChoice[cm.Height] == nil {
		ExecPool.commitChoice[cm.Height] = make(map[string]string)
	}
	// `prior` stores the value produced by this operation.
	if prior := strings.TrimSpace(ExecPool.commitChoice[cm.Height][cm.From]); prior == "" || prior == scope || replacedPrior {
		ExecPool.commitChoice[cm.Height][cm.From] = scope
	}
	ExecPool.mu.Unlock()
	return count, required, true
}

// broadcastCommitVoteForProposal implements the broadcast commit vote for proposal helper.
func (n *Node) broadcastCommitVoteForProposal(block Block, execHash string, txMerkle string) bool {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" || n.isShuttingDown() {
		return false
	}
	execHash = strings.TrimSpace(execHash)
	txMerkle = strings.TrimSpace(txMerkle)
	from := n.localConsensusValidatorIDForHeight(block.ID)
	if execHash == "" || from == "" {
		return false
	}
	// `cm` stores the value produced by this operation.
	cm := CommitMsg{
		Height:                  block.ID,
		Hash:                    strings.TrimSpace(block.BlockHash),
		ExecHash:                execHash,
		TxMerkle:                txMerkle,
		ExecutionCommitmentHash: executionCommitmentHashForBlock(block),
		Block:                   block,
		From:                    from,
	}
	// `commitScope` stores the value produced by this operation.
	commitScope := commitVoteResultScopeKey(cm.Height, cm.Hash, cm.ExecHash, cm.TxMerkle)
	if commitScope == "" {
		return false
	}
	// `cachedSignature` stores a prior local signature for the same result.
	cachedSignature, alreadySignedSameResult := n.localSignedCommitSignature(block.ID, commitScope)
	if alreadySignedSameResult {
		cm.Signature = cachedSignature
		if !n.verifyCommitVoteSignatureForHeight(cm) {
			alreadySignedSameResult = false
		}
	}
	if !alreadySignedSameResult {
		// `sig` and `ok` store whether the related condition is satisfied.
		sig, ok := n.signValidatorPayload(commitVoteSignBytesForMessage(cm))
		if !ok {
			return false
		}
		cm.Signature = hex.EncodeToString(sig)
		n.handleCommitMsg(cm)
	}
	// `msg` stores the value produced by this operation.
	msg := Message{Type: MsgCommit, Data: MustJSON(cm)}
	n.fanoutConsensusMessageToPeers(msg)
	// `data` stores the value produced by this operation.
	data, _ := MarshalP2PMessage(msg)
	// `publishTopic` stores the value produced by this operation.
	publishTopic := n.ConsensusTopic
	if publishTopic == nil {
		publishTopic = n.ValidatorTopic
	}
	if publishTopic != nil && !n.isShuttingDown() {
		_ = n.publishConsensusTopicWithTimeout(publishTopic, data)
	}
	return true
}

// rebroadcastCommitVoteForProposal sends an already-signed or newly-signed commit vote during recovery.
func (n *Node) rebroadcastCommitVoteForProposal(block Block, execHash string, txMerkle string, reason string) bool {
	if n == nil {
		return false
	}
	if n.shouldLogLivenessReason(fmt.Sprintf("commit_rebroadcast:%d:%s:%s", block.ID, ShortHash(block.BlockHash), strings.TrimSpace(reason)), 5*time.Second) {
		log.Printf("[COMMIT-REBROADCAST] validator=%s height=%d reason=%s block=%s exec=%s",
			ShortID(n.ID),
			block.ID,
			strings.TrimSpace(reason),
			ShortHash(block.BlockHash),
			ShortHash(execHash),
		)
	}
	return n.broadcastCommitVoteForProposal(block, execHash, txMerkle)
}

// publishExecutionResult implements the publish execution result helper.
func (n *Node) publishExecutionResult(ctx execBroadcastContext, force bool) {
	if n == nil || n.isShuttingDown() {
		return
	}
	// `heightHint` stores the value produced by this operation.
	heightHint := ctx.HeightHint
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := ctx.ExecHash
	// `txMerkle` stores the transaction data handled by this operation.
	txMerkle := ctx.TxMerkle
	// `proposalKey` stores the key used to access the related value.
	proposalKey := ctx.ProposalKey
	// `roundHint` stores the value produced by this operation.
	roundHint := ctx.RoundHint
	// `blockHashHint` stores the block data handled by this operation.
	blockHashHint := ctx.BlockHashHint
	// Execution votes are consensus messages, so their signer must be the
	// validator identity rather than the independent network node identity.
	signerID := n.localConsensusValidatorIDForHeight(heightHint)
	if signerID == "" {
		return
	}

	if !n.allowLocalExecutionVoteRound(heightHint, roundHint, proposalKey) {
		return
	}
	if !force {
		if !n.markExecBroadcastedByValidatorForResult(heightHint, proposalKey, execHash, signerID) {
			return
		}
		if !n.markExecBroadcasted(heightHint, proposalKey, execHash, txMerkle) {
			return
		}
	}
	log.Printf("[EXEC-BROADCAST] validator=%s height=%d round=%d block=%s prev=%s tx_count=%d tx_merkle=%s exec=%s current_runtime=%s current_execution=%s current_tip=%d/%s proposal=%s force=%t",
		ShortID(n.ID),
		heightHint,
		roundHint,
		ShortHash(blockHashHint),
		ShortHash(ctx.PrevHash),
		ctx.TxCount,
		ShortHash(txMerkle),
		ShortHash(execHash),
		ShortHash(ctx.RuntimeLedgerHash),
		ShortHash(ctx.ExecutionLedgerHash),
		ctx.TipHeight,
		ShortHash(ctx.TipHash),
		proposalKey,
		force,
	)

	// `sigVersion` stores the value produced by this operation.
	sigVersion := execResultSigVersionV1
	// `sigBytes` stores the value produced by this operation.
	sigBytes := execResultSignBytes(heightHint, execHash, txMerkle)
	if blockHashHint != "" {
		sigVersion = execResultSigVersionV2
		sigBytes = execResultSignBytesV2(heightHint, roundHint, blockHashHint, execHash, txMerkle)
	}
	// `signature` stores the value produced by this operation.
	signature := ""
	// `sig` and `ok` store whether the related condition is satisfied.
	if sig, ok := n.signValidatorPayload(sigBytes); ok {
		signature = hex.EncodeToString(sig)
	}
	// `executionResultHash` stores the digest used to identify or verify the related data.
	executionResultHash := executionResultHashFromProposal(heightHint, proposalKey, blockHashHint, execHash, txMerkle)

	// `msg` stores the value produced by this operation.
	msg := Message{
		Type: MsgExecutionResult,
		Data: MustJSON(ExecutionResultMsg{
			HeightHint:          heightHint,
			RoundHint:           roundHint,
			BlockHashHint:       blockHashHint,
			Block:               ctx.Block,
			SigVersion:          sigVersion,
			ExecHash:            execHash,
			TxMerkle:            txMerkle,
			ExecutionResultHash: executionResultHash,
			Signer:              signerID,
			Signature:           signature,
		}),
	}
	// Production safety: do not rely on pubsub loopback for the local
	// validator's own execution vote. Record it immediately so transport
	// hiccups cannot stall quorum formation.
	if !force || !execVoteCreditedGlobal(heightHint, proposalKey, signerID, execHash, txMerkle) {
		if n.isShuttingDown() {
			return
		}
		n.handleExecutionResultMsg(ExecutionResultMsg{
			HeightHint:          heightHint,
			RoundHint:           roundHint,
			BlockHashHint:       blockHashHint,
			Block:               ctx.Block,
			SigVersion:          sigVersion,
			ExecHash:            execHash,
			TxMerkle:            txMerkle,
			ExecutionResultHash: executionResultHash,
			Signer:              signerID,
			Signature:           signature,
		})
	}
	n.fanoutConsensusMessageToPeers(msg)
	n.noteExecBroadcastActivity(heightHint)

	// `data` stores the value produced by this operation.
	data, _ := MarshalP2PMessage(msg)
	// `publishTopic` stores the value produced by this operation.
	publishTopic := n.ConsensusTopic
	if publishTopic == nil {
		publishTopic = n.ValidatorTopic
	}
	if publishTopic != nil {
		if n.isShuttingDown() {
			return
		}
		_ = n.publishConsensusTopicWithTimeout(publishTopic, data)
	}
}

// broadcastExecutionResultForBlockInternal implements the broadcast execution result for block internal helper.
func (n *Node) broadcastExecutionResultForBlockInternal(block Block, execHash string, txMerkle string, force bool) {
	// `ready` and `reason` store the value produced by this operation.
	if ready, reason := n.localExecutionBroadcastReady(block.ID); !ready {
		log.Printf("[EXEC-BROADCAST-DEFER] validator=%s height=%d reason=%s trigger=direct_block_publish block=%s prev=%s",
			ShortID(n.ID),
			block.ID,
			reason,
			ShortHash(block.BlockHash),
			ShortHash(block.PrevHash),
		)
		return
	}
	// `ctx` and `ok` store whether the related condition is satisfied.
	ctx, ok := n.prepareExecutionBroadcastForBlock(block, execHash, txMerkle)
	if !ok {
		return
	}
	n.publishExecutionResult(ctx, force)
}

// broadcastExecutionResultInternal implements the broadcast execution result internal helper.
func (n *Node) broadcastExecutionResultInternal(heightHint uint64, execHash string, txMerkle string, force bool) {
	if n.ConsensusTopic == nil && n.ValidatorTopic == nil {
		return
	}
	if !n.canParticipateInConsensusNow() {
		// Rule 2: candidates cannot publish execution hashes.
		return
	}
	if validatorOnboardingStrictActivationEnabled() {
		// `active` and `reason` store the value produced by this operation.
		if active, reason := n.selfActiveValidatorAt(heightHint); !active {
			if DebugConsensus {
				fmt.Printf("[PROPOSAL-GATE] skipped exec-broadcast validator=%s height=%d reason=%s\n", ShortID(n.ID), heightHint, reason)
			}
			return
		}
	}
	if ConsensusProposeRequiresSyncReady {
		// `ready` and `reason` store the value produced by this operation.
		if ready, reason := n.syncReadyForConsensus(heightHint); !ready {
			if DebugConsensus {
				fmt.Printf("[PROPOSAL-GATE] skipped exec-broadcast validator=%s height=%d reason=%s\n", ShortID(n.ID), heightHint, reason)
			}
			return
		}
	}
	if ConsensusPostBlockSafeModeEnabled {
		// `active` stores the value produced by this operation.
		if active, _, _ := n.postBlockSafeModeState(heightHint); active {
			if DebugConsensus {
				fmt.Printf("[PROPOSAL-GATE] skipped exec-broadcast validator=%s height=%d reason=safe_mode_active\n", ShortID(n.ID), heightHint)
			}
			return
		}
	}
	// `ctx` and `ok` store whether the related condition is satisfied.
	ctx, ok := n.prepareExecutionBroadcast(heightHint, execHash, txMerkle)
	if !ok {
		return
	}
	n.publishExecutionResult(ctx, force)
}

// shouldRebroadcastExec implements the should rebroadcast exec helper.
func (n *Node) shouldRebroadcastExec(epoch uint64, cooldown time.Duration) bool {
	if epoch == 0 {
		return false
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Second
	}
	n.execRebroadcastMu.Lock()
	defer n.execRebroadcastMu.Unlock()
	if n.execRebroadcastAt == nil {
		n.execRebroadcastAt = make(map[uint64]time.Time)
	}
	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.execRebroadcastAt[epoch]; ok {
		if time.Since(last) < cooldown {
			return false
		}
	}
	n.execRebroadcastAt[epoch] = time.Now()
	return true
}

// noteExecBroadcastActivity implements the note exec broadcast activity helper.
func (n *Node) noteExecBroadcastActivity(epoch uint64) {
	if n == nil || epoch == 0 {
		return
	}
	n.execRebroadcastMu.Lock()
	defer n.execRebroadcastMu.Unlock()
	if n.execRebroadcastAt == nil {
		n.execRebroadcastAt = make(map[uint64]time.Time)
	}
	n.execRebroadcastAt[epoch] = time.Now()
}

// executionVoteNetworkIngressID implements the execution vote network ingress id helper.
func executionVoteNetworkIngressID(res ExecutionResultMsg) string {
	// `signer` stores the value produced by this operation.
	signer := normalizeValidatorID(res.Signer)
	if res.HeightHint == 0 || signer == "" {
		return ""
	}
	// `blockHash` stores the block data handled by this operation.
	blockHash := strings.TrimSpace(res.BlockHashHint)
	if blockHash == "" {
		blockHash = strings.TrimSpace(res.ExecHash)
	}
	if blockHash == "" {
		blockHash = strings.TrimSpace(res.TxMerkle)
	}
	if blockHash == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s:%s", res.HeightHint, res.RoundHint, signer, blockHash)
}

// markStaleExecutionVoteNetworkIngress implements the mark stale execution vote network ingress helper.
func (n *Node) markStaleExecutionVoteNetworkIngress(res ExecutionResultMsg) bool {
	if n == nil {
		return false
	}
	// `voteID` stores the value produced by this operation.
	voteID := executionVoteNetworkIngressID(res)
	if voteID == "" {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if n.execVoteStaleIngressSeen == nil {
		n.execVoteStaleIngressSeen = make(map[string]time.Time)
	}
	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.execVoteStaleIngressSeen[voteID]; ok && now.Sub(last) <= execVoteStaleIngressTTL {
		return true
	}
	n.execVoteStaleIngressSeen[voteID] = now
	if len(n.execVoteStaleIngressSeen) > ExecVoteReplayMaxKeys {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-execVoteStaleIngressTTL)
		// `key` and `seenAt` track the key used to access the related value.
		for key, seenAt := range n.execVoteStaleIngressSeen {
			if seenAt.Before(cutoff) {
				delete(n.execVoteStaleIngressSeen, key)
			}
		}
	}
	return false
}

// executionVoteTipLag implements the execution vote tip lag helper.
func executionVoteTipLag(currentEpoch uint64, voteHeight uint64) (uint64, bool) {
	if currentEpoch == 0 || voteHeight == 0 || voteHeight >= currentEpoch {
		return 0, false
	}
	// currentEpoch() returns the next height, so subtract one to compare
	// ingress lag against the node's current tip.
	return currentEpoch - voteHeight - 1, true
}

// executionVoteTooFarBehind implements the execution vote too far behind helper.
func executionVoteTooFarBehind(currentEpoch uint64, voteHeight uint64) bool {
	// `lag` and `ok` store whether the related condition is satisfied.
	lag, ok := executionVoteTipLag(currentEpoch, voteHeight)
	return ok && lag > execVoteStaleLagBlocks
}

// benignExecutionVoteIngressReason implements the benign execution vote ingress reason helper.
func benignExecutionVoteIngressReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "ignored_committed_vote", "ignored_committed_vote_cached", "ignored_late_vote", "ignored_late_vote_cached":
		return true
	default:
		return false
	}
}

// allowExecutionVoteNetworkIngress implements the allow execution vote network ingress helper.
func (n *Node) allowExecutionVoteNetworkIngress(res ExecutionResultMsg) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	res.Signer = normalizeValidatorID(res.Signer)
	if res.HeightHint == 0 || res.Signer == "" || strings.TrimSpace(res.ExecHash) == "" {
		return false, "invalid_vote"
	}
	// `currentEpoch` stores the value produced by this operation.
	currentEpoch := n.currentEpoch()
	lag, hasLag := executionVoteTipLag(currentEpoch, res.HeightHint)
	if hasLag && lag > execVoteStaleLagBlocks {
		if n.markStaleExecutionVoteNetworkIngress(res) {
			return false, "ignored_late_vote_cached"
		}
		return false, "ignored_late_vote"
	}
	if n.isCommittedReplayHeight(res.HeightHint) && (!hasLag || lag == 0) {
		if n.markStaleExecutionVoteNetworkIngress(res) {
			return false, "ignored_committed_vote_cached"
		}
		return false, "ignored_committed_vote"
	}
	// `voteID` stores the value produced by this operation.
	voteID := executionVoteNetworkIngressID(res)
	if voteID == "" {
		return true, ""
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if n.execVoteIngressSeen == nil {
		n.execVoteIngressSeen = make(map[string]time.Time)
	}
	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.execVoteIngressSeen[voteID]; ok && now.Sub(last) <= execVoteReplayTTL {
		// Treat rebroadcasts as idempotent at network ingress. The proposal-aware
		// replay/signer guards later in processing are authoritative; rejecting
		// here can starve quorum if an earlier copy was only queued or otherwise
		// failed to resolve into a credited vote.
		return true, ""
	}
	n.execVoteIngressSeen[voteID] = now
	if len(n.execVoteIngressSeen) > ExecVoteReplayMaxKeys {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-execVoteReplayTTL)
		// `key` and `seenAt` track the key used to access the related value.
		for key, seenAt := range n.execVoteIngressSeen {
			if seenAt.Before(cutoff) {
				delete(n.execVoteIngressSeen, key)
			}
		}
	}
	return true, ""
}

// logExecutionVoteIngressDrop implements the log execution vote ingress drop helper.
func (n *Node) logExecutionVoteIngressDrop(reason string, res ExecutionResultMsg, source string) {
	if n == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "ingress"
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("exec_ingress_drop:%s:%d:%d:%s:%s", reason, res.HeightHint, res.RoundHint, normalizeValidatorID(res.Signer), ShortHash(strings.TrimSpace(res.BlockHashHint)))
	if !n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
		return
	}
	log.Printf("[EXEC-INGRESS-DROP] source=%s reason=%s signer=%s height=%d round=%d block=%s exec=%s",
		source,
		reason,
		ShortID(res.Signer),
		res.HeightHint,
		res.RoundHint,
		ShortHash(strings.TrimSpace(res.BlockHashHint)),
		ShortHash(strings.TrimSpace(res.ExecHash)),
	)
}

// shouldForceExecutionVoteRebroadcast implements the should force execution vote rebroadcast helper.
func (n *Node) shouldForceExecutionVoteRebroadcast(epoch uint64, proposalKey string, votes int, cooldown time.Duration) bool {
	if n == nil || epoch == 0 || proposalKey == "" || votes <= 0 {
		return false
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	if cooldown <= 0 {
		cooldown = execVoteRebroadcastCooldown
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.execRebroadcastMu.Lock()
	defer n.execRebroadcastMu.Unlock()
	if n.execRebroadcastState == nil {
		n.execRebroadcastState = make(map[uint64]execVoteRebroadcastState)
	}
	// `state` stores the value produced by this operation.
	state := n.execRebroadcastState[epoch]
	if state.ProposalKey != proposalKey || state.VoteCount != votes {
		state.ProposalKey = proposalKey
		state.VoteCount = votes
		state.LastObservedAt = now
		n.execRebroadcastState[epoch] = state
		return false
	}
	if !state.LastObservedAt.IsZero() && now.Sub(state.LastObservedAt) < cooldown {
		return false
	}
	if !state.LastForcedAt.IsZero() && now.Sub(state.LastForcedAt) < cooldown {
		return false
	}
	state.LastForcedAt = now
	n.execRebroadcastState[epoch] = state
	return true
}

// markExecBroadcasted implements the mark exec broadcasted helper.
func (n *Node) markExecBroadcasted(epoch uint64, proposalKey string, execHash string, txMerkle string) bool {
	// `key` stores the key used to access the related value.
	key := execBroadcastKey(execPoolResultKey(epoch, proposalKey, execHash), txMerkle)
	n.commitMu.Lock()
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execBroadcasted == nil {
		n.execBroadcasted = make(map[uint64]map[string]bool)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execBroadcasted[epoch]; !ok {
		n.execBroadcasted[epoch] = make(map[string]bool)
	}
	if n.execBroadcasted[epoch][key] {
		return false
	}
	n.execBroadcasted[epoch][key] = true
	n.trimConsensusCachesLocked(committedHeight)
	return true
}

// execProposalMarkerKey implements the exec proposal marker key helper.
func execProposalMarkerKey(epoch uint64, proposalKey string, execHash string) string {
	if strings.TrimSpace(execHash) != "" {
		return execPoolResultKey(epoch, proposalKey, execHash)
	}
	return execPoolScopeKey(epoch, proposalKey)
}

// markExecBroadcastedByValidator implements the mark exec broadcasted by validator helper.
func (n *Node) markExecBroadcastedByValidator(epoch uint64, proposalKey string, signer string) bool {
	return n.markExecBroadcastedByValidatorForResult(epoch, proposalKey, "", signer)
}

// markExecBroadcastedByValidatorForResult implements the mark exec broadcasted by validator for result helper.
func (n *Node) markExecBroadcastedByValidatorForResult(epoch uint64, proposalKey string, execHash string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execProposalMarkerKey(epoch, proposalKey, execHash)
	n.commitMu.Lock()
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execBroadcastedByValidator == nil {
		n.execBroadcastedByValidator = make(map[uint64]map[string]map[string]bool)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execBroadcastedByValidator[epoch]; !ok {
		n.execBroadcastedByValidator[epoch] = make(map[string]map[string]bool)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execBroadcastedByValidator[epoch][proposalKey]; !ok {
		n.execBroadcastedByValidator[epoch][proposalKey] = make(map[string]bool)
	}
	if n.execBroadcastedByValidator[epoch][proposalKey][signer] {
		return false
	}
	n.execBroadcastedByValidator[epoch][proposalKey][signer] = true
	n.trimConsensusCachesLocked(committedHeight)
	return true
}

// hasExecBroadcastedByValidator implements the has exec broadcasted by validator helper.
func (n *Node) hasExecBroadcastedByValidator(epoch uint64, proposalKey string, signer string) bool {
	return n.hasExecBroadcastedByValidatorForResult(epoch, proposalKey, "", signer)
}

// hasExecBroadcastedByValidatorForResult implements the has exec broadcasted by validator for result helper.
func (n *Node) hasExecBroadcastedByValidatorForResult(epoch uint64, proposalKey string, execHash string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if n == nil || epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	originalProposalKey := proposalKey
	proposalKey = execProposalMarkerKey(epoch, proposalKey, execHash)
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execBroadcastedByValidator == nil {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execBroadcastedByValidator[epoch]; !ok {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execBroadcastedByValidator[epoch][proposalKey]; !ok {
		if strings.TrimSpace(execHash) == "" {
			prefix := execPoolScopeKey(epoch, originalProposalKey) + "|"
			for key, signers := range n.execBroadcastedByValidator[epoch] {
				if strings.HasPrefix(key, prefix) && signers[signer] {
					return true
				}
			}
		}
		return false
	}
	return n.execBroadcastedByValidator[epoch][proposalKey][signer]
}

// markExecSignerSeenForProposal implements the mark exec signer seen for proposal helper.
func (n *Node) markExecSignerSeenForProposal(epoch uint64, proposalKey string, signer string) bool {
	return n.markExecSignerSeenForProposalResult(epoch, proposalKey, "", signer)
}

// markExecSignerSeenForProposalResult implements the mark exec signer seen for proposal result helper.
func (n *Node) markExecSignerSeenForProposalResult(epoch uint64, proposalKey string, execHash string, signer string) bool {
	if epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execProposalMarkerKey(epoch, proposalKey, execHash)
	n.commitMu.Lock()
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execSignerSeen == nil {
		n.execSignerSeen = make(map[uint64]map[string]map[string]bool)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execSignerSeen[epoch]; !ok {
		n.execSignerSeen[epoch] = make(map[string]map[string]bool)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execSignerSeen[epoch][proposalKey]; !ok {
		n.execSignerSeen[epoch][proposalKey] = make(map[string]bool)
	}
	if n.execSignerSeen[epoch][proposalKey][signer] {
		return false
	}
	n.execSignerSeen[epoch][proposalKey][signer] = true
	n.trimConsensusCachesLocked(committedHeight)
	return true
}

// hasExecSignerSeenForProposal implements the has exec signer seen for proposal helper.
func (n *Node) hasExecSignerSeenForProposal(epoch uint64, proposalKey string, signer string) bool {
	return n.hasExecSignerSeenForProposalResult(epoch, proposalKey, "", signer)
}

// hasExecSignerSeenForProposalResult implements the has exec signer seen for proposal result helper.
func (n *Node) hasExecSignerSeenForProposalResult(epoch uint64, proposalKey string, execHash string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if n == nil || epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execProposalMarkerKey(epoch, proposalKey, execHash)
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execSignerSeen == nil {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execSignerSeen[epoch]; !ok {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.execSignerSeen[epoch][proposalKey]; !ok {
		return false
	}
	return n.execSignerSeen[epoch][proposalKey][signer]
}

// allowLocalExecutionVoteRound implements the allow local execution vote round helper.
func (n *Node) allowLocalExecutionVoteRound(epoch uint64, round uint32, proposalKey string) bool {
	if n == nil || epoch == 0 || proposalKey == "" {
		return false
	}
	proposalKey = strings.TrimSpace(proposalKey)
	n.execResultsMu.Lock()
	if n.localExecVoteByRound == nil {
		n.localExecVoteByRound = make(map[uint64]map[uint32]string)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.localExecVoteByRound[epoch]; !ok {
		n.localExecVoteByRound[epoch] = make(map[uint32]string)
	}
	// `existing` stores the value produced by this operation.
	existing := strings.TrimSpace(n.localExecVoteByRound[epoch][round])
	// Commit-choice resolution reads proposal state guarded by execResultsMu.
	// Snapshot local markers before resolving it to avoid self-deadlock.
	priorByRound := make(map[uint32]string, len(n.localExecVoteByRound[epoch]))
	// `existingRound` and `existingKey` track the key used to access the related value.
	for existingRound, existingKey := range n.localExecVoteByRound[epoch] {
		priorByRound[existingRound] = strings.TrimSpace(existingKey)
	}
	n.execResultsMu.Unlock()

	if existing == proposalKey {
		return true
	}
	if existing != "" {
		log.Printf("[EXEC-VOTE-GUARD] validator=%s height=%d round=%d action=skip_conflicting_round_vote existing=%s incoming=%s",
			ShortID(n.ID),
			epoch,
			round,
			existing,
			proposalKey,
		)
		return false
	}

	// Execution votes remain movable until this node signs a commit for a
	// proposal that already has execution quorum. At that point moving the
	// execution evidence can strand a later signed commit quorum.
	incomingScope := execPoolScopeKey(epoch, proposalKey)
	// `committedProposal` stores the value produced by this operation.
	committedProposal := n.localSignedCommitChoice(epoch)
	commitChoiceLocked := committedProposal != "" && n.signedCommitQuorumForHash(epoch, committedProposal)
	if !commitChoiceLocked && committedProposal != "" {
		if committedBlock, ok := n.proposalBlockByHash(epoch, committedProposal); ok {
			commitChoiceLocked = n.proposalHasLocalSignedExecutionQuorum(committedBlock, n.proposalVoteCount(committedBlock))
		}
	}
	if committedProposal != "" && commitChoiceLocked && commitVoteScopeKey(epoch, committedProposal) != incomingScope {
		log.Printf("[EXEC-VOTE-GUARD] validator=%s height=%d round=%d action=skip_conflicting_signed_commit_vote committed=%s incoming=%s",
			ShortID(n.ID),
			epoch,
			round,
			committedProposal,
			proposalKey,
		)
		return false
	}

	// `highestRound` stores the value used by this operation.
	var highestRound uint32
	// `hasPriorRound` stores the value produced by this operation.
	hasPriorRound := false
	// `existingRound` and `existingKey` track the key used to access the related value.
	for existingRound, existingKey := range priorByRound {
		if existingKey == "" {
			continue
		}
		if !hasPriorRound || existingRound > highestRound {
			highestRound = existingRound
			hasPriorRound = true
		}
	}
	if hasPriorRound && round < highestRound {
		log.Printf("[EXEC-VOTE-GUARD] validator=%s height=%d round=%d action=skip_lower_round_after_advance highest_round=%d incoming=%s",
			ShortID(n.ID),
			epoch,
			round,
			highestRound,
			proposalKey,
		)
		return false
	}

	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.localExecVoteByRound == nil {
		n.localExecVoteByRound = make(map[uint64]map[uint32]string)
	}
	if n.localExecVoteByRound[epoch] == nil {
		n.localExecVoteByRound[epoch] = make(map[uint32]string)
	}
	// `current` stores the value produced by this operation.
	if current := strings.TrimSpace(n.localExecVoteByRound[epoch][round]); current != "" && current != proposalKey {
		return false
	}
	// `existingRound` and `existingKey` track the key used to access the related value.
	for existingRound, existingKey := range n.localExecVoteByRound[epoch] {
		if strings.TrimSpace(existingKey) != "" && existingRound > round {
			return false
		}
	}
	n.localExecVoteByRound[epoch][round] = proposalKey
	return true
}

// execResultKey implements the exec result key helper.
func execResultKey(epoch uint64, execHash string, txMerkle string) string {
	return fmt.Sprintf("%d:%s:%s", epoch, execHash, txMerkle)
}

// execBroadcastKey implements the exec broadcast key helper.
func execBroadcastKey(execHash string, txMerkle string) string {
	return fmt.Sprintf("%s:%s", execHash, txMerkle)
}

// strictExecSupermajority implements the strict exec supermajority helper.
func strictExecSupermajority(total int) int {
	if total <= 0 {
		return 0
	}
	return (2*total)/3 + 1
}

// allowExecutionVoteIngress implements the allow execution vote ingress helper.
func (n *Node) allowExecutionVoteIngress(signer string, epoch uint64, proposalKey string, execHash string, txMerkle string) (bool, string) {
	if n == nil || epoch == 0 || proposalKey == "" || signer == "" || execHash == "" {
		return false, "invalid_vote"
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("%d:%s:%s:%s:%s", epoch, proposalKey, signer, execHash, txMerkle)
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()

	if n.execVoteSeen == nil {
		n.execVoteSeen = make(map[string]time.Time)
	}
	if n.execVoteLimiter == nil {
		n.execVoteLimiter = make(map[string]*rate.Limiter)
	}

	// `limiter` stores the value produced by this operation.
	limiter := n.execVoteLimiter[signer]
	if limiter == nil {
		// `interval` stores the value currently being processed.
		interval := time.Second / time.Duration(execVoteRatePerSigner)
		if interval <= 0 {
			interval = time.Second
		}
		limiter = rate.NewLimiter(rate.Every(interval), execVoteRateBurst)
		n.execVoteLimiter[signer] = limiter
	}
	if !limiter.Allow() {
		return false, "rate_limited"
	}

	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.execVoteSeen[key]; ok && now.Sub(last) <= execVoteReplayTTL && execVoteCreditedGlobal(epoch, proposalKey, signer, execHash, txMerkle) {
		return false, "replay_cache"
	}
	n.execVoteSeen[key] = now
	if len(n.execVoteSeen) > ExecVoteReplayMaxKeys {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-execVoteReplayTTL)
		// `k` and `seenAt` track the current values while iterating.
		for k, seenAt := range n.execVoteSeen {
			if seenAt.Before(cutoff) {
				delete(n.execVoteSeen, k)
			}
		}
	}
	return true, ""
}

// shouldThrottleExecutionVoteDrop implements the should throttle execution vote drop helper.
func shouldThrottleExecutionVoteDrop(reason string) bool {
	reason = strings.TrimSpace(reason)
	if strings.HasPrefix(reason, "queued_") {
		return true
	}
	switch reason {
	case "duplicate_exec_vote", "duplicate_signer_proposal", "rate_limited", "replay_cache", "stale_committed_height":
		return true
	default:
		return false
	}
}

// shouldLogExecutionVoteDrop implements the should log execution vote drop helper.
func (n *Node) shouldLogExecutionVoteDrop(reason string, res ExecutionResultMsg, proposalSnap execProposalSnapshot) bool {
	if n == nil || !shouldThrottleExecutionVoteDrop(reason) {
		return true
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("exec_vote_drop:%s:%s", reason, normalizeValidatorID(res.Signer))
	// Queue-state drops can arrive once per signer for every future height while
	// catching up. Keep their key coarse even in debug/file logging mode, or the
	// per-vote key defeats throttling and creates a sync-amplifying log storm.
	if !strings.HasPrefix(strings.TrimSpace(reason), "queued_") &&
		(DebugConsensus || DebugSync || log.Writer() != os.Stderr) {
		key = fmt.Sprintf("exec_vote_drop:%s:%d:%d:%s:%s:%s:%s",
			reason,
			res.HeightHint,
			res.RoundHint,
			normalizeValidatorID(res.Signer),
			ShortHash(strings.TrimSpace(res.BlockHashHint)),
			ShortHash(strings.TrimSpace(res.ExecHash)),
			ShortHash(strings.TrimSpace(proposalSnap.ProposalKey)),
		)
	}
	return n.shouldLogLivenessReason(key, livenessReasonLogCooldown)
}

// logExecutionVoteDrop implements the log execution vote drop helper.
func (n *Node) logExecutionVoteDrop(reason string, res ExecutionResultMsg, proposalSnap execProposalSnapshot) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	if !n.shouldLogExecutionVoteDrop(reason, res, proposalSnap) {
		return
	}
	log.Printf("[VOTE-DROP] reason=%s signer=%s height=%d round=%d vote_block=%s current_block=%s proposal=%s exec=%s tx_merkle=%s",
		reason,
		ShortID(res.Signer),
		res.HeightHint,
		res.RoundHint,
		ShortHash(res.BlockHashHint),
		ShortHash(proposalSnap.BlockHash),
		proposalSnap.ProposalKey,
		ShortHash(res.ExecHash),
		ShortHash(res.TxMerkle),
	)
}

// logExecutionVoteStaleAccept implements the log execution vote stale accept helper.
func (n *Node) logExecutionVoteStaleAccept(reason string, res ExecutionResultMsg) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "benign"
	}
	log.Printf("[VOTE-STALE-ACCEPT] reason=%s signer=%s height=%d round=%d vote_block=%s exec=%s tx_merkle=%s",
		reason,
		ShortID(res.Signer),
		res.HeightHint,
		res.RoundHint,
		ShortHash(res.BlockHashHint),
		ShortHash(res.ExecHash),
		ShortHash(res.TxMerkle),
	)
}

// logExecutionVoteAccept implements the log execution vote accept helper.
func (n *Node) logExecutionVoteAccept(reason string, res ExecutionResultMsg, proposalSnap execProposalSnapshot, votes int, required int) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "recorded"
	}
	if n != nil && !DebugConsensus && !DebugSync && log.Writer() == os.Stderr {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("exec_vote_accept:%s:%s", reason, normalizeValidatorID(res.Signer))
		if !n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			return
		}
	}
	log.Printf("[VOTE-ACCEPT] reason=%s signer=%s height=%d round=%d vote_block=%s current_block=%s proposal=%s exec=%s tx_merkle=%s votes=%d required=%d",
		reason,
		ShortID(res.Signer),
		res.HeightHint,
		res.RoundHint,
		ShortHash(res.BlockHashHint),
		ShortHash(proposalSnap.BlockHash),
		proposalSnap.ProposalKey,
		ShortHash(res.ExecHash),
		ShortHash(res.TxMerkle),
		votes,
		required,
	)
}

// tryFinalizeExecutionQuorumFromPool implements the try finalize execution quorum from pool helper.
func (n *Node) tryFinalizeExecutionQuorumFromPool(targetEpoch uint64, proposalSnap execProposalSnapshot, leaderBlock Block, execHash string, txMerkle string, reason string) bool {
	if n == nil || targetEpoch == 0 || execHash == "" || targetEpoch != n.currentEpoch() {
		return false
	}
	if leaderBlock.ID != targetEpoch || proposalSnap.BlockHash == "" || proposalSnap.BlockHash != leaderBlock.BlockHash {
		return false
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(targetEpoch, n.GetConsensusValidators(int(targetEpoch)))
	// `total` stores the measured quantity used by this operation.
	total := len(validators)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(targetEpoch)
	if required == 0 {
		required = execQuorumRequired(total)
	}
	if total == 0 || required == 0 {
		return false
	}
	storedCount := getExecCountGlobal(targetEpoch, proposalSnap.ProposalKey, execHash, txMerkle)
	results, signers, votes, ok := getExecResultsGlobal(targetEpoch, proposalSnap.ProposalKey, execHash, txMerkle)
	if !ok || votes < required || len(results) < required {
		results, signers, votes, ok = getExecResultsForBlockHashGlobal(targetEpoch, proposalSnap.BlockHash, execHash, txMerkle)
		if ok && votes > storedCount {
			storedCount = votes
		}
	}
	if !ok || votes < required || len(results) < required {
		results, signers, votes, ok = n.consensusExecutionResultsForBlock(leaderBlock, execHash, txMerkle)
		if ok && votes > storedCount {
			storedCount = votes
		}
	}
	if !ok || storedCount < required || len(results) < required {
		return false
	}
	// `result` tracks the result produced by this operation.
	for _, result := range results {
		n.mirrorConsensusExecVote(targetEpoch, proposalSnap.BlockHash, result)
	}
	_ = n.maybeAdoptProposalOnExecutionVote(leaderBlock)
	log.Printf("[EXEC-QUORUM-RECOVER] height=%d reason=%s block=%s exec=%s votes=%d required=%d",
		targetEpoch,
		strings.TrimSpace(reason),
		ShortHash(proposalSnap.BlockHash),
		ShortHash(execHash),
		storedCount,
		required,
	)
	commitScope := commitVoteResultScopeKey(targetEpoch, proposalSnap.BlockHash, execHash, txMerkle)
	localCommitAlreadySigned := commitScope != "" && n.hasLocalSignedCommitScope(targetEpoch, commitScope)
	broadcasted := true
	if localCommitAlreadySigned {
		broadcasted = n.rebroadcastCommitVoteForProposal(leaderBlock, execHash, txMerkle, "exec_quorum_recover")
	} else {
		broadcasted = n.broadcastCommitVoteForProposal(leaderBlock, execHash, txMerkle)
		if !broadcasted {
			broadcasted = n.rebroadcastCommitVoteForProposal(leaderBlock, execHash, txMerkle, "exec_quorum_recover_fallback")
		}
	}
	freezeExecPool(targetEpoch, proposalSnap.ProposalKey, execHash)
	if n.finalizeExecutionResult(targetEpoch, execHash, txMerkle, results, signers) {
		return true
	}
	_, _, commitCount, commitRequired := n.commitVoteEvidenceForResult(targetEpoch, proposalSnap.BlockHash, execHash, txMerkle)
	log.Printf("[EXEC-QUORUM-RECOVER-DEFER] height=%d reason=%s block=%s exec=%s commit_votes=%d required=%d rebroadcast=%t",
		targetEpoch,
		strings.TrimSpace(reason),
		ShortHash(proposalSnap.BlockHash),
		ShortHash(execHash),
		commitCount,
		commitRequired,
		broadcasted,
	)
	return broadcasted
}

// storeVoteButIgnoreForCommit keeps post-commit execution votes available for
// diagnostics/replay visibility without letting them trigger a second commit.
func (n *Node) storeVoteButIgnoreForCommit(epoch uint64, res ExecutionResultMsg, proposalSnap execProposalSnapshot, votes int) {
	if n == nil || epoch == 0 {
		return
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(epoch)
	if required == 0 {
		required = execQuorumRequired(len(validators))
	}
	n.logExecutionVoteAccept("recorded_committed", res, proposalSnap, votes, required)
	if n.Blockchain != nil && n.Blockchain.Height() >= epoch {
		return
	}
	_ = n.advanceConsensusToCommittedTip("committed_execution_vote")
}

// resetExecutionMismatchStrike implements the reset execution mismatch strike helper.
func (n *Node) resetExecutionMismatchStrike(signer string) {
	signer = normalizeValidatorID(signer)
	if n == nil || signer == "" {
		return
	}
	n.execVoteGuardMu.Lock()
	if n.execMismatch != nil {
		delete(n.execMismatch, signer)
	}
	n.execVoteGuardMu.Unlock()
}

// recordExecutionMismatchStrike implements the record execution mismatch strike helper.
func (n *Node) recordExecutionMismatchStrike(signer string, epoch uint64) int {
	signer = normalizeValidatorID(signer)
	if n == nil || signer == "" || epoch == 0 {
		return 0
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if n.execMismatch == nil {
		n.execMismatch = make(map[string]ExecMismatchTracker)
	}
	// `tracker` stores the value produced by this operation.
	tracker := n.execMismatch[signer]
	if tracker.LastAt.IsZero() || now.Sub(tracker.LastAt) > execMismatchStrikeWindow {
		tracker.Count = 0
	}
	if tracker.LastEpoch > 0 && epoch > tracker.LastEpoch+1 {
		tracker.Count = 0
	}
	tracker.Count++
	tracker.LastEpoch = epoch
	tracker.LastAt = now
	n.execMismatch[signer] = tracker
	if len(n.execMismatch) > ExecMismatchTrackMax {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-execMismatchStrikeWindow)
		// `id` and `st` track the current position in the related collection.
		for id, st := range n.execMismatch {
			if st.LastAt.Before(cutoff) {
				delete(n.execMismatch, id)
			}
		}
	}
	return tracker.Count
}

// executionMismatchUniqueSignersAtEpoch implements the execution mismatch unique signers at epoch helper.
func (n *Node) executionMismatchUniqueSignersAtEpoch(epoch uint64) int {
	if n == nil || epoch == 0 {
		return 0
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `cutoff` stores the value produced by this operation.
	cutoff := now.Add(-execMismatchStrikeWindow)
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if len(n.execMismatch) == 0 {
		return 0
	}
	// `unique` stores the value produced by this operation.
	unique := 0
	// `tracker` tracks the current values while iterating.
	for _, tracker := range n.execMismatch {
		if tracker.LastEpoch != epoch {
			continue
		}
		if tracker.LastAt.Before(cutoff) {
			continue
		}
		unique++
	}
	return unique
}

// disconnectValidatorPeers implements the disconnect validator peers helper.
func (n *Node) disconnectValidatorPeers(validatorID string, reason string) int {
	validatorID = normalizeValidatorID(validatorID)
	if n == nil || validatorID == "" {
		return 0
	}
	// `peers` stores the value used by this operation.
	var peers []string
	n.peerStateMu.Lock()
	// `peerID` and `vid` track the current values while iterating.
	for peerID, vid := range n.peerToValidator {
		if normalizeValidatorID(vid) == validatorID {
			peers = append(peers, peerID)
		}
	}
	n.peerStateMu.Unlock()
	// `peerID` tracks the current values while iterating.
	for _, peerID := range peers {
		n.disconnectPeerID(peerID, reason)
	}
	return len(peers)
}

// peerClaimsValidator implements the peer claims validator helper.
func (n *Node) peerClaimsValidator(peerID, validatorID string) bool {
	peerID = strings.TrimSpace(peerID)
	validatorID = normalizeValidatorID(validatorID)
	if n == nil || peerID == "" || validatorID == "" {
		return false
	}
	n.peerStateMu.Lock()
	// `claimed` stores the value produced by this operation.
	claimed := normalizeValidatorID(n.peerToValidator[peerID])
	n.peerStateMu.Unlock()
	return claimed == validatorID
}

// handleInvalidProposerPolicy handles invalid proposer policy.
func (n *Node) handleInvalidProposerPolicy(sourcePeer string, height uint64, expected string, got string) {
	got = normalizeValidatorID(got)
	expected = normalizeValidatorID(expected)
	if n == nil || got == "" || height == 0 {
		return
	}
	if n.isCorePendingAtHeight(got, height) {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=core_pending policy=invalid_proposer expected=%s got=%s\n",
				height, ShortID(expected), ShortID(got))
		}
		return
	}
	// `enforce` and `reason` store the value produced by this operation.
	if enforce, reason := n.canEnforceConsensusPenalty(height); !enforce {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=%s policy=invalid_proposer expected=%s got=%s\n",
				height, reason, ShortID(expected), ShortID(got))
		}
		return
	}
	if DebugConsensus {
		fmt.Printf("[PENALTY-GATE] height=%d enforce=true reason=converged policy=invalid_proposer\n", height)
	}
	// `strikes` stores the value produced by this operation.
	strikes := n.recordInvalidProposerStrike(got, height, expected, got)
	if DebugConsensus {
		fmt.Printf("Invalid proposer strike from %s @ height %d | strike=%d expected=%s got=%s\n",
			ShortID(got), height, strikes, ShortID(expected), ShortID(got))
	}
	// `quarantineAt` stores the value produced by this operation.
	quarantineAt := ConsensusInvalidProposerQuarantineAfter
	if quarantineAt <= 0 {
		quarantineAt = invalidProposerQuarantineAt
	}
	if strikes == quarantineAt {
		// `quarantined` stores the value produced by this operation.
		quarantined := n.disconnectValidatorPeers(got, "invalid_proposer_repeat")
		if DebugConsensus && quarantined > 0 {
			fmt.Printf("Invalid proposer quarantine applied to %s | peers=%d\n", ShortID(got), quarantined)
		}
	}
	if sourcePeer != "" && n.peerClaimsValidator(sourcePeer, got) {
		// `peerStrikes` stores the value produced by this operation.
		peerStrikes := n.recordInvalidProposerPeerStrike(sourcePeer, height, expected, got)
		if DebugConsensus {
			fmt.Printf("Invalid proposer peer strike %s | strike=%d validator=%s height=%d\n",
				sourcePeer, peerStrikes, ShortID(got), height)
		}
		// `peerQuarantineAt` stores the value produced by this operation.
		peerQuarantineAt := invalidProposerPeerQuarantineAt
		if quarantineAt > 0 {
			peerQuarantineAt = quarantineAt
		}
		if peerStrikes == peerQuarantineAt {
			n.disconnectPeerID(sourcePeer, "invalid_proposer_repeat_peer")
			if DebugConsensus {
				fmt.Printf("Invalid proposer peer quarantined %s | validator=%s\n", sourcePeer, ShortID(got))
			}
		}
	}
}

// handleExecutionMismatchPolicy handles execution mismatch policy.
func (n *Node) handleExecutionMismatchPolicy(signer string, epoch uint64, expected string, got string) {
	if n.isCorePendingAtHeight(signer, epoch) {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=core_pending policy=exec_mismatch signer=%s\n",
				epoch, ShortID(signer))
		}
		return
	}
	// `enforce` and `reason` store the value produced by this operation.
	if enforce, reason := n.canEnforceConsensusPenalty(epoch); !enforce {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=%s policy=exec_mismatch signer=%s\n",
				epoch, reason, ShortID(signer))
		}
		n.maybeSyncToBestObservedHeight("exec_mismatch_no_penalty")
		return
	}
	if DebugConsensus {
		fmt.Printf("[PENALTY-GATE] height=%d enforce=true reason=converged policy=exec_mismatch\n", epoch)
	}
	// `strikes` stores the value produced by this operation.
	strikes := n.recordExecutionMismatchStrike(signer, epoch)
	if strikes <= 0 {
		return
	}
	if DebugConsensus {
		fmt.Printf("Execution mismatch strike from %s @ epoch %d | strike=%d expected=%s got=%s\n",
			ShortID(signer), epoch, strikes, ShortHash(expected), ShortHash(got))
	}
	// `uniqueSigners` stores the value produced by this operation.
	uniqueSigners := n.executionMismatchUniqueSignersAtEpoch(epoch)
	if uniqueSigners >= 2 {
		log.Printf("[EXEC-MISMATCH] severity=high epoch=%d unique_signers=%d last_signer=%s expected=%s got=%s",
			epoch, uniqueSigners, ShortID(signer), ShortHash(expected), ShortHash(got))
		n.maybeSyncToBestObservedHeight("exec_mismatch_multi_signer")
	}
	// `quarantineAt` stores the value produced by this operation.
	quarantineAt := ConsensusExecMismatchQuarantineAfter
	if quarantineAt <= 0 {
		quarantineAt = execMismatchQuarantineAt
	}
	if strikes == quarantineAt {
		// `quarantined` stores the value produced by this operation.
		quarantined := n.disconnectValidatorPeers(signer, "exec_mismatch_repeat")
		if DebugConsensus && quarantined > 0 {
			fmt.Printf("Execution mismatch quarantine applied to %s | peers=%d\n", ShortID(signer), quarantined)
		}
		n.maybeSyncToBestObservedHeight("exec_mismatch_repeat")
		go n.forceSnapshotResyncNow(epoch, "exec_mismatch_repeat")
	}
	// `slashAt` stores the value produced by this operation.
	slashAt := ConsensusExecMismatchSlashAfter
	if slashAt <= 0 {
		slashAt = execMismatchSlashAt
	}
	if strikes == slashAt {
		n.RecordMisbehavior(signer, "exec_mismatch_repeat", int(epoch), expected)
		n.SlashValidator(signer)
	}
}

// handleExecutionEquivocationPolicy handles execution equivocation policy.
func (n *Node) handleExecutionEquivocationPolicy(signer string, epoch uint64, execHash string) {
	n.resetExecutionMismatchStrike(signer)
	if n.isCorePendingAtHeight(signer, epoch) {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=core_pending policy=exec_equivocation signer=%s\n",
				epoch, ShortID(signer))
		}
		return
	}
	// `enforce` and `reason` store the value produced by this operation.
	if enforce, reason := n.canEnforceConsensusPenalty(epoch); !enforce {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=%s policy=exec_equivocation signer=%s\n",
				epoch, reason, ShortID(signer))
		}
		n.maybeSyncToBestObservedHeight("exec_equivocation_no_penalty")
		return
	}
	n.RecordMisbehavior(signer, "exec_equivocation_signed", int(epoch), execHash)
	log.Printf("[EXEC-EQUIVOCATION-SOFT] signer=%s height=%d exec=%s action=record_evidence_keep_validator_mesh",
		ShortID(signer),
		epoch,
		ShortHash(execHash),
	)
	n.maybeSyncToBestObservedHeight("exec_equivocation")
	go n.forceSnapshotResyncNow(epoch, "exec_equivocation")
}

// executionVoteSignerLikelyStale implements the execution vote signer likely stale helper.
func (n *Node) executionVoteSignerLikelyStale(signer string, epoch uint64) bool {
	if n == nil || epoch == 0 {
		return false
	}
	signer = normalizeValidatorID(signer)
	if signer == "" {
		return false
	}
	// `st` and `ok` store whether the related condition is satisfied.
	st, ok := n.validatorStatusSnapshot(signer)
	if !ok {
		return false
	}
	// `observed` stores the value produced by this operation.
	observed := st.FinalizedHeight
	if st.ReportedHeight > observed {
		observed = st.ReportedHeight
	}
	if observed > 0 && observed+execVoteStaleLagBlocks+1 < epoch {
		return true
	}
	if st.ExecEpoch > 0 && st.ExecEpoch+1 < epoch {
		return true
	}
	if !st.LastSeen.IsZero() && time.Since(st.LastSeen) > 30*time.Second {
		return true
	}
	return false
}

// validatorStatusSnapshot implements the validator status snapshot helper.
func (n *Node) validatorStatusSnapshot(id string) (ValidatorStatus, bool) {
	id = normalizeValidatorID(id)
	if n == nil || id == "" {
		return ValidatorStatus{}, false
	}
	n.validatorMu.RLock()
	// `st` stores the value produced by this operation.
	st := n.validatorStatus[id]
	if st == nil {
		// `key` and `candidate` track the key used to access the related value.
		for key, candidate := range n.validatorStatus {
			if normalizeValidatorID(key) == id {
				st = candidate
				break
			}
		}
	}
	if st == nil {
		n.validatorMu.RUnlock()
		return ValidatorStatus{}, false
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := *st
	n.validatorMu.RUnlock()
	return snapshot, true
}

// appendExecResultPubKeyCandidate implements the append exec result pub key candidate helper.
func appendExecResultPubKeyCandidate(candidates []ed25519.PublicKey, pk ed25519.PublicKey) []ed25519.PublicKey {
	if len(pk) != ed25519.PublicKeySize {
		return candidates
	}
	// `existing` tracks the current values while iterating.
	for _, existing := range candidates {
		if bytes.Equal(existing, pk) {
			return candidates
		}
	}
	// `copied` stores the value produced by this operation.
	copied := make([]byte, len(pk))
	copy(copied, pk)
	return append(candidates, ed25519.PublicKey(copied))
}

// execResultPubKeyCandidates implements the exec result pub key candidates helper.
func execResultPubKeyCandidates(signer string) []ed25519.PublicKey {
	// `normalized` stores the value produced by this operation.
	normalized := normalizeValidatorID(signer)
	// `candidates` stores the value produced by this operation.
	candidates := make([]ed25519.PublicKey, 0, 4)
	validatorPubKeysMu.RLock()
	candidates = appendExecResultPubKeyCandidate(candidates, ValidatorPubKeys[normalized])
	candidates = appendExecResultPubKeyCandidate(candidates, ValidatorPubKeys[signer])
	candidates = appendExecResultPubKeyCandidate(candidates, GenesisValidatorPubKeys[normalized])
	candidates = appendExecResultPubKeyCandidate(candidates, GenesisValidatorPubKeys[signer])
	validatorPubKeysMu.RUnlock()
	return candidates
}

// execResultPubKeyCandidatesFromRegistry returns the consensus key committed
// for signer in the supplied validator-registry snapshot.
func execResultPubKeyCandidatesFromRegistry(snapshot map[string]ValidatorRecord, signer string) []ed25519.PublicKey {
	rec, ok := validatorRecordFromStakeSnapshot(snapshot, signer)
	if !ok {
		return nil
	}
	pubBytes, err := decodeConsensusPubKeyHex(rec.ConsensusPubKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return nil
	}
	return appendExecResultPubKeyCandidate(nil, ed25519.PublicKey(pubBytes))
}

// appendCoreRegistryExecResultPubKeyCandidate implements the append core registry exec result pub key candidate helper.
func (n *Node) appendCoreRegistryExecResultPubKeyCandidate(candidates []ed25519.PublicKey, signer string) []ed25519.PublicKey {
	if n == nil {
		return candidates
	}
	signer = normalizeValidatorID(signer)
	if signer == "" {
		return candidates
	}
	n.coreRegistryMu.RLock()
	// `entry` and `ok` store whether the related condition is satisfied.
	entry, ok := n.coreRegistryEntries[signer]
	n.coreRegistryMu.RUnlock()
	if !ok {
		return candidates
	}
	// `pubBytes` and `err` store the error produced by this operation.
	pubBytes, err := decodeConsensusPubKeyHex(entry.ConsensusPubKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return candidates
	}
	return appendExecResultPubKeyCandidate(candidates, ed25519.PublicKey(pubBytes))
}

// execResultPubKeyCandidatesForHeight implements the exec result pub key candidates for height helper.
func (n *Node) execResultPubKeyCandidatesForHeight(signer string, height uint64) []ed25519.PublicKey {
	if n == nil || height == 0 {
		return execResultPubKeyCandidates(signer)
	}
	// Once a committed parent/current registry exists, it is the exclusive
	// signature authority. Local runtime/core keys must never broaden it.
	if snapshot := n.committedProposerRegistrySnapshot(Block{ID: height}); len(snapshot) > 0 {
		return execResultPubKeyCandidatesFromRegistry(snapshot, signer)
	}
	if n.validatorRegistryCommitmentRequiredAt(height) {
		return nil
	}
	// Pre-commitment/bootstrap compatibility only.
	candidates := execResultPubKeyCandidates(signer)
	return n.appendCoreRegistryExecResultPubKeyCandidate(candidates, signer)
}

// recordExecResultGlobal implements the record exec result global helper.
func recordExecResultGlobal(epoch uint64, proposalKey string, execHash string, txMerkle string, res ExecutionResult) (int, bool, bool) {
	return recordExecResultGlobalWithRequired(epoch, proposalKey, execHash, txMerkle, res, 0)
}

// recordExecResultGlobalWithRequired implements the record exec result global with required helper.
func recordExecResultGlobalWithRequired(epoch uint64, proposalKey string, execHash string, txMerkle string, res ExecutionResult, requiredQuorum int) (int, bool, bool) {
	proposalKey = strings.TrimSpace(proposalKey)
	execHash = strings.TrimSpace(execHash)
	txMerkle = strings.TrimSpace(txMerkle)
	if epoch == 0 || execHash == "" || res.Signer == "" {
		return 0, false, false
	}
	// `poolScopeKey` stores the key used to access the related value.
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	// `scopedExecKey` stores the key used to access the related value.
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)
	// `signer` stores the value produced by this operation.
	signer := normalizeValidatorID(res.Signer)
	if signer == "" {
		return 0, false, false
	}
	res.Signer = signer
	if strings.TrimSpace(res.ResultHash) == "" {
		res.ResultHash = execHash
	}
	if strings.TrimSpace(res.TxMerkle) == "" {
		res.TxMerkle = txMerkle
	}
	// `expectedResultHash` stores the digest used to identify or verify the related data.
	expectedResultHash := executionResultHashFromProposal(epoch, proposalKey, res.BlockHash, execHash, txMerkle)
	if !executionResultHashMatches(res.ExecutionResultHash, expectedResultHash) {
		return 0, false, false
	}
	res.ExecutionResultHash = expectedResultHash

	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()
	ensureExecPoolTopMapsLocked()

	// `frozenHash` and `ok` store whether the related condition is satisfied.
	if frozenHash, ok := ExecPool.frozen[epoch][poolScopeKey]; ok && frozenHash != "" && frozenHash != execHash {
		// `byHash` and `ok` store whether the related condition is satisfied.
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	if execVoteCreditedGlobalLocked(epoch, poolScopeKey, signer, execHash, txMerkle) {
		// `byHash` and `ok` store whether the related condition is satisfied.
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := execPoolTxMerkleLocked(epoch, scopedExecKey); ok && existing != "" && existing != txMerkle {
		return execPoolResultCountLocked(epoch, scopedExecKey), false, false
	}

	// `choice` stores the value produced by this operation.
	choice := execBroadcastKey(execHash, txMerkle)
	// `epochChoice` stores the value produced by this operation.
	epochChoice := poolScopeKey + "|" + choice
	// `epochChoiceKey` stores the key used to access the related value.
	epochChoiceKey := execEpochChoiceSignerKey(res.Signer, proposalKey)
	if epochChoiceKey != "" {
		// `prev` and `exists` store whether the related condition is satisfied.
		if prev, exists := ExecPool.epochChoice[epoch][epochChoiceKey]; exists && prev != epochChoice {
			if !releaseStaleExecPoolSignerChoiceLocked(epoch, signer, proposalKey, poolScopeKey, choice, res.Round, requiredQuorum) {
				return 0, false, true
			}
		}
	}
	// `prev` and `exists` store whether the related condition is satisfied.
	if prev, exists := execPoolChoiceLocked(epoch, poolScopeKey, res.Signer); exists {
		if prev != choice {
			// Signed equivocation proof: same signer, same epoch+proposal, different exec vote.
			return 0, false, true
		}
		// `byHash` and `ok` store whether the related condition is satisfied.
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	if execPoolSignerKnownLocked(epoch, poolScopeKey, res.Signer) {
		// `byHash` and `ok` store whether the related condition is satisfied.
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	if !execPoolCanAdmitVoteLocked(epoch, poolScopeKey, scopedExecKey, res.Signer) {
		_ = pruneExecPoolLocked(0, nil)
		if !execPoolCanAdmitVoteLocked(epoch, poolScopeKey, scopedExecKey, res.Signer) {
			return execPoolResultCountLocked(epoch, scopedExecKey), false, false
		}
	}
	ensureExecPoolScopeMapsLocked(epoch, poolScopeKey)
	// `ok` stores whether the related condition is satisfied.
	if _, ok := ExecPool.txMerkle[epoch][scopedExecKey]; !ok {
		ExecPool.txMerkle[epoch][scopedExecKey] = txMerkle
	}
	if epochChoiceKey != "" {
		ExecPool.epochChoice[epoch][epochChoiceKey] = epochChoice
	}
	ExecPool.choice[epoch][poolScopeKey][res.Signer] = choice
	ExecPool.signers[epoch][poolScopeKey][res.Signer] = true

	// `ok` stores whether the related condition is satisfied.
	if _, ok := ExecPool.pool[epoch][scopedExecKey]; !ok {
		ExecPool.pool[epoch][scopedExecKey] = make(map[string]ExecutionResult)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := ExecPool.pool[epoch][scopedExecKey][res.Signer]; !ok {
		ExecPool.pool[epoch][scopedExecKey][res.Signer] = res
	}

	return len(ExecPool.pool[epoch][scopedExecKey]), true, false
}

// releaseStaleExecPoolSignerChoiceLocked implements the release stale exec pool signer choice locked helper.
func releaseStaleExecPoolSignerChoiceLocked(epoch uint64, signer string, incomingProposalKey string, incomingScope string, incomingChoice string, incomingRound uint32, requiredQuorum int) bool {
	if epoch == 0 || signer == "" || incomingScope == "" || incomingChoice == "" {
		return false
	}
	if requiredQuorum <= 0 {
		return false
	}
	// `round` and `ok` store whether the related condition is satisfied.
	if _, round, _, _, _, ok := proposalVoteKeyParts(incomingProposalKey); ok {
		incomingRound = round
	}
	// `previousScope` stores the value used by this operation.
	var previousScope string
	// `previousChoice` stores the value used by this operation.
	var previousChoice string
	// `previousRound` stores the value used by this operation.
	var previousRound uint32
	// `previousBestCount` stores the measured quantity used by this operation.
	previousBestCount := 0
	// `scope` and `bySigner` track the current values while iterating.
	for scope, bySigner := range ExecPool.choice[epoch] {
		// `choice` stores the value produced by this operation.
		choice := strings.TrimSpace(bySigner[signer])
		if choice == "" {
			continue
		}
		previousScope = scope
		previousChoice = choice
		// `byHash` and `ok` store whether the related condition is satisfied.
		if byHash, ok := ExecPool.pool[epoch]; ok {
			// `prefix` stores the value produced by this operation.
			prefix := scope + "|"
			// `key` and `results` track the key used to access the related value.
			for key, results := range byHash {
				if !strings.HasPrefix(key, prefix) {
					continue
				}
				if len(results) > previousBestCount {
					previousBestCount = len(results)
				}
				// `existing` and `ok` store whether the related condition is satisfied.
				if existing, ok := results[signer]; ok {
					previousRound = existing.Round
				}
			}
		}
		break
	}
	if previousScope == "" || previousChoice == "" || previousScope == incomingScope {
		return false
	}
	if incomingRound <= previousRound {
		return false
	}
	// `frozen` stores the value produced by this operation.
	if frozen := strings.TrimSpace(ExecPool.frozen[epoch][previousScope]); frozen != "" {
		return false
	}
	if previousBestCount >= requiredQuorum {
		return false
	}
	// Keep one authoritative signer choice per height by atomically moving the
	// vote forward. Waiting for the incoming proposal to already have quorum
	// creates a circular wait when every validator previously voted elsewhere.
	if byHash, ok := ExecPool.pool[epoch]; ok {
		// `prefix` stores the value produced by this operation.
		prefix := previousScope + "|"
		// `key` and `results` track the key used to access the related value.
		for key, results := range byHash {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			delete(results, signer)
			if len(results) == 0 {
				delete(byHash, key)
			}
		}
		if len(byHash) == 0 {
			delete(ExecPool.pool, epoch)
		}
	}
	// `byScope` and `ok` store whether the related condition is satisfied.
	if byScope, ok := ExecPool.signers[epoch]; ok {
		// `signers` stores the value produced by this operation.
		if signers := byScope[previousScope]; signers != nil {
			delete(signers, signer)
			if len(signers) == 0 {
				delete(byScope, previousScope)
			}
		}
		if len(byScope) == 0 {
			delete(ExecPool.signers, epoch)
		}
	}
	// `byScope` and `ok` store whether the related condition is satisfied.
	if byScope, ok := ExecPool.choice[epoch]; ok {
		// `choices` stores the value produced by this operation.
		if choices := byScope[previousScope]; choices != nil {
			delete(choices, signer)
			if len(choices) == 0 {
				delete(byScope, previousScope)
			}
		}
		if len(byScope) == 0 {
			delete(ExecPool.choice, epoch)
		}
	}
	// `bySigner` and `ok` store whether the related condition is satisfied.
	if bySigner, ok := ExecPool.epochChoice[epoch]; ok {
		delete(bySigner, signer)
		if len(bySigner) == 0 {
			delete(ExecPool.epochChoice, epoch)
		}
	}
	return true
}

// freezeExecPool implements the freeze exec pool helper.
func freezeExecPool(epoch uint64, proposalKey string, execHash string) {
	if epoch == 0 || execHash == "" {
		return
	}
	// `poolScopeKey` stores the key used to access the related value.
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()
	if ExecPool.frozen == nil {
		ExecPool.frozen = make(map[uint64]map[string]string)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := ExecPool.frozen[epoch]; !ok {
		ExecPool.frozen[epoch] = make(map[string]string)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := ExecPool.frozen[epoch][poolScopeKey]; !ok {
		ExecPool.frozen[epoch][poolScopeKey] = execHash
	}
}

// getExecResultsGlobal implements the get exec results global helper.
func getExecResultsGlobal(epoch uint64, proposalKey string, execHash string, txMerkle string) ([]ExecutionResult, []string, int, bool) {
	if epoch == 0 || execHash == "" {
		return nil, nil, 0, false
	}
	// `scopedExecKey` stores the key used to access the related value.
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)

	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	// `byHash` and `ok` store whether the related condition is satisfied.
	byHash, ok := ExecPool.pool[epoch]
	if !ok {
		return nil, nil, 0, false
	}
	// `resultsMap` and `ok` store whether the related condition is satisfied.
	resultsMap, ok := byHash[scopedExecKey]
	if !ok {
		return nil, nil, 0, false
	}
	if txMerkle != "" {
		// `expected` and `ok` store whether the related condition is satisfied.
		if expected, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && expected != "" && expected != txMerkle {
			return nil, nil, 0, false
		}
	}

	// `results` stores the result produced by this operation.
	results := make([]ExecutionResult, 0, len(resultsMap))
	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(resultsMap))
	// `r` tracks the current values while iterating.
	for _, r := range resultsMap {
		results = append(results, r)
		if r.Signer != "" {
			signers = append(signers, r.Signer)
		}
	}

	return results, signers, len(resultsMap), true
}

func getExecResultsForBlockHashGlobal(epoch uint64, blockHash string, execHash string, txMerkle string) ([]ExecutionResult, []string, int, bool) {
	blockHash = strings.TrimSpace(blockHash)
	execHash = strings.TrimSpace(execHash)
	txMerkle = strings.TrimSpace(txMerkle)
	if epoch == 0 || blockHash == "" || execHash == "" {
		return nil, nil, 0, false
	}
	scope := commitVoteScopeKey(epoch, blockHash)
	if scope == "" {
		return nil, nil, 0, false
	}

	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	byHash, ok := ExecPool.pool[epoch]
	if !ok {
		return nil, nil, 0, false
	}
	prefix := scope + "|"
	bySigner := make(map[string]ExecutionResult)
	for scopedExecKey, resultsMap := range byHash {
		if !strings.HasPrefix(scopedExecKey, prefix) {
			continue
		}
		if txMerkle != "" {
			if expected, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && expected != "" && expected != txMerkle {
				continue
			}
		}
		for signer, res := range resultsMap {
			signer = normalizeValidatorID(signer)
			if signer == "" {
				signer = normalizeValidatorID(res.Signer)
			}
			if signer == "" {
				continue
			}
			if strings.TrimSpace(res.ResultHash) != "" && !strings.EqualFold(strings.TrimSpace(res.ResultHash), execHash) {
				continue
			}
			if txMerkle != "" && strings.TrimSpace(res.TxMerkle) != "" && strings.TrimSpace(res.TxMerkle) != txMerkle {
				continue
			}
			res.Signer = signer
			if existing, ok := bySigner[signer]; !ok || executionResultSortLess(res, existing) {
				bySigner[signer] = res
			}
		}
	}
	if len(bySigner) == 0 {
		return nil, nil, 0, false
	}
	results := make([]ExecutionResult, 0, len(bySigner))
	signers := make([]string, 0, len(bySigner))
	for signer, res := range bySigner {
		results = append(results, res)
		signers = append(signers, signer)
	}
	sort.Slice(results, func(i, j int) bool {
		return executionResultSortLess(results[i], results[j])
	})
	signers = canonicalValidatorIDs(signers)
	return results, signers, len(results), true
}

func (n *Node) consensusExecutionResultsForBlock(block Block, execHash string, txMerkle string) ([]ExecutionResult, []string, int, bool) {
	if n == nil || n.Consensus == nil || block.ID == 0 {
		return nil, nil, 0, false
	}
	blockHash := strings.TrimSpace(block.BlockHash)
	execHash = strings.TrimSpace(execHash)
	txMerkle = strings.TrimSpace(txMerkle)
	if txMerkle == "" {
		txMerkle = strings.TrimSpace(block.MempoolRoot)
	}
	if blockHash == "" || execHash == "" {
		return nil, nil, 0, false
	}
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	required := n.executionQuorumRequiredForEpoch(block.ID)
	if required == 0 {
		required = execQuorumRequired(len(validators))
	}
	if len(validators) == 0 || required == 0 {
		return nil, nil, 0, false
	}
	allowed := make(map[string]struct{}, len(validators))
	for _, id := range validators {
		if id = normalizeValidatorID(id); id != "" {
			allowed[id] = struct{}{}
		}
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, blockHash, block.MempoolRoot, execHash)
	expectedResultHash := executionResultHashFromProposal(block.ID, proposalKey, blockHash, execHash, txMerkle)

	n.Consensus.mu.Lock()
	rawVotes := make([]ExecutionResult, 0, len(n.Consensus.ExecVotes[blockHash]))
	for signer, vote := range n.Consensus.ExecVotes[blockHash] {
		mapSigner := normalizeValidatorID(signer)
		voteSigner := normalizeValidatorID(vote.Signer)
		if mapSigner != "" {
			vote.Signer = mapSigner
		} else {
			vote.Signer = voteSigner
		}
		rawVotes = append(rawVotes, vote)
	}
	n.Consensus.mu.Unlock()

	bySigner := make(map[string]ExecutionResult, len(rawVotes))
	for _, vote := range rawVotes {
		signer := normalizeValidatorID(vote.Signer)
		if signer == "" {
			continue
		}
		if _, ok := allowed[signer]; !ok {
			continue
		}
		if vote.Height == 0 {
			vote.Height = block.ID
		}
		if vote.Height != block.ID {
			continue
		}
		if strings.TrimSpace(vote.BlockHash) == "" {
			vote.BlockHash = blockHash
		}
		if !strings.EqualFold(strings.TrimSpace(vote.BlockHash), blockHash) {
			continue
		}
		if strings.TrimSpace(vote.ResultHash) == "" {
			vote.ResultHash = execHash
		}
		if !strings.EqualFold(strings.TrimSpace(vote.ResultHash), execHash) {
			continue
		}
		if strings.TrimSpace(vote.TxMerkle) == "" {
			vote.TxMerkle = txMerkle
		}
		if strings.TrimSpace(vote.TxMerkle) != txMerkle {
			continue
		}
		if !executionResultHashMatches(vote.ExecutionResultHash, expectedResultHash) {
			continue
		}
		vote.ExecutionResultHash = expectedResultHash
		if strings.TrimSpace(vote.Signature) != "" {
			if !n.verifyBlockExecutionResultSignatureForHint(vote, block, blockHash) {
				if err := n.verifyBlockExecutionResultSignature(vote, block); err != nil {
					continue
				}
			}
		} else if !IsTestnet {
			continue
		}
		vote.Signer = signer
		if existing, ok := bySigner[signer]; !ok || executionResultSortLess(vote, existing) {
			bySigner[signer] = vote
		}
	}
	if len(bySigner) < required {
		return nil, nil, len(bySigner), false
	}
	results := make([]ExecutionResult, 0, len(bySigner))
	signers := make([]string, 0, len(bySigner))
	for signer, vote := range bySigner {
		results = append(results, vote)
		signers = append(signers, signer)
	}
	results = canonicalExecutionResults(results)
	signers = canonicalValidatorIDs(signers)
	return results, signers, len(results), true
}

// getExecCountGlobal implements the get exec count global helper.
func getExecCountGlobal(epoch uint64, proposalKey string, execHash string, txMerkle string) int {
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	if epoch == 0 || execHash == "" {
		return 0
	}
	// `scopedExecKey` stores the key used to access the related value.
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)

	// `byHash` and `ok` store whether the related condition is satisfied.
	byHash, ok := ExecPool.pool[epoch]
	if !ok {
		return 0
	}
	// `resultsMap` and `ok` store whether the related condition is satisfied.
	resultsMap, ok := byHash[scopedExecKey]
	if !ok {
		return 0
	}
	if txMerkle != "" {
		// `expected` and `ok` store whether the related condition is satisfied.
		if expected, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && expected != "" && expected != txMerkle {
			return 0
		}
	}
	return len(resultsMap)
}

// getExecCountForProposalScopeGlobal implements the get exec count for proposal scope global helper.
func getExecCountForProposalScopeGlobal(epoch uint64, proposalKey string, txMerkle string) int {
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	if epoch == 0 || strings.TrimSpace(proposalKey) == "" {
		return 0
	}
	// `scope` stores the value produced by this operation.
	scope := execPoolScopeKey(epoch, proposalKey)
	if scope == "" {
		return 0
	}
	// `byHash` and `ok` store whether the related condition is satisfied.
	byHash, ok := ExecPool.pool[epoch]
	if !ok {
		return 0
	}
	// `prefix` stores the value produced by this operation.
	prefix := scope + "|"
	// `best` stores the value produced by this operation.
	best := 0
	// `scopedExecKey` and `resultsMap` track the key used to access the related value.
	for scopedExecKey, resultsMap := range byHash {
		if !strings.HasPrefix(scopedExecKey, prefix) {
			continue
		}
		if txMerkle != "" {
			// `expected` and `ok` store whether the related condition is satisfied.
			if expected, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && expected != "" && expected != txMerkle {
				continue
			}
		}
		if len(resultsMap) > best {
			best = len(resultsMap)
		}
	}
	return best
}

// clearExecPoolProposal implements the clear exec pool proposal helper.
func clearExecPoolProposal(epoch uint64, proposalKey string) {
	if epoch == 0 || proposalKey == "" {
		return
	}
	// `poolScopeKey` stores the key used to access the related value.
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	// `byHash` and `ok` store whether the related condition is satisfied.
	if byHash, ok := ExecPool.pool[epoch]; ok {
		// `prefix` stores the value produced by this operation.
		prefix := poolScopeKey + "|"
		// `key` tracks the key used to access the related value.
		for key := range byHash {
			if strings.HasPrefix(key, prefix) {
				delete(byHash, key)
			}
		}
		if len(byHash) == 0 {
			delete(ExecPool.pool, epoch)
		}
	}
	// `byMerkle` and `ok` store whether the related condition is satisfied.
	if byMerkle, ok := ExecPool.txMerkle[epoch]; ok {
		// `prefix` stores the value produced by this operation.
		prefix := poolScopeKey + "|"
		// `key` tracks the key used to access the related value.
		for key := range byMerkle {
			if strings.HasPrefix(key, prefix) {
				delete(byMerkle, key)
			}
		}
		if len(byMerkle) == 0 {
			delete(ExecPool.txMerkle, epoch)
		}
	}
	// `byProposal` and `ok` store whether the related condition is satisfied.
	if byProposal, ok := ExecPool.frozen[epoch]; ok {
		delete(byProposal, poolScopeKey)
		if len(byProposal) == 0 {
			delete(ExecPool.frozen, epoch)
		}
	}
	// `byProposal` and `ok` store whether the related condition is satisfied.
	if byProposal, ok := ExecPool.signers[epoch]; ok {
		delete(byProposal, poolScopeKey)
		if len(byProposal) == 0 {
			delete(ExecPool.signers, epoch)
		}
	}
	// `byProposal` and `ok` store whether the related condition is satisfied.
	if byProposal, ok := ExecPool.choice[epoch]; ok {
		delete(byProposal, poolScopeKey)
		if len(byProposal) == 0 {
			delete(ExecPool.choice, epoch)
		}
	}
	// `bySigner` and `ok` store whether the related condition is satisfied.
	if bySigner, ok := ExecPool.epochChoice[epoch]; ok {
		// `prefix` stores the value produced by this operation.
		prefix := poolScopeKey + "|"
		// `signer` and `choice` track the current values while iterating.
		for signer, choice := range bySigner {
			if strings.HasPrefix(choice, prefix) {
				delete(bySigner, signer)
			}
		}
		if len(bySigner) == 0 {
			delete(ExecPool.epochChoice, epoch)
		}
	}
}

// clearExecPoolUpTo implements the clear exec pool up to helper.
func clearExecPoolUpTo(height uint64) {
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	// `h` tracks the current values while iterating.
	for h := range ExecPool.pool {
		if h <= height {
			delete(ExecPool.pool, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range ExecPool.txMerkle {
		if h <= height {
			delete(ExecPool.txMerkle, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range ExecPool.frozen {
		if h <= height {
			delete(ExecPool.frozen, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range ExecPool.signers {
		if h <= height {
			delete(ExecPool.signers, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range ExecPool.choice {
		if h <= height {
			delete(ExecPool.choice, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range ExecPool.epochChoice {
		if h <= height {
			delete(ExecPool.epochChoice, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range ExecPool.commitChoice {
		if h <= height {
			delete(ExecPool.commitChoice, h)
		}
	}
}

// clearCommitVoteStateUpTo implements the clear commit vote state up to helper.
func (n *Node) clearCommitVoteStateUpTo(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.commitMu.Lock()
	// `h` tracks the current values while iterating.
	for h := range n.commitVotes {
		if h <= height {
			delete(n.commitVotes, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range n.commitVoted {
		if h <= height {
			delete(n.commitVoted, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range n.commitVoteSignatures {
		if h <= height {
			delete(n.commitVoteSignatures, h)
		}
	}
	n.commitMu.Unlock()
	n.clearUnsignedExecPoolHintsUpTo(height)
	n.persistConsensusSafetyStateAsync("commit_vote_state_pruned")
}

// buildExecPoolSnapshot builds exec pool snapshot.
func buildExecPoolSnapshot(epoch uint64, proposalKey string) *ExecPoolSnapshot {
	if epoch == 0 {
		return nil
	}
	// `poolScopeKey` stores the key used to access the related value.
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	// `byHash` and `ok` store whether the related condition is satisfied.
	byHash, ok := ExecPool.pool[epoch]
	if !ok || len(byHash) == 0 {
		return nil
	}

	// `prefix` stores the value produced by this operation.
	prefix := poolScopeKey + "|"
	// `hashes` stores the digest used to identify or verify the related data.
	hashes := make(map[string][]string)
	// `scopedHash` and `resMap` track the digest used to identify or verify the related data.
	for scopedHash, resMap := range byHash {
		if !strings.HasPrefix(scopedHash, prefix) {
			continue
		}
		// `hash` stores the digest used to identify or verify the related data.
		hash := strings.TrimPrefix(scopedHash, prefix)
		// `signers` stores the value produced by this operation.
		signers := make([]string, 0, len(resMap))
		// `signer` tracks the current values while iterating.
		for signer := range resMap {
			signers = append(signers, signer)
		}
		hashes[hash] = canonicalValidatorIDs(signers)
	}
	if len(hashes) == 0 {
		return nil
	}

	// `txMerkle` stores the transaction data handled by this operation.
	txMerkle := make(map[string]string)
	// `tm` and `ok` store whether the related condition is satisfied.
	if tm, ok := ExecPool.txMerkle[epoch]; ok {
		// `scopedHash` and `merkle` track the digest used to identify or verify the related data.
		for scopedHash, merkle := range tm {
			if !strings.HasPrefix(scopedHash, prefix) {
				continue
			}
			// `hash` stores the digest used to identify or verify the related data.
			hash := strings.TrimPrefix(scopedHash, prefix)
			txMerkle[hash] = merkle
		}
	}

	return &ExecPoolSnapshot{
		Epoch:       epoch,
		ProposalKey: proposalKey,
		Hashes:      hashes,
		TxMerkle:    txMerkle,
	}
}

// markUnsignedExecPoolHint records that this node observed an unsigned
// exec-pool hint for a proposal. The hint is intentionally not counted as vote
// evidence; it only forces signed commit quorum before finality for that
// proposal.
func (n *Node) markUnsignedExecPoolHint(epoch uint64, proposalKey string) {
	if n == nil || epoch == 0 {
		return
	}
	proposalKey = strings.TrimSpace(proposalKey)
	if proposalKey == "" {
		return
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.unsignedExecPoolHints == nil {
		n.unsignedExecPoolHints = make(map[uint64]map[string]bool)
	}
	if n.unsignedExecPoolHints[epoch] == nil {
		n.unsignedExecPoolHints[epoch] = make(map[string]bool)
	}
	n.unsignedExecPoolHints[epoch][proposalKey] = true
}

// unsignedExecPoolHintSeen reports whether this proposal has seen unsigned
// propagated exec-pool state and therefore must wait for signed commit quorum.
func (n *Node) unsignedExecPoolHintSeen(epoch uint64, proposalKey string) bool {
	if n == nil || epoch == 0 {
		return false
	}
	proposalKey = strings.TrimSpace(proposalKey)
	if proposalKey == "" {
		return false
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.unsignedExecPoolHints == nil || n.unsignedExecPoolHints[epoch] == nil {
		return false
	}
	return n.unsignedExecPoolHints[epoch][proposalKey]
}

// clearUnsignedExecPoolHintsUpTo prunes local unsigned-hint safety markers
// after the related heights are finalized.
func (n *Node) clearUnsignedExecPoolHintsUpTo(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	for h := range n.unsignedExecPoolHints {
		if h <= height {
			delete(n.unsignedExecPoolHints, h)
		}
	}
}

// execPoolSnapshotProposalKnown reports whether an exec-pool snapshot targets a
// currently active proposal this node already knows. Stale/future unsigned hints
// must not influence local finality policy.
func (n *Node) execPoolSnapshotProposalKnown(epoch uint64, proposalKey string) bool {
	if n == nil || epoch == 0 {
		return false
	}
	proposalKey = strings.TrimSpace(proposalKey)
	if proposalKey == "" {
		return false
	}
	n.commitMu.Lock()
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	if epoch <= committedHeight || epoch != n.currentEpoch() {
		return false
	}
	if snap, ok := n.proposalSnapshotForEpoch(epoch); ok && proposalKey == snap.ProposalKey {
		return true
	}
	for _, block := range n.candidateProposalBlocksForEpoch(epoch) {
		if proposalKey == proposalSnapshotFromBlock(block).ProposalKey {
			return true
		}
	}
	return false
}

// mergeExecPoolSnapshot implements the merge exec pool snapshot helper.
func (n *Node) mergeExecPoolSnapshot(snapshot ExecPoolSnapshot) {
	if snapshot.Epoch == 0 {
		return
	}
	// `proposalKey` stores the key used to access the related value.
	proposalKey := strings.TrimSpace(snapshot.ProposalKey)
	if proposalKey == "" {
		proposalKey = legacyProposalVoteKey(snapshot.Epoch)
	}
	if len(snapshot.Hashes) > 0 {
		// ExecPoolSnapshot is an unsigned hint from a peer. Do not let delayed
		// proof injection manufacture live quorum; validators must replay their
		// signed execution votes through processExecutionResultMsg.
		if n.execPoolSnapshotProposalKnown(snapshot.Epoch, proposalKey) {
			n.markUnsignedExecPoolHint(snapshot.Epoch, proposalKey)
		}
		if DebugConsensus || DebugSync {
			fmt.Printf("[EXEC-POOL-SNAPSHOT-REJECT] epoch=%d reason=unsigned_quorum_hint hashes=%d\n",
				snapshot.Epoch, len(snapshot.Hashes))
		}
		return
	}
	// `proposalBlockHash` stores the digest used to identify or verify the related data.
	proposalBlockHash := ""
	n.commitMu.Lock()
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	if snapshot.Epoch <= committedHeight {
		return
	}
	// Accept only current epoch to avoid stale pollution.
	if snapshot.Epoch != n.currentEpoch() {
		return
	}
	// `snap` and `ok` store whether the related condition is satisfied.
	if snap, ok := n.proposalSnapshotForEpoch(snapshot.Epoch); ok && proposalKey == snap.ProposalKey {
		proposalBlockHash = snap.BlockHash
		goto merge_exec_snapshot
	}
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range n.candidateProposalBlocksForEpoch(snapshot.Epoch) {
		// `snap` stores the value produced by this operation.
		snap := proposalSnapshotFromBlock(block)
		if proposalKey == snap.ProposalKey {
			proposalBlockHash = snap.BlockHash
			goto merge_exec_snapshot
		}
	}
	return

merge_exec_snapshot:
	if strings.TrimSpace(proposalBlockHash) == "" {
		return
	}
	// `requiredQuorum` stores the request data being processed.
	requiredQuorum := n.executionQuorumRequiredForEpoch(snapshot.Epoch)
	// `hash` and `signers` track the digest used to identify or verify the related data.
	for hash, signers := range snapshot.Hashes {
		// `txMerkle` stores the transaction data handled by this operation.
		txMerkle := ""
		if snapshot.TxMerkle != nil {
			txMerkle = snapshot.TxMerkle[hash]
		}
		// `executionResultHash` stores the digest used to identify or verify the related data.
		executionResultHash := executionResultHashFromProposal(snapshot.Epoch, proposalKey, proposalBlockHash, hash, txMerkle)
		// `signer` tracks the current values while iterating.
		for _, signer := range signers {
			if !n.isValidatorInSetForHeight(signer, snapshot.Epoch) {
				continue
			}
			_, _, _ = recordExecResultGlobalWithRequired(snapshot.Epoch, proposalKey, hash, txMerkle, ExecutionResult{
				Height:              snapshot.Epoch,
				BlockHash:           proposalBlockHash,
				Signer:              signer,
				ResultHash:          hash,
				TxMerkle:            txMerkle,
				ExecutionResultHash: executionResultHash,
			}, requiredQuorum)
			n.mirrorConsensusExecVote(snapshot.Epoch, proposalBlockHash, ExecutionResult{
				Height:              snapshot.Epoch,
				BlockHash:           proposalBlockHash,
				Signer:              signer,
				ResultHash:          hash,
				TxMerkle:            txMerkle,
				ExecutionResultHash: executionResultHash,
			})
		}
	}
}

// currentEpoch returns current epoch.
func (n *Node) currentEpoch() uint64 {
	if n == nil {
		return 1
	}
	// `h` stores the value produced by this operation.
	h := uint64(0)
	if n.Blockchain != nil {
		h = n.Blockchain.Height()
	}
	n.commitMu.Lock()
	if n.committedHeight > h {
		h = n.committedHeight
	}
	n.commitMu.Unlock()
	return h + 1
}

// queueFutureLeaderBlock implements the queue future leader block helper.
func (n *Node) queueFutureLeaderBlock(block Block, sourcePeer string) bool {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return false
	}
	// `currentEpoch` stores the value produced by this operation.
	currentEpoch := n.currentEpoch()
	if block.ID <= currentEpoch {
		return false
	}
	if MaxFutureBlockGap > 0 && block.ID > currentEpoch+MaxFutureBlockGap {
		if DebugConsensus || DebugSync {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d reason=future_epoch_gap local_epoch=%d max_gap=%d peer=%s proposer=%s block=%s\n",
				block.ID, currentEpoch, MaxFutureBlockGap, sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		n.maybeSyncToBestObservedHeight("future_leader_block_gap")
		return false
	}

	n.leaderMu.Lock()
	defer n.leaderMu.Unlock()
	if n.queuedFutureLeaderBlocks == nil {
		n.queuedFutureLeaderBlocks = make(map[uint64][]Block)
	}
	// `i` and `existing` track the current position in the related collection.
	for i, existing := range n.queuedFutureLeaderBlocks[block.ID] {
		if existing.BlockHash != block.BlockHash {
			continue
		}
		if block.Round > existing.Round {
			n.queuedFutureLeaderBlocks[block.ID][i] = block
			goto queued
		}
		return false
	}
	n.queuedFutureLeaderBlocks[block.ID] = append(n.queuedFutureLeaderBlocks[block.ID], block)

queued:
	if len(n.queuedFutureLeaderBlocks[block.ID]) > maxQueuedForkBlocksPerHeight {
		sort.SliceStable(n.queuedFutureLeaderBlocks[block.ID], func(i, j int) bool {
			// `a` stores the value produced by this operation.
			a := n.queuedFutureLeaderBlocks[block.ID][i]
			// `b` stores the value produced by this operation.
			b := n.queuedFutureLeaderBlocks[block.ID][j]
			if a.Round != b.Round {
				return a.Round > b.Round
			}
			return a.BlockHash < b.BlockHash
		})
		n.queuedFutureLeaderBlocks[block.ID] = append([]Block(nil), n.queuedFutureLeaderBlocks[block.ID][:maxQueuedForkBlocksPerHeight]...)
	}
	if DebugConsensus || DebugSync {
		fmt.Printf("[LEADER-BLOCK-QUEUE] height=%d local_epoch=%d round=%d peer=%s proposer=%s block=%s\n",
			block.ID, currentEpoch, block.Round, sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
	}
	return true
}

// replayQueuedLeaderBlocksForCurrentEpoch implements the replay queued leader blocks for current epoch helper.
func (n *Node) replayQueuedLeaderBlocksForCurrentEpoch() {
	if n == nil {
		return
	}
	// `epoch` stores the value produced by this operation.
	epoch := n.currentEpoch()
	if epoch == 0 {
		return
	}

	n.leaderMu.Lock()
	if len(n.queuedFutureLeaderBlocks) == 0 {
		n.leaderMu.Unlock()
		return
	}
	// `blocks` stores the block data handled by this operation.
	blocks := append([]Block(nil), n.queuedFutureLeaderBlocks[epoch]...)
	delete(n.queuedFutureLeaderBlocks, epoch)
	// `h` tracks the current values while iterating.
	for h := range n.queuedFutureLeaderBlocks {
		if h < epoch {
			delete(n.queuedFutureLeaderBlocks, h)
		}
	}
	n.leaderMu.Unlock()

	if len(blocks) == 0 {
		return
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].Round != blocks[j].Round {
			return blocks[i].Round < blocks[j].Round
		}
		return blocks[i].BlockHash < blocks[j].BlockHash
	})
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range blocks {
		n.handleLeaderBlock(block, "queued_future")
	}
}

// recordVerifiedProposedBlock implements the record verified proposed block helper.
func recordVerifiedProposedBlock(height uint64, round uint32, proposer string, blockHash string) (bool, bool, string) {
	proposer = normalizeValidatorID(proposer)
	if height == 0 || proposer == "" || blockHash == "" {
		return false, false, ""
	}

	proposedBlocksMu.Lock()
	defer proposedBlocksMu.Unlock()

	if ProposedBlocks == nil {
		ProposedBlocks = make(map[uint64]map[uint32]map[string]string)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := ProposedBlocks[height]; !ok {
		ProposedBlocks[height] = make(map[uint32]map[string]string)
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := ProposedBlocks[height][round]; !ok {
		ProposedBlocks[height][round] = make(map[string]string)
	}

	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := ProposedBlocks[height][round][proposer]; ok {
		if prev == blockHash {
			return false, false, prev
		}
		return false, true, prev
	}
	ProposedBlocks[height][round][proposer] = blockHash

	// `keepWindow` defines the constant value used by this package.
	const keepWindow = uint64(512)
	if height > keepWindow {
		// `cutoff` stores the value produced by this operation.
		cutoff := height - keepWindow
		// `h` tracks the current values while iterating.
		for h := range ProposedBlocks {
			if h < cutoff {
				delete(ProposedBlocks, h)
			}
		}
	}

	return true, false, ""
}

// recordProposedBlock implements the record proposed block helper.
func recordProposedBlock(height uint64, round uint32, proposer string, blockHash string) (bool, bool, string) {
	return recordVerifiedProposedBlock(height, round, proposer, blockHash)
}

// penalizeSignedProposal implements the penalize signed proposal helper.
func penalizeSignedProposal(n *Node, block Block, reason string) {
	if n == nil || block.ID == 0 || block.Proposer == "" {
		return
	}
	n.RecordMisbehavior(block.Proposer, reason, int(block.ID), block.BlockHash)
	n.SlashValidator(block.Proposer)
}

// verifyLeaderBlockSignatureFromCommittedRegistry verifies leader block signature from committed registry.
func (n *Node) verifyLeaderBlockSignatureFromCommittedRegistry(block Block) (bool, bool) {
	// `strictRegistry` stores whether mutable/runtime key fallback is forbidden.
	strictRegistry := strings.TrimSpace(block.ValidatorRegistryHash) != "" ||
		(n != nil && n.validatorRegistryCommitmentRequiredAt(block.ID))
	if n == nil || block.ID == 0 || len(block.Signature) == 0 {
		return false, strictRegistry
	}
	// `proposerID` stores the value produced by this operation.
	proposerID := normalizeValidatorID(block.Proposer)
	if proposerID == "" {
		return false, strictRegistry
	}
	// `committee` and `ok` store whether the related condition is satisfied.
	committee, _, ok := n.deterministicCommitteeValidatorsForHeight(block.ID)
	if !ok {
		return false, strictRegistry
	}
	if !containsNormalizedValidatorID(committee, proposerID) {
		return false, true
	}
	// `candidates` stores the deterministic public keys accepted for the proposer.
	candidates := n.blockProposerSignatureCandidates(block, committee)
	if len(candidates) == 0 {
		return false, strictRegistry
	}
	if !verifyBlockSignatureWithCandidates(block, candidates) {
		return false, true
	}
	if DebugConsensus {
		fmt.Printf("[PROPOSER-SIG-FALLBACK] height=%d proposer=%s result=accepted source=committed_registry\n",
			block.ID, ShortID(proposerID))
	}
	return true, true
}

// verifyLeaderBlockSignatureFromGenesis verifies leader block signature from genesis.
func verifyLeaderBlockSignatureFromGenesis(block Block) bool {
	// `pub` stores the value produced by this operation.
	pub := onChainValidatorPubKeyForID(block.Proposer)
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return verifyBlockSignatureWithCandidates(block, []ed25519.PublicKey{ed25519.PublicKey(pub)})
}

// verifyLeaderBlock verifies leader block.
func (n *Node) verifyLeaderBlock(block Block, sourcePeer string) bool {
	if block.ID == 0 {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] reason=zero_height peer=%s proposer=%s block=%s\n",
				sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		return false
	}
	// `epoch` stores the value produced by this operation.
	epoch := n.currentEpoch()
	if block.ID != epoch {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d reason=wrong_epoch local_epoch=%d peer=%s proposer=%s block=%s\n",
				block.ID, epoch, sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		return false
	}

	// `validSig` and `committedAuthority` store whether the related condition is satisfied.
	validSig, committedAuthority := n.verifyLeaderBlockSignatureFromCommittedRegistry(block)
	if !committedAuthority && len(block.Signature) > 0 {
		validSig = verifyLeaderBlockSignatureFromGenesis(block)
	}
	if !validSig {
		if DebugConsensus {
			fmt.Printf("Invalid proposer signature at height %d | proposer=%s\n",
				block.ID, ShortID(block.Proposer))
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_leader_block_signature")
		return false
	}

	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := n.getLeaderBlock(block.ID); ok {
		if block.Round == existing.Round && block.BlockHash == existing.BlockHash {
			if DebugConsensus {
				fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=duplicate_proposal peer=%s proposer=%s block=%s\n",
					block.ID, block.Round, sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
			}
			return false
		}
	}

	// `requiresValidatorSetHash` stores the digest used to identify or verify the related data.
	requiresValidatorSetHash := validatorSetCommitmentV2EnabledAt(block.ID)
	if requiresValidatorSetHash && strings.TrimSpace(block.ValidatorSetHash) == "" {
		if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-REJECT] height=%d reason=missing_validator_set_hash\n", block.ID)
		}
		n.recordPeerSecurityFault(sourcePeer, "missing_validator_set_hash")
		return false
	}
	if block.ValidatorSetHash != "" {
		// `expectedHash` and `expectedSource` store the digest used to identify or verify the related data.
		expectedHash, expectedSource := n.expectedValidatorSetHashWithSource(block.ID)
		// `hashMode` stores the digest used to identify or verify the related data.
		hashMode := validatorSetHashModeForHeight(block.ID)
		if validatorSetSourceIsChainAuthoritative(expectedSource) && strings.TrimSpace(expectedHash) == "" {
			if DebugConsensus {
				fmt.Printf("[SET-COMMITMENT-REJECT] height=%d source=%s mode=%s reason=missing_parent_commitment got=%s\n",
					block.ID, expectedSource, hashMode, ShortHash(block.ValidatorSetHash))
			}
			n.maybeSyncToBestObservedHeight("validator_set_parent_commitment_missing")
			return false
		}
		if expectedHash != "" && block.ValidatorSetHash != expectedHash {
			// `localHeight` stores the value produced by this operation.
			localHeight, _ := n.localHeightSnapshot()
			if !validatorSetSourceIsChainAuthoritative(expectedSource) {
				if DebugConsensus {
					fmt.Printf("[SET-COMMITMENT-SOURCE] weak-source block mismatch deferred height=%d source=%s mode=%s expected=%s got=%s\n",
						block.ID, expectedSource, hashMode, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash))
				}
				n.maybeSyncToBestObservedHeight("validator_set_mismatch_weak_source")
				return false
			}
			if block.ID > localHeight {
				if DebugConsensus {
					fmt.Printf("[SYNC-OVERRIDE] mismatch snapshot-first local=%d mismatch_h=%d expected=%s got=%s\n",
						localHeight, block.ID, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash))
				}
				n.forceSnapshotResyncNow(block.ID, "validator-set-hash-mismatch-snapshot-first")
				n.maybeSyncToBestObservedHeight("validator_set_mismatch_snapshot_first")
				return false
			}
			if n.shouldTreatValidatorSetMismatchAsPeerDrift(block.ID, expectedHash, block.ValidatorSetHash) {
				if sourcePeer != "" {
					// `localHeightNow` and `localFinalized` store the value produced by this operation.
					localHeightNow, localFinalized := n.localHeightSnapshot()
					// `state` stores the value produced by this operation.
					state := n.recordFinalizedDrift(sourcePeer, block.ID, localFinalized, expectedHash, block.ValidatorSetHash, 1)
					n.applyFinalizedDriftPolicy(sourcePeer, state)
					if DebugConsensus {
						fmt.Printf("Ignoring validator-set mismatch as peer drift | peer=%s local=%d expected=%s got=%s height=%d\n",
							sourcePeer, localHeightNow, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash), block.ID)
					}
				}
				n.handlePersistentPeerDrift(localHeight, block.ID, expectedHash, block.ValidatorSetHash, "validator-set-hash-mismatch-autoheal")
				return false
			}
			if n.maybeAdoptCoreQuorumValidatorSetHash(block.ID, expectedHash, block.ValidatorSetHash) {
				expectedHash = n.expectedValidatorSetHash(block.ID)
			}
			if n.tryRepairValidatorSetHash(block.ID, block.ValidatorSetHash) {
				expectedHash = n.expectedValidatorSetHash(block.ID)
			}
		}
		if expectedHash != "" && block.ValidatorSetHash != expectedHash {
			// `count` and `shouldLog` store the measured quantity used by this operation.
			count, shouldLog := n.recordPeerDriftTuple(block.ID, expectedHash, block.ValidatorSetHash, peerDriftTupleLogCooldown)
			if DebugConsensus && shouldLog {
				fmt.Printf("[SET-COMMITMENT-REJECT] height=%d source=%s mode=%s expected=%s got=%s count=%d\n",
					block.ID, expectedSource, hashMode, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash), count)
			}
			if DebugConsensus && shouldLog {
				fmt.Printf("Validator set hash mismatch at height %d | expected=%s got=%s count=%d\n",
					block.ID, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash), count)
			}
			// `localHeight` stores the value produced by this operation.
			localHeight, _ := n.localHeightSnapshot()
			if n.recordValidatorSetMismatchWithLocal(localHeight, block.ID, expectedHash, block.ValidatorSetHash) {
				n.requestConsensusRecomputePause(block.ID, "validator_set_hash_mismatch")
				if shouldForceSnapshotResyncForValidatorSetMismatch(localHeight, block.ID) {
					n.forceSnapshotResyncNow(block.ID, "validator-set-hash-mismatch-autoheal")
				} else {
					n.maybeForceNearTipValidatorSetResync(localHeight, block.ID, "validator-set-hash-mismatch-autoheal")
				}
			}
			// Near-tip mismatch should still drive regular catch-up.
			n.maybeSyncToBestObservedHeight("validator_set_mismatch")
			return false
		} else if expectedHash != "" && strings.EqualFold(strings.TrimSpace(block.ValidatorSetHash), strings.TrimSpace(expectedHash)) && DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-APPLY] height=%d source=%s mode=%s hash=%s\n",
				block.ID, expectedSource, hashMode, ShortHash(block.ValidatorSetHash))
		}
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-REJECT] height=%d field=next_validator_set_hash err=%v\n", block.ID, err)
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_next_validator_set_commitment")
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("Validator set next-root commitment mismatch at height %d | err=%v\n", block.ID, err)
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_next_validator_set_root")
		return false
	}

	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	// `err` stores the error produced by this operation.
	if err := verifyBlockQuorumMetadata(block, len(validators)); err != nil {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=%s peer=%s proposer=%s block=%s\n",
				block.ID, block.Round, err.Error(), sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_leader_quorum_metadata")
		return false
	}
	// `expectedLeader` stores the value produced by this operation.
	expectedLeader := n.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	// `gotProposer` stores the value produced by this operation.
	gotProposer := normalizeValidatorID(block.Proposer)
	// `wantProposer` stores the value produced by this operation.
	wantProposer := normalizeValidatorID(expectedLeader)
	if wantProposer == "" || gotProposer != wantProposer {
		if !n.shouldCountInvalidProposerEvidence(block.ID, block.Round, wantProposer, gotProposer, block.BlockHash) {
			return false
		}
		// `count` and `shouldPause` store the measured quantity used by this operation.
		count, shouldPause := n.invalidProposerEvent(block.ID, wantProposer, gotProposer)
		if DebugConsensus && (count <= 3 || count%25 == 0) {
			// `expectedIdx` stores the current position in the related collection.
			expectedIdx := -1
			// `gotIdx` stores the current position in the related collection.
			gotIdx := -1
			// `i` and `id` track the current position in the related collection.
			for i, id := range validators {
				// `norm` stores the value produced by this operation.
				norm := normalizeValidatorID(id)
				if expectedIdx < 0 && norm == wantProposer {
					expectedIdx = i
				}
				if gotIdx < 0 && norm == gotProposer {
					gotIdx = i
				}
			}
			fmt.Printf("Invalid proposer at height %d | round=%d expected=%s got=%s seen=%d block=%s vhash=%s eidx=%d gidx=%d\n",
				block.ID, block.Round, ShortID(wantProposer), ShortID(gotProposer), count, ShortHash(block.BlockHash), ShortHash(ValidatorSetHash(validators)), expectedIdx, gotIdx)
		}
		if shouldPause {
			n.requestConsensusRecomputePause(block.ID, "invalid_proposer")
		}
		n.handleInvalidProposerPolicy(sourcePeer, block.ID, wantProposer, gotProposer)
		// Production safety: proposer mismatch near tip should not force snapshot
		// rollback loops. Use regular heartbeat-driven sync instead.
		n.maybeSyncToBestObservedHeight("invalid_proposer")
		return false
	}

	// `last` stores the value produced by this operation.
	last := n.Blockchain.LastBlock()
	if block.PrevHash != last.BlockHash {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=prev_hash_mismatch peer=%s proposer=%s expected=%s got=%s block=%s\n",
				block.ID, block.Round, sourcePeer, ShortID(block.Proposer), ShortHash(last.BlockHash), ShortHash(block.PrevHash), ShortHash(block.BlockHash))
		}
		n.maybeConvergeTipFromLeaderPrev(sourcePeer, block, "leader_prev_hash_mismatch")
		return false
	}
	if HashBlock(block) != block.BlockHash {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=block_hash_mismatch peer=%s proposer=%s expected=%s got=%s\n",
				block.ID, block.Round, sourcePeer, ShortID(block.Proposer), ShortHash(HashBlock(block)), ShortHash(block.BlockHash))
		}
		n.recordPeerSecurityFault(sourcePeer, "leader_block_hash_mismatch")
		return false
	}
	// `err` stores the error produced by this operation.
	if err := VerifyMempoolRoot(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=mempool_root_mismatch peer=%s proposer=%s err=%v block=%s\n",
				block.ID, block.Round, sourcePeer, ShortID(block.Proposer), err, ShortHash(block.BlockHash))
		}
		n.recordPeerSecurityFault(sourcePeer, "leader_mempool_root_mismatch")
		return false
	}
	// `maxRound` stores the value produced by this operation.
	if maxRound := ProposerRoundMax; maxRound > 0 && block.Round > maxRound {
		if DebugConsensus {
			fmt.Printf("Rejecting over-cap proposal at height %d | peer=%s round=%d max_round=%d\n",
				block.ID, sourcePeer, block.Round, maxRound)
		}
		return false
	}
	// `localRound` stores the value produced by this operation.
	localRound := n.localProposerRoundForHeight(block.ID)
	// `maxSkew` stores the value produced by this operation.
	maxSkew := ProposerRoundMaxSkew
	if maxSkew == 0 {
		maxSkew = 1
	}
	if block.Round > localRound+maxSkew {
		n.setProposedRound(block.ID, block.Round)
		// `syncedRound` stores the value produced by this operation.
		syncedRound := n.localProposerRoundForHeight(block.ID)
		if DebugConsensus && syncedRound > localRound {
			fmt.Printf("[ROUND-CATCHUP] height=%d peer=%s round=%d local_round=%d synced_round=%d proposer=%s\n",
				block.ID, sourcePeer, block.Round, localRound, syncedRound, ShortID(block.Proposer))
		}
		localRound = syncedRound
		if block.Round > localRound+maxSkew {
			if DebugConsensus {
				fmt.Printf("Rejecting future-round proposal at height %d | peer=%s round=%d local_round=%d max_skew=%d\n",
					block.ID, sourcePeer, block.Round, localRound, maxSkew)
			}
			return false
		}
	}
	// `equivocated` and `prevHash` store the digest used to identify or verify the related data.
	_, equivocated, prevHash := recordVerifiedProposedBlock(block.ID, block.Round, block.Proposer, block.BlockHash)
	if equivocated {
		if !n.shouldCountDoubleProposalEvidence(block.ID, block.Round, block.Proposer, prevHash, block.BlockHash) {
			return false
		}
		if DebugConsensus {
			fmt.Printf("Double proposal detected at height %d | round=%d proposer=%s prev=%s got=%s\n",
				block.ID, block.Round, ShortID(block.Proposer), ShortHash(prevHash), ShortHash(block.BlockHash))
		}
		n.requestConsensusRecomputePause(block.ID, "double_proposal")
		return false
	}
	// StateRoot mismatch should be resolved by execution-result quorum.
	// Proposal-stage rejection here can create false-positive slashing loops.
	return true
}

// handleLeaderBlock handles leader block.
func (n *Node) handleLeaderBlock(block Block, sourcePeer string) {
	if block.ID > n.currentEpoch() {
		_ = n.queueFutureLeaderBlock(block, sourcePeer)
		return
	}
	if !n.verifyLeaderBlock(block, sourcePeer) {
		return
	}

	if !n.storeLeaderBlock(block) {
		// Keep verified alternate proposals available for queued vote resolution
		// even when they do not replace the current leader slot.
		n.noteObservedProposal(block)
		n.processQueuedExecutionVotesForProposal(block)
		return
	}
	n.processQueuedExecutionVotesForProposal(block)
	if n.executionResultAlreadyCommitted(block.ID) {
		return
	}
	if n.tryFinalizeProposalIfQuorum(block, "proposal_existing_quorum") {
		return
	}
	if n.consensusRecomputePauseActive() {
		// Validator-set recompute barrier: do not emit votes while paused.
		return
	}
	// `lockedBlock` and `locked` store the synchronization state protecting shared data.
	if lockedBlock, _, locked, _ := n.acceptedProposalVoteLockForRound(block.ID, block.Round); locked && proposalConflictsWithAcceptedLock(lockedBlock, block) {
		n.maybeBroadcastExecutionVoteForBlock(lockedBlock, "accepted_vote_lock")
		return
	}
	n.maybeBroadcastExecutionVoteForBlock(block, "leader_block")
}

// setLogicalTick implements the set logical tick helper.
func (n *Node) setLogicalTick(epoch uint64, tick uint64) {
	n.logicalMu.Lock()
	n.logicalClock = LogicalClock{Epoch: epoch, Tick: tick}
	n.logicalMu.Unlock()
}

// executionResultSortLess implements the execution result sort less helper.
func executionResultSortLess(a ExecutionResult, b ExecutionResult) bool {
	// `aSigner` stores the value produced by this operation.
	aSigner := normalizeValidatorID(a.Signer)
	// `bSigner` stores the value produced by this operation.
	bSigner := normalizeValidatorID(b.Signer)
	if aSigner != bSigner {
		return aSigner < bSigner
	}
	// `aSigned` stores the value produced by this operation.
	aSigned := strings.TrimSpace(a.Signature) != ""
	// `bSigned` stores the value produced by this operation.
	bSigned := strings.TrimSpace(b.Signature) != ""
	if aSigned != bSigned {
		return aSigned
	}
	if a.ResultHash != b.ResultHash {
		return a.ResultHash < b.ResultHash
	}
	if a.TxMerkle != b.TxMerkle {
		return a.TxMerkle < b.TxMerkle
	}
	if a.ExecutionResultHash != b.ExecutionResultHash {
		return a.ExecutionResultHash < b.ExecutionResultHash
	}
	if a.BlockHash != b.BlockHash {
		return a.BlockHash < b.BlockHash
	}
	return a.Height < b.Height
}

// canonicalExecutionResults enforces deterministic ordering and one entry per signer.
func canonicalExecutionResults(results []ExecutionResult) []ExecutionResult {
	if len(results) == 0 {
		return nil
	}
	// `bySigner` stores the value produced by this operation.
	bySigner := make(map[string]ExecutionResult, len(results))
	// `res` tracks the result produced by this operation.
	for _, res := range results {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(res.Signer)
		if signer == "" {
			continue
		}
		res.Signer = signer
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := bySigner[signer]; !ok || executionResultSortLess(res, existing) {
			bySigner[signer] = res
		}
	}
	if len(bySigner) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]ExecutionResult, 0, len(bySigner))
	// `res` tracks the result produced by this operation.
	for _, res := range bySigner {
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool {
		return executionResultSortLess(out[i], out[j])
	})
	return out
}

// executionResultsWithCommitWitnesses implements the execution results with commit witnesses helper.
func executionResultsWithCommitWitnesses(block Block, execHash string, txMerkle string, results []ExecutionResult, witnesses []ValidatorSignature) []ExecutionResult {
	execHash = strings.TrimSpace(execHash)
	txMerkle = strings.TrimSpace(txMerkle)
	// `out` stores the result produced by this operation.
	out := append([]ExecutionResult{}, results...)
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(out)+len(witnesses))
	// `i` tracks the current position in the related collection.
	for i := range out {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(out[i].Signer)
		if signer == "" {
			continue
		}
		out[i].Signer = signer
		seen[signer] = struct{}{}
	}
	// `witness` tracks the current values while iterating.
	for _, witness := range witnesses {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(witness.Validator)
		// `sig` stores the value produced by this operation.
		sig := strings.TrimSpace(witness.Signature)
		if signer == "" || sig == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[signer]; ok {
			continue
		}
		seen[signer] = struct{}{}
		out = append(out, ExecutionResult{
			Height:              block.ID,
			Round:               block.Round,
			BlockHash:           strings.TrimSpace(block.BlockHash),
			Signer:              signer,
			ResultHash:          execHash,
			TxMerkle:            txMerkle,
			ExecutionResultHash: canonicalExecutionResultHash(block.ID, strings.TrimSpace(block.BlockHash), execHash, txMerkle),
			Signature:           sig,
		})
	}
	return out
}

// executionResultFinalityWitnessSigners returns the validator IDs whose
// execution-result signatures prove this exact proposal/result. It is used only
// as a compatibility finality witness when a node has already collected an
// execution quorum but has not separately observed enough commit-vote messages.
func (n *Node) executionResultFinalityWitnessSigners(block Block, execHash string, txMerkle string, results []ExecutionResult) []string {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" || strings.TrimSpace(execHash) == "" {
		return nil
	}
	execHash = strings.TrimSpace(execHash)
	txMerkle = strings.TrimSpace(txMerkle)
	// `validators` stores whether the related condition is satisfied.
	validators, _, ok := n.deterministicCommitteeValidatorsForHeight(block.ID)
	if !ok || len(validators) == 0 {
		validators = n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	}
	if len(validators) == 0 {
		return nil
	}
	// `validatorSet` stores whether the related condition is satisfied.
	validatorSet := make(map[string]struct{}, len(validators))
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(results))
	// `result` tracks the result produced by this operation.
	for _, result := range results {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(result.Signer)
		if signer == "" {
			continue
		}
		if _, ok := validatorSet[signer]; !ok {
			continue
		}
		if _, ok := seen[signer]; ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(result.ResultHash), execHash) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(result.TxMerkle), txMerkle) {
			continue
		}
		// `sigHex` stores the value produced by this operation.
		sigHex := strings.TrimSpace(result.Signature)
		if sigHex == "" {
			continue
		}
		// `blockHashHint` stores the block data handled by this operation.
		blockHashHint := strings.TrimSpace(result.BlockHash)
		if blockHashHint == "" {
			blockHashHint = strings.TrimSpace(block.BlockHash)
		}
		if !strings.EqualFold(blockHashHint, strings.TrimSpace(block.BlockHash)) {
			continue
		}
		// `sig` and `err` store the error produced by this operation.
		sig, err := hex.DecodeString(sigHex)
		if err != nil || len(sig) != ed25519.SignatureSize {
			continue
		}
		// `candidates` stores the value produced by this operation.
		candidates := n.execResultPubKeyCandidatesForHeight(signer, block.ID)
		if len(candidates) == 0 {
			if DebugConsensus {
				log.Printf("[EXEC-WITNESS-DROP] height=%d signer=%s reason=no_pubkey_candidate block=%s",
					block.ID, signer, ShortHash(block.BlockHash))
			}
			continue
		}
		// `round` stores the value produced by this operation.
		round := result.Round
		if round == 0 {
			round = block.Round
		}
		// `msg` stores the value produced by this operation.
		msg := ExecutionResultMsg{
			HeightHint:    block.ID,
			RoundHint:     round,
			BlockHashHint: blockHashHint,
			SigVersion:    execResultSigVersionV2,
			ExecHash:      execHash,
			TxMerkle:      txMerkle,
			Signer:        signer,
			Signature:     sigHex,
		}
		if !verifyExecutionResultSignature(msg, candidates, sig) {
			if DebugConsensus {
				log.Printf("[EXEC-WITNESS-DROP] height=%d signer=%s reason=invalid_signature block=%s candidates=%d",
					block.ID, signer, ShortHash(block.BlockHash), len(candidates))
			}
			continue
		}
		seen[signer] = struct{}{}
	}
	// `signers` stores the value produced by this operation.
	signers := make([]string, 0, len(seen))
	// `signer` tracks the current values while iterating.
	for signer := range seen {
		signers = append(signers, signer)
	}
	return canonicalValidatorIDs(signers)
}

// executionResultsFromCommitWitnesses converts signed commit quorum evidence
// into final-block execution witnesses for recovery paths where the separate
// execution vote cache is incomplete locally.
func executionResultsFromCommitWitnesses(block Block, execHash string, txMerkle string, witnesses []ValidatorSignature) ([]ExecutionResult, []string, int, bool) {
	results := canonicalExecutionResults(executionResultsWithCommitWitnesses(block, execHash, txMerkle, nil, witnesses))
	if len(results) == 0 {
		return nil, nil, 0, false
	}
	signers := make([]string, 0, len(results))
	for _, result := range results {
		if signer := normalizeValidatorID(result.Signer); signer != "" {
			signers = append(signers, signer)
		}
	}
	signers = canonicalValidatorIDs(signers)
	return results, signers, len(results), len(results) > 0
}

// proposalRequiresSignedCommitQuorum returns true when execution-result witness
// compatibility must not substitute for signed commit votes.
func (n *Node) proposalRequiresSignedCommitQuorum(block Block, execHash string, txMerkle string) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
	switch mode {
	case "STRICT", "SIGNED_COMMIT", "SIGNED_COMMIT_QUORUM", "FINALITY_COMMIT":
		return true
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, txMerkle, execHash)
	return n.unsignedExecPoolHintSeen(block.ID, proposalKey)
}

// executionCommitPrecondition implements the execution commit precondition helper.
func (n *Node) executionCommitPrecondition(epoch uint64, leaderBlock Block) (int, int, bool, string) {
	if n == nil || epoch == 0 || leaderBlock.ID != epoch {
		return 0, 0, false, "invalid_commit_target"
	}
	// `validators` and `ok` store whether the related condition is satisfied.
	validators, _, ok := n.deterministicCommitteeValidatorsForHeight(epoch)
	if !ok {
		return 0, 0, false, "commit_committee_unresolved"
	}
	// `total` stores the measured quantity used by this operation.
	total := len(validators)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(epoch)
	if total == 0 || required == 0 {
		return 0, required, false, "commit_quorum_unresolved"
	}
	// `lockedBlock`, `lockedVotes`, and `keepLocked` store the synchronization state protecting shared data.
	lockedBlock, lockedVotes, keepLocked, _ := n.quorumLockedProposalLockState(epoch)
	if !keepLocked {
		return 0, required, false, "precommit_quorum_missing"
	}
	if proposalConflictsWithAcceptedLock(lockedBlock, leaderBlock) {
		return lockedVotes, required, false, "precommit_block_mismatch"
	}
	if lockedVotes < required {
		return lockedVotes, required, false, "precommit_votes_shortfall"
	}
	return lockedVotes, required, true, "precommit_quorum"
}

// leaderFromExecHash implements the leader from exec hash helper.
func leaderFromExecHash(execHash string, epoch uint64, validators []string) string {
	// `ordered` stores the value produced by this operation.
	ordered := deterministicStakeHashOrderedValidatorIDs(validators, nil)
	if len(ordered) == 0 {
		return ""
	}

	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", execHash, epoch)))
	// `idx` stores the current position in the related collection.
	idx := binary.BigEndian.Uint64(sum[:8]) % uint64(len(ordered))
	return ordered[idx]
}

// leaderBlockCommitmentsReadyForFinality implements the leader block commitments ready for finality helper.
func (n *Node) leaderBlockCommitmentsReadyForFinality(block Block) bool {
	if n == nil {
		return false
	}
	if block.ID == 0 {
		return true
	}
	if strings.TrimSpace(block.ValidatorSetHash) == "" {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] missing validator_set_hash height=%d\n", block.ID)
		}
		return false
	}
	// `expectedHash` and `expectedSource` store the digest used to identify or verify the related data.
	expectedHash, expectedSource := n.expectedValidatorSetHashWithSource(block.ID)
	if validatorSetSourceIsChainAuthoritative(expectedSource) && strings.TrimSpace(expectedHash) == "" {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] missing parent validator_set commitment height=%d source=%s\n", block.ID, expectedSource)
		}
		return false
	}
	if expectedHash != "" &&
		!strings.EqualFold(strings.TrimSpace(expectedHash), strings.TrimSpace(block.ValidatorSetHash)) &&
		validatorSetSourceIsChainAuthoritative(expectedSource) {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] validator_set_hash mismatch height=%d expected=%s got=%s source=%s\n",
				block.ID, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash), expectedSource)
		}
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] next-set commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] next-set-root commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorSetRootCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] set-root commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] registry commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	return true
}

// executionProposalBlockForResult implements the execution proposal block for result helper.
func (n *Node) executionProposalBlockForResult(epoch uint64, execHash string, txMerkle string) (Block, bool) {
	if n == nil || epoch == 0 || execHash == "" {
		return Block{}, false
	}
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range n.candidateProposalBlocksForEpoch(epoch) {
		if block.ID != epoch {
			continue
		}
		if txMerkle != "" && block.MempoolRoot != txMerkle {
			continue
		}
		if txMerkle == "" && block.MempoolRoot != "" {
			continue
		}
		// `expected` stores the value produced by this operation.
		expected := strings.TrimSpace(block.StateRoot)
		if expected == "" {
			expected = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
		}
		if expected == execHash {
			return block, true
		}
	}
	return Block{}, false
}

// executionResultAlreadyCommitted implements the execution result already committed helper.
func (n *Node) executionResultAlreadyCommitted(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	// `committedHeight` stores the value produced by this operation.
	committedHeight := uint64(0)
	if n.Blockchain != nil {
		committedHeight = n.Blockchain.Height()
	}
	n.commitMu.Lock()
	if n.committedHeight > committedHeight {
		committedHeight = n.committedHeight
	}
	n.commitMu.Unlock()
	return committedHeight >= height
}

// committedReplayFenceHeight implements the committed replay fence height helper.
func (n *Node) committedReplayFenceHeight() uint64 {
	if n == nil {
		return 0
	}
	// `fence` stores the value produced by this operation.
	fence := uint64(0)
	if n.Blockchain != nil {
		fence = n.Blockchain.Height()
	}
	n.commitMu.Lock()
	if n.committedHeight > fence {
		fence = n.committedHeight
	}
	if n.finalizedHeight > fence {
		fence = n.finalizedHeight
	}
	if n.lastCommitHeight > fence {
		fence = n.lastCommitHeight
	}
	n.commitMu.Unlock()
	return fence
}

// isCommittedReplayHeight implements the is committed replay height helper.
func (n *Node) isCommittedReplayHeight(height uint64) bool {
	return height > 0 && height <= n.committedReplayFenceHeight()
}

// beginExecutionCommitApply implements the begin execution commit apply helper.
func (n *Node) beginExecutionCommitApply(height uint64, hash string) bool {
	if n == nil || height == 0 || strings.TrimSpace(hash) == "" {
		return false
	}
	hash = strings.TrimSpace(hash)
	n.commitMu.Lock()
	defer n.commitMu.Unlock()
	if n.committedHeight >= height {
		return false
	}
	if n.committed != nil {
		// `existing` stores the value produced by this operation.
		if existing := strings.TrimSpace(n.committed[height]); existing != "" {
			return false
		}
	}
	if n.commitInFlight == nil {
		n.commitInFlight = make(map[uint64]string)
	}
	// `existing` stores the value produced by this operation.
	if existing := strings.TrimSpace(n.commitInFlight[height]); existing != "" {
		return false
	}
	n.commitInFlight[height] = hash
	return true
}

// finishExecutionCommitApply implements the finish execution commit apply helper.
func (n *Node) finishExecutionCommitApply(height uint64, hash string) {
	if n == nil || height == 0 {
		return
	}
	hash = strings.TrimSpace(hash)
	n.commitMu.Lock()
	defer n.commitMu.Unlock()
	if n.commitInFlight == nil {
		return
	}
	// `existing` stores the value produced by this operation.
	if existing := strings.TrimSpace(n.commitInFlight[height]); existing == "" || existing == hash {
		delete(n.commitInFlight, height)
	}
}

// finalizeExecutionResult implements the finalize execution result helper.
func (n *Node) finalizeExecutionResult(epoch uint64, execHash string, txMerkle string, results []ExecutionResult, signers []string) bool {
	if n.consensusRecomputePauseActive() {
		return false
	}
	if n.executionResultAlreadyCommitted(epoch) {
		return n.advanceConsensusToCommittedTip("finalize_execution_result_already_committed")
	}
	// `ready` and `reason` store the value produced by this operation.
	if ready, reason := n.localExecutionFinalityReady(epoch); !ready {
		if n.shouldLogLivenessReason(fmt.Sprintf("exec_commit_defer:%d:%s", epoch, reason), 5*time.Second) {
			// `localHeight` stores the value produced by this operation.
			localHeight := uint64(0)
			if n.Blockchain != nil {
				localHeight = n.Blockchain.Height()
			}
			log.Printf("[EXEC-COMMIT-DEFER] height=%d reason=%s local=%d exec=%s tx_merkle=%s",
				epoch,
				reason,
				localHeight,
				ShortHash(execHash),
				ShortHash(txMerkle),
			)
		}
		return false
	}
	// `leaderBlock` and `ok` store whether the related condition is satisfied.
	leaderBlock, ok := n.executionProposalBlockForResult(epoch, execHash, txMerkle)
	if !ok || leaderBlock.ID != epoch {
		return false
	}
	if !n.leaderBlockCommitmentsReadyForFinality(leaderBlock) {
		return false
	}
	if txMerkle != "" && leaderBlock.MempoolRoot != txMerkle {
		return false
	}
	if txMerkle == "" && leaderBlock.MempoolRoot != "" {
		return false
	}
	// `executionWitnessSigners` stores execution-result signatures that can
	// safely stand in as finality witnesses when signed commit votes have not
	// propagated locally yet.
	executionWitnessSigners := n.executionResultFinalityWitnessSigners(leaderBlock, execHash, txMerkle, results)
	// `requiresSignedCommitQuorum` stores whether compatibility witnesses are
	// unsafe for this proposal.
	requiresSignedCommitQuorum := n.proposalRequiresSignedCommitQuorum(leaderBlock, execHash, txMerkle)
	// `precommitVotes`, `required`, and `commitReady` store the request data being processed.
	precommitVotes, required, commitReady, _ := n.executionCommitPrecondition(epoch, leaderBlock)
	if !commitReady {
		if !requiresSignedCommitQuorum && required > 0 && len(executionWitnessSigners) >= required {
			precommitVotes = len(executionWitnessSigners)
			commitReady = true
		}
	}
	if !commitReady {
		if DebugConsensus {
			log.Printf("[EXEC-COMMIT-DEFER] height=%d reason=precommit_not_ready votes=%d required=%d block=%s",
				epoch,
				precommitVotes,
				required,
				ShortHash(leaderBlock.BlockHash),
			)
		}
		return false
	}

	// `expected` stores the value produced by this operation.
	expected := leaderBlock.StateRoot
	if expected == "" {
		expected = n.ExecuteBlockAndGetStateRoot(leaderBlock)
	}
	if expected == "" || expected != execHash {
		return false
	}
	// `commitSigners`, `commitWitnesses`, `commitCount`, and `commitRequired` store the measured quantity used by this operation.
	commitSigners, commitWitnesses, commitCount, commitRequired := n.commitVoteEvidenceForResult(epoch, leaderBlock.BlockHash, expected, leaderBlock.MempoolRoot)
	if commitRequired == 0 {
		commitRequired = required
	}
	if commitCount < commitRequired {
		if !requiresSignedCommitQuorum && commitRequired > 0 && len(executionWitnessSigners) >= commitRequired {
			commitSigners = executionWitnessSigners
			commitWitnesses = nil
			commitCount = len(executionWitnessSigners)
		}
	}
	if commitCount < commitRequired {
		if DebugConsensus {
			log.Printf("[EXEC-COMMIT-DEFER] height=%d reason=signed_commit_votes_shortfall votes=%d required=%d block=%s",
				epoch,
				commitCount,
				commitRequired,
				ShortHash(leaderBlock.BlockHash),
			)
		}
		return false
	}

	// `final` stores the value produced by this operation.
	final := leaderBlock
	final.BlockTime = LogicalTimeForEpochTick(epoch, TickFinalize)
	final.Timestamp = int64(SystemTimeUnits(final.BlockTime))
	final.StateRoot = execHash
	// `validators` stores whether the related condition is satisfied.
	validators, _, _ := n.deterministicCommitteeValidatorsForHeight(epoch)
	// `strictRequired` stores the value produced by this operation.
	strictRequired := execQuorumRequired(len(validators))
	// `policy` stores the value produced by this operation.
	policy := quorumPolicySnapshot{
		Mode:             "NORMAL",
		Version:          quorumPolicyVersionV1,
		ActiveReadyCount: len(validators),
		Required:         required,
		StrictRequired:   strictRequired,
	}
	_ = signers
	// Quorum metadata is part of the signed proposal/hash envelope. Preserve it
	// when it matches the deterministic frozen-committee quorum.
	proposalPolicyDefined := strings.TrimSpace(leaderBlock.QuorumPolicyVersion) != "" ||
		strings.TrimSpace(leaderBlock.ConsensusMode) != "" ||
		leaderBlock.ActiveReadyCount > 0 ||
		leaderBlock.RequiredQuorum > 0 ||
		leaderBlock.StrictQuorum > 0
	// `proposalPolicyCompatible` stores the value produced by this operation.
	proposalPolicyCompatible := true
	if proposalPolicyDefined &&
		leaderBlock.RequiredQuorum > 0 &&
		required > 0 &&
		leaderBlock.RequiredQuorum != required {
		proposalPolicyCompatible = false
	}
	if proposalPolicyDefined &&
		leaderBlock.StrictQuorum > 0 &&
		policy.StrictRequired > 0 &&
		leaderBlock.StrictQuorum != policy.StrictRequired {
		proposalPolicyCompatible = false
	}
	// `preserveProposalPolicy` stores the value produced by this operation.
	preserveProposalPolicy := proposalPolicyDefined && proposalPolicyCompatible
	if preserveProposalPolicy {
		policy.Mode = strings.TrimSpace(leaderBlock.ConsensusMode)
		policy.Version = strings.TrimSpace(leaderBlock.QuorumPolicyVersion)
		policy.ActiveReadyCount = leaderBlock.ActiveReadyCount
		policy.Required = leaderBlock.RequiredQuorum
		policy.StrictRequired = leaderBlock.StrictQuorum
	} else if proposalPolicyDefined && DebugConsensus {
		fmt.Printf("[QUORUM-METADATA-REWRITE] height=%d proposal_mode=%s proposal_required=%d actual_mode=%s actual_required=%d signers=%d strict=%d\n",
			epoch,
			strings.TrimSpace(leaderBlock.ConsensusMode),
			leaderBlock.RequiredQuorum,
			strings.TrimSpace(policy.Mode),
			policy.Required,
			len(commitSigners),
			policy.StrictRequired,
		)
	}
	final.ConsensusMode = policy.Mode
	final.QuorumPolicyVersion = policy.Version
	final.ActiveReadyCount = policy.ActiveReadyCount
	final.RequiredQuorum = policy.Required
	final.StrictQuorum = policy.StrictRequired

	final.ExecutionResults = canonicalExecutionResults(executionResultsWithCommitWitnesses(leaderBlock, execHash, txMerkle, results, commitWitnesses))
	final.Signatures = commitSigners
	// `proposalHashForVotes` stores the value produced by this operation.
	proposalHashForVotes := strings.TrimSpace(leaderBlock.BlockHash)
	if proposalHashForVotes == "" {
		proposalHashForVotes = executionVoteProposalHashForFinalBlock(final)
	}
	// `i` tracks the current position in the related collection.
	for i := range final.ExecutionResults {
		// Always bind execution evidence to the canonical proposal hash for this
		// height. Votes are recorded against the leader block hash before quorum
		// metadata is stamped onto the finalized envelope; leaving that stale
		// value in place makes verifyBlock reject our own commits.
		if proposalHashForVotes != "" {
			final.ExecutionResults[i].BlockHash = proposalHashForVotes
		} else if strings.TrimSpace(final.ExecutionResults[i].BlockHash) == "" {
			final.ExecutionResults[i].BlockHash = executionVoteProposalHashForFinalBlock(final)
		}
		if final.ExecutionResults[i].Round == 0 {
			final.ExecutionResults[i].Round = final.Round
		}
		final.ExecutionResults[i].Height = final.ID
		final.ExecutionResults[i].TxMerkle = txMerkle
		final.ExecutionResults[i].ExecutionResultHash = executionResultHashFromBlockResult(final.ExecutionResults[i], final)
	}
	n.attachFinalityCommitmentsForProposal(&final, proposalHashForVotes)
	final.BlockHash = HashBlock(final)
	n.attachFinalityCertificateForProposal(&final, proposalHashForVotes)
	if n.executionResultAlreadyCommitted(final.ID) {
		return n.advanceConsensusToCommittedTip("finalize_execution_result_commit_raced")
	}
	if !n.beginExecutionCommitApply(final.ID, final.BlockHash) {
		if n.executionResultAlreadyCommitted(final.ID) {
			return n.advanceConsensusToCommittedTip("finalize_execution_result_apply_inflight_committed")
		}
		return false
	}
	defer n.finishExecutionCommitApply(final.ID, final.BlockHash)
	log.Printf("[EXEC-COMMIT] height=%d reason=precommit_quorum round=%d block=%s votes=%d required=%d precommits=%d",
		final.ID,
		final.Round,
		ShortHash(final.BlockHash),
		commitCount,
		commitRequired,
		precommitVotes,
	)

	// `finalValidators` stores the value produced by this operation.
	finalValidators := n.freezeValidatorSetForHeight(final.ID, n.GetConsensusValidators(int(final.ID)))
	// `err` stores the error produced by this operation.
	if err := verifyBlockQuorumMetadata(final, len(finalValidators)); err != nil {
		log.Printf("[EXEC-COMMIT-REJECT] height=%d block=%s reason=%s", final.ID, ShortHash(final.BlockHash), err.Error())
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyFinalityCommitments(final, finalValidators); err != nil {
		log.Printf("[EXEC-COMMIT-REJECT] height=%d block=%s reason=%s", final.ID, ShortHash(final.BlockHash), err.Error())
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.validateCommittedBlockQuorumEvidence(final); err != nil {
		log.Printf("[EXEC-COMMIT-REJECT] height=%d block=%s reason=%s", final.ID, ShortHash(final.BlockHash), err.Error())
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}

	// `before` stores the value produced by this operation.
	before := n.Blockchain.Height()
	n.setLogicalTick(epoch, TickFinalize)
	// `err` stores the error produced by this operation.
	if err := n.ReceiveBlock(final, n.Blockchain); err != nil {
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}
	// `after` stores the value produced by this operation.
	after := n.Blockchain.Height()
	if after > before && ResultGossipOnly && n.Consensus != nil {
		n.Consensus.mu.Lock()
		n.Consensus.Paused = false
		n.Consensus.Syncing = false
		n.Consensus.SyncTarget = 0
		n.Consensus.syncInFlight = false
		n.Consensus.mu.Unlock()
	}
	if after > before {
		n.clearAcceptedProposal(epoch)
		n.clearLeaderBlock(epoch)
		n.clearCommitVoteStateUpTo(epoch)
		n.schedulePostCommitConsensusDrain(epoch)
	}
	return true
}

// handleExecutionResultMsg handles execution result msg.
func (n *Node) handleExecutionResultMsg(res ExecutionResultMsg) {
	n.processExecutionResultMsg(res, true)
}

// queueExecResult implements the queue exec result helper.
func (n *Node) queueExecResult(res ExecutionResultMsg) {
	res = stripEmbeddedExecutionVoteBlockForQueue(res)
	res.Signer = normalizeValidatorID(res.Signer)
	if res.HeightHint == 0 || res.Signer == "" || res.ExecHash == "" {
		return
	}
	if n.isCommittedReplayHeight(res.HeightHint) {
		return
	}
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedReplayFenceHeight()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("%d", res.HeightHint)
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()

	if n.queuedExecVotes == nil {
		n.queuedExecVotes = make(map[string][]ExecutionResultMsg)
	}
	// Avoid duplicate queued entries from the same signer for the same hash.
	for _, existing := range n.queuedExecVotes[key] {
		if existing.Signer == res.Signer &&
			existing.ExecHash == res.ExecHash &&
			existing.TxMerkle == res.TxMerkle &&
			existing.RoundHint == res.RoundHint &&
			existing.BlockHashHint == res.BlockHashHint {
			return
		}
	}
	n.queuedExecVotes[key] = append(n.queuedExecVotes[key], res)
	n.trimConsensusCachesLocked(committedHeight)
}

// shouldTreatUnresolvedExecutionVoteAsStaleAccept implements the should treat unresolved execution vote as stale accept helper.
func (n *Node) shouldTreatUnresolvedExecutionVoteAsStaleAccept(res ExecutionResultMsg, currentEpoch uint64) bool {
	if n == nil || res.HeightHint == 0 || currentEpoch == 0 {
		return false
	}
	if res.HeightHint != currentEpoch {
		return false
	}
	// `currentRound` and `ok` store whether the related condition is satisfied.
	currentRound, _, ok := n.consensusRoundSnapshot(currentEpoch)
	if !ok {
		currentRound = n.localProposerRoundForHeight(currentEpoch)
	}
	return res.RoundHint == currentRound
}

// processExecutionResultMsg implements the process execution result msg helper.
func (n *Node) processExecutionResultMsg(res ExecutionResultMsg, allowQueue bool) {
	res.Signer = normalizeValidatorID(res.Signer)
	_ = allowQueue
	if res.Signer == "" || res.ExecHash == "" {
		n.logExecutionVoteDrop("missing_signer_or_exec", res, execProposalSnapshot{})
		return
	}

	if res.HeightHint == 0 {
		// Height hint is mandatory to scope execution votes per epoch.
		n.logExecutionVoteDrop("missing_height", res, execProposalSnapshot{})
		return
	}
	if n.consensusRecomputePauseActive() {
		if allowQueue {
			n.queueExecResult(res)
			n.logExecutionVoteDrop("queued_recompute_pause", res, execProposalSnapshot{})
		}
		return
	}
	// `blocked` and `reason` store the block data handled by this operation.
	if blocked, reason, _ := n.consensusSyncGateForHeight(res.HeightHint); blocked {
		if allowQueue {
			n.queueExecResult(res)
			if reason == "" {
				reason = "syncing"
			}
			n.logExecutionVoteDrop("queued_"+reason, res, execProposalSnapshot{})
		}
		return
	}
	// `currentEpoch` stores the value produced by this operation.
	currentEpoch := n.currentEpoch()
	// Future-epoch execution votes are expected during late join/catch-up.
	// Queue them instead of misclassifying as "non-active validator".
	if res.HeightHint > currentEpoch {
		if allowQueue {
			n.queueExecResult(res)
			n.maybeSyncToBestObservedHeight("future_exec_vote")
			if n.Blockchain != nil {
				localHeight := n.Blockchain.Height()
				if res.HeightHint > localHeight {
					n.maybeRecoverMissingBlock(localHeight+1, "future_exec_vote")
				}
			}
			n.logExecutionVoteDrop("queued_future_epoch", res, execProposalSnapshot{})
		}
		return
	}
	if executionVoteTooFarBehind(currentEpoch, res.HeightHint) {
		return
	}
	if n.isCommittedReplayHeight(res.HeightHint) {
		if _, proposalSnap, ok := n.resolveExecutionVoteProposal(res.HeightHint, res); ok && proposalSnap.ProposalKey != "" {
			n.logExecutionVoteDrop("stale_committed_height", res, proposalSnap)
		} else {
			n.logExecutionVoteStaleAccept("committed_proposal_unresolved", res)
		}
		return
	}
	// `targetEpoch` stores the value produced by this operation.
	targetEpoch := res.HeightHint
	// Only validators can send execution votes.
	validators := n.freezeValidatorSetForHeight(res.HeightHint, n.GetConsensusValidators(int(res.HeightHint)))
	if len(validators) == 0 {
		if allowQueue {
			n.queueExecResult(res)
			n.logExecutionVoteDrop("queued_missing_validator_set", res, execProposalSnapshot{})
		}
		return
	}
	// `allowed` stores whether the related condition is satisfied.
	allowed := false
	// `v` tracks the current values while iterating.
	for _, v := range validators {
		if v == res.Signer {
			allowed = true
			break
		}
	}
	if !allowed {
		if DebugConsensus {
			fmt.Printf("Ignoring exec hash from non-active validator %s @ epoch %d\n",
				ShortID(res.Signer), res.HeightHint)
		}
		n.logExecutionVoteDrop("non_active_validator", res, execProposalSnapshot{})
		return
	}
	res = normalizeEmbeddedExecutionVoteHints(res)
	// `embeddedVoteSignatureVerified` stores the value produced by this operation.
	embeddedVoteSignatureVerified := false
	if res.Block != nil {
		if res.Signature == "" {
			n.logExecutionVoteDrop("missing_signature", res, execProposalSnapshot{})
			return
		}
		// `sig` and `err` store the error produced by this operation.
		sig, err := hex.DecodeString(res.Signature)
		if err != nil {
			n.logExecutionVoteDrop("invalid_signature_encoding", res, execProposalSnapshot{})
			return
		}
		// `candidates` stores the value produced by this operation.
		candidates := n.execResultPubKeyCandidatesForHeight(res.Signer, targetEpoch)
		if len(candidates) == 0 {
			if allowQueue {
				n.queueExecResult(res)
				n.logExecutionVoteDrop("queued_missing_pubkey", res, execProposalSnapshot{})
			}
			return
		}
		if !verifyExecutionResultSignature(res, candidates, sig) {
			if DebugConsensus {
				fmt.Printf("Invalid embedded execution result signature: signer=%s height=%d exec=%s\n",
					ShortID(res.Signer), res.HeightHint, ShortHash(res.ExecHash))
			}
			n.logExecutionVoteDrop("invalid_signature", res, execProposalSnapshot{})
			return
		}
		embeddedVoteSignatureVerified = true
	}
	n.commitMu.Lock()
	// `committedHeight` stores the value produced by this operation.
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	// `committedEpoch` stores the value produced by this operation.
	committedEpoch := res.HeightHint <= committedHeight

	if res.Block != nil {
		// `observed` and `reason` store the value produced by this operation.
		if observed, reason := n.observeExecutionVoteProposalBlock(res); !observed {
			if reason == "" {
				reason = "embedded_proposal_invalid"
			}
			n.logExecutionVoteDrop("embedded_proposal_"+reason, res, execProposalSnapshot{})
			return
		}
		n.processQueuedExecutionVotesForProposal(*res.Block)
	}
	// `leaderBlock`, `proposalSnap`, and `ok` store whether the related condition is satisfied.
	leaderBlock, proposalSnap, ok := n.resolveExecutionVoteProposal(targetEpoch, res)
	if !ok {
		if committedEpoch {
			n.logExecutionVoteStaleAccept("committed_proposal_unresolved", res)
			return
		}
		if allowQueue {
			n.queueExecResult(res)
			if n.shouldTreatUnresolvedExecutionVoteAsStaleAccept(res, currentEpoch) {
				n.logExecutionVoteStaleAccept("queued_proposal_unresolved", res)
			} else {
				n.logExecutionVoteDrop("queued_proposal_unresolved", res, execProposalSnapshot{})
			}
		}
		return
	}
	if !n.leaderBlockCommitmentsReadyForFinality(leaderBlock) {
		if DebugConsensus {
			fmt.Printf("Ignoring execution vote at height %d due to invalid leader commitments\n", res.HeightHint)
		}
		n.logExecutionVoteDrop("invalid_leader_commitments", res, proposalSnap)
		return
	}
	// `expected` stores the value produced by this operation.
	expected := leaderBlock.StateRoot
	if expected == "" {
		expected = n.ExecuteBlockAndGetStateRoot(leaderBlock)
	}
	if expected == "" {
		n.logExecutionVoteDrop("exec_unavailable", res, proposalSnap)
		return
	}
	if proposalSnap.StateRoot == "" {
		proposalSnap.StateRoot = expected
		proposalSnap.ProposalKey = proposalVoteKey(leaderBlock.ID, leaderBlock.Round, leaderBlock.BlockHash, leaderBlock.MempoolRoot, expected)
	}
	// `projectedVotes` stores the value produced by this operation.
	projectedVotes := getExecCountGlobal(targetEpoch, proposalSnap.ProposalKey, expected, proposalSnap.TxMerkle) + 1
	// `lockedBlock`, `lockedVotes`, `keepLocked`, and `lockReason` store the synchronization state protecting shared data.
	if lockedBlock, lockedVotes, keepLocked, lockReason := n.quorumLockedProposalHoldStateForIncomingRound(targetEpoch, leaderBlock, projectedVotes); keepLocked && proposalConflictsWithAcceptedLock(lockedBlock, leaderBlock) {
		if DebugConsensus {
			fmt.Printf("[EXEC-VOTE-RECENT] signer=%s epoch=%d locked_block=%s vote_block=%s locked_round=%d vote_round=%d locked_votes=%d reason=%s\n",
				ShortID(res.Signer),
				res.HeightHint,
				ShortHash(lockedBlock.BlockHash),
				ShortHash(proposalSnap.BlockHash),
				lockedBlock.Round,
				proposalSnap.Round,
				lockedVotes,
				lockReason,
			)
		}
		n.logExecutionVoteDrop("conflict_with_lock", res, proposalSnap)
		return
	}
	// `currentSnap` and `currentOK` store whether the related condition is satisfied.
	if currentSnap, currentOK := n.proposalSnapshotForEpoch(targetEpoch); currentOK && currentSnap.ProposalKey != proposalSnap.ProposalKey {
		if DebugConsensus {
			fmt.Printf("[EXEC-VOTE-RECENT] signer=%s epoch=%d locked_block=%s vote_block=%s locked_round=%d vote_round=%d\n",
				ShortID(res.Signer),
				res.HeightHint,
				ShortHash(currentSnap.BlockHash),
				ShortHash(proposalSnap.BlockHash),
				currentSnap.Round,
				proposalSnap.Round,
			)
		}
	}

	// Enforce validator identity with signature.
	if !embeddedVoteSignatureVerified {
		if res.Signature == "" {
			n.logExecutionVoteDrop("missing_signature", res, proposalSnap)
			return
		}
		// `sig` and `err` store the error produced by this operation.
		sig, err := hex.DecodeString(res.Signature)
		if err != nil {
			n.logExecutionVoteDrop("invalid_signature_encoding", res, proposalSnap)
			return
		}
		// `candidates` stores the value produced by this operation.
		candidates := n.execResultPubKeyCandidatesForHeight(res.Signer, targetEpoch)
		if len(candidates) == 0 {
			if allowQueue {
				n.queueExecResult(res)
				n.logExecutionVoteDrop("queued_missing_pubkey", res, proposalSnap)
			}
			return
		}
		if !verifyExecutionResultSignature(res, candidates, sig) {
			if DebugConsensus {
				fmt.Printf("Invalid execution result signature: signer=%s height=%d exec=%s\n",
					ShortID(res.Signer), res.HeightHint, ShortHash(res.ExecHash))
			}
			n.logExecutionVoteDrop("invalid_signature", res, proposalSnap)
			return
		}
	}

	if res.TxMerkle != leaderBlock.MempoolRoot {
		n.logExecutionVoteDrop("tx_merkle_mismatch", res, proposalSnap)
		return
	}
	// `expectedExecutionResultHash` stores the digest used to identify or verify the related data.
	expectedExecutionResultHash := executionResultHashFromProposal(targetEpoch, proposalSnap.ProposalKey, proposalSnap.BlockHash, res.ExecHash, res.TxMerkle)
	if !executionResultHashMatches(res.ExecutionResultHash, expectedExecutionResultHash) {
		n.logExecutionVoteDrop("execution_result_hash_mismatch", res, proposalSnap)
		return
	}
	res.ExecutionResultHash = expectedExecutionResultHash
	// `allowed` and `reason` store whether the related condition is satisfied.
	if allowed, reason := n.allowExecutionVoteIngress(res.Signer, res.HeightHint, proposalSnap.ProposalKey, res.ExecHash, res.TxMerkle); !allowed {
		if reason == "replay_cache" {
			if !committedEpoch && targetEpoch == currentEpoch && n.commitStallDuration() >= execQuorumEmergencyStallTimeout && n.canParticipateInConsensusNow() && res.ExecHash == expected {
				if n.maybeBroadcastExecutionVoteForBlock(leaderBlock, "replay_cache_stalled_marker_refresh") {
					return
				}
			}
			if n.tryFinalizeExecutionQuorumFromPool(targetEpoch, proposalSnap, leaderBlock, res.ExecHash, res.TxMerkle, reason) {
				return
			}
		}
		n.logExecutionVoteDrop(reason, res, proposalSnap)
		return
	}
	if res.ExecHash != expected {
		// `staleByProposal` stores the value produced by this operation.
		staleByProposal := strings.TrimSpace(res.BlockHashHint) != "" && res.BlockHashHint != proposalSnap.BlockHash
		if staleByProposal {
			if DebugConsensus {
				fmt.Printf("[EXEC-VOTE] stale_proposal ignored signer=%s epoch=%d vote_block=%s current_block=%s\n",
					ShortID(res.Signer), targetEpoch, ShortHash(res.BlockHashHint), ShortHash(proposalSnap.BlockHash))
			}
			n.logExecutionVoteDrop("stale_proposal", res, proposalSnap)
			return
		}
		// `staleByLiveness` stores the value produced by this operation.
		staleByLiveness := n.executionVoteSignerLikelyStale(res.Signer, targetEpoch)
		if staleByLiveness {
			if allowQueue {
				n.queueExecResult(res)
				n.logExecutionVoteDrop("queued_stale_liveness", res, proposalSnap)
			}
			if DebugConsensus {
				fmt.Printf("Deferred stale execution vote from %s @ epoch %d | expected=%s got=%s\n",
					ShortID(res.Signer), targetEpoch, ShortHash(expected), ShortHash(res.ExecHash))
			}
			return
		}
		log.Printf(
			"[EXEC-TRACE] height=%d tx_count=%d root=%s",
			targetEpoch,
			len(leaderBlock.Transactions),
			expected,
		)
		// `currentRuntimeLedgerHash`, `currentExecutionLedgerHash`, `tipHeight`, and `tipHash` store the digest used to identify or verify the related data.
		currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
		log.Printf("[EXEC-COMPARE] signer=%s epoch=%d round=%d proposal=%s vote_block=%s local_block=%s tx_count=%d tx_merkle=%s proposal_root=%s expected=%s got=%s current_runtime=%s current_execution=%s current_tip=%d/%s stale_by_proposal=%t stale_by_liveness=%t",
			ShortID(res.Signer),
			targetEpoch,
			res.RoundHint,
			proposalSnap.ProposalKey,
			ShortHash(res.BlockHashHint),
			ShortHash(proposalSnap.BlockHash),
			len(leaderBlock.Transactions),
			ShortHash(res.TxMerkle),
			ShortHash(proposalSnap.StateRoot),
			ShortHash(expected),
			ShortHash(res.ExecHash),
			ShortHash(currentRuntimeLedgerHash),
			ShortHash(currentExecutionLedgerHash),
			tipHeight,
			ShortHash(tipHash),
			staleByProposal,
			staleByLiveness,
		)
		log.Printf("[EXEC-MISMATCH] signer=%s epoch=%d round=%d proposal=%s vote_block=%s local_block=%s tx_merkle=%s expected=%s got=%s stale_by_proposal=%t stale_by_liveness=%t",
			ShortID(res.Signer),
			targetEpoch,
			res.RoundHint,
			proposalSnap.ProposalKey,
			ShortHash(res.BlockHashHint),
			ShortHash(proposalSnap.BlockHash),
			ShortHash(res.TxMerkle),
			ShortHash(expected),
			ShortHash(res.ExecHash),
			staleByProposal,
			staleByLiveness,
		)
		if DebugConsensus {
			fmt.Printf("Invalid execution vote from %s @ epoch %d | expected=%s got=%s\n",
				ShortID(res.Signer), targetEpoch, ShortHash(expected), ShortHash(res.ExecHash))
		}
		n.logExecutionVoteDrop("exec_hash_mismatch", res, proposalSnap)
		n.handleExecutionMismatchPolicy(res.Signer, targetEpoch, expected, res.ExecHash)
		return
	}
	n.resetExecutionMismatchStrike(res.Signer)
	if !committedEpoch && proposalSnap.ProposalKey != n.currentProposalVoteKey(targetEpoch) {
		_ = n.maybeAdoptProposalOnExecutionVote(leaderBlock)
	}

	// If we haven't broadcast yet for this epoch+hash and our local execution matches,
	// broadcast our execution result to help convergence.
	if !committedEpoch && n.canParticipateInConsensusNow() {
		// `localHash` stores the digest used to identify or verify the related data.
		localHash := expected
		if localHash == "" {
			localHash = n.ExecuteBlockAndGetStateRoot(leaderBlock)
		}
		if localHash != "" && localHash == res.ExecHash {
			n.setLogicalTick(targetEpoch, TickExec)
			n.maybeBroadcastExecutionVoteForBlock(leaderBlock, "exec_match")
		}
	}

	// Refresh validator liveness on any execution result
	n.validatorMu.Lock()
	// `st` and `ok` store whether the related condition is satisfied.
	if st, ok := n.validatorStatus[res.Signer]; ok {
		st.LastSeen = time.Now()
		st.Active = true
		st.Enabled = true
		st.ConsensusReadyKnown = true
		if targetEpoch > st.ReportedHeight {
			st.ReportedHeight = targetEpoch
			st.Height = targetEpoch
		}
		if targetEpoch > 0 && targetEpoch-1 > st.FinalizedHeight {
			st.FinalizedHeight = targetEpoch - 1
		}
		if targetEpoch > st.ExecEpoch {
			st.ExecEpoch = targetEpoch
		}
		if targetEpoch > st.ValidatorSetHeight {
			st.ValidatorSetHeight = targetEpoch
		}
	} else {
		// `finalizedHeight` stores the value produced by this operation.
		finalizedHeight := uint64(0)
		if targetEpoch > 0 {
			finalizedHeight = targetEpoch - 1
		}
		n.validatorStatus[res.Signer] = &ValidatorStatus{
			Height:              targetEpoch,
			ReportedHeight:      targetEpoch,
			FinalizedHeight:     finalizedHeight,
			ExecEpoch:           targetEpoch,
			ValidatorSetHeight:  targetEpoch,
			LastSeen:            time.Now(),
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
		}
	}
	n.recordValidatorRejoinSignedLocked(res.Signer, targetEpoch)
	n.validatorMu.Unlock()

	if DebugConsensus {
		fmt.Printf("[EXEC-HASH] hint=%d from=%s exec=%s\n",
			res.HeightHint, res.Signer, ShortHash(res.ExecHash))
	}

	// `epochValidators` stores the value produced by this operation.
	epochValidators := n.freezeValidatorSetForHeight(targetEpoch, n.GetConsensusValidators(int(targetEpoch)))
	// `total` stores the measured quantity used by this operation.
	total := len(epochValidators)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(targetEpoch)
	if required == 0 {
		required = execQuorumRequired(total)
	}
	// `storedCount`, `ok`, and `equivocation` store whether the related condition is satisfied.
	storedCount, ok, equivocation := recordExecResultGlobalWithRequired(targetEpoch, proposalSnap.ProposalKey, res.ExecHash, res.TxMerkle, ExecutionResult{
		Height:              targetEpoch,
		Round:               res.RoundHint,
		BlockHash:           proposalSnap.BlockHash,
		Signer:              res.Signer,
		ResultHash:          res.ExecHash,
		TxMerkle:            res.TxMerkle,
		ExecutionResultHash: res.ExecutionResultHash,
		Signature:           strings.TrimSpace(res.Signature),
	}, required)
	if equivocation {
		if n.tryFinalizeExecutionQuorumFromPool(targetEpoch, proposalSnap, leaderBlock, res.ExecHash, res.TxMerkle, "equivocation_existing_quorum") {
			return
		}
		if DebugConsensus {
			fmt.Printf("Execution equivocation detected from %s @ epoch %d\n", ShortID(res.Signer), targetEpoch)
		}
		n.logExecutionVoteDrop("exec_equivocation", res, proposalSnap)
		n.handleExecutionEquivocationPolicy(res.Signer, targetEpoch, res.ExecHash)
		return
	}
	if !ok {
		if n.tryFinalizeExecutionQuorumFromPool(targetEpoch, proposalSnap, leaderBlock, res.ExecHash, res.TxMerkle, "duplicate_exec_vote") {
			return
		}
		n.logExecutionVoteDrop("duplicate_exec_vote", res, proposalSnap)
		return
	}
	// Keep the local signer map as a best-effort mirror. Duplicate/equivocation
	// decisions were already made atomically by recordExecResultGlobalWithRequired.
	_ = n.markExecSignerSeenForProposalResult(targetEpoch, proposalSnap.ProposalKey, res.ExecHash, res.Signer)
	n.mirrorConsensusExecVote(targetEpoch, proposalSnap.BlockHash, ExecutionResult{
		Height:              targetEpoch,
		Round:               res.RoundHint,
		BlockHash:           proposalSnap.BlockHash,
		Signer:              res.Signer,
		ResultHash:          res.ExecHash,
		TxMerkle:            res.TxMerkle,
		ExecutionResultHash: res.ExecutionResultHash,
		Signature:           strings.TrimSpace(res.Signature),
	})
	if committedEpoch {
		n.storeVoteButIgnoreForCommit(targetEpoch, res, proposalSnap, storedCount)
	}

	if DebugConsensus {
		fmt.Printf("Execution hash received: %s @ epoch %d\n",
			ShortID(res.Signer), targetEpoch)
	}

	// Only finalize for the current epoch; future epochs are stored.
	if targetEpoch != currentEpoch {
		return
	}
	n.setLogicalTick(targetEpoch, TickVote)

	if total == 0 || required == 0 {
		return
	}
	n.logExecutionVoteAccept("recorded", res, proposalSnap, storedCount, required)
	if DebugConsensus {
		fmt.Printf("[EXEC-POOL] epoch=%d hash=%s voters=%d/%d\n",
			targetEpoch, ShortHash(res.ExecHash), storedCount, total)
		fmt.Printf("[EXEC-QUORUM] epoch=%d required=%d total=%d votes=%d\n",
			targetEpoch, required, total, storedCount)
	}

	if storedCount < required {
		if n.shouldForceExecutionVoteRebroadcast(targetEpoch, proposalSnap.ProposalKey, storedCount, execVoteRebroadcastCooldown) {
			n.maybeRebroadcastExecutionVoteForBlock(leaderBlock, "quorum_shortfall")
		}
		return
	}

	// `results` and `ok` store whether the related condition is satisfied.
	results, _, _, ok := getExecResultsGlobal(targetEpoch, proposalSnap.ProposalKey, res.ExecHash, res.TxMerkle)
	if !ok {
		return
	}

	if DebugConsensus {
		fmt.Printf("Execution quorum reached (hash=%s votes=%d/%d)\n",
			ShortHash(res.ExecHash), storedCount, total)
	}

	_ = n.maybeAdoptProposalOnExecutionVote(leaderBlock)
	_ = n.broadcastCommitVoteForProposal(leaderBlock, res.ExecHash, res.TxMerkle)
	_ = n.finalizeExecutionResult(targetEpoch, res.ExecHash, res.TxMerkle, results, nil)
}

// recordCandidateExecutionResult implements the record candidate execution result helper.
func (n *Node) recordCandidateExecutionResult(res ExecutionResultMsg) bool {
	if res.Signer == "" || res.ExecHash == "" || res.HeightHint == 0 {
		return false
	}
	n.candidateMu.RLock()
	// `cand` and `ok` store whether the related condition is satisfied.
	cand, ok := n.candidates[res.Signer]
	if !ok || cand == nil || cand.PermanentBan {
		n.candidateMu.RUnlock()
		return false
	}
	if cand.BanUntil > 0 && res.HeightHint < cand.BanUntil {
		n.candidateMu.RUnlock()
		return false
	}
	// `pub` stores the value produced by this operation.
	pub := cand.PubKey
	n.candidateMu.RUnlock()

	if len(pub) != ed25519.PublicKeySize || res.Signature == "" {
		return false
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(res.Signature)
	if err != nil {
		return false
	}
	if !verifyExecutionResultSignature(res, []ed25519.PublicKey{pub}, sig) {
		return false
	}
	if !executionResultHashMatches(res.ExecutionResultHash, executionResultHashFromMessage(res)) {
		return false
	}

	// `expected` stores the value produced by this operation.
	expected := ""
	if res.HeightHint == n.currentEpoch() {
		// `leaderBlock` and `ok` store whether the related condition is satisfied.
		leaderBlock, ok := n.getLeaderBlock(res.HeightHint)
		if ok {
			if res.TxMerkle != leaderBlock.MempoolRoot {
				return false
			}
			expected = leaderBlock.StateRoot
			if expected == "" {
				expected = n.ExecuteBlockAndGetStateRoot(leaderBlock)
			}
		}
	} else {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := n.Blockchain.GetBlock(res.HeightHint); ok {
			if res.TxMerkle != block.MempoolRoot {
				return false
			}
			expected = block.StateRoot
		}
	}

	n.candidateMu.Lock()
	cand, ok = n.candidates[res.Signer]
	if !ok || cand == nil {
		n.candidateMu.Unlock()
		return false
	}
	if cand.ExecHashes == nil {
		cand.ExecHashes = make(map[uint64]string)
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := cand.ExecHashes[res.HeightHint]; ok && existing != res.ExecHash {
		if res.Signer == "F" || res.Signer == "G" {
			fmt.Printf("DBG candidate %s exec_equivocation height=%d existing=%s new=%s\n",
				res.Signer,
				res.HeightHint,
				ShortHash(existing),
				ShortHash(res.ExecHash),
			)
		}
		n.recordCandidateMisbehaviorLocked(res.Signer, cand, res.HeightHint, "exec_equivocation")
		n.candidateMu.Unlock()
		return false
	}
	if expected != "" && res.ExecHash != expected {
		if res.Signer == "F" || res.Signer == "G" {
			fmt.Printf("DBG candidate %s exec_mismatch height=%d expected=%s got=%s\n",
				res.Signer,
				res.HeightHint,
				ShortHash(expected),
				ShortHash(res.ExecHash),
			)
		}
		n.recordCandidateMisbehaviorLocked(res.Signer, cand, res.HeightHint, "exec_mismatch")
		n.candidateMu.Unlock()
		return false
	}
	cand.ExecHashes[res.HeightHint] = res.ExecHash
	if res.Signer == "F" || res.Signer == "G" {
		fmt.Printf("DBG candidate %s exec_hash_stored height=%d hash=%s expected_known=%t\n",
			res.Signer,
			res.HeightHint,
			ShortHash(res.ExecHash),
			expected != "",
		)
	}
	// `pending` stores the value produced by this operation.
	pending := false
	if cand.PendingMatch != nil {
		pending = cand.PendingMatch[res.HeightHint]
	}
	n.candidateMu.Unlock()

	if pending {
		// `expected` stores the value used by this operation.
		var expected string
		n.Blockchain.mu.RLock()
		if res.HeightHint < uint64(len(n.Blockchain.Blocks)) {
			// `block` stores the synchronization state protecting shared data.
			block := n.Blockchain.Blocks[res.HeightHint]
			if block.ID == res.HeightHint {
				expected = block.StateRoot
			}
		}
		n.Blockchain.mu.RUnlock()
		if expected != "" {
			n.candidateMu.Lock()
			cand, ok = n.candidates[res.Signer]
			if ok && cand != nil && cand.PendingMatch != nil && cand.PendingMatch[res.HeightHint] {
				if res.ExecHash == expected {
					cand.MatchedEpochs++
				} else {
					n.recordCandidateMisbehaviorLocked(res.Signer, cand, res.HeightHint, "exec_mismatch_late")
				}
				delete(cand.PendingMatch, res.HeightHint)
			}
			n.candidateMu.Unlock()
		}
	}

	return true
}

// replayQueuedExecutionVotes implements the replay queued execution votes helper.
func (n *Node) replayQueuedExecutionVotes() {
	n.execResultsMu.Lock()
	if len(n.queuedExecVotes) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	// `queued` stores the value produced by this operation.
	queued := n.queuedExecVotes
	n.queuedExecVotes = make(map[string][]ExecutionResultMsg)
	n.execResultsMu.Unlock()

	// `msgs` tracks the current values while iterating.
	for _, msgs := range queued {
		// `msg` tracks the current values while iterating.
		for _, msg := range msgs {
			n.processExecutionResultMsg(msg, true)
		}
	}
	n.tryFinalizeFromStoredResults()
}

// replayQueuedExecutionVotesForEpoch implements the replay queued execution votes for epoch helper.
func (n *Node) replayQueuedExecutionVotesForEpoch(epoch uint64) {
	if n == nil || epoch == 0 {
		return
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("%d", epoch)
	n.execResultsMu.Lock()
	if len(n.queuedExecVotes) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	// `msgs` stores the value produced by this operation.
	msgs := append([]ExecutionResultMsg(nil), n.queuedExecVotes[key]...)
	if len(msgs) > 0 {
		delete(n.queuedExecVotes, key)
	}
	n.execResultsMu.Unlock()

	// `msg` tracks the current values while iterating.
	for _, msg := range msgs {
		n.processExecutionResultMsg(msg, true)
	}
	n.tryFinalizeFromStoredResults()
}

// schedulePostCommitConsensusDrain implements the schedule post commit consensus drain helper.
func (n *Node) schedulePostCommitConsensusDrain(committedEpoch uint64) {
	if n == nil || n.isShuttingDown() {
		return
	}
	// `nextEpoch` stores the value produced by this operation.
	nextEpoch := committedEpoch + 1
	if nextEpoch == 0 {
		return
	}
	if !n.scheduleConsensusPriorityTask(func() {
		n.runPostCommitConsensusDrain(nextEpoch)
	}) {
		n.SafeGo(fmt.Sprintf("post_commit_queue_drain_%d", nextEpoch), func() {
			n.runPostCommitConsensusDrain(nextEpoch)
		})
	}
}

// runPostCommitConsensusDrain replays next-height consensus work only after the
// previous ReceiveBlock call has released its non-reentrant apply lock.
func (n *Node) runPostCommitConsensusDrain(nextEpoch uint64) {
	if n == nil || nextEpoch == 0 || n.isShuttingDown() {
		return
	}
	n.receiveMu.Lock()
	n.receiveMu.Unlock()
	if n.isShuttingDown() || n.currentEpoch() != nextEpoch {
		return
	}
	n.replayQueuedLeaderBlocksForCurrentEpoch()
	n.replayQueuedExecutionVotesForEpoch(nextEpoch)
	n.maybeBroadcastCurrentLeaderExecutionVote("post_commit_queue_drain")
}

// processQueuedExecutionVotesForProposal implements the process queued execution votes for proposal helper.
func (n *Node) processQueuedExecutionVotesForProposal(block Block) {
	if n == nil || block.ID == 0 {
		return
	}
	// `proposalSnap` stores the value produced by this operation.
	proposalSnap := proposalSnapshotFromBlock(block)
	if proposalSnap.Epoch == 0 {
		return
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("%d", block.ID)
	n.execResultsMu.Lock()
	if len(n.queuedExecVotes) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	// `msgs` stores the value produced by this operation.
	msgs := n.queuedExecVotes[key]
	if len(msgs) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	// `replay` stores the value produced by this operation.
	replay := make([]ExecutionResultMsg, 0, len(msgs))
	// `remaining` stores the value produced by this operation.
	remaining := make([]ExecutionResultMsg, 0, len(msgs))
	// `msg` tracks the current values while iterating.
	for _, msg := range msgs {
		if voteBelongsToCurrentProposal(msg, proposalSnap) {
			replay = append(replay, msg)
			continue
		}
		remaining = append(remaining, msg)
	}
	if len(remaining) == 0 {
		delete(n.queuedExecVotes, key)
	} else {
		n.queuedExecVotes[key] = remaining
	}
	n.execResultsMu.Unlock()

	// `msg` tracks the current values while iterating.
	for _, msg := range replay {
		n.processExecutionResultMsg(msg, true)
	}
	n.tryFinalizeFromStoredResults()
}

// tryFinalizeProposalIfQuorum implements the try finalize proposal if quorum helper.
func (n *Node) tryFinalizeProposalIfQuorum(block Block, reason string) bool {
	if n == nil || block.ID == 0 || block.ID != n.currentEpoch() {
		return false
	}
	if n.consensusRecomputePauseActive() {
		return false
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	// `total` stores the measured quantity used by this operation.
	total := len(validators)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(block.ID)
	if required == 0 {
		required = execQuorumRequired(total)
	}
	if total == 0 || required == 0 {
		return false
	}
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		return false
	}
	// `txMerkle` stores the transaction data handled by this operation.
	txMerkle := block.MempoolRoot
	// `proposalKey` stores the key used to access the related value.
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, execHash)
	if proposalKey == "" {
		return false
	}
	// `count` stores the measured quantity used by this operation.
	count := getExecCountGlobal(block.ID, proposalKey, execHash, txMerkle)
	if count < required {
		return false
	}
	// `results` and `ok` store whether the related condition is satisfied.
	results, _, _, ok := getExecResultsGlobal(block.ID, proposalKey, execHash, txMerkle)
	if !ok {
		return false
	}
	if strings.TrimSpace(reason) == "" {
		reason = "proposal_existing_quorum"
	}
	_ = n.maybeAdoptProposalOnExecutionVote(block)
	_ = results
	return n.broadcastCommitVoteForProposal(block, execHash, txMerkle)
}

// tryFinalizeFromStoredResults implements the try finalize from stored results helper.
func (n *Node) tryFinalizeFromStoredResults() {
	if n.consensusRecomputePauseActive() {
		return
	}
	// `epoch` stores the value produced by this operation.
	epoch := n.currentEpoch()
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	// `total` stores the measured quantity used by this operation.
	total := len(validators)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(epoch)
	if required == 0 {
		required = execQuorumRequired(total)
	}
	if total == 0 || required == 0 {
		return
	}
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range n.candidateProposalBlocksForEpoch(epoch) {
		if block.ID != epoch {
			continue
		}
		if n.tryFinalizeProposalIfQuorum(block, "stored_results_quorum") {
			return
		}
	}
}

func (n *Node) recoverSignedCommitQuorumAtCurrentHeight(reason string) bool {
	if n == nil || n.Blockchain == nil || n.consensusRecomputePauseActive() {
		return false
	}
	epoch := n.Blockchain.Height() + 1
	if epoch == 0 || epoch != n.currentEpoch() {
		return false
	}
	choice := n.localSignedCommitChoiceSnapshot(epoch)
	if !choice.Quorum || strings.TrimSpace(choice.ProposalHash) == "" {
		choice = n.signedCommitQuorumChoiceSnapshot(epoch)
		if !choice.Quorum || strings.TrimSpace(choice.ProposalHash) == "" {
			return false
		}
	}
	block, ok := n.proposalBlockByHash(epoch, choice.ProposalHash)
	if !ok || block.ID != epoch {
		return false
	}
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		return false
	}
	txMerkle := strings.TrimSpace(block.MempoolRoot)
	_, _, commitVotes, required := n.commitVoteEvidenceForResult(epoch, block.BlockHash, execHash, txMerkle)
	if required == 0 {
		required = n.executionQuorumRequiredForEpoch(epoch)
	}
	if required == 0 || commitVotes < required {
		return false
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, execHash)
	results, signers, executionVotes, executionOK := getExecResultsGlobal(epoch, proposalKey, execHash, txMerkle)
	if !executionOK || executionVotes < required {
		results, signers, executionVotes, executionOK = getExecResultsForBlockHashGlobal(epoch, block.BlockHash, execHash, txMerkle)
	}
	if !executionOK || executionVotes < required {
		results, signers, executionVotes, executionOK = n.consensusExecutionResultsForBlock(block, execHash, txMerkle)
	}
	if !executionOK || executionVotes < required {
		var commitWitnesses []ValidatorSignature
		_, commitWitnesses, _, _ = n.commitVoteEvidenceForResult(epoch, block.BlockHash, execHash, txMerkle)
		results, signers, executionVotes, executionOK = executionResultsFromCommitWitnesses(block, execHash, txMerkle, commitWitnesses)
	}
	if !executionOK || executionVotes < required {
		if DebugConsensus {
			log.Printf("[SIGNED-COMMIT-RECOVER-DEFER] height=%d reason=execution_votes_missing block=%s exec=%s votes=%d required=%d trigger=%s",
				epoch,
				ShortHash(block.BlockHash),
				ShortHash(execHash),
				executionVotes,
				required,
				strings.TrimSpace(reason),
			)
		}
		return false
	}
	n.execResultsMu.Lock()
	_ = n.setQuorumLockedProposalLocked(block, "signed_commit_recovery", commitVotes, required)
	n.execResultsMu.Unlock()
	freezeExecPool(epoch, proposalKey, execHash)
	if n.finalizeExecutionResult(epoch, execHash, txMerkle, results, signers) {
		log.Printf("[SIGNED-COMMIT-RECOVER] height=%d reason=%s block=%s exec=%s commit_votes=%d execution_votes=%d required=%d",
			epoch,
			strings.TrimSpace(reason),
			ShortHash(block.BlockHash),
			ShortHash(execHash),
			commitVotes,
			executionVotes,
			required,
		)
		return true
	}
	if DebugConsensus {
		log.Printf("[SIGNED-COMMIT-RECOVER-DEFER] height=%d reason=finalize_deferred block=%s exec=%s commit_votes=%d execution_votes=%d required=%d trigger=%s",
			epoch,
			ShortHash(block.BlockHash),
			ShortHash(execHash),
			commitVotes,
			executionVotes,
			required,
			strings.TrimSpace(reason),
		)
	}
	return false
}

// hasFinalExecutionResult implements the has final execution result helper.
func (n *Node) hasFinalExecutionResult(epoch uint64, execHash string, txMerkle string) bool {
	if execHash == "" {
		return false
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	// `total` stores the measured quantity used by this operation.
	total := len(validators)
	// `required` stores the request data being processed.
	required := n.executionQuorumRequiredForEpoch(epoch)
	if required == 0 {
		required = execQuorumRequired(total)
	}
	if total == 0 || required == 0 {
		return false
	}
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range n.candidateProposalBlocksForEpoch(epoch) {
		if block.ID != epoch {
			continue
		}
		// `stateRoot` stores the digest used to identify or verify the related data.
		stateRoot := block.StateRoot
		if stateRoot == "" {
			stateRoot = execHash
		}
		// `proposalKey` stores the key used to access the related value.
		proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, stateRoot)
		if getExecCountGlobal(epoch, proposalKey, execHash, txMerkle) >= required {
			return true
		}
	}
	return false
}

// proposalBlockByHash implements the proposal block by hash helper.
func (n *Node) proposalBlockByHash(height uint64, proposalHash string) (Block, bool) {
	if n == nil || height == 0 || strings.TrimSpace(proposalHash) == "" {
		return Block{}, false
	}
	proposalHash = strings.TrimSpace(proposalHash)
	// `block` tracks the synchronization state protecting shared data.
	for _, block := range n.candidateProposalBlocksForEpoch(height) {
		if block.ID == height && strings.EqualFold(strings.TrimSpace(block.BlockHash), proposalHash) {
			return block, true
		}
	}
	return Block{}, false
}

// handleCommitMsg handles commit msg.
func (n *Node) handleCommitMsg(cm CommitMsg) {
	if n == nil || cm.Height == 0 || n.isCommittedReplayHeight(cm.Height) {
		return
	}
	cm.From = normalizeValidatorID(cm.From)
	cm.Hash = strings.TrimSpace(cm.Hash)
	cm.ExecHash = strings.TrimSpace(cm.ExecHash)
	cm.TxMerkle = strings.TrimSpace(cm.TxMerkle)
	cm.ExecutionCommitmentHash = strings.TrimSpace(cm.ExecutionCommitmentHash)
	cm.Signature = strings.TrimSpace(cm.Signature)
	if cm.From == "" || cm.Hash == "" || cm.ExecHash == "" || cm.Signature == "" {
		return
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.proposalBlockByHash(cm.Height, cm.Hash)
	if !ok && cm.Block.ID == cm.Height && strings.EqualFold(strings.TrimSpace(cm.Block.BlockHash), cm.Hash) {
		if n.verifyLeaderBlock(cm.Block, "") {
			n.noteObservedProposal(cm.Block)
			block = cm.Block
			ok = true
		}
	}
	if !ok {
		return
	}
	// `expected` stores the value produced by this operation.
	expected := strings.TrimSpace(block.StateRoot)
	if expected == "" {
		expected = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if expected == "" || !strings.EqualFold(expected, cm.ExecHash) || strings.TrimSpace(block.MempoolRoot) != cm.TxMerkle {
		return
	}
	if err := verifyCommitVoteExecutionCommitment(block, cm.ExecutionCommitmentHash); err != nil {
		return
	}
	// `count`, `required`, and `accepted` store the measured quantity used by this operation.
	count, required, accepted := n.recordVerifiedCommitVote(cm)
	if !accepted {
		return
	}
	log.Printf("[COMMIT-VOTE] signer=%s height=%d block=%s votes=%d required=%d",
		ShortID(cm.From),
		cm.Height,
		ShortHash(cm.Hash),
		count,
		required,
	)
	if n.shouldFollowCommitEvidence(cm.Height, cm.Hash, count, required) {
		log.Printf("[COMMIT-FOLLOW] validator=%s height=%d block=%s votes=%d required=%d action=broadcast_commit_vote",
			ShortID(n.ID),
			cm.Height,
			ShortHash(cm.Hash),
			count,
			required,
		)
		n.maybeBroadcastExecutionVoteForBlock(block, "commit_quorum_minus_one")
		if !n.broadcastCommitVoteForProposal(block, expected, block.MempoolRoot) {
			n.maybeRebroadcastExecutionVoteForBlock(block, "commit_quorum_minus_one")
		}
	}
	if required == 0 || count < required || cm.Height != n.currentEpoch() {
		return
	}
	n.execResultsMu.Lock()
	_ = n.setQuorumLockedProposalLocked(block, "signed_commit_quorum", count, required)
	n.execResultsMu.Unlock()
	// `proposalKey` stores the key used to access the related value.
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, expected)
	// `results`, `signers`, `executionVotes`, and `executionOK` store whether the related condition is satisfied.
	results, signers, executionVotes, executionOK := getExecResultsGlobal(block.ID, proposalKey, expected, block.MempoolRoot)
	if !executionOK || executionVotes < required {
		results, signers, executionVotes, executionOK = getExecResultsForBlockHashGlobal(block.ID, block.BlockHash, expected, block.MempoolRoot)
		if !executionOK || executionVotes < required {
			results, signers, executionVotes, executionOK = n.consensusExecutionResultsForBlock(block, expected, block.MempoolRoot)
			if !executionOK || executionVotes < required {
				if n.shouldLogLivenessReason(fmt.Sprintf("commit_quorum_exec_cache_shortfall:%d:%s", block.ID, block.BlockHash), 5*time.Second) {
					log.Printf("[EXEC-COMMIT-DEFER] height=%d reason=execution_votes_missing_for_commit block=%s exec=%s commit_votes=%d required=%d exec_votes=%d",
						block.ID,
						ShortHash(block.BlockHash),
						ShortHash(expected),
						count,
						required,
						executionVotes,
					)
				}
				var commitWitnesses []ValidatorSignature
				_, commitWitnesses, _, _ = n.commitVoteEvidenceForResult(block.ID, block.BlockHash, expected, block.MempoolRoot)
				results, signers, executionVotes, executionOK = executionResultsFromCommitWitnesses(block, expected, block.MempoolRoot, commitWitnesses)
				if !executionOK || executionVotes < required {
					return
				}
			}
		}
	}
	freezeExecPool(block.ID, proposalKey, expected)
	_ = n.finalizeExecutionResult(block.ID, expected, block.MempoolRoot, results, signers)
}

// BroadcastBlock implements the broadcast block helper.
func (n *Node) BroadcastBlock(block Block) {
	if ResultGossipOnly {
		return
	}
	if n.BlockTopic == nil && n.TopicBlocks == nil {
		fmt.Println("Block topic not initialized")
		return
	}

	// `msg` stores the value produced by this operation.
	msg := Message{Type: MsgBlock, Data: MustJSON(block)}
	// `data` and `err` store the error produced by this operation.
	data, err := MarshalP2PMessage(msg)
	if err != nil {
		data, _ = json.Marshal(block)
	}

	if n.BlockTopic != nil {
		// `err` stores the error produced by this operation.
		if err := n.BlockTopic.Publish(context.Background(), data); err != nil {
			fmt.Println("Block publish failed:", err)
			return
		}
	} else if n.TopicBlocks != nil {
		// `err` stores the error produced by this operation.
		if err := n.TopicBlocks.Publish(context.Background(), data); err != nil {
			fmt.Println("Block publish failed:", err)
			return
		}
	}

	if DebugConsensus {
		fmt.Printf("Block published | height=%d\n", block.ID)
	}
}

// DiscoverPeers implements the discover peers helper.
func (n *Node) DiscoverPeers() {
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()
	// `seeds` stores the value produced by this operation.
	seeds := n.seedsSnapshot()

	// `seed` tracks the current values while iterating.
	for _, seed := range seeds {
		// `seedAddr` and `err` store the error produced by this operation.
		seedAddr, err := ma.NewMultiaddr(seed)
		if err != nil {
			continue
		}

		// `seedInfo` and `err` store the error produced by this operation.
		seedInfo, err := peer.AddrInfoFromP2pAddr(seedAddr)
		if err != nil {
			continue
		}
		if n.Host != nil && seedInfo.ID == n.Host.ID() {
			continue
		}

		if !n.canDialPeerID(seedInfo.ID.String()) {
			continue
		}
		// `err` stores the error produced by this operation.
		if err := n.Host.Connect(ctx, *seedInfo); err == nil {
			n.Host.ConnManager().TagPeer(seedInfo.ID, "bootstrap", 100)

			if DebugNet {
				fmt.Println("Ã°Å¸â€â€” Connected to seed:", seedInfo.ID.String())
			}
			n.recordDialSuccess(seedInfo.ID.String())
		} else {
			// `errLower` stores the error produced by this operation.
			errLower := strings.ToLower(err.Error())
			if strings.Contains(errLower, "peer id mismatch") ||
				strings.Contains(errLower, "dial to self attempted") {
				if n.refreshPeerIDMismatch(seed, seedInfo.ID.String(), err) {
					continue
				}
				n.forgetPeer(seedInfo.ID.String(), "peer_id_mismatch")
			}
			n.recordDialFailure(seedInfo.ID.String())
		}
	}
}

// connectPubSubPeers implements the connect pub sub peers helper.
func (n *Node) connectPubSubPeers() {
	// =====================================================
	// HARD GUARD - require host + pubsub
	// =====================================================
	if n.Host == nil || n.PubSub == nil {
		if DebugNet {
			fmt.Println("Ã¢Å¡Â Ã¯Â¸Â PubSub handshake skipped: host or pubsub nil")
		}
		return
	}
	// =====================================================
	// 1Ã¯Â¸ÂÃ¢Æ’Â£ CANONICAL TOPICS (AUTHORITATIVE SET)
	canonicalTopics := []string{
		TopicBlock,
		TopicTx,
		TopicConsensus,
		TopicValidator,
	}
	// 2Ã¯Â¸ÂÃ¢Æ’Â£ ENSURE ALL TOPICS ARE JOINED (ONCE)
	// =====================================================
	for _, topic := range canonicalTopics {
		if n.isTopicJoined(topic) {
			continue
		}
		// `err` stores the error produced by this operation.
		if _, err := n.PubSub.Join(topic); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to join topic %s: %v\n", topic, err)
			}
		}
	}
	// =====================================================
	// 3Ã¯Â¸ÂÃ¢Æ’Â£ FETCH CONNECTED LIBP2P PEERS
	// =====================================================
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		if DebugNet {
			fmt.Println("Ã¢Å¡Â Ã¯Â¸Â No connected peers Ã¢â‚¬â€ PubSub mesh idle")
		}
		return
	}
	if DebugNet && n.shouldLogNetworkProbe(fmt.Sprintf("pubsub_mesh:%d", len(peers)), 20*time.Second) {
		fmt.Printf("Ã°Å¸â€â€” PubSub mesh check against %d peers\n", len(peers))
	}
	// =====================================================
	// 4Ã¯Â¸ÂÃ¢Æ’Â£ BUILD FAST LOOKUP OF MESH PEERS (O(1))
	// =====================================================
	meshPeers := make(map[string]bool)
	// `topic` tracks the current values while iterating.
	for _, topic := range canonicalTopics {
		// `pid` tracks the current values while iterating.
		for _, pid := range n.PubSub.ListPeers(topic) {
			meshPeers[pid.String()] = true
		}
	}
	// =====================================================
	// 5Ã¯Â¸ÂÃ¢Æ’Â£ FORCE GOSSIPSUB GRAFT FOR MISSING PEERS
	// =====================================================
	for _, pid := range peers {
		if meshPeers[pid.String()] {
			continue
		}
		if n.peerConnectedFor(pid.String()) < peerMinHold {
			continue
		}
		if !n.shouldForceGraft(pid.String()) {
			continue
		}
		if DebugNet {
			fmt.Printf(
				"Ã°Å¸â€â€ž Forcing GossipSub graft with peer %s\n",
				pid.String()[:8],
			)
		}
		// Ã°Å¸â€Â Controlled publish to trigger IWANT/IHAVE exchange
		// Prefer consensus gossip when ResultGossipOnly is enabled.
		publishTopic := n.BlockTopic
		if publishTopic == nil {
			publishTopic = n.TopicBlocks
		}
		if ResultGossipOnly && n.ConsensusTopic != nil {
			publishTopic = n.ConsensusTopic
		}
		if publishTopic != nil {
			_ = publishTopic.Publish(
				context.Background(),
				[]byte{0x01}, // minimal payload, safe
			)
		}
		// Ã°Å¸Â§Ëœ Throttle to protect mesh stability
		time.Sleep(100 * time.Millisecond)
	}
}

// validatorSetHash implements the validator set hash helper.
func (n *Node) validatorSetHash() string {
	// `nextHeight` stores the value produced by this operation.
	nextHeight := n.Blockchain.Height() + 1
	if validatorOnboardingStrictActivationEnabled() {
		// `active` stores the value produced by this operation.
		if active, _ := n.selfActiveValidatorAt(nextHeight); !active {
			return n.stableFrozenHashForAdvertise(nextHeight)
		}
	}
	// `hash` stores the digest used to identify or verify the related data.
	if hash, _ := n.runtimeAdvertisedValidatorSetHash(nextHeight); hash != "" {
		return hash
	}
	return n.stableFrozenHashForAdvertise(nextHeight)
}

// peerHelloAdvertiseIdentity implements the peer hello advertise identity helper.
func (n *Node) peerHelloAdvertiseIdentity(height uint64) (string, string, string) {
	// `role` stores the value produced by this operation.
	role := normalizeNodeRole(n.Role)
	if role != "validator" {
		return role, "", ""
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	// `active` stores the value produced by this operation.
	active, _ := n.selfActiveValidatorAt(height)
	if validatorOnboardingStrictActivationEnabled() && !active {
		return "full", "", ""
	}
	if !active {
		return role, "", ""
	}
	// `pubHex` stores the value produced by this operation.
	pubHex := ""
	if len(n.ValidatorKey.PublicKey) == ed25519.PublicKeySize {
		pubHex = hex.EncodeToString(n.ValidatorKey.PublicKey)
	}
	if pubHex == "" {
		return role, "", ""
	}
	if !validatorOnboardingStrictActivationEnabled() && !isAutoGeneratedNodeID(n.ID) {
		if legacyID := normalizeValidatorID(n.ID); legacyID != "" {
			return "validator", legacyID, pubHex
		}
	}
	validatorID := n.localConsensusValidatorIDForHeight(height)
	if validatorID == "" {
		return role, "", ""
	}
	// Peer hello is an identity advertisement, not a participation vote.
	// Existing-chain validators must continue to identify themselves even while
	// startup/sync gates temporarily keep them out of vote/propose mode.
	return "validator", validatorID, pubHex
}

// outboundPeerHello implements the outbound peer hello helper.
func (n *Node) outboundPeerHello() PeerHello {
	// `role`, `validatorID`, and `validatorPubKey` store whether the related condition is satisfied.
	role, validatorID, validatorPubKey := n.peerHelloAdvertiseIdentity(n.currentEpoch())
	// `validatorSetHeight` stores whether the related condition is satisfied.
	validatorSetHeight := n.Blockchain.Height() + 1
	// `nextValidatorSetHash` stores the digest used to identify or verify the related data.
	nextValidatorSetHash := strings.TrimSpace(n.deterministicNextValidatorSetHash(validatorSetHeight, n.validatorSetHash()))
	// `tipHash` stores the digest used to identify or verify the related data.
	tipHash := ""
	if n.Blockchain != nil {
		tipHash = strings.TrimSpace(n.Blockchain.LastBlock().BlockHash)
	}
	// `hello` stores the value produced by this operation.
	nodeIdentity := n.NetworkID
	if strings.TrimSpace(nodeIdentity) == "" {
		nodeIdentity = n.ID
	}
	hello := PeerHello{
		ChainID:              protocolChainID(),
		GenesisHash:          GenesisHash,
		Version:              Version,
		ConsensusHash:        consensusParamsHash(),
		Role:                 role,
		NodeID:               normalizeNodeIdentityID(nodeIdentity),
		ValidatorID:          validatorID,
		ValidatorPubKey:      validatorPubKey,
		P2PAddr:              n.SelfAddr,
		ValidatorSetHash:     n.validatorSetHash(),
		ValidatorSetHeight:   validatorSetHeight,
		NextValidatorSetHash: nextValidatorSetHash,
		ActivationHeight:     validatorSetHeight,
		Height:               n.Blockchain.Height(),
		TipHash:              tipHash,
	}
	n.signPeerHello(&hello)
	return hello
}

// outboundPeerHelloPreValidation implements the outbound peer hello pre validation helper.
func (n *Node) outboundPeerHelloPreValidation() PeerHello {
	// `hello` stores the value produced by this operation.
	hello := PeerHello{
		ChainID:       protocolChainID(),
		GenesisHash:   GenesisHash,
		Version:       Version,
		ConsensusHash: consensusParamsHash(),
	}
	n.signPeerHello(&hello)
	return hello
}

// peerHelloHasPostValidationFields implements the peer hello has post validation fields helper.
func peerHelloHasPostValidationFields(hello PeerHello) bool {
	return strings.TrimSpace(hello.Role) != "" ||
		strings.TrimSpace(hello.NodeID) != "" ||
		strings.TrimSpace(hello.ValidatorID) != "" ||
		strings.TrimSpace(hello.ValidatorPubKey) != "" ||
		strings.TrimSpace(hello.P2PAddr) != "" ||
		strings.TrimSpace(hello.ValidatorSetHash) != "" ||
		hello.ValidatorSetHeight != 0 ||
		strings.TrimSpace(hello.NextValidatorSetHash) != "" ||
		hello.ActivationHeight != 0 ||
		hello.Height != 0 ||
		strings.TrimSpace(hello.TipHash) != ""
}

// setGossipQuiet implements the set gossip quiet helper.
func (n *Node) setGossipQuiet(quiet bool) {
	n.peerStateMu.Lock()
	n.gossipQuiet = quiet
	n.peerStateMu.Unlock()
}

// isGossipQuiet implements the is gossip quiet helper.
func (n *Node) isGossipQuiet() bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.gossipQuiet
}

// setPeerConnected implements the set peer connected helper.
func (n *Node) setPeerConnected(peerID string, connected bool) {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if connected {
		n.connectedPeers[peerID] = true
		delete(n.connectingPeers, peerID)
	} else {
		delete(n.connectedPeers, peerID)
		delete(n.connectingPeers, peerID)
	}
}

// isPeerConnected implements the is peer connected helper.
func (n *Node) isPeerConnected(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.connectedPeers[peerID]
}

// setPeerConnectedAt implements the set peer connected at helper.
func (n *Node) setPeerConnectedAt(peerID string, t time.Time) {
	n.peerStateMu.Lock()
	n.peerConnectedAt[peerID] = t
	n.peerStateMu.Unlock()
}

// clearPeerConnectedAt implements the clear peer connected at helper.
func (n *Node) clearPeerConnectedAt(peerID string) {
	n.peerStateMu.Lock()
	delete(n.peerConnectedAt, peerID)
	n.peerStateMu.Unlock()
}

// peerConnectedFor implements the peer connected for helper.
func (n *Node) peerConnectedFor(peerID string) time.Duration {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `t` and `ok` store whether the related condition is satisfied.
	if t, ok := n.peerConnectedAt[peerID]; ok {
		return time.Since(t)
	}
	return 0
}

// hasActivePeerConnection implements the has active peer connection helper.
func (n *Node) hasActivePeerConnection(pid peer.ID) bool {
	if n == nil || n.Host == nil || pid == "" {
		return false
	}
	if n.Host.Network().Connectedness(pid) == network.Connected {
		return true
	}
	return len(n.Host.Network().ConnsToPeer(pid)) > 0
}

// shouldForceGraft implements the should force graft helper.
func (n *Node) shouldForceGraft(peerID string) bool {
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `t` and `ok` store whether the related condition is satisfied.
	if t, ok := n.peerGraftAt[peerID]; ok {
		if now.Sub(t) < peerGraftCooldown {
			return false
		}
	}
	n.peerGraftAt[peerID] = now
	return true
}

// markPeerConnecting implements the mark peer connecting helper.
func (n *Node) markPeerConnecting(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.connectingPeers[peerID] {
		return false
	}
	n.connectingPeers[peerID] = true
	return true
}

// dialBackoffDelay implements the dial backoff delay helper.
func dialBackoffDelay(failures int) time.Duration {
	switch failures {
	case 1:
		return dialBackoffStep1
	case 2:
		return dialBackoffStep2
	case 3:
		return dialBackoffStep3
	default:
		return dialBackoffMax
	}
}

// validatorDialBackoffDelay implements the validator dial backoff delay helper.
func validatorDialBackoffDelay(failures int) time.Duration {
	switch failures {
	case 1:
		return validatorDialBackoffStep1
	case 2:
		return validatorDialBackoffStep2
	case 3:
		return validatorDialBackoffStep3
	default:
		return validatorDialBackoffMax
	}
}

// peerIDInPeerAddrs implements the peer id in peer addrs helper.
func peerIDInPeerAddrs(peerID string, addrs []string) bool {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return false
	}
	// `addr` tracks the address used by this operation.
	for _, addr := range addrs {
		// `parsed` and `ok` store whether the related condition is satisfied.
		_, parsed, ok := splitPeerAddress(addr)
		if ok && strings.TrimSpace(parsed) == peerID {
			return true
		}
	}
	return false
}

// isValidatorOrPersistentPeerID implements the is validator or persistent peer id helper.
func (n *Node) isValidatorOrPersistentPeerID(peerID string) bool {
	if n == nil || strings.TrimSpace(peerID) == "" {
		return false
	}
	// `targets` stores the value produced by this operation.
	targets := n.persistentPeersSnapshot()
	if normalizeNodeRole(n.Role) == "validator" {
		targets = mergePeerLists(targets, n.validatorMeshTargets())
	}
	if peerIDInPeerAddrs(peerID, targets) {
		return true
	}
	n.peerStateMu.Lock()
	// `allowed` stores whether the peer was provided by the operator/runtime
	// bootstrap path. Those peers need the same soft-failure handling as
	// persistent peers even when config.toml was intentionally bypassed by
	// --peers or MSC_P2P_PEERS.
	allowed := n.allowedPeerIDs[peerID]
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := strings.TrimSpace(n.peerToValidator[peerID])
	n.peerStateMu.Unlock()
	return allowed || validatorID != ""
}

// isLocalhostPeerAddr implements the is localhost peer addr helper.
func isLocalhostPeerAddr(addr string) bool {
	// `base` stores the value produced by this operation.
	base := strings.ToLower(strings.TrimSpace(stripP2PComponent(addr)))
	if base == "" {
		return false
	}
	if strings.Contains(base, "/ip4/127.0.0.1/") || strings.HasPrefix(base, "127.0.0.1:") {
		return true
	}
	if strings.Contains(base, "/ip6/::1/") || strings.HasPrefix(base, "[::1]") || strings.HasPrefix(base, "::1") {
		return true
	}
	if strings.Contains(base, "localhost") {
		return true
	}
	return false
}

// isDialRefusedError implements the is dial refused error helper.
func isDialRefusedError(err error) bool {
	if err == nil {
		return false
	}
	// `msg` stores the value produced by this operation.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused")
}

// dialFailureCount implements the dial failure count helper.
func (n *Node) dialFailureCount(peerID string) int {
	if n == nil || peerID == "" {
		return 0
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.peerDialFailures[peerID]
}

// shouldPruneLocalhostDialRefused implements the should prune localhost dial refused helper.
func shouldPruneLocalhostDialRefused(n *Node, peerID, addr string, err error) bool {
	if !PruneLocalhostOnRefused {
		return false
	}
	if PruneLocalhostRefusedFailures <= 0 {
		return false
	}
	if peerID == "" || n == nil {
		return false
	}
	if !isLocalhostPeerAddr(addr) {
		return false
	}
	if !isDialRefusedError(err) {
		return false
	}
	return n.dialFailureCount(peerID) >= PruneLocalhostRefusedFailures
}

// shouldPruneStaleStaticDialFailure drops stale static/public /p2p/ IDs after
// repeated hard dial failures. The address can be rediscovered from validator
// hello, peers-list gossip, or registry seeds when it becomes healthy again.
func shouldPruneStaleStaticDialFailure(n *Node, peerID, addr string, err error) bool {
	if n == nil || strings.TrimSpace(peerID) == "" || strings.TrimSpace(addr) == "" || err == nil {
		return false
	}
	if !n.isValidatorOrPersistentPeerID(peerID) {
		return false
	}
	if n.isPeerConnected(peerID) {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "context canceled") || strings.Contains(errMsg, "shutdown") {
		return false
	}
	hardDialFailure := strings.Contains(errMsg, "all dials failed") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "actively refused") ||
		strings.Contains(errMsg, "no route") ||
		strings.Contains(errMsg, "i/o timeout") ||
		strings.Contains(errMsg, "deadline exceeded")
	if !hardDialFailure {
		return false
	}
	threshold := PruneLocalhostRefusedFailures * 2
	if threshold < 6 {
		threshold = 6
	}
	return n.dialFailureCount(peerID) >= threshold
}

// `staleStaticPeerPruneCooldown` defines the value currently being processed.
const staleStaticPeerPruneCooldown = 10 * time.Minute

// staleStaticPeerKey implements the stale static peer key helper.
func staleStaticPeerKey(peerID string) string {
	return "stale-static-peer:" + strings.TrimSpace(peerID)
}

// markStaleStaticPeerSuppressed implements the mark stale static peer suppressed helper.
func (n *Node) markStaleStaticPeerSuppressed(peerID string) {
	if n == nil || strings.TrimSpace(peerID) == "" {
		return
	}
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	n.noBlockLogAt[staleStaticPeerKey(peerID)] = time.Now()
}

// staleStaticPeerSuppressed implements the stale static peer suppressed helper.
func (n *Node) staleStaticPeerSuppressed(peerID string) bool {
	if n == nil || strings.TrimSpace(peerID) == "" {
		return false
	}
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		return false
	}
	key := staleStaticPeerKey(peerID)
	last, ok := n.noBlockLogAt[key]
	if !ok || last.IsZero() {
		return false
	}
	if time.Since(last) > staleStaticPeerPruneCooldown {
		delete(n.noBlockLogAt, key)
		return false
	}
	return true
}

// canDialPeerID implements the can dial peer id helper.
func (n *Node) canDialPeerID(peerID string) bool {
	if peerID == "" {
		return true
	}
	if n.staleStaticPeerSuppressed(peerID) {
		return false
	}
	if !n.peerAdmissionAllowed(peerID) {
		return false
	}
	if n.isPeerQuarantined(peerID) {
		return false
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `next` and `ok` store whether the related condition is satisfied.
	next, ok := n.peerDialNext[peerID]
	if !ok || next.IsZero() {
		return true
	}
	return time.Now().After(next)
}

// recordDialFailure implements the record dial failure helper.
func (n *Node) recordDialFailure(peerID string) {
	if peerID == "" {
		return
	}
	// `backoff` stores the value produced by this operation.
	backoff := dialBackoffDelay
	if n.isValidatorOrPersistentPeerID(peerID) {
		backoff = validatorDialBackoffDelay
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `failures` stores the result produced by this operation.
	failures := n.peerDialFailures[peerID] + 1
	n.peerDialFailures[peerID] = failures
	n.peerDialNext[peerID] = time.Now().Add(backoff(failures))
	n.notePeerDialScore(peerID, false)
}

// recordDialSuccess implements the record dial success helper.
func (n *Node) recordDialSuccess(peerID string) {
	if peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerDialFailures, peerID)
	delete(n.peerDialNext, peerID)
	delete(n.quarantineUntil, peerID)
	n.peerStateMu.Unlock()
	n.notePeerDialScore(peerID, true)
}

// clearDialBackoffForPeerID implements the clear dial backoff for peer id helper.
func (n *Node) clearDialBackoffForPeerID(peerID string) {
	if n == nil || strings.TrimSpace(peerID) == "" {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerDialNext, peerID)
	delete(n.quarantineUntil, peerID)
	delete(n.connectingPeers, peerID)
	n.peerStateMu.Unlock()
}

// clearDialBackoffForPeerAddrs implements the clear dial backoff for peer addrs helper.
func (n *Node) clearDialBackoffForPeerAddrs(peerAddrs []string) {
	if n == nil || len(peerAddrs) == 0 {
		return
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(peerAddrs))
	// `addr` tracks the address used by this operation.
	for _, addr := range peerAddrs {
		// `peerID` and `ok` store whether the related condition is satisfied.
		_, peerID, ok := splitPeerAddress(addr)
		if !ok || strings.TrimSpace(peerID) == "" {
			continue
		}
		// `done` stores the value produced by this operation.
		if _, done := seen[peerID]; done {
			continue
		}
		seen[peerID] = struct{}{}
		n.clearDialBackoffForPeerID(peerID)
	}
}

// protectValidatorMeshPeerID implements the protect validator mesh peer id helper.
func (n *Node) protectValidatorMeshPeerID(peerID string) {
	if n == nil || n.Host == nil {
		return
	}
	peerID = strings.TrimSpace(peerID)
	if peerID == "" || !n.isValidatorOrPersistentPeerID(peerID) {
		return
	}
	// `pid` and `err` store the error produced by this operation.
	pid, err := peer.Decode(peerID)
	if err != nil {
		return
	}
	// `cm` stores the value produced by this operation.
	if cm := n.Host.ConnManager(); cm != nil {
		cm.TagPeer(pid, "validator-mesh", 1000)
		cm.Protect(pid, "validator-mesh")
	}
}

// unprotectValidatorMeshPeerID implements the unprotect validator mesh peer id helper.
func (n *Node) unprotectValidatorMeshPeerID(peerID string) {
	if n == nil || n.Host == nil {
		return
	}
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return
	}
	// `pid` and `err` store the error produced by this operation.
	pid, err := peer.Decode(peerID)
	if err != nil {
		return
	}
	// `cm` stores the value produced by this operation.
	if cm := n.Host.ConnManager(); cm != nil {
		cm.Unprotect(pid, "validator-mesh")
	}
}

// decayDialFailures implements the decay dial failures helper.
func (n *Node) decayDialFailures(now time.Time) {
	if n == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	n.peerStateMu.Lock()
	// `peerID` and `next` track the current values while iterating.
	for peerID, next := range n.peerDialNext {
		if next.IsZero() || now.After(next.Add(dialBackoffMax)) {
			delete(n.peerDialNext, peerID)
			delete(n.peerDialFailures, peerID)
		}
	}
	n.peerStateMu.Unlock()
}

// quarantineDurationFor implements the quarantine duration for helper.
func quarantineDurationFor(reason string) time.Duration {
	switch {
	case strings.Contains(reason, "peer_flap"):
		return peerQuarantineForFlap
	case strings.Contains(reason, "peerinfo_protocol_mismatch"):
		// Peer-info streams can fail transiently during peer startup/restart.
		// Keep quarantine short so healthy peers recover quickly.
		return peerQuarantineForPeerInfoStream
	case strings.Contains(reason, "chain_id_mismatch"),
		strings.Contains(reason, "genesis_hash_mismatch"),
		strings.Contains(reason, "version_mismatch"),
		strings.Contains(reason, "consensus_params_mismatch"),
		strings.Contains(reason, "consensus_protocol_mismatch"):
		return peerQuarantineForMismatch
	case strings.Contains(reason, "protocol"):
		return peerQuarantineForProtocol
	default:
		return peerQuarantineFor
	}
}

// trustedPeerCanBypassQuarantine implements the trusted peer can bypass quarantine helper.
func trustedPeerCanBypassQuarantine(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" || shouldForgetPeer(reason) {
		return false
	}
	switch {
	case strings.Contains(reason, "duplicate"),
		strings.Contains(reason, "peer_id_mismatch"),
		strings.Contains(reason, "bad_signature"),
		strings.Contains(reason, "replay"),
		strings.Contains(reason, "spoof"),
		strings.Contains(reason, "identity"),
		strings.Contains(reason, "drift_same_height"),
		strings.Contains(reason, "dangerous"):
		return false
	default:
		return true
	}
}

// shouldForgetPeer implements the should forget peer helper.
func shouldForgetPeer(reason string) bool {
	switch {
	case strings.Contains(reason, "chain_id_mismatch"),
		strings.Contains(reason, "genesis_hash_mismatch"),
		strings.Contains(reason, "version_mismatch"),
		strings.Contains(reason, "consensus_params_mismatch"),
		strings.Contains(reason, "consensus_protocol_mismatch"):
		return true
	default:
		return false
	}
}

// clearPeerState implements the clear peer state helper.
func (n *Node) clearPeerState(peerID string) {
	if peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerDialFailures, peerID)
	delete(n.peerDialNext, peerID)
	delete(n.peerSubnet, peerID)
	delete(n.peerASN, peerID)
	delete(n.peerOutbound, peerID)
	delete(n.peerHelloOK, peerID)
	delete(n.peerSuspectAt, peerID)
	delete(n.peerHashMatch, peerID)
	// `nodeID` and `mappedPeerID` track the current values while iterating.
	for nodeID, mappedPeerID := range n.nodeIDToPeer {
		if mappedPeerID == peerID {
			delete(n.nodeIDToPeer, nodeID)
		}
	}
	delete(n.peerToValidator, peerID)
	// `validatorID` and `mappedPeerID` track whether the related condition is satisfied.
	for validatorID, mappedPeerID := range n.validatorToPeer {
		if mappedPeerID == peerID {
			delete(n.validatorToPeer, validatorID)
		}
	}
	delete(n.peerRole, peerID)
	delete(n.peerSetHash, peerID)
	delete(n.peerTipHash, peerID)
	delete(n.peerAckHeight, peerID)
	delete(n.peerSyncOnlyUntil, peerID)
	delete(n.peerSyncOnlyClass, peerID)
	delete(n.peerFlapTimes, peerID)
	delete(n.quarantineUntil, peerID)
	delete(n.connectedPeers, peerID)
	delete(n.connectingPeers, peerID)
	delete(n.peerConnectedAt, peerID)
	delete(n.allowedPeerIDs, peerID)
	// `prefix` stores the value produced by this operation.
	prefix := peerID + "|"
	// `key` tracks the key used to access the related value.
	for key := range n.peerDriftState {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerDriftState, key)
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range n.peerSyncOnlyLastDropLog {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerSyncOnlyLastDropLog, key)
		}
	}
	n.peerStateMu.Unlock()
}

// remotePeerIDFromMismatchError implements the remote peer id from mismatch error helper.
func remotePeerIDFromMismatchError(err error) string {
	if err == nil {
		return ""
	}
	// `msg` stores the value produced by this operation.
	msg := err.Error()
	// `lower` stores the value produced by this operation.
	lower := strings.ToLower(msg)
	// `markers` stores the value produced by this operation.
	markers := []string{
		"remote key matches ",
		"but got ",
	}
	// `marker` tracks the current values while iterating.
	for _, marker := range markers {
		// `idx` stores the current position in the related collection.
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		// `tail` stores the value produced by this operation.
		tail := strings.TrimSpace(msg[idx+len(marker):])
		if tail == "" {
			continue
		}
		// `fields` stores the value produced by this operation.
		fields := strings.Fields(tail)
		if len(fields) == 0 {
			continue
		}
		// `candidate` stores the value produced by this operation.
		candidate := strings.Trim(fields[0], " ,.;:)]}\"'")
		if strings.HasPrefix(candidate, "12D3") {
			return candidate
		}
	}
	return ""
}

// peerAddrWithPeerID implements the peer addr with peer id helper.
func peerAddrWithPeerID(rawAddr, peerID string) (string, bool) {
	rawAddr = strings.TrimSpace(rawAddr)
	peerID = strings.TrimSpace(peerID)
	if rawAddr == "" || peerID == "" {
		return "", false
	}
	// `base` and `hasPID` store the value produced by this operation.
	base, _, hasPID := splitPeerAddress(rawAddr)
	if hasPID {
		return fmt.Sprintf("%s/p2p/%s", base, peerID), true
	}
	if strings.HasPrefix(rawAddr, "/") {
		// `err` stores the error produced by this operation.
		if _, err := ma.NewMultiaddr(rawAddr); err == nil {
			return fmt.Sprintf("%s/p2p/%s", rawAddr, peerID), true
		}
	}
	return "", false
}

// replacePeerAddrForBase implements the replace peer addr for base helper.
func replacePeerAddrForBase(list []string, fixedAddr string) []string {
	fixedAddr = strings.TrimSpace(fixedAddr)
	if fixedAddr == "" {
		return list
	}
	// `base` stores the value produced by this operation.
	base := stripP2PComponent(fixedAddr)
	if base == "" {
		return list
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(list)+1)
	// `addr` tracks the address used by this operation.
	for _, addr := range list {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if stripP2PComponent(addr) == base {
			continue
		}
		out = append(out, addr)
	}
	out = append(out, fixedAddr)
	return sanitizePeerListWithPreferred(out, []string{fixedAddr})
}

// resetPeerRetryState implements the reset peer retry state helper.
func (n *Node) resetPeerRetryState(peerID string) {
	if n == nil || peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerDialFailures, peerID)
	delete(n.peerDialNext, peerID)
	delete(n.quarantineUntil, peerID)
	delete(n.connectingPeers, peerID)
	n.peerStateMu.Unlock()
}

// refreshPeerIDMismatch implements the refresh peer id mismatch helper.
func (n *Node) refreshPeerIDMismatch(rawAddr, expectedPeerID string, dialErr error) bool {
	if n == nil || expectedPeerID == "" || rawAddr == "" || dialErr == nil {
		return false
	}
	// Auto-heal only for private/LAN targets to avoid trusting public mismatches.
	if !isPeerAddrPrivate(rawAddr) {
		return false
	}
	// `remotePeerID` stores the value produced by this operation.
	remotePeerID := remotePeerIDFromMismatchError(dialErr)
	if remotePeerID == "" || remotePeerID == expectedPeerID {
		return false
	}
	// `fixedAddr` and `ok` store whether the related condition is satisfied.
	fixedAddr, ok := peerAddrWithPeerID(rawAddr, remotePeerID)
	if !ok {
		return false
	}
	// Never keep stale self entries.
	if n.Host != nil && remotePeerID == n.Host.ID().String() {
		n.forgetPeer(expectedPeerID, "self_dial")
		return true
	}
	// `persistent` and `seeds` store the value produced by this operation.
	persistent, seeds := n.configPeerListsSnapshot()
	persistent = replacePeerAddrForBase(persistent, fixedAddr)
	seeds = replacePeerAddrForBase(seeds, fixedAddr)
	n.setConfigPeerLists(persistent, seeds)
	if PersistPeerIDRefresh {
		// `err` stores the error produced by this operation.
		if err := savePersistentPeers(n.DataDir, n.ID, persistent); err != nil && DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to persist peer-id refresh for %s: %v\n", expectedPeerID, err)
		}
	}
	// `base` and `hasBase` store the value produced by this operation.
	base, _, hasBase := splitPeerAddress(rawAddr)
	if hasBase {
		ValidatorAddrBook.mu.Lock()
		// `vid` and `addr` track the address used by this operation.
		for vid, addr := range ValidatorAddrBook.m {
			// `addrBase` and `has` store the address used by this operation.
			addrBase, _, has := splitPeerAddress(addr)
			if has && addrBase == base {
				ValidatorAddrBook.m[vid] = fixedAddr
			}
		}
		ValidatorAddrBook.mu.Unlock()
	}
	n.peerStateMu.Lock()
	delete(n.allowedPeerIDs, expectedPeerID)
	n.allowedPeerIDs[remotePeerID] = true
	n.peerStateMu.Unlock()
	n.resetPeerRetryState(remotePeerID)
	if DebugNet {
		fmt.Printf("[PEER-REFRESH] old=%s new=%s addr=%s\n", expectedPeerID, remotePeerID, stripP2PComponent(fixedAddr))
	}
	n.forgetPeer(expectedPeerID, "peer_id_mismatch")
	return true
}

// forgetPeer implements the forget peer helper.
func (n *Node) forgetPeer(peerID, reason string) {
	if peerID == "" {
		return
	}
	n.unprotectValidatorMeshPeerID(peerID)
	n.clearPeerState(peerID)
	if n.Host != nil {
		// `pid` and `err` store the error produced by this operation.
		if pid, err := peer.Decode(peerID); err == nil {
			n.Host.Peerstore().ClearAddrs(pid)
		}
	}
	ValidatorAddrBook.mu.Lock()
	// `vid` and `addr` track the address used by this operation.
	for vid, addr := range ValidatorAddrBook.m {
		if strings.Contains(addr, peerID) {
			delete(ValidatorAddrBook.m, vid)
		}
	}
	ValidatorAddrBook.mu.Unlock()
	// `persistent` and `seeds` store the value produced by this operation.
	persistent, seeds := n.configPeerListsSnapshot()
	// `filteredPersistent` stores the value produced by this operation.
	filteredPersistent := make([]string, 0, len(persistent))
	// `filteredSeeds` stores the value produced by this operation.
	filteredSeeds := make([]string, 0, len(seeds))
	// `persistentChanged` stores the value produced by this operation.
	persistentChanged := false
	// `seedsChanged` stores the value produced by this operation.
	seedsChanged := false
	// `addr` tracks the address used by this operation.
	for _, addr := range persistent {
		if strings.Contains(addr, peerID) {
			persistentChanged = true
			continue
		}
		filteredPersistent = append(filteredPersistent, addr)
	}
	// `addr` tracks the address used by this operation.
	for _, addr := range seeds {
		if strings.Contains(addr, peerID) {
			seedsChanged = true
			continue
		}
		filteredSeeds = append(filteredSeeds, addr)
	}
	if persistentChanged || seedsChanged {
		n.setConfigPeerLists(filteredPersistent, filteredSeeds)
		if persistentChanged && PersistPeerIDRefresh {
			// `err` stores the error produced by this operation.
			if err := savePersistentPeers(n.DataDir, n.ID, filteredPersistent); err != nil && DebugNet {
				fmt.Printf("?? Failed to persist peers list: %v\n", err)
			}
		}
	}
	if DebugNet {
		fmt.Printf("?? Peer forgotten: %s reason=%s\n", peerID, reason)
	}
}

// quarantinePeer implements the quarantine peer helper.
func (n *Node) quarantinePeer(peerID, reason string) {
	if peerID == "" {
		return
	}
	if n.isValidatorOrPersistentPeerID(peerID) && trustedPeerCanBypassQuarantine(reason) {
		// Validator mesh targets are safety-critical. A transport flap should
		// trigger bounded redial backoff, not isolate a validator long enough
		// to lose quorum. Hard identity/chain mismatches still quarantine.
		n.peerStateMu.Lock()
		delete(n.quarantineUntil, peerID)
		n.peerStateMu.Unlock()
		if DebugNet {
			fmt.Printf("Peer quarantine bypassed for validator/persistent peer: %s reason=%s\n", peerID, reason)
		}
		return
	}
	// `duration` stores the value produced by this operation.
	duration := quarantineDurationFor(reason)
	// `forget` stores the value produced by this operation.
	forget := shouldForgetPeer(reason)
	if forget {
		n.forgetPeer(peerID, reason)
	}
	n.peerStateMu.Lock()
	n.quarantineUntil[peerID] = time.Now().Add(duration)
	n.peerStateMu.Unlock()
	if DebugNet {
		fmt.Printf("?? Peer quarantined: %s reason=%s\n", peerID, reason)
	}
}

// disconnectPeerID implements the disconnect peer id helper.
func (n *Node) disconnectPeerID(peerID, reason string) {
	if peerID == "" {
		return
	}
	reason = strings.TrimSpace(reason)
	trustedPeer := n.isValidatorOrPersistentPeerID(peerID)
	if trustedPeer && isSoftTrustedPeerDisconnectReason(reason) {
		n.clearDialBackoffForPeerID(peerID)
		log.Printf("[PEER-DISCONNECT-SOFT] peer=%s reason=%s trusted=true action=keep_trusted_connection", ShortID(peerID), reason)
		return
	}
	log.Printf("[PEER-DISCONNECT] peer=%s reason=%s trusted=%t", ShortID(peerID), reason, trustedPeer)
	n.observePeerDisconnect(reason)
	n.quarantinePeer(peerID, reason)
	n.recordDialFailure(peerID)
	n.clearPeerHello(peerID)
	if n.Host == nil {
		return
	}
	// `pid` and `err` store the error produced by this operation.
	pid, err := peer.Decode(peerID)
	if err != nil {
		return
	}
	_ = n.Host.Network().ClosePeer(pid)
}

// isSoftTrustedPeerDisconnectReason returns true for transient local-pressure reasons.
func isSoftTrustedPeerDisconnectReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	if strings.HasPrefix(reason, "rate_limit_") {
		return true
	}
	switch reason {
	case "low_peer_reputation", "rate_limited", "connection_flood", "peer_limit",
		"max_connections", "max_peers", "inbound_peer_limit", "outbound_peer_limit":
		return true
	default:
		return false
	}
}

// peerIdentityFromAddrOrID implements the peer identity from addr or id helper.
func peerIdentityFromAddrOrID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	// `pid` and `ok` store whether the related condition is satisfied.
	if _, pid, ok := splitPeerAddress(raw); ok && strings.TrimSpace(pid) != "" {
		return strings.TrimSpace(pid), true
	}
	// `err` stores the error produced by this operation.
	if _, err := peer.Decode(raw); err == nil {
		return raw, true
	}
	return "", false
}

// peerHelloAdvertisedPeerID implements the peer hello advertised peer id helper.
func peerHelloAdvertisedPeerID(hello PeerHello) string {
	// `pid` and `ok` store whether the related condition is satisfied.
	_, pid, ok := splitPeerAddress(strings.TrimSpace(hello.P2PAddr))
	if !ok {
		return ""
	}
	return strings.TrimSpace(pid)
}

// validatorIdentityPeerID implements the validator identity peer id helper.
func validatorIdentityPeerID(peerAddr, advertisedAddr string) string {
	// `pid` and `ok` store whether the related condition is satisfied.
	if pid, ok := peerIdentityFromAddrOrID(peerAddr); ok {
		return strings.TrimSpace(pid)
	}
	// `pid` and `ok` store whether the related condition is satisfied.
	_, pid, ok := splitPeerAddress(strings.TrimSpace(advertisedAddr))
	if !ok {
		return ""
	}
	return strings.TrimSpace(pid)
}

// peerHelloNodeID implements the peer hello node id helper.
func peerHelloNodeID(hello PeerHello) string {
	// `nodeID` stores the value produced by this operation.
	nodeID := normalizeNodeIdentityID(hello.NodeID)
	if nodeID == "" {
		nodeID = normalizeNodeIdentityID(hello.ValidatorID)
	}
	return nodeID
}

// reserveNodePeerIdentity implements the reserve node peer identity helper.
func (n *Node) reserveNodePeerIdentity(peerID, nodeID string) bool {
	if n == nil {
		return true
	}
	rawNodeID := normalizeNodeIdentityID(nodeID)
	nodeID = nodeIdentityMapKey(rawNodeID)
	peerID = strings.TrimSpace(peerID)
	if nodeID == "" || peerID == "" {
		return true
	}
	// `selfPeerID` stores the value produced by this operation.
	selfPeerID := ""
	if n.Host != nil {
		selfPeerID = n.Host.ID().String()
	}
	// `selfID` stores the value produced by this operation.
	selfIdentity := n.NetworkID
	if strings.TrimSpace(selfIdentity) == "" {
		selfIdentity = n.ID
	}
	if selfID := nodeIdentityMapKey(selfIdentity); selfID != "" && nodeID == selfID && peerID != selfPeerID {
		log.Printf("[DUPLICATE-NODE-ID] node=%s existing_peer=%s new_peer=%s action=reject reason=local_node_id_claim",
			rawNodeID, selfPeerID, peerID)
		log.Printf("%s", duplicateNodeIdentityErrorMessage(rawNodeID))
		n.disconnectPeerID(peerID, "duplicate_local_node_id")
		return false
	}

	n.ensurePeerIsolationMaps()
	n.peerStateMu.Lock()
	// `existingPeerID` stores the value produced by this operation.
	existingPeerID := strings.TrimSpace(n.nodeIDToPeer[nodeID])
	if existingPeerID != "" && existingPeerID != peerID {
		n.peerStateMu.Unlock()
		log.Printf("[DUPLICATE-NODE-ID] node=%s existing_peer=%s new_peer=%s action=reject reason=live_node_conflict",
			rawNodeID, existingPeerID, peerID)
		log.Printf("%s", duplicateNodeIdentityErrorMessage(rawNodeID))
		n.disconnectPeerID(peerID, "duplicate_node_id")
		return false
	}
	n.nodeIDToPeer[nodeID] = peerID
	n.peerStateMu.Unlock()
	return true
}

// reserveValidatorPeerIdentity implements the reserve validator peer identity helper.
func (n *Node) reserveValidatorPeerIdentity(peerID, validatorID, advertisedAddr string) bool {
	if n == nil {
		return true
	}
	validatorID = normalizeValidatorID(validatorID)
	peerID = strings.TrimSpace(peerID)
	if validatorID == "" || peerID == "" {
		return true
	}
	// `selfPeerID` stores the value produced by this operation.
	selfPeerID := ""
	if n.Host != nil {
		selfPeerID = n.Host.ID().String()
	}
	for _, selfID := range n.localConsensusValidatorIDCandidates() {
		if selfID != "" && validatorID == selfID && peerID != selfPeerID {
			log.Printf("[DUPLICATE-NODE-ID] validator=%s existing_peer=%s new_peer=%s action=reject reason=local_validator_id_claim",
				validatorID, selfPeerID, peerID)
			n.disconnectPeerID(peerID, "duplicate_local_validator_id")
			return false
		}
	}

	// `advertisedPeerID` stores the value produced by this operation.
	if advertisedPeerID := strings.TrimSpace(peerHelloAdvertisedPeerID(PeerHello{P2PAddr: advertisedAddr})); advertisedPeerID != "" && advertisedPeerID != peerID {
		log.Printf("[DUPLICATE-NODE-ID] validator=%s advertised_peer=%s remote_peer=%s action=reject reason=advertised_peer_mismatch",
			validatorID, advertisedPeerID, peerID)
		n.disconnectPeerID(peerID, "duplicate_validator_peer_mismatch")
		return false
	}

	ValidatorAddrBook.mu.Lock()
	// `oldAddr` stores the address used by this operation.
	oldAddr := strings.TrimSpace(ValidatorAddrBook.m[validatorID])
	ValidatorAddrBook.mu.Unlock()
	// `oldPeerID` stores the value produced by this operation.
	if oldPeerID := strings.TrimSpace(peerHelloAdvertisedPeerID(PeerHello{P2PAddr: oldAddr})); oldPeerID != "" && oldPeerID != peerID {
		// Same transport endpoint with a new libp2p ID is a peer-ID refresh, not
		// a duplicate validator. This keeps AWS/public validators stable after
		// identity rotation or stale peers.json entries.
		oldBase := stripP2PComponent(oldAddr)
		newBase := stripP2PComponent(advertisedAddr)
		if oldBase == "" || newBase == "" || oldBase != newBase {
			log.Printf("[DUPLICATE-NODE-ID] validator=%s existing_peer=%s new_peer=%s action=reject reason=address_book_conflict",
				validatorID, oldPeerID, peerID)
			n.disconnectPeerID(peerID, "duplicate_validator_id")
			return false
		}
		log.Printf("[PEER-ID-REFRESH] validator=%s endpoint=%s old_peer=%s new_peer=%s source=peer_hello",
			validatorID, oldBase, oldPeerID, peerID)
	}

	n.ensurePeerIsolationMaps()
	n.peerStateMu.Lock()
	// `existingPeerID` stores the value produced by this operation.
	existingPeerID := strings.TrimSpace(n.validatorToPeer[validatorID])
	if existingPeerID != "" && existingPeerID != peerID {
		n.peerStateMu.Unlock()
		log.Printf("[DUPLICATE-NODE-ID] validator=%s existing_peer=%s new_peer=%s action=reject reason=live_peer_conflict",
			validatorID, existingPeerID, peerID)
		n.disconnectPeerID(peerID, "duplicate_validator_id")
		return false
	}
	n.validatorToPeer[validatorID] = peerID
	n.peerStateMu.Unlock()
	return true
}

// peerHelloNonce implements the peer hello nonce helper.
func peerHelloNonce() string {
	// `b` stores the value used by this operation.
	var b [16]byte
	// `err` stores the error produced by this operation.
	if _, err := crand.Read(b[:]); err != nil {
		// `fallback` stores the value produced by this operation.
		fallback := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(fallback[:16])
	}
	return hex.EncodeToString(b[:])
}

// peerHelloSignBytes implements the peer hello sign bytes helper.
func peerHelloSignBytes(hello PeerHello) []byte {
	// `fields` stores the value produced by this operation.
	fields := []string{
		"msc-peer-hello-v1",
		strings.TrimSpace(hello.ChainID),
		strings.TrimSpace(hello.GenesisHash),
		strings.TrimSpace(hello.Version),
		strings.TrimSpace(hello.ConsensusHash),
		normalizeNodeRole(hello.Role),
		normalizeValidatorID(hello.ValidatorID),
		strings.TrimSpace(hello.ValidatorPubKey),
		strings.TrimSpace(hello.P2PAddr),
		strings.TrimSpace(hello.ValidatorSetHash),
		strconv.FormatUint(hello.ValidatorSetHeight, 10),
		strings.TrimSpace(hello.NextValidatorSetHash),
		strconv.FormatUint(hello.ActivationHeight, 10),
		strconv.FormatUint(hello.Height, 10),
		strings.TrimSpace(hello.TipHash),
		strconv.FormatInt(hello.Timestamp, 10),
		strings.TrimSpace(hello.Nonce),
	}
	return []byte(strings.Join(fields, "\n"))
}

// signPeerHello implements the sign peer hello helper.
func (n *Node) signPeerHello(hello *PeerHello) {
	if n == nil || hello == nil || n.Host == nil {
		return
	}
	// `priv` stores the value produced by this operation.
	priv := n.Host.Peerstore().PrivKey(n.Host.ID())
	if priv == nil {
		return
	}
	if hello.Timestamp == 0 {
		hello.Timestamp = time.Now().Unix()
	}
	if strings.TrimSpace(hello.Nonce) == "" {
		hello.Nonce = peerHelloNonce()
	}
	hello.SignatureHex = ""
	// `sig` and `err` store the error produced by this operation.
	sig, err := priv.Sign(peerHelloSignBytes(*hello))
	if err != nil {
		return
	}
	hello.SignatureHex = hex.EncodeToString(sig)
}

// peerHelloPublicKey implements the peer hello public key helper.
func (n *Node) peerHelloPublicKey(peerID string) (libp2pcrypto.PubKey, bool) {
	// `remotePeerID` and `ok` store whether the related condition is satisfied.
	remotePeerID, ok := peerIdentityFromAddrOrID(peerID)
	if !ok {
		return nil, false
	}
	// `pid` and `err` store the error produced by this operation.
	pid, err := peer.Decode(remotePeerID)
	if err != nil {
		return nil, false
	}
	if n != nil && n.Host != nil {
		// `pub` stores the value produced by this operation.
		if pub := n.Host.Peerstore().PubKey(pid); pub != nil {
			return pub, true
		}
	}
	// `pub` and `err` store the error produced by this operation.
	pub, err := pid.ExtractPublicKey()
	if err != nil || pub == nil {
		return nil, false
	}
	return pub, true
}

type peerHelloNonceStore struct {
	// `Nonces` stores the value associated with this record.
	Nonces map[string]int64 `json:"nonces"`
}

// peerHelloNonceStorePath implements the peer hello nonce store path helper.
func (n *Node) peerHelloNonceStorePath() string {
	if n == nil || strings.TrimSpace(n.DataDir) == "" || strings.TrimSpace(n.ID) == "" {
		return ""
	}
	return filepath.Join(nodeDataPath(n.DataDir, n.ID), peerHelloNonceStoreFile)
}

// prunePeerHelloNoncesLocked implements the prune peer hello nonces locked helper.
func (n *Node) prunePeerHelloNoncesLocked(now time.Time) bool {
	if n == nil || n.peerHelloNonces == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	// `changed` stores the value produced by this operation.
	changed := false
	// `key` and `seenAt` track the key used to access the related value.
	for key, seenAt := range n.peerHelloNonces {
		if strings.TrimSpace(key) == "" || seenAt.IsZero() || now.Sub(seenAt) > peerHelloNonceTTL || seenAt.After(now.Add(peerHelloMaxClockSkew)) {
			delete(n.peerHelloNonces, key)
			changed = true
		}
	}
	return changed
}

// loadPeerHelloNonces implements the load peer hello nonces helper.
func (n *Node) loadPeerHelloNonces() {
	// `path` stores the value produced by this operation.
	path := n.peerHelloNonceStorePath()
	if path == "" {
		return
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// `store` stores the value used by this operation.
	var store peerHelloNonceStore
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &store); err != nil {
		return
	}
	n.ensurePeerIsolationMaps()
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `changed` stores the value produced by this operation.
	changed := false
	n.peerStateMu.Lock()
	// `key` and `seenUnix` track the key used to access the related value.
	for key, seenUnix := range store.Nonces {
		key = strings.TrimSpace(key)
		if key == "" || seenUnix <= 0 {
			changed = true
			continue
		}
		// `seenAt` stores the value produced by this operation.
		seenAt := time.Unix(seenUnix, 0)
		if now.Sub(seenAt) > peerHelloNonceTTL || seenAt.After(now.Add(peerHelloMaxClockSkew)) {
			changed = true
			continue
		}
		n.peerHelloNonces[key] = seenAt
	}
	if n.prunePeerHelloNoncesLocked(now) {
		changed = true
	}
	n.peerStateMu.Unlock()
	if changed {
		n.persistPeerHelloNonces()
	}
}

// persistPeerHelloNonces implements the persist peer hello nonces helper.
func (n *Node) persistPeerHelloNonces() {
	// `path` stores the value produced by this operation.
	path := n.peerHelloNonceStorePath()
	if path == "" {
		return
	}
	n.ensurePeerIsolationMaps()
	// `nonces` stores the value produced by this operation.
	nonces := make(map[string]int64)
	n.peerStateMu.Lock()
	// `key` and `seenAt` track the key used to access the related value.
	for key, seenAt := range n.peerHelloNonces {
		key = strings.TrimSpace(key)
		if key != "" && !seenAt.IsZero() {
			nonces[key] = seenAt.Unix()
		}
	}
	n.peerStateMu.Unlock()
	if len(nonces) == 0 {
		_ = os.Remove(path)
		return
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(peerHelloNonceStore{Nonces: nonces}, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(path, raw, 0o600)
}

// sweepPeerHelloNonces implements the sweep peer hello nonces helper.
func (n *Node) sweepPeerHelloNonces(now time.Time) bool {
	if n == nil {
		return false
	}
	n.ensurePeerIsolationMaps()
	n.peerStateMu.Lock()
	// `changed` stores the value produced by this operation.
	changed := n.prunePeerHelloNoncesLocked(now)
	n.peerStateMu.Unlock()
	if changed {
		n.persistPeerHelloNonces()
	}
	return changed
}

// startPeerHelloNonceSweeper implements the start peer hello nonce sweeper helper.
func (n *Node) startPeerHelloNonceSweeper(ctx context.Context) {
	if n == nil {
		return
	}
	if ctx == nil {
		ctx = n.RootContext()
	}
	// `interval` stores the value currently being processed.
	interval := peerHelloNonceSweepInterval
	if interval <= 0 {
		interval = time.Minute
	}
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	n.sweepPeerHelloNonces(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.shutdownCh:
			return
		case now := <-ticker.C:
			n.sweepPeerHelloNonces(now)
		}
	}
}

// acceptPeerHelloNonce implements the accept peer hello nonce helper.
func (n *Node) acceptPeerHelloNonce(peerID string, hello PeerHello) bool {
	// `nonce` stores the value produced by this operation.
	nonce := strings.TrimSpace(hello.Nonce)
	if n == nil || peerID == "" || nonce == "" {
		return false
	}
	n.ensurePeerIsolationMaps()
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := peerID + "|" + nonce
	n.peerStateMu.Lock()
	// `exists` stores whether the related condition is satisfied.
	if _, exists := n.peerHelloNonces[key]; exists {
		n.peerStateMu.Unlock()
		return false
	}
	n.peerHelloNonces[key] = now
	n.peerStateMu.Unlock()
	n.persistPeerHelloNonces()
	return true
}

// verifyPeerHelloSignature verifies peer hello signature.
func (n *Node) verifyPeerHelloSignature(peerID string, hello PeerHello) bool {
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := n.peerHelloPublicKey(peerID)
	if !ok {
		return false
	}
	if hello.Timestamp == 0 || strings.TrimSpace(hello.Nonce) == "" || strings.TrimSpace(hello.SignatureHex) == "" {
		return false
	}
	// `age` stores the value produced by this operation.
	age := time.Since(time.Unix(hello.Timestamp, 0))
	if age < -peerHelloMaxClockSkew || age > peerHelloMaxClockSkew {
		return false
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(strings.TrimSpace(hello.SignatureHex))
	if err != nil || len(sig) == 0 {
		return false
	}
	// `unsigned` stores the value produced by this operation.
	unsigned := hello
	unsigned.SignatureHex = ""
	ok, err = pub.Verify(peerHelloSignBytes(unsigned), sig)
	return err == nil && ok
}

// validatePeerHelloEnvelope validates peer hello envelope.
func (n *Node) validatePeerHelloEnvelope(peerID string, hello PeerHello) bool {
	// `advertisedPeerID` stores the value produced by this operation.
	if advertisedPeerID := peerHelloAdvertisedPeerID(hello); advertisedPeerID != "" {
		// `remotePeerID` and `ok` store whether the related condition is satisfied.
		if remotePeerID, ok := peerIdentityFromAddrOrID(peerID); ok && remotePeerID != advertisedPeerID {
			n.disconnectPeerID(peerID, "peer_id_mismatch")
			return false
		}
	}
	if !isProtocolChainID(hello.ChainID) {
		n.disconnectPeerID(peerID, "chain_id_mismatch")
		return false
	}
	if GenesisHash != "" && (hello.GenesisHash == "" || hello.GenesisHash != GenesisHash) {
		n.disconnectPeerID(peerID, "genesis_hash_mismatch")
		return false
	}
	if hello.Version == "" || hello.Version != Version {
		n.disconnectPeerID(peerID, "version_mismatch")
		return false
	}
	if hello.ConsensusHash == "" || hello.ConsensusHash != consensusParamsHash() {
		n.disconnectPeerID(peerID, "consensus_params_mismatch")
		return false
	}
	// `hasPubKey` stores the key used to access the related value.
	if _, hasPubKey := n.peerHelloPublicKey(peerID); hasPubKey {
		if !n.verifyPeerHelloSignature(peerID, hello) {
			n.disconnectPeerID(peerID, "peer_hello_bad_signature")
			return false
		}
	}
	return true
}

// validatePeerHello validates peer hello.
func (n *Node) validatePeerHello(peerID string, hello PeerHello) bool {
	if !n.validatePeerHelloEnvelope(peerID, hello) {
		return false
	}
	// `hasPubKey` stores the key used to access the related value.
	if _, hasPubKey := n.peerHelloPublicKey(peerID); hasPubKey {
		if !n.acceptPeerHelloNonce(peerID, hello) {
			n.disconnectPeerID(peerID, "peer_hello_replay")
			return false
		}
	}
	// `identityPeerID` stores the current position in the related collection.
	identityPeerID := validatorIdentityPeerID(peerID, hello.P2PAddr)
	if !n.reserveNodePeerIdentity(identityPeerID, peerHelloNodeID(hello)) {
		return false
	}
	if !n.reserveValidatorPeerIdentity(identityPeerID, hello.ValidatorID, hello.P2PAddr) {
		return false
	}
	n.applyPeerHelloPubKey(hello)
	n.peerStateMu.Lock()
	n.peerHelloOK[peerID] = true
	delete(n.quarantineUntil, peerID)
	n.peerStateMu.Unlock()
	return true
}

// applyPeerHelloPubKey applies peer hello pub key.
func (n *Node) applyPeerHelloPubKey(hello PeerHello) {
	// Keep strict signed-announcement behavior when the protocol requires it.
	if !protocolIsTestnet() {
		return
	}
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := normalizeValidatorID(hello.ValidatorID)
	// `pubHex` stores the value produced by this operation.
	pubHex := strings.TrimSpace(hello.ValidatorPubKey)
	if validatorID == "" || pubHex == "" {
		return
	}
	// `pubBytes` and `err` store the error produced by this operation.
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return
	}
	validatorPubKeysMu.RLock()
	// `existing` and `existingOK` store whether the related condition is satisfied.
	existing, existingOK := ValidatorPubKeys[validatorID]
	validatorPubKeysMu.RUnlock()
	// `pubKeyUpdated` stores the value produced by this operation.
	pubKeyUpdated := !existingOK || !bytes.Equal(existing, pubBytes)
	if existingOK && len(existing) == ed25519.PublicKeySize {
		if !bytes.Equal(existing, pubBytes) && DebugConsensus {
			fmt.Printf("Pubkey override (peer-hello testnet) for validator %s\n", ShortID(validatorID))
		}
	}
	validatorPubKeysMu.Lock()
	ValidatorPubKeys[validatorID] = ed25519.PublicKey(pubBytes)
	validatorPubKeysMu.Unlock()
	if pubKeyUpdated {
		// Retry queued blocks that may have failed signature verification with a
		// stale validator pubkey before peer-hello refresh completed.
		go n.ProcessQueuedBlocks()
	}
}

// isPeerHelloOK implements the is peer hello ok helper.
func (n *Node) isPeerHelloOK(peerID string) bool {
	if peerID == "" {
		return false
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.peerHelloOK[peerID]
}

// clearPeerHello implements the clear peer hello helper.
func (n *Node) clearPeerHello(peerID string) {
	if peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerHelloOK, peerID)
	n.peerStateMu.Unlock()
}

// shouldHandshakePeer implements the should handshake peer helper.
func (n *Node) shouldHandshakePeer(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if BlockPublicPeers {
		return n.allowedPeerIDs[peerID]
	}
	if strings.HasPrefix(peerID, "12D3") {
		return true
	}
	return n.allowedPeerIDs[peerID]
}

// recomputeGossipQuiet implements the recompute gossip quiet helper.
func (n *Node) recomputeGossipQuiet() {
	if n.Host == nil {
		return
	}
	// `peers` stores the value produced by this operation.
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		n.setGossipQuiet(true)
		return
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `pid` tracks the current values while iterating.
	for _, pid := range peers {
		// `match` and `ok` store whether the related condition is satisfied.
		if match, ok := n.peerHashMatch[pid.String()]; !ok || !match {
			n.gossipQuiet = false
			return
		}
	}
	n.gossipQuiet = true
}

// isPeerQuarantined implements the is peer quarantined helper.
func (n *Node) isPeerQuarantined(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `until` and `ok` store whether the related condition is satisfied.
	until, ok := n.quarantineUntil[peerID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(n.quarantineUntil, peerID)
		return false
	}
	return true
}

// markHelloSent implements the mark hello sent helper.
func (n *Node) markHelloSent(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `last` and `ok` store whether the related condition is satisfied.
	if last, ok := n.peerHelloSentAt[peerID]; ok {
		if time.Since(last) < peerHelloCooldown {
			return false
		}
	}
	n.peerHelloSentAt[peerID] = time.Now()
	return true
}

// sendPeerHello implements the send peer hello helper.
func (n *Node) sendPeerHello(pid peer.ID) {
	if n.Host == nil {
		return
	}
	if n.isPeerQuarantined(pid.String()) {
		return
	}
	if !n.shouldHandshakePeer(pid.String()) {
		return
	}
	if !n.markHelloSent(pid.String()) {
		return
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, "/msc/peerinfo/1.0.0")
	if err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peer hello stream failed to %s: %v\n", pid, err)
		}
		n.recordDialFailure(pid.String())
		// `errMsg` stores the error produced by this operation.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "peerinfo_protocol_mismatch")
		}
		return
	}
	defer s.Close()
	// `deadline` stores the value produced by this operation.
	deadline := time.Now().Add(5 * time.Second)
	_ = s.SetDeadline(deadline)
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	_ = n.completeOutboundPeerHelloExchange(pid.String(), enc, dec)
}

// decodePeerHelloPayload implements the decode peer hello payload helper.
func decodePeerHelloPayload(raw json.RawMessage) (PeerHello, error) {
	// `hello` stores the value used by this operation.
	var hello PeerHello
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &hello); err == nil {
		if hello.ChainID != "" {
			return hello, nil
		}
	}
	// `wrapped` stores the value used by this operation.
	var wrapped Message
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if wrapped.Type == MsgPeerHello && len(wrapped.Data) > 0 {
			// `err` stores the error produced by this operation.
			if err := json.Unmarshal(wrapped.Data, &hello); err == nil {
				if hello.ChainID != "" {
					return hello, nil
				}
			}
		}
	}
	return PeerHello{}, fmt.Errorf("invalid peer hello payload")
}

// completeOutboundPeerHelloExchange implements the complete outbound peer hello exchange helper.
func (n *Node) completeOutboundPeerHelloExchange(peerID string, enc *json.Encoder, dec *json.Decoder) bool {
	// `raw` stores the value used by this operation.
	var raw json.RawMessage
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&raw); err != nil {
		n.recordDialFailure(peerID)
		return false
	}
	// `peerInfo` and `derr` store the error produced by this operation.
	peerInfo, derr := decodePeerHelloPayload(raw)
	if derr != nil {
		n.recordDialFailure(peerID)
		return false
	}
	if !n.validatePeerHelloEnvelope(peerID, peerInfo) {
		return false
	}
	// `err` stores the error produced by this operation.
	if err := enc.Encode(n.outboundPeerHello()); err != nil {
		n.recordDialFailure(peerID)
		return false
	}
	if !peerHelloHasPostValidationFields(peerInfo) {
		// `err` stores the error produced by this operation.
		if err := dec.Decode(&raw); err != nil {
			n.recordDialFailure(peerID)
			return false
		}
		peerInfo, derr = decodePeerHelloPayload(raw)
		if derr != nil {
			n.recordDialFailure(peerID)
			return false
		}
	}
	if !peerHelloHasPostValidationFields(peerInfo) {
		n.recordDialFailure(peerID)
		return false
	}
	if !n.validatePeerHello(peerID, peerInfo) {
		return false
	}
	n.applyPeerInfo(peerID, peerInfo)
	n.recordDialSuccess(peerID)
	return true
}

// exchangePeerInfo implements the exchange peer info helper.
func (n *Node) exchangePeerInfo(pid peer.ID) {
	if n.Host == nil {
		return
	}
	if n.isPeerQuarantined(pid.String()) {
		return
	}
	if !n.shouldHandshakePeer(pid.String()) {
		return
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, "/msc/peerinfo/1.0.0")
	if err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peer info stream failed to %s: %v\n", pid, err)
		}
		n.recordDialFailure(pid.String())
		// `errMsg` stores the error produced by this operation.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "peerinfo_protocol_mismatch")
		}
		return
	}
	defer s.Close()
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	_ = n.completeOutboundPeerHelloExchange(pid.String(), enc, dec)
}

// sendPeersList implements the send peers list helper.
func (n *Node) sendPeersList(pid peer.ID) {
	if n.Host == nil {
		return
	}
	if n.isPeerQuarantined(pid.String()) {
		return
	}
	// `peers` stores the value produced by this operation.
	peers := n.collectPeerMultiaddrs()
	if len(peers) == 0 {
		return
	}
	// `self` stores the value produced by this operation.
	self := stripP2PComponent(n.SelfAddr)
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(peers))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(peers))
	// `addr` tracks the address used by this operation.
	for _, addr := range peers {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if n.SelfAddr != "" {
			if addr == n.SelfAddr || stripP2PComponent(addr) == self {
				continue
			}
		}
		if n.Host != nil && strings.Contains(addr, n.Host.ID().String()) {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	if len(out) == 0 {
		return
	}
	if len(out) > 50 {
		out = out[:50]
	}
	// `msg` stores the value produced by this operation.
	msg := Message{
		Type: MsgPeers,
		Data: MustJSON(out),
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, pid, "/msc/consensus/1.0.0")
	if err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peers list stream failed to %s: %v\n", pid, err)
		}
		n.recordDialFailure(pid.String())
		// `errMsg` stores the error produced by this operation.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "consensus_protocol_mismatch")
		}
		return
	}
	defer s.Close()
	// `data` stores the value produced by this operation.
	data, _ := json.Marshal(msg)
	_, _ = s.Write(append(data, '\n'))
}

// handlePeersList handles peers list.
func (n *Node) handlePeersList(peerAddr string, peers []string) {
	if len(peers) == 0 {
		return
	}
	// `self` stores the value produced by this operation.
	self := stripP2PComponent(n.SelfAddr)
	// `uniq` stores the value produced by this operation.
	uniq := make(map[string]struct{}, len(peers))
	// `addr` tracks the address used by this operation.
	for _, addr := range peers {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if addr == peerAddr {
			continue
		}
		if n.SelfAddr != "" && (addr == n.SelfAddr || stripP2PComponent(addr) == self) {
			continue
		}
		if n.Host != nil && strings.Contains(addr, n.Host.ID().String()) {
			continue
		}
		uniq[addr] = struct{}{}
	}
	if len(uniq) == 0 {
		return
	}
	// `list` stores the value produced by this operation.
	list := make([]string, 0, len(uniq))
	// `addr` tracks the address used by this operation.
	for addr := range uniq {
		list = append(list, addr)
	}
	sort.Strings(list)
	list = sanitizePeerListWithPreferred(list, n.trustedPeerMultiaddrs())
	// `currentPersistent` stores the value produced by this operation.
	currentPersistent := n.persistentPeersSnapshot()
	// `merged` stores the value produced by this operation.
	merged := mergePeerLists(currentPersistent, list)
	// `sanitizedPersistent` stores the value produced by this operation.
	sanitizedPersistent := sanitizePeerListWithPreferred(merged, n.trustedPeerMultiaddrs())
	n.setPersistentPeers(sanitizedPersistent)
	// `err` stores the error produced by this operation.
	if err := savePersistentPeers(n.DataDir, n.ID, sanitizedPersistent); err != nil && DebugNet {
		fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to persist peers list: %v\n", err)
	}
	n.connectToPeersAsync(sanitizePeerListWithPreferred(list, n.trustedPeerMultiaddrs()), 15*time.Second)
}

// shouldSyncForValidatorSetMismatch implements the should sync for validator set mismatch helper.
func shouldSyncForValidatorSetMismatch(localHeight, peerHeight uint64) bool {
	if peerHeight == 0 {
		return false
	}
	// Never roll back due to a stale peer advertisement.
	return peerHeight >= localHeight
}

// shouldForceSnapshotResyncForValidatorSetMismatch implements the should force snapshot resync for validator set mismatch helper.
func shouldForceSnapshotResyncForValidatorSetMismatch(localHeight, targetHeight uint64) bool {
	if targetHeight == 0 {
		return false
	}
	// Snapshot-first rule: if local is behind, bypass autoheal-repair churn
	// and force deterministic snapshot recovery immediately.
	return targetHeight > localHeight
}

// markValidatorSuspect implements the mark validator suspect helper.
func (n *Node) markValidatorSuspect(id string, at time.Time) {
	id = normalizeValidatorID(id)
	if n == nil || id == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	n.validatorMu.Lock()
	if n.validatorSuspect == nil {
		n.validatorSuspect = make(map[string]time.Time)
	}
	n.validatorSuspect[id] = at
	n.validatorMu.Unlock()
}

// clearValidatorSuspect implements the clear validator suspect helper.
func (n *Node) clearValidatorSuspect(id string) {
	id = normalizeValidatorID(id)
	if n == nil || id == "" {
		return
	}
	n.validatorMu.Lock()
	delete(n.validatorSuspect, id)
	n.validatorMu.Unlock()
}

// validatorSuspectCount implements the validator suspect count helper.
func (n *Node) validatorSuspectCount() int {
	if n == nil {
		return 0
	}
	n.validatorMu.RLock()
	// `count` stores the measured quantity used by this operation.
	count := len(n.validatorSuspect)
	n.validatorMu.RUnlock()
	return count
}

// applyPeerInfo applies peer info.
func (n *Node) applyPeerInfo(peerAddr string, hello PeerHello) {
	// `peerRole` stores the value produced by this operation.
	peerRole := normalizeNodeRole(hello.Role)
	if peerRole == "validator" && hello.ValidatorID == "" {
		peerRole = "full"
	}
	if DebugNet && hello.ValidatorID != "" {
		fmt.Printf("Peer info from %s | validator=%s role=%s\n",
			peerAddr, ShortID(hello.ValidatorID), peerRole)
	} else if DebugNet {
		fmt.Printf("Peer info from %s | role=%s\n", peerAddr, peerRole)
	}
	// Peer hello is informational only. Do not update sync state.
	expectedHash := ""
	// `expectedSource` stores the value produced by this operation.
	expectedSource := "none"
	// `expectedHeight` stores the value produced by this operation.
	expectedHeight := uint64(0)
	// `advertisedActivationHeight` stores the value produced by this operation.
	advertisedActivationHeight := peerHelloActivationHeight(hello)
	if advertisedActivationHeight > 0 {
		expectedHeight = advertisedActivationHeight
	} else if hello.Height > 0 {
		expectedHeight = hello.Height + 1
	}
	if n.shouldTrackLateJoinAuthority() &&
		hello.Height > 0 &&
		(GenesisHash == "" || hello.GenesisHash == "" || strings.EqualFold(strings.TrimSpace(hello.GenesisHash), strings.TrimSpace(GenesisHash))) &&
		strings.TrimSpace(hello.ValidatorSetHash) != "" {
		// `sampleHeight` stores the value produced by this operation.
		sampleHeight := expectedHeight
		if sampleHeight == 0 {
			sampleHeight = hello.Height + 1
		}
		n.noteLateJoinAuthoritySample(sampleHeight, hello.ValidatorSetHash)
	}
	if expectedHeight > 0 {
		expectedHash, expectedSource = n.expectedValidatorSetHashWithSource(expectedHeight)
	}
	// `hashMatch` stores the digest used to identify or verify the related data.
	hashMatch := true
	if hello.ValidatorSetHash != "" && expectedHash != "" {
		hashMatch = hello.ValidatorSetHash == expectedHash
	}
	// `mismatch` stores the value produced by this operation.
	mismatch := !hashMatch && hello.ValidatorSetHash != "" && expectedHash != ""
	// `effectiveValidatorPeer` stores the value produced by this operation.
	effectiveValidatorPeer := peerRole == "validator" && normalizeValidatorID(hello.ValidatorID) != ""
	n.peerStateMu.Lock()
	if n.peerSetHash == nil {
		n.peerSetHash = make(map[string]string)
	}
	if n.peerTipHash == nil {
		n.peerTipHash = make(map[string]string)
	}
	if n.peerHashMatch == nil {
		n.peerHashMatch = make(map[string]bool)
	}
	if n.nodeIDToPeer == nil {
		n.nodeIDToPeer = make(map[string]string)
	}
	if n.peerToValidator == nil {
		n.peerToValidator = make(map[string]string)
	}
	if n.peerRole == nil {
		n.peerRole = make(map[string]string)
	}
	if n.peerHelloOK == nil {
		n.peerHelloOK = make(map[string]bool)
	}
	// `nodeID` stores the value produced by this operation.
	if nodeID := peerHelloNodeID(hello); nodeID != "" {
		n.nodeIDToPeer[nodeID] = peerAddr
	}
	n.peerToValidator[peerAddr] = hello.ValidatorID
	n.peerRole[peerAddr] = peerRole
	n.peerSetHash[peerAddr] = hello.ValidatorSetHash
	n.peerTipHash[peerAddr] = hello.TipHash
	n.peerHashMatch[peerAddr] = hashMatch
	if hello.Height > 0 {
		// Reset peer ACK cursor from fresh hello advertisement.
		// A restarted peer may rejoin from a lower height, and stale higher ACK
		// would otherwise make us skip required historical blocks.
		n.peerAckHeight[peerAddr] = hello.Height
	}
	n.peerStateMu.Unlock()
	n.clearValidatorSuspect(hello.ValidatorID)
	if hashMatch {
		n.clearPeerDriftState(peerAddr)
	}
	if effectiveValidatorPeer {
		n.HandlePeerHello(peerAddr, hello.ValidatorID, hello.P2PAddr)
		n.protectValidatorMeshPeerID(peerAddr)
	}
	if mismatch && effectiveValidatorPeer {
		if n.shouldTrackLateJoinAuthority() && normalizeExpectedValidatorSetSource(expectedSource) == "genesis_bootstrap" {
			if DebugConsensus || DebugNet {
				fmt.Printf("[SET-COMMITMENT] bootstrap_superseded %s | expected=%s got=%s height=%d action=trusted_snapshot\n",
					peerAddr, ShortHash(expectedHash), ShortHash(hello.ValidatorSetHash), hello.Height)
			}
			n.noteLateJoinAuthoritySample(expectedHeight, hello.ValidatorSetHash)
			n.maybeSyncToBestObservedHeight("peer_validator_set_bootstrap_superseded")
			n.recomputeGossipQuiet()
			return
		}
		if !validatorSetSourceIsChainAuthoritative(expectedSource) {
			if DebugConsensus || DebugNet {
				fmt.Printf("[SET-COMMITMENT] weak-source peer mismatch deferred %s | source=%s expected=%s got=%s height=%d\n",
					peerAddr, expectedSource, ShortHash(expectedHash), ShortHash(hello.ValidatorSetHash), hello.Height)
			}
			// Give network time to converge without creating local repair loops from
			// non-authoritative sources (legacy frozen/runtime views).
			n.maybeSyncToBestObservedHeight("peer_validator_set_mismatch_weak_source")
			n.recomputeGossipQuiet()
			return
		}
		if DebugConsensus || DebugNet {
			fmt.Printf("Peer validator set mismatch %s | expected=%s got=%s height=%d\n",
				peerAddr, expectedHash[:8], hello.ValidatorSetHash[:8], hello.Height)
		}
		// `localHeight` stores the value produced by this operation.
		localHeight := n.Blockchain.Height()
		// `finalizedHeight` stores the value produced by this operation.
		finalizedHeight := n.getFinalizedHeight()
		// `effectiveLocal` stores the value produced by this operation.
		effectiveLocal := localHeight
		if finalizedHeight > effectiveLocal {
			effectiveLocal = finalizedHeight
		}
		if !shouldSyncForValidatorSetMismatch(effectiveLocal, hello.Height) {
			if DebugConsensus || DebugNet {
				fmt.Printf("Ignoring validator-set mismatch from stale peer %s (peer=%d local=%d)\n",
					peerAddr, hello.Height, effectiveLocal)
			}
			n.recomputeGossipQuiet()
			return
		}
		// `targetHeight` stores the value produced by this operation.
		targetHeight := expectedHeight
		if targetHeight == 0 {
			targetHeight = hello.Height + 1
		}
		if n.shouldTreatValidatorSetMismatchAsPeerDrift(targetHeight, expectedHash, hello.ValidatorSetHash) {
			// `state` stores the value produced by this operation.
			state := n.recordFinalizedDrift(peerAddr, hello.Height, effectiveLocal, expectedHash, hello.ValidatorSetHash, 1)
			n.applyFinalizedDriftPolicy(peerAddr, state)
			n.handlePersistentPeerDrift(effectiveLocal, targetHeight, expectedHash, hello.ValidatorSetHash, "peer-validator-set-mismatch-autoheal")
			n.recomputeGossipQuiet()
			return
		}
		if n.tryRepairValidatorSetHash(targetHeight, hello.ValidatorSetHash) {
			n.recomputeGossipQuiet()
			return
		}
		// `target` stores the value produced by this operation.
		target := hello.Height
		if target == 0 || target < effectiveLocal {
			target = effectiveLocal
		}
		if n.recordValidatorSetMismatchWithLocal(effectiveLocal, targetHeight, expectedHash, hello.ValidatorSetHash) {
			if target > effectiveLocal {
				n.forceSnapshotResyncNow(target, "peer-validator-set-mismatch-snapshot-first")
				n.maybeSyncToBestObservedHeight("peer_validator_set_mismatch_snapshot_first")
				n.recomputeGossipQuiet()
				return
			}
			n.requestConsensusRecomputePause(targetHeight, "peer_validator_set_mismatch")
			if shouldForceSnapshotResyncForValidatorSetMismatch(effectiveLocal, target) {
				n.forceSnapshotResyncNow(target, "peer-validator-set-mismatch-autoheal")
			} else {
				n.maybeForceNearTipValidatorSetResync(effectiveLocal, targetHeight, "peer-validator-set-mismatch-autoheal")
			}
		}
		// Ensure mismatch still nudges normal sync forward when force-resync is skipped.
		n.maybeSyncToBestObservedHeight("peer_validator_set_mismatch")
	}
	n.maybeConvergeTipFromPeerHello(peerAddr, hello)
	n.recomputeGossipQuiet()
}

// recordPeerAck implements the record peer ack helper.
func (n *Node) recordPeerAck(peerAddr string, height uint64) {
	if peerAddr == "" || height == 0 {
		return
	}
	n.peerStateMu.Lock()
	if n.peerAckHeight == nil {
		n.peerAckHeight = make(map[string]uint64)
	}
	if n.peerTipHash == nil {
		n.peerTipHash = make(map[string]string)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.peerAckHeight[peerAddr]; !ok || height > prev {
		n.peerAckHeight[peerAddr] = height
	}
	n.peerStateMu.Unlock()
}

// peerAckHeightFor implements the peer ack height for helper.
func (n *Node) peerAckHeightFor(peerAddr string) uint64 {
	if peerAddr == "" {
		return 0
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.peerAckHeight[peerAddr]
}

// shouldLogNoBlocks implements the should log no blocks helper.
func (n *Node) shouldLogNoBlocks(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 60*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	// Keep map bounded to avoid unbounded growth on noisy peers.
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// shouldLogSentBlocks implements the should log sent blocks helper.
func (n *Node) shouldLogSentBlocks(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("sent:%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 10*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// shouldLogPartialBatch implements the should log partial batch helper.
func (n *Node) shouldLogPartialBatch(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("partial:%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 10*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// shouldLogFinalizedDrift implements the should log finalized drift helper.
func (n *Node) shouldLogFinalizedDrift(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("drift:%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 30*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// classifyPeerDrift implements the classify peer drift helper.
func classifyPeerDrift(peerHeight, peerFinalized, localHeight, localFinalized uint64) PeerDriftClass {
	if peerHeight > localHeight {
		return PeerDriftClassAhead
	}
	if peerHeight < localHeight {
		return PeerDriftClassStale
	}
	if peerFinalized > localFinalized {
		return PeerDriftClassAhead
	}
	if peerFinalized < localFinalized {
		return PeerDriftClassStale
	}
	return PeerDriftClassDangerous
}

// peerHeightSnapshot implements the peer height snapshot helper.
func (n *Node) peerHeightSnapshot(peerID string) (peerHeight, peerFinalized uint64) {
	if n == nil || peerID == "" {
		return 0, 0
	}
	// `validatorID` stores whether the related condition is satisfied.
	var validatorID string
	n.peerStateMu.Lock()
	peerHeight = n.peerAckHeight[peerID]
	validatorID = n.peerToValidator[peerID]
	n.peerStateMu.Unlock()

	if validatorID == "" {
		return peerHeight, peerFinalized
	}
	// `st` and `ok` store whether the related condition is satisfied.
	st, ok := n.validatorStatusSnapshot(validatorID)
	if !ok {
		return peerHeight, peerFinalized
	}
	if st.ReportedHeight > peerHeight {
		peerHeight = st.ReportedHeight
	}
	if st.FinalizedHeight > 0 {
		peerFinalized = st.FinalizedHeight
	} else if st.ReportedHeight > 0 {
		peerFinalized = st.ReportedHeight
	}
	return peerHeight, peerFinalized
}

// localHeightSnapshot implements the local height snapshot helper.
func (n *Node) localHeightSnapshot() (localHeight, localFinalized uint64) {
	if n == nil {
		return 0, 0
	}
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
	}
	localFinalized = n.getFinalizedHeight()
	if localFinalized == 0 && n.Blockchain != nil {
		localFinalized = n.Blockchain.FinalizedHeight()
	}
	if localHeight == 0 {
		localHeight = localFinalized
	}
	if localFinalized == 0 {
		localFinalized = localHeight
	}
	return localHeight, localFinalized
}

// driftTupleKey implements the drift tuple key helper.
func driftTupleKey(peerID, expected, got string) string {
	return peerID + "|" + expected + "|" + got
}

// recordFinalizedDrift implements the record finalized drift helper.
func (n *Node) recordFinalizedDrift(peerID string, from, to uint64, expected, got string, countDelta int) PeerDriftState {
	if n == nil || peerID == "" {
		return PeerDriftState{}
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := driftTupleKey(peerID, expected, got)
	// `cutoff` stores the value produced by this operation.
	cutoff := now.Add(-2 * finalizedDriftWindow)

	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.peerDriftState == nil {
		n.peerDriftState = make(map[string]PeerDriftState)
	}
	// `k` and `st` track the current values while iterating.
	for k, st := range n.peerDriftState {
		if !st.LastSeen.IsZero() && st.LastSeen.Before(cutoff) {
			delete(n.peerDriftState, k)
		}
	}

	// `st` stores the value produced by this operation.
	st := n.peerDriftState[key]
	if st.LastSeen.IsZero() || now.Sub(st.LastSeen) > finalizedDriftWindow {
		st = PeerDriftState{
			Expected:  expected,
			Got:       got,
			FirstSeen: now,
		}
	}
	if st.FirstSeen.IsZero() {
		st.FirstSeen = now
	}
	st.LastSeen = now
	st.Expected = expected
	st.Got = got
	if countDelta > 0 {
		st.Count += countDelta
	}
	if from > 0 && (st.From == 0 || from < st.From) {
		st.From = from
	}
	if to > st.To {
		st.To = to
	}
	n.peerDriftState[key] = st
	return st
}

// setPeerSyncOnly implements the set peer sync only helper.
func (n *Node) setPeerSyncOnly(peerID string, class PeerDriftClass, until time.Time) {
	if n == nil || peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.peerSyncOnlyUntil == nil {
		n.peerSyncOnlyUntil = make(map[string]time.Time)
	}
	if n.peerSyncOnlyClass == nil {
		n.peerSyncOnlyClass = make(map[string]string)
	}
	if until.IsZero() {
		delete(n.peerSyncOnlyUntil, peerID)
		delete(n.peerSyncOnlyClass, peerID)
		return
	}
	n.peerSyncOnlyUntil[peerID] = until
	n.peerSyncOnlyClass[peerID] = string(class)
}

// isPeerSyncOnly implements the is peer sync only helper.
func (n *Node) isPeerSyncOnly(peerID string) bool {
	if n == nil || peerID == "" {
		return false
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	// `until` and `ok` store whether the related condition is satisfied.
	until, ok := n.peerSyncOnlyUntil[peerID]
	if !ok || until.IsZero() {
		return false
	}
	if time.Now().After(until) {
		delete(n.peerSyncOnlyUntil, peerID)
		delete(n.peerSyncOnlyClass, peerID)
		return false
	}
	return true
}

// isSyncOnlyAllowedMsgType implements the is sync only allowed msg type helper.
func isSyncOnlyAllowedMsgType(msgType string) bool {
	switch msgType {
	case MsgPeerHello, MsgGetBlocks, MsgBlockAck, MsgPing, MsgPong:
		return true
	default:
		return false
	}
}

// shouldLogSyncOnlyDrop implements the should log sync only drop helper.
func (n *Node) shouldLogSyncOnlyDrop(peerID, msgType string) bool {
	if n == nil || peerID == "" || msgType == "" {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := peerID + "|" + msgType
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.peerSyncOnlyLastDropLog == nil {
		n.peerSyncOnlyLastDropLog = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.peerSyncOnlyLastDropLog[key]; ok && now.Sub(prev) < finalizedDriftDropLogInterval {
		return false
	}
	n.peerSyncOnlyLastDropLog[key] = now
	return true
}

// clearPeerDriftState implements the clear peer drift state helper.
func (n *Node) clearPeerDriftState(peerID string) {
	if n == nil || peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	delete(n.peerSyncOnlyUntil, peerID)
	delete(n.peerSyncOnlyClass, peerID)
	// `prefix` stores the value produced by this operation.
	prefix := peerID + "|"
	// `key` tracks the key used to access the related value.
	for key := range n.peerDriftState {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerDriftState, key)
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range n.peerSyncOnlyLastDropLog {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerSyncOnlyLastDropLog, key)
		}
	}
}

// peerMaxDriftState implements the peer max drift state helper.
func (n *Node) peerMaxDriftState(peerID string) (PeerDriftState, bool) {
	if n == nil || peerID == "" {
		return PeerDriftState{}, false
	}
	// `prefix` stores the value produced by this operation.
	prefix := peerID + "|"
	// `best` stores the value used by this operation.
	var best PeerDriftState
	// `found` stores whether the related condition is satisfied.
	found := false
	n.peerStateMu.Lock()
	// `key` and `st` track the key used to access the related value.
	for key, st := range n.peerDriftState {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if !found || st.Count > best.Count || (st.Count == best.Count && st.LastSeen.After(best.LastSeen)) {
			best = st
			found = true
		}
	}
	n.peerStateMu.Unlock()
	return best, found
}

// driftRangeNearTip implements the drift range near tip helper.
func driftRangeNearTip(localFinalized, to uint64) bool {
	if localFinalized == 0 || localFinalized <= finalizedDriftNearTipSlack {
		return true
	}
	return to >= (localFinalized - finalizedDriftNearTipSlack)
}

// shouldLogFinalizedDriftPolicy implements the should log finalized drift policy helper.
func (n *Node) shouldLogFinalizedDriftPolicy(peerID, expected, got string) bool {
	if n == nil {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("drift-policy:%s:%s:%s", peerID, expected, got)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < finalizedDriftDropLogInterval {
		return false
	}
	n.noBlockLogAt[key] = now
	return true
}

// applyFinalizedDriftPolicy applies finalized drift policy.
func (n *Node) applyFinalizedDriftPolicy(peerID string, state PeerDriftState) {
	if n == nil || peerID == "" || state.Count <= finalizedDriftThreshold {
		return
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `peerHeight` and `peerFinalized` store the value produced by this operation.
	peerHeight, peerFinalized := n.peerHeightSnapshot(peerID)
	// `localHeight` and `localFinalized` store the value produced by this operation.
	localHeight, localFinalized := n.localHeightSnapshot()
	// `class` stores the value produced by this operation.
	class := classifyPeerDrift(peerHeight, peerFinalized, localHeight, localFinalized)
	// `syncOnlyUntil` stores the value produced by this operation.
	syncOnlyUntil := now.Add(finalizedDriftCooldown)
	n.setPeerSyncOnly(peerID, class, syncOnlyUntil)

	// `key` stores the key used to access the related value.
	key := driftTupleKey(peerID, state.Expected, state.Got)
	// `recomputeTriggered` stores the value produced by this operation.
	recomputeTriggered := false
	// `action` stores the value produced by this operation.
	action := "sync_only"

	n.peerStateMu.Lock()
	// `st` stores the value produced by this operation.
	st := n.peerDriftState[key]
	if st.Count == 0 {
		st = state
	}
	st.LastClass = class
	st.SyncOnlyUntil = syncOnlyUntil
	switch class {
	case PeerDriftClassStale, PeerDriftClassDangerous:
		if st.RecomputedAt.IsZero() || now.Sub(st.RecomputedAt) >= finalizedDriftCooldown {
			st.RecomputedAt = now
			recomputeTriggered = true
		}
	}
	n.peerDriftState[key] = st
	n.peerStateMu.Unlock()

	// `repairHeight` stores the value produced by this operation.
	repairHeight := st.To
	if repairHeight == 0 {
		repairHeight = localFinalized
	}
	if repairHeight == 0 {
		repairHeight = localHeight
	}

	switch class {
	case PeerDriftClassStale:
		if recomputeTriggered {
			if n.shouldTreatValidatorSetMismatchAsPeerDrift(repairHeight, st.Expected, st.Got) {
				action = "sync_only_cooldown"
			} else {
				_ = n.tryRepairValidatorSetHash(repairHeight, st.Got)
				action = "sync_only_recompute"
			}
		} else {
			action = "sync_only_cooldown"
		}
		if st.Count >= finalizedDriftEscalateThreshold {
			n.disconnectPeerID(peerID, "validator_set_drift_stale_escalated")
			action = "disconnect_quarantine"
		}
	case PeerDriftClassAhead:
		n.maybeSyncToBestObservedHeight("finalized_drift_ahead")
		action = "sync_only_nudge_sync"
	case PeerDriftClassDangerous:
		if recomputeTriggered {
			if n.shouldTreatValidatorSetMismatchAsPeerDrift(repairHeight, st.Expected, st.Got) {
				action = "sync_only_cooldown"
			} else {
				_ = n.tryRepairValidatorSetHash(repairHeight, st.Got)
				action = "sync_only_recompute"
			}
		} else {
			n.disconnectPeerID(peerID, "validator_set_drift_same_height")
			action = "disconnect_quarantine"
		}
	}

	if (DebugSync || DebugConsensus || DebugNet) && n.shouldLogFinalizedDriftPolicy(peerID, st.Expected, st.Got) {
		fmt.Printf("[DRIFT-POLICY] peer=%s class=%s count=%d action=%s recompute=%t sync_only_until=%s\n",
			peerID, class, st.Count, action, recomputeTriggered, syncOnlyUntil.Format(time.RFC3339))
	}
}

// shouldServeBlockRange implements the should serve block range helper.
func (n *Node) shouldServeBlockRange(peerID string, from, to uint64, wantSnapshot bool) bool {
	if n == nil || peerID == "" {
		return false
	}
	if to < from {
		return false
	}
	// If a peer is repeatedly drifting, keep serving lane narrow and near-tip only.
	if drift, ok := n.peerMaxDriftState(peerID); ok && drift.Count > finalizedDriftThreshold {
		// `localFinalized` stores the value produced by this operation.
		_, localFinalized := n.localHeightSnapshot()
		// `rangeLen` stores the measured quantity used by this operation.
		rangeLen := to - from
		if !driftRangeNearTip(localFinalized, to) || rangeLen > finalizedDriftMaxServeRange {
			if (DebugSync || DebugNet) && n.shouldLogFinalizedDriftPolicy(peerID, drift.Expected, drift.Got) {
				fmt.Printf("[DRIFT-SERVE] peer=%s denied range=%d-%d count=%d reason=history_or_range\n",
					peerID, from, to, drift.Count)
			}
			return false
		}
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `mode` stores the value produced by this operation.
	mode := "blocks"
	if wantSnapshot {
		mode = "snapshot"
	}
	// Keep duplicate-loop protection, but don't collide snapshot bootstrap
	// requests with immediate block-verify requests on the same range.
	key := fmt.Sprintf("serve:%s:%s:%d:%d", peerID, mode, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// Guard against tight duplicate request loops for the exact same range.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 1500*time.Millisecond {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// shouldSendSnapshotToPeer implements the should send snapshot to peer helper.
func (n *Node) shouldSendSnapshotToPeer(peerID string, height uint64) bool {
	if n == nil || peerID == "" || height == 0 {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `minInterval` stores the value currently being processed.
	minInterval := 5 * time.Second
	// `drift` and `ok` store whether the related condition is satisfied.
	if drift, ok := n.peerMaxDriftState(peerID); ok && drift.Count > finalizedDriftThreshold {
		minInterval = finalizedDriftSnapshotCooldown
		if drift.Count >= finalizedDriftEscalateThreshold {
			minInterval = finalizedDriftCooldown
		}
	}
	// `peerKey` stores the key used to access the related value.
	peerKey := fmt.Sprintf("snap-send-peer:%s", peerID)
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("snap-send:%s:%d", peerID, height)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[peerKey]; ok && now.Sub(prev) < minInterval {
		return false
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < minInterval {
		return false
	}
	n.noBlockLogAt[peerKey] = now
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// shouldProcessSnapshotOffer implements the should process snapshot offer helper.
func (n *Node) shouldProcessSnapshotOffer(from string, height uint64) bool {
	if n == nil || from == "" || height == 0 {
		return false
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("snap-offer:%s:%d", from, height)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 5*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// shouldLogNetworkProbe implements the should log network probe helper.
func (n *Node) shouldLogNetworkProbe(tag string, interval time.Duration) bool {
	if n == nil || strings.TrimSpace(tag) == "" {
		return false
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("probe:%s", tag)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	// `prev` and `ok` store whether the related condition is satisfied.
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < interval {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		// `cutoff` stores the value produced by this operation.
		cutoff := now.Add(-2 * time.Minute)
		// `k` and `ts` track the current values while iterating.
		for k, ts := range n.noBlockLogAt {
			if ts.Before(cutoff) {
				delete(n.noBlockLogAt, k)
			}
		}
		if len(n.noBlockLogAt) > 3072 {
			n.noBlockLogAt = make(map[string]time.Time)
		}
	}
	return true
}

// recordPeerFlap implements the record peer flap helper.
func (n *Node) recordPeerFlap(peerID string) {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return
	}
	n.ensurePeerIsolationMaps()
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `cutoff` stores the value produced by this operation.
	cutoff := now.Add(-peerFlapWindow)
	// `quarantine` stores the value produced by this operation.
	quarantine := false
	// `trustedMeshPeer` stores the value produced by this operation.
	trustedMeshPeer := n.isValidatorOrPersistentPeerID(peerID)
	n.peerStateMu.Lock()
	// `list` stores the value produced by this operation.
	list := n.peerFlapTimes[peerID]
	// `filtered` stores the value produced by this operation.
	filtered := list[:0]
	// `t` tracks the current values while iterating.
	for _, t := range list {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	n.peerFlapTimes[peerID] = filtered
	if len(filtered) >= peerFlapThreshold {
		if trustedMeshPeer {
			n.peerFlapTimes[peerID] = []time.Time{now}
			delete(n.quarantineUntil, peerID)
			delete(n.peerDialFailures, peerID)
			delete(n.peerDialNext, peerID)
		} else {
			quarantine = true
		}
	}
	n.peerStateMu.Unlock()
	if quarantine {
		n.quarantinePeer(peerID, "peer_flap")
	}
}

// onPeerConnected implements the on peer connected helper.
func (n *Node) onPeerConnected(pid peer.ID) {
	if n.isPeerConnected(pid.String()) {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerSuspectAt, pid.String())
	n.peerHashMatch[pid.String()] = false
	delete(n.peerHelloOK, pid.String())
	n.peerStateMu.Unlock()
	n.setGossipQuiet(false)
	n.setPeerConnected(pid.String(), true)
	n.setPeerConnectedAt(pid.String(), time.Now())
	n.peerStateMu.Lock()
	// `vid` stores the value produced by this operation.
	vid := n.peerToValidator[pid.String()]
	n.peerStateMu.Unlock()
	if vid != "" {
		n.touchValidator(vid, n.Blockchain.Height())
	}
	n.protectValidatorMeshPeerID(pid.String())
	go n.exchangePeerInfo(pid)
	// Send peer list after a short hello window to avoid deterministic
	// "unverified peer" drops when both sides race peers-list first.
	go func() {
		// `deadline` stores the value produced by this operation.
		deadline := time.Now().Add(1200 * time.Millisecond)
		for time.Now().Before(deadline) {
			if n.isPeerHelloOK(pid.String()) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		n.sendPeersList(pid)
	}()
	// Publish validator heartbeat immediately on new peer links so late joiners
	// learn fresh validator pubkeys without waiting for periodic heartbeat ticks.
	if n.canAdvertiseValidatorPresence() {
		n.requestHeartbeatBroadcast(true)
	}
}

// onPeerDisconnected implements the on peer disconnected helper.
func (n *Node) onPeerDisconnected(pid peer.ID) {
	// libp2p disconnect notifications are per connection. Do not mark the peer
	// offline while another live connection to the same peer still exists.
	if n.hasActivePeerConnection(pid) {
		n.setPeerConnected(pid.String(), true)
		if n.peerConnectedFor(pid.String()) == 0 {
			n.setPeerConnectedAt(pid.String(), time.Now())
		}
		return
	}
	// `connectedFor` stores the value produced by this operation.
	connectedFor := n.peerConnectedFor(pid.String())
	n.clearPeerConnectedAt(pid.String())
	n.setPeerConnected(pid.String(), false)
	if connectedFor > 0 && connectedFor < peerMinHold {
		return
	}
	n.peerStateMu.Lock()
	n.peerSuspectAt[pid.String()] = time.Now()
	delete(n.peerHelloOK, pid.String())
	delete(n.peerAckHeight, pid.String())
	delete(n.peerTipHash, pid.String())
	n.peerStateMu.Unlock()
	n.recordPeerFlap(pid.String())
	n.setGossipQuiet(false)
	// Mark validator as suspect if we know the mapping
	n.peerStateMu.Lock()
	// `vid` stores the value produced by this operation.
	vid := n.peerToValidator[pid.String()]
	n.peerStateMu.Unlock()
	n.markValidatorSuspect(vid, time.Now())
	// Disconnect notifications can be transient or per-connection. Keep the
	// validator in suspect state first; monitorPeerSuspects marks it offline
	// only if the peer remains gone past peerSuspectTimeout.
}

// removeValidatorByPeer implements the remove validator by peer helper.
func (n *Node) removeValidatorByPeer(peerID string) {
	n.peerStateMu.Lock()
	// `vid` and `ok` store whether the related condition is satisfied.
	vid, ok := n.peerToValidator[peerID]
	n.peerStateMu.Unlock()
	if !ok || vid == "" {
		return
	}
	n.markValidatorOffline(vid, "peer_suspect_expired")
	n.validatorMu.Lock()
	// `st` and `ok` store whether the related condition is satisfied.
	if st, ok := n.validatorStatus[vid]; ok && st != nil {
		// Keep status entry so inactive-removal logic can evict deterministically
		// after ValidatorInactiveBlocks instead of keeping the validator forever.
		if st.LastSeen.IsZero() || time.Since(st.LastSeen) > 20*time.Second {
			st.Active = false
		}
	}
	n.validatorMu.Unlock()
}

// monitorPeerSuspects implements the monitor peer suspects helper.
func (n *Node) monitorPeerSuspects() {
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(peerSuspectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			// `toRemove` stores the value used by this operation.
			var toRemove []string
			n.peerStateMu.Lock()
			// `peerID` and `ts` track the current values while iterating.
			for peerID, ts := range n.peerSuspectAt {
				if time.Since(ts) >= peerSuspectTimeout {
					toRemove = append(toRemove, peerID)
				}
			}
			n.peerStateMu.Unlock()
			// `peerID` tracks the current values while iterating.
			for _, peerID := range toRemove {
				if n.Host != nil {
					// `pid` and `err` store the error produced by this operation.
					if pid, err := peer.Decode(peerID); err == nil {
						if n.Host.Network().Connectedness(pid) == network.Connected {
							continue
						}
					}
				}
				if DebugNet {
					fmt.Printf("Ã°Å¸Â§Â¹ Suspect peer expired: %s\n", peerID)
				}
				n.removeValidatorByPeer(peerID)
				n.peerStateMu.Lock()
				delete(n.peerSuspectAt, peerID)
				delete(n.peerHashMatch, peerID)
				delete(n.peerSetHash, peerID)
				delete(n.peerTipHash, peerID)
				delete(n.peerHelloOK, peerID)
				delete(n.peerSyncOnlyUntil, peerID)
				delete(n.peerSyncOnlyClass, peerID)
				// `prefix` stores the value produced by this operation.
				prefix := peerID + "|"
				// `key` tracks the key used to access the related value.
				for key := range n.peerDriftState {
					if strings.HasPrefix(key, prefix) {
						delete(n.peerDriftState, key)
					}
				}
				// `key` tracks the key used to access the related value.
				for key := range n.peerSyncOnlyLastDropLog {
					if strings.HasPrefix(key, prefix) {
						delete(n.peerSyncOnlyLastDropLog, key)
					}
				}
				n.peerStateMu.Unlock()
			}
			n.recomputeGossipQuiet()
		}
	}
}

// startSelfHeal implements the start self heal helper.
func (n *Node) startSelfHeal(ctx context.Context) {
	if n == nil || !SelfHealEnabled {
		return
	}
	// `interval` stores the value currently being processed.
	interval := SelfHealInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	// `minPeers` stores the value produced by this operation.
	minPeers := normalizedMinimumPeers(SelfHealMinPeers)
	// Heal toward a healthy sparse mesh, not merely the emergency floor.
	targetPeers := TargetPeers
	if targetPeers < minPeers {
		targetPeers = minPeers
	}
	if MaxPeers > 0 && targetPeers > MaxPeers {
		targetPeers = MaxPeers
	}
	// `lastHeight` stores the value produced by this operation.
	lastHeight := uint64(0)
	// `lastProgress` stores the value produced by this operation.
	lastProgress := time.Now()
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			if n.Host == nil {
				continue
			}
			// Reputation is also enforced for connections that became unhealthy
			// after admission. Trusted validator/persistent peers remain subject to
			// hard quarantine, but are not removed for transient soft failures.
			n.enforceConnectedPeerReputation()
			// `height` stores the value produced by this operation.
			height := uint64(0)
			if n.Blockchain != nil {
				height = n.Blockchain.FinalizedHeight()
				if height == 0 {
					height = n.Blockchain.Height()
				}
			}
			if height != 0 && height != lastHeight {
				lastHeight = height
				lastProgress = time.Now()
			}
			// `peers` stores the value produced by this operation.
			peers := len(n.Host.Network().Peers())
			// `stalled` stores the value produced by this operation.
			stalled := false
			if SelfHealStallSeconds > 0 {
				// `observedHeight` stores the value produced by this operation.
				observedHeight, _ := n.bestObservedSyncHeight()
				// `lagging` stores the value produced by this operation.
				lagging := observedHeight > height && observedHeight > 0
				if lagging && time.Since(lastProgress) >= time.Duration(SelfHealStallSeconds)*time.Second {
					stalled = true
				}
			}
			if peers < targetPeers || stalled {
				if peers < targetPeers && n.DHT != nil {
					n.triggerDHTPeerDiscovery(ctx, n.DHT)
				}
				if n.Role == "validator" {
					// `targets` stores the value produced by this operation.
					targets := n.validatorMeshTargets()
					if len(targets) > 0 {
						if peers < n.validatorMeshUrgentPeerFloor(len(targets)) || stalled {
							n.clearDialBackoffForPeerAddrs(targets)
						}
						n.connectToPeers(ctx, targets)
					}
				}
				// `persistent` and `seeds` store the value produced by this operation.
				persistent, seeds := n.configPeerListsSnapshot()
				// `extras` stores the value produced by this operation.
				extras := mergePeerLists(nil, persistent)
				extras = mergePeerLists(extras, seeds)
				extras = sanitizePeerListWithPreferred(extras, n.trustedPeerMultiaddrs())
				if len(extras) > 0 {
					if peers < n.validatorMeshUrgentPeerFloor(len(extras)) || stalled {
						n.clearDialBackoffForPeerAddrs(extras)
					}
					n.connectToPeers(ctx, extras)
				}
				n.connectPubSubPeers()
				n.recomputeGossipQuiet()
			}
			n.decayDialFailures(time.Now())
		}
	}
}

// isTopicJoined implements the is topic joined helper.
func (n *Node) isTopicJoined(topic string) bool {
	if n.PubSub == nil {
		return false
	}
	// `t` tracks the current values while iterating.
	for _, t := range n.PubSub.GetTopics() {
		if t == topic {
			return true
		}
	}
	return false
}

// ShortID returns a shortened version of the ID
func ShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// signData implements the sign data helper.
func signData(data map[string]interface{}, privKey ed25519.PrivateKey) ([]byte, error) {
	// Create a deterministic representation of the data for signing
	// Remove signature field if present
	delete(data, "signature")
	// Sort keys for consistent serialization
	keys := make([]string, 0, len(data))
	// `k` tracks the current values while iterating.
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// `buffer` stores the value used by this operation.
	var buffer bytes.Buffer
	// `k` tracks the current values while iterating.
	for _, k := range keys {
		// `value` stores the value currently being processed.
		value := data[k]
		buffer.WriteString(fmt.Sprintf("%s:%v|", k, value))
	}
	// Sign the data
	hash := sha256.Sum256(buffer.Bytes())
	return ed25519.Sign(privKey, hash[:]), nil
}

// validatorAnnounceSignBytes implements the validator announce sign bytes helper.
func validatorAnnounceSignBytes(nodeID string, pubKey string, reported uint64, finalized uint64, execEpoch uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%d|%d|%d|%t", nodeID, pubKey, reported, finalized, execEpoch, isValidator))
}

// validatorAnnounceSignBytesV2 implements the validator announce sign bytes v2 helper.
func validatorAnnounceSignBytesV2(nodeID string, pubKey string, p2pAddr string, reported uint64, finalized uint64, execEpoch uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d|%d|%d|%t", nodeID, pubKey, p2pAddr, reported, finalized, execEpoch, isValidator))
}

// validatorAnnounceSignBytesV3 implements the validator announce sign bytes v3 helper.
func validatorAnnounceSignBytesV3(nodeID string, pubKey string, p2pAddr string, reported uint64, finalized uint64, execEpoch uint64, validatorSetHeight uint64, validatorSetHash string, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d|%d|%d|%d|%s|%t", nodeID, pubKey, p2pAddr, reported, finalized, execEpoch, validatorSetHeight, validatorSetHash, isValidator))
}

// validatorAnnounceSignBytesV4 implements the validator announce sign bytes v4 helper.
func validatorAnnounceSignBytesV4(nodeID string, pubKey string, p2pAddr string, reported uint64, finalized uint64, execEpoch uint64, validatorSetHeight uint64, validatorSetHash string, nextValidatorSetHash string, nextActivationHeight uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d|%d|%d|%d|%s|%s|%d|%t",
		nodeID,
		pubKey,
		p2pAddr,
		reported,
		finalized,
		execEpoch,
		validatorSetHeight,
		validatorSetHash,
		nextValidatorSetHash,
		nextActivationHeight,
		isValidator,
	))
}

// validatorAnnounceSignBytesV5 implements the validator announce sign bytes v5 helper.
func validatorAnnounceSignBytesV5(nodeID string, pubKey string, p2pAddr string, reported uint64, finalized uint64, execEpoch uint64, validatorSetHeight uint64, validatorSetHash string, nextValidatorSetHash string, nextActivationHeight uint64, consensusReadySet bool, consensusReady bool, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d|%d|%d|%d|%s|%s|%d|%t|%t|%t",
		nodeID,
		pubKey,
		p2pAddr,
		reported,
		finalized,
		execEpoch,
		validatorSetHeight,
		validatorSetHash,
		nextValidatorSetHash,
		nextActivationHeight,
		consensusReadySet,
		consensusReady,
		isValidator,
	))
}

// validatorAnnounceSignBytesLegacy implements the validator announce sign bytes legacy helper.
func validatorAnnounceSignBytesLegacy(nodeID string, pubKey string, height uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%d|%t", nodeID, pubKey, height, isValidator))
}

// broadcastValidatorInfoViaBlocks implements the broadcast validator info via blocks helper.
func (n *Node) broadcastValidatorInfoViaBlocks() {
	// =====================================================
	// HARD GUARD - Validator topic must exist
	// =====================================================
	if n.ValidatorTopic == nil && n.PubSub == nil {
		if DebugConsensus {
			fmt.Println("Cannot broadcast validator info: validator topic is nil")
		}
		return
	}
	// `reported` stores the value produced by this operation.
	reported := uint64(0)
	if n.Blockchain != nil {
		reported = n.Blockchain.Height()
	}
	// `finalized` stores the value produced by this operation.
	finalized := n.getFinalizedHeight()
	if finalized == 0 {
		finalized = reported
	}
	// `execEpoch` stores the value produced by this operation.
	execEpoch := finalized + 1
	// `pubHex` stores the value produced by this operation.
	pubHex := hex.EncodeToString(n.ValidatorKey.PublicKey)

	// Keep the wire payload aligned with handleValidatorAnnouncement's
	// verifier. The older map/hash signature format was not accepted by the
	// receiver, so peers dropped otherwise valid validator heartbeats.
	announcement := ValidatorAnnouncement{
		NodeID:          n.ID,
		PubKey:          pubHex,
		P2PAddr:         n.SelfAddr,
		Height:          reported,
		ReportedHeight:  reported,
		FinalizedHeight: finalized,
		ExecEpoch:       execEpoch,
		IsValidator:     true,
	}
	if pubHex != "" {
		// `sig` and `ok` store whether the related condition is satisfied.
		sig, ok := n.signValidatorPayload(
			validatorAnnounceSignBytesV2(n.ID, pubHex, n.SelfAddr, reported, finalized, execEpoch, true),
		)
		if ok {
			announcement.Signature = hex.EncodeToString(sig)
		}
	}
	// =====================================================
	// SERIALIZE PAYLOAD
	// =====================================================
	payload, err := json.Marshal(announcement)
	if err != nil {
		if DebugConsensus {
			fmt.Printf("Failed to marshal validator announcement: %v\n", err)
		}
		return
	}
	// `msg` stores the value produced by this operation.
	msg := Message{
		Type: MsgValidatorAnnounce,
		Data: payload,
	}
	// `msgBytes` and `err` store the error produced by this operation.
	msgBytes, err := MarshalP2PMessage(msg)
	if err != nil {
		if DebugConsensus {
			fmt.Printf("Failed to marshal message wrapper: %v\n", err)
		}
		return
	}
	// =====================================================
	// PUBLISH VIA VALIDATOR TOPIC
	// =====================================================
	if n.ValidatorTopic != nil {
		// `err` stores the error produced by this operation.
		if err := n.ValidatorTopic.Publish(context.Background(), msgBytes); err != nil {
			if DebugConsensus {
				fmt.Printf("Validator broadcast failed: %v\n", err)
			}
			return
		}
	} else if n.PubSub != nil {
		// `err` stores the error produced by this operation.
		if err := n.PubSub.Publish(TopicValidator, msgBytes); err != nil {
			_ = n.PubSub.Publish(TopicValidatorsLegacy, msgBytes)
		}
	}
	if DebugConsensus {
		fmt.Printf(
			"Validator info broadcast | %s | height=%d\n",
			ShortID(n.ID),
			n.Blockchain.Height(),
		)
	}
}

// Add this function for DHT peer discovery
func (n *Node) discoverPeersViaDHT(ctx context.Context, dhtInst *dht.IpfsDHT) {
	if DisableDHT {
		if DebugNet {
			fmt.Println("DHT discovery disabled by config")
		}
		return
	}
	if DebugNet {
		fmt.Println("Ã°Å¸â€Â Starting DHT peer discovery...")
	}
	// Initial discovery after a short delay
	time.Sleep(5 * time.Second)
	// Do not wait for the first 45-second ticker before provider discovery.
	n.triggerDHTPeerDiscovery(ctx, dhtInst)
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	// Function to attempt connection to a peer
	tryConnect := func(pid peer.ID) {
		if pid == n.Host.ID() {
			return // Don't connect to self
		}
		if !n.canDialPeerID(pid.String()) {
			return
		}
		// Check if already connected
		if len(n.Host.Network().ConnsToPeer(pid)) > 0 {
			return
		}
		// Get addresses from peerstore
		addrs := n.Host.Peerstore().Addrs(pid)
		if len(addrs) == 0 {
			return
		}
		addrs = sanitizeDiscoveredAddrs(addrs)
		if len(addrs) == 0 {
			n.recordDialFailure(pid.String())
			return
		}
		if BlockPublicPeers {
			addrs = filterPrivateAddrs(addrs)
			if len(addrs) == 0 {
				return
			}
		}
		// `addrInfo` stores the address used by this operation.
		addrInfo := peer.AddrInfo{
			ID:    pid,
			Addrs: addrs,
		}
		if !n.allowDiscoveredPeer(addrInfo) {
			return
		}
		if !connectionLimiter.Allow() {
			return
		}
		if !n.canDialPeer() {
			return
		}
		// Try to connect
		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		// `err` stores the error produced by this operation.
		if err := n.Host.Connect(connectCtx, addrInfo); err == nil {
			if DebugNet {
				fmt.Printf("Ã¢Å“â€¦ DHT discovered and connected to peer: %s\n", pid)
			}
			// Tag and track the peer
			if cm := n.Host.ConnManager(); cm != nil {
				cm.TagPeer(pid, "dht-discovered", 50)
			}
			n.PeersLibp2p = append(n.PeersLibp2p, pid)
			n.rememberPeerDiversityAddr(pid.String(), addrInfo.Addrs, true)
			n.recordDialSuccess(pid.String())
		} else {
			// `errLower` stores the error produced by this operation.
			errLower := strings.ToLower(err.Error())
			if strings.Contains(errLower, "peer id mismatch") ||
				strings.Contains(errLower, "dial to self attempted") {
				// `rawAddr` stores the address used by this operation.
				rawAddr := ""
				if len(addrs) > 0 {
					rawAddr = fmt.Sprintf("%s/p2p/%s", addrs[0].String(), pid.String())
				}
				if n.refreshPeerIDMismatch(rawAddr, pid.String(), err) {
					return
				}
				n.forgetPeer(pid.String(), "peer_id_mismatch")
			}
			n.recordDialFailure(pid.String())
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			// Look for specific providers (network-key only)
			// This helps nodes in the same network find each other
			networkKey := fmt.Sprintf("msc-chain-network-%s", protocolChainID())
			// `mh` and `err` store the error produced by this operation.
			mh, err := multihash.Sum([]byte(networkKey), multihash.SHA2_256, -1)
			if err != nil {
				continue
			}
			// `c` stores the value produced by this operation.
			c := cid.NewCidV1(cid.Raw, mh)
			// `providersCtx` and `cancel` store the context controlling this operation.
			providersCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			// Announce ourselves as a provider
			dhtInst.Provide(providersCtx, c, true)
			// Look for other providers
			providers := dhtInst.FindProvidersAsync(providersCtx, c, MaxPeers)
			// `provider` tracks the current values while iterating.
			for provider := range providers {
				if provider.ID != n.Host.ID() {
					tryConnect(provider.ID)
				}
			}
			cancel()
		}
	}
}

type mdnsNotifee struct {
	// `node` stores the value associated with this record.
	node *Node
}

// HandlePeerFound handles peer found.
func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if m == nil || m.node == nil || m.node.Host == nil {
		return
	}
	if pi.ID == m.node.Host.ID() {
		return
	}
	if len(m.node.Host.Network().ConnsToPeer(pi.ID)) > 0 {
		return
	}
	if BlockPublicPeers {
		// `filtered` stores the value produced by this operation.
		filtered := filterPrivateAddrs(pi.Addrs)
		if len(filtered) == 0 {
			return
		}
		pi.Addrs = filtered
	}
	pi.Addrs = sanitizeDiscoveredAddrs(pi.Addrs)
	if len(pi.Addrs) == 0 {
		m.node.recordDialFailure(pi.ID.String())
		return
	}
	if !connectionLimiter.Allow() || !m.node.canDialPeer() {
		return
	}
	if !m.node.canDialPeerID(pi.ID.String()) {
		return
	}
	if !m.node.allowDiscoveredPeer(pi) {
		return
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `err` stores the error produced by this operation.
	if err := m.node.Host.Connect(ctx, pi); err == nil {
		// `cm` stores the value produced by this operation.
		if cm := m.node.Host.ConnManager(); cm != nil {
			cm.TagPeer(pi.ID, "mdns-discovered", 50)
		}
		if DebugNet {
			fmt.Printf("Ã¢Å“â€¦ mDNS discovered and connected to peer: %s\n", pi.ID.String())
		}
		m.node.rememberPeerDiversityAddr(pi.ID.String(), pi.Addrs, true)
		m.node.recordDialSuccess(pi.ID.String())
	} else {
		// `errLower` stores the error produced by this operation.
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "peer id mismatch") ||
			strings.Contains(errLower, "dial to self attempted") {
			// `rawAddr` stores the address used by this operation.
			rawAddr := ""
			if len(pi.Addrs) > 0 {
				rawAddr = fmt.Sprintf("%s/p2p/%s", pi.Addrs[0].String(), pi.ID.String())
			}
			if m.node.refreshPeerIDMismatch(rawAddr, pi.ID.String(), err) {
				return
			}
			m.node.forgetPeer(pi.ID.String(), "peer_id_mismatch")
		}
		m.node.recordDialFailure(pi.ID.String())
	}
}

// startMDNS implements the start mdns helper.
func (n *Node) startMDNS() {
	if !EnableMDNS || n == nil || n.Host == nil {
		return
	}
	// Windows commonly emits multicast-interface warnings from mDNS.
	// Keep default behavior quiet; allow explicit opt-in when needed.
	if runtime.GOOS == "windows" && strings.TrimSpace(os.Getenv("MSC_FORCE_MDNS")) != "1" {
		fmt.Println("mDNS auto-disabled on Windows (set MSC_FORCE_MDNS=1 to force-enable)")
		return
	}
	// `serviceTag` stores the value produced by this operation.
	serviceTag := fmt.Sprintf("msc-mdns-%s", protocolChainID())
	// `service` stores the value produced by this operation.
	service := mdns.NewMdnsService(n.Host, serviceTag, &mdnsNotifee{node: n})
	// `err` stores the error produced by this operation.
	if err := service.Start(); err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â mDNS start failed: %v\n", err)
		}
		return
	}
	if DebugNet {
		fmt.Printf("Ã°Å¸â€œÂ¡ mDNS discovery enabled (%s)\n", serviceTag)
	}
}

// initLibp2p implements the init libp2p helper.
func (n *Node) initLibp2p(ctx context.Context, listenPort int) error {
	// =====================================================
	// Ã°Å¸â€â€˜ CONVERT VALIDATOR KEY TO LIBP2P KEY
	// =====================================================
	lpPriv, err := loadOrCreateP2PIdentityKey(n.DataDir, n.ID)
	if err != nil {
		return fmt.Errorf("p2p identity key: %w", err)
	}
	// =====================================================
	// Ã°Å¸Å¡â‚¬ CREATE LIBP2P HOST
	// =====================================================
	if n.SelfAddr != "" {
		// `maddr` and `err` store the error produced by this operation.
		if maddr, err := ma.NewMultiaddr(stripP2PComponent(n.SelfAddr)); err == nil {
			// `portStr` and `err` store the error produced by this operation.
			if portStr, err := maddr.ValueForProtocol(ma.P_TCP); err == nil {
				// `port` and `err` store the error produced by this operation.
				if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
					listenPort = port
				}
			}
		}
	}
	// `listenAddr` stores the address used by this operation.
	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)
	// `listenIP` stores the value produced by this operation.
	listenIP := ""
	if n.SelfAddr != "" {
		// `selfAddr` stores the address used by this operation.
		selfAddr := stripP2PComponent(n.SelfAddr)
		// `maddr` and `err` store the error produced by this operation.
		if maddr, err := ma.NewMultiaddr(selfAddr); err == nil {
			// `ip` and `err` store the error produced by this operation.
			if ip, err := maddr.ValueForProtocol(ma.P_IP4); err == nil && ip != "" {
				listenIP = ip
				listenAddr = fmt.Sprintf("/ip4/%s/tcp/%d", ip, listenPort)
			} else {
				listenAddr = selfAddr
			}
		}
	}
	// `sourceMultiAddr` stores the address used by this operation.
	sourceMultiAddr, _ := ma.NewMultiaddr(listenAddr)
	// `opts` stores the value produced by this operation.
	opts := []libp2p.Option{
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(lpPriv),
		libp2p.NATPortMap(),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.Transport(libp2pwebsocket.New),
		libp2p.Transport(libp2pwebrtc.New),
		libp2p.Security(noisesec.ID, noisesec.New),
		libp2p.Muxer(libp2pyamux.ID, libp2pyamux.DefaultTransport),
		libp2p.Ping(true),
	}
	if EnableAutoNAT {
		opts = append(opts, libp2p.EnableNATService())
	}
	if EnableRelay {
		opts = append(opts, libp2p.EnableRelay(), libp2p.EnableRelayService())
	}
	if EnableHolePunch {
		opts = append(opts, libp2p.EnableHolePunching())
	}
	// Connection manager enforces peer limits.
	high := MaxPeers
	if MaxConnections > 0 && (high == 0 || MaxConnections < high) {
		high = MaxConnections
	}
	if high <= 0 {
		high = MaxPeers
	}
	// `low` stores the value produced by this operation.
	low := TargetPeers
	if low > high {
		low = high
	}
	if low < 1 {
		low = 1
	}
	// `cm` and `err` store the error produced by this operation.
	if cm, err := connmgr.NewConnManager(low, high, connmgr.WithGracePeriod(time.Minute)); err == nil {
		opts = append(opts, libp2p.ConnectionManager(cm))
	}
	// `externalAddr` stores the address used by this operation.
	externalAddr := ""
	if ConfigP2PExternalAddr != "" {
		externalAddr = stripP2PComponent(ConfigP2PExternalAddr)
	}
	if listenIP != "" || externalAddr != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			// `filtered` stores the value produced by this operation.
			filtered := make([]ma.Multiaddr, 0, len(addrs))
			if listenIP != "" {
				// `addr` tracks the address used by this operation.
				for _, addr := range addrs {
					// `ip` and `err` store the error produced by this operation.
					if ip, err := addr.ValueForProtocol(ma.P_IP4); err == nil && ip == listenIP {
						filtered = append(filtered, addr)
					}
				}
			} else {
				filtered = append(filtered, addrs...)
			}
			if externalAddr != "" {
				// `maddr` and `err` store the error produced by this operation.
				if maddr, err := ma.NewMultiaddr(externalAddr); err == nil {
					filtered = append(filtered, maddr)
				}
			}
			if len(filtered) == 0 {
				return addrs
			}
			return filtered
		}))
	}
	// `h` and `err` store the error produced by this operation.
	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}
	// `keyPath` stores the key used to access the related value.
	if keyPath := p2pIdentityKeyPath(n.DataDir, n.ID); strings.TrimSpace(keyPath) != "" {
		log.Printf("[P2P-ID] identity key=%s peer_id=%s", keyPath, h.ID().String())
	}
	n.streamManager = NewStreamManager(h)
	// =====================================================
	// Ã°Å¸â€œÂ¡ CREATE GOSSIPSUB
	// =====================================================
	// `ps` and `err` store the error produced by this operation.
	ps, err := newMSCGossipSub(ctx, h, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to create pubsub: %w", err)
	}
	// =====================================================
	// Ã°Å¸â€™Â¾ STORE CORE REFERENCES
	// =====================================================
	n.Host = h
	n.PubSub = ps
	n.Libp2pHost = h
	// =====================================================
	// Ã°Å¸â€â€ LIVE CONNECT/DISCONNECT EVENTS
	// =====================================================
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(_ network.Network, c network.Conn) {
			if !n.allowConnection(c) {
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peer limit reached; closing %s\n", c.RemotePeer().String())
				}
				_ = c.Close()
				return
			}
			n.rememberPeerDiversityConn(c)
			// `remotePeer` stores the value produced by this operation.
			remotePeer := c.RemotePeer()
			peerLastSeenMu.Lock()
			PeerLastSeen[remotePeer.String()] = time.Now()
			peerLastSeenMu.Unlock()
			if !n.isPeerConnected(remotePeer.String()) {
				fmt.Printf("Ã°Å¸â€â€” Peer connected: %s\n", c.RemotePeer().String())
			}
			n.SafeGo("on_peer_connected", func() { n.onPeerConnected(remotePeer) })
		},
		DisconnectedF: func(_ network.Network, c network.Conn) {
			// `remotePeer` stores the value produced by this operation.
			remotePeer := c.RemotePeer()
			if n.hasActivePeerConnection(remotePeer) {
				return
			}
			fmt.Printf("Ã°Å¸â€Å’ Peer disconnected: %s\n", c.RemotePeer().String())
			n.SafeGo("on_peer_disconnected", func() { n.onPeerDisconnected(remotePeer) })
		},
	})
	// =====================================================
	// Ã°Å¸â€”ÂºÃ¯Â¸Â INITIALIZE DHT
	// =====================================================
	var d *dht.IpfsDHT
	if DisableDHT {
		if DebugNet {
			fmt.Println("DHT disabled by config")
		}
	} else {
		// `bootstrapPeers` stores the value produced by this operation.
		bootstrapPeers := make([]peer.AddrInfo, 0)
		// `addBootstrap` stores the value produced by this operation.
		addBootstrap := func(raw string) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return
			}
			// `maddr` and `err` store the error produced by this operation.
			maddr, err := ma.NewMultiaddr(raw)
			if err != nil {
				return
			}
			// `info` and `err` store the error produced by this operation.
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return
			}
			bootstrapPeers = append(bootstrapPeers, *info)
		}
		// `seeds` stores the value produced by this operation.
		_, seeds := n.configPeerListsSnapshot()
		// `persistent` stores the value produced by this operation.
		persistent := n.persistentPeersSnapshot()
		// `seed` tracks the current values while iterating.
		for _, seed := range seeds {
			addBootstrap(seed)
		}
		// `p` tracks the current values while iterating.
		for _, p := range persistent {
			addBootstrap(p)
		}
		if len(bootstrapPeers) == 0 {
			if DebugNet {
				fmt.Println("No DHT bootstrap peers available (set p2p.seeds)")
			}
		} else {
			d, err = dht.New(
				ctx,
				h,
				dht.Mode(dht.ModeAuto),
				dht.ProtocolPrefix("/msc/kad/1.0.0"),
				dht.BootstrapPeers(bootstrapPeers...),
				dht.RoutingTableRefreshPeriod(30*time.Second),
			)
			if err != nil {
				if DebugNet {
					fmt.Printf("Failed to create DHT: %v (continuing without DHT)\n", err)
				}
				d = nil
			}
		}
	}
	n.DHT = d
	// =====================================================
	// Ã°Å¸â€œÂ¢ DEBUG INFO
	// =====================================================
	if DebugNet {
		fmt.Println("Ã°Å¸Å’Â LibP2P host initialized")
		fmt.Println("Ã°Å¸â€ â€ PeerID:", h.ID().String())
		fmt.Println("Ã°Å¸â€œÂ Listening addresses:")
		// `addr` tracks the address used by this operation.
		for _, addr := range h.Addrs() {
			fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
		}
		fmt.Println("P2P stack: discovery=DNS seeds + DHT + mDNS, transport=QUIC, security=Noise, messaging=GossipSub, NAT=UPnP + Relay")
		fmt.Printf(
			"Ã°Å¸â€œÅ  Host info: Addrs=%d DHT=%v\n",
			len(h.Addrs()),
			d != nil,
		)
	}
	// =====================================================
	// Ã°Å¸â€â€ž BOOTSTRAP DHT (ASYNC)
	// =====================================================
	if d != nil {
		go func() {
			time.Sleep(3 * time.Second)
			if DebugNet {
				fmt.Println("Ã°Å¸â€â€ž Bootstrapping DHT...")
			}
			// `bootstrapCtx` and `cancel` store the context controlling this operation.
			bootstrapCtx, cancel := context.WithTimeout(
				ctx,
				15*time.Second,
			)
			defer cancel()
			// `err` stores the error produced by this operation.
			if err := d.Bootstrap(bootstrapCtx); err != nil {
				if DebugNet {
					fmt.Printf(
						"Ã¢Å¡Â Ã¯Â¸Â DHT bootstrap failed: %v\n",
						err,
					)
				}
			}
			if DebugNet {
				fmt.Println("Ã¢Å“â€¦ DHT bootstrap complete")
			}
			go n.discoverPeersViaDHT(ctx, d)
		}()
	}
	// =====================================================
	// Ã°Å¸â€œÂ¡ mDNS DISCOVERY (LAN)
	// =====================================================
	if EnableMDNS {
		go n.startMDNS()
	}
	return nil
}

// validatorMeshMode implements the validator mesh mode helper.
func validatorMeshMode() string {
	return "sparse_active_set"
}

// validatorMeshReconcileInterval implements the validator mesh reconcile interval helper.
func validatorMeshReconcileInterval() time.Duration {
	return 5 * time.Second
}

// validatorMeshTargets implements the validator mesh targets helper.
func (n *Node) validatorMeshTargets() []string {
	if n == nil {
		return nil
	}
	// `nextHeight` stores the value produced by this operation.
	nextHeight := uint64(1)
	if n.Blockchain != nil {
		nextHeight = n.Blockchain.Height() + 1
	}
	// `activeSet` stores the value produced by this operation.
	activeSet := canonicalValidatorIDs(n.frozenValidatorsForHeight(nextHeight))
	if len(activeSet) == 0 {
		// Fallback only when deterministic frozen set is not available yet.
		activeSet = canonicalValidatorIDs(n.GetConsensusValidators(int(nextHeight)))
	}
	// `targetIDs` stores the value produced by this operation.
	targetIDs := make(map[string]struct{})
	// `vid` tracks the current values while iterating.
	for _, vid := range activeSet {
		vid = strings.TrimSpace(vid)
		if vid == "" {
			continue
		}
		targetIDs[strings.ToUpper(vid)] = struct{}{}
	}
	// `addrs` stores the address used by this operation.
	addrs := make([]string, 0, len(targetIDs))
	// `vid` tracks the current values while iterating.
	for vid := range targetIDs {
		if strings.EqualFold(vid, n.ID) {
			continue
		}
		// `addr` stores the address used by this operation.
		if addr := n.GetValidatorAddr(vid); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	addrs = sanitizePeerListWithPreferred(addrs, addrs)
	return selectSparsePeerTargets(n.ID, nextHeight, addrs, TargetPeers)
}

// validatorMeshUrgentPeerFloor implements the validator mesh urgent peer floor helper.
func (n *Node) validatorMeshUrgentPeerFloor(targetCount int) int {
	if n == nil {
		return 1
	}
	// `floor` stores the value produced by this operation.
	floor := 1
	if n.Blockchain != nil {
		// `nextHeight` stores the value produced by this operation.
		nextHeight := n.Blockchain.Height() + 1
		// `required` stores the request data being processed.
		if required := n.executionQuorumRequiredForEpoch(nextHeight); required > 0 && required+1 > floor {
			floor = required + 1
		}
	}
	if minPeers := normalizedMinimumPeers(SelfHealMinPeers); minPeers > floor {
		floor = minPeers
	}
	if targetCount > 0 && floor > targetCount {
		floor = targetCount
	}
	if floor < 1 {
		return 1
	}
	return floor
}

// trustedPeerMultiaddrs implements the trusted peer multiaddrs helper.
func (n *Node) trustedPeerMultiaddrs() []string {
	if n == nil {
		return nil
	}
	if n.Config == nil {
		return nil
	}
	// `persistent` and `seeds` store the value produced by this operation.
	persistent, seeds := n.configPeerListsSnapshot()
	// Order matters: later sources override earlier ones for same endpoint.
	// Keep runtime order so fresh discoveries can win over stale static entries.
	trusted := make([]string, 0, len(persistent)+len(seeds)+8)
	trusted = append(trusted, seeds...)
	trusted = append(trusted, persistent...)
	ValidatorAddrBook.mu.RLock()
	// `addr` tracks the address used by this operation.
	for _, addr := range ValidatorAddrBook.m {
		if strings.TrimSpace(addr) != "" {
			trusted = append(trusted, strings.TrimSpace(addr))
		}
	}
	ValidatorAddrBook.mu.RUnlock()
	if n.Host != nil {
		// `pid` tracks the current values while iterating.
		for _, pid := range n.Host.Network().Peers() {
			// `addr` tracks the address used by this operation.
			for _, addr := range n.Host.Peerstore().Addrs(pid) {
				trusted = append(trusted, fmt.Sprintf("%s/p2p/%s", addr.String(), pid.String()))
			}
		}
	}
	return sanitizePeerListWithPreferred(trusted, trusted)
}

// validatorMeshNeedsBackoffReset implements the validator mesh needs backoff reset helper.
func (n *Node) validatorMeshNeedsBackoffReset(targets []string) bool {
	if n == nil || n.Host == nil || len(targets) == 0 {
		return false
	}
	if len(n.Host.Network().Peers()) < n.validatorMeshUrgentPeerFloor(len(targets)) {
		return true
	}
	// `target` tracks the current values while iterating.
	for _, target := range targets {
		// `peerID` and `ok` store whether the related condition is satisfied.
		_, peerID, ok := splitPeerAddress(target)
		if !ok || strings.TrimSpace(peerID) == "" {
			continue
		}
		// `pid` and `err` store the error produced by this operation.
		pid, err := peer.Decode(peerID)
		if err != nil {
			continue
		}
		if n.Host.ID() == pid {
			continue
		}
		if !n.hasActivePeerConnection(pid) {
			return true
		}
	}
	return false
}

// maintainValidatorMesh implements the maintain validator mesh helper.
func (n *Node) maintainValidatorMesh(ctx context.Context) {
	if n == nil || n.Host == nil {
		return
	}
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(validatorMeshReconcileInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			// Persistent peers are an operator-requested topology for every
			// node role. Full/archive observers must redial them after transient
			// disconnects just as validators do.
			targets := n.persistentPeersSnapshot()
			if normalizeNodeRole(n.Role) == "validator" {
				targets = mergePeerLists(targets, n.validatorMeshTargets())
			}
			if len(targets) == 0 {
				continue
			}
			if n.validatorMeshNeedsBackoffReset(targets) {
				n.clearDialBackoffForPeerAddrs(targets)
			}
			n.connectToPeers(ctx, targets)
		}
	}
}

// HandlePeerHello handles peer hello.
func (n *Node) HandlePeerHello(peerAddr, validatorID, advertisedAddr string) {
	// Hard validation
	if validatorID == "" || advertisedAddr == "" {
		return
	}
	advertisedAddr = strings.TrimSpace(advertisedAddr)
	peerAddr = strings.TrimSpace(peerAddr)
	// If /p2p/ is missing in advertised address, attach remote stream peer ID.
	if _, _, hasPID := splitPeerAddress(advertisedAddr); !hasPID {
		// `remotePeerID` and `ok` store whether the related condition is satisfied.
		if _, remotePeerID, ok := splitPeerAddress(peerAddr); ok {
			// `fixedAddr` and `ok` store whether the related condition is satisfied.
			if fixedAddr, ok := peerAddrWithPeerID(advertisedAddr, remotePeerID); ok {
				advertisedAddr = fixedAddr
			}
		}
	}
	if !n.reserveValidatorPeerIdentity(validatorIdentityPeerID(peerAddr, advertisedAddr), validatorID, advertisedAddr) {
		return
	}
	// `updated` stores the value produced by this operation.
	updated := false
	ValidatorAddrBook.mu.Lock()
	// `old` and `exists` store whether the related condition is satisfied.
	if old, exists := ValidatorAddrBook.m[validatorID]; exists {
		updated = old != advertisedAddr
	} else {
		updated = true
	}
	ValidatorAddrBook.m[validatorID] = advertisedAddr
	ValidatorAddrBook.mu.Unlock()
	n.upsertDiscoveredPeerAddress(advertisedAddr, true)
	if updated && DebugConsensus {
		fmt.Printf("Validator registered | id=%s addr=%s\n", validatorID, advertisedAddr)
	}
	// Ensure late-joining peers see our heartbeat quickly.
	if updated && n.canAdvertiseValidatorPresence() {
		n.requestHeartbeatBroadcast(true)
	}
}

// peerListsEqual implements the peer lists equal helper.
func peerListsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// `i` tracks the current position in the related collection.
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// upsertDiscoveredPeerAddress persists verified peer addresses so stale /p2p/
// identities are corrected across restarts.
func (n *Node) upsertDiscoveredPeerAddress(addr string, allowPublic bool) {
	if n == nil {
		return
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	if !allowPublic && !isPeerAddrPrivate(addr) {
		return
	}
	// `selfBase` stores the value produced by this operation.
	selfBase := stripP2PComponent(n.SelfAddr)
	// `addrBase` stores the address used by this operation.
	addrBase := stripP2PComponent(addr)
	if n.SelfAddr != "" && (addr == n.SelfAddr || addrBase == selfBase) {
		return
	}
	if n.Host != nil && strings.Contains(addr, n.Host.ID().String()) {
		return
	}
	// `currentPersistent` stores the value produced by this operation.
	currentPersistent := n.persistentPeersSnapshot()
	// `merged` stores the value produced by this operation.
	merged := mergePeerLists(currentPersistent, []string{addr})
	// `preferred` stores the value produced by this operation.
	preferred := append(n.trustedPeerMultiaddrs(), addr)
	// `sanitized` stores the value produced by this operation.
	sanitized := sanitizePeerListWithPreferred(merged, preferred)
	if peerListsEqual(sanitized, currentPersistent) {
		return
	}
	n.setPersistentPeers(sanitized)
	// `err` stores the error produced by this operation.
	if err := savePersistentPeers(n.DataDir, n.ID, sanitized); err != nil && DebugNet {
		fmt.Printf("Failed to persist discovered peer %s: %v\n", stripP2PComponent(addr), err)
	}
}

// parsePeerAddress parses peer address.
func parsePeerAddress(addr string) (string, error) {
	// If it's already a multiaddr, use it as-is
	if strings.HasPrefix(addr, "/") {
		// Validate it's a proper multiaddr
		_, err := ma.NewMultiaddr(addr)
		if err != nil {
			return "", fmt.Errorf("invalid multiaddr %s: %w", addr, err)
		}
		return addr, nil
	}
	// Try to parse as host:port
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Try adding default port
		if strings.Contains(addr, ":") {
			return "", fmt.Errorf("invalid address %s: %w", addr, err)
		}
		host = addr
		port = "7001" // default port
	}
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	// Create multiaddr WITHOUT peer ID
	return fmt.Sprintf("/ip4/%s/tcp/%s", host, port), nil
}

// isPrivateIP implements the is private ip helper.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	return false
}

// isPrivateMultiaddr implements the is private multiaddr helper.
func isPrivateMultiaddr(maddr ma.Multiaddr) bool {
	// `v` and `err` store the error produced by this operation.
	if v, err := maddr.ValueForProtocol(ma.P_IP4); err == nil && v != "" {
		return isPrivateIP(net.ParseIP(v))
	}
	// `v` and `err` store the error produced by this operation.
	if v, err := maddr.ValueForProtocol(ma.P_IP6); err == nil && v != "" {
		return isPrivateIP(net.ParseIP(v))
	}
	return false
}

// filterPrivateAddrs implements the filter private addrs helper.
func filterPrivateAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	if len(addrs) == 0 {
		return addrs
	}
	// `out` stores the result produced by this operation.
	out := make([]ma.Multiaddr, 0, len(addrs))
	// `addr` tracks the address used by this operation.
	for _, addr := range addrs {
		if isPrivateMultiaddr(addr) {
			out = append(out, addr)
		}
	}
	return out
}

// isPeerAddrPrivate implements the is peer addr private helper.
func isPeerAddrPrivate(raw string) bool {
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "/") {
		// `maddr` and `err` store the error produced by this operation.
		maddr, err := ma.NewMultiaddr(raw)
		if err != nil {
			return false
		}
		return isPrivateMultiaddr(maddr)
	}
	// `host` stores the value produced by this operation.
	host := raw
	// `h` and `err` store the error produced by this operation.
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	// `ip` stores the current position in the related collection.
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return isPrivateIP(ip)
}

// countConnectionDirections implements the count connection directions helper.
func (n *Node) countConnectionDirections() (int, int, int) {
	if n == nil || n.Host == nil {
		return 0, 0, 0
	}
	// `conns` stores the value produced by this operation.
	conns := n.Host.Network().Conns()
	// `inbound` stores the current position in the related collection.
	inbound := 0
	// `outbound` stores the result produced by this operation.
	outbound := 0
	// `c` tracks the current values while iterating.
	for _, c := range conns {
		switch c.Stat().Direction {
		case network.DirInbound:
			inbound++
		case network.DirOutbound:
			outbound++
		}
	}
	return inbound, outbound, len(conns)
}

// canDialPeer implements the can dial peer helper.
func (n *Node) canDialPeer() bool {
	if n == nil || n.Host == nil {
		return false
	}
	// `inbound`, `outbound`, and `total` store the measured quantity used by this operation.
	inbound, outbound, total := n.countConnectionDirections()
	if MaxConnections > 0 && total >= MaxConnections {
		return false
	}
	if MaxPeers > 0 && len(n.Host.Network().Peers()) >= MaxPeers {
		return false
	}
	if MaxOutboundPeers > 0 && outbound >= MaxOutboundPeers {
		return false
	}
	_ = inbound // inbound is enforced for inbound conns only
	return true
}

// allowConnection implements the allow connection helper.
func (n *Node) allowConnection(conn network.Conn) bool {
	if n == nil || n.Host == nil || conn == nil {
		return true
	}
	if !n.allowPeerConnectionFlood(conn) {
		return false
	}
	// `inbound`, `outbound`, and `total` store the measured quantity used by this operation.
	inbound, outbound, total := n.countConnectionDirections()
	if MaxConnections > 0 && total > MaxConnections {
		return false
	}
	if MaxPeers > 0 && len(n.Host.Network().Peers()) > MaxPeers {
		return false
	}
	switch conn.Stat().Direction {
	case network.DirInbound:
		if MaxInboundPeers > 0 && inbound > MaxInboundPeers {
			return false
		}
	case network.DirOutbound:
		if MaxOutboundPeers > 0 && outbound > MaxOutboundPeers {
			return false
		}
	}
	if !n.allowPeerDiversityConn(conn) {
		return false
	}
	return true
}

// peerSubnetKeyFromMultiaddr implements the peer subnet key from multiaddr helper.
func peerSubnetKeyFromMultiaddr(maddr ma.Multiaddr) string {
	if maddr == nil {
		return ""
	}
	// `ip4` and `err` store the error produced by this operation.
	if ip4, err := maddr.ValueForProtocol(ma.P_IP4); err == nil && ip4 != "" {
		// `ip` stores the current position in the related collection.
		ip := net.ParseIP(ip4)
		if ip == nil {
			return ""
		}
		if ip.IsLoopback() {
			return ""
		}
		// `v4` stores the value produced by this operation.
		v4 := ip.To4()
		if v4 == nil {
			return ""
		}
		// `prefix` stores the value produced by this operation.
		prefix := PeerDiversityIPv4Prefix
		if prefix <= 0 || prefix > 32 {
			prefix = 24
		}
		// `masked` stores the value produced by this operation.
		masked := v4.Mask(net.CIDRMask(prefix, 32))
		if masked == nil {
			return ""
		}
		return fmt.Sprintf("%s/%d", masked.String(), prefix)
	}
	// `ip6` and `err` store the error produced by this operation.
	if ip6, err := maddr.ValueForProtocol(ma.P_IP6); err == nil && ip6 != "" {
		// `ip` stores the current position in the related collection.
		ip := net.ParseIP(ip6)
		if ip == nil {
			return ""
		}
		if ip.IsLoopback() {
			return ""
		}
		// `v6` stores the value produced by this operation.
		v6 := ip.To16()
		if v6 == nil {
			return ""
		}
		// `prefix` stores the value produced by this operation.
		prefix := PeerDiversityIPv6Prefix
		if prefix <= 0 || prefix > 128 {
			prefix = 64
		}
		// `masked` stores the value produced by this operation.
		masked := v6.Mask(net.CIDRMask(prefix, 128))
		if masked == nil {
			return ""
		}
		return fmt.Sprintf("%s/%d", masked.String(), prefix)
	}
	return ""
}

// connectedPeersInSubnet implements the connected peers in subnet helper.
func (n *Node) connectedPeersInSubnet(subnet string, excludePeer string) int {
	if n == nil || subnet == "" {
		return 0
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	if n.Host != nil {
		// `c` tracks the current values while iterating.
		for _, c := range n.Host.Network().Conns() {
			if c == nil {
				continue
			}
			// `pid` stores the value produced by this operation.
			pid := c.RemotePeer().String()
			if pid == "" || pid == excludePeer {
				continue
			}
			// `key` stores the key used to access the related value.
			key := peerSubnetKeyFromMultiaddr(c.RemoteMultiaddr())
			if key != subnet {
				continue
			}
			seen[pid] = struct{}{}
		}
	}
	n.peerStateMu.Lock()
	// `pid` and `key` track the key used to access the related value.
	for pid, key := range n.peerSubnet {
		if pid == "" || pid == excludePeer || key != subnet {
			continue
		}
		if !n.connectedPeers[pid] {
			continue
		}
		seen[pid] = struct{}{}
	}
	n.peerStateMu.Unlock()
	return len(seen)
}

// normalizePeerDiversityASN normalizes peer diversity asn.
func normalizePeerDiversityASN(asn string) string {
	asn = strings.ToUpper(strings.TrimSpace(asn))
	if asn == "" {
		return ""
	}
	asn = strings.TrimPrefix(asn, "ASN")
	asn = strings.TrimSpace(asn)
	if asn == "" || strings.EqualFold(asn, "UNKNOWN") {
		return ""
	}
	if !strings.HasPrefix(asn, "AS") {
		asn = "AS" + asn
	}
	return asn
}

// peerDiversityASNFromEnv implements the peer diversity asn from env helper.
func peerDiversityASNFromEnv(peerID string) string {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return ""
	}
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(os.Getenv("MSC_PEER_ASN_MAP"))
	if raw == "" {
		return ""
	}
	// `item` tracks the current position in the related collection.
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 {
			parts = strings.SplitN(strings.TrimSpace(item), ":", 2)
		}
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == peerID {
			return normalizePeerDiversityASN(parts[1])
		}
	}
	return ""
}

// setPeerDiversityASN implements the set peer diversity asn helper.
func (n *Node) setPeerDiversityASN(peerID string, asn string) {
	peerID = strings.TrimSpace(peerID)
	asn = normalizePeerDiversityASN(asn)
	if n == nil || peerID == "" {
		return
	}
	n.ensurePeerIsolationMaps()
	n.peerStateMu.Lock()
	if asn == "" {
		delete(n.peerASN, peerID)
	} else {
		n.peerASN[peerID] = asn
	}
	n.peerStateMu.Unlock()
}

// peerDiversityASNForPeer implements the peer diversity asn for peer helper.
func (n *Node) peerDiversityASNForPeer(peerID string) string {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return ""
	}
	n.peerStateMu.Lock()
	// `asn` stores the value produced by this operation.
	asn := normalizePeerDiversityASN(n.peerASN[peerID])
	n.peerStateMu.Unlock()
	if asn != "" {
		return asn
	}
	if asn = peerDiversityASNFromEnv(peerID); asn != "" {
		n.setPeerDiversityASN(peerID, asn)
		return asn
	}
	return ""
}

// connectedPeersInASN implements the connected peers in asn helper.
func (n *Node) connectedPeersInASN(asn string, excludePeer string) int {
	asn = normalizePeerDiversityASN(asn)
	if n == nil || asn == "" {
		return 0
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	if n.Host != nil {
		// `c` tracks the current values while iterating.
		for _, c := range n.Host.Network().Conns() {
			if c == nil {
				continue
			}
			// `pid` stores the value produced by this operation.
			pid := c.RemotePeer().String()
			if pid == "" || pid == excludePeer {
				continue
			}
			if n.peerDiversityASNForPeer(pid) == asn {
				seen[pid] = struct{}{}
			}
		}
	}
	n.peerStateMu.Lock()
	// `pid` and `knownASN` track the current values while iterating.
	for pid, knownASN := range n.peerASN {
		if pid == "" || pid == excludePeer || normalizePeerDiversityASN(knownASN) != asn {
			continue
		}
		if !n.connectedPeers[pid] {
			continue
		}
		seen[pid] = struct{}{}
	}
	n.peerStateMu.Unlock()
	return len(seen)
}

// rememberPeerDiversityAddr implements the remember peer diversity addr helper.
func (n *Node) rememberPeerDiversityAddr(peerID string, addrs []ma.Multiaddr, outbound bool) {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return
	}
	// `subnet` stores the value produced by this operation.
	subnet := ""
	// `addr` tracks the address used by this operation.
	for _, addr := range addrs {
		subnet = peerSubnetKeyFromMultiaddr(addr)
		if subnet != "" {
			break
		}
	}
	n.ensurePeerIsolationMaps()
	n.peerStateMu.Lock()
	if subnet != "" {
		n.peerSubnet[peerID] = subnet
	}
	if outbound {
		n.peerOutbound[peerID] = true
	}
	n.peerStateMu.Unlock()
	// `asn` stores the value produced by this operation.
	if asn := peerDiversityASNFromEnv(peerID); asn != "" {
		n.setPeerDiversityASN(peerID, asn)
	}
}

// rememberPeerDiversityConn implements the remember peer diversity conn helper.
func (n *Node) rememberPeerDiversityConn(conn network.Conn) {
	if n == nil || conn == nil {
		return
	}
	n.rememberPeerDiversityAddr(conn.RemotePeer().String(), []ma.Multiaddr{conn.RemoteMultiaddr()}, conn.Stat().Direction == network.DirOutbound)
}

// outboundPeersInSubnet implements the outbound peers in subnet helper.
func (n *Node) outboundPeersInSubnet(subnet string) int {
	if n == nil || subnet == "" {
		return 0
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	if n.Host != nil {
		// `c` tracks the current values while iterating.
		for _, c := range n.Host.Network().Conns() {
			if c == nil || c.Stat().Direction != network.DirOutbound {
				continue
			}
			// `pid` stores the value produced by this operation.
			pid := c.RemotePeer().String()
			if pid == "" || peerSubnetKeyFromMultiaddr(c.RemoteMultiaddr()) != subnet {
				continue
			}
			seen[pid] = struct{}{}
		}
	}
	n.peerStateMu.Lock()
	// `pid` and `key` track the key used to access the related value.
	for pid, key := range n.peerSubnet {
		if pid == "" || key != subnet || !n.connectedPeers[pid] || !n.peerOutbound[pid] {
			continue
		}
		seen[pid] = struct{}{}
	}
	n.peerStateMu.Unlock()
	return len(seen)
}

// outboundPeersInASN implements the outbound peers in asn helper.
func (n *Node) outboundPeersInASN(asn string) int {
	asn = normalizePeerDiversityASN(asn)
	if n == nil || asn == "" {
		return 0
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	if n.Host != nil {
		// `c` tracks the current values while iterating.
		for _, c := range n.Host.Network().Conns() {
			if c == nil || c.Stat().Direction != network.DirOutbound {
				continue
			}
			// `pid` stores the value produced by this operation.
			pid := c.RemotePeer().String()
			if pid == "" || n.peerDiversityASNForPeer(pid) != asn {
				continue
			}
			seen[pid] = struct{}{}
		}
	}
	n.peerStateMu.Lock()
	// `pid` and `knownASN` track the current values while iterating.
	for pid, knownASN := range n.peerASN {
		if pid == "" || normalizePeerDiversityASN(knownASN) != asn || !n.connectedPeers[pid] || !n.peerOutbound[pid] {
			continue
		}
		seen[pid] = struct{}{}
	}
	n.peerStateMu.Unlock()
	return len(seen)
}

// allowPeerDiversityASN implements the allow peer diversity asn helper.
func (n *Node) allowPeerDiversityASN(peerID string, outbound bool) bool {
	if n == nil || !PeerDiversityEnabled {
		return true
	}
	if n.isValidatorOrPersistentPeerID(peerID) {
		return true
	}
	// `asn` stores the value produced by this operation.
	asn := n.peerDiversityASNForPeer(peerID)
	if asn == "" {
		return true
	}
	if PeerDiversityMaxPerASN > 0 && n.connectedPeersInASN(asn, peerID) >= PeerDiversityMaxPerASN {
		n.observePeerDiversityReject(outbound)
		if DebugNet {
			fmt.Printf("WARN peer diversity ASN limit blocked: asn=%s max=%d peer=%s\n",
				asn, PeerDiversityMaxPerASN, peerID)
		}
		return false
	}
	if outbound && PeerDiversityMaxOutboundPerASN > 0 && n.outboundPeersInASN(asn) >= PeerDiversityMaxOutboundPerASN {
		n.observePeerDiversityReject(true)
		if DebugNet {
			fmt.Printf("WARN outbound peer diversity ASN blocked: asn=%s max=%d peer=%s\n",
				asn, PeerDiversityMaxOutboundPerASN, peerID)
		}
		return false
	}
	return true
}

// allowPeerDiversityConn implements the allow peer diversity conn helper.
func (n *Node) allowPeerDiversityConn(conn network.Conn) bool {
	if n == nil || conn == nil || !PeerDiversityEnabled {
		return true
	}
	// `peerID` stores the value produced by this operation.
	peerID := conn.RemotePeer().String()
	if n.isValidatorOrPersistentPeerID(peerID) {
		return true
	}
	// `subnet` stores the value produced by this operation.
	subnet := peerSubnetKeyFromMultiaddr(conn.RemoteMultiaddr())
	if subnet != "" && PeerDiversityMaxPerSubnet > 0 {
		// `connected` stores the value produced by this operation.
		connected := n.connectedPeersInSubnet(subnet, peerID)
		if connected >= PeerDiversityMaxPerSubnet {
			n.observePeerDiversityReject(conn.Stat().Direction == network.DirOutbound)
			if DebugNet {
				fmt.Printf("WARN peer diversity subnet limit blocked: subnet=%s connected=%d max=%d peer=%s\n",
					subnet, connected, PeerDiversityMaxPerSubnet, peerID)
			}
			return false
		}
	}
	if !n.allowPeerDiversityASN(peerID, conn.Stat().Direction == network.DirOutbound) {
		return false
	}
	return true
}

// allowPeerDiversityDial implements the allow peer diversity dial helper.
func (n *Node) allowPeerDiversityDial(maddr ma.Multiaddr) bool {
	if n == nil {
		return true
	}
	if !PeerDiversityEnabled {
		return true
	}
	// `peerID` and `ok` store whether the related condition is satisfied.
	if _, peerID, ok := splitPeerAddress(maddr.String()); ok && n.isValidatorOrPersistentPeerID(peerID) {
		return true
	}
	// `subnet` stores the value produced by this operation.
	subnet := peerSubnetKeyFromMultiaddr(maddr)
	if subnet == "" {
		return true
	}
	if PeerDiversityMaxPerSubnet > 0 && n.connectedPeersInSubnet(subnet, "") >= PeerDiversityMaxPerSubnet {
		n.observePeerDiversityReject(true)
		if DebugNet {
			fmt.Printf("WARN peer diversity dial blocked: subnet=%s max=%d\n",
				subnet, PeerDiversityMaxPerSubnet)
		}
		return false
	}
	if PeerDiversityMaxOutboundPerSubnet > 0 && n.outboundPeersInSubnet(subnet) >= PeerDiversityMaxOutboundPerSubnet {
		n.observePeerDiversityReject(true)
		if DebugNet {
			fmt.Printf("WARN outbound peer diversity dial blocked: subnet=%s max=%d\n",
				subnet, PeerDiversityMaxOutboundPerSubnet)
		}
		return false
	}
	return true
}

// allowPeerDiversityOutboundPeer implements the allow peer diversity outbound peer helper.
func (n *Node) allowPeerDiversityOutboundPeer(info *peer.AddrInfo) bool {
	if n == nil || info == nil || !PeerDiversityEnabled {
		return true
	}
	// `peerID` stores the value produced by this operation.
	peerID := info.ID.String()
	if n.isValidatorOrPersistentPeerID(peerID) {
		return true
	}
	if !n.allowPeerDiversityASN(peerID, true) {
		return false
	}
	// `addr` tracks the address used by this operation.
	for _, addr := range info.Addrs {
		// `subnet` stores the value produced by this operation.
		subnet := peerSubnetKeyFromMultiaddr(addr)
		if subnet == "" {
			continue
		}
		if PeerDiversityMaxPerSubnet > 0 && n.connectedPeersInSubnet(subnet, peerID) >= PeerDiversityMaxPerSubnet {
			n.observePeerDiversityReject(true)
			return false
		}
		if PeerDiversityMaxOutboundPerSubnet > 0 && n.outboundPeersInSubnet(subnet) >= PeerDiversityMaxOutboundPerSubnet {
			n.observePeerDiversityReject(true)
			return false
		}
	}
	return true
}

// allowDiscoveredPeer implements the allow discovered peer helper.
func (n *Node) allowDiscoveredPeer(info peer.AddrInfo) bool {
	if n == nil {
		return false
	}
	if info.ID == "" {
		return false
	}
	if n.Host != nil && info.ID == n.Host.ID() {
		return false
	}
	if len(info.Addrs) == 0 {
		n.recordDialFailure(info.ID.String())
		return false
	}
	if !n.peerAdmissionAllowed(info.ID.String()) {
		return false
	}
	if !n.allowPeerDiversityOutboundPeer(&info) {
		return false
	}
	return true
}

type peerDiversitySnapshot struct {
	// `SubnetBuckets` stores the value associated with this record.
	SubnetBuckets int
	// `ASNBuckets` stores the value associated with this record.
	ASNBuckets int
	// `OutboundSubnetBuckets` stores the result produced by this operation.
	OutboundSubnetBuckets int
	// `OutboundASNBuckets` stores the result produced by this operation.
	OutboundASNBuckets int
	// `RejectTotal` stores the measured quantity used by this operation.
	RejectTotal uint64
	// `OutboundRejectTotal` stores the measured quantity used by this operation.
	OutboundRejectTotal uint64
}

// peerDiversitySnapshot implements the peer diversity snapshot helper.
func (n *Node) peerDiversitySnapshot() peerDiversitySnapshot {
	if n == nil {
		return peerDiversitySnapshot{}
	}
	// `subnets` stores the value produced by this operation.
	subnets := make(map[string]struct{})
	// `asns` stores the value produced by this operation.
	asns := make(map[string]struct{})
	// `outSubnets` stores the result produced by this operation.
	outSubnets := make(map[string]struct{})
	// `outASNs` stores the result produced by this operation.
	outASNs := make(map[string]struct{})
	n.peerStateMu.Lock()
	// `pid` and `connected` track the current values while iterating.
	for pid, connected := range n.connectedPeers {
		if !connected {
			continue
		}
		// `subnet` stores the value produced by this operation.
		if subnet := strings.TrimSpace(n.peerSubnet[pid]); subnet != "" {
			subnets[subnet] = struct{}{}
			if n.peerOutbound[pid] {
				outSubnets[subnet] = struct{}{}
			}
		}
		// `asn` stores the value produced by this operation.
		if asn := normalizePeerDiversityASN(n.peerASN[pid]); asn != "" {
			asns[asn] = struct{}{}
			if n.peerOutbound[pid] {
				outASNs[asn] = struct{}{}
			}
		}
	}
	n.peerStateMu.Unlock()
	if n.Host != nil {
		// `c` tracks the current values while iterating.
		for _, c := range n.Host.Network().Conns() {
			if c == nil {
				continue
			}
			// `subnet` stores the value produced by this operation.
			if subnet := peerSubnetKeyFromMultiaddr(c.RemoteMultiaddr()); subnet != "" {
				subnets[subnet] = struct{}{}
				if c.Stat().Direction == network.DirOutbound {
					outSubnets[subnet] = struct{}{}
				}
			}
			// `asn` stores the value produced by this operation.
			if asn := n.peerDiversityASNForPeer(c.RemotePeer().String()); asn != "" {
				asns[asn] = struct{}{}
				if c.Stat().Direction == network.DirOutbound {
					outASNs[asn] = struct{}{}
				}
			}
		}
	}
	// `obs` stores the value produced by this operation.
	obs := n.observabilityStatsSnapshot()
	return peerDiversitySnapshot{
		SubnetBuckets:         len(subnets),
		ASNBuckets:            len(asns),
		OutboundSubnetBuckets: len(outSubnets),
		OutboundASNBuckets:    len(outASNs),
		RejectTotal:           obs.PeerDiversityRejectTotal,
		OutboundRejectTotal:   obs.PeerDiversityOutboundRejectTotal,
	}
}

// `maxDialFanout` defines the result produced by this operation.
const maxDialFanout = 16

// connectToPeersAsync implements the connect to peers async helper.
func (n *Node) connectToPeersAsync(peerMultiaddrs []string, timeout time.Duration) {
	if n == nil || len(peerMultiaddrs) == 0 {
		return
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	// `targets` stores the value produced by this operation.
	targets := append([]string(nil), peerMultiaddrs...)
	go func() {
		if n == nil {
			return
		}
		if n.shutdownCh != nil {
			select {
			case <-n.shutdownCh:
				return
			default:
			}
		}
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(n.RootContext(), timeout)
		defer cancel()
		n.connectToPeers(ctx, targets)
	}()
}

// dialSemaphore implements the dial semaphore helper.
func (n *Node) dialSemaphore() chan struct{} {
	if n == nil {
		return nil
	}
	n.dialSlotsMu.Do(func() {
		n.dialSlots = make(chan struct{}, maxDialFanout)
	})
	return n.dialSlots
}

// releaseDialSlot implements the release dial slot helper.
func (n *Node) releaseDialSlot() {
	// `sem` stores the value produced by this operation.
	sem := n.dialSemaphore()
	if sem == nil {
		return
	}
	select {
	case <-sem:
	default:
	}
}

// Fallback method using blocks topic
func (n *Node) connectToPeers(ctx context.Context, peerMultiaddrs []string) {
	if n.Host == nil {
		if DebugNet {
			fmt.Println("Cannot connect to peers: libp2p host not initialized")
		}
		return
	}
	if ctx == nil {
		ctx = n.RootContext()
	}
	if len(peerMultiaddrs) == 0 {
		if DebugNet {
			fmt.Println("No static peers specified, relying on DHT discovery")
		}
		return
	}
	peerMultiaddrs = sanitizePeerListWithPreferred(peerMultiaddrs, n.trustedPeerMultiaddrs())
	if len(peerMultiaddrs) == 0 {
		return
	}
	// `uniquePeers` stores the value produced by this operation.
	uniquePeers := make(map[string]struct{})
	// `p` tracks the current values while iterating.
	for _, p := range peerMultiaddrs {
		if p == "" {
			continue
		}
		uniquePeers[p] = struct{}{}
	}
	if DebugNet && len(uniquePeers) > 0 && n.shouldLogNetworkProbe(fmt.Sprintf("connect_targets:%d", len(uniquePeers)), 20*time.Second) {
		fmt.Printf("Attempting to connect to %d peers\n", len(uniquePeers))
	}
	// `sem` stores the value produced by this operation.
	sem := n.dialSemaphore()
	// `rawAddr` tracks the address used by this operation.
	for rawAddr := range uniquePeers {
		if rawAddr == "" {
			continue
		}
		if n.shutdownCh != nil {
			select {
			case <-n.shutdownCh:
				return
			default:
			}
		}
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		default:
			if DebugNet && n.shouldLogNetworkProbe("dial_fanout_full", 10*time.Second) {
				fmt.Printf("Dial fanout full (%d); skipping this dial batch item\n", cap(sem))
			}
			continue
		}
		go func(addr string) {
			defer n.releaseDialSlot()
			defer func() {
				// `r` stores the value produced by this operation.
				if r := recover(); r != nil {
					fmt.Printf("[RECOVERED] connect_to_peers_dial panic: %v\n%s\n", r, debug.Stack())
				}
			}()
			// `delay` stores the value produced by this operation.
			delay := time.Duration(rand.Intn(2000)) * time.Millisecond
			// `timer` stores the value produced by this operation.
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			case <-n.shutdownCh:
				return
			}
			if BlockPublicPeers && !isPeerAddrPrivate(addr) {
				if DebugNet {
					fmt.Printf("Skipping public peer (block_public_peers): %s\n", addr)
				}
				return
			}
			if !connectionLimiter.Allow() {
				if DebugNet {
					fmt.Println("Connection rate limited; skipping dial")
				}
				return
			}
			if !n.canDialPeer() {
				if DebugNet {
					fmt.Println("Outbound peer limit reached; skipping dial")
				}
				return
			}
			// `selfBase` stores the value produced by this operation.
			selfBase := stripP2PComponent(n.SelfAddr)
			if selfBase != "" && stripP2PComponent(addr) == selfBase {
				// `stalePID` and `has` store the value produced by this operation.
				if _, stalePID, has := splitPeerAddress(addr); has && (n.Host == nil || stalePID != n.Host.ID().String()) {
					n.forgetPeer(stalePID, "self_dial")
				}
				if DebugNet {
					fmt.Printf("Skipping self dial target: %s\n", addr)
				}
				return
			}
			if !strings.HasPrefix(addr, "/") {
				// `parsedAddr` and `err` store the error produced by this operation.
				parsedAddr, err := parsePeerAddress(addr)
				if err != nil {
					if DebugNet {
						fmt.Printf("Invalid peer address %s: %v\n", addr, err)
					}
					return
				}
				if DebugNet {
					fmt.Printf("Converted %s -> %s (missing peer ID for direct connection)\n", addr, parsedAddr)
				}
				return
			}
			// `maddr` and `err` store the error produced by this operation.
			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				if DebugNet {
					fmt.Printf("Invalid multiaddr %s: %v\n", addr, err)
				}
				return
			}
			// `hasPeerID` stores the value produced by this operation.
			hasPeerID := false
			ma.ForEach(maddr, func(c ma.Component) bool {
				if c.Protocol().Name == "p2p" {
					hasPeerID = true
					return false
				}
				return true
			})
			if !hasPeerID {
				if DebugNet {
					fmt.Printf("Multiaddr %s missing /p2p/ component\n", addr)
				}
				return
			}
			// `addrInfo` and `err` store the error produced by this operation.
			addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				if DebugNet {
					fmt.Printf("Failed to parse peer info: %v\n", err)
				}
				return
			}
			if addrInfo.ID == n.Host.ID() {
				return
			}
			if !n.canDialPeerID(addrInfo.ID.String()) {
				if DebugNet {
					fmt.Printf("Dial backoff active; skipping peer %s\n", addrInfo.ID)
				}
				return
			}
			if n.isPeerConnected(addrInfo.ID.String()) {
				return
			}
			// `err` stores the error produced by this operation.
			if err := n.connectToPeer(ctx, addr); err != nil {
				if DebugNet {
					fmt.Printf("Failed to connect to peer %s: %v\n", addr, err)
				}
			}
		}(rawAddr)
	}
}

// Add this method to the Node struct
func (n *Node) startNetworkCLI() {
	// `reader` stores the value produced by this operation.
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Ã°Å¸Å’Â Network CLI ready (type 'network help' for commands)")
	fmt.Println("Type 'back' to return to main CLI")
	for {
		if n != nil && n.isShuttingDown() {
			return
		}
		fmt.Printf("[network:%s] > ", n.ID)
		// `line` and `err` store the error produced by this operation.
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("[WARN] network CLI input closed; node keeps running: %v\n", err)
			return
		}
		line = strings.TrimSpace(line)
		switch line {
		case "network help", "help":
			fmt.Println("Ã°Å¸Å’Â Network Commands:")
			fmt.Println("  network list       - List connected peers")
			fmt.Println("  network connect    - Connect to a peer")
			fmt.Println("  network info       - Network information")
			fmt.Println("  network topics     - List active PubSub topics")
			fmt.Println("  network stats      - Network statistics")
			fmt.Println("  network maps       - Map size metrics")
			fmt.Println("  status             - Node status")
			fmt.Println("  snapshot latest    - Show latest committed snapshot metadata")
			fmt.Println("  snapshot create    - Ensure/create the committed tip snapshot")
			fmt.Println("  snapshot export    - Export the committed tip snapshot under data/.../snapshots")
			fmt.Println("  snapshot download  - Download, verify, store, and optionally apply a trusted snapshot")
			fmt.Println("  validators         - List active validators")
			fmt.Println("  validatorset       - Show current validator set + hash + pending updates")
			fmt.Println("  validator-pending  - Show pending validator updates")
			fmt.Println("  candidates         - Show candidate admission status")
			fmt.Println("  back               - Return to main CLI")
		case "network list", "list":
			if n.Host == nil {
				fmt.Println("Ã¢Å¡Â Ã¯Â¸Â libp2p host not initialized")
				continue
			}
			// `peers` stores the value produced by this operation.
			peers := n.Host.Network().Peers()
			fmt.Printf("=== Connected Peers (%d) ===\n", len(peers))
			if len(peers) == 0 {
				fmt.Println("No connected peers")
				continue
			}
			// `pid` tracks the current values while iterating.
			for _, pid := range peers {
				// `conns` stores the value produced by this operation.
				conns := n.Host.Network().ConnsToPeer(pid)
				// `addrInfo` stores the address used by this operation.
				addrInfo := n.Host.Peerstore().PeerInfo(pid)
				fmt.Printf("Peer: %s\n", pid.String())
				fmt.Printf("  Connections: %d\n", len(conns))
				fmt.Printf("  Addresses: %v\n", addrInfo.Addrs)
				// Get connection state
				connState := n.Host.Network().Connectedness(pid)
				fmt.Printf("  State: %s\n", connState.String())
				// Get protocol info
				protos, _ := n.Host.Peerstore().GetProtocols(pid)
				if len(protos) > 0 {
					fmt.Printf("  Protocols: %v\n", protos)
				}
				fmt.Println()
			}
		case "network info", "info":
			if n.Host == nil {
				fmt.Println("Ã¢Å¡Â Ã¯Â¸Â libp2p host not initialized")
				continue
			}
			fmt.Println("=== Network Information ===")
			fmt.Printf("Peer ID: %s\n", n.Host.ID())
			fmt.Printf("Listening addresses:\n")
			// `addr` tracks the address used by this operation.
			for _, addr := range n.Host.Addrs() {
				fmt.Printf("  %s\n", addr)
			}
			// Network stats
			peers := n.Host.Network().Peers()
			fmt.Printf("Connected peers: %d\n", len(peers))
			// Connection count
			totalConns := 0
			// `pid` tracks the current values while iterating.
			for _, pid := range peers {
				totalConns += len(n.Host.Network().ConnsToPeer(pid))
			}
			fmt.Printf("Total connections: %d\n", totalConns)
		case "network topics", "topics":
			fmt.Println("=== PubSub Topics ===")
			// `topics` stores the value produced by this operation.
			topics := []string{TopicBlock, TopicTx, TopicConsensus, TopicValidator, TopicSnapshotMeta, TopicSnapshotChunk, TopicSnapshotProof}
			// `topic` tracks the current values while iterating.
			for _, topic := range topics {
				fmt.Printf("- %s\n", topic)
			}
			// Check if topics are active
			if n.BlockTopic != nil {
				fmt.Println("\nBlock topic active")
			}
			if n.TxTopic != nil {
				fmt.Println("Transaction topic active")
			}
			if n.ConsensusTopic != nil {
				fmt.Println("Consensus topic active")
			}
			if n.ValidatorTopic != nil {
				fmt.Println("Validator topic active")
			}
			if n.SnapshotMetaTopic != nil {
				fmt.Println("Snapshot meta topic active")
			}
			if n.SnapshotChunkTopic != nil {
				fmt.Println("Snapshot chunk topic active")
			}
			if n.SnapshotProofTopic != nil {
				fmt.Println("Snapshot proof topic active")
			}
			/* legacy consensus removed
			if n.ProposalTopic != nil {
				fmt.Println("Ã¢Å“â€¦ Proposal topic active")
			}
			if n.VoteTopic != nil {
				fmt.Println("Ã¢Å“â€¦ Vote topic active")
			}
			*/
		case "network stats", "stats":
			if n.Host == nil {
				fmt.Println("Ã¢Å¡Â Ã¯Â¸Â libp2p host not initialized")
				continue
			}
			fmt.Println("=== Network Statistics ===")
			// Peer count by connection state
			peers := n.Host.Network().Peers()
			// `states` stores the value produced by this operation.
			states := make(map[string]int)
			// `pid` tracks the current values while iterating.
			for _, pid := range peers {
				// `state` stores the value produced by this operation.
				state := n.Host.Network().Connectedness(pid)
				states[state.String()]++
			}
			// `state` and `count` track the measured quantity used by this operation.
			for state, count := range states {
				fmt.Printf("%s: %d\n", state, count)
			}
			// Protocol usage
			fmt.Println("\nProtocol usage:")
			// `pid` tracks the current values while iterating.
			for _, pid := range peers {
				// `protos` stores the value produced by this operation.
				protos, _ := n.Host.Peerstore().GetProtocols(pid)
				// `proto` tracks the current values while iterating.
				for _, proto := range protos {
					fmt.Printf("  %s: %s\n", pid.ShortString(), proto)
				}
			}
		case "network maps", "maps":
			// `stats` stores the value produced by this operation.
			stats := n.MapStats()
			fmt.Println("=== Map Sizes ===")
			fmt.Printf("SeenBlockHashes: %d\n", stats.SeenBlockHashes)
			fmt.Printf("SeenTxIDs: %d\n", stats.SeenTxIDs)
			fmt.Printf("ForkBlocks: heights=%d total=%d\n", stats.ForkBlocksHeights, stats.ForkBlocksTotal)
			fmt.Printf("ExecResults: keys=%d total=%d\n", stats.ExecResultsKeys, stats.ExecResultsTotal)
			fmt.Printf("PendingBlocks: %d\n", stats.PendingBlocks)
			fmt.Printf("QueuedExecVotes: keys=%d total=%d\n", stats.QueuedExecVotesKeys, stats.QueuedExecVotesTotal)
			fmt.Printf("AcceptedProposal: %d\n", stats.AcceptedProposal)
			fmt.Printf("ExecBroadcasted: epochs=%d keys=%d\n", stats.ExecBroadcastedEpochs, stats.ExecBroadcastedKeys)
			fmt.Printf("ExecSignerSeen: epochs=%d total=%d\n", stats.ExecSignerSeenEpochs, stats.ExecSignerSeenTotal)
			fmt.Printf("ExecBroadcastedByVal: epochs=%d total=%d\n", stats.ExecBroadcastedByValEpochs, stats.ExecBroadcastedByValTotal)
			fmt.Printf("ExecRebroadcastAt: epochs=%d\n", stats.ExecRebroadcastAtEpochs)
			fmt.Printf("ValidatorStatus: %d\n", stats.ValidatorStatusCount)
			fmt.Printf("Candidates: %d\n", stats.CandidateCount)
			fmt.Printf("PeerState: %d\n", stats.PeerStateCount)
			fmt.Printf("Peers: connected=%d connecting=%d suspect=%d allowed=%d\n",
				stats.PeerConnectedCount, stats.PeerConnectingCount, stats.PeerSuspectCount, stats.AllowedPeerCount)
			fmt.Printf("ValidatorSet: pending=%d removals=%d queued_updates=%d\n",
				stats.ValidatorSetPendingCount, stats.ValidatorSetRemovalCount, stats.QueuedValidatorSetUpdates)
			fmt.Printf("HeightReports: epochs=%d total=%d\n",
				stats.HeightReportsEpochs, stats.HeightReportValidatorsTotal)
		case "status", "network status":
			// `runtime` stores the value produced by this operation.
			runtime := n.runtimeStatusSnapshot()
			// `validators` stores whether the related condition is satisfied.
			validators := len(n.GetConsensusValidators(int(runtime.Height)))
			fmt.Printf(
				"[STATUS] height=%d finalized=%d peers=%d validators=%d syncing=%t sync_complete=%t execution_ready=%t execution_reason=%s gossip=%t gossip_pipeline=%s consensus_running=%t mode=%s vote=%t propose=%t role=%s validator_state=%s warmup_blocks=%d ready=%t reason=%s\n",
				runtime.Height,
				runtime.FinalizedHeight,
				runtime.Peers,
				validators,
				runtime.Syncing,
				runtime.SyncComplete,
				runtime.ExecutionReady,
				runtime.ExecutionWaitReason,
				runtime.GossipRealtime,
				runtime.GossipPipeline,
				runtime.ConsensusRunning,
				runtime.ConsensusMode,
				runtime.VoteEnabled,
				runtime.ProposeEnabled,
				runtime.Role,
				runtime.ValidatorState,
				runtime.SnapshotWarmupRemainingBlocks,
				runtime.Ready,
				runtime.WaitReason,
			)
		case "snapshot latest", "network snapshot latest":
			// `snapshot`, `meta`, `source`, and `err` store the error produced by this operation.
			snapshot, meta, source, err := n.latestCommittedSnapshotMeta()
			if err != nil || snapshot == nil {
				fmt.Println("snapshot not found")
				continue
			}
			fmt.Printf("snapshot height=%d source=%s\n", snapshot.Height, source)
			fmt.Printf("  block_hash=%s\n", strings.TrimSpace(snapshot.BlockHash))
			fmt.Printf("  snapshot_hash=%s\n", strings.TrimSpace(snapshot.SnapshotHash))
			fmt.Printf("  state_root=%s\n", strings.TrimSpace(snapshot.StateRoot))
			fmt.Printf("  validator_set_hash=%s\n", strings.TrimSpace(snapshotValidatorSetHash(snapshot)))
			fmt.Printf("  validator_registry_hash=%s\n", strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)))
			if meta != nil {
				fmt.Printf("  meta_source=%s state_type=%s base_height=%d timestamp=%d\n",
					strings.TrimSpace(meta.Source),
					strings.TrimSpace(meta.StateType),
					meta.BaseHeight,
					meta.Timestamp,
				)
			}
		case "snapshot create", "network snapshot create":
			// `snapshot`, `meta`, `source`, and `err` store the error produced by this operation.
			snapshot, meta, source, err := n.createCommittedTipSnapshot("cli_create", false)
			if err != nil || snapshot == nil {
				fmt.Printf("snapshot create failed: %v\n", err)
				continue
			}
			fmt.Printf("snapshot ready height=%d source=%s hash=%s\n",
				snapshot.Height,
				source,
				strings.TrimSpace(snapshot.SnapshotHash),
			)
			if meta != nil {
				fmt.Printf("  state_root=%s validator_set_hash=%s validator_registry_hash=%s\n",
					strings.TrimSpace(meta.StateRoot),
					strings.TrimSpace(meta.ValidatorSetHash),
					strings.TrimSpace(meta.ValidatorRegistryHash),
				)
			}
		case "snapshot export", "network snapshot export":
			// `snapshot`, `meta`, `source`, and `err` store the error produced by this operation.
			snapshot, meta, source, err := n.createCommittedTipSnapshot("cli_export", false)
			if err != nil || snapshot == nil {
				fmt.Printf("snapshot export failed: %v\n", err)
				continue
			}
			// `err` stores the error produced by this operation.
			if err := n.exportSnapshotArtifacts(snapshot); err != nil {
				fmt.Printf("snapshot export failed: %v\n", err)
				continue
			}
			_ = n.publishSnapshotMetaGossip(snapshot)
			_ = n.publishSnapshotChunkGossip(snapshot)
			fmt.Printf("snapshot exported height=%d source=%s dir=%s\n",
				snapshot.Height,
				source,
				n.snapshotExportDirForHeight(snapshot.Height),
			)
			if meta != nil {
				fmt.Printf("  snapshot_hash=%s state_root=%s\n",
					strings.TrimSpace(meta.SnapshotHash),
					strings.TrimSpace(meta.StateRoot),
				)
			}
		case "snapshot download", "network snapshot download":
			// `result` and `err` store the error produced by this operation.
			result, err := n.downloadTrustedSnapshotAndStore(0, 0, false, true, false, false)
			if err != nil || result == nil || result.Snapshot == nil {
				fmt.Printf("snapshot download failed: %v\n", err)
				continue
			}
			fmt.Printf("snapshot downloaded height=%d source=%s stored=%t applied=%t proofs=%d/%d\n",
				result.Snapshot.Height,
				result.Source,
				result.Stored,
				result.Applied,
				result.Proofs,
				result.RequiredProofs,
			)
			if result.ExportDir != "" {
				fmt.Printf("  export_dir=%s\n", result.ExportDir)
			}
		case "validators", "validator list", "network validators":
			// `vals` stores the value currently being processed.
			vals := n.GetActiveValidators()
			if len(vals) == 0 {
				fmt.Println("no validators")
				continue
			}
			// `v` tracks the current values while iterating.
			for _, v := range vals {
				fmt.Println(v)
			}
		case "validatorset", "validator set", "validator-set", "network validatorset", "network validator set", "network validator-set":
			// `current` stores the value produced by this operation.
			current := n.GetConsensusValidators(int(n.Blockchain.Height() + 1))
			n.validatorSetMu.RLock()
			// `pendingAdds` stores the value produced by this operation.
			pendingAdds := make([]string, 0, len(n.pendingValidators))
			// `id` and `act` track the current position in the related collection.
			for id, act := range n.pendingValidators {
				pendingAdds = append(pendingAdds, fmt.Sprintf("%s@%d", id, act))
			}
			// `pendingRemoves` stores the value produced by this operation.
			pendingRemoves := make([]string, 0, len(n.pendingValidatorRemovals))
			// `id` and `act` track the current position in the related collection.
			for id, act := range n.pendingValidatorRemovals {
				pendingRemoves = append(pendingRemoves, fmt.Sprintf("%s@%d", id, act))
			}
			n.validatorSetMu.RUnlock()
			sort.Strings(current)
			sort.Strings(pendingAdds)
			sort.Strings(pendingRemoves)
			fmt.Printf("validator_set_hash=%s\n", ValidatorSetHash(current)[:8])
			fmt.Printf("validators (%d):\n", len(current))
			// `v` tracks the current values while iterating.
			for _, v := range current {
				fmt.Println(v)
			}
			if len(pendingAdds) > 0 || len(pendingRemoves) > 0 {
				fmt.Println("pending_updates:")
				if len(pendingAdds) > 0 {
					fmt.Printf("  add: %s\n", strings.Join(pendingAdds, ", "))
				}
				if len(pendingRemoves) > 0 {
					fmt.Printf("  remove: %s\n", strings.Join(pendingRemoves, ", "))
				}
			}
		case "validator-pending", "validator pending", "network validator-pending", "network validator pending":
			n.validatorSetMu.RLock()
			// `pendingAdds` stores the value produced by this operation.
			pendingAdds := make([]string, 0, len(n.pendingValidators))
			// `id` and `act` track the current position in the related collection.
			for id, act := range n.pendingValidators {
				pendingAdds = append(pendingAdds, fmt.Sprintf("%s@%d", id, act))
			}
			// `pendingRemoves` stores the value produced by this operation.
			pendingRemoves := make([]string, 0, len(n.pendingValidatorRemovals))
			// `id` and `act` track the current position in the related collection.
			for id, act := range n.pendingValidatorRemovals {
				pendingRemoves = append(pendingRemoves, fmt.Sprintf("%s@%d", id, act))
			}
			n.validatorSetMu.RUnlock()
			sort.Strings(pendingAdds)
			sort.Strings(pendingRemoves)
			if len(pendingAdds) == 0 && len(pendingRemoves) == 0 {
				fmt.Println("no pending validator updates")
				continue
			}
			if len(pendingAdds) > 0 {
				fmt.Printf("pending add: %s\n", strings.Join(pendingAdds, ", "))
			}
			if len(pendingRemoves) > 0 {
				fmt.Printf("pending remove: %s\n", strings.Join(pendingRemoves, ", "))
			}
		case "candidates", "network candidates":
			n.candidateMu.RLock()
			if len(n.candidates) == 0 {
				n.candidateMu.RUnlock()
				fmt.Println("no candidates")
				continue
			}
			// `ids` stores the current position in the related collection.
			ids := make([]string, 0, len(n.candidates))
			// `id` tracks the current position in the related collection.
			for id := range n.candidates {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			fmt.Printf("candidates (%d):\n", len(ids))
			// `id` tracks the current position in the related collection.
			for _, id := range ids {
				// `cand` stores the value produced by this operation.
				cand := n.candidates[id]
				if cand == nil {
					continue
				}
				// `observed` stores the value produced by this operation.
				observed := cand.ObservedEpochs
				// `matched` stores the value produced by this operation.
				matched := cand.MatchedEpochs
				// `dcs` stores the value produced by this operation.
				dcs := 0.0
				// `uptime` stores the value produced by this operation.
				uptime := 0.0
				// `diversityAvg` stores the value produced by this operation.
				diversityAvg := 0.0
				// `gossipTimeliness` stores the value produced by this operation.
				gossipTimeliness := 0.0
				if observed > 0 {
					dcs = float64(matched) / float64(observed)
					diversityAvg = cand.DiversitySum / float64(observed)
				}
				if cand.HeartbeatTotal > 0 {
					uptime = float64(cand.HeartbeatGood) / float64(cand.HeartbeatTotal)
				}
				if cand.GossipTotal > 0 {
					gossipTimeliness = float64(cand.GossipTimely) / float64(cand.GossipTotal)
				}
				// `ban` stores the value produced by this operation.
				ban := "none"
				if cand.PermanentBan {
					ban = "permanent"
				} else if cand.BanUntil > 0 {
					ban = fmt.Sprintf("until %d", cand.BanUntil)
				}
				fmt.Printf("  %s observed=%d matched=%d dcs=%.4f uptime=%.2f%% diversity=%.2f%% gossip=%.2f%% strikes=%d ban=%s promoted=%v\n",
					id,
					observed,
					matched,
					dcs,
					uptime*100,
					diversityAvg*100,
					gossipTimeliness*100,
					cand.Strikes,
					ban,
					cand.Promoted,
				)
			}
			n.candidateMu.RUnlock()
		case "network connect":
			fmt.Print("Enter peer multiaddr (e.g., /ip4/127.0.0.1/tcp/7001/p2p/...): ")
			// `addrInput` stores the address used by this operation.
			addrInput, _ := reader.ReadString('\n')
			addrInput = strings.TrimSpace(addrInput)
			if addrInput == "" {
				fmt.Println("Ã¢ÂÅ’ No address provided")
				continue
			}
			// `ctx` and `cancel` store the context controlling this operation.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// `maddr` and `err` store the error produced by this operation.
			maddr, err := ma.NewMultiaddr(addrInput)
			if err != nil {
				fmt.Printf("Ã¢ÂÅ’ Invalid multiaddr: %v\n", err)
				continue
			}
			// `addrInfo` and `err` store the error produced by this operation.
			addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Printf("Ã¢ÂÅ’ Failed to parse peer info: %v\n", err)
				continue
			}
			fmt.Printf("Attempting to connect to %s...\n", addrInfo.ID)
			// `err` stores the error produced by this operation.
			if err := n.connectToPeer(ctx, addrInput); err != nil {
				fmt.Printf("Ã¢ÂÅ’ Connection failed: %v\n", err)
			} else {
				fmt.Println("Ã¢Å“â€¦ Connected successfully")
			}
		case "back", "exit", "quit":
			fmt.Println("Returning to main CLI...")
			return
		default:
			if line != "" {
				fmt.Println("Ã¢ÂÅ’ Unknown command. Type 'network help' for available commands.")
			}
		}
	}
}

// connectToPeer implements the connect to peer helper.
func (n *Node) connectToPeer(ctx context.Context, peerAddr string) error {
	// Convert string to multiaddr
	maddr, err := ma.NewMultiaddr(peerAddr)
	if err != nil {
		return fmt.Errorf("invalid multiaddr %s: %w", peerAddr, err)
	}
	if !n.allowPeerDiversityDial(maddr) {
		return fmt.Errorf("peer diversity policy blocked dial")
	}
	// Get peer info from multiaddr
	addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		// If it's just an address without peer ID, we can't connect directly
		return fmt.Errorf("multiaddr must include peer ID: %w", err)
	}
	if n.Host != nil && addrInfo.ID == n.Host.ID() {
		return nil
	}
	if !n.allowPeerDiversityOutboundPeer(addrInfo) {
		return fmt.Errorf("peer diversity policy blocked outbound peer")
	}
	if !n.canDialPeerID(addrInfo.ID.String()) {
		return fmt.Errorf("dial backoff active for peer %s", addrInfo.ID)
	}
	if !connectionLimiter.Allow() {
		return fmt.Errorf("connection rate limited")
	}
	if !n.canDialPeer() {
		return fmt.Errorf("outbound peer limit reached")
	}
	// Check if already connected
	if n.hasActivePeerConnection(addrInfo.ID) {
		n.setPeerConnected(addrInfo.ID.String(), true)
		if n.peerConnectedFor(addrInfo.ID.String()) == 0 {
			n.setPeerConnectedAt(addrInfo.ID.String(), time.Now())
		}
		n.recordDialSuccess(addrInfo.ID.String())
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Already connected to peer %s\n", addrInfo.ID)
		}
		return nil
	}
	if n.isPeerConnected(addrInfo.ID.String()) {
		n.clearPeerConnectedAt(addrInfo.ID.String())
		n.setPeerConnected(addrInfo.ID.String(), false)
	}
	if !n.markPeerConnecting(addrInfo.ID.String()) {
		return nil
	}
	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// `err` stores the error produced by this operation.
	if err := n.Host.Connect(connectCtx, *addrInfo); err != nil {
		n.setPeerConnected(addrInfo.ID.String(), false)
		// `errLower` stores the error produced by this operation.
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "peer id mismatch") ||
			strings.Contains(errLower, "dial to self attempted") {
			if n.refreshPeerIDMismatch(peerAddr, addrInfo.ID.String(), err) {
				return fmt.Errorf("peer id refreshed for %s; retrying later", addrInfo.ID)
			}
			n.forgetPeer(addrInfo.ID.String(), "peer_id_mismatch")
		}
		n.recordDialFailure(addrInfo.ID.String())
		if shouldPruneLocalhostDialRefused(n, addrInfo.ID.String(), peerAddr, err) {
			log.Printf("[WARN] pruning localhost peer after %d refused dials: peer=%s addr=%s",
				n.dialFailureCount(addrInfo.ID.String()),
				addrInfo.ID.String(),
				stripP2PComponent(peerAddr),
			)
			n.forgetPeer(addrInfo.ID.String(), "dial_refused_localhost")
		} else if shouldPruneStaleStaticDialFailure(n, addrInfo.ID.String(), peerAddr, err) {
			log.Printf("[PEER-PRUNE] reason=stale_static_dial_failure failures=%d peer=%s addr=%s",
				n.dialFailureCount(addrInfo.ID.String()),
				addrInfo.ID.String(),
				stripP2PComponent(peerAddr),
			)
			n.forgetPeer(addrInfo.ID.String(), "stale_static_dial_failure")
			n.markStaleStaticPeerSuppressed(addrInfo.ID.String())
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	// Tag peer for priority
	if cm := n.Host.ConnManager(); cm != nil {
		cm.TagPeer(addrInfo.ID, "persistent", 100)
	}
	n.protectValidatorMeshPeerID(addrInfo.ID.String())
	// Add to peers list
	n.PeersLibp2p = append(n.PeersLibp2p, addrInfo.ID)
	n.rememberPeerDiversityAddr(addrInfo.ID.String(), addrInfo.Addrs, true)
	n.setPeerConnected(addrInfo.ID.String(), true)
	if n.peerConnectedFor(addrInfo.ID.String()) == 0 {
		n.setPeerConnectedAt(addrInfo.ID.String(), time.Now())
	}
	if DebugNet {
		fmt.Printf("Ã¢Å“â€¦ Connected to peer: %s\n", addrInfo.ID)
	}
	n.recordDialSuccess(addrInfo.ID.String())
	return nil
}

// handlePeerInfoStream handles peer info stream.
func (n *Node) handlePeerInfoStream(s network.Stream) {
	defer s.Close()
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	// `peerID` stores the value produced by this operation.
	peerID := s.Conn().RemotePeer().String()
	// `err` stores the error produced by this operation.
	if err := enc.Encode(n.outboundPeerHelloPreValidation()); err != nil {
		n.recordDialFailure(peerID)
		return
	}
	// `raw` stores the value used by this operation.
	var raw json.RawMessage
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&raw); err == nil {
		// `peerInfo` and `derr` store the error produced by this operation.
		peerInfo, derr := decodePeerHelloPayload(raw)
		if derr != nil {
			n.recordDialFailure(peerID)
			return
		}
		if !peerHelloHasPostValidationFields(peerInfo) {
			n.recordDialFailure(peerID)
			return
		}
		if n.validatePeerHello(peerID, peerInfo) {
			n.applyPeerInfo(peerID, peerInfo)
			n.recordDialSuccess(peerID)
			_ = enc.Encode(n.outboundPeerHello())
		}
	} else {
		n.recordDialFailure(peerID)
	}
}

// handleMessage handles message.
func (n *Node) handleMessage(msg Message, peerAddr string) {
	if n.isPeerQuarantined(peerAddr) {
		if DebugNet {
			fmt.Printf("Ã°Å¸Å¡Â§ Ignoring message from quarantined peer %s\n", peerAddr)
		}
		return
	}
	if strings.TrimSpace(msg.Type) == "" {
		n.recordPeerSecurityFault(peerAddr, "empty_message_type")
		return
	}
	if !n.allowPeerResource(peerAddr, msg.Type, len(msg.Data)) {
		return
	}
	if !checkRateLimitForPeer(peerAddr, msg.Type, n.isValidatorOrPersistentPeerID(peerAddr)) {
		n.recordPeerRateLimitDrop(peerAddr, msg.Type)
		return
	}
	if msg.Type != MsgPeerHello && !n.isPeerHelloOK(peerAddr) {
		if DebugNet {
			if msg.Type == MsgPeers {
				fmt.Printf("Skipping pre-hello peers list from %s\n", peerAddr)
			} else {
				fmt.Printf("WARNING: ignoring message from unverified peer %s type=%s\n", peerAddr, msg.Type)
			}
		}
		if n.Host != nil {
			// `pid` and `err` store the error produced by this operation.
			if pid, err := peer.Decode(peerAddr); err == nil {
				go n.sendPeerHello(pid)
			}
		}
		return
	}
	if n.isPeerSyncOnly(peerAddr) && !isSyncOnlyAllowedMsgType(msg.Type) {
		if (DebugNet || DebugSync || DebugConsensus) && n.shouldLogSyncOnlyDrop(peerAddr, msg.Type) {
			fmt.Printf("[DRIFT-SYNC-ONLY] dropped message type=%s from %s\n", msg.Type, peerAddr)
		}
		return
	}
	// =====================================================
	// Ã°Å¸Å½Â¯ MESSAGE TYPE ROUTING
	// =====================================================
	switch msg.Type {
	// =================================================
	// Ã°Å¸â€œÂ¦ BLOCK PROPOSAL
	// =================================================
	/* legacy consensus removed: proposals/votes
	case MsgProposeBlock:
		var block Block
		if err := json.Unmarshal(msg.Data, &block); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal block proposal from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€œÂ¦ Processing block proposal from %s | height=%d\n",
				peerAddr, block.ID)
		}
		n.HandleProposal(block)
	// =================================================
	// Ã°Å¸â€”Â³Ã¯Â¸Â BLOCK VOTE
	// =================================================
	case MsgBlockVote:
		var vote BlockVote
		if err := json.Unmarshal(msg.Data, &vote); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal vote from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€”Â³Ã¯Â¸Â Processing vote from %s | height=%d validator=%s\n",
				peerAddr, vote.Height, ShortID(vote.Validator))
		}
	// =================================================
	// Ã°Å¸Â¤Â PEER HELLO (VALIDATOR DISCOVERY)
	// =================================================
	*/
	case MsgPeerHello:
		// `hello` stores the value used by this operation.
		var hello PeerHello
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &hello); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal peer hello from %s: %v\n", peerAddr, err)
			}
			return
		}
		if !n.validatePeerHello(peerAddr, hello) {
			return
		}
		n.HandlePeerHello(peerAddr, hello.ValidatorID, hello.P2PAddr)
		n.applyPeerInfo(peerAddr, hello)
	// =================================================
	// Ã°Å¸â€œÂ¦ LEADER BLOCK (REAL-TIME PROPOSAL)
	// =================================================
	case MsgLeaderBlock:
		// `block` stores the synchronization state protecting shared data.
		var block Block
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &block); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal leader block from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€œÂ¦ Leader block received from %s | height=%d proposer=%s\n",
				peerAddr, block.ID, ShortID(block.Proposer))
		}
		_ = n.submitLeaderBlockOnConsensusLane(block, peerAddr)
	// =================================================
	// Ã°Å¸â€ºÂ¡Ã¯Â¸Â CENSORSHIP EVIDENCE
	// =================================================
	case MsgCensorshipEvidence:
		// `evidence` stores the value used by this operation.
		var evidence CensorshipEvidence
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &evidence); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal censorship evidence from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€ºÂ¡Ã¯Â¸Â Processing censorship evidence from %s | height=%d\n",
				peerAddr, evidence.Height)
		}
		// Load the block and apply evidence if available
		if block, ok := n.LoadBlock(int(evidence.Height)); ok {
			if ApplyCensorshipEvidence(n, evidence, block) {
				CheckCensorshipSlashing(evidence.Leader, int(evidence.Height))
			}
		}
	// =================================================
	// Ã°Å¸â€™Â¸ TRANSACTION
	// =================================================
	case MsgTx:
		// `tx` stores the transaction data handled by this operation.
		var tx Transaction
		// `err` stores the error produced by this operation.
		if err := UnmarshalTransactionWire(msg.Data, &tx); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal transaction from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€™Â¸ Processing transaction from %s | id=%s\n",
				peerAddr, ShortID(tx.ID))
		}
		n.ReceiveTransaction(tx)
	// =================================================
	// Ã°Å¸â€œÂ¦ BLOCK (FINALIZED)
	// =================================================
	case MsgFinalBlock:
		// `block` stores the synchronization state protecting shared data.
		var block Block
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &block); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal final block from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€œÂ¦ Processing final block from %s | height=%d\n",
				peerAddr, block.ID)
		}
		_ = n.submitFinalBlockOnConsensusLane(block)
	// =================================================
	// Ã¢Å“â€¦ EXECUTION RESULT (CONSENSUS QUORUM)
	// =================================================
	case MsgExecutionResult:
		// `res` stores the result produced by this operation.
		var res ExecutionResultMsg
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &res); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal execution result from %s: %v\n", peerAddr, err)
			}
			return
		}
		// `allowed` and `reason` store whether the related condition is satisfied.
		if allowed, reason := n.allowExecutionVoteNetworkIngress(res); !allowed {
			if !benignExecutionVoteIngressReason(reason) {
				n.logExecutionVoteIngressDrop(reason, res, "stream:"+peerAddr)
			}
			return
		}
		_ = n.submitExecutionResultOnConsensusLane(res, true)
	// =================================================
	// Ã¢Å“â€¦ COMMIT (FINALITY)
	// =================================================
	case MsgCommit:
		// `cm` stores the value used by this operation.
		var cm CommitMsg
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &cm); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal commit from %s: %v\n", peerAddr, err)
			}
			return
		}
		_ = n.submitCommitMsgOnConsensusLane(cm)
	// =================================================
	// Ã¢Å“â€¦ BLOCK ACK (SYNC CONFIRMATION)
	// =================================================
	case MsgBlockAck:
		// `ack` stores the value used by this operation.
		var ack BlockAck
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &ack); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal block ack from %s: %v\n", peerAddr, err)
			}
			return
		}
		n.recordPeerAck(peerAddr, ack.Height)
	// =================================================
	// Ã°Å¸â€â€ž BLOCK SYNC REQUEST
	// =================================================
	case MsgGetBlocks:
		// `req` stores the request data being processed.
		var req BlockRequest
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal block request from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugSync {
			fmt.Printf("Ã°Å¸â€â€ž Block sync request from %s | %d-%d\n",
				peerAddr, req.From, req.To)
		}
		if req.To < req.From {
			if DebugNet {
				fmt.Printf("invalid block request from %s: from=%d to=%d\n", peerAddr, req.From, req.To)
			}
			return
		}
		// `maxInt` stores the value produced by this operation.
		maxInt := uint64(^uint(0) >> 1)
		if req.From > maxInt || req.To > maxInt {
			if DebugNet {
				fmt.Printf("block request overflow from %s: from=%d to=%d\n", peerAddr, req.From, req.To)
			}
			return
		}
		// `maxBlocks` stores the value produced by this operation.
		maxBlocks := syncBlockRequestMaxBlocks(0)
		if maxBlocks == 0 {
			maxBlocks = 512
		}
		if req.To-req.From > maxBlocks {
			n.recordPeerRateLimitDrop(peerAddr, "block_request_range")
			req.To = req.From + maxBlocks
		}
		select {
		case blockRequestServeSem <- struct{}{}:
			go func(from, to uint64) {
				defer func() { <-blockRequestServeSem }()
				n.sendBlocksToPeer(peerAddr, int(from), int(to))
			}(req.From, req.To)
		default:
			n.recordPeerRateLimitDrop(peerAddr, "block_request_concurrency")
			if DebugNet || DebugSync {
				fmt.Printf("[RATE-LIMIT] block request concurrency full peer=%s range=%d-%d\n", peerAddr, req.From, req.To)
			}
		}
	case MsgBlocksBatch:
		// `resp` stores the response produced by this operation.
		var resp BlockResponse
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			if DebugNet {
				fmt.Printf("failed to unmarshal block batch from %s: %v\n", peerAddr, err)
			}
			return
		}
		if len(resp.Blocks) == 0 {
			return
		}
		sort.Slice(resp.Blocks, func(i, j int) bool {
			return resp.Blocks[i].ID < resp.Blocks[j].ID
		})
		_ = n.submitBlockBatchOnConsensusLane(resp.Blocks)
		if DebugSync {
			// `first` stores the value produced by this operation.
			first := resp.Blocks[0].ID
			// `last` stores the value produced by this operation.
			last := resp.Blocks[len(resp.Blocks)-1].ID
			fmt.Printf("applied block batch from %s: %d-%d (%d blocks)\n",
				peerAddr, first, last, len(resp.Blocks))
		}
	// =================================================
	// Ã°Å¸â€â€ž PEERS LIST EXCHANGE
	// =================================================
	case MsgPeers:
		// `peers` stores the value used by this operation.
		var peers []string
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &peers); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal peers list from %s: %v\n", peerAddr, err)
			}
			return
		}
		if DebugNet {
			fmt.Printf("Ã°Å¸â€â€” Received %d peers from %s\n", len(peers), peerAddr)
		}
		n.handlePeersList(peerAddr, peers)
	// =================================================
	// Ã°Å¸Ââ€œ PING (KEEPALIVE)
	// =================================================
	case MsgPing:
		if DebugNet {
			fmt.Printf("Ã°Å¸Ââ€œ Ping from %s\n", peerAddr)
		}
		// `pid` and `err` store the error produced by this operation.
		if pid, err := peer.Decode(peerAddr); err == nil {
			go n.sendConsensusMessage(pid, Message{
				Type: MsgPong,
				Data: []byte(`{"pong":true}`),
			})
		}
	case MsgPong:
		if DebugNet {
			fmt.Printf("pong from %s\n", peerAddr)
		}
	// =================================================
	// Ã°Å¸Å¡Â¨ UNKNOWN MESSAGE TYPE
	// =================================================
	default:
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Unknown message type from %s: %s\n", peerAddr, msg.Type)
		}
	}
}

// addBootstrapAddr implements the add bootstrap addr helper.
func (n *Node) addBootstrapAddr(maddr ma.Multiaddr) {
	// This function would add addresses to DHT for bootstrap
	// In practice, DHT bootstrap addresses should be set during DHT creation
	if DebugNet {
		fmt.Printf("Ã¢Å¾â€¢ Would add bootstrap address: %s (but DHT bootstrap addresses should be set during initialization)\n", maddr)
	}
}

// Helper function to create a proper multiaddr from host:port
func createMultiaddrFromHostPort(host string, port int) string {
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("/ip4/%s/tcp/%d", host, port)
}

// verifyCensorshipEvidenceObserver verifies censorship evidence observer.
func verifyCensorshipEvidenceObserver(ev CensorshipEvidence) bool {
	// `observer` stores the value produced by this operation.
	observer := normalizeValidatorID(ev.Observer)
	if observer == "" {
		return false
	}
	if len(ev.ObserverSig) != ed25519.SignatureSize {
		return false
	}
	validatorPubKeysMu.RLock()
	// `pubKey` and `ok` store whether the related condition is satisfied.
	pubKey, ok := ValidatorPubKeys[observer]
	validatorPubKeysMu.RUnlock()
	if !ok || len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pubKey, censorshipEvidenceSignBytes(ev), ev.ObserverSig)
}

// ApplyCensorshipEvidence applies censorship evidence.
func ApplyCensorshipEvidence(
	n *Node,
	ev CensorshipEvidence,
	block Block,
) bool {
	CensorshipMu.Lock()
	defer CensorshipMu.Unlock()
	if ev.Leader == "" || ev.TxID == "" {
		return false
	}
	if ev.Height != block.ID {
		return false
	}
	ev.Leader = normalizeValidatorID(ev.Leader)
	ev.Observer = normalizeValidatorID(ev.Observer)
	if ev.Observer == "" || ev.Observer == ev.Leader {
		return false
	}
	if ev.ObservedAt == 0 {
		ev.ObservedAt = block.ID
	}
	if ev.MempoolRoot == "" {
		ev.MempoolRoot = block.MempoolRoot
	}
	if n != nil && !n.isValidatorInSetForHeight(ev.Observer, ev.Height) {
		return false
	}
	if !verifyCensorshipEvidenceObserver(ev) {
		return false
	}
	// `key` stores the key used to access the related value.
	key := EvidenceKey{
		Leader: ev.Leader,
		Height: ev.Height,
	}
	// `list` stores the value produced by this operation.
	list := CensorshipEvidencePool[key]
	if len(list) >= MaxEvidencePerHeight {
		return false
	}
	// `existing` tracks the current values while iterating.
	for _, existing := range list {
		if existing.TxID == ev.TxID && normalizeValidatorID(existing.Observer) == ev.Observer {
			return false
		}
	}
	CensorshipEvidencePool[key] = append(list, ev)
	if DebugConsensus {
		fmt.Printf(
			"Censorship evidence applied | leader=%s tx=%s height=%d\n",
			ShortID(ev.Leader),
			ev.TxID,
			ev.Height,
		)
	}
	return true
}

// You might also want this function to help users create proper multiaddrs
func (n *Node) GetPublicMultiaddr() string {
	if n.Host == nil || len(n.Host.Addrs()) == 0 {
		return ""
	}
	// Return the first address with peer ID
	for _, addr := range n.Host.Addrs() {
		// `fullAddr` stores the address used by this operation.
		fullAddr := fmt.Sprintf("%s/p2p/%s", addr, n.Host.ID())
		// Return a loopback address if available
		if strings.Contains(addr.String(), "127.0.0.1") {
			return fullAddr
		}
	}
	// Otherwise return any address
	return fmt.Sprintf("%s/p2p/%s", n.Host.Addrs()[0], n.Host.ID())
}

// selectAdvertisedHostAddr implements the select advertised host addr helper.
func selectAdvertisedHostAddr(addrs []ma.Multiaddr) ma.Multiaddr {
	if len(addrs) == 0 {
		return nil
	}
	// `addr` tracks the address used by this operation.
	for _, addr := range addrs {
		if !multiaddrHasLoopbackOrUnspecifiedIP(addr) {
			return addr
		}
	}
	return addrs[0]
}

// multiaddrHasLoopbackOrUnspecifiedIP implements the multiaddr has loopback or unspecified ip helper.
func multiaddrHasLoopbackOrUnspecifiedIP(addr ma.Multiaddr) bool {
	if addr == nil {
		return true
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := addr.ValueForProtocol(ma.P_IP4); err == nil {
		// `ip` stores the current position in the related collection.
		ip := net.ParseIP(raw)
		return ip == nil || ip.IsLoopback() || ip.IsUnspecified()
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := addr.ValueForProtocol(ma.P_IP6); err == nil {
		// `ip` stores the current position in the related collection.
		ip := net.ParseIP(raw)
		return ip == nil || ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

// Helper function for public IP detection
func GetPublicIP() (string, error) {
	// `resp` and `err` store the error produced by this operation.
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// `ip` and `err` store the error produced by this operation.
	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(ip), nil
}

// snapshotChunkHash implements the snapshot chunk hash helper.
func snapshotChunkHash(data []byte) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// latestSnapshotMetaForSyncRequest implements the latest snapshot meta for sync request helper.
func (n *Node) latestSnapshotMetaForSyncRequest() *StateSnapshot {
	if n == nil {
		return nil
	}
	// `snapshot` stores the value produced by this operation.
	if snapshot := n.publishedValidatorSnapshotForSyncRequest(0); snapshot != nil {
		return snapshot
	}
	// `snapshot` and `err` store the error produced by this operation.
	if snapshot, err := n.verifiedStoredSnapshotAtOrBelow(0); err == nil && snapshot != nil {
		return snapshot
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if tip == 0 {
		return nil
	}
	// `snapshot` and `ok` store whether the related condition is satisfied.
	if snapshot, _, _, ok := n.ResolveCommittedStateSnapshot(tip); ok && snapshot != nil && n.snapshotMatchesLocalAnchor(snapshot) {
		return snapshot
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.Blockchain.GetBlock(tip)
	if !ok {
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := n.CreateSnapshot(tip, block.BlockHash); err != nil {
		return n.waitForSnapshotAtHeightForSync(tip)
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(tip)
	if err != nil || snapshot == nil || !n.snapshotMatchesLocalAnchor(snapshot) {
		// `snapshot` stores the value produced by this operation.
		if snapshot := n.waitForSnapshotAtHeightForSync(tip); snapshot != nil {
			return snapshot
		}
		_ = n.deleteStoredSnapshotHeight(tip)
		_ = n.refreshLatestSnapshotPointer()
		return nil
	}
	return snapshot
}

// waitForSnapshotAtHeightForSync implements the wait for snapshot at height for sync helper.
func (n *Node) waitForSnapshotAtHeightForSync(height uint64) *StateSnapshot {
	if n == nil || height == 0 {
		return nil
	}
	// `attempt` stores the value produced by this operation.
	for attempt := 0; attempt < snapshotServeRetryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(snapshotServeRetryBackoff)
		}
		// `snapshot` stores the value produced by this operation.
		if snapshot := n.publishedValidatorSnapshotForSyncRequest(height); snapshot != nil && snapshot.Height == height {
			return snapshot
		}
		// `snapshot` and `ok` store whether the related condition is satisfied.
		if snapshot, _, _, ok := n.ResolveCommittedStateSnapshot(height); ok && snapshot != nil && snapshot.Height == height && n.snapshotMatchesLocalAnchor(snapshot) {
			return snapshot
		}
		// `snapshot` and `err` store the error produced by this operation.
		snapshot, err := n.GetSnapshot(height)
		if err == nil && snapshot != nil && snapshot.Height == height && n.snapshotMatchesLocalAnchor(snapshot) {
			return snapshot
		}
	}
	return nil
}

// snapshotForSyncRequest implements the snapshot for sync request helper.
func (n *Node) snapshotForSyncRequest(targetHeight uint64) *StateSnapshot {
	if n == nil {
		return nil
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if targetHeight > 0 {
	} else {
		if tip > 50 {
			targetHeight = tip - 50
		}
	}
	if targetHeight > tip && tip > 0 {
		targetHeight = tip
	}
	// `snapshot` stores the value produced by this operation.
	if snapshot := n.publishedValidatorSnapshotForSyncRequest(targetHeight); snapshot != nil {
		return snapshot
	}
	// `snapshot` and `err` store the error produced by this operation.
	if snapshot, err := n.verifiedStoredSnapshotAtOrBelow(targetHeight); err == nil && snapshot != nil {
		return snapshot
	}
	if tip == 0 || targetHeight == 0 || targetHeight != tip {
		return nil
	}
	// `snapshot` and `ok` store whether the related condition is satisfied.
	if snapshot, _, _, ok := n.ResolveCommittedStateSnapshot(tip); ok && snapshot != nil && n.snapshotMatchesLocalAnchor(snapshot) {
		return snapshot
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.Blockchain.GetBlock(tip)
	if !ok {
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := n.CreateSnapshot(tip, block.BlockHash); err != nil {
		return n.waitForSnapshotAtHeightForSync(tip)
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(tip)
	if err != nil || snapshot == nil || !n.snapshotMatchesLocalAnchor(snapshot) {
		// `snapshot` stores the value produced by this operation.
		if snapshot := n.waitForSnapshotAtHeightForSync(tip); snapshot != nil {
			return snapshot
		}
		_ = n.deleteStoredSnapshotHeight(tip)
		_ = n.refreshLatestSnapshotPointer()
		return nil
	}
	return snapshot
}

// handleBlockStream handles block stream.
func (n *Node) handleBlockStream(s network.Stream) {
	defer s.Close()
	// `requestStarted` stores the request data being processed.
	requestStarted := time.Now()
	// =====================================================
	// Ã¢ÂÂ±Ã¯Â¸Â SET TIMEOUTS FOR NETWORK OPERATIONS
	// =====================================================
	deadline := time.Now().Add(30 * time.Second)
	s.SetDeadline(deadline)
	s.SetReadDeadline(deadline)
	s.SetWriteDeadline(deadline)
	// =====================================================
	// Ã°Å¸â€œÂ¥ RECEIVE BLOCK REQUEST
	// =====================================================
	dec := json.NewDecoder(s)
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)
	// `req` stores the request data being processed.
	var req BlockRequest
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&req); err != nil {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=decode err=%v\n",
			ShortID(s.Conn().RemotePeer().String()), err)
		return
	}
	// `origFrom` stores the value produced by this operation.
	origFrom := req.From
	// `origTo` stores the value produced by this operation.
	origTo := req.To
	fmt.Printf("[SYNC-SERVE-REQUEST] peer=%s requested=%d-%d snapshot=%t snapshot_height=%d\n",
		ShortID(s.Conn().RemotePeer().String()), req.From, req.To, req.WantSnapshot, req.SnapshotHeight)
	if req.From == 0 || req.To < req.From {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=validate requested=%d-%d reason=invalid_range\n",
			ShortID(s.Conn().RemotePeer().String()), req.From, req.To)
		return
	}
	// `peerID` stores the value produced by this operation.
	peerID := s.Conn().RemotePeer().String()
	// `ackHeight` stores the value produced by this operation.
	if ackHeight := n.peerAckHeightFor(peerID); !req.BypassAck && ackHeight > 0 {
		if ackHeight >= req.From {
			req.From = ackHeight + 1
		}
		if req.From > req.To && !req.WantSnapshot {
			fmt.Printf("[SYNC-SERVE-SKIP] peer=%s requested=%d-%d adjusted=%d-%d reason=ack_ahead\n",
				ShortID(peerID), origFrom, origTo, req.From, req.To)
			_ = enc.Encode(BlockResponse{})
			return
		}
	}
	// `blockRangeEmpty` stores the block data handled by this operation.
	blockRangeEmpty := req.From > req.To
	if !blockRangeEmpty {
		req.To = clampBlockSyncRangeToServeLimit(req.From, req.To, 0)
	}
	if req.From != origFrom || req.To != origTo {
		fmt.Printf("[SYNC-SERVE-ADJUST] peer=%s requested=%d-%d adjusted=%d-%d\n",
			ShortID(peerID), origFrom, origTo, req.From, req.To)
	}
	if !blockRangeEmpty && !n.shouldServeBlockRange(peerID, req.From, req.To, req.WantSnapshot) {
		fmt.Printf("[SYNC-SERVE-SKIP] peer=%s requested=%d-%d adjusted=%d-%d reason=duplicate_or_policy\n",
			ShortID(peerID), origFrom, origTo, req.From, req.To)
		_ = enc.Encode(BlockResponse{})
		return
	}
	// =====================================================
	// Ã°Å¸â€œÂ¦ SNAPSHOT (OPTIONAL)
	// =====================================================
	var snapshot *StateSnapshot
	// `start` stores the value produced by this operation.
	start := req.From
	if req.WantSnapshot {
		snapshot = n.snapshotForSyncRequest(req.SnapshotHeight)
		if snapshot != nil {
			// Prevent snapshot resend storms to the same peer at the same height.
			if !n.shouldSendSnapshotToPeer(peerID, snapshot.Height) {
				snapshot = nil
			} else if snapshot.Height >= start {
				start = snapshot.Height + 1
			}
		}
	}
	// =====================================================
	// Ã°Å¸â€œÂ¦ FETCH REQUESTED BLOCKS
	// =====================================================
	var blocks []Block
	// `fetchedCount` stores the measured quantity used by this operation.
	fetchedCount := 0
	// `currentTip` stores the value produced by this operation.
	currentTip := n.Blockchain.Height()
	// `finalizedHeight` stores the value produced by this operation.
	finalizedHeight := n.getFinalizedHeight()
	if finalizedHeight == 0 {
		finalizedHeight = currentTip
	}
	// `tipGapOnly` stores the value produced by this operation.
	tipGapOnly := false
	// `historicalGapOnly` stores the value produced by this operation.
	historicalGapOnly := false
	type finalizedDriftAggregate struct {
		// `count` stores the measured quantity used by this operation.
		count int
		// `from` stores the value associated with this record.
		from uint64
		// `to` stores the value associated with this record.
		to uint64
		// `expected` stores the value associated with this record.
		expected string
		// `got` stores the value associated with this record.
		got string
	}
	// `finalizedDrifts` stores the value produced by this operation.
	finalizedDrifts := make(map[string]finalizedDriftAggregate)
	if blockRangeEmpty {
		// The peer already acknowledged this whole block range. Snapshot payloads
		// may still be useful, but do not synthesize a new block range.
	} else if start > currentTip {
		// Peer is asking for blocks beyond our current tip; return snapshot/empty response quietly.
		tipGapOnly = true
	} else {
		// `h` stores the value produced by this operation.
		for h := start; h <= req.To; h++ {
			// Avoid noisy "not found" logs for blocks we simply don't have yet.
			if h > currentTip {
				tipGapOnly = true
				break
			}
			// `b` and `ok` store whether the related condition is satisfied.
			if b, ok := n.LoadBlock(int(h)); ok {
				// For non-finalized heights, keep strict validator-set consistency.
				// For finalized history, serve canonical blocks even if expected-set
				// view drifted due repair/snapshot recomputation.
				if b.ValidatorSetHash != "" {
					// `expected` stores the value produced by this operation.
					if expected := n.expectedValidatorSetHash(b.ID); expected != "" && expected != b.ValidatorSetHash {
						if b.ID > finalizedHeight {
							historicalGapOnly = true
							if DebugSync || DebugConsensus {
								fmt.Printf("Refusing non-finalized inconsistent block %d for %s (expected_set=%s got=%s)\n",
									b.ID, s.Conn().RemotePeer(), ShortHash(expected), ShortHash(b.ValidatorSetHash))
							}
							break
						}
						// `tupleKey` stores the key used to access the related value.
						tupleKey := expected + "|" + b.ValidatorSetHash
						// `agg` stores the value produced by this operation.
						agg := finalizedDrifts[tupleKey]
						agg.count++
						if agg.from == 0 || b.ID < agg.from {
							agg.from = b.ID
						}
						if b.ID > agg.to {
							agg.to = b.ID
						}
						agg.expected = expected
						agg.got = b.ValidatorSetHash
						finalizedDrifts[tupleKey] = agg
					}
				}
				blocks = append(blocks, b)
				fetchedCount++
			} else {
				// Node may be anchored from a snapshot at/above tip without full historical blocks.
				// Treat this as a quiet historical gap instead of noisy warning spam.
				historicalGapOnly = true
				if DebugSync {
					fmt.Printf("Ã¢ÂÂ³ Historical block gap at %d for %s (tip=%d)\n",
						h, s.Conn().RemotePeer(), currentTip)
				}
				break
			}
			// Limit batch size for memory/network efficiency
			if fetchedCount >= int(syncBlockRequestMaxBlocks(0)) {
				if DebugNet && len(blocks) > 0 {
					// `fromHeight` stores the value produced by this operation.
					fromHeight := blocks[0].ID
					// `toHeight` stores the value produced by this operation.
					toHeight := blocks[len(blocks)-1].ID
					if n.shouldLogPartialBatch(peerID, fromHeight, toHeight) {
						fmt.Printf("Partial batch: %d blocks to %s (range %d-%d)\n",
							fetchedCount, s.Conn().RemotePeer(), fromHeight, toHeight)
					}
				}
				break
			}
		}
	}
	// =====================================================
	// Ã°Å¸â€œÂ¤ SEND BLOCK RESPONSE
	// =====================================================
	if len(finalizedDrifts) > 0 {
		// `keys` stores the key used to access the related value.
		keys := make([]string, 0, len(finalizedDrifts))
		// `key` tracks the key used to access the related value.
		for key := range finalizedDrifts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		// `key` tracks the key used to access the related value.
		for _, key := range keys {
			// `agg` stores the value produced by this operation.
			agg := finalizedDrifts[key]
			// `state` stores the value produced by this operation.
			state := n.recordFinalizedDrift(peerID, agg.from, agg.to, agg.expected, agg.got, agg.count)
			if (DebugSync || DebugConsensus) && n.shouldLogFinalizedDrift(peerID, agg.from, agg.to) {
				fmt.Printf("Serving finalized blocks %d-%d to %s despite expected-set drift (expected_set=%s got=%s count=%d)\n",
					agg.from, agg.to, s.Conn().RemotePeer(), ShortHash(agg.expected), ShortHash(agg.got), state.Count)
			}
			n.applyFinalizedDriftPolicy(peerID, state)
		}
	}
	// `execPoolSnap` stores the value used by this operation.
	var execPoolSnap *ExecPoolSnapshot
	if ResultGossipOnly && req.WantSnapshot {
		// `epoch` stores the value produced by this operation.
		epoch := n.currentEpoch()
		execPoolSnap = buildExecPoolSnapshot(epoch, n.currentProposalVoteKey(epoch))
	}
	// `resp` stores the response produced by this operation.
	resp := BlockResponse{Blocks: blocks, Snapshot: snapshot, ExecPool: execPoolSnap}
	// `err` stores the error produced by this operation.
	if err := enc.Encode(resp); err != nil {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=encode requested=%d-%d count=%d err=%v\n",
			ShortID(peerID), req.From, req.To, len(blocks), err)
		return
	}
	// `err` stores the error produced by this operation.
	if err := s.CloseWrite(); err != nil {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=close_write requested=%d-%d count=%d err=%v\n",
			ShortID(peerID), req.From, req.To, len(blocks), err)
	}
	// `servedFrom` stores the value produced by this operation.
	servedFrom := uint64(0)
	// `servedTo` stores the value produced by this operation.
	servedTo := uint64(0)
	if len(blocks) > 0 {
		servedFrom = blocks[0].ID
		servedTo = blocks[len(blocks)-1].ID
	}
	fmt.Printf("[SYNC-SERVE-RESPONSE] peer=%s requested=%d-%d served=%d-%d count=%d snapshot=%t tip_gap=%t historical_gap=%t duration_ms=%d\n",
		ShortID(peerID), req.From, req.To, servedFrom, servedTo, len(blocks), snapshot != nil, tipGapOnly, historicalGapOnly, time.Since(requestStarted).Milliseconds())
	// =====================================================
	// Ã°Å¸â€œÅ  LOGGING AND METRICS
	// =====================================================
	if DebugNet {
		if snapshot != nil {
			fmt.Printf("Ã°Å¸â€œÂ¦ Sent snapshot height=%d to %s\n",
				snapshot.Height, s.Conn().RemotePeer())
		}
		if len(blocks) > 0 {
			// `fromHeight` stores the value produced by this operation.
			fromHeight := blocks[0].ID
			// `toHeight` stores the value produced by this operation.
			toHeight := blocks[len(blocks)-1].ID
			if n.shouldLogSentBlocks(peerID, fromHeight, toHeight) {
				fmt.Printf("Ã°Å¸â€œÂ¦ Sent blocks %d-%d (%d blocks) to %s\n",
					fromHeight, toHeight, len(blocks), s.Conn().RemotePeer())
			}
		} else {
			if tipGapOnly || historicalGapOnly {
				if DebugSync {
					if tipGapOnly {
						fmt.Printf("Ã¢ÂÂ³ Peer %s requested future range %d-%d (local tip=%d)\n",
							s.Conn().RemotePeer(), req.From, req.To, currentTip)
					} else {
						fmt.Printf("Ã¢ÂÂ³ Peer %s requested unavailable historical range %d-%d (local tip=%d)\n",
							s.Conn().RemotePeer(), req.From, req.To, currentTip)
					}
				}
			} else {
				if n.shouldLogNoBlocks(peerID, req.From, req.To) {
					fmt.Printf("Ã°Å¸â€œÂ­ No blocks sent to %s (requested %d-%d)\n",
						s.Conn().RemotePeer(), req.From, req.To)
				}
			}
		}
	}
	// Update sync metrics
	if len(blocks) > 0 {
		// Record successful sync
		lastBlock := blocks[len(blocks)-1]
		if DebugSync {
			fmt.Printf("Ã°Å¸â€â€ž Sync stats: Sent %d blocks (height %d-%d) to %s\n",
				len(blocks), blocks[0].ID, lastBlock.ID, s.Conn().RemotePeer())
		}
	}
}

// handleSnapshotMetaStream handles snapshot meta stream.
func (n *Node) handleSnapshotMetaStream(s network.Stream) {
	defer s.Close()
	// `timeout` stores the result produced by this operation.
	timeout := syncPeerRequestTimeout()
	_ = s.SetDeadline(time.Now().Add(timeout))
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)

	// `req` stores the request data being processed.
	var req SnapshotMetaRequest
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&req); err != nil {
		return
	}
	// `snapshot`, `manifest`, and `err` store the error produced by this operation.
	snapshot, manifest, _, _, err := n.snapshotManifestForTransfer(req.Height)
	if err != nil || snapshot == nil || manifest == nil {
		_ = enc.Encode(SnapshotMetaResponse{Available: false})
		return
	}
	// `chunkSize` stores the measured quantity used by this operation.
	chunkSize := syncSnapshotChunkSizeBytes()
	if chunkSize == 0 {
		chunkSize = 1024 * 1024
	}
	// `totalChunks` stores the measured quantity used by this operation.
	totalChunks := manifest.ChunkCount
	if totalChunks == 0 {
		totalChunks = 1
	}
	// `resp` stores the response produced by this operation.
	resp := SnapshotMetaResponse{
		Height:                snapshot.Height,
		SnapshotHash:          snapshot.SnapshotHash,
		StateRoot:             snapshot.StateRoot,
		StateMerkleRoot:       snapshot.StateMerkleRoot,
		ValidatorSetHash:      snapshot.ValidatorSetHash,
		ValidatorSetRoot:      snapshotValidatorSetRoot(snapshot),
		ValidatorRegistryHash: snapshot.ValidatorRegistryHash,
		FinalizedHeight:       snapshot.FinalizedHeight,
		FinalizedHash:         strings.TrimSpace(snapshot.FinalizedHash),
		EpochAnchorHash:       strings.TrimSpace(snapshot.EpochAnchorHash),
		FinalityRoot:          strings.TrimSpace(snapshot.FinalityRoot),
		Encoding:              "binary",
		Compression:           syncSnapshotCompression(),
		ChunkSize:             chunkSize,
		TotalChunks:           totalChunks,
		CheckpointProof:       snapshot.CheckpointProof,
		Available:             true,
	}
	resp.Manifest = manifest
	resp.ChunkHashes = append([]string{}, manifest.ChunkHashes...)
	resp.StateRoot = manifest.StateRoot
	resp.StateMerkleRoot = manifest.StateMerkleRoot
	resp.ValidatorSetHash = manifest.ValidatorSetHash
	resp.ValidatorSetRoot = manifest.ValidatorSetRoot
	resp.ValidatorRegistryHash = manifest.ValidatorRegistryHash
	resp.FinalizedHeight = manifest.FinalizedHeight
	resp.FinalizedHash = manifest.FinalizedHash
	resp.EpochAnchorHash = manifest.EpochAnchorHash
	resp.FinalityRoot = manifest.FinalityRoot
	resp.Encoding = manifest.Encoding
	resp.Compression = manifest.Compression
	_ = enc.Encode(resp)
}

// handleSnapshotChunkStream handles snapshot chunk stream.
func (n *Node) handleSnapshotChunkStream(s network.Stream) {
	defer s.Close()
	// `timeout` stores the result produced by this operation.
	timeout := syncPeerRequestTimeout()
	_ = s.SetDeadline(time.Now().Add(timeout))
	// `dec` stores the value produced by this operation.
	dec := json.NewDecoder(s)
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(s)

	// `req` stores the request data being processed.
	var req SnapshotChunkRequest
	// `err` stores the error produced by this operation.
	if err := dec.Decode(&req); err != nil {
		return
	}
	// `resp` and `err` store the error produced by this operation.
	resp, _, _, _, err := n.snapshotChunkForTransfer(req.Height, req.Index)
	if err != nil || resp == nil {
		_ = enc.Encode(SnapshotChunkResponse{})
		return
	}
	_ = enc.Encode(resp)
}

// ed25519ToLibp2pKey implements the ed25519 to libp2p key helper.
func ed25519ToLibp2pKey(priv ed25519.PrivateKey) (libp2pcrypto.PrivKey, error) {
	return libp2pcrypto.UnmarshalEd25519PrivateKey(priv)
}

// sendToPeerLibp2p implements the send to peer libp2p helper.
func (n *Node) sendToPeerLibp2p(ctx context.Context, peerID peer.ID, msg Message) error {
	// `s` and `err` store the error produced by this operation.
	s, err := n.openStream(ctx, peerID, "/msc/1.0.0")
	if err != nil {
		n.recordDialFailure(peerID.String())
		// `errMsg` stores the error produced by this operation.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(peerID.String(), "consensus_protocol_mismatch")
		}
		return err
	}
	defer s.Close()
	// `data` stores the value produced by this operation.
	data, _ := json.Marshal(msg)
	_, err = s.Write(append(data, '\n'))
	return err
}

// libp2pKeyFromEd25519 implements the libp2p key from ed25519 helper.
func libp2pKeyFromEd25519(priv ed25519.PrivateKey) (lpcrypto.PrivKey, error) {
	return lpcrypto.UnmarshalEd25519PrivateKey(priv)
}

/*
legacy consensus removed

	// subscribeProposals implements the subscribe proposals helper.
	func (n *Node) subscribeProposals(ctx context.Context) {
			sub, err := n.TopicProposal.Subscribe()
			if err != nil {
				panic(err)
			}
			go func() {
				for {
					msg, err := sub.Next(ctx)
					if err != nil {
						return
					}
					var m Message
					if err := json.Unmarshal(msg.Data, &m); err != nil {
						continue
					}
					var block Block
					if err := json.Unmarshal(m.Data, &block); err != nil {
						continue
					}
					if DebugConsensus {
						fmt.Printf(
							"Ã°Å¸â€œÂ¦ Proposal received | height=%d proposer=%s\n",
							block.ID,
							ShortID(block.Proposer),
						)
					}
					n.HandleProposal(block)
				}
			}()
		}
*/
func (n *Node) setupProtocols() {
	// Block sync protocol
	n.Host.SetStreamHandler(BlockSyncProtocol, n.handleBlockStream)
	n.Host.SetStreamHandler(HeaderSyncProtocol, n.handleHeaderSyncStream)
	n.Host.SetStreamHandler(SnapshotMetaProtocol, n.handleSnapshotMetaStream)
	n.Host.SetStreamHandler(SnapshotChunkProtocol, n.handleSnapshotChunkStream)
	// Consensus stream
	n.Host.SetStreamHandler("/msc/consensus/1.0.0", n.handleConsensusStream)
	// Peer info exchange
	n.Host.SetStreamHandler("/msc/peerinfo/1.0.0", n.handlePeerInfoStream)
	// Ping protocol (already enabled in libp2p.New)
	if DebugNet {
		fmt.Println("Ã°Å¸â€Â§ Network protocols configured")
	}
}

// handleConsensusStream handles consensus stream.
func (n *Node) handleConsensusStream(s network.Stream) {
	defer s.Close()
	// =====================================================
	// Ã¢ÂÂ±Ã¯Â¸Â SET TIMEOUTS FOR NETWORK OPERATIONS
	// =====================================================
	deadline := time.Now().Add(120 * time.Second) // Longer timeout for consensus
	s.SetDeadline(deadline)
	s.SetReadDeadline(deadline)
	s.SetWriteDeadline(deadline)
	// =====================================================
	// Ã°Å¸â€œâ€“ SETUP READER/WRITER WITH BUFFERING
	// =====================================================
	reader := bufio.NewReaderSize(s, 65536) // 64KB buffer
	// `writer` stores the value produced by this operation.
	writer := bufio.NewWriterSize(s, 65536)
	defer writer.Flush()
	// `normalClose` stores the value produced by this operation.
	normalClose := false
	// =====================================================
	// Ã°Å¸â€â€ž PROCESSING LOOP
	// =====================================================
	for {
		// =================================================
		// Ã°Å¸â€œÂ¨ READ MESSAGE WITH NEWLINE DELIMITER
		// =================================================
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "closed") {
				normalClose = true
				break
			}
			if err == io.EOF {
				if DebugNet {
					fmt.Printf("Ã°Å¸â€Å’ Consensus stream EOF from %s\n", s.Conn().RemotePeer())
				}
			} else if !strings.Contains(err.Error(), "closed") {
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Consensus stream read error from %s: %v\n",
						s.Conn().RemotePeer(), err)
				}
			}
			break
		}
		// =================================================
		// Ã°Å¸Â§Â© PARSE JSON MESSAGE
		// =================================================
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// `msg` stores the value used by this operation.
		var msg Message
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Invalid JSON from %s: %v\n",
					s.Conn().RemotePeer(), err)
				fmt.Printf("   Raw: %s\n", line[:min(100, len(line))])
			}
			// Send error response
			errorResp := Message{
				Type: "error",
				Data: []byte(fmt.Sprintf(`{"error":"invalid_json","message":"%v"}`, err)),
			}
			// `respData` stores the response produced by this operation.
			respData, _ := json.Marshal(errorResp)
			writer.Write(append(respData, '\n'))
			writer.Flush()
			continue
		}
		// =================================================
		// Ã°Å¸Å½Â¯ VALIDATE MESSAGE TYPE
		// =================================================
		if msg.Type == "" {
			if DebugNet {
				fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Empty message type from %s\n", s.Conn().RemotePeer())
			}
			continue
		}
		// =================================================
		// Ã°Å¸â€œÅ  DEBUG LOGGING
		// =================================================
		if DebugConsensus {
			fmt.Printf("Ã°Å¸â€œÂ¨ Consensus message from %s: %s (size: %d bytes)\n",
				s.Conn().RemotePeer(), msg.Type, len(msg.Data))
			// Log specific message types in detail
			switch msg.Type {
			/* legacy consensus removed: proposals/votes
			case MsgProposeBlock:
				var block Block
				if err := json.Unmarshal(msg.Data, &block); err == nil {
					fmt.Printf("   Block proposal: height=%d proposer=%s\n",
						block.ID, ShortID(block.Proposer))
				}
			case MsgBlockVote:
				var vote BlockVote
				if err := json.Unmarshal(msg.Data, &vote); err == nil {
					fmt.Printf("   Block vote: height=%d validator=%s\n",
						vote.Height, ShortID(vote.Validator))
				}
			*/
			case MsgPeerHello:
				// `hello` stores the value used by this operation.
				var hello PeerHello
				// `err` stores the error produced by this operation.
				if err := json.Unmarshal(msg.Data, &hello); err == nil {
					fmt.Printf("   Peer hello: validator=%s\n", ShortID(hello.ValidatorID))
				}
			}
		}
		// =================================================
		// Ã°Å¸Å¡â‚¬ PROCESS MESSAGE
		// =================================================
		n.handleMessage(msg, s.Conn().RemotePeer().String())
		// =================================================
		// Ã°Å¸â€œÂ¤ SEND ACKNOWLEDGEMENT
		// =================================================
		ack := Message{
			Type: "ack",
			Data: []byte(fmt.Sprintf(`{"type":"%s","status":"processed"}`, msg.Type)),
		}
		// `ackData` stores the value produced by this operation.
		ackData, _ := json.Marshal(ack)
		// `err` stores the error produced by this operation.
		if _, err := writer.Write(append(ackData, '\n')); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to send ack to %s: %v\n",
					s.Conn().RemotePeer(), err)
			}
			break
		}
		// Flush periodically to ensure delivery
		if err := writer.Flush(); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Stream flush error to %s: %v\n",
					s.Conn().RemotePeer(), err)
			}
			break
		}
		// =================================================
		// Ã°Å¸â€â€ž RESET DEADLINE FOR NEXT MESSAGE
		// =================================================
		newDeadline := time.Now().Add(30 * time.Second)
		s.SetDeadline(newDeadline)
		s.SetReadDeadline(newDeadline)
		s.SetWriteDeadline(newDeadline)
	}
	if normalClose {
		return
	}
	// =====================================================
	// Ã°Å¸â€œÅ  FINAL LOGGING
	// =====================================================
	if DebugNet {
		fmt.Printf("Ã°Å¸â€Å’ Consensus stream closed with %s\n", s.Conn().RemotePeer())
	}
}

// joinGossipTopics implements the join gossip topics helper.
func (n *Node) joinGossipTopics() {
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()
	// Block topic
	blockTopic := TopicBlock
	// `blockSub` and `err` store the error produced by this operation.
	blockSub, err := n.PubSub.Subscribe(blockTopic)
	if err == nil {
		go n.handleBlockGossip(blockSub)
	}
	// Transaction topic
	txTopic := TopicTx
	// `txSub` and `err` store the error produced by this operation.
	txSub, err := n.PubSub.Subscribe(txTopic)
	if err == nil {
		go n.handleTransactionGossip(txSub)
	}
	// Validator gossip topic
	valTopic := TopicValidator
	// `valSub` and `err` store the error produced by this operation.
	valSub, err := n.PubSub.Subscribe(valTopic)
	if err == nil {
		go n.handleValidatorGossip(valSub)
	}
	// Snapshot meta gossip topic
	snapshotMetaTopic := TopicSnapshotMeta
	// `snapshotMetaSub` and `err` store the error produced by this operation.
	snapshotMetaSub, err := n.PubSub.Subscribe(snapshotMetaTopic)
	if err == nil {
		go n.handleSnapshotMetaGossip(snapshotMetaSub)
	}
	// Snapshot chunk gossip topic
	snapshotChunkTopic := TopicSnapshotChunk
	// `snapshotChunkSub` and `err` store the error produced by this operation.
	snapshotChunkSub, err := n.PubSub.Subscribe(snapshotChunkTopic)
	if err == nil {
		go n.handleSnapshotChunkGossip(snapshotChunkSub)
	}
	// Snapshot proof gossip topic
	snapshotProofTopic := TopicSnapshotProof
	// `snapshotProofSub` and `err` store the error produced by this operation.
	snapshotProofSub, err := n.PubSub.Subscribe(snapshotProofTopic)
	if err == nil {
		go n.handleSnapshotProofGossip(snapshotProofSub)
	}
	// Consensus gossip topic
	consensusTopic := TopicConsensus
	// `consensusSub` and `err` store the error produced by this operation.
	consensusSub, err := n.PubSub.Subscribe(consensusTopic)
	if err == nil {
		go n.handleConsensusGossip(consensusSub)
	}
	// Join topics (these don't return errors in Join)
	n.BlockTopic, _ = n.PubSub.Join(TopicBlock)
	n.TxTopic, _ = n.PubSub.Join(TopicTx)
	n.ValidatorTopic, _ = n.PubSub.Join(TopicValidator)
	n.ConsensusTopic, _ = n.PubSub.Join(TopicConsensus)
	n.SnapshotMetaTopic, _ = n.PubSub.Join(TopicSnapshotMeta)
	n.SnapshotChunkTopic, _ = n.PubSub.Join(TopicSnapshotChunk)
	n.SnapshotProofTopic, _ = n.PubSub.Join(TopicSnapshotProof)
	// Subscribe to topics
	// go n.listenVotes(ctx)
	go n.listenBlocks(ctx)
	go n.listenTx(ctx)
	go n.listenConsensus(ctx)
}

// listenBlocks implements the listen blocks helper.
func (n *Node) listenBlocks(ctx context.Context) {
	// =====================================================
	// Ã°Å¸â€œÂ¡ SUBSCRIPTION INITIALIZATION
	// =====================================================
	var sub *pubsub.Subscription
	// `err` stores the error produced by this operation.
	var err error
	// Try BlockTopic first
	if n.BlockTopic != nil {
		sub, err = n.BlockTopic.Subscribe(
			pubsub.WithBufferSize(1024),
		)
		if err != nil && DebugNet {
			fmt.Printf("BlockTopic subscribe failed: %v\n", err)
		}
	}
	// Fallback to direct PubSub subscription
	if sub == nil {
		sub, err = n.PubSub.Subscribe(TopicBlock)
		if err != nil {
			if DebugNet {
				fmt.Printf("Failed to subscribe to blocks: %v\n", err)
			}
		}
	}
	if sub == nil {
		sub, err = n.PubSub.Subscribe(TopicBlocksLegacy)
		if err != nil {
			if DebugNet {
				fmt.Printf("Failed to subscribe to blocks (legacy): %v\n", err)
			}
			return
		}
	}
	defer func() {
		if sub != nil {
			sub.Cancel()
			if n.BlockSubscription == sub {
				n.BlockSubscription = nil
			}
			if DebugNet {
				fmt.Println("Ã°Å¸â€ºâ€˜ Block listener stopped")
			}
		}
	}()
	n.BlockSubscription = sub
	if DebugNet {
		fmt.Println("Ã¢Å“â€¦ Listening for blocks...")
	}
	// =====================================================
	// Ã°Å¸â€œÅ  METRICS AND STATE
	// =====================================================
	var (
		// `blockCount` stores the block data handled by this operation.
		blockCount int
		// `lastLogTime` stores the value used by this operation.
		lastLogTime = time.Now()
		// `seenBlockMu` stores the synchronization state protecting shared data.
		seenBlockMu sync.RWMutex
		// `seenBlocks` stores the value used by this operation.
		seenBlocks = make(map[string]time.Time)
		// `lastHeight` stores the value used by this operation.
		lastHeight uint64
		// `heightJumps` stores the value used by this operation.
		heightJumps int
	)
	// `cleanupDone` stores the value produced by this operation.
	cleanupDone := make(chan struct{})
	defer close(cleanupDone)
	// Cleanup goroutine for seen blocks cache
	go func() {
		// `ticker` stores the value produced by this operation.
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupDone:
				return
			case <-ctx.Done():
				return
			case <-n.shutdownCh:
				return
			case <-ticker.C:
				seenBlockMu.Lock()
				// `now` stores the value produced by this operation.
				now := time.Now()
				// `hash` and `seenTime` track the digest used to identify or verify the related data.
				for hash, seenTime := range seenBlocks {
					if now.Sub(seenTime) > 30*time.Minute {
						delete(seenBlocks, hash)
					}
				}
				seenBlockMu.Unlock()
			}
		}
	}()
	// =====================================================
	// Ã°Å¸â€â€ž PROCESSING LOOP
	// =====================================================
	for {
		select {
		case <-ctx.Done():
			if DebugNet {
				fmt.Printf("Ã°Å¸â€ºâ€˜ Block listener stopped (context): %v\n", ctx.Err())
			}
			return
		case <-n.shutdownCh:
			if DebugNet {
				fmt.Println("Ã°Å¸â€ºâ€˜ Block listener (shutdown)")
			}
			return
		default:
			// =================================================
			// Ã°Å¸â€œÂ¨ RECEIVE MESSAGE
			// =================================================
			msg, err := sub.Next(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Block subscription error: %v\n", err)
				}
				time.Sleep(500 * time.Millisecond) // Longer delay for blocks
				continue
			}
			// `receivedAt` stores the value produced by this operation.
			receivedAt := time.Now()
			// =================================================
			// Ã°Å¸â„¢â€¦ SELF-MESSAGE FILTERING
			// =================================================
			if msg.ReceivedFrom == n.Host.ID() {
				continue
			}
			// Ignore empty payloads; block gossip supports JSON and protobuf-wire envelopes.
			if len(msg.Data) == 0 {
				continue
			}
			// =================================================
			// Ã°Å¸Â§Â© MESSAGE PARSING
			// =================================================
			var block Block
			// `parseErr` stores the error produced by this operation.
			var parseErr error
			// Try Message wrapper format first
			var m Message
			// `err` stores the error produced by this operation.
			if err := UnmarshalP2PMessage(msg.Data, &m); err == nil && m.Type != "" {
				if m.Type == MsgLeaderBlock {
					// `leader` stores the value used by this operation.
					var leader Block
					// `err` stores the error produced by this operation.
					if err := json.Unmarshal(m.Data, &leader); err == nil {
						_ = n.submitLeaderBlockOnConsensusLane(leader, msg.ReceivedFrom.String())
						n.observeBlockPropagation(leader, receivedAt)
					} else {
						n.recordPeerSecurityFault(msg.ReceivedFrom.String(), "invalid_leader_block_gossip")
					}
					continue
				}
				if m.Type == MsgBlock {
					parseErr = json.Unmarshal(m.Data, &block)
				} else {
					continue
				}
			} else {
				// Try direct Block format
				parseErr = json.Unmarshal(msg.Data, &block)
			}
			if parseErr != nil {
				n.recordPeerSecurityFault(msg.ReceivedFrom.String(), "invalid_block_gossip")
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to parse block: %v\n", parseErr)
					if len(msg.Data) < 500 {
						fmt.Printf("   Raw data (first 500 chars): %.500s\n", string(msg.Data))
					}
				}
				continue
			}
			// =================================================
			// Ã°Å¸Å½Â¯ BASIC VALIDATION
			// =================================================
			if block.ID == 0 || block.BlockHash == "" || block.PrevHash == "" {
				n.recordPeerSecurityFault(msg.ReceivedFrom.String(), "invalid_block_fields")
				if DebugNet {
					fmt.Println("Ã¢Å¡Â Ã¯Â¸Â Invalid block: missing required fields")
				}
				continue
			}
			// Check for height jumps (possible fork or sync)
			if lastHeight > 0 && block.ID > lastHeight+1 {
				heightJumps++
				if DebugSync {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Height jump detected: %d -> %d (gap: %d)\n",
						lastHeight, block.ID, block.ID-lastHeight-1)
				}
			}
			lastHeight = block.ID
			// =================================================
			// Ã°Å¸â€â€ž DEDUPLICATION
			// =================================================
			seenBlockMu.RLock()
			// `seenTime` and `exists` store whether the related condition is satisfied.
			seenTime, exists := seenBlocks[block.BlockHash]
			seenBlockMu.RUnlock()
			if exists && time.Since(seenTime) < 5*time.Minute {
				if DebugConsensus {
					fmt.Printf("Ã¢ÂÂ­Ã¯Â¸Â Duplicate block ignored: height=%d hash=%s\n",
						block.ID, block.BlockHash[:8])
				}
				continue
			}
			seenBlockMu.Lock()
			seenBlocks[block.BlockHash] = time.Now()
			seenBlockMu.Unlock()
			// =================================================
			// Ã°Å¸â€œÅ  DEBUG LOGGING
			// =================================================
			blockCount++
			if DebugConsensus {
				// Log every block in detail
				fmt.Printf("Ã°Å¸â€œÂ¦ Block received | height=%d type=%s proposer=%s from=%s\n",
					block.ID,
					block.Type,
					ShortID(block.Proposer),
					msg.ReceivedFrom)
				// Additional info for work blocks
				if block.Type == BlockTypeWork && len(block.Transactions) > 0 {
					fmt.Printf("   TX count: %d, Mempool root: %s\n",
						len(block.Transactions), block.MempoolRoot[:8])
				}
				// Periodic summary
				if blockCount%100 == 0 || time.Since(lastLogTime) > 30*time.Second {
					fmt.Printf("Ã°Å¸â€œÅ  Block stats: Received %d blocks, height jumps: %d\n",
						blockCount, heightJumps)
					lastLogTime = time.Now()
				}
			}
			// =================================================
			// Ã°Å¸Å¡â‚¬ PROCESS BLOCK
			// =================================================
			_ = n.submitFinalBlockOnConsensusLane(block)
			n.observeBlockPropagation(block, receivedAt)
			// =================================================
			// Ã¢ÂÂ¸Ã¯Â¸Â THROTTLING (blocks are less frequent than TXs)
			// =================================================
			// Small delay to allow other goroutines to run
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// listenTx implements the listen tx helper.
func (n *Node) listenTx(ctx context.Context) {
	// =====================================================
	// Ã°Å¸â€œÂ¡ SUBSCRIPTION INITIALIZATION
	// =====================================================
	var sub *pubsub.Subscription
	// `err` stores the error produced by this operation.
	var err error
	// Try TxTopic first, fallback to PubSub
	if n.TxTopic != nil {
		sub, err = n.TxTopic.Subscribe(
			pubsub.WithBufferSize(2048), // Larger buffer for TX flood
		)
		if err != nil && DebugNet {
			fmt.Printf("TxTopic subscribe failed: %v\n", err)
		}
	}
	if sub == nil {
		// Fallback to direct PubSub subscription
		sub, err = n.PubSub.Subscribe(TopicTx)
		if err != nil {
			if DebugNet {
				fmt.Printf("Failed to subscribe to transactions: %v\n", err)
			}
		}
	}
	if sub == nil {
		// Legacy fallback
		sub, err = n.PubSub.Subscribe(TopicTransactionsLegacy)
		if err != nil {
			if DebugNet {
				fmt.Printf("Failed to subscribe to transactions (legacy): %v\n", err)
			}
			return
		}
	}
	defer func() {
		if sub != nil {
			sub.Cancel()
			if n.TxSubscription == sub {
				n.TxSubscription = nil
			}
			if DebugNet {
				fmt.Println("Ã°Å¸â€ºâ€˜ TX listener stopped")
			}
		}
	}()
	n.TxSubscription = sub
	if DebugNet {
		fmt.Println("Ã¢Å“â€¦ Listening for transactions...")
	}
	// =====================================================
	// Ã°Å¸â€œÅ  METRICS AND RATE LIMITING
	// =====================================================
	var (
		// `txCount` stores the transaction data handled by this operation.
		txCount int
		// `lastLogTime` stores the value used by this operation.
		lastLogTime = time.Now()
		// `globalRate` stores the value used by this operation.
		globalRate = TxGossipGlobalRatePerSecond
		// `peerRate` stores the value used by this operation.
		peerRate = TxGossipPeerRatePerSecond
		// `peerBurst` stores the value used by this operation.
		peerBurst = TxGossipPeerBurst
		// `peerLimiterTTL` stores the value used by this operation.
		peerLimiterTTL = TxGossipPeerLimiterTTL
		// `seenTxCache` stores the value used by this operation.
		seenTxCache = make(map[string]time.Time)
		// `peerLimiters` stores the value used by this operation.
		peerLimiters = make(map[string]*rate.Limiter)
		// `peerLimiterLastSeen` stores the value used by this operation.
		peerLimiterLastSeen = make(map[string]time.Time)
		// `cacheCleanup` stores the value used by this operation.
		cacheCleanup = time.NewTicker(5 * time.Minute)
	)
	if globalRate <= 0 {
		globalRate = 1000
	}
	if peerRate <= 0 {
		peerRate = 100
	}
	if peerBurst <= 0 {
		peerBurst = peerRate
	}
	if peerLimiterTTL <= 0 {
		peerLimiterTTL = 10 * time.Minute
	}
	// `rateLimiter` stores the value produced by this operation.
	rateLimiter := rate.NewLimiter(rate.Every(time.Second/time.Duration(globalRate)), globalRate)
	defer cacheCleanup.Stop()
	// =====================================================
	// Ã°Å¸â€â€ž PROCESSING LOOP
	// =====================================================
	for {
		select {
		case <-ctx.Done():
			if DebugNet {
				fmt.Printf("Ã°Å¸â€ºâ€˜ TX listener stopped (context): %v\n", ctx.Err())
			}
			return
		case <-n.shutdownCh:
			if DebugNet {
				fmt.Println("Ã°Å¸â€ºâ€˜ TX listener (shutdown)")
			}
			return
		case <-cacheCleanup.C:
			// Clean old entries from cache
			now := time.Now()
			// `txID` and `seenTime` track the transaction data handled by this operation.
			for txID, seenTime := range seenTxCache {
				if now.Sub(seenTime) > 10*time.Minute {
					delete(seenTxCache, txID)
				}
			}
			// `peerID` and `seenTime` track the current values while iterating.
			for peerID, seenTime := range peerLimiterLastSeen {
				if now.Sub(seenTime) > peerLimiterTTL {
					delete(peerLimiters, peerID)
					delete(peerLimiterLastSeen, peerID)
				}
			}
		default:
			// =================================================
			// Ã°Å¸â€œÂ¨ RECEIVE MESSAGE
			// =================================================
			msg, err := sub.Next(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â TX subscription error: %v\n", err)
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// =================================================
			// Ã°Å¸â„¢â€¦ SELF-MESSAGE FILTERING
			// =================================================
			if msg.ReceivedFrom == n.Host.ID() {
				if DebugConsensus {
					fmt.Println("Ã¢ÂÂ­Ã¯Â¸Â Ignoring self-published TX")
				}
				continue
			}
			// =================================================
			// Ã¢ÂÂ¸Ã¯Â¸Â RATE LIMITING
			// =================================================
			peerKey := strings.TrimSpace(msg.ReceivedFrom.String())
			if peerKey == "" {
				peerKey = "unknown"
			}
			if len(msg.Data) == 0 || len(msg.Data) > MaxTxGossipMessageBytes {
				n.recordPeerSecurityFault(peerKey, "tx_gossip_invalid_size")
				if DebugNet {
					fmt.Println("TX gossip payload rejected: invalid size")
				}
				continue
			}
			if !rateLimiter.Allow() {
				n.recordPeerRateLimitDrop(peerKey, "tx_gossip_global")
				if DebugNet {
					fmt.Println("Ã¢Å¡Â Ã¯Â¸Â TX rate limit exceeded, skipping")
				}
				continue
			}
			// =================================================
			// Ã°Å¸Â§Â© MESSAGE PARSING (MULTI-FORMAT SUPPORT)
			// =================================================
			peerLimiter := peerLimiters[peerKey]
			if peerLimiter == nil {
				peerLimiter = rate.NewLimiter(rate.Every(time.Second/time.Duration(peerRate)), peerBurst)
				peerLimiters[peerKey] = peerLimiter
			}
			peerLimiterLastSeen[peerKey] = time.Now()
			if !peerLimiter.Allow() {
				n.recordPeerRateLimitDrop(peerKey, "tx_gossip_peer")
				if DebugNet {
					fmt.Printf("TX peer rate limit exceeded peer=%s\n", ShortID(peerKey))
				}
				continue
			}
			// `tx` stores the transaction data handled by this operation.
			var tx Transaction
			// `parseErr` stores the error produced by this operation.
			var parseErr error
			// Try Message wrapper format first
			var m Message
			// `err` stores the error produced by this operation.
			if err := UnmarshalP2PMessage(msg.Data, &m); err == nil && m.Type == MsgTx {
				parseErr = UnmarshalTransactionWire(m.Data, &tx)
			} else {
				// Try direct Transaction format
				parseErr = json.Unmarshal(msg.Data, &tx)
			}
			if parseErr != nil {
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to parse transaction: %v\n", parseErr)
					if len(msg.Data) < 200 {
						fmt.Printf("   Raw data: %s\n", string(msg.Data))
					}
				}
				continue
			}
			// =================================================
			// Ã°Å¸Å½Â¯ BASIC VALIDATION
			// =================================================
			normalizeIncomingTx(&tx)
			// `err` stores the error produced by this operation.
			if err := validateRemovedVMEnvelope(tx); err != nil {
				n.recordPeerSecurityFault(peerKey, "tx_gossip_evm_removed")
				if DebugNet {
					fmt.Printf("Invalid removed EVM transaction surface: %v\n", err)
				}
				continue
			}
			// `err` stores the error produced by this operation.
			if err := validateTransactionShape(tx); err != nil {
				n.recordPeerSecurityFault(peerKey, "tx_gossip_shape")
				if DebugNet {
					fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Invalid transaction shape: %v\n", err)
				}
				continue
			}
			// =================================================
			// Ã°Å¸â€â€ž DEDUPLICATION CACHE
			// =================================================
			now := time.Now()
			// `dedupeID` stores the value produced by this operation.
			dedupeID := tx.ID
			if dedupeID == "" {
				dedupeID = ComputeTxID(tx)
			}
			// `seenTime` and `exists` store whether the related condition is satisfied.
			if seenTime, exists := seenTxCache[dedupeID]; exists {
				if now.Sub(seenTime) < 1*time.Minute {
					// Recently seen, skip
					if DebugConsensus {
						fmt.Printf("Ã¢ÂÂ­Ã¯Â¸Â Duplicate TX ignored (recent): %s\n", ShortID(dedupeID))
					}
					continue
				}
			}
			seenTxCache[dedupeID] = now
			// =================================================
			// Ã°Å¸â€œÅ  DEBUG LOGGING (THROTTLED)
			// =================================================
			txCount++
			if DebugConsensus {
				// Log first few and then periodically
				if txCount <= 10 || txCount%100 == 0 || time.Since(lastLogTime) > 10*time.Second {
					fmt.Printf("Ã°Å¸â€™Â¸ TX received | id=%s from=%s to=%s amount=%d fee=%d\n",
						ShortID(tx.ID),
						ShortID(tx.From),
						ShortID(tx.To),
						tx.Amount,
						tx.Fee)
					lastLogTime = time.Now()
				}
			}
			// =================================================
			// Ã°Å¸Å¡â‚¬ PROCESS TRANSACTION
			// =================================================
			if ok, reason := n.ReceiveTransactionWithReason(tx); !ok && DebugNet {
				// Keep gossip path aligned with canonical mempool validation.
				txID := tx.ID
				if txID == "" {
					txID = ComputeTxID(tx)
				}
				if reason == "" {
					reason = "transaction rejected"
				}
				fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â TX rejected from gossip | id=%s reason=%s\n", ShortID(txID), reason)
			}
			// =================================================
			// Ã°Å¸â€œË† PERIODIC STATS
			// =================================================
			if txCount%1000 == 0 && DebugNet {
				fmt.Printf("Ã°Å¸â€œÅ  TX stats: Processed %d transactions\n", txCount)
			}
			// =================================================
			// Ã¢ÂÂ¸Ã¯Â¸Â SMALL DELAY FOR CPU BREATHER
			// =================================================
			time.Sleep(1 * time.Millisecond)
		}
	}
}

/*
legacy consensus removed
*/
