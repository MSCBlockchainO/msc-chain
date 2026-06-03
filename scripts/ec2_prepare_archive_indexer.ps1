param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("archive", "indexer")]
    [string]$Role,

    [Parameter(Mandatory = $true)]
    [string]$HostName,

    [string]$User = "ubuntu",
    [string]$KeyPath = "C:\Users\Mohammad Talha\Downloads\msc-key.pem",
    [string]$RepoUrl = "https://github.com/mohammadtalha0226/msc-chain.git",
    [string]$Branch = "main",
    [string]$NodeID = "",
    [int]$P2PPort = 7011,
    [int]$ArchiveRPCPort = 26667,
    [int]$IndexerPort = 26780,
    [string]$Peers = "",
    [string]$ArchiveSourceRPC = "http://127.0.0.1:26667"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $KeyPath -PathType Leaf)) {
    throw "SSH key not found: $KeyPath"
}

function Quote-Sh {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value)
    return "'" + ($Value -replace "'", "'\''") + "'"
}

if ([string]::IsNullOrWhiteSpace($NodeID)) {
    $NodeID = if ($Role -eq "archive") { "ARCHIVE1" } else { "INDEXER1" }
}

$remote = @'
set -euo pipefail

if command -v apt-get >/dev/null 2>&1; then
  sudo apt-get update -y
  sudo apt-get install -y git curl build-essential ca-certificates
elif command -v dnf >/dev/null 2>&1; then
  sudo dnf install -y git curl gcc make ca-certificates
elif command -v yum >/dev/null 2>&1; then
  sudo yum install -y git curl gcc make ca-certificates
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required on this EC2. Install Go 1.22+ before running MSC archive/indexer." >&2
  exit 1
fi

mkdir -p "$HOME"
if [ ! -d "$HOME/msc-chain/.git" ]; then
  git clone "$REPO_URL" "$HOME/msc-chain"
fi
cd "$HOME/msc-chain"
git fetch origin "$BRANCH"
git checkout "$BRANCH"
git pull --ff-only origin "$BRANCH"
go build -o msc-node .
chmod +x msc-node
mkdir -p runtime-data/archive runtime-data/indexer runtime-logs/archive-indexer

write_archive_config() {
  cp config.toml config.archive.toml
  python3 - <<'PY'
from pathlib import Path

p = Path("config.archive.toml")
text = p.read_text(encoding="utf-8")

def replace_or_add(section, key, value):
    global text
    lines = text.splitlines()
    out = []
    in_section = False
    replaced = False
    inserted = False
    header = f"[{section}]"
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            if in_section and not replaced and not inserted:
                out.append(f"{key} = {value}")
                inserted = True
            in_section = stripped == header
        if in_section and stripped.startswith(f"{key} "):
            out.append(f"{key} = {value}")
            replaced = True
            continue
        out.append(line)
    if not replaced and not inserted:
        if header not in [ln.strip() for ln in lines]:
            out.append("")
            out.append(header)
        out.append(f"{key} = {value}")
    text = "\n".join(out) + "\n"

replace_or_add("storage", "history_profile", '"archive"')
replace_or_add("storage", "state_pruning_enabled", "false")
replace_or_add("sync", "history_mode", '"archive_full"')
p.write_text(text, encoding="utf-8")
PY
}

install_archive_service() {
  write_archive_config
  sudo tee /etc/systemd/system/msc-archive.service >/dev/null <<SERVICE
[Unit]
Description=MSC Archive Node ($NODE_ID)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$HOME/msc-chain
Environment=MSC_ALLOW_CONFIG_OVERRIDE=1
Environment=MSC_ALLOW_VALIDATOR_KEY_CREATE=0
ExecStart=$HOME/msc-chain/msc-node --mode=full --role=full --id=$NODE_ID --port=$P2P_PORT --datadir runtime-data/archive/$NODE_ID --rpcaddr 127.0.0.1:$ARCHIVE_RPC_PORT --config config.archive.toml --peers "$PEERS"
Restart=always
RestartSec=4
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SERVICE
  sudo systemctl daemon-reload
  sudo systemctl enable --now msc-archive.service
}

install_indexer_service() {
  sudo tee /etc/systemd/system/msc-indexer.service >/dev/null <<SERVICE
[Unit]
Description=MSC Explorer Indexer ($NODE_ID)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$HOME/msc-chain
Environment=MSC_INDEXER_SOURCE_RPC=$ARCHIVE_SOURCE_RPC
Environment=MSC_INDEXER_LISTEN=127.0.0.1:$INDEXER_PORT
Environment=MSC_INDEXER_DATADIR=$HOME/msc-chain/runtime-data/indexer/$NODE_ID
ExecStart=$HOME/msc-chain/msc-node indexer run --source $ARCHIVE_SOURCE_RPC --listen 127.0.0.1:$INDEXER_PORT --datadir runtime-data/indexer/$NODE_ID
Restart=always
RestartSec=4
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SERVICE
  sudo systemctl daemon-reload
  sudo systemctl enable --now msc-indexer.service
}

case "$ROLE" in
  archive)
    install_archive_service
    sleep 2
    curl -fsS "http://127.0.0.1:$ARCHIVE_RPC_PORT/storage/policy" || true
    echo
    echo "Archive service installed: msc-archive.service"
    echo "Gateway tunnel/env candidate: MSC_ARCHIVE_ENDPOINTS=http://127.0.0.1:$ARCHIVE_RPC_PORT"
    ;;
  indexer)
    install_indexer_service
    sleep 2
    curl -fsS "http://127.0.0.1:$INDEXER_PORT/indexer/status" || true
    echo
    echo "Indexer service installed: msc-indexer.service"
    echo "Gateway tunnel/env candidate: MSC_INDEXER_ENDPOINTS=http://127.0.0.1:$INDEXER_PORT"
    ;;
  *)
    echo "unknown role $ROLE" >&2
    exit 2
    ;;
esac

echo
echo "Useful checks:"
echo "  sudo systemctl status msc-${ROLE} --no-pager"
echo "  journalctl -u msc-${ROLE} -f"
'@

$remote = ($remote -replace "`r`n", "`n") -replace "`r", ""
$target = "$User@$HostName"
$envPrefix = @(
    "ROLE=$(Quote-Sh $Role)",
    "NODE_ID=$(Quote-Sh $NodeID)",
    "REPO_URL=$(Quote-Sh $RepoUrl)",
    "BRANCH=$(Quote-Sh $Branch)",
    "P2P_PORT=$(Quote-Sh ([string]$P2PPort))",
    "ARCHIVE_RPC_PORT=$(Quote-Sh ([string]$ArchiveRPCPort))",
    "INDEXER_PORT=$(Quote-Sh ([string]$IndexerPort))",
    "ARCHIVE_SOURCE_RPC=$(Quote-Sh $ArchiveSourceRPC)",
    "PEERS=$(Quote-Sh $Peers)"
) -join " "

Write-Host "Preparing MSC $Role role on $target..."
$remote | ssh -i $KeyPath -o StrictHostKeyChecking=no $target "$envPrefix bash -s"
if ($LASTEXITCODE -ne 0) {
    throw "remote $Role preparation failed"
}

Write-Host ""
Write-Host "Prepared $Role node $NodeID on $target."
if ($Role -eq "archive") {
    Write-Host "Archive local RPC: http://127.0.0.1:$ArchiveRPCPort"
    Write-Host "Expose through gateway with /archive-rpc only, not wallet defaults."
} else {
    Write-Host "Indexer local API: http://127.0.0.1:$IndexerPort/indexer/status"
    Write-Host "Explorer should use /indexer first, archive second, public full node last."
}
