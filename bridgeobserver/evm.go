package bridgeobserver

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/crypto/sha3"

	"msc-chain/bridgeevmproof"
)

const (
	DefaultEVMLockEventSignature   = "Locked(address,address,bytes,uint256)"
	DefaultEVMUnlockEventSignature = "Unlocked(bytes32,address,address,uint256)"
	maxRPCResponseBytes            = 16 << 20
	maxBridgeRecipientBytes        = 192
	maxEVMBlockTransactions        = 10_000
	maxConcurrentReceiptRequests   = 16
	maxEVMReceiptLogs              = 1 << 16
)

type EVMConfig struct {
	SourceChainID        string   `json:"source_chain_id"`
	RPCURLs              []string `json:"rpc_urls"`
	RPCQuorum            int      `json:"rpc_quorum"`
	Confirmations        uint64   `json:"confirmations"`
	BridgeContract       string   `json:"bridge_contract"`
	AssetDenom           string   `json:"asset_denom"`
	OriginAsset          string   `json:"origin_asset"`
	AssetDecimals        uint8    `json:"asset_decimals"`
	LockEventSignature   string   `json:"lock_event_signature,omitempty"`
	UnlockEventSignature string   `json:"unlock_event_signature,omitempty"`
	RequestTimeout       string   `json:"request_timeout,omitempty"`
	AllowInsecureHTTP    bool     `json:"allow_insecure_http,omitempty"`
}

type evmBlock struct {
	Number           string            `json:"number"`
	Hash             string            `json:"hash"`
	ParentHash       string            `json:"parentHash"`
	StateRoot        string            `json:"stateRoot"`
	TransactionsRoot string            `json:"transactionsRoot"`
	ReceiptsRoot     string            `json:"receiptsRoot"`
	Timestamp        string            `json:"timestamp"`
	Transactions     []json.RawMessage `json:"transactions"`
}

type evmLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	BlockHash        string   `json:"blockHash"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`
}

type endpointObservation struct {
	endpointHash string
	head         uint64
	block        evmBlock
	events       []Event
	nativeProofs map[string]bridgeevmproof.Proof
	fingerprint  string
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func normalizeEVMConfig(config EVMConfig) (EVMConfig, time.Duration, error) {
	config.SourceChainID = NormalizeRegistryID(config.SourceChainID)
	config.BridgeContract = normalizeEVMAddress(config.BridgeContract)
	config.OriginAsset = normalizeEVMAddress(config.OriginAsset)
	config.AssetDenom = strings.TrimSpace(config.AssetDenom)
	if config.LockEventSignature == "" {
		config.LockEventSignature = DefaultEVMLockEventSignature
	}
	if config.UnlockEventSignature == "" {
		config.UnlockEventSignature = DefaultEVMUnlockEventSignature
	}
	config.LockEventSignature = strings.TrimSpace(config.LockEventSignature)
	config.UnlockEventSignature = strings.TrimSpace(config.UnlockEventSignature)
	if config.LockEventSignature != DefaultEVMLockEventSignature || config.UnlockEventSignature != DefaultEVMUnlockEventSignature {
		return config, 0, errors.New("canonical EVM receipt proofs require the MSCBridgeVault Locked/Unlocked event ABI")
	}
	if config.SourceChainID == "" || config.BridgeContract == "" || config.OriginAsset == "" || config.AssetDenom == "" {
		return config, 0, errors.New("source_chain_id, bridge_contract, origin_asset, and asset_denom are required")
	}
	if config.AssetDecimals > 30 || config.Confirmations == 0 {
		return config, 0, errors.New("asset_decimals must be <= 30 and confirmations must be positive")
	}
	if len(config.RPCURLs) < 2 {
		return config, 0, errors.New("at least two independent EVM RPC URLs are required")
	}
	if config.RPCQuorum < 2 || config.RPCQuorum > len(config.RPCURLs) {
		return config, 0, errors.New("rpc_quorum must be at least two and no greater than rpc_urls")
	}
	seen := make(map[string]struct{}, len(config.RPCURLs))
	seenHosts := make(map[string]struct{}, len(config.RPCURLs))
	for index, rawURL := range config.RPCURLs {
		normalized, err := validateRPCURL(rawURL, config.AllowInsecureHTTP)
		if err != nil {
			return config, 0, fmt.Errorf("rpc_urls[%d]: %w", index, err)
		}
		if _, exists := seen[normalized]; exists {
			return config, 0, fmt.Errorf("rpc_urls[%d] duplicates another endpoint", index)
		}
		parsed, _ := url.Parse(normalized)
		host := strings.ToLower(parsed.Host)
		if _, exists := seenHosts[host]; exists {
			return config, 0, fmt.Errorf("rpc_urls[%d] reuses provider host %s", index, host)
		}
		seen[normalized] = struct{}{}
		seenHosts[host] = struct{}{}
		config.RPCURLs[index] = normalized
	}
	timeout := 12 * time.Second
	if strings.TrimSpace(config.RequestTimeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(config.RequestTimeout))
		if err != nil || parsed < time.Second || parsed > 60*time.Second {
			return config, 0, errors.New("request_timeout must be between 1s and 60s")
		}
		timeout = parsed
	}
	return config, timeout, nil
}

