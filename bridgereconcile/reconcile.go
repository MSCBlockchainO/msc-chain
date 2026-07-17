package bridgereconcile

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"msc-chain/bridgeobserver"
)

const (
	LegacyConfigVersion = "msc-bridge-reconciler-config-v1"
	ConfigVersion       = "msc-bridge-reconciler-config-v2"
	AccountingVersion   = "msc-bridge-accounting-v1"
	ReportVersion       = "msc-bridge-reconciliation-v2"
	maxResponseBytes    = 2 << 20
)

type Config struct {
	Version              string        `json:"version"`
	MSCAccountingURL     string        `json:"msc_accounting_url"`
	RequestTimeout       string        `json:"request_timeout,omitempty"`
	MaxAccountingAge     string        `json:"max_accounting_age,omitempty"`
	PollInterval         string        `json:"poll_interval,omitempty"`
	FailureThreshold     int           `json:"failure_threshold,omitempty"`
	AutoPause            bool          `json:"auto_pause,omitempty"`
	AdminSettingsURL     string        `json:"admin_settings_url,omitempty"`
	AdminTokenEnv        string        `json:"admin_token_env,omitempty"`
	ListenAddress        string        `json:"listen_address,omitempty"`
	AllowInsecureMSCHTTP bool          `json:"allow_insecure_msc_http,omitempty"`
	Routes               []RouteConfig `json:"routes"`
}

type RouteConfig struct {
	ChainType              string   `json:"chain_type,omitempty"`
	RouteID                string   `json:"route_id"`
	SourceChainID          string   `json:"source_chain_id"`
	ExpectedEVMChainID     uint64   `json:"expected_evm_chain_id"`
	ExpectedGenesisID      string   `json:"expected_genesis_block_id,omitempty"`
	ExpectedVaultCodeHash  string   `json:"expected_vault_runtime_code_hash,omitempty"`
	ExpectedTokenCodeHash  string   `json:"expected_token_runtime_code_hash,omitempty"`
	ExpectedRouteEnabled   bool     `json:"expected_route_enabled,omitempty"`
	ExpectedMinAmountRaw   string   `json:"expected_min_amount_raw,omitempty"`
	ExpectedMaxAmountRaw   string   `json:"expected_max_amount_raw,omitempty"`
	ExpectedDailyLockRaw   string   `json:"expected_daily_lock_limit_raw,omitempty"`
	ExpectedDailyUnlockRaw string   `json:"expected_daily_unlock_limit_raw,omitempty"`
	RPCURLs                []string `json:"rpc_urls"`
	RPCQuorum              int      `json:"rpc_quorum"`
	Confirmations          uint64   `json:"confirmations"`
	VaultAddress           string   `json:"vault_address"`
	TokenAddress           string   `json:"token_address"`
	LocalTokenID           string   `json:"local_token_id"`
	SourceDecimals         uint8    `json:"source_decimals"`
	AllowInsecureRPCHTTP   bool     `json:"allow_insecure_rpc_http,omitempty"`
}

