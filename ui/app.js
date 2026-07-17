const enc = new TextEncoder();
const dec = new TextDecoder();

const STORAGE_KEY = "msc_wallet_v1";
const DEFAULT_STAKE_EPOCHS = 19872000;
const DEFAULT_FEE_POLICY = Object.freeze({
  min_bps: 20,
  max_bps: 300,
  floor_amount: 200,
  ceil_amount: 100000,
});

const normalizeAuthToken = (raw) => {
  if (!raw) return "";
  let token = String(raw).trim();
  if (!token) return "";
  if (token.startsWith("Bearer ")) {
    token = token.slice(7).trim();
  }
  token = token.replace(/^[<"'`]+|[>"'`]+$/g, "").trim();
  return token;
};

const preferHttpsForLocalRpc = (rpc) => {
  const raw = String(rpc || "").trim();
  if (!raw) return raw;
  if (window.location.protocol !== "https:") return raw;
  if (/^http:\/\/(127\.0\.0\.1|localhost)(:\d+)?(\/|$)/i.test(raw)) {
    return raw.replace(/^http:\/\//i, "https://");
  }
  return raw;
};

const state = {
  rpcUrl: preferHttpsForLocalRpc(localStorage.getItem("msc_rpc") || window.location.origin),
  chainId: localStorage.getItem("msc_chain") || "91938",
  apiToken: normalizeAuthToken(localStorage.getItem("msc_token") || ""),
  feePolicy: { ...DEFAULT_FEE_POLICY },
  wallet: null,
  secretKey: null,
};

const el = (id) => document.getElementById(id);

const connectionStatus = el("connectionStatus");
const walletStatus = el("walletStatus");
const faucetStatus = el("faucetStatus");
const balanceStatus = el("balanceStatus");
const sendStatus = el("sendStatus");
const stakeStatus = el("stakeStatus");
const unstakeStatus = el("unstakeStatus");

const setStatus = (element, message, tone = "info") => {
  element.textContent = message;
  element.dataset.tone = tone;
};

const hexToBytes = (hex) => {
  if (!hex) return new Uint8Array();
  const clean = hex.trim();
  const bytes = new Uint8Array(clean.length / 2);
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(clean.substr(i * 2, 2), 16);
  }
  return bytes;
};

const normalizeValidatorPubKeyHex = (value) => {
  const clean = String(value || "").trim().replace(/^0x/i, "").toLowerCase();
  if (!clean) return "";
  if (!/^[0-9a-f]{64}$/.test(clean)) {
    throw new Error("Validator consensus pubkey must be 32-byte hex");
  }
  return clean;
};

const bytesToHex = (bytes) =>
  Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

const concatBytes = (parts) => {
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
};

const sha256 = async (bytes) => {
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return new Uint8Array(hash);
};

const HD_SCHEME = "bip39-slip10-ed25519";
const HD_PURPOSE = 44;
const HD_DEFAULT_COIN_TYPE = 91938;
const HD_DEFAULT_ACCOUNT = 0;
const HD_DEFAULT_CHANGE = 0;
const HD_DEFAULT_INDEX = 0;
const HD_MAX_NON_HARDENED = 0x7fffffff;
const HD_HARDENED_OFFSET = 0x80000000;

const ser32BE = (value) => {
  const v = value >>> 0;
  return new Uint8Array([
    (v >>> 24) & 0xff,
    (v >>> 16) & 0xff,
    (v >>> 8) & 0xff,
    v & 0xff,
  ]);
};

const hmacSha512 = async (keyBytes, dataBytes) => {
  const key = await crypto.subtle.importKey(
    "raw",
    keyBytes,
    { name: "HMAC", hash: "SHA-512" },
    false,
    ["sign"],
  );
  const mac = await crypto.subtle.sign("HMAC", key, dataBytes);
  return new Uint8Array(mac);
};

const hdCoinTypeFromChainId = (chainId) => {
  const parsed = Number.parseInt(String(chainId || "").trim(), 10);
  if (Number.isInteger(parsed) && parsed >= 0 && parsed <= HD_MAX_NON_HARDENED) {
    return parsed;
  }
  return HD_DEFAULT_COIN_TYPE;
};

const hdHardened = (value) => {
  if (!Number.isInteger(value) || value < 0 || value > HD_MAX_NON_HARDENED) {
    throw new Error(`Invalid HD index: ${value}`);
  }
  return (value + HD_HARDENED_OFFSET) >>> 0;
};

const deriveSlip10Master = async (seedBytes) => {
  const out = await hmacSha512(enc.encode("ed25519 seed"), seedBytes);
  if (out.length !== 64) {
    throw new Error("Invalid SLIP-0010 master output");
  }
  return {
    key: out.slice(0, 32),
    chainCode: out.slice(32),
  };
};

const deriveSlip10Child = async (node, childIndex) => {
  if (!node || node.key.length !== 32 || node.chainCode.length !== 32) {
    throw new Error("Invalid SLIP-0010 node state");
  }
  const data = new Uint8Array(37);
  data[0] = 0x00;
  data.set(node.key, 1);
  data.set(ser32BE(childIndex), 33);
  const out = await hmacSha512(node.chainCode, data);
  if (out.length !== 64) {
    throw new Error("Invalid SLIP-0010 child output");
  }
  return {
    key: out.slice(0, 32),
    chainCode: out.slice(32),
  };
};

const hdPath = (coinType, account, change, index) =>
  `m/${HD_PURPOSE}'/${coinType}'/${account}'/${change}'/${index}'`;

const deriveHDKeyPairFromMnemonic = async (
  mnemonic,
  password,
  {
    account = HD_DEFAULT_ACCOUNT,
    change = HD_DEFAULT_CHANGE,
    index = HD_DEFAULT_INDEX,
  } = {},
) => {
  const seed = await bip39.mnemonicToSeed(mnemonic, password);
  const coinType = hdCoinTypeFromChainId(state.chainId);
  let node = await deriveSlip10Master(new Uint8Array(seed));
  const parts = [HD_PURPOSE, coinType, account, change, index];
  for (const part of parts) {
    node = await deriveSlip10Child(node, hdHardened(part));
  }
  const keyPair = nacl.sign.keyPair.fromSeed(node.key);
  return {
    keyPair,
    hd: {
      scheme: HD_SCHEME,
      path: hdPath(coinType, account, change, index),
      purpose: HD_PURPOSE,
      coin_type: coinType,
      account,
      change,
      index,
    },
  };
};

const addressFromPublicKey = async (pubKey, chainId) => {
  const prefix = enc.encode(`MSC-ADDR|${chainId}|`);
  const payload = concatBytes([prefix, pubKey]);
  const h1 = await sha256(payload);
  const h2 = await sha256(h1);
  const addressBytes = new Uint8Array(21);
  addressBytes[0] = 0x01;
  addressBytes.set(h2.slice(0, 20), 1);
  return `MSC${bytesToHex(addressBytes)}`;
};

const deriveKey = async (password, salt, iterations = 150000) => {
  const keyMaterial = await crypto.subtle.importKey(
    "raw",
    enc.encode(password),
    "PBKDF2",
    false,
    ["deriveKey"],
  );
  return crypto.subtle.deriveKey(
    {
      name: "PBKDF2",
      salt,
      iterations,
      hash: "SHA-256",
    },
    keyMaterial,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
};

const encryptSecretKey = async (secretKey, password) => {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await deriveKey(password, salt);
  const cipher = await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, secretKey);
  return {
    ciphertext: bytesToHex(new Uint8Array(cipher)),
    iv: bytesToHex(iv),
    salt: bytesToHex(salt),
    iterations: 150000,
  };
};

const decryptSecretKey = async (cryptoData, password) => {
  const salt = hexToBytes(cryptoData.salt);
  const iv = hexToBytes(cryptoData.iv);
  const ciphertext = hexToBytes(cryptoData.ciphertext);
  const key = await deriveKey(password, salt, cryptoData.iterations || 150000);
  const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ciphertext);
  return new Uint8Array(plain);
};

const storeWallet = (wallet) => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(wallet));
};

