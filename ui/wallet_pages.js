const enc = new TextEncoder();
const STORAGE_KEY = "msc_wallet_browser_v1";
const CHAIN_ID = "91938";
const DEFAULT_STAKE_EPOCHS = 19872000;
const AES_ITERATIONS = 150000;

const $ = (id) => document.getElementById(id);
const page = document.body.dataset.page || "dashboard";

const state = {
  rpc: normalizeRPC(localStorage.getItem("msc_rpc") || window.location.origin),
  wallet: null,
  secretKey: null,
  status: null,
  cmd: null,
};

function normalizeRPC(raw) {
  let value = String(raw || "").trim();
  if (!value) value = window.location.origin;
  if (!/^[a-zA-Z][a-zA-Z\d+\-.]*:\/\//.test(value)) value = `https://${value}`;
  return value.replace(/\/+$/, "");
}

function bytesToHex(bytes) {
  return Array.from(bytes).map((b) => b.toString(16).padStart(2, "0")).join("");
}

function hexToBytes(hex) {
  const clean = String(hex || "").trim().replace(/^0x/i, "");
  if (!clean || clean.length % 2 !== 0 || !/^[0-9a-fA-F]+$/.test(clean)) return new Uint8Array();
  const out = new Uint8Array(clean.length / 2);
  for (let i = 0; i < out.length; i += 1) out[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16);
  return out;
}

function concatBytes(parts) {
  const total = parts.reduce((sum, item) => sum + item.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const item of parts) {
    out.set(item, offset);
    offset += item.length;
  }
  return out;
}

async function sha256(bytes) {
  if (crypto?.subtle) {
    return new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  }
  if (window.MSC_CRYPTO_FALLBACK?.sha256) return window.MSC_CRYPTO_FALLBACK.sha256(bytes);
  throw new Error("SHA-256 unavailable. Open over HTTPS or localhost.");
}

async function addressFromPublicKey(pubKey) {
  const prefix = enc.encode(`MSC-ADDR|${CHAIN_ID}|`);
  const h1 = await sha256(concatBytes([prefix, pubKey]));
  const h2 = await sha256(h1);
  const payload = new Uint8Array(21);
  payload[0] = 0x01;
  payload.set(h2.slice(0, 20), 1);
  return `MSC${bytesToHex(payload)}`;
}

async function deriveAesKey(password, salt, iterations = AES_ITERATIONS) {
  if (!crypto?.subtle) throw new Error("WebCrypto required for encrypted wallet storage");
  const material = await crypto.subtle.importKey("raw", enc.encode(password), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", salt, iterations, hash: "SHA-256" },
    material,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

async function encryptSecretKey(secretKey, password) {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await deriveAesKey(password, salt);
  const cipher = await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, secretKey);
  return {
    cipher: "aes-256-gcm",
    kdf: "pbkdf2-sha256",
    ciphertext: bytesToHex(new Uint8Array(cipher)),
    iv: bytesToHex(iv),
    salt: bytesToHex(salt),
    iterations: AES_ITERATIONS,
  };
}

async function decryptSecretKey(cryptoData, password) {
  const salt = hexToBytes(cryptoData.salt);
  const iv = hexToBytes(cryptoData.iv);
  const ciphertext = hexToBytes(cryptoData.ciphertext);
  const key = await deriveAesKey(password, salt, cryptoData.iterations || AES_ITERATIONS);
  return new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ciphertext));
}

function loadWallet() {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) || "null");
  } catch (_) {
    return null;
  }
}

function saveWallet(wallet) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(wallet));
  state.wallet = wallet;
}

function shortAddress(value) {
  const raw = String(value || "");
  return raw.length > 14 ? `${raw.slice(0, 8)}...${raw.slice(-6)}` : raw || "-";
}

function setText(id, value) {
  const node = $(id);
  if (node) node.textContent = value ?? "-";
}

function setValue(id, value) {
  const node = $(id);
  if (node) node.value = value ?? "";
}

