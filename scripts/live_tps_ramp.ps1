param(
    [string]$SshKey = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [double]$StepTps = 5,
    [int]$DurationSeconds = 300,
    [int]$GateSamples = 12,
    [int]$SampleSeconds = 10,
    [int]$MinPeers = 5,
    [int]$MinLiveValidators = 5,
    [int]$MaxMempool = 2500,
    [int]$MaxBlockAgeSeconds = 10,
    [int]$MaxFinalitySeconds = 10,
    [string]$LoadHostName = "F",
    [string]$RemoteWorkDir = "msc-chain",
    [string]$LoadgenPath = "./live_tps_loadgen.linux",
    [string]$LoadRpc = "http://127.0.0.1:26661",
    [string]$AuthTokenFile = "",
    [switch]$Run,
    [switch]$GateOnly
)

$ErrorActionPreference = "Stop"

$Nodes = @(
    [pscustomobject]@{ Name = "A"; Target = "ubuntu@54.80.4.133"; Port = 26657 },
    [pscustomobject]@{ Name = "B"; Target = "ec2-user@98.90.205.156"; Port = 26658 },
    [pscustomobject]@{ Name = "C"; Target = "ec2-user@3.88.214.207"; Port = 26659 },
    [pscustomobject]@{ Name = "D"; Target = "ec2-user@34.201.64.103"; Port = 26660 },
    [pscustomobject]@{ Name = "F"; Target = "ubuntu@50.19.167.221"; Port = 26661 }
)

function Invoke-NodeSsh {
    param(
        [Parameter(Mandatory = $true)][pscustomobject]$Node,
        [Parameter(Mandatory = $true)][string]$Command
    )

    $normalized = $Command -replace "`r", ""
    $normalized | ssh -i $SshKey -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=7 $Node.Target bash -s 2>$null
}

function Get-NodeRuntime {
    param([Parameter(Mandatory = $true)][pscustomobject]$Node)

    $remote = @"
cd $RemoteWorkDir || exit 1
HASH=`$(sha256sum msc-node 2>/dev/null | awk '{print `$1}')
PROC=`$(ps -eo pid,pcpu,pmem,rss,etime,args | grep './msc-node' | grep -v grep | head -n1 || true)
STATUS=`$(curl -sf --max-time 4 http://127.0.0.1:$($Node.Port)/status 2>/dev/null || true)
printf '__HASH__%s\n' "`$HASH"
printf '__PROC__%s\n' "`$PROC"
printf '__STATUS__%s\n' "`$STATUS"
"@
    $raw = Invoke-NodeSsh -Node $Node -Command $remote
    $hash = ""
    $proc = ""
    $statusRaw = ""
    foreach ($line in @($raw -split "`n")) {
        if ($line.StartsWith("__HASH__")) {
            $hash = $line.Substring(8).Trim()
        } elseif ($line.StartsWith("__PROC__")) {
            $proc = $line.Substring(8).Trim()
        } elseif ($line.StartsWith("__STATUS__")) {
            $statusRaw = $line.Substring(10).Trim()
        }
    }

    $procId = ""
    $cpu = 0.0
    $rss = 0
    $etime = ""
    if ($proc) {
        $parts = $proc -split "\s+"
        if ($parts.Count -ge 5) {
            $procId = $parts[0]
            $cpu = [double]$parts[1]
            $rss = [int64]$parts[3]
            $etime = $parts[4]
        }
    }

    $status = $null
    if ($statusRaw) {
        try {
            $status = $statusRaw | ConvertFrom-Json
        } catch {
            $status = $null
        }
    }

    [pscustomobject]@{
        Name = $Node.Name
        Target = $Node.Target
        Port = $Node.Port
        Hash = $hash
        Pid = $procId
        Cpu = $cpu
        RssKb = $rss
        Elapsed = $etime
        Status = $status
        StatusRaw = $statusRaw
    }
}

