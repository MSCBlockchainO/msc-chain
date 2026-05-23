#!/usr/bin/env bash
set -euo pipefail

BINARY="${BINARY:-./msc-chain}"
CONFIG="${CONFIG:-config.toml}"
DATA_ROOT="${DATA_ROOT:-runtime-data/ec2-single-host}"
LOG_ROOT="${LOG_ROOT:-runtime-logs/ec2-single-host}"
NODE_IDS_RAW="${NODE_IDS:-A B C D}"
VALIDATOR_COUNT="${VALIDATOR_COUNT:-4}"
BASE_P2P_PORT="${BASE_P2P_PORT:-19001}"
BASE_RPC_PORT="${BASE_RPC_PORT:-29157}"
DURATION_SECONDS="${DURATION_SECONDS:-75}"
WARMUP_SECONDS="${WARMUP_SECONDS:-15}"
SAMPLE_SECONDS="${SAMPLE_SECONDS:-5}"
MAX_BLOCK_GAP_SECONDS="${MAX_BLOCK_GAP_SECONDS:-60}"
MAX_FINALIZED_LAG_BLOCKS="${MAX_FINALIZED_LAG_BLOCKS:-25}"
MAX_HEIGHT_LAG_BLOCKS="${MAX_HEIGHT_LAG_BLOCKS:-35}"
RESTART_EVERY_SECONDS="${RESTART_EVERY_SECONDS:-0}"
RESTART_DOWN_SECONDS="${RESTART_DOWN_SECONDS:-4}"
MSC_VALIDATOR_PASSWORD="${MSC_VALIDATOR_PASSWORD:-mainnet-smoke-password}"

IFS=' ' read -r -a NODE_IDS <<< "$NODE_IDS_RAW"
mkdir -p "$DATA_ROOT" "$LOG_ROOT"

declare -A PIDS
declare -A PEER_IDS
declare -A PEER_ADDRS

node_port() {
  local idx="$1"
  echo $((BASE_P2P_PORT + idx))
}

rpc_port() {
  local idx="$1"
  echo $((BASE_RPC_PORT + idx))
}

node_log() {
  local id="$1"
  echo "$LOG_ROOT/${id}.log"
}

