param(
  [string]$Root = ".",
  [string[]]$Nodes = @("A", "B", "C", "D"),
  [switch]$DryRun
)

$ErrorActionPreference = "Stop"

$rootPath = (Resolve-Path $Root).Path
$dataPath = Join-Path $rootPath "data"
if (!(Test-Path $dataPath)) {
  throw "data directory not found: $dataPath"
}

$ts = Get-Date -Format "yyyyMMdd_HHmmss"
$backupRoot = Join-Path $dataPath "_resync_backup_$ts"

function Log([string]$msg) {
  Write-Host "[resync] $msg"
}

function EnsureDir([string]$path) {
  if (!(Test-Path $path)) {
    New-Item -ItemType Directory -Path $path | Out-Null
  }
}

Log "workspace: $rootPath"
Log "nodes: $($Nodes -join ', ')"
Log "backup: $backupRoot"
if ($DryRun) {
  Log "mode: DRY RUN"
}

# Stop locally running node processes from this workspace (safe no-op if not running).
$procNames = @("msc-chain.exe", "learining3.exe", "go.exe")
$procs = Get-CimInstance Win32_Process | Where-Object {
  $name = $_.Name
  $cmd = $_.CommandLine
  ($procNames -contains $name) -and $cmd -and $cmd.ToLower().Contains($rootPath.ToLower())
}
foreach ($p in $procs) {
  Log "stopping process pid=$($p.ProcessId) name=$($p.Name)"
  if (-not $DryRun) {
    Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
  }
}

if (-not $DryRun) {
  EnsureDir $backupRoot
}

$targets = @(
  "blocks.db",
  "state.db",
  "ledger.json",
  "validators.json",
  "peers.json"
)

foreach ($id in $Nodes) {
  $nodeDir = Join-Path $dataPath $id
  $nodeData = Join-Path $nodeDir ("node_" + $id)
  if (!(Test-Path $nodeData)) {
    Log "skip node $id (missing $nodeData)"
    continue
  }

  $nodeBackup = Join-Path $backupRoot ("node_" + $id)
  Log "processing node $id"
  if (-not $DryRun) {
    EnsureDir $nodeBackup
  }

  foreach ($keep in @("validator.sec", "tokenomics.json")) {
    $src = if ($keep -eq "tokenomics.json") { Join-Path $nodeDir $keep } else { Join-Path $nodeData $keep }
    if (Test-Path $src) {
      $dst = if ($keep -eq "tokenomics.json") { Join-Path $nodeBackup $keep } else { Join-Path $nodeBackup "validator.sec" }
      Log "backup $src -> $dst"
      if (-not $DryRun) {
        Copy-Item -Path $src -Destination $dst -Force
      }
    }
  }

  foreach ($t in $targets) {
    $p = Join-Path $nodeData $t
    if (Test-Path $p) {
      Log "remove $p"
      if (-not $DryRun) {
        Remove-Item -Path $p -Recurse -Force
      }
    }
  }

  # Recreate minimal files expected by node startup.
  $validatorsJson = Join-Path $nodeData "validators.json"
  if (-not $DryRun -and !(Test-Path $validatorsJson)) {
    Set-Content -Path $validatorsJson -Value "[]" -NoNewline
  }
}

Log "done"
