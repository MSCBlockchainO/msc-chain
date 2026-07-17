#!/usr/bin/env bash
set -euo pipefail

MIRROR_DIR=""
TARGET=""
SSH_KEY=""
PUBLIC_KEY=""
INTERVAL="2min"
USER_NAME="${SUDO_USER:-$USER}"
RUN_NOW=1

usage() {
  cat <<'USAGE'
Usage: install_msc_snapshot_web_sync.sh --mirror-dir DIR --target USER@HOST:/path [options]

Installs a systemd timer that pushes a signed MSC snapshot mirror to a public
static web directory using scripts/sync_msc_snapshot_mirror.sh.

Options:
  --mirror-dir DIR      Local mirror directory containing latest.json.
  --target TARGET       SSH target directory, for example ubuntu@web:/var/www/msc-ui/snapshots.
  --ssh-key PATH        Optional private key for ssh/scp.
  --public-key PATH     Optional snapshot public key to publish beside latest.json.
  --interval DURATION   systemd OnUnitActiveSec value. Default: 2min.
  --user USER           User to run sync as. Default: invoking sudo user.
  --no-run-now          Install timer without running an immediate sync.
  -h, --help            Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mirror-dir) MIRROR_DIR="${2:?--mirror-dir requires value}"; shift 2 ;;
    --target) TARGET="${2:?--target requires value}"; shift 2 ;;
    --ssh-key) SSH_KEY="${2:?--ssh-key requires value}"; shift 2 ;;
    --public-key) PUBLIC_KEY="${2:?--public-key requires value}"; shift 2 ;;
    --interval) INTERVAL="${2:?--interval requires value}"; shift 2 ;;
    --user) USER_NAME="${2:?--user requires value}"; shift 2 ;;
    --no-run-now) RUN_NOW=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "$MIRROR_DIR" && -n "$TARGET" ]] || { usage >&2; exit 2; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYNCER="$SCRIPT_DIR/sync_msc_snapshot_mirror.sh"
if [[ ! -f "$SYNCER" ]]; then
  echo "sync script not found: $SYNCER" >&2
  exit 1
fi

cmd=(/usr/bin/env bash "$SYNCER" --mirror-dir "$MIRROR_DIR" --target "$TARGET")
if [[ -n "$SSH_KEY" ]]; then
  cmd+=(--ssh-key "$SSH_KEY")
fi
if [[ -n "$PUBLIC_KEY" ]]; then
  cmd+=(--public-key "$PUBLIC_KEY")
fi

quote_cmd="$(python3 - "${cmd[@]}" <<'PY'
import shlex, sys
print(" ".join(shlex.quote(arg) for arg in sys.argv[1:]))
PY
)"

service="msc-snapshot-web-sync.service"
timer="msc-snapshot-web-sync.timer"

sudo tee "/etc/systemd/system/$service" >/dev/null <<SERVICE
[Unit]
Description=MSC Snapshot Web Mirror Sync
After=network-online.target msc-snapshot-publish-a.service
Wants=network-online.target

[Service]
Type=oneshot
User=$USER_NAME
WorkingDirectory=$(pwd)
ExecStart=$quote_cmd
TimeoutStartSec=5min
Nice=5
IOSchedulingClass=best-effort
IOSchedulingPriority=6
SERVICE

sudo tee "/etc/systemd/system/$timer" >/dev/null <<TIMER
[Unit]
Description=Run MSC Snapshot Web Mirror Sync

[Timer]
OnBootSec=3min
OnActiveSec=20s
OnUnitActiveSec=$INTERVAL
AccuracySec=15s
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

echo "snapshot web sync installed: $timer"
echo "service: $service"
