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
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
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
	peerSuspectTimeout              = 2 * time.Minute
	peerFlapWindow                  = 5 * time.Minute
	peerFlapThreshold               = 5
	peerQuarantineFor               = 5 * time.Minute
	peerQuarantineForProtocol       = 30 * time.Minute
	peerQuarantineForPeerInfoStream = 20 * time.Second
	peerQuarantineForMismatch       = 1 * time.Hour
	peerHelloCooldown               = 30 * time.Second
	peerHelloMaxClockSkew           = 5 * time.Minute
	peerHelloNonceTTL               = 15 * time.Minute
	peerSuspectInterval             = 20 * time.Second
	syncCooldown                    = 30 * time.Second
	peerMinHold                     = 30 * time.Second
	peerGraftCooldown               = 30 * time.Second
	dialBackoffStep1                = 5 * time.Second
	dialBackoffStep2                = 30 * time.Second
	dialBackoffStep3                = 2 * time.Minute
	dialBackoffMax                  = 5 * time.Minute
	execVoteReplayTTL               = 2 * time.Minute
	execVoteStaleIngressTTL         = 30 * time.Second
	execVoteRebroadcastCooldown     = 5 * time.Second
	execVoteRatePerSigner           = 8
	execVoteRateBurst               = 8
	execMismatchStrikeWindow        = 3 * time.Minute
	execMismatchQuarantineAt        = 2
	execMismatchSlashAt             = 3
	invalidProposerStrikeWindow     = 3 * time.Minute
	invalidProposerQuarantineAt     = 2
	invalidProposerPeerQuarantineAt = 3
	execVoteStaleLagBlocks          = 2
	finalizedDriftThreshold         = 20
	finalizedDriftEscalateThreshold = 200
	finalizedDriftWindow            = 10 * time.Minute
	finalizedDriftCooldown          = 10 * time.Minute
	finalizedDriftDropLogInterval   = 30 * time.Second
	finalizedDriftNearTipSlack      = 16
	finalizedDriftMaxServeRange     = 64
	finalizedDriftSnapshotCooldown  = 30 * time.Second
	blockSyncServeMaxBlocks         = 512
	consensusPublishTimeout         = 500 * time.Millisecond
)

var syncPeerRequestTimeoutOverride time.Duration

type blockRequestPhaseError struct {
	Stage   string
	Peer    string
	From    uint64
	To      uint64
	After   time.Duration
	Timeout bool
	Err     error
}

func (e *blockRequestPhaseError) Error() string {
	if e == nil {
		return ""
	}
	status := "failed"
	if e.Timeout {
		status = "timeout"
	}
	msg := fmt.Sprintf("block_request_%s_%s peer=%s range=%d-%d", strings.TrimSpace(e.Stage), status, strings.TrimSpace(e.Peer), e.From, e.To)
	if e.After > 0 {
		msg += fmt.Sprintf(" after=%s", e.After)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *blockRequestPhaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

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

func syncBlockRequestMaxBlocks(batchMax uint64) uint64 {
	if batchMax == 0 || batchMax > blockSyncServeMaxBlocks {
		return blockSyncServeMaxBlocks
	}
	return batchMax
}

func clampBlockSyncRangeToServeLimit(from, to uint64, batchMax uint64) uint64 {
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
	PeerDriftClassStale     PeerDriftClass = "STALE_DRIFT"
	PeerDriftClassDangerous PeerDriftClass = "DANGEROUS_DRIFT"
	PeerDriftClassAhead     PeerDriftClass = "AHEAD_DRIFT"
)

type PeerDriftState struct {
	Count         int
	FirstSeen     time.Time
	LastSeen      time.Time
	From          uint64
	To            uint64
	Expected      string
	Got           string
	RecomputedAt  time.Time
	SyncOnlyUntil time.Time
	LastClass     PeerDriftClass
}

func (n *Node) configPeerListsSnapshot() ([]string, []string) {
	if n == nil || n.Config == nil {
		return nil, nil
	}
	n.configMu.RLock()
	persistent := append([]string{}, n.Config.PersistentPeers...)
	seeds := append([]string{}, n.Config.Seeds...)
	n.configMu.RUnlock()
	return persistent, seeds
}

func (n *Node) persistentPeersSnapshot() []string {
	persistent, _ := n.configPeerListsSnapshot()
	return persistent
}

func (n *Node) seedsSnapshot() []string {
	_, seeds := n.configPeerListsSnapshot()
	return seeds
}

func (n *Node) setPersistentPeers(peers []string) {
	if n == nil || n.Config == nil {
		return
	}
	n.configMu.Lock()
	n.Config.PersistentPeers = append([]string{}, peers...)
	n.configMu.Unlock()
}

func (n *Node) setSeedPeers(peers []string) {
	if n == nil || n.Config == nil {
		return
	}
	n.configMu.Lock()
	n.Config.Seeds = append([]string{}, peers...)
	n.configMu.Unlock()
}

func (n *Node) setConfigPeerLists(persistent []string, seeds []string) {
	if n == nil || n.Config == nil {
		return
	}
	n.configMu.Lock()
	n.Config.PersistentPeers = append([]string{}, persistent...)
	n.Config.Seeds = append([]string{}, seeds...)
	n.configMu.Unlock()
}

func (n *Node) logPubSubStatus() {
	if n.PubSub == nil {
		fmt.Println("PubSub: Not initialized")
		return
	}

	topics := []string{TopicBlock, TopicTx, TopicConsensus, TopicValidator, TopicSnapshotMeta, TopicSnapshotChunk, TopicSnapshotProof}

	fmt.Println("PubSub Mesh Status:")
	hasConnections := false

	for _, topicName := range topics {
		peers := n.PubSub.ListPeers(topicName)
		fmt.Printf("  %s: %d peers\n", topicName, len(peers))

		if len(peers) > 0 {
			hasConnections = true
			if DebugConsensus {
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

func (n *Node) publishConsensusTopicWithTimeout(topic *pubsub.Topic, data []byte) error {
	if topic == nil {
		return nil
	}
	ctx := context.Background()
	if n != nil {
		if rootCtx := n.RootContext(); rootCtx != nil {
			ctx = rootCtx
		}
	}
	publishCtx, cancel := context.WithTimeout(ctx, consensusPublishTimeout)
	defer cancel()
	return topic.Publish(publishCtx, data)
}

func (n *Node) broadcastLeaderBlockUnchecked(block Block) {
	if n == nil || (n.BlockTopic == nil && n.TopicBlocks == nil && n.PubSub == nil) {
		return
	}
	msg := Message{
		Type: MsgLeaderBlock,
		Data: MustJSON(block),
	}
	data, err := MarshalP2PMessage(msg)
	if err != nil {
		return
	}
	n.fanoutConsensusMessageToPeers(msg)

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
		if err := n.PubSub.Publish(TopicBlock, data); err != nil {
			_ = n.PubSub.Publish(TopicBlocksLegacy, data)
		}
		_ = n.PubSub.Publish(TopicConsensus, data)
	})
}

func (n *Node) handleTransactionGossip(sub *pubsub.Subscription) {
	for {
		_, err := sub.Next(n.RootContext())
		if err != nil {
			return
		}
	}
}

func (n *Node) handleValidatorGossip(sub *pubsub.Subscription) {
	ctx := n.RootContext()
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		peerID := ""
		if msg.ReceivedFrom != "" {
			peerID = msg.ReceivedFrom.String()
		}
		if n.handleConsensusEnvelopeFromPeer(msg.Data, peerID) {
			continue
		}

		var wrapped Message
		if err := json.Unmarshal(msg.Data, &wrapped); err == nil && wrapped.Type != "" {
			switch wrapped.Type {
			case MsgValidatorAnnounce:
				n.handleValidatorAnnouncement(wrapped.Data)
			case MsgValidatorSetUpdate:
				var update ValidatorSetUpdate
				if err := json.Unmarshal(wrapped.Data, &update); err == nil {
					n.handleValidatorSetUpdate(update)
				}
			}
			continue
		}

		var info ValidatorInfo
		if err := json.Unmarshal(msg.Data, &info); err != nil {
			continue
		}
		info.ID = normalizeValidatorID(info.ID)
		if info.ID == "" {
			continue
		}

		n.validatorMu.Lock()
		st, ok := n.validatorStatus[info.ID]
		if !ok {
			st = &ValidatorStatus{}
			n.validatorStatus[info.ID] = st
		}
		st.Height = info.Height
		st.LastSeen = time.Now()
		n.validatorMu.Unlock()

		participationMu.Lock()
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

func (n *Node) handleConsensusGossip(sub *pubsub.Subscription) {
	ctx := n.RootContext()
	for {
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

func (n *Node) handleBlockGossip(sub *pubsub.Subscription) {
	ctx := n.RootContext()

	for {
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
		if err := json.Unmarshal(msg.Data, &wrapped); err == nil && wrapped.Type != "" {
			if wrapped.Type == MsgLeaderBlock {
				var block Block
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

		var block Block
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

func (n *Node) requestBlocksFromPeer(
	pid peer.ID,
	from, to uint64,
	wantSnapshot bool,
	snapshotHeight uint64,
) ([]Block, *StateSnapshot, *ExecPoolSnapshot, error) {
	if !wantSnapshot {
		if blocks, ok := n.localBlocksForRange(from, to); ok {
			fmt.Printf("[SYNC-REQUEST-SKIP] peer=%s range=%d-%d reason=local_blocks_present count=%d\n",
				ShortID(pid.String()), from, to, len(blocks))
			return blocks, nil, nil, nil
		}
	}
	return n.requestBlocksFromPeerDirect(pid, from, to, wantSnapshot, snapshotHeight)
}

func (n *Node) requestBlocksFromPeerDirect(
	pid peer.ID,
	from, to uint64,
	wantSnapshot bool,
	snapshotHeight uint64,
) ([]Block, *StateSnapshot, *ExecPoolSnapshot, error) {
	if n.Host == nil {
		return nil, nil, nil, fmt.Errorf("host not initialized")
	}
	timeout := syncPeerRequestTimeout()
	peerLabel := ShortID(pid.String())
	fmt.Printf("[SYNC-REQUEST-START] peer=%s range=%d-%d snapshot=%t snapshot_height=%d timeout_ms=%d\n",
		peerLabel, from, to, wantSnapshot, snapshotHeight, timeout.Milliseconds())

	type openResult struct {
		stream network.Stream
		err    error
	}
	openCtx, cancelOpen := context.WithTimeout(context.Background(), timeout)
	defer cancelOpen()
	openStarted := time.Now()
	fmt.Printf("[SYNC-REQUEST-OPEN] peer=%s range=%d-%d\n", peerLabel, from, to)
	openCh := make(chan openResult, 1)
	go func() {
		stream, err := n.openStream(openCtx, pid, BlockSyncProtocol)
		select {
		case openCh <- openResult{stream: stream, err: err}:
		case <-openCtx.Done():
			if stream != nil {
				_ = stream.Reset()
				_ = stream.Close()
			}
		}
	}()

	var s network.Stream
	select {
	case out := <-openCh:
		if out.err != nil {
			err := newBlockRequestPhaseError("open", pid, from, to, time.Since(openStarted), false, out.err)
			fmt.Printf("[SYNC-REQUEST-OPEN-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, err)
			return nil, nil, nil, err
		}
		s = out.stream
		fmt.Printf("[SYNC-REQUEST-OPEN-OK] peer=%s range=%d-%d duration_ms=%d\n",
			peerLabel, from, to, time.Since(openStarted).Milliseconds())
	case <-openCtx.Done():
		err := newBlockRequestPhaseError("open", pid, from, to, timeout, true, openCtx.Err())
		fmt.Printf("[SYNC-REQUEST-OPEN-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, err)
		return nil, nil, nil, err
	}
	defer s.Close()

	req := BlockRequest{
		From:           from,
		To:             to,
		WantSnapshot:   wantSnapshot,
		SnapshotHeight: snapshotHeight,
		BypassAck:      true,
	}
	_ = s.SetWriteDeadline(time.Now().Add(timeout))
	enc := json.NewEncoder(s)
	encodeStarted := time.Now()
	fmt.Printf("[SYNC-REQUEST-ENCODE] peer=%s range=%d-%d\n", peerLabel, from, to)
	encodeCh := make(chan error, 1)
	go func() {
		encodeCh <- enc.Encode(req)
	}()
	select {
	case err := <-encodeCh:
		if err != nil {
			_ = s.Reset()
			wrapped := newBlockRequestPhaseError("encode", pid, from, to, time.Since(encodeStarted), false, err)
			fmt.Printf("[SYNC-REQUEST-ENCODE-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, wrapped)
			return nil, nil, nil, wrapped
		}
		fmt.Printf("[SYNC-REQUEST-ENCODE-OK] peer=%s range=%d-%d duration_ms=%d\n",
			peerLabel, from, to, time.Since(encodeStarted).Milliseconds())
	case <-time.After(timeout):
		_ = s.Reset()
		err := newBlockRequestPhaseError("encode", pid, from, to, timeout, true, context.DeadlineExceeded)
		fmt.Printf("[SYNC-REQUEST-ENCODE-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, err)
		return nil, nil, nil, err
	}

	_ = s.SetReadDeadline(time.Now().Add(timeout))
	dec := json.NewDecoder(s)
	type blockResponseResult struct {
		resp BlockResponse
		err  error
	}
	decodeStarted := time.Now()
	fmt.Printf("[SYNC-REQUEST-DECODE] peer=%s range=%d-%d\n", peerLabel, from, to)
	respCh := make(chan blockResponseResult, 1)
	go func() {
		var resp BlockResponse
		err := dec.Decode(&resp)
		respCh <- blockResponseResult{resp: resp, err: err}
	}()

	select {
	case out := <-respCh:
		if out.err != nil {
			_ = s.Reset()
			err := newBlockRequestPhaseError("decode", pid, from, to, time.Since(decodeStarted), false, out.err)
			fmt.Printf("[SYNC-REQUEST-DECODE-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, err)
			return nil, nil, nil, err
		}
		fmt.Printf("[SYNC-REQUEST-RESULT] peer=%s range=%d-%d count=%d snapshot=%t duration_ms=%d\n",
			peerLabel, from, to, len(out.resp.Blocks), out.resp.Snapshot != nil, time.Since(decodeStarted).Milliseconds())
		return out.resp.Blocks, out.resp.Snapshot, out.resp.ExecPool, nil
	case <-time.After(timeout):
		_ = s.Reset()
		err := newBlockRequestPhaseError("decode", pid, from, to, timeout, true, context.DeadlineExceeded)
		fmt.Printf("[SYNC-REQUEST-DECODE-FAIL] peer=%s range=%d-%d err=%v\n", peerLabel, from, to, err)
		return nil, nil, nil, err
	}
}

func (n *Node) localBlocksForRange(from uint64, to uint64) ([]Block, bool) {
	if n == nil || n.Blockchain == nil || from == 0 || to < from {
		return nil, false
	}
	blocks := make([]Block, 0, to-from+1)
	for height := from; ; height++ {
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

func (n *Node) requestSnapshotMetaFromPeer(pid peer.ID, height uint64) (*SnapshotMetaResponse, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host not initialized")
	}
	timeout := syncPeerRequestTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s, err := n.openStream(ctx, pid, SnapshotMetaProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	_ = s.SetDeadline(time.Now().Add(timeout))
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)

	req := SnapshotMetaRequest{Height: height}
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	var resp SnapshotMetaResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	if !resp.Available {
		return nil, fmt.Errorf("snapshot metadata unavailable")
	}
	return &resp, nil
}

func (n *Node) requestSnapshotChunkFromPeer(pid peer.ID, height uint64, index uint64) (*SnapshotChunkResponse, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host not initialized")
	}
	timeout := syncPeerRequestTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s, err := n.openStream(ctx, pid, SnapshotChunkProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	_ = s.SetDeadline(time.Now().Add(timeout))
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)
	req := SnapshotChunkRequest{Height: height, Index: index}
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	var resp SnapshotChunkResponse
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

func (n *Node) sendBlockAck(pid peer.ID, height uint64) {
	if n.Host == nil || height == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := n.openStream(ctx, pid, "/msc/consensus/1.0.0")
	if err != nil {
		n.recordDialFailure(pid.String())
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "consensus_protocol_mismatch")
		}
		return
	}
	defer s.Close()

	msg := Message{
		Type: MsgBlockAck,
		Data: MustJSON(BlockAck{Height: height}),
	}
	data, _ := json.Marshal(msg)
	_, _ = s.Write(append(data, '\n'))
}

func (n *Node) sendConsensusMessage(pid peer.ID, msg Message) {
	if n == nil || n.Host == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := n.openStream(ctx, pid, "/msc/consensus/1.0.0")
	if err != nil {
		n.recordDialFailure(pid.String())
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "consensus_protocol_mismatch")
		}
		return
	}
	defer s.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_, _ = s.Write(append(data, '\n'))
	n.recordDialSuccess(pid.String())
}

func (n *Node) fanoutConsensusMessageToPeers(msg Message) {
	if n == nil || n.Host == nil || n.isShuttingDown() {
		return
	}
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		return
	}
	for _, pid := range peers {
		peerID := pid
		n.SafeGo(fmt.Sprintf("consensus_fanout_%s_%d", msg.Type, time.Now().UnixNano()), func() {
			n.sendConsensusMessage(peerID, msg)
		})
	}
}

func (n *Node) broadcastExecutionResult(heightHint uint64, execHash string, txMerkle string) {
	n.broadcastExecutionResultInternal(heightHint, execHash, txMerkle, false)
}

const (
	execResultSigVersionV1 uint8 = 1
	execResultSigVersionV2 uint8 = 2
	execVoteModeDual             = "dual"
	// Keep a wider recent-proposal window so delayed execution votes can still
	// resolve after long round-failover churn at the same height.
	execRecentProposalWindow        = 64
	execProposalSwitchRoundGap      = 4
	execProposalStickyVoteThreshold = 2
)

type execProposalSnapshot struct {
	Epoch       uint64
	Round       uint32
	BlockHash   string
	TxMerkle    string
	StateRoot   string
	ProposalKey string
}

type execBroadcastContext struct {
	HeightHint          uint64
	RoundHint           uint32
	BlockHashHint       string
	ProposalKey         string
	ExecHash            string
	TxMerkle            string
	TxCount             int
	PrevHash            string
	RuntimeLedgerHash   string
	ExecutionLedgerHash string
	TipHeight           uint64
	TipHash             string
}

type execVoteRebroadcastState struct {
	ProposalKey    string
	VoteCount      int
	LastObservedAt time.Time
	LastForcedAt   time.Time
}

const localExecVoteStaleRoundReleaseGap uint32 = 8

func legacyProposalVoteKey(height uint64) string {
	return fmt.Sprintf("legacy|%d", height)
}

func proposalVoteKey(height uint64, round uint32, blockHash string, txMerkle string, stateRoot string) string {
	// ProposalID / consensus identity must remain round-aware even when the
	// underlying block hash stays stable across retries.
	return fmt.Sprintf("%d|%d|%s|%s|%s", height, round, blockHash, txMerkle, stateRoot)
}

func proposalVoteKeyParts(proposalKey string) (uint64, uint32, string, string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(proposalKey), "|", 5)
	if len(parts) != 5 {
		return 0, 0, "", "", "", false
	}
	height, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, 0, "", "", "", false
	}
	round64, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil {
		return 0, 0, "", "", "", false
	}
	return height, uint32(round64), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3]), strings.TrimSpace(parts[4]), true
}

func proposalVoteKeyRound(proposalKey string) (uint32, bool) {
	_, round, _, _, _, ok := proposalVoteKeyParts(proposalKey)
	return round, ok
}

func execEpochChoiceSignerKey(signer string, proposalKey string) string {
	signer = normalizeValidatorID(signer)
	if signer == "" {
		return ""
	}
	_ = proposalKey
	return signer
}

func proposalExecKey(proposalKey string, execHash string) string {
	if proposalKey == "" {
		return execHash
	}
	return fmt.Sprintf("%s|%s", proposalKey, execHash)
}

func execPoolScopeKey(epoch uint64, proposalKey string) string {
	proposalKey = strings.TrimSpace(proposalKey)
	if proposalKey == "" {
		return legacyProposalVoteKey(epoch)
	}
	if strings.HasPrefix(proposalKey, "legacy|") {
		return proposalKey
	}
	parts := strings.SplitN(proposalKey, "|", 5)
	if len(parts) != 5 {
		return proposalKey
	}
	heightPart := strings.TrimSpace(parts[0])
	blockHash := strings.TrimSpace(parts[2])
	if blockHash == "" {
		return proposalKey
	}
	if heightPart == "" {
		heightPart = fmt.Sprintf("%d", epoch)
	}
	return fmt.Sprintf("block|%s|%s", heightPart, blockHash)
}

func execPoolResultKey(epoch uint64, proposalKey string, execHash string) string {
	return proposalExecKey(execPoolScopeKey(epoch, proposalKey), execHash)
}

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

func acceptedProposalHeightKey(height uint64) string {
	return fmt.Sprintf("%d", height)
}

func proposalRoundGap(currentRound uint32, incomingRound uint32) uint32 {
	if incomingRound <= currentRound {
		return 0
	}
	return incomingRound - currentRound
}

func (n *Node) acceptedProposalVoteCountLocked(epoch uint64, proposalKey string) int {
	if n == nil || epoch == 0 || proposalKey == "" {
		return 0
	}
	scopeKey := execPoolScopeKey(epoch, proposalKey)
	count := 0
	if n.execSignerSeen != nil {
		if byProposal, ok := n.execSignerSeen[epoch]; ok {
			count = len(byProposal[scopeKey])
		}
	}
	if n.acceptedProposalBlocks != nil {
		if block, ok := n.acceptedProposalBlocks[proposalKey]; ok && block.ID == epoch {
			execHash := strings.TrimSpace(block.StateRoot)
			if execHash == "" {
				execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
			}
			if execHash != "" {
				if global := getExecCountGlobal(epoch, proposalKey, execHash, block.MempoolRoot); global > count {
					count = global
				}
			}
			if n.Consensus != nil && strings.TrimSpace(block.BlockHash) != "" {
				n.Consensus.mu.Lock()
				if votes := n.Consensus.ExecVotes[strings.TrimSpace(block.BlockHash)]; len(votes) > count {
					count = len(votes)
				}
				n.Consensus.mu.Unlock()
			}
		}
	}
	return count
}

func (n *Node) proposalVoteCount(block Block) int {
	if n == nil || block.ID == 0 {
		return 0
	}
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		return 0
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, execHash)
	if proposalKey == "" {
		return 0
	}
	return getExecCountGlobal(block.ID, proposalKey, execHash, block.MempoolRoot)
}

func (n *Node) proposalHasExecutionQuorum(block Block) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	required := n.executionQuorumRequired(block.ID)
	if required == 0 {
		validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
		required = strictExecSupermajority(len(validators))
	}
	if required == 0 {
		return false
	}
	return n.proposalVoteCount(block) >= required
}

func (n *Node) proposalAlreadySeenLocked(block Block) bool {
	if n == nil || block.ID == 0 || n.acceptedProposalBlocks == nil {
		return false
	}
	snap := proposalSnapshotFromBlock(block)
	if snap.ProposalKey == "" {
		return false
	}
	_, ok := n.acceptedProposalBlocks[snap.ProposalKey]
	return ok
}

func (n *Node) proposalShouldStayLocked(block Block, voteCount int) (bool, string) {
	if block.ID != 0 {
		required := n.executionQuorumRequired(block.ID)
		if required == 0 {
			validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
			required = strictExecSupermajority(len(validators))
		}
		if required > 0 && voteCount >= required {
			return true, "quorum_locked"
		}
		if n.proposalHasExecutionQuorum(block) {
			return true, "quorum_locked"
		}
	}
	return false, ""
}

func (n *Node) proposalMatchesLocalExecution(block Block) (bool, string) {
	if n == nil || block.ID == 0 {
		return false, ""
	}
	expected := strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	if expected == "" {
		return false, ""
	}
	actual := strings.TrimSpace(block.StateRoot)
	return actual != "" && strings.EqualFold(actual, expected), expected
}

func proposalHasObservedExecutionProof(block Block, projectedVotes int) bool {
	if block.ID == 0 {
		return false
	}
	return projectedVotes > 0
}

func (n *Node) acceptedProposalVoteLockForRound(epoch uint64, incomingRound uint32) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	heightKey := acceptedProposalHeightKey(epoch)
	currentKey := strings.TrimSpace(n.acceptedProposal[heightKey])
	if currentKey == "" {
		return Block{}, 0, false, ""
	}
	block, ok := n.acceptedProposalBlocks[currentKey]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	if incomingRound <= block.Round {
		return Block{}, 0, false, ""
	}
	votes := n.acceptedProposalVoteCountLocked(epoch, currentKey)
	if votes <= 0 {
		return Block{}, 0, false, ""
	}
	if n.acceptedProposalSoftLockExpired(epoch) {
		return Block{}, votes, false, ""
	}
	if executionOK, _ := n.proposalMatchesLocalExecution(block); !executionOK {
		return Block{}, votes, false, ""
	}
	return block, votes, true, "accepted_vote_lock"
}

func (n *Node) executionQuorumRequiredForEpoch(epoch uint64) int {
	if n == nil || epoch == 0 {
		return 0
	}
	required := n.executionQuorumRequired(epoch)
	if required == 0 {
		validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
		required = strictExecSupermajority(len(validators))
	}
	return required
}

func (n *Node) higherRoundQuorumSeenForProposal(epoch uint64, lockedBlock Block, incoming Block, projectedVotes int) (int, int, bool) {
	if n == nil || epoch == 0 || lockedBlock.ID != epoch || incoming.ID != epoch {
		return 0, 0, false
	}
	if !proposalConflictsWithAcceptedLock(lockedBlock, incoming) {
		return 0, 0, false
	}
	if incoming.Round <= lockedBlock.Round {
		return 0, 0, false
	}
	required := n.executionQuorumRequiredForEpoch(epoch)
	if required == 0 {
		return 0, 0, false
	}
	votes := projectedVotes
	if votes < 0 {
		votes = n.proposalVoteCount(incoming)
	}
	return votes, required, votes >= required
}

func (n *Node) proposalShouldHoldAgainstIncomingLocked(epoch uint64, proposalKey string, block Block, voteCount int, incomingRound uint32) (bool, string) {
	if keep, reason := n.proposalShouldStayLocked(block, voteCount); keep {
		return true, reason
	}
	if block.ID == 0 || proposalKey == "" || incomingRound == 0 {
		return false, ""
	}
	if n.acceptedProposalSoftLockExpired(epoch) {
		return false, ""
	}
	if proposalRoundGap(block.Round, incomingRound) > execProposalSwitchRoundGap {
		if keep, reason := n.recentLeaderProposalHoldState(epoch, block, incomingRound); keep {
			return true, reason
		}
		return false, ""
	}
	if keep, reason := n.recentLeaderProposalHoldState(epoch, block, incomingRound); keep {
		return true, reason
	}
	return false, ""
}

func (n *Node) acceptedProposalSoftLockExpired(epoch uint64) bool {
	if n == nil || epoch == 0 {
		return false
	}
	threshold := blockProductionStaleThreshold()
	if threshold <= 0 {
		return false
	}
	return n.commitStallDuration() >= threshold
}

func (n *Node) acceptedProposalLockState(epoch uint64) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	key := strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, 0, false, ""
	}
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	voteCount := n.acceptedProposalVoteCountLocked(epoch, key)
	keep, reason := n.proposalShouldHoldAgainstIncomingLocked(epoch, key, block, voteCount, 0)
	return block, voteCount, keep, reason
}

func (n *Node) acceptedProposalHoldStateForIncomingRound(epoch uint64, incomingRound uint32) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	key := strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, 0, false, ""
	}
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	voteCount := n.acceptedProposalVoteCountLocked(epoch, key)
	keep, reason := n.proposalShouldHoldAgainstIncomingLocked(epoch, key, block, voteCount, incomingRound)
	return block, voteCount, keep, reason
}

func (n *Node) quorumLockedProposalLockState(epoch uint64) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.quorumLockedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, 0, false, ""
	}
	key := strings.TrimSpace(n.quorumLockedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, 0, false, ""
	}
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, 0, false, ""
	}
	voteCount := n.acceptedProposalVoteCountLocked(epoch, key)
	keep, reason := n.proposalShouldStayLocked(block, voteCount)
	return block, voteCount, keep, reason
}

