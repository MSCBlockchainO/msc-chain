package main

// prepareBlockExecutionAuthority is the consensus/runtime adapter. All Node
// reads stop here; the returned value can be replayed by the pure engine.
func (n *Node) prepareBlockExecutionAuthority(block Block) BlockExecutionAuthority {
	authority := BlockExecutionAuthority{}
	if n == nil || block.ID == 0 {
		return authority
	}
	authority.ValidatorRegistry = copyValidatorRegistrySnapshot(
		n.validatorRegistrySnapshotForHeight(block.ID),
	)
	if blockHasValidatorUpdateTx(block) {
		authority.ValidatorUpdates = cloneValidatorUpdateExecutionContext(
			n.newValidatorUpdateExecutionContext(block.ID),
		)
		if authority.ValidatorUpdates != nil && len(authority.ValidatorUpdates.registrySnapshot) > 0 {
			authority.ValidatorRegistry = copyValidatorRegistrySnapshot(
				authority.ValidatorUpdates.registrySnapshot,
			)
		}
	}
	authority.Registry = newExecutionRegistrySnapshot(block.ID, authority.ValidatorRegistry)
	return authority
}
