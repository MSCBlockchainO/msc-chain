param(
    [int]$DurationMinutes = 1440,
    [int]$MinRestartSeconds = 90,
    [int]$MaxRestartSeconds = 240,
    [int]$MaxBlockGapSeconds = 15,
    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [string]$DataRoot = "data",
    [string]$LogRoot = "runtime-logs",
    [switch]$SkipStart,
    [switch]$NoRandomRestarts
)

$ErrorActionPreference = "Stop"

$nodes = @(
    @{ Id = "A"; Role = "validator"; Port = 7001; Rpc = "127.0.0.1:26657"; PasswordEnv = "MSC_VALIDATOR_PASSWORD_A" },
    @{ Id = "B"; Role = "validator"; Port = 7002; Rpc = "127.0.0.1:26658"; PasswordEnv = "MSC_VALIDATOR_PASSWORD_B" },
    @{ Id = "C"; Role = "validator"; Port = 7003; Rpc = "127.0.0.1:26659"; PasswordEnv = "MSC_VALIDATOR_PASSWORD_C" },
    @{ Id = "D"; Role = "validator"; Port = 7004; Rpc = "127.0.0.1:26660"; PasswordEnv = "MSC_VALIDATOR_PASSWORD_D" },
    @{ Id = "F"; Role = "full";      Port = 7005; Rpc = "127.0.0.1:26661"; PasswordEnv = "MSC_VALIDATOR_PASSWORD_F" }
)

function Get-NodePassword {
    param([hashtable]$Node)
    $specific = [Environment]::GetEnvironmentVariable($Node.PasswordEnv, "Process")
    if ([string]::IsNullOrWhiteSpace($specific)) {
        $specific = [Environment]::GetEnvironmentVariable($Node.PasswordEnv, "User")
    }
    if (-not [string]::IsNullOrWhiteSpace($specific)) {
        return $specific
    }
    $global = [Environment]::GetEnvironmentVariable("MSC_VALIDATOR_PASSWORD", "Process")
    if ([string]::IsNullOrWhiteSpace($global)) {
        $global = [Environment]::GetEnvironmentVariable("MSC_VALIDATOR_PASSWORD", "User")
    }
    return $global
}

function Start-SoakNode {
    param(
        [hashtable]$Node,
        [string]$RunDir
    )
    $id = $Node.Id
    $password = Get-NodePassword -Node $Node
    if ([string]::IsNullOrWhiteSpace($password)) {
        throw "Missing validator password for node $id. Set $($Node.PasswordEnv) or MSC_VALIDATOR_PASSWORD."
    }
    $nodeLog = Join-Path $RunDir "$id.log"
    $dataDir = Join-Path $DataRoot $id
    $command = @(
        "`$env:MSC_VALIDATOR_PASSWORD='$($password.Replace("'","''"))'",
        "Set-Location '$($RepoRoot.Replace("'","''"))'",
        "go run . --mode=full --role=$($Node.Role) --id=$id --port=$($Node.Port) --datadir=$dataDir --rpcaddr $($Node.Rpc) *> '$($nodeLog.Replace("'","''"))'"
    ) -join "; "
    $proc = Start-Process -FilePath "powershell" -WindowStyle Hidden -PassThru -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $command)
    $Node.Process = $proc
    $Node.StartedAt = Get-Date
    return $proc
}

function Stop-SoakNode {
    param([hashtable]$Node)
    if ($Node.ContainsKey("Process") -and $Node.Process -and -not $Node.Process.HasExited) {
        Stop-Process -Id $Node.Process.Id -Force -ErrorAction SilentlyContinue
    }
    $Node.Remove("Process")
}

function Read-NodeStatus {
    param([hashtable]$Node)
    $url = "http://$($Node.Rpc)/status"
    try {
        return Invoke-RestMethod -Uri $url -TimeoutSec 3
    } catch {
        return $null
    }
}

function Write-SoakEvent {
    param(
        [string]$Path,
        [string]$Type,
        [hashtable]$Fields
    )
    $event = [ordered]@{
        at = (Get-Date).ToUniversalTime().ToString("o")
        type = $Type
    }
    foreach ($key in $Fields.Keys) {
        $event[$key] = $Fields[$key]
    }
    ($event | ConvertTo-Json -Compress -Depth 8) | Add-Content -Path $Path
}

