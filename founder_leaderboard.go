package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

var (
	// `FounderValidatorBadgeEnabled` stores whether the related condition is satisfied.
	FounderValidatorBadgeEnabled  = false
	// `FounderValidatorCutoffHeight` stores whether the related condition is satisfied.
	FounderValidatorCutoffHeight  uint64
	// `FounderValidatorMaxCount` stores whether the related condition is satisfied.
	FounderValidatorMaxCount      int
	// `FounderValidatorMinSignedBPS` stores whether the related condition is satisfied.
	FounderValidatorMinSignedBPS  = 9500
	// `FounderValidatorNFTCollection` stores whether the related condition is satisfied.
	FounderValidatorNFTCollection string
)

type founderValidatorBadge struct {
	// `Eligible` stores the value associated with this record.
	Eligible      bool   `json:"eligible"`
	// `Badge` stores the value associated with this record.
	Badge         bool   `json:"badge"`
	// `Reason` stores the value associated with this record.
	Reason        string `json:"reason,omitempty"`
	// `NFTCollection` stores the value associated with this record.
	NFTCollection string `json:"nft_collection,omitempty"`
	// `NFTTokenID` stores the value associated with this record.
	NFTTokenID    uint64 `json:"nft_token_id,omitempty"`
	// `NFTVerified` stores the value associated with this record.
	NFTVerified   bool   `json:"nft_verified"`
}

