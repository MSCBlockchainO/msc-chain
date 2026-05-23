param(
    [Parameter(Mandatory=$true)][string]$TopologyPath,
    [Parameter(Mandatory=$true)][string]$KeyPath,
    [int]$DurationSeconds = 86400,
    [int]$WarmupSeconds = 300,
    [int]$SampleSeconds = 30,
    [int]$MaxBlockGapSeconds = 120,
    [int]$MaxFinalizedLagBlocks = 48,
    [int]$MaxHeightLagBlocks = 64,
    [int]$MinReachableNodes = 4,
    [string]$LogRoot = "runtime-logs",
    [string]$RunId = ("ec2-ssh-monitor-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
)

$ErrorActionPreference = "Stop"

function Read-Topology {
    param([string]$Path)
    $raw = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    if ($raw.PSObject.Properties.Name -contains "nodes") {
        return @($raw.nodes)
    }
    return @($raw)
}

function Invoke-NodeStatus {
    param([object]$Node, [string]$Key)
    $target = "$($Node.user)@$($Node.public)"
    $cmd = "curl -fsS --max-time 4 http://127.0.0.1:26657/status"
    $out = @()
    try {
        $out = @(ssh -o ConnectTimeout=8 -o BatchMode=yes -i $Key $target $cmd 2>$null)
    } catch {
        return [pscustomobject]@{ id=$Node.id; reachable=$false; height=0L; finalized=0L; peers=0; ready=0 }
    }
    if ($LASTEXITCODE -ne 0 -or $out.Count -eq 0) {
        return [pscustomobject]@{ id=$Node.id; reachable=$false; height=0L; finalized=0L; peers=0; ready=0 }
    }
    try {
        $s = ($out -join "`n") | ConvertFrom-Json
        $h = [int64]($s.height)
        $f = [int64]($s.finalized_height)
        if ($f -le 0) { $f = $h }
        $p = [int]($s.peers)
        $r = 0
        if ($s.ready) { $r = 1 }
        return [pscustomobject]@{ id=$Node.id; reachable=$true; height=$h; finalized=$f; peers=$p; ready=$r }
    } catch {
        return [pscustomobject]@{ id=$Node.id; reachable=$false; height=0L; finalized=0L; peers=0; ready=0 }
    }
}

function Write-JsonLine {
    param([string]$Path, [hashtable]$Fields)
    $Fields["ts"] = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    Add-Content -LiteralPath $Path -Value ($Fields | ConvertTo-Json -Depth 10 -Compress)
}

$nodes = Read-Topology -Path $TopologyPath
if ($nodes.Count -eq 0) {
    throw "topology has no nodes: $TopologyPath"
}
if (-not (Test-Path -LiteralPath $KeyPath)) {
    throw "ssh key not found: $KeyPath"
}

$runDir = Join-Path $LogRoot $RunId
New-Item -ItemType Directory -Force -Path $runDir | Out-Null
$eventsPath = Join-Path $runDir "events.jsonl"
$summaryPath = Join-Path $runDir "summary.json"
$pidPath = Join-Path $runDir "monitor.pid"
[System.Diagnostics.Process]::GetCurrentProcess().Id | Set-Content -LiteralPath $pidPath

Write-JsonLine -Path $eventsPath -Fields @{
    type = "monitor_start"
    run_id = $RunId
    duration_seconds = $DurationSeconds
    warmup_seconds = $WarmupSeconds
    node_count = $nodes.Count
}

$startAt = Get-Date
$deadline = $startAt.AddSeconds($DurationSeconds)
$lastProgressAt = Get-Date
$lastMaxFinalized = [int64]0
$lastMaxHeight = [int64]0
$maxGap = 0
$maxLagF = [int64]0
$maxLagH = [int64]0
$warnings = 0
$samples = 0

try {
    while ((Get-Date) -lt $deadline) {
        $samples++
        $rows = @()
        foreach ($node in $nodes) {
            $rows += Invoke-NodeStatus -Node $node -Key $KeyPath
        }
        $reachable = @($rows | Where-Object { $_.reachable })
        $ready = @($rows | Where-Object { $_.ready -eq 1 })
        if ($reachable.Count -gt 0) {
            $maxH = [int64](($reachable | Measure-Object -Property height -Maximum).Maximum)
            $minH = [int64](($reachable | Measure-Object -Property height -Minimum).Minimum)
            $maxF = [int64](($reachable | Measure-Object -Property finalized -Maximum).Maximum)
            $minF = [int64](($reachable | Measure-Object -Property finalized -Minimum).Minimum)
        } else {
            $maxH = 0L; $minH = 0L; $maxF = 0L; $minF = 0L
        }
        if ($maxF -gt $lastMaxFinalized) {
            $lastMaxFinalized = $maxF
            $lastProgressAt = Get-Date
        }
        if ($maxH -gt $lastMaxHeight) { $lastMaxHeight = $maxH }
        $gap = [int]((Get-Date) - $lastProgressAt).TotalSeconds
        if ($gap -gt $maxGap) { $maxGap = $gap }
        $lagF = [int64]($maxF - $minF)
        if ($lagF -gt $maxLagF) { $maxLagF = $lagF }
        $lagH = [int64]($maxH - $minH)
        if ($lagH -gt $maxLagH) { $maxLagH = $lagH }
        $pastWarmup = ((Get-Date) - $startAt).TotalSeconds -ge $WarmupSeconds
        if ($pastWarmup -and $gap -gt $MaxBlockGapSeconds) { $warnings++ }
        if ($pastWarmup -and $reachable.Count -lt $MinReachableNodes) { $warnings++ }
        if ($pastWarmup -and $lagF -gt $MaxFinalizedLagBlocks) { $warnings++ }
        if ($pastWarmup -and $lagH -gt $MaxHeightLagBlocks) { $warnings++ }

        $line = "sample=$samples reachable=$($reachable.Count) ready=$($ready.Count) height=$minH-$maxH finalized=$minF-$maxF gap_s=$gap lag_f=$lagF lag_h=$lagH warnings=$warnings"
        Write-Host $line
        Write-JsonLine -Path $eventsPath -Fields @{
            type = "status_sample"
            sample = $samples
            reachable = $reachable.Count
            ready = $ready.Count
            min_height = $minH
            max_height = $maxH
            min_finalized = $minF
            max_finalized = $maxF
            gap_seconds = $gap
            finalized_lag = $lagF
            height_lag = $lagH
            warnings = $warnings
            nodes = $rows
        }
        Start-Sleep -Seconds $SampleSeconds
    }
} finally {
    Remove-Item -LiteralPath $pidPath -Force -ErrorAction SilentlyContinue
}

if ($lastMaxHeight -le 0) { $warnings++ }
if ($lastMaxFinalized -le 0) { $warnings++ }
$passed = ($warnings -eq 0)
$summary = [ordered]@{
    run_id = $RunId
    passed = $passed
    warning_count = $warnings
    samples = $samples
    max_height = $lastMaxHeight
    max_finalized = $lastMaxFinalized
    max_no_progress_seconds = $maxGap
    max_finalized_lag_blocks = $maxLagF
    max_height_lag_blocks = $maxLagH
    events_path = $eventsPath
}
$summary | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $summaryPath
Write-JsonLine -Path $eventsPath -Fields @{
    type = "monitor_stop"
    run_id = $RunId
    passed = $passed
    warnings = $warnings
    max_height = $lastMaxHeight
    max_finalized = $lastMaxFinalized
}
if (-not $passed) {
    exit 1
}
