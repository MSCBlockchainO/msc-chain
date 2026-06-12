package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
)

type testStreamOpener struct {
	openFn func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error)
}

func (o testStreamOpener) Open(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
	return o.openFn(ctx, pid, proto)
}

type testConn struct {
	remote peer.ID
}

func (c *testConn) As(any) bool                                { return false }
func (c *testConn) Close() error                               { return nil }
func (c *testConn) CloseWithError(network.ConnErrorCode) error { return nil }
func (c *testConn) ConnState() network.ConnectionState         { return network.ConnectionState{} }
func (c *testConn) GetStreams() []network.Stream               { return nil }
func (c *testConn) ID() string                                 { return "test-conn" }
func (c *testConn) IsClosed() bool                             { return false }
func (c *testConn) LocalMultiaddr() ma.Multiaddr               { return nil }
func (c *testConn) LocalPeer() peer.ID                         { return "" }
func (c *testConn) NewStream(context.Context) (network.Stream, error) {
	return nil, errors.New("unsupported")
}
func (c *testConn) RemoteMultiaddr() ma.Multiaddr { return nil }
func (c *testConn) RemotePeer() peer.ID           { return c.remote }
func (c *testConn) RemotePublicKey() ic.PubKey    { return nil }
func (c *testConn) Scope() network.ConnScope      { return nil }
func (c *testConn) Stat() network.ConnStats       { return network.ConnStats{} }

type testStream struct {
	conn     network.Conn
	readFn   func([]byte) (int, error)
	writeFn  func([]byte) (int, error)
	resetMu  sync.Mutex
	reset    bool
	closed   bool
	resetCh  chan struct{}
	writeBuf bytes.Buffer
	protocol protocol.ID
}

func newJSONResponseStream(pid peer.ID, resp BlockResponse) *testStream {
	payload, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	reader := bytes.NewReader(payload)
	s := &testStream{
		conn:    &testConn{remote: pid},
		resetCh: make(chan struct{}),
	}
	s.readFn = func(p []byte) (int, error) {
		return reader.Read(p)
	}
	s.writeFn = func(p []byte) (int, error) {
		return s.writeBuf.Write(p)
	}
	return s
}

func newBlockingWriteStream(pid peer.ID) *testStream {
	s := &testStream{
		conn:    &testConn{remote: pid},
		resetCh: make(chan struct{}),
	}
	s.readFn = func(p []byte) (int, error) {
		return 0, io.EOF
	}
	s.writeFn = func(p []byte) (int, error) {
		<-s.resetCh
		return 0, io.ErrClosedPipe
	}
	return s
}

func newBlockingReadStream(pid peer.ID) *testStream {
	s := &testStream{
		conn:    &testConn{remote: pid},
		resetCh: make(chan struct{}),
	}
	s.readFn = func(p []byte) (int, error) {
		<-s.resetCh
		return 0, io.EOF
	}
	s.writeFn = func(p []byte) (int, error) {
		return s.writeBuf.Write(p)
	}
	return s
}

func newFailingWriteStream(pid peer.ID) *testStream {
	s := &testStream{
		conn:    &testConn{remote: pid},
		resetCh: make(chan struct{}),
	}
	s.readFn = func(p []byte) (int, error) {
		return 0, io.EOF
	}
	s.writeFn = func(p []byte) (int, error) {
		return 0, io.ErrClosedPipe
	}
	return s
}

func newFailingReadStream(pid peer.ID) *testStream {
	s := &testStream{
		conn:    &testConn{remote: pid},
		resetCh: make(chan struct{}),
	}
	s.readFn = func(p []byte) (int, error) {
		return 0, io.ErrUnexpectedEOF
	}
	s.writeFn = func(p []byte) (int, error) {
		return s.writeBuf.Write(p)
	}
	return s
}

func (s *testStream) wasReset() bool {
	s.resetMu.Lock()
	defer s.resetMu.Unlock()
	return s.reset
}

func (s *testStream) wasClosed() bool {
	s.resetMu.Lock()
	defer s.resetMu.Unlock()
	return s.closed
}

func (s *testStream) markReset() {
	s.resetMu.Lock()
	defer s.resetMu.Unlock()
	if s.reset {
		return
	}
	s.reset = true
	close(s.resetCh)
}

func (s *testStream) Close() error {
	s.resetMu.Lock()
	s.closed = true
	s.resetMu.Unlock()
	s.markReset()
	return nil
}
func (s *testStream) CloseRead() error                             { return nil }
func (s *testStream) CloseWrite() error                            { return nil }
func (s *testStream) Conn() network.Conn                           { return s.conn }
func (s *testStream) ID() string                                   { return "test-stream" }
func (s *testStream) Protocol() protocol.ID                        { return s.protocol }
func (s *testStream) Read(p []byte) (int, error)                   { return s.readFn(p) }
func (s *testStream) Reset() error                                 { s.markReset(); return nil }
func (s *testStream) ResetWithError(network.StreamErrorCode) error { s.markReset(); return nil }
func (s *testStream) Scope() network.StreamScope                   { return nil }
func (s *testStream) SetDeadline(time.Time) error                  { return nil }
func (s *testStream) SetProtocol(id protocol.ID) error             { s.protocol = id; return nil }
func (s *testStream) SetReadDeadline(time.Time) error              { return nil }
func (s *testStream) SetWriteDeadline(time.Time) error             { return nil }
func (s *testStream) Stat() network.Stats                          { return network.Stats{} }
func (s *testStream) Write(p []byte) (int, error)                  { return s.writeFn(p) }

func connectHosts(t *testing.T, a host.Host, b host.Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Connect(ctx, peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
}

func withSyncPeerTimeoutOverride(t *testing.T, timeout time.Duration) {
	t.Helper()
	old := syncPeerRequestTimeoutOverride
	syncPeerRequestTimeoutOverride = timeout
	t.Cleanup(func() {
		syncPeerRequestTimeoutOverride = old
	})
}

func newRequestTestNode(t *testing.T) (*Node, host.Host) {
	t.Helper()
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("libp2p host: %v", err)
	}
	t.Cleanup(func() {
		_ = h.Close()
	})
	node.Host = h
	return node, h
}

func decodeCapturedRequest(t *testing.T, stream *testStream) BlockRequest {
	t.Helper()
	var req BlockRequest
	if err := json.Unmarshal(stream.writeBuf.Bytes(), &req); err != nil {
		t.Fatalf("decode captured request: %v", err)
	}
	return req
}

func withScheduledUpdateTraceGlobals(t *testing.T, deterministic bool) {
	t.Helper()
	oldDeterministic := DeterministicValidatorSelection
	oldDynamic := DynamicValidatorSelectionEnabled
	oldInactiveBlocks := ValidatorInactiveBlocks
	oldCommitmentV2Height := ValidatorSetCommitmentV2Height
	oldSafeMode := ConsensusPostBlockSafeModeEnabled
	t.Cleanup(func() {
		DeterministicValidatorSelection = oldDeterministic
		DynamicValidatorSelectionEnabled = oldDynamic
		ValidatorInactiveBlocks = oldInactiveBlocks
		ValidatorSetCommitmentV2Height = oldCommitmentV2Height
		ConsensusPostBlockSafeModeEnabled = oldSafeMode
	})
	DeterministicValidatorSelection = deterministic
	DynamicValidatorSelectionEnabled = true
	ValidatorInactiveBlocks = 0
	ValidatorSetCommitmentV2Height = 1
	ConsensusPostBlockSafeModeEnabled = false
}

func withScheduledUpdateTraceBuffer(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := scheduledUpdateTraceOutput
	scheduledUpdateTraceOutput = &buf
	t.Cleanup(func() {
		scheduledUpdateTraceOutput = old
	})
	return &buf
}

func assertTraceSubstringsInOrder(t *testing.T, out string, parts []string) {
	t.Helper()
	offset := 0
	for _, part := range parts {
		idx := strings.Index(out[offset:], part)
		if idx < 0 {
			t.Fatalf("expected trace substring %q in output:\n%s", part, out)
		}
		offset += idx + len(part)
	}
}

