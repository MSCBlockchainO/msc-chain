const enc = new TextEncoder();
const STORAGE_KEY = "msc_wallet_browser_v1";
const CHAIN_ID = "91938";
const DEFAULT_STAKE_EPOCHS = 19872000;
const AES_ITERATIONS = 150000;
const RPC_ENDPOINTS_KEY = "msc_rpc_endpoints_v1";
const RPC_MODE_KEY = "msc_rpc_mode_v1";
const LEGACY_RPC_KEY = "msc_rpc";
const DEFAULT_PUBLIC_RPCS = ["https://mscblockexplorer.in"];
const HEALTH_CHECK_MIN_MS = 15000;
const REQUEST_TIMEOUT_MS = 7000;
const WALLET_CACHE_KEY = "msc_wallet_data_cache_v1";
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
};
const POLL_FALLBACK_MIN_MS = 15000;
const POLL_FALLBACK_MAX_MS = 60000;

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
  publicNodesRegistry: null,
  dataCache: null,
  schedulerTimer: null,
  networkRefreshTimer: null,
  lastNetworkMetadataRefreshAt: 0,
  walletRefreshTimer: null,
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
  },
  blockAgeTimer: null,
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

function renderVerification(verification) {
  const mode = verification?.mode || "spv_pending";
  let text = "SPV pending";
  let tone = "";
  if (mode === "quorum") {
    text = `Quorum verified ${verification.matches}/${verification.checked}`;
    tone = "success";
  } else if (mode === "unverified") {
    text = "RPC unverified";
  } else if (mode === "mismatch") {
    text = "RPC mismatch";
    tone = "error";
  }
  setStatus("balanceVerification", text, tone);
  setStatus("dashboardVerification", text, tone);
  setText("settingsRpcVerification", text);
}

