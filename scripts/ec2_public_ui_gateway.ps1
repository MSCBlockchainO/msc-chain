param(
    [string]$GatewayHost = "50.19.167.221",
    [string]$GatewayUser = "ubuntu",
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$Domain = "mscblockexplorer.in",
    [string]$LetsEncryptEmail = "admin@mscblockexplorer.in",
    [string]$IdeUser = "mscadmin",
    [string]$IdePassword = $env:MSC_IDE_PASSWORD,
    [string]$RpcTarget = "127.0.0.1:26665",
    [string]$UiSource = (Join-Path (Split-Path -Parent $PSScriptRoot) "ui"),
    [switch]$AllowHttpOnlyUntilDNS
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $UiSource -PathType Container)) {
    throw "UI source directory not found: $UiSource"
}
if ([string]::IsNullOrWhiteSpace($Domain)) {
    throw "Domain is required for the production public gateway."
}

function Quote-Sh {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value)
    return "'" + ($Value -replace "'", "'\''") + "'"
}

function Resolve-DomainIPv4 {
    param([Parameter(Mandatory = $true)][string]$Name)
    try {
        return @(Resolve-DnsName $Name -Type A -ErrorAction Stop |
            Where-Object { $_.IPAddress } |
            ForEach-Object { [string]$_.IPAddress })
    } catch {
        return @()
    }
}

$domainIPs = Resolve-DomainIPv4 -Name $Domain
if (-not $AllowHttpOnlyUntilDNS -and ($domainIPs -notcontains $GatewayHost)) {
    $current = if ($domainIPs.Count -gt 0) { $domainIPs -join ", " } else { "none" }
    throw "DNS preflight failed: $Domain resolves to [$current], not gateway $GatewayHost. Point the A record to $GatewayHost before issuing HTTPS."
}

$enableHttps = -not $AllowHttpOnlyUntilDNS

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
sudo find /var/www/msc-ui -mindepth 1 -maxdepth 1 -exec rm -rf {} +
sudo cp -a "$HOME/msc-ui-upload/." /var/www/msc-ui/
sudo chown -R www-data:www-data /var/www/msc-ui 2>/dev/null || sudo chown -R nginx:nginx /var/www/msc-ui
sudo find /var/www/msc-ui -type f -exec chmod 0644 {} \;
sudo find /var/www/msc-ui -type d -exec chmod 0755 {} \;

if [ -n "${IDE_PASSWORD:-}" ]; then
  sudo htpasswd -bc /etc/nginx/msc_ide.htpasswd "$IDE_USER" "$IDE_PASSWORD" >/dev/null
elif [ ! -f /etc/nginx/msc_ide.htpasswd ]; then
  tmp_pass="$(openssl rand -hex 24 2>/dev/null || date +%s%N)"
  sudo htpasswd -bc /etc/nginx/msc_ide.htpasswd "$IDE_USER" "$tmp_pass" >/dev/null
  echo "DTL IDE locked with a generated server-local password because MSC_IDE_PASSWORD was not provided."
fi

sudo tee /etc/nginx/conf.d/msc_public_gateway_limits.conf >/dev/null <<'NGINX'
limit_req_zone $binary_remote_addr zone=msc_static:10m rate=240r/m;
limit_req_zone $binary_remote_addr zone=msc_read:10m rate=120r/m;
limit_req_zone $binary_remote_addr zone=msc_write:10m rate=20r/m;
limit_req_zone $binary_remote_addr zone=msc_rpc:10m rate=60r/m;
limit_conn_zone $binary_remote_addr zone=msc_conn:10m;
NGINX

sudo tee /etc/nginx/sites-available/msc-ui >/dev/null <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name __DOMAIN__ __PUBLIC_HOST__;
    root /var/www/msc-ui;
    absolute_redirect off;
    server_tokens off;

    client_max_body_size 2m;
    proxy_read_timeout 60s;
    proxy_send_timeout 60s;
    limit_req_status 429;
    limit_conn_status 429;
    limit_conn msc_conn 20;

    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy no-referrer always;
    add_header X-Frame-Options DENY always;
    add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/letsencrypt;
        default_type text/plain;
    }

    location = / {
        return 302 /explorer.html;
    }

    location ~ /\. {
        return 404;
    }

    location = /msc_wallet.html {
        limit_req zone=msc_static burst=60 nodelay;
        try_files /index.html =404;
    }

    location = /wallet.html {
        limit_req zone=msc_static burst=60 nodelay;
        try_files /index.html =404;
    }

    location = /index.html {
        limit_req zone=msc_static burst=60 nodelay;
        try_files /index.html =404;
    }

    location ~ ^/(explorer\.html|explorer\.js|explorer\.css|msc_wallet\.js|msc_wallet\.css|app\.js|styles\.css)$ {
        limit_req zone=msc_static burst=60 nodelay;
        try_files $uri =404;
    }

    location ^~ /vendor/ {
        limit_req zone=msc_static burst=60 nodelay;
        try_files $uri =404;
    }

    location ~ ^/(dtl_ide\.html|dtl_ide\.js|dtl_ide\.css)$ {
        auth_basic "MSC DTL IDE";
        auth_basic_user_file /etc/nginx/msc_ide.htpasswd;
        limit_req zone=msc_read burst=20 nodelay;
        try_files $uri =404;
    }

    # Public JSON-RPC is allowed only through this full-node gateway. Validator
    # RPC ports must remain private or loopback-only.
    location ~ ^/(rpc|jsonrpc|v1/rpc)$ {
        limit_req zone=msc_rpc burst=30 nodelay;
        proxy_pass http://__RPC_TARGET__;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ~ ^/(sendTx|submitTx|faucet|stake|unstake|pool/transfer|auth/challenge|auth/verify|wallet/create|wallet/recover|v1/tx|v1/sendTx|v1/submitTx) {
        limit_req zone=msc_write burst=10 nodelay;
        proxy_pass http://__RPC_TARGET__;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ~ ^/(status|healthz|misbehavior|validators|validatorset/hash|validatorset/audit|validators/pending|consensus/mode|tx/status|txs|explorer/blocks|explorer/block|explorer/tx|explorer/peers|balance|nonce|nonce/pending|wallet/status|coins|tokenomics|governance/status|governance/proposals|upgrade/status|dtl/quote|dtl/route_quote|dtl/farm_info|dtl/season_info|dtl/leaderboard|dtl/nft721/owner|dtl/nft1155/owner|v1/) {
        limit_req zone=msc_read burst=40 nodelay;
        proxy_pass http://__RPC_TARGET__;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /metrics {
        return 404;
    }

    location / {
        return 404;
    }
}
NGINX

