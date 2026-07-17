package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type SnapshotVerifier struct {
	// `Node` stores the value associated with this record.
	Node *Node
}

// VerifySnapshot verifies snapshot.
func VerifySnapshot(root []byte, calculated []byte) bool {
	return bytes.Equal(root, calculated)
}

// VerifySnapshotStateRoot verifies snapshot state root.
func VerifySnapshotStateRoot(expected string, calculated string) bool {
	return VerifySnapshot(
		[]byte(strings.ToLower(strings.TrimSpace(expected))),
		[]byte(strings.ToLower(strings.TrimSpace(calculated))),
	)
}

func snapshotExecutionAuthorityRejectReason(snapshot *StateSnapshot) string {
	if !snapshotHasTrustedExecutionLedger(snapshot) {
		return ""
	}
	return snapshotCurrentExecutionAuthorityRejectReason(snapshot)
}

// snapshotApplyExecutionAuthorityRejectReason is stricter than passive storage
// compatibility. A v4 snapshot is about to become live chain state, so even an
// early v4 record that omitted LedgerStage must prove that its ledger actually
// produces the committed StateRoot before any mutation starts.
func snapshotApplyExecutionAuthorityRejectReason(snapshot *StateSnapshot) string {
	if snapshot == nil {
		return "snapshot_metadata_invalid"
	}
	if snapshot.Version < snapshotAuthorityBindingVersion {
		return "snapshot_authority_binding_version_required"
	}
	stage := strings.TrimSpace(snapshot.LedgerStage)
	if stage != "" && !strings.EqualFold(stage, snapshotLedgerStageExecution) {
		return "snapshot_ledger_stage_invalid"
	}
	return snapshotCurrentExecutionAuthorityRejectReason(snapshot)
}

func snapshotCurrentExecutionAuthorityRejectReason(snapshot *StateSnapshot) string {
	if snapshot == nil {
		return "snapshot_metadata_invalid"
	}
	anchor := snapshotAnchorBlock(*snapshot)
	expectedRoot := ComputeExecHashVersioned(anchor, HashLedger(snapshot.Ledger), executionStateRootVersionForHeight(anchor.ID))
	if strings.TrimSpace(expectedRoot) == "" ||
		!strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(anchor.StateRoot)) ||
		!strings.EqualFold(strings.TrimSpace(snapshot.StateRoot), strings.TrimSpace(anchor.StateRoot)) {
		return "snapshot_execution_state_root_mismatch"
	}
	return ""
}

// Verify verifies its operation.
func (v SnapshotVerifier) Verify(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot unavailable")
	}
	// `reason` stores the value produced by this operation.
	if reason := snapshotMetadataRejectReason(snapshot); reason != "" {
		return errors.New(reason)
	}
	if reason := snapshotApplyExecutionAuthorityRejectReason(snapshot); reason != "" {
		return errors.New(reason)
	}
	if v.Node != nil {
		// `reason` stores the value produced by this operation.
		if reason := v.Node.snapshotLocalFinalityRejectReason(snapshot); reason != "" {
			return errors.New(reason)
		}
	}
	// `expectedMerkle` stores the value produced by this operation.
	expectedMerkle := LedgerStateMerkleRoot(snapshot.Ledger)
	if expectedMerkle == "" {
		return fmt.Errorf("state_merkle_root_mismatch")
	}
	if !VerifySnapshotStateRoot(strings.TrimSpace(snapshot.StateMerkleRoot), expectedMerkle) {
		return fmt.Errorf("state_merkle_root_mismatch")
	}
	if v.Node != nil {
		// `ok` and `reason` store whether the related condition is satisfied.
		if ok, reason := v.Node.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok && strings.TrimSpace(reason) != "anchor_block_unavailable" {
			return errors.New(strings.TrimSpace(reason))
		}
	}
	return nil
}

