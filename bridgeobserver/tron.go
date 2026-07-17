package bridgeobserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
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
	"strings"
	"sync"
	"time"
)

const (
	TronMainnetChainID        = "tron-mainnet"
	TronMainnetGenesisBlockID = "00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc"
	TronMainnetUSDTAddress    = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	TronMainnetConfirmations  = uint64(64)
)

type TronConfig struct {
	SourceChainID        string   `json:"source_chain_id"`
	GenesisBlockID       string   `json:"genesis_block_id,omitempty"`
	APIURLs              []string `json:"api_urls"`
	APIQuorum            int      `json:"api_quorum"`
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

type tronBlock struct {
	BlockID     string `json:"blockID"`
	BlockHeader struct {
		RawData struct {
			Number           int64  `json:"number"`
			TransactionRoot  string `json:"txTrieRoot"`
			ParentHash       string `json:"parentHash"`
			Timestamp        int64  `json:"timestamp"`
			AccountStateRoot string `json:"accountStateRoot"`
		} `json:"raw_data"`
	} `json:"block_header"`
	Transactions []tronBlockTransaction `json:"transactions"`
}

type tronLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type tronTransactionInfo struct {
	ID             string          `json:"id"`
	BlockNumber    int64           `json:"blockNumber"`
	BlockTimestamp int64           `json:"blockTimeStamp"`
	Result         json.RawMessage `json:"result,omitempty"`
	Receipt        struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Logs []tronLog `json:"log"`
}

type tronEndpointObservation struct {
	EndpointHash string
	Block        tronBlock
	Events       []Event
	Fingerprint  string
}

func normalizeTronConfig(config TronConfig) (TronConfig, time.Duration, error) {
	config.SourceChainID = NormalizeRegistryID(config.SourceChainID)
	config.GenesisBlockID = normalizeTronHash(config.GenesisBlockID)
	config.BridgeContract = normalizeTronAddress(config.BridgeContract)
	config.OriginAsset = normalizeTronAddress(config.OriginAsset)
	config.AssetDenom = strings.TrimSpace(config.AssetDenom)
	if config.LockEventSignature == "" {
		config.LockEventSignature = DefaultEVMLockEventSignature
	}
	if config.UnlockEventSignature == "" {
		config.UnlockEventSignature = DefaultEVMUnlockEventSignature
	}
	config.LockEventSignature = strings.TrimSpace(config.LockEventSignature)
	config.UnlockEventSignature = strings.TrimSpace(config.UnlockEventSignature)
	if config.SourceChainID == "" || config.BridgeContract == "" || config.OriginAsset == "" || config.AssetDenom == "" {
		return config, 0, errors.New("source_chain_id, bridge_contract, origin_asset, and asset_denom are required")
	}
	if config.BridgeContract == config.OriginAsset || config.AssetDecimals > 30 || config.Confirmations == 0 {
		return config, 0, errors.New("bridge and token addresses must differ, asset_decimals must be <= 30, and confirmations must be positive")
	}
	if config.SourceChainID == TronMainnetChainID {
		if config.GenesisBlockID != TronMainnetGenesisBlockID || config.OriginAsset != TronMainnetUSDTAddress ||
			config.AssetDenom != "USDT-TRON" || config.AssetDecimals != 6 || config.Confirmations < TronMainnetConfirmations ||
			config.AllowInsecureHTTP || config.LockEventSignature != DefaultEVMLockEventSignature ||
			config.UnlockEventSignature != DefaultEVMUnlockEventSignature {
			return config, 0, errors.New("TRON mainnet observer must use canonical genesis, official Tether USDT, 6 decimals, frozen vault ABI, HTTPS, and at least 64 confirmations")
		}
	}
	if len(config.APIURLs) < 2 || config.APIQuorum < 2 || config.APIQuorum > len(config.APIURLs) {
		return config, 0, errors.New("at least two independent Tron APIs and a quorum of at least two are required")
	}
	seenURLs := make(map[string]struct{}, len(config.APIURLs))
	seenProviders := make(map[string]struct{}, len(config.APIURLs))
	for index, rawURL := range config.APIURLs {
		normalized, provider, err := validateTronAPIURL(rawURL, config.AllowInsecureHTTP)
		if err != nil {
			return config, 0, fmt.Errorf("api_urls[%d]: %w", index, err)
		}
		if _, exists := seenURLs[normalized]; exists {
			return config, 0, fmt.Errorf("api_urls[%d] duplicates another endpoint", index)
		}
		if _, exists := seenProviders[provider]; exists {
			return config, 0, fmt.Errorf("api_urls[%d] reuses provider host %s", index, provider)
		}
		seenURLs[normalized] = struct{}{}
		seenProviders[provider] = struct{}{}
		config.APIURLs[index] = normalized
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

// ValidateTronConfig applies the observer's complete endpoint, network,
// contract, asset, and finality policy without contacting the network.
func ValidateTronConfig(config TronConfig) (TronConfig, error) {
	normalized, _, err := normalizeTronConfig(config)
	return normalized, err
}

func validateTronAPIURL(value string, allowInsecure bool) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errors.New("API URL must be absolute and contain no credentials, query, or fragment")
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	local := hostname == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || (!local && !allowInsecure)) {
		return "", "", errors.New("API URL must use HTTPS; HTTP is restricted to localhost unless explicitly enabled")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	provider := hostname
	if local {
		provider = strings.ToLower(parsed.Host)
	}
	return parsed.String(), provider, nil
}

// ValidateTronAPIURL exposes the observer's hardened endpoint and independent
// provider identity policy to sibling bridge operators.
func ValidateTronAPIURL(value string, allowInsecure bool) (string, string, error) {
	return validateTronAPIURL(value, allowInsecure)
}

func tronAPICall(ctx context.Context, client *http.Client, endpoint, path string, body any, target any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(payload))
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
		return fmt.Errorf("Tron API HTTP status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxRPCResponseBytes+1))
	if err != nil || len(raw) > maxRPCResponseBytes {
		return errors.New("Tron API response unreadable or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode Tron API response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("Tron API response must contain exactly one JSON value")
	}
	return nil
}

func normalizeTronHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "0x")
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func validateTronBlock(block tronBlock, expectedHeight uint64, requireTransactions bool) error {
	if expectedHeight == 0 || expectedHeight > math.MaxInt64 || block.BlockHeader.RawData.Number != int64(expectedHeight) ||
		normalizeTronHash(block.BlockID) == "" || normalizeTronHash(block.BlockHeader.RawData.ParentHash) == "" ||
		normalizeTronHash(block.BlockHeader.RawData.TransactionRoot) == "" ||
		block.BlockHeader.RawData.Timestamp <= 0 {
		return errors.New("Tron API returned an invalid solidified block header")
	}
	if stateRoot := strings.TrimSpace(block.BlockHeader.RawData.AccountStateRoot); stateRoot != "" && normalizeTronHash(stateRoot) == "" {
		return errors.New("Tron API returned an invalid account state root")
	}
	if !strings.HasPrefix(normalizeTronHash(block.BlockID), fmt.Sprintf("%016x", expectedHeight)) {
		return errors.New("Tron block ID does not encode the requested height")
	}
	if requireTransactions && len(block.Transactions) == 0 {
		return errors.New("solidified block contains no transactions")
	}
	if requireTransactions {
		transactionRoot, err := tronTransactionMerkleRoot(block.Transactions)
		if err != nil {
			return fmt.Errorf("Tron transaction commitment: %w", err)
		}
		if transactionRoot != normalizeTronHash(block.BlockHeader.RawData.TransactionRoot) {
			return errors.New("Tron transaction list does not match block txTrieRoot")
		}
	}
	seen := make(map[string]struct{}, len(block.Transactions))
	for _, transaction := range block.Transactions {
		txID := normalizeTronHash(transaction.TxID)
		if txID == "" {
			return errors.New("solidified block contains an invalid transaction ID")
		}
		if _, exists := seen[txID]; exists {
			return errors.New("solidified block duplicates a transaction ID")
		}
		seen[txID] = struct{}{}
	}
	return nil
}

func tronTransactionSucceeded(info tronTransactionInfo) bool {
	if !strings.EqualFold(strings.TrimSpace(info.Receipt.Result), "SUCCESS") {
		return false
	}
	result := strings.TrimSpace(string(info.Result))
	if result == "" || result == "null" || result == `""` || strings.EqualFold(result, `"SUCCESS"`) {
		return true
	}
	return false
}

func decodeTronEvents(config TronConfig, block tronBlock, infos []tronTransactionInfo, lockTopic, unlockTopic string) ([]Event, string, error) {
	infoByID := make(map[string]tronTransactionInfo, len(infos))
	for _, info := range infos {
		txID := normalizeTronHash(info.ID)
		if txID == "" || info.BlockNumber != block.BlockHeader.RawData.Number || (info.BlockTimestamp != 0 && info.BlockTimestamp != block.BlockHeader.RawData.Timestamp) {
			return nil, "", errors.New("transaction info does not belong to the requested solidified block")
		}
		if _, exists := infoByID[txID]; exists {
			return nil, "", errors.New("transaction info duplicates a transaction ID")
		}
		infoByID[txID] = info
	}
	if len(infoByID) != len(block.Transactions) {
		return nil, "", errors.New("transaction info set does not cover the complete solidified block")
	}
	lockTopic = strings.TrimPrefix(strings.ToLower(lockTopic), "0x")
	unlockTopic = strings.TrimPrefix(strings.ToLower(unlockTopic), "0x")
	events := make([]Event, 0)
	fingerprintParts := []string{
		normalizeTronHash(block.BlockID), normalizeTronHash(block.BlockHeader.RawData.ParentHash),
		normalizeTronHash(block.BlockHeader.RawData.TransactionRoot), normalizeTronHash(block.BlockHeader.RawData.AccountStateRoot),
		fmt.Sprintf("%d", block.BlockHeader.RawData.Timestamp),
	}
	globalLogIndex := uint64(0)
	for _, transaction := range block.Transactions {
		txID := normalizeTronHash(transaction.TxID)
		info, found := infoByID[txID]
		if !found {
			return nil, "", errors.New("solidified block transaction has no transaction info")
		}
		delete(infoByID, txID)
		succeeded := tronTransactionSucceeded(info)
		fingerprintParts = append(fingerprintParts, txID, strings.ToUpper(strings.TrimSpace(info.Receipt.Result)), strings.TrimSpace(string(info.Result)))
		for _, log := range info.Logs {
			if globalLogIndex == math.MaxUint64 {
				return nil, "", errors.New("Tron block log index overflow")
			}
			fingerprintParts = append(fingerprintParts, strings.ToLower(strings.TrimSpace(log.Address)), strings.Join(normalizeTronTopics(log.Topics), ","), strings.ToLower(strings.TrimSpace(log.Data)))
			if succeeded {
				event, matched, err := decodeTronBridgeLog(config, block, txID, globalLogIndex, log, lockTopic, unlockTopic)
				if err != nil {
					return nil, "", err
				}
				if matched {
					events = append(events, event)
				}
			}
			globalLogIndex++
		}
	}
	if len(infoByID) != 0 {
		return nil, "", errors.New("transaction info contains IDs absent from the solidified block")
	}
	if len(events) == 0 {
		return nil, "", errors.New("solidified block contains no configured bridge events")
	}
	sort.Slice(events, func(i, j int) bool { return BridgeID(events[i]) < BridgeID(events[j]) })
	for _, event := range events {
		fingerprintParts = append(fingerprintParts, CanonicalEventPayload(event))
	}
	return events, HashString(strings.Join(fingerprintParts, "|")), nil
}

func normalizeTronTopics(topics []string) []string {
	out := make([]string, len(topics))
	for index, topic := range topics {
		out[index] = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(topic)), "0x")
	}
	return out
}

