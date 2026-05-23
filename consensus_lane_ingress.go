package main

// Consensus-affecting ingress must hop onto the consensus lane before it
// mutates proposal, execution-vote, or finalization state.

func copyConsensusLaneBlocks(blocks []Block) []Block {
	if len(blocks) == 0 {
		return nil
	}
	copied := make([]Block, len(blocks))
	copy(copied, blocks)
	return copied
}

func (n *Node) submitExecutionResultOnConsensusLane(res ExecutionResultMsg, allowQueue bool) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		n.processExecutionResultMsg(res, allowQueue)
	})
}

func (n *Node) submitCommitMsgOnConsensusLane(cm CommitMsg) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		n.handleCommitMsg(cm)
	})
}

func (n *Node) submitLeaderBlockOnConsensusLane(block Block, sourcePeer string) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		n.handleLeaderBlock(block, sourcePeer)
	})
}

func (n *Node) submitFinalBlockOnConsensusLane(block Block) bool {
	if n == nil {
		return false
	}
	return n.scheduleConsensusTask(func() {
		_ = n.ReceiveBlock(block, n.Blockchain)
		n.ProcessQueuedBlocks()
	})
}

func (n *Node) submitBlockBatchOnConsensusLane(blocks []Block) bool {
	if n == nil || len(blocks) == 0 {
		return false
	}
	copied := copyConsensusLaneBlocks(blocks)
	return n.scheduleConsensusTask(func() {
		for _, block := range copied {
			_ = n.ReceiveBlock(block, n.Blockchain)
		}
		n.ProcessQueuedBlocks()
	})
}
