package bridgeobserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
)

const (
	SolanaBridgeEventDomain = "MSC_BRIDGE_EVENT_V1"
	solanaEventVersion      = byte(1)
	solanaEventLock         = byte(1)
	solanaEventUnlock       = byte(2)
	solanaRecipientMSC      = byte(1)
	solanaRecipientPubkey   = byte(2)
	solanaEventHeaderBytes  = 85
	solanaBase58Alphabet    = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	maxSolanaRPCResponse    = 64 << 20
)

type SolanaConfig struct {
	SourceChainID     string   `json:"source_chain_id"`
	RPCURLs           []string `json:"rpc_urls"`
	RPCQuorum         int      `json:"rpc_quorum"`
	Confirmations     uint64   `json:"confirmations"`
	BridgeProgram     string   `json:"bridge_program"`
	AssetDenom        string   `json:"asset_denom"`
	OriginMint        string   `json:"origin_mint"`
	AssetDecimals     uint8    `json:"asset_decimals"`
	RequestTimeout    string   `json:"request_timeout,omitempty"`
	AllowInsecureHTTP bool     `json:"allow_insecure_http,omitempty"`
}

type solanaTransactionMeta struct {
	Err         json.RawMessage `json:"err"`
	LogMessages *[]string       `json:"logMessages"`
}

type solanaBlockTransaction struct {
	Meta        *solanaTransactionMeta `json:"meta"`
	Transaction struct {
		Signatures []string `json:"signatures"`
	} `json:"transaction"`
}

type solanaBlock struct {
	BlockHeight       *uint64                  `json:"blockHeight"`
	BlockTime         *int64                   `json:"blockTime"`
	Blockhash         string                   `json:"blockhash"`
	ParentSlot        uint64                   `json:"parentSlot"`
	PreviousBlockhash string                   `json:"previousBlockhash"`
	Transactions      []solanaBlockTransaction `json:"transactions"`
}

type solanaEndpointObservation struct {
	EndpointHash string
	Block        solanaBlock
	Events       []Event
	Fingerprint  string
}

type solanaFingerprintTransaction struct {
	Signatures  []string `json:"signatures"`
	Error       string   `json:"error"`
	LogsPresent bool     `json:"logs_present"`
	Logs        []string `json:"logs"`
}

type solanaFingerprintView struct {
	Slot              uint64                         `json:"slot"`
	BlockHeight       *uint64                        `json:"block_height"`
	BlockTime         *int64                         `json:"block_time"`
	Blockhash         string                         `json:"blockhash"`
	ParentSlot        uint64                         `json:"parent_slot"`
	PreviousBlockhash string                         `json:"previous_blockhash"`
	Transactions      []solanaFingerprintTransaction `json:"transactions"`
	Events            []string                       `json:"events"`
}

