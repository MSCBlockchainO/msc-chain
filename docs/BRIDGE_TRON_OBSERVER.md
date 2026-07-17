# MSC Tron Bridge Observer

The Tron observer creates deterministic MSC checkpoint and event-proof
artifacts from blocks returned by independent Java-Tron Solidity APIs. It is
intended for TRC20 routes such as USDT after the source custody contract, token
identity, audit, and testnet gates are complete.

It does not deploy a Tron contract, move funds, submit a mint, or hold MSC mint
authority. The matching custody contract and separate TIP-712 release ceremony
are documented in `docs/BRIDGE_TRON_VAULT.md`.

## Solidified Evidence

For one explicit block height, every configured provider is queried through:

- `POST /walletsolidity/getnowblock`
- `POST /walletsolidity/getblockbynum`
- `POST /walletsolidity/gettransactioninfobyblocknum`

The `/walletsolidity` namespace serves solidified state. The observer requires
every Solidity head to cover `height + confirmations - 1`, checks that the block
ID embeds the requested height, and binds the block ID, parent hash, transaction trie
root, optional account state root, timestamp, complete transaction order,
transaction results, and complete log order into the provider-agreement
fingerprint.

The observer reconstructs each block transaction's canonical protobuf bytes
from `raw_data_hex`, signatures, and execution result, verifies that the
SHA-256 raw-data hash equals `txID`, and recomputes Java-Tron's complete
`txTrieRoot`. Every event proof then carries a compact
`msc-tron-transaction-proof-v1` branch that the MSC gateway verifies against
the signed checkpoint transaction root.

Official Java-Tron references:

- https://developers.tron.network/reference/gettransactioninfobyblocknum-1
- https://developers.tron.network/docs/event
- https://developers.tron.network/docs/block
- https://github.com/tronprotocol/java-tron/blob/e89c0d66520231b0b8abb2baee776d1d570e5fca/chainbase/src/main/java/org/tron/core/capsule/BlockCapsule.java#L218-L250
- https://github.com/tronprotocol/java-tron/blob/e89c0d66520231b0b8abb2baee776d1d570e5fca/chainbase/src/main/java/org/tron/core/capsule/utils/MerkleTree.java

The v2 checkpoint carries Tron's `block_header.raw_data.txTrieRoot` as
`transaction_root`. The compatibility `receipt_root` slot carries the same
commitment for generic gateway checks. When `accountStateRoot` is present it is
carried separately as `state_root`.

## Event Decoding

The custody contract must emit the same frozen event ABI as the EVM vault:

```solidity
event Locked(address indexed token, address indexed sender, bytes recipient, uint256 amount);
event Unlocked(bytes32 indexed withdrawalId, address indexed token, address indexed recipient, uint256 amount);
```

The indexed `withdrawalId` is normalized to canonical lowercase `0x` hex and
is part of the signed v5 event payload. Gateway completion requires an exact
match with the ID derived from the authorized MSC burn; recipient and amount
matching alone is insufficient.

Tron event-log contract and indexed addresses are 20-byte TVM hex values without
the `41` network prefix. The observer reconstructs and checksum-validates the
canonical Base58Check address before comparing it with configuration.

Because TransactionInfo logs do not expose an EVM-style global `logIndex`, MSC
defines the Tron bridge log index as the zero-based position of the log in the
complete solidified block: transaction order first, then each transaction's log
order. Unrelated and failed-transaction logs still consume an index. Logs from a
failed transaction are never accepted as bridge events.

Base58 values are case-sensitive and are committed exactly in event payloads.

## Trust Boundary

This is a federated source observer, not a chain-native Tron light client. The
threshold bridge-validator committee attests that independent Solidity nodes
agreed on the solidified block and complete event view. The artifact proves
transaction inclusion against the checkpoint `txTrieRoot`, but Java-Tron
execution logs are still committee-attested rather than proven directly
against SR consensus.
The confirmation count is a deterministic gateway compatibility boundary; the
queried source block itself is already from the solidified API namespace.

Production requires independently operated Java-Tron Solidity nodes or
independent providers. Put provider API authentication behind an operator-owned
HTTPS proxy; do not store provider secrets in observer JSON or artifact files.

## Build And Configure

```powershell
go test ./bridgeobserver ./ops/bridge_observer -count=1
go build -trimpath -o bin\bridge-observer.exe .\ops\bridge_observer
```

Before a release rehearsal, also recompute a live finalized mainnet block's
complete Java-Tron transaction commitment:

```powershell
$env:MSC_TRON_MAINNET_LIVE_TEST = "1"
go test ./bridgeobserver -run TestTronMainnetLiveTransactionCommitment -count=1
Remove-Item Env:MSC_TRON_MAINNET_LIVE_TEST
```

Copy `deploy/bridge/tron-observer.example.json` to a protected operator
directory. It is pinned to the canonical TRON mainnet genesis and official
Tether TRC20 USDT address. Replace only the bridge contract and independent API
placeholders; changing network, token, decimals, ABI, or lowering the 64-block
confirmation policy is rejected.

## Observe And Sign

Use the exact solidified block containing the custody event:

```powershell
bin\bridge-observer.exe observe-tron `
  --config C:\msc-bridge\tron-observer.json `
  --height 61815425 `
  --previous-checkpoint bcp_<previous-id> `
  --output C:\msc-bridge\tron-unsigned-61815425.json
```

Each operator independently produces and reviews the unsigned artifact. The
existing `sign`, `merge`, and `verify` commands are chain-aware and work for
both EVM and Tron artifacts. Production keys remain offline or in HSM/KMS and
must be distinct from DTL mint-authority keys.

After threshold merge, load the artifact in `bridge-admin.html`. The portal
fills the checkpoint and first proof for review but submits nothing
automatically. Register the checkpoint before attaching its proof to a deposit
intent.

## Stop Conditions

Stop the route on any of these conditions:

- Solidity head below the requested height
- block ID/height, parent, transaction root, or timestamp mismatch
- incomplete, duplicate, or foreign transaction-info set
- failed transaction presented as a successful event
- invalid Base58Check contract, token, or recipient address
- wrong event topic, token binding, MSC recipient, amount, or log order
- provider disagreement, under-quorum result, or two conflicting quorum views
- stale/mismatched previous checkpoint or gateway emergency pause

The route remains `testing` until the full checklist in
`docs/BRIDGE_PRODUCTION_GATE.md` is independently evidenced.
