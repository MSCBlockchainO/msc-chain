param(
    [int]$DurationSeconds = 21600,
    [int]$DurationMinutes = 0,
    [int]$WarmupSeconds = 90,
    [int]$NodeCount = 10,
    [int]$ValidatorNodeCount = 5,
    [string]$NodeIds = "",
    [string]$TopologyPath = "",
    [string]$RpcHost = "127.0.0.1",
    [int]$BaseP2PPort = 7001,
    [int]$BaseRpcPort = 26657,
    [int]$SampleSeconds = 5,
    [int]$FullStatusEverySamples = 6,
    [int]$ForkCheckEverySamples = 3,
    [int]$MaxBlockGapSeconds = 30,
    [int]$MaxFinalizedLagBlocks = 12,
    [int]$MaxHeightLagBlocks = 24,
    [int]$MinReachableNodes = 0,
    [int]$StatusTimeoutSeconds = 8,
    [int]$StartupWaitSeconds = 45,
    [int]$MinRestartSeconds = 90,
    [int]$MaxRestartSeconds = 240,
    [int]$RestartDownMinSeconds = 10,
    [int]$RestartDownMaxSeconds = 30,
    [int]$RestartStormSize = 2,
    [int]$IsolationEverySeconds = 75,
    [int]$IsolationMinSeconds = 8,
    [int]$IsolationMaxSeconds = 25,
    [int]$PacketLossEverySeconds = 55,
    [int]$PacketLossMinSeconds = 4,
    [int]$PacketLossMaxSeconds = 12,
    [int]$PacketLossPercent = 15,
    [int]$LatencyMs = 250,
    [int]$LatencyJitterMs = 80,
    [string]$NetemInterface = "eth0",
    [int]$SlowEverySeconds = 120,
    [int]$SlowMinSeconds = 20,
    [int]$SlowMaxSeconds = 45,
    [int]$CpuPressureWorkers = 0,
    [int]$StaleSnapshotEverySeconds = 180,
    [int]$StaleSnapshotDownSeconds = 20,
    [int]$StaleSnapshotHideNewest = 1,
    [int]$TxFloodEverySeconds = 20,
    [int]$TxFloodBatch = 10,
    [int]$ValidatorFaultBiasPercent = 70,
    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [string]$DataRoot = "data",
    [string]$LogRoot = "runtime-logs",
    [string]$Config = "config.toml",
    [switch]$AutoLocalPeers,
    [switch]$UseBuiltBinary,
    [string]$BinaryPath = ".\msc-chain.exe",
    [switch]$SkipStart,
    [switch]$DryRun,
    [switch]$Tier1Survival,
    [switch]$NoRandomRestarts,
    [switch]$RestartStorms,
    [switch]$NetworkChaos,
    [switch]$PacketLoss,
    [switch]$UseRemoteNetem,
    [switch]$SlowValidators,
    [switch]$StaleSnapshots,
    [switch]$AllowSnapshotMutation,
    [switch]$TxFlood,
    [switch]$IncludeRpcInIsolation,
    [switch]$FailOnWarning
)

$ErrorActionPreference = "Stop"

function New-ChaosNodeIds {
    param([int]$Count, [string]$RawIds)
    if (-not [string]::IsNullOrWhiteSpace($RawIds)) {
        $ids = @($RawIds -split "," | ForEach-Object { $_.Trim().ToUpperInvariant() } | Where-Object { $_ })
        if ($ids.Count -eq 0) {
            throw "NodeIds was provided but no IDs were parsed."
        }
        return $ids
    }
    $defaults = @("A","B","C","D","F","G","H","I","J","K","L","M","N","O","P","Q","R","S","T","U","V","W","X","Y","Z")
    $ids = @()
    for ($i = 0; $i -lt $Count; $i++) {
        if ($i -lt $defaults.Count) {
            $ids += $defaults[$i]
        } else {
            $ids += ("N{0:D2}" -f ($i - $defaults.Count + 1))
        }
    }
    return $ids
}

function Convert-ToRpcParts {
    param([string]$RawRpc, [string]$DefaultHost, [int]$DefaultPort)
    $raw = "$RawRpc".Trim()
    if ([string]::IsNullOrWhiteSpace($raw)) {
        $raw = "$DefaultHost`:$DefaultPort"
    }
    $url = $raw
    if ($url -notmatch "^https?://") {
        $url = "http://$url"
    }
    $uri = [Uri]$url
    return @{
        Rpc = "$($uri.Host):$($uri.Port)"
        RpcUrl = $url.TrimEnd("/")
        RpcHost = $uri.Host
        RpcPort = [int]$uri.Port
    }
}

function Test-LocalHostName {
    param([string]$HostName)
    $h = "$HostName".Trim().ToLowerInvariant()
    if ($h -eq "" -or $h -eq "localhost" -or $h -eq "127.0.0.1" -or $h -eq "::1") {
        return $true
    }
    $machine = "$env:COMPUTERNAME".Trim().ToLowerInvariant()
    return $machine -ne "" -and $h -eq $machine
}

function New-ChaosNode {
    param(
        [string]$Id,
        [int]$Index,
        [string]$Role,
        [string]$HostName,
        [string]$Rpc,
        [int]$P2PPort,
        [string]$DataDir,
        [string]$NodeDataDir,
        [string]$PasswordEnv,
        [string]$SshTarget,
        [string]$Os
    )
    $idNorm = "$Id".Trim().ToUpperInvariant()
    if ([string]::IsNullOrWhiteSpace($idNorm)) {
        throw "node id cannot be empty"
    }
    $rpcParts = Convert-ToRpcParts -RawRpc $Rpc -DefaultHost $HostName -DefaultPort ($BaseRpcPort + $Index)
    if ([string]::IsNullOrWhiteSpace($HostName)) {
        $HostName = $rpcParts.RpcHost
    }
    if ($P2PPort -le 0) {
        $P2PPort = $BaseP2PPort + $Index
    }
    if ([string]::IsNullOrWhiteSpace($Role)) {
        $Role = "full"
    }
    if ([string]::IsNullOrWhiteSpace($DataDir)) {
        $DataDir = Join-Path $DataRoot $idNorm
    }
    if ([string]::IsNullOrWhiteSpace($NodeDataDir)) {
        $NodeDataDir = Join-Path $DataDir ("node_" + $idNorm)
    }
    if ([string]::IsNullOrWhiteSpace($PasswordEnv)) {
        $PasswordEnv = "MSC_VALIDATOR_PASSWORD_$idNorm"
    }
    return [ordered]@{
        Id = $idNorm
        Index = $Index
        Host = $HostName
        Local = (Test-LocalHostName -HostName $HostName)
        Os = "$Os".Trim().ToLowerInvariant()
        Port = $P2PPort
        Rpc = $rpcParts.Rpc
        RpcUrl = $rpcParts.RpcUrl
        RpcHost = $rpcParts.RpcHost
        RpcPort = $rpcParts.RpcPort
        DataDir = $DataDir
        NodeDataDir = $NodeDataDir
        PasswordEnv = $PasswordEnv
        Role = $Role.ToLowerInvariant()
        SshTarget = "$SshTarget".Trim()
        Process = $null
        StartedAt = $null
        IsolationRules = @()
        IsolationUntil = $null
        NetemUntil = $null
        SlowUntil = $null
        CpuLoad = @()
        OriginalPriority = $null
        PeerAddrs = @()
    }
}

function New-ChaosNodesFromDefaults {
    param([object[]]$Ids)
    $nodes = @()
    for ($i = 0; $i -lt $Ids.Count; $i++) {
        $id = "$($Ids[$i])".Trim().ToUpperInvariant()
        if ([string]::IsNullOrWhiteSpace($id)) {
            continue
        }
        $role = $(if ($i -lt $ValidatorNodeCount) { "validator" } else { "full" })
        $nodes += New-ChaosNode -Id $id -Index $i -Role $role -HostName $RpcHost -Rpc "" -P2PPort ($BaseP2PPort + $i) -DataDir "" -NodeDataDir "" -PasswordEnv "" -SshTarget "" -Os ""
    }
    return $nodes
}

