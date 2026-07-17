param(
    [int[]]$Levels = @(50, 100, 250, 500, 1000, 2000, 5000),
    [int]$DurationSeconds = 60,
    [int]$SampleSeconds = 5,
    [int]$GateSamples = 3,
    [int]$MaxMempool = 1000,
    [int]$MaxBlockAgeSeconds = 15,
    [int]$MaxLagBlocks = 2,
    [int]$MaxUnhealthySeconds = 45,
    [int]$FaucetRps = 0,
    [int]$MaxPendingFaucets = 20,
    [int]$SubmitWorkers = 0,
    [string]$RemoteWorkDir = "msc-chain",
    [string]$LoadHostName = "B",
    [string[]]$LoadHostNames = @(),
    [switch]$DistributedLoad,
    [string]$LoadRpc = "http://127.0.0.1:26657",
    [string]$LoadgenPath = "./live_tps_loadgen.linux",
    [string]$AuthTokenFile = "",
    [string]$PreparedStatePrefix = "",
    [switch]$Run
)

$ErrorActionPreference = "Stop"

$OldKey = "C:\Users\Mohammad Talha\Downloads\msc-key.pem"
$TempKeys = Join-Path $env:TEMP "msc-deploy-keys"
$Nodes = @(
    [pscustomobject]@{ Name = "B"; Target = "ubuntu@54.80.4.133"; Key = $OldKey; Port = 26657 },
    [pscustomobject]@{ Name = "C"; Target = "ec2-user@98.90.205.156"; Key = $OldKey; Port = 26658 },
    [pscustomobject]@{ Name = "D"; Target = "ec2-user@3.88.214.207"; Key = $OldKey; Port = 26659 },
    [pscustomobject]@{ Name = "E"; Target = "ec2-user@34.201.64.103"; Key = $OldKey; Port = 26660 },
    [pscustomobject]@{ Name = "F"; Target = "ubuntu@3.109.153.171"; Key = (Join-Path $TempKeys "testnet.pem"); Port = 26657 },
    [pscustomobject]@{ Name = "G"; Target = "ubuntu@47.129.174.99"; Key = (Join-Path $TempKeys "Nodeskey.pem"); Port = 26657 }
)

function Invoke-NodeSsh {
    param(
        [Parameter(Mandatory = $true)][pscustomobject]$Node,
        [Parameter(Mandatory = $true)][string]$Command
    )

    $normalized = $Command -replace "`r", ""
    $normalized | ssh -i $Node.Key -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=8 $Node.Target "tr -d '\r' | bash -s" 2>$null
}