func validateRPCURL(value string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("RPC URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" {
		host := strings.ToLower(parsed.Hostname())
		ip := net.ParseIP(host)
		local := host == "localhost" || (ip != nil && ip.IsLoopback())
		if parsed.Scheme != "http" || (!local && !allowInsecure) {
			return "", errors.New("RPC URL must use HTTPS; HTTP is restricted to localhost unless explicitly enabled")
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// ValidateEVMRPCURL exposes the observer's hardened RPC URL policy to sibling
// bridge operators such as the escrow reconciler.
func ValidateEVMRPCURL(value string, allowInsecure bool) (string, error) {
	return validateRPCURL(value, allowInsecure)
}

func normalizeEVMAddress(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "0x") || len(value) != 42 {
		return ""
	}
	if _, err := hex.DecodeString(value[2:]); err != nil {
		return ""
	}
	return value
}

func normalizeEVMHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "0x") || len(value) != 66 {
		return ""
	}
	if _, err := hex.DecodeString(value[2:]); err != nil {
		return ""
	}
	return value
}

func parseHexUint64(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "0x") || len(value) <= 2 {
		return 0, errors.New("hex quantity required")
	}
	parsed, err := strconv.ParseUint(value[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hex quantity: %w", err)
	}
	return parsed, nil
}

func eventTopic(signature string) string {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte(signature))
	return "0x" + hex.EncodeToString(hasher.Sum(nil))
}

func rpcHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxIdleConns = 16
	transport.MaxIdleConnsPerHost = 2
	transport.IdleConnTimeout = 30 * time.Second
	return &http.Client{
		Timeout: timeout, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("RPC redirects are disabled") },
	}
}

