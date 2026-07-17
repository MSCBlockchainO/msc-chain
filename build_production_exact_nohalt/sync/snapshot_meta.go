package syncpipeline

import "context"

type SnapshotMeta struct {
	Height           uint64
	Chunks           int
	StateRoot        string
	SnapshotHash     string
	CheckpointHeight uint64
	Providers        []string
}

type SnapshotMetaRequester interface {
	RequestSnapshotMeta(ctx context.Context, localHeight uint64, networkHeight uint64) (SnapshotMeta, error)
}
