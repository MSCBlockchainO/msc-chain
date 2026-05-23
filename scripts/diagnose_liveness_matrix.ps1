param(
  [string]$Targets = "A=http://127.0.0.1:26657,B=http://127.0.0.1:26658,C=http://127.0.0.1:26659,D=http://127.0.0.1:26660,F=http://127.0.0.1:26661",
  [int]$TimeoutSec = 5,
  [switch]$SkipValidators,
  [switch]$AsJson
)

$ErrorActionPreference = "Stop"

function Parse-Targets([string]$raw) {
  $map = [ordered]@{}
  foreach ($entry in ($raw -split ",")) {
    $part = $entry.Trim()
    if ([string]::IsNullOrWhiteSpace($part)) {
      continue
    }
    $kv = $part -split "=", 2
    if ($kv.Count -ne 2) {
      throw "invalid target entry: '$part' (expected NODE=http://host:port)"
    }
    $id = $kv[0].Trim().ToUpperInvariant()
    $rpc = $kv[1].Trim().TrimEnd("/")
    if ($id -eq "" -or $rpc -eq "") {
      throw "invalid target entry: '$part'"
    }
    $map[$id] = $rpc
  }
  if ($map.Count -eq 0) {
    throw "no targets parsed"
  }
  return $map
}

function Invoke-JsonGet([string]$url, [int]$timeoutSec) {
  try {
    return Invoke-RestMethod -Method Get -Uri $url -TimeoutSec $timeoutSec -ErrorAction Stop
  } catch {
    return $null
  }
}

function Join-IDs([object[]]$items) {
  if (-not $items -or $items.Count -eq 0) {
    return "-"
  }
  return (($items | ForEach-Object { "$_" }) -join ",")
}

function Short-Hash([object]$v, [int]$n = 8) {
  $s = "$v".Trim()
  if ([string]::IsNullOrWhiteSpace($s) -or $s -eq "-") {
    return "-"
  }
  if ($s.Length -le $n) {
    return $s
  }
  return $s.Substring(0, $n)
}

function To-IntOrNull([object]$v) {
  if ($null -eq $v) { return $null }
  try { return [int64]$v } catch { return $null }
}

$targetMap = Parse-Targets $Targets
$rows = @()
$onlineByNode = @{}
$offlineByNode = @{}

