#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR=""
SERVICE=""
PEERS=""
RESTART=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir) INSTALL_DIR="${2:?--install-dir requires value}"; shift 2 ;;
    --service) SERVICE="${2:?--service requires value}"; shift 2 ;;
    --peers) PEERS="${2:?--peers requires value}"; shift 2 ;;
    --restart) RESTART=1; shift ;;
    -h|--help)
      echo "Usage: $0 --install-dir DIR --service NAME --peers CSV [--restart]"
      exit 0
      ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$INSTALL_DIR" ]] || { echo "--install-dir is required" >&2; exit 2; }
[[ -n "$SERVICE" ]] || { echo "--service is required" >&2; exit 2; }
[[ -n "$PEERS" ]] || { echo "--peers is required" >&2; exit 2; }

CONFIG_PATH="$INSTALL_DIR/config.toml"
ENV_PATH="$INSTALL_DIR/node.env"
START_PATH="$INSTALL_DIR/start.sh"
[[ -f "$CONFIG_PATH" ]] || { echo "config not found: $CONFIG_PATH" >&2; exit 1; }
[[ -f "$ENV_PATH" ]] || { echo "environment not found: $ENV_PATH" >&2; exit 1; }
[[ -f "$START_PATH" ]] || { echo "start script not found: $START_PATH" >&2; exit 1; }

mesh_result="$(python3 - "$CONFIG_PATH" "$ENV_PATH" "$START_PATH" "$PEERS" <<'PY'
import json
import os
import re
import shlex
import sys
import tempfile

config_path, env_path, start_path, raw_peers = sys.argv[1:]

peers = []
seen = set()
peer_re = re.compile(r"^/(?:ip4|dns4)/[^/]+/tcp/\d+/p2p/[A-Za-z0-9]+$")
for raw in raw_peers.split(","):
    peer = raw.strip()
    if not peer:
        continue
    if not peer_re.fullmatch(peer):
        raise SystemExit(f"invalid full peer multiaddr: {peer}")
    if peer not in seen:
        seen.add(peer)
        peers.append(peer)

if not peers:
    raise SystemExit("peer mesh is empty")

peer_csv = ",".join(peers)

def atomic_write(path, text):
    mode = os.stat(path).st_mode & 0o777
    directory = os.path.dirname(path)
    fd, tmp = tempfile.mkstemp(prefix=".msc-peer-mesh-", dir=directory, text=True)
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(text)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)

config_original = open(config_path, encoding="utf-8").read()
config = config_original
section_re = re.compile(r"(?ms)(^\[p2p\]\s*.*?)(?=^\[|\Z)")
match = section_re.search(config)
line = "persistent_peers = " + json.dumps(peers, separators=(",", ":"))
if not match:
    config = config.rstrip() + "\n\n[p2p]\n" + line + "\n"
else:
    block = match.group(1)
    key_re = re.compile(r"(?m)^\s*persistent_peers\s*=.*$")
    if key_re.search(block):
        block = key_re.sub(line, block, count=1)
    else:
        block = block.rstrip() + "\n" + line + "\n"
    config = config[:match.start()] + block + config[match.end():]
changed = config != config_original
if changed:
    atomic_write(config_path, config)

env_original = open(env_path, encoding="utf-8").read()
env_lines = env_original.splitlines()
env_lines = [line for line in env_lines if not line.startswith("export MSC_PERSISTENT_PEERS=")]
env_lines.append("export MSC_PERSISTENT_PEERS=" + shlex.quote(peer_csv))
env = "\n".join(env_lines) + "\n"
if env != env_original:
    changed = True
    atomic_write(env_path, env)

start_original = open(start_path, encoding="utf-8").read()
start = start_original
peer_block_re = re.compile(r"(?ms)^peer_args=\(\)\n.*?^p2p_external_args=\(\)\n")
replacement = '''peer_args=()
peer_list="${MSC_PERSISTENT_PEERS:-}"
if [[ -n "$peer_list" ]]; then
  peer_args=(--peers "$peer_list")
fi
p2p_external_args=()
'''
if not peer_block_re.search(start):
    raise SystemExit(f"peer argument block not found in {start_path}")
start = peer_block_re.sub(lambda _match: replacement, start, count=1)
if start != start_original:
    changed = True
    atomic_write(start_path, start)

print(f"configured_peers={len(peers)} changed={1 if changed else 0}")
PY
)"
echo "$mesh_result"

if [[ "$RESTART" == "1" && "$mesh_result" == *"changed=1"* ]]; then
  sudo systemctl restart "$SERVICE"
  deadline=$((SECONDS + 45))
  until sudo systemctl is-active --quiet "$SERVICE"; do
    if (( SECONDS >= deadline )); then
      sudo systemctl status "$SERVICE" --no-pager || true
      echo "service did not become active: $SERVICE" >&2
      exit 1
    fi
    sleep 1
  done
fi

changed=0
if [[ "$mesh_result" == *"changed=1"* ]]; then
  changed=1
fi
echo "peer mesh applied: service=$SERVICE restart_requested=$RESTART changed=$changed"
