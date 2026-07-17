import { createHash } from "node:crypto";
import {
  Interface,
  TypedDataEncoder,
  getAddress,
  id,
  parseUnits,
  verifyTypedData,
} from "ethers";
import { TronWeb, utils as TronWebUtils } from "tronweb";
import { computeVaultWithdrawalID, EVM_UNLOCK_TYPES } from "./release-lib.mjs";

export const TRON_DEPLOYMENT_VERSION = "msc-bridge-tron-deployment-v2";
export const TRON_RELEASE_VERSION = "msc-bridge-tron-release-v1";
export const TRON_RELEASE_MAX_LIFETIME = 7 * 24 * 60 * 60;
export const TRON_MAX_FEE_LIMIT_SUN = 15_000_000_000n;
export const TRON_UNLOCK_SELECTOR =
  "unlock((bytes32,bytes32,uint64,address,address,uint256,uint64,uint64),bytes[])";

const UNLOCK_INTERFACE = new Interface([
  "function unlock((bytes32 sourceChainId,bytes32 sourceTxHash,uint64 sourceLogIndex,address token,address recipient,uint256 amount,uint64 validUntil,uint64 committeeEpoch) authorization,bytes[] signatures) returns (bytes32 withdrawalId)",
]);
const UINT64_MAX = (1n << 64n) - 1n;
const UINT256_MAX = (1n << 256n) - 1n;

function requiredString(value, label) {
  const normalized = String(value ?? "").trim();
  if (!normalized) throw new Error(`${label} is required`);
  return normalized;
}

function fixedHex(value, bytes, label, allowZero = false) {
  const normalized = requiredString(value, label).toLowerCase();
  const pattern = new RegExp(`^0x[0-9a-f]{${bytes * 2}}$`);
  if (!pattern.test(normalized) || (!allowZero && /^0x0+$/.test(normalized))) {
    throw new Error(`${label} must be a non-zero ${bytes}-byte 0x hex value`);
  }
  return normalized;
}

function transactionHash(value, label) {
  return fixedHex(`0x${requiredString(value, label).replace(/^0x/i, "")}`, 32, label);
}

function uintString(value, label, maximum = UINT64_MAX) {
  const normalized = requiredString(value, label);
  if (!/^(0|[1-9][0-9]*)$/.test(normalized)) {
    throw new Error(`${label} must be an unsigned decimal integer`);
  }
  const parsed = BigInt(normalized);
  if (parsed > maximum) throw new Error(`${label} is out of range`);
  return parsed.toString();
}

function canonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map(
      (key) => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`,
    ).join(",")}}`;
  }
  return JSON.stringify(value);
}

function sha256JSON(value) {
  return createHash("sha256").update(canonicalJSON(value)).digest("hex");
}

export function normalizeTronAddress(value, label = "TRON address") {
  const raw = requiredString(value, label);
  let hex;
  try {
    if (/^0x[0-9a-fA-F]{40}$/.test(raw)) {
      hex = `41${raw.slice(2)}`;
    } else if (/^(?:0x)?41[0-9a-fA-F]{40}$/.test(raw)) {
      hex = raw.replace(/^0x/i, "");
    } else {
      if (!TronWeb.isAddress(raw)) throw new Error("invalid");
      hex = TronWeb.address.toHex(raw);
    }
    hex = String(hex).replace(/^0x/i, "").toLowerCase();
    if (!/^41[0-9a-f]{40}$/.test(hex) || /^410{40}$/.test(hex)) throw new Error("invalid");
    const base58 = TronWeb.address.fromHex(hex);
    if (!TronWeb.isAddress(base58) || TronWeb.address.toHex(base58).toLowerCase() !== hex) {
      throw new Error("invalid");
    }
    return Object.freeze({
      base58,
      hex: `0x${hex}`,
      solidity: getAddress(`0x${hex.slice(2)}`),
    });
  } catch {
    throw new Error(`${label} must be a non-zero TRON Base58Check, 41-prefixed hex, or Solidity address`);
  }
}

export function tronTip712ChainID(value) {
  const raw = requiredString(value, "TIP-712 chain ID");
  if (!/^(?:0x[0-9a-fA-F]+|[1-9][0-9]*)$/.test(raw)) {
    throw new Error("TIP-712 chain ID must be positive decimal or 0x hex");
  }
  const parsed = BigInt(raw);
  if (parsed <= 0n || parsed > 0xffffffffn) {
    throw new Error("TIP-712 chain ID must fit the low 32 bits of TVM block.chainid");
  }
  return `0x${parsed.toString(16).padStart(8, "0")}`;
}

