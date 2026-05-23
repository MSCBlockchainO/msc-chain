#!/usr/bin/env bash
set -euo pipefail

NODE_ID="${NODE_ID:?NODE_ID is required}"
ROLE="${ROLE:-validator}"
PEERS="${PEERS:-}"
PRIVATE_IP="${PRIVATE_IP:-127.0.0.1}"
BINARY="${BINARY:-./msc-chain}"
CONFIG="${CONFIG:-config.toml}"
DATA_DIR="${DATA_DIR:-runtime-data/distributed/${NODE_ID}}"
LOG_ROOT="${LOG_ROOT:-runtime-logs/distributed}"
P2P_PORT="${P2P_PORT:-7001}"
RPC_PORT="${RPC_PORT:-26657}"
RPC_ADDR="${RPC_ADDR:-${PRIVATE_IP}:${RPC_PORT}}"
DURATION_SECONDS="${DURATION_SECONDS:-86400}"
RESTART_EVERY_SECONDS="${RESTART_EVERY_SECONDS:-0}"
RESTART_DOWN_SECONDS="${RESTART_DOWN_SECONDS:-8}"
MSC_VALIDATOR_PASSWORD="${MSC_VALIDATOR_PASSWORD:-mainnet-smoke-password}"

mkdir -p "$DATA_DIR" "$LOG_ROOT"
NODE_LOG="$LOG_ROOT/${NODE_ID}.node.log"
SUPERVISOR_LOG="$LOG_ROOT/${NODE_ID}.supervisor.log"
SUPERVISOR_PID_FILE="$LOG_ROOT/${NODE_ID}.supervisor.pid"
PID_FILE="$LOG_ROOT/${NODE_ID}.pid"

NODE_PID=""

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$SUPERVISOR_LOG"
}

stop_node() {
  if [[ -n "${NODE_PID:-}" ]] && kill -0 "$NODE_PID" >/dev/null 2>&1; then
    kill "$NODE_PID" >/dev/null 2>&1 || true
    sleep 2
    kill -9 "$NODE_PID" >/dev/null 2>&1 || true
    wait "$NODE_PID" 2>/dev/null || true
  fi
  NODE_PID=""
  rm -f "$PID_FILE"
}

start_node() {
  local peer_args=()
  if [[ -n "$PEERS" ]]; then
    peer_args=(--peers "$PEERS")
  fi
  log "start node=$NODE_ID role=$ROLE rpc=$RPC_ADDR p2p=$P2P_PORT"
  MSC_ALLOW_VALIDATOR_KEY_CREATE=1 \
  MSC_VALIDATOR_PASSWORD="$MSC_VALIDATOR_PASSWORD" \
  MSC_AUTH_OPEN_BROWSER=0 \
    nohup "$BINARY" \
      --mode=full \
      --role="$ROLE" \
      --id="$NODE_ID" \
      --port="$P2P_PORT" \
      --datadir="$DATA_DIR" \
      --rpcaddr "$RPC_ADDR" \
      --config "$CONFIG" \
      "${peer_args[@]}" \
      > "$NODE_LOG" 2>&1 &
  NODE_PID="$!"
  echo "$NODE_PID" > "$PID_FILE"
}

echo "$$" > "$SUPERVISOR_PID_FILE"
trap 'log "stop requested"; stop_node; rm -f "$SUPERVISOR_PID_FILE"' EXIT INT TERM

start_at="$SECONDS"
deadline=$((SECONDS + DURATION_SECONDS))
next_restart=0
if (( RESTART_EVERY_SECONDS > 0 )); then
  next_restart=$((SECONDS + RESTART_EVERY_SECONDS))
fi

start_node

while (( SECONDS < deadline )); do
  if [[ -n "${NODE_PID:-}" ]] && ! kill -0 "$NODE_PID" >/dev/null 2>&1; then
    log "node exited unexpectedly; restarting"
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=""
    start_node
  fi

  if (( RESTART_EVERY_SECONDS > 0 && SECONDS >= next_restart )); then
    log "scheduled restart begin"
    stop_node
    sleep "$RESTART_DOWN_SECONDS"
    start_node
    next_restart=$((SECONDS + RESTART_EVERY_SECONDS))
    log "scheduled restart done"
  fi

  sleep 5
done

elapsed=$((SECONDS - start_at))
log "duration complete elapsed=${elapsed}s"
