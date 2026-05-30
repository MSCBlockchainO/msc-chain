param(
    [string]$VersionTag = "v1.0.0-mainnet",
    [string]$OutputDir = "dist",
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

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    throw "git is required for release metadata"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "go is required to build release binaries"
}

$gitCommit = (& git rev-parse --verify HEAD).Trim()
$gitShort = (& git rev-parse --short=12 HEAD).Trim()
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

$releaseDir = Join-Path $OutputDir $VersionTag
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null

$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Ext = ".exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; Ext = "" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Ext = "" }
)

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
        & go build -trimpath -buildvcs=false -ldflags="-s -w" -o $outPath .
        $buildOutputs += [ordered]@{
            os = $target.GOOS
            arch = $target.GOARCH
            file = $name
            sha256 = Get-LowerSha256 $outPath
        }
    }
}
finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
}

Copy-Item -LiteralPath "genesis.json" -Destination (Join-Path $releaseDir "genesis.json") -Force
Copy-Item -LiteralPath "config.toml" -Destination (Join-Path $releaseDir "config.toml") -Force
if (Test-Path "docs\MAINNET_GENESIS_FREEZE.md") {
    Copy-Item -LiteralPath "docs\MAINNET_GENESIS_FREEZE.md" -Destination (Join-Path $releaseDir "MAINNET_GENESIS_FREEZE.md") -Force
}
if (Test-Path "docs\MAINNET_LAUNCH_GATE.md") {
    Copy-Item -LiteralPath "docs\MAINNET_LAUNCH_GATE.md" -Destination (Join-Path $releaseDir "MAINNET_LAUNCH_GATE.md") -Force
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
    reproducible_build_flags = "-trimpath -buildvcs=false -ldflags=-s -w CGO_ENABLED=0"
    artifacts = $buildOutputs
}
$manifestPath = Join-Path $releaseDir "release-manifest.json"
$manifest | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 -LiteralPath $manifestPath

$checksumPath = Join-Path $releaseDir "checksums.txt"
Get-ChildItem -LiteralPath $releaseDir -File |
    Sort-Object Name |
    ForEach-Object {
        $hash = Get-LowerSha256 $_.FullName
        "$hash  $($_.Name)"
    } |
    Set-Content -Encoding ASCII -LiteralPath $checksumPath

Write-Host "Release build complete: $releaseDir"
Write-Host "Genesis SHA256: $genesisHash"
Write-Host "Checksums: $checksumPath"
Write-Host "Manifest: $manifestPath"