function addressOrder(left, right) {
  const a = BigInt(normalizeTronAddress(left).solidity.toLowerCase());
  const b = BigInt(normalizeTronAddress(right).solidity.toLowerCase());
  return a < b ? -1 : a > b ? 1 : 0;
}

function unwrapInstruction(input) {
  return input?.unlock_instruction ?? input?.unlock_package ?? input;
}

function ethersDomain(domain) {
  return {
    name: "MSCBridgeVault",
    version: "1",
    chainId: BigInt(tronTip712ChainID(domain?.chainId)),
    verifyingContract: normalizeTronAddress(domain?.verifyingContract, "domain verifying contract").solidity,
  };
}

function ethersAuthorization(authorization) {
  return {
    sourceChainId: fixedHex(authorization?.sourceChainId, 32, "authorization source chain ID"),
    sourceTxHash: fixedHex(authorization?.sourceTxHash, 32, "authorization source transaction hash"),
    sourceLogIndex: uintString(authorization?.sourceLogIndex, "authorization source log index"),
    token: normalizeTronAddress(authorization?.token, "authorization token").solidity,
    recipient: normalizeTronAddress(authorization?.recipient, "authorization recipient").solidity,
    amount: uintString(authorization?.amount, "authorization amount", UINT256_MAX),
    validUntil: uintString(authorization?.validUntil, "authorization expiry"),
    committeeEpoch: uintString(authorization?.committeeEpoch, "authorization committee epoch"),
  };
}

function releaseContext(deployment, gateway, authorization) {
  return {
    deployment: {
      vault: deployment.vault,
      executor: deployment.executor,
      chainId: deployment.chainId,
      tip712ChainId: deployment.tip712ChainId,
      feeLimitSun: deployment.feeLimitSun,
    },
    gateway,
    authorization,
  };
}

function buildTransaction(artifact, signatures) {
  const authorization = ethersAuthorization(artifact.authorization);
  const calldata = UNLOCK_INTERFACE.encodeFunctionData("unlock", [
    authorization,
    signatures.map((entry) => entry.signature),
  ]);
  return {
    endpoint: "/wallet/triggersmartcontract",
    method: "POST",
    body: {
      owner_address: artifact.deployment.executor,
      contract_address: artifact.deployment.vault,
      function_selector: TRON_UNLOCK_SELECTOR,
      parameter: calldata.slice(10),
      fee_limit: Number(artifact.deployment.feeLimitSun),
      call_value: 0,
      visible: true,
    },
    calldata: calldata.slice(2),
  };
}

