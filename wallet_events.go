package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var walletEventsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type walletEvent struct {
	Type                string `json:"type"`
	Height              uint64 `json:"height,omitempty"`
	FinalizedHeight     uint64 `json:"finalized_height,omitempty"`
	Hash                string `json:"hash,omitempty"`
	Mode                string `json:"mode,omitempty"`
	Reason              string `json:"reason,omitempty"`
	FinalityLag         uint64 `json:"finality_lag,omitempty"`
	LastBlockAgeSeconds uint64 `json:"last_block_age_seconds"`
	PeerCount           int    `json:"peer_count,omitempty"`
	ActiveValidators    int    `json:"active_validators,omitempty"`
	TotalValidators     int    `json:"total_validators,omitempty"`
	Quorum              int    `json:"quorum,omitempty"`
	NetworkHealth       string `json:"network_health,omitempty"`
	TS                  int64  `json:"ts"`
}

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
	conn, err := walletEventsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.SetReadLimit(1024)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	runtime := s.Node.runtimeStatusSnapshot()
	if err := walletEventsWrite(conn, walletEventFromRuntime("hello", runtime, s.Node)); err != nil {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	lastHeight := runtime.Height
	lastFinalized := runtime.FinalizedHeight
	lastMode := runtime.ConsensusDetectorMode
	if strings.TrimSpace(lastMode) == "" {
		lastMode = runtime.ConsensusMode
	}
	lastValidatorState := walletEventValidatorState(runtime, s.Node)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case <-pingTicker.C:
			if err := walletEventsWrite(conn, walletEvent{Type: "ping", TS: time.Now().Unix()}); err != nil {
				return
			}
		case <-ticker.C:
			runtime = s.Node.runtimeStatusSnapshot()
			mode := strings.TrimSpace(runtime.ConsensusDetectorMode)
			if mode == "" {
				mode = runtime.ConsensusMode
			}
			if runtime.Height != lastHeight {
				lastHeight = runtime.Height
				if err := walletEventsWrite(conn, walletEventFromRuntime("new_block", runtime, s.Node)); err != nil {
					return
				}
			}
			if runtime.FinalizedHeight != lastFinalized {
				lastFinalized = runtime.FinalizedHeight
				if err := walletEventsWrite(conn, walletEventFromRuntime("finality_update", runtime, s.Node)); err != nil {
					return
				}
			}
			if mode != lastMode {
				lastMode = mode
				if err := walletEventsWrite(conn, walletEventFromRuntime("consensus_mode", runtime, s.Node)); err != nil {
					return
				}
			}
			validatorState := walletEventValidatorState(runtime, s.Node)
			if validatorState != lastValidatorState {
				lastValidatorState = validatorState
				if err := walletEventsWrite(conn, walletEventFromRuntime("validator_update", runtime, s.Node)); err != nil {
					return
				}
			}
		}
	}
}

func walletEventsWrite(conn *websocket.Conn, event walletEvent) error {
	if event.TS == 0 {
		event.TS = time.Now().Unix()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(event)
}

func walletEventFromRuntime(kind string, runtime RuntimeStatusSnapshot, node *Node) walletEvent {
	mode := strings.TrimSpace(runtime.ConsensusDetectorMode)
	if mode == "" {
		mode = runtime.ConsensusMode
	}
	totalValidators := 0
	if node != nil {
		totalValidators = len(node.GetConsensusValidators(int(runtime.Height + 1)))
	}
	if totalValidators == 0 {
		totalValidators = runtime.LiveValidators
	}
	return walletEvent{
		Type:                kind,
		Height:              runtime.Height,
		FinalizedHeight:     runtime.FinalizedHeight,
		Hash:                walletEventBlockHash(node, runtime.Height),
		Mode:                mode,
		Reason:              runtime.ConsensusDetectorReason,
		FinalityLag:         runtime.ConsensusDetectorFinalityLagBlocks,
		LastBlockAgeSeconds: runtime.LastBlockAgeSeconds,
		PeerCount:           runtime.Peers,
		ActiveValidators:    runtime.LiveValidators,
		TotalValidators:     totalValidators,
		Quorum:              runtime.RequiredQuorum,
		NetworkHealth:       runtime.NetworkHealth,
		TS:                  time.Now().Unix(),
	}
}

func walletEventValidatorState(runtime RuntimeStatusSnapshot, node *Node) string {
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

func walletEventBlockHash(node *Node, height uint64) string {
	if node == nil || node.Blockchain == nil || height == 0 {
		return ""
	}
	block, ok := node.Blockchain.GetBlock(height)
	if !ok {
		return ""
	}
	switch {
	case strings.TrimSpace(block.BlockHash) != "":
		return strings.TrimSpace(block.BlockHash)
	case strings.TrimSpace(block.Hash) != "":
		return strings.TrimSpace(block.Hash)
	default:
		return ""
	}
}
