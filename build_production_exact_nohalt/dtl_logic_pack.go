package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"
)

const (
	dtlLogicRegPrefix = "r"
	dtlLogicRegMax    = 31
)

type dtlLogicValue struct {
	kind string
	u64  uint64
	str  string
	b    bool
}

func dtlLogicValueU64(v uint64) dtlLogicValue { return dtlLogicValue{kind: "u64", u64: v} }
func dtlLogicValueStr(v string) dtlLogicValue {
	return dtlLogicValue{kind: "string", str: strings.TrimSpace(v)}
}
func dtlLogicValueBool(v bool) dtlLogicValue { return dtlLogicValue{kind: "bool", b: v} }

func isDTLLogicIdentifier(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > DTLMaxContractKeyLen {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func normalizeDTLLogicIdentifier(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if !isDTLLogicIdentifier(s) {
		return "", fmt.Errorf("dtl: invalid logic identifier: %s", raw)
	}
	return s, nil
}

func normalizeDTLLogicType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isDTLLogicType(raw string) bool {
	switch normalizeDTLLogicType(raw) {
	case "u64", "string", "bool", "address":
		return true
	default:
		return false
	}
}

func normalizeDTLLogicRegister(raw string) (string, error) {
	reg := strings.ToLower(strings.TrimSpace(raw))
	if !strings.HasPrefix(reg, dtlLogicRegPrefix) {
		return "", fmt.Errorf("dtl: invalid logic register: %s", raw)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(reg, dtlLogicRegPrefix))
	if err != nil || n < 0 || n > dtlLogicRegMax {
		return "", fmt.Errorf("dtl: invalid logic register: %s", raw)
	}
	return fmt.Sprintf("%s%d", dtlLogicRegPrefix, n), nil
}

func normalizeDTLLogicArgs(raw map[string]string) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	return out
}

func cloneDTLLogicPack(src *DTLLogicPack) *DTLLogicPack {
	if src == nil {
		return nil
	}
	out := &DTLLogicPack{
		Version: src.Version,
		Name:    src.Name,
		Limits:  src.Limits,
	}
	if len(src.ABI) > 0 {
		out.ABI = make([]DTLLogicPackABIMethod, 0, len(src.ABI))
		for _, m := range src.ABI {
			cm := DTLLogicPackABIMethod{Name: m.Name}
			if len(m.Args) > 0 {
				cm.Args = make([]DTLLogicPackArg, 0, len(m.Args))
				for _, a := range m.Args {
					cm.Args = append(cm.Args, DTLLogicPackArg{Name: a.Name, Type: a.Type})
				}
			}
			if len(m.Returns) > 0 {
				cm.Returns = append([]string(nil), m.Returns...)
			}
			out.ABI = append(out.ABI, cm)
		}
	}
	if len(src.Storage) > 0 {
		out.Storage = make([]DTLLogicPackStorageField, 0, len(src.Storage))
		for _, s := range src.Storage {
			out.Storage = append(out.Storage, DTLLogicPackStorageField{
				Key:  s.Key,
				Type: s.Type,
				Init: s.Init,
			})
		}
	}
	if len(src.Methods) > 0 {
		out.Methods = make([]DTLLogicPackMethod, 0, len(src.Methods))
		for _, m := range src.Methods {
			cm := DTLLogicPackMethod{
				Name:     m.Name,
				MaxSteps: m.MaxSteps,
			}
			if len(m.Ops) > 0 {
				cm.Ops = make([]DTLLogicPackOp, 0, len(m.Ops))
				for _, op := range m.Ops {
					cm.Ops = append(cm.Ops, DTLLogicPackOp{
						Op:               op.Op,
						Dest:             op.Dest,
						A:                op.A,
						B:                op.B,
						Src:              op.Src,
						Cond:             op.Cond,
						Key:              op.Key,
						Arg:              op.Arg,
						TokenID:          op.TokenID,
						TokenArg:         op.TokenArg,
						ToArg:            op.ToArg,
						AmountArg:        op.AmountArg,
						FromArg:          op.FromArg,
						SpenderArg:       op.SpenderArg,
						NameArg:          op.NameArg,
						SymbolArg:        op.SymbolArg,
						DecimalsArg:      op.DecimalsArg,
						MaxSupplyArg:     op.MaxSupplyArg,
						InitialSupplyArg: op.InitialSupplyArg,
						From:             op.From,
						Message:          op.Message,
						Target:           op.Target,
						Map:              op.Map,
						MapKeyArg:        op.MapKeyArg,
						Topic0Arg:        op.Topic0Arg,
						Topic1Arg:        op.Topic1Arg,
						Topic2Arg:        op.Topic2Arg,
						Topic3Arg:        op.Topic3Arg,
						DataArg:          op.DataArg,
					})
				}
			}
			out.Methods = append(out.Methods, cm)
		}
	}
	return out
}

func dtlLogicPackHash(pack *DTLLogicPack) (string, error) {
	if pack == nil {
		return "", fmt.Errorf("dtl: nil logic pack")
	}
	return DTLPayloadHash(pack)
}

func validateDTLLogicPackOpcode(raw string) (string, error) {
	op := strings.ToUpper(strings.TrimSpace(raw))
	switch op {
	case "ARG_U64", "ARG_STR", "ARG_BOOL",
		"LOAD_U64", "LOAD_STR",
		"STORE_U64", "STORE_STR",
		"ADD_U64", "SUB_U64", "MUL_U64", "DIV_U64", "MUL_DIV_U64", "MIN_U64", "MAX_U64",
		"CMP_EQ", "CMP_NEQ", "CMP_GT", "CMP_GTE", "CMP_LT", "CMP_LTE",
		"JMP_IF", "JMP",
		"ASSERT",
		"TOKEN_TRANSFER", "TOKEN_CREATE", "TOKEN_MINT", "TOKEN_BURN", "TOKEN_APPROVE", "TOKEN_TRANSFER_FROM",
		"CTX_CALLER", "CTX_CONTRACT", "CTX_CHAIN_ID", "CTX_BLOCK_HEIGHT", "CTX_BLOCK_HASH", "CTX_PREV_BLOCK_HASH",
		"MAP_GET_U64", "MAP_SET_U64", "MAP_GET_STR", "MAP_SET_STR", "MAP_GET_BOOL", "MAP_SET_BOOL",
		"REQUIRE_ROLE", "GRANT_ROLE", "REVOKE_ROLE", "CALL_DTL_RO",
		"EMIT_LOG",
		"RET_U64", "RET_STR", "RET_BOOL",
		"RET_OK", "RET_ERR":
		return op, nil
	default:
		return "", fmt.Errorf("dtl: unsupported logic opcode: %s", raw)
	}
}

func isDTLLogicPackV2Opcode(op string) bool {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "CTX_CALLER", "CTX_CONTRACT", "CTX_CHAIN_ID", "CTX_BLOCK_HEIGHT",
		"MAP_GET_U64", "MAP_SET_U64", "MAP_GET_STR", "MAP_SET_STR",
		"EMIT_LOG",
		"RET_U64", "RET_STR", "RET_BOOL":
		return true
	default:
		return false
	}
}

func isDTLLogicPackV3Opcode(op string) bool {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "ARG_BOOL",
		"MUL_DIV_U64", "MIN_U64", "MAX_U64",
		"CTX_BLOCK_HASH", "CTX_PREV_BLOCK_HASH",
		"MAP_GET_BOOL", "MAP_SET_BOOL",
		"REQUIRE_ROLE", "GRANT_ROLE", "REVOKE_ROLE", "CALL_DTL_RO":
		return true
	default:
		return false
	}
}

func normalizeDTLLogicPackLimits(raw DTLLogicPackLimits) (DTLLogicPackLimits, error) {
	out := raw
	if out.MaxReads == 0 {
		out.MaxReads = DTLDefaultLogicPackReads
	}
	if out.MaxWrites == 0 {
		out.MaxWrites = DTLDefaultLogicPackWrites
	}
	if out.MaxTokenTransfers == 0 {
		out.MaxTokenTransfers = DTLDefaultLogicPackTransfers
	}
	if out.MaxLogs == 0 {
		out.MaxLogs = DTLDefaultLogicPackLogs
	}
	if out.MaxMapReads == 0 {
		out.MaxMapReads = DTLDefaultLogicPackMapReads
	}
	if out.MaxMapWrites == 0 {
		out.MaxMapWrites = DTLDefaultLogicPackMapWrites
	}
	if out.MaxCrossCalls == 0 {
		out.MaxCrossCalls = DTLDefaultLogicPackCrossCalls
	}
	if out.MaxRoleOps == 0 {
		out.MaxRoleOps = DTLDefaultLogicPackRoleOps
	}
	if out.MaxReads > DTLMaxLogicPackReads {
		return out, fmt.Errorf("dtl: logic pack max_reads exceeds cap")
	}
	if out.MaxWrites > DTLMaxLogicPackWrites {
		return out, fmt.Errorf("dtl: logic pack max_writes exceeds cap")
	}
	if out.MaxTokenTransfers > DTLMaxLogicPackTransfers {
		return out, fmt.Errorf("dtl: logic pack max_token_transfers exceeds cap")
	}
	if out.MaxLogs > DTLMaxLogicPackLogs {
		return out, fmt.Errorf("dtl: logic pack max_logs exceeds cap")
	}
	if out.MaxMapReads > DTLMaxLogicPackMapReads {
		return out, fmt.Errorf("dtl: logic pack max_map_reads exceeds cap")
	}
	if out.MaxMapWrites > DTLMaxLogicPackMapWrites {
		return out, fmt.Errorf("dtl: logic pack max_map_writes exceeds cap")
	}
	if out.MaxCrossCalls > DTLMaxLogicPackCrossCalls {
		return out, fmt.Errorf("dtl: logic pack max_cross_calls exceeds cap")
	}
	if out.MaxRoleOps > DTLMaxLogicPackRoleOps {
		return out, fmt.Errorf("dtl: logic pack max_role_ops exceeds cap")
	}
	return out, nil
}

