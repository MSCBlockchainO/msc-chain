package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TestnetCampaignConfig struct {
	// `Enabled` stores whether the related condition is satisfied.
	Enabled bool `toml:"enabled"`
	// `SeasonID` stores the value associated with this record.
	SeasonID string `toml:"season_id"`
	// `StartTime` stores the value associated with this record.
	StartTime string `toml:"start_time"`
	// `EndTime` stores the value associated with this record.
	EndTime string `toml:"end_time"`
	// `DiscordURL` stores the value associated with this record.
	DiscordURL string `toml:"discord_url"`
	// `TelegramURL` stores the value associated with this record.
	TelegramURL string `toml:"telegram_url"`
	// `BugReportURL` stores the value associated with this record.
	BugReportURL string `toml:"bug_report_url"`
	// `WeeklyPublishDay` stores the value associated with this record.
	WeeklyPublishDay string `toml:"weekly_publish_day"`
	// `ProgramName` stores the value associated with this record.
	ProgramName string `toml:"program_name"`
	// `DataDir` stores the value associated with this record.
	DataDir string `toml:"data_dir"`
	// `FounderMaxCount` stores whether the related condition is satisfied.
	FounderMaxCount int `toml:"founder_max_count"`
	// `FounderDays` stores whether the related condition is satisfied.
	FounderDays int `toml:"founder_days"`
	// `ReporterCooldown` stores the value associated with this record.
	ReporterCooldown int `toml:"reporter_cooldown_per_day"`
}

var (
	// `TestnetCampaignEnabled` stores whether the related condition is satisfied.
	TestnetCampaignEnabled bool
	// `TestnetCampaignSeasonID` stores the value used by this operation.
	TestnetCampaignSeasonID = "testnet-season-1"
	// `TestnetCampaignProgramName` stores the value used by this operation.
	TestnetCampaignProgramName = "MSC Founding Validators Program"
	// `TestnetCampaignStartTime` stores the value used by this operation.
	TestnetCampaignStartTime string
	// `TestnetCampaignEndTime` stores the value used by this operation.
	TestnetCampaignEndTime string
	// `TestnetCampaignDiscordURL` stores the value used by this operation.
	TestnetCampaignDiscordURL string
	// `TestnetCampaignTelegramURL` stores the value used by this operation.
	TestnetCampaignTelegramURL string
	// `TestnetCampaignBugReportURL` stores the value used by this operation.
	TestnetCampaignBugReportURL string
	// `TestnetCampaignWeeklyPublishDay` stores the value used by this operation.
	TestnetCampaignWeeklyPublishDay = "Friday"
	// `TestnetCampaignDataDir` stores the value used by this operation.
	TestnetCampaignDataDir = filepath.Join("data", "campaign")
	// `TestnetCampaignFounderMaxCount` stores the measured quantity used by this operation.
	TestnetCampaignFounderMaxCount = 100
	// `TestnetCampaignFounderDays` stores the value used by this operation.
	TestnetCampaignFounderDays = 30
	// `TestnetCampaignReporterCooldown` stores the value used by this operation.
	TestnetCampaignReporterCooldown = 3
)

type testnetCampaignBugReport struct {
	// `ID` stores the current position in the related collection.
	ID string
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string
	// `ReporterID` stores the value associated with this record.
	ReporterID string
	// `Severity` stores the value associated with this record.
	Severity string
	// `Status` stores the value associated with this record.
	Status string
	// `DuplicateOf` stores the value associated with this record.
	DuplicateOf string
	// `ReviewedAt` stores the value associated with this record.
	ReviewedAt string
	// `UsefulDup` stores the value associated with this record.
	UsefulDup bool
}

type testnetCampaignScore struct {
	// `TotalPoints` stores the measured quantity used by this operation.
	TotalPoints int
	// `RawNodePoints` stores the value associated with this record.
	RawNodePoints int
	// `NodeOnlinePoints` stores the value associated with this record.
	NodeOnlinePoints int
	// `WeeklyUptimePoints` stores the value associated with this record.
	WeeklyUptimePoints int
	// `DoctorProofPoints` stores the value associated with this record.
	DoctorProofPoints int
	// `HomePCPoints` stores the value associated with this record.
	HomePCPoints int
	// `BugPoints` stores the value associated with this record.
	BugPoints int
	// `OperatorWeightBPS` stores the value associated with this record.
	OperatorWeightBPS int
	// `UsefulNode` stores the value associated with this record.
	UsefulNode bool
	// `Badges` stores the value associated with this record.
	Badges []string
	// `LeaderboardCategory` stores the value associated with this record.
	LeaderboardCategory string
	// `AntiFarmingReduction` stores the value associated with this record.
	AntiFarmingReduction int
}

