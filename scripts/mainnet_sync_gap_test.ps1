param(
    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [string]$BinaryPath = ".\msc-chain.exe",
    [string]$Config = "config.toml",
    [string]$DataRoot = "",
    [string]$LogRoot = "runtime-logs",
    [int]$BaseP2PPort = 7001,
    [int]$BaseRpcPort = 26657,
    [int]$InitialFreshFullGapBlocks = 0,
    [int]$WarmupSeconds = 70,
    [int]$MinWarmupHeight = 10,
    [int]$SmallGapOfflineSeconds = 5,
    [int]$SmallGapTargetBlocks = 1,
    [int]$MinSmallGapBlocks = 1,
    [int]$LargeGapOfflineSeconds = 35,
    [int]$LargeGapTargetBlocks = 0,
    [int]$MinLargeGapBlocks = 10,
    [int]$CatchupTimeoutSeconds = 90,
    [switch]$KeepRunning
)

$ErrorActionPreference = "Stop"

function Resolve-RepoPath {
    param([string]$Path)
    if ([System.IO.Path]::IsPathRooted($Path)) {
        return $Path
    }
    return Join-Path $RepoRoot $Path
}

function Get-NodePassword {
    param([string]$Id)
    foreach ($name in @("MSC_NODE_PASSWORD_$Id", "MSC_VALIDATOR_PASSWORD_$Id", "MSC_VALIDATOR_PASSWORD", "MSC_CHAOS_VALIDATOR_PASSWORD")) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if (-not [string]::IsNullOrWhiteSpace($value)) {
            return $value
        }
    }
    throw "missing validator password env for node $Id"
}

function Invoke-Status {
    param([int]$RpcPort)
    try {
        return Invoke-RestMethod -Uri "http://127.0.0.1:$RpcPort/status" -TimeoutSec 3
    } catch {
        return $null
    }
}

function Wait-Rpc {
    param([int]$RpcPort, [int]$TimeoutSeconds)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if ($null -ne (Invoke-Status -RpcPort $RpcPort)) {
            return $true
        }
        Start-Sleep -Seconds 1
    }
    return $false
}

function Stop-Node {
    param([hashtable]$Node)
    if ($Node.Process -and -not $Node.Process.HasExited) {
        Stop-Process -Id $Node.Process.Id -Force -ErrorAction SilentlyContinue
    }
    $rpcNeedle = "127.0.0.1:$($Node.RpcPort)"
    Get-CimInstance Win32_Process |
        Where-Object { "$($_.CommandLine)" -like "*$rpcNeedle*" -or "$($_.CommandLine)" -like "*--port=$($Node.P2PPort)*" } |
        ForEach-Object {
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }
    $Node.Process = $null
}

function Start-Node {
    param([hashtable]$Node)
    Stop-Node -Node $Node
    New-Item -ItemType Directory -Force -Path $Node.DataDir | Out-Null
    if (-not $Node.ContainsKey("Generation")) {
        $Node.Generation = 0
    }
    $Node.Generation = [int]$Node.Generation + 1
    $outLog = Join-Path $script:RunLogRoot "$($Node.Id)-$($Node.Generation).out.log"
    $errLog = Join-Path $script:RunLogRoot "$($Node.Id)-$($Node.Generation).err.log"
    $Node.CurrentOutLog = $outLog
    $Node.CurrentErrLog = $errLog

    $oldAllow = $env:MSC_ALLOW_VALIDATOR_KEY_CREATE
    $oldPassword = $env:MSC_VALIDATOR_PASSWORD
    $env:MSC_ALLOW_VALIDATOR_KEY_CREATE = "1"
    $env:MSC_VALIDATOR_PASSWORD = $Node.Password
    try {
        $args = @(
            "--mode=full",
            "--role=$($Node.Role)",
            "--id=$($Node.Id)",
            "--port=$($Node.P2PPort)",
            "--datadir=$($Node.DataDir)",
            "--rpcaddr",
            "127.0.0.1:$($Node.RpcPort)",
            "--config",
            $Config
        )
        $peerArg = Get-PeerArgForNode -Node $Node
        if (-not [string]::IsNullOrWhiteSpace($peerArg)) {
            $args += @("--peers", $peerArg)
        }
        $Node.Process = Start-Process -FilePath $script:ResolvedBinary -ArgumentList $args -WorkingDirectory $RepoRoot -WindowStyle Hidden -RedirectStandardOutput $outLog -RedirectStandardError $errLog -PassThru
    } finally {
        $env:MSC_ALLOW_VALIDATOR_KEY_CREATE = $oldAllow
        $env:MSC_VALIDATOR_PASSWORD = $oldPassword
    }
}

