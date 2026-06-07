package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type validatorDiversityGlobals struct {
	enabled       bool
	mode          string
	minCountries  int
	minASNs       int
	minClouds     int
	maxCountryPct int
	maxASNPct     int
	maxCloudPct   int
	metadata      map[string]ValidatorDiversityInfo
}

func snapshotValidatorDiversityGlobals() validatorDiversityGlobals {
	validatorDiversityMu.RLock()
	metadata := make(map[string]ValidatorDiversityInfo, len(validatorDiversityMetadata))
	for k, v := range validatorDiversityMetadata {
		metadata[k] = v
	}
	validatorDiversityMu.RUnlock()
	return validatorDiversityGlobals{
		enabled:       ValidatorDiversityEnabled,
		mode:          ValidatorDiversityMode,
		minCountries:  ValidatorDiversityMinCountries,
		minASNs:       ValidatorDiversityMinASNs,
		minClouds:     ValidatorDiversityMinClouds,
		maxCountryPct: ValidatorDiversityMaxCountryPct,
		maxASNPct:     ValidatorDiversityMaxASNPct,
		maxCloudPct:   ValidatorDiversityMaxCloudPct,
		metadata:      metadata,
	}
}

func restoreValidatorDiversityGlobals(g validatorDiversityGlobals) {
	ValidatorDiversityEnabled = g.enabled
	ValidatorDiversityMode = g.mode
	ValidatorDiversityMinCountries = g.minCountries
	ValidatorDiversityMinASNs = g.minASNs
	ValidatorDiversityMinClouds = g.minClouds
	ValidatorDiversityMaxCountryPct = g.maxCountryPct
	ValidatorDiversityMaxASNPct = g.maxASNPct
	ValidatorDiversityMaxCloudPct = g.maxCloudPct
	validatorDiversityMu.Lock()
	validatorDiversityMetadata = g.metadata
	validatorDiversityMu.Unlock()
}

func configureValidatorDiversityForTest(t *testing.T, entries []string) {
	t.Helper()
	old := snapshotValidatorDiversityGlobals()
	t.Cleanup(func() { restoreValidatorDiversityGlobals(old) })
	ValidatorDiversityEnabled = true
	ValidatorDiversityMode = "warn"
	ValidatorDiversityMinCountries = 2
	ValidatorDiversityMinASNs = 3
	ValidatorDiversityMinClouds = 2
	ValidatorDiversityMaxCountryPct = 67
	ValidatorDiversityMaxASNPct = 50
	ValidatorDiversityMaxCloudPct = 67
	SetValidatorDiversityMetadata(entries)
}

func TestValidatorGeographicDiversityHealthyMultiProvider(t *testing.T) {
	configureValidatorDiversityForTest(t, []string{
		"A|US|AS14618|AWS|us-east-1",
		"B|DE|AS24940|HETZNER|fsn1",
		"C|SG|AS14061|DIGITALOCEAN|sgp1",
		"D|US|AS8075|AZURE|eastus",
	})

	report := EvaluateValidatorGeographicDiversity([]string{"A", "B", "C", "D"})
	if !report.Healthy {
		t.Fatalf("expected healthy diversity report, got reason=%s violations=%v", report.Reason, report.Violations)
	}
	if report.CountryBuckets != 3 || report.ASNBuckets != 4 || report.CloudBuckets != 4 {
		t.Fatalf("unexpected diversity buckets: countries=%d asns=%d clouds=%d", report.CountryBuckets, report.ASNBuckets, report.CloudBuckets)
	}
}

func TestValidatorDiversityParsesOperatorAndHomePCMetadata(t *testing.T) {
	info, ok := parseValidatorDiversityEntry("H|US|AS7018|Home ISP|home-1|operator-home|home_pc")
	if !ok {
		t.Fatalf("expected metadata entry to parse")
	}
	if info.ValidatorID != "H" || info.OperatorID != "OPERATORHOME" || !info.HomePC {
		t.Fatalf("unexpected extended diversity metadata: %+v", info)
	}
}

func TestValidatorGeographicDiversityDetectsSingleAWSRegion(t *testing.T) {
	configureValidatorDiversityForTest(t, []string{
		"A|US|AS14618|AWS|us-east-1",
		"B|US|AS14618|AWS|us-east-1",
		"C|US|AS14618|AWS|us-east-1",
		"D|US|AS14618|AWS|us-east-1",
	})

	report := EvaluateValidatorGeographicDiversity([]string{"A", "B", "C", "D"})
	if report.Healthy {
		t.Fatalf("expected unhealthy report for single-region AWS validator set")
	}
	for _, want := range []string{"country_diversity_below_min", "asn_diversity_below_min", "cloud_diversity_below_min", "cloud_concentration_above_limit"} {
		if !strings.Contains(report.Reason, want) {
			t.Fatalf("expected violation %q in reason %q", want, report.Reason)
		}
	}
	if report.MaxCloudPct != 100 || report.MaxASNPct != 100 || report.MaxCountryPct != 100 {
		t.Fatalf("expected 100%% concentration, got country=%d asn=%d cloud=%d", report.MaxCountryPct, report.MaxASNPct, report.MaxCloudPct)
	}
}

func TestValidatorGeographicDiversityEnforceRejectsConcentratingCandidate(t *testing.T) {
	configureValidatorDiversityForTest(t, []string{
		"A|US|AS14618|AWS|us-east-1",
		"B|US|AS14618|AWS|us-east-1",
		"C|US|AS14618|AWS|us-east-1",
		"D|US|AS14618|AWS|us-east-1",
		"F|US|AS14618|AWS|us-east-1",
	})
	ValidatorDiversityMode = "enforce"

	allowed, report := ValidatorDiversityAllowsCandidate([]string{"A", "B", "C", "D"}, "F")
	if allowed {
		t.Fatalf("expected concentrated candidate to be rejected in enforce mode")
	}
	if report.Healthy {
		t.Fatalf("expected unhealthy report for rejected candidate")
	}
}

func TestValidatorDiversityEndpointUsesV1Envelope(t *testing.T) {
	configureValidatorDiversityForTest(t, []string{
		"A|US|AS14618|AWS|us-east-1",
		"B|DE|AS24940|HETZNER|fsn1",
		"C|SG|AS14061|DIGITALOCEAN|sgp1",
		"D|US|AS8075|AZURE|eastus",
	})
	oldRequireWallet := ConfigAuthRequireWallet
	oldAPIToken := apiToken
	t.Cleanup(func() {
		ConfigAuthRequireWallet = oldRequireWallet
		apiToken = oldAPIToken
	})
	ConfigAuthRequireWallet = false
	apiToken = ""

	node := &Node{
		ID:                "A",
		GenesisValidators: []string{"A", "B", "C", "D"},
		Blockchain:        &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "genesis"}}},
	}
	s := NewServer(node)
	req := httptest.NewRequest(http.MethodGet, "/v1/validators/diversity", nil)
	rec := httptest.NewRecorder()
	s.handleValidatorsDiversity(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Report ValidatorDiversityReport `json:"report"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	if !body.Success || !body.Data.Report.Healthy {
		t.Fatalf("expected healthy v1 diversity response, got success=%t report=%+v", body.Success, body.Data.Report)
	}
}
