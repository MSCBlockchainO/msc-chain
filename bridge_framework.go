package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	// `BridgeProtocolVersion` defines the constant value used by this package.
	BridgeProtocolVersion   = "msc-bridge-v5"
	BridgeCheckpointVersion = "msc-bridge-checkpoint-v2"

	// `BridgeModeDisabled` defines the constant value used by this package.
	BridgeModeDisabled = "disabled"
	// `BridgeModeVerificationOnly` defines the constant value used by this package.
	BridgeModeVerificationOnly = "verification_only"
	// `BridgeModeGuardedMint` defines the constant value used by this package.
	BridgeModeGuardedMint = "guarded_mint"

	// `BridgeTrustLightClient` defines the constant value used by this package.
	BridgeTrustLightClient = "light_client"
	// `BridgeTrustOracleQuorum` defines the constant value used by this package.
	BridgeTrustOracleQuorum = "oracle_quorum"
	// `BridgeTrustHybrid` defines the constant value used by this package.
	BridgeTrustHybrid = "hybrid"

	BridgeRouteStatusSetupRequired = "setup_required"
	BridgeRouteStatusTesting       = "testing"
	BridgeRouteStatusActive        = "active"
	BridgeRouteStatusPaused        = "paused"
	BridgeRouteStatusDisabled      = "disabled"

	BridgeExecutionEVMVaultV1      = "evm_vault_v1"
	BridgeExecutionTronVaultV1     = "tron_vault_v1"
	BridgeExecutionSolanaProgramV1 = "solana_program_v1"
)

type BridgeConfig struct {
	// `Enabled` stores whether the related condition is satisfied.
	Enabled *bool `toml:"enabled"`
	// `Mode` stores the value associated with this record.
	Mode string `toml:"mode"`
	// `IBCStyleEnabled` stores whether the related condition is satisfied.
	IBCStyleEnabled *bool `toml:"ibc_style_enabled"`
	// `LightClientRequired` stores the value associated with this record.
	LightClientRequired *bool `toml:"light_client_required"`
	// `RequiredConfirmations` stores the request data being processed.
	RequiredConfirmations *uint64 `toml:"required_confirmations"`
	// `OracleQuorum` stores the value associated with this record.
	OracleQuorum *uint16 `toml:"oracle_quorum"`
	// `Chains` stores the value associated with this record.
	Chains StringList `toml:"chains"`
	// `Assets` stores the value associated with this record.
	Assets StringList `toml:"assets"`
}

type BridgeChainConfig struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `ChainType` stores the value associated with this record.
	ChainType string `json:"chain_type"`
	// `TrustModel` stores the value associated with this record.
	TrustModel string `json:"trust_model"`
	// `MinConfirmations` stores the value associated with this record.
	MinConfirmations uint64 `json:"min_confirmations"`
	// `LightClient` stores the value associated with this record.
	LightClient string `json:"light_client,omitempty"`
	// Status is setup_required, testing, active, paused, or disabled.
	Status string `json:"status,omitempty"`
	// FinalityStatus reports whether the external finality tracker is usable.
	FinalityStatus string `json:"finality_status,omitempty"`
	// NativeSymbol is the source chain gas token symbol.
	NativeSymbol string `json:"native_symbol,omitempty"`
	// ExplorerURL is a public explorer base URL; private RPC URLs are never exposed.
	ExplorerURL string `json:"explorer_url,omitempty"`
	// LatestObservedHeight is the latest source height observed by bridge infrastructure.
	LatestObservedHeight uint64 `json:"latest_observed_height,omitempty"`
	// LatestFinalizedHeight is the latest source height considered final.
	LatestFinalizedHeight uint64 `json:"latest_finalized_height,omitempty"`
}

type BridgeAssetConfig struct {
	// `Denom` stores the value associated with this record.
	Denom string `json:"denom"`
	// `OriginChain` stores the value associated with this record.
	OriginChain string `json:"origin_chain"`
	// `OriginAsset` stores the value associated with this record.
	OriginAsset string `json:"origin_asset"`
	// `LocalDenom` stores the value associated with this record.
	LocalDenom string `json:"local_denom"`
	// `Decimals` stores the value associated with this record.
	Decimals uint8 `json:"decimals"`
	// `EscrowPolicy` stores the value associated with this record.
	EscrowPolicy string `json:"escrow_policy"`
	// Symbol is the user-facing asset ticker, for example USDT.
	Symbol string `json:"symbol,omitempty"`
	// Status is setup_required, testing, active, paused, or disabled.
	Status string `json:"status,omitempty"`
	// MinDeposit is expressed in display units and enforced before an intent is issued.
	MinDeposit string `json:"min_deposit,omitempty"`
	// DailyLimit is expressed in display units and applies per route.
	DailyLimit string `json:"daily_limit,omitempty"`
}

// BridgeContractConfig is public bridge route metadata. It intentionally does
// not contain private RPC URLs, relayer secrets, or execution authority.
type BridgeContractConfig struct {
	ContractID       string `json:"contract_id,omitempty"`
	RouteID          string `json:"route_id"`
	ChainID          string `json:"chain_id"`
	Address          string `json:"address,omitempty"`
	AssetDenom       string `json:"asset_denom"`
	ContractAddress  string `json:"contract_address"`
	DepositAddress   string `json:"deposit_address"`
	DepositMode      string `json:"deposit_mode"`
	ExecutionAdapter string `json:"execution_adapter,omitempty"`
	RuntimeCodeHash  string `json:"runtime_code_hash,omitempty"`
	Status           string `json:"status"`
	FinalityStatus   string `json:"finality_status"`
	MinDeposit       string `json:"min_deposit,omitempty"`
	DailyLimit       string `json:"daily_limit,omitempty"`
	AuditReference   string `json:"audit_reference,omitempty"`
	DeploymentTxHash string `json:"deployment_tx_hash,omitempty"`
	UpdatedAtUnix    int64  `json:"updated_at_unix"`
}

// BridgeValidatorConfig identifies a public Ed25519 key admitted by the bridge
// control plane for oracle-proof verification.
type BridgeValidatorConfig struct {
	ValidatorID string `json:"validator_id"`
	PublicKey   string `json:"public_key"`
	Status      string `json:"status"`
	Endpoint    string `json:"endpoint,omitempty"`
	Weight      uint16 `json:"weight,omitempty"`
	UpdatedAt   int64  `json:"updated_at_unix"`
}

type BridgeFinalityCheckpoint struct {
	Version              string                  `json:"version"`
	CheckpointID         string                  `json:"checkpoint_id"`
	PreviousCheckpointID string                  `json:"previous_checkpoint_id,omitempty"`
	SourceChainID        string                  `json:"source_chain_id"`
	Height               uint64                  `json:"height"`
	ObservedHeight       uint64                  `json:"observed_height"`
	BlockHash            string                  `json:"block_hash"`
	EventRoot            string                  `json:"event_root"`
	TransactionRoot      string                  `json:"transaction_root,omitempty"`
	ReceiptRoot          string                  `json:"receipt_root,omitempty"`
	StateRoot            string                  `json:"state_root,omitempty"`
	IssuedAtUnix         int64                   `json:"issued_at_unix"`
	ValidatorSignatures  []BridgeOracleSignature `json:"validator_signatures"`
	CreatedAtUnix        int64                   `json:"created_at_unix,omitempty"`
}

type BridgeOracleSignature struct {
	// `Signer` stores the value associated with this record.
	Signer string `json:"signer"`
	// `PublicKey` stores the key used to access the related value.
	PublicKey string `json:"public_key,omitempty"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature,omitempty"`
}

