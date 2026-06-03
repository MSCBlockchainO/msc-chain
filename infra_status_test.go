package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeInfraStatusTest(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var wrapped struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		t.Fatalf("decode public infra status: %v", err)
	}
	if !wrapped.Success {
		t.Fatalf("expected v1 success wrapper, got %s", string(body))
	}
	return wrapped.Data
}

func TestPublicInfraStatusIncludesIndependentLayers(t *testing.T) {
	full := newPublicNodeProbeServer("full", 120, 120, "NORMAL", "abc")
	defer full.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+full.URL+"|full")

	server := NewServer(&Node{ID: "F", Role: "full"})
	req := httptest.NewRequest(http.MethodGet, "/v1/public/status", nil)
	rr := httptest.NewRecorder()
	server.handleV1PublicInfraStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	payload := decodeInfraStatusTest(t, rr.Body.Bytes())
	for _, key := range []string{"chain", "validators", "public_rpc", "archive", "indexer", "storage", "light_client", "gateway"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing %q in public infra payload: %+v", key, payload)
		}
	}
	light, ok := payload["light_client"].(map[string]any)
	if !ok || light["balance_proof"] != "/proof/balance" || light["headers"] != "/light/headers" {
		t.Fatalf("light client proof capability missing: %+v", payload["light_client"])
	}
	gateway, ok := payload["gateway"].(map[string]any)
	if !ok || gateway["metrics_public"] != false {
		t.Fatalf("gateway metrics policy must stay non-public: %+v", payload["gateway"])
	}
}

func TestPublicInfraStatusKeepsValidatorRPCsPrivate(t *testing.T) {
	full := newPublicNodeProbeServer("full", 120, 120, "NORMAL", "abc")
	validator := newPublicNodeProbeServer("validator", 120, 120, "NORMAL", "abc")
	defer full.Close()
	defer validator.Close()
	withPublicNodeRegistryTestEnv(t, "F|"+full.URL+"|full;A|"+validator.URL+"|validator")

	server := NewServer(&Node{ID: "F", Role: "full"})
	req := httptest.NewRequest(http.MethodGet, "/v1/public/status", nil)
	rr := httptest.NewRecorder()
	server.handleV1PublicInfraStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	payload := decodeInfraStatusTest(t, rr.Body.Bytes())
	validators, ok := payload["validators"].(map[string]any)
	if !ok || validators["validator_rpc"] != "private_only" {
		t.Fatalf("validator RPC policy missing/private-only expected: %+v", payload["validators"])
	}
	publicRPC, ok := payload["public_rpc"].(map[string]any)
	if !ok {
		t.Fatalf("missing public_rpc: %+v", payload)
	}
	nodes, ok := publicRPC["nodes"].([]any)
	if !ok || len(nodes) != 1 {
		t.Fatalf("expected only the full public node, got %+v", publicRPC["nodes"])
	}
	node, _ := nodes[0].(map[string]any)
	if node["id"] == "A" || node["role"] == "validator" {
		t.Fatalf("validator RPC leaked into public status: %+v", node)
	}
}
