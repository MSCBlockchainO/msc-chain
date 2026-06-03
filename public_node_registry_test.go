package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func resetPublicNodeRegistryTestState(t *testing.T) {
	t.Helper()
	publicNodeRegistryMu.Lock()
	publicNodeRegistryCachedKey = ""
	publicNodeRegistryCheckedAt = time.Time{}
	publicNodeRegistryCached = publicNodesPayload{}
	publicNodeHealthMemory = map[string]publicNodeHealthSample{}
	publicNodeRegistryMu.Unlock()
}

func withPublicNodeRegistryTestEnv(t *testing.T, raw string) {
	t.Helper()
	oldNodes := ConfigPublicNodes
	oldChainID := ChainID
	oldConfigGenesisHash := ConfigGenesisHash
	oldGenesisHashExpected := GenesisHashExpected
	oldAllow := os.Getenv("MSC_PUBLIC_NODES_ALLOW_UNSAFE")
	oldEnv := os.Getenv("MSC_PUBLIC_NODES")
	oldRegistry := os.Getenv("MSC_PUBLIC_NODE_REGISTRY")
	oldEndpoints := os.Getenv("MSC_PUBLIC_RPC_ENDPOINTS")
	t.Cleanup(func() {
		ConfigPublicNodes = oldNodes
		ChainID = oldChainID
		ConfigGenesisHash = oldConfigGenesisHash
		GenesisHashExpected = oldGenesisHashExpected
		_ = os.Setenv("MSC_PUBLIC_NODES_ALLOW_UNSAFE", oldAllow)
		_ = os.Setenv("MSC_PUBLIC_NODES", oldEnv)
		_ = os.Setenv("MSC_PUBLIC_NODE_REGISTRY", oldRegistry)
		_ = os.Setenv("MSC_PUBLIC_RPC_ENDPOINTS", oldEndpoints)
		resetPublicNodeRegistryTestState(t)
	})
	ConfigPublicNodes = nil
	ChainID = "91938"
	ConfigGenesisHash = "abc"
	GenesisHashExpected = "abc"
	_ = os.Setenv("MSC_PUBLIC_NODES_ALLOW_UNSAFE", "1")
	_ = os.Setenv("MSC_PUBLIC_NODES", raw)
	_ = os.Unsetenv("MSC_PUBLIC_NODE_REGISTRY")
	_ = os.Unsetenv("MSC_PUBLIC_RPC_ENDPOINTS")
	resetPublicNodeRegistryTestState(t)
}

