package bridgereconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/sha3"
)

type evmRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type evmBlock struct {
	Number string `json:"number"`
	Hash   string `json:"hash"`
}

type headObservation struct {
	Endpoint     string
	EndpointHash string
	Head         uint64
}

type routeProbe struct {
	Fingerprint      string
	EndpointHash     string
	BlockHeight      uint64
	BlockHash        string
	TrackedEscrow    *big.Int
	TokenBalance     *big.Int
	Paused           bool
	RouteEnabled     bool
	RouteMinAmount   *big.Int
	RouteMaxAmount   *big.Int
	RouteDailyLock   *big.Int
	RouteDailyUnlock *big.Int
}

type evmProbeResult struct {
	BlockHeight      uint64
	BlockHash        string
	TrackedEscrow    *big.Int
	TokenBalance     *big.Int
	Paused           bool
	RouteEnabled     bool
	RouteMinAmount   *big.Int
	RouteMaxAmount   *big.Int
	RouteDailyLock   *big.Int
	RouteDailyUnlock *big.Int
	Agreed           int
	EndpointHashes   []string
}

func (r *Reconciler) rpcCall(ctx context.Context, endpoint, method string, params any, target any) error {
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
	request.Header.Set("User-Agent", "msc-bridge-reconciler/1")
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("RPC HTTP status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return errors.New("RPC response unreadable or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var envelope evmRPCResponse
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

func parseHexQuantity(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "0x") || len(value) <= 2 || (len(value) > 3 && value[2] == '0') {
		return 0, errors.New("canonical hex quantity required")
	}
	parsed, err := strconv.ParseUint(value[2:], 16, 64)
	if err != nil {
		return 0, errors.New("invalid hex quantity")
	}
	return parsed, nil
}

func endpointDigest(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:8])
}

func (r *Reconciler) observeHead(ctx context.Context, endpoint string, expectedChainID uint64) (headObservation, error) {
	var chainHex, headHex string
	if err := r.rpcCall(ctx, endpoint, "eth_chainId", []any{}, &chainHex); err != nil {
		return headObservation{}, err
	}
	chainID, err := parseHexQuantity(chainHex)
	if err != nil || chainID != expectedChainID {
		return headObservation{}, fmt.Errorf("RPC chain ID mismatch: got %d want %d", chainID, expectedChainID)
	}
	if err := r.rpcCall(ctx, endpoint, "eth_blockNumber", []any{}, &headHex); err != nil {
		return headObservation{}, err
	}
	head, err := parseHexQuantity(headHex)
	if err != nil {
		return headObservation{}, err
	}
	return headObservation{Endpoint: endpoint, EndpointHash: endpointDigest(endpoint), Head: head}, nil
}

func methodSelector(signature string) string {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte(signature))
	return hex.EncodeToString(hasher.Sum(nil)[:4])
}

func addressCallData(signature, address string) string {
	return "0x" + methodSelector(signature) + strings.Repeat("0", 24) + strings.TrimPrefix(address, "0x")
}

func noArgCallData(signature string) string { return "0x" + methodSelector(signature) }

func decodeUint256(value string) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return nil, errors.New("eth_call must return one ABI uint256 word")
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil || len(raw) != 32 {
		return nil, errors.New("eth_call returned invalid ABI data")
	}
	return new(big.Int).SetBytes(raw), nil
}

func normalizeHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return ""
	}
	if _, err := hex.DecodeString(value[2:]); err != nil || strings.Trim(value[2:], "0") == "" {
		return ""
	}
	return value
}

