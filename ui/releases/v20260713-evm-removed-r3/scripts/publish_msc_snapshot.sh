#!/usr/bin/env bash
set -euo pipefail

NODE_ID=""
DATA_DIR=""
BINARY=""
MIRROR_DIR=""
S3_URI=""
HEIGHT="0"
REASON="scheduled_snapshot_publish"
RETAIN="8"
BACKUP_DIR=""
SIGNING_KEY=""
EXPORT_TIMEOUT="${MSC_SNAPSHOT_EXPORT_TIMEOUT:-90s}"

usage() {
  cat <<'USAGE'
Usage: publish_msc_snapshot.sh --id NODE --datadir DATA --mirror-dir DIR [options]

Creates a native MSC recovery snapshot backup, verifies it, packages it as a
tar.gz artifact, and atomically updates latest.json in a static mirror.

Options:
  --id NODE              Node ID to export from, for example A.
  --datadir DATA         Base data directory used by msc-node.
  --binary PATH          msc-node binary path. Default: ./msc-node.
  --mirror-dir DIR       Local static mirror directory to publish into.
  --s3-uri URI           Optional s3://bucket/prefix mirror upload target.
  --height HEIGHT        Snapshot height. 0 means best available snapshot.
  --reason TEXT          Export reason stored in backup metadata.
  --backup-dir DIR       Package an existing backup directory instead of exporting.
  --signing-key PATH     Optional OpenSSL private key used to sign latest.json.
  --export-timeout DURATION
                         Bound live export time. If a backup was created before
                         timeout, it is packaged. Default: 90s.
  --retain N             Keep latest N packaged artifacts in mirror-dir. Default: 8.
  -h, --help             Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --id) NODE_ID="${2:?--id requires value}"; shift 2 ;;
    --datadir) DATA_DIR="${2:?--datadir requires value}"; shift 2 ;;
    --binary) BINARY="${2:?--binary requires value}"; shift 2 ;;
    --mirror-dir) MIRROR_DIR="${2:?--mirror-dir requires value}"; shift 2 ;;
    --s3-uri) S3_URI="${2:?--s3-uri requires value}"; shift 2 ;;
    --height) HEIGHT="${2:?--height requires value}"; shift 2 ;;
    --reason) REASON="${2:?--reason requires value}"; shift 2 ;;
    --backup-dir) BACKUP_DIR="${2:?--backup-dir requires value}"; shift 2 ;;
    --signing-key) SIGNING_KEY="${2:?--signing-key requires value}"; shift 2 ;;
    --export-timeout) EXPORT_TIMEOUT="${2:?--export-timeout requires value}"; shift 2 ;;
    --retain) RETAIN="${2:?--retain requires value}"; shift 2 ;;
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
need stat
if [[ -n "$SIGNING_KEY" ]]; then
  need openssl
fi

if [[ -z "$NODE_ID" || -z "$DATA_DIR" ]]; then
  usage >&2
  exit 2
fi
if [[ -z "$MIRROR_DIR" && -z "$S3_URI" ]]; then
  echo "--mirror-dir or --s3-uri is required" >&2
  exit 2
fi
if [[ -z "$BINARY" ]]; then
  BINARY="./msc-node"
fi
if [[ ! -x "$BINARY" ]]; then
  echo "binary is not executable: $BINARY" >&2
  exit 1
fi

NODE_ID="$(printf '%s' "$NODE_ID" | tr '[:lower:]' '[:upper:]')"
DATA_DIR="$(cd "$DATA_DIR" && pwd)"
BACKUP_ROOT="$DATA_DIR/node_$NODE_ID/backups"

if [[ -z "$BACKUP_DIR" ]]; then
  export_log="$(mktemp)"
  export_marker="$(mktemp)"
  touch "$export_marker"
  set +e
  timeout "$EXPORT_TIMEOUT" "$BINARY" snapshot export --id "$NODE_ID" --datadir "$DATA_DIR" --height "$HEIGHT" --reason "$REASON" >"$export_log" 2>&1
  export_rc=$?
  set -e
  if [[ "$export_rc" != "0" ]]; then
    cat "$export_log" >&2
  fi
  BACKUP_DIR="$(find "$BACKUP_ROOT" -maxdepth 1 -type d -name 'backup_*' -newer "$export_marker" -printf '%T@ %p\n' 2>/dev/null | sort -nr | awk 'NR==1{print $2}')"
  if [[ -z "$BACKUP_DIR" ]]; then
    BACKUP_DIR="$(find "$BACKUP_ROOT" -maxdepth 1 -type d -name 'backup_*' -printf '%T@ %p\n' 2>/dev/null | sort -nr | awk 'NR==1{print $2}')"
  fi
  if [[ "$export_rc" != "0" && -n "$BACKUP_DIR" ]]; then
    echo "snapshot export exited rc=$export_rc; packaging completed backup: $BACKUP_DIR" >&2
  elif [[ "$export_rc" != "0" ]]; then
    rm -f "$export_log" "$export_marker"
    exit "$export_rc"
  fi
  rm -f "$export_log"
  rm -f "$export_marker"
fi

if [[ -z "$BACKUP_DIR" || ! -d "$BACKUP_DIR" ]]; then
  echo "backup directory not found" >&2
  exit 1
fi
if [[ ! -f "$BACKUP_DIR/backup_manifest.json" ]]; then
  echo "backup manifest missing: $BACKUP_DIR/backup_manifest.json" >&2
  exit 1