function Get-NodeSample {
    param([Parameter(Mandatory = $true)][pscustomobject]$Node)

    $remote = @"
status=`$(curl -sf --max-time 4 http://127.0.0.1:$($Node.Port)/status 2>/dev/null || true)
proc=`$(ps -eo pid,pcpu,pmem,rss,args | awk '/msc-node/ && !/awk/ {print; exit}' || true)
disk=`$(df -Pk . 2>/dev/null | awk 'NR==2 {print `$4}' || true)
net=`$(cat /proc/net/dev 2>/dev/null | awk 'NR>2 {rx+=`$2; tx+=`$10} END {printf "%s %s", rx, tx}' || true)
diskio=`$(cat /proc/diskstats 2>/dev/null | awk '`$3 !~ /^(loop|ram)/ {read+=`$6*512; write+=`$10*512} END {printf "%s %s", read+0, write+0}' || true)
metrics=`$(curl -sf --max-time 4 http://127.0.0.1:$($Node.Port)/metrics 2>/dev/null || true)
fork=`$(printf '%s\n' "`$metrics" | awk '/^msc_map_fork_blocks_total/ {print `$2; exit}' || true)
fork_heights=`$(printf '%s\n' "`$metrics" | awk '/^msc_map_fork_heights/ {print `$2; exit}' || true)
printf '__STATUS__%s\n' "`$status"
printf '__PROC__%s\n' "`$proc"
printf '__DISK__%s\n' "`$disk"
printf '__NET__%s\n' "`$net"
printf '__DISKIO__%s\n' "`$diskio"
printf '__FORK__%s %s\n' "`$fork" "`$fork_heights"
"@
    $raw = Invoke-NodeSsh -Node $Node -Command $remote
    $status = $null
    $proc = ""
    $diskFreeKb = $null
    $rxBytes = $null
    $txBytes = $null
    $diskReadBytes = $null
    $diskWriteBytes = $null
    $forkBlocks = 0
    $forkHeights = 0
    foreach ($line in @($raw -split "`n")) {
        if ($line.StartsWith("__STATUS__")) {
            $statusRaw = $line.Substring(10).Trim()
            if ($statusRaw) {
                try { $status = $statusRaw | ConvertFrom-Json } catch { $status = $null }
            }
        } elseif ($line.StartsWith("__PROC__")) {
            $proc = $line.Substring(8).Trim()
        } elseif ($line.StartsWith("__DISK__")) {
            $diskText = $line.Substring(8).Trim()
            if ($diskText) { $diskFreeKb = [int64]$diskText }
        } elseif ($line.StartsWith("__NET__")) {
            $parts = $line.Substring(7).Trim() -split "\s+"
            if ($parts.Count -ge 2) {
                $rxBytes = [int64]$parts[0]
                $txBytes = [int64]$parts[1]
            }
        } elseif ($line.StartsWith("__DISKIO__")) {
            $parts = $line.Substring(10).Trim() -split "\s+"
            if ($parts.Count -ge 2) {
                $diskReadBytes = [int64]$parts[0]
                $diskWriteBytes = [int64]$parts[1]
            }
        } elseif ($line.StartsWith("__FORK__")) {
            $parts = $line.Substring(8).Trim() -split "\s+"
            if ($parts.Count -ge 1 -and $parts[0]) { $forkBlocks = [int]([double]$parts[0]) }
            if ($parts.Count -ge 2 -and $parts[1]) { $forkHeights = [int]([double]$parts[1]) }
        }
    }

    $procPid = ""
    $cpu = 0.0
    $memPct = 0.0
    $rssKb = 0
    if ($proc) {
        $parts = $proc -split "\s+"
        if ($parts.Count -ge 4) {
            $procPid = $parts[0]
            $cpu = [double]$parts[1]
            $memPct = [double]$parts[2]
            $rssKb = [int64]$parts[3]
        }
    }

    [pscustomobject]@{
        Name = $Node.Name
        Target = $Node.Target
        Port = $Node.Port
        Status = $status
        Pid = $procPid
        CpuPct = $cpu
        MemPct = $memPct
        RssKb = $rssKb
        DiskFreeKb = $diskFreeKb
        RxBytes = $rxBytes
        TxBytes = $txBytes
        DiskReadBytes = $diskReadBytes
        DiskWriteBytes = $diskWriteBytes
        ForkBlocks = $forkBlocks
        ForkHeights = $forkHeights
        CapturedAt = (Get-Date).ToUniversalTime().ToString("o")
    }
}

function Get-ClusterSamples {
    $samples = @()
    foreach ($node in $Nodes) {
        $samples += Get-NodeSample -Node $node
    }
    return $samples
}

function Test-SamplesHealthy {
    param([Parameter(Mandatory = $true)]$Samples)

    $reasons = New-Object System.Collections.Generic.List[string]
    foreach ($sample in $Samples) {
        $s = $sample.Status
        if ($null -eq $s) {
            $reasons.Add("$($sample.Name):status_down")
            continue
        }
        $lag = 0
        if ($null -ne $s.network_lag_blocks) { $lag = [int]$s.network_lag_blocks }
        $age = [int]$s.last_block_age_seconds
        $minorCatchup = $s.syncing -and $lag -le $MaxLagBlocks -and $age -le $MaxBlockAgeSeconds -and $s.block_production_status -ne "stalled"
        if (-not $s.ready -and -not $minorCatchup) { $reasons.Add("$($sample.Name):not_ready") }
        if ($s.syncing -and -not $minorCatchup) { $reasons.Add("$($sample.Name):syncing") }
        if ($s.network_health -and $s.network_health -ne "healthy" -and -not $minorCatchup) { $reasons.Add("$($sample.Name):health_$($s.network_health)") }
        if ([int]$s.mempool_depth -gt $MaxMempool) { $reasons.Add("$($sample.Name):mempool_$($s.mempool_depth)") }
        if ($age -gt $MaxBlockAgeSeconds) { $reasons.Add("$($sample.Name):age_$($s.last_block_age_seconds)s") }
        if ($lag -gt $MaxLagBlocks) { $reasons.Add("$($sample.Name):lag_$lag") }
    }
    [pscustomobject]@{ Ok = $reasons.Count -eq 0; Reason = ($reasons -join ",") }
}