const loadWallet = () => {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch (err) {
    return null;
  }
};

const api = async (path, { method = "GET", body } = {}) => {
  const headers = {};
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
  }
  const token = normalizeAuthToken(state.apiToken);
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${state.rpcUrl}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch (err) {
      data = text;
    }
  }
  if (!res.ok) {
    const message = (data && data.error) || text || res.statusText;
    throw new Error(message);
  }
  return data;
};

const updateWalletUI = () => {
  const wallet = state.wallet;
  el("walletAddress").textContent = wallet ? wallet.address : "—";
  el("walletPublicKey").textContent = wallet ? wallet.publicKey : "—";
  el("balanceAddress").value = wallet ? wallet.address : "";
  el("faucetAddress").value = wallet ? wallet.address : "";
  if (state.secretKey) {
    setStatus(walletStatus, "Unlocked", "success");
  } else if (wallet) {
    setStatus(walletStatus, "Locked", "info");
  } else {
    setStatus(walletStatus, "No wallet", "error");
  }
};

const normalizeFeePolicy = (raw) => {
  const parsed = raw && typeof raw === "object" ? raw : {};
  let minBps = Number(parsed.min_bps);
  let maxBps = Number(parsed.max_bps);
  let floorAmount = Number(parsed.floor_amount);
  let ceilAmount = Number(parsed.ceil_amount);

  if (!Number.isFinite(minBps) || minBps <= 0) minBps = DEFAULT_FEE_POLICY.min_bps;
  if (!Number.isFinite(maxBps) || maxBps <= 0) maxBps = DEFAULT_FEE_POLICY.max_bps;
  if (!Number.isFinite(floorAmount) || floorAmount <= 0) floorAmount = DEFAULT_FEE_POLICY.floor_amount;
  if (!Number.isFinite(ceilAmount) || ceilAmount <= 0) ceilAmount = DEFAULT_FEE_POLICY.ceil_amount;

  minBps = Math.max(1, Math.floor(minBps));
  maxBps = Math.max(minBps, Math.floor(maxBps));
  floorAmount = Math.max(1, Math.floor(floorAmount));
  ceilAmount = Math.max(floorAmount, Math.floor(ceilAmount));

  return {
    min_bps: minBps,
    max_bps: maxBps,
    floor_amount: floorAmount,
    ceil_amount: ceilAmount,
  };
};

