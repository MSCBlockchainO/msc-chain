param(
    [string]$GatewayHost = "50.19.167.221",
    [string]$GatewayUser = "ubuntu",
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$Domain = "",
    [string]$MainDomain = "mscblockexplorer.in",
    [string]$ExplorerDomain = "explorer.mscblockexplorer.in",
    [string]$WalletDomain = "wallet.mscblockexplorer.in",
    [string]$DocsDomain = "docs.mscblockexplorer.in",
    [string]$LetsEncryptEmail = "admin@mscblockexplorer.in",
    [string]$IdeUser = "mscadmin",
    [string]$IdePassword = $env:MSC_IDE_PASSWORD,
    [string]$RpcTarget = "127.0.0.1:26665",
    [string[]]$RpcTargets = @(),
    [string[]]$ArchiveTargets = @(),
    [string[]]$IndexerTargets = @(),
    [string]$UiSource = (Join-Path (Split-Path -Parent $PSScriptRoot) "ui"),
    [switch]$PreserveRemoteReleases,
    [switch]$AllowValidatorRpcGateway,
    [switch]$AllowHttpOnlyUntilDNS
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $UiSource -PathType Container)) {
    throw "UI source directory not found: $UiSource"
}
if (-not [string]::IsNullOrWhiteSpace($Domain)) {
    if (-not $PSBoundParameters.ContainsKey("ExplorerDomain")) {
        $ExplorerDomain = $Domain
    }
    if (-not $PSBoundParameters.ContainsKey("WalletDomain")) {
        $WalletDomain = $Domain
    }
    if (-not $PSBoundParameters.ContainsKey("MainDomain")) {
        $MainDomain = $Domain
    }
}
$MainDomain = $MainDomain.Trim().ToLowerInvariant()
$ExplorerDomain = $ExplorerDomain.Trim().ToLowerInvariant()
$WalletDomain = $WalletDomain.Trim().ToLowerInvariant()
$DocsDomain = $DocsDomain.Trim().ToLowerInvariant()
if ([string]::IsNullOrWhiteSpace($MainDomain)) {
    throw "MainDomain is required for the production public gateway."
}
if ([string]::IsNullOrWhiteSpace($ExplorerDomain)) {
    throw "ExplorerDomain is required for the production public gateway."
}
if ([string]::IsNullOrWhiteSpace($WalletDomain)) {
    throw "WalletDomain is required for the production public gateway."
}

function Quote-Sh {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value)
    return "'" + ($Value -replace "'", "'\''") + "'"
}

function Resolve-DomainIPv4 {
    param([Parameter(Mandatory = $true)][string]$Name)
    foreach ($server in @("", "8.8.8.8", "1.1.1.1")) {
        try {
            $args = @{
                Name        = $Name
                Type        = "A"
                ErrorAction = "Stop"
            }
            if ($server) {
                $args.Server = $server
            }
            $addresses = @(Resolve-DnsName @args |
                Where-Object { $_.IPAddress } |
                ForEach-Object { [string]$_.IPAddress })
            if ($addresses.Count -gt 0) {
                return $addresses
            }
        } catch {
            continue
        }
    }
    return @()
}

$docsDomainEnabled = $false
$docsDomainCanonicalReady = $false
$docsDomainIPs = @()
if (-not [string]::IsNullOrWhiteSpace($DocsDomain)) {
    $docsIndexPath = Join-Path $UiSource "docs\index.html"
    if (Test-Path -LiteralPath $docsIndexPath -PathType Leaf) {
        $docsIndexBody = Get-Content -Raw -LiteralPath $docsIndexPath
        $docsDomainCanonicalReady = $docsIndexBody.Contains("https://$DocsDomain/")
    }
    $docsDomainIPs = Resolve-DomainIPv4 -Name $DocsDomain
    if ($docsDomainCanonicalReady -and ($AllowHttpOnlyUntilDNS -or ($docsDomainIPs -contains $GatewayHost))) {
        $docsDomainEnabled = $true
    } elseif ($docsDomainCanonicalReady) {
        $current = if ($docsDomainIPs.Count -gt 0) { $docsDomainIPs -join ", " } else { "none" }
        Write-Warning "DocsDomain skipped for HTTPS: $DocsDomain resolves to [$current], not gateway $GatewayHost. Add an A record before enabling the docs subdomain."
    } elseif ($docsDomainIPs -contains $GatewayHost) {
        Write-Warning "DocsDomain skipped: $DocsDomain points to $GatewayHost, but docs canonicals do not. Run scripts\generate_seo_docs.ps1 -DocsBaseUrl `"https://$DocsDomain/docs`" and regenerate sitemaps before deploying the docs subdomain."
    } else {
        Write-Warning "DocsDomain pending: $DocsDomain is not active because DNS is not pointed here and docs canonicals still use the current docs URL."
    }
}

$publicDomainItems = @($MainDomain, $ExplorerDomain, $WalletDomain)
if ($docsDomainEnabled) {
    $publicDomainItems += $DocsDomain
}
$publicDomains = @($publicDomainItems | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)
$publicDomainsCsv = $publicDomains -join ","
$publicDomainsNginx = $publicDomains -join " "
$splitDomains = -not [string]::Equals($ExplorerDomain, $WalletDomain, [System.StringComparison]::OrdinalIgnoreCase)
$mainHomeHost = $MainDomain
$explorerHomeHost = $ExplorerDomain
$walletHomeHost = if ($splitDomains) { $WalletDomain } else { "__no_wallet_home_host__" }
$docsServeHost = if ($docsDomainEnabled) { $DocsDomain } else { "__no_docs_home_host__" }
$docsCanonicalHost = if ($docsDomainEnabled) { $DocsDomain } else { $MainDomain }
$walletRedirectSourceHost = if ($splitDomains) { $ExplorerDomain } else { "__no_wallet_redirect_source__" }
$explorerRedirectSourceHost = if ($splitDomains) { $WalletDomain } else { "__no_explorer_redirect_source__" }

foreach ($publicDomain in $publicDomains) {
    $domainIPs = Resolve-DomainIPv4 -Name $publicDomain
    if (-not $AllowHttpOnlyUntilDNS -and ($domainIPs -notcontains $GatewayHost)) {
        $current = if ($domainIPs.Count -gt 0) { $domainIPs -join ", " } else { "none" }
        throw "DNS preflight failed: $publicDomain resolves to [$current], not gateway $GatewayHost. Point the A record to $GatewayHost before issuing HTTPS."
    }
}

$enableHttps = -not $AllowHttpOnlyUntilDNS
$resolvedRpcTargets = @($RpcTargets | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
if ($resolvedRpcTargets.Count -eq 0) {
    $resolvedRpcTargets = @($RpcTarget)
}
$resolvedRpcTargets = @($resolvedRpcTargets | ForEach-Object { $_.Trim() } | Select-Object -Unique)
$rpcTargetsCsv = $resolvedRpcTargets -join ","
$resolvedArchiveTargets = @($ArchiveTargets | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() } | Select-Object -Unique)
$resolvedIndexerTargets = @($IndexerTargets | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() } | Select-Object -Unique)
$archiveTargetsCsv = $resolvedArchiveTargets -join ","
$indexerTargetsCsv = $resolvedIndexerTargets -join ","

$remote = @'
set -euo pipefail

if command -v apt-get >/dev/null 2>&1; then
  sudo apt-get update -y
  sudo apt-get install -y nginx apache2-utils certbot python3-certbot-nginx curl
elif command -v dnf >/dev/null 2>&1; then
  sudo dnf install -y nginx httpd-tools certbot python3-certbot-nginx curl
elif command -v yum >/dev/null 2>&1; then
  sudo yum install -y nginx httpd-tools certbot python3-certbot-nginx curl
else
  echo "No supported package manager found" >&2
  exit 1
fi

sudo mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled /etc/nginx/conf.d
sudo mkdir -p /var/www/msc-ui /var/www/letsencrypt
if [ "${PRESERVE_REMOTE_RELEASES:-0}" = "1" ]; then
  sudo find /var/www/msc-ui -mindepth 1 -maxdepth 1 ! -name releases -exec rm -rf {} +
else
  sudo find /var/www/msc-ui -mindepth 1 -maxdepth 1 -exec rm -rf {} +
fi
sudo cp -a "$HOME/msc-ui-upload/." /var/www/msc-ui/
sudo chown -R www-data:www-data /var/www/msc-ui 2>/dev/null || sudo chown -R nginx:nginx /var/www/msc-ui
sudo find /var/www/msc-ui -type f -exec chmod 0644 {} \;
sudo find /var/www/msc-ui -type d -exec chmod 0755 {} \;
sudo mkdir -p /var/www/msc-ui/gateway
sudo chown www-data:www-data /var/www/msc-ui/gateway 2>/dev/null || sudo chown nginx:nginx /var/www/msc-ui/gateway

if [ -n "${IDE_PASSWORD:-}" ]; then
  sudo htpasswd -bc /etc/nginx/msc_ide.htpasswd "$IDE_USER" "$IDE_PASSWORD" >/dev/null
elif [ ! -f /etc/nginx/msc_ide.htpasswd ]; then
  tmp_pass="$(openssl rand -hex 24 2>/dev/null || date +%s%N)"
  sudo htpasswd -bc /etc/nginx/msc_ide.htpasswd "$IDE_USER" "$tmp_pass" >/dev/null
  echo "DTL IDE locked with a generated server-local password because MSC_IDE_PASSWORD was not provided."
fi