type testnetCampaignBugScore struct {
	// `Points` stores the value associated with this record.
	Points map[string]int
	// `Badges` stores the value associated with this record.
	Badges map[string][]string
	// `BugsReported` stores the value associated with this record.
	BugsReported int
	// `CriticalBugs` stores the value associated with this record.
	CriticalBugs int
}

// applyTestnetCampaignConfig applies testnet campaign config.
func applyTestnetCampaignConfig(cfg TestnetCampaignConfig) bool {
	// `changed` stores the value produced by this operation.
	changed := false
	// `setBool` stores the value produced by this operation.
	setBool := func(dst *bool, v bool) {
		if *dst != v {
			*dst = v
			changed = true
		}
	}
	// `setString` stores the value produced by this operation.
	setString := func(dst *string, v string) {
		v = strings.TrimSpace(v)
		if *dst != v {
			*dst = v
			changed = true
		}
	}
	setBool(&TestnetCampaignEnabled, cfg.Enabled)
	if strings.TrimSpace(cfg.ProgramName) != "" {
		setString(&TestnetCampaignProgramName, cfg.ProgramName)
	}
	if strings.TrimSpace(cfg.SeasonID) != "" {
		setString(&TestnetCampaignSeasonID, cfg.SeasonID)
	}
	setString(&TestnetCampaignStartTime, cfg.StartTime)
	setString(&TestnetCampaignEndTime, cfg.EndTime)
	setString(&TestnetCampaignDiscordURL, cfg.DiscordURL)
	setString(&TestnetCampaignTelegramURL, cfg.TelegramURL)
	setString(&TestnetCampaignBugReportURL, cfg.BugReportURL)
	if strings.TrimSpace(cfg.WeeklyPublishDay) != "" {
		setString(&TestnetCampaignWeeklyPublishDay, cfg.WeeklyPublishDay)
	}
	if strings.TrimSpace(cfg.DataDir) != "" {
		setString(&TestnetCampaignDataDir, cfg.DataDir)
	}
	if cfg.FounderMaxCount > 0 && TestnetCampaignFounderMaxCount != cfg.FounderMaxCount {
		TestnetCampaignFounderMaxCount = cfg.FounderMaxCount
		changed = true
	}
	if cfg.FounderDays > 0 && TestnetCampaignFounderDays != cfg.FounderDays {
		TestnetCampaignFounderDays = cfg.FounderDays
		changed = true
	}
	if cfg.ReporterCooldown > 0 && TestnetCampaignReporterCooldown != cfg.ReporterCooldown {
		TestnetCampaignReporterCooldown = cfg.ReporterCooldown
		changed = true
	}
	return changed
}

// testnetCampaignParseTime implements the testnet campaign parse time helper.
func testnetCampaignParseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	// `layout` tracks the result produced by this operation.
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		// `ts` and `err` store the error produced by this operation.
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// testnetCampaignStatus implements the testnet campaign status helper.
func testnetCampaignStatus(now time.Time) (string, int64) {
	if !TestnetCampaignEnabled {
		return "disabled", 0
	}
	// `start` and `hasStart` store the value produced by this operation.
	start, hasStart := testnetCampaignParseTime(TestnetCampaignStartTime)
	// `end` and `hasEnd` store the value produced by this operation.
	end, hasEnd := testnetCampaignParseTime(TestnetCampaignEndTime)
	if hasStart && now.Before(start) {
		return "scheduled", int64(start.Sub(now).Seconds())
	}
	if hasEnd && now.After(end) {
		return "ended", 0
	}
	if hasEnd {
		return "active", int64(end.Sub(now).Seconds())
	}
	return "active", 0
}

// testnetCampaignRules implements the testnet campaign rules helper.
func testnetCampaignRules() map[string]any {
	return map[string]any{
		"rewards": "reputation_badges_only",
		"reputation": map[string]int{
			"node_online_day":               10,
			"weekly_uptime_95_bps":          100,
			"doctor_healthy_proof":          50,
			"home_pc_weekly_bonus_95_bps":   50,
			"operator_first_validator_bps":  10000,
			"operator_second_validator_bps": 5000,
			"operator_third_plus_bps":       1000,
		},
		"bug_reports": map[string]int{
			"critical":             1000,
			"high":                 500,
			"medium":               250,
			"low":                  100,
			"docs_ui":              50,
			"duplicate":            0,
			"useful_duplicate_max": 5,
			"invalid":              0,
		},
		"badges": []string{
			"MSC Founder",
			"MSC Genesis Validator",
			"MSC Home Validator",
			"MSC Uptime Hero",
			"MSC Bug Hunter",
			"Critical Hunter",
			"Early Builder",
			"Community Helper",
			"Documentation Contributor",
			"Governance Pioneer",
			"Decentralization Champion",
		},
		"bug_report_fields": []string{
			"summary",
			"severity",
			"node_version",
			"os",
			"logs",
			"reproduction_steps",
			"expected_result",
			"actual_result",
			"screenshot_or_link",
		},
		"anti_sybil":             testnetCampaignAntiSybilRules(),
		"leaderboard_categories": testnetCampaignLeaderboardCategories(),
	}
}

