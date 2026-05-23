param(
    [string[]]$Targets = @(
        "http://127.0.0.1:26657",
        "http://127.0.0.1:26658",
        "http://127.0.0.1:26659",
        "http://127.0.0.1:26660",
        "http://127.0.0.1:26661",
        "http://127.0.0.1:26662",
        "http://127.0.0.1:26663",
        "http://127.0.0.1:26664",
        "http://127.0.0.1:26665"
    ),
    [string]$BearerToken = "",
    [int]$TimeoutSeconds = 3,
    [int]$LagWarnBlocks = 50,
    [int]$FinalityCriticalBlocks = 20,
    [int]$NoBlockCriticalSeconds = 60,
    [int]$MinPeers = 3,
    [switch]$SkipUnreachable
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Normalize-Target {
    param([string]$Target)
    $target = $Target.Trim()
    if ($target -notmatch '^https?://') {
        $target = "http://$target"
    }
    return $target.TrimEnd('/')
}

function Read-Metrics {
    param([string]$Body)
    $metrics = @{}
    foreach ($line in ($Body -split "`n")) {
        $line = $line.Trim()
        if ($line -eq "" -or $line.StartsWith("#")) {
            continue
        }
        if ($line -notmatch '^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+([-+]?[0-9]*\.?[0-9]+([eE][-+]?[0-9]+)?)') {
            continue
        }
        $name = $matches[1]
        $value = [double]$matches[3]
        if (-not $metrics.ContainsKey($name)) {
            $metrics[$name] = $value
        }
    }
    return $metrics
}

function Metric {
    param(
        [hashtable]$Metrics,
        [string]$Name,
        [double]$Default = 0
    )
    if ($Metrics.ContainsKey($Name)) {
        return [double]$Metrics[$Name]
    }
    return $Default
}

$headers = @{}
if ($BearerToken.Trim() -ne "") {
    $headers["Authorization"] = "Bearer $BearerToken"
}

$rows = New-Object System.Collections.ArrayList
$failures = 0

foreach ($targetRaw in $Targets) {
    $target = Normalize-Target $targetRaw
    $uri = "$target/metrics"
    try {
        $response = Invoke-WebRequest -Uri $uri -Headers $headers -TimeoutSec $TimeoutSeconds -UseBasicParsing
        $metrics = Read-Metrics -Body $response.Content
        $lag = Metric $metrics "msc_consensus_lag_blocks"
        $finalityGap = Metric $metrics "msc_finality_gap"
        $quorumFailure = Metric $metrics "msc_quorum_failures_total"
        $lastBlockAge = Metric $metrics "msc_consensus_last_block_age_seconds"
        $peers = Metric $metrics "msc_peers_connected"
        $snapshotFailures = Metric $metrics "msc_snapshot_failures_total"
        $replayFailures = Metric $metrics "msc_replay_failures_total"
        $checkpointConflicts = Metric $metrics "msc_checkpoint_conflicts_total"
        $digestMismatch = Metric $metrics "msc_replay_digest_mismatch_total"

        $status = "OK"
        $reasons = New-Object System.Collections.ArrayList
        if ($lag -gt $LagWarnBlocks) {
            $status = "WARN"
            [void]$reasons.Add("lag=$lag")
        }
        if ($peers -lt $MinPeers) {
            $status = "WARN"
            [void]$reasons.Add("peers=$peers")
        }
        if ($finalityGap -gt $FinalityCriticalBlocks) {
            $status = "CRITICAL"
            [void]$reasons.Add("finality_gap=$finalityGap")
        }
        if ($quorumFailure -gt 0) {
            $status = "CRITICAL"
            [void]$reasons.Add("quorum_failure")
        }
        if ($lastBlockAge -gt $NoBlockCriticalSeconds) {
            $status = "CRITICAL"
            [void]$reasons.Add("last_block_age=${lastBlockAge}s")
        }
        if ($snapshotFailures -gt 0) {
            $status = "WARN"
            [void]$reasons.Add("snapshot_failures=$snapshotFailures")
        }
        if ($replayFailures -gt 0) {
            $status = "WARN"
            [void]$reasons.Add("replay_failures=$replayFailures")
        }
        if ($checkpointConflicts -gt 0 -or $digestMismatch -gt 0) {
            $status = "CRITICAL"
            [void]$reasons.Add("checkpoint_or_digest_conflict")
        }
        if ($status -eq "CRITICAL") {
            $failures++
        }

        [void]$rows.Add([PSCustomObject]@{
            Target       = $target
            Status       = $status
            Height       = Metric $metrics "msc_block_height"
            Finalized    = Metric $metrics "msc_finalized_height"
            Lag          = $lag
            FinalityGap  = $finalityGap
            Peers        = $peers
            Quorum       = "$(Metric $metrics "msc_quorum_observed")/$(Metric $metrics "msc_quorum_required")"
            SyncMode     = Metric $metrics "msc_sync_mode"
            LastBlockSec = $lastBlockAge
            Reason       = ($reasons -join ",")
        })
    } catch {
        if (-not $SkipUnreachable) {
            $failures++
        }
        [void]$rows.Add([PSCustomObject]@{
            Target       = $target
            Status       = if ($SkipUnreachable) { "UNREACHABLE" } else { "CRITICAL" }
            Height       = 0
            Finalized    = 0
            Lag          = 0
            FinalityGap  = 0
            Peers        = 0
            Quorum       = "0/0"
            SyncMode     = 0
            LastBlockSec = 0
            Reason       = $_.Exception.Message
        })
    }
}

$rows | Format-Table -AutoSize
if ($failures -gt 0) {
    exit 2
}
exit 0