func rpcCall(ctx context.Context, client *http.Client, endpoint, method string, params any, target any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "msc-bridge-observer/1")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("RPC HTTP status %d", response.StatusCode)
	}
	rawResponse, err := io.ReadAll(io.LimitReader(response.Body, maxRPCResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read RPC response: %w", err)
	}
	if len(rawResponse) > maxRPCResponseBytes {
		return errors.New("RPC response exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(rawResponse))
	var envelope rpcResponse
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decode RPC response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("RPC response must contain exactly one JSON object")
	}
	if envelope.JSONRPC != "2.0" || string(bytes.TrimSpace(envelope.ID)) != "1" {
		return errors.New("RPC response protocol or request ID mismatch")
	}
	if envelope.Error != nil {
		return fmt.Errorf("RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) {
		return errors.New("RPC returned null result")
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("decode RPC result: %w", err)
	}
	return nil
}

func endpointHead(ctx context.Context, client *http.Client, endpoint string) (uint64, error) {
	var raw string
	if err := rpcCall(ctx, client, endpoint, "eth_blockNumber", []any{}, &raw); err != nil {
		return 0, err
	}
	return parseHexUint64(raw)
}

func resolveObservationHeight(ctx context.Context, client *http.Client, config EVMConfig, requested uint64) (uint64, error) {
	type result struct {
		head uint64
		err  error
	}
	results := make(chan result, len(config.RPCURLs))
	var group sync.WaitGroup
	for _, endpoint := range config.RPCURLs {
		group.Add(1)
		go func(endpoint string) {
			defer group.Done()
			head, err := endpointHead(ctx, client, endpoint)
			results <- result{head: head, err: err}
		}(endpoint)
	}
	group.Wait()
	close(results)
	heads := make([]uint64, 0, len(config.RPCURLs))
	for result := range results {
		if result.err == nil {
			heads = append(heads, result.head)
		}
	}
	if len(heads) < config.RPCQuorum {
		return 0, fmt.Errorf("only %d RPC heads available, quorum requires %d", len(heads), config.RPCQuorum)
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i] > heads[j] })
	quorumHead := heads[config.RPCQuorum-1]
	if requested > 0 {
		if quorumHead < requested || quorumHead-requested+1 < config.Confirmations {
			return 0, fmt.Errorf("height %d has fewer than %d confirmations on RPC quorum head %d", requested, config.Confirmations, quorumHead)
		}
		return requested, nil
	}
	if quorumHead+1 < config.Confirmations {
		return 0, errors.New("RPC quorum head is below confirmation policy")
	}
	return quorumHead - config.Confirmations + 1, nil
}

func decodeBlockTransactions(block evmBlock) (types.Transactions, error) {
	if len(block.Transactions) == 0 || len(block.Transactions) > maxEVMBlockTransactions {
		return nil, fmt.Errorf("EVM block transaction count %d is invalid", len(block.Transactions))
	}
	transactions := make(types.Transactions, len(block.Transactions))
	seen := make(map[common.Hash]struct{}, len(block.Transactions))
	for index, raw := range block.Transactions {
		if len(raw) == 0 || raw[0] != '{' {
			return nil, fmt.Errorf("EVM block transaction %d is not a full transaction object", index)
		}
		transaction := new(types.Transaction)
		if err := transaction.UnmarshalJSON(raw); err != nil {
			return nil, fmt.Errorf("decode EVM block transaction %d: %w", index, err)
		}
		if _, exists := seen[transaction.Hash()]; exists {
			return nil, fmt.Errorf("EVM block transaction %d duplicates hash %s", index, transaction.Hash())
		}
		seen[transaction.Hash()] = struct{}{}
		transactions[index] = transaction
	}
	return transactions, nil
}

func fetchBlockReceipts(ctx context.Context, client *http.Client, endpoint string, block evmBlock, height uint64, transactions types.Transactions) (types.Receipts, error) {
	type receiptResult struct {
		receipt *types.Receipt
		err     error
	}
	results := make([]receiptResult, len(transactions))
	semaphore := make(chan struct{}, maxConcurrentReceiptRequests)
	var group sync.WaitGroup
	for index, transaction := range transactions {
		group.Add(1)
		go func(index int, transaction *types.Transaction) {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[index].err = ctx.Err()
				return
			}
			receipt := new(types.Receipt)
			if err := rpcCall(ctx, client, endpoint, "eth_getTransactionReceipt", []any{transaction.Hash().Hex()}, receipt); err != nil {
				results[index].err = err
				return
			}
			results[index].receipt = receipt
		}(index, transaction)
	}
	group.Wait()

	wantBlockHash := common.HexToHash(block.Hash)
	receipts := make(types.Receipts, len(transactions))
	for index, result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("fetch EVM receipt %d: %w", index, result.err)
		}
		receipt := result.receipt
		if receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() != height ||
			receipt.BlockHash != wantBlockHash || receipt.TransactionIndex != uint(index) || receipt.TxHash != transactions[index].Hash() ||
			receipt.Type != transactions[index].Type() || len(receipt.Logs) > maxEVMReceiptLogs {
			return nil, fmt.Errorf("EVM receipt %d inclusion metadata is invalid", index)
		}
		if receipt.Bloom != types.CreateBloom(receipt) {
			return nil, fmt.Errorf("EVM receipt %d log bloom does not match its logs", index)
		}
		receipts[index] = receipt
	}
	return receipts, nil
}

