param(
  [Parameter(Mandatory = $true)][string]$NodeDir,
  [Parameter(Mandatory = $true)][string]$BackupDir
)

$ErrorActionPreference = "Stop"

$keyPath = Join-Path $NodeDir "validator.sec"
if (!(Test-Path $keyPath)) {
  throw "validator.sec not found at $keyPath"
}

New-Item -ItemType Directory -Force -Path $BackupDir | Out-Null

$ts = Get-Date -Format "yyyyMMdd_HHmmss"
$outPath = Join-Path $BackupDir ("validator.sec." + $ts + ".bak")
Copy-Item -Path $keyPath -Destination $outPath -Force

$hash = (Get-FileHash -Algorithm SHA256 -Path $outPath).Hash.ToLowerInvariant()

$manifest = [ordered]@{
  backup_path = $outPath
  backup_sha256 = $hash
  generated_at = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
}
$manifestPath = Join-Path $BackupDir ("validator.backup." + $ts + ".json")
$manifest | ConvertTo-Json -Depth 4 | Set-Content -Encoding UTF8 $manifestPath

Write-Host "Backup created: $outPath"
Write-Host "Manifest: $manifestPath"
