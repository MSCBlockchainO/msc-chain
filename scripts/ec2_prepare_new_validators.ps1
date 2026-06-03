param(
  [Parameter(Mandatory = $true)]
  [string]$KeyPath,

  [Parameter(Mandatory = $true)]
  [string[]]$NodeTargets,

  [string]$RepoUrl = "https://github.com/MSCBlockchainO/msc-chain.git",
  [string]$Branch = "main",
  [int]$P2PBasePort = 7011,
  [int]$RPCBasePort = 26667,
  [int]$MPCThreshold = 2,
  [int]$MPCParticipants = 3,
  [string]$Peers = "",
  [switch]$StartAsValidator
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Split-NodeTarget {
  param([string]$Target)
  $parts = $Target.Split("=", 2)
  if ($parts.Count -ne 2 -or [string]::IsNullOrWhiteSpace($parts[0]) -or [string]::IsNullOrWhiteSpace($parts[1])) {
    throw "Invalid NodeTargets entry '$Target'. Use H=ubuntu@1.2.3.4"
  }
  $ssh = $parts[1].Trim()
  if ($ssh -notmatch "^[^@]+@[^@]+$") {
    throw "Invalid SSH target '$ssh'. Use user@host"
  }
  [pscustomobject]@{
    Id = $parts[0].Trim().ToUpperInvariant()
    SSH = $ssh
  }
}

if (-not (Test-Path -LiteralPath $KeyPath)) {
  throw "SSH key not found: $KeyPath"
}

$nodes = @($NodeTargets | ForEach-Object { Split-NodeTarget $_ })
if ($nodes.Count -eq 0) {
  throw "No node targets supplied."
}

$validIds = @("H", "I", "J", "K")
foreach ($node in $nodes) {
  if ($validIds -notcontains $node.Id) {
    throw "This script is scoped to new validator IDs H/I/J/K. Got '$($node.Id)'."
  }
}

for ($i = 0; $i -lt $nodes.Count; $i++) {
  $node = $nodes[$i]
  $p2pPort = $P2PBasePort + $i
  $rpcPort = $RPCBasePort + $i
  $role = if ($StartAsValidator) { "validator" } else { "full" }
  $remoteScript = @'
#!/usr/bin/env bash
set -euo pipefail

NODE_ID="__NODE_ID__"
ROLE="__ROLE__"
P2P_PORT="__P2P_PORT__"
RPC_PORT="__RPC_PORT__"
REPO_URL="__REPO_URL__"
BRANCH="__BRANCH__"
PEERS="__PEERS__"
MPC_THRESHOLD="__MPC_THRESHOLD__"
MPC_PARTICIPANTS="__MPC_PARTICIPANTS__"

echo "[prepare] node=\$NODE_ID role=\$ROLE p2p=\$P2P_PORT rpc=\$RPC_PORT"

if command -v growpart >/dev/null 2>&1; then
  sudo growpart /dev/nvme0n1 1 >/dev/null 2>&1 || sudo growpart /dev/xvda 1 >/dev/null 2>&1 || true
fi
root_src="\$(findmnt -n -o SOURCE / 2>/dev/null || true)"
if [ -n "\$root_src" ]; then
  sudo resize2fs "\$root_src" >/dev/null 2>&1 || sudo xfs_growfs / >/dev/null 2>&1 || true
fi

if ! command -v git >/dev/null 2>&1; then
  sudo apt-get update
  sudo apt-get install -y git curl ca-certificates build-essential
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is not installed on this EC2. Install Go, then rerun this script." >&2
  exit 1
fi

if [ ! -d "\$HOME/msc-chain/.git" ]; then
  git clone --branch "\$BRANCH" "\$REPO_URL" "\$HOME/msc-chain"
fi

cd "\$HOME/msc-chain"
git fetch origin "\$BRANCH"
git checkout "\$BRANCH"
git pull --ff-only origin "\$BRANCH"
go build -o msc-node .
chmod +x msc-node scripts/*.sh

mkdir -p runtime-logs/distributed runtime-data/distributed/"\$NODE_ID"
MSC_VALIDATOR_PASSWORD="\${MSC_VALIDATOR_PASSWORD:-mainnet-mpc-\$NODE_ID}" scripts/enable_mpc_signing.sh "\$NODE_ID" "\$MPC_THRESHOLD" "\$MPC_PARTICIPANTS" | tee "runtime-data/distributed/\$NODE_ID/mpc-setup.out"

if [ -f "runtime-logs/distributed/\$NODE_ID.supervisor.pid" ]; then
  old_pid="\$(cat "runtime-logs/distributed/\$NODE_ID.supervisor.pid" 2>/dev/null || true)"
  [ -n "\$old_pid" ] && kill "\$old_pid" >/dev/null 2>&1 || true
fi
if [ -f "runtime-logs/distributed/\$NODE_ID.pid" ]; then
  old_pid="\$(cat "runtime-logs/distributed/\$NODE_ID.pid" 2>/dev/null || true)"
  [ -n "\$old_pid" ] && kill "\$old_pid" >/dev/null 2>&1 || true
fi
sleep 2

nohup env \
  NODE_ID="\$NODE_ID" \
  ROLE="\$ROLE" \
  BINARY="./msc-node" \
  CONFIG="runtime-data/distributed/\$NODE_ID/config.mpc.toml" \
  DATA_DIR="runtime-data/distributed/\$NODE_ID" \
  LOG_ROOT="runtime-logs/distributed" \
  P2P_PORT="\$P2P_PORT" \
  RPC_PORT="\$RPC_PORT" \
  RPC_ADDR="127.0.0.1:\$RPC_PORT" \
  PEERS="\$PEERS" \
  HEALTHCHECK_SECONDS=20 \
  HEALTHCHECK_ADDR="127.0.0.1:\$RPC_PORT" \
  GOMAXPROCS=2 \
  GOGC=75 \
  scripts/ec2_node_supervisor.sh > "runtime-logs/distributed/\$NODE_ID.bootstrap.log" 2>&1 &

sleep 5
PUBKEY="\$(awk -F= '/^mpc_public_key=/ {print \$2}' "runtime-data/distributed/\$NODE_ID/mpc-setup.out" | tail -1)"
echo "[ready] node=\$NODE_ID mpc_public_key=\$PUBKEY"
echo "[logs] tail -f ~/msc-chain/runtime-logs/distributed/\$NODE_ID.node.log"
echo "[stake after synced] ./msc-node stake \$NODE_ID 100 \$PUBKEY"
'@

  $remoteScript = $remoteScript.
    Replace("__NODE_ID__", $node.Id).
    Replace("__ROLE__", $role).
    Replace("__P2P_PORT__", [string]$p2pPort).
    Replace("__RPC_PORT__", [string]$rpcPort).
    Replace("__REPO_URL__", $RepoUrl).
    Replace("__BRANCH__", $Branch).
    Replace("__PEERS__", $Peers).
    Replace("__MPC_THRESHOLD__", [string]$MPCThreshold).
    Replace("__MPC_PARTICIPANTS__", [string]$MPCParticipants)
  $remoteScript = $remoteScript.Replace('\$', '$')

  $tmp = [System.IO.Path]::GetTempFileName()
    Set-Content -LiteralPath $tmp -Value $remoteScript -Encoding ASCII
  try {
    Write-Host "==> Preparing $($node.Id) on $($node.SSH) (role=$role p2p=$p2pPort rpc=$rpcPort)"
    scp -i $KeyPath -o StrictHostKeyChecking=no $tmp "$($node.SSH):/tmp/msc_prepare_$($node.Id).sh" | Out-Host
    ssh -i $KeyPath -o StrictHostKeyChecking=no $node.SSH "bash /tmp/msc_prepare_$($node.Id).sh" | Out-Host
  } finally {
    Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
  }
}

Write-Host ""
Write-Host "H/I/J/K should stay full nodes until each is fully synced. Activate one at a time with the printed MPC public key, then wait for stable finality before the next validator."
