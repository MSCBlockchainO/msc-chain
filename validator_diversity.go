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
	// `ValidatorDiversityEnabled` stores whether the related condition is satisfied.
	ValidatorDiversityEnabled       = true
	// `ValidatorDiversityMode` stores whether the related condition is satisfied.
	ValidatorDiversityMode          = "warn"
	// `ValidatorDiversityMinCountries` stores whether the related condition is satisfied.
	ValidatorDiversityMinCountries  = 2
	// `ValidatorDiversityMinASNs` stores whether the related condition is satisfied.
	ValidatorDiversityMinASNs       = 3
	// `ValidatorDiversityMinClouds` stores whether the related condition is satisfied.
	ValidatorDiversityMinClouds     = 2
	// `ValidatorDiversityMaxCountryPct` stores whether the related condition is satisfied.
	ValidatorDiversityMaxCountryPct = 67
	// `ValidatorDiversityMaxASNPct` stores whether the related condition is satisfied.
	ValidatorDiversityMaxASNPct     = 50
	// `ValidatorDiversityMaxCloudPct` stores whether the related condition is satisfied.
	ValidatorDiversityMaxCloudPct   = 67

	// `validatorDiversityMu` stores whether the related condition is satisfied.
	validatorDiversityMu       sync.RWMutex
	// `validatorDiversityMetadata` stores whether the related condition is satisfied.
	validatorDiversityMetadata = map[string]ValidatorDiversityInfo{}
)

type ValidatorDiversityInfo struct {
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID string `json:"validator_id"`
	// `Country` stores the measured quantity used by this operation.
	Country     string `json:"country,omitempty"`
	// `ASN` stores the value associated with this record.
	ASN         string `json:"asn,omitempty"`
	// `Cloud` stores the value associated with this record.
	Cloud       string `json:"cloud,omitempty"`
	// `Region` stores the value associated with this record.
	Region      string `json:"region,omitempty"`
	// `OperatorID` stores the value associated with this record.
	OperatorID  string `json:"operator_id,omitempty"`
	// `HomePC` stores the value associated with this record.
	HomePC      bool   `json:"home_pc,omitempty"`
	// `Source` stores the value associated with this record.
	Source      string `json:"source,omitempty"`
}

type ValidatorDiversityReport struct {
	// `Enabled` stores whether the related condition is satisfied.
	Enabled          bool                     `json:"enabled"`
	// `Mode` stores the value associated with this record.
	Mode             string                   `json:"mode"`
	// `Healthy` stores the value associated with this record.
	Healthy          bool                     `json:"healthy"`
	// `Reason` stores the value associated with this record.
	Reason           string                   `json:"reason,omitempty"`
	// `ActiveValidators` stores the value associated with this record.
	ActiveValidators int                      `json:"active_validators"`
	// `MetadataKnown` stores the value associated with this record.
	MetadataKnown    int                      `json:"metadata_known"`
	// `MetadataMissing` stores the value associated with this record.
	MetadataMissing  int                      `json:"metadata_missing"`
	// `CountryBuckets` stores the measured quantity used by this operation.
	CountryBuckets   int                      `json:"country_buckets"`
	// `ASNBuckets` stores the value associated with this record.
	ASNBuckets       int                      `json:"asn_buckets"`
	// `CloudBuckets` stores the value associated with this record.
	CloudBuckets     int                      `json:"cloud_buckets"`
	// `RegionBuckets` stores the value associated with this record.
	RegionBuckets    int                      `json:"region_buckets"`
	// `MaxCountryPct` stores the value associated with this record.
	MaxCountryPct    int                      `json:"max_country_pct"`
	// `MaxASNPct` stores the value associated with this record.
	MaxASNPct        int                      `json:"max_asn_pct"`
	// `MaxCloudPct` stores the value associated with this record.
	MaxCloudPct      int                      `json:"max_cloud_pct"`
	// `Violations` stores the value associated with this record.
	Violations       []string                 `json:"violations,omitempty"`
	// `CountryCounts` stores the measured quantity used by this operation.
	CountryCounts    map[string]int           `json:"country_counts,omitempty"`
	// `ASNCounts` stores the value associated with this record.
	ASNCounts        map[string]int           `json:"asn_counts,omitempty"`
	// `CloudCounts` stores the value associated with this record.
	CloudCounts      map[string]int           `json:"cloud_counts,omitempty"`
	// `RegionCounts` stores the value associated with this record.
	RegionCounts     map[string]int           `json:"region_counts,omitempty"`
	// `Validators` stores whether the related condition is satisfied.
	Validators       []ValidatorDiversityInfo `json:"validators,omitempty"`
	// `Thresholds` stores the value associated with this record.
	Thresholds       map[string]int           `json:"thresholds,omitempty"`
}

// normalizeValidatorDiversityMode normalizes validator diversity mode.
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

