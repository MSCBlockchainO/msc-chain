package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// `walletEventsUpgrader` stores the value used by this operation.
var walletEventsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type walletEvent struct {
	// `Type` stores the value associated with this record.
	Type                 string                 `json:"type"`
	// `Height` stores the value associated with this record.
	Height               uint64                 `json:"height,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight      uint64                 `json:"finalized_height,omitempty"`
	// `Hash` stores the digest used to identify or verify the related data.
	Hash                 string                 `json:"hash,omitempty"`
	// `Proposer` stores the value associated with this record.
	Proposer             string                 `json:"proposer,omitempty"`
	// `BlockType` stores the block data handled by this operation.
	BlockType            string                 `json:"block_type,omitempty"`
	// `TxCount` stores the transaction data handled by this operation.
	TxCount              int                    `json:"tx_count,omitempty"`
	// `ExecutionResultCount` stores the measured quantity used by this operation.
	ExecutionResultCount int                    `json:"execution_result_count,omitempty"`
	// `Mode` stores the value associated with this record.
	Mode                 string                 `json:"mode,omitempty"`
	// `Reason` stores the value associated with this record.
	Reason               string                 `json:"reason,omitempty"`
	// `FinalityLag` stores the value associated with this record.
	FinalityLag          uint64                 `json:"finality_lag,omitempty"`
	// `LastBlockAgeSeconds` stores the value associated with this record.
	LastBlockAgeSeconds  uint64                 `json:"last_block_age_seconds"`
	// `PeerCount` stores the measured quantity used by this operation.
	PeerCount            int                    `json:"peer_count,omitempty"`
	// `ActiveValidators` stores the value associated with this record.
	ActiveValidators     int                    `json:"active_validators,omitempty"`
	// `TotalValidators` stores the measured quantity used by this operation.
	TotalValidators      int                    `json:"total_validators,omitempty"`
	// `Quorum` stores the value associated with this record.
	Quorum               int                    `json:"quorum,omitempty"`
	// `NetworkHealth` stores the value associated with this record.
	NetworkHealth        string                 `json:"network_health,omitempty"`
	// `TS` stores the value associated with this record.
	TS                   int64                  `json:"ts"`
	// `PublicNodesTotal` stores the measured quantity used by this operation.
	PublicNodesTotal     int                    `json:"public_nodes_total,omitempty"`
	// `PublicNodesHealthy` stores the value associated with this record.
	PublicNodesHealthy   int                    `json:"public_nodes_healthy,omitempty"`
	// `PublicNodesBest` stores the value associated with this record.
	PublicNodesBest      string                 `json:"public_nodes_best,omitempty"`
	// `PublicNodes` stores the value associated with this record.
	PublicNodes          []publicNodeHealthView `json:"public_nodes,omitempty"`
	// `TSMS` stores the value associated with this record.
	TSMS                 int64                  `json:"ts_ms,omitempty"`
}

// `walletEventSequentialBlockLimit` defines the constant value used by this package.
const walletEventSequentialBlockLimit uint64 = 64

// walletEventHeights implements the wallet event heights helper.
func walletEventHeights(previous, current uint64) []uint64 {
	if current <= previous {
		return []uint64{current}
	}
	// `gap` stores the value produced by this operation.
	gap := current - previous
	if gap > walletEventSequentialBlockLimit {
		return []uint64{current}
	}
	// `heights` stores the value produced by this operation.
	heights := make([]uint64, 0, gap)
	// `height` stores the value produced by this operation.
	for height := previous + 1; height <= current; height++ {
		heights = append(heights, height)
	}
	return heights
}

// handleWalletEvents handles wallet events.
func (s *Server) handleWalletEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}
	// `conn` and `err` store the error produced by this operation.
	conn, err := walletEventsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// `done` stores the value produced by this operation.
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.SetReadLimit(1024)
		for {
			// `err` stores the error produced by this operation.
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// `runtime` stores the value produced by this operation.
	runtime := s.Node.runtimeStatusSnapshot()
	// `err` stores the error produced by this operation.
	if err := walletEventsWrite(conn, walletEventFromRuntime("hello", runtime, s.Node)); err != nil {
		return
	}

	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// `pingTicker` stores the value produced by this operation.
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	// `lastHeight` stores the value produced by this operation.
	lastHeight := runtime.Height
	// `lastFinalized` stores the value produced by this operation.
	lastFinalized := runtime.FinalizedHeight
	// `lastMode` stores the value produced by this operation.
	lastMode := runtime.ConsensusDetectorMode
	if strings.TrimSpace(lastMode) == "" {
		lastMode = runtime.ConsensusMode
	}
	// `lastValidatorState` stores the value produced by this operation.
	lastValidatorState := walletEventValidatorState(runtime, s.Node)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case <-pingTicker.C:
			// `err` stores the error produced by this operation.
			if err := walletEventsWrite(conn, walletEvent{Type: "ping", TS: time.Now().Unix()}); err != nil {
				return
			}
		case <-ticker.C:
			runtime = s.Node.runtimeStatusSnapshot()
			// `mode` stores the value produced by this operation.
			mode := strings.TrimSpace(runtime.ConsensusDetectorMode)
			if mode == "" {
				mode = runtime.ConsensusMode
			}
			if runtime.Height != lastHeight {
				// `previousHeight` stores the value produced by this operation.
				previousHeight := lastHeight
				lastHeight = runtime.Height
				// `height` tracks the current values while iterating.
				for _, height := range walletEventHeights(previousHeight, runtime.Height) {
					// `eventRuntime` stores the value produced by this operation.
					eventRuntime := runtime
					eventRuntime.Height = height
					if height < runtime.Height {
						eventRuntime.LastBlockAgeSeconds = 0
					}
					// `err` stores the error produced by this operation.
					if err := walletEventsWrite(conn, walletEventFromRuntime("new_block", eventRuntime, s.Node)); err != nil {
						return
					}
				}
			}
			if runtime.FinalizedHeight != lastFinalized {
				lastFinalized = runtime.FinalizedHeight
				// `err` stores the error produced by this operation.
				if err := walletEventsWrite(conn, walletEventFromRuntime("finality_update", runtime, s.Node)); err != nil {
					return
				}
			}
			if mode != lastMode {
				lastMode = mode
				// `err` stores the error produced by this operation.
				if err := walletEventsWrite(conn, walletEventFromRuntime("consensus_mode", runtime, s.Node)); err != nil {
					return
				}
			}
			// `validatorState` stores whether the related condition is satisfied.
			validatorState := walletEventValidatorState(runtime, s.Node)
			if validatorState != lastValidatorState {
				lastValidatorState = validatorState
				// `err` stores the error produced by this operation.
				if err := walletEventsWrite(conn, walletEventFromRuntime("validator_update", runtime, s.Node)); err != nil {
					return
				}
			}
		}
	}
}

// walletEventsWrite implements the wallet events write helper.
func walletEventsWrite(conn *websocket.Conn, event walletEvent) error {
	if event.TS == 0 {
		event.TS = time.Now().Unix()
	}
	if event.TSMS == 0 {
		event.TSMS = time.Now().UnixMilli()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(event)
}

// walletEventFromRuntime implements the wallet event from runtime helper.
func walletEventFromRuntime(kind string, runtime RuntimeStatusSnapshot, node *Node) walletEvent {
	// `mode` stores the value produced by this operation.
	mode := strings.TrimSpace(runtime.ConsensusDetectorMode)
	if mode == "" {
		mode = runtime.ConsensusMode
	}
	// `totalValidators` stores the measured quantity used by this operation.
	totalValidators := 0
	if node != nil {
		totalValidators = len(node.GetConsensusValidators(int(runtime.Height + 1)))
	}
	if totalValidators == 0 {
		totalValidators = runtime.LiveValidators
	}
	// `blockInfo` stores the block data handled by this operation.
	blockInfo := walletEventBlockInfo(node, runtime.Height)
	// `event` stores the value produced by this operation.
	event := walletEvent{
		Type:                 kind,
		Height:               runtime.Height,
		FinalizedHeight:      runtime.FinalizedHeight,
		Hash:                 blockInfo.Hash,
		Proposer:             blockInfo.Proposer,
		BlockType:            blockInfo.BlockType,
		TxCount:              blockInfo.TxCount,
		ExecutionResultCount: blockInfo.ExecutionResultCount,
		Mode:                 mode,
		Reason:               runtime.ConsensusDetectorReason,
		FinalityLag:          runtime.ConsensusDetectorFinalityLagBlocks,
		LastBlockAgeSeconds:  runtime.LastBlockAgeSeconds,
		PeerCount:            runtime.Peers,
		ActiveValidators:     runtime.LiveValidators,
		TotalValidators:      totalValidators,
		Quorum:               runtime.RequiredQuorum,
		NetworkHealth:        runtime.NetworkHealth,
		TS:                   time.Now().Unix(),
		TSMS:                 time.Now().UnixMilli(),
	}
	// `publicNodes` stores the value produced by this operation.
	publicNodes := publicNodesSnapshot(node, false)
	event.PublicNodesTotal = publicNodes.Total
	event.PublicNodesHealthy = publicNodes.Healthy
	event.PublicNodesBest = publicNodes.Best
	event.PublicNodes = publicNodes.Nodes
	return event
}

// walletEventValidatorState implements the wallet event validator state helper.
func walletEventValidatorState(runtime RuntimeStatusSnapshot, node *Node) string {
	// `total` stores the measured quantity used by this operation.
	total := 0
	if node != nil {
		total = len(node.GetConsensusValidators(int(runtime.Height + 1)))
	}
	return strings.Join([]string{
		runtime.ValidatorState,
		runtime.OnboardingState,
		runtime.ActivationBlockerReason,
		strings.TrimSpace(runtime.ResolvedVsetHash),
		strings.TrimSpace(runtime.ExpectedVsetHash),
		strings.TrimSpace(runtime.ExpectedNextVsetHash),
		strconv.Itoa(runtime.LiveValidators),
		strconv.Itoa(runtime.ActiveReadyCount),
		strconv.Itoa(runtime.RequiredQuorum),
		strconv.Itoa(total),
	}, "|")
}

type walletEventBlockMetadata struct {
	// `Hash` stores the digest used to identify or verify the related data.
	Hash                 string
	// `Proposer` stores the value associated with this record.
	Proposer             string
	// `BlockType` stores the block data handled by this operation.
	BlockType            string
	// `TxCount` stores the transaction data handled by this operation.
	TxCount              int
	// `ExecutionResultCount` stores the measured quantity used by this operation.
	ExecutionResultCount int
}

// walletEventBlockInfo implements the wallet event block info helper.
func walletEventBlockInfo(node *Node, height uint64) walletEventBlockMetadata {
	if node == nil || node.Blockchain == nil || height == 0 {
		return walletEventBlockMetadata{}
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := node.Blockchain.GetBlock(height)
	if !ok {
		return walletEventBlockMetadata{}
	}
	// `info` stores the current position in the related collection.
	info := walletEventBlockMetadata{
		Proposer:             strings.TrimSpace(block.Proposer),
		BlockType:            strings.TrimSpace(block.Type.String()),
		TxCount:              len(block.Transactions),
		ExecutionResultCount: len(block.ExecutionResults),
	}
	switch {
	case strings.TrimSpace(block.BlockHash) != "":
		info.Hash = strings.TrimSpace(block.BlockHash)
	case strings.TrimSpace(block.Hash) != "":
		info.Hash = strings.TrimSpace(block.Hash)
	}
	return info
}

// walletEventBlockHash implements the wallet event block hash helper.
func walletEventBlockHash(node *Node, height uint64) string {
	return walletEventBlockInfo(node, height).Hash
}
