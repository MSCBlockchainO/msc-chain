import json
import os
import socket
import subprocess
import sys
import time
import urllib.request

label = os.environ.get("MSC_LABEL", "")
hosts = ["127.0.0.1"]
try:
    ips = subprocess.check_output(["hostname", "-I"], text=True, timeout=2).split()
    hosts.extend(ip for ip in ips if ip and ip not in hosts)
except Exception:
    pass

ports = list(range(26657, 26675))
seen = set()
rows = []
for host in hosts:
    for port in ports:
        base = f"http://{host}:{port}"
        try:
            with urllib.request.urlopen(base + "/status", timeout=2) as r:
                status = json.load(r)
        except Exception:
            continue
        key = (status.get("chain_id"), status.get("height"), status.get("finalized"), port)
        if key in seen:
            continue
        seen.add(key)
        validators = {}
        try:
            with urllib.request.urlopen(base + "/v1/validators", timeout=2) as r:
                validators = json.load(r).get("data", {})
        except Exception:
            pass
        rows.append({
            "label": label,
            "host": socket.gethostname(),
            "user": os.environ.get("USER", ""),
            "bind": f"{host}:{port}",
            "chain_id": status.get("chain_id"),
            "height": status.get("height"),
            "finalized": status.get("finalized"),
            "network_best_height": status.get("network_best_height"),
            "consensus_mode": status.get("consensus_mode"),
            "consensus_ready": status.get("consensus_ready"),
            "consensus_running": status.get("consensus_running"),
            "block_production_status": status.get("block_production_status"),
            "block_production_reason": status.get("block_production_reason"),
            "detector_mode": status.get("consensus_detector_mode"),
            "detector_reason": status.get("consensus_detector_reason"),
            "active_ready_count": status.get("active_ready_count"),
            "committee_height": status.get("committee_height"),
            "committee_live_count": status.get("committee_live_count"),
            "committee_offline_count": status.get("committee_offline_count"),
            "committee_size": status.get("committee_size"),
            "committee_target": status.get("committee_target"),
            "activation_blocker_reason": status.get("activation_blocker_reason"),
            "online_validators": validators.get("online_validators"),
            "offline_validators": validators.get("offline_validators"),
            "pending_add": validators.get("pending_add"),
            "pending_remove": validators.get("pending_remove"),
        })

print(json.dumps({
    "label": label,
    "time": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "rows": rows,
}, sort_keys=True))