type validatorsLeaderboardEntry struct {
	// `Rank` stores the value associated with this record.
	Rank                        int                    `json:"rank"`
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID                 string                 `json:"validator_id"`
	// `Status` stores the value associated with this record.
	Status                      string                 `json:"status"`
	// `SlotType` stores the value associated with this record.
	SlotType                    string                 `json:"slot_type"`
	// `Active` stores the value associated with this record.
	Active                      bool                   `json:"active"`
	// `Online` stores the value associated with this record.
	Online                      bool                   `json:"online"`
	// `CMD` stores the value associated with this record.
	CMD                         string                 `json:"cmd"`
	// `FinalScore` stores the value associated with this record.
	FinalScore                  float64                `json:"final_score"`
	// `StakeScore` stores the value associated with this record.
	StakeScore                  float64                `json:"stake_score"`
	// `UptimeScore` stores the value associated with this record.
	UptimeScore                 float64                `json:"uptime_score"`
	// `PerformanceScore` stores the value associated with this record.
	PerformanceScore            float64                `json:"performance_score"`
	// `DecentralizationScore` stores the value associated with this record.
	DecentralizationScore       float64                `json:"decentralization_score"`
	// `EffectiveStake` stores the value associated with this record.
	EffectiveStake              int64                  `json:"effective_stake"`
	// `ActualStake` stores the value associated with this record.
	ActualStake                 int64                  `json:"actual_stake"`
	// `SignedRatioBPS` stores the value associated with this record.
	SignedRatioBPS              int                    `json:"signed_ratio_bps"`
	// `MissedBlocks` stores the value associated with this record.
	MissedBlocks                uint64                 `json:"missed_blocks"`
	// `TotalSlashes` stores the measured quantity used by this operation.
	TotalSlashes                uint64                 `json:"total_slashes"`
	// `DoubleSign` stores the value associated with this record.
	DoubleSign                  uint64                 `json:"double_sign_events"`
	// `BadExecution` stores the value associated with this record.
	BadExecution                uint64                 `json:"bad_execution_events"`
	// `ValidatorAgeBlocks` stores whether the related condition is satisfied.
	ValidatorAgeBlocks          uint64                 `json:"validator_age_blocks"`
	// `ValidatorAgeEpochs` stores whether the related condition is satisfied.
	ValidatorAgeEpochs          uint64                 `json:"validator_age_epochs"`
	// `PerformanceEligible` stores the value associated with this record.
	PerformanceEligible         bool                   `json:"performance_eligible"`
	// `PerformanceAgeEligible` stores the value associated with this record.
	PerformanceAgeEligible      bool                   `json:"performance_age_eligible"`
	// `PerformanceIneligibleReason` stores the value associated with this record.
	PerformanceIneligibleReason string                 `json:"performance_ineligible_reason,omitempty"`
	// `HomePC` stores the value associated with this record.
	HomePC                      bool                   `json:"home_pc"`
	// `OperatorID` stores the value associated with this record.
	OperatorID                  string                 `json:"operator_id,omitempty"`
	// `ASN` stores the value associated with this record.
	ASN                         string                 `json:"asn,omitempty"`
	// `Country` stores the measured quantity used by this operation.
	Country                     string                 `json:"country,omitempty"`
	// `Provider` stores the value associated with this record.
	Provider                    string                 `json:"provider,omitempty"`
	// `Region` stores the value associated with this record.
	Region                      string                 `json:"region,omitempty"`
	// `FounderBadge` stores whether the related condition is satisfied.
	FounderBadge                bool                   `json:"founder_badge"`
	// `FounderEligible` stores whether the related condition is satisfied.
	FounderEligible             bool                   `json:"founder_eligible"`
	// `FounderBadgeReason` stores whether the related condition is satisfied.
	FounderBadgeReason          string                 `json:"founder_badge_reason,omitempty"`
	// `FounderNFTCollection` stores whether the related condition is satisfied.
	FounderNFTCollection        string                 `json:"founder_nft_collection,omitempty"`
	// `FounderNFTTokenID` stores whether the related condition is satisfied.
	FounderNFTTokenID           uint64                 `json:"founder_nft_token_id,omitempty"`
	// `FounderNFTVerified` stores whether the related condition is satisfied.
	FounderNFTVerified          bool                   `json:"founder_nft_verified"`
	// `CampaignRank` stores the value associated with this record.
	CampaignRank                int                    `json:"campaign_rank,omitempty"`
	// `CampaignWeeklyRank` stores the value associated with this record.
	CampaignWeeklyRank          int                    `json:"campaign_weekly_rank,omitempty"`
	// `CampaignReputationPoints` stores the value associated with this record.
	CampaignReputationPoints    int                    `json:"campaign_reputation_points,omitempty"`
	// `CampaignRawNodePoints` stores the value associated with this record.
	CampaignRawNodePoints       int                    `json:"campaign_raw_node_points,omitempty"`
	// `CampaignOperatorWeightBPS` stores the value associated with this record.
	CampaignOperatorWeightBPS   int                    `json:"campaign_operator_weight_bps,omitempty"`
	// `CampaignUsefulNode` stores the value associated with this record.
	CampaignUsefulNode          bool                   `json:"campaign_useful_node,omitempty"`
	// `CampaignNodeOnlinePoints` stores the value associated with this record.
	CampaignNodeOnlinePoints    int                    `json:"campaign_node_online_points,omitempty"`
	// `CampaignWeeklyUptimePoints` stores the value associated with this record.
	CampaignWeeklyUptimePoints  int                    `json:"campaign_weekly_uptime_points,omitempty"`
	// `CampaignDoctorProofPoints` stores the value associated with this record.
	CampaignDoctorProofPoints   int                    `json:"campaign_doctor_proof_points,omitempty"`
	// `CampaignHomePCPoints` stores the value associated with this record.
	CampaignHomePCPoints        int                    `json:"campaign_home_pc_points,omitempty"`
	// `CampaignBugPoints` stores the value associated with this record.
	CampaignBugPoints           int                    `json:"campaign_bug_points,omitempty"`
	// `CampaignBadges` stores the value associated with this record.
	CampaignBadges              []string               `json:"campaign_badges,omitempty"`
	// `DecentralizationComponents` stores the value associated with this record.
	DecentralizationComponents  map[string]float64     `json:"decentralization_components,omitempty"`
	// `ValidatorPoolRaw` stores whether the related condition is satisfied.
	ValidatorPoolRaw            map[string]interface{} `json:"validator_pool_raw,omitempty"`
}

