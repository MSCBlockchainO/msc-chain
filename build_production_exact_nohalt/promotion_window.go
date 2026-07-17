package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const promotionWindowRecordVersionV1 = "promotion_window_record_v1"

type PromotionWindowRecord struct {
	Version                     string   `json:"version"`
	Window                      uint64   `json:"window"`
	StartHeight                 uint64   `json:"start_height"`
	EndHeight                   uint64   `json:"end_height"`
	EpochBucketStart            uint64   `json:"epoch_bucket_start"`
	PerformanceValidators       []string `json:"performance_validators"`
	SelectionHash               string   `json:"selection_hash"`
	ScoreConfigHash             string   `json:"score_config_hash"`
	SourceValidatorRegistryHash string   `json:"source_validator_registry_hash"`
	SeedAnchor                  string   `json:"seed_anchor,omitempty"`
	CreatedAtBlock              uint64   `json:"created_at_block"`
}

type PromotionWindowReplacementRecord struct {
	Version                     string `json:"version"`
	Window                      uint64 `json:"window"`
	Height                      uint64 `json:"height"`
	RemovedValidator            string `json:"removed_validator"`
	ReplacementValidator        string `json:"replacement_validator"`
	Reason                      string `json:"reason"`
	SourceValidatorRegistryHash string `json:"source_validator_registry_hash"`
	RecordHash                  string `json:"record_hash"`
}

type promotionWindowStoredState struct {
	Record       PromotionWindowRecord              `json:"record"`
	Replacements []PromotionWindowReplacementRecord `json:"replacements,omitempty"`
	Hash         string                             `json:"hash"`
}