// applyTestnetCampaignToLeaderboard applies testnet campaign to leaderboard.
func applyTestnetCampaignToLeaderboard(entries []validatorsLeaderboardEntry) map[string]any {
	return applyTestnetCampaignToLeaderboardWithReports(entries, nil)
}

// applyTestnetCampaignToLeaderboardWithReports applies testnet campaign to leaderboard with reports.
func applyTestnetCampaignToLeaderboardWithReports(entries []validatorsLeaderboardEntry, reports []testnetCampaignBugReport) map[string]any {
	// `now` stores the value produced by this operation.
	now := time.Now().UTC()
	// `status` and `remaining` store the value produced by this operation.
	status, remaining := testnetCampaignStatus(now)
	// `bugScores` stores the result produced by this operation.
	bugScores := testnetCampaignScoreBugReportsDetailed(reports)
	// `summary` stores the value produced by this operation.
	summary := map[string]any{
		"program_name":                     strings.TrimSpace(TestnetCampaignProgramName),
		"enabled":                          TestnetCampaignEnabled,
		"season_id":                        strings.TrimSpace(TestnetCampaignSeasonID),
		"status":                           status,
		"start_time":                       strings.TrimSpace(TestnetCampaignStartTime),
		"end_time":                         strings.TrimSpace(TestnetCampaignEndTime),
		"time_remaining_seconds":           remaining,
		"weekly_publish_day":               strings.TrimSpace(TestnetCampaignWeeklyPublishDay),
		"discord_url":                      strings.TrimSpace(TestnetCampaignDiscordURL),
		"telegram_url":                     strings.TrimSpace(TestnetCampaignTelegramURL),
		"bug_report_url":                   strings.TrimSpace(TestnetCampaignBugReportURL),
		"rules":                            testnetCampaignRules(),
		"participants":                     len(entries),
		"active_validators":                countTestnetCampaignActive(entries),
		"home_validators":                  countTestnetCampaignHome(entries),
		"bugs_reported":                    bugScores.BugsReported,
		"critical_bugs":                    bugScores.CriticalBugs,
		"time_remaining":                   testnetCampaignHumanRemaining(remaining),
		"leaderboard_categories":           testnetCampaignLeaderboardCategories(),
		"badge_rules":                      testnetCampaignBadgeRules(),
		"anti_sybil_rules":                 testnetCampaignAntiSybilRules(),
		"founder_min_signed_bps":           FounderValidatorMinSignedBPS,
		"founder_award_window_days":        testnetCampaignFounderDays(),
		"founder_max_count":                testnetCampaignFounderMaxCount(),
		"founder_requires_useful_node":     true,
		"founder_requires_no_severe_fault": true,
	}
	if !TestnetCampaignEnabled {
		summary["top_validators"] = []map[string]any{}
		return summary
	}
	// `operatorSeen` stores the value produced by this operation.
	operatorSeen := map[string]int{}
	// `i` tracks the current position in the related collection.
	for i := range entries {
		// `op` stores the value produced by this operation.
		op := strings.TrimSpace(entries[i].OperatorID)
		if op == "" {
			op = entries[i].ValidatorID
		}
		operatorSeen[op]++
		// `score` stores the value produced by this operation.
		score := testnetCampaignScoreEntry(entries[i], operatorSeen[op], bugScores.Points[entries[i].ValidatorID], bugScores.Badges[entries[i].ValidatorID])
		entries[i].CampaignReputationPoints = score.TotalPoints
		entries[i].CampaignNodeOnlinePoints = score.NodeOnlinePoints
		entries[i].CampaignWeeklyUptimePoints = score.WeeklyUptimePoints
		entries[i].CampaignDoctorProofPoints = score.DoctorProofPoints
		entries[i].CampaignHomePCPoints = score.HomePCPoints
		entries[i].CampaignBugPoints = score.BugPoints
		entries[i].CampaignBadges = score.Badges
		entries[i].CampaignOperatorWeightBPS = score.OperatorWeightBPS
		entries[i].CampaignRawNodePoints = score.RawNodePoints
		entries[i].CampaignUsefulNode = score.UsefulNode
	}
	// `order` stores the value produced by this operation.
	order := make([]int, len(entries))
	// `i` tracks the current position in the related collection.
	for i := range entries {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		// `a` and `b` store the value produced by this operation.
		a, b := entries[order[i]], entries[order[j]]
		if a.CampaignReputationPoints != b.CampaignReputationPoints {
			return a.CampaignReputationPoints > b.CampaignReputationPoints
		}
		if a.CampaignBugPoints != b.CampaignBugPoints {
			return a.CampaignBugPoints > b.CampaignBugPoints
		}
		if a.SignedRatioBPS != b.SignedRatioBPS {
			return a.SignedRatioBPS > b.SignedRatioBPS
		}
		return a.Rank < b.Rank
	})
	// `top` stores the value produced by this operation.
	top := make([]map[string]any, 0, min(len(order), 20))
	// `rank` and `idx` track the current position in the related collection.
	for rank, idx := range order {
		entries[idx].CampaignRank = rank + 1
		entries[idx].CampaignWeeklyRank = rank + 1
		if rank < 20 {
			top = append(top, map[string]any{
				"rank":                rank + 1,
				"validator_id":        entries[idx].ValidatorID,
				"points":              entries[idx].CampaignReputationPoints,
				"weekly_rank":         entries[idx].CampaignWeeklyRank,
				"signed_ratio_bps":    entries[idx].SignedRatioBPS,
				"home_pc":             entries[idx].HomePC,
				"badges":              entries[idx].CampaignBadges,
				"validator_rank":      entries[idx].Rank,
				"operator_id":         entries[idx].OperatorID,
				"decentralization":    entries[idx].DecentralizationScore,
				"founder_badge":       entries[idx].FounderBadge,
				"founder_eligible":    entries[idx].FounderEligible,
				"bug_points":          entries[idx].CampaignBugPoints,
				"uptime_hero_score":   entries[idx].CampaignWeeklyUptimePoints,
				"useful_node":         entries[idx].CampaignUsefulNode,
				"operator_weight_bps": entries[idx].CampaignOperatorWeightBPS,
			})
		}
	}
	summary["top_validators"] = top
	summary["weekly_snapshot"] = map[string]any{
		"source":       "/v1/validators/leaderboard",
		"publish_day":  strings.TrimSpace(TestnetCampaignWeeklyPublishDay),
		"freeze_mode":  "manual_export_snapshot",
		"description":  "Publish the exported weekly leaderboard; later live score changes do not rewrite the published snapshot.",
		"top_count":    len(top),
		"generated_at": now.Format(time.RFC3339),
	}
	return summary
}