func validCode(value string) bool {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) < 2 || len(value)%2 != 0 || strings.Trim(value, "0") == "" {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (r *Reconciler) observeRouteAt(ctx context.Context, head headObservation, config RouteConfig, height uint64) (routeProbe, error) {
	blockTag := fmt.Sprintf("0x%x", height)
	var block evmBlock
	if err := r.rpcCall(ctx, head.Endpoint, "eth_getBlockByNumber", []any{blockTag, false}, &block); err != nil {
		return routeProbe{}, err
	}
	blockNumber, err := parseHexQuantity(block.Number)
	blockHash := normalizeHash(block.Hash)
	if err != nil || blockNumber != height || blockHash == "" {
		return routeProbe{}, errors.New("RPC returned invalid reconciliation block")
	}
	var vaultCode, tokenCode string
	if err := r.rpcCall(ctx, head.Endpoint, "eth_getCode", []any{config.VaultAddress, blockTag}, &vaultCode); err != nil || !validCode(vaultCode) {
		return routeProbe{}, errors.New("vault bytecode missing at reconciliation block")
	}
	if err := r.rpcCall(ctx, head.Endpoint, "eth_getCode", []any{config.TokenAddress, blockTag}, &tokenCode); err != nil || !validCode(tokenCode) {
		return routeProbe{}, errors.New("token bytecode missing at reconciliation block")
	}
	call := func(to, data string) (string, error) {
		var result string
		err := r.rpcCall(ctx, head.Endpoint, "eth_call", []any{map[string]string{"to": to, "data": data}, blockTag}, &result)
		return result, err
	}
	trackedRaw, err := call(config.VaultAddress, addressCallData("trackedEscrow(address)", config.TokenAddress))
	if err != nil {
		return routeProbe{}, fmt.Errorf("trackedEscrow call: %w", err)
	}
	tracked, err := decodeUint256(trackedRaw)
	if err != nil {
		return routeProbe{}, err
	}
	balanceRaw, err := call(config.TokenAddress, addressCallData("balanceOf(address)", config.VaultAddress))
	if err != nil {
		return routeProbe{}, fmt.Errorf("balanceOf call: %w", err)
	}
	balance, err := decodeUint256(balanceRaw)
	if err != nil {
		return routeProbe{}, err
	}
	pausedRaw, err := call(config.VaultAddress, noArgCallData("paused()"))
	if err != nil {
		return routeProbe{}, fmt.Errorf("paused call: %w", err)
	}
	pausedValue, err := decodeUint256(pausedRaw)
	if err != nil || (pausedValue.Sign() != 0 && pausedValue.Cmp(big.NewInt(1)) != 0) {
		return routeProbe{}, errors.New("paused call returned invalid boolean")
	}
	fingerprint := strings.Join([]string{blockHash, tracked.String(), balance.String(), pausedValue.String()}, "|")
	return routeProbe{
		Fingerprint:   fingerprint,
		EndpointHash:  head.EndpointHash,
		BlockHeight:   height,
		BlockHash:     blockHash,
		TrackedEscrow: tracked,
		TokenBalance:  balance,
		Paused:        pausedValue.Sign() != 0,
	}, nil
}

func (r *Reconciler) probeEVMRoute(ctx context.Context, config RouteConfig) (evmProbeResult, error) {
	heads := make([]headObservation, 0, len(config.RPCURLs))
	var headsMu sync.Mutex
	var wait sync.WaitGroup
	for _, endpoint := range config.RPCURLs {
		endpoint := endpoint
		wait.Add(1)
		go func() {
			defer wait.Done()
			head, err := r.observeHead(ctx, endpoint, config.ExpectedEVMChainID)
			if err == nil {
				headsMu.Lock()
				heads = append(heads, head)
				headsMu.Unlock()
			}
		}()
	}
	wait.Wait()
	if len(heads) < config.RPCQuorum {
		return evmProbeResult{}, fmt.Errorf("RPC head quorum not met: got %d need %d", len(heads), config.RPCQuorum)
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].Head > heads[j].Head })
	// The quorum-th highest head is supported by at least quorum providers. A
	// single healthy-but-lagging provider must not drag the whole monitor back.
	quorumHead := heads[config.RPCQuorum-1].Head
	if quorumHead+1 < config.Confirmations {
		return evmProbeResult{}, errors.New("RPC quorum head is below confirmation policy")
	}
	height := quorumHead - config.Confirmations + 1
	probes := make([]routeProbe, 0, len(heads))
	var probesMu sync.Mutex
	for _, head := range heads {
		if head.Head < quorumHead {
			continue
		}
		head := head
		wait.Add(1)
		go func() {
			defer wait.Done()
			probe, err := r.observeRouteAt(ctx, head, config, height)
			if err == nil {
				probesMu.Lock()
				probes = append(probes, probe)
				probesMu.Unlock()
			}
		}()
	}
	wait.Wait()
	groups := make(map[string][]routeProbe)
	for _, probe := range probes {
		groups[probe.Fingerprint] = append(groups[probe.Fingerprint], probe)
	}
	var agreed []routeProbe
	quorumGroups := 0
	for _, group := range groups {
		if len(group) >= config.RPCQuorum {
			quorumGroups++
		}
		if len(group) > len(agreed) {
			agreed = group
		}
	}
	if quorumGroups > 1 {
		return evmProbeResult{}, errors.New("RPC split view: multiple conflicting states independently reached quorum")
	}
	if len(agreed) < config.RPCQuorum {
		return evmProbeResult{}, fmt.Errorf("RPC state quorum not met: best agreement %d need %d", len(agreed), config.RPCQuorum)
	}
	endpointHashes := make([]string, 0, len(agreed))
	for _, probe := range agreed {
		endpointHashes = append(endpointHashes, probe.EndpointHash)
	}
	sort.Strings(endpointHashes)
	selected := agreed[0]
	return evmProbeResult{
		BlockHeight:    selected.BlockHeight,
		BlockHash:      selected.BlockHash,
		TrackedEscrow:  new(big.Int).Set(selected.TrackedEscrow),
		TokenBalance:   new(big.Int).Set(selected.TokenBalance),
		Paused:         selected.Paused,
		Agreed:         len(agreed),
		EndpointHashes: endpointHashes,
	}, nil
}