func buildSyncTestBlocks(t *testing.T, count int) []Block {
	t.Helper()
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	blocks := make([]Block, 0, count)
	for i := 0; i < count; i++ {
		block := node.BuildLeaderBlock(node.currentEpoch())
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		_ = node.ReceiveBlock(block, node.Blockchain)
		if node.Blockchain.Height() != block.ID {
			t.Fatalf("failed to build sync test block %d", block.ID)
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func installBlockRangeStreamHandler(t *testing.T, h host.Host, blocks map[uint64]Block) {
	t.Helper()
	h.SetStreamHandler(BlockSyncProtocol, func(s network.Stream) {
		defer s.Close()
		dec := json.NewDecoder(s)
		enc := json.NewEncoder(s)
		var req BlockRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		respBlocks := make([]Block, 0, int(req.To-req.From+1))
		for height := req.From; height <= req.To; height++ {
			block, ok := blocks[height]
			if !ok {
				break
			}
			respBlocks = append(respBlocks, block)
		}
		_ = enc.Encode(BlockResponse{Blocks: respBlocks})
	})
}

func newBlockRequestTestStream(t *testing.T, pid peer.ID, req BlockRequest) *testStream {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal block request: %v", err)
	}
	reader := bytes.NewReader(payload)
	s := &testStream{
		conn:    &testConn{remote: pid},
		resetCh: make(chan struct{}),
	}
	s.readFn = func(p []byte) (int, error) {
		return reader.Read(p)
	}
	s.writeFn = func(p []byte) (int, error) {
		return s.writeBuf.Write(p)
	}
	return s
}

func decodeCapturedBlockResponse(t *testing.T, stream *testStream) BlockResponse {
	t.Helper()
	var resp BlockResponse
	if err := json.Unmarshal(stream.writeBuf.Bytes(), &resp); err != nil {
		t.Fatalf("decode captured block response: %v", err)
	}
	return resp
}

func installInMemoryBlocksForSyncServe(node *Node, count int) []Block {
	blocks := make([]Block, 0, count)
	prevHash := ""
	for height := 1; height <= count; height++ {
		block := Block{
			ID:        uint64(height),
			PrevHash:  prevHash,
			BlockHash: "block-" + strconv.Itoa(height),
		}
		blocks = append(blocks, block)
		prevHash = block.BlockHash
	}
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = append([]Block{}, blocks...)
	node.Blockchain.mu.Unlock()
	return blocks
}

func TestHandleBlockStreamCapsServedRangeExactlyAtLimit(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	installInMemoryBlocksForSyncServe(node, blockSyncServeMaxBlocks+1)

	stream := newBlockRequestTestStream(t, "peer-a", BlockRequest{
		From: 1,
		To:   uint64(blockSyncServeMaxBlocks + 1),
	})
	node.handleBlockStream(stream)

	resp := decodeCapturedBlockResponse(t, stream)
	if len(resp.Blocks) != blockSyncServeMaxBlocks {
		t.Fatalf("expected capped response with %d blocks, got %d", blockSyncServeMaxBlocks, len(resp.Blocks))
	}
	if got := resp.Blocks[len(resp.Blocks)-1].ID; got != uint64(blockSyncServeMaxBlocks) {
		t.Fatalf("expected last served height %d, got %d", blockSyncServeMaxBlocks, got)
	}
}

func TestHandleBlockStreamDoesNotWidenAckedSnapshotRange(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	installInMemoryBlocksForSyncServe(node, 10)
	peerID := peer.ID("peer-a")
	node.recordPeerAck(peerID.String(), 5)

	stream := newBlockRequestTestStream(t, peerID, BlockRequest{
		From:         1,
		To:           2,
		WantSnapshot: true,
	})
	node.handleBlockStream(stream)

	resp := decodeCapturedBlockResponse(t, stream)
	if len(resp.Blocks) != 0 {
		t.Fatalf("expected acked snapshot range to return no blocks, got %d", len(resp.Blocks))
	}
}

func TestBlockRequestServeConcurrencyScalesWithMaxPeers(t *testing.T) {
	want := MaxPeers * blockRequestServeSlotsPerPeer
	if want < blockRequestServeMinConcurrency {
		want = blockRequestServeMinConcurrency
	}
	if got := blockRequestServeConcurrencyLimit(MaxPeers); got != want {
		t.Fatalf("expected concurrency limit %d for MaxPeers=%d, got %d", want, MaxPeers, got)
	}
	if got := cap(blockRequestServeSem); got != want {
		t.Fatalf("expected semaphore capacity %d, got %d", want, got)
	}
	if got := blockRequestServeConcurrencyLimit(1); got != blockRequestServeMinConcurrency {
		t.Fatalf("expected small peer counts to keep floor %d, got %d", blockRequestServeMinConcurrency, got)
	}
}

func TestRequestBlocksFromPeerOpenTimeout(t *testing.T) {
	withSyncPeerTimeoutOverride(t, 20*time.Millisecond)
	node, _ := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()

	blocker := make(chan struct{})
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			<-blocker
			return nil, nil
		},
	}

	_, _, _, err = node.requestBlocksFromPeer(remote.ID(), 1, 2, false, 0)
	close(blocker)
	if err == nil {
		t.Fatalf("expected open timeout error")
	}
	var phaseErr *blockRequestPhaseError
	if !errors.As(err, &phaseErr) {
		t.Fatalf("expected blockRequestPhaseError, got %T", err)
	}
	if phaseErr.Stage != "open" || !phaseErr.Timeout {
		t.Fatalf("unexpected phase error: %+v", phaseErr)
	}
}

func TestRequestBlocksFromPeerEncodeTimeout(t *testing.T) {
	withSyncPeerTimeoutOverride(t, 20*time.Millisecond)
	node, _ := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()

	stream := newBlockingWriteStream(remote.ID())
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			return stream, nil
		},
	}

	_, _, _, err = node.requestBlocksFromPeer(remote.ID(), 1, 2, false, 0)
	if err == nil {
		t.Fatalf("expected encode timeout error")
	}
	var phaseErr *blockRequestPhaseError
	if !errors.As(err, &phaseErr) {
		t.Fatalf("expected blockRequestPhaseError, got %T", err)
	}
	if phaseErr.Stage != "encode" || !phaseErr.Timeout {
		t.Fatalf("unexpected phase error: %+v", phaseErr)
	}
	if !stream.wasReset() {
		t.Fatalf("expected encode timeout to reset stream")
	}
}

func TestRequestBlocksFromPeerDecodeTimeout(t *testing.T) {
	withSyncPeerTimeoutOverride(t, 20*time.Millisecond)
	node, _ := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()

	stream := newBlockingReadStream(remote.ID())
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			return stream, nil
		},
	}

	_, _, _, err = node.requestBlocksFromPeer(remote.ID(), 1, 2, false, 0)
	if err == nil {
		t.Fatalf("expected decode timeout error")
	}
	var phaseErr *blockRequestPhaseError
	if !errors.As(err, &phaseErr) {
		t.Fatalf("expected blockRequestPhaseError, got %T", err)
	}
	if phaseErr.Stage != "decode" || !phaseErr.Timeout {
		t.Fatalf("unexpected phase error: %+v", phaseErr)
	}
	if !stream.wasReset() {
		t.Fatalf("expected decode timeout to reset stream")
	}
}

func TestRequestBlocksFromPeerEncodeFailureDoesNotCloseAfterReset(t *testing.T) {
	node, _ := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()

	stream := newFailingWriteStream(remote.ID())
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			return stream, nil
		},
	}

	_, _, _, err = node.requestBlocksFromPeerDirect(remote.ID(), 1, 2, false, 0)
	if err == nil {
		t.Fatalf("expected encode error")
	}
	var phaseErr *blockRequestPhaseError
	if !errors.As(err, &phaseErr) {
		t.Fatalf("expected blockRequestPhaseError, got %T", err)
	}
	if phaseErr.Stage != "encode" {
		t.Fatalf("unexpected phase error: %+v", phaseErr)
	}
	if !stream.wasReset() {
		t.Fatalf("expected encode failure to reset stream")
	}
	if stream.wasClosed() {
		t.Fatalf("encode failure should not close stream after reset")
	}
}