// testnetCampaignScoreEntry implements the testnet campaign score entry helper.
func testnetCampaignScoreEntry(entry validatorsLeaderboardEntry, operatorOrdinal int, bugPoints int, bugBadges []string) testnetCampaignScore {
	// `out` stores the result produced by this operation.
	out := testnetCampaignScore{OperatorWeightBPS: testnetCampaignOperatorWeightBPS(operatorOrdinal)}
	if !TestnetCampaignEnabled {
		return out
	}
	// `clean` stores the value produced by this operation.
	clean := testnetCampaignCleanValidator(entry)
	out.UsefulNode = testnetCampaignUsefulNode(entry)
	if out.UsefulNode {
		out.NodeOnlinePoints = 10
	}
	if out.UsefulNode && entry.SignedRatioBPS >= 9500 {
		out.WeeklyUptimePoints = 100
	}
	if out.UsefulNode && entry.HomePC && entry.SignedRatioBPS >= 9500 && clean && operatorOrdinal == 1 {
		out.HomePCPoints = 50
	}
	// `founderQualified` stores whether the related condition is satisfied.
	founderQualified := testnetCampaignFounderQualified(entry)
	if founderQualified {
		out.Badges = append(out.Badges, "MSC Founder")
	}
	if founderQualified && entry.ValidatorAgeBlocks == 0 {
		out.Badges = append(out.Badges, "MSC Genesis Validator")
	}
	if out.UsefulNode && entry.SignedRatioBPS >= 9900 && clean {
		out.Badges = append(out.Badges, "MSC Uptime Hero")
	}
	if entry.HomePC && out.HomePCPoints > 0 {
		out.Badges = append(out.Badges, "MSC Home Validator")
	}
	if clean && entry.DecentralizationScore >= 0.80 {
		out.Badges = append(out.Badges, "Decentralization Champion")
	}
	out.Badges = append(out.Badges, bugBadges...)
	out.BugPoints = bugPoints
	out.RawNodePoints = out.NodeOnlinePoints + out.WeeklyUptimePoints + out.DoctorProofPoints + out.HomePCPoints
	// `weightedNodePoints` stores the value produced by this operation.
	weightedNodePoints := out.RawNodePoints * out.OperatorWeightBPS / 10000
	if out.OperatorWeightBPS < 10000 {
		out.AntiFarmingReduction = out.RawNodePoints - weightedNodePoints
	}
	out.TotalPoints = weightedNodePoints + out.BugPoints
	return out
}

