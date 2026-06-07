param(
  [ValidateSet("full", "candidate", "validator")][string]$Role = "candidate",
  [Parameter(Mandatory = $true)][string]$NodeId,
  [string]$Rpc = "127.0.0.1:26657",
  [string]$Peers = "",
  [switch]$LowRam,
  [switch]$AutoStart,
  [switch]$PublicGateway,
  [switch]$Allow4GBValidator,
  [string]$PrebuiltBinary = "",
  [string]$InstallDir = "",
  [int]$P2PPort = 7001
)

$ErrorActionPreference = "Stop"

function Require-Command {
  param([string]$Name)
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "$Name is required in PATH"
  }
}

function Set-OwnerOnlyAcl {
  param([string]$Path)
  if ($env:OS -notlike "*Windows*") { return }
  $user = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
  & icacls $Path /inheritance:r /grant:r "$user`:(OI)(CI)F" /remove:g "Users" "Authenticated Users" "Everyone" | Out-Null
}

function Get-HostMemoryMiB {
  try {
    $mem = (Get-CimInstance Win32_ComputerSystem -ErrorAction Stop).TotalPhysicalMemory
    if ($mem -gt 0) { return [int64]($mem / 1MB) }
  } catch {
    return 0
  }
  return 0
}

function Replace-Or-AppendToml {
  param([string]$Path, [string]$Section, [string]$Key, [string]$Value)
  $content = Get-Content -Path $Path -Raw
  $pattern = "(?ms)(^\[$([Regex]::Escape($Section))\]\s*.*?)(?=^\[|\z)"
  $match = [Regex]::Match($content, $pattern)
  if (-not $match.Success) {
    $content = $content.TrimEnd() + "`r`n`r`n[$Section]`r`n$Key = $Value`r`n"
  } else {
    $sectionText = $match.Groups[1].Value
    $keyPattern = "(?m)^\s*$([Regex]::Escape($Key))\s*=.*$"
    if ([Regex]::IsMatch($sectionText, $keyPattern)) {
      $sectionText = [Regex]::Replace($sectionText, $keyPattern, "$Key = $Value", 1)
    } else {
      $sectionText = $sectionText.TrimEnd() + "`r`n$Key = $Value`r`n"
    }
    $content = $content.Substring(0, $match.Index) + $sectionText + $content.Substring($match.Index + $match.Length)
  }
  [System.IO.File]::WriteAllText($Path, $content, [System.Text.UTF8Encoding]::new($false))
}

if ($PrebuiltBinary -eq "") {
  Require-Command go
}

$repo = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repo

if (-not (Test-Path "genesis.json")) { throw "genesis.json not found" }
if (-not (Test-Path "config.toml")) { throw "config.toml not found" }

$genesisHash = (Get-FileHash -Algorithm SHA256 "genesis.json").Hash.ToLowerInvariant()
$configRaw = Get-Content "config.toml" -Raw
$configuredHash = ""
$m = [Regex]::Match($configRaw, '(?m)^\s*genesis_hash\s*=\s*"([^"]+)"')
if ($m.Success) { $configuredHash = $m.Groups[1].Value.Trim().ToLowerInvariant() }
if ($configuredHash -and $configuredHash -ne $genesisHash) {
  throw "genesis hash mismatch: file=$genesisHash config=$configuredHash"
}

$id = $NodeId.Trim().ToUpperInvariant()
$hostMemMiB = Get-HostMemoryMiB
if ($Role -eq "validator" -and $LowRam -and $hostMemMiB -gt 0 -and $hostMemMiB -lt 8192 -and -not $Allow4GBValidator) {
  throw "validator low-RAM mode requires at least 8GB RAM. This host has ${hostMemMiB}MiB. Use -Allow4GBValidator only for test/candidate rehearsal."
}
if (-not $InstallDir) {
  $InstallDir = Join-Path $HOME ".msc\nodes\$id"
}
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
$dataDir = Join-Path $InstallDir "data"
$logDir = Join-Path $InstallDir "logs"
$configPath = Join-Path $InstallDir "config.toml"
$envPath = Join-Path $InstallDir "node.env.ps1"
$binaryPath = Join-Path $InstallDir "msc-node.exe"
$startPath = Join-Path $InstallDir "start.ps1"
$nodePath = Join-Path $dataDir "node_$id"
$manifestPath = Join-Path $InstallDir "install_manifest.json"

New-Item -ItemType Directory -Force -Path $InstallDir, $dataDir, $logDir | Out-Null
Set-OwnerOnlyAcl $InstallDir

if (-not (Test-Path -LiteralPath $configPath)) {
  Copy-Item "config.toml" $configPath -Force
}
Replace-Or-AppendToml $configPath "rpc" "laddr" "`"$Rpc`""
Replace-Or-AppendToml $configPath "storage" "history_profile" "`"full`""
Replace-Or-AppendToml $configPath "storage" "state_pruning_enabled" "true"
if ($LowRam) {
  Replace-Or-AppendToml $configPath "sync" "delta_replay_verify_workers" "2"
  Replace-Or-AppendToml $configPath "sync" "snapshot_parallel_chunks" "2"
  Replace-Or-AppendToml $configPath "storage" "parallel_gc_workers" "1"
  Replace-Or-AppendToml $configPath "rpc" "max_concurrent_requests" "64"
}

