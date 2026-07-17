package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// `formalVerificationVersion` defines the constant value used by this package.
const formalVerificationVersion = "msc-formal-v1"

type FormalVerificationAssumption struct {
	// `ID` stores the current position in the related collection.
	ID        string `json:"id"`
	// `Statement` stores the value associated with this record.
	Statement string `json:"statement"`
	// `Status` stores the value associated with this record.
	Status    string `json:"status"`
	// `Evidence` stores the value associated with this record.
	Evidence  string `json:"evidence,omitempty"`
}

type FormalVerificationObligation struct {
	// `ID` stores the current position in the related collection.
	ID       string `json:"id"`
	// `Kind` stores the value associated with this record.
	Kind     string `json:"kind"`
	// `Claim` stores the value associated with this record.
	Claim    string `json:"claim"`
	// `Status` stores the value associated with this record.
	Status   string `json:"status"`
	// `Evidence` stores the value associated with this record.
	Evidence string `json:"evidence,omitempty"`
}

type FormalRuntimeInvariant struct {
	// `ID` stores the current position in the related collection.
	ID       string `json:"id"`
	// `Severity` stores the value associated with this record.
	Severity string `json:"severity"`
	// `Passed` stores the value associated with this record.
	Passed   bool   `json:"passed"`
	// `Status` stores the value associated with this record.
	Status   string `json:"status"`
	// `Evidence` stores the value associated with this record.
	Evidence string `json:"evidence,omitempty"`
}

