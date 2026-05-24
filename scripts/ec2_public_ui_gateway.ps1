param(
    [string]$GatewayHost = "50.19.167.221",
    [string]$GatewayUser = "ubuntu",
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$IdeUser = "mscadmin",
    [string]$IdePassword = $env:MSC_IDE_PASSWORD,
    [string]$RpcTarget = "127.0.0.1:26657",
    [string]$UiSource = (Join-Path (Split-Path -Parent $PSScriptRoot) "ui")
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not $IdePassword) {
    throw "Set `$env:MSC_IDE_PASSWORD before running this script."
}
if (-not (Test-Path -LiteralPath $UiSource -PathType Container)) {
    throw "UI source directory not found: $UiSource"
}

function Quote-Sh {
    param([Parameter(Mandatory = $true)][string]$Value)
    return "'" + ($Value -replace "'", "'\''") + "'"
}

$remote = @'
set -euo pipefail

if command -v apt-get >/dev/null 2>&1; then
  sudo apt-get update -y
  sudo apt-get install -y nginx apache2-utils
elif command -v dnf >/dev/null 2>&1; then
  sudo dnf install -y nginx httpd-tools
elif command -v yum >/dev/null 2>&1; then
  sudo yum install -y nginx httpd-tools
else
  echo "No supported package manager found" >&2
  exit 1
fi

sudo htpasswd -bc /etc/nginx/msc_ide.htpasswd "$IDE_USER" "$IDE_PASSWORD" >/dev/null

sudo mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
sudo mkdir -p /var/www/msc-ui
sudo find /var/www/msc-ui -mindepth 1 -maxdepth 1 -exec rm -rf {} +
sudo cp -a /tmp/msc-ui-upload/. /var/www/msc-ui/
sudo find /var/www/msc-ui -type f -exec chmod 0644 {} \;
sudo find /var/www/msc-ui -type d -exec chmod 0755 {} \;
sudo tee /etc/nginx/sites-available/msc-ui >/dev/null <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    root /var/www/msc-ui;
    absolute_redirect off;

    client_max_body_size 4m;
    proxy_read_timeout 120s;
    proxy_send_timeout 120s;

    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy no-referrer always;

    location = / {
        return 302 /explorer.html;
    }

    # Keep the wallet public and stable even though the Go file server redirects
    # index.html to ./ for direct localhost use.
    location = /msc_wallet.html {
        try_files /index.html =404;
    }

    location = /wallet.html {
        try_files /index.html =404;
    }

    location = /index.html {
        try_files /index.html =404;
    }

    location ~ ^/(explorer\.html|explorer\.js|explorer\.css|msc_wallet\.js|msc_wallet\.css|app\.js|styles\.css)$ {
        try_files $uri =404;
    }

    location ^~ /vendor/ {
        try_files $uri =404;
    }

    location ~ ^/(dtl_ide\.html|dtl_ide\.js|dtl_ide\.css)$ {
        auth_basic "MSC DTL IDE";
        auth_basic_user_file /etc/nginx/msc_ide.htpasswd;
        try_files $uri =404;
    }

    location ~ ^/(rpc|jsonrpc|v1/rpc)$ {
        auth_basic "MSC DTL RPC";
        auth_basic_user_file /etc/nginx/msc_ide.htpasswd;
        proxy_pass http://__RPC_TARGET__;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ~ ^/(status|healthz|metrics|misbehavior|validators|validatorset/hash|validatorset/audit|validators/pending|tx/status|txs|explorer/blocks|explorer/block|explorer/tx|explorer/peers|balance|sendTx|createWallet|faucet|submitTx|nonce|nonce/pending|wallet|wallet/status|wallet/create|wallet/recover|auth/challenge|auth/verify|stake|unstake|coins|tokenomics|pool/transfer|governance/status|governance/proposals|upgrade/status|dtl/quote|dtl/route_quote|dtl/farm_info|dtl/season_info|dtl/leaderboard|dtl/nft721/owner|dtl/nft1155/owner|v1/) {
        proxy_pass http://__RPC_TARGET__;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        return 404;
    }
}
NGINX

sudo sed -i "s#__RPC_TARGET__#$RPC_TARGET#g" /etc/nginx/sites-available/msc-ui
sudo ln -sf /etc/nginx/sites-available/msc-ui /etc/nginx/sites-enabled/msc-ui
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx

curl -sI -H "Host: $PUBLIC_HOST" http://127.0.0.1/explorer.html
curl -sI -H "Host: $PUBLIC_HOST" http://127.0.0.1/msc_wallet.html
curl -sI -H "Host: $PUBLIC_HOST" http://127.0.0.1/dtl_ide.html
echo "IDE auth check:"
ide_auth_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -u "${IDE_USER}:${IDE_PASSWORD}" -H "Host: $PUBLIC_HOST" http://127.0.0.1/dtl_ide.html)
echo "HTTP $ide_auth_code"
test "$ide_auth_code" = "200"
'@

$remote = ($remote -replace "`r`n", "`n") -replace "`r", ""

$target = "$GatewayUser@$GatewayHost"
$envPrefix = "IDE_USER=$(Quote-Sh $IdeUser) IDE_PASSWORD=$(Quote-Sh $IdePassword) RPC_TARGET=$(Quote-Sh $RpcTarget) PUBLIC_HOST=$(Quote-Sh $GatewayHost)"

Write-Host "Deploying MSC public UI gateway on $target..."
Write-Host "Uploading static UI from $UiSource..."
$uiArchive = Join-Path ([System.IO.Path]::GetTempPath()) ("msc-ui-upload-{0}.tar" -f ([System.Guid]::NewGuid().ToString("N")))
try {
    tar -cf $uiArchive -C $UiSource .
    scp -i $KeyPath -o StrictHostKeyChecking=no $uiArchive "${target}:/tmp/msc-ui-upload.tar"
    ssh -i $KeyPath -o StrictHostKeyChecking=no $target "rm -rf /tmp/msc-ui-upload && mkdir -p /tmp/msc-ui-upload && tar -xf /tmp/msc-ui-upload.tar -C /tmp/msc-ui-upload"
    $remote | ssh -i $KeyPath -o StrictHostKeyChecking=no $target "$envPrefix bash -s"
} finally {
    if (Test-Path -LiteralPath $uiArchive) {
        Remove-Item -LiteralPath $uiArchive -Force
    }
}

Write-Host ""
Write-Host "Gateway local-on-EC2 checks passed. Public URLs:"
Write-Host "  Explorer: http://$GatewayHost/explorer.html"
Write-Host "  Wallet  : http://$GatewayHost/msc_wallet.html"
Write-Host "  DTL IDE : http://$GatewayHost/dtl_ide.html"
Write-Host ""
Write-Host "If public URLs timeout, open the EC2 Security Group inbound rule: TCP 80 from 0.0.0.0/0."