const computeTxFee = (amount) => {
  const policy = normalizeFeePolicy(state.feePolicy);
  let minBps = policy.min_bps;
  let maxBps = policy.max_bps;
  let floorAmt = policy.floor_amount;
  let ceilAmt = policy.ceil_amount;

  if (amount <= 0) return 0;
  if (maxBps < minBps) maxBps = minBps;
  if (floorAmt <= 0) floorAmt = 200;
  if (ceilAmt <= floorAmt) ceilAmt = floorAmt;

  let bps = minBps;
  if (amount > floorAmt && maxBps > minBps) {
    if (amount >= ceilAmt) {
      bps = maxBps;
    } else {
      bps = minBps + ((amount - floorAmt) * (maxBps - minBps)) / (ceilAmt - floorAmt);
    }
  }

  let fee = Math.floor((amount * bps) / 10000);
  if (fee < 1) fee = 1;
  return fee;
};

const buildTxPayload = (tx, chainId) => {
	const parts = [];
  const txType = Number.parseInt(tx.type ?? tx.Type ?? 0, 10) || 0;
  const normalizedValidatorPubKey = normalizeValidatorPubKeyHex(
    tx.validator_pubkey || tx.ValidatorPubKey || "",
  );
  const pushString = (value) => {
    const bytes = enc.encode(value);
    parts.push(bytes);
    parts.push(new Uint8Array([0]));
  };
  const pushInt64 = (value) => {
    const buf = new ArrayBuffer(8);
    const view = new DataView(buf);
    view.setBigInt64(0, BigInt(value), false);
    parts.push(new Uint8Array(buf));
  };

  pushString(tx.from);
  pushString(tx.to);
  pushString(tx.coin || "MSC");
  pushInt64(tx.amount);
  pushInt64(tx.fee);
  pushInt64(tx.nonce);
  pushInt64(tx.expiry);
  pushInt64(tx.stake_epochs || 0);
  if (txType === 2 && normalizedValidatorPubKey) {
    pushString(normalizedValidatorPubKey);
  }
	// Historical wire slots stay fixed and empty. The removed VM has no wallet
	// signing surface.
	pushInt64(0);
	pushString("");
	pushString("");
	pushString("");
	pushString("");
  pushString(chainId);
  parts.push(new Uint8Array([txType & 0xff]));

  return concatBytes(parts);
};

const signTransaction = async (tx, chainId) => {
  if (!state.secretKey) {
    throw new Error("Wallet locked");
  }
  const payload = buildTxPayload(tx, chainId);
  const signature = nacl.sign.detached(payload, state.secretKey);
  const sigHex = bytesToHex(signature);
  const txId = await sha256(payload);
  return {
    ...tx,
    signature: sigHex,
    id: bytesToHex(txId),
  };
};