function Save-Json {
    param(
        [Parameter(Mandatory = $true)]$Path,
        [Parameter(Mandatory = $true)]$Value
    )
    $dir = Split-Path -Parent $Path
    if ($dir) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    $Value | ConvertTo-Json -Depth 12 | Set-Content -Path $Path -Encoding UTF8
}

function Get-StatusNumber {
    param(
        [Parameter(Mandatory = $true)]$Sample,
        [Parameter(Mandatory = $true)][string[]]$Names,
        [double]$Default = 0
    )
    if ($null -eq $Sample -or $null -eq $Sample.Status) { return $Default }
    foreach ($name in $Names) {
        $property = $Sample.Status.PSObject.Properties[$name]
        if ($null -ne $property -and $null -ne $property.Value -and "$($property.Value)" -ne "") {
            return [double]$property.Value
        }
    }
    return $Default
}

function Measure-NumberSummary {
    param([double[]]$Values)
    $clean = @($Values | Where-Object { $null -ne $_ })
    if ($clean.Count -eq 0) {
        return [pscustomobject]@{ min = 0; max = 0; avg = 0 }
    }
    $measure = $clean | Measure-Object -Minimum -Maximum -Average
    return [pscustomobject]@{
        min = [Math]::Round([double]$measure.Minimum, 3)
        max = [Math]::Round([double]$measure.Maximum, 3)
        avg = [Math]::Round([double]$measure.Average, 3)
    }
}

function Get-SampleDeltaSum {
    param(
        [Parameter(Mandatory = $true)]$Samples,
        [Parameter(Mandatory = $true)][string]$Property
    )
    $total = [int64]0
    foreach ($group in ($Samples | Where-Object { $null -ne $_.$Property } | Group-Object -Property Name)) {
        $values = @($group.Group | ForEach-Object { [int64]$_.$Property })
        if ($values.Count -lt 2) { continue }
        $min = ($values | Measure-Object -Minimum).Minimum
        $max = ($values | Measure-Object -Maximum).Maximum
        if ($max -gt $min) {
            $total += [int64]($max - $min)
        }
    }
    return $total
}