func validateAndNormalizeDTLLogicPack(state *DTLState, pack *DTLLogicPack) (*DTLLogicPack, error) {
	if pack == nil {
		return nil, fmt.Errorf("dtl: missing logic pack")
	}
	if state == nil {
		return nil, ErrDTLInvalidState
	}
	state.ensure()

	if pack.Version != DTLLogicPackVersionV1 && pack.Version != DTLLogicPackVersionV2 && pack.Version != DTLLogicPackVersionV3 {
		return nil, fmt.Errorf("dtl: unsupported logic pack version")
	}
	name, err := normalizeDTLLogicIdentifier(pack.Name)
	if err != nil {
		return nil, err
	}

	limits, err := normalizeDTLLogicPackLimits(pack.Limits)
	if err != nil {
		return nil, err
	}

	if len(pack.ABI) == 0 || len(pack.ABI) > DTLMaxContractMethods {
		return nil, fmt.Errorf("dtl: invalid logic pack abi method count")
	}
	if len(pack.Methods) == 0 || len(pack.Methods) > DTLMaxContractMethods {
		return nil, fmt.Errorf("dtl: invalid logic pack method count")
	}
	if len(pack.Storage) > DTLMaxLogicPackStorage {
		return nil, fmt.Errorf("dtl: logic pack storage field limit exceeded")
	}

	out := &DTLLogicPack{
		Version: pack.Version,
		Name:    name,
		Limits:  limits,
	}

	storageTypes := make(map[string]string, len(pack.Storage))
	out.Storage = make([]DTLLogicPackStorageField, 0, len(pack.Storage))
	for _, field := range pack.Storage {
		key, err := normalizeDTLLogicIdentifier(field.Key)
		if err != nil {
			return nil, err
		}
		if _, exists := storageTypes[key]; exists {
			return nil, fmt.Errorf("dtl: duplicate logic pack storage key: %s", key)
		}
		fieldType := normalizeDTLLogicType(field.Type)
		switch fieldType {
		case "u64", "string", "bool":
		default:
			return nil, fmt.Errorf("dtl: unsupported storage type for key %s", key)
		}
		initValue := strings.TrimSpace(field.Init)
		switch fieldType {
		case "u64":
			if initValue == "" {
				initValue = "0"
			}
			if _, err := strconv.ParseUint(initValue, 10, 64); err != nil {
				return nil, fmt.Errorf("dtl: storage init for %s must be uint64", key)
			}
		case "bool":
			if initValue == "" {
				initValue = "false"
			}
			if _, err := strconv.ParseBool(strings.ToLower(initValue)); err != nil {
				return nil, fmt.Errorf("dtl: storage init for %s must be bool", key)
			}
		case "string":
			if len(initValue) > DTLMaxContractValueLen {
				return nil, fmt.Errorf("dtl: storage init value too long for key %s", key)
			}
		}
		storageTypes[key] = fieldType
		out.Storage = append(out.Storage, DTLLogicPackStorageField{
			Key:  key,
			Type: fieldType,
			Init: initValue,
		})
	}
	sort.Slice(out.Storage, func(i, j int) bool { return out.Storage[i].Key < out.Storage[j].Key })

	abiMethods := make(map[string]DTLLogicPackABIMethod, len(pack.ABI))
	for _, method := range pack.ABI {
		methodName, err := normalizeDTLLogicIdentifier(method.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := abiMethods[methodName]; exists {
			return nil, fmt.Errorf("dtl: duplicate abi method: %s", methodName)
		}
		if len(method.Args) > 16 {
			return nil, fmt.Errorf("dtl: abi method %s arg limit exceeded", methodName)
		}
		if len(method.Returns) > 4 {
			return nil, fmt.Errorf("dtl: abi method %s return limit exceeded", methodName)
		}
		seenArgs := make(map[string]struct{}, len(method.Args))
		args := make([]DTLLogicPackArg, 0, len(method.Args))
		for _, arg := range method.Args {
			argName, err := normalizeDTLLogicIdentifier(arg.Name)
			if err != nil {
				return nil, err
			}
			if _, exists := seenArgs[argName]; exists {
				return nil, fmt.Errorf("dtl: duplicate abi arg %s in method %s", argName, methodName)
			}
			seenArgs[argName] = struct{}{}
			argType := normalizeDTLLogicType(arg.Type)
			if !isDTLLogicType(argType) {
				return nil, fmt.Errorf("dtl: invalid abi arg type in method %s", methodName)
			}
			args = append(args, DTLLogicPackArg{Name: argName, Type: argType})
		}
		returns := make([]string, 0, len(method.Returns))
		for _, ret := range method.Returns {
			retType := normalizeDTLLogicType(ret)
			if !isDTLLogicType(retType) {
				return nil, fmt.Errorf("dtl: invalid abi return type in method %s", methodName)
			}
			returns = append(returns, retType)
		}
		abiMethods[methodName] = DTLLogicPackABIMethod{
			Name:    methodName,
			Args:    args,
			Returns: returns,
		}
	}
	abiNames := make([]string, 0, len(abiMethods))
	for name := range abiMethods {
		abiNames = append(abiNames, name)
	}
	sort.Strings(abiNames)
	out.ABI = make([]DTLLogicPackABIMethod, 0, len(abiNames))
	for _, name := range abiNames {
		out.ABI = append(out.ABI, abiMethods[name])
	}

	totalOps := 0
	methodMap := make(map[string]DTLLogicPackMethod, len(pack.Methods))
	for _, method := range pack.Methods {
		methodName, err := normalizeDTLLogicIdentifier(method.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := methodMap[methodName]; exists {
			return nil, fmt.Errorf("dtl: duplicate logic method: %s", methodName)
		}
		abi, exists := abiMethods[methodName]
		if !exists {
			return nil, fmt.Errorf("dtl: logic method %s missing in abi", methodName)
		}
		if len(method.Ops) == 0 || len(method.Ops) > DTLMaxLogicPackOps {
			return nil, fmt.Errorf("dtl: invalid op count for method %s", methodName)
		}
		totalOps += len(method.Ops)
		if totalOps > DTLMaxLogicPackTotalOps {
			return nil, fmt.Errorf("dtl: logic pack total ops limit exceeded")
		}
		maxSteps := method.MaxSteps
		if maxSteps == 0 {
			maxSteps = uint16(len(method.Ops) + 1)
		}
		if maxSteps > DTLMaxLogicPackSteps {
			return nil, fmt.Errorf("dtl: method %s max_steps exceeds cap", methodName)
		}

		ops := make([]DTLLogicPackOp, 0, len(method.Ops))
		for idx, op := range method.Ops {
			normalizedOp, err := normalizeAndValidateDTLLogicOp(state, storageTypes, abi, pack.Version, op, idx, len(method.Ops))
			if err != nil {
				return nil, fmt.Errorf("dtl: method %s op %d: %w", methodName, idx, err)
			}
			ops = append(ops, normalizedOp)
		}
		lastOp := strings.ToUpper(strings.TrimSpace(ops[len(ops)-1].Op))
		if lastOp != "RET_OK" && lastOp != "RET_ERR" && lastOp != "RET_U64" && lastOp != "RET_STR" && lastOp != "RET_BOOL" {
			return nil, fmt.Errorf("dtl: method %s must end with RET_OK/RET_ERR/RET_*", methodName)
		}

		methodMap[methodName] = DTLLogicPackMethod{
			Name:     methodName,
			MaxSteps: maxSteps,
			Ops:      ops,
		}
	}
	for _, abiName := range abiNames {
		if _, exists := methodMap[abiName]; !exists {
			return nil, fmt.Errorf("dtl: abi method %s missing implementation", abiName)
		}
	}

	methodNames := make([]string, 0, len(methodMap))
	for name := range methodMap {
		methodNames = append(methodNames, name)
	}
	sort.Strings(methodNames)
	out.Methods = make([]DTLLogicPackMethod, 0, len(methodNames))
	for _, methodName := range methodNames {
		out.Methods = append(out.Methods, methodMap[methodName])
	}

	return out, nil
}

func normalizeAndValidateDTLLogicOp(
	state *DTLState,
	storageTypes map[string]string,
	abi DTLLogicPackABIMethod,
	packVersion uint16,
	op DTLLogicPackOp,
	index int,
	opCount int,
) (DTLLogicPackOp, error) {
	norm := DTLLogicPackOp{}
	var err error
	norm.Op, err = validateDTLLogicPackOpcode(op.Op)
	if err != nil {
		return norm, err
	}
	if packVersion < DTLLogicPackVersionV2 && isDTLLogicPackV2Opcode(norm.Op) {
		return norm, fmt.Errorf("opcode %s requires logic pack v2", norm.Op)
	}
	if packVersion < DTLLogicPackVersionV3 && isDTLLogicPackV3Opcode(norm.Op) {
		return norm, fmt.Errorf("opcode %s requires logic pack v3", norm.Op)
	}

	abiArgs := make(map[string]string, len(abi.Args))
	for _, arg := range abi.Args {
		abiArgs[strings.ToLower(strings.TrimSpace(arg.Name))] = normalizeDTLLogicType(arg.Type)
	}
	argType := func(name string) string {
		if t, exists := abiArgs[name]; exists {
			return t
		}
		switch name {
		case "caller", "contract":
			return "address"
		default:
			if strings.HasPrefix(name, "concat(") && strings.HasSuffix(name, ")") {
				return "string"
			}
			return ""
		}
	}
	isSpecialArg := func(name string) bool {
		return name == "caller" || name == "contract"
	}
	normalizeArg := func(raw string) (string, error) {
		in := strings.TrimSpace(raw)
		if strings.HasPrefix(in, "concat(") && strings.HasSuffix(in, ")") {
			body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(in, "concat("), ")"))
			if body == "" {
				return "", fmt.Errorf("unknown abi arg: %s", raw)
			}
			parts := strings.Split(body, ",")
			normalizedParts := make([]string, 0, len(parts))
			for _, part := range parts {
				name, err := normalizeDTLLogicIdentifier(part)
				if err != nil {
					return "", err
				}
				if _, exists := abiArgs[name]; !exists && !isSpecialArg(name) {
					return "", fmt.Errorf("unknown abi arg: %s", name)
				}
				normalizedParts = append(normalizedParts, name)
			}
			return "concat(" + strings.Join(normalizedParts, ",") + ")", nil
		}
		name, err := normalizeDTLLogicIdentifier(in)
		if err != nil {
			return "", err
		}
		if _, exists := abiArgs[name]; !exists && !isSpecialArg(name) {
			return "", fmt.Errorf("unknown abi arg: %s", name)
		}
		return name, nil
	}
	normalizeStorageKey := func(raw string) (string, error) {
		key, err := normalizeDTLLogicIdentifier(raw)
		if err != nil {
			return "", err
		}
		if _, exists := storageTypes[key]; !exists {
			return "", fmt.Errorf("unknown storage key: %s", key)
		}
		return key, nil
	}
	normalizeMapName := func(raw string) (string, error) {
		name, err := normalizeDTLLogicIdentifier(raw)
		if err != nil {
			return "", err
		}
		return name, nil
	}
	normalizeTarget := func(target int) (int, error) {
		if target < 0 || target >= opCount {
			return 0, fmt.Errorf("jump target out of range")
		}
		if target <= index {
			return 0, fmt.Errorf("backward jump is not allowed")
		}
		return target, nil
	}
	normalizeFromMode := func(raw, defaultMode string) (string, error) {
		mode := strings.ToLower(strings.TrimSpace(raw))
		if mode == "" {
			mode = defaultMode
		}
		if mode == "" {
			mode = "caller"
		}
		if mode != "caller" && mode != "contract" {
			return "", fmt.Errorf("invalid from mode")
		}
		return mode, nil
	}
	normalizeStringArg := func(raw, field string) (string, error) {
		if strings.HasPrefix(strings.TrimSpace(raw), "literal:") {
			lit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "literal:"))
			if lit == "" {
				return "", fmt.Errorf("%s must not be empty", field)
			}
			if len(lit) > DTLMaxContractValueLen {
				return "", fmt.Errorf("%s too long", field)
			}
			return "literal:" + lit, nil
		}
		name, err := normalizeArg(raw)
		if err != nil {
			return "", err
		}
		t := argType(name)
		if field == "data_arg" {
			if t == "" {
				return "", fmt.Errorf("%s must be string", field)
			}
			return name, nil
		}
		if t != "string" && t != "address" {
			return "", fmt.Errorf("%s must be string", field)
		}
		return name, nil
	}
	normalizeTopicArg := func(raw, field string) (string, error) {
		if strings.HasPrefix(strings.TrimSpace(raw), "literal:") {
			lit := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "literal:"))
			if lit == "" {
				return "", fmt.Errorf("%s must not be empty", field)
			}
			if len(lit) > DTLMaxContractValueLen {
				return "", fmt.Errorf("%s too long", field)
			}
			return "literal:" + lit, nil
		}
		name, err := normalizeArg(raw)
		if err != nil {
			return "", err
		}
		t := argType(name)
		if t != "string" && t != "address" {
			return "", fmt.Errorf("%s must be string/address", field)
		}
		return name, nil
	}
	normalizeAddressArg := func(raw, field string) (string, error) {
		name, err := normalizeArg(raw)
		if err != nil {
			return "", err
		}
		t := argType(name)
		if t != "address" && t != "string" {
			return "", fmt.Errorf("%s must be address/string", field)
		}
		return name, nil
	}
	normalizeU64Arg := func(raw, fallback, field string) (string, error) {
		argRaw := strings.TrimSpace(raw)
		if argRaw == "" {
			argRaw = strings.TrimSpace(fallback)
		}
		name, err := normalizeArg(argRaw)
		if err != nil {
			return "", err
		}
		if argType(name) != "u64" {
			return "", fmt.Errorf("%s must be u64", field)
		}
		return name, nil
	}
	normalizeTokenRef := func(rawTokenID, rawTokenArg, opName string) (string, string, error) {
		tokenArgRaw := strings.TrimSpace(rawTokenArg)
		if tokenArgRaw != "" {
			tokenArg, err := normalizeArg(tokenArgRaw)
			if err != nil {
				return "", "", err
			}
			if argType(tokenArg) != "string" && argType(tokenArg) != "address" {
				return "", "", fmt.Errorf("token_arg must be string")
			}
			return "", tokenArg, nil
		}
		tokenID, ok := resolveDTLTokenRef(state, rawTokenID)
		if tokenID == "" {
			return "", "", fmt.Errorf("%s requires token reference", strings.ToLower(opName))
		}
		if !ok {
			return "", "", ErrDTLUnknownToken
		}
		return tokenID, "", nil
	}

	switch norm.Op {
	case "ARG_U64", "ARG_STR", "ARG_BOOL":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.Arg, err = normalizeArg(op.Arg)
		if err != nil {
			return norm, err
		}
		if norm.Op == "ARG_U64" && argType(norm.Arg) != "u64" {
			return norm, fmt.Errorf("arg %s type mismatch for ARG_U64", norm.Arg)
		}
		if norm.Op == "ARG_STR" && argType(norm.Arg) != "string" && argType(norm.Arg) != "address" {
			return norm, fmt.Errorf("arg %s type mismatch for ARG_STR", norm.Arg)
		}
		if norm.Op == "ARG_BOOL" && argType(norm.Arg) != "bool" {
			return norm, fmt.Errorf("arg %s type mismatch for ARG_BOOL", norm.Arg)
		}
	case "LOAD_U64", "LOAD_STR":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.Key, err = normalizeStorageKey(op.Key)
		if err != nil {
			return norm, err
		}
		st := storageTypes[norm.Key]
		if norm.Op == "LOAD_U64" && st != "u64" {
			return norm, fmt.Errorf("storage key %s is not u64", norm.Key)
		}
		if norm.Op == "LOAD_STR" && st != "string" {
			return norm, fmt.Errorf("storage key %s is not string", norm.Key)
		}
	case "STORE_U64", "STORE_STR":
		norm.Src, err = normalizeDTLLogicRegister(op.Src)
		if err != nil {
			return norm, err
		}
		norm.Key, err = normalizeStorageKey(op.Key)
		if err != nil {
			return norm, err
		}
		st := storageTypes[norm.Key]
		if norm.Op == "STORE_U64" && st != "u64" {
			return norm, fmt.Errorf("storage key %s is not u64", norm.Key)
		}
		if norm.Op == "STORE_STR" && st != "string" {
			return norm, fmt.Errorf("storage key %s is not string", norm.Key)
		}
	case "ADD_U64", "SUB_U64", "MUL_U64", "DIV_U64", "MIN_U64", "MAX_U64":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.A, err = normalizeDTLLogicRegister(op.A)
		if err != nil {
			return norm, err
		}
		norm.B, err = normalizeDTLLogicRegister(op.B)
		if err != nil {
			return norm, err
		}
	case "MUL_DIV_U64":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.A, err = normalizeDTLLogicRegister(op.A)
		if err != nil {
			return norm, err
		}
		norm.B, err = normalizeDTLLogicRegister(op.B)
		if err != nil {
			return norm, err
		}
		norm.Src, err = normalizeDTLLogicRegister(op.Src)
		if err != nil {
			return norm, err
		}
	case "CMP_EQ", "CMP_NEQ", "CMP_GT", "CMP_GTE", "CMP_LT", "CMP_LTE":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.A, err = normalizeDTLLogicRegister(op.A)
		if err != nil {
			return norm, err
		}
		norm.B, err = normalizeDTLLogicRegister(op.B)
		if err != nil {
			return norm, err
		}
	case "JMP_IF":
		norm.Cond, err = normalizeDTLLogicRegister(op.Cond)
		if err != nil {
			return norm, err
		}
		norm.Target, err = normalizeTarget(op.Target)
		if err != nil {
			return norm, err
		}
	case "JMP":
		norm.Target, err = normalizeTarget(op.Target)
		if err != nil {
			return norm, err
		}
	case "ASSERT":
		norm.Cond, err = normalizeDTLLogicRegister(op.Cond)
		if err != nil {
			return norm, err
		}
		norm.Message = strings.TrimSpace(op.Message)
		if len(norm.Message) > DTLMaxContractValueLen {
			return norm, fmt.Errorf("assert message too long")
		}
	case "CTX_CALLER", "CTX_CONTRACT", "CTX_CHAIN_ID", "CTX_BLOCK_HEIGHT", "CTX_BLOCK_HASH", "CTX_PREV_BLOCK_HASH":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
	case "MAP_GET_U64", "MAP_GET_STR", "MAP_GET_BOOL":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.Map, err = normalizeMapName(op.Map)
		if err != nil {
			return norm, err
		}
		norm.MapKeyArg, err = normalizeArg(op.MapKeyArg)
		if err != nil {
			return norm, err
		}
	case "MAP_SET_U64", "MAP_SET_STR", "MAP_SET_BOOL":
		norm.Map, err = normalizeMapName(op.Map)
		if err != nil {
			return norm, err
		}
		norm.MapKeyArg, err = normalizeArg(op.MapKeyArg)
		if err != nil {
			return norm, err
		}
		norm.Src, err = normalizeDTLLogicRegister(op.Src)
		if err != nil {
			return norm, err
		}
	case "REQUIRE_ROLE":
		norm.Key, err = normalizeDTLLogicIdentifier(op.Key)
		if err != nil {
			return norm, err
		}
	case "GRANT_ROLE", "REVOKE_ROLE":
		norm.Key, err = normalizeDTLLogicIdentifier(op.Key)
		if err != nil {
			return norm, err
		}
		norm.ToArg, err = normalizeAddressArg(op.ToArg, "to_arg")
		if err != nil {
			return norm, err
		}
	case "CALL_DTL_RO":
		norm.Dest, err = normalizeDTLLogicRegister(op.Dest)
		if err != nil {
			return norm, err
		}
		norm.Key, err = normalizeArg(op.Key)
		if err != nil {
			return norm, err
		}
		norm.Arg, err = normalizeArg(op.Arg)
		if err != nil {
			return norm, err
		}
	case "EMIT_LOG":
		norm.Topic0Arg, err = normalizeTopicArg(op.Topic0Arg, "topic0_arg")
		if err != nil {
			return norm, err
		}
		if strings.TrimSpace(op.Topic1Arg) != "" {
			norm.Topic1Arg, err = normalizeTopicArg(op.Topic1Arg, "topic1_arg")
			if err != nil {
				return norm, err
			}
		}
		if strings.TrimSpace(op.Topic2Arg) != "" {
			norm.Topic2Arg, err = normalizeTopicArg(op.Topic2Arg, "topic2_arg")
			if err != nil {
				return norm, err
			}
		}
		if strings.TrimSpace(op.Topic3Arg) != "" {
			norm.Topic3Arg, err = normalizeTopicArg(op.Topic3Arg, "topic3_arg")
			if err != nil {
				return norm, err
			}
		}
		norm.DataArg, err = normalizeStringArg(op.DataArg, "data_arg")
		if err != nil {
			return norm, err
		}
	case "RET_U64", "RET_STR", "RET_BOOL":
		norm.Src, err = normalizeDTLLogicRegister(op.Src)
		if err != nil {
			return norm, err
		}
		if len(abi.Returns) > 0 {
			retType := normalizeDTLLogicType(abi.Returns[0])
			switch norm.Op {
			case "RET_U64":
				if retType != "u64" {
					return norm, fmt.Errorf("ret_u64 requires first abi return u64")
				}
			case "RET_STR":
				if retType != "string" && retType != "address" {
					return norm, fmt.Errorf("ret_str requires first abi return string/address")
				}
			case "RET_BOOL":
				if retType != "bool" {
					return norm, fmt.Errorf("ret_bool requires first abi return bool")
				}
			}
		}
	case "TOKEN_TRANSFER":
		tokenID, tokenArg, err := normalizeTokenRef(op.TokenID, op.TokenArg, norm.Op)
		if err != nil {
			return norm, err
		}
		norm.TokenID = tokenID
		norm.TokenArg = tokenArg
		norm.From, err = normalizeFromMode(op.From, "caller")
		if err != nil {
			return norm, err
		}
		norm.ToArg, err = normalizeAddressArg(op.ToArg, "to_arg")
		if err != nil {
			return norm, err
		}
		norm.AmountArg, err = normalizeU64Arg(op.AmountArg, op.Arg, "amount_arg")
		if err != nil {
			return norm, err
		}
	case "TOKEN_APPROVE":
		tokenID, tokenArg, err := normalizeTokenRef(op.TokenID, op.TokenArg, norm.Op)
		if err != nil {
			return norm, err
		}
		norm.TokenID = tokenID
		norm.TokenArg = tokenArg
		norm.From, err = normalizeFromMode(op.From, "caller")
		if err != nil {
			return norm, err
		}
		norm.SpenderArg, err = normalizeAddressArg(op.SpenderArg, "spender_arg")
		if err != nil {
			return norm, err
		}
		norm.AmountArg, err = normalizeU64Arg(op.AmountArg, op.Arg, "amount_arg")
		if err != nil {
			return norm, err
		}
	case "TOKEN_TRANSFER_FROM":
		tokenID, tokenArg, err := normalizeTokenRef(op.TokenID, op.TokenArg, norm.Op)
		if err != nil {
			return norm, err
		}
		norm.TokenID = tokenID
		norm.TokenArg = tokenArg
		norm.From, err = normalizeFromMode(op.From, "caller")
		if err != nil {
			return norm, err
		}
		norm.FromArg, err = normalizeAddressArg(op.FromArg, "from_arg")
		if err != nil {
			return norm, err
		}
		norm.ToArg, err = normalizeAddressArg(op.ToArg, "to_arg")
		if err != nil {
			return norm, err
		}
		norm.AmountArg, err = normalizeU64Arg(op.AmountArg, op.Arg, "amount_arg")
		if err != nil {
			return norm, err
		}
	case "TOKEN_MINT":
		tokenID, tokenArg, err := normalizeTokenRef(op.TokenID, op.TokenArg, norm.Op)
		if err != nil {
			return norm, err
		}
		norm.TokenID = tokenID
		norm.TokenArg = tokenArg
		norm.From, err = normalizeFromMode(op.From, "contract")
		if err != nil {
			return norm, err
		}
		norm.ToArg, err = normalizeAddressArg(op.ToArg, "to_arg")
		if err != nil {
			return norm, err
		}
		norm.AmountArg, err = normalizeU64Arg(op.AmountArg, op.Arg, "amount_arg")
		if err != nil {
			return norm, err
		}
	case "TOKEN_BURN":
		tokenID, tokenArg, err := normalizeTokenRef(op.TokenID, op.TokenArg, norm.Op)
		if err != nil {
			return norm, err
		}
		norm.TokenID = tokenID
		norm.TokenArg = tokenArg
		norm.From, err = normalizeFromMode(op.From, "caller")
		if err != nil {
			return norm, err
		}
		norm.AmountArg, err = normalizeU64Arg(op.AmountArg, op.Arg, "amount_arg")
		if err != nil {
			return norm, err
		}
	case "TOKEN_CREATE":
		norm.From, err = normalizeFromMode(op.From, "contract")
		if err != nil {
			return norm, err
		}
		norm.NameArg, err = normalizeStringArg(op.NameArg, "name_arg")
		if err != nil {
			return norm, err
		}
		norm.SymbolArg, err = normalizeStringArg(op.SymbolArg, "symbol_arg")
		if err != nil {
			return norm, err
		}
		norm.DecimalsArg, err = normalizeU64Arg(op.DecimalsArg, "", "decimals_arg")
		if err != nil {
			return norm, err
		}
		norm.MaxSupplyArg, err = normalizeU64Arg(op.MaxSupplyArg, "", "max_supply_arg")
		if err != nil {
			return norm, err
		}
		norm.InitialSupplyArg, err = normalizeU64Arg(op.InitialSupplyArg, op.AmountArg, "initial_supply_arg")
		if err != nil {
			return norm, err
		}
		if strings.TrimSpace(op.ToArg) != "" {
			norm.ToArg, err = normalizeAddressArg(op.ToArg, "to_arg")
			if err != nil {
				return norm, err
			}
		}
	case "RET_ERR":
		norm.Message = strings.TrimSpace(op.Message)
		if len(norm.Message) > DTLMaxContractValueLen {
			return norm, fmt.Errorf("ret_err message too long")
		}
	case "RET_OK":
		// no-op
	default:
		return norm, fmt.Errorf("unsupported op")
	}
	return norm, nil
}

