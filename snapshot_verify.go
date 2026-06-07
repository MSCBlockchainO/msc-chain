package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type SnapshotVerifier struct {
	Node *Node
}

func VerifySnapshot(root []byte, calculated []byte) bool {
	return bytes.Equal(root, calculated)
}

func VerifySnapshotStateRoot(expected string, calculated string) bool {
	return VerifySnapshot(
		[]byte(strings.ToLower(strings.TrimSpace(expected))),
		[]byte(strings.ToLower(strings.TrimSpace(calculated))),
	)
}

func (v SnapshotVerifier) Verify(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot unavailable")
	}
	if reason := snapshotMetadataRejectReason(snapshot); reason != "" {
		return errors.New(reason)
	}
	if v.Node != nil {
		if reason := v.Node.snapshotLocalFinalityRejectReason(snapshot); reason != "" {
			return errors.New(reason)
		}
	}
	expectedMerkle := LedgerStateMerkleRoot(snapshot.Ledger)
	if expectedMerkle == "" {
		return fmt.Errorf("state_merkle_root_mismatch")
	}
	if !VerifySnapshotStateRoot(strings.TrimSpace(snapshot.StateMerkleRoot), expectedMerkle) {
		return fmt.Errorf("state_merkle_root_mismatch")
	}
	if v.Node != nil {
		if ok, reason := v.Node.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok && strings.TrimSpace(reason) != "anchor_block_unavailable" {
			return errors.New(strings.TrimSpace(reason))
		}
	}
	return nil
}

func snapshotFinalityMetadataRejectReason(snapshot *StateSnapshot) string {
	if snapshot == nil {
		return "snapshot_metadata_invalid"
	}
	cert := snapshot.FinalityCertificate
	finalizedHash := strings.TrimSpace(snapshot.FinalizedHash)
	finalizedStateRoot := strings.TrimSpace(snapshot.FinalizedStateRoot)
	finalizedSetHash := strings.TrimSpace(snapshot.FinalizedValidatorSetHash)
	finalizedSetRoot := strings.TrimSpace(snapshot.FinalizedValidatorSetRoot)
	epochAnchor := strings.TrimSpace(snapshot.EpochAnchorHash)
	previousAnchor := strings.TrimSpace(snapshot.PreviousEpochAnchorHash)
	finalityRoot := strings.TrimSpace(snapshot.FinalityRoot)
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
	if required := cert.StrictQuorum; required > 0 && len(canonicalValidatorIDs(cert.Signers)) > 0 && len(canonicalValidatorIDs(cert.Signers)) < required {
		return "snapshot_finality_certificate_quorum_missing"
	}
	return ""
}

func (n *Node) snapshotLocalFinalityRejectReason(snapshot *StateSnapshot) string {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return ""
	}
	localHeight := uint64(0)
	localFinalized := uint64(0)
	if n.Blockchain != nil {
		localHeight = n.Blockchain.Height()
		if chainFinalized := n.Blockchain.FinalizedHeight(); chainFinalized > localFinalized {
			localFinalized = chainFinalized
		}
	}
	n.commitMu.Lock()
	committed := n.committedHeight
	nodeFinalized := n.finalizedHeight
	n.commitMu.Unlock()
	// Peer heartbeat majority can advance n.finalizedHeight ahead of the local
	// block anchor while a full node is catching up. Snapshot regression checks
	// must only use locally anchored finality, otherwise a valid catch-up
	// snapshot below the remote-observed network head is rejected before apply.
	if localHeight > 0 && committed <= localHeight && committed > localFinalized {
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
		if expected, found, err := n.loadFinalizedHashInvariant(snapshot.Height); err == nil && found &&
			expected != "" && !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(snapshot.BlockHash)) {
			return "snapshot_irreversible_hash_conflict"
		}
	}
	if snapshot.FinalizedHeight > 0 && strings.TrimSpace(snapshot.FinalizedHash) != "" {
		if expected, found, err := n.loadFinalizedHashInvariant(snapshot.FinalizedHeight); err == nil && found &&
			expected != "" && !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(snapshot.FinalizedHash)) {
			return "snapshot_finalized_hash_conflict"
		}
	}
	return ""
}
