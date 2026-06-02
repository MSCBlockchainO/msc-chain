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
