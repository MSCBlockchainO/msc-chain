package syncpipeline

import "context"

type SnapshotApplier interface {
	ApplySnapshot(ctx context.Context, meta SnapshotMeta) error
	DeltaReplay(ctx context.Context, fromHeight uint64, toHeight uint64) error
}
