package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	ValidatorDiversityEnabled       = true
	ValidatorDiversityMode          = "warn"
	ValidatorDiversityMinCountries  = 2
	ValidatorDiversityMinASNs       = 3
	ValidatorDiversityMinClouds     = 2
	ValidatorDiversityMaxCountryPct = 67
	ValidatorDiversityMaxASNPct     = 50
	ValidatorDiversityMaxCloudPct   = 67

	validatorDiversityMu       sync.RWMutex
	validatorDiversityMetadata = map[string]ValidatorDiversityInfo{}
)

type ValidatorDiversityInfo struct {
	ValidatorID string `json:"validator_id"`
	Country     string `json:"country,omitempty"`
	ASN         string `json:"asn,omitempty"`
	Cloud       string `json:"cloud,omitempty"`
	Region      string `json:"region,omitempty"`
	OperatorID  string `json:"operator_id,omitempty"`
	HomePC      bool   `json:"home_pc,omitempty"`
	Source      string `json:"source,omitempty"`
}

type ValidatorDiversityReport struct {
	Enabled          bool                     `json:"enabled"`
	Mode             string                   `json:"mode"`
	Healthy          bool                     `json:"healthy"`
	Reason           string                   `json:"reason,omitempty"`
	ActiveValidators int                      `json:"active_validators"`
	MetadataKnown    int                      `json:"metadata_known"`
	MetadataMissing  int                      `json:"metadata_missing"`
	CountryBuckets   int                      `json:"country_buckets"`
	ASNBuckets       int                      `json:"asn_buckets"`
	CloudBuckets     int                      `json:"cloud_buckets"`
	RegionBuckets    int                      `json:"region_buckets"`
	MaxCountryPct    int                      `json:"max_country_pct"`
	MaxASNPct        int                      `json:"max_asn_pct"`
	MaxCloudPct      int                      `json:"max_cloud_pct"`
	Violations       []string                 `json:"violations,omitempty"`
	CountryCounts    map[string]int           `json:"country_counts,omitempty"`
	ASNCounts        map[string]int           `json:"asn_counts,omitempty"`
	CloudCounts      map[string]int           `json:"cloud_counts,omitempty"`
	RegionCounts     map[string]int           `json:"region_counts,omitempty"`
	Validators       []ValidatorDiversityInfo `json:"validators,omitempty"`
	Thresholds       map[string]int           `json:"thresholds,omitempty"`
}

func normalizeValidatorDiversityMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "warn", "warning":
		return "warn"
	case "off", "disabled", "disable":
		return "off"
	case "enforce", "strict", "hard":
		return "enforce"
	default:
		return "warn"
	}
}

func normalizeValidatorDiversityCountry(country string) string {
	country = strings.ToUpper(strings.TrimSpace(country))
	country = strings.ReplaceAll(country, " ", "")
	if country == "UNKNOWN" || country == "N/A" || country == "NA" {
		return ""
	}
	return country
}

func normalizeValidatorDiversityCloud(cloud string) string {
	cloud = strings.ToUpper(strings.TrimSpace(cloud))
	cloud = strings.ReplaceAll(cloud, " ", "")
	cloud = strings.ReplaceAll(cloud, "_", "")
	cloud = strings.ReplaceAll(cloud, "-", "")
	switch cloud {
	case "", "UNKNOWN", "N/A", "NA":
		return ""
	case "AMAZON", "AMAZONWEB SERVICES", "AMAZONWEBSERVICES", "EC2":
		return "AWS"
	case "GOOGLE", "GOOGLECLOUD", "GOOGLECLOUDPLATFORM":
		return "GCP"
	case "MICROSOFT", "MICROSOFTAZURE":
		return "AZURE"
	case "DO", "DIGITALOCEAN":
		return "DIGITALOCEAN"
	default:
		return cloud
	}
}

func normalizeValidatorDiversityRegion(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "unknown" || region == "n/a" || region == "na" {
		return ""
	}
	return region
}

func normalizeValidatorDiversityOperator(operator string) string {
	operator = strings.ToUpper(strings.TrimSpace(operator))
	operator = strings.ReplaceAll(operator, " ", "")
	operator = strings.ReplaceAll(operator, "_", "")
	operator = strings.ReplaceAll(operator, "-", "")
	if operator == "UNKNOWN" || operator == "N/A" || operator == "NA" {
		return ""
	}
	return operator
}

func parseValidatorDiversityBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "home", "homepc", "home_pc", "residential", "consumer", "isp":
		return true
	default:
		return false
	}
}

func validatorDiversityEntryParts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "|") {
		return splitAndTrim(raw, "|")
	}
	if strings.Contains(raw, "=") {
		kv := strings.SplitN(raw, "=", 2)
		if len(kv) != 2 {
			return nil
		}
		return append([]string{strings.TrimSpace(kv[0])}, splitAndTrim(kv[1], ",")...)
	}
	return strings.Fields(raw)
}

func parseValidatorDiversityEntry(raw string) (ValidatorDiversityInfo, bool) {
	parts := validatorDiversityEntryParts(raw)
	if len(parts) == 0 {
		return ValidatorDiversityInfo{}, false
	}
	info := ValidatorDiversityInfo{
		ValidatorID: normalizeValidatorID(parts[0]),
		Source:      "config",
	}
	if info.ValidatorID == "" {
		return ValidatorDiversityInfo{}, false
	}
	positional := 0
	for _, rawPart := range parts[1:] {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			continue
		}
		key := ""
		value := part
		if strings.Contains(part, ":") {
			kv := strings.SplitN(part, ":", 2)
			key = strings.ToLower(strings.TrimSpace(kv[0]))
			value = strings.TrimSpace(kv[1])
		}
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			key = strings.ToLower(strings.TrimSpace(kv[0]))
			value = strings.TrimSpace(kv[1])
		}
		switch key {
		case "country", "cc":
			info.Country = normalizeValidatorDiversityCountry(value)
		case "asn", "as":
			info.ASN = normalizePeerDiversityASN(value)
		case "cloud", "provider":
			info.Cloud = normalizeValidatorDiversityCloud(value)
		case "region", "zone":
			info.Region = normalizeValidatorDiversityRegion(value)
		case "operator", "operator_id", "owner":
			info.OperatorID = normalizeValidatorDiversityOperator(value)
		case "home", "home_pc", "homepc", "residential":
			info.HomePC = parseValidatorDiversityBool(value)
		case "":
			switch positional {
			case 0:
				info.Country = normalizeValidatorDiversityCountry(value)
			case 1:
				info.ASN = normalizePeerDiversityASN(value)
			case 2:
				info.Cloud = normalizeValidatorDiversityCloud(value)
			case 3:
				info.Region = normalizeValidatorDiversityRegion(value)
			case 4:
				info.OperatorID = normalizeValidatorDiversityOperator(value)
			case 5:
				info.HomePC = parseValidatorDiversityBool(value)
			}
			positional++
		}
	}
	return info, true
}

func splitAndTrim(raw string, sep string) []string {
	pieces := strings.Split(raw, sep)
	out := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		if piece != "" {
			out = append(out, piece)
		}
	}
	return out
}

func SetValidatorDiversityMetadata(entries []string) {
	next := make(map[string]ValidatorDiversityInfo)
	for _, raw := range entries {
		info, ok := parseValidatorDiversityEntry(raw)
		if !ok {
			continue
		}
		next[info.ValidatorID] = info
	}
	validatorDiversityMu.Lock()
	validatorDiversityMetadata = next
	validatorDiversityMu.Unlock()
}

func validatorDiversityCanonicalInfo(info ValidatorDiversityInfo) string {
	homePC := "0"
	if info.HomePC {
		homePC = "1"
	}
	return strings.Join([]string{
		normalizeValidatorID(info.ValidatorID),
		info.Country,
		info.ASN,
		info.Cloud,
		info.Region,
		info.OperatorID,
		homePC,
	}, "|")
}

