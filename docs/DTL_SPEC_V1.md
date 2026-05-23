# DTL-SPEC v1 (Decentralized Token Ledger)

Status: Draft v1.0  
Scope: Native token protocol for MSC chain (no smart contracts, no VM execution)

## 1. Objective

DTL defines a protocol-native token system where:

- Token logic is enforced by consensus rules, not user-deployed code.
- Control is distributed with N-of-M governance signers.
- State transitions are deterministic across all nodes.

Design principle:

`No single key, no single node, and no single developer can unilaterally control a token.`

## 2. Non-Goals

- No arbitrary smart-contract execution.
- No external contract calls.
- No dynamic bytecode/runtime upgrade path in token logic.

## 3. Core Data Model

All structures are consensus state objects.

### 3.1 TokenState

`TokenState` fields:

- `token_id` (bytes32/hex)
- `name` (string, 1..64)
- `symbol` (string, 1..16)
- `decimals` (uint8, default 18, max 18)
- `max_supply` (uint256, immutable)
- `total_supply` (uint256)
- `paused` (bool)
- `freeze_enabled` (bool)
- `tax_bps` (uint16, immutable in v1)
- `authority_signers` (set<address>)
- `authority_threshold` (uint16, N-of-M)
- `metadata_uri` (string, optional)

### 3.2 BalanceState

- Key: `(token_id, account)`
- Value: `balance (uint256)`

### 3.3 Governance Certificate (GCERT)

Used for governance-authorized operations.

- `token_id`
- `epoch`
- `action`
- `action_payload_hash`
- `signers[]`
- `signer_public_keys[]`
- `signatures[]`

Validation:

- Signers must be in `authority_signers`
- Unique signers only
- `len(signers) == len(signer_public_keys) == len(signatures)`
- `address(signers[i])` must match `signer_public_keys[i]`
- Valid signatures over canonical message
- `len(valid_signers) >= authority_threshold`
- `epoch` must be fresh (anti-replay window)

Canonical governance-signing message:

`MSC|DTL|GCERT|<chain_id>|<token_id>|<epoch>|<action>|<action_payload_hash>`

## 4. Transaction Types (User Plane)

DTL currently supports protocol-native token, DeFi, and GameFi transaction families:

1. `TOKEN_CREATE`
2. `TOKEN_TRANSFER`
3. `TOKEN_MINT` (requires GCERT)
4. `TOKEN_BURN`
5. `POOL_CREATE`
6. `POOL_ADD_LIQUIDITY`
7. `POOL_REMOVE_LIQUIDITY`
8. `POOL_SWAP`
9. `DUEL_CREATE`
10. `DUEL_JOIN`
11. `DUEL_REVEAL`
12. `DUEL_FINALIZE`
13. `LEND_MARKET_CREATE`
14. `LEND_DEPOSIT_COLLATERAL`
15. `LEND_BORROW`
16. `LEND_REPAY`
17. `LEND_WITHDRAW_COLLATERAL`
18. `LEND_LIQUIDATE`
19. `TOURNAMENT_CREATE`
20. `TOURNAMENT_JOIN`
21. `TOURNAMENT_REVEAL`
22. `TOURNAMENT_FINALIZE`
23. `CONTRACT_DEPLOY`
24. `CONTRACT_CALL`

All are consensus-native. No arbitrary bytecode VM is required. `CONTRACT_*` uses a deterministic DTL op model.

## 5. Transaction Schemas

### 5.1 TOKEN_CREATE

Fields:

- `creator`
- `name`
- `symbol`
- `decimals`
- `max_supply`
- `initial_supply`
- `authority_signers[]`
- `authority_threshold`
- `freeze_enabled`
- `tax_bps`
- `metadata_uri`
- `nonce`
- `fee`
- `signature`

Rules:

- `1 <= authority_threshold <= len(authority_signers)`
- `initial_supply <= max_supply`
- `symbol` unique (chain-wide or namespace-based, implementation choice)
- `token_id = H(chain_id || creator || symbol || nonce)`

