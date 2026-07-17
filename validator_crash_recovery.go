package main

// hasDueValidatorTransitionAtStartup implements the has due validator transition at startup helper.
func (n *Node) hasDueValidatorTransitionAtStartup(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}

	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()

	if n.queuedValidatorSetUpdates != nil {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := n.queuedValidatorSetUpdates[height]; ok {
			return true
		}
	}
	// `act` tracks the current values while iterating.
	for _, act := range n.pendingValidators {
		// `effective` stores the value produced by this operation.
		effective := validatorSetTransitionActivationHeightAt(act, height)
		if effective > 0 && effective <= height {
			return true
		}
	}
	// `act` tracks the current values while iterating.
	for _, act := range n.pendingValidatorRemovals {
		// `effective` stores the value produced by this operation.
		effective := validatorSetTransitionActivationHeightAt(act, height)
		if effective > 0 && effective <= height {
			return true
		}
	}
	return n.transitionPlan.Active && n.transitionPlan.UpdateHeight > 0 && n.transitionPlan.UpdateHeight <= height
}

// recoverDueValidatorTransitionsAtStartup implements the recover due validator transitions at startup helper.
func (n *Node) recoverDueValidatorTransitionsAtStartup(height uint64) bool {
	if n == nil || !n.hasDueValidatorTransitionAtStartup(height) {
		return false
	}
	n.applyScheduledValidatorUpdates(height)
	return true
}