// applyFounderConfig applies founder config.
func applyFounderConfig(fc FounderConfig) bool {
	// `changed` stores the value produced by this operation.
	changed := false
	if FounderValidatorBadgeEnabled != fc.ValidatorBadgeEnabled {
		FounderValidatorBadgeEnabled = fc.ValidatorBadgeEnabled
		changed = true
	}
	if FounderValidatorCutoffHeight != fc.ValidatorCutoffHeight {
		FounderValidatorCutoffHeight = fc.ValidatorCutoffHeight
		changed = true
	}
	if FounderValidatorMaxCount != fc.ValidatorMaxCount {
		FounderValidatorMaxCount = fc.ValidatorMaxCount
		changed = true
	}
	if fc.ValidatorMinSignedBPS > 0 && FounderValidatorMinSignedBPS != fc.ValidatorMinSignedBPS {
		FounderValidatorMinSignedBPS = fc.ValidatorMinSignedBPS
		changed = true
	}
	if strings.TrimSpace(fc.ValidatorNFTCollection) != strings.TrimSpace(FounderValidatorNFTCollection) {
		FounderValidatorNFTCollection = strings.TrimSpace(fc.ValidatorNFTCollection)
		changed = true
	}
	return changed
}

// founderValidatorTokenID implements the founder validator token id helper.
func founderValidatorTokenID(validatorID string) uint64 {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte("MSC_FOUNDER_VALIDATOR_V1|" + normalizeValidatorID(validatorID)))
	// `tokenID` stores the value produced by this operation.
	tokenID := binary.BigEndian.Uint64(sum[:8])
	if tokenID == 0 {
		tokenID = 1
	}
	return tokenID
}

// founderNFTVerified implements the founder nft verified helper.
func founderNFTVerified(collectionID string, tokenID uint64, validatorID string, ledger *Ledger) bool {
	collectionID = normalizeDTLCollectionID(collectionID)
	if collectionID == "" || tokenID == 0 || ledger == nil || ledger.DTL == nil {
		return false
	}
	// `owner` stores the value produced by this operation.
	owner := strings.TrimSpace(ledger.DTL.NFT721Owners[collectionID+"|"+strconvFormatUint(tokenID)])
	if owner == "" {
		return false
	}
	return strings.EqualFold(owner, normalizeDTLAccount(validatorID)) || strings.EqualFold(owner, normalizeValidatorID(validatorID))
}

// founderBadgeForValidator implements the founder badge for validator helper.
func founderBadgeForValidator(entry ValidatorPoolEntry, rec ValidatorRecord, rank int, height uint64, ledger *Ledger) founderValidatorBadge {
	// `out` stores the result produced by this operation.
	out := founderValidatorBadge{
		Eligible:      false,
		Badge:         false,
		Reason:        "disabled",
		NFTCollection: strings.TrimSpace(FounderValidatorNFTCollection),
	}
	if !FounderValidatorBadgeEnabled {
		return out
	}
	out.Reason = ""
	if entry.ID == "" {
		out.Reason = "missing_validator"
		return out
	}
	out.NFTTokenID = founderValidatorTokenID(entry.ID)
	if out.NFTCollection != "" {
		out.NFTVerified = founderNFTVerified(out.NFTCollection, out.NFTTokenID, entry.ID, ledger)
	}
	if rec.Status != ValidatorActive {
		out.Reason = "not_active"
		return out
	}
	if FounderValidatorCutoffHeight == 0 {
		out.Reason = "founder_cutoff_not_configured"
		return out
	}
	if FounderValidatorCutoffHeight > 0 && rec.JoinHeight > FounderValidatorCutoffHeight {
		out.Reason = "after_cutoff"
		return out
	}
	if FounderValidatorMaxCount > 0 && rank > FounderValidatorMaxCount {
		out.Reason = "outside_founder_max"
		return out
	}
	if entry.SignedRatioBPS < FounderValidatorMinSignedBPS {
		out.Reason = "signed_ratio_below_founder_minimum"
		return out
	}
	if rec.TotalSlashes > 0 || rec.DoubleSign > 0 || rec.BadExecution > 0 {
		out.Reason = "severe_fault"
		return out
	}
	if height > 0 && rec.JoinHeight > height {
		out.Reason = "join_height_in_future"
		return out
	}
	out.Eligible = true
	out.Badge = true
	return out
}