foreach ($id in $targetMap.Keys) {
  $rpc = $targetMap[$id]
  $status = Invoke-JsonGet "$rpc/status" $TimeoutSec
  if ($null -eq $status) {
    $rows += [pscustomobject]@{
      Node = $id
      RPC = $rpc
      Height = $null
      Finalized = $null
      Ready = $false
      Syncing = $false
      WaitReason = "rpc_unreachable"
      Peers = $null
      LiveReq = "-"
      StrictHbOut = "-"
      LivenessMode = "-"
      DriftLimit = "-"
      Autoheal = "-"
      AutohealReason = "-"
      StartupExpected = "-"
      StartupGot = "-"
      Online = "-"
      Offline = "-"
      Error = "status_unreachable"
    }
    $onlineByNode[$id] = @()
    $offlineByNode[$id] = @()
    continue
  }

  $online = @()
  $offline = @()
  if (-not $SkipValidators) {
    $finalized = To-IntOrNull $status.finalized_height
    $heightHint = if ($null -ne $finalized -and $finalized -ge 0) { [uint64]($finalized + 1) } else { 0 }
    $validators = $null
    if ($heightHint -gt 0) {
      $validators = Invoke-JsonGet "$rpc/validators?height=$heightHint" $TimeoutSec
    }
    if ($null -eq $validators) {
      $validators = Invoke-JsonGet "$rpc/validators" $TimeoutSec
    }
    if ($null -ne $validators) {
      if ($validators.online_validators) {
        $online = @($validators.online_validators | ForEach-Object { "$_".Trim().ToUpperInvariant() } | Where-Object { $_ } | Sort-Object -Unique)
      } elseif ($validators.validators) {
        $online = @($validators.validators | ForEach-Object { "$_".Trim().ToUpperInvariant() } | Where-Object { $_ } | Sort-Object -Unique)
      }
      if ($validators.inactive_validators) {
        $offline = @($validators.inactive_validators | ForEach-Object { "$_".Trim().ToUpperInvariant() } | Where-Object { $_ } | Sort-Object -Unique)
      }
    }
  }

  $onlineByNode[$id] = $online
  $offlineByNode[$id] = $offline

  $live = To-IntOrNull $status.live_validators
  $required = To-IntOrNull $status.required_quorum
  $strict = To-IntOrNull $status.validator_live_strict_count
  $heartbeat = To-IntOrNull $status.validator_live_heartbeat_count
  $outOfDrift = To-IntOrNull $status.validator_live_out_of_drift_count

  if ($null -eq $strict -and $null -ne $live) {
    $strict = $live
  }
  if ($null -eq $heartbeat -and $null -ne $live) {
    $heartbeat = $live
  }
  if ($null -eq $outOfDrift) {
    $outOfDrift = 0
  }

  $rows += [pscustomobject]@{
    Node = $id
    RPC = $rpc
    Height = To-IntOrNull $status.height
    Finalized = To-IntOrNull $status.finalized_height
    Ready = [bool]$status.ready
    Syncing = [bool]$status.syncing
    WaitReason = "$($status.wait_reason)"
    Peers = To-IntOrNull $status.peers
    LiveReq = "$(if ($null -eq $live) { "-" } else { $live })/$(if ($null -eq $required) { "-" } else { $required })"
    StrictHbOut = "$(if ($null -eq $strict) { "-" } else { $strict })/$(if ($null -eq $heartbeat) { "-" } else { $heartbeat })/$(if ($null -eq $outOfDrift) { "-" } else { $outOfDrift })"
    SyncProvider = "$(if ([string]::IsNullOrWhiteSpace("$($status.sync_provider)")) { "-" } else { "$($status.sync_provider)" })"
    SyncStallSeconds = To-IntOrNull $status.sync_stall_seconds
    SnapshotHeightApplied = To-IntOrNull $status.snapshot_height_applied
    DeltaRemainingBlocks = To-IntOrNull $status.delta_remaining_blocks
    LivenessMode = "$(if ([string]::IsNullOrWhiteSpace("$($status.validator_liveness_mode)")) { "-" } else { "$($status.validator_liveness_mode)" })"
    DriftLimit = "$(if ($null -eq $status.validator_liveness_max_height_drift_blocks) { "-" } else { $status.validator_liveness_max_height_drift_blocks })"
    Autoheal = "$(if ([string]::IsNullOrWhiteSpace("$($status.validator_autoheal_state)")) { "-" } else { "$($status.validator_autoheal_state)" })"
    AutohealReason = "$(if ([string]::IsNullOrWhiteSpace("$($status.validator_autoheal_last_reason)")) { "-" } else { "$($status.validator_autoheal_last_reason)" })"
    StartupExpected = Short-Hash $status.validator_startup_self_check_expected_hash
    StartupGot = Short-Hash $status.validator_startup_self_check_got_hash
    Online = Join-IDs $online
    Offline = Join-IDs $offline
    Error = $null
  }
}

$reporters = @($targetMap.Keys | Sort-Object)
$reachableRows = @($rows | Where-Object { -not $_.Error })
$maxFinalized = $null
$minFinalized = $null
if ($reachableRows.Count -gt 0) {
  $finalizedValues = @($reachableRows | Where-Object { $null -ne $_.Finalized } | ForEach-Object { [int64]$_.Finalized })
  if ($finalizedValues.Count -gt 0) {
    $maxFinalized = ($finalizedValues | Measure-Object -Maximum).Maximum
    $minFinalized = ($finalizedValues | Measure-Object -Minimum).Minimum
  }
}

$allValidators = @{}
foreach ($rid in $reporters) {
  $online = @($onlineByNode[$rid])
  foreach ($v in $online) {
    if (-not $allValidators.ContainsKey($v)) {
      $allValidators[$v] = $true
    }
  }
}

