package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

const (
	DefaultEVMGasLimit             uint64 = 500000
	MaxEVMGasLimit                 uint64 = 10000000
	zeroEVMWordHex                        = "0x0000000000000000000000000000000000000000000000000000000000000000"
	evmStateKeyBaseFee                    = "__sys:evm_base_fee"
	evmStateKeyAliasPrefix                = "__sys:evm_alias:"
	defaultEVMBaseFeeMin                  = 1
	defaultEVMBaseFeeMax                  = 50_000_000
	defaultEVMBaseFeeUpdateDivisor        = 8
	// Hard-disable switch for EVM stack in this codebase.
	// Keep true to fully remove EVM execution/config activation.
	EVMHardDisabled = true
)

var customVMMagicPrefix = []byte{0x4d, 0x53, 0x43, 0x43, 0x56, 0x4d, 0x01} // "MSCCVM\x01"

const (
	customVMKindCounter byte = 0x01
	customVMOpRead      byte = 0x00
	customVMOpIncrement byte = 0x01
	customVMOpSet       byte = 0x02
	customVMOpDecrement byte = 0x03
)

var (
	evmNativeUnitWei = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	// Runtime-configurable EVM profile loaded from [evm] config.
	ConfigEVMEnabled                     = false
	ConfigEVMDefaultGasLimit      uint64 = DefaultEVMGasLimit
	ConfigEVMMaxGasLimit          uint64 = MaxEVMGasLimit
	ConfigEVMBaseFeeMin                  = defaultEVMBaseFeeMin
	ConfigEVMBaseFeeMax                  = defaultEVMBaseFeeMax
	ConfigEVMBaseFeeUpdateDivisor        = defaultEVMBaseFeeUpdateDivisor
	ConfigEVMTargetTxPerBlock            = 0 // 0 => auto (GlobalConfig.MaxTxPerBlock/2)
	ConfigEVMCustomVMEnabled             = true
	ConfigEVMOnlyCustomVM                = true
)

func forceDisableEVMConfig() bool {
	changed := false
	if ConfigEVMEnabled {
		ConfigEVMEnabled = false
		changed = true
	}
	if ConfigEVMCustomVMEnabled {
		ConfigEVMCustomVMEnabled = false
		changed = true
	}
	if ConfigEVMOnlyCustomVM {
		ConfigEVMOnlyCustomVM = false
		changed = true
	}
	if ConfigEVMTargetTxPerBlock != 0 {
		ConfigEVMTargetTxPerBlock = 0
		changed = true
	}
	return changed
}

func init() {
	if EVMHardDisabled {
		forceDisableEVMConfig()
	}
}

type EVMSandboxResult struct {
	OutputHex       string                       `json:"output_hex"`
	OutputHash      string                       `json:"output_hash"`
	GasLimit        uint64                       `json:"gas_limit"`
	GasUsed         uint64                       `json:"gas_used"`
	ExecAddress     string                       `json:"exec_address,omitempty"`
	ContractAddress string                       `json:"contract_address,omitempty"`
	StateCode       map[string]string            `json:"state_code,omitempty"`
	StateStorage    map[string]map[string]string `json:"state_storage,omitempty"`
	StateBalances   map[string]string            `json:"state_balances,omitempty"`
	StateNonces     map[string]uint64            `json:"state_nonces,omitempty"`
	RuntimeLogs     []EVMRuntimeLog              `json:"runtime_logs,omitempty"`
}

type EVMRuntimeLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type decodedEVMRawTx struct {
	Hash    common.Hash
	From    common.Address
	To      *common.Address
	Nonce   uint64
	Gas     uint64
	Value   *big.Int
	Data    []byte
	ChainID *big.Int
}

func isEVMRawTransaction(tx Transaction) bool {
	return tx.Type == TxEVM && strings.TrimSpace(tx.EVMRawTx) != ""
}

func ensureEVMStateMap(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.EVMState == nil {
		ledger.EVMState = make(map[string]string)
	}
}

func ensureEVMCodeMap(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.EVMCode == nil {
		ledger.EVMCode = make(map[string]string)
	}
}

func ensureEVMStorageMap(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.EVMStorage == nil {
		ledger.EVMStorage = make(map[string]map[string]string)
	}
}

func normalizeEVMAddressKey(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if common.IsHexAddress(addr) {
		return strings.ToLower(common.HexToAddress(addr).Hex())
	}
	return strings.ToLower(evmAddressFromString(addr).Hex())
}

func evmAliasStateKey(evmAddr string) string {
	evmAddr = strings.TrimSpace(evmAddr)
	if !common.IsHexAddress(evmAddr) {
		return ""
	}
	return evmStateKeyAliasPrefix + strings.ToLower(common.HexToAddress(evmAddr).Hex())
}

func isLikelyMSCWalletAddress(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	hasPrefix := len(raw) >= len(addressPrefix) && strings.EqualFold(raw[:len(addressPrefix)], addressPrefix)
	trimmed := stripAddressPrefix(raw)
	switch len(trimmed) {
	case addressPayloadSizeV1 * 2:
		return true
	case addressPayloadSizeLegacy * 2:
		// 20-byte payload is ambiguous with plain EVM hex. Accept only if explicit MSC prefix is present.
		return hasPrefix
	default:
		return false
	}
}

// registerLedgerAddressAlias stores an explicit mapping from EVM alias (0x...)
// to an MSC wallet address, enabling Remix/MetaMask transfers to resolve to the
// native MSC account representation.
func registerLedgerAddressAlias(ledger *Ledger, addr string) string {
	if ledger == nil || !isLikelyMSCWalletAddress(addr) {
		return ""
	}
	if _, err := decodeAddressPayload(addr); err != nil {
		return ""
	}

	internal := canonicalAddressKey(addr)
	if internal == "" {
		return ""
	}
	alias := normalizeEVMAddressKey(internal)
	if !common.IsHexAddress(alias) {
		return ""
	}
	alias = common.HexToAddress(alias).Hex()

	ensureEVMStateMap(ledger)
	ledger.EVMState[evmAliasStateKey(alias)] = internal
	return alias
}

