#!/usr/bin/env bash
set -euo pipefail

NODE_ID=""
DATA_DIR=""
BINARY=""
MIRROR_URL=""
MIRROR_DIR=""
LATEST_PATH=""
WORK_DIR=""
CONFIG_DIR=""
SERVICE=""
APPLY=1
RESTART_SERVICE=0
TRUSTED_PUBLIC_KEY="${MSC_SNAPSHOT_PUBLIC_KEY:-}"
REQUIRE_SIGNATURE="${MSC_SNAPSHOT_REQUIRE_SIGNATURE:-0}"
SKIP_IF_LOCAL_NEWER=1

usage() {
  cat <<'USAGE'
Usage: bootstrap_msc_snapshot.sh --id NODE --datadir DATA [source] [options]

Downloads or reads the latest MSC snapshot mirror manifest, verifies the
artifact hash, verifies the native MSC recovery backup, and imports/applies the
snapshot. It never copies validator.sec, MPC shares, p2p_identity.key, wallets,
or other node identity material.

Sources:
  --mirror-url URL       HTTP(S) base URL or direct latest.json URL.
  --mirror-dir DIR       Local static mirror directory containing latest.json.
  --latest PATH          Local latest.json path.

Options:
  --id NODE              Target node ID.
  --datadir DATA         Target base data directory.
  --binary PATH          msc-node binary path. Default: ./msc-node.
  --config-dir DIR       Directory containing config.toml/genesis.json.
  --service NAME         Stop this systemd service before import.
  --restart-service      Restart --service after import.
  --trusted-public-key PATH
                         Verify latest.json.sig with this OpenSSL public key.
  --snapshot-public-key PATH
                         Alias for --trusted-public-key.
  --require-signature    Fail if latest.json.sig cannot be verified.
  --no-apply             Store snapshot but do not apply it.
  --no-local-height-skip Re-import even if local bootstrap marker/blocks are newer.
  --work-dir DIR         Reuse/create a work directory for downloads.
  -h, --help             Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --datadir) DATA_DIR="${2:?--datadir requires value}"; shift 2 ;;
    --binary) BINARY="${2:?--binary requires value}"; shift 2 ;;
    --mirror-url|--snapshot-url) MIRROR_URL="${2:?--mirror-url requires value}"; shift 2 ;;
    --mirror-dir|--snapshot-mirror) MIRROR_DIR="${2:?--mirror-dir requires value}"; shift 2 ;;
    --latest) LATEST_PATH="${2:?--latest requires value}"; shift 2 ;;
    --config-dir) CONFIG_DIR="${2:?--config-dir requires value}"; shift 2 ;;
    --service) SERVICE="${2:?--service requires value}"; shift 2 ;;
    --restart-service) RESTART_SERVICE=1; shift ;;
    --trusted-public-key|--snapshot-public-key) TRUSTED_PUBLIC_KEY="${2:?--trusted-public-key requires value}"; shift 2 ;;
    --require-signature|--require-snapshot-signature) REQUIRE_SIGNATURE=1; shift ;;
    --no-apply) APPLY=0; shift ;;
    --no-local-height-skip) SKIP_IF_LOCAL_NEWER=0; shift ;;
    --work-dir) WORK_DIR="${2:?--work-dir requires value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required in PATH" >&2; exit 1; }
}

need python3
need tar
need sha256sum
if [[ -n "$TRUSTED_PUBLIC_KEY" || "$REQUIRE_SIGNATURE" == "1" ]]; then
  need openssl
fi

if [[ -z "$NODE_ID" || -z "$DATA_DIR" ]]; then
  usage >&2
  exit 2
fi
sources=0
[[ -n "$MIRROR_URL" ]] && sources=$((sources + 1))
[[ -n "$MIRROR_DIR" ]] && sources=$((sources + 1))
[[ -n "$LATEST_PATH" ]] && sources=$((sources + 1))
if [[ "$sources" -ne 1 ]]; then
  echo "exactly one source is required: --mirror-url, --mirror-dir, or --latest" >&2
  exit 2
fi
if [[ -z "$BINARY" ]]; then
  BINARY="./msc-node"
fi
if [[ ! -x "$BINARY" ]]; then
  echo "binary is not executable: $BINARY" >&2
  exit 1
fi
BINARY="$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")"

