import {
  Interface,
  formatUnits,
  getAddress,
  id,
  isAddress,
  ZeroAddress,
} from "ethers";

export const DEPLOY_CONFIG_VERSION = "msc-bridge-evm-deploy-config-v1";
export const DEPLOY_RECORD_VERSION = "msc-bridge-evm-deployment-v1";
export const MSC_PROTOCOL_CHAIN_ID = "91938";

const TOP_LEVEL_KEYS = new Set([
  "version",
  "testnet",
  "network",
  "rpc_url_env",
  "deployer_key_env",
  "deployment_confirmations",
  "default_admin_delay_seconds",
  "governance",
  "guardian",
  "msc_source_chain_id",
  "committee",
  "committee_threshold",
  "route",
]);
const NETWORK_KEYS = new Set(["label", "expected_chain_id", "explorer_url"]);
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

function rawAmount(value, label) {
  const normalized = requiredString(value, label, /^(0|[1-9][0-9]*)$/);
  const parsed = BigInt(normalized);
  if (parsed <= 0n) throw new Error(`${label} must be positive`);
  return parsed;
}

function strictAddress(value, label) {
  const raw = requiredString(value, label);
  if (!isAddress(raw)) throw new Error(`${label} is not an EVM address`);
  const normalized = getAddress(raw);
  if (normalized === ZeroAddress) throw new Error(`${label} cannot be zero`);
  return normalized;
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

export function validateDeploymentConfig(input) {
  assertObject(input, "config");
  assertKnownKeys(input, TOP_LEVEL_KEYS, "config");
  if (input.version !== DEPLOY_CONFIG_VERSION) throw new Error("unsupported deploy config version");
  if (typeof input.testnet !== "boolean") throw new Error("testnet must be boolean");

  assertObject(input.network, "network");
  assertKnownKeys(input.network, NETWORK_KEYS, "network");
  assertObject(input.route, "route");
  assertKnownKeys(input.route, ROUTE_KEYS, "route");

  const expectedChainID = positiveInteger(
    input.network.expected_chain_id,
    "network.expected_chain_id",
  );
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

  const governance = strictAddress(input.governance, "governance");
  const guardian = strictAddress(input.guardian, "guardian");
  if (governance === guardian) throw new Error("governance and guardian must be separate");

  if (!Array.isArray(input.committee) || input.committee.length < 3 || input.committee.length > 32) {
    throw new Error("committee must contain between 3 and 32 members");
  }
  const committee = input.committee.map((member, index) =>
    strictAddress(member, `committee[${index}]`),
  );
  const uniqueCommittee = new Set(committee.map((member) => member.toLowerCase()));
  if (uniqueCommittee.size !== committee.length) throw new Error("committee members must be unique");
  if (uniqueCommittee.has(governance.toLowerCase()) || uniqueCommittee.has(guardian.toLowerCase())) {
    throw new Error("committee members must be separate from governance and guardian");
  }
  const threshold = positiveInteger(input.committee_threshold, "committee_threshold", 32);
  const minimumThreshold = Math.ceil((2 * committee.length) / 3);
  if (threshold < minimumThreshold || threshold > committee.length) {
    throw new Error(`committee_threshold must be between ${minimumThreshold} and ${committee.length}`);
  }

  const confirmations = positiveInteger(
    input.deployment_confirmations,
    "deployment_confirmations",
    64,
  );
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

	return {
    version: DEPLOY_CONFIG_VERSION,
    testnet: input.testnet,
    network: {
      label: requiredString(input.network.label, "network.label", /^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$/),
      expected_chain_id: expectedChainID,
      explorer_url: explorerURL,
    },
    rpc_url_env: requiredString(input.rpc_url_env, "rpc_url_env", /^[A-Z][A-Z0-9_]{2,127}$/),
    deployer_key_env: requiredString(
      input.deployer_key_env,
      "deployer_key_env",
      /^[A-Z][A-Z0-9_]{2,127}$/,
    ),
    deployment_confirmations: confirmations,
    default_admin_delay_seconds: defaultAdminDelay,
    governance,
    guardian,
		msc_source_chain_id: mscSourceChainID,
    committee,
    committee_threshold: threshold,
    route: {
      route_id: requiredString(input.route.route_id, "route.route_id", /^[a-z0-9][a-z0-9-]{2,63}$/),
      chain_name: requiredString(input.route.chain_name, "route.chain_name"),
      native_symbol: requiredString(input.route.native_symbol, "route.native_symbol", /^[A-Za-z0-9]{2,16}$/),
      asset_denom: requiredString(input.route.asset_denom, "route.asset_denom", /^[A-Za-z0-9][A-Za-z0-9-]{1,31}$/),
      symbol: requiredString(input.route.symbol, "route.symbol", /^[A-Za-z0-9]{2,16}$/),
      token_address: strictAddress(input.route.token_address, "route.token_address"),
      local_denom: requiredString(input.route.local_denom, "route.local_denom", /^[A-Za-z0-9][A-Za-z0-9._-]{1,31}$/),
      decimals,
      min_confirmations: positiveInteger(
        input.route.min_confirmations,
        "route.min_confirmations",
        100_000,
      ),
      min_amount_raw: minAmount.toString(),
      max_amount_raw: maxAmount.toString(),
      daily_lock_limit_raw: dailyLockLimit.toString(),
      daily_unlock_limit_raw: dailyUnlockLimit.toString(),
      audit_reference: auditReference,
    },
  };
}

export function validateRPCURL(rawValue, allowLocalHTTP) {
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
  const local = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1";
  if (parsed.protocol !== "https:" && !(allowLocalHTTP && local && parsed.protocol === "http:")) {
    throw new Error("RPC URL must use HTTPS; testnet may opt into localhost HTTP only");
  }
  return parsed.toString().replace(/\/$/, "");
}

export function governanceActions(vaultABI, vaultAddress, config) {
  const iface = new Interface(vaultABI);
  return [
    {
      order: 1,
      label: "Configure token route while vault is paused",
      to: getAddress(vaultAddress),
      value: "0",
      data: iface.encodeFunctionData("setTokenRoute", [
        config.route.token_address,
        {
          enabled: true,
          minAmount: config.route.min_amount_raw,
          maxAmount: config.route.max_amount_raw,
          dailyLockLimit: config.route.daily_lock_limit_raw,
          dailyUnlockLimit: config.route.daily_unlock_limit_raw,
        },
      ]),
    },
    {
      order: 2,
      label: "Unpause only after observer and MSC route registration pass",
      to: getAddress(vaultAddress),
      value: "0",
      data: iface.encodeFunctionData("unpause"),
    },
  ];
}

export function deploymentRecord(config, deployment, vaultABI) {
  const route = config.route;
  return {
    version: DEPLOY_RECORD_VERSION,
    created_at: new Date().toISOString(),
    testnet: config.testnet,
    network: {
      label: config.network.label,
      chain_id: String(config.network.expected_chain_id),
      chain_name: route.chain_name,
      native_symbol: route.native_symbol,
      explorer_url: config.network.explorer_url,
      min_confirmations: route.min_confirmations,
    },
    contract: {
      address: getAddress(deployment.address),
      deployment_tx_hash: deployment.transactionHash,
      deployment_block: deployment.blockNumber,
      runtime_code_hash: deployment.runtimeCodeHash,
      compiler: deployment.compiler,
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
    },
    governance_actions: governanceActions(vaultABI, deployment.address, config),
    observer_config: {
      source_chain_id: String(config.network.expected_chain_id),
      confirmations: route.min_confirmations,
      bridge_contract: getAddress(deployment.address).toLowerCase(),
      asset_denom: route.asset_denom,
      origin_asset: route.token_address.toLowerCase(),
      asset_decimals: route.decimals,
      lock_event_signature: "Locked(address,address,bytes,uint256)",
      unlock_event_signature: "Unlocked(bytes32,address,address,uint256)",
    },
  };
}