func normalizeSolanaConfig(config SolanaConfig) (SolanaConfig, time.Duration, error) {
	config.SourceChainID = NormalizeRegistryID(config.SourceChainID)
	config.BridgeProgram = normalizeSolanaPubkey(config.BridgeProgram)
	config.OriginMint = normalizeSolanaPubkey(config.OriginMint)
	config.AssetDenom = strings.TrimSpace(config.AssetDenom)
	if config.SourceChainID == "" || config.BridgeProgram == "" || config.OriginMint == "" || config.AssetDenom == "" {
		return config, 0, errors.New("source_chain_id, bridge_program, origin_mint, and asset_denom are required")
	}
	if config.BridgeProgram == config.OriginMint || config.AssetDecimals > 30 || config.Confirmations == 0 {
		return config, 0, errors.New("bridge program and token mint must differ, asset_decimals must be <= 30, and confirmations must be positive")
	}
	if len(config.RPCURLs) < 2 || config.RPCQuorum < 2 || config.RPCQuorum > len(config.RPCURLs) {
		return config, 0, errors.New("at least two independent Solana RPCs and a quorum of at least two are required")
	}
	seenURLs := make(map[string]struct{}, len(config.RPCURLs))
	seenProviders := make(map[string]struct{}, len(config.RPCURLs))
	for index, rawURL := range config.RPCURLs {
		normalized, err := validateRPCURL(rawURL, config.AllowInsecureHTTP)
		if err != nil {
			return config, 0, fmt.Errorf("rpc_urls[%d]: %w", index, err)
		}
		parsed, _ := url.Parse(normalized)
		hostname := strings.ToLower(parsed.Hostname())
		ip := net.ParseIP(hostname)
		provider := hostname
		if hostname == "localhost" || (ip != nil && ip.IsLoopback()) {
			provider = strings.ToLower(parsed.Host)
		}
		if _, exists := seenURLs[normalized]; exists {
			return config, 0, fmt.Errorf("rpc_urls[%d] duplicates another endpoint", index)
		}
		if _, exists := seenProviders[provider]; exists {
			return config, 0, fmt.Errorf("rpc_urls[%d] reuses provider host %s", index, provider)
		}
		seenURLs[normalized] = struct{}{}
		seenProviders[provider] = struct{}{}
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

func solanaRPCCall(ctx context.Context, client *http.Client, endpoint, method string, params any, target any) error {
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
		return fmt.Errorf("Solana RPC HTTP status %d", response.StatusCode)
	}
	rawResponse, err := io.ReadAll(io.LimitReader(response.Body, maxSolanaRPCResponse+1))
	if err != nil || len(rawResponse) > maxSolanaRPCResponse {
		return errors.New("Solana RPC response unreadable or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(rawResponse))
	var envelope rpcResponse
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decode Solana RPC response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("Solana RPC response must contain exactly one JSON object")
	}
	if envelope.JSONRPC != "2.0" || len(envelope.ID) == 0 {
		return errors.New("Solana RPC response version or ID invalid")
	}
	if envelope.Error != nil {
		return fmt.Errorf("Solana RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) {
		return errors.New("Solana RPC returned no result")
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("decode Solana RPC result: %w", err)
	}
	return nil
}

func decodeSolanaBase58(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty Solana base58 value")
	}
	number := new(big.Int)
	base := big.NewInt(58)
	for index := 0; index < len(value); index++ {
		digit := strings.IndexByte(solanaBase58Alphabet, value[index])
		if digit < 0 {
			return nil, errors.New("invalid Solana base58 character")
		}
		number.Mul(number, base)
		number.Add(number, big.NewInt(int64(digit)))
	}
	decoded := number.Bytes()
	leadingZeroes := 0
	for leadingZeroes < len(value) && value[leadingZeroes] == solanaBase58Alphabet[0] {
		leadingZeroes++
	}
	out := make([]byte, leadingZeroes+len(decoded))
	copy(out[leadingZeroes:], decoded)
	return out, nil
}

func encodeSolanaBase58(raw []byte) string {
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
		encoded = append(encoded, solanaBase58Alphabet[modulus.Int64()])
	}
	for _, current := range raw {
		if current != 0 {
			break
		}
		encoded = append(encoded, solanaBase58Alphabet[0])
	}
	for left, right := 0, len(encoded)-1; left < right; left, right = left+1, right-1 {
		encoded[left], encoded[right] = encoded[right], encoded[left]
	}
	return string(encoded)
}

func normalizeSolanaBase58(value string, size int) string {
	value = strings.TrimSpace(value)
	decoded, err := decodeSolanaBase58(value)
	if err != nil || len(decoded) != size || encodeSolanaBase58(decoded) != value {
		return ""
	}
	return value
}

func normalizeSolanaPubkey(value string) string    { return normalizeSolanaBase58(value, 32) }
func normalizeSolanaSignature(value string) string { return normalizeSolanaBase58(value, 64) }

// SolanaPubkeyFromBytes returns the canonical base58 form used for programs,
// mints, accounts, and blockhashes.
func SolanaPubkeyFromBytes(raw []byte) (string, error) {
	if len(raw) != 32 {
		return "", errors.New("Solana public key must be 32 bytes")
	}
	return encodeSolanaBase58(raw), nil
}

// SolanaSignatureFromBytes returns the canonical base58 representation of a
// 64-byte Solana transaction signature.
func SolanaSignatureFromBytes(raw []byte) (string, error) {
	if len(raw) != 64 {
		return "", errors.New("Solana signature must be 64 bytes")
	}
	return encodeSolanaBase58(raw), nil
}

func solanaEventDiscriminator() [8]byte {
	digest := sha256.Sum256([]byte(SolanaBridgeEventDomain))
	var discriminator [8]byte
	copy(discriminator[:], digest[:8])
	return discriminator
}

func decodeSolanaBridgeEvent(config SolanaConfig, raw []byte) (string, string, string, string, bool, error) {
	discriminator := solanaEventDiscriminator()
	if len(raw) < len(discriminator) || !bytes.Equal(raw[:8], discriminator[:8]) {
		return "", "", "", "", false, nil
	}
	if len(raw) < solanaEventHeaderBytes || raw[8] != solanaEventVersion {
		return "", "", "", "", true, errors.New("Solana bridge event version or length invalid")
	}
	eventType := raw[9]
	mint := encodeSolanaBase58(raw[10:42])
	if mint != config.OriginMint {
		return "", "", "", "", true, errors.New("Solana bridge event token mint mismatch")
	}
	amount := binary.LittleEndian.Uint64(raw[42:50])
	if amount == 0 {
		return "", "", "", "", true, errors.New("Solana bridge event amount must be positive")
	}
	withdrawalRaw := raw[50:82]
	withdrawalID := canonicalWithdrawalID(hex.EncodeToString(withdrawalRaw))
	recipientKind := raw[82]
	recipientLength := int(binary.LittleEndian.Uint16(raw[83:85]))
	if recipientLength <= 0 || recipientLength > maxBridgeRecipientBytes || len(raw) != solanaEventHeaderBytes+recipientLength {
		return "", "", "", "", true, errors.New("Solana bridge event recipient length invalid")
	}
	recipientRaw := raw[85:]
	var kind, recipient string
	switch eventType {
	case solanaEventLock:
		if recipientKind != solanaRecipientMSC || !bytes.Equal(withdrawalRaw, make([]byte, 32)) {
			return "", "", "", "", true, errors.New("Solana lock event recipient or withdrawal ID encoding invalid")
		}
		kind, withdrawalID, recipient = "lock", "", string(recipientRaw)
		if !validMSCRecipient(recipient) {
			return "", "", "", "", true, errors.New("Solana lock event MSC recipient invalid")
		}
	case solanaEventUnlock:
		if recipientKind != solanaRecipientPubkey || len(recipientRaw) != 32 || bytes.Equal(withdrawalRaw, make([]byte, 32)) {
			return "", "", "", "", true, errors.New("Solana unlock event recipient or withdrawal ID encoding invalid")
		}
		kind, recipient = "unlock", encodeSolanaBase58(recipientRaw)
	default:
		return "", "", "", "", true, errors.New("Solana bridge event type invalid")
	}
	return kind, withdrawalID, recipient, formatTokenUnits(new(big.Int).SetUint64(amount), config.AssetDecimals), true, nil
}

func encodeSolanaBridgeEvent(eventType byte, mint, withdrawalID, recipient string, amount uint64) ([]byte, error) {
	mintRaw, err := decodeSolanaBase58(mint)
	if err != nil || len(mintRaw) != 32 || amount == 0 {
		return nil, errors.New("invalid Solana event mint or amount")
	}
	var recipientKind byte
	var recipientRaw []byte
	withdrawalRaw := make([]byte, 32)
	switch eventType {
	case solanaEventLock:
		if strings.TrimSpace(withdrawalID) != "" || !validMSCRecipient(recipient) {
			return nil, errors.New("invalid MSC event recipient")
		}
		recipientKind, recipientRaw = solanaRecipientMSC, []byte(recipient)
	case solanaEventUnlock:
		canonicalID := canonicalWithdrawalID(withdrawalID)
		if canonicalID == "" {
			return nil, errors.New("invalid Solana withdrawal ID")
		}
		withdrawalRaw, err = hex.DecodeString(strings.TrimPrefix(canonicalID, "0x"))
		if err != nil || bytes.Equal(withdrawalRaw, make([]byte, 32)) {
			return nil, errors.New("invalid Solana withdrawal ID")
		}
		recipientRaw, err = decodeSolanaBase58(recipient)
		if err != nil || len(recipientRaw) != 32 {
			return nil, errors.New("invalid Solana event recipient")
		}
		recipientKind = solanaRecipientPubkey
	default:
		return nil, errors.New("invalid Solana event type")
	}
	discriminator := solanaEventDiscriminator()
	out := make([]byte, solanaEventHeaderBytes+len(recipientRaw))
	copy(out[:8], discriminator[:8])
	out[8], out[9] = solanaEventVersion, eventType
	copy(out[10:42], mintRaw)
	binary.LittleEndian.PutUint64(out[42:50], amount)
	copy(out[50:82], withdrawalRaw)
	out[82] = recipientKind
	binary.LittleEndian.PutUint16(out[83:85], uint16(len(recipientRaw)))
	copy(out[85:], recipientRaw)
	return out, nil
}

func canonicalSolanaError(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 {
		return "", false, errors.New("Solana transaction metadata omits err")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, errors.New("Solana transaction error metadata invalid")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", false, err
	}
	return string(canonical), value == nil, nil
}

func validateSolanaBlock(block solanaBlock, slot uint64) error {
	if slot == 0 || normalizeSolanaPubkey(block.Blockhash) == "" || normalizeSolanaPubkey(block.PreviousBlockhash) == "" || block.ParentSlot >= slot {
		return errors.New("Solana RPC returned an invalid finalized block header")
	}
	if block.BlockTime != nil && *block.BlockTime <= 0 {
		return errors.New("Solana finalized block time invalid")
	}
	if len(block.Transactions) == 0 {
		return errors.New("Solana finalized block contains no transactions")
	}
	seen := make(map[string]struct{}, len(block.Transactions))
	for index, transaction := range block.Transactions {
		if transaction.Meta == nil || len(transaction.Transaction.Signatures) == 0 {
			return fmt.Errorf("Solana transaction %d metadata or signature missing", index)
		}
		if _, _, err := canonicalSolanaError(transaction.Meta.Err); err != nil {
			return fmt.Errorf("Solana transaction %d: %w", index, err)
		}
		for _, signature := range transaction.Transaction.Signatures {
			if normalizeSolanaSignature(signature) == "" {
				return fmt.Errorf("Solana transaction %d contains invalid signature", index)
			}
		}
		primary := transaction.Transaction.Signatures[0]
		if _, exists := seen[primary]; exists {
			return errors.New("Solana finalized block duplicates a primary transaction signature")
		}
		seen[primary] = struct{}{}
	}
	return nil
}

func solanaProgramInvocation(line string) (string, int, bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) != 4 || parts[0] != "Program" || parts[2] != "invoke" || len(parts[3]) < 3 || parts[3][0] != '[' || parts[3][len(parts[3])-1] != ']' {
		return "", 0, false
	}
	depth, err := strconv.Atoi(parts[3][1 : len(parts[3])-1])
	if err != nil || depth < 1 || normalizeSolanaPubkey(parts[1]) == "" {
		return "", 0, false
	}
	return parts[1], depth, true
}