function New-LevelMetrics {
    param(
        [Parameter(Mandatory = $true)][int]$Level,
        [Parameter(Mandatory = $true)][datetime]$StartedAt,
        [Parameter(Mandatory = $true)][datetime]$EndedAt,
        [Parameter(Mandatory = $true)]$Samples,
        [Parameter(Mandatory = $true)][int64]$TargetTotal,
        [Parameter(Mandatory = $true)][int64]$AcceptedTotal,
        [Parameter(Mandatory = $true)][int64]$TransferAccepted,
        [Parameter(Mandatory = $true)][int64]$FaucetAccepted,
        [Parameter(Mandatory = $true)][int64]$TransferFailed
    )

    $duration = [Math]::Max(0.001, ($EndedAt - $StartedAt).TotalSeconds)
    $cpu = Measure-NumberSummary -Values @($Samples | ForEach-Object { [double]$_.CpuPct })
    $memPct = Measure-NumberSummary -Values @($Samples | ForEach-Object { [double]$_.MemPct })
    $rss = Measure-NumberSummary -Values @($Samples | ForEach-Object { [double]$_.RssKb })
    $finalitySeconds = Measure-NumberSummary -Values @($Samples | ForEach-Object { Get-StatusNumber -Sample $_ -Names @("consensus_detector_last_finality_seconds", "last_finality_seconds") })
    $blockAge = Measure-NumberSummary -Values @($Samples | ForEach-Object { Get-StatusNumber -Sample $_ -Names @("last_block_age_seconds") })
    $networkLag = Measure-NumberSummary -Values @($Samples | ForEach-Object { Get-StatusNumber -Sample $_ -Names @("network_lag_blocks", "sync_lag_blocks") })
    $height = Measure-NumberSummary -Values @($Samples | ForEach-Object { Get-StatusNumber -Sample $_ -Names @("height") })
    $finalized = Measure-NumberSummary -Values @($Samples | ForEach-Object { Get-StatusNumber -Sample $_ -Names @("finalized_height") })
    $finalityGap = Measure-NumberSummary -Values @($Samples | ForEach-Object {
        $h = Get-StatusNumber -Sample $_ -Names @("height")
        $f = Get-StatusNumber -Sample $_ -Names @("finalized_height")
        [Math]::Max(0, $h - $f)
    })
    $diskFree = Measure-NumberSummary -Values @($Samples | Where-Object { $null -ne $_.DiskFreeKb } | ForEach-Object { [double]$_.DiskFreeKb })
    $forkBlocks = Measure-NumberSummary -Values @($Samples | ForEach-Object { [double]$_.ForkBlocks })
    $forkHeights = Measure-NumberSummary -Values @($Samples | ForEach-Object { [double]$_.ForkHeights })
    $failureRate = 0.0
    if (($TransferAccepted + $TransferFailed) -gt 0) {
        $failureRate = [double]$TransferFailed / [double]($TransferAccepted + $TransferFailed)
    }

    [pscustomobject][ordered]@{
        duration_seconds = [Math]::Round($duration, 3)
        tps = [pscustomobject][ordered]@{
            target = $Level
            observed = [Math]::Round([double]$TransferAccepted / $duration, 3)
            accepted_observed = [Math]::Round([double]$AcceptedTotal / $duration, 3)
            target_total = $TargetTotal
            accepted_total = $AcceptedTotal
            transfer_accepted = $TransferAccepted
            faucet_accepted = $FaucetAccepted
        }
        latency = [pscustomobject][ordered]@{
            finality_seconds = $finalitySeconds
            last_block_age_seconds = $blockAge
        }
        cpu = [pscustomobject][ordered]@{
            percent = $cpu
        }
        ram = [pscustomobject][ordered]@{
            rss_kb = $rss
            percent = $memPct
        }
        disk_io = [pscustomobject][ordered]@{
            read_bytes_delta = Get-SampleDeltaSum -Samples $Samples -Property "DiskReadBytes"
            write_bytes_delta = Get-SampleDeltaSum -Samples $Samples -Property "DiskWriteBytes"
            free_kb = $diskFree
            free_kb_delta = Get-SampleDeltaSum -Samples $Samples -Property "DiskFreeKb"
        }
        network = [pscustomobject][ordered]@{
            rx_bytes_delta = Get-SampleDeltaSum -Samples $Samples -Property "RxBytes"
            tx_bytes_delta = Get-SampleDeltaSum -Samples $Samples -Property "TxBytes"
            lag_blocks = $networkLag
        }
        finality = [pscustomobject][ordered]@{
            height = $height
            finalized_height = $finalized
            gap_blocks = $finalityGap
            seconds = $finalitySeconds
        }
        failed_tx = [pscustomobject][ordered]@{
            transfer_failed = $TransferFailed
            failure_rate = [Math]::Round($failureRate, 6)
        }
        fork_count = [pscustomobject][ordered]@{
            blocks = $forkBlocks
            heights = $forkHeights
        }
        consensus_delay = [pscustomobject][ordered]@{
            finality_seconds = $finalitySeconds
            network_lag_blocks = $networkLag
            finality_gap_blocks = $finalityGap
        }
    }
}

function Get-RemoteHome {
    param([Parameter(Mandatory = $true)][pscustomobject]$Node)
    $user = (($Node.Target -split "@")[0]).Trim()
    if (-not $user) { return "~" }
    return "/home/$user"
}

function Get-LoadNodes {
    if ($DistributedLoad) {
        $names = @($LoadHostNames | Where-Object { $_ -and $_.Trim() } | ForEach-Object { $_.Trim() })
        if ($names.Count -eq 0) {
            return @($Nodes)
        }
        $selected = @()
        foreach ($name in $names) {
            $node = $Nodes | Where-Object { $_.Name -eq $name } | Select-Object -First 1
            if ($null -eq $node) { throw "unknown load host: $name" }
            $selected += $node
        }
        return $selected
    }
    $loadNode = $Nodes | Where-Object { $_.Name -eq $LoadHostName } | Select-Object -First 1
    if ($null -eq $loadNode) { throw "unknown load host: $LoadHostName" }
    return @($loadNode)
}

