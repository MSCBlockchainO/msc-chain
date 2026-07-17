param(
    [string]$UiRoot = (Join-Path $PSScriptRoot "..\ui"),
    [string]$LastModified = "2026-06-18"
)

$ErrorActionPreference = "Stop"
$UiRoot = (Resolve-Path -LiteralPath $UiRoot).Path

function EscapeXml([string]$Value) {
    return [Security.SecurityElement]::Escape($Value)
}

function WriteUtf8([string]$Path, [string]$Content) {
    [IO.File]::WriteAllText($Path, $Content.TrimEnd() + "`n", [Text.UTF8Encoding]::new($false))
}

function UrlSet([string[]]$Urls) {
    $items = foreach ($url in ($Urls | Sort-Object -Unique)) {
        $priority = if ($url -match '^https://[^/]+/$') { "1.0" } elseif ($url -match '/docs/$') { "0.9" } else { "0.7" }
        @"
  <url>
    <loc>$(EscapeXml $url)</loc>
    <lastmod>$LastModified</lastmod>
    <priority>$priority</priority>
  </url>
"@
    }
    return @"
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
$($items -join "`n")
</urlset>
"@
}

function SitemapIndex([string[]]$Sitemaps) {
    $items = foreach ($url in $Sitemaps) {
        @"
  <sitemap>
    <loc>$(EscapeXml $url)</loc>
    <lastmod>$LastModified</lastmod>
  </sitemap>
"@
    }
    return @"
<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
$($items -join "`n")
</sitemapindex>
"@
}

$records = @()
$docsDir = (Resolve-Path -LiteralPath (Join-Path $UiRoot "docs")).Path
$blogDirCandidate = Join-Path $UiRoot "blog"
$blogDir = if (Test-Path -LiteralPath $blogDirCandidate -PathType Container) { (Resolve-Path -LiteralPath $blogDirCandidate).Path } else { "" }
$extraHtmlDirs = @()
if ($blogDir) {
    $extraHtmlDirs += Get-ChildItem -LiteralPath $blogDir -File -Filter "*.html"
}
$htmlFiles = @(
    Get-ChildItem -LiteralPath $UiRoot -File -Filter "*.html"
    Get-ChildItem -LiteralPath (Join-Path $UiRoot "portal") -File -Filter "*.html"
    Get-ChildItem -LiteralPath $docsDir -File -Filter "*.html"
    $extraHtmlDirs
)
foreach ($file in $htmlFiles) {
    $raw = Get-Content -Raw -LiteralPath $file.FullName
    $canonical = [regex]::Match($raw, '<link rel="canonical" href="([^"]+)"', "IgnoreCase").Groups[1].Value
    $robots = [regex]::Match($raw, '<meta name="robots" content="([^"]+)"', "IgnoreCase").Groups[1].Value
    if (-not $canonical -or $robots -match 'noindex') {
        continue
    }
    $records += [pscustomobject]@{
        Canonical = $canonical
        Host      = ([uri]$canonical).Host
        IsDocs    = $file.FullName.StartsWith($docsDir, [System.StringComparison]::OrdinalIgnoreCase)
    }
}

$mainUrls = @($records | Where-Object Host -eq "mscblockexplorer.in" | ForEach-Object Canonical | Sort-Object -Unique)
$explorerUrls = @($records | Where-Object Host -eq "explorer.mscblockexplorer.in" | ForEach-Object Canonical | Sort-Object -Unique)
$walletUrls = @($records | Where-Object Host -eq "wallet.mscblockexplorer.in" | ForEach-Object Canonical | Sort-Object -Unique)
$docsUrls = @($records | Where-Object IsDocs | ForEach-Object Canonical | Sort-Object -Unique)
$mainPageUrls = @($records | Where-Object { $_.Host -eq "mscblockexplorer.in" -and -not $_.IsDocs } | ForEach-Object Canonical | Sort-Object -Unique)

if ($mainPageUrls -notcontains "https://mscblockexplorer.in/") {
    throw "Main sitemap is missing canonical homepage"
}
if ($mainPageUrls -notcontains "https://mscblockexplorer.in/blog/") {
    throw "Main sitemap is missing canonical blog"
}
if ($docsUrls.Count -lt 31) {
    throw "Docs sitemap expected at least 31 URLs, got $($docsUrls.Count)"
}
if ($explorerUrls.Count -lt 15) {
    throw "Explorer sitemap expected at least 15 URLs, got $($explorerUrls.Count)"
}
if ($walletUrls.Count -lt 5) {
    throw "Wallet sitemap expected at least 5 URLs, got $($walletUrls.Count)"
}

WriteUtf8 (Join-Path $UiRoot "sitemap-main.xml") (UrlSet $mainPageUrls)
WriteUtf8 (Join-Path $UiRoot "sitemap-docs.xml") (UrlSet $docsUrls)
WriteUtf8 (Join-Path $UiRoot "sitemap-explorer.xml") (UrlSet $explorerUrls)
WriteUtf8 (Join-Path $UiRoot "sitemap-wallet.xml") (UrlSet $walletUrls)

$docsHost = ([uri]$docsUrls[0]).Host
$mainIndexUrls = @(
    "https://mscblockexplorer.in/sitemap-main.xml"
)
if ($docsHost -eq "mscblockexplorer.in") {
    $mainIndexUrls += "https://mscblockexplorer.in/sitemap-docs.xml"
}
$mainIndex = SitemapIndex $mainIndexUrls
WriteUtf8 (Join-Path $UiRoot "sitemap.xml") $mainIndex
WriteUtf8 (Join-Path $UiRoot "sitemap-index.xml") $mainIndex

WriteUtf8 (Join-Path $UiRoot "robots.txt") @"
User-agent: *
Allow: /
Disallow: /dtl_ide.html

Sitemap: https://mscblockexplorer.in/sitemap.xml
Feed: https://mscblockexplorer.in/feed.xml
Feed: https://mscblockexplorer.in/feed.json
"@
WriteUtf8 (Join-Path $UiRoot "robots-explorer.txt") @"
User-agent: *
Allow: /
Disallow: /dtl_ide.html
Disallow: /explorer-settings.html

Sitemap: https://explorer.mscblockexplorer.in/sitemap.xml
"@
WriteUtf8 (Join-Path $UiRoot "robots-wallet.txt") @"
User-agent: *
Allow: /
Disallow: /create-wallet.html
Disallow: /login.html
Disallow: /send.html
Disallow: /receive.html
Disallow: /transactions.html
Disallow: /settings.html

Sitemap: https://wallet.mscblockexplorer.in/sitemap.xml
"@

$docsSitemapUrl = if ($docsHost -eq "mscblockexplorer.in") { "https://mscblockexplorer.in/sitemap-docs.xml" } else { "https://$docsHost/sitemap.xml" }
WriteUtf8 (Join-Path $UiRoot "robots-docs.txt") @"
User-agent: *
Allow: /

Sitemap: $docsSitemapUrl
"@

Write-Host "Generated SEO sitemaps: main=$($mainPageUrls.Count), docs=$($docsUrls.Count), explorer=$($explorerUrls.Count), wallet=$($walletUrls.Count)."
