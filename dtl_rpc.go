package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
)

const (
	jsonRPCVersion         = "2.0"
	dtlNFTListLimitDefault = uint64(50)
	dtlNFTListLimitMax     = uint64(200)
)

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

func rpcError(code int, message string) *jsonRPCError {
	return &jsonRPCError{Code: code, Message: message}
}

func parseRPCID(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var id any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&id); err != nil {
		return nil
	}
	return id
}

func encodeRPCQuantityUint64(value uint64) string {
	return "0x" + strconv.FormatUint(value, 16)
}

func encodeRPCQuantityInt(value int) string {
	if value < 0 {
		return "-0x" + strconv.FormatInt(-int64(value), 16)
	}
	return "0x" + strconv.FormatInt(int64(value), 16)
}

func encodeRPCQuantityBig(value *big.Int) string {
	if value == nil {
		return "0x0"
	}
	if value.Sign() < 0 {
		return "-0x" + new(big.Int).Abs(value).Text(16)
	}
	return "0x" + value.Text(16)
}

// chainIDBigInt exposes the immutable protocol identifier to native RPC code.
func chainIDBigInt() *big.Int {
	value := new(big.Int)
	if _, ok := value.SetString(protocolChainID(), 10); ok {
		return value
	}
	return new(big.Int)
}

func parseRPCQuantity(raw json.RawMessage) (uint64, error) {
	return parseNativeRPCQuantity(raw)
}

func rpcParamsAsArray(raw json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []json.RawMessage{}, nil
	}
	var params []json.RawMessage
	if err := json.Unmarshal(trimmed, &params); err != nil {
		return nil, errors.New("params must be an array")
	}
	return params, nil
}

func parseRPCStringParam(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("invalid string parameter")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("empty string parameter")
	}
	return value, nil
}

func jsonRPCMethodNeedsSubmit(method string) bool {
	return strings.EqualFold(strings.TrimSpace(method), "dtl_submit")
}

func isRemovedVMRPCMethod(method string) bool {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" || strings.HasPrefix(method, "dtl_") {
		return false
	}
	for _, prefix := range []string{
		"eth_", "msc_", "web3_", "net_", "wallet_", "personal_",
		"debug_", "trace_", "txpool_", "engine_", "admin_", "miner_",
	} {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
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
	if matchesBearerToken(header, apiToken) || matchesBearerToken(header, apiReadToken) {
		return true
	}
	if strings.HasPrefix(header, "Bearer ") {
		return isValidAuthToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	}
	return false
}

func rpcStateTag(params []json.RawMessage, index int) json.RawMessage {
	if index < 0 || index >= len(params) {
		return nil
	}
	return params[index]
}

func rpcRequiredString(params []json.RawMessage, index int, label string) (string, *jsonRPCError) {
	if index < 0 || index >= len(params) {
		return "", rpcError(-32602, "missing "+label)
	}
	value, err := parseRPCStringParam(params[index])
	if err != nil {
		return "", rpcError(-32602, err.Error())
	}
	return value, nil
}

func rpcQueryError(err error) *jsonRPCError {
	if err == nil {
		return nil
	}
	return rpcError(-32000, err.Error())
}