function Start-LoadgenOnNode {
    param(
        [Parameter(Mandatory = $true)][pscustomobject]$Node,
        [Parameter(Mandatory = $true)][int]$Level,
        [Parameter(Mandatory = $true)][double]$NodeTPS,
        [Parameter(Mandatory = $true)][int]$TargetTotal
    )

    $stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
    $state = "runtime-logs/stress-${Level}tps-$($Node.Name)-${stamp}.json"
    $report = "$state.report.json"
    $log = "runtime-logs/stress-${Level}tps-$($Node.Name)-${stamp}.log"
    $accountPool = [Math]::Max(100, [int][Math]::Ceiling($NodeTPS * 20.0))
    $warmupReady = [Math]::Min($accountPool, [Math]::Max([int][Math]::Ceiling($NodeTPS / 2.0), [int][Math]::Ceiling($TargetTotal / 40.0)))
    $levelFaucetRps = $FaucetRps
    if ($levelFaucetRps -le 0) {
        $levelFaucetRps = [Math]::Min(100, [Math]::Max(2, [int][Math]::Ceiling($NodeTPS / 10.0)))
    }
    $levelWorkers = $SubmitWorkers
    if ($levelWorkers -le 0) {
        $levelWorkers = [Math]::Min(512, [Math]::Max(2, [int][Math]::Ceiling($NodeTPS / 5.0)))
    }
    $rpc = $LoadRpc
    if ($DistributedLoad) {
        $rpc = "http://127.0.0.1:$($Node.Port)"
    }
    $authPath = $AuthTokenFile.Trim()
    if (-not $authPath) {
        $authPath = "$(Get-RemoteHome -Node $Node)/.msc/production-v2/loadtest.token"
    }
    $authArg = ""
    if ($authPath) {
        $authArg = "-auth-token-file '$authPath'"
    }
    $nodeTpsText = $NodeTPS.ToString([Globalization.CultureInfo]::InvariantCulture)
    $preparedSource = ""
    $warmupArgs = "-warmup-ready-accounts $warmupReady -warmup-timeout 180s"
    if ($PreparedStatePrefix.Trim()) {
        $preparedSource = "$($PreparedStatePrefix.Trim())-$($Node.Name).json"
        $warmupReady = 0
        $warmupArgs = "-warmup-ready-accounts 0"
    }
    $stateInit = ""
    if ($preparedSource) {
        $stateInit = @"
python3 - '$preparedSource' '$state' '$TargetTotal' <<'PY'
import copy, datetime, json, sys
source, target, target_total = sys.argv[1], sys.argv[2], int(sys.argv[3])
with open(source, 'r', encoding='utf-8') as fh:
    state = json.load(fh)
accounts = [
    copy.deepcopy(account)
    for account in (state.get('accounts') or [])
    if not account.get('retired') and int(account.get('budget') or 0) > 0
]
if len(accounts) < 2:
    raise SystemExit(f"prepared state {source} has only {len(accounts)} funded accounts")
for account in accounts:
    account['in_flight'] = False
state.update({
    'target_total': target_total,
    'total_accepted': 0,
    'faucet_accepted': 0,
    'transfer_accepted': 0,
    'transfer_failed': 0,
    'transfer_failure_reasons': {},
    'retired_accounts': 0,
    'accounts': accounts,
    'pending_faucets': [],
    'last_tx_id': '',
    'last_submit_error': '',
    'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
})
with open(target, 'w', encoding='utf-8') as fh:
    json.dump(state, fh, separators=(',', ':'))
PY
"@
    }
    $remote = @"
set -e
cd $RemoteWorkDir
mkdir -p runtime-logs
$stateInit
nohup $LoadgenPath -rpc '$rpc' -target-total $TargetTotal -target-transfers-only -target-tps $nodeTpsText -submit-workers $levelWorkers -account-pool $accountPool $warmupArgs -max-pending-faucets $MaxPendingFaucets -runtime-faucet=false -faucet-rps $levelFaucetRps -max-mempool $MaxMempool -max-block-age $MaxBlockAgeSeconds -max-unhealthy ${MaxUnhealthySeconds}s -status-every ${SampleSeconds}s -state '$state' -report '$report' $authArg > '$log' 2>&1 &
printf '%s\n' `$!
printf '%s\n' '$state'
printf '%s\n' '$report'
printf '%s\n' '$log'
"@
    $raw = Invoke-NodeSsh -Node $Node -Command $remote
    $lines = @($raw -split "`n" | Where-Object { $_.Trim() })
    if ($lines.Count -lt 4) { throw "loadgen did not start: $raw" }
    [pscustomobject]@{ Node = $Node; Pid = $lines[0].Trim(); State = $lines[1].Trim(); Report = $lines[2].Trim(); Log = $lines[3].Trim(); TargetTotal = $TargetTotal; TargetTPS = $NodeTPS; FaucetRps = $levelFaucetRps; SubmitWorkers = $levelWorkers; WarmupReady = $warmupReady }
}

