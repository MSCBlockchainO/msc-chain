package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// `promotionWindowRecordVersionV1` defines the constant value used by this package.
const promotionWindowRecordVersionV1 = "promotion_window_record_v1"

type PromotionWindowRecord struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Window` stores the value associated with this record.
	Window uint64 `json:"window"`
	// `StartHeight` stores the value associated with this record.
	StartHeight uint64 `json:"start_height"`
	// `EndHeight` stores the value associated with this record.
	EndHeight uint64 `json:"end_height"`
	// `EpochBucketStart` stores the value associated with this record.
	EpochBucketStart uint64 `json:"epoch_bucket_start"`
	// `PerformanceValidators` stores the value associated with this record.
	PerformanceValidators []string `json:"performance_validators"`
	// `SelectionHash` stores the digest used to identify or verify the related data.
	SelectionHash string `json:"selection_hash"`
	// `ScoreConfigHash` stores the digest used to identify or verify the related data.
	ScoreConfigHash string `json:"score_config_hash"`
	// `SourceValidatorRegistryHash` stores the digest used to identify or verify the related data.
	SourceValidatorRegistryHash string `json:"source_validator_registry_hash"`
	// `SeedAnchor` stores the value associated with this record.
	SeedAnchor string `json:"seed_anchor,omitempty"`
	// `CreatedAtBlock` stores the synchronization state protecting shared data.
	CreatedAtBlock uint64 `json:"created_at_block"`
}

type PromotionWindowReplacementRecord struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Window` stores the value associated with this record.
	Window uint64 `json:"window"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `RemovedValidator` stores the value associated with this record.
	RemovedValidator string `json:"removed_validator"`
	// `ReplacementValidator` stores the value associated with this record.
	ReplacementValidator string `json:"replacement_validator"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason"`
	// `SourceValidatorRegistryHash` stores the digest used to identify or verify the related data.
	SourceValidatorRegistryHash string `json:"source_validator_registry_hash"`
	// `RecordHash` stores the digest used to identify or verify the related data.
	RecordHash string `json:"record_hash"`
}

type promotionWindowStoredState struct {
	// `Record` stores the value associated with this record.
	Record PromotionWindowRecord `json:"record"`
	// `Replacements` stores the value associated with this record.
	Replacements []PromotionWindowReplacementRecord `json:"replacements,omitempty"`
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string `json:"hash"`
}

// promotionWindowScoreConfigHash implements the promotion window score config hash helper.
func promotionWindowScoreConfigHash() string {
	return HashStrings([]string{
		"promotion_window_score_config_v1",
		protocolChainID(),
		normalizeActiveSetMode(protocolValidatorActiveSetModeValue()),
		strconv.Itoa(validatorHybridMaxActiveValidators()),
		strconv.Itoa(validatorHybridPerformanceSlots()),
		strconv.Itoa(validatorHybridRotationSlots()),
		strconv.FormatInt(validatorHybridEffectiveStakeCap(), 10),
		strconv.FormatUint(validatorHybridEpochBlocks(), 10),
		strconv.Itoa(ValidatorHybridStakeWeight),
		strconv.Itoa(ValidatorHybridUptimeWeight),
		strconv.Itoa(ValidatorHybridPerformanceWeight),
		strconv.Itoa(ValidatorHybridDecentralizationWeight),
		strconv.Itoa(validatorHybridPerformanceMinSignedBPS()),
		strconv.FormatUint(validatorHybridPromotionWindowEpochs(), 10),
		strconv.FormatUint(validatorHybridMinimumPerformanceAgeEpochs(), 10),
		strconv.Itoa(ValidatorHybridMinimumOnlineWhenFull),
		strconv.Itoa(ValidatorHybridDiversityASNWeight),
		strconv.Itoa(ValidatorHybridDiversityRegionWeight),
		strconv.Itoa(ValidatorHybridDiversityProviderWeight),
		strconv.Itoa(ValidatorHybridDiversityHomePCWeight),
		ValidatorDiversityMetadataHash(),
	})
}

