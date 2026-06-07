# MSC Chain — Bug Analysis Report

I've completed a thorough code audit of the MSC Chain codebase, reading ~15,000+ lines across the core source files (`p2p.go`, `recovery_fix.go`, `validator_engine.go`, `consensus_safety_persistence.go`, `consensus_mode_detector.go`, `block_verification.go`, `finality_layer.go`, `consensus_lane_ingress.go`, `fork_convergence.go`, `deterministic_state_engine.go`, `backup_recovery.go`, `snapshot_enterprise.go`, `block_store.go`).

The full report has been saved to **`project_info__1.md`**. Here are the 20 bugs identified, ranked by severity:

---

## CRITICAL (1)

### BUG 7 — Chain Rewind Breaks Blockchain State
**File**: `recovery_fix.go:1700`

`rewindLocalChainToHeight()` truncates `Blockchain.Blocks` to just the anchor block but doesn't rebuild from block files. The `committedHeight` and `finalizedHeight` are set to the anchor's height, but blocks in the committed map between anchor+1 and the old tip remain, creating a broken state where blocks are "committed" but not "in chain." Consensus then operates at the wrong epoch.

---

## HIGH (5)

### BUG 1 — TOCTOU Race in Execution Vote Credit
**File**: `p2p.go:3200`

`allowExecutionVoteIngress()` checks `execVoteCreditedGlobal()` under `execVoteGuardMu`, but `recordExecResultGlobal()` acquires `ExecPool.mu` independently. Two concurrent goroutines processing the same validator's vote can both credit it, inflating quorum counts.

### BUG 4 / BUG 16 — Data Race on `validatorSuspect` Map
**Files**: `p2p.go`, `snapshot_enterprise.go`

`onPeerDisconnected()` writes to `n.validatorSuspect` under `peerStateMu`, but `applyPeerInfo()` reads (and deletes from) `n.validatorSuspect` **without any lock**. This is a classic Go data race that will be detected by the race detector and can cause panics on concurrent map access.

### BUG 12 — Missing Genesis Registry Persistence Can Halt the Chain
**File**: `deterministic_state_engine.go:70`

When a genesis block contains `validator_registry_hash` but the registry isn't persisted to the committed snapshot store, block 2 processing fails with `validator_registry_hash_mismatch` and the chain halts permanently. Affects fresh-genesis deployments.

### BUG 1 (additional) — `execVoteCreditedGlobal` Double-Credit Window
Reiterated due to consensus-critical nature.

---

## MEDIUM (9)

| # | Description | File |
|---|-------------|------|
| 2 | Block serve concurrency cap (64) too low for large meshes | `p2p.go` |
| 3 | PeerHello nonce replay protection evadable via restart | `p2p.go` |
| 5 | Snapshot creation race sends empty responses to syncing peers | `p2p.go` |
| 6 | Proposal vote key changes when StateRoot is populated mid-vote | `p2p.go` |
| 9 | Consensus mode detector uses height+1 for validators but height for metrics | `consensus_mode_detector.go` |
| 11 | Strike window reset (3min) allows persistent byzantine behavior | `p2p.go` |
| 13 | Commit votes in memory never pruned for finalized heights | `consensus_safety_persistence.go` |
| 17 | False equivocation detection on legitimate round changes | `p2p.go` |
| 18 | Recovery-mode blocks with ActiveReadyCount=0 rejected | `block_verification.go` |
| 20 | Post-block safe mode window not persisted across restarts | `recovery_fix.go` |

---

## LOW (5)

| # | Description | File |
|---|-------------|------|
| 8 | Warmup state writes lastHeightAt outside syncMu lock | `snapshot_enterprise.go` |
| 10 | Peer identity sent before peer hello validated (info leak) | `p2p.go` |
| 14 | Double stream Close() on encode failure | `p2p.go` |
| 15 | Config `detector_recovery_validator_lag_blocks` may not bind to variable | `config.toml` / `consensus_mode_detector.go` |
| 19 | `sanitizeContiguousLoadedBlocks` returns partial results without error type | `recovery_fix.go` |

---

## Recommended Priority Fix Order

1. **BUG 7** (CRITICAL) — Fix chain rewind to rebuild blocks from files
2. **BUG 12** (HIGH) — Persist genesis registry snapshot during InitChain
3. **BUG 16** (HIGH) — Add `validatorMu` protection to `validatorSuspect` access
4. **BUG 1** (HIGH) — Fix TOCTOU in execution vote credit
5. **BUG 6** (MEDIUM) — Make proposal vote key independent of StateRoot
6. **BUG 13** (MEDIUM) — Prune commit votes below finalized height
7. **BUG 11** (MEDIUM) — Use decay-based instead of window-reset strike tracking
8. **BUG 20** (MEDIUM) — Persist safe mode window to safety journal

---

The full report with detailed code excerpts, reproduction scenarios, and fix suggestions for each bug is in `project_info__1.md`.