function Get-PeerArgForNode {
    param([hashtable]$Node)
    if (-not $script:PeerAddrs -or $script:PeerAddrs.Count -eq 0) {
        return ""
    }
    $peers = @(
        $script:PeerAddrs |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) -and $_ -notmatch "/tcp/$($Node.P2PPort)/" } |
            Sort-Object -Unique
    )
    if ($peers.Count -eq 0) {
        return ""
    }
    return ($peers -join ",")
}

function Update-PeerAddressBook {
    param([int]$TimeoutSeconds = 15)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $found = @()
        foreach ($node in $script:Nodes) {
            $logPath = [string]$node.CurrentOutLog
            if ([string]::IsNullOrWhiteSpace($logPath) -or -not (Test-Path -LiteralPath $logPath)) {
                continue
            }
            $peerId = ""
            $matches = Select-String -LiteralPath $logPath -Pattern "peer_id=([A-Za-z0-9]+)" -AllMatches -ErrorAction SilentlyContinue
            foreach ($match in $matches) {
                foreach ($m in $match.Matches) {
                    $peerId = $m.Groups[1].Value
                }
            }
            if (-not [string]::IsNullOrWhiteSpace($peerId)) {
                $found += "/ip4/127.0.0.1/tcp/$($node.P2PPort)/p2p/$peerId"
            }
        }
        if ($found.Count -gt 0) {
            $script:PeerAddrs = @($found | Sort-Object -Unique)
        }
        $running = @($script:Nodes | Where-Object { $_.Process -and -not $_.Process.HasExited })
        if ($script:PeerAddrs.Count -ge [Math]::Min(4, $running.Count)) {
            return
        }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $deadline)
}

function Max-ValidatorFinalized {
	$max = [int64]0
	foreach ($node in @($script:Nodes | Where-Object { $_.Role -eq "validator" })) {
		$status = Invoke-Status -RpcPort $node.RpcPort
		if ($status) {
			$height = [int64]$status.height
			if ($height -gt $max) {
				$max = $height
			}
        }
    }
    return $max
}

function Wait-FullNodeCatchup {
    param([int64]$TargetHeight, [int]$TimeoutSeconds, [switch]$RequireSyncComplete, [int]$StableSamples = 2)
    $start = Get-Date
	$deadline = $start.AddSeconds($TimeoutSeconds)
	$lastHeight = [int64]0
	$lastFinalized = [int64]0
	$lastPeers = 0
	$lastSyncing = $true
	$lastSyncComplete = $false
	$lastSyncStage = ""
	$lastSyncAction = ""
	$lastNetworkLag = [int64]0
	$stable = 0
	while ((Get-Date) -lt $deadline) {
		$status = Invoke-Status -RpcPort $script:FullNode.RpcPort
		if ($status) {
			$lastHeight = [int64]$status.height
			$lastFinalized = [int64]$status.finalized_height
			$lastPeers = [int]$status.peers
			$lastSyncing = [bool]$status.syncing
			$lastSyncComplete = [bool]$status.sync_complete
			$lastSyncStage = [string]$status.sync_stage
			$lastSyncAction = [string]$status.sync_action
			try { $lastNetworkLag = [int64]$status.network_lag_blocks } catch { $lastNetworkLag = [int64]0 }
			$targetCaught = $lastHeight -ge $TargetHeight
			$syncDone = (-not $lastSyncing) -and $lastSyncComplete
			if ($targetCaught -and ((-not $RequireSyncComplete) -or $syncDone)) {
				$stable++
			} else {
				$stable = 0
			}
			if ($targetCaught -and $stable -ge $StableSamples) {
				return [pscustomobject]@{
					ok = $true
					seconds = [int]((Get-Date) - $start).TotalSeconds
					chain_height = $lastHeight
					finalized_height = $lastFinalized
					peers = $lastPeers
					syncing = $lastSyncing
					sync_complete = $lastSyncComplete
					sync_stage = $lastSyncStage
					sync_action = $lastSyncAction
					network_lag_blocks = $lastNetworkLag
                }
            }
        }
        Start-Sleep -Seconds 2
    }
	return [pscustomobject]@{
		ok = $false
		seconds = $TimeoutSeconds
		chain_height = $lastHeight
		finalized_height = $lastFinalized
		peers = $lastPeers
		syncing = $lastSyncing
		sync_complete = $lastSyncComplete
		sync_stage = $lastSyncStage
		sync_action = $lastSyncAction
		network_lag_blocks = $lastNetworkLag
	}
}