func decodeTronBridgeLog(config TronConfig, block tronBlock, txID string, logIndex uint64, log tronLog, lockTopic, unlockTopic string) (Event, bool, error) {
	contract := tronLogAddress(log.Address)
	if contract == "" {
		return Event{}, false, errors.New("Tron event log contains an invalid contract address")
	}
	if contract != config.BridgeContract {
		return Event{}, false, nil
	}
	topics := normalizeTronTopics(log.Topics)
	if len(topics) == 0 {
		return Event{}, false, nil
	}
	base := Event{
		SourceChainID: config.SourceChainID, SourceTxHash: txID, LogIndex: logIndex,
		EventContract: config.BridgeContract, AssetDenom: config.AssetDenom, OriginAsset: config.OriginAsset,
		SourceHeight: uint64(block.BlockHeader.RawData.Number), SourceBlockHash: normalizeTronHash(block.BlockID),
	}
	switch topics[0] {
	case lockTopic:
		if len(topics) != 3 || tronTopicAddress(topics[1]) != config.OriginAsset || tronTopicAddress(topics[2]) == "" {
			return Event{}, false, errors.New("Tron lock event topic layout or token binding mismatch")
		}
		recipient, amount, err := decodeLockData("0x"+strings.TrimPrefix(strings.TrimSpace(log.Data), "0x"), config.AssetDecimals)
		if err != nil {
			return Event{}, false, fmt.Errorf("Tron lock event data: %w", err)
		}
		base.EventType, base.Recipient, base.Amount = "lock", recipient, amount
		return base, true, nil
	case unlockTopic:
		withdrawalID := canonicalWithdrawalID(topics[1])
		if len(topics) != 4 || withdrawalID == "" || tronTopicAddress(topics[2]) != config.OriginAsset {
			return Event{}, false, errors.New("Tron unlock event topic layout or token binding mismatch")
		}
		recipient := tronTopicAddress(topics[3])
		amount, err := decodeUint256Data("0x"+strings.TrimPrefix(strings.TrimSpace(log.Data), "0x"), config.AssetDecimals)
		if err != nil || recipient == "" {
			return Event{}, false, errors.New("Tron unlock event recipient or amount invalid")
		}
		base.EventType, base.WithdrawalID, base.Recipient, base.Amount = "unlock", withdrawalID, recipient, amount
		return base, true, nil
	default:
		return Event{}, false, nil
	}
}

