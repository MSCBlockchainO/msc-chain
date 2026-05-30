param(
    [string]$TargetBase = "https://mscblockexplorer.in",
    [int]$DurationSeconds = 30,
    [int]$Concurrency = 12,
    [int]$RequestsPerWorker = 80,
    [int]$TimeoutSeconds = 5,
    [int]$MaxBlockAgeSeconds = 60,
    [int]$MaxServerErrorPercent = 2,
    [string]$ReportDir = "runtime-logs",
    [switch]$Heavy,
    [switch]$SkipConnectionFlood
)

$ErrorActionPreference = "Stop"

if ($Heavy) {
    $Concurrency = [Math]::Max($Concurrency, 48)
    $RequestsPerWorker = [Math]::Max($RequestsPerWorker, 250)
}

$TargetBase = $TargetBase.TrimEnd("/")
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$reportPath = Join-Path $ReportDir "ddos-spam-$stamp.json"
New-Item -ItemType Directory -Force -Path $ReportDir | Out-Null

function Get-MscStatus {
    param([string]$Base, [int]$Timeout)
    try {
        return Invoke-RestMethod -Uri "$Base/status" -TimeoutSec $Timeout -Method GET
    } catch {
        throw "status probe failed for $Base/status: $($_.Exception.Message)"
    }
}

function Format-StatusLine {
    param($Status)
    if ($null -eq $Status) { return "status=<nil>" }
    return "height=$($Status.height) finalized=$($Status.finalized_height) peers=$($Status.peers) mempool=$($Status.mempool_depth) mode=$($Status.consensus_detector_mode) block_age=$($Status.last_block_age_seconds)s production=$($Status.block_production_status)/$($Status.block_production_reason)"
}

