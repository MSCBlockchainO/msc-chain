package main

import (
	"strings"
	"testing"
)

func resetConsensusLayerInvariantGlobals(t *testing.T) {
	t.Helper()

	oldRegistry := GlobalValidatorRegistry.Snapshot()
	oldMinStake := ValidatorMinStake
	oldRequireStake := ValidatorRequireStake
	oldCoreStakeExempt := ValidatorCoreStakeExempt
	oldCandidateIsolationMode := CandidateIsolationMode
	oldCoreValidators := append([]string{}, ConfigAuthCoreValidators...)
	oldBanned := append([]string{}, ValidatorBannedList...)

	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
		ValidatorMinStake = oldMinStake
		ValidatorRequireStake = oldRequireStake
		ValidatorCoreStakeExempt = oldCoreStakeExempt
		CandidateIsolationMode = oldCandidateIsolationMode
		ConfigAuthCoreValidators = oldCoreValidators
		setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
		setValidatorBannedValidators(oldBanned)
	})

	ValidatorMinStake = 100
	ValidatorRequireStake = true
	ValidatorCoreStakeExempt = false
	CandidateIsolationMode = true
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D"}
	setRuntimeCoreValidatorIDs(ConfigAuthCoreValidators)
	setValidatorBannedValidators(nil)
}

func consensusLayerActiveValidator(id string, stake int64) ValidatorRecord {
	return ValidatorRecord{
		ID:            id,
		Stake:         stake,
		Reputation:    1,
		Status:        ValidatorActive,
		JoinHeight:    1,
		LastActive:    12,
		ActiveHeights: []uint64{10, 11, 12},
		SignedHeights: []uint64{10, 11, 12},
	}
}

func TestConsensusLayerPoSSlashingJailAndRotationInvariants(t *testing.T) {
	resetConsensusLayerInvariantGlobals(t)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A":   consensusLayerActiveValidator("A", 400),
		"B":   consensusLayerActiveValidator("B", 300),
		"C":   consensusLayerActiveValidator("C", 250),
		"D":   consensusLayerActiveValidator("D", 200),
		"E":   consensusLayerActiveValidator("E", 150),
		"LOW": consensusLayerActiveValidator("LOW", ValidatorMinStake-1),
	})

	selected := ValidatorSelector.Select(50, 5)
	if containsValidatorID(selected, "LOW") {
		t.Fatalf("proof-of-stake gate selected below-min-stake validator: %v", selected)
	}
	if !containsValidatorID(selected, "B") {
		t.Fatalf("expected healthy staked validator B before slash, selected=%v", selected)
	}

	ApplyValidatorPenalty("B", "double_sign", 60)
	rec, ok := GlobalValidatorRegistry.Get("B")
	if !ok || rec == nil {
		t.Fatal("expected validator B record after double-sign slash")
	}
	if rec.DoubleSign != 1 || rec.TotalSlashes != 1 {
		t.Fatalf("double-sign slash counters mismatch: double_sign=%d slashes=%d", rec.DoubleSign, rec.TotalSlashes)
	}
	if rec.Status != ValidatorJailed {
		t.Fatalf("double-sign slash must jail validator, got status=%s", rec.Status)
	}
	if rec.JailUntilHeight < 60+JailDoubleSignBlocks {
		t.Fatalf("double-sign jail too short: got=%d want_at_least=%d", rec.JailUntilHeight, 60+JailDoubleSignBlocks)
	}

	rotated := ValidatorSelector.Select(61, 5)
	if containsValidatorID(rotated, "B") {
		t.Fatalf("jailed validator remained in active rotation: %v", rotated)
	}
	if !containsValidatorID(rotated, "E") {
		t.Fatalf("rotation did not fill with next eligible validator after jail: %v", rotated)
	}

	jailUntil := rec.JailUntilHeight
	GlobalValidatorRegistry.mu.Lock()
	GlobalValidatorRegistry.records["B"].Reputation = ValidatorReputationRecoveryThreshold
	GlobalValidatorRegistry.mu.Unlock()
	UpdateValidatorStates(jailUntil)
	rec, _ = GlobalValidatorRegistry.Get("B")
	if rec.Status != ValidatorActive || rec.JailUntilHeight != 0 {
		t.Fatalf("validator should rejoin only after jail and reputation recovery, got status=%s jail_until=%d", rec.Status, rec.JailUntilHeight)
	}
}

func TestConsensusLayerFinalityRejectsDegradedQuorumCertificate(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	block.Signatures = canonicalValidatorIDs([]string{"A", "B"})
	if block.FinalityCertificate != nil {
		block.FinalityCertificate.Signers = block.Signatures
	}

	err := node.verifyFinalityCommitments(block, validators)
	if err == nil || !strings.Contains(err.Error(), "finality_quorum_shortfall") {
		t.Fatalf("strict finality must reject degraded quorum, got %v", err)
	}
}

func TestConsensusLayerEvidenceDoubleSignJailsAndRotatesValidator(t *testing.T) {
	resetConsensusLayerInvariantGlobals(t)
	resetSlashEvidenceGlobalsForTest(t)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": consensusLayerActiveValidator("A", 400),
		"B": consensusLayerActiveValidator("B", 300),
		"C": consensusLayerActiveValidator("C", 250),
		"D": consensusLayerActiveValidator("D", 200),
		"E": consensusLayerActiveValidator("E", 150),
	})
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D", "E"})

	node.RecordMisbehavior("b", "double_vote", 42, "vote-a")
	node.RecordMisbehavior("B", "double_sign", 42, "vote-a")

	entries := slashEvidenceEntriesForTest(t, node, "B")
	if len(entries) != 1 || entries[0].Reason != "double_sign" {
		t.Fatalf("double-sign evidence should canonicalize and dedupe, got %#v", entries)
	}
	rec, ok := GlobalValidatorRegistry.Get("B")
	if !ok || rec == nil {
		t.Fatal("expected validator B record after evidence slash")
	}
	if rec.DoubleSign != 1 || rec.TotalSlashes != 1 || rec.Status != ValidatorJailed {
		t.Fatalf("double-sign evidence did not apply jail/slash counters: %+v", *rec)
	}
	if rec.JailUntilHeight < 42+JailDoubleSignBlocks {
		t.Fatalf("double-sign evidence jail too short: got=%d want_at_least=%d", rec.JailUntilHeight, 42+JailDoubleSignBlocks)
	}
	if selected := ValidatorSelector.Select(43, 5); containsValidatorID(selected, "B") {
		t.Fatalf("double-sign jailed validator remained selectable: %v", selected)
	}
}

func TestConsensusLayerIrreversibleRootRejectsFinalizedReorg(t *testing.T) {
	validators := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	block := finalityLayerTestBlock(t, node, validators)
	if err := node.persistFinalityCheckpoint(block); err != nil {
		t.Fatalf("persist finality checkpoint: %v", err)
	}

	reorg := block
	reorg.StateRoot = "reorg-state-root"
	reorg.FinalizedStateRoot = reorg.StateRoot
	reorg.FinalityRoot = computeFinalityRoot(reorg, finalitySignersForBlock(reorg))
	reorg.EpochAnchorHash = computeEpochAnchorHash(reorg, reorg.PreviousEpochAnchorHash, finalitySignersForBlock(reorg))
	reorg.BlockHash = HashBlock(reorg)
	node.attachFinalityCertificate(&reorg)

	err := node.verifyFinalityCommitments(reorg, validators)
	if err == nil || !strings.Contains(err.Error(), "irreversible_finality_checkpoint_conflict") {
		t.Fatalf("finalized reorg below irreversible root must be rejected, got %v", err)
	}
}
