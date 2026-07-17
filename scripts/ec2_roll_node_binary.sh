#!/usr/bin/env bash
set -euo pipefail

NODE_ID="${1:?usage: ec2_roll_node_binary.sh NODE_ID RPC_ADDRESS}"
RPC_ADDRESS="${2:?usage: ec2_roll_node_binary.sh NODE_ID RPC_ADDRESS}"
NODE_ROOT="${MSC_NODE_ROOT:-$HOME/msc-chain}"
NEW_BINARY="${MSC_NEW_BINARY:-./msc-node.new}"
ACTIVE_BINARY="${MSC_ACTIVE_BINARY:-./msc-node}"
LOG_ROOT="${MSC_LOG_ROOT:-runtime-logs/distributed}"
GOMAXPROCS_VALUE="${MSC_ROLL_GOMAXPROCS:-2}"
GOMEMLIMIT_VALUE="${MSC_ROLL_GOMEMLIMIT:-3502MiB}"
GOGC_VALUE="${MSC_ROLL_GOGC:-75}"
LEDGER_CACHE_DEPTH="${MSC_ROLL_LEDGER_CACHE_DEPTH:-2}"
HEALTH_TIMEOUT_SECONDS="${MSC_ROLL_HEALTH_TIMEOUT_SECONDS:-120}"

cd "$NODE_ROOT"

if [[ ! -f "$NEW_BINARY" ]]; then
  printf 'missing staged binary: %s\n' "$NEW_BINARY" >&2
  exit 1
fi

main_pid="$(
  ps -eo pid=,args= |
    awk -v id="$NODE_ID" '$0 ~ /(^|[[:space:]\/])msc-node([[:space:]]|$)/ && $0 ~ /--mode=full([[:space:]]|$)/ && $0 ~ ("--id=" id "([[:space:]]|$)") {print $1; exit}'
)"
if [[ -z "$main_pid" || ! -r "/proc/$main_pid/cmdline" ]]; then
  printf 'no running node command found for id=%s\n' "$NODE_ID" >&2
  exit 1
fi

readarray -d '' node_command < "/proc/$main_pid/cmdline"
if (( ${#node_command[@]} == 0 )); then
  printf 'could not capture running command for id=%s pid=%s\n' "$NODE_ID" "$main_pid" >&2
  exit 1
fi

data_dir=""
for ((i = 0; i < ${#node_command[@]}; i++)); do
  case "${node_command[$i]}" in
    --datadir=*)
      data_dir="${node_command[$i]#--datadir=}"
      ;;
    --datadir)
      if (( i + 1 < ${#node_command[@]} )); then
        data_dir="${node_command[$((i + 1))]}"
      fi
      ;;
  esac
done
if [[ -z "$data_dir" ]]; then
  printf 'could not resolve data directory from running command for id=%s\n' "$NODE_ID" >&2
  exit 1
fi
process_lock="${data_dir}/node_${NODE_ID}/node.process.lock"

chmod +x "$NEW_BINARY"
new_hash="$(sha256sum "$NEW_BINARY" | awk '{print $1}')"
timestamp="$(date -u +%Y%m%d%H%M%S)"
if [[ -x "$ACTIVE_BINARY" ]]; then
  cp -p "$ACTIVE_BINARY" "${ACTIVE_BINARY}.prev-${timestamp}"
fi
mv "$NEW_BINARY" "$ACTIVE_BINARY"
chmod +x "$ACTIVE_BINARY"

# Stop any legacy supervisor first. Older supervisor versions handled TERM
# cleanup but returned to their loop, so use a bounded wait and KILL fallback.
mapfile -t supervisor_pids < <(pgrep -f '^bash scripts/ec2_node_supervisor.sh' || true)
for supervisor_pid in "${supervisor_pids[@]}"; do
  [[ -z "$supervisor_pid" ]] && continue
  kill -TERM "$supervisor_pid" 2>/dev/null || true
done
for _ in $(seq 1 10); do
  remaining=0
  for supervisor_pid in "${supervisor_pids[@]}"; do
    if [[ -n "$supervisor_pid" ]] && kill -0 "$supervisor_pid" 2>/dev/null; then
      remaining=1
    fi
  done
  (( remaining == 0 )) && break
  sleep 1
done
for supervisor_pid in "${supervisor_pids[@]}"; do
  [[ -z "$supervisor_pid" ]] && continue
  kill -KILL "$supervisor_pid" 2>/dev/null || true
done

kill -TERM "$main_pid" 2>/dev/null || true
for _ in $(seq 1 20); do
  ! kill -0 "$main_pid" 2>/dev/null && break
  sleep 1
done
kill -KILL "$main_pid" 2>/dev/null || true

for _ in $(seq 1 10); do
  if ! ps -eo args= | awk -v id="$NODE_ID" '$0 ~ /(^|[[:space:]\/])msc-node([[:space:]]|$)/ && $0 ~ /--mode=full([[:space:]]|$)/ && $0 ~ ("--id=" id "([[:space:]]|$)") {found=1} END {exit found ? 0 : 1}'; then
    break
  fi
  sleep 1
done

lock_available=0
for _ in $(seq 1 30); do
  if flock -n "$process_lock" true 2>/dev/null; then
    lock_available=1
    break
  fi
  sleep 1
done
if (( lock_available == 0 )); then
  printf 'node process lock did not release id=%s lock=%s\n' "$NODE_ID" "$process_lock" >&2
  exit 1
fi

mkdir -p "$LOG_ROOT"
node_log="$LOG_ROOT/${NODE_ID}.node.log"
printf '[%s] binary rollout id=%s sha256=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$NODE_ID" "$new_hash" >> "$node_log"
nohup env \
  MSC_ALLOW_CONFIG_OVERRIDE=1 \
  GOMAXPROCS="$GOMAXPROCS_VALUE" \
  GOMEMLIMIT="$GOMEMLIMIT_VALUE" \
  GOGC="$GOGC_VALUE" \
  MSC_EXECUTION_LEDGER_CACHE_DEPTH="$LEDGER_CACHE_DEPTH" \
  "${node_command[@]}" >> "$node_log" 2>&1 < /dev/null &
new_pid="$!"
printf '%s\n' "$new_pid" > "$LOG_ROOT/${NODE_ID}.pid"

deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  if ! kill -0 "$new_pid" 2>/dev/null; then
    printf 'new node process exited before health check id=%s pid=%s\n' "$NODE_ID" "$new_pid" >&2
    exit 1
  fi
  if curl -fsS --max-time 5 "http://${RPC_ADDRESS}/status" >/dev/null 2>&1; then
    printf 'rollout healthy id=%s pid=%s sha256=%s rpc=%s\n' "$NODE_ID" "$new_pid" "$new_hash" "$RPC_ADDRESS"
    exit 0
  fi
  sleep 2
done

printf 'rollout health timeout id=%s pid=%s rpc=%s\n' "$NODE_ID" "$new_pid" "$RPC_ADDRESS" >&2
exit 1