// promotionWindowBlockSpan implements the promotion window block span helper.
func promotionWindowBlockSpan() uint64 {
	// `span` stores the value produced by this operation.
	span := validatorHybridEpochBlocks() * validatorHybridPromotionWindowEpochs()
	if span == 0 {
		return 100_000
	}
	return span
}

// promotionWindowBounds implements the promotion window bounds helper.
func promotionWindowBounds(height uint64) (uint64, uint64, uint64, uint64) {
	// `window` stores the value produced by this operation.
	window := validatorHybridPromotionWindowBucket(height)
	// `span` stores the value produced by this operation.
	span := promotionWindowBlockSpan()
	// `start` stores the value produced by this operation.
	start := window * span
	if start == 0 {
		start = 1
	}
	if promotionWindowRecordV1EnabledAt(height) &&
		PromotionWindowRecordV1Height > start &&
		PromotionWindowRecordV1Height <= height &&
		validatorHybridPromotionWindowBucket(PromotionWindowRecordV1Height) == window {
		start = PromotionWindowRecordV1Height
	}
	// `end` stores the value produced by this operation.
	end := (window+1)*span - 1
	// `epochStart` stores the value produced by this operation.
	epochStart := uint64(0)
	// `epochBlocks` stores the value produced by this operation.
	if epochBlocks := validatorHybridEpochBlocks(); epochBlocks > 0 {
		epochStart = start / epochBlocks
	}
	return window, start, end, epochStart
}

// promotionWindowCanCreateAtHeight implements the promotion window can create at height helper.
func promotionWindowCanCreateAtHeight(height uint64) bool {
	if !promotionWindowRecordV1EnabledAt(height) {
		return false
	}
	// `start` stores the value produced by this operation.
	_, start, _, _ := promotionWindowBounds(height)
	return height == start || height == PromotionWindowRecordV1Height
}

// normalizePromotionWindowRecord normalizes promotion window record.
func normalizePromotionWindowRecord(rec PromotionWindowRecord) PromotionWindowRecord {
	rec.Version = strings.TrimSpace(rec.Version)
	if rec.Version == "" {
		rec.Version = promotionWindowRecordVersionV1
	}
	rec.SelectionHash = strings.TrimSpace(rec.SelectionHash)
	rec.ScoreConfigHash = strings.TrimSpace(rec.ScoreConfigHash)
	rec.SourceValidatorRegistryHash = strings.TrimSpace(rec.SourceValidatorRegistryHash)
	rec.SeedAnchor = strings.TrimSpace(rec.SeedAnchor)
	rec.PerformanceValidators = canonicalValidatorIDs(rec.PerformanceValidators)
	return rec
}

// normalizePromotionWindowReplacement normalizes promotion window replacement.
func normalizePromotionWindowReplacement(rec PromotionWindowReplacementRecord) PromotionWindowReplacementRecord {
	rec.Version = strings.TrimSpace(rec.Version)
	if rec.Version == "" {
		rec.Version = promotionWindowRecordVersionV1
	}
	rec.RemovedValidator = normalizeValidatorID(rec.RemovedValidator)
	rec.ReplacementValidator = normalizeValidatorID(rec.ReplacementValidator)
	rec.Reason = strings.TrimSpace(rec.Reason)
	rec.SourceValidatorRegistryHash = strings.TrimSpace(rec.SourceValidatorRegistryHash)
	rec.RecordHash = strings.TrimSpace(rec.RecordHash)
	return rec
}

// PromotionWindowRecordHash implements the promotion window record hash helper.
func PromotionWindowRecordHash(rec PromotionWindowRecord) string {
	rec = normalizePromotionWindowRecord(rec)
	if rec.Window == 0 && len(rec.PerformanceValidators) == 0 {
		return ""
	}
	return HashStrings([]string{
		rec.Version,
		protocolChainID(),
		strconv.FormatUint(rec.Window, 10),
		strconv.FormatUint(rec.StartHeight, 10),
		strconv.FormatUint(rec.EndHeight, 10),
		strconv.FormatUint(rec.EpochBucketStart, 10),
		strings.Join(rec.PerformanceValidators, ","),
		rec.SelectionHash,
		rec.ScoreConfigHash,
		rec.SourceValidatorRegistryHash,
		rec.SeedAnchor,
		strconv.FormatUint(rec.CreatedAtBlock, 10),
	})
}

