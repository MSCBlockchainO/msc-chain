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
	Enabled          bool   `toml:"enabled"`
	SeasonID         string `toml:"season_id"`
	StartTime        string `toml:"start_time"`
	EndTime          string `toml:"end_time"`
	DiscordURL       string `toml:"discord_url"`
	TelegramURL      string `toml:"telegram_url"`
	BugReportURL     string `toml:"bug_report_url"`
	WeeklyPublishDay string `toml:"weekly_publish_day"`
	ProgramName      string `toml:"program_name"`
	DataDir          string `toml:"data_dir"`
	FounderMaxCount  int    `toml:"founder_max_count"`
	FounderDays      int    `toml:"founder_days"`
	ReporterCooldown int    `toml:"reporter_cooldown_per_day"`
}

var (
	TestnetCampaignEnabled          bool
	TestnetCampaignSeasonID         = "testnet-season-1"
	TestnetCampaignProgramName      = "MSC Founding Validators Program"
	TestnetCampaignStartTime        string
	TestnetCampaignEndTime          string
	TestnetCampaignDiscordURL       string
	TestnetCampaignTelegramURL      string
	TestnetCampaignBugReportURL     string
	TestnetCampaignWeeklyPublishDay = "Friday"
	TestnetCampaignDataDir          = filepath.Join("data", "campaign")
	TestnetCampaignFounderMaxCount  = 100
	TestnetCampaignFounderDays      = 30
	TestnetCampaignReporterCooldown = 3
)

type testnetCampaignBugReport struct {
	ID          string
	ValidatorID string
	ReporterID  string
	Severity    string
	Status      string
	DuplicateOf string
	ReviewedAt  string
	UsefulDup   bool
}

type testnetCampaignScore struct {
	TotalPoints          int
	RawNodePoints        int
	NodeOnlinePoints     int
	WeeklyUptimePoints   int
	DoctorProofPoints    int
	HomePCPoints         int
	BugPoints            int
	OperatorWeightBPS    int
	UsefulNode           bool
	Badges               []string
	LeaderboardCategory  string
	AntiFarmingReduction int
}

type testnetCampaignBugScore struct {
	Points       map[string]int
	Badges       map[string][]string
	BugsReported int
	CriticalBugs int
}