func observeEndpoint(ctx context.Context, client *http.Client, endpoint string, config EVMConfig, height uint64, lockTopic, unlockTopic string) (endpointObservation, error) {
	head, err := endpointHead(ctx, client, endpoint)
	if err != nil {
		return endpointObservation{}, err
	}
	if head < height || head-height+1 < config.Confirmations {
		return endpointObservation{}, fmt.Errorf("endpoint head %d has insufficient confirmations for %d", head, height)
	}
	blockTag := fmt.Sprintf("0x%x", height)
	var block evmBlock
	if err := rpcCall(ctx, client, endpoint, "eth_getBlockByNumber", []any{blockTag, true}, &block); err != nil {
		return endpointObservation{}, err
	}
	blockNumber, err := parseHexUint64(block.Number)
	blockTimestamp, timestampErr := parseHexUint64(block.Timestamp)
	if err != nil || timestampErr != nil || blockTimestamp == 0 || blockNumber != height || normalizeEVMHash(block.Hash) == "" || normalizeEVMHash(block.ParentHash) == "" ||
		NormalizeHexHash(block.TransactionsRoot) == "" || NormalizeHexHash(block.ReceiptsRoot) == "" || NormalizeHexHash(block.StateRoot) == "" {
		return endpointObservation{}, errors.New("RPC returned an invalid canonical block header")
	}
	transactions, err := decodeBlockTransactions(block)
	if err != nil {
		return endpointObservation{}, err
	}
	receipts, err := fetchBlockReceipts(ctx, client, endpoint, block, height, transactions)
	if err != nil {
		return endpointObservation{}, err
	}
	proofBuilder, err := bridgeevmproof.NewBuilder(transactions, receipts)
	if err != nil {
		return endpointObservation{}, err
	}
	if proofBuilder.TransactionRoot() != NormalizeHexHash(block.TransactionsRoot) {
		return endpointObservation{}, errors.New("RPC block transactions do not reproduce transactionsRoot")
	}
	if proofBuilder.ReceiptRoot() != NormalizeHexHash(block.ReceiptsRoot) {
		return endpointObservation{}, errors.New("RPC receipts do not reproduce receiptsRoot")
	}

	events := make([]Event, 0)
	nativeProofs := make(map[string]bridgeevmproof.Proof)
	for transactionIndex, receipt := range receipts {
		for receiptLogIndex, receiptLog := range receipt.Logs {
			if receiptLog == nil || receiptLog.Address != common.HexToAddress(config.BridgeContract) || len(receiptLog.Topics) == 0 {
				continue
			}
			firstTopic := strings.ToLower(receiptLog.Topics[0].Hex())
			if firstTopic != strings.ToLower(lockTopic) && firstTopic != strings.ToLower(unlockTopic) {
				continue
			}
			topics := make([]string, len(receiptLog.Topics))
			for index, topic := range receiptLog.Topics {
				topics[index] = topic.Hex()
			}
			log := evmLog{
				Address: receiptLog.Address.Hex(), Topics: topics, Data: "0x" + hex.EncodeToString(receiptLog.Data),
				BlockNumber: block.Number, TransactionHash: transactions[transactionIndex].Hash().Hex(),
				TransactionIndex: fmt.Sprintf("0x%x", transactionIndex), BlockHash: block.Hash,
				// Bridge IDs use this transaction-local index, which is directly proven by the receipt trie.
				LogIndex: fmt.Sprintf("0x%x", receiptLogIndex),
			}
			event, err := decodeBridgeLog(config, block, height, log, lockTopic, unlockTopic)
			if err != nil {
				return endpointObservation{}, fmt.Errorf("transaction %d receipt log %d: %w", transactionIndex, receiptLogIndex, err)
			}
			nativeProof, err := proofBuilder.Proof(uint64(transactionIndex), uint64(receiptLogIndex))
			if err != nil {
				return endpointObservation{}, err
			}
			id := BridgeID(event)
			if id == "" {
				return endpointObservation{}, errors.New("canonical EVM bridge ID unavailable")
			}
			if _, exists := nativeProofs[id]; exists {
				return endpointObservation{}, fmt.Errorf("duplicate EVM bridge event %s", id)
			}
			events = append(events, event)
			nativeProofs[id] = nativeProof
		}
	}
	if len(events) == 0 {
		return endpointObservation{}, errors.New("finalized block contains no configured bridge events")
	}
	sort.Slice(events, func(i, j int) bool { return BridgeID(events[i]) < BridgeID(events[j]) })
	fingerprintMaterial := []string{
		strings.ToLower(block.Number), strings.ToLower(block.Hash), strings.ToLower(block.ParentHash),
		strings.ToLower(block.TransactionsRoot), strings.ToLower(block.ReceiptsRoot), strings.ToLower(block.StateRoot),
	}
	for _, event := range events {
		fingerprintMaterial = append(fingerprintMaterial, CanonicalEventPayload(event))
	}
	return endpointObservation{
		endpointHash: HashString(endpoint)[:16], head: head, block: block, events: events, nativeProofs: nativeProofs,
		fingerprint: HashString(strings.Join(fingerprintMaterial, "|")),
	}, nil
}

