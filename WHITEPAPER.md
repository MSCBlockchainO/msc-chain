# MSC Chain Whitepaper

Status: Technical draft  
Network: MSC Mainnet  
Chain ID: `91938`  
Coin: `MSC`  
Decimals: `18`  
Policy version: `mainnet-economic-v1`

## Abstract

MSC Chain is a Go-based blockchain network built around validator consensus, deterministic execution, finality certificates, native token workflows, fast snapshot synchronization, and hardened public gateway operation. The chain is designed to support a practical mainnet architecture: private validator RPC, public full-node gateway access, fixed-supply economics, stake-backed validator participation, protocol-native token logic through DTL, and operational tooling for chaos testing, monitoring, backup, and recovery.

The central design choice of MSC Chain is to keep consensus-critical behavior deterministic and bounded. Token logic is implemented as a protocol-native Decentralized Token Ledger (DTL), rather than relying on arbitrary smart-contract execution in the default mainnet runtime. Validators finalize blocks through strict quorum evidence, and finalized state is anchored with verifiable finality certificates and snapshot checkpoints.

## 1. Vision

MSC Chain is intended to be a production-ready base layer for simple value transfer, validator-secured settlement, native token issuance, DeFi/GameFi primitives, wallet-first user flows, and explorer-visible transparency.

The project prioritizes:

- deterministic consensus and state execution;
- strict validator identity and key lifecycle controls;
- fixed-supply tokenomics with transparent emission policy;
- native token logic that avoids common arbitrary-contract risk;
- fast sync and recovery through trusted snapshots and finality anchors;
- public access through hardened full-node gateways;
- operational evidence through unit tests, replay tests, monitoring, and chaos-network scripts.

## 2. Chain Identity

MSC Mainnet is identified by chain ID `91938`. The frozen genesis artifact is `genesis.json`, with expected SHA256:

```text
d6d7d96ea1a70d2aca31389ce7ef7953794ce77b4c933828295269702768fa3c
```

The native asset is `MSC`, described in the codebase as `Mythical system coin`, with 18 decimals and a fixed total supply cap of `9,193,823,602 MSC`.

Mainnet genesis is locked. Production nodes are expected to verify the genesis hash before startup. Mutating `genesis.json` after launch creates a different chain.

## 3. System Architecture

MSC Chain is composed of five primary layers:

1. Consensus layer: validator proposal, execution vote, quorum tracking, final block construction, finality certificate generation, and committed-hash safety checks.
2. Execution layer: deterministic transaction processing, ledger mutation, state root calculation, receipts, fees, staking, validator updates, and DTL operations.
3. Networking layer: libp2p peer connectivity, peer hello validation, block and transaction propagation, block range sync, snapshot transfer, peer reputation, and rate limiting.
4. Storage and sync layer: Pebble-backed state, block, snapshot, tx, and metadata stores, plus checkpointed snapshots and delta replay.
5. Application layer: RPC APIs, explorer, wallet UI, DTL IDE, metrics, public gateway deployment scripts, and operator tools.

The chain separates validator operation from public access. Validators should keep RPC private, while public users connect through full-node gateways, explorer endpoints, and wallet interfaces.

## 4. Consensus And Finality

MSC Chain uses a validator committee model with deterministic validator set resolution. Validators propose blocks, independently execute the proposed state transition, sign execution results, and finalize blocks once quorum evidence is available.

The strict execution supermajority is:

```text
required = floor((2 * validator_count) / 3) + 1
```

This gives the familiar Byzantine-fault-tolerant threshold where a minority of faulty validators cannot finalize conflicting state under honest quorum assumptions.

Each finalized block carries quorum metadata, including:

- consensus mode;
- quorum policy version;
- active ready validator count;
- required quorum;
- strict quorum;
- execution result signatures;
- validator set commitments;
- state root;
- mempool root;
- finality root.

Finality is not only a height marker. The chain builds finality artifacts:

- `FinalizedEpochCertificate`;
- epoch anchor hash;
- previous epoch anchor hash;
- finality root;
- validator commitment record;
- irreversible root record.

These artifacts bind finalized state to validator set identity, quorum policy, execution evidence, and the previous finality anchor. This makes finalized history auditable and gives snapshot sync a trust base.

## 5. Block Safety Model

The implementation defends against several classes of consensus failure:

- forked finalized blocks are rejected through committed-hash checks;
- unknown commit signers are rejected;
- fake proposer signatures are rejected;
- invalid execution result roots do not count toward quorum;
- delayed old votes are replay-fenced after finality;
- unsigned propagated quorum claims do not satisfy final execution quorum;
- conflicting quorum propagation does not poison later valid quorum;
- quorum metadata below mainnet floors is rejected.

Blocks commit state roots, mempool roots, receipt roots, validator set hashes, next-validator-set hashes, and finality metadata. The safety goal is:

```text
same input block + same pre-state => same post-state + same receipts
```

### 5.1 Formal Verification And Runtime Proof Obligations

MSC Chain separates formal-verification claims into two categories:

- machine-checked implementation invariants that are enforced by Go code, tests, RPC, and Prometheus;
- independent mathematical models, such as TLA+, Coq, or Isabelle proofs, which remain a launch-gate item until externally produced and reviewed.

The current implementation exposes a formal-verification audit surface:

```text
GET /formal/verification
GET /v1/formal/verification
```

The response reports:

- runtime invariant health;
- checked invariant count;
- failed invariant count;
- current height and finalized height;
- active validator count;
- required quorum;
- strict quorum floor;
- consensus detector mode;
- explicit assumptions;
- safety and liveness proof obligations;
- external proof status.

Core runtime invariants include:

- finalized height must never exceed local height;
- required finality quorum must be at least `floor(2n/3)+1`;
- active validator count must not exceed the known validator set;
- finality timeout must be classified as `HALTED` or stronger;
- partition risk must be classified as `PARTITION` or stronger;
- attack signals must not be active during healthy operation.

Formal proof obligations tracked by the node include:

| Obligation | Type | Current status |
| --- | --- | --- |
| No two finalized hashes at the same height | Safety | Machine-checked in runtime and tests |
| Strict finality quorum | Safety | Machine-checked in runtime and tests |
| Irreversible finalized roots | Safety | Machine-checked in finality artifact tests |
| Eventual finality under quorum and partial synchrony | Liveness | Runtime-observed, not theorem-proven |
| Independent TLA+/Coq/Isabelle model | Formal model | Pending external review |

Prometheus exposes:

```text
msc_formal_verification_healthy
msc_formal_invariants_checked
msc_formal_invariants_failed
msc_formal_external_model_checked
```

Mainnet rule: MSC Chain must not claim complete formal verification until an independent consensus model proves safety and liveness assumptions outside the Go implementation. Until then, the formal-verification subsystem is an auditable runtime guardrail and launch-gate tracker.

## 6. Validator System

MSC Mainnet launches with four frozen core validators:

| Validator | Genesis stake | Lock epochs |
| --- | ---: | ---: |
| `A` | `100` | `19872000` |
| `B` | `100` | `19872000` |
| `C` | `100` | `19872000` |
| `D` | `100` | `19872000` |

Mainnet validator policy includes:

- minimum active validators: `4`;
- active set target: `50`;
- maximum active committee: `512`;
- minimum validator stake: `100 MSC`;
- stake cap: `5%` of fixed supply;
- deterministic and equal-chance validator selection enabled;
- adaptive committee mode;
- committee rotation every `32` blocks;
- strict onboarding activation;
- activation delay of `5` blocks;
- one wallet bound to one validator at a time;
- first non-core validator stake must include a consensus public key.