function Test-NodeGate {
    param([Parameter(Mandatory = $true)]$Runtime)

    $s = $Runtime.Status
    $reasons = New-Object System.Collections.Generic.List[string]
    if ($null -eq $s) {
        $reasons.Add("status_down")
    } else {
        if (-not $s.ready) { $reasons.Add("not_ready") }
        if ($s.syncing) { $reasons.Add("syncing") }
        if ([int64]$s.network_lag_blocks -ne 0) { $reasons.Add("lag_$($s.network_lag_blocks)") }
        if ([int]$s.peers -lt $MinPeers) { $reasons.Add("peers_$($s.peers)") }
        if ([int]$s.live_validators -lt $MinLiveValidators) { $reasons.Add("live_$($s.live_validators)") }
        if ([int]$s.mempool_depth -gt $MaxMempool) { $reasons.Add("mempool_$($s.mempool_depth)") }
        if ([int]$s.last_block_age_seconds -gt $MaxBlockAgeSeconds) { $reasons.Add("age_$($s.last_block_age_seconds)s") }
        if ($null -ne $s.consensus_detector_last_finality_seconds -and [int]$s.consensus_detector_last_finality_seconds -gt $MaxFinalitySeconds) {
            $reasons.Add("finality_$($s.consensus_detector_last_finality_seconds)s")
        }
        if ($s.network_health -and $s.network_health -ne "healthy") { $reasons.Add("health_$($s.network_health)") }
    }
    if (-not $Runtime.Pid) { $reasons.Add("process_missing") }

    [pscustomobject]@{
        Ok = $reasons.Count -eq 0
        Reason = ($reasons -join ",")
    }
}

