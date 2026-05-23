package main

import (
	"context"
	"log"
	"runtime/debug"
	"time"
)

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
