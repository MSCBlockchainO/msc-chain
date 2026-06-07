package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func testSnapshotForManifest() *StateSnapshot {
	ledger := NewLedger()
	return &StateSnapshot{
		Version:                  SnapshotVersion,
		Height:                   64,
		PrevHash:                 "h63",
		BlockHash:                "h64",
		StateRoot:                "state64",
		GenesisHash:              "genesis",
		Ledger:                   ledger,
		LedgerHash:               HashLedger(ledger),
		Validators:               map[string]bool{"A": true, "B": true, "C": true, "D": true},
		ValidatorRegistry:        map[string]ValidatorRecord{"A": {ID: "A", Stake: 400}, "B": {ID: "B", Stake: 300}, "C": {ID: "C", Stake: 200}, "D": {ID: "D", Stake: 100}},
		PendingValidators:        map[string]uint64{"F": 68},
		PendingValidatorRemovals: map[string]uint64{"D": 68},
		NextValidatorSetHeight:   65,
		ActivationHeight:         65,
		CheckpointProof: map[string]string{
			"A": "sig-a",
			"B": "sig-b",
			"C": "sig-c",
		},
	}
}

func TestSnapshotManifestFromSnapshotDeterministic(t *testing.T) {
	snap := testSnapshotForManifest()

	manifestA, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot first call: %v", err)
	}
	manifestB, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot second call: %v", err)
	}
	if !snapshotManifestMatches(manifestA, manifestB) {
		t.Fatalf("expected deterministic manifest, got A=%+v B=%+v", manifestA, manifestB)
	}
	if manifestA.ChunkCount == 0 || len(manifestA.ChunkHashes) == 0 {
		t.Fatalf("expected manifest chunk metadata")
	}
	if manifestA.ChunkCount != uint64(len(manifestA.ChunkHashes)) {
		t.Fatalf("chunk count mismatch: count=%d hashes=%d", manifestA.ChunkCount, len(manifestA.ChunkHashes))
	}
	if manifestA.ValidatorRegistryHash != strings.TrimSpace(snapshotValidatorRegistryHash(snap)) {
		t.Fatalf("manifest must carry validator registry hash: got=%q want=%q", manifestA.ValidatorRegistryHash, snapshotValidatorRegistryHash(snap))
	}
	if manifestA.ValidatorSetRoot != strings.TrimSpace(snapshotValidatorSetRoot(snap)) {
		t.Fatalf("manifest must carry validator set root: got=%q want=%q", manifestA.ValidatorSetRoot, snapshotValidatorSetRoot(snap))
	}
	if manifestA.Encoding != "binary" || manifestA.Compression != "zstd" {
		t.Fatalf("expected binary/zstd manifest codec, got encoding=%q compression=%q", manifestA.Encoding, manifestA.Compression)
	}
}

func TestSnapshotStubUsesManifestCheckpointProofFallback(t *testing.T) {
	snap := testSnapshotForManifest()
	manifest, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}
	meta := &SnapshotMetaResponse{
		Available: true,
		Height:    manifest.Height,
		Manifest:  manifest,
	}

	stub := snapshotStubFromMeta(meta)
	if stub == nil {
		t.Fatalf("expected snapshot stub")
	}
	if got := stub.CheckpointProof["A"]; got != "sig-a" {
		t.Fatalf("expected manifest checkpoint proof fallback, got %q", got)
	}
	if stub.ValidatorSetRoot != manifest.ValidatorSetRoot {
		t.Fatalf("expected manifest validator set root fallback, got=%q want=%q", stub.ValidatorSetRoot, manifest.ValidatorSetRoot)
	}
}

func TestSnapshotMetaResponseExposesCheckpointProof(t *testing.T) {
	snap := testSnapshotForManifest()
	resp := snapshotMetaResponse(snap, nil, "local")
	proof, ok := resp["checkpoint_proof"].(map[string]string)
	if !ok {
		t.Fatalf("expected checkpoint_proof map in snapshot metadata response")
	}
	if got := proof["B"]; got != "sig-b" {
		t.Fatalf("unexpected checkpoint proof value: got=%q want=%q", got, "sig-b")
	}
}

func TestMergeSnapshotProofsFromManifestAddsCachedQuorumProofs(t *testing.T) {
	snap := testSnapshotForManifest()
	manifest, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}

	downloaded := *snap
	downloaded.CheckpointProof = map[string]string{"A": "sig-a"}
	manifest.CheckpointProof = map[string]string{"A": "sig-a"}

	candidate := strictSnapshotMetaCandidateFromManifest(manifest)
	if candidate == nil {
		t.Fatalf("expected manifest candidate")
	}
	key := strictSnapshotMetaCandidateKey(candidate)
	node := &Node{
		snapshotProofs: map[string]map[string]SnapshotProof{
			key: {
				"B": {Validator: "B", SignatureHex: "sig-b"},
				"C": {Validator: "C", SignatureHex: "sig-c"},
			},
		},
	}

	node.mergeSnapshotProofsFromManifest(&downloaded, manifest)
	if got := downloaded.CheckpointProof["A"]; got != "sig-a" {
		t.Fatalf("existing proof changed: got=%q", got)
	}
	if got := downloaded.CheckpointProof["B"]; got != "sig-b" {
		t.Fatalf("expected cached B proof to merge, got=%q", got)
	}
	if got := downloaded.CheckpointProof["C"]; got != "sig-c" {
		t.Fatalf("expected cached C proof to merge, got=%q", got)
	}
}