type BridgeProof struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version,omitempty"`
	// `SourceChainID` stores the value associated with this record.
	SourceChainID string `json:"source_chain_id"`
	// `EventID` stores the value associated with this record.
	EventID string `json:"event_id"`
	// SourceTxHash is the canonical source-chain transaction hash containing the lock event.
	SourceTxHash string `json:"source_tx_hash,omitempty"`
	// LogIndex identifies the event within a transaction and prevents same-tx collisions.
	LogIndex uint64 `json:"log_index,omitempty"`
	// EventType separates source locks from destination unlock confirmations.
	EventType string `json:"event_type,omitempty"`
	// WithdrawalID binds an unlock event to the exact MSC burn authorization.
	WithdrawalID string `json:"withdrawal_id,omitempty"`
	// EventContract is the source-chain bridge contract that emitted the event.
	EventContract string `json:"event_contract,omitempty"`
	// CheckpointID anchors the event proof to a threshold-signed finalized source checkpoint.
	CheckpointID string `json:"checkpoint_id,omitempty"`
	// FinalityCheckpoint carries the signed checkpoint certificate for stateless verification.
	FinalityCheckpoint *BridgeFinalityCheckpoint `json:"finality_checkpoint,omitempty"`
	// `AssetDenom` stores the value associated with this record.
	AssetDenom string `json:"asset_denom"`
	// `OriginAsset` stores the value associated with this record.
	OriginAsset string `json:"origin_asset,omitempty"`
	// `Recipient` stores the value associated with this record.
	Recipient string `json:"recipient,omitempty"`
	// `Amount` stores the value associated with this record.
	Amount string `json:"amount,omitempty"`
	// `PayloadHash` stores the digest used to identify or verify the related data.
	PayloadHash string `json:"payload_hash,omitempty"`
	// `SourceHeight` stores the value associated with this record.
	SourceHeight uint64 `json:"source_height"`
	// `ConfirmedHeight` stores the value associated with this record.
	ConfirmedHeight uint64 `json:"confirmed_height,omitempty"`
	// `SourceBlockHash` stores the digest used to identify or verify the related data.
	SourceBlockHash string `json:"source_block_hash,omitempty"`
	// `HeaderChain` stores the block data handled by this operation.
	HeaderChain []LightHeader `json:"header_chain,omitempty"`
	// `LightClientHeader` stores the block data handled by this operation.
	LightClientHeader *LightHeader `json:"light_client_header,omitempty"`
	// `MerkleProof` stores the value associated with this record.
	MerkleProof *LightMerkleProof `json:"merkle_proof,omitempty"`
	// EVMReceiptProof binds the source transaction and receipt log to canonical EVM header roots.
	EVMReceiptProof *BridgeEVMReceiptProof `json:"evm_receipt_proof,omitempty"`
	// TronTransactionProof binds the source transaction bytes to the signed txTrieRoot.
	TronTransactionProof *BridgeTronTransactionProof `json:"tron_transaction_proof,omitempty"`
	// `OracleSignatures` stores the result produced by this operation.
	OracleSignatures []BridgeOracleSignature `json:"oracle_signatures,omitempty"`
	// `ReplayProtectionKey` stores the key used to access the related value.
	ReplayProtectionKey string `json:"replay_protection_key,omitempty"`
}

type BridgeVerificationResult struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Accepted` stores the value associated with this record.
	Accepted bool `json:"accepted"`
	// `Status` stores the value associated with this record.
	Status string `json:"status"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
	// `Mode` stores the value associated with this record.
	Mode string `json:"mode"`
	// `Verification` stores the value associated with this record.
	Verification string `json:"verification"`
	// `SourceChainID` stores the value associated with this record.
	SourceChainID string `json:"source_chain_id,omitempty"`
	// `AssetDenom` stores the value associated with this record.
	AssetDenom string `json:"asset_denom,omitempty"`
	// `RequiredConfirmations` stores the request data being processed.
	RequiredConfirmations uint64 `json:"required_confirmations"`
	// `ObservedConfirmations` stores the value associated with this record.
	ObservedConfirmations uint64 `json:"observed_confirmations"`
	// `OracleSigners` stores the value associated with this record.
	OracleSigners int `json:"oracle_signers,omitempty"`
	// CryptographicOracleSigners is the number of valid signatures from active registered validators.
	CryptographicOracleSigners int `json:"cryptographic_oracle_signers,omitempty"`
	// `OracleQuorum` stores the value associated with this record.
	OracleQuorum uint16 `json:"oracle_quorum,omitempty"`
	// `ReplayProtectionKey` stores the key used to access the related value.
	ReplayProtectionKey string `json:"replay_protection_key,omitempty"`
	// BridgeID is the canonical hash of source chain, source transaction, and log index.
	BridgeID string `json:"bridge_id,omitempty"`
	// `Warnings` stores the value associated with this record.
	Warnings []string `json:"warnings,omitempty"`
}

