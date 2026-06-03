(function () {
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
    apiToken: (localStorage.getItem("msc_token") || "").replace(/^Bearer\s+/i, "").trim(),
    refreshMs: 3000,
    timer: null,
    selectedBlockHeight: 0,
    adminMode: localStorage.getItem("msc_admin_mode") === "1",
    latestValidators: null,
    latestPeers: null,
    latestPublicNodes: null,
    latestStatus: null,
    latestBlocks: null,
    txRawMode: false,
    lastTxPayload: null,
    refreshSeq: 0,
    lastAppliedSeq: 0,
    refreshInFlight: false,
    refreshQueued: false,
    realtimeSocket: null,
    realtimeConnected: false,
    realtimeAttempts: 0,
    realtimeHeight: 0,
    realtimeFinalized: 0,
    heightAnimationTimer: null,
    lastBlockAgeBaseSeconds: null,
    lastBlockAgeUpdatedAt: 0,
    blockAgeTimer: null,
    eventDelayMs: null,
  };

  const UPTIME_CACHE_KEY = "msc_public_node_uptime_v1";
  const UPTIME_MAX_SAMPLES = 200;

  const byId = (id) => document.getElementById(id);

  const els = {
    connControls: byId("connControls"),
    rpcUrl: byId("rpcUrl"),
    apiTokenField: byId("apiTokenField"),
    apiToken: byId("apiToken"),
    refreshMs: byId("refreshMs"),
    connectBtn: byId("connectBtn"),
    refreshBtn: byId("refreshBtn"),
    adminToggleBtn: byId("adminToggleBtn"),
    connState: byId("connState"),
    quickSearchForm: byId("quickSearchForm"),
    quickSearchInput: byId("quickSearchInput"),
    topHeight: byId("topHeight"),
    topLastBlockAge: byId("topLastBlockAge"),
    topEventDelay: byId("topEventDelay"),
    topCmd: byId("topCmd"),
    topPeers: byId("topPeers"),
    topState: byId("topState"),
    nodeId: byId("nodeId"),
    chainId: byId("chainId"),
    nodeRole: byId("nodeRole"),
    height: byId("height"),
    finalized: byId("finalized"),
    lastBlockAge: byId("lastBlockAge"),
    peerCount: byId("peerCount"),
    quorum: byId("quorum"),
    consensusDetectorMode: byId("consensusDetectorMode"),
    waitReason: byId("waitReason"),
    livenessMode: byId("livenessMode"),
    livenessDriftLimit: byId("livenessDriftLimit"),
    livenessCounts: byId("livenessCounts"),
    autohealState: byId("autohealState"),
    autohealReason: byId("autohealReason"),
    autohealMismatch: byId("autohealMismatch"),
    autohealSuccess: byId("autohealSuccess"),
    bootstrapLane: byId("bootstrapLane"),
    stateText: byId("state"),
    blocksMeta: byId("blocksMeta"),
    blocksBody: byId("blocksBody"),
    validatorsOnline: byId("validatorsOnline"),
    validatorsOffline: byId("validatorsOffline"),
    validatorsPendingAdd: byId("validatorsPendingAdd"),
    validatorsPendingRemove: byId("validatorsPendingRemove"),
    validatorsConnected: byId("validatorsConnected"),
    validatorsConnectedUnhealthy: byId("validatorsConnectedUnhealthy"),
    validatorsGap: byId("validatorsGap"),
    validatorMeta: byId("validatorMeta"),
    blockSearchForm: byId("blockSearchForm"),
    blockHeightInput: byId("blockHeightInput"),
    blockHashInput: byId("blockHashInput"),
    txSearchForm: byId("txSearchForm"),
    txIdInput: byId("txIdInput"),
    blockDetailMeta: byId("blockDetailMeta"),
    blockDetail: byId("blockDetail"),
    txDetailMeta: byId("txDetailMeta"),
    txDetail: byId("txDetail"),
    txRawToggle: byId("txRawToggle"),
    peersMeta: byId("peersMeta"),
    peersBody: byId("peersBody"),
    publicNodesMeta: byId("publicNodesMeta"),
    publicNodesBody: byId("publicNodesBody"),
  };

  const LEGACY_CONTRACT_TX_KEYS = new Set([
    "contract_id",
    "runtime_mode",
    "logic_hash",
    "logic_pack_hash",
    "contract_standard",
    "contract_interfaces",
    "abi_hash",
    "upgradeable",
    "proxy_target",
    "bytecode_format",
    "bytecode_hash",
    "bytecode_size",
    "compiler",
    "source_hash",
  ]);

  const setAdminMode = (enabled) => {
    state.adminMode = !!enabled || !!state.apiToken;
    if (els.connControls) {
      els.connControls.classList.toggle("show-admin", state.adminMode);
    }
    if (els.adminToggleBtn) {
      els.adminToggleBtn.textContent = state.adminMode ? "Hide Admin" : "Admin";
    }
    localStorage.setItem("msc_admin_mode", state.adminMode ? "1" : "0");
  };

  const txTypeName = (t) => {
    const n = Number(t);
    switch (n) {
      case 0:
        return "TRANSFER";
      case 1:
        return "TASK";
      case 2:
        return "STAKE";
      case 3:
        return "VOTE";
      case 4:
        return "VALIDATOR_UPDATE";
      case 5:
        return "FAUCET";
      case 6:
        return "UNSTAKE";
      case 7:
        return "EVM";
      default:
        return String(t);
    }
  };

  const short = (v, n = 10) => {
    if (!v) return "-";
    const s = String(v);
    if (s.length <= n * 2) return s;
    return `${s.slice(0, n)}...${s.slice(-n)}`;
  };

  const asIntOrNull = (value) => {
    const num = Number(value);
    if (!Number.isFinite(num)) return null;
    return Math.trunc(num);
  };

  const asTextOrDash = (value) => {
    if (value === undefined || value === null) return "-";
    const text = String(value).trim();
    return text || "-";
  };

  const fmtWallTime = (ts) => {
    const num = Number(ts);
    if (!Number.isFinite(num) || num <= 0) return "-";
    let ms = num;
    if (num < 1e12) ms = num * 1000;
    else if (num > 1e16) ms = Math.floor(num / 1e6);
    const d = new Date(ms);
    if (Number.isNaN(d.getTime())) return String(ts);
    return `${d.toLocaleString()} (${ts})`;
  };

  const fmtAge = (seconds) => {
    const n = Number(seconds);
    if (!Number.isFinite(n) || n < 0) return "-";
    if (n < 60) return `${Math.trunc(n)}s`;
    const mins = Math.floor(n / 60);
    const secs = Math.trunc(n % 60);
    if (mins < 60) return secs ? `${mins}m ${secs}s` : `${mins}m`;
    const hours = Math.floor(mins / 60);
    const remMins = mins % 60;
    return remMins ? `${hours}h ${remMins}m` : `${hours}h`;
  };

  const fmtBlocks = (value) => {
    const n = asIntOrNull(value);
    if (n === null || n < 0) return "-";
    return `${n} block${n === 1 ? "" : "s"}`;
  };

  const fmtLatency = (value) => {
    const n = asIntOrNull(value);
    if (n === null || n < 0) return "-";
    return `${n}ms`;
  };

  const publicNodeKey = (node) => String(node?.id || node?.target || node?.rpc_url || node?.rpc || "-").trim();

  const loadUptimeCache = () => {
    try {
      return JSON.parse(localStorage.getItem(UPTIME_CACHE_KEY) || "{}") || {};
    } catch (_) {
      return {};
    }
  };

  const saveUptimeCache = (cache) => {
    try {
      localStorage.setItem(UPTIME_CACHE_KEY, JSON.stringify(cache));
    } catch (_) {
      // Uptime samples are display-only.
    }
  };

  const recordPublicNodeUptime = (nodes) => {
    if (!Array.isArray(nodes) || !nodes.length) return;
    const cache = loadUptimeCache();
    const now = Date.now();
    for (const node of nodes) {
      const key = publicNodeKey(node);
      if (!key || key === "-") continue;
      const samples = Array.isArray(cache[key]) ? cache[key] : [];
      samples.push({ t: now, h: !!node.healthy });
      cache[key] = samples.slice(-UPTIME_MAX_SAMPLES);
    }
    saveUptimeCache(cache);
  };

  const publicNodeUptimePct = (node) => {
    const samples = loadUptimeCache()[publicNodeKey(node)] || [];
    if (!samples.length) return "-";
    const healthy = samples.filter((sample) => !!sample.h).length;
    return `${Math.round((healthy / samples.length) * 100)}%`;
  };

  const publicNodeDisplayAgeSeconds = (node) => {
    const base = asIntOrNull(node?.last_block_age_seconds);
    if (base === null) return null;
    const checked = Number(node?.last_checked || 0);
    const checkedMs = checked > 0 ? (checked < 1e12 ? checked * 1000 : checked) : 0;
    const elapsed = checkedMs > 0 ? Math.max(0, Math.floor((Date.now() - checkedMs) / 1000)) : 0;
    return base + elapsed;
  };

  const publicNodeHeightLag = (node, bestHeight) => {
    const explicit = asIntOrNull(node?.height_lag_blocks);
    if (explicit !== null && explicit >= 0) return explicit;
    const h = asIntOrNull(node?.height);
    if (h === null || !Number.isFinite(bestHeight) || bestHeight <= 0) return 0;
    return Math.max(0, Math.trunc(bestHeight - h));
  };

  const publicNodeTone = (node, bestHeight) => {
    const healthState = String(node?.health_state || "").toLowerCase();
    if (healthState === "unhealthy") return "bad";
    if (healthState === "warning") return "warn";
    if (!node?.healthy || node?.suspicious_reason) return "bad";
    const heightLag = publicNodeHeightLag(node, bestHeight);
    const finalityLag = asIntOrNull(node?.finality_lag) || 0;
    const age = publicNodeDisplayAgeSeconds(node);
    const cmd = String(node?.consensus_mode || "").toUpperCase();
    if (heightLag > 20 || finalityLag > 20 || (age !== null && age > 60) || ["EMERGENCY", "HALTED", "ATTACK", "PARTITION"].includes(cmd)) return "bad";
    if (heightLag > 2 || finalityLag > 2 || (age !== null && age >= 12) || ["STRICT", "RECOVERY", "DEGRADED"].includes(cmd)) return "warn";
    return "ok";
  };

  const blockAgeTone = (status) => {
    const age = asIntOrNull(status.last_block_age_seconds);
    if (age === null) return "";
    const haltedAfter = asIntOrNull(status.halted_after_seconds) || 60;
    const degradedAfter = asIntOrNull(status.degraded_after_seconds) || 12;
    if (age >= haltedAfter) return "bad";
    if (age >= degradedAfter) return "warn";
    return "ok";
  };

  const normalizeGatewayPublicNodes = (payload) => {
    const backends = Array.isArray(payload?.backends) ? payload.backends : Array.isArray(payload?.upstreams) ? payload.upstreams : null;
    if (!backends) return payload;
    const nodes = backends.map((item) => ({
      ...item,
      id: item.id || item.node_id || item.target || item.rpc_url || "-",
      rpc_url: item.rpc_url || window.location.origin,
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
  };

  const mergePublicNodes = (base, update) => {
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
  };

  const currentLastBlockAge = () => {
    const base = asIntOrNull(state.lastBlockAgeBaseSeconds);
    if (base === null) return null;
    const updatedAt = Number(state.lastBlockAgeUpdatedAt || 0);
    const elapsed = updatedAt > 0 ? Math.max(0, Math.floor((Date.now() - updatedAt) / 1000)) : 0;
    return base + elapsed;
  };

  const renderBlockAgeDisplay = (age) => {
    const n = asIntOrNull(age);
    const blockAgeText = n === null ? "-" : fmtAge(n);
    const ageTone = blockAgeTone({
      last_block_age_seconds: n,
      degraded_after_seconds: state.latestStatus?.degraded_after_seconds,
      halted_after_seconds: state.latestStatus?.halted_after_seconds,
    });
    if (els.lastBlockAge) {
      els.lastBlockAge.textContent = blockAgeText;
      setTone(els.lastBlockAge, ageTone);
    }
    if (els.topLastBlockAge) {
      els.topLastBlockAge.textContent = blockAgeText;
      setTone(els.topLastBlockAge, ageTone);
    }
  };

  const renderEventDelay = () => {
    const n = asIntOrNull(state.eventDelayMs);
    if (els.topEventDelay) {
      els.topEventDelay.textContent = n === null ? "-" : fmtLatency(n);
    }
  };

  const updateBlockAgeBase = (age) => {
    const n = asIntOrNull(age);
    if (n === null) return;
    state.lastBlockAgeBaseSeconds = n;
    state.lastBlockAgeUpdatedAt = Date.now();
    renderBlockAgeDisplay(n);
  };

  const startBlockAgeTicker = () => {
    if (state.blockAgeTimer) return;
    state.blockAgeTimer = setInterval(() => {
      renderBlockAgeDisplay(currentLastBlockAge());
      renderEventDelay();
      if (state.latestPublicNodes) renderPublicNodes(state.latestPublicNodes);
    }, 1000);
  };

  const setTone = (el, tone) => {
    if (!el) return;
    el.classList.remove("ok", "warn", "bad");
    if (tone) el.classList.add(tone);
  };

  const walletEventURL = () => {
    try {
      const url = new URL(state.rpcUrl || window.location.origin);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.pathname = "/wallet/events";
      url.search = "";
      url.hash = "";
      return url.toString();
    } catch (_) {
      return "";
    }
  };

  const renderLiveHeight = ({ height, finalized, lastBlockAge, mode, peers, networkHealth, syncing, ready }) => {
    const h = asIntOrNull(height);
    const f = asIntOrNull(finalized);
    const age = asIntOrNull(lastBlockAge);
    if (h !== null) {
      if (els.height) els.height.textContent = String(h);
      if (els.topHeight) els.topHeight.textContent = String(h);
    }
    if (f !== null && els.finalized) els.finalized.textContent = String(f);
    if (age !== null) updateBlockAgeBase(age);
    else renderBlockAgeDisplay(currentLastBlockAge());
    if (els.topCmd && mode) {
      els.topCmd.textContent = asTextOrDash(mode);
      const cmd = String(mode || "").toUpperCase();
      setTone(els.topCmd, cmd === "NORMAL" ? "ok" : cmd === "HALTED" || cmd === "EMERGENCY" || cmd === "ATTACK" ? "bad" : "warn");
    }
    if (els.consensusDetectorMode && mode) els.consensusDetectorMode.textContent = asTextOrDash(mode);
    if (els.topPeers && peers !== undefined && peers !== null) els.topPeers.textContent = String(peers);
    if (els.peerCount && peers !== undefined && peers !== null) els.peerCount.textContent = String(peers);
    if (els.topState) {
      els.topState.textContent = syncing ? "SYNCING" : ready ? "READY" : networkHealth || "LIVE";
      setTone(els.topState, syncing ? "warn" : "ok");
    }
  };

  const animateConfirmedHeights = (heights, event) => {
    const clean = Array.from(new Set((heights || []).map((x) => asIntOrNull(x)).filter((x) => x !== null && x > 0))).sort((a, b) => a - b);
    if (state.heightAnimationTimer) {
      clearTimeout(state.heightAnimationTimer);
      state.heightAnimationTimer = null;
    }
    if (!clean.length) return;
    let idx = 0;
    const tick = () => {
      const h = clean[idx];
      renderLiveHeight({
        height: h,
        finalized: idx === clean.length - 1 ? event.finalized_height : null,
        lastBlockAge: idx === clean.length - 1 ? event.last_block_age_seconds : null,
        mode: event.mode,
        peers: event.peer_count,
        networkHealth: event.network_health,
      });
      idx += 1;
      if (idx < clean.length) {
        state.heightAnimationTimer = setTimeout(tick, 160);
      }
    };
    tick();
  };

  const inferLogicalClock = (ts, height) => {
    const units = Number(ts);
    const h = Number(height);
    if (!Number.isFinite(units) || units <= 0 || !Number.isFinite(h) || h <= 0) {
      return null;
    }
    if (units < h) return null;
    const ticksPerEpoch = Math.round(units / h);
    if (!Number.isFinite(ticksPerEpoch) || ticksPerEpoch < 2 || ticksPerEpoch > 100000) {
      return null;
    }
    const tick = units - h * ticksPerEpoch;
    if (tick < 0 || tick > ticksPerEpoch) return null;
    return { epoch: h, tick, ticksPerEpoch };
  };

  const fmtBlockTime = (ts, blockTime, height) => {
    const epoch = Number(blockTime && blockTime.epoch);
    const tick = Number(blockTime && blockTime.tick);
    if (Number.isFinite(epoch) && epoch > 0 && Number.isFinite(tick) && tick >= 0) {
      return `Epoch ${epoch}, Tick ${tick} (units ${ts})`;
    }
    const inferred = inferLogicalClock(ts, height);
    if (inferred) {
      return `Epoch ${inferred.epoch}, Tick ${inferred.tick} (units ${ts})`;
    }
    return fmtWallTime(ts);
  };

  const setConn = (msg, tone) => {
    els.connState.textContent = msg;
    els.connState.classList.remove("ok", "bad", "warn");
    if (tone) els.connState.classList.add(tone);
  };

  const stripHTMLForError = (value) => {
    const raw = String(value || "").trim();
    if (!raw) return "";
    if (!/<[a-z][\s\S]*>/i.test(raw)) return raw;
    const withoutComments = raw.replace(/<!--[\s\S]*?-->/g, " ");
    const titleMatch = withoutComments.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
    const h1Match = withoutComments.match(/<h1[^>]*>([\s\S]*?)<\/h1>/i);
    const picked = titleMatch?.[1] || h1Match?.[1] || withoutComments;
    return picked
      .replace(/<[^>]+>/g, " ")
      .replace(/&nbsp;/gi, " ")
      .replace(/&lt;/gi, "<")
      .replace(/&gt;/gi, ">")
      .replace(/&amp;/gi, "&")
      .replace(/&quot;/gi, '"')
      .replace(/&#39;/gi, "'")
      .replace(/\s+/g, " ")
      .trim();
  };

  const friendlyHTTPErrorMessage = (status, data, text, statusText) => {
    if (status === 429) return "Rate limit hit — wait a few seconds";
    if (data && typeof data === "object") {
      if (typeof data.error === "string") return data.error;
      if (data.error && typeof data.error.message === "string") return data.error.message;
      if (typeof data.message === "string") return data.message;
    }
    const cleanText = stripHTMLForError(typeof data === "string" ? data : text);
    if (/too many requests/i.test(cleanText)) return "Rate limit hit — wait a few seconds";
    return cleanText || statusText || "Request failed";
  };

  const api = async (path) => {
    const headers = {};
    if (state.apiToken) headers.Authorization = `Bearer ${state.apiToken}`;
    const res = await fetch(`${state.rpcUrl}${path}`, { headers });
    const text = await res.text();
    let data = null;
    if (text) {
      try {
        data = JSON.parse(text);
      } catch (_) {
        data = text;
      }
    }
    if (!res.ok) {
      const message = friendlyHTTPErrorMessage(res.status, data, text, res.statusText);
      const err = new Error(message);
      err.status = res.status;
      err.payload = data;
      throw err;
    }
    return data;
  };

  const apiV1 = async (path, fallbackPath = "") => {
    try {
      const payload = await api(path);
      if (payload && typeof payload === "object" && Object.prototype.hasOwnProperty.call(payload, "success")) {
        if (!payload.success) {
          const message =
            (payload.error && payload.error.message) || "request failed";
          throw new Error(message);
        }
        return payload.data;
      }
      return payload;
    } catch (err) {
      if (fallbackPath) {
        const st = Number(err && err.status);
        if (st === 404 || st === 405 || st === 501) return api(fallbackPath);
      }
      throw err;
    }
  };

  const unwrapSuccessPayload = (payload) => {
    if (payload && typeof payload === "object" && Object.prototype.hasOwnProperty.call(payload, "success")) {
      if (!payload.success) {
        const message = (payload.error && payload.error.message) || "request failed";
        throw new Error(message);
      }
      return payload.data;
    }
    return payload;
  };

  const apiFirst = async (paths) => {
    let lastErr = null;
    for (const path of paths) {
      try {
        return unwrapSuccessPayload(await api(path));
      } catch (err) {
        lastErr = err;
        const st = Number(err && err.status);
        if (![0, 404, 405, 429, 500, 502, 503, 504].includes(st)) {
          throw err;
        }
      }
    }
    throw lastErr || new Error("all explorer sources unavailable");
  };

  const renderChipList = (container, values, variant = "") => {
    if (!values || values.length === 0) {
      container.innerHTML = "<span class=\"meta\">None</span>";
      return;
    }
    const extraClass = variant ? ` ${variant}` : "";
    container.innerHTML = values
      .map((v) => `<span class="chip${extraClass}">${v}</span>`)
      .join("");
  };

  const normalizeValidatorID = (value) => String(value || "").trim().toUpperCase();

  const sortedUniqueValidatorIDs = (values) => {
    const set = new Set();
    for (const raw of values || []) {
      const id = normalizeValidatorID(raw);
      if (!id) continue;
      set.add(id);
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  };

  const derivePeerConnectivity = (peerPayload) => {
    const healthySet = new Set();
    const unhealthyReasons = new Map();
    const peers = peerPayload && Array.isArray(peerPayload.peers) ? peerPayload.peers : [];
    for (const p of peers) {
      if (!p || !p.connected) continue;
      const vid = normalizeValidatorID(p.validator_id);
      if (!vid || vid === "-") continue;
      const isHelloOK = !!p.hello_ok;
      const isHashMatch = !!p.hash_match;
      if (isHelloOK && isHashMatch) {
        healthySet.add(vid);
        unhealthyReasons.delete(vid);
        continue;
      }
      if (healthySet.has(vid)) continue;
      const reasonParts = [];
      if (!isHashMatch) reasonParts.push("hash");
      if (!isHelloOK) reasonParts.push("hello");
      const incoming = reasonParts.join("+") || "health";
      const prev = unhealthyReasons.get(vid);
      if (!prev) {
        unhealthyReasons.set(vid, incoming);
        continue;
      }
      const merged = new Set(`${prev}+${incoming}`.split("+").map((x) => x.trim()).filter(Boolean));
      unhealthyReasons.set(vid, Array.from(merged).sort((a, b) => a.localeCompare(b)).join("+"));
    }
    return { healthySet, unhealthyReasons };
  };

  const renderValidatorsDualView = () => {
    const snap = state.latestValidators;
    const peerSnap = state.latestPeers;
    if (!snap || !peerSnap) return;

    const online = sortedUniqueValidatorIDs(snap.online);
    const offline = sortedUniqueValidatorIDs(snap.offline);
    const pendingAdd = Array.isArray(snap.pendingAdd) ? snap.pendingAdd : [];
    const pendingRemove = Array.isArray(snap.pendingRemove) ? snap.pendingRemove : [];

    const connectivity = derivePeerConnectivity(peerSnap);
    const connectedHealthy = Array.from(connectivity.healthySet).sort((a, b) => a.localeCompare(b));
    const connectedUnhealthy = Array.from(connectivity.unhealthyReasons.keys())
      .filter((id) => !connectivity.healthySet.has(id))
      .sort((a, b) => a.localeCompare(b))
      .map((id) => `${id} (${connectivity.unhealthyReasons.get(id) || "health"})`);
    const onlineSet = new Set(online);
    const gap = online.filter((id) => !connectivity.healthySet.has(id));

    renderChipList(els.validatorsOnline, online);
    renderChipList(els.validatorsOffline, offline, "offline");
    renderChipList(els.validatorsPendingAdd, pendingAdd);
    renderChipList(els.validatorsPendingRemove, pendingRemove, "offline");
    renderChipList(els.validatorsConnected, connectedHealthy);
    renderChipList(els.validatorsConnectedUnhealthy, connectedUnhealthy, "unhealthy");
    renderChipList(els.validatorsGap, gap, "offline");

    els.validatorMeta.textContent =
      `set_h=${snap.height ?? "-"} online_liveness=${onlineSet.size} offline=${offline.length} connected_healthy=${connectedHealthy.length} connected_unhealthy=${connectedUnhealthy.length}`;
  };

  const renderStatus = (status) => {
    const strictLive = asIntOrNull(status.validator_live_strict_count);
    const heartbeatLive = asIntOrNull(status.validator_live_heartbeat_count);
    const outOfDrift = asIntOrNull(status.validator_live_out_of_drift_count);
    const fallbackLive = asIntOrNull(status.live_validators);
    const requiredQuorum = asIntOrNull(status.required_quorum);
    const quorumLive = strictLive !== null ? strictLive : fallbackLive;
    const driftLimit = asIntOrNull(status.validator_liveness_max_height_drift_blocks);
    const mismatchHeight = asIntOrNull(status.validator_autoheal_last_mismatch_height);
    const successHeight = asIntOrNull(status.validator_autoheal_last_success_height);
    const laneCandidates = asIntOrNull(status.validator_bootstrap_lane_candidates);
    const laneSlotsUsed = asIntOrNull(status.validator_bootstrap_lane_slots_used);
    const blockAge = asIntOrNull(status.last_block_age_seconds);
    const expectedHash = asTextOrDash(status.validator_autoheal_expected_hash);
    const gotHash = asTextOrDash(status.validator_autoheal_got_hash);
    const mismatchHashText =
      expectedHash === "-" && gotHash === "-" && mismatchHeight === null
        ? "-"
        : `h=${mismatchHeight === null ? "-" : mismatchHeight} exp=${short(expectedHash, 6)} got=${short(gotHash, 6)}`;

    els.nodeId.textContent = status.node_id || "-";
    els.chainId.textContent = status.chain_id || "-";
    els.nodeRole.textContent = status.role || (status.is_validator ? "validator" : "full");
    const statusHeight = asIntOrNull(status.height);
    const statusFinalized = asIntOrNull(status.finalized_height);
    const displayHeight = Math.max(statusHeight || 0, state.realtimeHeight || 0) || status.height;
    const displayFinalized = Math.max(statusFinalized || 0, state.realtimeFinalized || 0) || status.finalized_height;
    renderLiveHeight({
      height: displayHeight,
      finalized: displayFinalized,
      lastBlockAge: blockAge,
      mode: status.consensus_detector_mode,
      peers: status.peers,
      networkHealth: status.network_health,
      syncing: status.syncing,
      ready: status.ready,
    });
    els.peerCount.textContent = String(status.peers ?? "-");
    els.quorum.textContent = `${quorumLive === null ? "-" : quorumLive} / ${requiredQuorum === null ? "-" : requiredQuorum}`;
    els.consensusDetectorMode.textContent = asTextOrDash(status.consensus_detector_mode);
    els.waitReason.textContent = status.wait_reason || "-";
    els.livenessMode.textContent = asTextOrDash(status.validator_liveness_mode);
    els.livenessDriftLimit.textContent = driftLimit === null ? "-" : `${driftLimit} blocks`;
    els.livenessCounts.textContent = `${strictLive === null ? "-" : strictLive} / ${heartbeatLive === null ? "-" : heartbeatLive} / ${outOfDrift === null ? "-" : outOfDrift}`;
    els.autohealState.textContent = asTextOrDash(status.validator_autoheal_state);
    els.autohealReason.textContent = asTextOrDash(status.validator_autoheal_last_reason);
    els.autohealMismatch.textContent = mismatchHashText;
    els.autohealSuccess.textContent = successHeight === null ? "-" : String(successHeight);
    els.bootstrapLane.textContent =
      laneSlotsUsed === null && laneCandidates === null
        ? "-"
        : `used=${laneSlotsUsed === null ? "-" : laneSlotsUsed} candidates=${laneCandidates === null ? "-" : laneCandidates}`;

    const parts = [];
    parts.push(status.ready ? "READY" : "NOT_READY");
    if (status.syncing) parts.push("SYNCING");
    if (status.consensus_running) parts.push("CONSENSUS");
    if (status.consensus_ready) parts.push("CONSENSUS_OK");
    els.stateText.textContent = parts.join(" | ");
  };

  const renderBlocks = (payload) => {
    const blocks = payload.blocks || [];
    els.blocksMeta.textContent = `latest=${payload.latest_height ?? "-"} finalized=${payload.finalized_height ?? "-"}`;

    if (blocks.length === 0) {
      els.blocksBody.innerHTML = "<tr><td colspan=\"7\">No blocks</td></tr>";
      return;
    }

    els.blocksBody.innerHTML = blocks
      .map((b) => {
        const sel = Number(state.selectedBlockHeight) === Number(b.height) ? " style=\"background:rgba(29,209,161,.12)\"" : "";
        return `<tr class="clickable" data-height="${b.height}"${sel}>
          <td class="mono">${b.height}</td>
          <td>${b.type}</td>
          <td class="mono">${b.proposer || "-"}</td>
          <td class="mono">${b.tx_count}</td>
          <td class="mono">${b.execution_result_count}</td>
          <td class="mono">${fmtBlockTime(b.timestamp, b.block_time, b.height)}</td>
          <td class="mono">${short(b.hash, 8)}</td>
        </tr>`;
      })
      .join("");

    els.blocksBody.querySelectorAll("tr.clickable").forEach((row) => {
      row.addEventListener("click", () => {
        const h = Number(row.getAttribute("data-height"));
        if (Number.isFinite(h) && h > 0) {
          state.selectedBlockHeight = h;
          loadBlockByHeight(h).catch((err) => showBlockError(err));
          renderBlocks(payload);
        }
      });
    });

    if (!state.selectedBlockHeight && blocks[0] && blocks[0].height) {
      state.selectedBlockHeight = Number(blocks[0].height);
      loadBlockByHeight(state.selectedBlockHeight).catch((err) => showBlockError(err));
    }
  };

  const showBlockError = (err) => {
    els.blockDetailMeta.textContent = "Error";
    els.blockDetail.textContent = `Failed to load block\n\n${err.message || err}`;
  };

  const renderBlockDetail = (data) => {
    const header = {
      height: data.height,
      hash: data.hash,
      prev_hash: data.prev_hash,
      type: data.type,
      proposer: data.proposer,
      timestamp: data.timestamp,
      timestamp_local: fmtBlockTime(data.timestamp, data.block_time, data.height),
      latest_height: data.latest_height,
      finalized_height: data.finalized_height,
      confirmations: data.confirmations,
      is_finalized: data.is_finalized,
      round: data.round,
      mempool_root: data.mempool_root,
      state_root: data.state_root,
      validator_set_hash: data.validator_set_hash,
      validator_registry_hash: data.validator_registry_hash || (data.summary && data.summary.validator_registry_hash) || "",
      tx_count: data.tx_count,
      execution_result_count: data.execution_result_count,
      receipt_count: data.receipt_count,
      signature_count: data.signature_count,
      signatures: data.signatures,
    };

    const txs = Array.isArray(data.transactions) ? data.transactions : [];
    const exec = Array.isArray(data.execution_results) ? data.execution_results : [];
    const receipts = Array.isArray(data.receipts) ? data.receipts : [];

    const packed = {
      header,
      transactions: txs.map((tx) => ({
        id: tx.id,
        from: tx.from,
        to: tx.to,
        amount: tx.amount,
        fee: tx.fee,
        nonce: tx.nonce,
        type: txTypeName(tx.type),
        coin: tx.coin || "MSC",
        chain_id: tx.ChainID || tx.chain_id || "",
        expiry: tx.expiry,
        stake_epochs: tx.stake_epochs,
        signature: tx.signature,
      })),
      execution_results: exec,
      receipts,
    };

    els.blockDetailMeta.textContent = `h=${data.height} tx=${txs.length} exec=${exec.length}`;
    els.blockDetail.textContent = JSON.stringify(packed, null, 2);
  };

  const buildCuratedTxView = (data) => {
    const out = {};
    const rootOrder = [
      "tx_id",
      "state",
      "height",
      "latest_height",
      "finalized_height",
      "confirmations",
      "is_finalized",
      "dtl_tx_type",
      "oracle_feed_id",
      "health_factor",
    ];
    for (const key of rootOrder) {
      if (data[key] !== undefined && data[key] !== null && data[key] !== "") {
        out[key] = data[key];
      }
    }

    if (data.tx && typeof data.tx === "object") {
      const tx = data.tx;
      const txView = {
        id: tx.id,
        from: tx.from,
        to: tx.to,
        amount: tx.amount,
        fee: tx.fee,
        nonce: tx.nonce,
        type: tx.type,
        type_name: txTypeName(tx.type),
        coin: tx.coin || "MSC",
      };
      if (tx.chain_id !== undefined) txView.chain_id = tx.chain_id;
      if (tx.ChainID !== undefined && txView.chain_id === undefined) txView.chain_id = tx.ChainID;
      if (tx.expiry !== undefined) txView.expiry = tx.expiry;
      if (tx.stake_epochs !== undefined) txView.stake_epochs = tx.stake_epochs;
      out.tx = txView;
    }

    if (data.block && typeof data.block === "object") {
      const block = { ...data.block };
      if (block.timestamp !== undefined) {
        block.timestamp_local = fmtBlockTime(block.timestamp, block.block_time, block.height);
      }
      out.block = block;
    }

    if (data.receipt && typeof data.receipt === "object") {
      out.receipt = data.receipt;
    }
    if (data.error !== undefined) {
      out.error = data.error;
    }
    if (data.receipts !== undefined) {
      out.receipts = data.receipts;
    }

    // Keep any non-legacy top-level keys in curated output.
    for (const [key, value] of Object.entries(data)) {
      if (out[key] !== undefined) continue;
      if (LEGACY_CONTRACT_TX_KEYS.has(key)) continue;
      if (value === undefined || value === null || value === "") continue;
      out[key] = value;
    }
    return out;
  };

  const updateTxRawToggleLabel = () => {
    if (!els.txRawToggle) return;
    els.txRawToggle.textContent = state.txRawMode ? "Show Curated View" : "Show Raw JSON";
  };

  const renderTxDetail = (data) => {
    state.lastTxPayload = data;
    if (state.txRawMode) {
      els.txDetailMeta.textContent = `state=${data.state || "-"} | raw`;
      els.txDetail.textContent = JSON.stringify(data, null, 2);
      updateTxRawToggleLabel();
      return;
    }
    const view = buildCuratedTxView(data);
    els.txDetailMeta.textContent = `state=${data.state || "-"} | curated`;
    els.txDetail.textContent = JSON.stringify(view, null, 2);
    updateTxRawToggleLabel();
  };

  const renderPeers = (data) => {
    const peers = data.peers || [];
    const roleCounts = { validator: 0, full: 0, light: 0 };
    for (const p of peers) {
      const role = (p.role || (p.validator_id ? "validator" : "full")).toLowerCase();
      if (role === "validator" || role === "full" || role === "light") {
        roleCounts[role] += 1;
      }
    }
    els.peersMeta.textContent = `count=${data.count ?? peers.length} v=${roleCounts.validator} f=${roleCounts.full} l=${roleCounts.light}`;

    if (peers.length === 0) {
      els.peersBody.innerHTML = "<tr><td colspan=\"9\">No peer records</td></tr>";
      return;
    }

    els.peersBody.innerHTML = peers
      .map((p) => {
        const connected = p.connected ? "YES" : "NO";
        const suspect = p.suspect_since && p.suspect_since > 0 ? fmtWallTime(p.suspect_since) : "-";
        const role = p.role || (p.validator_id ? "validator" : "full");
        return `<tr>
          <td class="mono">${short(p.peer_id, 12)}</td>
          <td class="mono">${role}</td>
          <td class="mono">${p.validator_id || "-"}</td>
          <td class="mono">${connected}</td>
          <td class="mono">${p.hello_ok ? "YES" : "NO"}</td>
          <td class="mono">${p.hash_match ? "YES" : "NO"}</td>
          <td class="mono">${p.ack_height ?? 0}</td>
          <td class="mono">${p.dial_failures ?? 0}</td>
          <td class="mono">${suspect}</td>
        </tr>`;
      })
      .join("");
  };

  const renderPublicNodes = (data) => {
    if (!els.publicNodesBody || !els.publicNodesMeta) return;
    const nodes = Array.isArray(data?.nodes) ? data.nodes : [];
    const healthy = Number(data?.healthy ?? nodes.filter((item) => item.healthy).length);
    const total = Number(data?.total ?? nodes.length);
    const best = data?.best || data?.best_node?.rpc_url || "-";
    els.publicNodesMeta.textContent = `healthy=${healthy}/${total} best=${best}`;

    if (!nodes.length) {
      els.publicNodesBody.innerHTML = "<tr><td colspan=\"14\">No public full nodes discovered yet</td></tr>";
      return;
    }
    const bestHeight = Math.max(0, ...nodes.map((node) => Number(node.height || 0)).filter((height) => Number.isFinite(height)));

    els.publicNodesBody.innerHTML = nodes
      .map((node) => {
        const status = node.health_state || (node.healthy ? "healthy" : node.suspicious_reason || node.error ? "unhealthy" : "warning");
        const gateway = node.active_gateway ? "active" : node.excluded_reason ? `standby:${node.excluded_reason}` : "standby";
        const reason = node.selected_reason || node.excluded_reason || node.health_reason || node.suspicious_reason || node.error || node.network_health || "-";
        const tone = publicNodeTone(node, bestHeight);
        const heightLag = publicNodeHeightLag(node, bestHeight);
        const finalityLag = asIntOrNull(node.finality_lag) || 0;
        const age = publicNodeDisplayAgeSeconds(node);
        return `<tr class="${tone}">
          <td class="mono">${node.id || "-"}</td>
          <td class="mono">${short(node.gateway_rpc_url || node.rpc_url || "-", 18)}</td>
          <td class="mono ${tone === "ok" ? "ok" : tone === "warn" ? "warn" : "bad"}">${status}</td>
          <td class="mono">${gateway}</td>
          <td class="mono">${node.height ?? 0}</td>
          <td class="mono">${fmtBlocks(heightLag)}</td>
          <td class="mono">${fmtBlocks(finalityLag)}</td>
          <td class="mono">${age === null ? "-" : fmtAge(age)}</td>
          <td class="mono">${node.peer_count ?? "-"}</td>
          <td class="mono">${fmtLatency(node.latency_ms)}</td>
          <td class="mono">${node.consensus_mode || "-"}</td>
          <td class="mono">${Math.round(Number(node.score || 0))}</td>
          <td class="mono">${publicNodeUptimePct(node)}</td>
          <td class="mono">${reason}</td>
        </tr>`;
      })
      .join("");
  };

  const normalizePendingEntries = (values) =>
    (Array.isArray(values) ? values : []).map((x) => {
      if (x && typeof x === "object") {
        const id = normalizeValidatorID(x.id);
        const activation = x.activation_height;
        if (!id || activation === undefined || activation === null || activation === "") {
          return "";
        }
        return `${id}@${activation}`;
      }
      return String(x || "").trim();
    }).filter(Boolean);

  const fetchValidatorsData = async () => {
    try {
      const current = await apiV1("/v1/validators");
      return {
        height: current.height,
        online: current.online_validators || [],
        offline: current.offline_validators || current.inactive_validators || [],
        pendingAdd: normalizePendingEntries(current.pending_add),
        pendingRemove: normalizePendingEntries(current.pending_remove),
      };
    } catch (err) {
      const st = Number(err && err.status);
      if (st !== 404 && st !== 405 && st !== 501) throw err;
    }

    const [current, pending] = await Promise.all([api("/validators"), api("/validators/pending")]);
    return {
      height: current.height,
      online: current.online_validators || [],
      offline: current.offline_validators || current.inactive_validators || [],
      pendingAdd: normalizePendingEntries(pending.pending_add),
      pendingRemove: normalizePendingEntries(pending.pending_remove),
    };
  };

  const fetchStatusData = async () => apiV1("/v1/status", "/status");

  const fetchBlocksData = async () =>
    apiFirst([
      "/indexer/blocks?limit=40",
      "/archive-rpc/explorer/blocks?limit=40",
      "/v1/blocks?limit=40",
      "/explorer/blocks?limit=40",
    ]);

  const fetchPeersData = async () => apiV1("/v1/peers", "/explorer/peers");

  const fetchPublicNodesData = async () => {
    try {
      return normalizeGatewayPublicNodes(await api("/gateway/lb-status.json"));
    } catch (_) {
      return apiV1("/v1/public-nodes", "/public-nodes");
    }
  };

  const refreshBlocksPanel = async () => {
    const payload = await fetchBlocksData();
    state.latestBlocks = payload;
    renderBlocks(payload);
    return payload;
  };

  const renderRealtimeEvent = (event) => {
    if (!event || typeof event !== "object") return;
    const sentMs = Number(event.ts_ms || (event.ts ? Number(event.ts) * 1000 : 0));
    if (Number.isFinite(sentMs) && sentMs > 0) {
      state.eventDelayMs = Math.max(0, Date.now() - sentMs);
      renderEventDelay();
    }
    const incomingHeight = asIntOrNull(event.height);
    const incomingFinalized = asIntOrNull(event.finalized_height);
    const currentHeight = Math.max(
      state.realtimeHeight || 0,
      asIntOrNull(state.latestStatus && state.latestStatus.height) || 0,
    );
    if (incomingHeight !== null && incomingHeight > 0) {
      state.realtimeHeight = Math.max(state.realtimeHeight || 0, incomingHeight);
    }
    if (incomingFinalized !== null && incomingFinalized > 0) {
      state.realtimeFinalized = Math.max(state.realtimeFinalized || 0, incomingFinalized);
    }

    const merged = {
      ...(state.latestStatus || {}),
      height: state.realtimeHeight || incomingHeight || state.latestStatus?.height,
      finalized_height: state.realtimeFinalized || incomingFinalized || state.latestStatus?.finalized_height,
      last_block_age_seconds:
        event.last_block_age_seconds !== undefined ? event.last_block_age_seconds : state.latestStatus?.last_block_age_seconds,
      consensus_detector_mode: event.mode || state.latestStatus?.consensus_detector_mode,
      consensus_detector_reason: event.reason || state.latestStatus?.consensus_detector_reason,
      peers: event.peer_count !== undefined ? event.peer_count : state.latestStatus?.peers,
      network_health: event.network_health || state.latestStatus?.network_health,
    };
    state.latestStatus = merged;

    if (Array.isArray(event.public_nodes)) {
      state.latestPublicNodes = mergePublicNodes(state.latestPublicNodes, {
        status: event.public_nodes_healthy === event.public_nodes_total ? "healthy" : event.public_nodes_healthy > 0 ? "degraded" : "down",
        healthy: event.public_nodes_healthy || 0,
        total: event.public_nodes_total || event.public_nodes.length,
        best: event.public_nodes_best || "",
        nodes: event.public_nodes,
        ts: event.ts || Math.floor(Date.now() / 1000),
      });
      recordPublicNodeUptime(state.latestPublicNodes.nodes);
      renderPublicNodes(state.latestPublicNodes);
    }

    if (incomingHeight !== null && incomingHeight > currentHeight + 1) {
      refreshBlocksPanel()
        .then((payload) => {
          const confirmed = (payload.blocks || [])
            .map((b) => asIntOrNull(b.height))
            .filter((h) => h !== null && h > currentHeight && h <= incomingHeight)
            .sort((a, b) => a - b);
          animateConfirmedHeights(confirmed.length ? confirmed : [incomingHeight], event);
        })
        .catch(() => animateConfirmedHeights([incomingHeight], event));
      return;
    }

    renderStatus(merged);
    if (event.type === "new_block") {
      refreshBlocksPanel().catch(() => {});
    }
  };

  const renderAllFromState = () => {
    if (state.latestStatus) renderStatus(state.latestStatus);
    if (state.latestBlocks) renderBlocks(state.latestBlocks);
    if (state.latestPeers) renderPeers(state.latestPeers);
    if (state.latestPublicNodes) renderPublicNodes(state.latestPublicNodes);
    if (state.latestValidators && state.latestPeers) renderValidatorsDualView();
  };

  const loadBlockByHeight = async (height) => {
    const encoded = encodeURIComponent(height);
    const data = await apiFirst([
      `/indexer/block?height=${encoded}`,
      `/archive-rpc/explorer/block?height=${encoded}`,
      `/explorer/block?height=${encoded}`,
    ]);
    renderBlockDetail(data);
  };

  const loadBlockByHash = async (hash) => {
    const encoded = encodeURIComponent(hash);
    const data = await apiFirst([
      `/indexer/block?hash=${encoded}`,
      `/archive-rpc/explorer/block?hash=${encoded}`,
      `/explorer/block?hash=${encoded}`,
    ]);
    state.selectedBlockHeight = Number(data.height) || 0;
    renderBlockDetail(data);
  };

  const loadTx = async (txId) => {
    const encoded = encodeURIComponent(txId);
    const data = await apiFirst([
      `/indexer/tx?tx_id=${encoded}`,
      `/archive-rpc/explorer/tx?tx_id=${encoded}`,
      `/v1/tx/${encoded}`,
      `/explorer/tx?tx_id=${encoded}`,
    ]);
    renderTxDetail(data);
  };

  const refreshAll = async () => {
    if (state.refreshInFlight) {
      state.refreshQueued = true;
      return;
    }
    state.refreshInFlight = true;
    const seq = state.refreshSeq + 1;
    state.refreshSeq = seq;
    try {
      const tasks = [
        fetchStatusData(),
        fetchBlocksData(),
        fetchValidatorsData(),
        fetchPeersData(),
        fetchPublicNodesData(),
      ];
      const results = await Promise.allSettled(tasks);
      if (seq < state.lastAppliedSeq) {
        return;
      }
      const nextStatus = results[0];
      const nextBlocks = results[1];
      const nextValidators = results[2];
      const nextPeers = results[3];
      const nextPublicNodes = results[4];
      if (nextStatus.status === "fulfilled") state.latestStatus = nextStatus.value;
      if (nextBlocks.status === "fulfilled") state.latestBlocks = nextBlocks.value;
      if (nextValidators.status === "fulfilled") state.latestValidators = nextValidators.value;
      if (nextPeers.status === "fulfilled") state.latestPeers = nextPeers.value;
      if (nextPublicNodes.status === "fulfilled") {
        state.latestPublicNodes = nextPublicNodes.value;
        recordPublicNodeUptime(state.latestPublicNodes.nodes);
      }
      state.lastAppliedSeq = seq;
      renderAllFromState();
      const failed = results.filter((r) => r.status === "rejected");
      if (failed.length === 0) {
        setConn("Connected", "ok");
      } else if (failed.length < results.length) {
        const sample = failed[0];
        const msg = sample && sample.reason && sample.reason.message ? sample.reason.message : "partial refresh error";
        setConn(`Warning: partial refresh (${failed.length}/${results.length}) - ${msg}`, "warn");
      } else {
        const first = failed[0];
        const msg = first && first.reason && first.reason.message ? first.reason.message : "refresh failed";
        setConn(`Error: ${msg}`, "bad");
      }
    } catch (err) {
      setConn(`Error: ${err.message || err}`, "bad");
    } finally {
      state.refreshInFlight = false;
      if (state.refreshQueued) {
        state.refreshQueued = false;
        refreshAll();
      }
    }
  };

  const restartTimer = () => {
    if (state.timer) clearInterval(state.timer);
    const interval = state.realtimeConnected ? Math.max(state.refreshMs, 15000) : state.refreshMs;
    state.timer = setInterval(refreshAll, interval);
  };

  const connectRealtime = (force = false) => {
    if (!window.WebSocket) {
      state.realtimeConnected = false;
      restartTimer();
      return;
    }
    if (!force && state.realtimeSocket && [WebSocket.CONNECTING, WebSocket.OPEN].includes(state.realtimeSocket.readyState)) {
      return;
    }
    try {
      state.realtimeSocket?.close();
    } catch (_) {
      // Best-effort cleanup before reconnecting to the active RPC.
    }
    const url = walletEventURL();
    if (!url) return;
    const ws = new WebSocket(url);
    state.realtimeSocket = ws;
    setConn("Connecting realtime...", "warn");
    ws.onopen = () => {
      if (state.realtimeSocket !== ws) return;
      state.realtimeConnected = true;
      state.realtimeAttempts = 0;
      setConn("Realtime connected", "ok");
      restartTimer();
    };
    ws.onmessage = (message) => {
      try {
        renderRealtimeEvent(JSON.parse(message.data || "{}"));
      } catch (_) {
        // Polling remains active as a safety net for malformed events.
      }
    };
    ws.onerror = () => {
      if (state.realtimeSocket !== ws) return;
      state.realtimeConnected = false;
      setConn("Realtime error - fallback polling", "warn");
    };
    ws.onclose = () => {
      if (state.realtimeSocket !== ws) return;
      state.realtimeConnected = false;
      restartTimer();
      const attempt = Math.min(6, state.realtimeAttempts + 1);
      state.realtimeAttempts = attempt;
      const delay = Math.min(60000, 1000 * (2 ** attempt)) + Math.floor(Math.random() * 1500);
      setConn("Realtime fallback polling", "warn");
      setTimeout(() => connectRealtime(), delay);
    };
  };

  const applyConnection = () => {
    state.rpcUrl = preferHttpsForLocalRpc((els.rpcUrl.value || "").trim() || window.location.origin);
    state.apiToken = (els.apiToken.value || "").replace(/^Bearer\s+/i, "").trim();
    if (state.apiToken) {
      setAdminMode(true);
    }

    const r = Number(els.refreshMs.value);
    state.refreshMs = Number.isFinite(r) && r >= 500 ? r : 3000;
    els.refreshMs.value = String(state.refreshMs);

    localStorage.setItem("msc_rpc", state.rpcUrl);
    localStorage.setItem("msc_token", state.apiToken);

    restartTimer();
    connectRealtime(true);
    refreshAll();
  };

  els.connectBtn.addEventListener("click", applyConnection);
  els.refreshBtn.addEventListener("click", refreshAll);
  if (els.adminToggleBtn) {
    els.adminToggleBtn.addEventListener("click", () => setAdminMode(!state.adminMode));
  }

  if (els.quickSearchForm && els.quickSearchInput) {
    els.quickSearchForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const query = (els.quickSearchInput.value || "").trim();
      if (!query) return;
      try {
        if (/^\d+$/.test(query)) {
          const h = Number(query);
          state.selectedBlockHeight = h;
          await loadBlockByHeight(h);
          return;
        }
        try {
          const search = await apiFirst([`/indexer/search?q=${encodeURIComponent(query)}`]);
          if (search && search.type === "block" && search.result) {
            const block = search.result;
            state.selectedBlockHeight = Number(block.height || block.summary?.height) || 0;
            renderBlockDetail(block);
            return;
          }
          if (search && search.type === "tx" && search.result) {
            renderTxDetail(search.result);
            return;
          }
          if (search && search.type === "address" && search.result) {
            els.txDetailMeta.textContent = `address=${query}`;
            els.txDetail.textContent = JSON.stringify(search.result, null, 2);
            return;
          }
        } catch (_) {
          // Indexer search is preferred when available; legacy lookups remain below.
        }
        try {
          await loadTx(query);
        } catch (_) {
          await loadBlockByHash(query);
        }
      } catch (err) {
        els.txDetailMeta.textContent = "Search error";
        els.txDetail.textContent = `Search failed\n\n${err.message || err}`;
        showBlockError(err);
      }
    });
  }

  els.blockSearchForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const h = Number(els.blockHeightInput.value);
    const hash = (els.blockHashInput.value || "").trim();

    try {
      if (hash) {
        await loadBlockByHash(hash);
      } else if (Number.isFinite(h) && h > 0) {
        state.selectedBlockHeight = h;
        await loadBlockByHeight(h);
      } else {
        throw new Error("Provide block height or hash");
      }
    } catch (err) {
      showBlockError(err);
    }
  });

  els.txSearchForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const txId = (els.txIdInput.value || "").trim();
    if (!txId) {
      els.txDetailMeta.textContent = "Error";
      els.txDetail.textContent = "Please enter a tx id";
      return;
    }
    try {
      await loadTx(txId);
    } catch (err) {
      els.txDetailMeta.textContent = "Error";
      els.txDetail.textContent = `Failed to load tx\n\n${err.message || err}`;
    }
  });

  if (els.txRawToggle) {
    els.txRawToggle.addEventListener("click", () => {
      state.txRawMode = !state.txRawMode;
      updateTxRawToggleLabel();
      if (state.lastTxPayload) {
        renderTxDetail(state.lastTxPayload);
      }
    });
    updateTxRawToggleLabel();
  }

  els.rpcUrl.value = state.rpcUrl;
  els.apiToken.value = state.apiToken;
  els.refreshMs.value = String(state.refreshMs);
  setAdminMode(state.adminMode);

  startBlockAgeTicker();
  restartTimer();
  connectRealtime();
  refreshAll();
})();
