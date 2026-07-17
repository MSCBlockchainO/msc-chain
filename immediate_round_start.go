package main

// tryScheduleImmediateRoundStart implements the try schedule immediate round start helper.
func (n *Node) tryScheduleImmediateRoundStart(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	n.immediateRoundStartMu.Lock()
	defer n.immediateRoundStartMu.Unlock()
	if n.immediateRoundStartPendingHeight == height || n.immediateRoundStartStartedHeight == height {
		return false
	}
	n.immediateRoundStartPendingHeight = height
	return true
}

// finishImmediateRoundStart implements the finish immediate round start helper.
func (n *Node) finishImmediateRoundStart(height uint64, started bool) {
	if n == nil || height == 0 {
		return
	}
	n.immediateRoundStartMu.Lock()
	defer n.immediateRoundStartMu.Unlock()
	if n.immediateRoundStartPendingHeight == height {
		n.immediateRoundStartPendingHeight = 0
	}
	if started {
		n.immediateRoundStartStartedHeight = height
	}
}

// immediateRoundStartAlreadyHandled implements the immediate round start already handled helper.
func (n *Node) immediateRoundStartAlreadyHandled(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	n.immediateRoundStartMu.Lock()
	defer n.immediateRoundStartMu.Unlock()
	return n.immediateRoundStartStartedHeight == height
}

// clearImmediateRoundStart implements the clear immediate round start helper.
func (n *Node) clearImmediateRoundStart(height uint64) {
	if n == nil {
		return
	}
	n.immediateRoundStartMu.Lock()
	defer n.immediateRoundStartMu.Unlock()
	if height == 0 || n.immediateRoundStartPendingHeight == height {
		n.immediateRoundStartPendingHeight = 0
	}
	if height == 0 || n.immediateRoundStartStartedHeight == height {
		n.immediateRoundStartStartedHeight = 0
	}
}