// normalizeValidatorDiversityCountry normalizes validator diversity country.
func normalizeValidatorDiversityCountry(country string) string {
	country = strings.ToUpper(strings.TrimSpace(country))
	country = strings.ReplaceAll(country, " ", "")
	if country == "UNKNOWN" || country == "N/A" || country == "NA" {
		return ""
	}
	return country
}

// normalizeValidatorDiversityCloud normalizes validator diversity cloud.
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

// normalizeValidatorDiversityRegion normalizes validator diversity region.
func normalizeValidatorDiversityRegion(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "unknown" || region == "n/a" || region == "na" {
		return ""
	}
	return region
}

// normalizeValidatorDiversityOperator normalizes validator diversity operator.
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

// parseValidatorDiversityBool parses validator diversity bool.
func parseValidatorDiversityBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "home", "homepc", "home_pc", "residential", "consumer", "isp":
		return true
	default:
		return false
	}
}

// validatorDiversityEntryParts implements the validator diversity entry parts helper.
func validatorDiversityEntryParts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "|") {
		return splitAndTrim(raw, "|")
	}
	if strings.Contains(raw, "=") {
		// `kv` stores the value produced by this operation.
		kv := strings.SplitN(raw, "=", 2)
		if len(kv) != 2 {
			return nil
		}
		return append([]string{strings.TrimSpace(kv[0])}, splitAndTrim(kv[1], ",")...)
	}
	return strings.Fields(raw)
}

