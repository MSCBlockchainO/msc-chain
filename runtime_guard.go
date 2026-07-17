package main

import (
	"context"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// `runtimeGuardMiB` defines the constant value used by this package.
const runtimeGuardMiB int64 = 1024 * 1024
// `homeValidatorRecommendedRAMMiB` defines the constant value used by this package.
const homeValidatorRecommendedRAMMiB int64 = 8192
// `homeFullNodeMinimumRAMMiB` defines the constant value used by this package.
const homeFullNodeMinimumRAMMiB int64 = 4096

// truthyEnv implements the truthy env helper.
func truthyEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// currentNodeProfile returns current node profile.
func currentNodeProfile(role string) string {
	// `profile` stores the value produced by this operation.
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

// homeLowRAMModeEnabled implements the home low ram mode enabled helper.
func homeLowRAMModeEnabled(role string) bool {
	return currentNodeProfile(role) == "home_low_ram" || truthyEnv("MSC_LOW_RAM_MODE")
}

// homeValidatorSupported implements the home validator supported helper.
func homeValidatorSupported(role string, totalMiB int64) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "validator" {
		return totalMiB <= 0 || totalMiB >= homeValidatorRecommendedRAMMiB || truthyEnv("MSC_ALLOW_4GB_VALIDATOR")
	}
	return totalMiB <= 0 || totalMiB >= homeFullNodeMinimumRAMMiB
}

// runtimeMemoryLimitBytes implements the runtime memory limit bytes helper.
func runtimeMemoryLimitBytes() int64 {
	// `current` stores the value produced by this operation.
	current := debug.SetMemoryLimit(-1)
	if current < 0 {
		return 0
	}
	return current
}

// homeNodeStatusFields implements the home node status fields helper.
func homeNodeStatusFields(role string) map[string]any {
	// `totalMiB` stores the measured quantity used by this operation.
	totalMiB := hostMemoryTotalMiB()
	return map[string]any{
		"node_profile":                       currentNodeProfile(role),
		"low_ram_mode":                       homeLowRAMModeEnabled(role),
		"memory_limit_bytes":                 runtimeMemoryLimitBytes(),
		"host_memory_total_mib":              totalMiB,
		"home_validator_supported":           homeValidatorSupported(role, totalMiB),
		"validator_min_recommended_ram_gb":   homeValidatorRecommendedRAMMiB / 1024,
		"full_node_min_recommended_ram_gb":   homeFullNodeMinimumRAMMiB / 1024,
		"allow_4gb_validator_override":       truthyEnv("MSC_ALLOW_4GB_VALIDATOR"),
		"home_low_ram_profile_env_required":  "MSC_NODE_PROFILE=home_low_ram",
		"home_low_ram_validator_safety_note": "4GB is full/candidate only by default; validator-ready is 8GB recommended.",
	}
}

// runtimeAutoMemoryLimitMiB implements the runtime auto memory limit mi b helper.
func runtimeAutoMemoryLimitMiB(totalMiB int64, role string) int64 {
	if totalMiB < 2048 {
		return 0
	}
	role = strings.ToLower(strings.TrimSpace(role))
	// `limit` stores the value used by this operation.
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

// ledgerMemoryCacheDepthForRole implements the ledger memory cache depth for role helper.
func ledgerMemoryCacheDepthForRole(role string, override string) uint64 {
	// `raw` stores the value produced by this operation.
	if raw := strings.TrimSpace(override); raw != "" {
		// `parsed` and `err` store the error produced by this operation.
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

// nodeLedgerMemoryCacheDepth implements the node ledger memory cache depth helper.
func nodeLedgerMemoryCacheDepth(role string) uint64 {
	return ledgerMemoryCacheDepthForRole(role, os.Getenv("MSC_EXECUTION_LEDGER_CACHE_DEPTH"))
}

// ledgerMemoryCacheDepth implements the ledger memory cache depth helper.
func (n *Node) ledgerMemoryCacheDepth() uint64 {
	if n == nil {
		return nodeLedgerMemoryCacheDepth("")
	}
	return nodeLedgerMemoryCacheDepth(n.Role)
}

// maybeReleaseMemoryAfterLedgerCachePrune implements the maybe release memory after ledger cache prune helper.
func maybeReleaseMemoryAfterLedgerCachePrune(removed int, height uint64) {
	if removed <= 0 {
		return
	}
	if removed >= 8 || height%32 == 0 {
		debug.FreeOSMemory()
	}
}

// hostMemoryTotalMiB implements the host memory total mi b helper.
func hostMemoryTotalMiB() int64 {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	// `line` tracks the current values while iterating.
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		// `fields` stores the value produced by this operation.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		// `kib` and `err` store the error produced by this operation.
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kib <= 0 {
			return 0
		}
		return kib / 1024
	}
	return 0
}

// configureRuntimeMemoryGuard implements the configure runtime memory guard helper.
func configureRuntimeMemoryGuard(role string) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MSC_DISABLE_RUNTIME_MEMORY_GUARD")), "1") {
		return
	}
	// `limitMiB` stores the value produced by this operation.
	limitMiB := int64(0)
	// `source` stores the value produced by this operation.
	source := "auto"
	// `raw` stores the value produced by this operation.
	if raw := strings.TrimSpace(os.Getenv("MSC_RUNTIME_MEMORY_LIMIT_MIB")); raw != "" {
		// `parsed` and `err` store the error produced by this operation.
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
	// `desired` stores the value produced by this operation.
	desired := limitMiB * runtimeGuardMiB
	// `current` stores the value produced by this operation.
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

// runtimePressureModeFor implements the runtime pressure mode for helper.
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

// runtimePressureModeCode implements the runtime pressure mode code helper.
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

// runGuarded implements the run guarded helper.
func runGuarded(name string, fn func()) (panicked bool) {
	defer func() {
		// `r` stores the value produced by this operation.
		if r := recover(); r != nil {
			panicked = true
			log.Printf("[RECOVERED] %s panic: %v\n%s", name, r, debug.Stack())
		}
	}()
	fn()
	return
}

// isShuttingDown implements the is shutting down helper.
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

// SetRootContext sets root context.
func (n *Node) SetRootContext(ctx context.Context, cancel context.CancelFunc) {
	if n == nil {
		return
	}
	n.rootCtx = ctx
	n.rootCancel = cancel
}

// RootContext implements the root context helper.
func (n *Node) RootContext() context.Context {
	if n == nil || n.rootCtx == nil {
		return context.Background()
	}
	return n.rootCtx
}

// CancelRootContext implements the cancel root context helper.
func (n *Node) CancelRootContext() {
	if n == nil || n.rootCancel == nil {
		return
	}
	n.rootCancel()
}

// SafeGo implements the safe go helper.
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
			// `panicked` stores the value produced by this operation.
			panicked := runGuarded(name, fn)
			if n.isShuttingDown() {
				return
			}
			if !panicked {
				log.Printf("[SUPERVISOR] %s exited; restarting in %s", name, restartDelay)
			} else {
				log.Printf("[SUPERVISOR] %s crashed; restarting in %s", name, restartDelay)
			}
			// `timer` stores the value produced by this operation.
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
