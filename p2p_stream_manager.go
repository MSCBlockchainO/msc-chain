package main

import (
	"context"
	"errors"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type streamOpener interface {
	Open(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error)
}

type StreamManager struct {
	// `host` stores the value associated with this record.
	host host.Host
}

// NewStreamManager creates a new stream manager.
func NewStreamManager(h host.Host) *StreamManager {
	if h == nil {
		return nil
	}
	return &StreamManager{host: h}
}

// Open implements the open helper.
func (m *StreamManager) Open(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
	if m == nil || m.host == nil {
		return nil, errors.New("stream_manager_unavailable")
	}
	return m.host.NewStream(ctx, pid, proto)
}

// openStream implements the open stream helper.
func (n *Node) openStream(ctx context.Context, pid peer.ID, proto string) (network.Stream, error) {
	if n == nil || n.Host == nil {
		return nil, errors.New("host_unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// `opener` stores the value produced by this operation.
	opener := n.streamManager
	if opener == nil {
		opener = NewStreamManager(n.Host)
	}
	type openResult struct {
		// `stream` stores the value associated with this record.
		stream network.Stream
		// `err` stores the error produced by this operation.
		err    error
	}
	// `resultCh` stores the result produced by this operation.
	resultCh := make(chan openResult, 1)
	go func() {
		// `stream` and `err` store the error produced by this operation.
		stream, err := opener.Open(ctx, pid, protocol.ID(proto))
		// `result` stores the result produced by this operation.
		result := openResult{stream: stream, err: err}
		select {
		case resultCh <- result:
		case <-ctx.Done():
			if stream != nil {
				_ = stream.Reset()
			}
		}
	}()
	select {
	case result := <-resultCh:
		return result.stream, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