### 5.2 TOKEN_TRANSFER

Fields:

- `from`
- `to`
- `token_id`
- `amount`
- `nonce`
- `fee`
- `signature`

Rules:

- reject if token paused
- reject if sender frozen (if freeze enabled)
- reject if insufficient balance
- apply static tax if configured

### 5.3 TOKEN_MINT

Fields:

- `proposer`
- `to`
- `token_id`
- `amount`
- `gcert`
- `nonce`
- `fee`
- `signature`

Rules:

- valid `gcert.action == MINT`
- `total_supply + amount <= max_supply`
- mint target may be treasury/user as approved in GCERT payload

### 5.4 TOKEN_BURN

Fields:

- `from`
- `token_id`
- `amount`
- `nonce`
- `fee`
- `signature`

Rules:

- caller burns own balance only in v1
- `total_supply -= amount`

### 5.5 Trustless DeFi/GameFi Extensions

#### AMM Pools

- `POOL_CREATE`: creates canonical pair pool `(token_a, token_b)` with initial reserves and LP shares.
- `POOL_ADD_LIQUIDITY`: adds liquidity at pool ratio; mints LP shares.
- `POOL_REMOVE_LIQUIDITY`: burns LP shares; returns proportional reserves.
- `POOL_SWAP`: constant-product swap with bounded pool fee and user slippage guard.

Protocol invariants:

- pair uniqueness is deterministic (`pair_key`)
- LP mint/burn is deterministic from reserves and total LP supply
- taxed tokens are rejected for pool operations in this version to preserve invariant correctness

#### Commit-Reveal Duel (GameFi Primitive)

- `DUEL_CREATE`: creator locks stake + commitment hash.
- `DUEL_JOIN`: challenger locks equal stake + commitment hash.
- `DUEL_REVEAL`: each player reveals secret; hash must match commitment.
- `DUEL_FINALIZE`: deterministic payout:
  - both revealed -> winner from deterministic hash rule
  - one revealed after deadline -> revealer wins by forfeit
  - none revealed after deadline -> refund both
  - no join after deadline -> refund creator

This makes duel settlement fully on-chain and trustless (no admin/oracle settlement path).

#### Overcollateralized Lending (Trustless DeFi Primitive)

- `LEND_MARKET_CREATE`: directional market `(collateral_token, debt_token)` with risk params and initial debt liquidity.
- `LEND_DEPOSIT_COLLATERAL`: lock collateral into market position.
- `LEND_BORROW`: borrow debt token up to collateral factor limit.
- `LEND_REPAY`: repay debt.
- `LEND_WITHDRAW_COLLATERAL`: withdraw collateral only if health remains valid.
- `LEND_LIQUIDATE`: unhealthy positions can be permissionlessly liquidated.

Deterministic risk model:

- health check: `debt <= collateral * collateral_factor_bps / 10000`
- liquidation seize: `repay * (10000 + liq_bonus_bps) / 10000` (capped by borrower collateral)

#### Tournament Pool (GameFi Primitive)

- `TOURNAMENT_CREATE`: define entry fee, capacity, deadlines.
- `TOURNAMENT_JOIN`: player locks entry fee and submits commitment hash.
- `TOURNAMENT_REVEAL`: reveal secret matching commitment.
- `TOURNAMENT_FINALIZE`: on-chain deterministic payout:
  - candidates = players with valid reveal
  - no candidate => refund all entrants
  - else deterministic winner from hash over `(tournament_id, candidates, reveals)` gets full pot

#### Deterministic Contract Compatibility (Solidity/Vyper-like Patterns)

- `CONTRACT_DEPLOY`: deploy a contract blueprint with named methods and deterministic ops.
- `CONTRACT_CALL`: execute a named method with bounded args.

Supported op family in v1:

- `SET_STR`
- `SET_U64`
- `ADD_U64`
- `SUB_U64`
- `TOKEN_TRANSFER` (from caller or contract vault)