func lookupLedgerAddressAlias(ledger *Ledger, addr string) string {
	if ledger == nil {
		return ""
	}
	key := evmAliasStateKey(addr)
	if key == "" {
		return ""
	}
	ensureEVMStateMap(ledger)
	mapped := canonicalAddressKey(strings.TrimSpace(ledger.EVMState[key]))
	if mapped == "" {
		return ""
	}
	return mapped
}

// resolveLedgerAddressAlias maps a known EVM alias (0x...) back to a canonical
// MSC wallet address when possible, so MetaMask transfers can credit MSC
// wallets directly.
func resolveLedgerAddressAlias(ledger *Ledger, addr string) string {
	addr = canonicalAddressKey(addr)
	if addr == "" {
		return ""
	}
	if !common.IsHexAddress(addr) || ledger == nil {
		return addr
	}
	if mapped := lookupLedgerAddressAlias(ledger, addr); mapped != "" {
		return mapped
	}

	target := strings.ToLower(common.HexToAddress(addr).Hex())
	tryCandidate := func(candidate string) (string, bool) {
		candidate = canonicalAddressKey(candidate)
		if candidate == "" {
			return "", false
		}
		if _, err := decodeAddressPayload(candidate); err != nil {
			return "", false
		}
		if strings.EqualFold(toEVMHexAddress(candidate), target) {
			registerLedgerAddressAlias(ledger, candidate)
			return candidate, true
		}
		return "", false
	}

	for key := range ledger.Balances {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if resolved, ok := tryCandidate(parts[1]); ok {
			return resolved
		}
	}
	for candidate := range ledger.Nonces {
		if resolved, ok := tryCandidate(candidate); ok {
			return resolved
		}
	}
	for key := range ledger.Stakes {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if resolved, ok := tryCandidate(parts[0]); ok {
			return resolved
		}
	}
	for _, candidate := range ledger.ValidatorRewardWallets {
		if resolved, ok := tryCandidate(candidate); ok {
			return resolved
		}
	}
	return addr
}

func normalizeEVMHexData(v string) string {
	raw := strings.ToLower(stripHexPrefix(v))
	if raw == "" {
		return "0x"
	}
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	return "0x" + raw
}

func normalizeEVMStorageSlotKey(slot string) string {
	raw := strings.ToLower(stripHexPrefix(slot))
	if raw == "" {
		raw = "0"
	}
	if len(raw) > 64 {
		raw = raw[len(raw)-64:]
	}
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	if len(raw) < 64 {
		raw = strings.Repeat("0", 64-len(raw)) + raw
	}
	return "0x" + raw
}

func normalizeEVMStorageValue(value string) string {
	raw := strings.ToLower(stripHexPrefix(value))
	if raw == "" {
		return zeroEVMWordHex
	}
	if len(raw) > 64 {
		raw = raw[len(raw)-64:]
	}
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	if len(raw) < 64 {
		raw = strings.Repeat("0", 64-len(raw)) + raw
	}
	return "0x" + raw
}

func evmInputWords(input string) []string {
	raw := strings.ToLower(stripHexPrefix(input))
	if raw == "" {
		return nil
	}
	// If calldata carries a 4-byte selector + 32-byte words, skip selector.
	if len(raw) >= 8+64 && (len(raw)-8)%64 == 0 {
		raw = raw[8:]
	}
	if len(raw) == 0 || len(raw)%64 != 0 {
		return nil
	}
	words := make([]string, 0, len(raw)/64)
	for i := 0; i < len(raw); i += 64 {
		words = append(words, "0x"+raw[i:i+64])
	}
	return words
}

func deterministicEVMStorageMutation(input string) (slot string, value string, ok bool) {
	words := evmInputWords(input)
	if len(words) < 2 {
		return "", "", false
	}
	return normalizeEVMStorageSlotKey(words[0]), normalizeEVMStorageValue(words[1]), true
}

func evmAddressFromTxFrom(from string) common.Address {
	if common.IsHexAddress(from) {
		return common.HexToAddress(from)
	}
	return evmAddressFromString(from)
}

func deriveEVMContractAddress(tx Transaction) string {
	from := evmAddressFromTxFrom(tx.From)
	var evmNonce uint64
	if tx.Nonce > 0 {
		evmNonce = uint64(tx.Nonce - 1)
	}
	return strings.ToLower(ethcrypto.CreateAddress(from, evmNonce).Hex())
}

func evmExecutionAddress(tx Transaction) string {
	if strings.TrimSpace(tx.To) != "" {
		return normalizeEVMAddressKey(tx.To)
	}
	if strings.TrimSpace(tx.From) == "" {
		return ""
	}
	return deriveEVMContractAddress(tx)
}

func evmCodeByAddress(ledger Ledger, addr string) string {
	key := normalizeEVMAddressKey(addr)
	if key == "" || ledger.EVMCode == nil {
		return "0x"
	}
	code := strings.TrimSpace(ledger.EVMCode[key])
	if code == "" {
		return "0x"
	}
	return normalizeEVMHexData(code)
}

func evmStorageByAddress(ledger Ledger, addr, slot string) string {
	key := normalizeEVMAddressKey(addr)
	slotKey := normalizeEVMStorageSlotKey(slot)
	if key == "" || ledger.EVMStorage == nil {
		return zeroEVMWordHex
	}
	contractSlots, ok := ledger.EVMStorage[key]
	if !ok || contractSlots == nil {
		return zeroEVMWordHex
	}
	return normalizeEVMStorageValue(contractSlots[slotKey])
}

func setEVMStorageWord(ledger *Ledger, addr, slot, value string) {
	if ledger == nil {
		return
	}
	key := normalizeEVMAddressKey(addr)
	if key == "" {
		return
	}
	ensureEVMStorageMap(ledger)
	if ledger.EVMStorage[key] == nil {
		ledger.EVMStorage[key] = make(map[string]string)
	}
	ledger.EVMStorage[key][normalizeEVMStorageSlotKey(slot)] = normalizeEVMStorageValue(value)
}

func isCustomVMBytecode(code []byte) bool {
	if len(code) < len(customVMMagicPrefix)+1 {
		return false
	}
	return bytes.Equal(code[:len(customVMMagicPrefix)], customVMMagicPrefix)
}

