const enc = new TextEncoder();
const dec = new TextDecoder();
const STORAGE_KEY = "msc_wallet_browser_v1";
const ADDRESS_BOOK_KEY = "msc_wallet_address_book_v1";
const SESSION_KEY = "msc_wallet_session_v1";
const CHAIN_ID = "91938";
const DEFAULT_STAKE_EPOCHS = 19872000;
const MIN_RECOMMENDED_VALIDATOR_SCORE = 80;
const MIN_RECOMMENDED_SIGNED_PERCENT = 95;
const MAX_RECOMMENDED_COMMISSION_PERCENT = 20;
const RECOMMENDED_VALIDATOR_POOL_SIZE = 5;
const AES_ITERATIONS = 150000;
const DEFAULT_AUTO_LOCK_MS = 5 * 60 * 1000;
const WIZARD_VERIFY_WORD_COUNT = 3;
const HARDWARE_WALLET_HID_FILTERS = [{ vendorId: 0x2c97 }, { vendorId: 0x1209 }, { vendorId: 0x534c }];
const HARDWARE_WALLET_USB_FILTERS = [{ vendorId: 0x2c97 }, { vendorId: 0x1209 }, { vendorId: 0x534c }];
const RPC_ENDPOINTS_KEY = "msc_rpc_endpoints_v1";
const RPC_MODE_KEY = "msc_rpc_mode_v1";
const LEGACY_RPC_KEY = "msc_rpc";
const DEFAULT_PUBLIC_RPCS = ["https://wallet.mscblockexplorer.in"];
const MSC_LOGO_SRC = "assets/msc-logo-64.png";
const MSC_APP_ICON_SRC = "assets/msc-app-icon-64.png";
const MSC_WALLET_ICON_SRC = "assets/msc-wallet-icon.png";
const MSC_EXPLORER_ICON_SRC = "assets/msc-explorer-icon.png";
const MSC_VALIDATOR_BADGE_SRC = "assets/msc-validator-badge.png";
const MSC_GOVERNANCE_BADGE_SRC = "assets/msc-governance-badge.png";
const MSC_NFT_BADGE_SRC = "assets/msc-nft-badge.png";
const MSC_BRIDGE_BADGE_SRC = "assets/msc-bridge-badge.png";
const BRIDGE_GATEWAY_VERSION = "msc-bridge-gateway-v3";
const FAUCET_COOLDOWN_KEY = "msc_wallet_faucet_cooldown_v1";
const AMBASSADOR_REFERRAL_KEY = "msc_wallet_ambassador_referral_v1";
const RECOVERY_ATTEMPTS_KEY = "msc_wallet_recovery_attempts_v1";
const FAUCET_AMOUNT = 100;
const FAUCET_COOLDOWN_MS = 24 * 60 * 60 * 1000;
const RECOVERY_ATTEMPT_LIMIT = 5;
const RECOVERY_COOLDOWN_MS = 30000;
const VALIDATOR_LINK_TYPE = "msc_validator_wallet_link_v1";
const VALIDATOR_LINK_STORAGE_KEY = "msc_validator_wallet_links_v1";
const VALIDATOR_VOTE_HISTORY_KEY = "msc_validator_vote_history_v1";
const QR_SCAN_MAX_SIZE = 960;
const QR_IMAGE_MAX_BYTES = 8 * 1024 * 1024;
const QR_IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/webp"]);
const HEALTH_CHECK_MIN_MS = 15000;
const REQUEST_TIMEOUT_MS = 7000;
const WALLET_CACHE_KEY = "msc_wallet_data_cache_v3";
const CACHE_TTL = {
  status: 8000,
  cmd: 8000,
  balance: 30000,
  walletStatus: 30000,
  txs: 30000,
  validators: 60000,
  governance: 60000,
  bridge: 60000,
  lb: 10000,
  publicNodes: 15000,
  publicStatus: 10000,
};
const POLL_FALLBACK_MIN_MS = 15000;
const POLL_FALLBACK_MAX_MS = 60000;
const UPTIME_CACHE_KEY = "msc_public_node_uptime_v1";
const UPTIME_MAX_SAMPLES = 200;
const MSC_WORDS = [
  "apple", "moon", "river", "tiger", "metal", "cloud", "bridge", "orange", "galaxy", "stone", "future", "energy",
  "anchor", "asset", "beacon", "binary", "breeze", "canvas", "castle", "cedar", "circle", "copper", "crystal", "delta",
  "desert", "dragon", "eagle", "ember", "engine", "fabric", "falcon", "forest", "garden", "gold", "harbor", "harmony",
  "hazel", "island", "jungle", "kernel", "ladder", "lantern", "legend", "lotus", "magnet", "matrix", "meteor", "mirror",
  "nebula", "north", "ocean", "orbit", "pearl", "phoenix", "planet", "plasma", "quantum", "radar", "rocket", "saddle",
  "signal", "silver", "solar", "sphere", "spring", "summit", "temple", "timber", "token", "tower", "tunnel", "velvet",
  "victory", "violet", "voyage", "wallet", "willow", "winter", "yellow", "zenith", "zero", "active", "bright", "carbon",
  "cipher", "domain", "echo", "fabric", "globe", "honest", "impact", "jewel", "keeper", "linear", "moment", "native",
  "oxygen", "packet", "public", "reason", "satoshi", "secure", "stable", "system", "thunder", "united", "vector", "wisdom",
  "yield", "zebra", "apollo", "aurora", "butter", "citadel", "diamond", "emerald", "fortune", "granite", "helium", "insight",
  "journey", "kinetic", "liberty", "mercury", "network", "opal", "prairie", "reserve", "shelter", "topaz", "upgrade", "valley",
  "wander", "xenon", "yearn", "zephyr",
];
const MSC_WORD_SET = new Set(MSC_WORDS);

const $ = (id) => document.getElementById(id);
const page = document.body.dataset.page || "dashboard";

const state = {
  rpc: "",
  wallet: null,
  secretKey: null,
  status: null,
  cmd: null,
  rpcManager: null,
  balanceVerification: null,
  balanceHeight: 0,
  publicNodesRegistry: null,
  dataCache: null,
  schedulerTimer: null,
  networkRefreshTimer: null,
  lastNetworkMetadataRefreshAt: 0,
  walletRefreshTimer: null,
  sessionTimer: null,
  refreshRunning: false,
  pollDelayMs: POLL_FALLBACK_MIN_MS,
  realtime: {
    socket: null,
    connected: false,
    fallback: true,
    lastEventAt: 0,
    reconnectAttempts: 0,
    height: 0,
    finalizedHeight: 0,
    lastBlockAgeBaseSeconds: null,
    lastBlockAgeUpdatedAt: 0,
    eventDelayMs: null,
  },
  blockAgeTimer: null,
  validatorFilter: "all",
  selectedStakeValidatorId: "",
  validatorLoadState: "idle",
  qrScan: {
    stream: null,
    detector: null,
    detectorFailed: false,
    active: false,
    raf: 0,
  },
  ambassadorReferralCode: "",
  recoveryMethod: "seed",
  recoveryCooldownTimer: null,
  createWizard: null,
  bridgeRoutes: [],
  bridgeHistory: [],
  bridgeHistoryFilter: "all",
};

function normalizeRPC(raw) {
  let value = String(raw || "").trim();
  if (!value) value = window.location.origin;
  if (value === "null" || value === "file://") value = "";
  if (!value) value = DEFAULT_PUBLIC_RPCS[0];
  if (!/^[a-zA-Z][a-zA-Z\d+\-.]*:\/\//.test(value)) value = `https://${value}`;
  return value.replace(/\/+$/, "");
}

function isUsableRPC(raw) {
  const value = String(raw || "").trim();
  return !!value && value !== "null" && value !== "file://" && !value.startsWith("chrome-extension:");
}

function uniqueRPCs(items) {
  const seen = new Set();
  const out = [];
  for (const item of items || []) {
    if (!isUsableRPC(item)) continue;
    const rpc = normalizeRPC(item);
    const key = rpc.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(rpc);
  }
  return out;
}

function parseRPCEndpointList(raw) {
  if (Array.isArray(raw)) return uniqueRPCs(raw);
  const text = String(raw || "").trim();
  if (!text) return [];
  try {
    const parsed = JSON.parse(text);
    if (Array.isArray(parsed)) return uniqueRPCs(parsed);
  } catch (_) {
    // Comma/newline separated lists are easier for operators to edit by hand.
  }
  return uniqueRPCs(text.split(/[\n,]+/));
}

function defaultRPCEndpoints() {
  const fromWindow = Array.isArray(window.MSC_PUBLIC_RPC_ENDPOINTS) ? window.MSC_PUBLIC_RPC_ENDPOINTS : [];
  const origin = isUsableRPC(window.location.origin) && /^https?:\/\//i.test(window.location.origin) ? [window.location.origin] : [];
  return uniqueRPCs([...origin, ...fromWindow, ...DEFAULT_PUBLIC_RPCS]);
}

function rpcPolicyWarning(rpc) {
  try {
    const url = new URL(rpc);
    const host = url.hostname;
    const port = Number(url.port || 0);
    if (host === "localhost" || host === "127.0.0.1" || host === "::1") return "local/custom";
    if (/^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[0-1])\.)/.test(host)) return "private/custom";
    if (port >= 26657 && port <= 26666) return "validator-port/custom";
  } catch (_) {
    return "custom";
  }
  return "";
}

function loadRPCMode() {
  const mode = String(localStorage.getItem(RPC_MODE_KEY) || "auto").toLowerCase();
  return ["auto", "manual", "custom"].includes(mode) ? mode : "auto";
}

function loadRPCEndpoints() {
  const saved = localStorage.getItem(RPC_ENDPOINTS_KEY);
  const legacy = localStorage.getItem(LEGACY_RPC_KEY);
  const savedList = parseRPCEndpointList(saved);
  const legacyList = legacy ? parseRPCEndpointList(legacy) : [];
  const defaults = defaultRPCEndpoints();
  const endpoints = savedList.length ? savedList : uniqueRPCs([...legacyList, ...defaults]);
  if (!savedList.length) localStorage.setItem(RPC_ENDPOINTS_KEY, JSON.stringify(endpoints));
  if (legacyList.length && endpoints[0]) localStorage.setItem(LEGACY_RPC_KEY, endpoints[0]);
  return endpoints.length ? endpoints : defaults;
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

function randomMnemonic(wordCount = 12) {
  const count = wordCount === 24 ? 24 : 12;
  const bytes = crypto.getRandomValues(new Uint8Array(count));
  return Array.from(bytes).map((value) => MSC_WORDS[value % MSC_WORDS.length]);
}

function normalizeMnemonic(words) {
  const list = Array.isArray(words) ? words : String(words || "").trim().split(/\s+/);
  return list.map((word) => String(word || "").trim().toLowerCase()).filter(Boolean);
}

function validateMnemonicPhrase(words) {
  const list = normalizeMnemonic(words);
  if (![12, 24].includes(list.length)) throw new Error("Seed phrase must contain 12 or 24 words.");
  const unknown = list.find((word) => !MSC_WORD_SET.has(word));
  if (unknown) throw new Error(`Unknown seed word: ${unknown}`);
  return list;
}

async function seedFromMnemonic(words) {
  const phrase = normalizeMnemonic(words).join(" ");
  if (!phrase) throw new Error("Seed phrase required");
  return sha256(enc.encode(`MSC-MNEMONIC-V1|${CHAIN_ID}|${phrase}`));
}

async function walletFromMnemonic(words) {
  const cleanWords = validateMnemonicPhrase(words);
  const seed = await seedFromMnemonic(cleanWords);
  if (!window.nacl?.sign?.keyPair?.fromSeed) throw new Error("Seed wallet generator unavailable");
  const kp = nacl.sign.keyPair.fromSeed(seed.slice(0, 32));
  return {
    keyPair: kp,
    address: await addressFromPublicKey(kp.publicKey),
    publicKey: bytesToHex(kp.publicKey),
    words: cleanWords,
  };
}

function sessionExpiresAt() {
  const autoLockMs = Number(state.wallet?.preferences?.autoLockMs ?? DEFAULT_AUTO_LOCK_MS);
  return autoLockMs > 0 ? Date.now() + autoLockMs : 0;
}

function sessionExpired(session) {
  const expiresAt = Number(session?.expiresAt || 0);
  return expiresAt > 0 && expiresAt <= Date.now();
}

function scheduleSessionAutoLock() {
  if (state.sessionTimer) window.clearTimeout(state.sessionTimer);
  state.sessionTimer = null;
  try {
    const session = JSON.parse(sessionStorage.getItem(SESSION_KEY) || "null");
    const expiresAt = Number(session?.expiresAt || 0);
    if (!expiresAt) return;
    const delay = Math.max(0, expiresAt - Date.now());
    state.sessionTimer = window.setTimeout(clearSessionUnlock, delay);
  } catch (_) {
    sessionStorage.removeItem(SESSION_KEY);
  }
}

function clearSessionUnlock() {
  if (state.sessionTimer) window.clearTimeout(state.sessionTimer);
  state.sessionTimer = null;
  sessionStorage.removeItem(SESSION_KEY);
  state.secretKey = null;
  updateWalletUI();
}

function saveSessionUnlock(secretKey, wallet = state.wallet) {
  if (!secretKey || !wallet?.address) return;
  sessionStorage.setItem(SESSION_KEY, JSON.stringify({
    address: wallet.address,
    publicKey: wallet.publicKey,
    secretKey: bytesToHex(secretKey),
    expiresAt: sessionExpiresAt(),
  }));
  scheduleSessionAutoLock();
}

function restoreSessionUnlock() {
  try {
    const wallet = state.wallet || loadWallet();
    const session = JSON.parse(sessionStorage.getItem(SESSION_KEY) || "null");
    if (!wallet?.address || !session?.secretKey || session.address !== wallet.address) return false;
    if (sessionExpired(session)) {
      sessionStorage.removeItem(SESSION_KEY);
      return false;
    }
    const secretKey = hexToBytes(session.secretKey);
    if (secretKey.length !== 64) return false;
    state.wallet = wallet;
    state.secretKey = secretKey;
    session.expiresAt = sessionExpiresAt();
    sessionStorage.setItem(SESSION_KEY, JSON.stringify(session));
    scheduleSessionAutoLock();
    return true;
  } catch (_) {
    sessionStorage.removeItem(SESSION_KEY);
    return false;
  }
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

function normalizeAmbassadorReferralCode(value) {
  return String(value || "").trim().toUpperCase().replace(/[^A-Z0-9-]/g, "").slice(0, 48);
}

function readAmbassadorReferralFromURL() {
  try {
    const params = new URLSearchParams(window.location.search);
    return normalizeAmbassadorReferralCode(params.get("ref") || params.get("ambassador") || params.get("msc_ref") || params.get("ambassador_code"));
  } catch (_) {
    return "";
  }
}

function currentAmbassadorReferralCode() {
  return normalizeAmbassadorReferralCode(state.ambassadorReferralCode || localStorage.getItem(AMBASSADOR_REFERRAL_KEY));
}

function initAmbassadorReferral() {
  const fromURL = readAmbassadorReferralFromURL();
  if (fromURL) localStorage.setItem(AMBASSADOR_REFERRAL_KEY, fromURL);
  state.ambassadorReferralCode = currentAmbassadorReferralCode();
}

function walletWithAmbassadorReferral(wallet) {
  const code = normalizeAmbassadorReferralCode(wallet?.ambassadorReferralCode || currentAmbassadorReferralCode());
  if (!wallet || !code) return wallet;
  return {
    ...wallet,
    ambassadorReferralCode: code,
    ambassadorReferralLinkedAt: wallet.ambassadorReferralLinkedAt || new Date().toISOString(),
  };
}

function saveWallet(wallet) {
  const storedWallet = walletWithAmbassadorReferral(wallet);
  localStorage.setItem(STORAGE_KEY, JSON.stringify(storedWallet));
  state.wallet = storedWallet;
}

function loadValidatorLinks() {
  try {
    return JSON.parse(localStorage.getItem(VALIDATOR_LINK_STORAGE_KEY) || "{}") || {};
  } catch (_) {
    return {};
  }
}

function loadValidatorVoteHistory() {
  try {
    return JSON.parse(localStorage.getItem(VALIDATOR_VOTE_HISTORY_KEY) || "[]") || [];
  } catch (_) {
    return [];
  }
}

function saveValidatorVoteHistoryEntry(entry) {
  const items = loadValidatorVoteHistory();
  items.unshift({ ...entry, recordedAt: new Date().toISOString() });
  localStorage.setItem(VALIDATOR_VOTE_HISTORY_KEY, JSON.stringify(items.slice(0, 40)));
}

function saveValidatorLinkForAddress(address, link) {
  const walletAddress = String(address || "").trim();
  if (!walletAddress || !link) return;
  const links = loadValidatorLinks();
  links[walletAddress] = link;
  localStorage.setItem(VALIDATOR_LINK_STORAGE_KEY, JSON.stringify(links));
  if (state.wallet?.address === walletAddress) {
    saveWallet({ ...state.wallet, validatorWallet: link });
  }
}

function validatorWalletLink() {
  const address = state.wallet?.address || "";
  if (!address) return null;
  return state.wallet?.validatorWallet || loadValidatorLinks()[address] || null;
}

function normalizeValidatorPubkey(value) {
  return String(value || "").trim().replace(/^0x/i, "").toLowerCase();
}

function validatorLinkMessage({ walletAddress, validatorId, consensusPubkey }) {
  return [
    "MSC_VALIDATOR_WALLET_LINK_V1",
    CHAIN_ID,
    String(walletAddress || "").trim(),
    String(validatorId || "").trim(),
    normalizeValidatorPubkey(consensusPubkey),
  ].join("|");
}

function buildValidatorLinkFromForm() {
  if (!state.wallet?.address) throw new Error("Create or unlock wallet first.");
  const validatorId = $("validatorLinkId")?.value.trim() || "";
  const consensusPubkey = normalizeValidatorPubkey($("validatorLinkPubkey")?.value || "");
  const nodeURL = $("validatorLinkNode")?.value.trim() || "";
  const signature = normalizeValidatorPubkey($("validatorLinkSignature")?.value || "");
  if (!validatorId) throw new Error("Validator ID required.");
  if (!consensusPubkey || hexToBytes(consensusPubkey).length !== 32) throw new Error("Consensus pubkey must be 32-byte hex.");
  return {
    type: VALIDATOR_LINK_TYPE,
    chainId: CHAIN_ID,
    walletAddress: state.wallet.address,
    validatorId,
    consensusPubkey,
    nodeURL,
    signature,
    message: validatorLinkMessage({ walletAddress: state.wallet.address, validatorId, consensusPubkey }),
  };
}

function verifyValidatorLink(link) {
  if (!link || link.type !== VALIDATOR_LINK_TYPE) throw new Error("Invalid validator link proof.");
  if (link.chainId !== CHAIN_ID) throw new Error("Validator proof is for a different chain.");
  if (!state.wallet?.address || link.walletAddress !== state.wallet.address) throw new Error("Proof wallet address does not match this wallet.");
  const pubkey = hexToBytes(link.consensusPubkey);
  const signature = hexToBytes(link.signature);
  if (pubkey.length !== 32) throw new Error("Invalid consensus pubkey.");
  if (signature.length !== 64) throw new Error("Node signature must be 64-byte hex.");
  if (!window.nacl?.sign?.detached?.verify) throw new Error("Signature verification unavailable.");
  const message = validatorLinkMessage(link);
  if (!nacl.sign.detached.verify(enc.encode(message), signature, pubkey)) {
    throw new Error("Validator node signature verification failed.");
  }
  return {
    ...link,
    message,
    verifiedAt: new Date().toISOString(),
    status: "verified",
  };
}

function walletSecurityFeatures(words = 12) {
  const recoveryLabel = words ? `${words} Word Mnemonic Backup` : "Private Key Backup";
  const features = [
    recoveryLabel,
    "12/24 Word Mnemonic",
    "AES-256 Wallet Encryption",
    "Password Protection",
    "Biometric Login",
    "Encrypted Recovery Kit",
    "Offline Backup Verification",
    "Auto Lock",
    "Hardware Wallet Support",
    "Offline Signing",
    "QR Import/Export",
    "Multi-Sig Wallet",
    "MPC Wallet (Future)",
  ];
  features.splice(3, 0, words ? "Seed Recovery" : "Private Key Restore");
  return features;
}

function biometricSupported() {
  return typeof window.PublicKeyCredential === "function";
}

function hardwareWalletCapabilities() {
  const secure = window.isSecureContext || ["localhost", "127.0.0.1"].includes(window.location.hostname);
  return {
    secure,
    hid: secure && !!navigator.hid?.requestDevice,
    usb: secure && !!navigator.usb?.requestDevice,
  };
}

function isHardwareWallet(wallet = state.wallet) {
  return wallet?.type === "hardware" || wallet?.hardware?.mode === "hardware";
}

function uniqueList(items) {
  return Array.from(new Set(items.filter(Boolean)));
}

function randomInt(maxExclusive) {
  if (globalThis.crypto?.getRandomValues && maxExclusive > 0) {
    const data = new Uint32Array(1);
    globalThis.crypto.getRandomValues(data);
    return data[0] % maxExclusive;
  }
  return Math.floor(Math.random() * maxExclusive);
}

function randomSeedVerifyPositions(wordCount = 12, count = WIZARD_VERIFY_WORD_COUNT) {
  const size = Math.max(1, Number(wordCount || 12));
  const positions = Array.from({ length: size }, (_, index) => index + 1);
  for (let index = positions.length - 1; index > 0; index -= 1) {
    const swap = randomInt(index + 1);
    [positions[index], positions[swap]] = [positions[swap], positions[index]];
  }
  return positions.slice(0, Math.min(count, positions.length)).sort((a, b) => a - b);
}

function buildRecoveryWallet({ address, publicKey, secretKey, password, words, source }) {
  return encryptSecretKey(secretKey, password).then(async (cryptoData) => ({
    address,
    publicKey,
    crypto: cryptoData,
    mnemonicCrypto: words?.length ? await encryptSecretKey(enc.encode(words.join(" ")), password) : undefined,
    mnemonic: words?.length ? {
      type: "msc_mnemonic_v1",
      words: words.length,
      verifiedAt: new Date().toISOString(),
    } : undefined,
    preferences: {
      autoLockMs: DEFAULT_AUTO_LOCK_MS,
    },
    securityFeatures: walletSecurityFeatures(words?.length || 0),
    createdAt: new Date().toISOString(),
    recoveredFrom: source,
  }));
}

function recoveryKitPayload(wallet = state.wallet) {
  if (!wallet?.address || !wallet?.crypto) throw new Error("Create or import a wallet first.");
  return {
    type: "msc_wallet_recovery_kit_v1",
    chainId: CHAIN_ID,
    exportedAt: new Date().toISOString(),
    wallet: {
      address: wallet.address,
      publicKey: wallet.publicKey,
      crypto: wallet.crypto,
      mnemonicCrypto: wallet.mnemonicCrypto || null,
      mnemonic: wallet.mnemonic || null,
      preferences: wallet.preferences || {},
      securityFeatures: wallet.securityFeatures || [],
      createdAt: wallet.createdAt || null,
      ambassadorReferralCode: wallet.ambassadorReferralCode || "",
    },
  };
}

function validateRecoveryKit(raw) {
  const data = typeof raw === "string" ? JSON.parse(raw) : raw;
  if (data?.chainId && String(data.chainId) !== CHAIN_ID) throw new Error("Recovery kit is for a different MSC chain ID.");
  const wallet = data?.wallet || data;
  if (!wallet?.address || !wallet?.publicKey || !wallet?.crypto?.ciphertext || !wallet?.crypto?.iv || !wallet?.crypto?.salt) {
    throw new Error("Invalid MSC recovery kit.");
  }
  return {
    address: wallet.address,
    publicKey: wallet.publicKey,
    crypto: wallet.crypto,
    mnemonicCrypto: wallet.mnemonicCrypto || undefined,
    mnemonic: wallet.mnemonic || undefined,
    preferences: wallet.preferences || { autoLockMs: DEFAULT_AUTO_LOCK_MS },
    securityFeatures: wallet.securityFeatures || walletSecurityFeatures(wallet.mnemonic?.words || 0),
    createdAt: wallet.createdAt || new Date().toISOString(),
    chainId: data?.chainId || CHAIN_ID,
    ambassadorReferralCode: wallet.ambassadorReferralCode || currentAmbassadorReferralCode(),
    importedAt: new Date().toISOString(),
  };
}

function downloadTextFile(filename, text, type = "application/json") {
  const blob = new Blob([text], { type });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = filename;
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(link.href), 1000);
}

function renderAmbassadorReferral() {
  const code = currentAmbassadorReferralCode();
  setText("ambassadorReferralCode", code || "No code");
  setStatus(
    "ambassadorReferralStatus",
    code ? "Ambassador code saved for this browser." : "Open the wallet from an ambassador link to attach a code.",
    code ? "success" : "warn",
  );
}

function shortAddress(value) {
  const raw = String(value || "");
  return raw.length > 14 ? `${raw.slice(0, 8)}...${raw.slice(-6)}` : raw || "-";
}

function setText(id, value) {
  const node = $(id);
  if (node) node.textContent = value ?? "-";
}

function setHTML(id, value) {
  const node = $(id);
  if (node) node.innerHTML = value;
}

function setTone(id, tone = "") {
  const node = $(id);
  if (!node) return;
  node.classList.toggle("success", tone === "success");
  node.classList.toggle("warn", tone === "warn");
  node.classList.toggle("error", tone === "error");
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
  node.classList.toggle("warn", tone === "warn");
  node.classList.toggle("error", tone === "error");
}

function passwordStrengthLabel(password) {
  const value = String(password || "");
  if (!value) return { label: "Enter password", tone: "", score: 0 };
  let score = 0;
  if (value.length >= 8) score += 1;
  if (value.length >= 12) score += 1;
  if (/[a-z]/.test(value) && /[A-Z]/.test(value)) score += 1;
  if (/\d/.test(value)) score += 1;
  if (/[^A-Za-z0-9]/.test(value)) score += 1;
  if (score >= 4) return { label: "Strong", tone: "success", score };
  if (score >= 3) return { label: "Good", tone: "warn", score };
  return { label: "Weak", tone: "error", score };
}

function renderPasswordStrength(inputId, meterId) {
  const input = $(inputId);
  const meter = $(meterId);
  if (!input || !meter) return;
  const result = passwordStrengthLabel(input.value);
  meter.dataset.strength = result.tone || "empty";
  meter.innerHTML = `<span></span><strong>${escapeHTML(result.label)}</strong>`;
}

function seedWordTarget(count) {
  return count > 12 ? 24 : 12;
}

function renderSeedDiagnostics(textareaId, counterId, chipsId) {
  const textarea = $(textareaId);
  if (!textarea) return { words: [], valid: false };
  const words = normalizeMnemonic(textarea.value);
  const target = seedWordTarget(words.length);
  const unknown = words.filter((word) => !MSC_WORD_SET.has(word));
  const countOK = words.length === 12 || words.length === 24;
  const valid = countOK && !unknown.length;
  textarea.classList.toggle("input-error", !!unknown.length);
  textarea.classList.toggle("input-warn", words.length > 0 && !countOK && !unknown.length);
  const counter = $(counterId);
  if (counter) {
    counter.textContent = `${Math.min(words.length, target)}/${target} words`;
    counter.classList.toggle("success", valid);
    counter.classList.toggle("warn", words.length > 0 && !countOK && !unknown.length);
    counter.classList.toggle("error", !!unknown.length);
  }
  const chips = $(chipsId);
  if (chips) {
    chips.innerHTML = words.length
      ? words.map((word, index) => `<span class="${MSC_WORD_SET.has(word) ? "" : "invalid"}"><b>${index + 1}</b>${escapeHTML(word)}</span>`).join("")
      : `<em>No seed words entered.</em>`;
  }
  return { words, valid, unknown };
}

function loadRecoveryAttempts() {
  try {
    const data = JSON.parse(sessionStorage.getItem(RECOVERY_ATTEMPTS_KEY) || "{}");
    if (Number(data.lockedUntil || 0) > 0 && Number(data.lockedUntil || 0) <= Date.now()) {
      sessionStorage.removeItem(RECOVERY_ATTEMPTS_KEY);
      return { count: 0, lockedUntil: 0 };
    }
    return {
      count: Number(data.count || 0),
      lockedUntil: Number(data.lockedUntil || 0),
    };
  } catch (_) {
    return { count: 0, lockedUntil: 0 };
  }
}

function saveRecoveryAttempts(data) {
  sessionStorage.setItem(RECOVERY_ATTEMPTS_KEY, JSON.stringify(data));
}

function recoveryCooldownRemaining() {
  const attempts = loadRecoveryAttempts();
  return Math.max(0, Number(attempts.lockedUntil || 0) - Date.now());
}

function renderRecoveryAttemptState() {
  const remaining = recoveryCooldownRemaining();
  const locked = remaining > 0;
  const seconds = Math.ceil(remaining / 1000);
  const attempts = loadRecoveryAttempts();
  const statusText = locked
    ? `Too many failed recovery attempts. Try again in ${seconds}s.`
    : attempts.count > 0
      ? `${Math.max(0, RECOVERY_ATTEMPT_LIMIT - attempts.count)} recovery attempts remaining.`
      : "Recovery attempts are protected with a cooldown.";
  setStatus("recoveryAttemptStatus", statusText, locked ? "error" : attempts.count ? "warn" : "");
  ["seedRecoverySubmit", "recoveryKitSubmit", "privateKeyRecoverySubmit"].forEach((id) => {
    const button = $(id);
    if (button) button.disabled = locked || !!button.closest("[data-recovery-panel]")?.hidden;
  });
  if (state.recoveryCooldownTimer) window.clearTimeout(state.recoveryCooldownTimer);
  state.recoveryCooldownTimer = locked ? window.setTimeout(renderRecoveryAttemptState, Math.min(1000, remaining)) : null;
}

function recoveryGate(statusId) {
  const remaining = recoveryCooldownRemaining();
  if (remaining <= 0) return true;
  setStatus(statusId, `Try again in ${Math.ceil(remaining / 1000)}s.`, "error");
  renderRecoveryAttemptState();
  return false;
}

function recordRecoveryFailure(statusId, message) {
  const attempts = loadRecoveryAttempts();
  const nextCount = attempts.count + 1;
  const locked = nextCount >= RECOVERY_ATTEMPT_LIMIT;
  const next = {
    count: locked ? RECOVERY_ATTEMPT_LIMIT : nextCount,
    lockedUntil: locked ? Date.now() + RECOVERY_COOLDOWN_MS : 0,
  };
  saveRecoveryAttempts(next);
  const remaining = Math.max(0, RECOVERY_ATTEMPT_LIMIT - next.count);
  setStatus(statusId, locked ? `${message} Try again in 30s.` : `${message} ${remaining} attempts left.`, "error");
  renderRecoveryAttemptState();
}

function resetRecoveryFailures() {
  sessionStorage.removeItem(RECOVERY_ATTEMPTS_KEY);
  renderRecoveryAttemptState();
}

function clearRecoveryPreview(previewId, confirmId) {
  const preview = $(previewId);
  if (preview) {
    preview.hidden = true;
    preview.innerHTML = "";
  }
  const confirm = $(confirmId);
  if (confirm) confirm.checked = false;
}

function showRecoveryPreview(previewId, confirmId, details) {
  const preview = $(previewId);
  if (!preview) return true;
  const confirmed = !!$(confirmId)?.checked;
  const current = loadWallet();
  const replaceWarning = current?.address
    ? `<div class="recovery-warning"><strong>Existing local wallet replace ho jayega.</strong><span>${escapeHTML(shortAddress(current.address))} will be replaced in this browser.</span></div>`
    : "";
  const rows = [
    ["Recovered address", details.address],
    ...(details.meta || []),
  ];
  preview.hidden = false;
  preview.innerHTML = `
    <div class="recovery-preview-head">
      <span>${escapeHTML(details.title || "Recovery Preview")}</span>
      <strong class="mono">${escapeHTML(shortAddress(details.address))}</strong>
    </div>
    <div class="simple-status-grid recovery-fingerprint">
      ${rows.map(([label, value]) => `<div><span>${escapeHTML(label)}</span><strong>${escapeHTML(value || "-")}</strong></div>`).join("")}
    </div>
    ${replaceWarning}
    <label class="check-row recovery-confirm-row">
      <input id="${confirmId}" type="checkbox" ${confirmed ? "checked" : ""} />
      <span>I confirm this recovered address and understand local wallet replacement.</span>
    </label>`;
  return confirmed;
}

function saveRecoveredWallet(wallet, secretKey, statusId, message) {
  saveWallet(wallet);
  state.secretKey = secretKey;
  saveSessionUnlock(state.secretKey, state.wallet);
  resetRecoveryFailures();
  setStatus(statusId, message, "success");
  updateWalletUI();
}

function setRecoveryMethod(method) {
  const next = ["seed", "kit", "private"].includes(method) ? method : "seed";
  state.recoveryMethod = next;
  document.querySelectorAll("[data-recovery-method]").forEach((button) => {
    const active = button.dataset.recoveryMethod === next;
    button.classList.toggle("active", active);
    button.setAttribute("aria-pressed", active ? "true" : "false");
  });
  document.querySelectorAll("[data-recovery-panel]").forEach((panel) => {
    const active = panel.dataset.recoveryPanel === next;
    panel.hidden = !active;
    panel.querySelectorAll("input, textarea, select, button").forEach((node) => {
      node.disabled = !active;
    });
  });
  renderRecoveryAttemptState();
}

async function renderRecoveryKitFingerprint() {
  const output = $("recoveryKitFingerprint");
  const file = $("recoveryKitFile")?.files?.[0];
  if (!output) return;
  if (!file) {
    output.innerHTML = `<em>Choose a recovery kit to show fingerprint.</em>`;
    clearRecoveryPreview("recoveryKitPreview", "recoveryKitConfirm");
    return;
  }
  try {
    const data = JSON.parse(await file.text());
    const wallet = data.wallet || data;
    output.innerHTML = `
      <div><span>Kit address</span><strong>${escapeHTML(shortAddress(wallet.address || "-"))}</strong></div>
      <div><span>Chain ID</span><strong>${escapeHTML(data.chainId || CHAIN_ID)}</strong></div>
      <div><span>Created</span><strong>${escapeHTML(wallet.createdAt || data.exportedAt || "-")}</strong></div>`;
  } catch (err) {
    output.innerHTML = `<div class="error"><span>Kit file</span><strong>Invalid JSON</strong></div>`;
  }
  clearRecoveryPreview("recoveryKitPreview", "recoveryKitConfirm");
}

function renderVerification(verification) {
  const mode = verification?.mode || "spv_pending";
  let text = "Checking";
  let tone = "";
  if (mode === "light") {
    text = `Verified h${verification.height || "-"}`;
    tone = "success";
  } else if (mode === "quorum") {
    text = verification.height
      ? `Verified h${verification.height} ${verification.matches}/${verification.checked}`
      : `Verified ${verification.matches}/${verification.checked}`;
    tone = "success";
  } else if (mode === "freshest") {
    text = verification.height
      ? `Fresh h${verification.height} ${verification.matches}/${verification.checked}`
      : `Fresh ${verification.matches}/${verification.checked}`;
    tone = "warn";
  } else if (mode === "unverified") {
    text = "Pending";
  } else if (mode === "mismatch") {
    text = "Check failed";
    tone = "error";
  }
  setStatus("balanceVerification", text, tone);
  setStatus("dashboardVerification", text, tone);
  setText("settingsRpcVerification", text);
}

function walletState() {
  if (!state.wallet) {
    return {
      label: "No wallet yet",
      tone: "warn",
      hint: "Create a wallet or unlock an existing one to start using MSC.",
      address: "Create or unlock a wallet to show your address.",
      actions: [
        ["create-wallet.html", "plus-circle", "Create Wallet", "primary"],
        ["login.html", "unlock-keyhole", "Unlock Wallet", ""],
      ],
    };
  }
  if (isHardwareWallet(state.wallet)) {
    return {
      label: "Hardware Wallet",
      tone: "success",
      hint: "Hardware wallet linked. Transactions require device approval.",
      address: state.wallet.address,
      actions: [
        ["receive.html", "qr-code", "Receive", "primary"],
        ["send.html", "send", "Prepare Send", ""],
        ["security.html", "lock-keyhole", "Security", ""],
      ],
    };
  }
  if (!state.secretKey) {
    return {
      label: "Locked",
      tone: "warn",
      hint: "Wallet found in this browser. Unlock it before sending MSC.",
      address: state.wallet.address,
      actions: [
        ["login.html", "unlock-keyhole", "Unlock Wallet", "primary"],
        ["receive.html", "qr-code", "Receive", ""],
      ],
    };
  }
  return {
    label: "Ready",
    tone: "success",
    hint: "Wallet unlocked. You can send, receive, or stake MSC.",
    address: state.wallet.address,
    actions: [
      ["send.html", "send", "Send", "primary"],
      ["receive.html", "qr-code", "Receive", ""],
      ["staking.html", "landmark", "Stake", ""],
    ],
  };
}

function renderWalletHome() {
  if (page !== "dashboard") return;
  const current = walletState();
  setStatus("walletHomeState", current.label, current.tone);
  setText("walletHomeAddress", current.address);
  setHTML("walletHomeActions", current.actions.map(([href, icon, label, kind]) =>
    `<a class="button ${kind}" href="${href}"><i data-lucide="${icon}"></i>${label}</a>`
  ).join(""));
  const guide = $("walletHomeGuide");
  if (guide) guide.classList.toggle("compact", !!state.wallet);
  window.lucide?.createIcons();
}

function ensureCreateWizard() {
  if (!state.createWizard) {
    state.createWizard = {
      step: 1,
      method: "create",
      wordCount: 12,
      words: [],
      generated: null,
      verifyPositions: [],
      verification: {},
      seedVerified: false,
      securityAccepted: false,
      biometric: false,
      autoLock: true,
      hardware: {
        transport: "",
        connected: false,
        deviceName: "",
        vendorId: "",
        productId: "",
        openState: "",
        label: "",
        address: "",
        publicKey: "",
      },
      status: "",
      tone: "",
      busy: false,
    };
  }
  return state.createWizard;
}

function wizardProgressLabels() {
  const wizard = ensureCreateWizard();
  return wizard.method === "hardware" && wizard.step > 1
    ? ["Welcome", "Connect", "Register", "Success"]
    : ["Welcome", "Security", "Generate", "Backup", "Verify", "Password", "Success"];
}

function wizardProgressActiveStep() {
  const wizard = ensureCreateWizard();
  if (wizard.method !== "hardware" || wizard.step <= 1) return wizard.step;
  if (wizard.step === 7) return 4;
  return Math.min(Math.max(wizard.step, 2), 3);
}

function setWizardStatus(message, tone = "") {
  const wizard = ensureCreateWizard();
  wizard.status = message || "";
  wizard.tone = tone;
  renderCreateWizard();
}

function wizardFrame(body) {
  const wizard = ensureCreateWizard();
  const steps = wizardProgressLabels();
  const activeStep = wizardProgressActiveStep();
  const progress = steps.map((label, index) => {
    const step = index + 1;
    const stateClass = step === activeStep ? "active" : step < activeStep ? "done" : "";
    return `<span class="${stateClass}"><b>${step}</b>${escapeHTML(label)}</span>`;
  }).join("");
  return `
    <div class="wizard-shell">
      <div class="wizard-progress">${progress}</div>
      ${body}
      ${wizard.status ? `<div class="wizard-status ${escapeHTML(wizard.tone)}">${escapeHTML(wizard.status)}</div>` : ""}
    </div>`;
}

function wizardBackButton() {
  const wizard = ensureCreateWizard();
  return wizard.step > 1 && wizard.step < 7
    ? `<button type="button" data-action="wizard-back"><i data-lucide="arrow-left"></i>Back</button>`
    : "";
}

function wizardFooter(primaryLabel = "Continue", primaryAction = "wizard-continue", disabled = false) {
  return `
    <div class="wizard-footer">
      ${wizardBackButton()}
      <button class="primary" type="button" data-action="${primaryAction}" ${disabled ? "disabled" : ""}>${escapeHTML(primaryLabel)}<i data-lucide="arrow-right"></i></button>
    </div>`;
}

function renderCreateWelcome() {
  const wizard = ensureCreateWizard();
  const options = [
    ["create", "plus-circle", "Create New Wallet", "Generate fresh MSC keys and a seed phrase on this device.", "Recommended"],
    ["import", "download", "Import Existing Wallet", "Use a seed phrase, encrypted recovery kit, or private key backup.", "Recovery"],
    ["hardware", "usb", "Hardware Wallet", "Connect a signing device when hardware support is enabled.", "Advanced"],
  ];
  return wizardFrame(`
    <section class="wizard-hero">
      <div class="logo large"><img src="${MSC_LOGO_SRC}" alt="MSC logo" /></div>
      <div>
        <div class="eyebrow">MSC Wallet Setup</div>
        <h1>Create New Wallet</h1>
        <p class="muted">Choose a new wallet, import an existing wallet, or prepare hardware wallet support. New users should save the seed phrase offline.</p>
      </div>
    </section>
    <div class="wizard-options">
      ${options.map(([method, iconName, title, text, badge]) => {
        const selected = wizard.method === method;
        return `
        <button type="button" class="wizard-option ${selected ? "active" : ""}" data-method="${method}" aria-pressed="${selected ? "true" : "false"}">
          <span class="wizard-option-top">
            <span class="wizard-option-icon"><i data-lucide="${iconName}"></i></span>
            <span class="wizard-option-badge">${escapeHTML(badge)}</span>
          </span>
          <strong>${escapeHTML(title)}</strong>
          <span>${escapeHTML(text)}</span>
          <span class="wizard-option-selected"><i data-lucide="${selected ? "check" : "circle"}"></i>${selected ? "Selected" : "Select"}</span>
        </button>`;
      }).join("")}
    </div>
    ${wizardFooter("Continue")}`);
}

function renderSecurityNotice() {
  const wizard = ensureCreateWizard();
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Security Notice</div>
      <h1>Protect your keys</h1>
      <p class="muted">MSC wallet keys are controlled only by you. Keep backups private and offline.</p>
    </section>
    <div class="security-notice-grid">
      <article><i data-lucide="lock-keyhole"></i><strong>Private Key kabhi share na karein</strong><span>Anyone with your private key can move your MSC.</span></article>
      <article><i data-lucide="file-key"></i><strong>Seed Phrase offline likhein</strong><span>Write it on paper and keep it away from screenshots or cloud sync.</span></article>
      <article><i data-lucide="shield-alert"></i><strong>MSC Team kabhi Seed Phrase nahi maangegi</strong><span>Support will never ask for your seed phrase or private key.</span></article>
    </div>
    <label class="check-row">
      <input id="riskCheck" type="checkbox" ${wizard.securityAccepted ? "checked" : ""} />
      <span>I understand the risks</span>
    </label>
    ${wizardFooter("Continue", "wizard-continue", !wizard.securityAccepted)}`);
}

function renderGenerateWalletStep() {
  const wizard = ensureCreateWizard();
  const generated = wizard.generated;
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Create Wallet</div>
      <h1>Generate wallet keys</h1>
      <p class="muted">The keypair is generated locally in your browser. The private key stays hidden.</p>
    </section>
    <div class="card">
      <div class="row">
        <div><div class="label">Address</div><div class="value mono">${escapeHTML(generated?.address || "Not generated")}</div></div>
        <div><div class="label">Public Key</div><div class="value mono">${escapeHTML(generated?.publicKey || "-")}</div></div>
        <div><div class="label">Private Key</div><div class="value mono">hidden</div></div>
      </div>
      <div class="safe-note"><strong>Local generation:</strong> wallet keys are created on this device and then encrypted with your password in step 6.</div>
      <button type="button" class="primary" data-action="generate-wallet" ${wizard.busy ? "disabled" : ""}><i data-lucide="sparkles"></i>${generated ? "Regenerate Wallet" : "Generate Wallet"}</button>
    </div>
    ${wizardFooter("Continue", "wizard-continue", !generated)}`);
}

