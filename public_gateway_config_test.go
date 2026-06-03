package main

import (
	"os"
	"strings"
	"testing"
)

func TestPublicGatewayProductionGuards(t *testing.T) {
	body, err := os.ReadFile("scripts/ec2_public_ui_gateway.ps1")
	if err != nil {
		t.Fatalf("read gateway script: %v", err)
	}
	script := string(body)

	required := []string{
		`[string]$Domain = "mscblockexplorer.in"`,
		`[string]$RpcTarget = "127.0.0.1:26665"`,
		`DNS preflight failed`,
		`certbot --nginx`,
		`--redirect`,
		`--hsts`,
		`limit_req_zone $binary_remote_addr zone=msc_read`,
		`limit_req_zone $binary_remote_addr zone=msc_write`,
		`limit_req zone=msc_rpc`,
		`light/headers`,
		`public-nodes`,
		`proof/balance`,
		`proof/receipt`,
		`"consensus_mode": mode`,
		`"last_block_age_seconds": block_age`,
		`sticky_stable_healthy_backend`,
		`standby_lower_score`,
		`location = /metrics`,
		`return 404;`,
		`auth_basic "MSC DTL IDE"`,
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Fatalf("gateway script missing %q", want)
		}
	}

	rpcLocation := `location ~ ^/(rpc|jsonrpc|v1/rpc)$`
	idx := strings.Index(script, rpcLocation)
	if idx < 0 {
		t.Fatalf("gateway script missing public rpc location")
	}
	end := strings.Index(script[idx:], `location ~ ^/(sendTx`)
	if end < 0 {
		t.Fatalf("gateway script missing write rpc location after public rpc")
	}
	publicRPC := script[idx : idx+end]
	if strings.Contains(publicRPC, "auth_basic") {
		t.Fatalf("public RPC location must not trigger browser basic-auth prompts")
	}
}
