#!/usr/bin/env bash
set -euo pipefail

NODE_ID="${1:-}"
THRESHOLD="${2:-2}"
PARTICIPANTS="${3:-3}"

if [ -z "$NODE_ID" ]; then
  echo "usage: scripts/enable_mpc_signing.sh <NODE_ID> [threshold] [participants]" >&2
  exit 2
fi

cd "$(dirname "$0")/.."

NODE_ID="$(printf '%s' "$NODE_ID" | tr '[:lower:]' '[:upper:]')"
NODE_DIR="runtime-data/distributed/$NODE_ID"
MPC_DIR="$NODE_DIR/mpc"
CONFIG_SRC="config.toml"
CONFIG_DST="$NODE_DIR/config.mpc.toml"
PASS_FILE="$MPC_DIR/share.pass"

mkdir -p "$MPC_DIR"
chmod 700 "$MPC_DIR" 2>/dev/null || true

if [ ! -s "$PASS_FILE" ]; then
  umask 077
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 > "$PASS_FILE"
  else
    head -c 32 /dev/urandom | base64 > "$PASS_FILE"
  fi
fi
chmod 600 "$PASS_FILE" 2>/dev/null || true

export MSC_MPC_SHARE_PASSWORD="$(cat "$PASS_FILE")"

if [ ! -s "$MPC_DIR/validator.pub" ]; then
  ./msc-node validator mpc-keygen \
    --validator "$NODE_ID" \
    --threshold "$THRESHOLD" \
    --participants "$PARTICIPANTS" \
    --outdir "$MPC_DIR" \
    --force >/tmp/msc_mpc_keygen_"$NODE_ID".json
fi

PUBKEY="$(./msc-node validator mpc-pubkey --pub "$MPC_DIR/validator.pub" | python3 -c 'import json,sys; print(json.load(sys.stdin)["public_key"])')"
SIGNER="./msc-node validator mpc-sign --shares $MPC_DIR/share1.sec,$MPC_DIR/share2.sec --password-file $PASS_FILE"

python3 - "$CONFIG_SRC" "$CONFIG_DST" "$NODE_ID" "$PUBKEY" "$SIGNER" "$THRESHOLD" "$PARTICIPANTS" <<'PY'
import sys

src, dst, node_id, pubkey, signer, threshold, participants = sys.argv[1:8]
updates = {
    "hsm_enabled": "false",
    "mpc_enabled": "true",
    "mpc_provider": '"threshold_ed25519"',
    "mpc_key_id": f'"msc-validator-{node_id}-cluster"',
    "mpc_public_key": f'"{pubkey}"',
    "mpc_external_signer_command": '"' + signer.replace("\\", "\\\\").replace('"', '\\"') + '"',
    "mpc_timeout_ms": "5000",
    "mpc_threshold": str(threshold),
    "mpc_participants": str(participants),
}
seen = set()
out = []
for line in open(src, encoding="utf-8"):
    stripped = line.strip()
    if "=" in stripped and not stripped.startswith("#"):
        key = stripped.split("=", 1)[0].strip()
        if key in updates:
            out.append(f"{key} = {updates[key]}\n")
            seen.add(key)
            continue
    out.append(line)
missing = [k for k in updates if k not in seen]
if missing:
    out.append("\n# Generated MPC signer settings\n")
    for key in missing:
        out.append(f"{key} = {updates[key]}\n")
open(dst, "w", encoding="utf-8").writelines(out)
PY

echo "node=$NODE_ID"
echo "config=$CONFIG_DST"
echo "mpc_public_key=$PUBKEY"
echo "mpc_signer=$SIGNER"
echo "mpc_password_file=$PASS_FILE"
echo "start_with=--config $CONFIG_DST"