// testnetCampaignScoreBugReports implements the testnet campaign score bug reports helper.
func testnetCampaignScoreBugReports(reports []testnetCampaignBugReport) map[string]int {
	return testnetCampaignScoreBugReportsDetailed(reports).Points
}

// testnetCampaignScoreBugReportsDetailed implements the testnet campaign score bug reports detailed helper.
func testnetCampaignScoreBugReportsDetailed(reports []testnetCampaignBugReport) testnetCampaignBugScore {
	// `out` stores the result produced by this operation.
	out := testnetCampaignBugScore{Points: map[string]int{}, Badges: map[string][]string{}}
	// `seen` stores the value produced by this operation.
	seen := map[string]bool{}
	// `reporterDayCounts` stores the value produced by this operation.
	reporterDayCounts := map[string]int{}
	// `report` tracks the current values while iterating.
	for _, report := range reports {
		// `id` stores the current position in the related collection.
		id := strings.ToLower(strings.TrimSpace(report.ID))
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := normalizeValidatorID(report.ValidatorID)
		if validatorID == "" {
			continue
		}
		out.BugsReported++
		if id != "" && seen[id] {
			continue
		}
		if id != "" {
			seen[id] = true
		}
		// `status` stores the value produced by this operation.
		status := strings.ToLower(strings.TrimSpace(report.Status))
		if status == "duplicate" || strings.TrimSpace(report.DuplicateOf) != "" {
			if status == "accepted" && report.UsefulDup {
				// `reporterKey` stores the key used to access the related value.
				reporterKey := testnetCampaignReporterDayKey(report)
				if reporterKey != "" {
					if reporterDayCounts[reporterKey] >= testnetCampaignReporterCooldown() {
						continue
					}
					reporterDayCounts[reporterKey]++
				}
				out.Points[validatorID] += 5
			}
			continue
		}
		if status == "invalid" || status == "rejected" {
			continue
		}
		if status != "accepted" && status != "verified" && status != "resolved" {
			continue
		}
		// `reporterKey` stores the key used to access the related value.
		reporterKey := testnetCampaignReporterDayKey(report)
		if reporterKey != "" {
			if reporterDayCounts[reporterKey] >= testnetCampaignReporterCooldown() {
				continue
			}
			reporterDayCounts[reporterKey]++
		}
		// `points` stores the value produced by this operation.
		points := 0
		switch strings.ToLower(strings.TrimSpace(report.Severity)) {
		case "critical", "crit":
			points = 1000
			out.CriticalBugs++
			out.Badges[validatorID] = append(out.Badges[validatorID], "Critical Hunter")
		case "high":
			points = 500
		case "medium", "med":
			points = 250
		case "low":
			points = 100
		case "docs", "doc", "ui", "docs/ui", "documentation":
			points = 50
			out.Badges[validatorID] = append(out.Badges[validatorID], "Documentation Contributor")
		}
		if points > 0 {
			out.Points[validatorID] += points
			out.Badges[validatorID] = append(out.Badges[validatorID], "MSC Bug Hunter")
		}
	}
	return out
}

// collectTestnetCampaignStatus implements the collect testnet campaign status helper.
func (s *Server) collectTestnetCampaignStatus() map[string]any {
	// `entries` stores the value produced by this operation.
	entries := []validatorsLeaderboardEntry{}
	// `leaderboard` stores the value used by this operation.
	var leaderboard map[string]any
	if s != nil && s.Node != nil {
		// `out`, `status`, and `errText` store the error produced by this operation.
		if out, status, errText := s.collectValidatorsLeaderboard(0); errText == "" && status == http.StatusOK {
			leaderboard = out
			// `raw` and `ok` store whether the related condition is satisfied.
			if raw, ok := out["entries"].([]validatorsLeaderboardEntry); ok {
				entries = raw
			}
		}
	}
	// `reports` stores the value produced by this operation.
	reports := s.loadTestnetCampaignBugReports()
	// `campaign` stores the value produced by this operation.
	campaign := applyTestnetCampaignToLeaderboardWithReports(entries, reports)
	if leaderboard != nil {
		// `enriched` and `ok` store whether the related condition is satisfied.
		if enriched, ok := leaderboard["entries"].([]validatorsLeaderboardEntry); ok {
			entries = enriched
		}
	}
	return map[string]any{
		"campaign":    campaign,
		"leaderboard": map[string]any{"entries": entries},
		"community": map[string]any{
			"discord_primary": true,
			"telegram_mirror": true,
			"discord_channels": []string{
				"#start-here",
				"#node-support",
				"#bug-reports",
				"#leaderboard",
				"#announcements",
				"#validator-chat",
			},
		},
	}
}

