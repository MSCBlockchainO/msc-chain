package main

import (
	"context"
	"encoding/json"
	"testing"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func withSyncReplicationGlobalsIsolated(t *testing.T) {
	t.Helper()
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
	})
}

func installHeaderRangeStreamHandler(t *testing.T, h host.Host, blocks map[uint64]Block) {
	t.Helper()
	h.SetStreamHandler(HeaderSyncProtocol, func(s network.Stream) {
		defer s.Close()
		dec := json.NewDecoder(s)
		enc := json.NewEncoder(s)
		var req HeaderSyncRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		if len(req.Locators) > 0 {
			for _, locator := range req.Locators {
				block, ok := blocks[locator.Height]
				if !ok {
					continue
				}
				if block.BlockHash != locator.BlockHash {
					continue
				}
				_ = enc.Encode(HeaderSyncResponse{
					CommonHeight: locator.Height,
					CommonHash:   locator.BlockHash,
				})
				return
			}
			_ = enc.Encode(HeaderSyncResponse{})
			return
		}
		headers := make([]SyncBlockHeader, 0, int(req.To-req.From+1))
		for height := req.From; height <= req.To; height++ {
			block, ok := blocks[height]
			if !ok {
				break
			}
			headers = append(headers, buildSyncBlockHeader(block))
		}
		_ = enc.Encode(HeaderSyncResponse{Headers: headers})
	})
}

func TestPlanReplicatedSyncRangeAssignmentsAddsReplicaPeers(t *testing.T) {
	withSyncReplicationGlobalsIsolated(t)
	peers := []peer.ID{"peer-a", "peer-b", "peer-c", "peer-d"}
	assignments := planReplicatedSyncRangeAssignments(1, 400, 256, peers, 2)
	if len(assignments) != 3 {
		t.Fatalf("expected 3 replicated assignments, got %d", len(assignments))
	}
	for idx, assignment := range assignments {
		if len(assignment.Peers) != 2 {
			t.Fatalf("assignment %d expected 2 peers, got %d", idx, len(assignment.Peers))
		}
		if assignment.From == 0 || assignment.To < assignment.From {
			t.Fatalf("assignment %d has invalid range %d-%d", idx, assignment.From, assignment.To)
		}
	}
}

func TestRequestReplicatedBlockRangeFallsBackWhenHeaderProtocolUnavailable(t *testing.T) {
	withSyncReplicationGlobalsIsolated(t)
	requester, _ := newRequestTestNode(t)
	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()

	blocks := buildSyncTestBlocks(t, 2)
	requester.streamManager = testStreamOpener{
		openFn: func(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
			_ = ctx
			_ = pid
			if proto == BlockSyncProtocol {
				return newJSONResponseStream(remote.ID(), BlockResponse{Blocks: blocks}), nil
			}
			return newJSONResponseStream(remote.ID(), BlockResponse{}), nil
		},
	}

	assign := syncReplicatedRangeAssignment{
		From:  1,
		To:    2,
		Peers: []peer.ID{remote.ID()},
	}
	merged, failedPeers, err := requester.requestReplicatedBlockRange(assign)
	if err != nil {
		t.Fatalf("replicated block range fallback failed: %v", err)
	}
	if len(failedPeers) != 0 {
		t.Fatalf("expected no failed peers, got %d", len(failedPeers))
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 blocks after fallback, got %d", len(merged))
	}
}

func TestRequestHeadersAndCommonAncestorFromPeer(t *testing.T) {
	withSyncReplicationGlobalsIsolated(t)
	requester, localHost := newRequestTestNode(t)
	blocks := buildSyncTestBlocks(t, 4)
	blocksByHeight := make(map[uint64]Block, len(blocks))
	for _, block := range blocks {
		blocksByHeight[block.ID] = block
	}

	remote, err := libp2p.New()
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	defer remote.Close()
	installHeaderRangeStreamHandler(t, remote, blocksByHeight)
	connectHosts(t, localHost, remote)

	headers, err := requester.requestHeadersFromPeer(remote.ID(), 1, 3)
	if err != nil {
		t.Fatalf("request headers failed: %v", err)
	}
	if len(headers) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(headers))
	}

	locators := []HeaderSyncLocator{
		{Height: 4, BlockHash: "deadbeef"},
		{Height: 2, BlockHash: blocksByHeight[2].BlockHash},
	}
	commonHeight, commonHash, err := requester.requestCommonAncestorFromPeer(remote.ID(), locators)
	if err != nil {
		t.Fatalf("request common ancestor failed: %v", err)
	}
	if commonHeight != 2 || commonHash != blocksByHeight[2].BlockHash {
		t.Fatalf("unexpected common ancestor got=%d/%s", commonHeight, commonHash)
	}
}