const saveConnection = () => {
  state.rpcUrl = preferHttpsForLocalRpc(el("rpcUrl").value.trim() || window.location.origin);
  state.chainId = el("chainId").value.trim() || "91938";
  state.apiToken = normalizeAuthToken(el("apiToken").value);
  el("apiToken").value = state.apiToken;
  localStorage.setItem("msc_rpc", state.rpcUrl);
  localStorage.setItem("msc_chain", state.chainId);
  localStorage.setItem("msc_token", state.apiToken);
  refreshFeePolicy();
  setStatus(connectionStatus, "Connected", "success");
};

const refreshFeePolicy = async () => {
  try {
    const status = await api("/status");
    if (status && status.chain_id) {
      state.chainId = String(status.chain_id).trim() || state.chainId;
      const chainInput = el("chainId");
      if (chainInput) {
        chainInput.value = state.chainId;
      }
      localStorage.setItem("msc_chain", state.chainId);
    }
    if (status && status.fee_policy) {
      state.feePolicy = normalizeFeePolicy(status.fee_policy);
      updateFeeLabels();
    }
  } catch (err) {
    // Keep local defaults when RPC is unavailable.
  }
};

const createWallet = async (event) => {
  event.preventDefault();
  const password = el("createPassword").value.trim();
  if (!password) {
    setStatus(walletStatus, "Password required", "error");
    return;
  }

  const mnemonic = bip39.generateMnemonic(256);
  const { keyPair, hd } = await deriveHDKeyPairFromMnemonic(mnemonic, password);
  const address = await addressFromPublicKey(keyPair.publicKey, state.chainId);

  const cryptoData = await encryptSecretKey(keyPair.secretKey, password);
  const wallet = {
    address,
    publicKey: bytesToHex(keyPair.publicKey),
    crypto: cryptoData,
    hd,
  };

  storeWallet(wallet);
  state.wallet = wallet;
  state.secretKey = keyPair.secretKey;

  el("mnemonicBox").textContent = mnemonic;
  el("createPassword").value = "";
  updateWalletUI();
  setStatus(walletStatus, "Wallet created", "success");
};

const importMnemonic = async (event) => {
  event.preventDefault();
  const mnemonic = el("importMnemonic").value.trim();
  const password = el("importPassword").value.trim();
  if (!mnemonic || !password) {
    setStatus(walletStatus, "Mnemonic + password required", "error");
    return;
  }
  if (!bip39.validateMnemonic(mnemonic)) {
    setStatus(walletStatus, "Invalid mnemonic", "error");
    return;
  }

  const { keyPair, hd } = await deriveHDKeyPairFromMnemonic(mnemonic, password);
  const address = await addressFromPublicKey(keyPair.publicKey, state.chainId);

  const cryptoData = await encryptSecretKey(keyPair.secretKey, password);
  const wallet = {
    address,
    publicKey: bytesToHex(keyPair.publicKey),
    crypto: cryptoData,
    hd,
  };

  storeWallet(wallet);
  state.wallet = wallet;
  state.secretKey = keyPair.secretKey;

  el("importMnemonic").value = "";
  el("importPassword").value = "";
  setStatus(walletStatus, "Wallet imported", "success");
  updateWalletUI();
};

const importPrivateKey = async (event) => {
  event.preventDefault();
  const rawKey = el("importPrivateKey").value.trim();
  const password = el("importKeyPassword").value.trim();
  if (!rawKey || !password) {
    setStatus(walletStatus, "Private key + password required", "error");
    return;
  }
  const secretKey = hexToBytes(rawKey);
  if (secretKey.length !== 64) {
    setStatus(walletStatus, "Invalid key length", "error");
    return;
  }
  const publicKey = secretKey.slice(32);
  const address = await addressFromPublicKey(publicKey, state.chainId);
  const cryptoData = await encryptSecretKey(secretKey, password);

  const wallet = {
    address,
    publicKey: bytesToHex(publicKey),
    crypto: cryptoData,
  };

  storeWallet(wallet);
  state.wallet = wallet;
  state.secretKey = secretKey;
  el("importPrivateKey").value = "";
  el("importKeyPassword").value = "";
  setStatus(walletStatus, "Private key imported", "success");
  updateWalletUI();
};

const unlockWallet = async (event) => {
  event.preventDefault();
  const password = el("unlockPassword").value.trim();
  if (!password) {
    setStatus(walletStatus, "Password required", "error");
    return;
  }
  const wallet = state.wallet || loadWallet();
  if (!wallet) {
    setStatus(walletStatus, "No wallet found", "error");
    return;
  }
  try {
    const secretKey = await decryptSecretKey(wallet.crypto, password);
    state.wallet = wallet;
    state.secretKey = secretKey;
    el("unlockPassword").value = "";
    updateWalletUI();
    setStatus(walletStatus, "Unlocked", "success");
  } catch (err) {
    setStatus(walletStatus, "Unlock failed", "error");
  }
};

