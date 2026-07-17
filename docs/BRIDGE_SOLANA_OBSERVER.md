# MSC Solana Bridge Observer

The Solana observer creates deterministic MSC checkpoint and event-proof
artifacts from one explicit finalized Solana slot returned by independent JSON
RPC providers. It is intended for SPL-token routes such as USDT only after the
custody program, mint identity, audits, and testnet gates are complete.

It does not deploy a Solana program, transfer SPL tokens, submit an MSC mint,
or hold MSC mint authority.

## Finalized Evidence

Every provider is queried with:

- `getSlot` using `commitment: finalized`
- `getBlock` using `commitment: finalized`, `encoding: json`,
  `transactionDetails: full`, `maxSupportedTransactionVersion: 0`, and
  `rewards: false`

The finalized head must cover `slot + confirmations - 1`. The observer binds
the case-sensitive blockhash, previous blockhash, parent slot, optional block
height/time, complete transaction-signature order, every transaction result,
complete program-log order, and decoded bridge events into one provider
agreement fingerprint.

Official Solana references:

- https://solana.com/docs/rpc/http/getslot
- https://solana.com/docs/rpc/http/getblock
- https://solana.com/docs/rpc/json-structures
- https://solana.com/docs/core/programs/syscall-reference

Solana does not expose an EVM-style receipt or state root through `getBlock`.
The shared v1 checkpoint field named `receipt_root` therefore carries the
SHA-256 fingerprint of the complete quorum-agreed block/transaction-result/log
view. This is a threshold-attested compatibility commitment, not a native
Solana receipt root or transaction-inclusion proof.

## Frozen Program Event

The audited custody program must emit one `sol_log_data` slice. RPC exposes it
as `Program data: <base64>`. The decoded bytes are:

| Offset | Size | Value |
| --- | ---: | --- |
| 0 | 8 | first 8 bytes of `SHA-256("MSC_BRIDGE_EVENT_V1")` |
| 8 | 1 | event version, currently `1` |
| 9 | 1 | `1` lock, `2` unlock |
| 10 | 32 | SPL token mint bytes |
| 42 | 8 | positive token base units, unsigned little-endian |
| 50 | 32 | external withdrawal ID; all zero for lock, non-zero for unlock |
| 82 | 1 | recipient encoding: `1` MSC text, `2` Solana pubkey bytes |
| 83 | 2 | recipient byte length, unsigned little-endian |
| 85 | N | recipient bytes, with no trailing data |

A lock requires recipient encoding `1`, an exact canonical 45-character MSC
address, and an all-zero withdrawal-ID slot. An unlock requires encoding `2`,
exactly 32 recipient pubkey bytes, and the non-zero 32-byte withdrawal ID from
the authorized MSC burn package. The ID is exposed as canonical lowercase
`0x` hex and is signed and Merkle-committed in the v4 event payload.
The event mint must equal the configured origin mint and the amount must be
positive. Decimal conversion is deterministic from configured token decimals.

The observer follows runtime `Program <id> invoke [depth]` and completion logs.
It accepts data only while the configured bridge program is the active runtime
frame and only when transaction metadata reports success. Text printed by a
different program, failed-transaction events, extra data slices, wrong mints,
and malformed recipients are rejected.

Solana has no native block-global event index. MSC defines `log_index` as the
zero-based position of the log message in the complete finalized block:
transaction order first, then each transaction's `logMessages` order. Every
unrelated and failed-transaction log message still consumes an index.

Program IDs, token mints, accounts, transaction signatures, and blockhashes are
Base58 and case-sensitive. They are committed exactly; lowercasing is invalid.

## Trust Boundary

This is a federated finalized-source observer, not a Solana light client. The
threshold bridge-validator committee attests that independent RPC providers
agreed on the finalized slot and complete result/log view. The artifact does
not prove transaction or program-log inclusion directly against Solana
consensus.

Production requires independently operated RPC infrastructure, an independently
audited custody program, protected upgrade authority, published deployment and
program hashes, verified SPL mint identity, and route-specific soak/recovery
evidence. Provider credentials belong behind operator-owned HTTPS proxies and
must never appear in observer configs or artifacts.

## Build And Configure

```powershell
go test ./bridgeobserver ./ops/bridge_observer -count=1
go build -trimpath -o bin\bridge-observer.exe .\ops\bridge_observer
```

Copy `deploy/bridge/solana-observer.example.json` to a protected operator
directory. Replace both placeholders only after independently checking the
audited program ID and official token mint.

## Observe And Sign

```powershell
bin\bridge-observer.exe observe-solana `
  --config C:\msc-bridge\solana-observer.json `
  --slot 378967388 `
  --previous-checkpoint bcp_<previous-id> `
  --output C:\msc-bridge\solana-unsigned-378967388.json
```

Each operator independently creates and reviews the unsigned artifact. Use the
existing `sign`, `merge`, and `verify` commands for the offline threshold
ceremony. Bridge observer keys must remain separate from DTL mint-authority
keys.

After threshold merge, load the artifact in `bridge-admin.html`. Import fills
review forms only and never submits a checkpoint or proof automatically.

## Stop Conditions

Stop the route for any of these conditions:

- finalized head below the deterministic confirmation boundary
- missing/skipped slot or invalid blockhash, parent slot, metadata, or signature
- provider disagreement, under-quorum result, or conflicting quorum views
- failed transaction presented as a bridge event
- event emitted outside the configured bridge-program runtime frame
- invalid event discriminator, version, layout, mint, recipient, or amount
- case-mutated Base58 identifier or changed global log position
- stale/mismatched previous checkpoint or gateway emergency pause

The route remains `setup_required` or `testing` until every applicable gate in
`docs/BRIDGE_PRODUCTION_GATE.md` has independent evidence.