// PromotionWindowReplacementHash implements the promotion window replacement hash helper.
func PromotionWindowReplacementHash(rec PromotionWindowReplacementRecord) string {
	rec = normalizePromotionWindowReplacement(rec)
	if rec.Window == 0 || rec.RemovedValidator == "" || rec.ReplacementValidator == "" {
		return ""
	}
	return HashStrings([]string{
		rec.Version,
		protocolChainID(),
		strconv.FormatUint(rec.Window, 10),
		strconv.FormatUint(rec.Height, 10),
		rec.RemovedValidator,
		rec.ReplacementValidator,
		rec.Reason,
		rec.SourceValidatorRegistryHash,
	})
}

// PromotionWindowStateHash implements the promotion window state hash helper.
func PromotionWindowStateHash(record *PromotionWindowRecord, replacements []PromotionWindowReplacementRecord) string {
	if record == nil {
		return ""
	}
	// `recHash` stores the digest used to identify or verify the related data.
	recHash := PromotionWindowRecordHash(*record)
	if recHash == "" {
		return ""
	}
	// `replacementHashes` stores the value produced by this operation.
	replacementHashes := make([]string, 0, len(replacements))
	// `replacement` tracks the current values while iterating.
	for _, replacement := range replacements {
		replacement = normalizePromotionWindowReplacement(replacement)
		if replacement.RecordHash == "" {
			replacement.RecordHash = PromotionWindowReplacementHash(replacement)
		}
		if replacement.RecordHash != "" {
			replacementHashes = append(replacementHashes, replacement.RecordHash)
		}
	}
	sort.Strings(replacementHashes)
	return HashStrings([]string{
		"promotion_window_state_v1",
		protocolChainID(),
		recHash,
		strings.Join(replacementHashes, ","),
	})
}

// buildPromotionWindowRecord builds promotion window record.
func buildPromotionWindowRecord(height uint64, eligible []ValidatorPoolEntry, sourceRegistryHash string, seedAnchor string) *PromotionWindowRecord {
	if len(eligible) <= validatorHybridMaxActiveValidators() {
		return nil
	}
	// `window`, `start`, `end`, and `epochStart` store the value produced by this operation.
	window, start, end, epochStart := promotionWindowBounds(height)
	// `performanceSlots` stores the value produced by this operation.
	performanceSlots := validatorHybridPerformanceSlots()
	// `validators` stores whether the related condition is satisfied.
	validators := make([]string, 0, performanceSlots)
	// `entry` tracks the current values while iterating.
	for _, entry := range eligible {
		if !entry.PerformanceEligible {
			continue
		}
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(entry.ID)
		if id == "" {
			continue
		}
		validators = append(validators, id)
		if len(validators) >= performanceSlots {
			break
		}
	}
	if len(validators) == 0 {
		return nil
	}
	// `scoreConfigHash` stores the digest used to identify or verify the related data.
	scoreConfigHash := promotionWindowScoreConfigHash()
	// `selectionHash` stores the digest used to identify or verify the related data.
	selectionHash := HashStrings([]string{
		"promotion_window_selection_v1",
		protocolChainID(),
		strconv.FormatUint(window, 10),
		strconv.FormatUint(start, 10),
		strconv.FormatUint(end, 10),
		strings.Join(validators, ","),
		strings.TrimSpace(sourceRegistryHash),
		scoreConfigHash,
		strings.TrimSpace(seedAnchor),
	})
	// `rec` stores the value produced by this operation.
	rec := PromotionWindowRecord{
		Version:                     promotionWindowRecordVersionV1,
		Window:                      window,
		StartHeight:                 start,
		EndHeight:                   end,
		EpochBucketStart:            epochStart,
		PerformanceValidators:       validators,
		SelectionHash:               selectionHash,
		ScoreConfigHash:             scoreConfigHash,
		SourceValidatorRegistryHash: strings.TrimSpace(sourceRegistryHash),
		SeedAnchor:                  strings.TrimSpace(seedAnchor),
		CreatedAtBlock:              height,
	}
	rec = normalizePromotionWindowRecord(rec)
	return &rec
}

