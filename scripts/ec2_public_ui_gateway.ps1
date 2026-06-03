param(
    [string]$GatewayHost = "50.19.167.221",
    [string]$GatewayUser = "ubuntu",
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$Domain = "mscblockexplorer.in",
    [string]$LetsEncryptEmail = "admin@mscblockexplorer.in",
    [string]$IdeUser = "mscadmin",
    [string]$IdePassword = $env:MSC_IDE_PASSWORD,
    [string]$RpcTarget = "127.0.0.1:26665",
    [string[]]$RpcTargets = @(),
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
$resolvedRpcTargets = @($RpcTargets | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
if ($resolvedRpcTargets.Count -eq 0) {
    $resolvedRpcTargets = @($RpcTarget)
}
$resolvedRpcTargets = @($resolvedRpcTargets | ForEach-Object { $_.Trim() } | Select-Object -Unique)
$rpcTargetsCsv = $resolvedRpcTargets -join ","

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

sudo tee /etc/nginx/conf.d/msc_public_gateway_limits.conf >/dev/null <<'NGINX'
limit_req_zone $binary_remote_addr zone=msc_static:10m rate=600r/m;
limit_req_zone $binary_remote_addr zone=msc_read:10m rate=600r/m;
limit_req_zone $binary_remote_addr zone=msc_write:10m rate=60r/m;
limit_req_zone $binary_remote_addr zone=msc_rpc:10m rate=120r/m;
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
    error_page 429 = @msc_rate_limited;
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
        try_files /msc_wallet.html =404;
    }

    location = /wallet.html {
        limit_req zone=msc_static burst=60 nodelay;
        try_files /wallet.html =404;
    }

    location = /index.html {
        limit_req zone=msc_static burst=60 nodelay;
        try_files /index.html =404;
    }

    location ~ ^/(dashboard\.html|send\.html|receive\.html|transactions\.html|staking\.html|validators\.html|governance\.html|bridge\.html|security\.html|settings\.html|login\.html|create-wallet\.html|explorer\.html|explorer\.js|explorer\.css|wallet_pages\.js|wallet_pages\.css|msc_wallet\.js|msc_wallet\.css|app\.js|styles\.css)$ {
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

    include /etc/nginx/msc_public_node_routes.inc;

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

    location ~ ^/(status|healthz|misbehavior|validators|validatorset/hash|validatorset/audit|validators/pending|validators/diversity|public-nodes|consensus/mode|formal/verification|storage/policy|bridge/status|bridge/verify|light/headers|light/checkpoint/latest|proof/balance|proof/tx|proof/receipt|tx/status|txs|explorer/blocks|explorer/block|explorer/tx|explorer/peers|balance|nonce|nonce/pending|wallet/status|coins|tokenomics|governance/status|governance/proposals|upgrade/status|dtl/quote|dtl/route_quote|dtl/farm_info|dtl/season_info|dtl/leaderboard|dtl/nft721/owner|dtl/nft1155/owner|v1/) {
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

sudo sed -i "s#__DOMAIN__#$DOMAIN#g; s#__PUBLIC_HOST__#$PUBLIC_HOST#g" /etc/nginx/sites-available/msc-ui
sudo ln -sf /etc/nginx/sites-available/msc-ui /etc/nginx/sites-enabled/msc-ui
sudo rm -f /etc/nginx/sites-enabled/default

sudo tee /usr/local/bin/msc-lb-health.sh >/dev/null <<'SH'
#!/usr/bin/env bash
set -euo pipefail
out="/var/www/msc-ui/gateway/lb-status.json"
public_nodes_out="/var/www/msc-ui/gateway/public-nodes.json"
targets="${RPC_TARGETS:-${RPC_TARGET:-127.0.0.1:26665}}"
mkdir -p "$(dirname "$out")"
while true; do
  tmp="$(mktemp)"
  if python3 - "$targets" "$tmp" <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

targets = [item.strip() for item in sys.argv[1].split(",") if item.strip()]
out = sys.argv[2]
domain = os.environ.get("DOMAIN", "").strip()
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
    if role == "validator":
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
payload = {
    "status": "healthy" if healthy_count == len(backends) and backends else ("degraded" if healthy_count else "down"),
    "healthy": healthy_count,
    "total": len(backends),
    "failover_count": 0,
    "last_switch": "",
    "backends": backends,
    "active_targets": active_targets,
    "ts": int(time.time()),
}
public_nodes = {
    "status": payload["status"],
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
Environment=DOMAIN=$DOMAIN
Environment=RPC_TARGETS=$RPC_TARGETS
Environment=RPC_TARGET=$RPC_TARGET
ExecStart=/usr/local/bin/msc-lb-health.sh
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
SERVICE
sudo systemctl daemon-reload
sudo systemctl enable --now msc-lb-health.service
for i in $(seq 1 12); do
  [ -s /var/www/msc-ui/gateway/lb-status.json ] && break
  sleep 1
done
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
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsSI "https://$DOMAIN/explorer.html" >/dev/null || echo "WARN explorer.html check failed"
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsSI "https://$DOMAIN/msc_wallet.html" >/dev/null || echo "WARN msc_wallet.html check failed"
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsSI "https://$DOMAIN/dashboard.html" >/dev/null || echo "WARN dashboard.html check failed"
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsSI "https://$DOMAIN/wallet.html" >/dev/null || echo "WARN wallet.html check failed"
  curl --resolve "$DOMAIN:443:127.0.0.1" -fsS "https://$DOMAIN/gateway/lb-status.json" >/dev/null || echo "WARN lb-status check failed"
  status_code=$(curl --resolve "$DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/status")
  metrics_code=$(curl --resolve "$DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/metrics")
  rpc_code=$(curl --resolve "$DOMAIN:443:127.0.0.1" --max-time 10 -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/rpc")
else
  curl -fsSI -H "Host: $DOMAIN" http://127.0.0.1/explorer.html >/dev/null || echo "WARN explorer.html check failed"
  curl -fsSI -H "Host: $DOMAIN" http://127.0.0.1/msc_wallet.html >/dev/null || echo "WARN msc_wallet.html check failed"
  curl -fsSI -H "Host: $DOMAIN" http://127.0.0.1/dashboard.html >/dev/null || echo "WARN dashboard.html check failed"
  curl -fsSI -H "Host: $DOMAIN" http://127.0.0.1/wallet.html >/dev/null || echo "WARN wallet.html check failed"
  curl -fsS -H "Host: $DOMAIN" http://127.0.0.1/gateway/lb-status.json >/dev/null || echo "WARN lb-status check failed"
  status_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $DOMAIN" http://127.0.0.1/status)
  metrics_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $DOMAIN" http://127.0.0.1/metrics)
  rpc_code=$(curl --max-time 10 -s -o /dev/null -w "%{http_code}" -H "Host: $DOMAIN" http://127.0.0.1/rpc)
fi
if [ "$status_code" != "200" ]; then echo "WARN /status returned $status_code"; fi
if [ "$metrics_code" != "404" ]; then echo "WARN /metrics returned $metrics_code"; fi
if [ "$rpc_code" = "401" ]; then echo "WARN /rpc returned 401"; fi
echo "MSC public gateway checks passed."
'@

$remote = ($remote -replace "`r`n", "`n") -replace "`r", ""

$target = "$GatewayUser@$GatewayHost"
$envPrefix = @(
    "DOMAIN=$(Quote-Sh $Domain)",
    "PUBLIC_HOST=$(Quote-Sh $GatewayHost)",
    "RPC_TARGET=$(Quote-Sh $RpcTarget)",
    "RPC_TARGETS=$(Quote-Sh $rpcTargetsCsv)",
    "ENABLE_HTTPS=$(Quote-Sh ($(if ($enableHttps) { "1" } else { "0" })))",
    "LETSENCRYPT_EMAIL=$(Quote-Sh $LetsEncryptEmail)",
    "IDE_USER=$(Quote-Sh $IdeUser)",
    "IDE_PASSWORD=$(Quote-Sh $IdePassword)"
) -join " "

Write-Host "Deploying MSC public gateway on $target..."
Write-Host "Domain: $Domain"
Write-Host "RPC targets: $($resolvedRpcTargets -join ', ')"
if (-not $enableHttps) {
    Write-Warning "HTTP-only pre-DNS mode enabled. HTTPS is not complete until $Domain A record points to $GatewayHost."
}

$uiArchive = Join-Path ([System.IO.Path]::GetTempPath()) ("msc-ui-upload-{0}.tar" -f ([System.Guid]::NewGuid().ToString("N")))
try {
    tar -cf $uiArchive -C $UiSource .
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
    Write-Host "  Explorer: https://$Domain/explorer.html"
    Write-Host "  Wallet  : https://$Domain/msc_wallet.html"
    Write-Host "  Dashboard: https://$Domain/dashboard.html"
} else {
    Write-Host "  Temporary Explorer: http://$GatewayHost/explorer.html"
    Write-Host "  Temporary Wallet  : http://$GatewayHost/msc_wallet.html"
    Write-Host "  Production URL pending DNS: https://$Domain/explorer.html"
}
Write-Host ""
Write-Host "Security rules:"
Write-Host "  - Validator RPC stays private."
Write-Host "  - Public RPC is proxied only through full node targets: $($resolvedRpcTargets -join ', ')."
Write-Host "  - /gateway/lb-status.json exposes backend health for the wallet RPC manager."
Write-Host "  - /wallet/events is proxied with websocket Upgrade headers."
Write-Host "  - /metrics is not public through nginx."
Write-Host "  - DTL IDE is protected by basic auth."
