"use strict";

const $ = (id) => document.getElementById(id);
const BRIDGE_ADMIN_RPC_KEY = "msc_bridge_admin_rpc_v1";
const BRIDGE_ADMIN_TOKEN_KEY = "msc_bridge_admin_token_v1";
const MAX_OBSERVER_ARTIFACT_BYTES = 8 * 1024 * 1024;
const MAX_DEPLOYMENT_RECORD_BYTES = 2 * 1024 * 1024;
const TRON_MAINNET_GENESIS_BLOCK_ID = "00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc";
const TRON_MAINNET_USDT = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t";

const adminState = {
  rpc: "",
  token: "",
  snapshot: null,
};

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>'"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
}

function normalizeRPC(value) {
  let rpc = String(value || "").trim();
  if (!rpc && /^https?:$/.test(window.location.protocol)) rpc = window.location.origin;
  if (!/^[a-z][a-z\d+.-]*:\/\//i.test(rpc)) rpc = `https://${rpc}`;
  return rpc.replace(/\/+$/, "");
}

function setResult(value, tone = "") {
  const target = $("bridgeAdminActionResult") || $("bridgeAdminConnectStatus");
  if (!target) return;
  target.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
  target.classList.toggle("success", tone === "success");
  target.classList.toggle("error", tone === "error");
}

async function adminRequest(path, body) {
  if (!adminState.rpc || !adminState.token) throw new Error("Connect with the node admin token first.");
  const response = await fetch(`${adminState.rpc}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      "Authorization": `Bearer ${adminState.token}`,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  let data;
  try { data = text ? JSON.parse(text) : null; } catch (_) { data = text; }
  if (!response.ok) throw new Error(data?.message || data?.error || text || response.statusText);
  return data;
}

function formObject(form) {
  return Object.fromEntries(new FormData(form).entries());
}

function numberOrZero(value) {
  const parsed = Number(value || 0);
  return Number.isFinite(parsed) && parsed >= 0 ? Math.trunc(parsed) : 0;
}

function validHTTPSURL(value) {
  try {
    const parsed = new URL(String(value || ""));
    return parsed.protocol === "https:" && !parsed.username && !parsed.password && !!parsed.hostname;
  } catch (_) {
    return false;
  }
}

function routeRow(route) {
  const ready = !!route.ready;
  const checkpoint = Number(route.checkpoint_height || 0);
  return `<div class="bridge-route-row ${ready ? "success" : "warn"}">
    <div class="bridge-route-identity"><strong>${escapeHTML(route.chain_name || route.chain_id)}</strong><span>${escapeHTML(route.asset_symbol || route.asset_denom)} / ${escapeHTML(route.route_id)}${checkpoint ? ` / checkpoint #${escapeHTML(checkpoint)}` : " / no checkpoint"}</span></div>
    <div><span>Chain</span><strong>${escapeHTML(route.status)}</strong></div>
    <div><span>Finality</span><strong>${escapeHTML(route.finality_status)}</strong></div>
    <div><span>Confirmations</span><strong>${escapeHTML(route.min_confirmations || "-")}</strong></div>
    <div><span>Readiness</span><strong class="${ready ? "success" : "warn"}">${escapeHTML(ready ? "Ready" : route.unavailable_reason || "Not ready")}</strong></div>
  </div>`;
}

function checkpointRow(checkpoint, latestByChain) {
  const id = String(checkpoint.checkpoint_id || "");
  const chain = String(checkpoint.source_chain_id || "-");
  const latest = Object.values(latestByChain || {}).some((value) => String(value).toLowerCase() === id.toLowerCase());
  const issued = Number(checkpoint.issued_at_unix || 0);
  return `<div class="bridge-route-row ${latest ? "success" : ""}">
    <div class="bridge-route-identity"><strong>${escapeHTML(chain)}${latest ? " / Latest" : ""}</strong><span class="mono" title="${escapeHTML(id)}">${escapeHTML(id ? `${id.slice(0, 12)}...${id.slice(-8)}` : "-")}</span></div>
    <div><span>Height</span><strong>${escapeHTML(checkpoint.height || "-")}</strong></div>
    <div><span>Observed</span><strong>${escapeHTML(checkpoint.observed_height || "-")}</strong></div>
    <div><span>Signers</span><strong>${escapeHTML(checkpoint.validator_signatures?.length || 0)}</strong></div>
    <div><span>Issued</span><strong>${escapeHTML(issued ? new Date(issued * 1000).toLocaleString() : "-")}</strong></div>
  </div>`;
}

function transferRow(transfer) {
  const direction = String(transfer.direction || "unknown");
  const complete = String(transfer.status || "").toLowerCase() === "completed";
  return `<div class="bridge-route-row ${complete ? "success" : "warn"}">
    <div class="bridge-route-identity"><strong>${escapeHTML(direction === "withdrawal" ? "Withdrawal" : "Deposit")} / ${escapeHTML(transfer.asset_denom || "Asset")}</strong><span>${escapeHTML(transfer.transfer_id || "-")}</span></div>
    <div><span>Route</span><strong>${escapeHTML(transfer.route_id || "-")}</strong></div>
    <div><span>Amount</span><strong>${escapeHTML(transfer.amount || "-")}</strong></div>
    <div><span>Status</span><strong class="${complete ? "success" : "warn"}">${escapeHTML(String(transfer.status || "unknown").replace(/_/g, " "))}</strong></div>
    <div><span>Actions</span><button type="button" data-use-transfer="${escapeHTML(transfer.transfer_id || "")}" title="Use this transfer ID"><i data-lucide="panel-top-open"></i></button></div>
  </div>`;
}

function renderAdminState(payload) {
  const state = payload?.state || {};
  const routes = Array.isArray(payload?.routes) ? payload.routes : [];
  const transfers = Object.values(state.transfers || {})
    .sort((a, b) => Number(b.updated_at_unix || 0) - Number(a.updated_at_unix || 0))
    .slice(0, 100);
  const checkpoints = Object.values(state.checkpoints || {})
    .sort((a, b) => Number(b.height || 0) - Number(a.height || 0))
    .slice(0, 100);
  adminState.snapshot = payload;
  $("bridgeAdminDashboard").hidden = false;
  $("adminPaused").textContent = state.paused ? "Paused" : "Unpaused";
  $("adminPaused").className = `value ${state.paused ? "error" : "success"}`;
  $("adminChainsCount").textContent = String(state.chains?.length || 0);
  $("adminAssetsCount").textContent = String(state.assets?.length || 0);
  $("adminContractsCount").textContent = String(state.contracts?.length || 0);
  $("adminValidatorsCount").textContent = String(state.validators?.length || 0);
  $("adminCheckpointsCount").textContent = String(Object.keys(state.checkpoints || {}).length);
  const stateRoot = String(state.state_root || "");
  $("adminStateRoot").textContent = stateRoot ? `${stateRoot.slice(0, 10)}...${stateRoot.slice(-8)}` : "-";
  $("adminStateRoot").title = stateRoot;
  $("adminPauseState").value = String(!!state.paused);
  $("adminPauseReason").value = state.pause_reason || "";
  $("bridgeAdminRegistry").innerHTML = routes.length ? routes.map(routeRow).join("") : `<div class="list-item">No asset routes registered.</div>`;
  $("bridgeAdminCheckpoints").innerHTML = checkpoints.length ? checkpoints.map((checkpoint) => checkpointRow(checkpoint, state.latest_checkpoint_by_chain)).join("") : `<div class="list-item">No threshold finality checkpoints registered.</div>`;
  $("bridgeAdminTransfers").innerHTML = transfers.length ? transfers.map(transferRow).join("") : `<div class="list-item">No bridge transfers recorded.</div>`;
  if (window.lucide) window.lucide.createIcons();
}

async function refreshAdminState() {
  const payload = await adminRequest("/bridge/admin/state");
  renderAdminState(payload);
  return payload;
}

async function connectAdmin(event) {
  event?.preventDefault();
  adminState.rpc = normalizeRPC($("bridgeAdminRPC").value);
  adminState.token = $("bridgeAdminToken").value.trim();
  if (!adminState.rpc || !adminState.token) return;
  $("bridgeAdminConnectStatus").textContent = "Authenticating...";
  try {
    await refreshAdminState();
    sessionStorage.setItem(BRIDGE_ADMIN_RPC_KEY, adminState.rpc);
    sessionStorage.setItem(BRIDGE_ADMIN_TOKEN_KEY, adminState.token);
    $("bridgeAdminConnectStatus").textContent = `Connected to ${adminState.rpc}`;
    $("bridgeAdminConnectStatus").classList.add("success");
    setResult("Admin state loaded.", "success");
  } catch (error) {
    adminState.token = "";
    sessionStorage.removeItem(BRIDGE_ADMIN_TOKEN_KEY);
    $("bridgeAdminDashboard").hidden = true;
    $("bridgeAdminConnectStatus").textContent = error.message || "Admin authentication failed";
    $("bridgeAdminConnectStatus").classList.add("error");
  }
}

function bindAdminForm(id, path, transform) {
  $(id)?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const button = event.currentTarget.querySelector("button[type='submit']");
    if (button) button.disabled = true;
    try {
      const body = transform(formObject(event.currentTarget));
      const result = await adminRequest(path, body);
      setResult(result, "success");
      await refreshAdminState();
    } catch (error) {
      setResult(error.message || "Admin operation failed", "error");
    } finally {
      if (button) button.disabled = false;
    }
  });
}

async function loadObserverArtifact(event) {
  const input = event.currentTarget;
  const file = input.files?.[0];
  if (!file) return;
	try {
		if (file.size <= 0 || file.size > MAX_OBSERVER_ARTIFACT_BYTES) throw new Error("Observer artifact must be between 1 byte and 8 MB.");
		const artifact = JSON.parse(await file.text());
		const chainType = String(artifact?.chain_type || "").toLowerCase();
		if (artifact?.version !== "msc-bridge-observation-v3" || !new Set(["evm", "tron", "solana"]).has(chainType) || !artifact?.checkpoint || !Array.isArray(artifact?.proofs) || artifact.proofs.length === 0) {
			throw new Error("Unsupported or incomplete observer artifact.");
		}
		const sameIdentifier = (left, right) => chainType === "solana"
			? String(left || "").trim() === String(right || "").trim()
			: String(left || "").trim().toLowerCase() === String(right || "").trim().toLowerCase();
		const validWithdrawalBinding = (proof) => {
			const eventType = String(proof?.event_type || "lock").trim().toLowerCase();
			const withdrawalID = String(proof?.withdrawal_id || "").trim();
			if (eventType === "lock") return withdrawalID === "";
			return eventType === "unlock" && /^0x(?!0{64}$)[0-9a-f]{64}$/.test(withdrawalID);
		};
		if (
			artifact.checkpoint.version !== "msc-bridge-checkpoint-v2"
			|| !/^bcp_[0-9a-f]{64}$/.test(String(artifact.checkpoint.checkpoint_id || ""))
			|| !validObserverBlockID(chainType, artifact.checkpoint.block_hash)
			|| !artifact.proofs.every((proof) => proof?.version === "msc-bridge-v5"
				&& proof?.source_chain_id === artifact.checkpoint.source_chain_id
				&& proof?.checkpoint_id === artifact.checkpoint.checkpoint_id
				&& (chainType !== "evm" || proof?.evm_receipt_proof?.version === "msc-evm-receipt-proof-v1")
				&& (chainType === "evm" || !proof?.evm_receipt_proof)
				&& (chainType !== "tron" || proof?.tron_transaction_proof?.version === "msc-tron-transaction-proof-v1")
				&& (chainType === "tron" || !proof?.tron_transaction_proof)
				&& validWithdrawalBinding(proof)
				&& validObserverTransactionID(chainType, proof?.source_tx_hash)
				&& validObserverBlockID(chainType, proof?.source_block_hash)
				&& sameIdentifier(proof?.source_block_hash, artifact.checkpoint.block_hash))
		) {
			throw new Error("Observer artifact checkpoint is not canonical.");
		}
    const checkpointField = document.querySelector('#bridgeCheckpointForm [name="checkpoint"]');
    const proofField = document.querySelector('#bridgeEventForm [name="proof"]');
    if (checkpointField) checkpointField.value = JSON.stringify(artifact.checkpoint, null, 2);
    if (proofField) proofField.value = JSON.stringify(artifact.proofs[0], null, 2);
		setResult(`Loaded ${chainType.toUpperCase()} checkpoint ${artifact.checkpoint.checkpoint_id} with ${artifact.proofs.length} proof${artifact.proofs.length === 1 ? "" : "s"}. Review and submit the checkpoint first.`, "success");
  } catch (error) {
    setResult(error.message || "Observer artifact import failed", "error");
  } finally {
    input.value = "";
  }
}

const BRIDGE_BASE58_ALPHABET = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";

function decodeBase58Bytes(value) {
	const text = String(value || "").trim();
	if (!text || text.length > 128) return null;
	let number = 0n;
	for (const character of text) {
		const digit = BRIDGE_BASE58_ALPHABET.indexOf(character);
		if (digit < 0) return null;
		number = number * 58n + BigInt(digit);
	}
	const decoded = [];
	for (let remaining = number; remaining > 0n; remaining >>= 8n) {
		decoded.unshift(Number(remaining & 0xffn));
	}
	let leadingZeroes = 0;
	while (leadingZeroes < text.length && text[leadingZeroes] === "1") leadingZeroes++;
	return Uint8Array.from([...new Array(leadingZeroes).fill(0), ...decoded]);
}

function decodedBase58Length(value) {
	return decodeBase58Bytes(value)?.length || 0;
}

async function validTronBase58CheckAddress(value) {
	const decoded = decodeBase58Bytes(value);
	if (!decoded || decoded.length !== 25 || decoded[0] !== 0x41 || !globalThis.crypto?.subtle) return false;
	const payload = decoded.slice(0, 21);
	const first = new Uint8Array(await globalThis.crypto.subtle.digest("SHA-256", payload));
	const second = new Uint8Array(await globalThis.crypto.subtle.digest("SHA-256", first));
	let mismatch = 0;
	for (let index = 0; index < 4; index++) mismatch |= decoded[21 + index] ^ second[index];
	return mismatch === 0;
}

function validObserverTransactionID(chainType, value) {
	const text = String(value || "").trim();
	if (chainType === "solana") return decodedBase58Length(text) === 64;
	return /^(?:0x)?[0-9a-fA-F]{64}$/.test(text);
}

function validObserverBlockID(chainType, value) {
	const text = String(value || "").trim();
	if (chainType === "solana") return decodedBase58Length(text) === 32;
	return /^(?:0x)?[0-9a-fA-F]{64}$/.test(text);
}

function setFormValue(formID, name, value) {
  const field = $(formID)?.elements.namedItem(name);
  if (field) field.value = String(value ?? "");
}

function openAdminModule(formID) {
  const details = $(formID)?.closest("details");
  if (details) details.open = true;
}

async function loadVaultDeploymentRecord(event) {
  const input = event.currentTarget;
  const file = input.files?.[0];
  if (!file) return;
  try {
    if (file.size <= 0 || file.size > MAX_DEPLOYMENT_RECORD_BYTES) {
      throw new Error("Vault deployment record must be between 1 byte and 2 MB.");
    }
    const record = JSON.parse(await file.text());
    const chain = record?.network;
    const contract = record?.contract;
    const route = record?.route;
    const recordVersion = String(record?.version || "");
		const chainType = recordVersion === "msc-bridge-evm-deployment-v1"
			? "evm"
			: recordVersion === "msc-bridge-tron-deployment-v2" ? "tron" : "";
		const executionAdapter = chainType === "evm" ? "evm_vault_v1" : "tron_vault_v1";
    if (!chainType || !chain || !contract || !route) {
      throw new Error("Unsupported or incomplete vault deployment record.");
    }
		const runtimeCodeHash = String(contract.runtime_code_hash || "");
		const deploymentTxHash = String(contract.deployment_tx_hash || "");
		const committeeThreshold = Number(contract.committee_threshold || 0);
		const committeeMembers = Array.isArray(contract.committee_members) ? contract.committee_members : [];
		const chainID = String(chain.chain_id || "");
		const validChainID = /^[a-z0-9][a-z0-9._:-]{0,95}$/i.test(chainID);
		const validRuntimeHash = /^0x[0-9a-fA-F]{64}$/.test(runtimeCodeHash) && !/^0x0{64}$/i.test(runtimeCodeHash);
		const validCommittee = Number.isInteger(committeeThreshold)
			&& committeeMembers.length >= 3
			&& committeeMembers.length <= 32
			&& committeeThreshold >= 2
			&& committeeThreshold <= committeeMembers.length
			&& committeeThreshold >= Math.ceil((committeeMembers.length * 2) / 3);
    if (
      contract.paused !== true
      || !validChainID
      || !validRuntimeHash
      || !validCommittee
      || String(route.execution_adapter || executionAdapter) !== executionAdapter
      || !Array.isArray(record.governance_actions)
      || record.governance_actions.length !== 2
    ) {
      throw new Error("Vault record failed fail-closed deployment validation.");
    }
		if (chainType === "evm") {
			if (
				!/^0x[0-9a-fA-F]{40}$/.test(String(contract.address || ""))
				|| !/^0x[0-9a-fA-F]{40}$/.test(String(route.token_address || ""))
				|| !/^0x[0-9a-fA-F]{64}$/.test(deploymentTxHash)
				|| /^0x0{64}$/i.test(deploymentTxHash)
				|| !/^[1-9][0-9]*$/.test(chainID)
			) throw new Error("EVM vault record failed address or transaction validation.");
		} else {
			const genesisBlockID = String(chain.genesis_block_id || "").replace(/^0x/i, "");
			const tip712ChainID = String(chain.tip712_chain_id || "").toLowerCase();
			const tokenRuntimeCodeHash = String(route.token_runtime_code_hash || "");
			const roleAddresses = [
				contract.address,
				contract.governance,
				contract.guardian,
				contract.release_executor,
				route.token_address,
				...committeeMembers,
			];
			const validAddresses = await Promise.all(roleAddresses.map(validTronBase58CheckAddress));
			const separatedRoles = new Set(roleAddresses.map(String)).size === roleAddresses.length;
			if (
				validAddresses.some((valid) => !valid)
				|| !separatedRoles
				|| record.testnet !== false
				|| chainID !== "tron-mainnet"
				|| String(chain.label || "") !== "tron-mainnet"
				|| String(chain.explorer_url || "").replace(/\/$/, "") !== "https://tronscan.org"
				|| Number(chain.min_confirmations || 0) < 64
				|| !/^(?:0x)?[0-9a-fA-F]{64}$/.test(deploymentTxHash)
				|| /^(?:0x)?0{64}$/i.test(deploymentTxHash)
				|| genesisBlockID.toLowerCase() !== TRON_MAINNET_GENESIS_BLOCK_ID
				|| tip712ChainID !== "0x2b6653dc"
				|| contract.tip712 !== true
				|| String(contract.tvm_target || "").toLowerCase() !== "cancun"
				|| String(contract.msc_source_chain_id || "") !== "91938"
				|| Number(contract.default_admin_delay_seconds || 0) < 86400
				|| committeeMembers.length < 5
				|| committeeThreshold < 4
				|| String(route.route_id || "") !== "usdt-tron-mainnet"
				|| String(route.asset_denom || "") !== "USDT-TRON"
				|| String(route.symbol || "") !== "USDT"
				|| String(route.token_address || "") !== TRON_MAINNET_USDT
				|| String(route.local_denom || "") !== "mscUSDT"
				|| Number(route.decimals) !== 6
				|| String(route.token_symbol_verified || "") !== "USDT"
				|| Number(route.token_decimals_verified) !== 6
				|| !/^0x[0-9a-fA-F]{64}$/.test(tokenRuntimeCodeHash)
				|| /^0x0{64}$/i.test(tokenRuntimeCodeHash)
				|| !validHTTPSURL(route.audit_reference)
				|| record.governance_actions.some((action) => action?.endpoint !== "/wallet/triggersmartcontract")
			) throw new Error("TRON vault record failed TIP-712, role, or network validation.");
		}

    setFormValue("bridgeChainForm", "chain_id", chain.chain_id);
    setFormValue("bridgeChainForm", "name", chain.chain_name);
    setFormValue("bridgeChainForm", "native_symbol", chain.native_symbol);
    setFormValue("bridgeChainForm", "chain_type", chainType);
    setFormValue("bridgeChainForm", "trust_model", chainType === "tron" ? "hybrid" : "oracle_quorum");
    setFormValue("bridgeChainForm", "min_confirmations", chain.min_confirmations);
    setFormValue("bridgeChainForm", "status", "testing");
    setFormValue("bridgeChainForm", "finality_status", "unknown");
    setFormValue("bridgeChainForm", "light_client", chainType === "tron" ? "tron-solidified-checkpoint-v2" : "federated-checkpoint-v2");
    setFormValue("bridgeChainForm", "latest_observed_height", 0);
    setFormValue("bridgeChainForm", "latest_finalized_height", 0);
    setFormValue("bridgeChainForm", "explorer_url", chain.explorer_url);

    setFormValue("bridgeAssetForm", "denom", route.asset_denom);
    setFormValue("bridgeAssetForm", "symbol", route.symbol);
    setFormValue("bridgeAssetForm", "origin_chain", chain.chain_id);
    setFormValue("bridgeAssetForm", "origin_asset", route.token_address);
    setFormValue("bridgeAssetForm", "local_denom", route.local_denom);
    setFormValue("bridgeAssetForm", "decimals", route.decimals);
    setFormValue("bridgeAssetForm", "status", "testing");
    setFormValue("bridgeAssetForm", "min_deposit", route.min_amount);
    setFormValue("bridgeAssetForm", "daily_limit", route.daily_lock_limit);
    setFormValue("bridgeAssetForm", "escrow_policy", "lock_and_mint");

    setFormValue("bridgeContractForm", "route_id", route.route_id);
    setFormValue("bridgeContractForm", "chain_id", chain.chain_id);
    setFormValue("bridgeContractForm", "asset_denom", route.asset_denom);
    setFormValue("bridgeContractForm", "contract_address", contract.address);
    setFormValue("bridgeContractForm", "deposit_address", contract.address);
    setFormValue("bridgeContractForm", "deposit_mode", "contract_call");
		setFormValue("bridgeContractForm", "execution_adapter", executionAdapter);
    setFormValue("bridgeContractForm", "status", "testing");
    setFormValue("bridgeContractForm", "finality_status", "unknown");
    setFormValue("bridgeContractForm", "min_deposit", route.min_amount);
    setFormValue("bridgeContractForm", "daily_limit", route.daily_lock_limit);
    setFormValue("bridgeContractForm", "audit_reference", route.audit_reference);
    setFormValue("bridgeContractForm", "deployment_tx_hash", contract.deployment_tx_hash);
		setFormValue("bridgeContractForm", "runtime_code_hash", contract.runtime_code_hash);

    openAdminModule("bridgeChainForm");
    openAdminModule("bridgeAssetForm");
    openAdminModule("bridgeContractForm");
    setResult(
      `Loaded paused ${chainType.toUpperCase()} vault ${contract.address} for ${route.asset_denom} on chain ${chain.chain_id}. Review all three forms; nothing was submitted.`,
      "success",
    );
  } catch (error) {
    setResult(error.message || "Vault deployment import failed", "error");
  } finally {
    input.value = "";
  }
}

$("bridgeAdminConnectForm")?.addEventListener("submit", connectAdmin);
$("refreshBridgeAdmin")?.addEventListener("click", () => refreshAdminState().then(() => setResult("Admin state refreshed.", "success")).catch((error) => setResult(error.message, "error")));
$("bridgeAdminTransfers")?.addEventListener("click", (event) => {
  const button = event.target.closest("[data-use-transfer]");
  if (!button) return;
  const transferID = button.dataset.useTransfer || "";
  document.querySelectorAll('[name="transfer_id"]').forEach((input) => { input.value = transferID; });
  setResult(`Transfer ${transferID} loaded into operation forms.`, "success");
});
$("bridgeObserverArtifactUpload")?.addEventListener("change", loadObserverArtifact);
$("bridgeVaultDeploymentUpload")?.addEventListener("change", loadVaultDeploymentRecord);

bindAdminForm("bridgeSettingsForm", "/bridge/admin/settings", (data) => ({ paused: data.paused === "true", pause_reason: data.pause_reason.trim() }));
bindAdminForm("bridgeChainForm", "/bridge/admin/chains", (data) => ({
  ...data,
  min_confirmations: numberOrZero(data.min_confirmations),
  latest_observed_height: numberOrZero(data.latest_observed_height),
  latest_finalized_height: numberOrZero(data.latest_finalized_height),
}));
bindAdminForm("bridgeAssetForm", "/bridge/admin/assets", (data) => ({ ...data, decimals: numberOrZero(data.decimals) }));
bindAdminForm("bridgeContractForm", "/bridge/admin/contracts", (data) => data);
bindAdminForm("bridgeValidatorForm", "/bridge/admin/validators", (data) => ({ ...data, weight: numberOrZero(data.weight) }));
bindAdminForm("bridgeCheckpointForm", "/bridge/checkpoints", (data) => JSON.parse(data.checkpoint));
bindAdminForm("bridgeEventForm", "/bridge/admin/events", (data) => ({ intent_id: data.intent_id.trim(), proof: JSON.parse(data.proof) }));
bindAdminForm("bridgeMintForm", "/bridge/admin/mints", (data) => ({
  transfer_id: data.transfer_id.trim(),
  proposer: data.proposer.trim(),
  governance_cert: JSON.parse(data.governance_cert),
}));
bindAdminForm("bridgeMintConfirmForm", "/bridge/admin/mints/confirm", (data) => ({
  transfer_id: data.transfer_id.trim(),
  msc_transaction_id: data.msc_transaction_id.trim(),
}));
bindAdminForm("bridgeBurnConfirmForm", "/bridge/admin/burns/confirm", (data) => ({
  transfer_id: data.transfer_id.trim(),
  msc_transaction_id: data.msc_transaction_id.trim(),
}));
bindAdminForm("bridgeUnlockAuthorizeForm", "/bridge/admin/unlocks/authorize", (data) => ({
  transfer_id: data.transfer_id.trim(),
  signatures: JSON.parse(data.signatures),
}));
bindAdminForm("bridgeUnlockConfirmForm", "/bridge/admin/unlocks/confirm", (data) => ({
  transfer_id: data.transfer_id.trim(),
  proof: JSON.parse(data.proof),
}));

const savedRPC = sessionStorage.getItem(BRIDGE_ADMIN_RPC_KEY) || (/^https?:$/.test(window.location.protocol) ? window.location.origin : "");
const savedToken = sessionStorage.getItem(BRIDGE_ADMIN_TOKEN_KEY) || "";
$("bridgeAdminRPC").value = savedRPC;
$("bridgeAdminToken").value = savedToken;
if (window.lucide) window.lucide.createIcons();
if (savedRPC && savedToken) connectAdmin();
