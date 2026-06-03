package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type infraServiceStatus struct {
	ID          string `json:"id"`
	URL         string `json:"url,omitempty"`
	Role        string `json:"role"`
	Healthy     bool   `json:"healthy"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	LatencyMS   int64  `json:"latency_ms,omitempty"`
	LastChecked int64  `json:"last_checked"`
}

func splitInfraEndpoints(raw string) []string {
	out := make([]string, 0)
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t'
	}) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func probeInfraService(id, rawURL, role string) infraServiceStatus {
	status := infraServiceStatus{
		ID:          strings.TrimSpace(id),
		URL:         strings.TrimRight(strings.TrimSpace(rawURL), "/"),
		Role:        strings.TrimSpace(role),
		State:       "not_configured",
		Reason:      "missing_url",
		LastChecked: time.Now().Unix(),
	}
	if status.ID == "" {
		status.ID = strings.ToUpper(status.Role)
	}
	if status.URL == "" {
		return status
	}
	target := status.URL + "/healthz"
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	started := time.Now()
	resp, err := client.Get(target)
	status.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		status.State = "unhealthy"
		status.Reason = err.Error()
		return status
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		status.Healthy = resp.StatusCode < 400
		if status.Healthy {
			status.State = "healthy"
			status.Reason = "healthz_ok"
		} else {
			status.State = "warning"
			status.Reason = resp.Status
		}
		return status
	}
	status.State = "unhealthy"
	status.Reason = resp.Status
	return status
}

func configuredInfraServiceStatuses(envName, role string) []infraServiceStatus {
	endpoints := splitInfraEndpoints(os.Getenv(envName))
	out := make([]infraServiceStatus, 0, len(endpoints))
	for i, endpoint := range endpoints {
		id := strings.ToUpper(role)
		if len(endpoints) > 1 {
			id = id + "-" + strconvItoa(i+1)
		}
		out = append(out, probeInfraService(id, endpoint, role))
	}
	if len(out) == 0 {
		out = append(out, infraServiceStatus{
			ID:          strings.ToUpper(role),
			Role:        role,
			Healthy:     false,
			State:       "not_configured",
			Reason:      envName + "_unset",
			LastChecked: time.Now().Unix(),
		})
	}
	return out
}

func strconvItoa(v int) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	buf := [20]byte{}
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}

func lightProofCapabilityStatus(n *Node) map[string]any {
	height := uint64(0)
	finalized := uint64(0)
	if n != nil && n.Blockchain != nil {
		height = n.Blockchain.Height()
		finalized = n.Blockchain.FinalizedHeight()
	}
	if n != nil {
		if fh := n.getFinalizedHeight(); fh > finalized {
			finalized = fh
		}
	}
	ready := height > 0 && finalized > 0
	return map[string]any{
		"status":             map[bool]string{true: "ready", false: "warming"}[ready],
		"ready":              ready,
		"height":             height,
		"finalized_height":   finalized,
		"headers":            "/light/headers",
		"checkpoint":         "/light/checkpoint/latest",
		"balance_proof":      "/proof/balance",
		"tx_proof":           "/proof/tx",
		"receipt_proof":      "/proof/receipt",
		"trust_model":        "headers+finality+merkle_proof",
		"wallet_trust_label": "Light verified only after local proof validation",
	}
}

func (s *Server) publicInfraStatusSnapshot() map[string]any {
	runtime := RuntimeStatusSnapshot{}
	if s != nil && s.Node != nil {
		if s.Node.Blockchain != nil {
			runtime = s.Node.runtimeStatusSnapshot()
		} else {
			runtime = RuntimeStatusSnapshot{
				Role:                  normalizeNodeRole(s.Node.Role),
				NetworkHealth:         "initializing",
				ConsensusDetectorMode: string(ConsensusDetectorRecovery),
				ConsensusMode:         "observer",
			}
		}
	}
	publicNodes := publicNodesSnapshot(nil, true)
	if s != nil {
		publicNodes = publicNodesSnapshot(s.Node, true)
	}
	storage := map[string]any{}
	if s != nil && s.Node != nil {
		storage = s.Node.storagePolicySnapshot()
	}
	archiveServices := make([]infraServiceStatus, 0)
	for _, node := range publicNodes.Nodes {
		if strings.EqualFold(strings.TrimSpace(node.Role), "archive") {
			archiveServices = append(archiveServices, infraServiceStatus{
				ID:          node.ID,
				URL:         node.GatewayRPCURL,
				Role:        "archive",
				Healthy:     node.Healthy && strings.ToLower(node.HealthState) != "unhealthy",
				State:       strings.TrimSpace(node.HealthState),
				Reason:      firstNonEmpty(node.HealthReason, node.ExcludedReason, node.SuspiciousReason, node.Error),
				LatencyMS:   node.LatencyMS,
				LastChecked: node.LastChecked,
			})
		}
	}
	if len(archiveServices) == 0 {
		archiveServices = configuredInfraServiceStatuses("MSC_ARCHIVE_ENDPOINTS", "archive")
	}
	indexerServices := configuredInfraServiceStatuses("MSC_INDEXER_ENDPOINTS", "indexer")
	return map[string]any{
		"status": "ok",
		"chain": map[string]any{
			"chain_id":               ChainID,
			"genesis_hash":           expectedGenesisHash(),
			"height":                 runtime.Height,
			"finalized_height":       runtime.FinalizedHeight,
			"finality_lag":           safeHeightLag(runtime.Height, runtime.FinalizedHeight),
			"last_block_age_seconds": runtime.LastBlockAgeSeconds,
			"cmd":                    firstNonEmpty(runtime.ConsensusDetectorMode, runtime.ConsensusMode),
			"cmd_reason":             runtime.ConsensusDetectorReason,
			"network_health":         runtime.NetworkHealth,
			"peers":                  runtime.Peers,
		},
		"validators": map[string]any{
			"role":              runtime.Role,
			"active_ready":      runtime.ActiveReadyCount,
			"live":              runtime.LiveValidators,
			"required_quorum":   runtime.RequiredQuorum,
			"strict_quorum":     runtime.StrictQuorum,
			"consensus_ready":   runtime.ConsensusReady,
			"validator_rpc":     "private_only",
			"new_validator_ids": []string{"H", "I", "J", "K"},
			"activation_order":  []string{"H", "I", "J", "K"},
		},
		"public_rpc": map[string]any{
			"status":         publicNodes.Status,
			"healthy":        publicNodes.Healthy,
			"total":          publicNodes.Total,
			"best":           publicNodes.Best,
			"nodes":          publicNodes.Nodes,
			"validator_rpcs": "excluded",
		},
		"archive": archiveServices,
		"indexer": indexerServices,
		"storage": storage,
		"light_client": lightProofCapabilityStatus(func() *Node {
			if s == nil {
				return nil
			}
			return s.Node
		}()),
		"gateway": map[string]any{
			"layout":            "hybrid_path_routes",
			"public_node_api":   "/v1/public-nodes",
			"lb_status":         "/gateway/lb-status.json",
			"events":            "/wallet/events",
			"metrics_public":    false,
			"validator_routing": "disabled",
		},
		"ts": time.Now().Unix(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func safeHeightLag(height, finalized uint64) uint64 {
	if height > finalized {
		return height - finalized
	}
	return 0
}

func expectedGenesisHash() string {
	if hash := strings.TrimSpace(ConfigGenesisHash); hash != "" {
		return hash
	}
	return strings.TrimSpace(GenesisHashExpected)
}

func (s *Server) handlePublicInfraStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(s.publicInfraStatusSnapshot())
	}
}

func (s *Server) handleV1PublicInfraStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeV1Data(w, http.StatusOK, s.publicInfraStatusSnapshot())
}
