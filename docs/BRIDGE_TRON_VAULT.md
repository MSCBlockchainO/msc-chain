# MSC TRON Bridge Vault

`contracts/bridge/contracts/MSCBridgeTronVault.sol` is the TRC20 custody
contract for MSC bridge routes. It shares the lock, limits, pause, escrow,
committee rotation, and replay protections of `MSCBridgeVault`, but verifies
withdrawal signatures with TRON TIP-712 semantics.

This source has not received an external smart-contract audit. Do not custody
mainnet funds until every gate in `docs/BRIDGE_PRODUCTION_GATE.md` is proven.

## TRON-Specific Security Model

- The vault starts paused. A token route must be configured before a separate
  governance action can unpause it.
- Governance, guardian, one-time deployer, release executor, and all committee
  keys must be separate. The release executor pays energy but cannot create a
  valid unlock without the threshold committee.
- TRON TIP-712 uses EIP-712 encoding with TRON address conversion and the low
  32 bits of `block.chainid`. The deployed vault reports this value through
  `tip712ChainId()` and exposes the matching domain.
- The deployment command verifies the genesis-derived TIP-712 chain ID,
  `getAllowTvmCancun`, the network maximum fee limit, solidified deployment,
  runtime code, paused state, MSC chain binding, and committee state.
- Unlocks bind the canonical MSC burn transaction and log index to one
  withdrawal ID. That ID is consumed permanently after successful release.
- The Ed25519 observer and DTL committees are not TRON release signers. Never
  reuse people, devices, seeds, backup channels, or signing infrastructure.

Official protocol and SDK references:

- https://github.com/tronprotocol/tips/blob/master/tip-712.md
- https://tronweb.network/docu/docs/API%20List/trx/signTypedData/
- https://developers.tron.network/docs/account
- https://developers.tron.network/docs/connect-to-the-tron-network
- https://developers.tron.network/docs/feelimit

## Build And Test

TRONBox uses the official TRON Solidity 0.8.26 compiler and targets TVM Cancun.
The deployment script refuses a network where Cancun is not activated.

```powershell
cd contracts\bridge
npm ci
npm audit --audit-level=high
npm run compile:tron
npm test
```

## TRON Mainnet Deployment

Copy `deploy/bridge/tron-vault-deploy.example.json` into a protected operator
directory. It deliberately fails validation until every Base58Check role and a
published HTTPS audit report are supplied. The production schema is pinned to:

- chain ID `tron-mainnet`
- genesis block `00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc`
- TIP-712 chain ID `0x2b6653dc`
- official Tether TRC20 USDT `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t`
- six token decimals and at least 64 observer confirmations
- at least five release committee members with a threshold of four

The deploy command fetches block 0 and chain parameters, then verifies the
official token's live runtime code, `symbol()`, and `decimals()` before spending
TRX on deployment. The resulting code hash and metadata are committed to the
immutable deployment record.

Use a newly generated one-time deployer. Never put a private key in JSON,
source control, or command arguments.

```powershell
$env:MSC_BRIDGE_TRON_RPC_URL = "https://api.trongrid.io"
$env:MSC_BRIDGE_TRON_DEPLOYER_KEY = "...32-byte hex key..."

cd contracts\bridge
npm run deploy:tron -- `
  --config C:\msc-bridge\tron-mainnet-vault.json `
  --output C:\msc-bridge\tron-mainnet-deployment.json

Remove-Item Env:MSC_BRIDGE_TRON_DEPLOYER_KEY
```

The output is created once and contains no RPC URL or private key. It embeds a
v2 reconciler template already pinned to the deployed vault/token runtime
hashes; only protected operator endpoints and the MSC wrapped-token ID remain
as placeholders. The vault remains paused. Import the deployment record in `ui/bridge-admin.html`; the
portal verifies its TRON addresses, genesis/TIP-712 binding, runtime and
deployment hashes, committee threshold, separated roles, governance actions,
and paused state before filling the chain, asset, and route forms. Import does
not submit a form, execute governance, or unpause the vault.

## Activation Ceremony

The record contains two ordered `/wallet/triggersmartcontract` requests:

1. Configure the reviewed TRC20 route while paused.
2. Unpause only after registry, observer quorum, checkpoint freshness,
   reconciler, alerts, low testnet caps, and incident procedures are ready.

Before either action, independently compare the live contract, compiler,
runtime hash, official token address and decimals, governance roles, committee,
TIP-712 chain ID, fee limits, and observer config. Production config additionally
requires an HTTPS audit reference and at least a 24-hour default-admin delay.
Run the embedded reconciler config against three independently hosted APIs and
retain a healthy JSON report before unpausing. See
`docs/BRIDGE_RECONCILIATION.md`. The reconciler also reads
`tokenRoutes(officialUSDT)` and rejects a disabled route or any min, max, daily
lock, or daily unlock limit that differs from the audited deployment record.

Execute action 1 while the vault is paused, then assemble and verify the
production evidence bundle described in `docs/BRIDGE_RELEASE_GATE.md`. Only a
green immutable gate report followed by a separate human review permits action
2 (`unpause()`). The release gate never broadcasts either action.

The MSC gateway accepts `tron_vault_v1` as an implemented adapter, but an
`active` route still fails closed without valid Base58Check custody addresses,
a non-zero runtime hash, a chain-format deployment transaction ID, an HTTPS
audit reference, finalized fresh threshold checkpoints, and all other route
readiness conditions.

## Offline Withdrawal Release

Export the gateway's authorized unlock JSON only after the MSC burn is final.
Prepare a bounded TIP-712 release artifact; expiry cannot exceed seven days.

```powershell
cd contracts\bridge
npm run release:tron -- prepare `
  --deployment C:\msc-bridge\tron-mainnet-deployment.json `
  --unlock C:\msc-bridge\gateway-unlock.json `
  --valid-until 1784300000 `
  --output C:\msc-bridge\tron-release-unsigned.json
```

Each committee operator reviews and signs the same immutable artifact offline:

```powershell
$env:MSC_TRON_RELEASE_KEY = "...operator key..."
npm run release:tron -- sign `
  --input C:\msc-bridge\tron-release-unsigned.json `
  --key-env MSC_TRON_RELEASE_KEY `
  --output C:\msc-bridge\tron-release-signer-a.json
Remove-Item Env:MSC_TRON_RELEASE_KEY
```

Merge enough independent signatures and verify the final request:

```powershell
npm run release:tron -- merge `
  --input C:\msc-bridge\tron-release-signer-a.json `
  --input C:\msc-bridge\tron-release-signer-b.json `
  --output C:\msc-bridge\tron-release-final.json

npm run release:tron -- verify --input C:\msc-bridge\tron-release-final.json
```

The release tool cross-checks TronWeb and Ethers typed-data digests, recovers
and sorts every signer, rejects mixed or tampered artifacts, and creates the
exact `/wallet/triggersmartcontract` body only after threshold. It never signs
the executor transaction or broadcasts it. Before a separately controlled
executor submits that body, re-check solidified contract code, pause state,
route state, escrow balance, committee epoch/members, expiry, and fee limit.
