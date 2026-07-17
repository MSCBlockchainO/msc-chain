package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"
)

const (
	BridgeGatewayVersion    = "msc-bridge-gateway-v6"
	BridgeAccountingVersion = "msc-bridge-accounting-v1"

	BridgeFinalityUnknown   = "unknown"
	BridgeFinalitySyncing   = "syncing"
	BridgeFinalityFinalized = "finalized"
	BridgeFinalityStalled   = "stalled"

	bridgeGatewayStateFile       = "bridge_gateway.json"
	bridgeDepositIntentTTL       = 24 * time.Hour
	bridgeWithdrawalRequestTTL   = 10 * time.Minute
	bridgeCheckpointFreshness    = 15 * time.Minute
	bridgeMaxDepositIntents      = 10000
	bridgeMaxTransfers           = 10000
	bridgeMaxEvents              = 2000
	bridgeMaxPublicHistoryItems  = 100
	bridgeMaxAdminStringLen      = 512
	bridgeMaxBridgeProofBodySize = 2 << 20
	bridgeMSCBurnLogIndex        = uint64(0)

	bridgeTronMainnetChainID          = "tron-mainnet"
	bridgeTronMainnetName             = "TRON Mainnet"
	bridgeTronMainnetNativeSymbol     = "TRX"
	bridgeTronMainnetExplorerURL      = "https://tronscan.org"
	bridgeTronMainnetVerifier         = "tron-solidified-checkpoint-v2"
	bridgeTronMainnetUSDTAddress      = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	bridgeTronMainnetAssetDenom       = "USDT-TRON"
	bridgeTronMainnetLocalDenom       = "mscUSDT"
	bridgeTronMainnetMinConfirmations = uint64(64)
)

type BridgeDepositIntent struct {
	IntentID         string `json:"intent_id"`
	RouteID          string `json:"route_id"`
	SourceChainID    string `json:"source_chain_id"`
	AssetDenom       string `json:"asset_denom"`
	AssetSymbol      string `json:"asset_symbol"`
	Recipient        string `json:"recipient"`
	RequestedAmount  string `json:"requested_amount"`
	DepositAddress   string `json:"deposit_address"`
	TokenContract    string `json:"token_contract"`
	DepositMode      string `json:"deposit_mode"`
	Memo             string `json:"memo,omitempty"`
	Status           string `json:"status"`
	CreatedAtUnix    int64  `json:"created_at_unix"`
	ExpiresAtUnix    int64  `json:"expires_at_unix"`
	SourceTxHash     string `json:"source_tx_hash,omitempty"`
	SourceLogIndex   uint64 `json:"source_log_index,omitempty"`
	BridgeID         string `json:"bridge_id,omitempty"`
	ReplayKey        string `json:"replay_key,omitempty"`
	MSCTransactionID string `json:"msc_transaction_id,omitempty"`
}

type BridgeTransfer struct {
	TransferID            string                   `json:"transfer_id"`
	IntentID              string                   `json:"intent_id"`
	RouteID               string                   `json:"route_id"`
	Direction             string                   `json:"direction"`
	SourceChainID         string                   `json:"source_chain_id"`
	DestinationChainID    string                   `json:"destination_chain_id,omitempty"`
	AssetDenom            string                   `json:"asset_denom"`
	Recipient             string                   `json:"recipient"`
	Sender                string                   `json:"sender,omitempty"`
	ExternalRecipient     string                   `json:"external_recipient,omitempty"`
	Amount                string                   `json:"amount"`
	SourceTxHash          string                   `json:"source_tx_hash,omitempty"`
	SourceLogIndex        uint64                   `json:"source_log_index,omitempty"`
	BridgeID              string                   `json:"bridge_id"`
	MSCTransactionID      string                   `json:"msc_transaction_id,omitempty"`
	ExternalTransactionID string                   `json:"external_transaction_id,omitempty"`
	ReplayKey             string                   `json:"replay_key"`
	Status                string                   `json:"status"`
	MintInstruction       *BridgeMintInstruction   `json:"mint_instruction,omitempty"`
	BurnInstruction       *BridgeBurnInstruction   `json:"burn_instruction,omitempty"`
	UnlockInstruction     *BridgeUnlockInstruction `json:"unlock_instruction,omitempty"`
	CreatedAtUnix         int64                    `json:"created_at_unix"`
	UpdatedAtUnix         int64                    `json:"updated_at_unix"`
}

type BridgeWithdrawalContext struct {
	BridgeVersion      string `json:"bridge_version"`
	TransferID         string `json:"transfer_id"`
	RouteID            string `json:"route_id"`
	SourceChainID      string `json:"source_chain_id"`
	DestinationChainID string `json:"destination_chain_id"`
	AssetDenom         string `json:"asset_denom"`
	OriginAsset        string `json:"origin_asset"`
	BridgeContract     string `json:"bridge_contract"`
	VaultAddress       string `json:"vault_address"`
	Sender             string `json:"sender"`
	ExternalRecipient  string `json:"external_recipient"`
	ExternalAmount     string `json:"external_amount"`
	LocalTokenID       string `json:"local_token_id"`
	LocalAmount        uint64 `json:"local_amount"`
	AuthorizationHash  string `json:"authorization_hash"`
	RequestNonce       string `json:"request_nonce"`
	ExpiresAtUnix      int64  `json:"expires_at_unix"`
}

type BridgeBurnPayload struct {
	From             string                  `json:"from"`
	TokenID          string                  `json:"token_id"`
	Amount           uint64                  `json:"amount"`
	BridgeWithdrawal BridgeWithdrawalContext `json:"bridge_withdrawal"`
}

type BridgeBurnInstruction struct {
	TokenID             string            `json:"token_id"`
	LocalAmount         uint64            `json:"local_amount"`
	TokenDecimals       uint8             `json:"token_decimals"`
	ExternalDecimals    uint8             `json:"external_decimals"`
	BurnPayload         BridgeBurnPayload `json:"burn_payload"`
	ContextHash         string            `json:"context_hash"`
	TransactionTemplate Transaction       `json:"transaction_template"`
}

type BridgeUnlockCertificatePayload struct {
	BridgeVersion        string `json:"bridge_version"`
	BridgeID             string `json:"bridge_id"`
	TransferID           string `json:"transfer_id"`
	MSCBurnTransactionID string `json:"msc_burn_transaction_id"`
	MSCBurnLogIndex      uint64 `json:"msc_burn_log_index"`
	MSCBurnHeight        uint64 `json:"msc_burn_height"`
	ExternalWithdrawalID string `json:"external_withdrawal_id"`
	DestinationChainID   string `json:"destination_chain_id"`
	AssetDenom           string `json:"asset_denom"`
	OriginAsset          string `json:"origin_asset"`
	BridgeContract       string `json:"bridge_contract"`
	VaultAddress         string `json:"vault_address"`
	ExternalRecipient    string `json:"external_recipient"`
	ExternalAmount       string `json:"external_amount"`
}

type BridgeUnlockInstruction struct {
	CertificatePayload          BridgeUnlockCertificatePayload `json:"certificate_payload"`
	CertificatePayloadHash      string                         `json:"certificate_payload_hash"`
	ValidatorSigningMessage     string                         `json:"validator_signing_message"`
	ValidatorSigningBytesHex    string                         `json:"validator_signing_bytes_hex"`
	RequiredValidatorIDs        []string                       `json:"required_validator_ids"`
	RequiredValidatorPublicKeys []string                       `json:"required_validator_public_keys"`
	RequiredQuorum              uint16                         `json:"required_quorum"`
	ValidatorSignatures         []BridgeOracleSignature        `json:"validator_signatures,omitempty"`
	Authorized                  bool                           `json:"authorized"`
}

type BridgeMintInstruction struct {
	TokenID                   string                       `json:"token_id"`
	BridgeID                  string                       `json:"bridge_id"`
	SourceChainID             string                       `json:"source_chain_id"`
	SourceTxHash              string                       `json:"source_tx_hash"`
	SourceLogIndex            uint64                       `json:"source_log_index"`
	SourceAmount              string                       `json:"source_amount"`
	SourceDecimals            uint8                        `json:"source_decimals"`
	TokenDecimals             uint8                        `json:"token_decimals"`
	MintPayload               DTLMintTx                    `json:"mint_payload"`
	ActionPayloadHash         string                       `json:"action_payload_hash"`
	CertificatePayload        BridgeMintCertificatePayload `json:"certificate_payload"`
	CertificatePayloadHash    string                       `json:"certificate_payload_hash"`
	GovernanceCertTemplate    DTLGovernanceCert            `json:"governance_cert_template"`
	GovernanceSigningMessage  string                       `json:"governance_signing_message"`
	GovernanceSigningBytesHex string                       `json:"governance_signing_bytes_hex"`
	RequiredAuthoritySigners  []string                     `json:"required_authority_signers"`
	RequiredThreshold         uint16                       `json:"required_threshold"`
	SubmitEndpoint            string                       `json:"submit_endpoint"`
	ApprovedGovernanceCert    *DTLGovernanceCert           `json:"approved_governance_cert,omitempty"`
	TransactionTemplate       *Transaction                 `json:"transaction_template,omitempty"`
}

type BridgeMintCertificatePayload struct {
	BridgeID           string `json:"bridge_id"`
	BridgeVersion      string `json:"bridge_version"`
	SourceChainID      string `json:"source_chain_id"`
	DestinationChainID string `json:"destination_chain_id"`
	SourceTxHash       string `json:"source_tx_hash"`
	SourceLogIndex     uint64 `json:"source_log_index"`
	AssetDenom         string `json:"asset_denom"`
	LocalTokenID       string `json:"local_token_id"`
	SourceAmount       string `json:"source_amount"`
	MintAmount         uint64 `json:"mint_amount"`
	Receiver           string `json:"receiver"`
	Epoch              uint64 `json:"epoch"`
	Sequence           uint64 `json:"sequence"`
	Expiry             uint64 `json:"expiry"`
	DTLPayloadHash     string `json:"dtl_payload_hash"`
}

type BridgeGatewayEvent struct {
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	RouteID       string `json:"route_id,omitempty"`
	IntentID      string `json:"intent_id,omitempty"`
	TransferID    string `json:"transfer_id,omitempty"`
	ReplayKey     string `json:"replay_key,omitempty"`
	Status        string `json:"status"`
	Detail        string `json:"detail,omitempty"`
	CreatedAtUnix int64  `json:"created_at_unix"`
}

type BridgeGatewayState struct {
	Version                 string                              `json:"version"`
	StateRoot               string                              `json:"state_root"`
	Paused                  bool                                `json:"paused"`
	PauseReason             string                              `json:"pause_reason,omitempty"`
	Chains                  []BridgeChainConfig                 `json:"chains"`
	Assets                  []BridgeAssetConfig                 `json:"assets"`
	Contracts               []BridgeContractConfig              `json:"contracts"`
	Validators              []BridgeValidatorConfig             `json:"validators"`
	Checkpoints             map[string]BridgeFinalityCheckpoint `json:"checkpoints"`
	LatestCheckpointByChain map[string]string                   `json:"latest_checkpoint_by_chain"`
	DepositIntents          map[string]BridgeDepositIntent      `json:"deposit_intents"`
	Transfers               map[string]BridgeTransfer           `json:"transfers"`
	ProcessedEvents         map[string]string                   `json:"processed_events"`
	Events                  []BridgeGatewayEvent                `json:"events"`
	UpdatedAtUnix           int64                               `json:"updated_at_unix"`
}

type BridgeRouteView struct {
	RouteID               string `json:"route_id"`
	ChainID               string `json:"chain_id"`
	ChainName             string `json:"chain_name"`
	ChainType             string `json:"chain_type"`
	NativeSymbol          string `json:"native_symbol,omitempty"`
	AssetDenom            string `json:"asset_denom"`
	AssetSymbol           string `json:"asset_symbol"`
	OriginAsset           string `json:"origin_asset"`
	LocalDenom            string `json:"local_denom"`
	Decimals              uint8  `json:"decimals"`
	ContractAddress       string `json:"contract_address,omitempty"`
	DepositAddress        string `json:"deposit_address,omitempty"`
	DepositMode           string `json:"deposit_mode,omitempty"`
	ExecutionAdapter      string `json:"execution_adapter,omitempty"`
	RuntimeCodeHash       string `json:"runtime_code_hash,omitempty"`
	Status                string `json:"status"`
	FinalityStatus        string `json:"finality_status"`
	MinConfirmations      uint64 `json:"min_confirmations"`
	MinDeposit            string `json:"min_deposit,omitempty"`
	DailyLimit            string `json:"daily_limit,omitempty"`
	LatestObservedHeight  uint64 `json:"latest_observed_height,omitempty"`
	LatestFinalizedHeight uint64 `json:"latest_finalized_height,omitempty"`
	LatestCheckpointID    string `json:"latest_checkpoint_id,omitempty"`
	CheckpointHeight      uint64 `json:"checkpoint_height,omitempty"`
	Ready                 bool   `json:"ready"`
	UnavailableReason     string `json:"unavailable_reason,omitempty"`
}