func newPublicNodeProbeServer(role string, height uint64, finalized uint64, mode string, genesis string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"node_id":                role + "-node",
				"role":                   role,
				"chain_id":               "91938",
				"genesis_hash":           genesis,
				"height":                 height,
				"finalized_height":       finalized,
				"last_block_age_seconds": 2,
				"peers":                  4,
				"network_health":         "healthy",
				"version":                Version,
			})
		case "/consensus/mode":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"mode":         mode,
				"finality_lag": height - finalized,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPublicNodesEndpointReturnsTrustedFullNodes(t *testing.T) {
	full := newPublicNodeProbeServer("full", 120, 120, "NORMAL", "abc")
	defer full.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+full.URL+"|full|US|AS14618|AWS|us-east-1")

	server := NewServer(&Node{ID: "F", Role: "full"})
	req := httptest.NewRequest(http.MethodGet, "/v1/public-nodes", nil)
	rr := httptest.NewRecorder()
	server.handlePublicNodes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var wrapped struct {
		Success bool               `json:"success"`
		Data    publicNodesPayload `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("decode public nodes: %v", err)
	}
	payload := wrapped.Data
	if payload.Total != 1 || payload.Healthy != 1 {
		t.Fatalf("unexpected public node counts: %+v", payload)
	}
	if payload.Nodes[0].ID != "F" || payload.Nodes[0].RPCURL != full.URL || !payload.Nodes[0].Healthy {
		t.Fatalf("unexpected public node view: %+v", payload.Nodes[0])
	}
}

func TestPublicNodesDefaultUsesServingFullNodeID(t *testing.T) {
	withPublicNodeRegistryTestEnv(t, "")

	server := NewServer(&Node{ID: "G", Role: "full"})
	payload := publicNodesSnapshot(server.Node, true)
	if payload.Total != 1 {
		t.Fatalf("expected one default public node, got %d", payload.Total)
	}
	if payload.Nodes[0].ID != "G" {
		t.Fatalf("expected serving full-node id G, got %q", payload.Nodes[0].ID)
	}
}

func TestPublicNodesThroughRPCHardening(t *testing.T) {
	oldRequireRead := ConfigRPCRequireAuthForReadEndpoints
	oldAPIToken := apiToken
	defer func() {
		ConfigRPCRequireAuthForReadEndpoints = oldRequireRead
		apiToken = oldAPIToken
	}()
	ConfigRPCRequireAuthForReadEndpoints = false
	apiToken = ""

	full := newPublicNodeProbeServer("full", 120, 120, "NORMAL", "abc")
	defer full.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+full.URL+"|full")

	server := NewServer(&Node{ID: "F", Role: "full"})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/public-nodes", server.handlePublicNodes)
	ts := httptest.NewServer(withRPCHardening(server.Node, mux))
	defer ts.Close()

	res, err := http.Get(ts.URL + "/v1/public-nodes")
	if err != nil {
		t.Fatalf("get public nodes through hardening: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

func TestPublicNodesExcludesValidatorRPCs(t *testing.T) {
	full := newPublicNodeProbeServer("full", 120, 120, "NORMAL", "abc")
	validator := newPublicNodeProbeServer("validator", 120, 120, "NORMAL", "abc")
	defer full.Close()
	defer validator.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+full.URL+"|full;A|"+validator.URL+"|validator")

	payload := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	if payload.Total != 1 {
		t.Fatalf("expected validator RPC to be excluded, got total=%d nodes=%+v", payload.Total, payload.Nodes)
	}
	if payload.Nodes[0].ID != "F" {
		t.Fatalf("unexpected remaining node: %+v", payload.Nodes[0])
	}
}

func TestPublicNodesMarksWrongGenesisSuspicious(t *testing.T) {
	full := newPublicNodeProbeServer("full", 120, 120, "NORMAL", "wrong")
	defer full.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+full.URL+"|full")

	payload := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	if payload.Total != 1 {
		t.Fatalf("unexpected total=%d", payload.Total)
	}
	if payload.Nodes[0].Healthy || payload.Nodes[0].SuspiciousReason != "genesis_hash_mismatch" {
		t.Fatalf("expected suspicious wrong genesis node, got %+v", payload.Nodes[0])
	}
}

func TestPublicNodesComputesHeightLag(t *testing.T) {
	fast := newPublicNodeProbeServer("full", 45680, 45680, "NORMAL", "abc")
	slow := newPublicNodeProbeServer("full", 45677, 45677, "NORMAL", "abc")
	defer fast.Close()
	defer slow.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+fast.URL+"|full;G|"+slow.URL+"|full")

	payload := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	if payload.Total != 2 {
		t.Fatalf("expected two public nodes, got %d", payload.Total)
	}
	lagByID := map[string]uint64{}
	for _, node := range payload.Nodes {
		lagByID[node.ID] = node.HeightLagBlocks
	}
	if lagByID["F"] != 0 || lagByID["G"] != 3 {
		t.Fatalf("unexpected height lag map: %+v nodes=%+v", lagByID, payload.Nodes)
	}
}

func TestPublicNodesActiveGatewayExcludesLaggingBackend(t *testing.T) {
	fast := newPublicNodeProbeServer("full", 45680, 45680, "NORMAL", "abc")
	slow := newPublicNodeProbeServer("full", 45677, 45677, "NORMAL", "abc")
	defer fast.Close()
	defer slow.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+slow.URL+"|full;G|"+fast.URL+"|full")

	_ = publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	payload := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	f := findPublicNodeByID(payload.Nodes, "F")
	g := findPublicNodeByID(payload.Nodes, "G")
	if f == nil || g == nil {
		t.Fatalf("expected F/G nodes, got %+v", payload.Nodes)
	}
	if f.ActiveGateway || f.ExcludedReason != "height_lag" {
		t.Fatalf("expected lagging F excluded from active gateway, got %+v", f)
	}
	if !g.ActiveGateway || g.SelectedReason == "" {
		t.Fatalf("expected healthy G active gateway, got %+v", g)
	}
}

func TestPublicNodesActiveGatewaySelectsSingleBestStableBackend(t *testing.T) {
	nodes := []publicNodeHealthView{
		{
			ID:                  "F",
			RPCURL:              "https://example.test/public-rpc/F",
			Role:                "full",
			Healthy:             true,
			HealthState:         "healthy",
			Height:              100,
			FinalizedHeight:     100,
			FinalityLag:         0,
			LastBlockAgeSeconds: 1,
			LatencyMS:           900,
			ConsensusMode:       "NORMAL",
			SyncComplete:        true,
			Score:               90,
		},
		{
			ID:                  "G",
			RPCURL:              "https://example.test/public-rpc/G",
			Role:                "full",
			Healthy:             true,
			HealthState:         "healthy",
			Height:              100,
			FinalizedHeight:     100,
			FinalityLag:         0,
			LastBlockAgeSeconds: 1,
			LatencyMS:           40,
			ConsensusMode:       "NORMAL",
			SyncComplete:        true,
			Score:               98,
		},
	}
	assignPublicNodeActiveGateway(nodes)
	active := 0
	for _, node := range nodes {
		if node.ActiveGateway {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("expected exactly one active public backend, got %d nodes=%+v", active, nodes)
	}
	if !nodes[1].ActiveGateway || nodes[1].SelectedReason != "highest_score_lowest_lag" {
		t.Fatalf("expected higher-scored G active, got %+v", nodes)
	}
	if nodes[0].ActiveGateway || nodes[0].ExcludedReason != "standby_lower_score" {
		t.Fatalf("expected F standby lower score, got %+v", nodes[0])
	}
}

func TestPublicNodesActiveGatewayExcludesSlowOrHighLatencyBackend(t *testing.T) {
	slow := publicNodeHealthView{
		ID:                  "F",
		RPCURL:              "https://example.test/public-rpc/F",
		Role:                "full",
		Healthy:             true,
		HealthState:         "healthy",
		Height:              100,
		FinalizedHeight:     100,
		FinalityLag:         0,
		LastBlockAgeSeconds: 14,
		LatencyMS:           80,
		ConsensusMode:       "NORMAL",
		SyncComplete:        true,
		Score:               96,
	}
	if got := publicNodeActiveGatewayExcludedReason(slow); got != "slow_block_age" {
		t.Fatalf("expected slow block age exclusion, got %q", got)
	}
	slow.LastBlockAgeSeconds = 1
	slow.LatencyMS = 2600
	if got := publicNodeActiveGatewayExcludedReason(slow); got != "high_latency" {
		t.Fatalf("expected high latency exclusion, got %q", got)
	}
}

func TestPublicNodeHealthHysteresisForHeightLag(t *testing.T) {
	fast := newPublicNodeProbeServer("full", 100, 100, "NORMAL", "abc")
	slowHeight := uint64(75)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"node_id":                "G",
				"role":                   "full",
				"chain_id":               "91938",
				"genesis_hash":           "abc",
				"height":                 slowHeight,
				"finalized_height":       slowHeight,
				"last_block_age_seconds": 2,
				"peers":                  4,
				"network_health":         "healthy",
			})
		case "/consensus/mode":
			_ = json.NewEncoder(w).Encode(map[string]any{"mode": "NORMAL", "finality_lag": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fast.Close()
	defer slow.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+fast.URL+"|full;G|"+slow.URL+"|full")

	first := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	g := findPublicNodeByID(first.Nodes, "G")
	if g == nil || g.HealthState != "warning" || !g.Healthy || g.SuspiciousReason != "" {
		t.Fatalf("first lag sample should be warning, got %+v", g)
	}

	second := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	g = findPublicNodeByID(second.Nodes, "G")
	if g == nil || g.HealthState != "warning" || !g.Healthy || g.SuspiciousReason != "" {
		t.Fatalf("second lag sample should still be warning, got %+v", g)
	}

	third := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	g = findPublicNodeByID(third.Nodes, "G")
	if g == nil || g.HealthState != "unhealthy" || g.Healthy || g.SuspiciousReason != "height_lag" {
		t.Fatalf("third lag sample should be unhealthy height_lag, got %+v", g)
	}

	slowHeight = 100
	recovering := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	g = findPublicNodeByID(recovering.Nodes, "G")
	if g == nil || g.HealthState != "warning" || !g.Healthy {
		t.Fatalf("first clean sample after lag should be warning, got %+v", g)
	}

	recovered := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	g = findPublicNodeByID(recovered.Nodes, "G")
	if g == nil || g.HealthState != "healthy" || !g.Healthy || g.HealthReason != "healthy" {
		t.Fatalf("second clean sample should be healthy, got %+v", g)
	}
}

func TestPublicNodeHealthHysteresisForTimeout(t *testing.T) {
	withPublicNodeRegistryTestEnv(t, "F|http://127.0.0.1:1|full")

	first := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	node := findPublicNodeByID(first.Nodes, "F")
	if node == nil || node.HealthState != "warning" || !node.Healthy || node.SuspiciousReason != "" {
		t.Fatalf("first timeout should be warning, got %+v", node)
	}

	second := publicNodesSnapshot(&Node{ID: "F", Role: "full"}, true)
	node = findPublicNodeByID(second.Nodes, "F")
	if node == nil || node.HealthState != "unhealthy" || node.Healthy || node.SuspiciousReason != "timeout" {
		t.Fatalf("second timeout should be unhealthy, got %+v", node)
	}
}

func findPublicNodeByID(nodes []publicNodeHealthView, id string) *publicNodeHealthView {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}
