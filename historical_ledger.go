package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

func (s *Server) rpcBlockSnapshot() []Block {
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return nil
	}
	s.Node.Blockchain.mu.RLock()
	defer s.Node.Blockchain.mu.RUnlock()
	return append([]Block(nil), s.Node.Blockchain.Blocks...)
}

func findBlockByHeight(blocks []Block, height uint64) (Block, bool) {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].ID == height {
			return blocks[i], true
		}
	}
	return Block{}, false
}

func (s *Server) latestChainHeight() uint64 {
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return 0
	}
	return s.Node.Blockchain.Height()
}

func parseNativeRPCQuantity(raw json.RawMessage) (uint64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0, errors.New("missing quantity")
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		text = strings.TrimSpace(text)
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			base = 16
			text = text[2:]
		}
		if text == "" {
			return 0, errors.New("invalid quantity")
		}
		return strconv.ParseUint(text, base, 64)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, errors.New("invalid quantity")
	}
	return strconv.ParseUint(number.String(), 10, 64)
}

func (s *Server) resolveBlockByTag(raw json.RawMessage) (Block, bool, error) {
	blocks := s.rpcBlockSnapshot()
	if len(blocks) == 0 {
		return Block{}, false, nil
	}
	latest := blocks[len(blocks)-1]
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return latest, true, nil
	}
	var tag string
	if err := json.Unmarshal(trimmed, &tag); err == nil {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "", "latest", "pending", "safe":
			return latest, true, nil
		case "earliest":
			return blocks[0], true, nil
		case "finalized":
			height := s.explorerAvailableFinalizedHeight()
			if height == 0 {
				return latest, true, nil
			}
			block, ok := findBlockByHeight(blocks, height)
			return block, ok, nil
		}
	}
	height, err := parseNativeRPCQuantity(trimmed)
	if err != nil {
		return Block{}, false, err
	}
	block, ok := findBlockByHeight(blocks, height)
	return block, ok, nil
}

func (s *Server) ledgerAtHeight(targetHeight uint64) (Ledger, bool) {
	if s == nil || s.Node == nil {
		return Ledger{}, false
	}
	latest := s.latestChainHeight()
	if targetHeight > latest {
		return Ledger{}, false
	}
	if targetHeight > 0 {
		if committed, ok := s.Node.committedTipLedgerFromExecutionSnapshot(targetHeight); ok {
			return committed, true
		}
	}
	if targetHeight == latest {
		return s.Node.Ledger.Clone(), true
	}
	ledger := GenesisLedger()
	startHeight := uint64(1)
	if snap, err := s.Node.GetSnapshotAtOrBelow(targetHeight); err == nil && snap != nil {
		ledger = snap.Ledger.Clone()
		if snap.Height > 0 {
			if committed, ok := s.Node.committedTipLedgerFromExecutionSnapshot(snap.Height); ok {
				ledger = committed
			}
		}
		if snap.Height >= targetHeight {
			return ledger, true
		}
		startHeight = snap.Height + 1
	}
	blocks := s.rpcBlockSnapshot()
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID < blocks[j].ID })
	for _, block := range blocks {
		if block.ID < startHeight || block.ID > targetHeight {
			continue
		}
		next, err := ApplyBlockStateWithNode(s.Node, ledger, block)
		if err != nil {
			return Ledger{}, false
		}
		nextHeight := uint64(0)
		if block.ID < targetHeight {
			nextHeight = block.ID + 1
		}
		ledger = s.Node.startupExecutionParentLedgerAfterBlock(block, next, nextHeight)
	}
	return ledger, true
}

func (s *Server) pendingLedger() (Ledger, uint64, bool) {
	latest := s.latestChainHeight()
	ledger, ok := s.ledgerAtHeight(latest)
	if !ok || s == nil || s.Node == nil {
		return Ledger{}, 0, false
	}
	s.Node.Mempool.mu.Lock()
	pending := append([]Transaction(nil), s.Node.Mempool.Transactions...)
	s.Node.Mempool.mu.Unlock()
	for _, tx := range pending {
		next, err := ExecuteTransaction(&ledger, tx, int(latest+1))
		if err == nil {
			ledger = next
		}
	}
	return ledger, latest + 1, true
}

func (s *Server) resolveLedgerByTag(raw json.RawMessage) (Ledger, uint64, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		height := s.latestChainHeight()
		ledger, ok := s.ledgerAtHeight(height)
		return ledger, height, ok, nil
	}
	var tag string
	if err := json.Unmarshal(trimmed, &tag); err == nil && strings.EqualFold(strings.TrimSpace(tag), "pending") {
		ledger, height, ok := s.pendingLedger()
		return ledger, height, ok, nil
	}
	block, found, err := s.resolveBlockByTag(trimmed)
	if err != nil || !found {
		return Ledger{}, 0, false, err
	}
	ledger, ok := s.ledgerAtHeight(block.ID)
	return ledger, block.ID, ok, nil
}
