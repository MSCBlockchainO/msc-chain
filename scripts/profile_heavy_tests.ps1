param(
    [string]$Regex = "Test(Storage|Sync|Snapshot|Consensus|Promotion|Hybrid|ValidatorDiversity|HashBlock|LongReplay)",
    [int]$PerTestTimeoutSeconds = 60,
    [string]$Package = ".",
    [string]$OutputDir = "",
    [switch]$RunLongTests,
    [int]$SlowSeconds = 10
)

$ErrorActionPreference = "Stop"

if (-not $OutputDir) {
    $OutputDir = Join-Path $env:TEMP "msc-test-profile"
}
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$reportPath = Join-Path $OutputDir "heavy-tests-$timestamp.json"

$listOutput = & go test $Package -list $Regex 2>&1
if ($LASTEXITCODE -ne 0) {
    $listOutput | ForEach-Object { Write-Host $_ }
    throw "go test -list failed"
}

$tests = @()
foreach ($line in $listOutput) {
    $name = ($line -as [string]).Trim()
    if ($name -match "^Test") {
        $tests += $name
    }
}

if ($tests.Count -eq 0) {
    Write-Host "No tests matched regex: $Regex"
    @() | ConvertTo-Json | Set-Content -Path $reportPath -Encoding UTF8
    Write-Host "Report: $reportPath"
    exit 0
}

$oldRunLong = $env:MSC_RUN_LONG_TESTS
if ($RunLongTests) {
    $env:MSC_RUN_LONG_TESTS = "1"
}

$results = New-Object System.Collections.Generic.List[object]
$hasFailure = $false

try {
    foreach ($test in $tests) {
        $timeout = "${PerTestTimeoutSeconds}s"
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        $output = & go test $Package -run "^$test$" -count=1 -timeout $timeout -json 2>&1
        $exitCode = $LASTEXITCODE
        $sw.Stop()

        $text = ($output | Out-String)
        $status = "passed"
        if ($text -match '"Action":"skip"') {
            $status = "skipped"
        }
        if ($exitCode -ne 0) {
            $status = "failed"
            $hasFailure = $true
            if ($text -match "panic: test timed out|test timed out after|signal: killed|deadline exceeded") {
                $status = "timed_out"
            }
        }
        if ($status -eq "passed" -and $sw.Elapsed.TotalSeconds -ge $SlowSeconds) {
            $status = "slow"
        }

        $tail = (($text -split "`r?`n") | Select-Object -Last 20) -join "`n"
        $result = [pscustomobject]@{
            test = $test
            status = $status
            seconds = [math]::Round($sw.Elapsed.TotalSeconds, 3)
            exit_code = $exitCode
            timeout_seconds = $PerTestTimeoutSeconds
            tail = $tail
        }
        $results.Add($result)
        "{0,-72} {1,-10} {2,8:N3}s" -f $test, $status, $result.seconds | Write-Host
    }
}
finally {
    $env:MSC_RUN_LONG_TESTS = $oldRunLong
}

$results | ConvertTo-Json -Depth 4 | Set-Content -Path $reportPath -Encoding UTF8
Write-Host "Report: $reportPath"

$summary = $results | Group-Object status | Sort-Object Name | ForEach-Object {
    "{0}={1}" -f $_.Name, $_.Count
}
Write-Host ("Summary: " + ($summary -join " "))

if ($hasFailure) {
    exit 1
}
