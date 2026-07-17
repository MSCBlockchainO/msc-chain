package main

import (
	"fmt"
	"strings"
)

type SnapshotManager struct {
	// `Node` stores the value associated with this record.
	Node             *Node
	// `TargetHeight` stores the value associated with this record.
	TargetHeight     uint64
	// `MinHeight` stores the value associated with this record.
	MinHeight        uint64
	// `StrictCoreQuorum` stores the value associated with this record.
	StrictCoreQuorum bool
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64
	// `Retries` stores the value associated with this record.
	Retries          int
	// `RequiredProofs` stores the request data being processed.
	RequiredProofs   int
	// `Proofs` stores the value associated with this record.
	Proofs           map[string]SnapshotProof
	// `Candidate` stores the value associated with this record.
	Candidate        *strictSnapshotMetaCandidate
	// `Snapshot` stores the value associated with this record.
	Snapshot         *StateSnapshot
	// `ExecPool` stores the value associated with this record.
	ExecPool         *ExecPoolSnapshot
	// `Meta` stores the value associated with this record.
	Meta             *SnapshotMetaRecord
	// `Source` stores the value associated with this record.
	Source           string
	// `Stored` stores the value associated with this record.
	Stored           bool
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(n *Node, targetHeight uint64, minHeight uint64, strictCoreQuorum bool) *SnapshotManager {
	return &SnapshotManager{
		Node:             n,
		TargetHeight:     targetHeight,
		MinHeight:        minHeight,
		StrictCoreQuorum: strictCoreQuorum,
		Proofs:           make(map[string]SnapshotProof),
	}
}

// DiscoverCheckpoint implements the discover checkpoint helper.
func (m *SnapshotManager) DiscoverCheckpoint() error {
	if m == nil || m.Node == nil {
		return fmt.Errorf("snapshot manager unavailable")
	}
	if m.TargetHeight == 0 {
		return fmt.Errorf("snapshot target unavailable")
	}
	// `session` stores the value produced by this operation.
	session := m.Node.snapshotSessionSnapshot()
	if !session.Active || session.FreezeHeight == 0 {
		session = m.Node.startSnapshotSession(m.TargetHeight, "snapshot_manager")
	}
	m.CheckpointHeight = session.CheckpointHeight
	// `cache` and `ok` store whether the related condition is satisfied.
	if cache, ok := m.Node.cachedSnapshotAnchor(m.TargetHeight, m.MinHeight); ok {
		if cache.Height > 0 && cache.Height <= m.TargetHeight {
			m.TargetHeight = cache.Height
		}
		if cache.CheckpointHeight > 0 {
			m.CheckpointHeight = cache.CheckpointHeight
		}
	}
	if m.CheckpointHeight == 0 {
		m.CheckpointHeight = snapshotCheckpointHeightFor(m.TargetHeight)
	}
	m.Retries = int(session.RetryCount)
	return nil
}

// CollectProofs implements the collect proofs helper.
func (m *SnapshotManager) CollectProofs() error {
	if m == nil || m.Node == nil {
		return fmt.Errorf("snapshot manager unavailable")
	}
	// `collector` stores the value produced by this operation.
	collector := SnapshotProofCollector{
		Node:             m.Node,
		TargetHeight:     m.TargetHeight,
		MinHeight:        m.MinHeight,
		CheckpointHeight: m.CheckpointHeight,
		StrictCoreQuorum: m.StrictCoreQuorum,
	}
	// `quorum` and `err` store the error produced by this operation.
	quorum, err := collector.Collect()
	if err != nil {
		return err
	}
	if quorum == nil {
		return nil
	}
	m.RequiredProofs = quorum.Required
	m.Candidate = quorum.Candidate
	if len(quorum.Proofs) > 0 {
		m.Proofs = quorum.Proofs
	}
	return nil
}

// DownloadSnapshot implements the download snapshot helper.
func (m *SnapshotManager) DownloadSnapshot() error {
	if m == nil || m.Node == nil {
		return fmt.Errorf("snapshot manager unavailable")
	}
	// `snapshot`, `execPool`, and `err` store the error produced by this operation.
	snapshot, execPool, err := m.Node.fetchTrustedSnapshot(m.TargetHeight, m.MinHeight, m.StrictCoreQuorum)
	if err != nil {
		m.Retries = SnapshotRetryController{Node: m.Node}.RetryCount()
		return err
	}
	m.Snapshot = snapshot
	m.ExecPool = execPool
	if snapshot != nil && snapshot.Height > 0 && snapshot.Height < m.TargetHeight {
		m.Node.retargetSnapshotSessionLowerAvailable(m.TargetHeight, snapshot.Height, snapshotCheckpointHeightFor(snapshot.Height), "snapshot_manager_lower_available")
	}
	return nil
}

// VerifySnapshot verifies snapshot.
func (m *SnapshotManager) VerifySnapshot() error {
	if m == nil {
		return fmt.Errorf("snapshot manager unavailable")
	}
	return (SnapshotVerifier{Node: m.Node}).Verify(m.Snapshot)
}

// PersistSnapshot persists snapshot.
func (m *SnapshotManager) PersistSnapshot(source string) error {
	if m == nil || m.Node == nil {
		return fmt.Errorf("snapshot manager unavailable")
	}
	if m.Snapshot == nil || m.Snapshot.Height == 0 {
		return fmt.Errorf("snapshot unavailable")
	}
	if strings.TrimSpace(source) == "" {
		source = "trusted_snapshot_download"
	}
	if m.Node.committedStateSnapshotRecordExists(m.Snapshot.Height) {
		// `meta` and `err` store the error produced by this operation.
		meta, err := m.Node.loadSnapshotMetaRecord(m.Snapshot.Height)
		if err != nil || meta == nil {
			_ = m.Node.ensureSnapshotMetaRecord(m.Snapshot, source)
			meta, _ = m.Node.loadSnapshotMetaRecord(m.Snapshot.Height)
		}
		if meta == nil {
			meta = snapshotMetaFromSnapshot(m.Snapshot, source, "committed_full", snapshotBaseHeight(m.Snapshot.Height))
		}
		m.Meta = meta
		m.Source = strings.TrimSpace(source)
		m.Stored = true
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := m.Node.storeCommittedStateSnapshotRecord(m.Snapshot, source); err != nil {
		return err
	}
	// `meta` and `err` store the error produced by this operation.
	meta, err := m.Node.loadSnapshotMetaRecord(m.Snapshot.Height)
	if err != nil || meta == nil {
		_ = m.Node.ensureSnapshotMetaRecord(m.Snapshot, source)
		meta, _ = m.Node.loadSnapshotMetaRecord(m.Snapshot.Height)
	}
	if meta == nil {
		meta = snapshotMetaFromSnapshot(m.Snapshot, source, "committed_full", snapshotBaseHeight(m.Snapshot.Height))
	}
	m.Meta = meta
	m.Source = strings.TrimSpace(source)
	m.Stored = true
	_ = m.Node.publishSnapshotMetaGossip(m.Snapshot)
	_ = m.Node.publishSnapshotChunkGossip(m.Snapshot)
	return nil
}

// ApplySnapshot applies snapshot.
func (m *SnapshotManager) ApplySnapshot(allowReapply bool) error {
	if m == nil || m.Node == nil {
		return fmt.Errorf("snapshot manager unavailable")
	}
	if m.Snapshot == nil || m.Snapshot.Height == 0 {
		return fmt.Errorf("snapshot unavailable")
	}
	// `reason` stores the value produced by this operation.
	if reason := m.Node.snapshotLocalFinalityRejectReason(m.Snapshot); reason != "" {
		m.Node.recordSnapshotSessionStrictResult("", reason)
		m.Node.snapshotSessionMarkFailure(reason)
		return fmt.Errorf("snapshot finality guard rejected: %s", reason)
	}
	// `localHeight` stores the value produced by this operation.
	localHeight := uint64(0)
	if m.Node.Blockchain != nil {
		localHeight = m.Node.Blockchain.Height()
	}
	if !m.Node.snapshotSessionApplyAttemptAllowed(strings.TrimSpace(m.Snapshot.SnapshotHash), localHeight, m.TargetHeight) {
		m.Node.recordSnapshotSessionStrictResult("", "same_snapshot_reapply")
		m.Node.snapshotSessionMarkFailure("same_snapshot_reapply")
		// `next` stores the value produced by this operation.
		next := m.Node.rotateSnapshotProvider()
		m.Node.persistSnapshotSessionState("same_snapshot_reapply_manager")
		return fmt.Errorf("snapshot reapply loop detected; rotated provider=%s", strings.TrimSpace(next))
	}
	if localHeight > 0 && m.Snapshot.Height < localHeight {
		m.Node.recordSnapshotSessionStrictResult("", "snapshot_height_regression")
		m.Node.snapshotSessionMarkFailure("snapshot_height_regression")
		return fmt.Errorf("snapshot height regression rejected: local=%d snapshot=%d target=%d", localHeight, m.Snapshot.Height, m.TargetHeight)
	}
	// `applied` stores the value produced by this operation.
	applied := false
	if allowReapply && m.Snapshot.Height == localHeight {
		applied = m.Node.ApplySnapshotForRecovery(*m.Snapshot)
	} else {
		applied = m.Node.ApplySnapshotForSync(*m.Snapshot)
	}
	if !applied {
		m.Node.recordSnapshotSessionStrictResult("", "snapshot_apply_noop")
		m.Node.snapshotSessionMarkFailure("snapshot_apply_noop")
		return fmt.Errorf("snapshot apply did not anchor chain: local=%d snapshot=%d target=%d", localHeight, m.Snapshot.Height, m.TargetHeight)
	}
	m.Node.noteSnapshotApplied(m.Snapshot.Height)
	m.Node.markSnapshotSessionApplied(m.Snapshot, m.RequiredProofs)
	if m.ExecPool != nil {
		m.Node.mergeExecPoolSnapshot(*m.ExecPool)
	}
	return nil
}

// StartSession starts session.
func (m *SnapshotManager) StartSession(allowReapply bool) (*StateSnapshot, *ExecPoolSnapshot, error) {
	// `err` stores the error produced by this operation.
	if err := m.DiscoverCheckpoint(); err != nil {
		return nil, nil, err
	}
	_ = m.CollectProofs()
	// `err` stores the error produced by this operation.
	if err := m.DownloadSnapshot(); err != nil {
		return nil, nil, err
	}
	// `err` stores the error produced by this operation.
	if err := m.VerifySnapshot(); err != nil {
		return nil, nil, err
	}
	// `err` stores the error produced by this operation.
	if err := m.PersistSnapshot("trusted_snapshot_download"); err != nil {
		return nil, nil, err
	}
	// `err` stores the error produced by this operation.
	if err := m.ApplySnapshot(allowReapply); err != nil {
		return nil, nil, err
	}
	return m.Snapshot, m.ExecPool, nil
}
