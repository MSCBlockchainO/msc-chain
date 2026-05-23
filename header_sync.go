package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func syncHeaderBatchSize() uint64 {
	if SyncHeaderBatchSize == 0 {
		return 512
	}
	return SyncHeaderBatchSize
}

func syncHeaderCommonAncestorDepth() uint64 {
	if SyncHeaderCommonAncestorDepth == 0 {
		return 2048
	}
	return SyncHeaderCommonAncestorDepth
}

func buildSyncBlockHeader(block Block) SyncBlockHeader {
	return SyncBlockHeader{
		Height:               block.ID,
		BlockHash:            strings.TrimSpace(block.BlockHash),
		PrevHash:             strings.TrimSpace(block.PrevHash),
		StateRoot:            strings.TrimSpace(block.StateRoot),
		ValidatorSetHash:     strings.TrimSpace(block.ValidatorSetHash),
		NextValidatorSetHash: strings.TrimSpace(block.NextValidatorSetHash),
		Timestamp:            block.Timestamp,
	}
}

func syncHeaderBatchesEqual(left []SyncBlockHeader, right []SyncBlockHeader) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx].Height != right[idx].Height {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(left[idx].BlockHash), strings.TrimSpace(right[idx].BlockHash)) {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(left[idx].PrevHash), strings.TrimSpace(right[idx].PrevHash)) {
			return false
		}
	}
	return true
}

func validateSyncHeaderBatch(start uint64, headers []SyncBlockHeader, expectedPrevHash string) error {
	if start == 0 || len(headers) == 0 {
		return fmt.Errorf("header_batch_empty")
	}
	for idx, header := range headers {
		expectedHeight := start + uint64(idx)
		switch {
		case header.Height != expectedHeight:
			return fmt.Errorf("header_height_mismatch index=%d got=%d want=%d", idx, header.Height, expectedHeight)
		case strings.TrimSpace(header.BlockHash) == "":
			return fmt.Errorf("header_missing_block_hash index=%d height=%d", idx, header.Height)
		case strings.TrimSpace(header.PrevHash) == "":
			return fmt.Errorf("header_missing_prev_hash index=%d height=%d", idx, header.Height)
		case idx == 0 && strings.TrimSpace(expectedPrevHash) != "" && !strings.EqualFold(strings.TrimSpace(header.PrevHash), strings.TrimSpace(expectedPrevHash)):
			return fmt.Errorf("header_first_prev_hash_mismatch got=%s want=%s", ShortHash(header.PrevHash), ShortHash(expectedPrevHash))
		case idx > 0 && !strings.EqualFold(strings.TrimSpace(header.PrevHash), strings.TrimSpace(headers[idx-1].BlockHash)):
			return fmt.Errorf("header_prev_hash_mismatch index=%d height=%d", idx, header.Height)
		}
	}
	return nil
}

func (n *Node) requestHeadersFromPeer(pid peer.ID, from uint64, to uint64) ([]SyncBlockHeader, error) {
	if n.Host == nil {
		return nil, fmt.Errorf("host not initialized")
	}
	timeout := syncPeerRequestTimeout()
	openCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type openResult struct {
		stream network.Stream
		err    error
	}
	openCh := make(chan openResult, 1)
	go func() {
		stream, err := n.openStream(openCtx, pid, HeaderSyncProtocol)
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
			return nil, out.err
		}
		if out.stream == nil {
			return nil, fmt.Errorf("header sync open returned nil stream")
		}
		s = out.stream
	case <-openCtx.Done():
		return nil, openCtx.Err()
	}
	defer s.Close()

	_ = s.SetDeadline(time.Now().Add(timeout))
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)
	req := HeaderSyncRequest{From: from, To: to}
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	var resp HeaderSyncResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	if len(resp.Headers) == 0 {
		return nil, fmt.Errorf("empty header response")
	}
	return resp.Headers, nil
}

func (n *Node) requestCommonAncestorFromPeer(pid peer.ID, locators []HeaderSyncLocator) (uint64, string, error) {
	if n.Host == nil {
		return 0, "", fmt.Errorf("host not initialized")
	}
	if len(locators) == 0 {
		return 0, "", fmt.Errorf("common ancestor locators unavailable")
	}
	timeout := syncPeerRequestTimeout()
	openCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type openResult struct {
		stream network.Stream
		err    error
	}
	openCh := make(chan openResult, 1)
	go func() {
		stream, err := n.openStream(openCtx, pid, HeaderSyncProtocol)
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
			return 0, "", out.err
		}
		if out.stream == nil {
			return 0, "", fmt.Errorf("header sync open returned nil stream")
		}
		s = out.stream
	case <-openCtx.Done():
		return 0, "", openCtx.Err()
	}
	defer s.Close()

	_ = s.SetDeadline(time.Now().Add(timeout))
	enc := json.NewEncoder(s)
	dec := json.NewDecoder(s)
	req := HeaderSyncRequest{Locators: locators}
	if err := enc.Encode(req); err != nil {
		return 0, "", err
	}
	var resp HeaderSyncResponse
	if err := dec.Decode(&resp); err != nil {
		return 0, "", err
	}
	if resp.CommonHeight == 0 || strings.TrimSpace(resp.CommonHash) == "" {
		return 0, "", fmt.Errorf("common ancestor unavailable")
	}
	return resp.CommonHeight, strings.TrimSpace(resp.CommonHash), nil
}

func (n *Node) handleHeaderSyncStream(s network.Stream) {
	defer s.Close()
	timeout := syncPeerRequestTimeout()
	_ = s.SetDeadline(time.Now().Add(timeout))
	dec := json.NewDecoder(s)
	enc := json.NewEncoder(s)

	var req HeaderSyncRequest
	if err := dec.Decode(&req); err != nil {
		return
	}

	if len(req.Locators) > 0 {
		for _, locator := range req.Locators {
			if locator.Height == 0 || strings.TrimSpace(locator.BlockHash) == "" {
				continue
			}
			block, ok := n.LoadBlock(int(locator.Height))
			if !ok {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(block.BlockHash), strings.TrimSpace(locator.BlockHash)) {
				continue
			}
			_ = enc.Encode(HeaderSyncResponse{
				CommonHeight: locator.Height,
				CommonHash:   strings.TrimSpace(block.BlockHash),
			})
			return
		}
		_ = enc.Encode(HeaderSyncResponse{})
		return
	}

	if req.From == 0 || req.To < req.From {
		_ = enc.Encode(HeaderSyncResponse{})
		return
	}
	maxHeaders := syncHeaderBatchSize()
	if maxHeaders == 0 {
		maxHeaders = 256
	}
	if span := req.To - req.From + 1; span > maxHeaders {
		req.To = req.From + maxHeaders - 1
	}
	headers := make([]SyncBlockHeader, 0, int(req.To-req.From+1))
	for height := req.From; height <= req.To; height++ {
		block, ok := n.LoadBlock(int(height))
		if !ok {
			break
		}
		headers = append(headers, buildSyncBlockHeader(block))
	}
	_ = enc.Encode(HeaderSyncResponse{Headers: headers})
}