function formatNumber(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return value === 0 ? "0" : "-";
  return n.toLocaleString();
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

function currentLastBlockAge() {
  const base = Number(state.realtime.lastBlockAgeBaseSeconds);
  if (!Number.isFinite(base) || base < 0) return null;
  const updatedAt = Number(state.realtime.lastBlockAgeUpdatedAt || 0);
  const elapsed = updatedAt > 0 ? Math.max(0, Math.floor((Date.now() - updatedAt) / 1000)) : 0;
  return Math.trunc(base) + elapsed;
}

function setLastBlockAgeBase(seconds) {
  const age = Number(seconds);
  if (!Number.isFinite(age) || age < 0) return;
  state.realtime.lastBlockAgeBaseSeconds = Math.trunc(age);
  state.realtime.lastBlockAgeUpdatedAt = Date.now();
  renderLastBlockAge();
}

function renderLastBlockAge() {
  setText("topLastBlockAge", formatAge(currentLastBlockAge()));
}

function startBlockAgeTicker() {
  if (state.blockAgeTimer) return;
  state.blockAgeTimer = window.setInterval(() => {
    renderLastBlockAge();
    renderPublicNodesRegistry(state.publicNodesRegistry);
  }, 1000);
}

function stripHTML(value) {
  return String(value || "").replace(/<!--[\s\S]*?-->/g, " ").replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
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
        .map((item) => item.rpc_url || item.rpc || item.url),
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
      .map((node) => ({ node, raw: node?.rpc_url || node?.rpc || node?.url || "" }))
      .filter((item) => isUsableRPC(item.raw))
      .map((item) => ({ node: item.node, rpc: normalizeRPC(item.raw) }));
    const maxHeight = Math.max(0, ...normalized.map((item) => Number(item.node.height || 0)));
    for (const { node, rpc } of normalized) {
      const suspiciousReason = String(node.suspicious_reason || "").trim();
      if (suspiciousReason) this.suspicious.add(rpc);
      const staleBy = maxHeight && node.height ? maxHeight - Number(node.height || 0) : 0;
      const healthy = !!node.healthy && !suspiciousReason && staleBy <= 20;
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
        suspicious: !!suspiciousReason || this.suspicious.has(rpc),
        error: suspiciousReason || node.error || "",
        staleBy,
        source: "public-node-registry",
        publicGateway: node.public_gateway !== false,
      });
    }
    const best = this.bestEndpoints(1)[0];
    if (best) {
      this.active = best;
      state.rpc = best;
      localStorage.setItem(LEGACY_RPC_KEY, best);
    }
  }

  async checkEndpoint(rpc) {
    const started = performance.now();
    try {
      const [status, cmdResult] = await Promise.allSettled([
        this.fetchDedup(rpc, "/status", { timeoutMs: 5000 }),
        this.fetchDedup(rpc, "/consensus/mode", { timeoutMs: 5000 }),
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
        if (method === "GET" && err.status && err.status < 500 && err.status !== 429) break;
      }
    }
    throw lastErr || new Error("All RPC endpoints unavailable");
  }

  async quorumRead(path) {
    await this.refreshHealth(false);
    const targets = this.bestEndpoints(3);
    const usableTargets = targets.length ? targets : this.endpoints.slice(0, 3);
    const settled = await Promise.allSettled(usableTargets.map(async (rpc) => ({ rpc, data: await this.fetchDedup(rpc, path) })));
    const successes = settled.filter((item) => item.status === "fulfilled").map((item) => item.value);
    if (!successes.length) {
      const err = settled.find((item) => item.status === "rejected")?.reason || new Error("All RPC endpoints unavailable");
      throw err;
    }
    if (successes.length === 1) {
      return { data: successes[0].data, verification: { mode: "unverified", rpc: successes[0].rpc, matches: 1, checked: 1 } };
    }
    const groups = new Map();
    successes.forEach((item) => {
      const key = quorumKey(path, item.data);
      const group = groups.get(key) || [];
      group.push(item);
      groups.set(key, group);
    });
    const majority = Array.from(groups.values()).sort((a, b) => b.length - a.length)[0];
    if (majority.length >= 2) {
      const majorityRPCs = new Set(majority.map((item) => item.rpc));
      successes.forEach((item) => {
        if (!majorityRPCs.has(item.rpc)) this.suspicious.add(item.rpc);
      });
      return {
        data: majority[0].data,
        verification: { mode: "quorum", rpc: majority[0].rpc, matches: majority.length, checked: successes.length },
      };
    }
    successes.slice(1).forEach((item) => this.suspicious.add(item.rpc));
    return {
      data: successes[0].data,
      verification: { mode: "mismatch", rpc: successes[0].rpc, matches: 1, checked: successes.length },
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
        const data = await state.rpcManager.fetchDedup(rpc, "/gateway/lb-status.json", { timeoutMs: 4000 });
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
  setText("settingsPublicNodesSummary", `${healthy}/${total} healthy`);
  setText("settingsPublicNodesBest", registry.best || registry.best_node?.rpc_url || "-");
  const box = $("settingsPublicNodesHealth");
  if (!box) return;
  const displayNodeAge = (item) => {
    const base = Number(item.last_block_age_seconds);
    if (!Number.isFinite(base) || base < 0) return "-";
    const checked = Number(item.last_checked || 0);
    const checkedMs = checked > 0 ? (checked < 1e12 ? checked * 1000 : checked) : 0;
    const elapsed = checkedMs > 0 ? Math.max(0, Math.floor((Date.now() - checkedMs) / 1000)) : 0;
    return formatAge(Math.trunc(base) + elapsed);
  };
  box.innerHTML = nodes
    .map((item) => {
      const tone = item.healthy ? "success" : item.suspicious_reason || item.error ? "error" : "";
      const flags = [item.consensus_mode, item.network_health, item.suspicious_reason, item.error].filter(Boolean).join(" | ");
      return `
        <div class="health-row ${tone}">
          <span class="mono">${item.id || "-"}</span>
          <span class="mono">${item.rpc_url || "-"}</span>
          <span>${item.healthy ? "healthy" : "degraded"}</span>
          <span>score ${Math.round(Number(item.score || 0))}</span>
          <span>h ${formatNumber(item.height || 0)}</span>
          <span>age ${displayNodeAge(item)}</span>
          <span>${item.latency_ms ?? "-"}ms</span>
          <span>${flags || "-"}</span>
        </div>`;
    })
    .join("") || `<div class="list-item">No public full nodes discovered yet</div>`;
}

function normalizeGatewayPublicNodes(payload) {
  const backends = Array.isArray(payload?.backends) ? payload.backends : Array.isArray(payload?.upstreams) ? payload.upstreams : null;
  if (!backends) return payload;
  const nodes = backends.map((item) => ({
    ...item,
    id: item.id || item.node_id || item.target || item.rpc_url || "-",
    rpc_url: item.rpc_url || window.location.origin,
    role: item.role || "full",
    public_gateway: item.public_gateway !== false,
    healthy: !!item.healthy || Number(item.status_code) === 200,
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
  if (state.rpcManager && nodes.length) {
    state.rpcManager.mergePublicNodes(nodes);
  }
  renderPublicNodesRegistry(data);
}

async function refreshPublicNodes(options = {}) {
  const unwrapV1 = (payload) =>
    payload && typeof payload === "object" && Object.prototype.hasOwnProperty.call(payload, "success")
      ? payload.data
      : payload;
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

function refreshRPCSettingsUI() {
  if (!state.rpcManager) return;
  state.rpc = state.rpcManager.active;
  setText("topRpc", String(state.rpc || "-").replace(/^https?:\/\//, ""));
  setValue("settingsRpcMode", state.rpcManager.mode);
  setValue("settingsRpc", state.rpcManager.active);
  setValue("settingsRpcEndpoints", state.rpcManager.endpoints.join("\n"));
  setText("settingsActiveRpc", state.rpcManager.active || "-");
  const healthBox = $("settingsRpcHealth");
  if (healthBox) {
    const rows = state.rpcManager.healthList().map((item) => {
      const tone = item.healthy ? "success" : item.ok ? "" : "error";
      const flags = [item.publicGateway ? "public" : "", rpcPolicyWarning(item.rpc), item.suspicious ? "suspicious" : "", item.syncing ? "syncing" : "", item.error || ""]
        .filter(Boolean)
        .join(" | ");
      return `
        <div class="health-row ${tone}">
          <span class="mono">${item.rpc}</span>
          <span>${item.healthy ? "healthy" : item.ok ? "degraded" : "offline"}</span>
          <span>score ${Math.round(item.score || 0)}</span>
          <span>h ${formatNumber(item.height || 0)}</span>
          <span>${item.latency ? `${item.latency}ms` : "-"}</span>
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
  setLastBlockAgeBase(best.last_block_age_seconds ?? status.last_block_age_seconds);
  setText("topHeight", formatNumber(height));
  setText("networkStatus", status.health || status.network_health || "connected");
  setText("blockHeight", formatNumber(height));
  setText("finalizedHeight", formatNumber(finalized));
  setText("latestBlocks", `height ${formatNumber(height)} | finalized ${formatNumber(finalized)}`);
  setText("txBlockHeight", formatNumber(height));
  setStatus("networkPill", "Mainnet", "success");
}

function renderCMD(cmd) {
  if (!cmd) return;
  state.cmd = cmd;
  const mode = cmd.mode || "UNKNOWN";
  setText("topCmd", mode);
  setText("cmdStatus", mode);
  setText("validatorStatus", `${cmd.active_validators ?? "-"} / ${cmd.total_validators ?? "-"} active`);
  setText("validatorCMD", mode);
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
  state.balanceVerification = verification || state.balanceVerification;
  if (verification) renderVerification(verification);
  const amount = bal.balance ?? bal.Balance ?? "-";
  setText("totalBalance", `${formatNumber(amount)} MSC`);
  setText("walletBalance", `${formatNumber(amount)} MSC`);
  setText("assetMSC", `${formatNumber(amount)} MSC`);
}

function renderWalletStatus(ws) {
  if (!ws) return;
  setText("stakedBalance", `${formatNumber(ws.stake || 0)} MSC`);
  setText("rewardBalance", `${formatNumber(ws.rewards || 0)} MSC`);
  setText("delegations", ws.validator_id ? `${ws.validator_id}: ${formatNumber(ws.stake || 0)} MSC` : "No active delegation");
}

async function refreshBalance(options = {}) {
  if (!state.wallet?.address) return;
  setText("topWallet", shortAddress(state.wallet.address));
  setText("walletAddress", state.wallet.address);
  setText("walletPublicKey", state.wallet.publicKey || "-");
  setText("receiveAddress", state.wallet.address);
  setValue("sendFrom", state.wallet.address);
  try {
    const balResult = await cachedAPI(
      cacheKey("balance", state.wallet.address),
      `/balance?address=${encodeURIComponent(state.wallet.address)}&coin=MSC&state=finalized`,
      { ttl: CACHE_TTL.balance, force: !!options.force, cacheOnly: !!options.cacheOnly, quorum: true },
    );
    renderBalanceData(balResult?.data, balResult?.verification);
  } catch (err) {
    setText("walletBalance", "balance unavailable");
    renderVerification({ mode: "mismatch", checked: 0, matches: 0 });
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
  if (!list) return;
  const vals = data?.validators || data?.active || data?.items || [];
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
}

async function refreshValidators(options = {}) {
  const list = $("validatorList");
  if (!list) return;
  try {
    const result = await cachedAPI("validators", "/v1/validators", { ttl: CACHE_TTL.validators, force: !!options.force, cacheOnly: !!options.cacheOnly });
    renderValidatorsData(result?.data);
  } catch (err) {
    list.innerHTML = `<div class="list-item">${err.message || "Validator sync failed"}</div>`;
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

function renderBridgeData(data) {
  if (!$("bridgeStatus")) return;
  setStatus("bridgeStatus", data?.enabled ? "Enabled" : "Verification Only", data?.enabled ? "success" : "");
  setText("bridgeMode", data?.mode || "disabled");
  setText("bridgeChains", formatNumber(data?.registered_chains || 0));
  setText("bridgeAssets", formatNumber(data?.registered_assets || 0));
  setText("bridgeFuture", data?.light_client_required === false ? "Asset transfer allowed" : "Light-client verified transfer pending");
}

async function refreshBridge(options = {}) {
  if (!$("bridgeStatus")) return;
  try {
    const result = await cachedAPI("bridge", "/bridge/status", { ttl: CACHE_TTL.bridge, force: !!options.force, cacheOnly: !!options.cacheOnly });
    renderBridgeData(result?.data);
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
    refreshBalance({ force: true });
    refreshTransactions({ force: true });
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
    refreshBalance({ force: true });
    refreshValidators({ force: true });
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
    refreshBalance({ force: true });
    refreshValidators({ force: true });
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
          <span class="pill">RPC <strong id="topRpc">-</strong></span>
          <span class="pill">Realtime <strong id="topRealtime">Polling</strong></span>
          <span class="pill">Height <strong id="topHeight">-</strong></span>
          <span class="pill">Last block <strong id="topLastBlockAge">-</strong></span>
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
    setRealtimeStatus("Fallback polling", "");
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
    setRealtimeStatus("Fallback polling", "");
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
    setRealtimeStatus("Connected", "success");
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
    setRealtimeStatus("Fallback polling", "");
  };
  ws.onclose = () => {
    if (state.realtime.socket !== ws) return;
    state.realtime.connected = false;
    state.realtime.fallback = true;
    setRealtimeStatus("Fallback polling", "");
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
installShell();
bindEvents();
initTheme();
startBlockAgeTicker();
refreshAll({ cacheOnly: true });
refreshPublicNodes({ force: true })
  .catch(() => {})
  .finally(() => {
    refreshRPCSettingsUI();
    connectRealtime(true);
    scheduleRefresh(1000 + Math.floor(Math.random() * 1500));
  });