export function prepareTronRelease({ deployment, unlockInstruction, validUntil, nowUnix }) {
  if (deployment?.version !== TRON_DEPLOYMENT_VERSION) {
    throw new Error("unsupported TRON vault deployment record");
  }
  const instruction = unwrapInstruction(unlockInstruction);
  const payload = instruction?.certificate_payload;
  if (!payload || instruction.authorized !== true) {
    throw new Error("gateway unlock instruction is not threshold-authorized");
  }
  const requiredQuorum = Number(instruction.required_quorum);
  if (!Number.isSafeInteger(requiredQuorum) || requiredQuorum < 1 ||
      !Array.isArray(instruction.validator_signatures) ||
      instruction.validator_signatures.length < requiredQuorum) {
    throw new Error("gateway unlock instruction has no authorization quorum evidence");
  }
  if (payload.bridge_version !== "msc-bridge-v5") {
    throw new Error("unsupported gateway bridge protocol version");
  }

  const vault = normalizeTronAddress(deployment.contract?.address, "vault address");
  const executor = normalizeTronAddress(deployment.contract?.release_executor, "release executor");
  const token = normalizeTronAddress(deployment.route?.token_address, "route token address");
  const recipient = normalizeTronAddress(payload.external_recipient, "external recipient");
  if (normalizeTronAddress(payload.bridge_contract, "gateway bridge contract").base58 !== vault.base58 ||
      normalizeTronAddress(payload.vault_address, "gateway vault address").base58 !== vault.base58 ||
      normalizeTronAddress(payload.origin_asset, "gateway origin asset").base58 !== token.base58) {
    throw new Error("gateway route does not match the TRON deployment record");
  }
  if (String(payload.destination_chain_id) !== String(deployment.network?.chain_id) ||
      String(payload.asset_denom) !== String(deployment.route?.asset_denom)) {
    throw new Error("gateway chain or asset does not match the TRON deployment record");
  }

  const sourceChainID = fixedHex(deployment.contract?.msc_source_chain_hash, 32, "MSC source chain hash");
  if (sourceChainID !== id("91938").toLowerCase() || deployment.contract?.msc_source_chain_id !== "91938") {
    throw new Error("deployment record does not target MSC protocol chain ID 91938");
  }
  const burnHash = transactionHash(payload.msc_burn_transaction_id, "MSC burn transaction hash");
  const burnLogIndex = uintString(payload.msc_burn_log_index, "MSC burn log index");
  if (burnLogIndex !== "0") throw new Error("MSC consensus burn log index must be zero");
  const withdrawalID = computeVaultWithdrawalID(sourceChainID, burnHash, burnLogIndex);
  if (fixedHex(payload.external_withdrawal_id, 32, "external withdrawal ID") !== withdrawalID) {
    throw new Error("gateway external withdrawal ID does not match the vault formula");
  }

  const decimals = Number(deployment.route?.decimals);
  if (!Number.isSafeInteger(decimals) || decimals < 0 || decimals > 30) {
    throw new Error("route decimals are invalid");
  }
  let amount;
  try {
    amount = parseUnits(requiredString(payload.external_amount, "external amount"), decimals);
  } catch {
    throw new Error("external amount is not exactly representable in route decimals");
  }
  if (amount <= 0n) throw new Error("external amount must be positive");

  const now = BigInt(uintString(nowUnix, "current Unix time"));
  const expiry = BigInt(uintString(validUntil, "valid-until Unix time"));
  if (expiry <= now || expiry > now + BigInt(TRON_RELEASE_MAX_LIFETIME)) {
    throw new Error("valid-until must be in the future and no more than seven days away");
  }

  const epoch = uintString(deployment.contract?.committee_epoch, "committee epoch");
  const members = (deployment.contract?.committee_members ?? []).map(
    (value, index) => normalizeTronAddress(value, `committee member ${index}`).base58,
  );
  const uniqueMembers = new Set(members);
  const threshold = Number(deployment.contract?.committee_threshold);
  if (members.length < 3 || uniqueMembers.size !== members.length ||
      !Number.isSafeInteger(threshold) || threshold < Math.ceil((2 * members.length) / 3) ||
      threshold > members.length) {
    throw new Error("deployment committee is invalid");
  }
  if (uniqueMembers.has(executor.base58)) {
    throw new Error("release executor must be separate from the vault committee");
  }

  const tip712ChainId = tronTip712ChainID(deployment.network?.tip712_chain_id);
  const feeLimitSun = uintString(
    deployment.network?.release_fee_limit_sun,
    "release fee limit",
    TRON_MAX_FEE_LIMIT_SUN,
  );
  if (feeLimitSun === "0") throw new Error("release fee limit must be positive");
  const domain = {
    name: "MSCBridgeVault",
    version: "1",
    chainId: tip712ChainId,
    verifyingContract: vault.base58,
  };
  const authorization = {
    sourceChainId: sourceChainID,
    sourceTxHash: burnHash,
    sourceLogIndex: burnLogIndex,
    token: token.base58,
    recipient: recipient.base58,
    amount: amount.toString(),
    validUntil: expiry.toString(),
    committeeEpoch: epoch,
  };
  const gateway = {
    bridgeID: requiredString(payload.bridge_id, "bridge ID"),
    transferID: requiredString(payload.transfer_id, "transfer ID"),
    certificatePayloadHash: transactionHash(
      instruction.certificate_payload_hash,
      "certificate payload hash",
    ),
    withdrawalID,
  };
  const artifactDeployment = {
    vault: vault.base58,
    executor: executor.base58,
    chainId: requiredString(deployment.network?.chain_id, "destination chain ID"),
    tip712ChainId,
    feeLimitSun,
    deploymentTransactionHash: transactionHash(
      deployment.contract?.deployment_tx_hash,
      "deployment transaction hash",
    ),
    runtimeCodeHash: fixedHex(deployment.contract?.runtime_code_hash, 32, "runtime code hash"),
  };
  const contextHash = sha256JSON(releaseContext(artifactDeployment, gateway, authorization));
  return {
    version: TRON_RELEASE_VERSION,
    deployment: artifactDeployment,
    gateway,
    contextHash,
    domain,
    types: EVM_UNLOCK_TYPES,
    authorization,
    authorizationDigest: TypedDataEncoder.hash(
      ethersDomain(domain),
      EVM_UNLOCK_TYPES,
      ethersAuthorization(authorization),
    ),
    committee: { epoch, threshold, members },
    signatures: [],
    transaction: null,
  };
}