This gives Solidity/Vyper-style state-machine workflows in DTL while preserving deterministic execution and bounded complexity.

For the next upgrade path (user-authored custom logic without EVM/VM bytecode), see:

- `docs/DTL_LOGIC_PACK_V1.md`
- `docs/dtl_logic_pack_v1.schema.json`

## 6. Governance Control Plane (Protocol Plane)

To keep user tx surface fixed, admin controls are protocol governance actions, not new user tx types.

Supported governance actions:

- `PAUSE`
- `UNPAUSE`
- `FREEZE_ACCOUNT`
- `UNFREEZE_ACCOUNT`
- `ROTATE_AUTHORITY`

Each action is applied only with valid GCERT and deterministic payload hash checks.

## 7. Deterministic Execution Rules

Every node must execute identical steps:

1. Decode canonical transaction bytes.
2. Verify signature and nonce.
3. Verify fee and balance preconditions.
4. Verify token-specific invariants.
5. Apply state mutation in fixed order.
6. Emit deterministic event record.

Consensus safety requirement:

`Same input block + same pre-state => same post-state + same receipts`

## 8. Gas / Fee Schedule (Fixed)

DTL uses fixed operation weights (example baseline):

- `TOKEN_CREATE`: 120000
- `TOKEN_TRANSFER`: 21000
- `TOKEN_MINT`: 50000 (+ governance verify cost bounded)
- `TOKEN_BURN`: 25000
- `GCERT verify`: fixed cap by max signers (bounded M)

No runtime opcode metering exists in DTL.

## 9. Security Invariants

Must always hold:

- `0 <= total_supply <= max_supply`
- Sum of all balances equals `total_supply` (per token)
- Nonce strictly increases per account
- GCERT replay impossible within replay window
- Authority threshold checks enforced on every governance action
- AMM reserves and LP supply transition deterministically
- Duel vault payouts conserve total staked amount
- Lending debt/collateral transitions are collateral-factor bounded
- Tournament pot transitions are conservation-safe (winner payout or full refund)

## 10. Misbehavior and Slashing Hooks

Validators are slashable for:

- proposing/approving invalid mint beyond `max_supply`
- accepting invalid GCERT signatures/threshold
- equivocation on governance action at same `(token_id, epoch)`
- state transition mismatch vs deterministic rules

Recommended penalties:

- immediate reward = 0 for penalty window
- stake slash (configurable bps)
- jail for fixed epochs
- repeated severe faults => validator removal flow

## 11. Threat Model Notes

DTL removes common smart-contract class exploits:

- re-entrancy
- proxy upgrade abuse
- arbitrary hidden mint in code
- approval-race style ERC allowance bugs

Remaining risks:

- governance key compromise
- signer collusion at threshold
- liveness failures during signer outage

Security statement:

`Rug-pull is prevented unless governance threshold itself is compromised.`

## 12. Wallet-First Interfaces (Recommended)

Wallet/explorer should support protocol-native endpoints:

- `dtl_tokenInfo(token_id)`
- `dtl_balanceOf(token_id, account)`
- `dtl_totalSupply(token_id)`
- `dtl_authority(token_id)`
- `dtl_submitTx(raw_tx)`
- `dtl_estimateFee(tx_type, payload_size)`

No Remix/Solidity dependency required for token lifecycle.

## 13. Compatibility and Rollout

Rollout phases:

1. `v1-shadow`: validate DTL state transitions in parallel (read-only).
2. `v1-enforced`: reject non-DTL token lifecycle paths.
3. `v1-governed`: authority rotation and pause/freeze through GCERT.

## 14. Reference Positioning

Comparable framing:

- ERC-20: app-layer programmable token
- DTL: protocol-layer deterministic token ledger

One-line positioning:

`MSC uses a Decentralized Token Ledger (DTL) where token rules are protocol-native and enforced by N-of-M consensus governance, not smart contracts.`