func ValidatorDiversityMetadataHash() string {
	effective := make(map[string]ValidatorDiversityInfo)
	validatorDiversityMu.RLock()
	for id, info := range validatorDiversityMetadata {
		effective[id] = info
	}
	validatorDiversityMu.RUnlock()
	for _, envName := range []string{"MSC_VALIDATOR_DIVERSITY_MAP", "MSC_VALIDATOR_DIVERSITY"} {
		raw := strings.TrimSpace(os.Getenv(envName))
		if raw == "" {
			continue
		}
		for _, entry := range splitValidatorDiversityEnv(raw) {
			info, ok := parseValidatorDiversityEntry(entry)
			if !ok {
				continue
			}
			if _, exists := effective[info.ValidatorID]; !exists {
				effective[info.ValidatorID] = info
			}
		}
	}
	if len(effective) == 0 {
		return ""
	}
	ids := make([]string, 0, len(effective))
	for id := range effective {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	canonical := make([]string, 0, len(ids))
	for _, id := range ids {
		canonical = append(canonical, validatorDiversityCanonicalInfo(effective[id]))
	}
	return HashStrings(canonical)
}

func validatorDiversityMetadataForID(id string) (ValidatorDiversityInfo, bool) {
	id = normalizeValidatorID(id)
	if id == "" {
		return ValidatorDiversityInfo{}, false
	}
	validatorDiversityMu.RLock()
	info, ok := validatorDiversityMetadata[id]
	validatorDiversityMu.RUnlock()
	if ok {
		info.Source = "config"
		return info, true
	}
	for _, envName := range []string{"MSC_VALIDATOR_DIVERSITY_MAP", "MSC_VALIDATOR_DIVERSITY"} {
		raw := strings.TrimSpace(os.Getenv(envName))
		if raw == "" {
			continue
		}
		for _, entry := range splitValidatorDiversityEnv(raw) {
			info, ok := parseValidatorDiversityEntry(entry)
			if !ok || info.ValidatorID != id {
				continue
			}
			info.Source = "env:" + envName
			return info, true
		}
	}
	return ValidatorDiversityInfo{ValidatorID: id}, false
}

func splitValidatorDiversityEnv(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", ";")
	raw = strings.ReplaceAll(raw, "\n", ";")
	raw = strings.ReplaceAll(raw, "\r", ";")
	return splitAndTrim(raw, ";")
}

func uniqueValidatorDiversityIDs(validators []string) []string {
	seen := make(map[string]struct{}, len(validators))
	out := make([]string, 0, len(validators))
	for _, raw := range validators {
		id := normalizeValidatorID(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func maxBucketPercent(counts map[string]int, total int) int {
	if total <= 0 {
		return 0
	}
	maxCount := 0
	for _, count := range counts {
		if count > maxCount {
			maxCount = count
		}
	}
	if maxCount == 0 {
		return 0
	}
	return (maxCount*100 + total - 1) / total
}

func copyStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func EvaluateValidatorGeographicDiversity(validators []string) ValidatorDiversityReport {
	mode := normalizeValidatorDiversityMode(ValidatorDiversityMode)
	enabled := ValidatorDiversityEnabled && mode != "off"
	ids := uniqueValidatorDiversityIDs(validators)
	report := ValidatorDiversityReport{
		Enabled:          enabled,
		Mode:             mode,
		Healthy:          true,
		Reason:           "ok",
		ActiveValidators: len(ids),
		CountryCounts:    make(map[string]int),
		ASNCounts:        make(map[string]int),
		CloudCounts:      make(map[string]int),
		RegionCounts:     make(map[string]int),
		Thresholds: map[string]int{
			"min_countries":   ValidatorDiversityMinCountries,
			"min_asns":        ValidatorDiversityMinASNs,
			"min_clouds":      ValidatorDiversityMinClouds,
			"max_country_pct": ValidatorDiversityMaxCountryPct,
			"max_asn_pct":     ValidatorDiversityMaxASNPct,
			"max_cloud_pct":   ValidatorDiversityMaxCloudPct,
		},
	}
	if !enabled {
		report.Reason = "disabled"
		return report
	}
	if len(ids) == 0 {
		report.Violations = append(report.Violations, "no_active_validators")
	}
	for _, id := range ids {
		info, ok := validatorDiversityMetadataForID(id)
		if !ok || info.Country == "" || info.ASN == "" || info.Cloud == "" {
			report.MetadataMissing++
			if info.ValidatorID == "" {
				info.ValidatorID = id
			}
		} else {
			report.MetadataKnown++
		}
		report.Validators = append(report.Validators, info)
		if info.Country != "" {
			report.CountryCounts[info.Country]++
		}
		if info.ASN != "" {
			report.ASNCounts[info.ASN]++
		}
		if info.Cloud != "" {
			report.CloudCounts[info.Cloud]++
		}
		if info.Region != "" {
			report.RegionCounts[info.Region]++
		}
	}
	report.CountryBuckets = len(report.CountryCounts)
	report.ASNBuckets = len(report.ASNCounts)
	report.CloudBuckets = len(report.CloudCounts)
	report.RegionBuckets = len(report.RegionCounts)
	report.MaxCountryPct = maxBucketPercent(report.CountryCounts, len(ids))
	report.MaxASNPct = maxBucketPercent(report.ASNCounts, len(ids))
	report.MaxCloudPct = maxBucketPercent(report.CloudCounts, len(ids))
	if report.MetadataMissing > 0 {
		report.Violations = append(report.Violations, "missing_validator_diversity_metadata="+strconv.Itoa(report.MetadataMissing))
	}
	if ValidatorDiversityMinCountries > 0 && report.CountryBuckets < ValidatorDiversityMinCountries {
		report.Violations = append(report.Violations, "country_diversity_below_min")
	}
	if ValidatorDiversityMinASNs > 0 && report.ASNBuckets < ValidatorDiversityMinASNs {
		report.Violations = append(report.Violations, "asn_diversity_below_min")
	}
	if ValidatorDiversityMinClouds > 0 && report.CloudBuckets < ValidatorDiversityMinClouds {
		report.Violations = append(report.Violations, "cloud_diversity_below_min")
	}
	if ValidatorDiversityMaxCountryPct > 0 && report.MaxCountryPct > ValidatorDiversityMaxCountryPct {
		report.Violations = append(report.Violations, "country_concentration_above_limit")
	}
	if ValidatorDiversityMaxASNPct > 0 && report.MaxASNPct > ValidatorDiversityMaxASNPct {
		report.Violations = append(report.Violations, "asn_concentration_above_limit")
	}
	if ValidatorDiversityMaxCloudPct > 0 && report.MaxCloudPct > ValidatorDiversityMaxCloudPct {
		report.Violations = append(report.Violations, "cloud_concentration_above_limit")
	}
	if len(report.Violations) > 0 {
		report.Healthy = false
		report.Reason = strings.Join(report.Violations, ",")
	}
	report.CountryCounts = copyStringIntMap(report.CountryCounts)
	report.ASNCounts = copyStringIntMap(report.ASNCounts)
	report.CloudCounts = copyStringIntMap(report.CloudCounts)
	report.RegionCounts = copyStringIntMap(report.RegionCounts)
	sort.Slice(report.Validators, func(i, j int) bool {
		return report.Validators[i].ValidatorID < report.Validators[j].ValidatorID
	})
	return report
}

func ValidatorDiversityAllowsCandidate(active []string, candidate string) (bool, ValidatorDiversityReport) {
	candidate = normalizeValidatorID(candidate)
	next := uniqueValidatorDiversityIDs(active)
	if candidate != "" {
		found := false
		for _, id := range next {
			if id == candidate {
				found = true
				break
			}
		}
		if !found {
			next = append(next, candidate)
			sort.Strings(next)
		}
	}
	report := EvaluateValidatorGeographicDiversity(next)
	if normalizeValidatorDiversityMode(ValidatorDiversityMode) != "enforce" {
		return true, report
	}
	return report.Healthy, report
}

func (s *Server) handleValidatorsDiversity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}
	height := s.defaultValidatorViewHeight()
	if hq := r.URL.Query().Get("height"); hq != "" {
		if parsed, err := strconv.Atoi(hq); err == nil && parsed > 0 {
			height = parsed
		}
	}
	validators := canonicalValidatorIDs(s.Node.GetConsensusValidators(height))
	if len(validators) == 0 {
		validators = canonicalValidatorIDs(s.Node.GenesisValidators)
	}
	report := EvaluateValidatorGeographicDiversity(validators)
	payload := map[string]any{
		"height": height,
		"report": report,
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		writeV1Data(w, http.StatusOK, payload)
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}
