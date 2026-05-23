param(
  [string]$NodeID = "F",
  [string]$RpcUrl = "http://127.0.0.1:26661",
  [string]$CoreRpcUrl = "http://127.0.0.1:26657",
  [int]$PollSeconds = 5,
  [int]$StallSeconds = 180,
  [uint64]$LagThreshold = 64,
  [int]$MaxRestarts = 3,
  [int]$BaseBackoffSeconds = 10,
  [string]$Workspace = ".",
  [string]$RestartCommand = "go run . --config config.toml --mode=full --id=F --port=7005 --datadir=data/F --rpcaddr 127.0.0.1:26661",
  [string]$EnvPasswordName = "MSC_VALIDATOR_PASSWORD",
  [string]$EnvPasswordValue = "",
  [switch]$OneShot,
  [switch]$DryRun
)

$ErrorActionPreference = "Stop"

function Log([string]$msg) {
  $ts = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
  Write-Host "[$ts] [watchdog] $msg"
}

function Invoke-Status([string]$baseUrl) {
  try {
    return Invoke-RestMethod -Method Get -Uri "$($baseUrl.TrimEnd('/'))/status" -TimeoutSec 5 -ErrorAction Stop
  } catch {
    return $null
  }
}

function To-Int64OrZero([object]$v) {
  if ($null -eq $v) { return [int64]0 }
  try { return [int64]$v } catch { return [int64]0 }
}

function Stop-NodeProcess([string]$nodeID, [string]$rpcUrl, [string]$workspacePath, [switch]$DryRunMode) {
  $rpcHostPort = ""
  try {
    $uri = [Uri]$rpcUrl
    $rpcHostPort = "$($uri.Host):$($uri.Port)"
  } catch {
    $rpcHostPort = ""
  }

  $candidates = Get-CimInstance Win32_Process | Where-Object {
    $cmd = "$($_.CommandLine)"
    if ([string]::IsNullOrWhiteSpace($cmd)) { return $false }
    $name = "$($_.Name)".ToLowerInvariant()
    if ($name -notin @("go.exe", "msc-chain.exe", "learining3.exe")) { return $false }

    $matchNode = $cmd.Contains("--id=$nodeID")
    $matchRpc = ($rpcHostPort -ne "" -and $cmd.Contains("--rpcaddr $rpcHostPort"))
    $matchWorkspace = $cmd.ToLowerInvariant().Contains($workspacePath.ToLowerInvariant())

    return ($matchNode -or $matchRpc) -and $matchWorkspace
  }

  if (-not $candidates -or $candidates.Count -eq 0) {
    Log "no running process matched for node=$nodeID"
    return
  }

  foreach ($proc in $candidates) {
    Log "stopping pid=$($proc.ProcessId) name=$($proc.Name)"
    if (-not $DryRunMode) {
      Stop-Process -Id $proc.ProcessId -Force -ErrorAction SilentlyContinue
    }
  }
}

function Start-NodeProcess([string]$workspacePath, [string]$restartCmd, [string]$envName, [string]$envValue, [switch]$DryRunMode) {
  $escapedValue = $envValue.Replace("'", "''")
  $body = ""
  if (-not [string]::IsNullOrWhiteSpace($envName) -and -not [string]::IsNullOrWhiteSpace($envValue)) {
    $body = "`$env:$envName='$escapedValue'; $restartCmd"
  } else {
    $body = $restartCmd
  }
  $cmd = "& { $body }"

  Log "starting: $restartCmd"
  if ($DryRunMode) {
    return
  }

  Start-Process `
    -FilePath "powershell" `
    -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $cmd) `
    -WorkingDirectory $workspacePath | Out-Null
}

if ($PollSeconds -lt 2) { $PollSeconds = 2 }
if ($StallSeconds -lt 30) { $StallSeconds = 30 }
if ($MaxRestarts -lt 0) { $MaxRestarts = 0 }
if ($BaseBackoffSeconds -lt 1) { $BaseBackoffSeconds = 1 }

$workspacePath = (Resolve-Path $Workspace).Path
Log "node=$NodeID rpc=$RpcUrl core=$CoreRpcUrl workspace=$workspacePath dry_run=$DryRun"

$restarts = 0
$lastFinalized = [int64]0
$lastProgressAt = Get-Date
$lastSummaryAt = [DateTime]::MinValue

while ($true) {
  $core = Invoke-Status $CoreRpcUrl
  $self = Invoke-Status $RpcUrl

  if ($null -eq $self) {
    Log "status unreachable at $RpcUrl"
    if ($OneShot) { break }
    Start-Sleep -Seconds $PollSeconds
    continue
  }

  $selfFinalized = To-Int64OrZero $self.finalized_height
  $selfSyncing = [bool]$self.syncing
  $selfReady = [bool]$self.ready
  $selfWait = "$($self.wait_reason)"
  $syncMode = "$($self.sync_mode)"
  $syncAction = "$($self.sync_action)"
  $statusLag = To-Int64OrZero $self.sync_lag_blocks

  $coreFinalized = To-Int64OrZero ($core.finalized_height)
  $computedLag = [Math]::Max(0, $coreFinalized - $selfFinalized)
  $coreReachable = ($null -ne $core -and $coreFinalized -gt 0)
  $effectiveLag = if ($coreReachable) { $computedLag } else { [Math]::Max($computedLag, $statusLag) }

  if ($selfFinalized -gt $lastFinalized) {
    $lastFinalized = $selfFinalized
    $lastProgressAt = Get-Date
  }

  $stallFor = [int]((Get-Date) - $lastProgressAt).TotalSeconds
  $shouldRestart = $selfSyncing -and ($effectiveLag -ge [int64]$LagThreshold) -and ($stallFor -ge $StallSeconds)

  $now = Get-Date
  if (($now - $lastSummaryAt).TotalSeconds -ge 15 -or $shouldRestart -or $selfReady) {
    Log ("status finalized={0} core_finalized={1} lag={2} syncing={3} ready={4} wait={5} mode={6} action={7} stall_s={8} restarts={9}/{10}" -f `
      $selfFinalized, $coreFinalized, $effectiveLag, $selfSyncing, $selfReady, $selfWait, $syncMode, $syncAction, $stallFor, $restarts, $MaxRestarts)
    $lastSummaryAt = $now
  }

  if ($shouldRestart) {
    if ($restarts -ge $MaxRestarts) {
      Log "ALERT max restarts reached ($MaxRestarts). manual intervention required."
      break
    }

    $restarts++
    Log "stall detected: syncing with no finalized progress for ${stallFor}s and lag=$effectiveLag. restart #$restarts."
    Stop-NodeProcess -nodeID $NodeID -rpcUrl $RpcUrl -workspacePath $workspacePath -DryRunMode:$DryRun
    Start-Sleep -Seconds 2
    Start-NodeProcess -workspacePath $workspacePath -restartCmd $RestartCommand -envName $EnvPasswordName -envValue $EnvPasswordValue -DryRunMode:$DryRun

    $backoff = [int]([Math]::Pow(2, [Math]::Max(0, $restarts - 1)) * $BaseBackoffSeconds)
    if ($backoff -gt 120) { $backoff = 120 }
    Log "waiting ${backoff}s before next health check after restart"
    Start-Sleep -Seconds $backoff
    $lastProgressAt = Get-Date
    if ($OneShot) { break }
    continue
  }

  if ($OneShot) {
    break
  }
  Start-Sleep -Seconds $PollSeconds
}
