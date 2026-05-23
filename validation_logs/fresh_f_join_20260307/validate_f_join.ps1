$ErrorActionPreference = 'Stop'
$root = 'c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain'
$logDir = Join-Path $root 'validation_logs\fresh_f_join_20260307'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

function Start-NodeJob {
  param(
    [string]$Root,
    [string]$Id,
    [string]$Password,
    [int]$Port,
    [string]$DataDir,
    [string]$Rpc,
    [string]$Stdout,
    [string]$Stderr,
    [switch]$AllowCreate
  )
  Start-Job -ArgumentList $Root,$Id,$Password,$Port,$DataDir,$Rpc,$Stdout,$Stderr,$AllowCreate.IsPresent -ScriptBlock {
    param($Root,$Id,$Password,$Port,$DataDir,$Rpc,$Stdout,$Stderr,$AllowCreate)
    Set-Location $Root
    $env:MSC_VALIDATOR_PASSWORD_MODE = 'env_only'
    $env:MSC_VALIDATOR_PASSWORD = $Password
    if ($AllowCreate) {
      $env:MSC_ALLOW_VALIDATOR_KEY_CREATE = '1'
    } else {
      Remove-Item Env:MSC_ALLOW_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue
    }
    & go run . --config config.toml --mode=full --id=$Id --port=$Port --datadir=$DataDir --rpcaddr $Rpc 1>> $Stdout 2>> $Stderr
  }
}

$fSource = Join-Path $root 'data\F\node_F'
$fData = Join-Path $root 'data\F_validation'
$fNode = Join-Path $fData 'node_F'
if (Test-Path $fData) { Remove-Item $fData -Recurse -Force }
New-Item -ItemType Directory -Force -Path $fNode | Out-Null
$copyFiles = @('validator.sec','validator.pub','validator.meta.json','validator.backup.manifest.json','fingerprint.lock')
foreach ($name in $copyFiles) {
  $src = Join-Path $fSource $name
  if (Test-Path $src) { Copy-Item $src -Destination (Join-Path $fNode $name) -Force }
}
$srcBackup = Join-Path $fSource 'secure-backups'
if (Test-Path $srcBackup) { Copy-Item $srcBackup -Destination (Join-Path $fNode 'secure-backups') -Recurse -Force }

Get-ChildItem $logDir -Filter '*.log' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

$jobs = @()
$bootstrap = @(
  @{Id='A'; Pass='mfd@12g1'; Port=7001; Data='data/A'; Rpc='127.0.0.1:26657'},
  @{Id='B'; Pass='mfd@12g2'; Port=7002; Data='data/B'; Rpc='127.0.0.1:26658'},
  @{Id='C'; Pass='mfd@12g3'; Port=7003; Data='data/C'; Rpc='127.0.0.1:26659'},
  @{Id='D'; Pass='mfd@12g3'; Port=7004; Data='data/D'; Rpc='127.0.0.1:26660'}
)
foreach ($n in $bootstrap) {
  $jobs += Start-NodeJob -Root $root -Id $n.Id -Password $n.Pass -Port $n.Port -DataDir $n.Data -Rpc $n.Rpc -Stdout (Join-Path $logDir ($n.Id + '.out.log')) -Stderr (Join-Path $logDir ($n.Id + '.err.log'))
}

$deadline = (Get-Date).AddSeconds(45)
$portsReady = @{}
while ((Get-Date) -lt $deadline) {
  $listening = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -in 7001,7002,7003,7004 } | Select-Object -ExpandProperty LocalPort
  foreach ($p in $listening) { $portsReady[$p] = $true }
  if ($portsReady.Count -eq 4) { break }
  Start-Sleep -Seconds 2
}

$bootSummary = foreach ($n in $bootstrap) {
  $out = Join-Path $logDir ($n.Id + '.out.log')
  $err = Join-Path $logDir ($n.Id + '.err.log')
  [pscustomobject]@{
    Id = $n.Id
    Port = $n.Port
    Listening = [bool]$portsReady[$n.Port]
    Unlocked = [bool]((Get-Content $out -Raw -ErrorAction SilentlyContinue) -match 'validator key unlocked')
    Fatal = [bool]((Get-Content $out -Raw -ErrorAction SilentlyContinue) -match '\[FATAL\]')
    ErrTail = ((Get-Content $err -Tail 3 -ErrorAction SilentlyContinue) -join ' | ')
  }
}

$fOut = Join-Path $logDir 'F_validation.out.log'
$fErr = Join-Path $logDir 'F_validation.err.log'
$jobs += Start-NodeJob -Root $root -Id 'F' -Password 'mfd@12g3' -Port 7005 -DataDir 'data/F_validation' -Rpc '127.0.0.1:26661' -Stdout $fOut -Stderr $fErr

$fDeadline = (Get-Date).AddSeconds(90)
$obs = [ordered]@{
  chain_sample_pending_snapshot = $false
  snapshot_anchor = $false
  status_expected_genesis_bootstrap = $false
  weak_source_genesis_bootstrap = $false
  snapshot_reject_count = 0
  latest_status = ''
}
while ((Get-Date) -lt $fDeadline) {
  $content = Get-Content $fOut -Raw -ErrorAction SilentlyContinue
  if ($null -ne $content -and $content.Length -gt 0) {
    if ($content -match 'chain_sample_pending_snapshot') { $obs.chain_sample_pending_snapshot = $true }
    if ($content -match 'snapshot_anchor') { $obs.snapshot_anchor = $true }
    if ($content -match 'expected_source=genesis_bootstrap') { $obs.status_expected_genesis_bootstrap = $true }
    if ($content -match '\[SET-COMMITMENT\].*weak-source peer mismatch deferred .*source=genesis_bootstrap') { $obs.weak_source_genesis_bootstrap = $true }
    $obs.snapshot_reject_count = ([regex]::Matches($content,'\[SNAPSHOT-VERIFY\] reject')).Count
    $statusMatches = [regex]::Matches($content,'\[STATUS\].*')
    if ($statusMatches.Count -gt 0) { $obs.latest_status = $statusMatches[$statusMatches.Count-1].Value }
  }
  if ($obs.chain_sample_pending_snapshot -or $obs.snapshot_anchor -or $obs.weak_source_genesis_bootstrap) { break }
  Start-Sleep -Seconds 3
}

$fTail = (Get-Content $fOut -Tail 60 -ErrorAction SilentlyContinue) -join "`n"
$eTail = (Get-Content $fErr -Tail 20 -ErrorAction SilentlyContinue) -join "`n"

$jobs | Stop-Job -ErrorAction SilentlyContinue | Out-Null
$jobs | Remove-Job -Force -ErrorAction SilentlyContinue | Out-Null

'BOOTSTRAP SUMMARY'
$bootSummary | Format-Table -AutoSize | Out-String
'F OBSERVATIONS'
($obs.GetEnumerator() | ForEach-Object { "{0}={1}" -f $_.Key,$_.Value }) -join "`n"
'F OUT TAIL'
$fTail
'F ERR TAIL'
$eTail
