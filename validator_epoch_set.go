package main

import (
	"sort"
	"strings"
)

const (
	// The live MSC network enters epoch-frozen validator membership at a
	// pre-announced 10,000-block boundary. Historical blocks keep their original
	// rules and operators have a safe window to deploy the same binary.
	protocolValidatorEpochSetV1Height uint64 = 100_001
	protocolValidatorEpochLength      uint64 = 10_000
)

func validatorEpochSetV1EnabledAt(height uint64) bool {
	return height >= protocolValidatorEpochSetV1Height
}

func validatorEpochLengthBlocks() uint64 {
	return protocolValidatorEpochLength
}

func validatorEpochNumber(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	return (height-1)/validatorEpochLengthBlocks() + 1
}

func validatorEpochStartHeight(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	return ((height-1)/validatorEpochLengthBlocks())*validatorEpochLengthBlocks() + 1
}

func validatorEpochEndHeight(height uint64) uint64 {
	start := validatorEpochStartHeight(height)
	if start == 0 {
		return 0
	}
	return start + validatorEpochLengthBlocks() - 1
}

func isValidatorEpochBoundary(height uint64) bool {
	return height > 0 && validatorEpochStartHeight(height) == height
}

func nextValidatorEpochBoundaryAtOrAfter(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	start := validatorEpochStartHeight(height)
	if start == height {
		return height
	}
	return start + validatorEpochLengthBlocks()
}

func validatorEpochTransitionHeight(scheduledHeight uint64) uint64 {
	if scheduledHeight == 0 || scheduledHeight == ^uint64(0) {
		return scheduledHeight
	}
	childHeight := scheduledHeight + 1
	if childHeight < protocolValidatorEpochSetV1Height {
		return childHeight
	}
	return nextValidatorEpochBoundaryAtOrAfter(childHeight)
}

func nextValidatorEpochBoundaryAfter(height uint64) uint64 {
	if height < protocolValidatorEpochSetV1Height {
		return protocolValidatorEpochSetV1Height
	}
	end := validatorEpochEndHeight(height)
	if end == 0 || end == ^uint64(0) {
		return end
	}
	return end + 1
}

func validatorEpochBlocksRemaining(height uint64) uint64 {
	next := nextValidatorEpochBoundaryAfter(height)
	if next <= height {
		return 0
	}
	return next - height
}

func validatorSignedInHeightRange(rec ValidatorRecord, start, end uint64) bool {
	if start == 0 || end < start {
		return false
	}
	for _, height := range rec.SignedHeights {
		if height >= start && height <= end {
			return true
		}
	}
	return false
}

func validatorRecordEpochEligible(rec ValidatorRecord, height uint64) bool {
	id := normalizeValidatorID(rec.ID)
	if id == "" || isProtocolValidatorBanned(id) || rec.Status != ValidatorActive {
		return false
	}
	if rec.JailUntilHeight > 0 && height < rec.JailUntilHeight {
		return false
	}
	if rec.JoinHeight > 0 && height < rec.JoinHeight {
		return false
	}
	return validatorPassesStakeGate(id, rec.Stake)
}

