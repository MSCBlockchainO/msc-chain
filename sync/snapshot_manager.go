package syncpipeline

import (
	"context"
	"errors"
	"fmt"
)

type SyncInput struct {
	// `LocalHeight` stores the value associated with this record.
	LocalHeight   uint64
	// `NetworkHeight` stores the value associated with this record.
	NetworkHeight uint64
}

type SyncResult struct {
	// `UsedSnapshot` stores the value associated with this record.
	UsedSnapshot    bool
	// `SnapshotHeight` stores the value associated with this record.
	SnapshotHeight  uint64
	// `ProofVotes` stores the value associated with this record.
	ProofVotes      int
	// `ProofRequired` stores the value associated with this record.
	ProofRequired   int
	// `ReplicationMin` stores the value associated with this record.
	ReplicationMin  int
	// `ReplicationUsed` stores the value associated with this record.
	ReplicationUsed []string
	// `WarmupBlocks` stores the value associated with this record.
	WarmupBlocks    uint64
	// `DeltaReplayFrom` stores the value associated with this record.
	DeltaReplayFrom uint64
	// `DeltaReplayTo` stores the value associated with this record.
	DeltaReplayTo   uint64
}

type SnapshotManager struct {
	// `Meta` stores the value associated with this record.
	Meta       SnapshotMetaRequester
	// `Verifier` stores the value associated with this record.
	Verifier   SnapshotVerifier
	// `Chunks` stores the value associated with this record.
	Chunks     SnapshotChunkDownloader
	// `Applier` stores the value associated with this record.
	Applier    SnapshotApplier
	// `PeerSource` stores the value associated with this record.
	PeerSource func() []Peer
}

// StartSync starts sync.
func (m *SnapshotManager) StartSync(ctx context.Context, input SyncInput) (SyncResult, error) {
	// `result` stores the result produced by this operation.
	result := SyncResult{
		UsedSnapshot: false,
	}
	if input.NetworkHeight == 0 || input.LocalHeight >= input.NetworkHeight {
		return result, nil
	}
	if m == nil || m.Meta == nil || m.Verifier == nil || m.Chunks == nil || m.Applier == nil {
		return result, errors.New("snapshot manager dependencies unavailable")
	}

	// `meta` and `err` store the error produced by this operation.
	meta, err := m.Meta.RequestSnapshotMeta(ctx, input.LocalHeight, input.NetworkHeight)
	if err != nil {
		return result, fmt.Errorf("request snapshot metadata: %w", err)
	}
	if meta.Height == 0 {
		return result, errors.New("snapshot metadata height unavailable")
	}
	if meta.Height > input.NetworkHeight {
		return result, fmt.Errorf("snapshot metadata height %d above network height %d", meta.Height, input.NetworkHeight)
	}
	if meta.Height < input.LocalHeight {
		return result, fmt.Errorf("snapshot metadata height %d below local height %d", meta.Height, input.LocalHeight)
	}
	if m != nil && m.PeerSource != nil {
		// `peers` stores the value produced by this operation.
		peers := SelectSnapshotPeers(m.PeerSource())
		if len(peers) > 0 {
			// `defaultReplicationMin` defines the constant value used by this package.
			const defaultReplicationMin = 3
			// `replicas` stores the value produced by this operation.
			replicas := SelectSnapshotReplicationPeers(peers, defaultReplicationMin, meta.SnapshotHash)
			result.ReplicationMin = defaultReplicationMin
			result.ReplicationUsed = peerIDs(replicas)
		}
	}

	// `votes`, `required`, and `err` store the error produced by this operation.
	votes, required, err := VerifySnapshotProofQuorum(ctx, m.Verifier, meta)
	if err != nil {
		return result, err
	}
	// `err` stores the error produced by this operation.
	if err := m.Chunks.DownloadSnapshotChunks(ctx, meta); err != nil {
		return result, fmt.Errorf("download snapshot chunks: %w", err)
	}
	// `err` stores the error produced by this operation.
	if err := m.Verifier.VerifyStateRoot(ctx, meta); err != nil {
		return result, fmt.Errorf("verify snapshot state root: %w", err)
	}
	// `err` stores the error produced by this operation.
	if err := m.Applier.ApplySnapshot(ctx, meta); err != nil {
		return result, fmt.Errorf("apply snapshot: %w", err)
	}

	result.UsedSnapshot = true
	result.SnapshotHeight = meta.Height
	result.ProofVotes = votes
	result.ProofRequired = required

	if meta.Height < input.NetworkHeight {
		// `err` stores the error produced by this operation.
		if err := m.Applier.DeltaReplay(ctx, meta.Height, input.NetworkHeight); err != nil {
			return result, fmt.Errorf("delta replay recent blocks: %w", err)
		}
		result.DeltaReplayFrom = meta.Height
		result.DeltaReplayTo = input.NetworkHeight
	}

	return result, nil
}

// peerIDs implements the peer i ds helper.
func peerIDs(peers []Peer) []string {
	if len(peers) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(peers))
	// `peer` tracks the current values while iterating.
	for _, peer := range peers {
		if peer.ID == "" {
			continue
		}
		out = append(out, peer.ID)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
