package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// `dtlLogicRegPrefix` defines the constant value used by this package.
	dtlLogicRegPrefix = "r"
	// `dtlLogicRegMax` defines the constant value used by this package.
	dtlLogicRegMax = 31
)

type dtlLogicValue struct {
	// `kind` stores the value associated with this record.
	kind string
	// `u64` stores the value associated with this record.
	u64 uint64
	// `str` stores the value associated with this record.
	str string
	// `b` stores the value associated with this record.
	b bool
}

// dtlLogicValueU64 implements the dtl logic value u64 helper.
func dtlLogicValueU64(v uint64) dtlLogicValue { return dtlLogicValue{kind: "u64", u64: v} }

// dtlLogicValueStr implements the dtl logic value str helper.
func dtlLogicValueStr(v string) dtlLogicValue {
	return dtlLogicValue{kind: "string", str: strings.TrimSpace(v)}
}

// dtlLogicValueBool implements the dtl logic value bool helper.
func dtlLogicValueBool(v bool) dtlLogicValue { return dtlLogicValue{kind: "bool", b: v} }

// isDTLLogicIdentifier implements the is dtl logic identifier helper.
func isDTLLogicIdentifier(raw string) bool {
	// `s` stores the value produced by this operation.
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > DTLMaxContractKeyLen {
		return false
	}
	// `i` and `r` track the current position in the related collection.
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

// normalizeDTLLogicIdentifier normalizes dtl logic identifier.
func normalizeDTLLogicIdentifier(raw string) (string, error) {
	// `s` stores the value produced by this operation.
	s := strings.ToLower(strings.TrimSpace(raw))
	if !isDTLLogicIdentifier(s) {
		return "", fmt.Errorf("dtl: invalid logic identifier: %s", raw)
	}
	return s, nil
}

// normalizeDTLLogicType normalizes dtl logic type.
func normalizeDTLLogicType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// isDTLLogicType implements the is dtl logic type helper.
func isDTLLogicType(raw string) bool {
	switch normalizeDTLLogicType(raw) {
	case "u64", "string", "bool", "address":
		return true
	default:
		return false
	}
}

// normalizeDTLLogicRegister normalizes dtl logic register.
func normalizeDTLLogicRegister(raw string) (string, error) {
	// `reg` stores the value produced by this operation.
	reg := strings.ToLower(strings.TrimSpace(raw))
	if !strings.HasPrefix(reg, dtlLogicRegPrefix) {
		return "", fmt.Errorf("dtl: invalid logic register: %s", raw)
	}
	// `n` and `err` store the error produced by this operation.
	n, err := strconv.Atoi(strings.TrimPrefix(reg, dtlLogicRegPrefix))
	if err != nil || n < 0 || n > dtlLogicRegMax {
		return "", fmt.Errorf("dtl: invalid logic register: %s", raw)
	}
	return fmt.Sprintf("%s%d", dtlLogicRegPrefix, n), nil
}

// normalizeDTLStringMapSorted normalizes dtl string map sorted.
func normalizeDTLStringMapSorted(raw map[string]string, context string, normalizeKey func(string) string) (map[string]string, error) {
	// `out` stores the result produced by this operation.
	out := make(map[string]string, len(raw))
	// `rawKeys` stores the value produced by this operation.
	rawKeys := make([]string, 0, len(raw))
	// `key` tracks the key used to access the related value.
	for key := range raw {
		rawKeys = append(rawKeys, key)
	}
	sort.Strings(rawKeys)
	// `rawKey` tracks the key used to access the related value.
	for _, rawKey := range rawKeys {
		// `key` stores the key used to access the related value.
		key := normalizeKey(rawKey)
		if key == "" {
			return nil, fmt.Errorf("dtl: empty %s key", context)
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("dtl: duplicate %s key after normalization: %s", context, key)
		}
		out[key] = strings.TrimSpace(raw[rawKey])
	}
	return out, nil
}

// normalizeDTLLogicArgs normalizes dtl logic args.
func normalizeDTLLogicArgs(raw map[string]string) (map[string]string, error) {
	return normalizeDTLStringMapSorted(raw, "logic arg", func(key string) string {
		return strings.ToLower(strings.TrimSpace(key))
	})
}

// normalizeDTLContractInitStorage normalizes dtl contract init storage.
func normalizeDTLContractInitStorage(raw map[string]string, schema []DTLLogicPackStorageField) (map[string]string, error) {
	// `fieldKeys` stores the value produced by this operation.
	fieldKeys := map[string]struct{}(nil)
	if len(schema) > 0 {
		fieldKeys = make(map[string]struct{}, len(schema))
		// `field` tracks the current values while iterating.
		for _, field := range schema {
			// `key` and `err` store the error produced by this operation.
			key, err := normalizeDTLLogicIdentifier(field.Key)
			if err != nil {
				return nil, err
			}
			fieldKeys[key] = struct{}{}
		}
	}
	return normalizeDTLStringMapSorted(raw, "contract init", func(rawKey string) string {
		// `key` stores the key used to access the related value.
		key := strings.TrimSpace(rawKey)
		if len(fieldKeys) == 0 {
			return key
		}
		// `normalized` and `err` store the error produced by this operation.
		normalized, err := normalizeDTLLogicIdentifier(key)
		if err != nil {
			return ""
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := fieldKeys[normalized]; !exists {
			return ""
		}
		return normalized
	})
}

// cloneDTLLogicPack clones dtl logic pack into canonical persisted form.
func cloneDTLLogicPack(src *DTLLogicPack) *DTLLogicPack {
	if src == nil {
		return nil
	}
	return canonicalDTLLogicPackForHash(src)
}

// dtlLogicPackHash implements the dtl logic pack hash helper.
func dtlLogicPackHash(pack *DTLLogicPack) (string, error) {
	if pack == nil {
		return "", fmt.Errorf("dtl: nil logic pack")
	}
	return DTLPayloadHash(canonicalDTLLogicPackForHash(pack))
}

// canonicalDTLLogicPackForHash returns a stable, protocol-only view of a logic
// pack for state hashing. Deployed packs are already normalized on the execution
// path; this helper keeps imported/persisted state hashing insensitive to benign
// slice ordering and case drift without reading runtime state.
func canonicalDTLLogicPackForHash(pack *DTLLogicPack) *DTLLogicPack {
	if pack == nil {
		return nil
	}
	// `limits` stores the value produced by this operation.
	limits, _ := normalizeDTLLogicPackLimits(pack.Limits)
	// `out` stores the result produced by this operation.
	out := &DTLLogicPack{
		Version: pack.Version,
		Name:    canonicalDTLLogicIdentifierForHash(pack.Name),
		Limits:  limits,
	}

	out.Storage = make([]DTLLogicPackStorageField, 0, len(pack.Storage))
	// `field` tracks the current values while iterating.
	for _, field := range pack.Storage {
		// `fieldType` stores the value produced by this operation.
		fieldType := normalizeDTLLogicType(field.Type)
		// `initValue` stores the value currently being processed.
		initValue := strings.TrimSpace(field.Init)
		switch fieldType {
		case "u64":
			if initValue == "" {
				initValue = "0"
			}
		case "bool":
			if initValue == "" {
				initValue = "false"
			}
		}
		out.Storage = append(out.Storage, DTLLogicPackStorageField{
			Key:  canonicalDTLLogicIdentifierForHash(field.Key),
			Type: fieldType,
			Init: initValue,
		})
	}
	sort.SliceStable(out.Storage, func(i, j int) bool {
		return out.Storage[i].Key < out.Storage[j].Key
	})

	out.ABI = make([]DTLLogicPackABIMethod, 0, len(pack.ABI))
	// `method` tracks the current values while iterating.
	for _, method := range pack.ABI {
		// `canonical` stores the value produced by this operation.
		canonical := DTLLogicPackABIMethod{
			Name:    canonicalDTLLogicIdentifierForHash(method.Name),
			Args:    make([]DTLLogicPackArg, 0, len(method.Args)),
			Returns: make([]string, 0, len(method.Returns)),
		}
		// `arg` tracks the current values while iterating.
		for _, arg := range method.Args {
			canonical.Args = append(canonical.Args, DTLLogicPackArg{
				Name: canonicalDTLLogicIdentifierForHash(arg.Name),
				Type: normalizeDTLLogicType(arg.Type),
			})
		}
		// `ret` tracks the current values while iterating.
		for _, ret := range method.Returns {
			canonical.Returns = append(canonical.Returns, normalizeDTLLogicType(ret))
		}
		out.ABI = append(out.ABI, canonical)
	}
	sort.SliceStable(out.ABI, func(i, j int) bool {
		return out.ABI[i].Name < out.ABI[j].Name
	})

	out.Methods = make([]DTLLogicPackMethod, 0, len(pack.Methods))
	// `method` tracks the current values while iterating.
	for _, method := range pack.Methods {
		// `maxSteps` stores the value produced by this operation.
		maxSteps := method.MaxSteps
		if maxSteps == 0 {
			maxSteps = uint16(len(method.Ops) + 1)
		}
		// `canonical` stores the value produced by this operation.
		canonical := DTLLogicPackMethod{
			Name:     canonicalDTLLogicIdentifierForHash(method.Name),
			MaxSteps: maxSteps,
			Ops:      make([]DTLLogicPackOp, 0, len(method.Ops)),
		}
		// `op` tracks the current values while iterating.
		for _, op := range method.Ops {
			canonical.Ops = append(canonical.Ops, canonicalDTLLogicPackOpForHash(op))
		}
		out.Methods = append(out.Methods, canonical)
	}
	sort.SliceStable(out.Methods, func(i, j int) bool {
		return out.Methods[i].Name < out.Methods[j].Name
	})

	return out
}

// canonicalDTLLogicPackOpForHash returns stable operation material for logic
// pack hashing. It intentionally performs no state lookup or validation.
func canonicalDTLLogicPackOpForHash(op DTLLogicPackOp) DTLLogicPackOp {
	// `opName` stores the value produced by this operation.
	opName := strings.ToUpper(strings.TrimSpace(op.Op))
	// `out` stores the result produced by this operation.
	out := DTLLogicPackOp{
		Op:               opName,
		Dest:             canonicalDTLLogicRegisterForHash(op.Dest),
		A:                canonicalDTLLogicRegisterForHash(op.A),
		B:                canonicalDTLLogicRegisterForHash(op.B),
		Src:              canonicalDTLLogicRegisterForHash(op.Src),
		Cond:             canonicalDTLLogicRegisterForHash(op.Cond),
		Key:              canonicalDTLLogicIdentifierOrArgForHash(op.Key),
		Arg:              canonicalDTLLogicArgForHash(op.Arg),
		TokenID:          normalizeDTLTokenID(op.TokenID),
		TokenArg:         canonicalDTLLogicArgForHash(op.TokenArg),
		ToArg:            canonicalDTLLogicArgForHash(op.ToArg),
		AmountArg:        canonicalDTLLogicArgForHash(op.AmountArg),
		FromArg:          canonicalDTLLogicArgForHash(op.FromArg),
		SpenderArg:       canonicalDTLLogicArgForHash(op.SpenderArg),
		NameArg:          canonicalDTLLogicLiteralOrArgForHash(op.NameArg),
		SymbolArg:        canonicalDTLLogicLiteralOrArgForHash(op.SymbolArg),
		DecimalsArg:      canonicalDTLLogicArgForHash(op.DecimalsArg),
		MaxSupplyArg:     canonicalDTLLogicArgForHash(op.MaxSupplyArg),
		InitialSupplyArg: canonicalDTLLogicArgForHash(op.InitialSupplyArg),
		From:             canonicalDTLLogicFromModeForHash(op.From, canonicalDTLLogicFromDefaultForHash(opName)),
		Message:          strings.TrimSpace(op.Message),
		Target:           op.Target,
		Map:              canonicalDTLLogicIdentifierForHash(op.Map),
		MapKeyArg:        canonicalDTLLogicArgForHash(op.MapKeyArg),
		Topic0Arg:        canonicalDTLLogicLiteralOrArgForHash(op.Topic0Arg),
		Topic1Arg:        canonicalDTLLogicLiteralOrArgForHash(op.Topic1Arg),
		Topic2Arg:        canonicalDTLLogicLiteralOrArgForHash(op.Topic2Arg),
		Topic3Arg:        canonicalDTLLogicLiteralOrArgForHash(op.Topic3Arg),
		DataArg:          canonicalDTLLogicLiteralOrArgForHash(op.DataArg),
	}
	return out
}

// canonicalDTLLogicIdentifierForHash implements the canonical dtl logic
// identifier for hash helper.
func canonicalDTLLogicIdentifierForHash(raw string) string {
	// `normalized` and `err` store the error produced by this operation.
	normalized, err := normalizeDTLLogicIdentifier(raw)
	if err == nil {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// canonicalDTLLogicRegisterForHash implements the canonical dtl logic register
// for hash helper.
func canonicalDTLLogicRegisterForHash(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	// `normalized` and `err` store the error produced by this operation.
	normalized, err := normalizeDTLLogicRegister(raw)
	if err == nil {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// canonicalDTLLogicArgForHash implements the canonical dtl logic arg for hash
// helper.
func canonicalDTLLogicArgForHash(raw string) string {
	// `in` stores the current position in the related collection.
	in := strings.TrimSpace(raw)
	if in == "" {
		return ""
	}
	if strings.HasPrefix(in, "concat(") && strings.HasSuffix(in, ")") {
		// `body` stores the value produced by this operation.
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(in, "concat("), ")"))
		// `parts` stores the value produced by this operation.
		parts := strings.Split(body, ",")
		// `normalizedParts` stores the value produced by this operation.
		normalizedParts := make([]string, 0, len(parts))
		// `part` tracks the current values while iterating.
		for _, part := range parts {
			normalizedParts = append(normalizedParts, canonicalDTLLogicIdentifierForHash(part))
		}
		return "concat(" + strings.Join(normalizedParts, ",") + ")"
	}
	return canonicalDTLLogicIdentifierForHash(in)
}

// canonicalDTLLogicIdentifierOrArgForHash implements the canonical dtl logic
// identifier or arg for hash helper.
func canonicalDTLLogicIdentifierOrArgForHash(raw string) string {
	return canonicalDTLLogicArgForHash(raw)
}

// canonicalDTLLogicLiteralOrArgForHash implements the canonical dtl logic
// literal or arg for hash helper.
func canonicalDTLLogicLiteralOrArgForHash(raw string) string {
	// `in` stores the current position in the related collection.
	in := strings.TrimSpace(raw)
	if in == "" {
		return ""
	}
	if strings.HasPrefix(in, "literal:") {
		return "literal:" + strings.TrimSpace(strings.TrimPrefix(in, "literal:"))
	}
	return canonicalDTLLogicArgForHash(in)
}

// canonicalDTLLogicFromDefaultForHash implements the canonical dtl logic from
// default for hash helper.
func canonicalDTLLogicFromDefaultForHash(opName string) string {
	switch strings.ToUpper(strings.TrimSpace(opName)) {
	case "TOKEN_MINT", "TOKEN_CREATE":
		return "contract"
	case "TOKEN_TRANSFER", "TOKEN_APPROVE", "TOKEN_TRANSFER_FROM", "TOKEN_BURN":
		return "caller"
	default:
		return ""
	}
}

// canonicalDTLLogicFromModeForHash implements the canonical dtl logic from mode
// for hash helper.
func canonicalDTLLogicFromModeForHash(raw string, defaultMode string) string {
	// `mode` stores the value produced by this operation.
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(defaultMode))
	}
	return mode
}

// validateDTLLogicPackOpcode validates dtl logic pack opcode.
func validateDTLLogicPackOpcode(raw string) (string, error) {
	// `op` stores the value produced by this operation.
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

// isDTLLogicPackV2Opcode implements the is dtl logic pack v2 opcode helper.
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

// isDTLLogicPackV3Opcode implements the is dtl logic pack v3 opcode helper.
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

// normalizeDTLLogicPackLimits normalizes dtl logic pack limits.
func normalizeDTLLogicPackLimits(raw DTLLogicPackLimits) (DTLLogicPackLimits, error) {
	// `out` stores the result produced by this operation.
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

// validateAndNormalizeDTLLogicPack is a permanent tombstone. Logic-pack
// execution was removed; retained state schema is only for deterministic
// rejection and historical decoding.
func validateAndNormalizeDTLLogicPack(_ *DTLState, _ *DTLLogicPack) (*DTLLogicPack, error) {
	return nil, dtlContractRuntimeRemovedError("logic_pack")
}

// validateDTLLogicPackCall is a permanent tombstone.
func validateDTLLogicPackCall(_ *DTLState, _ string, _ *DTLContractState, _ DTLContractCallTx) error {
	return dtlContractRuntimeRemovedError("logic_pack_call")
}

// dtlLogicMapStorageKey is retained only to decode legacy contract-shaped data
// on the fail-closed path; no transaction can execute it.
func dtlLogicMapStorageKey(mapName, key string) string {
	return strings.ToLower(strings.TrimSpace(mapName)) + "\x00" + strings.TrimSpace(key)
}

type dtlLogicCallResult struct{}
type dtlLogicExecContext struct{}

// newDTLLogicExecContext returns an inert compatibility value.
func newDTLLogicExecContext(_ uint64, _ string) dtlLogicExecContext {
	return dtlLogicExecContext{}
}

// executeDTLLogicPackCallWithContext is a permanent tombstone.
func executeDTLLogicPackCallWithContext(
	_ *DTLState,
	_ string,
	_ *DTLContractState,
	_ DTLContractCallTx,
	_ dtlLogicExecContext,
	_ bool,
) (dtlLogicCallResult, error) {
	return dtlLogicCallResult{}, dtlContractRuntimeRemovedError("logic_pack_call")
}
