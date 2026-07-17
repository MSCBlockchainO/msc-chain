#!/usr/bin/env bash
set -euo pipefail

RELEASE_BASE_URL="${MSC_RELEASE_BASE_URL:-https://mscblockexplorer.in/releases}"
LATEST_URL="${MSC_LATEST_URL:-${RELEASE_BASE_URL%/}/latest.json}"
INSTALL_PATH="${MSC_INSTALL_PATH:-/usr/local/bin/msc}"
SHARE_DIR="${MSC_SHARE_DIR:-/usr/local/share/msc}"
ROLE="${MSC_NODE_ROLE:-}"
NODE_ID="${MSC_NODE_ID:-}"
AUTO_START="${MSC_AUTO_START:-0}"
BOOTNODES="${MSC_BOOTNODES:-}"
RPC="${MSC_RPC:-}"
P2P_PORT="${MSC_P2P_PORT:-}"
P2P_EXTERNAL="${MSC_P2P_EXTERNAL:-}"
SNAPSHOT_URL="${MSC_SNAPSHOT_URL:-}"
SNAPSHOT_PUBLIC_KEY="${MSC_SNAPSHOT_PUBLIC_KEY:-}"
SNAPSHOT_REQUIRED="${MSC_SNAPSHOT_REQUIRED:-}"
SNAPSHOT_REQUIRE_SIGNATURE="${MSC_SNAPSHOT_REQUIRE_SIGNATURE:-}"
NO_SNAPSHOT=0
NON_INTERACTIVE=0
NO_NODE_SETUP=0

usage() {
  cat <<USAGE
MSC production installer

Usage:
  curl -fsSL https://mscblockexplorer.in/install.sh | bash
  curl -fsSL https://mscblockexplorer.in/install.sh | bash -s -- --role validator --id V1 --auto-start

Options:
  --role candidate|validator|full|archive
  --id, --node-id NODE_ID
  --auto-start
  --bootnodes PEERS           comma-separated P2P bootstrap peers
  --rpc ADDR                  local RPC bind address, for example 127.0.0.1:26657
  --p2p-port PORT             P2P listen port
  --p2p-external MULTIADDR    advertised P2P multiaddr
  --non-interactive
  --no-node-setup
  --install-path PATH          default: /usr/local/bin/msc
  --share-dir PATH             default: /usr/local/share/msc
  --release-base-url URL       default: https://mscblockexplorer.in/releases
  --latest-url URL             default: <release-base-url>/latest.json
  --snapshot-url URL           override release snapshot mirror URL
  --snapshot-public-key PATH   trusted OpenSSL public key for latest.json.sig
  --snapshot-required          fail setup/start when snapshot bootstrap fails
  --snapshot-optional          allow P2P fallback if snapshot bootstrap fails
  --no-snapshot                disable automatic snapshot bootstrap
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --role) ROLE="${2:?--role requires value}"; shift 2 ;;
    --id|--node-id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --auto-start) AUTO_START=1; shift ;;
    --bootnodes|--peers) BOOTNODES="${2:?--bootnodes requires value}"; shift 2 ;;
    --rpc) RPC="${2:?--rpc requires value}"; shift 2 ;;
    --p2p-port) P2P_PORT="${2:?--p2p-port requires value}"; shift 2 ;;
    --p2p-external) P2P_EXTERNAL="${2:?--p2p-external requires value}"; shift 2 ;;
    --non-interactive) NON_INTERACTIVE=1; shift ;;
    --no-node-setup) NO_NODE_SETUP=1; shift ;;
    --install-path) INSTALL_PATH="${2:?--install-path requires value}"; shift 2 ;;
    --share-dir) SHARE_DIR="${2:?--share-dir requires value}"; shift 2 ;;
    --release-base-url) RELEASE_BASE_URL="${2:?--release-base-url requires value}"; shift 2 ;;
    --latest-url) LATEST_URL="${2:?--latest-url requires value}"; shift 2 ;;
    --snapshot-url) SNAPSHOT_URL="${2:?--snapshot-url requires value}"; shift 2 ;;
    --snapshot-public-key) SNAPSHOT_PUBLIC_KEY="${2:?--snapshot-public-key requires value}"; shift 2 ;;
    --snapshot-required) SNAPSHOT_REQUIRED=1; shift ;;
    --snapshot-optional) SNAPSHOT_REQUIRED=0; shift ;;
    --no-snapshot) NO_SNAPSHOT=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required in PATH" >&2; exit 1; }
}

need curl
need sha256sum
need python3
need uname
need install

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "MSC installer currently supports Linux only." >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) MSC_ARCH="amd64" ;;
  aarch64|arm64) MSC_ARCH="arm64" ;;
  *) echo "unsupported Linux architecture: $(uname -m)" >&2; exit 1 ;;