// snapshotFinalityMetadataRejectReason implements the snapshot finality metadata reject reason helper.
func snapshotFinalityMetadataRejectReason(snapshot *StateSnapshot) string {
	if snapshot == nil {
		return "snapshot_metadata_invalid"
	}
	// `cert` stores the value produced by this operation.
	cert := snapshot.FinalityCertificate
	// `finalizedHash` stores the digest used to identify or verify the related data.
	finalizedHash := strings.TrimSpace(snapshot.FinalizedHash)
	// `finalizedStateRoot` stores the digest used to identify or verify the related data.
	finalizedStateRoot := strings.TrimSpace(snapshot.FinalizedStateRoot)
	// `finalizedSetHash` stores the digest used to identify or verify the related data.
	finalizedSetHash := strings.TrimSpace(snapshot.FinalizedValidatorSetHash)
	// `finalizedSetRoot` stores the digest used to identify or verify the related data.
	finalizedSetRoot := strings.TrimSpace(snapshot.FinalizedValidatorSetRoot)
	// `epochAnchor` stores the value produced by this operation.
	epochAnchor := strings.TrimSpace(snapshot.EpochAnchorHash)
	// `previousAnchor` stores the value produced by this operation.
	previousAnchor := strings.TrimSpace(snapshot.PreviousEpochAnchorHash)
	// `finalityRoot` stores the digest used to identify or verify the related data.
	finalityRoot := strings.TrimSpace(snapshot.FinalityRoot)
	// `hasFinality` stores the value produced by this operation.
	hasFinality := snapshot.FinalizedEpoch > 0 ||
		snapshot.FinalizedHeight > 0 ||
		finalizedHash != "" ||
		finalizedStateRoot != "" ||
		finalizedSetHash != "" ||
		finalizedSetRoot != "" ||
		epochAnchor != "" ||
		previousAnchor != "" ||
		finalityRoot != "" ||
		cert != nil
	if !hasFinality {
		return ""
	}
	if snapshot.FinalizedHeight == 0 {
		return "snapshot_finality_incomplete"
	}
	if snapshot.FinalizedHeight > snapshot.Height {
		return "snapshot_finality_height_ahead"
	}
	if snapshot.FinalizedEpoch > 0 && snapshot.FinalizedEpoch != snapshot.FinalizedHeight {
		return "snapshot_finality_height_mismatch"
	}
	if (epochAnchor == "") != (finalityRoot == "") {
		return "snapshot_finality_incomplete"
	}
	if finalizedHash != "" && snapshot.FinalizedHeight == snapshot.Height &&
		!strings.EqualFold(finalizedHash, strings.TrimSpace(snapshot.BlockHash)) {
		return "snapshot_finalized_hash_mismatch"
	}
	if finalizedStateRoot != "" && snapshot.FinalizedHeight == snapshot.Height &&
		!strings.EqualFold(finalizedStateRoot, strings.TrimSpace(snapshot.StateRoot)) {
		return "snapshot_finalized_state_root_mismatch"
	}
	if finalizedSetHash != "" && snapshot.FinalizedHeight == snapshot.Height {
		// `currentSetHash` stores the digest used to identify or verify the related data.
		currentSetHash := strings.TrimSpace(snapshotValidatorSetHash(snapshot))
		if currentSetHash != "" && !strings.EqualFold(finalizedSetHash, currentSetHash) {
			return "snapshot_finalized_validator_set_mismatch"
		}
	}
	if cert == nil {
		return ""
	}
	if cert.Height != snapshot.FinalizedHeight {
		return "snapshot_finality_certificate_mismatch"
	}
	if snapshot.FinalizedEpoch > 0 && cert.Epoch != snapshot.FinalizedEpoch {
		return "snapshot_finality_certificate_mismatch"
	}
	if finalizedHash != "" && !strings.EqualFold(strings.TrimSpace(cert.BlockHash), finalizedHash) {
		return "snapshot_finality_certificate_mismatch"
	}
	if finalizedStateRoot != "" && !strings.EqualFold(strings.TrimSpace(cert.StateRoot), finalizedStateRoot) {
		return "snapshot_finality_certificate_mismatch"
	}
	if finalizedSetHash != "" && !strings.EqualFold(strings.TrimSpace(cert.FinalizedValidatorSetHash), finalizedSetHash) {
		return "snapshot_finality_certificate_mismatch"
	}
	if finalizedSetRoot != "" && !strings.EqualFold(strings.TrimSpace(cert.FinalizedValidatorSetRoot), finalizedSetRoot) {
		return "snapshot_finality_certificate_mismatch"
	}
	if epochAnchor != "" && !strings.EqualFold(strings.TrimSpace(cert.EpochAnchorHash), epochAnchor) {
		return "snapshot_finality_certificate_mismatch"
	}
	if previousAnchor != "" && !strings.EqualFold(strings.TrimSpace(cert.PreviousEpochAnchorHash), previousAnchor) {
		return "snapshot_finality_certificate_mismatch"
	}
	if finalityRoot != "" && !strings.EqualFold(strings.TrimSpace(cert.FinalityRoot), finalityRoot) {
		return "snapshot_finality_certificate_mismatch"
	}
	// `required` stores the request data being processed.
	if required := cert.StrictQuorum; required > 0 && len(canonicalValidatorIDs(cert.Signers)) > 0 && len(canonicalValidatorIDs(cert.Signers)) < required {
		return "snapshot_finality_certificate_quorum_missing"
	}
	return ""
}

