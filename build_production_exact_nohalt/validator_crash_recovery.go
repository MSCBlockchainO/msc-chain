package main

func (n *Node) hasDueValidatorTransitionAtStartup(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()

	if n.queuedValidatorSetUpdates != nil {
		if _, ok := n.queuedValidatorSetUpdates[height]; ok {
			return true
		}
	}
	for _, act := range n.pendingValidators {
		effective := validatorSetTransitionActivationHeightAt(act, height)
		if effective > 0 && effective <= height {
			return true
		}
	}
	for _, act := range n.pendingValidatorRemovals {
		effective := validatorSetTransitionActivationHeightAt(act, height)
		if effective > 0 && effective <= height {
			return true
		}
	}
	return n.transitionPlan.Active && n.transitionPlan.UpdateHeight > 0 && n.transitionPlan.UpdateHeight <= height
}

func (n *Node) recoverDueValidatorTransitionsAtStartup(height uint64) bool {
	if n == nil || !n.hasDueValidatorTransitionAtStartup(height) {
		return false
	}
	n.applyScheduledValidatorUpdates(height)
	return true
}
