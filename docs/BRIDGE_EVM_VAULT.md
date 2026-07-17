# MSC EVM Bridge Vault

`contracts/bridge/contracts/MSCBridgeVault.sol` is the source-chain custody
contract for ERC-20 routes such as BNB Chain, Ethereum, Polygon, Arbitrum, Base,
and Optimism. It implements the event ABI consumed by the MSC EVM observer.

This source has not received an external smart-contract audit. Do not custody
mainnet funds until the complete production gate is satisfied.

## Security Model

- The contract starts paused and cannot receive deposits until governance
  configures a token route and unpauses it.
- Governance uses OpenZeppelin's delayed, two-step single-default-admin rules.
  The production admin address must itself be a reviewed multisig or timelock.
- A separate guardian can pause but cannot unpause, change routes, rotate the
  committee, rescue assets, or transfer governance.
- Deposits accept only canonical 45-byte MSC recipients and exact-transfer
  ERC-20 assets. Fee-on-transfer and rebasing behavior is rejected.
- Each token has minimum, maximum, daily lock, and daily unlock limits.
- Unlocks require sorted, unique ECDSA signatures from the current threshold
  committee over an EIP-712 authorization bound to the source event, token,
  recipient, amount, committee epoch, destination chain, and vault contract.
- Withdrawal IDs derive from the MSC source chain, burn transaction hash, and
  log index and are permanently consumed after one successful unlock.
- Authorizations expire and cannot be valid for more than seven days.
- Governance can rescue only token balance above tracked escrow and only while
  paused. There is no function to withdraw user liabilities.

The EVM unlock committee is cryptographically distinct from the Ed25519 DTL
mint authority committee. Do not reuse people, devices, seed material, or key
backup channels between those roles.

The vault must be deployed with the immutable MSC protocol chain ID `91938`.
Its bytes32 constructor value is `keccak256("91938")`. MSC burn authorizations
use source log index `0`; the gateway mirrors `computeWithdrawalId` and requires
the observed `Unlocked.withdrawalId` to match before marking a transfer
complete.

## Build And Test

```powershell
cd contracts\bridge
npm ci --ignore-scripts
npm audit --audit-level=high
npm run compile
npm test
```

The compiler, OpenZeppelin, Ethers, Hardhat, and transitive security override
versions are exact-pinned in `package-lock.json`.

## Testnet Deployment

Copy `deploy/bridge/evm-vault-deploy.example.json` to a protected operator
directory and replace every placeholder. The example deliberately fails
validation until all addresses are set.

Set the two environment variables named by the config. The deployment key is a
temporary testnet key; never commit it or put it in command history.

```powershell
$env:MSC_BRIDGE_EVM_RPC_URL = "https://independent-rpc.example"
$env:MSC_BRIDGE_EVM_DEPLOYER_KEY = "0x..."

cd contracts\bridge
npm run deploy -- `
  --config ..\..\deploy\bridge\evm-vault-deploy.testnet.json `
  --output C:\msc-bridge\bnb-testnet-vault-deployment.json
```

The deploy command verifies the RPC chain ID, deployed runtime code, paused
state, source chain ID, committee epoch, threshold, and member ordering. It
writes an immutable deployment record without the RPC URL or private key.

## Governance Activation

The deployment record contains two encoded governance actions:

1. `setTokenRoute` while the vault remains paused.
2. `unpause` only after the observer, MSC chain/asset/contract registry,
   checkpoint committee, limits, monitoring, and incident drill are ready.

Import the record in `bridge-admin.html` to prefill the chain, asset, and
contract forms. Import never submits or activates a route.

Before executing the unpause action, independently compare:

- source chain ID and official token address from at least two authoritative
  sources
- deployed runtime code hash and verified source on the chain explorer
- governance, guardian, committee, threshold, and admin delay
- raw token limits and decimal-form limits
- observer contract address, token address, confirmations, and event topics
- audit report scope and exact deployed commit/compiler settings

Mainnet configuration requires an HTTPS explorer URL, an HTTPS audit reference,
at least a 24-hour default-admin transfer delay, three or more separate
committee members, and a threshold of at least two thirds.

## Offline Withdrawal Release

The gateway's Ed25519 observer authorization is not submitted to an EVM vault.
The vault has a separate secp256k1 committee and requires EIP-712 signatures.
After an MSC burn is committed and the gateway unlock package reaches
`authorized: true`, prepare a bounded release artifact:

```powershell
cd contracts\bridge
npm run release -- prepare `
  --deployment C:\msc-bridge\bnb-testnet-vault-deployment.json `
  --unlock C:\msc-bridge\gateway-unlock.json `
  --valid-until 1784300000 `
  --output C:\msc-bridge\release-unsigned.json
```

`valid-until` must be a future Unix timestamp no more than seven days away.
Preparation verifies the deployment chain, vault, token, recipient, decimal
amount, MSC burn hash, fixed burn log index `0`, and the exact on-chain
withdrawal-ID formula before creating an EIP-712 digest.

Each EVM vault committee operator reviews the complete artifact and signs on a
separate offline machine. Put the key in an operator-specific environment
variable; never pass it as an argument or reuse an observer, DTL, validator,
governance, guardian, or deployment key.

```powershell
$env:MSC_EVM_RELEASE_KEY = "0x..."
npm run release -- sign `
  --input C:\msc-bridge\release-unsigned.json `
  --key-env MSC_EVM_RELEASE_KEY `
  --output C:\msc-bridge\release-signer-a.json
Remove-Item Env:MSC_EVM_RELEASE_KEY
```

Merge independently signed artifacts and verify the final package:

```powershell
npm run release -- merge `
  --input C:\msc-bridge\release-signer-a.json `
  --input C:\msc-bridge\release-signer-b.json `
  --output C:\msc-bridge\release-final.json

npm run release -- verify --input C:\msc-bridge\release-final.json
```

Merge recovers every signer, enforces current deployment-record membership,
sorts signatures by EVM address as required by the vault, rejects mixed
withdrawals, and emits immutable transaction calldata only after threshold.
The tool never broadcasts a transaction. Compare the live vault bytecode,
committee epoch/members, pause state, route, balance, and authorization expiry
again before a separately controlled relayer submits the calldata. After any
committee rotation, stop releases until a newly verified deployment/state
record and ceremony have been approved.
