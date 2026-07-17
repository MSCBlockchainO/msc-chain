(function () {
  "use strict";

  const MSC_PROVIDER_BRIDGE_NAMESPACE = "msc-wallet-bridge-v1";
  const WALLET_STORAGE_KEYS = ["msc_wallet_browser_v1", "msc_wallet_v1"];

  const $ = (id) => document.getElementById(id);
  function isLoopbackHost(host) {
    const h = String(host || "").trim().toLowerCase();
    return h === "localhost" || h === "::1" || /^127(?:\.\d{1,3}){1,3}$/.test(h);
  }
  function inferDefaultRPCBase() {
    try {
      const params = new URLSearchParams(window.location.search);
      const fromQuery = String(params.get("rpc") || "").trim();
      if (fromQuery) return fromQuery;
    } catch (_) {
      // Ignore query parse errors and use host heuristics.
    }
    const host = String(window.location.hostname || "");
    const localHost = isLoopbackHost(host);
    const port = String(window.location.port || "").trim();
    if (localHost && port && port !== "26657") {
      return `${window.location.protocol === "https:" ? "https" : "http"}://127.0.0.1:26657`;
    }
    return window.location.origin;
  }
  const els = {
    rpcBase: $("rpcBase"),
    apiToken: $("apiToken"),
    connState: $("connState"),
    wsConn: $("wsConn"),
    wsType: $("wsType"),
    wsTxId: $("wsTxId"),
    chainMeta: $("chainMeta"),
    walletMeta: $("walletMeta"),
    compatMeta: $("compatMeta"),
    tokenRef: $("tokenRef"),
    accountRef: $("accountRef"),
    poolRef: $("poolRef"),
    farmRef: $("farmRef"),
    seasonRef: $("seasonRef"),
    routeTokenOut: $("routeTokenOut"),
    routeAmountIn: $("routeAmountIn"),
    routeMaxHops: $("routeMaxHops"),
    leaderboardLimit: $("leaderboardLimit"),
    tokenRefList: $("tokenRefList"),
    poolRefList: $("poolRefList"),
    farmRefList: $("farmRefList"),
    syncRefsBtn: $("syncRefsBtn"),
    readOut: $("readOut"),
    dtlType: $("dtlType"),
    txFrom: $("txFrom"),
    txNonce: $("txNonce"),
    txFee: $("txFee"),
    txTtlSec: $("txTtlSec"),
    txChainId: $("txChainId"),
    txCoin: $("txCoin"),
    txTo: $("txTo"),
    txPublicKey: $("txPublicKey"),
    txSignature: $("txSignature"),
    txId: $("txId"),
    txTokenId: $("txTokenId"),
    txPayload: $("txPayload"),
    payloadTemplateBtn: $("payloadTemplateBtn"),
    txPayloadPreview: $("txPayloadPreview"),
    txGovCert: $("txGovCert"),
    txObject: $("txObject"),
    txOut: $("txOut"),
    statusTxId: $("statusTxId"),
    statusOut: $("statusOut"),
    signerState: $("signerState"),
    signerAddress: $("signerAddress"),
    signerPassword: $("signerPassword"),
    walletBridgeModal: $("walletBridgeModal"),
    walletBridgeFrame: $("walletBridgeFrame"),
    walletBridgeClose: $("walletBridgeClose"),
    walletBridgeHint: $("walletBridgeHint"),
    tokenCreateGuidedBox: $("tokenCreateGuidedBox"),
    tokenTransferGuidedBox: $("tokenTransferGuidedBox"),
    tokenApproveGuidedBox: $("tokenApproveGuidedBox"),
    tokenTransferFromGuidedBox: $("tokenTransferFromGuidedBox"),
    tokenMintGuidedBox: $("tokenMintGuidedBox"),
    tokenBurnGuidedBox: $("tokenBurnGuidedBox"),
    quickPayloadBox: $("quickPayloadBox"),
    quickPayloadFields: $("quickPayloadFields"),
    beginnerMode: $("beginnerMode"),
		txPayloadWrap: $("txPayloadWrap"),
    txGovCertWrap: $("txGovCertWrap"),
    tcName: $("tcName"),
    tcSymbol: $("tcSymbol"),
    tcDecimals: $("tcDecimals"),
    tcInitialSupply: $("tcInitialSupply"),
    tcMaxSupply: $("tcMaxSupply"),
    tcAuthorityThreshold: $("tcAuthorityThreshold"),
    tcFreezeEnabled: $("tcFreezeEnabled"),
    tcTaxBps: $("tcTaxBps"),
    tcMetadataUri: $("tcMetadataUri"),
    tcAuthoritySigners: $("tcAuthoritySigners"),
    ttTokenId: $("ttTokenId"),
    ttTo: $("ttTo"),
    ttAmount: $("ttAmount"),
    taTokenId: $("taTokenId"),
    taSpender: $("taSpender"),
    taAmount: $("taAmount"),
    tfTokenId: $("tfTokenId"),
    tfSpender: $("tfSpender"),
    tfFrom: $("tfFrom"),
    tfTo: $("tfTo"),
    tfAmount: $("tfAmount"),
    transferFromSpenderHint: $("transferFromSpenderHint"),
    useSenderAsSpenderBtn: $("useSenderAsSpenderBtn"),
    prepareApproveFromTransferBtn: $("prepareApproveFromTransferBtn"),
    tmTokenId: $("tmTokenId"),
    tmTo: $("tmTo"),
    tmAmount: $("tmAmount"),
    tmEpoch: $("tmEpoch"),
    tmAction: $("tmAction"),
    tmSigners: $("tmSigners"),
    tmSignatures: $("tmSignatures"),
    tbTokenId: $("tbTokenId"),
    tbAmount: $("tbAmount"),
		copyLastTxBtn: $("copyLastTxBtn"),
    clearDraftBtn: $("clearDraftBtn"),
  };

  const state = {
    rpcBase: normalizeRpcBase(localStorage.getItem("msc_dtl_rpc") || inferDefaultRPCBase()),
    apiToken: normalizeToken(localStorage.getItem("msc_dtl_token") || ""),
    chainId: localStorage.getItem("msc_dtl_chain") || "91938",
    walletAccount: String(localStorage.getItem("msc_dtl_wallet_account") || "").trim(),
    lastTxID: String(localStorage.getItem("msc_dtl_last_tx_id") || "").trim().toLowerCase(),
    lastBuiltTx: null,
    wallet: null,
    secretKey: null,
    bridgeTarget: null,
    bridgeMode: "iframe",
    bridgeFrameLoadedURL: "",
    bridgeOrigin: "",
    bridgeSeq: 0,
		contractRuntimeRemoved: true,
    refSuggestions: {
      tokens: [],
      pools: [],
      farms: [],
      updatedAt: 0,
    },
  };

  const enc = new TextEncoder();
  const U64_MAX = (1n << 64n) - 1n;
  const DRAFT_KEY = "msc_dtl_ide_draft_v1";
  const draftFieldIDs = [
    "rpcBase",
    "apiToken",
    "tokenRef",
    "accountRef",
    "poolRef",
    "farmRef",
    "seasonRef",
    "routeTokenOut",
    "routeAmountIn",
    "routeMaxHops",
    "leaderboardLimit",
    "beginnerMode",
    "dtlType",
    "txFrom",
    "txNonce",
    "txFee",
    "txTtlSec",
    "txChainId",
    "txCoin",
    "txTo",
    "txPublicKey",
    "txSignature",
    "txId",
    "txTokenId",
    "txPayload",
    "txGovCert",
    "statusTxId",
    "tcName",
    "tcSymbol",
    "tcDecimals",
    "tcInitialSupply",
    "tcMaxSupply",
    "tcAuthorityThreshold",
    "tcFreezeEnabled",
    "tcTaxBps",
    "tcMetadataUri",
    "tcAuthoritySigners",
    "ttTokenId",
    "ttTo",
    "ttAmount",
    "taTokenId",
    "taSpender",
    "taAmount",
    "tfTokenId",
    "tfSpender",
    "tfFrom",
    "tfTo",
    "tfAmount",
    "tmTokenId",
    "tmTo",
    "tmAmount",
    "tmEpoch",
    "tmAction",
    "tmSigners",
    "tmSignatures",
    "tbTokenId",
    "tbAmount",
  ];
  let draftSaveTimer = 0;

  function normalizeToken(raw) {
    const value = String(raw || "").trim();
    return value.replace(/^Bearer\s+/i, "").trim();
  }

  function normalizeRpcBase(raw) {
    let value = String(raw || "").trim();
    if (!value) return "";
    if (value.endsWith("/rpc")) value = value.slice(0, -4);
    return value.replace(/\/+$/, "");
  }

  function renderCompatMode() {
    if (!els.compatMeta) return;
    els.compatMeta.textContent = "Native DTL: ON · Programmable VM: PERMANENTLY REMOVED";
    els.compatMeta.classList.add("runtime-meta");
  }



  function normalizeBridgeOrigin(raw) {
    try {
      return new URL(String(raw || "").trim()).origin.toLowerCase();
    } catch (_) {
      return "";
    }
  }

  function setInputIfEmpty(inputEl, value) {
    if (!inputEl) return;
    const existing = String(inputEl.value || "").trim();
    if (existing) return;
    inputEl.value = String(value || "").trim();
  }

  function applyLaunchHintsFromQuery() {
    try {
      const params = new URLSearchParams(window.location.search);
      setInputIfEmpty(els.tokenRef, params.get("token"));
      setInputIfEmpty(els.poolRef, params.get("pool"));
      setInputIfEmpty(els.farmRef, params.get("farm"));
      setInputIfEmpty(els.accountRef, params.get("account"));
      setInputIfEmpty(els.routeTokenOut, params.get("route_out"));
      setInputIfEmpty(els.txFrom, params.get("from"));
      const dtlType = String(params.get("dtl_type") || "").trim().toUpperCase();
      if (dtlType && els.dtlType) {
        try {
          assertSupportedDTLTxType(dtlType);
          els.dtlType.value = dtlType;
        } catch (_) {
          // Ignore unsupported query value to keep page usable.
        }
      }
    } catch (_) {
      // Query parsing failure should not break IDE.
    }
  }

  function uniqueSorted(values) {
    const out = [];
    const seen = new Set();
    for (const value of values || []) {
      const normalized = String(value || "").trim();
      if (!normalized) continue;
      const key = normalized.toLowerCase();
      if (seen.has(key)) continue;
      seen.add(key);
      out.push(normalized);
    }
    out.sort((a, b) => a.localeCompare(b));
    return out;
  }

  function setDatalistOptions(listEl, values) {
    if (!listEl) return;
    listEl.textContent = "";
    const unique = uniqueSorted(values);
    for (const value of unique) {
      const option = document.createElement("option");
      option.value = value;
      listEl.appendChild(option);
    }
  }

  function applyRefSuggestionsToInputs() {
    const tokens = uniqueSorted(state.refSuggestions.tokens);
    const pools = uniqueSorted(state.refSuggestions.pools);
    const farms = uniqueSorted(state.refSuggestions.farms);

    state.refSuggestions.tokens = tokens;
    state.refSuggestions.pools = pools;
    state.refSuggestions.farms = farms;

    setDatalistOptions(els.tokenRefList, tokens);
    setDatalistOptions(els.poolRefList, pools);
    setDatalistOptions(els.farmRefList, farms);
  }

  function collectTokenRefsFromRows(rows) {
    const refs = [];
    for (const row of Array.isArray(rows) ? rows : []) {
      if (!row || typeof row !== "object") continue;
      const tokenID = String(row.token_id || "").trim();
      const symbol = String(row.symbol || "").trim();
      if (tokenID) refs.push(tokenID);
      if (symbol) refs.push(symbol);
    }
    return refs;
  }

  function collectPoolRefsFromRows(rows) {
    const refs = [];
    for (const row of Array.isArray(rows) ? rows : []) {
      if (!row || typeof row !== "object") continue;
      const poolID = String(row.pool_id || "").trim();
      const tokenA = String(row.token_a || "").trim();
      const tokenB = String(row.token_b || "").trim();
      if (poolID) refs.push(poolID);
      if (tokenA) refs.push(tokenA);
      if (tokenB) refs.push(tokenB);
    }
    return refs;
  }

  async function syncRefSuggestions() {
    const account = String((els.accountRef && els.accountRef.value) || "").trim();
    const [tokensResult, poolsResult] = await Promise.allSettled([
      rpc("dtl_listTokens", account ? [account] : []),
      rpc("dtl_listPools", []),
    ]);

    let tokenRows = [];
    let poolRows = [];
    if (tokensResult.status === "fulfilled" && Array.isArray(tokensResult.value)) {
      tokenRows = tokensResult.value;
    }
    if (poolsResult.status === "fulfilled" && Array.isArray(poolsResult.value)) {
      poolRows = poolsResult.value;
    }
    if (!tokenRows.length && !poolRows.length) {
      throw new Error("Unable to sync refs (dtl_listTokens/dtl_listPools failed)");
    }

    state.refSuggestions.tokens = uniqueSorted([
      ...state.refSuggestions.tokens,
      ...collectTokenRefsFromRows(tokenRows),
    ]);
    state.refSuggestions.pools = uniqueSorted([
      ...state.refSuggestions.pools,
      ...collectPoolRefsFromRows(poolRows),
    ]);
    state.refSuggestions.updatedAt = Date.now();
    applyRefSuggestionsToInputs();

    els.readOut.textContent = asPretty({
      status: "ok",
      refs_synced: true,
      token_refs: state.refSuggestions.tokens.length,
      pool_refs: state.refSuggestions.pools.length,
    });
  }

  function resolveTxSender() {
    const explicit = String((els.txFrom && els.txFrom.value) || "").trim();
    if (explicit) return explicit;
    return String((state.walletAccount || "")).trim();
  }

  function applyTxSenderAsSpender() {
    const sender = resolveTxSender();
    if (!sender) {
      throw new Error("Set tx From first (spender must match tx sender)");
    }
    if (els.tfSpender) {
      els.tfSpender.value = sender;
    }
    updateTransferFromSpenderHint();
    scheduleDraftSave();
  }

  function updateTransferFromSpenderHint() {
    if (!els.transferFromSpenderHint) return;
    const sender = resolveTxSender();
    const spender = String((els.tfSpender && els.tfSpender.value) || "").trim();
    if (!sender) {
      els.transferFromSpenderHint.textContent = "Effective spender: set tx From first";
      return;
    }
    if (!spender) {
      els.transferFromSpenderHint.textContent = `Effective spender: ${sender} (auto from tx sender)`;
      return;
    }
    if (spender === sender) {
      els.transferFromSpenderHint.textContent = `Effective spender: ${sender} (valid)`;
      return;
    }
    els.transferFromSpenderHint.textContent = `Effective spender invalid: spender must equal tx sender ${sender}`;
  }

  function prepareApproveFromTransfer() {
    const sender = resolveTxSender();
    if (!sender) {
      throw new Error("Set tx From first");
    }
    const tokenID = String((els.tfTokenId && els.tfTokenId.value) || "").trim();
    const amount = Math.max(0, toBoundedInt(els.tfAmount && els.tfAmount.value, 0, 0));
    if (els.taTokenId) els.taTokenId.value = tokenID;
    if (els.taSpender) els.taSpender.value = sender;
    if (els.taAmount) els.taAmount.value = String(amount);
    if (els.dtlType) els.dtlType.value = "TOKEN_APPROVE";
    syncGuidedVisibility();
    applyApproveDetailsToPayload();
    refreshWorkspaceMeta();
    els.txOut.textContent = "TOKEN_APPROVE prefilled from TOKEN_TRANSFER_FROM. Submit this first from owner wallet.";
    scheduleDraftSave();
  }

  function renderHeaderMeta(el, title, value, stateKind) {
    if (!el) return;
    const raw = String(value || "").trim() || "-";
    const text = raw.length > 72 ? `${raw.slice(0, 69)}...` : raw;
    let klass = "runtime-pill-info";
    if (stateKind === "ok") klass = "runtime-pill-on";
    if (stateKind === "error") klass = "runtime-pill-off";
    if (stateKind === "pending" || stateKind === "unknown") klass = "runtime-pill-unknown";

    el.textContent = "";
    el.classList.add("runtime-meta");

    const titleEl = document.createElement("span");
    titleEl.className = "runtime-meta-title";
    titleEl.textContent = title;
    el.appendChild(titleEl);

    const pills = document.createElement("span");
    pills.className = "runtime-pills";
    const pill = document.createElement("span");
    pill.className = `runtime-pill ${klass}`;
    pill.textContent = text;
    if (text !== raw) {
      pill.title = raw;
    }
    pills.appendChild(pill);
    el.appendChild(pills);
  }

  function setChainMeta(value, stateKind) {
    renderHeaderMeta(els.chainMeta, "Chain ID", value, stateKind);
  }

  function setWalletMeta(value, stateKind) {
    renderHeaderMeta(els.walletMeta, "Wallet", value, stateKind);
  }

  function setStatePill(el, label, stateKind) {
    if (!el) return;
    let klass = "runtime-pill-info";
    if (stateKind === "ok") klass = "runtime-pill-on";
    if (stateKind === "error") klass = "runtime-pill-off";
    if (stateKind === "pending" || stateKind === "unknown") klass = "runtime-pill-unknown";
    el.className = `runtime-pill ${klass}`;
    el.textContent = String(label || "").trim() || "-";
  }

  function setConnState(label, ok) {
    setStatePill(els.connState, label, ok ? "ok" : "error");
    if (els.wsConn) {
      els.wsConn.textContent = label;
      els.wsConn.classList.remove("ws-state-on", "ws-state-off", "ws-state-pending");
      els.wsConn.classList.add(ok ? "ws-state-on" : "ws-state-off");
    }
  }

  function setSignerState(label, ok) {
    setStatePill(els.signerState, label, ok ? "ok" : "error");
  }

  function authHeaders() {
    const token = normalizeToken(els.apiToken.value);
    const h = { "Content-Type": "application/json" };
    if (token) h.Authorization = `Bearer ${token}`;
    return h;
  }

  function endpoint(path) {
    return `${normalizeRpcBase(els.rpcBase.value)}${path}`;
  }

  function wait(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  function walletURLFromRPC() {
    const base = normalizeRpcBase(els.rpcBase.value) || state.rpcBase || window.location.origin;
    try {
      const url = new URL(base);
      url.pathname = "/index.html";
      url.searchParams.set("bridge", "1");
      url.searchParams.set("cb", String(Date.now()));
      return url.toString();
    } catch (_) {
      return `${window.location.origin}/index.html?bridge=1&cb=${Date.now()}`;
    }
  }

  function showWalletBridgeModal() {
    if (!els.walletBridgeModal) return;
    els.walletBridgeModal.classList.remove("hidden");
    els.walletBridgeModal.setAttribute("aria-hidden", "false");
  }

  function hideWalletBridgeModal() {
    if (!els.walletBridgeModal) return;
    els.walletBridgeModal.classList.add("hidden");
    els.walletBridgeModal.setAttribute("aria-hidden", "true");
  }

  async function openWalletFrame(url) {
    const frame = els.walletBridgeFrame;
    if (!frame) {
      throw new Error("Wallet frame container missing");
    }
    showWalletBridgeModal();
    if (els.walletBridgeHint) {
      els.walletBridgeHint.textContent = "Approve wallet connection in this embedded MSC wallet panel.";
    }
    if (state.bridgeFrameLoadedURL === url && frame.contentWindow) {
      return frame.contentWindow;
    }
    return new Promise((resolve, reject) => {
      let done = false;
      const finish = (err, value) => {
        if (done) return;
        done = true;
        frame.removeEventListener("load", onLoad);
        clearTimeout(timer);
        if (err) {
          reject(err);
          return;
        }
        resolve(value);
      };
      const onLoad = () => {
        try {
          state.bridgeFrameLoadedURL = url;
          finish(null, frame.contentWindow);
        } catch (err) {
          finish(err instanceof Error ? err : new Error(String(err)));
        }
      };
      const timer = setTimeout(() => {
        finish(new Error("Embedded MSC wallet did not load in time"));
      }, 20000);
      frame.addEventListener("load", onLoad, { once: true });
      frame.src = url;
    });
  }

  async function ensureBridgeTarget(url) {
    if (state.bridgeMode === "iframe" && state.bridgeTarget) {
      return state.bridgeTarget;
    }
    const frameWin = await openWalletFrame(url);
    state.bridgeMode = "iframe";
    state.bridgeTarget = frameWin;
    return frameWin;
  }

  function bridgeRequest(method, params, timeoutMs) {
    const target = state.bridgeTarget;
    if (!target) {
      throw new Error("MSC wallet bridge target unavailable");
    }
    const requestID = `dtl-${Date.now()}-${++state.bridgeSeq}`;
    const targetOrigin = state.bridgeOrigin || "*";
    const expectedOrigin = normalizeBridgeOrigin(state.bridgeOrigin);

    return new Promise((resolve, reject) => {
      let finished = false;
      const finish = (err, value) => {
        if (finished) return;
        finished = true;
        window.removeEventListener("message", onMessage);
        clearTimeout(timer);
        if (err) {
          reject(err);
          return;
        }
        resolve(value);
      };

      const onMessage = (event) => {
        const data = event && event.data;
        if (!data || typeof data !== "object") return;
        if (data.namespace !== MSC_PROVIDER_BRIDGE_NAMESPACE) return;
        if (String(data.id || "") !== requestID) return;
        if (event.source !== target) return;
        if (expectedOrigin && normalizeBridgeOrigin(event.origin) !== expectedOrigin) return;

        if (data.type === "response") {
          if (data.error) {
            finish(new Error(String(data.error.message || "wallet bridge request failed")));
            return;
          }
          finish(null, data.result);
        }
      };

      window.addEventListener("message", onMessage);
      const timer = setTimeout(() => {
        finish(new Error(`MSC Wallet bridge timeout for ${method}`));
      }, Math.max(1000, timeoutMs || 45000));

      try {
        target.postMessage(
          {
            namespace: MSC_PROVIDER_BRIDGE_NAMESPACE,
            type: "request",
            id: requestID,
            method,
            params: Array.isArray(params) ? params : [],
          },
          targetOrigin
        );
      } catch (err) {
        finish(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  async function waitForBridgeReady() {
    for (let i = 0; i < 18; i += 1) {
      if (!state.bridgeTarget) {
        throw new Error("MSC wallet bridge target unavailable");
      }
      try {
        const out = await bridgeRequest("msc_bridge_ping", [], 1200);
        if (out && out.ok) return out;
      } catch (err) {
        const msg = err && err.message ? String(err.message) : "";
        if (/method not found/i.test(msg)) {
          return { ok: true, pingUnsupported: true };
        }
        // wait for wallet page bootstrap
      }
      await wait(250);
    }
    throw new Error("MSC wallet bridge is not ready");
  }

  async function requestMSCAccountsViaBridge(timeoutMs) {
    const methods = ["msc_requestAccounts", "msc_requestaccounts", "msc_request_accounts"];
    let lastErr = null;
    for (const method of methods) {
      try {
        const out = await bridgeRequest(method, [], timeoutMs || 180000);
        if (Array.isArray(out)) {
          return out;
        }
      } catch (err) {
        const msg = err && err.message ? String(err.message) : "";
        if (/method not found/i.test(msg)) {
          lastErr = err;
          continue;
        }
        throw err;
      }
    }
    if (lastErr) throw lastErr;
    throw new Error("MSC custom account method unavailable");
  }

  async function waitForBridgeWalletReady(maxWaitMs) {
    const deadline = Date.now() + Math.max(5000, maxWaitMs || 120000);
    let lastState = null;
    while (Date.now() < deadline) {
      try {
        const status = await bridgeRequest("msc_bridge_ping", [], 1500);
        lastState = status;
        if (status && status.walletLoaded && status.unlocked) {
          return status;
        }
        if (els.walletBridgeHint) {
          if (!status || !status.walletLoaded) {
            els.walletBridgeHint.textContent =
              "Create or import wallet in embedded panel, then unlock it.";
          } else if (!status.unlocked) {
            els.walletBridgeHint.textContent =
              "Wallet found. Unlock it in embedded panel, approval popup will appear automatically.";
          }
        }
      } catch (_) {
        // ignore intermittent bridge read errors during startup.
      }
      await wait(700);
    }

    if (lastState && !lastState.walletLoaded) {
      throw new Error("MSC wallet not loaded. Create/import wallet in embedded panel.");
    }
    if (lastState && !lastState.unlocked) {
      throw new Error("MSC wallet is locked. Unlock wallet in embedded panel.");
    }
    throw new Error("MSC wallet is not ready");
  }

  async function connectWalletViaBridge() {
    try {
      const popupURL = walletURLFromRPC();
      await ensureBridgeTarget(popupURL);
      state.bridgeOrigin = (() => {
        try {
          return new URL(popupURL).origin;
        } catch (_) {
          return window.location.origin;
        }
      })();
      showWalletBridgeModal();
      if (els.walletBridgeHint) {
        els.walletBridgeHint.textContent =
          "Open wallet panel, unlock wallet, then approve the connect request.";
      }

      setWalletMeta("Waiting approval", "pending");
      const ping = await waitForBridgeReady();
      if (!ping || !ping.ok) {
        throw new Error("MSC wallet bridge ping failed");
      }
      const readyState = ping.pingUnsupported
        ? ping
        : (ping.walletLoaded && ping.unlocked
          ? ping
          : await waitForBridgeWalletReady(180000));
      if (els.walletBridgeHint) {
        els.walletBridgeHint.textContent =
          "Wallet is ready. Approve the custom connect popup to finish linking.";
      }
      const accounts = await requestMSCAccountsViaBridge(180000);
      if (!Array.isArray(accounts) || !accounts.length || !accounts[0]) {
        throw new Error("MSC wallet returned no account");
      }

      state.walletAccount = String(accounts[0]).trim();
      localStorage.setItem("msc_dtl_wallet_account", state.walletAccount);

      if (!String(els.accountRef.value || "").trim()) {
        els.accountRef.value = state.walletAccount;
      }
      if (!String(els.txFrom.value || "").trim() && state.wallet && state.wallet.address) {
        els.txFrom.value = state.wallet.address;
      }

      setWalletMeta(state.walletAccount, "ok");
      els.txOut.textContent = asPretty({
        status: "wallet_connected",
        account: state.walletAccount,
        method: "msc_requestAccounts",
        signer_from: String(els.txFrom.value || "").trim() || null,
        chain_id_hex: readyState.chainId || ping.chainId || null,
      });
      if (state.bridgeMode === "iframe") {
        hideWalletBridgeModal();
      }
      return true;
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      setWalletMeta(msg, "error");
      els.txOut.textContent = `Wallet connect failed: ${msg}`;
      if (els.walletBridgeHint) {
        if (/not loaded/i.test(msg)) {
          els.walletBridgeHint.textContent =
            "Create or import wallet in panel, then unlock and click Connect Wallet again.";
        } else if (/locked/i.test(msg)) {
          els.walletBridgeHint.textContent =
            "Wallet is locked. Unlock it in panel, then click Connect Wallet again.";
        } else if (/rejected/i.test(msg)) {
          els.walletBridgeHint.textContent =
            "Request rejected. Click Connect Wallet again and press Approve.";
        } else if (/method not found/i.test(msg)) {
          els.walletBridgeHint.textContent =
            "Wallet bridge is stale cache. Hard refresh (Ctrl+F5) index.html and dtl_ide.html, then retry.";
        } else {
          els.walletBridgeHint.textContent = `Connect failed: ${msg}`;
        }
      }
      return false;
    }
  }

  async function rpc(method, params, submit) {
    const body = {
      jsonrpc: "2.0",
      id: Date.now(),
      method,
      params: Array.isArray(params) ? params : [],
    };
    const res = await fetch(endpoint("/rpc"), {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status} ${await res.text()}`);
    }
    const json = await res.json();
    if (json.error) {
      throw new Error(json.error.message || "rpc error");
    }
    if (submit && typeof json.result === "string") {
      els.statusTxId.value = json.result;
    }
    return json.result;
  }

  async function getJSON(path) {
    const res = await fetch(endpoint(path), {
      method: "GET",
      headers: authHeaders(),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status} ${await res.text()}`);
    return res.json();
  }

  function asPretty(v) {
    return JSON.stringify(v, null, 2);
  }

  function shortValue(value, head, tail) {
    const raw = String(value || "").trim();
    if (!raw) return "-";
    const left = Number.isInteger(head) ? head : 14;
    const right = Number.isInteger(tail) ? tail : 10;
    if (raw.length <= left + right + 3) return raw;
    return `${raw.slice(0, left)}...${raw.slice(-right)}`;
  }

  function refreshWorkspaceMeta() {
    if (els.wsType) {
      els.wsType.textContent = String(els.dtlType && els.dtlType.value ? els.dtlType.value : "TOKEN_CREATE").trim();
    }
    if (els.wsTxId) {
      els.wsTxId.textContent = shortValue(state.lastTxID || "-", 14, 12);
      els.wsTxId.title = state.lastTxID || "";
    }
  }

  function firstSupportedDTLType() {
    if (!els.dtlType) return "TOKEN_CREATE";
    for (const option of Array.from(els.dtlType.options || [])) {
      const value = String((option && option.value) || "").trim().toUpperCase();
      if (value && !isContractTxType(value)) {
        return value;
      }
    }
    return "TOKEN_CREATE";
  }

  function pruneUnsupportedDTLTypeOptions() {
    if (!els.dtlType) return;
    for (const option of Array.from(els.dtlType.options || [])) {
      const value = String((option && option.value) || "").trim().toUpperCase();
      if (isContractTxType(value)) {
        option.remove();
      }
    }
  }

  function coerceToSupportedDTLType(emitMessage) {
    if (!els.dtlType) return;
    const current = String(els.dtlType.value || "").trim().toUpperCase();
    const needsFallback = !current || isContractTxType(current);
    if (!needsFallback) return;
    const fallback = firstSupportedDTLType();
    els.dtlType.value = fallback;
    if (emitMessage && els.txOut) {
      els.txOut.textContent = `${DTL_CONTRACT_RUNTIME_REMOVED_REASON}. Switched tx type to ${fallback}.`;
    }
  }

  function rememberLastTxID(txID) {
    const normalized = String(txID || "").trim().toLowerCase();
    if (!normalized) return;
    state.lastTxID = normalized;
    localStorage.setItem("msc_dtl_last_tx_id", normalized);
    refreshWorkspaceMeta();
  }

  function copyValueToClipboard(value, successLabel) {
    const raw = String(value || "").trim();
    if (!raw) {
      els.txOut.textContent = "Nothing to copy.";
      return;
    }
    navigator.clipboard.writeText(raw).then(
      () => {
        els.txOut.textContent = successLabel;
      },
      () => {
        els.txOut.textContent = "Copy failed.";
      },
    );
  }

  function hexToBytes(hex) {
    const raw = String(hex || "").trim();
    if (!raw) return new Uint8Array();
    const clean = raw.replace(/^0x/i, "");
    if (clean.length % 2 !== 0) {
      throw new Error("hex length must be even");
    }
    const out = new Uint8Array(clean.length / 2);
    for (let i = 0; i < out.length; i++) {
      out[i] = Number.parseInt(clean.slice(i * 2, i * 2 + 2), 16);
    }
    return out;
  }

  function bytesToHex(bytes) {
    return Array.from(bytes).map((b) => b.toString(16).padStart(2, "0")).join("");
  }

  function concatBytes(parts) {
    const size = parts.reduce((n, p) => n + p.length, 0);
    const out = new Uint8Array(size);
    let off = 0;
    for (const part of parts) {
      out.set(part, off);
      off += part.length;
    }
    return out;
  }


	const UNSUPPORTED_CONTRACT_TYPES = new Set(["CONTRACT_DEPLOY", "CONTRACT_CALL"]);
  const DTL_CONTRACT_RUNTIME_REMOVED_REASON = "DTL contract runtime removed in this build";

  function isContractTxType(kind) {
    return UNSUPPORTED_CONTRACT_TYPES.has(String(kind || "").trim().toUpperCase());
  }

  function assertSupportedDTLTxType(kind) {
    const normalized = String(kind || "").trim().toUpperCase();
    if (isContractTxType(normalized)) {
      throw new Error(`${DTL_CONTRACT_RUNTIME_REMOVED_REASON}: ${normalized}`);
    }
    return normalized;
  }


  async function sha256(bytes) {
    const out = await crypto.subtle.digest("SHA-256", bytes);
    return new Uint8Array(out);
  }


  async function deriveKey(password, salt, iterations) {
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
        iterations: iterations || 150000,
        hash: "SHA-256",
      },
      keyMaterial,
      { name: "AES-GCM", length: 256 },
      false,
      ["decrypt"],
    );
  }

  async function decryptSecretKey(cryptoData, password) {
    const salt = hexToBytes(cryptoData.salt);
    const iv = hexToBytes(cryptoData.iv);
    const ciphertext = hexToBytes(cryptoData.ciphertext);
    const key = await deriveKey(password, salt, cryptoData.iterations || 150000);
    const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ciphertext);
    return new Uint8Array(plain);
  }

  function loadStoredWallet() {
    for (const key of WALLET_STORAGE_KEYS) {
      const raw = localStorage.getItem(key);
      if (!raw) continue;
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === "object") {
          return parsed;
        }
      } catch (_) {
        // try next known key
      }
    }
    return null;
  }

  function stripHexPrefix(v) {
    return String(v || "").trim().replace(/^0x/i, "");
  }

  function buildTxPayload(tx) {
    const parts = [];
    const txType = Number.parseInt(tx.Type ?? tx.type ?? 0, 10) || 0;
    const normalizedValidatorPubKey = normalizeFixedHex(
      tx.validator_pubkey || tx.ValidatorPubKey || ""
    ).toLowerCase();
    const pushString = (value) => {
      const bytes = enc.encode(String(value || ""));
      parts.push(bytes);
      parts.push(new Uint8Array([0]));
    };
    const pushInt64 = (value) => {
      const buf = new ArrayBuffer(8);
      const view = new DataView(buf);
      view.setBigInt64(0, BigInt(value || 0), false);
      parts.push(new Uint8Array(buf));
    };

    pushString(String(tx.from || "").trim());
    pushString(String(tx.to || "").trim());
    const coinValue = tx.Coin !== undefined && tx.Coin !== null && String(tx.Coin) !== ""
      ? tx.Coin
      : (tx.coin !== undefined && tx.coin !== null && String(tx.coin) !== "" ? tx.coin : "MSC");
    pushString(String(coinValue));
    pushInt64(Number.parseInt(tx.amount, 10) || 0);
    pushInt64(Number.parseInt(tx.fee, 10) || 0);
    pushInt64(Number.parseInt(tx.nonce, 10) || 0);
    pushInt64(Number.parseInt(tx.expiry, 10) || 0);
    pushInt64(Number.parseInt(tx.stake_epochs || 0, 10) || 0);
    if (txType === 2 && isFixedHex(normalizedValidatorPubKey, 32)) {
      pushString(normalizedValidatorPubKey);
    }
		// Preserve historical wire slots as constants; DTL signing never carries
		// programmable VM payloads.
		pushInt64(0);
		pushString("");
		pushString("");
		pushString("");
		pushString("");

    if (txType === 8) {
      pushString(String(tx.dtl_tx_type || "").trim());
      pushString(String(tx.dtl_token_id || "").trim());
      pushString(String(tx.dtl_payload || "").trim());
      pushString(String(tx.dtl_governance_cert || "").trim());
    }

    const chainIDForHash = String(state.chainId || els.txChainId.value || tx.ChainID || tx.chainId || "").trim();
    pushString(chainIDForHash);
    parts.push(new Uint8Array([txType & 0xff]));
    return concatBytes(parts);
  }

  function toBoundedInt(value, fallback, min, max) {
    const n = Number.parseInt(String(value ?? ""), 10);
    if (!Number.isInteger(n)) return fallback;
    if (Number.isInteger(min) && n < min) return min;
    if (Number.isInteger(max) && n > max) return max;
    return n;
  }

  function parseAuthoritySignersInput(raw, from) {
    const items = String(raw || "")
      .split(",")
      .map((item) => String(item || "").trim())
      .filter(Boolean);
    if (!items.length && from) return [from];
    return items;
  }

  function parseCSVList(raw) {
    return String(raw || "")
      .split(",")
      .map((item) => String(item || "").trim())
      .filter(Boolean);
  }

  function buildDTLGovCertSignBytes(certLike) {
    const chainID = String(els.txChainId.value || state.chainId || "").trim();
    const tokenID = String(certLike && certLike.token_id ? certLike.token_id : "")
      .trim()
      .toLowerCase();
    const epoch = Number.parseInt(
      String(certLike && certLike.epoch !== undefined ? certLike.epoch : 0),
      10
    );
    const action = String(certLike && certLike.action ? certLike.action : "")
      .trim()
      .toUpperCase();
    const payloadHash = stripHexPrefix(
      certLike && certLike.action_payload_hash ? certLike.action_payload_hash : ""
    ).toLowerCase();
    const msg = `MSC|DTL|GCERT|${chainID}|${tokenID}|${Number.isInteger(epoch) ? epoch : 0}|${action}|${payloadHash}`;
    return enc.encode(msg);
  }

  function buildTokenCreatePayloadFromFields(fromAddress) {
    const from = String(fromAddress || els.txFrom.value || "").trim();
    const name = String(els.tcName && els.tcName.value ? els.tcName.value : "Mythical Token").trim() || "Mythical Token";
    const symbol = String(els.tcSymbol && els.tcSymbol.value ? els.tcSymbol.value : "MYTK").trim() || "MYTK";
    const decimals = toBoundedInt(els.tcDecimals && els.tcDecimals.value, 18, 0, 18);
    const maxSupply = Math.max(1, toBoundedInt(els.tcMaxSupply && els.tcMaxSupply.value, 1000000, 1));
    const initialSupply = Math.max(0, toBoundedInt(els.tcInitialSupply && els.tcInitialSupply.value, 1000, 0));
    const taxBps = toBoundedInt(els.tcTaxBps && els.tcTaxBps.value, 0, 0, 10000);
    const freezeEnabled = String(els.tcFreezeEnabled && els.tcFreezeEnabled.value).toLowerCase() === "true";
    const metadataURI = String(els.tcMetadataUri && els.tcMetadataUri.value ? els.tcMetadataUri.value : "").trim();
    const signers = parseAuthoritySignersInput(els.tcAuthoritySigners && els.tcAuthoritySigners.value, from);
    const thresholdRaw = toBoundedInt(els.tcAuthorityThreshold && els.tcAuthorityThreshold.value, 1, 1);
    const threshold = Math.max(1, Math.min(thresholdRaw, Math.max(1, signers.length)));
    return {
      creator: from,
      name,
      symbol,
      decimals,
      max_supply: maxSupply,
      initial_supply: Math.min(initialSupply, maxSupply),
      authority_signers: signers,
      authority_threshold: threshold,
      freeze_enabled: freezeEnabled,
      tax_bps: taxBps,
      metadata_uri: metadataURI,
    };
  }

  function applyTokenCreateDetailsFromPayload(payload) {
    if (!payload || typeof payload !== "object") return;
    if (els.tcName) els.tcName.value = String(payload.name || "");
    if (els.tcSymbol) els.tcSymbol.value = String(payload.symbol || "");
    if (els.tcDecimals) els.tcDecimals.value = String(toBoundedInt(payload.decimals, 18, 0, 18));
    if (els.tcInitialSupply) els.tcInitialSupply.value = String(toBoundedInt(payload.initial_supply, 0, 0));
    if (els.tcMaxSupply) els.tcMaxSupply.value = String(toBoundedInt(payload.max_supply, 1000000, 1));
    if (els.tcAuthorityThreshold) {
      els.tcAuthorityThreshold.value = String(toBoundedInt(payload.authority_threshold, 1, 1));
    }
    if (els.tcFreezeEnabled) {
      els.tcFreezeEnabled.value = payload.freeze_enabled ? "true" : "false";
    }
    if (els.tcTaxBps) els.tcTaxBps.value = String(toBoundedInt(payload.tax_bps, 0, 0, 10000));
    if (els.tcMetadataUri) els.tcMetadataUri.value = String(payload.metadata_uri || "");
    if (els.tcAuthoritySigners) {
      const signers = Array.isArray(payload.authority_signers) ? payload.authority_signers : [];
      if (!signers.length) {
        const from = String(els.txFrom && els.txFrom.value ? els.txFrom.value : "").trim();
        if (from) signers.push(from);
      }
      els.tcAuthoritySigners.value = signers.join(", ");
    }
  }

  function buildTokenTransferPayloadFromFields(fromAddress) {
    const from = String(fromAddress || els.txFrom.value || "").trim();
    const tokenID = String(
      (els.ttTokenId && els.ttTokenId.value) || els.txTokenId.value || ""
    ).trim();
    const to = String((els.ttTo && els.ttTo.value) || "").trim();
    const amount = Math.max(1, toBoundedInt(els.ttAmount && els.ttAmount.value, 1, 1));
    return {
      from,
      to,
      token_id: tokenID,
      amount,
    };
  }

  function applyTokenTransferDetailsFromPayload(payload) {
    if (!payload || typeof payload !== "object") return;
    if (els.ttTokenId) els.ttTokenId.value = String(payload.token_id || "");
    if (els.txTokenId) els.txTokenId.value = String(payload.token_id || "");
    if (els.ttTo) els.ttTo.value = String(payload.to || "");
    if (els.ttAmount) els.ttAmount.value = String(toBoundedInt(payload.amount, 1, 1));
  }

  function buildTokenApprovePayloadFromFields(fromAddress) {
    const owner = String(fromAddress || els.txFrom.value || "").trim();
    const tokenID = String(
      (els.taTokenId && els.taTokenId.value) || els.txTokenId.value || ""
    ).trim();
    const spender = String((els.taSpender && els.taSpender.value) || "").trim();
    const amount = Math.max(0, toBoundedInt(els.taAmount && els.taAmount.value, 0, 0));
    return {
      owner,
      spender,
      token_id: tokenID,
      amount,
    };
  }

  function applyTokenApproveDetailsFromPayload(payload) {
    if (!payload || typeof payload !== "object") return;
    if (els.taTokenId) els.taTokenId.value = String(payload.token_id || "");
    if (els.txTokenId) els.txTokenId.value = String(payload.token_id || "");
    if (els.taSpender) els.taSpender.value = String(payload.spender || "");
    if (els.taAmount) els.taAmount.value = String(toBoundedInt(payload.amount, 0, 0));
  }

  function buildTokenTransferFromPayloadFromFields(fromAddress) {
    const sender = String(fromAddress || els.txFrom.value || "").trim();
    const rawSpender = String((els.tfSpender && els.tfSpender.value) || "").trim();
    const spender = rawSpender || sender;
    if (!rawSpender && sender && els.tfSpender) {
      els.tfSpender.value = sender;
    }
    const tokenID = String(
      (els.tfTokenId && els.tfTokenId.value) || els.txTokenId.value || ""
    ).trim();
    const from = String((els.tfFrom && els.tfFrom.value) || "").trim();
    const to = String((els.tfTo && els.tfTo.value) || "").trim();
    const amount = Math.max(1, toBoundedInt(els.tfAmount && els.tfAmount.value, 1, 1));
    if (sender && spender && sender !== spender) {
      throw new Error(`TransferFrom spender must match tx sender (${sender})`);
    }
    updateTransferFromSpenderHint();
    return {
      spender,
      from,
      to,
      token_id: tokenID,
      amount,
    };
  }

  function applyTokenTransferFromDetailsFromPayload(payload) {
    if (!payload || typeof payload !== "object") return;
    if (els.tfTokenId) els.tfTokenId.value = String(payload.token_id || "");
    if (els.txTokenId) els.txTokenId.value = String(payload.token_id || "");
    if (els.tfSpender) els.tfSpender.value = String(payload.spender || "");
    if (els.tfFrom) els.tfFrom.value = String(payload.from || "");
    if (els.tfTo) els.tfTo.value = String(payload.to || "");
    if (els.tfAmount) els.tfAmount.value = String(toBoundedInt(payload.amount, 1, 1));
    updateTransferFromSpenderHint();
  }

  function buildTokenMintPayloadFromFields(fromAddress) {
    const proposer = String(fromAddress || els.txFrom.value || "").trim();
    const tokenID = String(
      (els.tmTokenId && els.tmTokenId.value) || els.txTokenId.value || ""
    ).trim();
    const to = String((els.tmTo && els.tmTo.value) || "").trim();
    const amount = Math.max(1, toBoundedInt(els.tmAmount && els.tmAmount.value, 1, 1));
    return {
      proposer,
      to,
      token_id: tokenID,
      amount,
    };
  }

  function applyTokenMintDetailsFromPayload(payload) {
    if (!payload || typeof payload !== "object") return;
    if (els.tmTokenId) els.tmTokenId.value = String(payload.token_id || "");
    if (els.txTokenId) els.txTokenId.value = String(payload.token_id || "");
    if (els.tmTo) els.tmTo.value = String(payload.to || "");
    if (els.tmAmount) els.tmAmount.value = String(toBoundedInt(payload.amount, 1, 1));
  }

  async function buildMintGovernanceCertFromFields(payloadObj) {
    const tokenID = String(
      (els.tmTokenId && els.tmTokenId.value) ||
      payloadObj.token_id ||
      els.txTokenId.value ||
      ""
    ).trim();
    const epoch = Math.max(1, toBoundedInt(els.tmEpoch && els.tmEpoch.value, 1, 1));
    const action = String((els.tmAction && els.tmAction.value) || "MINT").trim() || "MINT";
    const signers = parseCSVList(els.tmSigners && els.tmSigners.value);
    const signatureEntries = parseCSVList(els.tmSignatures && els.tmSignatures.value);
    if (signatureEntries.length && signers.length !== signatureEntries.length) {
      throw new Error("Governance cert signers/signatures count mismatch");
    }
    const payloadBytes = enc.encode(JSON.stringify(payloadObj || {}));
    const payloadHash = bytesToHex(await sha256(payloadBytes));

    const certBase = {
      token_id: tokenID,
      epoch,
      action,
      action_payload_hash: payloadHash,
    };
    const signBytes = buildDTLGovCertSignBytes(certBase);

    const walletAddress = String((state.wallet && state.wallet.address) || "").trim().toLowerCase();
    const walletPub = normalizeFixedHex((state.wallet && state.wallet.publicKey) || "");
    const canWalletSign =
      !!state.secretKey &&
      !!walletAddress &&
      isFixedHex(walletPub, 32) &&
      typeof nacl !== "undefined" &&
      !!nacl.sign &&
      !!nacl.sign.detached;

    const signatures = [];
    const signerPublicKeys = [];

    for (let i = 0; i < signers.length; i += 1) {
      const signer = String(signers[i] || "").trim();
      const signerNorm = signer.toLowerCase();
      const rawEntry = String(signatureEntries[i] || "").trim();
      let sigHex = "";
      let pubHex = "";

      if (rawEntry) {
        const sep = rawEntry.indexOf(":");
        if (sep > 0) {
          pubHex = normalizeFixedHex(rawEntry.slice(0, sep));
          sigHex = normalizeFixedHex(rawEntry.slice(sep + 1));
        } else {
          sigHex = normalizeFixedHex(rawEntry);
        }
      }

      if (!sigHex && canWalletSign && signerNorm === walletAddress) {
        sigHex = bytesToHex(nacl.sign.detached(signBytes, state.secretKey));
      }
      if (!pubHex && signerNorm === walletAddress && isFixedHex(walletPub, 32)) {
        pubHex = walletPub;
      }

      if (!sigHex) {
        throw new Error(`Missing cert signature for signer ${signer || i + 1}`);
      }
      if (!isFixedHex(sigHex, 64)) {
        throw new Error(`Invalid cert signature format for signer ${signer || i + 1}`);
      }
      if (!pubHex) {
        throw new Error(
          `Missing signer public key for ${signer || i + 1}. Use 'pubkey:signature' format.`
        );
      }
      if (!isFixedHex(pubHex, 32)) {
        throw new Error(`Invalid signer public key for ${signer || i + 1}`);
      }

      signatures.push(sigHex);
      signerPublicKeys.push(pubHex);
    }

    return {
      ...certBase,
      signers,
      signer_public_keys: signerPublicKeys,
      signatures,
    };
  }

  function applyMintCertDetailsFromCert(cert) {
    if (!cert || typeof cert !== "object") return;
    if (els.tmTokenId && cert.token_id) {
      els.tmTokenId.value = String(cert.token_id || "");
    }
    if (els.tmEpoch) els.tmEpoch.value = String(toBoundedInt(cert.epoch, 1, 1));
    if (els.tmAction) els.tmAction.value = String(cert.action || "MINT");
    if (els.tmSigners) {
      const signers = Array.isArray(cert.signers) ? cert.signers : [];
      els.tmSigners.value = signers.join(", ");
    }
    if (els.tmSignatures) {
      const signatures = Array.isArray(cert.signatures) ? cert.signatures : [];
      const signerPubs = Array.isArray(cert.signer_public_keys) ? cert.signer_public_keys : [];
      if (signerPubs.length === signatures.length && signerPubs.length > 0) {
        const combined = signatures.map((sig, idx) => {
          const pub = String(signerPubs[idx] || "").trim();
          const s = String(sig || "").trim();
          return pub && s ? `${pub}:${s}` : s;
        });
        els.tmSignatures.value = combined.join(", ");
      } else {
        els.tmSignatures.value = signatures.join(", ");
      }
    }
  }

  function buildTokenBurnPayloadFromFields(fromAddress) {
    const from = String(fromAddress || els.txFrom.value || "").trim();
    const tokenID = String(
      (els.tbTokenId && els.tbTokenId.value) || els.txTokenId.value || ""
    ).trim();
    const amount = Math.max(1, toBoundedInt(els.tbAmount && els.tbAmount.value, 1, 1));
    return {
      from,
      token_id: tokenID,
      amount,
    };
  }

  function applyTokenBurnDetailsFromPayload(payload) {
    if (!payload || typeof payload !== "object") return;
    if (els.tbTokenId) els.tbTokenId.value = String(payload.token_id || "");
    if (els.txTokenId) els.txTokenId.value = String(payload.token_id || "");
    if (els.tbAmount) els.tbAmount.value = String(toBoundedInt(payload.amount, 1, 1));
  }

  function setPayloadPreview(payloadObj) {
    if (!els.txPayloadPreview) return;
    if (!payloadObj || typeof payloadObj !== "object") {
      els.txPayloadPreview.value = "";
      return;
    }
    els.txPayloadPreview.value = asPretty(payloadObj);
  }


  const DTL_DEDICATED_GUIDED_TYPES = new Set([
    "TOKEN_CREATE",
    "TOKEN_TRANSFER",
    "TOKEN_APPROVE",
    "TOKEN_TRANSFER_FROM",
    "TOKEN_MINT",
    "TOKEN_BURN",
  ]);

  function isBeginnerMode() {
    return !!(els.beginnerMode && els.beginnerMode.checked);
  }

  function shouldUseQuickPayloadFields(dtlType) {
    const kind = String(dtlType || "").trim().toUpperCase();
    if (!kind) return false;
    if (!isBeginnerMode()) return false;
    if (isContractTxType(kind)) return false;
    return !DTL_DEDICATED_GUIDED_TYPES.has(kind);
  }

  function parseQuickList(raw) {
    return String(raw || "")
      .split(/[\n,]/)
      .map((part) => String(part || "").trim())
      .filter(Boolean);
  }

  function quickFieldDisplayName(key) {
    return String(key || "")
      .trim()
      .replace(/_/g, " ")
      .replace(/\b\w/g, (ch) => ch.toUpperCase());
  }

  function readQuickPayloadValues() {
    const out = {};
    if (!els.quickPayloadFields) return out;
    const controls = els.quickPayloadFields.querySelectorAll("[data-qp-key]");
    controls.forEach((control) => {
      const key = String(control.getAttribute("data-qp-key") || "").trim();
      if (!key) return;
      out[key] = String(control.value || "");
    });
    return out;
  }

  function renderQuickPayloadFields(seedPayload) {
    if (!els.quickPayloadBox || !els.quickPayloadFields) return;
    const dtlType = String(els.dtlType && els.dtlType.value ? els.dtlType.value : "").trim();
    const shouldRender = shouldUseQuickPayloadFields(dtlType);
    els.quickPayloadBox.classList.toggle("hidden", !shouldRender);
    if (!shouldRender) return;

    let template = {};
    try {
      template = defaultPayload(dtlType);
    } catch (_) {
      template = {};
    }
    if (!template || typeof template !== "object" || Array.isArray(template)) {
      els.quickPayloadBox.classList.add("hidden");
      return;
    }

    const previousValues = readQuickPayloadValues();
    const seed = seedPayload && typeof seedPayload === "object" ? seedPayload : {};
    els.quickPayloadFields.textContent = "";

    Object.keys(template).forEach((key) => {
      const templateValue = template[key];
      const label = document.createElement("label");
      const caption = document.createElement("span");
      caption.textContent = quickFieldDisplayName(key);
      label.appendChild(caption);

      const preferSeed = Object.prototype.hasOwnProperty.call(seed, key) ? seed[key] : templateValue;
      const prevRaw = Object.prototype.hasOwnProperty.call(previousValues, key) ? previousValues[key] : null;
      let control = null;

      if (Array.isArray(templateValue)) {
        control = document.createElement("textarea");
        control.rows = 2;
        const baseList = Array.isArray(preferSeed) ? preferSeed : [];
        control.value = prevRaw !== null ? prevRaw : baseList.join(", ");
        control.placeholder = "comma-separated values";
      } else if (typeof templateValue === "boolean") {
        control = document.createElement("select");
        const optTrue = document.createElement("option");
        optTrue.value = "true";
        optTrue.textContent = "true";
        const optFalse = document.createElement("option");
        optFalse.value = "false";
        optFalse.textContent = "false";
        control.appendChild(optTrue);
        control.appendChild(optFalse);
        const baseBool = typeof preferSeed === "boolean" ? preferSeed : !!templateValue;
        control.value = prevRaw !== null ? String(prevRaw).toLowerCase() : String(baseBool);
      } else if (typeof templateValue === "number") {
        control = document.createElement("input");
        control.type = "number";
        control.step = "1";
        control.min = "0";
        const baseNum = Number.isFinite(Number(preferSeed)) ? Number(preferSeed) : Number(templateValue || 0);
        control.value = prevRaw !== null ? prevRaw : String(baseNum);
      } else {
        control = document.createElement("input");
        control.type = "text";
        const baseText = String(preferSeed === undefined || preferSeed === null ? "" : preferSeed);
        control.value = prevRaw !== null ? prevRaw : baseText;
      }

      control.setAttribute("data-qp-key", key);
      const syncQuickToPayload = () => {
        const payload = buildPayloadFromQuickFields(dtlType);
        if (els.txPayload) {
          els.txPayload.value = asPretty(payload);
        }
        setPayloadPreview(payload);
        scheduleDraftSave();
      };
      control.addEventListener("input", syncQuickToPayload);
      control.addEventListener("change", syncQuickToPayload);
      label.appendChild(control);
      els.quickPayloadFields.appendChild(label);
    });
  }

  function buildPayloadFromQuickFields(dtlType) {
    let template = {};
    try {
      template = defaultPayload(dtlType);
    } catch (_) {
      template = {};
    }
    if (!template || typeof template !== "object" || Array.isArray(template)) {
      return {};
    }
    if (!els.quickPayloadFields) return template;

    const out = {};
    Object.keys(template).forEach((key) => {
      const templateValue = template[key];
      const control = els.quickPayloadFields.querySelector(`[data-qp-key="${key}"]`);
      const raw = control ? String(control.value || "").trim() : "";
      if (Array.isArray(templateValue)) {
        out[key] = raw ? parseQuickList(raw) : (Array.isArray(templateValue) ? templateValue : []);
        return;
      }
      if (typeof templateValue === "boolean") {
        if (!raw) {
          out[key] = !!templateValue;
          return;
        }
        out[key] = String(raw).toLowerCase() === "true";
        return;
      }
      if (typeof templateValue === "number") {
        if (!raw) {
          out[key] = Number(templateValue || 0);
          return;
        }
        const parsed = Number(raw);
        out[key] = Number.isFinite(parsed) ? parsed : Number(templateValue || 0);
        return;
      }
      out[key] = raw || String(templateValue === undefined || templateValue === null ? "" : templateValue);
    });
    return out;
  }

  function applyBeginnerModeVisibility(isMint) {
    const beginner = isBeginnerMode();
    const advancedNodes = document.querySelectorAll(".advanced-only");
    advancedNodes.forEach((node) => {
      node.classList.toggle("hidden", beginner);
    });
    if (els.txGovCertWrap) {
      els.txGovCertWrap.classList.toggle("hidden", beginner || !isMint);
    }
    if (els.payloadTemplateBtn) {
      els.payloadTemplateBtn.classList.toggle("hidden", beginner);
    }
    renderQuickPayloadFields();
  }

  function syncGuidedVisibility() {
    const dtlType = String(els.dtlType && els.dtlType.value ? els.dtlType.value : "").trim();
    const isCreate = dtlType === "TOKEN_CREATE";
    const isTransfer = dtlType === "TOKEN_TRANSFER";
    const isApprove = dtlType === "TOKEN_APPROVE";
    const isTransferFrom = dtlType === "TOKEN_TRANSFER_FROM";
    const isMint = dtlType === "TOKEN_MINT";
    const isBurn = dtlType === "TOKEN_BURN";
    if (els.tokenCreateGuidedBox) {
      els.tokenCreateGuidedBox.classList.toggle("hidden", !isCreate);
    }
    if (els.tokenTransferGuidedBox) {
      els.tokenTransferGuidedBox.classList.toggle("hidden", !isTransfer);
    }
    if (els.tokenApproveGuidedBox) {
      els.tokenApproveGuidedBox.classList.toggle("hidden", !isApprove);
    }
    if (els.tokenTransferFromGuidedBox) {
      els.tokenTransferFromGuidedBox.classList.toggle("hidden", !isTransferFrom);
    }
    if (els.tokenMintGuidedBox) {
      els.tokenMintGuidedBox.classList.toggle("hidden", !isMint);
    }
    if (els.tokenBurnGuidedBox) {
      els.tokenBurnGuidedBox.classList.toggle("hidden", !isBurn);
    }
    applyBeginnerModeVisibility(isMint);
    updateTransferFromSpenderHint();
  }









  function defaultPayload(kind) {
    const from = String(els.txFrom.value || "").trim();
    const tokenId = String(els.txTokenId.value || "").trim();
    switch (kind) {
      case "TOKEN_CREATE":
        return buildTokenCreatePayloadFromFields(from);
      case "TOKEN_TRANSFER":
        return buildTokenTransferPayloadFromFields(from);
      case "TOKEN_APPROVE":
        return buildTokenApprovePayloadFromFields(from);
      case "TOKEN_TRANSFER_FROM":
        return buildTokenTransferFromPayloadFromFields(from);
      case "TOKEN_MINT":
        return buildTokenMintPayloadFromFields(from);
      case "TOKEN_BURN":
        return buildTokenBurnPayloadFromFields(from);
      case "NFT721_CREATE":
        return {
          creator: from,
          name: "My NFT 721",
          symbol: "MN721",
          base_uri: "",
        };
      case "NFT721_MINT":
        return {
          creator: from,
          collection_id: tokenId,
          to: from,
          token_uri: "",
        };
      case "NFT721_TRANSFER":
        return {
          from: from,
          to: "",
          collection_id: tokenId,
          token_id: 1,
        };
      case "NFT1155_CREATE":
        return {
          creator: from,
          name: "My NFT 1155",
          symbol: "MN1155",
          base_uri: "",
        };
      case "NFT1155_MINT":
        return {
          creator: from,
          collection_id: tokenId,
          to: from,
          token_id: 1,
          amount: 1,
        };
      case "NFT1155_TRANSFER":
        return {
          from: from,
          to: "",
          collection_id: tokenId,
          token_id: 1,
          amount: 1,
        };
      case "POOL_CREATE":
        return {
          creator: from,
          token_a: tokenId,
          token_b: "",
          amount_a: 1000,
          amount_b: 1000,
          fee_bps: 30,
        };
      case "POOL_ADD_LIQUIDITY":
        return {
          provider: from,
          pool_id: "",
          amount_a: 100,
          amount_b: 100,
          min_lp_shares: 1,
        };
      case "POOL_REMOVE_LIQUIDITY":
        return {
          provider: from,
          pool_id: "",
          lp_shares: 1,
          min_amount_a: 1,
          min_amount_b: 1,
        };
      case "POOL_SWAP":
        return {
          trader: from,
          pool_id: "",
          token_in: tokenId,
          amount_in: 1,
          min_amount_out: 1,
        };
      case "POOL_SWAP_ROUTE":
        return {
          trader: from,
          token_in: tokenId,
          amount_in: 1,
          min_amount_out: 1,
          path: [],
          deadline_height: 1,
        };
      case "FARM_CREATE":
        return {
          creator: from,
          farm_id: "",
          pool_id: "",
          multiplier_bps: 10000,
        };
      case "FARM_STAKE_LP":
        return {
          account: from,
          farm_id: "",
          amount: 1,
        };
      case "FARM_UNSTAKE_LP":
        return {
          account: from,
          farm_id: "",
          amount: 1,
        };
      case "FARM_CLAIM":
        return {
          account: from,
          farm_id: "",
        };
      case "DUEL_CREATE":
        return {
          creator: from,
          token_id: tokenId,
          stake: 10,
          commit_hash: "",
          join_expiry_blocks: 20,
          reveal_expiry_blocks: 20,
        };
      case "DUEL_JOIN":
        return {
          joiner: from,
          duel_id: "",
          commit_hash: "",
        };
      case "DUEL_REVEAL":
        return {
          player: from,
          duel_id: "",
          secret: "",
        };
      case "DUEL_FINALIZE":
        return {
          caller: from,
          duel_id: "",
        };
      case "LEND_MARKET_CREATE":
        return {
          creator: from,
          collateral_token_id: tokenId,
          debt_token_id: "",
          debt_liquidity: 1000,
          collateral_factor_bps: 7500,
          liquidation_bonus_bps: 500,
        };
      case "LEND_DEPOSIT_COLLATERAL":
        return {
          account: from,
          market_id: "",
          amount: 100,
        };
      case "LEND_BORROW":
        return {
          account: from,
          market_id: "",
          amount: 50,
        };
      case "LEND_REPAY":
        return {
          account: from,
          market_id: "",
          amount: 50,
        };
      case "LEND_WITHDRAW_COLLATERAL":
        return {
          account: from,
          market_id: "",
          amount: 50,
        };
      case "LEND_LIQUIDATE":
        return {
          liquidator: from,
          borrower: "",
          market_id: "",
          repay_amount: 50,
        };
      case "TOURNAMENT_CREATE":
        return {
          creator: from,
          token_id: tokenId,
          entry_fee: 10,
          max_players: 8,
          join_expiry_blocks: 30,
          reveal_expiry_blocks: 30,
        };
      case "TOURNAMENT_JOIN":
        return {
          player: from,
          tournament_id: "",
          commit_hash: "",
        };
      case "TOURNAMENT_REVEAL":
        return {
          player: from,
          tournament_id: "",
          secret: "",
        };
      case "TOURNAMENT_FINALIZE":
        return {
          caller: from,
          tournament_id: "",
        };
      case "SEASON_CREATE":
        return {
          creator: from,
          season_id: "",
          start_height: 0,
        };
      case "SEASON_FINALIZE":
        return {
          caller: from,
          season_id: "",
        };
      case "SEASON_CLAIM":
        return {
          account: from,
          season_id: "",
        };
      case "ORACLE_FEED_CREATE":
        return {
          creator: from,
          feed_id: "",
          base_token_id: tokenId,
          quote_token_id: "MSC",
          signers: from ? [from] : [],
          threshold: 1,
          decimals: 8,
        };
      case "ORACLE_PRICE_SUBMIT":
        return {
          submitter: from,
          feed_id: "",
          price: 1,
        };
      case "CONTRACT_DEPLOY":
      case "CONTRACT_CALL":
        throw new Error(`${DTL_CONTRACT_RUNTIME_REMOVED_REASON}: ${kind}`);
      default:
        return {};
    }
  }

  function parseJSONField(text, fallback) {
    const raw = String(text || "").trim();
    if (!raw) return fallback;
    return JSON.parse(raw);
  }

  function isHexAddress(value) {
    return /^0x[0-9a-fA-F]{40}$/.test(String(value || "").trim());
  }

  function isFixedHex(value, bytes) {
    const raw = String(value || "").trim().replace(/^0x/i, "");
    return new RegExp(`^[0-9a-fA-F]{${bytes * 2}}$`).test(raw);
  }

  function normalizeFixedHex(value) {
    return String(value || "").trim().replace(/^0x/i, "");
  }

  function assertMSCAddress(label, value) {
    const v = String(value || "").trim();
    if (!v) return;
    if (isHexAddress(v)) {
      throw new Error(`${label} must be MSC address (MSC...), not 0x alias`);
    }
  }

  function validateDTLAddressFields(dtlType, tx, payloadObj) {
    assertMSCAddress("From", tx.from);
    assertMSCAddress("To", tx.to);

    if (!payloadObj || typeof payloadObj !== "object") return;
    if (dtlType === "TOKEN_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      if (Array.isArray(payloadObj.authority_signers)) {
        payloadObj.authority_signers.forEach((signer, idx) => {
          assertMSCAddress(`payload.authority_signers[${idx}]`, signer);
        });
      }
      return;
    }
    if (dtlType === "TOKEN_TRANSFER") {
      assertMSCAddress("payload.from", payloadObj.from || tx.from);
      assertMSCAddress("payload.to", payloadObj.to);
      return;
    }
    if (dtlType === "TOKEN_APPROVE") {
      assertMSCAddress("payload.owner", payloadObj.owner || tx.from);
      assertMSCAddress("payload.spender", payloadObj.spender);
      return;
    }
    if (dtlType === "TOKEN_TRANSFER_FROM") {
      assertMSCAddress("payload.spender", payloadObj.spender || tx.from);
      assertMSCAddress("payload.from", payloadObj.from);
      assertMSCAddress("payload.to", payloadObj.to);
      return;
    }
    if (dtlType === "TOKEN_BURN") {
      assertMSCAddress("payload.from", payloadObj.from || tx.from);
      return;
    }
    if (dtlType === "TOKEN_MINT") {
      assertMSCAddress("payload.proposer", payloadObj.proposer || tx.from);
      assertMSCAddress("payload.to", payloadObj.to);
      return;
    }
    if (dtlType === "NFT721_CREATE" || dtlType === "NFT1155_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "NFT721_MINT" || dtlType === "NFT1155_MINT") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      assertMSCAddress("payload.to", payloadObj.to);
      return;
    }
    if (dtlType === "NFT721_TRANSFER" || dtlType === "NFT1155_TRANSFER") {
      assertMSCAddress("payload.from", payloadObj.from || tx.from);
      assertMSCAddress("payload.to", payloadObj.to);
      return;
    }
    if (dtlType === "POOL_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "POOL_ADD_LIQUIDITY" || dtlType === "POOL_REMOVE_LIQUIDITY") {
      assertMSCAddress("payload.provider", payloadObj.provider || tx.from);
      return;
    }
    if (dtlType === "POOL_SWAP") {
      assertMSCAddress("payload.trader", payloadObj.trader || tx.from);
      return;
    }
    if (dtlType === "POOL_SWAP_ROUTE") {
      assertMSCAddress("payload.trader", payloadObj.trader || tx.from);
      return;
    }
    if (dtlType === "FARM_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "FARM_STAKE_LP" || dtlType === "FARM_UNSTAKE_LP" || dtlType === "FARM_CLAIM") {
      assertMSCAddress("payload.account", payloadObj.account || tx.from);
      return;
    }
    if (dtlType === "DUEL_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "DUEL_JOIN") {
      assertMSCAddress("payload.joiner", payloadObj.joiner || tx.from);
      return;
    }
    if (dtlType === "DUEL_REVEAL") {
      assertMSCAddress("payload.player", payloadObj.player || tx.from);
      return;
    }
    if (dtlType === "DUEL_FINALIZE") {
      assertMSCAddress("payload.caller", payloadObj.caller || tx.from);
      return;
    }
    if (dtlType === "LEND_MARKET_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "LEND_DEPOSIT_COLLATERAL" || dtlType === "LEND_BORROW" || dtlType === "LEND_REPAY" || dtlType === "LEND_WITHDRAW_COLLATERAL") {
      assertMSCAddress("payload.account", payloadObj.account || tx.from);
      return;
    }
    if (dtlType === "LEND_LIQUIDATE") {
      assertMSCAddress("payload.liquidator", payloadObj.liquidator || tx.from);
      assertMSCAddress("payload.borrower", payloadObj.borrower);
      return;
    }
    if (dtlType === "TOURNAMENT_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "TOURNAMENT_JOIN" || dtlType === "TOURNAMENT_REVEAL") {
      assertMSCAddress("payload.player", payloadObj.player || tx.from);
      return;
    }
    if (dtlType === "TOURNAMENT_FINALIZE") {
      assertMSCAddress("payload.caller", payloadObj.caller || tx.from);
      return;
    }
    if (dtlType === "SEASON_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "SEASON_FINALIZE") {
      assertMSCAddress("payload.caller", payloadObj.caller || tx.from);
      return;
    }
    if (dtlType === "SEASON_CLAIM") {
      assertMSCAddress("payload.account", payloadObj.account || tx.from);
      return;
    }
    if (dtlType === "ORACLE_FEED_CREATE") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      if (Array.isArray(payloadObj.signers)) {
        payloadObj.signers.forEach((signer, idx) => {
          assertMSCAddress(`payload.signers[${idx}]`, signer);
        });
      }
      return;
    }
    if (dtlType === "ORACLE_PRICE_SUBMIT") {
      assertMSCAddress("payload.submitter", payloadObj.submitter || tx.from);
      return;
    }
  }

  function requireSignedDTLTx(tx) {
    if (!isFixedHex(tx.publicKey, 32)) {
      throw new Error("Invalid public key. Unlock signer and click 'Build + Sign Tx'.");
    }
    if (!isFixedHex(tx.signature, 64)) {
      throw new Error("Invalid signature. Click 'Build + Sign Tx' before submit.");
    }
  }

  function normalizePayloadForSigner(dtlType, payloadObj, from) {
    if (!payloadObj || typeof payloadObj !== "object" || !from) return payloadObj;
    if (dtlType === "TOKEN_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      if (Array.isArray(payloadObj.authority_signers)) {
        payloadObj.authority_signers = payloadObj.authority_signers.map((signer) =>
          isHexAddress(signer) ? from : signer
        );
      }
      return payloadObj;
    }
    if (dtlType === "TOKEN_TRANSFER" || dtlType === "TOKEN_BURN") {
      if (isHexAddress(payloadObj.from)) {
        payloadObj.from = from;
      }
      return payloadObj;
    }
    if (dtlType === "TOKEN_APPROVE") {
      if (isHexAddress(payloadObj.owner)) {
        payloadObj.owner = from;
      }
      return payloadObj;
    }
    if (dtlType === "TOKEN_TRANSFER_FROM") {
      if (isHexAddress(payloadObj.spender)) {
        payloadObj.spender = from;
      }
      return payloadObj;
    }
    if (dtlType === "TOKEN_MINT") {
      if (isHexAddress(payloadObj.proposer)) {
        payloadObj.proposer = from;
      }
      return payloadObj;
    }
    if (dtlType === "NFT721_CREATE" || dtlType === "NFT1155_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "NFT721_MINT" || dtlType === "NFT1155_MINT") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "NFT721_TRANSFER" || dtlType === "NFT1155_TRANSFER") {
      if (isHexAddress(payloadObj.from)) {
        payloadObj.from = from;
      }
      return payloadObj;
    }
    if (dtlType === "POOL_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "POOL_ADD_LIQUIDITY" || dtlType === "POOL_REMOVE_LIQUIDITY") {
      if (isHexAddress(payloadObj.provider)) {
        payloadObj.provider = from;
      }
      return payloadObj;
    }
    if (dtlType === "POOL_SWAP") {
      if (isHexAddress(payloadObj.trader)) {
        payloadObj.trader = from;
      }
      return payloadObj;
    }
    if (dtlType === "POOL_SWAP_ROUTE") {
      if (isHexAddress(payloadObj.trader)) {
        payloadObj.trader = from;
      }
      return payloadObj;
    }
    if (dtlType === "FARM_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "FARM_STAKE_LP" || dtlType === "FARM_UNSTAKE_LP" || dtlType === "FARM_CLAIM") {
      if (isHexAddress(payloadObj.account)) {
        payloadObj.account = from;
      }
      return payloadObj;
    }
    if (dtlType === "DUEL_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "DUEL_JOIN") {
      if (isHexAddress(payloadObj.joiner)) {
        payloadObj.joiner = from;
      }
      return payloadObj;
    }
    if (dtlType === "DUEL_REVEAL") {
      if (isHexAddress(payloadObj.player)) {
        payloadObj.player = from;
      }
      return payloadObj;
    }
    if (dtlType === "DUEL_FINALIZE") {
      if (isHexAddress(payloadObj.caller)) {
        payloadObj.caller = from;
      }
      return payloadObj;
    }
    if (dtlType === "LEND_MARKET_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "LEND_DEPOSIT_COLLATERAL" || dtlType === "LEND_BORROW" || dtlType === "LEND_REPAY" || dtlType === "LEND_WITHDRAW_COLLATERAL") {
      if (isHexAddress(payloadObj.account)) {
        payloadObj.account = from;
      }
      return payloadObj;
    }
    if (dtlType === "LEND_LIQUIDATE") {
      if (isHexAddress(payloadObj.liquidator)) {
        payloadObj.liquidator = from;
      }
      return payloadObj;
    }
    if (dtlType === "TOURNAMENT_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "TOURNAMENT_JOIN" || dtlType === "TOURNAMENT_REVEAL") {
      if (isHexAddress(payloadObj.player)) {
        payloadObj.player = from;
      }
      return payloadObj;
    }
    if (dtlType === "TOURNAMENT_FINALIZE") {
      if (isHexAddress(payloadObj.caller)) {
        payloadObj.caller = from;
      }
      return payloadObj;
    }
    if (dtlType === "SEASON_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "SEASON_FINALIZE") {
      if (isHexAddress(payloadObj.caller)) {
        payloadObj.caller = from;
      }
      return payloadObj;
    }
    if (dtlType === "SEASON_CLAIM") {
      if (isHexAddress(payloadObj.account)) {
        payloadObj.account = from;
      }
      return payloadObj;
    }
    if (dtlType === "ORACLE_FEED_CREATE") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      if (Array.isArray(payloadObj.signers)) {
        payloadObj.signers = payloadObj.signers.map((signer) =>
          isHexAddress(signer) ? from : signer
        );
      }
      return payloadObj;
    }
    if (dtlType === "ORACLE_PRICE_SUBMIT") {
      if (isHexAddress(payloadObj.submitter)) {
        payloadObj.submitter = from;
      }
      return payloadObj;
    }
		return payloadObj;
  }

  async function buildTxObject() {
    const dtlType = assertSupportedDTLTxType(String(els.dtlType.value || "").trim());
    if (!dtlType) throw new Error("DTL type missing");

    const nonce = Number.parseInt(els.txNonce.value, 10);
    const fee = Number.parseInt(els.txFee.value, 10);
    const ttlSec = Number.parseInt(els.txTtlSec.value, 10);
    if (!Number.isInteger(nonce) || nonce <= 0) throw new Error("nonce must be >= 1");
    if (!Number.isInteger(fee) || fee < 0) throw new Error("fee must be >= 0");
    if (!Number.isInteger(ttlSec) || ttlSec <= 0) throw new Error("ttl seconds must be > 0");

    const chainID = String(state.chainId || els.txChainId.value || "91938").trim();
    if (els.txChainId && String(els.txChainId.value || "").trim() !== chainID) {
      els.txChainId.value = chainID;
    }
    const coin = String(els.txCoin.value || "MSC").trim().toUpperCase();
    const signerAddress = String((state.wallet && state.wallet.address) || "").trim();
    let from = String(els.txFrom.value || "").trim();
    if (!from && signerAddress) {
      from = signerAddress;
      els.txFrom.value = from;
    }
    if (isHexAddress(from) && signerAddress) {
      from = signerAddress;
      els.txFrom.value = from;
    }
    if (!from) {
      throw new Error("from required");
    }
    updateTransferFromSpenderHint();
    let payloadObj = null;
    if (dtlType === "TOKEN_CREATE") {
      payloadObj = buildTokenCreatePayloadFromFields(from);
      els.txPayload.value = asPretty(payloadObj);
    } else if (dtlType === "TOKEN_TRANSFER") {
      payloadObj = buildTokenTransferPayloadFromFields(from);
      els.txPayload.value = asPretty(payloadObj);
    } else if (dtlType === "TOKEN_APPROVE") {
      payloadObj = buildTokenApprovePayloadFromFields(from);
      els.txPayload.value = asPretty(payloadObj);
    } else if (dtlType === "TOKEN_TRANSFER_FROM") {
      payloadObj = buildTokenTransferFromPayloadFromFields(from);
      els.txPayload.value = asPretty(payloadObj);
    } else if (dtlType === "TOKEN_MINT") {
      payloadObj = buildTokenMintPayloadFromFields(from);
      els.txPayload.value = asPretty(payloadObj);
    } else if (dtlType === "TOKEN_BURN") {
      payloadObj = buildTokenBurnPayloadFromFields(from);
      els.txPayload.value = asPretty(payloadObj);
    } else {
      if (shouldUseQuickPayloadFields(dtlType)) {
        payloadObj = buildPayloadFromQuickFields(dtlType);
        els.txPayload.value = asPretty(payloadObj);
      } else {
        payloadObj = parseJSONField(els.txPayload.value, defaultPayload(dtlType));
      }
    }
    normalizePayloadForSigner(dtlType, payloadObj, from);
    if (dtlType === "TOKEN_CREATE") {
      if (!String(payloadObj.name || "").trim()) throw new Error("Token name required");
      if (!String(payloadObj.symbol || "").trim()) throw new Error("Token symbol required");
      if (!Number.isInteger(Number(payloadObj.max_supply)) || Number(payloadObj.max_supply) <= 0) {
        throw new Error("max_supply must be > 0");
      }
    }
    if (dtlType === "TOKEN_TRANSFER") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("Transfer token_id required");
      if (!String(payloadObj.to || "").trim()) throw new Error("Transfer recipient required");
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error("Transfer amount must be > 0");
      }
    }
    if (dtlType === "TOKEN_APPROVE") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("Approve token_id required");
      if (!String(payloadObj.spender || "").trim()) throw new Error("Approve spender required");
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) < 0) {
        throw new Error("Approve amount must be >= 0");
      }
    }
    if (dtlType === "TOKEN_TRANSFER_FROM") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("TransferFrom token_id required");
      if (!String(payloadObj.from || "").trim()) throw new Error("TransferFrom from required");
      if (!String(payloadObj.to || "").trim()) throw new Error("TransferFrom recipient required");
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error("TransferFrom amount must be > 0");
      }
    }
    if (dtlType === "TOKEN_MINT") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("Mint token_id required");
      if (!String(payloadObj.to || "").trim()) throw new Error("Mint recipient required");
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error("Mint amount must be > 0");
      }
    }
    if (dtlType === "TOKEN_BURN") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("Burn token_id required");
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error("Burn amount must be > 0");
      }
    }
    if (dtlType === "NFT721_CREATE") {
      if (!String(payloadObj.name || "").trim()) throw new Error("NFT721 name required");
      if (!String(payloadObj.symbol || "").trim()) throw new Error("NFT721 symbol required");
    }
    if (dtlType === "NFT721_MINT") {
      if (!String(payloadObj.collection_id || "").trim()) throw new Error("NFT721_MINT collection_id required");
      if (!String(payloadObj.to || "").trim()) throw new Error("NFT721_MINT recipient required");
    }
    if (dtlType === "NFT721_TRANSFER") {
      if (!String(payloadObj.collection_id || "").trim()) throw new Error("NFT721_TRANSFER collection_id required");
      if (!String(payloadObj.to || "").trim()) throw new Error("NFT721_TRANSFER recipient required");
      if (!Number.isInteger(Number(payloadObj.token_id)) || Number(payloadObj.token_id) <= 0) {
        throw new Error("NFT721_TRANSFER token_id must be >= 1");
      }
    }
    if (dtlType === "NFT1155_CREATE") {
      if (!String(payloadObj.name || "").trim()) throw new Error("NFT1155 name required");
      if (!String(payloadObj.symbol || "").trim()) throw new Error("NFT1155 symbol required");
    }
    if (dtlType === "NFT1155_MINT") {
      if (!String(payloadObj.collection_id || "").trim()) throw new Error("NFT1155_MINT collection_id required");
      if (!String(payloadObj.to || "").trim()) throw new Error("NFT1155_MINT recipient required");
      if (!Number.isInteger(Number(payloadObj.token_id)) || Number(payloadObj.token_id) <= 0) {
        throw new Error("NFT1155_MINT token_id must be >= 1");
      }
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error("NFT1155_MINT amount must be > 0");
      }
    }
    if (dtlType === "NFT1155_TRANSFER") {
      if (!String(payloadObj.collection_id || "").trim()) throw new Error("NFT1155_TRANSFER collection_id required");
      if (!String(payloadObj.to || "").trim()) throw new Error("NFT1155_TRANSFER recipient required");
      if (!Number.isInteger(Number(payloadObj.token_id)) || Number(payloadObj.token_id) <= 0) {
        throw new Error("NFT1155_TRANSFER token_id must be >= 1");
      }
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error("NFT1155_TRANSFER amount must be > 0");
      }
    }
    if (dtlType === "POOL_CREATE") {
      if (!String(payloadObj.token_a || "").trim() || !String(payloadObj.token_b || "").trim()) {
        throw new Error("POOL_CREATE requires token_a and token_b");
      }
      if (!Number.isInteger(Number(payloadObj.amount_a)) || Number(payloadObj.amount_a) <= 0) {
        throw new Error("POOL_CREATE amount_a must be > 0");
      }
      if (!Number.isInteger(Number(payloadObj.amount_b)) || Number(payloadObj.amount_b) <= 0) {
        throw new Error("POOL_CREATE amount_b must be > 0");
      }
    }
    if (dtlType === "POOL_ADD_LIQUIDITY") {
      if (!String(payloadObj.pool_id || "").trim()) throw new Error("POOL_ADD_LIQUIDITY requires pool_id");
      if (!Number.isInteger(Number(payloadObj.amount_a)) || Number(payloadObj.amount_a) <= 0) {
        throw new Error("POOL_ADD_LIQUIDITY amount_a must be > 0");
      }
      if (!Number.isInteger(Number(payloadObj.amount_b)) || Number(payloadObj.amount_b) <= 0) {
        throw new Error("POOL_ADD_LIQUIDITY amount_b must be > 0");
      }
    }
    if (dtlType === "POOL_REMOVE_LIQUIDITY") {
      if (!String(payloadObj.pool_id || "").trim()) throw new Error("POOL_REMOVE_LIQUIDITY requires pool_id");
      if (!Number.isInteger(Number(payloadObj.lp_shares)) || Number(payloadObj.lp_shares) <= 0) {
        throw new Error("POOL_REMOVE_LIQUIDITY lp_shares must be > 0");
      }
    }
    if (dtlType === "POOL_SWAP") {
      if (!String(payloadObj.pool_id || "").trim()) throw new Error("POOL_SWAP requires pool_id");
      if (!String(payloadObj.token_in || "").trim()) throw new Error("POOL_SWAP requires token_in");
      if (!Number.isInteger(Number(payloadObj.amount_in)) || Number(payloadObj.amount_in) <= 0) {
        throw new Error("POOL_SWAP amount_in must be > 0");
      }
    }
    if (dtlType === "POOL_SWAP_ROUTE") {
      if (!String(payloadObj.token_in || "").trim()) throw new Error("POOL_SWAP_ROUTE requires token_in");
      if (!Number.isInteger(Number(payloadObj.amount_in)) || Number(payloadObj.amount_in) <= 0) {
        throw new Error("POOL_SWAP_ROUTE amount_in must be > 0");
      }
      if (!Array.isArray(payloadObj.path) || payloadObj.path.length < 1) {
        throw new Error("POOL_SWAP_ROUTE requires path (at least 1 pool_id)");
      }
      for (let i = 0; i < payloadObj.path.length; i++) {
        if (!String(payloadObj.path[i] || "").trim()) {
          throw new Error(`POOL_SWAP_ROUTE path[${i}] must be non-empty`);
        }
      }
      if (!Number.isInteger(Number(payloadObj.deadline_height)) || Number(payloadObj.deadline_height) <= 0) {
        throw new Error("POOL_SWAP_ROUTE deadline_height must be > 0");
      }
    }
    if (dtlType === "FARM_CREATE") {
      if (!String(payloadObj.pool_id || "").trim()) throw new Error("FARM_CREATE requires pool_id");
      if (payloadObj.multiplier_bps !== undefined && payloadObj.multiplier_bps !== null) {
        if (!Number.isInteger(Number(payloadObj.multiplier_bps)) || Number(payloadObj.multiplier_bps) < 0) {
          throw new Error("FARM_CREATE multiplier_bps must be >= 0");
        }
      }
    }
    if (dtlType === "FARM_STAKE_LP" || dtlType === "FARM_UNSTAKE_LP") {
      if (!String(payloadObj.farm_id || "").trim()) throw new Error(`${dtlType} requires farm_id`);
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error(`${dtlType} amount must be > 0`);
      }
    }
    if (dtlType === "FARM_CLAIM") {
      if (!String(payloadObj.farm_id || "").trim()) throw new Error("FARM_CLAIM requires farm_id");
    }
    if (dtlType === "DUEL_CREATE") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("DUEL_CREATE requires token_id");
      if (!String(payloadObj.commit_hash || "").trim()) throw new Error("DUEL_CREATE requires commit_hash");
      if (!Number.isInteger(Number(payloadObj.stake)) || Number(payloadObj.stake) <= 0) {
        throw new Error("DUEL_CREATE stake must be > 0");
      }
    }
    if (dtlType === "DUEL_JOIN") {
      if (!String(payloadObj.duel_id || "").trim()) throw new Error("DUEL_JOIN requires duel_id");
      if (!String(payloadObj.commit_hash || "").trim()) throw new Error("DUEL_JOIN requires commit_hash");
    }
    if (dtlType === "DUEL_REVEAL") {
      if (!String(payloadObj.duel_id || "").trim()) throw new Error("DUEL_REVEAL requires duel_id");
      if (!String(payloadObj.secret || "").trim()) throw new Error("DUEL_REVEAL requires secret");
    }
    if (dtlType === "DUEL_FINALIZE") {
      if (!String(payloadObj.duel_id || "").trim()) throw new Error("DUEL_FINALIZE requires duel_id");
    }
    if (dtlType === "LEND_MARKET_CREATE") {
      if (!String(payloadObj.collateral_token_id || "").trim() || !String(payloadObj.debt_token_id || "").trim()) {
        throw new Error("LEND_MARKET_CREATE requires collateral_token_id and debt_token_id");
      }
      if (!Number.isInteger(Number(payloadObj.debt_liquidity)) || Number(payloadObj.debt_liquidity) <= 0) {
        throw new Error("LEND_MARKET_CREATE debt_liquidity must be > 0");
      }
    }
    if (dtlType === "LEND_DEPOSIT_COLLATERAL" || dtlType === "LEND_BORROW" || dtlType === "LEND_REPAY" || dtlType === "LEND_WITHDRAW_COLLATERAL") {
      if (!String(payloadObj.market_id || "").trim()) throw new Error(`${dtlType} requires market_id`);
      if (!Number.isInteger(Number(payloadObj.amount)) || Number(payloadObj.amount) <= 0) {
        throw new Error(`${dtlType} amount must be > 0`);
      }
    }
    if (dtlType === "LEND_LIQUIDATE") {
      if (!String(payloadObj.market_id || "").trim()) throw new Error("LEND_LIQUIDATE requires market_id");
      if (!String(payloadObj.borrower || "").trim()) throw new Error("LEND_LIQUIDATE requires borrower");
      if (!Number.isInteger(Number(payloadObj.repay_amount)) || Number(payloadObj.repay_amount) <= 0) {
        throw new Error("LEND_LIQUIDATE repay_amount must be > 0");
      }
    }
    if (dtlType === "TOURNAMENT_CREATE") {
      if (!String(payloadObj.token_id || "").trim()) throw new Error("TOURNAMENT_CREATE requires token_id");
      if (!Number.isInteger(Number(payloadObj.entry_fee)) || Number(payloadObj.entry_fee) <= 0) {
        throw new Error("TOURNAMENT_CREATE entry_fee must be > 0");
      }
      if (!Number.isInteger(Number(payloadObj.max_players)) || Number(payloadObj.max_players) < 2) {
        throw new Error("TOURNAMENT_CREATE max_players must be >= 2");
      }
    }
    if (dtlType === "TOURNAMENT_JOIN") {
      if (!String(payloadObj.tournament_id || "").trim()) throw new Error("TOURNAMENT_JOIN requires tournament_id");
      if (!String(payloadObj.commit_hash || "").trim()) throw new Error("TOURNAMENT_JOIN requires commit_hash");
    }
    if (dtlType === "TOURNAMENT_REVEAL") {
      if (!String(payloadObj.tournament_id || "").trim()) throw new Error("TOURNAMENT_REVEAL requires tournament_id");
      if (!String(payloadObj.secret || "").trim()) throw new Error("TOURNAMENT_REVEAL requires secret");
    }
    if (dtlType === "TOURNAMENT_FINALIZE") {
      if (!String(payloadObj.tournament_id || "").trim()) throw new Error("TOURNAMENT_FINALIZE requires tournament_id");
    }
    if (dtlType === "SEASON_CREATE") {
      if (payloadObj.start_height !== undefined && payloadObj.start_height !== null && String(payloadObj.start_height).trim() !== "") {
        if (!Number.isInteger(Number(payloadObj.start_height)) || Number(payloadObj.start_height) < 0) {
          throw new Error("SEASON_CREATE start_height must be >= 0");
        }
      }
    }
    if (dtlType === "SEASON_FINALIZE" || dtlType === "SEASON_CLAIM") {
      if (!String(payloadObj.season_id || "").trim()) throw new Error(`${dtlType} requires season_id`);
    }
    if (dtlType === "ORACLE_FEED_CREATE") {
      if (!String(payloadObj.base_token_id || "").trim() || !String(payloadObj.quote_token_id || "").trim()) {
        throw new Error("ORACLE_FEED_CREATE requires base_token_id and quote_token_id");
      }
      if (!Array.isArray(payloadObj.signers) || payloadObj.signers.length < 1) {
        throw new Error("ORACLE_FEED_CREATE requires signers");
      }
      if (!Number.isInteger(Number(payloadObj.threshold)) || Number(payloadObj.threshold) <= 0) {
        throw new Error("ORACLE_FEED_CREATE threshold must be > 0");
      }
      if (!Number.isInteger(Number(payloadObj.decimals)) || Number(payloadObj.decimals) < 0 || Number(payloadObj.decimals) > 18) {
        throw new Error("ORACLE_FEED_CREATE decimals must be between 0 and 18");
      }
    }
    if (dtlType === "ORACLE_PRICE_SUBMIT") {
      if (!String(payloadObj.feed_id || "").trim()) throw new Error("ORACLE_PRICE_SUBMIT requires feed_id");
      if (!Number.isInteger(Number(payloadObj.price)) || Number(payloadObj.price) <= 0) {
        throw new Error("ORACLE_PRICE_SUBMIT price must be > 0");
      }
    }
    const normalizedPayloadStr = JSON.stringify(payloadObj);

    const tx = {
      id: String(els.txId.value || "").trim(),
      from,
      to: String(els.txTo.value || "").trim(),
      amount: 1,
      nonce,
      publicKey: String(els.txPublicKey.value || "").trim(),
      signature: String(els.txSignature.value || "").trim(),
      fee,
      expiry: Math.floor(Date.now() / 1000) + ttlSec,
      dtl_tx_type: dtlType,
      dtl_token_id: String(els.txTokenId.value || payloadObj.token_id || "").trim(),
      dtl_payload: normalizedPayloadStr,
      ChainID: chainID,
      Coin: coin,
      Type: 8,
    };

    if (dtlType === "TOKEN_MINT") {
      const certObj = await buildMintGovernanceCertFromFields(payloadObj);
      els.txGovCert.value = asPretty(certObj);
      tx.dtl_governance_cert = JSON.stringify(certObj);
      applyMintCertDetailsFromCert(certObj);
    }

    validateDTLAddressFields(dtlType, tx, payloadObj);

    state.lastBuiltTx = tx;
    els.txObject.value = asPretty(tx);
    setPayloadPreview(payloadObj);
    if (dtlType === "TOKEN_CREATE") {
      applyTokenCreateDetailsFromPayload(payloadObj);
    } else if (dtlType === "TOKEN_TRANSFER") {
      applyTokenTransferDetailsFromPayload(payloadObj);
    } else if (dtlType === "TOKEN_APPROVE") {
      applyTokenApproveDetailsFromPayload(payloadObj);
    } else if (dtlType === "TOKEN_TRANSFER_FROM") {
      applyTokenTransferFromDetailsFromPayload(payloadObj);
    } else if (dtlType === "TOKEN_MINT") {
      applyTokenMintDetailsFromPayload(payloadObj);
    } else if (dtlType === "TOKEN_BURN") {
      applyTokenBurnDetailsFromPayload(payloadObj);
    }
    scheduleDraftSave();
    return tx;
  }

  function applyWalletToForm() {
    if (!state.wallet) return;
    const walletAddress = state.wallet.address || "";
    if (els.signerAddress) {
      els.signerAddress.value = walletAddress;
    }
    const currentFrom = String(els.txFrom.value || "").trim();
    if (!currentFrom || isHexAddress(currentFrom)) {
      els.txFrom.value = walletAddress;
    }
    if (!String(els.txPublicKey.value || "").trim()) {
      els.txPublicKey.value = state.wallet.publicKey || "";
    }
    if (els.tcAuthoritySigners && !String(els.tcAuthoritySigners.value || "").trim() && walletAddress) {
      els.tcAuthoritySigners.value = walletAddress;
    }
    if (els.tmSigners && !String(els.tmSigners.value || "").trim() && walletAddress) {
      els.tmSigners.value = walletAddress;
    }
  }

  function lockSigner() {
    state.secretKey = null;
    setSignerState("Locked", false);
  }

  function loadSignerWallet() {
    const wallet = loadStoredWallet();
    if (!wallet) {
      throw new Error(
        "Local wallet not found (msc_wallet_browser_v1). Open Wallet app on same host (127.0.0.1 vs localhost), then create/import wallet."
      );
    }
    state.wallet = wallet;
    applyWalletToForm();
    if (state.secretKey) {
      setSignerState("Unlocked", true);
    } else {
      setSignerState("Loaded", true);
    }
  }

  async function unlockSignerWallet() {
    if (!state.wallet) {
      loadSignerWallet();
    }
    const password = String(els.signerPassword.value || "").trim();
    if (!password) throw new Error("Wallet password required");
    const cryptoData = state.wallet && state.wallet.crypto;
    if (!cryptoData) throw new Error("Wallet crypto data missing");
    const secret = await decryptSecretKey(cryptoData, password);
    if (secret.length !== 64) throw new Error("Invalid secret key length");
    state.secretKey = secret;
    els.signerPassword.value = "";
    applyWalletToForm();
    setSignerState("Unlocked", true);
  }

  async function signCurrentTx() {
    if (!state.secretKey) throw new Error("Signer locked");
    const tx = await buildTxObject();
    const signerAddress = String((state.wallet && state.wallet.address) || "").trim();
    if (signerAddress && String(tx.from || "").trim() !== signerAddress) {
      throw new Error(`From must match unlocked wallet: ${signerAddress}`);
    }
    if (!tx.publicKey && state.wallet && state.wallet.publicKey) {
      tx.publicKey = state.wallet.publicKey;
    }
    const pkHex = normalizeFixedHex(tx.publicKey);
    if (!isFixedHex(pkHex, 32)) {
      throw new Error("Invalid public key. Load local wallet and unlock signer.");
    }
    if (typeof nacl === "undefined" || !nacl.sign || !nacl.sign.detached) {
      throw new Error("nacl signer not available");
    }
    const payload = buildTxPayload(tx);
    const signature = nacl.sign.detached(payload, state.secretKey);
    const txID = await sha256(payload);
    tx.signature = bytesToHex(signature);
    tx.id = bytesToHex(txID);
    tx.publicKey = pkHex;
    els.txSignature.value = tx.signature;
    els.txId.value = tx.id;
    if (tx.publicKey) {
      els.txPublicKey.value = tx.publicKey;
    }
    state.lastBuiltTx = tx;
    els.txObject.value = asPretty(tx);
    els.txOut.textContent = asPretty({ status: "signed", tx_id: tx.id });
  }

  async function connect() {
    const rawBase = normalizeRpcBase(els.rpcBase.value);
    if (!rawBase) throw new Error("RPC base URL required");
    const chainId = await rpc("dtl_chainId", []);
    state.rpcBase = rawBase;
    state.apiToken = normalizeToken(els.apiToken.value);
    state.chainId = String(chainId || "");
    localStorage.setItem("msc_dtl_rpc", state.rpcBase);
    localStorage.setItem("msc_dtl_token", state.apiToken);
    localStorage.setItem("msc_dtl_chain", state.chainId);
    els.txChainId.value = state.chainId || "91938";
    setChainMeta(state.chainId || "-", "ok");
    await refreshCompatMode();
    try {
      await syncRefSuggestions();
    } catch (_) {
      // Ref sync is best-effort; keep connect success even if reads fail.
    }
    setConnState("Connected", true);
    scheduleDraftSave();
  }

  async function checkChainId() {
    const cid = await rpc("dtl_chainId", []);
    setChainMeta(cid || "-", "ok");
    await refreshCompatMode();
  }

  async function refreshCompatMode() {
    state.contractRuntimeRemoved = true;
    renderCompatMode();
  }



  async function readTokenInfo() {
    const tokenRef = String(els.tokenRef.value || "").trim();
    if (!tokenRef) throw new Error("token ref required");
    const out = await rpc("dtl_tokenInfo", [tokenRef]);
    els.readOut.textContent = asPretty(out);
  }

  async function readBalance() {
    const tokenRef = String(els.tokenRef.value || "").trim();
    const account = String(els.accountRef.value || "").trim();
    if (!tokenRef || !account) throw new Error("token ref and account required");
    const out = await rpc("dtl_balanceOf", [tokenRef, account]);
    els.readOut.textContent = asPretty({ token: tokenRef, account, balance_hex: out });
  }

  async function readTotalSupply() {
    const tokenRef = String(els.tokenRef.value || "").trim();
    if (!tokenRef) throw new Error("token ref required");
    const out = await rpc("dtl_totalSupply", [tokenRef]);
    els.readOut.textContent = asPretty({ token: tokenRef, total_supply_hex: out });
  }

  async function readListTokens() {
    const account = String((els.accountRef && els.accountRef.value) || "").trim();
    const params = account ? [account] : [];
    const out = await rpc("dtl_listTokens", params);
    state.refSuggestions.tokens = uniqueSorted([
      ...state.refSuggestions.tokens,
      ...collectTokenRefsFromRows(out),
    ]);
    applyRefSuggestionsToInputs();
    els.readOut.textContent = asPretty(out);
  }

  async function readPoolInfo() {
    const poolRef = String((els.poolRef && els.poolRef.value) || "").trim();
    if (!poolRef) throw new Error("pool ref required");
    const out = await rpc("dtl_poolInfo", [poolRef]);
    els.readOut.textContent = asPretty(out);
  }

  async function readListPools() {
    const out = await rpc("dtl_listPools", []);
    state.refSuggestions.pools = uniqueSorted([
      ...state.refSuggestions.pools,
      ...collectPoolRefsFromRows(out),
    ]);
    applyRefSuggestionsToInputs();
    els.readOut.textContent = asPretty(out);
  }

  async function readRouteQuote() {
    const tokenIn = String((els.tokenRef && els.tokenRef.value) || "").trim();
    const tokenOut = String((els.routeTokenOut && els.routeTokenOut.value) || "").trim();
    const amountIn = Number.parseInt(String((els.routeAmountIn && els.routeAmountIn.value) || ""), 10);
    if (!tokenIn) throw new Error("token_in required (Token Ref)");
    if (!tokenOut) throw new Error("route token_out required");
    if (!Number.isInteger(amountIn) || amountIn <= 0) throw new Error("route amount_in must be > 0");
    const rawMaxHops = String((els.routeMaxHops && els.routeMaxHops.value) || "").trim();
    const params = [tokenIn, tokenOut, amountIn];
    if (rawMaxHops) {
      const maxHops = Number.parseInt(rawMaxHops, 10);
      if (!Number.isInteger(maxHops) || maxHops <= 0) {
        throw new Error("route max_hops must be > 0");
      }
      params.push(maxHops);
    }
    const out = await rpc("dtl_routeQuote", params);
    els.readOut.textContent = asPretty(out);
  }

  async function readFarmInfo() {
    const farmRef = String((els.farmRef && els.farmRef.value) || "").trim();
    if (!farmRef) throw new Error("farm ref required");
    const out = await rpc("dtl_farmInfo", [farmRef]);
    els.readOut.textContent = asPretty(out);
  }

  async function readFarmPosition() {
    const farmRef = String((els.farmRef && els.farmRef.value) || "").trim();
    const account = String((els.accountRef && els.accountRef.value) || "").trim();
    if (!farmRef) throw new Error("farm ref required");
    if (!account) throw new Error("account required");
    const out = await rpc("dtl_positionFarm", [farmRef, account]);
    els.readOut.textContent = asPretty(out);
  }

  async function readSeasonInfo() {
    const seasonRef = String((els.seasonRef && els.seasonRef.value) || "").trim();
    const params = seasonRef ? [seasonRef] : [];
    const out = await rpc("dtl_seasonInfo", params);
    els.readOut.textContent = asPretty(out);
  }

  async function readLeaderboard() {
    const seasonRef = String((els.seasonRef && els.seasonRef.value) || "").trim();
    const rawLimit = String((els.leaderboardLimit && els.leaderboardLimit.value) || "").trim();
    let limit = 20;
    if (rawLimit) {
      const parsed = Number.parseInt(rawLimit, 10);
      if (!Number.isInteger(parsed) || parsed <= 0) {
        throw new Error("leaderboard limit must be > 0");
      }
      limit = parsed;
    }
    const out = await rpc("dtl_leaderboard", [seasonRef, limit]);
    els.readOut.textContent = asPretty(out);
  }

  function captureDraft() {
    const draft = {};
    for (const id of draftFieldIDs) {
      const el = $(id);
      if (!el) continue;
      if (el.type === "checkbox") {
        draft[id] = el.checked ? "1" : "0";
      } else {
        draft[id] = el.value;
      }
    }
    return draft;
  }

  function saveDraftNow() {
    try {
      localStorage.setItem(DRAFT_KEY, JSON.stringify(captureDraft()));
    } catch (_) {
      // Ignore quota/write failures.
    }
  }

  function scheduleDraftSave() {
    if (draftSaveTimer) {
      clearTimeout(draftSaveTimer);
    }
    draftSaveTimer = setTimeout(() => {
      draftSaveTimer = 0;
      saveDraftNow();
    }, 180);
  }

  function restoreDraft() {
    const raw = String(localStorage.getItem(DRAFT_KEY) || "").trim();
    if (!raw) return false;
    let parsed = null;
    try {
      parsed = JSON.parse(raw);
    } catch (_) {
      return false;
    }
    if (!parsed || typeof parsed !== "object") return false;
    for (const id of draftFieldIDs) {
      const el = $(id);
      if (!el || !Object.prototype.hasOwnProperty.call(parsed, id)) continue;
      const value = parsed[id];
      if (typeof value === "string") {
        if (el.type === "checkbox") {
          const normalized = value.trim().toLowerCase();
          el.checked = normalized === "1" || normalized === "true" || normalized === "on" || normalized === "yes";
        } else {
          el.value = value;
        }
      }
    }
    return true;
  }

  function bindDraftPersistence() {
    for (const id of draftFieldIDs) {
      const el = $(id);
      if (!el) continue;
      el.addEventListener("input", scheduleDraftSave);
      el.addEventListener("change", scheduleDraftSave);
    }
  }

  function clearDraft() {
    if (draftSaveTimer) {
      clearTimeout(draftSaveTimer);
      draftSaveTimer = 0;
    }
    localStorage.removeItem(DRAFT_KEY);
    els.txOut.textContent = "Draft cleared.";
  }

  async function loadPayloadTemplate() {
    const kind = assertSupportedDTLTxType(String(els.dtlType.value || "").trim());
    const payload = defaultPayload(kind);
    if (kind === "TOKEN_CREATE") {
      applyTokenCreateDetailsFromPayload(payload);
      if (els.txGovCert) els.txGovCert.value = "";
    } else if (kind === "TOKEN_TRANSFER") {
      applyTokenTransferDetailsFromPayload(payload);
      if (els.txGovCert) els.txGovCert.value = "";
    } else if (kind === "TOKEN_APPROVE") {
      applyTokenApproveDetailsFromPayload(payload);
      if (els.txGovCert) els.txGovCert.value = "";
    } else if (kind === "TOKEN_TRANSFER_FROM") {
      applyTokenTransferFromDetailsFromPayload(payload);
      if (els.txGovCert) els.txGovCert.value = "";
    } else if (kind === "TOKEN_MINT") {
      applyTokenMintDetailsFromPayload(payload);
      const certObj = await buildMintGovernanceCertFromFields(payload);
      if (els.txGovCert) els.txGovCert.value = asPretty(certObj);
      applyMintCertDetailsFromCert(certObj);
    } else if (kind === "TOKEN_BURN") {
      applyTokenBurnDetailsFromPayload(payload);
      if (els.txGovCert) els.txGovCert.value = "";
    } else {
      if (els.txGovCert) els.txGovCert.value = "";
    }
		els.txPayload.value = asPretty(payload);
		setPayloadPreview(payload);
		scheduleDraftSave();
  }

  async function loadTxPreset(kind, label) {
    const normalized = String(kind || "").trim().toUpperCase();
    if (!normalized) throw new Error("preset tx type missing");
    assertSupportedDTLTxType(normalized);
    if (els.dtlType) {
      els.dtlType.value = normalized;
      syncGuidedVisibility();
      refreshWorkspaceMeta();
    }
    await loadPayloadTemplate();
    if (els.txOut) {
      els.txOut.textContent = asPretty({
        status: "preset_loaded",
        tx_type: normalized,
        label: String(label || normalized),
      });
    }
  }

  async function loadNextNonce() {
    const from = String(els.txFrom.value || "").trim();
    if (!from) throw new Error("from required");
    if (isHexAddress(from)) {
      throw new Error("From must be MSC address (MSC...), not 0x alias");
    }
    const out = await getJSON(`/nonce/pending?address=${encodeURIComponent(from)}`);
    if (!out || typeof out.nonce !== "number") throw new Error("invalid nonce response");
    els.txNonce.value = String(out.nonce);
  }

  function extractExpectedNonce(rawMessage) {
    const msg = String(rawMessage || "").trim();
    if (!msg) return 0;
    const patterns = [
      /invalid nonce:\s*got\s*\d+\s*expected\s*(\d+)/i,
      /invalid nonce[^\\n]*expected\s+(\d+)/i,
      /\bexpected\s+(\d+)\b/i,
    ];
    for (const re of patterns) {
      const match = msg.match(re);
      if (!match) continue;
      const parsed = Number.parseInt(match[1], 10);
      if (Number.isInteger(parsed) && parsed > 0) return parsed;
    }
    return 0;
  }

  function syncNonceFromError(rawMessage) {
    const expected = extractExpectedNonce(rawMessage);
    if (expected > 0) {
      els.txNonce.value = String(expected);
    }
    return expected;
  }

  async function submitWithNonceRetry(sendOnce) {
    try {
      return await sendOnce(false);
    } catch (err) {
      const msg = err && err.message ? String(err.message) : String(err || "");
      const expected = syncNonceFromError(msg);
      if (!expected) throw err;
      if (!state.secretKey) {
        throw new Error(
          `Invalid nonce: expected ${expected}. Click Build + Sign Tx, then submit again.`
        );
      }
      return sendOnce(true);
    }
  }

  async function computeCanonicalTxID(tx) {
    if (!tx || typeof tx !== "object") return "";
    const payload = buildTxPayload(tx);
    const sum = await sha256(payload);
    return bytesToHex(sum);
  }

  async function syncTxIDWithCanonicalPayload(tx) {
    if (!tx || typeof tx !== "object") return "";
    const expectedID = await computeCanonicalTxID(tx);
    if (!expectedID) return "";
    if (String(tx.id || "").trim().toLowerCase() !== expectedID) {
      tx.id = expectedID;
      if (els.txId) {
        els.txId.value = expectedID;
      }
      if (state.lastBuiltTx && typeof state.lastBuiltTx === "object") {
        state.lastBuiltTx.id = expectedID;
      }
    }
    return expectedID;
  }

  async function ensureSignedForSubmit(tx) {
    if (state.secretKey) {
      await signCurrentTx();
      return state.lastBuiltTx || tx;
    }
    await syncTxIDWithCanonicalPayload(tx);
    requireSignedDTLTx(tx);
    return tx;
  }


  async function submitDTL() {
    await submitWithNonceRetry(async (retried) => {
      const builtTx = await buildTxObject();
      const tx = await ensureSignedForSubmit(builtTx);
      assertSupportedDTLTxType(tx.dtl_tx_type);
      requireSignedDTLTx(tx);
      const txID = await rpc("dtl_submit", [tx], true);
      if (els.statusTxId) {
        els.statusTxId.value = String(txID || "").trim();
      }
      const signedTxID = String(tx.id || "").trim();
      const acceptedTxID = String(txID || signedTxID || "").trim();
      if (acceptedTxID) {
        rememberLastTxID(acceptedTxID);
      }
      const result = {
        status: "accepted",
        via: "dtl_submit",
        tx_id: txID,
      };
      if (signedTxID && txID && signedTxID.toLowerCase() !== String(txID).toLowerCase()) {
        result.signed_tx_id = signedTxID;
      }
      if (retried) {
        result.nonce_synced = true;
        result.nonce = Number.parseInt(String(els.txNonce.value || "0"), 10) || null;
      }
      els.txOut.textContent = asPretty(result);
      scheduleDraftSave();
      return result;
    });
  }



  async function submitRaw() {
    await submitWithNonceRetry(async (retried) => {
      const builtTx = await buildTxObject();
      const tx = await ensureSignedForSubmit(builtTx);
      assertSupportedDTLTxType(tx.dtl_tx_type);
      requireSignedDTLTx(tx);
      if (tx && tx.id) {
        rememberLastTxID(String(tx.id || "").trim());
      }
      const res = await fetch(endpoint("/submitTx"), {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify(tx),
      });
      const text = await res.text();
      if (!res.ok) throw new Error(`HTTP ${res.status} ${text}`);
      let result = text;
      try {
        result = JSON.parse(text);
        if (result && result.tx_id) {
          els.statusTxId.value = result.tx_id;
          rememberLastTxID(String(result.tx_id || "").trim());
        }
        if (retried && result && typeof result === "object") {
          result.nonce_synced = true;
          result.nonce = Number.parseInt(String(els.txNonce.value || "0"), 10) || null;
        }
      } catch (_) {
        // Keep non-JSON response text.
      }
      if (retried && typeof result === "string") {
        result = `${result}\n(nonce synced to ${String(els.txNonce.value || "").trim()} and retried)`;
      }
      els.txOut.textContent = typeof result === "string" ? result : asPretty(result);
      scheduleDraftSave();
      return result;
    });
  }



  function applyTokenDetailsToPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_CREATE") {
      throw new Error("Guided token details are only for TOKEN_CREATE");
    }
    const payload = buildTokenCreatePayloadFromFields(String(els.txFrom.value || "").trim());
    els.txPayload.value = asPretty(payload);
    setPayloadPreview(payload);
  }

  function readTokenDetailsFromPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_CREATE") {
      throw new Error("Switch DTL type to TOKEN_CREATE first");
    }
    const payload = parseJSONField(els.txPayload.value, defaultPayload(kind));
    applyTokenCreateDetailsFromPayload(payload);
    setPayloadPreview(payload);
  }

  function applyTransferDetailsToPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_TRANSFER") {
      throw new Error("Switch DTL type to TOKEN_TRANSFER first");
    }
    const payload = buildTokenTransferPayloadFromFields(String(els.txFrom.value || "").trim());
    els.txPayload.value = asPretty(payload);
    if (els.txGovCert) els.txGovCert.value = "";
    setPayloadPreview(payload);
  }

  function readTransferDetailsFromPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_TRANSFER") {
      throw new Error("Switch DTL type to TOKEN_TRANSFER first");
    }
    const payload = parseJSONField(els.txPayload.value, defaultPayload(kind));
    applyTokenTransferDetailsFromPayload(payload);
    setPayloadPreview(payload);
  }

  function applyApproveDetailsToPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_APPROVE") {
      throw new Error("Switch DTL type to TOKEN_APPROVE first");
    }
    const payload = buildTokenApprovePayloadFromFields(String(els.txFrom.value || "").trim());
    els.txPayload.value = asPretty(payload);
    if (els.txGovCert) els.txGovCert.value = "";
    setPayloadPreview(payload);
  }

  function readApproveDetailsFromPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_APPROVE") {
      throw new Error("Switch DTL type to TOKEN_APPROVE first");
    }
    const payload = parseJSONField(els.txPayload.value, defaultPayload(kind));
    applyTokenApproveDetailsFromPayload(payload);
    setPayloadPreview(payload);
  }

  function applyTransferFromDetailsToPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_TRANSFER_FROM") {
      throw new Error("Switch DTL type to TOKEN_TRANSFER_FROM first");
    }
    const payload = buildTokenTransferFromPayloadFromFields(String(els.txFrom.value || "").trim());
    els.txPayload.value = asPretty(payload);
    if (els.txGovCert) els.txGovCert.value = "";
    setPayloadPreview(payload);
  }

  function readTransferFromDetailsFromPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_TRANSFER_FROM") {
      throw new Error("Switch DTL type to TOKEN_TRANSFER_FROM first");
    }
    const payload = parseJSONField(els.txPayload.value, defaultPayload(kind));
    applyTokenTransferFromDetailsFromPayload(payload);
    setPayloadPreview(payload);
  }

  async function applyMintDetailsToPayloadAndCert() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_MINT") {
      throw new Error("Switch DTL type to TOKEN_MINT first");
    }
    const payload = buildTokenMintPayloadFromFields(String(els.txFrom.value || "").trim());
    const certObj = await buildMintGovernanceCertFromFields(payload);
    els.txPayload.value = asPretty(payload);
    if (els.txGovCert) els.txGovCert.value = asPretty(certObj);
    setPayloadPreview(payload);
  }

  function readMintDetailsFromPayloadAndCert() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_MINT") {
      throw new Error("Switch DTL type to TOKEN_MINT first");
    }
    const payload = parseJSONField(els.txPayload.value, defaultPayload(kind));
    applyTokenMintDetailsFromPayload(payload);
    setPayloadPreview(payload);
    const certRaw = String(els.txGovCert.value || "").trim();
    if (certRaw) {
      const certObj = parseJSONField(certRaw, {});
      applyMintCertDetailsFromCert(certObj);
    }
  }

  function applyBurnDetailsToPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_BURN") {
      throw new Error("Switch DTL type to TOKEN_BURN first");
    }
    const payload = buildTokenBurnPayloadFromFields(String(els.txFrom.value || "").trim());
    els.txPayload.value = asPretty(payload);
    if (els.txGovCert) els.txGovCert.value = "";
    setPayloadPreview(payload);
  }

  function readBurnDetailsFromPayload() {
    const kind = String(els.dtlType.value || "").trim();
    if (kind !== "TOKEN_BURN") {
      throw new Error("Switch DTL type to TOKEN_BURN first");
    }
    const payload = parseJSONField(els.txPayload.value, defaultPayload(kind));
    applyTokenBurnDetailsFromPayload(payload);
    setPayloadPreview(payload);
  }

  function friendlyErrorMessage(rawMessage) {
    const msg = String(rawMessage || "").trim();
    if (!msg) return "Unknown error";
    if (/dtl contract runtime removed/i.test(msg)) {
      return "Contract tx disabled hai: CONTRACT_DEPLOY/CONTRACT_CALL supported nahi hain.";
    }
    if (/tx id mismatch/i.test(msg)) {
      const liveChain = String(state.chainId || "").trim();
      const fieldChain = String(els.txChainId && els.txChainId.value ? els.txChainId.value : "").trim();
      if (liveChain && fieldChain && liveChain !== fieldChain) {
        return `Tx ID mismatch: chain mismatch detected (connected=${liveChain}, tx=${fieldChain}). Connect karo, phir Build + Sign Tx dubara karo.`;
      }
      if (state.secretKey) {
        return "Tx ID mismatch: payload/signature stale thi. IDE ne tx_id sync kar diya; Build + Sign Tx karke submit dubara karo.";
      }
      return "Tx ID mismatch: transaction fields sign ke baad change hue hain ya chain/coin mismatch hai. Connect karo, Build + Sign Tx karo, phir submit karo.";
    }
    const expected = extractExpectedNonce(msg);
    if (expected > 0) {
      if (state.secretKey) {
        return `Invalid nonce detected (expected ${expected}). IDE auto-synced nonce; submit again if needed.`;
      }
      return `Invalid nonce: expected ${expected}. Click Load Next Nonce, then Build + Sign Tx and submit again.`;
    }
    if (/insufficient balance/i.test(msg)) {
      return `Insufficient balance: fee ke liye wallet me enough MSC nahi hai (current fee: ${String(els.txFee.value || "1")} MSC). Faucet/transfer karo, phir retry karo.`;
    }
    if (/insufficient allowance/i.test(msg)) {
      return "Allowance kam hai. Pehle owner wallet se TOKEN_APPROVE karo (same token_id, spender, amount), phir TOKEN_TRANSFER_FROM retry karo.";
    }
    if (/spender mismatch/i.test(msg)) {
      return "TransferFrom spender mismatch: tx sender aur payload.spender same hone chahiye. 'Use Tx Sender as Spender' click karo, phir retry karo.";
    }
    if (/invalid signature/i.test(msg)) {
      return "Invalid signature: Unlock Signer karke 'Build + Sign Tx' dabao, phir submit karo.";
    }
    if (/from must be msc address/i.test(msg)) {
      return "From address MSC format me hona chahiye (MSC...), 0x alias use na karo.";
    }
    if (/unknown pool|pool not found/i.test(msg)) {
      return "Pool nahi mila. 'Sync Token/Pool Refs' ya dtl_listPools chalao, exact pool_id use karo.";
    }
    if (/unknown token/i.test(msg)) {
      return "Token nahi mila. 'Sync Token/Pool Refs' ya dtl_listTokens se exact token_id copy karke use karo (symbol nahi).";
    }
    if (/pool tokens with transfer tax are not supported/i.test(msg)) {
      return "AMM pool me transfer-tax token allowed nahi hai. TOKEN_CREATE tax_bps=0 rakho, dtl_tokenInfo se verify karo, phir POOL_CREATE try karo.";
    }
    if (/token_id required|recipient required|amount must/i.test(msg)) {
      return `Details incomplete: ${msg}. Guided section me required fields fill karo, phir Apply + Build karo.`;
    }
    if (/signers\/signatures count mismatch/i.test(msg)) {
      return "Mint governance cert invalid: signers aur signatures ki count equal honi chahiye.";
    }
    return msg;
  }

  async function checkStatus() {
    const txID = String(els.statusTxId.value || "").trim();
    if (!txID) throw new Error("tx_id required");
    rememberLastTxID(txID);
    const result = await getJSON(`/tx/status?tx_id=${encodeURIComponent(txID)}`);
    els.statusOut.textContent = asPretty(result);
  }



  async function run(action) {
    try {
      await action();
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      els.txOut.textContent = friendlyErrorMessage(msg);
    }
  }

  async function runConnection(action) {
    try {
      await action();
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      els.txOut.textContent = friendlyErrorMessage(msg);
      setChainMeta("Error", "error");
      setConnState("Error", false);
    }
  }

  async function runWallet(action) {
    try {
      await action();
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      els.txOut.textContent = `Wallet action failed: ${friendlyErrorMessage(msg)}`;
    }
  }

  function copyTxJSON() {
    const v = String(els.txObject.value || "").trim();
    copyValueToClipboard(v, "Tx JSON copied.");
  }

  $("connectBtn").addEventListener("click", () => runConnection(connect));
  $("walletConnectBtn").addEventListener("click", () => runWallet(connectWalletViaBridge));
  $("chainIdBtn").addEventListener("click", () => runConnection(checkChainId));
  $("tokenInfoBtn").addEventListener("click", () => run(readTokenInfo));
  $("balanceBtn").addEventListener("click", () => run(readBalance));
  $("supplyBtn").addEventListener("click", () => run(readTotalSupply));
  $("listTokensBtn").addEventListener("click", () => run(readListTokens));
  $("poolInfoBtn").addEventListener("click", () => run(readPoolInfo));
  $("listPoolsBtn").addEventListener("click", () => run(readListPools));
  if (els.syncRefsBtn) {
    els.syncRefsBtn.addEventListener("click", () => run(syncRefSuggestions));
  }
  $("routeQuoteBtn").addEventListener("click", () => run(readRouteQuote));
  $("farmInfoBtn").addEventListener("click", () => run(readFarmInfo));
  $("farmPositionBtn").addEventListener("click", () => run(readFarmPosition));
  $("seasonInfoBtn").addEventListener("click", () => run(readSeasonInfo));
  $("leaderboardBtn").addEventListener("click", () => run(readLeaderboard));
  $("payloadTemplateBtn").addEventListener("click", () => run(loadPayloadTemplate));
  $("nextNonceBtn").addEventListener("click", () => run(loadNextNonce));
  $("buildBtn").addEventListener("click", () => run(async () => buildTxObject()));
  $("copyBtn").addEventListener("click", copyTxJSON);
  if (els.copyLastTxBtn) {
    els.copyLastTxBtn.addEventListener("click", () =>
      copyValueToClipboard(state.lastTxID, "Last tx ID copied.")
    );
  }
  if (els.clearDraftBtn) {
    els.clearDraftBtn.addEventListener("click", () => run(async () => clearDraft()));
  }
  if ($("applyTokenDetailsBtn")) {
    $("applyTokenDetailsBtn").addEventListener("click", () => run(async () => applyTokenDetailsToPayload()));
  }
  if ($("readTokenDetailsBtn")) {
    $("readTokenDetailsBtn").addEventListener("click", () => run(async () => readTokenDetailsFromPayload()));
  }
  if ($("applyTransferDetailsBtn")) {
    $("applyTransferDetailsBtn").addEventListener("click", () => run(async () => applyTransferDetailsToPayload()));
  }
  if ($("readTransferDetailsBtn")) {
    $("readTransferDetailsBtn").addEventListener("click", () => run(async () => readTransferDetailsFromPayload()));
  }
  if ($("applyApproveDetailsBtn")) {
    $("applyApproveDetailsBtn").addEventListener("click", () => run(async () => applyApproveDetailsToPayload()));
  }
  if ($("readApproveDetailsBtn")) {
    $("readApproveDetailsBtn").addEventListener("click", () => run(async () => readApproveDetailsFromPayload()));
  }
  if ($("applyTransferFromDetailsBtn")) {
    $("applyTransferFromDetailsBtn").addEventListener("click", () => run(async () => applyTransferFromDetailsToPayload()));
  }
  if (els.useSenderAsSpenderBtn) {
    els.useSenderAsSpenderBtn.addEventListener("click", () => run(async () => applyTxSenderAsSpender()));
  }
  if (els.prepareApproveFromTransferBtn) {
    els.prepareApproveFromTransferBtn.addEventListener("click", () => run(async () => prepareApproveFromTransfer()));
  }
  if ($("readTransferFromDetailsBtn")) {
    $("readTransferFromDetailsBtn").addEventListener("click", () => run(async () => readTransferFromDetailsFromPayload()));
  }
  if ($("applyMintDetailsBtn")) {
    $("applyMintDetailsBtn").addEventListener("click", () => run(applyMintDetailsToPayloadAndCert));
  }
  if ($("readMintDetailsBtn")) {
    $("readMintDetailsBtn").addEventListener("click", () => run(async () => readMintDetailsFromPayloadAndCert()));
  }
  if ($("applyBurnDetailsBtn")) {
    $("applyBurnDetailsBtn").addEventListener("click", () => run(async () => applyBurnDetailsToPayload()));
  }
  if ($("readBurnDetailsBtn")) {
    $("readBurnDetailsBtn").addEventListener("click", () => run(async () => readBurnDetailsFromPayload()));
  }
  $("submitDtlBtn").addEventListener("click", () => run(submitDTL));
  $("submitRawBtn").addEventListener("click", () => run(submitRaw));
  $("statusBtn").addEventListener("click", () => run(checkStatus));
  $("signerLoadBtn").addEventListener("click", () => run(async () => loadSignerWallet()));
  $("signerUnlockBtn").addEventListener("click", () => run(unlockSignerWallet));
  $("signerLockBtn").addEventListener("click", () => run(async () => lockSigner()));
  $("signTxBtn").addEventListener("click", () => run(signCurrentTx));
  if (els.walletBridgeClose) {
    els.walletBridgeClose.addEventListener("click", () => {
      hideWalletBridgeModal();
    });
  }
  if (els.walletBridgeModal) {
    els.walletBridgeModal.addEventListener("click", (event) => {
      if (event.target === els.walletBridgeModal) {
        hideWalletBridgeModal();
      }
    });
  }

  els.rpcBase.value = state.rpcBase;
  els.apiToken.value = state.apiToken;
  applyLaunchHintsFromQuery();
  if (els.dtlType) {
    els.dtlType.addEventListener("change", () => {
      coerceToSupportedDTLType(true);
      syncGuidedVisibility();
      refreshWorkspaceMeta();
      const kind = String(els.dtlType.value || "").trim();
      if (kind === "TOKEN_CREATE") run(async () => readTokenDetailsFromPayload());
      if (kind === "TOKEN_TRANSFER") run(async () => readTransferDetailsFromPayload());
      if (kind === "TOKEN_APPROVE") run(async () => readApproveDetailsFromPayload());
      if (kind === "TOKEN_TRANSFER_FROM") run(async () => readTransferFromDetailsFromPayload());
      if (kind === "TOKEN_MINT") run(async () => readMintDetailsFromPayloadAndCert());
      if (kind === "TOKEN_BURN") run(async () => readBurnDetailsFromPayload());
    });
  }
  if (els.beginnerMode) {
    els.beginnerMode.addEventListener("change", () => {
      syncGuidedVisibility();
      if (isBeginnerMode()) {
        let seed = null;
        try {
          seed = parseJSONField(els.txPayload.value, {});
        } catch (_) {
          seed = null;
        }
        renderQuickPayloadFields(seed);
      }
      scheduleDraftSave();
    });
  }
  if (els.txFrom) {
    els.txFrom.addEventListener("input", () => {
      updateTransferFromSpenderHint();
    });
  }
  if (els.tfSpender) {
    els.tfSpender.addEventListener("input", () => {
      updateTransferFromSpenderHint();
    });
  }
  document.addEventListener("keydown", (event) => {
    const key = String(event.key || "").toLowerCase();
    const mod = !!(event.ctrlKey || event.metaKey);
    if (mod && !event.shiftKey && key === "enter") {
      event.preventDefault();
      run(submitDTL);
      return;
    }
    if (mod && event.shiftKey && key === "s") {
      event.preventDefault();
      run(signCurrentTx);
      return;
    }
    if (!mod && event.altKey && key === "b") {
      event.preventDefault();
      run(async () => buildTxObject());
    }
  });
  els.txChainId.value = state.chainId;
  if (state.walletAccount) {
    if (!String(els.accountRef.value || "").trim()) {
      els.accountRef.value = state.walletAccount;
    }
    if (!String(els.txFrom.value || "").trim()) {
      els.txFrom.value = state.walletAccount;
    }
    setWalletMeta(state.walletAccount, "ok");
  } else {
    setWalletMeta("-", "unknown");
  }
  setChainMeta(state.chainId || "-", state.chainId ? "info" : "unknown");
  applyRefSuggestionsToInputs();
  updateTransferFromSpenderHint();
  renderCompatMode();
  const draftRestored = restoreDraft();
  pruneUnsupportedDTLTypeOptions();
  coerceToSupportedDTLType(false);
  syncGuidedVisibility();
  if (!draftRestored || !String(els.txPayload.value || "").trim()) {
    run(loadPayloadTemplate);
  } else {
    try {
		const restoredPayload = parseJSONField(els.txPayload.value, {});
		renderQuickPayloadFields(restoredPayload);
		setPayloadPreview(restoredPayload);
    } catch (_) {
      // Keep restored draft text as-is if JSON is incomplete.
    }
    const restoredKind = String(els.dtlType.value || "").trim();
    if (restoredKind === "TOKEN_CREATE") run(async () => readTokenDetailsFromPayload());
    if (restoredKind === "TOKEN_TRANSFER") run(async () => readTransferDetailsFromPayload());
    if (restoredKind === "TOKEN_APPROVE") run(async () => readApproveDetailsFromPayload());
    if (restoredKind === "TOKEN_TRANSFER_FROM") run(async () => readTransferFromDetailsFromPayload());
    if (restoredKind === "TOKEN_MINT") run(async () => readMintDetailsFromPayloadAndCert());
    if (restoredKind === "TOKEN_BURN") run(async () => readBurnDetailsFromPayload());
  }
	bindDraftPersistence();
	refreshWorkspaceMeta();
  setConnState(String(els.connState.textContent || "Disconnected").trim() || "Disconnected", false);
  lockSigner();
  try {
    state.wallet = loadStoredWallet();
    applyWalletToForm();
    if (state.wallet) {
      setSignerState("Loaded", true);
    }
  } catch (_) {
    setSignerState("Locked", false);
  }
  runConnection(refreshCompatMode);
})();
