#!/usr/bin/env bash
set -euo pipefail

ROLE="candidate"
NODE_ID=""
RPC="127.0.0.1:26657"
PEERS=""
LOW_RAM=0
AUTO_START=0
PUBLIC_GATEWAY=0
ALLOW_4GB_VALIDATOR=0
INSTALL_DIR=""
P2P_PORT=7001
PREBUILT_BINARY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --role) ROLE="${2:?--role requires value}"; shift 2 ;;
    --id|--node-id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --rpc) RPC="${2:?--rpc requires value}"; shift 2 ;;
    --peers) PEERS="${2:?--peers requires value}"; shift 2 ;;
    --low-ram) LOW_RAM=1; shift ;;
    --auto-start) AUTO_START=1; shift ;;
    --public-gateway) PUBLIC_GATEWAY=1; shift ;;
    --allow-4gb-validator) ALLOW_4GB_VALIDATOR=1; shift ;;
    --install-dir) INSTALL_DIR="${2:?--install-dir requires value}"; shift 2 ;;
    --p2p-port) P2P_PORT="${2:?--p2p-port requires value}"; shift 2 ;;
    --prebuilt-binary) PREBUILT_BINARY="${2:?--prebuilt-binary requires value}"; shift 2 ;;
    -h|--help)
      echo "Usage: $0 --role full|candidate|validator --id NODE [--low-ram] [--auto-start]"
      exit 0
      ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

case "$ROLE" in
  full|candidate|validator) ;;
  *) echo "--role must be full, candidate, or validator" >&2; exit 2 ;;
esac
if [[ -z "$NODE_ID" ]]; then
  echo "--id is required" >&2
  exit 2
fi

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required in PATH" >&2; exit 1; }
}

if [[ -z "$PREBUILT_BINARY" ]]; then
  need go
fi
need sha256sum
need python3

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_DIR"

[[ -f genesis.json ]] || { echo "genesis.json not found" >&2; exit 1; }
[[ -f config.toml ]] || { echo "config.toml not found" >&2; exit 1; }

GENESIS_HASH="$(sha256sum genesis.json | awk '{print tolower($1)}')"
CONFIG_HASH="$(awk -F\" '/^[[:space:]]*genesis_hash[[:space:]]*=/{print tolower($2); exit}' config.toml)"
if [[ -n "$CONFIG_HASH" && "$CONFIG_HASH" != "$GENESIS_HASH" ]]; then
  echo "genesis hash mismatch: file=$GENESIS_HASH config=$CONFIG_HASH" >&2
  exit 1
fi

NODE_ID="$(printf '%s' "$NODE_ID" | tr '[:lower:]' '[:upper:]')"
HOST_MEM_MIB=0
if [[ -r /proc/meminfo ]]; then
  HOST_MEM_MIB="$(awk '/^MemTotal:/{printf "%d", $2/1024; exit}' /proc/meminfo)"
fi
if [[ "$ROLE" == "validator" && "$LOW_RAM" == "1" && "$HOST_MEM_MIB" -gt 0 && "$HOST_MEM_MIB" -lt 8192 && "$ALLOW_4GB_VALIDATOR" != "1" ]]; then
  echo "validator low-RAM mode requires at least 8GB RAM. This host has ${HOST_MEM_MIB}MiB. Use --allow-4gb-validator only for test/candidate rehearsal." >&2
  exit 1
fi
if [[ -z "$INSTALL_DIR" ]]; then
  INSTALL_DIR="$HOME/.msc/nodes/$NODE_ID"
fi
DATA_DIR="$INSTALL_DIR/data"
LOG_DIR="$INSTALL_DIR/logs"
CONFIG_PATH="$INSTALL_DIR/config.toml"
ENV_PATH="$INSTALL_DIR/node.env"
BINARY_PATH="$INSTALL_DIR/msc-node"
ALIAS_PATH="$INSTALL_DIR/msc"
START_PATH="$INSTALL_DIR/start.sh"
NODE_PATH="$DATA_DIR/node_$NODE_ID"
MANIFEST_PATH="$INSTALL_DIR/install_manifest.json"

mkdir -p "$DATA_DIR" "$LOG_DIR"
chmod 700 "$INSTALL_DIR" "$DATA_DIR" "$LOG_DIR"
if [[ ! -f "$CONFIG_PATH" ]]; then
  cp config.toml "$CONFIG_PATH"
fi

toml_set() {
  local section="$1" key="$2" value="$3"
  python3 - "$CONFIG_PATH" "$section" "$key" "$value" <<'PY'
import re, sys
path, section, key, value = sys.argv[1:]
text = open(path, encoding="utf-8").read()
pat = re.compile(rf"(?ms)(^\[{re.escape(section)}\]\s*.*?)(?=^\[|\Z)")
m = pat.search(text)
line = f"{key} = {value}"
if not m:
    text = text.rstrip() + f"\n\n[{section}]\n{line}\n"
else:
    block = m.group(1)
    kpat = re.compile(rf"(?m)^\s*{re.escape(key)}\s*=.*$")
    if kpat.search(block):
        block = kpat.sub(line, block, count=1)
    else:
        block = block.rstrip() + "\n" + line + "\n"
    text = text[:m.start()] + block + text[m.end():]
open(path, "w", encoding="utf-8").write(text)
PY
}