function renderSeedPhraseStep() {
  const wizard = ensureCreateWizard();
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Seed Phrase</div>
      <h1>Write these words offline</h1>
      <p class="muted">You will verify selected words on the next screen. Store this phrase somewhere private.</p>
    </section>
    <section class="seed-panel" aria-label="MSC wallet seed phrase">
      <div class="seed-panel-head">
        <div><span class="label">Recovery words</span><strong>${wizard.words.length} word seed phrase</strong></div>
        <span class="pill warn"><i data-lucide="eye-off"></i>Private</span>
      </div>
      <div class="seed-grid">
        ${wizard.words.map((word, index) => `<button type="button" class="seed-word" data-action="copy-seed-word" data-word-index="${index}" aria-label="Copy seed word ${index + 1}">
          <span class="seed-index">${String(index + 1).padStart(2, "0")}</span>
          <strong>${escapeHTML(word)}</strong>
          <i data-lucide="copy"></i>
        </button>`).join("")}
      </div>
    </section>
    <div class="wizard-tools">
      <button type="button" data-action="copy-seed"><i data-lucide="copy"></i>Copy</button>
      <button type="button" data-action="download-seed"><i data-lucide="download"></i>Download PDF</button>
      <button type="button" data-action="print-seed"><i data-lucide="printer"></i>Print</button>
    </div>
    <div class="safe-note"><strong>Important:</strong> do not send this seed phrase to anyone, including MSC support.</div>
    <div class="safe-note warn"><strong>No screenshot:</strong> Screenshot/cloud backup mat lo. Paper/offline backup use karo.</div>
    ${wizardFooter("Continue")}`);
}

function wizardVerifyPositions() {
  const wizard = ensureCreateWizard();
  if (!wizard.verifyPositions?.length && wizard.words.length) {
    wizard.verifyPositions = randomSeedVerifyPositions(wizard.words.length);
  }
  return wizard.verifyPositions?.length ? wizard.verifyPositions : randomSeedVerifyPositions(wizard.wordCount || 12);
}

function renderVerifySeedStep() {
  const wizard = ensureCreateWizard();
  const positions = wizardVerifyPositions();
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Seed Verification</div>
      <h1>Confirm your backup</h1>
      <p class="muted">Enter the requested random words exactly as shown. These positions stay locked for this generated wallet.</p>
    </section>
    <div class="card">
      ${positions.map((position) => `
        <div class="field">
          <label for="verifyWord${position}">Word #${position} ?</label>
          <input id="verifyWord${position}" data-verify-word="${position}" autocomplete="off" spellcheck="false" value="${escapeHTML(wizard.verification[position] || "")}" />
        </div>`).join("")}
    </div>
    ${wizardFooter("Continue")}`);
}

function renderPasswordStep() {
  const wizard = ensureCreateWizard();
  const biometricReady = biometricSupported();
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Wallet Password</div>
      <h1>Encrypt this wallet</h1>
      <p class="muted">Use a strong password. It encrypts your private key and saved seed backup in this browser.</p>
    </section>
    <div class="card">
      <div class="row">
        <div class="field"><label for="wizardPassword">Password</label><input id="wizardPassword" type="password" autocomplete="new-password" /><div id="wizardPasswordStrength" class="password-meter" data-strength="empty"><span></span><strong>Enter password</strong></div></div>
        <div class="field"><label for="wizardPasswordConfirm">Confirm Password</label><input id="wizardPasswordConfirm" type="password" autocomplete="new-password" /></div>
      </div>
      <label class="check-row ${biometricReady ? "" : "disabled"}"><input id="biometricCheck" type="checkbox" ${wizard.biometric && biometricReady ? "checked" : ""} ${biometricReady ? "" : "disabled"} /><span>Face ID / Fingerprint${biometricReady ? "" : " not available on this browser"}</span></label>
      <label class="check-row"><input id="autoLockCheck" type="checkbox" ${wizard.autoLock ? "checked" : ""} /><span>Auto Lock 5 min</span></label>
      <div class="safe-note">Biometric login requires browser/device support. Password encryption is always enabled.</div>
    </div>
    ${wizardFooter("Create Wallet", "finish-wallet", wizard.busy)}`);
}

function wizardHardwareState() {
  const wizard = ensureCreateWizard();
  if (!wizard.hardware) {
    wizard.hardware = {
      transport: "",
      connected: false,
      deviceName: "",
      vendorId: "",
      productId: "",
      openState: "",
      label: "",
      address: "",
      publicKey: "",
    };
  }
  return wizard.hardware;
}

function hardwareId(value) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? `0x${number.toString(16).padStart(4, "0")}` : "-";
}

function renderHardwareConnectStep() {
  const caps = hardwareWalletCapabilities();
  const hardware = wizardHardwareState();
  const connected = !!hardware.connected;
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Hardware Wallet</div>
      <h1>Connect signing device</h1>
      <p class="muted">Use WebHID or WebUSB when your browser supports it. The private key stays inside the hardware wallet.</p>
    </section>
    <div class="hardware-status-grid">
      <div class="card"><div class="label">Secure Context</div><div class="value">${caps.secure ? "Ready" : "HTTPS/localhost required"}</div></div>
      <div class="card"><div class="label">WebHID</div><div class="value">${caps.hid ? "Available" : "Unavailable"}</div></div>
      <div class="card"><div class="label">WebUSB</div><div class="value">${caps.usb ? "Available" : "Unavailable"}</div></div>
    </div>
    <div class="hardware-wallet-card ${connected ? "connected" : ""}">
      <div>
        <span class="label">${connected ? "Connected device" : "No device connected"}</span>
        <strong>${escapeHTML(hardware.deviceName || "Select a hardware wallet")}</strong>
        <em>${connected ? `${escapeHTML(hardware.transport.toUpperCase())} | Vendor ${escapeHTML(hardwareId(hardware.vendorId))} | Product ${escapeHTML(hardwareId(hardware.productId))}` : "Ledger/Trezor-compatible browser transport, or continue to manual address registration."}</em>
      </div>
      <span class="pill ${connected ? "success" : "warn"}">${connected ? "Device selected" : "Waiting"}</span>
    </div>
    <div class="wizard-tools">
      <button type="button" data-action="connect-hardware-hid" ${caps.hid && !wizard.busy ? "" : "disabled"}><i data-lucide="usb"></i>Connect WebHID</button>
      <button type="button" data-action="connect-hardware-usb" ${caps.usb && !wizard.busy ? "" : "disabled"}><i data-lucide="cable"></i>Connect WebUSB</button>
    </div>
    <div class="safe-note"><strong>Hardware signing:</strong> after connecting, export/confirm the MSC address and public key on your device, then register them on the next step.</div>
    ${wizardFooter("Continue")}`);
}

function renderHardwareRegisterStep() {
  const hardware = wizardHardwareState();
  return wizardFrame(`
    <section class="page-hero compact">
      <div class="eyebrow">Register Hardware Wallet</div>
      <h1>Save device address</h1>
      <p class="muted">Paste only the public MSC address and public key exported by the hardware wallet app. Never paste a private key here.</p>
    </section>
    <div class="card">
      <div class="row">
        <div class="field"><label for="hardwareLabel">Device label</label><input id="hardwareLabel" value="${escapeHTML(hardware.label || hardware.deviceName || "MSC Hardware Wallet")}" /></div>
        <div class="field"><label>Transport</label><input readonly value="${escapeHTML(hardware.transport ? hardware.transport.toUpperCase() : "Manual / not connected")}" /></div>
      </div>
      <div class="field"><label for="hardwareAddress">Hardware wallet address</label><input id="hardwareAddress" placeholder="MSC..." autocomplete="off" value="${escapeHTML(hardware.address || "")}" /></div>
      <div class="field"><label for="hardwarePublicKey">Hardware public key</label><input id="hardwarePublicKey" class="mono" placeholder="64-byte public key hex" autocomplete="off" spellcheck="false" value="${escapeHTML(hardware.publicKey || "")}" /></div>
      <div class="safe-note warn"><strong>Private key browser me mat lao.</strong> Hardware wallet ke app/screen se address confirm karo; signing device approval se hoga.</div>
      <button class="primary" type="button" data-action="save-hardware-wallet" ${ensureCreateWizard().busy ? "disabled" : ""}><i data-lucide="shield-check"></i>Save Hardware Wallet</button>
    </div>`);
}

function renderSuccessStep() {
  const wallet = state.wallet || ensureCreateWizard().generated;
  const hardware = isHardwareWallet(wallet);
  return wizardFrame(`
    <section class="wizard-hero success">
      <div class="logo large"><img src="${MSC_LOGO_SRC}" alt="MSC logo" /></div>
      <div>
        <div class="eyebrow">${hardware ? "Hardware Wallet Linked" : "Wallet Created Successfully"}</div>
        <h1>${hardware ? "Your device wallet is ready" : "Your MSC wallet is ready"}</h1>
        <p class="muted">${hardware ? "Receive MSC with this address. Sending requires hardware device approval." : "You can now open the wallet, receive MSC, or bridge/buy when liquidity is available."}</p>
      </div>
    </section>
    <div class="grid">
      <div class="card"><div class="label">Address</div><div class="value mono">${escapeHTML(wallet?.address || "-")}</div></div>
      <div class="card"><div class="label">Network</div><div class="value">MSC Mainnet</div></div>
      <div class="card"><div class="label">Balance</div><div class="value">0 MSC</div></div>
    </div>
    <div class="security-feature-list">
      ${walletSecurityFeatures(wallet?.mnemonic?.words || 12).map((item) => `<span><i data-lucide="check-circle"></i>${escapeHTML(item)}</span>`).join("")}
    </div>
    <div class="wizard-footer end">
      <a class="button primary" href="dashboard.html"><i data-lucide="layout-dashboard"></i>Open Wallet</a>
      <a class="button" href="receive.html"><i data-lucide="qr-code"></i>Receive MSC</a>
      <a class="button" href="bridge.html"><i data-lucide="repeat-2"></i>Buy MSC</a>
    </div>`);
}

function renderCreateWizard() {
  const root = $("walletWizard");
  if (!root) return;
  const wizard = ensureCreateWizard();
  if (wizard.method === "hardware" && wizard.step === 2) {
    root.innerHTML = renderHardwareConnectStep();
    window.lucide?.createIcons();
    return;
  }
  if (wizard.method === "hardware" && wizard.step === 3) {
    root.innerHTML = renderHardwareRegisterStep();
    window.lucide?.createIcons();
    return;
  }
  const views = {
    1: renderCreateWelcome,
    2: renderSecurityNotice,
    3: renderGenerateWalletStep,
    4: renderSeedPhraseStep,
    5: renderVerifySeedStep,
    6: renderPasswordStep,
    7: renderSuccessStep,
  };
  root.innerHTML = (views[wizard.step] || renderCreateWelcome)();
  window.lucide?.createIcons();
}

function hardwareDeviceDetails(transport, device, openState = "") {
  return {
    transport,
    connected: true,
    deviceName: device?.productName || device?.productName === "" ? device.productName || `${transport.toUpperCase()} hardware wallet` : `${transport.toUpperCase()} hardware wallet`,
    vendorId: Number(device?.vendorId || 0),
    productId: Number(device?.productId || 0),
    openState,
    connectedAt: new Date().toISOString(),
  };
}

async function connectHardwareWallet(transport) {
  const wizard = ensureCreateWizard();
  const caps = hardwareWalletCapabilities();
  try {
    if (!caps.secure) throw new Error("Hardware wallet needs HTTPS or localhost.");
    if (transport === "hid" && !caps.hid) throw new Error("WebHID is not available in this browser.");
    if (transport === "usb" && !caps.usb) throw new Error("WebUSB is not available in this browser.");
    wizard.busy = true;
    wizard.status = `Opening ${transport.toUpperCase()} device chooser...`;
    wizard.tone = "warn";
    renderCreateWizard();
    let device = null;
    let openState = "permission granted";
    if (transport === "hid") {
      const devices = await navigator.hid.requestDevice({ filters: HARDWARE_WALLET_HID_FILTERS });
      device = devices?.[0] || null;
      if (!device) throw new Error("No hardware wallet selected.");
      try {
        if (!device.opened) await device.open();
        openState = device.opened ? "open" : "selected";
      } catch (err) {
        openState = err.message || "selected";
      }
    } else {
      device = await navigator.usb.requestDevice({ filters: HARDWARE_WALLET_USB_FILTERS });
      if (!device) throw new Error("No hardware wallet selected.");
      try {
        if (!device.opened) await device.open();
        openState = device.opened ? "open" : "selected";
      } catch (err) {
        openState = err.message || "selected";
      }
    }
    wizard.hardware = { ...wizardHardwareState(), ...hardwareDeviceDetails(transport, device, openState) };
    wizard.status = "Hardware wallet selected. Confirm/export address on device, then continue.";
    wizard.tone = "success";
  } catch (err) {
    wizard.status = err.message || "Hardware wallet connection failed.";
    wizard.tone = "error";
  } finally {
    wizard.busy = false;
    renderCreateWizard();
  }
}

async function saveHardwareWizardWallet() {
  const wizard = ensureCreateWizard();
  const hardware = wizardHardwareState();
  try {
    const label = $("hardwareLabel")?.value.trim() || hardware.deviceName || "MSC Hardware Wallet";
    const address = normalizeMSCAddress($("hardwareAddress")?.value || "");
    const publicKey = String($("hardwarePublicKey")?.value || "").trim().toLowerCase();
    hardware.label = label;
    hardware.address = $("hardwareAddress")?.value || "";
    hardware.publicKey = publicKey;
    if (!address) throw new Error("Enter a valid MSC hardware wallet address.");
    if (!/^[0-9a-f]{64}$/.test(publicKey)) throw new Error("Enter a 32-byte public key as 64 hex characters.");
    const derived = await addressFromPublicKey(hexToBytes(publicKey));
    if (derived !== address) throw new Error("Public key does not match the MSC address.");
    wizard.busy = true;
    wizard.status = "Saving hardware wallet profile...";
    wizard.tone = "warn";
    renderCreateWizard();
    const wallet = {
      type: "hardware",
      address,
      publicKey,
      hardware: {
        mode: "hardware",
        label,
        transport: hardware.transport || "manual",
        deviceName: hardware.deviceName || label,
        vendorId: hardware.vendorId || "",
        productId: hardware.productId || "",
        openState: hardware.openState || "",
        connectedAt: hardware.connectedAt || "",
        registeredAt: new Date().toISOString(),
      },
      preferences: {
        autoLockMs: DEFAULT_AUTO_LOCK_MS,
        hardwareSigning: true,
      },
      securityFeatures: uniqueList([...walletSecurityFeatures(0), "Hardware Signing Required"]),
      createdAt: new Date().toISOString(),
    };
    saveWallet(wallet);
    state.secretKey = null;
    sessionStorage.removeItem(SESSION_KEY);
    wizard.step = 7;
    wizard.status = "";
    wizard.tone = "";
    updateWalletUI();
  } catch (err) {
    wizard.status = err.message || "Hardware wallet save failed.";
    wizard.tone = "error";
  } finally {
    wizard.busy = false;
    renderCreateWizard();
  }
}

async function generateWizardWallet() {
  const wizard = ensureCreateWizard();
  try {
    wizard.busy = true;
    wizard.status = "Generating wallet keys...";
    wizard.tone = "";
    renderCreateWizard();
    const words = randomMnemonic(wizard.wordCount);
    const generated = await walletFromMnemonic(words);
    wizard.words = words;
    wizard.generated = generated;
    wizard.verifyPositions = randomSeedVerifyPositions(words.length);
    wizard.verification = {};
    wizard.seedVerified = false;
    wizard.status = "Wallet keys generated on this device.";
    wizard.tone = "success";
  } catch (err) {
    wizard.status = err.message || "Wallet generation failed";
    wizard.tone = "error";
  } finally {
    wizard.busy = false;
    renderCreateWizard();
  }
}

function verifyWizardSeed() {
  const wizard = ensureCreateWizard();
  for (const position of wizardVerifyPositions()) {
    const expected = wizard.words[position - 1];
    const actual = normalizeMnemonic(wizard.verification[position] || "")[0] || "";
    if (actual !== expected) {
      setWizardStatus(`Word #${position} does not match.`, "error");
      return false;
    }
  }
  wizard.seedVerified = true;
  return true;
}

function pdfTextEscape(value) {
  return String(value || "").replace(/\\/g, "\\\\").replace(/\(/g, "\\(").replace(/\)/g, "\\)");
}

