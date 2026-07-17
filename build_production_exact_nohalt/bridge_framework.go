package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	BridgeProtocolVersion = "msc-bridge-v1"

	BridgeModeDisabled         = "disabled"
	BridgeModeVerificationOnly = "verification_only"
	BridgeModeGuardedMint      = "guarded_mint"

	BridgeTrustLightClient  = "light_client"
	BridgeTrustOracleQuorum = "oracle_quorum"
	BridgeTrustHybrid       = "hybrid"
)

type BridgeConfig struct {
	Enabled               *bool      `toml:"enabled"`
	Mode                  string     `toml:"mode"`
	IBCStyleEnabled       *bool      `toml:"ibc_style_enabled"`
	LightClientRequired   *bool      `toml:"light_client_required"`
	RequiredConfirmations *uint64    `toml:"required_confirmations"`
	OracleQuorum          *uint16    `toml:"oracle_quorum"`
	Chains                StringList `toml:"chains"`
	Assets                StringList `toml:"assets"`
}

type BridgeChainConfig struct {
	ChainID          string `json:"chain_id"`
	Name             string `json:"name"`
	ChainType        string `json:"chain_type"`
	TrustModel       string `json:"trust_model"`
	MinConfirmations uint64 `json:"min_confirmations"`
	LightClient      string `json:"light_client,omitempty"`
}

type BridgeAssetConfig struct {
	Denom        string `json:"denom"`
	OriginChain  string `json:"origin_chain"`
	OriginAsset  string `json:"origin_asset"`
	LocalDenom   string `json:"local_denom"`
	Decimals     uint8  `json:"decimals"`
	EscrowPolicy string `json:"escrow_policy"`
}