NODE_ID="$(printf '%s' "$NODE_ID" | tr '[:lower:]' '[:upper:]')"
DATA_DIR="$(mkdir -p "$DATA_DIR" && cd "$DATA_DIR" && pwd)"
if [[ -z "$CONFIG_DIR" ]]; then
  CONFIG_DIR="$(pwd)"
fi
CONFIG_DIR="$(cd "$CONFIG_DIR" && pwd)"

cleanup_work=0
if [[ -z "$WORK_DIR" ]]; then
  WORK_DIR="$(mktemp -d)"
  cleanup_work=1
else
  mkdir -p "$WORK_DIR"
  WORK_DIR="$(cd "$WORK_DIR" && pwd)"
fi
cleanup() {
  if [[ "$cleanup_work" == "1" ]]; then
    rm -rf "$WORK_DIR"
  fi
}
trap cleanup EXIT

latest="$WORK_DIR/latest.json"
latest_sig="$WORK_DIR/latest.json.sig"
base_url=""
if [[ -n "$MIRROR_URL" ]]; then
  need curl
  if [[ "$MIRROR_URL" == *.json ]]; then
    latest_url="$MIRROR_URL"
    base_url="${MIRROR_URL%/*}/"
  else
    latest_url="${MIRROR_URL%/}/latest.json"
    base_url="${MIRROR_URL%/}/"
  fi
  curl -fsSL "$latest_url" -o "$latest"
  curl -fsSL "${base_url}latest.json.sig" -o "$latest_sig" 2>/dev/null || true
elif [[ -n "$MIRROR_DIR" ]]; then
  cp "$MIRROR_DIR/latest.json" "$latest"
  if [[ -f "$MIRROR_DIR/latest.json.sig" ]]; then
    cp "$MIRROR_DIR/latest.json.sig" "$latest_sig"
  fi
else
  cp "$LATEST_PATH" "$latest"
  latest_dir="$(cd "$(dirname "$LATEST_PATH")" && pwd)"
  if [[ -f "$latest_dir/latest.json.sig" ]]; then
    cp "$latest_dir/latest.json.sig" "$latest_sig"
  fi
fi

signature_verified=0
if [[ -n "$TRUSTED_PUBLIC_KEY" || "$REQUIRE_SIGNATURE" == "1" ]]; then
  [[ -n "$TRUSTED_PUBLIC_KEY" ]] || { echo "--trusted-public-key is required when signature verification is required" >&2; exit 1; }
  [[ -f "$TRUSTED_PUBLIC_KEY" ]] || { echo "snapshot public key not found: $TRUSTED_PUBLIC_KEY" >&2; exit 1; }
  [[ -s "$latest_sig" ]] || { echo "snapshot signature missing: latest.json.sig" >&2; exit 1; }
  openssl dgst -sha256 -verify "$TRUSTED_PUBLIC_KEY" -signature "$latest_sig" "$latest" >/dev/null
  signature_verified=1
fi

artifact="$(python3 - "$latest" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8-sig"))
print((m.get("artifact") or "").strip())
PY
)"
artifact_sha="$(python3 - "$latest" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8-sig"))
print((m.get("artifact_sha256") or "").strip().lower())
PY
)"
height="$(python3 - "$latest" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8-sig"))
print(int(m.get("height") or 0))
PY
)"
if [[ -z "$artifact" || -z "$artifact_sha" || "$height" == "0" ]]; then
  echo "invalid latest.json" >&2
  exit 1
fi

local_height="$(python3 - "$DATA_DIR" "$NODE_ID" <<'PY'
import json, pathlib, re, sys
data_dir = pathlib.Path(sys.argv[1])
node_id = sys.argv[2]
heights = []
for marker in (data_dir / "bootstrap_snapshot_latest.json", data_dir / f"node_{node_id}" / "bootstrap_snapshot_latest.json"):
    try:
        with marker.open(encoding="utf-8-sig") as fh:
            heights.append(int((json.load(fh).get("height") or 0)))
    except Exception:
        pass
block_dir = data_dir / f"node_{node_id}" / "blocks"
if block_dir.exists():
    for path in block_dir.glob("block_*"):
        m = re.search(r"block_0*([0-9]+)", path.name)
        if m:
            try:
                heights.append(int(m.group(1)))
            except ValueError:
                pass
print(max(heights) if heights else 0)
PY
)"
if [[ "$SKIP_IF_LOCAL_NEWER" == "1" && "$local_height" =~ ^[0-9]+$ && "$local_height" -ge "$height" ]]; then
  mkdir -p "$DATA_DIR"
  cp "$latest" "$DATA_DIR/bootstrap_snapshot_latest.json"
  python3 - "$latest" "$DATA_DIR" "$NODE_ID" "$local_height" "$signature_verified" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8-sig"))