function New-ChaosNodesFromTopology {
    param([string]$Path)
    $resolved = Resolve-Path -LiteralPath $Path
    $json = Get-Content -LiteralPath $resolved.Path -Raw | ConvertFrom-Json
    $rawNodes = @()
    if ($json.PSObject.Properties.Name -contains "nodes") {
        $rawNodes = @($json.nodes)
    } else {
        $rawNodes = @($json)
    }
    if ($rawNodes.Count -eq 0) {
        throw "topology has no nodes: $Path"
    }
    $nodes = @()
    for ($i = 0; $i -lt $rawNodes.Count; $i++) {
        $raw = $rawNodes[$i]
        $id = "$($raw.id)"
        if ([string]::IsNullOrWhiteSpace($id)) {
            throw "topology node at index $i has no id"
        }
        $hostName = "$($raw.host)"
        if ([string]::IsNullOrWhiteSpace($hostName)) {
            $hostName = $RpcHost
        }
        $rpc = "$($raw.rpc)"
        $role = "$($raw.role)"
        if ([string]::IsNullOrWhiteSpace($role)) {
            $role = $(if ($i -lt $ValidatorNodeCount) { "validator" } else { "full" })
        }
        $p2pPort = 0
        if ($raw.PSObject.Properties.Name -contains "p2p_port") {
            try { $p2pPort = [int]$raw.p2p_port } catch { $p2pPort = 0 }
        } elseif ($raw.PSObject.Properties.Name -contains "port") {
            try { $p2pPort = [int]$raw.port } catch { $p2pPort = 0 }
        }
        $dataDir = "$($raw.data_dir)"
        $nodeDataDir = "$($raw.node_data_dir)"
        $passwordEnv = "$($raw.password_env)"
        $sshTarget = "$($raw.ssh_target)"
        if ([string]::IsNullOrWhiteSpace($sshTarget)) {
            $sshTarget = "$($raw.ssh)"
        }
        $os = "$($raw.os)"
        $nodes += New-ChaosNode -Id $id -Index $i -Role $role -HostName $hostName -Rpc $rpc -P2PPort $p2pPort -DataDir $dataDir -NodeDataDir $nodeDataDir -PasswordEnv $passwordEnv -SshTarget $sshTarget -Os $os
    }
    return $nodes
}

function Get-NodePassword {
    param([hashtable]$Node)
    foreach ($name in @($Node.PasswordEnv, "MSC_NODE_PASSWORD_$($Node.Id)", "MSC_VALIDATOR_PASSWORD", "MSC_CHAOS_VALIDATOR_PASSWORD")) {
        $value = [Environment]::GetEnvironmentVariable($name, "Process")
        if ([string]::IsNullOrWhiteSpace($value)) {
            $value = [Environment]::GetEnvironmentVariable($name, "User")
        }
        if (-not [string]::IsNullOrWhiteSpace($value)) {
            return $value
        }
    }
    return ""
}

function Get-ResolvedBinaryPath {
    $resolvedBinary = $BinaryPath
    if (-not [System.IO.Path]::IsPathRooted($resolvedBinary)) {
        $resolvedBinary = Join-Path $RepoRoot $resolvedBinary
    }
    return $resolvedBinary
}

