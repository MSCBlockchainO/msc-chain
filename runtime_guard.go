package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const runtimeGuardMiB int64 = 1024 * 1024
const homeValidatorRecommendedRAMMiB int64 = 8192
const homeFullNodeMinimumRAMMiB int64 = 4096

func truthyEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func currentNodeProfile(role string) string {
	profile := strings.ToLower(strings.TrimSpace(os.Getenv("MSC_NODE_PROFILE")))
	if profile == "" && truthyEnv("MSC_LOW_RAM_MODE") {
		profile = "home_low_ram"
	}
	switch profile {
	case "home", "homepc", "home_pc", "home-low-ram":
		return "home_low_ram"
	case "":
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "light" {
			return "light"
		}
		return "standard"
	default:
		return profile
	}
}

func homeLowRAMModeEnabled(role string) bool {
	return currentNodeProfile(role) == "home_low_ram" || truthyEnv("MSC_LOW_RAM_MODE")
}

func homeValidatorSupported(role string, totalMiB int64) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "validator" {
		return totalMiB <= 0 || totalMiB >= homeValidatorRecommendedRAMMiB || truthyEnv("MSC_ALLOW_4GB_VALIDATOR")
	}
	return totalMiB <= 0 || totalMiB >= homeFullNodeMinimumRAMMiB
}

func runtimeMemoryLimitBytes() int64 {
	current := debug.SetMemoryLimit(-1)
	if current < 0 {
		return 0
	}
	return current
}

func runtimeAutoMaxProcs(cpuCount int, role string) int {
	if cpuCount <= 0 {
		return 0
	}
	role = strings.ToLower(strings.TrimSpace(role))
	maxProcs := 2
	switch role {
	case "light":
		maxProcs = 1
	case "archive":
		maxProcs = 4
	case "validator", "auto", "":
		maxProcs = 1
	case "full":
		maxProcs = 2
	default:
		maxProcs = 2
	}
	if cpuCount < maxProcs {
		return cpuCount
	}
	return maxProcs
}

func runtimeHighCPUOverrideEnabled() bool {
	return truthyEnv("MSC_ALLOW_HIGH_NODE_CPU") || truthyEnv("MSC_ALLOW_HIGH_VALIDATOR_CPU")
}

func runtimeCPUHardLimit(role string) int {
	if runtimeHighCPUOverrideEnabled() || truthyEnv("MSC_DISABLE_RUNTIME_CPU_HARD_LIMIT") {
		return 0
	}
	if value, ok := parsePositiveIntEnv("MSC_RUNTIME_CPU_HARD_LIMIT"); ok {
		return value
	}
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "light":
		if value, ok := parsePositiveIntEnv("MSC_LIGHT_CPU_HARD_LIMIT"); ok {
			return value
		}
		return 1
	case "archive":
		if value, ok := parsePositiveIntEnv("MSC_ARCHIVE_CPU_HARD_LIMIT"); ok {
			return value
		}
		return 4
	case "validator", "auto", "":
		if value, ok := parsePositiveIntEnv("MSC_VALIDATOR_CPU_HARD_LIMIT"); ok {
			return value
		}
		return 1
	case "full":
		if value, ok := parsePositiveIntEnv("MSC_FULLNODE_CPU_HARD_LIMIT"); ok {
			return value
		}
		return 2
	default:
		return 2
	}
}

func clampRuntimeMaxProcs(role string, target int) int {
	if target <= 0 {
		return target
	}
	limit := runtimeCPUHardLimit(role)
	if limit > 0 && target > limit {
		return limit
	}
	return target
}

