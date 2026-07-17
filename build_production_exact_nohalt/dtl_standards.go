package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

func dtlStandardTemplateLimits() DTLLogicPackLimits {
	return DTLLogicPackLimits{
		MaxReads:          128,
		MaxWrites:         128,
		MaxTokenTransfers: 64,
		MaxLogs:           64,
		MaxMapReads:       128,
		MaxMapWrites:      128,
		MaxCrossCalls:     16,
		MaxRoleOps:        16,
	}
}

func buildDTLStandardTemplate(tx DTLContractDeployTx) (DTLContractDeployTx, error) {
	standard := normalizeDTLContractStandard(tx.Standard)
	if standard == DTLContractStandardCustom {
		return tx, nil
	}
	if tx.LogicPack != nil || len(tx.Methods) > 0 {
		return tx, nil
	}

	name := strings.TrimSpace(tx.Name)
	if name == "" {
		name = "Contract"
	}
	symbol := strings.ToUpper(strings.TrimSpace(tx.Init["symbol"]))
	if symbol == "" {
		symbol = strings.ToUpper(name)
		if len(symbol) > DTLMaxSymbolLen {
			symbol = symbol[:DTLMaxSymbolLen]
		}
	}
	decimals := uint8(18)
	if raw := strings.TrimSpace(tx.Init["decimals"]); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 8); err == nil {
			decimals = uint8(parsed)
		}
	}
	totalSupply := uint64(0)
	if raw := strings.TrimSpace(tx.Init["total_supply"]); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
			totalSupply = parsed
		}
	}
	baseURI := strings.TrimSpace(tx.Init["base_uri"])

	var (
		pack       *DTLLogicPack
		abiRaw     json.RawMessage
		interfaces []string
		err        error
	)
	switch standard {
	case DTLContractStandardMSC20:
		pack, abiRaw, interfaces, err = BuildMSC20Template(name, symbol, totalSupply, decimals)
	case DTLContractStandardMSC721:
		pack, abiRaw, interfaces, err = BuildMSC721Template(name, symbol, baseURI)
	case DTLContractStandardMSC1155:
		pack, abiRaw, interfaces, err = BuildMSC1155Template(name, symbol, baseURI)
	default:
		return tx, nil
	}
	if err != nil {
		return tx, err
	}

	tx.LogicPack = pack
	tx.Methods = nil
	tx.Standard = standard
	tx.Lang = "dtl-script-v2"
	if tx.Version == 0 {
		tx.Version = DTLLogicPackVersionV3
	}
	if len(tx.ABI) == 0 {
		tx.ABI = abiRaw
	}
	if len(tx.Interfaces) == 0 {
		tx.Interfaces = interfaces
	}
	return tx, nil
}

