# MSC Cross-Chain Bridge Production Gate

Do not activate a value-bearing bridge route until every required gate for that
route is green. A route is evaluated independently; passing BNB Chain does not
activate Ethereum, Tron, Solana, or any other network.

## Current Trust Model

The repository currently implements a guarded federated bridge:

- Source events use `chain_id + source_tx_hash + log_index` as the permanent
  bridge replay identity.
- Observer artifacts use `msc-bridge-observation-v3`; embedded event proofs use
  `msc-bridge-v5`. Older artifacts are rejected instead of migrated silently.
- Source finality is anchored by an append-only, threshold-signed checkpoint.
- A v2 checkpoint binds source chain, height, observed height, source block
  hash, event root, transaction root, receipt/state root, parent checkpoint,
  and issue time.
- Deposit and unlock proofs must bind their canonical event payload to the
  checkpoint event root with a Merkle proof.
- EVM artifacts additionally prove the source transaction and successful
  receipt at one trie index against the signed `transactionsRoot` and
  `receiptsRoot`, then bind the exact transaction-local log to the bridge ABI.
- TRON artifacts reconstruct full Java-Tron transaction protobuf bytes,
  recompute the complete block `txTrieRoot`, and attach a transaction Merkle
  branch that the gateway binds to the event's source transaction ID.
- Every unlock proof carries a canonical 32-byte withdrawal ID and must match
  the exact threshold-authorized MSC burn package; recipient/amount matching
  alone never completes a transfer.
- Registered source bridge validators sign source checkpoints/events.
- A separate DTL authority committee signs the MSC mint certificate.
- Mint and withdrawal execution use independent certificates and key roles.
- EVM withdrawals use a separate offline secp256k1 EIP-712 vault committee;
  observer Ed25519 and DTL authority signatures are never submitted as vault
  release signatures.
- Active contract metadata must name a chain-compatible execution adapter and
  exact non-zero runtime code/program hash. This gateway release implements
  `evm_vault_v1` and `tron_vault_v1`. Solana adapter metadata can be staged for
  testing but cannot be activated until its custody implementation ships.
- Checkpoint equivocation at the same chain height triggers emergency pause.
- Checkpoints, transfers, replay records, pause state, and events are committed
  into the persisted bridge state root.

This is not yet a chain-native trust-minimized light client. The threshold
checkpoint committee attests to source-chain finality. Route status must remain
`setup_required` or `testing` until the external gates below are proven.

The EVM observer implementation and offline ceremony are documented in
`docs/BRIDGE_EVM_OBSERVER.md`. The matching source-chain custody contract and
deployment ceremony are documented in `docs/BRIDGE_EVM_VAULT.md`.
Independent escrow-versus-wrapped-supply monitoring and the one-way automatic
pause operator are documented in `docs/BRIDGE_RECONCILIATION.md`.
The solidified-block Tron adapter and its address/log-index rules are documented
in `docs/BRIDGE_TRON_OBSERVER.md`.
The matching TIP-712 custody vault, TRONBox deployment ceremony, and offline
release workflow are documented in `docs/BRIDGE_TRON_VAULT.md`.
The fail-closed evidence bundle, DTL snapshot attestations, and exact activation
order are documented in `docs/BRIDGE_RELEASE_GATE.md`.
The finalized-slot Solana adapter, frozen program-event bytes, and
case-sensitive identifier rules are documented in
`docs/BRIDGE_SOLANA_OBSERVER.md`.

## Contract Gate

Required for each route:

- Source lock/unlock contract or program deployed from reproducible source.
- Deployment transaction and exact bytecode/program hash published.
- Official source token contract or mint independently verified.
- Deposit mode and emitted event schema frozen and versioned.
- Withdrawal nonce, replay protection, daily limit, and pause behavior tested.
- Upgrade/admin privileges documented and protected by threshold governance.
- Independent bridge and token-integration audits published.
- Escrow balance and wrapped supply reconciliation tested.

Never place a plain wallet address in an active contract route when the route is
configured as `contract_call`.

## Observer And Proof Gate

Required for each source chain:

- At least two independently operated observers and independent RPC providers.
- Observer derives the canonical event from source receipts/logs, not relayer
  text fields.
- Chain-specific finality policy is documented and tested against reorgs.
- Observer signs the exact `msc-bridge-checkpoint-v2` canonical payload.
- Event root construction is deterministic across observer implementations.
- Conflicting block, receipt, state, or event roots are detected and alerted.
- Stale checkpoints stop route readiness without stopping proof inspection.
- Observer lag, RPC disagreement, signer count, and checkpoint age are monitored.

Chain-native proof work still required:

| Chain family | Required evidence |
| --- | --- |
| EVM | Canonical header/finality source beyond the threshold checkpoint |
| Tron | Solidified block evidence, transaction-info/event inclusion, TRC20 contract binding |
| Solana | Finalized commitment, transaction/program evidence, instruction/log and mint binding |
| UTXO | Header work/finality policy, transaction Merkle inclusion, output/script binding |
| IBC-style | Verified consensus state, membership proof, channel/packet sequence binding |

## Key Separation Gate