func TestRequestBlocksFromPeerDecodeFailureDoesNotCloseAfterReset(t *testing.T) {
	node, _ := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()

	stream := newFailingReadStream(remote.ID())
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			return stream, nil
		},
	}

	_, _, _, err = node.requestBlocksFromPeerDirect(remote.ID(), 1, 2, false, 0)
	if err == nil {
		t.Fatalf("expected decode error")
	}
	var phaseErr *blockRequestPhaseError
	if !errors.As(err, &phaseErr) {
		t.Fatalf("expected blockRequestPhaseError, got %T", err)
	}
	if phaseErr.Stage != "decode" {
		t.Fatalf("unexpected phase error: %+v", phaseErr)
	}
	if !stream.wasReset() {
		t.Fatalf("expected decode failure to reset stream")
	}
	if stream.wasClosed() {
		t.Fatalf("decode failure should not close stream after reset")
	}
}

func TestRequestBlocksFromPeerSkipsNetworkWhenRangeAlreadyLocal(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	blocks := []Block{
		{ID: 1, BlockHash: "block-1"},
		{ID: 2, PrevHash: "block-1", BlockHash: "block-2"},
	}

	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = append([]Block{}, blocks...)
	node.Blockchain.mu.Unlock()

	got, snap, execPool, err := node.requestBlocksFromPeer("peer-a", 1, 2, false, 0)
	if err != nil {
		t.Fatalf("expected local short-circuit success, got err=%v", err)
	}
	if snap != nil || execPool != nil {
		t.Fatalf("expected no snapshot payloads from local short-circuit")
	}
	if len(got) != len(blocks) {
		t.Fatalf("expected %d local blocks, got %d", len(blocks), len(got))
	}
	for i := range blocks {
		if got[i].BlockHash != blocks[i].BlockHash {
			t.Fatalf("unexpected block at index %d: got=%s want=%s", i, got[i].BlockHash, blocks[i].BlockHash)
		}
	}
}

func TestReceiveBlockDoesNotDeadlockInScheduledValidatorUpdates(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	done := make(chan struct{})
	go func() {
		_ = node.ReceiveBlock(block, node.Blockchain)
		close(done)
	}()

	select {
	case <-done:
		if got := node.Blockchain.Height(); got != block.ID {
			t.Fatalf("expected block height %d after receive, got %d", block.ID, got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReceiveBlock deadlocked during scheduled validator updates")
	}
}

func TestComputeAdaptiveSyncFetchWindow(t *testing.T) {
	tests := []struct {
		lag  uint64
		want uint64
	}{
		{lag: 50, want: 128},
		{lag: 300, want: 512},
		{lag: 1500, want: 1024},
		{lag: 9000, want: 2048},
	}
	for _, tt := range tests {
		if got := computeAdaptiveSyncFetchWindow(tt.lag); got != tt.want {
			t.Fatalf("lag=%d got=%d want=%d", tt.lag, got, tt.want)
		}
	}
}

func TestPlanSyncRangeAssignmentsRespectsServeLimit(t *testing.T) {
	peers := []peer.ID{"peer-a", "peer-b", "peer-c"}
	assignments := planSyncRangeAssignments(1, 600, 512, peers)
	if len(assignments) != 3 {
		t.Fatalf("expected 3 assignments, got %d", len(assignments))
	}
	if assignments[0].From != 1 || assignments[0].To != 100 {
		t.Fatalf("unexpected first assignment %d-%d", assignments[0].From, assignments[0].To)
	}
	if assignments[1].From != 101 || assignments[1].To != 200 {
		t.Fatalf("unexpected second assignment %d-%d", assignments[1].From, assignments[1].To)
	}
	if assignments[2].From != 201 || assignments[2].To != 300 {
		t.Fatalf("unexpected third assignment %d-%d", assignments[2].From, assignments[2].To)
	}
}

func TestRequestParallelBlockRangesMergesAssignments(t *testing.T) {
	requester, localHost := newRequestTestNode(t)
	blocks := buildSyncTestBlocks(t, 6)
	blocksByHeight := make(map[uint64]Block, len(blocks))
	for _, block := range blocks {
		blocksByHeight[block.ID] = block
	}

	remoteA, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host A: %v", err)
	}
	defer remoteA.Close()
	remoteB, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host B: %v", err)
	}
	defer remoteB.Close()
	remoteC, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host C: %v", err)
	}
	defer remoteC.Close()

	installBlockRangeStreamHandler(t, remoteA, blocksByHeight)
	installBlockRangeStreamHandler(t, remoteB, blocksByHeight)
	installBlockRangeStreamHandler(t, remoteC, blocksByHeight)
	connectHosts(t, localHost, remoteA)
	connectHosts(t, localHost, remoteB)
	connectHosts(t, localHost, remoteC)

	assignments := []syncRangeAssignment{
		{Peer: remoteA.ID(), From: 1, To: 2},
		{Peer: remoteB.ID(), From: 3, To: 4},
		{Peer: remoteC.ID(), From: 5, To: 6},
	}
	merged, failedPeers, err := requester.requestParallelBlockRanges(assignments)
	if err != nil {
		t.Fatalf("parallel fetch failed: %v", err)
	}
	if len(failedPeers) != 0 {
		t.Fatalf("expected no failed peers, got %d", len(failedPeers))
	}
	if len(merged) != 6 {
		t.Fatalf("expected 6 merged blocks, got %d", len(merged))
	}
	for idx, block := range merged {
		if got, want := block.ID, uint64(idx+1); got != want {
			t.Fatalf("merged block index=%d got=%d want=%d", idx, got, want)
		}
	}
}

func TestSyncApplyDownloadedBlocksCommitsOrderedBlocks(t *testing.T) {
	oldCommitmentV2 := ValidatorSetCommitmentV2Height
	t.Cleanup(func() {
		ValidatorSetCommitmentV2Height = oldCommitmentV2
	})
	ValidatorSetCommitmentV2Height = 1_000_000

	source := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	blocks := make([]Block, 0, 3)
	for i := 0; i < 3; i++ {
		block := source.BuildLeaderBlock(source.currentEpoch())
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		_ = source.ReceiveBlock(block, source.Blockchain)
		if source.Blockchain.Height() != block.ID {
			t.Fatalf("failed to build source block %d", block.ID)
		}
		blocks = append(blocks, block)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	height := target.syncApplyDownloadedBlocks(blocks)
	if height != blocks[len(blocks)-1].ID {
		t.Fatalf("expected worker apply height %d, got %d", blocks[len(blocks)-1].ID, height)
	}
	if target.Blockchain.Height() != blocks[len(blocks)-1].ID {
		t.Fatalf("expected blockchain height %d, got %d", blocks[len(blocks)-1].ID, target.Blockchain.Height())
	}
}

func TestApplySyncBlockWithRetryReturnsFailureReason(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.PrevHash = "deadbeef"
	block.BlockHash = HashBlock(block)

	result := node.applySyncBlockWithRetry(block)
	if result.Applied {
		t.Fatal("expected invalid block to fail worker apply")
	}
	if result.Reason != "parent_mismatch" {
		t.Fatalf("expected parent_mismatch, got %q", result.Reason)
	}
	if node.Blockchain.Height() != 0 {
		t.Fatalf("expected blockchain height 0, got %d", node.Blockchain.Height())
	}
}

func TestApplySyncBlockWithRetryClearsSeenNoProgressAndRetries(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	node.markBlockSeen(blockSeenKey(block))

	result := node.applySyncBlockWithRetry(block)
	if !result.Applied {
		t.Fatalf("expected already-seen sync block to clear seen marker and retry, got reason %q", result.Reason)
	}
	if result.Retries == 0 {
		t.Fatal("expected apply to require at least one retry after clearing seen marker")
	}
	if _, _, ok := node.syncApplyFailure(block.ID, block.BlockHash); ok {
		t.Fatal("expected no-progress apply failure to clear after retry commit")
	}
	if got := node.Blockchain.Height(); got != block.ID {
		t.Fatalf("expected blockchain height %d, got %d", block.ID, got)
	}
}

func TestSnapshotSyncProcessBlockIncompleteUsesTrustedBoundaryCrossing(t *testing.T) {
	if !snapshotSyncReasonRequiresTrustedSource("queue_apply_failed_process_block_incomplete") {
		t.Fatal("expected process_block_incomplete to require trusted snapshot source")
	}
	if got := snapshotSyncMinHeightOverrideForReason("queue_apply_failed_process_block_incomplete", 100, 200); got != 101 {
		t.Fatalf("expected min height override 101, got %d", got)
	}
}

func TestReceiveBlockReturnsStructuredApplyError(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.PrevHash = "deadbeef"
	block.BlockHash = HashBlock(block)

	err := node.ReceiveBlock(block, node.Blockchain)
	if err == nil {
		t.Fatal("expected structured block apply error")
	}
	var applyErr *BlockApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("expected BlockApplyError, got %T", err)
	}
	if applyErr.Reason != "parent_mismatch" {
		t.Fatalf("expected parent_mismatch, got %q", applyErr.Reason)
	}
}