Validator stake persists across restart and rejoin until the wallet submits an unstake transaction. A rejoining validator does not need to stake again if the stake was never unstaked.

### Chain-Committed Promotion Window Freeze

When `hybrid_score_rotation` has more than 21 eligible validators, MSC can freeze the 15 performance-slot validators for a promotion window. This freeze must be chain-committed, not saved only in a local cache.

Simple Hinglish explanation:

If every node recalculates top validators from the same chain state, all nodes get the same answer. That is deterministic. The danger starts if one node stores `top15.cache` locally for 10 epochs while another node has a different cache or a fresh node has no cache at all. Same blockchain data could then produce a different active validator set, which is a consensus failure.

MSC solves this with `PromotionWindowRecord`:

```text
PromotionWindow: 42
StartHeight: 4,200,000
EndHeight: 4,299,999
PerformanceValidators:
- valA
- valB
- valC
...
SelectionHash: ...
ScoreConfigHash: ...
SourceValidatorRegistryHash: ...
```

The record is consensus state. Its hash is committed through `promotion_window_hash` in blocks and snapshots after `promotion_window_record_v1_height`. Future nodes replaying from genesis or restoring a trusted snapshot can reconstruct or verify the same frozen top-15 set. Local caches may mirror this record for speed, but the cache is never the source of truth.

Hard exceptions such as jailed, exited, double-sign, or confirmed offline replacement are recorded through `PromotionWindowReplacementRecord`. Mid-window score changes do not modify the frozen top-15; promotion and demotion happen at the next promotion boundary unless a chain-committed hard replacement is required.

## 7. Validator Key Lifecycle

Validator identity is rooted in the node's validator secret key. MSC Chain treats validator key lifecycle as consensus-adjacent infrastructure, not an operator convenience.

The operator policy is:

- `validator.sec` is the identity root;
- HSM/external-signer mode can replace `validator.sec` for validator consensus signing;
- MPC/threshold-signing mode can split validator signing authority across multiple signer participants;
- fingerprint mismatch fails startup;
- backup restore is preferred over chain reset;
- key backup is required;
- secure backups are stored under `secure-backups`;
- identity rotation on an existing chain is disabled by default;
- core validator startup can require prompt-only or file/prompt password flow.

Core validator onboarding uses a signed registry and a two-phase flow:

1. Add the validator as `pending`.
2. Promote the validator to `active` after signed approval and effective-height buffer.

This avoids chain reset, identity drift, and accidental proposer inclusion before the node is ready.

### Hardware Security Module Support

MSC supports HSM-backed validator signing through an external signer interface. In this mode the node loads only the validator consensus public key and sends canonical Ed25519 signing payloads to an operator-controlled signer command. The signer command can be backed by an HSM, YubiHSM, Ledger Enterprise, PKCS#11 bridge, or cloud/KMS adapter, but the MSC node never imports the validator private key into process memory.

The HSM path is fail-closed:

- `validators.hsm_enabled=true` disables silent software-key fallback;
- `validators.hsm_public_key` pins the expected validator consensus public key;
- `validators.hsm_external_signer_command` must return a valid 64-byte Ed25519 signature;
- every returned signature is verified against the pinned public key before it is used;
- fingerprint locks and `validators.required_key_fingerprint` still apply;
- `/status` and `/metrics` expose HSM readiness, provider, key ID, signer configuration, and failure reason.

This allows large validators to run MSC consensus keys behind dedicated signing hardware while still preserving deterministic consensus signing and operator observability.

### MPC Validator Signing

MSC also supports MPC-backed validator signing through a threshold signer adapter. In this mode the node stores only the validator consensus public key. It sends canonical Ed25519 signing payloads to an external MPC signer command, and that command coordinates threshold participants to return one final 64-byte Ed25519 signature for the pinned validator public key.

The MPC path is fail-closed:

- `validators.mpc_enabled=true` disables software private-key fallback;
- `validators.mpc_public_key` pins the expected validator consensus public key;
- `validators.mpc_external_signer_command` must return a valid 64-byte Ed25519 signature;
- optional `validators.mpc_threshold` and `validators.mpc_participants` document and expose the threshold policy;
- every returned signature is verified against the pinned public key before use;
- a timeout, bad signature, wrong key, missing signer command, or invalid threshold makes the validator signer unavailable;
- `/status` and `/metrics` expose MPC readiness, provider, key ID, threshold, participant count, and failure reason.

MPC mode is intended for large validators that do not want one `validator.sec` compromise to compromise the whole validator. The MSC node does not implement the threshold cryptography internally; it defines the deterministic payload, fail-closed signer boundary, verification, configuration, and observability contract for external MPC systems.

### Recommended Mainnet Signing Policy

MSC launch policy is:

1. Use local `validator.sec` only for bootstrap, development, and emergency break-glass operation.
2. Move production validators to HSM/external-signer mode first, one validator at a time, after signer rehearsal.
3. Keep MPC/threshold signing disabled on core validators until the threshold signer cluster has passed long-running testnet and EC2 rehearsal.
4. For future large validators, prefer MPC threshold signing once participant monitoring, failover, and signer quorum recovery are proven.

The effective signer is observable through `/status` and Prometheus:

- `validator_signer_mode`: `software`, `hsm`, `mpc`, `public_key_only`, or `none`;
- `validator_signer_ready`;
- `validator_signer_provider`;
- `validator_signer_reason`;
- `msc_validator_signer_mode_code`;
- `msc_validator_signer_ready`.

If both HSM and MPC are configured, MPC is the effective signer. MSC does not silently fall back to software signing when HSM or MPC mode is enabled. This is intentional: signer infrastructure failure should make the validator unavailable rather than secretly downgrading to a hot key.

## 8. Slashing And Liveness

MSC Chain applies economic penalties for severe validator faults and inactivity.

Severe faults include:

- double proposal or double signing;
- invalid block or invalid proposer behavior;
- bad execution;
- execution equivocation;
- systematic censorship;
- finality-breaking behavior.

The configured severe slash burns `1000` basis points, or `10%`, from validator stake. After `3` severe slashes, the validator exits permanently.

Inactivity penalties are tiered:

- tier 1: half of the configured inactivity burn rate;
- tier 2: full configured inactivity burn rate;
- tier 3 and beyond: double, capped at `10000` basis points.

Jailed or exited validators do not receive normal rewards. Blocked validator rewards are routed to treasury and burned according to policy.

## 9. Native Asset And Tokenomics

The native token is `MSC`.

| Field | Value |
| --- | --- |
| Symbol | `MSC` |
| Decimals | `18` |
| Fixed supply cap | `9,193,823,602` |
| Policy version | `mainnet-economic-v1` |
| Chain ID | `91938` |

The explicit frozen genesis balance total is `5,977,385,341 MSC`, allocated across foundation, treasury, validator wallets, and user reward pool.

Mainnet genesis pools include:

| Pool | Address | Notes |
| --- | --- | --- |
| Foundation | `MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d` | Locked foundation allocation |
| Treasury | `MSC01102bdf87789381354be6ec8af1f49688306ea83c` | Governance-only, locked |
| User rewards | `USER_REW` | Genesis user reward allocation |

Transaction fees route to `MSC_TREASURY`.

