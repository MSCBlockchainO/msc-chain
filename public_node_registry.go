package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPublicNodeRPC = "https://mscblockexplorer.in"
	publicNodeProbeTTL   = 5 * time.Second
)

type PublicNodeConfig struct {
	ID            string `toml:"id" json:"id"`
	RPCURL        string `toml:"rpc_url" json:"rpc_url"`
	Role          string `toml:"role" json:"role"`
	PublicGateway bool   `toml:"public_gateway" json:"public_gateway"`
	Version       string `toml:"version" json:"version,omitempty"`
	Country       string `toml:"country" json:"country,omitempty"`
	ASN           string `toml:"asn" json:"asn,omitempty"`
	Cloud         string `toml:"cloud" json:"cloud,omitempty"`
	Region        string `toml:"region" json:"region,omitempty"`
}

type PublicNodesConfig struct {
	Nodes     []PublicNodeConfig `toml:"nodes"`
	Endpoints StringList         `toml:"endpoints"`
}

type publicNodeHealthView struct {
	ID                  string `json:"id"`
	RPCURL              string `json:"rpc_url"`
	Role                string `json:"role"`
	PublicGateway       bool   `json:"public_gateway"`
	ChainID             string `json:"chain_id,omitempty"`
	GenesisHash         string `json:"genesis_hash,omitempty"`
	Version             string `json:"version,omitempty"`
	Country             string `json:"country,omitempty"`
	ASN                 string `json:"asn,omitempty"`
	Cloud               string `json:"cloud,omitempty"`
	Region              string `json:"region,omitempty"`
	Height              uint64 `json:"height"`
	FinalizedHeight     uint64 `json:"finalized_height"`
	HeightLagBlocks     uint64 `json:"height_lag_blocks,omitempty"`
	FinalityLag         uint64 `json:"finality_lag"`
	LastBlockAgeSeconds uint64 `json:"last_block_age_seconds"`
	PeerCount           int    `json:"peer_count"`
	ConsensusMode       string `json:"consensus_mode,omitempty"`
	NetworkHealth       string `json:"network_health,omitempty"`
	LatencyMS           int64  `json:"latency_ms"`
	Healthy             bool   `json:"healthy"`
	HealthState         string `json:"health_state,omitempty"`
	HealthReason        string `json:"health_reason,omitempty"`
	BadSamples          int    `json:"bad_samples,omitempty"`
	GoodSamples         int    `json:"good_samples,omitempty"`
	LastHealthyAt       int64  `json:"last_healthy_at,omitempty"`
	Score               int    `json:"score"`
	SuspiciousReason    string `json:"suspicious_reason,omitempty"`
	LastChecked         int64  `json:"last_checked"`
	StatusCode          int    `json:"status_code,omitempty"`
	Error               string `json:"error,omitempty"`
}

type publicNodesPayload struct {
	Status      string                 `json:"status"`
	ChainID     string                 `json:"chain_id"`
	GenesisHash string                 `json:"genesis_hash,omitempty"`
	Healthy     int                    `json:"healthy"`
	Total       int                    `json:"total"`
	Best        string                 `json:"best,omitempty"`
	BestNode    *publicNodeHealthView  `json:"best_node,omitempty"`
	Nodes       []publicNodeHealthView `json:"nodes"`
	TS          int64                  `json:"ts"`
}

var (
	ConfigPublicNodes []PublicNodeConfig

	publicNodeRegistryMu        sync.Mutex
	publicNodeRegistryCachedKey string
	publicNodeRegistryCheckedAt time.Time
	publicNodeRegistryCached    publicNodesPayload
	publicNodeHealthMemory      = map[string]publicNodeHealthSample{}
)

type publicNodeHealthSample struct {
	GoodSamples   int
	BadSamples    int
	LastHealthyAt int64
	LastState     string
}

