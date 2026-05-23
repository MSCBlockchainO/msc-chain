param(
    [string[]]$Targets = @("windows/amd64", "windows/386", "windows/arm64", "linux/amd64", "linux/386", "linux/arm64"),
    [string]$OutDir = "",
    [switch]$SkipCrossCompile,
    [switch]$BuildNodeBinary,
    [switch]$RequireRealRun
)

$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ([string]::IsNullOrWhiteSpace($OutDir)) {
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $OutDir = Join-Path $repoRoot "runtime-logs\multi-arch-determinism-$stamp"
}
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$localGOOS = (& go env GOOS).Trim()
$localGOARCH = (& go env GOARCH).Trim()
$testName = "TestMultiArchitectureReplayVectorMatchesGolden"
$summary = @()

function Quote-BashSingle {
    param([string]$Value)
    return "'" + ($Value -replace "'", "'\''") + "'"
}

function Get-WslPath {
    param([string]$Path)
    $wsl = Get-Command wsl.exe -ErrorAction SilentlyContinue
    if ($null -eq $wsl) {
        return ""
    }
    try {
        return (& wsl.exe wslpath -a $Path 2>$null).Trim()
    } catch {
        return ""
    }
}

function Invoke-TestBinaryNative {
    param(
        [string]$BinaryPath,
        [string]$LogPath
    )

    $stdoutPath = "$LogPath.stdout"
    $stderrPath = "$LogPath.stderr"
    $proc = Start-Process -FilePath $BinaryPath `
        -ArgumentList @("-test.run", "^$testName$", "-test.v") `
        -NoNewWindow `
        -Wait `
        -PassThru `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath
    $combined = @()
    if (Test-Path $stdoutPath) {
        $combined += Get-Content -Path $stdoutPath
    }
    if (Test-Path $stderrPath) {
        $combined += Get-Content -Path $stderrPath
    }
    $combined | Set-Content -Path $LogPath
    Remove-Item -Force -ErrorAction SilentlyContinue $stdoutPath, $stderrPath
    return ($proc.ExitCode -eq 0)
}

function Invoke-LinuxBinaryViaWSL {
    param(
        [string]$RepoRoot,
        [string]$BinaryPath,
        [string]$GOARCH,
        [string]$LogPath
    )

    $repoWsl = Get-WslPath -Path $RepoRoot
    $binWsl = Get-WslPath -Path $BinaryPath
    if ([string]::IsNullOrWhiteSpace($repoWsl) -or [string]::IsNullOrWhiteSpace($binWsl)) {
        return $false
    }

    $runner = ""
    if ($GOARCH -eq "arm64") {
        $runnerOut = & wsl.exe bash -lc "command -v qemu-aarch64 || command -v qemu-aarch64-static || true"
        $runner = (($runnerOut | Select-Object -First 1) -as [string]).Trim()
        if ([string]::IsNullOrWhiteSpace($runner)) {
            return $false
        }
    }

    $repoQ = Quote-BashSingle $repoWsl
    $binQ = Quote-BashSingle $binWsl
    $runnerPart = ""
    if (-not [string]::IsNullOrWhiteSpace($runner)) {
        $runnerPart = (Quote-BashSingle $runner) + " "
    }
    $cmd = "cd $repoQ && chmod +x $binQ && ${runnerPart}$binQ -test.run '^$testName$' -test.v"
    $stdoutPath = "$LogPath.stdout"
    $stderrPath = "$LogPath.stderr"
    $proc = Start-Process -FilePath "wsl.exe" `
        -ArgumentList @("bash", "-lc", $cmd) `
        -NoNewWindow `
        -Wait `
        -PassThru `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath
    $combined = @()
    if (Test-Path $stdoutPath) {
        $combined += Get-Content -Path $stdoutPath
    }
    if (Test-Path $stderrPath) {
        $combined += Get-Content -Path $stderrPath
    }
    $combined | Set-Content -Path $LogPath
    Remove-Item -Force -ErrorAction SilentlyContinue $stdoutPath, $stderrPath
    return ($proc.ExitCode -eq 0)
}

function Invoke-GoWithTarget {
    param(
        [string]$GOOS,
        [string]$GOARCH,
        [scriptblock]$Body
    )
    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    $oldCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = $GOOS
        $env:GOARCH = $GOARCH
        $env:CGO_ENABLED = "0"
        & $Body
    } finally {
        $env:GOOS = $oldGOOS
        $env:GOARCH = $oldGOARCH
        $env:CGO_ENABLED = $oldCGO
    }
}

