param(
    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
    [string]$DataRoot = "data",
    [string]$LogRoot = "runtime-logs",
    [string]$PasswordFile = (Join-Path $PSScriptRoot "mainnet_node_passwords.local.ps1"),
    [int]$StartupWaitSeconds = 90,
    [switch]$SkipBackup,
    [switch]$NoStart
)

$ErrorActionPreference = "Stop"

. $PasswordFile

$nodes = @(
    @{ Id = "A";     Port = 7001; Rpc = "127.0.0.1:26657"; PasswordEnv = "MSC_NODE_PASSWORD_A" },
    @{ Id = "B";     Port = 7002; Rpc = "127.0.0.1:26658"; PasswordEnv = "MSC_NODE_PASSWORD_B" },
    @{ Id = "C";     Port = 7003; Rpc = "127.0.0.1:26659"; PasswordEnv = "MSC_NODE_PASSWORD_C" },
    @{ Id = "D";     Port = 7004; Rpc = "127.0.0.1:26660"; PasswordEnv = "MSC_NODE_PASSWORD_D" },
    @{ Id = "F";     Port = 7005; Rpc = "127.0.0.1:26661"; PasswordEnv = "MSC_NODE_PASSWORD_F" },
    @{ Id = "G";     Port = 7006; Rpc = "127.0.0.1:26662"; PasswordEnv = "MSC_NODE_PASSWORD_G" },
    @{ Id = "Talha"; Port = 7007; Rpc = "127.0.0.1:26663"; PasswordEnv = "MSC_NODE_PASSWORD_TALHA" },
    @{ Id = "T";     Port = 7008; Rpc = "127.0.0.1:26664"; PasswordEnv = "MSC_NODE_PASSWORD_T" },
    @{ Id = "W";     Port = 7010; Rpc = "127.0.0.1:26665"; PasswordEnv = "MSC_NODE_PASSWORD_W" }
)

function Stop-ExistingChainProcesses {
    for ($pass = 0; $pass -lt 5; $pass++) {
        $targets = @(Get-CimInstance Win32_Process |
            Where-Object {
                $cmd = "$($_.CommandLine)"
                $_.ProcessId -ne $PID -and (
                    $_.Name -eq "msc-chain.exe" -or
                    ($_.Name -eq "go.exe" -and "$($_.CommandLine)" -match "go run \.") -or
                    ($_.Name -eq "powershell.exe" -and $cmd -match "msc-chain|mainnet-chaos|mainnet_chaos_network|Tier1Survival|TxFloodBatch|MSC_CHAO|genesis-restart|your-validator-password|--id=H|--id=I|--id=J|--port=7009|data[/\\]H|data[/\\]I|data[/\\]J")
                )
            } |
            Sort-Object ParentProcessId -Descending)
        if ($targets.Count -eq 0) {
            return
        }
        foreach ($proc in $targets) {
            Stop-Process -Id ([int]$proc.ProcessId) -Force -ErrorAction SilentlyContinue
        }
        Start-Sleep -Seconds 1
    }
}

function Move-IfExists {
    param([string]$Path, [string]$BackupRoot)
    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }
    $item = Get-Item -LiteralPath $Path -Force
    $dest = Join-Path $BackupRoot $item.Name
    Move-Item -LiteralPath $item.FullName -Destination $dest -Force
}