func applyTestnetCampaignConfig(cfg TestnetCampaignConfig) bool {
	changed := false
	setBool := func(dst *bool, v bool) {
		if *dst != v {
			*dst = v
			changed = true
		}
	}
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

func testnetCampaignParseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func testnetCampaignStatus(now time.Time) (string, int64) {
	if !TestnetCampaignEnabled {
		return "disabled", 0
	}
	start, hasStart := testnetCampaignParseTime(TestnetCampaignStartTime)
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

func applyTestnetCampaignToLeaderboard(entries []validatorsLeaderboardEntry) map[string]any {
	return applyTestnetCampaignToLeaderboardWithReports(entries, nil)
}

func applyTestnetCampaignToLeaderboardWithReports(entries []validatorsLeaderboardEntry, reports []testnetCampaignBugReport) map[string]any {
	now := time.Now().UTC()
	status, remaining := testnetCampaignStatus(now)
	bugScores := testnetCampaignScoreBugReportsDetailed(reports)
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
	operatorSeen := map[string]int{}
	for i := range entries {
		op := strings.TrimSpace(entries[i].OperatorID)
		if op == "" {
			op = entries[i].ValidatorID
		}
		operatorSeen[op]++
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
	order := make([]int, len(entries))
	for i := range entries {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
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
	top := make([]map[string]any, 0, min(len(order), 20))
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

func testnetCampaignScoreEntry(entry validatorsLeaderboardEntry, operatorOrdinal int, bugPoints int, bugBadges []string) testnetCampaignScore {
	out := testnetCampaignScore{OperatorWeightBPS: testnetCampaignOperatorWeightBPS(operatorOrdinal)}
	if !TestnetCampaignEnabled {
		return out
	}
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
	weightedNodePoints := out.RawNodePoints * out.OperatorWeightBPS / 10000
	if out.OperatorWeightBPS < 10000 {
		out.AntiFarmingReduction = out.RawNodePoints - weightedNodePoints
	}
	out.TotalPoints = weightedNodePoints + out.BugPoints
	return out
}

func testnetCampaignScoreBugReports(reports []testnetCampaignBugReport) map[string]int {
	return testnetCampaignScoreBugReportsDetailed(reports).Points
}

func testnetCampaignScoreBugReportsDetailed(reports []testnetCampaignBugReport) testnetCampaignBugScore {
	out := testnetCampaignBugScore{Points: map[string]int{}, Badges: map[string][]string{}}
	seen := map[string]bool{}
	reporterDayCounts := map[string]int{}
	for _, report := range reports {
		id := strings.ToLower(strings.TrimSpace(report.ID))
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
		status := strings.ToLower(strings.TrimSpace(report.Status))
		if status == "duplicate" || strings.TrimSpace(report.DuplicateOf) != "" {
			if status == "accepted" && report.UsefulDup {
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
		reporterKey := testnetCampaignReporterDayKey(report)
		if reporterKey != "" {
			if reporterDayCounts[reporterKey] >= testnetCampaignReporterCooldown() {
				continue
			}
			reporterDayCounts[reporterKey]++
		}
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

func (s *Server) collectTestnetCampaignStatus() map[string]any {
	entries := []validatorsLeaderboardEntry{}
	var leaderboard map[string]any
	if s != nil && s.Node != nil {
		if out, status, errText := s.collectValidatorsLeaderboard(0); errText == "" && status == http.StatusOK {
			leaderboard = out
			if raw, ok := out["entries"].([]validatorsLeaderboardEntry); ok {
				entries = raw
			}
		}
	}
	reports := s.loadTestnetCampaignBugReports()
	campaign := applyTestnetCampaignToLeaderboardWithReports(entries, reports)
	if leaderboard != nil {
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

func (s *Server) handleV1TestnetCampaignExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	week, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("week")))
	if err != nil || week <= 0 {
		writeV1Error(w, http.StatusBadRequest, "", "week must be a positive integer")
		return
	}
	path := testnetCampaignWeeklySnapshotPath(testnetCampaignDataRoot(s), testnetCampaignSeasonID(), week)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeV1Error(w, http.StatusNotFound, "", "campaign weekly snapshot not found")
			return
		}
		writeV1Error(w, http.StatusServiceUnavailable, "", "campaign weekly snapshot unavailable")
		return
	}
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
	csvText, err := testnetCampaignSnapshotCSV(raw)
	if err != nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "campaign weekly snapshot csv export failed")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, csvText)
}

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

func testnetCampaignCleanValidator(entry validatorsLeaderboardEntry) bool {
	status := strings.ToUpper(strings.TrimSpace(entry.Status))
	return entry.TotalSlashes == 0 && entry.DoubleSign == 0 && entry.BadExecution == 0 && (status == "" || status == "ACTIVE")
}

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

func testnetCampaignReporterDayKey(report testnetCampaignBugReport) string {
	reporter := strings.ToLower(strings.TrimSpace(report.ReporterID))
	if reporter == "" {
		reporter = strings.ToLower(strings.TrimSpace(report.ValidatorID))
	}
	if reporter == "" {
		return ""
	}
	ts, ok := testnetCampaignParseTime(report.ReviewedAt)
	if !ok {
		ts = time.Now().UTC()
	}
	return reporter + "|" + ts.UTC().Format("2006-01-02")
}

func testnetCampaignReporterCooldown() int {
	if TestnetCampaignReporterCooldown <= 0 {
		return 3
	}
	return TestnetCampaignReporterCooldown
}

func testnetCampaignFounderMaxCount() int {
	if TestnetCampaignFounderMaxCount <= 0 {
		return 100
	}
	return TestnetCampaignFounderMaxCount
}

func testnetCampaignFounderDays() int {
	if TestnetCampaignFounderDays <= 0 {
		return 30
	}
	return TestnetCampaignFounderDays
}

func testnetCampaignSeasonID() string {
	season := strings.TrimSpace(TestnetCampaignSeasonID)
	if season == "" {
		season = "testnet-season-1"
	}
	return sanitizePathSegment(season)
}

func testnetCampaignDataRoot(s *Server) string {
	root := strings.TrimSpace(TestnetCampaignDataDir)
	if root == "" {
		root = filepath.Join("data", "campaign")
	}
	return root
}

func testnetCampaignWeeklySnapshotPath(root, season string, week int) string {
	return filepath.Join(root, sanitizePathSegment(season), fmt.Sprintf("week_%d.json", week))
}

func testnetCampaignAuditLogPath(root, season string) string {
	return filepath.Join(root, sanitizePathSegment(season), "campaign_events.log")
}

func testnetCampaignBugReportsPath(root, season string) string {
	return filepath.Join(root, sanitizePathSegment(season), "bug_reports.json")
}

func sanitizePathSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "default"
	}
	var b strings.Builder
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

func (s *Server) loadTestnetCampaignBugReports() []testnetCampaignBugReport {
	path := testnetCampaignBugReportsPath(testnetCampaignDataRoot(s), testnetCampaignSeasonID())
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var reports []testnetCampaignBugReport
	if err := json.Unmarshal(raw, &reports); err != nil {
		return nil
	}
	return reports
}

func testnetCampaignWriteWeeklySnapshot(root, season string, week int, payload any) error {
	path := testnetCampaignWeeklySnapshotPath(root, season, week)
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err := writeJSONAtomic(path, payload); err != nil {
		return err
	}
	return testnetCampaignAppendAuditEvent(root, season, map[string]any{
		"type": "snapshot_published",
		"week": week,
		"path": path,
	})
}

func testnetCampaignAppendAuditEvent(root, season string, event map[string]any) error {
	path := testnetCampaignAuditLogPath(root, season)
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if event == nil {
		event = map[string]any{}
	}
	if _, ok := event["ts"]; !ok {
		event["ts"] = time.Now().UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

func testnetCampaignSnapshotCSV(raw []byte) (string, error) {
	entries, err := testnetCampaignSnapshotEntries(raw)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write([]string{"rank", "validator_id", "points", "weekly_rank", "signed_ratio_bps", "home_pc", "bug_points", "badges"}); err != nil {
		return "", err
	}
	for _, entry := range entries {
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

func testnetCampaignSnapshotEntries(raw []byte) ([]validatorsLeaderboardEntry, error) {
	var direct []validatorsLeaderboardEntry
	if err := json.Unmarshal(raw, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}
	var wrapped struct {
		Entries     []validatorsLeaderboardEntry `json:"entries"`
		Validators  []validatorsLeaderboardEntry `json:"validators"`
		Leaderboard struct {
			Entries []validatorsLeaderboardEntry `json:"entries"`
		} `json:"leaderboard"`
	}
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

func countTestnetCampaignActive(entries []validatorsLeaderboardEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Active {
			count++
		}
	}
	return count
}

func countTestnetCampaignHome(entries []validatorsLeaderboardEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.HomePC {
			count++
		}
	}
	return count
}

func testnetCampaignHumanRemaining(seconds int64) string {
	if seconds <= 0 {
		return "0d"
	}
	days := seconds / 86400
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := seconds / 3600
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	minutes := seconds / 60
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%dm", minutes)
}

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
