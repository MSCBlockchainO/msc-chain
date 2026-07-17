#!/usr/bin/env bash
set -euo pipefail

ROLE="candidate"
NODE_TYPE=""
NODE_ID=""
RPC="127.0.0.1:26657"
PEERS=""
PERSISTENT_PEERS=""
LOW_RAM=0
AUTO_START=0
PUBLIC_GATEWAY=0
ALLOW_4GB_VALIDATOR=0
INSTALL_DIR=""
P2P_PORT=7001
P2P_EXTERNAL="${MSC_P2P_EXTERNAL:-}"
PREBUILT_BINARY=""
RELEASE_URL=""
RELEASE_SHA256=""
SNAPSHOT_URL=""
SNAPSHOT_MIRROR_DIR=""
SNAPSHOT_REQUIRED=0
SNAPSHOT_PUBLIC_KEY=""
SNAPSHOT_REQUIRE_SIGNATURE=0
SNAPSHOT_BOOTSTRAP_ON_START=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --role) ROLE="${2:?--role requires value}"; shift 2 ;;
    --node-type) NODE_TYPE="${2:?--node-type requires value}"; shift 2 ;;
    --id|--node-id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --rpc) RPC="${2:?--rpc requires value}"; shift 2 ;;
    --peers) PEERS="${2:?--peers requires value}"; shift 2 ;;
    --bootnodes) PEERS="${2:?--bootnodes requires value}"; shift 2 ;;
    --persistent-peers) PERSISTENT_PEERS="${2:?--persistent-peers requires value}"; shift 2 ;;
    --low-ram) LOW_RAM=1; shift ;;
    --auto-start) AUTO_START=1; shift ;;
    --public-gateway) PUBLIC_GATEWAY=1; shift ;;
    --allow-4gb-validator) ALLOW_4GB_VALIDATOR=1; shift ;;
    --install-dir) INSTALL_DIR="${2:?--install-dir requires value}"; shift 2 ;;
    --p2p-port) P2P_PORT="${2:?--p2p-port requires value}"; shift 2 ;;
    --p2p-external) P2P_EXTERNAL="${2:?--p2p-external requires value}"; shift 2 ;;
    --prebuilt-binary) PREBUILT_BINARY="${2:?--prebuilt-binary requires value}"; shift 2 ;;
    --release-url) RELEASE_URL="${2:?--release-url requires value}"; shift 2 ;;
    --release-sha256) RELEASE_SHA256="${2:?--release-sha256 requires value}"; shift 2 ;;
    --snapshot-url) SNAPSHOT_URL="${2:?--snapshot-url requires value}"; shift 2 ;;
    --snapshot-mirror-dir) SNAPSHOT_MIRROR_DIR="${2:?--snapshot-mirror-dir requires value}"; shift 2 ;;
    --snapshot-required) SNAPSHOT_REQUIRED=1; shift ;;
    --snapshot-public-key|--trusted-snapshot-public-key) SNAPSHOT_PUBLIC_KEY="${2:?--snapshot-public-key requires value}"; shift 2 ;;
    --require-snapshot-signature|--require-signature) SNAPSHOT_REQUIRE_SIGNATURE=1; shift ;;
    --no-snapshot-on-start) SNAPSHOT_BOOTSTRAP_ON_START=0; shift ;;
    -h|--help)
      echo "Usage: $0 --node-type full|public-rpc|candidate-validator|private-validator|archive --id NODE [--release-url URL --release-sha256 HEX] [--snapshot-url URL|--snapshot-mirror-dir DIR] [--snapshot-public-key PATH --require-snapshot-signature] [--bootnodes PEERS] [--p2p-port PORT --p2p-external MULTIADDR] [--auto-start]"
      echo "Legacy: $0 --role full|candidate|validator --id NODE [--low-ram] [--auto-start]"
      exit 0
      ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [[ -n "$NODE_TYPE" ]]; then
  case "$NODE_TYPE" in
    full)
      ROLE="full"
      ;;
    public-rpc)
      ROLE="full"
      PUBLIC_GATEWAY=1
      ;;
    candidate-validator)
      ROLE="candidate"
      ;;
    private-validator)
      ROLE="validator"
      ;;
    archive)
      ROLE="full"
      ;;
    *) echo "--node-type must be full, public-rpc, candidate-validator, private-validator, or archive" >&2; exit 2 ;;
  esac
