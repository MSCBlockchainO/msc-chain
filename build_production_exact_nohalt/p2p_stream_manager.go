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
	host host.Host
}

func NewStreamManager(h host.Host) *StreamManager {
	if h == nil {
		return nil
	}
	return &StreamManager{host: h}
}

func (m *StreamManager) Open(ctx context.Context, pid peer.ID, proto protocol.ID) (network.Stream, error) {
	if m == nil || m.host == nil {
		return nil, errors.New("stream_manager_unavailable")
	}
	return m.host.NewStream(ctx, pid, proto)
}

func (n *Node) openStream(ctx context.Context, pid peer.ID, proto string) (network.Stream, error) {
	if n == nil || n.Host == nil {
		return nil, errors.New("host_unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opener := n.streamManager
	if opener == nil {
		opener = NewStreamManager(n.Host)
	}
	type openResult struct {
		stream network.Stream
		err    error
	}
	resultCh := make(chan openResult, 1)
	go func() {
		stream, err := opener.Open(ctx, pid, protocol.ID(proto))
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