type BridgeStatus struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Enabled` stores whether the related condition is satisfied.
	Enabled bool `json:"enabled"`
	// `Mode` stores the value associated with this record.
	Mode string `json:"mode"`
	// `IBCStyleEnabled` stores whether the related condition is satisfied.
	IBCStyleEnabled bool `json:"ibc_style_enabled"`
	// `LightClientRequired` stores the value associated with this record.
	LightClientRequired bool `json:"light_client_required"`
	// `RequiredConfirmations` stores the request data being processed.
	RequiredConfirmations uint64 `json:"required_confirmations"`
	// `OracleQuorum` stores the value associated with this record.
	OracleQuorum uint16 `json:"oracle_quorum"`
	// `RegisteredChains` stores the value associated with this record.
	RegisteredChains []BridgeChainConfig `json:"registered_chains"`
	// `RegisteredAssets` stores the value associated with this record.
	RegisteredAssets []BridgeAssetConfig `json:"registered_assets"`
	// RegisteredContracts contains route contracts configured through the gateway control plane.
	RegisteredContracts []BridgeContractConfig `json:"registered_contracts,omitempty"`
	// BridgeValidators contains public validator identities used for proof quorum.
	BridgeValidators []BridgeValidatorConfig `json:"bridge_validators,omitempty"`
	// Paused is the independent emergency-pause state.
	Paused bool `json:"paused"`
	// PauseReason explains why new deposits and withdrawals are unavailable.
	PauseReason string `json:"pause_reason,omitempty"`
	// Operational is true only when config, pause state, and at least one route are ready.
	Operational bool `json:"operational"`
	// PendingTransfers is the number of transfers that have not reached a terminal state.
	PendingTransfers int `json:"pending_transfers"`
	// CompletedTransfers is the number of completed transfers retained by this gateway.
	CompletedTransfers int `json:"completed_transfers"`
	// `Safety` stores the value associated with this record.
	Safety []string `json:"safety"`
}

var (
	// BridgeRegistryMu protects runtime registry changes made through the bridge admin API.
	BridgeRegistryMu sync.RWMutex
	// `BridgeEnabled` stores whether the related condition is satisfied.
	BridgeEnabled = false
	// `BridgeMode` stores the value used by this operation.
	BridgeMode = BridgeModeDisabled
	// `BridgeIBCStyleEnabled` stores whether the related condition is satisfied.
	BridgeIBCStyleEnabled = false
	// `BridgeLightClientRequired` stores the value used by this operation.
	BridgeLightClientRequired = true
	// `BridgeRequiredConfirmations` stores the value used by this operation.
	BridgeRequiredConfirmations = uint64(64)
	// `BridgeOracleQuorum` stores the value used by this operation.
	BridgeOracleQuorum = uint16(3)
	// `BridgeChains` stores the value used by this operation.
	BridgeChains []BridgeChainConfig
	// `BridgeAssets` stores the value used by this operation.
	BridgeAssets []BridgeAssetConfig
	// BridgeValidators are public validator keys accepted for cryptographic oracle quorum.
	BridgeValidators []BridgeValidatorConfig
)

// normalizeBridgeMode normalizes bridge mode.
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

// normalizeBridgeTrustModel normalizes bridge trust model.
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

// parseBridgeChainConfig parses bridge chain config.
func parseBridgeChainConfig(raw string) (BridgeChainConfig, error) {
	// `parts` stores the value produced by this operation.
	parts := strings.Split(raw, "|")
	if len(parts) < 3 {
		return BridgeChainConfig{}, fmt.Errorf("bridge chain entry must be chain_id|name|chain_type|trust_model|min_confirmations|light_client")
	}
	// `i` tracks the current position in the related collection.
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// `minConfirmations` stores the value produced by this operation.
	minConfirmations := BridgeRequiredConfirmations
	if len(parts) >= 5 && parts[4] != "" {
		// `v` and `err` store the error produced by this operation.
		v, err := strconv.ParseUint(parts[4], 10, 64)
		if err != nil {
			return BridgeChainConfig{}, fmt.Errorf("invalid bridge chain confirmations %q: %w", parts[4], err)
		}
		minConfirmations = v
	}
	// `trustModel` stores the value produced by this operation.
	trustModel := BridgeTrustLightClient
	if len(parts) >= 4 && parts[3] != "" {
		trustModel = normalizeBridgeTrustModel(parts[3])
	}
	// `lightClient` stores the value produced by this operation.
	lightClient := ""
	if len(parts) >= 6 {
		lightClient = parts[5]
	}
	status := ""
	if len(parts) >= 7 {
		status = normalizeBridgeRouteStatus(parts[6])
	}
	finalityStatus := ""
	if len(parts) >= 8 {
		finalityStatus = normalizeBridgeFinalityStatus(parts[7])
	}
	nativeSymbol := ""
	if len(parts) >= 9 {
		nativeSymbol = strings.ToUpper(parts[8])
	}
	explorerURL := ""
	if len(parts) >= 10 {
		explorerURL = parts[9]
	}
	// `cfg` stores the configuration used by this operation.
	cfg := BridgeChainConfig{
		ChainID:          parts[0],
		Name:             parts[1],
		ChainType:        parts[2],
		TrustModel:       trustModel,
		MinConfirmations: minConfirmations,
		LightClient:      lightClient,
		Status:           status,
		FinalityStatus:   finalityStatus,
		NativeSymbol:     nativeSymbol,
		ExplorerURL:      explorerURL,
	}
	if cfg.ChainID == "" || cfg.Name == "" || cfg.ChainType == "" {
		return BridgeChainConfig{}, fmt.Errorf("bridge chain entry has empty required fields")
	}
	return cfg, nil
}

// parseBridgeAssetConfig parses bridge asset config.
func parseBridgeAssetConfig(raw string) (BridgeAssetConfig, error) {
	// `parts` stores the value produced by this operation.
	parts := strings.Split(raw, "|")
	if len(parts) < 4 {
		return BridgeAssetConfig{}, fmt.Errorf("bridge asset entry must be denom|origin_chain|origin_asset|local_denom|decimals|escrow_policy")
	}
	// `i` tracks the current position in the related collection.
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// `decimals` stores the value produced by this operation.
	decimals := uint64(18)
	if len(parts) >= 5 && parts[4] != "" {
		// `v` and `err` store the error produced by this operation.
		v, err := strconv.ParseUint(parts[4], 10, 8)
		if err != nil {
			return BridgeAssetConfig{}, fmt.Errorf("invalid bridge asset decimals %q: %w", parts[4], err)
		}
		decimals = v
	}
	// `escrowPolicy` stores the value produced by this operation.
	escrowPolicy := "locked_escrow_or_verified_burn"
	if len(parts) >= 6 && parts[5] != "" {
		escrowPolicy = parts[5]
	}
	symbol := ""
	if len(parts) >= 7 {
		symbol = strings.ToUpper(parts[6])
	}
	status := ""
	if len(parts) >= 8 {
		status = normalizeBridgeRouteStatus(parts[7])
	}
	minDeposit := ""
	if len(parts) >= 9 {
		minDeposit = parts[8]
	}
	dailyLimit := ""
	if len(parts) >= 10 {
		dailyLimit = parts[9]
	}
	// `cfg` stores the configuration used by this operation.
	cfg := BridgeAssetConfig{
		Denom:        parts[0],
		OriginChain:  parts[1],
		OriginAsset:  parts[2],
		LocalDenom:   parts[3],
		Decimals:     uint8(decimals),
		EscrowPolicy: escrowPolicy,
		Symbol:       symbol,
		Status:       status,
		MinDeposit:   minDeposit,
		DailyLimit:   dailyLimit,
	}
	if cfg.Denom == "" || cfg.OriginChain == "" || cfg.OriginAsset == "" || cfg.LocalDenom == "" {
		return BridgeAssetConfig{}, fmt.Errorf("bridge asset entry has empty required fields")
	}
	return cfg, nil
}

// applyBridgeConfig applies bridge config.
func applyBridgeConfig(cfg BridgeConfig) bool {
	BridgeRegistryMu.Lock()
	defer BridgeRegistryMu.Unlock()

	// `changed` stores the value produced by this operation.
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
		// `chains` stores the value produced by this operation.
		chains := make([]BridgeChainConfig, 0, len(cfg.Chains))
		// `raw` tracks the current values while iterating.
		for _, raw := range cfg.Chains {
			// `parsed` and `err` store the error produced by this operation.
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
		// `assets` stores the value produced by this operation.
		assets := make([]BridgeAssetConfig, 0, len(cfg.Assets))
		// `raw` tracks the current values while iterating.
		for _, raw := range cfg.Assets {
			// `parsed` and `err` store the error produced by this operation.
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

// bridgeChainRegistry implements the bridge chain registry helper.
func bridgeChainRegistry() map[string]BridgeChainConfig {
	BridgeRegistryMu.RLock()
	defer BridgeRegistryMu.RUnlock()
	return bridgeChainRegistryLocked()
}

func bridgeChainRegistryLocked() map[string]BridgeChainConfig {
	// `out` stores the result produced by this operation.
	out := make(map[string]BridgeChainConfig, len(BridgeChains))
	// `chain` tracks the current values while iterating.
	for _, chain := range BridgeChains {
		out[strings.ToLower(strings.TrimSpace(chain.ChainID))] = chain
	}
	return out
}

// bridgeAssetRegistry implements the bridge asset registry helper.
func bridgeAssetRegistry() map[string]BridgeAssetConfig {
	BridgeRegistryMu.RLock()
	defer BridgeRegistryMu.RUnlock()
	return bridgeAssetRegistryLocked()
}

func bridgeAssetRegistryLocked() map[string]BridgeAssetConfig {
	// `out` stores the result produced by this operation.
	out := make(map[string]BridgeAssetConfig, len(BridgeAssets))
	// `asset` tracks the current values while iterating.
	for _, asset := range BridgeAssets {
		out[strings.ToLower(strings.TrimSpace(asset.Denom))] = asset
	}
	return out
}

// bridgeObservedConfirmations implements the bridge observed confirmations helper.
func bridgeObservedConfirmations(proof BridgeProof) uint64 {
	if proof.ConfirmedHeight == 0 || proof.SourceHeight == 0 || proof.ConfirmedHeight < proof.SourceHeight {
		return 0
	}
	return proof.ConfirmedHeight - proof.SourceHeight + 1
}

func validBridgeSourceTransactionID(chainType, value string) bool {
	value = strings.TrimSpace(value)
	switch normalizeBridgeChainType(chainType) {
	case "evm", "tron", "utxo":
		raw := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
		if len(raw) != 64 {
			return false
		}
		_, err := hex.DecodeString(raw)
		return err == nil
	case "solana":
		decoded, err := decodeBridgeBase58(value)
		return err == nil && len(decoded) == 64
	case "msc-compatible", "ibc":
		return len(value) >= 8 && len(value) <= 192
	default:
		return false
	}
}

func validBridgeSourceBlockID(chainType, value string) bool {
	value = strings.TrimSpace(value)
	switch normalizeBridgeChainType(chainType) {
	case "evm", "tron", "utxo":
		raw := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
		if len(raw) != 64 {
			return false
		}
		_, err := hex.DecodeString(raw)
		return err == nil
	case "solana":
		decoded, err := decodeBridgeBase58(value)
		return err == nil && len(decoded) == 32
	case "msc-compatible", "ibc":
		return len(value) >= 8 && len(value) <= 192
	default:
		return false
	}
}

func normalizeBridgeFinalityCheckpoint(checkpoint BridgeFinalityCheckpoint, chainType string) BridgeFinalityCheckpoint {
	checkpoint.Version = strings.TrimSpace(checkpoint.Version)
	checkpoint.CheckpointID = strings.ToLower(strings.TrimSpace(checkpoint.CheckpointID))
	checkpoint.PreviousCheckpointID = strings.ToLower(strings.TrimSpace(checkpoint.PreviousCheckpointID))
	checkpoint.SourceChainID = normalizeBridgeRegistryID(checkpoint.SourceChainID)
	checkpoint.BlockHash = strings.TrimSpace(checkpoint.BlockHash)
	if normalizedType := normalizeBridgeChainType(chainType); normalizedType == "evm" || normalizedType == "tron" || normalizedType == "utxo" {
		checkpoint.BlockHash = strings.ToLower(checkpoint.BlockHash)
	}
	checkpoint.EventRoot = normalizeLightHexHash(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(checkpoint.EventRoot), "0x"), "0X"))
	checkpoint.TransactionRoot = normalizeLightHexHash(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(checkpoint.TransactionRoot), "0x"), "0X"))
	checkpoint.ReceiptRoot = normalizeLightHexHash(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(checkpoint.ReceiptRoot), "0x"), "0X"))
	checkpoint.StateRoot = normalizeLightHexHash(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(checkpoint.StateRoot), "0x"), "0X"))
	checkpoint.CreatedAtUnix = 0
	return checkpoint
}

func canonicalBridgeCheckpointPayload(checkpoint BridgeFinalityCheckpoint, chainType string) string {
	checkpoint = normalizeBridgeFinalityCheckpoint(checkpoint, chainType)
	return strings.Join([]string{
		"MSC", "BRIDGE_CHECKPOINT", BridgeCheckpointVersion,
		checkpoint.SourceChainID,
		strconv.FormatUint(checkpoint.Height, 10),
		strconv.FormatUint(checkpoint.ObservedHeight, 10),
		checkpoint.BlockHash,
		checkpoint.EventRoot,
		checkpoint.TransactionRoot,
		checkpoint.ReceiptRoot,
		checkpoint.StateRoot,
		checkpoint.PreviousCheckpointID,
		strconv.FormatInt(checkpoint.IssuedAtUnix, 10),
	}, "|")
}

func bridgeCheckpointID(checkpoint BridgeFinalityCheckpoint, chainType string) string {
	return "bcp_" + lightHashString(canonicalBridgeCheckpointPayload(checkpoint, chainType))
}

func validBridgeCheckpointID(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "bcp_") || len(value) != 68 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "bcp_"))
	return err == nil
}

func bridgeVerifiedCheckpointSignatures(checkpoint BridgeFinalityCheckpoint, chainType string, validators []BridgeValidatorConfig) []BridgeOracleSignature {
	active := make(map[string]ed25519.PublicKey, len(validators)*2)
	for _, validator := range validators {
		if !strings.EqualFold(strings.TrimSpace(validator.Status), BridgeRouteStatusActive) {
			continue
		}
		pubRaw, err := hex.DecodeString(strings.TrimSpace(validator.PublicKey))
		if err != nil || len(pubRaw) != ed25519.PublicKeySize {
			continue
		}
		pub := ed25519.PublicKey(append([]byte(nil), pubRaw...))
		active[strings.ToLower(strings.TrimSpace(validator.ValidatorID))] = pub
		active[strings.ToLower(hex.EncodeToString(pub))] = pub
	}
	message := []byte(canonicalBridgeCheckpointPayload(checkpoint, chainType))
	verified := make(map[string]BridgeOracleSignature, len(checkpoint.ValidatorSignatures))
	for _, candidate := range checkpoint.ValidatorSignatures {
		lookup := strings.ToLower(strings.TrimSpace(candidate.Signer))
		if lookup == "" {
			lookup = strings.ToLower(strings.TrimSpace(candidate.PublicKey))
		}
		pub, ok := active[lookup]
		if !ok {
			continue
		}
		if supplied := strings.ToLower(strings.TrimSpace(candidate.PublicKey)); supplied != "" && supplied != hex.EncodeToString(pub) {
			continue
		}
		sig, err := hex.DecodeString(strings.TrimSpace(candidate.Signature))
		if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, message, sig) {
			continue
		}
		candidate.PublicKey = hex.EncodeToString(pub)
		verified[hex.EncodeToString(pub)] = candidate
	}
	keys := make([]string, 0, len(verified))
	for key := range verified {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]BridgeOracleSignature, 0, len(keys))
	for _, key := range keys {
		out = append(out, verified[key])
	}
	return out
}

func bridgeVerifiedCheckpointSigners(checkpoint BridgeFinalityCheckpoint, chainType string, validators []BridgeValidatorConfig) int {
	return len(bridgeVerifiedCheckpointSignatures(checkpoint, chainType, validators))
}

func verifyBridgeFinalityCheckpoint(checkpoint BridgeFinalityCheckpoint, chain BridgeChainConfig, validators []BridgeValidatorConfig) (BridgeFinalityCheckpoint, int, error) {
	checkpoint = normalizeBridgeFinalityCheckpoint(checkpoint, chain.ChainType)
	if checkpoint.Version != BridgeCheckpointVersion {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint version mismatch")
	}
	if !strings.EqualFold(checkpoint.SourceChainID, chain.ChainID) {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint source chain mismatch")
	}
	if checkpoint.Height == 0 || checkpoint.ObservedHeight < checkpoint.Height {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint heights invalid")
	}
	required := BridgeRequiredConfirmations
	if chain.MinConfirmations > required {
		required = chain.MinConfirmations
	}
	observed := checkpoint.ObservedHeight - checkpoint.Height + 1
	if observed < required {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint confirmations insufficient: got %d need %d", observed, required)
	}
	if !validBridgeSourceBlockID(chain.ChainType, checkpoint.BlockHash) {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint block hash invalid")
	}
	if checkpoint.EventRoot == "" {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint event root invalid")
	}
	if normalizeBridgeChainType(chain.ChainType) == "evm" && checkpoint.TransactionRoot == "" {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint EVM transaction root required")
	}
	if checkpoint.ReceiptRoot == "" && checkpoint.StateRoot == "" {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint source receipt or state root required")
	}
	if checkpoint.PreviousCheckpointID != "" && !validBridgeCheckpointID(checkpoint.PreviousCheckpointID) {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint previous id invalid")
	}
	if checkpoint.IssuedAtUnix <= 0 {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint issue time required")
	}
	wantID := bridgeCheckpointID(checkpoint, chain.ChainType)
	if checkpoint.CheckpointID == "" {
		checkpoint.CheckpointID = wantID
	}
	if checkpoint.CheckpointID != wantID {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint id mismatch")
	}
	if BridgeOracleQuorum == 0 {
		return checkpoint, 0, fmt.Errorf("bridge checkpoint quorum unavailable")
	}
	signers := bridgeVerifiedCheckpointSigners(checkpoint, chain.ChainType, validators)
	if signers < int(BridgeOracleQuorum) {
		return checkpoint, signers, fmt.Errorf("bridge checkpoint validator quorum not met: got %d need %d", signers, BridgeOracleQuorum)
	}
	return checkpoint, signers, nil
}

func bridgeSourceTransactionHash(proof BridgeProof) string {
	if txHash := strings.TrimSpace(proof.SourceTxHash); txHash != "" {
		return txHash
	}
	return strings.TrimSpace(proof.EventID)
}

func bridgeProofEventType(proof BridgeProof) string {
	value := strings.ToLower(strings.TrimSpace(proof.EventType))
	if value == "" {
		return "lock"
	}
	return value
}

func canonicalBridgeWithdrawalID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "0x")
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	if strings.Trim(value, "0") == "" {
		return ""
	}
	return "0x" + value
}

// bridgeID is derived by the verifier and cannot be selected by a relayer.
func bridgeID(proof BridgeProof) string {
	source := strings.ToLower(strings.TrimSpace(proof.SourceChainID))
	txHash := canonicalBridgeExternalIdentifier(bridgeSourceTransactionHash(proof))
	if source == "" || txHash == "" {
		return ""
	}
	material := strings.Join([]string{
		"MSC", "BRIDGE_ID", BridgeProtocolVersion, source, txHash,
		strconv.FormatUint(proof.LogIndex, 10),
	}, "|")
	return "bridge_" + lightHashString(material)
}

// bridgeReplayKey aliases the canonical BridgeID for persisted replay checks.
func bridgeReplayKey(proof BridgeProof) string {
	return bridgeID(proof)
}

func bridgeIDFromMintCertificate(cert DTLGovernanceCert) (string, bool) {
	const prefix = "bridge-mint-bridge_"
	nonce := normalizeDTLGovernanceNonce(cert.Nonce)
	if !strings.HasPrefix(nonce, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(nonce, prefix)
	if len(remainder) < 64 {
		return "", false
	}
	digest := remainder[:64]
	if len(remainder) > 64 && (len(remainder) != 105 || remainder[64] != '-') {
		return "", false
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", false
	}
	return "bridge_" + digest, true
}

// canonicalBridgeEventPayload returns the deterministic external event material
// that a light-client Merkle proof must commit before the bridge verifier treats
// the proof as evidence for this BridgeProof. Confirmation height is deliberately
// excluded because it is verifier context, not event identity.
func canonicalBridgeEventPayload(proof BridgeProof, asset BridgeAssetConfig) string {
	originAsset := strings.TrimSpace(proof.OriginAsset)
	if originAsset == "" {
		originAsset = strings.TrimSpace(asset.OriginAsset)
	}
	return strings.Join([]string{
		"MSC",
		"BRIDGE",
		BridgeProtocolVersion,
		strings.ToLower(strings.TrimSpace(proof.SourceChainID)),
		canonicalBridgeExternalIdentifier(bridgeSourceTransactionHash(proof)),
		strconv.FormatUint(proof.LogIndex, 10),
		bridgeID(proof),
		bridgeProofEventType(proof),
		canonicalBridgeWithdrawalID(proof.WithdrawalID),
		canonicalBridgeEventContract(proof.EventContract),
		strings.ToLower(strings.TrimSpace(proof.AssetDenom)),
		originAsset,
		strings.TrimSpace(proof.Recipient),
		strings.TrimSpace(proof.Amount),
		strconv.FormatUint(proof.SourceHeight, 10),
		canonicalBridgeExternalIdentifier(proof.SourceBlockHash),
	}, "|")
}

func canonicalBridgeExternalIdentifier(value string) string {
	value = strings.TrimSpace(value)
	raw := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(raw) == 64 {
		if _, err := hex.DecodeString(raw); err == nil {
			return strings.ToLower(value)
		}
	}
	return value
}

func bridgeExternalIdentifierEqual(chainType, left, right string) bool {
	if normalizeBridgeChainType(chainType) == "solana" {
		return strings.TrimSpace(left) == strings.TrimSpace(right)
	}
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func canonicalBridgeEventContract(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 42 && strings.HasPrefix(strings.ToLower(value), "0x") {
		if _, err := hex.DecodeString(value[2:]); err == nil {
			return strings.ToLower(value)
		}
	}
	return value
}

// bridgeEventPayloadHash returns the hash committed by bridge light proofs.
func bridgeEventPayloadHash(proof BridgeProof, asset BridgeAssetConfig) string {
	return lightHashString(canonicalBridgeEventPayload(proof, asset))
}

// bridgeLightProofBindsEvent verifies that a valid light-client proof commits
// this exact bridge event, rather than merely proving any state/tx/receipt leaf
// under a header root.
func bridgeLightProofBindsEvent(proof BridgeProof, asset BridgeAssetConfig) (bool, string) {
	if proof.MerkleProof == nil {
		return false, "light_client_proof_required"
	}
	expectedHash := bridgeEventPayloadHash(proof, asset)
	if expectedHash == "" {
		return false, "payload_hash_unavailable"
	}
	if payloadHash := normalizeLightHexHash(proof.PayloadHash); payloadHash != "" && payloadHash != expectedHash {
		return false, "payload_hash_mismatch"
	}
	leafHash := normalizeLightHexHash(proof.MerkleProof.LeafHash)
	if leafHash != expectedHash {
		return false, "bridge_payload_not_committed"
	}
	if value := strings.TrimSpace(proof.MerkleProof.LeafValue); value != "" && value != canonicalBridgeEventPayload(proof, asset) {
		return false, "bridge_payload_value_mismatch"
	}
	return true, ""
}

// bridgeUniqueOracleSigners implements the bridge unique oracle signers helper.
func bridgeUniqueOracleSigners(signatures []BridgeOracleSignature) int {
	// `seen` stores the value produced by this operation.
	seen := map[string]struct{}{}
	// `sig` tracks the current values while iterating.
	for _, sig := range signatures {
		// `signer` stores the value produced by this operation.
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

func bridgeValidatorSnapshot() []BridgeValidatorConfig {
	BridgeRegistryMu.RLock()
	defer BridgeRegistryMu.RUnlock()
	return append([]BridgeValidatorConfig(nil), BridgeValidators...)
}

// bridgeCryptographicOracleSigners verifies signatures over the canonical
// external lock event using active validator keys from the bridge registry.
func bridgeCryptographicOracleSigners(proof BridgeProof, asset BridgeAssetConfig, validators []BridgeValidatorConfig) int {
	active := make(map[string]ed25519.PublicKey, len(validators)*2)
	for _, validator := range validators {
		if !strings.EqualFold(strings.TrimSpace(validator.Status), "active") {
			continue
		}
		pubRaw, err := hex.DecodeString(strings.TrimSpace(validator.PublicKey))
		if err != nil || len(pubRaw) != ed25519.PublicKeySize {
			continue
		}
		pub := ed25519.PublicKey(append([]byte(nil), pubRaw...))
		if id := strings.ToLower(strings.TrimSpace(validator.ValidatorID)); id != "" {
			active[id] = pub
		}
		active[strings.ToLower(hex.EncodeToString(pub))] = pub
	}
	if len(active) == 0 {
		return 0
	}

	message := []byte(canonicalBridgeEventPayload(proof, asset))
	verified := make(map[string]struct{}, len(proof.OracleSignatures))
	for _, candidate := range proof.OracleSignatures {
		lookup := strings.ToLower(strings.TrimSpace(candidate.Signer))
		if lookup == "" {
			lookup = strings.ToLower(strings.TrimSpace(candidate.PublicKey))
		}
		pub, ok := active[lookup]
		if !ok {
			continue
		}
		if supplied := strings.ToLower(strings.TrimSpace(candidate.PublicKey)); supplied != "" && supplied != hex.EncodeToString(pub) {
			continue
		}
		sig, err := hex.DecodeString(strings.TrimSpace(candidate.Signature))
		if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, message, sig) {
			continue
		}
		verified[hex.EncodeToString(pub)] = struct{}{}
	}
	return len(verified)
}

// bridgeFailure implements the bridge failure helper.
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
		BridgeID:              bridgeID(proof),
	}
}

func bridgeProofRequiresCheckpoint(mode string, chain BridgeChainConfig) bool {
	trustModel := normalizeBridgeTrustModel(chain.TrustModel)
	return normalizeBridgeMode(mode) == BridgeModeGuardedMint || BridgeLightClientRequired ||
		trustModel == BridgeTrustLightClient || trustModel == BridgeTrustHybrid
}

// VerifyBridgeProof verifies bridge proof.
func VerifyBridgeProof(proof BridgeProof) BridgeVerificationResult {
	// `mode` stores the value produced by this operation.
	mode := normalizeBridgeMode(BridgeMode)
	// `required` stores the request data being processed.
	required := BridgeRequiredConfirmations
	// `observed` stores the value produced by this operation.
	observed := bridgeObservedConfirmations(proof)
	if !BridgeEnabled || mode == BridgeModeDisabled {
		return bridgeFailure(proof, mode, required, observed, "bridge_disabled")
	}
	if proof.Version != BridgeProtocolVersion {
		return bridgeFailure(proof, mode, required, observed, "unsupported_bridge_protocol_version")
	}
	if strings.TrimSpace(proof.SourceChainID) == "" {
		return bridgeFailure(proof, mode, required, observed, "missing_source_chain_id")
	}
	if bridgeSourceTransactionHash(proof) == "" {
		return bridgeFailure(proof, mode, required, observed, "missing_source_tx_hash")
	}
	if mode == BridgeModeGuardedMint && strings.TrimSpace(proof.SourceTxHash) == "" {
		return bridgeFailure(proof, mode, required, observed, "source_tx_hash_required_for_guarded_mint")
	}
	if bridgeReplayKey(proof) == "" {
		return bridgeFailure(proof, mode, required, observed, "missing_replay_protection_key")
	}
	eventType := bridgeProofEventType(proof)
	if eventType != "lock" && eventType != "unlock" {
		return bridgeFailure(proof, mode, required, observed, "invalid_bridge_event_type")
	}
	if eventType == "lock" && strings.TrimSpace(proof.WithdrawalID) != "" {
		return bridgeFailure(proof, mode, required, observed, "unexpected_withdrawal_id")
	}
	if eventType == "unlock" && canonicalBridgeWithdrawalID(proof.WithdrawalID) == "" {
		return bridgeFailure(proof, mode, required, observed, "invalid_withdrawal_id")
	}
	if supplied := strings.TrimSpace(proof.ReplayProtectionKey); supplied != "" && !strings.EqualFold(supplied, bridgeReplayKey(proof)) {
		return bridgeFailure(proof, mode, required, observed, "replay_protection_key_mismatch")
	}
	// `chains` stores the value produced by this operation.
	chains := bridgeChainRegistry()
	// `chain` and `ok` store whether the related condition is satisfied.
	chain, ok := chains[strings.ToLower(strings.TrimSpace(proof.SourceChainID))]
	if !ok {
		return bridgeFailure(proof, mode, required, observed, "source_chain_not_registered")
	}
	if mode == BridgeModeGuardedMint && !validBridgeSourceTransactionID(chain.ChainType, proof.SourceTxHash) {
		return bridgeFailure(proof, mode, required, observed, "invalid_source_tx_hash")
	}
	if mode == BridgeModeGuardedMint && !validExternalBridgeAddress(chain.ChainType, proof.EventContract) {
		return bridgeFailure(proof, mode, required, observed, "invalid_event_contract")
	}
	if status := strings.ToLower(strings.TrimSpace(chain.Status)); status != "" && status != BridgeRouteStatusActive && status != BridgeRouteStatusTesting {
		return bridgeFailure(proof, mode, required, observed, "source_chain_not_active")
	}
	if mode == BridgeModeGuardedMint && !strings.EqualFold(strings.TrimSpace(chain.Status), BridgeRouteStatusActive) {
		return bridgeFailure(proof, mode, required, observed, "source_chain_not_active")
	}
	if mode == BridgeModeGuardedMint && !strings.EqualFold(strings.TrimSpace(chain.FinalityStatus), BridgeFinalityFinalized) {
		return bridgeFailure(proof, mode, required, observed, "source_chain_finality_not_ready")
	}
	if mode == BridgeModeGuardedMint && chain.LatestFinalizedHeight > 0 && proof.SourceHeight > chain.LatestFinalizedHeight {
		return bridgeFailure(proof, mode, required, observed, "source_event_not_finalized")
	}
	if chain.MinConfirmations > required {
		required = chain.MinConfirmations
	}
	if observed < required {
		return bridgeFailure(proof, mode, required, observed, "insufficient_confirmations")
	}
	// `assets` stores the value produced by this operation.
	assets := bridgeAssetRegistry()
	// `asset` and `ok` store whether the related condition is satisfied.
	asset, ok := assets[strings.ToLower(strings.TrimSpace(proof.AssetDenom))]
	if !ok {
		return bridgeFailure(proof, mode, required, observed, "asset_not_registered")
	}
	if !strings.EqualFold(strings.TrimSpace(asset.OriginChain), strings.TrimSpace(proof.SourceChainID)) {
		return bridgeFailure(proof, mode, required, observed, "asset_origin_chain_mismatch")
	}
	if status := strings.ToLower(strings.TrimSpace(asset.Status)); status != "" && status != BridgeRouteStatusActive && status != BridgeRouteStatusTesting {
		return bridgeFailure(proof, mode, required, observed, "asset_not_active")
	}
	if mode == BridgeModeGuardedMint && !strings.EqualFold(strings.TrimSpace(asset.Status), BridgeRouteStatusActive) {
		return bridgeFailure(proof, mode, required, observed, "asset_not_active")
	}
	if mode == BridgeModeGuardedMint && normalizeBridgeChainType(chain.ChainType) == "tron" && !isCanonicalTronMainnetAsset(chain, asset) {
		return bridgeFailure(proof, mode, required, observed, "unsupported_tron_route_identity")
	}
	if proof.OriginAsset != "" && !bridgeExternalAddressEqual(chain.ChainType, asset.OriginAsset, proof.OriginAsset) {
		return bridgeFailure(proof, mode, required, observed, "asset_origin_asset_mismatch")
	}

	// `trustModel` stores the value produced by this operation.
	trustModel := normalizeBridgeTrustModel(chain.TrustModel)
	validators := bridgeValidatorSnapshot()
	// `warnings` stores the value produced by this operation.
	warnings := []string{}
	// `verification` stores the value produced by this operation.
	verification := "syntax_and_quorum_only"
	checkpointRequired := bridgeProofRequiresCheckpoint(mode, chain)
	if checkpointRequired {
		if proof.FinalityCheckpoint == nil || strings.TrimSpace(proof.CheckpointID) == "" {
			return bridgeFailure(proof, mode, required, observed, "finality_checkpoint_required")
		}
		checkpoint, _, err := verifyBridgeFinalityCheckpoint(*proof.FinalityCheckpoint, chain, validators)
		if err != nil {
			return bridgeFailure(proof, mode, required, observed, "invalid_finality_checkpoint: "+err.Error())
		}
		if !strings.EqualFold(proof.CheckpointID, checkpoint.CheckpointID) {
			return bridgeFailure(proof, mode, required, observed, "finality_checkpoint_id_mismatch")
		}
		if proof.SourceHeight != checkpoint.Height || proof.ConfirmedHeight > checkpoint.ObservedHeight ||
			!bridgeExternalIdentifierEqual(chain.ChainType, proof.SourceBlockHash, checkpoint.BlockHash) {
			return bridgeFailure(proof, mode, required, observed, "source_event_checkpoint_mismatch")
		}
		if proof.MerkleProof == nil || !strings.EqualFold(proof.MerkleProof.Root, checkpoint.EventRoot) || !VerifyLightMerkleProof(*proof.MerkleProof) {
			return bridgeFailure(proof, mode, required, observed, "checkpoint_event_proof_invalid")
		}
		if ok, reason := bridgeLightProofBindsEvent(proof, asset); !ok {
			return bridgeFailure(proof, mode, required, observed, reason)
		}
		if proof.LightClientHeader != nil {
			header := proof.LightClientHeader
			if !strings.EqualFold(header.ChainID, checkpoint.SourceChainID) || header.Height != checkpoint.Height ||
				!bridgeExternalIdentifierEqual(chain.ChainType, header.BlockHash, checkpoint.BlockHash) ||
				!strings.EqualFold(strings.TrimSpace(header.EventRoot), checkpoint.EventRoot) ||
				(checkpoint.TransactionRoot != "" && !strings.EqualFold(strings.TrimSpace(header.TxRoot), checkpoint.TransactionRoot)) ||
				(checkpoint.ReceiptRoot != "" && !strings.EqualFold(strings.TrimSpace(header.ReceiptRoot), checkpoint.ReceiptRoot)) ||
				(checkpoint.StateRoot != "" && !strings.EqualFold(strings.TrimSpace(header.StateRoot), checkpoint.StateRoot)) {
				return bridgeFailure(proof, mode, required, observed, "light_header_checkpoint_mismatch")
			}
		}
		verification = "threshold_finality_checkpoint+event_merkle_proof"
		switch normalizeBridgeChainType(chain.ChainType) {
		case "evm":
			if proof.TronTransactionProof != nil {
				return bridgeFailure(proof, mode, required, observed, "unexpected_tron_transaction_proof")
			}
			if proof.EVMReceiptProof == nil {
				if mode == BridgeModeGuardedMint {
					return bridgeFailure(proof, mode, required, observed, "canonical_evm_receipt_proof_required")
				}
			} else {
				if reason := verifyBridgeEVMReceiptProof(proof, asset, checkpoint); reason != "" {
					return bridgeFailure(proof, mode, required, observed, reason)
				}
				verification += "+canonical_evm_tx_receipt_proof"
			}
		case "tron":
			if proof.EVMReceiptProof != nil {
				return bridgeFailure(proof, mode, required, observed, "unexpected_evm_receipt_proof")
			}
			if reason := verifyBridgeTronTransactionProof(proof, checkpoint); reason != "" {
				return bridgeFailure(proof, mode, required, observed, reason)
			}
			verification += "+canonical_tron_transaction_proof"
		default:
			if proof.EVMReceiptProof != nil || proof.TronTransactionProof != nil {
				return bridgeFailure(proof, mode, required, observed, "unexpected_chain_specific_transaction_proof")
			}
		}
	}
	if len(proof.HeaderChain) > 0 {
		// `err` stores the error produced by this operation.
		if err := VerifyLightHeaderChain(proof.HeaderChain); err != nil {
			return bridgeFailure(proof, mode, required, observed, "invalid_light_header_chain: "+err.Error())
		}
		if checkpointRequired {
			last := proof.HeaderChain[len(proof.HeaderChain)-1]
			checkpoint := proof.FinalityCheckpoint
			if checkpoint == nil || last.Height != checkpoint.Height || !bridgeExternalIdentifierEqual(chain.ChainType, last.BlockHash, checkpoint.BlockHash) {
				return bridgeFailure(proof, mode, required, observed, "light_header_chain_checkpoint_mismatch")
			}
			verification += "+light_header_chain"
		} else {
			verification = "light_header_chain"
		}
	}
	if proof.LightClientHeader != nil && proof.MerkleProof != nil {
		// `proofRoot` stores the digest used to identify or verify the related data.
		proofRoot := strings.TrimSpace(proof.MerkleProof.Root)
		// `headerRootMatch` stores the block data handled by this operation.
		headerRootMatch := proofRoot != "" && (strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.StateMerkleRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.StateRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.TxRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.ReceiptRoot)) ||
			strings.EqualFold(proofRoot, strings.TrimSpace(proof.LightClientHeader.EventRoot)))
		if !headerRootMatch {
			return bridgeFailure(proof, mode, required, observed, "proof_root_not_in_light_header")
		}
		if VerifyLightMerkleProof(*proof.MerkleProof) {
			if ok, reason := bridgeLightProofBindsEvent(proof, asset); !ok {
				return bridgeFailure(proof, mode, required, observed, reason)
			}
			if checkpointRequired {
				verification += "+light_header_binding"
			} else {
				verification = "light_client_merkle_proof"
			}
		} else {
			return bridgeFailure(proof, mode, required, observed, "invalid_merkle_proof")
		}
	} else if BridgeLightClientRequired || trustModel == BridgeTrustLightClient || trustModel == BridgeTrustHybrid {
		return bridgeFailure(proof, mode, required, observed, "light_client_proof_required")
	}

	// `oracleSigners` stores the value produced by this operation.
	oracleSigners := bridgeUniqueOracleSigners(proof.OracleSignatures)
	cryptographicOracleSigners := bridgeCryptographicOracleSigners(proof, asset, validators)
	if trustModel == BridgeTrustOracleQuorum || trustModel == BridgeTrustHybrid {
		if len(validators) > 0 {
			if cryptographicOracleSigners < int(BridgeOracleQuorum) {
				return bridgeFailure(proof, mode, required, observed, "cryptographic_oracle_quorum_not_met")
			}
			oracleSigners = cryptographicOracleSigners
			if verification == "syntax_and_quorum_only" {
				verification = "cryptographic_oracle_quorum"
			} else {
				verification += "+cryptographic_oracle_quorum"
			}
		} else {
			if mode == BridgeModeGuardedMint {
				return bridgeFailure(proof, mode, required, observed, "bridge_validator_registry_required")
			}
			if oracleSigners < int(BridgeOracleQuorum) {
				return bridgeFailure(proof, mode, required, observed, "oracle_quorum_not_met")
			}
			if verification == "syntax_and_quorum_only" {
				verification = "oracle_quorum_syntax"
				warnings = append(warnings, "verification-only mode accepted signer syntax; guarded mint requires registered cryptographic validators")
			}
		}
	}
	if mode == BridgeModeGuardedMint {
		warnings = append(warnings, "guarded_mint mode configured, but this verifier does not execute mint/unlock")
	}

	return BridgeVerificationResult{
		Version:                    BridgeProtocolVersion,
		Accepted:                   true,
		Status:                     "verified",
		Mode:                       mode,
		Verification:               verification,
		SourceChainID:              strings.TrimSpace(proof.SourceChainID),
		AssetDenom:                 strings.TrimSpace(proof.AssetDenom),
		RequiredConfirmations:      required,
		ObservedConfirmations:      observed,
		OracleSigners:              oracleSigners,
		CryptographicOracleSigners: cryptographicOracleSigners,
		OracleQuorum:               BridgeOracleQuorum,
		ReplayProtectionKey:        bridgeReplayKey(proof),
		BridgeID:                   bridgeID(proof),
		Warnings:                   warnings,
	}
}

// ApplyVerifiedBridgeMint is the only bridge mint entry point: an accepted
// external lock proof is required, and the mint still passes ApplySupplyDelta.
func ApplyVerifiedBridgeMint(ledger *Ledger, proof BridgeProof) (SupplyChange, error) {
	var change SupplyChange
	if ledger == nil {
		return change, fmt.Errorf("bridge mint ledger unavailable")
	}
	result := VerifyBridgeProof(proof)
	if !result.Accepted {
		return change, fmt.Errorf("bridge mint proof rejected: %s", result.Reason)
	}
	if normalizeBridgeMode(BridgeMode) != BridgeModeGuardedMint {
		return change, fmt.Errorf("bridge mint requires guarded_mint mode")
	}
	asset, ok := bridgeAssetRegistry()[strings.ToLower(strings.TrimSpace(proof.AssetDenom))]
	if !ok {
		return change, fmt.Errorf("bridge mint asset is not registered")
	}
	if !strings.EqualFold(strings.TrimSpace(asset.LocalDenom), CoinSymbol) {
		return change, fmt.Errorf("wrapped asset %s must mint through a consensus DTL token transaction, not native MSC supply", asset.LocalDenom)
	}
	replayKey := strings.ToLower(strings.TrimSpace(bridgeReplayKey(proof)))
	if replayKey == "" {
		return change, fmt.Errorf("bridge mint replay key required")
	}
	if ledger.UsedBridgeEvents == nil {
		ledger.UsedBridgeEvents = make(map[string]uint64)
	}
	if _, exists := ledger.UsedBridgeEvents[replayKey]; exists {
		return change, fmt.Errorf("bridge event already consumed: %s", replayKey)
	}
	recipient := strings.TrimSpace(proof.Recipient)
	if recipient == "" {
		return change, fmt.Errorf("bridge mint recipient required")
	}
	amount, err := strconv.ParseInt(strings.TrimSpace(proof.Amount), 10, 64)
	if err != nil || amount <= 0 {
		return change, fmt.Errorf("bridge mint amount invalid")
	}
	change, err = ApplySupplyDelta(ledger, SupplyDelta{
		Coin:       CoinSymbol,
		MintTo:     recipient,
		MintAmount: amount,
		Reason:     "bridge_external_lock:" + strings.TrimSpace(proof.EventID),
	})
	if err != nil {
		return change, err
	}
	ledger.UsedBridgeEvents[replayKey] = proof.SourceHeight
	return change, nil
}

// ApplyBridgeBurnForExternalUnlock is the reverse bridge supply path. Unlocks
// on the external chain must be driven by this burn proof, not by mint repair.
func ApplyBridgeBurnForExternalUnlock(ledger *Ledger, holder string, amount int64, eventID string) (SupplyChange, error) {
	if normalizeBridgeMode(BridgeMode) == BridgeModeDisabled || !BridgeEnabled {
		return SupplyChange{}, fmt.Errorf("bridge burn requires enabled bridge")
	}
	if ledger == nil {
		return SupplyChange{}, fmt.Errorf("bridge burn ledger unavailable")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return SupplyChange{}, fmt.Errorf("bridge burn event id required")
	}
	if amount <= 0 {
		return SupplyChange{}, fmt.Errorf("bridge burn amount invalid")
	}
	replayKey := "withdraw:" + strings.ToLower(eventID)
	if ledger.UsedBridgeEvents == nil {
		ledger.UsedBridgeEvents = make(map[string]uint64)
	}
	if _, exists := ledger.UsedBridgeEvents[replayKey]; exists {
		return SupplyChange{}, fmt.Errorf("bridge withdrawal already consumed: %s", eventID)
	}
	change, err := ApplySupplyDelta(ledger, SupplyDelta{
		Coin:       CoinSymbol,
		BurnFrom:   holder,
		BurnAmount: amount,
		Reason:     "bridge_external_unlock:" + eventID,
	})
	if err != nil {
		return change, err
	}
	ledger.UsedBridgeEvents[replayKey] = 0
	return change, nil
}

// BridgeStatusSnapshot implements the bridge status snapshot helper.
func BridgeStatusSnapshot() BridgeStatus {
	BridgeRegistryMu.RLock()
	defer BridgeRegistryMu.RUnlock()
	// `chains` stores the value produced by this operation.
	chains := append([]BridgeChainConfig(nil), BridgeChains...)
	sort.Slice(chains, func(i, j int) bool {
		return chains[i].ChainID < chains[j].ChainID
	})
	// `assets` stores the value produced by this operation.
	assets := append([]BridgeAssetConfig(nil), BridgeAssets...)
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Denom < assets[j].Denom
	})
	// `safety` stores the value produced by this operation.
	safety := []string{
		"bridge execution is disabled unless explicitly enabled by config and governance",
		"verification endpoint never mints or unlocks assets",
		"guarded bridge mint requires an accepted external lock proof and ApplySupplyDelta",
		"external unlock requires MSC burn through ApplySupplyDelta",
		"light-client/SPV proof is required by default",
		"asset mapping requires an explicit registered origin chain and origin asset",
		"replay protection key must bind source chain and event id and is consumed in consensus ledger state",
		"wrapped assets must mint through consensus DTL token transactions and can never mutate native MSC supply",
		"guarded mint requires cryptographic quorum from registered bridge validators",
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

// bridgeStatusResponse implements the bridge status response helper.
func (s *Server) bridgeStatusResponse() (BridgeStatus, int, string) {
	if s == nil || s.Node == nil {
		return BridgeStatus{}, http.StatusServiceUnavailable, "node unavailable"
	}
	status, err := s.augmentBridgeStatus(BridgeStatusSnapshot())
	if err != nil {
		return BridgeStatus{}, http.StatusServiceUnavailable, err.Error()
	}
	return status, http.StatusOK, ""
}

// handleBridgeStatus handles bridge status.
func (s *Server) handleBridgeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `resp`, `status`, and `errMsg` store the error produced by this operation.
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

// handleV1BridgeStatus handles v1 bridge status.
func (s *Server) handleV1BridgeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `resp`, `status`, and `errMsg` store the error produced by this operation.
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

// handleBridgeVerify handles bridge verify.
func (s *Server) handleBridgeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// `proof` stores the value used by this operation.
	var proof BridgeProof
	// `err` stores the error produced by this operation.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ConfigRPCMaxRequestBodyBytes)).Decode(&proof); err != nil {
		http.Error(w, "invalid bridge proof JSON", http.StatusBadRequest)
		return
	}
	// `result` stores the result produced by this operation.
	result := s.verifyBridgeProofWithGatewayState(proof)
	w.Header().Set("Content-Type", "application/json")
	// `status` stores the value produced by this operation.
	status := http.StatusOK
	if !result.Accepted {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

// handleV1BridgeVerify handles v1 bridge verify.
func (s *Server) handleV1BridgeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	// `proof` stores the value used by this operation.
	var proof BridgeProof
	// `err` stores the error produced by this operation.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ConfigRPCMaxRequestBodyBytes)).Decode(&proof); err != nil {
		writeV1Error(w, http.StatusBadRequest, "", "invalid bridge proof JSON")
		return
	}
	// `result` stores the result produced by this operation.
	result := s.verifyBridgeProofWithGatewayState(proof)
	if !result.Accepted {
		writeV1Data(w, http.StatusBadRequest, result)
		return
	}
	writeV1Data(w, http.StatusOK, result)
}