sudo sed -i "s#__RPC_TARGET__#$RPC_TARGET#g; s#__DOMAIN__#$DOMAIN#g; s#__PUBLIC_HOST__#$PUBLIC_HOST#g" /etc/nginx/sites-available/msc-ui
sudo ln -sf /etc/nginx/sites-available/msc-ui /etc/nginx/sites-enabled/msc-ui
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx

if [ "$ENABLE_HTTPS" = "1" ]; then
  sudo certbot --nginx \
    --non-interactive \
    --agree-tos \
    --email "$LETSENCRYPT_EMAIL" \
    --redirect \
    --hsts \
    -d "$DOMAIN"
  sudo nginx -t
  sudo systemctl reload nginx
fi

if [ "$ENABLE_HTTPS" = "1" ]; then
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsSI "https://$DOMAIN/explorer.html" >/dev/null
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsSI "https://$DOMAIN/msc_wallet.html" >/dev/null
  status_code=$(curl --resolve "$DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/status")
  metrics_code=$(curl --resolve "$DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/metrics")
  rpc_code=$(curl --resolve "$DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/rpc")
else
  curl -fsSI -H "Host: $DOMAIN" http://127.0.0.1/explorer.html >/dev/null
  curl -fsSI -H "Host: $DOMAIN" http://127.0.0.1/msc_wallet.html >/dev/null
  status_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $DOMAIN" http://127.0.0.1/status)
  metrics_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $DOMAIN" http://127.0.0.1/metrics)
  rpc_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $DOMAIN" http://127.0.0.1/rpc)
fi
test "$status_code" = "200"
test "$metrics_code" = "404"
test "$rpc_code" != "401"
echo "MSC public gateway checks passed."
'@

$remote = ($remote -replace "`r`n", "`n") -replace "`r", ""

$target = "$GatewayUser@$GatewayHost"
$envPrefix = @(
    "DOMAIN=$(Quote-Sh $Domain)",
    "PUBLIC_HOST=$(Quote-Sh $GatewayHost)",
    "RPC_TARGET=$(Quote-Sh $RpcTarget)",
    "ENABLE_HTTPS=$(Quote-Sh ($(if ($enableHttps) { "1" } else { "0" })))",
    "LETSENCRYPT_EMAIL=$(Quote-Sh $LetsEncryptEmail)",
    "IDE_USER=$(Quote-Sh $IdeUser)",
    "IDE_PASSWORD=$(Quote-Sh $IdePassword)"
) -join " "

Write-Host "Deploying MSC public gateway on $target..."
Write-Host "Domain: $Domain"
Write-Host "RPC target: $RpcTarget"
if (-not $enableHttps) {
    Write-Warning "HTTP-only pre-DNS mode enabled. HTTPS is not complete until $Domain A record points to $GatewayHost."
}

$uiArchive = Join-Path ([System.IO.Path]::GetTempPath()) ("msc-ui-upload-{0}.tar" -f ([System.Guid]::NewGuid().ToString("N")))
try {
    tar -cf $uiArchive -C $UiSource .
    ssh -i $KeyPath -o StrictHostKeyChecking=no $target 'sudo rm -rf "$HOME/msc-ui-upload" "$HOME/msc-ui-upload.tar" && mkdir -p "$HOME/msc-ui-upload"'
    if ($LASTEXITCODE -ne 0) { throw "failed to prepare remote UI upload directory" }
    scp -i $KeyPath -o StrictHostKeyChecking=no $uiArchive "${target}:msc-ui-upload.tar"
    if ($LASTEXITCODE -ne 0) { throw "failed to upload UI archive" }
    ssh -i $KeyPath -o StrictHostKeyChecking=no $target "tar -xf ~/msc-ui-upload.tar -C ~/msc-ui-upload"
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
    Write-Host "  Explorer: https://$Domain/explorer.html"
    Write-Host "  Wallet  : https://$Domain/msc_wallet.html"
} else {
    Write-Host "  Temporary Explorer: http://$GatewayHost/explorer.html"
    Write-Host "  Temporary Wallet  : http://$GatewayHost/msc_wallet.html"
    Write-Host "  Production URL pending DNS: https://$Domain/explorer.html"
}
Write-Host ""
Write-Host "Security rules:"
Write-Host "  - Validator RPC stays private."
Write-Host "  - Public RPC is proxied only through the full node target $RpcTarget."
Write-Host "  - /metrics is not public through nginx."
Write-Host "  - DTL IDE is protected by basic auth."
