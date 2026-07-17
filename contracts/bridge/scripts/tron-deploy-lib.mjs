import { Interface, formatUnits, id } from "ethers";
import {
  TRON_DEPLOYMENT_VERSION,
  TRON_MAX_FEE_LIMIT_SUN,
  normalizeTronAddress,
  tronTip712ChainID,
} from "./tron-release-lib.mjs";

export const TRON_DEPLOY_CONFIG_VERSION = "msc-bridge-tron-deploy-config-v2";
export const MSC_PROTOCOL_CHAIN_ID = "91938";
export const TRON_MAINNET_CHAIN_ID = "tron-mainnet";
export const TRON_MAINNET_LABEL = "tron-mainnet";
export const TRON_MAINNET_GENESIS_BLOCK_ID =
  "00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc";
export const TRON_MAINNET_TIP712_CHAIN_ID = "0x2b6653dc";
export const TRON_MAINNET_USDT = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t";
export const TRON_MAINNET_MIN_CONFIRMATIONS = 64;

const TOP_LEVEL_KEYS = new Set([
  "version",
  "testnet",
  "network",
  "rpc_url_env",
  "deployer_key_env",
  "deployment_confirmations",
  "deployment_timeout_seconds",
  "fee_limit_sun",
  "release_fee_limit_sun",
  "origin_energy_limit",
  "user_fee_percentage",
  "default_admin_delay_seconds",
  "governance",
  "guardian",
  "release_executor",
  "msc_source_chain_id",
  "committee",
  "committee_threshold",
  "route",
]);
const NETWORK_KEYS = new Set([
  "label",
  "chain_id",
  "tip712_chain_id",
  "explorer_url",
]);
const ROUTE_KEYS = new Set([
  "route_id",
  "chain_name",
  "native_symbol",
  "asset_denom",
  "symbol",
  "token_address",
  "local_denom",
  "decimals",
  "min_confirmations",
  "min_amount_raw",
  "max_amount_raw",
  "daily_lock_limit_raw",
  "daily_unlock_limit_raw",
  "audit_reference",
]);

function assertObject(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
}

function assertKnownKeys(value, allowed, label) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) throw new Error(`${label} contains unknown field ${key}`);
  }
}

function requiredString(value, label, pattern) {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`${label} is required`);
  const normalized = value.trim();
  if (pattern && !pattern.test(normalized)) throw new Error(`${label} is invalid`);
  return normalized;
}

function positiveInteger(value, label, maximum = Number.MAX_SAFE_INTEGER) {
  if (!Number.isSafeInteger(value) || value <= 0 || value > maximum) {
    throw new Error(`${label} must be a positive integer`);
  }
  return value;
}