func findDTLLogicPackMethod(pack *DTLLogicPack, method string) *DTLLogicPackMethod {
	if pack == nil {
		return nil
	}
	n := strings.ToLower(strings.TrimSpace(method))
	for i := range pack.Methods {
		if strings.ToLower(strings.TrimSpace(pack.Methods[i].Name)) == n {
			return &pack.Methods[i]
		}
	}
	return nil
}

func findDTLLogicPackABIMethod(pack *DTLLogicPack, method string) *DTLLogicPackABIMethod {
	if pack == nil {
		return nil
	}
	n := strings.ToLower(strings.TrimSpace(method))
	for i := range pack.ABI {
		if strings.ToLower(strings.TrimSpace(pack.ABI[i].Name)) == n {
			return &pack.ABI[i]
		}
	}
	return nil
}

func validateDTLLogicPackCall(state *DTLState, contractID string, contract *DTLContractState, tx DTLContractCallTx) error {
	if state == nil || contract == nil || contract.LogicPack == nil {
		return fmt.Errorf("dtl: logic pack unavailable")
	}
	method := strings.ToLower(strings.TrimSpace(tx.Method))
	if method == "" {
		return fmt.Errorf("dtl: missing contract method")
	}
	if findDTLLogicPackMethod(contract.LogicPack, method) == nil {
		return fmt.Errorf("dtl: unknown logic method: %s", method)
	}
	abi := findDTLLogicPackABIMethod(contract.LogicPack, method)
	if abi == nil {
		return fmt.Errorf("dtl: abi missing for logic method: %s", method)
	}
	args := normalizeDTLLogicArgs(tx.Args)
	if len(args) > DTLMaxContractArgs {
		return fmt.Errorf("dtl: too many contract args")
	}
	for _, arg := range abi.Args {
		key := strings.ToLower(strings.TrimSpace(arg.Name))
		raw, exists := args[key]
		if !exists {
			return fmt.Errorf("dtl: missing contract arg: %s", key)
		}
		switch normalizeDTLLogicType(arg.Type) {
		case "u64":
			if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
				return fmt.Errorf("dtl: arg %s must be uint64", key)
			}
		case "address":
			if normalizeDTLAccount(raw) == "" {
				return fmt.Errorf("dtl: arg %s must be address", key)
			}
		case "bool":
			if _, err := strconv.ParseBool(strings.ToLower(raw)); err != nil {
				return fmt.Errorf("dtl: arg %s must be bool", key)
			}
		case "string":
			if len(raw) > DTLMaxContractValueLen {
				return fmt.Errorf("dtl: arg %s too long", key)
			}
		default:
			return fmt.Errorf("dtl: unsupported abi arg type")
		}
	}
	_ = contractID
	return nil
}