// strconvFormatUint implements the strconv format uint helper.
func strconvFormatUint(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// validatorPoolEntryRaw implements the validator pool entry raw helper.
func validatorPoolEntryRaw(entry ValidatorPoolEntry) map[string]interface{} {
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]interface{})
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// sortValidatorLeaderboardEntries implements the sort validator leaderboard entries helper.
func sortValidatorLeaderboardEntries(entries []validatorsLeaderboardEntry) {
	// `slotRank` stores the value produced by this operation.
	slotRank := func(slot string) int {
		switch strings.ToLower(strings.TrimSpace(slot)) {
		case "performance":
			return 0
		case "rotation":
			return 1
		case "standby":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Active != entries[j].Active {
			return entries[i].Active
		}
		if slotRank(entries[i].SlotType) != slotRank(entries[j].SlotType) {
			return slotRank(entries[i].SlotType) < slotRank(entries[j].SlotType)
		}
		if entries[i].FinalScore != entries[j].FinalScore {
			return entries[i].FinalScore > entries[j].FinalScore
		}
		if entries[i].SignedRatioBPS != entries[j].SignedRatioBPS {
			return entries[i].SignedRatioBPS > entries[j].SignedRatioBPS
		}
		if entries[i].EffectiveStake != entries[j].EffectiveStake {
			return entries[i].EffectiveStake > entries[j].EffectiveStake
		}
		return entries[i].ValidatorID < entries[j].ValidatorID
	})
	// `i` tracks the current position in the related collection.
	for i := range entries {
		entries[i].Rank = i + 1
	}
}

