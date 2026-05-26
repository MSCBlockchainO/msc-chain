param(
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$SourceUser = "ubuntu",
    [string]$SourceHost = "54.80.4.133",
    [string]$SourceRPC = "http://127.0.0.1:26657",
    [string]$SourceRPCToken = $env:MSC_RPC_TOKEN,
    [string]$TargetUser = "ubuntu",
    [string]$TargetHost = "50.19.167.221",
    [string]$TargetNodeId = "RESTORE",
    [string]$RepoPath = "~/msc-chain",
    [int]$RecoveryTargetSeconds = 300,
    [switch]$KeepRemoteArtifacts
)

$ErrorActionPreference = "Stop"

function Quote-BashArg {
    param([string]$Value)
    return "'" + ($Value -replace "'", "'`"`"'`"'") + "'"
}

function Invoke-SSH {
    param(
        [string]$User,
        [string]$HostName,
        [string]$Command
    )
    & ssh -o StrictHostKeyChecking=no -i $KeyPath "$User@$HostName" $Command
    if ($LASTEXITCODE -ne 0) {
        throw "ssh command failed on $User@$HostName"
    }
}

function Copy-FromRemote {
    param(
        [string]$User,
        [string]$HostName,
        [string]$RemotePath,
        [string]$LocalPath
    )
    & scp -o StrictHostKeyChecking=no -i $KeyPath "$User@$HostName`:$RemotePath" $LocalPath
    if ($LASTEXITCODE -ne 0) {
        throw "scp download failed from $User@$HostName"
    }
}

function Copy-ToRemote {
    param(
        [string]$User,
        [string]$HostName,
        [string]$LocalPath,
        [string]$RemotePath
    )
    & scp -o StrictHostKeyChecking=no -i $KeyPath $LocalPath "$User@$HostName`:$RemotePath"
    if ($LASTEXITCODE -ne 0) {
        throw "scp upload failed to $User@$HostName"
    }
}

$stamp = Get-Date -Format "yyyyMMddHHmmss"
$localRoot = Join-Path $env:TEMP "msc-restore-test-$stamp"
New-Item -ItemType Directory -Force -Path $localRoot | Out-Null

Write-Host "== MSC backup/restore EC2 test =="
Write-Host "source: $SourceUser@$SourceHost rpc=$SourceRPC"
Write-Host "target: $TargetUser@$TargetHost temp node=$TargetNodeId"

$sourceAuthHeader = ""
if (-not [string]::IsNullOrWhiteSpace($SourceRPCToken)) {
    $sourceAuthHeader = "-H " + (Quote-BashArg ("Authorization: Bearer " + $SourceRPCToken))
}

$sourceScript = @"
set -euo pipefail
repo="$RepoPath"
stamp="$stamp"
cd "`$repo"
curl -fsS -X POST $sourceAuthHeader "$SourceRPC/backup/export" > "/tmp/msc_backup_export_`$stamp.json"
backup_dir=`$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["backup_dir"])' "/tmp/msc_backup_export_`$stamp.json")
backup_base=`$(basename "`$backup_dir")
archive="/tmp/msc_backup_`$stamp.tgz"
tar -C "`$(dirname "`$backup_dir")" -czf "`$archive" "`$backup_base"
printf '%s\n%s\n' "`$archive" "`$backup_base" > "/tmp/msc_backup_archive_`$stamp.txt"
cat "/tmp/msc_backup_archive_`$stamp.txt"
"@

$sourceInfoRaw = Invoke-SSH -User $SourceUser -HostName $SourceHost -Command ("bash -lc " + (Quote-BashArg $sourceScript))
$sourceInfo = @($sourceInfoRaw | Where-Object { $_ -ne "" })
if ($sourceInfo.Count -lt 2) {
    throw "source did not return archive info"
}
$remoteArchive = $sourceInfo[$sourceInfo.Count - 2]
$backupBase = $sourceInfo[$sourceInfo.Count - 1]
$archiveName = Split-Path -Leaf $remoteArchive
$localArchive = Join-Path $localRoot $archiveName

Write-Host "backup archive: $remoteArchive"
Copy-FromRemote -User $SourceUser -HostName $SourceHost -RemotePath $remoteArchive -LocalPath $localArchive

$targetArchive = "/tmp/$archiveName"
Copy-ToRemote -User $TargetUser -HostName $TargetHost -LocalPath $localArchive -RemotePath $targetArchive

$targetScript = @"
set -euo pipefail
repo="$RepoPath"
stamp="$stamp"
root="/tmp/msc-restore-test-`$stamp"
mkdir -p "`$root/input"
tar -C "`$root/input" -xzf "$targetArchive"
backup="`$root/input/$backupBase"
cd "`$repo"
start=`$(date +%s)
./msc-node backup verify --path "`$backup" > "`$root/verify.json"
./msc-node backup import --id "$TargetNodeId" --datadir "`$root/data" --path "`$backup" --apply > "`$root/import.json"
end=`$(date +%s)
duration=`$((end-start))
cp -a "`$backup" "`$root/corrupt"
printf X >> "`$root/corrupt/snapshot.json"
if ./msc-node backup verify --path "`$root/corrupt" > "`$root/corrupt.out" 2>&1; then
  echo "CORRUPT_ACCEPTED"
  exit 44
fi
echo "CORRUPT_REJECTED"
ROOT="`$root" DURATION="`$duration" TARGET_SECONDS="$RecoveryTargetSeconds" python3 - <<'PY'
import json
import os
root=os.environ["ROOT"]
duration=int(os.environ["DURATION"])
target=int(os.environ["TARGET_SECONDS"])
verify=json.load(open(root + "/verify.json"))
imp=json.load(open(root + "/import.json"))
print(json.dumps({
  "restore_seconds": duration,
  "target_seconds": target,
  "height": imp.get("height"),
  "snapshot_hash": imp.get("snapshot_hash"),
  "stored": imp.get("stored"),
  "applied": imp.get("applied"),
  "node_root": imp.get("node_root"),
  "verify_ok": verify.get("ok"),
  "corrupt_snapshot_rejected": True
}, indent=2))
PY
if [ "`$duration" -gt "$RecoveryTargetSeconds" ]; then
  exit 45
fi
"@

Invoke-SSH -User $TargetUser -HostName $TargetHost -Command ("bash -lc " + (Quote-BashArg $targetScript))

if (-not $KeepRemoteArtifacts.IsPresent) {
    Invoke-SSH -User $SourceUser -HostName $SourceHost -Command ("rm -f " + (Quote-BashArg $remoteArchive))
    Invoke-SSH -User $TargetUser -HostName $TargetHost -Command ("rm -rf " + (Quote-BashArg "/tmp/msc-restore-test-$stamp") + " " + (Quote-BashArg $targetArchive))
}

Remove-Item -Recurse -Force -LiteralPath $localRoot
Write-Host "EC2 backup/restore test passed within target."