func decodeBridgeLog(config EVMConfig, block evmBlock, height uint64, log evmLog, lockTopic, unlockTopic string) (Event, error) {
	if log.Removed || normalizeEVMAddress(log.Address) != config.BridgeContract || normalizeEVMHash(log.BlockHash) != normalizeEVMHash(block.Hash) {
		return Event{}, errors.New("removed log or block/contract mismatch")
	}
	logHeight, err := parseHexUint64(log.BlockNumber)
	if err != nil || logHeight != height {
		return Event{}, errors.New("log block height mismatch")
	}
	transactionHash := normalizeEVMHash(log.TransactionHash)
	logIndex, err := parseHexUint64(log.LogIndex)
	if err != nil || transactionHash == "" || len(log.Topics) == 0 {
		return Event{}, errors.New("log identity invalid")
	}
	base := Event{
		SourceChainID: config.SourceChainID, SourceTxHash: transactionHash, LogIndex: logIndex,
		EventContract: config.BridgeContract, AssetDenom: config.AssetDenom, OriginAsset: config.OriginAsset,
		SourceHeight: height, SourceBlockHash: normalizeEVMHash(block.Hash),
	}
	switch strings.ToLower(log.Topics[0]) {
	case strings.ToLower(lockTopic):
		if len(log.Topics) != 3 || topicAddress(log.Topics[1]) != config.OriginAsset {
			return Event{}, errors.New("lock event token/topic layout mismatch")
		}
		recipient, amount, err := decodeLockData(log.Data, config.AssetDecimals)
		if err != nil {
			return Event{}, err
		}
		base.EventType, base.Recipient, base.Amount = "lock", recipient, amount
	case strings.ToLower(unlockTopic):
		withdrawalID := canonicalWithdrawalID(log.Topics[1])
		if len(log.Topics) != 4 || withdrawalID == "" || topicAddress(log.Topics[2]) != config.OriginAsset {
			return Event{}, errors.New("unlock event token/topic layout mismatch")
		}
		recipient := topicAddress(log.Topics[3])
		amount, err := decodeUint256Data(log.Data, config.AssetDecimals)
		if err != nil || recipient == "" {
			return Event{}, errors.New("unlock event recipient/amount invalid")
		}
		base.EventType, base.WithdrawalID, base.Recipient, base.Amount = "unlock", withdrawalID, recipient, amount
	default:
		return Event{}, errors.New("unexpected bridge event topic")
	}
	return base, nil
}