func customVMKind(code []byte) byte {
	if !isCustomVMBytecode(code) {
		return 0
	}
	return code[len(customVMMagicPrefix)]
}

func decodeUint256Word(raw string) *big.Int {
	word, err := decodeHexBytes(raw)
	if err != nil || len(word) == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(word)
}

func encodeUint256Word(v *big.Int) string {
	if v == nil || v.Sign() <= 0 {
		return zeroEVMWordHex
	}
	if v.BitLen() > 256 {
		v = new(big.Int).SetUint64(^uint64(0))
	}
	word := make([]byte, 32)
	v.FillBytes(word)
	return normalizeEVMStorageValue("0x" + hex.EncodeToString(word))
}

func uint256FromLedgerInt(v int) *uint256.Int {
	if v <= 0 {
		return uint256.NewInt(0)
	}
	return uint256.NewInt(uint64(v))
}

func evmSandboxHashForNumber(n uint64) common.Hash {
	sum := sha256.Sum256([]byte(fmt.Sprintf("msc-evm-block-%d", n)))
	return common.BytesToHash(sum[:])
}

func applyEVMExecutionState(ledger *Ledger, tx Transaction, result EVMSandboxResult) {
	if ledger == nil {
		return
	}
	ensureEVMStateMap(ledger)
	ensureEVMCodeMap(ledger)
	ensureEVMStorageMap(ledger)

	ledger.EVMState[evmStateKey(tx)] = strings.ToLower(strings.TrimSpace(result.OutputHash))

	target := strings.ToLower(strings.TrimSpace(result.ExecAddress))
	if target == "" {
		target = evmExecutionAddress(tx)
	}
	if target == "" {
		return
	}

	if len(result.StateBalances) > 0 {
		coin := normalizeCoin(tx.Coin)
		maxInt := int(^uint(0) >> 1)
		for addr, raw := range result.StateBalances {
			resolvedAddr := resolveLedgerAddressAlias(ledger, addr)
			if resolvedAddr == "" {
				continue
			}
			bn := new(big.Int)
			if _, ok := bn.SetString(strings.TrimSpace(raw), 10); !ok {
				continue
			}
			if bn.Sign() <= 0 {
				setBalance(ledger, coin, resolvedAddr, 0)
				continue
			}
			if bn.BitLen() > 63 {
				setBalance(ledger, coin, resolvedAddr, maxInt)
				continue
			}
			val := int(bn.Int64())
			if val < 0 {
				val = 0
			}
			if val > maxInt {
				val = maxInt
			}
			setBalance(ledger, coin, resolvedAddr, val)
		}
	}

	if len(result.StateNonces) > 0 {
		maxInt := int(^uint(0) >> 1)
		for addr, nonce := range result.StateNonces {
			resolvedAddr := resolveLedgerAddressAlias(ledger, addr)
			if resolvedAddr == "" {
				continue
			}
			internal := nonce
			if internal > uint64(maxInt) {
				internal = uint64(maxInt)
			}
			setNonce(ledger, resolvedAddr, int(internal))
		}
	}

	if len(result.StateCode) > 0 {
		for addr, codeHex := range result.StateCode {
			addrKey := normalizeEVMAddressKey(addr)
			if addrKey == "" {
				continue
			}
			normCode := normalizeEVMHexData(codeHex)
			if normCode == "0x" {
				continue
			}
			ledger.EVMCode[addrKey] = normCode
		}
	} else {
		code := normalizeEVMHexData(tx.EVMCode)
		if strings.TrimSpace(tx.To) == "" {
			if code != "0x" {
				ledger.EVMCode[target] = code
			}
		} else {
			if _, ok := ledger.EVMCode[target]; !ok && code != "0x" {
				ledger.EVMCode[target] = code
			}
		}
	}

	if len(result.StateStorage) > 0 {
		for addr, slots := range result.StateStorage {
			addrKey := normalizeEVMAddressKey(addr)
			if addrKey == "" {
				continue
			}
			for slot, val := range slots {
				setEVMStorageWord(ledger, addrKey, slot, val)
			}
		}
	}

	if len(result.StateStorage) == 0 {
		// Backward-compatible fallback for old custom-calldata storage writes
		// when full EVM state diff is unavailable.
		if slot, value, ok := deterministicEVMStorageMutation(tx.EVMInput); ok {
			setEVMStorageWord(ledger, target, slot, value)
		}
	}
}

func evmConfiguredGasLimits() (defaultGas uint64, maxGas uint64) {
	defaultGas = ConfigEVMDefaultGasLimit
	maxGas = ConfigEVMMaxGasLimit
	if maxGas == 0 {
		maxGas = MaxEVMGasLimit
	}
	if defaultGas == 0 {
		defaultGas = DefaultEVMGasLimit
	}
	if defaultGas > maxGas {
		defaultGas = maxGas
	}
	if defaultGas == 0 {
		defaultGas = DefaultEVMGasLimit
	}
	return defaultGas, maxGas
}

func currentEVMDefaultGasLimit() uint64 {
	defaultGas, _ := evmConfiguredGasLimits()
	return defaultGas
}

func currentEVMMaxGasLimit() uint64 {
	_, maxGas := evmConfiguredGasLimits()
	return maxGas
}

func evmConfiguredBaseFeeBounds() (baseMin int, baseMax int) {
	baseMin = ConfigEVMBaseFeeMin
	baseMax = ConfigEVMBaseFeeMax
	if baseMin < 1 {
		baseMin = defaultEVMBaseFeeMin
	}
	if baseMax < baseMin {
		baseMax = baseMin
	}
	return baseMin, baseMax
}

func evmConfiguredBaseFeeUpdateDivisor() int {
	if ConfigEVMBaseFeeUpdateDivisor <= 0 {
		return defaultEVMBaseFeeUpdateDivisor
	}
	return ConfigEVMBaseFeeUpdateDivisor
}

func normalizeEVMGasLimit(gasLimit uint64) uint64 {
	defaultGas, maxGas := evmConfiguredGasLimits()
	if gasLimit == 0 {
		return defaultGas
	}
	if gasLimit > maxGas {
		return maxGas
	}
	return gasLimit
}