type BridgeOracleSignature struct {
	Signer    string `json:"signer"`
	PublicKey string `json:"public_key,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type BridgeProof struct {
	Version             string                  `json:"version,omitempty"`
	SourceChainID       string                  `json:"source_chain_id"`
	EventID             string                  `json:"event_id"`
	AssetDenom          string                  `json:"asset_denom"`
	OriginAsset         string                  `json:"origin_asset,omitempty"`
	Recipient           string                  `json:"recipient,omitempty"`
	Amount              string                  `json:"amount,omitempty"`
	PayloadHash         string                  `json:"payload_hash,omitempty"`
	SourceHeight        uint64                  `json:"source_height"`
	ConfirmedHeight     uint64                  `json:"confirmed_height,omitempty"`
	SourceBlockHash     string                  `json:"source_block_hash,omitempty"`
	HeaderChain         []LightHeader           `json:"header_chain,omitempty"`
	LightClientHeader   *LightHeader            `json:"light_client_header,omitempty"`
	MerkleProof         *LightMerkleProof       `json:"merkle_proof,omitempty"`
	OracleSignatures    []BridgeOracleSignature `json:"oracle_signatures,omitempty"`
	ReplayProtectionKey string                  `json:"replay_protection_key,omitempty"`
}

type BridgeVerificationResult struct {
	Version               string   `json:"version"`
	Accepted              bool     `json:"accepted"`
	Status                string   `json:"status"`
	Reason                string   `json:"reason,omitempty"`
	Mode                  string   `json:"mode"`
	Verification          string   `json:"verification"`
	SourceChainID         string   `json:"source_chain_id,omitempty"`
	AssetDenom            string   `json:"asset_denom,omitempty"`
	RequiredConfirmations uint64   `json:"required_confirmations"`
	ObservedConfirmations uint64   `json:"observed_confirmations"`
	OracleSigners         int      `json:"oracle_signers,omitempty"`
	OracleQuorum          uint16   `json:"oracle_quorum,omitempty"`
	ReplayProtectionKey   string   `json:"replay_protection_key,omitempty"`
	Warnings              []string `json:"warnings,omitempty"`
}

type BridgeStatus struct {
	Version               string              `json:"version"`
	Enabled               bool                `json:"enabled"`
	Mode                  string              `json:"mode"`
	IBCStyleEnabled       bool                `json:"ibc_style_enabled"`
	LightClientRequired   bool                `json:"light_client_required"`
	RequiredConfirmations uint64              `json:"required_confirmations"`
	OracleQuorum          uint16              `json:"oracle_quorum"`
	RegisteredChains      []BridgeChainConfig `json:"registered_chains"`
	RegisteredAssets      []BridgeAssetConfig `json:"registered_assets"`
	Safety                []string            `json:"safety"`
}

var (
	BridgeEnabled               = false
	BridgeMode                  = BridgeModeDisabled
	BridgeIBCStyleEnabled       = false
	BridgeLightClientRequired   = true
	BridgeRequiredConfirmations = uint64(64)
	BridgeOracleQuorum          = uint16(3)
	BridgeChains                []BridgeChainConfig
	BridgeAssets                []BridgeAssetConfig
)

func normalizeBridgeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", BridgeModeDisabled:
		return BridgeModeDisabled
	case BridgeModeVerificationOnly, "verify", "verification":
		return BridgeModeVerificationOnly
	case BridgeModeGuardedMint, "mint":
		return BridgeModeGuardedMint
	default:
		return BridgeModeDisabled
	}
}

func normalizeBridgeTrustModel(trust string) string {
	switch strings.ToLower(strings.TrimSpace(trust)) {
	case BridgeTrustLightClient, "spv":
		return BridgeTrustLightClient
	case BridgeTrustOracleQuorum, "oracle", "multisig":
		return BridgeTrustOracleQuorum
	case BridgeTrustHybrid:
		return BridgeTrustHybrid
	default:
		return BridgeTrustLightClient
	}
}

func parseBridgeChainConfig(raw string) (BridgeChainConfig, error) {
	parts := strings.Split(raw, "|")
	if len(parts) < 3 {
		return BridgeChainConfig{}, fmt.Errorf("bridge chain entry must be chain_id|name|chain_type|trust_model|min_confirmations|light_client")
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	minConfirmations := BridgeRequiredConfirmations
	if len(parts) >= 5 && parts[4] != "" {
		v, err := strconv.ParseUint(parts[4], 10, 64)
		if err != nil {
			return BridgeChainConfig{}, fmt.Errorf("invalid bridge chain confirmations %q: %w", parts[4], err)
		}
		minConfirmations = v
	}
	trustModel := BridgeTrustLightClient
	if len(parts) >= 4 && parts[3] != "" {
		trustModel = normalizeBridgeTrustModel(parts[3])
	}
	lightClient := ""
	if len(parts) >= 6 {
		lightClient = parts[5]
	}
	cfg := BridgeChainConfig{
		ChainID:          parts[0],
		Name:             parts[1],
		ChainType:        parts[2],
		TrustModel:       trustModel,
		MinConfirmations: minConfirmations,
		LightClient:      lightClient,
	}
	if cfg.ChainID == "" || cfg.Name == "" || cfg.ChainType == "" {
		return BridgeChainConfig{}, fmt.Errorf("bridge chain entry has empty required fields")
	}
	return cfg, nil
}

func parseBridgeAssetConfig(raw string) (BridgeAssetConfig, error) {
	parts := strings.Split(raw, "|")
	if len(parts) < 4 {
		return BridgeAssetConfig{}, fmt.Errorf("bridge asset entry must be denom|origin_chain|origin_asset|local_denom|decimals|escrow_policy")
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	decimals := uint64(18)
	if len(parts) >= 5 && parts[4] != "" {
		v, err := strconv.ParseUint(parts[4], 10, 8)
		if err != nil {
			return BridgeAssetConfig{}, fmt.Errorf("invalid bridge asset decimals %q: %w", parts[4], err)
		}
		decimals = v
	}
	escrowPolicy := "locked_escrow_or_verified_burn"
	if len(parts) >= 6 && parts[5] != "" {
		escrowPolicy = parts[5]
	}
	cfg := BridgeAssetConfig{
		Denom:        parts[0],
		OriginChain:  parts[1],
		OriginAsset:  parts[2],
		LocalDenom:   parts[3],
		Decimals:     uint8(decimals),
		EscrowPolicy: escrowPolicy,
	}
	if cfg.Denom == "" || cfg.OriginChain == "" || cfg.OriginAsset == "" || cfg.LocalDenom == "" {
		return BridgeAssetConfig{}, fmt.Errorf("bridge asset entry has empty required fields")
	}
	return cfg, nil
}

func applyBridgeConfig(cfg BridgeConfig) bool {
	changed := false
	if cfg.Enabled != nil {
		BridgeEnabled = *cfg.Enabled
		changed = true
	}
	if cfg.Mode != "" {
		BridgeMode = normalizeBridgeMode(cfg.Mode)
		changed = true
	}
	if cfg.IBCStyleEnabled != nil {
		BridgeIBCStyleEnabled = *cfg.IBCStyleEnabled
		changed = true
	}
	if cfg.LightClientRequired != nil {
		BridgeLightClientRequired = *cfg.LightClientRequired
		changed = true
	}
	if cfg.RequiredConfirmations != nil && *cfg.RequiredConfirmations > 0 {
		BridgeRequiredConfirmations = *cfg.RequiredConfirmations
		changed = true
	}
	if cfg.OracleQuorum != nil && *cfg.OracleQuorum > 0 {
		BridgeOracleQuorum = *cfg.OracleQuorum
		changed = true
	}
	if len(cfg.Chains) > 0 {
		chains := make([]BridgeChainConfig, 0, len(cfg.Chains))
		for _, raw := range cfg.Chains {
			parsed, err := parseBridgeChainConfig(raw)
			if err != nil {
				fmt.Printf("WARN bridge.chain ignored: %v entry=%q\n", err, raw)
				continue
			}
			chains = append(chains, parsed)
		}
		BridgeChains = chains
		changed = true
	}
	if len(cfg.Assets) > 0 {
		assets := make([]BridgeAssetConfig, 0, len(cfg.Assets))
		for _, raw := range cfg.Assets {
			parsed, err := parseBridgeAssetConfig(raw)
			if err != nil {
				fmt.Printf("WARN bridge.asset ignored: %v entry=%q\n", err, raw)
				continue
			}
			assets = append(assets, parsed)
		}
		BridgeAssets = assets
		changed = true
	}
	if !BridgeEnabled {
		BridgeMode = BridgeModeDisabled
	}
	if BridgeMode == BridgeModeGuardedMint {
		fmt.Printf("WARN bridge.mode=guarded_mint configured; execution path remains proof-gated and should be governance/audit activated only\n")
	}
	return changed
}

func bridgeChainRegistry() map[string]BridgeChainConfig {
	out := make(map[string]BridgeChainConfig, len(BridgeChains))
	for _, chain := range BridgeChains {
		out[strings.ToLower(strings.TrimSpace(chain.ChainID))] = chain
	}
	return out
}

func bridgeAssetRegistry() map[string]BridgeAssetConfig {
	out := make(map[string]BridgeAssetConfig, len(BridgeAssets))
	for _, asset := range BridgeAssets {
		out[strings.ToLower(strings.TrimSpace(asset.Denom))] = asset
	}
	return out
}

func bridgeObservedConfirmations(proof BridgeProof) uint64 {
	if proof.ConfirmedHeight == 0 || proof.SourceHeight == 0 || proof.ConfirmedHeight < proof.SourceHeight {
		return 0
	}
	return proof.ConfirmedHeight - proof.SourceHeight + 1
}

func bridgeReplayKey(proof BridgeProof) string {
	if key := strings.TrimSpace(proof.ReplayProtectionKey); key != "" {
		return key
	}
	source := strings.TrimSpace(proof.SourceChainID)
	event := strings.TrimSpace(proof.EventID)
	if source == "" || event == "" {
		return ""
	}
	return source + ":" + event
}

func bridgeUniqueOracleSigners(signatures []BridgeOracleSignature) int {
	seen := map[string]struct{}{}
	for _, sig := range signatures {
		signer := strings.ToLower(strings.TrimSpace(sig.Signer))
		if signer == "" {
			signer = strings.ToLower(strings.TrimSpace(sig.PublicKey))
		}
		if signer == "" {
			continue
		}
		seen[signer] = struct{}{}
	}
	return len(seen)
}

func bridgeFailure(proof BridgeProof, mode string, required, observed uint64, reason string) BridgeVerificationResult {
	return BridgeVerificationResult{
		Version:               BridgeProtocolVersion,
		Accepted:              false,
		Status:                "rejected",
		Reason:                reason,
		Mode:                  mode,
		Verification:          "none",
		SourceChainID:         strings.TrimSpace(proof.SourceChainID),
		AssetDenom:            strings.TrimSpace(proof.AssetDenom),
		RequiredConfirmations: required,
		ObservedConfirmations: observed,
		OracleQuorum:          BridgeOracleQuorum,
		ReplayProtectionKey:   bridgeReplayKey(proof),
	}
}

func VerifyBridgeProof(proof BridgeProof) BridgeVerificationResult {
	mode := normalizeBridgeMode(BridgeMode)
	required := BridgeRequiredConfirmations
	observed := bridgeObservedConfirmations(proof)
	if !BridgeEnabled || mode == BridgeModeDisabled {
		return bridgeFailure(proof, mode, required, observed, "bridge_disabled")
	}
	if strings.TrimSpace(proof.SourceChainID) == "" {
		return bridgeFailure(proof, mode, required, observed, "missing_source_chain_id")
	}
	if strings.TrimSpace(proof.EventID) == "" {
		return bridgeFailure(proof, mode, required, observed, "missing_event_id")
	}
	if bridgeReplayKey(proof) == "" {
		return bridgeFailure(proof, mode, required, observed, "missing_replay_protection_key")
	}
	chains := bridgeChainRegistry()
	chain, ok := chains[strings.ToLower(strings.TrimSpace(proof.SourceChainID))]
	if !ok {
		return bridgeFailure(proof, mode, required, observed, "source_chain_not_registered")
	}
	if chain.MinConfirmations > required {
		required = chain.MinConfirmations
	}
	if observed < required {
		return bridgeFailure(proof, mode, required, observed, "insufficient_confirmations")
	}
	assets := bridgeAssetRegistry()
	asset, ok := assets[strings.ToLower(strings.TrimSpace(proof.AssetDenom))]
	if !ok {
		return bridgeFailure(proof, mode, required, observed, "asset_not_registered")
	}
	if !strings.EqualFold(strings.TrimSpace(asset.OriginChain), strings.TrimSpace(proof.SourceChainID)) {
		return bridgeFailure(proof, mode, required, observed, "asset_origin_chain_mismatch")
	}
	if proof.OriginAsset != "" && !strings.EqualFold(strings.TrimSpace(asset.OriginAsset), strings.TrimSpace(proof.OriginAsset)) {
		return bridgeFailure(proof, mode, required, observed, "asset_origin_asset_mismatch")
	}

	trustModel := normalizeBridgeTrustModel(chain.TrustModel)
	warnings := []string{}
	verification := "syntax_and_quorum_only"
	if len(proof.HeaderChain) > 0 {
		if err := VerifyLightHeaderChain(proof.HeaderChain); err != nil {
			return bridgeFailure(proof, mode, required, observed, "invalid_light_header_chain: "+err.Error())
		}
		verification = "light_header_chain"
	}
	if proof.LightClientHeader != nil && proof.MerkleProof != nil {
		proofRoot := strings.TrimSpace(proof.MerkleProof.Root)
		headerRootMatch := proofRoot != "" && (strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.StateMerkleRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.StateRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.TxRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.ReceiptRoot)))
		if !headerRootMatch {
			return bridgeFailure(proof, mode, required, observed, "proof_root_not_in_light_header")
		}
		if VerifyLightMerkleProof(*proof.MerkleProof) {
			verification = "light_client_merkle_proof"
		} else {
			return bridgeFailure(proof, mode, required, observed, "invalid_merkle_proof")
		}
	} else if BridgeLightClientRequired || trustModel == BridgeTrustLightClient || trustModel == BridgeTrustHybrid {
		return bridgeFailure(proof, mode, required, observed, "light_client_proof_required")
	}

	oracleSigners := bridgeUniqueOracleSigners(proof.OracleSignatures)
	if trustModel == BridgeTrustOracleQuorum || trustModel == BridgeTrustHybrid {
		if oracleSigners < int(BridgeOracleQuorum) {
			return bridgeFailure(proof, mode, required, observed, "oracle_quorum_not_met")
		}
		if verification == "syntax_and_quorum_only" {
			verification = "oracle_quorum_syntax"
			warnings = append(warnings, "oracle signature cryptographic verification is external to this scaffold")
		}
	}
	if mode == BridgeModeGuardedMint {
		warnings = append(warnings, "guarded_mint mode configured, but this verifier does not execute mint/unlock")
	}

	return BridgeVerificationResult{
		Version:               BridgeProtocolVersion,
		Accepted:              true,
		Status:                "verified",
		Mode:                  mode,
		Verification:          verification,
		SourceChainID:         strings.TrimSpace(proof.SourceChainID),
		AssetDenom:            strings.TrimSpace(proof.AssetDenom),
		RequiredConfirmations: required,
		ObservedConfirmations: observed,
		OracleSigners:         oracleSigners,
		OracleQuorum:          BridgeOracleQuorum,
		ReplayProtectionKey:   bridgeReplayKey(proof),
		Warnings:              warnings,
	}
}

func BridgeStatusSnapshot() BridgeStatus {
	chains := append([]BridgeChainConfig(nil), BridgeChains...)
	sort.Slice(chains, func(i, j int) bool {
		return chains[i].ChainID < chains[j].ChainID
	})
	assets := append([]BridgeAssetConfig(nil), BridgeAssets...)
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Denom < assets[j].Denom
	})
	safety := []string{
		"bridge execution is disabled unless explicitly enabled by config and governance",
		"verification endpoint never mints or unlocks assets",
		"light-client/SPV proof is required by default",
		"asset mapping requires an explicit registered origin chain and origin asset",
		"replay protection key must bind source chain and event id",
	}
	return BridgeStatus{
		Version:               BridgeProtocolVersion,
		Enabled:               BridgeEnabled,
		Mode:                  normalizeBridgeMode(BridgeMode),
		IBCStyleEnabled:       BridgeIBCStyleEnabled,
		LightClientRequired:   BridgeLightClientRequired,
		RequiredConfirmations: BridgeRequiredConfirmations,
		OracleQuorum:          BridgeOracleQuorum,
		RegisteredChains:      chains,
		RegisteredAssets:      assets,
		Safety:                safety,
	}
}

func (s *Server) bridgeStatusResponse() (BridgeStatus, int, string) {
	if s == nil || s.Node == nil {
		return BridgeStatus{}, http.StatusServiceUnavailable, "node unavailable"
	}
	return BridgeStatusSnapshot(), http.StatusOK, ""
}

func (s *Server) handleBridgeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	resp, status, errMsg := s.bridgeStatusResponse()
	if status != http.StatusOK {
		http.Error(w, errMsg, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) handleV1BridgeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	resp, status, errMsg := s.bridgeStatusResponse()
	if status != http.StatusOK {
		writeV1Error(w, status, "", errMsg)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeV1Data(w, http.StatusOK, resp)
}

func (s *Server) handleBridgeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var proof BridgeProof
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ConfigRPCMaxRequestBodyBytes)).Decode(&proof); err != nil {
		http.Error(w, "invalid bridge proof JSON", http.StatusBadRequest)
		return
	}
	result := VerifyBridgeProof(proof)
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if !result.Accepted {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleV1BridgeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	var proof BridgeProof
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ConfigRPCMaxRequestBodyBytes)).Decode(&proof); err != nil {
		writeV1Error(w, http.StatusBadRequest, "", "invalid bridge proof JSON")
		return
	}
	result := VerifyBridgeProof(proof)
	if !result.Accepted {
		writeV1Data(w, http.StatusBadRequest, result)
		return
	}
	writeV1Data(w, http.StatusOK, result)
}
