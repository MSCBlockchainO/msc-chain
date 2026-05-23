param(
  [Parameter(Mandatory = $true)][string]$NodeId,
  [Parameter(Mandatory = $true)][ValidateSet("init", "start", "status", "backup", "add-core-pending", "add-core-active")][string]$Action,
  [int]$Port = 0,
  [string]$Rpc = "",
  [string]$DataDir = "",
  [string]$Config = "",
  [ValidateSet("prompt_only", "file_or_prompt", "env_only")][string]$PasswordMode = "prompt_only",
  [string]$RegistryPath = "core_validators.json",
  [string]$ExternalBackupDir = "",
  [string]$P2PSeed = "",
  [UInt64]$EffectiveHeight = 0
)

$ErrorActionPreference = "Stop"

function Normalize-NodeId {
  param([string]$Id)
  return ($Id.Trim().ToUpperInvariant())
}

function Get-DefaultPort {
  param([string]$Id)
  $map = @{
    "A" = 7001
    "B" = 7002
    "C" = 7003
    "D" = 7004
  }
  if ($map.ContainsKey($Id)) { return [int]$map[$Id] }
  return 7001
}

function Get-DefaultRpc {
  param([string]$Id)
  $map = @{
    "A" = "127.0.0.1:26657"
    "B" = "127.0.0.1:26658"
    "C" = "127.0.0.1:26659"
    "D" = "127.0.0.1:26660"
  }
  if ($map.ContainsKey($Id)) { return [string]$map[$Id] }
  return "127.0.0.1:26657"
}

function Get-DefaultDataDir {
  param([string]$Id)
  return Join-Path "data" $Id
}

function Get-DefaultConfig {
  param([string]$Id)
  return "config.toml"
}

function Get-NodeDir {
  param([string]$Root, [string]$Id)
  return Join-Path $Root ("node_" + $Id)
}

function Assert-GoAvailable {
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "'go' command not found in PATH."
  }
}

function Test-PortListening {
  param([int]$PortNumber)
  if ($PortNumber -le 0) { return $false }
  $conn = Get-NetTCPConnection -LocalPort $PortNumber -State Listen -ErrorAction SilentlyContinue
  return ($null -ne $conn)
}

function Assert-PortsFree {
  param([int[]]$Ports)
  foreach ($p in $Ports) {
    if ($p -le 0) { continue }
    if (Test-PortListening -PortNumber $p) {
      throw "Port $p is already in LISTEN state. Stop conflicting process first."
    }
  }
}

function Assert-NoLegacyEnvSecrets {
  param([bool]$AllowRuntimePasswordEnv = $false)
  $blocked = @(
    "MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE",
    "MSC_ALLOW_VALIDATOR_KEY_CREATE"
  )
  if (-not $AllowRuntimePasswordEnv) {
    $blocked += "MSC_VALIDATOR_PASSWORD"
  }
  $hits = @()
  foreach ($name in $blocked) {
    if (Test-Path "Env:$name") {
      $value = (Get-Item "Env:$name").Value
      if (-not [string]::IsNullOrWhiteSpace($value)) {
        $hits += $name
      }
    }
  }
  if ($hits.Count -gt 0) {
    $clear = @(
      "Remove-Item Env:MSC_VALIDATOR_PASSWORD -ErrorAction SilentlyContinue",
      "Remove-Item Env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue",
      "Remove-Item Env:MSC_ALLOW_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue"
    ) -join "`r`n"
    throw "Blocked legacy env vars detected: $($hits -join ', ').`nClear them first:`n$clear"
  }
}

