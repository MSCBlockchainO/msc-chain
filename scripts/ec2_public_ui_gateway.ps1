param(
    [string]$GatewayHost = "50.19.167.221",
    [string]$GatewayUser = "ubuntu",
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$IdeUser = "mscadmin",
    [string]$IdePassword = $env:MSC_IDE_PASSWORD,
    [string]$RpcTarget = "127.0.0.1:26657"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not $IdePassword) {
    throw "Set `$env:MSC_IDE_PASSWORD before running this script."
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
sudo tee /etc/nginx/sites-available/msc-ui >/dev/null <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    absolute_redirect off;

    client_max_body_size 4m;
    proxy_read_timeout 120s;
    proxy_send_timeout 120s;

    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy no-referrer always;

    location = / {
        return 302 /explorer.html;
    }

    location ~ ^/(dtl_ide\.html|dtl_ide\.js|dtl_ide\.css)$ {
        auth_basic "MSC DTL IDE";
        auth_basic_user_file /etc/nginx/msc_ide.htpasswd;
        proxy_pass http://__RPC_TARGET__;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        proxy_pass http://__RPC_TARGET__;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
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
$remote | ssh -i $KeyPath -o StrictHostKeyChecking=no $target "$envPrefix bash -s"

Write-Host ""
Write-Host "Gateway local-on-EC2 checks passed. Public URLs:"
Write-Host "  Explorer: http://$GatewayHost/explorer.html"
Write-Host "  Wallet  : http://$GatewayHost/msc_wallet.html"
Write-Host "  DTL IDE : http://$GatewayHost/dtl_ide.html"
Write-Host ""
Write-Host "If public URLs timeout, open the EC2 Security Group inbound rule: TCP 80 from 0.0.0.0/0."