stop_node() {
  local id="$1"
  local pid="${PIDS[$id]:-}"
  if [[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1; then
    kill "$pid" >/dev/null 2>&1 || true
    sleep 1
    kill -9 "$pid" >/dev/null 2>&1 || true
    wait "$pid" 2>/dev/null || true
  fi
  PIDS[$id]=""
}

stop_all() {
  for id in "${NODE_IDS[@]}"; do
    stop_node "$id"
  done
}
trap stop_all EXIT

start_node() {
  local id="$1"
  local idx="$2"
  local role="full"
  if (( idx < VALIDATOR_COUNT )); then
    role="validator"
  fi
  local port
  port="$(node_port "$idx")"
  local rpc
  rpc="127.0.0.1:$(rpc_port "$idx")"
  local data_dir="$DATA_ROOT/$id"
  local log
  log="$(node_log "$id")"
  local peer_arg=()
  if [[ "${3:-}" != "" ]]; then
    peer_arg=(--peers "$3")
  fi
  mkdir -p "$data_dir" "$(dirname "$log")"
  MSC_ALLOW_VALIDATOR_KEY_CREATE=1 \
  MSC_VALIDATOR_PASSWORD="$MSC_VALIDATOR_PASSWORD" \
  MSC_AUTH_OPEN_BROWSER=0 \
    nohup "$BINARY" \
      --mode=full \
      --role="$role" \
      --id="$id" \
      --port="$port" \
      --datadir="$data_dir" \
      --rpcaddr "$rpc" \
      --config "$CONFIG" \
      "${peer_arg[@]}" \
      > "$log" 2>&1 &
  PIDS[$id]="$!"
}

wait_rpc() {
  local id="$1"
  local idx="$2"
  local deadline=$((SECONDS + 45))
  local url="http://127.0.0.1:$(rpc_port "$idx")/status"
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "node $id RPC did not become ready: $url" >&2
  return 1
}

wait_peer_id() {
  local id="$1"
  local log
  log="$(node_log "$id")"
  local deadline=$((SECONDS + 30))
  while (( SECONDS < deadline )); do
    local peer_id
    peer_id="$(grep -Eo 'peer_id=[A-Za-z0-9]+' "$log" 2>/dev/null | tail -n1 | cut -d= -f2 || true)"
    if [[ "$peer_id" != "" ]]; then
      echo "$peer_id"
      return 0
    fi
    sleep 1
  done
  echo "node $id peer_id not found in $log" >&2
  return 1
}

status_row() {
  local id="$1"
  local idx="$2"
  local url="http://127.0.0.1:$(rpc_port "$idx")/status"
  curl -fsS --max-time 3 "$url" 2>/dev/null | python3 -c '
import json,sys
try:
    s=json.load(sys.stdin)
    print("{id} {height} {finalized} {peers} {ready}".format(
        id=s.get("node_id","?"),
        height=int(s.get("height") or 0),
        finalized=int(s.get("finalized_height") or s.get("height") or 0),
        peers=int(s.get("peers") or 0),
        ready=1 if s.get("ready") else 0,
    ))
except Exception:
    sys.exit(1)
' || echo "$id 0 0 0 0"
}

write_peer_file() {
  local id="$1"
  shift
  local node_dir="$DATA_ROOT/$id/node_$id"
  mkdir -p "$node_dir"
  python3 - "$node_dir/peers.json" "$@" <<'PY'
import json,sys
path=sys.argv[1]
peers=sys.argv[2:]
with open(path,"w",encoding="utf-8") as f:
    json.dump(peers,f,indent=2)
PY
}

echo "[ec2-chaos] bootstrap phase: starting nodes to collect peer IDs"
for idx in "${!NODE_IDS[@]}"; do
  start_node "${NODE_IDS[$idx]}" "$idx" ""
  wait_rpc "${NODE_IDS[$idx]}" "$idx"
done

for idx in "${!NODE_IDS[@]}"; do
  id="${NODE_IDS[$idx]}"
  PEER_IDS[$id]="$(wait_peer_id "$id")"
  PEER_ADDRS[$id]="/ip4/127.0.0.1/tcp/$(node_port "$idx")/p2p/${PEER_IDS[$id]}"
  echo "[ec2-chaos] $id ${PEER_ADDRS[$id]}"
done

for idx in "${!NODE_IDS[@]}"; do
  id="${NODE_IDS[$idx]}"
  peers=()
  for other in "${NODE_IDS[@]}"; do
    [[ "$other" == "$id" ]] && continue
    peers+=("${PEER_ADDRS[$other]}")
  done
  write_peer_file "$id" "${peers[@]}"
done

stop_all
sleep 2

echo "[ec2-chaos] run phase: starting nodes with generated peer topology"
for idx in "${!NODE_IDS[@]}"; do
  id="${NODE_IDS[$idx]}"
  peers=()
  for other in "${NODE_IDS[@]}"; do
    [[ "$other" == "$id" ]] && continue
    peers+=("${PEER_ADDRS[$other]}")
  done
  IFS=, peer_csv="${peers[*]}"
  unset IFS
  start_node "$id" "$idx" "$peer_csv"
  wait_rpc "$id" "$idx"
done

start_time="$SECONDS"
deadline=$((SECONDS + DURATION_SECONDS))
last_progress_at="$SECONDS"
last_max_finalized=0
last_max_height=0
max_gap=0
max_finalized_lag=0
max_height_lag=0
warnings=0
sample=0
next_restart=$((SECONDS + RESTART_EVERY_SECONDS))

while (( SECONDS < deadline )); do
  sample=$((sample + 1))
  min_height=0
  max_height=0
  min_finalized=0
  max_finalized=0
  reachable=0
  ready=0
  rows=()
  for idx in "${!NODE_IDS[@]}"; do
    row="$(status_row "${NODE_IDS[$idx]}" "$idx")"
    rows+=("$row")
    read -r rid height finalized peers is_ready <<< "$row"
    if [[ "$rid" != "?" ]]; then
      reachable=$((reachable + 1))
      (( is_ready == 1 )) && ready=$((ready + 1))
      if (( reachable == 1 )); then
        min_height="$height"; max_height="$height"
        min_finalized="$finalized"; max_finalized="$finalized"
      else
        (( height < min_height )) && min_height="$height"
        (( height > max_height )) && max_height="$height"
        (( finalized < min_finalized )) && min_finalized="$finalized"
        (( finalized > max_finalized )) && max_finalized="$finalized"
      fi
    fi
  done

  if (( max_finalized > last_max_finalized )); then
    last_max_finalized="$max_finalized"
    last_progress_at="$SECONDS"
  fi
  (( max_height > last_max_height )) && last_max_height="$max_height"
  gap=$((SECONDS - last_progress_at))
  (( gap > max_gap )) && max_gap="$gap"
  finalized_lag=$((max_finalized - min_finalized))
  height_lag=$((max_height - min_height))
  (( finalized_lag > max_finalized_lag )) && max_finalized_lag="$finalized_lag"
  (( height_lag > max_height_lag )) && max_height_lag="$height_lag"

  past_warmup=0
  (( SECONDS - start_time >= WARMUP_SECONDS )) && past_warmup=1
  if (( past_warmup == 1 && gap > MAX_BLOCK_GAP_SECONDS )); then warnings=$((warnings + 1)); fi
  if (( past_warmup == 1 && finalized_lag > MAX_FINALIZED_LAG_BLOCKS )); then warnings=$((warnings + 1)); fi
  if (( past_warmup == 1 && height_lag > MAX_HEIGHT_LAG_BLOCKS )); then warnings=$((warnings + 1)); fi

  echo "sample=$sample reachable=$reachable ready=$ready height=$min_height-$max_height finalized=$min_finalized-$max_finalized gap_s=$gap lag_f=$finalized_lag lag_h=$height_lag warnings=$warnings"

  if (( RESTART_EVERY_SECONDS > 0 && past_warmup == 1 && SECONDS >= next_restart )); then
    restart_idx=$((sample % ${#NODE_IDS[@]}))
    restart_id="${NODE_IDS[$restart_idx]}"
    echo "[ec2-chaos] restarting $restart_id"
    stop_node "$restart_id"
    sleep "$RESTART_DOWN_SECONDS"
    peers=()
    for other in "${NODE_IDS[@]}"; do
      [[ "$other" == "$restart_id" ]] && continue
      peers+=("${PEER_ADDRS[$other]}")
    done
    IFS=, peer_csv="${peers[*]}"
    unset IFS
    start_node "$restart_id" "$restart_idx" "$peer_csv"
    wait_rpc "$restart_id" "$restart_idx"
    next_restart=$((SECONDS + RESTART_EVERY_SECONDS))
  fi

  sleep "$SAMPLE_SECONDS"
done

if (( VALIDATOR_COUNT > 0 && last_max_height <= 0 )); then
  warnings=$((warnings + 1))
  echo "[ec2-chaos] failure: no block progress"
fi
if (( VALIDATOR_COUNT > 0 && last_max_finalized <= 0 )); then
  warnings=$((warnings + 1))
  echo "[ec2-chaos] failure: no finality progress"
fi

passed=false
if (( warnings == 0 )); then
  passed=true
fi
echo "PASS=$passed warnings=$warnings max_height=$last_max_height max_finalized=$last_max_finalized max_gap_s=$max_gap max_lag_f=$max_finalized_lag max_lag_h=$max_height_lag logs=$LOG_ROOT"

if (( warnings > 0 )); then
  exit 1
fi
