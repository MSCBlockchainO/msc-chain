package syncpipeline

import "context"

type SnapshotMeta struct {
	// `Height` stores the value associated with this record.
	Height           uint64
	// `Chunks` stores the value associated with this record.
	Chunks           int
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot        string
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash     string
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64
	// `Providers` stores the value associated with this record.
	Providers        []string
}

type SnapshotMetaRequester interface {
	RequestSnapshotMeta(ctx context.Context, localHeight uint64, networkHeight uint64) (SnapshotMeta, error)
}