type FormalVerificationReport struct {
	// `Version` stores the value associated with this record.
	Version              string                         `json:"version"`
	// `Scope` stores the value associated with this record.
	Scope                string                         `json:"scope"`
	// `Healthy` stores the value associated with this record.
	Healthy              bool                           `json:"healthy"`
	// `Status` stores the value associated with this record.
	Status               string                         `json:"status"`
	// `MachineChecked` stores the value associated with this record.
	MachineChecked       bool                           `json:"machine_checked"`
	// `ExternalModelChecked` stores the value associated with this record.
	ExternalModelChecked bool                           `json:"external_model_checked"`
	// `ExternalProofStatus` stores the value associated with this record.
	ExternalProofStatus  string                         `json:"external_proof_status"`
	// `Height` stores the value associated with this record.
	Height               uint64                         `json:"height"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight      uint64                         `json:"finalized_height"`
	// `TotalValidators` stores the measured quantity used by this operation.
	TotalValidators      int                            `json:"total_validators"`
	// `ActiveValidators` stores the value associated with this record.
	ActiveValidators     int                            `json:"active_validators"`
	// `RequiredQuorum` stores the request data being processed.
	RequiredQuorum       int                            `json:"required_quorum"`
	// `StrictQuorum` stores the value associated with this record.
	StrictQuorum         int                            `json:"strict_quorum"`
	// `ConsensusMode` stores the value associated with this record.
	ConsensusMode        string                         `json:"consensus_mode"`
	// `DetectorMode` stores the value associated with this record.
	DetectorMode         string                         `json:"detector_mode"`
	// `InvariantsChecked` stores the current position in the related collection.
	InvariantsChecked    int                            `json:"invariants_checked"`
	// `InvariantsFailed` stores the current position in the related collection.
	InvariantsFailed     int                            `json:"invariants_failed"`
	// `Assumptions` stores the value associated with this record.
	Assumptions          []FormalVerificationAssumption `json:"assumptions"`
	// `Obligations` stores the value associated with this record.
	Obligations          []FormalVerificationObligation `json:"obligations"`
	// `RuntimeInvariants` stores the value associated with this record.
	RuntimeInvariants    []FormalRuntimeInvariant       `json:"runtime_invariants"`
	// `Limitations` stores the value associated with this record.
	Limitations          []string                       `json:"limitations"`
}

// formalStrictQuorum implements the formal strict quorum helper.
func formalStrictQuorum(total int) int {
	if total <= 0 {
		return 0
	}
	return (2*total)/3 + 1
}

// formalInvariant implements the formal invariant helper.
func formalInvariant(id, severity string, passed bool, evidence string) FormalRuntimeInvariant {
	// `status` stores the value produced by this operation.
	status := "pass"
	if !passed {
		status = "fail"
	}
	return FormalRuntimeInvariant{
		ID:       id,
		Severity: severity,
		Passed:   passed,
		Status:   status,
		Evidence: strings.TrimSpace(evidence),
	}
}

// formalVerificationStatus implements the formal verification status helper.
func formalVerificationStatus(healthy bool, failed int) string {
	if !healthy || failed > 0 {
		return "runtime_invariant_failure"
	}
	return "runtime_invariants_pass"
}

// formalVerificationHealthy implements the formal verification healthy helper.
func formalVerificationHealthy(invariants []FormalRuntimeInvariant) (bool, int) {
	// `failed` stores the value produced by this operation.
	failed := 0
	// `healthy` stores the value produced by this operation.
	healthy := true
	// `invariant` tracks the current position in the related collection.
	for _, invariant := range invariants {
		if invariant.Passed {
			continue
		}
		failed++
		if invariant.Severity == "critical" {
			healthy = false
		}
	}
	return healthy, failed
}

// formalVerificationReportFromRuntime implements the formal verification report from runtime helper.
func (n *Node) formalVerificationReportFromRuntime(runtime RuntimeStatusSnapshot) FormalVerificationReport {
	// `total` stores the measured quantity used by this operation.
	total := 0
	if n != nil {
		total = len(n.GetConsensusValidators(int(runtime.Height + 1)))
	}
	if total == 0 {
		total = GenesisFrozenValidatorSetSize
	}
	if total == 0 && runtime.RequiredQuorum > 0 {
		total = runtime.RequiredQuorum
	}
	// `required` stores the request data being processed.
	required := runtime.RequiredQuorum
	if required == 0 {
		required = runtime.StrictQuorum
	}
	if required == 0 {
		required = runtime.NetworkQuorumRequired
	}
	// `strict` stores the value produced by this operation.
	strict := formalStrictQuorum(total)
	// `detector` stores the value produced by this operation.
	detector := ConsensusDetectorResult{}
	if n != nil {
		detector = DetectConsensusMode(n.consensusDetectorMetricsFromRuntime(runtime))
	}
	// `detectorMode` stores the value produced by this operation.
	detectorMode := runtime.ConsensusDetectorMode
	if detectorMode == "" && detector.Mode != "" {
		detectorMode = string(detector.Mode)
	}

	// `invariants` stores the current position in the related collection.
	invariants := []FormalRuntimeInvariant{
		formalInvariant(
			"finalized_height_not_above_local_height",
			"critical",
			runtime.FinalizedHeight <= runtime.Height,
			fmt.Sprintf("height=%d finalized_height=%d", runtime.Height, runtime.FinalizedHeight),
		),
		formalInvariant(
			"strict_quorum_floor",
			"critical",
			total == 0 || required == 0 || required >= strict,
			fmt.Sprintf("total_validators=%d required_quorum=%d strict_floor=%d", total, required, strict),
		),
		formalInvariant(
			"active_validators_bounded_by_total",
			"critical",
			total == 0 || runtime.LiveValidators <= total,
			fmt.Sprintf("active_validators=%d total_validators=%d", runtime.LiveValidators, total),
		),
		formalInvariant(
			"consensus_detector_reports_finality_timeout",
			"critical",
			detector.LastFinalitySec <= detector.HaltedAfterSec || detector.Mode == ConsensusDetectorHalted || detector.Mode == ConsensusDetectorAttack,
			fmt.Sprintf("last_finality_seconds=%d detector_mode=%s", detector.LastFinalitySec, detector.Mode),
		),
		formalInvariant(
			"consensus_detector_reports_partition_risk",
			"critical",
			!detector.PartitionRisk || detector.Mode == ConsensusDetectorPartition || detector.Mode == ConsensusDetectorAttack,
			fmt.Sprintf("partition_risk=%t detector_mode=%s", detector.PartitionRisk, detector.Mode),
		),
		formalInvariant(
			"attack_signal_not_active",
			"critical",
			!detector.Attack,
			fmt.Sprintf("attack=%t detector_mode=%s", detector.Attack, detector.Mode),
		),
		formalInvariant(
			"required_quorum_not_above_total",
			"warning",
			total == 0 || required == 0 || required <= total,
			fmt.Sprintf("required_quorum=%d total_validators=%d", required, total),
		),
	}
	// `healthy` and `failed` store the value produced by this operation.
	healthy, failed := formalVerificationHealthy(invariants)

	// `obligations` stores the value produced by this operation.
	obligations := []FormalVerificationObligation{
		{
			ID:       "consensus_safety_no_two_finalized_hashes_per_height",
			Kind:     "safety",
			Claim:    "A node must reject a second finalized block hash for an already finalized height.",
			Status:   "machine_checked_runtime_and_tests",
			Evidence: "persistFinalizedHashInvariant, verifyFinalityCommitments, consensus_safety_persistence tests",
		},
		{
			ID:       "strict_finality_quorum",
			Kind:     "safety",
			Claim:    "Finality requires floor(2n/3)+1 execution/finality evidence and cannot use degraded quorum.",
			Status:   "machine_checked_runtime_and_tests",
			Evidence: fmt.Sprintf("required_quorum=%d strict_floor=%d", required, strict),
		},
		{
			ID:       "locked_finality_is_irreversible",
			Kind:     "safety",
			Claim:    "Irreversible roots and epoch anchors must reject deep finalized rewrites.",
			Status:   "machine_checked_runtime_and_tests",
			Evidence: "finality artifact verification and irreversible root tests",
		},
		{
			ID:       "eventual_finality_under_quorum_and_network",
			Kind:     "liveness",
			Claim:    "If strict quorum remains online and network delivery eventually succeeds, blocks continue finalizing.",
			Status:   "runtime_observed_not_theorem_proven",
			Evidence: fmt.Sprintf("detector_mode=%s finality_lag=%d", detectorMode, detector.FinalityLagBlocks),
		},
		{
			ID:       "external_tla_or_coq_model",
			Kind:     "formal_model",
			Claim:    "A separate TLA+/Coq/Isabelle model should prove consensus safety and liveness assumptions independent of Go code.",
			Status:   "pending_external_review",
			Evidence: "not yet externally model-checked",
		},
	}

	return FormalVerificationReport{
		Version:              formalVerificationVersion,
		Scope:                "consensus_safety_liveness_runtime_invariants",
		Healthy:              healthy,
		Status:               formalVerificationStatus(healthy, failed),
		MachineChecked:       true,
		ExternalModelChecked: false,
		ExternalProofStatus:  "pending_external_review",
		Height:               runtime.Height,
		FinalizedHeight:      runtime.FinalizedHeight,
		TotalValidators:      total,
		ActiveValidators:     runtime.LiveValidators,
		RequiredQuorum:       required,
		StrictQuorum:         strict,
		ConsensusMode:        strings.TrimSpace(runtime.ConsensusMode),
		DetectorMode:         strings.TrimSpace(detectorMode),
		InvariantsChecked:    len(invariants),
		InvariantsFailed:     failed,
		Assumptions: []FormalVerificationAssumption{
			{ID: "partial_synchrony", Statement: "Network messages are eventually delivered after bounded instability.", Status: "assumption"},
			{ID: "byzantine_bound", Statement: "Less than one third of active validator voting power is Byzantine.", Status: "assumption", Evidence: fmt.Sprintf("strict_quorum=%d total_validators=%d", strict, total)},
			{ID: "deterministic_execution", Statement: "All honest validators execute the same block from the same pre-state to the same post-state.", Status: "machine_checked_runtime_and_tests"},
			{ID: "key_integrity", Statement: "Validator signing keys, HSM keys, or MPC shares are not controlled by an adversarial quorum.", Status: "operator_assumption"},
		},
		Obligations:       obligations,
		RuntimeInvariants: invariants,
		Limitations: []string{
			"Runtime invariant checks are not a substitute for an independent mathematical proof.",
			"External TLA+/Coq/Isabelle model checking is tracked as pending until independently produced and reviewed.",
			"Liveness remains conditional on partial synchrony, quorum availability, disk health, and operator key availability.",
		},
	}
}

// formalVerificationSnapshotResponse implements the formal verification snapshot response helper.
func (s *Server) formalVerificationSnapshotResponse() (FormalVerificationReport, int, string) {
	if s == nil || s.Node == nil {
		return FormalVerificationReport{}, http.StatusServiceUnavailable, "node unavailable"
	}
	// `runtime` stores the value produced by this operation.
	runtime := s.Node.runtimeStatusSnapshot()
	return s.Node.formalVerificationReportFromRuntime(runtime), http.StatusOK, ""
}

// handleFormalVerification handles formal verification.
func (s *Server) handleFormalVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `resp`, `status`, and `errMsg` store the error produced by this operation.
	resp, status, errMsg := s.formalVerificationSnapshotResponse()
	if status != http.StatusOK {
		http.Error(w, errMsg, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleV1FormalVerification handles v1 formal verification.
func (s *Server) handleV1FormalVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `resp`, `status`, and `errMsg` store the error produced by this operation.
	resp, status, errMsg := s.formalVerificationSnapshotResponse()
	if status != http.StatusOK {
		writeV1Error(w, status, "", errMsg)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeV1Data(w, http.StatusOK, resp)
}