func TestStrictSnapshotMetaObservationDefersLegacyRootlessProof(t *testing.T) {
	snap := testSnapshotForManifest()
	manifest, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}
	manifest.ValidatorSetRoot = ""
	meta := &SnapshotMetaResponse{
		Available: true,
		Height:    manifest.Height,
		Manifest:  manifest,
	}
	meta.ValidatorSetRoot = ""

	node := &Node{}
	obs, reason := node.strictSnapshotMetaObservationForTarget(meta, "peer-a", "A", manifest.Height, 0, true)
	if obs == nil {
		t.Fatalf("expected legacy rootless proof to be deferred, reason=%q", reason)
	}
	if obs.Reason != "checkpoint_proof_deferred" {
		t.Fatalf("unexpected observation reason: %q", obs.Reason)
	}
}

func TestStrictSnapshotMetaObservationDefersLegacyMetadataMissingSignFields(t *testing.T) {
	oldGate := SyncSnapshotCheckpointV2Height
	SyncSnapshotCheckpointV2Height = 0
	t.Cleanup(func() { SyncSnapshotCheckpointV2Height = oldGate })

	snap := testSnapshotForManifest()
	manifest, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}
	if manifest.ValidatorSetRoot == "" {
		t.Fatalf("test requires rooted metadata")
	}
	meta := &SnapshotMetaResponse{
		Available: true,
		Height:    manifest.Height,
		Manifest:  manifest,
	}
	meta.ValidatorSetRoot = manifest.ValidatorSetRoot

	node := &Node{}
	obs, reason := node.strictSnapshotMetaObservationForTarget(meta, "peer-a", "A", manifest.Height, 0, true)
	if obs == nil {
		t.Fatalf("expected legacy proof to be deferred while metadata omits block/ledger sign fields, reason=%q", reason)
	}
	if obs.Reason != "checkpoint_proof_deferred" {
		t.Fatalf("unexpected observation reason: %q", obs.Reason)
	}
}

func TestSnapshotDownloadExistingSnapshotRequiresFreshEnoughHeight(t *testing.T) {
	oldWindow := SyncUsableHeadRecentReplayWindowBlocks
	SyncUsableHeadRecentReplayWindowBlocks = 2048
	t.Cleanup(func() { SyncUsableHeadRecentReplayWindowBlocks = oldWindow })

	if !snapshotDownloadExistingSnapshotAcceptable(73299, 73299, 0) {
		t.Fatalf("exact target snapshot should be acceptable")
	}
	if !snapshotDownloadExistingSnapshotAcceptable(73000, 73299, 70000) {
		t.Fatalf("caller min-height should allow a selected high snapshot")
	}
	if !snapshotDownloadExistingSnapshotAcceptable(73200, 73299, 0) {
		t.Fatalf("recent snapshot should be acceptable for small delta replay")
	}
	if snapshotDownloadExistingSnapshotAcceptable(1537, 73299, 0) {
		t.Fatalf("very stale local snapshot must not block remote trusted download")
	}
	if snapshotDownloadExistingSnapshotAcceptable(1537, 73299, 70000) {
		t.Fatalf("snapshot below explicit min-height must be rejected")
	}
}

func TestManualSnapshotDownloadResetClearsActiveSession(t *testing.T) {
	node := &Node{
		snapshotSession: SnapshotSession{
			Active:             true,
			FreezeHeight:       73562,
			CheckpointHeight:   73536,
			CurrentProvider:    "peer-a",
			RetryCount:         9,
			ProviderSet:        []string{"peer-a"},
			StrictReasonCounts: map[string]uint64{"snapshot_anchor_timeout": 3},
		},
		syncSnapshotSessionFailures:      9,
		syncSnapshotSessionDegradedUntil: time.Now().Add(time.Minute),
	}

	node.resetActiveSnapshotSessionForManualDownload(73474, 73400)

	session := node.snapshotSessionSnapshot()
	if session.Active {
		t.Fatalf("manual snapshot download should clear stale active session: %+v", session)
	}
	node.syncMu.Lock()
	failures := node.syncSnapshotSessionFailures
	degradedUntil := node.syncSnapshotSessionDegradedUntil
	node.syncMu.Unlock()
	if failures != 0 {
		t.Fatalf("expected manual reset to clear failure counter, got %d", failures)
	}
	if !degradedUntil.IsZero() {
		t.Fatalf("expected manual reset to clear degraded cooldown, got %s", degradedUntil)
	}
}