Mainnet emission policy is deterministic and bounded:

- emission enabled;
- per-block emission reward range: `2` to `4`;
- halving interval: `1,105,840` blocks;
- treasury share: `2000` bps, or `20%`;
- validator/proposer share: `7200` bps, or `72%`;
- burn share: `800` bps, or `8%`;
- configured burn floor: `62,000,000`;
- random user reward chance: `2500` bps, or `25%`;
- work block base reward: `2`.

The fixed supply cap is enforced by policy. Scheduled emission cannot exceed the fixed total supply.

## 10. Staking

MSC Chain uses locked validator stake as part of validator eligibility and penalty enforcement.

Mainnet staking rules:

- minimum validator stake: `100 MSC`;
- default lock: `19,872,000` epochs;
- minimum unstake period: `23` months;
- one wallet can bind to one validator at a time;
- validator stake survives restarts and rejoins;
- below-minimum stake transactions are rejected for validator positions;
- first non-core stake must anchor a consensus public key.

The stake lock is designed to make validator commitment long-lived and to give slashing meaningful weight.

## 11. DTL: Decentralized Token Ledger

MSC Chain includes DTL, a protocol-native token system for MSC. DTL is designed around a simple principle:

```text
No single key, no single node, and no single developer can unilaterally control a token.
```

DTL token rules are enforced by consensus, not by arbitrary user-deployed runtime code. In the current mainnet-oriented configuration, EVM execution is disabled and DTL is the native token workflow.

DTL supports:

- token creation;
- token transfer;
- governance-authorized mint;
- burn;
- AMM pool creation;
- add and remove liquidity;
- swaps;
- commit-reveal duels;
- lending markets;
- collateral deposit;
- borrow, repay, withdraw, and liquidate;
- tournaments;
- deterministic contract-style deploy/call operations.

DTL governance uses a Governance Certificate, or GCERT. A GCERT binds:

- token ID;
- epoch;
- action;
- payload hash;
- unique signer set;
- signer public keys;
- signatures;
- threshold.

Minting, pause/unpause, freeze/unfreeze, and authority rotation require valid threshold governance. This reduces the risk of hidden mint functions, proxy upgrade abuse, re-entrancy, and other common arbitrary-contract failure modes.

## 12. Deterministic User Logic

The codebase also defines a proposed DTL Logic Pack model for future user-authored logic without enabling arbitrary VM bytecode.

The model is:

1. User writes `dtl-script-v1`.
2. Compiler emits canonical `logic_pack` JSON.
3. Nodes statically validate the logic pack at deploy time.
4. Calls execute bounded deterministic operations.
5. All validators derive identical post-state.

The proposed model disallows loops, recursion, floating point, wall-clock reads, network IO, and filesystem IO. It uses checked arithmetic, forward-only control flow, method step limits, read/write limits, and deterministic token transfer actions.

## 13. Networking

MSC Chain uses libp2p networking for peer connectivity and message propagation. The node includes:

- peer hello validation;
- chain ID and genesis hash checks;
- persistent peer support;
- mDNS and DHT configuration;
- peer diversity limits;
- peer reputation and quarantine;
- stream-based block sync;
- transaction gossip;
- snapshot metadata and chunk gossip;
- ping/pong keepalive;
- block range serving with bounded request policy;
- peer rate limits and resource limits.

The default local development ports use P2P ports beginning at `7001` and RPC ports beginning at `26657`.

## 14. Validator Geographic Diversity

MSC Mainnet treats validator placement diversity as a high-priority launch gate. Peer subnet diversity protects P2P connectivity, but validator safety also needs diversity across countries, ASNs, cloud providers, and regions. If all validators run on AWS in one region, the validator set is exposed to correlated outage, routing, account, and provider-risk failures.

Validator diversity rules track:

- country buckets;
- ASN/operator buckets;
- cloud/provider buckets;
- region buckets;
- missing validator metadata;
- maximum concentration percentage per country, ASN, and cloud.

Default mainnet policy:

| Rule | Target |
| --- | ---: |
| Minimum countries | `2` |
| Minimum ASNs | `3` |
| Minimum cloud/providers | `2` |
| Maximum one-country concentration | `67%` |
| Maximum one-ASN concentration | `50%` |
| Maximum one-cloud concentration | `67%` |

The rule is advisory by default through `diversity_mode = "warn"` so it cannot accidentally halt an existing validator set. Mainnet launch should not proceed until the diversity report is healthy. A future governance upgrade may switch the policy to `diversity_mode = "enforce"` after testnet rehearsal.

Validator metadata is configured locally:

```toml
[validators]
diversity_enabled = true
diversity_mode = "warn"
diversity_min_countries = 2
diversity_min_asns = 3
diversity_min_clouds = 2
diversity_max_country_pct = 67
diversity_max_asn_pct = 50
diversity_max_cloud_pct = 67
diversity_metadata = [
  "A|US|AS14618|AWS|us-east-1",
  "B|DE|AS24940|HETZNER|fsn1",
  "C|SG|AS14061|DIGITALOCEAN|sgp1",
  "D|US|AS8075|AZURE|eastus",
]
```

The same metadata can be supplied through `MSC_VALIDATOR_DIVERSITY_MAP` using semicolon-separated entries. The public full-node API exposes:

```text
GET /v1/validators/diversity
GET /validators/diversity
```

Prometheus metrics include `msc_validator_country_buckets`, `msc_validator_asn_buckets`, `msc_validator_cloud_buckets`, `msc_validator_region_buckets`, `msc_validator_diversity_missing_metadata`, `msc_validator_diversity_violations`, `msc_validator_diversity_healthy`, `msc_validator_diversity_max_country_pct`, `msc_validator_diversity_max_asn_pct`, and `msc_validator_diversity_max_cloud_pct`.

If all validators run on AWS in the same region, the diversity report must become unhealthy and alerts should block mainnet launch.

## 15. Sync, Snapshots, And Recovery

MSC Chain is designed for nodes to catch up through a combination of block replay and trusted snapshots.

Mainnet sync settings include:

- direct gossip max blocks: `128`;
- fast block sync max blocks: `256`;
- range fetch max blocks: `50000`;
- snapshot threshold: `2000` blocks;
- checkpoint interval: `32` blocks;
- snapshot chunk size: `1 MiB`;
- parallel snapshot chunks: `8`;
- delta replay batch: `1024` blocks;
- snapshot anchor timeout: `10` seconds;
- invalid proof quarantine threshold: `3`.

Snapshots include ledger state, validator state, DTL state, validator registry hashes, finalized height/hash, epoch anchor hash, finality root, and checkpoint proof. Snapshot application is guarded by finality and local anchor checks to prevent replay loops, stale snapshot regressions, and invalid state adoption.

Storage is split into separate Pebble stores:

- `state.db`;
- `blocks.db`;
- `snapshot.db`;
- `tx.db`;
- `meta.db`;
- block files;
- snapshot files;
- checkpoints;
- cold-storage archive.

Database writes use synced Pebble batches, and the storage layer can quarantine corrupted DB paths during recovery.

### 14.1 State Pruning And Archive Policy

MSC Chain treats state pruning as a mainnet safety requirement. Validators are not expected to retain unlimited historical state. Archive retention is separated into archive nodes so validator disk growth stays bounded even after very large block counts.

Storage profile is configured through `[storage]`:

```toml
history_profile = "auto" # auto | validator | full | archive
state_pruning_enabled = true
validator_retained_epochs = 10
validator_rollback_window_blocks = 256
validator_snapshot_keep_last = 3
validator_recent_block_window = 2048
full_node_history_blocks = 5256000
cold_export_enabled = true
cold_export_compression = "zstd"
parallel_gc_workers = 4
```

Profile rules:

- `validator`: keep current/finalized state, last validator epochs, rollback window, recent hot blocks, compacted snapshots, finality artifacts, and cold block exports.
- `full`: keep extended recent block history for public RPC/explorer use, but do not promise complete historical state.
- `archive`: retain full hot history and skip pruning. Use this only for dedicated archive nodes with large storage.
- `auto`: validators use `validator`, full nodes use `full`, and `sync.history_mode = "archive_full"` forces `archive`.

The storage manager runs after finalized epochs and may:

- persist a state checkpoint;
- compact old snapshot metadata and deltas;
- prune old execution snapshot cache;
- prune old hot block files;
- export old block files to `cold-storage/blocks` using `zstd`;
- write a durable `state_prune_marker` in `meta.db`.

The prune marker records:

- finalized height;
- retain-from height;
- pruned-through height;
- hot window size;
- snapshot retention;
- block prune boundary;
- cold export boundary;
- latest checkpoint height.

Operators can inspect pruning policy:

```bash
curl -s http://127.0.0.1:26657/storage/policy | jq
curl -s http://127.0.0.1:26657/v1/storage/policy | jq
```

Important Prometheus metrics:

```text
msc_storage_pruning_enabled
msc_storage_archive_mode
msc_storage_retain_from_height
msc_storage_hot_window_blocks
msc_storage_pruned_states_total
msc_storage_pruned_snapshots_total
msc_storage_gc_cycles_total
msc_storage_gc_duration_seconds
msc_cold_exports_total
msc_cold_storage_size_bytes
```

Mainnet rule: validator nodes should not run as archive nodes. Archive service should be separated into dedicated full-history infrastructure.

## 16. Light Client Protocol

MSC Chain should support trust-minimized wallets and mobile clients that do not need to trust a public RPC server for balances, transactions, or state claims. Public RPC remains useful for availability, but a wallet should be able to verify the data it receives.

The light client protocol has three parts:

1. Light client header chain.
2. SPV and Merkle proof APIs.
3. Stateless verification rules for wallets and mobile clients.

### 16.1 Light Client Header Chain

A light client stores only compact block headers and finality artifacts, not the full state database. Each header must include:

- height;
- block hash;
- parent hash;
- state root;
- transaction root;
- receipt root;
- validator set hash;
- next validator set hash;
- finality root;
- epoch anchor hash;
- finalized epoch certificate hash;
- proposer identity;
- quorum signature evidence hash.

The mobile or browser wallet verifies:

```text
parent_hash(header N) == hash(header N-1)
header.finality_root matches finalized certificate
validator_set_hash matches finalized validator commitment
epoch_anchor links to previous epoch anchor
```

This lets a light client follow the canonical finalized chain without downloading every block body.

### 16.2 SPV Proofs

MSC Chain should expose proof APIs for wallet-critical data:

```text
GET /proof/account?address=MSC...
GET /proof/balance?address=MSC...&coin=MSC
GET /proof/tx?id=...
GET /proof/receipt?id=...
GET /proof/validator?id=A
GET /light/headers?from=...&limit=...
GET /light/checkpoint/latest
```

Each proof response should include:

- target key and value;
- proof height;
- state root or transaction root;
- Merkle branch;
- block header;
- finalized epoch certificate;
- validator set commitment;
- epoch anchor hash.

The verifier accepts the proof only if:

```text
MerkleBranch(value) -> expected_root
expected_root == header.state_root OR tx_root OR receipt_root
header is connected to trusted finalized checkpoint
FEC has strict quorum signatures
validator commitment matches the FEC
```

For transactions, an SPV proof proves inclusion in the transaction root. For receipts, it proves execution result inclusion. For balances and account state, it proves inclusion in the state root.

### 16.3 Stateless Wallet Verification

The MSC wallet should support two modes:

| Mode | Behavior |
| --- | --- |
| RPC mode | Trusts full-node RPC response for fast UX |
| Light mode | Verifies headers, finality certificate, and Merkle proof before displaying confirmed state |

In light mode, the wallet must not show a balance, transaction, staking status, or validator status as verified unless the corresponding proof validates against a trusted finalized checkpoint.

Recommended wallet labels:

```text
Verified by light client
RPC-only response
Proof missing
Proof invalid
```

This prevents a malicious public RPC gateway from lying to mobile wallets about balances, transaction inclusion, staking state, governance votes, or validator membership.

### 16.4 Mobile Verification

Mobile wallets should persist:

- latest trusted finalized checkpoint;
- last N light headers;
- last N epoch anchors;
- validator set commitments;
- finality certificate chain;
- proof verification cache.

Bootstrap flow:

1. Download latest finalized checkpoint.
2. Verify the finalized epoch certificate.
3. Verify validator set commitment.
4. Download compact headers from checkpoint to current height.
5. Verify header chain and epoch anchors.
6. Request balance, transaction, and staking proofs.
7. Display verified data only after proof validation.

The mobile wallet should be able to reject:

- fake balances;
- fake transaction confirmations;
- stale finalized checkpoints;
- rewritten validator sets;
- invalid epoch anchors;
- long-range attack headers.

### 16.5 Mainnet Requirement

Trustless mobile wallets require Merkle proofs and light-client headers before they can be called fully mainnet-ready. Until this protocol is implemented and tested, public wallets should clearly treat full-node RPC as an availability layer, not as a cryptographic proof source.

Mainnet recommended implementation order:

1. Add canonical account, balance, transaction, receipt, and validator Merkle proof generation.
2. Add `/light/headers` and `/light/checkpoint/latest` APIs.
3. Add browser/mobile proof verifier library.
4. Add wallet UI proof status.
5. Add corruption, stale proof, wrong-root, and wrong-validator-set tests.
6. Add mobile restore test from only checkpoint plus headers plus proofs.

## 17. Cross Chain Bridge Framework

MSC Chain includes a disabled-by-default cross-chain bridge framework for future IBC-style interoperability, external asset verification, and trust-minimized bridge proof handling. The framework is intentionally verification-first: it exposes bridge status and proof verification APIs, but it does not mint, unlock, or release external assets unless a separately audited governance activation enables an execution path.

The bridge framework has three layers:

1. External chain registry: explicit chain ID, name, chain type, trust model, confirmation floor, and optional light-client endpoint.
2. External asset registry: explicit denom, origin chain, origin asset, local denom, decimals, and escrow policy.
3. Bridge proof verifier: validates registered chain, registered asset, replay key, confirmation count, light-client/Merkle proof requirements, and oracle quorum syntax where configured.

Bridge configuration is in `config.toml`:

```toml
[bridge]
enabled = false
mode = "verification_only"
ibc_style_enabled = false
light_client_required = true
required_confirmations = 64
oracle_quorum = 3
chains = []
assets = []
```

Supported trust models:

| Trust model | Intended use |
| --- | --- |
| `light_client` | Preferred path using header chain plus Merkle/SPV proof |
| `oracle_quorum` | Transitional path requiring threshold bridge attestations |
| `hybrid` | Requires both light-client evidence and oracle quorum policy |

The mainnet safety rule is simple:

```text
No external asset mint or unlock without explicit chain registry,
asset registry, replay protection, confirmation floor, and verified proof.
```

Bridge proof APIs:

```text
GET  /bridge/status
POST /bridge/verify
GET  /v1/bridge/status
POST /v1/bridge/verify
```

The verifier returns `accepted=false` for disabled bridge mode, unregistered source chains, unregistered assets, insufficient confirmations, missing replay protection, and missing light-client proof when `light_client_required=true`.

Prometheus bridge metrics:

```text
msc_bridge_enabled
msc_bridge_ibc_style_enabled
msc_bridge_light_client_required
msc_bridge_registered_chains
msc_bridge_registered_assets
msc_bridge_oracle_quorum
```

Future IBC-style work should add client, connection, channel, packet, acknowledgement, timeout, and relayer state machines. External asset execution should remain disabled on mainnet until an independent bridge audit, replay tests, fork tests, oracle compromise tests, and governance launch vote are complete.

## 18. RPC, Explorer, And Wallet

MSC Chain exposes RPC endpoints for node status, block data, transaction submission, tokenomics, governance, explorer views, wallet interactions, DTL token data, and observability.

The repository includes:

- web explorer UI;
- MSC wallet UI;
- DTL IDE;
- token/logo/NFT image guidance;
- public gateway deployment scripts;
- TLS certificate generation tooling.

Production public access should terminate at a full-node gateway, not a validator RPC port. Validator RPC should remain private or bound to localhost. Public gateway deployments should use HTTPS, nginx rate limits, node RPC limits, and private metrics scraping.

## 19. Security Baseline

MSC Chain's security model combines consensus rules, node hardening, operator policy, and public gateway separation.

Security controls include:

- chain ID enforcement;
- frozen genesis hash verification;
- finality certificate verification;
- strict quorum metadata checks;
- validator signer validation;
- committed-hash fork rejection;
- transaction signature and nonce validation;
- transaction TTL and body-size limits;
- mempool caps;
- peer hello gating;
- peer reputation and quarantine;
- rate limits for RPC and gossip;
- optional RPC auth tokens;
- TLS support;
- DB encryption key configuration;
- validator key backup and fingerprint locking.

The recommended local security baseline is to set:

```powershell
$env:MSC_RPC_TOKEN = "replace-with-strong-random-token"
$env:MSC_DB_ENCRYPTION_KEY = "<base64-32-byte-key>"
```

For large validators, use HSM mode instead of a software `validator.sec` where possible. Example:

```toml
[validators]
hsm_enabled = true
hsm_provider = "yubihsm"
hsm_key_id = "slot-1"
hsm_public_key = "<32-byte-ed25519-public-key-hex>"
hsm_external_signer_command = "/opt/msc/bin/yubihsm-msc-signer --key slot-1"
hsm_timeout_ms = 3000
hsm_require_user_presence = true
```

For production, public validator RPC exposure should be avoided.

## 20. Governance

MSC Chain currently has four governance surfaces:

1. Core validator registry governance: signed registry updates add, activate, retire, or enforce core validator identity.
2. Validator update certificates: threshold-approved validator add/remove transactions prevent unilateral validator set mutation.
3. DTL governance certificates: token-specific threshold governance controls minting, pause/unpause, freeze/unfreeze, and authority rotation.
4. On-chain governance proposals: validator voting, treasury voting, protocol upgrade scheduling, rollback-protected activation heights, and bounded emergency pause records.

Treasury operations are disabled on mainnet unless explicitly enabled and authorized.

Protocol upgrades are versioned and activated by height. Rollbacks are rejected unless the proposal explicitly declares rollback approval and receives strict quorum. Emergency upgrades can use a shorter activation path, but still require strict quorum. Emergency pause proposals are bounded, observable through RPC and Prometheus, and intended for operator coordination during incident response.

## 21. Testing And Mainnet Readiness

The repository includes extensive tests for:

- validator onboarding;
- validator liveness;
- validator churn;
- adaptive committee behavior;
- finality certificates;
- block verification;
- Byzantine block rejection;
- coordinated Byzantine minority behavior;
- delayed vote replay;
- timing attacks;
- network partitions;
- peer security;
- RPC hardening;
- storage durability;
- snapshot verification;
- sync recovery;
- execution determinism;
- economic policy;
- tokenomics;
- DTL state transitions.

Mainnet chaos tooling tests survival under:

- randomized validator and full-node restarts;
- restart storms;
- local firewall disconnect windows;
- packet loss;
- latency;
- slow validators;
- stale snapshot restarts;
- transaction floods;
- fork checks across finalized hashes.

The Tier 1 survival pass condition is no finality stall beyond configured gap, reachable quorum above minimum, bounded finalized lag, bounded height lag, and no finalized block hash divergence across reachable nodes.

## 22. Limitations And Risk Statement

MSC Chain is an active implementation. The whitepaper describes the repository's current protocol direction and mainnet configuration, not a completed third-party security audit.

Known risks and limitations:

- the frozen genesis core set starts small, with four core validators;
- validator decentralization depends on successful signed onboarding and operator participation;
- DTL Logic Pack is proposed and should be activated only after testnet validation;
- public gateway security depends on correct deployment and DNS/TLS setup;
- validator key loss or poor backup hygiene can affect liveness;
- HSM adapters must be rehearsed per provider; a signer timeout or unavailable HSM makes that validator unable to vote until recovered;
- MPC signer clusters must be rehearsed as production infrastructure; an unavailable threshold quorum makes that validator unable to vote until signer quorum recovers;
- any governance threshold can be compromised if enough signing keys are compromised;
- trustless mobile wallet verification requires the light client proof APIs and verifier library described in Section 16;
- production readiness should include independent audit, long-duration distributed chaos runs, and public monitoring.

## 23. Roadmap

Near-term roadmap:

- enforce signed core registry after staged rollout;
- complete public full-node gateway hardening;
- run long-duration 10-node and 50-node chaos survival tests;
- publish reproducible genesis and operator verification steps;
- expand monitoring dashboards and alert thresholds;
- implement Merkle proof APIs and light-client header verification;
- harden wallet and explorer production deployment;
- complete DTL Logic Pack testnet activation path;
- prepare independent security review.

Medium-term roadmap:

- broader validator onboarding;
- stronger on-chain governance workflows;
- snapshot proof improvements;
- deeper wallet support for DTL assets;
- public token registry and metadata standards;
- richer DeFi/GameFi primitives under deterministic execution limits.

## 24. Operator Terminal Command Reference

This section is the production command reference for operators. Do not commit validator passwords, wallet passwords, private keys, SSH keys, or mnemonic phrases. Use environment variables, local secret files, or an interactive terminal.

### Build And Test

```powershell
go test . -run "TestOperator|TestProductionGenesis|TestConsensusMode|TestFormalVerification|TestGovernance|TestProtocolUpgrade|TestEmergency" -count=1
go build -o msc-node.exe .
.\msc-node.exe help
```

### Universal 1-Click Setup

Preferred home/operator setup uses the built-in dispatcher. The release package may expose the same binary as `msc` and `msc-node`.

```powershell
.\msc-node.exe setup validator --id HOME1 --low-ram --auto-start
.\msc-node.exe setup candidate --id HOME1 --low-ram --auto-start
.\msc-node.exe doctor --id HOME1 --role validator --datadir data --json
.\msc-node.exe backup wizard --id HOME1 --datadir data
.\msc-node.exe service status --install-dir "$env:LOCALAPPDATA\MSCNode"
```