function Wait-ValidatorHeightAtLeast {
    param([int64]$TargetHeight, [int]$TimeoutSeconds)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $last = Max-ValidatorFinalized
    while ((Get-Date) -lt $deadline) {
        $last = Max-ValidatorFinalized
        if ($last -ge $TargetHeight) {
            return $last
        }
        Start-Sleep -Milliseconds 500
    }
    return $last
}

Set-Location $RepoRoot
$passwordPath = Join-Path $PSScriptRoot "mainnet_node_passwords.local.ps1"
if (Test-Path -LiteralPath $passwordPath) {
    . $passwordPath
}

$script:ResolvedBinary = Resolve-RepoPath -Path $BinaryPath
if (-not (Test-Path -LiteralPath $script:ResolvedBinary)) {
    throw "binary not found: $script:ResolvedBinary"
}

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
if ([string]::IsNullOrWhiteSpace($DataRoot)) {
    $DataRoot = "data\sync-gap-live-$stamp"
}
$script:RunLogRoot = Resolve-RepoPath -Path (Join-Path $LogRoot "sync-gap-live-$stamp")
New-Item -ItemType Directory -Force -Path (Resolve-RepoPath -Path $DataRoot), $script:RunLogRoot | Out-Null

$ids = @("A", "B", "C", "D", "F")
$script:Nodes = @()
for ($i = 0; $i -lt $ids.Count; $i++) {
    $id = $ids[$i]
    $role = if ($i -lt 4) { "validator" } else { "full" }
    $script:Nodes += @{
        Id = $id
        Role = $role
        P2PPort = $BaseP2PPort + $i
        RpcPort = $BaseRpcPort + $i
        DataDir = Join-Path $DataRoot $id
        Password = Get-NodePassword -Id $id
        Process = $null
    }
}
$script:FullNode = @($script:Nodes | Where-Object { $_.Id -eq "F" })[0]
$script:PeerAddrs = @()