func topicAddress(topic string) string {
	topic = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(topic)), "0x")
	if len(topic) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(topic); err != nil || strings.Trim(topic[:24], "0") != "" {
		return ""
	}
	return normalizeEVMAddress("0x" + topic[24:])
}

func decodeHexData(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "0x") || len(value)%2 != 0 {
		return nil, errors.New("ABI data must be even-length 0x hex")
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil {
		return nil, errors.New("ABI data is not hex")
	}
	return raw, nil
}

func uint256Int(raw []byte) (*big.Int, error) {
	if len(raw) != 32 {
		return nil, errors.New("uint256 word must be 32 bytes")
	}
	return new(big.Int).SetBytes(raw), nil
}

func bigIntToBoundedInt(value *big.Int, max int) (int, error) {
	if value == nil || value.Sign() < 0 || !value.IsUint64() || value.Uint64() > uint64(max) {
		return 0, errors.New("ABI offset or length out of range")
	}
	return int(value.Uint64()), nil
}

func decodeLockData(value string, decimals uint8) (string, string, error) {
	recipientBytes, amountRaw, err := decodeLockDataRaw(value)
	if err != nil {
		return "", "", err
	}
	return string(recipientBytes), formatTokenUnits(amountRaw, decimals), nil
}

func decodeLockDataRaw(value string) ([]byte, *big.Int, error) {
	raw, err := decodeHexData(value)
	if err != nil || len(raw) < 96 || len(raw)%32 != 0 {
		return nil, nil, errors.New("lock event ABI data length invalid")
	}
	offsetWord, _ := uint256Int(raw[:32])
	offset, err := bigIntToBoundedInt(offsetWord, len(raw)-32)
	if err != nil || offset != 64 {
		return nil, nil, errors.New("lock recipient ABI offset invalid")
	}
	amountRaw, _ := uint256Int(raw[32:64])
	if amountRaw.Sign() <= 0 {
		return nil, nil, errors.New("lock amount must be positive")
	}
	lengthWord, _ := uint256Int(raw[offset : offset+32])
	length, err := bigIntToBoundedInt(lengthWord, maxBridgeRecipientBytes)
	paddedLength := ((length + 31) / 32) * 32
	if err != nil || offset+32+paddedLength != len(raw) || length == 0 {
		return nil, nil, errors.New("lock recipient ABI length invalid")
	}
	recipientBytes := raw[offset+32 : offset+32+length]
	for _, paddingByte := range raw[offset+32+length:] {
		if paddingByte != 0 {
			return nil, nil, errors.New("lock recipient ABI padding is non-canonical")
		}
	}
	recipient := string(recipientBytes)
	if !validMSCRecipient(recipient) {
		return nil, nil, errors.New("lock recipient is not a canonical MSC address")
	}
	return append([]byte(nil), recipientBytes...), amountRaw, nil
}

