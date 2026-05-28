package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDetectConsensusModePriorityAndModes(t *testing.T) {
	tests := []struct {
		name string
		in   ConsensusDetectorMetrics
		want ConsensusDetectorMode
	}{
		{
			name: "attack wins",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 4,
				Quorum:           3,
				DoubleSign:       true,
			},
			want: ConsensusDetectorAttack,
		},
		{
			name: "halted on finality timeout",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 4,
				Quorum:           3,
				LastFinalitySec:  61,
			},
			want: ConsensusDetectorHalted,
		},
		{
			name: "partition risk",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 4,
				Quorum:           3,
				PartitionRisk:    true,
			},
			want: ConsensusDetectorPartition,
		},
		{
			name: "emergency below quorum",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 2,
				Quorum:           3,
			},
			want: ConsensusDetectorEmergency,
		},
		{
			name: "recovery while validators sync",
			in: ConsensusDetectorMetrics{
				TotalValidators:   4,
				ActiveValidators:  4,
				Quorum:            3,
				SyncingValidators: 1,
			},
			want: ConsensusDetectorRecovery,
		},
		{
			name: "strict at minimum quorum",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 3,
				Quorum:           3,
			},
			want: ConsensusDetectorStrict,
		},
		{
			name: "degraded slow blocks",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 4,
				Quorum:           3,
				BlockTimeMS:      6000,
			},
			want: ConsensusDetectorDegraded,
		},
		{
			name: "normal healthy",
			in: ConsensusDetectorMetrics{
				TotalValidators:  4,
				ActiveValidators: 4,
				Quorum:           3,
				BlockTimeMS:      1000,
			},
			want: ConsensusDetectorNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectConsensusMode(tt.in)
			if got.Mode != tt.want {
				t.Fatalf("mode mismatch: got=%s want=%s result=%+v", got.Mode, tt.want, got)
			}
			if got.Code != consensusDetectorModeCode(tt.want) {
				t.Fatalf("code mismatch: got=%d want=%d", got.Code, consensusDetectorModeCode(tt.want))
			}
		})
	}
}

func TestHandleConsensusModeEndpoint(t *testing.T) {
	oldRequireWallet := ConfigAuthRequireWallet
	oldAPIToken := apiToken
	defer func() {
		ConfigAuthRequireWallet = oldRequireWallet
		apiToken = oldAPIToken
	}()
	ConfigAuthRequireWallet = false
	apiToken = ""

	node := &Node{
		ID:   "TEST",
		Role: "validator",
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 1}, {ID: 2}},
		},
		Ledger: Ledger{
			Balances:               map[string]int{},
			Nonces:                 map[string]int{},
			Stakes:                 map[string]StakeLock{},
			ValidatorRewardWallets: map[string]string{},
		},
		lastCommitHeight: 2,
		lastCommitAt:     time.Now(),
	}

	s := NewServer(node)
	req := httptest.NewRequest(http.MethodGet, "/consensus/mode", nil)
	w := httptest.NewRecorder()
	s.handleConsensusMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["mode"] == "" || payload["reason"] == "" {
		t.Fatalf("expected mode and reason in response: %#v", payload)
	}
	if payload["mainnet_safety"] != "observe_classify_alert_only" {
		t.Fatalf("unexpected safety marker: %#v", payload["mainnet_safety"])
	}
}