export function validateTronReleaseArtifact(input, requireThreshold = false) {
  if (input?.version !== TRON_RELEASE_VERSION) {
    throw new Error("unsupported TRON release artifact version");
  }
  const artifact = structuredClone(input);
  const vault = normalizeTronAddress(artifact.deployment?.vault, "deployment vault");
  const executor = normalizeTronAddress(artifact.deployment?.executor, "deployment executor");
  const chainId = requiredString(artifact.deployment?.chainId, "deployment chain ID");
  const tip712ChainId = tronTip712ChainID(artifact.deployment?.tip712ChainId);
  const feeLimitSun = uintString(
    artifact.deployment?.feeLimitSun,
    "deployment fee limit",
    TRON_MAX_FEE_LIMIT_SUN,
  );
  if (feeLimitSun === "0") throw new Error("deployment fee limit must be positive");
  transactionHash(artifact.deployment?.deploymentTransactionHash, "deployment transaction hash");
  fixedHex(artifact.deployment?.runtimeCodeHash, 32, "runtime code hash");
  if (artifact.domain?.name !== "MSCBridgeVault" || artifact.domain?.version !== "1" ||
      tronTip712ChainID(artifact.domain?.chainId) !== tip712ChainId ||
      normalizeTronAddress(artifact.domain?.verifyingContract, "domain verifying contract").base58 !== vault.base58 ||
      canonicalJSON(artifact.types) !== canonicalJSON(EVM_UNLOCK_TYPES)) {
    throw new Error("TIP-712 domain or type schema is not canonical");
  }

  const authorization = artifact.authorization ?? {};
  const normalizedAuthorization = ethersAuthorization(authorization);
  if (normalizedAuthorization.amount === "0") throw new Error("authorization amount must be positive");
  const withdrawalID = computeVaultWithdrawalID(
    normalizedAuthorization.sourceChainId,
    normalizedAuthorization.sourceTxHash,
    normalizedAuthorization.sourceLogIndex,
  );
  if (fixedHex(artifact.gateway?.withdrawalID, 32, "gateway withdrawal ID") !== withdrawalID) {
    throw new Error("release withdrawal ID is not canonical");
  }
  transactionHash(artifact.gateway?.certificatePayloadHash, "gateway certificate hash");
  requiredString(artifact.gateway?.bridgeID, "gateway bridge ID");
  requiredString(artifact.gateway?.transferID, "gateway transfer ID");

  const members = (artifact.committee?.members ?? []).map(
    (value, index) => normalizeTronAddress(value, `committee member ${index}`).base58,
  );
  const threshold = Number(artifact.committee?.threshold);
  const epoch = uintString(artifact.committee?.epoch, "committee epoch");
  if (epoch !== normalizedAuthorization.committeeEpoch || members.length < 3 ||
      new Set(members).size !== members.length || !Number.isSafeInteger(threshold) ||
      threshold < Math.ceil((2 * members.length) / 3) || threshold > members.length ||
      members.includes(executor.base58)) {
    throw new Error("release committee is invalid");
  }
  const digest = TypedDataEncoder.hash(
    ethersDomain(artifact.domain),
    EVM_UNLOCK_TYPES,
    normalizedAuthorization,
  );
  if (fixedHex(artifact.authorizationDigest, 32, "authorization digest") !== digest) {
    throw new Error("authorization digest mismatch");
  }
  const tronWebDigest = TronWebUtils._TypedDataEncoder.hash(
    artifact.domain,
    EVM_UNLOCK_TYPES,
    artifact.authorization,
  );
  if (tronWebDigest !== digest) throw new Error("TIP-712 SDK digest mismatch");
  const contextHash = sha256JSON(releaseContext(
    { vault: vault.base58, executor: executor.base58, chainId, tip712ChainId, feeLimitSun },
    artifact.gateway,
    artifact.authorization,
  ));
  if (requiredString(artifact.contextHash, "context hash") !== contextHash) {
    throw new Error("release context hash mismatch");
  }

  const memberSet = new Set(members);
  const signatures = Array.isArray(artifact.signatures) ? artifact.signatures : [];
  let previous = null;
  for (const [index, entry] of signatures.entries()) {
    const signer = normalizeTronAddress(entry?.signer, `signature ${index} signer`).base58;
    const signature = requiredString(entry?.signature, `signature ${index}`);
    let recovered = "";
    try {
      recovered = normalizeTronAddress(verifyTypedData(
        ethersDomain(artifact.domain),
        EVM_UNLOCK_TYPES,
        normalizedAuthorization,
        signature,
      )).base58;
    } catch {
      throw new Error(`signature ${index} is malformed`);
    }
    if (!/^0x[0-9a-fA-F]{130}$/.test(signature) || !memberSet.has(signer) ||
        recovered !== signer || (previous && addressOrder(previous, signer) >= 0)) {
      throw new Error(`signature ${index} is invalid, unauthorized, duplicate, or unsorted`);
    }
    previous = signer;
  }
  if (signatures.length > members.length) throw new Error("release has too many signatures");
  const authorized = signatures.length >= threshold;
  if (requireThreshold && !authorized) {
    throw new Error(`TRON vault committee threshold not met: got ${signatures.length} need ${threshold}`);
  }
  const expectedTransaction = authorized ? buildTransaction(artifact, signatures) : null;
  if (canonicalJSON(artifact.transaction) !== canonicalJSON(expectedTransaction)) {
    throw new Error("release transaction request does not match signatures and authorization");
  }
  return { artifact, authorized, recoveredSigners: signatures.map((entry) => entry.signer) };
}