print(json.dumps({
    "command": "snapshot bootstrap",
    "node_id": sys.argv[3],
    "height": m.get("height"),
    "local_height": int(sys.argv[4]),
    "snapshot_hash": m.get("snapshot_hash"),
    "artifact": m.get("artifact"),
    "datadir": sys.argv[2],
    "signature_verified": sys.argv[5] == "1",
    "skipped": "local_height_is_current_or_newer",
}, indent=2, sort_keys=True))
PY
  exit 0
fi

artifact_path="$WORK_DIR/$(basename "$artifact")"
if [[ -n "$MIRROR_URL" ]]; then
  artifact_url="$(python3 - "$base_url" "$artifact" <<'PY'
import sys, urllib.parse
print(urllib.parse.urljoin(sys.argv[1], sys.argv[2]))
PY
)"
  curl -fsSL "$artifact_url" -o "$artifact_path"
elif [[ -n "$MIRROR_DIR" ]]; then
  cp "$MIRROR_DIR/$artifact" "$artifact_path"
else
  latest_dir="$(cd "$(dirname "$LATEST_PATH")" && pwd)"
  cp "$latest_dir/$artifact" "$artifact_path"
fi

actual_sha="$(sha256sum "$artifact_path" | awk '{print tolower($1)}')"
if [[ "$actual_sha" != "$artifact_sha" ]]; then
  echo "snapshot artifact checksum mismatch: got=$actual_sha want=$artifact_sha" >&2
  exit 1
fi

python3 - "$artifact_path" <<'PY'
import sys, tarfile
path = sys.argv[1]
forbidden = (
    "validator.sec",
    "p2p_identity",
    "mpc",
    "share.pass",
    "wallet",
    "secure-backups",
    "private",
    "secret",
    ".pem",
    ".key",
)
with tarfile.open(path, "r:gz") as tf:
    for member in tf.getmembers():
        name = member.name.replace("\\", "/")
        parts = [part for part in name.split("/") if part]
        low = name.lower()
        if name.startswith("/") or ".." in parts:
            raise SystemExit(f"unsafe snapshot artifact path: {member.name}")
        if member.issym() or member.islnk():
            raise SystemExit(f"snapshot artifact contains link: {member.name}")
        if any(token in low for token in forbidden):
            raise SystemExit(f"snapshot artifact contains identity material: {member.name}")
PY

extract_dir="$WORK_DIR/extract"
mkdir -p "$extract_dir"
tar -xzf "$artifact_path" -C "$extract_dir"
backup_dir="$(find "$extract_dir" -maxdepth 1 -type d -name 'backup_*' | head -1)"
if [[ -z "$backup_dir" || ! -d "$backup_dir" ]]; then
  echo "snapshot artifact did not contain backup_* directory" >&2
  exit 1
fi

if [[ -n "$SERVICE" ]]; then
  sudo systemctl stop "$SERVICE" || true
fi

pushd "$CONFIG_DIR" >/dev/null
"$BINARY" snapshot verify --path "$backup_dir" >/dev/null
if [[ "$APPLY" == "1" ]]; then
  "$BINARY" snapshot import --id "$NODE_ID" --datadir "$DATA_DIR" --path "$backup_dir" --apply
else
  "$BINARY" snapshot import --id "$NODE_ID" --datadir "$DATA_DIR" --path "$backup_dir" --apply=false
fi
popd >/dev/null

mkdir -p "$DATA_DIR"
cp "$latest" "$DATA_DIR/bootstrap_snapshot_latest.json"

if [[ -n "$SERVICE" && "$RESTART_SERVICE" == "1" ]]; then
  sudo systemctl start "$SERVICE"
fi

python3 - "$latest" "$DATA_DIR" "$NODE_ID" "$APPLY" "$signature_verified" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8-sig"))
print(json.dumps({
    "command": "snapshot bootstrap",
    "node_id": sys.argv[3],
    "height": m.get("height"),
    "snapshot_hash": m.get("snapshot_hash"),
    "artifact": m.get("artifact"),
    "datadir": sys.argv[2],
    "applied": sys.argv[4] == "1",
    "signature_verified": sys.argv[5] == "1",
}, indent=2, sort_keys=True))
PY