func observeTronEndpoint(ctx context.Context, client *http.Client, endpoint string, config TronConfig, height, observedHeight uint64, lockTopic, unlockTopic string) (tronEndpointObservation, error) {
	if config.GenesisBlockID != "" {
		var genesis tronBlock
		if err := tronAPICall(ctx, client, endpoint, "/wallet/getblockbynum", map[string]any{"num": 0, "detail": false}, &genesis); err != nil {
			return tronEndpointObservation{}, fmt.Errorf("verify Tron genesis: %w", err)
		}
		if normalizeTronHash(genesis.BlockID) != config.GenesisBlockID || genesis.BlockHeader.RawData.Number != 0 {
			return tronEndpointObservation{}, errors.New("Tron API genesis block does not match configured network")
		}
	}
	var head tronBlock
	if err := tronAPICall(ctx, client, endpoint, "/walletsolidity/getnowblock", map[string]any{}, &head); err != nil {
		return tronEndpointObservation{}, err
	}
	if head.BlockHeader.RawData.Number < 1 || uint64(head.BlockHeader.RawData.Number) < observedHeight || normalizeTronHash(head.BlockID) == "" {
		return tronEndpointObservation{}, errors.New("Tron Solidity API head is below requested solidified height")
	}
	var block tronBlock
	if err := tronAPICall(ctx, client, endpoint, "/walletsolidity/getblockbynum", map[string]any{"num": height, "detail": true}, &block); err != nil {
		return tronEndpointObservation{}, err
	}
	if err := validateTronBlock(block, height, true); err != nil {
		return tronEndpointObservation{}, err
	}
	var infos []tronTransactionInfo
	if err := tronAPICall(ctx, client, endpoint, "/walletsolidity/gettransactioninfobyblocknum", map[string]any{"num": height}, &infos); err != nil {
		return tronEndpointObservation{}, err
	}
	events, fingerprint, err := decodeTronEvents(config, block, infos, lockTopic, unlockTopic)
	if err != nil {
		return tronEndpointObservation{}, err
	}
	return tronEndpointObservation{EndpointHash: HashString(endpoint)[:16], Block: block, Events: events, Fingerprint: fingerprint}, nil
}