function Read-SecretString {
  param([string]$Prompt)
  $secure = Read-Host -AsSecureString -Prompt $Prompt
  $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
  try {
    return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
  } finally {
    if ($bstr -ne [IntPtr]::Zero) {
      [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
    }
  }
}

function Protect-PathOwnerOnly {
  param([string]$Path, [bool]$Directory = $false)
  if ($env:OS -notlike "*Windows*") { return }
  $user = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
  if ($Directory) {
    & icacls $Path /inheritance:r /grant:r "$user`:(OI)(CI)F" /remove:g "Users" "Authenticated Users" "Everyone" | Out-Null
  } else {
    & icacls $Path /inheritance:r /grant:r "$user`:F" /remove:g "Users" "Authenticated Users" "Everyone" | Out-Null
  }
  if ($LASTEXITCODE -ne 0) {
    throw "Failed to apply owner-only ACL on $Path"
  }
}

function Write-PasswordFile {
  param([string]$Path, [string]$Password)
  if ([string]::IsNullOrWhiteSpace($Password)) {
    throw "Password is empty."
  }
  $parent = Split-Path -Parent $Path
  New-Item -ItemType Directory -Force -Path $parent | Out-Null
  Protect-PathOwnerOnly -Path $parent -Directory $true
  [System.IO.File]::WriteAllText($Path, $Password, [System.Text.UTF8Encoding]::new($false))
  Protect-PathOwnerOnly -Path $Path -Directory $false
}

function Get-DefaultPasswordFile {
  param([string]$Id)
  return Join-Path $HOME (Join-Path ".msc-secrets" "$Id.pass")
}

function Get-ValidatorFingerprint {
  param([string]$ValidatorSecPath)
  if (-not (Test-Path $ValidatorSecPath)) {
    throw "validator.sec not found: $ValidatorSecPath"
  }
  $json = Get-Content $ValidatorSecPath -Raw | ConvertFrom-Json
  $pubHex = [string]$json.publicKey
  if ([string]::IsNullOrWhiteSpace($pubHex)) {
    throw "publicKey missing in validator.sec"
  }
  $bytes = New-Object byte[] ($pubHex.Length / 2)
  for ($i = 0; $i -lt $bytes.Length; $i++) {
    $bytes[$i] = [Convert]::ToByte($pubHex.Substring($i * 2, 2), 16)
  }
  $sum = [System.Security.Cryptography.SHA256]::Create().ComputeHash($bytes)
  return -join ($sum[0..7] | ForEach-Object { $_.ToString('x2') })
}

function Get-ValidatorPubHex {
  param([string]$ValidatorSecPath)
  $json = Get-Content $ValidatorSecPath -Raw | ConvertFrom-Json
  return [string]$json.publicKey
}

function Set-TomlValidatorString {
  param(
    [string]$ConfigPath,
    [string]$KeyName,
    [string]$Value
  )
  if (-not (Test-Path $ConfigPath)) {
    return
  }
  $content = Get-Content $ConfigPath -Raw
  $linePattern = "(?m)^\s*$([Regex]::Escape($KeyName))\s*=\s*""[^""]*""\s*$"
  $replacement = "$KeyName = ""$Value"""
  if ([Regex]::IsMatch($content, $linePattern)) {
    $updated = [Regex]::Replace($content, $linePattern, $replacement, 1)
  } else {
    $validatorsHeader = "(?m)^\[validators\]\s*$"
    if (-not [Regex]::IsMatch($content, $validatorsHeader)) {
      throw "Cannot set ${KeyName}: [validators] section not found in $ConfigPath"
    }
    $updated = [Regex]::Replace($content, $validatorsHeader, "[validators]`r`n$replacement", 1)
  }
  [System.IO.File]::WriteAllText($ConfigPath, $updated, [System.Text.UTF8Encoding]::new($false))
}

function Set-TomlValidatorBool {
  param(
    [string]$ConfigPath,
    [string]$KeyName,
    [bool]$Value
  )
  if (-not (Test-Path $ConfigPath)) {
    return
  }
  $content = Get-Content $ConfigPath -Raw
  $linePattern = "(?m)^\s*$([Regex]::Escape($KeyName))\s*=\s*(true|false)\s*$"
  $replacement = "$KeyName = " + ($(if ($Value) { "true" } else { "false" }))
  if ([Regex]::IsMatch($content, $linePattern)) {
    $updated = [Regex]::Replace($content, $linePattern, $replacement, 1)
  } else {
    $validatorsHeader = "(?m)^\[validators\]\s*$"
    if (-not [Regex]::IsMatch($content, $validatorsHeader)) {
      throw "Cannot set ${KeyName}: [validators] section not found in $ConfigPath"
    }
    $updated = [Regex]::Replace($content, $validatorsHeader, "[validators]`r`n$replacement", 1)
  }
  [System.IO.File]::WriteAllText($ConfigPath, $updated, [System.Text.UTF8Encoding]::new($false))
}

function Parse-RpcPort {
  param([string]$RpcAddr)
  if ([string]::IsNullOrWhiteSpace($RpcAddr)) { return 0 }
  $m = [Regex]::Match($RpcAddr, ':(\d+)\s*$')
  if (-not $m.Success) { return 0 }
  return [int]$m.Groups[1].Value
}

function Get-CorePasswordFileFromConfig {
  param([string]$ConfigPath)
  if (-not (Test-Path $ConfigPath)) { return "" }
  $content = Get-Content $ConfigPath -Raw
  $m = [Regex]::Match($content, '(?m)^\s*core_password_file\s*=\s*"([^"]*)"\s*$')
  if (-not $m.Success) { return "" }
  return $m.Groups[1].Value.Trim()
}

function Get-ValidatorPasswordModeFromConfig {
  param([string]$ConfigPath)
  if (-not (Test-Path $ConfigPath)) { return "" }
  $content = Get-Content $ConfigPath -Raw
  $m = [Regex]::Match($content, '(?m)^\s*password_mode\s*=\s*"([^"]*)"\s*$')
  if (-not $m.Success) { return "" }
  return $m.Groups[1].Value.Trim().ToLowerInvariant()
}

function Resolve-ConfigPasswordMode {
  param([string]$RequestedMode)
  if ($RequestedMode -eq "file_or_prompt") { return "file_or_prompt" }
  return "prompt_only"
}

function Resolve-ConfigPath {
  param([string]$ExplicitConfig, [string]$Id)
  if (-not [string]::IsNullOrWhiteSpace($ExplicitConfig)) {
    if ($ExplicitConfig.Trim().ToLowerInvariant() -ne "config.toml") {
      throw "only config.toml is allowed in single-config mode"
    }
    return "config.toml"
  }
  return Get-DefaultConfig -Id $Id
}

function Validate-PasswordFile {
  param([string]$Path)
  if ([string]::IsNullOrWhiteSpace($Path)) { throw "Password file path empty." }
  if (-not (Test-Path $Path)) { throw "Password file missing: $Path" }
  $item = Get-Item $Path
  if ($item.Length -le 0) { throw "Password file is empty: $Path" }
}

function Resolve-RpcUrl {
  param([string]$RpcAddr)
  if ($RpcAddr.StartsWith("http://") -or $RpcAddr.StartsWith("https://")) {
    return $RpcAddr
  }
  return "http://$RpcAddr"
}

function Invoke-StatusRequest {
  param([string]$RpcAddr)
  $url = (Resolve-RpcUrl -RpcAddr $RpcAddr).TrimEnd("/") + "/status"
  return Invoke-RestMethod -Uri $url -Method Get -TimeoutSec 10
}

function Print-EnvClearBlock {
  Write-Host ""
  Write-Host "Use this if env-policy errors appear:"
  Write-Host "Remove-Item Env:MSC_VALIDATOR_PASSWORD -ErrorAction SilentlyContinue"
  Write-Host "Remove-Item Env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue"
  Write-Host "Remove-Item Env:MSC_ALLOW_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue"
}

$node = Normalize-NodeId -Id $NodeId
$portValue = if ($Port -gt 0) { $Port } else { Get-DefaultPort -Id $node }
$rpcValue = if (-not [string]::IsNullOrWhiteSpace($Rpc)) { $Rpc } else { Get-DefaultRpc -Id $node }
$dataDirValue = if (-not [string]::IsNullOrWhiteSpace($DataDir)) { $DataDir } else { Get-DefaultDataDir -Id $node }
$configValue = Resolve-ConfigPath -ExplicitConfig $Config -Id $node
$nodeDir = Get-NodeDir -Root $dataDirValue -Id $node
$keyPath = Join-Path $nodeDir "validator.sec"
$pubPath = Join-Path $nodeDir "validator.pub"
$lockPath = Join-Path $nodeDir "fingerprint.lock"
$metaPath = Join-Path $nodeDir "validator.meta.json"
$manifestPath = Join-Path $nodeDir "validator.backup.manifest.json"
$defaultPassFile = Get-DefaultPasswordFile -Id $node
$configPasswordMode = Resolve-ConfigPasswordMode -RequestedMode $PasswordMode

Assert-GoAvailable

switch ($Action) {
  "init" {
    Assert-NoLegacyEnvSecrets
    Assert-PortsFree -Ports @($portValue, (Parse-RpcPort -RpcAddr $rpcValue))
    if (Test-Path $keyPath) {
      throw "validator.sec already exists at $keyPath. Refusing overwrite."
    }

    if (Test-Path $configValue) {
      Set-TomlValidatorString -ConfigPath $configValue -KeyName "password_mode" -Value $configPasswordMode
      Set-TomlValidatorBool -ConfigPath $configValue -KeyName "allow_env_password_in_production" -Value $false
      Set-TomlValidatorBool -ConfigPath $configValue -KeyName "core_env_password_allowed" -Value $false
      if ($configPasswordMode -eq "file_or_prompt") {
        if (-not (Test-Path $defaultPassFile)) {
          $p1 = Read-SecretString -Prompt "Create validator password"
          $p2 = Read-SecretString -Prompt "Confirm validator password"
          if ($p1 -ne $p2) {
            throw "Password confirmation mismatch."
          }
          if ($p1.Trim().Length -lt 8) {
            throw "Password too short. Minimum length is 8."
          }
          Write-PasswordFile -Path $defaultPassFile -Password $p1
          Write-Host "Password file created: $defaultPassFile"
        } else {
          Validate-PasswordFile -Path $defaultPassFile
          Write-Host "Using existing password file: $defaultPassFile"
        }
        Set-TomlValidatorString -ConfigPath $configValue -KeyName "core_password_file" -Value $defaultPassFile
      } else {
        Write-Host "Password mode set to prompt_only. No .pass file required for core startup."
      }
    }

    $keygenArgs = @("run", ".", "--mode=keygen", "--id=$node", "--datadir=$dataDirValue")
    if (-not [string]::IsNullOrWhiteSpace($Config)) {
      $keygenArgs += @("--config", $Config)
    }
    & go @keygenArgs
    if ($LASTEXITCODE -ne 0) {
      throw "keygen failed."
    }

    foreach ($path in @($keyPath, $pubPath, $lockPath, $metaPath, $manifestPath)) {
      if (-not (Test-Path $path)) {
        throw "Expected artifact missing after init: $path"
      }
    }

    $fp = Get-ValidatorFingerprint -ValidatorSecPath $keyPath
    if (Test-Path $configValue) {
      Set-TomlValidatorString -ConfigPath $configValue -KeyName "required_key_fingerprint" -Value $fp
      Write-Host "Fingerprint pinned in $configValue => $fp"
    } else {
      Write-Warning "Config file not found ($configValue). Manually pin required_key_fingerprint=$fp"
    }

    Write-Host "Init complete for $node"
    Write-Host "Next command:"
    Write-Host ".\scripts\core_beginner.ps1 -NodeId $node -Action start -Port $portValue -Rpc $rpcValue -DataDir $dataDirValue"
    break
  }
  "start" {
    Assert-NoLegacyEnvSecrets -AllowRuntimePasswordEnv ($PasswordMode -eq "env_only")
    Assert-PortsFree -Ports @($portValue, (Parse-RpcPort -RpcAddr $rpcValue))

    if (-not (Test-Path $keyPath)) {
      throw "Missing validator key: $keyPath. Run init action first."
    }
    if (-not (Test-Path $lockPath)) {
      throw "Missing fingerprint lock: $lockPath"
    }
    if (-not (Test-Path $manifestPath)) {
      throw "Missing backup manifest: $manifestPath"
    }

    $effectiveStartMode = $PasswordMode
    if ($effectiveStartMode -eq "prompt_only" -or [string]::IsNullOrWhiteSpace($effectiveStartMode)) {
      $cfgMode = Get-ValidatorPasswordModeFromConfig -ConfigPath $configValue
      if (-not [string]::IsNullOrWhiteSpace($cfgMode)) {
        $effectiveStartMode = $cfgMode
      }
    }
    if ([string]::IsNullOrWhiteSpace($effectiveStartMode)) {
      $effectiveStartMode = "prompt_only"
    }

    if ($effectiveStartMode -eq "env_only") {
      if (-not (Test-Path Env:MSC_VALIDATOR_PASSWORD) -or [string]::IsNullOrWhiteSpace((Get-Item Env:MSC_VALIDATOR_PASSWORD).Value)) {
        $ep = Read-SecretString -Prompt "Runtime validator password (env_only)"
        if ([string]::IsNullOrWhiteSpace($ep)) {
          throw "MSC_VALIDATOR_PASSWORD is required for env_only mode."
        }
        $env:MSC_VALIDATOR_PASSWORD = $ep
      }
      $env:MSC_VALIDATOR_PASSWORD_MODE = "env_only"
      Write-Host "Start mode: env_only override enabled for this terminal session."
    } else {
      Remove-Item Env:MSC_VALIDATOR_PASSWORD_MODE -ErrorAction SilentlyContinue
    }

    if ($effectiveStartMode -eq "file_or_prompt" -and (Test-Path $configValue)) {
      $cfgPassPath = Get-CorePasswordFileFromConfig -ConfigPath $configValue
      if (-not [string]::IsNullOrWhiteSpace($cfgPassPath)) {
        Validate-PasswordFile -Path $cfgPassPath
      } else {
        if (-not (Test-Path $defaultPassFile)) {
          Write-Warning "No core_password_file in config and no default secret at $defaultPassFile. Prompt fallback may be required."
        }
      }
    }
    if ($effectiveStartMode -eq "prompt_only") {
      Write-Host "Start mode: prompt_only (runtime hidden password prompt expected)."
    }

    $args = @("run", ".", "--mode=full", "--id=$node", "--port=$portValue", "--datadir=$dataDirValue", "--rpcaddr", $rpcValue)
    if (-not [string]::IsNullOrWhiteSpace($Config)) {
      $args += @("--config", $Config)
    }
    & go @args
    exit $LASTEXITCODE
  }
  "status" {
    try {
      $s = Invoke-StatusRequest -RpcAddr $rpcValue
    } catch {
      throw "Failed to query /status on $rpcValue. Ensure node is running. Error: $($_.Exception.Message)"
    }
    $checks = @(
      @{ Key = "validator_key_loaded"; Expect = $true },
      @{ Key = "validator_key_fingerprint_match"; Expect = $true },
      @{ Key = "validator_key_integrity_ok"; Expect = $true },
      @{ Key = "validator_key_backup_present"; Expect = $true },
      @{ Key = "core_required_fingerprint_match"; Expect = $true },
      @{ Key = "ready"; Expect = $true }
    )
    $allOk = $true
    foreach ($c in $checks) {
      $k = $c.Key
      $expect = $c.Expect
      $actual = $s.$k
      $ok = ($null -ne $actual -and [bool]$actual -eq $expect)
      if (-not $ok) { $allOk = $false }
      Write-Host ("{0} => actual={1} expected={2} result={3}" -f $k, $actual, $expect, $(if ($ok) { "OK" } else { "FAIL" }))
    }
    Write-Host ("validator_password_mode={0} validator_secret_source={1}" -f $s.validator_password_mode, $s.validator_secret_source)
    Write-Host ("height={0} peers={1} role={2} reason={3}" -f $s.height, $s.peers, $s.role, $s.reason)
    if (-not $allOk) {
      Write-Host ""
      Write-Host "Status check failed. Next steps:"
      Print-EnvClearBlock
      Write-Host "Check key artifacts under $nodeDir and retry start."
      exit 1
    }
    Write-Host "Status check passed."
    break
  }
  "backup" {
    if (-not (Test-Path $keyPath)) {
      throw "validator.sec not found: $keyPath"
    }
    $inNodeBackupDir = Join-Path $nodeDir "secure-backups"
    & powershell -NoProfile -ExecutionPolicy Bypass -File ".\scripts\validator_key_backup.ps1" -NodeDir $nodeDir -BackupDir $inNodeBackupDir
    if ($LASTEXITCODE -ne 0) {
      throw "Backup script failed."
    }
    if (-not (Test-Path $manifestPath)) {
      Write-Warning "Runtime manifest missing: $manifestPath"
    } else {
      $manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
      $backupPath = [string]$manifest.backup_path
      if (-not [System.IO.Path]::IsPathRooted($backupPath)) {
        $backupPath = Join-Path $nodeDir $backupPath
      }
      if (Test-Path $backupPath) {
        $sha = (Get-FileHash -Algorithm SHA256 -Path $backupPath).Hash.ToLowerInvariant()
        if ($sha -ne ([string]$manifest.backup_sha256).ToLowerInvariant()) {
          throw "Runtime backup manifest checksum mismatch: $backupPath"
        }
        Write-Host "Runtime backup manifest verified: $backupPath"
      } else {
        throw "Runtime backup file missing from manifest path: $backupPath"
      }
    }

    if (-not [string]::IsNullOrWhiteSpace($ExternalBackupDir)) {
      New-Item -ItemType Directory -Force -Path $ExternalBackupDir | Out-Null
      $ts = Get-Date -Format "yyyyMMdd_HHmmss"
      $externalPath = Join-Path $ExternalBackupDir ("validator.sec.$node.$ts.bak")
      Copy-Item -Path (Join-Path $nodeDir "validator.sec") -Destination $externalPath -Force
      $extHash = (Get-FileHash -Algorithm SHA256 -Path $externalPath).Hash.ToLowerInvariant()
      $hashFile = "$externalPath.sha256.txt"
      [System.IO.File]::WriteAllText($hashFile, $extHash, [System.Text.UTF8Encoding]::new($false))
      Write-Host "External backup copied: $externalPath"
      Write-Host "External checksum file: $hashFile"
    } else {
      Write-Host "Optional external copy command:"
      Write-Host ".\scripts\core_beginner.ps1 -NodeId $node -Action backup -DataDir $dataDirValue -ExternalBackupDir C:\offline-backups\$node"
    }
    break
  }
  "add-core-pending" {
    if (-not (Test-Path $keyPath)) {
      throw "Missing validator key: $keyPath"
    }
    $fp = Get-ValidatorFingerprint -ValidatorSecPath $keyPath
    $pubHex = Get-ValidatorPubHex -ValidatorSecPath $keyPath
    $targetHeight = $EffectiveHeight
    if ($targetHeight -eq 0) {
      try {
        $s = Invoke-StatusRequest -RpcAddr $rpcValue
        $h = [UInt64]$s.finalized_height
        if ($h -gt 0) {
          $targetHeight = $h + 128
        }
      } catch {
      }
    }
    if ($targetHeight -eq 0) { $targetHeight = 128 }
    $entry = [ordered]@{
      id                       = $node
      required_key_fingerprint = $fp
      consensus_pubkey         = $pubHex
      p2p_seed                 = $(if ([string]::IsNullOrWhiteSpace($P2PSeed)) { "<SET_NODE_MULTIADDR_WITH_PEER_ID>" } else { $P2PSeed })
      status                   = "pending"
      effective_height         = $targetHeight
    }
    $outPath = Join-Path $dataDirValue ("core_registry_pending_" + $node + ".snippet.json")
    $entry | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 $outPath
    Write-Host "Pending entry snippet written: $outPath"
    Write-Host "Next: merge into $RegistryPath, bump epoch/effective_height, collect quorum signatures, distribute to all core nodes."
    break
  }
  "add-core-active" {
    $targetHeight = $EffectiveHeight
    if ($targetHeight -eq 0) {
      try {
        $s = Invoke-StatusRequest -RpcAddr $rpcValue
        $h = [UInt64]$s.finalized_height
        if ($h -gt 0) {
          $targetHeight = $h + 128
        }
      } catch {
      }
    }
    if ($targetHeight -eq 0) { $targetHeight = 128 }
    $entry = [ordered]@{
      id               = $node
      status           = "active"
      effective_height = $targetHeight
      note             = "Promote pending core validator to active after quorum signatures."
    }
    $outPath = Join-Path $dataDirValue ("core_registry_active_" + $node + ".snippet.json")
    $entry | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 $outPath
    Write-Host "Active transition snippet written: $outPath"
    Write-Host "Next: apply signed registry update and verify /status => core_activation_status.status=active."
    break
  }
  default {
    throw "Unknown action: $Action"
  }
}