function downloadSeedPDF(words, address) {
  const lines = [
    "MSC Wallet Seed Phrase",
    `Address: ${address || "-"}`,
    "",
    ...words.map((word, index) => `${index + 1}. ${word}`),
    "",
    "Keep this phrase offline. Never share it with anyone.",
  ];
  const stream = `BT\n/F1 18 Tf\n72 760 Td\n22 TL\n${lines.map((line) => `(${pdfTextEscape(line)}) Tj T*`).join("\n")}\nET`;
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(pdf.length);
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = pdf.length;
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${offsets.slice(1).map((offset) => `${String(offset).padStart(10, "0")} 00000 n `).join("\n")}\n`;
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;
  const blob = new Blob([pdf], { type: "application/pdf" });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = "msc-wallet-seed.pdf";
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(link.href), 1000);
}

function printSeedPhrase(words, address) {
  const popup = window.open("", "_blank", "noopener,noreferrer");
  if (!popup) return false;
  popup.document.write(`
    <!doctype html><html><head><title>MSC Wallet Seed Phrase</title>
    <style>body{font-family:Arial,sans-serif;padding:32px;color:#111}ol{columns:2;font-size:18px;line-height:1.8}.mono{font-family:monospace;word-break:break-all}.warn{border:1px solid #999;padding:12px;margin-top:20px}</style>
    </head><body><h1>MSC Wallet Seed Phrase</h1><p class="mono">${escapeHTML(address || "-")}</p><ol>${words.map((word) => `<li>${escapeHTML(word)}</li>`).join("")}</ol><div class="warn">Keep this phrase offline. Never share it with anyone.</div><script>window.print();</script></body></html>`);
  popup.document.close();
  return true;
}

async function finishWizardWallet() {
  const wizard = ensureCreateWizard();
  try {
    if (!wizard.generated?.keyPair || !wizard.words.length) throw new Error("Generate wallet first.");
    if (!wizard.seedVerified) throw new Error("Seed backup verification is required before creating the wallet.");
    const password = $("wizardPassword")?.value || "";
    const confirm = $("wizardPasswordConfirm")?.value || "";
    if (password.length < 8) throw new Error("Use at least 8 characters.");
    if (password !== confirm) throw new Error("Passwords do not match.");
    wizard.busy = true;
    wizard.status = "Encrypting wallet...";
    wizard.tone = "";
    renderCreateWizard();
    const wallet = {
      address: wizard.generated.address,
      publicKey: wizard.generated.publicKey,
      crypto: await encryptSecretKey(wizard.generated.keyPair.secretKey, password),
      mnemonicCrypto: await encryptSecretKey(enc.encode(wizard.words.join(" ")), password),
      mnemonic: {
        type: "msc_mnemonic_v1",
        words: wizard.words.length,
      },
      preferences: {
        biometric: !!wizard.biometric && biometricSupported(),
        autoLockMs: wizard.autoLock ? DEFAULT_AUTO_LOCK_MS : 0,
      },
      securityFeatures: walletSecurityFeatures(wizard.words.length),
      createdAt: new Date().toISOString(),
    };
    saveWallet(wallet);
    state.secretKey = wizard.generated.keyPair.secretKey;
    saveSessionUnlock(state.secretKey, wallet);
    wizard.step = 7;
    wizard.status = "";
    wizard.tone = "";
    updateWalletUI();
  } catch (err) {
    wizard.status = err.message || "Wallet setup failed";
    wizard.tone = "error";
  } finally {
    wizard.busy = false;
    renderCreateWizard();
  }
}

function handleWizardContinue() {
  const wizard = ensureCreateWizard();
  wizard.status = "";
  wizard.tone = "";
  if (wizard.step === 1) {
    if (wizard.method === "import") {
      window.location.href = "login.html";
      return;
    }
    if (wizard.method === "hardware") {
      wizard.step = 2;
    } else {
      wizard.step = 2;
    }
  } else if (wizard.method === "hardware" && wizard.step === 2) {
    wizard.step = 3;
  } else if (wizard.method === "hardware" && wizard.step === 3) {
    setWizardStatus("Save the hardware wallet address and public key to finish.", "warn");
    return;
  } else if (wizard.step === 2) {
    if (!wizard.securityAccepted) {
      setWizardStatus("Please confirm that you understand the risks.", "error");
      return;
    }
    wizard.step = 3;
  } else if (wizard.step === 3) {
    if (!wizard.generated) {
      setWizardStatus("Generate wallet keys first.", "error");
      return;
    }
    wizard.step = 4;
  } else if (wizard.step === 4) {
    wizard.step = 5;
  } else if (wizard.step === 5) {
    if (!verifyWizardSeed()) return;
    wizard.step = 6;
  }
  renderCreateWizard();
}

function handleWizardClick(event) {
  const root = $("walletWizard");
  if (!root?.contains(event.target)) return;
  const methodButton = event.target.closest("[data-method]");
  const actionButton = event.target.closest("[data-action]");
  if (methodButton) {
    const wizard = ensureCreateWizard();
    wizard.method = methodButton.dataset.method || "create";
    wizard.status = "";
    wizard.tone = "";
    renderCreateWizard();
    return;
  }
  if (!actionButton) return;
  event.preventDefault();
  const wizard = ensureCreateWizard();
  const action = actionButton.dataset.action;
  if (action === "wizard-back") {
    wizard.step = Math.max(1, wizard.step - 1);
    wizard.status = "";
    wizard.tone = "";
    renderCreateWizard();
  } else if (action === "wizard-continue") {
    handleWizardContinue();
  } else if (action === "generate-wallet") {
    generateWizardWallet();
  } else if (action === "connect-hardware-hid") {
    connectHardwareWallet("hid");
  } else if (action === "connect-hardware-usb") {
    connectHardwareWallet("usb");
  } else if (action === "save-hardware-wallet") {
    saveHardwareWizardWallet();
  } else if (action === "copy-seed") {
    copyText(wizard.words.join(" ")).then(() => setWizardStatus("Seed phrase copied. Clipboard clear kar dein after saving offline.", "success")).catch(() => setWizardStatus("Copy failed.", "error"));
  } else if (action === "copy-seed-word") {
    const index = Number(actionButton.dataset.wordIndex);
    const word = wizard.words[index] || "";
    copyText(word).then(() => setWizardStatus(`Word #${index + 1} copied. Clipboard clear kar dein.`, "success")).catch(() => setWizardStatus("Copy failed.", "error"));
  } else if (action === "download-seed") {
    downloadSeedPDF(wizard.words, wizard.generated?.address);
    setWizardStatus("Seed PDF downloaded. Keep it private.", "success");
  } else if (action === "print-seed") {
    const printed = printSeedPhrase(wizard.words, wizard.generated?.address);
    setWizardStatus(printed ? "Print window opened." : "Pop-up blocked. Allow pop-ups to print.", printed ? "success" : "error");
  } else if (action === "finish-wallet") {
    finishWizardWallet();
  }
}

function handleWizardChange(event) {
  const root = $("walletWizard");
  if (!root?.contains(event.target)) return;
  const wizard = ensureCreateWizard();
  if (event.target.id === "riskCheck") {
    wizard.securityAccepted = !!event.target.checked;
    renderCreateWizard();
  }
  if (event.target.id === "biometricCheck") wizard.biometric = biometricSupported() && !!event.target.checked;
  if (event.target.id === "autoLockCheck") wizard.autoLock = !!event.target.checked;
}

function handleWizardInput(event) {
  const position = event.target?.dataset?.verifyWord;
  const wizard = ensureCreateWizard();
  if (position) {
    wizard.verification[position] = event.target.value;
    wizard.seedVerified = false;
  }
  if (event.target?.id === "wizardPassword") {
    renderPasswordStrength("wizardPassword", "wizardPasswordStrength");
  }
  if (event.target?.id === "hardwareLabel") wizardHardwareState().label = event.target.value;
  if (event.target?.id === "hardwareAddress") wizardHardwareState().address = event.target.value;
  if (event.target?.id === "hardwarePublicKey") wizardHardwareState().publicKey = event.target.value;
}

function initCreateWizard() {
  const root = $("walletWizard");
  if (!root) return;
  ensureCreateWizard();
  root.addEventListener("click", handleWizardClick);
  root.addEventListener("change", handleWizardChange);
  root.addEventListener("input", handleWizardInput);
  renderCreateWizard();
}

function initRecoveryUI() {
  if (!$("recoveryMethodTabs")) return;
  $("recoveryMethodTabs").addEventListener("click", (event) => {
    const button = event.target.closest("[data-recovery-method]");
    if (!button) return;
    setRecoveryMethod(button.dataset.recoveryMethod);
  });
  $("recoverSeedPhrase")?.addEventListener("input", () => {
    renderSeedDiagnostics("recoverSeedPhrase", "recoverSeedCounter", "recoverSeedWordChips");
    clearRecoveryPreview("seedRecoveryPreview", "seedRecoveryConfirm");
  });
  $("recoverSeedPassword")?.addEventListener("input", () => {
    renderPasswordStrength("recoverSeedPassword", "recoverSeedPasswordStrength");
    clearRecoveryPreview("seedRecoveryPreview", "seedRecoveryConfirm");
  });
  $("recoverSeedPasswordConfirm")?.addEventListener("input", () => clearRecoveryPreview("seedRecoveryPreview", "seedRecoveryConfirm"));
  $("recoveryKitFile")?.addEventListener("change", renderRecoveryKitFingerprint);
  $("recoveryKitPassword")?.addEventListener("input", () => clearRecoveryPreview("recoveryKitPreview", "recoveryKitConfirm"));
  $("importPrivateKey")?.addEventListener("input", () => clearRecoveryPreview("privateKeyRecoveryPreview", "privateKeyRecoveryConfirm"));
  $("importPassword")?.addEventListener("input", () => {
    renderPasswordStrength("importPassword", "importPasswordStrength");
    clearRecoveryPreview("privateKeyRecoveryPreview", "privateKeyRecoveryConfirm");
  });
  renderSeedDiagnostics("recoverSeedPhrase", "recoverSeedCounter", "recoverSeedWordChips");
  renderPasswordStrength("recoverSeedPassword", "recoverSeedPasswordStrength");
  renderPasswordStrength("importPassword", "importPasswordStrength");
  setRecoveryMethod(state.recoveryMethod || "seed");
  renderRecoveryKitFingerprint();
}

function loadAddressBook() {
  try {
    const items = JSON.parse(localStorage.getItem(ADDRESS_BOOK_KEY) || "[]");
    return Array.isArray(items) ? items.filter((item) => item?.address) : [];
  } catch (_) {
    return [];
  }
}

function saveAddressBook(items) {
  localStorage.setItem(ADDRESS_BOOK_KEY, JSON.stringify(items.slice(0, 200)));
}

function renderAddressBook() {
  const list = $("addressBookList");
  if (!list) return;
  const items = loadAddressBook();
  if (!items.length) {
    list.innerHTML = `<div class="list-item">No saved contacts yet.</div>`;
    return;
  }
  list.innerHTML = items.map((item, index) => `
    <div class="address-book-row">
      <div>
        <strong>${escapeHTML(item.name || "MSC Contact")}</strong>
        <span class="mono">${escapeHTML(item.address)}</span>
      </div>
      <div class="address-book-actions">
        <button type="button" data-copy-contact="${index}"><i data-lucide="copy"></i>Copy</button>
        <button type="button" data-delete-contact="${index}"><i data-lucide="trash-2"></i>Delete</button>
      </div>
    </div>`).join("");
  window.lucide?.createIcons();
}

function addAddressBookContact(event) {
  event.preventDefault();
  const name = $("contactName")?.value.trim() || "";
  const address = $("contactAddress")?.value.trim() || "";
  if (!address || !/^MSC[0-9a-fA-F]{42}$/.test(address)) {
    setStatus("addressBookStatus", "Enter a valid MSC address.", "error");
    return;
  }
  const items = loadAddressBook().filter((item) => item.address !== address);
  items.unshift({ name: name || "MSC Contact", address, addedAt: new Date().toISOString() });
  saveAddressBook(items);
  setValue("contactName", "");
  setValue("contactAddress", "");
  setStatus("addressBookStatus", "Contact saved", "success");
  renderAddressBook();
}

function handleAddressBookClick(event) {
  const copyButton = event.target.closest("[data-copy-contact]");
  const deleteButton = event.target.closest("[data-delete-contact]");
  if (!copyButton && !deleteButton) return;
  const items = loadAddressBook();
  if (copyButton) {
    const item = items[Number(copyButton.dataset.copyContact)];
    copyText(item?.address || "").then(() => setStatus("addressBookStatus", "Address copied", "success")).catch(() => setStatus("addressBookStatus", "Copy failed", "error"));
    return;
  }
  const index = Number(deleteButton.dataset.deleteContact);
  if (Number.isInteger(index)) {
    items.splice(index, 1);
    saveAddressBook(items);
    setStatus("addressBookStatus", "Contact deleted", "warn");
    renderAddressBook();
  }
}

function formatNumber(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return value === 0 ? "0" : "-";
  return n.toLocaleString();
}

function formatDateTime(value) {
  const date = new Date(Number(value || 0));
  if (!Number.isFinite(date.getTime()) || date.getTime() <= 0) return "-";
  return date.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

function formatAge(seconds) {
  const n = Number(seconds);
  if (!Number.isFinite(n) || n < 0) return "-";
  if (n < 60) return `${Math.trunc(n)}s`;
  const mins = Math.floor(n / 60);
  const secs = Math.trunc(n % 60);
  if (mins < 60) return secs ? `${mins}m ${secs}s` : `${mins}m`;
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  return remMins ? `${hours}h ${remMins}m` : `${hours}h`;
}

function formatDuration(ms) {
  const total = Math.max(0, Math.ceil(Number(ms || 0) / 1000));
  const hours = Math.floor(total / 3600);
  const mins = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hours > 0) return `${hours}h ${mins}m`;
  if (mins > 0) return `${mins}m ${secs}s`;
  return `${secs}s`;
}

function formatBlocks(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) return "-";
  const blocks = Math.trunc(n);
  return `${blocks} block${blocks === 1 ? "" : "s"}`;
}

function formatLatency(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) return "-";
  return `${Math.round(n)}ms`;
}

function clampChart(value, min = 0, max = 100) {
  return Math.max(min, Math.min(max, Number(value) || 0));
}

function percentChart(value, total) {
  const n = Number(value);
  const d = Number(total);
  if (!Number.isFinite(n) || !Number.isFinite(d) || d <= 0) return 0;
  return clampChart((n / d) * 100);
}

function chartCard(id, title, meta, body, chip = "") {
  const node = $(id);
  if (!node) return;
  if (!body) {
    node.classList.add("empty");
    node.innerHTML = `<div>No chart data available</div>`;
    return;
  }
  node.classList.remove("empty");
  node.innerHTML = `
    <div class="chart-head">
      <div class="chart-title"><strong>${escapeHTML(title)}</strong><span>${escapeHTML(meta)}</span></div>
      ${chip ? `<span class="pill">${escapeHTML(chip)}</span>` : ""}
    </div>
    ${body}`;
}

function donutHTML(value, label, detail, tone = "success") {
  const color = tone === "error" ? "var(--coral)" : tone === "warn" ? "var(--amber)" : "var(--mint)";
  const safe = clampChart(value);
  return `<div class="chart-donut">
    <div class="donut-ring" style="--donut-value:${safe};--donut-color:${color};"></div>
    <div class="donut-copy"><div class="donut-value">${Math.round(safe)}%</div><div class="donut-label">${escapeHTML(label)}<br>${escapeHTML(detail)}</div></div>
  </div>`;
}

function barsHTML(items) {
  const rows = (items || []).filter(Boolean);
  if (!rows.length) return "";
  const max = Math.max(...rows.map((item) => Number(item.value) || 0), 1);
  return `<div class="chart-bars">${rows.map((item) => {
    const height = clampChart(((Number(item.value) || 0) / max) * 100, 4, 100);
    return `<span class="chart-bar" title="${escapeHTML(item.label)}: ${escapeHTML(item.display ?? item.value)}"><span class="bar-fill" style="--bar-height:${height}%;"></span></span>`;
  }).join("")}</div>`;
}

function rowsHTML(items) {
  const rows = (items || []).filter(Boolean);
  if (!rows.length) return "";
  const max = Math.max(...rows.map((item) => Number(item.value) || 0), 1);
  return `<div class="chart-list">${rows.map((item) => {
    const width = clampChart(((Number(item.value) || 0) / max) * 100);
    const color = item.tone === "error" ? "var(--coral)" : item.tone === "warn" ? "var(--amber)" : item.color || "var(--mint)";
    return `<div class="chart-row">
      <span>${escapeHTML(item.label)}</span>
      <span class="chart-track"><span style="--track-value:${width}%;--track-color:${color};"></span></span>
      <strong class="mono">${escapeHTML(item.display ?? item.value)}</strong>
    </div>`;
  }).join("")}</div>`;
}

function matrixHTML(items) {
  const rows = (items || []).filter(Boolean);
  if (!rows.length) return "";
  return `<div class="matrix-grid">${rows.map((item) => `
    <div class="matrix-cell"><span>${escapeHTML(item.label)}</span><strong>${escapeHTML(item.value)}</strong></div>
  `).join("")}</div>`;
}

function pillHTML(label, id, fallback = "-") {
  return `<span class="pill-label">${label}</span><strong id="${id}">${fallback}</strong>`;
}

function publicNodeKey(node) {
  return String(node?.id || node?.target || node?.rpc_url || node?.rpc || "-").trim();
}

function loadUptimeCache() {
  try {
    return JSON.parse(localStorage.getItem(UPTIME_CACHE_KEY) || "{}") || {};
  } catch (_) {
    return {};
  }
}

function saveUptimeCache(cache) {
  try {
    localStorage.setItem(UPTIME_CACHE_KEY, JSON.stringify(cache));
  } catch (_) {
    // Uptime is display-only; storage pressure must not break the wallet.
  }
}

function recordPublicNodeUptime(nodes) {
  if (!Array.isArray(nodes) || nodes.length === 0) return;
  const now = Date.now();
  const cache = loadUptimeCache();
  for (const node of nodes) {
    const key = publicNodeKey(node);
    if (!key || key === "-") continue;
    const samples = Array.isArray(cache[key]) ? cache[key] : [];
    samples.push({ t: now, h: !!node.healthy });
    cache[key] = samples.slice(-UPTIME_MAX_SAMPLES);
  }
  saveUptimeCache(cache);
}

function publicNodeUptimePct(node) {
  const samples = loadUptimeCache()[publicNodeKey(node)] || [];
  if (!samples.length) return "-";
  const healthy = samples.filter((sample) => !!sample.h).length;
  return `${Math.round((healthy / samples.length) * 100)}%`;
}

function publicNodeDisplayAgeSeconds(node) {
  const base = Number(node?.last_block_age_seconds);
  if (!Number.isFinite(base) || base < 0) return null;
  const checked = Number(node?.last_checked || 0);
  const checkedMs = checked > 0 ? (checked < 1e12 ? checked * 1000 : checked) : 0;
  const elapsed = checkedMs > 0 ? Math.max(0, Math.floor((Date.now() - checkedMs) / 1000)) : 0;
  return Math.trunc(base) + elapsed;
}

function publicNodeHeightLag(node, bestHeight) {
  const explicit = Number(node?.height_lag_blocks);
  if (Number.isFinite(explicit) && explicit >= 0) return Math.trunc(explicit);
  const height = Number(node?.height || 0);
  if (!Number.isFinite(bestHeight) || bestHeight <= 0 || !Number.isFinite(height) || height <= 0) return 0;
  return Math.max(0, Math.trunc(bestHeight - height));
}

function publicNodeTone(node, bestHeight) {
  const healthState = String(node?.health_state || "").toLowerCase();
  if (healthState === "unhealthy") return "error";
  if (healthState === "warning") return "warn";
  if (!node?.healthy || node?.suspicious_reason) return "error";
  const heightLag = publicNodeHeightLag(node, bestHeight);
  const finalityLag = Number(node?.finality_lag || 0);
  const age = publicNodeDisplayAgeSeconds(node);
  const cmd = String(node?.consensus_mode || "").toUpperCase();
  if (heightLag > 20 || finalityLag > 20 || (age !== null && age > 60) || ["EMERGENCY", "HALTED", "ATTACK", "PARTITION"].includes(cmd)) {
    return "error";
  }
  if (heightLag > 2 || finalityLag > 2 || (age !== null && age >= 12) || ["STRICT", "RECOVERY", "DEGRADED"].includes(cmd)) {
    return "warn";
  }
  return "success";
}

function currentLastBlockAge() {
  const base = Number(state.realtime.lastBlockAgeBaseSeconds);
  if (!Number.isFinite(base) || base < 0) return null;
  const updatedAt = Number(state.realtime.lastBlockAgeUpdatedAt || 0);
  const elapsed = updatedAt > 0 ? Math.max(0, Math.floor((Date.now() - updatedAt) / 1000)) : 0;
  return Math.trunc(base) + elapsed;
}

function blockAgeTone(seconds) {
  const age = Number(seconds);
  if (!Number.isFinite(age) || age < 0) return "";
  if (age >= 15) return "error";
  if (age >= 10) return "warn";
  return "success";
}

function setLastBlockAgeBase(seconds) {
  const age = Number(seconds);
  if (!Number.isFinite(age) || age < 0) return;
  state.realtime.lastBlockAgeBaseSeconds = Math.trunc(age);
  state.realtime.lastBlockAgeUpdatedAt = Date.now();
  renderLastBlockAge();
}

function renderLastBlockAge() {
  const age = currentLastBlockAge();
  setText("topLastBlockAge", formatAge(age));
  setTone("topLastBlockAge", blockAgeTone(age));
}

function renderEventDelay() {
  const delay = Number(state.realtime.eventDelayMs);
  setText("topEventDelay", Number.isFinite(delay) && delay >= 0 ? formatLatency(delay) : "-");
  setText("settingsEventDelay", Number.isFinite(delay) && delay >= 0 ? formatLatency(delay) : "-");
}

function startBlockAgeTicker() {
  if (state.blockAgeTimer) return;
  state.blockAgeTimer = window.setInterval(() => {
    renderLastBlockAge();
    renderFaucetState();
    renderEventDelay();
    renderPublicNodesRegistry(state.publicNodesRegistry);
  }, 1000);
}