func runtimeWorkerBudget() int {
	workers := runtime.GOMAXPROCS(0)
	if workers <= 0 {
		workers = runtimeAutoMaxProcs(runtime.NumCPU(), "auto")
	}
	if workers <= 0 {
		workers = 1
	}
	if value, ok := parsePositiveIntEnv("MSC_RUNTIME_WORKER_BUDGET"); ok {
		if workers > value {
			workers = value
		}
	} else if !runtimeHighCPUOverrideEnabled() {
		if limit := runtimeCPUHardLimit("auto"); limit > 0 && workers > limit {
			workers = limit
		}
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

func capRuntimeWorkers(workers int) int {
	budget := runtimeWorkerBudget()
	if workers <= 0 {
		return budget
	}
	if workers > budget {
		return budget
	}
	if workers < 1 {
		return 1
	}
	return workers
}

func parsePositiveIntEnv(name string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func runtimeRequestedMaxProcs(role string) (int, string) {
	if value, ok := parsePositiveIntEnv("MSC_RUNTIME_MAX_PROCS"); ok {
		return value, "MSC_RUNTIME_MAX_PROCS"
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "validator" || role == "auto" {
		if value, ok := parsePositiveIntEnv("MSC_VALIDATOR_MAX_PROCS"); ok {
			return value, "MSC_VALIDATOR_MAX_PROCS"
		}
	}
	if strings.TrimSpace(os.Getenv("GOMAXPROCS")) != "" {
		return runtime.GOMAXPROCS(0), "GOMAXPROCS"
	}
	return runtimeAutoMaxProcs(runtime.NumCPU(), role), "auto"
}

func configureRuntimeCPUGuard(role string) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MSC_DISABLE_RUNTIME_CPU_GUARD")), "1") {
		return
	}
	target, source := runtimeRequestedMaxProcs(role)
	if target <= 0 {
		return
	}
	if cpuCount := runtime.NumCPU(); cpuCount > 0 && target > cpuCount {
		target = cpuCount
	}
	requested := target
	target = clampRuntimeMaxProcs(role, target)
	current := runtime.GOMAXPROCS(0)
	if target != current {
		runtime.GOMAXPROCS(target)
		capNote := ""
		if requested != target {
			capNote = fmt.Sprintf(" requested=%d hard_limit=%d", requested, runtimeCPUHardLimit(role))
		}
		log.Printf("[RUNTIME-GUARD] gomaxprocs=%d role=%s source=%s previous=%d cpus=%d%s", target, role, source, current, runtime.NumCPU(), capNote)
		return
	}
	log.Printf("[RUNTIME-GUARD] gomaxprocs_existing=%d role=%s source=%s cpus=%d hard_limit=%d", current, role, source, runtime.NumCPU(), runtimeCPUHardLimit(role))
}

func homeNodeStatusFields(role string) map[string]any {
	totalMiB := hostMemoryTotalMiB()
	return map[string]any{
		"node_profile":                       currentNodeProfile(role),
		"low_ram_mode":                       homeLowRAMModeEnabled(role),
		"memory_limit_bytes":                 runtimeMemoryLimitBytes(),
		"gomaxprocs":                         runtime.GOMAXPROCS(0),
		"host_cpu_count":                     runtime.NumCPU(),
		"cpu_guard_auto_max_procs":           runtimeAutoMaxProcs(runtime.NumCPU(), role),
		"cpu_guard_hard_limit":               runtimeCPUHardLimit(role),
		"cpu_guard_high_cpu_override":        runtimeHighCPUOverrideEnabled(),
		"cpu_guard_disabled":                 truthyEnv("MSC_DISABLE_RUNTIME_CPU_GUARD"),
		"runtime_worker_budget":              runtimeWorkerBudget(),
		"sync_worker_cpu_cap":                runtimeSyncWorkerCap(role),
		"sync_delta_replay_verify_workers":   SyncDeltaReplayVerifyWorkers,
		"sync_ed25519_batch_verify_workers":  SyncEd25519BatchVerifyWorkers,
		"sync_snapshot_parallel_chunks":      SyncSnapshotParallelChunks,
		"host_memory_total_mib":              totalMiB,
		"home_validator_supported":           homeValidatorSupported(role, totalMiB),
		"validator_min_recommended_ram_gb":   homeValidatorRecommendedRAMMiB / 1024,
		"full_node_min_recommended_ram_gb":   homeFullNodeMinimumRAMMiB / 1024,
		"allow_4gb_validator_override":       truthyEnv("MSC_ALLOW_4GB_VALIDATOR"),
		"home_low_ram_profile_env_required":  "MSC_NODE_PROFILE=home_low_ram",
		"home_low_ram_validator_safety_note": "4GB is full/candidate only by default; validator-ready is 8GB recommended.",
	}
}

func runtimeAutoMemoryLimitMiB(totalMiB int64, role string) int64 {
	if totalMiB < 2048 {
		return 0
	}
	role = strings.ToLower(strings.TrimSpace(role))
	var limit int64
	if homeLowRAMModeEnabled(role) {
		limit = totalMiB * 35 / 100
		if role == "validator" {
			limit = totalMiB * 40 / 100
		}
		if limit > 2048 {
			limit = 2048
		}
	} else if role == "full" || role == "light" {
		limit = totalMiB * 40 / 100
		if limit > 3072 {
			limit = 3072
		}
	} else {
		limit = totalMiB * 45 / 100
		if limit > 3584 {
			limit = 3584
		}
	}
	if limit < 1536 {
		limit = 1536
	}
	return limit
}

func ledgerMemoryCacheDepthForRole(role string, override string) uint64 {
	if raw := strings.TrimSpace(override); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err == nil && parsed > 0 {
			if parsed > 32 {
				return 32
			}
			return parsed
		}
	}
	// Each retained height is a full deep copy in both the execution-snapshot
	// and post-commit caches. Keep the default window deliberately small so an
	// 8GB validator cannot accumulate dozens of complete state copies before
	// the Go memory guard can reclaim them. Historical recovery uses persisted
	// snapshots/block replay rather than this in-memory optimization window.
	role = strings.ToLower(strings.TrimSpace(role))
	if homeLowRAMModeEnabled(role) {
		if role == "validator" {
			return 2
		}
		return 1
	}
	if role == "validator" || role == "auto" || role == "full" || role == "light" {
		return 2
	}
	if role == "archive" {
		return 4
	}
	return 2
}

func nodeLedgerMemoryCacheDepth(role string) uint64 {
	return ledgerMemoryCacheDepthForRole(role, os.Getenv("MSC_EXECUTION_LEDGER_CACHE_DEPTH"))
}

func (n *Node) ledgerMemoryCacheDepth() uint64 {
	if n == nil {
		return nodeLedgerMemoryCacheDepth("")
	}
	return nodeLedgerMemoryCacheDepth(n.Role)
}

func maybeReleaseMemoryAfterLedgerCachePrune(removed int, height uint64) {
	if removed <= 0 {
		return
	}
	if removed >= 8 || height%32 == 0 {
		debug.FreeOSMemory()
	}
}

func hostMemoryTotalMiB() int64 {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kib <= 0 {
			return 0
		}
		return kib / 1024
	}
	return 0
}