// promotionWindowRecordAppliesAtHeight implements the promotion window record applies at height helper.
func promotionWindowRecordAppliesAtHeight(record *PromotionWindowRecord, height uint64) bool {
	if record == nil || !promotionWindowRecordV1EnabledAt(height) {
		return false
	}
	// `rec` stores the value produced by this operation.
	rec := normalizePromotionWindowRecord(*record)
	return rec.Window == validatorHybridPromotionWindowBucket(height) &&
		height >= rec.StartHeight &&
		(rec.EndHeight == 0 || height <= rec.EndHeight) &&
		len(rec.PerformanceValidators) > 0
}

// promotionWindowRecordKey implements the promotion window record key helper.
func promotionWindowRecordKey(window uint64) []byte {
	return []byte(fmt.Sprintf("promotion_window_record:%d", window))
}

// storePromotionWindowState implements the store promotion window state helper.
func (n *Node) storePromotionWindowState(record PromotionWindowRecord, replacements []PromotionWindowReplacementRecord) error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	record = normalizePromotionWindowRecord(record)
	if len(record.PerformanceValidators) == 0 {
		return nil
	}
	// `cleanReplacements` stores the value produced by this operation.
	cleanReplacements := make([]PromotionWindowReplacementRecord, 0, len(replacements))
	// `replacement` tracks the current values while iterating.
	for _, replacement := range replacements {
		replacement = normalizePromotionWindowReplacement(replacement)
		if replacement.RecordHash == "" {
			replacement.RecordHash = PromotionWindowReplacementHash(replacement)
		}
		if replacement.RecordHash != "" {
			cleanReplacements = append(cleanReplacements, replacement)
		}
	}
	// `state` stores the value produced by this operation.
	state := promotionWindowStoredState{
		Record:       record,
		Replacements: cleanReplacements,
	}
	state.Hash = PromotionWindowStateHash(&state.Record, state.Replacements)
	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return n.DB.State.Update(func(txn *Txn) error {
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(promotionWindowRecordKey(record.Window), enc)
	})
}

// loadPromotionWindowState implements the load promotion window state helper.
func (n *Node) loadPromotionWindowState(window uint64) (*PromotionWindowRecord, []PromotionWindowReplacementRecord, string, error) {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil, nil, "", ErrKeyNotFound
	}
	// `state` stores the value used by this operation.
	var state promotionWindowStoredState
	// `err` stores the error produced by this operation.
	err := n.DB.State.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(promotionWindowRecordKey(window))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			// `dec` and `err` store the error produced by this operation.
			dec, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			return json.Unmarshal(dec, &state)
		})
	})
	if err != nil {
		return nil, nil, "", err
	}
	state.Record = normalizePromotionWindowRecord(state.Record)
	// `i` tracks the current position in the related collection.
	for i := range state.Replacements {
		state.Replacements[i] = normalizePromotionWindowReplacement(state.Replacements[i])
		if state.Replacements[i].RecordHash == "" {
			state.Replacements[i].RecordHash = PromotionWindowReplacementHash(state.Replacements[i])
		}
	}
	// `got` stores the value produced by this operation.
	got := PromotionWindowStateHash(&state.Record, state.Replacements)
	if state.Hash != "" && !strings.EqualFold(strings.TrimSpace(state.Hash), got) {
		return nil, nil, "", errors.New("promotion_window_hash_mismatch")
	}
	return &state.Record, state.Replacements, got, nil
}

// copyPromotionWindowRecords copies promotion window records.
func copyPromotionWindowRecords(src map[uint64]PromotionWindowRecord) map[uint64]PromotionWindowRecord {
	if len(src) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]PromotionWindowRecord, len(src))
	// `window` and `record` track the current values while iterating.
	for window, record := range src {
		out[window] = normalizePromotionWindowRecord(record)
	}
	return out
}