func getDTLLogicReg(regs map[string]dtlLogicValue, reg string) (dtlLogicValue, error) {
	v, ok := regs[reg]
	if !ok {
		return dtlLogicValue{}, fmt.Errorf("dtl: register not initialized: %s", reg)
	}
	return v, nil
}

func getDTLLogicRegU64(regs map[string]dtlLogicValue, reg string) (uint64, error) {
	v, err := getDTLLogicReg(regs, reg)
	if err != nil {
		return 0, err
	}
	if v.kind != "u64" {
		return 0, fmt.Errorf("dtl: register %s is not u64", reg)
	}
	return v.u64, nil
}

func getDTLLogicRegStr(regs map[string]dtlLogicValue, reg string) (string, error) {
	v, err := getDTLLogicReg(regs, reg)
	if err != nil {
		return "", err
	}
	if v.kind != "string" {
		return "", fmt.Errorf("dtl: register %s is not string", reg)
	}
	return v.str, nil
}

func getDTLLogicRegBool(regs map[string]dtlLogicValue, reg string) (bool, error) {
	v, err := getDTLLogicReg(regs, reg)
	if err != nil {
		return false, err
	}
	if v.kind != "bool" {
		return false, fmt.Errorf("dtl: register %s is not bool", reg)
	}
	return v.b, nil
}