// handleV1TestnetCampaign handles v1 testnet campaign.
func (s *Server) handleV1TestnetCampaign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	writeV1Data(w, http.StatusOK, s.collectTestnetCampaignStatus())
}

// handleV1TestnetCampaignExport handles v1 testnet campaign export.
func (s *Server) handleV1TestnetCampaignExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `week` and `err` store the error produced by this operation.
	week, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("week")))
	if err != nil || week <= 0 {
		writeV1Error(w, http.StatusBadRequest, "", "week must be a positive integer")
		return
	}
	// `path` stores the value produced by this operation.
	path := testnetCampaignWeeklySnapshotPath(testnetCampaignDataRoot(s), testnetCampaignSeasonID(), week)
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeV1Error(w, http.StatusNotFound, "", "campaign weekly snapshot not found")
			return
		}
		writeV1Error(w, http.StatusServiceUnavailable, "", "campaign weekly snapshot unavailable")
		return
	}
	// `format` stores the value produced by this operation.
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" || format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}
	if format != "csv" {
		writeV1Error(w, http.StatusBadRequest, "", "format must be json or csv")
		return
	}
	// `csvText` and `err` store the error produced by this operation.
	csvText, err := testnetCampaignSnapshotCSV(raw)
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "campaign weekly snapshot csv export failed")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, csvText)
}

// testnetCampaignOperatorWeightBPS implements the testnet campaign operator weight bps helper.
func testnetCampaignOperatorWeightBPS(operatorOrdinal int) int {
	switch {
	case operatorOrdinal <= 1:
		return 10000
	case operatorOrdinal == 2:
		return 5000
	default:
		return 1000
	}
}

// testnetCampaignCleanValidator implements the testnet campaign clean validator helper.
func testnetCampaignCleanValidator(entry validatorsLeaderboardEntry) bool {
	// `status` stores the value produced by this operation.
	status := strings.ToUpper(strings.TrimSpace(entry.Status))
	return entry.TotalSlashes == 0 && entry.DoubleSign == 0 && entry.BadExecution == 0 && (status == "" || status == "ACTIVE")
}

// testnetCampaignUsefulNode implements the testnet campaign useful node helper.
func testnetCampaignUsefulNode(entry validatorsLeaderboardEntry) bool {
	if !entry.Active || !entry.Online || !testnetCampaignCleanValidator(entry) {
		return false
	}
	if entry.SignedRatioBPS < 9000 {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(entry.CMD)) {
	case "ATTACK", "PARTITION", "HALTED", "EMERGENCY":
		return false
	default:
		return true
	}
}

// testnetCampaignFounderQualified implements the testnet campaign founder qualified helper.
func testnetCampaignFounderQualified(entry validatorsLeaderboardEntry) bool {
	if FounderValidatorCutoffHeight == 0 {
		return false
	}
	if !(entry.FounderBadge || entry.FounderEligible) {
		return false
	}
	if entry.Rank <= 0 || entry.Rank > testnetCampaignFounderMaxCount() {
		return false
	}
	if entry.SignedRatioBPS < FounderValidatorMinSignedBPS {
		return false
	}
	return testnetCampaignUsefulNode(entry)
}

// testnetCampaignReporterDayKey implements the testnet campaign reporter day key helper.
func testnetCampaignReporterDayKey(report testnetCampaignBugReport) string {
	// `reporter` stores the value produced by this operation.
	reporter := strings.ToLower(strings.TrimSpace(report.ReporterID))
	if reporter == "" {
		reporter = strings.ToLower(strings.TrimSpace(report.ValidatorID))
	}
	if reporter == "" {
		return ""
	}
	// `ts` and `ok` store whether the related condition is satisfied.
	ts, ok := testnetCampaignParseTime(report.ReviewedAt)
	if !ok {
		ts = time.Now().UTC()
	}
	return reporter + "|" + ts.UTC().Format("2006-01-02")
}

// testnetCampaignReporterCooldown implements the testnet campaign reporter cooldown helper.
func testnetCampaignReporterCooldown() int {
	if TestnetCampaignReporterCooldown <= 0 {
		return 3
	}
	return TestnetCampaignReporterCooldown
}

