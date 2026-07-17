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
	"net/http"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/sha3"

	"msc-chain/bridgeobserver"
)

var errTronHeadAdvanced = errors.New("Tron solidified state advanced during reconciliation reads")

type tronReconcileBlock struct {
	BlockID     string `json:"blockID"`
	BlockHeader struct {
		RawData struct {
			Number int64 `json:"number"`
		} `json:"raw_data"`
	} `json:"block_header"`
}

type tronContractInfo struct {
	RuntimeCode   string `json:"runtimecode"`
	SmartContract struct {
		ContractAddress string `json:"contract_address"`
	} `json:"smart_contract"`
}

type tronConstantResult struct {
	Result struct {
		Result  bool   `json:"result"`
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"result"`
	ConstantResult []string `json:"constant_result"`
}

func (r *Reconciler) tronCall(ctx context.Context, endpoint, path string, body any, target any) error {
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
	request.Header.Set("User-Agent", "msc-bridge-reconciler/2")
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Tron API HTTP status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
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

func normalizeTronBlock(block tronReconcileBlock, allowGenesis bool) (uint64, string, error) {
	if block.BlockHeader.RawData.Number < 0 {
		return 0, "", errors.New("Tron block height is negative")
	}
	height := uint64(block.BlockHeader.RawData.Number)
	if !allowGenesis && height == 0 {
		return 0, "", errors.New("Tron solidified head is empty")
	}
	blockID := normalizeExternalHash(block.BlockID)
	if blockID == "" || !strings.HasPrefix(blockID, fmt.Sprintf("%016x", height)) {
		return 0, "", errors.New("Tron block ID does not encode its height")
	}
	return height, blockID, nil
}

func runtimeCodeHash(runtimeCode string) string {
	runtimeCode = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(runtimeCode)), "0x")
	if len(runtimeCode) < 2 || len(runtimeCode)%2 != 0 || strings.Trim(runtimeCode, "0") == "" {
		return ""
	}
	raw, err := hex.DecodeString(runtimeCode)
	if err != nil {
		return ""
	}
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(raw)
	return "0x" + hex.EncodeToString(hasher.Sum(nil))
}

func tronAddressParameter(address string) (string, error) {
	addressHex, err := bridgeobserver.TronAddressToTVMHex(address)
	if err != nil {
		return "", err
	}
	return strings.Repeat("0", 24) + strings.TrimPrefix(addressHex, "0x"), nil
}

func (r *Reconciler) tronHead(ctx context.Context, endpoint string) (uint64, string, error) {
	var block tronReconcileBlock
	if err := r.tronCall(ctx, endpoint, "/walletsolidity/getnowblock", map[string]any{}, &block); err != nil {
		return 0, "", err
	}
	return normalizeTronBlock(block, false)
}

func (r *Reconciler) verifyTronGenesis(ctx context.Context, endpoint, expected string) error {
	var block tronReconcileBlock
	if err := r.tronCall(ctx, endpoint, "/wallet/getblockbynum", map[string]any{"num": 0, "detail": false}, &block); err != nil {
		return err
	}
	height, blockID, err := normalizeTronBlock(block, true)
	if err != nil || height != 0 || blockID != expected {
		return errors.New("Tron API genesis does not match configured network")
	}
	return nil
}

func (r *Reconciler) tronCodeHash(ctx context.Context, endpoint, address string) (string, error) {
	var info tronContractInfo
	if err := r.tronCall(ctx, endpoint, "/wallet/getcontractinfo", map[string]any{"value": address, "visible": true}, &info); err != nil {
		return "", err
	}
	if bridgeobserver.NormalizeTronAddress(info.SmartContract.ContractAddress) != address {
		return "", errors.New("Tron contract-info address mismatch")
	}
	hash := runtimeCodeHash(info.RuntimeCode)
	if hash == "" {
		return "", errors.New("Tron contract runtime code is missing")
	}
	return hash, nil
}

func (r *Reconciler) tronWordsCall(ctx context.Context, endpoint, owner, contract, selector, parameter string, count int) ([]*big.Int, error) {
	if count <= 0 || count > 16 {
		return nil, errors.New("invalid Tron ABI word count")
	}
	body := map[string]any{
		"owner_address":     owner,
		"contract_address":  contract,
		"function_selector": selector,
		"visible":           true,
	}
	if parameter != "" {
		body["parameter"] = parameter
	}
	var response tronConstantResult
	if err := r.tronCall(ctx, endpoint, "/walletsolidity/triggerconstantcontract", body, &response); err != nil {
		return nil, err
	}
	if !response.Result.Result || len(response.ConstantResult) != 1 {
		return nil, errors.New("Tron constant contract call failed or returned an ambiguous result")
	}
	value := strings.TrimPrefix(strings.TrimSpace(response.ConstantResult[0]), "0x")
	if len(value) != count*64 {
		return nil, fmt.Errorf("Tron constant contract result has %d ABI words, expected %d", len(value)/64, count)
	}
	words := make([]*big.Int, 0, count)
	for index := 0; index < count; index++ {
		decoded, err := decodeUint256("0x" + value[index*64:(index+1)*64])
		if err != nil {
			return nil, fmt.Errorf("Tron constant contract result word %d: %w", index, err)
		}
		words = append(words, decoded)
	}
	return words, nil
}

func (r *Reconciler) tronUintCall(ctx context.Context, endpoint, owner, contract, selector, parameter string) (*big.Int, error) {
	words, err := r.tronWordsCall(ctx, endpoint, owner, contract, selector, parameter, 1)
	if err != nil {
		return nil, err
	}
	return words[0], nil
}

func (r *Reconciler) observeTronRoute(ctx context.Context, endpoint string, config RouteConfig) (routeProbe, error) {
	if err := r.verifyTronGenesis(ctx, endpoint, config.ExpectedGenesisID); err != nil {
		return routeProbe{}, err
	}
	vaultCodeHash, err := r.tronCodeHash(ctx, endpoint, config.VaultAddress)
	if err != nil || vaultCodeHash != config.ExpectedVaultCodeHash {
		return routeProbe{}, errors.New("Tron vault runtime code hash mismatch")
	}
	tokenCodeHash, err := r.tronCodeHash(ctx, endpoint, config.TokenAddress)
	if err != nil || tokenCodeHash != config.ExpectedTokenCodeHash {
		return routeProbe{}, errors.New("Tron token runtime code hash mismatch")
	}
	tokenParameter, err := tronAddressParameter(config.TokenAddress)
	if err != nil {
		return routeProbe{}, err
	}
	vaultParameter, err := tronAddressParameter(config.VaultAddress)
	if err != nil {
		return routeProbe{}, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		probe, probeErr := r.observeTronState(
			ctx, endpoint, config, vaultCodeHash, tokenCodeHash, tokenParameter, vaultParameter,
		)
		if probeErr == nil {
			return probe, nil
		}
		if !errors.Is(probeErr, errTronHeadAdvanced) {
			return routeProbe{}, probeErr
		}
	}
	return routeProbe{}, errTronHeadAdvanced
}

func (r *Reconciler) observeTronState(
	ctx context.Context,
	endpoint string,
	config RouteConfig,
	vaultCodeHash string,
	tokenCodeHash string,
	tokenParameter string,
	vaultParameter string,
) (routeProbe, error) {
	heightBefore, blockHashBefore, err := r.tronHead(ctx, endpoint)
	if err != nil {
		return routeProbe{}, err
	}
	if heightBefore < config.Confirmations {
		return routeProbe{}, errors.New("Tron solidified head is below confirmation policy")
	}

	type callResult struct {
		value *big.Int
		err   error
	}
	trackedResult := make(chan callResult, 1)
	balanceResult := make(chan callResult, 1)
	pausedResult := make(chan callResult, 1)
	routeResult := make(chan struct {
		values []*big.Int
		err    error
	}, 1)
	go func() {
		value, callErr := r.tronUintCall(ctx, endpoint, config.VaultAddress, config.VaultAddress, "trackedEscrow(address)", tokenParameter)
		trackedResult <- callResult{value: value, err: callErr}
	}()
	go func() {
		value, callErr := r.tronUintCall(ctx, endpoint, config.VaultAddress, config.TokenAddress, "balanceOf(address)", vaultParameter)
		balanceResult <- callResult{value: value, err: callErr}
	}()
	go func() {
		value, callErr := r.tronUintCall(ctx, endpoint, config.VaultAddress, config.VaultAddress, "paused()", "")
		pausedResult <- callResult{value: value, err: callErr}
	}()
	go func() {
		values, callErr := r.tronWordsCall(ctx, endpoint, config.VaultAddress, config.VaultAddress, "tokenRoutes(address)", tokenParameter, 5)
		routeResult <- struct {
			values []*big.Int
			err    error
		}{values: values, err: callErr}
	}()
	tracked, balance, paused, tokenRoute := <-trackedResult, <-balanceResult, <-pausedResult, <-routeResult
	if tracked.err != nil || balance.err != nil || paused.err != nil || tokenRoute.err != nil {
		return routeProbe{}, errors.New("Tron solidified contract state query failed")
	}
	if paused.value.Sign() != 0 && paused.value.Cmp(big.NewInt(1)) != 0 {
		return routeProbe{}, errors.New("Tron paused call returned an invalid boolean")
	}
	if tokenRoute.values[0].Sign() != 0 && tokenRoute.values[0].Cmp(big.NewInt(1)) != 0 {
		return routeProbe{}, errors.New("Tron token route returned an invalid enabled boolean")
	}
	want := []*big.Int{}
	for _, value := range []string{
		config.ExpectedMinAmountRaw, config.ExpectedMaxAmountRaw, config.ExpectedDailyLockRaw, config.ExpectedDailyUnlockRaw,
	} {
		parsed, _ := new(big.Int).SetString(value, 10)
		want = append(want, parsed)
	}
	if tokenRoute.values[0].Sign() == 0 || tokenRoute.values[1].Cmp(want[0]) != 0 || tokenRoute.values[2].Cmp(want[1]) != 0 ||
		tokenRoute.values[3].Cmp(want[2]) != 0 || tokenRoute.values[4].Cmp(want[3]) != 0 {
		return routeProbe{}, errors.New("Tron token route is disabled or does not match audited limits")
	}
	heightAfter, blockHashAfter, err := r.tronHead(ctx, endpoint)
	if err != nil {
		return routeProbe{}, err
	}
	if heightAfter != heightBefore || blockHashAfter != blockHashBefore {
		return routeProbe{}, errTronHeadAdvanced
	}
	fingerprint := strings.Join([]string{
		blockHashBefore, tracked.value.String(), balance.value.String(), paused.value.String(),
		tokenRoute.values[0].String(), tokenRoute.values[1].String(), tokenRoute.values[2].String(),
		tokenRoute.values[3].String(), tokenRoute.values[4].String(), vaultCodeHash, tokenCodeHash,
	}, "|")
	return routeProbe{
		Fingerprint:      fingerprint,
		EndpointHash:     endpointDigest(endpoint),
		BlockHeight:      heightBefore,
		BlockHash:        blockHashBefore,
		TrackedEscrow:    new(big.Int).Set(tracked.value),
		TokenBalance:     new(big.Int).Set(balance.value),
		Paused:           paused.value.Sign() != 0,
		RouteEnabled:     tokenRoute.values[0].Sign() != 0,
		RouteMinAmount:   new(big.Int).Set(tokenRoute.values[1]),
		RouteMaxAmount:   new(big.Int).Set(tokenRoute.values[2]),
		RouteDailyLock:   new(big.Int).Set(tokenRoute.values[3]),
		RouteDailyUnlock: new(big.Int).Set(tokenRoute.values[4]),
	}, nil
}

func (r *Reconciler) probeTronRoute(ctx context.Context, config RouteConfig) (evmProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	probes := make([]routeProbe, 0, len(config.RPCURLs))
	var probesMu sync.Mutex
	var wait sync.WaitGroup
	for _, endpoint := range config.RPCURLs {
		endpoint := endpoint
		wait.Add(1)
		go func() {
			defer wait.Done()
			probe, err := r.observeTronRoute(ctx, endpoint, config)
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
		return evmProbeResult{}, errors.New("Tron API split view: multiple conflicting states independently reached quorum")
	}
	if len(agreed) < config.RPCQuorum {
		return evmProbeResult{}, fmt.Errorf("Tron state quorum not met: best agreement %d need %d", len(agreed), config.RPCQuorum)
	}
	endpointHashes := make([]string, 0, len(agreed))
	for _, probe := range agreed {
		endpointHashes = append(endpointHashes, probe.EndpointHash)
	}
	sort.Strings(endpointHashes)
	selected := agreed[0]
	return evmProbeResult{
		BlockHeight:      selected.BlockHeight,
		BlockHash:        selected.BlockHash,
		TrackedEscrow:    new(big.Int).Set(selected.TrackedEscrow),
		TokenBalance:     new(big.Int).Set(selected.TokenBalance),
		Paused:           selected.Paused,
		RouteEnabled:     selected.RouteEnabled,
		RouteMinAmount:   new(big.Int).Set(selected.RouteMinAmount),
		RouteMaxAmount:   new(big.Int).Set(selected.RouteMaxAmount),
		RouteDailyLock:   new(big.Int).Set(selected.RouteDailyLock),
		RouteDailyUnlock: new(big.Int).Set(selected.RouteDailyUnlock),
		Agreed:           len(agreed),
		EndpointHashes:   endpointHashes,
	}, nil
}