func dtlSafeMulU64(a, b uint64) (uint64, error) {
	hi, lo := bits.Mul64(a, b)
	if hi != 0 {
		return 0, fmt.Errorf("dtl: uint64 overflow")
	}
	return lo, nil
}

func dtlSafeDivU64(a, b uint64) (uint64, error) {
	if b == 0 {
		return 0, fmt.Errorf("dtl: division by zero")
	}
	return a / b, nil
}

func dtlLogicValuesEqual(a, b dtlLogicValue) (bool, error) {
	if a.kind != b.kind {
		return false, fmt.Errorf("dtl: compare type mismatch: %s vs %s", a.kind, b.kind)
	}
	switch a.kind {
	case "u64":
		return a.u64 == b.u64, nil
	case "string":
		return a.str == b.str, nil
	case "bool":
		return a.b == b.b, nil
	default:
		return false, fmt.Errorf("dtl: unsupported compare type")
	}
}

func dtlLogicResolvePrincipal(mode, caller, contractID string) (string, error) {
	fromMode := strings.ToLower(strings.TrimSpace(mode))
	if fromMode == "" {
		fromMode = "caller"
	}
	switch fromMode {
	case "caller":
		principal := normalizeDTLAccount(caller)
		if principal == "" {
			return "", fmt.Errorf("dtl: invalid logic caller")
		}
		return principal, nil
	case "contract":
		return dtlContractVaultAccount(contractID), nil
	default:
		return "", fmt.Errorf("dtl: invalid logic from mode")
	}
}

func dtlLogicResolveTokenRef(state *DTLState, op DTLLogicPackOp, args map[string]string) (string, error) {
	if state == nil {
		return "", ErrDTLInvalidState
	}
	tokenRef := strings.TrimSpace(op.TokenID)
	if strings.TrimSpace(op.TokenArg) != "" {
		tokenRef = strings.TrimSpace(args[op.TokenArg])
	}
	tokenID, ok := resolveDTLTokenRef(state, tokenRef)
	if tokenID == "" || !ok {
		return "", ErrDTLUnknownToken
	}
	return tokenID, nil
}

func dtlLogicTokenSignerAuthorized(token *DTLTokenState, signer string) bool {
	normSigner := normalizeDTLAccount(signer)
	if token == nil || normSigner == "" {
		return false
	}
	for _, raw := range token.AuthoritySigners {
		if normalizeDTLAccount(raw) == normSigner {
			return true
		}
	}
	return false
}

func dtlLogicApplyTokenMint(state *DTLState, tokenID, signer, to string, amount uint64) error {
	if state == nil {
		return ErrDTLInvalidState
	}
	token := state.Tokens[tokenID]
	if token == nil {
		return ErrDTLUnknownToken
	}
	if token.Paused {
		return ErrDTLPaused
	}
	if amount == 0 {
		return fmt.Errorf("dtl: mint amount must be > 0")
	}
	if token.AuthorityThreshold != 1 || !dtlLogicTokenSignerAuthorized(token, signer) {
		return fmt.Errorf("dtl: token mint requires single-signer authority")
	}
	if token.MaxSupply > 0 && token.TotalSupply > token.MaxSupply-amount {
		return fmt.Errorf("dtl: mint exceeds max supply")
	}
	if err := dtlAddBalance(state.Balances, dtlBalanceKey(tokenID, to), amount); err != nil {
		return err
	}
	token.TotalSupply += amount
	state.Events = append(state.Events, fmt.Sprintf("TOKEN_MINT:%s:%s:%d", tokenID, to, amount))
	return nil
}

type dtlLogicCallResult struct {
	Kind string
	U64  uint64
	Str  string
	Bool bool
}

func dtlLogicMapStorageKey(mapName, key string) string {
	normalizedMap := strings.ToLower(strings.TrimSpace(mapName))
	if normalizedMap == "" {
		normalizedMap = "default"
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return "map:" + normalizedMap + ":" + hex.EncodeToString(sum[:])
}

func dtlLogicCallArg(args map[string]string, key string) string {
	if args == nil {
		return ""
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if strings.HasPrefix(k, "literal:") {
		return strings.TrimSpace(strings.TrimPrefix(k, "literal:"))
	}
	if strings.HasPrefix(k, "concat(") && strings.HasSuffix(k, ")") {
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(k, "concat("), ")"))
		if body == "" {
			return ""
		}
		parts := strings.Split(body, ",")
		segments := make([]string, 0, len(parts))
		for _, part := range parts {
			name := strings.ToLower(strings.TrimSpace(part))
			if name == "" {
				continue
			}
			segments = append(segments, strings.TrimSpace(args[name]))
		}
		return strings.Join(segments, "|")
	}
	return strings.TrimSpace(args[strings.ToLower(k)])
}