esac

run_privileged() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
    return
  fi
  if ! command -v sudo >/dev/null 2>&1; then
    echo "sudo is required to install into $INSTALL_PATH and $SHARE_DIR" >&2
    exit 1
  fi
  sudo "$@"
}

absolute_url() {
  local path="$1"
  case "$path" in
    http://*|https://*) printf '%s\n' "$path" ;;
    *) printf '%s/%s\n' "${RELEASE_BASE_URL%/}" "${path#/}" ;;
  esac
}

prompt_tty() {
  local prompt="$1" default="${2:-}" answer=""
  if [[ "$NON_INTERACTIVE" == "1" || ! -r /dev/tty || ! -w /dev/tty ]]; then
    return 1
  fi
  if [[ -n "$default" ]]; then
    printf '%s [%s]: ' "$prompt" "$default" > /dev/tty
  else
    printf '%s: ' "$prompt" > /dev/tty
  fi
  IFS= read -r answer < /dev/tty || return 1
  if [[ -z "$answer" ]]; then
    answer="$default"
  fi
  printf '%s\n' "$answer"
}

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

is_truthy() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    1|true|yes|y|on) return 0 ;;
    *) return 1 ;;
  esac
}

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

latest_json="$tmp_dir/latest.json"
echo "Fetching MSC release metadata: $LATEST_URL"
curl -fsSL "$LATEST_URL" -o "$latest_json"

mapfile -t release_meta < <(python3 - "$latest_json" "$MSC_ARCH" <<'PY'
import json, sys
path, arch = sys.argv[1:]
with open(path, encoding="utf-8-sig") as f:
    meta = json.load(f)
version = str(meta.get("version_tag") or "").strip()
artifact = None
for item in meta.get("artifacts") or []:
    if str(item.get("os") or "").lower() == "linux" and str(item.get("arch") or "").lower() == arch:
        artifact = item
        break
if not version:
    raise SystemExit("latest.json missing version_tag")
if artifact is None:
    raise SystemExit(f"latest.json has no linux/{arch} artifact")
print(version)
print(str(artifact.get("sha256") or "").strip().lower())
print(str(artifact.get("url") or artifact.get("url_path") or artifact.get("file") or "").strip())
print(str(meta.get("genesis_path") or f"{version}/genesis.json").strip())
print(str(meta.get("config_path") or f"{version}/config.toml").strip())
print(str(meta.get("installer_path") or f"{version}/scripts/install_msc_node.sh").strip())
print(str(meta.get("snapshot_bootstrap_path") or f"{version}/scripts/bootstrap_msc_snapshot.sh").strip())
print(str(meta.get("genesis_sha256") or "").strip().lower())
print(str(meta.get("config_sha256") or "").strip().lower())
print(str(meta.get("installer_sha256") or "").strip().lower())
print(str(meta.get("snapshot_bootstrap_sha256") or "").strip().lower())
print(str(meta.get("snapshot_url") or meta.get("snapshot_mirror_url") or "").strip())
print(str(meta.get("snapshot_public_key_path") or "").strip())
print(str(meta.get("snapshot_public_key_sha256") or "").strip().lower())
print(str(meta.get("snapshot_required") if "snapshot_required" in meta else "").strip().lower())
print(str(meta.get("snapshot_require_signature") if "snapshot_require_signature" in meta else "").strip().lower())
bootnodes = meta.get("bootnodes") or meta.get("bootstrap_peers") or meta.get("p2p_bootnodes") or ""
if isinstance(bootnodes, list):
    bootnodes = ",".join(str(item).strip() for item in bootnodes if str(item).strip())
print(str(bootnodes).strip())
PY
)

VERSION_TAG="${release_meta[0]}"
ARTIFACT_SHA256="${release_meta[1]}"
ARTIFACT_PATH="${release_meta[2]}"
GENESIS_PATH="${release_meta[3]}"
CONFIG_PATH="${release_meta[4]}"
NODE_INSTALLER_PATH="${release_meta[5]}"
SNAPSHOT_BOOTSTRAP_PATH="${release_meta[6]}"
GENESIS_SHA256="${release_meta[7]:-}"
CONFIG_SHA256="${release_meta[8]:-}"
NODE_INSTALLER_SHA256="${release_meta[9]:-}"
SNAPSHOT_BOOTSTRAP_SHA256="${release_meta[10]:-}"
META_SNAPSHOT_URL="${release_meta[11]:-}"
SNAPSHOT_PUBLIC_KEY_PATH="${release_meta[12]:-}"
SNAPSHOT_PUBLIC_KEY_SHA256="${release_meta[13]:-}"
META_SNAPSHOT_REQUIRED="${release_meta[14]:-}"
META_SNAPSHOT_REQUIRE_SIGNATURE="${release_meta[15]:-}"
META_BOOTNODES="${release_meta[16]:-}"

