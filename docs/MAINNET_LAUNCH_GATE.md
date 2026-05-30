# MSC Mainnet Launch Gate

This is the final checklist before MSC mainnet can be treated as launch-ready. Do not launch while any required gate is red.

## Storage Gate

Required validator policy:

- State pruning enabled for validator nodes.
- Snapshot compaction enabled with tiered retention.
- Archive history separated from validator nodes.
- Validator hot storage target: under `100 GB`.
- Disk-full auto-alerts enabled at `80%` warning and `90%` critical.

Configured defaults in `config.toml`:

| Control | Value |
| --- | ---: |
| `epoch_length_blocks` | `100` |
| `validator_retained_epochs` | `10` |
| `validator_rollback_window_blocks` | `256` |
| `validator_snapshot_keep_last` | `3` |
| `validator_recent_block_window` | `2048` |
| `full_node_history_blocks` | `5256000` |
| `hourly_snapshot_retain` | `24` |
| `daily_snapshot_retain` | `30` |
| `weekly_snapshot_retain` | `12` |
| `monthly_snapshot_retain` | `24` |
| `cold_export_enabled` | `true` |
| `cold_export_compression` | `zstd` |
| `parallel_gc_workers` | `4` |

Archive nodes must run with archive history mode and must not be used as validators. Public explorer/wallet should point at full nodes, not validator RPC.

## Release Gate

Required for each mainnet release:

1. Create a version tag, for example `v1.0.0-mainnet`.
2. Build reproducible binaries with `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=false`.
3. Publish `genesis.json` SHA256.
4. Publish binary SHA256 checksums.
5. Publish the Git commit hash used for the build.
6. Publish operator docs and the frozen genesis record.

Build command:

```powershell
.\scripts\build_mainnet_release.ps1 -VersionTag v1.0.0-mainnet
```

The script writes:

- `dist/<version>/release-manifest.json`
- `dist/<version>/checksums.txt`
- platform binaries for Windows, Linux amd64, and Linux arm64
- copied `genesis.json`, `config.toml`, and launch docs

For a smoke build before committing, use:

```powershell
.\scripts\build_mainnet_release.ps1 -VersionTag v1.0.0-mainnet -AllowDirty
```

Do not publish an `-AllowDirty` build as a real release.

## Required Tests

Run these before tagging:

```powershell
go test . -run "TestProductionGenesis|TestStorageConfig|TestStorageManager|TestBackup|TestRecovery|TestDurability|TestMultiArch|TestMetricsEndpoint" -count=1
```

Run distributed survival tests:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\mainnet_ddos_spam_test.ps1 -TargetBase https://mscblockexplorer.in -DurationSeconds 60 -Concurrency 16 -RequestsPerWorker 120
powershell -ExecutionPolicy Bypass -File scripts\mainnet_sync_gap_test.ps1
powershell -ExecutionPolicy Bypass -File scripts\ec2_backup_restore_test.ps1
```

## Monitoring Gate

Prometheus and alert rules must be active before launch:

- `NoBlockFor60Seconds`
- `FinalityGapOver20`
- `PeersBelow3`
- `DiskUsageOver80Percent`
- `DiskUsageOver90Percent`
- `MSCValidatorDiskTargetExceeded`
- `QuorumFailure`
- `ValidatorOffline`
- `SnapshotFailure`

Grafana must show:

- block height and finalized height
- consensus mode
- quorum health
- validator online/offline status
- peer count and disconnect rate
- mempool pressure
- snapshot failures
- disk usage and storage size

## Network Gate

Required:

- Validator RPC private only.
- Public RPC served only by full nodes or the public gateway.
- Explorer and wallet public through HTTPS/domain.
- Rate limits enabled on the public gateway.
- P2P ports open only as needed.
- No validator key generation on production startup.

## Backup Gate

Required:

- Snapshot export/import tested.
- Fresh EC2 restore tested.
- Recovery target: `2-5 minutes`.
- Corrupt snapshot rejected and older valid snapshot loaded.
- Recovery artifacts kept off-validator where possible.

## Governance Gate

Required:

- Proposal create tested.
- Validator voting tested with strict quorum.
- Protocol upgrade activation height tested.
- Rollback protection tested.
- Emergency pause/upgrade process tested.
- Slashing and validator lifecycle tested.

Runbook: `docs/governance_upgrade_slashing_runbook.md`.

## Launch Decision

Launch only when all are true:

- Genesis hash is frozen and published.
- Release manifest and checksums are published.
- Four or more validators produce and finalize blocks for a 24h/72h soak.
- DDoS/spam test does not halt block production.
- Backup restore passes on a fresh EC2.
- Disk usage stays under validator target.
- Alerts fire correctly during controlled failure drills.
- Explorer and wallet work over HTTPS.
- Validator RPC remains private.

If any gate fails, fix, push to GitHub, redeploy from GitHub, and restart the gate from the beginning.
