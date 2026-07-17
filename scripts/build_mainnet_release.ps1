param(
    [string]$VersionTag = "v1.0.0-mainnet",
    [string]$OutputDir = "dist",
    [string]$SnapshotUrl = $env:MSC_SNAPSHOT_URL,
    [string]$SnapshotPublicKeyPath = $env:MSC_SNAPSHOT_PUBLIC_KEY_PATH,
    [string]$Bootnodes = $env:MSC_BOOTNODES,
    [switch]$AllowDirty
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Resolve-Path (Join-Path $scriptDir "..")
Set-Location $repoRoot

function Get-LowerSha256 {
    param([string]$Path)
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
}

function Get-OptionalLowerSha256 {
    param([string]$Path)
    if (-not $Path) {
        return ""
    }
    if (Test-Path -LiteralPath $Path) {
        return Get-LowerSha256 $Path
    }
    return ""
}

function Write-UTF8NoBOM {
    param(
        [string]$Path,
        [string]$Content
    )
    [System.IO.File]::WriteAllText(
        $Path,
        $Content,
        (New-Object System.Text.UTF8Encoding($false))
    )
}

$removedVMRuntimePackagePaths = @(
    "github.com/ethereum/go-ethereum/core/vm",
    "github.com/ethereum/go-ethereum/core/state",
    "github.com/ethereum/go-ethereum/core/txpool",
    "github.com/ethereum/go-ethereum/eth",
    "github.com/ethereum/go-ethereum/internal/ethapi",
    "github.com/ethereum/go-ethereum/miner",
    "github.com/ethereum/go-ethereum/node"
)

function Assert-NoRemovedVMDependencies {
    param(
        [string[]]$Metadata,
        [string]$Context
    )
    foreach ($rawLine in $Metadata) {
        $dependency = ([string]$rawLine).Trim()
        foreach ($packagePath in $removedVMRuntimePackagePaths) {
            if ($dependency.Equals($packagePath, [System.StringComparison]::OrdinalIgnoreCase) -or
                $dependency.StartsWith($packagePath + "/", [System.StringComparison]::OrdinalIgnoreCase)) {
                throw "removed EVM/VM runtime dependency $packagePath found in $Context"
            }
        }
    }
}

function Get-ReleaseSourceInputs {
    param(
        [string]$RepositoryRoot,
        [array]$Targets
    )

    $root = [System.IO.Path]::GetFullPath($RepositoryRoot).TrimEnd([char[]]@('\', '/'))
    $paths = @{}
    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    $oldCGO = $env:CGO_ENABLED
    try {
        $env:CGO_ENABLED = "0"
        foreach ($target in $Targets) {
            $env:GOOS = $target.GOOS
            $env:GOARCH = $target.GOARCH
            $packageRows = & go list -deps -f '{{if .Module}}{{if .Module.Main}}{{.Dir}}|{{range .GoFiles}}{{.}};{{end}}|{{range .CgoFiles}}{{.}};{{end}}|{{range .EmbedFiles}}{{.}};{{end}}{{end}}{{end}}' . 2>&1
            if ($LASTEXITCODE -ne 0) {
                throw "unable to enumerate release source inputs for $($target.GOOS)/$($target.GOARCH)"
            }
            foreach ($row in $packageRows) {
                if (-not $row) {
                    continue
                }
                $parts = $row -split '\|', 4
                if ($parts.Count -ne 4) {
                    throw "invalid go list source-input row: $row"
                }
                $packageDir = $parts[0]
                foreach ($fileList in $parts[1..3]) {
                    foreach ($fileName in ($fileList -split ';')) {
                        if (-not $fileName) {
                            continue
                        }
                        $fullPath = [System.IO.Path]::GetFullPath((Join-Path $packageDir $fileName))
                        if (-not $fullPath.StartsWith($root, [System.StringComparison]::OrdinalIgnoreCase)) {
                            throw "release source input escaped repository root: $fullPath"
                        }
                        $relative = $fullPath.Substring($root.Length).TrimStart([char[]]@('\', '/')) -replace '\\', '/'
                        $paths[$relative] = $fullPath
                    }
                }
            }
        }
    }
    finally {
        $env:GOOS = $oldGOOS
        $env:GOARCH = $oldGOARCH
        $env:CGO_ENABLED = $oldCGO
    }

    foreach ($relative in @("go.mod", "go.sum", "config.toml", "genesis.json", "scripts/build_mainnet_release.ps1")) {
        $fullPath = Join-Path $root ($relative -replace '/', '\')
        if (-not (Test-Path -LiteralPath $fullPath)) {
            throw "release source input missing: $relative"
        }
        $paths[$relative] = [System.IO.Path]::GetFullPath($fullPath)
    }

    $entries = @()
    foreach ($relative in ($paths.Keys | Sort-Object)) {
        $entries += [ordered]@{
            path = $relative
            sha256 = Get-LowerSha256 $paths[$relative]
            source_path = $paths[$relative]
        }
    }
    return $entries
}

function Write-ReleaseSourceBundle {
    param(
        [array]$SourceInputs,
        [string]$ManifestPath,
        [string]$BundlePath
    )

    $publicEntries = @()
    foreach ($entry in $SourceInputs) {
        $publicEntries += [ordered]@{
            path = $entry.path
            sha256 = $entry.sha256
        }
    }
    $manifestJSON = $publicEntries | ConvertTo-Json -Depth 4
    [System.IO.File]::WriteAllText($ManifestPath, $manifestJSON, (New-Object System.Text.UTF8Encoding($false)))

    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    if (Test-Path -LiteralPath $BundlePath) {
        Remove-Item -LiteralPath $BundlePath -Force
    }
    $archive = [System.IO.Compression.ZipFile]::Open($BundlePath, [System.IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($entry in $SourceInputs) {
            [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
                $archive,
                $entry.source_path,
                $entry.path,
                [System.IO.Compression.CompressionLevel]::Optimal
            ) | Out-Null
        }
    }
    finally {
        $archive.Dispose()
    }
}

function Assert-ReleaseSourceInputsUnchanged {
    param([array]$SourceInputs)

    $changed = @()
    foreach ($entry in $SourceInputs) {
        if (-not (Test-Path -LiteralPath $entry.source_path)) {
            $changed += "$($entry.path):missing"
            continue
        }
        $currentSHA256 = Get-LowerSha256 $entry.source_path
        if ($currentSHA256 -ne $entry.sha256) {
            $changed += "$($entry.path):changed"
        }
    }
    if ($changed.Count -gt 0) {
        throw "release source changed after capture: $($changed -join ', ')"
    }
}

function Invoke-ReleaseTestGate {
    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    $oldCGO = $env:CGO_ENABLED
    $hostOS = (& go env GOHOSTOS).Trim()
    $hostArch = (& go env GOHOSTARCH).Trim()
    if (-not $hostOS -or -not $hostArch) {
        throw "unable to resolve Go host platform for release tests"
    }

    $started = Get-Date
    try {
        $env:GOOS = $hostOS
        $env:GOARCH = $hostArch
        $env:CGO_ENABLED = "0"
        $testOutput = & go test ./... -vet=off -count=1 2>&1
        $testExitCode = $LASTEXITCODE
        foreach ($line in $testOutput) {
            Write-Host $line
        }
        if ($testExitCode -ne 0) {
            throw "release test gate failed on $hostOS/$hostArch"
        }
    }
    finally {
        $env:GOOS = $oldGOOS
        $env:GOARCH = $oldGOARCH
        $env:CGO_ENABLED = $oldCGO
    }

    return [ordered]@{
        status = "passed"
        command = "CGO_ENABLED=0 go test ./... -vet=off -count=1"
        host = "$hostOS/$hostArch"
        duration_seconds = [math]::Round(((Get-Date) - $started).TotalSeconds, 3)
    }
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    throw "git is required for release metadata"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "go is required to build release binaries"
}

$sourceDependencies = & go list -deps . 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "go list dependency audit failed"
}
Assert-NoRemovedVMDependencies -Metadata $sourceDependencies -Context "source dependency graph"

$gitCommit = (& git rev-parse --verify HEAD).Trim()
$gitShort = (& git rev-parse --short=12 HEAD).Trim()
$buildLdflags = "-s -w -X main.BuildVersionTag=$VersionTag -X main.BuildGitCommit=$gitCommit"
$gitStatus = (& git status --porcelain)
$gitDirty = @($gitStatus).Count -gt 0
if ($gitDirty -and -not $AllowDirty) {
    throw "working tree is dirty; commit or use -AllowDirty for a non-publish smoke build"
}

$genesisHash = Get-LowerSha256 "genesis.json"
$configText = Get-Content -Raw -LiteralPath "config.toml"
if ($configText -notmatch 'genesis_hash\s*=\s*"([0-9a-fA-F]{64})"') {
    throw "config.toml does not contain a 64-hex chain.genesis_hash"
}
$configGenesisHash = $Matches[1].ToLowerInvariant()
if ($configGenesisHash -ne $genesisHash) {
    throw "config.toml genesis_hash $configGenesisHash does not match genesis.json $genesisHash"
}

$varText = Get-Content -Raw -LiteralPath "var.go"
if ($varText -notmatch 'GenesisHashExpected\s*=\s*"([0-9a-fA-F]{64})"') {
    throw "var.go does not pin GenesisHashExpected"
}
$compiledGenesisHash = $Matches[1].ToLowerInvariant()
if ($compiledGenesisHash -ne $genesisHash) {
    throw "compiled GenesisHashExpected $compiledGenesisHash does not match genesis.json $genesisHash"
}

$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Ext = ".exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; Ext = "" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Ext = "" }
)

$releaseDir = Join-Path $OutputDir $VersionTag
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null

$sourceInputs = Get-ReleaseSourceInputs -RepositoryRoot $repoRoot -Targets $targets
$testGate = Invoke-ReleaseTestGate
Assert-ReleaseSourceInputsUnchanged -SourceInputs $sourceInputs
$sourceInputsManifestPath = Join-Path $releaseDir "source-inputs.json"
$sourceInputsBundlePath = Join-Path $releaseDir "source-inputs.zip"
Write-ReleaseSourceBundle -SourceInputs $sourceInputs -ManifestPath $sourceInputsManifestPath -BundlePath $sourceInputsBundlePath
$sourceInputsManifestSHA256 = Get-LowerSha256 $sourceInputsManifestPath
$sourceInputsBundleSHA256 = Get-LowerSha256 $sourceInputsBundlePath

$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = "0"
    $buildOutputs = @()
    foreach ($target in $targets) {
        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH
        $name = "msc-chain-$VersionTag-$($target.GOOS)-$($target.GOARCH)$($target.Ext)"
        $outPath = Join-Path $releaseDir $name
        & go build -trimpath -buildvcs=false -ldflags="$buildLdflags" -o $outPath .
        if ($LASTEXITCODE -ne 0) {
            throw "release build failed for $($target.GOOS)/$($target.GOARCH)"
        }
        $binaryMetadata = & go version -m $outPath 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "unable to inspect release metadata for $name"
        }
        Assert-NoRemovedVMDependencies -Metadata $binaryMetadata -Context $name
        $buildOutputs += [ordered]@{
            os = $target.GOOS
            arch = $target.GOARCH
            file = $name
            sha256 = Get-LowerSha256 $outPath
        }
        $aliasName = "msc-$VersionTag-$($target.GOOS)-$($target.GOARCH)$($target.Ext)"
        $aliasPath = Join-Path $releaseDir $aliasName
        Copy-Item -LiteralPath $outPath -Destination $aliasPath -Force
        $buildOutputs += [ordered]@{
            os = $target.GOOS
            arch = $target.GOARCH
            file = $aliasName
            sha256 = Get-LowerSha256 $aliasPath
        }
    }
}
finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
}

Assert-ReleaseSourceInputsUnchanged -SourceInputs $sourceInputs

Copy-Item -LiteralPath "genesis.json" -Destination (Join-Path $releaseDir "genesis.json") -Force
Copy-Item -LiteralPath "config.toml" -Destination (Join-Path $releaseDir "config.toml") -Force
if (Test-Path "docs\MAINNET_GENESIS_FREEZE.md") {
    Copy-Item -LiteralPath "docs\MAINNET_GENESIS_FREEZE.md" -Destination (Join-Path $releaseDir "MAINNET_GENESIS_FREEZE.md") -Force
}
if (Test-Path "docs\MAINNET_LAUNCH_GATE.md") {
    Copy-Item -LiteralPath "docs\MAINNET_LAUNCH_GATE.md" -Destination (Join-Path $releaseDir "MAINNET_LAUNCH_GATE.md") -Force
}

$releaseScriptsDir = Join-Path $releaseDir "scripts"
New-Item -ItemType Directory -Force -Path $releaseScriptsDir | Out-Null
foreach ($scriptPath in @("scripts\install_msc_node.sh", "scripts\bootstrap_msc_snapshot.sh", "scripts\publish_msc_snapshot.sh", "scripts\install_msc_snapshot_publisher.sh", "scripts\sync_msc_snapshot_mirror.sh", "scripts\install_msc_snapshot_web_sync.sh")) {
    if (Test-Path -LiteralPath $scriptPath) {
        Copy-Item -LiteralPath $scriptPath -Destination (Join-Path $releaseScriptsDir (Split-Path -Leaf $scriptPath)) -Force
    }
}

$snapshotPublicKeyReleasePath = ""
if ($SnapshotPublicKeyPath) {
    if (-not (Test-Path -LiteralPath $SnapshotPublicKeyPath)) {
        throw "SnapshotPublicKeyPath not found: $SnapshotPublicKeyPath"
    }
    $snapshotPublicKeyReleasePath = Join-Path $releaseDir "snapshot_pubkey.pem"
    Copy-Item -LiteralPath $SnapshotPublicKeyPath -Destination $snapshotPublicKeyReleasePath -Force
}

$manifest = [ordered]@{
    version_tag = $VersionTag
    git_commit = $gitCommit
    git_short = $gitShort
    git_dirty = $gitDirty
    genesis_sha256 = $genesisHash
    config_genesis_hash = $configGenesisHash
    compiled_genesis_hash = $compiledGenesisHash
    generated_utc = (Get-Date).ToUniversalTime().ToString("o")
    go_version = (& go version).Trim()
    reproducible_build_flags = "-trimpath -buildvcs=false -ldflags=`"$buildLdflags`" CGO_ENABLED=0"
    test_gate = $testGate.status
    test_command = $testGate.command
    test_host = $testGate.host
    test_duration_seconds = $testGate.duration_seconds
    removed_vm_dependency_gate = "passed"
    source_inputs_manifest = "source-inputs.json"
    source_inputs_manifest_sha256 = $sourceInputsManifestSHA256
    source_inputs_bundle = "source-inputs.zip"
    source_inputs_bundle_sha256 = $sourceInputsBundleSHA256
    artifacts = $buildOutputs
}
$manifestPath = Join-Path $releaseDir "release-manifest.json"
Write-UTF8NoBOM -Path $manifestPath -Content ($manifest | ConvertTo-Json -Depth 6)

$checksumPath = Join-Path $releaseDir "checksums.txt"
if (Test-Path -LiteralPath $checksumPath) {
    Remove-Item -LiteralPath $checksumPath -Force
}
$releaseRoot = (Resolve-Path -LiteralPath $releaseDir).Path
Get-ChildItem -LiteralPath $releaseDir -Recurse -File |
    Sort-Object FullName |
    ForEach-Object {
        $hash = Get-LowerSha256 $_.FullName
        $relative = $_.FullName.Substring($releaseRoot.Length).TrimStart("\", "/") -replace "\\", "/"
        "$hash  $relative"
    } |
    Set-Content -Encoding ASCII -LiteralPath $checksumPath

$latestArtifacts = @()
foreach ($artifact in $buildOutputs) {
    if ($artifact["os"] -eq "linux" -and $artifact["file"].StartsWith("msc-$VersionTag-")) {
        $latestArtifacts += [ordered]@{
            os = $artifact["os"]
            arch = $artifact["arch"]
            file = $artifact["file"]
            sha256 = $artifact["sha256"]
            url_path = "$VersionTag/$($artifact["file"])"
        }
    }
}
$latest = [ordered]@{
    version_tag = $VersionTag
    git_commit = $gitCommit
    git_short = $gitShort
    generated_utc = $manifest["generated_utc"]
    manifest_path = "$VersionTag/release-manifest.json"
    checksums_path = "$VersionTag/checksums.txt"
    genesis_path = "$VersionTag/genesis.json"
    genesis_sha256 = $(Get-LowerSha256 (Join-Path $releaseDir "genesis.json"))
    config_path = "$VersionTag/config.toml"
    config_sha256 = $(Get-LowerSha256 (Join-Path $releaseDir "config.toml"))
    source_inputs_manifest_path = "$VersionTag/source-inputs.json"
    source_inputs_manifest_sha256 = $sourceInputsManifestSHA256
    source_inputs_bundle_path = "$VersionTag/source-inputs.zip"
    source_inputs_bundle_sha256 = $sourceInputsBundleSHA256
    installer_path = "$VersionTag/scripts/install_msc_node.sh"
    installer_sha256 = $(Get-OptionalLowerSha256 (Join-Path $releaseScriptsDir "install_msc_node.sh"))
    snapshot_bootstrap_path = "$VersionTag/scripts/bootstrap_msc_snapshot.sh"
    snapshot_bootstrap_sha256 = $(Get-OptionalLowerSha256 (Join-Path $releaseScriptsDir "bootstrap_msc_snapshot.sh"))
    snapshot_url = $SnapshotUrl
    snapshot_required = [bool]$SnapshotUrl
    snapshot_require_signature = [bool]$snapshotPublicKeyReleasePath
    snapshot_public_key_path = $(if ($snapshotPublicKeyReleasePath) { "$VersionTag/snapshot_pubkey.pem" } else { "" })
    snapshot_public_key_sha256 = $(Get-OptionalLowerSha256 $snapshotPublicKeyReleasePath)
    bootnodes = $Bootnodes
    artifacts = $latestArtifacts
}
$latestPath = Join-Path $OutputDir "latest.json"
Write-UTF8NoBOM -Path $latestPath -Content ($latest | ConvertTo-Json -Depth 6)

Write-Host "Release build complete: $releaseDir"
Write-Host "Genesis SHA256: $genesisHash"
Write-Host "Checksums: $checksumPath"
Write-Host "Manifest: $manifestPath"
Write-Host "Latest metadata: $latestPath"