func ComputeEVMFee(gasLimit uint64) int {
	baseMin, baseMax := evmConfiguredBaseFeeBounds()
	gl := normalizeEVMGasLimit(gasLimit)
	fee := int(gl / 1000)
	if fee < baseMin {
		fee = baseMin
	}
	if fee > baseMax {
		fee = baseMax
	}
	return fee
}

func evmDefaultBaseFee() int {
	return ComputeEVMFee(currentEVMDefaultGasLimit())
}

func clampEVMBaseFee(baseFee int) int {
	baseMin, baseMax := evmConfiguredBaseFeeBounds()
	if baseFee < baseMin {
		return baseMin
	}
	if baseFee > baseMax {
		return baseMax
	}
	return baseFee
}

func evmTargetTxPerBlock() int {
	if ConfigEVMTargetTxPerBlock > 0 {
		return ConfigEVMTargetTxPerBlock
	}
	target := GlobalConfig.MaxTxPerBlock / 2
	if target <= 0 {
		target = 1
	}
	return target
}

func evmBaseFeeFromLedger(ledger *Ledger) int {
	if ledger == nil || ledger.EVMState == nil {
		return evmDefaultBaseFee()
	}
	raw := strings.TrimSpace(ledger.EVMState[evmStateKeyBaseFee])
	if raw == "" {
		return evmDefaultBaseFee()
	}
	baseFee, err := strconv.Atoi(raw)
	if err != nil {
		return evmDefaultBaseFee()
	}
	return clampEVMBaseFee(baseFee)
}

func computeEVMFeeFromBase(baseFee int, gasLimit uint64) int {
	defaultGas := currentEVMDefaultGasLimit()
	baseMin, baseMax := evmConfiguredBaseFeeBounds()
	baseFee = clampEVMBaseFee(baseFee)
	gl := normalizeEVMGasLimit(gasLimit)
	fee64 := (int64(baseFee)*int64(gl) + int64(defaultGas) - 1) / int64(defaultGas)
	if fee64 < int64(baseMin) {
		fee64 = int64(baseMin)
	}
	if fee64 > int64(baseMax) {
		fee64 = int64(baseMax)
	}
	return int(fee64)
}

func requiredEVMFeeForLedger(ledger *Ledger, gasLimit uint64) int {
	return computeEVMFeeFromBase(evmBaseFeeFromLedger(ledger), gasLimit)
}

func computeEVMFeeWithDemandBase(baseFee int, demandTxCount int) int {
	_, baseMax := evmConfiguredBaseFeeBounds()
	baseFee = clampEVMBaseFee(baseFee)
	if demandTxCount <= 0 {
		return baseFee
	}

	target := evmTargetTxPerBlock()

	if demandTxCount < 0 {
		demandTxCount = 0
	}

	loadBPS := (demandTxCount * 10000) / target
	if loadBPS < 0 {
		loadBPS = 0
	}
	if loadBPS > 20000 {
		loadBPS = 20000
	}

	// 1.00x .. 3.00x multiplier depending on observed demand.
	multiplierBPS := 10000 + loadBPS
	fee64 := (int64(baseFee)*int64(multiplierBPS) + 9999) / 10000
	if fee64 < int64(baseFee) {
		fee64 = int64(baseFee)
	}
	if fee64 > int64(baseMax) {
		fee64 = int64(baseMax)
	}
	return int(fee64)
}

func ComputeEVMFeeWithDemand(gasLimit uint64, demandTxCount int) int {
	return computeEVMFeeWithDemandBase(ComputeEVMFee(gasLimit), demandTxCount)
}

func ComputeEVMFeeWithDemandAndLedger(ledger *Ledger, gasLimit uint64, demandTxCount int) int {
	return computeEVMFeeWithDemandBase(requiredEVMFeeForLedger(ledger, gasLimit), demandTxCount)
}

func computeNextEVMBaseFee(currentBaseFee int, evmTxCount int) int {
	current := clampEVMBaseFee(currentBaseFee)
	target := evmTargetTxPerBlock()
	divisor := evmConfiguredBaseFeeUpdateDivisor()
	if evmTxCount < 0 {
		evmTxCount = 0
	}
	if evmTxCount == target {
		return current
	}

	deltaTx := evmTxCount - target
	if deltaTx < 0 {
		deltaTx = -deltaTx
	}
	delta := (current * deltaTx) / target / divisor
	if delta < 1 {
		delta = 1
	}

	next := current
	if evmTxCount > target {
		next += delta
	} else {
		next -= delta
	}
	return clampEVMBaseFee(next)
}

func applyNextEVMBaseFee(ledger *Ledger, evmTxCount int) {
	if ledger == nil {
		return
	}
	ensureEVMStateMap(ledger)
	current := evmBaseFeeFromLedger(ledger)
	next := computeNextEVMBaseFee(current, evmTxCount)
	ledger.EVMState[evmStateKeyBaseFee] = strconv.Itoa(next)
}

func dtlBaseFeeByType(dtlTxType string) int {
	switch strings.ToUpper(strings.TrimSpace(dtlTxType)) {
	case string(DTLTxTokenCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxPoolCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxDuelCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxLendMarketCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxTournamentCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxFarmCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxSeasonCreate):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxNFT721Create), string(DTLTxNFT1155Create):
		return GlobalConfig.DTLCreateBaseFee
	case string(DTLTxTokenMint):
		return GlobalConfig.DTLMintBaseFee
	case string(DTLTxNFT721Mint), string(DTLTxNFT1155Mint):
		return GlobalConfig.DTLMintBaseFee
	case string(DTLTxTokenBurn):
		return GlobalConfig.DTLBurnBaseFee
	case string(DTLTxTokenTransfer):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxNFT721Transfer), string(DTLTxNFT1155Transfer):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxTokenApprove), string(DTLTxTokenTransferFrom):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxPoolAdd), string(DTLTxPoolRemove), string(DTLTxPoolSwap), string(DTLTxPoolSwapRoute):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxDuelJoin), string(DTLTxDuelReveal), string(DTLTxDuelFinalize):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxLendDeposit), string(DTLTxLendBorrow), string(DTLTxLendRepay), string(DTLTxLendWithdraw), string(DTLTxLendLiquidate):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxTournamentJoin), string(DTLTxTournamentReveal), string(DTLTxTournamentFinalize):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxFarmStakeLP), string(DTLTxFarmUnstakeLP), string(DTLTxFarmClaim):
		return GlobalConfig.DTLTransferBaseFee
	case string(DTLTxSeasonFinalize), string(DTLTxSeasonClaim):
		return GlobalConfig.DTLTransferBaseFee
	default:
		return GlobalConfig.DTLTransferBaseFee
	}
}

