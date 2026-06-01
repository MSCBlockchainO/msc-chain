package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFormalVerificationReportChecksCoreInvariants(t *testing.T) {
	node := &Node{ID: "TEST", Role: "validator"}
	report := node.formalVerificationReportFromRuntime(RuntimeStatusSnapshot{
		Height:                  10,
		FinalizedHeight:         9,
		LiveValidators:          3,
		RequiredQuorum:          3,
		StrictQuorum:            3,
		ConsensusMode:           "validator",
		ConsensusDetectorMode:   string(ConsensusDetectorNormal),
		LastBlockAgeSeconds:     1,
		NetworkQuorumRequired:   3,
		NetworkQuorumVotes:      3,
		BlockProductionStatus:   "producing",
		BlockProductionReason:   "recent_commit",
		ConsensusReady:          true,
		NetworkHealth:           "healthy",
		NetworkBestHeight:       10,
		ConsensusDetectorReason: "healthy",
	})

	if !report.Healthy {
		t.Fatalf("expected healthy formal report: %+v", report)
	}
	if !report.MachineChecked || report.ExternalModelChecked {
		t.Fatalf("unexpected proof status: machine=%t external=%t", report.MachineChecked, report.ExternalModelChecked)
	}
	if report.InvariantsChecked == 0 || report.InvariantsFailed != 0 {
		t.Fatalf("unexpected invariant counters: %+v", report)
	}
	if report.ExternalProofStatus != "pending_external_review" {
		t.Fatalf("external proof status must be honest/pending, got %q", report.ExternalProofStatus)
	}
}

func TestFormalVerificationReportRejectsFinalizedAboveHeight(t *testing.T) {
	node := &Node{ID: "TEST", Role: "validator"}
	report := node.formalVerificationReportFromRuntime(RuntimeStatusSnapshot{
		Height:              9,
		FinalizedHeight:     10,
		LiveValidators:      4,
		RequiredQuorum:      3,
		StrictQuorum:        3,
		LastBlockAgeSeconds: 1,
	})

	if report.Healthy {
		t.Fatalf("expected unhealthy formal report: %+v", report)
	}
	if report.InvariantsFailed == 0 {
		t.Fatalf("expected failed invariant counter: %+v", report)
	}
}

func TestFormalVerificationEndpoint(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodGet, "/formal/verification", nil)
	w := httptest.NewRecorder()
	s.handleFormalVerification(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}
	var payload FormalVerificationReport
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Version != formalVerificationVersion || payload.InvariantsChecked == 0 {
		t.Fatalf("unexpected formal verification payload: %+v", payload)
	}
}
