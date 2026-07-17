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
	FounderValidatorBadgeEnabled  = false
	FounderValidatorCutoffHeight  uint64
	FounderValidatorMaxCount      int
	FounderValidatorMinSignedBPS  = 9500
	FounderValidatorNFTCollection string
)

type founderValidatorBadge struct {
	Eligible      bool   `json:"eligible"`
	Badge         bool   `json:"badge"`
	Reason        string `json:"reason,omitempty"`
	NFTCollection string `json:"nft_collection,omitempty"`
	NFTTokenID    uint64 `json:"nft_token_id,omitempty"`
	NFTVerified   bool   `json:"nft_verified"`
}

type validatorsLeaderboardEntry struct {
	Rank                        int                    `json:"rank"`
	ValidatorID                 string                 `json:"validator_id"`
	Status                      string                 `json:"status"`
	SlotType                    string                 `json:"slot_type"`
	Active                      bool                   `json:"active"`
	Online                      bool                   `json:"online"`
	CMD                         string                 `json:"cmd"`
	FinalScore                  float64                `json:"final_score"`
	StakeScore                  float64                `json:"stake_score"`
	UptimeScore                 float64                `json:"uptime_score"`
	PerformanceScore            float64                `json:"performance_score"`
	DecentralizationScore       float64                `json:"decentralization_score"`
	EffectiveStake              int64                  `json:"effective_stake"`
	ActualStake                 int64                  `json:"actual_stake"`
	SignedRatioBPS              int                    `json:"signed_ratio_bps"`
	MissedBlocks                uint64                 `json:"missed_blocks"`
	TotalSlashes                uint64                 `json:"total_slashes"`
	DoubleSign                  uint64                 `json:"double_sign_events"`
	BadExecution                uint64                 `json:"bad_execution_events"`
	ValidatorAgeBlocks          uint64                 `json:"validator_age_blocks"`
	ValidatorAgeEpochs          uint64                 `json:"validator_age_epochs"`
	PerformanceEligible         bool                   `json:"performance_eligible"`
	PerformanceAgeEligible      bool                   `json:"performance_age_eligible"`
	PerformanceIneligibleReason string                 `json:"performance_ineligible_reason,omitempty"`
	HomePC                      bool                   `json:"home_pc"`
	OperatorID                  string                 `json:"operator_id,omitempty"`
	ASN                         string                 `json:"asn,omitempty"`
	Country                     string                 `json:"country,omitempty"`
	Provider                    string                 `json:"provider,omitempty"`
	Region                      string                 `json:"region,omitempty"`
	FounderBadge                bool                   `json:"founder_badge"`
	FounderEligible             bool                   `json:"founder_eligible"`
	FounderBadgeReason          string                 `json:"founder_badge_reason,omitempty"`
	FounderNFTCollection        string                 `json:"founder_nft_collection,omitempty"`
	FounderNFTTokenID           uint64                 `json:"founder_nft_token_id,omitempty"`
	FounderNFTVerified          bool                   `json:"founder_nft_verified"`
	CampaignRank                int                    `json:"campaign_rank,omitempty"`
	CampaignWeeklyRank          int                    `json:"campaign_weekly_rank,omitempty"`
	CampaignReputationPoints    int                    `json:"campaign_reputation_points,omitempty"`
	CampaignRawNodePoints       int                    `json:"campaign_raw_node_points,omitempty"`
	CampaignOperatorWeightBPS   int                    `json:"campaign_operator_weight_bps,omitempty"`
	CampaignUsefulNode          bool                   `json:"campaign_useful_node,omitempty"`
	CampaignNodeOnlinePoints    int                    `json:"campaign_node_online_points,omitempty"`
	CampaignWeeklyUptimePoints  int                    `json:"campaign_weekly_uptime_points,omitempty"`
	CampaignDoctorProofPoints   int                    `json:"campaign_doctor_proof_points,omitempty"`
	CampaignHomePCPoints        int                    `json:"campaign_home_pc_points,omitempty"`
	CampaignBugPoints           int                    `json:"campaign_bug_points,omitempty"`
	CampaignBadges              []string               `json:"campaign_badges,omitempty"`
	DecentralizationComponents  map[string]float64     `json:"decentralization_components,omitempty"`
	ValidatorPoolRaw            map[string]interface{} `json:"validator_pool_raw,omitempty"`
}

