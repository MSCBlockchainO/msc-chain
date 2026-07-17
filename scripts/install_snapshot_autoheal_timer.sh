#!/usr/bin/env bash
set -euo pipefail

NODE_ID=""
DATA_DIR=""
BINARY=""
SERVICE=""
RPC_URL=""
MIRROR_URL=""
CONFIG_DIR=""
BOOTSTRAP_SCRIPT=""
UNIT_NAME="msc-snapshot-autoheal"
LAG_THRESHOLD="${MSC_AUTOHEAL_LAG_THRESHOLD:-250}"
STALE_SECONDS="${MSC_AUTOHEAL_STALE_SECONDS:-600}"
STARTUP_GRACE_SECONDS="${MSC_AUTOHEAL_STARTUP_GRACE_SECONDS:-300}"
MIN_SNAPSHOT_ADVANCE="${MSC_AUTOHEAL_MIN_SNAPSHOT_ADVANCE:-500}"
ON_UNIT_ACTIVE_SEC="${MSC_AUTOHEAL_INTERVAL:-5min}"

usage() {
  cat <<'USAGE'
Usage: install_snapshot_autoheal_timer.sh --id NODE --datadir DATA --binary BIN --service UNIT --rpc-url URL --mirror-url URL [options]

Installs a systemd timer that repairs a lagging full/archive/explorer node by
using the verified snapshot bootstrap flow. The bootstrap script rejects
validator.sec, MPC material, p2p identity keys, wallets, and private key files
inside snapshot artifacts.

Options:
  --id NODE                 Node id from node_identity.json.
  --datadir DATA            Node base data directory.
  --binary BIN              msc-node binary used for snapshot import.
  --service UNIT            systemd service to stop/start around import.
  --rpc-url URL             Local status URL, e.g. http://127.0.0.1:26657/status.
  --mirror-url URL          Snapshot mirror base URL, e.g. https://example/snapshots.
  --config-dir DIR          Working directory for snapshot import. Default: binary dir.
  --bootstrap-script PATH   bootstrap_msc_snapshot.sh path.
  --unit-name NAME          systemd unit prefix. Default: msc-snapshot-autoheal.
  --lag-threshold N         Apply when network lag is >= N. Default: 250.
  --stale-seconds N         Apply when last block age is >= N and snapshot is newer.
  --startup-grace-seconds N Skip if the target service restarted recently. Default: 300.
  --min-snapshot-advance N  Require latest snapshot to be N blocks newer than local. Default: 500.
  --interval DURATION       systemd OnUnitActiveSec. Default: 5min.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --datadir) DATA_DIR="${2:?--datadir requires value}"; shift 2 ;;
    --binary) BINARY="${2:?--binary requires value}"; shift 2 ;;
    --service) SERVICE="${2:?--service requires value}"; shift 2 ;;
    --rpc-url) RPC_URL="${2:?--rpc-url requires value}"; shift 2 ;;
    --mirror-url) MIRROR_URL="${2:?--mirror-url requires value}"; shift 2 ;;
    --config-dir) CONFIG_DIR="${2:?--config-dir requires value}"; shift 2 ;;
    --bootstrap-script) BOOTSTRAP_SCRIPT="${2:?--bootstrap-script requires value}"; shift 2 ;;
    --unit-name) UNIT_NAME="${2:?--unit-name requires value}"; shift 2 ;;
    --lag-threshold) LAG_THRESHOLD="${2:?--lag-threshold requires value}"; shift 2 ;;
    --stale-seconds) STALE_SECONDS="${2:?--stale-seconds requires value}"; shift 2 ;;
    --startup-grace-seconds) STARTUP_GRACE_SECONDS="${2:?--startup-grace-seconds requires value}"; shift 2 ;;
    --min-snapshot-advance) MIN_SNAPSHOT_ADVANCE="${2:?--min-snapshot-advance requires value}"; shift 2 ;;
    --interval) ON_UNIT_ACTIVE_SEC="${2:?--interval requires value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required in PATH" >&2; exit 1; }
}

need python3
need sudo
need flock

[[ -n "$NODE_ID" && -n "$DATA_DIR" && -n "$BINARY" && -n "$SERVICE" && -n "$RPC_URL" && -n "$MIRROR_URL" ]] || {
  usage >&2
  exit 2
}
[[ -x "$BINARY" ]] || { echo "binary not executable: $BINARY" >&2; exit 1; }
if [[ -z "$CONFIG_DIR" ]]; then
  CONFIG_DIR="$(cd "$(dirname "$BINARY")" && pwd)"