func applyPublicNodesConfig(cfg PublicNodesConfig) bool {
	nodes := make([]PublicNodeConfig, 0, len(cfg.Nodes)+len(cfg.Endpoints))
	nodes = append(nodes, cfg.Nodes...)
	for i, endpoint := range cfg.Endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		nodes = append(nodes, PublicNodeConfig{
			ID:            fmt.Sprintf("P%d", i+1),
			RPCURL:        endpoint,
			Role:          "full",
			PublicGateway: true,
		})
	}
	normalized := normalizeConfiguredPublicNodes(nodes, false)
	if len(normalized) == 0 {
		return false
	}
	ConfigPublicNodes = normalized
	return true
}

func handlePublicNodesForbiddenDefault(id, role, rpc string) bool {
	if isCoreValidator(id) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(role), "validator") {
		return true
	}
	if publicNodeRPCUnsafeDefault(rpc) {
		return true
	}
	return false
}

func normalizeConfiguredPublicNodes(nodes []PublicNodeConfig, allowUnsafe bool) []PublicNodeConfig {
	out := make([]PublicNodeConfig, 0, len(nodes))
	seen := map[string]struct{}{}
	for _, node := range nodes {
		node.ID = strings.TrimSpace(node.ID)
		node.RPCURL = normalizePublicNodeRPCURL(node.RPCURL)
		node.Role = strings.ToLower(strings.TrimSpace(node.Role))
		if node.Role == "" {
			node.Role = "full"
		}
		if node.ID == "" {
			node.ID = publicNodeIDFromRPC(node.RPCURL)
		}
		if node.RPCURL == "" {
			continue
		}
		if !allowUnsafe && handlePublicNodesForbiddenDefault(node.ID, node.Role, node.RPCURL) {
			continue
		}
		node.PublicGateway = true
		key := strings.ToLower(node.RPCURL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, node)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

func configuredPublicNodes() []PublicNodeConfig {
	nodes := make([]PublicNodeConfig, 0, len(ConfigPublicNodes)+4)
	nodes = append(nodes, ConfigPublicNodes...)
	nodes = append(nodes, parsePublicNodesEnv(os.Getenv("MSC_PUBLIC_NODES"))...)
	nodes = append(nodes, parsePublicNodesEnv(os.Getenv("MSC_PUBLIC_NODE_REGISTRY"))...)
	if endpoints := splitCommaList(os.Getenv("MSC_PUBLIC_RPC_ENDPOINTS")); len(endpoints) > 0 {
		for i, endpoint := range endpoints {
			nodes = append(nodes, PublicNodeConfig{
				ID:            fmt.Sprintf("ENV%d", i+1),
				RPCURL:        endpoint,
				Role:          "full",
				PublicGateway: true,
			})
		}
	}
	if len(nodes) == 0 {
		nodes = append(nodes, PublicNodeConfig{
			ID:            "F",
			RPCURL:        defaultPublicNodeRPC,
			Role:          "full",
			PublicGateway: true,
		})
	}
	return normalizeConfiguredPublicNodes(nodes, publicNodesAllowUnsafeForTesting())
}

func parsePublicNodesEnv(raw string) []PublicNodeConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var nodes []PublicNodeConfig
		if err := json.Unmarshal([]byte(raw), &nodes); err == nil {
			return nodes
		}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == '\n'
	})
	nodes := make([]PublicNodeConfig, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) == 1 {
			nodes = append(nodes, PublicNodeConfig{ID: fmt.Sprintf("ENV%d", i+1), RPCURL: fields[0], Role: "full", PublicGateway: true})
			continue
		}
		node := PublicNodeConfig{ID: fields[0], RPCURL: fields[1], Role: "full", PublicGateway: true}
		if len(fields) > 2 && fields[2] != "" {
			node.Role = fields[2]
		}
		if len(fields) > 3 {
			node.Country = fields[3]
		}
		if len(fields) > 4 {
			node.ASN = fields[4]
		}
		if len(fields) > 5 {
			node.Cloud = fields[5]
		}
		if len(fields) > 6 {
			node.Region = fields[6]
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func publicNodesAllowUnsafeForTesting() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MSC_PUBLIC_NODES_ALLOW_UNSAFE")), "1")
}

func normalizePublicNodeRPCURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func publicNodeIDFromRPC(rpc string) string {
	u, err := url.Parse(rpc)
	if err == nil && u.Hostname() != "" {
		host := strings.Split(u.Hostname(), ".")[0]
		host = strings.TrimSpace(host)
		if host != "" {
			return strings.ToUpper(strings.ReplaceAll(host, "-", "_"))
		}
	}
	return "PUBLIC"
}

func publicNodeRPCUnsafeDefault(rpc string) bool {
	u, err := url.Parse(strings.TrimSpace(rpc))
	if err != nil {
		return true
	}
	host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}
	port, _ := strconv.Atoi(u.Port())
	if port >= 26657 && port <= 26666 {
		return true
	}
	return false
}

func publicNodesSourceKey(nodes []PublicNodeConfig) string {
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, strings.Join([]string{node.ID, node.RPCURL, node.Role, node.Country, node.ASN, node.Cloud, node.Region}, "|"))
	}
	return strings.Join(parts, ";")
}

func publicNodesSnapshot(node *Node, force bool) publicNodesPayload {
	nodes := configuredPublicNodes()
	if len(nodes) == 1 && strings.EqualFold(nodes[0].ID, "F") && strings.EqualFold(nodes[0].RPCURL, defaultPublicNodeRPC) && node != nil {
		nodeID := strings.TrimSpace(node.ID)
		nodeRole := strings.ToLower(strings.TrimSpace(node.Role))
		if nodeID != "" && nodeRole != "validator" && !isCoreValidator(nodeID) {
			nodes[0].ID = nodeID
		}
	}
	sourceKey := publicNodesSourceKey(nodes)
	publicNodeRegistryMu.Lock()
	if !force && sourceKey == publicNodeRegistryCachedKey && !publicNodeRegistryCheckedAt.IsZero() && time.Since(publicNodeRegistryCheckedAt) < publicNodeProbeTTL {
		cached := publicNodeRegistryCached
		publicNodeRegistryMu.Unlock()
		return cached
	}
	publicNodeRegistryMu.Unlock()

	views := make([]publicNodeHealthView, 0, len(nodes))
	for _, cfg := range nodes {
		view := probePublicNode(cfg)
		if strings.EqualFold(view.Role, "validator") || view.SuspiciousReason == "validator_rpc_not_allowed" {
			continue
		}
		views = append(views, view)
	}
	annotatePublicNodeHeightLag(views)
	best := publicNodeBest(views)
	healthy := 0
	for _, view := range views {
		if view.Healthy {
			healthy++
		}
	}
	status := "down"
	if healthy == len(views) && healthy > 0 {
		status = "healthy"
	} else if healthy > 0 {
		status = "degraded"
	}
	payload := publicNodesPayload{
		Status:      status,
		ChainID:     ChainID,
		GenesisHash: expectedPublicNodeGenesisHash(),
		Healthy:     healthy,
		Total:       len(views),
		Nodes:       views,
		TS:          time.Now().Unix(),
	}
	if best != nil {
		payload.Best = best.RPCURL
		copyBest := *best
		payload.BestNode = &copyBest
	}

	publicNodeRegistryMu.Lock()
	publicNodeRegistryCachedKey = sourceKey
	publicNodeRegistryCheckedAt = time.Now()
	publicNodeRegistryCached = payload
	publicNodeRegistryMu.Unlock()
	return payload
}

func annotatePublicNodeHeightLag(views []publicNodeHealthView) {
	maxHeight := uint64(0)
	for _, view := range views {
		if view.Height > maxHeight {
			maxHeight = view.Height
		}
	}
	for i := range views {
		if maxHeight > 0 && views[i].Height > 0 && maxHeight >= views[i].Height {
			views[i].HeightLagBlocks = maxHeight - views[i].Height
		}
		views[i].Score = publicNodeScore(views[i])
		applyPublicNodeHealthHysteresis(&views[i])
	}
}

func publicNodeHealthKey(view publicNodeHealthView) string {
	key := strings.TrimSpace(view.ID) + "|" + strings.TrimSpace(view.RPCURL)
	if strings.TrimSpace(key) == "|" {
		return strings.TrimSpace(view.RPCURL)
	}
	return key
}