// testnetCampaignFounderMaxCount implements the testnet campaign founder max count helper.
func testnetCampaignFounderMaxCount() int {
	if TestnetCampaignFounderMaxCount <= 0 {
		return 100
	}
	return TestnetCampaignFounderMaxCount
}

// testnetCampaignFounderDays implements the testnet campaign founder days helper.
func testnetCampaignFounderDays() int {
	if TestnetCampaignFounderDays <= 0 {
		return 30
	}
	return TestnetCampaignFounderDays
}

// testnetCampaignSeasonID implements the testnet campaign season id helper.
func testnetCampaignSeasonID() string {
	// `season` stores the value produced by this operation.
	season := strings.TrimSpace(TestnetCampaignSeasonID)
	if season == "" {
		season = "testnet-season-1"
	}
	return sanitizePathSegment(season)
}

// testnetCampaignDataRoot implements the testnet campaign data root helper.
func testnetCampaignDataRoot(_ *Server) string {
	// `root` stores the digest used to identify or verify the related data.
	root := strings.TrimSpace(TestnetCampaignDataDir)
	if root == "" {
		root = filepath.Join("data", "campaign")
	}
	return root
}

// testnetCampaignWeeklySnapshotPath implements the testnet campaign weekly snapshot path helper.
func testnetCampaignWeeklySnapshotPath(root, season string, week int) string {
	return filepath.Join(root, sanitizePathSegment(season), fmt.Sprintf("week_%d.json", week))
}

// testnetCampaignAuditLogPath implements the testnet campaign audit log path helper.
func testnetCampaignAuditLogPath(root, season string) string {
	return filepath.Join(root, sanitizePathSegment(season), "campaign_events.log")
}

// testnetCampaignBugReportsPath implements the testnet campaign bug reports path helper.
func testnetCampaignBugReportsPath(root, season string) string {
	return filepath.Join(root, sanitizePathSegment(season), "bug_reports.json")
}

// sanitizePathSegment implements the sanitize path segment helper.
func sanitizePathSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "default"
	}
	// `b` stores the value used by this operation.
	var b strings.Builder
	// `r` tracks the current values while iterating.
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

// loadTestnetCampaignBugReports implements the load testnet campaign bug reports helper.
func (s *Server) loadTestnetCampaignBugReports() []testnetCampaignBugReport {
	// `path` stores the value produced by this operation.
	path := testnetCampaignBugReportsPath(testnetCampaignDataRoot(s), testnetCampaignSeasonID())
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// `reports` stores the value used by this operation.
	var reports []testnetCampaignBugReport
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &reports); err != nil {
		return nil
	}
	return reports
}

// testnetCampaignWriteWeeklySnapshot implements the testnet campaign write weekly snapshot helper.
func testnetCampaignWriteWeeklySnapshot(root, season string, week int, payload any) error {
	// `path` stores the value produced by this operation.
	path := testnetCampaignWeeklySnapshotPath(root, season, week)
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := writeJSONAtomic(path, payload); err != nil {
		return err
	}
	return testnetCampaignAppendAuditEvent(root, season, map[string]any{
		"type": "snapshot_published",
		"week": week,
		"path": path,
	})
}