- Bridge source validators and DTL mint authorities are different key sets.
- No signer key is stored in the web portal, node config, repository, or logs.
- Production keys are generated in HSM/KMS or an equivalent protected signer.
- Threshold remains available after losing one signer, but one key cannot mint.
- Key rotation, revocation, and compromised-signer drills are completed.
- Historical checkpoint verification remains supported across planned rotations.
- Ceremony records contain public keys and commitments, never private material.

## Asset And Accounting Gate

- Source and MSC decimals conversion has round-trip and truncation tests.
- Mint amount never exceeds proven locked amount.
- Unlock amount never exceeds finalized burned amount.
- Wrapped maximum supply and per-route daily limits are configured.
- `source escrow == wrapped circulating supply + pending accounting adjustments`
  is continuously reconciled.
- The TRON v2 reconciler verifies canonical mainnet identity, three independent
  API hosts, exact vault/token runtime hashes, a stable solidified head, and
  quorum agreement on `trackedEscrow`, actual USDT balance, and pause state.
- Any TRON API split view, code-hash mismatch, moving-head read, stale MSC
  snapshot, or registry/deployment mismatch is `unknown` and pages operators;
  it cannot be treated as healthy evidence.
- Abnormal mint rate, escrow deficit, or reconciliation mismatch pauses the route.
- Sufficient source-chain gas and unlock liquidity are monitored.

## Testnet Evidence Gate

Retain reproducible evidence for every route:

1. Valid lock, checkpoint, proof, threshold mint certificate, MSC finality.
2. Valid burn, burn finality, unlock authorization, source unlock finality.
3. Duplicate source event rejection after restart and state restore.
4. Under-quorum, invalid signature, wrong contract, wrong asset, wrong recipient,
   wrong amount, wrong log index, and wrong root rejection.
5. Source reorg and stale-checkpoint route shutdown.
6. Same-height checkpoint equivocation emergency pause.
7. Validator and DTL authority key rotation.
8. Daily-limit exhaustion and controlled reset.
9. Crash between every transfer state transition and successful recovery.
10. At least 72 hours of multi-observer testnet soak without reconciliation drift.

Local regression command:

```powershell
go test . -run "TestBridge|TestGuarded|TestDefaultBridgeCatalog|TestPausedWrappedToken|TestSupplyInvariant" -count=1
go test ./bridgeobserver ./bridgereconcile ./ops/bridge_observer ./ops/bridge_reconciler -count=1
go build .
go build -trimpath -o bin\bridge-reconciler.exe .\ops\bridge_reconciler
go test ./bridgegate ./ops/bridge_release_gate -count=1
go build -trimpath -o bin\bridge-release-gate.exe .\ops\bridge_release_gate
node --check ui\bridge_admin.js
node --check ui\wallet_pages.js
node --test ui\bridge_admin.test.mjs
cd contracts\bridge
npm audit --audit-level=high
npm run compile:tron
npm test
```

## Operations Gate

Required dashboards and alerts:

- `msc_bridge_emergency_paused`
- `msc_bridge_finality_checkpoints`
- `msc_bridge_finality_checkpoints_fresh`
- `msc_bridge_validators_active`
- `msc_bridge_routes_ready`
- `msc_bridge_transfers_pending`
- `msc_bridge_transfers_completed`
- `msc_bridge_reconciler_up`
- `msc_bridge_reconciliation_critical_deficit`
- `msc_bridge_reconciliation_wrapped_supply_raw`
- `msc_bridge_reconciliation_tracked_backing_raw`
- `msc_bridge_reconciliation_route_vault_balance_deficit_raw`
- Per-route volume, daily-limit utilization, observer lag, and signer availability

The incident runbook must identify who can pause, how source observers are
isolated, how evidence is retained, and how a route is re-enabled after review.

## Activation Ladder

1. **Verification only:** proofs can be inspected; no mint or unlock execution.
2. **Testing:** audited testnet deployment, low caps, staffed monitoring, manual
   review of every transfer.
3. **Guarded production pilot:** one route, low daily cap, threshold committees,
   public audit/evidence, reconciliation after every batch.
4. **Active:** raise limits only after the soak, incident drill, and accounting
   gates remain green.

This release starts with exactly one catalog route: official TRC20 USDT on TRON
mainnet. Other chain families are later releases and must not be activated from
stale release bundles. TRON still requires its own audit, rehearsal evidence,
independent observers, protected keys, liquidity, and monitoring before funds
are accepted.

For TRON, a route may not be unpaused until `bridge-release-gate verify` creates
a green immutable report from the published audit and complete local evidence
bundle. The gate verifies the production observer checkpoint, exact live token
route, reconciliation, paused MSC/Tron state, committee separation, and DTL
snapshot signatures. It also requires recoverable 4-of-5 approval by the
deployed TRON release committee over the exact final manifest and resolves all
typed pause/soak evidence to distinct hash-pinned raw files. It never broadcasts
the final governance action.

## Release Decision

The bridge is not production-ready while any of these are missing:

- live audited source contracts/programs
- official token identities verified at deployment time
- real independent observers or chain-native light clients
- protected production signer ceremonies
- escrow liquidity and automated reconciliation
- route-specific testnet and reorg evidence
- external security audit and monitored incident drills

Configuration presence, a green UI, or a locally accepted synthetic proof is not
evidence that an external route is safe to carry user funds.