func TestReceiveBlockClearsSeenHashAfterVerifyFailure(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.PrevHash = "deadbeef"
	block.BlockHash = HashBlock(block)

	_ = node.ReceiveBlock(block, node.Blockchain)
	if node.Blockchain.Height() != 0 {
		t.Fatalf("expected invalid block to be rejected, got height %d", node.Blockchain.Height())
	}
	node.seenBlockMu.Lock()
	seenAfterFailure := node.SeenBlockHashes[block.BlockHash]
	node.seenBlockMu.Unlock()
	if seenAfterFailure {
		t.Fatal("expected failed block hash to be cleared from seen cache")
	}
}

func TestRecordSyncApplyFailureTracksAndClears(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	block := node.BuildLeaderBlock(node.currentEpoch())
	block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
	block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
	block.BlockHash = HashBlock(block)

	if got := node.recordSyncApplyFailure(0, block, "invalid_proposer"); got != 1 {
		t.Fatalf("expected first failure count 1, got %d", got)
	}
	if got := node.recordSyncApplyFailure(0, block, "invalid_proposer"); got != 2 {
		t.Fatalf("expected repeated failure count 2, got %d", got)
	}
	reason, count, ok := node.syncApplyFailure(block.ID, block.BlockHash)
	if !ok {
		t.Fatal("expected sync apply failure to be recorded")
	}
	if reason != "invalid_proposer" || count != 2 {
		t.Fatalf("unexpected sync apply failure state reason=%q count=%d", reason, count)
	}

	node.clearSyncApplyFailure(block.ID, block.BlockHash)
	if _, _, ok := node.syncApplyFailure(block.ID, block.BlockHash); ok {
		t.Fatal("expected sync apply failure to clear")
	}
}

func TestProcessQueuedBlocksCapturesRejectReasonForQueuedBlock(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	block1 := node.BuildLeaderBlock(node.currentEpoch())
	block1.BlockTime = LogicalTimeForEpochTick(block1.ID, TickFinalize)
	block1.Timestamp = int64(SystemTimeUnits(block1.BlockTime))
	block1.BlockHash = HashBlock(block1)
	_ = node.ReceiveBlock(block1, node.Blockchain)
	if node.Blockchain.Height() != block1.ID {
		t.Fatalf("expected first block to commit, got height %d", node.Blockchain.Height())
	}

	block2 := node.BuildLeaderBlock(node.currentEpoch())
	block2.BlockTime = LogicalTimeForEpochTick(block2.ID, TickFinalize)
	block2.Timestamp = int64(SystemTimeUnits(block2.BlockTime))
	for _, candidate := range []string{"A", "B", "C", "D"} {
		if normalizeValidatorID(candidate) == normalizeValidatorID(block2.Proposer) {
			continue
		}
		block2.Proposer = candidate
		break
	}
	block2.BlockHash = HashBlock(block2)
	node.QueueFutureBlock(block2)

	node.ProcessQueuedBlocks()

	if node.Blockchain.Height() != block1.ID {
		t.Fatalf("expected queued invalid block to be rejected, got height %d", node.Blockchain.Height())
	}
	reason, count, ok := node.syncApplyFailure(block2.ID, block2.BlockHash)
	if !ok {
		t.Fatal("expected queued apply failure reason to be recorded")
	}
	if reason != "validation_failed" || count != 1 {
		t.Fatalf("unexpected queued apply failure reason=%q count=%d", reason, count)
	}
}

func TestRewindLocalChainToCommonAncestorPrunesForkTip(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	block1 := node.BuildLeaderBlock(node.currentEpoch())
	block1.BlockTime = LogicalTimeForEpochTick(block1.ID, TickFinalize)
	block1.Timestamp = int64(SystemTimeUnits(block1.BlockTime))
	block1.BlockHash = HashBlock(block1)
	node.Blockchain.AddBlock(block1)
	node.StoreBlock(block1)
	node.cacheExecutionSnapshotLedger(block1.ID, node.currentExecutionLedgerClone())
	node.markExecutionSnapshotReadyHeight(block1.ID)

	forkTip := node.BuildLeaderBlock(block1.ID + 1)
	forkTip.PrevHash = block1.BlockHash
	forkTip.BlockTime = LogicalTimeForEpochTick(forkTip.ID, TickFinalize)
	forkTip.Timestamp = int64(SystemTimeUnits(forkTip.BlockTime))
	forkTip.StateRoot = "fork-state-root"
	forkTip.BlockHash = HashBlock(forkTip)
	node.Blockchain.AddBlock(forkTip)
	node.StoreBlock(forkTip)
	node.commitMu.Lock()
	node.committedHeight = forkTip.ID
	node.finalizedHeight = forkTip.ID
	node.committed[forkTip.ID] = forkTip.BlockHash
	node.commitMu.Unlock()
	node.QueueFutureBlock(Block{ID: forkTip.ID + 2, PrevHash: "missing-parent", BlockHash: "future"})

	if !node.rewindLocalChainToHeight(block1.ID, "test_common_ancestor") {
		t.Fatal("expected rewind to common ancestor")
	}
	if got := node.Blockchain.Height(); got != block1.ID {
		t.Fatalf("expected height %d after rewind, got %d", block1.ID, got)
	}
	node.Blockchain.mu.RLock()
	rewoundBlocks := append([]Block(nil), node.Blockchain.Blocks...)
	node.Blockchain.mu.RUnlock()
	if len(rewoundBlocks) != 2 || rewoundBlocks[0].ID != 0 || rewoundBlocks[1].ID != block1.ID {
		t.Fatalf("expected canonical genesis+anchor chain after rewind, got len=%d blocks=%v", len(rewoundBlocks), rewoundBlocks)
	}
	if _, ok := node.LoadBlock(int(forkTip.ID)); ok {
		t.Fatalf("expected fork tip %d to be pruned from storage", forkTip.ID)
	}
	node.commitMu.Lock()
	committedHeight := node.committedHeight
	_, forkCommitted := node.committed[forkTip.ID]
	node.commitMu.Unlock()
	if committedHeight != block1.ID || forkCommitted {
		t.Fatalf("unexpected commit state after rewind: height=%d fork_committed=%t", committedHeight, forkCommitted)
	}
	if queueTip := node.maxQueuedFutureHeight(block1.ID); queueTip != 0 {
		t.Fatalf("expected queued fork blocks to clear, got queue tip %d", queueTip)
	}
}

