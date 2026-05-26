# MSC Observability Runbook

Use this when Prometheus or Grafana shows a Tier-2 public-mainnet readiness alert.

## Fast Terminal Check

From the repo root:

```powershell
.\scripts\check_observability.ps1 -Targets 127.0.0.1:26657,127.0.0.1:26658,127.0.0.1:26659,127.0.0.1:26660
```

For a node that requires bearer auth:

```powershell
.\scripts\check_observability.ps1 -Targets https://127.0.0.1:26657 -BearerToken $env:MSC_API_TOKEN
```

## Critical Alerts

### NoBlockFor60Seconds

Meaning: `msc_consensus_last_block_age_seconds > 60`.

Checks:
- Confirm quorum health: `msc_quorum_observed >= msc_quorum_required`.
- Confirm peer floor: `msc_peers_connected >= 3`.
- Check proposer logs for stalled commit/proposal loop.

Actions:
- Restart only the stalled node first.
- If multiple validators show the same alert, inspect network partition and quorum before restarting more nodes.

### MSCFinalityLagHigh

Meaning: `msc_finality_gap > 20`.

Checks:
- Confirm `msc_quorum_observed >= msc_quorum_required`.
- Confirm `msc_consensus_last_block_age_seconds <= 60`.
- Check validator panels for offline or missed-vote validators.

Actions:
- Restart only the unhealthy validator process, not the whole network.
- If many validators are behind, use snapshot/bootstrap sync before rejoining consensus.
- Do not accept degraded finality certificates.

### MSCQuorumFailure

Meaning: observed validator quorum is below strict required quorum.

Checks:
- Compare `msc_validator_online` and `msc_validator_ready`.
- Check `msc_peers_connected` and `msc_peers_quarantined`.
- Check peer logs for protocol/genesis/version mismatch disconnects.

Actions:
- Bring back enough validators to satisfy strict quorum.
- Fix unstable peers or wrong genesis/config first.
- Avoid force-finalizing blocks while quorum is below requirement.

### ValidatorOffline

Meaning: `msc_validator_health_offline > 0` or a per-validator `msc_validator_online == 0`.

Checks:
- Find the validator label in Grafana or Prometheus.
- Confirm process, disk, and peer count on that validator.
- Compare with `msc_validator_ready` and `msc_validator_last_seen_seconds`.

Actions:
- Restart the offline validator only after verifying it has the correct genesis, chain ID, and validator key.
- Use snapshot restore if it is behind by many blocks.

### MSCBlockProductionStalled

Meaning: no committed block observed for more than 60 seconds.

Checks:
- `msc_consensus_last_block_age_seconds`
- `msc_mempool_size`
- `msc_quorum_failures_total`
- `msc_sync_lag_blocks`

Actions:
- If quorum is healthy and mempool is clear, inspect proposer/leader logs.
- If sync lag exists, let node catch up before validator participation.
- If peers are low, reconnect bootnodes/trusted peers.

### MSCCheckpointConflict / MSCReplayDigestMismatch

Meaning: finality artifact or deterministic replay conflict.

Checks:
- `msc_checkpoint_conflicts_total`
- `msc_finality_conflicts_total`
- `msc_replay_digest_mismatch_total`
- `msc_long_range_attack_reject_total`

Actions:
- Stop validator participation for the affected node.
- Preserve `data/<node>` for forensic review.
- Restart from the latest trusted snapshot and finalized epoch certificate.

## Warning Alerts

### MSCConsensusLagHigh

Meaning: `msc_consensus_lag_blocks > 50`.

Actions:
- Check `msc_sync_mode`: `0=gossip`, `1=range`, `2=delta`, `3=snapshot`.
- If lag is large, prefer range/delta/snapshot sync instead of gossip-only catchup.
- Check `msc_snapshot_failures_total` and peer reputation.

### MSCSnapshotOperationFailures

Actions:
- Check snapshot provider reputation.
- Verify snapshot anchor/proof availability.
- Fall back to another provider or older trusted checkpoint.

### MSCReplayOperationFailures

Actions:
- Check state snapshot and WAL durability first.
- Restart from previous valid checkpoint if replay keeps failing.

### MSCStorageUsageHigh

Actions:
- Check `msc_disk_usage_percent`; if above 80, prune/export before disk reaches 90.
- Confirm validator is not running archive mode.
- Check `msc_storage_pruned_states_total`, `msc_storage_gc_cycles_total`, and `msc_cold_storage_size_bytes`.
- Run storage manager after a finalized epoch, then verify snapshots were compacted.

### MSCPeersLow

Actions:
- Check `msc_peers_quarantined`, `msc_rate_limit_drops_total`, and `msc_bad_snapshot_proofs_total`.
- Verify bootnode list, chain ID, genesis hash, version, and consensus params.

### MSCRPCRateLimitHigh / MSCRPCUnauthorizedHigh

Actions:
- Check if traffic is coming from one client, proxy, or bot.
- Tighten `read_rate_limit_per_minute` and `write_rate_limit_per_minute` if public RPC is exposed directly.
- Require auth for read endpoints when running validator RPC on the public internet.

### MSCRPCBodyRejected

Actions:
- Confirm the client is not sending malformed or oversized JSON.
- Keep `max_request_body_bytes` low for public RPC; validators should not accept large arbitrary bodies.

### MSCRPCConcurrencyHigh

Actions:
- Check whether slow clients are holding connections open.
- Increase `max_concurrent_requests` only if CPU, memory, and storage latency are healthy.
- Prefer a reverse proxy in front of public full-node RPC; do not expose validator RPC broadly.
