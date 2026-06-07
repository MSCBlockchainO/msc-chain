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
	if err := n.validatePersistableValidatorRegistrySource(height, expectedHash, registry); err != nil {
		return err
	}
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
	runtime := copyValidatorRegistrySnapshot(GlobalValidatorRegistry.Snapshot())
	expectedHash := strings.TrimSpace(block.ValidatorRegistryHash)
	if expectedHash == "" {
		if committedSnapshot, _, committedSource := n.deterministicValidatorRegistryCommitmentForHeight(block.ID); len(committedSnapshot) > 0 {
			return committedSnapshot, committedSource, nil
		}
		if n.validatorRegistryCommitmentRequiredAt(block.ID) {
			return nil, "", errors.New("missing_validator_registry_hash")
		}
		return runtime, "runtime", nil
	}
	runtimeHash := deterministicRegistryHash(runtime)
	if got := deterministicRegistryHash(runtime); got != "" && strings.EqualFold(got, expectedHash) {
		return runtime, "runtime", nil
	}
	if carryForward, _, ok := n.findCommittedValidatorRegistrySnapshotByHashAtOrBelow(block.ID, expectedHash); ok && len(carryForward) > 0 {
		return carryForward, "committed_carry_forward", nil
	}
	if candidate, hash, ok := n.genesisCommittedValidatorRegistryCandidate(nil); ok && strings.EqualFold(strings.TrimSpace(hash), expectedHash) {
		return candidate, "genesis_bootstrap_projection", nil
	}
	if repaired, repairedHash, ok := n.repairGenesisCommittedValidatorRegistryByHash(expectedHash, runtime); ok && len(repaired) > 0 {
		if DebugConsensus {
			fmt.Printf("[REGISTRY-PRECOMMIT-REPAIR] height=%d source=genesis_bootstrap_repair expected=%s repaired=%s runtime=%s validators=%d\n",
				block.ID,
				ShortHash(expectedHash),
				ShortHash(repairedHash),
				ShortHash(runtimeHash),
				len(repaired),
			)
		}
		return repaired, "genesis_bootstrap_repair", nil
	}
	candidateHeights := []uint64{}
	if block.ID > 0 {
		candidateHeights = append(candidateHeights, block.ID-1, block.ID)
	}
	seen := make(map[uint64]struct{}, len(candidateHeights))
	for _, height := range candidateHeights {
		if _, ok := seen[height]; ok {
			continue
		}
		seen[height] = struct{}{}
		bootstrap := n.startupExecutionBootstrapRegistrySnapshot(height)
		if len(bootstrap) == 0 {
			continue
		}
		if got := deterministicRegistryHash(bootstrap); got != "" && strings.EqualFold(got, expectedHash) {
			return bootstrap, "startup_bootstrap_projection", nil
		}
	}
	if err := n.validatePersistableValidatorRegistrySource(block.ID, expectedHash, runtime); err == nil {
		return runtime, "runtime_repairable", nil
	}
	if DebugConsensus {
		fmt.Printf("[REGISTRY-PRECOMMIT-FAIL] height=%d expected=%s runtime=%s validators=%d\n",
			block.ID,
			ShortHash(expectedHash),
			ShortHash(runtimeHash),
			len(runtime),
		)
	}
	return nil, "", errors.New("validator_registry_hash_mismatch")
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
