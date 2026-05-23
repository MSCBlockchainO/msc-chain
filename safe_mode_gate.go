package main

import "sync/atomic"

func (n *Node) armPostBlockSafeModeGate(height uint64) bool {
	if n == nil || height == 0 || !ConsensusPostBlockSafeModeEnabled {
		return false
	}
	atomic.StoreUint64(&n.safeModeGateHeight, height)
	atomic.StoreInt32(&n.safeModeGateActive, 1)
	return true
}

func (n *Node) postBlockSafeModeGateActiveForHeight(height uint64) bool {
	if n == nil || height == 0 || !ConsensusPostBlockSafeModeEnabled {
		return false
	}
	if atomic.LoadInt32(&n.safeModeGateActive) == 0 {
		return false
	}
	return atomic.LoadUint64(&n.safeModeGateHeight) == height
}

func (n *Node) clearPostBlockSafeModeGate(height uint64) {
	if n == nil {
		return
	}
	if height == 0 {
		atomic.StoreInt32(&n.safeModeGateActive, 0)
		atomic.StoreUint64(&n.safeModeGateHeight, 0)
		return
	}
	if atomic.LoadUint64(&n.safeModeGateHeight) != height {
		return
	}
	atomic.StoreInt32(&n.safeModeGateActive, 0)
	atomic.StoreUint64(&n.safeModeGateHeight, 0)
}