func publicNodeHardFailReason(view publicNodeHealthView) string {
	if strings.TrimSpace(view.SuspiciousReason) != "" {
		switch view.SuspiciousReason {
		case "validator_rpc_not_allowed", "chain_id_mismatch", "genesis_hash_mismatch", "bad_status":
			return view.SuspiciousReason
		}
	}
	if view.StatusCode != 0 && view.StatusCode != http.StatusOK {
		return "bad_status"
	}
	switch strings.ToUpper(strings.TrimSpace(view.ConsensusMode)) {
	case "ATTACK", "PARTITION", "HALTED":
		return strings.ToLower(strings.TrimSpace(view.ConsensusMode))
	}
	return ""
}

func publicNodeBadReason(view publicNodeHealthView) string {
	if view.Error != "" || view.StatusCode == 0 {
		return "timeout"
	}
	if view.HeightLagBlocks > 20 {
		return "height_lag"
	}
	if view.FinalityLag > 20 {
		return "finality_lag"
	}
	if view.LastBlockAgeSeconds > 60 {
		return "block_stalled"
	}
	return ""
}

func publicNodeWarningReason(view publicNodeHealthView) string {
	if view.HeightLagBlocks > 2 {
		return "height_lag"
	}
	if view.FinalityLag > 2 {
		return "finality_lag"
	}
	if view.LastBlockAgeSeconds >= 12 {
		return "slow_block_age"
	}
	switch strings.ToUpper(strings.TrimSpace(view.ConsensusMode)) {
	case "STRICT", "RECOVERY", "DEGRADED", "EMERGENCY":
		return strings.ToLower(strings.TrimSpace(view.ConsensusMode))
	}
	if view.Score > 0 && view.Score < 60 {
		return "low_score"
	}
	return ""
}

func publicNodeBadThreshold(reason string) int {
	switch reason {
	case "height_lag":
		return 3
	case "timeout", "block_stalled", "finality_lag":
		return 2
	default:
		return 2
	}
}

func applyPublicNodeHealthHysteresis(view *publicNodeHealthView) {
	if view == nil {
		return
	}
	key := publicNodeHealthKey(*view)
	now := time.Now().Unix()
	hardReason := publicNodeHardFailReason(*view)
	badReason := publicNodeBadReason(*view)
	warnReason := publicNodeWarningReason(*view)

	publicNodeRegistryMu.Lock()
	sample := publicNodeHealthMemory[key]
	if hardReason != "" {
		sample.GoodSamples = 0
		sample.BadSamples++
		sample.LastState = "unhealthy"
		publicNodeHealthMemory[key] = sample
		publicNodeRegistryMu.Unlock()
		view.HealthState = "unhealthy"
		view.HealthReason = hardReason
		view.BadSamples = sample.BadSamples
		view.GoodSamples = sample.GoodSamples
		view.LastHealthyAt = sample.LastHealthyAt
		view.Healthy = false
		if view.SuspiciousReason == "" {
			view.SuspiciousReason = hardReason
		}
		return
	}

	if badReason != "" {
		sample.GoodSamples = 0
		sample.BadSamples++
		threshold := publicNodeBadThreshold(badReason)
		unhealthy := sample.BadSamples >= threshold
		if badReason == "height_lag" && view.HeightLagBlocks > 64 {
			unhealthy = true
		}
		if badReason == "finality_lag" && view.FinalityLag > 64 {
			unhealthy = true
		}
		if unhealthy {
			sample.LastState = "unhealthy"
		} else {
			sample.LastState = "warning"
		}
		publicNodeHealthMemory[key] = sample
		publicNodeRegistryMu.Unlock()
		view.HealthState = sample.LastState
		view.HealthReason = badReason
		view.BadSamples = sample.BadSamples
		view.GoodSamples = sample.GoodSamples
		view.LastHealthyAt = sample.LastHealthyAt
		view.Healthy = !unhealthy
		if unhealthy && view.SuspiciousReason == "" {
			view.SuspiciousReason = badReason
		}
		return
	}

	if warnReason != "" {
		sample.BadSamples = 0
		sample.GoodSamples = 0
		sample.LastState = "warning"
		publicNodeHealthMemory[key] = sample
		publicNodeRegistryMu.Unlock()
		view.HealthState = "warning"
		view.HealthReason = warnReason
		view.BadSamples = sample.BadSamples
		view.GoodSamples = sample.GoodSamples
		view.LastHealthyAt = sample.LastHealthyAt
		view.Healthy = true
		return
	}

	sample.BadSamples = 0
	sample.GoodSamples++
	healthy := sample.LastState == "healthy" || sample.GoodSamples >= 2
	if healthy {
		sample.LastState = "healthy"
		sample.LastHealthyAt = now
	} else {
		sample.LastState = "warning"
	}
	publicNodeHealthMemory[key] = sample
	publicNodeRegistryMu.Unlock()

	view.HealthState = sample.LastState
	if healthy {
		view.HealthReason = "healthy"
	} else {
		view.HealthReason = "warming_up"
	}
	view.BadSamples = sample.BadSamples
	view.GoodSamples = sample.GoodSamples
	view.LastHealthyAt = sample.LastHealthyAt
	view.Healthy = true
}