func requiredDTLFeeByPayload(dtlTxType string, payloadBytes int) int {
	fee := dtlBaseFeeByType(dtlTxType)
	if fee <= 0 {
		fee = 1
	}
	if payloadBytes < 0 {
		payloadBytes = 0
	}
	perKB := GlobalConfig.DTLPayloadFeePerKB
	if perKB > 0 && payloadBytes > 0 {
		kb := (payloadBytes + 1023) / 1024
		fee += kb * perKB
	}
	if fee < 1 {
		fee = 1
	}
	return fee
}

func requiredDTLFeeForTx(tx Transaction) int {
	payloadBytes := len(strings.TrimSpace(tx.DTLPayload)) + len(strings.TrimSpace(tx.DTLGovernanceCert))
	return requiredDTLFeeByPayload(tx.DTLTxType, payloadBytes)
}

func maxAllowedDTLFee(requiredFee int) int {
	if requiredFee < 1 {
		requiredFee = 1
	}
	multiplier := GlobalConfig.DTLFeeMaxMultiplier
	if multiplier < 1 {
		multiplier = 1
	}
	limit := requiredFee * multiplier
	if limit < requiredFee {
		limit = requiredFee
	}
	return limit
}

func validateDTLFeeBounds(gotFee int, requiredFee int) error {
	if gotFee < requiredFee {
		return fmt.Errorf("invalid fee: got %d minimum %d", gotFee, requiredFee)
	}
	maxFee := maxAllowedDTLFee(requiredFee)
	if gotFee > maxFee {
		return fmt.Errorf("invalid fee: got %d maximum %d", gotFee, maxFee)
	}
	return nil
}

func requiredFeeForTx(tx Transaction) int {
	if tx.Type == TxEVM {
		return ComputeEVMFee(tx.EVMGasLimit)
	}
	if tx.Type == TxDTL {
		return requiredDTLFeeForTx(tx)
	}
	return ComputeTxFee(tx.Amount)
}

func requiredFeeForTxWithLedger(ledger *Ledger, tx Transaction) int {
	if tx.Type == TxEVM {
		return requiredEVMFeeForLedger(ledger, tx.EVMGasLimit)
	}
	return requiredFeeForTx(tx)
}

func stripHexPrefix(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X")) {
		return v[2:]
	}
	return v
}

func decodeHexBytes(v string) ([]byte, error) {
	raw := stripHexPrefix(v)
	if raw == "" {
		return []byte{}, nil
	}
	if len(raw)%2 != 0 {
		return nil, errors.New("hex payload must have even length")
	}
	out, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func evmAddressFromString(v string) common.Address {
	key := strings.ToLower(strings.TrimSpace(v))
	sum := sha256.Sum256([]byte(key))
	return common.BytesToAddress(sum[12:])
}

func validateEVMTransaction(tx Transaction) error {
	code := stripHexPrefix(tx.EVMCode)
	input := stripHexPrefix(tx.EVMInput)
	if code == "" {
		return errors.New("evm_code required")
	}
	if len(code) > MaxTxEVMCodeHexLen {
		return fmt.Errorf("evm_code too large: max %d hex chars", MaxTxEVMCodeHexLen)
	}
	if len(input) > MaxTxEVMInputHexLen {
		return fmt.Errorf("evm_input too large: max %d hex chars", MaxTxEVMInputHexLen)
	}
	decodedCode, err := decodeHexBytes(code)
	if err != nil {
		return fmt.Errorf("invalid evm_code: %w", err)
	}
	if _, err := decodeHexBytes(input); err != nil {
		return fmt.Errorf("invalid evm_input: %w", err)
	}
	if ConfigEVMOnlyCustomVM && !isCustomVMBytecode(decodedCode) {
		return errors.New("evm disabled: custom vm bytecode required")
	}
	return nil
}

// hydrateEVMExecutionCode fills tx.EVMCode from on-chain EVMCode when caller
// submits a contract-call style TxEVM (to != "", evm_code == "").
func hydrateEVMExecutionCode(ledger *Ledger, tx Transaction) (Transaction, error) {
	tx.EVMCode = strings.TrimSpace(tx.EVMCode)
	if tx.EVMCode != "" {
		return tx, nil
	}
	if strings.TrimSpace(tx.To) == "" {
		return tx, errors.New("evm_code required")
	}
	if ledger == nil {
		return tx, errors.New("evm_code required")
	}

	code := evmCodeByAddress(*ledger, tx.To)
	if code == "0x" {
		return tx, errors.New("evm contract code not found for target")
	}
	tx.EVMCode = code
	return tx, nil
}

func internalNonceFromEVMNonce(evmNonce uint64) (int, error) {
	maxInt := int(^uint(0) >> 1)
	if evmNonce >= uint64(maxInt) {
		return 0, errors.New("raw tx nonce overflow")
	}
	return int(evmNonce) + 1, nil
}

func internalAmountToEVMWei(amount int) *big.Int {
	if amount <= 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Mul(big.NewInt(int64(amount)), evmNativeUnitWei)
}

func evmWeiToInternalAmount(value *big.Int) (int, error) {
	if value == nil || value.Sign() == 0 {
		return 0, nil
	}
	if value.Sign() < 0 {
		return 0, errors.New("raw tx value must be non-negative")
	}

	whole := new(big.Int)
	remainder := new(big.Int)
	whole.QuoRem(value, evmNativeUnitWei, remainder)
	if remainder.Sign() != 0 {
		// Compatibility: wallets sometimes send tiny wei dust values on deploy/call.
		// Since ledger native balances are integer MSC, treat sub-1 MSC as zero.
		if whole.Sign() == 0 {
			return 0, nil
		}
		return 0, errors.New("raw tx value precision unsupported: use whole MSC units or value=0")
	}

	maxInt := int(^uint(0) >> 1)
	maxValue := big.NewInt(int64(maxInt))
	if whole.Cmp(maxValue) > 0 {
		return 0, errors.New("raw tx value overflow")
	}
	return int(whole.Int64()), nil
}

func evmRawValueToInt(value *big.Int) (int, error) {
	return evmWeiToInternalAmount(value)
}

func decodeEVMRawTransaction(rawHex string) (decodedEVMRawTx, error) {
	var out decodedEVMRawTx
	raw, err := decodeHexBytes(rawHex)
	if err != nil {
		return out, fmt.Errorf("invalid raw tx hex: %w", err)
	}
	if len(raw) == 0 {
		return out, errors.New("empty raw tx")
	}

	var tx ethtypes.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return out, fmt.Errorf("invalid raw tx encoding: %w", err)
	}
	out.Hash = tx.Hash()
	out.To = tx.To()
	out.Nonce = tx.Nonce()
	out.Gas = tx.Gas()
	out.Value = tx.Value()
	out.Data = tx.Data()
	out.ChainID = tx.ChainId()

	if out.ChainID == nil || out.ChainID.Sign() <= 0 {
		return out, errors.New("raw tx chain id missing")
	}

	signer := ethtypes.LatestSignerForChainID(out.ChainID)
	sender, err := ethtypes.Sender(signer, &tx)
	if err != nil {
		return out, fmt.Errorf("raw tx signature invalid: %w", err)
	}
	out.From = sender

	return out, nil
}

