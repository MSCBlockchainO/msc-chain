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
    latestStatus: null,
    latestBlocks: null,
    txRawMode: false,
    lastTxPayload: null,
    refreshSeq: 0,
    lastAppliedSeq: 0,
    refreshInFlight: false,
    refreshQueued: false,
  };

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

  const blockAgeTone = (status) => {
    const age = asIntOrNull(status.last_block_age_seconds);
    if (age === null) return "";
    const haltedAfter = asIntOrNull(status.halted_after_seconds) || 60;
    const degradedAfter = asIntOrNull(status.degraded_after_seconds) || 12;
    if (age >= haltedAfter) return "bad";
    if (age >= degradedAfter) return "warn";
    return "ok";
  };

  const setTone = (el, tone) => {
    if (!el) return;
    el.classList.remove("ok", "warn", "bad");
    if (tone) el.classList.add(tone);
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
    const blockAgeText = blockAge === null ? "-" : fmtAge(blockAge);
    const ageTone = blockAgeTone(status);
    const expectedHash = asTextOrDash(status.validator_autoheal_expected_hash);
    const gotHash = asTextOrDash(status.validator_autoheal_got_hash);
    const mismatchHashText =
      expectedHash === "-" && gotHash === "-" && mismatchHeight === null
        ? "-"
        : `h=${mismatchHeight === null ? "-" : mismatchHeight} exp=${short(expectedHash, 6)} got=${short(gotHash, 6)}`;

    els.nodeId.textContent = status.node_id || "-";
    els.chainId.textContent = status.chain_id || "-";
    els.nodeRole.textContent = status.role || (status.is_validator ? "validator" : "full");
    els.height.textContent = String(status.height ?? "-");
    els.finalized.textContent = String(status.finalized_height ?? "-");
    if (els.lastBlockAge) {
      els.lastBlockAge.textContent = blockAgeText;
      setTone(els.lastBlockAge, ageTone);
    }
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
    if (els.topHeight) els.topHeight.textContent = String(status.height ?? "-");
    if (els.topLastBlockAge) {
      els.topLastBlockAge.textContent = blockAgeText;
      setTone(els.topLastBlockAge, ageTone);
    }
    if (els.topCmd) {
      els.topCmd.textContent = asTextOrDash(status.consensus_detector_mode);
      const cmd = String(status.consensus_detector_mode || "").toUpperCase();
      setTone(els.topCmd, cmd === "NORMAL" ? "ok" : cmd === "HALTED" || cmd === "EMERGENCY" || cmd === "ATTACK" ? "bad" : "warn");
    }
    if (els.topPeers) els.topPeers.textContent = String(status.peers ?? "-");
    if (els.topState) {
      els.topState.textContent = status.syncing ? "SYNCING" : status.ready ? "READY" : "LIVE";
      setTone(els.topState, status.syncing ? "warn" : "ok");
    }
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

  const fetchBlocksData = async () => apiV1("/v1/blocks?limit=40", "/explorer/blocks?limit=40");

  const fetchPeersData = async () => apiV1("/v1/peers", "/explorer/peers");

  const renderAllFromState = () => {
    if (state.latestStatus) renderStatus(state.latestStatus);
    if (state.latestBlocks) renderBlocks(state.latestBlocks);
    if (state.latestPeers) renderPeers(state.latestPeers);
    if (state.latestValidators && state.latestPeers) renderValidatorsDualView();
  };

  const loadBlockByHeight = async (height) => {
    const data = await api(`/explorer/block?height=${encodeURIComponent(height)}`);
    renderBlockDetail(data);
  };

  const loadBlockByHash = async (hash) => {
    const data = await api(`/explorer/block?hash=${encodeURIComponent(hash)}`);
    state.selectedBlockHeight = Number(data.height) || 0;
    renderBlockDetail(data);
  };

  const loadTx = async (txId) => {
    const encoded = encodeURIComponent(txId);
    const data = await apiV1(`/v1/tx/${encoded}`, `/explorer/tx?tx_id=${encoded}`);
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
      ];
      const results = await Promise.allSettled(tasks);
      if (seq < state.lastAppliedSeq) {
        return;
      }
      const nextStatus = results[0];
      const nextBlocks = results[1];
      const nextValidators = results[2];
      const nextPeers = results[3];
      if (nextStatus.status === "fulfilled") state.latestStatus = nextStatus.value;
      if (nextBlocks.status === "fulfilled") state.latestBlocks = nextBlocks.value;
      if (nextValidators.status === "fulfilled") state.latestValidators = nextValidators.value;
      if (nextPeers.status === "fulfilled") state.latestPeers = nextPeers.value;
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
    state.timer = setInterval(refreshAll, state.refreshMs);
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

  restartTimer();
  refreshAll();
})();