Ubuntu/Linux:

```bash
./msc setup validator --id HOME1 --low-ram --auto-start
./msc doctor --id HOME1 --role validator --datadir data --json
./msc backup wizard --id HOME1 --datadir data
./msc service status --install-dir "$HOME/.msc-node"
```

Setup source modes:

```text
--source auto      signed release when configured, otherwise local build
--source local     build from the local repository
--source release   download release artifact, verify checksum, and verify Ed25519 signature when --release-public-key is supplied
```

### Production Public Installer Flow

The public installer flow is:

1. Download the MSC release artifact.
2. Verify the operator-supplied SHA-256 checksum.
3. Choose the node type: `full`, `public-rpc`, `candidate-validator`, `private-validator`, or `archive`.
4. Auto-write `config.toml` for the selected storage/RPC profile.
5. Auto-add bootnodes and `persistent_peers`.
6. Start the node service.
7. Print the local status and health URLs.

Linux:

```bash
./scripts/install_msc_node.sh \
  --node-type full \
  --id FULL1 \
  --release-url https://releases.mscchain.io/msc-node-linux-amd64.tgz \
  --release-sha256 <sha256> \
  --bootnodes "/ip4/BOOTNODE_IP/tcp/7001/p2p/PEER_ID" \
  --auto-start
```

Windows:

```powershell
.\scripts\install_msc_node.ps1 `
  -NodeType public-rpc `
  -NodeId RPC1 `
  -ReleaseUrl https://releases.mscchain.io/msc-node-windows-amd64.zip `
  -ReleaseSha256 <sha256> `
  -Bootnodes "/ip4/BOOTNODE_IP/tcp/7001/p2p/PEER_ID" `
  -AutoStart
```

Node type policy:

```text
full                 full-node runtime, pruned full-node storage
public-rpc           full-node runtime, public-gateway hints; expose only through TLS/rate-limited gateway
candidate-validator  full-node runtime until synced and staked
private-validator    validator runtime, validator storage profile, localhost/VPN RPC only
archive              full-node runtime, archive storage profile, pruning disabled
```

Validators should keep RPC bound to `127.0.0.1` or private networks. Public wallet and explorer traffic should terminate at full/public-RPC/archive gateway nodes, never directly at validator RPC.

### Idempotent Install, Repair, And Recovery

Rerunning setup is recovery-safe by default. If MSC finds an existing install manifest, validator key, database, config, or service, it preserves them and treats the run as a repair/update path. A fresh destructive install is refused unless the operator explicitly confirms the node ID and has already removed or quarantined old data.

```powershell
.\msc-node.exe install validator --id HOME1 --low-ram --auto-start
.\msc-node.exe repair --id HOME1 --install-dir "$env:LOCALAPPDATA\MSCNode"
.\msc-node.exe update --id HOME1 --source auto --install-dir "$env:LOCALAPPDATA\MSCNode"
.\msc-node.exe start --id HOME1 --install-dir "$env:LOCALAPPDATA\MSCNode"
.\msc-node.exe stop --id HOME1 --install-dir "$env:LOCALAPPDATA\MSCNode"
.\msc-node.exe status --id HOME1 --install-dir "$env:LOCALAPPDATA\MSCNode"
.\msc-node.exe uninstall --id HOME1 --install-dir "$env:LOCALAPPDATA\MSCNode"
```

Ubuntu/Linux:

```bash
./msc install validator --id HOME1 --low-ram --auto-start
./msc repair --id HOME1 --install-dir "$HOME/.msc-node"
./msc update --id HOME1 --source auto --install-dir "$HOME/.msc-node"
./msc start --id HOME1 --install-dir "$HOME/.msc-node"
./msc stop --id HOME1 --install-dir "$HOME/.msc-node"
./msc status --id HOME1 --install-dir "$HOME/.msc-node"
./msc uninstall --id HOME1 --install-dir "$HOME/.msc-node"
```

Each install directory contains `install_manifest.json` with the node ID, role, data directory, config path, binary path, genesis hash, validator public key or fingerprint when available, service name, OS, architecture, source, and update time. Lifecycle commands read this manifest before touching files.

Safe backup and restore:

```powershell
.\msc-node.exe backup --id HOME1 --datadir data --out D:\msc-backups
.\msc-node.exe restore --id HOME1 --backup D:\msc-backups\HOME1-20260604T120000Z --install-dir "$env:LOCALAPPDATA\MSCNode"
```

Safe uninstall stops or disables the service and leaves keys, database, wallet files, and backups in place. Data removal requires an explicit dangerous confirmation:

```powershell
.\msc-node.exe uninstall --id HOME1 --install-dir "$env:LOCALAPPDATA\MSCNode" --purge-data --confirm-delete-node-id HOME1
```

If a validator key is lost and there is no backup, the old validator identity cannot be recovered. The operator must create a new validator identity and go through validator registration again. Key replacement during restore requires `--replace-key --confirm-validator-pubkey <hex>` so accidental identity drift cannot happen silently.

Future remote bootstrap commands must be enabled only after signed release manifests are published:

```powershell
irm https://install.mscchain.io | iex
```

```bash
curl -fsSL https://install.mscchain.io | bash
```

### Local Node Startup

Use `--role=auto` for normal startup. A node whose validator key, stake, and wallet auth are present becomes validator-ready automatically; otherwise it remains a full node.

```powershell
$env:MSC_ALLOW_VALIDATOR_KEY_CREATE = "1"
$env:MSC_VALIDATOR_PASSWORD = "<local-validator-password>"
go run . --mode=full --role=auto --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657
```

Local four-validator layout:

```powershell
go run . --mode=full --role=auto --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657
go run . --mode=full --role=auto --id=B --port=7002 --datadir=data/B --rpcaddr 127.0.0.1:26658
go run . --mode=full --role=auto --id=C --port=7003 --datadir=data/C --rpcaddr 127.0.0.1:26659
go run . --mode=full --role=auto --id=D --port=7004 --datadir=data/D --rpcaddr 127.0.0.1:26660
```

### Operator CLI

Wallet commands:

```powershell
.\msc-node.exe wallet new --wallet .\.msc\secure_wallet.json --show-mnemonic
.\msc-node.exe wallet import --private-key <hex> --wallet .\.msc\secure_wallet.json
.\msc-node.exe wallet import --mnemonic "word1 word2 ..." --wallet .\.msc\secure_wallet.json
.\msc-node.exe wallet export-public --wallet .\.msc\secure_wallet.json
```

Validator key commands:

```powershell
.\msc-node.exe validator-keygen --id F --datadir data/F
.\msc-node.exe validator-pubkey --id F --datadir data/F
.\msc-node.exe validator create --wallet .\.msc\secure_wallet.json --validator F --validator-pubkey <32-byte-ed25519-pubkey-hex> --amount 100 --rpc https://wallet.mscblockexplorer.in
```

MPC validator key ceremony commands:

Use MPC when the validator private key must not live as one complete hot file on disk. The validator still has one consensus public key, but signing is performed by threshold shares.

### New MPC Validator Key

Use this only for a new validator whose consensus public key is not already registered on-chain:

```powershell
$env:MSC_MPC_SHARE_PASSWORD = "<strong-share-password>"
.\msc-node.exe validator mpc-keygen --validator F --threshold 2 --participants 3 --outdir data/F/mpc
.\msc-node.exe validator mpc-pubkey --pub data/F/mpc/validator.pub
.\msc-node.exe validator create-mpc --wallet .\.msc\secure_wallet.json --validator F --mpc-pub data/F/mpc/validator.pub --amount 100 --rpc https://wallet.mscblockexplorer.in
```

This creates a fresh validator public key and three encrypted MPC shares:

```text
data/F/mpc/validator.pub
data/F/mpc/share1.sec
data/F/mpc/share2.sec
data/F/mpc/share3.sec
```

The threshold `2/3` means any two shares can sign, but one share alone cannot.

### Existing Registered Validator MPC Migration

Use this for existing validators such as `A`, `B`, `C`, and `D`, whose public keys are already registered in genesis or on-chain validator state. Do not run `mpc-keygen` for these validators, because that creates a different validator public key and causes a registry mismatch.

```powershell
$env:MSC_VALIDATOR_PASSWORD = "<existing-validator-key-password>"
$env:MSC_MPC_SHARE_PASSWORD = "<strong-share-password>"
.\msc-node.exe validator mpc-import-key --id A --datadir runtime-data/distributed/A --threshold 2 --participants 3 --outdir runtime-data/distributed/A/mpc
```

`mpc-import-key` decrypts the existing `validator.sec`, splits the same Ed25519 validator seed into encrypted MPC shares, and preserves the exact same consensus public key.

Verify the migrated public key:

```powershell
.\msc-node.exe validator-pubkey --id A --datadir runtime-data/distributed/A
.\msc-node.exe validator mpc-pubkey --pub runtime-data/distributed/A/mpc/validator.pub
```

Both commands must return the same public key.

### MPC Signing Smoke Test

The built-in share files are enough to test the external-signer path:

```powershell
$env:MSC_MPC_SHARE_PASSWORD = "<strong-share-password>"
.\msc-node.exe validator mpc-sign --shares data/F/mpc/share1.sec,data/F/mpc/share2.sec
```

For non-interactive servers, use a password file:

```powershell
.\msc-node.exe validator mpc-sign --shares runtime-data/distributed/A/mpc/share1.sec,runtime-data/distributed/A/mpc/share2.sec --password-file runtime-data/distributed/A/mpc/share.pass
```

### EC2 MPC Enablement Helper

On EC2, the helper creates node-local MPC files and a node-local config:

```text
runtime-data/distributed/A/mpc/validator.pub
runtime-data/distributed/A/mpc/share1.sec
runtime-data/distributed/A/mpc/share2.sec
runtime-data/distributed/A/mpc/share3.sec
runtime-data/distributed/A/mpc/share.pass
runtime-data/distributed/A/config.mpc.toml
```

```bash
cd ~/msc-chain

