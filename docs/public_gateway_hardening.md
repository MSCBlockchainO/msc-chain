# MSC Public Gateway Hardening

Production public access must terminate at a full node gateway, not at validator
RPC ports.

## Required Topology

- Validator RPC: private IP or localhost only.
- Public RPC: full node only, behind nginx.
- Explorer and wallet: public only through the full node gateway, on separate subdomains.
- HTTPS/domain: mandatory for production.
- Rate limits: mandatory at nginx and node RPC layers.
- Metrics: scrape privately; do not expose `/metrics` through the public gateway.

## Domain

Production domains:

```text
explorer.mscblockexplorer.in
wallet.mscblockexplorer.in
```

Both A records must point to the public full node gateway IP before HTTPS can
be issued:

```text
explorer.mscblockexplorer.in -> 50.19.167.221
wallet.mscblockexplorer.in -> 50.19.167.221
```

## Deploy

Set the DTL IDE password if the IDE is enabled:

```powershell
$env:MSC_IDE_PASSWORD = "<strong-password>"
.\scripts\ec2_public_ui_gateway.ps1 `
  -GatewayHost "50.19.167.221" `
  -GatewayUser "ubuntu" `
  -ExplorerDomain "explorer.mscblockexplorer.in" `
  -WalletDomain "wallet.mscblockexplorer.in" `
  -RpcTarget "127.0.0.1:26665"
```

The script fails if DNS does not point to the gateway. For a temporary pre-DNS
dry run only:

```powershell
.\scripts\ec2_public_ui_gateway.ps1 `
  -GatewayHost "50.19.167.221" `
  -GatewayUser "ubuntu" `
  -ExplorerDomain "explorer.mscblockexplorer.in" `
  -WalletDomain "wallet.mscblockexplorer.in" `
  -RpcTarget "127.0.0.1:26665" `
  -AllowHttpOnlyUntilDNS
```

Do not use HTTP-only mode for production.

## Security Group

Public full node gateway:

- Allow TCP `80` and `443` from `0.0.0.0/0`.
- Allow P2P TCP `7001` from validator/full-node peers.
- Do not allow public TCP `26665`; nginx proxies to `127.0.0.1:26665`.

Validators:

- Allow P2P from trusted peer subnets.
- Do not allow public validator RPC ports such as `26657`.
- Bind validator RPC to private IP or `127.0.0.1`.