if [[ -z "$ARTIFACT_SHA256" || -z "$ARTIFACT_PATH" ]]; then
  echo "latest.json selected linux/$MSC_ARCH but artifact URL/checksum is empty" >&2
  exit 1
fi

artifact_url="$(absolute_url "$ARTIFACT_PATH")"
downloaded_binary="$tmp_dir/msc"
echo "Downloading MSC $VERSION_TAG linux/$MSC_ARCH"
curl -fsSL "$artifact_url" -o "$downloaded_binary"
printf '%s  %s\n' "$ARTIFACT_SHA256" "$downloaded_binary" | sha256sum -c -
chmod +x "$downloaded_binary"

echo "Installing msc binary to $INSTALL_PATH"
run_privileged install -d -m 0755 "$(dirname "$INSTALL_PATH")"
run_privileged install -m 0755 "$downloaded_binary" "$INSTALL_PATH"

support_tmp="$tmp_dir/support"
mkdir -p "$support_tmp/scripts"
download_support() {
  local source_path="$1" target_rel="$2" mode="$3" expected_sha="${4:-}"
  [[ -z "$source_path" ]] && return 0
  local url target
  url="$(absolute_url "$source_path")"
  target="$support_tmp/$target_rel"
  mkdir -p "$(dirname "$target")"
  echo "Fetching support file: $url"
  curl -fsSL "$url" -o "$target"
  if [[ -n "$expected_sha" ]]; then
    printf '%s  %s\n' "$expected_sha" "$target" | sha256sum -c -
  fi
  chmod "$mode" "$target"
}

download_support "$GENESIS_PATH" "genesis.json" 0644 "$GENESIS_SHA256"
download_support "$CONFIG_PATH" "config.toml" 0644 "$CONFIG_SHA256"
download_support "$NODE_INSTALLER_PATH" "scripts/install_msc_node.sh" 0755 "$NODE_INSTALLER_SHA256"
download_support "$SNAPSHOT_BOOTSTRAP_PATH" "scripts/bootstrap_msc_snapshot.sh" 0755 "$SNAPSHOT_BOOTSTRAP_SHA256"
download_support "$SNAPSHOT_PUBLIC_KEY_PATH" "snapshot_pubkey.pem" 0644 "$SNAPSHOT_PUBLIC_KEY_SHA256"

echo "Installing support files to $SHARE_DIR"
run_privileged install -d -m 0755 "$SHARE_DIR" "$SHARE_DIR/scripts"
run_privileged install -m 0644 "$support_tmp/genesis.json" "$SHARE_DIR/genesis.json"
run_privileged install -m 0644 "$support_tmp/config.toml" "$SHARE_DIR/config.toml"
run_privileged install -m 0755 "$support_tmp/scripts/install_msc_node.sh" "$SHARE_DIR/scripts/install_msc_node.sh"
if [[ -f "$support_tmp/scripts/bootstrap_msc_snapshot.sh" ]]; then
  run_privileged install -m 0755 "$support_tmp/scripts/bootstrap_msc_snapshot.sh" "$SHARE_DIR/scripts/bootstrap_msc_snapshot.sh"
fi
if [[ -f "$support_tmp/snapshot_pubkey.pem" ]]; then
  run_privileged install -m 0644 "$support_tmp/snapshot_pubkey.pem" "$SHARE_DIR/snapshot_pubkey.pem"
fi

echo "Verifying installed binary"
if ! "$INSTALL_PATH" version; then
  echo "warning: installed binary did not return version metadata; continuing after checksum verification" >&2
fi

case "$(printf '%s' "$ROLE" | tr '[:upper:]' '[:lower:]')" in
  candidate|candidate-validator) ROLE="candidate" ;;
  validator|private-validator) ROLE="validator" ;;
  full|full-node|node) ROLE="full" ;;
  archive|archive-node) ROLE="archive" ;;
  "") ;;
  *) echo "--role must be candidate, validator, full, or archive" >&2; exit 2 ;;
esac

if [[ "$NO_NODE_SETUP" == "1" ]]; then
  echo "Binary install complete. Node setup skipped."
  exit 0
fi

if [[ -z "$ROLE" ]]; then
  echo
  echo "MSC onboarding wizard"
  echo "  1) Candidate"
  echo "  2) Validator"
  echo "  3) Full Node"
  echo "  4) Archive Node"
  choice="$(prompt_tty "Choose node type" "3" || true)"
  case "$choice" in
    1|candidate|Candidate) ROLE="candidate" ;;
    2|validator|Validator) ROLE="validator" ;;
    3|full|Full|"Full Node") ROLE="full" ;;
    4|archive|Archive|"Archive Node") ROLE="archive" ;;
    *) ROLE="full" ;;
  esac