func (s *Server) handleJSONRPCMethod(req jsonRPCRequest) jsonRPCResponse {
	resp := jsonRPCResponse{JSONRPC: jsonRPCVersion, ID: parseRPCID(req.ID)}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		resp.Error = rpcError(-32600, "invalid request: missing method")
		return resp
	}
	if isRemovedVMRPCMethod(method) {
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
		resp.Result = protocolChainID()

	case "dtl_tokenInfo":
		token, rpcErr := rpcRequiredString(params, 0, "token reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlTokenInfo(token, rpcStateTag(params, 1))

	case "dtl_balanceOf":
		token, rpcErr := rpcRequiredString(params, 0, "token reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		account, rpcErr := rpcRequiredString(params, 1, "account")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		var balance uint64
		balance, err = s.dtlBalanceOf(token, account, rpcStateTag(params, 2))
		if err == nil {
			resp.Result = encodeRPCQuantityUint64(balance)
		}

	case "dtl_totalSupply":
		token, rpcErr := rpcRequiredString(params, 0, "token reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		var supply uint64
		supply, err = s.dtlTotalSupply(token, rpcStateTag(params, 1))
		if err == nil {
			resp.Result = encodeRPCQuantityUint64(supply)
		}

	case "dtl_listTokens":
		account := ""
		var stateTag json.RawMessage
		if len(params) > 0 {
			if decodeErr := json.Unmarshal(params[0], &account); decodeErr != nil {
				stateTag = params[0]
			} else if len(params) > 1 {
				stateTag = params[1]
			}
		}
		resp.Result, err = s.dtlListTokens(account, stateTag)

	case "dtl_listNFT721ByOwner", "dtl_listNFT1155ByOwner":
		account, rpcErr := rpcRequiredString(params, 0, "owner account")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		offset, limit := uint64(0), dtlNFTListLimitDefault
		var stateTag json.RawMessage
		if len(params) > 1 {
			if parsed, parseErr := parseRPCQuantity(params[1]); parseErr == nil {
				offset = parsed
				if len(params) > 2 {
					if parsed, parseErr = parseRPCQuantity(params[2]); parseErr == nil {
						limit = parsed
						stateTag = rpcStateTag(params, 3)
					} else {
						stateTag = params[2]
					}
				}
			} else {
				stateTag = params[1]
			}
		}
		if method == "dtl_listNFT721ByOwner" {
			resp.Result, err = s.dtlListNFT721ByOwner(account, offset, limit, stateTag)
		} else {
			resp.Result, err = s.dtlListNFT1155ByOwner(account, offset, limit, stateTag)
		}

	case "dtl_poolInfo":
		ref, rpcErr := rpcRequiredString(params, 0, "pool reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlPoolInfo(ref, rpcStateTag(params, 1))

	case "dtl_listPools":
		resp.Result, err = s.dtlListPools(rpcStateTag(params, 0))

	case "dtl_farmInfo":
		ref, rpcErr := rpcRequiredString(params, 0, "farm reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlFarmInfo(ref, rpcStateTag(params, 1))

	case "dtl_positionFarm":
		ref, rpcErr := rpcRequiredString(params, 0, "farm reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		account, rpcErr := rpcRequiredString(params, 1, "account")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlPositionFarm(ref, account, rpcStateTag(params, 2))

	case "dtl_seasonInfo":
		season := ""
		var tag json.RawMessage
		if len(params) > 0 {
			if decodeErr := json.Unmarshal(params[0], &season); decodeErr != nil {
				tag = params[0]
			} else {
				tag = rpcStateTag(params, 1)
			}
		}
		resp.Result, err = s.dtlSeasonInfo(season, tag)

	case "dtl_leaderboard":
		season := ""
		limit := int(DTLDefaultLeaderboardLimit)
		if len(params) > 0 {
			if decodeErr := json.Unmarshal(params[0], &season); decodeErr != nil {
				resp.Error = rpcError(-32602, "invalid season reference")
				return resp
			}
		}
		var tag json.RawMessage
		if len(params) > 1 {
			if parsed, parseErr := parseRPCQuantity(params[1]); parseErr == nil && parsed > 0 {
				limit = int(parsed)
				tag = rpcStateTag(params, 2)
			} else {
				tag = params[1]
			}
		}
		resp.Result, err = s.dtlLeaderboard(season, limit, tag)

	case "dtl_routeQuote":
		tokenIn, rpcErr := rpcRequiredString(params, 0, "token_in")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		tokenOut, rpcErr := rpcRequiredString(params, 1, "token_out")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		if len(params) < 3 {
			resp.Error = rpcError(-32602, "missing amount_in")
			return resp
		}
		amountIn, parseErr := parseRPCQuantity(params[2])
		if parseErr != nil || amountIn == 0 {
			resp.Error = rpcError(-32602, "invalid amount_in")
			return resp
		}
		maxHops := dtlRouterMaxHops()
		var tag json.RawMessage
		if len(params) > 3 {
			if parsed, parseErr := parseRPCQuantity(params[3]); parseErr == nil && parsed > 0 {
				if int(parsed) < maxHops {
					maxHops = int(parsed)
				}
				tag = rpcStateTag(params, 4)
			} else {
				tag = params[3]
			}
		}
		resp.Result, err = s.dtlRouteQuote(tokenIn, tokenOut, amountIn, maxHops, tag)

	case "dtl_marketInfo":
		ref, rpcErr := rpcRequiredString(params, 0, "market reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlMarketInfo(ref, rpcStateTag(params, 1))

	case "dtl_positionOf":
		ref, rpcErr := rpcRequiredString(params, 0, "market reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		account, rpcErr := rpcRequiredString(params, 1, "account")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlPositionOf(ref, account, rpcStateTag(params, 2))

	case "dtl_tournamentInfo":
		ref, rpcErr := rpcRequiredString(params, 0, "tournament reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlTournamentInfo(ref, rpcStateTag(params, 1))

	case "dtl_contractInfo":
		ref, rpcErr := rpcRequiredString(params, 0, "contract reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		resp.Result, err = s.dtlContractInfo(ref, rpcStateTag(params, 1))

	case "dtl_contractStorage":
		ref, rpcErr := rpcRequiredString(params, 0, "contract reference")
		if rpcErr != nil {
			resp.Error = rpcErr
			return resp
		}
		key := ""
		var tag json.RawMessage
		if len(params) > 1 {
			if decodeErr := json.Unmarshal(params[1], &key); decodeErr != nil {
				tag = params[1]
			} else {
				tag = rpcStateTag(params, 2)
			}
		}
		resp.Result, err = s.dtlContractStorage(ref, key, tag)

	case "dtl_submit":
		if len(params) < 1 {
			resp.Error = rpcError(-32602, "missing transaction object")
			return resp
		}
		resp.Result, err = s.submitDTLTransactionObject(params[0])

	default:
		resp.Error = rpcError(-32601, "method not found")
		return resp
	}
	if err != nil {
		resp.Result = nil
		resp.Error = rpcQueryError(err)
	}
	return resp
}

func decodeJSONRPCRequest(raw []byte) (jsonRPCRequest, error) {
	var request jsonRPCRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return request, errors.New("trailing json data")
	}
	return request, nil
}

func writeJSONRPC(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		http.Error(w, "json-rpc websocket removed; use HTTP dtl_* methods", http.StatusGone)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxTxRequestBodyBytes+1))
	if err != nil || len(body) == 0 || int64(len(body)) > MaxTxRequestBodyBytes {
		writeJSONRPC(w, jsonRPCResponse{JSONRPC: jsonRPCVersion, Error: rpcError(-32700, "invalid request body")})
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var rawRequests []json.RawMessage
		if err := json.Unmarshal(trimmed, &rawRequests); err != nil || len(rawRequests) == 0 {
			writeJSONRPC(w, jsonRPCResponse{JSONRPC: jsonRPCVersion, Error: rpcError(-32700, "parse error")})
			return
		}
		requests := make([]jsonRPCRequest, len(rawRequests))
		requireSubmit := false
		for i, raw := range rawRequests {
			request, decodeErr := decodeJSONRPCRequest(raw)
			if decodeErr != nil {
				writeJSONRPC(w, jsonRPCResponse{JSONRPC: jsonRPCVersion, Error: rpcError(-32600, "invalid request")})
				return
			}
			requests[i] = request
			requireSubmit = requireSubmit || jsonRPCMethodNeedsSubmit(request.Method)
		}
		if !authorizeJSONRPCRequest(r, requireSubmit) {
			writeJSONRPC(w, jsonRPCResponse{JSONRPC: jsonRPCVersion, Error: rpcError(-32001, "unauthorized")})
			return
		}
		responses := make([]jsonRPCResponse, 0, len(requests))
		for _, request := range requests {
			responses = append(responses, s.handleJSONRPCMethod(request))
		}
		writeJSONRPC(w, responses)
		return
	}
	request, err := decodeJSONRPCRequest(trimmed)
	if err != nil {
		writeJSONRPC(w, jsonRPCResponse{JSONRPC: jsonRPCVersion, Error: rpcError(-32600, "invalid request")})
		return
	}
	if !authorizeJSONRPCRequest(r, jsonRPCMethodNeedsSubmit(request.Method)) {
		writeJSONRPC(w, jsonRPCResponse{JSONRPC: jsonRPCVersion, ID: parseRPCID(request.ID), Error: rpcError(-32001, "unauthorized")})
		return
	}
	writeJSONRPC(w, s.handleJSONRPCMethod(request))
}