// collectValidatorsLeaderboard implements the collect validators leaderboard helper.
func (s *Server) collectValidatorsLeaderboard(height uint64) (map[string]any, int, string) {
	if s == nil || s.Node == nil {
		return nil, http.StatusServiceUnavailable, "node unavailable"
	}
	if height == 0 {
		height = uint64(s.defaultValidatorViewHeight())
	}
	// `registry` stores the value produced by this operation.
	registry := s.Node.validatorRegistrySnapshotForHeight(height)
	// `pool` stores the value produced by this operation.
	pool := s.Node.validatorPoolSnapshotForHeight(height, registry)
	// `runtime` stores the value produced by this operation.
	runtime := s.Node.runtimeStatusSnapshot()
	// `committee` stores the value produced by this operation.
	committee := canonicalValidatorIDs(s.Node.GetConsensusValidators(int(height)))
	// `localOnline` stores the value produced by this operation.
	localOnline, _ := s.Node.splitValidatorsByLiveness(committee)
	// `onlineSet` stores the value produced by this operation.
	onlineSet := make(map[string]bool, len(localOnline))
	// `id` tracks the current position in the related collection.
	for _, id := range localOnline {
		onlineSet[normalizeValidatorID(id)] = true
	}
	if len(onlineSet) == 0 {
		// `finalOnline` stores the value produced by this operation.
		finalOnline, _ := s.Node.splitValidatorsByFinalizedActivity(committee, height)
		// `id` tracks the current position in the related collection.
		for _, id := range finalOnline {
			onlineSet[normalizeValidatorID(id)] = true
		}
	}
	// `cmd` stores the value produced by this operation.
	cmd := strings.TrimSpace(runtime.ConsensusDetectorMode)
	if cmd == "" {
		cmd = runtime.ConsensusMode
	}
	// `entries` stores the value produced by this operation.
	entries := make([]validatorsLeaderboardEntry, 0, len(pool.Entries))
	// `poolEntry` tracks the current values while iterating.
	for _, poolEntry := range pool.Entries {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(poolEntry.ID)
		if id == "" {
			continue
		}
		// `rec` stores the value produced by this operation.
		rec := registry[id]
		// `info` stores the current position in the related collection.
		info := validatorHybridDiversityInfoForScoring(id)
		// `badge` stores the value produced by this operation.
		badge := founderBadgeForValidator(poolEntry, rec, len(entries)+1, height, &s.Node.Ledger)
		// `entry` stores the value produced by this operation.
		entry := validatorsLeaderboardEntry{
			ValidatorID:                 id,
			Status:                      string(rec.Status),
			SlotType:                    strings.TrimSpace(poolEntry.SlotType),
			Active:                      poolEntry.Active,
			Online:                      onlineSet[id],
			CMD:                         cmd,
			FinalScore:                  poolEntry.FinalScore,
			StakeScore:                  poolEntry.StakeScore,
			UptimeScore:                 poolEntry.UptimeScore,
			PerformanceScore:            poolEntry.PerformanceScore,
			DecentralizationScore:       poolEntry.DecentralizationScore,
			EffectiveStake:              poolEntry.EffectiveStake,
			ActualStake:                 poolEntry.ActualStake,
			SignedRatioBPS:              poolEntry.SignedRatioBPS,
			MissedBlocks:                rec.MissedBlocks,
			TotalSlashes:                rec.TotalSlashes,
			DoubleSign:                  rec.DoubleSign,
			BadExecution:                rec.BadExecution,
			ValidatorAgeBlocks:          poolEntry.ValidatorAgeBlocks,
			ValidatorAgeEpochs:          poolEntry.ValidatorAgeEpochs,
			PerformanceEligible:         poolEntry.PerformanceEligible,
			PerformanceAgeEligible:      poolEntry.PerformanceAgeEligible,
			PerformanceIneligibleReason: poolEntry.PerformanceIneligibleReason,
			HomePC:                      info.HomePC,
			OperatorID:                  info.OperatorID,
			ASN:                         info.ASN,
			Country:                     info.Country,
			Provider:                    info.Cloud,
			Region:                      info.Region,
			FounderBadge:                badge.Badge,
			FounderEligible:             badge.Eligible,
			FounderBadgeReason:          badge.Reason,
			FounderNFTCollection:        badge.NFTCollection,
			FounderNFTTokenID:           badge.NFTTokenID,
			FounderNFTVerified:          badge.NFTVerified,
			DecentralizationComponents: map[string]float64{
				"asn":      poolEntry.ASNScore,
				"region":   poolEntry.RegionScore,
				"provider": poolEntry.ProviderScore,
				"home_pc":  poolEntry.HomePCScore,
				"operator": poolEntry.OperatorScore,
			},
			ValidatorPoolRaw: validatorPoolEntryRaw(poolEntry),
		}
		entries = append(entries, entry)
	}
	sortValidatorLeaderboardEntries(entries)
	// `i` tracks the current position in the related collection.
	for i := range entries {
		// `rec` stores the value produced by this operation.
		rec := registry[entries[i].ValidatorID]
		// `poolEntry` stores the value produced by this operation.
		poolEntry := ValidatorPoolEntry{ID: entries[i].ValidatorID, SignedRatioBPS: entries[i].SignedRatioBPS}
		// `candidate` tracks the current values while iterating.
		for _, candidate := range pool.Entries {
			if normalizeValidatorID(candidate.ID) == entries[i].ValidatorID {
				poolEntry = candidate
				break
			}
		}
		// `badge` stores the value produced by this operation.
		badge := founderBadgeForValidator(poolEntry, rec, entries[i].Rank, height, &s.Node.Ledger)
		entries[i].FounderBadge = badge.Badge
		entries[i].FounderEligible = badge.Eligible
		entries[i].FounderBadgeReason = badge.Reason
		entries[i].FounderNFTCollection = badge.NFTCollection
		entries[i].FounderNFTTokenID = badge.NFTTokenID
		entries[i].FounderNFTVerified = badge.NFTVerified
	}
	// `campaignSummary` stores the value produced by this operation.
	campaignSummary := applyTestnetCampaignToLeaderboardWithReports(entries, s.loadTestnetCampaignBugReports())
	// `active`, `performance`, `rotation`, `standby`, `homePC`, and `founder` store whether the related condition is satisfied.
	active, performance, rotation, standby, homePC, founder := 0, 0, 0, 0, 0, 0
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if entry.Active {
			active++
		}
		switch strings.ToLower(entry.SlotType) {
		case "performance":
			performance++
		case "rotation":
			rotation++
		case "standby":
			standby++
		}
		if entry.HomePC {
			homePC++
		}
		if entry.FounderBadge {
			founder++
		}
	}
	return map[string]any{
		"height":                 height,
		"cmd":                    cmd,
		"pool":                   pool,
		"entries":                entries,
		"validators":             entries,
		"active_count":           active,
		"performance_count":      performance,
		"rotation_count":         rotation,
		"standby_count":          standby,
		"home_pc_count":          homePC,
		"founder_count":          founder,
		"founder_badge_enabled":  FounderValidatorBadgeEnabled,
		"founder_cutoff_height":  FounderValidatorCutoffHeight,
		"founder_max_count":      FounderValidatorMaxCount,
		"founder_min_signed_bps": FounderValidatorMinSignedBPS,
		"founder_nft_collection": strings.TrimSpace(FounderValidatorNFTCollection),
		"testnet_campaign":       campaignSummary,
	}, http.StatusOK, ""
}