# Existing validator migration requires the current validator key password.
export MSC_VALIDATOR_PASSWORD="<existing-validator-key-password>"
scripts/enable_mpc_signing.sh A 2 3

# Manual startup example. The supervisor sets MSC_ALLOW_CONFIG_OVERRIDE automatically.
MSC_ALLOW_CONFIG_OVERRIDE=1 ./msc-node --mode=full --role=validator --id=A --port=7001 --datadir=runtime-data/distributed/A --rpcaddr 127.0.0.1:26657 --config runtime-data/distributed/A/config.mpc.toml
```

The running node should report MPC signer mode:

```bash
curl -s http://127.0.0.1:26657/status | jq '.validator_signer_mode, .validator_signer_ready'
```

Expected:

```text
"mpc"
true
```

Do not commit `share*.sec`, `share.pass`, or node-local `config.mpc.toml`. Each validator must generate or migrate its own MPC public key and shares. Never reuse one node's MPC config on another validator.

For production large validators, replace the built-in share-file signer with an audited MPC/DKG signer cluster. The chain-facing contract stays the same: provide one validator public key, and return a valid Ed25519 threshold signature for MSC's canonical signing payload.

HSM-backed validator startup:

```powershell
$env:MSC_ALLOW_VALIDATOR_KEY_CREATE = "0"
go run . --mode=full --role=auto --id=F --port=7005 --datadir=data/F --rpcaddr 127.0.0.1:26665
```

Use `config.toml` to pin the HSM signer:

```toml
[validators]
hsm_enabled = true
hsm_provider = "ledger_enterprise"
hsm_key_id = "msc-validator-F"
hsm_public_key = "<32-byte-ed25519-public-key-hex>"
hsm_external_signer_command = "ledger-msc-signer --key msc-validator-F"
hsm_timeout_ms = 3000
hsm_require_user_presence = false
```

The external signer receives JSON on stdin:

```json
{
  "domain": "msc-validator-ed25519-v1",
  "validator_id": "F",
  "provider": "ledger_enterprise",
  "key_id": "msc-validator-F",
  "public_key_hex": "<pubkey>",
  "payload_hex": "<canonical-signing-payload>"
}
```

It must return either raw signature hex or JSON:

```json
{"signature_hex":"<64-byte-ed25519-signature-hex>"}
```

The node verifies the returned signature before using it. A bad signature, timeout, missing command, wrong public key, or fingerprint mismatch makes the validator signer unavailable.

MPC-backed validator startup uses the same node command, but enables threshold signing in `config.toml`:

```toml
[validators]
mpc_enabled = true
mpc_provider = "threshold_ed25519"
mpc_key_id = "msc-validator-F-cluster"
mpc_public_key = "<32-byte-ed25519-public-key-hex>"
mpc_external_signer_command = "./msc-node validator mpc-sign --shares data/F/mpc/share1.sec,data/F/mpc/share2.sec --password-env MSC_MPC_SHARE_PASSWORD"
mpc_timeout_ms = 3000
mpc_threshold = 2
mpc_participants = 3
```

The MPC signer receives JSON on stdin:

```json
{
  "domain": "msc-validator-mpc-ed25519-v1",
  "signer_mode": "mpc",
  "validator_id": "F",
  "provider": "threshold_ed25519",
  "key_id": "msc-validator-F-cluster",
  "public_key_hex": "<pubkey>",
  "payload_hex": "<canonical-signing-payload>",
  "threshold": 2,
  "participants": 3
}
```

It must return the same response shape:

```json
{"signature_hex":"<64-byte-ed25519-threshold-signature-hex>"}
```

When `mpc_enabled=true`, MSC does not fall back to a local software validator key. This is intentional: unavailable MPC means the validator is unavailable rather than silently signing with a single hot key.

Signer readiness checks:

```powershell
Invoke-RestMethod http://127.0.0.1:26657/status | Select-Object validator_signer_mode,validator_signer_ready,validator_signer_provider,validator_signer_reason
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:26657/metrics | Select-String "msc_validator_signer_"
```

Transaction and staking commands:

```powershell
.\msc-node.exe balance --address MSC... --rpc https://wallet.mscblockexplorer.in
.\msc-node.exe send --wallet .\.msc\secure_wallet.json --to MSC... --amount 10 --rpc https://wallet.mscblockexplorer.in
.\msc-node.exe stake --wallet .\.msc\secure_wallet.json --validator F --validator-pubkey <32-byte-ed25519-pubkey-hex> --amount 100 --rpc https://wallet.mscblockexplorer.in
.\msc-node.exe unstake --wallet .\.msc\secure_wallet.json --validator F --amount 100 --rpc https://wallet.mscblockexplorer.in
.\msc-node.exe claim-rewards --wallet .\.msc\secure_wallet.json --rpc https://wallet.mscblockexplorer.in
```

Node status commands:

```powershell
.\msc-node.exe status --rpc https://explorer.mscblockexplorer.in
.\msc-node.exe peers --rpc https://explorer.mscblockexplorer.in
.\msc-node.exe sync-status --rpc https://explorer.mscblockexplorer.in
```

### RPC And Metrics Checks

```powershell
Invoke-RestMethod https://explorer.mscblockexplorer.in/status
Invoke-RestMethod https://explorer.mscblockexplorer.in/v1/public/status
Invoke-RestMethod https://explorer.mscblockexplorer.in/consensus/mode
Invoke-RestMethod https://explorer.mscblockexplorer.in/formal/verification
Invoke-RestMethod https://explorer.mscblockexplorer.in/storage/policy
Invoke-RestMethod https://explorer.mscblockexplorer.in/bridge/status
Invoke-RestMethod https://explorer.mscblockexplorer.in/v1/validators/diversity
Invoke-RestMethod https://explorer.mscblockexplorer.in/v1/peers
Invoke-WebRequest -UseBasicParsing https://wallet.mscblockexplorer.in/metrics
```

Bridge proof dry-run example, verification only:

```powershell
$proof = @{
  version = "msc-bridge-v1"
  source_chain_id = "external-1"
  event_id = "deposit-42"
  asset_denom = "wEXT"
  origin_asset = "EXT"
  recipient = "MSC01..."
  amount = "100"
  source_height = 1000
  confirmed_height = 1064
  oracle_signatures = @(
    @{ signer = "oracle-a"; signature = "sig-a" },
    @{ signer = "oracle-b"; signature = "sig-b" },
    @{ signer = "oracle-c"; signature = "sig-c" }
  )
} | ConvertTo-Json -Depth 8
Invoke-RestMethod https://explorer.mscblockexplorer.in/bridge/verify -Method POST -Body $proof -ContentType "application/json"
```

Validator RPC should stay private or bound to localhost. Public traffic should use the full-node gateway only:

```text
Explorer: https://explorer.mscblockexplorer.in/explorer.html
Wallet:   https://wallet.mscblockexplorer.in/msc_wallet.html
Status:   https://explorer.mscblockexplorer.in/portal/status.html
DTL IDE:  https://explorer.mscblockexplorer.in/dtl_ide.html
RPC:      https://wallet.mscblockexplorer.in
```

Prepare new validators `H/I/J/K` as synced full nodes first. Activate them one at a time only after sync completes and finality remains stable:

```powershell
.\scripts\ec2_prepare_new_validators.ps1 `
  -KeyPath "C:\Users\Mohammad Talha\Downloads\msc-key.pem" `
  -NodeTargets "H=ubuntu@<H_PUBLIC_IP>","I=ubuntu@<I_PUBLIC_IP>","J=ubuntu@<J_PUBLIC_IP>","K=ubuntu@<K_PUBLIC_IP>" `
  -Peers "<trusted-p2p-peer-list>"
```

