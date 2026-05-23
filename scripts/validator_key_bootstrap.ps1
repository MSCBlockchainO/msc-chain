param(
  [Parameter(Mandatory = $true)][string]$NodeId,
  [Parameter(Mandatory = $true)][string]$DataDir,
  [Parameter(Mandatory = $true)][string]$Password,
  [switch]$CoreValidator
)

$ErrorActionPreference = "Stop"

Write-Warning "[DEPRECATED] scripts/validator_key_bootstrap.ps1 is legacy ENV bootstrap helper."
Write-Warning "Use scripts/core_beginner.ps1 -Action init for beginner-safe key bootstrap."

if ([string]::IsNullOrWhiteSpace($Password) -or $Password.Length -lt 8 -or $Password -eq "m") {
  throw "Weak password blocked. Use a strong password (min length 8, not 'm')."
}

$env:MSC_VALIDATOR_PASSWORD = $Password
if ($CoreValidator) {
  $env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE = "1"
} else {
  $env:MSC_ALLOW_VALIDATOR_KEY_CREATE = "1"
}

Write-Host "Bootstrap env prepared for node $NodeId"
Write-Host "Start node once to create validator.sec if missing."
Write-Host "After successful startup, clear create override env vars."