func applyFounderConfig(fc FounderConfig) bool {
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

func founderValidatorTokenID(validatorID string) uint64 {
	sum := sha256.Sum256([]byte("MSC_FOUNDER_VALIDATOR_V1|" + normalizeValidatorID(validatorID)))
	tokenID := binary.BigEndian.Uint64(sum[:8])
	if tokenID == 0 {
		tokenID = 1
	}
	return tokenID
}

func founderNFTVerified(collectionID string, tokenID uint64, validatorID string, ledger *Ledger) bool {
	collectionID = normalizeDTLCollectionID(collectionID)
	if collectionID == "" || tokenID == 0 || ledger == nil || ledger.DTL == nil {
		return false
	}
	owner := strings.TrimSpace(ledger.DTL.NFT721Owners[collectionID+"|"+strconvFormatUint(tokenID)])
	if owner == "" {
		return false
	}
	return strings.EqualFold(owner, normalizeDTLAccount(validatorID)) || strings.EqualFold(owner, normalizeValidatorID(validatorID))
}

func founderBadgeForValidator(entry ValidatorPoolEntry, rec ValidatorRecord, rank int, height uint64, ledger *Ledger) founderValidatorBadge {
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

func strconvFormatUint(v uint64) string {
	return strconv.FormatUint(v, 10)
}

func validatorPoolEntryRaw(entry ValidatorPoolEntry) map[string]interface{} {
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil
	}
	out := make(map[string]interface{})
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func sortValidatorLeaderboardEntries(entries []validatorsLeaderboardEntry) {
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
	for i := range entries {
		entries[i].Rank = i + 1
	}
}

func (s *Server) collectValidatorsLeaderboard(height uint64) (map[string]any, int, string) {
	if s == nil || s.Node == nil {
		return nil, http.StatusServiceUnavailable, "node unavailable"
	}
	if height == 0 {
		height = uint64(s.defaultValidatorViewHeight())
	}
	registry := s.Node.validatorRegistrySnapshotForHeight(height)
	pool := s.Node.validatorPoolSnapshotForHeight(height, registry)
	runtime := s.Node.runtimeStatusSnapshot()
	committee := canonicalValidatorIDs(s.Node.GetConsensusValidators(int(height)))
	localOnline, _ := s.Node.splitValidatorsByLiveness(committee)
	onlineSet := make(map[string]bool, len(localOnline))
	for _, id := range localOnline {
		onlineSet[normalizeValidatorID(id)] = true
	}
	if len(onlineSet) == 0 {
		finalOnline, _ := s.Node.splitValidatorsByFinalizedActivity(committee, height)
		for _, id := range finalOnline {
			onlineSet[normalizeValidatorID(id)] = true
		}
	}
	cmd := strings.TrimSpace(runtime.ConsensusDetectorMode)
	if cmd == "" {
		cmd = runtime.ConsensusMode
	}
	entries := make([]validatorsLeaderboardEntry, 0, len(pool.Entries))
	for _, poolEntry := range pool.Entries {
		id := normalizeValidatorID(poolEntry.ID)
		if id == "" {
			continue
		}
		rec := registry[id]
		info := validatorHybridDiversityInfoForScoring(id)
		badge := founderBadgeForValidator(poolEntry, rec, len(entries)+1, height, &s.Node.Ledger)
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
	for i := range entries {
		rec := registry[entries[i].ValidatorID]
		poolEntry := ValidatorPoolEntry{ID: entries[i].ValidatorID, SignedRatioBPS: entries[i].SignedRatioBPS}
		for _, candidate := range pool.Entries {
			if normalizeValidatorID(candidate.ID) == entries[i].ValidatorID {
				poolEntry = candidate
				break
			}
		}
		badge := founderBadgeForValidator(poolEntry, rec, entries[i].Rank, height, &s.Node.Ledger)
		entries[i].FounderBadge = badge.Badge
		entries[i].FounderEligible = badge.Eligible
		entries[i].FounderBadgeReason = badge.Reason
		entries[i].FounderNFTCollection = badge.NFTCollection
		entries[i].FounderNFTTokenID = badge.NFTTokenID
		entries[i].FounderNFTVerified = badge.NFTVerified
	}
	campaignSummary := applyTestnetCampaignToLeaderboardWithReports(entries, s.loadTestnetCampaignBugReports())
	active, performance, rotation, standby, homePC, founder := 0, 0, 0, 0, 0, 0
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

func (s *Server) handleValidatorsLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	height := uint64(s.defaultValidatorViewHeight())
	if hq := strings.TrimSpace(r.URL.Query().Get("height")); hq != "" {
		parsed, err := strconv.ParseUint(hq, 10, 64)
		if err != nil || parsed == 0 {
			http.Error(w, "invalid height", http.StatusBadRequest)
			return
		}
		height = parsed
	}
	out, status, errText := s.collectValidatorsLeaderboard(height)
	if errText != "" {
		http.Error(w, errText, status)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleV1ValidatorsLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	height := uint64(s.defaultValidatorViewHeight())
	if hq := strings.TrimSpace(r.URL.Query().Get("height")); hq != "" {
		parsed, err := strconv.ParseUint(hq, 10, 64)
		if err != nil || parsed == 0 {
			writeV1Error(w, http.StatusBadRequest, "", "invalid height")
			return
		}
		height = parsed
	}
	out, status, errText := s.collectValidatorsLeaderboard(height)
	if errText != "" {
		writeV1Error(w, status, "", errText)
		return
	}
	writeV1Data(w, http.StatusOK, out)
}

func founderConfigSummary() map[string]any {
	return map[string]any{
		"validator_badge_enabled":  FounderValidatorBadgeEnabled,
		"validator_cutoff_height":  FounderValidatorCutoffHeight,
		"validator_max_count":      FounderValidatorMaxCount,
		"validator_min_signed_bps": FounderValidatorMinSignedBPS,
		"validator_nft_collection": strings.TrimSpace(FounderValidatorNFTCollection),
	}
}

func founderCollectionConfigured() bool {
	return strings.TrimSpace(FounderValidatorNFTCollection) != ""
}

func founderConfigHash() string {
	raw, _ := json.Marshal(founderConfigSummary())
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