func promotionWindowScoreConfigHash() string {
	return HashStrings([]string{
		"promotion_window_score_config_v1",
		strings.TrimSpace(ChainID),
		normalizeActiveSetMode(ValidatorActiveSetMode),
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

func promotionWindowBlockSpan() uint64 {
	span := validatorHybridEpochBlocks() * validatorHybridPromotionWindowEpochs()
	if span == 0 {
		return 100_000
	}
	return span
}

func promotionWindowBounds(height uint64) (uint64, uint64, uint64, uint64) {
	window := validatorHybridPromotionWindowBucket(height)
	span := promotionWindowBlockSpan()
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
	end := (window+1)*span - 1
	epochStart := uint64(0)
	if epochBlocks := validatorHybridEpochBlocks(); epochBlocks > 0 {
		epochStart = start / epochBlocks
	}
	return window, start, end, epochStart
}

func promotionWindowCanCreateAtHeight(height uint64) bool {
	if !promotionWindowRecordV1EnabledAt(height) {
		return false
	}
	_, start, _, _ := promotionWindowBounds(height)
	return height == start || height == PromotionWindowRecordV1Height
}

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

func PromotionWindowRecordHash(rec PromotionWindowRecord) string {
	rec = normalizePromotionWindowRecord(rec)
	if rec.Window == 0 && len(rec.PerformanceValidators) == 0 {
		return ""
	}
	return HashStrings([]string{
		rec.Version,
		strings.TrimSpace(ChainID),
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

func PromotionWindowReplacementHash(rec PromotionWindowReplacementRecord) string {
	rec = normalizePromotionWindowReplacement(rec)
	if rec.Window == 0 || rec.RemovedValidator == "" || rec.ReplacementValidator == "" {
		return ""
	}
	return HashStrings([]string{
		rec.Version,
		strings.TrimSpace(ChainID),
		strconv.FormatUint(rec.Window, 10),
		strconv.FormatUint(rec.Height, 10),
		rec.RemovedValidator,
		rec.ReplacementValidator,
		rec.Reason,
		rec.SourceValidatorRegistryHash,
	})
}

func PromotionWindowStateHash(record *PromotionWindowRecord, replacements []PromotionWindowReplacementRecord) string {
	if record == nil {
		return ""
	}
	recHash := PromotionWindowRecordHash(*record)
	if recHash == "" {
		return ""
	}
	replacementHashes := make([]string, 0, len(replacements))
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
		strings.TrimSpace(ChainID),
		recHash,
		strings.Join(replacementHashes, ","),
	})
}

func buildPromotionWindowRecord(height uint64, eligible []ValidatorPoolEntry, sourceRegistryHash string, seedAnchor string) *PromotionWindowRecord {
	if len(eligible) <= validatorHybridMaxActiveValidators() {
		return nil
	}
	window, start, end, epochStart := promotionWindowBounds(height)
	performanceSlots := validatorHybridPerformanceSlots()
	validators := make([]string, 0, performanceSlots)
	for _, entry := range eligible {
		if !entry.PerformanceEligible {
			continue
		}
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
	scoreConfigHash := promotionWindowScoreConfigHash()
	selectionHash := HashStrings([]string{
		"promotion_window_selection_v1",
		strings.TrimSpace(ChainID),
		strconv.FormatUint(window, 10),
		strconv.FormatUint(start, 10),
		strconv.FormatUint(end, 10),
		strings.Join(validators, ","),
		strings.TrimSpace(sourceRegistryHash),
		scoreConfigHash,
		strings.TrimSpace(seedAnchor),
	})
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

func promotionWindowRecordAppliesAtHeight(record *PromotionWindowRecord, height uint64) bool {
	if record == nil || !promotionWindowRecordV1EnabledAt(height) {
		return false
	}
	rec := normalizePromotionWindowRecord(*record)
	return rec.Window == validatorHybridPromotionWindowBucket(height) &&
		height >= rec.StartHeight &&
		(rec.EndHeight == 0 || height <= rec.EndHeight) &&
		len(rec.PerformanceValidators) > 0
}

func promotionWindowRecordKey(window uint64) []byte {
	return []byte(fmt.Sprintf("promotion_window_record:%d", window))
}

func (n *Node) storePromotionWindowState(record PromotionWindowRecord, replacements []PromotionWindowReplacementRecord) error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	record = normalizePromotionWindowRecord(record)
	if len(record.PerformanceValidators) == 0 {
		return nil
	}
	cleanReplacements := make([]PromotionWindowReplacementRecord, 0, len(replacements))
	for _, replacement := range replacements {
		replacement = normalizePromotionWindowReplacement(replacement)
		if replacement.RecordHash == "" {
			replacement.RecordHash = PromotionWindowReplacementHash(replacement)
		}
		if replacement.RecordHash != "" {
			cleanReplacements = append(cleanReplacements, replacement)
		}
	}
	state := promotionWindowStoredState{
		Record:       record,
		Replacements: cleanReplacements,
	}
	state.Hash = PromotionWindowStateHash(&state.Record, state.Replacements)
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return n.DB.State.Update(func(txn *Txn) error {
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(promotionWindowRecordKey(record.Window), enc)
	})
}

func (n *Node) loadPromotionWindowState(window uint64) (*PromotionWindowRecord, []PromotionWindowReplacementRecord, string, error) {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil, nil, "", ErrKeyNotFound
	}
	var state promotionWindowStoredState
	err := n.DB.State.View(func(txn *Txn) error {
		item, err := txn.Get(promotionWindowRecordKey(window))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
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
	for i := range state.Replacements {
		state.Replacements[i] = normalizePromotionWindowReplacement(state.Replacements[i])
		if state.Replacements[i].RecordHash == "" {
			state.Replacements[i].RecordHash = PromotionWindowReplacementHash(state.Replacements[i])
		}
	}
	got := PromotionWindowStateHash(&state.Record, state.Replacements)
	if state.Hash != "" && !strings.EqualFold(strings.TrimSpace(state.Hash), got) {
		return nil, nil, "", errors.New("promotion_window_hash_mismatch")
	}
	return &state.Record, state.Replacements, got, nil
}

func copyPromotionWindowRecords(src map[uint64]PromotionWindowRecord) map[uint64]PromotionWindowRecord {
	if len(src) == 0 {
		return nil
	}
	out := make(map[uint64]PromotionWindowRecord, len(src))
	for window, record := range src {
		out[window] = normalizePromotionWindowRecord(record)
	}
	return out
}

func copyPromotionWindowReplacements(src map[uint64][]PromotionWindowReplacementRecord) map[uint64][]PromotionWindowReplacementRecord {
	if len(src) == 0 {
		return nil
	}
	out := make(map[uint64][]PromotionWindowReplacementRecord, len(src))
	for window, replacements := range src {
		copied := make([]PromotionWindowReplacementRecord, 0, len(replacements))
		for _, replacement := range replacements {
			copied = append(copied, normalizePromotionWindowReplacement(replacement))
		}
		out[window] = copied
	}
	return out
}

func (n *Node) attachPromotionWindowStateToSnapshot(snapshot *StateSnapshot) {
	if n == nil || snapshot == nil || !promotionWindowRecordV1EnabledAt(snapshot.Height) {
		return
	}
	window := validatorHybridPromotionWindowBucket(snapshot.Height)
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
		copied := make([]PromotionWindowReplacementRecord, 0, len(replacements))
		for _, replacement := range replacements {
			copied = append(copied, normalizePromotionWindowReplacement(replacement))
		}
		snapshot.PromotionWindowReplacements[window] = copied
	}
	snapshot.PromotionWindowHash = strings.TrimSpace(hash)
}

func promotionWindowEffectivePerformanceIDs(record *PromotionWindowRecord, replacements []PromotionWindowReplacementRecord) []string {
	if record == nil {
		return nil
	}
	ids := append([]string{}, normalizePromotionWindowRecord(*record).PerformanceValidators...)
	if len(replacements) == 0 {
		return ids
	}
	replaceByRemoved := make(map[string]string, len(replacements))
	for _, replacement := range replacements {
		replacement = normalizePromotionWindowReplacement(replacement)
		if replacement.RemovedValidator != "" && replacement.ReplacementValidator != "" {
			replaceByRemoved[replacement.RemovedValidator] = replacement.ReplacementValidator
		}
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if repl := replaceByRemoved[id]; repl != "" {
			id = repl
		}
		id = normalizeValidatorID(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (n *Node) promotionWindowStateForHeight(height uint64, snapshot map[string]ValidatorRecord, seedAnchor string, eligible []ValidatorPoolEntry) (*PromotionWindowRecord, []PromotionWindowReplacementRecord, string, string) {
	if n == nil || !activeSetModeHybridScoreRotation() || !promotionWindowRecordV1EnabledAt(height) || len(eligible) <= validatorHybridMaxActiveValidators() {
		return nil, nil, "", "disabled"
	}
	window := validatorHybridPromotionWindowBucket(height)
	if record, replacements, hash, err := n.loadPromotionWindowState(window); err == nil && promotionWindowRecordAppliesAtHeight(record, height) {
		return record, replacements, hash, "committed_state"
	}
	if !promotionWindowCanCreateAtHeight(height) {
		return nil, nil, "", "missing_committed_record"
	}
	sourceHash := ValidatorRegistrySnapshotHash(snapshot)
	record := buildPromotionWindowRecord(height, eligible, sourceHash, seedAnchor)
	if record == nil {
		return nil, nil, "", "not_required"
	}
	hash := PromotionWindowStateHash(record, nil)
	_ = n.storePromotionWindowState(*record, nil)
	return record, nil, hash, "created_boundary"
}

func (n *Node) expectedPromotionWindowHashWithSource(height uint64) (string, string) {
	if n == nil || !promotionWindowRecordV1EnabledAt(height) || !activeSetModeHybridScoreRotation() {
		return "", "disabled"
	}
	snapshot := n.validatorRegistrySnapshotForHeight(height)
	if len(snapshot) == 0 {
		return "", "registry_unavailable"
	}
	anchor := ""
	if height > 1 {
		anchor = strings.TrimSpace(n.expectedValidatorSetHash(height - 1))
	}
	eligible := validatorHybridEligibleEntries(height, snapshot)
	if len(eligible) <= validatorHybridMaxActiveValidators() {
		return "", "not_required"
	}
	_, _, hash, source := n.promotionWindowStateForHeight(height, snapshot, anchor, eligible)
	return strings.TrimSpace(hash), source
}

func (n *Node) currentPromotionWindowStatus(height uint64) map[string]any {
	out := map[string]any{
		"enabled":           promotionWindowRecordV1EnabledAt(height),
		"activation_height": PromotionWindowRecordV1Height,
		"window":            validatorHybridPromotionWindowBucket(height),
	}
	if n == nil || height == 0 {
		return out
	}
	window := validatorHybridPromotionWindowBucket(height)
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

func blockPromotionWindowHashData(block Block) string {
	if !promotionWindowRecordV1EnabledAt(block.ID) {
		return ""
	}
	hash := strings.TrimSpace(block.PromotionWindowHash)
	if hash == "" {
		return ""
	}
	return "|pwh=" + hash
}
