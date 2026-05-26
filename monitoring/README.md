# Monitoring Stack

This folder provides a ready setup for:
- Prometheus
- Grafana
- Alertmanager

## What Is Monitored
- Node height and finalized height
- Peer count
- Sync state and readiness
- Live validator vs required quorum
- Current supply and tokenomics toggles
- Internal map/cache health counters
- Security counters (double-sign, invalid execution, censorship evidence, tx-abuse, fork activity, suspect peers, jailed validators)
- Tier 2 public-mainnet readiness:
  - consensus lag, finality gap, quorum health, and block production age
  - mempool depth, bytes, capacity, utilization, rejects, and throughput estimates
  - per-validator online/ready/last-seen/missed-vote/slash signals
  - snapshot create/load/apply timing, failures, and bootstrap counters
  - replay/rebuild timing, replayed blocks, failures, and digest health
  - storage size, pruning, GC, cold-storage, and finality artifact counters
  - node disk usage percentage for validator disk-pressure alerts
  - sync mode, sync lag, peer quarantine, rate-limit drops, and bad snapshot proofs
  - RPC request rate, body rejects, rate limits, unauthorized requests, and concurrency

## Prerequisites
- Docker + Docker Compose
- Node RPC servers running with `/metrics` enabled on:
  - `127.0.0.1:26657`
  - `127.0.0.1:26658`
  - `127.0.0.1:26659`
  - `127.0.0.1:26660`
  - `127.0.0.1:26661`
  - optional local validators/full nodes on `26662` through `26665`

If your ports differ, edit `prometheus/prometheus.yml`.

If RPC is running on HTTPS (TLS enabled), set this in `prometheus/prometheus.yml` under `job_name: msc_nodes`:
- `scheme: https`
- `tls_config.insecure_skip_verify: true` (for self-signed local certs)

## Run
```powershell
cd monitoring
docker compose up -d
```

Fast local health check:

```powershell
cd ..
.\scripts\check_observability.ps1 -Targets 127.0.0.1:26657,127.0.0.1:26658,127.0.0.1:26659,127.0.0.1:26660
```

## Access
- Prometheus: `http://localhost:9090`
- Alertmanager: `http://localhost:9093`
- Grafana: `http://localhost:3000`
  - user: `admin`
  - password: `admin`

## Default Alerts
- `NoBlockFor60Seconds`
- `FinalityGapOver20`
- `PeersBelow3`
- `DiskUsageOver80Percent`
- `QuorumFailure`
- `ValidatorOffline`
- `SnapshotFailure`
- `MSCNodeDown`
- `MSCNodeNotReady`
- `MSCFinalityStalled`
- `MSCPeersLow`
- `MSCSyncStuck`
- `MSCLiveValidatorsBelowQuorum`
- `MSCDoubleSignDetected`
- `MSCInvalidExecutionDetected`
- `MSCForkActivityDetected`
- `MSCCensorshipEvidenceDetected`
- `MSCTxAbuseDetected`
- `MSCPeerSuspectDetected`
- `MSCValidatorJailed`
- `MSCConsensusLagHigh`
- `MSCFinalityLagHigh`
- `MSCBlockProductionStalled`
- `MSCMempoolBacklogHigh`
- `MSCValidatorHealthLow`
- `MSCSnapshotOperationFailures`
- `MSCReplayOperationFailures`
- `MSCQuorumFailure`
- `MSCStorageUsageHigh`
- `MSCCheckpointConflict`
- `MSCReplayDigestMismatch`
- `MSCLongRangeAttackRejected`
- `MSCRPCRateLimitHigh`
- `MSCRPCBodyRejected`
- `MSCRPCUnauthorizedHigh`
- `MSCRPCConcurrencyHigh`

## Dashboards
- `MSC Public Mainnet Readiness`
- `MSC Chain Overview`
- `MSC Validators`
- `MSC Sync`
- `MSC Storage`
- `MSC Security`
- `MSC RPC API`
- `MSC Mainnet Launch Gates`

The readiness dashboards split the Tier 2 public-mainnet signals into operator views:
chain overview, validators, sync/bootstrap, storage/GC, and security.

For alert response steps, see `RUNBOOK.md`.

## Attack Coverage Mapping
- Double signing / nothing-at-stake:
  - metrics: `msc_security_validator_double_sign_total`
  - alert: `MSCDoubleSignDetected`
- Invalid block / invalid execution:
  - metrics: `msc_security_validator_bad_execution_total`
  - alert: `MSCInvalidExecutionDetected`
- Fork activity / chain split symptoms:
  - metrics: `msc_map_fork_heights`, `msc_map_fork_blocks_total`
  - alert: `MSCForkActivityDetected`
- Censorship signals:
  - metrics: `msc_security_censorship_evidence_total`
  - alert: `MSCCensorshipEvidenceDetected`
- Long-range rewrite risk:
  - metrics: `msc_security_weak_subjectivity_depth`
  - alert: `MSCWeakSubjectivityDisabled`
- Tx abuse / spam behavior:
  - metrics: `msc_security_tx_abuse_records`, `msc_security_tx_abuse_attempts_total`
  - alert: `MSCTxAbuseDetected`
- MEV reorder risk:
  - metrics: `msc_security_deterministic_tx_order_enforced`
  - alert: `MSCDeterministicTxOrderDisabled`
- Eclipse / network manipulation symptoms:
  - metrics: `msc_map_peer_suspect`, `msc_node_peers`
  - alerts: `MSCPeerSuspectDetected`, `MSCPeersLow`

Notes:
- Some attacks (long-range rewrite, time manipulation, MEV) need protocol-level controls (checkpointing, strict timestamp rules, proposer policy) in addition to monitoring.
- Monitoring detects symptoms; slashing/consensus rules enforce punishment.

Alert routing is set to a `null` receiver by default. Update `alertmanager/alertmanager.yml` to plug Slack, email, or webhook.
