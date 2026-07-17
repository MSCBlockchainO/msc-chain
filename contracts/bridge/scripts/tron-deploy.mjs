import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { id, keccak256 } from "ethers";
import { TronWeb } from "tronweb";
import {
  normalizeTronAddress,
} from "./tron-release-lib.mjs";
import {
  tronDeploymentRecord,
  validateTronDeploymentConfig,
  validateTronNetworkEvidence,
  validateTronRPCURL,
  validateTronTokenEvidence,
} from "./tron-deploy-lib.mjs";

const MAX_FILE_BYTES = 4 * 1024 * 1024;
const TRC20_METADATA_ABI = [
  { constant: true, inputs: [], name: "symbol", outputs: [{ name: "", type: "string" }], type: "function" },
  { constant: true, inputs: [], name: "decimals", outputs: [{ name: "", type: "uint8" }], type: "function" },
];

function usage() {
  console.error("Usage: npm run deploy:tron -- --config <config.json> --output <deployment.json>");
}

function parseArgs(argv) {
  if (argv.length !== 4) throw new Error("invalid arguments");
  const result = {};
  for (let index = 0; index < argv.length; index += 2) {
    const flag = argv[index];
    const value = argv[index + 1];
    if ((flag !== "--config" && flag !== "--output") || !value) {
      throw new Error("invalid arguments");
    }
    const key = flag.slice(2);
    if (result[key]) throw new Error(`duplicate ${flag}`);
    result[key] = value;
  }
  if (!result.config || !result.output) throw new Error("--config and --output are required");
  return result;
}

function readStrictJSON(filePath, label, maximum = MAX_FILE_BYTES) {
  const absolute = path.resolve(filePath);
  const stat = fs.lstatSync(absolute);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size <= 0 || stat.size > maximum) {
    throw new Error(`${label} must be a regular JSON file between 1 byte and ${maximum} bytes`);
  }
  return JSON.parse(fs.readFileSync(absolute, "utf8"));
}

function writeImmutable(filePath, value) {
  const absolute = path.resolve(filePath);
  fs.mkdirSync(path.dirname(absolute), { recursive: true });
  fs.writeFileSync(absolute, `${JSON.stringify(value, null, 2)}\n`, {
    flag: "wx",
    mode: 0o600,
  });
  return absolute;
}

function validateArtifact(input) {
  if (input?.contractName !== "MSCBridgeTronVault" || !Array.isArray(input.abi) ||
      !/^0x[0-9a-fA-F]+$/.test(input.bytecode ?? "") ||
      !/^0x[0-9a-fA-F]+$/.test(input.deployedBytecode ?? "") ||
      !String(input.compiler?.version ?? "").startsWith("0.8.26")) {
    throw new Error("artifacts-tron/MSCBridgeTronVault.json is missing or was not built by TRON solc 0.8.26");
  }
  return input;
}

function blockHeight(block) {
  return Number(block?.block_header?.raw_data?.number ?? 0);
}

async function waitForSolidifiedDeployment(tronWeb, transactionID, confirmations, timeoutSeconds) {
  const deadline = Date.now() + timeoutSeconds * 1000;
  let lastError = "transaction not visible in solidified state";
  while (Date.now() < deadline) {
    try {
      const info = await tronWeb.trx.getTransactionInfo(transactionID);
      if (info && Object.keys(info).length > 0 && Number.isSafeInteger(Number(info.blockNumber))) {
        const result = String(info.receipt?.result ?? info.result ?? "").toUpperCase();
        if (result && result !== "SUCCESS") {
          throw new Error(`deployment execution failed with ${result}`);
        }
        const confirmedHead = await tronWeb.trx.getConfirmedCurrentBlock();
        const requiredHeight = Number(info.blockNumber) + confirmations - 1;
        if (blockHeight(confirmedHead) >= requiredHeight) return info;
        lastError = `solidified head ${blockHeight(confirmedHead)} is below required height ${requiredHeight}`;
      }
    } catch (error) {
      lastError = error?.message || String(error);
      if (/deployment execution failed/.test(lastError)) throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 3_000));
  }
  throw new Error(`deployment confirmation timeout: ${lastError}`);
}