func TestRewindLocalChainRebuildsContiguousBlocksFromStorage(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	genesis := node.Blockchain.Blocks[0]
	block1 := Block{ID: 1, PrevHash: genesis.BlockHash, BlockHash: "block-1", StateRoot: "state-1"}
	block2 := Block{ID: 2, PrevHash: block1.BlockHash, BlockHash: "block-2", StateRoot: "state-2"}
	forkTip := Block{ID: 3, PrevHash: block2.BlockHash, BlockHash: "fork-3", StateRoot: "fork-state"}
	node.StoreBlock(block1)
	node.StoreBlock(block2)
	node.StoreBlock(forkTip)

	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{genesis, block2, forkTip}
	node.Blockchain.mu.Unlock()
	node.commitMu.Lock()
	node.committedHeight = forkTip.ID
	node.finalizedHeight = forkTip.ID
	node.committed[forkTip.ID] = forkTip.BlockHash
	node.commitMu.Unlock()

	if !node.rewindLocalChainToHeight(block2.ID, "test_rebuild_storage") {
		t.Fatal("expected rewind to rebuild canonical chain")
	}

	node.Blockchain.mu.RLock()
	rebuilt := append([]Block(nil), node.Blockchain.Blocks...)
	node.Blockchain.mu.RUnlock()
	if len(rebuilt) != 3 {
		t.Fatalf("expected genesis plus blocks 1..2 after rewind, got %d blocks: %#v", len(rebuilt), rebuilt)
	}
	for i, wantID := range []uint64{0, 1, 2} {
		if rebuilt[i].ID != wantID {
			t.Fatalf("rebuilt chain index %d ID=%d, want %d", i, rebuilt[i].ID, wantID)
		}
	}
	if rebuilt[2].BlockHash != block2.BlockHash || rebuilt[2].PrevHash != block1.BlockHash {
		t.Fatalf("anchor not preserved in rebuilt chain: %#v", rebuilt[2])
	}
	if _, ok := node.Blockchain.GetBlock(1); !ok {
		t.Fatalf("rewind lost parent block from canonical chain")
	}
	if _, ok := node.Blockchain.GetBlock(2); !ok {
		t.Fatalf("rewind lost anchor block from canonical chain")
	}
	if _, ok := node.Blockchain.GetBlock(3); ok {
		t.Fatalf("rewind retained fork tip in canonical chain")
	}
	node.commitMu.Lock()
	committedHeight := node.committedHeight
	_, forkCommitted := node.committed[forkTip.ID]
	node.commitMu.Unlock()
	if committedHeight != block2.ID || forkCommitted {
		t.Fatalf("unexpected committed state after rewind: height=%d fork_committed=%t", committedHeight, forkCommitted)
	}
}

func TestRewindLocalChainPrefersPersistedBlocksOverStaleMemory(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	genesis := node.Blockchain.Blocks[0]
	block1 := Block{ID: 1, PrevHash: genesis.BlockHash, BlockHash: "block-1", StateRoot: "state-1"}
	block2 := Block{ID: 2, PrevHash: block1.BlockHash, BlockHash: "block-2", StateRoot: "state-2"}
	forkTip := Block{ID: 3, PrevHash: block2.BlockHash, BlockHash: "fork-3", StateRoot: "fork-state"}
	node.StoreBlock(block1)
	node.StoreBlock(block2)
	node.StoreBlock(forkTip)

	staleBlock1 := block1
	staleBlock1.BlockHash = "stale-memory-block-1"
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{genesis, staleBlock1, block2, forkTip}
	node.Blockchain.mu.Unlock()
	node.commitMu.Lock()
	node.committedHeight = forkTip.ID
	node.finalizedHeight = forkTip.ID
	node.committed[forkTip.ID] = forkTip.BlockHash
	node.commitMu.Unlock()

	if !node.rewindLocalChainToHeight(block2.ID, "test_rebuild_prefers_persisted") {
		t.Fatal("expected rewind to rebuild from persisted canonical blocks")
	}

	node.Blockchain.mu.RLock()
	rebuilt := append([]Block(nil), node.Blockchain.Blocks...)
	node.Blockchain.mu.RUnlock()
	if len(rebuilt) != 3 {
		t.Fatalf("expected genesis plus blocks 1..2 after rewind, got %d blocks: %#v", len(rebuilt), rebuilt)
	}
	if rebuilt[1].BlockHash != block1.BlockHash {
		t.Fatalf("rewind used stale in-memory parent hash: got=%q want=%q", rebuilt[1].BlockHash, block1.BlockHash)
	}
	if rebuilt[2].PrevHash != block1.BlockHash {
		t.Fatalf("rebuilt anchor is not linked to persisted parent: got prev=%q want=%q", rebuilt[2].PrevHash, block1.BlockHash)
	}
}

func TestRewindLocalChainRebuildsWithPersistedGenesisParentHash(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	defaultGenesis := node.Blockchain.Blocks[0]
	persistedGenesisHash := "persisted-genesis-parent"
	block1 := Block{ID: 1, PrevHash: persistedGenesisHash, BlockHash: "block-1", StateRoot: "state-1"}
	block2 := Block{ID: 2, PrevHash: block1.BlockHash, BlockHash: "block-2", StateRoot: "state-2"}
	forkTip := Block{ID: 3, PrevHash: block2.BlockHash, BlockHash: "fork-3", StateRoot: "fork-state"}
	node.StoreBlock(block1)
	node.StoreBlock(block2)
	node.StoreBlock(forkTip)

	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{defaultGenesis, block2, forkTip}
	node.Blockchain.mu.Unlock()
	node.commitMu.Lock()
	node.committedHeight = forkTip.ID
	node.finalizedHeight = forkTip.ID
	node.committed[forkTip.ID] = forkTip.BlockHash
	node.commitMu.Unlock()

	if !node.rewindLocalChainToHeight(block2.ID, "test_rebuild_persisted_genesis_parent") {
		t.Fatal("expected rewind to rebuild from persisted block-one parent hash")
	}

	node.Blockchain.mu.RLock()
	rebuilt := append([]Block(nil), node.Blockchain.Blocks...)
	node.Blockchain.mu.RUnlock()
	if len(rebuilt) != 3 {
		t.Fatalf("expected genesis plus blocks 1..2 after rewind, got %d blocks: %#v", len(rebuilt), rebuilt)
	}
	if rebuilt[0].BlockHash != persistedGenesisHash {
		t.Fatalf("expected rebuilt genesis hash %q, got %q", persistedGenesisHash, rebuilt[0].BlockHash)
	}
	if rebuilt[1].PrevHash != persistedGenesisHash || rebuilt[2].PrevHash != block1.BlockHash {
		t.Fatalf("rebuilt chain is not contiguous: %#v", rebuilt)
	}
}

func TestRewindLocalChainUsesSparseSnapshotAnchorWhenHistoryPruned(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	ledger := NewLedger()
	vhash := ValidatorSetHash(validators)
	anchor := Block{
		ID:                   50,
		Height:               50,
		Type:                 BlockTypeTime,
		PrevHash:             "block-49",
		Proposer:             "A",
		ValidatorSetHash:     vhash,
		NextValidatorSetHash: vhash,
		BlockTime:            LogicalTimeForEpoch(50),
	}
	anchor.Timestamp = int64(SystemTimeUnits(anchor.BlockTime))
	anchor.StateRoot = ComputeExecHashVersioned(anchor, HashLedger(ledger), executionStateRootVersionForHeight(anchor.ID))
	anchor.BlockHash = HashBlock(anchor)
	node.StoreBlock(anchor)
	storeExecutionSnapshotForTest(t, node, anchor, ledger, SnapshotVersion, snapshotLedgerStageExecution)

	forkTip := Block{
		ID:        51,
		Height:    51,
		Type:      BlockTypeTime,
		PrevHash:  "fork-parent",
		BlockHash: "fork-51",
		StateRoot: "fork-state",
		BlockTime: LogicalTimeForEpoch(51),
	}
	node.StoreBlock(forkTip)
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{anchor, forkTip}
	node.Blockchain.mu.Unlock()
	node.commitMu.Lock()
	node.committedHeight = forkTip.ID
	node.finalizedHeight = forkTip.ID
	node.committed[forkTip.ID] = forkTip.BlockHash
	node.commitMu.Unlock()

	if !node.rewindLocalChainToHeight(anchor.ID, "test_sparse_snapshot_anchor") {
		t.Fatal("expected sparse snapshot rewind to anchor")
	}
	node.Blockchain.mu.RLock()
	rewound := append([]Block(nil), node.Blockchain.Blocks...)
	node.Blockchain.mu.RUnlock()
	if len(rewound) != 1 || rewound[0].ID != anchor.ID || rewound[0].BlockHash != anchor.BlockHash {
		t.Fatalf("expected sparse anchor-only chain after rewind, got %#v", rewound)
	}
	if got := node.Blockchain.Height(); got != anchor.ID {
		t.Fatalf("expected height %d after sparse rewind, got %d", anchor.ID, got)
	}
	if _, ok := node.LoadBlock(int(forkTip.ID)); ok {
		t.Fatalf("expected fork tip %d to be pruned from storage", forkTip.ID)
	}
	node.commitMu.Lock()
	committedHeight := node.committedHeight
	_, forkCommitted := node.committed[forkTip.ID]
	node.commitMu.Unlock()
	if committedHeight != anchor.ID || forkCommitted {
		t.Fatalf("unexpected commit state after sparse rewind: height=%d fork_committed=%t", committedHeight, forkCommitted)
	}
}