// copyPromotionWindowReplacements copies promotion window replacements.
func copyPromotionWindowReplacements(src map[uint64][]PromotionWindowReplacementRecord) map[uint64][]PromotionWindowReplacementRecord {
	if len(src) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64][]PromotionWindowReplacementRecord, len(src))
	// `window` and `replacements` track the current values while iterating.
	for window, replacements := range src {
		// `copied` stores the value produced by this operation.
		copied := make([]PromotionWindowReplacementRecord, 0, len(replacements))
		// `replacement` tracks the current values while iterating.
		for _, replacement := range replacements {
			copied = append(copied, normalizePromotionWindowReplacement(replacement))
		}
		out[window] = copied
	}
	return out
}

// attachPromotionWindowStateToSnapshot implements the attach promotion window state to snapshot helper.
func (n *Node) attachPromotionWindowStateToSnapshot(snapshot *StateSnapshot) {
	if n == nil || snapshot == nil || !promotionWindowRecordV1EnabledAt(snapshot.Height) {
		return
	}
	// `window` stores the value produced by this operation.
	window := validatorHybridPromotionWindowBucket(snapshot.Height)
	// `record`, `replacements`, `hash`, and `err` store the error produced by this operation.
	record, replacements, hash, err := n.loadPromotionWindowState(window)
	if err != nil || record == nil || !promotionWindowRecordAppliesAtHeight(record, snapshot.Height) {
		return
	}
	if snapshot.PromotionWindowRecords == nil {
		snapshot.PromotionWindowRecords = make(map[uint64]PromotionWindowRecord, 1)
	}
	if snapshot.PromotionWindowReplacements == nil && len(replacements) > 0 {
		snapshot.PromotionWindowReplacements = make(map[uint64][]PromotionWindowReplacementRecord, 1)
	}
	snapshot.PromotionWindowRecords[window] = normalizePromotionWindowRecord(*record)
	if len(replacements) > 0 {
		// `copied` stores the value produced by this operation.
		copied := make([]PromotionWindowReplacementRecord, 0, len(replacements))
		// `replacement` tracks the current values while iterating.
		for _, replacement := range replacements {
			copied = append(copied, normalizePromotionWindowReplacement(replacement))
		}
		snapshot.PromotionWindowReplacements[window] = copied
	}
	snapshot.PromotionWindowHash = strings.TrimSpace(hash)
}

// promotionWindowEffectivePerformanceIDs implements the promotion window effective performance i ds helper.
func promotionWindowEffectivePerformanceIDs(record *PromotionWindowRecord, replacements []PromotionWindowReplacementRecord) []string {
	if record == nil {
		return nil
	}
	// `ids` stores the current position in the related collection.
	ids := append([]string{}, normalizePromotionWindowRecord(*record).PerformanceValidators...)
	if len(replacements) == 0 {
		return ids
	}
	// `replaceByRemoved` stores the value produced by this operation.
	replaceByRemoved := make(map[string]string, len(replacements))
	// `replacement` tracks the current values while iterating.
	for _, replacement := range replacements {
		replacement = normalizePromotionWindowReplacement(replacement)
		if replacement.RemovedValidator != "" && replacement.ReplacementValidator != "" {
			replaceByRemoved[replacement.RemovedValidator] = replacement.ReplacementValidator
		}
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(ids))
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(ids))
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		// `repl` stores the value produced by this operation.
		if repl := replaceByRemoved[id]; repl != "" {
			id = repl
		}
		id = normalizeValidatorID(id)
		if id == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// promotionWindowStateForHeight implements the promotion window state for height helper.
