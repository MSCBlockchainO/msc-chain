import { createHash } from "node:crypto";
import {
  AbiCoder,
  Interface,
  TypedDataEncoder,
  getAddress,
  id,
  keccak256,
  parseUnits,
  verifyTypedData,
} from "ethers";

export const EVM_RELEASE_VERSION = "msc-bridge-evm-release-v1";
export const EVM_RELEASE_MAX_LIFETIME = 7 * 24 * 60 * 60;
export const EVM_UNLOCK_TYPES = Object.freeze({
  UnlockAuthorization: Object.freeze([
    Object.freeze({ name: "sourceChainId", type: "bytes32" }),
    Object.freeze({ name: "sourceTxHash", type: "bytes32" }),
    Object.freeze({ name: "sourceLogIndex", type: "uint64" }),
    Object.freeze({ name: "token", type: "address" }),
    Object.freeze({ name: "recipient", type: "address" }),
    Object.freeze({ name: "amount", type: "uint256" }),
    Object.freeze({ name: "validUntil", type: "uint64" }),
    Object.freeze({ name: "committeeEpoch", type: "uint64" }),
  ]),
});

const UNLOCK_INTERFACE = new Interface([
  "function unlock((bytes32 sourceChainId,bytes32 sourceTxHash,uint64 sourceLogIndex,address token,address recipient,uint256 amount,uint64 validUntil,uint64 committeeEpoch) authorization,bytes[] signatures) returns (bytes32 withdrawalId)",
]);
const UINT64_MAX = (1n << 64n) - 1n;

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

function uintString(value, label, maximum = UINT64_MAX) {
  const normalized = requiredString(value, label);
  if (!/^(0|[1-9][0-9]*)$/.test(normalized)) throw new Error(`${label} must be an unsigned decimal integer`);
  const parsed = BigInt(normalized);
  if (parsed > maximum) throw new Error(`${label} is out of range`);
  return parsed.toString();
}

function strictAddress(value, label) {
  try {
    const address = getAddress(requiredString(value, label));
    if (/^0x0{40}$/i.test(address)) throw new Error("zero");
    return address;
  } catch {
    throw new Error(`${label} must be a non-zero EVM address`);
  }
}

function addressOrder(left, right) {
  const a = BigInt(left.toLowerCase());
  const b = BigInt(right.toLowerCase());
  return a < b ? -1 : a > b ? 1 : 0;
}

function canonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

function sha256JSON(value) {
  return createHash("sha256").update(canonicalJSON(value)).digest("hex");
}

export function computeVaultWithdrawalID(sourceChainHash, burnTxHash, burnLogIndex) {
  const source = fixedHex(sourceChainHash, 32, "MSC source chain hash");
  const transaction = fixedHex(burnTxHash, 32, "MSC burn transaction hash");
  const index = uintString(burnLogIndex, "MSC burn log index");
  return keccak256(AbiCoder.defaultAbiCoder().encode(
    ["bytes32", "bytes32", "bytes32", "uint64"],
    [id("MSC_BRIDGE_WITHDRAWAL_V1"), source, transaction, index],
  ));
}

function unwrapInstruction(input) {
  return input?.unlock_instruction ?? input?.unlock_package ?? input;
}