fi

case "$ROLE" in
  full|candidate|validator) ;;
  *) echo "--role must be full, candidate, or validator" >&2; exit 2 ;;
esac
if [[ -z "$NODE_TYPE" ]]; then
  if [[ "$ROLE" == "validator" ]]; then
    NODE_TYPE="private-validator"
  elif [[ "$ROLE" == "candidate" ]]; then
    NODE_TYPE="candidate-validator"
  elif [[ "$PUBLIC_GATEWAY" == "1" ]]; then
    NODE_TYPE="public-rpc"
  else
    NODE_TYPE="full"
  fi
fi

normalize_node_id() {
  local clean
  clean="$(printf '%s' "$1" | tr -cd '[:alnum:]_-')"
  case "$(printf '%s' "$clean" | tr '[:upper:]' '[:lower:]')" in
    msc_*) printf '%s\n' "$clean" ;;
    *) printf '%s\n' "$clean" | tr '[:lower:]' '[:upper:]' ;;
  esac
}

generate_node_id() {
  python3 - <<'PY'
import secrets
alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
print("msc_" + "".join(secrets.choice(alphabet) for _ in range(30)))
PY
}

if [[ -n "$RELEASE_URL" && -z "$RELEASE_SHA256" ]]; then
  echo "--release-sha256 is required with --release-url" >&2
  exit 2
fi

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required in PATH" >&2; exit 1; }
}

if [[ -z "$PREBUILT_BINARY" && -z "$RELEASE_URL" ]]; then
  need go
fi
if [[ -n "$RELEASE_URL" ]]; then
  need curl
  need tar
fi
if [[ -n "$SNAPSHOT_URL" || -n "$SNAPSHOT_MIRROR_DIR" ]]; then
  need tar
  if [[ -n "$SNAPSHOT_URL" ]]; then
    need curl
  fi
  if [[ -n "$SNAPSHOT_PUBLIC_KEY" || "$SNAPSHOT_REQUIRE_SIGNATURE" == "1" ]]; then
    need openssl
  fi
fi
need sha256sum
need python3

if [[ -z "$NODE_ID" ]]; then
  NODE_ID="$(generate_node_id)"
  echo "generated node id: $NODE_ID"
else
  NODE_ID="$(normalize_node_id "$NODE_ID")"
fi

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

detect_public_ipv4() {
  command -v curl >/dev/null 2>&1 || return 1
  curl -fsS --max-time 1 http://169.254.169.254/latest/meta-data/public-ipv4 2>/dev/null ||
    curl -fsS --max-time 3 https://api.ipify.org 2>/dev/null
}