function stripHTML(value) {
  return String(value || "").replace(/<!--[\s\S]*?-->/g, " ").replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

function unwrapV1(payload) {
  return payload && typeof payload === "object" && Object.prototype.hasOwnProperty.call(payload, "success")
    ? payload.data
    : payload;
}

function unwrapDataEnvelope(payload) {
  if (!payload || typeof payload !== "object") return payload;
  if (Object.prototype.hasOwnProperty.call(payload, "success")) return payload.data;
  const keys = Object.keys(payload);
  if (Object.prototype.hasOwnProperty.call(payload, "data") && keys.every((key) => ["data", "ts", "timestamp"].includes(key))) {
    return payload.data;
  }
  return payload;
}

class WalletDataCache {
  constructor(storageKey) {
    this.storageKey = storageKey;
    this.items = {};
    try {
      this.items = JSON.parse(localStorage.getItem(storageKey) || "{}") || {};
    } catch (_) {
      this.items = {};
    }
  }

  get(key, ttlMs = 0) {
    const item = this.items[key];
    if (!item) return null;
    const age = Date.now() - Number(item.ts || 0);
    return {
      data: item.data,
      ts: item.ts,
      age,
      fresh: ttlMs <= 0 || age <= ttlMs,
    };
  }

  set(key, data) {
    this.items[key] = { data, ts: Date.now() };
    this.persist();
  }

  remove(key) {
    if (!Object.prototype.hasOwnProperty.call(this.items, key)) return;
    delete this.items[key];
    this.persist();
  }

  persist() {
    try {
      const entries = Object.entries(this.items)
        .sort((a, b) => Number(b[1]?.ts || 0) - Number(a[1]?.ts || 0))
        .slice(0, 80);
      localStorage.setItem(this.storageKey, JSON.stringify(Object.fromEntries(entries)));
    } catch (_) {
      // Cache is an optimization; quota failures must not break wallet usage.
    }
  }
}

function stableStringify(value) {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(",")}}`;
}

function quorumKey(path, data) {
  if (path.startsWith("/balance")) {
    return stableStringify({ balance: data?.balance ?? data?.Balance ?? null, coin: data?.coin ?? data?.Coin ?? "MSC" });
  }
  if (path.startsWith("/wallet/status")) {
    return stableStringify({
      stake: data?.stake ?? 0,
      rewards: data?.rewards ?? data?.reward_balance ?? 0,
      validator_id: data?.validator_id || "",
      status: data?.status || "",
      locked_until_epoch: data?.locked_until_epoch ?? 0,
    });
  }
  return stableStringify(data);
}

function isBalanceReadPath(path) {
  return String(path || "").startsWith("/balance");
}

function balanceResponseHeight(data) {
  const value = Number(data?.height ?? data?.Height ?? data?.finalized_height ?? data?.FinalizedHeight ?? 0);
  return Number.isFinite(value) && value > 0 ? value : 0;
}

function balanceResponseAmount(data) {
  const value = Number(data?.balance ?? data?.Balance);
  return Number.isFinite(value) ? value : null;
}

function balanceVerificationRank(verification) {
  const mode = verification?.mode || "";
  if (mode === "light" || mode === "quorum") return 3;
  if (mode === "freshest") return 2;
  if (mode === "unverified") return 1;
  return 0;
}

function balanceDataAtFinalizedHeight(data, height) {
  const out = { ...(data || {}) };
  if (height > 0) {
    out.height = height;
    out.Height = height;
    out.finalized_height = height;
    out.FinalizedHeight = height;
  }
  return out;
}

function verificationForRead(path, item, mode, matches, checked, staleCount = 0) {
  const verification = {
    mode,
    rpc: item?.rpc,
    matches,
    checked,
  };
  if (isBalanceReadPath(path)) {
    const height = balanceResponseHeight(item?.data);
    if (height) verification.height = height;
    if (staleCount > 0) verification.stale = staleCount;
  }
  return verification;
}

function groupedReadMajority(path, items) {
  const groups = new Map();
  items.forEach((item) => {
    const key = quorumKey(path, item.data);
    const group = groups.get(key) || [];
    group.push(item);
    groups.set(key, group);
  });
  return Array.from(groups.values()).sort((a, b) => b.length - a.length)[0] || [];
}

function balanceStableFloorSelection(path, successes) {
  const groups = new Map();
  successes.forEach((item) => {
    const key = quorumKey(path, item.data);
    const group = groups.get(key) || [];
    group.push(item);
    groups.set(key, group);
  });
  const candidates = Array.from(groups.values())
    .filter((group) => group.length >= 2)
    .map((group) => {
      const heights = group.map((item) => balanceResponseHeight(item.data)).filter((height) => height > 0);
      const finalizedHeight = heights.length ? Math.min(...heights) : 0;
      return {
        group,
        finalizedHeight,
        source: group.find((item) => balanceResponseHeight(item.data) === finalizedHeight) || group[0],
      };
    })
    .filter((item) => item.finalizedHeight > 0)
    .sort((a, b) => {
      if (b.finalizedHeight !== a.finalizedHeight) return b.finalizedHeight - a.finalizedHeight;
      return b.group.length - a.group.length;
    });
  return candidates[0] || null;
}

function balanceQuorumSelection(path, successes) {
  if (!isBalanceReadPath(path)) return null;
  if (successes.length === 1) {
    return {
      data: successes[0].data,
      verification: verificationForRead(path, successes[0], "unverified", 1, 1),
      majority: [successes[0]],
      comparable: [successes[0]],
    };
  }
  const byHeight = new Map();
  successes.forEach((item) => {
    const height = balanceResponseHeight(item.data);
    const group = byHeight.get(height) || [];
    group.push(item);
    byHeight.set(height, group);
  });
  const heights = Array.from(byHeight.keys()).sort((a, b) => b - a);
  for (const height of heights) {
    const comparable = byHeight.get(height) || [];
    const majority = groupedReadMajority(path, comparable);
    if (majority.length >= 2) {
      return {
        data: majority[0].data,
        verification: verificationForRead(path, majority[0], "quorum", majority.length, comparable.length, successes.length - comparable.length),
        majority,
        comparable,
      };
    }
  }
  const stableFloor = balanceStableFloorSelection(path, successes);
  if (stableFloor) {
    return {
      data: balanceDataAtFinalizedHeight(stableFloor.source.data, stableFloor.finalizedHeight),
      verification: {
        mode: "quorum",
        rpc: stableFloor.source.rpc,
        matches: stableFloor.group.length,
        checked: stableFloor.group.length,
        height: stableFloor.finalizedHeight,
        stale: successes.length - stableFloor.group.length,
      },
      majority: stableFloor.group,
      comparable: stableFloor.group,
    };
  }
  const topHeight = heights[0] || 0;
  const comparable = byHeight.get(topHeight) || successes;
  return {
    data: null,
    verification: {
      mode: "mismatch",
      matches: 0,
      checked: comparable.length,
      height: topHeight || undefined,
      stale: successes.length - comparable.length,
    },
    majority: [],
    comparable,
  };
}

function balanceVerificationCanDisplay(verification, incomingHeight, incomingAmount = null) {
  const mode = verification?.mode || "";
  const currentHeight = Number(state.balanceHeight || 0);
  const currentAmount = balanceResponseAmount({ balance: state.balanceAmount });
  const incomingRank = balanceVerificationRank(verification);
  const currentRank = balanceVerificationRank(state.balanceVerification);
  if (currentHeight > 0) {
    if (!incomingHeight || incomingHeight < currentHeight) return false;
    if (
      incomingHeight === currentHeight &&
      incomingAmount !== null &&
      currentAmount !== null &&
      Number(incomingAmount) !== Number(currentAmount) &&
      !(incomingRank > currentRank && incomingRank >= 3)
    ) {
      return false;
    }
  }
  if (mode === "light" || mode === "quorum") return true;
  if (mode === "unverified") return currentHeight <= 0 && Number(verification?.checked || 0) <= 1;
  if (mode === "freshest") return currentHeight <= 0 && Number(verification?.checked || 0) <= 1 && incomingHeight > 0;
  return false;
}

function balanceResultCacheable(result) {
  if (!result?.data) return false;
  const height = balanceResponseHeight(result.data);
  if (!height) return false;
  return balanceVerificationCanDisplay(result.verification, height, balanceResponseAmount(result.data));
}

function balanceHeightCacheKey(address, height) {
  return cacheKey("balance", `${address}:${height}`);
}

function balanceLatestCacheKey(address) {
  return cacheKey("balance-latest", address);
}

function loadCachedBalanceResult(address, ttl) {
  if (!address || !state.dataCache) return null;
  const latest = state.dataCache.get(balanceLatestCacheKey(address), ttl);
  const latestHeight = Number(latest?.data?.height || 0);
  const latestKey = latest?.data?.key || (latestHeight ? balanceHeightCacheKey(address, latestHeight) : "");
  if (!latestKey) return null;
  const cached = state.dataCache.get(latestKey, ttl);
  const payload = cached?.data;
  if (!payload?.data) return null;
  return {
    data: payload.data,
    verification: payload.verification || null,
    ts: cached.ts,
    age: cached.age,
    fresh: cached.fresh,
    fromCache: true,
  };
}

function storeCachedBalanceResult(address, result) {
  if (!address || !state.dataCache || !balanceResultCacheable(result)) return;
  const height = balanceResponseHeight(result.data);
  const latest = state.dataCache.get(balanceLatestCacheKey(address), 0);
  const latestHeight = Number(latest?.data?.height || 0);
  if (latestHeight > height) return;
  const key = balanceHeightCacheKey(address, height);
  state.dataCache.set(key, { data: result.data, verification: result.verification || null });
  state.dataCache.set(balanceLatestCacheKey(address), { height, key });
}

async function cachedBalanceAPI(address, path, { ttl = 0, force = false, cacheOnly = false } = {}) {
  const cached = loadCachedBalanceResult(address, ttl);
  if (cacheOnly) return cached;
  try {
    const result = await quorumApi(path);
    storeCachedBalanceResult(address, result);
    return { data: result.data, verification: result.verification, fromCache: false, fresh: true, age: 0 };
  } catch (err) {
    if (!force && cached) return cached;
    throw err;
  }
}

function normalizeHexHash(value) {
  const clean = String(value || "").trim().toLowerCase();
  return /^[0-9a-f]{64}$/.test(clean) ? clean : "";
}

async function lightHashString(value) {
  return bytesToHex(await sha256(enc.encode(String(value ?? ""))));
}

async function verifyLightMerkleProof(proof) {
  const root = normalizeHexHash(proof?.root);
  let current = normalizeHexHash(proof?.leaf_hash);
  const leafValue = proof?.leaf_value;
  const totalLeaves = Number(proof?.total_leaves || 0);
  const leafIndex = Number(proof?.leaf_index ?? -1);
  if (!root || !current || totalLeaves <= 0 || leafIndex < 0 || leafIndex >= totalLeaves) return false;
  if (leafValue !== undefined && leafValue !== null && String(leafValue) !== "") {
    const expectedLeaf = await lightHashString(leafValue);
    if (expectedLeaf !== current) return false;
  }
  for (const sibling of proof?.siblings || []) {
    const siblingHash = normalizeHexHash(sibling?.hash);
    const position = String(sibling?.position || "").toLowerCase();
    if (!siblingHash || (position !== "left" && position !== "right")) return false;
    current = position === "left"
      ? await lightHashString(`${siblingHash}${current}`)
      : await lightHashString(`${current}${siblingHash}`);
  }
  return current === root;
}

async function verifyLightProofResponse(response, expectedRootField, expectedValue = null) {
  const payload = unwrapV1(response);
  const proof = payload?.proof;
  const header = payload?.header;
  if (!proof || !header) return null;
  const proofRoot = normalizeHexHash(proof.root);
  const headerRoot = normalizeHexHash(header?.[expectedRootField]);
  if (!proofRoot || !headerRoot || proofRoot !== headerRoot) return null;
  if (!(await verifyLightMerkleProof(proof))) return null;
  if (expectedValue && typeof expectedValue === "object") {
    const expectedBalance = balanceResponseAmount(expectedValue);
    const proofBalance = balanceResponseAmount(payload?.value);
    if (expectedBalance !== null && proofBalance !== null && proofBalance !== expectedBalance) return null;
    const expectedHeight = balanceResponseHeight(expectedValue);
    const proofHeight = Number(header.height || payload?.value?.height || 0);
    if (expectedHeight > 0 && proofHeight > 0 && proofHeight < expectedHeight) return null;
  }
  return {
    mode: "light",
    height: header.height || payload?.value?.height || 0,
    proof_type: payload.proof_type || "proof",
    trusted: !!payload.trusted,
    trust_source: payload.trust_source || expectedRootField,
  };
}

async function verifyBalanceLightProof(address, expectedBalance = null, rpc = "") {
  if (!address) return null;
  try {
    const query = new URLSearchParams({ address, coin: "MSC", state: "finalized" }).toString();
    const response = rpc
      ? await state.rpcManager.fetchDedup(rpc, `/proof/balance?${query}`)
      : await state.rpcManager.proof("balance", { address, coin: "MSC", state: "finalized" });
    return verifyLightProofResponse(response, "state_merkle_root", expectedBalance);
  } catch (_) {
    return null;
  }
}

function isRetryableRPCError(err) {
  if (!err) return true;
  if (err.name === "AbortError" || err.network) return true;
  const status = Number(err.status || 0);
  return status === 0 || status === 429 || status >= 500;
}

async function fetchRPC(baseUrl, path, options = {}) {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), options.timeoutMs || REQUEST_TIMEOUT_MS);
  let res;
  try {
    res = await fetch(`${baseUrl}${path}`, {
      method: options.method || "GET",
      headers: options.body ? { "Content-Type": "application/json" } : undefined,
      body: options.body ? JSON.stringify(options.body) : undefined,
      signal: controller.signal,
    });
  } catch (err) {
    err.network = true;
    err.rpc = baseUrl;
    throw err;
  } finally {
    window.clearTimeout(timeout);
  }
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
    err.rpc = baseUrl;
    throw err;
  }
  return data;
}

class WalletRPCManager {
  constructor() {
    this.mode = loadRPCMode();
    this.endpoints = loadRPCEndpoints();
    this.active = this.endpoints[0] || DEFAULT_PUBLIC_RPCS[0];
    this.health = new Map();
    this.inflight = new Map();
    this.lastHealthAt = 0;
    this.suspicious = new Set();
    this.discoveredPublic = new Set();
  }

  setConfig({ mode, endpoints }) {
    this.mode = ["auto", "manual", "custom"].includes(mode) ? mode : "auto";
    this.endpoints = uniqueRPCs(endpoints && endpoints.length ? endpoints : defaultRPCEndpoints());
    if (!this.endpoints.length) this.endpoints = defaultRPCEndpoints();
    this.active = this.endpoints.includes(this.active) ? this.active : this.endpoints[0];
    localStorage.setItem(RPC_MODE_KEY, this.mode);
    localStorage.setItem(RPC_ENDPOINTS_KEY, JSON.stringify(this.endpoints));
    localStorage.setItem(LEGACY_RPC_KEY, this.active);
    this.lastHealthAt = 0;
  }

  inflightKey(rpc, path, options = {}) {
    return stableStringify({
      rpc,
      path,
      method: options.method || "GET",
      body: options.body || null,
    });
  }

  async fetchDedup(rpc, path, options = {}) {
    const method = String(options.method || "GET").toUpperCase();
    const key = this.inflightKey(rpc, path, options);
    if (method === "GET" && this.inflight.has(key)) return this.inflight.get(key);
    const pending = fetchRPC(rpc, path, options).finally(() => this.inflight.delete(key));
    if (method === "GET") this.inflight.set(key, pending);
    return pending;
  }

  recordError(rpc, err) {
    const previous = this.health.get(rpc) || { rpc, score: 0 };
    this.health.set(rpc, {
      ...previous,
      rpc,
      ok: false,
      healthy: false,
      score: 0,
      error: err?.status ? `${err.status} ${err.message || ""}`.trim() : err?.message || "unreachable",
      checkedAt: Date.now(),
      suspicious: this.suspicious.has(rpc),
    });
  }

  mergePublicNodes(nodes = []) {
    const discovered = uniqueRPCs(
      nodes
        .filter((item) => item && item.public_gateway !== false)
        .filter((item) => !String(item.role || "full").toLowerCase().includes("validator"))
        .map((item) => item.gateway_rpc_url || item.rpc_url || item.rpc || item.url),
    );
    this.discoveredPublic = new Set(discovered.map((rpc) => rpc.toLowerCase()));
    if (!discovered.length) return;
    this.endpoints = uniqueRPCs([...discovered, ...this.endpoints]);
    if (!this.endpoints.includes(this.active)) this.active = this.endpoints[0];
    localStorage.setItem(RPC_ENDPOINTS_KEY, JSON.stringify(this.endpoints));
    localStorage.setItem(LEGACY_RPC_KEY, this.active);
    this.updateHealthFromPublicNodes(nodes);
  }

  updateHealthFromPublicNodes(nodes = []) {
    const normalized = nodes
      .map((node) => ({ node, raw: node?.gateway_rpc_url || node?.rpc_url || node?.rpc || node?.url || "" }))
      .filter((item) => isUsableRPC(item.raw))
      .map((item) => ({ node: item.node, rpc: normalizeRPC(item.raw) }));
    const maxHeight = Math.max(0, ...normalized.map((item) => Number(item.node.height || 0)));
    for (const { node, rpc } of normalized) {
      const healthState = String(node.health_state || "").toLowerCase();
      const suspiciousReason = String(node.suspicious_reason || "").trim();
      if (suspiciousReason || healthState === "unhealthy") this.suspicious.add(rpc);
      else if (healthState === "healthy" || healthState === "warning") this.suspicious.delete(rpc);
      const staleBy = maxHeight && node.height ? maxHeight - Number(node.height || 0) : 0;
      const healthy = !!node.healthy && healthState !== "unhealthy" && !suspiciousReason;
      this.health.set(rpc, {
        ...(this.health.get(rpc) || {}),
        rpc,
        ok: healthy || Number(node.status_code || 0) === 200,
        healthy,
        score: Math.max(0, Math.min(100, Number(node.score || 0) - Math.max(0, staleBy - 2))),
        height: Number(node.height || 0),
        finalized: Number(node.finalized_height || 0),
        peers: Number(node.peer_count || 0),
        lag: Number(node.finality_lag || 0),
        syncing: String(node.network_health || "").toLowerCase().includes("syncing"),
        latency: Number(node.latency_ms || 0),
        cmdMode: String(node.consensus_mode || "UNKNOWN").toUpperCase(),
        checkedAt: Number(node.last_checked || 0) ? Number(node.last_checked) * 1000 : Date.now(),
        suspicious: healthState === "unhealthy" || !!suspiciousReason || this.suspicious.has(rpc),
        error: node.excluded_reason || suspiciousReason || node.health_reason || node.error || "",
        healthState: healthState || (healthy ? "healthy" : "unknown"),
        staleBy,
        source: "public-node-registry",
        publicGateway: node.public_gateway !== false,
        activeGateway: !!node.active_gateway,
        selectedReason: node.selected_reason || "",
        excludedReason: node.excluded_reason || "",
      });
    }
    const best = this.bestEndpoints(1)[0];
    if (best) {
      this.active = best;
      state.rpc = best;
      localStorage.setItem(LEGACY_RPC_KEY, best);
    }
  }

  async checkGatewayAggregate(rpc, started) {
    const payload = await this.fetchDedup(rpc, "/gateway/lb-status.json", { timeoutMs: 8000 });
    const registry = normalizeGatewayPublicNodes(payload);
    const nodes = Array.isArray(registry?.nodes) ? registry.nodes : [];
    if (!nodes.length) throw new Error("gateway health unavailable");
    this.updateHealthFromPublicNodes(nodes);
    const bestNode =
      nodes.slice().sort((a, b) => {
        if (Number(b.healthy) !== Number(a.healthy)) return Number(b.healthy) - Number(a.healthy);
        if ((b.score || 0) !== (a.score || 0)) return Number(b.score || 0) - Number(a.score || 0);
        if ((b.height || 0) !== (a.height || 0)) return Number(b.height || 0) - Number(a.height || 0);
        return Number(a.latency_ms || 999999) - Number(b.latency_ms || 999999);
      })[0];
    const healthyCount = Number(registry.healthy ?? nodes.filter((node) => node.healthy).length);
    if (!bestNode || healthyCount <= 0) throw new Error("all gateway backends unhealthy");
    const latency = Math.round(performance.now() - started);
    const entry = {
      ...(this.health.get(rpc) || {}),
      rpc,
      ok: true,
      healthy: true,
      score: Math.max(60, Math.min(100, Number(bestNode.score || 0))),
      height: Number(bestNode.height || 0),
      finalized: Number(bestNode.finalized_height || 0),
      peers: Number(bestNode.peer_count || 0),
      lag: Number(bestNode.finality_lag || 0),
      syncing: false,
      latency,
      cmdMode: String(bestNode.consensus_mode || "UNKNOWN").toUpperCase(),
      checkedAt: Date.now(),
      suspicious: false,
      error: healthyCount < Number(registry.total || nodes.length) ? `${healthyCount}/${registry.total || nodes.length} backends healthy` : "",
      source: "gateway-lb",
      publicGateway: true,
    };
    this.health.set(rpc, entry);
    return entry;
  }

  async checkEndpoint(rpc) {
    const started = performance.now();
    try {
      try {
        return await this.checkGatewayAggregate(rpc, started);
      } catch (_) {
        // Custom RPCs may not expose gateway health; fall back to direct node status.
      }
      const [status, cmdResult] = await Promise.allSettled([
        this.fetchDedup(rpc, "/status", { timeoutMs: 8000 }),
        this.fetchDedup(rpc, "/consensus/mode", { timeoutMs: 8000 }),
      ]);
      if (status.status !== "fulfilled") throw status.reason;
      const data = status.value || {};
      const cmd = cmdResult.status === "fulfilled" ? cmdResult.value || {} : {};
      const height = Number(data.height || data.chain_height || data.best?.height || 0);
      const finalized = Number(data.finalized_height || data.finalized || data.best?.finalized_height || 0);
      const peers = Number(data.peers || data.peer_count || 0);
      const lag = Number(data.network_lag_blocks ?? data.local_lag_blocks ?? data.lag ?? 0);
      const syncing = !!data.syncing || String(data.consensus || "").toLowerCase().includes("syncing");
      const latency = Math.round(performance.now() - started);
      const cmdMode = String(cmd.mode || data.consensus_mode || "UNKNOWN").toUpperCase();
      let score = 40;
      if (!syncing) score += 18;
      if (peers >= 3) score += 12;
      else score += Math.max(0, peers * 3);
      if (lag <= 2) score += 12;
      else if (lag <= 20) score += 6;
      if (latency <= 250) score += 12;
      else if (latency <= 1000) score += 7;
      else if (latency <= 2500) score += 3;
      if (cmdMode === "NORMAL") score += 8;
      else if (cmdMode === "STRICT" || cmdMode === "RECOVERY") score += 3;
      else if (cmdMode === "HALTED" || cmdMode === "ATTACK") score -= 30;
      if (this.suspicious.has(rpc)) score -= 20;
      const entry = {
        rpc,
        ok: true,
        healthy: !syncing && lag <= 50 && score > 0,
        score: Math.max(0, Math.min(100, score)),
        height,
        finalized,
        peers,
        lag,
        syncing,
        latency,
        cmdMode,
        checkedAt: Date.now(),
        suspicious: this.suspicious.has(rpc),
      };
      this.health.set(rpc, entry);
      return entry;
    } catch (err) {
      this.recordError(rpc, err);
      return this.health.get(rpc);
    }
  }

  async refreshHealth(force = false) {
    const jitter = Math.floor(Math.random() * 1500);
    if (!force && Date.now() - this.lastHealthAt < HEALTH_CHECK_MIN_MS + jitter) return this.healthList();
    this.lastHealthAt = Date.now();
    const checks = await Promise.all(this.endpoints.map((rpc) => this.checkEndpoint(rpc)));
    const maxHeight = Math.max(0, ...checks.map((item) => Number(item?.height || 0)));
    checks.forEach((item) => {
      if (!item?.ok || !maxHeight) return;
      const staleBy = maxHeight - Number(item.height || 0);
      if (staleBy > 20) {
        item.score = Math.max(0, item.score - Math.min(35, staleBy));
        item.healthy = false;
      }
      item.staleBy = staleBy;
      this.health.set(item.rpc, item);
    });
    const best = this.bestEndpoints(1)[0];
    if (best) {
      this.active = best;
      state.rpc = best;
      localStorage.setItem(LEGACY_RPC_KEY, best);
    }
    return this.healthList();
  }

  healthList() {
    return this.endpoints.map((rpc) => this.health.get(rpc) || { rpc, ok: false, healthy: false, score: 0, error: "unchecked" });
  }

  bestEndpoints(limit = this.endpoints.length) {
    if (this.mode === "manual") return this.endpoints.slice(0, limit);
    return this.healthList()
      .slice()
      .sort((a, b) => {
        if (b.healthy !== a.healthy) return Number(b.healthy) - Number(a.healthy);
        if ((b.score || 0) !== (a.score || 0)) return (b.score || 0) - (a.score || 0);
        if ((b.height || 0) !== (a.height || 0)) return (b.height || 0) - (a.height || 0);
        return (a.latency || 999999) - (b.latency || 999999);
      })
      .map((item) => item.rpc)
      .slice(0, limit);
  }

  async request(path, options = {}) {
    await this.refreshHealth(false);
    const method = String(options.method || "GET").toUpperCase();
    const candidates = this.bestEndpoints();
    const ordered = candidates.length ? candidates : this.endpoints;
    let lastErr;
    for (const rpc of ordered) {
      try {
        const data = await this.fetchDedup(rpc, path, options);
        this.active = rpc;
        state.rpc = rpc;
        localStorage.setItem(LEGACY_RPC_KEY, rpc);
        return data;
      } catch (err) {
        lastErr = err;
        if (isRetryableRPCError(err)) this.recordError(rpc, err);
        if (method !== "GET" && !isRetryableRPCError(err)) break;
        if (method === "GET" && err.status && err.status < 500 && err.status !== 429) {
          if (!(err.status === 404 && ordered.length > 1)) break;
        }
      }
    }
    throw lastErr || new Error("All RPC endpoints unavailable");
  }

  async quorumRead(path) {
    await this.refreshHealth(false);
    const rankedHealth = this.healthList()
      .slice()
      .sort((a, b) => {
        if (b.healthy !== a.healthy) return Number(b.healthy) - Number(a.healthy);
        if ((b.score || 0) !== (a.score || 0)) return (b.score || 0) - (a.score || 0);
        if ((b.height || 0) !== (a.height || 0)) return (b.height || 0) - (a.height || 0);
        return (a.latency || 999999) - (b.latency || 999999);
      });
    const healthyTargets = rankedHealth
      .filter((item) => item?.ok && item.healthy && !item.suspicious)
      .map((item) => item.rpc)
      .slice(0, 3);
    const fallbackTargets = this.bestEndpoints(3);
    const usableTargets = healthyTargets.length ? healthyTargets : fallbackTargets.length ? fallbackTargets : this.endpoints.slice(0, 3);
    const settled = await Promise.allSettled(usableTargets.map(async (rpc) => ({ rpc, data: await this.fetchDedup(rpc, path) })));
    const successes = settled.filter((item) => item.status === "fulfilled").map((item) => item.value);
    if (!successes.length) {
      const err = settled.find((item) => item.status === "rejected")?.reason || new Error("All RPC endpoints unavailable");
      throw err;
    }
    if (isBalanceReadPath(path)) {
      const selected = balanceQuorumSelection(path, successes);
      const majorityRPCs = new Set((selected.majority || []).map((item) => item.rpc));
      (selected.comparable || []).forEach((item) => {
        if (majorityRPCs.size > 0 && !majorityRPCs.has(item.rpc)) this.suspicious.add(item.rpc);
      });
      return { data: selected.data, verification: selected.verification };
    }
    if (successes.length === 1) {
      return { data: successes[0].data, verification: verificationForRead(path, successes[0], "unverified", 1, 1) };
    }
    const comparableSuccesses = successes;
    const groups = new Map();
    comparableSuccesses.forEach((item) => {
      const key = quorumKey(path, item.data);
      const group = groups.get(key) || [];
      group.push(item);
      groups.set(key, group);
    });
    const majority = Array.from(groups.values()).sort((a, b) => b.length - a.length)[0];
    if (majority.length >= 2) {
      const majorityRPCs = new Set(majority.map((item) => item.rpc));
      comparableSuccesses.forEach((item) => {
        if (!majorityRPCs.has(item.rpc)) this.suspicious.add(item.rpc);
      });
      return {
        data: majority[0].data,
        verification: verificationForRead(path, majority[0], "quorum", majority.length, successes.length),
      };
    }
    comparableSuccesses.slice(1).forEach((item) => this.suspicious.add(item.rpc));
    return {
      data: comparableSuccesses[0].data,
      verification: verificationForRead(path, comparableSuccesses[0], "mismatch", 1, successes.length),
    };
  }

  lightClientStatus() {
    return {
      mode: "spv_pending",
      endpoints: this.endpoints,
      proofEndpoints: ["/light/headers", "/light/checkpoint/latest", "/proof/balance", "/proof/tx", "/proof/receipt"],
    };
  }

  async lightHeaders(params = {}) {
    const query = new URLSearchParams(params).toString();
    return this.request(`/light/headers${query ? `?${query}` : ""}`);
  }

  async lightCheckpointLatest() {
    return this.request("/light/checkpoint/latest");
  }

  async proof(kind, params = {}) {
    const cleanKind = String(kind || "").replace(/[^a-z0-9_-]/gi, "");
    if (!cleanKind) throw new Error("Proof kind required");
    const query = new URLSearchParams(params).toString();
    return this.request(`/proof/${cleanKind}${query ? `?${query}` : ""}`);
  }
}

async function api(path, options = {}) {
  return state.rpcManager.request(path, options);
}

async function quorumApi(path) {
  return state.rpcManager.quorumRead(path);
}

function cacheKey(name, suffix = "") {
  return suffix ? `${name}:${suffix}` : name;
}

async function cachedAPI(key, path, { ttl = 0, force = false, cacheOnly = false, quorum = false } = {}) {
  const cached = state.dataCache?.get(key, ttl);
  if (cached && (cacheOnly || (!force && cached.fresh))) {
    const payload = cached.data;
    if (payload && typeof payload === "object" && Object.prototype.hasOwnProperty.call(payload, "data")) {
      return {
        data: payload.data,
        verification: payload.verification || null,
        ts: cached.ts,
        age: cached.age,
        fresh: cached.fresh,
        fromCache: true,
      };
    }
    return { data: payload, verification: null, ts: cached.ts, age: cached.age, fresh: cached.fresh, fromCache: true };
  }
  if (cacheOnly) return null;
  const result = quorum ? await quorumApi(path) : { data: await api(path), verification: null };
  state.dataCache?.set(key, result);
  return { data: result.data, verification: result.verification, fromCache: false, fresh: true, age: 0 };
}

function walletEventURL(rpc) {
  try {
    const url = new URL(rpc);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    url.pathname = "/wallet/events";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch (_) {
    return "";
  }
}

function setRealtimeStatus(text, tone = "") {
  setStatus("topRealtime", text, tone);
  setStatus("settingsRealtimeStatus", text, tone);
}

async function refreshLoadBalancerStatus(options = {}) {
  let result = state.dataCache?.get("lb-status", CACHE_TTL.lb);
  if (result && (options.cacheOnly || (!options.force && result.fresh))) {
    result = { data: result.data?.data || result.data, fromCache: true };
  } else if (options.cacheOnly) {
    result = null;
  } else {
    const origins = uniqueRPCs([window.location.origin, state.rpcManager?.active, ...defaultRPCEndpoints()]);
    for (const rpc of origins) {
      try {
        const data = await state.rpcManager.fetchDedup(rpc, "/gateway/lb-status.json", { timeoutMs: 8000 });
        state.dataCache?.set("lb-status", { data, verification: null });
        result = { data, fromCache: false };
        break;
      } catch (_) {
        // Not every RPC endpoint is a public gateway; try the next candidate.
      }
    }
  }
  const data = result?.data;
  if (!data) {
    setText("settingsLbStatus", "unavailable");
    return;
  }
  const backends = data.backends || data.upstreams || [];
  const healthy = backends.filter((item) => item.healthy || item.status_code === 200).length;
  setText("settingsLbStatus", data.status || (healthy > 0 ? "healthy" : "degraded"));
  setText("settingsLbBackends", `${healthy}/${backends.length || 0}`);
  const box = $("settingsLbHealth");
  if (box) {
    box.innerHTML = backends.map((item) => `
      <div class="health-row ${item.healthy || item.status_code === 200 ? "success" : "error"}">
        <span class="mono">${item.target || item.url || "-"}</span>
        <span>${item.healthy || item.status_code === 200 ? "healthy" : "down"}</span>
        <span>${item.latency_ms ?? "-"}ms</span>
        <span>${item.status_code ?? "-"}</span>
        <span>${item.last_checked || "-"}</span>
        <span>${item.error || "-"}</span>
      </div>`).join("") || `<div class="list-item">No backend status yet</div>`;
  }
}

function renderPublicNodesRegistry(data) {
  state.publicNodesRegistry = data || state.publicNodesRegistry;
  const registry = state.publicNodesRegistry || {};
  const nodes = Array.isArray(registry.nodes) ? registry.nodes : [];
  const healthy = Number(registry.healthy ?? nodes.filter((item) => item.healthy).length);
  const total = Number(registry.total ?? nodes.length);
  const bestHeight = Math.max(0, ...nodes.map((item) => Number(item.height || 0)).filter((height) => Number.isFinite(height)));
  setText("settingsPublicNodesSummary", `${healthy}/${total} healthy`);
  setText("settingsPublicNodesBest", registry.best || registry.best_node?.rpc_url || "-");
  const box = $("settingsPublicNodesHealth");
  if (box) {
    box.innerHTML = nodes
      .map((item) => {
        const heightLag = publicNodeHeightLag(item, bestHeight);
        const finalityLag = Number(item.finality_lag || 0);
        const ageSeconds = publicNodeDisplayAgeSeconds(item);
        const tone = publicNodeTone(item, bestHeight);
        const state = item.health_state || (item.healthy ? "healthy" : "unhealthy");
        const gateway = item.active_gateway
          ? `active ${item.selected_reason || ""}`.trim()
          : item.excluded_reason
            ? `standby ${item.excluded_reason}`
            : "standby";
        const flags = [gateway, state, item.health_reason, item.consensus_mode, item.network_health, item.suspicious_reason, item.error].filter(Boolean).join(" | ");
        return `
          <div class="health-row public-node-row ${tone}">
            <span class="mono">${item.id || "-"}</span>
            <span class="mono">${item.gateway_rpc_url || item.rpc_url || "-"}</span>
            <span>${escapeHTML(state)}</span>
            <span>${escapeHTML(item.active_gateway ? "active" : "standby")}</span>
            <span>score ${Math.round(Number(item.score || 0))}</span>
            <span>h ${formatNumber(item.height || 0)}</span>
            <span>lag ${formatBlocks(heightLag)}</span>
            <span>finality ${formatBlocks(finalityLag)}</span>
            <span>age ${ageSeconds === null ? "-" : formatAge(ageSeconds)}</span>
            <span>${formatLatency(item.latency_ms)}</span>
            <span>uptime ${publicNodeUptimePct(item)}</span>
            <span>${flags || "-"}</span>
          </div>`;
      })
      .join("") || `<div class="list-item">No public full nodes discovered yet</div>`;
  }
  renderWalletCharts();
}

function normalizeGatewayPublicNodes(payload) {
  const backends = Array.isArray(payload?.backends) ? payload.backends : Array.isArray(payload?.upstreams) ? payload.upstreams : null;
  if (!backends) return payload;
  const nodes = backends.map((item) => ({
    ...item,
    id: item.id || item.node_id || item.target || item.rpc_url || "-",
    rpc_url: item.rpc_url || window.location.origin,
    gateway_rpc_url: item.gateway_rpc_url || item.rpc_url || window.location.origin,
    role: item.role || "full",
    public_gateway: item.public_gateway !== false,
    healthy: item.health_state ? String(item.health_state).toLowerCase() !== "unhealthy" : (!!item.healthy || Number(item.status_code) === 200),
  }));
  const healthy = Number(payload.healthy ?? nodes.filter((item) => item.healthy).length);
  const bestNode =
    nodes.find((item) => item.rpc_url === payload.best) ||
    nodes.slice().sort((a, b) => Number(b.score || 0) - Number(a.score || 0)).find((item) => item.healthy) ||
    nodes[0] ||
    null;
  return {
    status: payload.status || (healthy > 0 ? "healthy" : "down"),
    healthy,
    total: Number(payload.total ?? nodes.length),
    best: payload.best || bestNode?.rpc_url || "",
    best_node: bestNode,
    nodes,
    ts: payload.ts || Math.floor(Date.now() / 1000),
  };
}

function escapeHTML(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function mergePublicNodeRegistry(base, update) {
  const baseNodes = Array.isArray(base?.nodes) ? base.nodes : [];
  const updateNodes = Array.isArray(update?.nodes) ? update.nodes : [];
  if (!baseNodes.length || updateNodes.length >= baseNodes.length) return update;
  const keyFor = (node) => String(node.id || node.target || node.rpc_url || "").trim();
  const merged = new Map(baseNodes.map((node) => [keyFor(node), node]));
  for (const node of updateNodes) {
    const key = keyFor(node);
    if (!key) continue;
    merged.set(key, { ...(merged.get(key) || {}), ...node });
  }
  const nodes = Array.from(merged.values());
  const healthy = nodes.filter((node) => node.healthy).length;
  const bestNode =
    nodes.find((node) => node.rpc_url === update?.best) ||
    nodes.slice().sort((a, b) => Number(b.score || 0) - Number(a.score || 0)).find((node) => node.healthy) ||
    nodes[0] ||
    null;
  return {
    ...(base || {}),
    ...(update || {}),
    healthy,
    total: nodes.length,
    best: update?.best || base?.best || bestNode?.rpc_url || "",
    best_node: bestNode,
    nodes,
  };
}

function applyPublicNodeRegistry(data) {
  if (!data || typeof data !== "object") return;
  const nodes = Array.isArray(data.nodes) ? data.nodes : [];
  state.publicNodesRegistry = data;
  recordPublicNodeUptime(nodes);
  if (state.rpcManager && nodes.length) {
    state.rpcManager.mergePublicNodes(nodes);
  }
  renderPublicNodesRegistry(data);
}

async function refreshPublicNodes(options = {}) {
  let result = state.dataCache?.get("public-nodes", CACHE_TTL.publicNodes);
  if (result && (options.cacheOnly || (!options.force && result.fresh))) {
    result = { data: result.data?.data || result.data, fromCache: true };
  } else if (options.cacheOnly) {
    result = null;
  } else {
    const origins = uniqueRPCs([state.rpcManager?.active, window.location.origin, ...defaultRPCEndpoints()]);
    for (const rpc of origins) {
      try {
        const data = normalizeGatewayPublicNodes(await state.rpcManager.fetchDedup(rpc, "/gateway/lb-status.json", { timeoutMs: 5000 }));
        state.dataCache?.set("public-nodes", { data, verification: null });
        result = { data, fromCache: false };
        break;
      } catch (err) {
        try {
          const data = unwrapV1(await state.rpcManager.fetchDedup(rpc, "/v1/public-nodes", { timeoutMs: 5000 }));
          state.dataCache?.set("public-nodes", { data, verification: null });
          result = { data, fromCache: false };
          break;
        } catch (_) {
          try {
            const data = unwrapV1(await state.rpcManager.fetchDedup(rpc, "/public-nodes", { timeoutMs: 5000 }));
            state.dataCache?.set("public-nodes", { data, verification: null });
            result = { data, fromCache: false };
            break;
          } catch (__) {
            // Older nodes may not have the registry endpoint yet.
          }
        }
      }
    }
  }
  if (result?.data) applyPublicNodeRegistry(result.data);
  else renderPublicNodesRegistry(state.publicNodesRegistry);
}

function renderInfraServiceList(id, services) {
  const box = $(id);
  if (!box) return;
  const list = Array.isArray(services) ? services : [];
  box.innerHTML = list.map((item) => {
    const stateText = item.state || (item.healthy ? "healthy" : "unhealthy");
    const tone = item.healthy ? "success" : stateText === "not_configured" || stateText === "warning" ? "warn" : "error";
    const extra = [];
    if (item.archive_mode !== undefined) extra.push(`archive=${item.archive_mode ? "yes" : "no"}`);
    if (item.height !== undefined) extra.push(`h=${formatNumber(item.height)}`);
    if (item.finality_lag !== undefined) extra.push(`finality_lag=${formatBlocks(item.finality_lag)}`);
    if (item.indexed_height !== undefined) extra.push(`indexed=${formatNumber(item.indexed_height)}`);
    if (item.archive_height !== undefined) extra.push(`archive_h=${formatNumber(item.archive_height)}`);
    if (item.index_lag !== undefined) extra.push(`index_lag=${formatBlocks(item.index_lag)}`);
    if (item.source_rpc) extra.push(`source=${item.source_rpc}`);
    return `<div class="health-row ${tone}">
      <span class="mono">${escapeHTML(item.id || item.role || "-")}</span>
      <span>${escapeHTML(stateText)}</span>
      <span>${formatLatency(item.latency_ms)}</span>
      <span>${escapeHTML(item.role || "-")}</span>
      <span>${item.last_checked || "-"}</span>
      <span>${escapeHTML([item.reason || item.url || "-", ...extra].join(" | "))}</span>
    </div>`;
  }).join("") || `<div class="list-item">No services configured</div>`;
}

function renderWalletCharts() {
  const status = state.status || {};
  const cmd = state.cmd || {};
  const walletStatus = state.walletStatus || {};
  const balance = Number(state.balanceAmount || 0);
  const staked = Number(walletStatus.stake || 0);
  const rewards = Number(walletStatus.rewards || 0);
  const totalPortfolio = balance + staked + rewards;
  const height = Number(status.height || 0);
  const finalized = Number(status.finalized_height || 0);
  const finalityLag = Math.max(0, height - finalized);
  const finalityScore = height > 0 ? clampChart(100 - finalityLag * 10) : 0;
  const registry = state.publicNodesRegistry || {};
  const nodes = Array.isArray(registry.nodes) ? registry.nodes : [];
  const healthyNodes = Number(registry.healthy ?? nodes.filter((item) => item.healthy || Number(item.status_code) === 200).length);
  const totalNodes = Number(registry.total ?? nodes.length);
  const bestHeight = Math.max(0, ...nodes.map((item) => Number(item.height || 0)).filter((item) => Number.isFinite(item)));
  const activeValidators = Number(cmd.active_validators ?? cmd.active_ready ?? 0);
  const totalValidators = Number(cmd.total_validators ?? cmd.committee_size ?? activeValidators);
  const validatorReady = totalValidators ? percentChart(activeValidators, totalValidators) : 0;
  const validators = validatorEntries();
  const publicStatus = state.publicStatusData || {};
  const serviceRows = [
    ...(Array.isArray(publicStatus.archive) ? publicStatus.archive : []),
    ...(Array.isArray(publicStatus.indexer) ? publicStatus.indexer : []),
  ].slice(0, 6);

  chartCard(
    "walletBalanceChart",
    "Wallet allocation",
    state.wallet?.address ? shortAddress(state.wallet.address) : "No wallet loaded",
    `${donutHTML(percentChart(staked, Math.max(totalPortfolio, 1)), "Staked share", `liquid ${formatNumber(balance)} | staked ${formatNumber(staked)}`, staked ? "success" : "warn")}${matrixHTML([
      { label: "Liquid", value: `${formatNumber(balance)} MSC` },
      { label: "Staked", value: `${formatNumber(staked)} MSC` },
      { label: "Rewards", value: `${formatNumber(rewards)} MSC` },
    ])}`,
  );
  chartCard(
    "walletFinalityChart",
    "Finality freshness",
    "Head vs finalized checkpoint",
    `${donutHTML(finalityScore, "Finality score", `${formatBlocks(finalityLag)} lag`, finalityLag <= 1 ? "success" : finalityLag <= 3 ? "warn" : "error")}${matrixHTML([
      { label: "Height", value: formatNumber(height) },
      { label: "Finalized", value: formatNumber(finalized) },
      { label: "Age", value: formatAge(status.last_block_age_seconds || 0) },
    ])}`,
  );
  chartCard(
    "walletNetworkChart",
    "Public RPC mesh",
    "Gateway nodes, latency, and active route",
    `${rowsHTML(nodes.slice(0, 8).map((item) => {
      const nodeTone = publicNodeTone(item, bestHeight);
      const score = item.score !== undefined ? Number(item.score || 0) : item.healthy || Number(item.status_code) === 200 ? 100 : 10;
      return {
        label: item.id || item.target || "node",
        value: score,
        display: `${item.health_state || (item.healthy ? "healthy" : "down")} ${formatLatency(item.latency_ms)}`,
        tone: nodeTone === "error" ? "error" : nodeTone === "warn" ? "warn" : "success",
      };
    }))}${matrixHTML([
      { label: "Healthy", value: `${formatNumber(healthyNodes)} / ${formatNumber(totalNodes)}` },
      { label: "Best", value: registry.best_node?.id || registry.best || "-" },
      { label: "Status", value: registry.status || "-" },
    ])}`,
    totalNodes ? `${formatNumber(healthyNodes)}/${formatNumber(totalNodes)}` : "",
  );
  chartCard(
    "walletValidatorChart",
    "Validator readiness",
    "CMD active set health",
    `${donutHTML(validatorReady, "Active validators", `${formatNumber(activeValidators)} / ${formatNumber(totalValidators)} active`, validatorReady >= 80 ? "success" : validatorReady >= 60 ? "warn" : "error")}${matrixHTML([
      { label: "CMD", value: cmd.mode || "-" },
      { label: "Active", value: formatNumber(activeValidators) },
      { label: "Total", value: formatNumber(totalValidators) },
    ])}`,
  );
  chartCard("statusFinalityChart", "Chain finality", "Public status checkpoint lag", donutHTML(finalityScore, "Finality freshness", `${formatBlocks(finalityLag)} lag | ${formatAge(status.last_block_age_seconds || 0)} age`, finalityLag <= 1 ? "success" : finalityLag <= 3 ? "warn" : "error"));
  chartCard("statusRpcChart", "RPC node quality", "Public gateway candidate scores", rowsHTML(nodes.slice(0, 10).map((item) => ({
    label: item.id || item.target || "node",
    value: item.score !== undefined ? Number(item.score || 0) : item.healthy ? 100 : 10,
    display: `${item.active_gateway ? "active" : "standby"} | ${formatLatency(item.latency_ms)}`,
    tone: publicNodeTone(item, bestHeight) === "error" ? "error" : publicNodeTone(item, bestHeight) === "warn" ? "warn" : "success",
  }))), registry.status || "");
  chartCard("statusServiceChart", "Archive and indexers", "Read-only history services", rowsHTML(serviceRows.map((item) => ({
    label: item.id || item.role || "service",
    value: item.healthy ? 100 : item.state === "warning" ? 55 : 12,
    display: item.state || (item.healthy ? "healthy" : "down"),
    tone: item.healthy ? "success" : item.state === "warning" ? "warn" : "error",
  }))));
  const validatorFallbackRows = [
    { label: "Active", value: activeValidators, display: formatNumber(activeValidators) },
    { label: "Total", value: totalValidators, display: formatNumber(totalValidators) },
    { label: "Height", value: height ? 100 : 0, display: formatNumber(height) },
    { label: "CMD", value: cmd.mode === "NORMAL" ? 100 : 45, display: cmd.mode || "-" },
  ];
  chartCard("walletValidatorScoreChart", "Validator score curve", "Top visible validator performance", validators.length
    ? barsHTML(validators.slice(0, 14).map((item) => ({
      label: item.validator_id || item.id || item.name || "validator",
      value: Number(item.final_score ?? item.signed_ratio_bps ?? item.effective_stake ?? 0),
      display: item.final_score !== undefined ? `${Math.round(Number(item.final_score || 0) * 1000) / 10}%` : formatNumber(item.effective_stake || 0),
    })))
    : rowsHTML(validatorFallbackRows), validators.length ? `${formatNumber(validators.length)} validators` : "CMD view");
  chartCard("walletValidatorStakeChart", "Stake distribution", "Effective stake by validator", validators.length
    ? rowsHTML(validators.slice(0, 7).map((item) => ({
      label: item.validator_id || item.id || item.name || "-",
      value: Number(item.effective_stake ?? item.actual_stake ?? 0),
      display: `${formatNumber(item.effective_stake ?? item.actual_stake ?? 0)} MSC`,
    })))
    : matrixHTML([
      { label: "Active", value: formatNumber(activeValidators) },
      { label: "Total", value: formatNumber(totalValidators) },
      { label: "Mode", value: cmd.mode || "-" },
      { label: "Source", value: "CMD" },
    ]));
  chartCard(
    "stakingPortfolioChart",
    "Staking portfolio",
    "Liquid, staked, and earned MSC",
    `${donutHTML(percentChart(staked + rewards, Math.max(totalPortfolio, 1)), "Locked plus earned", `${formatNumber(staked + rewards)} / ${formatNumber(totalPortfolio)} MSC`, staked ? "success" : "warn")}${matrixHTML([
      { label: "Delegation", value: walletStatus.validator_id || "-" },
      { label: "Rewards", value: `${formatNumber(rewards)} MSC` },
      { label: "Liquid", value: `${formatNumber(balance)} MSC` },
    ])}`,
  );
  chartCard("stakingValidatorChart", "Validator opportunities", "Score and stake leaders", validators.length
    ? rowsHTML(validators.slice(0, 8).map((item) => ({
      label: item.validator_id || item.id || item.name || "-",
      value: Number(item.final_score ?? item.effective_stake ?? 0),
      display: item.final_score !== undefined ? `${Math.round(Number(item.final_score || 0) * 1000) / 10}%` : `${formatNumber(item.effective_stake || 0)} MSC`,
    })))
    : rowsHTML(validatorFallbackRows));
}

function renderPublicStatus(data) {
  const payload = unwrapV1(data);
  if (!payload || typeof payload !== "object") return;
  state.publicStatusData = payload;
  const chain = payload.chain || {};
  const validators = payload.validators || {};
  const publicRPC = payload.public_rpc || {};
  const storage = payload.storage || {};
  const light = payload.light_client || {};
  const gateway = payload.gateway || {};

  setText("statusChainHeight", formatNumber(chain.height || 0));
  setText("statusFinalized", formatNumber(chain.finalized_height || 0));
  setText("statusFinalityLag", formatBlocks(chain.finality_lag || 0));
  setText("statusLastBlockAge", formatAge(chain.last_block_age_seconds || 0));
  setTone("statusLastBlockAge", blockAgeTone(chain.last_block_age_seconds || 0));
  setText("statusCMD", chain.cmd || "-");
  setText("statusNetwork", chain.network_health || "-");
  setText("statusValidators", `${validators.active_ready || 0}/${validators.strict_quorum || validators.required_quorum || 0} ready`);
  setText("statusValidatorPolicy", validators.validator_rpc || "private_only");
  setText("statusPublicRPC", `${publicRPC.healthy || 0}/${publicRPC.total || 0} healthy`);
  setText("statusBestRPC", publicRPC.best || "-");
  setText("statusArchiveMode", storage.archive_mode ? "Archive mode active" : storage.retention_summary || "archive node required");
  setText("statusStorageProfile", storage.profile || "-");
  setText("statusLightClient", light.ready ? "ready" : light.status || "warming");
  setText("statusProofAPIs", [light.headers, light.checkpoint, light.balance_proof, light.tx_proof, light.receipt_proof].filter(Boolean).join(" | "));
  setText("statusGatewayLayout", gateway.layout || "-");
  setText("statusGatewayEvents", gateway.events || "-");
  setText("statusMetricsPublic", gateway.metrics_public === false ? "blocked/private" : "check gateway policy");
  renderInfraServiceList("statusArchiveServices", payload.archive);
  renderInfraServiceList("statusIndexerServices", payload.indexer);
  if (publicRPC.nodes) {
    applyPublicNodeRegistry({
      status: publicRPC.status,
      healthy: publicRPC.healthy,
      total: publicRPC.total,
      best: publicRPC.best,
      nodes: publicRPC.nodes,
      ts: payload.ts,
    });
  }
  renderWalletCharts();
}

async function refreshPublicStatus(options = {}) {
  if (!$("statusChainHeight")) return;
  try {
    const result = await cachedAPI("public-status", "/v1/public/status", {
      ttl: CACHE_TTL.publicStatus,
      force: !!options.force,
      cacheOnly: !!options.cacheOnly,
    });
    renderPublicStatus(result?.data);
    if (!options.cacheOnly) {
      try {
        const rpc = state.rpcManager?.activeRPC() || window.location.origin;
        const gateway = await state.rpcManager.fetchDedup(rpc, "/gateway/lb-status.json", { timeoutMs: 5000 });
        if (Array.isArray(gateway?.archive)) renderInfraServiceList("statusArchiveServices", gateway.archive);
        if (Array.isArray(gateway?.indexer)) renderInfraServiceList("statusIndexerServices", gateway.indexer);
      } catch (_) {
        // Public status remains useful even when gateway-local health is absent.
      }
    }
  } catch (err) {
    setText("statusNetwork", err.message || "status unavailable");
  }
}

function selectedRPCReason(active, items) {
  const list = (items || []).filter((item) => item && (item.healthy || item.ok));
  const current = list.find((item) => item.rpc === active) || (items || []).find((item) => item && item.rpc === active);
  if (!current) return "-";
  if (list.length <= 1) return "Only Healthy Backend";
  const maxScore = Math.max(...list.map((item) => Number(item.score || 0)));
  if (Number(current.score || 0) >= maxScore) return "Highest Score";
  const minLag = Math.min(...list.map((item) => Number(item.staleBy ?? item.lag ?? 0)));
  if (Number(current.staleBy ?? current.lag ?? 0) <= minLag) return "Lowest Lag";
  const minLatency = Math.min(...list.map((item) => Number(item.latency || 999999)));
  if (Number(current.latency || 999999) <= minLatency) return "Lowest Latency";
  return "Realtime Quality";
}

function refreshRPCSettingsUI() {
  if (!state.rpcManager) return;
  state.rpc = state.rpcManager.active;
  setText("topRpc", String(state.rpc || "-").replace(/^https?:\/\//, ""));
  setValue("settingsRpcMode", state.rpcManager.mode);
  setValue("settingsRpc", state.rpcManager.active);
  setValue("settingsRpcEndpoints", state.rpcManager.endpoints.join("\n"));
  setText("settingsActiveRpc", state.rpcManager.active || "-");
  const health = state.rpcManager.healthList();
  setText("settingsRpcSelectedReason", selectedRPCReason(state.rpcManager.active, health));
  const healthBox = $("settingsRpcHealth");
  if (healthBox) {
    const rows = health.map((item) => {
      const healthState = item.healthState || (item.healthy ? "healthy" : item.ok ? "warning" : "unhealthy");
      const tone = healthState === "unhealthy" ? "error" : healthState === "warning" ? "warn" : "success";
      const flags = [item.publicGateway ? "public" : "", rpcPolicyWarning(item.rpc), item.suspicious ? "suspicious" : "", item.syncing ? "syncing" : "", item.error || ""]
        .filter(Boolean)
        .join(" | ");
      return `
        <div class="health-row rpc-health-row ${tone}">
          <span class="mono">${item.rpc}</span>
          <span>${escapeHTML(healthState)}</span>
          <span>score ${Math.round(item.score || 0)}</span>
          <span>h ${formatNumber(item.height || 0)}</span>
          <span>lag ${formatBlocks(item.staleBy ?? item.lag ?? 0)}</span>
          <span>${formatLatency(item.latency)}</span>
          <span>${flags || "-"}</span>
        </div>`;
    });
    healthBox.innerHTML = rows.join("") || `<div class="list-item">No RPC endpoints configured</div>`;
  }
}

function renderNetworkStatus(status) {
  if (!status) return;
  const best = status.best || status;
  const rawHeight = Number(best.height || status.height || status.chain_height || best.finalized_height || 0);
  const rawFinalized = Number(best.finalized_height || status.finalized_height || status.finalized || 0);
  const height = Math.max(Number.isFinite(rawHeight) ? rawHeight : 0, state.realtime.height || 0);
  const finalized = Math.max(Number.isFinite(rawFinalized) ? rawFinalized : 0, state.realtime.finalizedHeight || 0);
  state.status = { ...status, height, finalized_height: finalized };
  const headSynced = !!(best.head_synced ?? status.head_synced);
  const backfilling = !!(best.history_backfill_pending ?? status.history_backfill_pending);
  const fastStage = best.fast_bootstrap_stage || status.fast_bootstrap_stage || "";
  const networkLabel = headSynced
    ? backfilling
      ? `head synced | history backfilling${fastStage ? ` | ${fastStage}` : ""}`
      : "head synced"
    : status.health || status.network_health || "connected";
  setLastBlockAgeBase(best.last_block_age_seconds ?? status.last_block_age_seconds);
  setText("topHeight", formatNumber(height));
  setText("networkStatus", networkLabel);
  setText("blockHeight", formatNumber(height));
  setText("finalizedHeight", formatNumber(finalized));
  setText("latestBlocks", `height ${formatNumber(height)} | finalized ${formatNumber(finalized)}`);
  setText("txBlockHeight", formatNumber(height));
  const testnet = isTestnetWalletNetwork();
  const networkName = status.network_name || (testnet ? "Testnet" : "Mainnet");
  setStatus("networkPill", networkName, testnet ? "warn" : "success");
  renderFaucetState();
  renderWalletHome();
  renderWalletCharts();
}

function renderCMD(cmd) {
  if (!cmd) return;
  state.cmd = cmd;
  const mode = cmd.mode || "UNKNOWN";
  setText("topCmd", mode);
  setText("cmdStatus", mode);
  setText("validatorStatus", `${cmd.active_validators ?? "-"} / ${cmd.total_validators ?? "-"} active`);
  setText("validatorCMD", mode);
  renderWalletCharts();
}

async function refreshNetwork(options = {}) {
  await refreshPublicNodes(options);
  if (!options.cacheOnly) await state.rpcManager.refreshHealth(!!options.force);
  refreshRPCSettingsUI();
  try {
    const status = await cachedAPI("status", "/status", { ttl: CACHE_TTL.status, force: !!options.force, cacheOnly: !!options.cacheOnly });
    renderNetworkStatus(status?.data);
  } catch (err) {
    setStatus("networkPill", "RPC error", "error");
    setText("networkStatus", err.message || "unavailable");
  }

  try {
    const cmd = await cachedAPI("cmd", "/consensus/mode", { ttl: CACHE_TTL.cmd, force: !!options.force, cacheOnly: !!options.cacheOnly });
    renderCMD(cmd?.data);
  } catch (_) {
    setText("topCmd", "-");
  }
  await refreshLoadBalancerStatus(options);
  renderPublicNodesRegistry(state.publicNodesRegistry);
}

function renderBalanceData(bal, verification) {
  if (!bal) return;
  const height = balanceResponseHeight(bal);
  if (height > 0 && Number(state.balanceHeight || 0) > height) {
    return;
  }
  const amount = bal.balance ?? bal.Balance ?? "-";
  const numericAmount = balanceResponseAmount(bal);
  if (!balanceVerificationCanDisplay(verification, height, numericAmount)) {
    if (Number(state.balanceHeight || 0) <= 0 && verification) renderVerification(verification);
    return;
  }
  state.balanceVerification = verification || state.balanceVerification;
  if (verification) renderVerification(verification);
  setText("totalBalance", `${formatNumber(amount)} MSC`);
  setText("walletBalance", `${formatNumber(amount)} MSC`);
  setText("assetMSC", `${formatNumber(amount)} MSC`);
  state.balanceHeight = height || state.balanceHeight || 0;
  state.balanceAmount = Number(amount) || 0;
  renderWalletHome();
  renderWalletCharts();
  renderDelegationEstimate();
  renderValidatorWallet();
}

function renderWalletStatus(ws) {
  if (!ws) return;
  state.walletStatus = ws;
  setText("stakedBalance", `${formatNumber(ws.stake || 0)} MSC`);
  setText("rewardBalance", `${formatNumber(ws.rewards || 0)} MSC`);
  setText("delegations", ws.validator_id ? `${ws.validator_id}: ${formatNumber(ws.stake || 0)} MSC` : "No active delegation");
  renderWalletHome();
  renderWalletCharts();
  renderDelegationEstimate();
  renderValidatorWallet();
}

function validatorEntries() {
  const data = state.validatorsData || {};
  const raw = data.entries || data.validators || data.active || data.items || [];
  if (Array.isArray(raw)) return raw.filter(Boolean);
  if (raw && typeof raw === "object") return Object.values(raw).filter(Boolean);
  return [];
}

function validatorIdOf(validator) {
  return String(validator?.validator_id ?? validator?.id ?? validator?.validator ?? validator?.address ?? validator?.name ?? "").trim();
}

function validatorNameOf(validator) {
  return String(validator?.moniker ?? validator?.display_name ?? validator?.name ?? validatorIdOf(validator) ?? "Validator").trim() || "Validator";
}

function validatorPubkeyOf(validator) {
  return String(validator?.validator_pubkey ?? validator?.consensus_pubkey ?? validator?.pubkey ?? validator?.public_key ?? "").trim();
}

function validatorStakeOf(validator) {
  return Number(validator?.effective_stake ?? validator?.actual_stake ?? validator?.delegated_stake ?? validator?.self_stake ?? 0) || 0;
}

function normalizePercent(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return 0;
  if (n > 100) return Math.min(100, n / 100);
  if (n <= 1) return Math.min(100, n * 100);
  return Math.min(100, n);
}

function validatorScoreOf(validator) {
  return normalizePercent(validator?.final_score ?? validator?.score ?? validator?.validator_score ?? validator?.signed_ratio_bps ?? validator?.uptime_bps ?? 0);
}

function validatorSignedOf(validator) {
  return normalizePercent(validator?.signed_ratio_bps ?? validator?.signed_ratio ?? validator?.uptime_bps ?? validator?.uptime_percent ?? validator?.uptime ?? 0);
}

function validatorCommissionOf(validator) {
  const raw = Number(validator?.commission_bps ?? validator?.commission_rate_bps ?? validator?.commission_rate ?? validator?.commission ?? validator?.fee_bps);
  if (!Number.isFinite(raw) || raw < 0) return 0;
  if (raw <= 1) return raw * 100;
  if (raw > 100) return Math.min(100, raw / 100);
  return raw;
}

function stableNumberHash(value) {
  let hash = 2166136261;
  const text = String(value || "");
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function validatorStatusText(validator) {
  return [
    validator?.status,
    validator?.state,
    validator?.health,
    validator?.slot_type,
    validator?.set,
    validator?.online_status,
  ].filter(Boolean).join(" ").toLowerCase();
}

function validatorIsActiveCandidate(validator) {
  if (!validatorIdOf(validator)) return false;
  const status = validatorStatusText(validator);
  const blocked = ["banned", "suspended", "jailed", "tombstoned", "removed", "slashed", "offline", "inactive", "disabled", "decommissioned"];
  if (blocked.some((token) => status.includes(token))) return false;
  if (validator?.banned || validator?.suspended || validator?.jailed || validator?.tombstoned || validator?.removed || validator?.slashed || validator?.disabled) return false;
  if (validator?.active === false || validator?.is_active === false || validator?.online === false || validator?.reachable === false) return false;
  if (String(validator?.slot_type || "").toLowerCase().includes("standby")) return false;
  return true;
}

function validatorIsStrictRecommendationEligible(validator) {
  const commission = validatorCommissionOf(validator);
  return validatorIsActiveCandidate(validator) &&
    validatorScoreOf(validator) >= MIN_RECOMMENDED_VALIDATOR_SCORE &&
    validatorSignedOf(validator) >= MIN_RECOMMENDED_SIGNED_PERCENT &&
    commission <= MAX_RECOMMENDED_COMMISSION_PERCENT;
}

function validatorIsFallbackRecommendationEligible(validator) {
  const commission = validatorCommissionOf(validator);
  return validatorIsActiveCandidate(validator) && commission <= MAX_RECOMMENDED_COMMISSION_PERCENT;
}

function strictRecommendedStakeValidators() {
  return validatorEntries().filter(validatorIsStrictRecommendationEligible).sort(compareRecommendedValidators);
}

function fallbackRecommendedStakeValidators() {
  return validatorEntries().filter(validatorIsFallbackRecommendationEligible).sort(compareRecommendedValidators);
}

function validatorIsRecommendationEligible(validator) {
  const strict = strictRecommendedStakeValidators();
  if (strict.length) return validatorIsStrictRecommendationEligible(validator);
  return validatorIsFallbackRecommendationEligible(validator);
}

function compareRecommendedValidators(a, b) {
  return (validatorScoreOf(b) - validatorScoreOf(a)) ||
    (validatorSignedOf(b) - validatorSignedOf(a)) ||
    (validatorCommissionOf(a) - validatorCommissionOf(b)) ||
    (validatorStakeOf(b) - validatorStakeOf(a));
}

function recommendedStakeValidators() {
  const strict = strictRecommendedStakeValidators();
  return strict.length ? strict : fallbackRecommendedStakeValidators();
}

function recommendedStakeValidator(validators = recommendedStakeValidators()) {
  const pool = validators.slice(0, RECOMMENDED_VALIDATOR_POOL_SIZE);
  if (!pool.length) return null;
  const day = new Date().toISOString().slice(0, 10);
  const seed = `${state.wallet?.address || window.location.host || "msc"}:${day}`;
  return pool[stableNumberHash(seed) % pool.length];
}

function setStakeAvailability(enabled, note, tone = "") {
  const submit = $("stakeSubmitButton");
  const select = $("stakeValidatorSelect");
  if (submit) {
    submit.disabled = !enabled;
    submit.textContent = enabled ? "Confirm & Stake" : "Staking disabled";
  }
  if (select) select.disabled = !enabled;
  setText("validatorRecommendationNote", note || "");
  if (note) setStatus("stakeStatus", note, tone);
}

function percentText(value) {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return "-";
  return `${Math.round(n * 10) / 10}%`;
}

function validatorTone(validator) {
  const score = validatorScoreOf(validator);
  if (score >= 80) return "success";
  if (score >= 55) return "warn";
  return "error";
}

function findStakeValidator(id) {
  const value = String(id || "").trim();
  if (!value) return null;
  return validatorEntries().find((validator) => validatorIdOf(validator) === value) || null;
}

function currentStakeValidator() {
  const id = state.selectedStakeValidatorId || $("stakeValidatorSelect")?.value || $("stakeValidator")?.value || "";
  return findStakeValidator(id);
}

function renderDelegationEstimate() {
  const target = $("stakeEstimate");
  if (!target) return;
  const amount = Number($("stakeAmount")?.value || 0);
  const epochs = Number($("stakeEpochs")?.value || DEFAULT_STAKE_EPOCHS);
  const validator = currentStakeValidator();
  const fee = amount > 0 ? computeFee(amount) : 0;
  const net = Math.max(0, amount - fee);
  const score = validatorScoreOf(validator);
  const commission = validatorCommissionOf(validator);
  const rewardSignal = score >= 80 ? "High" : score >= 55 ? "Medium" : validator ? "Review" : "Select validator";
  target.innerHTML = `
    <div><span>Network fee</span><strong>${formatNumber(fee)} MSC</strong></div>
    <div><span>Delegated amount</span><strong>${amount > 0 ? `${formatNumber(net)} MSC` : "-"}</strong></div>
    <div><span>Lock epochs</span><strong>${Number.isFinite(epochs) && epochs > 0 ? formatNumber(epochs) : "-"}</strong></div>
    <div><span>Reward signal</span><strong>${escapeHTML(rewardSignal)}</strong></div>
    <div><span>Validator score</span><strong>${percentText(score)}</strong></div>
    <div><span>Commission</span><strong>${commission ? percentText(commission) : "Not listed"}</strong></div>
  `;
}

function renderSelectedValidatorCard(validator) {
  const card = $("selectedValidatorCard");
  const id = validatorIdOf(validator);
  const score = validatorScoreOf(validator);
  const signed = validatorSignedOf(validator);
  const stake = validatorStakeOf(validator);
  const commission = validatorCommissionOf(validator);
  const tone = validator ? validatorTone(validator) : "warn";
  setText("selectedValidatorStat", id ? shortAddress(id) : "-");
  if (!card) return;
  card.innerHTML = `
    <div class="section-head">
      <div>
        <div class="eyebrow">Validator Profile</div>
        <h2>${escapeHTML(validator ? validatorNameOf(validator) : id || "No validator selected")}</h2>
      </div>
      <span class="validator-score-pill ${tone}">${validator ? percentText(score) : "Review"}</span>
    </div>
    <div class="stake-estimate-grid">
      <div><span>Validator ID</span><strong class="mono">${escapeHTML(id || "-")}</strong></div>
      <div><span>Total stake</span><strong>${formatNumber(stake)} MSC</strong></div>
      <div><span>Signed</span><strong>${percentText(signed)}</strong></div>
      <div><span>Commission</span><strong>${commission ? percentText(commission) : "Not listed"}</strong></div>
    </div>
    <div class="validator-detail-line">
      <span>Pubkey</span>
      <strong class="mono">${escapeHTML(validatorPubkeyOf(validator) || $("stakePubkey")?.value || "-")}</strong>
    </div>
    <p class="muted">Delegation means this validator runs the node. Your wallet only signs the stake transaction; rewards are policy and performance dependent.</p>
  `;
}

function renderDelegationValidatorList() {
  const list = $("delegationValidatorList");
  if (!list) return;
  const validators = recommendedStakeValidators();
  const selected = state.selectedStakeValidatorId || $("stakeValidator")?.value || "";
  if (!validatorEntries().length) {
    const message = state.validatorLoadState === "loading" || state.validatorLoadState === "idle"
      ? "Loading recommended validators..."
      : "Validator list unavailable. Staking temporarily disabled.";
    list.innerHTML = `<div class="list-item">${escapeHTML(message)}</div>`;
    return;
  }
  if (!validators.length) {
    list.innerHTML = `<div class="list-item">No eligible active validator available. Staking temporarily disabled.</div>`;
    return;
  }
  list.innerHTML = validators.slice(0, 12).map((validator) => {
    const id = validatorIdOf(validator);
    const score = validatorScoreOf(validator);
    const stake = validatorStakeOf(validator);
    const signed = validatorSignedOf(validator);
    const active = id && id === selected ? " active" : "";
    return `
      <button type="button" class="validator-choice${active}" data-select-validator="${escapeHTML(id)}">
        <span>
          <strong>${escapeHTML(validatorNameOf(validator))}</strong>
          <em class="mono">${escapeHTML(id || "-")}</em>
        </span>
        <span class="validator-choice-meta">
          <em>${percentText(score)} score</em>
          <em>${formatNumber(stake)} MSC</em>
          <em>${percentText(signed)} signed</em>
        </span>
      </button>
    `;
  }).join("");
  list.querySelectorAll("[data-select-validator]").forEach((button) => {
    button.addEventListener("click", () => selectStakeValidator(button.dataset.selectValidator || ""));
  });
}

function populateStakeValidatorSelect() {
  const select = $("stakeValidatorSelect");
  const allValidators = validatorEntries();
  const validators = recommendedStakeValidators();
  if (!select) {
    renderDelegationValidatorList();
    return;
  }
  const current = state.selectedStakeValidatorId || select.value || $("stakeValidator")?.value || "";
  if (!allValidators.length) {
    const loading = state.validatorLoadState === "loading" || state.validatorLoadState === "idle";
    const message = loading ? "Loading validators..." : "Validator list unavailable. Staking temporarily disabled.";
    select.innerHTML = `<option value="">${escapeHTML(message)}</option>`;
    setValue("stakeValidator", "");
    setValue("stakePubkey", "");
    renderSelectedValidatorCard(null);
    renderDelegationValidatorList();
    renderDelegationEstimate();
    setStakeAvailability(false, message, loading ? "warn" : "error");
    return;
  }
  if (!validators.length) {
    const message = "No eligible active validator available. Staking temporarily disabled.";
    select.innerHTML = `<option value="">${message}</option>`;
    setValue("stakeValidator", "");
    setValue("stakePubkey", "");
    renderSelectedValidatorCard(null);
    renderDelegationValidatorList();
    renderDelegationEstimate();
    setStakeAvailability(false, message, "error");
    return;
  }
  select.innerHTML = validators.map((validator) => {
    const id = validatorIdOf(validator);
    const label = `${validatorNameOf(validator)} - ${id || "validator"}`;
    return `<option value="${escapeHTML(id)}">${escapeHTML(label)}</option>`;
  }).join("");
  const recommended = recommendedStakeValidator(validators);
  const currentEligible = validators.some((validator) => validatorIdOf(validator) === current);
  const next = currentEligible ? current : validatorIdOf(recommended);
  if (next) selectStakeValidator(next);
}

function selectStakeValidator(id) {
  const value = String(id || "").trim();
  const validator = findStakeValidator(value);
  const resolved = validator ? validatorIdOf(validator) : value;
  state.selectedStakeValidatorId = resolved;
  const select = $("stakeValidatorSelect");
  if (select && resolved && Array.from(select.options).some((option) => option.value === resolved)) {
    select.value = resolved;
  }
  setValue("stakeValidator", resolved);
  setValue("unstakeValidator", resolved);
  setValue("stakePubkey", validatorPubkeyOf(validator) || "");
  renderSelectedValidatorCard(validator || (resolved ? { validator_id: resolved } : null));
  renderDelegationEstimate();
  renderDelegationValidatorList();
  const eligible = !!validator && validatorIsRecommendationEligible(validator);
  setStakeAvailability(
    eligible,
    eligible ? "Recommended Validator selected automatically. You can change it before staking." : "No eligible active validator available. Staking temporarily disabled.",
    eligible ? "success" : "error",
  );
}

function renderStakingDelegationUI() {
  populateStakeValidatorSelect();
  renderDelegationValidatorList();
  renderDelegationEstimate();
}

function validatorEntryForLink(link = validatorWalletLink()) {
  const id = String(link?.validatorId || "").trim();
  if (!id) return null;
  return validatorEntries().find((validator) => validatorIdOf(validator) === id) || null;
}

function validatorCertificateText(link = validatorWalletLink(), entry = validatorEntryForLink(link)) {
  if (!link) return "";
  return [
    "MSC Validator Certificate",
    `Chain ID: ${CHAIN_ID}`,
    `Wallet: ${link.walletAddress}`,
    `Validator: ${link.validatorId}`,
    `Consensus Pubkey: ${link.consensusPubkey}`,
    `Node: ${link.nodeURL || "not provided"}`,
    `Status: ${link.status || "verified"}`,
    `Verified At: ${link.verifiedAt || "-"}`,
    `Self Stake: ${formatNumber(validatorStakeOf(entry))} MSC`,
    `Signed Ratio: ${percentText(validatorSignedOf(entry))}`,
  ].join("\n");
}

function renderValidatorAlerts(link, entry) {
  const list = $("validatorAlerts");
  if (!list) return;
  if (!link) {
    list.innerHTML = `<div class="list-item">Validator tools locked until node proof is linked.</div>`;
    setStatus("validatorAlertTone", "Locked", "warn");
    return;
  }
  const alerts = [];
  if (!entry) {
    alerts.push({ tone: "warn", title: "Leaderboard pending", text: "Validator proof is linked, but public validator data is not visible yet." });
  } else {
    if (!validatorIsActiveCandidate(entry)) alerts.push({ tone: "error", title: "Validator not active", text: "Node is not currently an active candidate." });
    if (entry.online === false) alerts.push({ tone: "error", title: "Node offline", text: "Public status marks this validator offline." });
    const missed = Number(entry.missed_blocks || 0);
    if (missed > 0) alerts.push({ tone: "warn", title: "Missed blocks", text: `${formatNumber(missed)} missed blocks recorded.` });
    const slashes = Number(entry.total_slashes || 0);
    if (slashes > 0) alerts.push({ tone: "error", title: "Slash history", text: `${formatNumber(slashes)} slash events recorded.` });
    if (!alerts.length) alerts.push({ tone: "success", title: "No critical alerts", text: "Validator is active in the current public data." });
  }
  const worst = alerts.some((item) => item.tone === "error") ? "error" : alerts.some((item) => item.tone === "warn") ? "warn" : "success";
  setStatus("validatorAlertTone", worst === "success" ? "Healthy" : worst === "warn" ? "Review" : "Alert", worst);
  list.innerHTML = alerts.map((item) => `
    <div class="list-item">
      <strong class="${item.tone}">${escapeHTML(item.title)}</strong>
      <span>${escapeHTML(item.text)}</span>
    </div>
  `).join("");
}

function renderValidatorVoteHistory(link = validatorWalletLink()) {
  const list = $("validatorVoteHistory");
  if (!list) return;
  const all = loadValidatorVoteHistory();
  const items = link ? all.filter((item) => item.validatorId === link.validatorId) : [];
  setText("validatorVoteHistoryCount", `${formatNumber(items.length)} votes`);
  if (!items.length) {
    list.innerHTML = `<div class="list-item">No validator votes prepared yet.</div>`;
    return;
  }
  list.innerHTML = items.slice(0, 8).map((item) => `
    <div class="list-item">
      <strong>Proposal ${escapeHTML(item.proposalId)} | ${escapeHTML(item.vote)}</strong>
      <span>${escapeHTML(item.status || "prepared")} | ${escapeHTML(item.recordedAt || "-")}</span>
      <span class="mono">${escapeHTML(item.txId || item.message || "-")}</span>
    </div>
  `).join("");
}

function renderValidatorOnlyVisibility() {
  const hasValidatorWallet = !!validatorWalletLink();
  document.querySelectorAll("[data-validator-wallet-only]").forEach((node) => {
    node.hidden = !hasValidatorWallet;
  });
}

function renderValidatorWallet() {
  if (!$("validatorWalletMode") && !$("validatorLinkForm")) return;
  const link = validatorWalletLink();
  const entry = validatorEntryForLink(link);
  const locked = $("validatorWalletLocked");
  const panel = $("validatorWalletPanel");
  const verified = !!link;
  if (locked) locked.hidden = verified;
  if (panel) panel.hidden = !verified;
  setText("validatorWalletMode", verified ? "Validator" : "Normal");
  setText("validatorWalletId", link?.validatorId || "-");
  setText("validatorWalletNode", entry ? (entry.online === false ? "Offline" : "Active") : verified ? "Linked" : "-");
  setText("validatorWalletSigner", verified ? "Verified" : "-");
  const heroBadge = $("validatorWalletHeroBadge");
  if (heroBadge) {
    heroBadge.classList.toggle("locked", !verified);
    heroBadge.classList.toggle("verified", verified);
    const text = heroBadge.querySelector("span");
    if (text) text.textContent = verified ? "Verified Validator Wallet" : "Validator tools locked";
  }
  setValue("validatorLinkId", link?.validatorId || "");
  setValue("validatorLinkNode", link?.nodeURL || "");
  setValue("validatorLinkPubkey", link?.consensusPubkey || "");
  if (!verified) {
    setStatus("validatorLinkStatus", state.wallet?.address ? "Waiting" : "Create wallet first", state.wallet?.address ? "" : "warn");
    try {
      const draft = state.wallet?.address && $("validatorLinkId")?.value && $("validatorLinkPubkey")?.value ? buildValidatorLinkFromForm() : null;
      setValue("validatorLinkMessage", draft?.message || "");
    } catch (_) {
      setValue("validatorLinkMessage", "");
    }
    renderValidatorAlerts(null, null);
    return;
  }
  setStatus("validatorLinkStatus", "Linked", "success");
  setText("validatorDashboardTitle", link.validatorId);
  setText("validatorDashboardPubkey", link.consensusPubkey || "-");
  setText("validatorSelfStake", `${formatNumber(validatorStakeOf(entry))} MSC`);
  setText("validatorUptime", percentText(validatorSignedOf(entry)));
  setText("validatorMissedBlocks", formatNumber(entry?.missed_blocks ?? 0));
  setText("validatorReputation", formatNumber(entry?.campaign_reputation_points ?? entry?.validator_reputation ?? entry?.final_score ?? 0));
  setText("validatorSlashCount", formatNumber(entry?.total_slashes ?? 0));
  setText("validatorDelegatorCount", `${formatNumber(entry?.delegator_count ?? entry?.referrals ?? 0)} delegators`);
  setHTML("validatorDelegatorList", `<div class="list-item"><strong>Delegator indexer</strong><span>Delegator list will populate when the public indexer exposes per-validator delegations.</span></div>`);
  setHTML("validatorRewardHistory", `<div class="list-item"><strong>Rewards</strong><span>Current wallet rewards: ${formatNumber(state.walletStatus?.rewards || 0)} MSC</span></div>`);
  setHTML("validatorPublicProfile", `
    <div class="list-item">
      <strong>${escapeHTML(link.validatorId)}</strong>
      <span>Stake ${formatNumber(validatorStakeOf(entry))} MSC | Signed ${percentText(validatorSignedOf(entry))} | Missed ${formatNumber(entry?.missed_blocks ?? 0)}</span>
      <span>Verified wallet ${shortAddress(link.walletAddress)}</span>
    </div>
  `);
  const profileLink = $("validatorPublicProfileLink");
  if (profileLink) profileLink.href = `validators.html?validator=${encodeURIComponent(link.validatorId)}`;
  setText("validatorCertificate", validatorCertificateText(link, entry));
  renderValidatorAlerts(link, entry);
  renderValidatorVoteHistory(link);
  renderValidatorOnlyVisibility();
}

function buildValidatorVotePayload() {
  const link = validatorWalletLink();
  if (!link) throw new Error("Link validator wallet first.");
  const proposalId = $("validatorVoteProposal")?.value.trim() || "";
  const vote = $("validatorVoteChoice")?.value || "YES";
  if (!proposalId) throw new Error("Proposal ID required.");
  const message = ["MSC_VALIDATOR_GOVERNANCE_VOTE_V1", CHAIN_ID, link.validatorId, proposalId, vote].join("|");
  return {
    type: "msc_validator_governance_vote_v1",
    chainId: CHAIN_ID,
    validatorId: link.validatorId,
    walletAddress: link.walletAddress,
    proposalId,
    vote,
    message,
    signing: "Sign this message on the validator node, then submit the signed vote transaction.",
  };
}

function handleValidatorLinkSubmit(event) {
  event.preventDefault();
  try {
    const link = verifyValidatorLink(buildValidatorLinkFromForm());
    saveValidatorLinkForAddress(link.walletAddress, link);
    setStatus("validatorLinkStatus", "Validator wallet linked", "success");
    setText("validatorLinkResult", "Validator proof verified. Special wallet activated.");
    renderValidatorWallet();
  } catch (err) {
    setStatus("validatorLinkStatus", err.message || "Link failed", "error");
    setText("validatorLinkResult", err.message || "Link failed");
  }
}

function handleValidatorVotePrepare(event) {
  event.preventDefault();
  try {
    const payload = buildValidatorVotePayload();
    setHTML("validatorVotePayload", escapeHTML(JSON.stringify(payload, null, 2)));
    setStatus("validatorVoteStatus", "Payload ready", "success");
    saveValidatorVoteHistoryEntry({ ...payload, status: "prepared" });
    renderValidatorVoteHistory();
  } catch (err) {
    setStatus("validatorVoteStatus", err.message || "Vote prepare failed", "error");
  }
}

async function handleValidatorNodeSignVote() {
  try {
    const payload = buildValidatorVotePayload();
    const link = validatorWalletLink();
    if (!link?.nodeURL) throw new Error("Linked node URL required for one-click node signing.");
    setStatus("validatorVoteStatus", "Sending to node signer...", "warn");
    const result = await fetchRPC(normalizeRPC(link.nodeURL), "/governance/vote", {
      method: "POST",
      body: payload,
      timeoutMs: 10000,
    });
    const txId = result?.tx_id || result?.id || result?.hash || "";
    setHTML("validatorVotePayload", escapeHTML(JSON.stringify({ request: payload, response: result }, null, 2)));
    setStatus("validatorVoteStatus", "Node vote submitted", "success");
    saveValidatorVoteHistoryEntry({ ...payload, status: "node-submitted", txId });
    renderValidatorVoteHistory();
  } catch (err) {
    setStatus("validatorVoteStatus", err.message || "Node vote failed", "error");
  }
}

function handleValidatorCommissionPrepare(event) {
  event.preventDefault();
  try {
    const link = validatorWalletLink();
    if (!link) throw new Error("Link validator wallet first.");
    const commission = Number($("validatorCommissionRate")?.value || 0);
    if (!Number.isFinite(commission) || commission < 0 || commission > MAX_RECOMMENDED_COMMISSION_PERCENT) {
      throw new Error(`Commission must be 0-${MAX_RECOMMENDED_COMMISSION_PERCENT}%.`);
    }
    const payload = {
      type: "msc_validator_commission_update_v1",
      chainId: CHAIN_ID,
      validatorId: link.validatorId,
      commissionPercent: commission,
      message: ["MSC_VALIDATOR_COMMISSION_UPDATE_V1", CHAIN_ID, link.validatorId, String(commission)].join("|"),
      signing: "Node/governance signed update required before broadcast.",
    };
    setHTML("validatorCommissionPayload", escapeHTML(JSON.stringify(payload, null, 2)));
    setStatus("validatorCommissionStatus", "Payload ready", "success");
  } catch (err) {
    setStatus("validatorCommissionStatus", err.message || "Commission prepare failed", "error");
  }
}

function initStakingUI() {
  if (!$("stakeForm")) return;
  $("stakeValidatorSelect")?.addEventListener("change", (event) => selectStakeValidator(event.target.value));
  $("stakeValidator")?.addEventListener("input", (event) => {
    const id = event.target.value.trim();
    state.selectedStakeValidatorId = id;
    const validator = findStakeValidator(id);
    if (validatorPubkeyOf(validator)) setValue("stakePubkey", validatorPubkeyOf(validator));
    renderSelectedValidatorCard(validator || (id ? { validator_id: id } : null));
    renderDelegationEstimate();
    renderDelegationValidatorList();
  });
  $("stakePubkey")?.addEventListener("input", () => renderSelectedValidatorCard(currentStakeValidator() || ($("stakeValidator")?.value ? { validator_id: $("stakeValidator").value } : null)));
  $("stakeAmount")?.addEventListener("input", renderDelegationEstimate);
  $("stakeEpochs")?.addEventListener("input", renderDelegationEstimate);
  document.querySelectorAll("[data-stake-amount]").forEach((button) => {
    button.addEventListener("click", () => {
      setValue("stakeAmount", button.dataset.stakeAmount || "100");
      renderDelegationEstimate();
    });
  });
  document.querySelectorAll("[data-stake-epoch-preset]").forEach((button) => {
    button.addEventListener("click", () => {
      setValue("stakeEpochs", button.dataset.stakeEpochPreset || DEFAULT_STAKE_EPOCHS);
      renderDelegationEstimate();
    });
  });
  renderStakingDelegationUI();
}

async function refreshBalance(options = {}) {
  if (!state.wallet?.address) return;
  setText("topWallet", shortAddress(state.wallet.address));
  setText("walletAddress", state.wallet.address);
  setText("walletPublicKey", state.wallet.publicKey || "-");
  setText("receiveAddress", state.wallet.address);
  setValue("sendFrom", state.wallet.address);
  try {
    const balResult = await cachedBalanceAPI(
      state.wallet.address,
      `/balance?address=${encodeURIComponent(state.wallet.address)}&coin=MSC&state=finalized`,
      { ttl: CACHE_TTL.balance, force: !!options.force, cacheOnly: !!options.cacheOnly },
    );
    if (balResult?.data) {
      renderBalanceData(balResult.data, balResult.verification);
    } else if (balResult?.verification && Number(state.balanceHeight || 0) <= 0) {
      renderVerification(balResult.verification);
    }
    if (!options.cacheOnly && balResult?.data) {
      const lightVerification = await verifyBalanceLightProof(state.wallet.address, balResult?.data, balResult?.verification?.rpc || "");
      if (lightVerification) {
        state.balanceVerification = lightVerification;
        renderVerification(lightVerification);
        storeCachedBalanceResult(state.wallet.address, { data: balResult.data, verification: lightVerification });
      }
    }
  } catch (err) {
    if (Number(state.balanceHeight || 0) <= 0) {
      setText("walletBalance", "balance unavailable");
      renderVerification({ mode: "mismatch", checked: 0, matches: 0 });
    }
  }
  try {
    const wsResult = await cachedAPI(
      cacheKey("wallet-status", state.wallet.address),
      `/wallet/status?address=${encodeURIComponent(state.wallet.address)}`,
      { ttl: CACHE_TTL.walletStatus, force: !!options.force, cacheOnly: !!options.cacheOnly, quorum: true },
    );
    renderWalletStatus(wsResult?.data);
  } catch (_) {
    setText("delegations", "No staking status yet");
  }
}

function renderTransactionsData(data) {
  const list = $("transactionsList") || $("latestTx");
  if (!list) return;
  const txs = data?.txs || data?.transactions || data?.items || [];
  list.innerHTML = "";
  if (!txs.length) {
    list.innerHTML = `<div class="list-item">No transactions yet</div>`;
    return;
  }
  txs.slice(0, 10).forEach((tx) => {
    const item = document.createElement("div");
    item.className = "list-item";
    const direction = tx.to === state.wallet?.address ? "Received" : tx.from === state.wallet?.address ? "Sent" : "Activity";
    item.innerHTML = `<strong>${direction} ${formatNumber(tx.amount || 0)} ${tx.coin || "MSC"}</strong><span class="mono">${shortAddress(tx.id || tx.tx_id || "tx")} | ${shortAddress(tx.from || "-")} -> ${shortAddress(tx.to || "-")}</span><span>Fee ${tx.fee || "-"}</span>`;
    list.appendChild(item);
  });
}

async function refreshTransactions(options = {}) {
  if (!state.wallet?.address) return;
  const list = $("transactionsList") || $("latestTx");
  if (!list) return;
  try {
    const result = await cachedAPI(
      cacheKey("txs", state.wallet.address),
      `/txs?address=${encodeURIComponent(state.wallet.address)}`,
      { ttl: CACHE_TTL.txs, force: !!options.force, cacheOnly: !!options.cacheOnly },
    );
    renderTransactionsData(result?.data);
  } catch (err) {
    list.innerHTML = `<div class="list-item">${err.message || "Transaction sync failed"}</div>`;
  }
}

function renderValidatorsData(data) {
  const list = $("validatorList");
  const payload = unwrapDataEnvelope(data) || {};
  state.validatorsData = payload;
  state.validatorLoadState = "ready";
  const vals = validatorEntries();
  renderStakingDelegationUI();
  renderValidatorWallet();
  const campaign = payload?.testnet_campaign || {};
  const campaignEnabled = !!campaign?.enabled;
  const campaignTop = Array.isArray(campaign?.top_validators) ? campaign.top_validators : [];
  setText("validatorActiveCount", formatNumber(payload?.active_count ?? payload?.pool?.active_count ?? "-"));
  setText("validatorHomePCCount", formatNumber(payload?.home_pc_count ?? "-"));
  setText("validatorFounderCount", formatNumber(payload?.founder_count ?? "-"));
  setText("validatorCampaignSeason", campaignEnabled ? (campaign.program_name || campaign.season_id || "Testnet") : "Disabled");
  setText("validatorCampaignStatus", campaignEnabled ? (campaign.status || "active") : "Disabled");
  setText("validatorCampaignLeader", campaignEnabled && campaignTop[0]?.validator_id ? `#1 ${campaignTop[0].validator_id}` : "-");
  if (!list) {
    renderWalletCharts();
    return;
  }
  const filter = state.validatorFilter || "all";
  const filtered = vals.filter((v) => {
    const slot = String(v.slot_type || "").toLowerCase();
    if (filter === "all") return true;
    if (filter === "home_pc") return !!v.home_pc;
    if (filter === "founder") return !!(v.founder_badge || v.founder_eligible);
    return slot === filter;
  });
  list.innerHTML = "";
  if (!filtered.length) {
    list.innerHTML = `<div class="list-item">Validator list unavailable</div>`;
    return;
  }
  filtered.forEach((v) => {
    const id = v.validator_id || v.id || v.validator || v.name || "-";
    const score = Number(v.final_score ?? 0);
    const scoreText = Number.isFinite(score) ? `${Math.round(score * 1000) / 10}%` : "-";
    const signed = Number(v.signed_ratio_bps ?? 0);
    const signedText = Number.isFinite(signed) ? `${Math.round(signed / 100) / 100}%` : "-";
    const slot = v.slot_type || (v.active ? "active" : "standby");
    const online = v.online ? "online" : "offline";
    const founder = v.founder_badge ? "Founder" : v.founder_eligible ? "Founder eligible" : "";
    const home = v.home_pc ? "Home-PC" : "";
    const campaignBadges = Array.isArray(v.campaign_badges) ? v.campaign_badges : [];
    const campaignRank = campaignEnabled && v.campaign_rank ? `Campaign #${v.campaign_rank}` : "";
    const tags = [slot, online, home, founder, campaignRank].filter(Boolean).join(" | ");
    const campaignLine = campaignEnabled
      ? `<span>Campaign ${formatNumber(v.campaign_reputation_points ?? 0)} pts | Weekly #${v.campaign_weekly_rank ?? "-"} | Bug ${formatNumber(v.campaign_bug_points ?? 0)} | Weight ${Math.round(Number(v.campaign_operator_weight_bps ?? 0) / 100)}%</span>`
      : "";
    const usefulLine = campaignEnabled
      ? `<span>Useful node: ${v.campaign_useful_node ? "yes" : "no"} | Raw node points ${formatNumber(v.campaign_raw_node_points ?? 0)}</span>`
      : "";
    const campaignBadgeLine = campaignEnabled
      ? `<span>${campaignBadges.length ? `Badges: ${campaignBadges.join(", ")}` : "Badges: -"}</span>`
      : "";
    const item = document.createElement("div");
    item.className = "list-item";
    item.innerHTML = `
      <strong>#${v.rank ?? "-"} ${id}</strong>
      <span>${tags || "validator"}</span>
      <span>Score ${scoreText} | Signed ${signedText} | CMD ${v.cmd || "-"}</span>
      ${campaignLine}
      ${usefulLine}
      <span>Stake ${formatNumber(v.actual_stake ?? 0)} MSC | Effective ${formatNumber(v.effective_stake ?? 0)} MSC</span>
      <span>Age ${formatNumber(v.validator_age_epochs ?? 0)} epochs | Missed ${formatNumber(v.missed_blocks ?? 0)}</span>
      ${campaignBadgeLine}
      <span>${v.performance_ineligible_reason ? `Performance gate: ${v.performance_ineligible_reason}` : "Performance gate: ok"}</span>`;
    list.appendChild(item);
  });
  renderWalletCharts();
}

