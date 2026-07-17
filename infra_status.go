package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type infraServiceStatus struct {
	// `ID` stores the current position in the related collection.
	ID              string `json:"id"`
	// `URL` stores the value associated with this record.
	URL             string `json:"url,omitempty"`
	// `Role` stores the value associated with this record.
	Role            string `json:"role"`
	// `Healthy` stores the value associated with this record.
	Healthy         bool   `json:"healthy"`
	// `State` stores the value associated with this record.
	State           string `json:"state"`
	// `Reason` stores the value associated with this record.
	Reason          string `json:"reason,omitempty"`
	// `LatencyMS` stores the value associated with this record.
	LatencyMS       int64  `json:"latency_ms,omitempty"`
	// `LastChecked` stores the value associated with this record.
	LastChecked     int64  `json:"last_checked"`
	// `Height` stores the value associated with this record.
	Height          uint64 `json:"height,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalityLag` stores the value associated with this record.
	FinalityLag     uint64 `json:"finality_lag,omitempty"`
	// `ArchiveMode` stores the value associated with this record.
	ArchiveMode     bool   `json:"archive_mode,omitempty"`
	// `IndexedHeight` stores the current position in the related collection.
	IndexedHeight   uint64 `json:"indexed_height,omitempty"`
	// `ArchiveHeight` stores the value associated with this record.
	ArchiveHeight   uint64 `json:"archive_height,omitempty"`
	// `IndexLag` stores the current position in the related collection.
	IndexLag        uint64 `json:"index_lag,omitempty"`
	// `SourceRPC` stores the value associated with this record.
	SourceRPC       string `json:"source_rpc,omitempty"`
	// `ChainID` stores the value associated with this record.
	ChainID         string `json:"chain_id,omitempty"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash     string `json:"genesis_hash,omitempty"`
}

// splitInfraEndpoints implements the split infra endpoints helper.
func splitInfraEndpoints(raw string) []string {
	// `out` stores the result produced by this operation.
	out := make([]string, 0)
	// `part` tracks the current values while iterating.
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

// probeInfraService implements the probe infra service helper.
func probeInfraService(id, rawURL, role string) infraServiceStatus {
	// `status` stores the value produced by this operation.
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
	// `client` stores the value produced by this operation.
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	// `started` stores the value produced by this operation.
	started := time.Now()
	if strings.EqualFold(status.Role, "archive") {
		probeArchiveInfraService(client, &status)
		status.LatencyMS = time.Since(started).Milliseconds()
		return status
	}
	if strings.EqualFold(status.Role, "indexer") {
		probeIndexerInfraService(client, &status)
		status.LatencyMS = time.Since(started).Milliseconds()
		return status
	}
	// `target` stores the value produced by this operation.
	target := status.URL + "/healthz"
	// `resp` and `err` store the error produced by this operation.
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

// probeInfraJSON implements the probe infra json helper.
func probeInfraJSON(client *http.Client, target string) (map[string]any, int, error) {
	// `resp` and `err` store the error produced by this operation.
	resp, err := client.Get(target)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	// `raw` stores the value used by this operation.
	var raw map[string]any
	// `err` stores the error produced by this operation.
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, resp.StatusCode, err
	}
	// `ok` and `has` store whether the related condition is satisfied.
	if ok, has := raw["success"].(bool); has {
		if !ok {
			return raw, resp.StatusCode, nil
		}
		// `data` and `ok` store whether the related condition is satisfied.
		if data, ok := raw["data"].(map[string]any); ok {
			raw = data
		}
	}
	return raw, resp.StatusCode, nil
}

// probeArchiveInfraService implements the probe archive infra service helper.
func probeArchiveInfraService(client *http.Client, status *infraServiceStatus) {
	// `runtime`, `code`, and `err` store the error produced by this operation.
	runtime, code, err := probeInfraJSON(client, status.URL+"/status")
	if err != nil {
		status.State = "unhealthy"
		status.Reason = err.Error()
		return
	}
	if code != http.StatusOK {
		status.State = "unhealthy"
		status.Reason = http.StatusText(code)
		return
	}
	// `policy` stores the value produced by this operation.
	policy, _, _ := probeInfraJSON(client, status.URL+"/storage/policy")
	status.Height = mapUint(runtime, "height")
	status.FinalizedHeight = mapUint(runtime, "finalized_height")
	status.FinalityLag = safeHeightLag(status.Height, status.FinalizedHeight)
	status.ChainID = mapString(runtime, "chain_id")
	status.GenesisHash = firstNonEmpty(mapString(runtime, "genesis_hash"), mapString(runtime, "expected_genesis_hash"))
	status.ArchiveMode = mapBool(policy, "archive_mode") || strings.EqualFold(mapString(policy, "profile"), storageProfileArchive)
	if mapBool(runtime, "is_validator") || strings.EqualFold(mapString(runtime, "role"), "validator") {
		status.State = "unhealthy"
		status.Reason = "validator_rpc_not_allowed"
		return
	}
	if !status.ArchiveMode {
		status.State = "warning"
		status.Reason = "archive_mode_required"
		return
	}
	if status.Height == 0 || status.FinalityLag > 2 {
		status.State = "warning"
		status.Reason = "archive_syncing"
		return
	}
	status.Healthy = true
	status.State = "healthy"
	status.Reason = "archive_synced"
}

// probeIndexerInfraService implements the probe indexer infra service helper.
func probeIndexerInfraService(client *http.Client, status *infraServiceStatus) {
	// `data`, `code`, and `err` store the error produced by this operation.
	data, code, err := probeInfraJSON(client, status.URL+"/indexer/status")
	if err != nil {
		status.State = "unhealthy"
		status.Reason = err.Error()
		return
	}
	if code != http.StatusOK {
		status.State = "unhealthy"
		status.Reason = http.StatusText(code)
		return
	}
	status.Healthy = mapBool(data, "healthy")
	status.State = firstNonEmpty(mapString(data, "state"), map[bool]string{true: "healthy", false: "warning"}[status.Healthy])
	status.Reason = firstNonEmpty(mapString(data, "reason"), mapString(data, "last_error"))
	status.IndexedHeight = mapUint(data, "indexed_height")
	status.ArchiveHeight = mapUint(data, "archive_height")
	status.FinalizedHeight = mapUint(data, "finalized_height")
	status.IndexLag = mapUint(data, "index_lag")
	status.ArchiveMode = mapBool(data, "archive_mode")
	status.SourceRPC = mapString(data, "source_rpc")
	status.ChainID = mapString(data, "chain_id")
	status.GenesisHash = mapString(data, "genesis_hash")
	if status.IndexLag > 2 {
		status.Healthy = false
		if status.State == "healthy" {
			status.State = "warning"
		}
		if status.Reason == "" {
			status.Reason = "index_lag"
		}
	}
}

// configuredInfraServiceStatuses implements the configured infra service statuses helper.
func configuredInfraServiceStatuses(envName, role string) []infraServiceStatus {
	// `endpoints` stores the value produced by this operation.
	endpoints := splitInfraEndpoints(os.Getenv(envName))
	// `out` stores the result produced by this operation.
	out := make([]infraServiceStatus, 0, len(endpoints))
	// `i` and `endpoint` track the current position in the related collection.
	for i, endpoint := range endpoints {
		// `id` stores the current position in the related collection.
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

// strconvItoa implements the strconv itoa helper.
func strconvItoa(v int) string {
	if v == 0 {
		return "0"
	}
	// `digits` defines the constant value used by this package.
	const digits = "0123456789"
	// `buf` stores the value produced by this operation.
	buf := [20]byte{}
	// `i` stores the current position in the related collection.
	i := len(buf)
	// `n` stores the value produced by this operation.
	n := v
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}

// lightProofCapabilityStatus implements the light proof capability status helper.
func lightProofCapabilityStatus(n *Node) map[string]any {
	// `height` stores the value produced by this operation.
	height := uint64(0)
	// `finalized` stores the value produced by this operation.
	finalized := uint64(0)
	if n != nil && n.Blockchain != nil {
		height = n.Blockchain.Height()
		finalized = n.Blockchain.FinalizedHeight()
	}
	if n != nil {
		// `fh` stores the value produced by this operation.
		if fh := n.getFinalizedHeight(); fh > finalized {
			finalized = fh
		}
	}
	// `ready` stores the value produced by this operation.
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

// publicInfraStatusSnapshot implements the public infra status snapshot helper.
func (s *Server) publicInfraStatusSnapshot() map[string]any {
	// `runtime` stores the value produced by this operation.
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
	// `publicNodes` stores the value produced by this operation.
	publicNodes := publicNodesSnapshot(nil, true)
	if s != nil {
		publicNodes = publicNodesSnapshot(s.Node, true)
	}
	// `storage` stores the value produced by this operation.
	storage := map[string]any{}
	if s != nil && s.Node != nil {
		storage = s.Node.storagePolicySnapshot()
	}
	// `archiveServices` stores the value produced by this operation.
	archiveServices := make([]infraServiceStatus, 0)
	// `node` tracks the current values while iterating.
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
	// `indexerServices` stores the current position in the related collection.
	indexerServices := configuredInfraServiceStatuses("MSC_INDEXER_ENDPOINTS", "indexer")
	return map[string]any{
		"status": "ok",
		"chain": map[string]any{
			"chain_id":               protocolChainID(),
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

// firstNonEmpty implements the first non empty helper.
func firstNonEmpty(values ...string) string {
	// `value` tracks the value currently being processed.
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// safeHeightLag implements the safe height lag helper.
func safeHeightLag(height, finalized uint64) uint64 {
	if height > finalized {
		return height - finalized
	}
	return 0
}

// expectedGenesisHash implements the expected genesis hash helper.
func expectedGenesisHash() string {
	// `hash` stores the digest used to identify or verify the related data.
	if hash := strings.TrimSpace(ConfigGenesisHash); hash != "" {
		return hash
	}
	return strings.TrimSpace(GenesisHashExpected)
}

// handlePublicInfraStatus handles public infra status.
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

// handleV1PublicInfraStatus handles v1 public infra status.
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