type dtlLogicExecContext struct {
	BlockHeight   uint64
	ChainID       string
	BlockHash     string
	PrevBlockHash string
	CallDepth     int
}

func newDTLLogicExecContext(blockHeight uint64, chainID string) dtlLogicExecContext {
	trimmedChainID := strings.TrimSpace(chainID)
	if trimmedChainID == "" {
		trimmedChainID = "dtl-logic"
	}
	blockHash := dtlDeterministicBeaconHash(trimmedChainID+"|block", blockHeight)
	prevHeight := uint64(0)
	if blockHeight > 0 {
		prevHeight = blockHeight - 1
	}
	prevHash := dtlDeterministicBeaconHash(trimmedChainID+"|block", prevHeight)
	return dtlLogicExecContext{
		BlockHeight:   blockHeight,
		ChainID:       trimmedChainID,
		BlockHash:     blockHash,
		PrevBlockHash: prevHash,
		CallDepth:     0,
	}
}

func dtlLogicRoleStorageKey(role, account string) string {
	nRole := strings.ToLower(strings.TrimSpace(role))
	nAccount := normalizeDTLAccount(account)
	return "role:" + nRole + ":" + nAccount
}

func dtlLogicRoleGranted(contract *DTLContractState, role, account string) bool {
	if contract == nil {
		return false
	}
	nAccount := normalizeDTLAccount(account)
	if nAccount == "" {
		return false
	}
	nRole := strings.ToLower(strings.TrimSpace(role))
	if nRole == "" {
		return false
	}
	creator := normalizeDTLAccount(contract.Creator)
	if creator != "" && creator == nAccount {
		return true
	}
	if nRole == "admin" && creator != "" && creator == nAccount {
		return true
	}
	v := strings.TrimSpace(contract.Storage[dtlLogicRoleStorageKey(nRole, nAccount)])
	return strings.EqualFold(v, "true")
}

func dtlLogicCanManageRole(contract *DTLContractState, caller string) bool {
	nCaller := normalizeDTLAccount(caller)
	if nCaller == "" {
		return false
	}
	creator := normalizeDTLAccount(contract.Creator)
	if creator != "" && creator == nCaller {
		return true
	}
	return dtlLogicRoleGranted(contract, "admin", nCaller)
}

func executeDTLLogicPackCall(state *DTLState, contractID string, contract *DTLContractState, tx DTLContractCallTx) error {
	ctx := newDTLLogicExecContext(0, ChainID)
	_, err := executeDTLLogicPackCallWithContext(state, contractID, contract, tx, ctx, false)
	return err
}

