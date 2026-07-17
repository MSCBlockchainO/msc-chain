package main

import (
	"errors"
	"fmt"
	"strings"
)

// deterministicRegistryHash canonicalizes registry hashing through the
// committed-snapshot representation used by consensus.
func deterministicRegistryHash(registry map[string]ValidatorRecord) string {
	return strings.TrimSpace(ValidatorRegistrySnapshotHash(copyValidatorRegistrySnapshot(registry)))
}

// deterministicValidatorRoot computes the canonical validator root for a height
// using sorted validator IDs and the committed registry snapshot.
func deterministicValidatorRoot(height uint64, validators []string, registry map[string]ValidatorRecord) string {
	return strings.TrimSpace(ValidatorSetMerkleRoot(height, canonicalValidatorIDs(validators), copyValidatorRegistrySnapshot(registry)))
}

// deterministicCommittedRegistryForHeight returns the chain-anchored committed
// registry snapshot and enforces an optional expected hash when provided.
func (n *Node) deterministicCommittedRegistryForHeight(height uint64, expectedHash string) (map[string]ValidatorRecord, string, error) {
	if n == nil || height == 0 {
		return nil, "", errors.New("registry_height_invalid")
	}
	// `registry`, `hash`, and `ok` store whether the related condition is satisfied.
	registry, hash, _, ok := n.resolveCommittedValidatorRegistrySnapshot(height)
	if !ok || len(registry) == 0 {
		return nil, "", errors.New("registry_snapshot_unavailable")
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		hash = deterministicRegistryHash(registry)
	}
	expectedHash = strings.TrimSpace(expectedHash)
	if expectedHash != "" && !strings.EqualFold(hash, expectedHash) {
		return nil, "", errors.New("validator_registry_hash_mismatch")
	}
	return copyValidatorRegistrySnapshot(registry), hash, nil
}

// deterministicPersistRegistrySnapshot is the strict persistence entrypoint for
// committed registry state. No mismatch is allowed to enter the DB.
func (n *Node) deterministicPersistRegistrySnapshot(height uint64, registry map[string]ValidatorRecord, expectedHash string) error {
	// `err` stores the error produced by this operation.
	if err := n.validatePersistableValidatorRegistrySource(height, expectedHash, registry); err != nil {
		return err
	}
	// `skip` and `sourceHeight` store the value produced by this operation.
	if skip, sourceHeight := n.shouldSkipCatchupRegistrySnapshotWrite(height, expectedHash); skip {
		if DebugConsensus {
			fmt.Printf("[REGISTRY-PERSIST-SKIP] height=%d source=catchup_carry_forward from=%d hash=%s\n",
				height,
				sourceHeight,
				ShortHash(expectedHash),
			)
		}
		return nil
	}
	return n.persistValidatorRegistrySnapshotFromSource(height, registry)
}