function normalizeBytes32(value, label) {
  const normalized = String(value ?? "").replace(/^0x/i, "").toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(normalized)) throw new Error(`${label} is not bytes32`);
  return `0x${normalized}`;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const config = validateTronDeploymentConfig(
    readStrictJSON(args.config, "config", 1024 * 1024),
  );
  const artifact = validateArtifact(readStrictJSON(
    path.resolve(import.meta.dirname, "..", "artifacts-tron", "MSCBridgeTronVault.json"),
    "TRON contract artifact",
  ));
  const rpcURL = validateTronRPCURL(process.env[config.rpc_url_env], config.testnet);
  const privateKey = String(process.env[config.deployer_key_env] ?? "").replace(/^0x/i, "");
  if (!/^[0-9a-fA-F]{64}$/.test(privateKey) || /^0+$/.test(privateKey)) {
    throw new Error(`${config.deployer_key_env} must contain one non-zero 32-byte hex deployment key`);
  }
  delete process.env[config.deployer_key_env];

  const tronWeb = new TronWeb({ fullHost: rpcURL, privateKey });
  const deployer = normalizeTronAddress(
    TronWeb.address.fromPrivateKey(privateKey),
    "deployer",
  ).base58;
  const protectedRoles = new Set([
    config.governance,
    config.guardian,
    config.release_executor,
    ...config.committee,
  ]);
  if (protectedRoles.has(deployer)) {
    throw new Error("one-time deployer must be separate from governance, guardian, executor, and committee keys");
  }

  const [genesisBlock, chainParameters] = await Promise.all([
    tronWeb.trx.getBlock(0),
    tronWeb.trx.getChainParameters(),
  ]);
  const networkEvidence = validateTronNetworkEvidence(config, genesisBlock, chainParameters);

  const tokenContractRecord = await tronWeb.trx.getContract(config.route.token_address);
  const tokenRuntimeCode = String(tokenContractRecord?.bytecode ?? "").replace(/^0x/i, "");
  const tokenContract = await tronWeb.contract(TRC20_METADATA_ABI, config.route.token_address);
  const [tokenSymbol, tokenDecimals] = await Promise.all([
    tokenContract.symbol().call(),
    tokenContract.decimals().call(),
  ]);
  const tokenEvidence = validateTronTokenEvidence(config, {
    address: config.route.token_address,
    runtime_code: tokenRuntimeCode,
    symbol: String(tokenSymbol),
    decimals: Number(String(tokenDecimals)),
  });

  const transaction = await tronWeb.transactionBuilder.createSmartContract({
    abi: artifact.abi,
    bytecode: artifact.bytecode,
    feeLimit: Number(config.fee_limit_sun),
    callValue: 0,
    userFeePercentage: config.user_fee_percentage,
    originEnergyLimit: config.origin_energy_limit,
    name: "MSCBridgeTronVault",
    parameters: [
      config.default_admin_delay_seconds,
      config.governance,
      config.guardian,
      id(config.msc_source_chain_id),
      config.committee,
      config.committee_threshold,
    ],
  }, deployer);
  const signedTransaction = await tronWeb.trx.sign(transaction, privateKey);
  const broadcast = await tronWeb.trx.sendRawTransaction(signedTransaction);
  if (!broadcast?.result || broadcast.code) {
    throw new Error(`TRON deployment broadcast failed: ${broadcast?.code ?? "unknown error"}`);
  }
  const transactionID = String(transaction.txID ?? "").toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(transactionID)) {
    throw new Error("deployment transaction ID is unavailable");
  }
  const contractAddress = normalizeTronAddress(
    transaction.contract_address,
    "deployed contract address",
  ).base58;
  const receipt = await waitForSolidifiedDeployment(
    tronWeb,
    transactionID,
    config.deployment_confirmations,
    config.deployment_timeout_seconds,
  );

  const onChainContract = await tronWeb.trx.getContract(contractAddress);
  const runtimeCode = String(onChainContract?.bytecode ?? "").replace(/^0x/i, "");
  if (!/^[0-9a-fA-F]+$/.test(runtimeCode)) throw new Error("deployed vault has no runtime bytecode");
  const vault = await tronWeb.contract(artifact.abi, contractAddress);
  const [paused, sourceChainID, tip712ChainId, epoch, threshold, members] = await Promise.all([
    vault.paused().call(),
    vault.MSC_SOURCE_CHAIN_ID().call(),
    vault.tip712ChainId().call(),
    vault.committeeEpoch().call(),
    vault.committeeThreshold().call(),
    vault.committeeMembers().call(),
  ]);
  if (paused !== true || normalizeBytes32(sourceChainID, "on-chain MSC source chain ID") !== id("91938")) {
    throw new Error("deployed vault fail-closed state does not match config");
  }
  if (BigInt(String(tip712ChainId)) !== BigInt(config.network.tip712_chain_id)) {
    throw new Error("deployed vault TIP-712 chain ID does not match config");
  }
  if (String(epoch) !== "1" || String(threshold) !== String(config.committee_threshold)) {
    throw new Error("deployed committee epoch or threshold does not match config");
  }
  const normalizedMembers = Array.from(members ?? []).map(
    (member, index) => normalizeTronAddress(member, `deployed committee member ${index}`).base58,
  );
  if (normalizedMembers.length !== config.committee.length ||
      normalizedMembers.some((member, index) => member !== config.committee[index])) {
    throw new Error("deployed committee members do not match config");
  }

  const record = tronDeploymentRecord(
    config,
    {
      address: contractAddress,
      transactionHash: transactionID,
      blockNumber: Number(receipt.blockNumber),
      runtimeCodeHash: keccak256(`0x${runtimeCode}`),
      compiler: artifact.compiler.version,
      genesisBlockID: networkEvidence.blockID,
      networkMaxFeeSun: networkEvidence.networkMaxFeeSun,
      tokenRuntimeCodeHash: keccak256(`0x${tokenEvidence.runtimeCode}`),
      tokenSymbol: tokenEvidence.symbol,
      tokenDecimals: tokenEvidence.decimals,
    },
    artifact.abi,
  );
  const output = writeImmutable(args.output, record);
  console.log(`MSC TRON bridge vault deployed at ${contractAddress}`);
  console.log(`Immutable deployment record written to ${output}`);
  console.log("Vault remains paused; execute recorded governance actions only after independent review.");
}

main().catch((error) => {
  usage();
  console.error(error?.message || String(error));
  process.exitCode = 1;
});