func (n *Node) promotionWindowStateForHeight(height uint64, snapshot map[string]ValidatorRecord, seedAnchor string, eligible []ValidatorPoolEntry) (*PromotionWindowRecord, []PromotionWindowReplacementRecord, string, string) {
	if n == nil || !promotionWindowRecordV1EnabledAt(height) || len(eligible) <= validatorHybridMaxActiveValidators() {
		return nil, nil, "", "disabled"
	}
	// `window` stores the value produced by this operation.
	window := validatorHybridPromotionWindowBucket(height)
	// `record`, `replacements`, `hash`, and `err` store the error produced by this operation.
	if record, replacements, hash, err := n.loadPromotionWindowState(window); err == nil && promotionWindowRecordAppliesAtHeight(record, height) {
		return record, replacements, hash, "committed_state"
	}
	if !promotionWindowCanCreateAtHeight(height) {
		return nil, nil, "", "missing_committed_record"
	}
	// `sourceHash` stores the digest used to identify or verify the related data.
	sourceHash := ValidatorRegistrySnapshotHash(snapshot)
	// `record` stores the value produced by this operation.
	record := buildPromotionWindowRecord(height, eligible, sourceHash, seedAnchor)
	if record == nil {
		return nil, nil, "", "not_required"
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := PromotionWindowStateHash(record, nil)
	_ = n.storePromotionWindowState(*record, nil)
	return record, nil, hash, "created_boundary"
}

// expectedPromotionWindowHashWithSource implements the expected promotion window hash with source helper.
func (n *Node) expectedPromotionWindowHashWithSource(height uint64) (string, string) {
	if n == nil || !promotionWindowRecordV1EnabledAt(height) || !activeSetModeHybridScoreRotation() {
		return "", "disabled"
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := n.validatorRegistrySnapshotForHeight(height)
	if len(snapshot) == 0 {
		return "", "registry_unavailable"
	}
	// `anchor` stores the value produced by this operation.
	anchor := ""
	if height > 1 {
		anchor = strings.TrimSpace(n.expectedValidatorSetHash(height - 1))
	}
	// `eligible` stores the value produced by this operation.
	eligible := validatorHybridEligibleEntries(height, snapshot)
	if len(eligible) <= validatorHybridMaxActiveValidators() {
		return "", "not_required"
	}
	// `hash` and `source` store the digest used to identify or verify the related data.
	_, _, hash, source := n.promotionWindowStateForHeight(height, snapshot, anchor, eligible)
	return strings.TrimSpace(hash), source
}

// currentPromotionWindowStatus returns current promotion window status.
func (n *Node) currentPromotionWindowStatus(height uint64) map[string]any {
	// `out` stores the result produced by this operation.
	out := map[string]any{
		"enabled":           promotionWindowRecordV1EnabledAt(height),
		"activation_height": PromotionWindowRecordV1Height,
		"window":            validatorHybridPromotionWindowBucket(height),
	}
	if n == nil || height == 0 {
		return out
	}
	// `window` stores the value produced by this operation.
	window := validatorHybridPromotionWindowBucket(height)
	// `record`, `replacements`, `stateHash`, and `err` store the error produced by this operation.
	record, replacements, stateHash, err := n.loadPromotionWindowState(window)
	if err != nil || record == nil {
		out["available"] = false
		out["hash"] = ""
		out["source"] = "not_found"
		out["error"] = strings.TrimSpace(fmt.Sprint(err))
		return out
	}
	out["available"] = true
	out["hash"] = stateHash
	out["source"] = "committed_state"
	out["record_hash"] = PromotionWindowRecordHash(*record)
	out["state_hash"] = stateHash
	out["start_height"] = record.StartHeight
	out["end_height"] = record.EndHeight
	out["performance_validators"] = append([]string{}, record.PerformanceValidators...)
	out["replacement_count"] = len(replacements)
	out["frozen"] = promotionWindowRecordAppliesAtHeight(record, height)
	return out
}

// blockPromotionWindowHashData implements the block promotion window hash data helper.
func blockPromotionWindowHashData(block Block) string {
	if !promotionWindowRecordV1EnabledAt(block.ID) {
		return ""
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := strings.TrimSpace(block.PromotionWindowHash)
	if hash == "" {
		return ""
	}
	return "|pwh=" + hash
}
