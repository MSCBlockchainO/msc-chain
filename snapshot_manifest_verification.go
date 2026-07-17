package main

import (
	"fmt"
	"strings"
)

const (
	// Snapshot manifests are received from untrusted peers. Keep their declared
	// layout within the same limits used by startup snapshot recovery before any
	// slice, channel, buffer, or on-disk resume state is allocated.
	maxSnapshotTransferPayloadBytes = uint64(1 << 30)
	maxSnapshotTransferChunkCount   = uint64(1 << 16)
	maxSnapshotManifestFieldBytes   = 512
	maxSnapshotManifestProofEntries = 4096
)

func snapshotChunkLayoutValid(chunkSize uint64, chunkCount uint64, payloadSize uint64) bool {
	if chunkSize == 0 || chunkSize > maxSnapshotTransferPayloadBytes ||
		chunkCount == 0 || chunkCount > maxSnapshotTransferChunkCount ||
		payloadSize > maxSnapshotTransferPayloadBytes {
		return false
	}
	if payloadSize > 0 {
		expectedChunks := (payloadSize-1)/chunkSize + 1
		return chunkCount == expectedChunks
	}
	// Legacy metadata did not publish an exact payload size. Bound the maximum
	// possible assembled payload without allowing uint64 multiplication to wrap.
	return chunkCount <= maxSnapshotTransferPayloadBytes/chunkSize
}

func snapshotManifestPayloadLayoutValid(manifest *SnapshotManifest, payloadSize uint64) bool {
	if manifest == nil || payloadSize == 0 || payloadSize > maxSnapshotTransferPayloadBytes {
		return false
	}
	if manifest.SnapshotSizeBytes > 0 && manifest.SnapshotSizeBytes != payloadSize {
		return false
	}
	return snapshotChunkLayoutValid(manifest.ChunkSize, manifest.ChunkCount, payloadSize)
}

func snapshotManifestChunkLengthValid(manifest *SnapshotManifest, index uint64, length uint64) bool {
	if manifest == nil || index >= manifest.ChunkCount || length == 0 || length > manifest.ChunkSize {
		return false
	}
	if manifest.SnapshotSizeBytes > 0 {
		if !snapshotChunkLayoutValid(manifest.ChunkSize, manifest.ChunkCount, manifest.SnapshotSizeBytes) {
			return false
		}
		start := index * manifest.ChunkSize
		remaining := manifest.SnapshotSizeBytes - start
		expected := manifest.ChunkSize
		if remaining < expected {
			expected = remaining
		}
		return length == expected
	}
	// Without an exact legacy payload size, every non-final chunk must still be
	// full-sized; otherwise the same byte stream has ambiguous chunk boundaries.
	return index+1 == manifest.ChunkCount || length == manifest.ChunkSize
}

func snapshotManifestChunkDataValid(manifest *SnapshotManifest, index uint64, data []byte) bool {
	return snapshotManifestChunkLengthValid(manifest, index, uint64(len(data)))
}

// verifySnapshotPayloadAgainstManifest verifies snapshot payload against manifest.
func verifySnapshotPayloadAgainstManifest(payload []byte, manifest *SnapshotManifest, minHeight uint64) (*StateSnapshot, error) {
	if manifest == nil {
		return nil, fmt.Errorf("snapshot manifest unavailable")
	}
	normalizeSnapshotManifest(manifest)
	if !snapshotManifestBasicValid(manifest, manifest.Height, minHeight) {
		return nil, fmt.Errorf("invalid snapshot manifest")
	}
	// `chunkSize` stores the measured quantity used by this operation.
	chunkSize := manifest.ChunkSize
	if chunkSize == 0 {
		return nil, fmt.Errorf("invalid snapshot manifest")
	}
	if manifest.SnapshotSizeBytes > 0 && uint64(len(payload)) != manifest.SnapshotSizeBytes {
		return nil, fmt.Errorf("snapshot manifest payload size mismatch")
	}
	if !snapshotManifestPayloadLayoutValid(manifest, uint64(len(payload))) {
		return nil, fmt.Errorf("snapshot manifest payload size mismatch")
	}
	// `idx` stores the current position in the related collection.
	for idx := uint64(0); idx < manifest.ChunkCount; idx++ {
		// `start` stores the value produced by this operation.
		start := idx * chunkSize
		// `end` stores the value produced by this operation.
		end := start + chunkSize
		if start >= uint64(len(payload)) {
			return nil, fmt.Errorf("snapshot manifest chunk range mismatch index=%d", idx)
		}
		if end > uint64(len(payload)) {
			end = uint64(len(payload))
		}
		if !snapshotManifestChunkDataValid(manifest, idx, payload[start:end]) ||
			idx >= uint64(len(manifest.ChunkHashes)) ||
			!strings.EqualFold(snapshotChunkHash(payload[start:end]), manifest.ChunkHashes[idx]) {
			return nil, fmt.Errorf("snapshot manifest checksum mismatch index=%d", idx)
		}
	}

	// `snap` stores the value used by this operation.
	var snap StateSnapshot
	// `err` stores the error produced by this operation.
	if err := UnmarshalSnapshotBinary(payload, &snap); err != nil {
		return nil, err
	}
	if len(snap.CheckpointProof) == 0 && len(manifest.CheckpointProof) > 0 {
		snap.CheckpointProof = copyStringMap(manifest.CheckpointProof)
	}
	populateSnapshotDerivedFields(&snap)
	if reason := snapshotIntrinsicMetadataRejectReason(&snap); reason != "" {
		return nil, fmt.Errorf("snapshot payload metadata invalid: %s", reason)
	}
	if reason := snapshotExecutionAuthorityRejectReason(&snap); reason != "" {
		return nil, fmt.Errorf("snapshot payload metadata invalid: %s", reason)
	}
	if snap.Height != manifest.Height {
		return nil, fmt.Errorf("snapshot manifest height mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(snap.SnapshotHash), manifest.SnapshotHash) {
		return nil, fmt.Errorf("snapshot manifest hash mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(snap.StateRoot), manifest.StateRoot) {
		return nil, fmt.Errorf("snapshot manifest state root mismatch")
	}
	// `manifestStateMerkle` stores the value produced by this operation.
	if manifestStateMerkle := strings.TrimSpace(manifest.StateMerkleRoot); manifestStateMerkle != "" {
		if !strings.EqualFold(strings.TrimSpace(snap.StateMerkleRoot), manifestStateMerkle) {
			return nil, fmt.Errorf("snapshot manifest state merkle root mismatch")
		}
	}
	if !strings.EqualFold(strings.TrimSpace(snapshotValidatorSetHash(&snap)), manifest.ValidatorSetHash) {
		return nil, fmt.Errorf("snapshot manifest validator set hash mismatch")
	}
	if manifest.ValidatorSetRoot != "" &&
		!strings.EqualFold(strings.TrimSpace(snapshotValidatorSetRoot(&snap)), manifest.ValidatorSetRoot) {
		return nil, fmt.Errorf("snapshot manifest validator set root mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(&snap)), manifest.ValidatorRegistryHash) {
		return nil, fmt.Errorf("snapshot manifest validator registry hash mismatch")
	}
	if manifest.PromotionWindowHash != "" &&
		!strings.EqualFold(strings.TrimSpace(snapshotPromotionWindowHash(&snap)), manifest.PromotionWindowHash) {
		return nil, fmt.Errorf("snapshot manifest promotion window hash mismatch")
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