function Start-Loadgen {
    param([Parameter(Mandatory = $true)][int]$Level)

    $loadNodes = @(Get-LoadNodes)
    if ($loadNodes.Count -eq 0) { throw "no load hosts selected" }
    $targetTotal = [Math]::Max(1, $Level * $DurationSeconds)
    $loads = @()
    for ($i = 0; $i -lt $loadNodes.Count; $i++) {
        $nodeTarget = [int][Math]::Floor($targetTotal / $loadNodes.Count)
        if ($i -lt ($targetTotal % $loadNodes.Count)) { $nodeTarget++ }
        if ($nodeTarget -le 0) { continue }
        $nodeTPS = [double]$Level / [double]$loadNodes.Count
        $loads += Start-LoadgenOnNode -Node $loadNodes[$i] -Level $Level -NodeTPS $nodeTPS -TargetTotal $nodeTarget
    }
    return $loads
}

function Stop-Loadgen {
    param([Parameter(Mandatory = $true)]$Load)
    foreach ($item in @($Load)) {
        if ($null -eq $item) { continue }
    $remote = @"
kill $($item.Pid) 2>/dev/null || true
sleep 1
kill -9 $($item.Pid) 2>/dev/null || true
"@
        Invoke-NodeSsh -Node $item.Node -Command $remote | Out-Null
    }
}

function Test-LoadgensRunning {
    param([Parameter(Mandatory = $true)]$Loads)
    foreach ($load in @($Loads)) {
        $stillRunning = Invoke-NodeSsh -Node $load.Node -Command "kill -0 $($load.Pid) 2>/dev/null && echo running || echo done"
        if (($stillRunning -join "").Trim() -eq "running") { return $true }
    }
    return $false
}

function Get-LoadgenRemoteSummary {
    param([Parameter(Mandatory = $true)]$Loads)

    $aggregate = [ordered]@{
        remote_text = ""
        accepted_total = 0
        transfer_accepted = 0
        faucet_accepted = 0
        transfer_failed = 0
        target_missed = $false
        parse_failed = $false
    }
    foreach ($load in @($Loads)) {
        $remoteSummary = Invoke-NodeSsh -Node $load.Node -Command @"
cd $RemoteWorkDir
printf '\n__LOADGEN_NODE__ $($load.Node.Name)\n'
tail -n 80 '$($load.Log)' 2>/dev/null || true
printf '\n__REPORT__\n'
cat '$($load.Report)' 2>/dev/null || true
printf '\n__STATE_SUMMARY__\n'
python3 - '$($load.State)' <<'PY' 2>/dev/null || true
import json, sys
path = sys.argv[1]
with open(path, 'r', encoding='utf-8') as fh:
    state = json.load(fh)
safe = {k: state.get(k) for k in (
    'chain_id', 'target_total', 'total_accepted', 'faucet_accepted',
    'transfer_accepted', 'transfer_failed', 'transfer_failure_reasons',
    'created_accounts', 'retired_accounts',
    'last_height', 'last_finalized', 'last_tx_id', 'last_submit_error',
    'started_at', 'updated_at'
)}
safe['account_count'] = len(state.get('accounts') or [])
safe['pending_faucet_count'] = len(state.get('pending_faucets') or [])
print(json.dumps(safe, indent=2, sort_keys=True))
PY
"@
        $text = ($remoteSummary -join "`n")
        $aggregate.remote_text += $text + "`n"
        if ($text -match "__REPORT__\s*\{") {
            $aggregate.parse_failed = $true
        }
        $stateMarker = $text.LastIndexOf("__STATE_SUMMARY__")
        if ($stateMarker -lt 0) {
            $aggregate.parse_failed = $true
            continue
        }
        $stateJson = $text.Substring($stateMarker + "__STATE_SUMMARY__".Length).Trim()
        if (-not $stateJson) {
            $aggregate.parse_failed = $true
            continue
        }
        try {
            $stateSummary = $stateJson | ConvertFrom-Json
            $aggregate.accepted_total += [int]$stateSummary.total_accepted
            $aggregate.transfer_accepted += [int]$stateSummary.transfer_accepted
            $aggregate.faucet_accepted += [int]$stateSummary.faucet_accepted
            $aggregate.transfer_failed += [int]$stateSummary.transfer_failed
            if ([int]$stateSummary.transfer_accepted -lt $load.TargetTotal) {
                $aggregate.target_missed = $true
            }
        } catch {
            $aggregate.parse_failed = $true
        }
    }
    [pscustomobject]$aggregate
}