func evmRuntimeTxHash(tx Transaction) common.Hash {
	if strings.TrimSpace(tx.EVMTxHash) != "" && common.IsHexHash(tx.EVMTxHash) {
		return common.HexToHash(tx.EVMTxHash)
	}
	if strings.TrimSpace(tx.ID) != "" && common.IsHexHash(tx.ID) {
		return common.HexToHash(tx.ID)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(tx.From) + "|" + strings.TrimSpace(tx.To) + "|" + strconv.Itoa(tx.Nonce)))
	return common.BytesToHash(sum[:])
}

func validateEVMRawTransactionBinding(tx Transaction) error {
	decoded, err := decodeEVMRawTransaction(tx.EVMRawTx)
	if err != nil {
		return err
	}

	wantChainID := chainIDBigInt()
	if decoded.ChainID.Cmp(wantChainID) != 0 {
		return fmt.Errorf("raw tx chain id mismatch: got %s expected %s", decoded.ChainID.String(), wantChainID.String())
	}

	if strings.TrimSpace(tx.EVMTxHash) != "" {
		if !strings.EqualFold(stripHexPrefix(tx.EVMTxHash), stripHexPrefix(decoded.Hash.Hex())) {
			return errors.New("evm_tx_hash mismatch")
		}
	}

	if !common.IsHexAddress(tx.From) {
		return errors.New("raw tx from must be hex address")
	}
	if !strings.EqualFold(common.HexToAddress(tx.From).Hex(), decoded.From.Hex()) {
		return errors.New("raw tx sender mismatch")
	}

	expectedNonce, err := internalNonceFromEVMNonce(decoded.Nonce)
	if err != nil {
		return err
	}
	if tx.Nonce != expectedNonce {
		return fmt.Errorf("raw tx nonce mismatch: got %d expected %d", tx.Nonce, expectedNonce)
	}

	if decoded.To == nil {
		if strings.TrimSpace(tx.To) != "" {
			return errors.New("raw tx to mismatch")
		}
	} else {
		if !common.IsHexAddress(tx.To) {
			return errors.New("raw tx to must be hex address")
		}
		if !strings.EqualFold(common.HexToAddress(tx.To).Hex(), decoded.To.Hex()) {
			return errors.New("raw tx to mismatch")
		}
	}

	valueInt, err := evmRawValueToInt(decoded.Value)
	if err != nil {
		return err
	}
	if tx.Amount != valueInt {
		return errors.New("raw tx value mismatch")
	}

	if tx.EVMGasLimit != decoded.Gas {
		return errors.New("raw tx gas mismatch")
	}

	rawDataHex := strings.ToLower(hex.EncodeToString(decoded.Data))
	rawInputHex := strings.ToLower(stripHexPrefix(tx.EVMInput))
	rawCodeHex := strings.ToLower(stripHexPrefix(tx.EVMCode))
	if decoded.To == nil {
		if rawCodeHex != rawDataHex {
			return errors.New("raw tx data mismatch")
		}
		if rawInputHex != "" {
			return errors.New("raw tx input mismatch")
		}
	} else {
		if rawInputHex != rawDataHex {
			return errors.New("raw tx input mismatch")
		}
	}

	return nil
}

func evmStateKey(tx Transaction) string {
	target := strings.TrimSpace(tx.To)
	if target == "" {
		target = "evm:" + canonicalAddressKey(tx.From)
	}
	return strings.ToLower(target)
}

