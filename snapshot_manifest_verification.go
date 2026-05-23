package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func verifySnapshotPayloadAgainstManifest(payload []byte, manifest *SnapshotManifest, minHeight uint64) (*StateSnapshot, error) {
	if manifest == nil {
		return nil, fmt.Errorf("snapshot manifest unavailable")
	}
	normalizeSnapshotManifest(manifest)
	if !snapshotManifestBasicValid(manifest, manifest.Height, minHeight) {
		return nil, fmt.Errorf("invalid snapshot manifest")
	}
	chunkSize := manifest.ChunkSize
	if chunkSize == 0 {
		return nil, fmt.Errorf("invalid snapshot manifest")
	}
	for idx := uint64(0); idx < manifest.ChunkCount; idx++ {
		start := idx * chunkSize
		end := start + chunkSize
		if start > uint64(len(payload)) {
			return nil, fmt.Errorf("snapshot manifest chunk range mismatch index=%d", idx)
		}
		if end > uint64(len(payload)) {
			end = uint64(len(payload))
		}
		if idx >= uint64(len(manifest.ChunkHashes)) ||
			!strings.EqualFold(snapshotChunkHash(payload[start:end]), manifest.ChunkHashes[idx]) {
			return nil, fmt.Errorf("snapshot manifest checksum mismatch index=%d", idx)
		}
	}

	var snap StateSnapshot
	if err := json.Unmarshal(payload, &snap); err != nil {
		return nil, err
	}
	if len(snap.CheckpointProof) == 0 && len(manifest.CheckpointProof) > 0 {
		snap.CheckpointProof = copyStringMap(manifest.CheckpointProof)
	}
	populateSnapshotDerivedFields(&snap)
	if !strings.EqualFold(strings.TrimSpace(snap.SnapshotHash), manifest.SnapshotHash) {
		return nil, fmt.Errorf("snapshot manifest hash mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(snap.StateRoot), manifest.StateRoot) {
		return nil, fmt.Errorf("snapshot manifest state root mismatch")
	}
	if manifestStateMerkle := strings.TrimSpace(manifest.StateMerkleRoot); manifestStateMerkle != "" {
		if !strings.EqualFold(strings.TrimSpace(snap.StateMerkleRoot), manifestStateMerkle) {
			return nil, fmt.Errorf("snapshot manifest state merkle root mismatch")
		}
	}
	if !strings.EqualFold(strings.TrimSpace(snapshotValidatorSetHash(&snap)), manifest.ValidatorSetHash) {
		return nil, fmt.Errorf("snapshot manifest validator set hash mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(&snap)), manifest.ValidatorRegistryHash) {
		return nil, fmt.Errorf("snapshot manifest validator registry hash mismatch")
	}
	if manifest.FinalizedHeight > 0 && snap.FinalizedHeight != manifest.FinalizedHeight {
		return nil, fmt.Errorf("snapshot manifest finalized height mismatch")
	}
	if manifest.FinalizedHash != "" && !strings.EqualFold(strings.TrimSpace(snap.FinalizedHash), manifest.FinalizedHash) {
		return nil, fmt.Errorf("snapshot manifest finalized hash mismatch")
	}
	if manifest.EpochAnchorHash != "" && !strings.EqualFold(strings.TrimSpace(snap.EpochAnchorHash), manifest.EpochAnchorHash) {
		return nil, fmt.Errorf("snapshot manifest epoch anchor mismatch")
	}
	if manifest.FinalityRoot != "" && !strings.EqualFold(strings.TrimSpace(snap.FinalityRoot), manifest.FinalityRoot) {
		return nil, fmt.Errorf("snapshot manifest finality root mismatch")
	}
	return &snap, nil
}
