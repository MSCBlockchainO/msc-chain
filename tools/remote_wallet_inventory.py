import json
import os
import pathlib

roots = [pathlib.Path.home() / ".msc", pathlib.Path.home() / "msc-chain"]
matches = []
for root in roots:
    if not root.exists():
        continue
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        name = path.name.lower()
        if not (name.endswith(".json") and ("wallet" in name or "secure" in name)):
            continue
        try:
            data = json.loads(path.read_text(errors="ignore"))
        except Exception:
            continue
        address = data.get("address")
        pub = data.get("publicKey") or data.get("public_key")
        if address or pub:
            matches.append({
                "path": str(path),
                "address": address,
                "public_key": pub,
                "has_crypto": bool(data.get("crypto")),
                "mode": oct(path.stat().st_mode & 0o777),
            })

print(json.dumps({
    "label": os.environ.get("MSC_LABEL", ""),
    "wallets": matches,
}, sort_keys=True))