func newEVMStateFromLedger(ledger *Ledger, coin string) (*state.StateDB, error) {
	statedb, err := state.New(ethtypes.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		return nil, err
	}
	if ledger == nil {
		return statedb, nil
	}

	normalizedCoin := normalizeCoin(coin)
	for key, amount := range ledger.Balances {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if normalizeCoin(parts[0]) != normalizedCoin {
			continue
		}
		addrKey := normalizeEVMAddressKey(parts[1])
		if addrKey == "" {
			continue
		}
		addr := common.HexToAddress(addrKey)
		statedb.CreateAccount(addr)
		statedb.SetBalance(addr, uint256FromLedgerInt(amount), tracing.BalanceChangeUnspecified)
	}

	for rawAddr, nonce := range ledger.Nonces {
		addrKey := normalizeEVMAddressKey(rawAddr)
		if addrKey == "" {
			continue
		}
		addr := common.HexToAddress(addrKey)
		statedb.CreateAccount(addr)
		evmNonce := uint64(0)
		if nonce > 0 {
			evmNonce = uint64(nonce)
		}
		statedb.SetNonce(addr, evmNonce, tracing.NonceChangeUnspecified)
	}

	for rawAddr, codeHex := range ledger.EVMCode {
		addrKey := normalizeEVMAddressKey(rawAddr)
		if addrKey == "" {
			continue
		}
		addr := common.HexToAddress(addrKey)
		statedb.CreateAccount(addr)
		code, err := decodeHexBytes(codeHex)
		if err != nil {
			continue
		}
		if len(code) > 0 {
			statedb.SetCode(addr, code, tracing.CodeChangeUnspecified)
		}
	}

	for rawAddr, slots := range ledger.EVMStorage {
		addrKey := normalizeEVMAddressKey(rawAddr)
		if addrKey == "" {
			continue
		}
		addr := common.HexToAddress(addrKey)
		statedb.CreateAccount(addr)
		for rawSlot, rawValue := range slots {
			slot := common.HexToHash(normalizeEVMStorageSlotKey(rawSlot))
			val := common.HexToHash(normalizeEVMStorageValue(rawValue))
			statedb.SetState(addr, slot, val)
		}
	}

	return statedb, nil
}

func runCustomVMSandbox(tx Transaction, code []byte, input []byte, _ int, ledger *Ledger) (EVMSandboxResult, error) {
	if !ConfigEVMCustomVMEnabled {
		return EVMSandboxResult{}, errors.New("custom vm is disabled")
	}
	if !isCustomVMBytecode(code) {
		return EVMSandboxResult{}, errors.New("invalid custom vm bytecode")
	}

	kind := customVMKind(code)
	if kind != customVMKindCounter {
		return EVMSandboxResult{}, fmt.Errorf("unsupported custom vm kind: 0x%02x", kind)
	}

	gasLimit := normalizeEVMGasLimit(tx.EVMGasLimit)
	if strings.TrimSpace(tx.To) == "" {
		contractAddress := strings.ToLower(deriveEVMContractAddress(tx))
		out := []byte("MSC-CVM-COUNTER-V1")
		outHash := sha256.Sum256(out)
		gasUsed := uint64(70000)
		if gasUsed > gasLimit {
			gasUsed = gasLimit
		}
		return EVMSandboxResult{
			OutputHex:       hex.EncodeToString(out),
			OutputHash:      hex.EncodeToString(outHash[:]),
			GasLimit:        gasLimit,
			GasUsed:         gasUsed,
			ExecAddress:     contractAddress,
			ContractAddress: contractAddress,
			StateCode: map[string]string{
				contractAddress: normalizeEVMHexData(hex.EncodeToString(code)),
			},
		}, nil
	}

	addrKey := normalizeEVMAddressKey(tx.To)
	if addrKey == "" {
		return EVMSandboxResult{}, errors.New("invalid custom vm target address")
	}
	currentWord := zeroEVMWordHex
	if ledger != nil {
		currentWord = evmStorageByAddress(*ledger, addrKey, zeroEVMWordHex)
	}
	current := decodeUint256Word(currentWord)
	next := new(big.Int).Set(current)
	op := customVMOpRead
	if len(input) > 0 {
		op = input[0]
	}

	changed := false
	switch op {
	case customVMOpRead:
		// no-op
	case customVMOpIncrement:
		next.Add(next, big.NewInt(1))
		changed = true
	case customVMOpSet:
		if len(input) < 33 {
			return EVMSandboxResult{}, errors.New("custom vm set requires 32-byte value")
		}
		next.SetBytes(input[1:33])
		changed = true
	case customVMOpDecrement:
		if next.Sign() > 0 {
			next.Sub(next, big.NewInt(1))
			changed = true
		}
	default:
		return EVMSandboxResult{}, fmt.Errorf("unsupported custom vm opcode: 0x%02x", op)
	}

	retWord, _ := decodeHexBytes(encodeUint256Word(next))
	if len(retWord) == 0 {
		retWord = make([]byte, 32)
	}
	outHash := sha256.Sum256(retWord)

	stateStorage := map[string]map[string]string{}
	if changed {
		stateStorage[addrKey] = map[string]string{
			normalizeEVMStorageSlotKey(zeroEVMWordHex): encodeUint256Word(next),
		}
	}

	gasUsed := uint64(21000)
	if changed {
		gasUsed = 50000
	}
	if gasUsed > gasLimit {
		gasUsed = gasLimit
	}

	return EVMSandboxResult{
		OutputHex:    hex.EncodeToString(retWord),
		OutputHash:   hex.EncodeToString(outHash[:]),
		GasLimit:     gasLimit,
		GasUsed:      gasUsed,
		ExecAddress:  addrKey,
		StateStorage: stateStorage,
	}, nil
}