function boundedInteger(value, label, minimum, maximum) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${label} must be between ${minimum} and ${maximum}`);
  }
  return value;
}

function rawAmount(value, label) {
  const normalized = requiredString(value, label, /^(0|[1-9][0-9]*)$/);
  const parsed = BigInt(normalized);
  if (parsed <= 0n) throw new Error(`${label} must be positive`);
  return parsed;
}

function feeLimit(value, label) {
  const parsed = rawAmount(value, label);
  if (parsed > TRON_MAX_FEE_LIMIT_SUN) {
    throw new Error(`${label} exceeds the TRON network maximum`);
  }
  return parsed.toString();
}

function optionalHTTPS(value, label, required) {
  if (!value && !required) return "";
  const raw = requiredString(value, label);
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error(`${label} must be a valid URL`);
  }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password) {
    throw new Error(`${label} must be an HTTPS URL without embedded credentials`);
  }
  return parsed.toString().replace(/\/$/, "");
}

export function validateTronDeploymentConfig(input) {
  assertObject(input, "config");
  assertKnownKeys(input, TOP_LEVEL_KEYS, "config");
  if (input.version !== TRON_DEPLOY_CONFIG_VERSION) {
    throw new Error("unsupported TRON deploy config version");
  }
  if (typeof input.testnet !== "boolean") throw new Error("testnet must be boolean");
  assertObject(input.network, "network");
  assertKnownKeys(input.network, NETWORK_KEYS, "network");
  assertObject(input.route, "route");
  assertKnownKeys(input.route, ROUTE_KEYS, "route");

  const explorerURL = optionalHTTPS(
    input.network.explorer_url,
    "network.explorer_url",
    !input.testnet,
  );
  const auditReference = optionalHTTPS(
    input.route.audit_reference,
    "route.audit_reference",
    !input.testnet,
  );
  const defaultAdminDelay = Number(input.default_admin_delay_seconds);
  if (!Number.isSafeInteger(defaultAdminDelay) || defaultAdminDelay < 0) {
    throw new Error("default_admin_delay_seconds must be a non-negative integer");
  }
  if (!input.testnet && defaultAdminDelay < 86_400) {
    throw new Error("production default admin delay must be at least 86400 seconds");
  }

  const governance = normalizeTronAddress(input.governance, "governance").base58;
  const guardian = normalizeTronAddress(input.guardian, "guardian").base58;
  const releaseExecutor = normalizeTronAddress(
    input.release_executor,
    "release executor",
  ).base58;
  if (new Set([governance, guardian, releaseExecutor]).size !== 3) {
    throw new Error("governance, guardian, and release executor must be separate");
  }
  if (!Array.isArray(input.committee) || input.committee.length < 3 || input.committee.length > 32) {
    throw new Error("committee must contain between 3 and 32 members");
  }
  const committee = input.committee.map(
    (member, index) => normalizeTronAddress(member, `committee[${index}]`).base58,
  );
  const uniqueCommittee = new Set(committee);
  if (uniqueCommittee.size !== committee.length) throw new Error("committee members must be unique");
  if ([governance, guardian, releaseExecutor].some((address) => uniqueCommittee.has(address))) {
    throw new Error("committee members must be separate from governance, guardian, and release executor");
  }
  const threshold = positiveInteger(input.committee_threshold, "committee_threshold", 32);
  const minimumThreshold = Math.ceil((2 * committee.length) / 3);
  if (threshold < minimumThreshold || threshold > committee.length) {
    throw new Error(`committee_threshold must be between ${minimumThreshold} and ${committee.length}`);
  }
  if (!input.testnet && (committee.length < 5 || threshold < 4)) {
    throw new Error("production committee must contain at least 5 members and require at least 4 signatures");
  }

  const decimals = Number(input.route.decimals);
  if (!Number.isSafeInteger(decimals) || decimals < 0 || decimals > 30) {
    throw new Error("route.decimals must be between 0 and 30");
  }
  const minAmount = rawAmount(input.route.min_amount_raw, "route.min_amount_raw");
  const maxAmount = rawAmount(input.route.max_amount_raw, "route.max_amount_raw");
  const dailyLockLimit = rawAmount(
    input.route.daily_lock_limit_raw,
    "route.daily_lock_limit_raw",
  );
  const dailyUnlockLimit = rawAmount(
    input.route.daily_unlock_limit_raw,
    "route.daily_unlock_limit_raw",
  );
  if (minAmount > maxAmount) throw new Error("route min amount exceeds max amount");
  if (maxAmount > dailyLockLimit || maxAmount > dailyUnlockLimit) {
    throw new Error("route max amount exceeds a daily limit");
  }

  const mscSourceChainID = requiredString(
    input.msc_source_chain_id,
    "msc_source_chain_id",
    /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/,
  );
  if (mscSourceChainID !== MSC_PROTOCOL_CHAIN_ID) {
    throw new Error(`msc_source_chain_id must equal MSC protocol chain ID ${MSC_PROTOCOL_CHAIN_ID}`);
  }

  const networkLabel = requiredString(input.network.label, "network.label", /^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$/);
  const networkChainID = requiredString(input.network.chain_id, "network.chain_id", /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/);
  const tip712ChainID = tronTip712ChainID(input.network.tip712_chain_id);
  const routeID = requiredString(input.route.route_id, "route.route_id", /^[a-z0-9][a-z0-9-]{2,63}$/);
  const chainName = requiredString(input.route.chain_name, "route.chain_name");
  const nativeSymbol = requiredString(input.route.native_symbol, "route.native_symbol", /^[A-Za-z0-9]{2,16}$/);
  const assetDenom = requiredString(input.route.asset_denom, "route.asset_denom", /^[A-Za-z0-9][A-Za-z0-9-]{1,31}$/);
  const symbol = requiredString(input.route.symbol, "route.symbol", /^[A-Za-z0-9]{2,16}$/);
  const tokenAddress = normalizeTronAddress(input.route.token_address, "route.token_address").base58;
  const localDenom = requiredString(input.route.local_denom, "route.local_denom", /^[A-Za-z0-9][A-Za-z0-9._-]{1,31}$/);
  const deploymentConfirmations = positiveInteger(input.deployment_confirmations, "deployment_confirmations", 100_000);
  const minConfirmations = positiveInteger(input.route.min_confirmations, "route.min_confirmations", 100_000);

  if (!input.testnet) {
    if (networkLabel !== TRON_MAINNET_LABEL || networkChainID !== TRON_MAINNET_CHAIN_ID ||
        tip712ChainID !== TRON_MAINNET_TIP712_CHAIN_ID ||
        explorerURL !== "https://tronscan.org") {
      throw new Error("production deployment must be pinned to the canonical TRON mainnet identity");
    }
    if (routeID !== "usdt-tron-mainnet" || chainName !== "TRON Mainnet" || nativeSymbol !== "TRX" ||
        assetDenom !== "USDT-TRON" || symbol !== "USDT" || tokenAddress !== TRON_MAINNET_USDT ||
        localDenom !== "mscUSDT" || decimals !== 6) {
      throw new Error("production route must use the official TRON mainnet Tether USDT identity");
    }
    if (deploymentConfirmations < 20 || minConfirmations < TRON_MAINNET_MIN_CONFIRMATIONS) {
      throw new Error(`production deployment requires at least 20 deployment confirmations and ${TRON_MAINNET_MIN_CONFIRMATIONS} observer confirmations`);
    }
  }

  return {
    version: TRON_DEPLOY_CONFIG_VERSION,
    testnet: input.testnet,
    network: {
      label: networkLabel,
      chain_id: networkChainID,
      tip712_chain_id: tip712ChainID,
      explorer_url: explorerURL,
    },
    rpc_url_env: requiredString(input.rpc_url_env, "rpc_url_env", /^[A-Z][A-Z0-9_]{2,127}$/),
    deployer_key_env: requiredString(
      input.deployer_key_env,
      "deployer_key_env",
      /^[A-Z][A-Z0-9_]{2,127}$/,
    ),
    deployment_confirmations: deploymentConfirmations,
    deployment_timeout_seconds: positiveInteger(
      input.deployment_timeout_seconds,
      "deployment_timeout_seconds",
      3_600,
    ),
    fee_limit_sun: feeLimit(input.fee_limit_sun, "fee_limit_sun"),
    release_fee_limit_sun: feeLimit(
      input.release_fee_limit_sun,
      "release_fee_limit_sun",
    ),
    origin_energy_limit: positiveInteger(
      input.origin_energy_limit,
      "origin_energy_limit",
      100_000_000,
    ),
    user_fee_percentage: boundedInteger(
      input.user_fee_percentage,
      "user_fee_percentage",
      0,
      100,
    ),
    default_admin_delay_seconds: defaultAdminDelay,
    governance,
    guardian,
    release_executor: releaseExecutor,
    msc_source_chain_id: mscSourceChainID,
    committee,
    committee_threshold: threshold,
    route: {
      route_id: routeID,
      chain_name: chainName,
      native_symbol: nativeSymbol,
      asset_denom: assetDenom,
      symbol,
      token_address: tokenAddress,
      local_denom: localDenom,
      decimals,
      min_confirmations: minConfirmations,
      min_amount_raw: minAmount.toString(),
      max_amount_raw: maxAmount.toString(),
      daily_lock_limit_raw: dailyLockLimit.toString(),
      daily_unlock_limit_raw: dailyUnlockLimit.toString(),
      audit_reference: auditReference,
    },
  };
}

export function validateTronRPCURL(rawValue, allowLocalHTTP) {
  const raw = requiredString(rawValue, "RPC URL");
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error("RPC URL is invalid");
  }
  if (parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error("RPC URL cannot contain credentials, query parameters, or fragments");
  }
  const local = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" ||
    parsed.hostname === "::1";
  if (parsed.protocol !== "https:" && !(allowLocalHTTP && local && parsed.protocol === "http:")) {
    throw new Error("RPC URL must use HTTPS; testnet may opt into localhost HTTP only");
  }
  return parsed.toString().replace(/\/$/, "");
}

export function validateTronNetworkEvidence(config, genesisBlock, chainParameters) {
  const blockID = String(genesisBlock?.blockID ?? "").toLowerCase();
  const blockNumber = genesisBlock?.block_header?.raw_data?.number ?? 0;
  if (!/^[0-9a-f]{64}$/.test(blockID) || Number(blockNumber) !== 0) {
    throw new Error("RPC did not return the canonical TRON genesis block");
  }
  if (!config.testnet && blockID !== TRON_MAINNET_GENESIS_BLOCK_ID) {
    throw new Error("RPC genesis block is not canonical TRON mainnet");
  }
  const tip712ChainId = tronTip712ChainID(`0x${blockID.slice(-8)}`);
  if (tip712ChainId !== config.network.tip712_chain_id) {
    throw new Error(`RPC TIP-712 chain ID ${tip712ChainId} does not match expected chain ID`);
  }
  if (!Array.isArray(chainParameters)) throw new Error("RPC chain parameters are missing");
  const values = new Map(chainParameters.map((entry) => [entry?.key, Number(entry?.value ?? 0)]));
  if (values.get("getAllowTvmCancun") !== 1) {
    throw new Error("RPC network has not activated the TVM Cancun opcode set");
  }
  const networkMaxFee = BigInt(values.get("getMaxFeeLimit") ?? 0);
  if (networkMaxFee <= 0n || BigInt(config.fee_limit_sun) > networkMaxFee ||
      BigInt(config.release_fee_limit_sun) > networkMaxFee) {
    throw new Error("configured fee limit exceeds the RPC network maximum");
  }
  return { blockID, tip712ChainId, networkMaxFeeSun: networkMaxFee.toString() };
}

export function validateTronTokenEvidence(config, evidence) {
  assertObject(evidence, "token evidence");
  const address = normalizeTronAddress(evidence.address, "token evidence address").base58;
  const runtimeCode = requiredString(evidence.runtime_code, "token runtime code")
    .replace(/^0x/i, "").toLowerCase();
  const symbol = requiredString(evidence.symbol, "token symbol");
  const decimals = Number(evidence.decimals);
  if (!/^[0-9a-f]+$/.test(runtimeCode) || runtimeCode.length % 2 !== 0) {
    throw new Error("token runtime code must be non-empty even-length hex");
  }
  if (address !== config.route.token_address || symbol !== config.route.symbol ||
      !Number.isSafeInteger(decimals) || decimals !== config.route.decimals) {
    throw new Error("on-chain TRC20 metadata does not match the configured route");
  }
  if (!config.testnet && address !== TRON_MAINNET_USDT) {
    throw new Error("production token evidence is not official TRON mainnet Tether USDT");
  }
  return { address, runtimeCode, symbol, decimals };
}

function tronGovernanceAction(vaultABI, vaultAddress, config, order, label, name, args) {
  const iface = new Interface(vaultABI);
  const data = iface.encodeFunctionData(name, args);
  return {
    order,
    label,
    endpoint: "/wallet/triggersmartcontract",
    method: "POST",
    body: {
      owner_address: config.governance,
      contract_address: normalizeTronAddress(vaultAddress).base58,
      function_selector: iface.getFunction(name).format("sighash"),
      parameter: data.slice(10),
      fee_limit: Number(config.fee_limit_sun),
      call_value: 0,
      visible: true,
    },
    calldata: data.slice(2),
  };
}

export function tronGovernanceActions(vaultABI, vaultAddress, config) {
  return [
    tronGovernanceAction(
      vaultABI,
      vaultAddress,
      config,
      1,
      "Configure TRC20 route while vault is paused",
      "setTokenRoute",
      [
        normalizeTronAddress(config.route.token_address).solidity,
        {
          enabled: true,
          minAmount: config.route.min_amount_raw,
          maxAmount: config.route.max_amount_raw,
          dailyLockLimit: config.route.daily_lock_limit_raw,
          dailyUnlockLimit: config.route.daily_unlock_limit_raw,
        },
      ],
    ),
    tronGovernanceAction(
      vaultABI,
      vaultAddress,
      config,
      2,
      "Unpause only after observer and MSC route registration pass",
      "unpause",
      [],
    ),
  ];
}

export function tronDeploymentRecord(config, deployment, vaultABI) {
  const route = config.route;
  const vault = normalizeTronAddress(deployment.address, "deployed vault").base58;
  const tokenRuntimeCodeHash = String(deployment.tokenRuntimeCodeHash ?? "").toLowerCase();
  if (!/^0x[0-9a-f]{64}$/.test(tokenRuntimeCodeHash) || /^0x0{64}$/.test(tokenRuntimeCodeHash) ||
      deployment.tokenSymbol !== route.symbol || Number(deployment.tokenDecimals) !== route.decimals) {
    throw new Error("deployment record requires verified non-zero TRC20 runtime code and matching metadata");
  }
  return {
    version: TRON_DEPLOYMENT_VERSION,
    created_at: new Date().toISOString(),
    testnet: config.testnet,
    network: {
      label: config.network.label,
      chain_id: config.network.chain_id,
      tip712_chain_id: config.network.tip712_chain_id,
      genesis_block_id: deployment.genesisBlockID,
      chain_name: route.chain_name,
      native_symbol: route.native_symbol,
      explorer_url: config.network.explorer_url,
      min_confirmations: route.min_confirmations,
      network_max_fee_limit_sun: deployment.networkMaxFeeSun,
      release_fee_limit_sun: config.release_fee_limit_sun,
    },
    contract: {
      address: vault,
      release_executor: config.release_executor,
      deployment_tx_hash: String(deployment.transactionHash).replace(/^0x/i, "").toLowerCase(),
      deployment_block: deployment.blockNumber,
      runtime_code_hash: deployment.runtimeCodeHash,
      compiler: deployment.compiler,
      tvm_target: "cancun",
      tip712: true,
      paused: true,
      default_admin_delay_seconds: config.default_admin_delay_seconds,
      governance: config.governance,
      guardian: config.guardian,
      msc_source_chain_id: config.msc_source_chain_id,
      msc_source_chain_hash: id(config.msc_source_chain_id),
      committee_epoch: 1,
      committee_threshold: config.committee_threshold,
      committee_members: config.committee,
    },
    route: {
      route_id: route.route_id,
      execution_adapter: "tron_vault_v1",
      asset_denom: route.asset_denom,
      symbol: route.symbol,
      token_address: route.token_address,
      local_denom: route.local_denom,
      decimals: route.decimals,
      min_amount_raw: route.min_amount_raw,
      max_amount_raw: route.max_amount_raw,
      daily_lock_limit_raw: route.daily_lock_limit_raw,
      daily_unlock_limit_raw: route.daily_unlock_limit_raw,
      min_amount: formatUnits(route.min_amount_raw, route.decimals),
      max_amount: formatUnits(route.max_amount_raw, route.decimals),
      daily_lock_limit: formatUnits(route.daily_lock_limit_raw, route.decimals),
      daily_unlock_limit: formatUnits(route.daily_unlock_limit_raw, route.decimals),
      audit_reference: route.audit_reference,
      token_runtime_code_hash: tokenRuntimeCodeHash,
      token_symbol_verified: deployment.tokenSymbol,
      token_decimals_verified: deployment.tokenDecimals,
    },
    governance_actions: tronGovernanceActions(vaultABI, vault, config),
    observer_config: {
      source_chain_id: config.network.chain_id,
      genesis_block_id: deployment.genesisBlockID,
      api_urls: [
        "REPLACE_WITH_INDEPENDENT_TRON_MAINNET_API_1",
        "REPLACE_WITH_INDEPENDENT_TRON_MAINNET_API_2",
        "REPLACE_WITH_INDEPENDENT_TRON_MAINNET_API_3",
      ],
      api_quorum: 2,
      confirmations: route.min_confirmations,
      bridge_contract: vault,
      asset_denom: route.asset_denom,
      origin_asset: route.token_address,
      asset_decimals: route.decimals,
      lock_event_signature: "Locked(address,address,bytes,uint256)",
      unlock_event_signature: "Unlocked(bytes32,address,address,uint256)",
      request_timeout: "12s",
      allow_insecure_http: false,
    },
    reconciler_config: {
      version: "msc-bridge-reconciler-config-v2",
      msc_accounting_url: "REPLACE_WITH_MSC_HTTPS_ORIGIN/bridge/accounting",
      request_timeout: "12s",
      max_accounting_age: "2m",
      routes: [{
        chain_type: "tron",
        route_id: route.route_id,
        source_chain_id: config.network.chain_id,
        expected_evm_chain_id: 0,
        expected_genesis_block_id: deployment.genesisBlockID,
        expected_vault_runtime_code_hash: deployment.runtimeCodeHash,
        expected_token_runtime_code_hash: tokenRuntimeCodeHash,
		expected_route_enabled: true,
		expected_min_amount_raw: route.min_amount_raw,
		expected_max_amount_raw: route.max_amount_raw,
		expected_daily_lock_limit_raw: route.daily_lock_limit_raw,
		expected_daily_unlock_limit_raw: route.daily_unlock_limit_raw,
        rpc_urls: [
          "REPLACE_WITH_INDEPENDENT_TRON_MAINNET_API_1",
          "REPLACE_WITH_INDEPENDENT_TRON_MAINNET_API_2",
          "REPLACE_WITH_INDEPENDENT_TRON_MAINNET_API_3",
        ],
        rpc_quorum: 2,
        confirmations: route.min_confirmations,
        vault_address: vault,
        token_address: route.token_address,
        local_token_id: "REPLACE_WITH_MSC_WRAPPED_TOKEN_ID",
        source_decimals: route.decimals,
        allow_insecure_rpc_http: false,
      }],
    },
  };
}
