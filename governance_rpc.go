package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type governanceVoteRequest struct {
	ProposalID string               `json:"proposal_id"`
	Voter      string               `json:"voter"`
	Choice     GovernanceVoteChoice `json:"choice"`
	Height     uint64               `json:"height,omitempty"`
}

type governanceProposalRequest struct {
	Proposal GovernanceProposal `json:"proposal"`
}

type governanceActionRequest struct {
	ProposalID string `json:"proposal_id"`
	Height     uint64 `json:"height,omitempty"`
}

func (s *Server) loadGovernanceStateForRPC() (*GovernanceState, error) {
	if s == nil || s.Node == nil {
		return nil, http.ErrServerClosed
	}
	state, err := s.Node.LoadGovernanceState()
	if err != nil {
		return nil, err
	}
	state.ensure()
	return state, nil
}

func (s *Server) governanceRPCHeight() uint64 {
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return 0
	}
	height := s.Node.Blockchain.Height()
	if height < 0 {
		return 0
	}
	return uint64(height)
}

func governanceRPCStatus(state *GovernanceState) map[string]any {
	if state == nil {
		state = NewGovernanceState()
	}
	state.ensure()
	return map[string]any{
		"state_hash":       state.Hash(),
		"treasury_balance": state.TreasuryBalance,
		"proposals":        state.Proposals,
		"upgrade_manager":  state.UpgradeManager,
		"protocol_gates": map[string]uint64{
			ProtocolGateValidatorSetActivationModelV2: ValidatorSetActivationModelV2Height,
			ProtocolGateValidatorSetCommitmentV2:      ValidatorSetCommitmentV2Height,
			ProtocolGateValidatorSetHashV3:            ValidatorSetHashV3Height,
			ProtocolGateSyncSnapshotCheckpointV2:      SyncSnapshotCheckpointV2Height,
			ProtocolGateDTLV2:                         ConfigDTLV2ActivationHeight,
		},
	}
}

func decodeGovernanceRPCBody(r *http.Request, out any) error {
	if r == nil || r.Body == nil {
		return nil
	}
	limit := int64(MaxTxRequestBodyBytes)
	if limit <= 0 {
		limit = 1 << 20
	}
	return json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(out)
}

func decodeGovernanceProposalBody(r *http.Request) (GovernanceProposal, error) {
	if r == nil || r.Body == nil {
		return GovernanceProposal{}, nil
	}
	limit := int64(MaxTxRequestBodyBytes)
	if limit <= 0 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		return GovernanceProposal{}, err
	}
	var wrapped governanceProposalRequest
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Proposal.Kind != "" {
		return wrapped.Proposal, nil
	}
	var proposal GovernanceProposal
	if err := json.Unmarshal(raw, &proposal); err != nil {
		return GovernanceProposal{}, err
	}
	return proposal, nil
}

func (s *Server) handleGovernanceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(governanceRPCStatus(state))
}

func (s *Server) handleGovernancePropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	proposal, err := decodeGovernanceProposalBody(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if proposal.CreatedHeight == 0 {
		proposal.CreatedHeight = s.governanceRPCHeight()
	}
	id, err := state.SubmitProposal(proposal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal_id": id, "proposal": state.Proposals[id]})
}

func (s *Server) handleGovernanceVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	var req governanceVoteRequest
	if err := decodeGovernanceRPCBody(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Height == 0 {
		req.Height = s.governanceRPCHeight()
	}
	if err := state.CastVote(req.ProposalID, req.Voter, req.Choice, req.Height, GlobalValidatorRegistry.Snapshot()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal": state.Proposals[strings.TrimSpace(req.ProposalID)]})
}

func (s *Server) handleGovernanceFinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	var req governanceActionRequest
	if err := decodeGovernanceRPCBody(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Height == 0 {
		req.Height = s.governanceRPCHeight()
	}
	tally, err := state.FinalizeProposal(req.ProposalID, req.Height, GlobalValidatorRegistry.Snapshot())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal": state.Proposals[strings.TrimSpace(req.ProposalID)], "tally": tally})
}

func (s *Server) handleGovernanceApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	var req governanceActionRequest
	if err := decodeGovernanceRPCBody(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Height == 0 {
		req.Height = s.governanceRPCHeight()
	}
	if err := state.ApplyApprovedProposal(req.ProposalID, req.Height); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal": state.Proposals[strings.TrimSpace(req.ProposalID)], "upgrade_manager": state.UpgradeManager})
}

func (s *Server) handleUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(governanceRPCStatus(state)["upgrade_manager"])
}

func (s *Server) handleV1GovernanceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "governance state unavailable")
		return
	}
	writeV1Data(w, http.StatusOK, governanceRPCStatus(state))
}

func (s *Server) handleV1GovernancePropose(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernancePropose)
}

func (s *Server) handleV1GovernanceVote(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernanceVote)
}

func (s *Server) handleV1GovernanceFinalize(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernanceFinalize)
}

func (s *Server) handleV1GovernanceApply(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernanceApply)
}

func (s *Server) handleV1UpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "governance state unavailable")
		return
	}
	writeV1Data(w, http.StatusOK, governanceRPCStatus(state)["upgrade_manager"])
}

func (s *Server) handleGovernanceV1Write(w http.ResponseWriter, r *http.Request, handler func(http.ResponseWriter, *http.Request)) {
	rec := &governanceV1Recorder{headers: make(http.Header)}
	handler(rec, r)
	for key, values := range rec.headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if rec.status >= 200 && rec.status < 300 {
		var payload any
		if len(rec.body) > 0 {
			_ = json.Unmarshal(rec.body, &payload)
		}
		writeV1Data(w, rec.status, payload)
		return
	}
	status := rec.status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writeV1Error(w, status, "", strings.TrimSpace(string(rec.body)))
}

type governanceV1Recorder struct {
	headers http.Header
	status  int
	body    []byte
}

func (r *governanceV1Recorder) Header() http.Header {
	return r.headers
}

func (r *governanceV1Recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *governanceV1Recorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body = append(r.body, data...)
	return len(data), nil
}