async function refreshValidators(options = {}) {
  const list = $("validatorList");
  const needsValidatorCharts = $("walletValidatorScoreChart") || $("walletValidatorStakeChart") || $("stakingValidatorChart");
  const needsDelegationUI = $("delegationValidatorList") || $("stakeValidatorSelect");
  const needsValidatorWallet = $("validatorWalletMode") || $("validatorLinkForm");
  if (!list && !needsValidatorCharts && !needsDelegationUI && !needsValidatorWallet) return;
  state.validatorLoadState = "loading";
  try {
    const result = await cachedAPI("validators", "/v1/validators/leaderboard", { ttl: CACHE_TTL.validators, force: !!options.force, cacheOnly: !!options.cacheOnly });
    renderValidatorsData(result?.data);
  } catch (err) {
    state.validatorLoadState = "error";
    if (!validatorEntries().length) renderStakingDelegationUI();
    renderValidatorWallet();
    if (list) list.innerHTML = `<div class="list-item">${err.message || "Validator sync failed"}</div>`;
  }
}

function renderGovernanceData(data) {
  const list = $("proposalList");
  if (!list) return;
  const proposals = data?.proposals || data?.active_proposals || [];
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
}

async function refreshGovernance(options = {}) {
  const list = $("proposalList");
  if (!list) return;
  try {
    const result = await cachedAPI("governance", "/governance/status", { ttl: CACHE_TTL.governance, force: !!options.force, cacheOnly: !!options.cacheOnly });
    renderGovernanceData(result?.data);
  } catch (err) {
    list.innerHTML = `<div class="list-item">${err.message || "Governance unavailable"}</div>`;
  }
}