export function prepareEVMRelease({ deployment, unlockInstruction, validUntil, nowUnix }) {
  if (deployment?.version !== "msc-bridge-evm-deployment-v1") {
    throw new Error("unsupported EVM vault deployment record");
  }
  const instruction = unwrapInstruction(unlockInstruction);
  const payload = instruction?.certificate_payload;
  if (!payload || instruction.authorized !== true) throw new Error("gateway unlock instruction is not threshold-authorized");
  const requiredQuorum = Number(instruction.required_quorum);
  if (!Number.isSafeInteger(requiredQuorum) || requiredQuorum < 1 || !Array.isArray(instruction.validator_signatures) || instruction.validator_signatures.length < requiredQuorum) {
    throw new Error("gateway unlock instruction has no authorization quorum evidence");
  }
  if (payload.bridge_version !== "msc-bridge-v5") throw new Error("unsupported gateway bridge protocol version");

  const vault = strictAddress(deployment.contract?.address, "vault address");
  const token = strictAddress(deployment.route?.token_address, "route token address");
  const recipient = strictAddress(payload.external_recipient, "external recipient");
  if (strictAddress(payload.bridge_contract, "gateway bridge contract") !== vault ||
      strictAddress(payload.vault_address, "gateway vault address") !== vault ||
      strictAddress(payload.origin_asset, "gateway origin asset") !== token) {
    throw new Error("gateway route does not match the deployment record");
  }
  if (String(payload.destination_chain_id) !== String(deployment.network?.chain_id) ||
      String(payload.asset_denom) !== String(deployment.route?.asset_denom)) {
    throw new Error("gateway chain or asset does not match the deployment record");
  }

  const sourceChainID = fixedHex(deployment.contract?.msc_source_chain_hash, 32, "MSC source chain hash");
  if (sourceChainID !== id("91938").toLowerCase() || deployment.contract?.msc_source_chain_id !== "91938") {
    throw new Error("deployment record does not target MSC protocol chain ID 91938");
  }
  const burnHash = fixedHex(`0x${String(payload.msc_burn_transaction_id ?? "").replace(/^0x/i, "")}`, 32, "MSC burn transaction hash");
  const burnLogIndex = uintString(payload.msc_burn_log_index, "MSC burn log index");
  if (burnLogIndex !== "0") throw new Error("MSC consensus burn log index must be zero");
  const withdrawalID = computeVaultWithdrawalID(sourceChainID, burnHash, burnLogIndex);
  if (fixedHex(payload.external_withdrawal_id, 32, "external withdrawal ID") !== withdrawalID) {
    throw new Error("gateway external withdrawal ID does not match the vault formula");
  }

  const decimals = Number(deployment.route?.decimals);
  if (!Number.isSafeInteger(decimals) || decimals < 0 || decimals > 30) throw new Error("route decimals are invalid");
  let amount;
  try {
    amount = parseUnits(requiredString(payload.external_amount, "external amount"), decimals);
  } catch {
    throw new Error("external amount is not exactly representable in route decimals");
  }
  if (amount <= 0n) throw new Error("external amount must be positive");

  const now = BigInt(uintString(nowUnix, "current Unix time"));
  const expiry = BigInt(uintString(validUntil, "valid-until Unix time"));
  if (expiry <= now || expiry > now + BigInt(EVM_RELEASE_MAX_LIFETIME)) {
    throw new Error("valid-until must be in the future and no more than seven days away");
  }
  const epoch = uintString(deployment.contract?.committee_epoch, "committee epoch");
  const members = (deployment.contract?.committee_members ?? []).map((value, index) => strictAddress(value, `committee member ${index}`));
  const uniqueMembers = new Set(members.map((value) => value.toLowerCase()));
  const threshold = Number(deployment.contract?.committee_threshold);
  if (members.length < 3 || uniqueMembers.size !== members.length || !Number.isSafeInteger(threshold) || threshold < Math.ceil((2 * members.length) / 3) || threshold > members.length) {
    throw new Error("deployment committee is invalid");
  }

  const domain = {
    name: "MSCBridgeVault",
    version: "1",
    chainId: uintString(deployment.network?.chain_id, "destination EVM chain ID"),
    verifyingContract: vault,
  };
  const authorization = {
    sourceChainId: sourceChainID,
    sourceTxHash: burnHash,
    sourceLogIndex: burnLogIndex,
    token,
    recipient,
    amount: amount.toString(),
    validUntil: expiry.toString(),
    committeeEpoch: epoch,
  };
  const gateway = {
    bridgeID: requiredString(payload.bridge_id, "bridge ID"),
    transferID: requiredString(payload.transfer_id, "transfer ID"),
    certificatePayloadHash: fixedHex(`0x${String(instruction.certificate_payload_hash ?? "").replace(/^0x/i, "")}`, 32, "certificate payload hash"),
    withdrawalID,
  };
  const contextHash = sha256JSON({ deployment: { vault, chainId: domain.chainId }, gateway, authorization });
  return {
    version: EVM_RELEASE_VERSION,
    deployment: {
      vault,
      chainId: domain.chainId,
      deploymentTransactionHash: fixedHex(deployment.contract.deployment_tx_hash, 32, "deployment transaction hash"),
      runtimeCodeHash: fixedHex(deployment.contract.runtime_code_hash, 32, "runtime code hash"),
    },
    gateway,
    contextHash,
    domain,
    types: EVM_UNLOCK_TYPES,
    authorization,
    authorizationDigest: TypedDataEncoder.hash(domain, EVM_UNLOCK_TYPES, authorization),
    committee: { epoch, threshold, members },
    signatures: [],
    transaction: null,
  };
}

