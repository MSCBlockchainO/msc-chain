package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/gorilla/websocket"
)

const (
	jsonRPCVersion = "2.0"
	emptyUncleHash = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
	emptyCodeHash  = "0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	emptyStateRoot = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	emptyLogsBloom = "0x" + "" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	msc21Standard                 = "MSC-21"
	msc721Standard                = "MSC-721"
	msc1155Standard               = "MSC-1155"
	mscOnlyChainID                = int64(91938)
	mscCoinFullName               = "Mythical System Coin"
	dtlNFTListLimitDefault uint64 = 50
	dtlNFTListLimitMax     uint64 = 200
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type ethCallObject struct {
	From                 string          `json:"from"`
	To                   string          `json:"to"`
	Gas                  string          `json:"gas"`
	Type                 string          `json:"type"`
	ChainID              string          `json:"chainId"`
	GasPrice             string          `json:"gasPrice"`
	MaxFeePerGas         string          `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string          `json:"maxPriorityFeePerGas"`
	AccessList           json.RawMessage `json:"accessList"`
	Value                string          `json:"value"`
	Data                 string          `json:"data"`
	Input                string          `json:"input"`
	Nonce                string          `json:"nonce"`
}

type rpcTxLocation struct {
	Tx         Transaction
	Block      Block
	Found      bool
	Pending    bool
	TxIndex    int
	Latest     uint64
	Finalized  uint64
	BlockFound bool
}

type rpcLogFilter struct {
	BlockHash string
	FromBlock string
	ToBlock   string
	Addresses map[string]struct{}
	Topics    []map[string]struct{}
}

type rpcCompatFilter struct {
	ID        string
	Kind      string
	LogFilter rpcLogFilter
	Seen      map[string]struct{}
}

type wsRPCSubscription struct {
	ID         string
	Kind       string
	LogFilter  rpcLogFilter
	Seen       map[string]struct{}
	LastHeight uint64
	// newPendingTransactions: when true, emit full tx object instead of tx hash.
	PendingFullTx bool
	// syncing: serialized previous state; emit only on state transitions.
	LastSyncState string
}

type wsSubscribeOptions struct {
	Kind          string
	LogFilter     rpcLogFilter
	PendingFullTx bool
}

var (
	rpcFilterMu  sync.Mutex
	rpcFilters   = make(map[string]*rpcCompatFilter)
	rpcFilterSeq uint64
)

var mscOnlyChainIDBig = big.NewInt(mscOnlyChainID)

var jsonRPCWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func rpcError(code int, msg string) *jsonRPCError {
	return &jsonRPCError{Code: code, Message: msg}
}

func parseRPCID(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var id any
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil
	}
	return id
}

func encodeRPCQuantityUint64(v uint64) string {
	return fmt.Sprintf("0x%x", v)
}

func encodeRPCQuantityInt(v int) string {
	if v <= 0 {
		return "0x0"
	}
	return fmt.Sprintf("0x%x", uint64(v))
}

func encodeRPCQuantityBig(v *big.Int) string {
	if v == nil || v.Sign() <= 0 {
		return "0x0"
	}
	return "0x" + strings.TrimLeft(v.Text(16), "0")
}

func encodeRPCNativeAmount(v int) string {
	return encodeRPCQuantityBig(internalAmountToEVMWei(v))
}

func parseRPCNativeAmount(v string) (int, error) {
	parsed, err := parseRPCBigIntString(v)
	if err != nil {
		return 0, errors.New("invalid value")
	}
	if parsed.Sign() < 0 {
		return 0, errors.New("value must be non-negative")
	}
	return evmWeiToInternalAmount(parsed)
}

func normalizeHexData(v string) string {
	raw := stripHexPrefix(v)
	if raw == "" {
		return "0x"
	}
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	return "0x" + strings.ToLower(raw)
}

func normalizeHexHash(v string) string {
	return normalizeHexData(v)
}

func toEVMHexAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if common.IsHexAddress(addr) {
		return common.HexToAddress(addr).Hex()
	}
	return evmAddressFromString(addr).Hex()
}

func rpcOptionalToAddress(addr string) any {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	return toEVMHexAddress(addr)
}

func rpcContractAddress(tx Transaction) any {
	if tx.Type != TxEVM || strings.TrimSpace(tx.To) != "" {
		return nil
	}
	derived := deriveEVMContractAddress(tx)
	if !common.IsHexAddress(derived) {
		return nil
	}
	return common.HexToAddress(derived).Hex()
}

func parseRPCQuantity(raw json.RawMessage) (uint64, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString == "" {
			return 0, nil
		}
		if strings.HasPrefix(asString, "0x") || strings.HasPrefix(asString, "0X") {
			v, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(asString, "0x"), "0X"), 16, 64)
			if err != nil {
				return 0, err
			}
			return v, nil
		}
		v, err := strconv.ParseUint(asString, 10, 64)
		if err != nil {
			return 0, err
		}
		return v, nil
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		if asFloat < 0 {
			return 0, errors.New("quantity must be non-negative")
		}
		if asFloat > math.MaxUint64 {
			return 0, errors.New("quantity overflow")
		}
		return uint64(asFloat), nil
	}

	return 0, errors.New("invalid quantity")
}

func parseRPCBool(raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, err
	}
	return b, nil
}

func rpcParamsAsArray(raw json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	if trimmed[0] == '{' {
		return []json.RawMessage{trimmed}, nil
	}
	return nil, errors.New("params must be array or object")
}

func chainIDBigInt() *big.Int {
	raw := strings.TrimSpace(ChainID)
	if raw == "" {
		return big.NewInt(0)
	}
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		v := new(big.Int)
		if _, ok := v.SetString(strings.TrimPrefix(strings.TrimPrefix(raw, "0x"), "0X"), 16); ok {
			return v
		}
	}
	v := new(big.Int)
	if _, ok := v.SetString(raw, 10); ok {
		return v
	}
	sum := uint64(0)
	for _, ch := range []byte(raw) {
		sum = sum*131 + uint64(ch)
	}
	return new(big.Int).SetUint64(sum)
}

func isMSCOnlyChainID(v *big.Int) bool {
	if v == nil {
		return false
	}
	return v.Cmp(mscOnlyChainIDBig) == 0
}

func parseWalletChainIDParam(raw json.RawMessage) (*big.Int, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, errors.New("missing chainId")
	}
	// Standard shape: { chainId: "0x..." }.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &payload); err == nil {
		if rawChainID, ok := payload["chainId"]; ok {
			var chainIDString string
			if err := json.Unmarshal(rawChainID, &chainIDString); err == nil && strings.TrimSpace(chainIDString) != "" {
				parsed, err := parseRPCBigIntString(chainIDString)
				if err != nil {
					return nil, errors.New("invalid chainId")
				}
				return parsed, nil
			}
			if qty, err := parseRPCQuantity(rawChainID); err == nil {
				return new(big.Int).SetUint64(qty), nil
			}
			return nil, errors.New("invalid chainId")
		}
	}
	// Compatibility fallback: direct string.
	var direct string
	if err := json.Unmarshal(trimmed, &direct); err == nil && strings.TrimSpace(direct) != "" {
		parsed, err := parseRPCBigIntString(direct)
		if err != nil {
			return nil, errors.New("invalid chainId")
		}
		return parsed, nil
	}
	if qty, err := parseRPCQuantity(trimmed); err == nil {
		return new(big.Int).SetUint64(qty), nil
	}
	return nil, errors.New("invalid chain params")
}

func parseRPCQuantityString(v string) (uint64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	return parseRPCQuantity(json.RawMessage(strconv.Quote(v)))
}

func parseRPCBigIntString(v string) (*big.Int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return big.NewInt(0), nil
	}
	if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
		raw := strings.TrimPrefix(strings.TrimPrefix(v, "0x"), "0X")
		if raw == "" {
			return big.NewInt(0), nil
		}
		out := new(big.Int)
		if _, ok := out.SetString(raw, 16); !ok {
			return nil, errors.New("invalid hex quantity")
		}
		return out, nil
	}
	out := new(big.Int)
	if _, ok := out.SetString(v, 10); !ok {
		return nil, errors.New("invalid decimal quantity")
	}
	return out, nil
}

func parseRPCAddressParam(raw json.RawMessage) (string, error) {
	var addr string
	if err := json.Unmarshal(raw, &addr); err != nil {
		return "", errors.New("invalid address")
	}
	addr = strings.TrimSpace(addr)
	if !common.IsHexAddress(addr) {
		return "", errors.New("invalid address")
	}
	return common.HexToAddress(addr).Hex(), nil
}

func parseRPCStringParam(raw json.RawMessage) (string, error) {
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", errors.New("invalid string param")
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("empty string param")
	}
	return v, nil
}

func parseRPCBigIntParam(raw json.RawMessage) (*big.Int, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString == "" {
			return nil, errors.New("invalid quantity")
		}
		v, err := parseRPCBigIntString(asString)
		if err != nil {
			return nil, err
		}
		if v.Sign() < 0 {
			return nil, errors.New("quantity must be non-negative")
		}
		return v, nil
	}

	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		v, err := parseRPCBigIntString(strings.TrimSpace(asNumber.String()))
		if err != nil {
			return nil, err
		}
		if v.Sign() < 0 {
			return nil, errors.New("quantity must be non-negative")
		}
		return v, nil
	}

	return nil, errors.New("invalid quantity")
}

func abiEncodeAddressWord(addr string) (string, error) {
	if !common.IsHexAddress(addr) {
		return "", errors.New("invalid address")
	}
	a := common.HexToAddress(addr).Hex()
	return strings.Repeat("0", 24) + strings.ToLower(stripHexPrefix(a)), nil
}

func abiEncodeUint256Word(v *big.Int) (string, error) {
	if v == nil || v.Sign() < 0 {
		return "", errors.New("invalid uint256 value")
	}
	raw := strings.TrimSpace(v.Text(16))
	if raw == "" {
		raw = "0"
	}
	if len(raw) > 64 {
		return "", errors.New("uint256 overflow")
	}
	return strings.Repeat("0", 64-len(raw)) + strings.ToLower(raw), nil
}

func abiEncodeBytes4Word(hex4 string) (string, error) {
	raw := strings.ToLower(stripHexPrefix(strings.TrimSpace(hex4)))
	if len(raw) != 8 {
		return "", errors.New("invalid bytes4 value")
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", errors.New("invalid bytes4 value")
	}
	return raw + strings.Repeat("0", 56), nil
}

func abiDecodeUint256(hexOut string) (*big.Int, error) {
	bin, err := decodeHexBytes(hexOut)
	if err != nil {
		return nil, err
	}
	if len(bin) < 32 {
		return nil, errors.New("abi uint256 response too short")
	}
	return new(big.Int).SetBytes(bin[len(bin)-32:]), nil
}

func abiDecodeStringDynamic(hexOut string) (string, error) {
	bin, err := decodeHexBytes(hexOut)
	if err != nil {
		return "", err
	}
	if len(bin) < 64 {
		return "", errors.New("abi string response too short")
	}
	offset := new(big.Int).SetBytes(bin[:32]).Uint64()
	if offset > uint64(len(bin)) || offset+32 > uint64(len(bin)) {
		return "", errors.New("abi string offset out of range")
	}
	length := new(big.Int).SetBytes(bin[offset : offset+32]).Uint64()
	start := offset + 32
	end := start + length
	if end > uint64(len(bin)) {
		return "", errors.New("abi string length out of range")
	}
	return string(bin[start:end]), nil
}

func abiDecodeBytes32String(hexOut string) (string, error) {
	bin, err := decodeHexBytes(hexOut)
	if err != nil {
		return "", err
	}
	if len(bin) < 32 {
		return "", errors.New("abi bytes32 response too short")
	}
	trimmed := bytes.TrimRight(bin[:32], "\x00")
	if len(trimmed) == 0 {
		return "", nil
	}
	return string(trimmed), nil
}

func abiDecodeAddress(hexOut string) (string, error) {
	bin, err := decodeHexBytes(hexOut)
	if err != nil {
		return "", err
	}
	if len(bin) < 32 {
		return "", errors.New("abi address response too short")
	}
	return common.BytesToAddress(bin[len(bin)-20:]).Hex(), nil
}

func abiDecodeBool(hexOut string) (bool, error) {
	v, err := abiDecodeUint256(hexOut)
	if err != nil {
		return false, err
	}
	return v.Sign() != 0, nil
}

func abiDecodeTokenString(hexOut string) string {
	if out, err := abiDecodeStringDynamic(hexOut); err == nil {
		return out
	}
	if out, err := abiDecodeBytes32String(hexOut); err == nil {
		return out
	}
	return ""
}

type dtlCompatABIMethodJSON struct {
	Type    string                `json:"type"`
	Name    string                `json:"name"`
	Inputs  []dtlCompatABIArgJSON `json:"inputs"`
	Outputs []dtlCompatABIArgJSON `json:"outputs"`
}

type dtlCompatABIArgJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func canonicalDTLABIType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "u64", "uint64", "u256", "uint256", "int", "int64":
		return "uint256"
	case "address":
		return "address"
	case "bool":
		return "bool"
	case "string":
		return "string"
	default:
		return ""
	}
}

func dtlABIMethodSignature(method DTLLogicPackABIMethod) string {
	name := strings.TrimSpace(method.Name)
	if name == "" {
		return ""
	}
	parts := make([]string, 0, len(method.Args))
	for _, arg := range method.Args {
		typ := canonicalDTLABIType(arg.Type)
		if typ == "" {
			return ""
		}
		parts = append(parts, typ)
	}
	return name + "(" + strings.Join(parts, ",") + ")"
}

func dtlABIMethodSelectorHex(method DTLLogicPackABIMethod) string {
	signature := dtlABIMethodSignature(method)
	if signature == "" {
		return ""
	}
	sum := ethcrypto.Keccak256([]byte(signature))
	return strings.ToLower(hex.EncodeToString(sum[:4]))
}

func parseDTLContractABIMethods(contract *DTLContractState) []DTLLogicPackABIMethod {
	if contract == nil {
		return nil
	}
	if len(contract.ABI) > 0 {
		var compact []DTLLogicPackABIMethod
		if err := json.Unmarshal(contract.ABI, &compact); err == nil && len(compact) > 0 {
			return compact
		}
		var standard []dtlCompatABIMethodJSON
		if err := json.Unmarshal(contract.ABI, &standard); err == nil && len(standard) > 0 {
			methods := make([]DTLLogicPackABIMethod, 0, len(standard))
			for _, m := range standard {
				if strings.TrimSpace(strings.ToLower(m.Type)) != "" && strings.TrimSpace(strings.ToLower(m.Type)) != "function" {
					continue
				}
				name := strings.TrimSpace(m.Name)
				if name == "" {
					continue
				}
				args := make([]DTLLogicPackArg, 0, len(m.Inputs))
				for i, in := range m.Inputs {
					argName := strings.TrimSpace(in.Name)
					if argName == "" {
						argName = fmt.Sprintf("arg%d", i)
					}
					args = append(args, DTLLogicPackArg{Name: argName, Type: strings.TrimSpace(in.Type)})
				}
				returns := make([]string, 0, len(m.Outputs))
				for _, out := range m.Outputs {
					returns = append(returns, strings.TrimSpace(out.Type))
				}
				methods = append(methods, DTLLogicPackABIMethod{
					Name:    name,
					Args:    args,
					Returns: returns,
				})
			}
			if len(methods) > 0 {
				return methods
			}
		}
	}
	if contract.LogicPack != nil && len(contract.LogicPack.ABI) > 0 {
		return append([]DTLLogicPackABIMethod(nil), contract.LogicPack.ABI...)
	}
	if strings.TrimSpace(contract.Bytecode) != "" {
		if program, _, err := DecodeDTLBytecode(contract.Bytecode); err == nil && len(program.ABI) > 0 {
			return append([]DTLLogicPackABIMethod(nil), program.ABI...)
		}
	}
	return nil
}

func decodeDTLEthCallArgs(method DTLLogicPackABIMethod, callData []byte) (map[string]string, error) {
	if len(callData) < 4 {
		return nil, errors.New("calldata too short")
	}
	payload := callData[4:]
	requiredHead := len(method.Args) * 32
	if len(payload) < requiredHead {
		return nil, errors.New("calldata head too short")
	}
	args := make(map[string]string, len(method.Args))
	for i, arg := range method.Args {
		name := strings.ToLower(strings.TrimSpace(arg.Name))
		if name == "" {
			name = fmt.Sprintf("arg%d", i)
		}
		head := payload[i*32 : (i+1)*32]
		switch canonicalDTLABIType(arg.Type) {
		case "uint256":
			n := new(big.Int).SetBytes(head)
			if !n.IsUint64() {
				return nil, fmt.Errorf("arg %s exceeds u64", name)
			}
			args[name] = strconv.FormatUint(n.Uint64(), 10)
		case "address":
			args[name] = common.BytesToAddress(head[12:32]).Hex()
		case "bool":
			args[name] = strconv.FormatBool(new(big.Int).SetBytes(head).Sign() != 0)
		case "string":
			offset := int(new(big.Int).SetBytes(head).Uint64())
			if offset < 0 || offset+32 > len(payload) {
				return nil, fmt.Errorf("arg %s string offset out of range", name)
			}
			length := int(new(big.Int).SetBytes(payload[offset : offset+32]).Uint64())
			start := offset + 32
			end := start + length
			if start < 0 || end < start || end > len(payload) {
				return nil, fmt.Errorf("arg %s string length out of range", name)
			}
			args[name] = string(payload[start:end])
		default:
			return nil, fmt.Errorf("unsupported abi type for arg %s", name)
		}
	}
	return args, nil
}

func encodeDTLEthCallStringReturn(value string) string {
	data := []byte(value)
	offset := make([]byte, 32)
	offset[31] = 32
	lengthWord := make([]byte, 32)
	new(big.Int).SetUint64(uint64(len(data))).FillBytes(lengthWord)
	paddedLen := ((len(data) + 31) / 32) * 32
	padded := make([]byte, paddedLen)
	copy(padded, data)
	out := append(offset, lengthWord...)
	out = append(out, padded...)
	return normalizeHexData(hex.EncodeToString(out))
}

func encodeDTLEthCallResult(method DTLLogicPackABIMethod, result dtlLogicCallResult) (string, error) {
	retType := ""
	if len(method.Returns) > 0 {
		retType = canonicalDTLABIType(method.Returns[0])
	}
	if retType == "" {
		switch result.Kind {
		case "":
			return "0x", nil
		case "u64":
			retType = "uint256"
		case "bool":
			retType = "bool"
		case "string":
			retType = "string"
		default:
			return "0x", nil
		}
	}
	switch retType {
	case "uint256":
		value := new(big.Int)
		switch result.Kind {
		case "", "u64":
			value = new(big.Int).SetUint64(result.U64)
		case "bool":
			if result.Bool {
				value = big.NewInt(1)
			}
		case "string":
			parsed, ok := new(big.Int).SetString(strings.TrimSpace(result.Str), 10)
			if !ok || parsed.Sign() < 0 {
				return "", errors.New("dtl readonly return type mismatch")
			}
			value = parsed
		default:
			return "", errors.New("dtl readonly return type mismatch")
		}
		word, err := abiEncodeUint256Word(value)
		if err != nil {
			return "", err
		}
		return normalizeHexData(word), nil
	case "bool":
		switch result.Kind {
		case "", "bool":
			if result.Bool {
				return normalizeHexData(strings.Repeat("0", 63) + "1"), nil
			}
			return normalizeHexData(strings.Repeat("0", 64)), nil
		case "u64":
			if result.U64 > 0 {
				return normalizeHexData(strings.Repeat("0", 63) + "1"), nil
			}
			return normalizeHexData(strings.Repeat("0", 64)), nil
		default:
			return "", errors.New("dtl readonly return type mismatch")
		}
	case "address":
		rawAddr := ""
		switch result.Kind {
		case "string":
			rawAddr = strings.TrimSpace(result.Str)
		default:
			return "", errors.New("dtl readonly return type mismatch")
		}
		addr := rawAddr
		if !common.IsHexAddress(addr) {
			addr = toEVMHexAddress(addr)
		}
		word, err := abiEncodeAddressWord(addr)
		if err != nil {
			return "", err
		}
		return normalizeHexData(word), nil
	case "string":
		switch result.Kind {
		case "", "string":
			return encodeDTLEthCallStringReturn(result.Str), nil
		default:
			return "", errors.New("dtl readonly return type mismatch")
		}
	default:
		return "0x", nil
	}
}

func dtlPseudoCodeEnvelope(contractID string, contract *DTLContractState) string {
	logicHash := ""
	bytecodeHash := ""
	bytecodeFormat := ""
	bytecodeVersion := uint16(0)
	version := uint16(0)
	standard := ""
	if contract != nil {
		logicHash = strings.TrimSpace(contract.LogicHash)
		bytecodeHash = strings.TrimSpace(contract.BytecodeHash)
		bytecodeFormat = normalizeDTLBytecodeFormat(contract.BytecodeFormat)
		bytecodeVersion = contract.BytecodeVersion
		version = contract.Version
		standard = normalizeDTLContractStandard(contract.Standard)
	}
	payload := fmt.Sprintf(
		"dtl-v2:%s:%s:%s:%d:%s:%s:%d",
		normalizeDTLContractID(contractID),
		logicHash,
		standard,
		version,
		bytecodeHash,
		bytecodeFormat,
		bytecodeVersion,
	)
	hash := ethcrypto.Keccak256([]byte(payload))
	return normalizeHexData(hex.EncodeToString(hash))
}

func dtlStorageSlotHash(key string) string {
	hash := ethcrypto.Keccak256([]byte(strings.TrimSpace(key)))
	return common.BytesToHash(hash).Hex()
}

func dtlStorageValueToWord(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return zeroEVMWordHex
	}
	if n, ok := new(big.Int).SetString(trimmed, 10); ok && n.Sign() >= 0 {
		word, err := abiEncodeUint256Word(n)
		if err == nil {
			return normalizeHexData(word)
		}
	}
	hash := ethcrypto.Keccak256([]byte(trimmed))
	return normalizeHexData(hex.EncodeToString(hash))
}

func dtlStorageBySlot(contract *DTLContractState, slot string) string {
	if contract == nil {
		return zeroEVMWordHex
	}
	target := strings.ToLower(common.HexToHash(normalizeHexData(slot)).Hex())
	for key, value := range contract.Storage {
		if strings.ToLower(dtlStorageSlotHash(key)) == target {
			return dtlStorageValueToWord(value)
		}
	}
	slotTrimmed := strings.TrimSpace(slot)
	if strings.HasPrefix(strings.ToLower(slotTrimmed), "0x") {
		if idx, ok := new(big.Int).SetString(strings.TrimPrefix(strings.ToLower(slotTrimmed), "0x"), 16); ok && idx.IsUint64() {
			keys := make([]string, 0, len(contract.Storage))
			for key := range contract.Storage {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			pos := idx.Uint64()
			if pos < uint64(len(keys)) {
				return dtlStorageValueToWord(contract.Storage[keys[pos]])
			}
		}
	}
	return zeroEVMWordHex
}

func resolveDTLContractByAddressFromLedger(ledger Ledger, refOrAddress string) (string, *DTLContractState, error) {
	if id, contract, err := resolveDTLContractFromLedger(ledger, refOrAddress); err == nil {
		return id, contract, nil
	}
	ref := strings.TrimSpace(refOrAddress)
	if !common.IsHexAddress(ref) {
		return "", nil, errors.New("dtl contract not found")
	}
	target := strings.ToLower(common.HexToAddress(ref).Hex())
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	for contractID, contract := range ledger.DTL.Contracts {
		if contract == nil {
			continue
		}
		if strings.ToLower(toEVMHexAddress(contractID)) == target {
			return normalizeDTLContractID(contractID), contract, nil
		}
	}
	return "", nil, errors.New("dtl contract not found")
}

func normalizeDTLEventTopicHash(topic string) common.Hash {
	raw := strings.TrimSpace(topic)
	if raw == "" {
		return common.Hash{}
	}
	if strings.HasPrefix(strings.ToLower(raw), "0x") {
		return common.HexToHash(normalizeHexHash(raw))
	}
	return common.BytesToHash(ethcrypto.Keccak256([]byte(raw)))
}

func normalizeDTLEventDataBytes(data string) []byte {
	raw := strings.TrimSpace(data)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(raw), "0x") {
		if decoded, err := decodeHexBytes(raw); err == nil {
			return decoded
		}
	}
	return []byte(raw)
}

func buildTypedDTLLogs(loc rpcTxLocation, receipt *StateReceipt, startIndex int) []*ethtypes.Log {
	if receipt == nil || len(receipt.Logs) == 0 {
		return nil
	}
	out := make([]*ethtypes.Log, 0, len(receipt.Logs))
	txHash := common.HexToHash(rpcTxHash(loc.Tx))
	blockHash := common.HexToHash(normalizeHexHash(loc.Block.BlockHash))
	for i, entry := range receipt.Logs {
		contractID := normalizeDTLContractID(entry.ContractID)
		if contractID == "" {
			contractID = normalizeDTLContractID(receipt.ContractID)
		}
		topics := make([]common.Hash, 0, len(entry.Topics))
		for _, topic := range entry.Topics {
			topics = append(topics, normalizeDTLEventTopicHash(topic))
		}
		out = append(out, &ethtypes.Log{
			Address:     common.HexToAddress(toEVMHexAddress(contractID)),
			Topics:      topics,
			Data:        normalizeDTLEventDataBytes(entry.Data),
			BlockNumber: loc.Block.ID,
			TxHash:      txHash,
			TxIndex:     uint(loc.TxIndex),
			BlockHash:   blockHash,
			Index:       uint(startIndex + i),
			Removed:     false,
		})
	}
	return out
}

func blockReceiptAtTxIndex(block Block, txIndex int) (*StateReceipt, bool) {
	if txIndex < 0 || txIndex >= len(block.Receipts) {
		return nil, false
	}
	receipt := block.Receipts[txIndex]
	return &receipt, true
}

func estimateDTLCallGas(contract *DTLContractState, methodName string, args map[string]string) uint64 {
	base := uint64(21000)
	if contract == nil || contract.LogicPack == nil {
		return base
	}
	normalizedName := normalizeDTLContractMethodName(methodName)
	for _, method := range contract.LogicPack.Methods {
		if normalizeDTLContractMethodName(method.Name) != normalizedName {
			continue
		}
		steps := uint64(method.MaxSteps)
		if steps == 0 {
			steps = uint64(len(method.Ops))
		}
		estimate := base + steps*200 + uint64(len(args))*128
		if estimate < base {
			return base
		}
		return estimate
	}
	return base
}

func (s *Server) msc21Call(tokenAddr string, callData string, stateTag json.RawMessage) (string, error) {
	stateLedger, stateHeight, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return "", errors.New("invalid block tag")
	}
	if !ok {
		return "", errors.New("header not found")
	}
	code := evmCodeByAddress(stateLedger, tokenAddr)
	if code == "" || code == "0x" {
		return "", errors.New("token contract code not found")
	}

	tx := Transaction{
		Type:        TxEVM,
		From:        "MSC_EVM_CALLER",
		To:          strings.TrimSpace(tokenAddr),
		EVMCode:     code,
		EVMInput:    normalizeHexData(callData),
		EVMGasLimit: currentEVMDefaultGasLimit(),
	}
	height := int(stateHeight)
	execLedger := &stateLedger
	result, err := runCustomEVMSandbox(tx, height, execLedger)
	if err != nil {
		return "", err
	}
	return normalizeHexData(result.OutputHex), nil
}

func (s *Server) msc21CallUint256(tokenAddr, selector string, args []string, stateTag json.RawMessage) (*big.Int, error) {
	data := strings.ToLower(stripHexPrefix(selector))
	for _, arg := range args {
		if len(arg) != 64 {
			return nil, errors.New("invalid abi argument width")
		}
		data += strings.ToLower(arg)
	}
	out, err := s.msc21Call(tokenAddr, "0x"+data, stateTag)
	if err != nil {
		return nil, err
	}
	return abiDecodeUint256(out)
}

func (s *Server) msc21TokenInfo(tokenAddr string, stateTag json.RawMessage) (map[string]any, error) {
	total, err := s.msc21CallUint256(tokenAddr, "0x18160ddd", nil, stateTag) // totalSupply()
	if err != nil {
		return nil, err
	}
	dec, err := s.msc21CallUint256(tokenAddr, "0x313ce567", nil, stateTag) // decimals()
	if err != nil {
		return nil, err
	}

	nameOut, _ := s.msc21Call(tokenAddr, "0x06fdde03", stateTag) // name()
	symOut, _ := s.msc21Call(tokenAddr, "0x95d89b41", stateTag)  // symbol()
	name := strings.TrimSpace(abiDecodeTokenString(nameOut))
	symbol := strings.TrimSpace(abiDecodeTokenString(symOut))

	dec64 := dec.Uint64()
	return map[string]any{
		"standard":      msc21Standard,
		"erc20Like":     true,
		"address":       common.HexToAddress(tokenAddr).Hex(),
		"name":          name,
		"symbol":        symbol,
		"decimals":      encodeRPCQuantityUint64(dec64),
		"decimalsInt":   dec64,
		"totalSupply":   encodeRPCQuantityBig(total),
		"totalSupplyBN": total.String(),
	}, nil
}

func (s *Server) msc21BalanceOf(tokenAddr, ownerAddr string, stateTag json.RawMessage) (*big.Int, error) {
	ownerWord, err := abiEncodeAddressWord(ownerAddr)
	if err != nil {
		return nil, err
	}
	return s.msc21CallUint256(tokenAddr, "0x70a08231", []string{ownerWord}, stateTag) // balanceOf(address)
}

func (s *Server) msc21Allowance(tokenAddr, ownerAddr, spenderAddr string, stateTag json.RawMessage) (*big.Int, error) {
	ownerWord, err := abiEncodeAddressWord(ownerAddr)
	if err != nil {
		return nil, err
	}
	spenderWord, err := abiEncodeAddressWord(spenderAddr)
	if err != nil {
		return nil, err
	}
	return s.msc21CallUint256(tokenAddr, "0xdd62ed3e", []string{ownerWord, spenderWord}, stateTag) // allowance(address,address)
}

func (s *Server) mscSupportsInterface(tokenAddr, interfaceID string, stateTag json.RawMessage) (bool, error) {
	ifaceWord, err := abiEncodeBytes4Word(interfaceID)
	if err != nil {
		return false, err
	}
	out, err := s.msc21Call(tokenAddr, "0x01ffc9a7"+ifaceWord, stateTag) // supportsInterface(bytes4)
	if err != nil {
		return false, err
	}
	return abiDecodeBool(out)
}

func (s *Server) msc721IsToken(tokenAddr string, stateTag json.RawMessage) (bool, error) {
	return s.mscSupportsInterface(tokenAddr, "0x80ac58cd", stateTag) // ERC-721 interface id
}

func (s *Server) msc721TokenInfo(tokenAddr string, stateTag json.RawMessage) (map[string]any, error) {
	is721, err := s.msc721IsToken(tokenAddr, stateTag)
	if err != nil {
		return nil, err
	}
	if !is721 {
		return nil, errors.New("not MSC-721 contract")
	}

	supportsMetadata, _ := s.mscSupportsInterface(tokenAddr, "0x5b5e139f", stateTag) // ERC-721 metadata
	supportsEnumerable, _ := s.mscSupportsInterface(tokenAddr, "0x780e9d63", stateTag)

	nameOut, _ := s.msc21Call(tokenAddr, "0x06fdde03", stateTag) // name()
	symOut, _ := s.msc21Call(tokenAddr, "0x95d89b41", stateTag)  // symbol()
	name := strings.TrimSpace(abiDecodeTokenString(nameOut))
	symbol := strings.TrimSpace(abiDecodeTokenString(symOut))

	info := map[string]any{
		"standard":           msc721Standard,
		"erc721Like":         true,
		"address":            common.HexToAddress(tokenAddr).Hex(),
		"name":               name,
		"symbol":             symbol,
		"supportsMetadata":   supportsMetadata,
		"supportsEnumerable": supportsEnumerable,
		"totalSupply":        nil,
		"totalSupplyBN":      "",
	}
	if total, err := s.msc21CallUint256(tokenAddr, "0x18160ddd", nil, stateTag); err == nil {
		info["totalSupply"] = encodeRPCQuantityBig(total)
		info["totalSupplyBN"] = total.String()
	}
	return info, nil
}

func (s *Server) msc721BalanceOf(tokenAddr, ownerAddr string, stateTag json.RawMessage) (*big.Int, error) {
	ownerWord, err := abiEncodeAddressWord(ownerAddr)
	if err != nil {
		return nil, err
	}
	return s.msc21CallUint256(tokenAddr, "0x70a08231", []string{ownerWord}, stateTag) // balanceOf(address)
}

func (s *Server) msc721OwnerOf(tokenAddr string, tokenID *big.Int, stateTag json.RawMessage) (string, error) {
	tokenWord, err := abiEncodeUint256Word(tokenID)
	if err != nil {
		return "", err
	}
	out, err := s.msc21Call(tokenAddr, "0x6352211e"+tokenWord, stateTag) // ownerOf(uint256)
	if err != nil {
		return "", err
	}
	return abiDecodeAddress(out)
}

func (s *Server) msc721TokenURI(tokenAddr string, tokenID *big.Int, stateTag json.RawMessage) (string, error) {
	tokenWord, err := abiEncodeUint256Word(tokenID)
	if err != nil {
		return "", err
	}
	out, err := s.msc21Call(tokenAddr, "0xc87b56dd"+tokenWord, stateTag) // tokenURI(uint256)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(abiDecodeTokenString(out)), nil
}

func (s *Server) msc1155IsToken(tokenAddr string, stateTag json.RawMessage) (bool, error) {
	return s.mscSupportsInterface(tokenAddr, "0xd9b67a26", stateTag) // ERC-1155 interface id
}

func (s *Server) msc1155TokenInfo(tokenAddr string, stateTag json.RawMessage) (map[string]any, error) {
	is1155, err := s.msc1155IsToken(tokenAddr, stateTag)
	if err != nil {
		return nil, err
	}
	if !is1155 {
		return nil, errors.New("not MSC-1155 contract")
	}
	supportsMetadata, _ := s.mscSupportsInterface(tokenAddr, "0x0e89341c", stateTag) // ERC-1155 metadata URI
	return map[string]any{
		"standard":         msc1155Standard,
		"erc1155Like":      true,
		"address":          common.HexToAddress(tokenAddr).Hex(),
		"supportsMetadata": supportsMetadata,
	}, nil
}

func (s *Server) msc1155BalanceOf(tokenAddr, ownerAddr string, tokenID *big.Int, stateTag json.RawMessage) (*big.Int, error) {
	ownerWord, err := abiEncodeAddressWord(ownerAddr)
	if err != nil {
		return nil, err
	}
	tokenWord, err := abiEncodeUint256Word(tokenID)
	if err != nil {
		return nil, err
	}
	return s.msc21CallUint256(tokenAddr, "0x00fdd58e", []string{ownerWord, tokenWord}, stateTag) // balanceOf(address,uint256)
}

func (s *Server) msc1155URI(tokenAddr string, tokenID *big.Int, stateTag json.RawMessage) (string, error) {
	tokenWord, err := abiEncodeUint256Word(tokenID)
	if err != nil {
		return "", err
	}
	out, err := s.msc21Call(tokenAddr, "0x0e89341c"+tokenWord, stateTag) // uri(uint256)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(abiDecodeTokenString(out)), nil
}

func (s *Server) msc1155IsApprovedForAll(tokenAddr, ownerAddr, operatorAddr string, stateTag json.RawMessage) (bool, error) {
	ownerWord, err := abiEncodeAddressWord(ownerAddr)
	if err != nil {
		return false, err
	}
	operatorWord, err := abiEncodeAddressWord(operatorAddr)
	if err != nil {
		return false, err
	}
	out, err := s.msc21Call(tokenAddr, "0xe985e9c5"+ownerWord+operatorWord, stateTag) // isApprovedForAll(address,address)
	if err != nil {
		return false, err
	}
	return abiDecodeBool(out)
}

type rpcAccessTuple struct {
	Address     string   `json:"address"`
	StorageKeys []string `json:"storageKeys"`
}

func parseEthAccessList(raw json.RawMessage) (ethtypes.AccessList, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var tuples []rpcAccessTuple
	if err := json.Unmarshal(trimmed, &tuples); err != nil {
		return nil, errors.New("invalid accessList")
	}
	out := make(ethtypes.AccessList, 0, len(tuples))
	for _, tuple := range tuples {
		if !common.IsHexAddress(tuple.Address) {
			return nil, errors.New("invalid accessList address")
		}
		item := ethtypes.AccessTuple{
			Address:     common.HexToAddress(tuple.Address),
			StorageKeys: make([]common.Hash, 0, len(tuple.StorageKeys)),
		}
		for _, key := range tuple.StorageKeys {
			item.StorageKeys = append(item.StorageKeys, common.HexToHash(normalizeEVMStorageSlotKey(key)))
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeEthSendTxType(raw string, hasAccessList bool, has1559 bool) (string, error) {
	txType := strings.ToLower(strings.TrimSpace(raw))
	switch txType {
	case "", "0x", "0x0", "0", "legacy":
		if txType == "" && has1559 {
			return "0x2", nil
		}
		if txType == "" && hasAccessList {
			return "0x1", nil
		}
		return "0x0", nil
	case "0x1", "1", "accesslist":
		return "0x1", nil
	case "0x2", "2", "dynamicfee":
		return "0x2", nil
	default:
		return "", errors.New("invalid transaction type")
	}
}

func loadDevEVMPrivateKey() (*ecdsa.PrivateKey, common.Address, bool, error) {
	raw := strings.TrimSpace(os.Getenv("MSC_EVM_DEV_PRIVKEY"))
	if raw == "" {
		return nil, common.Address{}, false, nil
	}
	key, err := ethcrypto.HexToECDSA(strings.TrimPrefix(raw, "0x"))
	if err != nil {
		return nil, common.Address{}, false, err
	}
	return key, ethcrypto.PubkeyToAddress(key.PublicKey), true, nil
}

func devSignerForAddress(addr string) (*ecdsa.PrivateKey, common.Address, error) {
	if !common.IsHexAddress(addr) {
		return nil, common.Address{}, errors.New("invalid address")
	}
	key, devAddr, ok, err := loadDevEVMPrivateKey()
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("invalid MSC_EVM_DEV_PRIVKEY: %w", err)
	}
	if !ok {
		return nil, common.Address{}, errors.New("requires MSC_EVM_DEV_PRIVKEY")
	}
	reqAddr := common.HexToAddress(addr)
	if !strings.EqualFold(reqAddr.Hex(), devAddr.Hex()) {
		return nil, common.Address{}, errors.New("address is not unlocked by node")
	}
	return key, devAddr, nil
}

func signHashWithDevSigner(addr string, hash []byte) (string, error) {
	key, _, err := devSignerForAddress(addr)
	if err != nil {
		return "", err
	}
	sig, err := ethcrypto.Sign(hash, key)
	if err != nil {
		return "", err
	}
	if len(sig) == 65 {
		sig[64] += 27
	}
	return "0x" + hex.EncodeToString(sig), nil
}

func (s *Server) rpcBlockSnapshot() []Block {
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return nil
	}
	s.Node.Blockchain.mu.RLock()
	defer s.Node.Blockchain.mu.RUnlock()
	return append([]Block{}, s.Node.Blockchain.Blocks...)
}

func findBlockByHeight(blocks []Block, height uint64) (Block, bool) {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].ID == height {
			return blocks[i], true
		}
	}
	return Block{}, false
}

func findBlockByHash(blocks []Block, hash string) (Block, bool) {
	target := strings.ToLower(stripHexPrefix(hash))
	if target == "" {
		return Block{}, false
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		got := strings.ToLower(stripHexPrefix(blocks[i].BlockHash))
		if got == target {
			return blocks[i], true
		}
	}
	return Block{}, false
}

func (s *Server) resolveBlockByTag(raw json.RawMessage) (Block, bool, error) {
	blocks := s.rpcBlockSnapshot()
	if len(blocks) == 0 {
		return Block{}, false, nil
	}
	latest := blocks[len(blocks)-1]

	if len(bytes.TrimSpace(raw)) == 0 {
		return latest, true, nil
	}

	var tag string
	if err := json.Unmarshal(raw, &tag); err == nil {
		tag = strings.TrimSpace(tag)
		switch strings.ToLower(tag) {
		case "", "latest", "pending", "safe":
			return latest, true, nil
		case "earliest":
			return blocks[0], true, nil
		case "finalized":
			fh := s.explorerAvailableFinalizedHeight()
			if fh == 0 {
				return latest, true, nil
			}
			if b, ok := findBlockByHeight(blocks, fh); ok {
				return b, true, nil
			}
			return latest, true, nil
		default:
			v, err := parseRPCQuantity(raw)
			if err != nil {
				return Block{}, false, err
			}
			b, ok := findBlockByHeight(blocks, v)
			return b, ok, nil
		}
	}

	v, err := parseRPCQuantity(raw)
	if err != nil {
		return Block{}, false, err
	}
	b, ok := findBlockByHeight(blocks, v)
	return b, ok, nil
}

func isRPCPendingTag(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	var tag string
	if err := json.Unmarshal(trimmed, &tag); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(tag), "pending")
}

func (s *Server) resolveInternalAddress(rpcAddr string) string {
	if s == nil || s.Node == nil {
		return canonicalAddressKey(rpcAddr)
	}
	rpcAddr = strings.TrimSpace(rpcAddr)
	if rpcAddr == "" {
		return ""
	}
	if !common.IsHexAddress(rpcAddr) {
		return canonicalAddressKey(rpcAddr)
	}
	if mapped := lookupLedgerAddressAlias(&s.Node.Ledger, rpcAddr); mapped != "" {
		return mapped
	}

	target := strings.ToLower(common.HexToAddress(rpcAddr).Hex())
	seen := make(map[string]struct{})
	for key := range s.Node.Ledger.Balances {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		addr := canonicalAddressKey(parts[1])
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		if strings.ToLower(toEVMHexAddress(addr)) == target {
			return addr
		}
	}
	for addr := range s.Node.Ledger.Nonces {
		ca := canonicalAddressKey(addr)
		if _, ok := seen[ca]; ok {
			continue
		}
		seen[ca] = struct{}{}
		if strings.ToLower(toEVMHexAddress(ca)) == target {
			return ca
		}
	}
	return canonicalAddressKey(rpcAddr)
}

func (s *Server) registerRPCAddressAlias(addr string) (string, string, error) {
	if s == nil || s.Node == nil {
		return "", "", errors.New("node unavailable")
	}
	internal := canonicalAddressKey(addr)
	alias := registerLedgerAddressAlias(&s.Node.Ledger, addr)
	if alias == "" {
		return "", "", errors.New("invalid MSC wallet address")
	}
	// Best-effort persistence so mapping survives restart without waiting for next block commit.
	_ = SaveNodeLedger(s.Node.ID, s.Node.DataDir, s.Node.Ledger)
	return internal, alias, nil
}

func txGasLimit(tx Transaction) uint64 {
	if tx.Type == TxEVM {
		return normalizeEVMGasLimit(tx.EVMGasLimit)
	}
	if tx.GasLimit > 0 {
		return tx.GasLimit
	}
	return 21000
}

func rpcTxHash(tx Transaction) string {
	if strings.TrimSpace(tx.EVMTxHash) != "" {
		return normalizeHexHash(tx.EVMTxHash)
	}
	return normalizeHexHash(tx.ID)
}

func rpcTxNonce(tx Transaction) int {
	if isEVMRawTransaction(tx) && tx.Nonce > 0 {
		return tx.Nonce - 1
	}
	return tx.Nonce
}

func (s *Server) rpcNextNonceForAddress(internalAddr string, includePending bool) int {
	if s == nil || s.Node == nil {
		return 0
	}
	targetInternal := canonicalAddressKey(internalAddr)
	if targetInternal == "" {
		return 0
	}

	nextNonce := getNonce(s.Node.Ledger, targetInternal)
	if !includePending {
		return nextNonce
	}

	targetEVM := strings.ToLower(toEVMHexAddress(targetInternal))
	s.Node.Mempool.mu.Lock()
	defer s.Node.Mempool.mu.Unlock()

	for _, tx := range s.Node.Mempool.Transactions {
		fromInternal := canonicalAddressKey(tx.From)
		if fromInternal == "" {
			continue
		}

		sameSender := strings.EqualFold(fromInternal, targetInternal)
		if !sameSender {
			fromEVM := strings.ToLower(toEVMHexAddress(fromInternal))
			sameSender = fromEVM == targetEVM
		}
		if !sameSender {
			continue
		}
		if tx.Nonce > nextNonce {
			nextNonce = tx.Nonce
		}
	}

	return nextNonce
}

func (s *Server) findTxByHash(hash string) rpcTxLocation {
	var out rpcTxLocation
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return out
	}
	target := strings.ToLower(stripHexPrefix(hash))
	if target == "" {
		return out
	}

	blocks := s.rpcBlockSnapshot()
	if len(blocks) > 0 {
		out.Latest = blocks[len(blocks)-1].ID
	}
	out.Finalized = s.explorerAvailableFinalizedHeight()

	s.Node.Mempool.mu.Lock()
	for idx, tx := range s.Node.Mempool.Transactions {
		if strings.ToLower(stripHexPrefix(tx.ID)) == target || strings.ToLower(stripHexPrefix(tx.EVMTxHash)) == target {
			out.Tx = tx
			out.Found = true
			out.Pending = true
			out.TxIndex = idx
			s.Node.Mempool.mu.Unlock()
			return out
		}
	}
	s.Node.Mempool.mu.Unlock()

	if rec, ok := s.Node.loadTxRecord(hash); ok {
		if b, found := s.Node.LoadBlock(int(rec.Height)); found {
			if rec.BlockHash != "" {
				blockHash := strings.TrimSpace(b.BlockHash)
				if blockHash != "" && !strings.EqualFold(strings.TrimSpace(rec.BlockHash), blockHash) {
					// Stale index entry; fall back to full scan.
				} else {
					out.Tx = rec.Tx
					out.Block = b
					out.Found = true
					out.BlockFound = true
					out.TxIndex = rec.Index
					return out
				}
			} else {
				out.Tx = rec.Tx
				out.Block = b
				out.Found = true
				out.BlockFound = true
				out.TxIndex = rec.Index
				return out
			}
		}
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		for txIndex, tx := range block.Transactions {
			if strings.ToLower(stripHexPrefix(tx.ID)) != target && strings.ToLower(stripHexPrefix(tx.EVMTxHash)) != target {
				continue
			}
			out.Tx = tx
			out.Block = block
			out.Found = true
			out.BlockFound = true
			out.TxIndex = txIndex
			return out
		}
	}
	return out
}

func normalizeRPCTopicHash(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return strings.ToLower(normalizeEVMStorageValue(v))
}

func parseRPCLogFilterAddresses(raw any) (map[string]struct{}, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case string:
		addr := strings.ToLower(toEVMHexAddress(v))
		return map[string]struct{}{addr: {}}, nil
	case []any:
		out := make(map[string]struct{}, len(v))
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, errors.New("invalid address filter")
			}
			out[strings.ToLower(toEVMHexAddress(str))] = struct{}{}
		}
		return out, nil
	default:
		return nil, errors.New("invalid address filter")
	}
}

func parseRPCLogFilterTopics(raw any) ([]map[string]struct{}, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("invalid topics filter")
	}
	out := make([]map[string]struct{}, 0, len(items))
	for _, item := range items {
		if item == nil {
			out = append(out, nil)
			continue
		}
		switch v := item.(type) {
		case string:
			topic := normalizeRPCTopicHash(v)
			if topic == "" {
				return nil, errors.New("invalid topic value")
			}
			out = append(out, map[string]struct{}{topic: {}})
		case []any:
			rule := make(map[string]struct{}, len(v))
			anyTopic := false
			for _, sub := range v {
				if sub == nil {
					anyTopic = true
					break
				}
				subStr, ok := sub.(string)
				if !ok {
					return nil, errors.New("invalid topic option")
				}
				topic := normalizeRPCTopicHash(subStr)
				if topic == "" {
					return nil, errors.New("invalid topic option")
				}
				rule[topic] = struct{}{}
			}
			if anyTopic {
				out = append(out, nil)
			} else {
				out = append(out, rule)
			}
		default:
			return nil, errors.New("invalid topic filter")
		}
	}
	return out, nil
}

func parseRPCLogFilter(raw json.RawMessage) (rpcLogFilter, error) {
	var out rpcLogFilter
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return out, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return out, err
	}

	if v, ok := obj["blockHash"]; ok && v != nil {
		str, ok := v.(string)
		if !ok {
			return out, errors.New("invalid blockHash")
		}
		out.BlockHash = normalizeHexHash(str)
	}
	if v, ok := obj["fromBlock"]; ok && v != nil {
		str, ok := v.(string)
		if !ok {
			return out, errors.New("invalid fromBlock")
		}
		out.FromBlock = strings.TrimSpace(str)
	}
	if v, ok := obj["toBlock"]; ok && v != nil {
		str, ok := v.(string)
		if !ok {
			return out, errors.New("invalid toBlock")
		}
		out.ToBlock = strings.TrimSpace(str)
	}
	if out.BlockHash != "" && (out.FromBlock != "" || out.ToBlock != "") {
		return out, errors.New("blockHash is mutually exclusive with fromBlock/toBlock")
	}

	var err error
	if v, ok := obj["address"]; ok {
		out.Addresses, err = parseRPCLogFilterAddresses(v)
		if err != nil {
			return out, err
		}
	}
	if v, ok := obj["topics"]; ok {
		out.Topics, err = parseRPCLogFilterTopics(v)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func rpcLogTopics(logEntry map[string]any) []string {
	topicsRaw, ok := logEntry["topics"]
	if !ok || topicsRaw == nil {
		return nil
	}
	switch v := topicsRaw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, str)
		}
		return out
	default:
		return nil
	}
}

func rpcLogMatchesFilter(logEntry map[string]any, filter rpcLogFilter) bool {
	if len(filter.Addresses) > 0 {
		addr, _ := logEntry["address"].(string)
		if _, ok := filter.Addresses[strings.ToLower(addr)]; !ok {
			return false
		}
	}
	if len(filter.Topics) == 0 {
		return true
	}
	logTopics := rpcLogTopics(logEntry)
	for i, rule := range filter.Topics {
		if rule == nil {
			continue
		}
		if i >= len(logTopics) {
			return false
		}
		got := normalizeRPCTopicHash(logTopics[i])
		if _, ok := rule[got]; !ok {
			return false
		}
	}
	return true
}

func nextRPCFilterID() string {
	rpcFilterMu.Lock()
	defer rpcFilterMu.Unlock()
	rpcFilterSeq++
	id := encodeRPCQuantityUint64(rpcFilterSeq)
	return strings.ToLower(id)
}

func setRPCFilter(f *rpcCompatFilter) string {
	if f == nil {
		return ""
	}
	f.ID = strings.ToLower(strings.TrimSpace(f.ID))
	if f.ID == "" {
		f.ID = nextRPCFilterID()
	}
	rpcFilterMu.Lock()
	rpcFilters[f.ID] = f
	rpcFilterMu.Unlock()
	return f.ID
}

func parseRPCFilterID(param json.RawMessage) (string, error) {
	var id string
	if err := json.Unmarshal(param, &id); err != nil {
		return "", errors.New("invalid filter id")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("invalid filter id")
	}
	raw := json.RawMessage(strconv.Quote(id))
	q, err := parseRPCQuantity(raw)
	if err != nil {
		return "", errors.New("invalid filter id")
	}
	return strings.ToLower(encodeRPCQuantityUint64(q)), nil
}

func getRPCFilter(id string) (*rpcCompatFilter, bool) {
	rpcFilterMu.Lock()
	defer rpcFilterMu.Unlock()
	f, ok := rpcFilters[strings.ToLower(strings.TrimSpace(id))]
	return f, ok
}

func deleteRPCFilter(id string) bool {
	rpcFilterMu.Lock()
	defer rpcFilterMu.Unlock()
	id = strings.ToLower(strings.TrimSpace(id))
	if _, ok := rpcFilters[id]; !ok {
		return false
	}
	delete(rpcFilters, id)
	return true
}

func rpcLogEntryIdentity(logEntry map[string]any) string {
	bh, _ := logEntry["blockHash"].(string)
	th, _ := logEntry["transactionHash"].(string)
	li, _ := logEntry["logIndex"].(string)
	return strings.ToLower(normalizeHexHash(bh)) + "|" + strings.ToLower(normalizeHexHash(th)) + "|" + strings.ToLower(li)
}

func (s *Server) currentPendingTxHashes() []string {
	if s == nil || s.Node == nil {
		return nil
	}
	s.Node.Mempool.mu.Lock()
	defer s.Node.Mempool.mu.Unlock()
	out := make([]string, 0, len(s.Node.Mempool.Transactions))
	for _, tx := range s.Node.Mempool.Transactions {
		out = append(out, rpcTxHash(tx))
	}
	return out
}

func (s *Server) currentPendingTxLocations() []rpcTxLocation {
	if s == nil || s.Node == nil {
		return nil
	}
	s.Node.Mempool.mu.Lock()
	defer s.Node.Mempool.mu.Unlock()

	out := make([]rpcTxLocation, 0, len(s.Node.Mempool.Transactions))
	for idx, tx := range s.Node.Mempool.Transactions {
		out = append(out, rpcTxLocation{
			Tx:      tx,
			Found:   true,
			Pending: true,
			TxIndex: idx,
		})
	}
	return out
}

func (s *Server) currentEVMFeeDemandTxCount() int {
	if s == nil || s.Node == nil {
		return 0
	}

	demand := 0
	s.Node.Mempool.mu.Lock()
	demand += len(s.Node.Mempool.Transactions)
	s.Node.Mempool.mu.Unlock()

	if s.Node.Blockchain != nil {
		last := s.Node.Blockchain.LastBlock()
		demand += len(last.Transactions)
	}

	return demand
}

func (s *Server) currentEVMMarketFee(gasLimit uint64) int {
	if s == nil || s.Node == nil {
		return ComputeEVMFeeWithDemand(gasLimit, 0)
	}
	return ComputeEVMFeeWithDemandAndLedger(&s.Node.Ledger, gasLimit, s.currentEVMFeeDemandTxCount())
}

func (s *Server) currentEVMBaseFee() int {
	if s == nil || s.Node == nil {
		return evmBaseFeeFromLedger(nil)
	}
	return evmBaseFeeFromLedger(&s.Node.Ledger)
}

func (s *Server) evmBaseFeeForBlock(height uint64) int {
	if height == 0 {
		genesis := GenesisLedger()
		return evmBaseFeeFromLedger(&genesis)
	}
	ledger, ok := s.ledgerAtHeight(height - 1)
	if !ok {
		return s.currentEVMBaseFee()
	}
	return evmBaseFeeFromLedger(&ledger)
}

func (s *Server) evmNextBaseFeeAfterBlock(height uint64) int {
	ledger, ok := s.ledgerAtHeight(height)
	if !ok {
		return s.currentEVMBaseFee()
	}
	return evmBaseFeeFromLedger(&ledger)
}

func (s *Server) currentBlockHashes() []string {
	blocks := s.rpcBlockSnapshot()
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, normalizeHexHash(b.BlockHash))
	}
	return out
}

func (s *Server) buildPendingEthBlockObject(fullTx bool) map[string]any {
	blocks := s.rpcBlockSnapshot()
	pendingLocs := s.currentPendingTxLocations()

	parentHash := "0x" + strings.Repeat("0", 64)
	nextNumber := uint64(0)
	if len(blocks) > 0 {
		latest := blocks[len(blocks)-1]
		parentHash = normalizeHexHash(latest.BlockHash)
		nextNumber = latest.ID + 1
	}

	txs := make([]any, 0, len(pendingLocs))
	var gasUsed uint64
	for _, loc := range pendingLocs {
		gasUsed += s.gasUsedForTxLocation(loc)
		if fullTx {
			txs = append(txs, s.buildEthTxObject(loc))
		} else {
			txs = append(txs, rpcTxHash(loc.Tx))
		}
	}

	return map[string]any{
		"number":           encodeRPCQuantityUint64(nextNumber),
		"hash":             nil,
		"parentHash":       parentHash,
		"nonce":            nil,
		"sha3Uncles":       emptyUncleHash,
		"logsBloom":        emptyLogsBloom,
		"transactionsRoot": emptyStateRoot,
		"stateRoot":        emptyStateRoot,
		"receiptsRoot":     emptyStateRoot,
		"miner":            toEVMHexAddress(TREASURY_ADDRESS),
		"difficulty":       "0x0",
		"totalDifficulty":  encodeRPCQuantityBig(new(big.Int).SetUint64(nextNumber)),
		"extraData":        "0x",
		"size":             encodeRPCQuantityInt(0),
		"gasLimit":         encodeRPCQuantityUint64(currentEVMMaxGasLimit()),
		"gasUsed":          encodeRPCQuantityUint64(gasUsed),
		"timestamp":        encodeRPCQuantityUint64(uint64(time.Now().Unix())),
		"transactions":     txs,
		"uncles":           []any{},
		"baseFeePerGas":    encodeRPCQuantityInt(s.currentEVMBaseFee()),
		"mixHash":          nil,
	}
}

func (s *Server) rpcSyncingResult() any {
	if s == nil || s.Node == nil {
		return false
	}
	runtime := s.Node.runtimeStatusSnapshot()
	if !runtime.Syncing {
		return false
	}
	return map[string]any{
		"startingBlock": encodeRPCQuantityUint64(runtime.FinalizedHeight),
		"currentBlock":  encodeRPCQuantityUint64(runtime.Height),
		"highestBlock":  encodeRPCQuantityUint64(runtime.SyncTarget),
	}
}

func rpcSyncingStateSignature(state any) string {
	enc, err := json.Marshal(state)
	if err != nil {
		return fmt.Sprintf("%v", state)
	}
	return string(enc)
}

func (s *Server) resolveRPCLogBlocks(filter rpcLogFilter) ([]Block, error) {
	blocks := s.rpcBlockSnapshot()
	if len(blocks) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(filter.BlockHash) != "" {
		block, ok := findBlockByHash(blocks, filter.BlockHash)
		if !ok {
			return []Block{}, nil
		}
		return []Block{block}, nil
	}

	earliest := blocks[0].ID
	latest := blocks[len(blocks)-1].ID
	parseTag := func(tag string, fallback uint64) (uint64, error) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return fallback, nil
		}
		switch strings.ToLower(tag) {
		case "latest", "pending", "safe":
			return latest, nil
		case "earliest":
			return 0, nil
		case "finalized":
			fh := s.explorerAvailableFinalizedHeight()
			if fh == 0 {
				return latest, nil
			}
			return fh, nil
		}
		q, err := parseRPCQuantity(json.RawMessage(strconv.Quote(tag)))
		if err != nil {
			return 0, err
		}
		return q, nil
	}

	from, err := parseTag(filter.FromBlock, earliest)
	if err != nil {
		return nil, err
	}
	to, err := parseTag(filter.ToBlock, latest)
	if err != nil {
		return nil, err
	}
	if from > latest || from > to {
		return []Block{}, nil
	}
	if to > latest {
		to = latest
	}

	out := make([]Block, 0, int(to-from)+1)
	for _, block := range blocks {
		if block.ID < from || block.ID > to {
			continue
		}
		out = append(out, block)
	}
	return out, nil
}

func buildTypedLogsWithSandbox(loc rpcTxLocation, sim *EVMSandboxResult) []*ethtypes.Log {
	tx := loc.Tx
	if tx.Type != TxEVM {
		return nil
	}
	if sim != nil && len(sim.RuntimeLogs) > 0 {
		txHash := common.HexToHash(rpcTxHash(tx))
		out := make([]*ethtypes.Log, 0, len(sim.RuntimeLogs))
		for _, lg := range sim.RuntimeLogs {
			addr := common.HexToAddress(normalizeEVMAddressKey(lg.Address))
			topics := make([]common.Hash, 0, len(lg.Topics))
			for _, t := range lg.Topics {
				topics = append(topics, common.HexToHash(normalizeHexHash(t)))
			}
			data, _ := decodeHexBytes(stripHexPrefix(lg.Data))
			out = append(out, &ethtypes.Log{
				Address:     addr,
				Topics:      topics,
				Data:        data,
				BlockNumber: loc.Block.ID,
				TxHash:      txHash,
				TxIndex:     uint(loc.TxIndex),
				BlockHash:   common.HexToHash(normalizeHexHash(loc.Block.BlockHash)),
				Removed:     false,
			})
		}
		return out
	}

	contract := evmExecutionAddress(tx)
	if contract == "" {
		return nil
	}
	addr := common.HexToAddress(contract)
	txHash := common.HexToHash(rpcTxHash(tx))

	logs := make([]*ethtypes.Log, 0, 2)
	execData, _ := decodeHexBytes(stripHexPrefix(tx.EVMTxHash))
	logs = append(logs, &ethtypes.Log{
		Address:     addr,
		Topics:      []common.Hash{evmExecTopic0(), txHash},
		Data:        execData,
		BlockNumber: loc.Block.ID,
		TxHash:      txHash,
		TxIndex:     uint(loc.TxIndex),
		BlockHash:   common.HexToHash(normalizeHexHash(loc.Block.BlockHash)),
		Removed:     false,
	})

	if slot, value, ok := deterministicEVMStorageMutation(tx.EVMInput); ok {
		storageData, _ := decodeHexBytes(stripHexPrefix(value))
		logs = append(logs, &ethtypes.Log{
			Address:     addr,
			Topics:      []common.Hash{evmStorageTopic0(), common.HexToHash(slot)},
			Data:        storageData,
			BlockNumber: loc.Block.ID,
			TxHash:      txHash,
			TxIndex:     uint(loc.TxIndex),
			BlockHash:   common.HexToHash(normalizeHexHash(loc.Block.BlockHash)),
			Removed:     false,
		})
	}
	return logs
}

func buildTypedLogs(loc rpcTxLocation) []*ethtypes.Log {
	return buildTypedLogsWithSandbox(loc, nil)
}

func buildRPCLogEntry(lg *ethtypes.Log) map[string]any {
	topics := make([]string, 0, len(lg.Topics))
	for _, t := range lg.Topics {
		topics = append(topics, t.Hex())
	}
	return map[string]any{
		"address":          lg.Address.Hex(),
		"topics":           topics,
		"data":             normalizeHexData(hex.EncodeToString(lg.Data)),
		"blockNumber":      encodeRPCQuantityUint64(lg.BlockNumber),
		"transactionHash":  normalizeHexHash(lg.TxHash.Hex()),
		"transactionIndex": encodeRPCQuantityUint64(uint64(lg.TxIndex)),
		"blockHash":        normalizeHexHash(lg.BlockHash.Hex()),
		"logIndex":         encodeRPCQuantityUint64(uint64(lg.Index)),
		"removed":          lg.Removed,
	}
}

func (s *Server) blockLogCountUntilTx(block Block, txIndex int) int {
	if txIndex <= 0 {
		return 0
	}
	if txIndex > len(block.Transactions) {
		txIndex = len(block.Transactions)
	}
	total := 0
	for i := 0; i < txIndex; i++ {
		loc := rpcTxLocation{
			Tx:         block.Transactions[i],
			Block:      block,
			Found:      true,
			BlockFound: true,
			TxIndex:    i,
		}
		if !dtlCompatRPCSubsetEnabled() {
			if sim, ok := s.simulateEVMAtTx(loc); ok {
				total += len(buildTypedLogsWithSandbox(loc, &sim))
			} else {
				total += len(buildTypedLogs(loc))
			}
		} else {
			total += len(buildTypedLogs(loc))
		}
		if receipt, ok := blockReceiptAtTxIndex(block, i); ok {
			total += len(receipt.Logs)
		}
	}
	return total
}

func (s *Server) gasUsedForTxLocation(loc rpcTxLocation) uint64 {
	gasUsed := txGasLimit(loc.Tx)
	if sim, ok := s.simulateEVMAtTx(loc); ok && sim.GasUsed > 0 {
		return sim.GasUsed
	}
	return gasUsed
}

func (s *Server) cumulativeGasUsed(block Block, txIndex int) uint64 {
	if txIndex < 0 || len(block.Transactions) == 0 {
		return 0
	}
	if txIndex >= len(block.Transactions) {
		txIndex = len(block.Transactions) - 1
	}
	var total uint64
	for i := 0; i <= txIndex; i++ {
		loc := rpcTxLocation{
			Tx:         block.Transactions[i],
			Block:      block,
			Found:      true,
			BlockFound: true,
			TxIndex:    i,
		}
		total += s.gasUsedForTxLocation(loc)
	}
	return total
}

func (s *Server) latestChainHeight() uint64 {
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		return 0
	}
	return s.Node.Blockchain.Height()
}

func (s *Server) ledgerAtHeight(targetHeight uint64) (Ledger, bool) {
	if s == nil || s.Node == nil {
		return Ledger{}, false
	}
	latest := s.latestChainHeight()
	if targetHeight > latest {
		return Ledger{}, false
	}
	if targetHeight == latest {
		return s.Node.Ledger.Clone(), true
	}

	ledger := GenesisLedger()
	startHeight := uint64(1)
	if snap, err := s.Node.GetSnapshotAtOrBelow(targetHeight); err == nil && snap != nil {
		ledger = snap.Ledger.Clone()
		if snap.Height >= targetHeight {
			return ledger, true
		}
		startHeight = snap.Height + 1
	}

	blocks := s.rpcBlockSnapshot()
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID < blocks[j].ID })
	for _, block := range blocks {
		if block.ID < startHeight || block.ID > targetHeight {
			continue
		}
		next, err := ApplyBlockState(ledger, block)
		if err != nil {
			return Ledger{}, false
		}
		ledger = next
	}
	return ledger, true
}

func (s *Server) pendingLedger() (Ledger, uint64, bool) {
	latest := s.latestChainHeight()
	ledger, ok := s.ledgerAtHeight(latest)
	if !ok || s == nil || s.Node == nil {
		return Ledger{}, 0, false
	}
	s.Node.Mempool.mu.Lock()
	pending := append([]Transaction(nil), s.Node.Mempool.Transactions...)
	s.Node.Mempool.mu.Unlock()

	execHeight := int(latest + 1)
	for _, tx := range pending {
		next, err := ExecuteTransaction(&ledger, tx, execHeight)
		if err != nil {
			continue
		}
		ledger = next
	}
	return ledger, latest + 1, true
}

func (s *Server) resolveLedgerByTag(raw json.RawMessage) (Ledger, uint64, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		height := s.latestChainHeight()
		ledger, ok := s.ledgerAtHeight(height)
		return ledger, height, ok, nil
	}

	var tag string
	if err := json.Unmarshal(trimmed, &tag); err == nil {
		if strings.EqualFold(strings.TrimSpace(tag), "pending") {
			ledger, height, ok := s.pendingLedger()
			return ledger, height, ok, nil
		}
	}

	block, found, err := s.resolveBlockByTag(trimmed)
	if err != nil {
		return Ledger{}, 0, false, err
	}
	if !found {
		return Ledger{}, 0, false, nil
	}
	ledger, ok := s.ledgerAtHeight(block.ID)
	return ledger, block.ID, ok, nil
}

func (s *Server) collectRPCLogs(filter rpcLogFilter) ([]any, error) {
	blocks, err := s.resolveRPCLogBlocks(filter)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0)
	for _, block := range blocks {
		blockLogIndex := 0
		for txIndex, tx := range block.Transactions {
			loc := rpcTxLocation{
				Tx:         tx,
				Block:      block,
				Found:      true,
				BlockFound: true,
				TxIndex:    txIndex,
			}
			typedLogs := make([]*ethtypes.Log, 0, 4)
			if !dtlCompatRPCSubsetEnabled() {
				var sim *EVMSandboxResult
				if r, ok := s.simulateEVMAtTx(loc); ok {
					sim = &r
				}
				evmLogs := buildTypedLogsWithSandbox(loc, sim)
				for _, lg := range evmLogs {
					lg.Index = uint(blockLogIndex)
					blockLogIndex++
					typedLogs = append(typedLogs, lg)
				}
			} else {
				// Even in DTL-compat mode keep deterministic order if historical EVM txs exist.
				evmLogs := buildTypedLogs(loc)
				for _, lg := range evmLogs {
					lg.Index = uint(blockLogIndex)
					blockLogIndex++
					typedLogs = append(typedLogs, lg)
				}
			}
			if receipt, ok := blockReceiptAtTxIndex(block, txIndex); ok {
				dtlLogs := buildTypedDTLLogs(loc, receipt, blockLogIndex)
				blockLogIndex += len(dtlLogs)
				typedLogs = append(typedLogs, dtlLogs...)
			}
			for _, lg := range typedLogs {
				entry := buildRPCLogEntry(lg)
				if rpcLogMatchesFilter(entry, filter) {
					out = append(out, entry)
				}
			}
		}
	}
	return out, nil
}

func (s *Server) ledgerBeforeTx(loc rpcTxLocation) (Ledger, bool) {
	if s == nil || s.Node == nil || !loc.BlockFound {
		return Ledger{}, false
	}
	targetHeight := loc.Block.ID
	if targetHeight == 0 {
		return s.Node.Ledger.Clone(), true
	}

	startHeight := uint64(1)
	ledger := NewLedger()
	if snap, err := s.Node.GetSnapshotAtOrBelow(targetHeight - 1); err == nil && snap != nil {
		ledger = snap.Ledger.Clone()
		startHeight = snap.Height + 1
	} else if snap0, err0 := s.Node.GetSnapshot(0); err0 == nil && snap0 != nil {
		ledger = snap0.Ledger.Clone()
		startHeight = 1
	} else {
		return Ledger{}, false
	}

	blocks := s.rpcBlockSnapshot()
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID < blocks[j].ID })
	for _, block := range blocks {
		if block.ID < startHeight || block.ID >= targetHeight {
			continue
		}
		for _, tx := range block.Transactions {
			next, err := ExecuteTransaction(&ledger, tx, int(block.ID))
			if err != nil {
				return Ledger{}, false
			}
			ledger = next
		}
	}
	for i := 0; i < loc.TxIndex && i < len(loc.Block.Transactions); i++ {
		next, err := ExecuteTransaction(&ledger, loc.Block.Transactions[i], int(targetHeight))
		if err != nil {
			return Ledger{}, false
		}
		ledger = next
	}
	return ledger, true
}

func (s *Server) simulateEVMAtTx(loc rpcTxLocation) (EVMSandboxResult, bool) {
	if !loc.BlockFound || loc.Tx.Type != TxEVM {
		return EVMSandboxResult{}, false
	}
	preLedger, ok := s.ledgerBeforeTx(loc)
	if !ok {
		return EVMSandboxResult{}, false
	}
	result, err := runCustomEVMSandboxWithContext(loc.Tx, int(loc.Block.ID), &preLedger, loc.TxIndex)
	if err != nil {
		return EVMSandboxResult{}, false
	}
	return result, true
}

func (s *Server) filterChanges(filter *rpcCompatFilter) ([]any, error) {
	if filter == nil {
		return nil, errors.New("filter not found")
	}
	switch filter.Kind {
	case "logs":
		logs, err := s.collectRPCLogs(filter.LogFilter)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(logs))
		if filter.Seen == nil {
			filter.Seen = make(map[string]struct{})
		}
		for _, item := range logs {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key := rpcLogEntryIdentity(entry)
			if _, exists := filter.Seen[key]; exists {
				continue
			}
			filter.Seen[key] = struct{}{}
			out = append(out, entry)
		}
		return out, nil
	case "blocks":
		hashes := s.currentBlockHashes()
		out := make([]any, 0, len(hashes))
		if filter.Seen == nil {
			filter.Seen = make(map[string]struct{})
		}
		for _, h := range hashes {
			key := strings.ToLower(h)
			if _, exists := filter.Seen[key]; exists {
				continue
			}
			filter.Seen[key] = struct{}{}
			out = append(out, h)
		}
		return out, nil
	case "pending":
		hashes := s.currentPendingTxHashes()
		out := make([]any, 0, len(hashes))
		if filter.Seen == nil {
			filter.Seen = make(map[string]struct{})
		}
		for _, h := range hashes {
			key := strings.ToLower(h)
			if _, exists := filter.Seen[key]; exists {
				continue
			}
			filter.Seen[key] = struct{}{}
			out = append(out, h)
		}
		return out, nil
	default:
		return nil, errors.New("unsupported filter kind")
	}
}

func parseWSSubscribeParams(raw json.RawMessage) (wsSubscribeOptions, error) {
	params, err := rpcParamsAsArray(raw)
	if err != nil {
		return wsSubscribeOptions{}, err
	}
	if len(params) < 1 {
		return wsSubscribeOptions{}, errors.New("missing subscription type")
	}
	var kind string
	if err := json.Unmarshal(params[0], &kind); err != nil {
		return wsSubscribeOptions{}, errors.New("invalid subscription type")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	opts := wsSubscribeOptions{
		Kind:      kind,
		LogFilter: rpcLogFilter{},
	}
	if kind == "logs" && len(params) > 1 {
		parsed, err := parseRPCLogFilter(params[1])
		if err != nil {
			return wsSubscribeOptions{}, err
		}
		opts.LogFilter = parsed
	}
	if kind == "newpendingtransactions" && len(params) > 1 {
		var full bool
		if err := json.Unmarshal(params[1], &full); err != nil {
			return wsSubscribeOptions{}, errors.New("invalid pending subscription options")
		}
		opts.PendingFullTx = full
	}
	return opts, nil
}

func parseWSUnsubscribeParams(raw json.RawMessage) (string, error) {
	params, err := rpcParamsAsArray(raw)
	if err != nil {
		return "", err
	}
	if len(params) < 1 {
		return "", errors.New("missing subscription id")
	}
	var subID string
	if err := json.Unmarshal(params[0], &subID); err != nil {
		return "", errors.New("invalid subscription id")
	}
	subID = strings.ToLower(strings.TrimSpace(subID))
	if subID == "" {
		return "", errors.New("invalid subscription id")
	}
	return subID, nil
}

func wsSubscriptionNotification(subID string, result any) map[string]any {
	return map[string]any{
		"jsonrpc": jsonRPCVersion,
		"method":  "msc_subscription",
		"params": map[string]any{
			"subscription": strings.ToLower(strings.TrimSpace(subID)),
			"result":       result,
		},
	}
}

func (s *Server) collectWSSubscriptionNotifications(subs map[string]*wsRPCSubscription) []map[string]any {
	if len(subs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, 8)
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		switch sub.Kind {
		case "newheads":
			blocks := s.rpcBlockSnapshot()
			for _, block := range blocks {
				if block.ID <= sub.LastHeight {
					continue
				}
				out = append(out, wsSubscriptionNotification(sub.ID, s.buildEthBlockObject(block, false)))
				sub.LastHeight = block.ID
			}
		case "logs":
			logs, err := s.collectRPCLogs(sub.LogFilter)
			if err != nil {
				continue
			}
			if sub.Seen == nil {
				sub.Seen = make(map[string]struct{})
			}
			for _, item := range logs {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				key := rpcLogEntryIdentity(entry)
				if _, exists := sub.Seen[key]; exists {
					continue
				}
				sub.Seen[key] = struct{}{}
				out = append(out, wsSubscriptionNotification(sub.ID, entry))
			}
		case "newpendingtransactions":
			pending := s.currentPendingTxLocations()
			if sub.Seen == nil {
				sub.Seen = make(map[string]struct{})
			}
			for _, loc := range pending {
				hash := strings.ToLower(strings.TrimSpace(rpcTxHash(loc.Tx)))
				key := hash
				if _, exists := sub.Seen[key]; exists {
					continue
				}
				sub.Seen[key] = struct{}{}
				result := any(normalizeHexHash(hash))
				if sub.PendingFullTx {
					result = s.buildEthTxObject(loc)
				}
				out = append(out, wsSubscriptionNotification(sub.ID, result))
			}
		case "syncing":
			state := s.rpcSyncingResult()
			stateSig := rpcSyncingStateSignature(state)
			if stateSig != sub.LastSyncState {
				sub.LastSyncState = stateSig
				out = append(out, wsSubscriptionNotification(sub.ID, state))
			}
		}
	}
	return out
}

func decodeRawTxForRPC(tx Transaction) (*ethtypes.Transaction, error) {
	raw := strings.TrimSpace(tx.EVMRawTx)
	if raw == "" {
		return nil, nil
	}
	bin, err := decodeHexBytes(raw)
	if err != nil {
		return nil, err
	}
	var out ethtypes.Transaction
	if err := out.UnmarshalBinary(bin); err != nil {
		return nil, err
	}
	return &out, nil
}

func rpcRawTxHex(tx Transaction) string {
	raw := normalizeHexData(tx.EVMRawTx)
	if strings.TrimSpace(stripHexPrefix(raw)) == "" {
		return ""
	}
	return raw
}

func buildRPCAccessList(list ethtypes.AccessList) []any {
	if len(list) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		keys := make([]string, 0, len(item.StorageKeys))
		for _, key := range item.StorageKeys {
			keys = append(keys, normalizeHexHash(key.Hex()))
		}
		out = append(out, map[string]any{
			"address":     item.Address.Hex(),
			"storageKeys": keys,
		})
	}
	return out
}

func buildEthTxObjectFromSigned(tx *ethtypes.Transaction, fallbackFrom string) map[string]any {
	chainID := chainIDBigInt()
	out := map[string]any{
		"hash":                 "0x0",
		"nonce":                "0x0",
		"from":                 toEVMHexAddress(fallbackFrom),
		"to":                   nil,
		"value":                "0x0",
		"gas":                  "0x0",
		"gasPrice":             "0x0",
		"input":                "0x",
		"type":                 "0x0",
		"chainId":              encodeRPCQuantityBig(chainID),
		"maxFeePerGas":         "0x0",
		"maxPriorityFeePerGas": "0x0",
		"v":                    "0x0",
		"r":                    "0x0",
		"s":                    "0x0",
		"blockHash":            nil,
		"blockNumber":          nil,
		"transactionIndex":     nil,
	}
	if tx == nil {
		return out
	}

	out["hash"] = normalizeHexHash(tx.Hash().Hex())
	out["nonce"] = encodeRPCQuantityUint64(tx.Nonce())
	out["type"] = encodeRPCQuantityUint64(uint64(tx.Type()))
	out["gas"] = encodeRPCQuantityUint64(tx.Gas())
	out["value"] = encodeRPCQuantityBig(tx.Value())
	out["input"] = normalizeHexData("0x" + hex.EncodeToString(tx.Data()))

	if to := tx.To(); to != nil {
		out["to"] = to.Hex()
	}
	if gp := tx.GasPrice(); gp != nil && gp.Sign() >= 0 {
		out["gasPrice"] = encodeRPCQuantityBig(gp)
	}
	if feeCap := tx.GasFeeCap(); feeCap != nil && feeCap.Sign() >= 0 {
		out["maxFeePerGas"] = encodeRPCQuantityBig(feeCap)
	}
	if tipCap := tx.GasTipCap(); tipCap != nil && tipCap.Sign() >= 0 {
		out["maxPriorityFeePerGas"] = encodeRPCQuantityBig(tipCap)
	}
	if accessList := tx.AccessList(); len(accessList) > 0 {
		out["accessList"] = buildRPCAccessList(accessList)
	}
	if cid := tx.ChainId(); cid != nil && cid.Sign() > 0 {
		chainID = cid
		out["chainId"] = encodeRPCQuantityBig(cid)
	}
	v, r, sVal := tx.RawSignatureValues()
	if v != nil {
		out["v"] = encodeRPCQuantityBig(v)
	}
	if r != nil {
		out["r"] = encodeRPCQuantityBig(r)
	}
	if sVal != nil {
		out["s"] = encodeRPCQuantityBig(sVal)
	}
	signer := ethtypes.LatestSignerForChainID(chainID)
	if sender, err := ethtypes.Sender(signer, tx); err == nil {
		out["from"] = sender.Hex()
	}

	return out
}

func rpcTxTypeAndEffectiveGasPrice(tx Transaction, fallbackEffectiveFee int) (string, string) {
	typ := "0x0"
	effective := encodeRPCQuantityInt(fallbackEffectiveFee)
	rawTx, err := decodeRawTxForRPC(tx)
	if err != nil || rawTx == nil {
		return typ, effective
	}
	typ = encodeRPCQuantityUint64(uint64(rawTx.Type()))
	if gp := rawTx.GasPrice(); gp != nil && gp.Sign() >= 0 {
		effective = encodeRPCQuantityBig(gp)
	}
	return typ, effective
}

func (s *Server) evmFeeBreakdown(tx Transaction, blockFound bool, blockID uint64) (paid int, base int, tip int) {
	paid = tx.Fee
	if paid < 0 {
		paid = 0
	}
	if tx.Type != TxEVM {
		return paid, 0, 0
	}

	base = ComputeEVMFee(tx.EVMGasLimit)
	switch {
	case s == nil || s.Node == nil:
		// fallback default base
	case blockFound:
		base = computeEVMFeeFromBase(s.evmBaseFeeForBlock(blockID), tx.EVMGasLimit)
	default:
		base = requiredEVMFeeForLedger(&s.Node.Ledger, tx.EVMGasLimit)
	}

	if paid < base {
		paid = base
	}
	tip = paid - base
	if tip < 0 {
		tip = 0
	}
	return paid, base, tip
}

func (s *Server) buildEthTxObject(loc rpcTxLocation) map[string]any {
	tx := loc.Tx
	chainID := chainIDBigInt()
	paidFee, baseFee, priorityFee := s.evmFeeBreakdown(tx, loc.BlockFound, loc.Block.ID)
	out := map[string]any{
		"hash":                 rpcTxHash(tx),
		"nonce":                encodeRPCQuantityInt(rpcTxNonce(tx)),
		"from":                 toEVMHexAddress(tx.From),
		"to":                   rpcOptionalToAddress(tx.To),
		"value":                encodeRPCNativeAmount(tx.Amount),
		"gas":                  encodeRPCQuantityUint64(txGasLimit(tx)),
		"gasPrice":             encodeRPCQuantityInt(paidFee),
		"input":                normalizeHexData(tx.EVMInput),
		"type":                 "0x0",
		"chainId":              encodeRPCQuantityBig(chainID),
		"maxFeePerGas":         encodeRPCQuantityInt(paidFee),
		"maxPriorityFeePerGas": encodeRPCQuantityInt(priorityFee),
		"v":                    "0x0",
		"r":                    "0x0",
		"s":                    "0x0",
	}
	if tx.Type == TxEVM {
		out["mscBaseFee"] = encodeRPCQuantityInt(baseFee)
		out["mscPriorityFee"] = encodeRPCQuantityInt(priorityFee)
		out["mscFeePaid"] = encodeRPCQuantityInt(paidFee)
	}
	if tx.Type == TxEVM && stripHexPrefix(tx.EVMInput) == "" {
		out["input"] = normalizeHexData(tx.EVMCode)
	}

	if loc.BlockFound {
		out["blockHash"] = normalizeHexHash(loc.Block.BlockHash)
		out["blockNumber"] = encodeRPCQuantityUint64(loc.Block.ID)
		out["transactionIndex"] = encodeRPCQuantityInt(loc.TxIndex)
	} else {
		out["blockHash"] = nil
		out["blockNumber"] = nil
		out["transactionIndex"] = nil
	}

	rawTx, err := decodeRawTxForRPC(tx)
	if err == nil && rawTx != nil {
		out["hash"] = normalizeHexHash(rawTx.Hash().Hex())
		out["nonce"] = encodeRPCQuantityUint64(rawTx.Nonce())
		out["type"] = encodeRPCQuantityUint64(uint64(rawTx.Type()))
		out["gas"] = encodeRPCQuantityUint64(rawTx.Gas())
		out["value"] = encodeRPCQuantityBig(rawTx.Value())
		if to := rawTx.To(); to == nil {
			out["to"] = nil
		} else {
			out["to"] = to.Hex()
		}
		out["input"] = normalizeHexData("0x" + hex.EncodeToString(rawTx.Data()))
		if cid := rawTx.ChainId(); cid != nil && cid.Sign() > 0 {
			out["chainId"] = encodeRPCQuantityBig(cid)
		}
		if gp := rawTx.GasPrice(); gp != nil && gp.Sign() >= 0 {
			out["gasPrice"] = encodeRPCQuantityBig(gp)
		}
		if feeCap := rawTx.GasFeeCap(); feeCap != nil && feeCap.Sign() >= 0 {
			out["maxFeePerGas"] = encodeRPCQuantityBig(feeCap)
		}
		if tipCap := rawTx.GasTipCap(); tipCap != nil && tipCap.Sign() >= 0 {
			out["maxPriorityFeePerGas"] = encodeRPCQuantityBig(tipCap)
		}
		if accessList := rawTx.AccessList(); len(accessList) > 0 {
			out["accessList"] = buildRPCAccessList(accessList)
		}
		v, r, sVal := rawTx.RawSignatureValues()
		if v != nil {
			out["v"] = encodeRPCQuantityBig(v)
		}
		if r != nil {
			out["r"] = encodeRPCQuantityBig(r)
		}
		if sVal != nil {
			out["s"] = encodeRPCQuantityBig(sVal)
		}
		signChainID := chainID
		if cid := rawTx.ChainId(); cid != nil && cid.Sign() > 0 {
			signChainID = cid
		}
		signer := ethtypes.LatestSignerForChainID(signChainID)
		if sender, err := ethtypes.Sender(signer, rawTx); err == nil {
			out["from"] = sender.Hex()
		}
	}
	if tx.Type == TxEVM {
		// Keep MSC fee accounting visible even for typed raw tx views.
		out["mscBaseFee"] = encodeRPCQuantityInt(baseFee)
		out["mscPriorityFee"] = encodeRPCQuantityInt(priorityFee)
		out["mscFeePaid"] = encodeRPCQuantityInt(paidFee)
	}
	return out
}

func evmExecTopic0() common.Hash {
	return common.BytesToHash(ethcrypto.Keccak256([]byte("MSC_EVM_EXEC(bytes32)")))
}

func evmStorageTopic0() common.Hash {
	return common.BytesToHash(ethcrypto.Keccak256([]byte("MSC_EVM_STORAGE(bytes32,bytes32)")))
}

func buildReceiptLogs(loc rpcTxLocation, startIndex int, sim *EVMSandboxResult, stateReceipt *StateReceipt) ([]any, string) {
	typedLogs := make([]*ethtypes.Log, 0, 4)
	evmLogs := buildTypedLogsWithSandbox(loc, sim)
	for i, lg := range evmLogs {
		lg.Index = uint(startIndex + i)
		typedLogs = append(typedLogs, lg)
	}
	dtlStart := startIndex + len(evmLogs)
	dtlLogs := buildTypedDTLLogs(loc, stateReceipt, dtlStart)
	typedLogs = append(typedLogs, dtlLogs...)
	if len(typedLogs) == 0 {
		return []any{}, emptyLogsBloom
	}

	out := make([]any, 0, len(typedLogs))
	for _, lg := range typedLogs {
		out = append(out, buildRPCLogEntry(lg))
	}

	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: typedLogs})
	bloomHex := "0x" + strings.ToLower(hex.EncodeToString(bloom[:]))
	return out, bloomHex
}

func (s *Server) buildEthReceipt(loc rpcTxLocation) map[string]any {
	if !loc.Found || !loc.BlockFound {
		return nil
	}
	tx := loc.Tx
	gasUsed := s.gasUsedForTxLocation(loc)
	var sim *EVMSandboxResult
	if r, ok := s.simulateEVMAtTx(loc); ok {
		sim = &r
		if r.GasUsed > 0 {
			gasUsed = r.GasUsed
		}
	}
	cumulativeGasUsed := s.cumulativeGasUsed(loc.Block, loc.TxIndex)
	if cumulativeGasUsed == 0 {
		cumulativeGasUsed = gasUsed
	}
	var stateReceipt *StateReceipt
	if rcpt, ok := blockReceiptAtTxIndex(loc.Block, loc.TxIndex); ok {
		stateReceipt = rcpt
	}
	logIndexStart := s.blockLogCountUntilTx(loc.Block, loc.TxIndex)
	logs, bloom := buildReceiptLogs(loc, logIndexStart, sim, stateReceipt)
	paidFee, baseFee, priorityFee := s.evmFeeBreakdown(tx, true, loc.Block.ID)
	txType, effectiveGasPrice := rpcTxTypeAndEffectiveGasPrice(tx, paidFee)
	contractAddress := rpcContractAddress(tx)
	if stateReceipt != nil && strings.EqualFold(strings.TrimSpace(stateReceipt.DTLTxType), string(DTLTxContractDeploy)) {
		contractID := normalizeDTLContractID(stateReceipt.ContractID)
		if contractID != "" {
			contractAddress = toEVMHexAddress(contractID)
		}
	}
	receipt := map[string]any{
		"transactionHash":   rpcTxHash(tx),
		"transactionIndex":  encodeRPCQuantityInt(loc.TxIndex),
		"blockHash":         normalizeHexHash(loc.Block.BlockHash),
		"blockNumber":       encodeRPCQuantityUint64(loc.Block.ID),
		"from":              toEVMHexAddress(tx.From),
		"to":                rpcOptionalToAddress(tx.To),
		"cumulativeGasUsed": encodeRPCQuantityUint64(cumulativeGasUsed),
		"gasUsed":           encodeRPCQuantityUint64(gasUsed),
		"contractAddress":   contractAddress,
		"logs":              logs,
		"logsBloom":         bloom,
		"status":            "0x1",
		"type":              txType,
		"effectiveGasPrice": effectiveGasPrice,
	}
	if stateReceipt != nil {
		if contractID := normalizeDTLContractID(stateReceipt.ContractID); contractID != "" {
			receipt["contract_id"] = contractID
		}
		if mode := strings.TrimSpace(stateReceipt.RuntimeMode); mode != "" {
			receipt["runtime_mode"] = mode
		}
		if standard := normalizeDTLContractStandard(stateReceipt.ContractStandard); standard != "" {
			receipt["contract_standard"] = standard
		}
		if len(stateReceipt.ContractInterfaces) > 0 {
			receipt["contract_interfaces"] = append([]string(nil), stateReceipt.ContractInterfaces...)
		}
		if strings.TrimSpace(stateReceipt.ABIHash) != "" {
			receipt["abi_hash"] = normalizeHexHash(stateReceipt.ABIHash)
		}
		if stateReceipt.Upgradeable {
			receipt["upgradeable"] = true
		}
		if proxy := normalizeDTLContractID(stateReceipt.ProxyTarget); proxy != "" {
			receipt["proxy_target"] = proxy
		}
		if feedID := normalizeDTLTokenID(stateReceipt.OracleFeedID); feedID != "" {
			receipt["oracle_feed_id"] = feedID
		}
		if stateReceipt.HealthFactor > 0 {
			receipt["health_factor"] = stateReceipt.HealthFactor
		}
		if bcFormat := normalizeDTLBytecodeFormat(stateReceipt.BytecodeFormat); bcFormat != "" {
			receipt["bytecode_format"] = bcFormat
		}
		if hash := strings.TrimSpace(stateReceipt.BytecodeHash); hash != "" {
			receipt["bytecode_hash"] = normalizeHexHash(hash)
		}
		if stateReceipt.BytecodeSize > 0 {
			receipt["bytecode_size"] = encodeRPCQuantityUint64(stateReceipt.BytecodeSize)
		}
		if compiler := strings.TrimSpace(stateReceipt.Compiler); compiler != "" {
			receipt["compiler"] = compiler
		}
		if sourceHash := strings.TrimSpace(stateReceipt.SourceHash); sourceHash != "" {
			receipt["source_hash"] = sourceHash
		}
	}
	if tx.Type == TxEVM {
		receipt["mscBaseFee"] = encodeRPCQuantityInt(baseFee)
		receipt["mscPriorityFee"] = encodeRPCQuantityInt(priorityFee)
		receipt["mscFeePaid"] = encodeRPCQuantityInt(paidFee)
	}
	return receipt
}

func debugTraceResult(gas uint64, gasUsed uint64, returnHex string, failed bool, errText string) map[string]any {
	out := map[string]any{
		"gas":        gas,
		"gasUsed":    gasUsed,
		"failed":     failed,
		"structLogs": []any{},
	}
	ret := strings.TrimSpace(returnHex)
	if ret == "" {
		out["returnValue"] = ""
	} else {
		out["returnValue"] = stripHexPrefix(normalizeHexData(ret))
	}
	if failed && strings.TrimSpace(errText) != "" {
		out["error"] = errText
	}
	return out
}

func (s *Server) buildDebugTraceForTx(loc rpcTxLocation) map[string]any {
	if !loc.Found {
		return nil
	}
	gasLimit := txGasLimit(loc.Tx)
	if loc.Tx.Type != TxEVM {
		return debugTraceResult(gasLimit, gasLimit, "", false, "")
	}

	if loc.BlockFound {
		if sim, ok := s.simulateEVMAtTx(loc); ok {
			gasUsed := sim.GasUsed
			if gasUsed == 0 {
				gasUsed = gasLimit
			}
			return debugTraceResult(gasLimit, gasUsed, sim.OutputHex, false, "")
		}
	}

	// Best-effort fallback when replay/simulation is unavailable.
	return debugTraceResult(gasLimit, gasLimit, "0x", false, "")
}

func (s *Server) buildDebugTraceBlock(block Block) []any {
	out := make([]any, 0, len(block.Transactions))
	for idx, tx := range block.Transactions {
		loc := rpcTxLocation{
			Tx:         tx,
			Block:      block,
			Found:      true,
			BlockFound: true,
			TxIndex:    idx,
		}
		trace := s.buildDebugTraceForTx(loc)
		if trace == nil {
			continue
		}
		out = append(out, trace)
	}
	return out
}

func (s *Server) buildDebugTraceForCall(call ethCallObject, stateTag json.RawMessage) (map[string]any, error) {
	stateLedger, stateHeight, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, errors.New("invalid block tag")
	}
	if !ok {
		return nil, errors.New("header not found")
	}

	rawData := strings.TrimSpace(call.Data)
	code := ""
	input := strings.TrimSpace(call.Input)
	toAddr := strings.TrimSpace(call.To)
	if toAddr != "" {
		code = evmCodeByAddress(stateLedger, toAddr)
		if input == "" {
			input = rawData
		}
	}
	if code == "" || code == "0x" {
		code = rawData
	}
	if code == "" || code == "0x" {
		return nil, errors.New("debug_traceCall requires bytecode in data or deployed code at `to`")
	}

	gas := uint64(0)
	if strings.TrimSpace(call.Gas) != "" {
		if parsed, err := parseRPCQuantity(json.RawMessage(`"` + call.Gas + `"`)); err == nil {
			gas = parsed
		}
	}
	amount := 0
	if strings.TrimSpace(call.Value) != "" {
		if parsedAmount, err := parseRPCNativeAmount(call.Value); err == nil {
			amount = parsedAmount
		}
	}
	tx := Transaction{
		Type:        TxEVM,
		From:        strings.TrimSpace(call.From),
		To:          strings.TrimSpace(call.To),
		Amount:      amount,
		EVMCode:     code,
		EVMInput:    input,
		EVMGasLimit: gas,
	}
	if tx.From == "" {
		tx.From = "MSC_EVM_CALLER"
	}

	height := int(stateHeight)
	execLedger := &stateLedger
	result, err := runCustomEVMSandbox(tx, height, execLedger)
	if err != nil {
		return nil, err
	}
	gasLimit := txGasLimit(tx)
	gasUsed := result.GasUsed
	if gasUsed == 0 {
		gasUsed = gasLimit
	}
	return debugTraceResult(gasLimit, gasUsed, result.OutputHex, false, ""), nil
}

func parseReplayTraceTypesRaw(raw json.RawMessage) map[string]bool {
	allowed := map[string]bool{
		"trace":     true,
		"vmtrace":   true,
		"statediff": true,
	}
	out := map[string]bool{"trace": true}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return out
	}
	var requested []string
	if err := json.Unmarshal(raw, &requested); err != nil {
		return out
	}
	out = map[string]bool{}
	for _, item := range requested {
		key := strings.ToLower(strings.TrimSpace(item))
		if allowed[key] {
			out[key] = true
		}
	}
	if len(out) == 0 {
		out["trace"] = true
	}
	return out
}

func parseReplayTraceTypes(params []json.RawMessage) map[string]bool {
	if len(params) < 2 {
		return map[string]bool{"trace": true}
	}
	return parseReplayTraceTypesRaw(params[1])
}

type traceFilterRequest struct {
	FromBlock   uint64
	ToBlock     uint64
	FromAddress map[string]struct{}
	ToAddress   map[string]struct{}
	After       int
	Count       int
}

func (s *Server) parseTraceFilterBlockTag(raw json.RawMessage, fallback uint64) (uint64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fallback, nil
	}
	block, found, err := s.resolveBlockByTag(trimmed)
	if err == nil {
		if found {
			return block.ID, nil
		}
		return 0, errors.New("header not found")
	}
	v, qErr := parseRPCQuantity(trimmed)
	if qErr != nil {
		return 0, err
	}
	return v, nil
}

func (s *Server) parseTraceFilterRequest(params []json.RawMessage) (traceFilterRequest, error) {
	latest := uint64(0)
	if s != nil && s.Node != nil && s.Node.Blockchain != nil {
		latest = s.Node.Blockchain.Height()
	}
	out := traceFilterRequest{
		FromBlock: 0,
		ToBlock:   latest,
		After:     0,
		Count:     128,
	}
	if len(params) == 0 {
		return out, errors.New("missing trace filter")
	}
	trimmed := bytes.TrimSpace(params[0])
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return out, nil
	}

	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return out, errors.New("invalid trace filter")
	}

	if v, ok := obj["fromBlock"]; ok && v != nil {
		raw, _ := json.Marshal(v)
		h, err := s.parseTraceFilterBlockTag(raw, out.FromBlock)
		if err != nil {
			return out, err
		}
		out.FromBlock = h
	}
	if v, ok := obj["toBlock"]; ok && v != nil {
		raw, _ := json.Marshal(v)
		h, err := s.parseTraceFilterBlockTag(raw, out.ToBlock)
		if err != nil {
			return out, err
		}
		out.ToBlock = h
	}
	if out.ToBlock < out.FromBlock {
		return out, errors.New("invalid block range")
	}

	if v, ok := obj["fromAddress"]; ok {
		addrs, err := parseRPCLogFilterAddresses(v)
		if err != nil {
			return out, err
		}
		out.FromAddress = addrs
	}
	if v, ok := obj["toAddress"]; ok {
		addrs, err := parseRPCLogFilterAddresses(v)
		if err != nil {
			return out, err
		}
		out.ToAddress = addrs
	}

	if v, ok := obj["after"]; ok && v != nil {
		raw, _ := json.Marshal(v)
		n, err := parseRPCQuantity(raw)
		if err != nil {
			return out, errors.New("invalid after")
		}
		out.After = int(n)
	}
	if v, ok := obj["count"]; ok && v != nil {
		raw, _ := json.Marshal(v)
		n, err := parseRPCQuantity(raw)
		if err != nil {
			return out, errors.New("invalid count")
		}
		out.Count = int(n)
		if out.Count <= 0 {
			out.Count = 128
		}
		if out.Count > 1000 {
			out.Count = 1000
		}
	}
	return out, nil
}

func traceAddressAllowed(set map[string]struct{}, addr string) bool {
	if len(set) == 0 {
		return true
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return false
	}
	_, ok := set[addr]
	return ok
}

func parseTraceCallParams(params []json.RawMessage) (ethCallObject, json.RawMessage, map[string]bool, error) {
	var call ethCallObject
	stateTag := json.RawMessage(`"latest"`)
	selected := map[string]bool{"trace": true}
	if len(params) < 1 {
		return call, stateTag, selected, errors.New("missing call object")
	}
	if err := json.Unmarshal(params[0], &call); err != nil {
		return call, stateTag, selected, errors.New("invalid call object")
	}

	for i := 1; i < len(params); i++ {
		raw := bytes.TrimSpace(params[i])
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		if raw[0] == '[' {
			selected = parseReplayTraceTypesRaw(params[i])
			continue
		}
		stateTag = params[i]
	}
	return call, stateTag, selected, nil
}

func buildTraceCallLocation(call ethCallObject) (rpcTxLocation, error) {
	dataHex := strings.TrimSpace(call.Input)
	if dataHex == "" {
		dataHex = strings.TrimSpace(call.Data)
	}
	if dataHex == "" {
		dataHex = "0x"
	}
	if _, err := decodeHexBytes(dataHex); err != nil {
		return rpcTxLocation{}, fmt.Errorf("invalid input data: %w", err)
	}
	dataHex = normalizeHexData(dataHex)

	amount := 0
	if strings.TrimSpace(call.Value) != "" {
		value, err := parseRPCNativeAmount(call.Value)
		if err != nil {
			return rpcTxLocation{}, fmt.Errorf("invalid value: %w", err)
		}
		amount = value
	}

	gasLimit := normalizeEVMGasLimit(currentEVMDefaultGasLimit())
	if strings.TrimSpace(call.Gas) != "" {
		parsedGas, err := parseRPCQuantityString(call.Gas)
		if err != nil {
			return rpcTxLocation{}, fmt.Errorf("invalid gas: %w", err)
		}
		gasLimit = normalizeEVMGasLimit(parsedGas)
	}

	from := strings.TrimSpace(call.From)
	if from == "" {
		from = "MSC_EVM_CALLER"
	}
	to := strings.TrimSpace(call.To)

	tx := Transaction{
		Type:        TxEVM,
		From:        from,
		To:          to,
		Amount:      amount,
		EVMGasLimit: gasLimit,
		EVMInput:    dataHex,
		EVMCode:     "0x",
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	if to == "" {
		tx.EVMCode = dataHex
		tx.EVMInput = "0x"
	}

	return rpcTxLocation{
		Tx:      tx,
		Found:   true,
		Pending: true,
	}, nil
}

func buildReplayTraceEntry(loc rpcTxLocation, trace map[string]any, receipt map[string]any) map[string]any {
	tx := loc.Tx
	typ := "call"
	action := map[string]any{
		"from":  toEVMHexAddress(tx.From),
		"gas":   encodeRPCQuantityUint64(txGasLimit(tx)),
		"input": normalizeHexData(tx.EVMInput),
		"value": encodeRPCNativeAmount(tx.Amount),
	}
	result := map[string]any{
		"gasUsed": encodeRPCQuantityUint64(txGasLimit(tx)),
		"output":  "0x",
	}
	if strings.TrimSpace(tx.To) == "" {
		typ = "create"
		action["init"] = normalizeHexData(tx.EVMCode)
	} else {
		action["to"] = toEVMHexAddress(tx.To)
		action["callType"] = "call"
	}
	if trace != nil {
		if gasUsed, ok := trace["gasUsed"]; ok {
			result["gasUsed"] = gasUsed
		}
		if ret, ok := trace["returnValue"].(string); ok {
			result["output"] = normalizeHexData(ret)
		}
	}
	if typ == "create" && receipt != nil {
		if addr, ok := receipt["contractAddress"].(string); ok && strings.TrimSpace(addr) != "" {
			result["address"] = toEVMHexAddress(addr)
		}
	}
	out := map[string]any{
		"action":       action,
		"result":       result,
		"subtraces":    0,
		"traceAddress": []any{},
		"type":         typ,
	}
	if loc.BlockFound {
		out["blockHash"] = normalizeHexHash(loc.Block.BlockHash)
		out["blockNumber"] = encodeRPCQuantityUint64(loc.Block.ID)
		out["transactionHash"] = rpcTxHash(tx)
		out["transactionPosition"] = encodeRPCQuantityInt(loc.TxIndex)
	} else if hash := strings.TrimSpace(rpcTxHash(tx)); hash != "" {
		out["transactionHash"] = hash
	}
	return out
}

func (s *Server) buildEthBlockObject(block Block, fullTx bool) map[string]any {
	chainID := chainIDBigInt()
	txs := make([]any, 0, len(block.Transactions))
	var gasUsed uint64
	for idx, tx := range block.Transactions {
		loc := rpcTxLocation{
			Tx:         tx,
			Block:      block,
			Found:      true,
			BlockFound: true,
			TxIndex:    idx,
		}
		gasUsed += s.gasUsedForTxLocation(loc)
		if fullTx {
			txs = append(txs, s.buildEthTxObject(loc))
		} else {
			txs = append(txs, rpcTxHash(tx))
		}
	}

	timestamp := uint64(0)
	if block.Timestamp > 0 {
		timestamp = uint64(block.Timestamp)
	}
	if block.ID == 0 && len(block.PrevHash) == 0 {
		// Keep genesis parent as zero hash.
		block.PrevHash = "0x" + strings.Repeat("0", 64)
	}

	return map[string]any{
		"number":           encodeRPCQuantityUint64(block.ID),
		"hash":             normalizeHexHash(block.BlockHash),
		"parentHash":       normalizeHexHash(block.PrevHash),
		"nonce":            "0x0000000000000000",
		"sha3Uncles":       emptyUncleHash,
		"logsBloom":        emptyLogsBloom,
		"transactionsRoot": normalizeHexHash(block.MempoolRoot),
		"stateRoot":        normalizeHexHash(block.StateRoot),
		"receiptsRoot":     normalizeHexHash(block.StateRoot),
		"miner":            toEVMHexAddress(block.Proposer),
		"difficulty":       "0x0",
		"totalDifficulty":  encodeRPCQuantityBig(new(big.Int).SetUint64(block.ID)),
		"extraData":        "0x",
		"size":             encodeRPCQuantityInt(0),
		"gasLimit":         encodeRPCQuantityUint64(currentEVMMaxGasLimit()),
		"gasUsed":          encodeRPCQuantityUint64(gasUsed),
		"timestamp":        encodeRPCQuantityUint64(timestamp),
		"transactions":     txs,
		"uncles":           []any{},
		"baseFeePerGas":    encodeRPCQuantityInt(s.evmBaseFeeForBlock(block.ID)),
		"mixHash":          normalizeHexHash(fmt.Sprintf("%064x", chainID.Uint64())),
	}
}

func (s *Server) submitEVMRawTransaction(rawHex string) (string, error) {
	if s == nil || s.Node == nil {
		return "", errors.New("node unavailable")
	}

	decoded, err := decodeEVMRawTransaction(rawHex)
	if err != nil {
		return "", err
	}

	wantChainID := chainIDBigInt()
	if decoded.ChainID.Cmp(wantChainID) != 0 {
		return "", fmt.Errorf("invalid chain id: got %s expected %s", decoded.ChainID.String(), wantChainID.String())
	}
	internalNonce, err := internalNonceFromEVMNonce(decoded.Nonce)
	if err != nil {
		return "", err
	}
	amount, err := evmRawValueToInt(decoded.Value)
	if err != nil {
		return "", err
	}

	txHash := decoded.Hash.Hex()
	to := ""
	if decoded.To != nil {
		to = decoded.To.Hex()
	}
	rawData := "0x" + hex.EncodeToString(decoded.Data)
	code := ""
	input := "0x"
	if decoded.To == nil {
		if len(decoded.Data) == 0 {
			return "", errors.New("raw deploy tx requires non-empty bytecode data")
		}
		code = rawData
	} else {
		input = rawData
		// Backward-compatible fallback: if contract code is unknown locally and
		// calldata carries bytes, keep inline code path available.
		if evmCodeByAddress(s.Node.Ledger, to) == "0x" {
			if stripHexPrefix(rawData) == "" {
				// Pure value transfer / no-calldata call fallback for EVM-compat:
				// execute a minimal STOP bytecode when target code is unavailable.
				code = "0x00"
			} else {
				code = rawData
			}
		}
	}

	tx := Transaction{
		From:        decoded.From.Hex(),
		To:          to,
		Amount:      amount,
		Nonce:       internalNonce,
		Fee:         s.currentEVMMarketFee(decoded.Gas),
		Expiry:      time.Now().Add(2 * time.Minute).Unix(),
		EVMCode:     code,
		EVMInput:    input,
		EVMGasLimit: decoded.Gas,
		EVMRawTx:    normalizeHexData(rawHex),
		EVMTxHash:   txHash,
		Type:        TxEVM,
		ChainID:     ChainID,
		Coin:        CoinSymbol,
	}

	if ok, reason := s.Node.ReceiveTransactionWithReason(tx); !ok {
		if reason == "duplicate transaction" {
			return txHash, nil
		}
		if reason == "" {
			reason = "transaction rejected"
		}
		return "", errors.New(reason)
	}

	return txHash, nil
}

func (s *Server) signEVMTransactionObject(call ethCallObject, method string) (*ethtypes.Transaction, string, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		method = "msc_sendTransaction"
	}

	key, devAddr, ok, err := loadDevEVMPrivateKey()
	if err != nil {
		return nil, "", fmt.Errorf("invalid MSC_EVM_DEV_PRIVKEY: %w", err)
	}
	if !ok {
		return nil, "", fmt.Errorf("%s requires MSC_EVM_DEV_PRIVKEY", method)
	}
	if !common.IsHexAddress(call.From) {
		return nil, "", errors.New("invalid from address")
	}
	from := common.HexToAddress(call.From)
	if !strings.EqualFold(from.Hex(), devAddr.Hex()) {
		return nil, "", errors.New("from address is not unlocked by node")
	}
	// MSC-only enforcement for EVM compatibility mode.
	wantChainID := chainIDBigInt()
	if !isMSCOnlyChainID(wantChainID) {
		return nil, "", fmt.Errorf("only %s chain (%d) is supported", mscCoinFullName, mscOnlyChainID)
	}
	if strings.TrimSpace(call.ChainID) != "" {
		callChainID, err := parseRPCBigIntString(call.ChainID)
		if err != nil {
			return nil, "", errors.New("invalid chainId")
		}
		if callChainID.Cmp(wantChainID) != 0 {
			return nil, "", fmt.Errorf("invalid chain id: got %s expected %s", callChainID.String(), wantChainID.String())
		}
	}

	var to *common.Address
	toRaw := strings.TrimSpace(call.To)
	if toRaw != "" {
		if !common.IsHexAddress(toRaw) {
			if !isLikelyMSCWalletAddress(toRaw) {
				return nil, "", errors.New("invalid to address")
			}
			if s != nil && s.Node != nil {
				registerLedgerAddressAlias(&s.Node.Ledger, toRaw)
			}
			toRaw = toEVMHexAddress(toRaw)
		}
		toAddr := common.HexToAddress(toRaw)
		to = &toAddr
	}

	gasLimit := normalizeEVMGasLimit(currentEVMDefaultGasLimit())
	if strings.TrimSpace(call.Gas) != "" {
		parsedGas, err := parseRPCQuantityString(call.Gas)
		if err != nil {
			return nil, "", errors.New("invalid gas")
		}
		gasLimit = normalizeEVMGasLimit(parsedGas)
	}

	gasPrice := big.NewInt(1)
	if strings.TrimSpace(call.GasPrice) != "" {
		parsed, err := parseRPCBigIntString(call.GasPrice)
		if err != nil {
			return nil, "", errors.New("invalid gasPrice")
		}
		if parsed.Sign() >= 0 {
			gasPrice = parsed
		}
	}
	maxFeePerGas := new(big.Int).Set(gasPrice)
	if strings.TrimSpace(call.MaxFeePerGas) != "" {
		parsed, err := parseRPCBigIntString(call.MaxFeePerGas)
		if err != nil {
			return nil, "", errors.New("invalid maxFeePerGas")
		}
		if parsed.Sign() < 0 {
			return nil, "", errors.New("maxFeePerGas must be non-negative")
		}
		maxFeePerGas = parsed
	}
	maxPriorityFeePerGas := big.NewInt(0)
	if strings.TrimSpace(call.MaxPriorityFeePerGas) != "" {
		parsed, err := parseRPCBigIntString(call.MaxPriorityFeePerGas)
		if err != nil {
			return nil, "", errors.New("invalid maxPriorityFeePerGas")
		}
		if parsed.Sign() < 0 {
			return nil, "", errors.New("maxPriorityFeePerGas must be non-negative")
		}
		maxPriorityFeePerGas = parsed
	}
	if maxFeePerGas.Cmp(maxPriorityFeePerGas) < 0 {
		maxFeePerGas = new(big.Int).Set(maxPriorityFeePerGas)
	}
	if maxFeePerGas.Sign() == 0 && maxPriorityFeePerGas.Sign() > 0 {
		maxFeePerGas = new(big.Int).Set(maxPriorityFeePerGas)
	}
	if maxFeePerGas.Sign() == 0 {
		maxFeePerGas = big.NewInt(1)
	}
	if gasPrice.Sign() == 0 {
		gasPrice = new(big.Int).Set(maxFeePerGas)
	}

	accessList, err := parseEthAccessList(call.AccessList)
	if err != nil {
		return nil, "", err
	}
	has1559Fields := strings.TrimSpace(call.MaxFeePerGas) != "" || strings.TrimSpace(call.MaxPriorityFeePerGas) != ""
	txType, err := normalizeEthSendTxType(call.Type, len(accessList) > 0, has1559Fields)
	if err != nil {
		return nil, "", err
	}
	if txType == "0x0" && len(accessList) > 0 {
		return nil, "", errors.New("accessList requires typed transaction")
	}

	value := big.NewInt(0)
	if strings.TrimSpace(call.Value) != "" {
		parsed, err := parseRPCBigIntString(call.Value)
		if err != nil {
			return nil, "", errors.New("invalid value")
		}
		if parsed.Sign() < 0 {
			return nil, "", errors.New("value must be non-negative")
		}
		value = parsed
	}

	dataHex := strings.TrimSpace(call.Input)
	if dataHex == "" {
		dataHex = strings.TrimSpace(call.Data)
	}
	data, err := decodeHexBytes(dataHex)
	if err != nil {
		return nil, "", errors.New("invalid data")
	}

	var nonce uint64
	if strings.TrimSpace(call.Nonce) != "" {
		nonce, err = parseRPCQuantityString(call.Nonce)
		if err != nil {
			return nil, "", errors.New("invalid nonce")
		}
	} else {
		internal := s.resolveInternalAddress(from.Hex())
		nextInternal := s.rpcNextNonceForAddress(internal, true)
		if nextInternal > 0 {
			nonce = uint64(nextInternal - 1)
		}
	}

	var unsigned *ethtypes.Transaction
	switch txType {
	case "0x0":
		unsigned = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    nonce,
			To:       to,
			Value:    value,
			Gas:      gasLimit,
			GasPrice: gasPrice,
			Data:     data,
		})
	case "0x1":
		unsigned = ethtypes.NewTx(&ethtypes.AccessListTx{
			ChainID:    chainIDBigInt(),
			Nonce:      nonce,
			To:         to,
			Value:      value,
			Gas:        gasLimit,
			GasPrice:   gasPrice,
			Data:       data,
			AccessList: accessList,
		})
	case "0x2":
		unsigned = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			ChainID:    chainIDBigInt(),
			Nonce:      nonce,
			To:         to,
			Value:      value,
			Gas:        gasLimit,
			GasFeeCap:  maxFeePerGas,
			GasTipCap:  maxPriorityFeePerGas,
			Data:       data,
			AccessList: accessList,
		})
	default:
		return nil, "", errors.New("invalid transaction type")
	}
	signed, err := ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(chainIDBigInt()), key)
	if err != nil {
		return nil, "", err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, "", err
	}
	return signed, "0x" + hex.EncodeToString(raw), nil
}

func (s *Server) submitEVMTransactionObject(call ethCallObject) (string, error) {
	_, rawTx, err := s.signEVMTransactionObject(call, "msc_sendTransaction")
	if err != nil {
		return "", err
	}
	return s.submitEVMRawTransaction(rawTx)
}

func jsonRPCMethodNeedsSubmit(method string) bool {
	switch method {
	case "msc_sendRawTransaction", "msc_sendTransaction", "msc_signTransaction", "dtl_submit":
		return true
	default:
		return false
	}
}

func isDTLCompatAllowedEVMRPCMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "msc_chainid",
		"msc_blocknumber",
		"msc_call",
		"msc_estimategas",
		"msc_getlogs",
		"msc_gettransactionreceipt",
		"msc_getcode",
		"msc_getstorageat":
		return true
	default:
		return false
	}
}

func isRemovedEVMRPCMethod(method string) bool {
	m := strings.ToLower(strings.TrimSpace(method))
	if m == "" {
		return false
	}
	compatSubset := dtlCompatRPCSubsetEnabled()
	if compatSubset && isDTLCompatAllowedEVMRPCMethod(m) {
		return false
	}
	if strings.HasPrefix(m, "eth_") {
		return true
	}
	if strings.HasPrefix(m, "msc_") {
		return true
	}
	if strings.HasPrefix(m, "web3_") ||
		strings.HasPrefix(m, "net_") ||
		strings.HasPrefix(m, "debug_") ||
		strings.HasPrefix(m, "trace_") ||
		strings.HasPrefix(m, "txpool_") ||
		strings.HasPrefix(m, "personal_") ||
		strings.HasPrefix(m, "parity_") ||
		strings.HasPrefix(m, "admin_") ||
		strings.HasPrefix(m, "msc21_") ||
		strings.HasPrefix(m, "msc721_") ||
		strings.HasPrefix(m, "msc1155_") {
		return true
	}
	switch m {
	case "dtl_tomscaddress":
		return true
	default:
		return false
	}
}

func resolveDTLTokenFromLedger(ledger Ledger, tokenRef string) (string, *DTLTokenState, error) {
	ref := strings.TrimSpace(tokenRef)
	if ref == "" {
		return "", nil, errors.New("missing token reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}

	tokenID := normalizeDTLTokenID(ref)
	if tok, ok := ledger.DTL.Tokens[tokenID]; ok && tok != nil {
		return tokenID, tok, nil
	}

	symbol := normalizeDTLSymbol(ref)
	if tokenIDBySymbol, ok := ledger.DTL.SymbolIndex[symbol]; ok {
		tokenIDBySymbol = normalizeDTLTokenID(tokenIDBySymbol)
		if tok, exists := ledger.DTL.Tokens[tokenIDBySymbol]; exists && tok != nil {
			return tokenIDBySymbol, tok, nil
		}
	}

	return "", nil, errors.New("dtl token not found")
}

func resolveDTLMarketFromLedger(ledger Ledger, marketRef string) (string, *DTLLendingMarketState, error) {
	ref := strings.TrimSpace(marketRef)
	if ref == "" {
		return "", nil, errors.New("missing market reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}

	marketID := normalizeDTLMarketID(ref)
	if market, ok := ledger.DTL.LendingMarkets[marketID]; ok && market != nil {
		return marketID, market, nil
	}

	for _, sep := range []string{"|", "/", ":"} {
		if !strings.Contains(ref, sep) {
			continue
		}
		parts := strings.SplitN(ref, sep, 2)
		if len(parts) != 2 {
			continue
		}
		left, _, err := resolveDTLTokenFromLedger(ledger, parts[0])
		if err != nil {
			continue
		}
		right, _, err := resolveDTLTokenFromLedger(ledger, parts[1])
		if err != nil {
			continue
		}
		pairKey := dtlLendingPairKey(left, right)
		candidate := normalizeDTLMarketID(ledger.DTL.LendingIndex[pairKey])
		if candidate == "" {
			continue
		}
		if market, ok := ledger.DTL.LendingMarkets[candidate]; ok && market != nil {
			return candidate, market, nil
		}
	}

	return "", nil, errors.New("dtl lending market not found")
}

func resolveDTLTournamentFromLedger(ledger Ledger, tournamentRef string) (string, *DTLTournamentState, error) {
	ref := strings.TrimSpace(tournamentRef)
	if ref == "" {
		return "", nil, errors.New("missing tournament reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	tournamentID := normalizeDTLTournamentID(ref)
	if t, ok := ledger.DTL.Tournaments[tournamentID]; ok && t != nil {
		return tournamentID, t, nil
	}
	return "", nil, errors.New("dtl tournament not found")
}

func resolveDTLContractFromLedger(ledger Ledger, contractRef string) (string, *DTLContractState, error) {
	ref := strings.TrimSpace(contractRef)
	if ref == "" {
		return "", nil, errors.New("missing contract reference")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	contractID := normalizeDTLContractID(ref)
	if c, ok := ledger.DTL.Contracts[contractID]; ok && c != nil {
		return contractID, c, nil
	}
	return "", nil, errors.New("dtl contract not found")
}

type preparedDTLEthCall struct {
	Ledger     Ledger
	Height     uint64
	ContractID string
	Contract   *DTLContractState
	Method     DTLLogicPackABIMethod
	Args       map[string]string
	Tx         DTLContractCallTx
}

func (s *Server) prepareDTLEthCall(call ethCallObject, stateTag json.RawMessage) (*preparedDTLEthCall, error) {
	if dtlContractRuntimeRemoved() {
		return nil, dtlContractRuntimeRemovedError("msc_call")
	}
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	stateLedger, stateHeight, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, errors.New("invalid block tag")
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	contractRef := strings.TrimSpace(call.To)
	if contractRef == "" {
		return nil, errors.New("msc_call requires `to` for dtl runtime")
	}
	contractID, contract, err := resolveDTLContractByAddressFromLedger(stateLedger, contractRef)
	if err != nil {
		return nil, err
	}
	if contract != nil && contract.LogicPack != nil &&
		contract.LogicPack.Version >= DTLLogicPackVersionV2 &&
		!dtlV2EnabledAtHeight(stateHeight) {
		return nil, errors.New("dtl v2 not active at requested height")
	}
	if contract != nil && strings.TrimSpace(contract.Bytecode) != "" && !dtlBytecodeEnabledAtHeight(stateHeight) {
		return nil, errors.New("dtl bytecode runtime not active at requested height")
	}
	abiMethods := parseDTLContractABIMethods(contract)
	if len(abiMethods) == 0 {
		return nil, errors.New("dtl contract abi unavailable")
	}
	rawInput := strings.TrimSpace(call.Input)
	if rawInput == "" {
		rawInput = strings.TrimSpace(call.Data)
	}
	if rawInput == "" {
		return nil, errors.New("msc_call requires calldata")
	}
	callData, err := decodeHexBytes(rawInput)
	if err != nil {
		return nil, errors.New("invalid call data")
	}
	if len(callData) < 4 {
		return nil, errors.New("calldata too short")
	}
	selector := strings.ToLower(hex.EncodeToString(callData[:4]))
	var matched *DTLLogicPackABIMethod
	for i := range abiMethods {
		if dtlABIMethodSelectorHex(abiMethods[i]) == selector {
			method := abiMethods[i]
			matched = &method
			break
		}
	}
	if matched == nil {
		return nil, errors.New("dtl abi selector not found")
	}
	args, err := decodeDTLEthCallArgs(*matched, callData)
	if err != nil {
		return nil, err
	}
	for _, arg := range matched.Args {
		if canonicalDTLABIType(arg.Type) != "address" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(arg.Name))
		if key == "" {
			continue
		}
		args[key] = normalizeDTLAccount(s.resolveInternalAddress(args[key]))
	}
	caller := normalizeDTLAccount(s.resolveInternalAddress(call.From))
	if caller == "" {
		caller = normalizeDTLAccount("MSC_EVM_CALLER")
	}
	tx := DTLContractCallTx{
		Caller:     caller,
		ContractID: contractID,
		Method:     matched.Name,
		Args:       args,
	}
	return &preparedDTLEthCall{
		Ledger:     stateLedger,
		Height:     stateHeight,
		ContractID: contractID,
		Contract:   contract,
		Method:     *matched,
		Args:       args,
		Tx:         tx,
	}, nil
}

func (s *Server) executeDTLEthCall(call ethCallObject, stateTag json.RawMessage) (string, error) {
	if dtlContractRuntimeRemoved() {
		return "", dtlContractRuntimeRemovedError("msc_call")
	}
	prepared, err := s.prepareDTLEthCall(call, stateTag)
	if err != nil {
		return "", err
	}
	if prepared.Ledger.DTL == nil {
		return "", errors.New("dtl state unavailable")
	}
	cloned := cloneDTLState(prepared.Ledger.DTL)
	if cloned == nil {
		return "", errors.New("dtl state unavailable")
	}
	contract := cloned.Contracts[prepared.ContractID]
	if contract == nil {
		return "", errors.New("dtl contract not found")
	}
	ctx := newDTLLogicExecContext(prepared.Height, ChainID)
	var result dtlLogicCallResult
	if strings.TrimSpace(contract.Bytecode) != "" {
		result, err = executeDTLBytecodeCallWithContext(
			cloned,
			prepared.ContractID,
			contract,
			prepared.Tx,
			ctx,
			true,
		)
	} else {
		result, err = executeDTLLogicPackCallWithContext(
			cloned,
			prepared.ContractID,
			contract,
			prepared.Tx,
			ctx,
			true,
		)
	}
	if err != nil {
		return "", err
	}
	return encodeDTLEthCallResult(prepared.Method, result)
}

func (s *Server) estimateDTLEthCallGas(call ethCallObject, stateTag json.RawMessage) (uint64, error) {
	if dtlContractRuntimeRemoved() {
		return 0, dtlContractRuntimeRemovedError("msc_estimateGas")
	}
	prepared, err := s.prepareDTLEthCall(call, stateTag)
	if err != nil {
		return 0, err
	}
	return estimateDTLCallGas(prepared.Contract, prepared.Method.Name, prepared.Args), nil
}

func (s *Server) dtlTokenInfo(tokenRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	tokenID, token, err := resolveDTLTokenFromLedger(ledger, tokenRef)
	if err != nil {
		return nil, err
	}
	signers := append([]string(nil), token.AuthoritySigners...)
	sort.Strings(signers)
	return map[string]any{
		"token_id":            tokenID,
		"name":                token.Name,
		"symbol":              token.Symbol,
		"decimals":            token.Decimals,
		"max_supply":          encodeRPCQuantityUint64(token.MaxSupply),
		"total_supply":        encodeRPCQuantityUint64(token.TotalSupply),
		"paused":              token.Paused,
		"freeze_enabled":      token.FreezeEnabled,
		"tax_bps":             token.TaxBPS,
		"authority_signers":   signers,
		"authority_threshold": token.AuthorityThreshold,
		"metadata_uri":        token.MetadataURI,
		"block_number":        encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlBalanceOf(tokenRef, account string, stateTag json.RawMessage) (uint64, error) {
	if s == nil || s.Node == nil {
		return 0, errors.New("node unavailable")
	}
	ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("header not found")
	}
	tokenID, _, err := resolveDTLTokenFromLedger(ledger, tokenRef)
	if err != nil {
		return 0, err
	}
	ensureDTLState(&ledger)
	return ledger.DTL.BalanceOf(tokenID, account), nil
}

func (s *Server) dtlTotalSupply(tokenRef string, stateTag json.RawMessage) (uint64, error) {
	if s == nil || s.Node == nil {
		return 0, errors.New("node unavailable")
	}
	ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("header not found")
	}
	_, token, err := resolveDTLTokenFromLedger(ledger, tokenRef)
	if err != nil {
		return 0, err
	}
	return token.TotalSupply, nil
}

func (s *Server) dtlListTokens(account string, stateTag json.RawMessage) ([]map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}

	ensureDTLState(&ledger)
	if ledger.DTL == nil || len(ledger.DTL.Tokens) == 0 {
		return []map[string]any{}, nil
	}

	type tokenRow struct {
		TokenID            string
		Name               string
		Symbol             string
		Decimals           uint8
		MaxSupply          uint64
		TotalSupply        uint64
		Paused             bool
		FreezeEnabled      bool
		TaxBPS             uint16
		AuthoritySigners   []string
		AuthorityThreshold uint16
		MetadataURI        string
		Balance            uint64
	}

	rows := make([]tokenRow, 0, len(ledger.DTL.Tokens))
	normalizedAccount := normalizeDTLAccount(account)
	for tokenID, token := range ledger.DTL.Tokens {
		if token == nil {
			continue
		}
		signers := append([]string(nil), token.AuthoritySigners...)
		sort.Strings(signers)
		balance := uint64(0)
		if normalizedAccount != "" {
			balance = ledger.DTL.BalanceOf(tokenID, normalizedAccount)
		}
		rows = append(rows, tokenRow{
			TokenID:            normalizeDTLTokenID(tokenID),
			Name:               token.Name,
			Symbol:             normalizeDTLSymbol(token.Symbol),
			Decimals:           token.Decimals,
			MaxSupply:          token.MaxSupply,
			TotalSupply:        token.TotalSupply,
			Paused:             token.Paused,
			FreezeEnabled:      token.FreezeEnabled,
			TaxBPS:             token.TaxBPS,
			AuthoritySigners:   signers,
			AuthorityThreshold: token.AuthorityThreshold,
			MetadataURI:        token.MetadataURI,
			Balance:            balance,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Symbol == rows[j].Symbol {
			return rows[i].TokenID < rows[j].TokenID
		}
		return rows[i].Symbol < rows[j].Symbol
	})

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"token_id":            row.TokenID,
			"name":                row.Name,
			"symbol":              row.Symbol,
			"decimals":            row.Decimals,
			"max_supply":          strconv.FormatUint(row.MaxSupply, 10),
			"total_supply":        strconv.FormatUint(row.TotalSupply, 10),
			"paused":              row.Paused,
			"freeze_enabled":      row.FreezeEnabled,
			"tax_bps":             row.TaxBPS,
			"authority_signers":   row.AuthoritySigners,
			"authority_threshold": row.AuthorityThreshold,
			"metadata_uri":        row.MetadataURI,
			"balance":             strconv.FormatUint(row.Balance, 10),
			"block_number":        encodeRPCQuantityUint64(height),
		})
	}
	return out, nil
}

type dtlNFT721OwnerRow struct {
	CollectionID   string
	CollectionName string
	CollectionSym  string
	TokenID        uint64
	Owner          string
	TokenURI       string
	BaseURI        string
}

type dtlNFT1155OwnerRow struct {
	CollectionID   string
	CollectionName string
	CollectionSym  string
	TokenID        uint64
	Owner          string
	Balance        uint64
	BaseURI        string
}

func sanitizeDTLListWindow(offset, limit uint64) (int, int) {
	const maxInt = int(^uint(0) >> 1)
	if offset > uint64(maxInt) {
		offset = uint64(maxInt)
	}
	if limit == 0 {
		limit = dtlNFTListLimitDefault
	}
	if limit > dtlNFTListLimitMax {
		limit = dtlNFTListLimitMax
	}
	return int(offset), int(limit)
}

func parseDTLNFT721OwnerKey(raw string) (string, uint64, bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	collectionID := normalizeDTLCollectionID(parts[0])
	tokenID, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if collectionID == "" || err != nil {
		return "", 0, false
	}
	return collectionID, tokenID, true
}

func parseDTLNFT1155BalanceKey(raw string) (string, uint64, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 3)
	if len(parts) != 3 {
		return "", 0, "", false
	}
	collectionID := normalizeDTLCollectionID(parts[0])
	tokenID, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	account := normalizeDTLAccount(parts[2])
	if collectionID == "" || account == "" || err != nil {
		return "", 0, "", false
	}
	return collectionID, tokenID, account, true
}

func (s *Server) dtlListNFT721ByOwner(account string, offset, limit uint64, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	normalizedOwner := normalizeDTLAccount(account)
	if normalizedOwner == "" {
		return nil, errors.New("invalid account")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return map[string]any{
			"items":        []map[string]any{},
			"total":        0,
			"next_offset":  0,
			"block_number": encodeRPCQuantityUint64(height),
		}, nil
	}

	rows := make([]dtlNFT721OwnerRow, 0)
	for ownerKey, owner := range ledger.DTL.NFT721Owners {
		if normalizeDTLAccount(owner) != normalizedOwner {
			continue
		}
		collectionID, tokenID, ok := parseDTLNFT721OwnerKey(ownerKey)
		if !ok {
			continue
		}
		row := dtlNFT721OwnerRow{
			CollectionID: collectionID,
			TokenID:      tokenID,
			Owner:        normalizedOwner,
			TokenURI:     strings.TrimSpace(ledger.DTL.NFT721TokenURIs[ownerKey]),
		}
		if collection := ledger.DTL.NFT721Collections[collectionID]; collection != nil {
			row.CollectionName = strings.TrimSpace(collection.Name)
			row.CollectionSym = normalizeDTLSymbol(collection.Symbol)
			row.BaseURI = strings.TrimSpace(collection.BaseURI)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CollectionID != rows[j].CollectionID {
			return rows[i].CollectionID < rows[j].CollectionID
		}
		return rows[i].TokenID < rows[j].TokenID
	})

	start, pageSize := sanitizeDTLListWindow(offset, limit)
	if start > len(rows) {
		start = len(rows)
	}
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}

	items := make([]map[string]any, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, map[string]any{
			"collection_id":     row.CollectionID,
			"collection_name":   row.CollectionName,
			"collection_symbol": row.CollectionSym,
			"token_id":          strconv.FormatUint(row.TokenID, 10),
			"owner":             row.Owner,
			"token_uri":         row.TokenURI,
			"base_uri":          row.BaseURI,
		})
	}

	nextOffset := 0
	if end < len(rows) {
		nextOffset = end
	}
	return map[string]any{
		"items":        items,
		"total":        len(rows),
		"next_offset":  nextOffset,
		"block_number": encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlListNFT1155ByOwner(account string, offset, limit uint64, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	normalizedOwner := normalizeDTLAccount(account)
	if normalizedOwner == "" {
		return nil, errors.New("invalid account")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return map[string]any{
			"items":        []map[string]any{},
			"total":        0,
			"next_offset":  0,
			"block_number": encodeRPCQuantityUint64(height),
		}, nil
	}

	rows := make([]dtlNFT1155OwnerRow, 0)
	for balanceKey, balance := range ledger.DTL.NFT1155Balances {
		if balance == 0 {
			continue
		}
		collectionID, tokenID, owner, ok := parseDTLNFT1155BalanceKey(balanceKey)
		if !ok || owner != normalizedOwner {
			continue
		}
		row := dtlNFT1155OwnerRow{
			CollectionID: collectionID,
			TokenID:      tokenID,
			Owner:        normalizedOwner,
			Balance:      balance,
		}
		if collection := ledger.DTL.NFT1155Collections[collectionID]; collection != nil {
			row.CollectionName = strings.TrimSpace(collection.Name)
			row.CollectionSym = normalizeDTLSymbol(collection.Symbol)
			row.BaseURI = strings.TrimSpace(collection.BaseURI)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CollectionID != rows[j].CollectionID {
			return rows[i].CollectionID < rows[j].CollectionID
		}
		return rows[i].TokenID < rows[j].TokenID
	})

	start, pageSize := sanitizeDTLListWindow(offset, limit)
	if start > len(rows) {
		start = len(rows)
	}
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}

	items := make([]map[string]any, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, map[string]any{
			"collection_id":     row.CollectionID,
			"collection_name":   row.CollectionName,
			"collection_symbol": row.CollectionSym,
			"token_id":          strconv.FormatUint(row.TokenID, 10),
			"owner":             row.Owner,
			"balance":           strconv.FormatUint(row.Balance, 10),
			"base_uri":          row.BaseURI,
		})
	}

	nextOffset := 0
	if end < len(rows) {
		nextOffset = end
	}
	return map[string]any{
		"items":        items,
		"total":        len(rows),
		"next_offset":  nextOffset,
		"block_number": encodeRPCQuantityUint64(height),
	}, nil
}

func resolveDTLPoolFromLedger(ledger Ledger, poolRef string) (string, *DTLPoolState, error) {
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return "", nil, errors.New("dtl state unavailable")
	}
	ref := strings.TrimSpace(poolRef)
	if ref == "" {
		return "", nil, errors.New("missing pool reference")
	}

	poolID := normalizeDTLPoolID(ref)
	if poolID != "" {
		if pool := ledger.DTL.Pools[poolID]; pool != nil {
			return poolID, pool, nil
		}
	}

	var left, right string
	if strings.Contains(ref, "/") {
		parts := strings.SplitN(ref, "/", 2)
		left = normalizeDTLTokenID(parts[0])
		right = normalizeDTLTokenID(parts[1])
	} else if strings.Contains(ref, "|") {
		parts := strings.SplitN(ref, "|", 2)
		left = normalizeDTLTokenID(parts[0])
		right = normalizeDTLTokenID(parts[1])
	}
	if left != "" && right != "" {
		if mappedID := normalizeDTLPoolID(ledger.DTL.PoolIndex[dtlPoolPairKey(left, right)]); mappedID != "" {
			if pool := ledger.DTL.Pools[mappedID]; pool != nil {
				return mappedID, pool, nil
			}
		}
	}

	return "", nil, errors.New("dtl pool not found")
}

func (s *Server) dtlPoolInfo(poolRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	poolID, pool, err := resolveDTLPoolFromLedger(ledger, poolRef)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"pool_id":              poolID,
		"token_a":              normalizeDTLTokenID(pool.TokenA),
		"token_b":              normalizeDTLTokenID(pool.TokenB),
		"reserve_a":            encodeRPCQuantityUint64(pool.ReserveA),
		"reserve_b":            encodeRPCQuantityUint64(pool.ReserveB),
		"total_lp_shares":      encodeRPCQuantityUint64(pool.TotalLPShares),
		"fee_bps":              pool.FeeBPS,
		"protocol_fee_bps":     pool.ProtocolFeeBPS,
		"protocol_fee_account": normalizeDTLAccount(pool.ProtocolFeeAccount),
		"pool_vault_account":   dtlPoolVaultAccount(poolID),
		"router_enabled":       dtlRouterEnabled(),
		"router_max_hops":      dtlRouterMaxHops(),
		"router_max_paths":     dtlRouterQuoteMaxPaths(),
		"block_number":         encodeRPCQuantityUint64(height),
		"max_price_impact_bps": dtlRouterMaxPriceImpactBPS(),
		"deadline_max_blocks":  dtlRouterDeadlineMaxBlocks(),
	}, nil
}

func (s *Server) dtlListPools(stateTag json.RawMessage) ([]map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil || len(ledger.DTL.Pools) == 0 {
		return []map[string]any{}, nil
	}

	poolIDs := make([]string, 0, len(ledger.DTL.Pools))
	for poolID := range ledger.DTL.Pools {
		poolIDs = append(poolIDs, normalizeDTLPoolID(poolID))
	}
	sort.Strings(poolIDs)
	out := make([]map[string]any, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		pool := ledger.DTL.Pools[poolID]
		if pool == nil {
			continue
		}
		out = append(out, map[string]any{
			"pool_id":          poolID,
			"token_a":          normalizeDTLTokenID(pool.TokenA),
			"token_b":          normalizeDTLTokenID(pool.TokenB),
			"reserve_a":        encodeRPCQuantityUint64(pool.ReserveA),
			"reserve_b":        encodeRPCQuantityUint64(pool.ReserveB),
			"total_lp_shares":  encodeRPCQuantityUint64(pool.TotalLPShares),
			"fee_bps":          pool.FeeBPS,
			"protocol_fee_bps": pool.ProtocolFeeBPS,
			"block_number":     encodeRPCQuantityUint64(height),
		})
	}
	return out, nil
}

func (s *Server) dtlFarmInfo(farmRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	farmID := normalizeDTLFarmID(farmRef)
	if farmID == "" {
		return nil, errors.New("missing farm reference")
	}
	farm := ledger.DTL.FarmPools[farmID]
	if farm == nil {
		return nil, errors.New("dtl farm not found")
	}
	positionCount := 0
	totalStakedLP := uint64(0)
	for _, pos := range ledger.DTL.FarmPositions {
		if pos == nil || normalizeDTLFarmID(pos.FarmID) != farmID {
			continue
		}
		positionCount++
		if ^uint64(0)-totalStakedLP < pos.StakedLP {
			totalStakedLP = ^uint64(0)
		} else {
			totalStakedLP += pos.StakedLP
		}
	}
	return map[string]any{
		"farm_id":            farmID,
		"pool_id":            normalizeDTLPoolID(farm.PoolID),
		"creator":            normalizeDTLAccount(farm.Creator),
		"multiplier_bps":     farm.MultiplierBPS,
		"created_height":     encodeRPCQuantityUint64(farm.CreatedHeight),
		"last_update_height": encodeRPCQuantityUint64(farm.LastUpdateHeight),
		"active":             farm.Active,
		"total_staked_lp":    encodeRPCQuantityUint64(totalStakedLP),
		"positions":          encodeRPCQuantityInt(positionCount),
		"vault_account":      dtlFarmVaultAccount(farmID),
		"block_number":       encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlPositionFarm(farmRef, account string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	farmID := normalizeDTLFarmID(farmRef)
	if farmID == "" {
		return nil, errors.New("missing farm reference")
	}
	if ledger.DTL.FarmPools[farmID] == nil {
		return nil, errors.New("dtl farm not found")
	}
	account = normalizeDTLAccount(account)
	if account == "" {
		return nil, errors.New("missing account")
	}
	pos := ledger.DTL.FarmPositions[dtlFarmPositionKey(farmID, account)]
	if pos == nil {
		return map[string]any{
			"farm_id":             farmID,
			"account":             account,
			"staked_lp":           encodeRPCQuantityUint64(0),
			"accrued_points":      encodeRPCQuantityUint64(0),
			"last_stake_height":   encodeRPCQuantityUint64(0),
			"last_accrual_height": encodeRPCQuantityUint64(0),
			"block_number":        encodeRPCQuantityUint64(height),
		}, nil
	}
	return map[string]any{
		"farm_id":             farmID,
		"account":             account,
		"staked_lp":           encodeRPCQuantityUint64(pos.StakedLP),
		"accrued_points":      encodeRPCQuantityUint64(pos.AccruedPoints),
		"last_stake_height":   encodeRPCQuantityUint64(pos.LastStakeHeight),
		"last_accrual_height": encodeRPCQuantityUint64(pos.LastAccrualHeight),
		"block_number":        encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlSeasonInfo(seasonRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	seasonID := normalizeDTLSeasonID(seasonRef)
	if seasonID == "" {
		seasonID, _ = dtlResolveActiveSeason(ledger.DTL, height+1)
	}
	if seasonID == "" {
		return nil, errors.New("dtl season not found")
	}
	season := ledger.DTL.Seasons[seasonID]
	if season == nil {
		return nil, errors.New("dtl season not found")
	}
	participants := 0
	claims := 0
	prefix := seasonID + "|"
	for key := range ledger.DTL.SeasonScores {
		if strings.HasPrefix(strings.TrimSpace(key), prefix) {
			participants++
		}
	}
	for key, claimed := range ledger.DTL.SeasonClaims {
		if claimed && strings.HasPrefix(strings.TrimSpace(key), prefix) {
			claims++
		}
	}
	return map[string]any{
		"season_id":              seasonID,
		"creator":                normalizeDTLAccount(season.Creator),
		"reward_token":           normalizeDTLTokenID(season.RewardToken),
		"start_height":           encodeRPCQuantityUint64(season.StartHeight),
		"end_height":             encodeRPCQuantityUint64(season.EndHeight),
		"claim_grace_end_height": encodeRPCQuantityUint64(season.ClaimGraceEndHeight),
		"finalized":              season.Finalized,
		"finalized_height":       encodeRPCQuantityUint64(season.FinalizedHeight),
		"total_score":            encodeRPCQuantityUint64(season.TotalScore),
		"total_claimed":          encodeRPCQuantityUint64(season.TotalClaimed),
		"vault_balance":          encodeRPCQuantityUint64(ledger.DTL.SeasonVaults[seasonID]),
		"participants":           encodeRPCQuantityInt(participants),
		"claims":                 encodeRPCQuantityInt(claims),
		"active":                 !season.Finalized && height >= season.StartHeight && height <= season.EndHeight,
		"block_number":           encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlLeaderboard(seasonRef string, limit int, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	seasonID := normalizeDTLSeasonID(seasonRef)
	if seasonID == "" {
		seasonID, _ = dtlResolveActiveSeason(ledger.DTL, height+1)
	}
	if seasonID == "" || ledger.DTL.Seasons[seasonID] == nil {
		return nil, errors.New("dtl season not found")
	}
	if limit < 1 {
		limit = int(DTLDefaultLeaderboardLimit)
	}
	if limit > int(DTLMaxLeaderboardLimit) {
		limit = int(DTLMaxLeaderboardLimit)
	}
	type entry struct {
		Account string
		Score   uint64
		Claimed bool
	}
	entries := make([]entry, 0, len(ledger.DTL.SeasonScores))
	prefix := seasonID + "|"
	for key, score := range ledger.DTL.SeasonScores {
		if !strings.HasPrefix(strings.TrimSpace(key), prefix) {
			continue
		}
		account := normalizeDTLAccount(strings.TrimPrefix(strings.TrimSpace(key), prefix))
		if account == "" {
			continue
		}
		entries = append(entries, entry{
			Account: account,
			Score:   score,
			Claimed: ledger.DTL.SeasonClaims[key],
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Account < entries[j].Account
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	result := make([]map[string]any, 0, len(entries))
	for i, entry := range entries {
		result = append(result, map[string]any{
			"rank":    encodeRPCQuantityInt(i + 1),
			"account": entry.Account,
			"score":   encodeRPCQuantityUint64(entry.Score),
			"claimed": entry.Claimed,
		})
	}
	return map[string]any{
		"season_id":     seasonID,
		"limit":         encodeRPCQuantityInt(limit),
		"total_entries": encodeRPCQuantityInt(len(result)),
		"entries":       result,
		"block_number":  encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlRouteQuote(tokenIn, tokenOut string, amountIn uint64, maxHops int, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	ensureDTLState(&ledger)
	if ledger.DTL == nil {
		return nil, errors.New("dtl state unavailable")
	}
	quote, err := dtlBestPoolSwapRouteQuote(ledger.DTL, tokenIn, tokenOut, amountIn, maxHops)
	if err != nil {
		return nil, err
	}

	totalFeeBPS := uint64(0)
	hopFees := make([]map[string]any, 0, len(quote.Hops))
	hops := make([]map[string]any, 0, len(quote.Hops))
	for i, hop := range quote.Hops {
		totalFeeBPS += uint64(hop.FeeBPS)
		hopFees = append(hopFees, map[string]any{
			"hop":     i + 1,
			"pool_id": hop.PoolID,
			"fee_bps": hop.FeeBPS,
		})
		hops = append(hops, map[string]any{
			"hop":        i + 1,
			"pool_id":    hop.PoolID,
			"token_in":   hop.TokenIn,
			"token_out":  hop.TokenOut,
			"amount_in":  encodeRPCQuantityUint64(hop.AmountIn),
			"amount_out": encodeRPCQuantityUint64(hop.AmountOut),
			"fee_bps":    hop.FeeBPS,
		})
	}
	if totalFeeBPS > DTLMaxTaxBPS {
		totalFeeBPS = DTLMaxTaxBPS
	}

	return map[string]any{
		"token_in":            quote.TokenIn,
		"token_out":           quote.TokenOut,
		"amount_in":           encodeRPCQuantityUint64(quote.AmountIn),
		"best_path":           quote.Path,
		"expected_amount_out": encodeRPCQuantityUint64(quote.AmountOut),
		"price_impact_bps":    quote.PriceImpactBPS,
		"hops":                hops,
		"fee_breakdown": map[string]any{
			"total_fee_bps_estimate": totalFeeBPS,
			"hops":                   hopFees,
		},
		"valid_until_height": encodeRPCQuantityUint64(height + dtlRouterDeadlineMaxBlocks()),
		"block_number":       encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlMarketInfo(marketRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	marketID, market, err := resolveDTLMarketFromLedger(ledger, marketRef)
	if err != nil {
		return nil, err
	}
	collateralSymbol := ""
	if tok := ledger.DTL.Tokens[normalizeDTLTokenID(market.CollateralTokenID)]; tok != nil {
		collateralSymbol = normalizeDTLSymbol(tok.Symbol)
	}
	debtSymbol := ""
	if tok := ledger.DTL.Tokens[normalizeDTLTokenID(market.DebtTokenID)]; tok != nil {
		debtSymbol = normalizeDTLSymbol(tok.Symbol)
	}
	return map[string]any{
		"market_id":             marketID,
		"collateral_token_id":   normalizeDTLTokenID(market.CollateralTokenID),
		"collateral_symbol":     collateralSymbol,
		"debt_token_id":         normalizeDTLTokenID(market.DebtTokenID),
		"debt_symbol":           debtSymbol,
		"collateral_factor_bps": market.CollateralFactorBPS,
		"liquidation_bonus_bps": market.LiquidationBonusBPS,
		"total_collateral":      encodeRPCQuantityUint64(market.TotalCollateral),
		"total_debt":            encodeRPCQuantityUint64(market.TotalDebt),
		"vault_account":         dtlLendingVaultAccount(marketID),
		"block_number":          encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlPositionOf(marketRef, account string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	marketID, market, err := resolveDTLMarketFromLedger(ledger, marketRef)
	if err != nil {
		return nil, err
	}
	normalizedAccount := normalizeDTLAccount(account)
	if normalizedAccount == "" {
		return nil, errors.New("invalid account")
	}
	key := dtlLendingPositionKey(marketID, normalizedAccount)
	position := ledger.DTL.LendingPositions[key]
	collateral := uint64(0)
	debt := uint64(0)
	if position != nil {
		collateral = position.Collateral
		debt = position.Debt
	}
	maxDebt, err := dtlLendingMaxDebt(collateral, market.CollateralFactorBPS)
	if err != nil {
		return nil, err
	}
	isHealthy, err := dtlLendingIsHealthy(collateral, debt, market.CollateralFactorBPS)
	if err != nil {
		return nil, err
	}
	healthBPS := uint64(0)
	if debt == 0 {
		healthBPS = ^uint64(0)
	} else {
		healthBPS = (maxDebt * DTLMaxTaxBPS) / debt
	}
	return map[string]any{
		"market_id":       marketID,
		"account":         normalizedAccount,
		"collateral":      encodeRPCQuantityUint64(collateral),
		"debt":            encodeRPCQuantityUint64(debt),
		"max_debt":        encodeRPCQuantityUint64(maxDebt),
		"health_bps":      encodeRPCQuantityUint64(healthBPS),
		"is_healthy":      isHealthy,
		"is_liquidatable": debt > 0 && !isHealthy,
		"block_number":    encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlTournamentInfo(tournamentRef string, stateTag json.RawMessage) (map[string]any, error) {
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	tournamentID, tournament, err := resolveDTLTournamentFromLedger(ledger, tournamentRef)
	if err != nil {
		return nil, err
	}
	players := append([]string(nil), tournament.Players...)
	revealers := make([]string, 0, len(tournament.Reveals))
	for player, secret := range tournament.Reveals {
		if strings.TrimSpace(secret) != "" {
			revealers = append(revealers, normalizeDTLAccount(player))
		}
	}
	sort.Strings(revealers)
	tokenSymbol := ""
	if tok := ledger.DTL.Tokens[normalizeDTLTokenID(tournament.TokenID)]; tok != nil {
		tokenSymbol = normalizeDTLSymbol(tok.Symbol)
	}
	return map[string]any{
		"tournament_id":   tournamentID,
		"token_id":        normalizeDTLTokenID(tournament.TokenID),
		"token_symbol":    tokenSymbol,
		"creator":         normalizeDTLAccount(tournament.Creator),
		"entry_fee":       encodeRPCQuantityUint64(tournament.EntryFee),
		"max_players":     tournament.MaxPlayers,
		"join_deadline":   encodeRPCQuantityUint64(tournament.JoinDeadline),
		"reveal_deadline": encodeRPCQuantityUint64(tournament.RevealDeadline),
		"players":         players,
		"player_count":    encodeRPCQuantityUint64(uint64(len(players))),
		"revealed_count":  encodeRPCQuantityUint64(uint64(len(revealers))),
		"revealers":       revealers,
		"pot":             encodeRPCQuantityUint64(tournament.Pot),
		"settled":         tournament.Settled,
		"winner":          normalizeDTLAccount(tournament.Winner),
		"vault_account":   dtlTournamentVaultAccount(tournamentID),
		"block_number":    encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlContractInfo(contractRef string, stateTag json.RawMessage) (map[string]any, error) {
	if dtlContractRuntimeRemoved() {
		return nil, dtlContractRuntimeRemovedError("dtl_contractInfo")
	}
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	contractID, contract, err := resolveDTLContractFromLedger(ledger, contractRef)
	if err != nil {
		return nil, err
	}
	methodNames := make([]string, 0, len(contract.Methods))
	methods := make([]map[string]any, 0, len(contract.Methods))
	logicABI := make([]map[string]any, 0)
	logicStorage := make([]map[string]any, 0)
	logicMethods := make([]map[string]any, 0)
	logicLimits := map[string]any{}
	var logicPack any
	if contract.LogicPack != nil {
		logicABI = make([]map[string]any, 0, len(contract.LogicPack.ABI))
		for _, abiMethod := range contract.LogicPack.ABI {
			args := make([]map[string]any, 0, len(abiMethod.Args))
			for _, arg := range abiMethod.Args {
				args = append(args, map[string]any{
					"name": strings.ToLower(strings.TrimSpace(arg.Name)),
					"type": strings.ToLower(strings.TrimSpace(arg.Type)),
				})
			}
			logicABI = append(logicABI, map[string]any{
				"name":    strings.ToLower(strings.TrimSpace(abiMethod.Name)),
				"args":    args,
				"returns": append([]string(nil), abiMethod.Returns...),
			})
		}
		sort.Slice(logicABI, func(i, j int) bool {
			left, _ := logicABI[i]["name"].(string)
			right, _ := logicABI[j]["name"].(string)
			return left < right
		})

		logicStorage = make([]map[string]any, 0, len(contract.LogicPack.Storage))
		for _, field := range contract.LogicPack.Storage {
			logicStorage = append(logicStorage, map[string]any{
				"key":  strings.ToLower(strings.TrimSpace(field.Key)),
				"type": strings.ToLower(strings.TrimSpace(field.Type)),
				"init": strings.TrimSpace(field.Init),
			})
		}
		sort.Slice(logicStorage, func(i, j int) bool {
			left, _ := logicStorage[i]["key"].(string)
			right, _ := logicStorage[j]["key"].(string)
			return left < right
		})

		logicMethods = make([]map[string]any, 0, len(contract.LogicPack.Methods))
		for _, method := range contract.LogicPack.Methods {
			ops := make([]map[string]any, 0, len(method.Ops))
			for _, op := range method.Ops {
				ops = append(ops, map[string]any{
					"op":         strings.ToUpper(strings.TrimSpace(op.Op)),
					"dest":       strings.ToLower(strings.TrimSpace(op.Dest)),
					"a":          strings.ToLower(strings.TrimSpace(op.A)),
					"b":          strings.ToLower(strings.TrimSpace(op.B)),
					"src":        strings.ToLower(strings.TrimSpace(op.Src)),
					"cond":       strings.ToLower(strings.TrimSpace(op.Cond)),
					"key":        strings.ToLower(strings.TrimSpace(op.Key)),
					"arg":        strings.ToLower(strings.TrimSpace(op.Arg)),
					"token_id":   normalizeDTLTokenID(op.TokenID),
					"to_arg":     strings.ToLower(strings.TrimSpace(op.ToArg)),
					"amount_arg": strings.ToLower(strings.TrimSpace(op.AmountArg)),
					"from":       strings.ToLower(strings.TrimSpace(op.From)),
					"message":    strings.TrimSpace(op.Message),
					"target":     op.Target,
				})
			}
			logicMethods = append(logicMethods, map[string]any{
				"name":      strings.ToLower(strings.TrimSpace(method.Name)),
				"max_steps": encodeRPCQuantityUint64(uint64(method.MaxSteps)),
				"op_count":  encodeRPCQuantityUint64(uint64(len(method.Ops))),
				"ops":       ops,
			})
			methods = append(methods, map[string]any{
				"name":      strings.ToLower(strings.TrimSpace(method.Name)),
				"max_steps": encodeRPCQuantityUint64(uint64(method.MaxSteps)),
				"op_count":  encodeRPCQuantityUint64(uint64(len(method.Ops))),
			})
		}
		sort.Slice(logicMethods, func(i, j int) bool {
			left, _ := logicMethods[i]["name"].(string)
			right, _ := logicMethods[j]["name"].(string)
			return left < right
		})
		sort.Slice(methods, func(i, j int) bool {
			left, _ := methods[i]["name"].(string)
			right, _ := methods[j]["name"].(string)
			return left < right
		})
		logicLimits = map[string]any{
			"max_reads":           encodeRPCQuantityUint64(uint64(contract.LogicPack.Limits.MaxReads)),
			"max_writes":          encodeRPCQuantityUint64(uint64(contract.LogicPack.Limits.MaxWrites)),
			"max_token_transfers": encodeRPCQuantityUint64(uint64(contract.LogicPack.Limits.MaxTokenTransfers)),
		}
		logicPack = map[string]any{
			"version": encodeRPCQuantityUint64(uint64(contract.LogicPack.Version)),
			"name":    strings.ToLower(strings.TrimSpace(contract.LogicPack.Name)),
			"abi":     logicABI,
			"storage": logicStorage,
			"methods": logicMethods,
			"limits":  logicLimits,
		}
	} else {
		for name := range contract.Methods {
			methodNames = append(methodNames, normalizeDTLContractMethodName(name))
		}
		sort.Strings(methodNames)
		for _, name := range methodNames {
			method := contract.Methods[name]
			if method == nil {
				continue
			}
			methods = append(methods, map[string]any{
				"name":     normalizeDTLContractMethodName(method.Name),
				"op":       strings.ToUpper(strings.TrimSpace(string(method.Op))),
				"key":      strings.TrimSpace(method.Key),
				"arg":      strings.TrimSpace(method.Arg),
				"to_arg":   strings.TrimSpace(method.ToArg),
				"token_id": normalizeDTLTokenID(method.TokenID),
				"from":     strings.ToLower(strings.TrimSpace(method.From)),
			})
		}
	}
	return map[string]any{
		"contract_id":     contractID,
		"creator":         normalizeDTLAccount(contract.Creator),
		"name":            strings.TrimSpace(contract.Name),
		"lang":            strings.ToLower(strings.TrimSpace(contract.Lang)),
		"version":         contract.Version,
		"logic_mode":      contract.LogicPack != nil,
		"logic_hash":      strings.ToLower(strings.TrimSpace(contract.LogicHash)),
		"logic_pack_hash": strings.ToLower(strings.TrimSpace(contract.LogicHash)),
		"logic_version": func() string {
			if contract.LogicPack == nil {
				return "0x0"
			}
			return encodeRPCQuantityUint64(uint64(contract.LogicPack.Version))
		}(),
		"logic_pack":    logicPack,
		"logic_abi":     logicABI,
		"logic_storage": logicStorage,
		"logic_limits":  logicLimits,
		"logic_methods": logicMethods,
		"paused":        contract.Paused,
		"method_count":  encodeRPCQuantityUint64(uint64(len(methods))),
		"storage_count": encodeRPCQuantityUint64(uint64(len(contract.Storage))),
		"vault_account": dtlContractVaultAccount(contractID),
		"methods":       methods,
		"block_number":  encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) dtlContractStorage(contractRef, storageKey string, stateTag json.RawMessage) (map[string]any, error) {
	if dtlContractRuntimeRemoved() {
		return nil, dtlContractRuntimeRemovedError("dtl_contractStorage")
	}
	if s == nil || s.Node == nil {
		return nil, errors.New("node unavailable")
	}
	ledger, height, ok, err := s.resolveLedgerByTag(stateTag)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("header not found")
	}
	contractID, contract, err := resolveDTLContractFromLedger(ledger, contractRef)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(storageKey)
	if key != "" {
		return map[string]any{
			"contract_id":  contractID,
			"key":          key,
			"value":        strings.TrimSpace(contract.Storage[key]),
			"block_number": encodeRPCQuantityUint64(height),
		}, nil
	}
	keys := make([]string, 0, len(contract.Storage))
	for k := range contract.Storage {
		keys = append(keys, strings.TrimSpace(k))
	}
	sort.Strings(keys)
	items := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		items = append(items, map[string]any{
			"key":   k,
			"value": strings.TrimSpace(contract.Storage[k]),
		})
	}
	return map[string]any{
		"contract_id":  contractID,
		"items":        items,
		"block_number": encodeRPCQuantityUint64(height),
	}, nil
}

func (s *Server) submitDTLTransactionObject(raw json.RawMessage) (string, error) {
	if s == nil || s.Node == nil {
		return "", errors.New("node unavailable")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", errors.New("missing transaction object")
	}

	var tx Transaction
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&tx); err != nil {
		return "", errors.New("invalid transaction object")
	}

	tx.Type = TxDTL
	tx.Coin = normalizeCoin(tx.Coin)
	if strings.TrimSpace(tx.Coin) == "" {
		tx.Coin = CoinSymbol
	}
	if strings.TrimSpace(tx.ChainID) == "" {
		tx.ChainID = ChainID
	}
	if tx.Expiry <= 0 {
		tx.Expiry = time.Now().Add(2 * time.Minute).Unix()
	}
	normalizeIncomingTx(&tx)
	if err := validateTransactionShape(tx); err != nil {
		return "", err
	}
	if tx.ID == "" {
		tx.ID = ComputeTxID(tx)
	}

	if ok, reason := s.Node.ReceiveTransactionWithReason(tx); !ok {
		if reason == "duplicate transaction" {
			return tx.ID, nil
		}
		if reason == "" {
			reason = "transaction rejected"
		}
		return "", errors.New(reason)
	}
	return tx.ID, nil
}

func authorizeJSONRPCRequest(r *http.Request, submit bool) bool {
	if submit {
		return authorizedSubmit(r)
	}
	if !ConfigRPCRequireAuthForReadEndpoints {
		return true
	}

	if r == nil {
		return false
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return false
	}
	if matchesBearerToken(header, apiToken) {
		return true
	}
	if matchesBearerToken(header, apiReadToken) {
		return true
	}
	if strings.HasPrefix(header, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		return isValidAuthToken(token)
	}
	return false
}

func (s *Server) handleJSONRPCMethod(req jsonRPCRequest) jsonRPCResponse {
	id := parseRPCID(req.ID)
	resp := jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
	}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		resp.Error = rpcError(-32600, "invalid request: missing method")
		return resp
	}
	if isRemovedEVMRPCMethod(method) {
		resp.Error = rpcError(-32000, "evm/vm removed permanently")
		return resp
	}

	params, err := rpcParamsAsArray(req.Params)
	if err != nil {
		resp.Error = rpcError(-32602, "invalid params")
		return resp
	}

	switch method {
	case "dtl_chainId":
		resp.Result = ChainID
	case "dtl_tokenInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token reference")
			return resp
		}
		tokenRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.dtlTokenInfo(tokenRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_balanceOf":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing token reference or account")
			return resp
		}
		tokenRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		account, err := parseRPCStringParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		bal, err := s.dtlBalanceOf(tokenRef, account, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = encodeRPCQuantityUint64(bal)
	case "dtl_totalSupply":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token reference")
			return resp
		}
		tokenRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		total, err := s.dtlTotalSupply(tokenRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = encodeRPCQuantityUint64(total)
	case "dtl_listTokens":
		account := ""
		stateTag := json.RawMessage(nil)
		if len(params) > 0 {
			if err := json.Unmarshal(params[0], &account); err != nil {
				stateTag = params[0]
			} else if len(params) > 1 {
				stateTag = params[1]
			}
		}
		tokens, err := s.dtlListTokens(account, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = tokens
	case "dtl_listNFT721ByOwner":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing owner account")
			return resp
		}
		account, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		offset := uint64(0)
		limit := dtlNFTListLimitDefault
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			if parsedOffset, err := parseRPCQuantity(params[1]); err == nil {
				offset = parsedOffset
				if len(params) > 2 {
					if parsedLimit, err := parseRPCQuantity(params[2]); err == nil {
						limit = parsedLimit
						if len(params) > 3 {
							stateTag = params[3]
						}
					} else {
						stateTag = params[2]
					}
				}
			} else {
				stateTag = params[1]
			}
		}
		items, err := s.dtlListNFT721ByOwner(account, offset, limit, stateTag)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "invalid account") {
				resp.Error = rpcError(-32602, err.Error())
				return resp
			}
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = items
	case "dtl_listNFT1155ByOwner":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing owner account")
			return resp
		}
		account, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		offset := uint64(0)
		limit := dtlNFTListLimitDefault
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			if parsedOffset, err := parseRPCQuantity(params[1]); err == nil {
				offset = parsedOffset
				if len(params) > 2 {
					if parsedLimit, err := parseRPCQuantity(params[2]); err == nil {
						limit = parsedLimit
						if len(params) > 3 {
							stateTag = params[3]
						}
					} else {
						stateTag = params[2]
					}
				}
			} else {
				stateTag = params[1]
			}
		}
		items, err := s.dtlListNFT1155ByOwner(account, offset, limit, stateTag)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "invalid account") {
				resp.Error = rpcError(-32602, err.Error())
				return resp
			}
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = items
	case "dtl_poolInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing pool reference")
			return resp
		}
		poolRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.dtlPoolInfo(poolRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_listPools":
		stateTag := json.RawMessage(nil)
		if len(params) > 0 {
			stateTag = params[0]
		}
		pools, err := s.dtlListPools(stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = pools
	case "dtl_farmInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing farm reference")
			return resp
		}
		farmRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.dtlFarmInfo(farmRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_positionFarm":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing farm reference/account")
			return resp
		}
		farmRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		account, err := parseRPCStringParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		info, err := s.dtlPositionFarm(farmRef, account, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_seasonInfo":
		seasonRef := ""
		stateTag := json.RawMessage(nil)
		if len(params) > 0 {
			if err := json.Unmarshal(params[0], &seasonRef); err != nil {
				stateTag = params[0]
			} else if len(params) > 1 {
				stateTag = params[1]
			}
		}
		info, err := s.dtlSeasonInfo(seasonRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_leaderboard":
		seasonRef := ""
		limit := int(DTLDefaultLeaderboardLimit)
		stateTag := json.RawMessage(nil)
		if len(params) > 0 {
			if err := json.Unmarshal(params[0], &seasonRef); err != nil {
				resp.Error = rpcError(-32602, "invalid season reference")
				return resp
			}
		}
		if len(params) > 1 {
			if parsedLimit, err := parseRPCQuantity(params[1]); err == nil {
				if parsedLimit == 0 {
					resp.Error = rpcError(-32602, "invalid limit")
					return resp
				}
				limit = int(parsedLimit)
				if len(params) > 2 {
					stateTag = params[2]
				}
			} else {
				stateTag = params[1]
			}
		}
		info, err := s.dtlLeaderboard(seasonRef, limit, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_routeQuote":
		if len(params) < 3 {
			resp.Error = rpcError(-32602, "missing token_in/token_out/amount_in")
			return resp
		}
		tokenIn, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		tokenOut, err := parseRPCStringParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		amountIn, err := parseRPCQuantity(params[2])
		if err != nil || amountIn == 0 {
			resp.Error = rpcError(-32602, "invalid amount_in")
			return resp
		}
		maxHops := dtlRouterMaxHops()
		stateTag := json.RawMessage(nil)
		if len(params) > 3 {
			if parsedHops, err := parseRPCQuantity(params[3]); err == nil && parsedHops > 0 {
				parsed := int(parsedHops)
				if parsed < maxHops {
					maxHops = parsed
				}
				if len(params) > 4 {
					stateTag = params[4]
				}
			} else {
				stateTag = params[3]
			}
		}
		quote, err := s.dtlRouteQuote(tokenIn, tokenOut, amountIn, maxHops, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = quote
	case "dtl_marketInfo", "marketInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing market reference")
			return resp
		}
		marketRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.dtlMarketInfo(marketRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_positionOf", "positionOf":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing market reference or account")
			return resp
		}
		marketRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		account, err := parseRPCStringParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		info, err := s.dtlPositionOf(marketRef, account, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_tournamentInfo", "tournamentInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tournament reference")
			return resp
		}
		tournamentRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.dtlTournamentInfo(tournamentRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_contractInfo", "contractInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing contract reference")
			return resp
		}
		contractRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.dtlContractInfo(contractRef, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_contractStorage", "contractStorage":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing contract reference")
			return resp
		}
		contractRef, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		storageKey := ""
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			if err := json.Unmarshal(params[1], &storageKey); err != nil {
				stateTag = params[1]
			} else if len(params) > 2 {
				stateTag = params[2]
			}
		}
		info, err := s.dtlContractStorage(contractRef, storageKey, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "dtl_submit":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing transaction object")
			return resp
		}
		txID, err := s.submitDTLTransactionObject(params[0])
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = txID
	case "web3_clientVersion":
		resp.Result = "MSC-EVM/compat-1.0"
	case "web3_sha3":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing data")
			return resp
		}
		var data string
		if err := json.Unmarshal(params[0], &data); err != nil {
			resp.Error = rpcError(-32602, "invalid data")
			return resp
		}
		raw, err := decodeHexBytes(data)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid hex data")
			return resp
		}
		h := ethcrypto.Keccak256(raw)
		resp.Result = "0x" + strings.ToLower(hex.EncodeToString(h))
	case "net_version":
		resp.Result = chainIDBigInt().String()
	case "net_listening":
		resp.Result = true
	case "net_peerCount":
		peerCount := 0
		if s != nil && s.Node != nil {
			runtime := s.Node.runtimeStatusSnapshot()
			peerCount = runtime.Peers
		}
		resp.Result = encodeRPCQuantityInt(peerCount)
	case "wallet_switchEthereumChain", "wallet_addEthereumChain":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing chain params")
			return resp
		}
		reqChainID, err := parseWalletChainIDParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		if !isMSCOnlyChainID(reqChainID) {
			resp.Error = rpcError(-32000, fmt.Sprintf("only %s chain (%d) is supported", mscCoinFullName, mscOnlyChainID))
			return resp
		}
		resp.Result = true
	case "wallet_watchAsset":
		resp.Result = true
	case "wallet_getPermissions", "wallet_requestPermissions":
		resp.Result = []any{}
	case "wallet_revokePermissions":
		resp.Result = true
	case "msc_getEvmAddress", "msc_registerAddressAlias":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing MSC wallet address")
			return resp
		}
		rawAddr, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		internal, alias, err := s.registerRPCAddressAlias(rawAddr)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = map[string]any{
			"msc_address": displayAddress(internal),
			"evm_address": alias,
		}
	case "msc_resolveAddress":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing address")
			return resp
		}
		rawAddr, err := parseRPCStringParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		internal := s.resolveInternalAddress(rawAddr)
		alias := toEVMHexAddress(internal)
		resp.Result = map[string]any{
			"input":       rawAddr,
			"msc_address": displayAddress(internal),
			"evm_address": alias,
		}
	case "msc21_tokenInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.msc21TokenInfo(tokenAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "msc21_balanceOf":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing token/owner address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		ownerAddr, err := parseRPCAddressParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		bal, err := s.msc21BalanceOf(tokenAddr, ownerAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = encodeRPCQuantityBig(bal)
	case "msc21_allowance":
		if len(params) < 3 {
			resp.Error = rpcError(-32602, "missing token/owner/spender address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		ownerAddr, err := parseRPCAddressParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		spenderAddr, err := parseRPCAddressParam(params[2])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 3 {
			stateTag = params[3]
		}
		allowance, err := s.msc21Allowance(tokenAddr, ownerAddr, spenderAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = encodeRPCQuantityBig(allowance)
	case "msc21_isToken":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		_, err = s.msc21TokenInfo(tokenAddr, stateTag)
		resp.Result = err == nil
	case "msc721_tokenInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.msc721TokenInfo(tokenAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "msc721_balanceOf":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing token/owner address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		ownerAddr, err := parseRPCAddressParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		bal, err := s.msc721BalanceOf(tokenAddr, ownerAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = encodeRPCQuantityBig(bal)
	case "msc721_ownerOf":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing token address or token id")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		tokenID, err := parseRPCBigIntParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		owner, err := s.msc721OwnerOf(tokenAddr, tokenID, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = owner
	case "msc721_tokenURI":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing token address or token id")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		tokenID, err := parseRPCBigIntParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		uri, err := s.msc721TokenURI(tokenAddr, tokenID, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = uri
	case "msc721_isToken":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		ok, err := s.msc721IsToken(tokenAddr, stateTag)
		resp.Result = err == nil && ok
	case "msc1155_tokenInfo":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		info, err := s.msc1155TokenInfo(tokenAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = info
	case "msc1155_balanceOf":
		if len(params) < 3 {
			resp.Error = rpcError(-32602, "missing token/owner/tokenId")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		ownerAddr, err := parseRPCAddressParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		tokenID, err := parseRPCBigIntParam(params[2])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 3 {
			stateTag = params[3]
		}
		bal, err := s.msc1155BalanceOf(tokenAddr, ownerAddr, tokenID, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = encodeRPCQuantityBig(bal)
	case "msc1155_uri":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing token address or token id")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		tokenID, err := parseRPCBigIntParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		uri, err := s.msc1155URI(tokenAddr, tokenID, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = uri
	case "msc1155_isApprovedForAll":
		if len(params) < 3 {
			resp.Error = rpcError(-32602, "missing token/owner/operator address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		ownerAddr, err := parseRPCAddressParam(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		operatorAddr, err := parseRPCAddressParam(params[2])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 3 {
			stateTag = params[3]
		}
		approved, err := s.msc1155IsApprovedForAll(tokenAddr, ownerAddr, operatorAddr, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = approved
	case "msc1155_isToken":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing token address")
			return resp
		}
		tokenAddr, err := parseRPCAddressParam(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		ok, err := s.msc1155IsToken(tokenAddr, stateTag)
		resp.Result = err == nil && ok
	case "msc_chainId":
		resp.Result = encodeRPCQuantityBig(chainIDBigInt())
	case "msc_protocolVersion":
		resp.Result = "0x1"
	case "msc_mining":
		resp.Result = false
	case "msc_hashrate":
		resp.Result = "0x0"
	case "msc_coinbase":
		resp.Result = toEVMHexAddress(TREASURY_ADDRESS)
	case "msc_gasPrice":
		resp.Result = encodeRPCQuantityInt(s.currentEVMMarketFee(currentEVMDefaultGasLimit()))
	case "msc_blobBaseFee":
		resp.Result = "0x0"
	case "msc_maxPriorityFeePerGas":
		resp.Result = "0x0"
	case "msc_blockNumber":
		height := uint64(0)
		if s != nil && s.Node != nil && s.Node.Blockchain != nil {
			height = s.Node.Blockchain.Height()
		}
		resp.Result = encodeRPCQuantityUint64(height)
	case "msc_syncing":
		resp.Result = s.rpcSyncingResult()
	case "msc_accounts", "msc_requestAccounts":
		_, devAddr, ok, err := loadDevEVMPrivateKey()
		if err != nil {
			resp.Error = rpcError(-32000, "invalid MSC_EVM_DEV_PRIVKEY")
			return resp
		}
		if !ok {
			resp.Result = []string{}
			return resp
		}
		resp.Result = []string{devAddr.Hex()}
	case "msc_sign":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing address or data")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		var dataHex string
		if err := json.Unmarshal(params[1], &dataHex); err != nil {
			resp.Error = rpcError(-32602, "invalid data")
			return resp
		}
		raw, err := decodeHexBytes(dataHex)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid data")
			return resp
		}
		// Keep behavior aligned with widely used clients by signing text-hash form.
		sig, err := signHashWithDevSigner(addr, accounts.TextHash(raw))
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = sig
	case "personal_sign":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing data or address")
			return resp
		}
		var first string
		if err := json.Unmarshal(params[0], &first); err != nil {
			resp.Error = rpcError(-32602, "invalid params")
			return resp
		}
		var second string
		if err := json.Unmarshal(params[1], &second); err != nil {
			resp.Error = rpcError(-32602, "invalid params")
			return resp
		}
		// Accept both orders: [data, address] and [address, data].
		addr := second
		dataHex := first
		if common.IsHexAddress(first) && !common.IsHexAddress(second) {
			addr = first
			dataHex = second
		}
		raw, err := decodeHexBytes(dataHex)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid data")
			return resp
		}
		sig, err := signHashWithDevSigner(addr, accounts.TextHash(raw))
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = sig
	case "msc_signTypedData_v4":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing address or typed data")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		var typedDataJSON string
		if err := json.Unmarshal(params[1], &typedDataJSON); err != nil {
			resp.Error = rpcError(-32602, "invalid typed data")
			return resp
		}
		var typedData apitypes.TypedData
		if err := json.Unmarshal([]byte(typedDataJSON), &typedData); err != nil {
			resp.Error = rpcError(-32602, "invalid typed data")
			return resp
		}
		hash, _, err := apitypes.TypedDataAndHash(typedData)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		sig, err := signHashWithDevSigner(addr, hash)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = sig
	case "msc_getBalance":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing address")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		internal := s.resolveInternalAddress(addr)
		bal := getBalance(ledger, CoinSymbol, internal)
		resp.Result = encodeRPCNativeAmount(bal)
	case "msc_getTransactionCount":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing address")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		stateTag := json.RawMessage(nil)
		isPendingTag := false
		if len(params) > 1 {
			stateTag = params[1]
			var tag string
			if err := json.Unmarshal(params[1], &tag); err == nil {
				isPendingTag = strings.EqualFold(strings.TrimSpace(tag), "pending")
			}
		}
		internal := s.resolveInternalAddress(addr)
		if isPendingTag {
			nonce := s.rpcNextNonceForAddress(internal, true)
			resp.Result = encodeRPCQuantityInt(nonce)
			return resp
		}
		ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		nonce := getNonce(ledger, internal)
		resp.Result = encodeRPCQuantityInt(nonce)
	case "msc_getCode":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing address")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		if dtlCompatRPCSubsetEnabled() {
			contractID, contract, err := resolveDTLContractByAddressFromLedger(ledger, addr)
			if err != nil {
				resp.Result = "0x"
				return resp
			}
			result := map[string]any{
				"code": dtlPseudoCodeEnvelope(contractID, contract),
			}
			if contract != nil {
				result["runtime_mode"] = resolveDTLContractRuntimeMode(contract)
				if format := normalizeDTLBytecodeFormat(contract.BytecodeFormat); format != "" {
					result["bytecode_format"] = format
				}
				if hash := strings.TrimSpace(contract.BytecodeHash); hash != "" {
					result["bytecode_hash"] = normalizeHexHash(hash)
				}
				if compiler := strings.TrimSpace(contract.Compiler); compiler != "" {
					result["compiler"] = compiler
				}
				if sourceHash := strings.TrimSpace(contract.SourceHash); sourceHash != "" {
					result["source_hash"] = sourceHash
				}
				if b, err := decodeDTLBytecodeHex(contract.Bytecode); err == nil {
					result["bytecode_size"] = encodeRPCQuantityUint64(uint64(len(b)))
				}
			}
			resp.Result = result
			return resp
		}
		resp.Result = evmCodeByAddress(ledger, addr)
	case "msc_getStorageAt":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing address or slot")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		var slot string
		if err := json.Unmarshal(params[1], &slot); err != nil {
			resp.Error = rpcError(-32602, "invalid slot")
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		if dtlCompatRPCSubsetEnabled() {
			_, contract, err := resolveDTLContractByAddressFromLedger(ledger, addr)
			if err != nil {
				resp.Result = zeroEVMWordHex
				return resp
			}
			resp.Result = dtlStorageBySlot(contract, slot)
			return resp
		}
		resp.Result = evmStorageByAddress(ledger, addr, slot)
	case "msc_getProof":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing address")
			return resp
		}
		var addr string
		if err := json.Unmarshal(params[0], &addr); err != nil {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		if !common.IsHexAddress(addr) {
			resp.Error = rpcError(-32602, "invalid address")
			return resp
		}
		keys := []string{}
		if len(params) > 1 {
			_ = json.Unmarshal(params[1], &keys)
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 2 {
			stateTag = params[2]
		}
		ledger, _, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		internal := s.resolveInternalAddress(addr)
		balance := 0
		nonce := 0
		codeHex := "0x"
		balance = getBalance(ledger, CoinSymbol, internal)
		nonce = getNonce(ledger, internal)
		codeHex = evmCodeByAddress(ledger, addr)
		codeHash := emptyCodeHash
		if codeHex != "0x" {
			codeBytes, _ := decodeHexBytes(codeHex)
			codeHash = common.BytesToHash(ethcrypto.Keccak256(codeBytes)).Hex()
		}
		storageProof := make([]any, 0, len(keys))
		for _, key := range keys {
			slot := normalizeEVMStorageSlotKey(key)
			val := zeroEVMWordHex
			val = evmStorageByAddress(ledger, addr, slot)
			storageProof = append(storageProof, map[string]any{
				"key":   slot,
				"value": val,
				"proof": []string{},
			})
		}
		resp.Result = map[string]any{
			"address":      common.HexToAddress(addr).Hex(),
			"accountProof": []string{},
			"balance":      encodeRPCNativeAmount(balance),
			"codeHash":     normalizeHexHash(codeHash),
			"nonce":        encodeRPCQuantityInt(nonce),
			"storageHash":  emptyStateRoot,
			"storageProof": storageProof,
		}
	case "msc_call":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing call object")
			return resp
		}
		var call ethCallObject
		if err := json.Unmarshal(params[0], &call); err != nil {
			resp.Error = rpcError(-32602, "invalid call object")
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		if dtlCompatRPCSubsetEnabled() {
			out, err := s.executeDTLEthCall(call, stateTag)
			if err != nil {
				resp.Error = rpcError(-32000, err.Error())
				return resp
			}
			resp.Result = normalizeHexData(out)
			return resp
		}
		stateLedger, stateHeight, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		rawData := strings.TrimSpace(call.Data)
		code := ""
		input := strings.TrimSpace(call.Input)
		toAddr := strings.TrimSpace(call.To)
		if toAddr != "" {
			code = evmCodeByAddress(stateLedger, toAddr)
			// Ethereum-style msc_call uses data as calldata.
			if input == "" {
				input = rawData
			}
		}
		if code == "" || code == "0x" {
			// Custom fallback: use inline bytecode from `data`.
			code = rawData
		}
		if code == "" || code == "0x" {
			resp.Error = rpcError(-32602, "msc_call requires bytecode in data or deployed code at `to`")
			return resp
		}
		gas := uint64(0)
		if strings.TrimSpace(call.Gas) != "" {
			if parsed, err := parseRPCQuantity(json.RawMessage(`"` + call.Gas + `"`)); err == nil {
				gas = parsed
			}
		}
		amount := 0
		if strings.TrimSpace(call.Value) != "" {
			if parsedAmount, err := parseRPCNativeAmount(call.Value); err == nil {
				amount = parsedAmount
			}
		}
		tx := Transaction{
			Type:        TxEVM,
			From:        strings.TrimSpace(call.From),
			To:          strings.TrimSpace(call.To),
			Amount:      amount,
			EVMCode:     code,
			EVMInput:    input,
			EVMGasLimit: gas,
		}
		if tx.From == "" {
			tx.From = "MSC_EVM_CALLER"
		}
		height := int(stateHeight)
		execLedger := &stateLedger
		result, err := runCustomEVMSandbox(tx, height, execLedger)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = normalizeHexData(result.OutputHex)
	case "msc_estimateGas":
		if len(params) < 1 {
			resp.Result = encodeRPCQuantityUint64(currentEVMDefaultGasLimit())
			return resp
		}
		var call ethCallObject
		if err := json.Unmarshal(params[0], &call); err != nil {
			resp.Error = rpcError(-32602, "invalid call object")
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		if dtlCompatRPCSubsetEnabled() {
			estimate, err := s.estimateDTLEthCallGas(call, stateTag)
			if err != nil {
				resp.Error = rpcError(-32000, err.Error())
				return resp
			}
			resp.Result = encodeRPCQuantityUint64(estimate)
			return resp
		}
		stateLedger, stateHeight, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		rawData := strings.TrimSpace(call.Data)
		code := ""
		input := strings.TrimSpace(call.Input)
		toAddr := strings.TrimSpace(call.To)
		if toAddr != "" {
			code = evmCodeByAddress(stateLedger, toAddr)
			if input == "" {
				input = rawData
			}
		}
		if code == "" || code == "0x" {
			code = rawData
		}
		if code == "" || code == "0x" {
			resp.Error = rpcError(-32602, "msc_estimateGas requires bytecode in data or deployed code at `to`")
			return resp
		}
		gas := uint64(0)
		if strings.TrimSpace(call.Gas) != "" {
			if parsed, err := parseRPCQuantity(json.RawMessage(`"` + call.Gas + `"`)); err == nil {
				gas = parsed
			}
		}
		amount := 0
		if strings.TrimSpace(call.Value) != "" {
			if parsedAmount, err := parseRPCNativeAmount(call.Value); err == nil {
				amount = parsedAmount
			}
		}
		tx := Transaction{
			Type:        TxEVM,
			From:        strings.TrimSpace(call.From),
			To:          strings.TrimSpace(call.To),
			Amount:      amount,
			EVMCode:     code,
			EVMInput:    input,
			EVMGasLimit: gas,
		}
		if tx.From == "" {
			tx.From = "MSC_EVM_CALLER"
		}
		height := int(stateHeight)
		execLedger := &stateLedger
		result, err := runCustomEVMSandbox(tx, height, execLedger)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		estimate := result.GasUsed
		if estimate == 0 {
			estimate = txGasLimit(tx)
		}
		resp.Result = encodeRPCQuantityUint64(estimate)
	case "msc_createAccessList":
		if len(params) < 1 {
			resp.Result = map[string]any{
				"accessList": []any{},
				"gasUsed":    encodeRPCQuantityUint64(currentEVMDefaultGasLimit()),
			}
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		stateLedger, stateHeight, ok, err := s.resolveLedgerByTag(stateTag)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		var call ethCallObject
		if err := json.Unmarshal(params[0], &call); err != nil {
			resp.Error = rpcError(-32602, "invalid call object")
			return resp
		}
		rawData := strings.TrimSpace(call.Data)
		code := ""
		input := strings.TrimSpace(call.Input)
		toAddr := strings.TrimSpace(call.To)
		if toAddr != "" {
			code = evmCodeByAddress(stateLedger, toAddr)
			if input == "" {
				input = rawData
			}
		}
		if code == "" || code == "0x" {
			code = rawData
		}
		if code == "" || code == "0x" {
			resp.Error = rpcError(-32602, "msc_createAccessList requires bytecode in data or deployed code at `to`")
			return resp
		}
		gas := uint64(0)
		if strings.TrimSpace(call.Gas) != "" {
			if parsed, err := parseRPCQuantity(json.RawMessage(`"` + call.Gas + `"`)); err == nil {
				gas = parsed
			}
		}
		amount := 0
		if strings.TrimSpace(call.Value) != "" {
			if parsedAmount, err := parseRPCNativeAmount(call.Value); err == nil {
				amount = parsedAmount
			}
		}
		tx := Transaction{
			Type:        TxEVM,
			From:        strings.TrimSpace(call.From),
			To:          strings.TrimSpace(call.To),
			Amount:      amount,
			EVMCode:     code,
			EVMInput:    input,
			EVMGasLimit: gas,
		}
		if tx.From == "" {
			tx.From = "MSC_EVM_CALLER"
		}
		height := int(stateHeight)
		execLedger := &stateLedger
		result, err := runCustomEVMSandbox(tx, height, execLedger)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		estimate := result.GasUsed
		if estimate == 0 {
			estimate = txGasLimit(tx)
		}
		resp.Result = map[string]any{
			"accessList": []any{},
			"gasUsed":    encodeRPCQuantityUint64(estimate),
		}
	case "msc_getBlockByNumber":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block tag")
			return resp
		}
		full := false
		if len(params) > 1 {
			if b, err := parseRPCBool(params[1]); err == nil {
				full = b
			}
		}
		if isRPCPendingTag(params[0]) {
			resp.Result = s.buildPendingEthBlockObject(full)
			return resp
		}
		block, ok, err := s.resolveBlockByTag(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Result = nil
			return resp
		}
		resp.Result = s.buildEthBlockObject(block, full)
	case "msc_getBlockByHash":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid block hash")
			return resp
		}
		full := false
		if len(params) > 1 {
			if b, err := parseRPCBool(params[1]); err == nil {
				full = b
			}
		}
		block, ok := findBlockByHash(s.rpcBlockSnapshot(), hash)
		if !ok {
			resp.Result = nil
			return resp
		}
		resp.Result = s.buildEthBlockObject(block, full)
	case "msc_getBlockTransactionCountByNumber":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block tag")
			return resp
		}
		if isRPCPendingTag(params[0]) {
			resp.Result = encodeRPCQuantityInt(len(s.currentPendingTxLocations()))
			return resp
		}
		block, ok, err := s.resolveBlockByTag(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Result = nil
			return resp
		}
		resp.Result = encodeRPCQuantityInt(len(block.Transactions))
	case "msc_getBlockTransactionCountByHash":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid block hash")
			return resp
		}
		block, ok := findBlockByHash(s.rpcBlockSnapshot(), hash)
		if !ok {
			resp.Result = nil
			return resp
		}
		resp.Result = encodeRPCQuantityInt(len(block.Transactions))
	case "msc_getTransactionByHash":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tx hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		if !loc.Found {
			resp.Result = nil
			return resp
		}
		resp.Result = s.buildEthTxObject(loc)
	case "msc_getRawTransactionByHash":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tx hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		if !loc.Found {
			resp.Result = nil
			return resp
		}
		raw := rpcRawTxHex(loc.Tx)
		if raw == "" {
			resp.Result = nil
			return resp
		}
		resp.Result = raw
	case "msc_getTransactionByBlockHashAndIndex":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing block hash or tx index")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid block hash")
			return resp
		}
		idx, err := parseRPCQuantity(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid tx index")
			return resp
		}
		block, ok := findBlockByHash(s.rpcBlockSnapshot(), hash)
		txIndex := int(idx)
		if !ok || txIndex >= len(block.Transactions) {
			resp.Result = nil
			return resp
		}
		loc := rpcTxLocation{
			Tx:         block.Transactions[txIndex],
			Block:      block,
			Found:      true,
			BlockFound: true,
			TxIndex:    txIndex,
		}
		resp.Result = s.buildEthTxObject(loc)
	case "msc_getRawTransactionByBlockHashAndIndex":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing block hash or tx index")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid block hash")
			return resp
		}
		idx, err := parseRPCQuantity(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid tx index")
			return resp
		}
		block, ok := findBlockByHash(s.rpcBlockSnapshot(), hash)
		txIndex := int(idx)
		if !ok || txIndex >= len(block.Transactions) {
			resp.Result = nil
			return resp
		}
		raw := rpcRawTxHex(block.Transactions[txIndex])
		if raw == "" {
			resp.Result = nil
			return resp
		}
		resp.Result = raw
	case "msc_getTransactionByBlockNumberAndIndex":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing block tag or tx index")
			return resp
		}
		if isRPCPendingTag(params[0]) {
			idx, err := parseRPCQuantity(params[1])
			if err != nil {
				resp.Error = rpcError(-32602, "invalid tx index")
				return resp
			}
			pending := s.currentPendingTxLocations()
			txIndex := int(idx)
			if txIndex >= len(pending) {
				resp.Result = nil
				return resp
			}
			resp.Result = s.buildEthTxObject(pending[txIndex])
			return resp
		}
		block, ok, err := s.resolveBlockByTag(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Result = nil
			return resp
		}
		idx, err := parseRPCQuantity(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid tx index")
			return resp
		}
		txIndex := int(idx)
		if txIndex >= len(block.Transactions) {
			resp.Result = nil
			return resp
		}
		loc := rpcTxLocation{
			Tx:         block.Transactions[txIndex],
			Block:      block,
			Found:      true,
			BlockFound: true,
			TxIndex:    txIndex,
		}
		resp.Result = s.buildEthTxObject(loc)
	case "msc_getRawTransactionByBlockNumberAndIndex":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing block tag or tx index")
			return resp
		}
		if isRPCPendingTag(params[0]) {
			idx, err := parseRPCQuantity(params[1])
			if err != nil {
				resp.Error = rpcError(-32602, "invalid tx index")
				return resp
			}
			pending := s.currentPendingTxLocations()
			txIndex := int(idx)
			if txIndex >= len(pending) {
				resp.Result = nil
				return resp
			}
			raw := rpcRawTxHex(pending[txIndex].Tx)
			if raw == "" {
				resp.Result = nil
				return resp
			}
			resp.Result = raw
			return resp
		}
		block, ok, err := s.resolveBlockByTag(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Result = nil
			return resp
		}
		idx, err := parseRPCQuantity(params[1])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid tx index")
			return resp
		}
		txIndex := int(idx)
		if txIndex >= len(block.Transactions) {
			resp.Result = nil
			return resp
		}
		raw := rpcRawTxHex(block.Transactions[txIndex])
		if raw == "" {
			resp.Result = nil
			return resp
		}
		resp.Result = raw
	case "msc_getTransactionReceipt":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tx hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		resp.Result = s.buildEthReceipt(loc)
	case "msc_getBlockReceipts":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block reference")
			return resp
		}
		if isRPCPendingTag(params[0]) {
			resp.Result = []any{}
			return resp
		}
		var block Block
		var ok bool
		var tagOrHash string
		if err := json.Unmarshal(params[0], &tagOrHash); err == nil {
			ref := strings.TrimSpace(tagOrHash)
			if common.IsHexHash(ref) {
				if b, found := findBlockByHash(s.rpcBlockSnapshot(), ref); found {
					block, ok = b, true
				}
			}
		}
		if !ok {
			var err error
			block, ok, err = s.resolveBlockByTag(params[0])
			if err != nil {
				resp.Error = rpcError(-32602, "invalid block reference")
				return resp
			}
		}
		if !ok {
			resp.Result = nil
			return resp
		}
		out := make([]any, 0, len(block.Transactions))
		for idx, tx := range block.Transactions {
			loc := rpcTxLocation{
				Tx:         tx,
				Block:      block,
				Found:      true,
				BlockFound: true,
				TxIndex:    idx,
			}
			out = append(out, s.buildEthReceipt(loc))
		}
		resp.Result = out
	case "msc_pendingTransactions":
		pending := s.currentPendingTxLocations()
		out := make([]any, 0, len(pending))
		for _, loc := range pending {
			out = append(out, s.buildEthTxObject(loc))
		}
		resp.Result = out
	case "msc_getUncleByBlockHashAndIndex", "msc_getUncleByBlockNumberAndIndex":
		resp.Result = nil
	case "msc_getUncleCountByBlockHash", "msc_getUncleCountByBlockNumber":
		resp.Result = "0x0"
	case "msc_getWork":
		resp.Result = []string{
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
		}
	case "msc_submitWork", "msc_submitHashrate":
		resp.Result = false
	case "msc_getLogs":
		filter := rpcLogFilter{}
		if len(params) > 0 {
			parsed, err := parseRPCLogFilter(params[0])
			if err != nil {
				resp.Error = rpcError(-32602, "invalid log filter")
				return resp
			}
			filter = parsed
		}
		logs, err := s.collectRPCLogs(filter)
		if err != nil {
			resp.Error = rpcError(-32602, "invalid log filter")
			return resp
		}
		resp.Result = logs
	case "msc_newFilter":
		filter := rpcLogFilter{}
		if len(params) > 0 {
			parsed, err := parseRPCLogFilter(params[0])
			if err != nil {
				resp.Error = rpcError(-32602, "invalid log filter")
				return resp
			}
			filter = parsed
		}
		f := &rpcCompatFilter{
			Kind:      "logs",
			LogFilter: filter,
			Seen:      make(map[string]struct{}),
		}
		if _, err := s.filterChanges(f); err != nil {
			resp.Error = rpcError(-32602, "invalid log filter")
			return resp
		}
		resp.Result = setRPCFilter(f)
	case "msc_newBlockFilter":
		f := &rpcCompatFilter{
			Kind: "blocks",
			Seen: make(map[string]struct{}),
		}
		for _, h := range s.currentBlockHashes() {
			f.Seen[strings.ToLower(h)] = struct{}{}
		}
		resp.Result = setRPCFilter(f)
	case "msc_newPendingTransactionFilter":
		f := &rpcCompatFilter{
			Kind: "pending",
			Seen: make(map[string]struct{}),
		}
		for _, h := range s.currentPendingTxHashes() {
			f.Seen[strings.ToLower(h)] = struct{}{}
		}
		resp.Result = setRPCFilter(f)
	case "msc_getFilterChanges":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing filter id")
			return resp
		}
		filterID, err := parseRPCFilterID(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid filter id")
			return resp
		}
		filter, ok := getRPCFilter(filterID)
		if !ok {
			resp.Error = rpcError(-32000, "filter not found")
			return resp
		}
		out, err := s.filterChanges(filter)
		if err != nil {
			resp.Error = rpcError(-32000, "filter not found")
			return resp
		}
		resp.Result = out
	case "msc_getFilterLogs":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing filter id")
			return resp
		}
		filterID, err := parseRPCFilterID(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid filter id")
			return resp
		}
		filter, ok := getRPCFilter(filterID)
		if !ok {
			resp.Error = rpcError(-32000, "filter not found")
			return resp
		}
		switch filter.Kind {
		case "logs":
			logs, err := s.collectRPCLogs(filter.LogFilter)
			if err != nil {
				resp.Error = rpcError(-32602, "invalid log filter")
				return resp
			}
			resp.Result = logs
		case "blocks", "pending":
			resp.Result = []any{}
		default:
			resp.Error = rpcError(-32000, "filter not found")
		}
	case "msc_uninstallFilter":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing filter id")
			return resp
		}
		filterID, err := parseRPCFilterID(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid filter id")
			return resp
		}
		resp.Result = deleteRPCFilter(filterID)
	case "msc_feeHistory":
		blockCount := uint64(1)
		if len(params) > 0 {
			if parsed, err := parseRPCQuantity(params[0]); err == nil && parsed > 0 {
				blockCount = parsed
			}
		}
		if blockCount > 1024 {
			blockCount = 1024
		}

		newest := uint64(0)
		if s != nil && s.Node != nil && s.Node.Blockchain != nil {
			newest = s.Node.Blockchain.Height()
		}
		if len(params) > 1 {
			if block, found, err := s.resolveBlockByTag(params[1]); err == nil && found {
				newest = block.ID
			}
		}

		available := newest + 1
		if blockCount > available {
			blockCount = available
		}
		if blockCount == 0 {
			blockCount = 1
		}
		oldest := newest - blockCount + 1

		baseFees := make([]string, blockCount+1)
		gasRatios := make([]float64, blockCount)
		rewards := make([][]string, blockCount)

		blocks := s.rpcBlockSnapshot()
		for i := uint64(0); i < blockCount; i++ {
			height := oldest + i
			baseFees[i] = encodeRPCQuantityInt(s.evmBaseFeeForBlock(height))
			rewards[i] = []string{"0x0"}
			gasRatios[i] = 0

			block, found := findBlockByHeight(blocks, height)
			if !found {
				continue
			}
			var gasUsed uint64
			for txIndex, tx := range block.Transactions {
				loc := rpcTxLocation{
					Tx:         tx,
					Block:      block,
					Found:      true,
					BlockFound: true,
					TxIndex:    txIndex,
				}
				gasUsed += s.gasUsedForTxLocation(loc)
			}
			maxGas := currentEVMMaxGasLimit()
			if maxGas > 0 {
				gasRatios[i] = float64(gasUsed) / float64(maxGas)
				if gasRatios[i] < 0 {
					gasRatios[i] = 0
				}
				if gasRatios[i] > 1 {
					gasRatios[i] = 1
				}
			}
		}
		baseFees[blockCount] = encodeRPCQuantityInt(s.evmNextBaseFeeAfterBlock(newest))
		resp.Result = map[string]any{
			"oldestBlock":   encodeRPCQuantityUint64(oldest),
			"baseFeePerGas": baseFees,
			"gasUsedRatio":  gasRatios,
			"reward":        rewards,
		}
	case "debug_traceTransaction":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tx hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		if !loc.Found {
			resp.Error = rpcError(-32000, "transaction not found")
			return resp
		}
		resp.Result = s.buildDebugTraceForTx(loc)
	case "debug_traceCall":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing call object")
			return resp
		}
		var call ethCallObject
		if err := json.Unmarshal(params[0], &call); err != nil {
			resp.Error = rpcError(-32602, "invalid call object")
			return resp
		}
		stateTag := json.RawMessage(nil)
		if len(params) > 1 {
			stateTag = params[1]
		}
		trace, err := s.buildDebugTraceForCall(call, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = trace
	case "debug_traceBlockByNumber":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block tag")
			return resp
		}
		block, ok, err := s.resolveBlockByTag(params[0])
		if err != nil {
			resp.Error = rpcError(-32602, "invalid block tag")
			return resp
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		resp.Result = s.buildDebugTraceBlock(block)
	case "debug_traceBlockByHash":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid block hash")
			return resp
		}
		block, ok := findBlockByHash(s.rpcBlockSnapshot(), hash)
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		resp.Result = s.buildDebugTraceBlock(block)
	case "trace_transaction":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tx hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		if !loc.Found || !loc.BlockFound {
			resp.Error = rpcError(-32000, "transaction not found")
			return resp
		}
		trace := s.buildDebugTraceForTx(loc)
		receipt := s.buildEthReceipt(loc)
		resp.Result = []any{buildReplayTraceEntry(loc, trace, receipt)}
	case "trace_call":
		call, stateTag, selected, err := parseTraceCallParams(params)
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		trace, err := s.buildDebugTraceForCall(call, stateTag)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		loc, err := buildTraceCallLocation(call)
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		out := map[string]any{}
		if selected["trace"] {
			out["trace"] = []any{buildReplayTraceEntry(loc, trace, nil)}
		}
		if selected["vmtrace"] {
			codeHex := strings.TrimSpace(call.Data)
			toAddr := strings.TrimSpace(call.To)
			if toAddr != "" {
				if stateLedger, _, ok, err := s.resolveLedgerByTag(stateTag); err == nil && ok {
					codeHex = evmCodeByAddress(stateLedger, toAddr)
					if codeHex == "0x" {
						codeHex = strings.TrimSpace(call.Data)
					}
				}
			}
			out["vmTrace"] = map[string]any{
				"code": stripHexPrefix(normalizeHexData(codeHex)),
				"ops":  []any{},
			}
		}
		if selected["statediff"] {
			out["stateDiff"] = map[string]any{}
		}
		resp.Result = out
	case "trace_block":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing block tag")
			return resp
		}
		var block Block
		var ok bool
		if raw := bytes.TrimSpace(params[0]); len(raw) > 0 && raw[0] == '"' {
			var tag string
			if err := json.Unmarshal(params[0], &tag); err == nil {
				trim := strings.TrimSpace(tag)
				if len(stripHexPrefix(trim)) == 64 {
					block, ok = findBlockByHash(s.rpcBlockSnapshot(), trim)
				}
			}
		}
		if !ok {
			var err error
			block, ok, err = s.resolveBlockByTag(params[0])
			if err != nil {
				resp.Error = rpcError(-32602, "invalid block tag")
				return resp
			}
		}
		if !ok {
			resp.Error = rpcError(-32000, "header not found")
			return resp
		}
		out := make([]any, 0, len(block.Transactions))
		for idx, tx := range block.Transactions {
			loc := rpcTxLocation{
				Tx:         tx,
				Block:      block,
				Found:      true,
				BlockFound: true,
				TxIndex:    idx,
			}
			trace := s.buildDebugTraceForTx(loc)
			receipt := s.buildEthReceipt(loc)
			out = append(out, buildReplayTraceEntry(loc, trace, receipt))
		}
		resp.Result = out
	case "trace_filter":
		filter, err := s.parseTraceFilterRequest(params)
		if err != nil {
			resp.Error = rpcError(-32602, err.Error())
			return resp
		}
		blocks := s.rpcBlockSnapshot()
		if len(blocks) == 0 {
			resp.Result = []any{}
			return resp
		}
		sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID < blocks[j].ID })
		matches := make([]any, 0, 32)
		for _, block := range blocks {
			if block.ID < filter.FromBlock || block.ID > filter.ToBlock {
				continue
			}
			for idx, tx := range block.Transactions {
				loc := rpcTxLocation{
					Tx:         tx,
					Block:      block,
					Found:      true,
					BlockFound: true,
					TxIndex:    idx,
				}
				trace := s.buildDebugTraceForTx(loc)
				receipt := s.buildEthReceipt(loc)
				entry := buildReplayTraceEntry(loc, trace, receipt)
				action, _ := entry["action"].(map[string]any)
				fromAddr, _ := action["from"].(string)
				toAddr, _ := action["to"].(string)
				if !traceAddressAllowed(filter.FromAddress, strings.ToLower(fromAddr)) {
					continue
				}
				if len(filter.ToAddress) > 0 {
					if strings.TrimSpace(toAddr) == "" {
						continue
					}
					if !traceAddressAllowed(filter.ToAddress, strings.ToLower(toAddr)) {
						continue
					}
				}
				matches = append(matches, entry)
			}
		}
		if filter.After >= len(matches) {
			resp.Result = []any{}
			return resp
		}
		start := filter.After
		if start < 0 {
			start = 0
		}
		end := len(matches)
		if filter.Count > 0 && start+filter.Count < end {
			end = start + filter.Count
		}
		resp.Result = matches[start:end]
	case "trace_get":
		if len(params) < 2 {
			resp.Error = rpcError(-32602, "missing params")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		if !loc.Found || !loc.BlockFound {
			resp.Error = rpcError(-32000, "transaction not found")
			return resp
		}
		var traceAddress []uint64
		if err := json.Unmarshal(params[1], &traceAddress); err != nil {
			resp.Error = rpcError(-32602, "invalid trace address")
			return resp
		}
		if len(traceAddress) > 0 {
			resp.Result = nil
			return resp
		}
		trace := s.buildDebugTraceForTx(loc)
		receipt := s.buildEthReceipt(loc)
		resp.Result = buildReplayTraceEntry(loc, trace, receipt)
	case "trace_replayTransaction":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing tx hash")
			return resp
		}
		var hash string
		if err := json.Unmarshal(params[0], &hash); err != nil {
			resp.Error = rpcError(-32602, "invalid tx hash")
			return resp
		}
		loc := s.findTxByHash(hash)
		if !loc.Found || !loc.BlockFound {
			resp.Error = rpcError(-32000, "transaction not found")
			return resp
		}
		selected := parseReplayTraceTypes(params)
		trace := s.buildDebugTraceForTx(loc)
		receipt := s.buildEthReceipt(loc)
		out := map[string]any{}
		if selected["trace"] {
			out["trace"] = []any{buildReplayTraceEntry(loc, trace, receipt)}
		}
		if selected["vmtrace"] {
			out["vmTrace"] = map[string]any{
				"code": stripHexPrefix(normalizeHexData(loc.Tx.EVMCode)),
				"ops":  []any{},
			}
		}
		if selected["statediff"] {
			out["stateDiff"] = map[string]any{}
		}
		resp.Result = out
	case "msc_sendRawTransaction":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing raw transaction")
			return resp
		}
		var rawTx string
		if err := json.Unmarshal(params[0], &rawTx); err != nil {
			resp.Error = rpcError(-32602, "invalid raw transaction")
			return resp
		}
		txHash, err := s.submitEVMRawTransaction(rawTx)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = normalizeHexHash(txHash)
	case "msc_sendTransaction":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing transaction object")
			return resp
		}
		var call ethCallObject
		if err := json.Unmarshal(params[0], &call); err != nil {
			resp.Error = rpcError(-32602, "invalid transaction object")
			return resp
		}
		txHash, err := s.submitEVMTransactionObject(call)
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = normalizeHexHash(txHash)
	case "msc_signTransaction":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing transaction object")
			return resp
		}
		var call ethCallObject
		if err := json.Unmarshal(params[0], &call); err != nil {
			resp.Error = rpcError(-32602, "invalid transaction object")
			return resp
		}
		signedTx, rawTx, err := s.signEVMTransactionObject(call, "msc_signTransaction")
		if err != nil {
			resp.Error = rpcError(-32000, err.Error())
			return resp
		}
		resp.Result = map[string]any{
			"raw": normalizeHexData(rawTx),
			"tx":  buildEthTxObjectFromSigned(signedTx, call.From),
		}
	default:
		resp.Error = rpcError(-32601, "method not found")
	}

	return resp
}

func (s *Server) handleJSONRPCWSRequest(
	_ *websocket.Conn,
	r *http.Request,
	req jsonRPCRequest,
	subMu *sync.Mutex,
	subs map[string]*wsRPCSubscription,
) jsonRPCResponse {
	id := parseRPCID(req.ID)
	method := strings.TrimSpace(req.Method)
	if method == "" {
		return jsonRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      id,
			Error:   rpcError(-32600, "invalid request: missing method"),
		}
	}
	if isRemovedEVMRPCMethod(method) {
		return jsonRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      id,
			Error:   rpcError(-32000, "evm/vm removed permanently"),
		}
	}

	if !authorizeJSONRPCRequest(r, jsonRPCMethodNeedsSubmit(method)) {
		return jsonRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      id,
			Error:   rpcError(-32000, "unauthorized"),
		}
	}

	switch strings.ToLower(method) {
	case "msc_subscribe":
		opts, err := parseWSSubscribeParams(req.Params)
		if err != nil {
			return jsonRPCResponse{
				JSONRPC: jsonRPCVersion,
				ID:      id,
				Error:   rpcError(-32602, "invalid subscription params"),
			}
		}
		switch opts.Kind {
		case "newheads", "logs", "newpendingtransactions", "syncing":
		default:
			return jsonRPCResponse{
				JSONRPC: jsonRPCVersion,
				ID:      id,
				Error:   rpcError(-32602, "unsupported subscription"),
			}
		}
		sub := &wsRPCSubscription{
			ID:            nextRPCFilterID(),
			Kind:          opts.Kind,
			LogFilter:     opts.LogFilter,
			Seen:          make(map[string]struct{}),
			PendingFullTx: opts.PendingFullTx,
		}
		if opts.Kind == "newheads" {
			if s != nil && s.Node != nil && s.Node.Blockchain != nil {
				sub.LastHeight = s.Node.Blockchain.Height()
			}
		}
		if opts.Kind == "newpendingtransactions" {
			for _, loc := range s.currentPendingTxLocations() {
				hash := strings.ToLower(strings.TrimSpace(rpcTxHash(loc.Tx)))
				sub.Seen[hash] = struct{}{}
			}
		}
		if opts.Kind == "logs" {
			initial, err := s.collectRPCLogs(sub.LogFilter)
			if err != nil {
				return jsonRPCResponse{
					JSONRPC: jsonRPCVersion,
					ID:      id,
					Error:   rpcError(-32602, "invalid subscription params"),
				}
			}
			for _, item := range initial {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				sub.Seen[rpcLogEntryIdentity(entry)] = struct{}{}
			}
		}
		if opts.Kind == "syncing" {
			sub.LastSyncState = rpcSyncingStateSignature(s.rpcSyncingResult())
		}
		subMu.Lock()
		subs[sub.ID] = sub
		subMu.Unlock()
		return jsonRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      id,
			Result:  sub.ID,
		}
	case "msc_unsubscribe":
		subID, err := parseWSUnsubscribeParams(req.Params)
		if err != nil {
			return jsonRPCResponse{
				JSONRPC: jsonRPCVersion,
				ID:      id,
				Error:   rpcError(-32602, "invalid subscription id"),
			}
		}
		removed := false
		subMu.Lock()
		if _, ok := subs[subID]; ok {
			delete(subs, subID)
			removed = true
		}
		subMu.Unlock()
		return jsonRPCResponse{
			JSONRPC: jsonRPCVersion,
			ID:      id,
			Result:  removed,
		}
	default:
		return s.handleJSONRPCMethod(req)
	}
}

func (s *Server) handleJSONRPCWS(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}
	conn, err := jsonRPCWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "websocket upgrade failed", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	writeCh := make(chan any, 64)
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() {
		closeOnce.Do(func() {
			close(done)
			close(writeCh)
		})
	}

	go func() {
		for msg := range writeCh {
			if err := conn.WriteJSON(msg); err != nil {
				closeDone()
				return
			}
		}
	}()

	subs := make(map[string]*wsRPCSubscription)
	var subMu sync.Mutex

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				subMu.Lock()
				notifications := s.collectWSSubscriptionNotifications(subs)
				subMu.Unlock()
				for _, n := range notifications {
					select {
					case <-done:
						return
					case writeCh <- n:
					}
				}
			}
		}
	}()

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			closeDone()
			return
		}
		trimmed := bytes.TrimSpace(payload)
		if len(trimmed) == 0 {
			continue
		}

		if trimmed[0] == '[' {
			var batch []jsonRPCRequest
			if err := json.Unmarshal(trimmed, &batch); err != nil {
				resp := jsonRPCResponse{
					JSONRPC: jsonRPCVersion,
					Error:   rpcError(-32600, "invalid request"),
				}
				select {
				case <-done:
					return
				case writeCh <- resp:
				}
				continue
			}
			out := make([]jsonRPCResponse, 0, len(batch))
			for _, req := range batch {
				out = append(out, s.handleJSONRPCWSRequest(conn, r, req, &subMu, subs))
			}
			select {
			case <-done:
				return
			case writeCh <- out:
			}
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(trimmed, &req); err != nil {
			resp := jsonRPCResponse{
				JSONRPC: jsonRPCVersion,
				Error:   rpcError(-32600, "invalid request"),
			}
			select {
			case <-done:
				return
			case writeCh <- resp:
			}
			continue
		}
		resp := s.handleJSONRPCWSRequest(conn, r, req, &subMu, subs)
		select {
		case <-done:
			return
		case writeCh <- resp:
		}
	}
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || s.Node == nil {
		http.Error(w, "node unavailable", http.StatusServiceUnavailable)
		return
	}

	rawBody, err := ioReadAllLimited(r.Body, MaxTxRequestBodyBytes)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	trimmed := bytes.TrimSpace(rawBody)
	if len(trimmed) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	var requests []jsonRPCRequest
	isBatch := trimmed[0] == '['
	if isBatch {
		if err := json.Unmarshal(trimmed, &requests); err != nil {
			http.Error(w, "invalid json-rpc batch", http.StatusBadRequest)
			return
		}
		if len(requests) == 0 {
			http.Error(w, "empty json-rpc batch", http.StatusBadRequest)
			return
		}
	} else {
		var req jsonRPCRequest
		if err := json.Unmarshal(trimmed, &req); err != nil {
			http.Error(w, "invalid json-rpc request", http.StatusBadRequest)
			return
		}
		requests = []jsonRPCRequest{req}
	}

	needsSubmit := false
	for _, req := range requests {
		if jsonRPCMethodNeedsSubmit(strings.TrimSpace(req.Method)) {
			needsSubmit = true
			break
		}
	}
	if !authorizeJSONRPCRequest(r, needsSubmit) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if isBatch {
		responses := make([]jsonRPCResponse, 0, len(requests))
		for _, req := range requests {
			responses = append(responses, s.handleJSONRPCMethod(req))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses)
		return
	}

	resp := s.handleJSONRPCMethod(requests[0])
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func ioReadAllLimited(body io.Reader, limit int64) ([]byte, error) {
	if body == nil {
		return nil, errors.New("nil body")
	}
	return io.ReadAll(io.LimitReader(body, limit))
}