// deterministicPreCommitRegistrySnapshot resolves the exact registry snapshot
// that should be treated as authoritative before a block mutates runtime
// validator state. This avoids rejecting fresh-sync bootstrap blocks when the
// local runtime registry has not yet converged but a deterministic committed
// projection already exists for the incoming registry hash.
func (n *Node) deterministicPreCommitRegistrySnapshot(block Block) (map[string]ValidatorRecord, string, error) {
	// `expectedHash` stores the digest used to identify or verify the related data.
	expectedHash := strings.TrimSpace(block.ValidatorRegistryHash)
	if expectedHash == "" {
		// `committedSnapshot` and `committedSource` store the value produced by this operation.
		if committedSnapshot, committedSource := n.deterministicNoHeaderPreCommitRegistrySnapshot(block); len(committedSnapshot) > 0 {
			return committedSnapshot, committedSource, nil
		}
		if !validatorSetCommitmentV2EnabledAt(block.ID) {
			// Legacy pre-V2 blocks did not carry a registry hash. Keep this
			// compatibility fallback strictly before the registry-commitment fork;
			// post-fork consensus remains committed-snapshot only.
			if legacySnapshot := n.validatorRegistrySnapshotForHeight(block.ID); len(legacySnapshot) > 0 {
				return copyValidatorRegistrySnapshot(legacySnapshot), "legacy_pre_v2_registry_snapshot", nil
			}
		}
		return nil, "", errors.New("missing_validator_registry_hash")
	}
	// `projected`, `projectedHash`, and `ok` store whether the related condition is satisfied.
	if projected, projectedHash, ok := n.projectedValidatorUpdateRegistrySnapshotForBlock(block); ok &&
		strings.TrimSpace(projectedHash) != "" &&
		strings.EqualFold(strings.TrimSpace(projectedHash), expectedHash) {
		return projected, "block_tx_registry_projection", nil
	}
	if block.ID > 1 {
		// `repaired`, `repairedHash`, and `ok` store whether the related condition is satisfied.
		if repaired, repairedHash, ok := n.repairCommittedValidatorRegistrySnapshotFromCommittedSnapshot(block.ID-1, expectedHash); ok &&
			len(repaired) > 0 &&
			strings.EqualFold(strings.TrimSpace(repairedHash), expectedHash) {
			return repaired, "committed_parent_state_snapshot_repair", nil
		}
		// `repaired`, `repairedHash`, and `ok` store whether the related condition is satisfied.
		if repaired, repairedHash, _, ok := n.repairValidatorRegistrySnapshotFromCommittedStateSnapshotAtOrBelow(block.ID-1, expectedHash); ok &&
			len(repaired) > 0 &&
			strings.EqualFold(strings.TrimSpace(repairedHash), expectedHash) {
			return repaired, "committed_parent_state_snapshot_carry_forward", nil
		}
	}
	// `repaired`, `repairedHash`, and `ok` store whether the related condition is satisfied.
	if repaired, repairedHash, ok := n.repairCommittedValidatorRegistrySnapshotFromCommittedSnapshot(block.ID, expectedHash); ok &&
		len(repaired) > 0 &&
		strings.EqualFold(strings.TrimSpace(repairedHash), expectedHash) {
		return repaired, "committed_state_snapshot_repair", nil
	}
	// `repaired`, `repairedHash`, and `ok` store whether the related condition is satisfied.
	if repaired, repairedHash, _, ok := n.repairValidatorRegistrySnapshotFromCommittedStateSnapshotAtOrBelow(block.ID, expectedHash); ok &&
		len(repaired) > 0 &&
		strings.EqualFold(strings.TrimSpace(repairedHash), expectedHash) {
		return repaired, "committed_state_snapshot_carry_forward", nil
	}
	// `carryForward` and `ok` store whether the related condition is satisfied.
	if carryForward, _, ok := n.findCommittedValidatorRegistrySnapshotByHashAtOrBelow(block.ID, expectedHash); ok && len(carryForward) > 0 {
		return carryForward, "committed_carry_forward", nil
	}
	// `candidate`, `hash`, and `ok` store whether the related condition is satisfied.
	if candidate, hash, ok := n.genesisCommittedValidatorRegistryCandidate(nil); ok && strings.EqualFold(strings.TrimSpace(hash), expectedHash) {
		return candidate, "genesis_bootstrap_projection", nil
	}
	// `candidateHeights` stores the value produced by this operation.
	candidateHeights := []uint64{}
	if block.ID > 0 {
		candidateHeights = append(candidateHeights, block.ID-1, block.ID)
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[uint64]struct{}, len(candidateHeights))
	// `height` tracks the current values while iterating.
	for _, height := range candidateHeights {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[height]; ok {
			continue
		}
		seen[height] = struct{}{}
		// `bootstrap` stores the value produced by this operation.
		bootstrap := n.startupExecutionBootstrapRegistrySnapshot(height)
		if len(bootstrap) == 0 {
			continue
		}
		// `got` stores the value produced by this operation.
		if got := deterministicRegistryHash(bootstrap); got != "" && strings.EqualFold(got, expectedHash) {
			return bootstrap, "startup_bootstrap_projection", nil
		}
	}
	if DebugConsensus {
		fmt.Printf("[REGISTRY-PRECOMMIT-FAIL] height=%d expected=%s reason=committed_source_unavailable\n",
			block.ID,
			ShortHash(expectedHash),
		)
	}
	return nil, "", errors.New("validator_registry_hash_mismatch")
}

// deterministicNoHeaderPreCommitRegistrySnapshot resolves pre-commit registry
// authority for legacy blocks that do not carry ValidatorRegistryHash. Only
// already committed/snapshot-backed sources are accepted; mutable runtime
// registry state is intentionally not a fallback.
func (n *Node) deterministicNoHeaderPreCommitRegistrySnapshot(block Block) (map[string]ValidatorRecord, string) {
	if n == nil || block.ID == 0 {
		return nil, ""
	}
	if block.ID > 1 {
		// `parent` and `ok` store whether the related condition is satisfied.
		if parent, _, _, ok := n.resolveCommittedValidatorRegistrySnapshot(block.ID - 1); ok && len(parent) > 0 {
			return copyValidatorRegistrySnapshot(parent), "committed_parent_snapshot"
		}
	}
	// Sparse anchors or locally materialized current-height snapshots can be
	// authoritative even when the historical parent is pruned.
	// `current` and `ok` store whether the related condition is satisfied.
	if current, _, _, ok := n.resolveCommittedValidatorRegistrySnapshot(block.ID); ok && len(current) > 0 {
		return copyValidatorRegistrySnapshot(current), "committed_current_snapshot"
	}
	return nil, ""
}

// deterministicEnsureExecutionLedgerAligned fail-closes when runtime and
// authoritative execution ledgers cannot be aligned before deterministic apply.
func (n *Node) deterministicEnsureExecutionLedgerAligned(height uint64, ctx *executionRootContext) error {
	if ctx == nil {
		return errors.New("execution_context_unavailable")
	}
	if !n.enforceRuntimeLedgerMatchesExecution(height, ctx) {
		return errors.New("execution_state_mismatch")
	}
	return nil
}