function Get-StartCommandPreview {
    param([hashtable]$Node)
    $program = "go"
    $args = @("run", ".")
    if ($UseBuiltBinary) {
        $program = Get-ResolvedBinaryPath
        $args = @()
    }
    $args += @(
        "--mode=full",
        "--role=$($Node.Role)",
        "--id=$($Node.Id)",
        "--port=$($Node.Port)",
        "--datadir=$($Node.DataDir)",
        "--rpcaddr", $Node.Rpc
    )
    $configPath = Join-Path $RepoRoot $Config
    if (-not [string]::IsNullOrWhiteSpace($Config) -and (Test-Path -LiteralPath $configPath)) {
        $args += @("--config", $Config)
    }
    $peerAddrs = @()
    if ($Node.Contains("PeerAddrs")) {
        $peerAddrs = @($Node.PeerAddrs | Where-Object { -not [string]::IsNullOrWhiteSpace("$_") })
    }
    if ($peerAddrs.Count -gt 0) {
        $args += @("--peers", ($peerAddrs -join ","))
    }
    return "& '$program' " + (($args | ForEach-Object { "'$($_.Replace("'","''"))'" }) -join " ")
}

function Start-ChaosNode {
    param(
        [hashtable]$Node,
        [string]$RunDir,
        [string]$EventsPath
    )
    if (-not $Node.Local) {
        throw "Node $($Node.Id) is remote ($($Node.Host)). Start it on that host and run this controller with -SkipStart."
    }

    $id = $Node.Id
    $password = Get-NodePassword -Node $Node
    if ([string]::IsNullOrWhiteSpace($password)) {
        if ($Node.Role -eq "validator") {
            throw "Missing validator password for node $id. Set $($Node.PasswordEnv), MSC_VALIDATOR_PASSWORD, or MSC_CHAOS_VALIDATOR_PASSWORD."
        }
        $password = "observer-chaos-password"
    }
    if ($Node.Role -eq "validator" -and $password.Trim() -in @("your-validator-password", "CHANGE_ME", "changeme")) {
        throw "Refusing to start validator $id with placeholder password '$password'. Set the real validator key password in $($Node.PasswordEnv), MSC_VALIDATOR_PASSWORD, or MSC_CHAOS_VALIDATOR_PASSWORD."
    }

    $nodeLog = Join-Path $RunDir "$id.log"
    $program = "go"
    $args = @("run", ".")
    if ($UseBuiltBinary) {
        $resolvedBinary = Get-ResolvedBinaryPath
        if (-not (Test-Path -LiteralPath $resolvedBinary)) {
            throw "Built binary not found: $resolvedBinary"
        }
        $program = $resolvedBinary
        $args = @()
    }
    $args += @(
        "--mode=full",
        "--role=$($Node.Role)",
        "--id=$id",
        "--port=$($Node.Port)",
        "--datadir=$($Node.DataDir)",
        "--rpcaddr", $Node.Rpc
    )
    $configPath = Join-Path $RepoRoot $Config
    if (-not [string]::IsNullOrWhiteSpace($Config) -and (Test-Path -LiteralPath $configPath)) {
        $args += @("--config", $Config)
    }
    $peerAddrs = @()
    if ($Node.Contains("PeerAddrs")) {
        $peerAddrs = @($Node.PeerAddrs | Where-Object { -not [string]::IsNullOrWhiteSpace("$_") })
    }
    if ($peerAddrs.Count -gt 0) {
        $args += @("--peers", ($peerAddrs -join ","))
    }
    $escapedArgs = ($args | ForEach-Object { "'$($_.Replace("'","''"))'" }) -join ","
    $command = @(
        "`$env:MSC_ALLOW_VALIDATOR_KEY_CREATE='1'",
        "`$env:MSC_VALIDATOR_PASSWORD='$($password.Replace("'","''"))'",
        "`$env:MSC_AUTH_OPEN_BROWSER='0'",
        "Set-Location '$($RepoRoot.Replace("'","''"))'",
        "& '$($program.Replace("'","''"))' @($escapedArgs) *> '$($nodeLog.Replace("'","''"))'"
    ) -join "; "
    $proc = Start-Process -FilePath "powershell" -WindowStyle Hidden -PassThru -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $command)
    $Node.Process = $proc
    $Node.StartedAt = Get-Date
    Write-ChaosEvent -Path $EventsPath -Type "node_start" -Fields @{
        node = $Node.Id
        rpc = $Node.RpcUrl
        port = $Node.Port
        role = $Node.Role
        pid = $proc.Id
    }
    return $proc
}

function Wait-NodeRpcReady {
    param(
        [hashtable]$Node,
        [int]$TimeoutSeconds,
        [string]$EventsPath
    )
    $deadline = (Get-Date).AddSeconds([Math]::Max(1, $TimeoutSeconds))
    while ((Get-Date) -lt $deadline) {
        if ($Node.Process -and $Node.Process.HasExited) {
            Write-ChaosEvent -Path $EventsPath -Type "node_start_failed" -Fields @{
                node = $Node.Id
                reason = "process_exited_before_rpc_ready"
                exit_code = $Node.Process.ExitCode
            }
            return $false
        }
        $status = Read-NodeStatus -Node $Node -TimeoutSec 2 -Full $false
        if ($status -and "$($status.node_id)".Trim().ToUpperInvariant() -eq $Node.Id) {
            Write-ChaosEvent -Path $EventsPath -Type "node_rpc_ready" -Fields @{
                node = $Node.Id
                rpc = $Node.RpcUrl
                height = $status.height
                finalized_height = $status.finalized_height
                role = $status.role
            }
            return $true
        }
        Start-Sleep -Seconds 1
    }
    Write-ChaosEvent -Path $EventsPath -Type "node_start_failed" -Fields @{
        node = $Node.Id
        reason = "rpc_not_ready_before_timeout"
        timeout_seconds = $TimeoutSeconds
    }
    return $false
}

function Assert-ValidatorPasswordsPresent {
    param([object[]]$Nodes)
    foreach ($node in $Nodes) {
        if ($node.Role -ne "validator" -or -not $node.Local) {
            continue
        }
        $password = Get-NodePassword -Node $node
        if ([string]::IsNullOrWhiteSpace($password)) {
            throw "Missing validator password before startup for node $($node.Id). Set $($node.PasswordEnv), MSC_VALIDATOR_PASSWORD, or MSC_CHAOS_VALIDATOR_PASSWORD."
        }
        if ($password.Trim() -in @("your-validator-password", "CHANGE_ME", "changeme")) {
            throw "Placeholder validator password configured for node $($node.Id). Set the real validator key password before running chaos."
        }
    }
}

function Assert-LocalRpcPortsFree {
    param([object[]]$Nodes)
    foreach ($node in $Nodes) {
        if (-not $node.Local) {
            continue
        }
        $status = Read-NodeStatus -Node $node -TimeoutSec 1 -Full $false
        if ($status) {
            $runningID = "$($status.node_id)".Trim()
            throw "RPC $($node.RpcUrl) already responds as node '$runningID'. Stop the existing node or run this harness with -SkipStart for controller-only mode."
        }
    }
}

function Stop-ProcessTree {
    param([int]$ProcessId)
    if ($ProcessId -le 0) {
        return
    }
    $children = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$ProcessId" -ErrorAction SilentlyContinue)
    foreach ($child in $children) {
        Stop-ProcessTree -ProcessId ([int]$child.ProcessId)
    }
    Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
}

function Stop-ChaosNode {
    param([hashtable]$Node)
    Stop-CpuPressure -Node $Node
    if ($Node.Contains("Process") -and $Node.Process) {
        Stop-ProcessTree -ProcessId ([int]$Node.Process.Id)
    }
    $idNeedle = "--id=$($Node.Id)"
    $rpcNeedle = "$($Node.Rpc)"
    $dataNeedle = "$($Node.DataDir)"
    $orphans = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
        Where-Object {
            ($_.Name -eq "msc-chain.exe" -or $_.Name -eq "go.exe") -and
            "$($_.CommandLine)" -like "*$idNeedle*" -and
            "$($_.CommandLine)" -like "*$rpcNeedle*" -and
            "$($_.CommandLine)" -like "*$dataNeedle*"
        })
    foreach ($orphan in $orphans) {
        Stop-Process -Id ([int]$orphan.ProcessId) -Force -ErrorAction SilentlyContinue
    }
    $Node.Process = $null
}

function Read-NodePeerIdFromLog {
    param([hashtable]$Node, [string]$RunDir)
    $logPath = Join-Path $RunDir "$($Node.Id).log"
    if (-not (Test-Path -LiteralPath $logPath)) {
        return ""
    }
    $matches = @(Select-String -LiteralPath $logPath -Pattern "peer_id=([A-Za-z0-9]+)" -AllMatches -ErrorAction SilentlyContinue)
    if ($matches.Count -eq 0) {
        return ""
    }
    $last = $matches[-1]
    if ($last.Matches.Count -eq 0) {
        return ""
    }
    return "$($last.Matches[-1].Groups[1].Value)".Trim()
}

function Wait-NodePeerId {
    param([hashtable]$Node, [string]$RunDir, [int]$TimeoutSeconds)
    $deadline = (Get-Date).AddSeconds([Math]::Max(1, $TimeoutSeconds))
    while ((Get-Date) -lt $deadline) {
        $peerId = Read-NodePeerIdFromLog -Node $Node -RunDir $RunDir
        if (-not [string]::IsNullOrWhiteSpace($peerId)) {
            return $peerId
        }
        if ($Node.Process -and $Node.Process.HasExited) {
            return ""
        }
        Start-Sleep -Milliseconds 500
    }
    return ""
}

function Convert-ToPeerHostMultiaddr {
    param([string]$HostName)
    $peerHost = "$HostName".Trim()
    if ([string]::IsNullOrWhiteSpace($peerHost) -or $peerHost -eq "localhost" -or $peerHost -eq "127.0.0.1") {
        return "/ip4/127.0.0.1"
    }
    if ($peerHost -match "^\d{1,3}(\.\d{1,3}){3}$") {
        return "/ip4/$peerHost"
    }
    return "/dns4/$peerHost"
}

function Get-NodePeerMultiaddr {
    param([hashtable]$Node, [string]$PeerId)
    $hostPart = Convert-ToPeerHostMultiaddr -HostName $Node.Host
    return "$hostPart/tcp/$($Node.Port)/p2p/$PeerId"
}

function Save-NodePersistentPeers {
    param([hashtable]$Node, [string[]]$Peers)
    $nodeDir = $Node.NodeDataDir
    if ([string]::IsNullOrWhiteSpace($nodeDir)) {
        $nodeDir = Join-Path $Node.DataDir ("node_" + $Node.Id)
    }
    if (-not [System.IO.Path]::IsPathRooted($nodeDir)) {
        $nodeDir = Join-Path $RepoRoot $nodeDir
    }
    New-Item -ItemType Directory -Force -Path $nodeDir | Out-Null
    $path = Join-Path $nodeDir "peers.json"
    @($Peers) | ConvertTo-Json -Depth 5 | Set-Content -Path $path
}

function Initialize-LocalPeerTopology {
    param([object[]]$Nodes, [string]$RunDir, [string]$EventsPath)
    $localNodes = @($Nodes | Where-Object { $_.Local })
    if ($localNodes.Count -lt 2) {
        return
    }
    Write-ChaosEvent -Path $EventsPath -Type "auto_peer_topology_begin" -Fields @{ nodes = @($localNodes | ForEach-Object { $_.Id }) }
    $startedNodes = @()
    $peerMap = @{}
    try {
        foreach ($node in $localNodes) {
            $node.PeerAddrs = @()
            Start-ChaosNode -Node $node -RunDir $RunDir -EventsPath $EventsPath | Out-Null
            $startedNodes += $node
            if (-not (Wait-NodeRpcReady -Node $node -TimeoutSeconds $StartupWaitSeconds -EventsPath $EventsPath)) {
                throw "Node $($node.Id) did not become RPC-ready during auto peer topology bootstrap. Check $RunDir\\$($node.Id).log"
            }
        }
        foreach ($node in $localNodes) {
            $peerId = Wait-NodePeerId -Node $node -RunDir $RunDir -TimeoutSeconds 30
            if ([string]::IsNullOrWhiteSpace($peerId)) {
                throw "Node $($node.Id) did not publish a libp2p peer_id during auto peer topology bootstrap. Check $RunDir\\$($node.Id).log"
            }
            $peerMap[$node.Id] = Get-NodePeerMultiaddr -Node $node -PeerId $peerId
        }
        foreach ($node in $localNodes) {
            $peers = @()
            foreach ($other in $localNodes) {
                if ($other.Id -eq $node.Id) {
                    continue
                }
                $peers += $peerMap[$other.Id]
            }
            $node.PeerAddrs = $peers
            Save-NodePersistentPeers -Node $node -Peers $peers
            Write-ChaosEvent -Path $EventsPath -Type "auto_peer_topology_node" -Fields @{
                node = $node.Id
                peer_addr = $peerMap[$node.Id]
                peers = $peers
            }
        }
    } finally {
        foreach ($node in $startedNodes) {
            Stop-ChaosNode -Node $node
        }
    }
    Start-Sleep -Seconds 2
    Write-ChaosEvent -Path $EventsPath -Type "auto_peer_topology_done" -Fields @{ peer_count = $peerMap.Count }
}

function Convert-BodyToJson {
    param([string]$Body)
    if ([string]::IsNullOrWhiteSpace($Body)) {
        return $null
    }
    try {
        return $Body | ConvertFrom-Json
    } catch {
        return $null
    }
}

function Invoke-ChaosJson {
    param([string]$Url, [int]$TimeoutSec = 3)
    try {
        return Invoke-RestMethod -Uri $Url -TimeoutSec $TimeoutSec
    } catch {
        $resp = $_.Exception.Response
        if ($null -eq $resp) {
            return $null
        }
        try {
            $stream = $resp.GetResponseStream()
            if ($null -eq $stream) {
                return $null
            }
            $reader = New-Object System.IO.StreamReader($stream)
            $body = $reader.ReadToEnd()
            $reader.Close()
            return Convert-BodyToJson -Body $body
        } catch {
            return $null
        }
    }
}

function Read-NodeStatus {
    param([hashtable]$Node, [int]$TimeoutSec, [bool]$Full)
    $suffix = ""
    if ($Full) {
        $suffix = "?full=1"
    }
    return Invoke-ChaosJson -Url "$($Node.RpcUrl)/status$suffix" -TimeoutSec $TimeoutSec
}

function Read-BlockHashAtHeight {
    param([hashtable]$Node, [uint64]$Height, [int]$TimeoutSec)
    if ($Height -le 0) {
        return ""
    }
    $block = Invoke-ChaosJson -Url "$($Node.RpcUrl)/explorer/block?height=$Height" -TimeoutSec $TimeoutSec
    if ($null -eq $block) {
        return ""
    }
    $hash = "$($block.hash)".Trim()
    if ($hash -eq "" -and $block.summary) {
        $hash = "$($block.summary.hash)".Trim()
    }
    return $hash
}

function Write-ChaosEvent {
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
    ($event | ConvertTo-Json -Compress -Depth 16) | Add-Content -Path $Path
}

function Submit-FaucetFlood {
    param(
        [hashtable]$Node,
        [int]$Batch,
        [string]$EventsPath
    )
    for ($i = 0; $i -lt $Batch; $i++) {
        $addr = "MSC_CHAOS_" + ([guid]::NewGuid().ToString("N").Substring(0, 24))
        $body = @{ address = $addr; amount = 1; coin = "MSC" } | ConvertTo-Json -Compress
        try {
            $resp = Invoke-WebRequest -Method Post -Uri "$($Node.RpcUrl)/faucet" -ContentType "application/json" -Body $body -TimeoutSec 4 -UseBasicParsing
            Write-ChaosEvent -Path $EventsPath -Type "tx_flood_submit" -Fields @{
                node = $Node.Id
                status = [int]$resp.StatusCode
                address = $addr
            }
        } catch {
            $status = 0
            if ($_.Exception.Response) {
                try { $status = [int]$_.Exception.Response.StatusCode } catch { $status = 0 }
            }
            Write-ChaosEvent -Path $EventsPath -Type "tx_flood_submit_failed" -Fields @{
                node = $Node.Id
                status = $status
                error = $_.Exception.Message
            }
        }
    }
}

function Test-WindowsAdmin {
    if ($PSVersionTable.Platform -and $PSVersionTable.Platform -ne "Win32NT") {
        return $false
    }
    try {
        $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
        $principal = New-Object Security.Principal.WindowsPrincipal($identity)
        return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    } catch {
        return $false
    }
}

function Start-FirewallIsolation {
    param(
        [hashtable]$Node,
        [int]$Seconds,
        [string]$RunId,
        [string]$EventsPath,
        [string]$Reason
    )
    if (-not $Node.Local) {
        Write-ChaosEvent -Path $EventsPath -Type "network_isolation_skipped" -Fields @{
            node = $Node.Id
            reason = "remote_node_no_local_firewall"
            host = $Node.Host
        }
        return $false
    }
    if (-not (Get-Command New-NetFirewallRule -ErrorAction SilentlyContinue)) {
        Write-ChaosEvent -Path $EventsPath -Type "network_isolation_skipped" -Fields @{
            node = $Node.Id
            reason = "firewall_cmdlets_unavailable"
        }
        return $false
    }
    if (-not (Test-WindowsAdmin)) {
        Write-ChaosEvent -Path $EventsPath -Type "network_isolation_skipped" -Fields @{
            node = $Node.Id
            reason = "admin_required_for_windows_firewall"
        }
        return $false
    }

    $ruleNames = @()
    $ports = @([int]$Node.Port)
    if ($IncludeRpcInIsolation) {
        $ports += [int]$Node.RpcPort
    }
    foreach ($port in ($ports | Sort-Object -Unique)) {
        foreach ($direction in @("Inbound", "Outbound")) {
            $name = "MSC Chaos $RunId $($Node.Id) $Reason $direction $port"
            if ($direction -eq "Inbound") {
                New-NetFirewallRule -DisplayName $name -Direction Inbound -Action Block -Protocol TCP -LocalPort $port -Profile Any | Out-Null
            } else {
                New-NetFirewallRule -DisplayName $name -Direction Outbound -Action Block -Protocol TCP -RemotePort $port -Profile Any | Out-Null
            }
            $ruleNames += $name
        }
    }
    $Node.IsolationRules = @($Node.IsolationRules + $ruleNames)
    $Node.IsolationUntil = (Get-Date).AddSeconds($Seconds)
    Write-ChaosEvent -Path $EventsPath -Type "network_isolation_begin" -Fields @{
        node = $Node.Id
        seconds = $Seconds
        reason = $Reason
        ports = $ports
        rules = $ruleNames
    }
    return $true
}

function Stop-FirewallIsolation {
    param([hashtable]$Node, [string]$EventsPath, [string]$Reason)
    $rules = @($Node.IsolationRules)
    foreach ($name in $rules) {
        Remove-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
    }
    if ($rules.Count -gt 0) {
        Write-ChaosEvent -Path $EventsPath -Type "network_isolation_end" -Fields @{
            node = $Node.Id
            reason = $Reason
            rules = $rules
        }
    }
    $Node.IsolationRules = @()
    $Node.IsolationUntil = $null
}

function Invoke-RemoteNetem {
    param(
        [hashtable]$Node,
        [bool]$Enable,
        [string]$EventsPath
    )
    if ([string]::IsNullOrWhiteSpace($Node.SshTarget)) {
        Write-ChaosEvent -Path $EventsPath -Type "netem_skipped" -Fields @{
            node = $Node.Id
            reason = "ssh_target_missing"
            host = $Node.Host
        }
        return $false
    }
    if (-not (Get-Command ssh -ErrorAction SilentlyContinue)) {
        Write-ChaosEvent -Path $EventsPath -Type "netem_skipped" -Fields @{
            node = $Node.Id
            reason = "ssh_not_found"
        }
        return $false
    }
    if ($Enable) {
        $cmd = "sudo tc qdisc replace dev $NetemInterface root netem delay ${LatencyMs}ms ${LatencyJitterMs}ms loss ${PacketLossPercent}%"
    } else {
        $cmd = "sudo tc qdisc del dev $NetemInterface root 2>/dev/null || true"
    }
    try {
        & ssh $Node.SshTarget $cmd | Out-Null
        Write-ChaosEvent -Path $EventsPath -Type $(if ($Enable) { "netem_begin" } else { "netem_end" }) -Fields @{
            node = $Node.Id
            ssh = $Node.SshTarget
            interface = $NetemInterface
            latency_ms = $LatencyMs
            jitter_ms = $LatencyJitterMs
            packet_loss_percent = $PacketLossPercent
        }
        return $true
    } catch {
        Write-ChaosEvent -Path $EventsPath -Type "netem_failed" -Fields @{
            node = $Node.Id
            ssh = $Node.SshTarget
            error = $_.Exception.Message
        }
        return $false
    }
}

function Start-PacketLossFault {
    param([hashtable]$Node, [int]$Seconds, [string]$RunId, [string]$EventsPath)
    if ($UseRemoteNetem -and (-not $Node.Local)) {
        if (Invoke-RemoteNetem -Node $Node -Enable $true -EventsPath $EventsPath) {
            $Node.NetemUntil = (Get-Date).AddSeconds($Seconds)
            return
        }
    }
    $pulseSeconds = [Math]::Max(1, [Math]::Min($Seconds, [Math]::Ceiling($Seconds * $PacketLossPercent / 100.0)))
    if (-not (Start-FirewallIsolation -Node $Node -Seconds $pulseSeconds -RunId $RunId -EventsPath $EventsPath -Reason "packet_loss_pulse")) {
        Write-ChaosEvent -Path $EventsPath -Type "packet_loss_degraded" -Fields @{
            node = $Node.Id
            requested_seconds = $Seconds
            pulse_seconds = $pulseSeconds
            loss_percent = $PacketLossPercent
            reason = "netem_unavailable"
        }
    }
}

function Set-ProcessTreePriority {
    param([int]$ProcessId, [string]$Priority)
    if ($ProcessId -le 0) {
        return
    }
    try {
        $p = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
        if ($p) {
            $p.PriorityClass = $Priority
        }
    } catch {
    }
    $children = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$ProcessId" -ErrorAction SilentlyContinue)
    foreach ($child in $children) {
        Set-ProcessTreePriority -ProcessId ([int]$child.ProcessId) -Priority $Priority
    }
}

function Start-CpuPressure {
    param([hashtable]$Node, [int]$Seconds)
    Stop-CpuPressure -Node $Node
    $workers = @()
    for ($i = 0; $i -lt $CpuPressureWorkers; $i++) {
        $script = "`$deadline=(Get-Date).AddSeconds($Seconds); while((Get-Date) -lt `$deadline){ [Math]::Sqrt(123456.789) | Out-Null }"
        $workers += Start-Process -FilePath "powershell" -WindowStyle Hidden -PassThru -ArgumentList @("-NoProfile", "-Command", $script)
    }
    $Node.CpuLoad = $workers
}

function Stop-CpuPressure {
    param([hashtable]$Node)
    foreach ($p in @($Node.CpuLoad)) {
        try {
            if ($p -and -not $p.HasExited) {
                Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
            }
        } catch {
        }
    }
    $Node.CpuLoad = @()
}

function Start-SlowValidatorFault {
    param([hashtable]$Node, [int]$Seconds, [string]$EventsPath)
    if (-not $Node.Local -or -not $Node.Process) {
        Write-ChaosEvent -Path $EventsPath -Type "slow_validator_skipped" -Fields @{
            node = $Node.Id
            reason = "local_process_required"
        }
        return
    }
    Set-ProcessTreePriority -ProcessId ([int]$Node.Process.Id) -Priority "Idle"
    if ($CpuPressureWorkers -gt 0) {
        Start-CpuPressure -Node $Node -Seconds $Seconds
    }
    $Node.SlowUntil = (Get-Date).AddSeconds($Seconds)
    Write-ChaosEvent -Path $EventsPath -Type "slow_validator_begin" -Fields @{
        node = $Node.Id
        seconds = $Seconds
        priority = "Idle"
        cpu_pressure_workers = $CpuPressureWorkers
    }
}

function Stop-SlowValidatorFault {
    param([hashtable]$Node, [string]$EventsPath)
    Stop-CpuPressure -Node $Node
    if ($Node.Local -and $Node.Process) {
        Set-ProcessTreePriority -ProcessId ([int]$Node.Process.Id) -Priority "Normal"
    }
    Write-ChaosEvent -Path $EventsPath -Type "slow_validator_end" -Fields @{ node = $Node.Id }
    $Node.SlowUntil = $null
}

function Get-LatestSnapshotDirs {
    param([hashtable]$Node)
    $snapRoot = Join-Path $Node.NodeDataDir "snapshots"
    if (-not (Test-Path -LiteralPath $snapRoot)) {
        return @()
    }
    return @(Get-ChildItem -LiteralPath $snapRoot -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^\d+$' } |
        Sort-Object -Property Name -Descending)
}

function Hide-LatestSnapshots {
    param([hashtable]$Node, [int]$Count, [string]$RunId, [string]$EventsPath)
    $dirs = @(Get-LatestSnapshotDirs -Node $Node | Select-Object -First $Count)
    if ($dirs.Count -eq 0) {
        Write-ChaosEvent -Path $EventsPath -Type "stale_snapshot_skipped" -Fields @{
            node = $Node.Id
            reason = "no_snapshot_dirs"
            node_data_dir = $Node.NodeDataDir
        }
        return 0
    }
    $hidden = @()
    foreach ($dir in $dirs) {
        $dest = Join-Path $dir.Parent.FullName ($dir.Name + ".chaos_hidden_" + $RunId)
        Move-Item -LiteralPath $dir.FullName -Destination $dest -Force
        $hidden += $dest
    }
    Write-ChaosEvent -Path $EventsPath -Type "stale_snapshot_injected" -Fields @{
        node = $Node.Id
        hidden = $hidden
        count = $hidden.Count
    }
    return $hidden.Count
}

function Heal-ExpiredFaults {
    param([object[]]$Nodes, [string]$EventsPath)
    $now = Get-Date
    foreach ($node in $Nodes) {
        if ($node.IsolationUntil -and $now -ge $node.IsolationUntil) {
            Stop-FirewallIsolation -Node $node -EventsPath $EventsPath -Reason "expired"
        }
        if ($node.NetemUntil -and $now -ge $node.NetemUntil) {
            [void](Invoke-RemoteNetem -Node $node -Enable $false -EventsPath $EventsPath)
            $node.NetemUntil = $null
        }
        if ($node.SlowUntil -and $now -ge $node.SlowUntil) {
            Stop-SlowValidatorFault -Node $node -EventsPath $EventsPath
        }
    }
}

function Choose-FaultNode {
    param([object[]]$Nodes, [bool]$PreferValidator)
    $eligible = @($Nodes)
    if ($PreferValidator -and (Get-Random -Minimum 1 -Maximum 101) -le $ValidatorFaultBiasPercent) {
        $validators = @($Nodes | Where-Object { $_.Role -eq "validator" })
        if ($validators.Count -gt 0) {
            $eligible = $validators
        }
    }
    return @($eligible | Get-Random -Count 1)[0]
}

function Choose-FaultNodes {
    param([object[]]$Nodes, [int]$Count, [bool]$PreferValidator)
    $pool = @($Nodes)
    if ($PreferValidator -and (Get-Random -Minimum 1 -Maximum 101) -le $ValidatorFaultBiasPercent) {
        $validators = @($Nodes | Where-Object { $_.Role -eq "validator" })
        if ($validators.Count -ge $Count) {
            $pool = $validators
        }
    }
    $take = [Math]::Min($Count, $pool.Count)
    return @($pool | Get-Random -Count $take)
}

function New-StatusRow {
    param([hashtable]$Node, [object]$Status)
    $reachable = $false
    $height = [int64]0
    $finalized = [int64]0
    $state = "unreachable"
    $ready = $false
    $syncing = $false
    $mempoolDepth = 0
    $peers = 0
    $mode = ""
    $waitReason = ""
    $consensusMode = ""
    $lastBlockAge = 0
    $live = 0
    $required = 0
    if ($status) {
        $reachable = $true
        try { $height = [int64]($status.height) } catch { $height = 0 }
        try { $finalized = [int64]($status.finalized_height) } catch { $finalized = $height }
        $state = [string]($status.state)
        $ready = [bool]($status.ready)
        $syncing = [bool]($status.syncing)
        try { $mempoolDepth = [int]($status.mempool_depth) } catch { $mempoolDepth = 0 }
        try { $peers = [int]($status.peers) } catch { $peers = 0 }
        $mode = [string]($status.quorum_policy_mode)
        $waitReason = [string]($status.wait_reason)
        $consensusMode = [string]($status.consensus_mode)
        try { $lastBlockAge = [int]($status.last_block_age_seconds) } catch { $lastBlockAge = 0 }
        try { $live = [int]($status.live_validators) } catch { $live = 0 }
        try { $required = [int]($status.required_quorum) } catch { $required = 0 }
    }
    return [pscustomobject]@{
        Node = $Node.Id
        Rpc = $Node.RpcUrl
        Role = $Node.Role
        Reachable = $reachable
        Height = $height
        Finalized = $finalized
        Ready = $ready
        Syncing = $syncing
        State = $state
        Peers = $peers
        MempoolDepth = $mempoolDepth
        Mode = $mode
        ConsensusMode = $consensusMode
        WaitReason = $waitReason
        LastBlockAgeSeconds = $lastBlockAge
        LiveValidators = $live
        RequiredQuorum = $required
    }
}

function Write-SampleLine {
    param(
        [int]$Sample,
        [int]$Reachable,
        [int]$Ready,
        [int64]$MinHeight,
        [int64]$MaxHeight,
        [int64]$MinFinalized,
        [int64]$MaxFinalized,
        [int]$GapSeconds,
        [int64]$FinalizedLag,
        [int64]$HeightLag,
        [int]$Warnings
    )
    Write-Host ("sample={0} reachable={1} ready={2} height={3}-{4} finalized={5}-{6} gap_s={7} lag_f={8} lag_h={9} warnings={10}" -f `
        $Sample, $Reachable, $Ready, $MinHeight, $MaxHeight, $MinFinalized, $MaxFinalized, $GapSeconds, $FinalizedLag, $HeightLag, $Warnings)
}

function Write-Plan {
    param([object[]]$Nodes)
    Write-Host ""
    Write-Host "=== Tier 1 Chaos Plan ==="
    Write-Host ("duration_seconds={0} warmup_seconds={1} sample_seconds={2}" -f $DurationSeconds, $WarmupSeconds, $SampleSeconds)
    Write-Host ("nodes={0} validators={1} skip_start={2} built_binary={3}" -f $Nodes.Count, @($Nodes | Where-Object { $_.Role -eq "validator" }).Count, $SkipStart.IsPresent, $UseBuiltBinary.IsPresent)
    Write-Host ("faults: restarts={0} restart_storms={1} network={2} packet_loss={3} slow={4} stale_snapshots={5} tx_flood={6}" -f `
        (-not $NoRandomRestarts.IsPresent), $script:EnableRestartStorms, $script:EnableNetworkChaos, $script:EnablePacketLoss, $script:EnableSlowValidators, $script:EnableStaleSnapshots, $script:EnableTxFlood)
    Write-Host ""
    $Nodes |
        ForEach-Object {
            [pscustomobject]@{
                Id = $_.Id
                Role = $_.Role
                Host = $_.Host
                Port = $_.Port
                RpcUrl = $_.RpcUrl
                Local = $_.Local
                SshTarget = $_.SshTarget
                DataDir = $_.DataDir
            }
        } |
        Format-Table -AutoSize
    Write-Host ""
    if (-not $SkipStart) {
        Write-Host "Start commands:"
        foreach ($node in $Nodes) {
            if ($node.Local) {
                Write-Host ("[{0}] {1}" -f $node.Id, (Get-StartCommandPreview -Node $node))
            } else {
                Write-Host ("[{0}] remote node, start manually on {1}; controller will monitor {2}" -f $node.Id, $node.Host, $node.RpcUrl)
            }
        }
    }
    Write-Host ""
}

if ($DurationMinutes -gt 0) {
    $DurationSeconds = $DurationMinutes * 60
}
if ($DurationSeconds -le 0) { throw "DurationSeconds must be > 0" }
if ($NodeCount -lt 1 -or $NodeCount -gt 50) { throw "NodeCount must be between 1 and 50" }
if ($ValidatorNodeCount -lt 0 -or $ValidatorNodeCount -gt $NodeCount) { throw "ValidatorNodeCount must be between 0 and NodeCount" }
if ($SampleSeconds -lt 1) { throw "SampleSeconds must be >= 1" }
if ($RestartStormSize -lt 1) { $RestartStormSize = 1 }
if ($RestartDownMaxSeconds -lt $RestartDownMinSeconds) { $RestartDownMaxSeconds = $RestartDownMinSeconds }
if ($MaxRestartSeconds -lt $MinRestartSeconds) { $MaxRestartSeconds = $MinRestartSeconds }
if ($IsolationMaxSeconds -lt $IsolationMinSeconds) { $IsolationMaxSeconds = $IsolationMinSeconds }
if ($PacketLossMaxSeconds -lt $PacketLossMinSeconds) { $PacketLossMaxSeconds = $PacketLossMinSeconds }
if ($SlowMaxSeconds -lt $SlowMinSeconds) { $SlowMaxSeconds = $SlowMinSeconds }
if ($StatusTimeoutSeconds -lt 1) { $StatusTimeoutSeconds = 1 }
if ($StartupWaitSeconds -lt 1) { $StartupWaitSeconds = 1 }
if ($FullStatusEverySamples -lt 1) { $FullStatusEverySamples = 1 }
if ($ForkCheckEverySamples -lt 1) { $ForkCheckEverySamples = 1 }
if ($TxFloodBatch -lt 1) { $TxFloodBatch = 1 }
if ($StaleSnapshotHideNewest -lt 1) { $StaleSnapshotHideNewest = 1 }
if ($ValidatorFaultBiasPercent -lt 0) { $ValidatorFaultBiasPercent = 0 }
if ($ValidatorFaultBiasPercent -gt 100) { $ValidatorFaultBiasPercent = 100 }

$script:EnableRestartStorms = $RestartStorms.IsPresent -or $Tier1Survival.IsPresent
$script:EnableNetworkChaos = $NetworkChaos.IsPresent -or $Tier1Survival.IsPresent
$script:EnablePacketLoss = $PacketLoss.IsPresent -or $Tier1Survival.IsPresent
$script:EnableSlowValidators = $SlowValidators.IsPresent -or $Tier1Survival.IsPresent
$script:RequestedStaleSnapshots = $StaleSnapshots.IsPresent -or $Tier1Survival.IsPresent
$script:EnableStaleSnapshots = $script:RequestedStaleSnapshots -and $AllowSnapshotMutation.IsPresent
$script:EnableTxFlood = $TxFlood.IsPresent -or $Tier1Survival.IsPresent

if (-not [string]::IsNullOrWhiteSpace($TopologyPath)) {
    $nodes = New-ChaosNodesFromTopology -Path $TopologyPath
    $NodeCount = $nodes.Count
} else {
    $ids = New-ChaosNodeIds -Count $NodeCount -RawIds $NodeIds
    $nodes = New-ChaosNodesFromDefaults -Ids $ids
}
if ($nodes.Count -lt 1 -or $nodes.Count -gt 50) {
    throw "chaos topology must contain between 1 and 50 nodes; got $($nodes.Count)"
}
if ($MinReachableNodes -le 0) {
    $MinReachableNodes = [Math]::Max(1, [Math]::Floor($nodes.Count / 2) + 1)
}

$runId = "mainnet-chaos-" + (Get-Date -Format "yyyyMMdd-HHmmss")
$runDir = Join-Path (Join-Path $RepoRoot $LogRoot) $runId
$eventsPath = Join-Path $runDir "events.jsonl"
$summaryPath = Join-Path $runDir "summary.json"

Write-Plan -Nodes $nodes
if ($script:RequestedStaleSnapshots -and -not $AllowSnapshotMutation) {
    Write-Host "Stale snapshot mutation is requested but disabled. Add -AllowSnapshotMutation to hide newest snapshot dirs during the run."
}
if ($DryRun) {
    Write-Host "Dry run only. No nodes started and no faults injected."
    exit 0
}

New-Item -ItemType Directory -Force -Path $runDir | Out-Null

Write-ChaosEvent -Path $eventsPath -Type "chaos_start" -Fields @{
    duration_seconds = $DurationSeconds
    warmup_seconds = $WarmupSeconds
    node_count = $nodes.Count
    validator_node_count = @($nodes | Where-Object { $_.Role -eq "validator" }).Count
    max_block_gap_seconds = $MaxBlockGapSeconds
    max_finalized_lag_blocks = $MaxFinalizedLagBlocks
    max_height_lag_blocks = $MaxHeightLagBlocks
    min_reachable_nodes = $MinReachableNodes
    status_timeout_seconds = $StatusTimeoutSeconds
    random_restarts = (-not $NoRandomRestarts.IsPresent)
    restart_storms = $script:EnableRestartStorms
    network_chaos = $script:EnableNetworkChaos
    packet_loss = $script:EnablePacketLoss
    remote_netem = $UseRemoteNetem.IsPresent
    slow_validators = $script:EnableSlowValidators
    stale_snapshots_requested = $script:RequestedStaleSnapshots
    allow_snapshot_mutation = $AllowSnapshotMutation.IsPresent
    stale_snapshots = $script:EnableStaleSnapshots
    tx_flood = $script:EnableTxFlood
    auto_local_peers = $AutoLocalPeers.IsPresent
}

if (-not $SkipStart) {
    Assert-ValidatorPasswordsPresent -Nodes $nodes
    Assert-LocalRpcPortsFree -Nodes $nodes
    if ($AutoLocalPeers.IsPresent) {
        Initialize-LocalPeerTopology -Nodes $nodes -RunDir $runDir -EventsPath $eventsPath
        Assert-LocalRpcPortsFree -Nodes $nodes
    }
    $startedNodes = @()
    foreach ($node in $nodes) {
        try {
            Start-ChaosNode -Node $node -RunDir $runDir -EventsPath $eventsPath | Out-Null
            $startedNodes += $node
            if (-not (Wait-NodeRpcReady -Node $node -TimeoutSeconds $StartupWaitSeconds -EventsPath $eventsPath)) {
                throw "Node $($node.Id) did not become RPC-ready within $StartupWaitSeconds seconds. Check $runDir\\$($node.Id).log"
            }
        } catch {
            foreach ($started in $startedNodes) {
                if ($started.Local) {
                    Stop-ChaosNode -Node $started
                }
            }
            throw
        }
    }
} else {
    foreach ($node in $nodes) {
        Write-ChaosEvent -Path $eventsPath -Type "node_monitor" -Fields @{ node = $node.Id; rpc = $node.RpcUrl; port = $node.Port; role = $node.Role; host = $node.Host }
    }
}

$startAt = Get-Date
$deadline = $startAt.AddSeconds($DurationSeconds)
$nextRestart = (Get-Date).AddSeconds((Get-Random -Minimum $MinRestartSeconds -Maximum ($MaxRestartSeconds + 1)))
$nextFlood = (Get-Date).AddSeconds([Math]::Max(1, $TxFloodEverySeconds))
$nextIsolation = (Get-Date).AddSeconds((Get-Random -Minimum 15 -Maximum ($IsolationEverySeconds + 1)))
$nextPacketLoss = (Get-Date).AddSeconds((Get-Random -Minimum 10 -Maximum ($PacketLossEverySeconds + 1)))
$nextSlow = (Get-Date).AddSeconds((Get-Random -Minimum 20 -Maximum ($SlowEverySeconds + 1)))
$nextStaleSnapshot = (Get-Date).AddSeconds((Get-Random -Minimum 30 -Maximum ($StaleSnapshotEverySeconds + 1)))
$lastMaxFinalized = [int64]0
$lastMaxHeight = [int64]0
$lastProgressAt = Get-Date
$warningCount = 0
$forkWarningCount = 0
$maxObservedGap = 0
$maxObservedFinalizedLag = [int64]0
$maxObservedHeightLag = [int64]0
$samples = 0

try {
    while ((Get-Date) -lt $deadline) {
        Heal-ExpiredFaults -Nodes $nodes -EventsPath $eventsPath

        $samples++
        $fullStatus = (($samples % $FullStatusEverySamples) -eq 0)
        $statuses = @()
        foreach ($node in $nodes) {
            $status = Read-NodeStatus -Node $node -TimeoutSec $StatusTimeoutSeconds -Full $fullStatus
            $statuses += New-StatusRow -Node $node -Status $status
        }

        $reachableRows = @($statuses | Where-Object { $_.Reachable })
        $readyRows = @($statuses | Where-Object { $_.Ready })
        $maxFinalized = [int64]0
        $minFinalized = [int64]0
        $maxHeight = [int64]0
        $minHeight = [int64]0
        if ($reachableRows.Count -gt 0) {
            $maxFinalized = [int64](($reachableRows | Measure-Object -Property Finalized -Maximum).Maximum)
            $minFinalized = [int64](($reachableRows | Measure-Object -Property Finalized -Minimum).Minimum)
            $maxHeight = [int64](($reachableRows | Measure-Object -Property Height -Maximum).Maximum)
            $minHeight = [int64](($reachableRows | Measure-Object -Property Height -Minimum).Minimum)
        }
        if ($maxFinalized -gt $lastMaxFinalized) {
            $lastMaxFinalized = $maxFinalized
            $lastProgressAt = Get-Date
        }
        if ($maxHeight -gt $lastMaxHeight) {
            $lastMaxHeight = $maxHeight
        }
        $gapSeconds = [int]((Get-Date) - $lastProgressAt).TotalSeconds
        if ($gapSeconds -gt $maxObservedGap) { $maxObservedGap = $gapSeconds }
        $finalizedLag = [int64]($maxFinalized - $minFinalized)
        if ($finalizedLag -gt $maxObservedFinalizedLag) { $maxObservedFinalizedLag = $finalizedLag }
        $heightLag = [int64]($maxHeight - $minHeight)
        if ($heightLag -gt $maxObservedHeightLag) { $maxObservedHeightLag = $heightLag }
        $pastWarmup = ((Get-Date) - $startAt).TotalSeconds -ge $WarmupSeconds

        Write-ChaosEvent -Path $eventsPath -Type "status_sample" -Fields @{
            sample = $samples
            full_status = $fullStatus
            reachable_count = $reachableRows.Count
            ready_count = $readyRows.Count
            max_height = $maxHeight
            min_height = $minHeight
            max_finalized = $maxFinalized
            min_finalized = $minFinalized
            finalized_lag_blocks = $finalizedLag
            height_lag_blocks = $heightLag
            no_progress_seconds = $gapSeconds
            nodes = $statuses
        }

        if ($pastWarmup -and $gapSeconds -gt $MaxBlockGapSeconds) {
            $warningCount++
            Write-ChaosEvent -Path $eventsPath -Type "block_gap_warning" -Fields @{
                max_finalized = $maxFinalized
                no_progress_seconds = $gapSeconds
                limit_seconds = $MaxBlockGapSeconds
            }
        }
        if ($pastWarmup -and $reachableRows.Count -lt $MinReachableNodes) {
            $warningCount++
            Write-ChaosEvent -Path $eventsPath -Type "reachable_quorum_warning" -Fields @{
                reachable_count = $reachableRows.Count
                min_reachable_nodes = $MinReachableNodes
                node_count = $nodes.Count
            }
        }
        if ($pastWarmup -and $finalizedLag -gt $MaxFinalizedLagBlocks) {
            $warningCount++
            Write-ChaosEvent -Path $eventsPath -Type "finalized_lag_warning" -Fields @{
                max_finalized = $maxFinalized
                min_finalized = $minFinalized
                lag_blocks = $finalizedLag
                limit_blocks = $MaxFinalizedLagBlocks
            }
        }
        if ($pastWarmup -and $heightLag -gt $MaxHeightLagBlocks) {
            $warningCount++
            Write-ChaosEvent -Path $eventsPath -Type "height_lag_warning" -Fields @{
                max_height = $maxHeight
                min_height = $minHeight
                lag_blocks = $heightLag
                limit_blocks = $MaxHeightLagBlocks
            }
        }

        if ($pastWarmup -and $reachableRows.Count -gt 1 -and $minFinalized -gt 0 -and (($samples % $ForkCheckEverySamples) -eq 0)) {
            $hashRows = @()
            foreach ($row in $reachableRows) {
                if ([int64]$row.Finalized -lt $minFinalized) {
                    continue
                }
                $node = @($nodes | Where-Object { $_.Id -eq $row.Node })[0]
                $hash = Read-BlockHashAtHeight -Node $node -Height ([uint64]$minFinalized) -TimeoutSec $StatusTimeoutSeconds
                if (-not [string]::IsNullOrWhiteSpace($hash)) {
                    $hashRows += [pscustomobject]@{ node = $node.Id; height = $minFinalized; hash = $hash }
                }
            }
            $hashGroups = @($hashRows | Group-Object -Property hash)
            Write-ChaosEvent -Path $eventsPath -Type "fork_check" -Fields @{
                height = $minFinalized
                responses = $hashRows.Count
                unique_hashes = $hashGroups.Count
                hashes = $hashRows
            }
            if ($hashGroups.Count -gt 1) {
                $warningCount++
                $forkWarningCount++
                Write-ChaosEvent -Path $eventsPath -Type "fork_divergence_warning" -Fields @{
                    height = $minFinalized
                    groups = $hashGroups
                }
            }
        }

        Write-SampleLine -Sample $samples -Reachable $reachableRows.Count -Ready $readyRows.Count -MinHeight $minHeight -MaxHeight $maxHeight -MinFinalized $minFinalized -MaxFinalized $maxFinalized -GapSeconds $gapSeconds -FinalizedLag $finalizedLag -HeightLag $heightLag -Warnings $warningCount

        if ($pastWarmup -and $script:EnableTxFlood -and (Get-Date) -ge $nextFlood) {
            if ($reachableRows.Count -gt 0) {
                $reachableIDs = @($reachableRows | ForEach-Object { $_.Node })
                $targetID = $reachableIDs | Get-Random
                $target = @($nodes | Where-Object { $_.Id -eq $targetID })[0]
                Submit-FaucetFlood -Node $target -Batch $TxFloodBatch -EventsPath $eventsPath
            } else {
                Write-ChaosEvent -Path $eventsPath -Type "tx_flood_skipped" -Fields @{ reason = "no_reachable_rpc" }
            }
            $nextFlood = (Get-Date).AddSeconds([Math]::Max(1, $TxFloodEverySeconds))
        }

        if ($pastWarmup -and -not $NoRandomRestarts -and (Get-Date) -ge $nextRestart) {
            $size = 1
            if ($script:EnableRestartStorms) {
                $size = [Math]::Min($RestartStormSize, $nodes.Count)
            }
            $candidates = @(Choose-FaultNodes -Nodes $nodes -Count $size -PreferValidator $true)
            Write-ChaosEvent -Path $eventsPath -Type "restart_storm_begin" -Fields @{ nodes = @($candidates | ForEach-Object { $_.Id }) }
            foreach ($candidate in $candidates) {
                if ($candidate.Local -and -not $SkipStart) {
                    Stop-ChaosNode -Node $candidate
                    Write-ChaosEvent -Path $eventsPath -Type "node_stop" -Fields @{ node = $candidate.Id; reason = "restart_storm" }
                } else {
                    Write-ChaosEvent -Path $eventsPath -Type "node_stop_skipped" -Fields @{ node = $candidate.Id; reason = "not_locally_started" }
                }
            }
            Start-Sleep -Seconds (Get-Random -Minimum $RestartDownMinSeconds -Maximum ($RestartDownMaxSeconds + 1))
            foreach ($candidate in $candidates) {
                if ($candidate.Local -and -not $SkipStart) {
                    Start-ChaosNode -Node $candidate -RunDir $runDir -EventsPath $eventsPath | Out-Null
                    Write-ChaosEvent -Path $eventsPath -Type "node_restart" -Fields @{ node = $candidate.Id; reason = "restart_storm" }
                }
            }
            Write-ChaosEvent -Path $eventsPath -Type "restart_storm_done" -Fields @{ nodes = @($candidates | ForEach-Object { $_.Id }) }
            $nextRestart = (Get-Date).AddSeconds((Get-Random -Minimum $MinRestartSeconds -Maximum ($MaxRestartSeconds + 1)))
        }

        if ($pastWarmup -and $script:EnableNetworkChaos -and (Get-Date) -ge $nextIsolation) {
            $target = Choose-FaultNode -Nodes $nodes -PreferValidator $true
            $seconds = Get-Random -Minimum $IsolationMinSeconds -Maximum ($IsolationMaxSeconds + 1)
            [void](Start-FirewallIsolation -Node $target -Seconds $seconds -RunId $runId -EventsPath $eventsPath -Reason "disconnect")
            $nextIsolation = (Get-Date).AddSeconds((Get-Random -Minimum 15 -Maximum ($IsolationEverySeconds + 1)))
        }

        if ($pastWarmup -and $script:EnablePacketLoss -and (Get-Date) -ge $nextPacketLoss) {
            $target = Choose-FaultNode -Nodes $nodes -PreferValidator $true
            $seconds = Get-Random -Minimum $PacketLossMinSeconds -Maximum ($PacketLossMaxSeconds + 1)
            Start-PacketLossFault -Node $target -Seconds $seconds -RunId $runId -EventsPath $eventsPath
            $nextPacketLoss = (Get-Date).AddSeconds((Get-Random -Minimum 10 -Maximum ($PacketLossEverySeconds + 1)))
        }

        if ($pastWarmup -and $script:EnableSlowValidators -and (Get-Date) -ge $nextSlow) {
            $validators = @($nodes | Where-Object { $_.Role -eq "validator" })
            if ($validators.Count -gt 0) {
                $target = @($validators | Get-Random -Count 1)[0]
                $seconds = Get-Random -Minimum $SlowMinSeconds -Maximum ($SlowMaxSeconds + 1)
                Start-SlowValidatorFault -Node $target -Seconds $seconds -EventsPath $eventsPath
            }
            $nextSlow = (Get-Date).AddSeconds((Get-Random -Minimum 20 -Maximum ($SlowEverySeconds + 1)))
        }

        if ($pastWarmup -and $script:EnableStaleSnapshots -and (Get-Date) -ge $nextStaleSnapshot) {
            $target = Choose-FaultNode -Nodes $nodes -PreferValidator $false
            if ($target.Local -and -not $SkipStart) {
                Write-ChaosEvent -Path $eventsPath -Type "stale_snapshot_begin" -Fields @{ node = $target.Id }
                Stop-ChaosNode -Node $target
                [void](Hide-LatestSnapshots -Node $target -Count $StaleSnapshotHideNewest -RunId $runId -EventsPath $eventsPath)
                Start-Sleep -Seconds $StaleSnapshotDownSeconds
                Start-ChaosNode -Node $target -RunDir $runDir -EventsPath $eventsPath | Out-Null
                Write-ChaosEvent -Path $eventsPath -Type "stale_snapshot_restart" -Fields @{ node = $target.Id }
            } else {
                Write-ChaosEvent -Path $eventsPath -Type "stale_snapshot_skipped" -Fields @{ node = $target.Id; reason = "local_started_node_required" }
            }
            $nextStaleSnapshot = (Get-Date).AddSeconds((Get-Random -Minimum 30 -Maximum ($StaleSnapshotEverySeconds + 1)))
        }

        Start-Sleep -Seconds $SampleSeconds
    }
} finally {
    Heal-ExpiredFaults -Nodes $nodes -EventsPath $eventsPath
    foreach ($node in $nodes) {
        if ($node.IsolationRules -and @($node.IsolationRules).Count -gt 0) {
            Stop-FirewallIsolation -Node $node -EventsPath $eventsPath -Reason "shutdown"
        }
        if ($node.NetemUntil) {
            [void](Invoke-RemoteNetem -Node $node -Enable $false -EventsPath $eventsPath)
            $node.NetemUntil = $null
        }
        if ($node.SlowUntil) {
            Stop-SlowValidatorFault -Node $node -EventsPath $eventsPath
        }
    }
    if (-not $SkipStart) {
        foreach ($node in $nodes) {
            if ($node.Local) {
                Stop-ChaosNode -Node $node
            }
        }
    }
}

$validatorNodeCount = @($nodes | Where-Object { $_.Role -eq "validator" }).Count
if ($validatorNodeCount -gt 0) {
    if ($lastMaxHeight -le 0) {
        $warningCount++
        Write-ChaosEvent -Path $eventsPath -Type "no_block_progress_failure" -Fields @{
            max_height = $lastMaxHeight
            validator_node_count = $validatorNodeCount
            reason = "validator_run_produced_no_blocks"
        }
    }
    if ($lastMaxFinalized -le 0) {
        $warningCount++
        Write-ChaosEvent -Path $eventsPath -Type "no_finality_progress_failure" -Fields @{
            max_finalized = $lastMaxFinalized
            validator_node_count = $validatorNodeCount
            reason = "validator_run_finalized_no_blocks"
        }
    }
}

$passed = ($warningCount -eq 0)
$summary = [ordered]@{
    run_id = $runId
    passed = $passed
    warning_count = $warningCount
    fork_warning_count = $forkWarningCount
    samples = $samples
    max_height = $lastMaxHeight
    max_finalized = $lastMaxFinalized
    max_no_progress_seconds = $maxObservedGap
    max_finalized_lag_blocks = $maxObservedFinalizedLag
    max_height_lag_blocks = $maxObservedHeightLag
    events_path = $eventsPath
    log_dir = $runDir
    node_count = $nodes.Count
    validator_node_count = $validatorNodeCount
    duration_seconds = $DurationSeconds
    tier1_survival = $Tier1Survival.IsPresent
}
$summary | ConvertTo-Json -Depth 10 | Set-Content -Path $summaryPath
Write-ChaosEvent -Path $eventsPath -Type "chaos_stop" -Fields $summary

Write-Host ""
Write-Host "Chaos logs: $runDir"
Write-Host "Summary: $summaryPath"
Write-Host ("PASS={0} warnings={1} forks={2} max_height={3} max_finalized={4} max_gap_s={5} max_lag_f={6} max_lag_h={7}" -f `
    $passed, $warningCount, $forkWarningCount, $lastMaxHeight, $lastMaxFinalized, $maxObservedGap, $maxObservedFinalizedLag, $maxObservedHeightLag)

if ($FailOnWarning -and -not $passed) {
    exit 1
}
