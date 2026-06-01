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

const runtimeGuardMiB int64 = 1024 * 1024

func runtimeAutoMemoryLimitMiB(totalMiB int64, role string) int64 {
	if totalMiB < 2048 {
		return 0
	}
	role = strings.ToLower(strings.TrimSpace(role))
	var limit int64
	if role == "full" || role == "light" {
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
