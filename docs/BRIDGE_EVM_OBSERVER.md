# MSC EVM Bridge Observer

The EVM observer creates deterministic MSC bridge artifacts from finalized
Ethereum-compatible blocks. The same adapter can be used for BNB Chain,
Ethereum, Polygon, Base, Arbitrum, and Optimism after each route passes its own
contract, finality, audit, and testnet gates.

It does not activate a route or hold mint authority.

## Security Boundary

For one requested block height, the observer:

1. Queries at least two independently operated JSON-RPC providers.
2. Requires quorum agreement on block hash, parent hash, state root, receipt
   root, and the complete filtered bridge log set.
3. Rejects multiple conflicting views even when each view independently reaches
   the configured quorum.
4. Filters logs by exact block hash, bridge contract, and event topic.
5. ABI-decodes the source token, MSC recipient, destination recipient, and raw
   token amount.
6. Builds the canonical `BridgeID`, event payload, event Merkle root, proof, and
   `msc-bridge-checkpoint-v2` checkpoint.
7. Redacts RPC URLs from the output and stores only endpoint hashes.

Checkpoint height, observed confirmation boundary, block roots, and source block
timestamp are deterministic, so independent observers produce the same signing
payload.

This is a federated source observer. It does not yet verify an EVM receipt MPT
against a chain-native consensus light client. The bridge validator committee
attests to the RPC-agreed header and event root. Keep production limits low until
receipt proofs and chain-native finality verification pass an external audit.

## Required Contract Events

The matching fail-closed custody contract and deployment ceremony are described
in `docs/BRIDGE_EVM_VAULT.md`.

The configured source contract must emit exactly these ABI event signatures:

```solidity
event Locked(
    address indexed token,
    address indexed sender,
    bytes recipient,
    uint256 amount
);

event Unlocked(
    bytes32 indexed withdrawalId,
    address indexed token,
    address indexed recipient,
    uint256 amount
);
```

For `Locked`, `recipient` must contain the ASCII bytes of a canonical MSC address
(`MSC` followed by 42 hexadecimal characters). `amount` is the source token's
raw integer amount and is converted with `asset_decimals`.

Changing the event name, parameter order, or parameter types changes the topic
and must be treated as a new audited bridge protocol version.

For `Unlocked`, the observer preserves `withdrawalId` as canonical lowercase
`0x` hex. Bridge protocol v4 signs and Merkle-commits it, and gateway completion
requires it to equal the withdrawal ID derived from the exact MSC burn. An
unlock event with the same token, recipient, and amount but a different ID
cannot complete the transfer.

## Build

```powershell
go build -trimpath -o bin\bridge-observer.exe .\ops\bridge_observer
```

Copy `deploy/bridge/evm-observer.example.json` to a protected operator
configuration location. Replace every placeholder with independently verified
route values. Do not treat the example zero addresses as deployable values.

Use RPC providers with different operators and infrastructure. Three URLs with
`rpc_quorum: 2` tolerate one unavailable provider but deliberately stop on a
two-versus-two split.

## Observation Ceremony

Every bridge source validator independently observes the same finalized event
block. Use the exact source block height containing the bridge event:

```powershell
bin\bridge-observer.exe observe `
  --config C:\msc-bridge\bnb-observer.json `
  --height 42105778 `
  --previous-checkpoint bcp_<previous-id> `
  --output C:\msc-bridge\unsigned-42105778.json
```

The observer refuses to overwrite an existing artifact. Retain each file as
audit evidence.

Compare the checkpoint ID and RPC fingerprint produced by independent observer
operators. A mismatch must stop the ceremony.

## Offline Signing

`keygen-testnet` creates a plaintext software key and must only be used for an
isolated testnet:

```powershell
bin\bridge-observer.exe keygen-testnet `
  --validator-id bridge-validator-a `
  --private-key-output C:\msc-bridge-offline\validator-a.key
```

Production signing requires an HSM/KMS adapter and an offline approval process.
Do not copy production bridge-validator keys onto RPC observer hosts.

On an offline signing host, validate and sign the independently reviewed
artifact:

```powershell
bin\bridge-observer.exe sign `
  --input C:\msc-bridge-offline\unsigned-42105778.json `
  --private-key C:\msc-bridge-offline\validator-a.key `
  --signer bridge-validator-a `
  --output C:\msc-bridge-offline\signed-a-42105778.json
```

The signer validates the canonical BridgeID, payload hash, Merkle path,
checkpoint/header roots, and all existing signatures before adding its own.

## Threshold Merge

After collecting independently signed copies:

```powershell
bin\bridge-observer.exe merge `
  --input C:\msc-bridge\signed-a-42105778.json `
  --input C:\msc-bridge\signed-b-42105778.json `
  --input C:\msc-bridge\signed-c-42105778.json `
  --required-signers 3 `
  --output C:\msc-bridge\merged-42105778.json

bin\bridge-observer.exe verify `
  --input C:\msc-bridge\merged-42105778.json `
  --required-signers 3
```

Merge rejects different checkpoints, different proof sets, invalid signatures,
duplicate signer keys, tampered event fields, and under-threshold output.

## Gateway Registration

Open `bridge-admin.html`, connect to the protected node RPC, and use **Register
Finality Checkpoint**. The admin page can load the merged observer artifact and
extract its checkpoint and first event proof for review. Submit the checkpoint
before recording its event proof.

Each deposit proof must be paired with the correct deposit intent ID. Never map
an event to an intent using recipient text alone; verify route, contract, asset,
amount, source transaction, and log index.

## Failure Rules

Stop and investigate when any of these occurs:

- RPC quorum or split-view error
- no expected bridge event at the claimed block
- wrong token, contract, topic, recipient, amount, block hash, or log index
- observer checkpoint ID disagreement
- previous checkpoint mismatch
- checkpoint older than the node freshness window
- under-quorum or unknown signer rejection
- bridge emergency pause or state-root persistence failure

The complete activation checklist is `docs/BRIDGE_PRODUCTION_GATE.md`.
