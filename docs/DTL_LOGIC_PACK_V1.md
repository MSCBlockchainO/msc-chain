# DTL Logic Pack v1 (No-EVM User Logic)

Status: Proposed extension  
Target: Let users write custom logic without enabling EVM or custom VM bytecode.

## 1. Problem

Current DTL contracts are deterministic but limited to single-op method patterns.
For complex DeFi/GameFi workflows, users need richer logic while preserving:

- deterministic execution,
- bounded resource usage,
- no arbitrary VM bytecode runtime.

## 2. Core Idea

Use a **Proof-Carrying Logic Pack** model:

1. User writes DSL source (`dtl-script-v1`).
2. Compiler converts source to canonical IR (`logic_pack` JSON).
3. Node validates IR statically at deploy time.
4. Calls execute bounded deterministic ops only.
5. All nodes derive identical post-state.

This gives "smart-contract style" customization without EVM.

## 3. Upgrade Scope

This spec extends existing tx families:

- `CONTRACT_DEPLOY` with `lang = "dtl-script-v1"` and `logic_pack`.
- `CONTRACT_CALL` unchanged envelope; method resolution uses `logic_pack`.

Activation gate (recommended):

- `dtl.logic_pack_v1_enabled=true` (network config / feature flag).

## 4. Deploy Payload Schema (Consensus Object)

`CONTRACT_DEPLOY` payload in logic-pack mode:

```json
{
  "creator": "MSC0...",
  "name": "Counter",
  "lang": "dtl-script-v1",
  "version": 2,
  "logic_pack": {
    "version": 1,
    "name": "Counter",
    "abi": [
      {
        "name": "inc",
        "args": [{ "name": "delta", "type": "u64" }],
        "returns": []
      }
    ],
    "storage": [
      { "key": "count", "type": "u64", "init": "0" }
    ],
    "methods": [
      {
        "name": "inc",
        "max_steps": 8,
        "ops": [
          { "op": "LOAD_U64", "dest": "r0", "key": "count" },
          { "op": "ARG_U64", "dest": "r1", "arg": "delta" },
          { "op": "ADD_U64", "dest": "r2", "a": "r0", "b": "r1" },
          { "op": "STORE_U64", "key": "count", "src": "r2" },
          { "op": "RET_OK" }
        ]
      }
    ],
    "limits": {
      "max_reads": 16,
      "max_writes": 16,
      "max_token_transfers": 4
    }
  }
}
```

## 5. DSL Grammar (Compiler Input)

Reference EBNF for `dtl-script-v1`:

```ebnf
program        = contract_decl ;

contract_decl  = "contract" ident "{" { state_decl | method_decl } "}" ;
state_decl     = "state" type ident [ "=" literal ] ";" ;

method_decl    = "fn" ident "(" [ param_list ] ")" [ "->" ret_type ] block ;
param_list     = param { "," param } ;
param          = type ident ;
ret_type       = type | "void" ;

block          = "{" { stmt } "}" ;
stmt           = assign_stmt ";"
               | if_stmt
               | assert_stmt ";"
               | transfer_stmt ";"
               | return_stmt ";"
               ;

assign_stmt    = ident "=" expr ;
if_stmt        = "if" "(" cond ")" block [ "else" block ] ;
assert_stmt    = "assert" "(" cond "," string_lit ")" ;
transfer_stmt  = "token_transfer" "(" string_lit "," ident "," ident ")" ;
return_stmt    = "return" [ expr ] ;

expr           = term { ("+" | "-" | "*" | "/") term } ;
term           = ident | literal ;
cond           = expr relop expr ;
relop          = "==" | "!=" | ">" | ">=" | "<" | "<=" ;

type           = "u64" | "string" | "bool" | "address" ;
literal        = number | string_lit | bool_lit ;
ident          = letter { letter | digit | "_" } ;
```

Non-goal in v1:

- no loops (`for`, `while`, `recursion`) in source language.

## 6. IR Instruction Set (Deterministic)

Required v1 opcodes:

- Input/state:
  - `ARG_U64`, `ARG_STR`, `LOAD_U64`, `LOAD_STR`
- State writes:
  - `STORE_U64`, `STORE_STR`
- Arithmetic:
  - `ADD_U64`, `SUB_U64`, `MUL_U64`, `DIV_U64`
- Compare/control:
  - `CMP_EQ`, `CMP_NEQ`, `CMP_GT`, `CMP_GTE`, `CMP_LT`, `CMP_LTE`
  - `JMP_IF`, `JMP`
- Safety/actions:
  - `ASSERT`
  - `TOKEN_TRANSFER`
  - `RET_OK`, `RET_ERR`

Determinism constraints:

- integer domain: unsigned 64-bit only,
- checked arithmetic (overflow/underflow = fail),
- no wall clock, randomness, floating point, network IO, filesystem IO.

## 7. Static Validator Rules (Deploy-Time)

Mandatory checks:

1. Canonical hash:
   - `logic_pack_hash = sha256(canonical_json(logic_pack))`
2. Limits:
   - `1 <= methods <= 64`
   - `1 <= ops_per_method <= 256`
   - `total_ops <= 4096`
   - `storage_keys <= 256`
3. CFG safety:
   - all jump targets in range,
   - no backward jumps (forward-only control flow),
   - all paths terminate with `RET_OK`/`RET_ERR`.
4. Type safety:
   - register/arg type consistency,
   - storage key type consistency,
   - ABI arg names unique.
5. Resource caps:
   - writes/call <= `limits.max_writes`,
   - reads/call <= `limits.max_reads`,
   - token transfers/call <= `limits.max_token_transfers`.
6. Action safety:
   - `TOKEN_TRANSFER` token id must exist,
   - source mode in `{caller, contract}`,
   - debit/credit conservation must hold.

Deploy fails if any check fails.

## 8. Runtime Validator Rules (Call-Time)

For `CONTRACT_CALL`:

1. ABI method exists.
2. All required args present, type parse success.
3. Step counter never exceeds method `max_steps`.
4. Runtime read/write/transfer counters respect declared limits.
5. Any failed `ASSERT` or checked-arithmetic failure reverts call.

## 9. Fee Model (Adoption-Friendly)

Recommended deterministic fee formula:

```text
fee = base_call_fee
    + (executed_steps * step_price)
    + (state_writes * write_price)
    + (token_transfers * transfer_price)
```

Policy:

- cheap reads, costlier writes,
- bounded max fee per call,
- transparent estimator in IDE before submit.

## 10. Trustless Execution Modes

Phase A (now):

- all validators re-execute IR deterministically.

Phase B (later):

- optional proof path (`execution_proof`) for fast verification.
- deterministic fallback re-execution remains mandatory for safety.

## 11. Compatibility Notes

- Existing `CONTRACT_DEPLOY`/`CONTRACT_CALL` behavior remains valid.
- `lang="solidity-like"` and `lang="vyper-like"` can compile to either:
  - legacy op-method mode, or
  - `dtl-script-v1` logic pack mode.
- Network activation must be feature-gated to avoid consensus splits.

## 12. Minimum Implementation Checklist

1. Add `logic_pack` field to contract deploy state model.
2. Add canonical JSON encoder and hash persistence.
3. Add static deploy validator as per Section 7.
4. Add runtime interpreter with step/read/write counters.
5. Add fee meter from Section 9.
6. Add IDE compile/simulate UI:
   - source -> IR preview,
   - gas/fee estimate,
   - deterministic trace.