The script binds validator RPC to `127.0.0.1`, generates MPC-ready validator config, prints each MPC public key, and prints the exact stake command to run after the node is fully synced.

### Backup, Snapshot, And Recovery

```powershell
.\msc-node.exe backup export --id A --datadir data/A
.\msc-node.exe backup verify --path data/A/node_A/backups/backup_...
.\msc-node.exe backup import --id RESTORE --datadir data/RESTORE --path .\backup --apply
.\msc-node.exe backup recover --id A --datadir data/A --height 1000
.\msc-node.exe snapshot export --id A --datadir data/A
.\msc-node.exe snapshot verify --path data/A/node_A/backups/backup_...
.\msc-node.exe snapshot import --id RESTORE --datadir data/RESTORE --path .\backup --apply
```

Fresh EC2 restore test:

```powershell
.\scripts\ec2_backup_restore_test.ps1 -KeyPath "C:\Users\Mohammad Talha\Downloads\msc-key.pem" -SourceUser ubuntu -SourceHost 54.80.4.133 -TargetUser ubuntu -TargetHost 50.19.167.221
```

### Chaos, Sync, And DDoS Tests

```powershell
.\scripts\mainnet_chaos_network.ps1 -Tier1Survival -DurationMinutes 60 -NodeCount 10 -ValidatorNodeCount 4 -AutoLocalPeers -NetworkChaos -PacketLoss -TxFlood
.\scripts\mainnet_sync_gap_test.ps1
.\scripts\mainnet_soak_churn.ps1
.\scripts\mainnet_ddos_spam_test.ps1 -TargetBase https://wallet.mscblockexplorer.in -DurationSeconds 300
.\scripts\multi_arch_determinism.ps1
```

### Monitoring Stack

```powershell
Set-Location monitoring
docker compose up -d
docker compose logs -f prometheus
docker compose logs -f grafana
```

Prometheus alerts must include no block for more than `60` seconds, finality gap greater than `20`, peers below `3`, disk usage above `80%`, quorum failure, validator offline, and snapshot failure.

### GitHub Release And EC2 Deploy

Every fix should be committed and pushed first:

```powershell
git status --short
git add WHITEPAPER.md
git commit -m "Document operator terminal commands"
git push origin main
```

Then each EC2 node pulls from GitHub and rebuilds locally:

```powershell
$key = "C:\Users\Mohammad Talha\Downloads\msc-key.pem"
ssh -i $key ubuntu@54.80.4.133
```

Run on the EC2 host:

```bash
cd ~/msc-chain
git fetch origin main
git pull --ff-only origin main
GOMAXPROCS=2 go build -o msc-node.new .
chmod +x msc-node.new
ts=$(date -u +%Y%m%d%H%M%S)
[ -x ./msc-node ] && mv ./msc-node ./msc-node.prev-$ts
mv ./msc-node.new ./msc-node
kill "$(cat runtime-logs/distributed/A.pid)"
tail -f runtime-logs/distributed/A.supervisor.log
```

Use the matching node ID and log path for each host:

| Node | SSH target | RPC | Role |
| --- | --- | --- | --- |
| `A` | `ubuntu@54.80.4.133` | `26657` | Validator |
| `B` | `ec2-user@98.90.205.156` | `26658` | Validator |
| `C` | `ec2-user@3.88.214.207` | `26659` | Validator |
| `D` | `ec2-user@34.201.64.103` | `26660` | Validator |
| `F` | `ubuntu@50.19.167.221` | `26665` | Public full node |
| `G` | `ubuntu@54.89.222.13` | `26666` | Full node |

EC2 health checks:

```bash
curl -s http://127.0.0.1:26657/status
curl -s http://127.0.0.1:26657/consensus/mode
curl -s http://127.0.0.1:26657/v1/peers
curl -s http://127.0.0.1:26657/metrics | head
pgrep -af './msc-node'
tail -f runtime-logs/distributed/A.node.log
```

## Conclusion

MSC Chain is a validator-secured blockchain focused on deterministic finality, fixed-supply economics, native token logic, and practical mainnet operation. Its design favors bounded protocol-native execution over arbitrary runtime complexity, strict validator identity over casual node churn, and finality-anchored snapshots over blind fast sync.

The result is a chain architecture aimed at being understandable, auditable, and operator-friendly while still supporting native assets, DTL token issuance, DeFi/GameFi primitives, explorer access, wallet flows, and production-grade monitoring.
