# MSC Chain

Go-based blockchain node with libp2p networking, validator consensus, snapshots, and explorer endpoints.

## What Was Improved (This Update)

1. Added legacy block-sync compatibility on consensus stream:
   - `MsgGetBlocks` now responds with real blocks.
   - `MsgBlocksBatch` is now handled and applied.
2. Added keepalive response:
   - `MsgPing` now returns `MsgPong`.
3. Removed panic paths in runtime-critical code:
   - `secureWalletPath()` now falls back safely when home directory resolution fails.
   - `StoreBlock()` now logs and returns on error instead of crashing the node.
4. Added explicit `MsgPong` message constant.

## Quick Start (Local Multi-Node)

Run nodes in separate terminals:

```powershell
go run . --mode=full --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657
go run . --mode=full --id=B --port=7002 --datadir=data/B --rpcaddr 127.0.0.1:26658
go run . --mode=full --id=C --port=7003 --datadir=data/C --rpcaddr 127.0.0.1:26659
go run . --mode=full --id=D --port=7004 --datadir=data/D --rpcaddr 127.0.0.1:26660
```

Explorer URL:

- `https://127.0.0.1:26657/explorer.html`

If browser warns about self-signed cert, proceed once for local development.

## Security Baseline (Recommended)

Set API token and DB encryption key before running node:

```powershell
$env:MSC_RPC_TOKEN = "replace-with-strong-random-token"
$k = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($k)
$env:MSC_DB_ENCRYPTION_KEY = [Convert]::ToBase64String($k)
```

## Step-by-Step Improvement Plan

1. Networking reliability (done in this update): legacy sync + ping/pong + safer error handling.
2. Validator liveness hardening: auto-evict stale validators faster with bounded grace windows.
3. Catch-up performance: tuned snapshot cadence + bounded block-range serving + stricter sync deadlines.
4. Security hardening: production TLS cert rotation, strict CORS allowlist, read endpoint auth enabled.
5. Observability: add Prometheus/Grafana alerts for quorum loss, height lag, peer churn, and sync stalls.

## Validation

Run:

```powershell
go test ./...
```

## DTL Spec (Native Token Ledger)

For protocol-native token model without smart contracts/VM, see:

- `docs/DTL_SPEC_V1.md`
- `docs/DTL_LOGIC_PACK_V1.md` (proposed user-logic extension without EVM)
- `docs/dtl_logic_pack_v1.schema.json` (machine-readable schema for `logic_pack`)

DTL custom web IDE (native token workflows) is available at:

- `https://127.0.0.1:26657/dtl_ide.html`

## EVM/VM Removal Status

EVM/VM execution and compatibility endpoints are permanently removed from runtime paths in this repo build.

- `TxEVM` transactions are rejected.
- EVM/compat JSON-RPC methods are rejected.
- EVM/remix fork scaffolding and Solidity template assets are removed from this repository.
