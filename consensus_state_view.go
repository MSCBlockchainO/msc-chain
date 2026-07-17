package main

import "strings"

// ensureExecVotesLocked implements the ensure exec votes locked helper.
func (cs *ConsensusState) ensureExecVotesLocked() {
	if cs == nil {
		return
	}
	if cs.ExecVotes == nil {
		cs.ExecVotes = make(map[string]map[string]ExecutionResult)
	}
}

// clearActiveExecutionViewLocked implements the clear active execution view locked helper.
func (cs *ConsensusState) clearActiveExecutionViewLocked() {
	if cs == nil {
		return
	}
	cs.ensureExecVotesLocked()
	// `blockHash` tracks the block data handled by this operation.
	for blockHash := range cs.ExecVotes {
		delete(cs.ExecVotes, blockHash)
	}
	cs.LockedBlock = ""
	cs.LockedBlockHash = ""
	cs.LockedRound = 0
	cs.Committed = false
}

// RecordExecVote implements the record exec vote helper.
func (cs *ConsensusState) RecordExecVote(height uint64, blockHash string, vote ExecutionResult) bool {
	if cs == nil || height == 0 {
		return false
	}
	blockHash = strings.TrimSpace(blockHash)
	// `signer` stores the value produced by this operation.
	signer := normalizeValidatorID(vote.Signer)
	if blockHash == "" || signer == "" {
		return false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.Height != height {
		return false
	}
	cs.ensureExecVotesLocked()
	// `ok` stores whether the related condition is satisfied.
	if _, ok := cs.ExecVotes[blockHash]; !ok {
		cs.ExecVotes[blockHash] = make(map[string]ExecutionResult)
	}
	vote.Height = height
	vote.BlockHash = blockHash
	vote.Signer = signer
	cs.ExecVotes[blockHash][signer] = vote
	cs.Committed = false
	return true
}

// SetLockedBlock sets locked block.
func (cs *ConsensusState) SetLockedBlock(height uint64, blockHash string, round uint32) bool {
	if cs == nil || height == 0 {
		return false
	}
	blockHash = strings.TrimSpace(blockHash)
	if blockHash == "" {
		return false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.Height != height {
		return false
	}
	cs.LockedBlock = blockHash
	cs.LockedBlockHash = blockHash
	cs.LockedRound = round
	cs.Committed = false
	return true
}

// ClearLockedBlock clears locked block.
func (cs *ConsensusState) ClearLockedBlock(height uint64) bool {
	if cs == nil || height == 0 {
		return false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.Height != height {
		return false
	}
	cs.LockedBlock = ""
	cs.LockedBlockHash = ""
	cs.LockedRound = 0
	return true
}

// MarkCommitted marks committed.
func (cs *ConsensusState) MarkCommitted(height uint64) bool {
	if cs == nil || height == 0 {
		return false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.Height != height {
		return false
	}
	cs.Committed = true
	return true
}

// mirrorConsensusExecVote implements the mirror consensus exec vote helper.
func (n *Node) mirrorConsensusExecVote(height uint64, blockHash string, vote ExecutionResult) bool {
	if n == nil || n.Consensus == nil {
		return false
	}
	return n.Consensus.RecordExecVote(height, blockHash, vote)
}

// syncConsensusLockedBlock implements the sync consensus locked block helper.
func (n *Node) syncConsensusLockedBlock(block Block) bool {
	if n == nil || n.Consensus == nil || block.ID == 0 {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	ok := n.Consensus.SetLockedBlock(block.ID, block.BlockHash, block.Round)
	if ok {
		n.persistConsensusSafetyStateAsync("locked_block")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "locked_block",
			Reason:    "execution_precommit",
			Height:    block.ID,
			Round:     block.Round,
			BlockHash: block.BlockHash,
		})
	}
	return ok
}

// clearConsensusLockedBlock implements the clear consensus locked block helper.
func (n *Node) clearConsensusLockedBlock(height uint64) bool {
	if n == nil || n.Consensus == nil {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	ok := n.Consensus.ClearLockedBlock(height)
	if ok {
		n.persistConsensusSafetyStateAsync("clear_locked_block")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:   "locked_block_cleared",
			Reason: "height_cleared",
			Height: height,
		})
	}
	return ok
}

// markConsensusCommittedHeight implements the mark consensus committed height helper.
func (n *Node) markConsensusCommittedHeight(height uint64) bool {
	if n == nil || n.Consensus == nil {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	ok := n.Consensus.MarkCommitted(height)
	if ok {
		n.persistConsensusSafetyStateAsync("consensus_committed")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:   "consensus_height_committed",
			Reason: "finality_barrier",
			Height: height,
		})
	}
	return ok
}