function Reset-NodeToGenesis {
    param([hashtable]$Node, [string]$BackupRoot)

    $id = $Node.Id
    $dataDir = Join-Path $RepoRoot (Join-Path $DataRoot $id)
    $nodeDir = Join-Path $dataDir ("node_" + $id)
    New-Item -ItemType Directory -Force -Path $nodeDir | Out-Null

    if ($SkipBackup) {
        return
    }

    $nodeBackup = Join-Path $BackupRoot $id
    $nodeDirBackup = Join-Path $nodeBackup ("node_" + $id)
    New-Item -ItemType Directory -Force -Path $nodeBackup | Out-Null
    New-Item -ItemType Directory -Force -Path $nodeDirBackup | Out-Null

    $coreValidatorIds = @("A", "B", "C", "D")
    $preservedCoreKeyFiles = @(
        "fingerprint.lock",
        "p2p_identity.key",
        "secure-backups",
        "validator.backup.manifest.json",
        "validator.meta.json",
        "validator.pub",
        "validator.sec"
    )

    foreach ($item in @(Get-ChildItem -LiteralPath $dataDir -Force -ErrorAction SilentlyContinue)) {
        if ($item.FullName -eq $nodeDir) {
            continue
        }
        Move-IfExists -Path $item.FullName -BackupRoot $nodeBackup
    }

    foreach ($item in @(Get-ChildItem -LiteralPath $nodeDir -Force -ErrorAction SilentlyContinue)) {
        if ($coreValidatorIds -contains $id -and $preservedCoreKeyFiles -contains $item.Name) {
            continue
        }
        Move-IfExists -Path $item.FullName -BackupRoot $nodeDirBackup
    }
}

function Start-Node {
    param([hashtable]$Node, [string]$RunDir)

    $id = $Node.Id
    $password = [Environment]::GetEnvironmentVariable($Node.PasswordEnv, "Process")
    if ([string]::IsNullOrWhiteSpace($password)) {
        throw "missing password env $($Node.PasswordEnv)"
    }
    $log = Join-Path $RunDir "$id.log"
    $dataDir = (Join-Path $DataRoot $id).Replace("\", "/")
    $command = @(
        "`$env:MSC_ALLOW_VALIDATOR_KEY_CREATE='1'",
        "`$env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE='1'",
        "`$env:MSC_AUTH_OPEN_BROWSER='0'",
        "`$env:MSC_VALIDATOR_PASSWORD='$($password.Replace("'","''"))'",
        "Set-Location '$($RepoRoot.Replace("'","''"))'",
        "go run . --mode=full --id=$id --port=$($Node.Port) --datadir=$dataDir --rpcaddr $($Node.Rpc) *> '$($log.Replace("'","''"))'"
    ) -join "; "
    $proc = Start-Process -FilePath "powershell" -WindowStyle Hidden -PassThru -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $command)
    $Node.Process = $proc
}

function Wait-Rpc {
    param([hashtable]$Node, [int]$TimeoutSeconds)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $status = Invoke-RestMethod -Uri "http://$($Node.Rpc)/status" -TimeoutSec 3
            return $status
        } catch {
            Start-Sleep -Seconds 2
        }
    }
    return $null
}

$runId = "genesis-restart-" + (Get-Date -Format "yyyyMMdd-HHmmss")
$runDir = Join-Path (Join-Path $RepoRoot $LogRoot) $runId
$backupRoot = Join-Path (Join-Path $RepoRoot "data") ("_genesis_reset_backup_" + (Get-Date -Format "yyyyMMdd-HHmmss"))
New-Item -ItemType Directory -Force -Path $runDir | Out-Null

Stop-ExistingChainProcesses
Start-Sleep -Seconds 2

foreach ($node in $nodes) {
    Reset-NodeToGenesis -Node $node -BackupRoot $backupRoot
}

if ($NoStart) {
    Write-Host "Genesis state reset done. Backup: $backupRoot"
    Write-Host "NoStart set; nodes not launched."
    exit 0
}

foreach ($node in $nodes) {
    Start-Node -Node $node -RunDir $runDir
    Start-Sleep -Seconds 3
}

$rows = @()
foreach ($node in $nodes) {
    $status = Wait-Rpc -Node $node -TimeoutSeconds $StartupWaitSeconds
    if ($null -eq $status) {
        $rows += [pscustomobject]@{ Node = $node.Id; Rpc = $node.Rpc; Started = $false; Height = "-"; Finalized = "-"; Ready = $false; Peers = "-"; Wait = "rpc_timeout" }
        continue
    }
    $rows += [pscustomobject]@{ Node = $node.Id; Rpc = $node.Rpc; Started = $true; Height = $status.height; Finalized = $status.finalized_height; Ready = $status.ready; Peers = $status.peers; Wait = $status.wait_reason }
}

Write-Host "Genesis restart logs: $runDir"
Write-Host "Previous chain data backup: $backupRoot"
$rows | Format-Table -AutoSize