export function validateEVMReleaseArtifact(input, requireThreshold = false) {
  if (input?.version !== EVM_RELEASE_VERSION) throw new Error("unsupported EVM release artifact version");
  const artifact = structuredClone(input);
  const vault = strictAddress(artifact.deployment?.vault, "deployment vault");
  const chainId = uintString(artifact.deployment?.chainId, "deployment chain ID");
  fixedHex(artifact.deployment?.deploymentTransactionHash, 32, "deployment transaction hash");
  fixedHex(artifact.deployment?.runtimeCodeHash, 32, "runtime code hash");
  if (artifact.domain?.name !== "MSCBridgeVault" || artifact.domain?.version !== "1" ||
      uintString(artifact.domain?.chainId, "domain chain ID") !== chainId ||
      strictAddress(artifact.domain?.verifyingContract, "domain verifying contract") !== vault ||
      canonicalJSON(artifact.types) !== canonicalJSON(EVM_UNLOCK_TYPES)) {
    throw new Error("EIP-712 domain or type schema is not canonical");
  }

  const authorization = artifact.authorization ?? {};
  const sourceChainID = fixedHex(authorization.sourceChainId, 32, "authorization source chain ID");
  const sourceTxHash = fixedHex(authorization.sourceTxHash, 32, "authorization source transaction hash");
  const sourceLogIndex = uintString(authorization.sourceLogIndex, "authorization source log index");
  strictAddress(authorization.token, "authorization token");
  strictAddress(authorization.recipient, "authorization recipient");
  const amount = uintString(authorization.amount, "authorization amount", (1n << 256n) - 1n);
  if (amount === "0") throw new Error("authorization amount must be positive");
  uintString(authorization.validUntil, "authorization expiry");
  const epoch = uintString(authorization.committeeEpoch, "authorization committee epoch");
  const withdrawalID = computeVaultWithdrawalID(sourceChainID, sourceTxHash, sourceLogIndex);
  if (fixedHex(artifact.gateway?.withdrawalID, 32, "gateway withdrawal ID") !== withdrawalID) {
    throw new Error("release withdrawal ID is not canonical");
  }
  fixedHex(artifact.gateway?.certificatePayloadHash, 32, "gateway certificate hash");
  requiredString(artifact.gateway?.bridgeID, "gateway bridge ID");
  requiredString(artifact.gateway?.transferID, "gateway transfer ID");

  const members = (artifact.committee?.members ?? []).map((value, index) => strictAddress(value, `committee member ${index}`));
  const threshold = Number(artifact.committee?.threshold);
  if (epoch !== uintString(artifact.committee?.epoch, "committee epoch") || members.length < 3 ||
      new Set(members.map((value) => value.toLowerCase())).size !== members.length ||
      !Number.isSafeInteger(threshold) || threshold < Math.ceil((2 * members.length) / 3) || threshold > members.length) {
    throw new Error("release committee is invalid");
  }
  const digest = TypedDataEncoder.hash(artifact.domain, EVM_UNLOCK_TYPES, authorization);
  if (fixedHex(artifact.authorizationDigest, 32, "authorization digest") !== digest) {
    throw new Error("authorization digest mismatch");
  }
  const contextHash = sha256JSON({ deployment: { vault, chainId }, gateway: artifact.gateway, authorization });
  if (requiredString(artifact.contextHash, "context hash") !== contextHash) throw new Error("release context hash mismatch");

  const memberSet = new Set(members.map((value) => value.toLowerCase()));
  const signatures = Array.isArray(artifact.signatures) ? artifact.signatures : [];
  let previous = null;
  for (const [index, entry] of signatures.entries()) {
    const signer = strictAddress(entry?.signer, `signature ${index} signer`);
    const signature = requiredString(entry?.signature, `signature ${index}`);
    if (!/^0x[0-9a-fA-F]{130}$/.test(signature) || !memberSet.has(signer.toLowerCase()) ||
        verifyTypedData(artifact.domain, EVM_UNLOCK_TYPES, authorization, signature) !== signer ||
        (previous && addressOrder(previous, signer) >= 0)) {
      throw new Error(`signature ${index} is invalid, unauthorized, duplicate, or unsorted`);
    }
    previous = signer;
  }
  if (signatures.length > members.length) throw new Error("release has too many signatures");
  const authorized = signatures.length >= threshold;
  if (requireThreshold && !authorized) throw new Error(`EVM vault committee threshold not met: got ${signatures.length} need ${threshold}`);
  const expectedTransaction = authorized ? {
    to: vault,
    value: "0",
    data: UNLOCK_INTERFACE.encodeFunctionData("unlock", [authorization, signatures.map((entry) => entry.signature)]),
  } : null;
  if (canonicalJSON(artifact.transaction) !== canonicalJSON(expectedTransaction)) {
    throw new Error("release transaction calldata does not match signatures and authorization");
  }
  return { artifact, authorized, recoveredSigners: signatures.map((entry) => entry.signer) };
}