const lockWallet = () => {
  state.secretKey = null;
  updateWalletUI();
  setStatus(walletStatus, "Locked", "info");
};

const exportPrivateKey = () => {
  if (!state.secretKey) {
    setStatus(walletStatus, "Unlock wallet first", "error");
    return;
  }
  el("exportKeyBox").textContent = bytesToHex(state.secretKey);
};

const copyAddress = async () => {
  const address = el("walletAddress").textContent.trim();
  if (!address || address === "—") {
    setStatus(walletStatus, "No address", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(address);
    setStatus(walletStatus, "Copied", "success");
  } catch (err) {
    setStatus(walletStatus, "Copy failed", "error");
  }
};

const updateFeeLabels = () => {
  const sendAmount = parseInt(el("sendAmount").value, 10) || 0;
  const stakeAmount = parseInt(el("stakeAmount").value, 10) || 0;
  const unstakeAmountEl = el("unstakeAmount");
  const unstakeAmount = unstakeAmountEl ? parseInt(unstakeAmountEl.value, 10) || 0 : 0;
  el("sendFee").textContent = computeTxFee(sendAmount);
  el("stakeFee").textContent = computeTxFee(stakeAmount);
  const unstakeFeeEl = el("unstakeFee");
  if (unstakeFeeEl) {
    unstakeFeeEl.textContent = computeTxFee(unstakeAmount);
  }
};

const fetchBalance = async (event) => {
  event.preventDefault();
  const address = el("balanceAddress").value.trim();
  const coin = el("balanceCoin").value.trim() || "MSC";
  if (!address) {
    setStatus(balanceStatus, "Address required", "error");
    return;
  }
  try {
    const data = await api(
      `/balance?address=${encodeURIComponent(address)}&coin=${encodeURIComponent(coin)}&state=finalized`,
    );
    el("balanceResult").textContent = `${data.balance} ${data.coin}`;
    setStatus(balanceStatus, "Balance updated", "success");
  } catch (err) {
    setStatus(balanceStatus, err.message || "Balance failed", "error");
  }
};

const requestFaucet = async (event) => {
  event.preventDefault();
  const address = el("faucetAddress").value.trim();
  const amount = parseInt(el("faucetAmount").value, 10);
  const coin = el("faucetCoin").value.trim() || "MSC";
  if (!address || !amount) {
    setStatus(faucetStatus, "Address + amount required", "error");
    return;
  }
  try {
    const data = await api("/faucet", { method: "POST", body: { address, amount, coin } });
    setStatus(faucetStatus, `Funded ${data.amount} ${data.coin}`, "success");
  } catch (err) {
    setStatus(faucetStatus, err.message || "Faucet failed", "error");
  }
};

const submitTransaction = async (tx) => {
  return api("/submitTx", { method: "POST", body: tx });
};

const sendTransfer = async (event) => {
  event.preventDefault();
  if (!state.wallet || !state.secretKey) {
    setStatus(sendStatus, "Unlock wallet first", "error");
    return;
  }
  const to = el("sendTo").value.trim();
  const amount = parseInt(el("sendAmount").value, 10);
  const coin = el("sendCoin").value.trim() || "MSC";
  if (!to || !amount) {
    setStatus(sendStatus, "Recipient + amount required", "error");
    return;
  }

  try {
    const nonceData = await api(`/nonce?address=${encodeURIComponent(state.wallet.address)}`);
    const fee = computeTxFee(amount);
    const tx = {
      from: state.wallet.address,
      to,
      amount,
      nonce: nonceData.nonce + 1,
      publicKey: state.wallet.publicKey,
      signature: "",
      fee,
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 0,
      coin,
    };
    const signed = await signTransaction(tx, state.chainId);
    const outgoing = {
      ...signed,
      ChainID: state.chainId,
      Coin: coin,
      Type: 0,
    };
    await submitTransaction(outgoing);
    setStatus(sendStatus, "Transaction submitted", "success");
  } catch (err) {
    setStatus(sendStatus, err.message || "Send failed", "error");
  }
};

const sendStake = async (event) => {
  event.preventDefault();
  if (!state.wallet || !state.secretKey) {
    setStatus(stakeStatus, "Unlock wallet first", "error");
    return;
  }
  const validatorId = el("stakeValidator").value.trim();
  const amount = parseInt(el("stakeAmount").value, 10);
  const coin = el("stakeCoin").value.trim() || "MSC";
  if (!validatorId || !amount) {
    setStatus(stakeStatus, "Validator + amount required", "error");
    return;
  }
  let validatorPubKey = "";
  try {
    validatorPubKey = normalizeValidatorPubKeyHex(el("stakeValidatorPubKey")?.value || "");
  } catch (err) {
    setStatus(stakeStatus, err.message || "Invalid validator consensus pubkey", "error");
    return;
  }

  try {
    const nonceData = await api(`/nonce?address=${encodeURIComponent(state.wallet.address)}`);
    const fee = computeTxFee(amount);
    const lockEpochsInput = el("stakeLockEpochs");
    let lockEpochs = parseInt(lockEpochsInput?.value, 10);
    if (!lockEpochs || lockEpochs <= 0) {
      lockEpochs = DEFAULT_STAKE_EPOCHS;
    }
    const tx = {
      from: state.wallet.address,
      to: validatorId,
      amount,
      nonce: nonceData.nonce + 1,
      publicKey: state.wallet.publicKey,
      signature: "",
      fee,
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 2,
      stake_epochs: lockEpochs,
      coin,
      ...(validatorPubKey ? { validator_pubkey: validatorPubKey } : {}),
    };
    const signed = await signTransaction(tx, state.chainId);
    const outgoing = {
      ...signed,
      ChainID: state.chainId,
      Coin: coin,
      Type: 2,
    };
    await submitTransaction(outgoing);
    setStatus(stakeStatus, "Stake submitted", "success");
  } catch (err) {
    setStatus(stakeStatus, err.message || "Stake failed", "error");
  }
};

const sendUnstake = async (event) => {
  event.preventDefault();
  if (!state.wallet || !state.secretKey) {
    setStatus(unstakeStatus, "Unlock wallet first", "error");
    return;
  }
  const validatorId = el("unstakeValidator").value.trim();
  const amount = parseInt(el("unstakeAmount").value, 10);
  const coin = el("unstakeCoin").value.trim() || "MSC";
  if (!validatorId || !amount) {
    setStatus(unstakeStatus, "Validator + amount required", "error");
    return;
  }

  try {
    const nonceData = await api(`/nonce?address=${encodeURIComponent(state.wallet.address)}`);
    const fee = computeTxFee(amount);
    const tx = {
      from: state.wallet.address,
      to: validatorId,
      amount,
      nonce: nonceData.nonce + 1,
      publicKey: state.wallet.publicKey,
      signature: "",
      fee,
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 6,
      coin,
    };
    const signed = await signTransaction(tx, state.chainId);
    const outgoing = {
      ...signed,
      ChainID: state.chainId,
      Coin: coin,
      Type: 6,
    };
    await submitTransaction(outgoing);
    setStatus(unstakeStatus, "Unstake submitted", "success");
  } catch (err) {
    setStatus(unstakeStatus, err.message || "Unstake failed", "error");
  }
};

const init = () => {
  el("rpcUrl").value = state.rpcUrl;
  el("chainId").value = state.chainId;
  state.apiToken = normalizeAuthToken(state.apiToken);
  el("apiToken").value = state.apiToken;

  state.wallet = loadWallet();
  updateWalletUI();
  updateFeeLabels();
  refreshFeePolicy();

  el("saveConnection").addEventListener("click", saveConnection);
  el("createWalletForm").addEventListener("submit", createWallet);
  el("importMnemonicForm").addEventListener("submit", importMnemonic);
  el("importKeyForm").addEventListener("submit", importPrivateKey);
  el("unlockForm").addEventListener("submit", unlockWallet);
  el("lockWallet").addEventListener("click", lockWallet);
  el("exportKey").addEventListener("click", exportPrivateKey);
  el("copyAddress").addEventListener("click", copyAddress);

  el("balanceForm").addEventListener("submit", fetchBalance);
  el("faucetForm").addEventListener("submit", requestFaucet);
  el("sendForm").addEventListener("submit", sendTransfer);
  el("stakeForm").addEventListener("submit", sendStake);
  el("unstakeForm").addEventListener("submit", sendUnstake);

  el("sendAmount").addEventListener("input", updateFeeLabels);
  el("stakeAmount").addEventListener("input", updateFeeLabels);
  el("unstakeAmount").addEventListener("input", updateFeeLabels);

  setStatus(connectionStatus, "Ready", "success");
};

init();