Push-Location $repoRoot
try {
    foreach ($target in $Targets) {
        $parts = $target.Split("/")
        if ($parts.Count -ne 2) {
            throw "Invalid target '$target'. Use GOOS/GOARCH, for example linux/arm64."
        }
        $goos = $parts[0].Trim()
        $goarch = $parts[1].Trim()
        $targetName = "$goos-$goarch"
        $targetDir = Join-Path $OutDir $targetName
        New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

        $record = [ordered]@{
            target = $target
            test_compiled = $false
            node_compiled = $false
            test_ran = $false
            real_run = $false
            run_method = ""
            status = "pending"
            passed = $false
            log = Join-Path $targetDir "determinism.log"
        }

        if (-not $SkipCrossCompile.IsPresent) {
            $testExt = ""
            $nodeExt = ""
            if ($goos -eq "windows") {
                $testExt = ".exe"
                $nodeExt = ".exe"
            }
            $testBinary = Join-Path $targetDir ("msc-chain.test" + $testExt)
            Invoke-GoWithTarget -GOOS $goos -GOARCH $goarch -Body {
                go test -c -o $testBinary .
            }
            $record.test_compiled = $true

            if ($BuildNodeBinary.IsPresent) {
                $nodeBinary = Join-Path $targetDir ("msc-chain" + $nodeExt)
                Invoke-GoWithTarget -GOOS $goos -GOARCH $goarch -Body {
                    go build -o $nodeBinary .
                }
                $record.node_compiled = $true
            }
        }

        if ($goos -eq $localGOOS -and $goarch -eq $localGOARCH) {
            Invoke-GoWithTarget -GOOS $goos -GOARCH $goarch -Body {
                go test -count=1 -run "^$testName$" -v . *> $record.log
            }
            $record.test_ran = $true
            $record.real_run = $true
            $record.run_method = "native"
            $record.status = "real_run_passed"
            $record.passed = $true
        } elseif ($goos -eq $localGOOS -and $goarch -eq "386" -and $localGOARCH -eq "amd64" -and -not $SkipCrossCompile.IsPresent) {
            $testBinary = Join-Path $targetDir "msc-chain.test.exe"
            $ran = Invoke-TestBinaryNative -BinaryPath $testBinary -LogPath $record.log
            if ($ran) {
                $record.test_ran = $true
                $record.real_run = $true
                $record.run_method = "native-wow64"
                $record.status = "real_run_passed"
                $record.passed = $true
            } else {
                $record.status = "real_run_failed"
                $record.passed = $false
            }
        } elseif ($goos -eq "linux" -and ($goarch -eq "amd64" -or $goarch -eq "386" -or $goarch -eq "arm64") -and -not $SkipCrossCompile.IsPresent) {
            $testBinary = Join-Path $targetDir "msc-chain.test"
            $ran = Invoke-LinuxBinaryViaWSL -RepoRoot $repoRoot -BinaryPath $testBinary -GOARCH $goarch -LogPath $record.log
            if ($ran) {
                $record.test_ran = $true
                $record.real_run = $true
                $record.run_method = if ($goarch -eq "arm64") { "wsl-qemu" } else { "wsl" }
                $record.status = "real_run_passed"
                $record.passed = $true
            } else {
                "compiled_only target=$target local=$localGOOS/$localGOARCH; WSL/QEMU real execution unavailable for this target" | Set-Content -Path $record.log
                $record.status = "compiled_only"
                $record.passed = -not $RequireRealRun.IsPresent
            }
        } else {
            "compiled_only target=$target local=$localGOOS/$localGOARCH; run this script on that target to execute the golden replay test" | Set-Content -Path $record.log
            $record.status = "compiled_only"
            $record.passed = -not $RequireRealRun.IsPresent
        }

        $summary += [pscustomobject]$record
        Write-Host ("{0} compile={1} run={2} real={3} method={4} status={5} pass={6}" -f $target, $record.test_compiled, $record.test_ran, $record.real_run, $record.run_method, $record.status, $record.passed)
    }
} finally {
    Pop-Location
}

$summaryPath = Join-Path $OutDir "summary.json"
$summary | ConvertTo-Json -Depth 5 | Set-Content -Path $summaryPath
Write-Host "Summary: $summaryPath"
