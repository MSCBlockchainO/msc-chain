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

func TestRuntimeAutoMaxProcs(t *testing.T) {
	tests := []struct {
		name     string
		cpuCount int
		role     string
		want     int
	}{
		{name: "validator capped at two", cpuCount: 8, role: "validator", want: 2},
		{name: "auto capped at two", cpuCount: 16, role: "auto", want: 2},
		{name: "full capped at two", cpuCount: 4, role: "full", want: 2},
		{name: "light capped at one", cpuCount: 4, role: "light", want: 1},
		{name: "archive capped at four", cpuCount: 16, role: "archive", want: 4},
		{name: "small host keeps available cpu", cpuCount: 1, role: "validator", want: 1},
		{name: "invalid cpu disabled", cpuCount: 0, role: "validator", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeAutoMaxProcs(tt.cpuCount, tt.role); got != tt.want {
				t.Fatalf("runtimeAutoMaxProcs(%d, %q) = %d, want %d", tt.cpuCount, tt.role, got, tt.want)
			}
		})
	}
}

func TestRuntimeRequestedMaxProcsHonorsEnv(t *testing.T) {
	t.Setenv("MSC_RUNTIME_MAX_PROCS", "3")
	t.Setenv("MSC_VALIDATOR_MAX_PROCS", "1")
	t.Setenv("GOMAXPROCS", "")

	got, source := runtimeRequestedMaxProcs("validator")
	if got != 3 || source != "MSC_RUNTIME_MAX_PROCS" {
		t.Fatalf("runtime max procs = %d/%s, want 3/MSC_RUNTIME_MAX_PROCS", got, source)
	}
}

func TestRuntimePressureModeFor(t *testing.T) {
	tests := []struct {
		name       string
		goroutines int
		threshold  int
		quiet      bool
		want       string
		wantCode   float64
	}{
		{name: "normal below warm threshold", goroutines: 1000, threshold: 10000, want: "normal", wantCode: 0},
		{name: "warming near threshold", goroutines: 8000, threshold: 10000, want: "warming", wantCode: 1},
		{name: "pressure at threshold", goroutines: 10000, threshold: 10000, want: "pressure", wantCode: 2},
		{name: "quiet overrides pressure", goroutines: 20000, threshold: 10000, quiet: true, want: "quiet_backpressure", wantCode: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runtimePressureModeFor(tt.goroutines, tt.threshold, tt.quiet)
			if got != tt.want {
				t.Fatalf("runtimePressureModeFor(%d, %d, %v) = %q, want %q", tt.goroutines, tt.threshold, tt.quiet, got, tt.want)
			}
			if code := runtimePressureModeCode(got); code != tt.wantCode {
				t.Fatalf("runtimePressureModeCode(%q) = %v, want %v", got, code, tt.wantCode)
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
		{name: "validator default", role: "validator", want: 2},
		{name: "auto default", role: "auto", want: 2},
		{name: "full default", role: "full", want: 2},
		{name: "light default", role: "light", want: 2},
		{name: "archive default", role: "archive", want: 4},
		{name: "override", role: "validator", override: "12", want: 12},
		{name: "override capped", role: "validator", override: "999", want: 32},
		{name: "invalid override", role: "full", override: "x", want: 2},
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

func TestHomeLowRAMProfileRuntimeLimits(t *testing.T) {
	t.Setenv("MSC_NODE_PROFILE", "home_low_ram")
	t.Setenv("MSC_LOW_RAM_MODE", "1")

	if got := runtimeAutoMemoryLimitMiB(4096, "full"); got != 1536 {
		t.Fatalf("4GB home full memory limit = %d, want 1536", got)
	}
	if got := runtimeAutoMemoryLimitMiB(8192, "validator"); got != 2048 {
		t.Fatalf("8GB home validator memory limit = %d, want 2048", got)
	}
	if got := ledgerMemoryCacheDepthForRole("full", ""); got != 1 {
		t.Fatalf("home full ledger cache depth = %d, want 1", got)
	}
	if got := ledgerMemoryCacheDepthForRole("validator", ""); got != 2 {
		t.Fatalf("home validator ledger cache depth = %d, want 2", got)
	}
}

func TestValidatorLedgerMemoryCachesStayAtConfiguredDepth(t *testing.T) {
	t.Setenv("MSC_NODE_PROFILE", "")
	t.Setenv("MSC_LOW_RAM_MODE", "")
	t.Setenv("MSC_EXECUTION_LEDGER_CACHE_DEPTH", "")

	n := &Node{Role: "validator"}
	for height := uint64(1); height <= 20; height++ {
		ledger := NewLedger()
		addBalance(&ledger, CoinSymbol, "cache-test", int(height))
		n.cacheExecutionSnapshotLedger(height, ledger)
		n.cachePostCommitLedger(height, ledger)
	}

	wantDepth := int(ledgerMemoryCacheDepthForRole("validator", ""))
	if got := len(n.snapshotExecutionLedgerByHeight); got != wantDepth {
		t.Fatalf("execution snapshot cache depth = %d, want %d", got, wantDepth)
	}
	if got := len(n.postCommitLedgerByHeight); got != wantDepth {
		t.Fatalf("post-commit cache depth = %d, want %d", got, wantDepth)
	}
	oldestRetained := uint64(20 - wantDepth + 1)
	if _, ok := n.snapshotExecutionLedgerByHeight[oldestRetained]; !ok {
		t.Fatalf("execution snapshot cache missing oldest retained height %d", oldestRetained)
	}
	if _, ok := n.postCommitLedgerByHeight[oldestRetained]; !ok {
		t.Fatalf("post-commit cache missing oldest retained height %d", oldestRetained)
	}
}

func TestHomeValidatorSupportGuard(t *testing.T) {
	t.Setenv("MSC_ALLOW_4GB_VALIDATOR", "")
	if !homeValidatorSupported("full", 4096) {
		t.Fatalf("4GB full/candidate node should be supported")
	}
	if homeValidatorSupported("validator", 4096) {
		t.Fatalf("4GB validator should require explicit override")
	}
	t.Setenv("MSC_ALLOW_4GB_VALIDATOR", "1")
	if !homeValidatorSupported("validator", 4096) {
		t.Fatalf("4GB validator override should be honored")
	}
}
