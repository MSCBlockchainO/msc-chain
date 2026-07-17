package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type governanceVoteRequest struct {
	// `ProposalID` stores the value associated with this record.
	ProposalID string `json:"proposal_id"`
	// `Voter` stores the value associated with this record.
	Voter string `json:"voter"`
	// `Choice` stores the value associated with this record.
	Choice GovernanceVoteChoice `json:"choice"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height,omitempty"`
}

type governanceProposalRequest struct {
	// `Proposal` stores the value associated with this record.
	Proposal GovernanceProposal `json:"proposal"`
}

type governanceActionRequest struct {
	// `ProposalID` stores the value associated with this record.
	ProposalID string `json:"proposal_id"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height,omitempty"`
}

// loadGovernanceStateForRPC implements the load governance state for rpc helper.
func (s *Server) loadGovernanceStateForRPC() (*GovernanceState, error) {
	if s == nil || s.Node == nil {
		return nil, http.ErrServerClosed
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.Node.LoadGovernanceState()
	if err != nil {
		return nil, err
	}
	state.ensure()
	return state, nil
}

// governanceRPCHeight implements the governance rpc height helper.
func (s *Server) governanceRPCHeight() uint64 {
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return 0
	}
	// `height` stores the value produced by this operation.
	return s.Node.Blockchain.Height()
}

// governanceRPCStatus implements the governance rpc status helper.
func governanceRPCStatus(state *GovernanceState) map[string]any {
	if state == nil {
		state = NewGovernanceState()
	}
	state.ensure()
	return map[string]any{
		"state_hash":            state.Hash(),
		"treasury_balance":      state.TreasuryBalance,
		"proposals":             state.Proposals,
		"upgrade_manager":       state.UpgradeManager,
		"emergency_pause":       state.EmergencyPause,
		"current_max_supply":    state.CurrentMaxSupply,
		"supply_change_records": state.SupplyChangeRecords,
		"protocol_gates": map[string]uint64{
			ProtocolGateValidatorSetActivationModelV2: ValidatorSetActivationModelV2Height,
			ProtocolGateValidatorSetCommitmentV2:      ValidatorSetCommitmentV2Height,
			ProtocolGateValidatorSetHashV3:            ValidatorSetHashV3Height,
			ProtocolGateSyncSnapshotCheckpointV2:      SyncSnapshotCheckpointV2Height,
			ProtocolGateDTLV2:                         ConfigDTLV2ActivationHeight,
			ProtocolGateSupplyCapV2:                   SupplyCapV2ActivationHeight,
		},
	}
}

// decodeGovernanceRPCBody implements the decode governance rpc body helper.
func decodeGovernanceRPCBody(r *http.Request, out any) error {
	if r == nil || r.Body == nil {
		return nil
	}
	// `limit` stores the value produced by this operation.
	limit := int64(MaxTxRequestBodyBytes)
	if limit <= 0 {
		limit = 1 << 20
	}
	return json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(out)
}

// decodeGovernanceProposalBody implements the decode governance proposal body helper.
func decodeGovernanceProposalBody(r *http.Request) (GovernanceProposal, error) {
	if r == nil || r.Body == nil {
		return GovernanceProposal{}, nil
	}
	// `limit` stores the value produced by this operation.
	limit := int64(MaxTxRequestBodyBytes)
	if limit <= 0 {
		limit = 1 << 20
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		return GovernanceProposal{}, err
	}
	// `wrapped` stores the value used by this operation.
	var wrapped governanceProposalRequest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Proposal.Kind != "" {
		return wrapped.Proposal, nil
	}
	// `proposal` stores the value used by this operation.
	var proposal GovernanceProposal
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &proposal); err != nil {
		return GovernanceProposal{}, err
	}
	return proposal, nil
}

// handleGovernanceStatus handles governance status.
func (s *Server) handleGovernanceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(governanceRPCStatus(state))
}

