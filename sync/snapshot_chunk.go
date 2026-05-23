package syncpipeline

import "context"

type SnapshotChunkDownloader interface {
	DownloadSnapshotChunks(ctx context.Context, meta SnapshotMeta) error
	ParallelChunks() int
}