fi

"$BINARY" snapshot verify --path "$BACKUP_DIR" >/dev/null

metadata_json="$(python3 - "$BACKUP_DIR/backup_manifest.json" "$NODE_ID" <<'PY'
import json, pathlib, sys, time
manifest_path = pathlib.Path(sys.argv[1])
node_id = sys.argv[2]
m = json.loads(manifest_path.read_text(encoding="utf-8"))
height = int(m.get("height") or 0)
snapshot_hash = (m.get("snapshot_hash") or "").strip()
if height <= 0 or not snapshot_hash:
    raise SystemExit("invalid backup manifest")
print(json.dumps({
    "height": height,
    "snapshot_hash": snapshot_hash,
    "chain_id": (m.get("chain_id") or "").strip(),
    "state_root": (m.get("state_root") or "").strip(),
    "validator_set_hash": (m.get("validator_set_hash") or "").strip(),
    "validator_registry_hash": (m.get("validator_registry_hash") or "").strip(),
    "finalized_height": int(m.get("finalized_height") or 0),
    "finalized_hash": (m.get("finalized_hash") or "").strip(),
    "source_node_id": node_id,
    "backup_name": manifest_path.parent.name,
    "exported_at_unix": int(time.time()),
}))
PY
)"

height="$(python3 - "$metadata_json" <<'PY'
import json, sys
print(json.loads(sys.argv[1])["height"])
PY
)"
snapshot_hash="$(python3 - "$metadata_json" <<'PY'
import json, sys
print(json.loads(sys.argv[1])["snapshot_hash"])
PY
)"
artifact="msc-snapshot-${height}-${snapshot_hash:0:12}.tar.gz"

work_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

tar_path="$work_dir/$artifact"
tar -C "$(dirname "$BACKUP_DIR")" -czf "$tar_path" "$(basename "$BACKUP_DIR")"
artifact_sha="$(sha256sum "$tar_path" | awk '{print $1}')"
artifact_size="$(stat -c '%s' "$tar_path")"

latest_path="$work_dir/latest.json"
python3 - "$metadata_json" "$artifact" "$artifact_sha" "$artifact_size" "$SIGNING_KEY" >"$latest_path" <<'PY'
import json, sys, time
meta = json.loads(sys.argv[1])
artifact = sys.argv[2]
artifact_sha = sys.argv[3]
artifact_size = int(sys.argv[4])
signed = bool(sys.argv[5])
out = {
    "schema_version": 1,
    "kind": "msc_snapshot_mirror_latest_v1",
    **meta,
    "artifact": artifact,
    "artifact_sha256": artifact_sha,
    "artifact_size": artifact_size,
    "created_at_unix": int(time.time()),
    "verification": {
        "format": "msc_recovery_backup_v1",
        "required_files": [
            "backup_manifest.json",
            "snapshot.json",
            "snapshot_manifest.json"
        ]
    }
}
if signed:
    out["signature"] = {
        "algorithm": "openssl-dgst-sha256",
        "target": "latest.json",
        "file": "latest.json.sig"
    }
print(json.dumps(out, indent=2, sort_keys=True))
PY

signature_path=""
if [[ -n "$SIGNING_KEY" ]]; then
  [[ -f "$SIGNING_KEY" ]] || { echo "snapshot signing key not found: $SIGNING_KEY" >&2; exit 1; }
  signature_path="$latest_path.sig"
  openssl dgst -sha256 -sign "$SIGNING_KEY" -out "$signature_path" "$latest_path"
fi

if [[ -n "$MIRROR_DIR" ]]; then
  mkdir -p "$MIRROR_DIR"
  cp "$tar_path" "$MIRROR_DIR/$artifact.tmp"
  mv "$MIRROR_DIR/$artifact.tmp" "$MIRROR_DIR/$artifact"
  cp "$latest_path" "$MIRROR_DIR/latest.json.tmp"
  mv "$MIRROR_DIR/latest.json.tmp" "$MIRROR_DIR/latest.json"
  if [[ -n "$signature_path" ]]; then
    cp "$signature_path" "$MIRROR_DIR/latest.json.sig.tmp"
    mv "$MIRROR_DIR/latest.json.sig.tmp" "$MIRROR_DIR/latest.json.sig"
  fi
  cp "$BACKUP_DIR/backup_manifest.json" "$MIRROR_DIR/backup_${height}.json.tmp"
  mv "$MIRROR_DIR/backup_${height}.json.tmp" "$MIRROR_DIR/backup_${height}.json"
  if [[ "$RETAIN" =~ ^[0-9]+$ && "$RETAIN" -gt 0 ]]; then
    find "$MIRROR_DIR" -maxdepth 1 -type f -name 'msc-snapshot-*.tar.gz' -printf '%T@ %p\n' |
      sort -nr | awk -v keep="$RETAIN" 'NR>keep{print $2}' | while read -r old; do
        rm -f "$old"
      done
  fi
fi

if [[ -n "$S3_URI" ]]; then
  need aws
  aws s3 cp "$tar_path" "${S3_URI%/}/$artifact"
  aws s3 cp "$latest_path" "${S3_URI%/}/latest.json"
  if [[ -n "$signature_path" ]]; then
    aws s3 cp "$signature_path" "${S3_URI%/}/latest.json.sig"
  fi
fi

cat "$latest_path"
