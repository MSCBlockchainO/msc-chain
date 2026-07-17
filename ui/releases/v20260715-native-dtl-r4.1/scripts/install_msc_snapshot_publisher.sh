#!/usr/bin/env bash
set -euo pipefail

NODE_ID=""
DATA_DIR=""
BINARY=""
MIRROR_DIR=""
S3_URI=""
INTERVAL="15min"
USER_NAME="${SUDO_USER:-$USER}"
RETAIN="8"
SIGNING_KEY=""
RUN_NOW=1
EXPORT_TIMEOUT="${MSC_SNAPSHOT_EXPORT_TIMEOUT:-90s}"

usage() {
  cat <<'USAGE'
Usage: install_msc_snapshot_publisher.sh --id NODE --datadir DATA --mirror-dir DIR [options]

Installs a systemd timer that periodically publishes a verified MSC recovery
snapshot mirror using scripts/publish_msc_snapshot.sh.

Options:
  --id NODE            Source node ID, for example A.
  --datadir DATA       Source base data directory.
  --binary PATH        msc-node binary path. Default: ./msc-node.
  --mirror-dir DIR     Local static mirror directory.
  --s3-uri URI         Optional s3://bucket/prefix upload target.
  --signing-key PATH   Optional OpenSSL private key for signed latest.json.
  --export-timeout DURATION
                       Bound live export time before packaging a completed backup.
                       Default: 90s.
  --interval DURATION  systemd OnUnitActiveSec value. Default: 15min.
  --retain N           Local mirror artifacts to keep. Default: 8.
  --user USER          User to run publisher as. Default: invoking sudo user.
  --no-run-now         Install timer without running an immediate export.
  -h, --help           Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --datadir) DATA_DIR="${2:?--datadir requires value}"; shift 2 ;;
    --binary) BINARY="${2:?--binary requires value}"; shift 2 ;;
    --mirror-dir) MIRROR_DIR="${2:?--mirror-dir requires value}"; shift 2 ;;
    --s3-uri) S3_URI="${2:?--s3-uri requires value}"; shift 2 ;;
    --signing-key) SIGNING_KEY="${2:?--signing-key requires value}"; shift 2 ;;
    --export-timeout) EXPORT_TIMEOUT="${2:?--export-timeout requires value}"; shift 2 ;;
    --interval) INTERVAL="${2:?--interval requires value}"; shift 2 ;;
    --retain) RETAIN="${2:?--retain requires value}"; shift 2 ;;
    --user) USER_NAME="${2:?--user requires value}"; shift 2 ;;
    --no-run-now) RUN_NOW=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$NODE_ID" || -z "$DATA_DIR" ]]; then
  usage >&2
  exit 2
fi
if [[ -z "$MIRROR_DIR" && -z "$S3_URI" ]]; then
  echo "--mirror-dir or --s3-uri is required" >&2
  exit 2
fi
if [[ -z "$BINARY" ]]; then
  BINARY="./msc-node"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PUBLISHER="$SCRIPT_DIR/publish_msc_snapshot.sh"
if [[ ! -f "$PUBLISHER" ]]; then
  echo "publisher script not found: $PUBLISHER" >&2
  exit 1
fi

NODE_ID="$(printf '%s' "$NODE_ID" | tr '[:lower:]' '[:upper:]')"
unit_id="$(printf '%s' "$NODE_ID" | tr -cd '[:alnum:]_-')"
service="msc-snapshot-publish-${unit_id}.service"
timer="msc-snapshot-publish-${unit_id}.timer"

cmd=(/usr/bin/env bash "$PUBLISHER" --id "$NODE_ID" --datadir "$DATA_DIR" --binary "$BINARY" --retain "$RETAIN")
cmd+=(--export-timeout "$EXPORT_TIMEOUT")
if [[ -n "$MIRROR_DIR" ]]; then
  cmd+=(--mirror-dir "$MIRROR_DIR")
fi
if [[ -n "$S3_URI" ]]; then
  cmd+=(--s3-uri "$S3_URI")
fi
if [[ -n "$SIGNING_KEY" ]]; then
  cmd+=(--signing-key "$SIGNING_KEY")
fi

quote_cmd="$(python3 - "${cmd[@]}" <<'PY'
import shlex, sys
print(" ".join(shlex.quote(arg) for arg in sys.argv[1:]))
PY
)"

sudo tee "/etc/systemd/system/$service" >/dev/null <<SERVICE
[Unit]
Description=MSC Snapshot Publisher $NODE_ID
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=$USER_NAME
WorkingDirectory=$(pwd)
ExecStart=$quote_cmd
TimeoutStartSec=30min
Nice=5
IOSchedulingClass=best-effort
IOSchedulingPriority=6
SERVICE

sudo tee "/etc/systemd/system/$timer" >/dev/null <<TIMER
[Unit]
Description=Run MSC Snapshot Publisher $NODE_ID

[Timer]
OnBootSec=2min
OnActiveSec=30s
OnUnitActiveSec=$INTERVAL
AccuracySec=30s
Persistent=true
Unit=$service

[Install]
WantedBy=timers.target
TIMER

sudo systemctl daemon-reload
sudo systemctl enable --now "$timer"
if [[ "$RUN_NOW" == "1" ]]; then
  sudo systemctl start "$service"
fi

echo "snapshot publisher installed: $timer"
echo "service: $service"
