package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleExplorerBlockIncludesValidatorRegistryHash(t *testing.T) {
	oldAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = oldAuth
	})

	server := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{
				{
					ID:                    251,
					BlockHash:             "block-251",
					PrevHash:              "block-250",
					Type:                  BlockTypeTime,
					Proposer:              "C",
					Timestamp:             25103,
					ValidatorSetHash:      "validator-set-hash",
					ValidatorRegistryHash: "validator-registry-hash",
					ConsensusMode:         "DEGRADED",
					QuorumPolicyVersion:   quorumPolicyVersionV1,
					ActiveReadyCount:      2,
					RequiredQuorum:        2,
					StrictQuorum:          3,
				},
			}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/block?height=251", nil)
	rec := httptest.NewRecorder()
	server.handleExplorerBlock(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, _ := payload["validator_registry_hash"].(string); got != "validator-registry-hash" {
		t.Fatalf("unexpected validator_registry_hash: got=%q want=%q", got, "validator-registry-hash")
	}
	if got, _ := payload["consensus_mode"].(string); got != "DEGRADED" {
		t.Fatalf("unexpected consensus_mode: got=%q want=DEGRADED", got)
	}
	if got := int(payload["required_quorum"].(float64)); got != 2 {
		t.Fatalf("unexpected required_quorum: got=%d want=2", got)
	}
	if got := int(payload["strict_quorum"].(float64)); got != 3 {
		t.Fatalf("unexpected strict_quorum: got=%d want=3", got)
	}

	summary, _ := payload["summary"].(map[string]any)
	if got, _ := summary["validator_registry_hash"].(string); got != "validator-registry-hash" {
		t.Fatalf("unexpected summary validator_registry_hash: got=%q want=%q", got, "validator-registry-hash")
	}
	if got, _ := summary["consensus_mode"].(string); got != "DEGRADED" {
		t.Fatalf("unexpected summary consensus_mode: got=%q want=DEGRADED", got)
	}
	if got := int(summary["active_ready_count"].(float64)); got != 2 {
		t.Fatalf("unexpected summary active_ready_count: got=%d want=2", got)
	}
}