fi
if [[ -z "$BOOTSTRAP_SCRIPT" ]]; then
  BOOTSTRAP_SCRIPT="$(cd "$(dirname "$0")" && pwd)/bootstrap_msc_snapshot.sh"
fi
[[ -x "$BOOTSTRAP_SCRIPT" ]] || { echo "bootstrap script not executable: $BOOTSTRAP_SCRIPT" >&2; exit 1; }
[[ "$LAG_THRESHOLD" =~ ^[0-9]+$ ]] || { echo "--lag-threshold must be numeric" >&2; exit 2; }
[[ "$STALE_SECONDS" =~ ^[0-9]+$ ]] || { echo "--stale-seconds must be numeric" >&2; exit 2; }
[[ "$STARTUP_GRACE_SECONDS" =~ ^[0-9]+$ ]] || { echo "--startup-grace-seconds must be numeric" >&2; exit 2; }
[[ "$MIN_SNAPSHOT_ADVANCE" =~ ^[0-9]+$ ]] || { echo "--min-snapshot-advance must be numeric" >&2; exit 2; }

unit_safe="$(printf '%s' "$UNIT_NAME" | tr -c 'A-Za-z0-9_.@-' '-')"
runner="/usr/local/bin/${unit_safe}.sh"
service_unit="/etc/systemd/system/${unit_safe}.service"
timer_unit="/etc/systemd/system/${unit_safe}.timer"
lock_file="/run/${unit_safe}.lock"

tmp_runner="$(mktemp)"
cat >"$tmp_runner" <<'RUNNER'
#!/usr/bin/env bash
set -euo pipefail

LOG_PREFIX="[SNAPSHOT-AUTOHEAL]"
exec 9>__LOCK_FILE__
if ! flock -n 9; then
  echo "$LOG_PREFIX already_running"
  exit 0
fi

status_json="$(mktemp)"
latest_json="$(mktemp)"
cleanup() {
  rm -f "$status_json" "$latest_json"
}
trap cleanup EXIT

active_age="$(python3 - "__SERVICE__" <<'PY'
import datetime as dt, subprocess, sys
service = sys.argv[1]
raw = subprocess.run(
    ["systemctl", "show", service, "-p", "ActiveEnterTimestamp", "--value"],
    text=True,
    capture_output=True,
    timeout=5,
).stdout.strip()
if not raw:
    print(999999)
    raise SystemExit(0)
try:
    entered = dt.datetime.strptime(raw, "%a %Y-%m-%d %H:%M:%S %Z").replace(tzinfo=dt.timezone.utc)
    print(max(int((dt.datetime.now(dt.timezone.utc) - entered).total_seconds()), 0))
except Exception:
    print(999999)
PY
)"
if [[ "$active_age" =~ ^[0-9]+$ && "$active_age" -lt __STARTUP_GRACE_SECONDS__ ]]; then
  echo "$LOG_PREFIX decision=skip reason=startup_grace active_age=$active_age grace=__STARTUP_GRACE_SECONDS__"
  exit 0
fi

status_ok=0
if python3 - "__RPC_URL__" "$status_json" <<'PY'; then
import sys, urllib.request
url, out = sys.argv[1], sys.argv[2]
with urllib.request.urlopen(url, timeout=5) as r:
    raw = r.read()
open(out, "wb").write(raw)
PY
  status_ok=1
fi

python3 - "__MIRROR_URL__" "$latest_json" <<'PY'
import sys, urllib.request
base, out = sys.argv[1].rstrip("/"), sys.argv[2]
with urllib.request.urlopen(base + "/latest.json", timeout=15) as r:
    raw = r.read()
open(out, "wb").write(raw)
PY

decision="$(python3 - "$status_ok" "$status_json" "$latest_json" "__LAG_THRESHOLD__" "__STALE_SECONDS__" "__MIN_SNAPSHOT_ADVANCE__" <<'PY'
import json, sys
status_ok = sys.argv[1] == "1"
status_path, latest_path = sys.argv[2], sys.argv[3]
lag_threshold, stale_seconds = int(sys.argv[4]), int(sys.argv[5])
min_advance = int(sys.argv[6])
latest = json.load(open(latest_path, encoding="utf-8-sig"))
latest_height = int(latest.get("height") or 0)
if latest_height <= 0:
    print("skip invalid_latest 0 0 0")
    raise SystemExit(0)