if [[ -z "$P2P_EXTERNAL" ]]; then
  detected_public_ip="$(detect_public_ipv4 || true)"
  if [[ "$detected_public_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    P2P_EXTERNAL="/ip4/$detected_public_ip/tcp/$P2P_PORT"
  fi
fi

if [[ -n "$SNAPSHOT_PUBLIC_KEY" ]]; then
  [[ -f "$SNAPSHOT_PUBLIC_KEY" ]] || { echo "snapshot public key not found: $SNAPSHOT_PUBLIC_KEY" >&2; exit 1; }
  SNAPSHOT_PUBLIC_KEY="$(cd "$(dirname "$SNAPSHOT_PUBLIC_KEY")" && pwd)/$(basename "$SNAPSHOT_PUBLIC_KEY")"
fi
DATA_DIR="$INSTALL_DIR/data"
LOG_DIR="$INSTALL_DIR/logs"
CONFIG_PATH="$INSTALL_DIR/config.toml"
ENV_PATH="$INSTALL_DIR/node.env"
BINARY_PATH="$INSTALL_DIR/msc-node"
ALIAS_PATH="$INSTALL_DIR/msc"
START_PATH="$INSTALL_DIR/start.sh"
BOOTSTRAP_SCRIPT_PATH="$INSTALL_DIR/bootstrap_msc_snapshot.sh"
NODE_PATH="$DATA_DIR/node_$NODE_ID"
MANIFEST_PATH="$INSTALL_DIR/install_manifest.json"

mkdir -p "$DATA_DIR" "$LOG_DIR"
chmod 700 "$INSTALL_DIR" "$DATA_DIR" "$LOG_DIR"
if [[ ! -f "$CONFIG_PATH" ]]; then
  cp config.toml "$CONFIG_PATH"
fi
cp genesis.json "$INSTALL_DIR/genesis.json"

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

toml_array_from_csv() {
  python3 - "$1" <<'PY'
import json, sys
items = [item.strip() for item in sys.argv[1].split(",") if item.strip()]
print("[" + ", ".join(json.dumps(item) for item in items) + "]")
PY
}

HISTORY_PROFILE="full"
STATE_PRUNING="true"
case "$NODE_TYPE" in
  private-validator)
    HISTORY_PROFILE="validator"
    STATE_PRUNING="true"
    ;;
  archive)
    HISTORY_PROFILE="archive"
    STATE_PRUNING="false"
    ;;
esac

toml_set rpc laddr "\"$RPC\""
toml_set storage history_profile "\"$HISTORY_PROFILE\""
toml_set storage state_pruning_enabled "$STATE_PRUNING"
if [[ -n "$PEERS" ]]; then
  toml_set p2p seeds "$(toml_array_from_csv "$PEERS")"
fi
if [[ -z "$PERSISTENT_PEERS" ]]; then
  PERSISTENT_PEERS="$PEERS"
fi
if [[ -n "$PERSISTENT_PEERS" ]]; then
  toml_set p2p persistent_peers "$(toml_array_from_csv "$PERSISTENT_PEERS")"
fi
if [[ "$LOW_RAM" == "1" ]]; then
  toml_set sync delta_replay_verify_workers "2"
  toml_set sync snapshot_parallel_chunks "2"
  toml_set storage parallel_gc_workers "1"
  toml_set rpc max_concurrent_requests "64"
fi

release_tmp=""
cleanup_release_tmp() {
  if [[ -n "$release_tmp" && -d "$release_tmp" ]]; then
    rm -rf "$release_tmp"
  fi
}
trap cleanup_release_tmp EXIT

if [[ -n "$RELEASE_URL" ]]; then
  release_tmp="$(mktemp -d)"
  release_artifact="$release_tmp/msc-release"
  echo "Downloading MSC release: $RELEASE_URL"
  curl -fL "$RELEASE_URL" -o "$release_artifact"
  echo "$RELEASE_SHA256  $release_artifact" | sha256sum -c -
  if tar -tzf "$release_artifact" >/dev/null 2>&1; then
    tar -xzf "$release_artifact" -C "$release_tmp"
    resolved="$(find "$release_tmp" -type f \( -name 'msc-node' -o -name 'msc' -o -name 'msc-node.linux' -o -name 'msc-node*.linux' \) | head -1)"
    if [[ -z "$resolved" ]]; then
      echo "release archive verified, but no msc-node binary was found" >&2
      exit 1
    fi
    PREBUILT_BINARY="$resolved"
  else
    PREBUILT_BINARY="$release_artifact"
  fi
elif [[ -n "$PREBUILT_BINARY" && -n "$RELEASE_SHA256" ]]; then
  echo "$RELEASE_SHA256  $PREBUILT_BINARY" | sha256sum -c -
fi

if [[ -n "$PREBUILT_BINARY" ]]; then
  cp "$PREBUILT_BINARY" "$BINARY_PATH"
else
  go build -o "$BINARY_PATH" .
