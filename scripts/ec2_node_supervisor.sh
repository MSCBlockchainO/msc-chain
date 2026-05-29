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
P2P_EXTERNAL_ADDR="${P2P_EXTERNAL_ADDR:-/ip4/${PRIVATE_IP}/tcp/${P2P_PORT}}"
RPC_PORT="${RPC_PORT:-26657}"
RPC_ADDR="${RPC_ADDR:-${PRIVATE_IP}:${RPC_PORT}}"
HEALTHCHECK_ADDR="${HEALTHCHECK_ADDR:-}"
DURATION_SECONDS="${DURATION_SECONDS:-86400}"
RESTART_EVERY_SECONDS="${RESTART_EVERY_SECONDS:-0}"
RESTART_DOWN_SECONDS="${RESTART_DOWN_SECONDS:-8}"
MSC_VALIDATOR_PASSWORD="${MSC_VALIDATOR_PASSWORD:-mainnet-smoke-password}"
NODE_NICE="${NODE_NICE:-0}"
OOM_SCORE_ADJ="${OOM_SCORE_ADJ:--500}"
GOMAXPROCS="${GOMAXPROCS:-}"
GOMEMLIMIT="${GOMEMLIMIT:-}"
HEALTHCHECK_SECONDS="${HEALTHCHECK_SECONDS:-0}"
STARTUP_HEALTH_GRACE_SECONDS="${STARTUP_HEALTH_GRACE_SECONDS:-300}"
UNHEALTHY_RESTART_SECONDS="${UNHEALTHY_RESTART_SECONDS:-120}"
START_BACKOFF_SECONDS="${START_BACKOFF_SECONDS:-3}"
MAX_RESTART_BACKOFF_SECONDS="${MAX_RESTART_BACKOFF_SECONDS:-60}"

mkdir -p "$DATA_DIR" "$LOG_ROOT"
NODE_LOG="$LOG_ROOT/${NODE_ID}.node.log"
SUPERVISOR_LOG="$LOG_ROOT/${NODE_ID}.supervisor.log"
SUPERVISOR_PID_FILE="$LOG_ROOT/${NODE_ID}.supervisor.pid"
PID_FILE="$LOG_ROOT/${NODE_ID}.pid"

NODE_PID=""
restart_backoff="$START_BACKOFF_SECONDS"
unhealthy_since=0
last_start_seconds=0

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
  local env_args=(
    "MSC_ALLOW_VALIDATOR_KEY_CREATE=1"
    "MSC_VALIDATOR_PASSWORD=$MSC_VALIDATOR_PASSWORD"
    "MSC_AUTH_OPEN_BROWSER=0"
    "MSC_P2P_EXTERNAL_ADDR=$P2P_EXTERNAL_ADDR"
  )
  if [[ -n "$PEERS" ]]; then
    peer_args=(--peers "$PEERS")
    env_args+=("MSC_P2P_PEERS=$PEERS")
  fi
  if [[ -n "$GOMAXPROCS" ]]; then
    env_args+=("GOMAXPROCS=$GOMAXPROCS")
  fi
  if [[ -n "$GOMEMLIMIT" ]]; then
    env_args+=("GOMEMLIMIT=$GOMEMLIMIT")
  fi
  log "start node=$NODE_ID role=$ROLE rpc=$RPC_ADDR p2p=$P2P_PORT"
  nohup env "${env_args[@]}" nice -n "$NODE_NICE" "$BINARY" \
      --mode=full \
      --role="$ROLE" \
      --id="$NODE_ID" \
      --port="$P2P_PORT" \
      --datadir="$DATA_DIR" \
      --rpcaddr "$RPC_ADDR" \
      --p2p-external "$P2P_EXTERNAL_ADDR" \
      --config "$CONFIG" \
      "${peer_args[@]}" \
      > "$NODE_LOG" 2>&1 &
  NODE_PID="$!"
  last_start_seconds="$SECONDS"
  unhealthy_since=0
  echo "$NODE_PID" > "$PID_FILE"
  if [[ -w "/proc/$NODE_PID/oom_score_adj" ]]; then
    printf '%s' "$OOM_SCORE_ADJ" > "/proc/$NODE_PID/oom_score_adj" 2>/dev/null || true
  fi
  log "node pid=$NODE_PID nice=$NODE_NICE oom_score_adj=$OOM_SCORE_ADJ gomaxprocs=${GOMAXPROCS:-auto} gomemlimit=${GOMEMLIMIT:-auto}"
}

node_healthy() {
  if [[ "$HEALTHCHECK_SECONDS" -le 0 ]]; then
    return 0
  fi
  local addr="$HEALTHCHECK_ADDR"
  if [[ -z "$addr" ]]; then
    addr="$RPC_ADDR"
    if [[ "$addr" == 0.0.0.0:* ]]; then
      addr="127.0.0.1:${addr#0.0.0.0:}"
    elif [[ "$addr" == "[::]:"* ]]; then
      addr="127.0.0.1:${addr#"[::]:"}"
    elif [[ "$addr" == :* ]]; then
      addr="127.0.0.1${addr}"
    fi
  fi
  curl -fsS --max-time 3 "http://${addr}/status" >/dev/null 2>&1
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
    exit_code=0
    if wait "$NODE_PID" 2>/dev/null; then
      exit_code=0
    else
      exit_code="$?"
    fi
    log "node exited unexpectedly exit_code=$exit_code; restarting after ${restart_backoff}s"
    NODE_PID=""
    sleep "$restart_backoff"
    start_node
    if (( restart_backoff < MAX_RESTART_BACKOFF_SECONDS )); then
      restart_backoff=$((restart_backoff * 2))
      if (( restart_backoff > MAX_RESTART_BACKOFF_SECONDS )); then
        restart_backoff="$MAX_RESTART_BACKOFF_SECONDS"
      fi
    fi
    unhealthy_since=0
  else
    restart_backoff="$START_BACKOFF_SECONDS"
  fi

  if (( HEALTHCHECK_SECONDS > 0 && SECONDS - last_start_seconds >= STARTUP_HEALTH_GRACE_SECONDS )); then
    if node_healthy; then
      unhealthy_since=0
    else
      if (( unhealthy_since == 0 )); then
        unhealthy_since="$SECONDS"
        log "healthcheck failed; waiting up to ${UNHEALTHY_RESTART_SECONDS}s before restart"
      elif (( SECONDS - unhealthy_since >= UNHEALTHY_RESTART_SECONDS )); then
        log "healthcheck unhealthy for $((SECONDS - unhealthy_since))s; restarting"
        stop_node
        sleep "$START_BACKOFF_SECONDS"
        start_node
        unhealthy_since=0
      fi
    fi
  fi

  if (( RESTART_EVERY_SECONDS > 0 && SECONDS >= next_restart )); then
    log "scheduled restart begin"
    stop_node
    sleep "$RESTART_DOWN_SECONDS"
    start_node
    next_restart=$((SECONDS + RESTART_EVERY_SECONDS))
    log "scheduled restart done"
  fi

  if (( HEALTHCHECK_SECONDS > 0 )); then
    sleep "$HEALTHCHECK_SECONDS"
  else
    sleep 5
  fi
done

elapsed=$((SECONDS - start_at))
log "duration complete elapsed=${elapsed}s"
