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
	// `defaultPublicNodeRPC` defines the constant value used by this package.
	defaultPublicNodeRPC = "https://wallet.mscblockexplorer.in"
	// `publicNodeProbeTTL` defines the constant value used by this package.
	publicNodeProbeTTL = 5 * time.Second
)

func defaultPublicNodeRPCURL() string {
	if value := strings.TrimSpace(os.Getenv("MSC_PUBLIC_NODE_DEFAULT_RPC")); value != "" {
		return value
	}
	return defaultPublicNodeRPC
}

type PublicNodeConfig struct {
	// `ID` stores the current position in the related collection.
	ID string `toml:"id" json:"id"`
	// `RPCURL` stores the value associated with this record.
	RPCURL string `toml:"rpc_url" json:"rpc_url"`
	// `Role` stores the value associated with this record.
	Role string `toml:"role" json:"role"`
	// `PublicGateway` stores the value associated with this record.
	PublicGateway bool `toml:"public_gateway" json:"public_gateway"`
	// `Version` stores the value associated with this record.
	Version string `toml:"version" json:"version,omitempty"`
	// `Country` stores the measured quantity used by this operation.
	Country string `toml:"country" json:"country,omitempty"`
	// `ASN` stores the value associated with this record.
	ASN string `toml:"asn" json:"asn,omitempty"`
	// `Cloud` stores the value associated with this record.
	Cloud string `toml:"cloud" json:"cloud,omitempty"`
	// `Region` stores the value associated with this record.
	Region string `toml:"region" json:"region,omitempty"`
}

type PublicNodesConfig struct {
	// `Nodes` stores the value associated with this record.
	Nodes []PublicNodeConfig `toml:"nodes"`
	// `Endpoints` stores the value associated with this record.
	Endpoints StringList `toml:"endpoints"`
}

type publicNodeHealthView struct {
	// `ID` stores the current position in the related collection.
	ID string `json:"id"`
	// `RPCURL` stores the value associated with this record.
	RPCURL string `json:"rpc_url"`
	// `GatewayRPCURL` stores the value associated with this record.
	GatewayRPCURL string `json:"gateway_rpc_url,omitempty"`
	// `Role` stores the value associated with this record.
	Role string `json:"role"`
	// `PublicGateway` stores the value associated with this record.
	PublicGateway bool `json:"public_gateway"`
	// `ActiveGateway` stores the value associated with this record.
	ActiveGateway bool `json:"active_gateway,omitempty"`
	// `SelectedReason` stores the value associated with this record.
	SelectedReason string `json:"selected_reason,omitempty"`
	// `ExcludedReason` stores the value associated with this record.
	ExcludedReason string `json:"excluded_reason,omitempty"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id,omitempty"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash,omitempty"`
	// `Version` stores the value associated with this record.
	Version string `json:"version,omitempty"`
	// `Country` stores the measured quantity used by this operation.
	Country string `json:"country,omitempty"`
	// `ASN` stores the value associated with this record.
	ASN string `json:"asn,omitempty"`
	// `Cloud` stores the value associated with this record.
	Cloud string `json:"cloud,omitempty"`
	// `Region` stores the value associated with this record.
	Region string `json:"region,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height"`
	// `HeightLagBlocks` stores the value associated with this record.
	HeightLagBlocks uint64 `json:"height_lag_blocks,omitempty"`
	// `FinalityLag` stores the value associated with this record.
	FinalityLag uint64 `json:"finality_lag"`
	// `LastBlockAgeSeconds` stores the value associated with this record.
	LastBlockAgeSeconds uint64 `json:"last_block_age_seconds"`
	// `PeerCount` stores the measured quantity used by this operation.
	PeerCount int `json:"peer_count"`
	// `ConsensusMode` stores the value associated with this record.
	ConsensusMode string `json:"consensus_mode,omitempty"`
	// `NetworkHealth` stores the value associated with this record.
	NetworkHealth string `json:"network_health,omitempty"`
	// `Syncing` stores the value associated with this record.
	Syncing bool `json:"syncing,omitempty"`
	// `SyncComplete` stores the value associated with this record.
	SyncComplete bool `json:"sync_complete,omitempty"`
	// `LatencyMS` stores the value associated with this record.
	LatencyMS int64 `json:"latency_ms"`
	// `Healthy` stores the value associated with this record.
	Healthy bool `json:"healthy"`
	// `HealthState` stores the value associated with this record.
	HealthState string `json:"health_state,omitempty"`
	// `HealthReason` stores the value associated with this record.
	HealthReason string `json:"health_reason,omitempty"`
	// `BadSamples` stores the value associated with this record.
	BadSamples int `json:"bad_samples,omitempty"`
	// `GoodSamples` stores the value associated with this record.
	GoodSamples int `json:"good_samples,omitempty"`
	// `LastHealthyAt` stores the value associated with this record.
	LastHealthyAt int64 `json:"last_healthy_at,omitempty"`
	// `Score` stores the value associated with this record.
	Score int `json:"score"`
	// `SuspiciousReason` stores the value associated with this record.
	SuspiciousReason string `json:"suspicious_reason,omitempty"`
	// `LastChecked` stores the value associated with this record.
	LastChecked int64 `json:"last_checked"`
	// `StatusCode` stores the value associated with this record.
	StatusCode int `json:"status_code,omitempty"`
	// `Error` stores the error produced by this operation.
	Error string `json:"error,omitempty"`
}