export async function signEVMReleaseArtifact(input, wallet) {
  const { artifact } = validateEVMReleaseArtifact(input, false);
  const signer = strictAddress(await wallet.getAddress(), "release signer");
  if (!artifact.committee.members.some((member) => member.toLowerCase() === signer.toLowerCase())) {
    throw new Error("release signer is not in the vault committee");
  }
  const signature = await wallet.signTypedData(artifact.domain, EVM_UNLOCK_TYPES, artifact.authorization);
  const entries = artifact.signatures.filter((entry) => entry.signer.toLowerCase() !== signer.toLowerCase());
  entries.push({ signer, signature });
  entries.sort((left, right) => addressOrder(left.signer, right.signer));
  artifact.signatures = entries;
  artifact.transaction = entries.length >= artifact.committee.threshold ? {
    to: artifact.deployment.vault,
    value: "0",
    data: UNLOCK_INTERFACE.encodeFunctionData("unlock", [artifact.authorization, entries.map((entry) => entry.signature)]),
  } : null;
  validateEVMReleaseArtifact(artifact, false);
  return artifact;
}

export function mergeEVMReleaseArtifacts(inputs) {
  if (!Array.isArray(inputs) || inputs.length < 2) throw new Error("at least two signed release artifacts are required");
  const validated = inputs.map((input) => validateEVMReleaseArtifact(input, false).artifact);
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
    if (identity(artifact) !== expectedIdentity) throw new Error("release artifacts authorize different withdrawals");
    for (const entry of artifact.signatures) signatures.set(entry.signer.toLowerCase(), entry);
  }
  base.signatures = [...signatures.values()].sort((left, right) => addressOrder(left.signer, right.signer));
  base.transaction = base.signatures.length >= base.committee.threshold ? {
    to: base.deployment.vault,
    value: "0",
    data: UNLOCK_INTERFACE.encodeFunctionData("unlock", [base.authorization, base.signatures.map((entry) => entry.signature)]),
  } : null;
  validateEVMReleaseArtifact(base, true);
  return base;
}