// snapshotLocalFinalityRejectReason implements the snapshot local finality reject reason helper.
func (n *Node) snapshotLocalFinalityRejectReason(snapshot *StateSnapshot) string {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return ""
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := uint64(0)
	// `localFinalized` stores the value produced by this operation.
	localFinalized := uint64(0)
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
		// `chainFinalized` stores the value produced by this operation.
		if chainFinalized := n.Blockchain.FinalizedHeight(); chainFinalized > localFinalized {
			localFinalized = chainFinalized
		}
	}
	n.commitMu.Lock()
	// `committed` stores the value produced by this operation.
	committed := n.committedHeight
	// `nodeFinalized` stores the value produced by this operation.
	nodeFinalized := n.finalizedHeight
	n.commitMu.Unlock()
	// Peer heartbeat majority can advance n.finalizedHeight ahead of the local
	// block anchor while a full node is catching up. Snapshot regression checks
	// must only use locally anchored finality, otherwise a valid catch-up
	// snapshot below the remote-observed network head is rejected before apply.
	if committed > localFinalized {
		localFinalized = committed
	}
	if localHeight > 0 && nodeFinalized <= localHeight && nodeFinalized > localFinalized {
		localFinalized = nodeFinalized
	}
	if localFinalized > 0 && snapshot.Height < localFinalized {
		return "snapshot_below_finalized_height"
	}
	if localFinalized > 0 && snapshot.FinalizedHeight > 0 && snapshot.FinalizedHeight < localFinalized {
		return "snapshot_stale_finality_anchor"
	}
	if snapshot.Height <= localFinalized {
		// `expected`, `found`, and `err` store the error produced by this operation.
		if expected, found, err := n.loadFinalizedHashInvariant(snapshot.Height); err == nil && found &&
			expected != "" && !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(snapshot.BlockHash)) {
			return "snapshot_irreversible_hash_conflict"
		}
	}
	if snapshot.FinalizedHeight > 0 && strings.TrimSpace(snapshot.FinalizedHash) != "" {
		// `expected`, `found`, and `err` store the error produced by this operation.
		if expected, found, err := n.loadFinalizedHashInvariant(snapshot.FinalizedHeight); err == nil && found &&
			expected != "" && !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(snapshot.FinalizedHash)) {
			return "snapshot_finalized_hash_conflict"
		}
	}
	return ""
}
