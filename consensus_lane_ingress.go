package main

// Consensus-affecting ingress must hop onto the consensus lane before it
// mutates proposal, execution-vote, or finalization state.

// copyConsensusLaneBlocks copies consensus lane blocks.
func copyConsensusLaneBlocks(blocks []Block) []Block {
	if len(blocks) == 0 {
		return nil
	}
	// `copied` stores the value produced by this operation.
	copied := make([]Block, len(blocks))
	copy(copied, blocks)
	return copied
}

// submitExecutionResultOnConsensusLane implements the submit execution result on consensus lane helper.
func (n *Node) submitExecutionResultOnConsensusLane(res ExecutionResultMsg, allowQueue bool) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		n.processExecutionResultMsg(res, allowQueue)
	})
}

// submitCommitMsgOnConsensusLane implements the submit commit msg on consensus lane helper.
func (n *Node) submitCommitMsgOnConsensusLane(cm CommitMsg) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		n.handleCommitMsg(cm)
	})
}

// submitLeaderBlockOnConsensusLane implements the submit leader block on consensus lane helper.
func (n *Node) submitLeaderBlockOnConsensusLane(block Block, sourcePeer string) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		n.handleLeaderBlock(block, sourcePeer)
	})
}

// submitFinalBlockOnConsensusLane implements the submit final block on consensus lane helper.
func (n *Node) submitFinalBlockOnConsensusLane(block Block) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		_ = n.ReceiveBlock(block, n.Blockchain)
		n.ProcessQueuedBlocks()
	})
}

// submitBlockBatchOnConsensusLane implements the submit block batch on consensus lane helper.
func (n *Node) submitBlockBatchOnConsensusLane(blocks []Block) bool {
	if n == nil || len(blocks) == 0 {
		return false
	}
	// `copied` stores the value produced by this operation.
	copied := copyConsensusLaneBlocks(blocks)
	return n.scheduleConsensusTask(func() {
		// `block` tracks the synchronization state protecting shared data.
		for _, block := range copied {
			_ = n.ReceiveBlock(block, n.Blockchain)
		}
		n.ProcessQueuedBlocks()
	})
}
