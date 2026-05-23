param(
  [Parameter(Mandatory = $true)][string]$NodeId,
  [Parameter(Mandatory = $true)][string]$Password,
  [string]$Config = "config.toml",
  [Parameter(Mandatory = $true)][int]$Port,
  [Parameter(Mandatory = $true)][string]$Rpc,
  [Parameter(Mandatory = $true)][string]$DataDir,
  [string]$Mode = "full",
  [switch]$AllowCreateOverride
)

$ErrorActionPreference = "Stop"

Write-Warning "[DEPRECATED] scripts/start_core_validator.ps1 is legacy ENV-based flow."
Write-Warning "Use scripts/core_beginner.ps1 for beginner-safe operations (init/start/status/backup)."

if ($Config.Trim().ToLowerInvariant() -ne "config.toml") {
  throw "only config.toml is allowed in single-config mode"
}

if (!(Test-Path "config.toml")) {
  throw "Missing config file: config.toml"
}

$nodeDir = Join-Path $DataDir ("node_" + $NodeId)
$keyPath = Join-Path $nodeDir "validator.sec"
if ($AllowCreateOverride -and (Test-Path $keyPath)) {
  throw "validator.sec already exists at $keyPath. Refusing create override. Run without -AllowCreateOverride."
}

${oldPassword} = $env:MSC_VALIDATOR_PASSWORD
${oldCreateOverride} = $env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE
try {
  $env:MSC_VALIDATOR_PASSWORD = $Password
  if ($AllowCreateOverride) {
    $env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE = "1"
  }

  Write-Host "Starting core validator $NodeId with config.toml"
  if ($AllowCreateOverride) {
    Write-Host "Create override enabled for this run only."
  }
  go run . --config config.toml --mode=$Mode --id=$NodeId --port=$Port --datadir=$DataDir --rpcaddr $Rpc
}
finally {
  if ($null -ne $oldPassword) {
    $env:MSC_VALIDATOR_PASSWORD = $oldPassword
  } else {
    Remove-Item Env:MSC_VALIDATOR_PASSWORD -ErrorAction SilentlyContinue
  }
  if ($null -ne $oldCreateOverride) {
    $env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE = $oldCreateOverride
  } else {
    Remove-Item Env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue
  }
}