type publicNodesPayload struct {
	// `Status` stores the value associated with this record.
	Status string `json:"status"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash,omitempty"`
	// `Healthy` stores the value associated with this record.
	Healthy int `json:"healthy"`
	// `Stable` stores the value associated with this record.
	Stable int `json:"stable_healthy,omitempty"`
	// `Warning` stores the value associated with this record.
	Warning int `json:"warning,omitempty"`
	// `Unhealthy` stores the value associated with this record.
	Unhealthy int `json:"unhealthy,omitempty"`
	// `Active` stores the value associated with this record.
	Active int `json:"active,omitempty"`
	// `Total` stores the measured quantity used by this operation.
	Total int `json:"total"`
	// `Best` stores the value associated with this record.
	Best string `json:"best,omitempty"`
	// `BestNode` stores the value associated with this record.
	BestNode *publicNodeHealthView `json:"best_node,omitempty"`
	// `Nodes` stores the value associated with this record.
	Nodes []publicNodeHealthView `json:"nodes"`
	// `TS` stores the value associated with this record.
	TS int64 `json:"ts"`
}

var (
	// `ConfigPublicNodes` stores the configuration used by this operation.
	ConfigPublicNodes []PublicNodeConfig

	// `publicNodeRegistryMu` stores the synchronization state protecting shared data.
	publicNodeRegistryMu sync.Mutex
	// `publicNodeRegistryCachedKey` stores the key used to access the related value.
	publicNodeRegistryCachedKey string
	// `publicNodeRegistryCheckedAt` stores the value used by this operation.
	publicNodeRegistryCheckedAt time.Time
	// `publicNodeRegistryCached` stores the value used by this operation.
	publicNodeRegistryCached publicNodesPayload
	// `publicNodeHealthMemory` stores the value used by this operation.
	publicNodeHealthMemory = map[string]publicNodeHealthSample{}
)

type publicNodeHealthSample struct {
	// `GoodSamples` stores the value associated with this record.
	GoodSamples int
	// `BadSamples` stores the value associated with this record.
	BadSamples int
	// `LastHealthyAt` stores the value associated with this record.
	LastHealthyAt int64
	// `LastState` stores the value associated with this record.
	LastState string
}

// applyPublicNodesConfig applies public nodes config.
func applyPublicNodesConfig(cfg PublicNodesConfig) bool {
	// `nodes` stores the value produced by this operation.
	nodes := make([]PublicNodeConfig, 0, len(cfg.Nodes)+len(cfg.Endpoints))
	nodes = append(nodes, cfg.Nodes...)
	// `i` and `endpoint` track the current position in the related collection.
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
	// `normalized` stores the value produced by this operation.
	normalized := normalizeConfiguredPublicNodes(nodes, false)
	if len(normalized) == 0 {
		return false
	}
	ConfigPublicNodes = normalized
	return true
}

// handlePublicNodesForbiddenDefault handles public nodes forbidden default.
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