func probePublicNode(cfg PublicNodeConfig) publicNodeHealthView {
	view := publicNodeHealthView{
		ID:            cfg.ID,
		RPCURL:        cfg.RPCURL,
		Role:          cfg.Role,
		PublicGateway: true,
		ChainID:       ChainID,
		GenesisHash:   expectedPublicNodeGenesisHash(),
		Version:       cfg.Version,
		Country:       cfg.Country,
		ASN:           cfg.ASN,
		Cloud:         cfg.Cloud,
		Region:        cfg.Region,
		LastChecked:   time.Now().Unix(),
	}
	started := time.Now()
	client := &http.Client{Timeout: 3 * time.Second}
	statusCode, statusData, err := publicNodeFetchJSON(client, cfg.RPCURL+"/status")
	view.StatusCode = statusCode
	view.LatencyMS = int64(time.Since(started) / time.Millisecond)
	if err != nil {
		view.Error = err.Error()
		view.HealthReason = "timeout"
		view.Score = 0
		return view
	}
	if statusCode != http.StatusOK {
		view.Error = fmt.Sprintf("status_%d", statusCode)
		view.SuspiciousReason = "bad_status"
		view.Score = 0
		return view
	}
	view.Height = uint64FromAny(firstNonNil(statusData["height"], statusData["chain_height"]))
	view.FinalizedHeight = uint64FromAny(firstNonNil(statusData["finalized_height"], statusData["finalized"]))
	if view.Height >= view.FinalizedHeight {
		view.FinalityLag = view.Height - view.FinalizedHeight
	}
	view.LastBlockAgeSeconds = uint64FromAny(statusData["last_block_age_seconds"])
	view.PeerCount = int(uint64FromAny(firstNonNil(statusData["peers"], statusData["peer_count"])))
	view.NetworkHealth = stringFromAny(statusData["network_health"])
	if role := strings.ToLower(strings.TrimSpace(stringFromAny(statusData["role"]))); role != "" {
		view.Role = role
	}
	if chainID := strings.TrimSpace(stringFromAny(statusData["chain_id"])); chainID != "" {
		view.ChainID = chainID
	}
	if genesis := strings.TrimSpace(stringFromAny(firstNonNil(statusData["genesis_hash"], statusData["expected_genesis_hash"]))); genesis != "" {
		view.GenesisHash = strings.ToLower(strings.TrimPrefix(genesis, "0x"))
	}
	if version := strings.TrimSpace(stringFromAny(statusData["version"])); version != "" {
		view.Version = version
	}
	if view.Role == "validator" || isCoreValidator(view.ID) {
		view.SuspiciousReason = "validator_rpc_not_allowed"
		view.Score = 0
		return view
	}
	if want := strings.TrimSpace(ChainID); want != "" && strings.TrimSpace(view.ChainID) != "" && !strings.EqualFold(want, view.ChainID) {
		view.SuspiciousReason = "chain_id_mismatch"
		view.Score = 0
		return view
	}
	if want := expectedPublicNodeGenesisHash(); want != "" && view.GenesisHash != "" && !strings.EqualFold(want, view.GenesisHash) {
		view.SuspiciousReason = "genesis_hash_mismatch"
		view.Score = 0
		return view
	}
	cmdStatus, cmdData, cmdErr := publicNodeFetchJSON(client, cfg.RPCURL+"/consensus/mode")
	if cmdErr == nil && cmdStatus == http.StatusOK {
		view.ConsensusMode = strings.ToUpper(strings.TrimSpace(stringFromAny(cmdData["mode"])))
		if lag := uint64FromAny(firstNonNil(cmdData["finality_lag"], cmdData["finality_lag_blocks"])); lag > view.FinalityLag {
			view.FinalityLag = lag
		}
	} else if view.ConsensusMode == "" {
		view.ConsensusMode = strings.ToUpper(strings.TrimSpace(stringFromAny(statusData["consensus_detector_mode"])))
	}
	view.Score = publicNodeScore(view)
	view.Healthy = view.Score >= 60 && view.SuspiciousReason == ""
	return view
}