function setStatus(id, text, tone = "") {
  const node = $(id);
  if (!node) return;
  node.textContent = text;
  node.classList.toggle("success", tone === "success");
  node.classList.toggle("error", tone === "error");
}

function formatNumber(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return value === 0 ? "0" : "-";
  return n.toLocaleString();
}

function stripHTML(value) {
  return String(value || "").replace(/<!--[\s\S]*?-->/g, " ").replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

async function api(path, options = {}) {
  const res = await fetch(`${state.rpc}${path}`, {
    method: options.method || "GET",
    headers: options.body ? { "Content-Type": "application/json" } : undefined,
    body: options.body ? JSON.stringify(options.body) : undefined,
  });
  const text = await res.text();
  let data = text;
  try {
    data = text ? JSON.parse(text) : null;
  } catch (_) {
    data = stripHTML(text);
  }
  if (!res.ok) {
    const err = new Error(typeof data === "string" ? data : data?.error || data?.message || res.statusText);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

async function refreshNetwork() {
  try {
    const status = await api("/status");
    state.status = status;
    const best = status.best || status;
    const height = best.height || status.height || status.chain_height || best.finalized_height || "-";
    const finalized = best.finalized_height || status.finalized_height || status.finalized || "-";
    setText("topHeight", formatNumber(height));
    setText("networkStatus", status.health || status.network_health || "connected");
    setText("blockHeight", formatNumber(height));
    setText("finalizedHeight", formatNumber(finalized));
    setText("latestBlocks", `height ${formatNumber(height)} | finalized ${formatNumber(finalized)}`);
    setText("txBlockHeight", formatNumber(height));
    setStatus("networkPill", "Mainnet", "success");
  } catch (err) {
    setStatus("networkPill", "RPC error", "error");
    setText("networkStatus", err.message || "unavailable");
  }

  try {
    const cmd = await api("/consensus/mode");
    state.cmd = cmd;
    const mode = cmd.mode || "UNKNOWN";
    setText("topCmd", mode);
    setText("cmdStatus", mode);
    setText("validatorStatus", `${cmd.active_validators ?? "-"} / ${cmd.total_validators ?? "-"} active`);
    setText("validatorCMD", mode);
  } catch (_) {
    setText("topCmd", "-");
  }
}

async function refreshBalance() {
  if (!state.wallet?.address) return;
  setText("topWallet", shortAddress(state.wallet.address));
  setText("walletAddress", state.wallet.address);
  setText("walletPublicKey", state.wallet.publicKey || "-");
  setText("receiveAddress", state.wallet.address);
  setValue("sendFrom", state.wallet.address);
  try {
    const bal = await api(`/balance?address=${encodeURIComponent(state.wallet.address)}&coin=MSC&state=finalized`);
    const amount = bal.balance ?? bal.Balance ?? "-";
    setText("totalBalance", `${formatNumber(amount)} MSC`);
    setText("walletBalance", `${formatNumber(amount)} MSC`);
    setText("assetMSC", `${formatNumber(amount)} MSC`);
  } catch (err) {
    setText("walletBalance", "balance unavailable");
  }
  try {
    const ws = await api(`/wallet/status?address=${encodeURIComponent(state.wallet.address)}`);
    setText("stakedBalance", `${formatNumber(ws.stake || 0)} MSC`);
    setText("rewardBalance", `${formatNumber(ws.rewards || 0)} MSC`);
    setText("delegations", ws.validator_id ? `${ws.validator_id}: ${formatNumber(ws.stake || 0)} MSC` : "No active delegation");
  } catch (_) {
    setText("delegations", "No staking status yet");
  }
}

async function refreshTransactions() {
  if (!state.wallet?.address) return;
  const list = $("transactionsList") || $("latestTx");
  if (!list) return;
  try {
    const data = await api(`/txs?address=${encodeURIComponent(state.wallet.address)}`);
    const txs = data.txs || data.transactions || data.items || [];
    list.innerHTML = "";
    if (!txs.length) {
      list.innerHTML = `<div class="list-item">No transactions yet</div>`;
      setText("latestTx", "No transactions yet");
      return;
    }
    txs.slice(0, 10).forEach((tx) => {
      const item = document.createElement("div");
      item.className = "list-item";
      item.innerHTML = `<strong>${tx.id || tx.tx_id || "tx"}</strong><span class="mono">${tx.from || "-"} -> ${tx.to || "-"}</span><span>${formatNumber(tx.amount || 0)} ${tx.coin || "MSC"} | fee ${tx.fee || "-"}</span>`;
      list.appendChild(item);
    });
    setText("latestTx", `${txs.length} transaction(s) loaded`);
  } catch (err) {
    list.innerHTML = `<div class="list-item">${err.message || "Transaction sync failed"}</div>`;
  }
}

async function refreshValidators() {
  const list = $("validatorList");
  if (!list) return;
  try {
    const data = await api("/v1/validators");
    const vals = data.validators || data.active || data.items || [];
    list.innerHTML = "";
    if (!vals.length) {
      list.innerHTML = `<div class="list-item">Validator list unavailable</div>`;
      return;
    }
    vals.forEach((v) => {
      const id = v.id || v.validator || v.name || "-";
      const item = document.createElement("div");
      item.className = "list-item";
      item.innerHTML = `<strong>${id}</strong><span>Status: ${v.status || (v.active ? "active" : "unknown")}</span><span>Uptime: ${v.uptime ?? "-"} | Voting power: ${v.voting_power ?? v.power ?? "-"}</span>`;
      list.appendChild(item);
    });
  } catch (err) {
    list.innerHTML = `<div class="list-item">${err.message || "Validator sync failed"}</div>`;
  }
}

async function refreshGovernance() {
  const list = $("proposalList");
  if (!list) return;
  try {
    const data = await api("/governance/status");
    const proposals = data.proposals || data.active_proposals || [];
    list.innerHTML = "";
    if (!proposals.length) {
      list.innerHTML = `<div class="list-item">No active proposals</div>`;
      return;
    }
    proposals.forEach((p) => {
      const item = document.createElement("div");
      item.className = "list-item";
      item.innerHTML = `<strong>Proposal ${p.id ?? "-"}</strong><span>${p.title || p.kind || "Protocol proposal"}</span><span>YES ${p.yes ?? 0}% | NO ${p.no ?? 0}% | ABSTAIN ${p.abstain ?? 0}%</span>`;
      list.appendChild(item);
    });
  } catch (err) {
    list.innerHTML = `<div class="list-item">${err.message || "Governance unavailable"}</div>`;
  }
}

async function refreshBridge() {
  if (!$("bridgeStatus")) return;
  try {
    const data = await api("/bridge/status");
    setStatus("bridgeStatus", data.enabled ? "Enabled" : "Verification Only", data.enabled ? "success" : "");
    setText("bridgeMode", data.mode || "disabled");
    setText("bridgeChains", formatNumber(data.registered_chains || 0));
    setText("bridgeAssets", formatNumber(data.registered_assets || 0));
    setText("bridgeFuture", data.light_client_required === false ? "Asset transfer allowed" : "Light-client verified transfer pending");
  } catch (err) {
    setStatus("bridgeStatus", "Unavailable", "error");
    setText("bridgeFuture", err.message || "Bridge status failed");
  }
}

async function verifyBridgeProof() {
  const raw = $("bridgeProof")?.value.trim();
  if (!raw) return setText("bridgeResult", "Paste proof JSON first.");
  try {
    const body = JSON.parse(raw);
    const data = await api("/bridge/verify", { method: "POST", body });
    setText("bridgeResult", JSON.stringify(data, null, 2));
  } catch (err) {
    setText("bridgeResult", typeof err.data === "object" ? JSON.stringify(err.data, null, 2) : err.message);
  }
}

function pushString(parts, value) {
  parts.push(enc.encode(String(value || "")));
  parts.push(new Uint8Array([0]));
}

function pushInt64(parts, value) {
  const buf = new ArrayBuffer(8);
  new DataView(buf).setBigInt64(0, BigInt(value || 0), false);
  parts.push(new Uint8Array(buf));
}

function buildTxPayload(tx) {
  const parts = [];
  const type = Number(tx.type || tx.Type || 0);
  pushString(parts, tx.from);
  pushString(parts, tx.to);
  pushString(parts, tx.coin || "MSC");
  pushInt64(parts, tx.amount);
  pushInt64(parts, tx.fee);
  pushInt64(parts, tx.nonce);
  pushInt64(parts, tx.expiry);
  pushInt64(parts, tx.stake_epochs || 0);
  if (type === 2 && tx.validator_pubkey) pushString(parts, tx.validator_pubkey);
  pushInt64(parts, tx.evm_gas_limit || 0);
  pushString(parts, "");
  pushString(parts, "");
  pushString(parts, "");
  pushString(parts, "");
  if (type === 8) {
    pushString(parts, tx.dtl_tx_type || "");
    pushString(parts, tx.dtl_token_id || "");
    pushString(parts, tx.dtl_payload || "");
    pushString(parts, tx.dtl_governance_cert || "");
  }
  pushString(parts, CHAIN_ID);
  parts.push(new Uint8Array([type & 0xff]));
  return concatBytes(parts);
}

async function signTx(tx) {
  if (!state.secretKey) throw new Error("Unlock wallet first");
  const payload = buildTxPayload(tx);
  const sig = nacl.sign.detached(payload, state.secretKey);
  const id = await sha256(payload);
  return { ...tx, signature: bytesToHex(sig), id: bytesToHex(id), ChainID: CHAIN_ID, Coin: tx.coin || "MSC", Type: tx.type || 0 };
}

async function nextNonce() {
  const data = await api(`/nonce/pending?address=${encodeURIComponent(state.wallet.address)}`);
  return Number(data.nonce || 1);
}

function computeFee(amount) {
  return Math.max(1, Math.floor((Number(amount || 0) * 20) / 10000));
}

async function submitSignedTx(tx) {
  const signed = await signTx(tx);
  return api("/submitTx", { method: "POST", body: signed });
}

async function handleSend(event) {
  event.preventDefault();
  try {
    const amount = Number($("sendAmount").value || 0);
    const tx = {
      from: state.wallet.address,
      to: $("sendTo").value.trim(),
      amount,
      nonce: await nextNonce(),
      publicKey: state.wallet.publicKey,
      fee: computeFee(amount),
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 0,
      coin: $("sendCoin")?.value.trim() || "MSC",
    };
    await submitSignedTx(tx);
    setStatus("sendStatus", "Transaction submitted", "success");
    refreshBalance();
    refreshTransactions();
  } catch (err) {
    setStatus("sendStatus", err.message || "Send failed", "error");
  }
}

async function handleStake(event) {
  event.preventDefault();
  try {
    const amount = Number($("stakeAmount").value || 0);
    const tx = {
      from: state.wallet.address,
      to: $("stakeValidator").value.trim(),
      amount,
      nonce: await nextNonce(),
      publicKey: state.wallet.publicKey,
      fee: computeFee(amount),
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 2,
      stake_epochs: Number($("stakeEpochs")?.value || DEFAULT_STAKE_EPOCHS),
      coin: "MSC",
      validator_pubkey: $("stakePubkey")?.value.trim() || "",
    };
    await submitSignedTx(tx);
    setStatus("stakeStatus", "Stake submitted", "success");
    refreshBalance();
  } catch (err) {
    setStatus("stakeStatus", err.message || "Stake failed", "error");
  }
}

async function handleUnstake(event) {
  event.preventDefault();
  try {
    const amount = Number($("unstakeAmount").value || 0);
    const tx = {
      from: state.wallet.address,
      to: $("unstakeValidator").value.trim(),
      amount,
      nonce: await nextNonce(),
      publicKey: state.wallet.publicKey,
      fee: computeFee(amount),
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 6,
      coin: "MSC",
    };
    await submitSignedTx(tx);
    setStatus("unstakeStatus", "Unstake submitted", "success");
    refreshBalance();
  } catch (err) {
    setStatus("unstakeStatus", err.message || "Unstake failed", "error");
  }
}

async function createWallet(event) {
  event.preventDefault();
  try {
    if (!window.nacl?.sign?.keyPair) throw new Error("nacl signer unavailable");
    const password = $("createPassword").value.trim();
    if (!password) throw new Error("Password required");
    const kp = nacl.sign.keyPair();
    const wallet = {
      address: await addressFromPublicKey(kp.publicKey),
      publicKey: bytesToHex(kp.publicKey),
      crypto: await encryptSecretKey(kp.secretKey, password),
    };
    saveWallet(wallet);
    state.secretKey = kp.secretKey;
    setText("createResult", `Wallet created\n${wallet.address}\nPublic key: ${wallet.publicKey}`);
    updateWalletUI();
  } catch (err) {
    setText("createResult", err.message || "Wallet create failed");
  }
}

async function unlockWallet(event) {
  event.preventDefault();
  try {
    const password = $("unlockPassword").value.trim();
    if (!state.wallet) throw new Error("No wallet saved");
    state.secretKey = await decryptSecretKey(state.wallet.crypto, password);
    setStatus("loginStatus", "Unlocked", "success");
    updateWalletUI();
  } catch (err) {
    setStatus("loginStatus", "Unlock failed", "error");
  }
}

async function importPrivateKey(event) {
  event.preventDefault();
  try {
    const raw = $("importPrivateKey").value.trim();
    const password = $("importPassword").value.trim();
    const secretKey = hexToBytes(raw);
    if (secretKey.length !== 64) throw new Error("Private key must be 64-byte hex");
    const publicKey = secretKey.slice(32);
    const wallet = {
      address: await addressFromPublicKey(publicKey),
      publicKey: bytesToHex(publicKey),
      crypto: await encryptSecretKey(secretKey, password),
    };
    saveWallet(wallet);
    state.secretKey = secretKey;
    setStatus("loginStatus", "Imported", "success");
    updateWalletUI();
  } catch (err) {
    setStatus("loginStatus", err.message || "Import failed", "error");
  }
}

function exportPrivateKey() {
  setText("privateKeyOutput", state.secretKey ? bytesToHex(state.secretKey) : "Unlock wallet first.");
}

function updateWalletUI() {
  state.wallet = state.wallet || loadWallet();
  setText("topWallet", state.wallet ? shortAddress(state.wallet.address) : "No wallet");
  setText("walletAddress", state.wallet?.address || "-");
  setText("walletPublicKey", state.wallet?.publicKey || "-");
  setText("receiveAddress", state.wallet?.address || "-");
  setText("securityEncryption", state.wallet ? "AES-GCM encrypted" : "No wallet");
  setText("securityBackup", state.wallet ? "Export backup offline" : "Create/import required");
  setText("securityMPC", "Validator-side optional");
  setText("securityHSM", "External signer optional");
  setText("securitySession", state.secretKey ? "Unlocked" : "Locked");
  if (state.wallet?.address) renderQR("receiveQr", state.wallet.address);
}

function renderQR(id, text) {
  const box = $(id);
  if (!box) return;
  box.innerHTML = "";
  if (window.QRCode) {
    new QRCode(box, { text, width: 180, height: 180 });
  } else {
    box.textContent = text;
  }
}

async function copyText(value) {
  await navigator.clipboard.writeText(String(value || ""));
}

function installShell() {
  if (document.querySelector(".app-shell")) return;
  const content = document.querySelector(".content") || document.createElement("main");
  content.classList.add("content");
  const shell = document.createElement("div");
  shell.className = "app-shell";
  shell.innerHTML = `
    <aside class="sidebar">
      <div class="brand">
        <div class="logo">MSC</div>
        <div>
          <div class="title">MSC Wallet</div>
          <div class="subtitle">Mainnet vault</div>
        </div>
      </div>
      <nav class="nav" aria-label="Wallet navigation">
        <a href="dashboard.html" data-page="dashboard">Dashboard</a>
        <a href="wallet.html" data-page="wallet">Wallet</a>
        <a href="send.html" data-page="send">Send</a>
        <a href="receive.html" data-page="receive">Receive</a>
        <a href="transactions.html" data-page="transactions">Transactions</a>
        <a href="staking.html" data-page="staking">Staking</a>
        <a href="validators.html" data-page="validators">Validators</a>
        <a href="governance.html" data-page="governance">Governance</a>
        <a href="bridge.html" data-page="bridge">Bridge</a>
        <a href="security.html" data-page="security">Security</a>
        <a href="settings.html" data-page="settings">Settings</a>
      </nav>
    </aside>
    <div class="main">
      <header class="topbar">
        <div class="topline">
          <div class="brand">
            <div class="logo">MSC</div>
            <div>
              <div class="title">Wallet 3.0</div>
              <div class="subtitle">Mainnet asset control center</div>
            </div>
          </div>
          <form id="quickSearch" class="search">
            <input id="quickSearchInput" type="search" placeholder="Search tx / address / block" />
            <button class="primary" type="submit">Search</button>
          </form>
        </div>
        <div class="status-row">
          <span id="networkPill" class="pill">Mainnet</span>
          <span class="pill">Height <strong id="topHeight">-</strong></span>
          <span class="pill">CMD <strong id="topCmd">-</strong></span>
          <span class="pill">Wallet <strong id="topWallet">No wallet</strong></span>
        </div>
      </header>
    </div>`;
  document.body.appendChild(shell);
  shell.querySelector(".main").appendChild(content);
}

function bindEvents() {
  document.querySelectorAll(".nav a").forEach((link) => {
    const active = link.dataset.page === page;
    link.classList.toggle("active", active);
  });
  $("quickSearch")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const q = $("quickSearchInput").value.trim();
    if (q) window.location.href = `explorer.html?q=${encodeURIComponent(q)}`;
  });
  $("settingsForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    state.rpc = normalizeRPC($("settingsRpc").value);
    localStorage.setItem("msc_rpc", state.rpc);
    document.body.dataset.theme = $("settingsTheme").value;
    localStorage.setItem("msc_wallet_theme", $("settingsTheme").value);
    refreshAll();
  });
  $("createWalletForm")?.addEventListener("submit", createWallet);
  $("unlockForm")?.addEventListener("submit", unlockWallet);
  $("importKeyForm")?.addEventListener("submit", importPrivateKey);
  $("sendForm")?.addEventListener("submit", handleSend);
  $("stakeForm")?.addEventListener("submit", handleStake);
  $("unstakeForm")?.addEventListener("submit", handleUnstake);
  $("exportPrivateKey")?.addEventListener("click", exportPrivateKey);
  $("copyAddress")?.addEventListener("click", () => copyText(state.wallet?.address || ""));
  $("copyReceiveAddress")?.addEventListener("click", () => copyText(state.wallet?.address || ""));
  $("shareReceive")?.addEventListener("click", async () => {
    const text = state.wallet?.address || "";
    if (navigator.share) await navigator.share({ title: "MSC receive address", text });
    else await copyText(text);
  });
  $("refreshBridge")?.addEventListener("click", refreshBridge);
  $("verifyBridgeProof")?.addEventListener("click", verifyBridgeProof);
  $("claimRewards")?.addEventListener("click", () => setStatus("claimStatus", "Claim endpoint pending", ""));
  $("sendAmount")?.addEventListener("input", () => {
    const amount = Number($("sendAmount").value || 0);
    setText("sendFee", `${computeFee(amount)} MSC`);
    setText("sendTotal", `${amount + computeFee(amount)} MSC`);
  });
}

async function refreshAll() {
  setValue("settingsRpc", state.rpc);
  await refreshNetwork();
  updateWalletUI();
  await refreshBalance();
  await refreshTransactions();
  await refreshValidators();
  await refreshGovernance();
  await refreshBridge();
}

function initTheme() {
  const theme = localStorage.getItem("msc_wallet_theme") || "dark";
  document.body.dataset.theme = theme;
  setValue("settingsTheme", theme);
}

installShell();
bindEvents();
initTheme();
refreshAll();
window.setInterval(refreshAll, 5000);