function Invoke-HttpFloodPhase {
    param(
        [string]$Name,
        [string]$Method,
        [string]$Path,
        [string]$Body,
        [string]$ContentType = "application/json"
    )

    $uri = "$TargetBase$Path"
    Write-Host "== $Name -> $Method $uri =="
    $jobs = @()
    for ($i = 0; $i -lt $Concurrency; $i++) {
        $jobs += Start-Job -ArgumentList $Name, $Method, $uri, $Body, $ContentType, $RequestsPerWorker, $DurationSeconds, $TimeoutSeconds -ScriptBlock {
            param($Name, $Method, $Uri, $Body, $ContentType, $RequestsPerWorker, $DurationSeconds, $TimeoutSeconds)
            Add-Type -AssemblyName System.Net.Http
            [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
            $client = [System.Net.Http.HttpClient]::new()
            $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
            $deadline = [DateTime]::UtcNow.AddSeconds($DurationSeconds)
            $stats = [ordered]@{
                phase = $Name
                ok2xx = 0
                rejected4xx = 0
                rateLimited429 = 0
                server5xx = 0
                networkErrors = 0
                requests = 0
                elapsedMs = 0
            }
            for ($n = 0; $n -lt $RequestsPerWorker -and [DateTime]::UtcNow -lt $deadline; $n++) {
                $sw = [Diagnostics.Stopwatch]::StartNew()
                try {
                    if ($Method -eq "GET") {
                        $resp = $client.GetAsync($Uri).GetAwaiter().GetResult()
                    } else {
                        $content = [System.Net.Http.StringContent]::new($Body, [Text.Encoding]::UTF8, $ContentType)
                        $resp = $client.PostAsync($Uri, $content).GetAwaiter().GetResult()
                    }
                    $code = [int]$resp.StatusCode
                    if ($code -eq 429) {
                        $stats.rateLimited429++
                    } elseif ($code -ge 500) {
                        $stats.server5xx++
                    } elseif ($code -ge 400) {
                        $stats.rejected4xx++
                    } elseif ($code -ge 200) {
                        $stats.ok2xx++
                    }
                    $resp.Dispose()
                } catch {
                    $stats.networkErrors++
                } finally {
                    $sw.Stop()
                    $stats.requests++
                    $stats.elapsedMs += $sw.ElapsedMilliseconds
                }
            }
            $client.Dispose()
            [pscustomobject]$stats
        }
    }

    $rows = $jobs | Wait-Job | Receive-Job
    $jobs | Remove-Job -Force
    $total = [ordered]@{
        phase = $Name
        requests = 0
        ok2xx = 0
        rejected4xx = 0
        rateLimited429 = 0
        server5xx = 0
        networkErrors = 0
        avgMs = 0
    }
    $elapsed = 0
    foreach ($row in $rows) {
        $total.requests += [int]$row.requests
        $total.ok2xx += [int]$row.ok2xx
        $total.rejected4xx += [int]$row.rejected4xx
        $total.rateLimited429 += [int]$row.rateLimited429
        $total.server5xx += [int]$row.server5xx
        $total.networkErrors += [int]$row.networkErrors
        $elapsed += [int64]$row.elapsedMs
    }
    if ($total.requests -gt 0) {
        $total.avgMs = [Math]::Round($elapsed / $total.requests, 2)
    }
    $line = "requests=$($total.requests) 2xx=$($total.ok2xx) 4xx=$($total.rejected4xx) 429=$($total.rateLimited429) 5xx=$($total.server5xx) neterr=$($total.networkErrors) avg_ms=$($total.avgMs)"
    Write-Host $line
    return [pscustomobject]$total
}

function Invoke-ConnectionFloodPhase {
    param([string]$Base)
    $target = [Uri]$Base
    $port = if ($target.Port -gt 0) { $target.Port } elseif ($target.Scheme -eq "https") { 443 } else { 80 }
    $hostName = $target.Host
    Write-Host "== connection_flood -> tcp $hostName`:$port =="
    $jobs = @()
    for ($i = 0; $i -lt $Concurrency; $i++) {
        $jobs += Start-Job -ArgumentList $hostName, $port, $RequestsPerWorker, $DurationSeconds, $TimeoutSeconds -ScriptBlock {
            param($HostName, $Port, $RequestsPerWorker, $DurationSeconds, $TimeoutSeconds)
            $deadline = [DateTime]::UtcNow.AddSeconds($DurationSeconds)
            $ok = 0
            $err = 0
            for ($n = 0; $n -lt $RequestsPerWorker -and [DateTime]::UtcNow -lt $deadline; $n++) {
                $client = [Net.Sockets.TcpClient]::new()
                try {
                    $async = $client.BeginConnect($HostName, $Port, $null, $null)
                    if ($async.AsyncWaitHandle.WaitOne([TimeSpan]::FromSeconds($TimeoutSeconds))) {
                        $client.EndConnect($async)
                        $ok++
                    } else {
                        $err++
                    }
                } catch {
                    $err++
                } finally {
                    $client.Close()
                }
            }
            [pscustomobject]@{ phase = "connection_flood"; requests = ($ok + $err); ok2xx = $ok; rejected4xx = 0; rateLimited429 = 0; server5xx = 0; networkErrors = $err; avgMs = 0 }
        }
    }
    $rows = $jobs | Wait-Job | Receive-Job
    $jobs | Remove-Job -Force
    $total = [ordered]@{ phase = "connection_flood"; requests = 0; ok2xx = 0; rejected4xx = 0; rateLimited429 = 0; server5xx = 0; networkErrors = 0; avgMs = 0 }
    foreach ($row in $rows) {
        $total.requests += [int]$row.requests
        $total.ok2xx += [int]$row.ok2xx
        $total.networkErrors += [int]$row.networkErrors
    }
    Write-Host "requests=$($total.requests) connects=$($total.ok2xx) neterr=$($total.networkErrors)"
    return [pscustomobject]$total
}

Write-Host "MSC public full-node DDoS/spam smoke"
Write-Host "target=$TargetBase duration=${DurationSeconds}s concurrency=$Concurrency requests_per_worker=$RequestsPerWorker"

$before = Get-MscStatus -Base $TargetBase -Timeout $TimeoutSeconds
Write-Host "before: $(Format-StatusLine $before)"

$invalidRpc = '{"jsonrpc":"2.0","method":"dtl_submit","params":[{"type":"INVALID_SPAM","from":"MSC_BAD","amount":-1}],"id":1}'
$invalidTx = '{"from":"MSC_BAD","to":"MSC_BAD","amount":-1,"coin":"MSC","signature":"bad","public_key":"bad"}'

$results = @()
$results += Invoke-HttpFloodPhase -Name "status_flood" -Method "GET" -Path "/status" -Body ""
$results += Invoke-HttpFloodPhase -Name "block_request_flood" -Method "GET" -Path "/v1/blocks?limit=25" -Body ""
$results += Invoke-HttpFloodPhase -Name "invalid_jsonrpc_tx_flood" -Method "POST" -Path "/rpc" -Body $invalidRpc
$results += Invoke-HttpFloodPhase -Name "invalid_submit_tx_flood" -Method "POST" -Path "/submitTx" -Body $invalidTx
if (-not $SkipConnectionFlood) {
    $results += Invoke-ConnectionFloodPhase -Base $TargetBase
}

Start-Sleep -Seconds 3
$after = Get-MscStatus -Base $TargetBase -Timeout $TimeoutSeconds
Write-Host "after:  $(Format-StatusLine $after)"

$serverErrors = ($results | Measure-Object -Property server5xx -Sum).Sum
$networkErrors = ($results | Measure-Object -Property networkErrors -Sum).Sum
$requests = ($results | Measure-Object -Property requests -Sum).Sum
$serverErrorPercent = if ($requests -gt 0) { [Math]::Round(($serverErrors * 100.0) / $requests, 2) } else { 0 }

$report = [ordered]@{
    target = $TargetBase
    started_at = $stamp
    duration_seconds = $DurationSeconds
    concurrency = $Concurrency
    requests_per_worker = $RequestsPerWorker
    before = $before
    phases = $results
    after = $after
    totals = [ordered]@{
        requests = $requests
        server5xx = $serverErrors
        network_errors = $networkErrors
        server_error_percent = $serverErrorPercent
    }
}

$report | ConvertTo-Json -Depth 12 | Set-Content -Path $reportPath -Encoding UTF8
Write-Host "report=$reportPath"

if ([int]$after.last_block_age_seconds -gt $MaxBlockAgeSeconds) {
    throw "FAIL: last block age $($after.last_block_age_seconds)s exceeded $MaxBlockAgeSeconds after flood"
}
if ([uint64]$after.finalized_height -lt [uint64]$before.finalized_height) {
    throw "FAIL: finalized height moved backward"
}
if ($serverErrorPercent -gt $MaxServerErrorPercent) {
    throw "FAIL: server 5xx rate $serverErrorPercent% exceeded $MaxServerErrorPercent%"
}

Write-Host "PASS: flood rejected/served safely; chain stayed live."