func (n *Node) quorumLockedProposalHoldStateForIncomingRound(epoch uint64, incoming Block, projectedVotes int) (Block, int, bool, string) {
	if n == nil || epoch == 0 {
		return Block{}, 0, false, ""
	}
	block, voteCount, keep, reason := n.quorumLockedProposalLockState(epoch)
	if !keep {
		return Block{}, 0, false, ""
	}
	if incoming.ID == 0 {
		return block, voteCount, true, reason
	}
	if votes, required, unlock := n.higherRoundQuorumSeenForProposal(epoch, block, incoming, projectedVotes); unlock {
		if DebugConsensus {
			fmt.Printf("[EXEC-UNLOCK] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d required=%d reason=higher_round_quorum_seen\n",
				epoch,
				block.Round,
				ShortHash(block.BlockHash),
				incoming.Round,
				ShortHash(incoming.BlockHash),
				votes,
				required,
			)
		}
		return block, voteCount, false, "higher_round_quorum_seen"
	}
	return block, voteCount, true, reason
}

func proposalConflictsWithAcceptedLock(locked Block, incoming Block) bool {
	if locked.ID == 0 || incoming.ID == 0 {
		return false
	}
	lockedHash := strings.TrimSpace(locked.BlockHash)
	incomingHash := strings.TrimSpace(incoming.BlockHash)
	if lockedHash == "" || incomingHash == "" {
		return false
	}
	return !strings.EqualFold(lockedHash, incomingHash)
}

func (n *Node) pruneAcceptedProposalBlocksForEpochLocked(epoch uint64) {
	if n == nil || epoch == 0 || n.acceptedProposalBlocks == nil {
		return
	}
	type proposalEntry struct {
		key   string
		round uint32
	}
	entries := make([]proposalEntry, 0, len(n.acceptedProposalBlocks))
	currentKey := ""
	quorumLockedKey := ""
	if n.acceptedProposal != nil {
		currentKey = strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	}
	if n.quorumLockedProposal != nil {
		quorumLockedKey = strings.TrimSpace(n.quorumLockedProposal[acceptedProposalHeightKey(epoch)])
	}
	for key, block := range n.acceptedProposalBlocks {
		if block.ID != epoch {
			continue
		}
		entries = append(entries, proposalEntry{key: key, round: block.Round})
	}
	if len(entries) <= execRecentProposalWindow {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		iLocked := entries[i].key == quorumLockedKey
		jLocked := entries[j].key == quorumLockedKey
		if iLocked != jLocked {
			return iLocked
		}
		iCurrent := entries[i].key == currentKey
		jCurrent := entries[j].key == currentKey
		if iCurrent != jCurrent {
			return iCurrent
		}
		if entries[i].round != entries[j].round {
			return entries[i].round > entries[j].round
		}
		return entries[i].key < entries[j].key
	})
	keep := make(map[string]bool, execRecentProposalWindow+1)
	for i := 0; i < len(entries) && i < execRecentProposalWindow; i++ {
		keep[entries[i].key] = true
	}
	if currentKey != "" {
		keep[currentKey] = true
	}
	if quorumLockedKey != "" {
		keep[quorumLockedKey] = true
	}
	for _, entry := range entries {
		if !keep[entry.key] {
			delete(n.acceptedProposalBlocks, entry.key)
		}
	}
}