type AccountingRoute struct {
	RouteID           string `json:"route_id"`
	ChainID           string `json:"chain_id"`
	ChainType         string `json:"chain_type"`
	AssetDenom        string `json:"asset_denom"`
	OriginAsset       string `json:"origin_asset"`
	VaultAddress      string `json:"vault_address"`
	ExecutionAdapter  string `json:"execution_adapter,omitempty"`
	RuntimeCodeHash   string `json:"runtime_code_hash,omitempty"`
	SourceDecimals    uint8  `json:"source_decimals"`
	Status            string `json:"status"`
	Ready             bool   `json:"ready"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

type AccountingAsset struct {
	LocalTokenID       string            `json:"local_token_id"`
	LocalDenom         string            `json:"local_denom"`
	Symbol             string            `json:"symbol"`
	Decimals           uint8             `json:"decimals"`
	TotalSupplyRaw     string            `json:"total_supply_raw"`
	MaxSupplyRaw       string            `json:"max_supply_raw"`
	Paused             bool              `json:"paused"`
	AuthorityThreshold uint16            `json:"authority_threshold"`
	Routes             []AccountingRoute `json:"routes"`
}

type AccountingSnapshot struct {
	Version         string            `json:"version"`
	BridgeVersion   string            `json:"bridge_version"`
	StateRoot       string            `json:"state_root"`
	MSCHeight       uint64            `json:"msc_height"`
	GeneratedAtUnix int64             `json:"generated_at_unix"`
	BridgePaused    bool              `json:"bridge_paused"`
	Assets          []AccountingAsset `json:"assets"`
}

type RouteReport struct {
	RouteID                  string   `json:"route_id"`
	SourceChainID            string   `json:"source_chain_id"`
	BlockHeight              uint64   `json:"block_height,omitempty"`
	BlockHash                string   `json:"block_hash,omitempty"`
	TrackedEscrowRaw         string   `json:"tracked_escrow_raw,omitempty"`
	VaultTokenBalanceRaw     string   `json:"vault_token_balance_raw,omitempty"`
	VaultBalanceDeficitRaw   string   `json:"vault_balance_deficit_raw,omitempty"`
	VaultPaused              bool     `json:"vault_paused"`
	TokenRouteEnabled        bool     `json:"token_route_enabled,omitempty"`
	TokenRouteMinAmountRaw   string   `json:"token_route_min_amount_raw,omitempty"`
	TokenRouteMaxAmountRaw   string   `json:"token_route_max_amount_raw,omitempty"`
	TokenRouteDailyLockRaw   string   `json:"token_route_daily_lock_limit_raw,omitempty"`
	TokenRouteDailyUnlockRaw string   `json:"token_route_daily_unlock_limit_raw,omitempty"`
	RPCAgreed                int      `json:"rpc_agreed"`
	RPCQueried               int      `json:"rpc_queried"`
	EndpointHashes           []string `json:"endpoint_hashes,omitempty"`
	Status                   string   `json:"status"`
	Reason                   string   `json:"reason,omitempty"`
}

type AssetReport struct {
	LocalTokenID       string        `json:"local_token_id"`
	Symbol             string        `json:"symbol"`
	Decimals           uint8         `json:"decimals"`
	WrappedSupplyRaw   string        `json:"wrapped_supply_raw"`
	MaxSupplyRaw       string        `json:"max_supply_raw"`
	TokenPaused        bool          `json:"token_paused"`
	AuthorityThreshold uint16        `json:"authority_threshold"`
	TrackedBackingRaw  string        `json:"tracked_backing_raw"`
	SurplusRaw         string        `json:"surplus_raw"`
	DeficitRaw         string        `json:"deficit_raw"`
	Status             string        `json:"status"`
	Reason             string        `json:"reason,omitempty"`
	Routes             []RouteReport `json:"routes"`
}

type Report struct {
	Version                 string        `json:"version"`
	CheckedAtUnix           int64         `json:"checked_at_unix"`
	MSCHeight               uint64        `json:"msc_height"`
	MSCStateRoot            string        `json:"msc_state_root"`
	MSCAccountingAgeSeconds int64         `json:"msc_accounting_age_seconds"`
	BridgePaused            bool          `json:"bridge_paused"`
	CriticalDeficit         bool          `json:"critical_deficit"`
	Healthy                 bool          `json:"healthy"`
	Assets                  []AssetReport `json:"assets"`
}

type Reconciler struct {
	config Config
	client *http.Client
	maxAge time.Duration
}

func New(config Config) (*Reconciler, error) {
	normalized, timeout, maxAge, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 4
	return &Reconciler{
		config: normalized,
		client: &http.Client{
			Timeout:       timeout,
			Transport:     transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("redirects are disabled") },
		},
		maxAge: maxAge,
	}, nil
}

func (r *Reconciler) Config() Config { return r.config }

func normalizeConfig(config Config) (Config, time.Duration, time.Duration, error) {
	config.Version = strings.TrimSpace(config.Version)
	if config.Version != ConfigVersion && config.Version != LegacyConfigVersion {
		return config, 0, 0, fmt.Errorf("version must be %s (or legacy %s for EVM-only configs)", ConfigVersion, LegacyConfigVersion)
	}
	mscURL, err := validateHTTPURL(config.MSCAccountingURL, config.AllowInsecureMSCHTTP)
	if err != nil {
		return config, 0, 0, fmt.Errorf("msc_accounting_url: %w", err)
	}
	parsedMSC, _ := url.Parse(mscURL)
	if parsedMSC.Path != "/bridge/accounting" && parsedMSC.Path != "/v1/bridge/accounting" {
		return config, 0, 0, errors.New("msc_accounting_url path must be /bridge/accounting or /v1/bridge/accounting")
	}
	config.MSCAccountingURL = mscURL
	timeout, err := parseDuration(config.RequestTimeout, 10*time.Second, time.Second, 60*time.Second, "request_timeout")
	if err != nil {
		return config, 0, 0, err
	}
	maxAge, err := parseDuration(config.MaxAccountingAge, 2*time.Minute, 5*time.Second, 15*time.Minute, "max_accounting_age")
	if err != nil {
		return config, 0, 0, err
	}
	if len(config.Routes) == 0 || len(config.Routes) > 64 {
		return config, 0, 0, errors.New("routes must contain between 1 and 64 entries")
	}
	seenRoutes := make(map[string]struct{}, len(config.Routes))
	for index := range config.Routes {
		route := &config.Routes[index]
		route.ChainType = strings.ToLower(strings.TrimSpace(route.ChainType))
		if route.ChainType == "" {
			route.ChainType = "evm"
		}
		route.RouteID = bridgeobserver.NormalizeRegistryID(route.RouteID)
		route.SourceChainID = bridgeobserver.NormalizeRegistryID(route.SourceChainID)
		route.LocalTokenID = strings.ToLower(strings.TrimSpace(route.LocalTokenID))
		if route.RouteID == "" || route.SourceChainID == "" || route.LocalTokenID == "" {
			return config, 0, 0, fmt.Errorf("routes[%d] has invalid required identity", index)
		}
		if config.Version == LegacyConfigVersion && route.ChainType != "evm" {
			return config, 0, 0, fmt.Errorf("routes[%d] requires %s for non-EVM reconciliation", index, ConfigVersion)
		}
		switch route.ChainType {
		case "evm":
			route.VaultAddress = normalizeAddress(route.VaultAddress)
			route.TokenAddress = normalizeAddress(route.TokenAddress)
			if route.ExpectedEVMChainID == 0 || route.VaultAddress == "" || route.TokenAddress == "" {
				return config, 0, 0, fmt.Errorf("routes[%d] has invalid EVM identity", index)
			}
		case "tron":
			route.VaultAddress = bridgeobserver.NormalizeTronAddress(route.VaultAddress)
			route.TokenAddress = bridgeobserver.NormalizeTronAddress(route.TokenAddress)
			route.ExpectedGenesisID = normalizeExternalHash(route.ExpectedGenesisID)
			route.ExpectedVaultCodeHash = normalizeRuntimeCodeHash(route.ExpectedVaultCodeHash)
			route.ExpectedTokenCodeHash = normalizeRuntimeCodeHash(route.ExpectedTokenCodeHash)
			if route.ExpectedEVMChainID != 0 || route.VaultAddress == "" || route.TokenAddress == "" ||
				route.ExpectedGenesisID == "" || route.ExpectedVaultCodeHash == "" || route.ExpectedTokenCodeHash == "" {
				return config, 0, 0, fmt.Errorf("routes[%d] requires canonical Tron addresses, genesis, and non-zero runtime code hashes", index)
			}
			minimum, minimumOK := parseRaw(route.ExpectedMinAmountRaw)
			maximum, maximumOK := parseRaw(route.ExpectedMaxAmountRaw)
			dailyLock, dailyLockOK := parseRaw(route.ExpectedDailyLockRaw)
			dailyUnlock, dailyUnlockOK := parseRaw(route.ExpectedDailyUnlockRaw)
			if !route.ExpectedRouteEnabled || !minimumOK || minimum.Sign() <= 0 || !maximumOK || !dailyLockOK || !dailyUnlockOK ||
				minimum.Cmp(maximum) > 0 || dailyLock.Cmp(maximum) < 0 || dailyUnlock.Cmp(maximum) < 0 {
				return config, 0, 0, fmt.Errorf("routes[%d] requires the exact enabled Tron token-route limits", index)
			}
			route.ExpectedMinAmountRaw = minimum.String()
			route.ExpectedMaxAmountRaw = maximum.String()
			route.ExpectedDailyLockRaw = dailyLock.String()
			route.ExpectedDailyUnlockRaw = dailyUnlock.String()
			if route.SourceChainID == bridgeobserver.TronMainnetChainID &&
				(route.RouteID != "usdt-tron-mainnet" || route.ExpectedGenesisID != bridgeobserver.TronMainnetGenesisBlockID ||
					route.TokenAddress != bridgeobserver.TronMainnetUSDTAddress || route.SourceDecimals != 6 ||
					route.Confirmations < bridgeobserver.TronMainnetConfirmations || route.AllowInsecureRPCHTTP ||
					len(route.RPCURLs) < 3) {
				return config, 0, 0, fmt.Errorf("routes[%d] must use the canonical TRON mainnet USDT route and at least three HTTPS providers", index)
			}
		default:
			return config, 0, 0, fmt.Errorf("routes[%d] chain_type must be evm or tron", index)
		}
		if route.VaultAddress == route.TokenAddress {
			return config, 0, 0, fmt.Errorf("routes[%d] vault and token addresses must differ", index)
		}
		if _, exists := seenRoutes[route.RouteID]; exists {
			return config, 0, 0, fmt.Errorf("routes[%d] duplicates route_id %s", index, route.RouteID)
		}
		seenRoutes[route.RouteID] = struct{}{}
		if route.Confirmations == 0 || route.SourceDecimals > 30 || len(route.RPCURLs) < 2 || route.RPCQuorum < 2 || route.RPCQuorum > len(route.RPCURLs) {
			return config, 0, 0, fmt.Errorf("routes[%d] requires confirmations, <=30 decimals, and RPC quorum >=2", index)
		}
		seenURLs := make(map[string]struct{}, len(route.RPCURLs))
		seenHosts := make(map[string]struct{}, len(route.RPCURLs))
		for rpcIndex, rawURL := range route.RPCURLs {
			var normalized, provider string
			var err error
			if route.ChainType == "tron" {
				normalized, provider, err = bridgeobserver.ValidateTronAPIURL(rawURL, route.AllowInsecureRPCHTTP)
			} else {
				normalized, err = bridgeobserver.ValidateEVMRPCURL(rawURL, route.AllowInsecureRPCHTTP)
				if err == nil {
					parsed, _ := url.Parse(normalized)
					provider = strings.ToLower(parsed.Host)
				}
			}
			if err != nil {
				return config, 0, 0, fmt.Errorf("routes[%d].rpc_urls[%d]: %w", index, rpcIndex, err)
			}
			if _, exists := seenURLs[normalized]; exists {
				return config, 0, 0, fmt.Errorf("routes[%d].rpc_urls[%d] duplicates an endpoint", index, rpcIndex)
			}
			if _, exists := seenHosts[provider]; exists {
				return config, 0, 0, fmt.Errorf("routes[%d].rpc_urls[%d] reuses provider host %s", index, rpcIndex, provider)
			}
			seenURLs[normalized] = struct{}{}
			seenHosts[provider] = struct{}{}
			route.RPCURLs[rpcIndex] = normalized
		}
	}
	return config, timeout, maxAge, nil
}

func validateHTTPURL(value string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" {
		host := strings.ToLower(parsed.Hostname())
		ip := net.ParseIP(host)
		local := host == "localhost" || (ip != nil && ip.IsLoopback())
		if parsed.Scheme != "http" || (!local && !allowInsecure) {
			return "", errors.New("must use HTTPS; HTTP is restricted to localhost unless explicitly enabled")
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func parseDuration(value string, fallback, minimum, maximum time.Duration, field string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", field, minimum, maximum)
	}
	return parsed, nil
}

func normalizeAddress(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return ""
	}
	if _, err := hex.DecodeString(value[2:]); err != nil || strings.Trim(value[2:], "0") == "" {
		return ""
	}
	return value
}

func normalizeExternalHash(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) != 64 || strings.Trim(value, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func normalizeRuntimeCodeHash(value string) string {
	value = normalizeExternalHash(value)
	if value == "" {
		return ""
	}
	return "0x" + value
}

func (r *Reconciler) fetchAccounting(ctx context.Context, now time.Time) (AccountingSnapshot, int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.config.MSCAccountingURL, nil)
	if err != nil {
		return AccountingSnapshot{}, 0, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "msc-bridge-reconciler/1")
	response, err := r.client.Do(request)
	if err != nil {
		return AccountingSnapshot{}, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return AccountingSnapshot{}, 0, fmt.Errorf("MSC accounting HTTP status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return AccountingSnapshot{}, 0, errors.New("MSC accounting response unreadable or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot AccountingSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return AccountingSnapshot{}, 0, fmt.Errorf("decode MSC accounting: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return AccountingSnapshot{}, 0, errors.New("MSC accounting response must contain exactly one JSON object")
	}
	age := now.Unix() - snapshot.GeneratedAtUnix
	if err := validateAccounting(snapshot, now, r.maxAge); err != nil {
		return AccountingSnapshot{}, age, err
	}
	return snapshot, age, nil
}

func validateAccounting(snapshot AccountingSnapshot, now time.Time, maxAge time.Duration) error {
	if snapshot.Version != AccountingVersion || strings.TrimSpace(snapshot.BridgeVersion) == "" || snapshot.MSCHeight == 0 {
		return errors.New("MSC accounting version, bridge version, or height invalid")
	}
	stateRoot := strings.ToLower(strings.TrimSpace(snapshot.StateRoot))
	if len(stateRoot) != 64 {
		return errors.New("MSC accounting state root invalid")
	}
	if _, err := hex.DecodeString(stateRoot); err != nil {
		return errors.New("MSC accounting state root invalid")
	}
	generated := time.Unix(snapshot.GeneratedAtUnix, 0)
	if snapshot.GeneratedAtUnix <= 0 || generated.After(now.Add(30*time.Second)) || generated.Before(now.Add(-maxAge)) {
		return errors.New("MSC accounting snapshot is stale or from the future")
	}
	seenAssets := make(map[string]struct{}, len(snapshot.Assets))
	seenRoutes := make(map[string]struct{})
	for index, asset := range snapshot.Assets {
		assetID := strings.ToLower(strings.TrimSpace(asset.LocalTokenID))
		if assetID == "" || asset.Decimals > 30 || asset.AuthorityThreshold == 0 {
			return fmt.Errorf("MSC accounting asset %d invalid", index)
		}
		if _, exists := seenAssets[assetID]; exists {
			return fmt.Errorf("MSC accounting duplicates local token %s", assetID)
		}
		seenAssets[assetID] = struct{}{}
		if _, ok := parseRaw(asset.TotalSupplyRaw); !ok {
			return fmt.Errorf("MSC accounting asset %s has invalid total supply", assetID)
		}
		if _, ok := parseRaw(asset.MaxSupplyRaw); !ok {
			return fmt.Errorf("MSC accounting asset %s has invalid max supply", assetID)
		}
		for _, route := range asset.Routes {
			routeID := bridgeobserver.NormalizeRegistryID(route.RouteID)
			chainType := strings.ToLower(strings.TrimSpace(route.ChainType))
			validAddresses := false
			switch chainType {
			case "evm":
				validAddresses = normalizeAddress(route.OriginAsset) != "" && normalizeAddress(route.VaultAddress) != ""
			case "tron":
				validAddresses = bridgeobserver.NormalizeTronAddress(route.OriginAsset) != "" && bridgeobserver.NormalizeTronAddress(route.VaultAddress) != ""
			}
			if routeID == "" || bridgeobserver.NormalizeRegistryID(route.ChainID) == "" || !validAddresses || route.SourceDecimals > 30 {
				return fmt.Errorf("MSC accounting route %q invalid", route.RouteID)
			}
			if route.RuntimeCodeHash != "" && normalizeRuntimeCodeHash(route.RuntimeCodeHash) == "" {
				return fmt.Errorf("MSC accounting route %q has invalid runtime code hash", route.RouteID)
			}
			if _, exists := seenRoutes[routeID]; exists {
				return fmt.Errorf("MSC accounting duplicates route %s", routeID)
			}
			seenRoutes[routeID] = struct{}{}
		}
	}
	return nil
}

func parseRaw(value string) (*big.Int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || (len(value) > 1 && value[0] == '0') || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return nil, false
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	return parsed, ok && parsed.Sign() >= 0
}

func convertDecimals(value *big.Int, from, to uint8) (*big.Int, error) {
	converted := new(big.Int).Set(value)
	if from == to {
		return converted, nil
	}
	difference := int(from) - int(to)
	if difference < 0 {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-difference)), nil)
		return converted.Mul(converted, factor), nil
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(difference)), nil)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(converted, factor, remainder)
	if remainder.Sign() != 0 {
		return nil, errors.New("escrow amount cannot be represented exactly in local token decimals")
	}
	return quotient, nil
}

func routeAddressEqual(chainType, left, right string) bool {
	if chainType == "tron" {
		return bridgeobserver.NormalizeTronAddress(left) != "" &&
			bridgeobserver.NormalizeTronAddress(left) == bridgeobserver.NormalizeTronAddress(right)
	}
	return normalizeAddress(left) != "" && normalizeAddress(left) == normalizeAddress(right)
}

func routeMetadataMatches(config RouteConfig, accounting AccountingRoute) bool {
	if bridgeobserver.NormalizeRegistryID(accounting.ChainID) != config.SourceChainID ||
		strings.ToLower(strings.TrimSpace(accounting.ChainType)) != config.ChainType ||
		!routeAddressEqual(config.ChainType, accounting.OriginAsset, config.TokenAddress) ||
		!routeAddressEqual(config.ChainType, accounting.VaultAddress, config.VaultAddress) ||
		accounting.SourceDecimals != config.SourceDecimals {
		return false
	}
	if config.ChainType == "tron" {
		if strings.ToLower(strings.TrimSpace(accounting.ExecutionAdapter)) != "tron_vault_v1" ||
			normalizeRuntimeCodeHash(accounting.RuntimeCodeHash) != config.ExpectedVaultCodeHash {
			return false
		}
		if config.SourceChainID == bridgeobserver.TronMainnetChainID && strings.TrimSpace(accounting.AssetDenom) != "USDT-TRON" {
			return false
		}
	}
	return true
}

func (r *Reconciler) Check(ctx context.Context) (Report, error) {
	now := time.Now().UTC()
	snapshot, age, err := r.fetchAccounting(ctx, now)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Version:                 ReportVersion,
		CheckedAtUnix:           now.Unix(),
		MSCHeight:               snapshot.MSCHeight,
		MSCStateRoot:            snapshot.StateRoot,
		MSCAccountingAgeSeconds: age,
		BridgePaused:            snapshot.BridgePaused,
		Healthy:                 true,
	}
	configsByToken := make(map[string][]RouteConfig)
	for _, route := range r.config.Routes {
		configsByToken[route.LocalTokenID] = append(configsByToken[route.LocalTokenID], route)
	}
	for _, asset := range snapshot.Assets {
		localID := strings.ToLower(strings.TrimSpace(asset.LocalTokenID))
		configs := configsByToken[localID]
		if len(configs) == 0 {
			continue
		}
		assetReport := AssetReport{
			LocalTokenID:       localID,
			Symbol:             asset.Symbol,
			Decimals:           asset.Decimals,
			WrappedSupplyRaw:   asset.TotalSupplyRaw,
			MaxSupplyRaw:       asset.MaxSupplyRaw,
			TokenPaused:        asset.Paused,
			AuthorityThreshold: asset.AuthorityThreshold,
			TrackedBackingRaw:  "0",
			SurplusRaw:         "0",
			DeficitRaw:         "0",
			Status:             "healthy",
		}
		accountingRoutes := make(map[string]AccountingRoute, len(asset.Routes))
		for _, route := range asset.Routes {
			accountingRoutes[bridgeobserver.NormalizeRegistryID(route.RouteID)] = route
		}
		if len(configs) != len(accountingRoutes) {
			assetReport.Status = "unknown"
			assetReport.Reason = "configured routes do not exactly cover every MSC accounting route for this wrapped token"
		}
		backing := new(big.Int)
		for _, config := range configs {
			routeReport := RouteReport{RouteID: config.RouteID, SourceChainID: config.SourceChainID, Status: "unknown", RPCQueried: len(config.RPCURLs)}
			accountingRoute, found := accountingRoutes[config.RouteID]
			if !found || !routeMetadataMatches(config, accountingRoute) {
				routeReport.Reason = "route configuration does not match MSC accounting metadata"
				assetReport.Status = "unknown"
				assetReport.Routes = append(assetReport.Routes, routeReport)
				continue
			}
			var probe evmProbeResult
			var probeErr error
			if config.ChainType == "tron" {
				probe, probeErr = r.probeTronRoute(ctx, config)
			} else {
				probe, probeErr = r.probeEVMRoute(ctx, config)
			}
			if probeErr != nil {
				routeReport.Reason = probeErr.Error()
				assetReport.Status = "unknown"
				assetReport.Routes = append(assetReport.Routes, routeReport)
				continue
			}
			routeReport.BlockHeight = probe.BlockHeight
			routeReport.BlockHash = probe.BlockHash
			routeReport.TrackedEscrowRaw = probe.TrackedEscrow.String()
			routeReport.VaultTokenBalanceRaw = probe.TokenBalance.String()
			routeReport.VaultPaused = probe.Paused
			routeReport.TokenRouteEnabled = probe.RouteEnabled
			if probe.RouteMinAmount != nil {
				routeReport.TokenRouteMinAmountRaw = probe.RouteMinAmount.String()
				routeReport.TokenRouteMaxAmountRaw = probe.RouteMaxAmount.String()
				routeReport.TokenRouteDailyLockRaw = probe.RouteDailyLock.String()
				routeReport.TokenRouteDailyUnlockRaw = probe.RouteDailyUnlock.String()
			}
			routeReport.RPCAgreed = probe.Agreed
			routeReport.EndpointHashes = probe.EndpointHashes
			routeReport.VaultBalanceDeficitRaw = "0"
			if probe.TokenBalance.Cmp(probe.TrackedEscrow) < 0 {
				deficit := new(big.Int).Sub(new(big.Int).Set(probe.TrackedEscrow), probe.TokenBalance)
				routeReport.VaultBalanceDeficitRaw = deficit.String()
				routeReport.Status = "deficit"
				routeReport.Reason = "vault token balance is below contract-tracked escrow"
				assetReport.Status = "deficit"
				report.CriticalDeficit = true
			} else {
				routeReport.Status = "healthy"
			}
			converted, conversionErr := convertDecimals(probe.TrackedEscrow, config.SourceDecimals, asset.Decimals)
			if conversionErr != nil {
				routeReport.Status = "unknown"
				routeReport.Reason = conversionErr.Error()
				if assetReport.Status != "deficit" {
					assetReport.Status = "unknown"
				}
			} else {
				backing.Add(backing, converted)
			}
			assetReport.Routes = append(assetReport.Routes, routeReport)
		}
		sort.Slice(assetReport.Routes, func(i, j int) bool { return assetReport.Routes[i].RouteID < assetReport.Routes[j].RouteID })
		assetReport.TrackedBackingRaw = backing.String()
		liability, _ := parseRaw(asset.TotalSupplyRaw)
		if assetReport.Status != "unknown" {
			switch backing.Cmp(liability) {
			case -1:
				assetReport.Status = "deficit"
				assetReport.DeficitRaw = new(big.Int).Sub(liability, backing).String()
				assetReport.Reason = "aggregate tracked source escrow is below wrapped token supply"
				report.CriticalDeficit = true
			case 0:
				if assetReport.Status != "deficit" {
					assetReport.Status = "healthy"
				}
			default:
				assetReport.SurplusRaw = new(big.Int).Sub(backing, liability).String()
				if assetReport.Status != "deficit" {
					assetReport.Status = "healthy"
				}
			}
		}
		if assetReport.Status != "healthy" {
			report.Healthy = false
		}
		report.Assets = append(report.Assets, assetReport)
		delete(configsByToken, localID)
	}
	for localID := range configsByToken {
		report.Assets = append(report.Assets, AssetReport{LocalTokenID: localID, Status: "unknown", Reason: "configured local token is absent from MSC accounting snapshot", WrappedSupplyRaw: "0", TrackedBackingRaw: "0", SurplusRaw: "0", DeficitRaw: "0"})
		report.Healthy = false
	}
	sort.Slice(report.Assets, func(i, j int) bool { return report.Assets[i].LocalTokenID < report.Assets[j].LocalTokenID })
	if len(report.Assets) == 0 {
		return Report{}, errors.New("MSC accounting snapshot contains none of the configured wrapped tokens")
	}
	return report, nil
}
