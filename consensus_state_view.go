package main

import "strings"

func (cs *ConsensusState) ensureExecVotesLocked() {
	if cs == nil {
		return
	}
	if cs.ExecVotes == nil {
		cs.ExecVotes = make(map[string]map[string]ExecutionResult)
	}
}

func (cs *ConsensusState) clearActiveExecutionViewLocked() {
	if cs == nil {
		return
	}
	cs.ensureExecVotesLocked()
	for blockHash := range cs.ExecVotes {
		delete(cs.ExecVotes, blockHash)
	}
	cs.LockedBlock = ""
	cs.LockedBlockHash = ""
	cs.LockedRound = 0
	cs.Committed = false
}

func (cs *ConsensusState) RecordExecVote(height uint64, blockHash string, vote ExecutionResult) bool {
	if cs == nil || height == 0 {
		return false
	}
	blockHash = strings.TrimSpace(blockHash)
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

func (n *Node) mirrorConsensusExecVote(height uint64, blockHash string, vote ExecutionResult) bool {
	if n == nil || n.Consensus == nil {
		return false
	}
	return n.Consensus.RecordExecVote(height, blockHash, vote)
}

func (n *Node) syncConsensusLockedBlock(block Block) bool {
	if n == nil || n.Consensus == nil || block.ID == 0 {
		return false
	}
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

func (n *Node) clearConsensusLockedBlock(height uint64) bool {
	if n == nil || n.Consensus == nil {
		return false
	}
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

func (n *Node) markConsensusCommittedHeight(height uint64) bool {
	if n == nil || n.Consensus == nil {
		return false
	}
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
