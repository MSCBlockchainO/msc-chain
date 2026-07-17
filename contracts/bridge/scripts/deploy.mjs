import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { ContractFactory, JsonRpcProvider, Wallet, id, keccak256 } from "ethers";
import solc from "solc";
import { artifact, compileContracts } from "./compile-lib.mjs";
import {
  deploymentRecord,
  validateDeploymentConfig,
  validateRPCURL,
} from "./deploy-lib.mjs";

function usage() {
  console.error("Usage: npm run deploy -- --config <config.json> --output <deployment.json>");
}

function parseArgs(argv) {
  if (argv.length !== 4) throw new Error("invalid arguments");
  const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    const flag = argv[index];
    const value = argv[index + 1];
    if ((flag !== "--config" && flag !== "--output") || !value) throw new Error("invalid arguments");
    const key = flag.slice(2);
    if (result[key]) throw new Error(`duplicate ${flag}`);
    result[key] = value;
  }
  if (!result.config || !result.output) throw new Error("--config and --output are required");
  return result;
}

function readStrictJSON(filePath) {
  const stat = fs.lstatSync(filePath);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size <= 0 || stat.size > 1024 * 1024) {
    throw new Error("config must be a regular JSON file between 1 byte and 1 MB");
  }
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function writeImmutable(filePath, value) {
  const absolute = path.resolve(filePath);
  fs.mkdirSync(path.dirname(absolute), { recursive: true });
  fs.writeFileSync(absolute, `${JSON.stringify(value, null, 2)}\n`, {
    flag: "wx",
    mode: 0o600,
  });
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const config = validateDeploymentConfig(readStrictJSON(path.resolve(args.config)));
  const rpcURL = validateRPCURL(process.env[config.rpc_url_env], config.testnet);
  const privateKey = process.env[config.deployer_key_env];
  if (!/^0x[0-9a-fA-F]{64}$/.test(privateKey ?? "")) {
    throw new Error(`${config.deployer_key_env} must contain one 32-byte hex deployment key`);
  }

  const provider = new JsonRpcProvider(rpcURL, undefined, { staticNetwork: false });
  const network = await provider.getNetwork();
  if (network.chainId !== BigInt(config.network.expected_chain_id)) {
    throw new Error(`RPC chain ID ${network.chainId} does not match expected chain ID`);
  }

  const signer = new Wallet(privateKey, provider);
  const contracts = compileContracts();
  const vaultArtifact = artifact(
    contracts,
    "contracts/MSCBridgeVault.sol",
    "MSCBridgeVault",
  );
  const factory = new ContractFactory(vaultArtifact.abi, vaultArtifact.bytecode, signer);
  const vault = await factory.deploy(
    config.default_admin_delay_seconds,
    config.governance,
    config.guardian,
    id(config.msc_source_chain_id),
    config.committee,
    config.committee_threshold,
  );
  const deploymentTransaction = vault.deploymentTransaction();
  if (!deploymentTransaction) throw new Error("deployment transaction is unavailable");
  const receipt = await deploymentTransaction.wait(config.deployment_confirmations);
  if (!receipt || receipt.status !== 1) throw new Error("vault deployment failed");

  const address = await vault.getAddress();
  const runtimeCode = await provider.getCode(address, receipt.blockNumber);
  if (!runtimeCode || runtimeCode === "0x") throw new Error("deployed vault has no runtime bytecode");
  const [paused, sourceChainID, epoch, threshold, members] = await Promise.all([
    vault.paused(),
    vault.MSC_SOURCE_CHAIN_ID(),
    vault.committeeEpoch(),
    vault.committeeThreshold(),
    vault.committeeMembers(),
  ]);
  if (!paused || sourceChainID.toLowerCase() !== id(config.msc_source_chain_id).toLowerCase()) {
    throw new Error("deployed vault fail-closed state does not match config");
  }
  if (epoch !== 1n || threshold !== BigInt(config.committee_threshold)) {
    throw new Error("deployed committee epoch or threshold does not match config");
  }
  if (
    members.length !== config.committee.length
    || members.some((member, index) => member.toLowerCase() !== config.committee[index].toLowerCase())
  ) throw new Error("deployed committee members do not match config");

  const record = deploymentRecord(
    config,
    {
      address,
      transactionHash: receipt.hash,
      blockNumber: receipt.blockNumber,
      runtimeCodeHash: keccak256(runtimeCode),
      compiler: solc.version(),
    },
    vaultArtifact.abi,
  );
  writeImmutable(args.output, record);
  console.log(`MSC bridge vault deployed at ${address}`);
  console.log(`Immutable deployment record written to ${path.resolve(args.output)}`);
  console.log("Vault remains paused; execute the recorded governance actions only after review.");
}

main().catch((error) => {
  usage();
  console.error(error?.message || String(error));
  process.exitCode = 1;
});