func TestReceiveBlockBackfillsExactNextWhenFinalizedWatermarkAhead(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})
	bootstrapValidatorRegistry(validators, 1)

	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block1 := node.BuildLeaderBlock(node.currentEpoch())
	block1.BlockTime = LogicalTimeForEpochTick(block1.ID, TickFinalize)
	block1.Timestamp = int64(SystemTimeUnits(block1.BlockTime))
	block1.BlockHash = HashBlock(block1)
	if err := node.ReceiveBlock(block1, node.Blockchain); err != nil {
		t.Fatalf("receive block1: %v", err)
	}

	node.syncMu.Lock()
	node.syncStage = "delta_replay"
	node.syncMu.Unlock()
	node.commitMu.Lock()
	node.committedHeight = block1.ID + WeakSubjectivityDepth + 10
	node.finalizedHeight = node.committedHeight
	node.commitMu.Unlock()

	block2 := node.BuildLeaderBlock(node.currentEpoch())
	block2.BlockTime = LogicalTimeForEpochTick(block2.ID, TickFinalize)
	block2.Timestamp = int64(SystemTimeUnits(block2.BlockTime))
	block2.BlockHash = HashBlock(block2)
	if err := node.ReceiveBlock(block2, node.Blockchain); err != nil {
		t.Fatalf("receive block2 with finalized watermark ahead: %v", err)
	}
	if got := node.Blockchain.Height(); got != block2.ID {
		t.Fatalf("expected exact-next backfill to apply block %d, got height %d", block2.ID, got)
	}
}

func TestApplyValidatorUpdatesFromBlockDoesNotDeadlockOnPlanContext(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	bootstrapValidatorRegistry(validators, 1)

	block := node.BuildLeaderBlock(1)
	block.Signatures = append([]string{}, validators...)
	block.ValidatorRegistryHash = ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	block.NextValidatorSetHeight = 2
	block.NextValidatorSetHash = strings.Repeat("f", 64)
	node.Blockchain.AddBlock(block)

	done := make(chan struct{})
	go func() {
		node.applyValidatorUpdatesFromBlock(block)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("applyValidatorUpdatesFromBlock deadlocked while planning child validator set")
	}
}

func TestScheduledTransitionValidatorsLockedDoesNotDeadlockOnParentPlanFallback(t *testing.T) {
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})

	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	bootstrapValidatorRegistry(validators, 1)

	parent := node.BuildLeaderBlock(1)
	parent.Signatures = append([]string{}, validators...)
	parent.ValidatorRegistryHash = ValidatorRegistrySnapshotHash(GlobalValidatorRegistry.Snapshot())
	parent.NextValidatorSetHeight = 2
	parent.NextValidatorSetHash = strings.Repeat("f", 64)
	node.Blockchain.AddBlock(parent)

	done := make(chan struct{})
	go func() {
		node.validatorSetMu.Lock()
		_ = node.scheduledTransitionValidatorsLocked(2)
		node.validatorSetMu.Unlock()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduledTransitionValidatorsLocked deadlocked while holding validatorSetMu")
	}
}