// normalizeConfiguredPublicNodes normalizes configured public nodes.
func normalizeConfiguredPublicNodes(nodes []PublicNodeConfig, allowUnsafe bool) []PublicNodeConfig {
	// `out` stores the result produced by this operation.
	out := make([]PublicNodeConfig, 0, len(nodes))
	// `seen` stores the value produced by this operation.
	seen := map[string]struct{}{}
	// `node` tracks the current values while iterating.
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
		// `key` stores the key used to access the related value.
		key := strings.ToLower(node.RPCURL)
		// `ok` stores whether the related condition is satisfied.
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

// configuredPublicNodes implements the configured public nodes helper.
func configuredPublicNodes() []PublicNodeConfig {
	// `nodes` stores the value produced by this operation.
	nodes := make([]PublicNodeConfig, 0, len(ConfigPublicNodes)+4)
	nodes = append(nodes, ConfigPublicNodes...)
	nodes = append(nodes, parsePublicNodesEnv(os.Getenv("MSC_PUBLIC_NODES"))...)
	nodes = append(nodes, parsePublicNodesEnv(os.Getenv("MSC_PUBLIC_NODE_REGISTRY"))...)
	// `endpoints` stores the value produced by this operation.
	if endpoints := splitCommaList(os.Getenv("MSC_PUBLIC_RPC_ENDPOINTS")); len(endpoints) > 0 {
		// `i` and `endpoint` track the current position in the related collection.
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
			RPCURL:        defaultPublicNodeRPCURL(),
			Role:          "full",
			PublicGateway: true,
		})
	}
	return normalizeConfiguredPublicNodes(nodes, publicNodesAllowUnsafeForTesting())
}

// parsePublicNodesEnv parses public nodes env.
func parsePublicNodesEnv(raw string) []PublicNodeConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		// `nodes` stores the value used by this operation.
		var nodes []PublicNodeConfig
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal([]byte(raw), &nodes); err == nil {
			return nodes
		}
	}
	// `parts` stores the value produced by this operation.
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == '\n'
	})
	// `nodes` stores the value produced by this operation.
	nodes := make([]PublicNodeConfig, 0, len(parts))
	// `i` and `part` track the current position in the related collection.
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// `fields` stores the value produced by this operation.
		fields := strings.Split(part, "|")
		// `i` tracks the current position in the related collection.
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) == 1 {
			nodes = append(nodes, PublicNodeConfig{ID: fmt.Sprintf("ENV%d", i+1), RPCURL: fields[0], Role: "full", PublicGateway: true})
			continue
		}
		// `node` stores the value produced by this operation.
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

// publicNodesAllowUnsafeForTesting implements the public nodes allow unsafe for testing helper.
func publicNodesAllowUnsafeForTesting() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MSC_PUBLIC_NODES_ALLOW_UNSAFE")), "1")
}

// normalizePublicNodeRPCURL normalizes public node rpcurl.
func normalizePublicNodeRPCURL(raw string) string {
	// `value` stores the value currently being processed.
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	// `u` and `err` store the error produced by this operation.
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

// publicNodeIDFromRPC implements the public node id from rpc helper.
func publicNodeIDFromRPC(rpc string) string {
	// `u` and `err` store the error produced by this operation.
	u, err := url.Parse(rpc)
	if err == nil && u.Hostname() != "" {
		// `host` stores the value produced by this operation.
		host := strings.Split(u.Hostname(), ".")[0]
		host = strings.TrimSpace(host)
		if host != "" {
			return strings.ToUpper(strings.ReplaceAll(host, "-", "_"))
		}
	}
	return "PUBLIC"
}

// publicNodeRPCUnsafeDefault implements the public node rpc unsafe default helper.
func publicNodeRPCUnsafeDefault(rpc string) bool {
	// `u` and `err` store the error produced by this operation.
	u, err := url.Parse(strings.TrimSpace(rpc))
	if err != nil {
		return true
	}
	// `host` stores the value produced by this operation.
	host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	// `ip` stores the current position in the related collection.
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}
	// `port` stores the value produced by this operation.
	port, _ := strconv.Atoi(u.Port())
	if port >= 26657 && port <= 26666 {
		return true
	}
	return false
}

