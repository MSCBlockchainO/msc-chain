(() => {
  "use strict";

  const PAGE = document.body.dataset.explorerPage || "overview";
  const DEFAULT_BASE = window.location.origin;
  const MSC_LOGO_SRC = "assets/msc-logo-64.png";
  const MSC_APP_ICON_SRC = "assets/msc-app-icon-64.png";
  const MSC_WALLET_ICON_SRC = "assets/msc-wallet-icon.png";
  const MSC_VALIDATOR_BADGE_SRC = "assets/msc-validator-badge.png";
  const MSC_GOVERNANCE_BADGE_SRC = "assets/msc-governance-badge.png";
  const MSC_BRIDGE_BADGE_SRC = "assets/msc-bridge-badge.png";
  const EXPLORER_REQUEST_TIMEOUT_MS = 4500;
  const EXPLORER_FALLBACK_HEDGE_MS = 350;
  const EXPLORER_REALTIME_CONNECT_TIMEOUT_MS = 5000;
  const MSC_FIXED_SUPPLY = 9193823602;
  const MSC_TOKENOMICS_BUCKETS = Object.freeze([
    ["Team Treasury", "MSC_OWNER_ACCOUNT", 25],
    ["Foundation", "MSC_FOUNDATION", 15],
    ["Validator Bootstrap", "MSC_VALIDATOR_BOOTSTRAP", 10],
    ["Community", "MSC_COMMUNITY_POOL", 20],
    ["Future Rewards", "USER_REWARD_POOL", 30],
  ]);
  const state = {
    status: null,
    blocks: [],
    validators: null,
    leaderboard: null,
    peers: [],
    publicNodes: null,
    publicStatus: null,
    governance: null,
    tokenomics: null,
    tokenomicsAudit: null,
    storage: null,
    snapshot: null,
    bridge: null,
    security: null,
    misbehavior: null,
    coins: null,
    recentTxs: [],
    recentTxRequest: 0,
    lastUpdated: 0,
    realtime: {
      socket: null,
      connected: false,
      reconnectAttempts: 0,
      displayHeight: 0,
      lastBlockAgeBaseSeconds: null,
      lastBlockAgeUpdatedAt: 0,
      queue: [],
      queuedHeights: new Set(),
      processing: false,
      connectTimer: null,
    },
  };

  const navGroups = [
    ["Core", [
      ["overview", "Dashboard", "explorer.html", "layout-dashboard"],
      ["blocks", "Blocks", "explorer-blocks.html", "box"],
      ["transactions", "Transactions", "explorer-transactions.html", "arrow-left-right"],
      ["addresses", "Addresses", "explorer-addresses.html", "wallet-cards"],
      ["validators", "Validators", "explorer-validators.html", "shield-check", MSC_VALIDATOR_BADGE_SRC],
    ]],
    ["Governance", [
      ["governance", "Governance", "explorer-governance.html", "landmark", MSC_GOVERNANCE_BADGE_SRC],
      ["treasury", "Treasury", "explorer-treasury.html", "vault"],
      ["council", "Council", "explorer-council.html", "users"],
    ]],
    ["Insights", [
      ["analytics", "Analytics", "explorer-analytics.html", "chart-no-axes-combined"],
      ["charts", "Charts", "explorer-charts.html", "chart-spline"],
      ["tokenomics", "Tokenomics", "explorer-tokenomics.html", "coins"],
      ["rich-list", "Rich list", "explorer-rich-list.html", "list-ordered"],
      ["staking", "Staking", "explorer-staking.html", "badge-percent"],
    ]],
    ["Infrastructure", [
      ["network", "Network", "explorer-network.html", "waypoints"],
      ["nodes", "Nodes", "explorer-nodes.html", "server"],
      ["snapshots", "Snapshots", "explorer-snapshots.html", "database-backup"],
      ["mempool", "Mempool", "explorer-mempool.html", "list-start"],
      ["epochs", "Epochs", "explorer-epochs.html", "rotate-cw"],
    ]],
    ["Resources", [
      ["bridge", "Bridge", "explorer-bridge.html", "route", MSC_BRIDGE_BADGE_SRC],
      ["security", "Security", "explorer-security.html", "shield-alert"],
      ["api", "API", "explorer-api.html", "braces"],
      ["search", "Search", "explorer-search.html", "search"],
      ["settings", "Settings", "explorer-settings.html", "settings-2"],
    ]],
  ];
  const nav = navGroups.flatMap(([, items]) => items);
  const mobileNav = [
    nav.find(([key]) => key === "overview"),
    nav.find(([key]) => key === "blocks"),
    nav.find(([key]) => key === "transactions"),
    nav.find(([key]) => key === "addresses"),
  ];

  const $ = (id) => document.getElementById(id);
  const esc = (value) => String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[ch]));
  const unwrap = (value) => value && value.success && value.data !== undefined ? value.data : value;
  const fmt = (value) => {
    const n = Number(value);
    return Number.isFinite(n) ? new Intl.NumberFormat("en-US").format(Math.round(n)) : "-";
  };
  const short = (value, left = 8, right = 6) => {
    const raw = String(value || "");
    return raw.length > left + right + 3 ? `${raw.slice(0, left)}...${raw.slice(-right)}` : raw || "-";
  };
  const age = (seconds) => {
    const n = Number(seconds);
    if (!Number.isFinite(n)) return "-";
    if (n < 60) return `${Math.max(0, Math.round(n))}s`;
    if (n < 3600) return `${Math.round(n / 60)}m`;
    return `${Math.round(n / 3600)}h`;
  };
  const delay = (ms) => new Promise((resolve) => window.setTimeout(resolve, ms));
  const tone = (value, good, warn) => {
    const n = Number(value);
    if (!Number.isFinite(n)) return "";
    if (n <= good) return "good";
    if (n <= warn) return "warn";
    return "bad";
  };
  const clamp = (value, min = 0, max = 100) => Math.max(min, Math.min(max, Number(value) || 0));
  const pct = (value, total) => {
    const n = Number(value);
    const d = Number(total);
    if (!Number.isFinite(n) || !Number.isFinite(d) || d <= 0) return 0;
    return clamp((n / d) * 100);
  };
  const UNKNOWN_PROPOSER_TEXT = "Unknown Proposer";
  const UNKNOWN_PROPOSER_TITLE = "Proposer data unavailable in block header";
  const proposerLabel = (value) => {
    const label = String(value ?? "").trim();
    if (!label || label === "-" || /^(unknown|null|undefined)$/i.test(label)) return "";
    return label;
  };
  const firstProposerLabel = (...values) => {
    for (const value of values) {
      const label = proposerLabel(value);
      if (label) return label;
    }
    return "";
  };
  const proposerHTML = (value, options = {}) => {
    const label = proposerLabel(value);
    if (label) return `<span class="mono">${esc(label)}</span>`;
    const text = options.compact ? "Unknown" : UNKNOWN_PROPOSER_TEXT;
    return `<span class="badge warn proposer-unknown" title="${esc(UNKNOWN_PROPOSER_TITLE)}">${esc(text)}</span>`;
  };
  const firstBlockType = (...values) => {
    for (const value of values) {
      const label = String(value ?? "").trim();
      if (label && label !== "-") return label;
    }
    return "BLOCK";
  };
  const blockTypeInfo = (block = {}) => {
    const raw = firstBlockType(block.type, block.block_type).toUpperCase();
    const txCount = Number(block.tx_count || block.transactions?.length || 0);
    if (raw === "TIME") {
      return {
        raw,
        label: "Empty",
        tone: "good",
        title: "TIME block: no user transactions; keeps finality and chain clock moving.",
      };
    }
    if (raw === "WORK") {
      return { raw, label: "Work", tone: "good", title: "WORK block" };
    }
    if (raw === "TASK") {
      return { raw, label: "Task", tone: "good", title: "TASK block" };
    }
    if (raw === "BLOCK") {
      return {
        raw,
        label: txCount > 0 ? "Tx block" : "Block",
        tone: txCount > 0 ? "good" : "",
        title: "Standard block",
      };
    }
    return { raw, label: raw, tone: "", title: `${raw} block` };
  };
  const blockTypeHTML = (block = {}) => {
    const info = blockTypeInfo(block);
    const toneClass = info.tone ? ` ${info.tone}` : "";
    return `<span class="badge block-type-badge${toneClass}" title="${esc(info.title)}"><span>${esc(info.label)}</span><span class="badge-raw">${esc(info.raw)}</span></span>`;
  };
  const firstFiniteNumber = (...values) => {
    for (const value of values) {
      const n = Number(value);
      if (Number.isFinite(n)) return n;
    }
    return null;
  };
  const blockQuorumInfo = (block = {}, status = {}) => ({
    votes: firstFiniteNumber(block.signature_count, block.vote_count, Array.isArray(block.signatures) ? block.signatures.length : undefined, block.execution_result_count),
    required: firstFiniteNumber(block.required_quorum, status.required_quorum),
    committee: firstFiniteNumber(block.committee_size, block.active_ready_count, status.committee_size, status.live_validators, status.committee_live_count),
  });
  const quorumBreakdownText = (block, status = {}) => {
    const info = blockQuorumInfo(block, status);
    return `Votes: ${fmt(info.votes)} Required: ${fmt(info.required)} Committee: ${fmt(info.committee)}`;
  };
  const quorumBreakdownHTML = (block, status = {}) => {
    const info = blockQuorumInfo(block, status);
    return `<span class="quorum-breakdown" title="Votes collected / required quorum / committee size">
      <span>Votes: ${esc(fmt(info.votes))}</span>
      <span>Required: ${esc(fmt(info.required))}</span>
      <span>Committee: ${esc(fmt(info.committee))}</span>
    </span>`;
  };
  const sum = (items, getter) => (items || []).reduce((total, item) => total + Number(getter(item) || 0), 0);
  const normalizeTokenomics = (raw = {}) => {
    const source = raw || {};
    const rawPolicy = source.economic_policy || {};
    const rawInflation = rawPolicy.inflation || {};
    const audit = source.audit || {};
    const maxSupply = Number(audit.max_supply ?? source.max_supply ?? MSC_FIXED_SUPPLY) || MSC_FIXED_SUPPLY;
    const totalSupply = Number(audit.current_supply ?? source.total_supply ?? MSC_FIXED_SUPPLY) || MSC_FIXED_SUPPLY;
    const rawBuckets = Array.isArray(source.buckets) && source.buckets.length
      ? source.buckets
      : MSC_TOKENOMICS_BUCKETS.map(([name, address, percent]) => ({ name, address, percent }));
    const buckets = rawBuckets.map((bucket) => {
      const percent = Number(bucket.percent);
      const allocation = Number.isFinite(percent)
        ? Math.round((maxSupply * percent) / 100)
        : Number(bucket.allocation || bucket.balance || 0);
      const balance = Number(bucket.balance);
      return {
        ...bucket,
        allocation,
        balance: Number.isFinite(balance) ? balance : allocation,
      };
    });
    const observedSupply = Number(source.observed_total_supply ?? audit.current_supply ?? source.total_supply ?? 0);
    return {
      ...source,
      audit,
      total_supply: totalSupply,
      max_supply: maxSupply,
      supply_cap_surplus: Math.max(0, Number(audit.supply_cap_surplus ?? observedSupply - maxSupply)),
      buckets,
      economic_policy: {
        ...rawPolicy,
        fixed_total_supply: maxSupply,
        inflation: {
          burn_bps: 800,
          ...rawInflation,
          fixed_supply_cap_enforced: true,
        },
      },
    };
  };
  const configuredBase = () => {
    const selected = String(localStorage.getItem("msc_explorer_rpc") || "").trim();
    if (!selected || selected === "same-origin") return DEFAULT_BASE;
    try {
      const url = new URL(selected, DEFAULT_BASE);
      if (window.location.protocol === "https:" && url.protocol !== "https:") return DEFAULT_BASE;
      return url.origin;
    } catch (_) {
      return DEFAULT_BASE;
    }
  };

  function setText(id, value) {
    const node = $(id);
    if (node) node.textContent = value ?? "-";
  }

  function setHTML(id, value) {
    const node = $(id);
    if (node) node.innerHTML = value;
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
        <div class="chart-title"><strong>${esc(title)}</strong><span>${esc(meta)}</span></div>
        ${chip ? `<span class="badge">${esc(chip)}</span>` : ""}
      </div>
      ${body}`;
  }

  function barsHTML(items) {
    if (!items.length) return "";
    const max = Math.max(...items.map((item) => Number(item.value) || 0), 1);
    return `<div class="chart-bars">${items.map((item) => {
      const height = clamp(((Number(item.value) || 0) / max) * 100, 4, 100);
      return `<span class="chart-bar" title="${esc(item.label)}: ${esc(item.value)}"><span class="bar-fill" style="--bar-height:${height}%;"></span></span>`;
    }).join("")}</div>`;
  }

  function lineHTML(values) {
    const nums = values.map((value) => Number(value) || 0);
    if (!nums.length) return "";
    const max = Math.max(...nums, 1);
    const step = nums.length > 1 ? 100 / (nums.length - 1) : 100;
    const points = nums.map((value, index) => {
      const x = nums.length > 1 ? index * step : 50;
      const y = 92 - clamp((value / max) * 84, 0, 84);
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    }).join(" ");
    const area = `0,100 ${points} 100,100`;
    return `<div class="chart-line"><svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true"><polygon class="line-area" points="${area}"></polygon><polyline class="line-path" points="${points}"></polyline></svg></div>`;
  }

  function donutHTML(value, label, detail, toneName = "good") {
    const color = toneName === "bad" ? "var(--coral)" : toneName === "warn" ? "var(--amber)" : "var(--mint)";
    const safe = clamp(value);
    return `<div class="chart-donut">
      <div class="donut-ring" style="--donut-value:${safe};--donut-color:${color};"></div>
      <div class="donut-copy"><div class="donut-value">${Math.round(safe)}%</div><div class="donut-label">${esc(label)}<br>${esc(detail)}</div></div>
    </div>`;
  }

  function rowsHTML(items) {
    if (!items.length) return "";
    const max = Math.max(...items.map((item) => Number(item.value) || 0), 1);
    return `<div class="chart-list">${items.map((item) => {
      const width = clamp(((Number(item.value) || 0) / max) * 100);
      const color = item.tone === "bad" ? "var(--coral)" : item.tone === "warn" ? "var(--amber)" : item.color || "var(--mint)";
      return `<div class="chart-row" ${item.title ? `title="${esc(item.title)}"` : ""}>
        <span>${esc(item.label)}</span>
        <span class="chart-track"><span style="--track-value:${width}%;--track-color:${color};"></span></span>
        <strong class="mono">${esc(item.display ?? item.value)}</strong>
      </div>`;
    }).join("")}</div>`;
  }

  function matrixHTML(items) {
    if (!items.length) return "";
    return `<div class="matrix-grid">${items.map((item) => `
      <div class="matrix-cell"><span>${esc(item.label)}</span><strong>${esc(item.value)}</strong></div>
    `).join("")}</div>`;
  }

  async function request(path, options = {}) {
    const {
      timeoutMs = EXPLORER_REQUEST_TIMEOUT_MS,
      signal: upstreamSignal,
      ...fetchOptions
    } = options;
    const controller = new AbortController();
    const abortFromUpstream = () => controller.abort(upstreamSignal?.reason);
    if (upstreamSignal?.aborted) abortFromUpstream();
    else upstreamSignal?.addEventListener("abort", abortFromUpstream, { once: true });
    const timer = window.setTimeout(() => controller.abort(), Math.max(1, Number(timeoutMs) || EXPLORER_REQUEST_TIMEOUT_MS));
    try {
      const response = await fetch(`${configuredBase()}${path}`, {
        cache: "no-store",
        ...fetchOptions,
        signal: controller.signal,
        headers: {
          ...(fetchOptions.body ? { "Content-Type": "application/json" } : {}),
          ...(fetchOptions.headers || {}),
        },
      });
      if (!response.ok) throw new Error(`${response.status} ${response.statusText}`.trim());
      return unwrap(await response.json());
    } catch (error) {
      if (controller.signal.aborted && !upstreamSignal?.aborted) {
        throw new Error(`Explorer request timed out after ${timeoutMs}ms`);
      }
      throw error;
    } finally {
      window.clearTimeout(timer);
      upstreamSignal?.removeEventListener("abort", abortFromUpstream);
    }
  }

  async function first(paths, options = {}) {
    const candidates = [...new Set((paths || []).filter(Boolean))];
    if (!candidates.length) throw new Error("No explorer source available");
    const timeoutMs = Math.max(1, Number(options.timeoutMs) || EXPLORER_REQUEST_TIMEOUT_MS);
    const controller = new AbortController();
    const timers = [];
    let completed = 0;
    let settled = false;
    let lastError;
    return new Promise((resolve, reject) => {
      const finishFailure = (error) => {
        completed += 1;
        lastError = error;
        if (completed !== candidates.length || settled) return;
        settled = true;
        controller.abort();
        reject(lastError || new Error("No explorer source available"));
      };
      candidates.forEach((path, index) => {
        timers.push(window.setTimeout(() => {
          request(path, { ...options, timeoutMs, signal: controller.signal }).then((value) => {
            if (settled) return;
            settled = true;
            controller.abort();
            timers.forEach((timer) => window.clearTimeout(timer));
            resolve(value);
          }).catch(finishFailure);
        }, index * EXPLORER_FALLBACK_HEDGE_MS));
      });
      timers.push(window.setTimeout(() => {
        if (settled) return;
        settled = true;
        controller.abort();
        timers.forEach((timer) => window.clearTimeout(timer));
        reject(lastError || new Error(`Explorer sources timed out after ${timeoutMs}ms`));
      }, timeoutMs));
    });
  }

  function explorerEventURL() {
    try {
      const url = new URL(configuredBase());
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.pathname = "/wallet/events";
      url.search = "";
      url.hash = "";
      return url.toString();
    } catch (_) {
      return "";
    }
  }

  function currentBlockAge() {
    const base = Number(state.realtime.lastBlockAgeBaseSeconds);
    if (!Number.isFinite(base) || base < 0) return Number(state.status?.last_block_age_seconds);
    const elapsed = Math.max(0, Math.floor((Date.now() - state.realtime.lastBlockAgeUpdatedAt) / 1000));
    return Math.trunc(base) + elapsed;
  }

  function setBlockAgeBase(value = 0) {
    const next = Number(value);
    if (!Number.isFinite(next) || next < 0) return;
    state.realtime.lastBlockAgeBaseSeconds = Math.trunc(next);
    state.realtime.lastBlockAgeUpdatedAt = Date.now();
    renderLiveBlockAge();
  }

  function renderLiveBlockAge() {
    const value = age(currentBlockAge());
    setText("topBlockAge", value);
    setText("metricBlockAge", value);
  }

  function setExplorerRealtimeStatus(text, toneName = "") {
    const node = $("explorerRealtime");
    if (!node) return;
    node.textContent = text;
    node.classList.remove("good", "warn", "bad");
    if (toneName) node.classList.add(toneName);
  }

  function normalizeBlockPayload(payload, fallback = {}) {
    const source = payload || {};
    const summary = source.summary || source.block?.summary || source.block || source;
    const block = source.block && typeof source.block === "object" ? source.block : {};
    const header = summary.header || summary.block_header || source.header || source.block_header || block.header || block.block_header || {};
    const transactions = source.transactions || source.txs || summary.transactions || summary.txs || [];
    const executionResults = source.execution_results || summary.execution_results || [];
    return {
      ...fallback,
      ...summary,
      height: Number(summary.height || source.height || fallback.height || 0),
      hash: summary.hash || summary.block_hash || source.hash || fallback.hash || "",
      proposer: firstProposerLabel(
        summary.proposer,
        source.proposer,
        block.proposer,
        header.proposer,
        summary.proposer_id,
        source.proposer_id,
        block.proposer_id,
        header.proposer_id,
        summary.leader,
        source.leader,
        fallback.proposer,
      ),
      type: firstBlockType(summary.type, source.block_type, block.type, header.type, source.type, fallback.type),
      tx_count: summary.tx_count ?? source.tx_count ?? transactions.length ?? fallback.tx_count ?? 0,
      execution_result_count:
        summary.execution_result_count ?? source.execution_result_count ?? executionResults.length ?? fallback.execution_result_count ?? 0,
    };
  }

  function mergeBlock(block) {
    const height = Number(block?.height || 0);
    if (!height) return;
    state.blocks = [block, ...(state.blocks || []).filter((candidate) => Number(candidate.height) !== height)]
      .sort((a, b) => Number(b.height || 0) - Number(a.height || 0))
      .slice(0, 40);
  }

  async function hydrateRealtimeBlock(height, event = {}, attempt = 0) {
    try {
      const payload = await first([
        `/archive-rpc/explorer/block?height=${encodeURIComponent(height)}`,
        `/explorer/block?height=${encodeURIComponent(height)}`,
        `/indexer/block?height=${encodeURIComponent(height)}`,
      ]);
      mergeBlock(normalizeBlockPayload(payload, {
        height,
        hash: event.hash || "",
        proposer: event.proposer || event.block_proposer || event.leader || "",
        type: event.block_type || "",
        tx_count: event.tx_count,
        execution_result_count: event.execution_result_count,
      }));
      renderBlocks();
      renderExplorerCharts();
    } catch (_) {
      if (attempt < 5) {
        window.setTimeout(() => hydrateRealtimeBlock(height, event, attempt + 1), 400 * (attempt + 1));
      }
    }
  }

  function enqueueRealtimeBlocks(event) {
    const target = Number(event?.height || 0);
    if (!target) return;
    const displayed = Number(state.realtime.displayHeight || state.status?.height || state.blocks?.[0]?.height || 0);
    if (target <= displayed) {
      hydrateRealtimeBlock(target, event);
      return;
    }
    const start = displayed > 0 && target - displayed <= 64 ? displayed + 1 : target;
    for (let height = start; height <= target; height += 1) {
      if (state.realtime.queuedHeights.has(height)) continue;
      state.realtime.queuedHeights.add(height);
      state.realtime.queue.push({
        height,
        event: height === target ? event : { ...event, height, hash: "" },
      });
    }
    processRealtimeBlockQueue();
  }

  async function processRealtimeBlockQueue() {
    if (state.realtime.processing) return;
    state.realtime.processing = true;
    while (state.realtime.queue.length) {
      const item = state.realtime.queue.shift();
      state.realtime.queuedHeights.delete(item.height);
      const event = item.event || {};
      mergeBlock(normalizeBlockPayload({}, {
        height: item.height,
        hash: event.hash || "",
        type: event.block_type || "BLOCK",
        proposer: event.proposer || event.block_proposer || event.leader || "-",
        consensus_mode: event.mode || state.status?.consensus_detector_mode || "",
        required_quorum: event.quorum ?? state.status?.required_quorum,
        active_ready_count: event.active_validators ?? state.status?.active_ready_count,
        tx_count: event.tx_count ?? 0,
        execution_result_count: event.execution_result_count ?? 0,
      }));
      state.realtime.displayHeight = item.height;
      state.status = {
        ...(state.status || {}),
        height: item.height,
        finalized_height: Number(event.finalized_height || state.status?.finalized_height || 0),
        consensus_detector_mode: event.mode || state.status?.consensus_detector_mode,
        network_health: event.network_health || state.status?.network_health,
        last_block_age_seconds: 0,
      };
      setBlockAgeBase(0);
      renderCommon();
      renderBlocks();
      renderExplorerCharts();
      hydrateRealtimeBlock(item.height, event);
      await delay(180);
    }
    state.realtime.processing = false;
  }

  function handleExplorerRealtimeEvent(event) {
    if (!event || typeof event !== "object") return;
    if (event.last_block_age_seconds !== undefined) setBlockAgeBase(event.last_block_age_seconds);
    if (event.type === "hello") {
      state.realtime.displayHeight = Number(event.height || state.realtime.displayHeight || 0);
      state.status = {
        ...(state.status || {}),
        height: state.realtime.displayHeight,
        finalized_height: Number(event.finalized_height || state.status?.finalized_height || 0),
        consensus_detector_mode: event.mode || state.status?.consensus_detector_mode,
        network_health: event.network_health || state.status?.network_health,
        peers: event.peer_count ?? state.status?.peers,
        live_validators: event.active_validators ?? state.status?.live_validators,
        required_quorum: event.quorum ?? state.status?.required_quorum,
      };
      renderCommon();
      hydrateRealtimeBlock(state.realtime.displayHeight, event);
      return;
    }
    if (event.type === "new_block") {
      enqueueRealtimeBlocks(event);
      return;
    }
    state.status = {
      ...(state.status || {}),
      finalized_height: Number(event.finalized_height || state.status?.finalized_height || 0),
      consensus_detector_mode: event.mode || state.status?.consensus_detector_mode,
      network_health: event.network_health || state.status?.network_health,
      peers: event.peer_count ?? state.status?.peers,
      live_validators: event.active_validators ?? state.status?.live_validators,
      required_quorum: event.quorum ?? state.status?.required_quorum,
    };
    renderCommon();
  }

  function connectExplorerRealtime(force = false) {
    if (!window.WebSocket) {
      setExplorerRealtimeStatus("Polling", "warn");
      return;
    }
    if (!force && state.realtime.socket && [WebSocket.CONNECTING, WebSocket.OPEN].includes(state.realtime.socket.readyState)) return;
    try {
      state.realtime.socket?.close();
    } catch (_) {
      // Best-effort cleanup before reconnecting.
    }
    const url = explorerEventURL();
    if (!url) return;
    const socket = new WebSocket(url);
    state.realtime.socket = socket;
    setExplorerRealtimeStatus("Connecting", "warn");
    window.clearTimeout(state.realtime.connectTimer);
    state.realtime.connectTimer = window.setTimeout(() => {
      if (state.realtime.socket !== socket || socket.readyState !== WebSocket.CONNECTING) return;
      setExplorerRealtimeStatus("Polling", "warn");
      socket.close();
    }, EXPLORER_REALTIME_CONNECT_TIMEOUT_MS);
    socket.onopen = () => {
      window.clearTimeout(state.realtime.connectTimer);
      state.realtime.connected = true;
      state.realtime.reconnectAttempts = 0;
      setExplorerRealtimeStatus("Live", "good");
    };
    socket.onmessage = (message) => {
      try {
        handleExplorerRealtimeEvent(JSON.parse(message.data || "{}"));
      } catch (_) {
        // Malformed events are ignored; the uncached polling fallback remains active.
      }
    };
    socket.onerror = () => {
      window.clearTimeout(state.realtime.connectTimer);
      state.realtime.connected = false;
      setExplorerRealtimeStatus("Polling", "warn");
    };
    socket.onclose = () => {
      if (state.realtime.socket !== socket) return;
      window.clearTimeout(state.realtime.connectTimer);
      state.realtime.connected = false;
      setExplorerRealtimeStatus("Polling", "warn");
      const attempt = Math.min(6, state.realtime.reconnectAttempts + 1);
      state.realtime.reconnectAttempts = attempt;
      window.setTimeout(() => connectExplorerRealtime(), Math.min(30000, 1000 * (2 ** attempt)));
    };
  }

  function navLinks(mobile = false) {
    const items = mobile ? mobileNav : nav;
    return items.map(([key, label, href, icon, asset]) => `
      <a class="${key === PAGE ? "active" : ""}" href="${href}">
        ${navIcon(icon, asset)}${mobile ? `<span>${label}</span>` : label}
      </a>`).join("");
  }

  function navIcon(icon, asset) {
    return asset
      ? `<img class="nav-brand-icon" src="${asset}" alt="" />`
      : `<i data-lucide="${icon}"></i>`;
  }

  function groupedNavLinks() {
    return navGroups.map(([group, items]) => `
      <div class="side-nav-group">
        <div class="side-section-label">${esc(group)}</div>
        <nav class="explorer-nav" aria-label="${esc(group)} navigation">${items.map(([key, label, href, icon, asset]) => `
          <a class="${key === PAGE ? "active" : ""}" href="${href}">${navIcon(icon, asset)}${esc(label)}</a>
        `).join("")}</nav>
      </div>`).join("");
  }

  function fullNavLinks() {
    return navGroups.map(([group, items]) => `
      <section class="mobile-menu-group"><div class="side-section-label">${esc(group)}</div>
        <nav>${items.map(([key, label, href, icon, asset]) => `<a class="${key === PAGE ? "active" : ""}" href="${href}">${navIcon(icon, asset)}<span>${esc(label)}</span></a>`).join("")}</nav>
      </section>`).join("");
  }

  function installShell() {
    const content = document.querySelector(".explorer-content");
    if (!content || document.querySelector(".explorer-shell")) return;
    const shell = document.createElement("div");
    shell.className = "explorer-shell";
    shell.innerHTML = `
      <aside class="explorer-sidebar">
        <a class="explorer-brand" href="explorer.html">
          <span class="brand-mark"><img src="${MSC_LOGO_SRC}" alt="MSC logo" /></span>
          <span class="brand-copy">
            <span class="brand-title">Chain Explorer</span>
            <span class="brand-subtitle">Mainnet 91938</span>
          </span>
        </a>
        <div class="sidebar-scroll">${groupedNavLinks()}</div>
        <div>
          <div class="side-section-label">Products</div>
          <nav class="explorer-nav">
            <a href="https://wallet.mscblockexplorer.in"><img class="nav-brand-icon" src="${MSC_WALLET_ICON_SRC}" alt="" />Wallet</a>
            <a href="portal/index.html"><img class="nav-brand-icon" src="${MSC_APP_ICON_SRC}" alt="" />Network portal</a>
            <a href="dtl_ide.html"><i data-lucide="code-2"></i>DTL IDE</a>
            <a href="https://github.com/MSCBlockchainO/msc-chain" target="_blank" rel="noopener noreferrer"><i data-lucide="github"></i>GitHub</a>
          </nav>
        </div>
        <div class="sidebar-foot">
          <div class="sidebar-foot-row"><span>Network</span><strong id="sideNetwork">Checking</strong></div>
          <div class="sidebar-foot-row"><span>Live data</span><span class="live-dot" aria-label="Live"></span></div>
        </div>
      </aside>
      <div class="explorer-main">
        <header class="explorer-topbar">
          <form id="globalSearch" class="top-search">
            <i data-lucide="search"></i>
            <input id="globalSearchInput" type="search" placeholder="Search block height, hash, transaction, or address" autocomplete="off" />
            <span class="key-hint">Enter</span>
          </form>
          <div class="top-status">
            <span class="status-chip">Height <strong id="topHeight">-</strong></span>
            <span class="status-chip">Finalized <strong id="topFinalized">-</strong></span>
            <span class="status-chip">Age <strong id="topBlockAge">-</strong></span>
            <span class="status-chip">CMD <strong id="topMode">-</strong></span>
            <span id="explorerRealtime" class="status-chip warn">Connecting</span>
          </div>
          <div class="top-actions">
            <a class="icon-button" href="https://github.com/MSCBlockchainO/msc-chain" target="_blank" rel="noopener noreferrer" title="Open MSC Chain on GitHub" aria-label="Open MSC Chain on GitHub"><i data-lucide="github"></i></a>
            <button id="refreshExplorer" class="icon-button" type="button" title="Refresh explorer" aria-label="Refresh explorer"><i data-lucide="refresh-cw"></i></button>
          </div>
        </header>
      </div>
      <nav class="mobile-nav" aria-label="Mobile explorer navigation">
        ${navLinks(true)}
        <button id="mobileMenuToggle" type="button" aria-label="Open all explorer pages"><i data-lucide="menu"></i><span>More</span></button>
      </nav>
      <aside id="mobileMenu" class="mobile-menu" aria-label="All explorer pages">
        <div class="mobile-menu-head"><strong>Explorer pages</strong><button id="mobileMenuClose" class="icon-button" type="button" aria-label="Close explorer pages"><i data-lucide="x"></i></button></div>
        <div class="mobile-menu-scroll">${fullNavLinks()}</div>
      </aside>
      <button id="mobileMenuBackdrop" class="mobile-menu-backdrop" type="button" aria-label="Close explorer pages"></button>`;
    document.body.appendChild(shell);
    shell.querySelector(".explorer-main").appendChild(content);
    window.lucide?.createIcons();
  }

  function bindShell() {
    $("globalSearch")?.addEventListener("submit", (event) => {
      event.preventDefault();
      routeSearch($("globalSearchInput")?.value || "");
    });
    $("refreshExplorer")?.addEventListener("click", () => refresh(true));
    const toggleMenu = (open) => {
      $("mobileMenu")?.classList.toggle("open", open);
      $("mobileMenuBackdrop")?.classList.toggle("open", open);
      document.body.classList.toggle("menu-open", open);
    };
    $("mobileMenuToggle")?.addEventListener("click", () => toggleMenu(true));
    $("mobileMenuClose")?.addEventListener("click", () => toggleMenu(false));
    $("mobileMenuBackdrop")?.addEventListener("click", () => toggleMenu(false));
    document.addEventListener("keydown", (event) => {
      if (event.key === "/" && !/INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "")) {
        event.preventDefault();
        $("globalSearchInput")?.focus();
      }
    });
  }

  function routeSearch(raw) {
    const query = String(raw || "").trim();
    if (!query) return;
    if (/^\d+$/.test(query)) {
      location.href = `explorer-blocks.html?height=${encodeURIComponent(query)}`;
      return;
    }
    location.href = `explorer-search.html?q=${encodeURIComponent(query)}`;
  }

  const pageDefinitions = {
    addresses: {
      eyebrow: "Accounts",
      title: "Addresses",
      description: "Inspect wallet balances, confirmed transaction history, and token holdings.",
      features: ["Wallet details", "Balance", "Transaction history", "Token holdings"],
    },
    governance: {
      eyebrow: "Protocol",
      title: "Governance",
      description: "Follow proposals, outcomes, voting statistics, and participation across the authority set.",
      features: ["Governance overview", "Proposal statistics", "Voting statistics", "Active proposals", "Passed proposals", "Rejected proposals", "Validator votes", "Proposal votes", "Vote participation"],
    },
    treasury: {
      eyebrow: "Governance",
      title: "Treasury",
      description: "Track treasury balance, governed transactions, and spending history.",
      features: ["Treasury balance", "Treasury transactions", "Spending history"],
    },
    council: {
      eyebrow: "Governance",
      title: "Council",
      description: "Inspect council membership, authority status, and governance council votes.",
      features: ["Council members", "Authority status", "Governance council votes"],
    },
    analytics: {
      eyebrow: "Insights",
      title: "Analytics",
      description: "Measure network growth, usage, economic activity, and operational trends.",
      features: ["Network analytics", "Growth metrics", "User statistics", "Economic charts"],
    },
    network: {
      eyebrow: "Infrastructure",
      title: "Network",
      description: "Monitor peer health, topology, consensus status, and observed network latency.",
      features: ["Peer health", "Node map", "Consensus status", "Network latency"],
    },
    snapshots: {
      eyebrow: "Storage",
      title: "Snapshots",
      description: "Inspect available state snapshots, retention history, and download endpoints.",
      features: ["Available snapshots", "Snapshot history", "Download center"],
    },
    api: {
      eyebrow: "Developers",
      title: "API",
      description: "Explore public REST and RPC endpoints, SDK integration, and gateway rate limits.",
      features: ["REST API docs", "RPC endpoints", "SDKs", "Rate limits"],
    },
    charts: {
      eyebrow: "Insights",
      title: "Charts",
      description: "Compare throughput, block timing, address activity, volume, and validator performance.",
      features: ["TPS chart", "Block time chart", "Active addresses", "Volume chart", "Validator performance charts"],
    },
    staking: {
      eyebrow: "Economics",
      title: "Staking",
      description: "Review validator stake, delegations, reward policy, and estimated APR inputs.",
      features: ["Validators", "Delegations", "Rewards", "APR"],
    },
    tokenomics: {
      eyebrow: "Economics",
      title: "Tokenomics",
      description: "Inspect MSC supply, circulation, emissions, reward allocation, and burn policy.",
      features: ["Supply", "Circulating supply", "Emissions", "Burn statistics"],
    },
    "rich-list": {
      eyebrow: "Economics",
      title: "Rich List",
      description: "Analyze publicly known allocation holders and MSC distribution concentration.",
      features: ["Top holders", "Distribution analysis"],
    },
    mempool: {
      eyebrow: "Activity",
      title: "Mempool",
      description: "Monitor pending transactions, queue pressure, and observed fee statistics.",
      features: ["Pending transactions", "Queue size", "Gas statistics"],
    },
    epochs: {
      eyebrow: "Consensus",
      title: "Epochs",
      description: "Track stable validator epochs, membership boundaries, and per-block proposer rotation.",
      features: ["Current epoch", "Active committee", "Online now", "Next set boundary"],
    },
    bridge: {
      eyebrow: "Interoperability",
      title: "Bridge",
      description: "Inspect bridge safety configuration, registered assets, and cross-chain activity.",
      features: ["Bridge transfers", "Bridge status", "Cross-chain history"],
    },
    security: {
      eyebrow: "Assurance",
      title: "Security",
      description: "Review runtime invariants, validator slashing signals, and incident reports.",
      features: ["Security status", "Validator slashing", "Incident reports"],
    },
    search: {
      eyebrow: "Discovery",
      title: "Universal Search",
      description: "Search blocks, transactions, addresses, validators, and governance proposals.",
      features: ["Block", "Transaction", "Address", "Validator", "Proposal"],
    },
    settings: {
      eyebrow: "Preferences",
      title: "Settings",
      description: "Configure explorer theme, language, RPC selection, and notifications.",
      features: ["Theme", "Language", "RPC selection", "Notifications"],
    },
  };

  function pageTabs(features) {
    return `<nav class="page-tabs" aria-label="Page sections">${features.map((feature, index) =>
      `<a href="#section-${index + 1}">${esc(feature)}</a>`).join("")}</nav>`;
  }

  function generatedSearch(page) {
    if (page === "addresses") {
      return `<form id="addressLookupForm" class="search-panel"><input id="addressQuery" type="search" placeholder="Enter MSC wallet address" autocomplete="off" /><button class="button primary" type="submit"><i data-lucide="search"></i>Inspect address</button></form>`;
    }
    if (page === "search") {
      return `<form id="universalSearchForm" class="search-panel"><input id="universalSearchQuery" type="search" placeholder="Block, transaction, address, validator, or proposal" autocomplete="off" /><button class="button primary" type="submit"><i data-lucide="search"></i>Search chain</button></form>`;
    }
    return "";
  }

  function generatedSettings() {
    return `<section class="settings-grid">
      <article class="setting-panel" id="section-1"><div><strong>Theme</strong><span>Choose the explorer appearance.</span></div><div class="segmented-control" id="themeControl"><button type="button" data-theme="dark">Dark</button><button type="button" data-theme="contrast">Contrast</button><button type="button" data-theme="system">System</button></div></article>
      <article class="setting-panel" id="section-2"><div><strong>Language</strong><span>Set interface language preference.</span></div><select id="languageSelect"><option value="en">English</option><option value="hi">Hindi</option><option value="ur">Urdu</option></select></article>
      <article class="setting-panel" id="section-3"><div><strong>RPC selection</strong><span>Select the explorer data source.</span></div><select id="rpcSelect"><option value="same-origin">Explorer gateway</option><option value="https://wallet.mscblockexplorer.in">Wallet public RPC</option></select></article>
      <article class="setting-panel" id="section-4"><div><strong>Notifications</strong><span>Notify on halted consensus or unhealthy RPC status.</span></div><label id="notificationToggleControl" class="toggle"><input id="notificationToggle" type="checkbox" /><span></span></label></article>
    </section>
    <section class="panel"><div class="panel-head"><h2>Current preferences</h2><span class="table-meta">Saved in this browser</span></div><div id="settingsSummary" class="detail-grid"></div></section>`;
  }

  function renderGeneratedPage() {
    const definition = pageDefinitions[PAGE];
    const content = document.querySelector(".explorer-content");
    const hasPrerenderFallback =
      content?.children.length === 1 &&
      content.firstElementChild?.classList.contains("seo-prerender");
    if (!definition || !content || (content.children.length && !hasPrerenderFallback)) return;
    document.title = `MSC Explorer | ${definition.title}`;
    content.innerHTML = `
      <section class="page-heading"><div><div class="eyebrow">${esc(definition.eyebrow)}</div><h1>${esc(definition.title)}</h1><p>${esc(definition.description)}</p></div><div class="heading-actions"><span class="badge good">Live explorer</span><span id="pageUpdated" class="badge">Updating</span></div></section>
      ${pageTabs(definition.features)}
      ${generatedSearch(PAGE)}
      ${PAGE === "settings" ? generatedSettings() : `
        <section class="metric-grid" id="extendedMetrics">
          ${definition.features.slice(0, 4).map((feature, index) => `<article class="metric" id="section-${index + 1}"><div class="metric-top"><span>${esc(feature)}</span><span class="metric-icon"><i data-lucide="${["activity", "chart-no-axes-column-increasing", "circle-gauge", "database"][index]}"></i></span></div><div id="extendedMetric${index}" class="metric-value">-</div><div id="extendedMetricFoot${index}" class="metric-foot">Live network data</div></article>`).join("")}
        </section>
        <section class="chart-grid compact"><div id="extendedChartPrimary" class="chart-card wide"></div><div id="extendedChartSecondary" class="chart-card"></div></section>
        <section class="workspace"><div class="panel"><div class="panel-head"><h2 id="extendedPrimaryTitle">${esc(definition.features[0])}</h2><span class="table-meta">Live data</span></div><div id="extendedPrimary" class="extended-list"><div class="empty-state">Loading ${esc(definition.title.toLowerCase())}...</div></div></div><aside class="panel"><div class="panel-head"><h2 id="extendedSideTitle">${esc(definition.features[1] || "Summary")}</h2><span class="badge">Current</span></div><div id="extendedSide" class="side-stack"></div></aside></section>
        <section class="panel"><div class="panel-head"><h2 id="extendedSecondaryTitle">${esc(definition.features.slice(2).join(" / ") || "Details")}</h2><span class="table-meta">Canonical explorer view</span></div><div id="extendedSecondary" class="detail-grid"></div></section>
      `}`;
  }

  function renderCommon() {
    const status = state.status || {};
    const audit = state.tokenomicsAudit || state.tokenomics?.audit || {};
    const displayHeight = state.realtime.connected && state.realtime.displayHeight
      ? state.realtime.displayHeight
      : status.height;
    setText("topHeight", fmt(displayHeight));
    setText("topFinalized", fmt(status.finalized_height));
    setText("topMode", status.consensus_detector_mode || status.quorum_policy_mode || "-");
    setText("sideNetwork", status.network_health || "unknown");
    setText("metricHeight", fmt(displayHeight));
    setText("metricFinalized", fmt(status.finalized_height));
    setText("metricPeers", fmt(status.peers));
    setText("metricValidators", fmt(status.committee_size ?? status.live_validators ?? status.active_ready_count));
    renderLiveBlockAge();
    setText("metricMode", status.consensus_detector_mode || status.quorum_policy_mode || "-");
    setText("metricMempool", fmt(status.mempool_depth));
    const recentBlocks = state.blocks || [];
    setText("metricTPS", recentBlocks.length ? (sum(recentBlocks, (block) => block.tx_count) / recentBlocks.length).toFixed(2) : "-");
    setText("metricNode", status.node_id || "-");
    setText("networkHealth", status.network_health || "-");
    setText("networkSummary", status.network_health_summary || "-");
    setText("chainId", status.chain_id || "-");
    setText("nodeRole", status.role || "-");
    setText("syncState", status.sync_complete ? "Complete" : status.sync_mode || "-");
    setText("quorum", `${status.active_ready_count ?? "-"} / ${status.required_quorum ?? "-"}`);
    setText("lastUpdated", state.lastUpdated ? new Date(state.lastUpdated).toLocaleTimeString() : "-");
    const auditAlert = $("supplyAuditAlert");
    if (auditAlert) {
      const failed = audit.invariant_ok === false || String(audit.invariant_status || "").toLowerCase() === "failed";
      auditAlert.hidden = !failed;
      auditAlert.textContent = failed
        ? `Supply Audit Failed: current ${fmt(audit.current_supply)} / max ${fmt(audit.max_supply)}`
        : "";
    }
    maybeNotifyStatus(status);
  }

  function maybeNotifyStatus(status) {
    if (localStorage.getItem("msc_explorer_notifications") !== "true" || !("Notification" in window) || Notification.permission !== "granted") return;
    const mode = String(status.consensus_detector_mode || status.network_health || "").toUpperCase();
    if (!/HALTED|STALLED|DOWN|UNHEALTHY/.test(mode)) return;
    const key = `${mode}:${status.height || 0}`;
    const previous = localStorage.getItem("msc_explorer_last_notification") || "";
    if (previous === key) return;
    localStorage.setItem("msc_explorer_last_notification", key);
    new Notification("MSC Explorer network alert", { body: `${mode} at height ${fmt(status.height)}` });
  }

  function blocksRows(blocks) {
    const status = state.status || {};
    return (blocks || []).map((block) => `
      <tr class="clickable" data-height="${esc(block.height)}">
        <td><a class="height-link" href="explorer-blocks.html?height=${encodeURIComponent(block.height)}">#${fmt(block.height)}</a></td>
        <td>${blockTypeHTML(block)}</td>
        <td>${proposerHTML(block.proposer, { compact: true })}</td>
        <td class="mono">${fmt(block.tx_count)}</td>
        <td class="mono">${fmt(block.execution_result_count)}</td>
        <td>${quorumBreakdownHTML(block, status)}</td>
        <td><a class="hash-link" href="explorer-blocks.html?hash=${encodeURIComponent(block.hash || "")}">${esc(short(block.hash, 9, 6))}</a></td>
      </tr>`).join("") || `<tr><td colspan="7">No blocks available</td></tr>`;
  }

  function renderBlocks() {
    const blocks = state.blocks || [];
    setHTML("overviewBlocks", blocksRows(blocks.slice(0, 8)));
    setHTML("blocksTable", blocksRows(blocks));
    setText("blocksCount", `${fmt(blocks.length)} recent blocks`);
    const heights = blocks.map((block) => Number(block.height || 0)).filter(Boolean);
    setText("blocksRange", heights.length ? `#${fmt(Math.min(...heights))} - #${fmt(Math.max(...heights))}` : "-");
    setHTML("activityVisual", blocks.slice(0, 28).reverse().map((block, index) => {
      const size = 24 + Math.min(70, Number(block.execution_result_count || 0) * 10 + Number(block.tx_count || 0) * 14 + (index % 5) * 5);
      return `<span class="activity-bar" style="--bar-height:${size}%;" title="Block ${esc(block.height)}"></span>`;
    }).join(""));
  }

  function renderOverviewSide() {
    const status = state.status || {};
    const publicNodes = state.publicNodes || {};
    const rows = [
      ["Network health", status.network_health || "-"],
      ["Node / role", `${status.node_id || "-"} / ${status.role || "-"}`],
      ["Sync", status.sync_complete ? "Complete" : status.sync_mode || "-"],
      ["Quorum", `Votes: ${fmt(status.network_quorum_votes ?? status.active_ready_count)} Required: ${fmt(status.network_quorum_required ?? status.required_quorum)} Committee: ${fmt(status.committee_size ?? status.live_validators)}`],
      ["Validator set", `${fmt(status.committee_size)} active / ${fmt(status.live_validators ?? status.active_ready_count)} online`],
      ["Next set boundary", status.validator_set_next_epoch_height ? `#${fmt(status.validator_set_next_epoch_height)}` : "-"],
      ["Public RPCs", `${publicNodes.healthy ?? "-"} / ${publicNodes.total ?? "-"} healthy`],
      ["Chain ID", status.chain_id || "-"],
    ];
    setHTML("overviewSummary", rows.map(([label, value]) => `<div class="summary-row"><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`).join(""));
  }

  function renderExplorerCharts() {
    const status = state.status || {};
    const blocks = state.blocks || [];
    const recent = blocks.slice(0, 32);
    const chronological = recent.slice().reverse();
    const txDensity = chronological.map((block) => Number(block.tx_count || 0) + Number(block.execution_result_count || 0) * 1.25);
    const execDensity = chronological.map((block) => Number(block.execution_result_count || 0));
    const proposerCounts = new Map();
    let unknownProposerBlocks = 0;
    for (const block of recent) {
      const proposer = proposerLabel(block.proposer);
      if (!proposer) {
        unknownProposerBlocks += 1;
        continue;
      }
      proposerCounts.set(proposer, (proposerCounts.get(proposer) || 0) + 1);
    }
    const knownProposerCount = proposerCounts.size;
    const proposerRows = [...proposerCounts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, unknownProposerBlocks > 0 ? 5 : 6)
      .map(([label, value]) => ({ label: `Validator ${label}`, value, display: value }));
    if (unknownProposerBlocks > 0) {
      proposerRows.push({
        label: "Unknown proposer",
        value: unknownProposerBlocks,
        display: `${fmt(unknownProposerBlocks)} block${unknownProposerBlocks === 1 ? "" : "s"}`,
        title: UNKNOWN_PROPOSER_TITLE,
        tone: "warn",
      });
    }
    const quorumRows = recent.slice(0, 8).map((block) => {
      const info = blockQuorumInfo(block, status);
      const votes = Number(info.votes || 0);
      const required = Number(info.required || 0);
      const value = pct(votes, Math.max(required, votes, 1));
      return {
        label: `#${fmt(block.height)}`,
        value,
        display: quorumBreakdownText(block, status),
        title: "Votes collected / required quorum / committee size",
        tone: votes >= required ? "good" : "warn",
      };
    });
    const height = Number(status.height || 0);
    const finalized = Number(status.finalized_height || 0);
    const finalityLag = Math.max(0, height - finalized);
    const finalityScore = height > 0 ? clamp(100 - finalityLag * 10) : 0;

    chartCard(
      "activityVisual",
      "Block activity",
      "Recent transaction and execution density",
      `${lineHTML(txDensity)}${matrixHTML([
        ["Window", `${fmt(recent.length)} blocks`],
        ["Tx seen", fmt(recent.reduce((sum, block) => sum + Number(block.tx_count || 0), 0))],
        ["Exec results", fmt(recent.reduce((sum, block) => sum + Number(block.execution_result_count || 0), 0))],
        ["Latest", height ? `#${fmt(height)}` : "-"],
      ].map(([label, value]) => ({ label, value })))}`,
      "live",
    );
    chartCard("proposerChart", "Proposer mix", "Recent block production share", rowsHTML(proposerRows), `${fmt(knownProposerCount)} validators`);
    chartCard("quorumChart", "Quorum strength", "Votes collected against required quorum", rowsHTML(quorumRows), `${fmt(status.network_quorum_required ?? status.required_quorum)} required`);
    chartCard("finalityChart", "Finality", "Head to finalized gap", donutHTML(finalityScore, "Finality freshness", `${fmt(finalityLag)} block lag`, finalityLag <= 1 ? "good" : finalityLag <= 3 ? "warn" : "bad"));

    chartCard(
      "blockProductionChart",
      "Production timeline",
      "Latest blocks by execution density",
      `${barsHTML(chronological.map((block) => ({
        label: `#${block.height}`,
        value: Number(block.execution_result_count || 0) + Number(block.tx_count || 0),
      })))}${matrixHTML([
        ["Range", recent.length ? `#${fmt(Math.min(...recent.map((block) => Number(block.height || 0))))} - #${fmt(Math.max(...recent.map((block) => Number(block.height || 0))))}` : "-"],
        ["Avg exec", recent.length ? (recent.reduce((sum, block) => sum + Number(block.execution_result_count || 0), 0) / recent.length).toFixed(1) : "-"],
        ["Avg tx", recent.length ? (recent.reduce((sum, block) => sum + Number(block.tx_count || 0), 0) / recent.length).toFixed(1) : "-"],
      ].map(([label, value]) => ({ label, value })))}`,
    );
    chartCard("blockMixChart", "Block mix", "Proposer and execution balance", rowsHTML([
      { label: "Transactions", value: recent.reduce((sum, block) => sum + Number(block.tx_count || 0), 0), display: fmt(recent.reduce((sum, block) => sum + Number(block.tx_count || 0), 0)) },
      { label: "Executions", value: recent.reduce((sum, block) => sum + Number(block.execution_result_count || 0), 0), display: fmt(recent.reduce((sum, block) => sum + Number(block.execution_result_count || 0), 0)) },
      { label: "Quorum votes", value: Number(status.network_quorum_votes ?? status.active_ready_count ?? 0), display: `${fmt(status.network_quorum_votes ?? status.active_ready_count)} votes` },
      { label: "Required", value: Number(status.network_quorum_required ?? status.required_quorum ?? 0), display: `${fmt(status.network_quorum_required ?? status.required_quorum)} required` },
    ]));
    chartCard("txFlowChart", "Transaction flow", "Confirmed tx count across recent blocks", `${barsHTML(chronological.map((block) => ({ label: `#${block.height}`, value: Number(block.tx_count || 0) })))}${lineHTML(execDensity)}`, `${fmt(status.mempool_depth || 0)} pending`);
    chartCard("mempoolChart", "Mempool pressure", "Pending queue and latest block age", donutHTML(clamp(100 - Number(status.mempool_depth || 0) * 8), "Queue headroom", `${fmt(status.mempool_depth || 0)} pending | ${age(status.last_block_age_seconds)} age`, Number(status.mempool_depth || 0) <= 2 ? "good" : "warn"));

    const validators = state.validators || {};
    const all = validators.validators || validators.online_validators || [];
    const online = new Set(validators.online_validators || []);
    const offline = new Set(validators.offline_validators || validators.inactive_validators || []);
    const readinessTotal = all.length || Number(status.committee_size || status.total_validators || 0);
    const readiness = online.size || Number(status.active_ready_count || 0);
    chartCard("validatorQuorumDonut", "Quorum readiness", "Online validators vs active set", donutHTML(pct(readiness, readinessTotal || readiness), "Readiness", `${fmt(readiness)} / ${fmt(readinessTotal || readiness)} online`, offline.size ? "warn" : "good"));
    chartCard("validatorReadinessChart", "Validator state", "Committee participation health", rowsHTML([
      { label: "Online", value: readiness, display: fmt(readiness) },
      { label: "Offline", value: offline.size, display: fmt(offline.size), tone: offline.size ? "bad" : "good" },
      { label: "Required", value: Number(status.required_quorum || 0), display: fmt(status.required_quorum) },
      { label: "Strict", value: Number(status.strict_quorum || 0), display: fmt(status.strict_quorum) },
    ]));
    chartCard("validatorPendingChart", "Pending set", "Validator registry changes", matrixHTML([
      { label: "Pending add", value: fmt((validators.pending_add || []).length) },
      { label: "Pending remove", value: fmt((validators.pending_remove || []).length) },
      { label: "Mode", value: status.consensus_detector_mode || status.quorum_policy_mode || "-" },
      { label: "Committee", value: fmt(status.committee_size || all.length) },
    ]));

    const publicNodes = normalizeNodes(state.publicNodes);
    const nodeTotal = Number(publicNodes.total ?? publicNodes.nodes?.length ?? 0);
    const nodeHealthy = Number(publicNodes.healthy ?? (publicNodes.nodes || []).filter((node) => node.healthy || node.status_code === 200).length);
    chartCard("nodeHealthChart", "Public RPC health", "Wallet-safe public node availability", donutHTML(pct(nodeHealthy, nodeTotal), "Healthy nodes", `${fmt(nodeHealthy)} / ${fmt(nodeTotal)} available`, nodeHealthy === nodeTotal ? "good" : nodeHealthy ? "warn" : "bad"));
    chartCard("peerReputationChart", "Peer reputation", "Direct peer quality and acknowledgement", rowsHTML((state.peers || []).slice(0, 8).map((peer) => ({
      label: peer.validator_id ? `Validator ${peer.validator_id}` : short(peer.peer_id, 8, 4),
      value: clamp(Number(peer.reputation || 0) * 100),
      display: Number(peer.reputation || 0).toFixed(3),
      tone: Number(peer.reputation || 0) >= 0.9 ? "good" : "warn",
    }))), `${fmt((state.peers || []).length)} peers`);
  }

  function renderValidators() {
    const payload = state.validators || {};
    const ranked = state.leaderboard?.entries || state.leaderboard?.validators || [];
    const all = payload.validators || payload.online_validators || ranked.map((validator) => validator.validator_id || validator.id);
    const online = new Set(payload.online_validators || []);
    const offline = new Set(payload.offline_validators || payload.inactive_validators || []);
    const committeeSize = Number(state.status?.committee_size || all.length || 0);
    const onlineCount = online.size || Number(state.status?.live_validators || state.status?.active_ready_count || 0);
    const offlineCount = Math.max(offline.size, committeeSize - onlineCount, 0);
    setText("validatorTotal", fmt(committeeSize));
    setText("validatorOnline", fmt(onlineCount));
    setText("validatorOffline", fmt(offlineCount));
    setText("validatorPending", fmt((payload.pending_add || []).length + (payload.pending_remove || []).length));
    setHTML("validatorList", all.map((id) => {
      const isOnline = online.has(id);
      const profile = ranked.find((validator) => String(validator.validator_id || validator.id) === String(id)) || {};
      return `<div class="validator-row">
        <span class="validator-avatar">${esc(id)}</span>
        <span><span class="row-title">Validator ${esc(id)}</span><span class="row-subtitle">Consensus participant · Mainnet</span></span>
        <span class="badge ${isOnline || profile.online ? "good" : "bad"}">${isOnline || profile.online ? "Online" : "Offline"}</span>
      </div>`;
    }).join("") || `<div class="empty-state">Validator roster unavailable</div>`);
    const readiness = state.status?.active_ready_count || online.size || 0;
    const required = state.status?.required_quorum || 0;
    setHTML("quorumSummary", [
      ["Active ready", readiness],
      ["Required quorum", required],
      ["Strict quorum", state.status?.strict_quorum ?? "-"],
      ["Committee size", state.status?.committee_size ?? all.length],
      ["Epoch", state.status?.validator_set_epoch_enabled ? `#${fmt(state.status?.validator_set_epoch_number)}` : "Activates at protocol gate"],
      ["Next set boundary", state.status?.validator_set_next_epoch_height ? `#${fmt(state.status.validator_set_next_epoch_height)}` : "-"],
      ["Pending add", (payload.pending_add || []).length],
      ["Pending remove", (payload.pending_remove || []).length],
    ].map(([label, value]) => `<div class="summary-row"><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`).join(""));
  }

  function normalizeNodes(payload) {
    const data = unwrap(payload) || {};
    return {
      ...data,
      nodes: data.nodes || data.backends || [],
    };
  }

  function renderNodes() {
    const peers = state.peers || [];
    const publicNodes = normalizeNodes(state.publicNodes);
    setText("peerTotal", fmt(peers.length));
    setText("publicHealthy", fmt(publicNodes.healthy));
    setText("publicTotal", fmt(publicNodes.total));
    setText("activeGateway", publicNodes.best_node?.id || publicNodes.nodes?.find((node) => node.active_gateway)?.id || "-");
    setHTML("publicNodeList", (publicNodes.nodes || []).map((node) => {
      const health = node.health_state || (node.healthy ? "healthy" : "unhealthy");
      const healthTone = health === "healthy" ? "good" : health === "warning" ? "warn" : "bad";
      return `<div class="node-row">
        <span class="node-avatar">${esc(node.id || "?")}</span>
        <span><span class="row-title">${esc(node.id || "Public node")} · ${esc(node.role || "full")}</span><span class="row-subtitle">${esc(node.gateway_rpc_url || node.rpc_url || node.target || "-")}</span></span>
        <span class="badge ${healthTone}">${esc(health)}</span>
      </div>`;
    }).join("") || `<div class="empty-state">Public node registry unavailable</div>`);
    setHTML("peersTable", peers.map((peer) => `
      <tr>
        <td class="mono">${esc(short(peer.peer_id, 10, 8))}</td>
        <td><span class="badge ${peer.connected ? "good" : "bad"}">${peer.connected ? "Connected" : "Offline"}</span></td>
        <td class="mono">${esc(peer.validator_id || "-")}</td>
        <td>${esc(peer.role || "-")}</td>
        <td class="mono">${Number(peer.reputation || 0).toFixed(3)}</td>
        <td class="mono">${fmt(peer.ack_height)}</td>
        <td><span class="badge ${peer.hash_match && peer.hello_ok ? "good" : "warn"}">${peer.hash_match && peer.hello_ok ? "Verified" : "Check"}</span></td>
      </tr>`).join("") || `<tr><td colspan="7">No peers available</td></tr>`);
  }

  function detailCards(items) {
    return items.map(([label, value]) => `<div class="detail-card"><span class="label">${esc(label)}</span><strong>${esc(value ?? "-")}</strong></div>`).join("");
  }

  function firstPresent(...values) {
    return values.find((value) => value !== undefined && value !== null && value !== "");
  }

  function snapshotChunkCount(snapshot) {
    const manifest = snapshot?.manifest || {};
    return firstPresent(
      snapshot?.chunk_count,
      snapshot?.chunkCount,
      snapshot?.total_chunks,
      snapshot?.totalChunks,
      Array.isArray(snapshot?.chunk_hashes) ? snapshot.chunk_hashes.length : undefined,
      Array.isArray(snapshot?.chunks) ? snapshot.chunks.length : undefined,
      manifest.chunk_count,
      manifest.chunkCount,
      manifest.total_chunks,
      manifest.totalChunks,
      Array.isArray(manifest.chunk_hashes) ? manifest.chunk_hashes.length : undefined,
      Array.isArray(manifest.chunks) ? manifest.chunks.length : undefined,
    );
  }

  async function loadSnapshotMetadata() {
    const latest = await first(["/snapshot/latest", "/v1/snapshot/latest"]);
    if (PAGE !== "snapshots" || !latest?.height) return latest;
    try {
      const manifest = await first([
        `/v1/snapshot/manifest?height=${encodeURIComponent(latest.height)}`,
        `/snapshot/manifest?height=${encodeURIComponent(latest.height)}`,
        "/v1/snapshot/manifest",
        "/snapshot/manifest",
      ]);
      return { ...latest, ...manifest, manifest: manifest.manifest || latest.manifest };
    } catch (_) {
      return latest;
    }
  }

  function listRows(items) {
    return items.map((item) => `<div class="data-row">
      <span class="data-row-main"><strong>${esc(item.title || "-")}</strong><small>${esc(item.subtitle || "")}</small></span>
      <span class="data-row-value ${item.tone ? `tone-${item.tone}` : ""}">${esc(item.value ?? "-")}</span>
    </div>`).join("") || `<div class="empty-state">No published data available</div>`;
  }

  function setExtendedMetrics(items) {
    for (let index = 0; index < 4; index += 1) {
      const item = items[index] || {};
      setText(`extendedMetric${index}`, item.value ?? "-");
      setText(`extendedMetricFoot${index}`, item.foot || "Live network data");
    }
  }

  function renderRecentTransactions() {
    const txs = state.recentTxs || [];
    setHTML("latestTransactionsTable", txs.slice(0, 20).map((entry) => {
      const tx = entry.tx || entry.transaction || entry;
      return `<tr><td class="mono"><a class="hash-link" href="explorer-transactions.html?q=${encodeURIComponent(tx.id || tx.tx_id || "")}">${esc(short(tx.id || tx.tx_id, 10, 7))}</a></td><td>${esc(tx.type || tx.tx_type || "transfer")}</td><td class="mono">${esc(short(tx.from || tx.sender, 8, 5))}</td><td class="mono">${esc(short(tx.to || tx.recipient, 8, 5))}</td><td class="mono">${esc(tx.amount ?? "-")}</td><td class="mono">#${fmt(entry.height || tx.height)}</td></tr>`;
    }).join("") || `<tr><td colspan="6">No recent transactions in the current block window</td></tr>`);
  }

  function renderExtendedPage() {
    if (!pageDefinitions[PAGE] || PAGE === "settings") return;
    const status = state.status || {};
    const blocks = state.blocks || [];
    const leaderboard = state.leaderboard?.entries || state.leaderboard?.validators || [];
    const publicStatus = state.publicStatus || {};
    const publicNodes = normalizeNodes(state.publicNodes);
    const governance = state.governance || {};
    const proposals = Object.values(governance.proposals || {});
    const tokenomics = normalizeTokenomics(state.tokenomics || {});
    const tokenomicsAudit = state.tokenomicsAudit || tokenomics.audit || {};
    const buckets = tokenomics.buckets || [];
    const storage = state.storage || {};
    const snapshot = state.snapshot || {};
    const bridge = state.bridge || {};
    const security = state.security || {};
    const incidents = state.misbehavior?.items || [];
    const recent = blocks.slice(0, 32);
    const chronological = recent.slice().reverse();
    const avgBlockTx = recent.length ? sum(recent, (block) => block.tx_count) / recent.length : 0;
    const avgExec = recent.length ? sum(recent, (block) => block.execution_result_count) / recent.length : 0;
    const onlineValidators = leaderboard.filter((validator) => validator.online);
    const totalStake = sum(leaderboard, (validator) => validator.effective_stake || validator.actual_stake);
    const set = (metrics, primaryTitle, primary, sideTitle, side, secondaryTitle, secondary) => {
      setExtendedMetrics(metrics);
      setText("extendedPrimaryTitle", primaryTitle);
      setHTML("extendedPrimary", primary);
      setText("extendedSideTitle", sideTitle);
      setHTML("extendedSide", side);
      setText("extendedSecondaryTitle", secondaryTitle);
      setHTML("extendedSecondary", secondary);
      setText("pageUpdated", state.lastUpdated ? new Date(state.lastUpdated).toLocaleTimeString() : "Unavailable");
    };

    if (PAGE === "addresses") {
      set([{ value: "-", foot: "Search a wallet address" }, { value: "-", foot: "Confirmed balance" }, { value: "-", foot: "Confirmed activity" }, { value: "-", foot: "Indexed assets" }],
        "Wallet details", `<div class="empty-state">Enter an MSC address to inspect wallet details.</div>`,
        "Address activity", listRows((state.recentTxs || []).slice(0, 5).map((item) => ({ title: short(item.tx?.from || item.from, 10, 6), subtitle: `Block #${fmt(item.height)}`, value: item.tx?.amount ?? item.amount ?? "-" }))),
        "Token holdings", detailCards((state.coins?.coins || []).map((coin) => [coin.symbol, `${coin.decimals} decimals`])));
      chartCard("extendedChartPrimary", "Address activity", "Recent observed transfers", barsHTML(chronological.map((block) => ({ label: `#${block.height}`, value: block.tx_count || 0 }))));
      chartCard("extendedChartSecondary", "Search readiness", "Indexer and public RPC availability", donutHTML(publicStatus.indexer?.some?.((node) => node.healthy) ? 100 : 30, "Address lookup", publicStatus.indexer?.some?.((node) => node.healthy) ? "Indexer online" : "RPC fallback"));
    } else if (PAGE === "governance") {
      const active = proposals.filter((proposal) => String(proposal.status || "").toLowerCase() === "active");
      const passed = proposals.filter((proposal) => /approved|passed|applied/.test(String(proposal.status || "").toLowerCase()));
      const rejected = proposals.filter((proposal) => /rejected|failed/.test(String(proposal.status || "").toLowerCase()));
      const votes = proposals.flatMap((proposal) => Object.entries(proposal.votes || {}).map(([voter, choice]) => ({ voter, choice, proposal: proposal.id || proposal.title })));
      set([{ value: fmt(proposals.length), foot: "All proposals" }, { value: fmt(active.length), foot: "Active proposals" }, { value: fmt(votes.length), foot: "Recorded votes" }, { value: `${fmt(passed.length)} / ${fmt(rejected.length)}`, foot: "Passed / rejected" }],
        "Proposals", listRows(proposals.map((proposal) => ({ title: proposal.title || proposal.id || "Proposal", subtitle: proposal.kind || "governance", value: proposal.status || "unknown", tone: /approved|passed|applied/.test(String(proposal.status || "").toLowerCase()) ? "good" : /rejected|failed/.test(String(proposal.status || "").toLowerCase()) ? "bad" : "warn" }))),
        "Governance status", detailCards([["State hash", short(governance.state_hash, 12, 8)], ["Treasury balance", fmt(governance.treasury_balance)], ["Emergency pause", governance.emergency_pause?.active ? "Active" : "Inactive"], ["Protocol gates", fmt(Object.keys(governance.protocol_gates || {}).length)]]),
        "Validator and proposal votes", listRows(votes.map((vote) => ({ title: vote.voter, subtitle: vote.proposal, value: vote.choice }))));
      chartCard("extendedChartPrimary", "Proposal outcomes", "Active, passed, and rejected proposals", rowsHTML([{ label: "Active", value: active.length, display: active.length }, { label: "Passed", value: passed.length, display: passed.length }, { label: "Rejected", value: rejected.length, display: rejected.length, tone: rejected.length ? "bad" : "good" }]));
      chartCard("extendedChartSecondary", "Vote participation", "Authority votes recorded", donutHTML(pct(votes.length, Math.max(proposals.length * Number(status.committee_size || 1), 1)), "Participation", `${fmt(votes.length)} votes`));
    } else if (PAGE === "treasury") {
      const treasuryBuckets = buckets.filter((bucket) => /treasury|foundation|community/i.test(bucket.name || bucket.address || ""));
      set([{ value: fmt(governance.treasury_balance), foot: "Governance treasury" }, { value: fmt(sum(treasuryBuckets, (bucket) => bucket.balance)), foot: "Known governed pools" }, { value: fmt(proposals.filter((p) => p.kind === "treasury").length), foot: "Treasury proposals" }, { value: tokenomics.economic_policy?.treasury?.allow_treasury_ops ? "Enabled" : "Locked", foot: "Direct operations" }],
        "Treasury balances", listRows(treasuryBuckets.map((bucket) => ({ title: bucket.name, subtitle: bucket.address, value: fmt(bucket.balance) }))),
        "Treasury policy", detailCards([["Fees to treasury", tokenomics.economic_policy?.treasury?.transaction_fees_to_treasury ? "Yes" : "No"], ["Admin required", tokenomics.economic_policy?.treasury?.treasury_ops_require_admin ? "Yes" : "No"], ["Treasury address", tokenomics.economic_policy?.treasury?.treasury_address || "-"], ["Owner address", tokenomics.economic_policy?.treasury?.owner_address || "-"]]),
        "Spending history", listRows(proposals.filter((p) => p.kind === "treasury").map((p) => ({ title: p.title || p.id, subtitle: p.treasury_recipient || "-", value: fmt(p.treasury_amount) }))));
      chartCard("extendedChartPrimary", "Governed pool balances", "Known treasury and foundation allocations", rowsHTML(treasuryBuckets.map((bucket) => ({ label: bucket.name, value: bucket.balance, display: fmt(bucket.balance) }))));
      chartCard("extendedChartSecondary", "Treasury controls", "Direct operations safety", donutHTML(tokenomics.economic_policy?.treasury?.allow_treasury_ops ? 100 : 0, "Operations", tokenomics.economic_policy?.treasury?.allow_treasury_ops ? "Enabled" : "Governance locked", tokenomics.economic_policy?.treasury?.allow_treasury_ops ? "warn" : "good"));
    } else if (PAGE === "council") {
      const members = leaderboard.filter((validator) => validator.active);
      set([{ value: fmt(members.length), foot: "Active authority members" }, { value: fmt(onlineValidators.length), foot: "Online members" }, { value: fmt(status.required_quorum), foot: "Required quorum" }, { value: governance.emergency_pause?.active ? "Paused" : "Active", foot: "Authority status" }],
        "Council members", listRows(members.map((member) => ({ title: `Validator ${member.validator_id || member.id}`, subtitle: `${member.country || "-"} · ${member.slot_type || "authority"}`, value: member.online ? "Online" : "Offline", tone: member.online ? "good" : "bad" }))),
        "Authority status", detailCards([["Consensus mode", status.consensus_detector_mode || "-"], ["Ready", fmt(status.active_ready_count)], ["Required quorum", fmt(status.required_quorum)], ["Strict quorum", fmt(status.strict_quorum)]]),
        "Governance council votes", listRows(proposals.flatMap((proposal) => Object.entries(proposal.votes || {}).map(([voter, choice]) => ({ title: voter, subtitle: proposal.title || proposal.id, value: choice })))));
      chartCard("extendedChartPrimary", "Council availability", "Active member uptime", rowsHTML(members.map((member) => ({ label: member.validator_id || member.id, value: member.signed_ratio_bps || 0, display: `${Number(member.signed_ratio_bps || 0) / 100}%`, tone: member.online ? "good" : "bad" }))));
      chartCard("extendedChartSecondary", "Authority quorum", "Ready members against strict quorum", donutHTML(pct(status.active_ready_count, status.strict_quorum || status.committee_size), "Ready authority", `${fmt(status.active_ready_count)} ready`));
    } else if (PAGE === "analytics" || PAGE === "charts") {
      set([{ value: avgBlockTx.toFixed(2), foot: "Average tx per recent block" }, { value: avgExec.toFixed(2), foot: "Average executions" }, { value: fmt(new Set((state.recentTxs || []).flatMap((item) => [item.tx?.from, item.tx?.to]).filter(Boolean)).size), foot: "Observed active addresses" }, { value: fmt(totalStake), foot: "Validator voting power" }],
        "Network analytics", listRows(recent.slice(0, 12).map((block) => ({ title: `Block #${fmt(block.height)}`, subtitle: proposerLabel(block.proposer) || UNKNOWN_PROPOSER_TEXT, value: `${fmt(block.tx_count)} tx / ${fmt(block.execution_result_count)} exec` }))),
        "Growth metrics", detailCards([["Current height", fmt(status.height)], ["Finalized height", fmt(status.finalized_height)], ["Validators", fmt(leaderboard.length)], ["Public RPC nodes", fmt(publicNodes.total)]]),
        "Economic and validator charts", detailCards([["Supply", fmt(tokenomics.total_supply)], ["Max supply", fmt(tokenomics.max_supply)], ["Known holder buckets", fmt(buckets.length)], ["Online validators", fmt(onlineValidators.length)]]));
      chartCard("extendedChartPrimary", PAGE === "charts" ? "TPS and volume chart" : "Network growth", "Recent block transaction and execution density", `${barsHTML(chronological.map((block) => ({ label: `#${block.height}`, value: block.tx_count || 0 })))}${lineHTML(chronological.map((block) => Number(block.execution_result_count || 0)))}`);
      chartCard("extendedChartSecondary", "Validator performance", "Signed participation across validators", rowsHTML(leaderboard.map((v) => ({ label: v.validator_id || v.id, value: v.signed_ratio_bps || 0, display: `${Number(v.signed_ratio_bps || 0) / 100}%`, tone: v.online ? "good" : "bad" }))));
    } else if (PAGE === "network") {
      const nodeList = publicNodes.nodes || [];
      set([{ value: fmt(state.peers.length), foot: "Connected peers" }, { value: fmt(publicNodes.healthy), foot: "Healthy public RPCs" }, { value: status.consensus_detector_mode || "-", foot: "Consensus status" }, { value: `${fmt(nodeList.length ? sum(nodeList, (node) => node.latency_ms) / nodeList.length : 0)}ms`, foot: "Average RPC latency" }],
        "Peer health", listRows(state.peers.map((peer) => ({ title: peer.validator_id || short(peer.peer_id, 10, 5), subtitle: peer.role || "peer", value: peer.connected ? "Connected" : "Offline", tone: peer.connected ? "good" : "bad" }))),
        "Consensus status", detailCards([["Mode", status.consensus_detector_mode || "-"], ["Reason", status.consensus_detector_reason || "-"], ["Finality lag", fmt(status.consensus_detector_finality_lag_blocks)], ["Partition risk", status.consensus_detector_partition_risk ? "Detected" : "Clear"]]),
        "Node map and latency", listRows(nodeList.map((node) => ({ title: node.id || "RPC node", subtitle: `${node.role || "full"} · ${node.gateway_rpc_url || node.rpc_url || "-"}`, value: `${fmt(node.latency_ms)}ms`, tone: node.healthy ? "good" : "bad" }))));
      chartCard("extendedChartPrimary", "Network latency", "Observed public RPC response times", barsHTML(nodeList.map((node) => ({ label: node.id || "node", value: node.latency_ms || 0 }))));
      chartCard("extendedChartSecondary", "Peer health", "Connected peer reputation", rowsHTML(state.peers.map((peer) => ({ label: peer.validator_id || short(peer.peer_id), value: Number(peer.reputation || 0) * 100, display: Number(peer.reputation || 0).toFixed(3) }))));
    } else if (PAGE === "snapshots") {
      const chunkCount = snapshotChunkCount(snapshot);
      set([{ value: snapshot.height ? `#${fmt(snapshot.height)}` : "-", foot: "Latest available snapshot" }, { value: fmt(storage.validator_snapshot_keep_last), foot: "Validator snapshots retained" }, { value: storage.profile || "-", foot: "Storage profile" }, { value: storage.cold_export_enabled ? "Enabled" : "Disabled", foot: "Cold export" }],
        "Available snapshots", listRows(snapshot.height ? [{ title: `Snapshot #${fmt(snapshot.height)}`, subtitle: short(snapshot.snapshot_hash, 12, 8), value: snapshot.source || "committed", tone: "good" }] : []),
        "Retention policy", detailCards([["Hourly retain", fmt(storage.hourly_snapshot_retain)], ["Daily retain", fmt(storage.daily_snapshot_retain)], ["Weekly retain", fmt(storage.weekly_snapshot_retain)], ["Monthly retain", fmt(storage.monthly_snapshot_retain)]]),
        "Download center", detailCards([["Snapshot manifest", snapshot.height ? `/v1/snapshot/manifest?height=${snapshot.height}` : "Unavailable"], ["Latest metadata", "/v1/snapshot/latest"], ["Compression", snapshot.compression || snapshot.manifest?.compression || storage.cold_export_compression || "-"], ["Chunk count", fmt(chunkCount)]]));
      chartCard("extendedChartPrimary", "Snapshot retention history", "Configured retention windows", barsHTML([["Hourly", storage.hourly_snapshot_retain], ["Daily", storage.daily_snapshot_retain], ["Weekly", storage.weekly_snapshot_retain], ["Monthly", storage.monthly_snapshot_retain]].map(([label, value]) => ({ label, value }))));
      chartCard("extendedChartSecondary", "Storage mode", "Hot and cold history policy", donutHTML(storage.archive_mode ? 100 : pct(storage.hot_window_blocks, storage.finalized_height), "Hot history", storage.retention_summary || "-"));
    } else if (PAGE === "api") {
      const endpoints = ["/status", "/explorer/blocks", "/explorer/block?height=1", "/explorer/tx?tx_id=...", "/balance?address=...", "/validators", "/governance/status", "/tokenomics", "/tokenomics/audit", "/bridge/status", "/snapshot/latest"];
      set([{ value: fmt(endpoints.length), foot: "Documented REST endpoints" }, { value: "JSON-RPC", foot: "/rpc and /v1/rpc" }, { value: "JavaScript", foot: "Browser fetch compatible" }, { value: "Gateway", foot: "Public read rate limits" }],
        "REST API docs", listRows(endpoints.map((endpoint) => ({ title: endpoint, subtitle: "GET · JSON response", value: "Public" }))),
        "RPC endpoints", detailCards([["JSON-RPC", "/rpc"], ["Versioned RPC", "/v1/rpc"], ["Events", "/wallet/events"], ["Indexer", "/indexer/* when configured"]]),
        "SDKs and rate limits", detailCards([["JavaScript", "fetch / WebSocket"], ["Go", "net/http / JSON-RPC"], ["Read policy", "Gateway rate limited"], ["Write policy", "Separate strict limit"]]));
      chartCard("extendedChartPrimary", "Public API surface", "Endpoint groups available through the explorer gateway", rowsHTML([{ label: "Chain", value: 6, display: "6" }, { label: "Governance", value: 5, display: "5" }, { label: "Light client", value: 4, display: "4" }, { label: "Snapshots", value: 6, display: "6" }]));
      chartCard("extendedChartSecondary", "RPC availability", "Active gateway health", donutHTML(publicNodes.healthy ? 100 : 0, "Public RPC", `${fmt(publicNodes.healthy)} healthy`, publicNodes.healthy ? "good" : "bad"));
    } else if (PAGE === "staking") {
      const policy = tokenomics.economic_policy?.staking || {};
      const rewards = tokenomics.economic_policy?.rewards || {};
      set([{ value: fmt(leaderboard.length), foot: "Active validators" }, { value: fmt(totalStake), foot: "Effective validator stake" }, { value: fmt(policy.validator_min_stake), foot: "Minimum validator stake" }, { value: rewards.work_block_base_reward ? `${fmt(rewards.work_block_base_reward)} MSC` : "-", foot: "Base work reward" }],
        "Validator staking", listRows(leaderboard.map((validator) => ({ title: `Validator ${validator.validator_id || validator.id}`, subtitle: validator.slot_type || "validator", value: `${fmt(validator.effective_stake || validator.actual_stake)} MSC`, tone: validator.online ? "good" : "warn" }))),
        "Staking policy", detailCards([["Delegations", policy.one_wallet_one_validator ? "One wallet / validator" : "Supported"], ["Lock epochs", fmt(policy.default_lock_epochs)], ["Minimum lock", fmt(policy.min_lock_epochs)], ["Rejoin restake", policy.rejoin_requires_restake ? "Required" : "Not required"]]),
        "Rewards and APR inputs", detailCards([["Base reward", fmt(rewards.work_block_base_reward)], ["Treasury split", `${tokenomics.economic_policy?.rewards?.unified_treasury_bps / 100 || 0}%`], ["Validator split", `${tokenomics.economic_policy?.rewards?.unified_validator_bps / 100 || 0}%`], ["APR", "Variable by participation and emissions"]]));
      chartCard("extendedChartPrimary", "Validator voting power", "Effective stake by active validator", rowsHTML(leaderboard.map((validator) => ({ label: validator.validator_id || validator.id, value: validator.effective_stake || 0, display: fmt(validator.effective_stake) }))));
      chartCard("extendedChartSecondary", "Online stake", "Participation-weighted availability", donutHTML(pct(sum(onlineValidators, (v) => v.effective_stake), totalStake), "Online voting power", `${fmt(sum(onlineValidators, (v) => v.effective_stake))} MSC`));
    } else if (PAGE === "tokenomics" || PAGE === "rich-list") {
      const maxSupply = Number(tokenomicsAudit.max_supply ?? tokenomics.max_supply ?? 0);
      const totalSupply = Number(tokenomicsAudit.current_supply ?? tokenomics.total_supply ?? 0);
      const circulating = Number(tokenomicsAudit.circulating ?? sum(buckets.filter((bucket) => !/future rewards/i.test(bucket.name)), (bucket) => bucket.balance));
      const burn = tokenomics.economic_policy?.inflation?.burn_bps || 0;
      set([{ value: fmt(totalSupply), foot: "Current supply" }, { value: fmt(circulating), foot: "Circulating supply" }, { value: fmt(maxSupply), foot: "Max supply" }, { value: tokenomicsAudit.invariant_ok === false ? "Check" : "OK", foot: "Runtime audit" }],
        PAGE === "rich-list" ? "Top holders" : "Supply allocation", listRows(buckets.slice().sort((a, b) => Number(b.balance) - Number(a.balance)).map((bucket, index) => ({ title: `${index + 1}. ${bucket.name}`, subtitle: bucket.address, value: `${fmt(bucket.balance)} MSC` }))),
        "Supply policy", detailCards([["Symbol", tokenomics.symbol || "MSC"], ["Decimals", fmt(tokenomics.decimals)], ["Genesis supply", fmt(tokenomicsAudit.genesis_supply)], ["Burn floor", fmt(tokenomics.supply_burn_floor)], ["Remaining mintable", fmt(tokenomicsAudit.remaining_mintable)]]),
        PAGE === "rich-list" ? "Distribution analysis" : "Runtime supply audit", detailCards([["Minted", fmt(tokenomicsAudit.minted)], ["Burned", fmt(tokenomicsAudit.burned)], ["Treasury", fmt(tokenomicsAudit.treasury)], ["Foundation", fmt(tokenomicsAudit.foundation)], ["Community", fmt(tokenomicsAudit.community)], ["Ecosystem", fmt(tokenomicsAudit.ecosystem)], ["Validator locked", fmt(tokenomicsAudit.validator_locked)], ["Last audit height", fmt(tokenomicsAudit.last_audit_height)], ["Invariant status", tokenomicsAudit.invariant_status || (tokenomicsAudit.invariant_ok === false ? "failed" : "ok")], ["Burn BPS", fmt(burn)]]));
      chartCard("extendedChartPrimary", "MSC distribution", "Known genesis and policy allocation buckets", rowsHTML(buckets.map((bucket) => ({ label: bucket.name, value: bucket.balance, display: `${bucket.percent || 0}%` }))));
      chartCard("extendedChartSecondary", "Supply cap utilization", "Reported supply against fixed cap", donutHTML(pct(totalSupply, maxSupply), "Supply utilization", `${fmt(totalSupply)} / ${fmt(maxSupply)}`, totalSupply <= maxSupply ? "good" : "warn"));
    } else if (PAGE === "mempool") {
      const fees = (state.recentTxs || []).map((item) => Number(item.tx?.fee ?? item.fee ?? 0));
      set([{ value: fmt(status.mempool_depth), foot: "Pending transactions" }, { value: status.tx_lane_status || "-", foot: "Queue status" }, { value: status.tx_lane_reason || "-", foot: "Queue reason" }, { value: fees.length ? (fees.reduce((a, b) => a + b, 0) / fees.length).toFixed(2) : "-", foot: "Observed average fee" }],
        "Pending transactions", `<div class="empty-state">${Number(status.mempool_depth || 0) ? "Pending transaction details are private to the node mempool." : "Mempool is currently clear."}</div>`,
        "Queue size", detailCards([["Depth", fmt(status.mempool_depth)], ["Lane status", status.tx_lane_status || "-"], ["Lane reason", status.tx_lane_reason || "-"], ["Gossip active", status.tx_gossip_active ? "Yes" : "No"]]),
        "Gas statistics", detailCards([["Observed fee samples", fmt(fees.length)], ["Average fee", fees.length ? (fees.reduce((a, b) => a + b, 0) / fees.length).toFixed(2) : "-"], ["Maximum fee", fees.length ? Math.max(...fees) : "-"], ["Pending nonce API", "/nonce/pending"]]));
      chartCard("extendedChartPrimary", "Recent queue pressure", "Mempool depth and block transaction throughput", barsHTML(chronological.map((block) => ({ label: `#${block.height}`, value: block.tx_count || 0 }))));
      chartCard("extendedChartSecondary", "Queue headroom", "Pending depth against a nominal 100 transaction window", donutHTML(clamp(100 - Number(status.mempool_depth || 0)), "Available capacity", `${fmt(status.mempool_depth)} pending`));
    } else if (PAGE === "epochs") {
      const epochLength = Number(status.validator_set_epoch_length_blocks || status.validator_pool_epoch_blocks || 10000);
      const height = Number(status.height || 0);
      const currentEpoch = Number(status.validator_set_epoch_number || (height ? Math.floor((height - 1) / epochLength) + 1 : 0));
      const epochStart = Number(status.validator_set_epoch_start_height || (currentEpoch ? (currentEpoch - 1) * epochLength + 1 : 0));
      const epochEnd = Number(status.validator_set_epoch_end_height || (epochStart ? epochStart + epochLength - 1 : 0));
      const progress = epochStart && height >= epochStart ? Math.min(epochLength, height - epochStart + 1) : 0;
      const epochEnabled = status.validator_set_epoch_enabled === true;
      const committeeSize = Number(status.committee_size || 0);
      const onlineCount = Number(status.live_validators ?? status.active_ready_count ?? 0);
      set([{ value: epochEnabled ? fmt(currentEpoch) : "Pending", foot: epochEnabled ? "Current validator epoch" : `Activates at #${fmt(status.validator_set_epoch_v1_height)}` }, { value: fmt(committeeSize), foot: "Active committee" }, { value: fmt(onlineCount), foot: "Online now" }, { value: `#${fmt(status.validator_set_next_epoch_height)}`, foot: "Next set boundary" }],
        "Current epoch", listRows([{ title: epochEnabled ? `Epoch ${fmt(currentEpoch)}` : "Epoch-frozen membership pending", subtitle: epochEnabled ? `Blocks #${fmt(epochStart)} - #${fmt(epochEnd)}` : `Protocol activation #${fmt(status.validator_set_epoch_v1_height)}`, value: epochEnabled ? `${fmt(progress)} / ${fmt(epochLength)}` : `${fmt(status.validator_set_epoch_blocks_remaining)} blocks` }]),
        "Validator membership", detailCards([["Active committee", fmt(committeeSize)], ["Online now", fmt(onlineCount)], ["Registered", fmt(status.validator_registered_count)], ["Maximum active", fmt(status.validator_set_max_active)], ["Set mode", epochEnabled ? "Frozen within epoch" : "Legacy until activation"], ["Proposer", "Changes every block"]]),
        "Epoch history", detailCards(Array.from({ length: 8 }, (_, index) => { const epoch = Math.max(1, currentEpoch - index); const start = (epoch - 1) * epochLength + 1; return [`Epoch ${fmt(epoch)}`, `#${fmt(start)} - #${fmt(start + epochLength - 1)}`]; })));
      chartCard("extendedChartPrimary", "Epoch progress", "Validator membership changes only at the next boundary", donutHTML(epochEnabled ? pct(progress, epochLength) : pct(height, Number(status.validator_set_epoch_v1_height || 1)), epochEnabled ? "Epoch progress" : "Activation progress", epochEnabled ? `${fmt(progress)} / ${fmt(epochLength)} blocks` : `Gate #${fmt(status.validator_set_epoch_v1_height)}`));
      chartCard("extendedChartSecondary", "Committee readiness", "Stable active set versus current liveness", rowsHTML([{ label: "Active committee", value: committeeSize, display: fmt(committeeSize) }, { label: "Online now", value: onlineCount, display: fmt(onlineCount), tone: onlineCount >= Number(status.required_quorum || 0) ? "good" : "bad" }, { label: "Required quorum", value: Number(status.required_quorum || 0), display: fmt(status.required_quorum) }]));
    } else if (PAGE === "bridge") {
      set([{ value: bridge.enabled ? "Enabled" : "Disabled", foot: "Bridge status" }, { value: bridge.mode || "-", foot: "Execution mode" }, { value: fmt(bridge.required_confirmations), foot: "Required confirmations" }, { value: fmt(bridge.oracle_quorum), foot: "Oracle quorum" }],
        "Bridge transfers", `<div class="empty-state">No public bridge transfer history has been published.</div>`,
        "Bridge status", detailCards([["Protocol", bridge.version || "-"], ["Light client required", bridge.light_client_required ? "Yes" : "No"], ["IBC style", bridge.ibc_style_enabled ? "Enabled" : "Disabled"], ["Registered assets", fmt((bridge.registered_assets || []).length)]]),
        "Cross-chain history and safety", listRows((bridge.safety || []).map((item) => ({ title: item, subtitle: "Bridge safety invariant", value: "Enforced", tone: "good" }))));
      chartCard("extendedChartPrimary", "Bridge readiness", "Configured chains and assets", rowsHTML([{ label: "Chains", value: (bridge.registered_chains || []).length, display: fmt((bridge.registered_chains || []).length) }, { label: "Assets", value: (bridge.registered_assets || []).length, display: fmt((bridge.registered_assets || []).length) }, { label: "Confirmations", value: bridge.required_confirmations || 0, display: fmt(bridge.required_confirmations) }]));
      chartCard("extendedChartSecondary", "Bridge execution", "Current safety mode", donutHTML(bridge.enabled ? 100 : 0, "Execution", bridge.enabled ? bridge.mode : "Disabled by policy", bridge.enabled ? "warn" : "good"));
    } else if (PAGE === "security") {
      const invariants = security.runtime_invariants || [];
      const failed = invariants.filter((item) => !item.passed);
      set([{ value: security.healthy ? "Healthy" : "Attention", foot: "Security status" }, { value: fmt(invariants.length), foot: "Runtime invariants" }, { value: fmt(failed.length), foot: "Failed invariants" }, { value: fmt(state.misbehavior?.events_total), foot: "Recorded validator incidents" }],
        "Security status", listRows(invariants.map((item) => ({ title: item.id, subtitle: item.evidence || item.severity, value: item.status, tone: item.passed ? "good" : "bad" }))),
        "Formal verification", detailCards([["Scope", security.scope || "-"], ["Machine checked", security.machine_checked ? "Yes" : "No"], ["External proof", security.external_proof_status || "-"], ["Consensus mode", security.detector_mode || "-"]]),
        "Validator slashing and incidents", listRows(incidents.map((item) => ({ title: `Validator ${item.validator}`, subtitle: `${item.last_reason} · block #${fmt(item.last_height)}`, value: `${fmt(item.count)} events`, tone: "bad" }))));
      chartCard("extendedChartPrimary", "Runtime invariants", "Machine-checked security conditions", rowsHTML(invariants.map((item) => ({ label: item.id, value: item.passed ? 100 : 0, display: item.passed ? "pass" : "fail", tone: item.passed ? "good" : "bad" }))));
      chartCard("extendedChartSecondary", "Security health", "Passing runtime invariants", donutHTML(pct(invariants.length - failed.length, invariants.length), "Invariant health", `${fmt(invariants.length - failed.length)} / ${fmt(invariants.length)} pass`, failed.length ? "bad" : "good"));
    } else if (PAGE === "search") {
      set([{ value: "Block", foot: "Height or hash" }, { value: "Transaction", foot: "Transaction ID" }, { value: "Address", foot: "MSC wallet" }, { value: "Validator / proposal", foot: "Authority and governance" }],
        "Search results", `<div id="universalSearchResults" class="empty-state">Enter a query to search the chain.</div>`,
        "Search sources", detailCards([["Indexer", publicStatus.indexer?.some?.((node) => node.healthy) ? "Online" : "Fallback"], ["Blocks", "/explorer/block"], ["Transactions", "/explorer/tx"], ["Governance", "/governance/status"]]),
        "Search tips", detailCards([["Block", "Enter a numeric height or block hash"], ["Transaction", "Enter the full transaction ID"], ["Address", "Enter an MSC-prefixed wallet address"], ["Validator / proposal", "Enter an exact or partial identifier"]]));
      chartCard("extendedChartPrimary", "Universal search coverage", "Explorer entity types", rowsHTML([{ label: "Blocks", value: 100, display: "Live" }, { label: "Transactions", value: 100, display: "Live" }, { label: "Addresses", value: 70, display: "RPC / indexer" }, { label: "Validators", value: 100, display: "Live" }, { label: "Proposals", value: 100, display: "Live" }]));
      chartCard("extendedChartSecondary", "Indexer status", "Historical search availability", donutHTML(publicStatus.indexer?.some?.((node) => node.healthy) ? 100 : 30, "Search index", publicStatus.indexer?.some?.((node) => node.healthy) ? "Online" : "RPC fallback"));
    }
    window.lucide?.createIcons();
  }

  function blockDetailHTML(block) {
    const summary = block.summary || block;
    const detailQuorumBlock = {
      ...block,
      ...summary,
      signature_count: summary.signature_count ?? block.signature_count,
      signatures: summary.signatures ?? block.signatures,
      committee_size: summary.committee_size ?? block.committee_size,
    };
    const rows = [
      ["Height", summary.height || block.height],
      ["Hash", summary.hash || block.hash],
      ["Previous hash", summary.prev_hash || block.prev_hash],
      ["Proposer", proposerHTML(summary.proposer ?? block.proposer), true],
      ["Type", blockTypeHTML({ ...block, ...summary }), true],
      ["Consensus mode", summary.consensus_mode || block.consensus_mode],
      ["Transactions", summary.tx_count ?? block.tx_count ?? (block.transactions || []).length],
      ["Execution results", summary.execution_result_count ?? block.execution_result_count ?? (block.execution_results || []).length],
      ["Required quorum", summary.required_quorum ?? block.required_quorum],
      ["Quorum", quorumBreakdownHTML(detailQuorumBlock, state.status || {}), true],
      ["State root", summary.state_root || block.state_root],
      ["Validator set hash", summary.validator_set_hash || block.validator_set_hash],
      ["Registry hash", summary.validator_registry_hash || block.validator_registry_hash],
    ];
    return rows.map(([label, value, html]) => `<div class="detail-card"><span class="label">${esc(label)}</span><strong>${html ? value : esc(value ?? "-")}</strong></div>`).join("");
  }

  async function loadBlock(query) {
    const raw = String(query || "").trim();
    if (!raw) return;
    setHTML("blockDetailGrid", `<div class="empty-state">Loading block...</div>`);
    try {
      const key = /^\d+$/.test(raw) ? `height=${encodeURIComponent(raw)}` : `hash=${encodeURIComponent(raw)}`;
      const block = await first([
        `/archive-rpc/explorer/block?${key}`,
        `/explorer/block?${key}`,
        `/indexer/block?${key}`,
      ]);
      setHTML("blockDetailGrid", blockDetailHTML(block));
      setText("blockDetailMeta", `Block #${fmt(block.height || block.summary?.height)}`);
      setText("blockRaw", JSON.stringify(block, null, 2));
    } catch (error) {
      setHTML("blockDetailGrid", `<div class="empty-state">Block lookup failed: ${esc(error.message || error)}</div>`);
      setText("blockRaw", "");
    }
  }

  function txSummaryHTML(data, query) {
    if (Array.isArray(data)) {
      return data.map((tx) => txSummaryHTML(tx, query)).join("");
    }
    const tx = data.transaction || data.tx || data;
    const rows = [
      ["Transaction ID", tx.id || tx.tx_id || query],
      ["Status", data.state || data.status || tx.status || "Found"],
      ["From", tx.from || tx.sender || "-"],
      ["To", tx.to || tx.recipient || "-"],
      ["Amount", tx.amount ?? "-"],
      ["Coin", tx.coin || tx.denom || "MSC"],
      ["Fee", tx.fee ?? "-"],
      ["Height", data.height || tx.height || "-"],
      ["Nonce", tx.nonce ?? "-"],
      ["Type", tx.type || tx.tx_type || "-"],
    ];
    return rows.map(([label, value]) => `<div class="detail-card"><span class="label">${esc(label)}</span><strong>${esc(value)}</strong></div>`).join("");
  }

  async function loadTransaction(query) {
    const raw = String(query || "").trim();
    if (!raw) return;
    setHTML("txDetailGrid", `<div class="empty-state">Searching chain activity...</div>`);
    try {
      let result;
      if (/^MSC/i.test(raw)) {
        result = await first([`/txs?address=${encodeURIComponent(raw)}`, `/v1/txs?address=${encodeURIComponent(raw)}`]);
      } else {
        result = await first([
          `/indexer/tx?tx_id=${encodeURIComponent(raw)}`,
          `/archive-rpc/explorer/tx?tx_id=${encodeURIComponent(raw)}`,
          `/v1/tx/${encodeURIComponent(raw)}`,
          `/explorer/tx?tx_id=${encodeURIComponent(raw)}`,
        ]);
      }
      const normalized = result.transactions || result.txs || result;
      setHTML("txDetailGrid", txSummaryHTML(normalized, raw));
      setText("txDetailMeta", /^MSC/i.test(raw) ? `Address ${short(raw)}` : `Transaction ${short(raw)}`);
      setText("txRaw", JSON.stringify(result, null, 2));
    } catch (error) {
      try {
        await loadBlock(raw);
        location.href = `explorer-blocks.html?hash=${encodeURIComponent(raw)}`;
      } catch (_) {
        setHTML("txDetailGrid", `<div class="empty-state">No transaction or address activity found: ${esc(error.message || error)}</div>`);
        setText("txRaw", "");
      }
    }
  }

  async function loadAddress(query) {
    const address = String(query || "").trim();
    if (!address) return;
    setHTML("extendedPrimary", `<div class="empty-state">Loading address...</div>`);
    const [balanceResult, historyResult, indexedResult] = await Promise.allSettled([
      first([`/v1/balance?address=${encodeURIComponent(address)}`, `/balance?address=${encodeURIComponent(address)}`]),
      first([`/txs?address=${encodeURIComponent(address)}`, `/indexer/address?address=${encodeURIComponent(address)}&limit=50`]),
      first([`/indexer/address?address=${encodeURIComponent(address)}&limit=50`]),
    ]);
    const balance = balanceResult.status === "fulfilled" ? balanceResult.value : {};
    const historyData = historyResult.status === "fulfilled" ? historyResult.value : {};
    const indexed = indexedResult.status === "fulfilled" ? indexedResult.value : {};
    const txs = historyData.transactions || historyData.txs || indexed.txs || [];
    setExtendedMetrics([
      { value: short(address, 10, 7), foot: "Wallet address" },
      { value: fmt(balance.balance ?? balance.amount), foot: `${balance.coin || "MSC"} balance` },
      { value: fmt(txs.length), foot: "Confirmed history entries" },
      { value: fmt((state.coins?.coins || []).length), foot: "Known chain assets" },
    ]);
    setText("extendedPrimaryTitle", "Wallet details");
    setHTML("extendedPrimary", detailCards([["Address", address], ["Balance", `${fmt(balance.balance ?? balance.amount)} ${balance.coin || "MSC"}`], ["Nonce", fmt(balance.nonce)], ["Indexed history", fmt(txs.length)]]));
    setText("extendedSideTitle", "Token holdings");
    setHTML("extendedSide", detailCards((state.coins?.coins || []).map((coin) => [coin.symbol, coin.symbol === (balance.coin || "MSC") ? fmt(balance.balance ?? balance.amount) : "Not indexed"])));
    setText("extendedSecondaryTitle", "Transaction history");
    setHTML("extendedSecondary", txs.length ? txSummaryHTML(txs, address) : `<div class="empty-state">No confirmed address activity found.</div>`);
    history.replaceState(null, "", `?address=${encodeURIComponent(address)}`);
  }

  async function universalSearch(query) {
    const raw = String(query || "").trim();
    if (!raw) return;
    const target = $("universalSearchResults");
    if (target) target.className = "extended-list";
    setHTML("universalSearchResults", `<div class="empty-state">Searching chain...</div>`);
    if (/^\d+$/.test(raw)) {
      location.href = `explorer-blocks.html?height=${encodeURIComponent(raw)}`;
      return;
    }
    if (/^MSC/i.test(raw) || /^0x[a-f0-9]{40}$/i.test(raw)) {
      location.href = `explorer-addresses.html?address=${encodeURIComponent(raw)}`;
      return;
    }
    try {
      const result = await first([`/indexer/search?q=${encodeURIComponent(raw)}`]);
      const type = result.type || "result";
      const value = result.result || result;
      setHTML("universalSearchResults", listRows([{ title: `${type[0]?.toUpperCase() || ""}${type.slice(1)} found`, subtitle: raw, value: "Open", tone: "good" }]) + `<pre class="json-view compact">${esc(JSON.stringify(value, null, 2))}</pre>`);
      return;
    } catch (_) {
      // Continue through public RPC and local governance/validator data.
    }
    const validator = (state.leaderboard?.entries || []).find((item) => String(item.validator_id || item.id || "").toLowerCase().includes(raw.toLowerCase()));
    if (validator) {
      setHTML("universalSearchResults", detailCards([["Type", "Validator"], ["Validator", validator.validator_id || validator.id], ["Status", validator.online ? "Online" : "Offline"], ["Voting power", fmt(validator.effective_stake)]]));
      return;
    }
    const proposal = Object.values(state.governance?.proposals || {}).find((item) => JSON.stringify(item).toLowerCase().includes(raw.toLowerCase()));
    if (proposal) {
      setHTML("universalSearchResults", detailCards([["Type", "Proposal"], ["Proposal", proposal.title || proposal.id], ["Status", proposal.status], ["Kind", proposal.kind]]));
      return;
    }
    try {
      const tx = await first([`/explorer/tx?tx_id=${encodeURIComponent(raw)}`, `/v1/tx/${encodeURIComponent(raw)}`]);
      setHTML("universalSearchResults", txSummaryHTML(tx, raw));
      return;
    } catch (_) {
      // Try block hash last.
    }
    try {
      const block = await first([`/explorer/block?hash=${encodeURIComponent(raw)}`, `/archive-rpc/explorer/block?hash=${encodeURIComponent(raw)}`]);
      setHTML("universalSearchResults", blockDetailHTML(block));
    } catch (_) {
      setHTML("universalSearchResults", `<div class="empty-state">No block, transaction, address, validator, or proposal matched this query.</div>`);
    }
  }

  function applyTheme(theme) {
    const selected = ["dark", "contrast", "system"].includes(theme) ? theme : "dark";
    document.documentElement.dataset.theme = selected;
    localStorage.setItem("msc_explorer_theme", selected);
    document.querySelectorAll("button[data-theme]").forEach((button) => button.classList.toggle("active", button.dataset.theme === selected));
  }

  function renderSettings() {
    if (PAGE !== "settings") return;
    const theme = localStorage.getItem("msc_explorer_theme") || "dark";
    const language = localStorage.getItem("msc_explorer_language") || "en";
    const rpc = localStorage.getItem("msc_explorer_rpc") || "same-origin";
    const notifications = localStorage.getItem("msc_explorer_notifications") === "true";
    applyTheme(theme);
    document.documentElement.lang = language;
    if ($("languageSelect")) $("languageSelect").value = language;
    if ($("rpcSelect")) $("rpcSelect").value = rpc;
    if ($("notificationToggle")) $("notificationToggle").checked = notifications;
    setHTML("settingsSummary", detailCards([["Theme", theme], ["Language", language], ["RPC", configuredBase()], ["Notifications", notifications ? "Enabled" : "Disabled"]]));
    setText("pageUpdated", new Date().toLocaleTimeString());
  }

  function bindSettings() {
    document.querySelectorAll("[data-theme]").forEach((button) => button.addEventListener("click", () => {
      applyTheme(button.dataset.theme);
      renderSettings();
    }));
    $("languageSelect")?.addEventListener("change", (event) => {
      localStorage.setItem("msc_explorer_language", event.target.value);
      renderSettings();
    });
    $("rpcSelect")?.addEventListener("change", (event) => {
      localStorage.setItem("msc_explorer_rpc", event.target.value);
      renderSettings();
      refresh(true).catch(() => {});
    });
    $("notificationToggleControl")?.addEventListener("click", (event) => {
      event.preventDefault();
      const input = $("notificationToggle");
      if (!input) return;
      input.checked = !input.checked;
      input.dispatchEvent(new Event("change", { bubbles: true }));
    });
    $("notificationToggle")?.addEventListener("change", (event) => {
      localStorage.setItem("msc_explorer_notifications", String(event.target.checked));
      if (event.target.checked && "Notification" in window && Notification.permission === "default") {
        Notification.requestPermission().catch(() => {});
      }
      renderSettings();
    });
  }

  function bindPage() {
    $("blockLookupForm")?.addEventListener("submit", (event) => {
      event.preventDefault();
      const query = $("blockQuery")?.value || "";
      const key = /^\d+$/.test(query.trim()) ? "height" : "hash";
      history.replaceState(null, "", `?${key}=${encodeURIComponent(query.trim())}`);
      loadBlock(query);
    });
    $("txLookupForm")?.addEventListener("submit", (event) => {
      event.preventDefault();
      const query = $("txQuery")?.value || "";
      history.replaceState(null, "", `?q=${encodeURIComponent(query.trim())}`);
      loadTransaction(query);
    });
    $("addressLookupForm")?.addEventListener("submit", (event) => {
      event.preventDefault();
      loadAddress($("addressQuery")?.value || "");
    });
    $("universalSearchForm")?.addEventListener("submit", (event) => {
      event.preventDefault();
      const query = $("universalSearchQuery")?.value || "";
      history.replaceState(null, "", `?q=${encodeURIComponent(query.trim())}`);
      universalSearch(query);
    });
    bindSettings();
    document.addEventListener("click", (event) => {
      const row = event.target.closest("tr[data-height]");
      if (row && PAGE === "blocks") loadBlock(row.dataset.height);
    });
  }

  async function loadCore() {
    const [status, blocks, validators, peers, publicNodes, publicStatus, leaderboard, governance, tokenomics, tokenomicsAudit, storage, snapshot, bridge, security, misbehavior, coins] = await Promise.allSettled([
      first(["/status", "/v1/status"]),
      first(["/archive-rpc/explorer/blocks?limit=40", "/explorer/blocks?limit=40", "/indexer/blocks?limit=40", "/v1/blocks?limit=40"]),
      first(["/validators", "/v1/validators"]),
      first(["/explorer/peers", "/v1/peers"]),
      first(["/gateway/lb-status.json", "/v1/public-nodes", "/public-nodes"]),
      first(["/public/status", "/v1/public/status"]),
      first(["/validators/leaderboard", "/v1/validators/leaderboard"]),
      first(["/governance/status", "/v1/governance/status"]),
      first(["/tokenomics"]),
      first(["/tokenomics/audit", "/v1/tokenomics/audit"]),
      first(["/storage/policy", "/v1/storage/policy"]),
      loadSnapshotMetadata(),
      first(["/bridge/status", "/v1/bridge/status"]),
      first(["/formal/verification", "/v1/formal/verification"]),
      first(["/misbehavior", "/v1/misbehavior"]),
      first(["/coins"]),
    ]);
    if (status.status === "fulfilled") state.status = status.value;
    if (blocks.status === "fulfilled") {
      const incoming = (blocks.value.blocks || []).map((block) => normalizeBlockPayload(block));
      if (state.realtime.connected && state.realtime.displayHeight) {
        const visible = incoming.filter((block) => Number(block.height || 0) <= state.realtime.displayHeight);
        state.blocks = [...visible, ...(state.blocks || [])]
          .filter((block, index, all) => all.findIndex((candidate) => Number(candidate.height) === Number(block.height)) === index)
          .sort((a, b) => Number(b.height || 0) - Number(a.height || 0))
          .slice(0, 40);
      } else {
        state.blocks = incoming;
      }
    }
    if (validators.status === "fulfilled") state.validators = validators.value;
    if (peers.status === "fulfilled") state.peers = peers.value.peers || [];
    if (publicNodes.status === "fulfilled") state.publicNodes = normalizeNodes(publicNodes.value);
    if (publicStatus.status === "fulfilled") state.publicStatus = publicStatus.value;
    if (leaderboard.status === "fulfilled") state.leaderboard = leaderboard.value;
    if (governance.status === "fulfilled") state.governance = governance.value;
    if (tokenomics.status === "fulfilled") state.tokenomics = tokenomics.value;
    if (tokenomicsAudit.status === "fulfilled") state.tokenomicsAudit = tokenomicsAudit.value;
    if (storage.status === "fulfilled") state.storage = storage.value;
    if (snapshot.status === "fulfilled") state.snapshot = snapshot.value;
    if (bridge.status === "fulfilled") state.bridge = bridge.value;
    if (security.status === "fulfilled") state.security = security.value;
    if (misbehavior.status === "fulfilled") state.misbehavior = misbehavior.value;
    if (coins.status === "fulfilled") state.coins = coins.value;
    const headHeight = Number(state.status?.height || 0);
    if (headHeight && state.realtime.connected && state.realtime.displayHeight && headHeight > state.realtime.displayHeight) {
      enqueueRealtimeBlocks({
        type: "new_block",
        height: headHeight,
        finalized_height: state.status?.finalized_height,
        mode: state.status?.consensus_detector_mode || state.status?.quorum_policy_mode,
        last_block_age_seconds: state.status?.last_block_age_seconds || 0,
        network_health: state.status?.network_health,
      });
    }
    if (headHeight && (!state.realtime.connected || !state.realtime.displayHeight)) {
      const hasHeadBlock = state.blocks.some((block) => Number(block.height || 0) === headHeight);
      if (!hasHeadBlock) {
        try {
          const head = await first([
            `/archive-rpc/explorer/block?height=${encodeURIComponent(headHeight)}`,
            `/explorer/block?height=${encodeURIComponent(headHeight)}`,
            `/indexer/block?height=${encodeURIComponent(headHeight)}`,
          ]);
          mergeBlock(normalizeBlockPayload(head, { height: headHeight }));
        } catch (_) {
          // The recent-block list remains the fallback when exact head lookup is unavailable.
        }
      }
      state.realtime.displayHeight = headHeight;
    }
    if (state.realtime.lastBlockAgeBaseSeconds === null || !state.realtime.connected) {
      setBlockAgeBase(state.status?.last_block_age_seconds || 0);
    }
    const needsRecentTransactions = ["overview", "transactions", "addresses", "analytics", "charts", "mempool"].includes(PAGE);
    if (needsRecentTransactions && state.blocks.length) {
      void hydrateRecentTransactions(state.blocks.slice(0, 6));
    }
    state.lastUpdated = Date.now();
  }

  async function hydrateRecentTransactions(blocks) {
    const requestID = ++state.recentTxRequest;
    const details = await Promise.allSettled(blocks.map((block) => first([
      `/archive-rpc/explorer/block?height=${encodeURIComponent(block.height)}`,
      `/explorer/block?height=${encodeURIComponent(block.height)}`,
      `/indexer/block?height=${encodeURIComponent(block.height)}`,
    ])));
    if (requestID !== state.recentTxRequest) return;
    state.recentTxs = details.flatMap((result) => {
      if (result.status !== "fulfilled") return [];
      const block = result.value || {};
      return (block.transactions || block.txs || []).map((tx) => ({ height: block.height || block.summary?.height, tx }));
    });
    renderRecentTransactions();
    if (["overview", "analytics", "charts"].includes(PAGE)) renderExplorerCharts();
  }

  async function refresh(force = false) {
    const button = $("refreshExplorer");
    button?.setAttribute("disabled", "disabled");
    try {
      await loadCore();
      renderCommon();
      renderBlocks();
      renderOverviewSide();
      renderValidators();
      renderNodes();
      renderExplorerCharts();
      renderRecentTransactions();
      renderExtendedPage();
      renderSettings();
      if (force) window.lucide?.createIcons();
    } finally {
      button?.removeAttribute("disabled");
    }
  }

  function runInitialQuery() {
    const params = new URLSearchParams(location.search);
    if (PAGE === "blocks") {
      const query = params.get("height") || params.get("hash") || "";
      if (query) {
        setText("blockQuery", query);
        const input = $("blockQuery");
        if (input) input.value = query;
        loadBlock(query);
      }
    }
    if (PAGE === "transactions") {
      const query = params.get("q") || params.get("tx") || params.get("address") || "";
      if (query) {
        const input = $("txQuery");
        if (input) input.value = query;
        loadTransaction(query);
      }
    }
    if (PAGE === "addresses") {
      const query = params.get("address") || params.get("q") || "";
      if (query) {
        const input = $("addressQuery");
        if (input) input.value = query;
        loadAddress(query);
      }
    }
    if (PAGE === "search") {
      const query = params.get("q") || "";
      if (query) {
        const input = $("universalSearchQuery");
        if (input) input.value = query;
        universalSearch(query);
      }
    }
  }

  applyTheme(localStorage.getItem("msc_explorer_theme") || "dark");
  document.documentElement.lang = localStorage.getItem("msc_explorer_language") || "en";
  renderGeneratedPage();
  installShell();
  bindShell();
  bindPage();
  renderSettings();
  refresh().then(runInitialQuery).catch((error) => {
    setText("sideNetwork", "Unavailable");
    setHTML("overviewSummary", `<div class="empty-state">${esc(error.message || error)}</div>`);
  });
  connectExplorerRealtime(true);
  window.setInterval(renderLiveBlockAge, 1000);
  window.setInterval(() => refresh(false).catch(() => {}), 30000);
})();
