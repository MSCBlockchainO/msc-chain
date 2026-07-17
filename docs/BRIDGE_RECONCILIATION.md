# MSC Bridge Escrow Reconciliation

The bridge reconciler independently compares source-chain custody with the
consensus-reported MSC wrapped-token supply. It is an operational circuit
breaker, not a relayer, signer, mint authority, or substitute for an audit.

## Invariant

For every configured wrapped token, the reconciler groups every source route
that backs that token and checks:

```text
sum(source vault tracked escrow) >= MSC wrapped token total supply
source vault token balance >= source vault tracked escrow
```

A positive difference is expected while a finalized source deposit is waiting
to mint or an MSC burn is waiting for its source unlock. A negative difference
is a collateral deficit and is critical.

The MSC node exposes raw integer accounting at `GET /bridge/accounting` and
`GET /v1/bridge/accounting`. The response groups routes by local token ID and
does not expose RPC URLs, private keys, admin tokens, or signer material.

## Source Verification

For each EVM route the reconciler:

1. Queries at least two independent RPC providers.
2. Verifies the expected EVM chain ID.
3. Selects a common block behind the configured confirmation boundary.
4. Requires contract bytecode for both vault and source token at that block.
5. Calls `trackedEscrow(token)`, token `balanceOf(vault)`, and `paused()` at the
   exact block tag.
6. Requires quorum agreement on block hash and every returned value.
7. Stores only hashes of provider endpoints in reports.

For the production TRON route the v2 reconciler:

1. Requires exactly `tron-mainnet`, route `usdt-tron-mainnet`, the canonical
   mainnet genesis block, official Tether TRC20 address, six decimals, at least
   64 confirmations, and three independently hosted HTTPS APIs.
2. Verifies the configured genesis on every provider.
3. Fetches the vault and token runtime bytecode and requires their Keccak-256
   hashes to match the immutable audited deployment record.
4. Reads `trackedEscrow(token)`, USDT `balanceOf(vault)`, and `paused()` through
   `/walletsolidity/triggerconstantcontract`, which executes against confirmed
   state.
5. Reads `tokenRoutes(token)` and requires the route to be enabled with the
   exact audited min, max, daily lock, and daily unlock limits.
6. Brackets the four concurrent contract calls with
   `/walletsolidity/getnowblock` and rejects an endpoint if its solidified head
   changes during the reads.
7. Requires quorum agreement on the exact solidified block, both runtime code
   hashes, token-route limits, tracked escrow, token balance, and pause state. Two conflicting
   views that each reach quorum are rejected explicitly.
8. Stores only hashes of provider endpoints in reports. API credentials must
   be supplied by a protected proxy or operator-side secret injection, never
   committed to this config.

RPC disagreement, stale MSC accounting, missing routes, metadata mismatch, or
decimal conversion dust produces `unknown`. It does not fabricate a deficit.

## Build And Test

```powershell
go test ./bridgereconcile ./ops/bridge_reconciler -count=1
go build -trimpath -o bin\bridge-reconciler.exe .\ops\bridge_reconciler
```

For TRON, start from the `reconciler_config` embedded in the immutable output of
`npm run deploy:tron`, or from
`deploy/bridge/tron-reconciler.example.json`. Replace the MSC URL, wrapped-token
ID, and three API hosts; do not manually retype the vault, token, genesis, or
runtime hashes when a deployment record is available. For EVM, use
`deploy/bridge/evm-reconciler.example.json`. Every accounting route for a
shared local token must be present; partial route coverage fails closed.

Schema `msc-bridge-reconciler-config-v2` is required for TRON. Legacy v1
configs remain accepted only for EVM routes.

Run one read-only check:

```powershell
bin\bridge-reconciler.exe once --config C:\msc-bridge\tron-mainnet-reconciler.json
```

Run continuous monitoring:

```powershell
bin\bridge-reconciler.exe run --config C:\msc-bridge\tron-mainnet-reconciler.json
```

The default metrics listener is `127.0.0.1:9470`. Expose it to Prometheus only
through the operator network or a protected metrics proxy.

## Automatic Pause

Keep `auto_pause` false during initial testnet evidence collection. After the
pause path has been exercised and reviewed, configure:

```json
{
  "auto_pause": true,
  "admin_settings_url": "https://msc-node.example/bridge/admin/settings",
  "admin_token_env": "MSC_BRIDGE_ADMIN_TOKEN",
  "failure_threshold": 3
}
```

The admin URL must have the exact scheme and host as `msc_accounting_url`; this
prevents sending the bearer token to another origin. The token is read from the
environment and never appears in configuration, reports, metrics, or logs.

Only consecutive quorum-proven deficits count toward the threshold. A
successful action submits only `paused: true`. The reconciler never unpauses the
bridge; recovery requires human investigation, reconciliation evidence, and an
explicit admin/governance decision.

## Required Alerts

Page operators immediately for:

- `MSCBridgeCollateralDeficit`
- `MSCBridgeVaultBalanceDeficit`
- `MSCBridgeReconcilerDown`

Investigate `MSCBridgeReconciliationUnknown` before route readiness is restored.
Retain the JSON report, the MSC state root, source block hashes, deployment
record, and observer artifacts as incident evidence.
