package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func withTestnetCampaignConfig(t *testing.T, enabled bool) {
	t.Helper()
	oldEnabled := TestnetCampaignEnabled
	oldSeason := TestnetCampaignSeasonID
	oldProgram := TestnetCampaignProgramName
	oldStart := TestnetCampaignStartTime
	oldEnd := TestnetCampaignEndTime
	oldDiscord := TestnetCampaignDiscordURL
	oldTelegram := TestnetCampaignTelegramURL
	oldBug := TestnetCampaignBugReportURL
	oldPublishDay := TestnetCampaignWeeklyPublishDay
	oldDataDir := TestnetCampaignDataDir
	oldFounderMax := TestnetCampaignFounderMaxCount
	oldFounderDays := TestnetCampaignFounderDays
	oldCooldown := TestnetCampaignReporterCooldown
	oldFounderCutoff := FounderValidatorCutoffHeight
	oldFounderMinSigned := FounderValidatorMinSignedBPS
	t.Cleanup(func() {
		TestnetCampaignEnabled = oldEnabled
		TestnetCampaignSeasonID = oldSeason
		TestnetCampaignProgramName = oldProgram
		TestnetCampaignStartTime = oldStart
		TestnetCampaignEndTime = oldEnd
		TestnetCampaignDiscordURL = oldDiscord
		TestnetCampaignTelegramURL = oldTelegram
		TestnetCampaignBugReportURL = oldBug
		TestnetCampaignWeeklyPublishDay = oldPublishDay
		TestnetCampaignDataDir = oldDataDir
		TestnetCampaignFounderMaxCount = oldFounderMax
		TestnetCampaignFounderDays = oldFounderDays
		TestnetCampaignReporterCooldown = oldCooldown
		FounderValidatorCutoffHeight = oldFounderCutoff
		FounderValidatorMinSignedBPS = oldFounderMinSigned
	})
	FounderValidatorCutoffHeight = 1000
	FounderValidatorMinSignedBPS = 9500
	applyTestnetCampaignConfig(TestnetCampaignConfig{
		Enabled:          enabled,
		ProgramName:      "MSC Founding Validators Program",
		SeasonID:         "season-test",
		StartTime:        "2026-06-01T00:00:00Z",
		EndTime:          "2026-06-29T00:00:00Z",
		DiscordURL:       "https://discord.example/msc",
		TelegramURL:      "https://t.me/msc",
		BugReportURL:     "https://github.com/msc/issues/new",
		WeeklyPublishDay: "Friday",
		FounderMaxCount:  100,
		FounderDays:      30,
		ReporterCooldown: 3,
	})
}