func publicNodeFetchJSON(client *http.Client, rawURL string) (int, map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "msc-public-node-registry/1")
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, data, nil
}

func publicNodeScore(view publicNodeHealthView) int {
	if view.SuspiciousReason != "" || view.StatusCode != http.StatusOK {
		return 0
	}
	score := 35
	if view.Height > 0 {
		score += 15
	}
	if view.HeightLagBlocks <= 2 {
		score += 8
	} else if view.HeightLagBlocks <= 20 {
		score += 3
	} else {
		score -= 20
	}
	if view.FinalityLag <= 2 {
		score += 14
	} else if view.FinalityLag <= 20 {
		score += 7
	}
	if view.PeerCount >= 3 {
		score += 12
	} else {
		score += maxInt(0, view.PeerCount*3)
	}
	if view.LastBlockAgeSeconds <= 12 {
		score += 10
	} else if view.LastBlockAgeSeconds <= 60 {
		score += 4
	}
	switch strings.ToUpper(view.ConsensusMode) {
	case "NORMAL":
		score += 9
	case "STRICT", "RECOVERY":
		score += 4
	case "EMERGENCY", "HALTED", "ATTACK", "PARTITION":
		score -= 30
	}
	if view.LatencyMS <= 250 {
		score += 9
	} else if view.LatencyMS <= 1000 {
		score += 5
	} else if view.LatencyMS <= 2500 {
		score += 2
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func publicNodeBest(nodes []publicNodeHealthView) *publicNodeHealthView {
	if len(nodes) == 0 {
		return nil
	}
	sorted := append([]publicNodeHealthView(nil), nodes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Healthy != sorted[j].Healthy {
			return sorted[i].Healthy
		}
		if sorted[i].Score != sorted[j].Score {
			return sorted[i].Score > sorted[j].Score
		}
		if sorted[i].Height != sorted[j].Height {
			return sorted[i].Height > sorted[j].Height
		}
		return sorted[i].LatencyMS < sorted[j].LatencyMS
	})
	return &sorted[0]
}

func expectedPublicNodeGenesisHash() string {
	for _, v := range []string{ConfigGenesisHash, GenesisHashExpected, GenesisHash} {
		v = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v), "0x"))
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func uint64FromAny(value any) uint64 {
	switch v := value.(type) {
	case uint64:
		return v
	case uint:
		return uint64(v)
	case int:
		if v > 0 {
			return uint64(v)
		}
	case int64:
		if v > 0 {
			return uint64(v)
		}
	case float64:
		if v > 0 {
			return uint64(v)
		}
	case string:
		n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Server) handlePublicNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}
	payload := publicNodesSnapshot(s.Node, strings.TrimSpace(r.URL.Query().Get("refresh")) == "1")
	writeV1Data(w, http.StatusOK, payload)
}
