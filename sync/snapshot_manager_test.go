package syncpipeline

import (
	"context"
	"errors"
	"testing"
)

type testMetaRequester struct {
	meta SnapshotMeta
	err  error
}

func (t testMetaRequester) RequestSnapshotMeta(context.Context, uint64, uint64) (SnapshotMeta, error) {
	return t.meta, t.err
}

type testVerifier struct {
	proofs   []SnapshotProof
	required int
	rootErr  error
}

func (t testVerifier) CollectSnapshotProofs(context.Context, SnapshotMeta) ([]SnapshotProof, error) {
	return t.proofs, nil
}
func (t testVerifier) RequiredProofs(context.Context, SnapshotMeta) int {
	return t.required
}
func (t testVerifier) VerifyProof(context.Context, SnapshotProof) bool {
	return true
}
func (t testVerifier) VerifyStateRoot(context.Context, SnapshotMeta) error {
	return t.rootErr
}

type testChunks struct {
	err error
}

func (t testChunks) DownloadSnapshotChunks(context.Context, SnapshotMeta) error {
	return t.err
}
func (t testChunks) ParallelChunks() int {
	return 8
}

type testApplier struct {
	applyErr  error
	replayErr error
	applied   int
	replayed  int
}

func (t *testApplier) ApplySnapshot(context.Context, SnapshotMeta) error {
	t.applied++
	return t.applyErr
}
func (t *testApplier) DeltaReplay(context.Context, uint64, uint64) error {
	t.replayed++
	return t.replayErr
}

func TestSnapshotManagerNoopWhenUpToDate(t *testing.T) {
	m := &SnapshotManager{}
	out, err := m.StartSync(context.Background(), SyncInput{LocalHeight: 100, NetworkHeight: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.UsedSnapshot {
		t.Fatalf("expected no snapshot usage")
	}
}

func TestSnapshotManagerRunsFullPipeline(t *testing.T) {
	applier := &testApplier{}
	m := &SnapshotManager{
		Meta: testMetaRequester{
			meta: SnapshotMeta{
				Height:       448,
				Chunks:       8,
				StateRoot:    "0xabc123",
				SnapshotHash: "0xhash",
			},
		},
		Verifier: testVerifier{
			proofs: []SnapshotProof{
				{Validator: "A"},
				{Validator: "B"},
				{Validator: "C"},
			},
			required: 3,
		},
		Chunks:  testChunks{},
		Applier: applier,
	}

	out, err := m.StartSync(context.Background(), SyncInput{LocalHeight: 0, NetworkHeight: 500})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.UsedSnapshot {
		t.Fatalf("expected snapshot usage")
	}
	if out.SnapshotHeight != 448 {
		t.Fatalf("unexpected snapshot height: %d", out.SnapshotHeight)
	}
	if applier.applied != 1 {
		t.Fatalf("expected snapshot apply call")
	}
	if applier.replayed != 1 {
		t.Fatalf("expected delta replay call")
	}
}

func TestSnapshotManagerFailsWhenProofQuorumMissing(t *testing.T) {
	applier := &testApplier{}
	m := &SnapshotManager{
		Meta: testMetaRequester{
			meta: SnapshotMeta{
				Height:       448,
				Chunks:       8,
				StateRoot:    "0xabc123",
				SnapshotHash: "0xhash",
			},
		},
		Verifier: testVerifier{
			proofs: []SnapshotProof{
				{Validator: "A"},
			},
			required: 3,
		},
		Chunks:  testChunks{},
		Applier: applier,
	}

	_, err := m.StartSync(context.Background(), SyncInput{LocalHeight: 0, NetworkHeight: 500})
	if err == nil {
		t.Fatalf("expected proof quorum error")
	}
}

func TestSnapshotManagerPropagatesDeltaReplayError(t *testing.T) {
	applier := &testApplier{replayErr: errors.New("delta replay failed")}
	m := &SnapshotManager{
		Meta: testMetaRequester{
			meta: SnapshotMeta{
				Height:       448,
				Chunks:       8,
				StateRoot:    "0xabc123",
				SnapshotHash: "0xhash",
			},
		},
		Verifier: testVerifier{
			proofs: []SnapshotProof{
				{Validator: "A"},
				{Validator: "B"},
				{Validator: "C"},
			},
			required: 3,
		},
		Chunks:  testChunks{},
		Applier: applier,
	}

	_, err := m.StartSync(context.Background(), SyncInput{LocalHeight: 0, NetworkHeight: 500})
	if err == nil {
		t.Fatalf("expected delta replay error")
	}
}