func TestSnapshotChunkResponseUsesManifestBinaryPayload(t *testing.T) {
	snap := testSnapshotForManifest()
	manifest, payload, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}
	chunk, err := snapshotChunkResponseFromPayload(snap, manifest, payload, 0)
	if err != nil {
		t.Fatalf("snapshotChunkResponseFromPayload: %v", err)
	}
	if chunk.Encoding != "binary" || chunk.Compression != "zstd" {
		t.Fatalf("expected binary/zstd chunk codec, got encoding=%q compression=%q", chunk.Encoding, chunk.Compression)
	}
	if !strings.EqualFold(chunk.ChunkHash, manifest.ChunkHashes[0]) {
		t.Fatalf("chunk hash mismatch got=%s want=%s", chunk.ChunkHash, manifest.ChunkHashes[0])
	}
	if !bytes.Equal(chunk.Data, payload[:len(chunk.Data)]) {
		t.Fatalf("chunk data must be sliced from binary manifest payload")
	}
	var decoded StateSnapshot
	if err := UnmarshalSnapshotBinary(payload, &decoded); err != nil {
		t.Fatalf("binary snapshot payload must decode: %v", err)
	}
	if decoded.Height != snap.Height {
		t.Fatalf("decoded snapshot height=%d want=%d", decoded.Height, snap.Height)
	}
}

func TestSnapshotCheckpointProofBatchVerification(t *testing.T) {
	oldRequire := SyncTrustedSnapshotRequireCheckpointProof
	oldWorkers := SyncEd25519BatchVerifyWorkers
	validatorPubKeysMu.Lock()
	oldValidatorPubKeys := make(map[string]ed25519.PublicKey, len(ValidatorPubKeys))
	for id, pk := range ValidatorPubKeys {
		oldValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	oldGenesisPubKeys := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pk := range GenesisValidatorPubKeys {
		oldGenesisPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		SyncTrustedSnapshotRequireCheckpointProof = oldRequire
		SyncEd25519BatchVerifyWorkers = oldWorkers
		validatorPubKeysMu.Lock()
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisPubKeys
		validatorPubKeysMu.Unlock()
	})

	SyncTrustedSnapshotRequireCheckpointProof = true
	SyncEd25519BatchVerifyWorkers = 4
	snap := &StateSnapshot{
		Version:          SnapshotVersion,
		Height:           96,
		BlockHash:        "h96",
		StateRoot:        "state96",
		Ledger:           NewLedger(),
		LedgerHash:       HashLedger(NewLedger()),
		Validators:       map[string]bool{"A": true, "B": true, "C": true, "D": true},
		CheckpointHeight: snapshotCheckpointHeightFor(96),
		CheckpointDomain: syncSnapshotCheckpointDomain(),
		CheckpointProof:  make(map[string]string),
	}
	populateSnapshotDerivedFields(snap)
	signBytes := snapshotCheckpointSignBytes(snap)
	validatorPubKeysMu.Lock()
	ValidatorPubKeys = make(map[string]ed25519.PublicKey)
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)
	for _, id := range []string{"A", "B", "C", "D"} {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("generate key %s: %v", id, err)
		}
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		if id != "D" {
			snap.CheckpointProof[id] = hex.EncodeToString(ed25519.Sign(priv, signBytes))
		}
	}
	validatorPubKeysMu.Unlock()

	node := &Node{}
	if got := node.verifySnapshotCheckpointProofBatch(snap, []string{"A", "B", "C", "D"}, 3); got != 3 {
		t.Fatalf("expected 3 valid checkpoint proofs, got %d", got)
	}
	snap.CheckpointProof["B"] = strings.Repeat("00", ed25519.SignatureSize)
	if got := node.verifySnapshotCheckpointProofBatch(snap, []string{"A", "B", "C", "D"}, 3); got != 2 {
		t.Fatalf("expected one corrupted proof to reduce valid count to 2, got %d", got)
	}
}

func TestSnapshotManifestHashChangesOnRegistryOrChunkOrderMismatch(t *testing.T) {
	snap := testSnapshotForManifest()

	manifest, _, err := snapshotManifestFromSnapshot(snap)
	if err != nil {
		t.Fatalf("snapshotManifestFromSnapshot: %v", err)
	}

	mutatedRegistry := *manifest
	mutatedRegistry.ValidatorRegistryHash = "different-registry"
	if snapshotManifestMatches(manifest, &mutatedRegistry) {
		t.Fatalf("expected registry hash mismatch to change manifest identity")
	}

	mutatedChunks := *manifest
	mutatedChunks.ChunkHashes = append([]string{}, manifest.ChunkHashes...)
	if len(mutatedChunks.ChunkHashes) == 0 {
		t.Fatalf("expected at least one chunk hash")
	}
	mutatedChunks.ChunkHashes[0] = "tampered-chunk-hash"
	if snapshotManifestMatches(manifest, &mutatedChunks) {
		t.Fatalf("expected chunk hash mismatch to change manifest identity")
	}
}