func (n *Node) registerAcceptedProposalBlockLocked(block Block) execProposalSnapshot {
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

func (n *Node) setAcceptedProposalLocked(block Block, reason string, force bool) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	snap := n.registerAcceptedProposalBlockLocked(block)
	if snap.ProposalKey == "" {
		return false
	}
	if n.acceptedProposal == nil {
		n.acceptedProposal = make(map[string]string)
	}
	heightKey := acceptedProposalHeightKey(block.ID)
	currentKey := strings.TrimSpace(n.acceptedProposal[heightKey])
	if currentKey == snap.ProposalKey {
		return false
	}
	prevRound := uint32(0)
	prevBlockHash := ""
	prevVotes := 0
	prevBlock := Block{}
	if currentKey != "" {
		if currentBlock, ok := n.acceptedProposalBlocks[currentKey]; ok {
			prevBlock = currentBlock
			prevRound = currentBlock.Round
			prevBlockHash = currentBlock.BlockHash
		}
		prevVotes = n.acceptedProposalVoteCountLocked(block.ID, currentKey)
		keepCurrent, keepReason := n.proposalShouldHoldAgainstIncomingLocked(block.ID, currentKey, prevBlock, prevVotes, block.Round)
		prevExecutionOK, prevExpectedRoot := n.proposalMatchesLocalExecution(prevBlock)
		softLockExpired := n.acceptedProposalSoftLockExpired(block.ID)
		incomingVotes := n.proposalVoteCount(block)
		if !force {
			switch {
			case block.Round <= prevRound:
				return false
			case prevBlock.ID == block.ID && prevVotes > 0 && !softLockExpired && !proposalHasObservedExecutionProof(block, incomingVotes):
				if DebugConsensus {
					fmt.Printf("[EXEC-PROPOSAL-KEEP] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d reason=accepted_vote_lock\n",
						block.ID,
						prevRound,
						ShortHash(prevBlockHash),
						block.Round,
						ShortHash(block.BlockHash),
						prevVotes,
					)
				}
				return false
			case prevBlock.ID == block.ID && prevRound > 0 && !prevExecutionOK:
				if DebugConsensus {
					fmt.Printf("[EXEC-PROPOSAL-RELEASE] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d reason=locked_state_root_mismatch locked_root=%s expected_root=%s\n",
						block.ID,
						prevRound,
						ShortHash(prevBlockHash),
						block.Round,
						ShortHash(block.BlockHash),
						prevVotes,
						ShortHash(prevBlock.StateRoot),
						ShortHash(prevExpectedRoot),
					)
				}
			case prevBlock.ID == block.ID && prevRound > 0 && !softLockExpired && !proposalHasObservedExecutionProof(block, n.proposalVoteCount(block)):
				if DebugConsensus {
					fmt.Printf("[EXEC-PROPOSAL-KEEP] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d reason=sticky_proposal_lock\n",
						block.ID,
						prevRound,
						ShortHash(prevBlockHash),
						block.Round,
						ShortHash(block.BlockHash),
						prevVotes,
					)
				}
				return false
			case prevBlock.ID == block.ID && prevRound > 0 && softLockExpired && !proposalHasObservedExecutionProof(block, n.proposalVoteCount(block)):
				if DebugConsensus {
					fmt.Printf("[EXEC-PROPOSAL-RELEASE] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d reason=sticky_soft_lock_expired\n",
						block.ID,
						prevRound,
						ShortHash(prevBlockHash),
						block.Round,
						ShortHash(block.BlockHash),
						prevVotes,
					)
				}
			case keepCurrent:
				if !prevExecutionOK {
					if DebugConsensus {
						fmt.Printf("[EXEC-PROPOSAL-RELEASE] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d reason=held_state_root_mismatch hold_reason=%s locked_root=%s expected_root=%s\n",
							block.ID,
							prevRound,
							ShortHash(prevBlockHash),
							block.Round,
							ShortHash(block.BlockHash),
							prevVotes,
							keepReason,
							ShortHash(prevBlock.StateRoot),
							ShortHash(prevExpectedRoot),
						)
					}
					break
				}
				if DebugConsensus {
					fmt.Printf("[EXEC-PROPOSAL-KEEP] height=%d locked_round=%d locked_block=%s incoming_round=%d incoming_block=%s votes=%d reason=%s\n",
						block.ID,
						prevRound,
						ShortHash(prevBlockHash),
						block.Round,
						ShortHash(block.BlockHash),
						prevVotes,
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

func (n *Node) setQuorumLockedProposalLocked(block Block, reason string, voteCount int, required int) bool {
	if n == nil || block.ID == 0 {
		return false
	}
	snap := n.registerAcceptedProposalBlockLocked(block)
	if snap.ProposalKey == "" {
		return false
	}
	if n.quorumLockedProposal == nil {
		n.quorumLockedProposal = make(map[string]string)
	}
	heightKey := acceptedProposalHeightKey(block.ID)
	currentKey := strings.TrimSpace(n.quorumLockedProposal[heightKey])
	if currentKey == snap.ProposalKey {
		return false
	}
	prevRound := uint32(0)
	prevBlockHash := ""
	if currentKey != "" {
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

func (n *Node) noteObservedProposal(block Block) {
	if n == nil || block.ID == 0 {
		return
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	n.registerAcceptedProposalBlockLocked(block)
	_ = n.setAcceptedProposalLocked(block, "observed", false)
}

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
	heightKey := acceptedProposalHeightKey(block.ID)
	currentKey := strings.TrimSpace(n.acceptedProposal[heightKey])
	if currentKey == "" {
		return n.setAcceptedProposalLocked(block, "vote_observed", true)
	}
	nextKey := proposalSnapshotFromBlock(block).ProposalKey
	if currentKey == nextKey {
		return false
	}
	currentVotes := n.acceptedProposalVoteCountLocked(block.ID, currentKey)
	currentBlock := n.acceptedProposalBlocks[currentKey]
	if keepCurrent, _ := n.proposalShouldStayLocked(currentBlock, currentVotes); keepCurrent {
		return false
	}
	if block.Round <= currentBlock.Round {
		return false
	}
	return n.setAcceptedProposalLocked(block, "vote_observed", true)
}

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

func (n *Node) clearAcceptedProposalIfBlock(epoch uint64, block Block, reason string) bool {
	if n == nil || epoch == 0 {
		return false
	}
	snap := proposalSnapshotFromBlock(block)
	proposalKey := strings.TrimSpace(snap.ProposalKey)
	if proposalKey == "" {
		return false
	}
	heightKey := acceptedProposalHeightKey(epoch)
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
		if _, ok := n.acceptedProposalBlocks[proposalKey]; ok {
			delete(n.acceptedProposalBlocks, proposalKey)
			cleared = true
		}
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

func executionCommitFailureInvalidatesProposal(err error) bool {
	if err == nil {
		return false
	}
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

func (n *Node) clearLocalExecutionVoteMarkerForProposal(epoch uint64, proposalKey string) bool {
	if n == nil || epoch == 0 || strings.TrimSpace(proposalKey) == "" {
		return false
	}
	scopedKey := execPoolScopeKey(epoch, proposalKey)
	cleared := false
	n.execResultsMu.Lock()
	if byRound := n.localExecVoteByRound[epoch]; len(byRound) > 0 {
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

func (n *Node) invalidateExecutionProposalAfterCommitFailure(epoch uint64, proposal Block, err error) {
	if n == nil || epoch == 0 || proposal.ID != epoch || !executionCommitFailureInvalidatesProposal(err) {
		return
	}
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		reason = "commit_verification_failed"
	}
	proposalKey := proposalVoteKey(proposal.ID, proposal.Round, proposal.BlockHash, proposal.MempoolRoot, proposal.StateRoot)
	if proposalKey == "" {
		proposalKey = proposalSnapshotFromBlock(proposal).ProposalKey
	}
	clearedProposal := n.clearAcceptedProposalIfBlock(epoch, proposal, reason)
	clearedLeader := n.clearLeaderBlockIfBlock(epoch, proposal)
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

func (n *Node) acceptedProposalBlock(epoch uint64) (Block, bool) {
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.acceptedProposal == nil || n.acceptedProposalBlocks == nil {
		return Block{}, false
	}
	key := strings.TrimSpace(n.acceptedProposal[acceptedProposalHeightKey(epoch)])
	if key == "" {
		return Block{}, false
	}
	block, ok := n.acceptedProposalBlocks[key]
	if !ok || block.ID != epoch {
		return Block{}, false
	}
	return block, true
}

func (n *Node) executionVoteTargetBlock(epoch uint64) (Block, bool) {
	if block, _, keep, _ := n.quorumLockedProposalLockState(epoch); keep {
		return block, true
	}
	if block, ok := n.acceptedProposalBlock(epoch); ok {
		return block, true
	}
	return n.getLeaderBlock(epoch)
}

func (n *Node) candidateProposalBlocksForEpoch(epoch uint64) []Block {
	if n == nil || epoch == 0 {
		return nil
	}
	seen := make(map[string]bool)
	blocks := make([]Block, 0, execRecentProposalWindow+1)
	if block, _, keep, _ := n.quorumLockedProposalLockState(epoch); keep {
		snap := proposalSnapshotFromBlock(block)
		if snap.ProposalKey != "" {
			seen[snap.ProposalKey] = true
		}
		blocks = append(blocks, block)
	}
	if block, ok := n.acceptedProposalBlock(epoch); ok {
		snap := proposalSnapshotFromBlock(block)
		if snap.ProposalKey != "" {
			seen[snap.ProposalKey] = true
		}
		blocks = append(blocks, block)
	}

	n.execResultsMu.Lock()
	extras := make([]Block, 0, len(n.acceptedProposalBlocks))
	for key, block := range n.acceptedProposalBlocks {
		if block.ID != epoch || seen[key] {
			continue
		}
		seen[key] = true
		extras = append(extras, block)
	}
	n.execResultsMu.Unlock()

	if committedBlock, ok := n.Blockchain.GetBlock(epoch); ok && committedBlock.ID == epoch {
		snap := proposalSnapshotFromBlock(committedBlock)
		if snap.ProposalKey != "" && !seen[snap.ProposalKey] {
			seen[snap.ProposalKey] = true
			extras = append(extras, committedBlock)
		}
	}

	if leaderBlock, ok := n.getLeaderBlock(epoch); ok && leaderBlock.ID == epoch {
		snap := proposalSnapshotFromBlock(leaderBlock)
		if snap.ProposalKey != "" && !seen[snap.ProposalKey] {
			seen[snap.ProposalKey] = true
			extras = append(extras, leaderBlock)
		}
	}

	sort.Slice(extras, func(i, j int) bool {
		if extras[i].Round != extras[j].Round {
			return extras[i].Round > extras[j].Round
		}
		return proposalSnapshotFromBlock(extras[i]).ProposalKey < proposalSnapshotFromBlock(extras[j]).ProposalKey
	})
	blocks = append(blocks, extras...)
	return blocks
}

func (n *Node) resolveExecutionVoteProposal(height uint64, res ExecutionResultMsg) (Block, execProposalSnapshot, bool) {
	candidates := n.candidateProposalBlocksForEpoch(height)
	if len(candidates) == 0 {
		return Block{}, execProposalSnapshot{}, false
	}
	if strings.TrimSpace(res.BlockHashHint) == "" && res.RoundHint == 0 && strings.TrimSpace(res.TxMerkle) == "" {
		snap := proposalSnapshotFromBlock(candidates[0])
		return candidates[0], snap, snap.ProposalKey != ""
	}
	for _, block := range candidates {
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

func (n *Node) proposalSnapshotForEpoch(height uint64) (execProposalSnapshot, bool) {
	block, ok := n.executionVoteTargetBlock(height)
	if !ok || block.ID != height {
		return execProposalSnapshot{}, false
	}
	snap := proposalSnapshotFromBlock(block)
	if strings.TrimSpace(snap.ProposalKey) == "" {
		return execProposalSnapshot{}, false
	}
	return snap, true
}

func (n *Node) prepareExecutionBroadcastForBlock(block Block, execHash string, txMerkle string) (execBroadcastContext, bool) {
	heightHint := block.ID
	currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
	ctx := execBroadcastContext{
		HeightHint:          heightHint,
		ExecHash:            strings.TrimSpace(execHash),
		TxMerkle:            strings.TrimSpace(txMerkle),
		RuntimeLedgerHash:   currentRuntimeLedgerHash,
		ExecutionLedgerHash: currentExecutionLedgerHash,
		TipHeight:           tipHeight,
		TipHash:             tipHash,
	}
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

	snap := proposalSnapshotFromBlock(block)
	if strings.TrimSpace(snap.ProposalKey) == "" {
		return logDefer("missing_proposal")
	}
	ctx.RoundHint = snap.Round
	ctx.BlockHashHint = strings.TrimSpace(snap.BlockHash)
	ctx.TxCount = len(block.Transactions)
	ctx.PrevHash = strings.TrimSpace(block.PrevHash)
	ctx.TxMerkle = strings.TrimSpace(block.MempoolRoot)
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

func (n *Node) prepareExecutionBroadcast(heightHint uint64, execHash string, txMerkle string) (execBroadcastContext, bool) {
	if heightHint == 0 {
		heightHint = n.currentEpoch()
	}
	block, ok := n.executionVoteTargetBlock(heightHint)
	if !ok || block.ID != heightHint {
		currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
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

func (n *Node) currentProposalVoteKey(height uint64) string {
	if snap, ok := n.proposalSnapshotForEpoch(height); ok && snap.ProposalKey != "" {
		return snap.ProposalKey
	}
	return legacyProposalVoteKey(height)
}

func voteProposalKey(res ExecutionResultMsg) string {
	if res.HeightHint == 0 {
		return ""
	}
	if strings.TrimSpace(res.BlockHashHint) != "" {
		return proposalVoteKey(res.HeightHint, res.RoundHint, res.BlockHashHint, res.TxMerkle, res.ExecHash)
	}
	return legacyProposalVoteKey(res.HeightHint)
}

func voteBelongsToCurrentProposal(res ExecutionResultMsg, snap execProposalSnapshot) bool {
	if res.HeightHint == 0 || snap.Epoch == 0 {
		return false
	}
	if res.HeightHint != snap.Epoch {
		return false
	}
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
	if blocked, reason, _ := n.consensusSyncGateForHeight(height); blocked {
		if reason == "" {
			reason = "syncing"
		}
		return false, reason
	}
	if ready, reason := n.validatorParticipationGateStatus(height); !ready {
		return false, reason
	}
	return true, ""
}

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
		syncing := n.Consensus.Syncing
		syncInFlight := n.Consensus.syncInFlight
		paused := n.Consensus.Paused
		target := n.Consensus.SyncTarget
		n.Consensus.mu.Unlock()
		if syncInFlight {
			return false, "sync_in_progress"
		}
		if syncing {
			return false, "syncing"
		}
		if target > 0 && n.Blockchain != nil {
			localHeight := n.Blockchain.Height()
			if target > localHeight && !nearSyncTip(localHeight, target) {
				return false, fmt.Sprintf("lagging_local_%d_target_%d", localHeight, target)
			}
		}
		if paused {
			return false, "consensus_paused"
		}
	}
	if blocked, reason, _ := n.consensusSyncGateForHeight(height); blocked {
		if reason == "" {
			reason = "syncing"
		}
		return false, reason
	}
	return true, "ready"
}

func (n *Node) maybeBroadcastExecutionVoteForBlock(block Block, trigger string) bool {
	return n.broadcastExecutionVoteForBlock(block, trigger, false, false)
}

func (n *Node) maybeRebroadcastExecutionVoteForBlock(block Block, trigger string) bool {
	return n.broadcastExecutionVoteForBlock(block, trigger, true, true)
}

func (n *Node) broadcastExecutionVoteForBlock(block Block, trigger string, force bool, requirePriorLocalVote bool) bool {
	if n == nil || block.ID == 0 || n.isShuttingDown() {
		return false
	}
	trigger = strings.TrimSpace(trigger)
	if requirePriorLocalVote {
		snap := proposalSnapshotFromBlock(block)
		if snap.ProposalKey == "" || !n.hasExecBroadcastedByValidator(block.ID, snap.ProposalKey, n.ID) {
			return false
		}
		if !n.shouldRebroadcastExec(block.ID, execVoteRebroadcastCooldown) {
			return false
		}
	}
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
	n.setLogicalTick(block.ID, TickExec)
	n.broadcastExecutionResultForBlockInternal(block, execHash, block.MempoolRoot, force)
	return true
}

func (n *Node) maybeBroadcastCurrentLeaderExecutionVote(trigger string) bool {
	if n == nil || n.isShuttingDown() {
		return false
	}
	epoch := n.currentEpoch()
	block, ok := n.executionVoteTargetBlock(epoch)
	if !ok || block.ID != epoch {
		return false
	}
	return n.maybeBroadcastExecutionVoteForBlock(block, trigger)
}

func (n *Node) onLeaderProposalReplaced(epoch uint64, oldBlock Block, newBlock Block) {
	if n == nil || epoch == 0 {
		return
	}
	oldKey := proposalVoteKey(epoch, oldBlock.Round, oldBlock.BlockHash, oldBlock.MempoolRoot, oldBlock.StateRoot)
	newKey := proposalVoteKey(epoch, newBlock.Round, newBlock.BlockHash, newBlock.MempoolRoot, newBlock.StateRoot)
	if oldKey == "" || oldKey == newKey {
		return
	}
	if DebugConsensus {
		fmt.Printf("[EXEC-PROPOSAL] epoch=%d round=%d block=%s state=%s\n",
			epoch, newBlock.Round, ShortHash(newBlock.BlockHash), ShortHash(newBlock.StateRoot))
	}
}

func execResultSignBytes(heightHint uint64, execHash string, txMerkle string) []byte {
	return []byte(fmt.Sprintf("%d|%s|%s", heightHint, execHash, txMerkle))
}

func execResultSignBytesV2(heightHint uint64, roundHint uint32, blockHashHint string, execHash string, txMerkle string) []byte {
	return []byte(fmt.Sprintf("%d|%d|%s|%s|%s", heightHint, roundHint, blockHashHint, execHash, txMerkle))
}

func verifyExecutionResultSignature(res ExecutionResultMsg, candidates []ed25519.PublicKey, sig []byte) bool {
	if len(candidates) == 0 || len(sig) == 0 {
		return false
	}
	isV2 := res.SigVersion == execResultSigVersionV2 || strings.TrimSpace(res.BlockHashHint) != ""
	if isV2 {
		signBytes := execResultSignBytesV2(res.HeightHint, res.RoundHint, res.BlockHashHint, res.ExecHash, res.TxMerkle)
		for _, pub := range candidates {
			if ed25519.Verify(pub, signBytes, sig) {
				return true
			}
		}
		return false
	}

	signBytes := execResultSignBytes(res.HeightHint, res.ExecHash, res.TxMerkle)
	for _, pub := range candidates {
		if ed25519.Verify(pub, signBytes, sig) {
			return true
		}
	}
	return false
}

func (n *Node) publishExecutionResult(ctx execBroadcastContext, force bool) {
	if n == nil || n.isShuttingDown() {
		return
	}
	heightHint := ctx.HeightHint
	execHash := ctx.ExecHash
	txMerkle := ctx.TxMerkle
	proposalKey := ctx.ProposalKey
	roundHint := ctx.RoundHint
	blockHashHint := ctx.BlockHashHint

	if !n.allowLocalExecutionVoteRound(heightHint, roundHint, proposalKey) {
		return
	}
	if !force {
		if !n.markExecBroadcastedByValidator(heightHint, proposalKey, n.ID) {
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

	sigVersion := execResultSigVersionV1
	sigBytes := execResultSignBytes(heightHint, execHash, txMerkle)
	if blockHashHint != "" {
		sigVersion = execResultSigVersionV2
		sigBytes = execResultSignBytesV2(heightHint, roundHint, blockHashHint, execHash, txMerkle)
	}
	signature := ""
	if len(n.ValidatorKey.PrivateKey) == ed25519.PrivateKeySize {
		sig := ed25519.Sign(n.ValidatorKey.PrivateKey, sigBytes)
		signature = hex.EncodeToString(sig)
	}

	msg := Message{
		Type: MsgExecutionResult,
		Data: MustJSON(ExecutionResultMsg{
			HeightHint:    heightHint,
			RoundHint:     roundHint,
			BlockHashHint: blockHashHint,
			SigVersion:    sigVersion,
			ExecHash:      execHash,
			TxMerkle:      txMerkle,
			Signer:        n.ID,
			Signature:     signature,
		}),
	}
	// Production safety: do not rely on pubsub loopback for the local
	// validator's own execution vote. Record it immediately so transport
	// hiccups cannot stall quorum formation.
	if !force || !n.hasExecSignerSeenForProposal(heightHint, proposalKey, n.ID) {
		if n.isShuttingDown() {
			return
		}
		n.handleExecutionResultMsg(ExecutionResultMsg{
			HeightHint:    heightHint,
			RoundHint:     roundHint,
			BlockHashHint: blockHashHint,
			SigVersion:    sigVersion,
			ExecHash:      execHash,
			TxMerkle:      txMerkle,
			Signer:        n.ID,
			Signature:     signature,
		})
	}
	n.fanoutConsensusMessageToPeers(msg)
	n.noteExecBroadcastActivity(heightHint)

	data, _ := MarshalP2PMessage(msg)
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

func (n *Node) broadcastExecutionResultForBlockInternal(block Block, execHash string, txMerkle string, force bool) {
	ctx, ok := n.prepareExecutionBroadcastForBlock(block, execHash, txMerkle)
	if !ok {
		return
	}
	n.publishExecutionResult(ctx, force)
}

func (n *Node) broadcastExecutionResultInternal(heightHint uint64, execHash string, txMerkle string, force bool) {
	if n.ConsensusTopic == nil && n.ValidatorTopic == nil {
		return
	}
	if !n.canParticipateInConsensusNow() {
		// Rule 2: candidates cannot publish execution hashes.
		return
	}
	if validatorOnboardingStrictActivationEnabled() {
		if active, reason := n.selfActiveValidatorAt(heightHint); !active {
			if DebugConsensus {
				fmt.Printf("[PROPOSAL-GATE] skipped exec-broadcast validator=%s height=%d reason=%s\n", ShortID(n.ID), heightHint, reason)
			}
			return
		}
	}
	if ConsensusProposeRequiresSyncReady {
		if ready, reason := n.syncReadyForConsensus(heightHint); !ready {
			if DebugConsensus {
				fmt.Printf("[PROPOSAL-GATE] skipped exec-broadcast validator=%s height=%d reason=%s\n", ShortID(n.ID), heightHint, reason)
			}
			return
		}
	}
	if ConsensusPostBlockSafeModeEnabled {
		if active, _, _ := n.postBlockSafeModeState(heightHint); active {
			if DebugConsensus {
				fmt.Printf("[PROPOSAL-GATE] skipped exec-broadcast validator=%s height=%d reason=safe_mode_active\n", ShortID(n.ID), heightHint)
			}
			return
		}
	}
	ctx, ok := n.prepareExecutionBroadcast(heightHint, execHash, txMerkle)
	if !ok {
		return
	}
	n.publishExecutionResult(ctx, force)
}

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
	if last, ok := n.execRebroadcastAt[epoch]; ok {
		if time.Since(last) < cooldown {
			return false
		}
	}
	n.execRebroadcastAt[epoch] = time.Now()
	return true
}

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

func executionVoteNetworkIngressID(res ExecutionResultMsg) string {
	signer := normalizeValidatorID(res.Signer)
	if res.HeightHint == 0 || signer == "" {
		return ""
	}
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

func (n *Node) markStaleExecutionVoteNetworkIngress(res ExecutionResultMsg) bool {
	if n == nil {
		return false
	}
	voteID := executionVoteNetworkIngressID(res)
	if voteID == "" {
		return false
	}
	now := time.Now()
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if n.execVoteStaleIngressSeen == nil {
		n.execVoteStaleIngressSeen = make(map[string]time.Time)
	}
	if last, ok := n.execVoteStaleIngressSeen[voteID]; ok && now.Sub(last) <= execVoteStaleIngressTTL {
		return true
	}
	n.execVoteStaleIngressSeen[voteID] = now
	if len(n.execVoteStaleIngressSeen) > ExecVoteReplayMaxKeys {
		cutoff := now.Add(-execVoteStaleIngressTTL)
		for key, seenAt := range n.execVoteStaleIngressSeen {
			if seenAt.Before(cutoff) {
				delete(n.execVoteStaleIngressSeen, key)
			}
		}
	}
	return false
}

func executionVoteTipLag(currentEpoch uint64, voteHeight uint64) (uint64, bool) {
	if currentEpoch == 0 || voteHeight == 0 || voteHeight >= currentEpoch {
		return 0, false
	}
	// currentEpoch() returns the next height, so subtract one to compare
	// ingress lag against the node's current tip.
	return currentEpoch - voteHeight - 1, true
}

func executionVoteTooFarBehind(currentEpoch uint64, voteHeight uint64) bool {
	lag, ok := executionVoteTipLag(currentEpoch, voteHeight)
	return ok && lag > execVoteStaleLagBlocks
}

func benignExecutionVoteIngressReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "ignored_late_vote", "ignored_late_vote_cached":
		return true
	default:
		return false
	}
}

func (n *Node) allowExecutionVoteNetworkIngress(res ExecutionResultMsg) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	res.Signer = normalizeValidatorID(res.Signer)
	if res.HeightHint == 0 || res.Signer == "" || strings.TrimSpace(res.ExecHash) == "" {
		return false, "invalid_vote"
	}
	currentEpoch := n.currentEpoch()
	if executionVoteTooFarBehind(currentEpoch, res.HeightHint) {
		if n.markStaleExecutionVoteNetworkIngress(res) {
			return false, "ignored_late_vote_cached"
		}
		return false, "ignored_late_vote"
	}
	voteID := executionVoteNetworkIngressID(res)
	if voteID == "" {
		return true, ""
	}
	now := time.Now()
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if n.execVoteIngressSeen == nil {
		n.execVoteIngressSeen = make(map[string]time.Time)
	}
	if last, ok := n.execVoteIngressSeen[voteID]; ok && now.Sub(last) <= execVoteReplayTTL {
		// Treat rebroadcasts as idempotent at network ingress. The proposal-aware
		// replay/signer guards later in processing are authoritative; rejecting
		// here can starve quorum if an earlier copy was only queued or otherwise
		// failed to resolve into a credited vote.
		return true, ""
	}
	n.execVoteIngressSeen[voteID] = now
	if len(n.execVoteIngressSeen) > ExecVoteReplayMaxKeys {
		cutoff := now.Add(-execVoteReplayTTL)
		for key, seenAt := range n.execVoteIngressSeen {
			if seenAt.Before(cutoff) {
				delete(n.execVoteIngressSeen, key)
			}
		}
	}
	return true, ""
}

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

func (n *Node) shouldForceExecutionVoteRebroadcast(epoch uint64, proposalKey string, votes int, cooldown time.Duration) bool {
	if n == nil || epoch == 0 || proposalKey == "" || votes <= 0 {
		return false
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	if cooldown <= 0 {
		cooldown = execVoteRebroadcastCooldown
	}
	now := time.Now()
	n.execRebroadcastMu.Lock()
	defer n.execRebroadcastMu.Unlock()
	if n.execRebroadcastState == nil {
		n.execRebroadcastState = make(map[uint64]execVoteRebroadcastState)
	}
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

func (n *Node) markExecBroadcasted(epoch uint64, proposalKey string, execHash string, txMerkle string) bool {
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	key := execBroadcastKey(proposalExecKey(proposalKey, execHash), txMerkle)
	n.commitMu.Lock()
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execBroadcasted == nil {
		n.execBroadcasted = make(map[uint64]map[string]bool)
	}
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

func (n *Node) markExecBroadcastedByValidator(epoch uint64, proposalKey string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	n.commitMu.Lock()
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execBroadcastedByValidator == nil {
		n.execBroadcastedByValidator = make(map[uint64]map[string]map[string]bool)
	}
	if _, ok := n.execBroadcastedByValidator[epoch]; !ok {
		n.execBroadcastedByValidator[epoch] = make(map[string]map[string]bool)
	}
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

func (n *Node) hasExecBroadcastedByValidator(epoch uint64, proposalKey string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if n == nil || epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execBroadcastedByValidator == nil {
		return false
	}
	if _, ok := n.execBroadcastedByValidator[epoch]; !ok {
		return false
	}
	if _, ok := n.execBroadcastedByValidator[epoch][proposalKey]; !ok {
		return false
	}
	return n.execBroadcastedByValidator[epoch][proposalKey][signer]
}

func (n *Node) markExecSignerSeenForProposal(epoch uint64, proposalKey string, signer string) bool {
	if epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	n.commitMu.Lock()
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execSignerSeen == nil {
		n.execSignerSeen = make(map[uint64]map[string]map[string]bool)
	}
	if _, ok := n.execSignerSeen[epoch]; !ok {
		n.execSignerSeen[epoch] = make(map[string]map[string]bool)
	}
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

func (n *Node) hasExecSignerSeenForProposal(epoch uint64, proposalKey string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if n == nil || epoch == 0 || proposalKey == "" || signer == "" {
		return false
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.execSignerSeen == nil {
		return false
	}
	if _, ok := n.execSignerSeen[epoch]; !ok {
		return false
	}
	if _, ok := n.execSignerSeen[epoch][proposalKey]; !ok {
		return false
	}
	return n.execSignerSeen[epoch][proposalKey][signer]
}

func (n *Node) allowLocalExecutionVoteRound(epoch uint64, round uint32, proposalKey string) bool {
	if n == nil || epoch == 0 || proposalKey == "" {
		return false
	}
	n.execResultsMu.Lock()
	defer n.execResultsMu.Unlock()
	if n.localExecVoteByRound == nil {
		n.localExecVoteByRound = make(map[uint64]map[uint32]string)
	}
	if _, ok := n.localExecVoteByRound[epoch]; !ok {
		n.localExecVoteByRound[epoch] = make(map[uint32]string)
	}
	existing := strings.TrimSpace(n.localExecVoteByRound[epoch][round])
	if existing == "" {
		sameKeySeen := false
		incomingScope := execPoolScopeKey(epoch, proposalKey)
		for existingRound, existingKey := range n.localExecVoteByRound[epoch] {
			existingKey = strings.TrimSpace(existingKey)
			if existingKey == "" {
				continue
			}
			if existingKey == proposalKey || execPoolScopeKey(epoch, existingKey) == incomingScope {
				sameKeySeen = true
				continue
			}
			if n.releaseStaleLocalExecutionVoteMarkerLocked(epoch, existingRound, round, existingKey, proposalKey) {
				delete(n.localExecVoteByRound[epoch], existingRound)
				continue
			}
			log.Printf("[EXEC-VOTE-GUARD] validator=%s height=%d round=%d action=skip_cross_round_equivocation existing_round=%d existing=%s incoming=%s",
				ShortID(n.ID),
				epoch,
				round,
				existingRound,
				existingKey,
				proposalKey,
			)
			return false
		}
		n.localExecVoteByRound[epoch][round] = proposalKey
		if sameKeySeen {
			return true
		}
		return true
	}
	if existing == proposalKey {
		return true
	}
	if n.releaseStaleLocalExecutionVoteMarkerLocked(epoch, round, round, existing, proposalKey) {
		n.localExecVoteByRound[epoch][round] = proposalKey
		return true
	}
	log.Printf("[EXEC-VOTE-GUARD] validator=%s height=%d round=%d action=skip_conflicting_round_vote existing=%s incoming=%s",
		ShortID(n.ID),
		epoch,
		round,
		existing,
		proposalKey,
	)
	return false
}

func (n *Node) releaseStaleLocalExecutionVoteMarkerLocked(epoch uint64, existingRound uint32, incomingRound uint32, existingKey string, incomingKey string) bool {
	if n == nil || epoch == 0 {
		return false
	}
	existingKey = strings.TrimSpace(existingKey)
	incomingKey = strings.TrimSpace(incomingKey)
	if existingKey == "" || incomingKey == "" || existingKey == incomingKey {
		return false
	}
	if incomingRound <= existingRound || incomingRound-existingRound < localExecVoteStaleRoundReleaseGap {
		return false
	}
	if n.localExecutionVoteMarkerHasEvidenceLocked(epoch, existingKey) {
		return false
	}
	log.Printf("[EXEC-VOTE-GUARD] validator=%s height=%d round=%d action=release_stale_cross_round_marker existing_round=%d existing=%s incoming=%s",
		ShortID(n.ID),
		epoch,
		incomingRound,
		existingRound,
		existingKey,
		incomingKey,
	)
	return true
}

func (n *Node) localExecutionVoteMarkerHasEvidenceLocked(epoch uint64, proposalKey string) bool {
	if n == nil || epoch == 0 {
		return false
	}
	proposalKey = strings.TrimSpace(proposalKey)
	if proposalKey == "" {
		return false
	}
	evidenceCount := n.acceptedProposalVoteCountLocked(epoch, proposalKey)
	_, _, blockHash, txMerkle, stateRoot, ok := proposalVoteKeyParts(proposalKey)
	if ok {
		if global := getExecCountGlobal(epoch, proposalKey, stateRoot, txMerkle); global > evidenceCount {
			evidenceCount = global
		}
		if blockHash != "" && n.Consensus != nil {
			n.Consensus.mu.Lock()
			votes := len(n.Consensus.ExecVotes[strings.TrimSpace(blockHash)])
			n.Consensus.mu.Unlock()
			if votes > evidenceCount {
				evidenceCount = votes
			}
		}
	}
	if evidenceCount <= 0 {
		return false
	}
	required := n.executionQuorumRequiredForEpoch(epoch)
	if required <= 0 {
		return true
	}
	return evidenceCount >= required
}

func execResultKey(epoch uint64, execHash string, txMerkle string) string {
	return fmt.Sprintf("%d:%s:%s", epoch, execHash, txMerkle)
}

func execBroadcastKey(execHash string, txMerkle string) string {
	return fmt.Sprintf("%s:%s", execHash, txMerkle)
}

func strictExecSupermajority(total int) int {
	if total <= 0 {
		return 0
	}
	return (2*total)/3 + 1
}

func (n *Node) allowExecutionVoteIngress(signer string, epoch uint64, proposalKey string, execHash string, txMerkle string) (bool, string) {
	if n == nil || epoch == 0 || proposalKey == "" || signer == "" || execHash == "" {
		return false, "invalid_vote"
	}
	proposalKey = execPoolScopeKey(epoch, proposalKey)
	now := time.Now()
	key := fmt.Sprintf("%d:%s:%s:%s:%s", epoch, proposalKey, signer, execHash, txMerkle)
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()

	if n.execVoteSeen == nil {
		n.execVoteSeen = make(map[string]time.Time)
	}
	if n.execVoteLimiter == nil {
		n.execVoteLimiter = make(map[string]*rate.Limiter)
	}

	limiter := n.execVoteLimiter[signer]
	if limiter == nil {
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

	if last, ok := n.execVoteSeen[key]; ok && now.Sub(last) <= execVoteReplayTTL {
		return false, "replay_cache"
	}
	n.execVoteSeen[key] = now
	if len(n.execVoteSeen) > ExecVoteReplayMaxKeys {
		cutoff := now.Add(-execVoteReplayTTL)
		for k, seenAt := range n.execVoteSeen {
			if seenAt.Before(cutoff) {
				delete(n.execVoteSeen, k)
			}
		}
	}
	return true, ""
}

func (n *Node) logExecutionVoteDrop(reason string, res ExecutionResultMsg, proposalSnap execProposalSnapshot) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
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

func (n *Node) logExecutionVoteAccept(reason string, res ExecutionResultMsg, proposalSnap execProposalSnapshot, votes int, required int) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "recorded"
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

// storeVoteButIgnoreForCommit keeps post-commit execution votes available for
// diagnostics/replay visibility without letting them trigger a second commit.
func (n *Node) storeVoteButIgnoreForCommit(epoch uint64, res ExecutionResultMsg, proposalSnap execProposalSnapshot, votes int) {
	if n == nil || epoch == 0 {
		return
	}
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	required := n.executionQuorumRequired(epoch)
	if required == 0 {
		required = strictExecSupermajority(len(validators))
	}
	n.logExecutionVoteAccept("recorded_committed", res, proposalSnap, votes, required)
	if n.Blockchain != nil && n.Blockchain.Height() >= epoch {
		return
	}
	_ = n.advanceConsensusToCommittedTip("committed_execution_vote")
}

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

func (n *Node) recordExecutionMismatchStrike(signer string, epoch uint64) int {
	signer = normalizeValidatorID(signer)
	if n == nil || signer == "" || epoch == 0 {
		return 0
	}
	now := time.Now()
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if n.execMismatch == nil {
		n.execMismatch = make(map[string]ExecMismatchTracker)
	}
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
		cutoff := now.Add(-execMismatchStrikeWindow)
		for id, st := range n.execMismatch {
			if st.LastAt.Before(cutoff) {
				delete(n.execMismatch, id)
			}
		}
	}
	return tracker.Count
}

func (n *Node) executionMismatchUniqueSignersAtEpoch(epoch uint64) int {
	if n == nil || epoch == 0 {
		return 0
	}
	now := time.Now()
	cutoff := now.Add(-execMismatchStrikeWindow)
	n.execVoteGuardMu.Lock()
	defer n.execVoteGuardMu.Unlock()
	if len(n.execMismatch) == 0 {
		return 0
	}
	unique := 0
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

func (n *Node) disconnectValidatorPeers(validatorID string, reason string) int {
	validatorID = normalizeValidatorID(validatorID)
	if n == nil || validatorID == "" {
		return 0
	}
	var peers []string
	n.peerStateMu.Lock()
	for peerID, vid := range n.peerToValidator {
		if normalizeValidatorID(vid) == validatorID {
			peers = append(peers, peerID)
		}
	}
	n.peerStateMu.Unlock()
	for _, peerID := range peers {
		n.disconnectPeerID(peerID, reason)
	}
	return len(peers)
}

func (n *Node) peerClaimsValidator(peerID, validatorID string) bool {
	peerID = strings.TrimSpace(peerID)
	validatorID = normalizeValidatorID(validatorID)
	if n == nil || peerID == "" || validatorID == "" {
		return false
	}
	n.peerStateMu.Lock()
	claimed := normalizeValidatorID(n.peerToValidator[peerID])
	n.peerStateMu.Unlock()
	return claimed == validatorID
}

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
	strikes := n.recordInvalidProposerStrike(got, height, expected, got)
	if DebugConsensus {
		fmt.Printf("Invalid proposer strike from %s @ height %d | strike=%d expected=%s got=%s\n",
			ShortID(got), height, strikes, ShortID(expected), ShortID(got))
	}
	quarantineAt := ConsensusInvalidProposerQuarantineAfter
	if quarantineAt <= 0 {
		quarantineAt = invalidProposerQuarantineAt
	}
	if strikes == quarantineAt {
		quarantined := n.disconnectValidatorPeers(got, "invalid_proposer_repeat")
		if DebugConsensus && quarantined > 0 {
			fmt.Printf("Invalid proposer quarantine applied to %s | peers=%d\n", ShortID(got), quarantined)
		}
	}
	if sourcePeer != "" && n.peerClaimsValidator(sourcePeer, got) {
		peerStrikes := n.recordInvalidProposerPeerStrike(sourcePeer, height, expected, got)
		if DebugConsensus {
			fmt.Printf("Invalid proposer peer strike %s | strike=%d validator=%s height=%d\n",
				sourcePeer, peerStrikes, ShortID(got), height)
		}
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

func (n *Node) handleExecutionMismatchPolicy(signer string, epoch uint64, expected string, got string) {
	if n.isCorePendingAtHeight(signer, epoch) {
		if DebugConsensus {
			fmt.Printf("[PENALTY-GATE] height=%d enforce=false reason=core_pending policy=exec_mismatch signer=%s\n",
				epoch, ShortID(signer))
		}
		return
	}
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
	strikes := n.recordExecutionMismatchStrike(signer, epoch)
	if strikes <= 0 {
		return
	}
	if DebugConsensus {
		fmt.Printf("Execution mismatch strike from %s @ epoch %d | strike=%d expected=%s got=%s\n",
			ShortID(signer), epoch, strikes, ShortHash(expected), ShortHash(got))
	}
	uniqueSigners := n.executionMismatchUniqueSignersAtEpoch(epoch)
	if uniqueSigners >= 2 {
		log.Printf("[EXEC-MISMATCH] severity=high epoch=%d unique_signers=%d last_signer=%s expected=%s got=%s",
			epoch, uniqueSigners, ShortID(signer), ShortHash(expected), ShortHash(got))
		n.maybeSyncToBestObservedHeight("exec_mismatch_multi_signer")
	}
	quarantineAt := ConsensusExecMismatchQuarantineAfter
	if quarantineAt <= 0 {
		quarantineAt = execMismatchQuarantineAt
	}
	if strikes == quarantineAt {
		quarantined := n.disconnectValidatorPeers(signer, "exec_mismatch_repeat")
		if DebugConsensus && quarantined > 0 {
			fmt.Printf("Execution mismatch quarantine applied to %s | peers=%d\n", ShortID(signer), quarantined)
		}
		n.maybeSyncToBestObservedHeight("exec_mismatch_repeat")
		go n.forceSnapshotResyncNow(epoch, "exec_mismatch_repeat")
	}
	slashAt := ConsensusExecMismatchSlashAfter
	if slashAt <= 0 {
		slashAt = execMismatchSlashAt
	}
	if strikes == slashAt {
		n.RecordMisbehavior(signer, "exec_mismatch_repeat", int(epoch), expected)
		n.SlashValidator(signer)
	}
}

func (n *Node) handleExecutionEquivocationPolicy(signer string, epoch uint64, execHash string) {
	n.resetExecutionMismatchStrike(signer)
	n.RecordMisbehavior(signer, "exec_equivocation_signed", int(epoch), execHash)
	n.disconnectValidatorPeers(signer, "exec_equivocation")
	n.SlashValidator(signer)
	n.maybeSyncToBestObservedHeight("exec_equivocation")
	go n.forceSnapshotResyncNow(epoch, "exec_equivocation")
}

func (n *Node) executionVoteSignerLikelyStale(signer string, epoch uint64) bool {
	if n == nil || epoch == 0 {
		return false
	}
	signer = normalizeValidatorID(signer)
	if signer == "" {
		return false
	}
	n.validatorMu.RLock()
	st := n.validatorStatus[signer]
	n.validatorMu.RUnlock()
	if st == nil {
		return false
	}
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

func execResultPubKeyCandidates(signer string) []ed25519.PublicKey {
	normalized := normalizeValidatorID(signer)
	candidates := make([]ed25519.PublicKey, 0, 4)
	addCandidate := func(pk ed25519.PublicKey) {
		if len(pk) != ed25519.PublicKeySize {
			return
		}
		for _, existing := range candidates {
			if bytes.Equal(existing, pk) {
				return
			}
		}
		copied := make([]byte, len(pk))
		copy(copied, pk)
		candidates = append(candidates, ed25519.PublicKey(copied))
	}
	validatorPubKeysMu.RLock()
	addCandidate(ValidatorPubKeys[normalized])
	addCandidate(ValidatorPubKeys[signer])
	addCandidate(GenesisValidatorPubKeys[normalized])
	addCandidate(GenesisValidatorPubKeys[signer])
	validatorPubKeysMu.RUnlock()
	return candidates
}

func recordExecResultGlobal(epoch uint64, proposalKey string, execHash string, txMerkle string, res ExecutionResult) (int, bool, bool) {
	if epoch == 0 || execHash == "" || res.Signer == "" {
		return 0, false, false
	}
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)
	signer := normalizeValidatorID(res.Signer)
	if signer == "" {
		return 0, false, false
	}
	res.Signer = signer

	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	if _, ok := ExecPool.frozen[epoch]; !ok {
		ExecPool.frozen[epoch] = make(map[string]string)
	}
	if frozenHash, ok := ExecPool.frozen[epoch][poolScopeKey]; ok && frozenHash != "" && frozenHash != execHash {
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	if _, ok := ExecPool.pool[epoch]; !ok {
		ExecPool.pool[epoch] = make(map[string]map[string]ExecutionResult)
	}
	if _, ok := ExecPool.txMerkle[epoch]; !ok {
		ExecPool.txMerkle[epoch] = make(map[string]string)
	}
	if _, ok := ExecPool.signers[epoch]; !ok {
		ExecPool.signers[epoch] = make(map[string]map[string]bool)
	}
	if _, ok := ExecPool.choice[epoch]; !ok {
		ExecPool.choice[epoch] = make(map[string]map[string]string)
	}
	if ExecPool.epochChoice == nil {
		ExecPool.epochChoice = make(map[uint64]map[string]string)
	}
	if _, ok := ExecPool.epochChoice[epoch]; !ok {
		ExecPool.epochChoice[epoch] = make(map[string]string)
	}
	if _, ok := ExecPool.signers[epoch][poolScopeKey]; !ok {
		ExecPool.signers[epoch][poolScopeKey] = make(map[string]bool)
	}
	if _, ok := ExecPool.choice[epoch][poolScopeKey]; !ok {
		ExecPool.choice[epoch][poolScopeKey] = make(map[string]string)
	}

	if existing, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && existing != "" && existing != txMerkle {
		return len(ExecPool.pool[epoch][scopedExecKey]), false, false
	}
	if _, ok := ExecPool.txMerkle[epoch][scopedExecKey]; !ok {
		ExecPool.txMerkle[epoch][scopedExecKey] = txMerkle
	}

	choice := execBroadcastKey(execHash, txMerkle)
	epochChoice := poolScopeKey + "|" + choice
	epochChoiceKey := execEpochChoiceSignerKey(res.Signer, proposalKey)
	if epochChoiceKey != "" {
		if prev, exists := ExecPool.epochChoice[epoch][epochChoiceKey]; exists && prev != epochChoice {
			if !releaseStaleExecPoolSignerChoiceLocked(epoch, signer, proposalKey, poolScopeKey, choice, res.Round) {
				return 0, false, true
			}
			if _, ok := ExecPool.pool[epoch]; !ok {
				ExecPool.pool[epoch] = make(map[string]map[string]ExecutionResult)
			}
			if _, ok := ExecPool.signers[epoch]; !ok {
				ExecPool.signers[epoch] = make(map[string]map[string]bool)
			}
			if _, ok := ExecPool.choice[epoch]; !ok {
				ExecPool.choice[epoch] = make(map[string]map[string]string)
			}
			if _, ok := ExecPool.epochChoice[epoch]; !ok {
				ExecPool.epochChoice[epoch] = make(map[string]string)
			}
			if _, ok := ExecPool.signers[epoch][poolScopeKey]; !ok {
				ExecPool.signers[epoch][poolScopeKey] = make(map[string]bool)
			}
			if _, ok := ExecPool.choice[epoch][poolScopeKey]; !ok {
				ExecPool.choice[epoch][poolScopeKey] = make(map[string]string)
			}
		}
	}
	if prev, exists := ExecPool.choice[epoch][poolScopeKey][res.Signer]; exists {
		if prev != choice {
			// Signed equivocation proof: same signer, same epoch+proposal, different exec vote.
			return 0, false, true
		}
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	if ExecPool.signers[epoch][poolScopeKey][res.Signer] {
		if byHash, ok := ExecPool.pool[epoch][scopedExecKey]; ok {
			return len(byHash), false, false
		}
		return 0, false, false
	}

	if epochChoiceKey != "" {
		ExecPool.epochChoice[epoch][epochChoiceKey] = epochChoice
	}
	ExecPool.choice[epoch][poolScopeKey][res.Signer] = choice
	ExecPool.signers[epoch][poolScopeKey][res.Signer] = true

	if _, ok := ExecPool.pool[epoch][scopedExecKey]; !ok {
		ExecPool.pool[epoch][scopedExecKey] = make(map[string]ExecutionResult)
	}
	if _, ok := ExecPool.pool[epoch][scopedExecKey][res.Signer]; !ok {
		ExecPool.pool[epoch][scopedExecKey][res.Signer] = res
	}

	return len(ExecPool.pool[epoch][scopedExecKey]), true, false
}

func releaseStaleExecPoolSignerChoiceLocked(epoch uint64, signer string, incomingProposalKey string, incomingScope string, incomingChoice string, incomingRound uint32) bool {
	if epoch == 0 || signer == "" || incomingScope == "" || incomingChoice == "" {
		return false
	}
	if _, round, _, _, _, ok := proposalVoteKeyParts(incomingProposalKey); ok {
		incomingRound = round
	}
	var previousScope string
	var previousChoice string
	var previousRound uint32
	for scope, bySigner := range ExecPool.choice[epoch] {
		choice := strings.TrimSpace(bySigner[signer])
		if choice == "" {
			continue
		}
		previousScope = scope
		previousChoice = choice
		if byHash, ok := ExecPool.pool[epoch]; ok {
			prefix := scope + "|"
			for key, results := range byHash {
				if !strings.HasPrefix(key, prefix) {
					continue
				}
				if existing, ok := results[signer]; ok {
					previousRound = existing.Round
					break
				}
			}
		}
		break
	}
	if previousScope == "" || previousChoice == "" || previousScope == incomingScope {
		return false
	}
	if incomingRound <= previousRound || incomingRound-previousRound < localExecVoteStaleRoundReleaseGap {
		return false
	}
	if frozen := strings.TrimSpace(ExecPool.frozen[epoch][previousScope]); frozen != "" {
		return false
	}
	if byHash, ok := ExecPool.pool[epoch]; ok {
		prefix := previousScope + "|"
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
	if byScope, ok := ExecPool.signers[epoch]; ok {
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
	if byScope, ok := ExecPool.choice[epoch]; ok {
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
	if bySigner, ok := ExecPool.epochChoice[epoch]; ok {
		delete(bySigner, signer)
		if len(bySigner) == 0 {
			delete(ExecPool.epochChoice, epoch)
		}
	}
	return true
}

func freezeExecPool(epoch uint64, proposalKey string, execHash string) {
	if epoch == 0 || execHash == "" {
		return
	}
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()
	if ExecPool.frozen == nil {
		ExecPool.frozen = make(map[uint64]map[string]string)
	}
	if _, ok := ExecPool.frozen[epoch]; !ok {
		ExecPool.frozen[epoch] = make(map[string]string)
	}
	if _, ok := ExecPool.frozen[epoch][poolScopeKey]; !ok {
		ExecPool.frozen[epoch][poolScopeKey] = execHash
	}
}

func getExecResultsGlobal(epoch uint64, proposalKey string, execHash string, txMerkle string) ([]ExecutionResult, []string, int, bool) {
	if epoch == 0 || execHash == "" {
		return nil, nil, 0, false
	}
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)

	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	byHash, ok := ExecPool.pool[epoch]
	if !ok {
		return nil, nil, 0, false
	}
	resultsMap, ok := byHash[scopedExecKey]
	if !ok {
		return nil, nil, 0, false
	}
	if txMerkle != "" {
		if expected, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && expected != "" && expected != txMerkle {
			return nil, nil, 0, false
		}
	}

	results := make([]ExecutionResult, 0, len(resultsMap))
	signers := make([]string, 0, len(resultsMap))
	for _, r := range resultsMap {
		results = append(results, r)
		if r.Signer != "" {
			signers = append(signers, r.Signer)
		}
	}

	return results, signers, len(resultsMap), true
}

func getExecCountGlobal(epoch uint64, proposalKey string, execHash string, txMerkle string) int {
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	if epoch == 0 || execHash == "" {
		return 0
	}
	scopedExecKey := execPoolResultKey(epoch, proposalKey, execHash)

	byHash, ok := ExecPool.pool[epoch]
	if !ok {
		return 0
	}
	resultsMap, ok := byHash[scopedExecKey]
	if !ok {
		return 0
	}
	if txMerkle != "" {
		if expected, ok := ExecPool.txMerkle[epoch][scopedExecKey]; ok && expected != "" && expected != txMerkle {
			return 0
		}
	}
	return len(resultsMap)
}

func clearExecPoolProposal(epoch uint64, proposalKey string) {
	if epoch == 0 || proposalKey == "" {
		return
	}
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	if byHash, ok := ExecPool.pool[epoch]; ok {
		prefix := poolScopeKey + "|"
		for key := range byHash {
			if strings.HasPrefix(key, prefix) {
				delete(byHash, key)
			}
		}
		if len(byHash) == 0 {
			delete(ExecPool.pool, epoch)
		}
	}
	if byMerkle, ok := ExecPool.txMerkle[epoch]; ok {
		prefix := poolScopeKey + "|"
		for key := range byMerkle {
			if strings.HasPrefix(key, prefix) {
				delete(byMerkle, key)
			}
		}
		if len(byMerkle) == 0 {
			delete(ExecPool.txMerkle, epoch)
		}
	}
	if byProposal, ok := ExecPool.frozen[epoch]; ok {
		delete(byProposal, poolScopeKey)
		if len(byProposal) == 0 {
			delete(ExecPool.frozen, epoch)
		}
	}
	if byProposal, ok := ExecPool.signers[epoch]; ok {
		delete(byProposal, poolScopeKey)
		if len(byProposal) == 0 {
			delete(ExecPool.signers, epoch)
		}
	}
	if byProposal, ok := ExecPool.choice[epoch]; ok {
		delete(byProposal, poolScopeKey)
		if len(byProposal) == 0 {
			delete(ExecPool.choice, epoch)
		}
	}
	if bySigner, ok := ExecPool.epochChoice[epoch]; ok {
		prefix := poolScopeKey + "|"
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

func clearExecPoolUpTo(height uint64) {
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	for h := range ExecPool.pool {
		if h <= height {
			delete(ExecPool.pool, h)
		}
	}
	for h := range ExecPool.txMerkle {
		if h <= height {
			delete(ExecPool.txMerkle, h)
		}
	}
	for h := range ExecPool.frozen {
		if h <= height {
			delete(ExecPool.frozen, h)
		}
	}
	for h := range ExecPool.signers {
		if h <= height {
			delete(ExecPool.signers, h)
		}
	}
	for h := range ExecPool.choice {
		if h <= height {
			delete(ExecPool.choice, h)
		}
	}
	for h := range ExecPool.epochChoice {
		if h <= height {
			delete(ExecPool.epochChoice, h)
		}
	}
}

func buildExecPoolSnapshot(epoch uint64, proposalKey string) *ExecPoolSnapshot {
	if epoch == 0 {
		return nil
	}
	poolScopeKey := execPoolScopeKey(epoch, proposalKey)
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()

	byHash, ok := ExecPool.pool[epoch]
	if !ok || len(byHash) == 0 {
		return nil
	}

	prefix := poolScopeKey + "|"
	hashes := make(map[string][]string)
	for scopedHash, resMap := range byHash {
		if !strings.HasPrefix(scopedHash, prefix) {
			continue
		}
		hash := strings.TrimPrefix(scopedHash, prefix)
		signers := make([]string, 0, len(resMap))
		for signer := range resMap {
			signers = append(signers, signer)
		}
		hashes[hash] = canonicalValidatorIDs(signers)
	}
	if len(hashes) == 0 {
		return nil
	}

	txMerkle := make(map[string]string)
	if tm, ok := ExecPool.txMerkle[epoch]; ok {
		for scopedHash, merkle := range tm {
			if !strings.HasPrefix(scopedHash, prefix) {
				continue
			}
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

func (n *Node) mergeExecPoolSnapshot(snapshot ExecPoolSnapshot) {
	if snapshot.Epoch == 0 {
		return
	}
	if len(snapshot.Hashes) > 0 {
		// ExecPoolSnapshot is an unsigned hint from a peer. Do not let delayed
		// proof injection manufacture live quorum; validators must replay their
		// signed execution votes through processExecutionResultMsg.
		if DebugConsensus || DebugSync {
			fmt.Printf("[EXEC-POOL-SNAPSHOT-REJECT] epoch=%d reason=unsigned_quorum_hint hashes=%d\n",
				snapshot.Epoch, len(snapshot.Hashes))
		}
		return
	}
	proposalKey := strings.TrimSpace(snapshot.ProposalKey)
	if proposalKey == "" {
		proposalKey = legacyProposalVoteKey(snapshot.Epoch)
	}
	proposalBlockHash := ""
	n.commitMu.Lock()
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	if snapshot.Epoch <= committedHeight {
		return
	}
	// Accept only current epoch to avoid stale pollution.
	if snapshot.Epoch != n.currentEpoch() {
		return
	}
	if snap, ok := n.proposalSnapshotForEpoch(snapshot.Epoch); ok && proposalKey == snap.ProposalKey {
		proposalBlockHash = snap.BlockHash
		goto merge_exec_snapshot
	}
	for _, block := range n.candidateProposalBlocksForEpoch(snapshot.Epoch) {
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
	for hash, signers := range snapshot.Hashes {
		txMerkle := ""
		if snapshot.TxMerkle != nil {
			txMerkle = snapshot.TxMerkle[hash]
		}
		for _, signer := range signers {
			if !n.isValidatorInSetForHeight(signer, snapshot.Epoch) {
				continue
			}
			_, _, _ = recordExecResultGlobal(snapshot.Epoch, proposalKey, hash, txMerkle, ExecutionResult{
				Height:     snapshot.Epoch,
				Signer:     signer,
				ResultHash: hash,
				TxMerkle:   txMerkle,
			})
			n.mirrorConsensusExecVote(snapshot.Epoch, proposalBlockHash, ExecutionResult{
				Height:     snapshot.Epoch,
				BlockHash:  proposalBlockHash,
				Signer:     signer,
				ResultHash: hash,
				TxMerkle:   txMerkle,
			})
		}
	}
}

func (n *Node) currentEpoch() uint64 {
	h := n.Blockchain.Height()
	n.commitMu.Lock()
	if n.committedHeight > h {
		h = n.committedHeight
	}
	n.commitMu.Unlock()
	return h + 1
}

func (n *Node) queueFutureLeaderBlock(block Block, sourcePeer string) bool {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return false
	}
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
			a := n.queuedFutureLeaderBlocks[block.ID][i]
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

func (n *Node) replayQueuedLeaderBlocksForCurrentEpoch() {
	if n == nil {
		return
	}
	epoch := n.currentEpoch()
	if epoch == 0 {
		return
	}

	n.leaderMu.Lock()
	if len(n.queuedFutureLeaderBlocks) == 0 {
		n.leaderMu.Unlock()
		return
	}
	blocks := append([]Block(nil), n.queuedFutureLeaderBlocks[epoch]...)
	delete(n.queuedFutureLeaderBlocks, epoch)
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
	for _, block := range blocks {
		n.handleLeaderBlock(block, "queued_future")
	}
}

func recordProposedBlock(height uint64, round uint32, proposer string, blockHash string) (bool, bool, string) {
	proposer = normalizeValidatorID(proposer)
	if height == 0 || proposer == "" || blockHash == "" {
		return false, false, ""
	}

	proposedBlocksMu.Lock()
	defer proposedBlocksMu.Unlock()

	if ProposedBlocks == nil {
		ProposedBlocks = make(map[uint64]map[uint32]map[string]string)
	}
	if _, ok := ProposedBlocks[height]; !ok {
		ProposedBlocks[height] = make(map[uint32]map[string]string)
	}
	if _, ok := ProposedBlocks[height][round]; !ok {
		ProposedBlocks[height][round] = make(map[string]string)
	}

	if prev, ok := ProposedBlocks[height][round][proposer]; ok {
		if prev == blockHash {
			return false, false, prev
		}
		return false, true, prev
	}
	ProposedBlocks[height][round][proposer] = blockHash

	const keepWindow = uint64(512)
	if height > keepWindow {
		cutoff := height - keepWindow
		for h := range ProposedBlocks {
			if h < cutoff {
				delete(ProposedBlocks, h)
			}
		}
	}

	return true, false, ""
}

func penalizeSignedProposal(n *Node, block Block, reason string) {
	if n == nil || block.ID == 0 || block.Proposer == "" {
		return
	}
	n.RecordMisbehavior(block.Proposer, reason, int(block.ID), block.BlockHash)
	n.SlashValidator(block.Proposer)
}

func (n *Node) verifyLeaderBlock(block Block, sourcePeer string) bool {
	if block.ID == 0 {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] reason=zero_height peer=%s proposer=%s block=%s\n",
				sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		return false
	}
	epoch := n.currentEpoch()
	if block.ID != epoch {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d reason=wrong_epoch local_epoch=%d peer=%s proposer=%s block=%s\n",
				block.ID, epoch, sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		return false
	}

	validSig := len(block.Signature) > 0 && VerifyBlockSignature(block)
	if !validSig && IsTestnet && len(block.Signature) > 0 {
		// Testnet startup race: pubkey map may still carry genesis keys until
		// peer-hello/heartbeat refresh lands. Briefly retry before rejecting.
		deadline := time.Now().Add(800 * time.Millisecond)
		for !validSig && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			validSig = VerifyBlockSignature(block)
		}
	}
	if !validSig && IsTestnet && len(block.Signature) > 0 {
		if inSet, _, ok := n.authoritativeHeartbeatMembershipAtHeight(block.Proposer, block.ID); ok && inSet {
			proposerID := normalizeValidatorID(block.Proposer)
			var cand *CandidateStatus
			n.candidateMu.RLock()
			if n.candidates != nil {
				cand = n.candidates[proposerID]
			}
			n.candidateMu.RUnlock()
			maxAge := validatorLivenessHeartbeatTTL() + validatorLivenessGrace()
			if cand == nil || len(cand.PubKey) != ed25519.PublicKeySize {
				if DebugConsensus {
					key := fmt.Sprintf("proposer_sig_fallback:%d:%s:missing_candidate", block.ID, proposerID)
					if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
						fmt.Printf("[PROPOSER-SIG-FALLBACK] height=%d proposer=%s result=failed reason=missing_candidate\n",
							block.ID, ShortID(proposerID))
					}
				}
			} else if cand.LastHeartbeatAt.IsZero() || time.Since(cand.LastHeartbeatAt) > maxAge {
				if DebugConsensus {
					key := fmt.Sprintf("proposer_sig_fallback:%d:%s:stale_candidate", block.ID, proposerID)
					if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
						fmt.Printf("[PROPOSER-SIG-FALLBACK] height=%d proposer=%s result=failed reason=stale_candidate\n",
							block.ID, ShortID(proposerID))
					}
				}
			} else if verifyBlockSignatureWithCandidates(block, []ed25519.PublicKey{cand.PubKey}) {
				validatorPubKeysMu.Lock()
				ValidatorPubKeys[proposerID] = cand.PubKey
				if proposerID != block.Proposer {
					ValidatorPubKeys[block.Proposer] = cand.PubKey
				}
				validatorPubKeysMu.Unlock()
				validSig = true
				if DebugConsensus {
					key := fmt.Sprintf("proposer_sig_fallback:%d:%s:accepted", block.ID, proposerID)
					if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
						fmt.Printf("[PROPOSER-SIG-FALLBACK] height=%d proposer=%s result=accepted source=candidate_heartbeat\n",
							block.ID, ShortID(proposerID))
					}
				}
			} else if DebugConsensus {
				key := fmt.Sprintf("proposer_sig_fallback:%d:%s:signature_mismatch", block.ID, proposerID)
				if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
					fmt.Printf("[PROPOSER-SIG-FALLBACK] height=%d proposer=%s result=failed reason=signature_mismatch\n",
						block.ID, ShortID(proposerID))
				}
			}
		}
	}
	if !validSig {
		if DebugConsensus {
			fmt.Printf("Invalid proposer signature at height %d | proposer=%s\n",
				block.ID, ShortID(block.Proposer))
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_leader_block_signature")
		return false
	}

	if existing, ok := n.getLeaderBlock(block.ID); ok {
		if block.Round == existing.Round && block.BlockHash == existing.BlockHash {
			if DebugConsensus {
				fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=duplicate_proposal peer=%s proposer=%s block=%s\n",
					block.ID, block.Round, sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
			}
			return false
		}
	}

	requiresValidatorSetHash := validatorSetCommitmentV2EnabledAt(block.ID)
	if requiresValidatorSetHash && strings.TrimSpace(block.ValidatorSetHash) == "" {
		if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-REJECT] height=%d reason=missing_validator_set_hash\n", block.ID)
		}
		n.recordPeerSecurityFault(sourcePeer, "missing_validator_set_hash")
		return false
	}
	if block.ValidatorSetHash != "" {
		expectedHash, expectedSource := n.expectedValidatorSetHashWithSource(block.ID)
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
					localHeightNow, localFinalized := n.localHeightSnapshot()
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
			count, shouldLog := n.recordPeerDriftTuple(block.ID, expectedHash, block.ValidatorSetHash, peerDriftTupleLogCooldown)
			if DebugConsensus && shouldLog {
				fmt.Printf("[SET-COMMITMENT-REJECT] height=%d source=%s mode=%s expected=%s got=%s count=%d\n",
					block.ID, expectedSource, hashMode, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash), count)
			}
			if DebugConsensus && shouldLog {
				fmt.Printf("Validator set hash mismatch at height %d | expected=%s got=%s count=%d\n",
					block.ID, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash), count)
			}
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
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-REJECT] height=%d field=next_validator_set_hash err=%v\n", block.ID, err)
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_next_validator_set_commitment")
		return false
	}
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("Validator set next-root commitment mismatch at height %d | err=%v\n", block.ID, err)
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_next_validator_set_root")
		return false
	}

	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	if err := verifyBlockQuorumMetadata(block, len(validators)); err != nil {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=%s peer=%s proposer=%s block=%s\n",
				block.ID, block.Round, err.Error(), sourcePeer, ShortID(block.Proposer), ShortHash(block.BlockHash))
		}
		n.recordPeerSecurityFault(sourcePeer, "invalid_leader_quorum_metadata")
		return false
	}
	expectedLeader := n.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	gotProposer := normalizeValidatorID(block.Proposer)
	wantProposer := normalizeValidatorID(expectedLeader)
	if wantProposer == "" || gotProposer != wantProposer {
		if !n.shouldCountInvalidProposerEvidence(block.ID, block.Round, wantProposer, gotProposer, block.BlockHash) {
			return false
		}
		count, shouldPause := n.invalidProposerEvent(block.ID, wantProposer, gotProposer)
		if DebugConsensus && (count <= 3 || count%25 == 0) {
			expectedIdx := -1
			gotIdx := -1
			for i, id := range validators {
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

	_, equivocated, prevHash := recordProposedBlock(block.ID, block.Round, block.Proposer, block.BlockHash)
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
	if err := VerifyMempoolRoot(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[LEADER-BLOCK-REJECT] height=%d round=%d reason=mempool_root_mismatch peer=%s proposer=%s err=%v block=%s\n",
				block.ID, block.Round, sourcePeer, ShortID(block.Proposer), err, ShortHash(block.BlockHash))
		}
		n.recordPeerSecurityFault(sourcePeer, "leader_mempool_root_mismatch")
		return false
	}
	if maxRound := ProposerRoundMax; maxRound > 0 && block.Round > maxRound {
		if DebugConsensus {
			fmt.Printf("Rejecting over-cap proposal at height %d | peer=%s round=%d max_round=%d\n",
				block.ID, sourcePeer, block.Round, maxRound)
		}
		return false
	}
	localRound := n.localProposerRoundForHeight(block.ID)
	maxSkew := ProposerRoundMaxSkew
	if maxSkew == 0 {
		maxSkew = 1
	}
	if block.Round > localRound+maxSkew {
		n.setProposedRound(block.ID, block.Round)
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
	// StateRoot mismatch should be resolved by execution-result quorum.
	// Proposal-stage rejection here can create false-positive slashing loops.
	return true
}

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
	if lockedBlock, _, locked, _ := n.acceptedProposalVoteLockForRound(block.ID, block.Round); locked && proposalConflictsWithAcceptedLock(lockedBlock, block) {
		n.maybeBroadcastExecutionVoteForBlock(lockedBlock, "accepted_vote_lock")
		return
	}
	n.maybeBroadcastExecutionVoteForBlock(block, "leader_block")
}

func (n *Node) setLogicalTick(epoch uint64, tick uint64) {
	n.logicalMu.Lock()
	n.logicalClock = LogicalClock{Epoch: epoch, Tick: tick}
	n.logicalMu.Unlock()
}

func executionResultSortLess(a ExecutionResult, b ExecutionResult) bool {
	aSigner := normalizeValidatorID(a.Signer)
	bSigner := normalizeValidatorID(b.Signer)
	if aSigner != bSigner {
		return aSigner < bSigner
	}
	aSigned := strings.TrimSpace(a.Signature) != ""
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
	bySigner := make(map[string]ExecutionResult, len(results))
	for _, res := range results {
		signer := normalizeValidatorID(res.Signer)
		if signer == "" {
			continue
		}
		res.Signer = signer
		if existing, ok := bySigner[signer]; !ok || executionResultSortLess(res, existing) {
			bySigner[signer] = res
		}
	}
	if len(bySigner) == 0 {
		return nil
	}
	out := make([]ExecutionResult, 0, len(bySigner))
	for _, res := range bySigner {
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool {
		return executionResultSortLess(out[i], out[j])
	})
	return out
}

func (n *Node) executionCommitPrecondition(epoch uint64, leaderBlock Block) (int, int, bool, string) {
	if n == nil || epoch == 0 || leaderBlock.ID != epoch {
		return 0, 0, false, "invalid_commit_target"
	}
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	total := len(validators)
	required := n.executionQuorumRequired(epoch)
	if required == 0 {
		required = strictExecSupermajority(total)
	}
	if total == 0 || required == 0 {
		return 0, required, false, "commit_quorum_unresolved"
	}
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

func (n *Node) recordDeterministicCommitVotes(height uint64, blockHash string, signers []string) (int, int) {
	if n == nil || height == 0 || strings.TrimSpace(blockHash) == "" {
		return 0, 0
	}
	canonical := canonicalValidatorIDs(signers)
	count := 0
	required := 0
	for _, signer := range canonical {
		count, required = n.recordCommitVote(height, blockHash, signer)
	}
	if count > 0 {
		n.persistConsensusSafetyStateAsync("commit_votes")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "commit_votes_recorded",
			Reason:    "deterministic_commit",
			Height:    height,
			BlockHash: blockHash,
			Required:  required,
			Fields: map[string]interface{}{
				"count":   count,
				"signers": len(canonical),
			},
		})
	}
	return count, required
}

func leaderFromExecHash(execHash string, epoch uint64, validators []string) string {
	ordered := deterministicStakeHashOrderedValidatorIDs(validators, nil)
	if len(ordered) == 0 {
		return ""
	}

	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", execHash, epoch)))
	idx := binary.BigEndian.Uint64(sum[:8]) % uint64(len(ordered))
	return ordered[idx]
}

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
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] next-set commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] next-set-root commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	if err := n.validateBlockValidatorSetRootCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] set-root commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		if DebugConsensus {
			fmt.Printf("[FINALITY-GATE] registry commitment invalid height=%d err=%v\n", block.ID, err)
		}
		return false
	}
	return true
}

func (n *Node) executionProposalBlockForResult(epoch uint64, execHash string, txMerkle string) (Block, bool) {
	if n == nil || epoch == 0 || execHash == "" {
		return Block{}, false
	}
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

func (n *Node) executionResultAlreadyCommitted(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
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

func (n *Node) committedReplayFenceHeight() uint64 {
	if n == nil {
		return 0
	}
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

func (n *Node) isCommittedReplayHeight(height uint64) bool {
	return height > 0 && height <= n.committedReplayFenceHeight()
}

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
		if existing := strings.TrimSpace(n.committed[height]); existing != "" {
			return false
		}
	}
	if n.commitInFlight == nil {
		n.commitInFlight = make(map[uint64]string)
	}
	if existing := strings.TrimSpace(n.commitInFlight[height]); existing != "" {
		return false
	}
	n.commitInFlight[height] = hash
	return true
}

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
	if existing := strings.TrimSpace(n.commitInFlight[height]); existing == "" || existing == hash {
		delete(n.commitInFlight, height)
	}
}

func (n *Node) finalizeExecutionResult(epoch uint64, execHash string, txMerkle string, results []ExecutionResult, signers []string) bool {
	if n.consensusRecomputePauseActive() {
		return false
	}
	if ready, reason := n.localExecutionFinalityReady(epoch); !ready {
		if n.shouldLogLivenessReason(fmt.Sprintf("exec_commit_defer:%d:%s", epoch, reason), 5*time.Second) {
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
	leaderBlock, ok := n.executionProposalBlockForResult(epoch, execHash, txMerkle)
	if !ok || leaderBlock.ID != epoch {
		return false
	}
	if !n.leaderBlockCommitmentsReadyForFinality(leaderBlock) {
		return false
	}
	if n.executionResultAlreadyCommitted(epoch) {
		return n.advanceConsensusToCommittedTip("finalize_execution_result_already_committed")
	}
	if txMerkle != "" && leaderBlock.MempoolRoot != txMerkle {
		return false
	}
	if txMerkle == "" && leaderBlock.MempoolRoot != "" {
		return false
	}
	precommitVotes, required, commitReady, _ := n.executionCommitPrecondition(epoch, leaderBlock)
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

	expected := leaderBlock.StateRoot
	if expected == "" {
		expected = n.ExecuteBlockAndGetStateRoot(leaderBlock)
	}
	if expected == "" || expected != execHash {
		return false
	}

	final := leaderBlock
	final.BlockTime = LogicalTimeForEpochTick(epoch, TickFinalize)
	final.Timestamp = int64(SystemTimeUnits(final.BlockTime))
	final.StateRoot = execHash
	policy := n.executionQuorumPolicy(epoch)
	if policy.Required <= 0 {
		policy.Required = required
	}
	if policy.StrictRequired <= 0 {
		policy.StrictRequired = strictExecSupermajority(len(n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))))
	}
	if policy.Required != required && required > 0 {
		policy.Required = required
		if policy.StrictRequired > 0 && required < policy.StrictRequired {
			policy.Mode = "DEGRADED"
			policy.Relaxed = true
		}
	}
	if strings.TrimSpace(policy.Mode) == "" {
		if policy.StrictRequired > 0 && policy.Required < policy.StrictRequired {
			policy.Mode = "DEGRADED"
		} else {
			policy.Mode = "NORMAL"
		}
	}
	if strings.TrimSpace(policy.Version) == "" {
		policy.Version = quorumPolicyVersionV1
	}
	commitSigners := canonicalValidatorIDs(signers)
	// Quorum metadata is part of the proposal/hash envelope, but finalized
	// metadata must describe the quorum evidence actually used to commit. When
	// the live ready set degrades after proposal time, preserving a stale
	// NORMAL/3 policy would make the node reject its own valid DEGRADED/2
	// recovery commit.
	proposalPolicyDefined := strings.TrimSpace(leaderBlock.QuorumPolicyVersion) != "" ||
		strings.TrimSpace(leaderBlock.ConsensusMode) != "" ||
		leaderBlock.ActiveReadyCount > 0 ||
		leaderBlock.RequiredQuorum > 0 ||
		leaderBlock.StrictQuorum > 0
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
	if proposalPolicyDefined && proposalPolicyCompatible {
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

	final.ExecutionResults = canonicalExecutionResults(results)
	final.Signatures = commitSigners
	if len(final.Signatures) == 0 && len(final.ExecutionResults) > 0 {
		derived := make([]string, 0, len(final.ExecutionResults))
		for _, res := range final.ExecutionResults {
			derived = append(derived, res.Signer)
		}
		final.Signatures = canonicalValidatorIDs(derived)
	}
	if len(final.ExecutionResults) > 0 && len(final.Signatures) > 0 {
		allowed := make(map[string]struct{}, len(final.Signatures))
		for _, signer := range final.Signatures {
			allowed[signer] = struct{}{}
		}
		filtered := make([]ExecutionResult, 0, len(final.ExecutionResults))
		for _, res := range final.ExecutionResults {
			if _, ok := allowed[res.Signer]; ok {
				filtered = append(filtered, res)
			}
		}
		final.ExecutionResults = filtered
	}
	for i := range final.ExecutionResults {
		if strings.TrimSpace(final.ExecutionResults[i].BlockHash) == "" {
			final.ExecutionResults[i].BlockHash = executionVoteProposalHashForFinalBlock(final)
		}
		if final.ExecutionResults[i].Round == 0 {
			final.ExecutionResults[i].Round = final.Round
		}
		final.ExecutionResults[i].Height = final.ID
		final.ExecutionResults[i].TxMerkle = txMerkle
	}
	n.attachFinalityCommitments(&final)
	final.BlockHash = HashBlock(final)
	n.attachFinalityCertificate(&final)
	if n.executionResultAlreadyCommitted(final.ID) {
		return n.advanceConsensusToCommittedTip("finalize_execution_result_commit_raced")
	}
	commitCount, commitRequired := n.recordDeterministicCommitVotes(final.ID, final.BlockHash, final.Signatures)
	if commitRequired == 0 {
		commitRequired = required
	}
	if commitCount < commitRequired {
		if DebugConsensus {
			log.Printf("[EXEC-COMMIT-DEFER] height=%d reason=commit_votes_shortfall votes=%d required=%d signers=%d block=%s",
				final.ID,
				commitCount,
				commitRequired,
				len(final.Signatures),
				ShortHash(final.BlockHash),
			)
		}
		return false
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

	finalValidators := n.freezeValidatorSetForHeight(final.ID, n.GetConsensusValidators(int(final.ID)))
	if err := verifyBlockQuorumMetadata(final, len(finalValidators)); err != nil {
		log.Printf("[EXEC-COMMIT-REJECT] height=%d block=%s reason=%s", final.ID, ShortHash(final.BlockHash), err.Error())
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}
	if err := n.verifyFinalityCommitments(final, finalValidators); err != nil {
		log.Printf("[EXEC-COMMIT-REJECT] height=%d block=%s reason=%s", final.ID, ShortHash(final.BlockHash), err.Error())
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}
	if err := n.validateCommittedBlockQuorumEvidence(final); err != nil {
		log.Printf("[EXEC-COMMIT-REJECT] height=%d block=%s reason=%s", final.ID, ShortHash(final.BlockHash), err.Error())
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}

	before := n.Blockchain.Height()
	n.setLogicalTick(epoch, TickFinalize)
	if err := n.ReceiveBlock(final, n.Blockchain); err != nil {
		n.invalidateExecutionProposalAfterCommitFailure(epoch, leaderBlock, err)
		return false
	}
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
	}
	return true
}

func (n *Node) handleExecutionResultMsg(res ExecutionResultMsg) {
	n.processExecutionResultMsg(res, true)
}

func (n *Node) queueExecResult(res ExecutionResultMsg) {
	res.Signer = normalizeValidatorID(res.Signer)
	if res.HeightHint == 0 || res.Signer == "" || res.ExecHash == "" {
		return
	}
	if n.isCommittedReplayHeight(res.HeightHint) {
		return
	}
	committedHeight := n.committedReplayFenceHeight()
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

func (n *Node) shouldTreatUnresolvedExecutionVoteAsStaleAccept(res ExecutionResultMsg, currentEpoch uint64) bool {
	if n == nil || res.HeightHint == 0 || currentEpoch == 0 {
		return false
	}
	if res.HeightHint != currentEpoch {
		return false
	}
	currentRound, _, ok := n.consensusRoundSnapshot(currentEpoch)
	if !ok {
		currentRound = n.localProposerRoundForHeight(currentEpoch)
	}
	return res.RoundHint == currentRound
}

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
	currentEpoch := n.currentEpoch()
	// Future-epoch execution votes are expected during late join/catch-up.
	// Queue them instead of misclassifying as "non-active validator".
	if res.HeightHint > currentEpoch {
		if allowQueue {
			n.queueExecResult(res)
			n.logExecutionVoteDrop("queued_future_epoch", res, execProposalSnapshot{})
		}
		return
	}
	if executionVoteTooFarBehind(currentEpoch, res.HeightHint) {
		return
	}
	if n.isCommittedReplayHeight(res.HeightHint) {
		n.logExecutionVoteDrop("stale_committed_height", res, execProposalSnapshot{})
		return
	}
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
	allowed := false
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
	n.commitMu.Lock()
	committedHeight := n.committedHeight
	n.commitMu.Unlock()
	committedEpoch := res.HeightHint <= committedHeight

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
	projectedVotes := getExecCountGlobal(targetEpoch, proposalSnap.ProposalKey, expected, proposalSnap.TxMerkle) + 1
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
	if res.Signature == "" {
		n.logExecutionVoteDrop("missing_signature", res, proposalSnap)
		return
	}
	sig, err := hex.DecodeString(res.Signature)
	if err != nil {
		n.logExecutionVoteDrop("invalid_signature_encoding", res, proposalSnap)
		return
	}
	candidates := execResultPubKeyCandidates(res.Signer)
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

	if res.TxMerkle != leaderBlock.MempoolRoot {
		n.logExecutionVoteDrop("tx_merkle_mismatch", res, proposalSnap)
		return
	}
	if allowed, reason := n.allowExecutionVoteIngress(res.Signer, res.HeightHint, proposalSnap.ProposalKey, res.ExecHash, res.TxMerkle); !allowed {
		n.logExecutionVoteDrop(reason, res, proposalSnap)
		return
	}
	if res.ExecHash != expected {
		staleByProposal := strings.TrimSpace(res.BlockHashHint) != "" && res.BlockHashHint != proposalSnap.BlockHash
		if staleByProposal {
			if DebugConsensus {
				fmt.Printf("[EXEC-VOTE] stale_proposal ignored signer=%s epoch=%d vote_block=%s current_block=%s\n",
					ShortID(res.Signer), targetEpoch, ShortHash(res.BlockHashHint), ShortHash(proposalSnap.BlockHash))
			}
			n.logExecutionVoteDrop("stale_proposal", res, proposalSnap)
			return
		}
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

	storedCount, ok, equivocation := recordExecResultGlobal(targetEpoch, proposalSnap.ProposalKey, res.ExecHash, res.TxMerkle, ExecutionResult{
		Height:     targetEpoch,
		Round:      res.RoundHint,
		BlockHash:  proposalSnap.BlockHash,
		Signer:     res.Signer,
		ResultHash: res.ExecHash,
		TxMerkle:   res.TxMerkle,
		Signature:  strings.TrimSpace(res.Signature),
	})
	if equivocation {
		if DebugConsensus {
			fmt.Printf("Execution equivocation detected from %s @ epoch %d\n", ShortID(res.Signer), targetEpoch)
		}
		n.logExecutionVoteDrop("exec_equivocation", res, proposalSnap)
		n.handleExecutionEquivocationPolicy(res.Signer, targetEpoch, res.ExecHash)
		return
	}
	if !ok {
		n.logExecutionVoteDrop("duplicate_exec_vote", res, proposalSnap)
		return
	}
	if !n.markExecSignerSeenForProposal(targetEpoch, proposalSnap.ProposalKey, res.Signer) {
		n.logExecutionVoteDrop("duplicate_signer_proposal", res, proposalSnap)
		return
	}
	n.mirrorConsensusExecVote(targetEpoch, proposalSnap.BlockHash, ExecutionResult{
		Height:     targetEpoch,
		Round:      res.RoundHint,
		BlockHash:  proposalSnap.BlockHash,
		Signer:     res.Signer,
		ResultHash: res.ExecHash,
		TxMerkle:   res.TxMerkle,
		Signature:  strings.TrimSpace(res.Signature),
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

	epochValidators := n.freezeValidatorSetForHeight(targetEpoch, n.GetConsensusValidators(int(targetEpoch)))
	total := len(epochValidators)
	required := n.executionQuorumRequired(targetEpoch)
	if required == 0 {
		required = strictExecSupermajority(total)
	}
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

	results, signers, _, ok := getExecResultsGlobal(targetEpoch, proposalSnap.ProposalKey, res.ExecHash, res.TxMerkle)
	if !ok {
		return
	}

	if DebugConsensus {
		fmt.Printf("Execution quorum reached (hash=%s votes=%d/%d)\n",
			ShortHash(res.ExecHash), storedCount, total)
	}

	_ = n.maybeAdoptProposalOnExecutionVote(leaderBlock)
	n.execResultsMu.Lock()
	_ = n.setQuorumLockedProposalLocked(leaderBlock, "quorum_precommit", storedCount, required)
	n.execResultsMu.Unlock()
	freezeExecPool(targetEpoch, proposalSnap.ProposalKey, res.ExecHash)
	_ = n.finalizeExecutionResult(targetEpoch, res.ExecHash, res.TxMerkle, results, signers)
}

func (n *Node) recordCandidateExecutionResult(res ExecutionResultMsg) bool {
	if res.Signer == "" || res.ExecHash == "" || res.HeightHint == 0 {
		return false
	}
	n.candidateMu.RLock()
	cand, ok := n.candidates[res.Signer]
	if !ok || cand == nil || cand.PermanentBan {
		n.candidateMu.RUnlock()
		return false
	}
	if cand.BanUntil > 0 && res.HeightHint < cand.BanUntil {
		n.candidateMu.RUnlock()
		return false
	}
	pub := cand.PubKey
	n.candidateMu.RUnlock()

	if len(pub) != ed25519.PublicKeySize || res.Signature == "" {
		return false
	}
	sig, err := hex.DecodeString(res.Signature)
	if err != nil {
		return false
	}
	if !verifyExecutionResultSignature(res, []ed25519.PublicKey{pub}, sig) {
		return false
	}

	expected := ""
	if res.HeightHint == n.currentEpoch() {
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
	pending := false
	if cand.PendingMatch != nil {
		pending = cand.PendingMatch[res.HeightHint]
	}
	n.candidateMu.Unlock()

	if pending {
		var expected string
		n.Blockchain.mu.RLock()
		if res.HeightHint < uint64(len(n.Blockchain.Blocks)) {
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

func (n *Node) replayQueuedExecutionVotes() {
	n.execResultsMu.Lock()
	if len(n.queuedExecVotes) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	queued := n.queuedExecVotes
	n.queuedExecVotes = make(map[string][]ExecutionResultMsg)
	n.execResultsMu.Unlock()

	for _, msgs := range queued {
		for _, msg := range msgs {
			n.processExecutionResultMsg(msg, true)
		}
	}
	n.tryFinalizeFromStoredResults()
}

func (n *Node) processQueuedExecutionVotesForProposal(block Block) {
	if n == nil || block.ID == 0 {
		return
	}
	proposalSnap := proposalSnapshotFromBlock(block)
	if proposalSnap.Epoch == 0 {
		return
	}
	key := fmt.Sprintf("%d", block.ID)
	n.execResultsMu.Lock()
	if len(n.queuedExecVotes) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	msgs := n.queuedExecVotes[key]
	if len(msgs) == 0 {
		n.execResultsMu.Unlock()
		n.tryFinalizeFromStoredResults()
		return
	}
	replay := make([]ExecutionResultMsg, 0, len(msgs))
	remaining := make([]ExecutionResultMsg, 0, len(msgs))
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

	for _, msg := range replay {
		n.processExecutionResultMsg(msg, true)
	}
	n.tryFinalizeFromStoredResults()
}

func (n *Node) tryFinalizeProposalIfQuorum(block Block, reason string) bool {
	if n == nil || block.ID == 0 || block.ID != n.currentEpoch() {
		return false
	}
	if n.consensusRecomputePauseActive() {
		return false
	}
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	total := len(validators)
	required := n.executionQuorumRequired(block.ID)
	if required == 0 {
		required = strictExecSupermajority(total)
	}
	if total == 0 || required == 0 {
		return false
	}
	execHash := strings.TrimSpace(block.StateRoot)
	if execHash == "" {
		execHash = strings.TrimSpace(n.ExecuteBlockAndGetStateRoot(block))
	}
	if execHash == "" {
		return false
	}
	txMerkle := block.MempoolRoot
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, execHash)
	if proposalKey == "" {
		return false
	}
	count := getExecCountGlobal(block.ID, proposalKey, execHash, txMerkle)
	if count < required {
		return false
	}
	results, signers, _, ok := getExecResultsGlobal(block.ID, proposalKey, execHash, txMerkle)
	if !ok {
		return false
	}
	if strings.TrimSpace(reason) == "" {
		reason = "proposal_existing_quorum"
	}
	_ = n.maybeAdoptProposalOnExecutionVote(block)
	n.execResultsMu.Lock()
	_ = n.setQuorumLockedProposalLocked(block, reason, count, required)
	n.execResultsMu.Unlock()
	freezeExecPool(block.ID, proposalKey, execHash)
	return n.finalizeExecutionResult(block.ID, execHash, txMerkle, results, signers)
}

func (n *Node) tryFinalizeFromStoredResults() {
	if n.consensusRecomputePauseActive() {
		return
	}
	epoch := n.currentEpoch()
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	total := len(validators)
	required := n.executionQuorumRequired(epoch)
	if required == 0 {
		required = strictExecSupermajority(total)
	}
	if total == 0 || required == 0 {
		return
	}
	for _, block := range n.candidateProposalBlocksForEpoch(epoch) {
		if block.ID != epoch {
			continue
		}
		if n.tryFinalizeProposalIfQuorum(block, "stored_results_quorum") {
			return
		}
	}
}

func (n *Node) hasFinalExecutionResult(epoch uint64, execHash string, txMerkle string) bool {
	if execHash == "" {
		return false
	}
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
	total := len(validators)
	required := n.executionQuorumRequired(epoch)
	if required == 0 {
		required = strictExecSupermajority(total)
	}
	if total == 0 || required == 0 {
		return false
	}
	for _, block := range n.candidateProposalBlocksForEpoch(epoch) {
		if block.ID != epoch {
			continue
		}
		stateRoot := block.StateRoot
		if stateRoot == "" {
			stateRoot = execHash
		}
		proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, stateRoot)
		if getExecCountGlobal(epoch, proposalKey, execHash, txMerkle) >= required {
			return true
		}
	}
	return false
}

func (n *Node) handleCommitMsg(cm CommitMsg) {
	// Commit messages are ignored in result-gossip consensus.
	_ = cm
}

func (n *Node) BroadcastBlock(block Block) {
	if ResultGossipOnly {
		return
	}
	if n.BlockTopic == nil && n.TopicBlocks == nil {
		fmt.Println("Block topic not initialized")
		return
	}

	msg := Message{Type: MsgBlock, Data: MustJSON(block)}
	data, err := MarshalP2PMessage(msg)
	if err != nil {
		data, _ = json.Marshal(block)
	}

	if n.BlockTopic != nil {
		if err := n.BlockTopic.Publish(context.Background(), data); err != nil {
			fmt.Println("Block publish failed:", err)
			return
		}
	} else if n.TopicBlocks != nil {
		if err := n.TopicBlocks.Publish(context.Background(), data); err != nil {
			fmt.Println("Block publish failed:", err)
			return
		}
	}

	if DebugConsensus {
		fmt.Printf("Block published | height=%d\n", block.ID)
	}
}

func (n *Node) DiscoverPeers() {
	ctx := n.RootContext()
	seeds := n.seedsSnapshot()

	for _, seed := range seeds {
		seedAddr, err := ma.NewMultiaddr(seed)
		if err != nil {
			continue
		}

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
		if err := n.Host.Connect(ctx, *seedInfo); err == nil {
			n.Host.ConnManager().TagPeer(seedInfo.ID, "bootstrap", 100)

			if DebugNet {
				fmt.Println("Ã°Å¸â€â€” Connected to seed:", seedInfo.ID.String())
			}
			n.recordDialSuccess(seedInfo.ID.String())
		} else {
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
	for _, topic := range canonicalTopics {
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
func (n *Node) validatorSetHash() string {
	nextHeight := n.Blockchain.Height() + 1
	if validatorOnboardingStrictActivationEnabled() {
		if active, _ := n.selfActiveValidatorAt(nextHeight); !active {
			return n.stableFrozenHashForAdvertise(nextHeight)
		}
	}
	if hash, _ := n.runtimeAdvertisedValidatorSetHash(nextHeight); hash != "" {
		return hash
	}
	return n.stableFrozenHashForAdvertise(nextHeight)
}

func (n *Node) peerHelloAdvertiseIdentity(height uint64) (string, string, string) {
	role := normalizeNodeRole(n.Role)
	if role != "validator" {
		return role, "", ""
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	active, _ := n.selfActiveValidatorAt(height)
	if validatorOnboardingStrictActivationEnabled() && !active {
		return "full", "", ""
	}
	if !active {
		return role, "", ""
	}
	pubHex := ""
	if len(n.ValidatorKey.PublicKey) == ed25519.PublicKeySize {
		pubHex = hex.EncodeToString(n.ValidatorKey.PublicKey)
	}
	if pubHex == "" {
		return role, "", ""
	}
	// Peer hello is an identity advertisement, not a participation vote.
	// Existing-chain validators must continue to identify themselves even while
	// startup/sync gates temporarily keep them out of vote/propose mode.
	return "validator", n.ID, pubHex
}

func (n *Node) outboundPeerHello() PeerHello {
	role, validatorID, validatorPubKey := n.peerHelloAdvertiseIdentity(n.currentEpoch())
	validatorSetHeight := n.Blockchain.Height() + 1
	nextValidatorSetHash := strings.TrimSpace(n.deterministicNextValidatorSetHash(validatorSetHeight, n.validatorSetHash()))
	tipHash := ""
	if n.Blockchain != nil {
		tipHash = strings.TrimSpace(n.Blockchain.LastBlock().BlockHash)
	}
	hello := PeerHello{
		ChainID:              ChainID,
		GenesisHash:          GenesisHash,
		Version:              Version,
		ConsensusHash:        consensusParamsHash(),
		Role:                 role,
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
func (n *Node) setGossipQuiet(quiet bool) {
	n.peerStateMu.Lock()
	n.gossipQuiet = quiet
	n.peerStateMu.Unlock()
}
func (n *Node) isGossipQuiet() bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.gossipQuiet
}
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
func (n *Node) isPeerConnected(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.connectedPeers[peerID]
}
func (n *Node) setPeerConnectedAt(peerID string, t time.Time) {
	n.peerStateMu.Lock()
	n.peerConnectedAt[peerID] = t
	n.peerStateMu.Unlock()
}
func (n *Node) clearPeerConnectedAt(peerID string) {
	n.peerStateMu.Lock()
	delete(n.peerConnectedAt, peerID)
	n.peerStateMu.Unlock()
}
func (n *Node) peerConnectedFor(peerID string) time.Duration {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if t, ok := n.peerConnectedAt[peerID]; ok {
		return time.Since(t)
	}
	return 0
}

func (n *Node) hasActivePeerConnection(pid peer.ID) bool {
	if n == nil || n.Host == nil || pid == "" {
		return false
	}
	if n.Host.Network().Connectedness(pid) == network.Connected {
		return true
	}
	return len(n.Host.Network().ConnsToPeer(pid)) > 0
}

func (n *Node) shouldForceGraft(peerID string) bool {
	now := time.Now()
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if t, ok := n.peerGraftAt[peerID]; ok {
		if now.Sub(t) < peerGraftCooldown {
			return false
		}
	}
	n.peerGraftAt[peerID] = now
	return true
}
func (n *Node) markPeerConnecting(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.connectingPeers[peerID] {
		return false
	}
	n.connectingPeers[peerID] = true
	return true
}
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

func isLocalhostPeerAddr(addr string) bool {
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

func isDialRefusedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused")
}

func (n *Node) dialFailureCount(peerID string) int {
	if n == nil || peerID == "" {
		return 0
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.peerDialFailures[peerID]
}

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
func (n *Node) canDialPeerID(peerID string) bool {
	if peerID == "" {
		return true
	}
	if !n.peerAdmissionAllowed(peerID) {
		return false
	}
	if n.isPeerQuarantined(peerID) {
		return false
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	next, ok := n.peerDialNext[peerID]
	if !ok || next.IsZero() {
		return true
	}
	return time.Now().After(next)
}
func (n *Node) recordDialFailure(peerID string) {
	if peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	failures := n.peerDialFailures[peerID] + 1
	n.peerDialFailures[peerID] = failures
	n.peerDialNext[peerID] = time.Now().Add(dialBackoffDelay(failures))
	n.notePeerDialScore(peerID, false)
}
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
func (n *Node) decayDialFailures(now time.Time) {
	if n == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	n.peerStateMu.Lock()
	for peerID, next := range n.peerDialNext {
		if next.IsZero() || now.After(next.Add(dialBackoffMax)) {
			delete(n.peerDialNext, peerID)
			delete(n.peerDialFailures, peerID)
		}
	}
	n.peerStateMu.Unlock()
}
func quarantineDurationFor(reason string) time.Duration {
	switch {
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
	delete(n.peerHelloNonces, peerID)
	delete(n.peerHelloOK, peerID)
	delete(n.peerSuspectAt, peerID)
	delete(n.peerHashMatch, peerID)
	delete(n.peerToValidator, peerID)
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
	prefix := peerID + "|"
	for key := range n.peerDriftState {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerDriftState, key)
		}
	}
	for key := range n.peerSyncOnlyLastDropLog {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerSyncOnlyLastDropLog, key)
		}
	}
	n.peerStateMu.Unlock()
}
func remotePeerIDFromMismatchError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	markers := []string{
		"remote key matches ",
		"but got ",
	}
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		tail := strings.TrimSpace(msg[idx+len(marker):])
		if tail == "" {
			continue
		}
		fields := strings.Fields(tail)
		if len(fields) == 0 {
			continue
		}
		candidate := strings.Trim(fields[0], " ,.;:)]}\"'")
		if strings.HasPrefix(candidate, "12D3") {
			return candidate
		}
	}
	return ""
}
func peerAddrWithPeerID(rawAddr, peerID string) (string, bool) {
	rawAddr = strings.TrimSpace(rawAddr)
	peerID = strings.TrimSpace(peerID)
	if rawAddr == "" || peerID == "" {
		return "", false
	}
	base, _, hasPID := splitPeerAddress(rawAddr)
	if hasPID {
		return fmt.Sprintf("%s/p2p/%s", base, peerID), true
	}
	if strings.HasPrefix(rawAddr, "/") {
		if _, err := ma.NewMultiaddr(rawAddr); err == nil {
			return fmt.Sprintf("%s/p2p/%s", rawAddr, peerID), true
		}
	}
	return "", false
}
func replacePeerAddrForBase(list []string, fixedAddr string) []string {
	fixedAddr = strings.TrimSpace(fixedAddr)
	if fixedAddr == "" {
		return list
	}
	base := stripP2PComponent(fixedAddr)
	if base == "" {
		return list
	}
	out := make([]string, 0, len(list)+1)
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
func (n *Node) refreshPeerIDMismatch(rawAddr, expectedPeerID string, dialErr error) bool {
	if n == nil || expectedPeerID == "" || rawAddr == "" || dialErr == nil {
		return false
	}
	// Auto-heal only for private/LAN targets to avoid trusting public mismatches.
	if !isPeerAddrPrivate(rawAddr) {
		return false
	}
	remotePeerID := remotePeerIDFromMismatchError(dialErr)
	if remotePeerID == "" || remotePeerID == expectedPeerID {
		return false
	}
	fixedAddr, ok := peerAddrWithPeerID(rawAddr, remotePeerID)
	if !ok {
		return false
	}
	// Never keep stale self entries.
	if n.Host != nil && remotePeerID == n.Host.ID().String() {
		n.forgetPeer(expectedPeerID, "self_dial")
		return true
	}
	persistent, seeds := n.configPeerListsSnapshot()
	persistent = replacePeerAddrForBase(persistent, fixedAddr)
	seeds = replacePeerAddrForBase(seeds, fixedAddr)
	n.setConfigPeerLists(persistent, seeds)
	if PersistPeerIDRefresh {
		if err := savePersistentPeers(n.DataDir, n.ID, persistent); err != nil && DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to persist peer-id refresh for %s: %v\n", expectedPeerID, err)
		}
	}
	base, _, hasBase := splitPeerAddress(rawAddr)
	if hasBase {
		ValidatorAddrBook.mu.Lock()
		for vid, addr := range ValidatorAddrBook.m {
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
func (n *Node) forgetPeer(peerID, reason string) {
	if peerID == "" {
		return
	}
	n.clearPeerState(peerID)
	if n.Host != nil {
		if pid, err := peer.Decode(peerID); err == nil {
			n.Host.Peerstore().ClearAddrs(pid)
		}
	}
	ValidatorAddrBook.mu.Lock()
	for vid, addr := range ValidatorAddrBook.m {
		if strings.Contains(addr, peerID) {
			delete(ValidatorAddrBook.m, vid)
		}
	}
	ValidatorAddrBook.mu.Unlock()
	persistent, seeds := n.configPeerListsSnapshot()
	filteredPersistent := make([]string, 0, len(persistent))
	filteredSeeds := make([]string, 0, len(seeds))
	persistentChanged := false
	seedsChanged := false
	for _, addr := range persistent {
		if strings.Contains(addr, peerID) {
			persistentChanged = true
			continue
		}
		filteredPersistent = append(filteredPersistent, addr)
	}
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
			if err := savePersistentPeers(n.DataDir, n.ID, filteredPersistent); err != nil && DebugNet {
				fmt.Printf("?? Failed to persist peers list: %v\n", err)
			}
		}
	}
	if DebugNet {
		fmt.Printf("?? Peer forgotten: %s reason=%s\n", peerID, reason)
	}
}
func (n *Node) quarantinePeer(peerID, reason string) {
	if peerID == "" {
		return
	}
	duration := quarantineDurationFor(reason)
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
func (n *Node) disconnectPeerID(peerID, reason string) {
	if peerID == "" {
		return
	}
	n.observePeerDisconnect(reason)
	n.quarantinePeer(peerID, reason)
	n.recordDialFailure(peerID)
	n.clearPeerHello(peerID)
	if n.Host == nil {
		return
	}
	pid, err := peer.Decode(peerID)
	if err != nil {
		return
	}
	_ = n.Host.Network().ClosePeer(pid)
}

func peerIdentityFromAddrOrID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if _, pid, ok := splitPeerAddress(raw); ok && strings.TrimSpace(pid) != "" {
		return strings.TrimSpace(pid), true
	}
	if _, err := peer.Decode(raw); err == nil {
		return raw, true
	}
	return "", false
}

func peerHelloAdvertisedPeerID(hello PeerHello) string {
	_, pid, ok := splitPeerAddress(strings.TrimSpace(hello.P2PAddr))
	if !ok {
		return ""
	}
	return strings.TrimSpace(pid)
}

func validatorIdentityPeerID(peerAddr, advertisedAddr string) string {
	if pid, ok := peerIdentityFromAddrOrID(peerAddr); ok {
		return strings.TrimSpace(pid)
	}
	_, pid, ok := splitPeerAddress(strings.TrimSpace(advertisedAddr))
	if !ok {
		return ""
	}
	return strings.TrimSpace(pid)
}

func (n *Node) reserveValidatorPeerIdentity(peerID, validatorID, advertisedAddr string) bool {
	if n == nil {
		return true
	}
	validatorID = normalizeValidatorID(validatorID)
	peerID = strings.TrimSpace(peerID)
	if validatorID == "" || peerID == "" {
		return true
	}
	selfPeerID := ""
	if n.Host != nil {
		selfPeerID = n.Host.ID().String()
	}
	if selfID := normalizeValidatorID(n.ID); selfID != "" && validatorID == selfID && peerID != selfPeerID {
		log.Printf("[DUPLICATE-NODE-ID] validator=%s existing_peer=%s new_peer=%s action=reject reason=local_validator_id_claim",
			validatorID, selfPeerID, peerID)
		n.disconnectPeerID(peerID, "duplicate_local_validator_id")
		return false
	}

	if advertisedPeerID := strings.TrimSpace(peerHelloAdvertisedPeerID(PeerHello{P2PAddr: advertisedAddr})); advertisedPeerID != "" && advertisedPeerID != peerID {
		log.Printf("[DUPLICATE-NODE-ID] validator=%s advertised_peer=%s remote_peer=%s action=reject reason=advertised_peer_mismatch",
			validatorID, advertisedPeerID, peerID)
		n.disconnectPeerID(peerID, "duplicate_validator_peer_mismatch")
		return false
	}

	ValidatorAddrBook.mu.Lock()
	oldAddr := strings.TrimSpace(ValidatorAddrBook.m[validatorID])
	ValidatorAddrBook.mu.Unlock()
	if oldPeerID := strings.TrimSpace(peerHelloAdvertisedPeerID(PeerHello{P2PAddr: oldAddr})); oldPeerID != "" && oldPeerID != peerID {
		log.Printf("[DUPLICATE-NODE-ID] validator=%s existing_peer=%s new_peer=%s action=reject reason=address_book_conflict",
			validatorID, oldPeerID, peerID)
		n.disconnectPeerID(peerID, "duplicate_validator_id")
		return false
	}

	n.ensurePeerIsolationMaps()
	n.peerStateMu.Lock()
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

func peerHelloNonce() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		fallback := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(fallback[:16])
	}
	return hex.EncodeToString(b[:])
}

func peerHelloSignBytes(hello PeerHello) []byte {
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

func (n *Node) signPeerHello(hello *PeerHello) {
	if n == nil || hello == nil || n.Host == nil {
		return
	}
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
	sig, err := priv.Sign(peerHelloSignBytes(*hello))
	if err != nil {
		return
	}
	hello.SignatureHex = hex.EncodeToString(sig)
}

func (n *Node) peerHelloPublicKey(peerID string) (libp2pcrypto.PubKey, bool) {
	remotePeerID, ok := peerIdentityFromAddrOrID(peerID)
	if !ok {
		return nil, false
	}
	pid, err := peer.Decode(remotePeerID)
	if err != nil {
		return nil, false
	}
	if n != nil && n.Host != nil {
		if pub := n.Host.Peerstore().PubKey(pid); pub != nil {
			return pub, true
		}
	}
	pub, err := pid.ExtractPublicKey()
	if err != nil || pub == nil {
		return nil, false
	}
	return pub, true
}

func (n *Node) acceptPeerHelloNonce(peerID string, hello PeerHello) bool {
	nonce := strings.TrimSpace(hello.Nonce)
	if n == nil || peerID == "" || nonce == "" {
		return false
	}
	n.ensurePeerIsolationMaps()
	now := time.Now()
	key := peerID + "|" + nonce
	n.peerStateMu.Lock()
	for seenKey, seenAt := range n.peerHelloNonces {
		if now.Sub(seenAt) > peerHelloNonceTTL {
			delete(n.peerHelloNonces, seenKey)
		}
	}
	if _, exists := n.peerHelloNonces[key]; exists {
		n.peerStateMu.Unlock()
		return false
	}
	n.peerHelloNonces[key] = now
	n.peerStateMu.Unlock()
	return true
}

func (n *Node) verifyPeerHelloSignature(peerID string, hello PeerHello) bool {
	pub, ok := n.peerHelloPublicKey(peerID)
	if !ok {
		return false
	}
	if hello.Timestamp == 0 || strings.TrimSpace(hello.Nonce) == "" || strings.TrimSpace(hello.SignatureHex) == "" {
		return false
	}
	age := time.Since(time.Unix(hello.Timestamp, 0))
	if age < -peerHelloMaxClockSkew || age > peerHelloMaxClockSkew {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimSpace(hello.SignatureHex))
	if err != nil || len(sig) == 0 {
		return false
	}
	unsigned := hello
	unsigned.SignatureHex = ""
	ok, err = pub.Verify(peerHelloSignBytes(unsigned), sig)
	return err == nil && ok
}

func (n *Node) validatePeerHello(peerID string, hello PeerHello) bool {
	if advertisedPeerID := peerHelloAdvertisedPeerID(hello); advertisedPeerID != "" {
		if remotePeerID, ok := peerIdentityFromAddrOrID(peerID); ok && remotePeerID != advertisedPeerID {
			n.disconnectPeerID(peerID, "peer_id_mismatch")
			return false
		}
	}
	if hello.ChainID == "" || hello.ChainID != ChainID {
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
	if _, hasPubKey := n.peerHelloPublicKey(peerID); hasPubKey {
		if !n.verifyPeerHelloSignature(peerID, hello) {
			n.disconnectPeerID(peerID, "peer_hello_bad_signature")
			return false
		}
		if !n.acceptPeerHelloNonce(peerID, hello) {
			n.disconnectPeerID(peerID, "peer_hello_replay")
			return false
		}
	}
	if !n.reserveValidatorPeerIdentity(validatorIdentityPeerID(peerID, hello.P2PAddr), hello.ValidatorID, hello.P2PAddr) {
		return false
	}
	n.applyPeerHelloPubKey(hello)
	n.peerStateMu.Lock()
	n.peerHelloOK[peerID] = true
	delete(n.quarantineUntil, peerID)
	n.peerStateMu.Unlock()
	return true
}

func (n *Node) applyPeerHelloPubKey(hello PeerHello) {
	// Keep strict signed-announcement behavior on non-testnet.
	if !IsTestnet {
		return
	}
	validatorID := normalizeValidatorID(hello.ValidatorID)
	pubHex := strings.TrimSpace(hello.ValidatorPubKey)
	if validatorID == "" || pubHex == "" {
		return
	}
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return
	}
	validatorPubKeysMu.RLock()
	existing, existingOK := ValidatorPubKeys[validatorID]
	validatorPubKeysMu.RUnlock()
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
func (n *Node) isPeerHelloOK(peerID string) bool {
	if peerID == "" {
		return false
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.peerHelloOK[peerID]
}
func (n *Node) clearPeerHello(peerID string) {
	if peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	delete(n.peerHelloOK, peerID)
	n.peerStateMu.Unlock()
}
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
func (n *Node) recomputeGossipQuiet() {
	if n.Host == nil {
		return
	}
	peers := n.Host.Network().Peers()
	if len(peers) == 0 {
		n.setGossipQuiet(true)
		return
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	for _, pid := range peers {
		if match, ok := n.peerHashMatch[pid.String()]; !ok || !match {
			n.gossipQuiet = false
			return
		}
	}
	n.gossipQuiet = true
}
func (n *Node) isPeerQuarantined(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
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
func (n *Node) markHelloSent(peerID string) bool {
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if last, ok := n.peerHelloSentAt[peerID]; ok {
		if time.Since(last) < peerHelloCooldown {
			return false
		}
	}
	n.peerHelloSentAt[peerID] = time.Now()
	return true
}
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := n.openStream(ctx, pid, "/msc/peerinfo/1.0.0")
	if err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peer hello stream failed to %s: %v\n", pid, err)
		}
		n.recordDialFailure(pid.String())
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "peerinfo_protocol_mismatch")
		}
		return
	}
	defer s.Close()
	hello := n.outboundPeerHello()
	_ = json.NewEncoder(s).Encode(hello)
	n.recordDialSuccess(pid.String())
}
func decodePeerHelloPayload(raw json.RawMessage) (PeerHello, error) {
	var hello PeerHello
	if err := json.Unmarshal(raw, &hello); err == nil {
		if hello.ChainID != "" {
			return hello, nil
		}
	}
	var wrapped Message
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if wrapped.Type == MsgPeerHello && len(wrapped.Data) > 0 {
			if err := json.Unmarshal(wrapped.Data, &hello); err == nil {
				if hello.ChainID != "" {
					return hello, nil
				}
			}
		}
	}
	return PeerHello{}, fmt.Errorf("invalid peer hello payload")
}
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := n.openStream(ctx, pid, "/msc/peerinfo/1.0.0")
	if err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peer info stream failed to %s: %v\n", pid, err)
		}
		n.recordDialFailure(pid.String())
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "peerinfo_protocol_mismatch")
		}
		return
	}
	defer s.Close()
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)
	info := n.outboundPeerHello()
	_ = enc.Encode(info)
	var peerInfo PeerHello
	var raw json.RawMessage
	if err := dec.Decode(&raw); err == nil {
		decoded, derr := decodePeerHelloPayload(raw)
		if derr != nil {
			n.recordDialFailure(pid.String())
			return
		}
		peerInfo = decoded
		if n.validatePeerHello(pid.String(), peerInfo) {
			n.applyPeerInfo(pid.String(), peerInfo)
			n.recordDialSuccess(pid.String())
		}
	} else {
		n.recordDialFailure(pid.String())
	}
}
func (n *Node) sendPeersList(pid peer.ID) {
	if n.Host == nil {
		return
	}
	if n.isPeerQuarantined(pid.String()) {
		return
	}
	peers := n.collectPeerMultiaddrs()
	if len(peers) == 0 {
		return
	}
	self := stripP2PComponent(n.SelfAddr)
	out := make([]string, 0, len(peers))
	seen := make(map[string]struct{}, len(peers))
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
	msg := Message{
		Type: MsgPeers,
		Data: MustJSON(out),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := n.openStream(ctx, pid, "/msc/consensus/1.0.0")
	if err != nil {
		if DebugNet {
			fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Peers list stream failed to %s: %v\n", pid, err)
		}
		n.recordDialFailure(pid.String())
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(pid.String(), "consensus_protocol_mismatch")
		}
		return
	}
	defer s.Close()
	data, _ := json.Marshal(msg)
	_, _ = s.Write(append(data, '\n'))
}
func (n *Node) handlePeersList(peerAddr string, peers []string) {
	if len(peers) == 0 {
		return
	}
	self := stripP2PComponent(n.SelfAddr)
	uniq := make(map[string]struct{}, len(peers))
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
	list := make([]string, 0, len(uniq))
	for addr := range uniq {
		list = append(list, addr)
	}
	sort.Strings(list)
	list = sanitizePeerListWithPreferred(list, n.trustedPeerMultiaddrs())
	currentPersistent := n.persistentPeersSnapshot()
	merged := mergePeerLists(currentPersistent, list)
	sanitizedPersistent := sanitizePeerListWithPreferred(merged, n.trustedPeerMultiaddrs())
	n.setPersistentPeers(sanitizedPersistent)
	if err := savePersistentPeers(n.DataDir, n.ID, sanitizedPersistent); err != nil && DebugNet {
		fmt.Printf("Ã¢Å¡Â Ã¯Â¸Â Failed to persist peers list: %v\n", err)
	}
	n.connectToPeersAsync(sanitizePeerListWithPreferred(list, n.trustedPeerMultiaddrs()), 15*time.Second)
}
func shouldSyncForValidatorSetMismatch(localHeight, peerHeight uint64) bool {
	if peerHeight == 0 {
		return false
	}
	// Never roll back due to a stale peer advertisement.
	return peerHeight >= localHeight
}
func shouldForceSnapshotResyncForValidatorSetMismatch(localHeight, targetHeight uint64) bool {
	if targetHeight == 0 {
		return false
	}
	// Snapshot-first rule: if local is behind, bypass autoheal-repair churn
	// and force deterministic snapshot recovery immediately.
	return targetHeight > localHeight
}
func (n *Node) applyPeerInfo(peerAddr string, hello PeerHello) {
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
	expectedSource := "none"
	expectedHeight := uint64(0)
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
		sampleHeight := expectedHeight
		if sampleHeight == 0 {
			sampleHeight = hello.Height + 1
		}
		n.noteLateJoinAuthoritySample(sampleHeight, hello.ValidatorSetHash)
	}
	if expectedHeight > 0 {
		expectedHash, expectedSource = n.expectedValidatorSetHashWithSource(expectedHeight)
	}
	hashMatch := true
	if hello.ValidatorSetHash != "" && expectedHash != "" {
		hashMatch = hello.ValidatorSetHash == expectedHash
	}
	mismatch := !hashMatch && hello.ValidatorSetHash != "" && expectedHash != ""
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
	if n.peerToValidator == nil {
		n.peerToValidator = make(map[string]string)
	}
	if n.peerRole == nil {
		n.peerRole = make(map[string]string)
	}
	if n.peerHelloOK == nil {
		n.peerHelloOK = make(map[string]bool)
	}
	n.peerToValidator[peerAddr] = hello.ValidatorID
	n.peerRole[peerAddr] = peerRole
	n.peerSetHash[peerAddr] = hello.ValidatorSetHash
	n.peerTipHash[peerAddr] = hello.TipHash
	n.peerHashMatch[peerAddr] = hashMatch
	if hello.ValidatorID != "" {
		delete(n.validatorSuspect, hello.ValidatorID)
	}
	if hello.Height > 0 {
		// Reset peer ACK cursor from fresh hello advertisement.
		// A restarted peer may rejoin from a lower height, and stale higher ACK
		// would otherwise make us skip required historical blocks.
		n.peerAckHeight[peerAddr] = hello.Height
	}
	n.peerStateMu.Unlock()
	if hashMatch {
		n.clearPeerDriftState(peerAddr)
	}
	if effectiveValidatorPeer {
		n.HandlePeerHello(peerAddr, hello.ValidatorID, hello.P2PAddr)
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
		localHeight := n.Blockchain.Height()
		finalizedHeight := n.getFinalizedHeight()
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
		targetHeight := expectedHeight
		if targetHeight == 0 {
			targetHeight = hello.Height + 1
		}
		if n.shouldTreatValidatorSetMismatchAsPeerDrift(targetHeight, expectedHash, hello.ValidatorSetHash) {
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
	if prev, ok := n.peerAckHeight[peerAddr]; !ok || height > prev {
		n.peerAckHeight[peerAddr] = height
	}
	n.peerStateMu.Unlock()
}
func (n *Node) peerAckHeightFor(peerAddr string) uint64 {
	if peerAddr == "" {
		return 0
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	return n.peerAckHeight[peerAddr]
}
func (n *Node) shouldLogNoBlocks(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	now := time.Now()
	key := fmt.Sprintf("%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 60*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	// Keep map bounded to avoid unbounded growth on noisy peers.
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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
func (n *Node) shouldLogSentBlocks(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	now := time.Now()
	key := fmt.Sprintf("sent:%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 10*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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
func (n *Node) shouldLogPartialBatch(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	now := time.Now()
	key := fmt.Sprintf("partial:%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 10*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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

func (n *Node) shouldLogFinalizedDrift(peerID string, from, to uint64) bool {
	if n == nil {
		return false
	}
	now := time.Now()
	key := fmt.Sprintf("drift:%s:%d:%d", peerID, from, to)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 30*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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

func (n *Node) peerHeightSnapshot(peerID string) (peerHeight, peerFinalized uint64) {
	if n == nil || peerID == "" {
		return 0, 0
	}
	var validatorID string
	n.peerStateMu.Lock()
	peerHeight = n.peerAckHeight[peerID]
	validatorID = n.peerToValidator[peerID]
	n.peerStateMu.Unlock()

	if validatorID == "" {
		return peerHeight, peerFinalized
	}
	n.validatorMu.RLock()
	st := n.validatorStatus[validatorID]
	n.validatorMu.RUnlock()
	if st == nil {
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

func driftTupleKey(peerID, expected, got string) string {
	return peerID + "|" + expected + "|" + got
}

func (n *Node) recordFinalizedDrift(peerID string, from, to uint64, expected, got string, countDelta int) PeerDriftState {
	if n == nil || peerID == "" {
		return PeerDriftState{}
	}
	now := time.Now()
	key := driftTupleKey(peerID, expected, got)
	cutoff := now.Add(-2 * finalizedDriftWindow)

	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.peerDriftState == nil {
		n.peerDriftState = make(map[string]PeerDriftState)
	}
	for k, st := range n.peerDriftState {
		if !st.LastSeen.IsZero() && st.LastSeen.Before(cutoff) {
			delete(n.peerDriftState, k)
		}
	}

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

func (n *Node) isPeerSyncOnly(peerID string) bool {
	if n == nil || peerID == "" {
		return false
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
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

func isSyncOnlyAllowedMsgType(msgType string) bool {
	switch msgType {
	case MsgPeerHello, MsgGetBlocks, MsgBlockAck, MsgPing, MsgPong:
		return true
	default:
		return false
	}
}

func (n *Node) shouldLogSyncOnlyDrop(peerID, msgType string) bool {
	if n == nil || peerID == "" || msgType == "" {
		return false
	}
	now := time.Now()
	key := peerID + "|" + msgType
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	if n.peerSyncOnlyLastDropLog == nil {
		n.peerSyncOnlyLastDropLog = make(map[string]time.Time)
	}
	if prev, ok := n.peerSyncOnlyLastDropLog[key]; ok && now.Sub(prev) < finalizedDriftDropLogInterval {
		return false
	}
	n.peerSyncOnlyLastDropLog[key] = now
	return true
}

func (n *Node) clearPeerDriftState(peerID string) {
	if n == nil || peerID == "" {
		return
	}
	n.peerStateMu.Lock()
	defer n.peerStateMu.Unlock()
	delete(n.peerSyncOnlyUntil, peerID)
	delete(n.peerSyncOnlyClass, peerID)
	prefix := peerID + "|"
	for key := range n.peerDriftState {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerDriftState, key)
		}
	}
	for key := range n.peerSyncOnlyLastDropLog {
		if strings.HasPrefix(key, prefix) {
			delete(n.peerSyncOnlyLastDropLog, key)
		}
	}
}

func (n *Node) peerMaxDriftState(peerID string) (PeerDriftState, bool) {
	if n == nil || peerID == "" {
		return PeerDriftState{}, false
	}
	prefix := peerID + "|"
	var best PeerDriftState
	found := false
	n.peerStateMu.Lock()
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

func driftRangeNearTip(localFinalized, to uint64) bool {
	if localFinalized == 0 || localFinalized <= finalizedDriftNearTipSlack {
		return true
	}
	return to >= (localFinalized - finalizedDriftNearTipSlack)
}

func (n *Node) shouldLogFinalizedDriftPolicy(peerID, expected, got string) bool {
	if n == nil {
		return false
	}
	now := time.Now()
	key := fmt.Sprintf("drift-policy:%s:%s:%s", peerID, expected, got)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < finalizedDriftDropLogInterval {
		return false
	}
	n.noBlockLogAt[key] = now
	return true
}

func (n *Node) applyFinalizedDriftPolicy(peerID string, state PeerDriftState) {
	if n == nil || peerID == "" || state.Count <= finalizedDriftThreshold {
		return
	}
	now := time.Now()
	peerHeight, peerFinalized := n.peerHeightSnapshot(peerID)
	localHeight, localFinalized := n.localHeightSnapshot()
	class := classifyPeerDrift(peerHeight, peerFinalized, localHeight, localFinalized)
	syncOnlyUntil := now.Add(finalizedDriftCooldown)
	n.setPeerSyncOnly(peerID, class, syncOnlyUntil)

	key := driftTupleKey(peerID, state.Expected, state.Got)
	recomputeTriggered := false
	action := "sync_only"

	n.peerStateMu.Lock()
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

func (n *Node) shouldServeBlockRange(peerID string, from, to uint64, wantSnapshot bool) bool {
	if n == nil || peerID == "" {
		return false
	}
	if to < from {
		return false
	}
	// If a peer is repeatedly drifting, keep serving lane narrow and near-tip only.
	if drift, ok := n.peerMaxDriftState(peerID); ok && drift.Count > finalizedDriftThreshold {
		_, localFinalized := n.localHeightSnapshot()
		rangeLen := to - from
		if !driftRangeNearTip(localFinalized, to) || rangeLen > finalizedDriftMaxServeRange {
			if (DebugSync || DebugNet) && n.shouldLogFinalizedDriftPolicy(peerID, drift.Expected, drift.Got) {
				fmt.Printf("[DRIFT-SERVE] peer=%s denied range=%d-%d count=%d reason=history_or_range\n",
					peerID, from, to, drift.Count)
			}
			return false
		}
	}
	now := time.Now()
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
		cutoff := now.Add(-2 * time.Minute)
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
func (n *Node) shouldSendSnapshotToPeer(peerID string, height uint64) bool {
	if n == nil || peerID == "" || height == 0 {
		return false
	}
	now := time.Now()
	minInterval := 5 * time.Second
	if drift, ok := n.peerMaxDriftState(peerID); ok && drift.Count > finalizedDriftThreshold {
		minInterval = finalizedDriftSnapshotCooldown
		if drift.Count >= finalizedDriftEscalateThreshold {
			minInterval = finalizedDriftCooldown
		}
	}
	peerKey := fmt.Sprintf("snap-send-peer:%s", peerID)
	key := fmt.Sprintf("snap-send:%s:%d", peerID, height)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[peerKey]; ok && now.Sub(prev) < minInterval {
		return false
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < minInterval {
		return false
	}
	n.noBlockLogAt[peerKey] = now
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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
func (n *Node) shouldProcessSnapshotOffer(from string, height uint64) bool {
	if n == nil || from == "" || height == 0 {
		return false
	}
	now := time.Now()
	key := fmt.Sprintf("snap-offer:%s:%d", from, height)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < 5*time.Second {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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
func (n *Node) shouldLogNetworkProbe(tag string, interval time.Duration) bool {
	if n == nil || strings.TrimSpace(tag) == "" {
		return false
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	now := time.Now()
	key := fmt.Sprintf("probe:%s", tag)
	n.noBlockLogMu.Lock()
	defer n.noBlockLogMu.Unlock()
	if n.noBlockLogAt == nil {
		n.noBlockLogAt = make(map[string]time.Time)
	}
	if prev, ok := n.noBlockLogAt[key]; ok && now.Sub(prev) < interval {
		return false
	}
	n.noBlockLogAt[key] = now
	if len(n.noBlockLogAt) > 2048 {
		cutoff := now.Add(-2 * time.Minute)
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
func (n *Node) recordPeerFlap(peerID string) {
	now := time.Now()
	cutoff := now.Add(-peerFlapWindow)
	quarantine := false
	n.peerStateMu.Lock()
	list := n.peerFlapTimes[peerID]
	filtered := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	n.peerFlapTimes[peerID] = filtered
	if len(filtered) >= peerFlapThreshold {
		quarantine = true
	}
	n.peerStateMu.Unlock()
	if quarantine {
		n.quarantinePeer(peerID, "peer_flap")
	}
}
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
	vid := n.peerToValidator[pid.String()]
	n.peerStateMu.Unlock()
	if vid != "" {
		n.touchValidator(vid, n.Blockchain.Height())
	}
	go n.exchangePeerInfo(pid)
	// Send peer list after a short hello window to avoid deterministic
	// "unverified peer" drops when both sides race peers-list first.
	go func() {
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
	vid := n.peerToValidator[pid.String()]
	if vid != "" {
		n.validatorSuspect[vid] = time.Now()
	}
	n.peerStateMu.Unlock()
	if vid != "" {
		n.markValidatorOffline(vid, "peer_disconnected")
	}
}
func (n *Node) removeValidatorByPeer(peerID string) {
	n.peerStateMu.Lock()
	vid, ok := n.peerToValidator[peerID]
	n.peerStateMu.Unlock()
	if !ok || vid == "" {
		return
	}
	n.markValidatorOffline(vid, "peer_suspect_expired")
	n.validatorMu.Lock()
	if st, ok := n.validatorStatus[vid]; ok && st != nil {
		// Keep status entry so inactive-removal logic can evict deterministically
		// after ValidatorInactiveBlocks instead of keeping the validator forever.
		if st.LastSeen.IsZero() || time.Since(st.LastSeen) > 20*time.Second {
			st.Active = false
		}
	}
	n.validatorMu.Unlock()
}
func (n *Node) monitorPeerSuspects() {
	ticker := time.NewTicker(peerSuspectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			var toRemove []string
			n.peerStateMu.Lock()
			for peerID, ts := range n.peerSuspectAt {
				if time.Since(ts) >= peerSuspectTimeout {
					toRemove = append(toRemove, peerID)
				}
			}
			n.peerStateMu.Unlock()
			for _, peerID := range toRemove {
				if n.Host != nil {
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
				prefix := peerID + "|"
				for key := range n.peerDriftState {
					if strings.HasPrefix(key, prefix) {
						delete(n.peerDriftState, key)
					}
				}
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
func (n *Node) startSelfHeal(ctx context.Context) {
	if n == nil || !SelfHealEnabled {
		return
	}
	interval := SelfHealInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	minPeers := SelfHealMinPeers
	if minPeers < 1 {
		minPeers = 1
	}
	lastHeight := uint64(0)
	lastProgress := time.Now()
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
			peers := len(n.Host.Network().Peers())
			stalled := false
			if SelfHealStallSeconds > 0 {
				observedHeight, _ := n.bestObservedSyncHeight()
				lagging := observedHeight > height && observedHeight > 0
				if lagging && time.Since(lastProgress) >= time.Duration(SelfHealStallSeconds)*time.Second {
					stalled = true
				}
			}
			if peers < minPeers || stalled {
				if n.Role == "validator" {
					targets := n.validatorMeshTargets()
					if len(targets) > 0 {
						n.connectToPeers(ctx, targets)
					}
				}
				persistent, seeds := n.configPeerListsSnapshot()
				extras := mergePeerLists(nil, persistent)
				extras = mergePeerLists(extras, seeds)
				extras = sanitizePeerListWithPreferred(extras, n.trustedPeerMultiaddrs())
				if len(extras) > 0 {
					n.connectToPeers(ctx, extras)
				}
				n.connectPubSubPeers()
				n.recomputeGossipQuiet()
			}
			n.decayDialFailures(time.Now())
		}
	}
}
func (n *Node) isTopicJoined(topic string) bool {
	if n.PubSub == nil {
		return false
	}
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
func signData(data map[string]interface{}, privKey ed25519.PrivateKey) ([]byte, error) {
	// Create a deterministic representation of the data for signing
	// Remove signature field if present
	delete(data, "signature")
	// Sort keys for consistent serialization
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buffer bytes.Buffer
	for _, k := range keys {
		value := data[k]
		buffer.WriteString(fmt.Sprintf("%s:%v|", k, value))
	}
	// Sign the data
	hash := sha256.Sum256(buffer.Bytes())
	return ed25519.Sign(privKey, hash[:]), nil
}
func validatorAnnounceSignBytes(nodeID string, pubKey string, reported uint64, finalized uint64, execEpoch uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%d|%d|%d|%t", nodeID, pubKey, reported, finalized, execEpoch, isValidator))
}
func validatorAnnounceSignBytesV2(nodeID string, pubKey string, p2pAddr string, reported uint64, finalized uint64, execEpoch uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d|%d|%d|%t", nodeID, pubKey, p2pAddr, reported, finalized, execEpoch, isValidator))
}
func validatorAnnounceSignBytesV3(nodeID string, pubKey string, p2pAddr string, reported uint64, finalized uint64, execEpoch uint64, validatorSetHeight uint64, validatorSetHash string, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%d|%d|%d|%d|%s|%t", nodeID, pubKey, p2pAddr, reported, finalized, execEpoch, validatorSetHeight, validatorSetHash, isValidator))
}
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
func validatorAnnounceSignBytesLegacy(nodeID string, pubKey string, height uint64, isValidator bool) []byte {
	return []byte(fmt.Sprintf("%s|%s|%d|%t", nodeID, pubKey, height, isValidator))
}
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
	reported := uint64(0)
	if n.Blockchain != nil {
		reported = n.Blockchain.Height()
	}
	finalized := n.getFinalizedHeight()
	if finalized == 0 {
		finalized = reported
	}
	execEpoch := finalized + 1
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
	if n.ValidatorKey.PrivateKey != nil && pubHex != "" {
		sig := ed25519.Sign(
			n.ValidatorKey.PrivateKey,
			validatorAnnounceSignBytesV2(n.ID, pubHex, n.SelfAddr, reported, finalized, execEpoch, true),
		)
		announcement.Signature = hex.EncodeToString(sig)
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
	msg := Message{
		Type: MsgValidatorAnnounce,
		Data: payload,
	}
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
		if err := n.ValidatorTopic.Publish(context.Background(), msgBytes); err != nil {
			if DebugConsensus {
				fmt.Printf("Validator broadcast failed: %v\n", err)
			}
			return
		}
	} else if n.PubSub != nil {
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
			errLower := strings.ToLower(err.Error())
			if strings.Contains(errLower, "peer id mismatch") ||
				strings.Contains(errLower, "dial to self attempted") {
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
			networkKey := fmt.Sprintf("msc-chain-network-%s", ChainID)
			mh, err := multihash.Sum([]byte(networkKey), multihash.SHA2_256, -1)
			if err != nil {
				continue
			}
			c := cid.NewCidV1(cid.Raw, mh)
			providersCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			// Announce ourselves as a provider
			dhtInst.Provide(providersCtx, c, true)
			// Look for other providers
			providers := dhtInst.FindProvidersAsync(providersCtx, c, 10)
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
	node *Node
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.node.Host.Connect(ctx, pi); err == nil {
		if cm := m.node.Host.ConnManager(); cm != nil {
			cm.TagPeer(pi.ID, "mdns-discovered", 50)
		}
		if DebugNet {
			fmt.Printf("Ã¢Å“â€¦ mDNS discovered and connected to peer: %s\n", pi.ID.String())
		}
		m.node.rememberPeerDiversityAddr(pi.ID.String(), pi.Addrs, true)
		m.node.recordDialSuccess(pi.ID.String())
	} else {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "peer id mismatch") ||
			strings.Contains(errLower, "dial to self attempted") {
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
	serviceTag := fmt.Sprintf("msc-mdns-%s", ChainID)
	service := mdns.NewMdnsService(n.Host, serviceTag, &mdnsNotifee{node: n})
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
		if maddr, err := ma.NewMultiaddr(stripP2PComponent(n.SelfAddr)); err == nil {
			if portStr, err := maddr.ValueForProtocol(ma.P_TCP); err == nil {
				if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
					listenPort = port
				}
			}
		}
	}
	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)
	listenIP := ""
	if n.SelfAddr != "" {
		selfAddr := stripP2PComponent(n.SelfAddr)
		if maddr, err := ma.NewMultiaddr(selfAddr); err == nil {
			if ip, err := maddr.ValueForProtocol(ma.P_IP4); err == nil && ip != "" {
				listenIP = ip
				listenAddr = fmt.Sprintf("/ip4/%s/tcp/%d", ip, listenPort)
			} else {
				listenAddr = selfAddr
			}
		}
	}
	sourceMultiAddr, _ := ma.NewMultiaddr(listenAddr)
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
		high = 50
	}
	low := high / 2
	if low < 1 {
		low = 1
	}
	if cm, err := connmgr.NewConnManager(low, high, connmgr.WithGracePeriod(time.Minute)); err == nil {
		opts = append(opts, libp2p.ConnectionManager(cm))
	}
	externalAddr := ""
	if ConfigP2PExternalAddr != "" {
		externalAddr = stripP2PComponent(ConfigP2PExternalAddr)
	}
	if listenIP != "" || externalAddr != "" {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			filtered := make([]ma.Multiaddr, 0, len(addrs))
			if listenIP != "" {
				for _, addr := range addrs {
					if ip, err := addr.ValueForProtocol(ma.P_IP4); err == nil && ip == listenIP {
						filtered = append(filtered, addr)
					}
				}
			} else {
				filtered = append(filtered, addrs...)
			}
			if externalAddr != "" {
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
	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}
	if keyPath := p2pIdentityKeyPath(n.DataDir, n.ID); strings.TrimSpace(keyPath) != "" {
		log.Printf("[P2P-ID] identity key=%s peer_id=%s", keyPath, h.ID().String())
	}
	n.streamManager = NewStreamManager(h)
	// =====================================================
	// Ã°Å¸â€œÂ¡ CREATE GOSSIPSUB
	// =====================================================
	params := pubsub.DefaultGossipSubParams()
	params.HeartbeatInterval = 5 * time.Second
	validateWorkers := runtime.NumCPU()
	if validateWorkers < 2 {
		validateWorkers = 2
	}
	if MaxValidateWorkers > 0 && validateWorkers > MaxValidateWorkers {
		validateWorkers = MaxValidateWorkers
	}
	validateQueue := MaxValidateQueue
	if validateQueue <= 0 {
		validateQueue = 128
	}
	validateThrottle := validateWorkers * 4
	if validateThrottle < validateQueue {
		validateThrottle = validateQueue
	}
	peerOutboundQueue := MaxPeerOutboundQueue
	if peerOutboundQueue <= 0 {
		peerOutboundQueue = 512
	}
	ps, err := pubsub.NewGossipSub(
		ctx,
		h,
		pubsub.WithMessageSignaturePolicy(pubsub.StrictSign),
		pubsub.WithPeerExchange(true),
		pubsub.WithMaxMessageSize(10<<20),
		pubsub.WithGossipSubParams(params),
		pubsub.WithValidateWorkers(validateWorkers),
		pubsub.WithValidateQueueSize(validateQueue),
		pubsub.WithValidateThrottle(validateThrottle),
		pubsub.WithPeerOutboundQueueSize(peerOutboundQueue),
	)
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
		bootstrapPeers := make([]peer.AddrInfo, 0)
		addBootstrap := func(raw string) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return
			}
			maddr, err := ma.NewMultiaddr(raw)
			if err != nil {
				return
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return
			}
			bootstrapPeers = append(bootstrapPeers, *info)
		}
		_, seeds := n.configPeerListsSnapshot()
		persistent := n.persistentPeersSnapshot()
		for _, seed := range seeds {
			addBootstrap(seed)
		}
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
			bootstrapCtx, cancel := context.WithTimeout(
				ctx,
				15*time.Second,
			)
			defer cancel()
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
func validatorMeshMode() string {
	return "active_only"
}

func validatorMeshReconcileInterval() time.Duration {
	return 8 * time.Second
}

func (n *Node) validatorMeshTargets() []string {
	if n == nil {
		return nil
	}
	nextHeight := uint64(1)
	if n.Blockchain != nil {
		nextHeight = n.Blockchain.Height() + 1
	}
	activeSet := canonicalValidatorIDs(n.frozenValidatorsForHeight(nextHeight))
	if len(activeSet) == 0 {
		// Fallback only when deterministic frozen set is not available yet.
		activeSet = canonicalValidatorIDs(n.GetConsensusValidators(int(nextHeight)))
	}
	targetIDs := make(map[string]struct{})
	for _, vid := range activeSet {
		vid = strings.TrimSpace(vid)
		if vid == "" {
			continue
		}
		targetIDs[strings.ToUpper(vid)] = struct{}{}
	}
	addrs := make([]string, 0, len(targetIDs))
	for vid := range targetIDs {
		if strings.EqualFold(vid, n.ID) {
			continue
		}
		if addr := n.GetValidatorAddr(vid); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	return sanitizePeerListWithPreferred(addrs, addrs)
}
func (n *Node) trustedPeerMultiaddrs() []string {
	if n == nil {
		return nil
	}
	if n.Config == nil {
		return nil
	}
	persistent, seeds := n.configPeerListsSnapshot()
	// Order matters: later sources override earlier ones for same endpoint.
	// Keep runtime order so fresh discoveries can win over stale static entries.
	trusted := make([]string, 0, len(persistent)+len(seeds)+8)
	trusted = append(trusted, seeds...)
	trusted = append(trusted, persistent...)
	ValidatorAddrBook.mu.RLock()
	for _, addr := range ValidatorAddrBook.m {
		if strings.TrimSpace(addr) != "" {
			trusted = append(trusted, strings.TrimSpace(addr))
		}
	}
	ValidatorAddrBook.mu.RUnlock()
	if n.Host != nil {
		for _, pid := range n.Host.Network().Peers() {
			for _, addr := range n.Host.Peerstore().Addrs(pid) {
				trusted = append(trusted, fmt.Sprintf("%s/p2p/%s", addr.String(), pid.String()))
			}
		}
	}
	return sanitizePeerListWithPreferred(trusted, trusted)
}
func (n *Node) maintainValidatorMesh(ctx context.Context) {
	if n == nil || n.Host == nil {
		return
	}
	ticker := time.NewTicker(validatorMeshReconcileInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			if n.Role != "validator" {
				continue
			}
			targets := n.validatorMeshTargets()
			if len(targets) == 0 {
				continue
			}
			n.connectToPeers(ctx, targets)
		}
	}
}
func (n *Node) HandlePeerHello(peerAddr, validatorID, advertisedAddr string) {
	// Hard validation
	if validatorID == "" || advertisedAddr == "" {
		return
	}
	advertisedAddr = strings.TrimSpace(advertisedAddr)
	peerAddr = strings.TrimSpace(peerAddr)
	// If /p2p/ is missing in advertised address, attach remote stream peer ID.
	if _, _, hasPID := splitPeerAddress(advertisedAddr); !hasPID {
		if _, remotePeerID, ok := splitPeerAddress(peerAddr); ok {
			if fixedAddr, ok := peerAddrWithPeerID(advertisedAddr, remotePeerID); ok {
				advertisedAddr = fixedAddr
			}
		}
	}
	if !n.reserveValidatorPeerIdentity(validatorIdentityPeerID(peerAddr, advertisedAddr), validatorID, advertisedAddr) {
		return
	}
	updated := false
	ValidatorAddrBook.mu.Lock()
	if old, exists := ValidatorAddrBook.m[validatorID]; exists {
		updated = old != advertisedAddr
	} else {
		updated = true
	}
	ValidatorAddrBook.m[validatorID] = advertisedAddr
	ValidatorAddrBook.mu.Unlock()
	n.upsertDiscoveredPeerAddress(advertisedAddr)
	if updated && DebugConsensus {
		fmt.Printf("Validator registered | id=%s addr=%s\n", validatorID, advertisedAddr)
	}
	// Ensure late-joining peers see our heartbeat quickly.
	if updated && n.canAdvertiseValidatorPresence() {
		n.requestHeartbeatBroadcast(true)
	}
}
func peerListsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// upsertDiscoveredPeerAddress persists private peer addresses learned during
// peer hello so stale /p2p/ identities are corrected across restarts.
func (n *Node) upsertDiscoveredPeerAddress(addr string) {
	if n == nil {
		return
	}
	addr = strings.TrimSpace(addr)
	if addr == "" || !isPeerAddrPrivate(addr) {
		return
	}
	selfBase := stripP2PComponent(n.SelfAddr)
	addrBase := stripP2PComponent(addr)
	if n.SelfAddr != "" && (addr == n.SelfAddr || addrBase == selfBase) {
		return
	}
	if n.Host != nil && strings.Contains(addr, n.Host.ID().String()) {
		return
	}
	currentPersistent := n.persistentPeersSnapshot()
	merged := mergePeerLists(currentPersistent, []string{addr})
	preferred := append(n.trustedPeerMultiaddrs(), addr)
	sanitized := sanitizePeerListWithPreferred(merged, preferred)
	if peerListsEqual(sanitized, currentPersistent) {
		return
	}
	n.setPersistentPeers(sanitized)
	if err := savePersistentPeers(n.DataDir, n.ID, sanitized); err != nil && DebugNet {
		fmt.Printf("Failed to persist discovered peer %s: %v\n", stripP2PComponent(addr), err)
	}
}
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
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	return false
}
func isPrivateMultiaddr(maddr ma.Multiaddr) bool {
	if v, err := maddr.ValueForProtocol(ma.P_IP4); err == nil && v != "" {
		return isPrivateIP(net.ParseIP(v))
	}
	if v, err := maddr.ValueForProtocol(ma.P_IP6); err == nil && v != "" {
		return isPrivateIP(net.ParseIP(v))
	}
	return false
}
func filterPrivateAddrs(addrs []ma.Multiaddr) []ma.Multiaddr {
	if len(addrs) == 0 {
		return addrs
	}
	out := make([]ma.Multiaddr, 0, len(addrs))
	for _, addr := range addrs {
		if isPrivateMultiaddr(addr) {
			out = append(out, addr)
		}
	}
	return out
}
func isPeerAddrPrivate(raw string) bool {
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "/") {
		maddr, err := ma.NewMultiaddr(raw)
		if err != nil {
			return false
		}
		return isPrivateMultiaddr(maddr)
	}
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return isPrivateIP(ip)
}
func (n *Node) countConnectionDirections() (int, int, int) {
	if n == nil || n.Host == nil {
		return 0, 0, 0
	}
	conns := n.Host.Network().Conns()
	inbound := 0
	outbound := 0
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
func (n *Node) canDialPeer() bool {
	if n == nil || n.Host == nil {
		return false
	}
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
func (n *Node) allowConnection(conn network.Conn) bool {
	if n == nil || n.Host == nil || conn == nil {
		return true
	}
	if !n.allowPeerConnectionFlood(conn) {
		return false
	}
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
func peerSubnetKeyFromMultiaddr(maddr ma.Multiaddr) string {
	if maddr == nil {
		return ""
	}
	if ip4, err := maddr.ValueForProtocol(ma.P_IP4); err == nil && ip4 != "" {
		ip := net.ParseIP(ip4)
		if ip == nil {
			return ""
		}
		if ip.IsLoopback() {
			return ""
		}
		v4 := ip.To4()
		if v4 == nil {
			return ""
		}
		prefix := PeerDiversityIPv4Prefix
		if prefix <= 0 || prefix > 32 {
			prefix = 24
		}
		masked := v4.Mask(net.CIDRMask(prefix, 32))
		if masked == nil {
			return ""
		}
		return fmt.Sprintf("%s/%d", masked.String(), prefix)
	}
	if ip6, err := maddr.ValueForProtocol(ma.P_IP6); err == nil && ip6 != "" {
		ip := net.ParseIP(ip6)
		if ip == nil {
			return ""
		}
		if ip.IsLoopback() {
			return ""
		}
		v6 := ip.To16()
		if v6 == nil {
			return ""
		}
		prefix := PeerDiversityIPv6Prefix
		if prefix <= 0 || prefix > 128 {
			prefix = 64
		}
		masked := v6.Mask(net.CIDRMask(prefix, 128))
		if masked == nil {
			return ""
		}
		return fmt.Sprintf("%s/%d", masked.String(), prefix)
	}
	return ""
}
func (n *Node) connectedPeersInSubnet(subnet string, excludePeer string) int {
	if n == nil || subnet == "" {
		return 0
	}
	seen := make(map[string]struct{})
	if n.Host != nil {
		for _, c := range n.Host.Network().Conns() {
			if c == nil {
				continue
			}
			pid := c.RemotePeer().String()
			if pid == "" || pid == excludePeer {
				continue
			}
			key := peerSubnetKeyFromMultiaddr(c.RemoteMultiaddr())
			if key != subnet {
				continue
			}
			seen[pid] = struct{}{}
		}
	}
	n.peerStateMu.Lock()
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

func peerDiversityASNFromEnv(peerID string) string {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return ""
	}
	raw := strings.TrimSpace(os.Getenv("MSC_PEER_ASN_MAP"))
	if raw == "" {
		return ""
	}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
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

func (n *Node) peerDiversityASNForPeer(peerID string) string {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return ""
	}
	n.peerStateMu.Lock()
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

func (n *Node) connectedPeersInASN(asn string, excludePeer string) int {
	asn = normalizePeerDiversityASN(asn)
	if n == nil || asn == "" {
		return 0
	}
	seen := make(map[string]struct{})
	if n.Host != nil {
		for _, c := range n.Host.Network().Conns() {
			if c == nil {
				continue
			}
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

func (n *Node) rememberPeerDiversityAddr(peerID string, addrs []ma.Multiaddr, outbound bool) {
	peerID = strings.TrimSpace(peerID)
	if n == nil || peerID == "" {
		return
	}
	subnet := ""
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
	if asn := peerDiversityASNFromEnv(peerID); asn != "" {
		n.setPeerDiversityASN(peerID, asn)
	}
}

func (n *Node) rememberPeerDiversityConn(conn network.Conn) {
	if n == nil || conn == nil {
		return
	}
	n.rememberPeerDiversityAddr(conn.RemotePeer().String(), []ma.Multiaddr{conn.RemoteMultiaddr()}, conn.Stat().Direction == network.DirOutbound)
}

func (n *Node) outboundPeersInSubnet(subnet string) int {
	if n == nil || subnet == "" {
		return 0
	}
	seen := make(map[string]struct{})
	if n.Host != nil {
		for _, c := range n.Host.Network().Conns() {
			if c == nil || c.Stat().Direction != network.DirOutbound {
				continue
			}
			pid := c.RemotePeer().String()
			if pid == "" || peerSubnetKeyFromMultiaddr(c.RemoteMultiaddr()) != subnet {
				continue
			}
			seen[pid] = struct{}{}
		}
	}
	n.peerStateMu.Lock()
	for pid, key := range n.peerSubnet {
		if pid == "" || key != subnet || !n.connectedPeers[pid] || !n.peerOutbound[pid] {
			continue
		}
		seen[pid] = struct{}{}
	}
	n.peerStateMu.Unlock()
	return len(seen)
}

func (n *Node) outboundPeersInASN(asn string) int {
	asn = normalizePeerDiversityASN(asn)
	if n == nil || asn == "" {
		return 0
	}
	seen := make(map[string]struct{})
	if n.Host != nil {
		for _, c := range n.Host.Network().Conns() {
			if c == nil || c.Stat().Direction != network.DirOutbound {
				continue
			}
			pid := c.RemotePeer().String()
			if pid == "" || n.peerDiversityASNForPeer(pid) != asn {
				continue
			}
			seen[pid] = struct{}{}
		}
	}
	n.peerStateMu.Lock()
	for pid, knownASN := range n.peerASN {
		if pid == "" || normalizePeerDiversityASN(knownASN) != asn || !n.connectedPeers[pid] || !n.peerOutbound[pid] {
			continue
		}
		seen[pid] = struct{}{}
	}
	n.peerStateMu.Unlock()
	return len(seen)
}

func (n *Node) allowPeerDiversityASN(peerID string, outbound bool) bool {
	if n == nil || !PeerDiversityEnabled {
		return true
	}
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

func (n *Node) allowPeerDiversityConn(conn network.Conn) bool {
	if n == nil || conn == nil || !PeerDiversityEnabled {
		return true
	}
	peerID := conn.RemotePeer().String()
	subnet := peerSubnetKeyFromMultiaddr(conn.RemoteMultiaddr())
	if subnet != "" && PeerDiversityMaxPerSubnet > 0 {
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

func (n *Node) allowPeerDiversityDial(maddr ma.Multiaddr) bool {
	if n == nil {
		return true
	}
	if !PeerDiversityEnabled {
		return true
	}
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

func (n *Node) allowPeerDiversityOutboundPeer(info *peer.AddrInfo) bool {
	if n == nil || info == nil || !PeerDiversityEnabled {
		return true
	}
	peerID := info.ID.String()
	if !n.allowPeerDiversityASN(peerID, true) {
		return false
	}
	for _, addr := range info.Addrs {
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
	SubnetBuckets         int
	ASNBuckets            int
	OutboundSubnetBuckets int
	OutboundASNBuckets    int
	RejectTotal           uint64
	OutboundRejectTotal   uint64
}

func (n *Node) peerDiversitySnapshot() peerDiversitySnapshot {
	if n == nil {
		return peerDiversitySnapshot{}
	}
	subnets := make(map[string]struct{})
	asns := make(map[string]struct{})
	outSubnets := make(map[string]struct{})
	outASNs := make(map[string]struct{})
	n.peerStateMu.Lock()
	for pid, connected := range n.connectedPeers {
		if !connected {
			continue
		}
		if subnet := strings.TrimSpace(n.peerSubnet[pid]); subnet != "" {
			subnets[subnet] = struct{}{}
			if n.peerOutbound[pid] {
				outSubnets[subnet] = struct{}{}
			}
		}
		if asn := normalizePeerDiversityASN(n.peerASN[pid]); asn != "" {
			asns[asn] = struct{}{}
			if n.peerOutbound[pid] {
				outASNs[asn] = struct{}{}
			}
		}
	}
	n.peerStateMu.Unlock()
	if n.Host != nil {
		for _, c := range n.Host.Network().Conns() {
			if c == nil {
				continue
			}
			if subnet := peerSubnetKeyFromMultiaddr(c.RemoteMultiaddr()); subnet != "" {
				subnets[subnet] = struct{}{}
				if c.Stat().Direction == network.DirOutbound {
					outSubnets[subnet] = struct{}{}
				}
			}
			if asn := n.peerDiversityASNForPeer(c.RemotePeer().String()); asn != "" {
				asns[asn] = struct{}{}
				if c.Stat().Direction == network.DirOutbound {
					outASNs[asn] = struct{}{}
				}
			}
		}
	}
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

const maxDialFanout = 16

func (n *Node) connectToPeersAsync(peerMultiaddrs []string, timeout time.Duration) {
	if n == nil || len(peerMultiaddrs) == 0 {
		return
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
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
		ctx, cancel := context.WithTimeout(n.RootContext(), timeout)
		defer cancel()
		n.connectToPeers(ctx, targets)
	}()
}

func (n *Node) dialSemaphore() chan struct{} {
	if n == nil {
		return nil
	}
	n.dialSlotsMu.Do(func() {
		n.dialSlots = make(chan struct{}, maxDialFanout)
	})
	return n.dialSlots
}

func (n *Node) releaseDialSlot() {
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
	uniquePeers := make(map[string]struct{})
	for _, p := range peerMultiaddrs {
		if p == "" {
			continue
		}
		uniquePeers[p] = struct{}{}
	}
	if DebugNet && len(uniquePeers) > 0 && n.shouldLogNetworkProbe(fmt.Sprintf("connect_targets:%d", len(uniquePeers)), 20*time.Second) {
		fmt.Printf("Attempting to connect to %d peers\n", len(uniquePeers))
	}
	sem := n.dialSemaphore()
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
				if r := recover(); r != nil {
					fmt.Printf("[RECOVERED] connect_to_peers_dial panic: %v\n%s\n", r, debug.Stack())
				}
			}()
			delay := time.Duration(rand.Intn(2000)) * time.Millisecond
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
			selfBase := stripP2PComponent(n.SelfAddr)
			if selfBase != "" && stripP2PComponent(addr) == selfBase {
				if _, stalePID, has := splitPeerAddress(addr); has && (n.Host == nil || stalePID != n.Host.ID().String()) {
					n.forgetPeer(stalePID, "self_dial")
				}
				if DebugNet {
					fmt.Printf("Skipping self dial target: %s\n", addr)
				}
				return
			}
			if !strings.HasPrefix(addr, "/") {
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
			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				if DebugNet {
					fmt.Printf("Invalid multiaddr %s: %v\n", addr, err)
				}
				return
			}
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
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Ã°Å¸Å’Â Network CLI ready (type 'network help' for commands)")
	fmt.Println("Type 'back' to return to main CLI")
	for {
		if n != nil && n.isShuttingDown() {
			return
		}
		fmt.Printf("[network:%s] > ", n.ID)
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
			peers := n.Host.Network().Peers()
			fmt.Printf("=== Connected Peers (%d) ===\n", len(peers))
			if len(peers) == 0 {
				fmt.Println("No connected peers")
				continue
			}
			for _, pid := range peers {
				conns := n.Host.Network().ConnsToPeer(pid)
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
			for _, addr := range n.Host.Addrs() {
				fmt.Printf("  %s\n", addr)
			}
			// Network stats
			peers := n.Host.Network().Peers()
			fmt.Printf("Connected peers: %d\n", len(peers))
			// Connection count
			totalConns := 0
			for _, pid := range peers {
				totalConns += len(n.Host.Network().ConnsToPeer(pid))
			}
			fmt.Printf("Total connections: %d\n", totalConns)
		case "network topics", "topics":
			fmt.Println("=== PubSub Topics ===")
			topics := []string{TopicBlock, TopicTx, TopicConsensus, TopicValidator, TopicSnapshotMeta, TopicSnapshotChunk, TopicSnapshotProof}
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
			states := make(map[string]int)
			for _, pid := range peers {
				state := n.Host.Network().Connectedness(pid)
				states[state.String()]++
			}
			for state, count := range states {
				fmt.Printf("%s: %d\n", state, count)
			}
			// Protocol usage
			fmt.Println("\nProtocol usage:")
			for _, pid := range peers {
				protos, _ := n.Host.Peerstore().GetProtocols(pid)
				for _, proto := range protos {
					fmt.Printf("  %s: %s\n", pid.ShortString(), proto)
				}
			}
		case "network maps", "maps":
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
			runtime := n.runtimeStatusSnapshot()
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
			snapshot, meta, source, err := n.createCommittedTipSnapshot("cli_export", false)
			if err != nil || snapshot == nil {
				fmt.Printf("snapshot export failed: %v\n", err)
				continue
			}
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
			result, err := n.downloadTrustedSnapshotAndStore(0, 0, false, true, false)
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
			vals := n.GetActiveValidators()
			if len(vals) == 0 {
				fmt.Println("no validators")
				continue
			}
			for _, v := range vals {
				fmt.Println(v)
			}
		case "validatorset", "validator set", "validator-set", "network validatorset", "network validator set", "network validator-set":
			current := n.GetConsensusValidators(int(n.Blockchain.Height() + 1))
			n.validatorSetMu.RLock()
			pendingAdds := make([]string, 0, len(n.pendingValidators))
			for id, act := range n.pendingValidators {
				pendingAdds = append(pendingAdds, fmt.Sprintf("%s@%d", id, act))
			}
			pendingRemoves := make([]string, 0, len(n.pendingValidatorRemovals))
			for id, act := range n.pendingValidatorRemovals {
				pendingRemoves = append(pendingRemoves, fmt.Sprintf("%s@%d", id, act))
			}
			n.validatorSetMu.RUnlock()
			sort.Strings(current)
			sort.Strings(pendingAdds)
			sort.Strings(pendingRemoves)
			fmt.Printf("validator_set_hash=%s\n", ValidatorSetHash(current)[:8])
			fmt.Printf("validators (%d):\n", len(current))
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
			pendingAdds := make([]string, 0, len(n.pendingValidators))
			for id, act := range n.pendingValidators {
				pendingAdds = append(pendingAdds, fmt.Sprintf("%s@%d", id, act))
			}
			pendingRemoves := make([]string, 0, len(n.pendingValidatorRemovals))
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
			ids := make([]string, 0, len(n.candidates))
			for id := range n.candidates {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			fmt.Printf("candidates (%d):\n", len(ids))
			for _, id := range ids {
				cand := n.candidates[id]
				if cand == nil {
					continue
				}
				observed := cand.ObservedEpochs
				matched := cand.MatchedEpochs
				dcs := 0.0
				uptime := 0.0
				diversityAvg := 0.0
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
			addrInput, _ := reader.ReadString('\n')
			addrInput = strings.TrimSpace(addrInput)
			if addrInput == "" {
				fmt.Println("Ã¢ÂÅ’ No address provided")
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			maddr, err := ma.NewMultiaddr(addrInput)
			if err != nil {
				fmt.Printf("Ã¢ÂÅ’ Invalid multiaddr: %v\n", err)
				continue
			}
			addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Printf("Ã¢ÂÅ’ Failed to parse peer info: %v\n", err)
				continue
			}
			fmt.Printf("Attempting to connect to %s...\n", addrInfo.ID)
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
	if err := n.Host.Connect(connectCtx, *addrInfo); err != nil {
		n.setPeerConnected(addrInfo.ID.String(), false)
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
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	// Tag peer for priority
	if cm := n.Host.ConnManager(); cm != nil {
		cm.TagPeer(addrInfo.ID, "persistent", 100)
	}
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
func (n *Node) handlePeerInfoStream(s network.Stream) {
	defer s.Close()
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)
	// Send our info
	info := n.outboundPeerHello()
	_ = enc.Encode(info)
	// Receive peer info
	var raw json.RawMessage
	if err := dec.Decode(&raw); err == nil {
		peerID := s.Conn().RemotePeer().String()
		peerInfo, derr := decodePeerHelloPayload(raw)
		if derr != nil {
			n.recordDialFailure(peerID)
			return
		}
		if n.validatePeerHello(peerID, peerInfo) {
			n.applyPeerInfo(peerID, peerInfo)
			n.recordDialSuccess(peerID)
		}
	} else {
		n.recordDialFailure(s.Conn().RemotePeer().String())
	}
}
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
	if !checkRateLimit(peerAddr, msg.Type) {
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
		var hello PeerHello
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
		var block Block
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
		var evidence CensorshipEvidence
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
		var tx Transaction
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
		var block Block
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
		var res ExecutionResultMsg
		if err := json.Unmarshal(msg.Data, &res); err != nil {
			if DebugNet {
				fmt.Printf("Ã¢ÂÅ’ Failed to unmarshal execution result from %s: %v\n", peerAddr, err)
			}
			return
		}
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
		var cm CommitMsg
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
		var ack BlockAck
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
		var req BlockRequest
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
		maxInt := uint64(^uint(0) >> 1)
		if req.From > maxInt || req.To > maxInt {
			if DebugNet {
				fmt.Printf("block request overflow from %s: from=%d to=%d\n", peerAddr, req.From, req.To)
			}
			return
		}
		maxBlocks := syncBlockRequestMaxBlocks(0)
		if maxBlocks == 0 {
			maxBlocks = 512
		}
		if req.To-req.From > maxBlocks {
			n.recordPeerRateLimitDrop(peerAddr, "block_request_range")
			req.To = req.From + maxBlocks
		}
		go n.sendBlocksToPeer(peerAddr, int(req.From), int(req.To))
	case MsgBlocksBatch:
		var resp BlockResponse
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
			first := resp.Blocks[0].ID
			last := resp.Blocks[len(resp.Blocks)-1].ID
			fmt.Printf("applied block batch from %s: %d-%d (%d blocks)\n",
				peerAddr, first, last, len(resp.Blocks))
		}
	// =================================================
	// Ã°Å¸â€â€ž PEERS LIST EXCHANGE
	// =================================================
	case MsgPeers:
		var peers []string
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
func verifyCensorshipEvidenceObserver(ev CensorshipEvidence) bool {
	observer := normalizeValidatorID(ev.Observer)
	if observer == "" {
		return false
	}
	if len(ev.ObserverSig) != ed25519.SignatureSize {
		return false
	}
	validatorPubKeysMu.RLock()
	pubKey, ok := ValidatorPubKeys[observer]
	validatorPubKeysMu.RUnlock()
	if !ok || len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pubKey, censorshipEvidenceSignBytes(ev), ev.ObserverSig)
}
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
	key := EvidenceKey{
		Leader: ev.Leader,
		Height: ev.Height,
	}
	list := CensorshipEvidencePool[key]
	if len(list) >= MaxEvidencePerHeight {
		return false
	}
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
		fullAddr := fmt.Sprintf("%s/p2p/%s", addr, n.Host.ID())
		// Return a loopback address if available
		if strings.Contains(addr.String(), "127.0.0.1") {
			return fullAddr
		}
	}
	// Otherwise return any address
	return fmt.Sprintf("%s/p2p/%s", n.Host.Addrs()[0], n.Host.ID())
}

// Helper function for public IP detection
func GetPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(ip), nil
}

func snapshotChunkHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (n *Node) latestSnapshotMetaForSyncRequest() *StateSnapshot {
	if n == nil {
		return nil
	}
	if snapshot := n.publishedValidatorSnapshotForSyncRequest(0); snapshot != nil {
		return snapshot
	}
	if snapshot, err := n.verifiedStoredSnapshotAtOrBelow(0); err == nil && snapshot != nil {
		return snapshot
	}
	tip := n.Blockchain.Height()
	if tip == 0 {
		return nil
	}
	if snapshot, _, _, ok := n.ResolveCommittedStateSnapshot(tip); ok && snapshot != nil && n.snapshotMatchesLocalAnchor(snapshot) {
		return snapshot
	}
	block, ok := n.Blockchain.GetBlock(tip)
	if !ok {
		return nil
	}
	if err := n.CreateSnapshot(tip, block.BlockHash); err != nil {
		return nil
	}
	snapshot, err := n.GetSnapshot(tip)
	if err != nil || snapshot == nil || !n.snapshotMatchesLocalAnchor(snapshot) {
		_ = n.deleteStoredSnapshotHeight(tip)
		_ = n.refreshLatestSnapshotPointer()
		return nil
	}
	return snapshot
}

func (n *Node) snapshotForSyncRequest(targetHeight uint64) *StateSnapshot {
	if n == nil {
		return nil
	}
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
	if snapshot := n.publishedValidatorSnapshotForSyncRequest(targetHeight); snapshot != nil {
		return snapshot
	}
	if snapshot, err := n.verifiedStoredSnapshotAtOrBelow(targetHeight); err == nil && snapshot != nil {
		return snapshot
	}
	if tip == 0 || targetHeight == 0 || targetHeight != tip {
		return nil
	}
	if snapshot, _, _, ok := n.ResolveCommittedStateSnapshot(tip); ok && snapshot != nil && n.snapshotMatchesLocalAnchor(snapshot) {
		return snapshot
	}
	block, ok := n.Blockchain.GetBlock(tip)
	if !ok {
		return nil
	}
	if err := n.CreateSnapshot(tip, block.BlockHash); err != nil {
		return nil
	}
	snapshot, err := n.GetSnapshot(tip)
	if err != nil || snapshot == nil || !n.snapshotMatchesLocalAnchor(snapshot) {
		_ = n.deleteStoredSnapshotHeight(tip)
		_ = n.refreshLatestSnapshotPointer()
		return nil
	}
	return snapshot
}

func (n *Node) handleBlockStream(s network.Stream) {
	defer s.Close()
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
	enc := json.NewEncoder(s)
	var req BlockRequest
	if err := dec.Decode(&req); err != nil {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=decode err=%v\n",
			ShortID(s.Conn().RemotePeer().String()), err)
		return
	}
	origFrom := req.From
	origTo := req.To
	fmt.Printf("[SYNC-SERVE-REQUEST] peer=%s requested=%d-%d snapshot=%t snapshot_height=%d\n",
		ShortID(s.Conn().RemotePeer().String()), req.From, req.To, req.WantSnapshot, req.SnapshotHeight)
	if req.From == 0 || req.To < req.From {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=validate requested=%d-%d reason=invalid_range\n",
			ShortID(s.Conn().RemotePeer().String()), req.From, req.To)
		return
	}
	peerID := s.Conn().RemotePeer().String()
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
	fetchedCount := 0
	currentTip := n.Blockchain.Height()
	finalizedHeight := n.getFinalizedHeight()
	if finalizedHeight == 0 {
		finalizedHeight = currentTip
	}
	tipGapOnly := false
	historicalGapOnly := false
	type finalizedDriftAggregate struct {
		count    int
		from     uint64
		to       uint64
		expected string
		got      string
	}
	finalizedDrifts := make(map[string]finalizedDriftAggregate)
	if blockRangeEmpty {
		// The peer already acknowledged this whole block range. Snapshot payloads
		// may still be useful, but do not synthesize a new block range.
	} else if start > currentTip {
		// Peer is asking for blocks beyond our current tip; return snapshot/empty response quietly.
		tipGapOnly = true
	} else {
		for h := start; h <= req.To; h++ {
			// Avoid noisy "not found" logs for blocks we simply don't have yet.
			if h > currentTip {
				tipGapOnly = true
				break
			}
			if b, ok := n.LoadBlock(int(h)); ok {
				// For non-finalized heights, keep strict validator-set consistency.
				// For finalized history, serve canonical blocks even if expected-set
				// view drifted due repair/snapshot recomputation.
				if b.ValidatorSetHash != "" {
					if expected := n.expectedValidatorSetHash(b.ID); expected != "" && expected != b.ValidatorSetHash {
						if b.ID > finalizedHeight {
							historicalGapOnly = true
							if DebugSync || DebugConsensus {
								fmt.Printf("Refusing non-finalized inconsistent block %d for %s (expected_set=%s got=%s)\n",
									b.ID, s.Conn().RemotePeer(), ShortHash(expected), ShortHash(b.ValidatorSetHash))
							}
							break
						}
						tupleKey := expected + "|" + b.ValidatorSetHash
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
					fromHeight := blocks[0].ID
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
		keys := make([]string, 0, len(finalizedDrifts))
		for key := range finalizedDrifts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			agg := finalizedDrifts[key]
			state := n.recordFinalizedDrift(peerID, agg.from, agg.to, agg.expected, agg.got, agg.count)
			if (DebugSync || DebugConsensus) && n.shouldLogFinalizedDrift(peerID, agg.from, agg.to) {
				fmt.Printf("Serving finalized blocks %d-%d to %s despite expected-set drift (expected_set=%s got=%s count=%d)\n",
					agg.from, agg.to, s.Conn().RemotePeer(), ShortHash(agg.expected), ShortHash(agg.got), state.Count)
			}
			n.applyFinalizedDriftPolicy(peerID, state)
		}
	}
	var execPoolSnap *ExecPoolSnapshot
	if ResultGossipOnly && req.WantSnapshot {
		epoch := n.currentEpoch()
		execPoolSnap = buildExecPoolSnapshot(epoch, n.currentProposalVoteKey(epoch))
	}
	resp := BlockResponse{Blocks: blocks, Snapshot: snapshot, ExecPool: execPoolSnap}
	if err := enc.Encode(resp); err != nil {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=encode requested=%d-%d count=%d err=%v\n",
			ShortID(peerID), req.From, req.To, len(blocks), err)
		return
	}
	if err := s.CloseWrite(); err != nil {
		fmt.Printf("[SYNC-SERVE-ERROR] peer=%s stage=close_write requested=%d-%d count=%d err=%v\n",
			ShortID(peerID), req.From, req.To, len(blocks), err)
	}
	servedFrom := uint64(0)
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
			fromHeight := blocks[0].ID
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
func (n *Node) handleSnapshotMetaStream(s network.Stream) {
	defer s.Close()
	timeout := syncPeerRequestTimeout()
	_ = s.SetDeadline(time.Now().Add(timeout))
	dec := json.NewDecoder(s)
	enc := json.NewEncoder(s)

	var req SnapshotMetaRequest
	if err := dec.Decode(&req); err != nil {
		return
	}
	var snapshot *StateSnapshot
	if req.Height == 0 {
		snapshot = n.latestSnapshotMetaForSyncRequest()
	} else {
		snapshot = n.snapshotForSyncRequest(req.Height)
	}
	if snapshot == nil {
		_ = enc.Encode(SnapshotMetaResponse{Available: false})
		return
	}
	manifest, data, err := snapshotManifestFromSnapshot(snapshot)
	if err != nil {
		_ = enc.Encode(SnapshotMetaResponse{Available: false})
		return
	}
	chunkSize := syncSnapshotChunkSizeBytes()
	if chunkSize == 0 {
		chunkSize = 1024 * 1024
	}
	totalChunks := uint64((len(data) + int(chunkSize) - 1) / int(chunkSize))
	resp := SnapshotMetaResponse{
		Height:                snapshot.Height,
		SnapshotHash:          snapshot.SnapshotHash,
		StateRoot:             snapshot.StateRoot,
		StateMerkleRoot:       snapshot.StateMerkleRoot,
		ValidatorSetHash:      snapshot.ValidatorSetHash,
		ValidatorRegistryHash: snapshot.ValidatorRegistryHash,
		FinalizedHeight:       snapshot.FinalizedHeight,
		FinalizedHash:         strings.TrimSpace(snapshot.FinalizedHash),
		EpochAnchorHash:       strings.TrimSpace(snapshot.EpochAnchorHash),
		FinalityRoot:          strings.TrimSpace(snapshot.FinalityRoot),
		ChunkSize:             chunkSize,
		TotalChunks:           totalChunks,
		CheckpointProof:       snapshot.CheckpointProof,
		Available:             true,
	}
	if manifest != nil {
		resp.Manifest = manifest
		resp.ChunkHashes = append([]string{}, manifest.ChunkHashes...)
		resp.StateRoot = manifest.StateRoot
		resp.StateMerkleRoot = manifest.StateMerkleRoot
		resp.ValidatorSetHash = manifest.ValidatorSetHash
		resp.ValidatorRegistryHash = manifest.ValidatorRegistryHash
		resp.FinalizedHeight = manifest.FinalizedHeight
		resp.FinalizedHash = manifest.FinalizedHash
		resp.EpochAnchorHash = manifest.EpochAnchorHash
		resp.FinalityRoot = manifest.FinalityRoot
	}
	_ = enc.Encode(resp)
}

func (n *Node) handleSnapshotChunkStream(s network.Stream) {
	defer s.Close()
	timeout := syncPeerRequestTimeout()
	_ = s.SetDeadline(time.Now().Add(timeout))
	dec := json.NewDecoder(s)
	enc := json.NewEncoder(s)

	var req SnapshotChunkRequest
	if err := dec.Decode(&req); err != nil {
		return
	}
	snapshot := n.snapshotForSyncRequest(req.Height)
	if snapshot == nil {
		_ = enc.Encode(SnapshotChunkResponse{})
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		_ = enc.Encode(SnapshotChunkResponse{})
		return
	}
	chunkSize := syncSnapshotChunkSizeBytes()
	if chunkSize == 0 {
		chunkSize = 1024 * 1024
	}
	totalChunks := uint64((len(data) + int(chunkSize) - 1) / int(chunkSize))
	if req.Index >= totalChunks {
		_ = enc.Encode(SnapshotChunkResponse{})
		return
	}
	start := req.Index * chunkSize
	end := start + chunkSize
	if end > uint64(len(data)) {
		end = uint64(len(data))
	}
	chunk := data[start:end]
	resp := SnapshotChunkResponse{
		Height:       snapshot.Height,
		Index:        req.Index,
		ChunkHash:    snapshotChunkHash(chunk),
		SnapshotHash: snapshot.SnapshotHash,
		Data:         chunk,
	}
	_ = enc.Encode(resp)
}

func ed25519ToLibp2pKey(priv ed25519.PrivateKey) (libp2pcrypto.PrivKey, error) {
	return libp2pcrypto.UnmarshalEd25519PrivateKey(priv)
}
func (n *Node) sendToPeerLibp2p(ctx context.Context, peerID peer.ID, msg Message) error {
	s, err := n.openStream(ctx, peerID, "/msc/1.0.0")
	if err != nil {
		n.recordDialFailure(peerID.String())
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "protocols not supported") || strings.Contains(errMsg, "protocol not supported") {
			n.disconnectPeerID(peerID.String(), "consensus_protocol_mismatch")
		}
		return err
	}
	defer s.Close()
	data, _ := json.Marshal(msg)
	_, err = s.Write(append(data, '\n'))
	return err
}
func libp2pKeyFromEd25519(priv ed25519.PrivateKey) (lpcrypto.PrivKey, error) {
	return lpcrypto.UnmarshalEd25519PrivateKey(priv)
}

/*
legacy consensus removed

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
	writer := bufio.NewWriterSize(s, 65536)
	defer writer.Flush()
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
		var msg Message
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
				var hello PeerHello
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
		ackData, _ := json.Marshal(ack)
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
func (n *Node) joinGossipTopics() {
	ctx := n.RootContext()
	// Block topic
	blockTopic := TopicBlock
	blockSub, err := n.PubSub.Subscribe(blockTopic)
	if err == nil {
		go n.handleBlockGossip(blockSub)
	}
	// Transaction topic
	txTopic := TopicTx
	txSub, err := n.PubSub.Subscribe(txTopic)
	if err == nil {
		go n.handleTransactionGossip(txSub)
	}
	// Validator gossip topic
	valTopic := TopicValidator
	valSub, err := n.PubSub.Subscribe(valTopic)
	if err == nil {
		go n.handleValidatorGossip(valSub)
	}
	// Snapshot meta gossip topic
	snapshotMetaTopic := TopicSnapshotMeta
	snapshotMetaSub, err := n.PubSub.Subscribe(snapshotMetaTopic)
	if err == nil {
		go n.handleSnapshotMetaGossip(snapshotMetaSub)
	}
	// Snapshot chunk gossip topic
	snapshotChunkTopic := TopicSnapshotChunk
	snapshotChunkSub, err := n.PubSub.Subscribe(snapshotChunkTopic)
	if err == nil {
		go n.handleSnapshotChunkGossip(snapshotChunkSub)
	}
	// Snapshot proof gossip topic
	snapshotProofTopic := TopicSnapshotProof
	snapshotProofSub, err := n.PubSub.Subscribe(snapshotProofTopic)
	if err == nil {
		go n.handleSnapshotProofGossip(snapshotProofSub)
	}
	// Consensus gossip topic
	consensusTopic := TopicConsensus
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
func (n *Node) listenBlocks(ctx context.Context) {
	// =====================================================
	// Ã°Å¸â€œÂ¡ SUBSCRIPTION INITIALIZATION
	// =====================================================
	var sub *pubsub.Subscription
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
		blockCount  int
		lastLogTime = time.Now()
		seenBlockMu sync.RWMutex
		seenBlocks  = make(map[string]time.Time)
		lastHeight  uint64
		heightJumps int
	)
	cleanupDone := make(chan struct{})
	defer close(cleanupDone)
	// Cleanup goroutine for seen blocks cache
	go func() {
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
				now := time.Now()
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
			var parseErr error
			// Try Message wrapper format first
			var m Message
			if err := UnmarshalP2PMessage(msg.Data, &m); err == nil && m.Type != "" {
				if m.Type == MsgLeaderBlock {
					var leader Block
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
func (n *Node) listenTx(ctx context.Context) {
	// =====================================================
	// Ã°Å¸â€œÂ¡ SUBSCRIPTION INITIALIZATION
	// =====================================================
	var sub *pubsub.Subscription
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
		txCount             int
		lastLogTime         = time.Now()
		globalRate          = TxGossipGlobalRatePerSecond
		peerRate            = TxGossipPeerRatePerSecond
		peerBurst           = TxGossipPeerBurst
		peerLimiterTTL      = TxGossipPeerLimiterTTL
		seenTxCache         = make(map[string]time.Time)
		peerLimiters        = make(map[string]*rate.Limiter)
		peerLimiterLastSeen = make(map[string]time.Time)
		cacheCleanup        = time.NewTicker(5 * time.Minute)
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
			for txID, seenTime := range seenTxCache {
				if now.Sub(seenTime) > 10*time.Minute {
					delete(seenTxCache, txID)
				}
			}
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
			var tx Transaction
			var parseErr error
			// Try Message wrapper format first
			var m Message
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
			dedupeID := tx.ID
			if dedupeID == "" {
				dedupeID = ComputeTxID(tx)
			}
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
