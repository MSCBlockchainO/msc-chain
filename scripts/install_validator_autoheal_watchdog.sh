#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="msc-production-v2-node.service"
RPC_URL="http://127.0.0.1:26657/status"
INTERVAL_SECONDS="60"
STUCK_SECONDS="300"
LAG_RESTART_THRESHOLD="120"
EMERGENCY_LAG_THRESHOLD="50"
BAD_SAMPLE_THRESHOLD="3"
RESTART_COOLDOWN_SECONDS="900"
STARTUP_GRACE_SECONDS="600"
SYNC_STUCK_SECONDS="900"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --service) SERVICE_NAME="${2:?}"; shift 2 ;;
    --rpc-url) RPC_URL="${2:?}"; shift 2 ;;
    --interval) INTERVAL_SECONDS="${2:?}"; shift 2 ;;
    --stuck-seconds) STUCK_SECONDS="${2:?}"; shift 2 ;;
    --lag-restart-threshold) LAG_RESTART_THRESHOLD="${2:?}"; shift 2 ;;
    --emergency-lag-threshold) EMERGENCY_LAG_THRESHOLD="${2:?}"; shift 2 ;;
    --bad-samples) BAD_SAMPLE_THRESHOLD="${2:?}"; shift 2 ;;
    --restart-cooldown) RESTART_COOLDOWN_SECONDS="${2:?}"; shift 2 ;;
    --startup-grace) STARTUP_GRACE_SECONDS="${2:?}"; shift 2 ;;
    --sync-stuck-seconds) SYNC_STUCK_SECONDS="${2:?}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

sudo install -d -m 0755 /usr/local/lib/msc /var/lib/msc-autoheal

sudo tee /usr/local/lib/msc/validator_autoheal_watchdog.py >/dev/null <<'PY'
#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import time
import urllib.request

service = os.environ.get("MSC_NODE_SERVICE", "msc-production-v2-node.service")
rpc_url = os.environ.get("MSC_STATUS_URL", "http://127.0.0.1:26657/status")
state_path = os.environ.get("MSC_AUTOHEAL_STATE", "/var/lib/msc-autoheal/validator-watchdog-state.json")
stuck_seconds = int(os.environ.get("MSC_AUTOHEAL_STUCK_SECONDS", "300"))
lag_restart_threshold = int(os.environ.get("MSC_AUTOHEAL_LAG_RESTART_THRESHOLD", "120"))
emergency_lag_threshold = int(os.environ.get("MSC_AUTOHEAL_EMERGENCY_LAG_THRESHOLD", "50"))
bad_sample_threshold = int(os.environ.get("MSC_AUTOHEAL_BAD_SAMPLE_THRESHOLD", "3"))
restart_cooldown = int(os.environ.get("MSC_AUTOHEAL_RESTART_COOLDOWN_SECONDS", "900"))
startup_grace = int(os.environ.get("MSC_AUTOHEAL_STARTUP_GRACE_SECONDS", "600"))
sync_stuck_seconds = int(os.environ.get("MSC_AUTOHEAL_SYNC_STUCK_SECONDS", "900"))

def log(message):
    print(f"[msc-validator-autoheal] {message}", flush=True)