func solanaProgramCompletion(line string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 3 || parts[0] != "Program" || (parts[2] != "success" && parts[2] != "failed:") || normalizeSolanaPubkey(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func decodeSolanaEvents(config SolanaConfig, slot uint64, block solanaBlock) ([]Event, string, error) {
	view := solanaFingerprintView{
		Slot: slot, BlockHeight: block.BlockHeight, BlockTime: block.BlockTime,
		Blockhash: block.Blockhash, ParentSlot: block.ParentSlot, PreviousBlockhash: block.PreviousBlockhash,
		Transactions: make([]solanaFingerprintTransaction, 0, len(block.Transactions)),
	}
	events := make([]Event, 0)
	globalLogIndex := uint64(0)
	for txIndex, transaction := range block.Transactions {
		errorText, succeeded, err := canonicalSolanaError(transaction.Meta.Err)
		if err != nil {
			return nil, "", err
		}
		logsPresent := transaction.Meta.LogMessages != nil
		logs := []string(nil)
		if logsPresent {
			logs = append([]string(nil), (*transaction.Meta.LogMessages)...)
		}
		view.Transactions = append(view.Transactions, solanaFingerprintTransaction{
			Signatures: append([]string(nil), transaction.Transaction.Signatures...),
			Error:      errorText, LogsPresent: logsPresent, Logs: logs,
		})
		stack := make([]string, 0, 4)
		for _, line := range logs {
			if program, depth, ok := solanaProgramInvocation(line); ok {
				if depth > len(stack)+1 {
					return nil, "", fmt.Errorf("Solana transaction %d program invocation stack is discontinuous", txIndex)
				}
				if depth <= len(stack) {
					stack = stack[:depth-1]
				}
				stack = append(stack, program)
			} else if program, ok := solanaProgramCompletion(line); ok {
				if len(stack) > 0 && stack[len(stack)-1] == program {
					stack = stack[:len(stack)-1]
				}
			} else if succeeded && strings.HasPrefix(line, "Program data: ") && len(stack) > 0 && stack[len(stack)-1] == config.BridgeProgram {
				fields := strings.Fields(strings.TrimPrefix(line, "Program data: "))
				if len(fields) == 0 {
					return nil, "", errors.New("Solana bridge program emitted empty data log")
				}
				raw, decodeErr := base64.StdEncoding.DecodeString(fields[0])
				if decodeErr != nil || base64.StdEncoding.EncodeToString(raw) != fields[0] {
					return nil, "", errors.New("Solana bridge program emitted invalid base64 data")
				}
				kind, withdrawalID, recipient, amount, matched, decodeErr := decodeSolanaBridgeEvent(config, raw)
				if decodeErr != nil {
					return nil, "", decodeErr
				}
				if matched {
					if len(fields) != 1 {
						return nil, "", errors.New("Solana bridge event must use exactly one data slice")
					}
					events = append(events, Event{
						SourceChainID: config.SourceChainID, SourceTxHash: transaction.Transaction.Signatures[0], LogIndex: globalLogIndex,
						EventType: kind, WithdrawalID: withdrawalID, EventContract: config.BridgeProgram,
						AssetDenom: config.AssetDenom, OriginAsset: config.OriginMint,
						Recipient: recipient, Amount: amount, SourceHeight: slot, SourceBlockHash: block.Blockhash,
					})
				}
			}
			if globalLogIndex == math.MaxUint64 {
				return nil, "", errors.New("Solana block log index overflow")
			}
			globalLogIndex++
		}
	}
	if len(events) == 0 {
		return nil, "", errors.New("Solana finalized block contains no configured bridge events")
	}
	sort.Slice(events, func(i, j int) bool { return BridgeID(events[i]) < BridgeID(events[j]) })
	for _, event := range events {
		view.Events = append(view.Events, CanonicalEventPayload(event))
	}
	rawView, err := json.Marshal(view)
	if err != nil {
		return nil, "", err
	}
	return events, HashString(string(rawView)), nil
}

func observeSolanaEndpoint(ctx context.Context, client *http.Client, endpoint string, config SolanaConfig, slot, observedSlot uint64) (solanaEndpointObservation, error) {
	var head uint64
	if err := solanaRPCCall(ctx, client, endpoint, "getSlot", []any{map[string]any{"commitment": "finalized"}}, &head); err != nil {
		return solanaEndpointObservation{}, err
	}
	if head < observedSlot {
		return solanaEndpointObservation{}, errors.New("Solana finalized RPC head is below required confirmation slot")
	}
	var block solanaBlock
	if err := solanaRPCCall(ctx, client, endpoint, "getBlock", []any{slot, map[string]any{
		"commitment": "finalized", "encoding": "json", "transactionDetails": "full", "maxSupportedTransactionVersion": 0, "rewards": false,
	}}, &block); err != nil {
		return solanaEndpointObservation{}, err
	}
	if err := validateSolanaBlock(block, slot); err != nil {
		return solanaEndpointObservation{}, err
	}
	events, fingerprint, err := decodeSolanaEvents(config, slot, block)
	if err != nil {
		return solanaEndpointObservation{}, err
	}
	return solanaEndpointObservation{EndpointHash: HashString(endpoint)[:16], Block: block, Events: events, Fingerprint: fingerprint}, nil
}

// ObserveSolanaSlot creates an unsigned threshold-observer artifact for one
// explicitly selected finalized Solana slot.
func ObserveSolanaSlot(ctx context.Context, rawConfig SolanaConfig, slot uint64, previousCheckpointID string, issuedAt time.Time) (Artifact, error) {
	config, timeout, err := normalizeSolanaConfig(rawConfig)
	if err != nil {
		return Artifact{}, err
	}
	if slot == 0 {
		return Artifact{}, errors.New("explicit non-zero finalized Solana slot required")
	}
	if slot > math.MaxUint64-config.Confirmations+1 {
		return Artifact{}, errors.New("Solana confirmation slot overflow")
	}
	observedSlot := slot + config.Confirmations - 1
	if ctx == nil {
		ctx = context.Background()
	}
	client := rpcHTTPClient(timeout)
	type result struct {
		Observation solanaEndpointObservation
		Err         error
	}
	results := make(chan result, len(config.RPCURLs))
	var group sync.WaitGroup
	for _, endpoint := range config.RPCURLs {
		group.Add(1)
		go func(endpoint string) {
			defer group.Done()
			observation, err := observeSolanaEndpoint(ctx, client, endpoint, config, slot, observedSlot)
			results <- result{Observation: observation, Err: err}
		}(endpoint)
	}
	group.Wait()
	close(results)
	groups := make(map[string][]solanaEndpointObservation)
	for result := range results {
		if result.Err == nil {
			groups[result.Observation.Fingerprint] = append(groups[result.Observation.Fingerprint], result.Observation)
		}
	}
	var agreed []solanaEndpointObservation
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
		return Artifact{}, fmt.Errorf("Solana finalized block/log agreement %d below RPC quorum %d", len(agreed), config.RPCQuorum)
	}
	if quorumViews != 1 {
		return Artifact{}, fmt.Errorf("Solana RPC split view: %d conflicting observations each reached quorum", quorumViews)
	}
	sort.Slice(agreed, func(i, j int) bool { return agreed[i].EndpointHash < agreed[j].EndpointHash })
	proofs, eventRoot, err := BuildProofs(agreed[0].Events, observedSlot)
	if err != nil {
		return Artifact{}, err
	}
	if issuedAt.IsZero() {
		if agreed[0].Block.BlockTime == nil {
			return Artifact{}, errors.New("Solana block time unavailable; issued-at override required")
		}
		issuedAt = time.Unix(*agreed[0].Block.BlockTime, 0).UTC()
	}
	checkpoint := Checkpoint{
		Version: BridgeCheckpointVersion, PreviousCheckpointID: strings.ToLower(strings.TrimSpace(previousCheckpointID)),
		SourceChainID: config.SourceChainID, Height: slot, ObservedHeight: observedSlot,
		BlockHash: agreed[0].Block.Blockhash, EventRoot: eventRoot,
		// Solana exposes a finalized blockhash but no EVM-style receipt/state root.
		// This compatibility slot commits the complete quorum-agreed transaction
		// result and program-log view. It is not a native Solana receipt root.
		ReceiptRoot: agreed[0].Fingerprint, IssuedAtUnix: issuedAt.UTC().Unix(),
	}
	checkpoint.CheckpointID = CheckpointID(checkpoint, "solana")
	for index := range proofs {
		proofs[index].CheckpointID = checkpoint.CheckpointID
		checkpointCopy := checkpoint
		proofs[index].FinalityCheckpoint = &checkpointCopy
		proofs[index].LightClientHeader = &LightHeader{
			Height: checkpoint.Height, ChainID: checkpoint.SourceChainID, BlockHash: checkpoint.BlockHash,
			ReceiptRoot: checkpoint.ReceiptRoot, EventRoot: checkpoint.EventRoot,
		}
	}
	endpointHashes := make([]string, 0, len(agreed))
	for _, observation := range agreed {
		endpointHashes = append(endpointHashes, observation.EndpointHash)
	}
	return Artifact{
		Version: ObservationVersion, ChainType: "solana", ObservedAtUnix: time.Now().UTC().Unix(),
		Evidence: QuorumEvidence{
			Queried: len(config.RPCURLs), Agreed: len(agreed), Required: config.RPCQuorum,
			Fingerprint: agreed[0].Fingerprint, EndpointHashes: endpointHashes,
		},
		Checkpoint: checkpoint, Proofs: proofs,
	}, nil
}