func ObserveTronBlock(ctx context.Context, rawConfig TronConfig, height uint64, previousCheckpointID string, issuedAt time.Time) (Artifact, error) {
	config, timeout, err := normalizeTronConfig(rawConfig)
	if err != nil {
		return Artifact{}, err
	}
	if height == 0 {
		return Artifact{}, errors.New("explicit non-zero solidified block height required")
	}
	if height > math.MaxUint64-config.Confirmations+1 {
		return Artifact{}, errors.New("Tron confirmation height overflow")
	}
	observedHeight := height + config.Confirmations - 1
	if ctx == nil {
		ctx = context.Background()
	}
	client := rpcHTTPClient(timeout)
	lockTopic := eventTopic(config.LockEventSignature)
	unlockTopic := eventTopic(config.UnlockEventSignature)
	type result struct {
		Observation tronEndpointObservation
		Err         error
	}
	results := make(chan result, len(config.APIURLs))
	var group sync.WaitGroup
	for _, endpoint := range config.APIURLs {
		group.Add(1)
		go func(endpoint string) {
			defer group.Done()
			observation, err := observeTronEndpoint(ctx, client, endpoint, config, height, observedHeight, lockTopic, unlockTopic)
			results <- result{Observation: observation, Err: err}
		}(endpoint)
	}
	group.Wait()
	close(results)
	groups := make(map[string][]tronEndpointObservation)
	for result := range results {
		if result.Err == nil {
			groups[result.Observation.Fingerprint] = append(groups[result.Observation.Fingerprint], result.Observation)
		}
	}
	var agreed []tronEndpointObservation
	quorumViews := 0
	for _, candidates := range groups {
		if len(candidates) >= config.APIQuorum {
			quorumViews++
		}
		if len(candidates) > len(agreed) {
			agreed = candidates
		}
	}
	if len(agreed) < config.APIQuorum {
		return Artifact{}, fmt.Errorf("Tron solidified block/log agreement %d below API quorum %d", len(agreed), config.APIQuorum)
	}
	if quorumViews != 1 {
		return Artifact{}, fmt.Errorf("Tron API split view: %d conflicting observations each reached quorum", quorumViews)
	}
	sort.Slice(agreed, func(i, j int) bool { return agreed[i].EndpointHash < agreed[j].EndpointHash })
	proofs, eventRoot, err := BuildProofs(agreed[0].Events, observedHeight)
	if err != nil {
		return Artifact{}, err
	}
	if err := attachTronTransactionProofs(proofs, agreed[0].Block); err != nil {
		return Artifact{}, err
	}
	if issuedAt.IsZero() {
		issuedAt = time.UnixMilli(agreed[0].Block.BlockHeader.RawData.Timestamp).UTC()
	}
	checkpoint := Checkpoint{
		Version: BridgeCheckpointVersion, PreviousCheckpointID: strings.ToLower(strings.TrimSpace(previousCheckpointID)),
		SourceChainID: config.SourceChainID, Height: height, ObservedHeight: observedHeight,
		BlockHash: normalizeTronHash(agreed[0].Block.BlockID), EventRoot: eventRoot,
		TransactionRoot: normalizeTronHash(agreed[0].Block.BlockHeader.RawData.TransactionRoot),
		// The wire protocol uses this generic external commitment slot for Tron's
		// block-header transaction Merkle root.
		ReceiptRoot: normalizeTronHash(agreed[0].Block.BlockHeader.RawData.TransactionRoot),
		StateRoot:   normalizeTronHash(agreed[0].Block.BlockHeader.RawData.AccountStateRoot), IssuedAtUnix: issuedAt.UTC().Unix(),
	}
	checkpoint.CheckpointID = CheckpointID(checkpoint, "tron")
	for index := range proofs {
		proofs[index].CheckpointID = checkpoint.CheckpointID
		checkpointCopy := checkpoint
		proofs[index].FinalityCheckpoint = &checkpointCopy
		proofs[index].LightClientHeader = &LightHeader{
			Height: checkpoint.Height, ChainID: checkpoint.SourceChainID, BlockHash: checkpoint.BlockHash,
			TxRoot:      checkpoint.TransactionRoot,
			ReceiptRoot: checkpoint.ReceiptRoot, StateRoot: checkpoint.StateRoot, EventRoot: checkpoint.EventRoot,
		}
	}
	endpointHashes := make([]string, 0, len(agreed))
	for _, observation := range agreed {
		endpointHashes = append(endpointHashes, observation.EndpointHash)
	}
	return Artifact{
		Version: ObservationVersion, ChainType: "tron", ObservedAtUnix: time.Now().UTC().Unix(),
		Evidence: QuorumEvidence{
			Queried: len(config.APIURLs), Agreed: len(agreed), Required: config.APIQuorum,
			Fingerprint: agreed[0].Fingerprint, EndpointHashes: endpointHashes,
		},
		Checkpoint: checkpoint, Proofs: proofs,
	}, nil
}