// handleValidatorsLeaderboard handles validators leaderboard.
func (s *Server) handleValidatorsLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `height` stores the value produced by this operation.
	height := uint64(s.defaultValidatorViewHeight())
	// `hq` stores the value produced by this operation.
	if hq := strings.TrimSpace(r.URL.Query().Get("height")); hq != "" {
		// `parsed` and `err` store the error produced by this operation.
		parsed, err := strconv.ParseUint(hq, 10, 64)
		if err != nil || parsed == 0 {
			http.Error(w, "invalid height", http.StatusBadRequest)
			return
		}
		height = parsed
	}
	// `out`, `status`, and `errText` store the error produced by this operation.
	out, status, errText := s.collectValidatorsLeaderboard(height)
	if errText != "" {
		http.Error(w, errText, status)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

// handleV1ValidatorsLeaderboard handles v1 validators leaderboard.
func (s *Server) handleV1ValidatorsLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `height` stores the value produced by this operation.
	height := uint64(s.defaultValidatorViewHeight())
	// `hq` stores the value produced by this operation.
	if hq := strings.TrimSpace(r.URL.Query().Get("height")); hq != "" {
		// `parsed` and `err` store the error produced by this operation.
		parsed, err := strconv.ParseUint(hq, 10, 64)
		if err != nil || parsed == 0 {
			writeV1Error(w, http.StatusBadRequest, "", "invalid height")
			return
		}
		height = parsed
	}
	// `out`, `status`, and `errText` store the error produced by this operation.
	out, status, errText := s.collectValidatorsLeaderboard(height)
	if errText != "" {
		writeV1Error(w, status, "", errText)
		return
	}
	writeV1Data(w, http.StatusOK, out)
}

// founderConfigSummary implements the founder config summary helper.
func founderConfigSummary() map[string]any {
	return map[string]any{
		"validator_badge_enabled":  FounderValidatorBadgeEnabled,
		"validator_cutoff_height":  FounderValidatorCutoffHeight,
		"validator_max_count":      FounderValidatorMaxCount,
		"validator_min_signed_bps": FounderValidatorMinSignedBPS,
		"validator_nft_collection": strings.TrimSpace(FounderValidatorNFTCollection),
	}
}

// founderCollectionConfigured implements the founder collection configured helper.
func founderCollectionConfigured() bool {
	return strings.TrimSpace(FounderValidatorNFTCollection) != ""
}

// founderConfigHash implements the founder config hash helper.
func founderConfigHash() string {
	// `raw` stores the value produced by this operation.
	raw, _ := json.Marshal(founderConfigSummary())
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