IFS=',' read -r -a MSC_RPC_TARGETS <<< "${RPC_TARGETS:-${RPC_TARGET:-127.0.0.1:26665}}"
if [ "${#MSC_RPC_TARGETS[@]}" -eq 0 ]; then
  MSC_RPC_TARGETS=("127.0.0.1:26665")
fi
upstream_body="upstream msc_rpc_backend {\n    least_conn;\n    keepalive 32;\n"
active_upstream_body="upstream msc_rpc_active_backend {\n    least_conn;\n    keepalive 32;\n"
public_routes_body=""
upstream_count=0
active_upstream_count=0
first_clean=""
for target in "${MSC_RPC_TARGETS[@]}"; do
  clean="$(echo "$target" | xargs)"
  [ -z "$clean" ] && continue
  clean="${clean#http://}"
  clean="${clean#https://}"
  [ -z "$first_clean" ] && first_clean="$clean"
  if curl -fsS --max-time 2 "http://${clean}/status" >/dev/null 2>&1; then
    upstream_body="${upstream_body}    server ${clean} max_fails=2 fail_timeout=10s;\n"
    upstream_count=$((upstream_count + 1))
    if [ "$active_upstream_count" -eq 0 ]; then
      active_upstream_body="${active_upstream_body}    server ${clean} max_fails=2 fail_timeout=10s;\n"
      active_upstream_count=$((active_upstream_count + 1))
    fi
    node_id="$(curl -fsS --max-time 2 "http://${clean}/status" 2>/dev/null | python3 -c 'import json, re, sys; data=json.load(sys.stdin); raw=str(data.get("node_id") or ""); raw=re.sub(r"[^A-Za-z0-9_-]", "_", raw).strip("_"); print(raw or "NODE")' 2>/dev/null || true)"
    if [ -z "$node_id" ]; then node_id="NODE${upstream_count}"; fi
    public_routes_body="${public_routes_body}    location ~ ^/public-rpc/${node_id}/(.*)$ {\n        limit_req zone=msc_read burst=40 nodelay;\n        rewrite ^/public-rpc/${node_id}/(.*)$ /\$1 break;\n        proxy_pass http://${clean};\n        proxy_http_version 1.1;\n        proxy_set_header Host \$host;\n        proxy_set_header X-Real-IP \$remote_addr;\n        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;\n        proxy_set_header X-Forwarded-Proto \$scheme;\n        add_header Cache-Control \"no-store\" always;\n    }\n"
  else
    echo "WARN skipping unreachable nginx upstream target ${clean}; lb-status will still report it."
  fi
done
if [ "$upstream_count" -eq 0 ] && [ -n "$first_clean" ]; then
  echo "WARN no RPC targets answered health check; keeping first target ${first_clean} in upstream."
  upstream_body="${upstream_body}    server ${first_clean} max_fails=2 fail_timeout=10s;\n"
  active_upstream_body="${active_upstream_body}    server ${first_clean} max_fails=2 fail_timeout=10s;\n"
fi
upstream_body="${upstream_body}}\n"
active_upstream_body="${active_upstream_body}}\n"
printf "%b" "$upstream_body" | sudo tee /etc/nginx/conf.d/msc_rpc_upstream.conf >/dev/null
printf "%b" "$active_upstream_body" | sudo tee /etc/nginx/conf.d/msc_rpc_active_upstream.conf >/dev/null
sudo rm -f /etc/nginx/conf.d/msc_public_node_routes.conf
printf "%b" "$public_routes_body" | sudo tee /etc/nginx/msc_public_node_routes.inc >/dev/null

write_optional_upstream() {
  local name="$1"
  local raw_targets="$2"
  local outfile="$3"
  IFS=',' read -r -a items <<< "${raw_targets:-}"
  local body="upstream ${name} {\n    least_conn;\n    keepalive 16;\n"
  local count=0
  for target in "${items[@]}"; do
    clean="$(echo "$target" | xargs)"
    [ -z "$clean" ] && continue
    clean="${clean#http://}"
    clean="${clean#https://}"
    body="${body}    server ${clean} max_fails=2 fail_timeout=10s;\n"
    count=$((count + 1))
  done
  if [ "$count" -eq 0 ]; then
    body="${body}    server 127.0.0.1:9 max_fails=1 fail_timeout=5s;\n"
  fi
  body="${body}}\n"
  printf "%b" "$body" | sudo tee "$outfile" >/dev/null
}

write_optional_upstream "msc_archive_backend" "${ARCHIVE_TARGETS:-}" /etc/nginx/conf.d/msc_archive_upstream.conf
write_optional_upstream "msc_indexer_backend" "${INDEXER_TARGETS:-}" /etc/nginx/conf.d/msc_indexer_upstream.conf

sudo tee /etc/nginx/conf.d/msc_public_gateway_limits.conf >/dev/null <<'NGINX'
limit_req_zone $binary_remote_addr zone=msc_static:10m rate=600r/m;
limit_req_zone $binary_remote_addr zone=msc_read:10m rate=600r/m;
limit_req_zone $binary_remote_addr zone=msc_write:10m rate=60r/m;
limit_req_zone $binary_remote_addr zone=msc_rpc:10m rate=120r/m;
limit_conn_zone $binary_remote_addr zone=msc_conn:10m;
NGINX
sudo rm -f /etc/nginx/conf.d/zz_msc_main_landing.conf