// handleGovernancePropose handles governance propose.
func (s *Server) handleGovernancePropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	// `proposal` and `err` store the error produced by this operation.
	proposal, err := decodeGovernanceProposalBody(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if proposal.CreatedHeight == 0 {
		proposal.CreatedHeight = s.governanceRPCHeight()
	}
	// `id` and `err` store the error produced by this operation.
	id, err := state.SubmitProposal(proposal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// `err` stores the error produced by this operation.
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal_id": id, "proposal": state.Proposals[id]})
}

// handleGovernanceVote handles governance vote.
func (s *Server) handleGovernanceVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	// `req` stores the request data being processed.
	var req governanceVoteRequest
	// `err` stores the error produced by this operation.
	if err := decodeGovernanceRPCBody(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Height == 0 {
		req.Height = s.governanceRPCHeight()
	}
	// `err` stores the error produced by this operation.
	if err := state.CastVote(req.ProposalID, req.Voter, req.Choice, req.Height, GlobalValidatorRegistry.Snapshot()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// `err` stores the error produced by this operation.
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal": state.Proposals[strings.TrimSpace(req.ProposalID)]})
}

// handleGovernanceFinalize handles governance finalize.
func (s *Server) handleGovernanceFinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	// `req` stores the request data being processed.
	var req governanceActionRequest
	// `err` stores the error produced by this operation.
	if err := decodeGovernanceRPCBody(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Height == 0 {
		req.Height = s.governanceRPCHeight()
	}
	// `tally` and `err` store the error produced by this operation.
	tally, err := state.FinalizeProposal(req.ProposalID, req.Height, GlobalValidatorRegistry.Snapshot())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// `err` stores the error produced by this operation.
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal": state.Proposals[strings.TrimSpace(req.ProposalID)], "tally": tally})
}

// handleGovernanceApply handles governance apply.
func (s *Server) handleGovernanceApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSubmit(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	// `req` stores the request data being processed.
	var req governanceActionRequest
	// `err` stores the error produced by this operation.
	if err := decodeGovernanceRPCBody(r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Height == 0 {
		req.Height = s.governanceRPCHeight()
	}
	// `err` stores the error produced by this operation.
	if err := state.ApplyApprovedProposal(req.ProposalID, req.Height); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// `err` stores the error produced by this operation.
	if err := s.Node.PersistGovernanceState(state); err != nil {
		http.Error(w, "governance state persist failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposal": state.Proposals[strings.TrimSpace(req.ProposalID)], "upgrade_manager": state.UpgradeManager})
}

// handleUpgradeStatus handles upgrade status.
func (s *Server) handleUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		http.Error(w, "governance state unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(governanceRPCStatus(state)["upgrade_manager"])
}

// handleV1GovernanceStatus handles v1 governance status.
func (s *Server) handleV1GovernanceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "governance state unavailable")
		return
	}
	writeV1Data(w, http.StatusOK, governanceRPCStatus(state))
}

// handleV1GovernancePropose handles v1 governance propose.
func (s *Server) handleV1GovernancePropose(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernancePropose)
}

// handleV1GovernanceVote handles v1 governance vote.
func (s *Server) handleV1GovernanceVote(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernanceVote)
}

// handleV1GovernanceFinalize handles v1 governance finalize.
func (s *Server) handleV1GovernanceFinalize(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernanceFinalize)
}

// handleV1GovernanceApply handles v1 governance apply.
func (s *Server) handleV1GovernanceApply(w http.ResponseWriter, r *http.Request) {
	s.handleGovernanceV1Write(w, r, s.handleGovernanceApply)
}

// handleV1UpgradeStatus handles v1 upgrade status.
func (s *Server) handleV1UpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `state` and `err` store the error produced by this operation.
	state, err := s.loadGovernanceStateForRPC()
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "governance state unavailable")
		return
	}
	writeV1Data(w, http.StatusOK, governanceRPCStatus(state)["upgrade_manager"])
}

// handleGovernanceV1Write handles governance v1 write.
func (s *Server) handleGovernanceV1Write(w http.ResponseWriter, r *http.Request, handler func(http.ResponseWriter, *http.Request)) {
	// `rec` stores the value produced by this operation.
	rec := &governanceV1Recorder{headers: make(http.Header)}
	handler(rec, r)
	// `key` and `values` track the key used to access the related value.
	for key, values := range rec.headers {
		// `value` tracks the value currently being processed.
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if rec.status >= 200 && rec.status < 300 {
		// `payload` stores the value used by this operation.
		var payload any
		if len(rec.body) > 0 {
			_ = json.Unmarshal(rec.body, &payload)
		}
		writeV1Data(w, rec.status, payload)
		return
	}
	// `status` stores the value produced by this operation.
	status := rec.status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writeV1Error(w, status, "", strings.TrimSpace(string(rec.body)))
}

type governanceV1Recorder struct {
	// `headers` stores the block data handled by this operation.
	headers http.Header
	// `status` stores the value associated with this record.
	status int
	// `body` stores the value associated with this record.
	body []byte
}

// Header implements the header helper.
func (r *governanceV1Recorder) Header() http.Header {
	return r.headers
}

// WriteHeader writes header.
func (r *governanceV1Recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

// Write implements the write helper.
func (r *governanceV1Recorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body = append(r.body, data...)
	return len(data), nil
}