if not status_ok:
    print(f"apply status_unavailable 0 {latest_height} 0")
    raise SystemExit(0)
status = json.load(open(status_path, encoding="utf-8-sig"))
local = int(status.get("height") or 0)
best = int(status.get("network_best_height") or 0)
lag = int(status.get("network_lag_blocks") or status.get("sync_lag_blocks") or max(best - local, 0))
last_age = int(status.get("last_block_age_seconds") or 0)
if latest_height <= local:
    print(f"skip local_current {local} {latest_height} {lag}")
elif latest_height - local < min_advance:
    print(f"skip snapshot_advance_small {local} {latest_height} {lag}")
elif lag >= lag_threshold:
    print(f"apply lag_threshold {local} {latest_height} {lag}")
elif last_age >= stale_seconds:
    print(f"apply stale_height {local} {latest_height} {lag}")
else:
    print(f"skip below_threshold {local} {latest_height} {lag}")
PY
)"
set -- $decision
action="$1"
reason="$2"
local_height="$3"
latest_height="$4"
lag_blocks="$5"
echo "$LOG_PREFIX decision=$action reason=$reason local=$local_height latest=$latest_height lag=$lag_blocks"
if [[ "$action" != "apply" ]]; then
  exit 0
fi

rc=0
sudo systemctl stop "__SERVICE__" || rc=$?
if [[ "$rc" != "0" ]]; then
  echo "$LOG_PREFIX stop_failed service=__SERVICE__ rc=$rc"
  sudo systemctl start "__SERVICE__" || true
  exit "$rc"
fi

set +e
bash "__BOOTSTRAP_SCRIPT__" \
  --id "__NODE_ID__" \
  --datadir "__DATA_DIR__" \
  --binary "__BINARY__" \
  --mirror-url "__MIRROR_URL__" \
  --config-dir "__CONFIG_DIR__" \
  --no-local-height-skip
rc=$?
set -e
sudo systemctl start "__SERVICE__" || true
echo "$LOG_PREFIX bootstrap_rc=$rc"
exit "$rc"
RUNNER

python3 - "$tmp_runner" "$runner" "$lock_file" "$RPC_URL" "$MIRROR_URL" "$LAG_THRESHOLD" "$STALE_SECONDS" "$STARTUP_GRACE_SECONDS" "$MIN_SNAPSHOT_ADVANCE" "$SERVICE" "$BOOTSTRAP_SCRIPT" "$NODE_ID" "$DATA_DIR" "$BINARY" "$CONFIG_DIR" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
repl = {
    "__LOCK_FILE__": sys.argv[3],
    "__RPC_URL__": sys.argv[4],
    "__MIRROR_URL__": sys.argv[5].rstrip("/"),
    "__LAG_THRESHOLD__": sys.argv[6],
    "__STALE_SECONDS__": sys.argv[7],
    "__STARTUP_GRACE_SECONDS__": sys.argv[8],
    "__MIN_SNAPSHOT_ADVANCE__": sys.argv[9],
    "__SERVICE__": sys.argv[10],
    "__BOOTSTRAP_SCRIPT__": sys.argv[11],
    "__NODE_ID__": sys.argv[12],
    "__DATA_DIR__": sys.argv[13],
    "__BINARY__": sys.argv[14],
    "__CONFIG_DIR__": sys.argv[15],
}
text = path.read_text()
for old, new in repl.items():
    text = text.replace(old, new)
path.write_text(text)
PY

sudo install -m 0755 "$tmp_runner" "$runner"
rm -f "$tmp_runner"

tmp_service="$(mktemp)"
cat >"$tmp_service" <<UNIT
[Unit]
Description=MSC Snapshot Autoheal ${unit_safe}
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=${runner}
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
UNIT
sudo install -m 0644 "$tmp_service" "$service_unit"
rm -f "$tmp_service"

tmp_timer="$(mktemp)"
cat >"$tmp_timer" <<UNIT
[Unit]
Description=Run MSC Snapshot Autoheal ${unit_safe}

[Timer]
OnBootSec=4min
OnUnitActiveSec=${ON_UNIT_ACTIVE_SEC}
AccuracySec=30s
Persistent=true
Unit=${unit_safe}.service

[Install]
WantedBy=timers.target
UNIT
sudo install -m 0644 "$tmp_timer" "$timer_unit"
rm -f "$tmp_timer"

sudo systemctl daemon-reload
sudo systemctl enable --now "${unit_safe}.timer"
echo "installed ${unit_safe}.timer"