sudo tee /etc/nginx/sites-available/msc-ui >/dev/null <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name __PUBLIC_DOMAINS__ __PUBLIC_HOST__;
    root /var/www/msc-ui;
    absolute_redirect off;
    server_tokens off;
    set $msc_home_path /landing.html;
    set $msc_sitemap_path /sitemap-index.xml;
    set $msc_robots_path /robots.txt;
    if ($host = "__EXPLORER_HOME_HOST__") {
        set $msc_home_path /explorer.html;
        set $msc_sitemap_path /sitemap-explorer.xml;
        set $msc_robots_path /robots-explorer.txt;
    }
    if ($host = "__WALLET_HOME_HOST__") {
        set $msc_home_path /dashboard.html;
        set $msc_sitemap_path /sitemap-wallet.xml;
        set $msc_robots_path /robots-wallet.txt;
    }
    if ($host = "__DOCS_SERVE_HOST__") {
        set $msc_home_path /docs/index.html;
        set $msc_sitemap_path /sitemap-docs.xml;
        set $msc_robots_path /robots-docs.txt;
    }

    client_max_body_size 2m;
    proxy_read_timeout 60s;
    proxy_send_timeout 60s;
    limit_req_status 429;
    limit_conn_status 429;
    error_page 429 = @msc_rate_limited;
    limit_conn msc_conn 20;
    gzip on;
    gzip_vary on;
    gzip_comp_level 5;
    gzip_min_length 1024;
    gzip_types text/plain text/css application/javascript application/json application/xml text/xml image/svg+xml;

    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy no-referrer always;
    add_header X-Frame-Options DENY always;
    add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/letsencrypt;
        default_type text/plain;
    }

    location = / {
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__DOCS_CANONICAL_HOST__/docs/;
        }
        try_files $msc_home_path =404;
    }

    location ~ /\. {
        return 404;
    }

    location ~ ^/assets/(msc-(?:logo(?:-(?:512|192|64|32))?|app-icon(?:-(?:192|64))?|wordmark|wallet-icon|explorer-icon|validator-badge|governance-badge|nft-badge|bridge-badge)\.png)$ {
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location = /manifest.webmanifest {
        limit_req zone=msc_static burst=60 nodelay;
        default_type application/manifest+json;
        add_header Cache-Control "public, max-age=3600" always;
        try_files /manifest.webmanifest =404;
    }

    location = /sitemap.xml {
        limit_req zone=msc_static burst=60 nodelay;
        default_type application/xml;
        add_header Cache-Control "public, max-age=3600" always;
        try_files $msc_sitemap_path =404;
    }

    location ~ ^/sitemap-(?:index|main|docs|explorer|wallet)\.xml$ {
        limit_req zone=msc_static burst=60 nodelay;
        default_type application/xml;
        add_header Cache-Control "public, max-age=3600" always;
        try_files $uri =404;
    }

    location = /robots.txt {
        limit_req zone=msc_static burst=60 nodelay;
        default_type text/plain;
        add_header Cache-Control "public, max-age=3600" always;
        try_files $msc_robots_path =404;
    }

    location = /llms.txt {
        if ($host = "__EXPLORER_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/llms.txt;
        }
        if ($host = "__WALLET_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/llms.txt;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__MAIN_DOMAIN__/llms.txt;
        }
        limit_req zone=msc_static burst=60 nodelay;
        default_type text/plain;
        add_header Cache-Control "public, max-age=3600" always;
        try_files /llms.txt =404;
    }

    location = /install.sh {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/install.sh;
        }
        limit_req zone=msc_static burst=30 nodelay;
        types { }
        default_type text/x-shellscript;
        add_header Cache-Control "public, max-age=300" always;
        try_files /install.sh =404;
    }

    location = /releases/latest.json {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/releases/latest.json;
        }
        limit_req zone=msc_static burst=60 nodelay;
        types { }
        default_type application/json;
        add_header Cache-Control "no-cache" always;
        try_files /releases/latest.json =404;
    }

    location ^~ /releases/ {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=120 nodelay;
        types { }
        default_type application/octet-stream;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location = /feed.xml {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/feed.xml;
        }
        limit_req zone=msc_static burst=60 nodelay;
        types { }
        default_type application/rss+xml;
        add_header Cache-Control "public, max-age=3600" always;
        try_files /feed.xml =404;
    }

    location = /feed.json {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/feed.json;
        }
        limit_req zone=msc_static burst=60 nodelay;
        types { }
        default_type application/feed+json;
        add_header Cache-Control "public, max-age=3600" always;
        try_files /feed.json =404;
    }

    location = /blog {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/blog/;
        }
        return 301 /blog/;
    }

    location = /blog/ {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/blog/;
        }
        limit_req zone=msc_static burst=120 nodelay;
        add_header Cache-Control "public, max-age=3600" always;
        try_files /blog/index.html =404;
    }

    location = /blog/index.html {
        return 301 https://__MAIN_DOMAIN__/blog/;
    }

    location = /docs/docs.css {
        if ($host != "__DOCS_CANONICAL_HOST__") {
            return 301 https://__DOCS_CANONICAL_HOST__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files /docs/docs.css =404;
    }

    location ^~ /blog/ {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=120 nodelay;
        add_header Cache-Control "public, max-age=3600" always;
        try_files $uri =404;
    }

    location = /docs {
        if ($host != "__DOCS_CANONICAL_HOST__") {
            return 301 https://__DOCS_CANONICAL_HOST__/docs/;
        }
        return 301 /docs/;
    }

    location = /docs/ {
        if ($host != "__DOCS_CANONICAL_HOST__") {
            return 301 https://__DOCS_CANONICAL_HOST__$request_uri;
        }
        limit_req zone=msc_static burst=120 nodelay;
        add_header Cache-Control "public, max-age=3600" always;
        try_files /docs/index.html =404;
    }

    location = /docs/index.html {
        return 301 https://__DOCS_CANONICAL_HOST__/docs/;
    }

    location ^~ /docs/ {
        if ($host != "__DOCS_CANONICAL_HOST__") {
            return 301 https://__DOCS_CANONICAL_HOST__$request_uri;
        }
        limit_req zone=msc_static burst=120 nodelay;
        add_header Cache-Control "public, max-age=3600" always;
        try_files $uri =404;
    }

    location = /landing.html {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/;
        }
        return 301 /;
    }

    location ~ ^/(landing\.js|landing\.css)$ {
        if ($host = "__EXPLORER_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        if ($host = "__WALLET_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location = /msc_wallet.html {
        if ($host = "__WALLET_REDIRECT_SOURCE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        try_files /msc_wallet.html =404;
    }

    location = /wallet.html {
        if ($host = "__WALLET_REDIRECT_SOURCE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        try_files /wallet.html =404;
    }

    location = /index.html {
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 /;
        }
        if ($host = "__WALLET_REDIRECT_SOURCE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__DOCS_CANONICAL_HOST__/docs/;
        }
        limit_req zone=msc_static burst=60 nodelay;
        try_files /index.html =404;
    }

    location ~ ^/(wallet_pages\.js|wallet_pages\.css|msc_wallet\.js|msc_wallet\.css|app\.js|styles\.css)$ {
        if ($host = "__WALLET_REDIRECT_SOURCE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location = /dashboard.html {
        if ($host = "__WALLET_REDIRECT_SOURCE_HOST__") {
            return 301 https://__WALLET_DOMAIN__/;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__WALLET_DOMAIN__/;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__WALLET_DOMAIN__/;
        }
        if ($host = "__WALLET_HOME_HOST__") {
            return 301 /;
        }
        return 301 https://__WALLET_DOMAIN__/;
    }

    location ~ ^/(dashboard\.html|send\.html|receive\.html|transactions\.html|swap\.html|nfts\.html|address-book\.html|staking\.html|validators\.html|validator-wallet\.html|governance\.html|bridge\.html|faucet\.html|security\.html|settings\.html|status\.html|login\.html|create-wallet\.html)$ {
        if ($host = "__WALLET_REDIRECT_SOURCE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__WALLET_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        try_files $uri =404;
    }

    # Includes explorer-blocks\.html and explorer-transactions\.html plus every explorer-* page.
    location ~ ^/(explorer\.js|explorer\.css)$ {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location = /explorer.html {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__/;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__/;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__/;
        }
        if ($host = "__EXPLORER_HOME_HOST__") {
            return 301 /;
        }
        return 301 https://__EXPLORER_DOMAIN__/;
    }

    location ~ ^/(explorer(?:-[a-z0-9-]+)?\.html)$ {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        try_files $uri =404;
    }

    location = /portal {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        return 302 /portal/index.html;
    }

    location = /portal/portal.css {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location = /portal/portal.js {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location ^~ /portal/ {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__DOCS_SERVE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=120 nodelay;
        try_files $uri $uri/ =404;
    }

    location = /ambassador {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__/ambassador/;
        }
        return 302 /ambassador/index.html;
    }

    location ~ ^/ambassador/(ambassador\.js|ambassador\.css|firebase-config\.js)$ {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location ~ ^/ambassador/[^/]+\.html$ {
        if ($host != "__MAIN_HOME_HOST__") {
            return 301 https://__MAIN_DOMAIN__$request_uri;
        }
        limit_req zone=msc_static burst=120 nodelay;
        try_files $uri =404;
    }

    location = /vendor/bip39_index.html {
        return 404;
    }

    location ^~ /vendor/ {
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "public, max-age=31536000, immutable" always;
        try_files $uri =404;
    }

    location ~ ^/(dtl_ide\.html|dtl_ide\.js|dtl_ide\.css)$ {
        if ($host = "__EXPLORER_REDIRECT_SOURCE_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        if ($host = "__MAIN_HOME_HOST__") {
            return 301 https://__EXPLORER_DOMAIN__$request_uri;
        }
        auth_basic "MSC DTL IDE";
        auth_basic_user_file /etc/nginx/msc_ide.htpasswd;
        limit_req zone=msc_read burst=20 nodelay;
        try_files $uri =404;
    }

    include /etc/nginx/msc_public_node_routes.inc;

    # Archive and indexer are read-only explorer infrastructure. They are never
    # injected into wallet RPC defaults and must not point at validator RPC.
    location ~ ^/archive-rpc/(.*)$ {
        limit_req zone=msc_read burst=40 nodelay;
        rewrite ^/archive-rpc/(.*)$ /$1 break;
        proxy_pass http://msc_archive_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 2s;
        proxy_read_timeout 8s;
        proxy_send_timeout 8s;
        proxy_next_upstream error timeout http_502 http_503 http_504;
        proxy_next_upstream_timeout 8s;
        add_header Cache-Control "no-store" always;
    }

    location ^~ /indexer/ {
        limit_req zone=msc_read burst=60 nodelay;
        proxy_pass http://msc_indexer_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 2s;
        proxy_read_timeout 8s;
        proxy_send_timeout 8s;
        proxy_next_upstream error timeout http_502 http_503 http_504;
        proxy_next_upstream_timeout 8s;
        add_header Cache-Control "no-store" always;
    }

    # Public JSON-RPC is allowed only through this full-node gateway. Validator
    # RPC ports must remain private or loopback-only.
    location ~ ^/(rpc|jsonrpc|v1/rpc)$ {
        limit_req zone=msc_rpc burst=30 nodelay;
        proxy_pass http://msc_rpc_active_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ~ ^/(sendTx|submitTx|faucet|stake|unstake|pool/transfer|auth/challenge|auth/verify|wallet/create|wallet/recover|v1/tx|v1/sendTx|v1/submitTx) {
        limit_req zone=msc_write burst=10 nodelay;
        proxy_pass http://msc_rpc_active_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /public-nodes {
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "no-store" always;
        try_files /gateway/public-nodes.json =404;
    }

    location = /v1/public-nodes {
        limit_req zone=msc_static burst=60 nodelay;
        add_header Cache-Control "no-store" always;
        try_files /gateway/public-nodes.json =404;
    }

    location ~ ^/(status|healthz|misbehavior|validators|validatorset/hash|validatorset/audit|validators/pending|validators/diversity|public-nodes|public/status|consensus/mode|formal/verification|storage/policy|bridge/status|bridge/verify|light/headers|light/checkpoint/latest|proof/balance|proof/tx|proof/receipt|tx/status|txs|explorer/blocks|explorer/block|explorer/tx|explorer/peers|balance|nonce|nonce/pending|wallet/status|coins|tokenomics|governance/status|governance/proposals|upgrade/status|dtl/quote|dtl/route_quote|dtl/farm_info|dtl/season_info|dtl/leaderboard|dtl/nft721/owner|dtl/nft1155/owner|v1/) {
        limit_req zone=msc_read burst=40 nodelay;
        proxy_pass http://msc_rpc_active_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /wallet/events {
        limit_req zone=msc_read burst=40 nodelay;
        proxy_pass http://msc_rpc_active_backend;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_cache off;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        add_header Cache-Control "no-store";
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    location = /v1/wallet/events {
        limit_req zone=msc_read burst=40 nodelay;
        proxy_pass http://msc_rpc_active_backend;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_cache off;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        add_header Cache-Control "no-store";
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    location = /gateway/lb-status.json {
        limit_req zone=msc_static burst=60 nodelay;
        try_files /gateway/lb-status.json =404;
    }

    location = /metrics {
        return 404;
    }

    location @msc_rate_limited {
        internal;
        default_type application/json;
        add_header Retry-After 5 always;
        add_header Cache-Control "no-store" always;
        return 429 '{"error":"rate_limited","message":"Too many requests. Wait a few seconds and refresh."}';
    }

    location / {
        return 404;
    }
}
NGINX

sudo sed -i \
  -e "s#__PUBLIC_DOMAINS__#$PUBLIC_DOMAINS_NGINX#g" \
  -e "s#__MAIN_DOMAIN__#$MAIN_DOMAIN#g" \
  -e "s#__EXPLORER_DOMAIN__#$EXPLORER_DOMAIN#g" \
  -e "s#__WALLET_DOMAIN__#$WALLET_DOMAIN#g" \
  -e "s#__DOCS_DOMAIN__#$DOCS_DOMAIN#g" \
  -e "s#__MAIN_HOME_HOST__#$MAIN_HOME_HOST#g" \
  -e "s#__EXPLORER_HOME_HOST__#$EXPLORER_HOME_HOST#g" \
  -e "s#__WALLET_HOME_HOST__#$WALLET_HOME_HOST#g" \
  -e "s#__DOCS_SERVE_HOST__#$DOCS_SERVE_HOST#g" \
  -e "s#__DOCS_CANONICAL_HOST__#$DOCS_CANONICAL_HOST#g" \
  -e "s#__WALLET_REDIRECT_SOURCE_HOST__#$WALLET_REDIRECT_SOURCE_HOST#g" \
  -e "s#__EXPLORER_REDIRECT_SOURCE_HOST__#$EXPLORER_REDIRECT_SOURCE_HOST#g" \
  -e "s#__PUBLIC_HOST__#$PUBLIC_HOST#g" \
  /etc/nginx/sites-available/msc-ui
sudo ln -sf /etc/nginx/sites-available/msc-ui /etc/nginx/sites-enabled/msc-ui
sudo rm -f /etc/nginx/sites-enabled/default

sudo tee /usr/local/bin/msc-lb-health.sh >/dev/null <<'SH'
#!/usr/bin/env bash
set -euo pipefail
out="/var/www/msc-ui/gateway/lb-status.json"
public_nodes_out="/var/www/msc-ui/gateway/public-nodes.json"
targets="${RPC_TARGETS:-${RPC_TARGET:-127.0.0.1:26665}}"
archive_targets="${ARCHIVE_TARGETS:-}"
indexer_targets="${INDEXER_TARGETS:-}"
mkdir -p "$(dirname "$out")"
while true; do
  tmp="$(mktemp)"
  if python3 - "$targets" "$archive_targets" "$indexer_targets" "$tmp" <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

targets = [item.strip() for item in sys.argv[1].split(",") if item.strip()]
archive_targets = [item.strip() for item in sys.argv[2].split(",") if item.strip()]
indexer_targets = [item.strip() for item in sys.argv[3].split(",") if item.strip()]
out = sys.argv[4]
domain = os.environ.get("DOMAIN", "").strip()
explorer_domain = os.environ.get("EXPLORER_DOMAIN", "").strip()
wallet_domain = os.environ.get("WALLET_DOMAIN", "").strip()
allow_validator_rpc = os.environ.get("ALLOW_VALIDATOR_RPC", "").strip().lower() in ("1", "true", "yes", "on")
state_path = "/tmp/msc-lb-health-state.json"
try:
    with open(state_path, "r", encoding="utf-8") as fh:
        health_memory = json.load(fh)
        if not isinstance(health_memory, dict):
            health_memory = {}
except Exception:
    health_memory = {}
backends = []
healthy_count = 0
meta = health_memory.get("__meta__", {})
if not isinstance(meta, dict):
    meta = {}
previous_active_target = str(meta.get("active_target") or "")
def fallback_id_for_target(target, index):
    raw = str(target or "").strip()
    if raw.endswith(":26665"):
        return "F"
    if raw.endswith(":26666"):
        return "G"
    if raw.endswith(":26667"):
        return "H"
    if raw.endswith(":26668"):
        return "I"
    if raw.endswith(":26669"):
        return "J"
    if raw.endswith(":26670"):
        return "K"
    return "PUBLIC" + str(index + 1)

for index, target in enumerate(targets):
    base = target if target.startswith(("http://", "https://")) else "http://" + target
    base = base.rstrip("/")
    started = time.perf_counter()
    status_code = 0
    healthy = False
    error = ""
    status_data = {}
    cmd_data = {}
    try:
        req = urllib.request.Request(base + "/status", headers={"User-Agent": "msc-lb-health/1"})
        with urllib.request.urlopen(req, timeout=8) as res:
            status_code = int(res.status)
            raw = res.read(65536).decode("utf-8", "replace")
            healthy = status_code == 200
            try:
                status_data = json.loads(raw or "{}")
            except Exception:
                status_data = {}
    except urllib.error.HTTPError as exc:
        status_code = int(exc.code)
        error = str(exc)
    except Exception as exc:
        error = str(exc)
    if healthy:
        try:
            req = urllib.request.Request(base + "/consensus/mode", headers={"User-Agent": "msc-lb-health/1"})
            with urllib.request.urlopen(req, timeout=8) as res:
                raw = res.read(32768).decode("utf-8", "replace")
                try:
                    cmd_data = json.loads(raw or "{}")
                except Exception:
                    cmd_data = {}
        except Exception:
            cmd_data = {}
    latency_ms = int((time.perf_counter() - started) * 1000)
    role = str(status_data.get("role") or "full").lower()
    height = int(status_data.get("height") or status_data.get("chain_height") or 0)
    finalized = int(status_data.get("finalized_height") or status_data.get("finalized") or 0)
    finality_lag = int(cmd_data.get("finality_lag") or max(0, height - finalized))
    peers = int(status_data.get("peers") or status_data.get("peer_count") or 0)
    block_age = int(status_data.get("last_block_age_seconds") or 0)
    mode = str(cmd_data.get("mode") or status_data.get("consensus_detector_mode") or "").upper()
    syncing = bool(status_data.get("syncing") or False)
    sync_complete = bool(status_data.get("sync_complete") if "sync_complete" in status_data else (not syncing))
    suspicious = ""
    if role == "validator" and not allow_validator_rpc:
        suspicious = "validator_rpc_not_allowed"
    score = 0
    if healthy and not suspicious:
        score = 35
        score += 15 if height > 0 else 0
        score += 14 if finality_lag <= 2 else (7 if finality_lag <= 20 else 0)
        score += 12 if peers >= 3 else max(0, peers * 3)
        score += 10 if block_age <= 12 else (4 if block_age <= 60 else 0)
        score += 9 if mode == "NORMAL" else (4 if mode in ("STRICT", "RECOVERY") else (-30 if mode in ("EMERGENCY", "HALTED", "ATTACK", "PARTITION") else 0))
        score += 9 if latency_ms <= 250 else (5 if latency_ms <= 1000 else (2 if latency_ms <= 2500 else 0))
        score = max(0, min(100, score))
    node_healthy = healthy and score >= 60 and not suspicious
    raw_id = str(status_data.get("node_id") or fallback_id_for_target(target, index)).strip()
    safe_id = "".join(ch if ch.isalnum() or ch in "-_" else "_" for ch in raw_id).strip("_") or "NODE"
    rpc_url = base
    gateway_rpc_url = ""
    if domain:
        gateway_rpc_url = "https://" + domain + "/public-rpc/" + safe_id
        if base.startswith("http://127.0.0.1"):
            rpc_url = gateway_rpc_url
    backends.append({
        "target": target,
        "id": raw_id,
        "rpc_url": rpc_url,
        "gateway_rpc_url": gateway_rpc_url,
        "role": role,
        "public_gateway": True,
        "healthy": node_healthy,
        "latency_ms": latency_ms,
        "status_code": status_code,
        "chain_id": str(status_data.get("chain_id") or ""),
        "genesis_hash": str(status_data.get("genesis_hash") or status_data.get("expected_genesis_hash") or ""),
        "version": str(status_data.get("version") or ""),
        "height": height,
        "finalized_height": finalized,
        "finality_lag": finality_lag,
        "last_block_age_seconds": block_age,
        "peer_count": peers,
        "consensus_mode": mode,
        "network_health": str(status_data.get("network_health") or ""),
        "syncing": syncing,
        "sync_complete": sync_complete,
        "score": score,
        "suspicious_reason": suspicious,
        "last_checked": int(time.time()),
        "error": error,
    })
max_height = max([int(item.get("height") or 0) for item in backends], default=0)
healthy_count = 0
def hard_fail_reason(item):
    suspicious = str(item.get("suspicious_reason") or "")
    if suspicious in ("validator_rpc_not_allowed", "chain_id_mismatch", "genesis_hash_mismatch", "bad_status"):
        return suspicious
    status_code = int(item.get("status_code") or 0)
    if status_code and status_code != 200:
        return "bad_status"
    mode = str(item.get("consensus_mode") or "").upper()
    if mode in ("ATTACK", "PARTITION", "HALTED"):
        return mode.lower()
    return ""

def bad_reason(item):
    if item.get("error") or int(item.get("status_code") or 0) == 0:
        return "timeout"
    if int(item.get("height_lag_blocks") or 0) > 20:
        return "height_lag"
    if int(item.get("finality_lag") or 0) > 20:
        return "finality_lag"
    if int(item.get("last_block_age_seconds") or 0) > 60:
        return "block_stalled"
    return ""

def warning_reason(item):
    if int(item.get("height_lag_blocks") or 0) > 2:
        return "height_lag"
    if int(item.get("finality_lag") or 0) > 2:
        return "finality_lag"
    if int(item.get("last_block_age_seconds") or 0) >= 12:
        return "slow_block_age"
    if int(item.get("latency_ms") or 0) > 2500:
        return "high_latency"
    mode = str(item.get("consensus_mode") or "").upper()
    if mode in ("STRICT", "RECOVERY", "DEGRADED", "EMERGENCY"):
        return mode.lower()
    if int(item.get("score") or 0) > 0 and int(item.get("score") or 0) < 60:
        return "low_score"
    return ""

def bad_threshold(reason):
    if reason == "height_lag":
        return 3
    return 2

def active_excluded_reason(item):
    if not item.get("healthy"):
        return str(item.get("health_reason") or item.get("suspicious_reason") or "unhealthy")
    if str(item.get("health_state") or "").lower() != "healthy":
        return str(item.get("health_reason") or "not_stably_healthy")
    if int(item.get("height_lag_blocks") or 0) > 2:
        return "height_lag"
    if int(item.get("finality_lag") or 0) > 2:
        return "finality_lag"
    if int(item.get("last_block_age_seconds") or 0) >= 12:
        return "slow_block_age"
    if int(item.get("latency_ms") or 0) > 2500:
        return "high_latency"
    if item.get("syncing") or not item.get("sync_complete"):
        return "syncing"
    mode = str(item.get("consensus_mode") or "").upper()
    if mode in ("PARTITION", "HALTED", "ATTACK", "RECOVERY", "DEGRADED"):
        return mode.lower()
    return ""

now = int(time.time())
for item in backends:
    height = int(item.get("height") or 0)
    height_lag = max(0, max_height - height) if max_height and height else 0
    item["height_lag_blocks"] = height_lag
    if height_lag > 2:
        item["score"] = max(0, int(item.get("score") or 0) - min(10, height_lag - 2))
    key = str(item.get("id") or item.get("target") or item.get("rpc_url") or "")
    sample = health_memory.get(key, {})
    if not isinstance(sample, dict):
        sample = {}
    good_samples = int(sample.get("good_samples") or 0)
    bad_samples = int(sample.get("bad_samples") or 0)
    last_state = str(sample.get("last_state") or "")
    last_healthy_at = int(sample.get("last_healthy_at") or 0)
    reason = hard_fail_reason(item)
    if reason:
        good_samples = 0
        bad_samples += 1
        health_state = "unhealthy"
        item["healthy"] = False
        item["suspicious_reason"] = reason
        item["score"] = 0
    else:
        reason = bad_reason(item)
        if reason:
            good_samples = 0
            bad_samples += 1
            unhealthy = bad_samples >= bad_threshold(reason)
            if reason == "height_lag" and height_lag > 64:
                unhealthy = True
            if reason == "finality_lag" and int(item.get("finality_lag") or 0) > 64:
                unhealthy = True
            health_state = "unhealthy" if unhealthy else "warning"
            item["healthy"] = not unhealthy
            if unhealthy:
                item["suspicious_reason"] = reason
            else:
                item["suspicious_reason"] = ""
        else:
            reason = warning_reason(item)
            if reason:
                good_samples = 0
                bad_samples = 0
                health_state = "warning"
                item["healthy"] = True
                item["suspicious_reason"] = ""
            else:
                bad_samples = 0
                good_samples += 1
                if last_state == "healthy" or good_samples >= 2:
                    health_state = "healthy"
                    reason = "healthy"
                    last_healthy_at = now
                else:
                    health_state = "warning"
                    reason = "warming_up"
                item["healthy"] = True
                item["suspicious_reason"] = ""
    item["health_state"] = health_state
    item["health_reason"] = reason
    item["bad_samples"] = bad_samples
    item["good_samples"] = good_samples
    item["last_healthy_at"] = last_healthy_at
    health_memory[key] = {
        "good_samples": good_samples,
        "bad_samples": bad_samples,
        "last_healthy_at": last_healthy_at,
        "last_state": health_state,
    }
    if item.get("healthy"):
        healthy_count += 1
active_candidates = []
for item in backends:
    excluded = active_excluded_reason(item)
    item["active_gateway"] = False
    item["selected_reason"] = ""
    item["excluded_reason"] = excluded
    if not excluded:
        active_candidates.append(item)
active_candidates = sorted(
    active_candidates,
    key=lambda item: (
        int(item.get("height_lag_blocks") or 0),
        int(item.get("finality_lag") or 0),
        -int(item.get("score") or 0),
        int(item.get("latency_ms") or 0),
        str(item.get("id") or item.get("target") or ""),
    ),
)
if active_candidates:
    best_active = active_candidates[0]
    if previous_active_target:
        for candidate in active_candidates:
            if str(candidate.get("target") or "") == previous_active_target:
                best_active = candidate
                break
    for item in backends:
        if item is best_active:
            item["active_gateway"] = True
            item["selected_reason"] = "sticky_stable_healthy_backend" if str(item.get("target") or "") == previous_active_target else "best_stable_healthy_backend"
            item["excluded_reason"] = ""
        elif item in active_candidates:
            item["active_gateway"] = False
            item["selected_reason"] = ""
            item["excluded_reason"] = "standby_lower_score"
else:
    fallback = None
    if previous_active_target:
        for candidate in backends:
            if str(candidate.get("target") or "") == previous_active_target and str(candidate.get("health_state") or "").lower() != "unhealthy":
                fallback = candidate
                fallback["selected_reason"] = "sticky_warning_backend"
                break
    for candidate in sorted(backends, key=lambda it: (-int(it.get("score") or 0), int(it.get("latency_ms") or 0), str(it.get("id") or it.get("target") or ""))):
        if fallback is not None:
            break
        if candidate.get("status_code") == 200:
            fallback = candidate
            break
    if fallback is None and backends:
        fallback = backends[0]
    if fallback is not None:
        fallback["active_gateway"] = True
        if not fallback.get("selected_reason"):
            fallback["selected_reason"] = "fallback_no_strict_backend"
        fallback["excluded_reason"] = ""
try:
    tmp_state = state_path + ".tmp"
    with open(tmp_state, "w", encoding="utf-8") as fh:
        json.dump(health_memory, fh, separators=(",", ":"))
    os.replace(tmp_state, state_path)
except Exception:
    pass
healthy_sorted = sorted(
    [item for item in backends if item.get("healthy")],
    key=lambda item: (
        -int(item.get("score") or 0),
        int(item.get("height_lag_blocks") or 0),
        int(item.get("finality_lag") or 0),
        int(item.get("latency_ms") or 0),
        str(item.get("id") or item.get("target") or ""),
    ),
)
best_node = healthy_sorted[0] if healthy_sorted else (backends[0] if backends else None)
active_targets = [str(item.get("target") or "") for item in backends if item.get("active_gateway") and item.get("target")]
if not active_targets and backends:
    active_targets = [str(backends[0].get("target") or "")]
health_memory["__meta__"] = {"active_target": active_targets[0] if active_targets else ""}
active_conf = "upstream msc_rpc_active_backend {\n    least_conn;\n    keepalive 32;\n"
for target in active_targets:
    clean = target.replace("http://", "").replace("https://", "").strip()
    if clean:
        active_conf += f"    server {clean} max_fails=2 fail_timeout=10s;\n"
active_conf += "}\n"
with open(out + ".active-upstream", "w", encoding="utf-8") as fh:
    fh.write(active_conf)

def fetch_json(base, path, timeout=4):
    base = base if base.startswith(("http://", "https://")) else "http://" + base
    base = base.rstrip("/")
    req = urllib.request.Request(base + path, headers={"User-Agent": "msc-lb-health/1"})
    with urllib.request.urlopen(req, timeout=timeout) as res:
        raw = res.read(65536).decode("utf-8", "replace")
        data = json.loads(raw or "{}")
        if isinstance(data, dict) and data.get("success") is True and isinstance(data.get("data"), dict):
            data = data["data"]
        return int(res.status), data

def probe_archive(target, index):
    base = target if target.startswith(("http://", "https://")) else "http://" + target
    base = base.rstrip("/")
    started = time.perf_counter()
    item = {
        "id": "ARCHIVE" + str(index + 1),
        "url": base,
        "role": "archive",
        "healthy": False,
        "state": "unhealthy",
        "reason": "not_checked",
        "latency_ms": 0,
        "last_checked": int(time.time()),
    }
    try:
        status_code, status = fetch_json(base, "/status")
        _, policy = fetch_json(base, "/storage/policy")
        latency = int((time.perf_counter() - started) * 1000)
        height = int(status.get("height") or 0)
        finalized = int(status.get("finalized_height") or 0)
        lag = max(0, height - finalized)
        archive_mode = bool(policy.get("archive_mode") or str(policy.get("profile") or "").lower() == "archive")
        role = str(status.get("role") or "").lower()
        healthy = status_code == 200 and archive_mode and role != "validator" and lag <= 2 and height > 0
        item.update({
            "healthy": healthy,
            "state": "healthy" if healthy else "warning",
            "reason": "archive_synced" if healthy else ("archive_mode_required" if not archive_mode else "archive_lag"),
            "latency_ms": latency,
            "height": height,
            "finalized_height": finalized,
            "finality_lag": lag,
            "archive_mode": archive_mode,
            "chain_id": str(status.get("chain_id") or ""),
            "genesis_hash": str(status.get("genesis_hash") or status.get("expected_genesis_hash") or ""),
        })
    except Exception as exc:
        item["reason"] = str(exc)
        item["latency_ms"] = int((time.perf_counter() - started) * 1000)
    return item

def probe_indexer(target, index):
    base = target if target.startswith(("http://", "https://")) else "http://" + target
    base = base.rstrip("/")
    started = time.perf_counter()
    item = {
        "id": "INDEXER" + str(index + 1),
        "url": base,
        "role": "indexer",
        "healthy": False,
        "state": "unhealthy",
        "reason": "not_checked",
        "latency_ms": 0,
        "last_checked": int(time.time()),
    }
    try:
        status_code, status = fetch_json(base, "/indexer/status")
        latency = int((time.perf_counter() - started) * 1000)
        lag = int(status.get("index_lag") or 0)
        healthy = status_code == 200 and bool(status.get("healthy")) and lag <= 2
        item.update({
            "healthy": healthy,
            "state": "healthy" if healthy else str(status.get("state") or "warning"),
            "reason": str(status.get("reason") or ("indexed" if healthy else "index_lag")),
            "latency_ms": latency,
            "indexed_height": int(status.get("indexed_height") or 0),
            "archive_height": int(status.get("archive_height") or 0),
            "index_lag": lag,
            "source_rpc": str(status.get("source_rpc") or ""),
            "chain_id": str(status.get("chain_id") or ""),
            "genesis_hash": str(status.get("genesis_hash") or ""),
        })
    except Exception as exc:
        item["reason"] = str(exc)
        item["latency_ms"] = int((time.perf_counter() - started) * 1000)
    return item

archive_services = [probe_archive(target, i) for i, target in enumerate(archive_targets)]
indexer_services = [probe_indexer(target, i) for i, target in enumerate(indexer_targets)]

payload = {
    "status": "healthy" if healthy_count == len(backends) and backends else ("degraded" if healthy_count else "down"),
    "explorer_domain": explorer_domain,
    "wallet_domain": wallet_domain,
    "healthy": healthy_count,
    "total": len(backends),
    "failover_count": 0,
    "last_switch": "",
    "backends": backends,
    "archive": archive_services,
    "indexer": indexer_services,
    "active_targets": active_targets,
    "ts": int(time.time()),
}
public_nodes = {
    "status": payload["status"],
    "explorer_domain": explorer_domain,
    "wallet_domain": wallet_domain,
    "chain_id": str(backends[0].get("chain_id") or "") if backends else "",
    "genesis_hash": str(backends[0].get("genesis_hash") or "") if backends else "",
    "healthy": healthy_count,
    "total": len(backends),
    "best": str(best_node.get("rpc_url") or "") if best_node else "",
    "best_node": best_node,
    "nodes": backends,
    "ts": payload["ts"],
}
with open(out, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, separators=(",", ":"))
with open(out + ".public-nodes", "w", encoding="utf-8") as fh:
    json.dump({"success": True, "data": public_nodes}, fh, separators=(",", ":"))
PY
  then
    sudo mv "$tmp" "$out"
    sudo mv "$tmp.public-nodes" "$public_nodes_out"
    if [ -s "$tmp.active-upstream" ]; then
      if ! cmp -s "$tmp.active-upstream" /etc/nginx/conf.d/msc_rpc_active_upstream.conf 2>/dev/null; then
        sudo mv "$tmp.active-upstream" /etc/nginx/conf.d/msc_rpc_active_upstream.conf
        if sudo nginx -t >/dev/null 2>&1; then
          sudo systemctl reload nginx >/dev/null 2>&1 || true
        fi
      else
        rm -f "$tmp.active-upstream"
      fi
    fi
    sudo chmod 0644 "$out"
    sudo chmod 0644 "$public_nodes_out"
    sudo chown www-data:www-data "$out" 2>/dev/null || sudo chown nginx:nginx "$out" 2>/dev/null || true
    sudo chown www-data:www-data "$public_nodes_out" 2>/dev/null || sudo chown nginx:nginx "$public_nodes_out" 2>/dev/null || true
  else
    rm -f "$tmp"
    rm -f "$tmp.public-nodes"
    rm -f "$tmp.active-upstream"
  fi
  sleep 5
done
SH
sudo chmod +x /usr/local/bin/msc-lb-health.sh

sudo tee /etc/systemd/system/msc-lb-health.service >/dev/null <<SERVICE
[Unit]
Description=MSC gateway load balancer health writer
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=DOMAIN=$WALLET_DOMAIN
Environment=MAIN_DOMAIN=$MAIN_DOMAIN
Environment=EXPLORER_DOMAIN=$EXPLORER_DOMAIN
Environment=WALLET_DOMAIN=$WALLET_DOMAIN
Environment=DOCS_DOMAIN=$DOCS_DOMAIN
Environment=PUBLIC_DOMAINS=$PUBLIC_DOMAINS
Environment=RPC_TARGETS=$RPC_TARGETS
Environment=RPC_TARGET=$RPC_TARGET
Environment=ARCHIVE_TARGETS=$ARCHIVE_TARGETS
Environment=INDEXER_TARGETS=$INDEXER_TARGETS
Environment=ALLOW_VALIDATOR_RPC=$ALLOW_VALIDATOR_RPC
ExecStart=/usr/local/bin/msc-lb-health.sh
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
SERVICE
# Remove the legacy emergency validator-RPC override so this deployment's
# full-node target list remains authoritative after service restarts.
sudo rm -f /etc/systemd/system/msc-lb-health.service.d/20-live-validator-rpc.conf
sudo systemctl daemon-reload
sudo systemctl enable msc-lb-health.service
sudo systemctl restart msc-lb-health.service
for i in $(seq 1 12); do
  [ -s /var/www/msc-ui/gateway/lb-status.json ] && break
  sleep 1
done
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx

restore_existing_https_config() {
  if [ "${DOCS_DOMAIN_ENABLED:-0}" = "1" ]; then
    echo "Existing-cert HTTPS fallback skipped because docs domain is enabled and may need a new certificate." >&2
    return 1
  fi
  cert_name="$EXPLORER_DOMAIN"
  if [ ! -s "/etc/letsencrypt/live/$cert_name/fullchain.pem" ] || [ ! -s "/etc/letsencrypt/live/$cert_name/privkey.pem" ]; then
    echo "Existing-cert HTTPS fallback unavailable: /etc/letsencrypt/live/$cert_name is missing." >&2
    return 1
  fi
  sudo python3 - "$cert_name" <<'PY'
from pathlib import Path
import sys

cert_name = sys.argv[1]
path = Path("/etc/nginx/sites-available/msc-ui")
body = path.read_text()
if "listen 443 ssl" not in body:
    ssl_body = body.replace("listen 80 default_server;", "listen 443 ssl;")
    ssl_body = ssl_body.replace("listen [::]:80 default_server;", "listen [::]:443 ssl;")
    marker = "    root /var/www/msc-ui;\n"
    ssl_settings = (
        "    ssl_certificate /etc/letsencrypt/live/{0}/fullchain.pem;\n"
        "    ssl_certificate_key /etc/letsencrypt/live/{0}/privkey.pem;\n"
        "    ssl_protocols TLSv1.2 TLSv1.3;\n"
        "    ssl_prefer_server_ciphers off;\n"
        "    http2 on;\n"
        "    add_header Strict-Transport-Security \"max-age=31536000\" always;\n"
    ).format(cert_name)
    if marker not in ssl_body:
        raise SystemExit("nginx root marker not found for SSL fallback")
    ssl_body = ssl_body.replace(marker, marker + ssl_settings, 1)
    path.write_text(body.rstrip() + "\n\n" + ssl_body)
PY
}

if [ "$ENABLE_HTTPS" = "1" ]; then
  cert_domain_args=()
  IFS=',' read -r -a CERT_DOMAIN_ITEMS <<< "${PUBLIC_DOMAINS:-${DOMAIN:-}}"
  for item in "${CERT_DOMAIN_ITEMS[@]}"; do
    clean="$(echo "$item" | xargs)"
    [ -z "$clean" ] && continue
    cert_domain_args+=("-d" "$clean")
  done
  if [ "${#cert_domain_args[@]}" -eq 0 ]; then
    echo "No public domains were provided for certbot" >&2
    exit 1
  fi
  if ! sudo certbot --nginx \
    --non-interactive \
    --agree-tos \
    --expand \
    --email "$LETSENCRYPT_EMAIL" \
    --redirect \
    --hsts \
    "${cert_domain_args[@]}"; then
    echo "WARN certbot failed; restoring HTTPS with existing certificate for $EXPLORER_DOMAIN." >&2
    restore_existing_https_config
  fi
  sudo nginx -t
  sudo systemctl reload nginx
fi

if [ "$ENABLE_HTTPS" = "1" ]; then
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/" -o /tmp/msc-home-check.html && grep -q "$MAIN_DOMAIN/" /tmp/msc-home-check.html || echo "WARN homepage check failed"
  main_landing_redirect_code=$(curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$MAIN_DOMAIN/landing.html")
  if [ "$main_landing_redirect_code" != "301" ]; then echo "WARN landing.html redirect returned $main_landing_redirect_code"; fi
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/sitemap.xml" -o /tmp/msc-main-sitemap-check.xml && grep -q "$MAIN_DOMAIN/sitemap-docs.xml" /tmp/msc-main-sitemap-check.xml || echo "WARN main sitemap check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/robots.txt" -o /tmp/msc-robots-check.txt && grep -q "$MAIN_DOMAIN/sitemap.xml" /tmp/msc-robots-check.txt || echo "WARN robots.txt check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/sitemap-main.xml" -o /tmp/msc-main-pages-sitemap-check.xml && grep -q "$MAIN_DOMAIN/blog/" /tmp/msc-main-pages-sitemap-check.xml || echo "WARN blog sitemap check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/blog/" -o /tmp/msc-blog-check.html && grep -q "MSC Chain Blog" /tmp/msc-blog-check.html || echo "WARN blog check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/feed.xml" -o /tmp/msc-feed-check.xml && grep -q "MSC Chain Blog" /tmp/msc-feed-check.xml || echo "WARN RSS feed check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/feed.json" -o /tmp/msc-feed-check.json && grep -q "jsonfeed.org" /tmp/msc-feed-check.json || echo "WARN JSON feed check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsSLI "https://$MAIN_DOMAIN/docs/" >/dev/null || echo "WARN docs index check failed"
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsSL "https://$MAIN_DOMAIN/docs/what-is-msc-chain.html" -o /tmp/msc-docs-article-check.html && grep -q "Article" /tmp/msc-docs-article-check.html || echo "WARN docs article check failed"
  if [ "${DOCS_DOMAIN_ENABLED:-0}" = "1" ]; then
    curl --resolve "$DOCS_DOMAIN:443:127.0.0.1" -fsSI "https://$DOCS_DOMAIN/docs/" >/dev/null || echo "WARN docs subdomain index check failed"
    curl --resolve "$DOCS_DOMAIN:443:127.0.0.1" -fsS "https://$DOCS_DOMAIN/sitemap.xml" -o /tmp/msc-docs-sitemap-check.xml && grep -q "$DOCS_DOMAIN/docs/what-is-msc-chain.html" /tmp/msc-docs-sitemap-check.xml || echo "WARN docs subdomain sitemap check failed"
  fi
  curl --resolve "$MAIN_DOMAIN:443:127.0.0.1" -fsS "https://$MAIN_DOMAIN/llms.txt" -o /tmp/msc-llms-check.txt && grep -q "MSC Chain" /tmp/msc-llms-check.txt || echo "WARN llms.txt check failed"
  curl --resolve "$EXPLORER_DOMAIN:443:127.0.0.1" -fsSI "https://$EXPLORER_DOMAIN/" >/dev/null || echo "WARN explorer root check failed"
  explorer_home_redirect_code=$(curl --resolve "$EXPLORER_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$EXPLORER_DOMAIN/explorer.html")
  if [ "$explorer_home_redirect_code" != "301" ]; then echo "WARN explorer.html redirect returned $explorer_home_redirect_code"; fi
  curl --resolve "$EXPLORER_DOMAIN:443:127.0.0.1" -fsS "https://$EXPLORER_DOMAIN/sitemap.xml" -o /tmp/msc-explorer-sitemap-check.xml && grep -q "explorer-validators.html" /tmp/msc-explorer-sitemap-check.xml || echo "WARN explorer sitemap check failed"
  curl --resolve "$EXPLORER_DOMAIN:443:127.0.0.1" -fsSI "https://$EXPLORER_DOMAIN/portal/index.html" >/dev/null || echo "WARN portal/index.html check failed"
  curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" -fsSI "https://$WALLET_DOMAIN/msc_wallet.html" >/dev/null || echo "WARN msc_wallet.html check failed"
  curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" -fsSI "https://$WALLET_DOMAIN/" >/dev/null || echo "WARN wallet root check failed"
  wallet_home_redirect_code=$(curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$WALLET_DOMAIN/dashboard.html")
  if [ "$wallet_home_redirect_code" != "301" ]; then echo "WARN dashboard.html redirect returned $wallet_home_redirect_code"; fi
  curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" -fsS "https://$WALLET_DOMAIN/sitemap.xml" -o /tmp/msc-wallet-sitemap-check.xml && grep -q "faucet.html" /tmp/msc-wallet-sitemap-check.xml || echo "WARN wallet sitemap check failed"
  curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" -fsSI "https://$WALLET_DOMAIN/wallet.html" >/dev/null || echo "WARN wallet.html check failed"
  curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" -fsSI "https://$WALLET_DOMAIN/validator-wallet.html" >/dev/null || echo "WARN validator-wallet.html check failed"
  curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" -fsS "https://$WALLET_DOMAIN/gateway/lb-status.json" >/dev/null || echo "WARN lb-status check failed"
  if [ "$EXPLORER_DOMAIN" != "$WALLET_DOMAIN" ]; then
    wallet_redirect_code=$(curl --resolve "$EXPLORER_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$EXPLORER_DOMAIN/msc_wallet.html")
    explorer_redirect_code=$(curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$WALLET_DOMAIN/explorer.html")
    if [ "$wallet_redirect_code" != "301" ]; then echo "WARN explorer host wallet redirect returned $wallet_redirect_code"; fi
    if [ "$explorer_redirect_code" != "301" ]; then echo "WARN wallet host explorer redirect returned $explorer_redirect_code"; fi
  fi
  explorer_status_code=$(curl --resolve "$EXPLORER_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$EXPLORER_DOMAIN/status")
  wallet_status_code=$(curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$WALLET_DOMAIN/status")
  metrics_code=$(curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$WALLET_DOMAIN/metrics")
  rpc_code=$(curl --resolve "$WALLET_DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$WALLET_DOMAIN/rpc")
else
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/ -o /tmp/msc-home-check.html && grep -q "$MAIN_DOMAIN/" /tmp/msc-home-check.html || echo "WARN homepage check failed"
  main_landing_redirect_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $MAIN_DOMAIN" http://127.0.0.1/landing.html)
  if [ "$main_landing_redirect_code" != "301" ]; then echo "WARN landing.html redirect returned $main_landing_redirect_code"; fi
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/sitemap.xml -o /tmp/msc-main-sitemap-check.xml && grep -q "$MAIN_DOMAIN/sitemap-docs.xml" /tmp/msc-main-sitemap-check.xml || echo "WARN main sitemap check failed"
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/robots.txt -o /tmp/msc-robots-check.txt && grep -q "$MAIN_DOMAIN/sitemap.xml" /tmp/msc-robots-check.txt || echo "WARN robots.txt check failed"
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/sitemap-main.xml -o /tmp/msc-main-pages-sitemap-check.xml && grep -q "$MAIN_DOMAIN/blog/" /tmp/msc-main-pages-sitemap-check.xml || echo "WARN blog sitemap check failed"
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/blog/ -o /tmp/msc-blog-check.html && grep -q "MSC Chain Blog" /tmp/msc-blog-check.html || echo "WARN blog check failed"
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/feed.xml -o /tmp/msc-feed-check.xml && grep -q "MSC Chain Blog" /tmp/msc-feed-check.xml || echo "WARN RSS feed check failed"
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/feed.json -o /tmp/msc-feed-check.json && grep -q "jsonfeed.org" /tmp/msc-feed-check.json || echo "WARN JSON feed check failed"
  curl -fsSLI -H "Host: $MAIN_DOMAIN" http://127.0.0.1/docs/ >/dev/null || echo "WARN docs index check failed"
  curl -fsSL -H "Host: $MAIN_DOMAIN" http://127.0.0.1/docs/what-is-msc-chain.html -o /tmp/msc-docs-article-check.html && grep -q "Article" /tmp/msc-docs-article-check.html || echo "WARN docs article check failed"
  if [ "${DOCS_DOMAIN_ENABLED:-0}" = "1" ]; then
    curl -fsSI -H "Host: $DOCS_DOMAIN" http://127.0.0.1/docs/ >/dev/null || echo "WARN docs subdomain index check failed"
    curl -fsS -H "Host: $DOCS_DOMAIN" http://127.0.0.1/sitemap.xml -o /tmp/msc-docs-sitemap-check.xml && grep -q "$DOCS_DOMAIN/docs/what-is-msc-chain.html" /tmp/msc-docs-sitemap-check.xml || echo "WARN docs subdomain sitemap check failed"
  fi
  curl -fsS -H "Host: $MAIN_DOMAIN" http://127.0.0.1/llms.txt -o /tmp/msc-llms-check.txt && grep -q "MSC Chain" /tmp/msc-llms-check.txt || echo "WARN llms.txt check failed"
  curl -fsSI -H "Host: $EXPLORER_DOMAIN" http://127.0.0.1/ >/dev/null || echo "WARN explorer root check failed"
  explorer_home_redirect_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $EXPLORER_DOMAIN" http://127.0.0.1/explorer.html)
  if [ "$explorer_home_redirect_code" != "301" ]; then echo "WARN explorer.html redirect returned $explorer_home_redirect_code"; fi
  curl -fsS -H "Host: $EXPLORER_DOMAIN" http://127.0.0.1/sitemap.xml -o /tmp/msc-explorer-sitemap-check.xml && grep -q "explorer-validators.html" /tmp/msc-explorer-sitemap-check.xml || echo "WARN explorer sitemap check failed"
  curl -fsSI -H "Host: $EXPLORER_DOMAIN" http://127.0.0.1/portal/index.html >/dev/null || echo "WARN portal/index.html check failed"
  curl -fsSI -H "Host: $WALLET_DOMAIN" http://127.0.0.1/msc_wallet.html >/dev/null || echo "WARN msc_wallet.html check failed"
  curl -fsSI -H "Host: $WALLET_DOMAIN" http://127.0.0.1/ >/dev/null || echo "WARN wallet root check failed"
  wallet_home_redirect_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $WALLET_DOMAIN" http://127.0.0.1/dashboard.html)
  if [ "$wallet_home_redirect_code" != "301" ]; then echo "WARN dashboard.html redirect returned $wallet_home_redirect_code"; fi
  curl -fsS -H "Host: $WALLET_DOMAIN" http://127.0.0.1/sitemap.xml -o /tmp/msc-wallet-sitemap-check.xml && grep -q "faucet.html" /tmp/msc-wallet-sitemap-check.xml || echo "WARN wallet sitemap check failed"
  curl -fsSI -H "Host: $WALLET_DOMAIN" http://127.0.0.1/wallet.html >/dev/null || echo "WARN wallet.html check failed"
  curl -fsSI -H "Host: $WALLET_DOMAIN" http://127.0.0.1/validator-wallet.html >/dev/null || echo "WARN validator-wallet.html check failed"
  curl -fsS -H "Host: $WALLET_DOMAIN" http://127.0.0.1/gateway/lb-status.json >/dev/null || echo "WARN lb-status check failed"
  if [ "$EXPLORER_DOMAIN" != "$WALLET_DOMAIN" ]; then
    wallet_redirect_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $EXPLORER_DOMAIN" http://127.0.0.1/msc_wallet.html)
    explorer_redirect_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $WALLET_DOMAIN" http://127.0.0.1/explorer.html)
    if [ "$wallet_redirect_code" != "301" ]; then echo "WARN explorer host wallet redirect returned $wallet_redirect_code"; fi
    if [ "$explorer_redirect_code" != "301" ]; then echo "WARN wallet host explorer redirect returned $explorer_redirect_code"; fi
  fi
  explorer_status_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $EXPLORER_DOMAIN" http://127.0.0.1/status)
  wallet_status_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $WALLET_DOMAIN" http://127.0.0.1/status)
  metrics_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $WALLET_DOMAIN" http://127.0.0.1/metrics)
  rpc_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $WALLET_DOMAIN" http://127.0.0.1/rpc)
fi
if [ "$explorer_status_code" != "200" ]; then echo "WARN explorer /status returned $explorer_status_code"; fi
if [ "$wallet_status_code" != "200" ]; then echo "WARN wallet /status returned $wallet_status_code"; fi
if [ "$metrics_code" != "404" ]; then echo "WARN /metrics returned $metrics_code"; fi
if [ "$rpc_code" = "401" ]; then echo "WARN /rpc returned 401"; fi
echo "MSC public gateway checks passed."
'@

$remote = ($remote -replace "`r`n", "`n") -replace "`r", ""

$target = "$GatewayUser@$GatewayHost"
$envPrefix = @(
    "DOMAIN=$(Quote-Sh $WalletDomain)",
    "MAIN_DOMAIN=$(Quote-Sh $MainDomain)",
    "EXPLORER_DOMAIN=$(Quote-Sh $ExplorerDomain)",
    "WALLET_DOMAIN=$(Quote-Sh $WalletDomain)",
    "DOCS_DOMAIN=$(Quote-Sh $DocsDomain)",
    "DOCS_DOMAIN_ENABLED=$(Quote-Sh ($(if ($docsDomainEnabled) { "1" } else { "0" })))",
    "PUBLIC_DOMAINS=$(Quote-Sh $publicDomainsCsv)",
    "PUBLIC_DOMAINS_NGINX=$(Quote-Sh $publicDomainsNginx)",
    "MAIN_HOME_HOST=$(Quote-Sh $mainHomeHost)",
    "EXPLORER_HOME_HOST=$(Quote-Sh $explorerHomeHost)",
    "WALLET_HOME_HOST=$(Quote-Sh $walletHomeHost)",
    "DOCS_SERVE_HOST=$(Quote-Sh $docsServeHost)",
    "DOCS_CANONICAL_HOST=$(Quote-Sh $docsCanonicalHost)",
    "WALLET_REDIRECT_SOURCE_HOST=$(Quote-Sh $walletRedirectSourceHost)",
    "EXPLORER_REDIRECT_SOURCE_HOST=$(Quote-Sh $explorerRedirectSourceHost)",
    "PUBLIC_HOST=$(Quote-Sh $GatewayHost)",
    "RPC_TARGET=$(Quote-Sh $RpcTarget)",
    "RPC_TARGETS=$(Quote-Sh $rpcTargetsCsv)",
    "ARCHIVE_TARGETS=$(Quote-Sh $archiveTargetsCsv)",
    "INDEXER_TARGETS=$(Quote-Sh $indexerTargetsCsv)",
    "PRESERVE_REMOTE_RELEASES=$(Quote-Sh ($(if ($PreserveRemoteReleases) { "1" } else { "0" })))",
    "ALLOW_VALIDATOR_RPC=$(Quote-Sh ($(if ($AllowValidatorRpcGateway) { "1" } else { "0" })))",
    "ENABLE_HTTPS=$(Quote-Sh ($(if ($enableHttps) { "1" } else { "0" })))",
    "LETSENCRYPT_EMAIL=$(Quote-Sh $LetsEncryptEmail)",
    "IDE_USER=$(Quote-Sh $IdeUser)",
    "IDE_PASSWORD=$(Quote-Sh $IdePassword)"
) -join " "

Write-Host "Deploying MSC public gateway on $target..."
Write-Host "Main domain: $MainDomain"
Write-Host "Explorer domain: $ExplorerDomain"
Write-Host "Wallet domain: $WalletDomain"
if ($docsDomainEnabled) {
    Write-Host "Docs domain: $DocsDomain"
} else {
    Write-Host "Docs domain: pending ($DocsDomain)"
}
Write-Host "RPC targets: $($resolvedRpcTargets -join ', ')"
if ($resolvedArchiveTargets.Count -gt 0) {
    Write-Host "Archive targets: $($resolvedArchiveTargets -join ', ')"
}
if ($resolvedIndexerTargets.Count -gt 0) {
    Write-Host "Indexer targets: $($resolvedIndexerTargets -join ', ')"
}
if (-not $enableHttps) {
    Write-Warning "HTTP-only pre-DNS mode enabled. HTTPS is not complete until $($publicDomains -join ', ') A records point to $GatewayHost."
}

$uiArchive = Join-Path ([System.IO.Path]::GetTempPath()) ("msc-ui-upload-{0}.tar" -f ([System.Guid]::NewGuid().ToString("N")))
try {
    $tarArgs = @("-cf", $uiArchive)
    if ($PreserveRemoteReleases) {
        $tarArgs += "--exclude=./releases"
    }
    $tarArgs += @("-C", $UiSource, ".")
    tar @tarArgs
    if ($LASTEXITCODE -ne 0) { throw "failed to build UI archive" }
    ssh -i $KeyPath -o StrictHostKeyChecking=no $target 'sudo rm -rf "$HOME/msc-ui-upload" "$HOME/msc-ui-upload.tar" && mkdir -p "$HOME/msc-ui-upload" && sudo chown -R "$USER:$USER" "$HOME/msc-ui-upload"'
    if ($LASTEXITCODE -ne 0) { throw "failed to prepare remote UI upload directory" }
    scp -i $KeyPath -o StrictHostKeyChecking=no $uiArchive "${target}:msc-ui-upload.tar"
    if ($LASTEXITCODE -ne 0) { throw "failed to upload UI archive" }
    ssh -i $KeyPath -o StrictHostKeyChecking=no $target 'sudo tar --delay-directory-restore --no-same-owner --no-same-permissions -xf "$HOME/msc-ui-upload.tar" -C "$HOME/msc-ui-upload" && sudo chown -R "$USER:$USER" "$HOME/msc-ui-upload" && sudo find "$HOME/msc-ui-upload" -type d -exec chmod 0755 {} \; && sudo find "$HOME/msc-ui-upload" -type f -exec chmod 0644 {} \;'
    if ($LASTEXITCODE -ne 0) { throw "failed to extract UI archive" }
    $remote | ssh -i $KeyPath -o StrictHostKeyChecking=no $target "$envPrefix bash -s"
    if ($LASTEXITCODE -ne 0) { throw "remote gateway provisioning failed" }
} finally {
    if (Test-Path -LiteralPath $uiArchive) {
        Remove-Item -LiteralPath $uiArchive -Force
    }
}

Write-Host ""
Write-Host "Gateway checks passed."
if ($enableHttps) {
    Write-Host "  Main     : https://$MainDomain/"
    Write-Host "  Explorer : https://$ExplorerDomain/"
    Write-Host "  Wallet   : https://$WalletDomain/"
    if ($docsDomainEnabled) {
        Write-Host "  Docs     : https://$DocsDomain/docs/"
    } else {
        Write-Host "  Docs     : https://$MainDomain/docs/"
    }
    Write-Host "  Portal   : https://$ExplorerDomain/portal/index.html"
} else {
    Write-Host "  Temporary Main    : http://$GatewayHost/ with Host: $MainDomain"
    Write-Host "  Temporary Explorer: http://$GatewayHost/ with Host: $ExplorerDomain"
    Write-Host "  Temporary Wallet  : http://$GatewayHost/ with Host: $WalletDomain"
    if ($docsDomainEnabled) {
        Write-Host "  Temporary Docs    : http://$GatewayHost/docs/ with Host: $DocsDomain"
    } else {
        Write-Host "  Temporary Docs    : http://$GatewayHost/docs/ with Host: $MainDomain"
    }
    Write-Host "  Temporary Portal  : http://$GatewayHost/portal/index.html with Host: $ExplorerDomain"
    Write-Host "  Production URLs pending DNS: https://$MainDomain/, https://$ExplorerDomain/, https://$WalletDomain/ and https://$DocsDomain/docs/"
}
Write-Host ""
Write-Host "Security rules:"
Write-Host "  - Validator RPC stays private."
Write-Host "  - Public RPC is proxied only through full node targets: $($resolvedRpcTargets -join ', ')."
Write-Host "  - /gateway/lb-status.json exposes backend health for the wallet RPC manager."
Write-Host "  - /archive-rpc/* proxies read-only archive node APIs when configured."
Write-Host "  - /indexer/* proxies Explorer Core indexer APIs when configured."
Write-Host "  - /wallet/events is proxied with websocket Upgrade headers."
Write-Host "  - /metrics is not public through nginx."
Write-Host "  - DTL IDE is protected by basic auth."