func TestScheduledTransitionValidatorsLockedDoesNotDeadlockOnFrozenExecutionFallback(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)

	parent := node.BuildLeaderBlock(1)
	node.Blockchain.AddBlock(parent)

	block := node.BuildLeaderBlock(2)
	block.Signatures = []string{"A", "B"}
	block.ExecutionResults = []ExecutionResult{{
		Height:     2,
		BlockHash:  block.BlockHash,
		Signer:     "A",
		ResultHash: "result",
		TxMerkle:   "tx",
	}}
	block.ValidatorSetHash = ValidatorSetHash(validators)
	node.Blockchain.AddBlock(block)

	node.validatorSetMu.Lock()
	if node.frozenValidatorsByHeight == nil {
		node.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if node.frozenValidatorHashByHeight == nil {
		node.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	node.frozenValidatorsByHeight[2] = append([]string{}, validators...)
	node.frozenValidatorHashByHeight[2] = block.ValidatorSetHash
	node.validatorSetMu.Unlock()

	done := make(chan []string, 1)
	go func() {
		node.validatorSetMu.Lock()
		done <- node.scheduledTransitionValidatorsLocked(2)
		node.validatorSetMu.Unlock()
	}()

	select {
	case resolved := <-done:
		if strings.Join(resolved, ",") != strings.Join(validators, ",") {
			t.Fatalf("unexpected resolved validators: %v", resolved)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduledTransitionValidatorsLocked deadlocked on frozen execution fallback while holding validatorSetMu")
	}
}

func TestShouldDeferNonConsensusCommitMaintenance(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	if node.shouldDeferNonConsensusCommitMaintenance() {
		t.Fatal("expected maintenance deferral to be disabled by default")
	}

	node.Consensus.mu.Lock()
	node.Consensus.Syncing = true
	node.Consensus.mu.Unlock()
	if !node.shouldDeferNonConsensusCommitMaintenance() {
		t.Fatal("expected maintenance deferral while syncing")
	}

	node.Consensus.mu.Lock()
	node.Consensus.Syncing = false
	node.Consensus.syncInFlight = true
	node.Consensus.mu.Unlock()
	if !node.shouldDeferNonConsensusCommitMaintenance() {
		t.Fatal("expected maintenance deferral while sync is in flight")
	}
}

func TestShouldScheduleImmediateSyncResume(t *testing.T) {
	if shouldScheduleImmediateSyncResume(132, 132, 1041) {
		t.Fatal("did not expect immediate sync resume without progress")
	}
	if !shouldScheduleImmediateSyncResume(132, 134, 1041) {
		t.Fatal("expected immediate sync resume after partial progress while still behind target")
	}
	if shouldScheduleImmediateSyncResume(132, 1041, 1041) {
		t.Fatal("did not expect immediate sync resume after reaching target")
	}
}

func TestMaybeExitSyncModeArmsWarmupAfterCatchup(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	for height := uint64(1); height <= 12; height++ {
		block := node.BuildLeaderBlock(height)
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		node.Blockchain.AddBlock(block)
	}

	node.Consensus.mu.Lock()
	node.Consensus.Syncing = true
	node.Consensus.Paused = true
	node.Consensus.syncInFlight = true
	node.Consensus.SyncTarget = 12
	node.Consensus.mu.Unlock()

	node.syncMu.Lock()
	node.syncWarmupJoinHeight = 0
	node.syncWarmupEligible = true
	node.syncMu.Unlock()

	if cleared := node.maybeExitSyncMode("rejoin_catchup_complete"); !cleared {
		t.Fatal("expected sync mode to clear")
	}
	if remaining := node.snapshotWarmupRemaining(node.Blockchain.Height()); remaining == 0 {
		t.Fatal("expected rejoin warmup after sync clear")
	}
	runtime := node.runtimeStatusSnapshot()
	if runtime.SnapshotWarmupRemainingBlocks == 0 {
		t.Fatal("expected runtime snapshot to report rejoin warmup")
	}
	if runtime.VoteEnabled {
		t.Fatal("expected voting to remain disabled during rejoin warmup")
	}
}

func TestSyncBlockRangeWarmupRequiresMeaningfulValidatorDrift(t *testing.T) {
	previous := ValidatorLivenessMaxHeightDriftBlocks
	ValidatorLivenessMaxHeightDriftBlocks = 8
	t.Cleanup(func() {
		ValidatorLivenessMaxHeightDriftBlocks = previous
	})

	if syncBlockRangeWarmupEligible(100, 102) {
		t.Fatal("routine two-block catch-up must not trigger validator rejoin warmup")
	}
	if syncBlockRangeWarmupEligible(100, 108) {
		t.Fatal("catch-up inside the configured liveness drift must not trigger validator rejoin warmup")
	}
	if !syncBlockRangeWarmupEligible(100, 109) {
		t.Fatal("catch-up beyond the configured liveness drift must trigger validator rejoin warmup")
	}
}

func TestSnapshotWarmupUsesLocalTipForProposalHeight(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	for height := uint64(1); height <= 12; height++ {
		block := node.BuildLeaderBlock(height)
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		node.Blockchain.AddBlock(block)
	}

	node.setSnapshotWarmupJoinHeight(node.Blockchain.Height())
	node.syncMu.Lock()
	node.syncWarmupStartAt = time.Now().Add(-2 * syncSnapshotWarmupDuration())
	node.syncWarmupLastHeight = node.Blockchain.Height()
	node.syncWarmupLastHeightAt = time.Now().Add(-2 * syncSnapshotWarmupDuration())
	node.syncWarmupQuorumSince = time.Now().Add(-2 * syncSnapshotWarmupDuration())
	node.syncMu.Unlock()

	_ = node.snapshotWarmupActive(node.Blockchain.Height() + 1)

	node.syncMu.Lock()
	got := node.syncWarmupLastHeight
	node.syncMu.Unlock()
	if got != node.Blockchain.Height() {
		t.Fatalf("snapshot warmup should evaluate at local tip, got height=%d want=%d", got, node.Blockchain.Height())
	}
}

func TestSnapshotWarmupDoesNotRegressLastHeightFromStaleCaller(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	for height := uint64(1); height <= 12; height++ {
		block := node.BuildLeaderBlock(height)
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		node.Blockchain.AddBlock(block)
	}

	tip := node.Blockchain.Height()
	node.setSnapshotWarmupJoinHeight(tip)
	observedHeight := tip + 2
	observedAt := time.Now().Add(-time.Second)
	node.syncMu.Lock()
	node.syncWarmupLastHeight = observedHeight
	node.syncWarmupLastHeightAt = observedAt
	node.syncMu.Unlock()

	_ = node.snapshotWarmupActive(tip)

	node.syncMu.Lock()
	gotHeight := node.syncWarmupLastHeight
	gotAt := node.syncWarmupLastHeightAt
	node.syncMu.Unlock()
	if gotHeight != observedHeight {
		t.Fatalf("stale warmup caller regressed last height: got=%d want=%d", gotHeight, observedHeight)
	}
	if !gotAt.Equal(observedAt) {
		t.Fatalf("stale warmup caller rewrote last height timestamp: got=%s want=%s", gotAt, observedAt)
	}
}

func TestSnapshotWarmupClearsWithStableLocalFinalizedSet(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	for height := uint64(1); height <= 12; height++ {
		block := node.BuildLeaderBlock(height)
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		node.Blockchain.AddBlock(block)
	}

	tip := node.Blockchain.Height()
	validators := node.GetConsensusValidators(int(tip))
	localHash := strings.ToLower(strings.TrimSpace(node.validatorSetHashFromFinalizedSnapshot(tip, validators)))
	if localHash == "" {
		t.Fatal("expected local finalized validator-set hash")
	}

	node.setSnapshotWarmupJoinHeight(tip)
	old := time.Now().Add(-2 * syncSnapshotWarmupDuration())
	node.syncMu.Lock()
	node.syncWarmupStartAt = old
	node.syncWarmupLastHeight = tip
	node.syncWarmupLastHeightAt = old
	node.syncWarmupQuorumHash = localHash
	node.syncWarmupQuorumVotes = 1
	node.syncWarmupQuorumSince = old
	node.syncMu.Unlock()

	if node.snapshotWarmupActive(tip) {
		t.Fatal("expected stable local finalized validator-set hash to clear warmup after duration")
	}
	node.syncMu.Lock()
	startAt := node.syncWarmupStartAt
	node.syncMu.Unlock()
	if !startAt.IsZero() {
		t.Fatal("expected completed warmup state to be cleared")
	}

	node.validatorMu.Lock()
	node.validatorStatus["B"] = &ValidatorStatus{
		LastSeen:         time.Now(),
		ReportedHeight:   tip,
		FinalizedHeight:  tip,
		ValidatorSetHash: "late-conflicting-validator-set-hash",
	}
	node.validatorMu.Unlock()
	if node.snapshotWarmupActive(tip) {
		t.Fatal("completed warmup must not reactivate after later validator-set observations change")
	}
}

func TestSnapshotWarmupKeepsBlockingOnConflictingValidatorSetHash(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	for height := uint64(1); height <= 12; height++ {
		block := node.BuildLeaderBlock(height)
		block.BlockTime = LogicalTimeForEpochTick(block.ID, TickFinalize)
		block.Timestamp = int64(SystemTimeUnits(block.BlockTime))
		block.BlockHash = HashBlock(block)
		node.Blockchain.AddBlock(block)
	}

	tip := node.Blockchain.Height()
	validators := node.GetConsensusValidators(int(tip))
	localHash := strings.ToLower(strings.TrimSpace(node.validatorSetHashFromFinalizedSnapshot(tip, validators)))
	if localHash == "" {
		t.Fatal("expected local finalized validator-set hash")
	}

	node.validatorMu.Lock()
	node.validatorStatus["B"] = &ValidatorStatus{
		LastSeen:         time.Now(),
		ReportedHeight:   tip,
		FinalizedHeight:  tip,
		ValidatorSetHash: "conflicting-validator-set-hash",
	}
	node.validatorMu.Unlock()

	node.setSnapshotWarmupJoinHeight(tip)
	old := time.Now().Add(-2 * syncSnapshotWarmupDuration())
	node.syncMu.Lock()
	node.syncWarmupStartAt = old
	node.syncWarmupLastHeight = tip
	node.syncWarmupLastHeightAt = old
	node.syncWarmupQuorumHash = localHash
	node.syncWarmupQuorumVotes = 1
	node.syncWarmupQuorumSince = old
	node.syncMu.Unlock()

	if !node.snapshotWarmupActive(tip) {
		t.Fatal("expected conflicting validator-set hash to keep warmup active")
	}
}

func TestApplyScheduledValidatorUpdatesTraceNonTransitionPath(t *testing.T) {
	withScheduledUpdateTraceGlobals(t, true)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	buf := withScheduledUpdateTraceBuffer(t)
	node.applyScheduledValidatorUpdates(1)
	out := buf.String()

	assertTraceSubstringsInOrder(t, out, []string{
		"step=queue_inactive_removals_begin",
		"step=queue_inactive_removals_done",
		"step=validator_set_lock_begin",
		"step=validator_set_lock_acquired",
		"step=snapshot_epoch_validators_begin",
		"step=snapshot_epoch_validators_done",
	})
}

func TestApplyScheduledValidatorUpdatesTraceDueTransitionPath(t *testing.T) {
	withScheduledUpdateTraceGlobals(t, true)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D", "E"})
	node.queuePendingValidatorRemoval("E", 1)
	buf := withScheduledUpdateTraceBuffer(t)
	node.applyScheduledValidatorUpdates(2)
	out := buf.String()

	if !strings.Contains(out, "step=due_transition_eval_done update_height=2 due_transition=true") {
		t.Fatalf("expected due transition trace, got:\n%s", out)
	}
	assertTraceSubstringsInOrder(t, out, []string{
		"step=scheduled_transition_validators_begin",
		"step=scheduled_transition_validators_done",
		"step=build_next_validators_begin",
		"step=build_next_validators_done",
		"step=transition_quorum_begin",
		"step=transition_quorum_done",
	})
}

func TestApplyScheduledValidatorUpdatesTraceQueuedUpdateFastReturn(t *testing.T) {
	withScheduledUpdateTraceGlobals(t, false)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.queuedValidatorSetUpdates = map[uint64]ValidatorSetUpdate{
		1: {
			Height:     1,
			Validators: []string{"A", "B", "C", "D"},
		},
	}
	buf := withScheduledUpdateTraceBuffer(t)
	node.applyScheduledValidatorUpdates(1)
	out := buf.String()

	assertTraceSubstringsInOrder(t, out, []string{
		"step=apply_queued_set_update_begin",
		"step=apply_queued_set_update_done",
		"step=snapshot_epoch_validators_begin",
		"step=snapshot_epoch_validators_done",
	})
	if strings.Contains(out, "step=validator_set_lock_begin") {
		t.Fatalf("expected queued update fast return before validator_set_lock_begin, got:\n%s", out)
	}
}

func TestApplyScheduledValidatorUpdatesTraceSnapshotSyncDeferPath(t *testing.T) {
	withScheduledUpdateTraceGlobals(t, true)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.queuePendingValidator("F", 1)
	node.snapshotSessionMu.Lock()
	node.snapshotSession.Active = true
	node.snapshotSessionMu.Unlock()
	buf := withScheduledUpdateTraceBuffer(t)
	node.applyScheduledValidatorUpdates(2)
	out := buf.String()

	if !strings.Contains(out, "step=defer_for_sync_eval_done update_height=2 due_transition=true defer_for_sync=true update_blocked=true") {
		t.Fatalf("expected snapshot defer trace, got:\n%s", out)
	}
	assertTraceSubstringsInOrder(t, out, []string{
		"step=defer_for_sync_eval_done",
		"step=onboarding_tracker_begin",
		"step=onboarding_tracker_done",
		"step=snapshot_epoch_validators_begin",
		"step=snapshot_epoch_validators_done",
	})
}

func TestSyncBlocksFromPeersWithBatchClampsRangeFetchWindow(t *testing.T) {
	node, localHost := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()
	connectHosts(t, localHost, remote)

	var streamMu sync.Mutex
	var lastStream *testStream
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			stream := newJSONResponseStream(remote.ID(), BlockResponse{})
			streamMu.Lock()
			lastStream = stream
			streamMu.Unlock()
			return stream, nil
		},
	}
	node.peerStateMu.Lock()
	node.peerToValidator[remote.ID().String()] = "A"
	node.peerStateMu.Unlock()
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{FinalizedHeight: 250, ReportedHeight: 250}
	node.validatorMu.Unlock()

	node.syncBlocksFromPeersWithBatch(250, 200, false)
	streamMu.Lock()
	stream := lastStream
	streamMu.Unlock()
	if stream == nil {
		t.Fatalf("expected request stream to be captured")
	}
	req := decodeCapturedRequest(t, stream)
	if req.From != 1 || req.To != 100 {
		t.Fatalf("expected range fetch request 1-100, got %d-%d", req.From, req.To)
	}
}