export async function signTronReleaseArtifact(input, privateKey) {
  const { artifact } = validateTronReleaseArtifact(input, false);
  const rawKey = requiredString(privateKey, "release signer private key").replace(/^0x/i, "");
  if (!/^[0-9a-fA-F]{64}$/.test(rawKey) || /^0+$/.test(rawKey)) {
    throw new Error("release signer private key must be a non-zero 32-byte hex key");
  }
  const signer = normalizeTronAddress(
    TronWeb.address.fromPrivateKey(rawKey),
    "release signer",
  ).base58;
  if (!artifact.committee.members.includes(signer)) {
    throw new Error("release signer is not in the TRON vault committee");
  }
  const tronWeb = new TronWeb({ fullHost: "http://127.0.0.1:9090" });
  const signature = await tronWeb.trx.signTypedData(
    artifact.domain,
    EVM_UNLOCK_TYPES,
    artifact.authorization,
    rawKey,
  );
  const entries = artifact.signatures.filter((entry) => entry.signer !== signer);
  entries.push({ signer, signature });
  entries.sort((left, right) => addressOrder(left.signer, right.signer));
  artifact.signatures = entries;
  artifact.transaction = entries.length >= artifact.committee.threshold
    ? buildTransaction(artifact, entries)
    : null;
  validateTronReleaseArtifact(artifact, false);
  return artifact;
}

export function mergeTronReleaseArtifacts(inputs) {
  if (!Array.isArray(inputs) || inputs.length < 2) {
    throw new Error("at least two signed TRON release artifacts are required");
  }
  const validated = inputs.map((input) => validateTronReleaseArtifact(input, false).artifact);
  const base = structuredClone(validated[0]);
  const identity = (artifact) => canonicalJSON({
    version: artifact.version,
    deployment: artifact.deployment,
    gateway: artifact.gateway,
    contextHash: artifact.contextHash,
    domain: artifact.domain,
    types: artifact.types,
    authorization: artifact.authorization,
    authorizationDigest: artifact.authorizationDigest,
    committee: artifact.committee,
  });
  const expectedIdentity = identity(base);
  const signatures = new Map();
  for (const artifact of validated) {
    if (identity(artifact) !== expectedIdentity) {
      throw new Error("TRON release artifacts authorize different withdrawals");
    }
    for (const entry of artifact.signatures) signatures.set(entry.signer, entry);
  }
  base.signatures = [...signatures.values()].sort(
    (left, right) => addressOrder(left.signer, right.signer),
  );
  base.transaction = base.signatures.length >= base.committee.threshold
    ? buildTransaction(base, base.signatures)
    : null;
  validateTronReleaseArtifact(base, true);
  return base;
}