fi
chmod +x "$BINARY_PATH"
cp "$BINARY_PATH" "$ALIAS_PATH"
chmod +x "$ALIAS_PATH"
if [[ -f "$SCRIPT_DIR/bootstrap_msc_snapshot.sh" ]]; then
  cp "$SCRIPT_DIR/bootstrap_msc_snapshot.sh" "$BOOTSTRAP_SCRIPT_PATH"
  chmod +x "$BOOTSTRAP_SCRIPT_PATH"
fi
if [[ -n "$SNAPSHOT_URL" || -n "$SNAPSHOT_MIRROR_DIR" ]]; then
  [[ -x "$BOOTSTRAP_SCRIPT_PATH" ]] || { echo "snapshot bootstrap script not found: $BOOTSTRAP_SCRIPT_PATH" >&2; exit 1; }
fi

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
cd "$INSTALL_DIR"
peer_args=()
if [[ -n "$PEERS" ]]; then
  peer_args=(--peers "$PEERS")
fi
p2p_external_args=()
if [[ -n "$P2P_EXTERNAL" ]]; then
  p2p_external_args=(--p2p-external "$P2P_EXTERNAL")
fi
snapshot_source_args=()
if [[ -n "$SNAPSHOT_URL" ]]; then
  snapshot_source_args=(--mirror-url "$SNAPSHOT_URL")
elif [[ -n "$SNAPSHOT_MIRROR_DIR" ]]; then
  snapshot_source_args=(--mirror-dir "$SNAPSHOT_MIRROR_DIR")
fi
if [[ "\${MSC_SNAPSHOT_BOOTSTRAP_ON_START:-$SNAPSHOT_BOOTSTRAP_ON_START}" == "1" && "\${#snapshot_source_args[@]}" -gt 0 ]]; then
  snapshot_bootstrap_args=(
    --id "$NODE_ID"
    --datadir "$DATA_DIR"
    --binary "$BINARY_PATH"
    --config-dir "$INSTALL_DIR"
    "\${snapshot_source_args[@]}"
  )
  if [[ -n "$SNAPSHOT_PUBLIC_KEY" ]]; then
    snapshot_bootstrap_args+=(--trusted-public-key "$SNAPSHOT_PUBLIC_KEY")
  fi
  if [[ "$SNAPSHOT_REQUIRE_SIGNATURE" == "1" ]]; then
    snapshot_bootstrap_args+=(--require-signature)
  fi
  if ! bash "$BOOTSTRAP_SCRIPT_PATH" "\${snapshot_bootstrap_args[@]}"; then
    if [[ "$SNAPSHOT_REQUIRED" == "1" ]]; then
      echo "snapshot bootstrap failed before node start" >&2
      exit 1
    fi
    echo "warning: snapshot bootstrap failed before node start; continuing with P2P sync" >&2
  fi
fi
exec "$BINARY_PATH" --mode=full --role="$RUNTIME_ROLE" --id="$NODE_ID" --port="$P2P_PORT" --datadir="$DATA_DIR" --rpcaddr="$RPC" --config="config.toml" "\${p2p_external_args[@]}" "\${peer_args[@]}"
SH
chmod +x "$START_PATH"

if [[ "$ROLE" == "validator" && "$LOW_RAM" == "1" && "$HOST_MEM_MIB" -eq 0 && "$ALLOW_4GB_VALIDATOR" != "1" ]]; then
  echo "warning: low-RAM validator mode is 8GB recommended. Host memory detection was unavailable; use --allow-4gb-validator only for test/candidate rehearsal." >&2
fi

