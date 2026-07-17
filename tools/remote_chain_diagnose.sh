#!/usr/bin/env bash
set -u

section() {
  printf '\n### %s\n' "$1"
}

short_file() {
  local path="$1"
  if [ -f "$path" ]; then
    printf '%s=' "$path"
    head -c 300 "$path" 2>/dev/null | tr '\n' ' '
    printf '\n'
  fi
}

section "host"
hostname 2>/dev/null || true
whoami 2>/dev/null || true
date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || true
uptime 2>/dev/null || true
uname -a 2>/dev/null || true

section "systemd"
if command -v systemctl >/dev/null 2>&1; then
  systemctl list-units --type=service --all --no-pager 2>/dev/null | grep -Ei 'msc|chain|validator|node' || true
  for svc in $(systemctl list-units --type=service --all --no-legend --no-pager 2>/dev/null | awk '{print $1}' | grep -Ei '^msc|msc|chain'); do
    echo "--- service $svc"
    systemctl is-active "$svc" 2>/dev/null || true
    systemctl is-enabled "$svc" 2>/dev/null || true
    systemctl show "$svc" -p Id -p ActiveState -p SubState -p ExecStart -p WorkingDirectory -p MainPID --no-pager 2>/dev/null || true
  done
else
  echo "systemctl unavailable"
fi

section "processes"
ps -eo pid,ppid,stat,etime,comm,args 2>/dev/null | grep -Ei 'msc-node|msc-chain|ec2_node_supervisor|validator|runtime-data/distributed' | grep -v grep || true

section "install manifests"
find "$HOME/.msc/nodes" "$HOME/msc-chain/runtime-data/distributed" "$HOME/msc-chain" -maxdepth 4 \( -name install_manifest.json -o -name config.toml -o -name config.mpc.toml -o -name "*.env" -o -name "mpc-setup.out" -o -name "validator.pub" -o -name "fingerprint.lock" \) 2>/dev/null | sort | while read -r f; do
  short_file "$f"
done

section "binaries"
for b in "$HOME"/.msc/nodes/*/msc-node "$HOME"/msc-chain/msc-node "$HOME"/msc-chain/msc-node.*.linux "$HOME"/msc-chain/bin/msc-chain-linux-amd64; do
  [ -f "$b" ] || continue
  ls -l "$b" 2>/dev/null || true
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$b" 2>/dev/null || true
  fi
done

section "rpc"
for port in 26657 26658 26659 26660 26661 26662 26663 26664 26665 26666 26667 26668 26669 26670 26671 26672 26673 26674; do
  base="http://127.0.0.1:${port}"
  if command -v curl >/dev/null 2>&1; then
    code=$(curl -sS --max-time 2 -o /tmp/msc_status_${port}.json -w "%{http_code}" "$base/status" 2>/dev/null || true)
    if [ "$code" != "000" ] && [ -s /tmp/msc_status_${port}.json ]; then
      echo "--- $base/status code=$code"
      head -c 2000 /tmp/msc_status_${port}.json 2>/dev/null
      printf '\n'
      for ep in healthz validators v1/validators metrics; do
        epcode=$(curl -sS --max-time 2 -o /tmp/msc_${port}_${ep//\//_}.json -w "%{http_code}" "$base/$ep" 2>/dev/null || true)
        if [ "$epcode" != "000" ] && [ -s /tmp/msc_${port}_${ep//\//_}.json ]; then
          echo "--- $base/$ep code=$epcode"
          head -c 1200 /tmp/msc_${port}_${ep//\//_}.json 2>/dev/null
          printf '\n'
        fi
      done
    fi
  fi
done
rm -f /tmp/msc_status_*.json /tmp/msc_*_*.json 2>/dev/null || true

section "logs"
for f in "$HOME"/.msc/nodes/*/logs/*.out "$HOME"/.msc/nodes/*/logs/*.log "$HOME"/msc-chain/runtime-logs/distributed/*.node.log "$HOME"/msc-chain/runtime-logs/distributed/*.bootstrap.log "$HOME"/msc-chain/runtime-logs/distributed/*.supervisor.log; do
  [ -f "$f" ] || continue
  echo "--- log $f"
  tail -n 80 "$f" 2>/dev/null | grep -Ei 'HALT|halt|panic|fatal|error|WARN|COMMIT|height|validator|quorum|mismatch|stuck|stall|sync|snapshot|registry|proposal|round' || tail -n 40 "$f" 2>/dev/null || true
done

section "disk"
df -h 2>/dev/null || true