fi

if [[ -z "$NODE_ID" ]]; then
  default_id="$(generate_node_id)"
  NODE_ID="$(prompt_tty "Node ID" "$default_id" || printf '%s\n' "$default_id")"
fi
NODE_ID="$(normalize_node_id "$NODE_ID")"
if [[ -z "$NODE_ID" ]]; then
  echo "node id resolved to empty value" >&2
  exit 2
fi

if [[ "$AUTO_START" != "1" ]]; then
  auto_answer="$(prompt_tty "Auto-start node after setup? y/N" "N" || true)"
  case "$(printf '%s' "$auto_answer" | tr '[:upper:]' '[:lower:]')" in
    y|yes) AUTO_START=1 ;;
  esac
fi

case "$ROLE" in
  candidate) NODE_TYPE="candidate-validator" ;;
  validator) NODE_TYPE="private-validator" ;;
  full) NODE_TYPE="full" ;;
  archive) NODE_TYPE="archive" ;;
  *) echo "invalid role: $ROLE" >&2; exit 2 ;;
esac

setup_args=(--node-type "$NODE_TYPE" --id "$NODE_ID" --prebuilt-binary "$INSTALL_PATH")
if [[ "$AUTO_START" == "1" ]]; then
  setup_args+=(--auto-start)
fi
if [[ -n "$RPC" ]]; then
  setup_args+=(--rpc "$RPC")
fi
if [[ -n "$P2P_PORT" ]]; then
  setup_args+=(--p2p-port "$P2P_PORT")
fi
if [[ -n "$P2P_EXTERNAL" ]]; then
  setup_args+=(--p2p-external "$P2P_EXTERNAL")
fi
if [[ -z "$BOOTNODES" ]]; then
  BOOTNODES="$META_BOOTNODES"
fi
if [[ -n "$BOOTNODES" ]]; then
  setup_args+=(--bootnodes "$BOOTNODES")
fi
if [[ "$NO_SNAPSHOT" != "1" ]]; then
  if [[ -z "$SNAPSHOT_URL" ]]; then
    SNAPSHOT_URL="$META_SNAPSHOT_URL"
  fi
  if [[ -z "$SNAPSHOT_PUBLIC_KEY" && -f "$SHARE_DIR/snapshot_pubkey.pem" ]]; then
    SNAPSHOT_PUBLIC_KEY="$SHARE_DIR/snapshot_pubkey.pem"
  fi
  if [[ -n "$SNAPSHOT_PUBLIC_KEY" && -f "$SNAPSHOT_PUBLIC_KEY" ]]; then
    SNAPSHOT_PUBLIC_KEY="$(cd "$(dirname "$SNAPSHOT_PUBLIC_KEY")" && pwd)/$(basename "$SNAPSHOT_PUBLIC_KEY")"
  fi
  if [[ -z "$SNAPSHOT_REQUIRED" ]]; then
    SNAPSHOT_REQUIRED="$META_SNAPSHOT_REQUIRED"
  fi
  if [[ -z "$SNAPSHOT_REQUIRE_SIGNATURE" ]]; then
    SNAPSHOT_REQUIRE_SIGNATURE="$META_SNAPSHOT_REQUIRE_SIGNATURE"
  fi
  if [[ -n "$SNAPSHOT_PUBLIC_KEY" && -z "$SNAPSHOT_REQUIRE_SIGNATURE" ]]; then
    SNAPSHOT_REQUIRE_SIGNATURE=1
  fi
  if [[ -n "$SNAPSHOT_URL" && -z "$SNAPSHOT_REQUIRED" ]]; then
    SNAPSHOT_REQUIRED=1
  fi
  if [[ -n "$SNAPSHOT_URL" ]]; then
    setup_args+=(--snapshot-url "$SNAPSHOT_URL")
  fi
  if [[ -n "$SNAPSHOT_PUBLIC_KEY" ]]; then
    setup_args+=(--snapshot-public-key "$SNAPSHOT_PUBLIC_KEY")
  fi
  if is_truthy "$SNAPSHOT_REQUIRE_SIGNATURE"; then
    setup_args+=(--require-snapshot-signature)
  fi
  if is_truthy "$SNAPSHOT_REQUIRED"; then
    setup_args+=(--snapshot-required)
  fi
fi

echo
echo "Running MSC node setup: role=$ROLE node_type=$NODE_TYPE node_id=$NODE_ID"
bash "$SHARE_DIR/scripts/install_msc_node.sh" "${setup_args[@]}"

echo
echo "MSC install complete."
echo "Binary: $INSTALL_PATH"
echo "Support files: $SHARE_DIR"
echo "Version: $VERSION_TAG"