function bridgeCollectionCount(value) {
  return Array.isArray(value) ? value.length : Number(value || 0);
}

function bridgeHumanStatus(value) {
  return String(value || "unknown")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function bridgeStatusTone(value) {
  const status = String(value || "").toLowerCase();
  if (["active", "finalized", "completed", "verified"].includes(status)) return "success";
  if (["paused", "stalled", "failed", "disabled"].includes(status)) return "error";
  return "warn";
}

function renderBridgeData(data, routeData = {}) {
  if (!$("bridgeStatus")) return;
  const operational = !!data?.operational;
  const paused = data?.paused !== false;
  setStatus("bridgeStatus", operational ? "Operational" : paused ? "Paused" : data?.enabled ? "Not Ready" : "Verification Only", operational ? "success" : paused ? "error" : "warn");
  setText("bridgeMode", data?.mode || "disabled");
  setText("bridgeChains", formatNumber(bridgeCollectionCount(data?.registered_chains)));
  setText("bridgeAssets", formatNumber(bridgeCollectionCount(data?.registered_assets)));
  setText("bridgeReadyRoutes", formatNumber(routeData?.ready_routes || 0));
  const checkpointChains = new Set((routeData?.routes || []).filter((route) => route.latest_checkpoint_id).map((route) => route.chain_id));
  setText("bridgeCheckpointChains", formatNumber(checkpointChains.size));
  setText("bridgeFuture", operational ? "At least one proof-gated route is ready" : "Light-client verified transfer pending");
  setText("bridgePauseReason", data?.pause_reason || (operational ? "Proof, finality, quorum, and route checks passed." : "No active route has passed every security gate."));
  const banner = $("bridgeSafetyBanner");
  if (banner) {
    banner.classList.toggle("success", operational);
    banner.classList.toggle("warn", !operational && !paused);
    banner.classList.toggle("error", paused);
    const title = banner.querySelector("strong");
    if (title) title.textContent = operational ? "Bridge route available" : paused ? "Emergency pause active" : "Bridge setup incomplete";
  }
}

function renderBridgeRoutes(payload = {}) {
  const routes = Array.isArray(payload?.routes) ? payload.routes : [];
  state.bridgeRoutes = routes;
  const select = $("bridgeRouteSelect");
  if (select) {
    const selected = select.value;
    select.innerHTML = `<option value="">Select network</option>${routes.map((route) => {
      const label = `${route.chain_name || route.chain_id} - ${route.asset_symbol || route.asset_denom}${route.ready ? "" : ` (${bridgeHumanStatus(route.unavailable_reason || route.status)})`}`;
      return `<option value="${escapeHTML(route.route_id)}" ${route.ready ? "" : "disabled"}>${escapeHTML(label)}</option>`;
    }).join("")}`;
    if (routes.some((route) => route.ready && route.route_id === selected)) select.value = selected;
  }
  const withdrawSelect = $("bridgeWithdrawRouteSelect");
  if (withdrawSelect) {
    const selected = withdrawSelect.value;
    withdrawSelect.innerHTML = `<option value="">Select destination network</option>${routes.map((route) => {
      const label = `${route.chain_name || route.chain_id} - ${route.local_denom || route.asset_symbol}${route.ready ? "" : ` (${bridgeHumanStatus(route.unavailable_reason || route.status)})`}`;
      return `<option value="${escapeHTML(route.route_id)}" ${route.ready ? "" : "disabled"}>${escapeHTML(label)}</option>`;
    }).join("")}`;
    if (routes.some((route) => route.ready && route.route_id === selected)) withdrawSelect.value = selected;
  }
  const list = $("bridgeRouteList");
  if (list) {
    list.innerHTML = routes.length ? routes.map((route) => `
      <div class="bridge-route-row ${route.ready ? "success" : "warn"}">
        <div class="bridge-route-identity"><strong>${escapeHTML(route.chain_name || route.chain_id || "Unknown network")}</strong><span>${escapeHTML(route.asset_symbol || route.asset_denom || "Asset")} to ${escapeHTML(route.local_denom || "MSC")}${route.checkpoint_height ? ` / checkpoint #${escapeHTML(route.checkpoint_height)}` : " / no checkpoint"}</span></div>
        <div><span>Finality</span><strong class="${bridgeStatusTone(route.finality_status)}">${escapeHTML(bridgeHumanStatus(route.finality_status))}${route.checkpoint_height ? ` @ ${escapeHTML(route.checkpoint_height)}` : ""}</strong></div>
        <div><span>Confirmations</span><strong>${escapeHTML(route.min_confirmations || "-")}</strong></div>
        <div><span>Limit</span><strong>${escapeHTML(route.daily_limit || "Not set")}</strong></div>
        <div><span>Status</span><strong class="${route.ready ? "success" : "warn"}">${escapeHTML(route.ready ? "Ready" : bridgeHumanStatus(route.unavailable_reason || route.status))}</strong></div>
      </div>`).join("") : `<div class="list-item">No bridge routes are registered.</div>`;
  }
  renderSelectedBridgeRoute();
  renderSelectedBridgeWithdrawalRoute();
}

function selectedBridgeRoute() {
  const routeId = $("bridgeRouteSelect")?.value || "";
  return state.bridgeRoutes.find((route) => route.route_id === routeId) || null;
}

function renderSelectedBridgeRoute() {
  const route = selectedBridgeRoute();
  const summary = $("bridgeRouteSummary");
  setText("bridgeAmountSymbol", route?.asset_symbol || "USDT");
  if (!summary) return;
  if (!route) {
    summary.innerHTML = `<span>Select an available route to review finality, confirmations, and limits.</span>`;
    return;
  }
  summary.innerHTML = `
    <div><span>Finality</span><strong class="${bridgeStatusTone(route.finality_status)}">${escapeHTML(bridgeHumanStatus(route.finality_status))}</strong></div>
    <div><span>Minimum</span><strong>${escapeHTML(route.min_deposit || "Not set")} ${escapeHTML(route.asset_symbol || "")}</strong></div>
    <div><span>24h limit</span><strong>${escapeHTML(route.daily_limit || "Not set")}</strong></div>
    <div><span>Local asset</span><strong>${escapeHTML(route.local_denom || "-")}</strong></div>
    <div><span>Checkpoint</span><strong>${escapeHTML(route.checkpoint_height ? `#${route.checkpoint_height}` : "Unavailable")}</strong></div>`;
}

function selectedBridgeWithdrawalRoute() {
  const routeId = $("bridgeWithdrawRouteSelect")?.value || "";
  return state.bridgeRoutes.find((route) => route.route_id === routeId) || null;
}

function renderSelectedBridgeWithdrawalRoute() {
  const route = selectedBridgeWithdrawalRoute();
  const summary = $("bridgeWithdrawRouteSummary");
  setText("bridgeWithdrawSymbol", route?.asset_symbol || "USDT");
  if (!summary) return;
  if (!route) {
    summary.innerHTML = `<span>Select an available route to review finality, limits, and the wrapped MSC asset.</span>`;
    return;
  }
  summary.innerHTML = `
    <div><span>Destination</span><strong>${escapeHTML(route.chain_name || route.chain_id)}</strong></div>
    <div><span>Minimum</span><strong>${escapeHTML(route.min_deposit || "Not set")} ${escapeHTML(route.asset_symbol || "")}</strong></div>
    <div><span>24h limit</span><strong>${escapeHTML(route.daily_limit || "Not set")}</strong></div>
    <div><span>Burn asset</span><strong>${escapeHTML(route.local_denom || "-")}</strong></div>
    <div><span>Checkpoint</span><strong>${escapeHTML(route.checkpoint_height ? `#${route.checkpoint_height}` : "Unavailable")}</strong></div>`;
}

function bridgeHistoryState(status) {
  const normalized = String(status || "").toLowerCase();
  if (normalized === "completed") return "completed";
  if (["failed", "cancelled", "refunded", "expired"].includes(normalized)) return "closed";
  return "pending";
}

function renderBridgeHistory() {
  const list = $("bridgeHistory");
  if (!list) return;
  const filter = state.bridgeHistoryFilter;
  const items = state.bridgeHistory.filter((item) => filter === "all" || bridgeHistoryState(item.status) === filter);
  if (!state.wallet?.address) {
    list.innerHTML = `<div class="list-item">Create or load a wallet to view bridge activity.</div>`;
    return;
  }
  if (!items.length) {
    list.innerHTML = `<div class="list-item">No ${filter === "all" ? "bridge" : filter} activity for this wallet.</div>`;
    return;
  }
  list.innerHTML = items.map((item) => `
    <div class="bridge-history-row">
      <div><strong>${escapeHTML(item.asset_symbol || item.asset_denom || "Bridge asset")}</strong><span>${escapeHTML(item.chain_name || item.source_chain_id || item.route_id || "Route")}</span></div>
      <div><span>Amount</span><strong>${escapeHTML(item.amount || item.requested_amount || "-")}</strong></div>
      <div><span>Created</span><strong>${escapeHTML(formatDateTime((item.created_at_unix || 0) * 1000))}</strong></div>
      <div><span>Status</span><strong class="${bridgeStatusTone(item.status)}">${escapeHTML(bridgeHumanStatus(item.status))}</strong></div>
    </div>`).join("");
}

async function refreshBridgeHistory() {
  if (!$("bridgeHistory")) return;
  if (!state.wallet?.address) {
    state.bridgeHistory = [];
    renderBridgeHistory();
    return;
  }
  try {
    const data = await api(`/bridge/transfers?recipient=${encodeURIComponent(state.wallet.address)}`);
    const intents = Array.isArray(data?.deposit_intents) ? data.deposit_intents : [];
    const transfers = Array.isArray(data?.transfers) ? data.transfers : [];
    state.bridgeHistory = [...transfers, ...intents.filter((intent) => !transfers.some((transfer) => transfer.intent_id === intent.intent_id))]
      .sort((a, b) => Number(b.created_at_unix || 0) - Number(a.created_at_unix || 0));
    renderBridgeHistory();
  } catch (err) {
    $("bridgeHistory").innerHTML = `<div class="list-item">${escapeHTML(err.message || "Bridge history unavailable")}</div>`;
  }
}

async function refreshBridge(options = {}) {
  if (!$("bridgeStatus")) return;
  try {
    const [statusResult, routesResult] = await Promise.all([
      cachedAPI("bridge-status", "/bridge/status", { ttl: CACHE_TTL.bridge, force: !!options.force, cacheOnly: !!options.cacheOnly }),
      cachedAPI("bridge-routes", "/bridge/routes", { ttl: CACHE_TTL.bridge, force: !!options.force, cacheOnly: !!options.cacheOnly }),
    ]);
    renderBridgeData(statusResult?.data, routesResult?.data);
    renderBridgeRoutes(routesResult?.data);
    if (!options.cacheOnly) await refreshBridgeHistory();
  } catch (err) {
    setStatus("bridgeStatus", "Unavailable", "error");
    setText("bridgeFuture", err.message || "Bridge status failed");
    setText("bridgePauseReason", err.message || "Bridge gateway unavailable");
  }
}

async function createBridgeDeposit(event) {
  event.preventDefault();
  const route = selectedBridgeRoute();
  const status = $("bridgeDepositStatus");
  if (!route?.ready) return setText("bridgeDepositStatus", "Select an active bridge route first.");
  if (!$("bridgeRiskConfirm")?.checked) return setText("bridgeDepositStatus", "Confirm the network and finality warning first.");
  const button = $("createBridgeDeposit");
  if (button) button.disabled = true;
  setText("bridgeDepositStatus", "Creating a proof-bound deposit intent...");
  try {
    const intent = await api("/bridge/deposits", {
      method: "POST",
      body: {
        route_id: route.route_id,
        recipient: $("bridgeRecipient")?.value.trim() || state.wallet?.address || "",
        amount: $("bridgeDepositAmount")?.value.trim() || "",
      },
    });
    if (status) status.innerHTML = `
      <div class="bridge-deposit-instructions">
        <strong>Deposit intent ${escapeHTML(intent.intent_id)}</strong>
        <span>${intent.deposit_mode === "contract_call" ? "Use the audited bridge contract call. Do not send a plain token transfer to this address." : "Send only the selected token on the selected source network."}</span>
        <div><span>Gateway</span><code>${escapeHTML(intent.deposit_address || "-")}</code><button type="button" data-copy-bridge="${escapeHTML(intent.deposit_address || "")}" title="Copy gateway address"><i data-lucide="copy"></i></button></div>
        <div><span>Token</span><code>${escapeHTML(intent.token_contract || "-")}</code><button type="button" data-copy-bridge="${escapeHTML(intent.token_contract || "")}" title="Copy token contract"><i data-lucide="copy"></i></button></div>
        ${intent.memo ? `<div><span>Memo</span><code>${escapeHTML(intent.memo)}</code><button type="button" data-copy-bridge="${escapeHTML(intent.memo)}" title="Copy memo"><i data-lucide="copy"></i></button></div>` : ""}
        <em>Expires ${escapeHTML(formatDateTime(Number(intent.expires_at_unix || 0) * 1000))}</em>
      </div>`;
    if (window.lucide) window.lucide.createIcons();
    await refreshBridgeHistory();
  } catch (err) {
    setText("bridgeDepositStatus", err.message || "Deposit intent failed");
  } finally {
    if (button) button.disabled = false;
  }
}

async function createBridgeWithdrawal(event) {
  event.preventDefault();
  const route = selectedBridgeWithdrawalRoute();
  if (!route?.ready) return setText("bridgeWithdrawStatus", "Select an active destination route first.");
  if (!state.wallet?.address || !state.wallet?.publicKey || !state.secretKey) return setText("bridgeWithdrawStatus", "Unlock your wallet before signing a withdrawal.");
  if (!$("bridgeWithdrawRiskConfirm")?.checked) return setText("bridgeWithdrawStatus", "Confirm the permanent burn and destination warning first.");
  const amount = $("bridgeWithdrawAmount")?.value.trim() || "";
  const externalRecipient = $("bridgeExternalRecipient")?.value.trim() || "";
  if (!amount || !externalRecipient) return setText("bridgeWithdrawStatus", "Amount and destination address are required.");
  const button = $("createBridgeWithdrawal");
  if (button) button.disabled = true;
  setText("bridgeWithdrawStatus", "Signing withdrawal authorization...");
  try {
    const requestNonce = bytesToHex(crypto.getRandomValues(new Uint8Array(32)));
    const expiresAtUnix = Math.floor(Date.now() / 1000) + 300;
    const message = [
      "MSC", "BRIDGE_WITHDRAWAL_REQUEST", BRIDGE_GATEWAY_VERSION,
      String(route.route_id || "").trim().toLowerCase(),
      state.wallet.address,
      externalRecipient,
      amount,
      requestNonce,
      String(expiresAtUnix),
    ].join("|");
    const authorizationSignature = nacl.sign.detached(enc.encode(message), state.secretKey);
    const prepared = await api("/bridge/withdrawals", {
      method: "POST",
      body: {
        route_id: route.route_id,
        sender: state.wallet.address,
        external_recipient: externalRecipient,
        amount,
        request_nonce: requestNonce,
        expires_at_unix: expiresAtUnix,
        public_key: state.wallet.publicKey,
        signature: bytesToHex(authorizationSignature),
      },
    });
    const template = { ...(prepared.transaction_template || {}), publicKey: state.wallet.publicKey };
    setText("bridgeWithdrawStatus", "Signing and submitting the consensus burn...");
    const signed = await signTx(template);
    await api("/submitTx", { method: "POST", body: signed });
    const status = $("bridgeWithdrawStatus");
    if (status) status.innerHTML = `
      <div class="bridge-deposit-instructions">
        <strong>Burn submitted</strong>
        <span>The bridge will release funds only after this MSC burn commits and bridge validators authorize the external unlock.</span>
        <div><span>Transfer</span><code>${escapeHTML(prepared.transfer?.transfer_id || "-")}</code><button type="button" data-copy-bridge="${escapeHTML(prepared.transfer?.transfer_id || "")}" title="Copy transfer ID"><i data-lucide="copy"></i></button></div>
        <div><span>MSC transaction</span><code>${escapeHTML(signed.id)}</code><button type="button" data-copy-bridge="${escapeHTML(signed.id)}" title="Copy transaction ID"><i data-lucide="copy"></i></button></div>
      </div>`;
    if (window.lucide) window.lucide.createIcons();
    invalidateNetworkCache();
    await refreshBridgeHistory();
  } catch (err) {
    setText("bridgeWithdrawStatus", err.message || "Withdrawal preparation failed");
  } finally {
    if (button) button.disabled = false;
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

function loadFaucetCooldowns() {
  try {
    return JSON.parse(localStorage.getItem(FAUCET_COOLDOWN_KEY) || "{}") || {};
  } catch (_) {
    return {};
  }
}

function saveFaucetCooldown(address, until) {
  const key = String(address || "").trim().toLowerCase();
  if (!key) return;
  const items = loadFaucetCooldowns();
  items[key] = Number(until || 0);
  localStorage.setItem(FAUCET_COOLDOWN_KEY, JSON.stringify(items));
}

function faucetCooldownRemaining(address) {
  const key = String(address || "").trim().toLowerCase();
  if (!key) return 0;
  return Math.max(0, Number(loadFaucetCooldowns()[key] || 0) - Date.now());
}

function isTestnetWalletNetwork() {
  if (state.status?.is_testnet === true) return true;
  if (state.status?.is_testnet === false) return false;
  const network = String(state.status?.network_name || "").toLowerCase();
  if (network.includes("main")) return false;
  if (network.includes("test")) return true;
  return true;
}

function renderFaucetState() {
  if (!$("faucetForm")) return;
  const input = $("faucetAddress");
  if (input && !input.value && state.wallet?.address) input.value = state.wallet.address;
  const address = input?.value.trim() || state.wallet?.address || "";
  const remaining = faucetCooldownRemaining(address);
  const testnet = isTestnetWalletNetwork();
  setText("faucetNetwork", state.status?.network_name || (testnet ? "MSC Testnet" : "MSC Mainnet"));
  setValue("faucetAmount", `${FAUCET_AMOUNT} MSC`);
  setValue("faucetCooldown", remaining > 0 ? formatDuration(remaining) : "Ready");
  const button = $("requestFaucet");
  if (button) {
    button.disabled = !testnet || !address || remaining > 0;
    button.title = !testnet ? "Faucet only works on MSC testnet" : remaining > 0 ? `Try again in ${formatDuration(remaining)}` : "Request test MSC";
  }
  const statusNode = $("faucetStatus");
  if (!statusNode) return;
  if (!testnet) setStatus("faucetStatus", "Faucet is disabled on MSC Mainnet. Switch to MSC testnet.", "warn");
  else if (remaining > 0) setStatus("faucetStatus", `Cooldown active. Next request in ${formatDuration(remaining)}.`, "warn");
  else if (!statusNode.textContent.trim() || /cooldown|mainnet/i.test(statusNode.textContent)) setStatus("faucetStatus", "Ready", "success");
}

async function requestFaucet(event) {
  event.preventDefault();
  const address = $("faucetAddress")?.value.trim() || state.wallet?.address || "";
  const check = $("faucetHumanCheck")?.value.trim().toUpperCase() || "";
  if (!address) return setStatus("faucetStatus", "Enter or unlock a wallet address first.", "error");
  if (!isTestnetWalletNetwork()) return setStatus("faucetStatus", "Faucet is disabled on MSC Mainnet. Use MSC testnet only.", "warn");
  const remaining = faucetCooldownRemaining(address);
  if (remaining > 0) return setStatus("faucetStatus", `Cooldown active. Next request in ${formatDuration(remaining)}.`, "warn");
  if (check !== "MSC") return setStatus("faucetStatus", "Human check failed. Type MSC to continue.", "error");
  const button = $("requestFaucet");
  button?.setAttribute("disabled", "disabled");
  setStatus("faucetStatus", "Submitting faucet transaction...", "");
  setHTML("faucetResult", "");
  try {
    const ambassadorReferralCode = currentAmbassadorReferralCode();
    const body = { address, amount: FAUCET_AMOUNT, coin: "MSC" };
    if (ambassadorReferralCode) body.ambassador_referral_code = ambassadorReferralCode;
    const result = await api("/faucet", { method: "POST", body });
    const txID = result?.tx_id || result?.id || "";
    const cooldownSeconds = Number(result?.cooldown_seconds || FAUCET_COOLDOWN_MS / 1000);
    saveFaucetCooldown(address, Date.now() + cooldownSeconds * 1000);
    setStatus("faucetStatus", `Submitted ${formatNumber(result?.amount || FAUCET_AMOUNT)} ${result?.coin || "MSC"}.`, "success");
    setHTML("faucetResult", txID
      ? `<a class="button" href="https://explorer.mscblockexplorer.in/explorer-transactions.html?q=${encodeURIComponent(txID)}"><i data-lucide="external-link"></i>View faucet transaction</a><span class="mono">${escapeHTML(txID)}</span>`
      : `<span class="mono">Faucet transaction submitted.</span>`);
    if ($("faucetHumanCheck")) $("faucetHumanCheck").value = "";
    refreshBalance({ force: true }).catch(() => {});
    refreshTransactions({ force: true }).catch(() => {});
    window.lucide?.createIcons();
  } catch (err) {
    const message = err?.message || "Faucet request failed";
    setStatus("faucetStatus", message, /mainnet|cooldown|rate limit/i.test(message) ? "warn" : "error");
  } finally {
    button?.removeAttribute("disabled");
    renderFaucetState();
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
	// Preserve the historical wire layout with permanently empty VM slots.
	pushInt64(parts, 0);
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
  return {
    ...tx,
    signature: bytesToHex(sig),
    id: bytesToHex(id),
    ChainID: CHAIN_ID,
    Coin: tx.coin || tx.Coin || "MSC",
    Type: Number(tx.type ?? tx.Type ?? 0),
  };
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

function normalizeMSCAddress(value) {
  const match = String(value || "").trim().match(/^msc([0-9a-fA-F]{42})$/i);
  return match ? `MSC${match[1]}` : "";
}

function isMSCAddress(value) {
  return Boolean(normalizeMSCAddress(value));
}

function parseOptionalQRAmount(value) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") return null;
  if (!/^[1-9][0-9]*$/.test(text)) throw new Error("QR amount must be a positive whole number.");
  const amount = Number(text);
  if (!Number.isSafeInteger(amount)) throw new Error("QR amount is too large.");
  return amount;
}

function normalizeQRTokenSymbol(value) {
  const symbol = String(value || "MSC").trim().toUpperCase();
  if (!/^[A-Z][A-Z0-9._-]{0,15}$/.test(symbol)) throw new Error("QR contains an invalid coin symbol.");
  return symbol;
}

function validatedWalletQRPayload({ address, amount, coin, chain, kind, raw }) {
  const normalizedAddress = normalizeMSCAddress(address);
  if (!normalizedAddress) throw new Error("QR does not contain a valid MSC address.");
  const normalizedChain = String(chain || "").trim();
  if (normalizedChain && normalizedChain !== CHAIN_ID) throw new Error(`QR is for MSC chain ${normalizedChain}, not ${CHAIN_ID}.`);
  return {
    address: normalizedAddress,
    amount: parseOptionalQRAmount(amount),
    coin: normalizeQRTokenSymbol(coin),
    chain: normalizedChain || CHAIN_ID,
    kind: String(kind || "address").trim().toLowerCase(),
    raw: String(raw || ""),
  };
}

function parseWalletQRPayload(raw) {
  const text = String(raw || "").trim();
  if (!text) throw new Error("QR code is empty.");
  if (text.startsWith("{")) {
    let data;
    try {
      data = JSON.parse(text);
    } catch (_) {
      throw new Error("QR contains invalid JSON.");
    }
    return validatedWalletQRPayload({
      address: data.address || data.to || data.recipient,
      amount: data.amount,
      coin: data.coin || data.asset,
      chain: data.chain || data.chainId || data.chain_id,
      kind: data.kind || data.type,
      raw: text,
    });
  }
  if (/^msc:/i.test(text)) {
    let url;
    try {
      url = new URL(text);
    } catch (_) {
      throw new Error("QR contains an invalid MSC payment URI.");
    }
    const kind = url.hostname.toLowerCase();
    if (kind && !["pay", "wallet", "address"].includes(kind)) throw new Error("QR contains an unsupported MSC request type.");
    const fromPath = url.pathname.replace(/^\/+/, "");
    return validatedWalletQRPayload({
      address: url.searchParams.get("address") || url.searchParams.get("to") || url.searchParams.get("recipient") || fromPath,
      amount: url.searchParams.get("amount"),
      coin: url.searchParams.get("coin") || url.searchParams.get("asset"),
      chain: url.searchParams.get("chain") || url.searchParams.get("chain_id"),
      kind,
      raw: text,
    });
  }
  if (isMSCAddress(text)) {
    return validatedWalletQRPayload({ address: text, coin: "MSC", chain: CHAIN_ID, kind: "address", raw: text });
  }
  throw new Error("QR is not a valid MSC address or payment request.");
}

function nativeQRDetectorAvailable() {
  return typeof window.BarcodeDetector === "function";
}

async function qrDetector() {
  if (!nativeQRDetectorAvailable() || state.qrScan.detectorFailed) return null;
  try {
    if (!state.qrScan.detector) state.qrScan.detector = new BarcodeDetector({ formats: ["qr_code"] });
  } catch (_) {
    state.qrScan.detectorFailed = true;
    return null;
  }
  return state.qrScan.detector;
}

function fallbackQRDetectorAvailable() {
  return typeof window.jsQR === "function";
}

function qrDetectorAvailable() {
  return nativeQRDetectorAvailable() || fallbackQRDetectorAvailable();
}

function qrDecoderName() {
  if (nativeQRDetectorAvailable() && !state.qrScan.detectorFailed) return "Native QR";
  if (fallbackQRDetectorAvailable()) return "Canvas QR";
  return "No QR decoder";
}

function showQRScanResult(parsed, source = "QR") {
  const box = $("qrScanResult");
  if (!box) return;
  box.innerHTML = `
    <div class="qr-scan-result-card">
      <span>${escapeHTML(source)} detected</span>
      <strong class="mono">${escapeHTML(parsed.address)}</strong>
      ${parsed.amount ? `<em>Amount ${formatNumber(parsed.amount)} ${escapeHTML(parsed.coin || "MSC")}</em>` : "<em>No amount in QR</em>"}
      <em>MSC Chain ${escapeHTML(parsed.chain || CHAIN_ID)}</em>
      <button type="button" data-fill-qr-address="${escapeHTML(parsed.address)}" data-fill-qr-amount="${parsed.amount || ""}" data-fill-qr-coin="${escapeHTML(parsed.coin || "MSC")}" data-fill-qr-chain="${escapeHTML(parsed.chain || CHAIN_ID)}">Use QR Details</button>
    </div>`;
  setStatus("qrScanStatus", "QR found", "success");
}

function applyQRPayload(parsed) {
  const payload = validatedWalletQRPayload(parsed);
  setValue("sendTo", payload.address);
  if (payload.amount) setValue("sendAmount", payload.amount);
  if (payload.coin) setValue("sendCoin", payload.coin);
  const amount = Number($("sendAmount")?.value || 0);
  setText("sendFee", `${computeFee(amount)} MSC`);
  setText("sendTotal", `${amount + computeFee(amount)} MSC`);
  setStatus("qrScanStatus", "Address filled", "success");
  setStatus("sendStatus", "Recipient filled from QR. Review before sending.", "warn");
}

function drawQRSourceToCanvas(source) {
  if (!fallbackQRDetectorAvailable()) throw new Error("QR fallback decoder missing. Refresh the page and try again.");
  const width = Number(source.videoWidth || source.naturalWidth || source.width || 0);
  const height = Number(source.videoHeight || source.naturalHeight || source.height || 0);
  if (!width || !height) throw new Error("QR image not ready.");
  const scale = Math.min(1, QR_SCAN_MAX_SIZE / Math.max(width, height));
  const canvas = $("qrScanCanvas") || document.createElement("canvas");
  const targetWidth = Math.max(1, Math.round(width * scale));
  const targetHeight = Math.max(1, Math.round(height * scale));
  canvas.width = targetWidth;
  canvas.height = targetHeight;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (!ctx) throw new Error("Canvas QR decode unavailable.");
  ctx.drawImage(source, 0, 0, targetWidth, targetHeight);
  return ctx.getImageData(0, 0, targetWidth, targetHeight);
}

function detectQRCodeWithFallback(source) {
  const imageData = drawQRSourceToCanvas(source);
  const code = window.jsQR(imageData.data, imageData.width, imageData.height, { inversionAttempts: "attemptBoth" });
  const value = code?.data || "";
  if (!value) throw new Error("No QR code found.");
  return parseWalletQRPayload(value);
}

async function detectQRCodeFromSource(source) {
  const detector = await qrDetector();
  if (detector) {
    try {
      const codes = await detector.detect(source);
      const value = codes?.[0]?.rawValue || "";
      if (value) return parseWalletQRPayload(value);
    } catch (_) {
      state.qrScan.detectorFailed = true;
    }
  }
  return detectQRCodeWithFallback(source);
}

function imageFromFile(file) {
  if (window.createImageBitmap) return createImageBitmap(file);
  return new Promise((resolve, reject) => {
    const image = new Image();
    const url = URL.createObjectURL(file);
    image.onload = () => {
      URL.revokeObjectURL(url);
      resolve(image);
    };
    image.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("Could not load QR image."));
    };
    image.src = url;
  });
}

function validateQRImageFile(file) {
  if (!QR_IMAGE_TYPES.has(String(file?.type || "").toLowerCase())) throw new Error("Upload a PNG, JPG, or WebP QR image.");
  if (!Number(file.size) || file.size > QR_IMAGE_MAX_BYTES) throw new Error("QR image must be smaller than 8 MB.");
}

function stopQRScan() {
  state.qrScan.active = false;
  if (state.qrScan.raf) cancelAnimationFrame(state.qrScan.raf);
  state.qrScan.raf = 0;
  if (state.qrScan.stream) {
    state.qrScan.stream.getTracks().forEach((track) => track.stop());
    state.qrScan.stream = null;
  }
  const video = $("qrScanVideo");
  if (video) {
    video.pause();
    video.srcObject = null;
    video.hidden = true;
  }
  const start = $("startQrScan");
  const stop = $("stopQrScan");
  if (start) {
    start.hidden = false;
    start.disabled = false;
  }
  if (stop) stop.hidden = true;
}

async function scanQRFrame() {
  if (!state.qrScan.active) return;
  const video = $("qrScanVideo");
  try {
    if (video?.readyState >= 2) {
      const parsed = await detectQRCodeFromSource(video);
      showQRScanResult(parsed, "Camera QR");
      stopQRScan();
      return;
    }
  } catch (err) {
    if (!/No QR code found/i.test(err.message || "")) setStatus("qrScanStatus", err.message || "Scan failed", "error");
  }
  state.qrScan.raf = requestAnimationFrame(scanQRFrame);
}

async function startQRScan() {
  try {
    if (!qrDetectorAvailable()) throw new Error("QR scanner unavailable. Upload QR works after decoder loads.");
    if (!window.isSecureContext) throw new Error("Camera scan requires HTTPS or localhost.");
    if (!navigator.mediaDevices?.getUserMedia) throw new Error("Camera access unavailable.");
    const video = $("qrScanVideo");
    if (!video) return;
    stopQRScan();
    $("startQrScan").disabled = true;
    state.qrScan.stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false });
    state.qrScan.stream.getVideoTracks()[0]?.addEventListener("ended", stopQRScan, { once: true });
    video.srcObject = state.qrScan.stream;
    video.hidden = false;
    await video.play();
    state.qrScan.active = true;
    $("startQrScan").hidden = true;
    $("stopQrScan").hidden = false;
    setStatus("qrScanStatus", `Scanning (${qrDecoderName()})`, "warn");
    state.qrScan.raf = requestAnimationFrame(scanQRFrame);
  } catch (err) {
    stopQRScan();
    setStatus("qrScanStatus", err.message || "Camera scan failed", "error");
  }
}

async function handleQRImageUpload(event) {
  const file = event.target.files?.[0];
  if (!file) return;
  let image = null;
  try {
    stopQRScan();
    validateQRImageFile(file);
    if (!qrDetectorAvailable()) throw new Error("QR decoder unavailable. Refresh this page and try again.");
    image = await imageFromFile(file);
    const parsed = await detectQRCodeFromSource(image);
    showQRScanResult(parsed, "Uploaded QR");
  } catch (err) {
    setStatus("qrScanStatus", err.message || "QR upload failed", "error");
    setText("qrScanResult", err.message || "QR upload failed");
  } finally {
    image?.close?.();
    event.target.value = "";
  }
}

async function handleSend(event) {
  event.preventDefault();
  try {
    if (!state.wallet?.address) throw new Error("Create or unlock wallet first.");
    if (isHardwareWallet()) throw new Error("Hardware wallet signing is required. Connect your device to approve this transaction.");
    if (!state.secretKey) throw new Error("Unlock wallet before sending.");
    const amount = Number($("sendAmount").value || 0);
    if (!Number.isSafeInteger(amount) || amount <= 0) throw new Error("Enter a positive whole-number amount.");
    const recipient = normalizeMSCAddress($("sendTo").value);
    if (!recipient) throw new Error("Enter a valid MSC recipient address.");
    const tx = {
      from: state.wallet.address,
      to: recipient,
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
    refreshBalance({ force: true });
    refreshTransactions({ force: true });
  } catch (err) {
    setStatus("sendStatus", err.message || "Send failed", "error");
  }
}

async function handleStake(event) {
  event.preventDefault();
  try {
    if (!state.wallet?.address) throw new Error("Create or unlock wallet first.");
    if (isHardwareWallet()) throw new Error("Hardware wallet signing is required before staking.");
    if (!state.secretKey) throw new Error("Unlock wallet before staking.");
    const amount = Number($("stakeAmount").value || 0);
    const validator = currentStakeValidator();
    const validatorId = validatorIdOf(validator);
    const validatorPubkey = validatorPubkeyOf(validator);
    const stakeEpochs = Number($("stakeEpochs")?.value || DEFAULT_STAKE_EPOCHS);
    if (!Number.isFinite(amount) || amount <= 0) throw new Error("Enter a valid amount.");
    if (!validator || !validatorIsRecommendationEligible(validator)) throw new Error("Recommended validator unavailable. Refresh validator list before staking.");
    if (!Number.isFinite(stakeEpochs) || stakeEpochs <= 0) throw new Error("Enter a valid lock period.");
    if ($("stakeRiskAgreement") && !$("stakeRiskAgreement").checked) throw new Error("Confirm staking risk before delegating.");
    const tx = {
      from: state.wallet.address,
      to: validatorId,
      amount,
      nonce: await nextNonce(),
      publicKey: state.wallet.publicKey,
      fee: computeFee(amount),
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 2,
      stake_epochs: stakeEpochs,
      coin: "MSC",
      validator_pubkey: validatorPubkey,
    };
    await submitSignedTx(tx);
    setStatus("stakeStatus", "Stake submitted", "success");
    refreshBalance({ force: true });
    refreshValidators({ force: true });
  } catch (err) {
    setStatus("stakeStatus", err.message || "Stake failed", "error");
  }
}

async function handleUnstake(event) {
  event.preventDefault();
  try {
    if (!state.wallet?.address) throw new Error("Create or unlock wallet first.");
    if (isHardwareWallet()) throw new Error("Hardware wallet signing is required before unstaking.");
    if (!state.secretKey) throw new Error("Unlock wallet before unstaking.");
    const amount = Number($("unstakeAmount").value || 0);
    const validatorId = $("unstakeValidator")?.value.trim() || "";
    if (!Number.isFinite(amount) || amount <= 0) throw new Error("Enter a valid amount.");
    if (!validatorId) throw new Error("Enter validator ID before unstaking.");
    const tx = {
      from: state.wallet.address,
      to: validatorId,
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
    refreshBalance({ force: true });
    refreshValidators({ force: true });
  } catch (err) {
    setStatus("unstakeStatus", err.message || "Unstake failed", "error");
  }
}

async function createWallet(event) {
  event.preventDefault();
  try {
    if (!window.nacl?.sign?.keyPair?.fromSeed) throw new Error("seed wallet generator unavailable");
    const password = $("createPassword").value.trim();
    if (!password) throw new Error("Password required");
    if (password.length < 8) throw new Error("Use at least 8 characters.");
    const words = randomMnemonic(12);
    const generated = await walletFromMnemonic(words);
    const wallet = await buildRecoveryWallet({
      address: generated.address,
      publicKey: generated.publicKey,
      secretKey: generated.keyPair.secretKey,
      password,
      words,
      source: "create-form",
    });
    saveWallet(wallet);
    state.secretKey = generated.keyPair.secretKey;
    saveSessionUnlock(state.secretKey, state.wallet);
    setHTML("createResult", `Wallet created successfully.<br><strong>${wallet.address}</strong><br><br><a class="button primary" href="dashboard.html">Go to Wallet Home</a> <a class="button" href="receive.html">Receive MSC</a>`);
    updateWalletUI();
  } catch (err) {
    setText("createResult", err.message || "Wallet create failed");
  }
}

async function unlockWallet(event) {
  event.preventDefault();
  try {
    state.wallet = state.wallet || loadWallet();
    const password = $("unlockPassword").value.trim();
    if (!state.wallet) throw new Error("No wallet saved");
    if (isHardwareWallet()) throw new Error("Hardware wallet does not use password unlock. Connect the device when signing.");
    state.secretKey = await decryptSecretKey(state.wallet.crypto, password);
    const publicKey = state.secretKey.slice(32);
    const derivedAddress = await addressFromPublicKey(publicKey);
    if (derivedAddress !== state.wallet.address || bytesToHex(publicKey) !== state.wallet.publicKey) {
      throw new Error("Wallet backup verification failed.");
    }
    saveSessionUnlock(state.secretKey, state.wallet);
    setStatus("loginStatus", "Wallet unlocked", "success");
    updateWalletUI();
  } catch (err) {
    setStatus("loginStatus", err.message || "Unlock failed", "error");
  }
}

async function importPrivateKey(event) {
  event.preventDefault();
  if (!recoveryGate("privateKeyRecoveryStatus")) return;
  try {
    const raw = $("importPrivateKey").value.trim();
    const secretKey = hexToBytes(raw);
    if (secretKey.length !== 64) throw new Error("Private key must be 64-byte hex");
    const publicKey = secretKey.slice(32);
    const address = await addressFromPublicKey(publicKey);
    const publicKeyHex = bytesToHex(publicKey);
    const confirmed = showRecoveryPreview("privateKeyRecoveryPreview", "privateKeyRecoveryConfirm", {
      title: "Private Key Recovery Preview",
      address,
      meta: [["Method", "Private Key"], ["Public key", shortAddress(publicKeyHex)]],
    });
    if (!confirmed) {
      setStatus("privateKeyRecoveryStatus", "Address preview ready. Confirm replacement, set password, then save.", "warn");
      return;
    }
    const password = $("importPassword").value.trim();
    if (password.length < 8) throw new Error("Use at least 8 characters.");
    const wallet = await buildRecoveryWallet({
      address,
      publicKey: publicKeyHex,
      secretKey,
      password,
      words: [],
      source: "private-key",
    });
    saveRecoveredWallet(wallet, secretKey, "privateKeyRecoveryStatus", `Imported ${shortAddress(wallet.address)}.`);
  } catch (err) {
    recordRecoveryFailure("privateKeyRecoveryStatus", err.message || "Import failed");
  }
}

async function recoverFromSeed(event) {
  event.preventDefault();
  if (!recoveryGate("seedRecoveryStatus")) return;
  try {
    const words = validateMnemonicPhrase($("recoverSeedPhrase")?.value || "");
    const generated = await walletFromMnemonic(words);
    const confirmed = showRecoveryPreview("seedRecoveryPreview", "seedRecoveryConfirm", {
      title: "Seed Recovery Preview",
      address: generated.address,
      meta: [["Method", "Seed Phrase"], ["Seed words", String(words.length)]],
    });
    if (!confirmed) {
      setStatus("seedRecoveryStatus", "Address preview ready. Confirm replacement, set password, then save.", "warn");
      return;
    }
    const password = $("recoverSeedPassword")?.value || "";
    const confirm = $("recoverSeedPasswordConfirm")?.value || "";
    if (password.length < 8) throw new Error("Use at least 8 characters.");
    if (password !== confirm) throw new Error("Passwords do not match.");
    const wallet = await buildRecoveryWallet({
      address: generated.address,
      publicKey: generated.publicKey,
      secretKey: generated.keyPair.secretKey,
      password,
      words: generated.words,
      source: "seed-phrase",
    });
    saveRecoveredWallet(wallet, generated.keyPair.secretKey, "seedRecoveryStatus", `Recovered ${shortAddress(wallet.address)}.`);
    if ($("recoverSeedPhrase")) $("recoverSeedPhrase").value = "";
    if ($("recoverSeedPassword")) $("recoverSeedPassword").value = "";
    if ($("recoverSeedPasswordConfirm")) $("recoverSeedPasswordConfirm").value = "";
    clearRecoveryPreview("seedRecoveryPreview", "seedRecoveryConfirm");
    renderSeedDiagnostics("recoverSeedPhrase", "recoverSeedCounter", "recoverSeedWordChips");
    renderPasswordStrength("recoverSeedPassword", "recoverSeedPasswordStrength");
  } catch (err) {
    recordRecoveryFailure("seedRecoveryStatus", err.message || "Seed recovery failed");
  }
}

async function importRecoveryKit(event) {
  event.preventDefault();
  if (!recoveryGate("recoveryKitImportStatus")) return;
  try {
    const file = $("recoveryKitFile")?.files?.[0];
    if (!file) throw new Error("Choose an MSC recovery kit file.");
    const wallet = validateRecoveryKit(await file.text());
    const confirmed = showRecoveryPreview("recoveryKitPreview", "recoveryKitConfirm", {
      title: "Recovery Kit Preview",
      address: wallet.address,
      meta: [["Method", "Recovery Kit"], ["Chain ID", wallet.chainId || CHAIN_ID], ["Created", wallet.createdAt || "-"]],
    });
    if (!confirmed) {
      setStatus("recoveryKitImportStatus", "Kit fingerprint ready. Confirm replacement, enter password, then import.", "warn");
      return;
    }
    const password = $("recoveryKitPassword")?.value || "";
    if (!password) throw new Error("Enter the kit password.");
    const secretKey = await decryptSecretKey(wallet.crypto, password);
    if (secretKey.length !== 64) throw new Error("Recovery kit decrypted invalid key material.");
    const publicKey = secretKey.slice(32);
    const publicKeyHex = bytesToHex(publicKey);
    const address = await addressFromPublicKey(publicKey);
    if (address !== wallet.address || publicKeyHex !== wallet.publicKey) {
      throw new Error("Recovery kit address verification failed.");
    }
    saveRecoveredWallet({ ...wallet, address, publicKey: publicKeyHex }, secretKey, "recoveryKitImportStatus", `Imported ${shortAddress(address)}.`);
    if ($("recoveryKitPassword")) $("recoveryKitPassword").value = "";
    clearRecoveryPreview("recoveryKitPreview", "recoveryKitConfirm");
  } catch (err) {
    recordRecoveryFailure("recoveryKitImportStatus", err.message || "Recovery kit import failed");
  }
}

function downloadRecoveryKit() {
  try {
    const payload = recoveryKitPayload(state.wallet || loadWallet());
    const suffix = payload.wallet.address ? payload.wallet.address.slice(-8).toLowerCase() : "wallet";
    downloadTextFile(`msc-wallet-recovery-kit-${suffix}.json`, JSON.stringify(payload, null, 2));
    setStatus("recoveryKitStatus", "Encrypted recovery kit downloaded.", "success");
  } catch (err) {
    setStatus("recoveryKitStatus", err.message || "Recovery kit export failed", "error");
  }
}

async function revealRecoveryPhrase(event) {
  event.preventDefault();
  try {
    state.wallet = state.wallet || loadWallet();
    const password = $("recoverySeedPassword")?.value || "";
    if (!state.wallet?.mnemonicCrypto) throw new Error("This wallet has no encrypted seed phrase saved.");
    if (!password) throw new Error("Enter wallet password.");
    const phrase = dec.decode(await decryptSecretKey(state.wallet.mnemonicCrypto, password));
    const words = validateMnemonicPhrase(phrase);
    const output = $("recoveryPhraseOutput");
    if (output) output.value = words.join(" ");
    setStatus("recoverySeedStatus", `${words.length} seed words revealed. Store offline only.`, "success");
  } catch (err) {
    setStatus("recoverySeedStatus", err.message || "Could not reveal seed phrase", "error");
  }
}

function copyRevealedSeed() {
  const phrase = $("recoveryPhraseOutput")?.value || "";
  if (!phrase) return setStatus("recoverySeedStatus", "Reveal the seed phrase first.", "error");
  return copyText(phrase)
    .then(() => setStatus("recoverySeedStatus", "Seed phrase copied. Clipboard clear kar dein after saving offline.", "success"))
    .catch(() => setStatus("recoverySeedStatus", "Copy failed.", "error"));
}

function downloadRevealedSeed() {
  const phrase = $("recoveryPhraseOutput")?.value || "";
  if (!phrase) return setStatus("recoverySeedStatus", "Reveal the seed phrase first.", "error");
  downloadTextFile("msc-wallet-seed-phrase.txt", `MSC Wallet Seed Phrase\n\n${phrase}\n\nNever share this phrase.\n`, "text/plain");
  setStatus("recoverySeedStatus", "Seed phrase text file downloaded. Keep it offline.", "success");
}

async function verifySeedBackup(event) {
  event.preventDefault();
  try {
    state.wallet = state.wallet || loadWallet();
    if (!state.wallet?.address) throw new Error("Create, import, or recover a wallet first.");
    const words = validateMnemonicPhrase($("verifySeedPhrase")?.value || "");
    const generated = await walletFromMnemonic(words);
    if (generated.address !== state.wallet.address) {
      throw new Error(`Seed opens ${shortAddress(generated.address)}, not this wallet.`);
    }
    setStatus("verifySeedStatus", `Backup verified for ${shortAddress(generated.address)}.`, "success");
  } catch (err) {
    setStatus("verifySeedStatus", err.message || "Seed verification failed", "error");
  }
}

function exportPrivateKey() {
  setText("privateKeyOutput", state.secretKey ? bytesToHex(state.secretKey) : "Unlock wallet first.");
}

function updateWalletUI() {
  state.wallet = state.wallet || loadWallet();
  if (state.wallet && currentAmbassadorReferralCode() && state.wallet.ambassadorReferralCode !== currentAmbassadorReferralCode()) {
    saveWallet(state.wallet);
  }
  setText("topWallet", state.wallet ? shortAddress(state.wallet.address) : "No wallet");
  setText("walletAddress", state.wallet?.address || "-");
  setText("walletPublicKey", state.wallet?.publicKey || "-");
  setText("receiveAddress", state.wallet?.address || "-");
  const bridgeRecipient = $("bridgeRecipient");
  if (bridgeRecipient && !bridgeRecipient.value && state.wallet?.address) bridgeRecipient.value = state.wallet.address;
  setText("securityEncryption", isHardwareWallet() ? "Hardware signing" : state.wallet ? "AES-256 Wallet Encryption" : "No wallet");
  setText("securityBackup", isHardwareWallet() ? "Device-held private key" : state.wallet?.mnemonic ? `${state.wallet.mnemonic.words || 12} Word Mnemonic` : state.wallet ? "Encrypted key backup" : "Create/import required");
  setText("securityMPC", "MPC Wallet (Future)");
  setText("securityHSM", "Hardware Wallet Support");
  setText("securitySession", state.secretKey ? "Unlocked" : "Locked");
  setText("recoveryKitState", isHardwareWallet() ? "Not needed for hardware wallet" : state.wallet ? "Ready to export" : "Create/import required");
  setText("recoverySeedState", isHardwareWallet() ? "Seed stays on device" : state.wallet?.mnemonicCrypto ? `${state.wallet.mnemonic?.words || 12} words encrypted` : "No seed saved");
  if (state.wallet?.address) renderQR("receiveQr", state.wallet.address);
  renderAmbassadorReferral();
  renderFaucetState();
  renderWalletHome();
  renderValidatorOnlyVisibility();
  renderValidatorWallet();
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
  if (!document.querySelector('script[data-msc-lucide]')) {
    const icons = document.createElement("script");
    icons.src = "vendor/lucide.min.js?v=20260618a";
    icons.dataset.mscLucide = "1";
    icons.addEventListener("load", () => window.lucide?.createIcons());
    document.head.appendChild(icons);
  }
  const shell = document.createElement("div");
  shell.className = "app-shell";
  shell.innerHTML = `
    <aside class="sidebar">
      <div class="brand">
        <div class="logo"><img src="${MSC_LOGO_SRC}" alt="MSC logo" /></div>
        <div>
          <div class="title">MSC Wallet</div>
          <div class="subtitle">Simple mainnet wallet</div>
        </div>
      </div>
      <div class="nav-section">
        <div class="nav-label">My Wallet</div>
        <nav class="nav" aria-label="Wallet navigation">
          <a href="dashboard.html" data-page="dashboard"><img class="nav-brand-icon" src="${MSC_APP_ICON_SRC}" alt="" />Dashboard</a>
          <a href="send.html" data-page="send"><i data-lucide="send"></i>Send</a>
          <a href="receive.html" data-page="receive"><i data-lucide="qr-code"></i>Receive</a>
          <a href="faucet.html" data-page="faucet"><img class="nav-brand-icon" src="${MSC_WALLET_ICON_SRC}" alt="" />Faucet</a>
          <a href="swap.html" data-page="swap"><i data-lucide="repeat-2"></i>Swap</a>
          <a href="staking.html" data-page="staking"><i data-lucide="landmark"></i>Stake</a>
          <a href="validator-wallet.html" data-page="validator-wallet" data-validator-wallet-only hidden><img class="nav-brand-icon" src="${MSC_VALIDATOR_BADGE_SRC}" alt="" />Validator Wallet</a>
          <a href="transactions.html" data-page="transactions"><i data-lucide="list"></i>Transactions</a>
          <a href="nfts.html" data-page="nfts"><img class="nav-brand-icon" src="${MSC_NFT_BADGE_SRC}" alt="" />NFTs</a>
          <a href="address-book.html" data-page="address-book"><i data-lucide="contact"></i>Address Book</a>
        </nav>
      </div>
      <div class="nav-section">
        <div class="nav-label">Network</div>
        <nav class="nav">
          <a href="validators.html" data-page="validators"><img class="nav-brand-icon" src="${MSC_VALIDATOR_BADGE_SRC}" alt="" />Validators</a>
          <a href="governance.html" data-page="governance"><img class="nav-brand-icon" src="${MSC_GOVERNANCE_BADGE_SRC}" alt="" />Governance</a>
          <a href="bridge.html" data-page="bridge"><img class="nav-brand-icon" src="${MSC_BRIDGE_BADGE_SRC}" alt="" />Buy / Bridge</a>
          <a href="https://explorer.mscblockexplorer.in"><img class="nav-brand-icon" src="${MSC_EXPLORER_ICON_SRC}" alt="" />Explorer</a>
        </nav>
      </div>
      <div class="nav-section">
        <div class="nav-label">Safety</div>
        <nav class="nav">
          <a href="security.html" data-page="security"><i data-lucide="lock-keyhole"></i>Security</a>
          <a href="settings.html" data-page="settings"><i data-lucide="settings"></i>Settings</a>
          <a href="status.html" data-page="status"><i data-lucide="activity"></i>Advanced status</a>
        </nav>
      </div>
      <div class="sidebar-foot">
        <div class="sidebar-foot-row"><span>Chain</span><strong>91938</strong></div>
        <div class="sidebar-foot-row"><span>Keys</span><strong>On device</strong></div>
      </div>
    </aside>
    <div class="main">
      <header class="topbar">
        <div class="topline">
          <div class="brand">
              <div class="logo"><img src="${MSC_LOGO_SRC}" alt="MSC logo" /></div>
              <div>
              <div class="title">MSC Wallet</div>
              <div class="subtitle">Send and receive safely</div>
            </div>
          </div>
          <form id="quickSearch" class="search">
            <input id="quickSearchInput" type="search" placeholder="Search transaction, address, or block" />
            <button class="primary" type="submit"><i data-lucide="search"></i><span>Search</span></button>
          </form>
        </div>
        <div class="status-row">
          <span id="networkPill" class="pill">Mainnet</span>
          <span class="pill">${pillHTML("Sync", "topRealtime", "Updating")}</span>
          <span class="pill">${pillHTML("Block", "topHeight")}</span>
          <span class="pill">${pillHTML("Wallet", "topWallet", "No wallet")}</span>
        </div>
      </header>
    </div>
    <nav class="wallet-mobile-nav" aria-label="Mobile wallet navigation">
      <a href="dashboard.html" data-page="dashboard"><img class="nav-brand-icon" src="${MSC_APP_ICON_SRC}" alt="" /><span>Home</span></a>
      <a href="send.html" data-page="send"><i data-lucide="send"></i><span>Send</span></a>
      <a href="receive.html" data-page="receive"><i data-lucide="qr-code"></i><span>Receive</span></a>
      <a href="faucet.html" data-page="faucet"><img class="nav-brand-icon" src="${MSC_WALLET_ICON_SRC}" alt="" /><span>Faucet</span></a>
      <a href="settings.html" data-page="settings"><i data-lucide="settings"></i><span>More</span></a>
    </nav>`;
  document.body.appendChild(shell);
  shell.querySelector(".main").appendChild(content);
  window.lucide?.createIcons();
}

function bindEvents() {
  initCreateWizard();
  initRecoveryUI();
  renderAddressBook();
  document.querySelectorAll(".nav a, .wallet-mobile-nav a").forEach((link) => {
    const active = link.dataset.page === page;
    link.classList.toggle("active", active);
  });
  document.querySelectorAll("[data-validator-filter]").forEach((button) => {
    button.addEventListener("click", () => {
      state.validatorFilter = button.dataset.validatorFilter || "all";
      document.querySelectorAll("[data-validator-filter]").forEach((candidate) => {
        candidate.classList.toggle("active", candidate === button);
      });
      refreshValidators({ force: false }).catch(() => {});
    });
  });
  $("quickSearch")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const q = $("quickSearchInput").value.trim();
    if (/^\d+$/.test(q)) {
      window.location.href = `https://explorer.mscblockexplorer.in/explorer-blocks.html?height=${encodeURIComponent(q)}`;
    } else if (q) {
      window.location.href = `https://explorer.mscblockexplorer.in/explorer-search.html?q=${encodeURIComponent(q)}`;
    }
  });
  $("settingsForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const manual = parseRPCEndpointList($("settingsRpc")?.value || "");
    const listed = parseRPCEndpointList($("settingsRpcEndpoints")?.value || "");
    const mode = $("settingsRpcMode")?.value || "auto";
    const endpoints = mode === "manual" ? uniqueRPCs([...manual, ...listed]) : uniqueRPCs([...listed, ...manual]);
    state.rpcManager.setConfig({ mode, endpoints });
    state.rpc = state.rpcManager.active;
    document.body.dataset.theme = $("settingsTheme").value;
    localStorage.setItem("msc_wallet_theme", $("settingsTheme").value);
    refreshAll({ force: true });
    connectRealtime(true);
  });
  $("createWalletForm")?.addEventListener("submit", createWallet);
  $("unlockForm")?.addEventListener("submit", unlockWallet);
  $("importKeyForm")?.addEventListener("submit", importPrivateKey);
  $("seedRecoveryForm")?.addEventListener("submit", recoverFromSeed);
  $("recoveryKitImportForm")?.addEventListener("submit", importRecoveryKit);
  $("downloadRecoveryKit")?.addEventListener("click", downloadRecoveryKit);
  $("revealRecoverySeedForm")?.addEventListener("submit", revealRecoveryPhrase);
  $("copyRecoveryPhrase")?.addEventListener("click", copyRevealedSeed);
  $("downloadRecoveryPhrase")?.addEventListener("click", downloadRevealedSeed);
  $("verifySeedForm")?.addEventListener("submit", verifySeedBackup);
  $("verifySeedPhrase")?.addEventListener("input", () => renderSeedDiagnostics("verifySeedPhrase", "verifySeedCounter", "verifySeedWordChips"));
  renderSeedDiagnostics("verifySeedPhrase", "verifySeedCounter", "verifySeedWordChips");
  $("showValidatorLinkForm")?.addEventListener("click", () => {
    const form = $("validatorLinkForm");
    if (form) form.hidden = false;
    setStatus("validatorLinkStatus", state.wallet?.address ? "Waiting" : "Create wallet first", state.wallet?.address ? "" : "warn");
  });
  $("buildValidatorLinkMessage")?.addEventListener("click", () => {
    try {
      const link = buildValidatorLinkFromForm();
      setValue("validatorLinkMessage", link.message);
      setStatus("validatorLinkStatus", "Message ready", "success");
      setText("validatorLinkResult", "Sign this message on your validator node, then paste the 64-byte hex signature.");
    } catch (err) {
      setStatus("validatorLinkStatus", err.message || "Message build failed", "error");
    }
  });
  $("validatorLinkForm")?.addEventListener("submit", handleValidatorLinkSubmit);
  $("validatorVoteForm")?.addEventListener("submit", handleValidatorVotePrepare);
  $("validatorNodeSignVote")?.addEventListener("click", handleValidatorNodeSignVote);
  $("validatorCommissionForm")?.addEventListener("submit", handleValidatorCommissionPrepare);
  $("copyValidatorCertificate")?.addEventListener("click", () => {
    copyText($("validatorCertificate")?.textContent || "")
      .then(() => setStatus("validatorDashboardBadge", "Certificate copied", "success"))
      .catch(() => setStatus("validatorDashboardBadge", "Copy failed", "error"));
  });
  $("addressBookForm")?.addEventListener("submit", addAddressBookContact);
  $("addressBookList")?.addEventListener("click", handleAddressBookClick);
  $("sendForm")?.addEventListener("submit", handleSend);
  $("startQrScan")?.addEventListener("click", startQRScan);
  $("stopQrScan")?.addEventListener("click", stopQRScan);
  $("qrImageUpload")?.addEventListener("change", handleQRImageUpload);
  $("qrScanResult")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-fill-qr-address]");
    if (!button) return;
    try {
      applyQRPayload({
        address: button.dataset.fillQrAddress || "",
        amount: button.dataset.fillQrAmount || null,
        coin: button.dataset.fillQrCoin || "MSC",
        chain: button.dataset.fillQrChain || CHAIN_ID,
      });
    } catch (err) {
      setStatus("qrScanStatus", err.message || "QR details are invalid", "error");
    }
  });
  if ($("qrScanStatus")) {
    if (qrDetectorAvailable()) {
      setStatus("qrScanStatus", `${qrDecoderName()} ready`, fallbackQRDetectorAvailable() ? "success" : "");
    } else {
      setStatus("qrScanStatus", "QR decoder missing", "error");
    }
    window.addEventListener("pagehide", stopQRScan);
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) stopQRScan();
    });
  }
  $("faucetForm")?.addEventListener("submit", requestFaucet);
  $("faucetAddress")?.addEventListener("input", renderFaucetState);
  initStakingUI();
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
  $("refreshBridge")?.addEventListener("click", () => refreshBridge({ force: true }));
  $("verifyBridgeProof")?.addEventListener("click", verifyBridgeProof);
  $("bridgeRouteSelect")?.addEventListener("change", renderSelectedBridgeRoute);
	$("bridgeWithdrawRouteSelect")?.addEventListener("change", renderSelectedBridgeWithdrawalRoute);
  $("bridgeDepositForm")?.addEventListener("submit", createBridgeDeposit);
	$("bridgeWithdrawForm")?.addEventListener("submit", createBridgeWithdrawal);
	[$("bridgeDepositStatus"), $("bridgeWithdrawStatus")].filter(Boolean).forEach((target) => target.addEventListener("click", (event) => {
    const button = event.target.closest("[data-copy-bridge]");
    if (!button) return;
    copyText(button.dataset.copyBridge || "")
      .then(() => setStatus("bridgeStatus", "Copied", "success"))
      .catch(() => setStatus("bridgeStatus", "Copy failed", "error"));
	}));
  document.querySelectorAll("[data-bridge-mode]").forEach((button) => {
    button.addEventListener("click", () => {
      const mode = button.dataset.bridgeMode;
      document.querySelectorAll("[data-bridge-mode]").forEach((item) => {
        const active = item === button;
        item.classList.toggle("active", active);
        item.setAttribute("aria-selected", String(active));
      });
      if ($("bridgeDepositForm")) $("bridgeDepositForm").hidden = mode !== "deposit";
      if ($("bridgeWithdrawForm")) $("bridgeWithdrawForm").hidden = mode !== "withdraw";
    });
  });
	if (new URLSearchParams(window.location.search).get("mode") === "withdraw") {
		document.querySelector('[data-bridge-mode="withdraw"]')?.click();
	}
  document.querySelectorAll("[data-bridge-filter]").forEach((button) => {
    button.addEventListener("click", () => {
      state.bridgeHistoryFilter = button.dataset.bridgeFilter || "all";
      document.querySelectorAll("[data-bridge-filter]").forEach((item) => item.classList.toggle("active", item === button));
      renderBridgeHistory();
    });
  });
  $("claimRewards")?.addEventListener("click", () => setStatus("claimStatus", "Claim endpoint pending", ""));
  $("sendAmount")?.addEventListener("input", () => {
    const amount = Number($("sendAmount").value || 0);
    setText("sendFee", `${computeFee(amount)} MSC`);
    setText("sendTotal", `${amount + computeFee(amount)} MSC`);
  });
}