$runId = "mainnet-soak-" + (Get-Date -Format "yyyyMMdd-HHmmss")
$runDir = Join-Path (Join-Path $RepoRoot $LogRoot) $runId
New-Item -ItemType Directory -Force -Path $runDir | Out-Null
$eventsPath = Join-Path $runDir "events.jsonl"

Write-SoakEvent -Path $eventsPath -Type "soak_start" -Fields @{
    duration_minutes = $DurationMinutes
    max_block_gap_seconds = $MaxBlockGapSeconds
    random_restarts = (-not $NoRandomRestarts.IsPresent)
}

if (-not $SkipStart) {
    foreach ($node in $nodes) {
        Start-SoakNode -Node $node -RunDir $runDir | Out-Null
        Write-SoakEvent -Path $eventsPath -Type "node_start" -Fields @{ node = $node.Id; rpc = $node.Rpc; port = $node.Port }
        Start-Sleep -Seconds 2
    }
}

$deadline = (Get-Date).AddMinutes($DurationMinutes)
$nextRestart = (Get-Date).AddSeconds((Get-Random -Minimum $MinRestartSeconds -Maximum ($MaxRestartSeconds + 1)))
$lastMaxHeight = 0
$lastProgressAt = Get-Date

try {
    while ((Get-Date) -lt $deadline) {
        $statuses = @()
        foreach ($node in $nodes) {
            $status = Read-NodeStatus -Node $node
            $height = 0
            $state = "unreachable"
            $ready = $false
            $mode = ""
            if ($status) {
                $height = [int64]($status.height)
                $state = [string]($status.state)
                $ready = ($state -notmatch "NOT_READY|SYNCING|WALLET_AUTH_REQUIRED")
                $mode = [string]($status.quorum_policy_mode)
            }
            $statuses += [pscustomobject]@{
                Node = $node.Id
                Height = $height
                State = $state
                Ready = $ready
                Mode = $mode
            }
        }

        $maxHeight = ($statuses | Measure-Object -Property Height -Maximum).Maximum
        $readyCount = @($statuses | Where-Object { $_.Ready }).Count
        if ($maxHeight -gt $lastMaxHeight) {
            $lastMaxHeight = $maxHeight
            $lastProgressAt = Get-Date
        }
        $gapSeconds = [int]((Get-Date) - $lastProgressAt).TotalSeconds
        Write-SoakEvent -Path $eventsPath -Type "status_sample" -Fields @{
            max_height = $maxHeight
            ready_count = $readyCount
            no_progress_seconds = $gapSeconds
            nodes = $statuses
        }
        if ($gapSeconds -gt $MaxBlockGapSeconds) {
            Write-SoakEvent -Path $eventsPath -Type "block_gap_warning" -Fields @{
                max_height = $maxHeight
                no_progress_seconds = $gapSeconds
                limit_seconds = $MaxBlockGapSeconds
            }
        }

        if (-not $NoRandomRestarts -and (Get-Date) -ge $nextRestart) {
            $candidate = $nodes | Get-Random
            Write-SoakEvent -Path $eventsPath -Type "node_restart_begin" -Fields @{ node = $candidate.Id }
            Stop-SoakNode -Node $candidate
            Start-Sleep -Seconds (Get-Random -Minimum 10 -Maximum 31)
            Start-SoakNode -Node $candidate -RunDir $runDir | Out-Null
            Write-SoakEvent -Path $eventsPath -Type "node_restart_done" -Fields @{ node = $candidate.Id }
            $nextRestart = (Get-Date).AddSeconds((Get-Random -Minimum $MinRestartSeconds -Maximum ($MaxRestartSeconds + 1)))
        }

        Start-Sleep -Seconds 5
    }
} finally {
    Write-SoakEvent -Path $eventsPath -Type "soak_stop" -Fields @{ max_height = $lastMaxHeight }
    if (-not $SkipStart) {
        foreach ($node in $nodes) {
            Stop-SoakNode -Node $node
        }
    }
    Write-Host "Soak logs: $runDir"
}