// BridgeAccountingRoute identifies one external escrow contributing backing to
// a local wrapped token. Values remain raw integers so monitoring never loses
// precision through JSON floating-point conversion.
type BridgeAccountingRoute struct {
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

type BridgeAccountingAsset struct {
	LocalTokenID       string                  `json:"local_token_id"`
	LocalDenom         string                  `json:"local_denom"`
	Symbol             string                  `json:"symbol"`
	Decimals           uint8                   `json:"decimals"`
	TotalSupplyRaw     string                  `json:"total_supply_raw"`
	MaxSupplyRaw       string                  `json:"max_supply_raw"`
	Paused             bool                    `json:"paused"`
	AuthorityThreshold uint16                  `json:"authority_threshold"`
	Routes             []BridgeAccountingRoute `json:"routes"`
}

type BridgeAccountingSnapshot struct {
	Version         string                  `json:"version"`
	BridgeVersion   string                  `json:"bridge_version"`
	StateRoot       string                  `json:"state_root"`
	MSCHeight       uint64                  `json:"msc_height"`
	GeneratedAtUnix int64                   `json:"generated_at_unix"`
	BridgePaused    bool                    `json:"bridge_paused"`
	Assets          []BridgeAccountingAsset `json:"assets"`
}

type bridgeDepositRequest struct {
	RouteID   string `json:"route_id"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
}

type bridgeWithdrawalRequest struct {
	RouteID           string `json:"route_id"`
	Sender            string `json:"sender"`
	ExternalRecipient string `json:"external_recipient"`
	Amount            string `json:"amount"`
	RequestNonce      string `json:"request_nonce"`
	ExpiresAtUnix     int64  `json:"expires_at_unix"`
	PublicKey         string `json:"public_key"`
	Signature         string `json:"signature"`
}

type bridgeEventSubmission struct {
	IntentID string      `json:"intent_id"`
	Proof    BridgeProof `json:"proof"`
}

type bridgeMintPrepareRequest struct {
	TransferID     string            `json:"transfer_id"`
	Proposer       string            `json:"proposer"`
	GovernanceCert DTLGovernanceCert `json:"governance_cert"`
}

type bridgeMintConfirmRequest struct {
	TransferID       string `json:"transfer_id"`
	MSCTransactionID string `json:"msc_transaction_id"`
}

type bridgeBurnConfirmRequest struct {
	TransferID       string `json:"transfer_id"`
	MSCTransactionID string `json:"msc_transaction_id"`
}

type bridgeUnlockAuthorizeRequest struct {
	TransferID string                  `json:"transfer_id"`
	Signatures []BridgeOracleSignature `json:"signatures"`
}

type bridgeUnlockConfirmRequest struct {
	TransferID string      `json:"transfer_id"`
	Proof      BridgeProof `json:"proof"`
}

type bridgeSettingsRequest struct {
	Paused      bool   `json:"paused"`
	PauseReason string `json:"pause_reason"`
}

func normalizeBridgeRegistryID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return ""
	}
	return value
}

func normalizeBridgeRouteStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BridgeRouteStatusSetupRequired:
		return BridgeRouteStatusSetupRequired
	case BridgeRouteStatusTesting:
		return BridgeRouteStatusTesting
	case BridgeRouteStatusActive:
		return BridgeRouteStatusActive
	case BridgeRouteStatusPaused:
		return BridgeRouteStatusPaused
	case BridgeRouteStatusDisabled:
		return BridgeRouteStatusDisabled
	default:
		return ""
	}
}

func normalizeBridgeFinalityStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BridgeFinalityUnknown:
		return BridgeFinalityUnknown
	case BridgeFinalitySyncing:
		return BridgeFinalitySyncing
	case BridgeFinalityFinalized:
		return BridgeFinalityFinalized
	case BridgeFinalityStalled:
		return BridgeFinalityStalled
	default:
		return ""
	}
}

func normalizeBridgeChainType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "evm", "tron", "solana", "utxo", "msc-compatible", "ibc":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeBridgeExecutionAdapter(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BridgeExecutionEVMVaultV1:
		return BridgeExecutionEVMVaultV1
	case BridgeExecutionTronVaultV1:
		return BridgeExecutionTronVaultV1
	case BridgeExecutionSolanaProgramV1:
		return BridgeExecutionSolanaProgramV1
	default:
		return ""
	}
}

func bridgeExecutionAdapterCompatible(chainType, adapter string) bool {
	switch normalizeBridgeChainType(chainType) {
	case "evm":
		return normalizeBridgeExecutionAdapter(adapter) == BridgeExecutionEVMVaultV1
	case "tron":
		return normalizeBridgeExecutionAdapter(adapter) == BridgeExecutionTronVaultV1
	case "solana":
		return normalizeBridgeExecutionAdapter(adapter) == BridgeExecutionSolanaProgramV1
	default:
		return false
	}
}

func bridgeExecutionAdapterImplemented(chainType, adapter string) bool {
	switch normalizeBridgeChainType(chainType) {
	case "evm":
		return normalizeBridgeExecutionAdapter(adapter) == BridgeExecutionEVMVaultV1
	case "tron":
		return normalizeBridgeExecutionAdapter(adapter) == BridgeExecutionTronVaultV1
	default:
		return false
	}
}

func validBridgeRuntimeCodeHash(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 66 || !strings.HasPrefix(value, "0x") || strings.Trim(value[2:], "0") == "" {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func validBridgeDeploymentTxHash(chainType, value string) bool {
	value = strings.TrimSpace(value)
	switch normalizeBridgeChainType(chainType) {
	case "evm":
		if len(value) != 66 || !strings.HasPrefix(strings.ToLower(value), "0x") {
			return false
		}
		_, err := hex.DecodeString(value[2:])
		return err == nil && strings.Trim(value[2:], "0") != ""
	case "tron":
		value = strings.TrimPrefix(strings.ToLower(value), "0x")
		if len(value) != 64 || strings.Trim(value, "0") == "" {
			return false
		}
		_, err := hex.DecodeString(value)
		return err == nil
	default:
		return false
	}
}

func validBridgeAuditReference(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func normalizeBridgeDepositMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "contract_call", "memo", "direct_transfer":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func validBridgePublicURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func isASCIIAlphaNumeric(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return value != ""
}

func decodeBridgeBase58(value string) ([]byte, error) {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty base58 value")
	}
	number := new(big.Int)
	base := big.NewInt(58)
	for index := 0; index < len(value); index++ {
		digit := strings.IndexByte(alphabet, value[index])
		if digit < 0 {
			return nil, fmt.Errorf("invalid base58 character")
		}
		number.Mul(number, base)
		number.Add(number, big.NewInt(int64(digit)))
	}
	decoded := number.Bytes()
	leadingZeroes := 0
	for leadingZeroes < len(value) && value[leadingZeroes] == '1' {
		leadingZeroes++
	}
	out := make([]byte, leadingZeroes+len(decoded))
	copy(out[leadingZeroes:], decoded)
	return out, nil
}

func validTronBase58CheckAddress(value string) bool {
	decoded, err := decodeBridgeBase58(value)
	if err != nil || len(decoded) != 25 || decoded[0] != 0x41 {
		return false
	}
	first := sha256.Sum256(decoded[:21])
	second := sha256.Sum256(first[:])
	return subtle.ConstantTimeCompare(decoded[21:], second[:4]) == 1
}

func validExternalBridgeAddress(chainType, value string) bool {
	value = strings.TrimSpace(value)
	switch normalizeBridgeChainType(chainType) {
	case "evm":
		if len(value) != 42 || !strings.HasPrefix(strings.ToLower(value), "0x") {
			return false
		}
		_, err := hex.DecodeString(value[2:])
		return err == nil
	case "tron":
		return len(value) == 34 && strings.HasPrefix(value, "T") && validTronBase58CheckAddress(value)
	case "solana":
		decoded, err := decodeBridgeBase58(value)
		return err == nil && len(decoded) == 32
	case "utxo":
		return len(value) >= 26 && len(value) <= 90 && isASCIIAlphaNumeric(value)
	case "msc-compatible", "ibc":
		return len(value) >= 8 && len(value) <= 128
	default:
		return false
	}
}

func bridgeDecimalUnits(value string, decimals uint8) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("amount required")
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") || strings.Count(value, ".") > 1 {
		return nil, fmt.Errorf("invalid decimal amount")
	}
	parts := strings.SplitN(value, ".", 2)
	if parts[0] == "" {
		parts[0] = "0"
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > int(decimals) || len(parts[0]) > 40 || len(frac) > 40 {
		return nil, fmt.Errorf("amount precision exceeds asset decimals")
	}
	if !allDecimalDigits(parts[0]) || (frac != "" && !allDecimalDigits(frac)) {
		return nil, fmt.Errorf("invalid decimal amount")
	}
	frac += strings.Repeat("0", int(decimals)-len(frac))
	units := new(big.Int)
	if _, ok := units.SetString(strings.TrimLeft(parts[0]+frac, "0"), 10); !ok {
		if strings.Trim(parts[0]+frac, "0") == "" {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("invalid decimal amount")
	}
	return units, nil
}

func bridgeTokenUnits(value string, sourceDecimals, tokenDecimals uint8) (uint64, error) {
	units, err := bridgeDecimalUnits(value, sourceDecimals)
	if err != nil || units.Sign() <= 0 {
		return 0, fmt.Errorf("bridge amount must be positive")
	}
	if tokenDecimals > sourceDecimals {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tokenDecimals-sourceDecimals)), nil)
		units.Mul(units, factor)
	} else if sourceDecimals > tokenDecimals {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(sourceDecimals-tokenDecimals)), nil)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(units, factor, remainder)
		if remainder.Sign() != 0 {
			return 0, fmt.Errorf("bridge amount cannot be represented exactly by local token decimals")
		}
		units = quotient
	}
	if !units.IsUint64() || units.Sign() <= 0 {
		return 0, fmt.Errorf("bridge amount exceeds local token range")
	}
	return units.Uint64(), nil
}

func bridgeCanonicalWithdrawalAuthorization(request bridgeWithdrawalRequest) string {
	return strings.Join([]string{
		"MSC", "BRIDGE_WITHDRAWAL_REQUEST", BridgeGatewayVersion,
		normalizeBridgeRegistryID(request.RouteID),
		displayAddress(request.Sender),
		strings.TrimSpace(request.ExternalRecipient),
		strings.TrimSpace(request.Amount),
		strings.ToLower(strings.TrimSpace(request.RequestNonce)),
		fmt.Sprintf("%d", request.ExpiresAtUnix),
	}, "|")
}

func verifyBridgeWithdrawalAuthorization(request bridgeWithdrawalRequest, now time.Time) (string, error) {
	if _, err := decodeAddressPayload(request.Sender); err != nil {
		return "", fmt.Errorf("sender must be a valid MSC address")
	}
	if request.ExpiresAtUnix <= now.Unix() || request.ExpiresAtUnix > now.Add(bridgeWithdrawalRequestTTL).Unix() {
		return "", fmt.Errorf("withdrawal authorization expiry must be within %s", bridgeWithdrawalRequestTTL)
	}
	nonce := strings.ToLower(strings.TrimSpace(request.RequestNonce))
	if len(nonce) != 64 {
		return "", fmt.Errorf("request_nonce must be 32-byte hex")
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return "", fmt.Errorf("request_nonce must be 32-byte hex")
	}
	pubRaw, err := hex.DecodeString(strings.TrimSpace(request.PublicKey))
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return "", fmt.Errorf("public_key must be 32-byte Ed25519 hex")
	}
	pub := ed25519.PublicKey(pubRaw)
	if !AddressMatchesPublicKey(request.Sender, pub) {
		return "", fmt.Errorf("public_key does not control sender")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(request.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", fmt.Errorf("signature must be 64-byte Ed25519 hex")
	}
	message := bridgeCanonicalWithdrawalAuthorization(request)
	if !ed25519.Verify(pub, []byte(message), sig) {
		return "", fmt.Errorf("withdrawal authorization signature invalid")
	}
	return "withdrawal-auth:" + lightHashString(message), nil
}

func bridgeExternalAddressEqual(chainType, a, b string) bool {
	if normalizeBridgeChainType(chainType) == "evm" {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func bridgeChainForID(chains []BridgeChainConfig, chainID string) (BridgeChainConfig, bool) {
	for _, chain := range chains {
		if strings.EqualFold(strings.TrimSpace(chain.ChainID), strings.TrimSpace(chainID)) {
			return chain, true
		}
	}
	return BridgeChainConfig{}, false
}

func bridgeContractForRoute(contracts []BridgeContractConfig, routeID string) (BridgeContractConfig, bool) {
	for _, contract := range contracts {
		if normalizeBridgeRegistryID(contract.RouteID) == normalizeBridgeRegistryID(routeID) {
			return contract, true
		}
	}
	return BridgeContractConfig{}, false
}

func buildBridgeBurnInstruction(state BridgeGatewayState, ledger Ledger, route BridgeRouteView, request bridgeWithdrawalRequest, transferID, authorizationHash string, nonce int) (BridgeBurnInstruction, string, error) {
	asset, found := bridgeAssetForDenom(state.Assets, route.AssetDenom)
	if !found {
		return BridgeBurnInstruction{}, "", fmt.Errorf("bridge asset is not registered")
	}
	contract, found := bridgeContractForRoute(state.Contracts, route.RouteID)
	if !found {
		return BridgeBurnInstruction{}, "", fmt.Errorf("bridge contract is not registered")
	}
	tokenID, token, found := bridgeLocalToken(ledger.DTL, asset)
	if !found {
		return BridgeBurnInstruction{}, "", fmt.Errorf("local wrapped token is not registered")
	}
	localAmount, err := bridgeTokenUnits(request.Amount, asset.Decimals, token.Decimals)
	if err != nil {
		return BridgeBurnInstruction{}, "", err
	}
	burn := DTLBurnTx{From: displayAddress(request.Sender), TokenID: tokenID, Amount: localAmount}
	if err := ValidateDTLBurnTx(ledger.DTL, burn); err != nil {
		return BridgeBurnInstruction{}, "", err
	}
	context := BridgeWithdrawalContext{
		BridgeVersion: BridgeProtocolVersion, TransferID: transferID, RouteID: route.RouteID,
		SourceChainID: protocolChainID(), DestinationChainID: route.ChainID,
		AssetDenom: route.AssetDenom, OriginAsset: route.OriginAsset,
		BridgeContract: contract.ContractAddress, VaultAddress: contract.DepositAddress,
		Sender: burn.From, ExternalRecipient: strings.TrimSpace(request.ExternalRecipient),
		ExternalAmount: strings.TrimSpace(request.Amount), LocalTokenID: tokenID, LocalAmount: localAmount,
		AuthorizationHash: authorizationHash, RequestNonce: strings.ToLower(strings.TrimSpace(request.RequestNonce)),
		ExpiresAtUnix: request.ExpiresAtUnix,
	}
	contextHash, err := DTLPayloadHash(context)
	if err != nil {
		return BridgeBurnInstruction{}, "", err
	}
	payload := BridgeBurnPayload{From: burn.From, TokenID: tokenID, Amount: localAmount, BridgeWithdrawal: context}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return BridgeBurnInstruction{}, "", err
	}
	tx := Transaction{
		From: burn.From, To: burn.From, Amount: 0,
		Nonce:  nonce,
		Expiry: request.ExpiresAtUnix, Type: TxDTL, ChainID: protocolChainID(), Coin: CoinSymbol,
		DTLTxType: string(DTLTxTokenBurn), DTLTokenID: tokenID, DTLPayload: string(payloadJSON),
	}
	return BridgeBurnInstruction{
		TokenID: tokenID, LocalAmount: localAmount, TokenDecimals: token.Decimals,
		ExternalDecimals: asset.Decimals, BurnPayload: payload, ContextHash: contextHash,
		TransactionTemplate: tx,
	}, "bridge_withdrawal_" + contextHash, nil
}

func bridgeMintPayloadHash(mint DTLMintTx) (string, error) {
	return DTLPayloadHash(struct {
		TokenID string `json:"token_id"`
		To      string `json:"to"`
		Amount  uint64 `json:"amount"`
	}{
		TokenID: normalizeDTLTokenID(mint.TokenID),
		To:      normalizeDTLAccount(mint.To),
		Amount:  mint.Amount,
	})
}

func bridgeMintSequence(state BridgeGatewayState, dtl *DTLState, tokenID string) uint64 {
	last := uint64(0)
	if dtl != nil {
		last = dtl.GovernanceReplay[normalizeDTLTokenID(tokenID)+"|v2|sequence"]
	}
	for _, transfer := range state.Transfers {
		instruction := transfer.MintInstruction
		if instruction == nil || !strings.EqualFold(instruction.TokenID, tokenID) {
			continue
		}
		if instruction.GovernanceCertTemplate.Sequence > last {
			last = instruction.GovernanceCertTemplate.Sequence
		}
	}
	return last + 1
}

func buildBridgeMintInstruction(state BridgeGatewayState, ledger Ledger, currentHeight uint64, proof BridgeProof, asset BridgeAssetConfig) (BridgeMintInstruction, error) {
	tokenID, token, found := bridgeLocalToken(ledger.DTL, asset)
	if !found {
		return BridgeMintInstruction{}, fmt.Errorf("local wrapped token is not registered: %s", asset.LocalDenom)
	}
	if ready, reason := bridgeLocalTokenReadiness(ledger.DTL, asset); !ready {
		return BridgeMintInstruction{}, fmt.Errorf("local wrapped token is not ready: %s", reason)
	}
	amount, err := bridgeTokenUnits(proof.Amount, asset.Decimals, token.Decimals)
	if err != nil {
		return BridgeMintInstruction{}, err
	}
	mint := DTLMintTx{To: strings.TrimSpace(proof.Recipient), TokenID: tokenID, Amount: amount}
	if err := ValidateDTLMintTx(ledger.DTL, mint); err != nil {
		return BridgeMintInstruction{}, err
	}
	payloadHash, err := bridgeMintPayloadHash(mint)
	if err != nil {
		return BridgeMintInstruction{}, err
	}
	id := bridgeID(proof)
	if id == "" {
		return BridgeMintInstruction{}, fmt.Errorf("canonical bridge id unavailable")
	}
	sequence := bridgeMintSequence(state, ledger.DTL, tokenID)
	expiry := currentHeight + DTLDefaultReplayWindow
	certificatePayload := BridgeMintCertificatePayload{
		BridgeID: id, BridgeVersion: BridgeProtocolVersion,
		SourceChainID: strings.TrimSpace(proof.SourceChainID), DestinationChainID: protocolChainID(),
		SourceTxHash: bridgeSourceTransactionHash(proof), SourceLogIndex: proof.LogIndex,
		AssetDenom: strings.TrimSpace(proof.AssetDenom), LocalTokenID: tokenID,
		SourceAmount: strings.TrimSpace(proof.Amount), MintAmount: amount,
		Receiver: strings.TrimSpace(proof.Recipient), Epoch: currentHeight,
		Sequence: sequence, Expiry: expiry, DTLPayloadHash: payloadHash,
	}
	certificatePayloadHash, err := DTLPayloadHash(certificatePayload)
	if err != nil {
		return BridgeMintInstruction{}, err
	}
	cert := DTLGovernanceCert{
		TokenID:           tokenID,
		Epoch:             currentHeight,
		Nonce:             "bridge-mint-" + id + "-" + certificatePayloadHash[:40],
		Sequence:          sequence,
		Expiry:            expiry,
		Action:            DTLGovMint,
		ActionPayloadHash: payloadHash,
	}
	signBytes := DTLGovernanceCertSignBytesV2(
		cert.TokenID, cert.Epoch, cert.Nonce, cert.Sequence, cert.Expiry,
		cert.Action, cert.ActionPayloadHash,
	)
	return BridgeMintInstruction{
		TokenID:                   tokenID,
		BridgeID:                  id,
		SourceChainID:             strings.TrimSpace(proof.SourceChainID),
		SourceTxHash:              bridgeSourceTransactionHash(proof),
		SourceLogIndex:            proof.LogIndex,
		SourceAmount:              strings.TrimSpace(proof.Amount),
		SourceDecimals:            asset.Decimals,
		TokenDecimals:             token.Decimals,
		MintPayload:               mint,
		ActionPayloadHash:         payloadHash,
		CertificatePayload:        certificatePayload,
		CertificatePayloadHash:    certificatePayloadHash,
		GovernanceCertTemplate:    cert,
		GovernanceSigningMessage:  string(signBytes),
		GovernanceSigningBytesHex: hex.EncodeToString(signBytes),
		RequiredAuthoritySigners:  append([]string(nil), token.AuthoritySigners...),
		RequiredThreshold:         token.AuthorityThreshold,
		SubmitEndpoint:            "/submitTx",
	}, nil
}

func allDecimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func bridgeAssetSymbol(asset BridgeAssetConfig) string {
	if symbol := strings.ToUpper(strings.TrimSpace(asset.Symbol)); symbol != "" {
		return symbol
	}
	denom := strings.ToUpper(strings.TrimSpace(asset.Denom))
	if at := strings.IndexAny(denom, "-_:."); at > 0 {
		return denom[:at]
	}
	return denom
}

func initialBridgeGatewayState() BridgeGatewayState {
	BridgeRegistryMu.RLock()
	chains := append([]BridgeChainConfig(nil), BridgeChains...)
	assets := append([]BridgeAssetConfig(nil), BridgeAssets...)
	validators := append([]BridgeValidatorConfig(nil), BridgeValidators...)
	BridgeRegistryMu.RUnlock()
	return BridgeGatewayState{
		Version:                 BridgeGatewayVersion,
		Paused:                  true,
		PauseReason:             "Bridge contracts, finality trackers, and governance activation are required.",
		Chains:                  chains,
		Assets:                  assets,
		Validators:              validators,
		Checkpoints:             make(map[string]BridgeFinalityCheckpoint),
		LatestCheckpointByChain: make(map[string]string),
		DepositIntents:          make(map[string]BridgeDepositIntent),
		Transfers:               make(map[string]BridgeTransfer),
		ProcessedEvents:         make(map[string]string),
		UpdatedAtUnix:           time.Now().UTC().Unix(),
	}
}

func bridgeGatewayStateRoot(state BridgeGatewayState) (string, error) {
	state.StateRoot = ""
	state.UpdatedAtUnix = 0
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeBridgeGatewayState(state *BridgeGatewayState) {
	if state == nil {
		return
	}
	state.Version = BridgeGatewayVersion
	if state.Checkpoints == nil {
		state.Checkpoints = make(map[string]BridgeFinalityCheckpoint)
	}
	if state.LatestCheckpointByChain == nil {
		state.LatestCheckpointByChain = make(map[string]string)
	}
	if state.DepositIntents == nil {
		state.DepositIntents = make(map[string]BridgeDepositIntent)
	}
	if state.Transfers == nil {
		state.Transfers = make(map[string]BridgeTransfer)
	}
	if state.ProcessedEvents == nil {
		state.ProcessedEvents = make(map[string]string)
	}
	for index := range state.Contracts {
		contract := &state.Contracts[index]
		if contract.RouteID == "" {
			contract.RouteID = contract.ContractID
		}
		if contract.ContractAddress == "" {
			contract.ContractAddress = contract.Address
		}
		if contract.ContractID == "" {
			contract.ContractID = contract.RouteID
		}
		if contract.Address == "" {
			contract.Address = contract.ContractAddress
		}
		contract.ExecutionAdapter = normalizeBridgeExecutionAdapter(contract.ExecutionAdapter)
		contract.RuntimeCodeHash = strings.ToLower(strings.TrimSpace(contract.RuntimeCodeHash))
	}
	if len(state.Events) > bridgeMaxEvents {
		state.Events = append([]BridgeGatewayEvent(nil), state.Events[len(state.Events)-bridgeMaxEvents:]...)
	}
}

func (s *Server) bridgeGatewayStatePath() string {
	if s == nil || s.Node == nil || strings.TrimSpace(s.Node.DataDir) == "" || strings.TrimSpace(s.Node.ID) == "" {
		return ""
	}
	return filepath.Join(nodeDataPath(s.Node.DataDir, s.Node.ID), bridgeGatewayStateFile)
}

func replaceBridgeRuntimeRegistry(state BridgeGatewayState) {
	BridgeRegistryMu.Lock()
	BridgeChains = append([]BridgeChainConfig(nil), state.Chains...)
	BridgeAssets = append([]BridgeAssetConfig(nil), state.Assets...)
	BridgeValidators = append([]BridgeValidatorConfig(nil), state.Validators...)
	BridgeRegistryMu.Unlock()
}

func (s *Server) ensureBridgeGatewayState() error {
	if s == nil || s.Node == nil {
		return fmt.Errorf("node unavailable")
	}
	s.bridgeMu.Lock()
	defer s.bridgeMu.Unlock()
	if s.bridgeLoaded {
		return nil
	}
	state := initialBridgeGatewayState()
	path := s.bridgeGatewayStatePath()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err == nil {
			var persisted BridgeGatewayState
			if err := json.Unmarshal(raw, &persisted); err != nil {
				return fmt.Errorf("decode bridge gateway state: %w", err)
			}
			if persistedRoot := strings.TrimSpace(persisted.StateRoot); persistedRoot != "" {
				computedRoot, rootErr := bridgeGatewayStateRoot(persisted)
				if rootErr != nil || !strings.EqualFold(persistedRoot, computedRoot) {
					return fmt.Errorf("bridge gateway state root mismatch: persisted=%s computed=%s", persistedRoot, computedRoot)
				}
			}
			state = persisted
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("read bridge gateway state: %w", err)
		}
	}
	normalizeBridgeGatewayState(&state)
	state.StateRoot, _ = bridgeGatewayStateRoot(state)
	s.bridgeState = state
	s.bridgeLoaded = true
	replaceBridgeRuntimeRegistry(state)
	return nil
}

func (s *Server) persistBridgeGatewayStateLocked() error {
	if s == nil {
		return fmt.Errorf("bridge gateway unavailable")
	}
	s.bridgeState.UpdatedAtUnix = time.Now().UTC().Unix()
	normalizeBridgeGatewayState(&s.bridgeState)
	stateRoot, err := bridgeGatewayStateRoot(s.bridgeState)
	if err != nil {
		return err
	}
	s.bridgeState.StateRoot = stateRoot
	path := s.bridgeGatewayStatePath()
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(s.bridgeState, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, raw, 0o600)
}

func cloneBridgeGatewayState(state BridgeGatewayState) BridgeGatewayState {
	out := state
	out.Chains = append([]BridgeChainConfig(nil), state.Chains...)
	out.Assets = append([]BridgeAssetConfig(nil), state.Assets...)
	out.Contracts = append([]BridgeContractConfig(nil), state.Contracts...)
	out.Validators = append([]BridgeValidatorConfig(nil), state.Validators...)
	out.Events = append([]BridgeGatewayEvent(nil), state.Events...)
	out.Checkpoints = make(map[string]BridgeFinalityCheckpoint, len(state.Checkpoints))
	for key, value := range state.Checkpoints {
		value.ValidatorSignatures = append([]BridgeOracleSignature(nil), value.ValidatorSignatures...)
		out.Checkpoints[key] = value
	}
	out.LatestCheckpointByChain = make(map[string]string, len(state.LatestCheckpointByChain))
	for key, value := range state.LatestCheckpointByChain {
		out.LatestCheckpointByChain[key] = value
	}
	out.DepositIntents = make(map[string]BridgeDepositIntent, len(state.DepositIntents))
	for key, value := range state.DepositIntents {
		out.DepositIntents[key] = value
	}
	out.Transfers = make(map[string]BridgeTransfer, len(state.Transfers))
	for key, value := range state.Transfers {
		if value.MintInstruction != nil {
			instruction := *value.MintInstruction
			instruction.RequiredAuthoritySigners = append([]string(nil), value.MintInstruction.RequiredAuthoritySigners...)
			instruction.GovernanceCertTemplate.Signers = append([]string(nil), value.MintInstruction.GovernanceCertTemplate.Signers...)
			instruction.GovernanceCertTemplate.SignerPublicKeys = append([]string(nil), value.MintInstruction.GovernanceCertTemplate.SignerPublicKeys...)
			instruction.GovernanceCertTemplate.Signatures = append([]string(nil), value.MintInstruction.GovernanceCertTemplate.Signatures...)
			if value.MintInstruction.ApprovedGovernanceCert != nil {
				cert := *value.MintInstruction.ApprovedGovernanceCert
				cert.Signers = append([]string(nil), cert.Signers...)
				cert.SignerPublicKeys = append([]string(nil), cert.SignerPublicKeys...)
				cert.Signatures = append([]string(nil), cert.Signatures...)
				instruction.ApprovedGovernanceCert = &cert
			}
			if value.MintInstruction.TransactionTemplate != nil {
				tx := *value.MintInstruction.TransactionTemplate
				instruction.TransactionTemplate = &tx
			}
			value.MintInstruction = &instruction
		}
		if value.BurnInstruction != nil {
			instruction := *value.BurnInstruction
			value.BurnInstruction = &instruction
		}
		if value.UnlockInstruction != nil {
			instruction := *value.UnlockInstruction
			instruction.RequiredValidatorIDs = append([]string(nil), instruction.RequiredValidatorIDs...)
			instruction.RequiredValidatorPublicKeys = append([]string(nil), instruction.RequiredValidatorPublicKeys...)
			instruction.ValidatorSignatures = append([]BridgeOracleSignature(nil), instruction.ValidatorSignatures...)
			value.UnlockInstruction = &instruction
		}
		out.Transfers[key] = value
	}
	out.ProcessedEvents = make(map[string]string, len(state.ProcessedEvents))
	for key, value := range state.ProcessedEvents {
		out.ProcessedEvents[key] = value
	}
	return out
}

func (s *Server) bridgeGatewaySnapshot() (BridgeGatewayState, error) {
	if err := s.ensureBridgeGatewayState(); err != nil {
		return BridgeGatewayState{}, err
	}
	s.bridgeMu.RLock()
	defer s.bridgeMu.RUnlock()
	return cloneBridgeGatewayState(s.bridgeState), nil
}

func bridgeContractForAsset(contracts []BridgeContractConfig, assetDenom string) (BridgeContractConfig, bool) {
	assetDenom = strings.ToLower(strings.TrimSpace(assetDenom))
	for _, contract := range contracts {
		if strings.ToLower(strings.TrimSpace(contract.AssetDenom)) == assetDenom {
			return contract, true
		}
	}
	return BridgeContractConfig{}, false
}

func bridgeAssetForDenom(assets []BridgeAssetConfig, assetDenom string) (BridgeAssetConfig, bool) {
	assetDenom = strings.ToLower(strings.TrimSpace(assetDenom))
	for _, asset := range assets {
		if strings.ToLower(strings.TrimSpace(asset.Denom)) == assetDenom {
			return asset, true
		}
	}
	return BridgeAssetConfig{}, false
}

func activeBridgeValidatorCount(validators []BridgeValidatorConfig) int {
	seen := make(map[string]struct{}, len(validators))
	for _, validator := range validators {
		if !strings.EqualFold(strings.TrimSpace(validator.Status), BridgeRouteStatusActive) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(validator.PublicKey))
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			continue
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}

func bridgeLocalToken(state *DTLState, asset BridgeAssetConfig) (string, *DTLTokenState, bool) {
	if state == nil {
		return "", nil, false
	}
	tokenID, ok := resolveDTLTokenRef(state, asset.LocalDenom)
	if !ok {
		return "", nil, false
	}
	token := state.Tokens[tokenID]
	return tokenID, token, token != nil
}

func bridgeLocalTokenReadiness(state *DTLState, asset BridgeAssetConfig) (bool, string) {
	_, token, found := bridgeLocalToken(state, asset)
	if !found {
		return false, "local_wrapped_token_not_registered"
	}
	if token.Paused {
		return false, "local_wrapped_token_paused"
	}
	unique := make(map[string]struct{}, len(token.AuthoritySigners))
	for _, signer := range token.AuthoritySigners {
		signer = normalizeDTLAccount(signer)
		if signer != "" && isMSCAddressLike(signer) {
			unique[signer] = struct{}{}
		}
	}
	if token.AuthorityThreshold == 0 || int(token.AuthorityThreshold) > len(unique) {
		return false, "local_mint_authority_threshold_not_ready"
	}
	if token.MaxSupply > 0 && token.TotalSupply >= token.MaxSupply {
		return false, "local_wrapped_token_max_supply_reached"
	}
	return true, ""
}

func bridgeLatestCheckpoint(state BridgeGatewayState, chainID string) (BridgeFinalityCheckpoint, bool) {
	key := normalizeBridgeRegistryID(chainID)
	id := strings.ToLower(strings.TrimSpace(state.LatestCheckpointByChain[key]))
	if id == "" {
		return BridgeFinalityCheckpoint{}, false
	}
	checkpoint, ok := state.Checkpoints[id]
	return checkpoint, ok
}

func bridgeCheckpointIsFresh(checkpoint BridgeFinalityCheckpoint, now time.Time) bool {
	issued := time.Unix(checkpoint.IssuedAtUnix, 0)
	return checkpoint.IssuedAtUnix > 0 && !issued.After(now.Add(5*time.Minute)) && !issued.Before(now.Add(-bridgeCheckpointFreshness))
}

func bridgeRouteReadiness(state BridgeGatewayState, chain BridgeChainConfig, asset BridgeAssetConfig, contract BridgeContractConfig, found bool, localDTL ...*DTLState) (bool, string) {
	if !BridgeEnabled || normalizeBridgeMode(BridgeMode) == BridgeModeDisabled {
		return false, "bridge_disabled_by_node_config"
	}
	if normalizeBridgeMode(BridgeMode) != BridgeModeGuardedMint {
		return false, "guarded_mint_not_activated"
	}
	if state.Paused {
		return false, "emergency_pause_active"
	}
	if normalizeBridgeRouteStatus(chain.Status) != BridgeRouteStatusActive {
		return false, "source_chain_not_active"
	}
	if normalizeBridgeFinalityStatus(chain.FinalityStatus) != BridgeFinalityFinalized {
		return false, "source_chain_finality_not_ready"
	}
	if normalizeBridgeRouteStatus(asset.Status) != BridgeRouteStatusActive {
		return false, "asset_not_active"
	}
	if normalizeBridgeChainType(chain.ChainType) == "tron" && !isCanonicalTronMainnetAsset(chain, asset) {
		return false, "unsupported_tron_route_identity"
	}
	if !validExternalBridgeAddress(chain.ChainType, asset.OriginAsset) {
		return false, "origin_asset_address_invalid"
	}
	if !found {
		return false, "bridge_contract_not_registered"
	}
	if normalizeBridgeRouteStatus(contract.Status) != BridgeRouteStatusActive {
		return false, "bridge_contract_not_active"
	}
	if normalizeBridgeFinalityStatus(contract.FinalityStatus) != BridgeFinalityFinalized {
		return false, "route_finality_not_ready"
	}
	if !validExternalBridgeAddress(chain.ChainType, contract.ContractAddress) || !validExternalBridgeAddress(chain.ChainType, contract.DepositAddress) {
		return false, "bridge_contract_address_invalid"
	}
	if normalizeBridgeDepositMode(contract.DepositMode) == "" {
		return false, "deposit_mode_invalid"
	}
	if !bridgeExecutionAdapterCompatible(chain.ChainType, contract.ExecutionAdapter) {
		return false, "execution_adapter_incompatible"
	}
	if !bridgeExecutionAdapterImplemented(chain.ChainType, contract.ExecutionAdapter) {
		return false, "execution_adapter_not_implemented"
	}
	if !validBridgeRuntimeCodeHash(contract.RuntimeCodeHash) {
		return false, "runtime_code_hash_invalid"
	}
	if (normalizeBridgeExecutionAdapter(contract.ExecutionAdapter) == BridgeExecutionEVMVaultV1 ||
		normalizeBridgeExecutionAdapter(contract.ExecutionAdapter) == BridgeExecutionTronVaultV1) &&
		normalizeBridgeDepositMode(contract.DepositMode) == "contract_call" &&
		!bridgeExternalAddressEqual(chain.ChainType, contract.ContractAddress, contract.DepositAddress) {
		return false, "vault_deposit_address_mismatch"
	}
	if !validBridgeAuditReference(contract.AuditReference) ||
		!validBridgeDeploymentTxHash(chain.ChainType, contract.DeploymentTxHash) {
		return false, "contract_audit_or_deployment_evidence_missing"
	}
	trust := normalizeBridgeTrustModel(chain.TrustModel)
	if (trust == BridgeTrustOracleQuorum || trust == BridgeTrustHybrid) && activeBridgeValidatorCount(state.Validators) < int(BridgeOracleQuorum) {
		return false, "bridge_validator_quorum_not_ready"
	}
	checkpoint, checkpointFound := bridgeLatestCheckpoint(state, chain.ChainID)
	if !checkpointFound {
		return false, "source_finality_checkpoint_not_ready"
	}
	if !bridgeCheckpointIsFresh(checkpoint, time.Now().UTC()) {
		return false, "source_finality_checkpoint_stale"
	}
	if _, _, err := verifyBridgeFinalityCheckpoint(checkpoint, chain, state.Validators); err != nil {
		return false, "source_finality_checkpoint_invalid"
	}
	if (trust == BridgeTrustLightClient || trust == BridgeTrustHybrid) && strings.TrimSpace(chain.LightClient) == "" {
		return false, "light_client_not_configured"
	}
	if len(localDTL) > 0 {
		if ready, reason := bridgeLocalTokenReadiness(localDTL[0], asset); !ready {
			return false, reason
		}
	}
	return true, ""
}

func bridgeRoutesFromState(state BridgeGatewayState, localDTL ...*DTLState) []BridgeRouteView {
	chains := make(map[string]BridgeChainConfig, len(state.Chains))
	for _, chain := range state.Chains {
		chains[strings.ToLower(strings.TrimSpace(chain.ChainID))] = chain
	}
	routes := make([]BridgeRouteView, 0, len(state.Assets))
	for _, asset := range state.Assets {
		chain, chainFound := chains[strings.ToLower(strings.TrimSpace(asset.OriginChain))]
		contract, contractFound := bridgeContractForAsset(state.Contracts, asset.Denom)
		routeID := normalizeBridgeRegistryID(contract.RouteID)
		if routeID == "" {
			routeID = normalizeBridgeRegistryID(asset.Denom)
		}
		ready, reason := bridgeRouteReadiness(state, chain, asset, contract, contractFound && chainFound, localDTL...)
		status := normalizeBridgeRouteStatus(contract.Status)
		if status == "" {
			status = BridgeRouteStatusSetupRequired
		}
		finality := normalizeBridgeFinalityStatus(contract.FinalityStatus)
		if finality == "" {
			finality = normalizeBridgeFinalityStatus(chain.FinalityStatus)
		}
		checkpoint, _ := bridgeLatestCheckpoint(state, chain.ChainID)
		if finality == "" {
			finality = BridgeFinalityUnknown
		}
		minDeposit := strings.TrimSpace(contract.MinDeposit)
		if minDeposit == "" {
			minDeposit = strings.TrimSpace(asset.MinDeposit)
		}
		dailyLimit := strings.TrimSpace(contract.DailyLimit)
		if dailyLimit == "" {
			dailyLimit = strings.TrimSpace(asset.DailyLimit)
		}
		routes = append(routes, BridgeRouteView{
			RouteID:               routeID,
			ChainID:               chain.ChainID,
			ChainName:             chain.Name,
			ChainType:             chain.ChainType,
			NativeSymbol:          chain.NativeSymbol,
			AssetDenom:            asset.Denom,
			AssetSymbol:           bridgeAssetSymbol(asset),
			OriginAsset:           asset.OriginAsset,
			LocalDenom:            asset.LocalDenom,
			Decimals:              asset.Decimals,
			ContractAddress:       contract.ContractAddress,
			DepositAddress:        contract.DepositAddress,
			DepositMode:           contract.DepositMode,
			ExecutionAdapter:      contract.ExecutionAdapter,
			RuntimeCodeHash:       contract.RuntimeCodeHash,
			Status:                status,
			FinalityStatus:        finality,
			MinConfirmations:      chain.MinConfirmations,
			MinDeposit:            minDeposit,
			DailyLimit:            dailyLimit,
			LatestObservedHeight:  chain.LatestObservedHeight,
			LatestFinalizedHeight: chain.LatestFinalizedHeight,
			LatestCheckpointID:    checkpoint.CheckpointID,
			CheckpointHeight:      checkpoint.Height,
			Ready:                 ready,
			UnavailableReason:     reason,
		})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].AssetSymbol == routes[j].AssetSymbol {
			return routes[i].ChainName < routes[j].ChainName
		}
		return routes[i].AssetSymbol < routes[j].AssetSymbol
	})
	return routes
}

func (s *Server) bridgeLedgerSnapshot() (Ledger, uint64, error) {
	if s == nil || s.Node == nil {
		return Ledger{}, 0, fmt.Errorf("bridge ledger unavailable")
	}
	height := uint64(0)
	if s.Node.Blockchain != nil {
		height = s.Node.Blockchain.Height()
	}
	s.Node.commitMu.Lock()
	if s.Node.committedHeight > height {
		height = s.Node.committedHeight
	}
	s.Node.commitMu.Unlock()
	ledger := s.Node.currentExecutionLedgerClone()
	if ledger.DTL == nil {
		return Ledger{}, height, fmt.Errorf("DTL state unavailable")
	}
	return ledger, height, nil
}

func (s *Server) bridgeRoutesForState(state BridgeGatewayState) []BridgeRouteView {
	ledger, _, err := s.bridgeLedgerSnapshot()
	if err != nil {
		return bridgeRoutesFromState(state, nil)
	}
	return bridgeRoutesFromState(state, ledger.DTL)
}

func findBridgeRoute(routes []BridgeRouteView, routeID string) (BridgeRouteView, bool) {
	routeID = normalizeBridgeRegistryID(routeID)
	for _, route := range routes {
		if normalizeBridgeRegistryID(route.RouteID) == routeID {
			return route, true
		}
	}
	return BridgeRouteView{}, false
}

func countBridgeTransferStates(state BridgeGatewayState) (pending, completed int) {
	for _, transfer := range state.Transfers {
		switch strings.ToLower(strings.TrimSpace(transfer.Status)) {
		case "completed", "failed", "cancelled", "refunded":
			if strings.EqualFold(transfer.Status, "completed") {
				completed++
			}
		default:
			pending++
		}
	}
	return pending, completed
}

func (s *Server) augmentBridgeStatus(status BridgeStatus) (BridgeStatus, error) {
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		return status, err
	}
	routes := s.bridgeRoutesForState(state)
	status.RegisteredChains = append([]BridgeChainConfig(nil), state.Chains...)
	status.RegisteredAssets = append([]BridgeAssetConfig(nil), state.Assets...)
	status.RegisteredContracts = append([]BridgeContractConfig(nil), state.Contracts...)
	status.BridgeValidators = append([]BridgeValidatorConfig(nil), state.Validators...)
	status.Paused = state.Paused
	status.PauseReason = state.PauseReason
	for _, route := range routes {
		if route.Ready {
			status.Operational = true
			break
		}
	}
	status.PendingTransfers, status.CompletedTransfers = countBridgeTransferStates(state)
	return status, nil
}

func bridgeRandomID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}

func authorizedBridgeAdmin(r *http.Request) bool {
	if r == nil || strings.TrimSpace(apiToken) == "" {
		return false
	}
	got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
	want := []byte("Bearer " + apiToken)
	return len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1
}

func decodeBridgeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	limit := int64(ConfigRPCMaxRequestBodyBytes)
	if limit <= 0 || limit > bridgeMaxBridgeProofBodySize {
		limit = bridgeMaxBridgeProofBodySize
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request must contain one JSON object")
	}
	return nil
}

func writeBridgeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func bridgeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeBridgeJSON(w, status, map[string]any{"error": code, "message": message})
}

func bridgeProofAnchoredToGatewayState(state BridgeGatewayState, proof BridgeProof) error {
	if proof.FinalityCheckpoint == nil || !validBridgeCheckpointID(proof.CheckpointID) {
		return fmt.Errorf("threshold finality checkpoint required")
	}
	stored, found := state.Checkpoints[strings.ToLower(strings.TrimSpace(proof.CheckpointID))]
	if !found {
		return fmt.Errorf("finality checkpoint is not registered")
	}
	chain, found := bridgeChainForID(state.Chains, proof.SourceChainID)
	if !found {
		return fmt.Errorf("finality checkpoint source chain is not registered")
	}
	stored, _, err := verifyBridgeFinalityCheckpoint(stored, chain, state.Validators)
	if err != nil {
		return fmt.Errorf("registered finality checkpoint invalid: %w", err)
	}
	supplied, _, err := verifyBridgeFinalityCheckpoint(*proof.FinalityCheckpoint, chain, state.Validators)
	if err != nil {
		return fmt.Errorf("supplied finality checkpoint invalid: %w", err)
	}
	if stored.CheckpointID != supplied.CheckpointID ||
		canonicalBridgeCheckpointPayload(stored, chain.ChainType) != canonicalBridgeCheckpointPayload(supplied, chain.ChainType) {
		return fmt.Errorf("supplied finality checkpoint does not match registry")
	}
	return nil
}

func (s *Server) verifyBridgeProofWithGatewayState(proof BridgeProof) BridgeVerificationResult {
	result := VerifyBridgeProof(proof)
	if !result.Accepted {
		return result
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		return bridgeFailure(proof, result.Mode, result.RequiredConfirmations, result.ObservedConfirmations, "finality_checkpoint_registry_unavailable")
	}
	chain, found := bridgeChainForID(state.Chains, proof.SourceChainID)
	if !found {
		return bridgeFailure(proof, result.Mode, result.RequiredConfirmations, result.ObservedConfirmations, "finality_checkpoint_source_chain_not_registered")
	}
	if !bridgeProofRequiresCheckpoint(result.Mode, chain) {
		return result
	}
	if err := bridgeProofAnchoredToGatewayState(state, proof); err != nil {
		return bridgeFailure(proof, result.Mode, result.RequiredConfirmations, result.ObservedConfirmations, "unregistered_finality_checkpoint: "+err.Error())
	}
	return result
}

func (s *Server) handleBridgeCheckpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		state, err := s.bridgeGatewaySnapshot()
		if err != nil {
			bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
			return
		}
		chainID := normalizeBridgeRegistryID(r.URL.Query().Get("chain_id"))
		checkpoints := make([]BridgeFinalityCheckpoint, 0)
		for _, checkpoint := range state.Checkpoints {
			if chainID == "" || strings.EqualFold(checkpoint.SourceChainID, chainID) {
				checkpoints = append(checkpoints, checkpoint)
			}
		}
		sort.Slice(checkpoints, func(i, j int) bool {
			if checkpoints[i].SourceChainID == checkpoints[j].SourceChainID {
				return checkpoints[i].Height > checkpoints[j].Height
			}
			return checkpoints[i].SourceChainID < checkpoints[j].SourceChainID
		})
		if len(checkpoints) > 100 {
			checkpoints = checkpoints[:100]
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		writeBridgeJSON(w, http.StatusOK, map[string]any{
			"version": BridgeCheckpointVersion, "checkpoints": checkpoints,
			"latest_by_chain": state.LatestCheckpointByChain,
		})
		return
	}
	if r.Method != http.MethodPost {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var checkpoint BridgeFinalityCheckpoint
	if err := decodeBridgeJSON(w, r, &checkpoint); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_checkpoint", err.Error())
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	chain, found := bridgeChainForID(state.Chains, checkpoint.SourceChainID)
	if !found {
		bridgeAPIError(w, http.StatusNotFound, "chain_not_found", "checkpoint source chain is not registered")
		return
	}
	checkpoint, signerCount, err := verifyBridgeFinalityCheckpoint(checkpoint, chain, state.Validators)
	if err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_checkpoint", err.Error())
		return
	}
	if !bridgeCheckpointIsFresh(checkpoint, time.Now().UTC()) {
		bridgeAPIError(w, http.StatusBadRequest, "stale_checkpoint", "checkpoint issue time is outside the accepted freshness window")
		return
	}
	checkpoint.ValidatorSignatures = bridgeVerifiedCheckpointSignatures(checkpoint, chain.ChainType, state.Validators)
	checkpoint.CreatedAtUnix = time.Now().UTC().Unix()

	s.bridgeMu.Lock()
	chain, found = bridgeChainForID(s.bridgeState.Chains, checkpoint.SourceChainID)
	if !found {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "chain_changed", "checkpoint source chain changed during validation")
		return
	}
	checkpoint, signerCount, err = verifyBridgeFinalityCheckpoint(checkpoint, chain, s.bridgeState.Validators)
	if err != nil {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "checkpoint_validator_set_changed", err.Error())
		return
	}
	checkpoint.ValidatorSignatures = bridgeVerifiedCheckpointSignatures(checkpoint, chain.ChainType, s.bridgeState.Validators)
	checkpoint.CreatedAtUnix = time.Now().UTC().Unix()
	if existing, exists := s.bridgeState.Checkpoints[checkpoint.CheckpointID]; exists {
		s.bridgeMu.Unlock()
		writeBridgeJSON(w, http.StatusOK, map[string]any{"checkpoint": existing, "signer_count": signerCount, "idempotent": true})
		return
	}
	for _, existing := range s.bridgeState.Checkpoints {
		if strings.EqualFold(existing.SourceChainID, checkpoint.SourceChainID) && existing.Height == checkpoint.Height && existing.CheckpointID != checkpoint.CheckpointID {
			s.bridgeState.Paused = true
			s.bridgeState.PauseReason = "Conflicting threshold finality checkpoints detected for " + checkpoint.SourceChainID
			eventID, _ := bridgeRandomID("bge_")
			s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
				EventID: eventID, EventType: "checkpoint_conflict", Status: "emergency_paused",
				Detail:        fmt.Sprintf("chain=%s height=%d existing=%s conflicting=%s", checkpoint.SourceChainID, checkpoint.Height, existing.CheckpointID, checkpoint.CheckpointID),
				CreatedAtUnix: time.Now().UTC().Unix(),
			})
			err = s.persistBridgeGatewayStateLocked()
			s.bridgeMu.Unlock()
			if err != nil {
				bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
				return
			}
			bridgeAPIError(w, http.StatusConflict, "checkpoint_conflict", "conflicting checkpoint detected; bridge emergency pause activated")
			return
		}
	}
	chainKey := normalizeBridgeRegistryID(checkpoint.SourceChainID)
	latestID := strings.ToLower(strings.TrimSpace(s.bridgeState.LatestCheckpointByChain[chainKey]))
	if latestID == "" {
		if checkpoint.PreviousCheckpointID != "" {
			s.bridgeMu.Unlock()
			bridgeAPIError(w, http.StatusConflict, "checkpoint_parent_mismatch", "first checkpoint must not reference a previous checkpoint")
			return
		}
	} else {
		latest := s.bridgeState.Checkpoints[latestID]
		if checkpoint.PreviousCheckpointID != latestID || checkpoint.Height <= latest.Height {
			s.bridgeMu.Unlock()
			bridgeAPIError(w, http.StatusConflict, "checkpoint_sequence_invalid", "checkpoint must extend the latest registered checkpoint at a greater height")
			return
		}
	}
	s.bridgeState.Checkpoints[checkpoint.CheckpointID] = checkpoint
	s.bridgeState.LatestCheckpointByChain[chainKey] = checkpoint.CheckpointID
	for index := range s.bridgeState.Chains {
		if strings.EqualFold(s.bridgeState.Chains[index].ChainID, checkpoint.SourceChainID) {
			if checkpoint.ObservedHeight > s.bridgeState.Chains[index].LatestObservedHeight {
				s.bridgeState.Chains[index].LatestObservedHeight = checkpoint.ObservedHeight
			}
			if checkpoint.Height > s.bridgeState.Chains[index].LatestFinalizedHeight {
				s.bridgeState.Chains[index].LatestFinalizedHeight = checkpoint.Height
			}
			s.bridgeState.Chains[index].FinalityStatus = BridgeFinalityFinalized
			break
		}
	}
	replaceBridgeRuntimeRegistry(s.bridgeState)
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "finality_checkpoint_registered", Status: "verified",
		Detail:        fmt.Sprintf("chain=%s height=%d checkpoint=%s signers=%d", checkpoint.SourceChainID, checkpoint.Height, checkpoint.CheckpointID, signerCount),
		CreatedAtUnix: checkpoint.CreatedAtUnix,
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusCreated, map[string]any{"checkpoint": checkpoint, "signer_count": signerCount, "state_root": stateRoot})
}

func (s *Server) handleBridgeRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	routes := s.bridgeRoutesForState(state)
	ready := 0
	for _, route := range routes {
		if route.Ready {
			ready++
		}
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeBridgeJSON(w, http.StatusOK, map[string]any{
		"version":      BridgeGatewayVersion,
		"paused":       state.Paused,
		"pause_reason": state.PauseReason,
		"ready_routes": ready,
		"routes":       routes,
	})
}

func (s *Server) bridgeAccountingSnapshot() (BridgeAccountingSnapshot, error) {
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		return BridgeAccountingSnapshot{}, err
	}
	ledger, height, err := s.bridgeLedgerSnapshot()
	if err != nil {
		return BridgeAccountingSnapshot{}, err
	}
	routes := bridgeRoutesFromState(state, ledger.DTL)
	assetsByToken := make(map[string]*BridgeAccountingAsset)
	for _, route := range routes {
		asset, found := bridgeAssetForDenom(state.Assets, route.AssetDenom)
		if !found {
			continue
		}
		tokenID, token, found := bridgeLocalToken(ledger.DTL, asset)
		if !found {
			continue
		}
		accountingAsset := assetsByToken[tokenID]
		if accountingAsset == nil {
			accountingAsset = &BridgeAccountingAsset{
				LocalTokenID:       tokenID,
				LocalDenom:         strings.TrimSpace(asset.LocalDenom),
				Symbol:             strings.TrimSpace(token.Symbol),
				Decimals:           token.Decimals,
				TotalSupplyRaw:     strconv.FormatUint(token.TotalSupply, 10),
				MaxSupplyRaw:       strconv.FormatUint(token.MaxSupply, 10),
				Paused:             token.Paused,
				AuthorityThreshold: token.AuthorityThreshold,
			}
			assetsByToken[tokenID] = accountingAsset
		}
		accountingAsset.Routes = append(accountingAsset.Routes, BridgeAccountingRoute{
			RouteID:           route.RouteID,
			ChainID:           route.ChainID,
			ChainType:         route.ChainType,
			AssetDenom:        route.AssetDenom,
			OriginAsset:       route.OriginAsset,
			VaultAddress:      route.ContractAddress,
			ExecutionAdapter:  route.ExecutionAdapter,
			RuntimeCodeHash:   route.RuntimeCodeHash,
			SourceDecimals:    route.Decimals,
			Status:            route.Status,
			Ready:             route.Ready,
			UnavailableReason: route.UnavailableReason,
		})
	}
	assets := make([]BridgeAccountingAsset, 0, len(assetsByToken))
	for _, asset := range assetsByToken {
		sort.Slice(asset.Routes, func(i, j int) bool { return asset.Routes[i].RouteID < asset.Routes[j].RouteID })
		assets = append(assets, *asset)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].LocalTokenID < assets[j].LocalTokenID })
	return BridgeAccountingSnapshot{
		Version:         BridgeAccountingVersion,
		BridgeVersion:   BridgeGatewayVersion,
		StateRoot:       state.StateRoot,
		MSCHeight:       height,
		GeneratedAtUnix: time.Now().UTC().Unix(),
		BridgePaused:    state.Paused,
		Assets:          assets,
	}, nil
}

func (s *Server) handleBridgeAccounting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	snapshot, err := s.bridgeAccountingSnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_accounting_unavailable", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, snapshot)
}

func bridgeRouteVolume(state BridgeGatewayState, route BridgeRouteView, now time.Time) *big.Int {
	total := new(big.Int)
	cutoff := now.Add(-24 * time.Hour).Unix()
	for _, transfer := range state.Transfers {
		if normalizeBridgeRegistryID(transfer.RouteID) != normalizeBridgeRegistryID(route.RouteID) || transfer.CreatedAtUnix < cutoff {
			continue
		}
		if strings.EqualFold(transfer.Direction, "withdrawal") {
			status := strings.ToLower(strings.TrimSpace(transfer.Status))
			if status == "waiting_wallet_burn_signature" || status == "burn_confirmed_rate_limit_hold" || status == "cancelled" || status == "failed" || status == "expired" {
				continue
			}
		}
		amount, err := bridgeDecimalUnits(transfer.Amount, route.Decimals)
		if err == nil {
			total.Add(total, amount)
		}
	}
	return total
}

func pruneBridgeGatewayStateLocked(state *BridgeGatewayState, now time.Time) {
	if state == nil {
		return
	}
	if len(state.DepositIntents) > bridgeMaxDepositIntents {
		type item struct {
			key     string
			created int64
		}
		items := make([]item, 0, len(state.DepositIntents))
		for key, intent := range state.DepositIntents {
			items = append(items, item{key: key, created: intent.CreatedAtUnix})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].created < items[j].created })
		for _, entry := range items[:len(items)-bridgeMaxDepositIntents] {
			delete(state.DepositIntents, entry.key)
		}
	}
	if len(state.Transfers) > bridgeMaxTransfers {
		type item struct {
			key     string
			created int64
		}
		items := make([]item, 0, len(state.Transfers))
		for key, transfer := range state.Transfers {
			items = append(items, item{key: key, created: transfer.CreatedAtUnix})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].created < items[j].created })
		for _, entry := range items[:len(items)-bridgeMaxTransfers] {
			delete(state.Transfers, entry.key)
		}
	}
	for key, intent := range state.DepositIntents {
		if intent.Status == "awaiting_deposit" && intent.ExpiresAtUnix > 0 && intent.ExpiresAtUnix < now.Unix() {
			intent.Status = "expired"
			state.DepositIntents[key] = intent
		}
	}
}

func (s *Server) handleBridgeDeposits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request bridgeDepositRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid deposit request")
		return
	}
	if _, err := decodeAddressPayload(request.Recipient); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_recipient", "recipient must be a valid MSC address")
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	route, found := findBridgeRoute(s.bridgeRoutesForState(state), request.RouteID)
	if !found {
		bridgeAPIError(w, http.StatusNotFound, "route_not_found", "bridge route is not registered")
		return
	}
	if !route.Ready {
		bridgeAPIError(w, http.StatusConflict, "route_not_ready", route.UnavailableReason)
		return
	}
	amount, err := bridgeDecimalUnits(request.Amount, route.Decimals)
	if err != nil || amount.Sign() <= 0 {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_amount", "amount must be a positive decimal supported by the route")
		return
	}
	if route.MinDeposit != "" {
		minimum, minErr := bridgeDecimalUnits(route.MinDeposit, route.Decimals)
		if minErr != nil || amount.Cmp(minimum) < 0 {
			bridgeAPIError(w, http.StatusBadRequest, "below_minimum", "amount is below the route minimum deposit")
			return
		}
	}
	if route.DailyLimit != "" {
		limit, limitErr := bridgeDecimalUnits(route.DailyLimit, route.Decimals)
		if limitErr != nil || new(big.Int).Add(bridgeRouteVolume(state, route, time.Now().UTC()), amount).Cmp(limit) > 0 {
			bridgeAPIError(w, http.StatusTooManyRequests, "daily_limit_exceeded", "route daily limit would be exceeded")
			return
		}
	}
	intentID, err := bridgeRandomID("bdi_")
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "id_generation_failed", "could not create deposit intent")
		return
	}
	now := time.Now().UTC()
	intent := BridgeDepositIntent{
		IntentID:        intentID,
		RouteID:         route.RouteID,
		SourceChainID:   route.ChainID,
		AssetDenom:      route.AssetDenom,
		AssetSymbol:     route.AssetSymbol,
		Recipient:       displayAddress(request.Recipient),
		RequestedAmount: strings.TrimSpace(request.Amount),
		DepositAddress:  route.DepositAddress,
		TokenContract:   route.OriginAsset,
		DepositMode:     route.DepositMode,
		Status:          "awaiting_deposit",
		CreatedAtUnix:   now.Unix(),
		ExpiresAtUnix:   now.Add(bridgeDepositIntentTTL).Unix(),
	}
	if route.DepositMode == "memo" {
		intent.Memo = intentID
	}
	s.bridgeMu.Lock()
	if s.bridgeState.StateRoot != state.StateRoot {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "bridge_state_changed", "bridge state changed; refresh the route and authorize again")
		return
	}
	if s.bridgeState.Paused {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "bridge_paused", s.bridgeState.PauseReason)
		return
	}
	pruneBridgeGatewayStateLocked(&s.bridgeState, now)
	s.bridgeState.DepositIntents[intentID] = intent
	err = s.persistBridgeGatewayStateLocked()
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", "deposit intent could not be persisted")
		return
	}
	writeBridgeJSON(w, http.StatusCreated, intent)
}

func (s *Server) handleBridgeWithdrawals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request bridgeWithdrawalRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid withdrawal request")
		return
	}
	now := time.Now().UTC()
	authorizationHash, err := verifyBridgeWithdrawalAuthorization(request, now)
	if err != nil {
		bridgeAPIError(w, http.StatusUnauthorized, "invalid_withdrawal_authorization", err.Error())
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	ledger, _, err := s.bridgeLedgerSnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "local_token_state_unavailable", err.Error())
		return
	}
	route, found := findBridgeRoute(bridgeRoutesFromState(state, ledger.DTL), request.RouteID)
	if !found {
		bridgeAPIError(w, http.StatusNotFound, "route_not_found", "bridge route is not registered")
		return
	}
	if !route.Ready {
		bridgeAPIError(w, http.StatusConflict, "route_not_ready", route.UnavailableReason)
		return
	}
	chain, found := bridgeChainForID(state.Chains, route.ChainID)
	if !found || !validExternalBridgeAddress(chain.ChainType, request.ExternalRecipient) {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_external_recipient", "destination address is invalid for the selected network")
		return
	}
	amount, err := bridgeDecimalUnits(request.Amount, route.Decimals)
	if err != nil || amount.Sign() <= 0 {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_amount", "amount must be a positive decimal supported by the route")
		return
	}
	if route.MinDeposit != "" {
		minimum, minErr := bridgeDecimalUnits(route.MinDeposit, route.Decimals)
		if minErr != nil || amount.Cmp(minimum) < 0 {
			bridgeAPIError(w, http.StatusBadRequest, "below_minimum", "amount is below the route minimum withdrawal")
			return
		}
	}
	if route.DailyLimit != "" {
		limit, limitErr := bridgeDecimalUnits(route.DailyLimit, route.Decimals)
		if limitErr != nil || new(big.Int).Add(bridgeRouteVolume(state, route, now), amount).Cmp(limit) > 0 {
			bridgeAPIError(w, http.StatusTooManyRequests, "daily_limit_exceeded", "route daily limit would be exceeded")
			return
		}
	}
	transferID, err := bridgeRandomID("btx_")
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "id_generation_failed", "could not create withdrawal")
		return
	}
	nonce := s.Node.Mempool.NextNonce(request.Sender, ledger)
	instruction, bridgeID, err := buildBridgeBurnInstruction(state, ledger, route, request, transferID, authorizationHash, nonce)
	if err != nil {
		bridgeAPIError(w, http.StatusConflict, "burn_instruction_unavailable", err.Error())
		return
	}
	instruction.TransactionTemplate.Fee = requiredDTLFeeForTx(instruction.TransactionTemplate)
	transfer := BridgeTransfer{
		TransferID: transferID, RouteID: route.RouteID, Direction: "withdrawal",
		SourceChainID: protocolChainID(), DestinationChainID: route.ChainID,
		AssetDenom: route.AssetDenom, Recipient: displayAddress(request.Sender), Sender: displayAddress(request.Sender),
		ExternalRecipient: strings.TrimSpace(request.ExternalRecipient), Amount: strings.TrimSpace(request.Amount),
		BridgeID: bridgeID, ReplayKey: authorizationHash, Status: "waiting_wallet_burn_signature",
		BurnInstruction: &instruction, CreatedAtUnix: now.Unix(), UpdatedAtUnix: now.Unix(),
	}
	s.bridgeMu.Lock()
	if s.bridgeState.Paused {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "bridge_paused", s.bridgeState.PauseReason)
		return
	}
	if existing := s.bridgeState.ProcessedEvents[authorizationHash]; existing != "" {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "authorization_replayed", "withdrawal authorization is already linked to transfer "+existing)
		return
	}
	currentRoute, currentFound := findBridgeRoute(bridgeRoutesFromState(s.bridgeState, ledger.DTL), route.RouteID)
	if !currentFound || !currentRoute.Ready {
		s.bridgeMu.Unlock()
		reason := "route_not_registered"
		if currentFound {
			reason = currentRoute.UnavailableReason
		}
		bridgeAPIError(w, http.StatusConflict, "route_not_ready", reason)
		return
	}
	pruneBridgeGatewayStateLocked(&s.bridgeState, now)
	s.bridgeState.Transfers[transferID] = transfer
	s.bridgeState.ProcessedEvents[authorizationHash] = transferID
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "withdrawal_burn_prepared", RouteID: route.RouteID,
		TransferID: transferID, ReplayKey: authorizationHash, Status: transfer.Status,
		Detail: "wallet authorization verified; consensus burn still requires wallet signature", CreatedAtUnix: now.Unix(),
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", "withdrawal could not be persisted")
		return
	}
	writeBridgeJSON(w, http.StatusCreated, map[string]any{
		"transfer": transfer, "transaction_template": instruction.TransactionTemplate, "state_root": stateRoot,
		"next_step": "sign this exact burn transaction with the sender wallet and submit it to /submitTx",
	})
}

func (s *Server) handleBridgeTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	recipient := strings.TrimSpace(r.URL.Query().Get("recipient"))
	if _, err := decodeAddressPayload(recipient); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_recipient", "valid MSC recipient query parameter required")
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	intents := make([]BridgeDepositIntent, 0)
	for _, intent := range state.DepositIntents {
		if addressesEqual(intent.Recipient, recipient) {
			intents = append(intents, intent)
		}
	}
	transfers := make([]BridgeTransfer, 0)
	for _, transfer := range state.Transfers {
		if addressesEqual(transfer.Recipient, recipient) {
			transfers = append(transfers, transfer)
		}
	}
	sort.Slice(intents, func(i, j int) bool { return intents[i].CreatedAtUnix > intents[j].CreatedAtUnix })
	sort.Slice(transfers, func(i, j int) bool { return transfers[i].CreatedAtUnix > transfers[j].CreatedAtUnix })
	if len(intents) > bridgeMaxPublicHistoryItems {
		intents = intents[:bridgeMaxPublicHistoryItems]
	}
	if len(transfers) > bridgeMaxPublicHistoryItems {
		transfers = transfers[:bridgeMaxPublicHistoryItems]
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeBridgeJSON(w, http.StatusOK, map[string]any{"deposit_intents": intents, "transfers": transfers})
}

func validateBridgeChainConfig(chain *BridgeChainConfig) error {
	if chain == nil {
		return fmt.Errorf("chain required")
	}
	chain.ChainID = normalizeBridgeRegistryID(chain.ChainID)
	chain.Name = strings.TrimSpace(chain.Name)
	chain.ChainType = normalizeBridgeChainType(chain.ChainType)
	chain.TrustModel = normalizeBridgeTrustModel(chain.TrustModel)
	chain.Status = normalizeBridgeRouteStatus(chain.Status)
	chain.FinalityStatus = normalizeBridgeFinalityStatus(chain.FinalityStatus)
	chain.NativeSymbol = strings.ToUpper(strings.TrimSpace(chain.NativeSymbol))
	chain.ExplorerURL = strings.TrimSpace(chain.ExplorerURL)
	chain.LightClient = strings.TrimSpace(chain.LightClient)
	if chain.ChainID == "" || chain.Name == "" || len(chain.Name) > 80 || chain.ChainType == "" {
		return fmt.Errorf("valid chain_id, name, and chain_type are required")
	}
	if chain.Status == "" || chain.FinalityStatus == "" || chain.MinConfirmations == 0 {
		return fmt.Errorf("status, finality_status, and min_confirmations are required")
	}
	if len(chain.NativeSymbol) > 16 || !validBridgePublicURL(chain.ExplorerURL) || len(chain.LightClient) > bridgeMaxAdminStringLen {
		return fmt.Errorf("invalid chain metadata")
	}
	if chain.LatestFinalizedHeight > chain.LatestObservedHeight && chain.LatestObservedHeight > 0 {
		return fmt.Errorf("finalized height cannot exceed observed height")
	}
	if chain.Status == BridgeRouteStatusActive && chain.ChainType == "tron" && !isCanonicalTronMainnetChain(*chain) {
		return fmt.Errorf("active TRON chain must use the pinned mainnet identity and verifier with at least 64 confirmations")
	}
	return nil
}

func isCanonicalTronMainnetChain(chain BridgeChainConfig) bool {
	return normalizeBridgeChainType(chain.ChainType) == "tron" &&
		normalizeBridgeRegistryID(chain.ChainID) == bridgeTronMainnetChainID &&
		strings.TrimSpace(chain.Name) == bridgeTronMainnetName &&
		strings.ToUpper(strings.TrimSpace(chain.NativeSymbol)) == bridgeTronMainnetNativeSymbol &&
		normalizeBridgeTrustModel(chain.TrustModel) == BridgeTrustHybrid &&
		strings.TrimSpace(chain.LightClient) == bridgeTronMainnetVerifier &&
		chain.MinConfirmations >= bridgeTronMainnetMinConfirmations &&
		strings.TrimRight(strings.TrimSpace(chain.ExplorerURL), "/") == bridgeTronMainnetExplorerURL
}

func isCanonicalTronMainnetAsset(chain BridgeChainConfig, asset BridgeAssetConfig) bool {
	return isCanonicalTronMainnetChain(chain) &&
		strings.EqualFold(strings.TrimSpace(asset.Denom), bridgeTronMainnetAssetDenom) &&
		strings.TrimSpace(asset.OriginChain) == bridgeTronMainnetChainID &&
		strings.TrimSpace(asset.OriginAsset) == bridgeTronMainnetUSDTAddress &&
		strings.EqualFold(strings.TrimSpace(asset.Symbol), "USDT") &&
		strings.TrimSpace(asset.LocalDenom) == bridgeTronMainnetLocalDenom && asset.Decimals == 6
}

func validateBridgeAssetConfig(asset *BridgeAssetConfig, chains []BridgeChainConfig) error {
	if asset == nil {
		return fmt.Errorf("asset required")
	}
	asset.Denom = strings.TrimSpace(asset.Denom)
	asset.OriginChain = normalizeBridgeRegistryID(asset.OriginChain)
	asset.OriginAsset = strings.TrimSpace(asset.OriginAsset)
	asset.LocalDenom = strings.TrimSpace(asset.LocalDenom)
	asset.EscrowPolicy = strings.ToLower(strings.TrimSpace(asset.EscrowPolicy))
	asset.Symbol = strings.ToUpper(strings.TrimSpace(asset.Symbol))
	asset.Status = normalizeBridgeRouteStatus(asset.Status)
	asset.MinDeposit = strings.TrimSpace(asset.MinDeposit)
	asset.DailyLimit = strings.TrimSpace(asset.DailyLimit)
	if normalizeBridgeRegistryID(asset.Denom) == "" || asset.OriginChain == "" || asset.OriginAsset == "" || asset.LocalDenom == "" {
		return fmt.Errorf("denom, origin_chain, origin_asset, and local_denom are required")
	}
	if asset.Decimals > 30 || asset.Status == "" || asset.Symbol == "" || len(asset.Symbol) > 16 {
		return fmt.Errorf("valid symbol, decimals, and status are required")
	}
	var chain BridgeChainConfig
	found := false
	for _, candidate := range chains {
		if strings.EqualFold(candidate.ChainID, asset.OriginChain) {
			chain = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("origin chain is not registered")
	}
	if asset.Status == BridgeRouteStatusActive && !validExternalBridgeAddress(chain.ChainType, asset.OriginAsset) {
		return fmt.Errorf("active asset origin contract/address is invalid for chain type")
	}
	if asset.Status == BridgeRouteStatusActive && normalizeBridgeChainType(chain.ChainType) == "tron" &&
		!isCanonicalTronMainnetAsset(chain, *asset) {
		return fmt.Errorf("active TRON asset must be official mainnet Tether USDT")
	}
	if asset.MinDeposit != "" {
		amount, err := bridgeDecimalUnits(asset.MinDeposit, asset.Decimals)
		if err != nil || amount.Sign() <= 0 {
			return fmt.Errorf("invalid min_deposit")
		}
	}
	if asset.DailyLimit != "" {
		amount, err := bridgeDecimalUnits(asset.DailyLimit, asset.Decimals)
		if err != nil || amount.Sign() <= 0 {
			return fmt.Errorf("invalid daily_limit")
		}
	}
	return nil
}

func validateBridgeContractConfig(contract *BridgeContractConfig, state BridgeGatewayState) error {
	if contract == nil {
		return fmt.Errorf("contract required")
	}
	contract.RouteID = normalizeBridgeRegistryID(contract.RouteID)
	contract.ChainID = normalizeBridgeRegistryID(contract.ChainID)
	contract.AssetDenom = strings.TrimSpace(contract.AssetDenom)
	contract.ContractAddress = strings.TrimSpace(contract.ContractAddress)
	contract.DepositAddress = strings.TrimSpace(contract.DepositAddress)
	contract.DepositMode = normalizeBridgeDepositMode(contract.DepositMode)
	contract.ExecutionAdapter = normalizeBridgeExecutionAdapter(contract.ExecutionAdapter)
	contract.RuntimeCodeHash = strings.ToLower(strings.TrimSpace(contract.RuntimeCodeHash))
	contract.Status = normalizeBridgeRouteStatus(contract.Status)
	contract.FinalityStatus = normalizeBridgeFinalityStatus(contract.FinalityStatus)
	contract.MinDeposit = strings.TrimSpace(contract.MinDeposit)
	contract.DailyLimit = strings.TrimSpace(contract.DailyLimit)
	contract.AuditReference = strings.TrimSpace(contract.AuditReference)
	contract.DeploymentTxHash = strings.TrimSpace(contract.DeploymentTxHash)
	contract.ContractID = contract.RouteID
	contract.Address = contract.ContractAddress
	if contract.RouteID == "" || contract.ChainID == "" || normalizeBridgeRegistryID(contract.AssetDenom) == "" {
		return fmt.Errorf("route_id, chain_id, and asset_denom are required")
	}
	if contract.Status == "" || contract.FinalityStatus == "" || contract.DepositMode == "" {
		return fmt.Errorf("valid status, finality_status, and deposit_mode are required")
	}
	var chain BridgeChainConfig
	chainFound := false
	for _, candidate := range state.Chains {
		if strings.EqualFold(candidate.ChainID, contract.ChainID) {
			chain = candidate
			chainFound = true
			break
		}
	}
	var asset BridgeAssetConfig
	assetFound := false
	for _, candidate := range state.Assets {
		if strings.EqualFold(candidate.Denom, contract.AssetDenom) {
			asset = candidate
			assetFound = true
			break
		}
	}
	if !chainFound || !assetFound || !strings.EqualFold(asset.OriginChain, chain.ChainID) {
		return fmt.Errorf("contract chain and asset mapping is not registered")
	}
	if contract.Status == BridgeRouteStatusActive {
		if normalizeBridgeChainType(chain.ChainType) == "tron" && !isCanonicalTronMainnetAsset(chain, asset) {
			return fmt.Errorf("active TRON contract route must target official mainnet Tether USDT")
		}
		if !validExternalBridgeAddress(chain.ChainType, contract.ContractAddress) || !validExternalBridgeAddress(chain.ChainType, contract.DepositAddress) {
			return fmt.Errorf("active contract and deposit addresses must be valid for chain type")
		}
		if !bridgeExecutionAdapterCompatible(chain.ChainType, contract.ExecutionAdapter) {
			return fmt.Errorf("active contract execution adapter is missing or incompatible with chain type")
		}
		if !bridgeExecutionAdapterImplemented(chain.ChainType, contract.ExecutionAdapter) {
			return fmt.Errorf("active contract execution adapter is not implemented by this gateway release")
		}
		if !validBridgeAuditReference(contract.AuditReference) {
			return fmt.Errorf("active contract requires an HTTPS audit_reference")
		}
		if !validBridgeDeploymentTxHash(chain.ChainType, contract.DeploymentTxHash) {
			return fmt.Errorf("active contract requires a valid chain deployment_tx_hash")
		}
		if !validBridgeRuntimeCodeHash(contract.RuntimeCodeHash) {
			return fmt.Errorf("active contract requires a non-zero 32-byte runtime_code_hash")
		}
		if (contract.ExecutionAdapter == BridgeExecutionEVMVaultV1 ||
			contract.ExecutionAdapter == BridgeExecutionTronVaultV1) &&
			contract.DepositMode == "contract_call" &&
			!bridgeExternalAddressEqual(chain.ChainType, contract.ContractAddress, contract.DepositAddress) {
			return fmt.Errorf("vault contract and deposit address must match")
		}
	}
	if len(contract.AuditReference) > bridgeMaxAdminStringLen || len(contract.DeploymentTxHash) > 192 {
		return fmt.Errorf("contract metadata is too long")
	}
	if contract.MinDeposit != "" {
		amount, err := bridgeDecimalUnits(contract.MinDeposit, asset.Decimals)
		if err != nil || amount.Sign() <= 0 {
			return fmt.Errorf("invalid min_deposit")
		}
	}
	if contract.DailyLimit != "" {
		amount, err := bridgeDecimalUnits(contract.DailyLimit, asset.Decimals)
		if err != nil || amount.Sign() <= 0 {
			return fmt.Errorf("invalid daily_limit")
		}
	}
	return nil
}

func validateBridgeValidatorConfig(validator *BridgeValidatorConfig) error {
	if validator == nil {
		return fmt.Errorf("validator required")
	}
	validator.ValidatorID = normalizeBridgeRegistryID(validator.ValidatorID)
	validator.PublicKey = strings.ToLower(strings.TrimSpace(validator.PublicKey))
	validator.Status = normalizeBridgeRouteStatus(validator.Status)
	validator.Endpoint = strings.TrimSpace(validator.Endpoint)
	if validator.ValidatorID == "" || validator.Status == "" {
		return fmt.Errorf("validator_id and status are required")
	}
	pub, err := hex.DecodeString(validator.PublicKey)
	if err != nil || len(pub) != 32 {
		return fmt.Errorf("public_key must be a 32-byte Ed25519 hex key")
	}
	if validator.Endpoint != "" && !validBridgePublicURL(validator.Endpoint) {
		return fmt.Errorf("validator endpoint must be an HTTP(S) URL")
	}
	if validator.Weight == 0 {
		validator.Weight = 1
	}
	return nil
}

func upsertBridgeChain(items []BridgeChainConfig, value BridgeChainConfig) []BridgeChainConfig {
	for index := range items {
		if strings.EqualFold(items[index].ChainID, value.ChainID) {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func upsertBridgeAsset(items []BridgeAssetConfig, value BridgeAssetConfig) []BridgeAssetConfig {
	for index := range items {
		if strings.EqualFold(items[index].Denom, value.Denom) {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func upsertBridgeContract(items []BridgeContractConfig, value BridgeContractConfig) []BridgeContractConfig {
	for index := range items {
		if strings.EqualFold(items[index].RouteID, value.RouteID) || strings.EqualFold(items[index].AssetDenom, value.AssetDenom) {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func upsertBridgeValidator(items []BridgeValidatorConfig, value BridgeValidatorConfig) []BridgeValidatorConfig {
	for index := range items {
		if strings.EqualFold(items[index].ValidatorID, value.ValidatorID) || strings.EqualFold(items[index].PublicKey, value.PublicKey) {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func (s *Server) requireBridgeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !authorizedBridgeAdmin(r) {
		bridgeAPIError(w, http.StatusUnauthorized, "admin_auth_required", "MSC_RPC_TOKEN bearer authentication required")
		return false
	}
	if err := s.ensureBridgeGatewayState(); err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return false
	}
	return true
}

func (s *Server) handleBridgeAdminState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !s.requireBridgeAdmin(w, r) {
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, map[string]any{"state": state, "routes": s.bridgeRoutesForState(state)})
}

func (s *Server) handleBridgeAdminChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var chain BridgeChainConfig
	if err := decodeBridgeJSON(w, r, &chain); err != nil || validateBridgeChainConfig(&chain) != nil {
		if err == nil {
			err = validateBridgeChainConfig(&chain)
		}
		bridgeAPIError(w, http.StatusBadRequest, "invalid_chain", err.Error())
		return
	}
	s.bridgeMu.Lock()
	s.bridgeState.Chains = upsertBridgeChain(s.bridgeState.Chains, chain)
	replaceBridgeRuntimeRegistry(s.bridgeState)
	err := s.persistBridgeGatewayStateLocked()
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, chain)
}

func (s *Server) handleBridgeAdminAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var asset BridgeAssetConfig
	if err := decodeBridgeJSON(w, r, &asset); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_asset", err.Error())
		return
	}
	s.bridgeMu.Lock()
	if err := validateBridgeAssetConfig(&asset, s.bridgeState.Chains); err != nil {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusBadRequest, "invalid_asset", err.Error())
		return
	}
	s.bridgeState.Assets = upsertBridgeAsset(s.bridgeState.Assets, asset)
	replaceBridgeRuntimeRegistry(s.bridgeState)
	err := s.persistBridgeGatewayStateLocked()
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleBridgeAdminContract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var contract BridgeContractConfig
	if err := decodeBridgeJSON(w, r, &contract); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_contract", err.Error())
		return
	}
	s.bridgeMu.Lock()
	if err := validateBridgeContractConfig(&contract, s.bridgeState); err != nil {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusBadRequest, "invalid_contract", err.Error())
		return
	}
	contract.UpdatedAtUnix = time.Now().UTC().Unix()
	s.bridgeState.Contracts = upsertBridgeContract(s.bridgeState.Contracts, contract)
	err := s.persistBridgeGatewayStateLocked()
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, contract)
}

func (s *Server) handleBridgeAdminValidator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var validator BridgeValidatorConfig
	if err := decodeBridgeJSON(w, r, &validator); err != nil || validateBridgeValidatorConfig(&validator) != nil {
		if err == nil {
			err = validateBridgeValidatorConfig(&validator)
		}
		bridgeAPIError(w, http.StatusBadRequest, "invalid_validator", err.Error())
		return
	}
	validator.UpdatedAt = time.Now().UTC().Unix()
	s.bridgeMu.Lock()
	s.bridgeState.Validators = upsertBridgeValidator(s.bridgeState.Validators, validator)
	replaceBridgeRuntimeRegistry(s.bridgeState)
	err := s.persistBridgeGatewayStateLocked()
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, validator)
}

func (s *Server) handleBridgeAdminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var request bridgeSettingsRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	request.PauseReason = strings.TrimSpace(request.PauseReason)
	if request.Paused && request.PauseReason == "" {
		bridgeAPIError(w, http.StatusBadRequest, "pause_reason_required", "pause_reason is required while paused")
		return
	}
	if len(request.PauseReason) > bridgeMaxAdminStringLen {
		bridgeAPIError(w, http.StatusBadRequest, "pause_reason_too_long", "pause_reason is too long")
		return
	}
	s.bridgeMu.Lock()
	s.bridgeState.Paused = request.Paused
	s.bridgeState.PauseReason = request.PauseReason
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "pause_changed", Status: fmt.Sprintf("paused=%t", request.Paused),
		Detail: request.PauseReason, CreatedAtUnix: time.Now().UTC().Unix(),
	})
	err := s.persistBridgeGatewayStateLocked()
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, request)
}

func (s *Server) handleBridgeAdminEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var submission bridgeEventSubmission
	if err := decodeBridgeJSON(w, r, &submission); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_event", err.Error())
		return
	}
	result := s.verifyBridgeProofWithGatewayState(submission.Proof)
	if !result.Accepted {
		writeBridgeJSON(w, http.StatusBadRequest, result)
		return
	}
	if bridgeProofEventType(submission.Proof) != "lock" {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_deposit_event_type", "deposit processing requires a lock event proof")
		return
	}
	ledger, currentHeight, err := s.bridgeLedgerSnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "local_token_state_unavailable", err.Error())
		return
	}
	replayKey := strings.ToLower(strings.TrimSpace(result.ReplayProtectionKey))
	s.bridgeMu.Lock()
	if s.bridgeState.Paused {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "bridge_paused", s.bridgeState.PauseReason)
		return
	}
	intent, found := s.bridgeState.DepositIntents[strings.TrimSpace(submission.IntentID)]
	if !found {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusNotFound, "intent_not_found", "deposit intent is required for this event")
		return
	}
	if existing := s.bridgeState.ProcessedEvents[replayKey]; existing != "" {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "replay_detected", "source event is already linked to transfer "+existing)
		return
	}
	if !strings.EqualFold(intent.SourceChainID, submission.Proof.SourceChainID) || !strings.EqualFold(intent.AssetDenom, submission.Proof.AssetDenom) || !addressesEqual(intent.Recipient, submission.Proof.Recipient) {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusBadRequest, "intent_mismatch", "proof does not match deposit intent route and recipient")
		return
	}
	asset, found := bridgeAssetForDenom(s.bridgeState.Assets, submission.Proof.AssetDenom)
	if !found {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "asset_not_registered", "deposit asset is no longer registered")
		return
	}
	requestedAmount, requestedErr := bridgeDecimalUnits(intent.RequestedAmount, asset.Decimals)
	proofAmount, proofErr := bridgeDecimalUnits(submission.Proof.Amount, asset.Decimals)
	if requestedErr != nil || proofErr != nil || requestedAmount.Cmp(proofAmount) != 0 {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusBadRequest, "intent_amount_mismatch", "proof amount does not match the deposit intent")
		return
	}
	route, routeFound := findBridgeRoute(bridgeRoutesFromState(s.bridgeState, ledger.DTL), intent.RouteID)
	if !routeFound || !route.Ready {
		s.bridgeMu.Unlock()
		reason := "route_not_registered"
		if routeFound {
			reason = route.UnavailableReason
		}
		bridgeAPIError(w, http.StatusConflict, "route_not_ready", reason)
		return
	}
	chain, chainFound := bridgeChainForID(s.bridgeState.Chains, route.ChainID)
	contract, contractFound := bridgeContractForRoute(s.bridgeState.Contracts, route.RouteID)
	if !chainFound || !contractFound || !bridgeExternalAddressEqual(chain.ChainType, submission.Proof.EventContract, contract.ContractAddress) {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusBadRequest, "event_contract_mismatch", "proof was not emitted by the registered bridge contract")
		return
	}
	if route.DailyLimit != "" {
		limit, limitErr := bridgeDecimalUnits(route.DailyLimit, route.Decimals)
		projected := new(big.Int).Add(bridgeRouteVolume(s.bridgeState, route, time.Now().UTC()), proofAmount)
		if limitErr != nil || projected.Cmp(limit) > 0 {
			s.bridgeMu.Unlock()
			bridgeAPIError(w, http.StatusTooManyRequests, "daily_limit_exceeded", "verified transfer would exceed the route daily limit")
			return
		}
	}
	instruction, err := buildBridgeMintInstruction(s.bridgeState, ledger, currentHeight, submission.Proof, asset)
	if err != nil {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "mint_instruction_unavailable", err.Error())
		return
	}
	transferID, err := bridgeRandomID("btx_")
	if err != nil {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusInternalServerError, "id_generation_failed", "could not create transfer")
		return
	}
	now := time.Now().UTC().Unix()
	transfer := BridgeTransfer{
		TransferID: transferID, IntentID: intent.IntentID, RouteID: intent.RouteID, Direction: "deposit",
		SourceChainID: submission.Proof.SourceChainID, AssetDenom: submission.Proof.AssetDenom,
		Recipient: intent.Recipient, Amount: submission.Proof.Amount,
		SourceTxHash: bridgeSourceTransactionHash(submission.Proof), SourceLogIndex: submission.Proof.LogIndex,
		BridgeID: instruction.BridgeID, ReplayKey: replayKey,
		Status: "verified_waiting_dtl_authority_certificate", MintInstruction: &instruction,
		CreatedAtUnix: now, UpdatedAtUnix: now,
	}
	s.bridgeState.Transfers[transferID] = transfer
	s.bridgeState.ProcessedEvents[replayKey] = transferID
	intent.Status = transfer.Status
	intent.SourceTxHash = bridgeSourceTransactionHash(submission.Proof)
	intent.SourceLogIndex = submission.Proof.LogIndex
	intent.BridgeID = instruction.BridgeID
	intent.ReplayKey = replayKey
	s.bridgeState.DepositIntents[intent.IntentID] = intent
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "deposit_proof_verified", RouteID: intent.RouteID,
		IntentID: intent.IntentID, TransferID: transferID, ReplayKey: replayKey,
		Status: transfer.Status, Detail: result.Verification, CreatedAtUnix: now,
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusAccepted, map[string]any{"verification": result, "transfer": transfer, "state_root": stateRoot})
}

func bridgeMintCertMatchesInstruction(cert DTLGovernanceCert, instruction BridgeMintInstruction) bool {
	template := instruction.GovernanceCertTemplate
	return normalizeDTLTokenID(cert.TokenID) == normalizeDTLTokenID(template.TokenID) &&
		cert.Epoch == template.Epoch &&
		normalizeDTLGovernanceNonce(cert.Nonce) == normalizeDTLGovernanceNonce(template.Nonce) &&
		cert.Sequence == template.Sequence &&
		cert.Expiry == template.Expiry &&
		cert.Action == template.Action &&
		strings.EqualFold(normalizeDTLHex(cert.ActionPayloadHash), normalizeDTLHex(template.ActionPayloadHash))
}

func validateBridgeMintInstructionBinding(instruction BridgeMintInstruction) error {
	payloadHash, err := bridgeMintPayloadHash(instruction.MintPayload)
	if err != nil || !strings.EqualFold(payloadHash, instruction.ActionPayloadHash) {
		return fmt.Errorf("mint payload hash mismatch")
	}
	certificateHash, err := DTLPayloadHash(instruction.CertificatePayload)
	if err != nil || !strings.EqualFold(certificateHash, instruction.CertificatePayloadHash) {
		return fmt.Errorf("bridge certificate payload hash mismatch")
	}
	if len(instruction.CertificatePayloadHash) != 64 {
		return fmt.Errorf("bridge certificate payload hash length invalid")
	}
	payload := instruction.CertificatePayload
	if payload.BridgeID != instruction.BridgeID || payload.BridgeVersion != BridgeProtocolVersion ||
		payload.DestinationChainID != protocolChainID() ||
		!strings.EqualFold(payload.LocalTokenID, instruction.TokenID) ||
		!strings.EqualFold(payload.LocalTokenID, instruction.MintPayload.TokenID) ||
		payload.MintAmount != instruction.MintPayload.Amount ||
		!addressesEqual(payload.Receiver, instruction.MintPayload.To) ||
		!strings.EqualFold(payload.DTLPayloadHash, instruction.ActionPayloadHash) {
		return fmt.Errorf("bridge certificate context mismatch")
	}
	template := instruction.GovernanceCertTemplate
	wantNonce := "bridge-mint-" + instruction.BridgeID + "-" + instruction.CertificatePayloadHash[:40]
	if normalizeDTLGovernanceNonce(template.Nonce) != normalizeDTLGovernanceNonce(wantNonce) ||
		template.Epoch != payload.Epoch || template.Sequence != payload.Sequence || template.Expiry != payload.Expiry {
		return fmt.Errorf("bridge certificate replay envelope mismatch")
	}
	return nil
}

func (s *Server) handleBridgeAdminMint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var request bridgeMintPrepareRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_mint_certificate", err.Error())
		return
	}
	request.TransferID = strings.TrimSpace(request.TransferID)
	request.Proposer = strings.TrimSpace(request.Proposer)
	if request.TransferID == "" {
		bridgeAPIError(w, http.StatusBadRequest, "transfer_id_required", "transfer_id is required")
		return
	}
	if _, err := decodeAddressPayload(request.Proposer); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_proposer", "proposer must be a valid MSC wallet address")
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	if state.Paused {
		bridgeAPIError(w, http.StatusConflict, "bridge_paused", state.PauseReason)
		return
	}
	transfer, found := state.Transfers[request.TransferID]
	if !found || transfer.MintInstruction == nil {
		bridgeAPIError(w, http.StatusNotFound, "mint_instruction_not_found", "transfer has no DTL mint instruction")
		return
	}
	instruction := *transfer.MintInstruction
	if err := validateBridgeMintInstructionBinding(instruction); err != nil {
		bridgeAPIError(w, http.StatusConflict, "mint_instruction_corrupt", err.Error())
		return
	}
	if !strings.EqualFold(transfer.BridgeID, instruction.BridgeID) ||
		!strings.EqualFold(transfer.SourceChainID, instruction.SourceChainID) ||
		!strings.EqualFold(transfer.SourceTxHash, instruction.SourceTxHash) ||
		transfer.SourceLogIndex != instruction.SourceLogIndex ||
		!addressesEqual(transfer.Recipient, instruction.MintPayload.To) {
		bridgeAPIError(w, http.StatusConflict, "mint_instruction_transfer_mismatch", "mint instruction is not bound to this transfer")
		return
	}
	if !bridgeMintCertMatchesInstruction(request.GovernanceCert, instruction) {
		bridgeAPIError(w, http.StatusBadRequest, "mint_certificate_mismatch", "certificate does not match the event-bound mint instruction")
		return
	}
	ledger, currentHeight, err := s.bridgeLedgerSnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "local_token_state_unavailable", err.Error())
		return
	}
	token := ledger.DTL.Tokens[normalizeDTLTokenID(instruction.TokenID)]
	if token == nil {
		bridgeAPIError(w, http.StatusConflict, "local_token_not_found", "wrapped token no longer exists")
		return
	}
	mint := instruction.MintPayload
	mint.Proposer = displayAddress(request.Proposer)
	if err := ValidateDTLMintTx(ledger.DTL, mint); err != nil {
		bridgeAPIError(w, http.StatusConflict, "mint_validation_failed", err.Error())
		return
	}
	if err := ValidateDTLGovernanceCert(token, request.GovernanceCert, DTLGovMint, instruction.ActionPayloadHash, currentHeight, DTLDefaultReplayWindow); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "mint_certificate_invalid", err.Error())
		return
	}
	if err := dtlCheckReplay(ledger.DTL, request.GovernanceCert); err != nil {
		bridgeAPIError(w, http.StatusConflict, "mint_certificate_replayed", err.Error())
		return
	}
	payloadJSON, err := json.Marshal(mint)
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "mint_payload_failed", err.Error())
		return
	}
	certJSON, err := json.Marshal(request.GovernanceCert)
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "mint_certificate_failed", err.Error())
		return
	}
	tx := Transaction{
		From: request.Proposer, To: mint.To, Amount: 0,
		Nonce:  s.Node.Mempool.NextNonce(request.Proposer, ledger),
		Expiry: time.Now().UTC().Add(10 * time.Minute).Unix(),
		Type:   TxDTL, ChainID: protocolChainID(), Coin: CoinSymbol,
		DTLTxType: string(DTLTxTokenMint), DTLTokenID: instruction.TokenID,
		DTLPayload: string(payloadJSON), DTLGovernanceCert: string(certJSON),
	}
	tx.Fee = requiredDTLFeeForTx(tx)
	instruction.MintPayload = mint
	approvedCert := request.GovernanceCert
	instruction.ApprovedGovernanceCert = &approvedCert
	instruction.TransactionTemplate = &tx
	now := time.Now().UTC().Unix()
	s.bridgeMu.Lock()
	current, stillFound := s.bridgeState.Transfers[request.TransferID]
	if !stillFound || current.MintInstruction == nil || !strings.EqualFold(current.BridgeID, instruction.BridgeID) {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "transfer_changed", "transfer changed while mint certificate was validated")
		return
	}
	current.MintInstruction = &instruction
	current.Status = "consensus_mint_ready_for_wallet_signature"
	current.UpdatedAtUnix = now
	s.bridgeState.Transfers[request.TransferID] = current
	if intent, ok := s.bridgeState.DepositIntents[current.IntentID]; ok {
		intent.Status = current.Status
		s.bridgeState.DepositIntents[current.IntentID] = intent
	}
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "mint_certificate_approved", RouteID: current.RouteID,
		IntentID: current.IntentID, TransferID: current.TransferID, ReplayKey: current.ReplayKey,
		Status: current.Status, Detail: "DTL threshold certificate verified; outer wallet signature still required", CreatedAtUnix: now,
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusAccepted, map[string]any{
		"transfer": current, "transaction_template": tx, "state_root": stateRoot,
		"next_step": "sign the transaction template with the proposer wallet, then submit it to /submitTx",
	})
}

func (s *Server) handleBridgeAdminMintConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var request bridgeMintConfirmRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_confirmation", err.Error())
		return
	}
	request.TransferID = strings.TrimSpace(request.TransferID)
	request.MSCTransactionID = strings.TrimSpace(request.MSCTransactionID)
	if request.TransferID == "" || request.MSCTransactionID == "" {
		bridgeAPIError(w, http.StatusBadRequest, "confirmation_fields_required", "transfer_id and msc_transaction_id are required")
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	transfer, found := state.Transfers[request.TransferID]
	if !found || transfer.MintInstruction == nil {
		bridgeAPIError(w, http.StatusNotFound, "mint_instruction_not_found", "transfer has no DTL mint instruction")
		return
	}
	height, committedTx, found := s.Node.findTxRecordInChain(request.MSCTransactionID)
	if !found {
		bridgeAPIError(w, http.StatusNotFound, "mint_transaction_not_committed", "MSC transaction was not found in the committed chain")
		return
	}
	if committedTx.Type != TxDTL || committedTx.DTLTxType != string(DTLTxTokenMint) {
		bridgeAPIError(w, http.StatusBadRequest, "not_a_dtl_mint", "committed transaction is not a DTL token mint")
		return
	}
	decoded, err := decodeDTLTransaction(committedTx)
	if err != nil || decoded.Mint == nil || decoded.MintCert == nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_committed_mint", "committed DTL mint payload is invalid")
		return
	}
	instruction := transfer.MintInstruction
	if err := validateBridgeMintInstructionBinding(*instruction); err != nil {
		bridgeAPIError(w, http.StatusConflict, "mint_instruction_corrupt", err.Error())
		return
	}
	committedBridgeID, bridgeBound := bridgeIDFromMintCertificate(*decoded.MintCert)
	if !bridgeBound || !strings.EqualFold(committedBridgeID, transfer.BridgeID) ||
		!bridgeMintCertMatchesInstruction(*decoded.MintCert, *instruction) ||
		!strings.EqualFold(decoded.Mint.TokenID, instruction.MintPayload.TokenID) ||
		!addressesEqual(decoded.Mint.To, instruction.MintPayload.To) ||
		decoded.Mint.Amount != instruction.MintPayload.Amount ||
		!strings.EqualFold(decoded.MintCert.ActionPayloadHash, instruction.ActionPayloadHash) {
		bridgeAPIError(w, http.StatusConflict, "committed_mint_mismatch", "committed transaction does not match the event-bound mint instruction")
		return
	}
	ledger, _, err := s.bridgeLedgerSnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "local_token_state_unavailable", err.Error())
		return
	}
	if _, consumed := ledger.UsedBridgeEvents[transfer.BridgeID]; !consumed {
		bridgeAPIError(w, http.StatusConflict, "bridge_replay_marker_missing", "committed ledger does not contain the bridge replay marker")
		return
	}
	now := time.Now().UTC().Unix()
	s.bridgeMu.Lock()
	current, stillFound := s.bridgeState.Transfers[request.TransferID]
	if !stillFound || !strings.EqualFold(current.BridgeID, transfer.BridgeID) {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "transfer_changed", "transfer changed during confirmation")
		return
	}
	current.Status = "completed"
	current.MSCTransactionID = committedTx.ID
	current.UpdatedAtUnix = now
	s.bridgeState.Transfers[request.TransferID] = current
	if intent, ok := s.bridgeState.DepositIntents[current.IntentID]; ok {
		intent.Status = current.Status
		intent.MSCTransactionID = committedTx.ID
		s.bridgeState.DepositIntents[current.IntentID] = intent
	}
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "consensus_mint_confirmed", RouteID: current.RouteID,
		IntentID: current.IntentID, TransferID: current.TransferID, ReplayKey: current.ReplayKey,
		Status: current.Status, Detail: fmt.Sprintf("MSC transaction %s committed at height %d", committedTx.ID, height), CreatedAtUnix: now,
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, map[string]any{"transfer": current, "committed_height": height, "state_root": stateRoot})
}

func validateBridgeBurnInstructionBinding(transfer BridgeTransfer) error {
	instruction := transfer.BurnInstruction
	if instruction == nil {
		return fmt.Errorf("burn instruction missing")
	}
	payload := instruction.BurnPayload
	context := payload.BridgeWithdrawal
	contextHash, err := DTLPayloadHash(context)
	if err != nil || !strings.EqualFold(contextHash, instruction.ContextHash) {
		return fmt.Errorf("withdrawal context hash mismatch")
	}
	if transfer.BridgeID != "bridge_withdrawal_"+contextHash || context.BridgeVersion != BridgeProtocolVersion ||
		context.TransferID != transfer.TransferID || normalizeBridgeRegistryID(context.RouteID) != normalizeBridgeRegistryID(transfer.RouteID) ||
		!strings.EqualFold(context.SourceChainID, protocolChainID()) || !strings.EqualFold(context.DestinationChainID, transfer.DestinationChainID) ||
		!strings.EqualFold(context.AssetDenom, transfer.AssetDenom) || !addressesEqual(context.Sender, transfer.Sender) ||
		strings.TrimSpace(context.ExternalRecipient) != strings.TrimSpace(transfer.ExternalRecipient) ||
		strings.TrimSpace(context.ExternalAmount) != strings.TrimSpace(transfer.Amount) ||
		!strings.EqualFold(context.AuthorizationHash, transfer.ReplayKey) {
		return fmt.Errorf("withdrawal context does not match transfer")
	}
	if !addressesEqual(payload.From, context.Sender) || !strings.EqualFold(payload.TokenID, context.LocalTokenID) ||
		payload.Amount != context.LocalAmount || instruction.LocalAmount != context.LocalAmount ||
		!strings.EqualFold(instruction.TokenID, context.LocalTokenID) {
		return fmt.Errorf("burn payload does not match withdrawal context")
	}
	tx := instruction.TransactionTemplate
	if tx.Type != TxDTL || tx.DTLTxType != string(DTLTxTokenBurn) || !addressesEqual(tx.From, context.Sender) ||
		!strings.EqualFold(tx.DTLTokenID, context.LocalTokenID) || tx.Expiry != context.ExpiresAtUnix {
		return fmt.Errorf("burn transaction envelope mismatch")
	}
	var txPayload BridgeBurnPayload
	if err := json.Unmarshal([]byte(tx.DTLPayload), &txPayload); err != nil {
		return fmt.Errorf("burn transaction payload invalid")
	}
	wantHash, err := DTLPayloadHash(payload)
	if err != nil {
		return err
	}
	gotHash, err := DTLPayloadHash(txPayload)
	if err != nil || !strings.EqualFold(wantHash, gotHash) {
		return fmt.Errorf("burn transaction payload mismatch")
	}
	return nil
}

func buildBridgeUnlockInstruction(state BridgeGatewayState, transfer BridgeTransfer, burnTxID string, burnHeight uint64) (BridgeUnlockInstruction, error) {
	contract, found := bridgeContractForRoute(state.Contracts, transfer.RouteID)
	if !found {
		return BridgeUnlockInstruction{}, fmt.Errorf("bridge contract is not registered")
	}
	asset, found := bridgeAssetForDenom(state.Assets, transfer.AssetDenom)
	if !found {
		return BridgeUnlockInstruction{}, fmt.Errorf("bridge asset is not registered")
	}
	validators := make([]BridgeValidatorConfig, 0, len(state.Validators))
	for _, validator := range state.Validators {
		pubRaw, err := hex.DecodeString(strings.TrimSpace(validator.PublicKey))
		if strings.EqualFold(validator.Status, BridgeRouteStatusActive) && err == nil && len(pubRaw) == ed25519.PublicKeySize {
			validators = append(validators, validator)
		}
	}
	sort.Slice(validators, func(i, j int) bool {
		return strings.ToLower(validators[i].ValidatorID) < strings.ToLower(validators[j].ValidatorID)
	})
	if BridgeOracleQuorum == 0 || len(validators) < int(BridgeOracleQuorum) {
		return BridgeUnlockInstruction{}, fmt.Errorf("active bridge validator quorum unavailable")
	}
	withdrawalID, err := bridgeExternalWithdrawalID(burnTxID, bridgeMSCBurnLogIndex)
	if err != nil {
		return BridgeUnlockInstruction{}, err
	}
	payload := BridgeUnlockCertificatePayload{
		BridgeVersion: BridgeProtocolVersion, BridgeID: transfer.BridgeID, TransferID: transfer.TransferID,
		MSCBurnTransactionID: burnTxID, MSCBurnLogIndex: bridgeMSCBurnLogIndex, MSCBurnHeight: burnHeight,
		ExternalWithdrawalID: withdrawalID,
		DestinationChainID:   transfer.DestinationChainID, AssetDenom: transfer.AssetDenom,
		OriginAsset: asset.OriginAsset, BridgeContract: contract.ContractAddress, VaultAddress: contract.DepositAddress,
		ExternalRecipient: transfer.ExternalRecipient, ExternalAmount: transfer.Amount,
	}
	payloadHash, err := DTLPayloadHash(payload)
	if err != nil {
		return BridgeUnlockInstruction{}, err
	}
	message := strings.Join([]string{"MSC", "BRIDGE_UNLOCK", BridgeProtocolVersion, strings.ToLower(payloadHash)}, "|")
	instruction := BridgeUnlockInstruction{
		CertificatePayload: payload, CertificatePayloadHash: payloadHash,
		ValidatorSigningMessage: message, ValidatorSigningBytesHex: hex.EncodeToString([]byte(message)),
		RequiredQuorum: BridgeOracleQuorum,
	}
	for _, validator := range validators {
		instruction.RequiredValidatorIDs = append(instruction.RequiredValidatorIDs, validator.ValidatorID)
		instruction.RequiredValidatorPublicKeys = append(instruction.RequiredValidatorPublicKeys, strings.ToLower(validator.PublicKey))
	}
	return instruction, nil
}

func bridgeKeccak256(raw []byte) [32]byte {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(raw)
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

// bridgeExternalWithdrawalID mirrors MSCBridgeVault.computeWithdrawalId.
// MSC burns use log index zero because a burn is a consensus transaction, not
// an event inside a receipt with multiple log positions.
func bridgeExternalWithdrawalID(burnTxID string, burnLogIndex uint64) (string, error) {
	rawHash := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(burnTxID)), "0x")
	if len(rawHash) != 64 {
		return "", fmt.Errorf("MSC burn transaction ID must be a 32-byte hex hash")
	}
	burnHash, err := hex.DecodeString(rawHash)
	if err != nil || bytesAllZero(burnHash) {
		return "", fmt.Errorf("MSC burn transaction ID must be a non-zero 32-byte hex hash")
	}
	domain := bridgeKeccak256([]byte("MSC_BRIDGE_WITHDRAWAL_V1"))
	sourceChain := bridgeKeccak256([]byte(protocolChainID()))
	abiEncoded := make([]byte, 32*4)
	copy(abiEncoded[0:32], domain[:])
	copy(abiEncoded[32:64], sourceChain[:])
	copy(abiEncoded[64:96], burnHash)
	binary.BigEndian.PutUint64(abiEncoded[120:128], burnLogIndex)
	withdrawalID := bridgeKeccak256(abiEncoded)
	return "0x" + hex.EncodeToString(withdrawalID[:]), nil
}

func bytesAllZero(raw []byte) bool {
	for _, value := range raw {
		if value != 0 {
			return false
		}
	}
	return true
}

func validateBridgeUnlockInstructionBinding(transfer BridgeTransfer) error {
	instruction := transfer.UnlockInstruction
	if instruction == nil {
		return fmt.Errorf("unlock instruction missing")
	}
	payload := instruction.CertificatePayload
	payloadHash, err := DTLPayloadHash(payload)
	if err != nil || !strings.EqualFold(payloadHash, instruction.CertificatePayloadHash) {
		return fmt.Errorf("unlock certificate payload hash mismatch")
	}
	wantWithdrawalID, withdrawalErr := bridgeExternalWithdrawalID(payload.MSCBurnTransactionID, payload.MSCBurnLogIndex)
	wantMessage := strings.Join([]string{"MSC", "BRIDGE_UNLOCK", BridgeProtocolVersion, strings.ToLower(payloadHash)}, "|")
	if instruction.ValidatorSigningMessage != wantMessage || instruction.ValidatorSigningBytesHex != hex.EncodeToString([]byte(wantMessage)) ||
		withdrawalErr != nil || canonicalBridgeWithdrawalID(payload.ExternalWithdrawalID) != wantWithdrawalID ||
		payload.BridgeVersion != BridgeProtocolVersion || payload.BridgeID != transfer.BridgeID || payload.TransferID != transfer.TransferID ||
		!strings.EqualFold(payload.MSCBurnTransactionID, transfer.MSCTransactionID) ||
		!strings.EqualFold(payload.DestinationChainID, transfer.DestinationChainID) ||
		!strings.EqualFold(payload.AssetDenom, transfer.AssetDenom) ||
		strings.TrimSpace(payload.ExternalRecipient) != strings.TrimSpace(transfer.ExternalRecipient) ||
		strings.TrimSpace(payload.ExternalAmount) != strings.TrimSpace(transfer.Amount) {
		return fmt.Errorf("unlock instruction does not match transfer")
	}
	if instruction.RequiredQuorum == 0 || len(instruction.RequiredValidatorPublicKeys) < int(instruction.RequiredQuorum) ||
		len(instruction.RequiredValidatorIDs) != len(instruction.RequiredValidatorPublicKeys) {
		return fmt.Errorf("unlock validator quorum invalid")
	}
	return nil
}

func validateBridgeUnlockProofBinding(chain BridgeChainConfig, transfer BridgeTransfer, proof BridgeProof) error {
	if transfer.UnlockInstruction == nil {
		return fmt.Errorf("unlock instruction missing")
	}
	payload := transfer.UnlockInstruction.CertificatePayload
	if !strings.EqualFold(proof.SourceChainID, transfer.DestinationChainID) ||
		!strings.EqualFold(proof.AssetDenom, transfer.AssetDenom) ||
		canonicalBridgeWithdrawalID(proof.WithdrawalID) != canonicalBridgeWithdrawalID(payload.ExternalWithdrawalID) ||
		!bridgeExternalAddressEqual(chain.ChainType, proof.EventContract, payload.BridgeContract) ||
		!bridgeExternalAddressEqual(chain.ChainType, proof.OriginAsset, payload.OriginAsset) ||
		!bridgeExternalAddressEqual(chain.ChainType, proof.Recipient, transfer.ExternalRecipient) {
		return fmt.Errorf("finalized event does not match the authorized destination release")
	}
	return nil
}

func verifyBridgeUnlockSignatures(instruction BridgeUnlockInstruction, candidates []BridgeOracleSignature, current []BridgeValidatorConfig) ([]BridgeOracleSignature, error) {
	required := make(map[string]struct{}, len(instruction.RequiredValidatorPublicKeys))
	for _, key := range instruction.RequiredValidatorPublicKeys {
		required[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	active := make(map[string]ed25519.PublicKey, len(current)*2)
	for _, validator := range current {
		if !strings.EqualFold(validator.Status, BridgeRouteStatusActive) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(validator.PublicKey))
		if _, ok := required[key]; !ok {
			continue
		}
		raw, err := hex.DecodeString(key)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		pub := ed25519.PublicKey(raw)
		active[key] = pub
		active[strings.ToLower(strings.TrimSpace(validator.ValidatorID))] = pub
	}
	verified := make(map[string]BridgeOracleSignature)
	for _, candidate := range candidates {
		lookup := strings.ToLower(strings.TrimSpace(candidate.Signer))
		if lookup == "" {
			lookup = strings.ToLower(strings.TrimSpace(candidate.PublicKey))
		}
		pub, ok := active[lookup]
		if !ok {
			continue
		}
		key := hex.EncodeToString(pub)
		if supplied := strings.ToLower(strings.TrimSpace(candidate.PublicKey)); supplied != "" && supplied != key {
			continue
		}
		sig, err := hex.DecodeString(strings.TrimSpace(candidate.Signature))
		if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, []byte(instruction.ValidatorSigningMessage), sig) {
			continue
		}
		candidate.PublicKey = key
		verified[key] = candidate
	}
	if len(verified) < int(instruction.RequiredQuorum) {
		return nil, fmt.Errorf("bridge validator quorum not met: got %d need %d", len(verified), instruction.RequiredQuorum)
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
	return out, nil
}

func (s *Server) handleBridgeAdminBurnConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var request bridgeBurnConfirmRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_confirmation", err.Error())
		return
	}
	request.TransferID = strings.TrimSpace(request.TransferID)
	request.MSCTransactionID = strings.TrimSpace(request.MSCTransactionID)
	if request.TransferID == "" || request.MSCTransactionID == "" {
		bridgeAPIError(w, http.StatusBadRequest, "confirmation_fields_required", "transfer_id and msc_transaction_id are required")
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	transfer, found := state.Transfers[request.TransferID]
	if !found || transfer.BurnInstruction == nil {
		bridgeAPIError(w, http.StatusNotFound, "burn_instruction_not_found", "transfer has no DTL burn instruction")
		return
	}
	if err := validateBridgeBurnInstructionBinding(transfer); err != nil {
		bridgeAPIError(w, http.StatusConflict, "burn_instruction_corrupt", err.Error())
		return
	}
	height, committedTx, found := s.Node.findTxRecordInChain(request.MSCTransactionID)
	if !found {
		bridgeAPIError(w, http.StatusNotFound, "burn_transaction_not_committed", "MSC transaction was not found in the committed chain")
		return
	}
	if committedTx.Type != TxDTL || committedTx.DTLTxType != string(DTLTxTokenBurn) {
		bridgeAPIError(w, http.StatusBadRequest, "not_a_dtl_burn", "committed transaction is not a DTL token burn")
		return
	}
	decoded, err := decodeDTLTransaction(committedTx)
	if err != nil || decoded.Burn == nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_committed_burn", "committed DTL burn payload is invalid")
		return
	}
	var committedPayload BridgeBurnPayload
	if err := json.Unmarshal([]byte(committedTx.DTLPayload), &committedPayload); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_bridge_burn_context", "committed burn has no valid bridge withdrawal context")
		return
	}
	wantPayloadHash, _ := DTLPayloadHash(transfer.BurnInstruction.BurnPayload)
	gotPayloadHash, _ := DTLPayloadHash(committedPayload)
	if !strings.EqualFold(wantPayloadHash, gotPayloadHash) || !addressesEqual(committedTx.From, transfer.Sender) ||
		!addressesEqual(decoded.Burn.From, transfer.Sender) || !strings.EqualFold(decoded.Burn.TokenID, transfer.BurnInstruction.TokenID) ||
		decoded.Burn.Amount != transfer.BurnInstruction.LocalAmount {
		bridgeAPIError(w, http.StatusConflict, "committed_burn_mismatch", "committed burn does not match the wallet-authorized withdrawal")
		return
	}
	instruction, err := buildBridgeUnlockInstruction(state, transfer, committedTx.ID, height)
	if err != nil {
		bridgeAPIError(w, http.StatusConflict, "unlock_instruction_unavailable", err.Error())
		return
	}
	now := time.Now().UTC()
	status := "burn_confirmed_waiting_bridge_validator_signatures"
	if state.Paused {
		status = "burn_confirmed_unlock_paused"
	}
	if route, ok := findBridgeRoute(bridgeRoutesFromState(state), transfer.RouteID); ok && route.DailyLimit != "" {
		amount, amountErr := bridgeDecimalUnits(transfer.Amount, route.Decimals)
		limit, limitErr := bridgeDecimalUnits(route.DailyLimit, route.Decimals)
		if amountErr != nil || limitErr != nil || new(big.Int).Add(bridgeRouteVolume(state, route, now), amount).Cmp(limit) > 0 {
			status = "burn_confirmed_rate_limit_hold"
		}
	}
	burnReplayKey := "msc-burn:" + strings.ToLower(committedTx.ID)
	s.bridgeMu.Lock()
	if existing := s.bridgeState.ProcessedEvents[burnReplayKey]; existing != "" && existing != transfer.TransferID {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "burn_replay_detected", "committed burn is already linked to transfer "+existing)
		return
	}
	current, stillFound := s.bridgeState.Transfers[transfer.TransferID]
	if !stillFound || current.BridgeID != transfer.BridgeID {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "transfer_changed", "transfer changed during burn confirmation")
		return
	}
	current.MSCTransactionID = committedTx.ID
	current.UnlockInstruction = &instruction
	current.Status = status
	current.UpdatedAtUnix = now.Unix()
	s.bridgeState.Transfers[current.TransferID] = current
	s.bridgeState.ProcessedEvents[burnReplayKey] = current.TransferID
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "consensus_burn_confirmed", RouteID: current.RouteID,
		TransferID: current.TransferID, ReplayKey: burnReplayKey, Status: current.Status,
		Detail: fmt.Sprintf("MSC burn %s committed at height %d", committedTx.ID, height), CreatedAtUnix: now.Unix(),
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, map[string]any{"transfer": current, "unlock_instruction": instruction, "committed_height": height, "state_root": stateRoot})
}

func (s *Server) handleBridgeAdminUnlockAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var request bridgeUnlockAuthorizeRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_unlock_authorization", err.Error())
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	if state.Paused {
		bridgeAPIError(w, http.StatusConflict, "bridge_paused", state.PauseReason)
		return
	}
	transfer, found := state.Transfers[strings.TrimSpace(request.TransferID)]
	if !found || transfer.UnlockInstruction == nil {
		bridgeAPIError(w, http.StatusNotFound, "unlock_instruction_not_found", "confirmed burn has no unlock instruction")
		return
	}
	if err := validateBridgeUnlockInstructionBinding(transfer); err != nil {
		bridgeAPIError(w, http.StatusConflict, "unlock_instruction_corrupt", err.Error())
		return
	}
	route, routeFound := findBridgeRoute(s.bridgeRoutesForState(state), transfer.RouteID)
	if !routeFound || !route.Ready {
		reason := "route_not_registered"
		if routeFound {
			reason = route.UnavailableReason
		}
		bridgeAPIError(w, http.StatusConflict, "route_not_ready", reason)
		return
	}
	if transfer.Status == "burn_confirmed_rate_limit_hold" && route.DailyLimit != "" {
		amount, amountErr := bridgeDecimalUnits(transfer.Amount, route.Decimals)
		limit, limitErr := bridgeDecimalUnits(route.DailyLimit, route.Decimals)
		if amountErr != nil || limitErr != nil || new(big.Int).Add(bridgeRouteVolume(state, route, time.Now().UTC()), amount).Cmp(limit) > 0 {
			bridgeAPIError(w, http.StatusTooManyRequests, "daily_limit_hold", "withdrawal remains queued by the route daily limit")
			return
		}
	}
	verified, err := verifyBridgeUnlockSignatures(*transfer.UnlockInstruction, request.Signatures, state.Validators)
	if err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "validator_quorum_invalid", err.Error())
		return
	}
	instruction := *transfer.UnlockInstruction
	instruction.ValidatorSignatures = verified
	instruction.Authorized = true
	now := time.Now().UTC().Unix()
	s.bridgeMu.Lock()
	current, stillFound := s.bridgeState.Transfers[transfer.TransferID]
	if !stillFound || current.UnlockInstruction == nil || current.BridgeID != transfer.BridgeID {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "transfer_changed", "transfer changed during unlock authorization")
		return
	}
	current.UnlockInstruction = &instruction
	current.Status = "external_unlock_authorized"
	current.UpdatedAtUnix = now
	s.bridgeState.Transfers[current.TransferID] = current
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "external_unlock_authorized", RouteID: current.RouteID,
		TransferID: current.TransferID, ReplayKey: current.ReplayKey, Status: current.Status,
		Detail: fmt.Sprintf("%d bridge validator signatures verified", len(verified)), CreatedAtUnix: now,
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusAccepted, map[string]any{
		"transfer": current, "unlock_package": instruction, "state_root": stateRoot,
		"next_step": "submit the authorized release package to the destination bridge contract, then confirm its finalized unlock proof",
	})
}

func (s *Server) handleBridgeAdminUnlockConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.requireBridgeAdmin(w, r) {
		if r.Method != http.MethodPost {
			bridgeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	var request bridgeUnlockConfirmRequest
	if err := decodeBridgeJSON(w, r, &request); err != nil {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_unlock_confirmation", err.Error())
		return
	}
	state, err := s.bridgeGatewaySnapshot()
	if err != nil {
		bridgeAPIError(w, http.StatusServiceUnavailable, "bridge_unavailable", err.Error())
		return
	}
	transfer, found := state.Transfers[strings.TrimSpace(request.TransferID)]
	if !found || transfer.UnlockInstruction == nil || !transfer.UnlockInstruction.Authorized {
		bridgeAPIError(w, http.StatusConflict, "unlock_not_authorized", "transfer has no threshold-authorized unlock package")
		return
	}
	if err := validateBridgeUnlockInstructionBinding(transfer); err != nil {
		bridgeAPIError(w, http.StatusConflict, "unlock_instruction_corrupt", err.Error())
		return
	}
	if bridgeProofEventType(request.Proof) != "unlock" {
		bridgeAPIError(w, http.StatusBadRequest, "invalid_unlock_event_type", "completion requires an unlock event proof")
		return
	}
	result := s.verifyBridgeProofWithGatewayState(request.Proof)
	if !result.Accepted {
		writeBridgeJSON(w, http.StatusBadRequest, result)
		return
	}
	chain, chainFound := bridgeChainForID(state.Chains, transfer.DestinationChainID)
	if !chainFound || validateBridgeUnlockProofBinding(chain, transfer, request.Proof) != nil {
		bridgeAPIError(w, http.StatusConflict, "unlock_proof_mismatch", "finalized event does not match the authorized destination release")
		return
	}
	asset, assetFound := bridgeAssetForDenom(state.Assets, transfer.AssetDenom)
	wantAmount, wantErr := bridgeDecimalUnits(transfer.Amount, asset.Decimals)
	gotAmount, gotErr := bridgeDecimalUnits(request.Proof.Amount, asset.Decimals)
	if !assetFound || wantErr != nil || gotErr != nil || wantAmount.Cmp(gotAmount) != 0 {
		bridgeAPIError(w, http.StatusConflict, "unlock_amount_mismatch", "finalized unlock amount does not match the burned amount")
		return
	}
	replayKey := strings.ToLower(result.ReplayProtectionKey)
	now := time.Now().UTC().Unix()
	s.bridgeMu.Lock()
	if existing := s.bridgeState.ProcessedEvents[replayKey]; existing != "" {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "unlock_replay_detected", "external unlock event is already linked to transfer "+existing)
		return
	}
	current, stillFound := s.bridgeState.Transfers[transfer.TransferID]
	if !stillFound || current.UnlockInstruction == nil || !current.UnlockInstruction.Authorized || current.BridgeID != transfer.BridgeID {
		s.bridgeMu.Unlock()
		bridgeAPIError(w, http.StatusConflict, "transfer_changed", "transfer changed during unlock confirmation")
		return
	}
	current.Status = "completed"
	current.ExternalTransactionID = bridgeSourceTransactionHash(request.Proof)
	current.UpdatedAtUnix = now
	s.bridgeState.Transfers[current.TransferID] = current
	s.bridgeState.ProcessedEvents[replayKey] = current.TransferID
	eventID, _ := bridgeRandomID("bge_")
	s.bridgeState.Events = append(s.bridgeState.Events, BridgeGatewayEvent{
		EventID: eventID, EventType: "external_unlock_confirmed", RouteID: current.RouteID,
		TransferID: current.TransferID, ReplayKey: replayKey, Status: current.Status,
		Detail: "destination unlock event passed finality and proof verification", CreatedAtUnix: now,
	})
	err = s.persistBridgeGatewayStateLocked()
	stateRoot := s.bridgeState.StateRoot
	s.bridgeMu.Unlock()
	if err != nil {
		bridgeAPIError(w, http.StatusInternalServerError, "persistence_failed", err.Error())
		return
	}
	writeBridgeJSON(w, http.StatusOK, map[string]any{"verification": result, "transfer": current, "state_root": stateRoot})
}