func BuildMSC20Template(name, symbol string, totalSupply uint64, decimals uint8) (*DTLLogicPack, json.RawMessage, []string, error) {
	pack := &DTLLogicPack{
		Version: DTLLogicPackVersionV3,
		Name:    "msc20",
		ABI: []DTLLogicPackABIMethod{
			{Name: "name", Returns: []string{"string"}},
			{Name: "symbol", Returns: []string{"string"}},
			{Name: "decimals", Returns: []string{"u64"}},
			{Name: "totalsupply", Returns: []string{"u64"}},
			{Name: "balanceof", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}}, Returns: []string{"u64"}},
			{Name: "allowance", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}, {Name: "spender", Type: "address"}}, Returns: []string{"u64"}},
			{Name: "transfer", Args: []DTLLogicPackArg{{Name: "to", Type: "address"}, {Name: "amount", Type: "u64"}}},
			{Name: "approve", Args: []DTLLogicPackArg{{Name: "spender", Type: "address"}, {Name: "amount", Type: "u64"}}},
			{Name: "transferfrom", Args: []DTLLogicPackArg{{Name: "from", Type: "address"}, {Name: "to", Type: "address"}, {Name: "amount", Type: "u64"}}},
			{Name: "mint", Args: []DTLLogicPackArg{{Name: "to", Type: "address"}, {Name: "amount", Type: "u64"}}},
			{Name: "burn", Args: []DTLLogicPackArg{{Name: "amount", Type: "u64"}}},
			{Name: "pause"},
			{Name: "unpause"},
		},
		Storage: []DTLLogicPackStorageField{
			{Key: "name", Type: "string", Init: strings.TrimSpace(name)},
			{Key: "symbol", Type: "string", Init: strings.ToUpper(strings.TrimSpace(symbol))},
			{Key: "decimals", Type: "u64", Init: strconv.FormatUint(uint64(decimals), 10)},
			{Key: "total_supply", Type: "u64", Init: strconv.FormatUint(totalSupply, 10)},
			{Key: "paused", Type: "u64", Init: "0"},
			{Key: "zero", Type: "u64", Init: "0"},
			{Key: "one", Type: "u64", Init: "1"},
		},
		Methods: []DTLLogicPackMethod{
			{Name: "name", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "LOAD_STR", Dest: "r0", Key: "name"}, {Op: "RET_STR", Src: "r0"}}},
			{Name: "symbol", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "LOAD_STR", Dest: "r0", Key: "symbol"}, {Op: "RET_STR", Src: "r0"}}},
			{Name: "decimals", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "LOAD_U64", Dest: "r0", Key: "decimals"}, {Op: "RET_U64", Src: "r0"}}},
			{Name: "totalsupply", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "LOAD_U64", Dest: "r0", Key: "total_supply"}, {Op: "RET_U64", Src: "r0"}}},
			{Name: "balanceof", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_U64", Dest: "r0", Map: "balances", MapKeyArg: "owner"}, {Op: "RET_U64", Src: "r0"}}},
			{Name: "allowance", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_U64", Dest: "r0", Map: "allowances", MapKeyArg: "concat(owner,spender)"}, {Op: "RET_U64", Src: "r0"}}},
			{
				Name:     "transfer",
				MaxSteps: 32,
				Ops: []DTLLogicPackOp{
					{Op: "LOAD_U64", Dest: "r0", Key: "paused"},
					{Op: "LOAD_U64", Dest: "r1", Key: "zero"},
					{Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"},
					{Op: "ASSERT", Cond: "r2", Message: "dtl: token paused"},
					{Op: "CTX_CALLER", Dest: "r3"},
					{Op: "MAP_GET_U64", Dest: "r4", Map: "balances", MapKeyArg: "caller"},
					{Op: "ARG_U64", Dest: "r5", Arg: "amount"},
					{Op: "CMP_GTE", Dest: "r6", A: "r4", B: "r5"},
					{Op: "ASSERT", Cond: "r6", Message: "dtl: insufficient balance"},
					{Op: "SUB_U64", Dest: "r7", A: "r4", B: "r5"},
					{Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "caller", Src: "r7"},
					{Op: "MAP_GET_U64", Dest: "r8", Map: "balances", MapKeyArg: "to"},
					{Op: "ADD_U64", Dest: "r9", A: "r8", B: "r5"},
					{Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "to", Src: "r9"},
					{Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "caller", Topic2Arg: "to", DataArg: "amount"},
					{Op: "RET_OK"},
				},
			},
			{
				Name:     "approve",
				MaxSteps: 24,
				Ops: []DTLLogicPackOp{
					{Op: "LOAD_U64", Dest: "r0", Key: "paused"},
					{Op: "LOAD_U64", Dest: "r1", Key: "zero"},
					{Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"},
					{Op: "ASSERT", Cond: "r2", Message: "dtl: token paused"},
					{Op: "ARG_U64", Dest: "r3", Arg: "amount"},
					{Op: "MAP_SET_U64", Map: "allowances", MapKeyArg: "concat(caller,spender)", Src: "r3"},
					{Op: "EMIT_LOG", Topic0Arg: "literal:Approval", Topic1Arg: "caller", Topic2Arg: "spender", DataArg: "amount"},
					{Op: "RET_OK"},
				},
			},
			{
				Name:     "transferfrom",
				MaxSteps: 48,
				Ops: []DTLLogicPackOp{
					{Op: "LOAD_U64", Dest: "r0", Key: "paused"},
					{Op: "LOAD_U64", Dest: "r1", Key: "zero"},
					{Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"},
					{Op: "ASSERT", Cond: "r2", Message: "dtl: token paused"},
					{Op: "ARG_U64", Dest: "r3", Arg: "amount"},
					{Op: "MAP_GET_U64", Dest: "r4", Map: "allowances", MapKeyArg: "concat(from,caller)"},
					{Op: "CMP_GTE", Dest: "r5", A: "r4", B: "r3"},
					{Op: "ASSERT", Cond: "r5", Message: "dtl: allowance exceeded"},
					{Op: "SUB_U64", Dest: "r6", A: "r4", B: "r3"},
					{Op: "MAP_SET_U64", Map: "allowances", MapKeyArg: "concat(from,caller)", Src: "r6"},
					{Op: "MAP_GET_U64", Dest: "r7", Map: "balances", MapKeyArg: "from"},
					{Op: "CMP_GTE", Dest: "r8", A: "r7", B: "r3"},
					{Op: "ASSERT", Cond: "r8", Message: "dtl: insufficient balance"},
					{Op: "SUB_U64", Dest: "r9", A: "r7", B: "r3"},
					{Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "from", Src: "r9"},
					{Op: "MAP_GET_U64", Dest: "r10", Map: "balances", MapKeyArg: "to"},
					{Op: "ADD_U64", Dest: "r11", A: "r10", B: "r3"},
					{Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "to", Src: "r11"},
					{Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "from", Topic2Arg: "to", DataArg: "amount"},
					{Op: "RET_OK"},
				},
			},
			{
				Name:     "mint",
				MaxSteps: 32,
				Ops: []DTLLogicPackOp{
					{Op: "REQUIRE_ROLE", Key: "minter"},
					{Op: "ARG_U64", Dest: "r0", Arg: "amount"},
					{Op: "MAP_GET_U64", Dest: "r1", Map: "balances", MapKeyArg: "to"},
					{Op: "ADD_U64", Dest: "r2", A: "r1", B: "r0"},
					{Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "to", Src: "r2"},
					{Op: "LOAD_U64", Dest: "r3", Key: "total_supply"},
					{Op: "ADD_U64", Dest: "r4", A: "r3", B: "r0"},
					{Op: "STORE_U64", Key: "total_supply", Src: "r4"},
					{Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "contract", Topic2Arg: "to", DataArg: "amount"},
					{Op: "RET_OK"},
				},
			},
			{
				Name:     "burn",
				MaxSteps: 32,
				Ops: []DTLLogicPackOp{
					{Op: "ARG_U64", Dest: "r0", Arg: "amount"},
					{Op: "MAP_GET_U64", Dest: "r1", Map: "balances", MapKeyArg: "caller"},
					{Op: "CMP_GTE", Dest: "r2", A: "r1", B: "r0"},
					{Op: "ASSERT", Cond: "r2", Message: "dtl: insufficient balance"},
					{Op: "SUB_U64", Dest: "r3", A: "r1", B: "r0"},
					{Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "caller", Src: "r3"},
					{Op: "LOAD_U64", Dest: "r4", Key: "total_supply"},
					{Op: "CMP_GTE", Dest: "r5", A: "r4", B: "r0"},
					{Op: "ASSERT", Cond: "r5", Message: "dtl: burn exceeds supply"},
					{Op: "SUB_U64", Dest: "r6", A: "r4", B: "r0"},
					{Op: "STORE_U64", Key: "total_supply", Src: "r6"},
					{Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "caller", Topic2Arg: "literal:burn", DataArg: "amount"},
					{Op: "RET_OK"},
				},
			},
			{
				Name:     "pause",
				MaxSteps: 8,
				Ops:      []DTLLogicPackOp{{Op: "REQUIRE_ROLE", Key: "pauser"}, {Op: "LOAD_U64", Dest: "r0", Key: "one"}, {Op: "STORE_U64", Key: "paused", Src: "r0"}, {Op: "RET_OK"}},
			},
			{
				Name:     "unpause",
				MaxSteps: 8,
				Ops:      []DTLLogicPackOp{{Op: "REQUIRE_ROLE", Key: "pauser"}, {Op: "LOAD_U64", Dest: "r0", Key: "zero"}, {Op: "STORE_U64", Key: "paused", Src: "r0"}, {Op: "RET_OK"}},
			},
		},
		Limits: dtlStandardTemplateLimits(),
	}
	abiRaw, err := json.Marshal(pack.ABI)
	if err != nil {
		return nil, nil, nil, err
	}
	return pack, abiRaw, []string{"IMSC20"}, nil
}
func BuildMSC721Template(name, symbol, baseURI string) (*DTLLogicPack, json.RawMessage, []string, error) {
	pack := &DTLLogicPack{
		Version: DTLLogicPackVersionV3,
		Name:    "msc721",
		ABI: []DTLLogicPackABIMethod{
			{Name: "name", Returns: []string{"string"}},
			{Name: "symbol", Returns: []string{"string"}},
			{Name: "tokenuri", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}}, Returns: []string{"string"}},
			{Name: "ownerof", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}}, Returns: []string{"string"}},
			{Name: "balanceof", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}}, Returns: []string{"u64"}},
			{Name: "getapproved", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}}, Returns: []string{"string"}},
			{Name: "isapprovedforall", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}, {Name: "operator", Type: "address"}}, Returns: []string{"bool"}},
			{Name: "approve", Args: []DTLLogicPackArg{{Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}}},
			{Name: "setapprovalforall", Args: []DTLLogicPackArg{{Name: "operator", Type: "address"}, {Name: "approved", Type: "bool"}}},
			{Name: "transferfrom", Args: []DTLLogicPackArg{{Name: "from", Type: "address"}, {Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}}},
			{Name: "safetransferfrom", Args: []DTLLogicPackArg{{Name: "from", Type: "address"}, {Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}}},
			{Name: "mint", Args: []DTLLogicPackArg{{Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}}},
			{Name: "burn", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}}},
		},
		Storage: []DTLLogicPackStorageField{
			{Key: "name", Type: "string", Init: strings.TrimSpace(name)},
			{Key: "symbol", Type: "string", Init: strings.ToUpper(strings.TrimSpace(symbol))},
			{Key: "base_uri", Type: "string", Init: strings.TrimSpace(baseURI)},
			{Key: "empty", Type: "string", Init: ""},
			{Key: "one", Type: "u64", Init: "1"},
		},
		Methods: []DTLLogicPackMethod{
			{Name: "name", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "LOAD_STR", Dest: "r0", Key: "name"}, {Op: "RET_STR", Src: "r0"}}},
			{Name: "symbol", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "LOAD_STR", Dest: "r0", Key: "symbol"}, {Op: "RET_STR", Src: "r0"}}},
			{Name: "tokenuri", MaxSteps: 12, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "token_uris", MapKeyArg: "token_id"}, {Op: "LOAD_STR", Dest: "r1", Key: "empty"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "JMP_IF", Cond: "r2", Target: 5}, {Op: "RET_STR", Src: "r0"}, {Op: "LOAD_STR", Dest: "r3", Key: "base_uri"}, {Op: "RET_STR", Src: "r3"}}},
			{Name: "ownerof", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "owners", MapKeyArg: "token_id"}, {Op: "RET_STR", Src: "r0"}}},
			{Name: "balanceof", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_U64", Dest: "r0", Map: "balances", MapKeyArg: "owner"}, {Op: "RET_U64", Src: "r0"}}},
			{Name: "getapproved", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "token_approvals", MapKeyArg: "token_id"}, {Op: "RET_STR", Src: "r0"}}},
			{Name: "isapprovedforall", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_BOOL", Dest: "r0", Map: "operator_approvals", MapKeyArg: "concat(owner,operator)"}, {Op: "RET_BOOL", Src: "r0"}}},
			{Name: "approve", MaxSteps: 20, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "owners", MapKeyArg: "token_id"}, {Op: "CTX_CALLER", Dest: "r1"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: unauthorized"}, {Op: "MAP_SET_STR", Map: "token_approvals", MapKeyArg: "token_id", Src: "to"}, {Op: "EMIT_LOG", Topic0Arg: "literal:Approval", Topic1Arg: "caller", Topic2Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "setapprovalforall", MaxSteps: 16, Ops: []DTLLogicPackOp{{Op: "ARG_BOOL", Dest: "r0", Arg: "approved"}, {Op: "MAP_SET_BOOL", Map: "operator_approvals", MapKeyArg: "concat(caller,operator)", Src: "r0"}, {Op: "EMIT_LOG", Topic0Arg: "literal:ApprovalForAll", Topic1Arg: "caller", Topic2Arg: "operator", DataArg: "approved"}, {Op: "RET_OK"}}},
			{Name: "transferfrom", MaxSteps: 64, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "owners", MapKeyArg: "token_id"}, {Op: "ARG_STR", Dest: "r1", Arg: "from"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: from mismatch"}, {Op: "CTX_CALLER", Dest: "r3"}, {Op: "CMP_EQ", Dest: "r4", A: "r3", B: "r0"}, {Op: "JMP_IF", Cond: "r4", Target: 12}, {Op: "MAP_GET_STR", Dest: "r5", Map: "token_approvals", MapKeyArg: "token_id"}, {Op: "CMP_EQ", Dest: "r6", A: "r3", B: "r5"}, {Op: "JMP_IF", Cond: "r6", Target: 12}, {Op: "MAP_GET_BOOL", Dest: "r7", Map: "operator_approvals", MapKeyArg: "concat(from,caller)"}, {Op: "ASSERT", Cond: "r7", Message: "dtl: unauthorized"}, {Op: "MAP_SET_STR", Map: "owners", MapKeyArg: "token_id", Src: "to"}, {Op: "LOAD_STR", Dest: "r8", Key: "empty"}, {Op: "MAP_SET_STR", Map: "token_approvals", MapKeyArg: "token_id", Src: "r8"}, {Op: "MAP_GET_U64", Dest: "r9", Map: "balances", MapKeyArg: "from"}, {Op: "LOAD_U64", Dest: "r10", Key: "one"}, {Op: "SUB_U64", Dest: "r11", A: "r9", B: "r10"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "from", Src: "r11"}, {Op: "MAP_GET_U64", Dest: "r12", Map: "balances", MapKeyArg: "to"}, {Op: "ADD_U64", Dest: "r13", A: "r12", B: "r10"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "to", Src: "r13"}, {Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "from", Topic2Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "safetransferfrom", MaxSteps: 64, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "owners", MapKeyArg: "token_id"}, {Op: "ARG_STR", Dest: "r1", Arg: "from"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: from mismatch"}, {Op: "CTX_CALLER", Dest: "r3"}, {Op: "CMP_EQ", Dest: "r4", A: "r3", B: "r0"}, {Op: "JMP_IF", Cond: "r4", Target: 12}, {Op: "MAP_GET_STR", Dest: "r5", Map: "token_approvals", MapKeyArg: "token_id"}, {Op: "CMP_EQ", Dest: "r6", A: "r3", B: "r5"}, {Op: "JMP_IF", Cond: "r6", Target: 12}, {Op: "MAP_GET_BOOL", Dest: "r7", Map: "operator_approvals", MapKeyArg: "concat(from,caller)"}, {Op: "ASSERT", Cond: "r7", Message: "dtl: unauthorized"}, {Op: "MAP_SET_STR", Map: "owners", MapKeyArg: "token_id", Src: "to"}, {Op: "LOAD_STR", Dest: "r8", Key: "empty"}, {Op: "MAP_SET_STR", Map: "token_approvals", MapKeyArg: "token_id", Src: "r8"}, {Op: "MAP_GET_U64", Dest: "r9", Map: "balances", MapKeyArg: "from"}, {Op: "LOAD_U64", Dest: "r10", Key: "one"}, {Op: "SUB_U64", Dest: "r11", A: "r9", B: "r10"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "from", Src: "r11"}, {Op: "MAP_GET_U64", Dest: "r12", Map: "balances", MapKeyArg: "to"}, {Op: "ADD_U64", Dest: "r13", A: "r12", B: "r10"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "to", Src: "r13"}, {Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "from", Topic2Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "mint", MaxSteps: 32, Ops: []DTLLogicPackOp{{Op: "REQUIRE_ROLE", Key: "minter"}, {Op: "MAP_GET_STR", Dest: "r0", Map: "owners", MapKeyArg: "token_id"}, {Op: "LOAD_STR", Dest: "r1", Key: "empty"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: token exists"}, {Op: "MAP_SET_STR", Map: "owners", MapKeyArg: "token_id", Src: "to"}, {Op: "LOAD_STR", Dest: "r3", Key: "base_uri"}, {Op: "MAP_SET_STR", Map: "token_uris", MapKeyArg: "token_id", Src: "r3"}, {Op: "MAP_GET_U64", Dest: "r4", Map: "balances", MapKeyArg: "to"}, {Op: "LOAD_U64", Dest: "r5", Key: "one"}, {Op: "ADD_U64", Dest: "r6", A: "r4", B: "r5"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "to", Src: "r6"}, {Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "contract", Topic2Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "burn", MaxSteps: 24, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "owners", MapKeyArg: "token_id"}, {Op: "CTX_CALLER", Dest: "r1"}, {Op: "CMP_EQ", Dest: "r2", A: "r1", B: "r0"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: unauthorized"}, {Op: "LOAD_STR", Dest: "r3", Key: "empty"}, {Op: "MAP_SET_STR", Map: "owners", MapKeyArg: "token_id", Src: "r3"}, {Op: "MAP_SET_STR", Map: "token_approvals", MapKeyArg: "token_id", Src: "r3"}, {Op: "MAP_GET_U64", Dest: "r4", Map: "balances", MapKeyArg: "caller"}, {Op: "LOAD_U64", Dest: "r5", Key: "one"}, {Op: "SUB_U64", Dest: "r6", A: "r4", B: "r5"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "caller", Src: "r6"}, {Op: "EMIT_LOG", Topic0Arg: "literal:Transfer", Topic1Arg: "caller", Topic2Arg: "literal:burn", DataArg: "token_id"}, {Op: "RET_OK"}}},
		},
		Limits: dtlStandardTemplateLimits(),
	}
	abiRaw, err := json.Marshal(pack.ABI)
	if err != nil {
		return nil, nil, nil, err
	}
	return pack, abiRaw, []string{"IMSC721"}, nil
}
func BuildMSC1155Template(name, symbol, baseURI string) (*DTLLogicPack, json.RawMessage, []string, error) {
	pack := &DTLLogicPack{
		Version: DTLLogicPackVersionV3,
		Name:    "msc1155",
		ABI: []DTLLogicPackABIMethod{
			{Name: "uri", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}}, Returns: []string{"string"}},
			{Name: "balanceof", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}, {Name: "token_id", Type: "u64"}}, Returns: []string{"u64"}},
			{Name: "balanceofbatch", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}}, Returns: []string{"u64"}},
			{Name: "isapprovedforall", Args: []DTLLogicPackArg{{Name: "owner", Type: "address"}, {Name: "operator", Type: "address"}}, Returns: []string{"bool"}},
			{Name: "setapprovalforall", Args: []DTLLogicPackArg{{Name: "operator", Type: "address"}, {Name: "approved", Type: "bool"}}},
			{Name: "safetransferfrom", Args: []DTLLogicPackArg{{Name: "from", Type: "address"}, {Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}, {Name: "amount", Type: "u64"}}},
			{Name: "safebatchtransferfrom", Args: []DTLLogicPackArg{{Name: "from", Type: "address"}, {Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}, {Name: "amount", Type: "u64"}}},
			{Name: "mint", Args: []DTLLogicPackArg{{Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}, {Name: "amount", Type: "u64"}}},
			{Name: "mintbatch", Args: []DTLLogicPackArg{{Name: "to", Type: "address"}, {Name: "token_id", Type: "u64"}, {Name: "amount", Type: "u64"}}},
			{Name: "burn", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}, {Name: "amount", Type: "u64"}}},
			{Name: "burnbatch", Args: []DTLLogicPackArg{{Name: "token_id", Type: "u64"}, {Name: "amount", Type: "u64"}}},
		},
		Storage: []DTLLogicPackStorageField{
			{Key: "name", Type: "string", Init: strings.TrimSpace(name)},
			{Key: "symbol", Type: "string", Init: strings.ToUpper(strings.TrimSpace(symbol))},
			{Key: "base_uri", Type: "string", Init: strings.TrimSpace(baseURI)},
			{Key: "empty", Type: "string", Init: ""},
		},
		Methods: []DTLLogicPackMethod{
			{Name: "uri", MaxSteps: 12, Ops: []DTLLogicPackOp{{Op: "MAP_GET_STR", Dest: "r0", Map: "uris", MapKeyArg: "token_id"}, {Op: "LOAD_STR", Dest: "r1", Key: "empty"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "JMP_IF", Cond: "r2", Target: 5}, {Op: "RET_STR", Src: "r0"}, {Op: "LOAD_STR", Dest: "r3", Key: "base_uri"}, {Op: "RET_STR", Src: "r3"}}},
			{Name: "balanceof", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_U64", Dest: "r0", Map: "balances", MapKeyArg: "concat(owner,token_id)"}, {Op: "RET_U64", Src: "r0"}}},
			{Name: "balanceofbatch", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_U64", Dest: "r0", Map: "owner_totals", MapKeyArg: "owner"}, {Op: "RET_U64", Src: "r0"}}},
			{Name: "isapprovedforall", MaxSteps: 4, Ops: []DTLLogicPackOp{{Op: "MAP_GET_BOOL", Dest: "r0", Map: "operator_approvals", MapKeyArg: "concat(owner,operator)"}, {Op: "RET_BOOL", Src: "r0"}}},
			{Name: "setapprovalforall", MaxSteps: 12, Ops: []DTLLogicPackOp{{Op: "ARG_BOOL", Dest: "r0", Arg: "approved"}, {Op: "MAP_SET_BOOL", Map: "operator_approvals", MapKeyArg: "concat(caller,operator)", Src: "r0"}, {Op: "EMIT_LOG", Topic0Arg: "literal:ApprovalForAll", Topic1Arg: "caller", Topic2Arg: "operator", DataArg: "approved"}, {Op: "RET_OK"}}},
			{Name: "safetransferfrom", MaxSteps: 56, Ops: []DTLLogicPackOp{{Op: "CTX_CALLER", Dest: "r0"}, {Op: "ARG_STR", Dest: "r1", Arg: "from"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "JMP_IF", Cond: "r2", Target: 6}, {Op: "MAP_GET_BOOL", Dest: "r3", Map: "operator_approvals", MapKeyArg: "concat(from,caller)"}, {Op: "ASSERT", Cond: "r3", Message: "dtl: unauthorized"}, {Op: "ARG_U64", Dest: "r4", Arg: "amount"}, {Op: "MAP_GET_U64", Dest: "r5", Map: "balances", MapKeyArg: "concat(from,token_id)"}, {Op: "CMP_GTE", Dest: "r6", A: "r5", B: "r4"}, {Op: "ASSERT", Cond: "r6", Message: "dtl: insufficient balance"}, {Op: "SUB_U64", Dest: "r7", A: "r5", B: "r4"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(from,token_id)", Src: "r7"}, {Op: "MAP_GET_U64", Dest: "r8", Map: "balances", MapKeyArg: "concat(to,token_id)"}, {Op: "ADD_U64", Dest: "r9", A: "r8", B: "r4"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(to,token_id)", Src: "r9"}, {Op: "MAP_GET_U64", Dest: "r10", Map: "owner_totals", MapKeyArg: "from"}, {Op: "SUB_U64", Dest: "r11", A: "r10", B: "r4"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "from", Src: "r11"}, {Op: "MAP_GET_U64", Dest: "r12", Map: "owner_totals", MapKeyArg: "to"}, {Op: "ADD_U64", Dest: "r13", A: "r12", B: "r4"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "to", Src: "r13"}, {Op: "EMIT_LOG", Topic0Arg: "literal:TransferSingle", Topic1Arg: "caller", Topic2Arg: "from", Topic3Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "safebatchtransferfrom", MaxSteps: 56, Ops: []DTLLogicPackOp{{Op: "CTX_CALLER", Dest: "r0"}, {Op: "ARG_STR", Dest: "r1", Arg: "from"}, {Op: "CMP_EQ", Dest: "r2", A: "r0", B: "r1"}, {Op: "JMP_IF", Cond: "r2", Target: 6}, {Op: "MAP_GET_BOOL", Dest: "r3", Map: "operator_approvals", MapKeyArg: "concat(from,caller)"}, {Op: "ASSERT", Cond: "r3", Message: "dtl: unauthorized"}, {Op: "ARG_U64", Dest: "r4", Arg: "amount"}, {Op: "MAP_GET_U64", Dest: "r5", Map: "balances", MapKeyArg: "concat(from,token_id)"}, {Op: "CMP_GTE", Dest: "r6", A: "r5", B: "r4"}, {Op: "ASSERT", Cond: "r6", Message: "dtl: insufficient balance"}, {Op: "SUB_U64", Dest: "r7", A: "r5", B: "r4"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(from,token_id)", Src: "r7"}, {Op: "MAP_GET_U64", Dest: "r8", Map: "balances", MapKeyArg: "concat(to,token_id)"}, {Op: "ADD_U64", Dest: "r9", A: "r8", B: "r4"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(to,token_id)", Src: "r9"}, {Op: "MAP_GET_U64", Dest: "r10", Map: "owner_totals", MapKeyArg: "from"}, {Op: "SUB_U64", Dest: "r11", A: "r10", B: "r4"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "from", Src: "r11"}, {Op: "MAP_GET_U64", Dest: "r12", Map: "owner_totals", MapKeyArg: "to"}, {Op: "ADD_U64", Dest: "r13", A: "r12", B: "r4"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "to", Src: "r13"}, {Op: "EMIT_LOG", Topic0Arg: "literal:TransferBatch", Topic1Arg: "caller", Topic2Arg: "from", Topic3Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "mint", MaxSteps: 40, Ops: []DTLLogicPackOp{{Op: "REQUIRE_ROLE", Key: "minter"}, {Op: "ARG_U64", Dest: "r0", Arg: "amount"}, {Op: "MAP_GET_U64", Dest: "r1", Map: "balances", MapKeyArg: "concat(to,token_id)"}, {Op: "ADD_U64", Dest: "r2", A: "r1", B: "r0"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(to,token_id)", Src: "r2"}, {Op: "MAP_GET_U64", Dest: "r3", Map: "owner_totals", MapKeyArg: "to"}, {Op: "ADD_U64", Dest: "r4", A: "r3", B: "r0"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "to", Src: "r4"}, {Op: "MAP_GET_U64", Dest: "r5", Map: "total_supplies", MapKeyArg: "token_id"}, {Op: "ADD_U64", Dest: "r6", A: "r5", B: "r0"}, {Op: "MAP_SET_U64", Map: "total_supplies", MapKeyArg: "token_id", Src: "r6"}, {Op: "LOAD_STR", Dest: "r7", Key: "base_uri"}, {Op: "MAP_SET_STR", Map: "uris", MapKeyArg: "token_id", Src: "r7"}, {Op: "EMIT_LOG", Topic0Arg: "literal:TransferSingle", Topic1Arg: "caller", Topic2Arg: "contract", Topic3Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "mintbatch", MaxSteps: 40, Ops: []DTLLogicPackOp{{Op: "REQUIRE_ROLE", Key: "minter"}, {Op: "ARG_U64", Dest: "r0", Arg: "amount"}, {Op: "MAP_GET_U64", Dest: "r1", Map: "balances", MapKeyArg: "concat(to,token_id)"}, {Op: "ADD_U64", Dest: "r2", A: "r1", B: "r0"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(to,token_id)", Src: "r2"}, {Op: "MAP_GET_U64", Dest: "r3", Map: "owner_totals", MapKeyArg: "to"}, {Op: "ADD_U64", Dest: "r4", A: "r3", B: "r0"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "to", Src: "r4"}, {Op: "MAP_GET_U64", Dest: "r5", Map: "total_supplies", MapKeyArg: "token_id"}, {Op: "ADD_U64", Dest: "r6", A: "r5", B: "r0"}, {Op: "MAP_SET_U64", Map: "total_supplies", MapKeyArg: "token_id", Src: "r6"}, {Op: "LOAD_STR", Dest: "r7", Key: "base_uri"}, {Op: "MAP_SET_STR", Map: "uris", MapKeyArg: "token_id", Src: "r7"}, {Op: "EMIT_LOG", Topic0Arg: "literal:TransferBatch", Topic1Arg: "caller", Topic2Arg: "contract", Topic3Arg: "to", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "burn", MaxSteps: 32, Ops: []DTLLogicPackOp{{Op: "ARG_U64", Dest: "r0", Arg: "amount"}, {Op: "MAP_GET_U64", Dest: "r1", Map: "balances", MapKeyArg: "concat(caller,token_id)"}, {Op: "CMP_GTE", Dest: "r2", A: "r1", B: "r0"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: insufficient balance"}, {Op: "SUB_U64", Dest: "r3", A: "r1", B: "r0"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(caller,token_id)", Src: "r3"}, {Op: "MAP_GET_U64", Dest: "r4", Map: "owner_totals", MapKeyArg: "caller"}, {Op: "SUB_U64", Dest: "r5", A: "r4", B: "r0"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "caller", Src: "r5"}, {Op: "MAP_GET_U64", Dest: "r6", Map: "total_supplies", MapKeyArg: "token_id"}, {Op: "SUB_U64", Dest: "r7", A: "r6", B: "r0"}, {Op: "MAP_SET_U64", Map: "total_supplies", MapKeyArg: "token_id", Src: "r7"}, {Op: "EMIT_LOG", Topic0Arg: "literal:TransferSingle", Topic1Arg: "caller", Topic2Arg: "caller", Topic3Arg: "literal:burn", DataArg: "token_id"}, {Op: "RET_OK"}}},
			{Name: "burnbatch", MaxSteps: 32, Ops: []DTLLogicPackOp{{Op: "ARG_U64", Dest: "r0", Arg: "amount"}, {Op: "MAP_GET_U64", Dest: "r1", Map: "balances", MapKeyArg: "concat(caller,token_id)"}, {Op: "CMP_GTE", Dest: "r2", A: "r1", B: "r0"}, {Op: "ASSERT", Cond: "r2", Message: "dtl: insufficient balance"}, {Op: "SUB_U64", Dest: "r3", A: "r1", B: "r0"}, {Op: "MAP_SET_U64", Map: "balances", MapKeyArg: "concat(caller,token_id)", Src: "r3"}, {Op: "MAP_GET_U64", Dest: "r4", Map: "owner_totals", MapKeyArg: "caller"}, {Op: "SUB_U64", Dest: "r5", A: "r4", B: "r0"}, {Op: "MAP_SET_U64", Map: "owner_totals", MapKeyArg: "caller", Src: "r5"}, {Op: "MAP_GET_U64", Dest: "r6", Map: "total_supplies", MapKeyArg: "token_id"}, {Op: "SUB_U64", Dest: "r7", A: "r6", B: "r0"}, {Op: "MAP_SET_U64", Map: "total_supplies", MapKeyArg: "token_id", Src: "r7"}, {Op: "EMIT_LOG", Topic0Arg: "literal:TransferBatch", Topic1Arg: "caller", Topic2Arg: "caller", Topic3Arg: "literal:burn", DataArg: "token_id"}, {Op: "RET_OK"}}},
		},
		Limits: dtlStandardTemplateLimits(),
	}
	abiRaw, err := json.Marshal(pack.ABI)
	if err != nil {
		return nil, nil, nil, err
	}
	return pack, abiRaw, []string{"IMSC1155"}, nil
}