func executeDTLLogicPackCallWithContext(
	state *DTLState,
	contractID string,
	contract *DTLContractState,
	tx DTLContractCallTx,
	ctx dtlLogicExecContext,
	readOnly bool,
) (dtlLogicCallResult, error) {
	result := dtlLogicCallResult{}
	if err := validateDTLLogicPackCall(state, contractID, contract, tx); err != nil {
		return result, err
	}
	if contract == nil || contract.LogicPack == nil {
		return result, fmt.Errorf("dtl: missing contract logic pack")
	}
	method := findDTLLogicPackMethod(contract.LogicPack, tx.Method)
	if method == nil {
		return result, fmt.Errorf("dtl: unknown logic method: %s", tx.Method)
	}

	args := normalizeDTLLogicArgs(tx.Args)
	args["caller"] = normalizeDTLAccount(tx.Caller)
	args["contract"] = normalizeDTLContractID(contractID)
	regs := make(map[string]dtlLogicValue, 32)

	reads := 0
	writes := 0
	transfers := 0
	logs := 0
	mapReads := 0
	mapWrites := 0
	crossCalls := 0
	roleOps := 0
	maxReads := int(contract.LogicPack.Limits.MaxReads)
	maxWrites := int(contract.LogicPack.Limits.MaxWrites)
	maxTransfers := int(contract.LogicPack.Limits.MaxTokenTransfers)
	maxLogs := int(contract.LogicPack.Limits.MaxLogs)
	maxMapReads := int(contract.LogicPack.Limits.MaxMapReads)
	maxMapWrites := int(contract.LogicPack.Limits.MaxMapWrites)
	maxCrossCalls := int(contract.LogicPack.Limits.MaxCrossCalls)
	maxRoleOps := int(contract.LogicPack.Limits.MaxRoleOps)
	if maxCrossCalls <= 0 {
		maxCrossCalls = int(DTLDefaultLogicPackCrossCalls)
	}
	if maxRoleOps <= 0 {
		maxRoleOps = int(DTLDefaultLogicPackRoleOps)
	}
	parseU64Arg := func(argName, field string, allowZero bool) (uint64, error) {
		raw := dtlLogicCallArg(args, argName)
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("dtl: invalid %s", field)
		}
		if !allowZero && n == 0 {
			return 0, fmt.Errorf("dtl: %s must be > 0", field)
		}
		return n, nil
	}
	chainID := strings.TrimSpace(ctx.ChainID)
	if chainID == "" {
		chainID = "dtl-logic"
	}

	pc := 0
	steps := 0
	for pc >= 0 && pc < len(method.Ops) {
		steps++
		if steps > int(method.MaxSteps) {
			return result, fmt.Errorf("dtl: logic method step limit exceeded")
		}
		op := method.Ops[pc]
		opName := strings.ToUpper(strings.TrimSpace(op.Op))
		switch opName {
		case "ADD_U64", "SUB_U64", "MUL_U64", "DIV_U64", "MIN_U64", "MAX_U64":
			if strings.TrimSpace(op.Dest) == "" || strings.TrimSpace(op.A) == "" || strings.TrimSpace(op.B) == "" {
				return result, fmt.Errorf("dtl: malformed %s op at pc=%d", opName, pc)
			}
		case "MUL_DIV_U64":
			if strings.TrimSpace(op.Dest) == "" || strings.TrimSpace(op.A) == "" || strings.TrimSpace(op.B) == "" || strings.TrimSpace(op.Src) == "" {
				return result, fmt.Errorf("dtl: malformed %s op at pc=%d", opName, pc)
			}
		}
		switch strings.ToUpper(strings.TrimSpace(op.Op)) {
		case "ARG_U64":
			raw := dtlLogicCallArg(args, op.Arg)
			n, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return result, fmt.Errorf("dtl: arg %s must be uint64", op.Arg)
			}
			regs[op.Dest] = dtlLogicValueU64(n)
			pc++
		case "ARG_STR":
			regs[op.Dest] = dtlLogicValueStr(dtlLogicCallArg(args, op.Arg))
			pc++
		case "ARG_BOOL":
			raw := strings.ToLower(strings.TrimSpace(dtlLogicCallArg(args, op.Arg)))
			v, err := strconv.ParseBool(raw)
			if err != nil {
				return result, fmt.Errorf("dtl: arg %s must be bool", op.Arg)
			}
			regs[op.Dest] = dtlLogicValueBool(v)
			pc++
		case "LOAD_U64":
			reads++
			if reads > maxReads {
				return result, fmt.Errorf("dtl: logic read limit exceeded")
			}
			n, err := parseDTLContractStoredU64(contract.Storage, op.Key)
			if err != nil {
				return result, err
			}
			regs[op.Dest] = dtlLogicValueU64(n)
			pc++
		case "LOAD_STR":
			reads++
			if reads > maxReads {
				return result, fmt.Errorf("dtl: logic read limit exceeded")
			}
			regs[op.Dest] = dtlLogicValueStr(contract.Storage[op.Key])
			pc++
		case "STORE_U64":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate state")
			}
			writes++
			if writes > maxWrites {
				return result, fmt.Errorf("dtl: logic write limit exceeded")
			}
			n, err := getDTLLogicRegU64(regs, op.Src)
			if err != nil {
				return result, err
			}
			contract.Storage[op.Key] = strconv.FormatUint(n, 10)
			pc++
		case "STORE_STR":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate state")
			}
			writes++
			if writes > maxWrites {
				return result, fmt.Errorf("dtl: logic write limit exceeded")
			}
			s, err := getDTLLogicRegStr(regs, op.Src)
			if err != nil {
				return result, err
			}
			if len(s) > DTLMaxContractValueLen {
				return result, fmt.Errorf("dtl: logic string value too long")
			}
			contract.Storage[op.Key] = s
			pc++
		case "ADD_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			n, err := dtlSafeAddU64(a, b)
			if err != nil {
				return result, err
			}
			regs[op.Dest] = dtlLogicValueU64(n)
			pc++
		case "SUB_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			if b > a {
				return result, fmt.Errorf("dtl: contract subtraction underflow")
			}
			regs[op.Dest] = dtlLogicValueU64(a - b)
			pc++
		case "MUL_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			n, err := dtlSafeMulU64(a, b)
			if err != nil {
				return result, err
			}
			regs[op.Dest] = dtlLogicValueU64(n)
			pc++
		case "DIV_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			n, err := dtlSafeDivU64(a, b)
			if err != nil {
				return result, err
			}
			regs[op.Dest] = dtlLogicValueU64(n)
			pc++
		case "MUL_DIV_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			denom, err := getDTLLogicRegU64(regs, op.Src)
			if err != nil {
				return result, err
			}
			n, err := dtlMulDivU64(a, b, denom)
			if err != nil {
				return result, err
			}
			regs[op.Dest] = dtlLogicValueU64(n)
			pc++
		case "MIN_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			if a < b {
				regs[op.Dest] = dtlLogicValueU64(a)
			} else {
				regs[op.Dest] = dtlLogicValueU64(b)
			}
			pc++
		case "MAX_U64":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			if a > b {
				regs[op.Dest] = dtlLogicValueU64(a)
			} else {
				regs[op.Dest] = dtlLogicValueU64(b)
			}
			pc++
		case "CMP_EQ", "CMP_NEQ":
			a, err := getDTLLogicReg(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicReg(regs, op.B)
			if err != nil {
				return result, err
			}
			eq, err := dtlLogicValuesEqual(a, b)
			if err != nil {
				return result, err
			}
			if strings.ToUpper(op.Op) == "CMP_NEQ" {
				eq = !eq
			}
			regs[op.Dest] = dtlLogicValueBool(eq)
			pc++
		case "CMP_GT", "CMP_GTE", "CMP_LT", "CMP_LTE":
			a, err := getDTLLogicRegU64(regs, op.A)
			if err != nil {
				return result, err
			}
			b, err := getDTLLogicRegU64(regs, op.B)
			if err != nil {
				return result, err
			}
			var result bool
			switch strings.ToUpper(op.Op) {
			case "CMP_GT":
				result = a > b
			case "CMP_GTE":
				result = a >= b
			case "CMP_LT":
				result = a < b
			case "CMP_LTE":
				result = a <= b
			}
			regs[op.Dest] = dtlLogicValueBool(result)
			pc++
		case "JMP_IF":
			cond, err := getDTLLogicRegBool(regs, op.Cond)
			if err != nil {
				return result, err
			}
			if cond {
				pc = op.Target
			} else {
				pc++
			}
		case "JMP":
			pc = op.Target
		case "ASSERT":
			cond, err := getDTLLogicRegBool(regs, op.Cond)
			if err != nil {
				return result, err
			}
			if !cond {
				msg := strings.TrimSpace(op.Message)
				if msg == "" {
					msg = "dtl: logic assert failed"
				}
				return result, errors.New(msg)
			}
			pc++
		case "CTX_CALLER":
			regs[op.Dest] = dtlLogicValueStr(normalizeDTLAccount(tx.Caller))
			pc++
		case "CTX_CONTRACT":
			regs[op.Dest] = dtlLogicValueStr(normalizeDTLContractID(contractID))
			pc++
		case "CTX_CHAIN_ID":
			regs[op.Dest] = dtlLogicValueStr(strings.TrimSpace(chainID))
			pc++
		case "CTX_BLOCK_HEIGHT":
			regs[op.Dest] = dtlLogicValueU64(ctx.BlockHeight)
			pc++
		case "CTX_BLOCK_HASH":
			regs[op.Dest] = dtlLogicValueStr(strings.TrimSpace(ctx.BlockHash))
			pc++
		case "CTX_PREV_BLOCK_HASH":
			regs[op.Dest] = dtlLogicValueStr(strings.TrimSpace(ctx.PrevBlockHash))
			pc++
		case "MAP_GET_U64":
			mapReads++
			if mapReads > maxMapReads {
				return result, fmt.Errorf("dtl: logic map read limit exceeded")
			}
			mapKey := dtlLogicMapStorageKey(op.Map, dtlLogicCallArg(args, op.MapKeyArg))
			value, err := parseDTLContractStoredU64(contract.Storage, mapKey)
			if err != nil {
				return result, err
			}
			regs[op.Dest] = dtlLogicValueU64(value)
			pc++
		case "MAP_SET_U64":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate state")
			}
			mapWrites++
			if mapWrites > maxMapWrites {
				return result, fmt.Errorf("dtl: logic map write limit exceeded")
			}
			value, err := getDTLLogicRegU64(regs, op.Src)
			if err != nil {
				return result, err
			}
			mapKey := dtlLogicMapStorageKey(op.Map, dtlLogicCallArg(args, op.MapKeyArg))
			contract.Storage[mapKey] = strconv.FormatUint(value, 10)
			pc++
		case "MAP_GET_STR":
			mapReads++
			if mapReads > maxMapReads {
				return result, fmt.Errorf("dtl: logic map read limit exceeded")
			}
			mapKey := dtlLogicMapStorageKey(op.Map, dtlLogicCallArg(args, op.MapKeyArg))
			regs[op.Dest] = dtlLogicValueStr(contract.Storage[mapKey])
			pc++
		case "MAP_SET_STR":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate state")
			}
			mapWrites++
			if mapWrites > maxMapWrites {
				return result, fmt.Errorf("dtl: logic map write limit exceeded")
			}
			value, err := getDTLLogicRegStr(regs, op.Src)
			if err != nil {
				return result, err
			}
			if len(value) > DTLMaxContractValueLen {
				return result, fmt.Errorf("dtl: logic string value too long")
			}
			mapKey := dtlLogicMapStorageKey(op.Map, dtlLogicCallArg(args, op.MapKeyArg))
			contract.Storage[mapKey] = value
			pc++
		case "MAP_GET_BOOL":
			mapReads++
			if mapReads > maxMapReads {
				return result, fmt.Errorf("dtl: logic map read limit exceeded")
			}
			mapKey := dtlLogicMapStorageKey(op.Map, dtlLogicCallArg(args, op.MapKeyArg))
			regs[op.Dest] = dtlLogicValueBool(strings.EqualFold(strings.TrimSpace(contract.Storage[mapKey]), "true"))
			pc++
		case "MAP_SET_BOOL":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate state")
			}
			mapWrites++
			if mapWrites > maxMapWrites {
				return result, fmt.Errorf("dtl: logic map write limit exceeded")
			}
			value, err := getDTLLogicRegBool(regs, op.Src)
			if err != nil {
				return result, err
			}
			mapKey := dtlLogicMapStorageKey(op.Map, dtlLogicCallArg(args, op.MapKeyArg))
			contract.Storage[mapKey] = strconv.FormatBool(value)
			pc++
		case "REQUIRE_ROLE":
			if !dtlLogicRoleGranted(contract, op.Key, tx.Caller) {
				return result, fmt.Errorf("dtl: missing role %s", strings.TrimSpace(op.Key))
			}
			pc++
		case "GRANT_ROLE":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate roles")
			}
			roleOps++
			if roleOps > maxRoleOps {
				return result, fmt.Errorf("dtl: logic role op limit exceeded")
			}
			if !dtlLogicCanManageRole(contract, tx.Caller) {
				return result, fmt.Errorf("dtl: role management unauthorized")
			}
			target := normalizeDTLAccount(dtlLogicCallArg(args, op.ToArg))
			if target == "" {
				return result, fmt.Errorf("dtl: invalid role target")
			}
			contract.Storage[dtlLogicRoleStorageKey(op.Key, target)] = "true"
			pc++
		case "REVOKE_ROLE":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate roles")
			}
			roleOps++
			if roleOps > maxRoleOps {
				return result, fmt.Errorf("dtl: logic role op limit exceeded")
			}
			if !dtlLogicCanManageRole(contract, tx.Caller) {
				return result, fmt.Errorf("dtl: role management unauthorized")
			}
			target := normalizeDTLAccount(dtlLogicCallArg(args, op.ToArg))
			if target == "" {
				return result, fmt.Errorf("dtl: invalid role target")
			}
			delete(contract.Storage, dtlLogicRoleStorageKey(op.Key, target))
			pc++
		case "CALL_DTL_RO":
			crossCalls++
			if crossCalls > maxCrossCalls {
				return result, fmt.Errorf("dtl: logic cross-call limit exceeded")
			}
			if ctx.CallDepth >= dtlMaxContractCallDepth() {
				return result, fmt.Errorf("dtl: logic call depth exceeded")
			}
			targetContractID := normalizeDTLContractID(dtlLogicCallArg(args, op.Key))
			targetMethod := strings.TrimSpace(dtlLogicCallArg(args, op.Arg))
			if targetContractID == "" || targetMethod == "" {
				return result, fmt.Errorf("dtl: invalid cross-call target")
			}
			targetContract := state.Contracts[targetContractID]
			if targetContract == nil || targetContract.LogicPack == nil {
				return result, fmt.Errorf("dtl: unknown cross-call contract")
			}
			childCtx := ctx
			childCtx.CallDepth++
			callResult, err := executeDTLLogicPackCallWithContext(
				state,
				targetContractID,
				targetContract,
				DTLContractCallTx{
					Caller:     tx.Caller,
					ContractID: targetContractID,
					Method:     targetMethod,
					Args:       args,
				},
				childCtx,
				true,
			)
			if err != nil {
				return result, err
			}
			switch callResult.Kind {
			case "u64":
				regs[op.Dest] = dtlLogicValueU64(callResult.U64)
			case "bool":
				regs[op.Dest] = dtlLogicValueBool(callResult.Bool)
			default:
				regs[op.Dest] = dtlLogicValueStr(callResult.Str)
			}
			pc++
		case "EMIT_LOG":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot emit logs")
			}
			logs++
			if logs > maxLogs {
				return result, fmt.Errorf("dtl: logic log limit exceeded")
			}
			if ConfigDTLLogsIndexEnabled {
				logEntry := DTLEventLog{
					ContractID:  normalizeDTLContractID(contractID),
					Topics:      []string{dtlLogicCallArg(args, op.Topic0Arg)},
					Data:        dtlLogicCallArg(args, op.DataArg),
					BlockHeight: ctx.BlockHeight,
				}
				if t := dtlLogicCallArg(args, op.Topic1Arg); t != "" {
					logEntry.Topics = append(logEntry.Topics, t)
				}
				if t := dtlLogicCallArg(args, op.Topic2Arg); t != "" {
					logEntry.Topics = append(logEntry.Topics, t)
				}
				if t := dtlLogicCallArg(args, op.Topic3Arg); t != "" {
					logEntry.Topics = append(logEntry.Topics, t)
				}
				state.EventLogs = append(state.EventLogs, logEntry)
			}
			state.Events = append(state.Events, fmt.Sprintf("CONTRACT_LOG:%s:%s", normalizeDTLContractID(contractID), dtlLogicCallArg(args, op.Topic0Arg)))
			pc++
		case "TOKEN_TRANSFER":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate balances")
			}
			transfers++
			if transfers > maxTransfers {
				return result, fmt.Errorf("dtl: logic transfer limit exceeded")
			}
			tokenID, err := dtlLogicResolveTokenRef(state, op, args)
			if err != nil {
				return result, err
			}
			to := normalizeDTLAccount(dtlLogicCallArg(args, op.ToArg))
			if to == "" {
				return result, fmt.Errorf("dtl: invalid token transfer args")
			}
			amount, err := parseU64Arg(op.AmountArg, "transfer amount", false)
			if err != nil {
				return result, err
			}
			from, err := dtlLogicResolvePrincipal(op.From, tx.Caller, contractID)
			if err != nil {
				return result, err
			}
			if err := dtlMoveBalance(state, tokenID, from, to, amount); err != nil {
				return result, err
			}
			pc++
		case "TOKEN_APPROVE":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate allowances")
			}
			transfers++
			if transfers > maxTransfers {
				return result, fmt.Errorf("dtl: logic transfer limit exceeded")
			}
			tokenID, err := dtlLogicResolveTokenRef(state, op, args)
			if err != nil {
				return result, err
			}
			owner, err := dtlLogicResolvePrincipal(op.From, tx.Caller, contractID)
			if err != nil {
				return result, err
			}
			spender := normalizeDTLAccount(dtlLogicCallArg(args, op.SpenderArg))
			if spender == "" {
				return result, fmt.Errorf("dtl: invalid spender")
			}
			amount, err := parseU64Arg(op.AmountArg, "approve amount", true)
			if err != nil {
				return result, err
			}
			if err := ApplyDTLApproveTx(state, DTLApproveTx{
				Owner:   owner,
				Spender: spender,
				TokenID: tokenID,
				Amount:  amount,
			}); err != nil {
				return result, err
			}
			pc++
		case "TOKEN_TRANSFER_FROM":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate balances")
			}
			transfers++
			if transfers > maxTransfers {
				return result, fmt.Errorf("dtl: logic transfer limit exceeded")
			}
			tokenID, err := dtlLogicResolveTokenRef(state, op, args)
			if err != nil {
				return result, err
			}
			spender, err := dtlLogicResolvePrincipal(op.From, tx.Caller, contractID)
			if err != nil {
				return result, err
			}
			from := normalizeDTLAccount(dtlLogicCallArg(args, op.FromArg))
			to := normalizeDTLAccount(dtlLogicCallArg(args, op.ToArg))
			if from == "" || to == "" {
				return result, fmt.Errorf("dtl: invalid transfer_from accounts")
			}
			amount, err := parseU64Arg(op.AmountArg, "transfer_from amount", false)
			if err != nil {
				return result, err
			}
			if err := ApplyDTLTransferFromTx(state, DTLTransferFromTx{
				Spender: spender,
				From:    from,
				To:      to,
				TokenID: tokenID,
				Amount:  amount,
			}); err != nil {
				return result, err
			}
			pc++
		case "TOKEN_MINT":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate supply")
			}
			transfers++
			if transfers > maxTransfers {
				return result, fmt.Errorf("dtl: logic transfer limit exceeded")
			}
			tokenID, err := dtlLogicResolveTokenRef(state, op, args)
			if err != nil {
				return result, err
			}
			authority, err := dtlLogicResolvePrincipal(op.From, tx.Caller, contractID)
			if err != nil {
				return result, err
			}
			to := normalizeDTLAccount(dtlLogicCallArg(args, op.ToArg))
			if to == "" {
				return result, fmt.Errorf("dtl: invalid mint recipient")
			}
			amount, err := parseU64Arg(op.AmountArg, "mint amount", false)
			if err != nil {
				return result, err
			}
			if err := dtlLogicApplyTokenMint(state, tokenID, authority, to, amount); err != nil {
				return result, err
			}
			pc++
		case "TOKEN_BURN":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot mutate supply")
			}
			transfers++
			if transfers > maxTransfers {
				return result, fmt.Errorf("dtl: logic transfer limit exceeded")
			}
			tokenID, err := dtlLogicResolveTokenRef(state, op, args)
			if err != nil {
				return result, err
			}
			from, err := dtlLogicResolvePrincipal(op.From, tx.Caller, contractID)
			if err != nil {
				return result, err
			}
			amount, err := parseU64Arg(op.AmountArg, "burn amount", false)
			if err != nil {
				return result, err
			}
			if err := ApplyDTLBurnTx(state, DTLBurnTx{
				From:    from,
				TokenID: tokenID,
				Amount:  amount,
			}); err != nil {
				return result, err
			}
			pc++
		case "TOKEN_CREATE":
			if readOnly {
				return result, fmt.Errorf("dtl: readonly call cannot create token")
			}
			transfers++
			if transfers > maxTransfers {
				return result, fmt.Errorf("dtl: logic transfer limit exceeded")
			}
			creator, err := dtlLogicResolvePrincipal(op.From, tx.Caller, contractID)
			if err != nil {
				return result, err
			}
			name := strings.TrimSpace(dtlLogicCallArg(args, op.NameArg))
			symbol := strings.TrimSpace(dtlLogicCallArg(args, op.SymbolArg))
			decimals, err := parseU64Arg(op.DecimalsArg, "decimals", true)
			if err != nil {
				return result, err
			}
			if decimals > uint64(DTLMaxDecimals) {
				return result, fmt.Errorf("dtl: decimals out of range")
			}
			maxSupply, err := parseU64Arg(op.MaxSupplyArg, "max_supply", true)
			if err != nil {
				return result, err
			}
			initialSupply, err := parseU64Arg(op.InitialSupplyArg, "initial_supply", true)
			if err != nil {
				return result, err
			}
			createTx := DTLCreateTx{
				Creator:            creator,
				Name:               name,
				Symbol:             symbol,
				Decimals:           uint8(decimals),
				MaxSupply:          maxSupply,
				InitialSupply:      initialSupply,
				AuthoritySigners:   []string{creator},
				AuthorityThreshold: 1,
				FreezeEnabled:      false,
				TaxBPS:             0,
			}
			createNonce := uint64(len(state.Tokens) + pc + 1)
			tokenID, err := ApplyDTLCreateTx(state, chainID, createNonce, createTx)
			if err != nil {
				return result, err
			}
			if op.ToArg != "" && initialSupply > 0 {
				to := normalizeDTLAccount(dtlLogicCallArg(args, op.ToArg))
				if to == "" {
					return result, fmt.Errorf("dtl: invalid token create recipient")
				}
				if to != creator {
					if err := dtlMoveBalance(state, tokenID, creator, to, initialSupply); err != nil {
						return result, err
					}
				}
			}
			pc++
		case "RET_U64":
			value, err := getDTLLogicRegU64(regs, op.Src)
			if err != nil {
				return result, err
			}
			result = dtlLogicCallResult{Kind: "u64", U64: value}
			return result, nil
		case "RET_STR":
			value, err := getDTLLogicRegStr(regs, op.Src)
			if err != nil {
				return result, err
			}
			result = dtlLogicCallResult{Kind: "string", Str: value}
			return result, nil
		case "RET_BOOL":
			value, err := getDTLLogicRegBool(regs, op.Src)
			if err != nil {
				return result, err
			}
			result = dtlLogicCallResult{Kind: "bool", Bool: value}
			return result, nil
		case "RET_OK":
			return result, nil
		case "RET_ERR":
			msg := strings.TrimSpace(op.Message)
			if msg == "" {
				msg = "dtl: contract method returned error"
			}
			return result, errors.New(msg)
		default:
			return result, fmt.Errorf("dtl: unsupported logic opcode")
		}
	}
	return result, fmt.Errorf("dtl: logic method terminated without return")
}