$results = @()
try {
    $startupNodes = @($script:Nodes)
    if ($InitialFreshFullGapBlocks -gt 0) {
        $startupNodes = @($script:Nodes | Where-Object { $_.Id -ne $script:FullNode.Id })
    }

    foreach ($node in $startupNodes) {
        Start-Node -Node $node
        if (-not (Wait-Rpc -RpcPort $node.RpcPort -TimeoutSeconds 60)) {
            throw "RPC not ready for node $($node.Id)"
        }
        Update-PeerAddressBook -TimeoutSeconds 10
    }
    Update-PeerAddressBook -TimeoutSeconds 10

    if ($InitialFreshFullGapBlocks -gt 0) {
        $target = Wait-ValidatorHeightAtLeast -TargetHeight ([int64]$InitialFreshFullGapBlocks) -TimeoutSeconds $WarmupSeconds
        if ($target -lt $InitialFreshFullGapBlocks) {
            throw "fresh full-node target gap not reached: got_height=$target want_height=$InitialFreshFullGapBlocks"
        }
        Update-PeerAddressBook -TimeoutSeconds 5
        Start-Node -Node $script:FullNode
        if (-not (Wait-Rpc -RpcPort $script:FullNode.RpcPort -TimeoutSeconds 60)) {
            throw "RPC not ready for fresh full node $($script:FullNode.Id)"
        }
        Update-PeerAddressBook -TimeoutSeconds 5
        $catchup = Wait-FullNodeCatchup -TargetHeight $target -TimeoutSeconds $CatchupTimeoutSeconds -RequireSyncComplete
        $row = [pscustomobject]@{
            phase = "fresh_full_initial_gap"
            gap_blocks = $target
            target_height = $target
            caught_up = $catchup.ok
            catchup_seconds = $catchup.seconds
            full_chain_height = $catchup.chain_height
            full_finalized_height = $catchup.finalized_height
            full_peers = $catchup.peers
            full_syncing = $catchup.syncing
            full_sync_complete = $catchup.sync_complete
            full_sync_stage = $catchup.sync_stage
            full_sync_action = $catchup.sync_action
            full_network_lag_blocks = $catchup.network_lag_blocks
        }
        $results += $row
        $row | Format-List | Out-String | Write-Host
        if (-not $catchup.ok) {
            throw "fresh full-node catchup failed target=$target full=$($catchup.chain_height)"
        }
    }

    $warmDeadline = (Get-Date).AddSeconds($WarmupSeconds)
    do {
        Start-Sleep -Seconds 5
		$validatorHeight = Max-ValidatorFinalized
		$fullStatus = Invoke-Status -RpcPort $script:FullNode.RpcPort
		$fullHeight = if ($fullStatus) { [int64]$fullStatus.height } else { 0 }
        Write-Host ("warmup validators={0} full={1}" -f $validatorHeight, $fullHeight)
    } while ((Get-Date) -lt $warmDeadline -and ($validatorHeight -lt $MinWarmupHeight -or [Math]::Abs($validatorHeight - $fullHeight) -gt 2))

    if ($validatorHeight -lt $MinWarmupHeight) {
        throw "validators did not produce enough blocks during warmup: got=$validatorHeight want>=$MinWarmupHeight"
    }

    foreach ($phase in @(
        @{ name = "small_gap"; offline = $SmallGapOfflineSeconds; min_gap = $MinSmallGapBlocks; target_blocks = $SmallGapTargetBlocks },
        @{ name = "large_gap"; offline = $LargeGapOfflineSeconds; min_gap = $MinLargeGapBlocks; target_blocks = $LargeGapTargetBlocks }
    )) {
        $before = Max-ValidatorFinalized
        Write-Host ("phase={0} stopping_full_node height={1} offline_s={2}" -f $phase.name, $before, $phase.offline)
        Stop-Node -Node $script:FullNode
        if ([int]$phase.target_blocks -gt 0) {
            $desiredTarget = $before + [int64]$phase.target_blocks
            $target = Wait-ValidatorHeightAtLeast -TargetHeight $desiredTarget -TimeoutSeconds ([int]$phase.offline)
            if ($target -lt $desiredTarget) {
                throw "phase=$($phase.name) target gap not reached: got_height=$target want_height=$desiredTarget timeout_s=$($phase.offline)"
            }
        } else {
            Start-Sleep -Seconds ([int]$phase.offline)
            $target = Max-ValidatorFinalized
        }
        $gap = $target - $before
        if ($gap -lt [int64]$phase.min_gap) {
            throw "phase=$($phase.name) created insufficient gap: got=$gap want>=$($phase.min_gap)"
        }
        Update-PeerAddressBook -TimeoutSeconds 5
        Start-Node -Node $script:FullNode
        if (-not (Wait-Rpc -RpcPort $script:FullNode.RpcPort -TimeoutSeconds 60)) {
            throw "full node RPC not ready after restart in phase $($phase.name)"
        }
        Update-PeerAddressBook -TimeoutSeconds 5
        $catchup = Wait-FullNodeCatchup -TargetHeight $target -TimeoutSeconds $CatchupTimeoutSeconds -RequireSyncComplete
        $row = [pscustomobject]@{
            phase = $phase.name
            gap_blocks = $gap
            target_height = $target
            caught_up = $catchup.ok
            catchup_seconds = $catchup.seconds
            full_chain_height = $catchup.chain_height
            full_finalized_height = $catchup.finalized_height
            full_peers = $catchup.peers
            full_syncing = $catchup.syncing
            full_sync_complete = $catchup.sync_complete
            full_sync_stage = $catchup.sync_stage
            full_sync_action = $catchup.sync_action
            full_network_lag_blocks = $catchup.network_lag_blocks
        }
        $results += $row
        $row | Format-List | Out-String | Write-Host
        if (-not $catchup.ok) {
            throw "catchup failed phase=$($phase.name) target=$target full=$($catchup.chain_height)"
        }
    }

    $summary = [ordered]@{
        passed = $true
        log_root = $script:RunLogRoot
        data_root = Resolve-RepoPath -Path $DataRoot
        peer_addrs = $script:PeerAddrs
        results = $results
    }
    $summary | ConvertTo-Json -Depth 8 | Set-Content -Path (Join-Path $script:RunLogRoot "summary.json")
    Write-Host ("SYNC_GAP_TEST_PASS log_root={0}" -f $script:RunLogRoot)
} finally {
    if (-not $KeepRunning) {
        foreach ($node in $script:Nodes) {
            Stop-Node -Node $node
        }
    }
}
