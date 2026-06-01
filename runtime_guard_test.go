package main

import "testing"

func TestRuntimeAutoMemoryLimitMiB(t *testing.T) {
	tests := []struct {
		name     string
		totalMiB int64
		role     string
		want     int64
	}{
		{name: "too small host disabled", totalMiB: 1024, role: "validator", want: 0},
		{name: "validator eight gib host", totalMiB: 7800, role: "validator", want: 3510},
		{name: "validator cap", totalMiB: 16000, role: "validator", want: 3584},
		{name: "full eight gib host cap", totalMiB: 7800, role: "full", want: 3072},
		{name: "light eight gib host cap", totalMiB: 7800, role: "light", want: 3072},
		{name: "auto treated as validator", totalMiB: 4096, role: "auto", want: 1843},
		{name: "minimum floor", totalMiB: 2048, role: "full", want: 1536},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runtimeAutoMemoryLimitMiB(tt.totalMiB, tt.role)
			if got != tt.want {
				t.Fatalf("runtimeAutoMemoryLimitMiB(%d, %q) = %d, want %d", tt.totalMiB, tt.role, got, tt.want)
			}
		})
	}
}

func TestLedgerMemoryCacheDepthForRole(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		override string
		want     uint64
	}{
		{name: "validator default", role: "validator", want: 32},
		{name: "auto default", role: "auto", want: 32},
		{name: "full default", role: "full", want: 16},
		{name: "light default", role: "light", want: 16},
		{name: "override", role: "validator", override: "64", want: 64},
		{name: "override capped", role: "validator", override: "999", want: 256},
		{name: "invalid override", role: "full", override: "x", want: 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ledgerMemoryCacheDepthForRole(tt.role, tt.override)
			if got != tt.want {
				t.Fatalf("ledgerMemoryCacheDepthForRole(%q, %q) = %d, want %d", tt.role, tt.override, got, tt.want)
			}
		})
	}
}
