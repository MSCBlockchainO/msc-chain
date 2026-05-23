package main

import (
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
}
