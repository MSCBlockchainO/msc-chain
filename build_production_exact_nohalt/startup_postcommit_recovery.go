package main

import (
	"fmt"
	"log"
	"strings"
)

// recoverCommittedExecutionSnapshotFromPostCommit rebuilds the execution-stage
// snapshot from the persisted post-commit ledger. The candidate is accepted
// only when reversing one deterministic reward pass matches the finalized
// StateRoot and replaying that pass recreates the persisted ledger exactly.
func (n *Node) recoverCommittedExecutionSnapshotFromPostCommit(height uint64) (string, bool) {
	if n == nil || height == 0 || n.Blockchain == nil || n.Blockchain.Height() != height {
		return "none", false
	}
	block, ok := n.committedBlockForLedgerHeight(height)
	if !ok || strings.TrimSpace(block.StateRoot) == "" {
		return "none", false
	}
	persisted, err := LoadNodeLedger(n.ID, n.DataDir)
	if err != nil || !ledgerHasInitializedBacking(persisted) {
		return "none", false
	}

	doubleApplied := n.startupExecutionReplayPostBlockEffectsToLedger(block, persisted)
	candidate, err := reversePostCommitBalanceDelta(persisted, doubleApplied)
	if err != nil {
		return "none", false
	}
	if matched, _, _ := n.executionSnapshotLedgerMatchesBlock(height, candidate); !matched {
		return "none", false
	}
	replayed := n.startupExecutionReplayPostBlockEffectsToLedger(block, candidate)
	if !strings.EqualFold(HashLedger(replayed), HashLedger(persisted)) {
		return "none", false
	}
	if err := n.createSnapshotWithLedger(height, strings.TrimSpace(block.BlockHash), candidate, snapshotLedgerStageExecution); err != nil {
		return "none", false
	}
	n.cacheExecutionSnapshotLedger(height, candidate)
	n.cachePostCommitLedger(height, persisted)
	n.markExecutionSnapshotReadyHeight(height)
	log.Printf("[STARTUP-EXECUTION-SNAPSHOT] action=recover source=persisted_post_commit_roundtrip status=ok height=%d ledger=%s",
		height,
		ShortHash(HashLedger(candidate)),
	)
	return "persisted_post_commit_roundtrip", true
}

func reversePostCommitBalanceDelta(persisted Ledger, doubleApplied Ledger) (Ledger, error) {
	candidate := persisted.Clone()
	if candidate.Balances == nil {
		candidate.Balances = make(map[string]int)
	}
	keys := make(map[string]struct{}, len(persisted.Balances)+len(doubleApplied.Balances))
	for key := range persisted.Balances {
		keys[key] = struct{}{}
	}
	for key := range doubleApplied.Balances {
		keys[key] = struct{}{}
	}
	for key := range keys {
		post := int64(persisted.Balances[key])
		doublePost := int64(doubleApplied.Balances[key])
		value := post - (doublePost - post)
		if value < 0 || int64(int(value)) != value {
			return Ledger{}, fmt.Errorf("post-commit balance inversion rejected key=%s", key)
		}
		candidate.Balances[key] = int(value)
	}
	return candidate, nil
}