toml_set rpc laddr "\"$RPC\""
toml_set storage history_profile "\"full\""
toml_set storage state_pruning_enabled "true"
if [[ "$LOW_RAM" == "1" ]]; then
  toml_set sync delta_replay_verify_workers "2"
  toml_set sync snapshot_parallel_chunks "2"
  toml_set storage parallel_gc_workers "1"
  toml_set rpc max_concurrent_requests "64"
fi

if [[ -n "$PREBUILT_BINARY" ]]; then
  cp "$PREBUILT_BINARY" "$BINARY_PATH"
else
  go build -o "$BINARY_PATH" .
fi
chmod +x "$BINARY_PATH"
cp "$BINARY_PATH" "$ALIAS_PATH"
chmod +x "$ALIAS_PATH"

RUNTIME_ROLE="full"
if [[ "$ROLE" == "validator" ]]; then
  RUNTIME_ROLE="validator"
fi
PROFILE="standard"
GOMEM=""
GOMAX=""
if [[ "$LOW_RAM" == "1" ]]; then
  PROFILE="home_low_ram"
  GOMAX="2"
  if [[ "$ROLE" == "validator" ]]; then
    GOMEM="2048MiB"
  else
    GOMEM="1536MiB"
  fi
fi

cat > "$ENV_PATH" <<ENV
export MSC_NODE_PROFILE="$PROFILE"
export MSC_LOW_RAM_MODE="$LOW_RAM"
export MSC_ALLOW_4GB_VALIDATOR="$ALLOW_4GB_VALIDATOR"
export MSC_PUBLIC_GATEWAY="$PUBLIC_GATEWAY"
export MSC_AUTH_OPEN_BROWSER=0
export GOGC=75
ENV
[[ -n "$GOMEM" ]] && echo "export GOMEMLIMIT=\"$GOMEM\"" >> "$ENV_PATH"
[[ -n "$GOMAX" ]] && echo "export GOMAXPROCS=\"$GOMAX\"" >> "$ENV_PATH"
chmod 600 "$ENV_PATH"

cat > "$START_PATH" <<SH
#!/usr/bin/env bash
set -euo pipefail
source "$ENV_PATH"
peer_args=()
if [[ -n "$PEERS" ]]; then
  peer_args=(--peers "$PEERS")
fi
exec "$BINARY_PATH" --mode=full --role="$RUNTIME_ROLE" --id="$NODE_ID" --port="$P2P_PORT" --datadir="$DATA_DIR" --rpcaddr="$RPC" --config="$CONFIG_PATH" "\${peer_args[@]}"
SH
chmod +x "$START_PATH"

if [[ "$ROLE" == "validator" && "$LOW_RAM" == "1" && "$HOST_MEM_MIB" -eq 0 && "$ALLOW_4GB_VALIDATOR" != "1" ]]; then
  echo "warning: low-RAM validator mode is 8GB recommended. Host memory detection was unavailable; use --allow-4gb-validator only for test/candidate rehearsal." >&2
fi

if [[ "$AUTO_START" == "1" ]]; then
  if command -v systemctl >/dev/null 2>&1 && [[ "$(id -u)" -eq 0 || -n "${SUDO_USER:-}" ]]; then
    SERVICE="msc-$NODE_ID.service"
    sudo tee "/etc/systemd/system/$SERVICE" >/dev/null <<SERVICE
[Unit]
Description=MSC Node $NODE_ID
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
ExecStart=$START_PATH
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SERVICE
    sudo systemctl daemon-reload
    sudo systemctl enable --now "$SERVICE"
    echo "systemd service started: $SERVICE"
  else
    nohup "$START_PATH" >"$LOG_DIR/node.out" 2>&1 &
    echo "node started in background pid=$!"
  fi
fi

python3 - "$MANIFEST_PATH" "$NODE_ID" "$ROLE" "$INSTALL_DIR" "$DATA_DIR" "$NODE_PATH" "$CONFIG_PATH" "$BINARY_PATH" "$ALIAS_PATH" "$GENESIS_HASH" "$PREBUILT_BINARY" <<'PY'
import json, os, platform, sys, time
path, node_id, role, install_dir, data_dir, node_path, config_path, binary_path, alias_path, genesis_hash, prebuilt = sys.argv[1:]
def read_trim(p):
    try:
        return open(p, encoding="utf-8").read().strip()
    except OSError:
        return ""
manifest = {
    "schema_version": 1,
    "node_id": node_id,
    "role": role,
    "install_dir": install_dir,
    "data_dir": data_dir,
    "node_path": node_path,
    "config_path": config_path,
    "binary_path": binary_path,
    "alias_path": alias_path,
    "genesis_hash": genesis_hash,
    "validator_pubkey": read_trim(os.path.join(node_path, "validator.pub")),
    "validator_fingerprint": read_trim(os.path.join(node_path, "fingerprint.lock")),
    "service_name": "msc-" + node_id,
    "os": "linux",
    "arch": platform.machine(),
    "source": "release" if prebuilt else "local",
    "updated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")
os.chmod(path, 0o600)
PY

echo "MSC node installed."
echo "Node ID: $NODE_ID"
echo "Role: $ROLE runtime=$RUNTIME_ROLE profile=$PROFILE"
echo "Install: $INSTALL_DIR"
echo "Binary: $BINARY_PATH"
echo "Alias: $ALIAS_PATH"
echo "Manifest: $MANIFEST_PATH"
echo "Start: $START_PATH"