// publicNodesSourceKey implements the public nodes source key helper.
func publicNodesSourceKey(nodes []PublicNodeConfig) string {
	// `parts` stores the value produced by this operation.
	parts := make([]string, 0, len(nodes))
	// `node` tracks the current values while iterating.
	for _, node := range nodes {
		parts = append(parts, strings.Join([]string{node.ID, node.RPCURL, node.Role, node.Country, node.ASN, node.Cloud, node.Region}, "|"))
	}
	return strings.Join(parts, ";")
}

// publicNodesSnapshot implements the public nodes snapshot helper.
func publicNodesSnapshot(node *Node, force bool) publicNodesPayload {
	// `nodes` stores the value produced by this operation.
	nodes := configuredPublicNodes()
	if len(nodes) == 1 &&
		strings.EqualFold(nodes[0].ID, "F") &&
		strings.EqualFold(nodes[0].RPCURL, normalizePublicNodeRPCURL(defaultPublicNodeRPCURL())) &&
		node != nil {
		// `nodeID` stores the value produced by this operation.
		nodeID := strings.TrimSpace(node.ID)
		// `nodeRole` stores the value produced by this operation.
		nodeRole := strings.ToLower(strings.TrimSpace(node.Role))
		if nodeID != "" && nodeRole != "validator" && !isCoreValidator(nodeID) {
			nodes[0].ID = nodeID
		}
	}
	// `sourceKey` stores the key used to access the related value.
	sourceKey := publicNodesSourceKey(nodes)
	publicNodeRegistryMu.Lock()
	if !force && sourceKey == publicNodeRegistryCachedKey && !publicNodeRegistryCheckedAt.IsZero() && time.Since(publicNodeRegistryCheckedAt) < publicNodeProbeTTL {
		// `cached` stores the value produced by this operation.
		cached := publicNodeRegistryCached
		publicNodeRegistryMu.Unlock()
		return cached
	}
	publicNodeRegistryMu.Unlock()

	// `views` stores the value produced by this operation.
	views := make([]publicNodeHealthView, 0, len(nodes))
	// `cfg` tracks the configuration used by this operation.
	for _, cfg := range nodes {
		// `view` stores the value produced by this operation.
		view := probePublicNode(cfg)
		if strings.EqualFold(view.Role, "validator") || view.SuspiciousReason == "validator_rpc_not_allowed" {
			continue
		}
		views = append(views, view)
	}
	annotatePublicNodeHeightLag(views)
	assignPublicNodeActiveGateway(views)
	// `healthy`, `stable`, `warning`, `unhealthy`, and `active` store the value produced by this operation.
	healthy, stable, warning, unhealthy, active := 0, 0, 0, 0, 0
	// `activeBest` stores the value used by this operation.
	var activeBest *publicNodeHealthView
	// `view` tracks the current values while iterating.
	for _, view := range views {
		if view.Healthy {
			healthy++
		}
		switch strings.ToLower(strings.TrimSpace(view.HealthState)) {
		case "healthy":
			stable++
		case "warning":
			warning++
		case "unhealthy":
			unhealthy++
		default:
			if view.Healthy {
				warning++
			} else {
				unhealthy++
			}
		}
		if view.ActiveGateway {
			active++
			// `copyView` stores the value produced by this operation.
			copyView := view
			activeBest = &copyView
		}
	}
	// `status` stores the value produced by this operation.
	status := "down"
	if stable == len(views) && stable > 0 {
		status = "healthy"
	} else if healthy > 0 || warning > 0 {
		status = "degraded"
	}
	// `payload` stores the value produced by this operation.
	payload := publicNodesPayload{
		Status:      status,
		ChainID:     protocolChainID(),
		GenesisHash: expectedPublicNodeGenesisHash(),
		Healthy:     healthy,
		Stable:      stable,
		Warning:     warning,
		Unhealthy:   unhealthy,
		Active:      active,
		Total:       len(views),
		Nodes:       views,
		TS:          time.Now().Unix(),
	}
	// `best` stores the value produced by this operation.
	best := activeBest
	if best == nil {
		best = publicNodeBest(views)
	}
	if best != nil {
		payload.Best = best.RPCURL
		// `copyBest` stores the value produced by this operation.
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

// annotatePublicNodeHeightLag implements the annotate public node height lag helper.
func annotatePublicNodeHeightLag(views []publicNodeHealthView) {
	// `maxHeight` stores the value produced by this operation.
	maxHeight := uint64(0)
	// `view` tracks the current values while iterating.
	for _, view := range views {
		if view.Height > maxHeight {
			maxHeight = view.Height
		}
	}
	// `i` tracks the current position in the related collection.
	for i := range views {
		if maxHeight > 0 && views[i].Height > 0 && maxHeight >= views[i].Height {
			views[i].HeightLagBlocks = maxHeight - views[i].Height
		}
		views[i].Score = publicNodeScore(views[i])
		applyPublicNodeHealthHysteresis(&views[i])
	}
}

// publicNodeHealthKey implements the public node health key helper.
func publicNodeHealthKey(view publicNodeHealthView) string {
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(view.ID) + "|" + strings.TrimSpace(view.RPCURL)
	if strings.TrimSpace(key) == "|" {
		return strings.TrimSpace(view.RPCURL)
	}
	return key
}

// publicNodeHardFailReason implements the public node hard fail reason helper.
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

// publicNodeBadReason implements the public node bad reason helper.
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

// publicNodeWarningReason implements the public node warning reason helper.
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
	if view.LatencyMS > 2500 {
		return "high_latency"
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

// publicNodeBadThreshold implements the public node bad threshold helper.
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

// applyPublicNodeHealthHysteresis applies public node health hysteresis.
func applyPublicNodeHealthHysteresis(view *publicNodeHealthView) {
	if view == nil {
		return
	}
	// `key` stores the key used to access the related value.
	key := publicNodeHealthKey(*view)
	// `now` stores the value produced by this operation.
	now := time.Now().Unix()
	// `hardReason` stores the value produced by this operation.
	hardReason := publicNodeHardFailReason(*view)
	// `badReason` stores the value produced by this operation.
	badReason := publicNodeBadReason(*view)
	// `warnReason` stores the value produced by this operation.
	warnReason := publicNodeWarningReason(*view)

	publicNodeRegistryMu.Lock()
	// `sample` stores the value produced by this operation.
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
		// `threshold` stores the value produced by this operation.
		threshold := publicNodeBadThreshold(badReason)
		// `unhealthy` stores the value produced by this operation.
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
	// `healthy` stores the value produced by this operation.
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

// probePublicNode implements the probe public node helper.
func probePublicNode(cfg PublicNodeConfig) publicNodeHealthView {
	// `view` stores the value produced by this operation.
	view := publicNodeHealthView{
		ID:            cfg.ID,
		RPCURL:        cfg.RPCURL,
		GatewayRPCURL: publicNodeGatewayRPCURL(cfg.ID),
		Role:          cfg.Role,
		PublicGateway: true,
		ChainID:       protocolChainID(),
		GenesisHash:   expectedPublicNodeGenesisHash(),
		Version:       cfg.Version,
		Country:       cfg.Country,
		ASN:           cfg.ASN,
		Cloud:         cfg.Cloud,
		Region:        cfg.Region,
		LastChecked:   time.Now().Unix(),
	}
	// `started` stores the value produced by this operation.
	started := time.Now()
	// `client` stores the value produced by this operation.
	client := &http.Client{Timeout: 8 * time.Second}
	// `statusCode`, `statusData`, and `err` store the error produced by this operation.
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
	view.Syncing = boolFromAny(statusData["syncing"])
	// `ok` stores whether the related condition is satisfied.
	if _, ok := statusData["sync_complete"]; ok {
		view.SyncComplete = boolFromAny(statusData["sync_complete"])
	} else {
		view.SyncComplete = !view.Syncing
	}
	// `role` stores the value produced by this operation.
	if role := strings.ToLower(strings.TrimSpace(stringFromAny(statusData["role"]))); role != "" {
		view.Role = role
	}
	// `chainID` stores the value produced by this operation.
	if chainID := strings.TrimSpace(stringFromAny(statusData["chain_id"])); chainID != "" {
		view.ChainID = chainID
	}
	// `genesis` stores the value produced by this operation.
	if genesis := strings.TrimSpace(stringFromAny(firstNonNil(statusData["genesis_hash"], statusData["expected_genesis_hash"]))); genesis != "" {
		view.GenesisHash = strings.ToLower(strings.TrimPrefix(genesis, "0x"))
	}
	// `version` stores the value produced by this operation.
	if version := strings.TrimSpace(stringFromAny(statusData["version"])); version != "" {
		view.Version = version
	}
	if view.Role == "validator" || isCoreValidator(view.ID) {
		view.SuspiciousReason = "validator_rpc_not_allowed"
		view.Score = 0
		return view
	}
	// `want` stores the value produced by this operation.
	if strings.TrimSpace(view.ChainID) != "" && !isProtocolChainID(view.ChainID) {
		view.SuspiciousReason = "chain_id_mismatch"
		view.Score = 0
		return view
	}
	// `want` stores the value produced by this operation.
	if want := expectedPublicNodeGenesisHash(); want != "" && view.GenesisHash != "" && !strings.EqualFold(want, view.GenesisHash) {
		view.SuspiciousReason = "genesis_hash_mismatch"
		view.Score = 0
		return view
	}
	// `cmdStatus`, `cmdData`, and `cmdErr` store the error produced by this operation.
	cmdStatus, cmdData, cmdErr := publicNodeFetchJSON(client, cfg.RPCURL+"/consensus/mode")
	if cmdErr == nil && cmdStatus == http.StatusOK {
		view.ConsensusMode = strings.ToUpper(strings.TrimSpace(stringFromAny(cmdData["mode"])))
		// `lag` stores the value produced by this operation.
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

// publicNodeGatewayRPCURL implements the public node gateway rpcurl helper.
func publicNodeGatewayRPCURL(id string) string {
	id = strings.Trim(strings.TrimSpace(id), "/")
	if id == "" {
		return ""
	}
	return strings.TrimRight(defaultPublicNodeRPC, "/") + "/public-rpc/" + url.PathEscape(id)
}

// publicNodeFetchJSON implements the public node fetch json helper.
func publicNodeFetchJSON(client *http.Client, rawURL string) (int, map[string]any, error) {
	// `req` and `err` store the error produced by this operation.
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "msc-public-node-registry/1")
	// `res` and `err` store the error produced by this operation.
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	// `data` stores the value used by this operation.
	var data map[string]any
	// `err` stores the error produced by this operation.
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, data, nil
}

// publicNodeScore implements the public node score helper.
func publicNodeScore(view publicNodeHealthView) int {
	if view.SuspiciousReason != "" || view.StatusCode != http.StatusOK {
		return 0
	}
	// `score` stores the value produced by this operation.
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
	if view.Syncing || !view.SyncComplete {
		score -= 12
	}
	if view.LatencyMS > 2500 {
		score -= 12
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// publicNodeBest implements the public node best helper.
func publicNodeBest(nodes []publicNodeHealthView) *publicNodeHealthView {
	if len(nodes) == 0 {
		return nil
	}
	// `sorted` stores the value produced by this operation.
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

// assignPublicNodeActiveGateway implements the assign public node active gateway helper.
func assignPublicNodeActiveGateway(nodes []publicNodeHealthView) {
	// `bestIndex` stores the current position in the related collection.
	bestIndex := -1
	// `i` tracks the current position in the related collection.
	for i := range nodes {
		// `reason` stores the value produced by this operation.
		reason := publicNodeActiveGatewayExcludedReason(nodes[i])
		if reason != "" {
			nodes[i].ActiveGateway = false
			nodes[i].ExcludedReason = reason
			continue
		}
		nodes[i].SelectedReason = "eligible"
		if bestIndex < 0 ||
			nodes[i].HeightLagBlocks < nodes[bestIndex].HeightLagBlocks ||
			(nodes[i].HeightLagBlocks == nodes[bestIndex].HeightLagBlocks && nodes[i].FinalityLag < nodes[bestIndex].FinalityLag) ||
			(nodes[i].HeightLagBlocks == nodes[bestIndex].HeightLagBlocks && nodes[i].FinalityLag == nodes[bestIndex].FinalityLag && nodes[i].Score > nodes[bestIndex].Score) ||
			(nodes[i].HeightLagBlocks == nodes[bestIndex].HeightLagBlocks && nodes[i].FinalityLag == nodes[bestIndex].FinalityLag && nodes[i].Score == nodes[bestIndex].Score && nodes[i].LatencyMS < nodes[bestIndex].LatencyMS) {
			bestIndex = i
		}
	}
	if bestIndex >= 0 {
		// `i` tracks the current position in the related collection.
		for i := range nodes {
			if i == bestIndex {
				nodes[i].ActiveGateway = true
				nodes[i].SelectedReason = "highest_score_lowest_lag"
				nodes[i].ExcludedReason = ""
				continue
			}
			if nodes[i].SelectedReason == "eligible" {
				nodes[i].ActiveGateway = false
				nodes[i].SelectedReason = ""
				nodes[i].ExcludedReason = "standby_lower_score"
			}
		}
		return
	}
	// `fallback` stores the value produced by this operation.
	fallback := publicNodeBest(nodes)
	if fallback == nil {
		return
	}
	// `i` tracks the current position in the related collection.
	for i := range nodes {
		if nodes[i].ID == fallback.ID && nodes[i].RPCURL == fallback.RPCURL {
			nodes[i].ActiveGateway = true
			nodes[i].SelectedReason = "fallback_no_strict_backend"
			nodes[i].ExcludedReason = ""
			return
		}
	}
}

// publicNodeActiveGatewayExcludedReason implements the public node active gateway excluded reason helper.
func publicNodeActiveGatewayExcludedReason(view publicNodeHealthView) string {
	if !view.Healthy || strings.EqualFold(view.HealthState, "unhealthy") {
		if view.HealthReason != "" {
			return view.HealthReason
		}
		return "unhealthy"
	}
	if !strings.EqualFold(view.HealthState, "healthy") {
		if view.HealthReason != "" {
			return view.HealthReason
		}
		return "not_stably_healthy"
	}
	if view.HeightLagBlocks > 2 {
		return "height_lag"
	}
	if view.FinalityLag > 2 {
		return "finality_lag"
	}
	if view.LastBlockAgeSeconds >= 12 {
		return "slow_block_age"
	}
	if view.LatencyMS > 2500 {
		return "high_latency"
	}
	if view.Syncing || !view.SyncComplete {
		return "syncing"
	}
	switch strings.ToUpper(strings.TrimSpace(view.ConsensusMode)) {
	case "PARTITION", "HALTED", "ATTACK", "RECOVERY", "DEGRADED":
		return strings.ToLower(strings.TrimSpace(view.ConsensusMode))
	}
	return ""
}

// expectedPublicNodeGenesisHash implements the expected public node genesis hash helper.
func expectedPublicNodeGenesisHash() string {
	// `v` tracks the current values while iterating.
	for _, v := range []string{ConfigGenesisHash, GenesisHashExpected, GenesisHash} {
		v = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v), "0x"))
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonNil implements the first non nil helper.
func firstNonNil(values ...any) any {
	// `value` tracks the value currently being processed.
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// uint64FromAny implements the uint64 from any helper.
func uint64FromAny(value any) uint64 {
	// `v` stores the value produced by this operation.
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
		// `n` stores the value produced by this operation.
		n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

// boolFromAny implements the bool from any helper.
func boolFromAny(value any) bool {
	// `v` stores the value produced by this operation.
	switch v := value.(type) {
	case bool:
		return v
	case string:
		v = strings.TrimSpace(strings.ToLower(v))
		return v == "1" || v == "true" || v == "yes" || v == "y"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	case uint64:
		return v != 0
	}
	return false
}

// stringFromAny implements the string from any helper.
func stringFromAny(value any) string {
	// `v` stores the value produced by this operation.
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

// maxInt returns the maximum int.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// handlePublicNodes handles public nodes.
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
	// `payload` stores the value produced by this operation.
	payload := publicNodesSnapshot(s.Node, strings.TrimSpace(r.URL.Query().Get("refresh")) == "1")
	writeV1Data(w, http.StatusOK, payload)
}
