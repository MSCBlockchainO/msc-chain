param(
  [string]$HostName = "56.155.122.236",
  [string]$User = "ubuntu",
  [string]$KeyPath = (Join-Path $env:TEMP "msc-deploy-keys\msc node 4.pem"),
  [string]$BinaryPath = "msc-node.linux",
  [string]$ConfigPath = "config.toml",
  [string]$GenesisPath = "genesis.json",
  [string]$ServicePath = "deploy\systemd\msc-production-v2-standby-node.service"
)

$ErrorActionPreference = "Stop"

function Resolve-RepoPath([string]$Path) {
  if ([System.IO.Path]::IsPathRooted($Path)) {
    return $Path
  }
  return (Join-Path (Get-Location) $Path)
}

$key = Resolve-RepoPath $KeyPath
$binary = Resolve-RepoPath $BinaryPath
$config = Resolve-RepoPath $ConfigPath
$genesis = Resolve-RepoPath $GenesisPath
$service = Resolve-RepoPath $ServicePath

foreach ($path in @($key, $binary, $config, $genesis, $service)) {
  if (-not (Test-Path -LiteralPath $path)) {
    throw "required file not found: $path"
  }
}

$target = "$User@$HostName"
$sshOpts = @(
  "-o", "BatchMode=yes",
  "-o", "ConnectTimeout=10",
  "-o", "StrictHostKeyChecking=no",
  "-i", $key
)

Write-Host "Checking SSH access to $target ..."
ssh @sshOpts $target "echo SSH_OK"
if ($LASTEXITCODE -ne 0) {
  throw "SSH failed. Open TCP 22 to $HostName before running this deploy helper."
}

$binaryHash = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()
$genesisHash = (Get-FileHash -LiteralPath $genesis -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Host "Binary SHA256 : $binaryHash"
Write-Host "Genesis SHA256: $genesisHash"

Write-Host "Uploading standby artifacts ..."
ssh @sshOpts $target "mkdir -p /tmp/msc-standby-deploy"
scp @sshOpts $binary "${target}:/tmp/msc-standby-deploy/msc-node"
scp @sshOpts $config "${target}:/tmp/msc-standby-deploy/config.toml"
scp @sshOpts $genesis "${target}:/tmp/msc-standby-deploy/genesis.json"
scp @sshOpts $service "${target}:/tmp/msc-standby-deploy/msc-production-v2-standby-node.service"

$remote = @'
set -euo pipefail
install_root=/home/ubuntu/.msc/production-v2
release_dir="$install_root/release"
data_dir="$install_root/data"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

sudo systemctl stop msc-production-v2-standby-node.service 2>/dev/null || true
mkdir -p "$release_dir" "$data_dir"
chmod 700 "$install_root" "$data_dir"

if [ -f "$release_dir/msc-node" ]; then
  cp "$release_dir/msc-node" "$release_dir/msc-node.codex-before-standby-$stamp"
fi
if [ -f "$release_dir/config.toml" ]; then
  cp "$release_dir/config.toml" "$release_dir/config.toml.codex-before-standby-$stamp"
fi
if [ -f "$release_dir/genesis.json" ]; then
  cp "$release_dir/genesis.json" "$release_dir/genesis.json.codex-before-standby-$stamp"
fi

install -m 0755 /tmp/msc-standby-deploy/msc-node "$release_dir/msc-node"
install -m 0644 /tmp/msc-standby-deploy/config.toml "$release_dir/config.toml"
install -m 0644 /tmp/msc-standby-deploy/genesis.json "$release_dir/genesis.json"
sudo install -m 0644 /tmp/msc-standby-deploy/msc-production-v2-standby-node.service /etc/systemd/system/msc-production-v2-standby-node.service

sudo systemctl daemon-reload
sudo systemctl enable msc-production-v2-standby-node.service
sudo systemctl restart msc-production-v2-standby-node.service
sleep 8

systemctl --no-pager --plain is-active msc-production-v2-standby-node.service
curl -sS --max-time 8 http://127.0.0.1:26662/status | python3 -m json.tool | sed -n '1,80p'
'@

Write-Host "Installing and starting standby service ..."
$remote | ssh @sshOpts $target "bash -s"
if ($LASTEXITCODE -ne 0) {
  throw "remote standby install failed"
}

Write-Host "Standby deploy complete."
