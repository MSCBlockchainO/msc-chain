param(
    [string]$Target = "https://mscblockexplorer.in",
    [int]$DurationSeconds = 180,
    [int]$IntervalSeconds = 2,
    [int]$MaxErrors = 3,
    [switch]$FailOnAnyDegraded
)

$ErrorActionPreference = "Stop"

if ($DurationSeconds -lt 1) {
    throw "DurationSeconds must be >= 1"
}
if ($IntervalSeconds -lt 1) {
    throw "IntervalSeconds must be >= 1"
}
if ($MaxErrors -lt 0) {
    throw "MaxErrors must be >= 0"
}

$base = $Target.TrimEnd("/")
$deadline = (Get-Date).AddSeconds($DurationSeconds)
$samples = 0
$falseDegraded = 0
$realDegraded = 0
$errors = 0
$lastHeight = 0

Write-Host "Watching consensus mode at $base for ${DurationSeconds}s..."

while ((Get-Date) -lt $deadline) {
    try {
        $mode = Invoke-RestMethod -Uri "$base/consensus/mode" -TimeoutSec 10
    } catch {
        $errors++
        Write-Warning "sample_error=$errors $($_.Exception.Message)"
        if ($errors -gt $MaxErrors) {
            throw "Consensus mode watch exceeded MaxErrors=$MaxErrors"
        }
        Start-Sleep -Seconds $IntervalSeconds
        continue
    }
    $samples++
    $height = [uint64]$mode.height
    if ($height -gt $lastHeight) {
        $lastHeight = $height
    }

    $line = "sample=$samples mode=$($mode.mode) reason=$($mode.reason) h=$($mode.height) f=$($mode.finalized_height) lag=$($mode.finality_lag) age=$($mode.last_finality_seconds)s peers=$($mode.peer_count) maxlag=$($mode.max_validator_lag)"
    Write-Host $line

    if ($mode.mode -eq "DEGRADED") {
        $threshold = [int64]$mode.degraded_after_seconds
        if ($threshold -le 0) {
            $threshold = 12
        }
        $looksHealthy = ([int64]$mode.finality_lag -eq 0) -and
            ([int64]$mode.max_validator_lag -eq 0) -and
            ([int64]$mode.last_finality_seconds -lt $threshold) -and
            ([int64]$mode.active_validators -gt [int64]$mode.quorum) -and
            ([int64]$mode.peer_count -ge 3)

        if ($looksHealthy) {
            $falseDegraded++
            Write-Warning "False DEGRADED candidate detected: $line"
        } else {
            $realDegraded++
            Write-Warning "Real DEGRADED signal detected: $line"
        }
    }

    Start-Sleep -Seconds $IntervalSeconds
}

if ($samples -eq 0) {
    throw "No samples collected"
}
if ($lastHeight -eq 0) {
    throw "No positive height observed"
}
if ($falseDegraded -gt 0) {
    throw "Consensus detector produced $falseDegraded false DEGRADED sample(s)"
}
if ($FailOnAnyDegraded -and $realDegraded -gt 0) {
    throw "Consensus detector produced $realDegraded real DEGRADED sample(s)"
}

Write-Host "PASS: samples=$samples errors=$errors false_degraded=$falseDegraded real_degraded=$realDegraded last_height=$lastHeight"
