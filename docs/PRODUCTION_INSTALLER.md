# MSC production installer

Public one-line install command:

```bash
curl -fsSL https://mscblockexplorer.in/install.sh | bash
```

Non-interactive example:

```bash
curl -fsSL https://mscblockexplorer.in/install.sh | bash -s -- --role validator --id V1 --auto-start
```

## Release layout

Build release artifacts from the repo root:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build_mainnet_release.ps1 -VersionTag v1.0.0-mainnet
```

Publish these paths to the website:

```text
https://mscblockexplorer.in/install.sh                 <- scripts/install.sh
https://mscblockexplorer.in/releases/latest.json       <- dist/latest.json
https://mscblockexplorer.in/releases/<version>/...     <- dist/<version>/*
```

`latest.json` tells the installer which Linux binary to download for `amd64`
or `arm64`, plus SHA-256 checksums for the binary and support files. The
installer refuses to install if a published checksum does not match.

## What the installer does

1. Detects Linux architecture: `amd64` or `arm64`.
2. Downloads `releases/latest.json`.
3. Selects the latest matching MSC Linux binary.
4. Verifies SHA-256 for the binary and support files.
5. Installs it as `/usr/local/bin/msc`.
6. Installs support files under `/usr/local/share/msc`.
7. Runs `msc version`.
8. Starts an onboarding wizard:
   - Candidate
   - Validator
   - Full Node
   - Archive Node

## Local staging test

Serve a release directory locally:

```bash
cd dist
python3 -m http.server 8088
```

Then test without touching production URLs:

```bash
MSC_RELEASE_BASE_URL=http://127.0.0.1:8088 \
bash scripts/install.sh --role full --id TESTNODE --no-node-setup
```

For a full node setup test, remove `--no-node-setup`.

## Production note

SHA-256 protects users from corrupted or swapped artifacts referenced by the
published release metadata. For a stronger supply-chain model, add signed
release metadata next: keep `latest.json` over HTTPS, and verify an offline
Ed25519 signature before trusting its checksum list.