// testnetCampaignAppendAuditEvent implements the testnet campaign append audit event helper.
func testnetCampaignAppendAuditEvent(root, season string, event map[string]any) error {
	// `path` stores the value produced by this operation.
	path := testnetCampaignAuditLogPath(root, season)
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if event == nil {
		event = map[string]any{}
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := event["ts"]; !ok {
		event["ts"] = time.Now().UTC().Format(time.RFC3339)
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	// `f` and `err` store the error produced by this operation.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

// testnetCampaignSnapshotCSV implements the testnet campaign snapshot csv helper.
func testnetCampaignSnapshotCSV(raw []byte) (string, error) {
	// `entries` and `err` store the error produced by this operation.
	entries, err := testnetCampaignSnapshotEntries(raw)
	if err != nil {
		return "", err
	}
	// `b` stores the value used by this operation.
	var b strings.Builder
	// `w` stores the value produced by this operation.
	w := csv.NewWriter(&b)
	// `err` stores the error produced by this operation.
	if err := w.Write([]string{"rank", "validator_id", "points", "weekly_rank", "signed_ratio_bps", "home_pc", "bug_points", "badges"}); err != nil {
		return "", err
	}
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		// `err` stores the error produced by this operation.
		if err := w.Write([]string{
			strconv.Itoa(entry.CampaignRank),
			entry.ValidatorID,
			strconv.Itoa(entry.CampaignReputationPoints),
			strconv.Itoa(entry.CampaignWeeklyRank),
			strconv.Itoa(entry.SignedRatioBPS),
			strconv.FormatBool(entry.HomePC),
			strconv.Itoa(entry.CampaignBugPoints),
			strings.Join(entry.CampaignBadges, "|"),
		}); err != nil {
			return "", err
		}
	}
	w.Flush()
	return b.String(), w.Error()
}

// testnetCampaignSnapshotEntries implements the testnet campaign snapshot entries helper.
func testnetCampaignSnapshotEntries(raw []byte) ([]validatorsLeaderboardEntry, error) {
	// `direct` stores the value used by this operation.
	var direct []validatorsLeaderboardEntry
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}
	// `wrapped` stores the value used by this operation.
	var wrapped struct {
		// `Entries` stores the value associated with this record.
		Entries []validatorsLeaderboardEntry `json:"entries"`
		// `Validators` stores whether the related condition is satisfied.
		Validators []validatorsLeaderboardEntry `json:"validators"`
		// `Leaderboard` stores the value associated with this record.
		Leaderboard struct {
			// `Entries` stores the value associated with this record.
			Entries []validatorsLeaderboardEntry `json:"entries"`
		} `json:"leaderboard"`
	}
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	switch {
	case len(wrapped.Entries) > 0:
		return wrapped.Entries, nil
	case len(wrapped.Validators) > 0:
		return wrapped.Validators, nil
	default:
		return wrapped.Leaderboard.Entries, nil
	}
}

// countTestnetCampaignActive implements the count testnet campaign active helper.
func countTestnetCampaignActive(entries []validatorsLeaderboardEntry) int {
	// `count` stores the measured quantity used by this operation.
	count := 0
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if entry.Active {
			count++
		}
	}
	return count
}

// countTestnetCampaignHome implements the count testnet campaign home helper.
func countTestnetCampaignHome(entries []validatorsLeaderboardEntry) int {
	// `count` stores the measured quantity used by this operation.
	count := 0
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		if entry.HomePC {
			count++
		}
	}
	return count
}

// testnetCampaignHumanRemaining implements the testnet campaign human remaining helper.
func testnetCampaignHumanRemaining(seconds int64) string {
	if seconds <= 0 {
		return "0d"
	}
	// `days` stores the value produced by this operation.
	days := seconds / 86400
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	// `hours` stores the value produced by this operation.
	hours := seconds / 3600
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	// `minutes` stores the value produced by this operation.
	minutes := seconds / 60
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%dm", minutes)
}

// testnetCampaignLeaderboardCategories implements the testnet campaign leaderboard categories helper.
func testnetCampaignLeaderboardCategories() []string {
	return []string{
		"Overall Reputation",
		"Most Stable Validator",
		"Fastest Sync Node",
		"Best Peer Connectivity",
		"Home-PC Champion",
		"Longest Consecutive Uptime",
		"Bug Hunter",
		"Community Contributor",
		"Decentralization Champion",
	}
}

// testnetCampaignBadgeRules implements the testnet campaign badge rules helper.
func testnetCampaignBadgeRules() map[string]string {
	return map[string]string{
		"MSC Founder":               "First 100 eligible validators during the first 30 campaign days, with signed ratio at or above 95%, useful-node health, and no severe fault.",
		"MSC Genesis Validator":     "Early genesis or founder-qualified validator with signed ratio at or above 95%, useful-node health, and no severe fault.",
		"MSC Home Validator":        "Verified Home-PC validator; one bonus per operator per week.",
		"MSC Uptime Hero":           "Useful node with signed ratio at or above 99% and no slash.",
		"MSC Bug Hunter":            "Accepted bug report.",
		"Critical Hunter":           "Accepted critical bug report.",
		"Early Builder":             "Manual campaign review badge for useful tooling or integrations.",
		"Community Helper":          "Manual campaign review badge for support contributions.",
		"Documentation Contributor": "Accepted docs or UI report/contribution.",
		"Governance Pioneer":        "Manual campaign review badge for governance participation.",
		"Decentralization Champion": "High decentralization score without severe fault.",
	}
}

// testnetCampaignAntiSybilRules implements the testnet campaign anti sybil rules helper.
func testnetCampaignAntiSybilRules() map[string]any {
	return map[string]any{
		"operator_id_required":       true,
		"operator_weight_bps":        []int{10000, 5000, 1000},
		"home_pc_bonus_per_operator": 1,
		"fingerprints":               "salted_only_no_raw_ip_or_hardware_public_api",
		"bug_reporter_cooldown":      testnetCampaignReporterCooldown(),
		"duplicate_points":           0,
		"useful_duplicate_max":       5,
	}
}