def load_state():
    try:
        with open(state_path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
            return data if isinstance(data, dict) else {}
    except Exception:
        return {}

def save_state(data):
    os.makedirs(os.path.dirname(state_path), exist_ok=True)
    tmp = state_path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(data, fh, separators=(",", ":"))
    os.replace(tmp, state_path)

def fetch_status():
    req = urllib.request.Request(rpc_url, headers={"User-Agent": "msc-validator-autoheal/1"})
    with urllib.request.urlopen(req, timeout=8) as res:
        raw = res.read(2 * 1024 * 1024).decode("utf-8", "replace")
        return json.loads(raw or "{}")

def service_is_active():
    return subprocess.run(
        ["systemctl", "is-active", "--quiet", service],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    ).returncode == 0

def service_active_seconds():
    try:
        out = subprocess.check_output(
            ["systemctl", "show", service, "-p", "ActiveEnterTimestampMonotonic", "--value"],
            text=True,
            timeout=2,
        ).strip()
        started_us = int(out or "0")
        if started_us <= 0:
            return None
        with open("/proc/uptime", "r", encoding="utf-8") as fh:
            uptime_s = float(fh.read().split()[0])
        return max(0, int(uptime_s - (started_us / 1000000.0)))
    except Exception:
        return None

now = int(time.time())
state = load_state()

try:
    status = fetch_status()
except Exception as exc:
    bad_samples = int(state.get("bad_samples") or 0) + 1
    state.update({"bad_samples": bad_samples, "last_error": str(exc), "last_checked": now})
    last_restart = int(state.get("last_restart") or 0)
    uptime = service_active_seconds()
    if not service_is_active():
        log(f"restart service={service} reason=service_inactive error={exc}")
        subprocess.run(["systemctl", "restart", service], check=False)
        state["last_restart"] = now
        state["bad_samples"] = 0
        save_state(state)
        sys.exit(0)
    if uptime is not None and uptime < startup_grace:
        save_state(state)
        log(f"status_fetch_failed startup_grace={uptime}s/{startup_grace}s samples={bad_samples} error={exc}")
        sys.exit(0)
    if bad_samples >= bad_sample_threshold and now - last_restart >= restart_cooldown:
        log(f"restart service={service} reason=status_fetch_failed samples={bad_samples} error={exc}")
        subprocess.run(["systemctl", "restart", service], check=False)
        state["last_restart"] = now
        state["bad_samples"] = 0
        save_state(state)
        sys.exit(0)
    save_state(state)
    log(f"status_fetch_failed samples={bad_samples} error={exc}")
    sys.exit(0)

height = int(status.get("height") or 0)
best = int(status.get("network_best_height") or height)
network_lag = int(status.get("network_lag_blocks") or max(0, best - height))
sync_lag = int(status.get("sync_lag_blocks") or 0)
lag = max(network_lag, sync_lag, max(0, best - height))
age = int(status.get("last_block_age_seconds") or 0)
mode = str(status.get("consensus_detector_mode") or "").upper()
health = str(status.get("network_health") or "")
action = str(status.get("autoheal_action") or "").lower()
level = str(status.get("autoheal_level") or "").lower()
validator_state = str(status.get("validator_state") or "").lower()
sync_stage = str(status.get("sync_stage") or "").lower()
syncing = bool(status.get("syncing") or False)
sync_complete = bool(status.get("sync_complete") if "sync_complete" in status else not syncing)
active_sync = (
    syncing
    or not sync_complete
    or validator_state in ("syncing", "catching_up", "joining", "observer")
    or "sync" in action
    or "catch" in action
    or "replay" in action
    or "snapshot" in action
    or "sync" in sync_stage
    or "gossip" in sync_stage
    or "recover" in level
)

last_height = int(state.get("last_height") or 0)
last_height_at = int(state.get("last_height_at") or now)
height_changed = bool(height and height != last_height)
if height and height != last_height:
    last_height_at = now
same_height_for = max(0, now - last_height_at)
stale_height = same_height_for >= stuck_seconds and age >= stuck_seconds
stale_sync = same_height_for >= sync_stuck_seconds and age >= stuck_seconds

reason = ""
if active_sync and not stale_sync:
    reason = ""
elif active_sync and stale_sync:
    reason = f"sync_stuck_{same_height_for}s"
elif mode in ("HALTED", "PARTITION", "ATTACK") and stale_height:
    reason = f"mode_{mode.lower()}"
elif lag >= lag_restart_threshold and stale_height:
    reason = f"lag_{lag}"
elif mode == "EMERGENCY" and lag >= emergency_lag_threshold and stale_height:
    reason = f"emergency_lag_{lag}"
elif stale_height:
    reason = f"stuck_{same_height_for}s"

bad_samples = int(state.get("bad_samples") or 0)
if reason:
    bad_samples += 1
else:
    bad_samples = 0

last_restart = int(state.get("last_restart") or 0)
state.update({
    "last_checked": now,
    "last_height": height,
    "last_height_at": last_height_at,
    "last_mode": mode,
    "last_health": health,
    "last_lag": lag,
    "last_age": age,
    "last_reason": reason,
    "bad_samples": bad_samples,
})

should_restart = bool(reason and bad_samples >= bad_sample_threshold and now - last_restart >= restart_cooldown)
if should_restart:
    log(f"restart service={service} reason={reason} samples={bad_samples} height={height} best={best} lag={lag} mode={mode} sync_complete={sync_complete}")
    subprocess.run(["systemctl", "restart", service], check=False)
    state["last_restart"] = now
    state["bad_samples"] = 0
else:
    progress = "advanced" if height_changed else f"same_for_{same_height_for}s"
    sync_note = " active_sync=true" if active_sync else ""
    log(f"observe height={height} best={best} lag={lag} age={age}s mode={mode or 'UNKNOWN'} health={health or 'unknown'} progress={progress}{sync_note} reason={reason or 'healthy'} samples={bad_samples}")

save_state(state)
PY
sudo chmod 0755 /usr/local/lib/msc/validator_autoheal_watchdog.py

sudo tee /etc/systemd/system/msc-validator-autoheal-watchdog.service >/dev/null <<SERVICE
[Unit]
Description=MSC validator soft auto-heal watchdog
After=network-online.target ${SERVICE_NAME}
Wants=network-online.target

[Service]
Type=oneshot
Environment=MSC_NODE_SERVICE=${SERVICE_NAME}
Environment=MSC_STATUS_URL=${RPC_URL}
Environment=MSC_AUTOHEAL_STUCK_SECONDS=${STUCK_SECONDS}
Environment=MSC_AUTOHEAL_LAG_RESTART_THRESHOLD=${LAG_RESTART_THRESHOLD}
Environment=MSC_AUTOHEAL_EMERGENCY_LAG_THRESHOLD=${EMERGENCY_LAG_THRESHOLD}
Environment=MSC_AUTOHEAL_BAD_SAMPLE_THRESHOLD=${BAD_SAMPLE_THRESHOLD}
Environment=MSC_AUTOHEAL_RESTART_COOLDOWN_SECONDS=${RESTART_COOLDOWN_SECONDS}
Environment=MSC_AUTOHEAL_STARTUP_GRACE_SECONDS=${STARTUP_GRACE_SECONDS}
Environment=MSC_AUTOHEAL_SYNC_STUCK_SECONDS=${SYNC_STUCK_SECONDS}
ExecStart=/usr/local/lib/msc/validator_autoheal_watchdog.py
SERVICE

sudo tee /etc/systemd/system/msc-validator-autoheal-watchdog.timer >/dev/null <<TIMER
[Unit]
Description=Run MSC validator soft auto-heal watchdog

[Timer]
OnBootSec=2min
OnUnitActiveSec=${INTERVAL_SECONDS}
AccuracySec=10s
Persistent=true

[Install]
WantedBy=timers.target
TIMER

sudo systemctl daemon-reload
sudo systemctl enable --now msc-validator-autoheal-watchdog.timer >/dev/null
sudo systemctl start msc-validator-autoheal-watchdog.service || true
sudo systemctl status msc-validator-autoheal-watchdog.timer --no-pager -l | sed -n '1,12p'