func TestSyncBlocksFromPeersWithBatchClampsDeltaReplayWindow(t *testing.T) {
	node, localHost := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()
	connectHosts(t, localHost, remote)

	var streamMu sync.Mutex
	var lastStream *testStream
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			stream := newJSONResponseStream(remote.ID(), BlockResponse{})
			streamMu.Lock()
			lastStream = stream
			streamMu.Unlock()
			return stream, nil
		},
	}
	node.peerStateMu.Lock()
	node.peerToValidator[remote.ID().String()] = "A"
	node.peerStateMu.Unlock()
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{FinalizedHeight: 250, ReportedHeight: 250}
	node.validatorMu.Unlock()

	node.syncBlocksFromPeersWithBatch(250, 1024, true)
	streamMu.Lock()
	stream := lastStream
	streamMu.Unlock()
	if stream == nil {
		t.Fatalf("expected request stream to be captured")
	}
	req := decodeCapturedRequest(t, stream)
	if req.From != 1 || req.To != 100 {
		t.Fatalf("expected delta replay request 1-100, got %d-%d", req.From, req.To)
	}
}

func TestFreshJoinBlockReplayAllowedAfterSnapshotRetriesForLargeGap(t *testing.T) {
	node, _ := newRequestTestNode(t)

	oldFallback := SyncFreshJoinFallbackBlockReplayEnabled
	SyncFreshJoinFallbackBlockReplayEnabled = true
	t.Cleanup(func() {
		SyncFreshJoinFallbackBlockReplayEnabled = oldFallback
	})

	node.snapshotSessionMu.Lock()
	node.snapshotSession = SnapshotSession{
		Active:     true,
		RetryCount: syncSnapshotRetryBudget(),
	}
	node.snapshotSessionMu.Unlock()

	target := uint64(10_000_000)
	if !node.allowFreshNodeBlockCatchup(0, target) {
		t.Fatalf("expected fresh node to allow block replay after snapshot retry budget for target=%d", target)
	}
	if !node.preferBlockRangeCatchup(0, target) {
		t.Fatalf("expected fresh node to prefer block range fallback after snapshot retry budget")
	}
}

func TestSyncRangeAssignmentsChunkHugeGapWithoutOverflow(t *testing.T) {
	peers := []peer.ID{"peer-a", "peer-b", "peer-c", "peer-d", "peer-e"}
	assignments := planSyncRangeAssignments(1, 100_000_000, 100_000_000, peers)
	if len(assignments) == 0 {
		t.Fatal("expected assignments for huge sync gap")
	}
	if len(assignments) > len(peers) {
		t.Fatalf("expected one bounded assignment per peer, got %d peers=%d", len(assignments), len(peers))
	}
	for i, assignment := range assignments {
		if assignment.From == 0 || assignment.To < assignment.From {
			t.Fatalf("assignment %d has invalid range %d-%d", i, assignment.From, assignment.To)
		}
		if got := assignment.To - assignment.From + 1; got > syncRangeAssignmentMaxBlocks {
			t.Fatalf("assignment %d exceeded max chunk: got=%d max=%d", i, got, syncRangeAssignmentMaxBlocks)
		}
		if i > 0 && assignment.From != assignments[i-1].To+1 {
			t.Fatalf("assignments not contiguous at %d: prev=%d next=%d", i, assignments[i-1].To, assignment.From)
		}
	}
}

func TestSyncBlocksFromPeersWithBatchRotatesFromTimedOutProvider(t *testing.T) {
	withSyncPeerTimeoutOverride(t, 20*time.Millisecond)
	node, localHost := newRequestTestNode(t)
	badHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("bad host: %v", err)
	}
	defer badHost.Close()
	goodHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("good host: %v", err)
	}
	defer goodHost.Close()

	connectHosts(t, localHost, badHost)
	connectHosts(t, localHost, goodHost)

	var goodStreamMu sync.Mutex
	var goodStream *testStream
	badBlocker := make(chan struct{})
	var openMu sync.Mutex
	openOrder := make([]peer.ID, 0, 2)
	node.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			openMu.Lock()
			openOrder = append(openOrder, pid)
			openMu.Unlock()
			switch pid {
			case badHost.ID():
				<-badBlocker
				return nil, nil
			case goodHost.ID():
				stream := newJSONResponseStream(goodHost.ID(), BlockResponse{})
				goodStreamMu.Lock()
				goodStream = stream
				goodStreamMu.Unlock()
				return stream, nil
			default:
				return nil, errors.New("unexpected peer")
			}
		},
	}
	t.Cleanup(func() {
		close(badBlocker)
	})

	node.peerStateMu.Lock()
	node.peerToValidator[badHost.ID().String()] = "A"
	node.peerToValidator[goodHost.ID().String()] = "B"
	node.peerStateMu.Unlock()
	node.validatorMu.Lock()
	node.validatorStatus["A"] = &ValidatorStatus{FinalizedHeight: 10, ReportedHeight: 10}
	node.validatorStatus["B"] = &ValidatorStatus{FinalizedHeight: 3, ReportedHeight: 3}
	node.validatorMu.Unlock()

	if ok := node.syncBlocksFromPeersWithBatch(3, 200, false); ok {
		t.Fatalf("expected sync to stop after healthy peer returned no blocks")
	}
	openMu.Lock()
	if len(openOrder) < 2 {
		t.Fatalf("expected both bad and good peers to be attempted, got %d opens", len(openOrder))
	}
	if openOrder[0] != badHost.ID() || openOrder[1] != goodHost.ID() {
		t.Fatalf("expected bad peer then good peer, got %v", openOrder)
	}
	openMu.Unlock()
	goodStreamMu.Lock()
	stream := goodStream
	goodStreamMu.Unlock()
	if stream == nil {
		t.Fatalf("expected healthy peer stream to be captured")
	}
	req := decodeCapturedRequest(t, stream)
	if req.From != 1 || req.To != 3 {
		t.Fatalf("expected healthy peer request 1-3, got %d-%d", req.From, req.To)
	}
}