func TestTestnetCampaignEndpointDisabled(t *testing.T) {
	withTestnetCampaignConfig(t, false)
	oldReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() { ConfigRPCRequireAuthForReadEndpoints = oldReadAuth })

	req := httptest.NewRequest(http.MethodGet, "/v1/testnet/campaign", nil)
	rec := httptest.NewRecorder()
	(&Server{}).handleV1TestnetCampaign(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Campaign struct {
				ProgramName               string `json:"program_name"`
				Enabled                   bool   `json:"enabled"`
				Status                    string `json:"status"`
				FounderMinSignedBPS       int    `json:"founder_min_signed_bps"`
				FounderAwardWindowDays    int    `json:"founder_award_window_days"`
				FounderMaxCount           int    `json:"founder_max_count"`
				FounderRequiresUsefulNode bool   `json:"founder_requires_useful_node"`
				FounderRequiresNoSevere   bool   `json:"founder_requires_no_severe_fault"`
			} `json:"campaign"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success || resp.Data.Campaign.Enabled || resp.Data.Campaign.Status != "disabled" || resp.Data.Campaign.ProgramName == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Data.Campaign.FounderMinSignedBPS != 9500 || resp.Data.Campaign.FounderAwardWindowDays != 30 || resp.Data.Campaign.FounderMaxCount != 100 || !resp.Data.Campaign.FounderRequiresUsefulNode || !resp.Data.Campaign.FounderRequiresNoSevere {
		t.Fatalf("expected founder reliability rules in campaign response: %+v", resp.Data.Campaign)
	}
}

func TestTestnetCampaignScoringAndOperatorAntiAbuse(t *testing.T) {
	withTestnetCampaignConfig(t, true)
	entries := []validatorsLeaderboardEntry{
		{Rank: 1, ValidatorID: "A", Status: "ACTIVE", Active: true, Online: true, HomePC: true, OperatorID: "OP1", SignedRatioBPS: 9900, FounderEligible: true, DecentralizationScore: 0.90},
		{Rank: 2, ValidatorID: "B", Status: "ACTIVE", Active: true, Online: true, HomePC: true, OperatorID: "OP1", SignedRatioBPS: 9600},
		{Rank: 3, ValidatorID: "C", Status: "ACTIVE", Active: true, Online: true, HomePC: true, OperatorID: "OP1", SignedRatioBPS: 9600},
		{Rank: 4, ValidatorID: "D", Status: "ACTIVE", Active: true, Online: true, HomePC: false, OperatorID: "OP2", SignedRatioBPS: 9400},
		{Rank: 5, ValidatorID: "E", Status: "ACTIVE", Active: true, Online: true, HomePC: true, OperatorID: "OP3", SignedRatioBPS: 9900, TotalSlashes: 1},
	}
	summary := applyTestnetCampaignToLeaderboard(entries)
	if summary["enabled"] != true {
		t.Fatalf("expected enabled summary: %+v", summary)
	}
	if entries[0].CampaignReputationPoints != 160 {
		t.Fatalf("expected first home-pc score 160, got %+v", entries[0])
	}
	if entries[1].CampaignHomePCPoints != 0 || entries[1].CampaignOperatorWeightBPS != 5000 || entries[1].CampaignReputationPoints != 55 {
		t.Fatalf("expected second operator node 50%% weighted score and no home bonus, got %+v", entries[1])
	}
	if entries[2].CampaignHomePCPoints != 0 || entries[2].CampaignOperatorWeightBPS != 1000 || entries[2].CampaignReputationPoints != 11 {
		t.Fatalf("expected third operator node 10%% weighted score and no home bonus, got %+v", entries[2])
	}
	if entries[3].CampaignWeeklyUptimePoints != 0 {
		t.Fatalf("expected below-95 uptime to miss weekly bonus, got %+v", entries[3])
	}
	if campaignTestContainsString(entries[4].CampaignBadges, "Home Validator") || campaignTestContainsString(entries[4].CampaignBadges, "Uptime Hero") {
		t.Fatalf("slashed validator should not get clean badges: %+v", entries[4].CampaignBadges)
	}
	if !campaignTestContainsString(entries[0].CampaignBadges, "MSC Founder") || !campaignTestContainsString(entries[0].CampaignBadges, "MSC Uptime Hero") || !campaignTestContainsString(entries[0].CampaignBadges, "Decentralization Champion") {
		t.Fatalf("expected founder and uptime badges: %+v", entries[0].CampaignBadges)
	}
}

func TestTestnetCampaignFounderReliabilityGate(t *testing.T) {
	withTestnetCampaignConfig(t, true)
	entries := []validatorsLeaderboardEntry{
		{Rank: 1, ValidatorID: "LOW", Status: "ACTIVE", Active: true, Online: true, SignedRatioBPS: 9499, FounderEligible: true},
		{Rank: 2, ValidatorID: "OK", Status: "ACTIVE", Active: true, Online: true, SignedRatioBPS: 9500, FounderEligible: true},
		{Rank: 3, ValidatorID: "SLASH", Status: "ACTIVE", Active: true, Online: true, SignedRatioBPS: 9900, FounderEligible: true, TotalSlashes: 1},
		{Rank: 4, ValidatorID: "DOUBLE", Status: "ACTIVE", Active: true, Online: true, SignedRatioBPS: 9900, FounderEligible: true, DoubleSign: 1},
		{Rank: 5, ValidatorID: "EXEC", Status: "ACTIVE", Active: true, Online: true, SignedRatioBPS: 9900, FounderEligible: true, BadExecution: 1},
		{Rank: 6, ValidatorID: "OFFLINE", Status: "ACTIVE", Active: true, Online: false, SignedRatioBPS: 9900, FounderEligible: true},
		{Rank: 7, ValidatorID: "NOTEARLY", Status: "ACTIVE", Active: true, Online: true, SignedRatioBPS: 9900, FounderEligible: false},
	}
	applyTestnetCampaignToLeaderboard(entries)
	if campaignTestContainsString(entries[0].CampaignBadges, "MSC Founder") {
		t.Fatalf("signed ratio below 95%% should not get Founder: %+v", entries[0])
	}
	if !campaignTestContainsString(entries[1].CampaignBadges, "MSC Founder") {
		t.Fatalf("95%% useful clean early validator should get Founder: %+v", entries[1])
	}
	for _, entry := range entries[2:] {
		if campaignTestContainsString(entry.CampaignBadges, "MSC Founder") {
			t.Fatalf("entry %s should not get Founder: %+v", entry.ValidatorID, entry.CampaignBadges)
		}
	}
}

func TestTestnetCampaignBugReportScoringRejectsDuplicatesAndCooldown(t *testing.T) {
	withTestnetCampaignConfig(t, true)
	got := testnetCampaignScoreBugReports([]testnetCampaignBugReport{
		{ID: "BUG-1", ValidatorID: "A", ReporterID: "R1", Severity: "critical", Status: "accepted", ReviewedAt: "2026-06-02T00:00:00Z"},
		{ID: "BUG-1", ValidatorID: "A", ReporterID: "R1", Severity: "high", Status: "accepted", ReviewedAt: "2026-06-02T01:00:00Z"},
		{ID: "BUG-2", ValidatorID: "B", ReporterID: "R2", Severity: "medium", Status: "duplicate", DuplicateOf: "BUG-1", ReviewedAt: "2026-06-02T00:00:00Z"},
		{ID: "BUG-3", ValidatorID: "C", ReporterID: "R3", Severity: "high", Status: "invalid", ReviewedAt: "2026-06-02T00:00:00Z"},
		{ID: "BUG-4", ValidatorID: "D", ReporterID: "R4", Severity: "docs/ui", Status: "accepted", ReviewedAt: "2026-06-02T00:00:00Z"},
		{ID: "BUG-5", ValidatorID: "E", ReporterID: "R5", Severity: "high", Status: "accepted", DuplicateOf: "BUG-1", UsefulDup: true, ReviewedAt: "2026-06-02T00:00:00Z"},
		{ID: "BUG-6", ValidatorID: "F", ReporterID: "R6", Severity: "low", Status: "accepted", ReviewedAt: "2026-06-02T00:00:00Z"},
		{ID: "BUG-7", ValidatorID: "F", ReporterID: "R6", Severity: "low", Status: "accepted", ReviewedAt: "2026-06-02T01:00:00Z"},
		{ID: "BUG-8", ValidatorID: "F", ReporterID: "R6", Severity: "low", Status: "accepted", ReviewedAt: "2026-06-02T02:00:00Z"},
		{ID: "BUG-9", ValidatorID: "F", ReporterID: "R6", Severity: "low", Status: "accepted", ReviewedAt: "2026-06-02T03:00:00Z"},
	})
	if got["A"] != 1000 {
		t.Fatalf("expected duplicate to score 0, got %+v", got)
	}
	if got["B"] != 0 {
		t.Fatalf("expected B duplicate points to be 0, got %+v", got)
	}
	if got["C"] != 0 {
		t.Fatalf("expected invalid report to score 0, got %+v", got)
	}
	if got["D"] != 50 {
		t.Fatalf("expected docs/ui score, got %+v", got)
	}
	if got["E"] != 5 {
		t.Fatalf("expected useful duplicate cap 5, got %+v", got)
	}
	if got["F"] != 300 {
		t.Fatalf("expected reporter cooldown after 3 scored reports, got %+v", got)
	}
}

func TestTestnetCampaignSnapshotExportAndAudit(t *testing.T) {
	withTestnetCampaignConfig(t, true)
	root := t.TempDir()
	TestnetCampaignDataDir = root
	entries := []validatorsLeaderboardEntry{{ValidatorID: "A", CampaignRank: 1, CampaignWeeklyRank: 1, CampaignReputationPoints: 160, CampaignBugPoints: 50, SignedRatioBPS: 9900, HomePC: true, CampaignBadges: []string{"MSC Founder"}}}
	if err := testnetCampaignWriteWeeklySnapshot(root, TestnetCampaignSeasonID, 1, map[string]any{"entries": entries}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	logRaw, err := os.ReadFile(testnetCampaignAuditLogPath(root, TestnetCampaignSeasonID))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(logRaw), "snapshot_published") {
		t.Fatalf("expected snapshot audit event, got %s", string(logRaw))
	}

	oldReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() { ConfigRPCRequireAuthForReadEndpoints = oldReadAuth })
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/v1/testnet/campaign/export?format=csv&week=1", nil)
	rec := httptest.NewRecorder()
	server.handleV1TestnetCampaignExport(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "validator_id") || !strings.Contains(rec.Body.String(), "MSC Founder") {
		t.Fatalf("unexpected csv export status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/testnet/campaign/export?format=json&week=2", nil)
	rec = httptest.NewRecorder()
	server.handleV1TestnetCampaignExport(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected missing snapshot 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestnetCampaignConfigTOML(t *testing.T) {
	var cfg struct {
		TestnetCampaign TestnetCampaignConfig `toml:"testnet_campaign"`
	}
	if _, err := toml.Decode(`
[testnet_campaign]
enabled = true
program_name = "MSC Founding Validators Program"
season_id = "season-1"
start_time = "2026-06-01T00:00:00Z"
end_time = "2026-06-29T00:00:00Z"
discord_url = "https://discord.example/msc"
telegram_url = "https://t.me/msc"
bug_report_url = "https://github.example/issues/new"
weekly_publish_day = "Friday"
data_dir = "data/campaign"
founder_max_count = 100
founder_days = 30
reporter_cooldown_per_day = 3
`, &cfg); err != nil {
		t.Fatalf("decode campaign config: %v", err)
	}
	if !cfg.TestnetCampaign.Enabled || cfg.TestnetCampaign.ProgramName == "" || cfg.TestnetCampaign.SeasonID != "season-1" || cfg.TestnetCampaign.WeeklyPublishDay != "Friday" || cfg.TestnetCampaign.ReporterCooldown != 3 {
		t.Fatalf("unexpected config: %+v", cfg.TestnetCampaign)
	}
}

func campaignTestContainsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