func validateEVMProofBinding(proof Proof, checkpoint Checkpoint) error {
	if proof.EVMReceiptProof == nil || proof.LogIndex != proof.EVMReceiptProof.ReceiptLogIndex {
		return errors.New("bridge log index does not match the proven receipt-local log index")
	}
	_, _, provenLog, err := bridgeevmproof.Verify(*proof.EVMReceiptProof, checkpoint.TransactionRoot, checkpoint.ReceiptRoot, proof.SourceTxHash)
	if err != nil {
		return err
	}
	if strings.ToLower(provenLog.Address.Hex()) != normalizeEVMAddress(proof.EventContract) || len(provenLog.Topics) == 0 {
		return errors.New("proven EVM log contract or topics do not match the bridge event")
	}
	eventType := normalizeEventType(proof.EventType)
	switch eventType {
	case "lock":
		if provenLog.Topics[0].Hex() != eventTopic(DefaultEVMLockEventSignature) || len(provenLog.Topics) != 3 ||
			topicAddress(provenLog.Topics[1].Hex()) != normalizeEVMAddress(proof.OriginAsset) {
			return errors.New("proven EVM lock topic layout does not match the bridge event")
		}
		recipient, _, err := decodeLockDataRaw("0x" + hex.EncodeToString(provenLog.Data))
		if err != nil || string(recipient) != proof.Recipient {
			return errors.New("proven EVM lock recipient does not match the bridge event")
		}
	case "unlock":
		if provenLog.Topics[0].Hex() != eventTopic(DefaultEVMUnlockEventSignature) || len(provenLog.Topics) != 4 ||
			canonicalWithdrawalID(provenLog.Topics[1].Hex()) != canonicalWithdrawalID(proof.WithdrawalID) ||
			topicAddress(provenLog.Topics[2].Hex()) != normalizeEVMAddress(proof.OriginAsset) ||
			topicAddress(provenLog.Topics[3].Hex()) != normalizeEVMAddress(proof.Recipient) {
			return errors.New("proven EVM unlock topic layout does not match the bridge event")
		}
		amount, err := decodeHexData("0x" + hex.EncodeToString(provenLog.Data))
		if err != nil || len(amount) != 32 || new(big.Int).SetBytes(amount).Sign() <= 0 {
			return errors.New("proven EVM unlock amount is invalid")
		}
	default:
		return errors.New("proven EVM event type is unsupported")
	}
	return nil
}

func decodeUint256Data(value string, decimals uint8) (string, error) {
	raw, err := decodeHexData(value)
	if err != nil || len(raw) != 32 {
		return "", errors.New("unlock amount ABI data must contain one uint256")
	}
	amount, _ := uint256Int(raw)
	if amount.Sign() <= 0 {
		return "", errors.New("unlock amount must be positive")
	}
	return formatTokenUnits(amount, decimals), nil
}

func validMSCRecipient(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "MSC") || len(value) != 45 {
		return false
	}
	_, err := hex.DecodeString(value[3:])
	return err == nil
}