// selectEpochActiveValidatorSet preserves healthy active validators, promotes
// standby validators only at an epoch boundary, and caps consensus membership
// without limiting the registered validator registry.
func selectEpochActiveValidatorSet(height uint64, current []string, snapshot map[string]ValidatorRecord) []string {
	current = canonicalValidatorIDs(current)
	maxActive := protocolValidatorMaxActiveCommitteeValue()
	if maxActive <= 0 {
		maxActive = 25
	}
	if !validatorEpochSetV1EnabledAt(height) || !isValidatorEpochBoundary(height) {
		if len(current) > maxActive {
			return append([]string{}, current[:maxActive]...)
		}
		return current
	}
	if len(snapshot) == 0 {
		if len(current) > maxActive {
			return append([]string{}, current[:maxActive]...)
		}
		return current
	}

	registry := make(map[string]ValidatorRecord, len(snapshot))
	eligible := make([]ValidatorRecord, 0, len(snapshot))
	for key, value := range snapshot {
		rec := value
		rec.ID = normalizeValidatorID(rec.ID)
		if rec.ID == "" {
			rec.ID = normalizeValidatorID(key)
		}
		if rec.ID == "" {
			continue
		}
		registry[rec.ID] = rec
		if validatorRecordEpochEligible(rec, height) {
			eligible = append(eligible, rec)
		}
	}

	previousEnd := height - 1
	previousStart := uint64(1)
	if previousEnd >= validatorEpochLengthBlocks() {
		previousStart = previousEnd - validatorEpochLengthBlocks() + 1
	}
	hasSigningEvidence := false
	for _, id := range current {
		if rec, ok := registry[id]; ok && validatorSignedInHeightRange(rec, previousStart, previousEnd) {
			hasSigningEvidence = true
			break
		}
	}

	selected := make([]string, 0, maxActive)
	selectedSet := make(map[string]struct{}, maxActive)
	offlineFallback := make([]string, 0, len(current))
	for _, id := range current {
		rec, known := registry[id]
		if known && !validatorRecordEpochEligible(rec, height) {
			continue
		}
		if known && hasSigningEvidence && rec.JoinHeight <= previousStart && !validatorSignedInHeightRange(rec, previousStart, previousEnd) {
			offlineFallback = append(offlineFallback, id)
			continue
		}
		if len(selected) < maxActive {
			selected = append(selected, id)
			selectedSet[id] = struct{}{}
		}
	}

	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].JoinHeight != eligible[j].JoinHeight {
			return eligible[i].JoinHeight < eligible[j].JoinHeight
		}
		if eligible[i].Stake != eligible[j].Stake {
			return eligible[i].Stake > eligible[j].Stake
		}
		left := HashStrings([]string{"epoch_standby_v1", protocolChainID(), strings.TrimSpace(eligible[i].ID)})
		right := HashStrings([]string{"epoch_standby_v1", protocolChainID(), strings.TrimSpace(eligible[j].ID)})
		if left != right {
			return left < right
		}
		return eligible[i].ID < eligible[j].ID
	})
	for _, rec := range eligible {
		if len(selected) >= maxActive {
			break
		}
		if _, exists := selectedSet[rec.ID]; exists || containsNormalizedValidatorID(current, rec.ID) {
			continue
		}
		selected = append(selected, rec.ID)
		selectedSet[rec.ID] = struct{}{}
	}
	for _, id := range offlineFallback {
		if len(selected) >= maxActive {
			break
		}
		if _, exists := selectedSet[id]; exists {
			continue
		}
		selected = append(selected, id)
		selectedSet[id] = struct{}{}
	}

	if len(selected) < minActiveValidatorsFloor() {
		return current
	}
	return canonicalValidatorIDs(selected)
}

func (n *Node) epochBoundaryValidatorSetForBlock(block Block) ([]string, map[string]ValidatorRecord, bool) {
	if n == nil || block.ID == 0 {
		return nil, nil, false
	}
	nextHeight := block.ID + 1
	if !validatorEpochSetV1EnabledAt(nextHeight) || !isValidatorEpochBoundary(nextHeight) {
		return nil, nil, false
	}
	current := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	if len(current) == 0 {
		current = canonicalValidatorIDs(block.Signatures)
	}
	if len(current) == 0 {
		return nil, nil, false
	}
	snapshot := n.validatorRegistrySnapshotForHeight(block.ID)
	if len(snapshot) == 0 {
		snapshot = n.validatorRegistrySnapshotForHeight(nextHeight)
	}
	selected := selectEpochActiveValidatorSet(nextHeight, current, snapshot)
	if len(selected) == 0 {
		return nil, nil, false
	}
	return selected, snapshot, true
}

func (n *Node) epochBoundaryValidatorSetForHeight(height uint64) ([]string, map[string]ValidatorRecord, bool) {
	if n == nil || !validatorEpochSetV1EnabledAt(height) || !isValidatorEpochBoundary(height) || height <= 1 {
		return nil, nil, false
	}
	current := n.freezeValidatorSetForHeight(height-1, n.GetConsensusValidators(int(height-1)))
	if len(current) == 0 {
		return nil, nil, false
	}
	snapshot := n.validatorRegistrySnapshotForHeight(height - 1)
	if len(snapshot) == 0 {
		return nil, nil, false
	}
	selected := selectEpochActiveValidatorSet(height, current, snapshot)
	if len(selected) == 0 {
		return nil, nil, false
	}
	return selected, snapshot, true
}