// parseValidatorDiversityEntry parses validator diversity entry.
func parseValidatorDiversityEntry(raw string) (ValidatorDiversityInfo, bool) {
	// `parts` stores the value produced by this operation.
	parts := validatorDiversityEntryParts(raw)
	if len(parts) == 0 {
		return ValidatorDiversityInfo{}, false
	}
	// `info` stores the current position in the related collection.
	info := ValidatorDiversityInfo{
		ValidatorID: normalizeValidatorID(parts[0]),
		Source:      "config",
	}
	if info.ValidatorID == "" {
		return ValidatorDiversityInfo{}, false
	}
	// `positional` stores the value produced by this operation.
	positional := 0
	// `rawPart` tracks the current values while iterating.
	for _, rawPart := range parts[1:] {
		// `part` stores the value produced by this operation.
		part := strings.TrimSpace(rawPart)
		if part == "" {
			continue
		}
		// `key` stores the key used to access the related value.
		key := ""
		// `value` stores the value currently being processed.
		value := part
		if strings.Contains(part, ":") {
			// `kv` stores the value produced by this operation.
			kv := strings.SplitN(part, ":", 2)
			key = strings.ToLower(strings.TrimSpace(kv[0]))
			value = strings.TrimSpace(kv[1])
		}
		if strings.Contains(part, "=") {
			// `kv` stores the value produced by this operation.
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

// splitAndTrim implements the split and trim helper.
func splitAndTrim(raw string, sep string) []string {
	// `pieces` stores the value produced by this operation.
	pieces := strings.Split(raw, sep)
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(pieces))
	// `piece` tracks the current values while iterating.
	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		if piece != "" {
			out = append(out, piece)
		}
	}
	return out
}

// SetValidatorDiversityMetadata sets validator diversity metadata.
func SetValidatorDiversityMetadata(entries []string) {
	// `next` stores the value produced by this operation.
	next := make(map[string]ValidatorDiversityInfo)
	// `raw` tracks the current values while iterating.
	for _, raw := range entries {
		// `info` and `ok` store whether the related condition is satisfied.
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

// validatorDiversityCanonicalInfo implements the validator diversity canonical info helper.
func validatorDiversityCanonicalInfo(info ValidatorDiversityInfo) string {
	// `homePC` stores the value produced by this operation.
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

// ValidatorDiversityMetadataHash implements the validator diversity metadata hash helper.
func ValidatorDiversityMetadataHash() string {
	// `effective` stores the value produced by this operation.
	effective := make(map[string]ValidatorDiversityInfo)
	validatorDiversityMu.RLock()
	// `id` and `info` track the current position in the related collection.
	for id, info := range validatorDiversityMetadata {
		effective[id] = info
	}
	validatorDiversityMu.RUnlock()
	// `envName` tracks the current values while iterating.
	for _, envName := range []string{"MSC_VALIDATOR_DIVERSITY_MAP", "MSC_VALIDATOR_DIVERSITY"} {
		// `raw` stores the value produced by this operation.
		raw := strings.TrimSpace(os.Getenv(envName))
		if raw == "" {
			continue
		}
		// `entry` tracks the current values while iterating.
		for _, entry := range splitValidatorDiversityEnv(raw) {
			// `info` and `ok` store whether the related condition is satisfied.
			info, ok := parseValidatorDiversityEntry(entry)
			if !ok {
				continue
			}
			// `exists` stores whether the related condition is satisfied.
			if _, exists := effective[info.ValidatorID]; !exists {
				effective[info.ValidatorID] = info
			}
		}
	}
	if len(effective) == 0 {
		return ""
	}
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, len(effective))
	// `id` tracks the current position in the related collection.
	for id := range effective {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// `canonical` stores the value produced by this operation.
	canonical := make([]string, 0, len(ids))
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		canonical = append(canonical, validatorDiversityCanonicalInfo(effective[id]))
	}
	return HashStrings(canonical)
}

// validatorDiversityMetadataForID implements the validator diversity metadata for id helper.
func validatorDiversityMetadataForID(id string) (ValidatorDiversityInfo, bool) {
	id = normalizeValidatorID(id)
	if id == "" {
		return ValidatorDiversityInfo{}, false
	}
	validatorDiversityMu.RLock()
	// `info` and `ok` store whether the related condition is satisfied.
	info, ok := validatorDiversityMetadata[id]
	validatorDiversityMu.RUnlock()
	if ok {
		info.Source = "config"
		return info, true
	}
	// `envName` tracks the current values while iterating.
	for _, envName := range []string{"MSC_VALIDATOR_DIVERSITY_MAP", "MSC_VALIDATOR_DIVERSITY"} {
		// `raw` stores the value produced by this operation.
		raw := strings.TrimSpace(os.Getenv(envName))
		if raw == "" {
			continue
		}
		// `entry` tracks the current values while iterating.
		for _, entry := range splitValidatorDiversityEnv(raw) {
			// `info` and `ok` store whether the related condition is satisfied.
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

// splitValidatorDiversityEnv implements the split validator diversity env helper.
func splitValidatorDiversityEnv(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", ";")
	raw = strings.ReplaceAll(raw, "\n", ";")
	raw = strings.ReplaceAll(raw, "\r", ";")
	return splitAndTrim(raw, ";")
}

// uniqueValidatorDiversityIDs implements the unique validator diversity i ds helper.
func uniqueValidatorDiversityIDs(validators []string) []string {
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(validators))
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(validators))
	// `raw` tracks the current values while iterating.
	for _, raw := range validators {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(raw)
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
	sort.Strings(out)
	return out
}

// maxBucketPercent returns the maximum bucket percent.
func maxBucketPercent(counts map[string]int, total int) int {
	if total <= 0 {
		return 0
	}
	// `maxCount` stores the measured quantity used by this operation.
	maxCount := 0
	// `count` tracks the measured quantity used by this operation.
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

// copyStringIntMap copies string int map.
func copyStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]int, len(in))
	// `k` and `v` track the current values while iterating.
	for k, v := range in {
		out[k] = v
	}
	return out
}

// EvaluateValidatorGeographicDiversity implements the evaluate validator geographic diversity helper.
func EvaluateValidatorGeographicDiversity(validators []string) ValidatorDiversityReport {
	// `mode` stores the value produced by this operation.
	mode := normalizeValidatorDiversityMode(ValidatorDiversityMode)
	// `enabled` stores whether the related condition is satisfied.
	enabled := ValidatorDiversityEnabled && mode != "off"
	// `ids` stores the current position in the related collection.
	ids := uniqueValidatorDiversityIDs(validators)
	// `report` stores the value produced by this operation.
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
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		// `info` and `ok` store whether the related condition is satisfied.
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

// ValidatorDiversityAllowsCandidate implements the validator diversity allows candidate helper.
func ValidatorDiversityAllowsCandidate(active []string, candidate string) (bool, ValidatorDiversityReport) {
	candidate = normalizeValidatorID(candidate)
	// `next` stores the value produced by this operation.
	next := uniqueValidatorDiversityIDs(active)
	if candidate != "" {
		// `found` stores whether the related condition is satisfied.
		found := false
		// `id` tracks the current position in the related collection.
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
	// `report` stores the value produced by this operation.
	report := EvaluateValidatorGeographicDiversity(next)
	if normalizeValidatorDiversityMode(ValidatorDiversityMode) != "enforce" {
		return true, report
	}
	return report.Healthy, report
}

// handleValidatorsDiversity handles validators diversity.
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
	// `height` stores the value produced by this operation.
	height := s.defaultValidatorViewHeight()
	// `hq` stores the value produced by this operation.
	if hq := r.URL.Query().Get("height"); hq != "" {
		// `parsed` and `err` store the error produced by this operation.
		if parsed, err := strconv.Atoi(hq); err == nil && parsed > 0 {
			height = parsed
		}
	}
	// `validators` stores whether the related condition is satisfied.
	validators := canonicalValidatorIDs(s.Node.GetConsensusValidators(height))
	if len(validators) == 0 {
		validators = canonicalValidatorIDs(s.Node.GenesisValidators)
	}
	// `report` stores the value produced by this operation.
	report := EvaluateValidatorGeographicDiversity(validators)
	// `payload` stores the value produced by this operation.
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