function invalidateNetworkCache() {
  state.dataCache?.remove("status");
  state.dataCache?.remove("cmd");
}

function scheduleNetworkMetadataRefresh(delayMs = 1500, minGapMs = 10000) {
  if (state.networkRefreshTimer) return;
  const sinceLast = Date.now() - state.lastNetworkMetadataRefreshAt;
  const delay = Math.max(delayMs, minGapMs - sinceLast, 0);
  state.networkRefreshTimer = window.setTimeout(() => {
    state.networkRefreshTimer = null;
    state.lastNetworkMetadataRefreshAt = Date.now();
    refreshNetwork({ force: true }).catch(() => {});
  }, delay);
}

function scheduleWalletDataRefresh(delayMs = 1500) {
  if (!state.wallet?.address || state.walletRefreshTimer) return;
  state.walletRefreshTimer = window.setTimeout(() => {
    state.walletRefreshTimer = null;
    refreshBalance({ cacheOnly: false }).catch(() => {});
    refreshTransactions({ cacheOnly: false }).catch(() => {});
  }, delayMs);
}

function renderRealtimeEvent(event) {
  if (!event || typeof event !== "object") return;
  if (event.height) {
    state.realtime.height = Math.max(state.realtime.height || 0, Number(event.height) || 0);
    setText("topHeight", formatNumber(event.height));
    setText("blockHeight", formatNumber(event.height));
    setText("latestBlocks", `height ${formatNumber(event.height)} | finalized ${formatNumber(event.finalized_height || "-")}`);
    setText("txBlockHeight", formatNumber(event.height));
  }
  if (event.finalized_height) {
    state.realtime.finalizedHeight = Math.max(state.realtime.finalizedHeight || 0, Number(event.finalized_height) || 0);
    setText("finalizedHeight", formatNumber(event.finalized_height));
  }
  if (event.mode) {
    setText("topCmd", event.mode);
    setText("cmdStatus", event.mode);
    setText("validatorCMD", event.mode);
    state.cmd = { ...(state.cmd || {}), mode: event.mode, reason: event.reason || state.cmd?.reason };
  }
  if (event.network_health) setText("networkStatus", event.network_health);
  if (event.last_block_age_seconds !== undefined) setLastBlockAgeBase(event.last_block_age_seconds);
  if (Array.isArray(event.public_nodes)) {
    applyPublicNodeRegistry(mergePublicNodeRegistry(state.publicNodesRegistry, {
      status: event.public_nodes_healthy === event.public_nodes_total ? "healthy" : event.public_nodes_healthy > 0 ? "degraded" : "down",
      healthy: event.public_nodes_healthy || 0,
      total: event.public_nodes_total || event.public_nodes.length,
      best: event.public_nodes_best || "",
      nodes: event.public_nodes,
      ts: event.ts || Math.floor(Date.now() / 1000),
    }));
    refreshRPCSettingsUI();
  }
  state.status = {
    ...(state.status || {}),
    height: state.realtime.height || state.status?.height,
    finalized_height: state.realtime.finalizedHeight || state.status?.finalized_height,
    network_health: event.network_health || state.status?.network_health,
    last_block_age_seconds:
      event.last_block_age_seconds !== undefined ? event.last_block_age_seconds : state.status?.last_block_age_seconds,
  };
}

