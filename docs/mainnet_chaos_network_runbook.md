# Mainnet Chaos Network Runbook

Tier 1 goal: prove consensus keeps finalizing under real network chaos, not only unit-level deterministic tests.

## Local 10 Node Survival Run

Build once, then run the chaos harness:

```powershell
go build -o .\msc-chain.exe .
$env:MSC_VALIDATOR_PASSWORD = "your-validator-password"
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\mainnet_chaos_network.ps1 `
  -UseBuiltBinary `
  -Tier1Survival `
  -NodeCount 10 `
  -ValidatorNodeCount 5 `
  -DurationMinutes 360 `
  -WarmupSeconds 180 `
  -RestartStormSize 2 `
  -AllowSnapshotMutation `
  -TxFloodBatch 20 `
  -MaxBlockGapSeconds 45 `
  -MaxFinalizedLagBlocks 16 `
  -MaxHeightLagBlocks 32 `
  -FailOnWarning
```

What this injects:
- randomized validator/full-node restarts
- restart storms
- disconnect windows through local firewall when run as Administrator
- packet-loss pulses, or Linux `tc netem` when remote netem is enabled
- slow-validator windows through low process priority and optional CPU pressure
- stale snapshot restarts by hiding newest snapshot directories in the test data dir
- faucet-based mempool floods
- fork checks by comparing finalized block hashes across reachable nodes

Outputs:
- terminal sample line every `SampleSeconds`
- `runtime-logs/mainnet-chaos-*/events.jsonl`
- `runtime-logs/mainnet-chaos-*/summary.json`
- one node log per node in the run log directory

Pass condition:
- no finality stall beyond `MaxBlockGapSeconds` after warmup
- reachable quorum stays above `MinReachableNodes`
- finalized lag stays within `MaxFinalizedLagBlocks`
- height lag stays within `MaxHeightLagBlocks`
- no finalized block hash divergence across nodes

## Local 50 Node Scale Run

Use this after the 10 node run is clean:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\mainnet_chaos_network.ps1 `
  -UseBuiltBinary `
  -Tier1Survival `
  -NodeCount 50 `
  -ValidatorNodeCount 10 `
  -DurationMinutes 720 `
  -WarmupSeconds 300 `
  -RestartStormSize 5 `
  -AllowSnapshotMutation `
  -TxFloodBatch 50 `
  -CpuPressureWorkers 1 `
  -MaxBlockGapSeconds 60 `
  -MaxFinalizedLagBlocks 24 `
  -MaxHeightLagBlocks 48 `
  -FailOnWarning
```

For 50 nodes, make sure validator keys/stakes exist for the validators you mark with `-ValidatorNodeCount`. Extra nodes can run as `full` observers.

## Distributed Controller Mode

Start nodes on separate machines first, then run the controller from one terminal with `-SkipStart` and a topology file:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\mainnet_chaos_network.ps1 `
  -Tier1Survival `
  -SkipStart `
  -TopologyPath .\scripts\mainnet_chaos_topology.example.json `
  -DurationMinutes 720 `
  -WarmupSeconds 300 `
  -UseRemoteNetem `
  -NetemInterface eth0 `
  -AllowSnapshotMutation `
  -FailOnWarning
```

Remote packet loss and latency need passwordless SSH plus `sudo tc` permission on Linux hosts. Without that, the controller still monitors consensus and records skipped netem events.
Stale snapshot injection is gated by `-AllowSnapshotMutation` because it temporarily renames newest snapshot directories in the selected test data directory.

## EC2 Single Host Rehearsal

Use this for a quick AWS rehearsal on one Ubuntu EC2 before spending money on a multi-instance topology. It starts nodes, collects fresh libp2p peer IDs, writes `peers.json`, restarts with generated peer topology, injects restarts, and fails if block/finality progress is zero.

```bash
cd ~/msc-chain
MSC_VALIDATOR_PASSWORD='your-validator-password' \
DATA_ROOT=runtime-data/ec2-six-node \
LOG_ROOT=runtime-logs/ec2-six-node \
NODE_IDS='A B C D F G' \
VALIDATOR_COUNT=4 \
DURATION_SECONDS=900 \
WARMUP_SECONDS=60 \
RESTART_EVERY_SECONDS=90 \
RESTART_DOWN_SECONDS=8 \
BASE_P2P_PORT=19101 \
BASE_RPC_PORT=29257 \
./scripts/ec2_single_host_chaos.sh
```

This is not a replacement for distributed EC2 testing because all nodes share one kernel, disk, clock, and network path. Treat it as a remote smoke test before the multi-machine run.

## Quick Dry Run

Before a real run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\mainnet_chaos_network.ps1 `
  -DryRun `
  -Tier1Survival `
  -NodeCount 10 `
  -ValidatorNodeCount 5 `
  -UseBuiltBinary
```
