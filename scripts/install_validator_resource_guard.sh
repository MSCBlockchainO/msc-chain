#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="msc-production-v2-node.service"
RELEASE_DIR="${MSC_RELEASE_DIR:-$HOME/.msc/production-v2/release}"
CONFIG_PATH=""
GOMEMLIMIT_VALUE="1600MiB"
GOGC_VALUE="50"
WORKERS="1"
SWAP_SIZE="2G"
RESTART_SERVICE="0"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --service) SERVICE_NAME="${2:?}"; shift 2 ;;
    --release-dir) RELEASE_DIR="${2:?}"; shift 2 ;;
    --config) CONFIG_PATH="${2:?}"; shift 2 ;;
    --gomemlimit) GOMEMLIMIT_VALUE="${2:?}"; shift 2 ;;
    --gogc) GOGC_VALUE="${2:?}"; shift 2 ;;
    --workers) WORKERS="${2:?}"; shift 2 ;;
    --swap-size) SWAP_SIZE="${2:?}"; shift 2 ;;
    --restart) RESTART_SERVICE="1"; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$CONFIG_PATH" ]; then
  CONFIG_PATH="$RELEASE_DIR/config.toml"
fi

sudo mkdir -p "/etc/systemd/system/${SERVICE_NAME}.d"

if [ -f "$CONFIG_PATH" ]; then
  ts="$(date -u +%Y%m%d%H%M%S)"
  sudo cp -a "$CONFIG_PATH" "$CONFIG_PATH.bak-resource-guard-$ts"
  sudo perl -0pi -e \
    "s/(delta_replay_verify_workers\\s*=\\s*)\\d+/\${1}${WORKERS}/g; s/(ed25519_batch_verify_workers\\s*=\\s*)\\d+/\${1}${WORKERS}/g; s/(snapshot_parallel_chunks\\s*=\\s*)\\d+/\${1}${WORKERS}/g; s/(snapshot_chunk_replication_factor\\s*=\\s*)\\d+/\${1}${WORKERS}/g" \
    "$CONFIG_PATH"
fi

if ! swapon --show=NAME --noheadings | grep -q .; then
  if command -v fallocate >/dev/null 2>&1; then
    sudo fallocate -l "$SWAP_SIZE" /swapfile
  else
    swap_mb="${SWAP_SIZE%G}"
    swap_mb=$((swap_mb * 1024))
    sudo dd if=/dev/zero of=/swapfile bs=1M count="$swap_mb" status=none
  fi
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile >/dev/null
  sudo swapon /swapfile
  if ! grep -q '^/swapfile ' /etc/fstab; then
    echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab >/dev/null
  fi
fi

sudo tee "/etc/systemd/system/${SERVICE_NAME}.d/98-memory-guard.conf" >/dev/null <<EOF
[Service]
Environment=GOMEMLIMIT=${GOMEMLIMIT_VALUE}
Environment=GOGC=${GOGC_VALUE}
Environment=GOMAXPROCS=1
Environment=MSC_RUNTIME_WORKER_BUDGET=1
Environment=MSC_SYNC_MAX_WORKERS=1
Environment=MSC_PUBSUB_VALIDATE_WORKERS=1
Environment=MSC_RUNTIME_CPU_PROFILE=validator
Environment=MSC_TURBO_SYNC_FAST=0
MemoryAccounting=true
MemoryHigh=2600M
Nice=10
EOF

sudo systemctl daemon-reload
if [ "$RESTART_SERVICE" = "1" ]; then
  sudo systemctl restart "$SERVICE_NAME"
fi

systemctl --no-pager --full status "$SERVICE_NAME" | sed -n '1,24p'
free -h
swapon --show || true