const tronBase58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func decodeTronBase58(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty Tron Base58 value")
	}
	number := new(big.Int)
	base := big.NewInt(58)
	for index := 0; index < len(value); index++ {
		digit := strings.IndexByte(tronBase58Alphabet, value[index])
		if digit < 0 {
			return nil, errors.New("invalid Tron Base58 character")
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

func encodeTronBase58(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	value := new(big.Int).SetBytes(raw)
	base := big.NewInt(58)
	zero := new(big.Int)
	modulus := new(big.Int)
	encoded := make([]byte, 0, len(raw)*2)
	for value.Cmp(zero) > 0 {
		value.QuoRem(value, base, modulus)
		encoded = append(encoded, tronBase58Alphabet[modulus.Int64()])
	}
	for _, current := range raw {
		if current != 0 {
			break
		}
		encoded = append(encoded, tronBase58Alphabet[0])
	}
	for left, right := 0, len(encoded)-1; left < right; left, right = left+1, right-1 {
		encoded[left], encoded[right] = encoded[right], encoded[left]
	}
	return string(encoded)
}

func normalizeTronAddress(value string) string {
	value = strings.TrimSpace(value)
	decoded, err := decodeTronBase58(value)
	if err != nil || len(decoded) != 25 || decoded[0] != 0x41 {
		return ""
	}
	first := sha256.Sum256(decoded[:21])
	second := sha256.Sum256(first[:])
	if subtle.ConstantTimeCompare(decoded[21:], second[:4]) != 1 || encodeTronBase58(decoded) != value {
		return ""
	}
	return value
}

// NormalizeTronAddress validates a Tron Base58Check address and returns its
// canonical representation. Tron addresses are case-sensitive.
func NormalizeTronAddress(value string) string {
	return normalizeTronAddress(value)
}

// TronAddressToTVMHex converts a canonical Tron Base58Check address to the
// 20-byte Solidity/TVM address representation used in ABI words.
func TronAddressToTVMHex(value string) (string, error) {
	value = normalizeTronAddress(value)
	if value == "" {
		return "", errors.New("invalid Tron Base58Check address")
	}
	decoded, err := decodeTronBase58(value)
	if err != nil || len(decoded) != 25 {
		return "", errors.New("invalid Tron Base58Check address")
	}
	return "0x" + hex.EncodeToString(decoded[1:21]), nil
}

func tronAddressFromPayload(payload []byte) string {
	if len(payload) != 20 {
		return ""
	}
	address := append([]byte{0x41}, payload...)
	first := sha256.Sum256(address)
	second := sha256.Sum256(first[:])
	address = append(address, second[:4]...)
	return encodeTronBase58(address)
}

func tronLogAddress(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) != 40 {
		return ""
	}
	raw, err := hex.DecodeString(value)
	if err != nil {
		return ""
	}
	return tronAddressFromPayload(raw)
}

// TronAddressFromTVMHex converts the 20-byte address representation used in
// TVM event logs into a canonical Tron Base58Check address.
func TronAddressFromTVMHex(value string) (string, error) {
	address := tronLogAddress(value)
	if address == "" {
		return "", errors.New("TVM address must be exactly 20-byte hex")
	}
	return address, nil
}

func tronTopicAddress(topic string) string {
	topic = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(topic)), "0x")
	if len(topic) != 64 || strings.Trim(topic[:24], "0") != "" {
		return ""
	}
	raw, err := hex.DecodeString(topic[24:])
	if err != nil {
		return ""
	}
	return tronAddressFromPayload(raw)
}
