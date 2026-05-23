#!/usr/bin/env bash
set -euo pipefail

NODES="${NODES:?NODES is required, e.g. A=http://10.0.0.1:26657,B=http://10.0.0.2:26657}"
DURATION_SECONDS="${DURATION_SECONDS:-86400}"
WARMUP_SECONDS="${WARMUP_SECONDS:-300}"
SAMPLE_SECONDS="${SAMPLE_SECONDS:-10}"
MAX_BLOCK_GAP_SECONDS="${MAX_BLOCK_GAP_SECONDS:-90}"
MAX_FINALIZED_LAG_BLOCKS="${MAX_FINALIZED_LAG_BLOCKS:-32}"
MAX_HEIGHT_LAG_BLOCKS="${MAX_HEIGHT_LAG_BLOCKS:-48}"
MIN_REACHABLE_NODES="${MIN_REACHABLE_NODES:-4}"
LOG_ROOT="${LOG_ROOT:-runtime-logs/distributed}"
RUN_ID="${RUN_ID:-ec2-distributed-$(date -u +%Y%m%d-%H%M%S)}"

mkdir -p "$LOG_ROOT"
EVENTS_PATH="$LOG_ROOT/${RUN_ID}.events.jsonl"
SUMMARY_PATH="$LOG_ROOT/${RUN_ID}.summary.json"
PID_PATH="$LOG_ROOT/${RUN_ID}.monitor.pid"

echo "$$" > "$PID_PATH"
trap 'rm -f "$PID_PATH"' EXIT INT TERM

IFS=',' read -r -a NODE_PAIRS <<< "$NODES"

json_event() {
  local type="$1"
  local payload="$2"
  python3 - "$type" "$payload" >> "$EVENTS_PATH" <<'PY'
import json,sys,time
typ=sys.argv[1]
payload=json.loads(sys.argv[2])
payload["type"]=typ
payload["ts"]=int(time.time())
print(json.dumps(payload,sort_keys=True))
PY
}

read_status() {
  local pair="$1"
  local id="${pair%%=*}"
  local url="${pair#*=}"
  local body
  if ! body="$(curl -fsS --max-time 4 "$url/status" 2>/dev/null)"; then
    echo "$id 0 0 0 0 0"
    return
  fi
  python3 - "$id" "$body" <<'PY'
import json,sys
node_id=sys.argv[1]
try:
    s=json.loads(sys.argv[2])
    height=int(s.get("height") or 0)
    finalized=int(s.get("finalized_height") or height)
    peers=int(s.get("peers") or 0)
    ready=1 if s.get("ready") else 0
    print(node_id,1,height,finalized,peers,ready)
except Exception:
    print(node_id,0,0,0,0,0)
PY
}

start_at="$SECONDS"
deadline=$((SECONDS + DURATION_SECONDS))
last_progress_at="$SECONDS"
last_max_finalized=0
last_max_height=0
max_gap=0
max_finalized_lag=0
max_height_lag=0
warnings=0
samples=0

json_event "monitor_start" "{\"run_id\":\"$RUN_ID\",\"duration_seconds\":$DURATION_SECONDS,\"warmup_seconds\":$WARMUP_SECONDS,\"nodes\":${#NODE_PAIRS[@]}}"

while (( SECONDS < deadline )); do
  samples=$((samples + 1))
  reachable=0
  ready_count=0
  min_height=0
  max_height=0
  min_finalized=0
  max_finalized=0
  rows=()

  for pair in "${NODE_PAIRS[@]}"; do
    row="$(read_status "$pair")"
    rows+=("$row")
    read -r id ok height finalized peers ready <<< "$row"
    if (( ok == 1 )); then
      reachable=$((reachable + 1))
      (( ready == 1 )) && ready_count=$((ready_count + 1))
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
  (( SECONDS - start_at >= WARMUP_SECONDS )) && past_warmup=1
  if (( past_warmup == 1 && gap > MAX_BLOCK_GAP_SECONDS )); then warnings=$((warnings + 1)); fi
  if (( past_warmup == 1 && reachable < MIN_REACHABLE_NODES )); then warnings=$((warnings + 1)); fi
  if (( past_warmup == 1 && finalized_lag > MAX_FINALIZED_LAG_BLOCKS )); then warnings=$((warnings + 1)); fi
  if (( past_warmup == 1 && height_lag > MAX_HEIGHT_LAG_BLOCKS )); then warnings=$((warnings + 1)); fi

  compact_rows="$(printf '%s\n' "${rows[@]}" | tr '\n' ';')"
  echo "sample=$samples reachable=$reachable ready=$ready_count height=$min_height-$max_height finalized=$min_finalized-$max_finalized gap_s=$gap lag_f=$finalized_lag lag_h=$height_lag warnings=$warnings rows=[$compact_rows]"
  json_event "status_sample" "{\"sample\":$samples,\"reachable\":$reachable,\"ready\":$ready_count,\"min_height\":$min_height,\"max_height\":$max_height,\"min_finalized\":$min_finalized,\"max_finalized\":$max_finalized,\"gap_seconds\":$gap,\"finalized_lag\":$finalized_lag,\"height_lag\":$height_lag,\"warnings\":$warnings}"

  sleep "$SAMPLE_SECONDS"
done

if (( last_max_height <= 0 )); then warnings=$((warnings + 1)); fi
if (( last_max_finalized <= 0 )); then warnings=$((warnings + 1)); fi

passed=false
if (( warnings == 0 )); then
  passed=true
fi

python3 - "$SUMMARY_PATH" "$RUN_ID" "$passed" "$warnings" "$samples" "$last_max_height" "$last_max_finalized" "$max_gap" "$max_finalized_lag" "$max_height_lag" <<'PY'
import json,sys
path,run_id,passed,warnings,samples,max_h,max_f,max_gap,max_lag_f,max_lag_h=sys.argv[1:]
summary={
  "run_id": run_id,
  "passed": passed.lower()=="true",
  "warning_count": int(warnings),
  "samples": int(samples),
  "max_height": int(max_h),
  "max_finalized": int(max_f),
  "max_no_progress_seconds": int(max_gap),
  "max_finalized_lag_blocks": int(max_lag_f),
  "max_height_lag_blocks": int(max_lag_h),
}
with open(path,"w",encoding="utf-8") as f:
    json.dump(summary,f,indent=2,sort_keys=True)
print(json.dumps(summary,sort_keys=True))
PY

json_event "monitor_stop" "{\"run_id\":\"$RUN_ID\",\"passed\":$passed,\"warnings\":$warnings,\"max_height\":$last_max_height,\"max_finalized\":$last_max_finalized}"

if (( warnings > 0 )); then
  exit 1
fi