$inconsistencies = @()
$majorityOnline = @()
$reporterCount = $reporters.Count
$majorityThreshold = [Math]::Floor($reporterCount / 2) + 1
foreach ($v in ($allValidators.Keys | Sort-Object)) {
  $present = @($reporters | Where-Object { @($onlineByNode[$_]) -contains $v })
  $missing = @($reporters | Where-Object { @($onlineByNode[$_]) -notcontains $v })
  if ($present.Count -ge $majorityThreshold) {
    $majorityOnline += $v
  }
  if ($present.Count -gt 0 -and $present.Count -lt $reporterCount) {
    $inconsistencies += [pscustomobject]@{
      Validator = $v
      Votes = "$($present.Count)/$reporterCount"
      SeenBy = Join-IDs $present
      MissingFrom = Join-IDs $missing
    }
  }
}

$viewDiff = @()
foreach ($rid in $reporters) {
  $online = @($onlineByNode[$rid])
  $missing = @($majorityOnline | Where-Object { $online -notcontains $_ })
  $extra = @($online | Where-Object { $majorityOnline -notcontains $_ })
  if ($missing.Count -gt 0 -or $extra.Count -gt 0) {
    $viewDiff += [pscustomobject]@{
      Node = $rid
      MissingFromMajority = Join-IDs $missing
      ExtraVsMajority = Join-IDs $extra
    }
  }
}

$lagRows = @()
if ($null -ne $maxFinalized) {
  foreach ($r in $reachableRows) {
    if ($null -eq $r.Finalized) { continue }
    $lag = [int64]$maxFinalized - [int64]$r.Finalized
    if ($lag -gt 0) {
      $lagRows += [pscustomobject]@{
        Node = $r.Node
        Finalized = $r.Finalized
        LagBlocks = $lag
      }
    }
  }
}

$output = [ordered]@{
  generated_at = (Get-Date).ToString("s")
  targets = $targetMap
  node_snapshot = $rows
  majority_online = $majorityOnline
  inconsistencies = $inconsistencies
  view_diff_vs_majority = $viewDiff
  lagging_nodes = $lagRows
  finalized_min = $minFinalized
  finalized_max = $maxFinalized
}

if ($AsJson) {
  $output | ConvertTo-Json -Depth 8
  exit 0
}

Write-Host ""
Write-Host "=== Node Snapshot ==="
$rows |
  Select-Object Node, Finalized, Height, Ready, Syncing, WaitReason, Peers, LiveReq, StrictHbOut, SyncProvider, SyncStallSeconds, SnapshotHeightApplied, DeltaRemainingBlocks, LivenessMode, DriftLimit, StartupExpected, StartupGot, Autoheal, AutohealReason, Error |
  Format-Table -AutoSize

Write-Host ""
Write-Host "=== Online / Offline View By Node ==="
$rows | Select-Object Node, Online, Offline | Format-Table -AutoSize -Wrap

Write-Host ""
Write-Host "=== Majority Online Set (threshold $majorityThreshold/$reporterCount) ==="
if ($majorityOnline.Count -eq 0) {
  Write-Host "none"
} else {
  Write-Host ($majorityOnline -join ",")
}

Write-Host ""
Write-Host "=== Inconsistencies (split-view validators) ==="
if ($inconsistencies.Count -eq 0) {
  Write-Host "none"
} else {
  $inconsistencies | Format-Table -AutoSize -Wrap
}

Write-Host ""
Write-Host "=== Node View Diff vs Majority ==="
if ($viewDiff.Count -eq 0) {
  Write-Host "none"
} else {
  $viewDiff | Format-Table -AutoSize -Wrap
}

Write-Host ""
Write-Host "=== Height Lag ==="
if ($lagRows.Count -eq 0) {
  Write-Host "none"
} else {
  $lagRows | Sort-Object -Property LagBlocks -Descending | Format-Table -AutoSize
}
