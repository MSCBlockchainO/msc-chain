package syncpipeline

import (
	"context"
	"errors"
	"fmt"
)

type SyncInput struct {
	LocalHeight   uint64
	NetworkHeight uint64
}

type SyncResult struct {
	UsedSnapshot    bool
	SnapshotHeight  uint64
	ProofVotes      int
	ProofRequired   int
	ReplicationMin  int
	ReplicationUsed []string
	WarmupBlocks    uint64
	DeltaReplayFrom uint64
	DeltaReplayTo   uint64
}

type SnapshotManager struct {
	Meta       SnapshotMetaRequester
	Verifier   SnapshotVerifier
	Chunks     SnapshotChunkDownloader
	Applier    SnapshotApplier
	PeerSource func() []Peer
}

func (m *SnapshotManager) StartSync(ctx context.Context, input SyncInput) (SyncResult, error) {
	result := SyncResult{
		UsedSnapshot: false,
	}
	if input.NetworkHeight == 0 || input.LocalHeight >= input.NetworkHeight {
		return result, nil
	}
	if m == nil || m.Meta == nil || m.Verifier == nil || m.Chunks == nil || m.Applier == nil {
		return result, errors.New("snapshot manager dependencies unavailable")
	}

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
		peers := SelectSnapshotPeers(m.PeerSource())
		if len(peers) > 0 {
			const defaultReplicationMin = 3
			replicas := SelectSnapshotReplicationPeers(peers, defaultReplicationMin, meta.SnapshotHash)
			result.ReplicationMin = defaultReplicationMin
			result.ReplicationUsed = peerIDs(replicas)
		}
	}

	votes, required, err := VerifySnapshotProofQuorum(ctx, m.Verifier, meta)
	if err != nil {
		return result, err
	}
	if err := m.Chunks.DownloadSnapshotChunks(ctx, meta); err != nil {
		return result, fmt.Errorf("download snapshot chunks: %w", err)
	}
	if err := m.Verifier.VerifyStateRoot(ctx, meta); err != nil {
		return result, fmt.Errorf("verify snapshot state root: %w", err)
	}
	if err := m.Applier.ApplySnapshot(ctx, meta); err != nil {
		return result, fmt.Errorf("apply snapshot: %w", err)
	}

	result.UsedSnapshot = true
	result.SnapshotHeight = meta.Height
	result.ProofVotes = votes
	result.ProofRequired = required

	if meta.Height < input.NetworkHeight {
		if err := m.Applier.DeltaReplay(ctx, meta.Height, input.NetworkHeight); err != nil {
			return result, fmt.Errorf("delta replay recent blocks: %w", err)
		}
		result.DeltaReplayFrom = meta.Height
		result.DeltaReplayTo = input.NetworkHeight
	}

	return result, nil
}

func peerIDs(peers []Peer) []string {
	if len(peers) == 0 {
		return nil
	}
	out := make([]string, 0, len(peers))
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
