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

## 17. RPC, Explorer, And Wallet

MSC Chain exposes RPC endpoints for node status, block data, transaction submission, tokenomics, governance, explorer views, wallet interactions, DTL token data, and observability.

The repository includes:

- web explorer UI;
- MSC wallet UI;
- DTL IDE;
- token/logo/NFT image guidance;
- public gateway deployment scripts;
- TLS certificate generation tooling.

Production public access should terminate at a full-node gateway, not a validator RPC port. Validator RPC should remain private or bound to localhost. Public gateway deployments should use HTTPS, nginx rate limits, node RPC limits, and private metrics scraping.

## 18. Security Baseline

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

## 19. Governance

MSC Chain currently has four governance surfaces:

1. Core validator registry governance: signed registry updates add, activate, retire, or enforce core validator identity.
2. Validator update certificates: threshold-approved validator add/remove transactions prevent unilateral validator set mutation.
3. DTL governance certificates: token-specific threshold governance controls minting, pause/unpause, freeze/unfreeze, and authority rotation.
4. On-chain governance proposals: validator voting, treasury voting, protocol upgrade scheduling, rollback-protected activation heights, and bounded emergency pause records.

Treasury operations are disabled on mainnet unless explicitly enabled and authorized.

Protocol upgrades are versioned and activated by height. Rollbacks are rejected unless the proposal explicitly declares rollback approval and receives strict quorum. Emergency upgrades can use a shorter activation path, but still require strict quorum. Emergency pause proposals are bounded, observable through RPC and Prometheus, and intended for operator coordination during incident response.

## 20. Testing And Mainnet Readiness

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

## 21. Limitations And Risk Statement

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

## 22. Roadmap

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

## 23. Operator Terminal Command Reference

This section is the production command reference for operators. Do not commit validator passwords, wallet passwords, private keys, SSH keys, or mnemonic phrases. Use environment variables, local secret files, or an interactive terminal.

### Build And Test

```powershell
go test . -run "TestOperator|TestProductionGenesis|TestConsensusMode|TestGovernance|TestProtocolUpgrade|TestEmergency" -count=1
go build -o msc-node.exe .
.\msc-node.exe help
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
.\msc-node.exe validator create --wallet .\.msc\secure_wallet.json --validator F --validator-pubkey <32-byte-ed25519-pubkey-hex> --amount 100 --rpc https://mscblockexplorer.in
```

MPC validator key ceremony commands:

```powershell
$env:MSC_MPC_SHARE_PASSWORD = "<strong-share-password>"
.\msc-node.exe validator mpc-keygen --validator F --threshold 2 --participants 3 --outdir data/F/mpc
.\msc-node.exe validator mpc-pubkey --pub data/F/mpc/validator.pub
.\msc-node.exe validator create-mpc --wallet .\.msc\secure_wallet.json --validator F --mpc-pub data/F/mpc/validator.pub --amount 100 --rpc https://mscblockexplorer.in
```

The built-in `mpc-keygen` creates:

```text
data/F/mpc/validator.pub
data/F/mpc/share1.sec
data/F/mpc/share2.sec
data/F/mpc/share3.sec
```

It does not write `validator.sec`. The full validator private key is not stored on disk by the MSC node. The encrypted share files are enough to test the MPC external-signer path:

```powershell
.\msc-node.exe validator mpc-sign --shares data/F/mpc/share1.sec,data/F/mpc/share2.sec
```

For production large validators, replace the built-in share-file signer with an audited MPC/DKG signer cluster. The chain-facing contract stays the same: provide one validator public key, and return a valid Ed25519 threshold signature for MSC's canonical signing payload.

EC2 node-local MPC enablement helper:

```bash
cd ~/msc-chain
scripts/enable_mpc_signing.sh A 2 3
./msc-node --mode=full --role=validator --id=A --port=7001 --datadir=runtime-data/distributed/A --rpcaddr 127.0.0.1:26657 --config runtime-data/distributed/A/config.mpc.toml
```

The helper writes node-local files only:

```text
runtime-data/distributed/A/mpc/validator.pub
runtime-data/distributed/A/mpc/share1.sec
runtime-data/distributed/A/mpc/share2.sec
runtime-data/distributed/A/mpc/share3.sec
runtime-data/distributed/A/mpc/share.pass
runtime-data/distributed/A/config.mpc.toml
```

Do not commit `share*.sec` or `share.pass`. Each validator must generate its own MPC public key and shares. Never reuse one node's MPC config on another validator.

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
.\msc-node.exe balance --address MSC... --rpc https://mscblockexplorer.in
.\msc-node.exe send --wallet .\.msc\secure_wallet.json --to MSC... --amount 10 --rpc https://mscblockexplorer.in
.\msc-node.exe stake --wallet .\.msc\secure_wallet.json --validator F --validator-pubkey <32-byte-ed25519-pubkey-hex> --amount 100 --rpc https://mscblockexplorer.in
.\msc-node.exe unstake --wallet .\.msc\secure_wallet.json --validator F --amount 100 --rpc https://mscblockexplorer.in
.\msc-node.exe claim-rewards --wallet .\.msc\secure_wallet.json --rpc https://mscblockexplorer.in
```

Node status commands:

```powershell
.\msc-node.exe status --rpc https://mscblockexplorer.in
.\msc-node.exe peers --rpc https://mscblockexplorer.in
.\msc-node.exe sync-status --rpc https://mscblockexplorer.in
```

### RPC And Metrics Checks

```powershell
Invoke-RestMethod https://mscblockexplorer.in/status
Invoke-RestMethod https://mscblockexplorer.in/consensus/mode
Invoke-RestMethod https://mscblockexplorer.in/v1/validators/diversity
Invoke-RestMethod https://mscblockexplorer.in/v1/peers
Invoke-WebRequest -UseBasicParsing https://mscblockexplorer.in/metrics
```

Validator RPC should stay private or bound to localhost. Public traffic should use the full-node gateway only:

```text
Explorer: https://mscblockexplorer.in/explorer.html
Wallet:   https://mscblockexplorer.in/msc_wallet.html
DTL IDE:  https://mscblockexplorer.in/dtl_ide.html
RPC:      https://mscblockexplorer.in
```

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
.\scripts\mainnet_ddos_spam_test.ps1 -TargetBase https://mscblockexplorer.in -DurationSeconds 300
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