if [[ -n "$SNAPSHOT_URL" || -n "$SNAPSHOT_MIRROR_DIR" ]]; then
  bootstrap_args=(
    --id "$NODE_ID"
    --datadir "$DATA_DIR"
    --binary "$BINARY_PATH"
    --config-dir "$INSTALL_DIR"
  )
  if [[ -n "$SNAPSHOT_URL" ]]; then
    bootstrap_args+=(--mirror-url "$SNAPSHOT_URL")
  fi
  if [[ -n "$SNAPSHOT_MIRROR_DIR" ]]; then
    bootstrap_args+=(--mirror-dir "$SNAPSHOT_MIRROR_DIR")
  fi
  if [[ -n "$SNAPSHOT_PUBLIC_KEY" ]]; then
    bootstrap_args+=(--trusted-public-key "$SNAPSHOT_PUBLIC_KEY")
  fi
  if [[ "$SNAPSHOT_REQUIRE_SIGNATURE" == "1" ]]; then
    bootstrap_args+=(--require-signature)
  fi
  if ! bash "$BOOTSTRAP_SCRIPT_PATH" "${bootstrap_args[@]}"; then
    if [[ "$SNAPSHOT_REQUIRED" == "1" ]]; then
      echo "snapshot bootstrap failed and --snapshot-required was set" >&2
      exit 1
    fi
    echo "warning: snapshot bootstrap failed; node will fall back to P2P sync" >&2
  fi
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

python3 - "$MANIFEST_PATH" "$NODE_ID" "$ROLE" "$NODE_TYPE" "$INSTALL_DIR" "$DATA_DIR" "$NODE_PATH" "$CONFIG_PATH" "$BINARY_PATH" "$ALIAS_PATH" "$GENESIS_HASH" "$PREBUILT_BINARY" "$P2P_EXTERNAL" "$SNAPSHOT_URL" "$SNAPSHOT_MIRROR_DIR" "$SNAPSHOT_PUBLIC_KEY" "$SNAPSHOT_REQUIRED" "$SNAPSHOT_REQUIRE_SIGNATURE" "$SNAPSHOT_BOOTSTRAP_ON_START" <<'PY'
import json, os, platform, sys, time
path, node_id, role, node_type, install_dir, data_dir, node_path, config_path, binary_path, alias_path, genesis_hash, prebuilt, p2p_external, snapshot_url, snapshot_mirror_dir, snapshot_public_key, snapshot_required, snapshot_require_signature, snapshot_bootstrap_on_start = sys.argv[1:]
def read_trim(p):
    try:
        return open(p, encoding="utf-8").read().strip()
    except OSError:
        return ""
manifest = {
    "schema_version": 1,
    "node_id": node_id,
    "role": role,
    "node_type": node_type,
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
    "p2p_external": p2p_external,
    "snapshot_url": snapshot_url,
    "snapshot_mirror_dir": snapshot_mirror_dir,
    "snapshot_public_key": snapshot_public_key,
    "snapshot_required": snapshot_required == "1",
    "snapshot_require_signature": snapshot_require_signature == "1",
    "snapshot_bootstrap_on_start": snapshot_bootstrap_on_start == "1",
    "updated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")
os.chmod(path, 0o600)
PY

echo "MSC node installed."
echo "Node ID: $NODE_ID"
echo "Node Type: $NODE_TYPE"
echo "Role: $ROLE runtime=$RUNTIME_ROLE profile=$PROFILE history=$HISTORY_PROFILE"
echo "Install: $INSTALL_DIR"
echo "Binary: $BINARY_PATH"
echo "Alias: $ALIAS_PATH"
echo "Manifest: $MANIFEST_PATH"
echo "Start: $START_PATH"
[[ -n "$P2P_EXTERNAL" ]] && echo "P2P External: $P2P_EXTERNAL"
echo "Status URL: http://$RPC/status"
echo "Health URL: http://$RPC/healthz"
if [[ "$NODE_TYPE" == "private-validator" ]]; then
  echo "Validator RPC is private by default. Keep $RPC behind localhost/VPN."
elif [[ "$NODE_TYPE" == "public-rpc" ]]; then
  echo "Public RPC node installed. Put nginx/TLS/rate limits in front of the local RPC before exposing it."
elif [[ "$NODE_TYPE" == "candidate-validator" ]]; then
  echo "Candidate validator installed. Sync fully, then submit validator stake with the validator public key."
elif [[ "$NODE_TYPE" == "archive" ]]; then
  echo "Archive node installed with pruning disabled."
fi