func configureRuntimeMemoryGuard(role string) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MSC_DISABLE_RUNTIME_MEMORY_GUARD")), "1") {
		return
	}
	limitMiB := int64(0)
	source := "auto"
	if raw := strings.TrimSpace(os.Getenv("MSC_RUNTIME_MEMORY_LIMIT_MIB")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && parsed > 0 {
			limitMiB = parsed
			source = "env"
		}
	}
	if limitMiB == 0 {
		limitMiB = runtimeAutoMemoryLimitMiB(hostMemoryTotalMiB(), role)
	}
	if limitMiB <= 0 {
		return
	}
	desired := limitMiB * runtimeGuardMiB
	current := debug.SetMemoryLimit(-1)
	if current <= 0 || current > desired || strings.TrimSpace(os.Getenv("GOMEMLIMIT")) == "" {
		debug.SetMemoryLimit(desired)
		log.Printf("[RUNTIME-GUARD] memory_limit=%dMiB role=%s source=%s", limitMiB, role, source)
	} else {
		log.Printf("[RUNTIME-GUARD] memory_limit_existing=%dMiB role=%s", current/runtimeGuardMiB, role)
	}
	if strings.TrimSpace(os.Getenv("GOGC")) == "" {
		debug.SetGCPercent(75)
		log.Printf("[RUNTIME-GUARD] gc_percent=75")
	}
}

func runtimePressureModeFor(goroutines int, threshold int, quiet bool) string {
	if quiet {
		return "quiet_backpressure"
	}
	if threshold > 0 {
		if goroutines >= threshold {
			return "pressure"
		}
		if goroutines >= (threshold*8)/10 {
			return "warming"
		}
	}
	return "normal"
}

func runtimePressureModeCode(mode string) float64 {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quiet_backpressure":
		return 3
	case "pressure":
		return 2
	case "warming":
		return 1
	default:
		return 0
	}
}

// SafeGo runs fn in a goroutine and recovers panics so the process stays alive.
func SafeGo(name string, fn func()) {
	go runGuarded(name, fn)
}

func runGuarded(name string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			log.Printf("[RECOVERED] %s panic: %v\n%s", name, r, debug.Stack())
		}
	}()
	fn()
	return
}

func (n *Node) isShuttingDown() bool {
	if n == nil {
		return false
	}
	if n.shutdownCh != nil {
		select {
		case <-n.shutdownCh:
			return true
		default:
		}
	}
	if n.rootCtx != nil {
		select {
		case <-n.rootCtx.Done():
			return true
		default:
		}
	}
	return false
}

func (n *Node) SetRootContext(ctx context.Context, cancel context.CancelFunc) {
	if n == nil {
		return
	}
	n.rootCtx = ctx
	n.rootCancel = cancel
}

func (n *Node) RootContext() context.Context {
	if n == nil || n.rootCtx == nil {
		return context.Background()
	}
	return n.rootCtx
}

func (n *Node) CancelRootContext() {
	if n == nil || n.rootCancel == nil {
		return
	}
	n.rootCancel()
}

func (n *Node) SafeGo(name string, fn func()) {
	if n == nil {
		SafeGo(name, fn)
		return
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		runGuarded(name, fn)
	}()
}

// SafeGoLoop restarts fn if it panics or exits unexpectedly, unless shutdown is in progress.
func (n *Node) SafeGoLoop(name string, restartDelay time.Duration, fn func()) {
	if n == nil {
		SafeGo(name, fn)
		return
	}
	if restartDelay <= 0 {
		restartDelay = 3 * time.Second
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			if n.isShuttingDown() {
				return
			}
			panicked := runGuarded(name, fn)
			if n.isShuttingDown() {
				return
			}
			if !panicked {
				log.Printf("[SUPERVISOR] %s exited; restarting in %s", name, restartDelay)
			} else {
				log.Printf("[SUPERVISOR] %s crashed; restarting in %s", name, restartDelay)
			}
			timer := time.NewTimer(restartDelay)
			select {
			case <-timer.C:
			case <-n.shutdownCh:
				timer.Stop()
				return
			}
		}
	}()
}