$runId = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$localDir = Join-Path "runtime-logs" "stress-$runId"
New-Item -ItemType Directory -Force -Path $localDir | Out-Null

Write-Host "MSC stress ladder run=$runId levels=$($Levels -join ',') duration=${DurationSeconds}s run=$Run faucet_rps=$FaucetRps submit_workers=$SubmitWorkers"
$summary = @()

foreach ($level in $Levels) {
    Write-Host "LEVEL $level TPS: gate"
    $levelStartedAt = Get-Date
    $levelSamples = @()
    $gateClean = 0
    for ($i = 1; $i -le $GateSamples; $i++) {
        $samples = Get-ClusterSamples
        $levelSamples += $samples
        $gate = Test-SamplesHealthy -Samples $samples
        Save-Json -Path (Join-Path $localDir "level-${level}-gate-${i}.json") -Value @{ level = $level; gate = $gate; samples = $samples }
        if ($gate.Ok) { $gateClean++ } else { $gateClean = 0 }
        if ($gateClean -lt $GateSamples) { Start-Sleep -Seconds $SampleSeconds }
    }
    if ($gateClean -lt $GateSamples) {
        $gateMetrics = New-LevelMetrics -Level $level -StartedAt $levelStartedAt -EndedAt (Get-Date) -Samples $levelSamples -TargetTotal ([int64]([Math]::Max(1, $level * $DurationSeconds))) -AcceptedTotal 0 -TransferAccepted 0 -FaucetAccepted 0 -TransferFailed 0
        $summary += [pscustomobject]@{ level = $level; result = "gate_failed"; metrics = $gateMetrics }
        break
    }
    if (-not $Run) {
        $gateMetrics = New-LevelMetrics -Level $level -StartedAt $levelStartedAt -EndedAt (Get-Date) -Samples $levelSamples -TargetTotal ([int64]([Math]::Max(1, $level * $DurationSeconds))) -AcceptedTotal 0 -TransferAccepted 0 -FaucetAccepted 0 -TransferFailed 0
        $summary += [pscustomobject]@{ level = $level; result = "gate_only"; metrics = $gateMetrics }
        continue
    }

    $loads = @(Start-Loadgen -Level $level)
    $started = Get-Date
    $maxRunSeconds = $DurationSeconds + $MaxUnhealthySeconds + 190
    $failed = $false
    $clusterReport = ""
    $clusterUnhealthySince = $null
    while (((Get-Date) - $started).TotalSeconds -lt $maxRunSeconds) {
        Start-Sleep -Seconds $SampleSeconds
        $samples = Get-ClusterSamples
        $levelSamples += $samples
        $gate = Test-SamplesHealthy -Samples $samples
        Save-Json -Path (Join-Path $localDir "level-${level}-sample-$([int]((Get-Date) - $started).TotalSeconds).json") -Value @{ level = $level; gate = $gate; samples = $samples }
        if (-not $gate.Ok) {
            Write-Host "level=$level gate=$($gate.Reason)"
            if ($null -eq $clusterUnhealthySince) { $clusterUnhealthySince = Get-Date }
            if (((Get-Date) - $clusterUnhealthySince).TotalSeconds -ge $MaxUnhealthySeconds) {
                $failed = $true
                $clusterReport = Join-Path $localDir "level-${level}-cluster-report.json"
                $errorId = "MSC-STRESS-" + (Get-Date).ToUniversalTime().ToString("yyyyMMddHHmmss")
                $targetTotalSum = [int64](($loads | Measure-Object -Property TargetTotal -Sum).Sum)
                $partialMetrics = New-LevelMetrics -Level $level -StartedAt $started -EndedAt (Get-Date) -Samples $levelSamples -TargetTotal $targetTotalSum -AcceptedTotal 0 -TransferAccepted 0 -FaucetAccepted 0 -TransferFailed 0
                Save-Json -Path $clusterReport -Value @{
                    error_id = $errorId
                    detected_at = (Get-Date).ToUniversalTime().ToString("o")
                    level = $level
                    reason = $gate.Reason
                    function = "scripts/live_tps_stress_ladder.ps1"
                    recovery_action = "auto_stop_loadgen"
                    target_tps = $level
                    target_total = (($loads | Measure-Object -Property TargetTotal -Sum).Sum)
                    load_hosts = (@($loads | ForEach-Object { $_.Node.Name }) -join ",")
                    load_pids = (@($loads | ForEach-Object { $_.Pid }) -join ",")
                    metrics = $partialMetrics
                    samples = $samples
                }
                Stop-Loadgen -Load $loads
                break
            }
        } else {
            $clusterUnhealthySince = $null
        }
        if (-not (Test-LoadgensRunning -Loads $loads)) { break }
    }
    Stop-Loadgen -Load $loads
    $ended = Get-Date
    $aggregate = Get-LoadgenRemoteSummary -Loads $loads
    $aggregate.remote_text | Set-Content -Path (Join-Path $localDir "level-${level}-remote.txt") -Encoding UTF8
    $acceptedTotal = $aggregate.accepted_total
    $transferAccepted = $aggregate.transfer_accepted
    $faucetAccepted = $aggregate.faucet_accepted
    $transferFailed = $aggregate.transfer_failed
    $targetMissed = [bool]$aggregate.target_missed
    if ($aggregate.parse_failed) { $failed = $true }
    if ($targetMissed) { $failed = $true }
    $result = if ($targetMissed) { "partial_target_missed" } elseif ($failed) { "failed_reported" } else { "completed" }
    $targetTotalSum = [int64](($loads | Measure-Object -Property TargetTotal -Sum).Sum)
    $metrics = New-LevelMetrics -Level $level -StartedAt $started -EndedAt $ended -Samples $levelSamples -TargetTotal $targetTotalSum -AcceptedTotal $acceptedTotal -TransferAccepted $transferAccepted -FaucetAccepted $faucetAccepted -TransferFailed $transferFailed
    $summary += [pscustomobject]@{
        level = $level
        result = $result
        target_total = $targetTotalSum
        accepted_total = $acceptedTotal
        transfer_accepted = $transferAccepted
        faucet_accepted = $faucetAccepted
        transfer_failed = $transferFailed
        metrics = $metrics
        faucet_rps = (($loads | Measure-Object -Property FaucetRps -Sum).Sum)
        submit_workers = (($loads | Measure-Object -Property SubmitWorkers -Sum).Sum)
        warmup_ready = (($loads | Measure-Object -Property WarmupReady -Sum).Sum)
        load_hosts = (@($loads | ForEach-Object { $_.Node.Name }) -join ",")
        state = (@($loads | ForEach-Object { $_.State }) -join ",")
        report = (@($loads | ForEach-Object { $_.Report }) -join ",")
        cluster_report = $clusterReport
        log = (@($loads | ForEach-Object { $_.Log }) -join ",")
    }
    if ($failed) { break }
}

Save-Json -Path (Join-Path $localDir "summary.json") -Value @{ run_id = $runId; levels = $Levels; summary = $summary }
$summary | Format-Table -AutoSize