function handleRealtimeEvent(event) {
  state.realtime.lastEventAt = Date.now();
  const eventSentMs = Number(event?.ts_ms || (event?.ts ? Number(event.ts) * 1000 : 0));
  if (Number.isFinite(eventSentMs) && eventSentMs > 0) {
    state.realtime.eventDelayMs = Math.max(0, Date.now() - eventSentMs);
    renderEventDelay();
  }
  renderRealtimeEvent(event);
  if (event.type === "hello") {
    invalidateNetworkCache();
    refreshNetwork({ force: true }).catch(() => {});
    return;
  }
  if (event.type === "new_block" || event.type === "finality_update" || event.type === "consensus_mode") {
    invalidateNetworkCache();
    scheduleNetworkMetadataRefresh();
    scheduleWalletDataRefresh();
  }
  if (event.type === "validator_update") refreshValidators({ force: true }).catch(() => {});
  if (event.type === "tx_update" && (!event.address || event.address === state.wallet?.address)) {
    refreshBalance({ force: true }).catch(() => {});
    refreshTransactions({ force: true }).catch(() => {});
  }
}

function connectRealtime(force = false) {
  if (!window.WebSocket) {
    state.realtime.connected = false;
    state.realtime.fallback = true;
    setRealtimeStatus("Updating", "");
    return;
  }
  if (!force && state.realtime.socket && [window.WebSocket.CONNECTING, window.WebSocket.OPEN].includes(state.realtime.socket.readyState)) return;
  try {
    state.realtime.socket?.close();
  } catch (_) {
    // Best-effort cleanup before dialing the currently selected RPC.
  }
  const url = walletEventURL(state.rpcManager?.active || state.rpc);
  if (!url) {
    setRealtimeStatus("Updating", "");
    return;
  }
  const ws = new WebSocket(url);
  state.realtime.socket = ws;
  setRealtimeStatus("Connecting", "");
  ws.onopen = () => {
    state.realtime.connected = true;
    state.realtime.fallback = false;
    state.realtime.reconnectAttempts = 0;
    state.pollDelayMs = POLL_FALLBACK_MAX_MS;
    setRealtimeStatus("Live", "success");
  };
  ws.onmessage = (message) => {
    try {
      handleRealtimeEvent(JSON.parse(message.data || "{}"));
    } catch (_) {
      // Ignore malformed push messages; the polling fallback remains active.
    }
  };
  ws.onerror = () => {
    state.realtime.connected = false;
    state.realtime.fallback = true;
    setRealtimeStatus("Updating", "");
  };
  ws.onclose = () => {
    if (state.realtime.socket !== ws) return;
    state.realtime.connected = false;
    state.realtime.fallback = true;
    setRealtimeStatus("Updating", "");
    const attempt = Math.min(6, state.realtime.reconnectAttempts + 1);
    state.realtime.reconnectAttempts = attempt;
    const delay = Math.min(POLL_FALLBACK_MAX_MS, 1000 * (2 ** attempt)) + Math.floor(Math.random() * 1500);
    window.setTimeout(() => connectRealtime(), delay);
  };
}

function scheduleRefresh(delayMs = state.pollDelayMs) {
  if (state.schedulerTimer) window.clearTimeout(state.schedulerTimer);
  state.schedulerTimer = window.setTimeout(backgroundRefresh, delayMs);
}

async function backgroundRefresh() {
  if (state.refreshRunning) return scheduleRefresh(Math.min(POLL_FALLBACK_MAX_MS, state.pollDelayMs + 5000));
  state.refreshRunning = true;
  try {
    await refreshAll({ cacheOnly: false });
    state.pollDelayMs = state.realtime.connected ? POLL_FALLBACK_MAX_MS : Math.min(POLL_FALLBACK_MAX_MS, Math.max(POLL_FALLBACK_MIN_MS, state.pollDelayMs + 5000));
  } finally {
    state.refreshRunning = false;
    scheduleRefresh(state.pollDelayMs + Math.floor(Math.random() * 1500));
  }
}

async function refreshAll(options = {}) {
  refreshRPCSettingsUI();
  await refreshNetwork(options);
  updateWalletUI();
  await refreshBalance(options);
  await refreshTransactions(options);
  await refreshValidators(options);
  await refreshGovernance(options);
  await refreshBridge(options);
  await refreshPublicStatus(options);
}

function initTheme() {
  const theme = localStorage.getItem("msc_wallet_theme") || "dark";
  document.body.dataset.theme = theme;
  setValue("settingsTheme", theme);
}

state.rpcManager = new WalletRPCManager();
state.rpc = state.rpcManager.active;
state.dataCache = new WalletDataCache(WALLET_CACHE_KEY);
window.MSC_WALLET_RPC_MANAGER = state.rpcManager;
initAmbassadorReferral();
state.wallet = loadWallet();
if (state.wallet && currentAmbassadorReferralCode()) saveWallet(state.wallet);
restoreSessionUnlock();
installShell();
bindEvents();
initTheme();
renderAmbassadorReferral();
startBlockAgeTicker();
refreshAll({ cacheOnly: true });
refreshPublicNodes({ force: true })
  .catch(() => {})
  .finally(() => {
    refreshRPCSettingsUI();
    connectRealtime(true);
    scheduleRefresh(1000 + Math.floor(Math.random() * 1500));
  });
