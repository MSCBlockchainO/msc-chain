package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupGovernanceRPCTest(t *testing.T) (*Server, func()) {
	t.Helper()
	db, cleanupDB := openNodeDBForTest(t)
	oldRegistry := GlobalValidatorRegistry
	oldReadAuth := ConfigRPCRequireAuthForReadEndpoints
	oldSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	GlobalValidatorRegistry = NewValidatorRegistry()
	for _, id := range []string{"A", "B", "C", "D"} {
		rec := GlobalValidatorRegistry.Ensure(id, 1)
		rec.Stake = 100
		rec.Status = ValidatorActive
		rec.GovernanceSigner = true
	}
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	server := &Server{Node: &Node{ID: "TEST", DB: db}}
	cleanup := func() {
		ConfigRPCRequireAuthForReadEndpoints = oldReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = oldSubmitAuth
		GlobalValidatorRegistry = oldRegistry
		cleanupDB()
	}
	return server, cleanup
}

func governanceRPCJSONRequest(t *testing.T, method string, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeGovernanceRPCResponse(t *testing.T, rr *httptest.ResponseRecorder, out any) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
}

func TestGovernanceRPCTreasuryLifecyclePersists(t *testing.T) {
	server, cleanup := setupGovernanceRPCTest(t)
	defer cleanup()

	state := NewGovernanceState()
	state.TreasuryBalance = 500
	if err := server.Node.PersistGovernanceState(state); err != nil {
		t.Fatalf("seed governance state: %v", err)
	}

	proposal := GovernanceProposal{
		Kind:              GovernanceProposalTreasury,
		Title:             "rpc treasury",
		Proposer:          "A",
		CreatedHeight:     1,
		VotingStartHeight: 1,
		VotingEndHeight:   10,
		ActivationHeight:  1,
		TreasuryRecipient: "ops",
		TreasuryAmount:    125,
	}
	rr := httptest.NewRecorder()
	server.handleGovernancePropose(rr, governanceRPCJSONRequest(t, http.MethodPost, "/governance/propose", proposal))
	var proposed struct {
		ProposalID string `json:"proposal_id"`
	}
	decodeGovernanceRPCResponse(t, rr, &proposed)
	if proposed.ProposalID == "" {
		t.Fatalf("proposal id missing")
	}

	for _, voter := range []string{"A", "B", "C"} {
		rr = httptest.NewRecorder()
		server.handleGovernanceVote(rr, governanceRPCJSONRequest(t, http.MethodPost, "/governance/vote", governanceVoteRequest{
			ProposalID: proposed.ProposalID,
			Voter:      voter,
			Choice:     GovernanceVoteYes,
			Height:     2,
		}))
		if rr.Code != http.StatusOK {
			t.Fatalf("vote %s status=%d body=%s", voter, rr.Code, rr.Body.String())
		}
	}

	rr = httptest.NewRecorder()
	server.handleGovernanceFinalize(rr, governanceRPCJSONRequest(t, http.MethodPost, "/governance/finalize", governanceActionRequest{
		ProposalID: proposed.ProposalID,
		Height:     2,
	}))
	if rr.Code != http.StatusOK {
		t.Fatalf("finalize status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	server.handleGovernanceApply(rr, governanceRPCJSONRequest(t, http.MethodPost, "/governance/apply", governanceActionRequest{
		ProposalID: proposed.ProposalID,
		Height:     2,
	}))
	if rr.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", rr.Code, rr.Body.String())
	}

	loaded, err := server.Node.LoadGovernanceState()
	if err != nil {
		t.Fatalf("load persisted governance: %v", err)
	}
	if loaded.TreasuryBalance != 375 {
		t.Fatalf("treasury balance = %d, want 375", loaded.TreasuryBalance)
	}
	if loaded.Proposals[proposed.ProposalID].Status != GovernanceProposalApplied {
		t.Fatalf("proposal status = %s, want applied", loaded.Proposals[proposed.ProposalID].Status)
	}
}

func TestV1GovernanceStatusWrapsUpgradeManager(t *testing.T) {
	server, cleanup := setupGovernanceRPCTest(t)
	defer cleanup()
	state := NewGovernanceState()
	state.UpgradeManager.CurrentVersion = "0.2.0"
	if err := server.Node.PersistGovernanceState(state); err != nil {
		t.Fatalf("persist state: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleV1UpgradeStatus(rr, governanceRPCJSONRequest(t, http.MethodGet, "/v1/upgrade/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("v1 status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			CurrentVersion string `json:"current_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode v1 response: %v", err)
	}
	if !resp.Success || resp.Data.CurrentVersion != "0.2.0" {
		t.Fatalf("unexpected v1 response: %+v", resp)
	}
}