func formatTokenUnits(amount *big.Int, decimals uint8) string {
	if decimals == 0 {
		return amount.String()
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	integer := new(big.Int)
	fraction := new(big.Int)
	integer.QuoRem(amount, scale, fraction)
	if fraction.Sign() == 0 {
		return integer.String()
	}
	fractionText := fraction.Text(10)
	fractionText = strings.Repeat("0", int(decimals)-len(fractionText)) + fractionText
	fractionText = strings.TrimRight(fractionText, "0")
	return integer.String() + "." + fractionText
}

func ObserveEVMBlock(ctx context.Context, rawConfig EVMConfig, requestedHeight uint64, previousCheckpointID string, issuedAt time.Time) (Artifact, error) {
	config, timeout, err := normalizeEVMConfig(rawConfig)
	if err != nil {
		return Artifact{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	client := rpcHTTPClient(timeout)
	height, err := resolveObservationHeight(ctx, client, config, requestedHeight)
	if err != nil {
		return Artifact{}, err
	}
	lockTopic := eventTopic(config.LockEventSignature)
	unlockTopic := eventTopic(config.UnlockEventSignature)
	type result struct {
		observation endpointObservation
		err         error
	}
	results := make(chan result, len(config.RPCURLs))
	var group sync.WaitGroup
	for _, endpoint := range config.RPCURLs {
		group.Add(1)
		go func(endpoint string) {
			defer group.Done()
			observation, err := observeEndpoint(ctx, client, endpoint, config, height, lockTopic, unlockTopic)
			results <- result{observation: observation, err: err}
		}(endpoint)
	}
	group.Wait()
	close(results)
	groups := make(map[string][]endpointObservation)
	for result := range results {
		if result.err == nil {
			groups[result.observation.fingerprint] = append(groups[result.observation.fingerprint], result.observation)
		}
	}
	var agreed []endpointObservation
	quorumViews := 0
	for _, candidates := range groups {
		if len(candidates) >= config.RPCQuorum {
			quorumViews++
		}
		if len(candidates) > len(agreed) {
			agreed = candidates
		}
	}
	if len(agreed) < config.RPCQuorum {
		return Artifact{}, fmt.Errorf("EVM block/log agreement %d below RPC quorum %d", len(agreed), config.RPCQuorum)
	}
	if quorumViews != 1 {
		return Artifact{}, fmt.Errorf("EVM RPC split view: %d conflicting observations each reached quorum", quorumViews)
	}
	sort.Slice(agreed, func(i, j int) bool { return agreed[i].endpointHash < agreed[j].endpointHash })
	if height > math.MaxUint64-config.Confirmations+1 {
		return Artifact{}, errors.New("confirmation height overflow")
	}
	observedHeight := height + config.Confirmations - 1
	endpointHashes := make([]string, 0, len(agreed))
	for _, observation := range agreed {
		endpointHashes = append(endpointHashes, observation.endpointHash)
	}
	proofs, eventRoot, err := BuildProofs(agreed[0].events, observedHeight)
	if err != nil {
		return Artifact{}, err
	}
	if issuedAt.IsZero() {
		blockTimestamp, _ := parseHexUint64(agreed[0].block.Timestamp)
		if blockTimestamp > math.MaxInt64 {
			return Artifact{}, errors.New("source block timestamp exceeds supported range")
		}
		issuedAt = time.Unix(int64(blockTimestamp), 0).UTC()
	}
	checkpoint := Checkpoint{
		Version: BridgeCheckpointVersion, PreviousCheckpointID: strings.ToLower(strings.TrimSpace(previousCheckpointID)),
		SourceChainID: config.SourceChainID, Height: height, ObservedHeight: observedHeight,
		BlockHash: normalizeEVMHash(agreed[0].block.Hash), EventRoot: eventRoot,
		TransactionRoot: NormalizeHexHash(agreed[0].block.TransactionsRoot),
		ReceiptRoot:     NormalizeHexHash(agreed[0].block.ReceiptsRoot), StateRoot: NormalizeHexHash(agreed[0].block.StateRoot),
		IssuedAtUnix: issuedAt.UTC().Unix(),
	}
	checkpoint.CheckpointID = CheckpointID(checkpoint, "evm")
	for index := range proofs {
		proofs[index].CheckpointID = checkpoint.CheckpointID
		nativeProof, exists := agreed[0].nativeProofs[proofs[index].EventID]
		if !exists {
			return Artifact{}, fmt.Errorf("canonical EVM proof missing for event %s", proofs[index].EventID)
		}
		proofs[index].EVMReceiptProof = &nativeProof
		checkpointCopy := checkpoint
		proofs[index].FinalityCheckpoint = &checkpointCopy
		proofs[index].LightClientHeader = &LightHeader{
			Height: checkpoint.Height, ChainID: checkpoint.SourceChainID, BlockHash: checkpoint.BlockHash,
			TxRoot:      checkpoint.TransactionRoot,
			ReceiptRoot: checkpoint.ReceiptRoot, StateRoot: checkpoint.StateRoot, EventRoot: checkpoint.EventRoot,
		}
	}
	return Artifact{
		Version: ObservationVersion, ChainType: "evm", ObservedAtUnix: time.Now().UTC().Unix(),
		Evidence: QuorumEvidence{
			Queried: len(config.RPCURLs), Agreed: len(agreed), Required: config.RPCQuorum,
			Fingerprint: agreed[0].fingerprint, EndpointHashes: endpointHashes,
		},
		Checkpoint: checkpoint, Proofs: proofs,
	}, nil
}