if ($PrebuiltBinary -ne "") {
  $resolvedPrebuilt = Resolve-Path -LiteralPath $PrebuiltBinary
  Copy-Item -LiteralPath $resolvedPrebuilt -Destination $binaryPath -Force
} else {
  go build -o $binaryPath .
}
$aliasPath = Join-Path $InstallDir "msc.exe"
Copy-Item -LiteralPath $binaryPath -Destination $aliasPath -Force

$runtimeRole = if ($Role -eq "validator") { "validator" } else { "full" }
$profile = if ($LowRam) { "home_low_ram" } else { "standard" }
$gomem = if ($LowRam) {
  if ($Role -eq "validator") { "2048MiB" } else { "1536MiB" }
} else { "" }
$gomax = if ($LowRam) { "2" } else { "" }
$allow4 = if ($Allow4GBValidator) { "1" } else { "0" }
$gateway = if ($PublicGateway) { "1" } else { "0" }

@"
`$env:MSC_NODE_PROFILE="$profile"
`$env:MSC_LOW_RAM_MODE="$(if ($LowRam) { "1" } else { "0" })"
`$env:MSC_ALLOW_4GB_VALIDATOR="$allow4"
`$env:MSC_PUBLIC_GATEWAY="$gateway"
`$env:MSC_AUTH_OPEN_BROWSER="0"
`$env:GOGC="75"
$(if ($gomem) { "`$env:GOMEMLIMIT=`"$gomem`"" })
$(if ($gomax) { "`$env:GOMAXPROCS=`"$gomax`"" })
"@ | Set-Content -Path $envPath -Encoding UTF8

@"
`$ErrorActionPreference = "Stop"
. "$envPath"
`$peerArgs = @()
if ("$Peers") { `$peerArgs = @("--peers", "$Peers") }
& "$binaryPath" --mode=full --role=$runtimeRole --id=$id --port=$P2PPort --datadir="$dataDir" --rpcaddr="$Rpc" --config="$configPath" @peerArgs
"@ | Set-Content -Path $startPath -Encoding UTF8

if ($Role -eq "validator" -and $LowRam -and $hostMemMiB -eq 0 -and -not $Allow4GBValidator) {
  Write-Warning "Low-RAM validator mode is 8GB recommended. Host memory detection was unavailable; use -Allow4GBValidator only for test/candidate rehearsal."
}

if ($AutoStart) {
  $taskName = "MSC-Node-$id"
  try {
    $action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$startPath`""
    $trigger = New-ScheduledTaskTrigger -AtLogOn
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Description "MSC node $id auto-start" -Force | Out-Null
    Start-ScheduledTask -TaskName $taskName
    Write-Host "scheduled task started: $taskName"
  } catch {
    Write-Warning "scheduled task setup failed; starting hidden process instead: $($_.Exception.Message)"
    Start-Process powershell -WindowStyle Hidden -ArgumentList @("-ExecutionPolicy", "Bypass", "-File", $startPath)
  }
}

$validatorPub = ""
$pubPath = Join-Path $nodePath "validator.pub"
if (Test-Path -LiteralPath $pubPath) {
  $validatorPub = (Get-Content -Raw -LiteralPath $pubPath).Trim()
}
$fingerprint = ""
$fpPath = Join-Path $nodePath "fingerprint.lock"
if (Test-Path -LiteralPath $fpPath) {
  $fingerprint = (Get-Content -Raw -LiteralPath $fpPath).Trim()
}
$manifest = [ordered]@{
  schema_version = 1
  node_id = $id
  role = $Role
  install_dir = $InstallDir
  data_dir = $dataDir
  node_path = $nodePath
  config_path = $configPath
  binary_path = $binaryPath
  alias_path = $aliasPath
  genesis_hash = $genesisHash
  validator_pubkey = $validatorPub
  validator_fingerprint = $fingerprint
  service_name = "MSC-Node-$id"
  os = "windows"
  arch = $env:PROCESSOR_ARCHITECTURE
  source = $(if ($PrebuiltBinary -ne "") { "release" } else { "local" })
  updated_at = (Get-Date).ToUniversalTime().ToString("o")
}
$manifest | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 -LiteralPath $manifestPath

Write-Host "MSC node installed."
Write-Host "Node ID: $id"
Write-Host "Role: $Role runtime=$runtimeRole profile=$profile"
Write-Host "Install: $InstallDir"
Write-Host "Binary: $binaryPath"
Write-Host "Alias: $aliasPath"
Write-Host "Manifest: $manifestPath"
Write-Host "Start: powershell -ExecutionPolicy Bypass -File `"$startPath`""