func runCustomEVMSandboxWithContext(tx Transaction, height int, ledger *Ledger, txIndex int) (EVMSandboxResult, error) {
	if err := validateEVMTransaction(tx); err != nil {
		return EVMSandboxResult{}, err
	}

	code, _ := decodeHexBytes(tx.EVMCode)
	input, _ := decodeHexBytes(tx.EVMInput)
	if ConfigEVMOnlyCustomVM && !isCustomVMBytecode(code) {
		return EVMSandboxResult{}, errors.New("evm disabled: only custom vm execution is enabled")
	}
	if isCustomVMBytecode(code) {
		return runCustomVMSandbox(tx, code, input, height, ledger)
	}

	blockNumber := big.NewInt(0)
	if height > 0 {
		blockNumber = big.NewInt(int64(height))
	}
	timestamp := uint64(1)
	if height > 0 {
		timestamp = uint64(height)
	}

	value := big.NewInt(0)
	if tx.Amount > 0 {
		value = big.NewInt(int64(tx.Amount))
	}

	stateCode := make(map[string]string)
	stateStorage := make(map[string]map[string]string)
	stateBalances := make(map[string]string)
	stateNonces := make(map[string]uint64)
	touchedStorage := make(map[string]map[common.Hash]struct{})
	logs := make([]EVMRuntimeLog, 0)
	hooks := &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			if op != byte(vm.SSTORE) {
				return
			}
			stack := scope.StackData()
			if len(stack) < 2 {
				return
			}
			addrKey := strings.ToLower(scope.Address().Hex())
			slot := common.Hash(stack[len(stack)-1].Bytes32())
			if touchedStorage[addrKey] == nil {
				touchedStorage[addrKey] = make(map[common.Hash]struct{})
			}
			touchedStorage[addrKey][slot] = struct{}{}
		},
		OnStorageChange: func(addr common.Address, slot common.Hash, prev, new common.Hash) {
			addrKey := strings.ToLower(addr.Hex())
			if stateStorage[addrKey] == nil {
				stateStorage[addrKey] = make(map[string]string)
			}
			stateStorage[addrKey][normalizeEVMStorageSlotKey(slot.Hex())] = normalizeEVMStorageValue(new.Hex())
		},
		OnCodeChange: func(addr common.Address, prevCodeHash common.Hash, prevCode []byte, codeHash common.Hash, code []byte) {
			if len(code) == 0 {
				return
			}
			addrKey := strings.ToLower(addr.Hex())
			stateCode[addrKey] = normalizeEVMHexData(hex.EncodeToString(code))
		},
		OnBalanceChange: func(addr common.Address, prev, new *big.Int, reason tracing.BalanceChangeReason) {
			if new == nil {
				return
			}
			addrKey := strings.ToLower(addr.Hex())
			stateBalances[addrKey] = new.String()
		},
		OnNonceChange: func(addr common.Address, prev, new uint64) {
			addrKey := strings.ToLower(addr.Hex())
			stateNonces[addrKey] = new
		},
		OnLog: func(lg *ethtypes.Log) {
			if lg == nil {
				return
			}
			topics := make([]string, 0, len(lg.Topics))
			for _, topic := range lg.Topics {
				topics = append(topics, topic.Hex())
			}
			logs = append(logs, EVMRuntimeLog{
				Address: strings.ToLower(lg.Address.Hex()),
				Topics:  topics,
				Data:    normalizeEVMHexData(hex.EncodeToString(lg.Data)),
			})
		},
	}

	baseFee := evmBaseFeeFromLedger(ledger)
	blockNumberU64 := uint64(0)
	if height > 0 {
		blockNumberU64 = uint64(height)
	}
	randomMix := evmSandboxHashForNumber(blockNumberU64)

	cfg := &runtime.Config{
		ChainConfig: params.AllEthashProtocolChanges,
		Difficulty:  big.NewInt(0),
		Origin:      evmAddressFromTxFrom(tx.From),
		Coinbase:    evmAddressFromString(TREASURY_ADDRESS),
		BlockNumber: blockNumber,
		Time:        timestamp,
		GasLimit:    normalizeEVMGasLimit(tx.EVMGasLimit),
		GasPrice:    big.NewInt(0),
		BaseFee:     big.NewInt(int64(baseFee)),
		BlobBaseFee: big.NewInt(0),
		Value:       value,
		Random:      &randomMix,
		EVMConfig: vm.Config{
			Tracer: hooks,
		},
		GetHashFn: func(n uint64) common.Hash {
			return evmSandboxHashForNumber(n)
		},
	}

	statedb, err := newEVMStateFromLedger(ledger, tx.Coin)
	if err != nil {
		return EVMSandboxResult{}, err
	}
	cfg.State = statedb
	cfg.State.SetTxContext(evmRuntimeTxHash(tx), txIndex)

	var (
		ret             []byte
		execAddr        string
		contractAddress string
		leftOverGas     uint64
	)
	if strings.TrimSpace(tx.To) == "" {
		_, addr, gasLeft, err := runtime.Create(code, cfg)
		if err != nil {
			return EVMSandboxResult{}, err
		}
		leftOverGas = gasLeft
		contractAddress = strings.ToLower(addr.Hex())
		execAddr = contractAddress
		ret = cfg.State.GetCode(addr)
	} else {
		toAddr := common.HexToAddress(normalizeEVMAddressKey(tx.To))
		var gasLeft uint64
		ret, gasLeft, err = runtime.Call(toAddr, input, cfg)
		if err != nil {
			return EVMSandboxResult{}, err
		}
		leftOverGas = gasLeft
		execAddr = strings.ToLower(toAddr.Hex())
	}

	for addrKey, slots := range touchedStorage {
		addr := common.HexToAddress(addrKey)
		if stateStorage[addrKey] == nil {
			stateStorage[addrKey] = make(map[string]string)
		}
		for slot := range slots {
			val := cfg.State.GetState(addr, slot)
			stateStorage[addrKey][normalizeEVMStorageSlotKey(slot.Hex())] = normalizeEVMStorageValue(val.Hex())
		}
	}

	if strings.TrimSpace(contractAddress) != "" {
		addrKey := strings.ToLower(contractAddress)
		if _, ok := stateCode[addrKey]; !ok {
			if codeBytes := cfg.State.GetCode(common.HexToAddress(addrKey)); len(codeBytes) > 0 {
				stateCode[addrKey] = normalizeEVMHexData(hex.EncodeToString(codeBytes))
			}
		}
	}
	outHash := sha256.Sum256(ret)
	gasUsed := cfg.GasLimit
	if leftOverGas <= cfg.GasLimit {
		gasUsed = cfg.GasLimit - leftOverGas
	}
	return EVMSandboxResult{
		OutputHex:       hex.EncodeToString(ret),
		OutputHash:      hex.EncodeToString(outHash[:]),
		GasLimit:        cfg.GasLimit,
		GasUsed:         gasUsed,
		ExecAddress:     execAddr,
		ContractAddress: contractAddress,
		StateCode:       stateCode,
		StateStorage:    stateStorage,
		StateBalances:   stateBalances,
		StateNonces:     stateNonces,
		RuntimeLogs:     logs,
	}, nil
}

func runCustomEVMSandbox(tx Transaction, height int, ledger *Ledger) (EVMSandboxResult, error) {
	return runCustomEVMSandboxWithContext(tx, height, ledger, 0)
}
