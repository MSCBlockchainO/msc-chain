package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPrometheusAlertsIncludeNetworkHardeningRules(t *testing.T) {
	data, err := os.ReadFile("monitoring/prometheus/alerts.yml")
	if err != nil {
		t.Fatalf("read alerts: %v", err)
	}
	body := string(data)
	for _, alert := range []string{
		"NoBlockFor60Seconds",
		"FinalityGapOver20",
		"PeersBelow3",
		"DiskUsageOver80Percent",
		"QuorumFailure",
		"ValidatorOffline",
		"SnapshotFailure",
		"HighPeerDisconnectRate",
		"LowPeerCount",
		"HighInvalidMessageRate",
		"BlockPropagationSlow",
		"ConsensusPeerLoss",
		"PartitionRisk",
	} {
		if !strings.Contains(body, "alert: "+alert) {
			t.Fatalf("expected alert %s in alerts.yml", alert)
		}
	}
	for _, expr := range []string{
		"msc_consensus_last_block_age_seconds{job=\"msc_nodes\"} > 60",
		"msc_finality_gap{job=\"msc_nodes\"} > 20",
		"msc_peers_connected{job=\"msc_nodes\"} < 3",
		"msc_disk_usage_percent{job=\"msc_nodes\"} > 80",
		"msc_quorum_failures_total{job=\"msc_nodes\"} > 0",
		"msc_validator_health_offline{job=\"msc_nodes\"} > 0",
		"increase(msc_snapshot_failures_total{job=\"msc_nodes\"}[10m]) > 0",
	} {
		if !strings.Contains(body, expr) {
			t.Fatalf("expected alert expression %q in alerts.yml", expr)
		}
	}
}

func TestGrafanaMainnetLaunchGatesDashboard(t *testing.T) {
	data, err := os.ReadFile("monitoring/grafana/dashboards/msc-mainnet-launch-gates.json")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal(data, &dashboard); err != nil {
		t.Fatalf("dashboard JSON invalid: %v", err)
	}
	body := string(data)
	for _, panel := range []string{
		"No Block > 60s",
		"Finality Gap > 20",
		"Peers < 3",
		"Disk > 80%",
		"Quorum Failure",
		"Validator Offline",
		"Snapshot Failure",
	} {
		if !strings.Contains(body, panel) {
			t.Fatalf("expected dashboard panel %q", panel)
		}
	}
}
