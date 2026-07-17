#!/usr/bin/env bash
set -euo pipefail

MIRROR_DIR=""
TARGET=""
SSH_KEY=""
PUBLIC_KEY=""
STRICT_HOST_KEY_CHECKING="accept-new"

usage() {
  cat <<'USAGE'
Usage: sync_msc_snapshot_mirror.sh --mirror-dir DIR --target USER@HOST:/path [options]

Pushes the current MSC snapshot mirror to a static web directory. The artifact
hash in latest.json is verified before upload. latest.json is uploaded last and
atomically moved into place, so installers never observe a half-published mirror.

Options:
  --mirror-dir DIR      Local mirror directory containing latest.json.
  --target TARGET       SSH target directory, for example ubuntu@web:/var/www/msc-ui/snapshots.
  --ssh-key PATH        Optional private key for ssh/scp.
  --public-key PATH     Optional snapshot public key to publish beside latest.json.
  --strict-host-key-checking MODE
                        ssh StrictHostKeyChecking value. Default: accept-new.
  -h, --help            Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mirror-dir) MIRROR_DIR="${2:?--mirror-dir requires value}"; shift 2 ;;
    --target) TARGET="${2:?--target requires value}"; shift 2 ;;
    --ssh-key) SSH_KEY="${2:?--ssh-key requires value}"; shift 2 ;;
    --public-key) PUBLIC_KEY="${2:?--public-key requires value}"; shift 2 ;;
    --strict-host-key-checking) STRICT_HOST_KEY_CHECKING="${2:?--strict-host-key-checking requires value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required in PATH" >&2; exit 1; }
}

need python3
need sha256sum
need ssh
need scp

[[ -n "$MIRROR_DIR" && -n "$TARGET" ]] || { usage >&2; exit 2; }
[[ "$TARGET" == *:* ]] || { echo "--target must be USER@HOST:/path" >&2; exit 2; }
MIRROR_DIR="$(cd "$MIRROR_DIR" && pwd)"
latest="$MIRROR_DIR/latest.json"
[[ -f "$latest" ]] || { echo "latest.json not found in $MIRROR_DIR" >&2; exit 1; }

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
[[ -n "$artifact" && -n "$artifact_sha" && "$height" != "0" ]] || { echo "invalid latest.json" >&2; exit 1; }

artifact_path="$MIRROR_DIR/$artifact"
[[ -f "$artifact_path" ]] || { echo "artifact missing: $artifact_path" >&2; exit 1; }
actual_sha="$(sha256sum "$artifact_path" | awk '{print tolower($1)}')"
if [[ "$actual_sha" != "$artifact_sha" ]]; then
  echo "snapshot artifact checksum mismatch: got=$actual_sha want=$artifact_sha" >&2
  exit 1
fi

target_host="${TARGET%%:*}"
target_dir="${TARGET#*:}"
[[ -n "$target_host" && -n "$target_dir" ]] || { echo "invalid --target: $TARGET" >&2; exit 2; }

ssh_opts=(-o BatchMode=yes -o StrictHostKeyChecking="$STRICT_HOST_KEY_CHECKING")
if [[ -n "$SSH_KEY" ]]; then
  [[ -f "$SSH_KEY" ]] || { echo "ssh key not found: $SSH_KEY" >&2; exit 1; }
  ssh_opts+=(-i "$SSH_KEY")
fi

remote_dir_q="$(python3 - "$target_dir" <<'PY'
import shlex, sys
print(shlex.quote(sys.argv[1]))
PY
)"
ssh "${ssh_opts[@]}" "$target_host" "mkdir -p $remote_dir_q"

upload_file() {
  local src="$1" name
  [[ -f "$src" ]] || return 0
  name="$(basename "$src")"
  scp "${ssh_opts[@]}" "$src" "$target_host:$target_dir/$name"
}

upload_file "$artifact_path"
upload_file "$MIRROR_DIR/backup_${height}.json"
upload_file "$MIRROR_DIR/latest.json.sig"
if [[ -n "$PUBLIC_KEY" ]]; then
  upload_file "$PUBLIC_KEY"
fi

scp "${ssh_opts[@]}" "$latest" "$target_host:$target_dir/latest.json.tmp"
ssh "${ssh_opts[@]}" "$target_host" "mv $remote_dir_q/latest.json.tmp $remote_dir_q/latest.json"

python3 - "$latest" "$TARGET" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8-sig"))
print(json.dumps({
    "command": "snapshot mirror sync",
    "target": sys.argv[2],
    "height": m.get("height"),
    "artifact": m.get("artifact"),
    "snapshot_hash": m.get("snapshot_hash"),
}, indent=2, sort_keys=True))
PY