function Write-RuntimeLine {
    param(
        [Parameter(Mandatory = $true)]$Runtime,
        [Parameter(Mandatory = $true)]$Gate
    )

    $s = $Runtime.Status
    if ($null -eq $s) {
        Write-Host ("{0} DOWN pid={1} cpu={2:n1}% rss={3:n0}KB hash={4} gate={5}" -f $Runtime.Name, $Runtime.Pid, $Runtime.Cpu, $Runtime.RssKb, $Runtime.Hash.Substring(0, [Math]::Min(8, $Runtime.Hash.Length)), $Gate.Reason)
        return
    }

    Write-Host ("{0} h={1} final={2} sync={3} lag={4} peers={5} live={6} ready={7} wait={8} age={9}s finality={10}s mempool={11} cpu={12:n1}% rss={13:n0}KB pid={14} hash={15} gate={16}" -f `
        $Runtime.Name,
        $s.height,
        $s.finalized_height,
        $s.syncing,
        $s.network_lag_blocks,
        $s.peers,
        $s.live_validators,
        $s.ready,
        $s.wait_reason,
        $s.last_block_age_seconds,
        $s.consensus_detector_last_finality_seconds,
        $s.mempool_depth,
        $Runtime.Cpu,
        $Runtime.RssKb,
        $Runtime.Pid,
        $Runtime.Hash.Substring(0, [Math]::Min(8, $Runtime.Hash.Length)),
        $(if ($Gate.Ok) { "ok" } else { $Gate.Reason }))
}

function Test-ClusterGate {
    param([switch]$Quiet)

    $runtimes = @()
    $allOk = $true
    foreach ($node in $Nodes) {
        $runtime = Get-NodeRuntime -Node $node
        $gate = Test-NodeGate -Runtime $runtime
        $runtime | Add-Member -NotePropertyName GateOk -NotePropertyValue $gate.Ok -Force
        $runtime | Add-Member -NotePropertyName GateReason -NotePropertyValue $gate.Reason -Force
        $runtimes += $runtime
        if (-not $gate.Ok) {
            $allOk = $false
        }
        if (-not $Quiet) {
            Write-RuntimeLine -Runtime $runtime -Gate $gate
        }
    }

    [pscustomobject]@{
        Ok = $allOk
        Runtimes = $runtimes
    }
}

function Wait-StrictGate {
    $clean = 0
    for ($sample = 1; $sample -le $GateSamples; $sample++) {
        Write-Host "GATE SAMPLE $sample/$GateSamples"
        $result = Test-ClusterGate
        if ($result.Ok) {
            $clean++
        } else {
            $clean = 0
        }
        Write-Host "gate_clean_consecutive=$clean required=$GateSamples"
        if ($clean -lt $GateSamples) {
            Start-Sleep -Seconds $SampleSeconds
        }
    }
    return $clean -ge $GateSamples
}

function Start-RemoteLoadgen {
    $loadNode = $Nodes | Where-Object { $_.Name -eq $LoadHostName } | Select-Object -First 1
    if ($null -eq $loadNode) {
        throw "unknown load host: $LoadHostName"
    }

    $targetTotal = [Math]::Max(1, [int][Math]::Ceiling($StepTps * $DurationSeconds))
    $safeTps = ("{0:0.###}" -f $StepTps)
    $state = "runtime-logs/live-tps-ramp-${safeTps}tps-state.json"
    $log = "runtime-logs/live-tps-ramp-${safeTps}tps.log"
    $accountPool = [Math]::Max(100, [int][Math]::Ceiling($StepTps * 20))
    $faucetRps = [Math]::Min(1.0, [Math]::Max(0.2, $StepTps / 25.0))
    $authArg = ""
    if ($AuthTokenFile.Trim()) {
        $authArg = "-auth-token-file '$AuthTokenFile'"
    }

    $remote = @"
set -e
cd $RemoteWorkDir
test -x $LoadgenPath
mkdir -p runtime-logs
nohup $LoadgenPath -rpc '$LoadRpc' -target-total $targetTotal -target-tps $safeTps -account-pool $accountPool -faucet-rps $faucetRps -max-mempool $MaxMempool -max-block-age $MaxBlockAgeSeconds -status-every 10s -state '$state' $authArg > '$log' 2>&1 &
PID=`$!
printf 'LOADGEN_PID=%s\n' "`$PID"
printf 'LOADGEN_LOG=%s\n' "$log"
printf 'LOADGEN_STATE=%s\n' "$state"
"@
    $raw = Invoke-NodeSsh -Node $loadNode -Command $remote
    Write-Host $raw
    $pid = ""
    foreach ($line in @($raw -split "`n")) {
        if ($line.StartsWith("LOADGEN_PID=")) {
            $pid = $line.Substring(12).Trim()
        }
    }
    if (-not $pid) {
        throw "loadgen did not return a pid"
    }
    [pscustomobject]@{ Node = $loadNode; Pid = $pid; Log = $log; State = $state; TargetTotal = $targetTotal }
}

function Stop-RemoteLoadgen {
    param([Parameter(Mandatory = $true)]$Load)
    $remote = @"
cd $RemoteWorkDir || exit 0
kill $($Load.Pid) 2>/dev/null || true
sleep 1
kill -9 $($Load.Pid) 2>/dev/null || true
tail -n 80 '$($Load.Log)' 2>/dev/null || true
"@
    Invoke-NodeSsh -Node $Load.Node -Command $remote
}

function Watch-Loadgen {
    param([Parameter(Mandatory = $true)]$Load)

    $baseline = @{}
    foreach ($runtime in (Test-ClusterGate -Quiet).Runtimes) {
        $baseline[$runtime.Name] = $runtime.Pid
    }

    $deadline = (Get-Date).AddSeconds($DurationSeconds)
    while ((Get-Date) -lt $deadline) {
        Write-Host "RUN WATCH tps=$StepTps remaining=$([int]($deadline - (Get-Date)).TotalSeconds)s"
        $gate = Test-ClusterGate
        foreach ($runtime in $gate.Runtimes) {
            if ($baseline.ContainsKey($runtime.Name) -and $baseline[$runtime.Name] -and $runtime.Pid -and $baseline[$runtime.Name] -ne $runtime.Pid) {
                Write-Host "restart_detected node=$($runtime.Name) before=$($baseline[$runtime.Name]) after=$($runtime.Pid)"
                $gate.Ok = $false
            }
        }
        if (-not $gate.Ok) {
            Write-Host "gate failed during load; stopping loadgen"
            Stop-RemoteLoadgen -Load $Load
            throw "load aborted because health gate failed"
        }
        Start-Sleep -Seconds $SampleSeconds
    }

    $remote = @"
cd $RemoteWorkDir || exit 1
if kill -0 $($Load.Pid) 2>/dev/null; then
  wait $($Load.Pid) 2>/dev/null || true
fi
tail -n 80 '$($Load.Log)' 2>/dev/null || true
"@
    Invoke-NodeSsh -Node $Load.Node -Command $remote
}

Write-Host "MSC live TPS ramp guard step_tps=$StepTps duration=${DurationSeconds}s run=$Run gate_only=$GateOnly"
$gateOk = Wait-StrictGate
if (-not $gateOk) {
    throw "strict gate failed; load was not started"
}

if ($GateOnly -or -not $Run) {
    Write-Host "strict gate passed; load not started because -Run was not provided"
    exit 0
}

$load = Start-RemoteLoadgen
Watch-Loadgen -Load $load
Write-Host "ramp step completed tps=$StepTps duration=${DurationSeconds}s target_total=$($load.TargetTotal)"
