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
    wsContractId: $("wsContractId"),
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
    statusContractId: $("statusContractId"),
    statusLogicHash: $("statusLogicHash"),
    statusUseContractBtn: $("statusUseContractBtn"),
    statusCopyContractBtn: $("statusCopyContractBtn"),
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
    contractDslGuidedBox: $("contractDslGuidedBox"),
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
    contractDslLang: $("contractDslLang"),
    contractDslName: $("contractDslName"),
    contractDslSource: $("contractDslSource"),
    contractDslOutputMode: $("contractDslOutputMode"),
    customCodeLang: $("customCodeLang"),
    customCodeName: $("customCodeName"),
    customCodeEditor: $("customCodeEditor"),
    customCodeOutputMode: $("customCodeOutputMode"),
    customCodeOut: $("customCodeOut"),
    customCodeAnalyzeBtn: $("customCodeAnalyzeBtn"),
    customCodeUseBtn: $("customCodeUseBtn"),
    customCodeCopyPayloadBtn: $("customCodeCopyPayloadBtn"),
    customCodeSampleBtn: $("customCodeSampleBtn"),
    customTplMsc20Btn: $("customTplMsc20Btn"),
    customTplNft721Btn: $("customTplNft721Btn"),
    customTplMsc1155Btn: $("customTplMsc1155Btn"),
    presetAmmPoolBtn: $("presetAmmPoolBtn"),
    presetLendingBtn: $("presetLendingBtn"),
    presetDuelBtn: $("presetDuelBtn"),
    presetTournamentBtn: $("presetTournamentBtn"),
    logicSimMethod: $("logicSimMethod"),
    logicSimArgs: $("logicSimArgs"),
    logicSimOut: $("logicSimOut"),
    copyLastContractBtn: $("copyLastContractBtn"),
    copyLastTxBtn: $("copyLastTxBtn"),
    clearDraftBtn: $("clearDraftBtn"),
  };

  const state = {
    rpcBase: normalizeRpcBase(localStorage.getItem("msc_dtl_rpc") || inferDefaultRPCBase()),
    apiToken: normalizeToken(localStorage.getItem("msc_dtl_token") || ""),
    chainId: localStorage.getItem("msc_dtl_chain") || "91938",
    walletAccount: String(localStorage.getItem("msc_dtl_wallet_account") || "").trim(),
    lastContractDeployID: String(localStorage.getItem("msc_dtl_last_contract_id") || "").trim().toLowerCase(),
    lastTxID: String(localStorage.getItem("msc_dtl_last_tx_id") || "").trim().toLowerCase(),
    lastBuiltTx: null,
    wallet: null,
    secretKey: null,
    bridgeTarget: null,
    bridgeMode: "iframe",
    bridgeFrameLoadedURL: "",
    bridgeOrigin: "",
    bridgeSeq: 0,
    customCodeLastPayload: null,
    compatSubset: null,
    bytecodeRuntimeEnabled: null,
    bytecodeRuntimeActive: null,
    bytecodeActivationHeight: null,
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
    "contractDslLang",
    "contractDslName",
    "contractDslSource",
    "contractDslOutputMode",
    "customCodeLang",
    "customCodeName",
    "customCodeEditor",
    "customCodeOutputMode",
    "logicSimMethod",
    "logicSimArgs",
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
    const toPillState = (value) => {
      if (value === true) return { text: "ON", klass: "runtime-pill-on" };
      if (value === false) return { text: "OFF", klass: "runtime-pill-off" };
      return { text: "UNKNOWN", klass: "runtime-pill-unknown" };
    };
    const toActivePillState = (value) => {
      if (value === true) return { text: "ACTIVE", klass: "runtime-pill-on" };
      if (value === false) return { text: "INACTIVE", klass: "runtime-pill-off" };
      return { text: "UNKNOWN", klass: "runtime-pill-unknown" };
    };
    const activationHeight =
      Number.isFinite(Number(state.bytecodeActivationHeight)) && Number(state.bytecodeActivationHeight) >= 0
        ? String(state.bytecodeActivationHeight)
        : "UNKNOWN";
    const activationClass = activationHeight === "UNKNOWN" ? "runtime-pill-unknown" : "runtime-pill-info";

    els.compatMeta.textContent = "";
    els.compatMeta.classList.add("runtime-meta");
    const title = document.createElement("span");
    title.className = "runtime-meta-title";
    title.textContent = "DTL Runtime";
    els.compatMeta.appendChild(title);

    const pills = document.createElement("span");
    pills.className = "runtime-pills";
    const entries = [
      { label: "MSC Compat", state: toPillState(state.compatSubset) },
      { label: "Contract Runtime Removed", state: toPillState(state.contractRuntimeRemoved) },
      { label: "Bytecode Runtime", state: toPillState(state.bytecodeRuntimeEnabled) },
      { label: "Bytecode Active", state: toActivePillState(state.bytecodeRuntimeActive) },
      { label: "Activation Height", state: { text: activationHeight, klass: activationClass } },
    ];
    for (const entry of entries) {
      const pill = document.createElement("span");
      pill.className = `runtime-pill ${entry.state.klass}`;
      pill.textContent = `${entry.label}: ${entry.state.text}`;
      pills.appendChild(pill);
    }
    els.compatMeta.appendChild(pills);
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
    if (els.wsContractId) {
      els.wsContractId.textContent = shortValue(state.lastContractDeployID || "-", 14, 12);
      els.wsContractId.title = state.lastContractDeployID || "";
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

  function encodeU16BE(value) {
    const out = new Uint8Array(2);
    const v = Number(value >>> 0) & 0xffff;
    out[0] = (v >>> 8) & 0xff;
    out[1] = v & 0xff;
    return out;
  }

  function encodeU32BE(value) {
    const out = new Uint8Array(4);
    const v = Number(value >>> 0) >>> 0;
    out[0] = (v >>> 24) & 0xff;
    out[1] = (v >>> 16) & 0xff;
    out[2] = (v >>> 8) & 0xff;
    out[3] = v & 0xff;
    return out;
  }

  function decodeU16BE(bytes, offset) {
    return ((bytes[offset] << 8) | bytes[offset + 1]) >>> 0;
  }

  function decodeU32BE(bytes, offset) {
    return (
      ((bytes[offset] << 24) >>> 0) |
      ((bytes[offset + 1] << 16) >>> 0) |
      ((bytes[offset + 2] << 8) >>> 0) |
      (bytes[offset + 3] >>> 0)
    ) >>> 0;
  }

  const DTL_BYTECODE_MAGIC = "DTLBC1";
  const DTL_BYTECODE_VERSION = 1;
  const DTL_BYTECODE_FORMAT = "dtl-bc-v1";
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

  let crc32Table = null;
  function getCRC32Table() {
    if (crc32Table) return crc32Table;
    const table = new Uint32Array(256);
    for (let i = 0; i < 256; i++) {
      let c = i;
      for (let j = 0; j < 8; j++) {
        if ((c & 1) !== 0) {
          c = (0xedb88320 ^ (c >>> 1)) >>> 0;
        } else {
          c = (c >>> 1) >>> 0;
        }
      }
      table[i] = c >>> 0;
    }
    crc32Table = table;
    return crc32Table;
  }

  function crc32IEEE(bytes) {
    const table = getCRC32Table();
    let crc = 0xffffffff;
    for (let i = 0; i < bytes.length; i++) {
      const idx = (crc ^ bytes[i]) & 0xff;
      crc = (table[idx] ^ (crc >>> 8)) >>> 0;
    }
    return (crc ^ 0xffffffff) >>> 0;
  }

  function normalizeContractOutputMode(raw) {
    const v = String(raw || "").trim().toLowerCase();
    if (v === "logic_pack" || v === "logic-pack") return "logic_pack";
    return "dtl-bc-v1";
  }

  function logicPackFromBytecodeProgram(program) {
    if (!program || typeof program !== "object") return null;
    const methods = Array.isArray(program.methods)
      ? program.methods.map((m) => ({
          name: String((m && m.name) || "").trim(),
          max_steps: Number.parseInt(String((m && m.max_steps) || "0"), 10) || 0,
          ops: Array.isArray(m && m.code) ? m.code : [],
        }))
      : [];
    if (!methods.length) return null;
    return {
      version: 1,
      name: String(program.name || "").trim(),
      abi: Array.isArray(program.abi) ? program.abi : [],
      storage: Array.isArray(program.storage) ? program.storage : [],
      methods,
      limits: (program.limits && typeof program.limits === "object") ? program.limits : {},
    };
  }

  function bytecodeProgramFromLogicPack(pack) {
    if (!pack || typeof pack !== "object") return null;
    return {
      version: DTL_BYTECODE_VERSION,
      name: String(pack.name || "").trim(),
      abi: Array.isArray(pack.abi) ? pack.abi : [],
      storage: Array.isArray(pack.storage) ? pack.storage : [],
      methods: Array.isArray(pack.methods)
        ? pack.methods.map((m) => ({
            name: String((m && m.name) || "").trim(),
            max_steps: Number.parseInt(String((m && m.max_steps) || "0"), 10) || 0,
            code: Array.isArray(m && m.ops) ? m.ops : [],
          }))
        : [],
      limits: (pack.limits && typeof pack.limits === "object") ? pack.limits : {},
    };
  }

  function encodeDTLBytecodeProgramHex(program) {
    if (!program || typeof program !== "object") {
      throw new Error("Invalid bytecode program");
    }
    const normalized = {
      ...program,
      version: DTL_BYTECODE_VERSION,
    };
    const payloadBytes = enc.encode(JSON.stringify(normalized));
    const checksum = crc32IEEE(payloadBytes);
    const headerBytes = concatBytes([
      enc.encode(DTL_BYTECODE_MAGIC),
      encodeU16BE(DTL_BYTECODE_VERSION),
      encodeU32BE(payloadBytes.length >>> 0),
      encodeU32BE(checksum),
    ]);
    return bytesToHex(concatBytes([headerBytes, payloadBytes]));
  }

  function decodeDTLBytecodeProgramHex(rawHex) {
    const bytes = hexToBytes(rawHex);
    if (bytes.length < 16) {
      throw new Error("Bytecode too short");
    }
    const magic = new TextDecoder().decode(bytes.slice(0, 6));
    if (magic !== DTL_BYTECODE_MAGIC) {
      throw new Error("Invalid bytecode magic");
    }
    const version = decodeU16BE(bytes, 6);
    if (version !== DTL_BYTECODE_VERSION) {
      throw new Error("Unsupported bytecode version");
    }
    const payloadSize = decodeU32BE(bytes, 8);
    const checksum = decodeU32BE(bytes, 12);
    const payload = bytes.slice(16);
    if (payload.length !== payloadSize) {
      throw new Error("Invalid bytecode payload size");
    }
    const computed = crc32IEEE(payload);
    if (computed !== checksum) {
      throw new Error("Bytecode checksum mismatch");
    }
    const decodedText = new TextDecoder().decode(payload);
    const program = JSON.parse(decodedText);
    if (!program || typeof program !== "object") {
      throw new Error("Invalid bytecode payload");
    }
    return program;
  }

  async function sha256(bytes) {
    const out = await crypto.subtle.digest("SHA-256", bytes);
    return new Uint8Array(out);
  }

  function rememberLastContractDeployID(contractID) {
    const normalized = String(contractID || "").trim().toLowerCase();
    if (!normalized) return;
    state.lastContractDeployID = normalized;
    localStorage.setItem("msc_dtl_last_contract_id", normalized);
    refreshWorkspaceMeta();
  }

  function setStatusDeployMeta(contractID, logicHash) {
    const normalizedID = String(contractID || "").trim().toLowerCase();
    const normalizedLogic = String(logicHash || "").trim().toLowerCase();
    if (els.statusContractId) {
      els.statusContractId.value = normalizedID;
      els.statusContractId.title = normalizedID;
    }
    if (els.statusLogicHash) {
      els.statusLogicHash.value = normalizedLogic;
      els.statusLogicHash.title = normalizedLogic;
    }
  }

  function firstNonEmptyString(...values) {
    for (const value of values) {
      const normalized = String(value || "").trim();
      if (normalized) return normalized;
    }
    return "";
  }

  function unwrapSuccessEnvelope(payload) {
    if (!payload || typeof payload !== "object") return payload;
    if (payload.success === true && payload.data && typeof payload.data === "object") {
      return payload.data;
    }
    return payload;
  }

  function extractDeployMetadata(payload) {
    const queue = [unwrapSuccessEnvelope(payload)];
    const seen = new Set();
    let contractID = "";
    let logicHash = "";
    let txType = "";

    while (queue.length > 0) {
      const current = queue.shift();
      if (!current || typeof current !== "object") continue;
      if (seen.has(current)) continue;
      seen.add(current);

      contractID = contractID || firstNonEmptyString(current.contract_id, current.contractId);
      logicHash = logicHash || firstNonEmptyString(
        current.logic_hash,
        current.logic_pack_hash,
        current.logicHash,
        current.logicPackHash
      );
      txType = txType || firstNonEmptyString(
        current.dtl_tx_type,
        current.dtlTxType,
        current.tx_type,
        current.txType
      );

      if (current.data && typeof current.data === "object") queue.push(current.data);
      if (current.result && typeof current.result === "object") queue.push(current.result);
      if (current.tx && typeof current.tx === "object") queue.push(current.tx);
      if (current.payload && typeof current.payload === "object") queue.push(current.payload);
    }

    return {
      contractID: String(contractID || "").trim().toLowerCase(),
      logicHash: String(logicHash || "").trim().toLowerCase(),
      txType: String(txType || "").trim().toUpperCase(),
    };
  }

  function applyDeployMetadata(meta, allowWorkspaceUpdate) {
    const details = meta && typeof meta === "object" ? meta : {};
    const contractID = String(details.contractID || "").trim().toLowerCase();
    const logicHash = String(details.logicHash || "").trim().toLowerCase();
    const txType = String(details.txType || "").trim().toUpperCase();
    const deployTypeOK = !txType || txType === "CONTRACT_DEPLOY";

    if (allowWorkspaceUpdate && contractID && deployTypeOK) {
      rememberLastContractDeployID(contractID);
      prefillContractCallPayloadFromLastDeploy();
    }
    setStatusDeployMeta(contractID, logicHash);
    return { contractID, logicHash, txType };
  }

  async function resolveDeployMetadataForTx(tx, txID, seedPayload) {
    const seedMeta = extractDeployMetadata(seedPayload);
    if (seedMeta.contractID || seedMeta.logicHash) return seedMeta;

    const resolvedTxID = String(txID || (tx && tx.id) || "").trim();
    if (resolvedTxID) {
      try {
        const statusPayload = await getJSON(`/tx/status?tx_id=${encodeURIComponent(resolvedTxID)}`);
        const statusMeta = extractDeployMetadata(statusPayload);
        if (statusMeta.contractID || statusMeta.logicHash) {
          return statusMeta;
        }
      } catch (_) {
        // status endpoint can fail transiently right after submit; fallback below.
      }
    }

    const derivedID = await deriveContractIDFromDeployTx(tx);
    if (derivedID) {
      return {
        contractID: derivedID,
        logicHash: "",
        txType: "CONTRACT_DEPLOY",
      };
    }
    return {
      contractID: "",
      logicHash: "",
      txType: "CONTRACT_DEPLOY",
    };
  }

  async function deriveContractIDFromDeployTx(tx) {
    if (!tx || String(tx.dtl_tx_type || "").trim() !== "CONTRACT_DEPLOY") return "";
    let payloadObj = null;
    try {
      payloadObj = JSON.parse(String(tx.dtl_payload || "{}"));
    } catch (_) {
      return "";
    }
    if (!payloadObj || typeof payloadObj !== "object") return "";
    const chainID = String(tx.ChainID || tx.chainID || state.chainId || els.txChainId.value || "").trim();
    const creator = String(payloadObj.creator || tx.from || "").trim().toLowerCase();
    const name = String(payloadObj.name || "").trim();
    const lang = String(payloadObj.lang || "").trim().toLowerCase();
    const nonceRaw = Number.parseInt(String(tx.nonce || "0"), 10);
    const versionRaw = Number.parseInt(String(payloadObj.version ?? "0"), 10);
    const nonce = Number.isInteger(nonceRaw) && nonceRaw >= 0 ? nonceRaw : 0;
    const version = Number.isInteger(versionRaw) && versionRaw >= 0 ? versionRaw : 0;
    if (!chainID || !creator || !name || !lang) return "";
    const material = `${chainID}|${creator}|${name}|${lang}|${version}|${nonce}`;
    return bytesToHex(await sha256(enc.encode(material)));
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
    pushInt64(Number.parseInt(tx.evm_gas_limit || tx.evmGasLimit || 0, 10) || 0);
    pushString(stripHexPrefix(tx.evm_code || tx.evmCode || ""));
    pushString(stripHexPrefix(tx.evm_input || tx.evmInput || ""));
    pushString(stripHexPrefix(tx.evm_raw_tx || tx.evmRawTx || ""));
    pushString(stripHexPrefix(tx.evm_tx_hash || tx.evmTxHash || ""));

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

  function prefillContractCallPayloadFromLastDeploy() {
    if (state.contractRuntimeRemoved !== false) return false;
    const lastContractID = String(state.lastContractDeployID || "").trim();
    if (!lastContractID) return false;
    const kind = String(els.dtlType && els.dtlType.value ? els.dtlType.value : "").trim();
    if (kind !== "CONTRACT_CALL") return false;
    let payload = defaultPayload("CONTRACT_CALL");
    try {
      payload = parseJSONField(els.txPayload.value, payload);
    } catch (_) {
      // Fallback to CONTRACT_CALL template.
    }
    if (String(payload.contract_id || "").trim()) return false;
    payload.contract_id = lastContractID;
    if (!String(payload.caller || "").trim()) {
      payload.caller = String(els.txFrom.value || "").trim();
    }
    els.txPayload.value = asPretty(payload);
    renderQuickPayloadFields(payload);
    setPayloadPreview(payload);
    prefillLogicSimulatorFromPayload(payload);
    scheduleDraftSave();
    return true;
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
    if (els.contractDslGuidedBox) {
      els.contractDslGuidedBox.classList.add("hidden");
    }
    applyBeginnerModeVisibility(isMint);
    updateTransferFromSpenderHint();
  }

  function sanitizeContractDslName(raw, fallback) {
    const base = String(raw || "").trim();
    const safe = base.replace(/[^A-Za-z0-9_]/g, "_").replace(/^_+/, "").slice(0, 64);
    if (safe) return safe;
    const alt = String(fallback || "").trim().replace(/[^A-Za-z0-9_]/g, "_").replace(/^_+/, "").slice(0, 64);
    return alt || "GeneratedContract";
  }

  function detectContractDslLang(source, preferred) {
    const pref = String(preferred || "").trim().toLowerCase();
    if (pref === "solidity-like" || pref === "vyper-like") return pref;
    const src = String(source || "");
    if (/\bdef\s+[A-Za-z_][A-Za-z0-9_]*\s*\(/.test(src) || /\bself\.[A-Za-z_][A-Za-z0-9_]*\b/.test(src)) {
      return "vyper-like";
    }
    return "solidity-like";
  }

  function contractDslSample(lang) {
    if (lang === "vyper-like") {
      return [
        "count: uint64",
        "label: String[64]",
        "",
        "@external",
        "def inc(delta: uint64):",
        "    self.count += delta",
        "",
        "@external",
        "def dec(delta: uint64):",
        "    self.count -= delta",
        "",
        "@external",
        "def set_label(value: String[64]):",
        "    self.label = value",
        "",
        "@external",
        "def reward(to: address, amount: uint64):",
        "    token_transfer_from_contract(\"MYTS\", to, amount)",
        "",
        "@external",
        "def mint_rewards(token: String[64], to: address, amount: uint64):",
        "    token_mint_from_contract(token, to, amount)",
        "",
        "@external",
        "def burn_user(token: String[64], amount: uint64):",
        "    token_burn(token, amount)",
      ].join("\n");
    }
    return [
      "contract Counter {",
      "  uint64 count;",
      "  string label;",
      "",
      "  function inc(uint64 delta) external { count += delta; }",
      "  function dec(uint64 delta) external { count -= delta; }",
      "  function setLabel(string value) external { label = value; }",
      "  function reward(address to, uint64 amount) external { token_transfer_from_contract(\"MYTS\", to, amount); }",
      "  function mintRewards(string token, address to, uint64 amount) external { token_mint_from_contract(token, to, amount); }",
      "  function burnUser(string token, uint64 amount) external { token_burn(token, amount); }",
      "}",
    ].join("\n");
  }

  function contractDslMsc20Template(lang) {
    if (lang === "vyper-like") {
      return [
        "# @title MSC20Controller",
        "",
        "@external",
        "def create_token(name: String[64], symbol: String[16], decimals: uint64, max_supply: uint64, initial_supply: uint64, owner: address):",
        "    token_create_from_contract(name, symbol, decimals, max_supply, initial_supply, owner)",
        "",
        "@external",
        "def mint(token: String[64], to: address, amount: uint64):",
        "    token_mint_from_contract(token, to, amount)",
        "",
        "@external",
        "def burn(token: String[64], amount: uint64):",
        "    token_burn(token, amount)",
        "",
        "@external",
        "def transfer(token: String[64], to: address, amount: uint64):",
        "    token_transfer(token, to, amount)",
        "",
        "@external",
        "def approve(token: String[64], spender: address, amount: uint64):",
        "    token_approve(token, spender, amount)",
        "",
        "@external",
        "def transfer_from(token: String[64], from_addr: address, to: address, amount: uint64):",
        "    token_transfer_from(token, from_addr, to, amount)",
      ].join("\n");
    }
    return [
      "contract MSC20Controller {",
      "  function createToken(string name, string symbol, uint64 decimals, uint64 maxSupply, uint64 initialSupply, address owner) external {",
      "    token_create_from_contract(name, symbol, decimals, maxSupply, initialSupply, owner);",
      "  }",
      "",
      "  function mint(string token, address to, uint64 amount) external { token_mint_from_contract(token, to, amount); }",
      "  function burn(string token, uint64 amount) external { token_burn(token, amount); }",
      "  function transfer(string token, address to, uint64 amount) external { token_transfer(token, to, amount); }",
      "  function approve(string token, address spender, uint64 amount) external { token_approve(token, spender, amount); }",
      "  function transferFrom(string token, address fromAddr, address to, uint64 amount) external { token_transfer_from(token, fromAddr, to, amount); }",
      "}",
    ].join("\n");
  }

  function stripContractComments(raw) {
    return String(raw || "")
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .replace(/\/\/.*$/gm, "")
      .replace(/#[^\n]*$/gm, "");
  }

  function parseContractParamNames(raw, mode) {
    const text = String(raw || "").trim();
    if (!text) return [];
    const out = [];
    text.split(",").forEach((part) => {
      const p = String(part || "").trim();
      if (!p) return;
      if (mode === "vyper-like") {
        const m = p.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*:/);
        if (m) {
          out.push(m[1]);
        }
        return;
      }
      const tokens = p.split(/\s+/).filter(Boolean);
      if (!tokens.length) return;
      const name = String(tokens[tokens.length - 1] || "").replace(/[,)]/g, "");
      if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) {
        out.push(name);
      }
    });
    return out;
  }

  function parseContractStorageDeclarations(source, lang) {
    const src = stripContractComments(source);
    const storageTypes = {};
    const init = {};
    if (lang === "vyper-like") {
      const re = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(uint(?:8|16|32|64|128|256)?|int(?:8|16|32|64|128|256)?|String(?:\[[0-9]+\])?)\s*$/gim;
      let m;
      while ((m = re.exec(src)) !== null) {
        const name = String(m[1] || "").trim();
        const t = String(m[2] || "").trim().toLowerCase();
        if (!name) continue;
        if (t.startsWith("string")) {
          storageTypes[name] = "string";
          init[name] = "";
        } else {
          storageTypes[name] = "u64";
          init[name] = "0";
        }
      }
      return { storageTypes, init };
    }

    const re = /^\s*(uint(?:8|16|32|64|128|256)?|int(?:8|16|32|64|128|256)?|string)\s+(?:public\s+|private\s+|internal\s+|external\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?:=\s*([^;]+))?\s*;/gim;
    let m;
    while ((m = re.exec(src)) !== null) {
      const t = String(m[1] || "").trim().toLowerCase();
      const name = String(m[2] || "").trim();
      const assigned = String(m[3] || "").trim();
      if (!name) continue;
      if (t === "string") {
        storageTypes[name] = "string";
        if (/^".*"$/.test(assigned) || /^'.*'$/.test(assigned)) {
          init[name] = assigned.slice(1, -1);
        } else {
          init[name] = "";
        }
      } else {
        storageTypes[name] = "u64";
        if (/^[0-9]+$/.test(assigned)) {
          init[name] = assigned;
        } else {
          init[name] = "0";
        }
      }
    }
    return { storageTypes, init };
  }

  function parseSolidityFunctions(source) {
    const src = stripContractComments(source);
    const out = [];
    const re = /function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*[^{;]*\{([\s\S]*?)\}/gim;
    let m;
    while ((m = re.exec(src)) !== null) {
      out.push({
        name: String(m[1] || "").trim(),
        params: parseContractParamNames(m[2], "solidity-like"),
        body: String(m[3] || ""),
      });
    }
    return out;
  }

  function parseVyperFunctions(source) {
    const lines = String(source || "").replace(/\r/g, "").split("\n");
    const out = [];
    for (let i = 0; i < lines.length; i += 1) {
      const line = lines[i];
      const match = line.match(/^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*:/);
      if (!match) continue;
      const indent = (line.match(/^\s*/) || [""])[0].length;
      const body = [];
      for (i += 1; i < lines.length; i += 1) {
        const next = lines[i];
        const nextIndent = (next.match(/^\s*/) || [""])[0].length;
        const trimmed = next.trim();
        if (!trimmed) continue;
        if (nextIndent <= indent) {
          i -= 1;
          break;
        }
        body.push(trimmed);
      }
      out.push({
        name: String(match[1] || "").trim(),
        params: parseContractParamNames(match[2], "vyper-like"),
        body: body.join("\n"),
      });
    }
    return out;
  }

  function splitCSVArgs(raw) {
    const s = String(raw || "");
    const out = [];
    let buf = "";
    let quote = "";
    for (let i = 0; i < s.length; i += 1) {
      const ch = s[i];
      if ((ch === '"' || ch === "'") && s[i - 1] !== "\\") {
        if (!quote) {
          quote = ch;
        } else if (quote === ch) {
          quote = "";
        }
        buf += ch;
        continue;
      }
      if (ch === "," && !quote) {
        out.push(buf.trim());
        buf = "";
        continue;
      }
      buf += ch;
    }
    if (buf.trim()) out.push(buf.trim());
    return out;
  }

  function extractContractStatements(body) {
    const normalized = stripContractComments(body).replace(/[{}]/g, "\n");
    return normalized
      .split(/[;\n]/)
      .map((line) => String(line || "").trim())
      .filter((line) => line && line !== "pass" && !/^return\b/i.test(line));
  }

  function parseContractStatement(statement, storageTypes) {
    const s = String(statement || "").trim();
    if (!s) return null;

    const normalizeArgName = (raw, field) => {
      const value = String(raw || "").trim().replace(/^self\./i, "");
      if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(value)) {
        throw new Error(`Invalid ${field} in: ${s}`);
      }
      return value;
    };
    const parseTokenRef = (raw) => {
      const tokenRaw = String(raw || "").trim();
      if (!tokenRaw) throw new Error(`Missing token reference in: ${s}`);
      const tokenLiteral = tokenRaw.replace(/^['"]|['"]$/g, "").trim();
      if (tokenLiteral !== tokenRaw && tokenLiteral) {
        return { token_id: tokenLiteral };
      }
      const tokenArg = normalizeArgName(tokenRaw, "token arg");
      return { token_arg: tokenArg };
    };
    const withTokenRef = (out, tokenRef) => {
      if (tokenRef.token_id) out.token_id = tokenRef.token_id;
      if (tokenRef.token_arg) out.token_arg = tokenRef.token_arg;
      return out;
    };

    const callMatch = s.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)$/i);
    if (callMatch) {
      const fn = String(callMatch[1] || "").toLowerCase();
      const args = splitCSVArgs(callMatch[2]);

      if ((fn === "token_transfer" || fn === "token_transfer_from_contract") && args.length === 3) {
        const tokenRef = parseTokenRef(args[0]);
        const toArg = normalizeArgName(args[1], "to arg");
        const amountArg = normalizeArgName(args[2], "amount arg");
        return withTokenRef(
          {
            op: "TOKEN_TRANSFER",
            from: fn === "token_transfer_from_contract" ? "contract" : "caller",
            arg: amountArg,
            to_arg: toArg,
          },
          tokenRef
        );
      }

      if ((fn === "token_approve" || fn === "token_approve_from_contract") && args.length === 3) {
        const tokenRef = parseTokenRef(args[0]);
        const spenderArg = normalizeArgName(args[1], "spender arg");
        const amountArg = normalizeArgName(args[2], "amount arg");
        return withTokenRef(
          {
            op: "TOKEN_APPROVE",
            from: fn === "token_approve_from_contract" ? "contract" : "caller",
            spender_arg: spenderArg,
            arg: amountArg,
          },
          tokenRef
        );
      }

      if ((fn === "token_transfer_from" || fn === "token_transfer_from_contract") && args.length === 4) {
        const tokenRef = parseTokenRef(args[0]);
        const fromArg = normalizeArgName(args[1], "from arg");
        const toArg = normalizeArgName(args[2], "to arg");
        const amountArg = normalizeArgName(args[3], "amount arg");
        return withTokenRef(
          {
            op: "TOKEN_TRANSFER_FROM",
            from: fn === "token_transfer_from_contract" ? "contract" : "caller",
            from_arg: fromArg,
            to_arg: toArg,
            arg: amountArg,
          },
          tokenRef
        );
      }

      if ((fn === "token_mint" || fn === "token_mint_from_contract") && args.length === 3) {
        const tokenRef = parseTokenRef(args[0]);
        const toArg = normalizeArgName(args[1], "to arg");
        const amountArg = normalizeArgName(args[2], "amount arg");
        return withTokenRef(
          {
            op: "TOKEN_MINT",
            from: fn === "token_mint_from_contract" ? "contract" : "caller",
            to_arg: toArg,
            arg: amountArg,
          },
          tokenRef
        );
      }

      if ((fn === "token_burn" || fn === "token_burn_from_contract") && args.length === 2) {
        const tokenRef = parseTokenRef(args[0]);
        const amountArg = normalizeArgName(args[1], "amount arg");
        return withTokenRef(
          {
            op: "TOKEN_BURN",
            from: fn === "token_burn_from_contract" ? "contract" : "caller",
            arg: amountArg,
          },
          tokenRef
        );
      }

      if ((fn === "token_create" || fn === "token_create_from_contract") && args.length === 6) {
        const nameArg = normalizeArgName(args[0], "name arg");
        const symbolArg = normalizeArgName(args[1], "symbol arg");
        const decimalsArg = normalizeArgName(args[2], "decimals arg");
        const maxSupplyArg = normalizeArgName(args[3], "max_supply arg");
        const initialSupplyArg = normalizeArgName(args[4], "initial_supply arg");
        const toArg = normalizeArgName(args[5], "to arg");
        return {
          op: "TOKEN_CREATE",
          from: fn === "token_create_from_contract" ? "contract" : "caller",
          name_arg: nameArg,
          symbol_arg: symbolArg,
          decimals_arg: decimalsArg,
          max_supply_arg: maxSupplyArg,
          initial_supply_arg: initialSupplyArg,
          to_arg: toArg,
        };
      }
    }

    const assignMatch = s.match(/^(?:self\.)?([A-Za-z_][A-Za-z0-9_]*)\s*(\+=|-=|=)\s*([A-Za-z_][A-Za-z0-9_]*)$/);
    if (!assignMatch) return null;
    const key = String(assignMatch[1] || "").trim();
    const operator = String(assignMatch[2] || "").trim();
    const arg = String(assignMatch[3] || "").trim();
    if (operator === "+=") {
      return { op: "ADD_U64", key, arg };
    }
    if (operator === "-=") {
      return { op: "SUB_U64", key, arg };
    }
    const declaredType = storageTypes[key] || "u64";
    if (declaredType === "string") {
      return { op: "SET_STR", key, arg };
    }
    return { op: "SET_U64", key, arg };
  }

  function compileContractMethod(fn, storageTypes) {
    const methodName = String(fn && fn.name ? fn.name : "").trim();
    if (!methodName) {
      throw new Error("Method name missing");
    }
    if (!Array.isArray(fn.params) || fn.params.length === 0) {
      throw new Error(`Method ${methodName} requires at least 1 argument`);
    }
    const statements = extractContractStatements(fn.body);
    let compiled = null;
    for (const stmt of statements) {
      const parsed = parseContractStatement(stmt, storageTypes);
      if (!parsed) continue;
      if (compiled) {
        throw new Error(`Method ${methodName} has multiple state operations (only 1 supported)`);
      }
      compiled = parsed;
    }
    if (!compiled) {
      throw new Error(`Method ${methodName} has no supported deterministic operation`);
    }
    return Object.assign({ name: methodName, params: Array.isArray(fn.params) ? fn.params : [] }, compiled);
  }

  function legacyCompiledMethodToLogicOps(compiled) {
    const op = String(compiled && compiled.op ? compiled.op : "").trim().toUpperCase();
    const tokenRef = {};
    if (String(compiled && compiled.token_arg ? compiled.token_arg : "").trim()) {
      tokenRef.token_arg = String(compiled.token_arg).trim();
    } else if (String(compiled && compiled.token_id ? compiled.token_id : "").trim()) {
      tokenRef.token_id = String(compiled.token_id).trim();
    }
    if (op === "TOKEN_TRANSFER") {
      return {
        max_steps: 4,
        ops: [
          Object.assign(
            {
              op: "TOKEN_TRANSFER",
              from: String(compiled.from || "caller").trim().toLowerCase(),
              to_arg: String(compiled.to_arg || "").trim(),
              amount_arg: String(compiled.arg || "").trim(),
            },
            tokenRef
          ),
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "TOKEN_APPROVE") {
      return {
        max_steps: 4,
        ops: [
          Object.assign(
            {
              op: "TOKEN_APPROVE",
              from: String(compiled.from || "caller").trim().toLowerCase(),
              spender_arg: String(compiled.spender_arg || "").trim(),
              amount_arg: String(compiled.arg || "").trim(),
            },
            tokenRef
          ),
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "TOKEN_TRANSFER_FROM") {
      return {
        max_steps: 4,
        ops: [
          Object.assign(
            {
              op: "TOKEN_TRANSFER_FROM",
              from: String(compiled.from || "caller").trim().toLowerCase(),
              from_arg: String(compiled.from_arg || "").trim(),
              to_arg: String(compiled.to_arg || "").trim(),
              amount_arg: String(compiled.arg || "").trim(),
            },
            tokenRef
          ),
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "TOKEN_MINT") {
      return {
        max_steps: 4,
        ops: [
          Object.assign(
            {
              op: "TOKEN_MINT",
              from: String(compiled.from || "caller").trim().toLowerCase(),
              to_arg: String(compiled.to_arg || "").trim(),
              amount_arg: String(compiled.arg || "").trim(),
            },
            tokenRef
          ),
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "TOKEN_BURN") {
      return {
        max_steps: 4,
        ops: [
          Object.assign(
            {
              op: "TOKEN_BURN",
              from: String(compiled.from || "caller").trim().toLowerCase(),
              amount_arg: String(compiled.arg || "").trim(),
            },
            tokenRef
          ),
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "TOKEN_CREATE") {
      return {
        max_steps: 4,
        ops: [
          {
            op: "TOKEN_CREATE",
            from: String(compiled.from || "caller").trim().toLowerCase(),
            name_arg: String(compiled.name_arg || "").trim(),
            symbol_arg: String(compiled.symbol_arg || "").trim(),
            decimals_arg: String(compiled.decimals_arg || "").trim(),
            max_supply_arg: String(compiled.max_supply_arg || "").trim(),
            initial_supply_arg: String(compiled.initial_supply_arg || "").trim(),
            to_arg: String(compiled.to_arg || "").trim(),
          },
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "SET_STR") {
      return {
        max_steps: 4,
        ops: [
          { op: "ARG_STR", dest: "r0", arg: String(compiled.arg || "").trim() },
          { op: "STORE_STR", key: String(compiled.key || "").trim(), src: "r0" },
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "SET_U64") {
      return {
        max_steps: 4,
        ops: [
          { op: "ARG_U64", dest: "r0", arg: String(compiled.arg || "").trim() },
          { op: "STORE_U64", key: String(compiled.key || "").trim(), src: "r0" },
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "ADD_U64") {
      return {
        max_steps: 8,
        ops: [
          { op: "LOAD_U64", dest: "r0", key: String(compiled.key || "").trim() },
          { op: "ARG_U64", dest: "r1", arg: String(compiled.arg || "").trim() },
          { op: "ADD_U64", dest: "r2", a: "r0", b: "r1" },
          { op: "STORE_U64", key: String(compiled.key || "").trim(), src: "r2" },
          { op: "RET_OK" },
        ],
      };
    }
    if (op === "SUB_U64") {
      return {
        max_steps: 8,
        ops: [
          { op: "LOAD_U64", dest: "r0", key: String(compiled.key || "").trim() },
          { op: "ARG_U64", dest: "r1", arg: String(compiled.arg || "").trim() },
          { op: "SUB_U64", dest: "r2", a: "r0", b: "r1" },
          { op: "STORE_U64", key: String(compiled.key || "").trim(), src: "r2" },
          { op: "RET_OK" },
        ],
      };
    }
    return null;
  }

  function deriveContractNameFromSource(source, lang) {
    if (lang === "solidity-like") {
      const m = String(source || "").match(/\bcontract\s+([A-Za-z_][A-Za-z0-9_]*)\b/);
      if (m && m[1]) return m[1];
    }
    const title = String(source || "").match(/^\s*#\s*@title\s+([A-Za-z_][A-Za-z0-9_]*)/im);
    if (title && title[1]) return title[1];
    return "";
  }

  function transpileContractSource(source, preferredLang, creator, nameOverride, outputModeRaw) {
    const src = String(source || "").trim();
    if (!src) throw new Error("Contract source required");

    const outputMode = normalizeContractOutputMode(outputModeRaw);
    const lang = detectContractDslLang(src, preferredLang);
    const parsed = parseContractStorageDeclarations(src, lang);
    const fnList = lang === "vyper-like" ? parseVyperFunctions(src) : parseSolidityFunctions(src);
    if (!fnList.length) {
      throw new Error("No functions found in source");
    }

    const methods = [];
    const abi = [];
    const warnings = [];
    const seen = new Set();
    fnList.forEach((fn) => {
      try {
        const compiled = compileContractMethod(fn, parsed.storageTypes);
        const methodKey = String(compiled.name || "").toLowerCase();
        if (seen.has(methodKey)) {
          warnings.push(`Duplicate method ignored: ${compiled.name}`);
          return;
        }
        seen.add(methodKey);
        methods.push(compiled);
        const typeHints = {};
        const setTypeHint = (rawName, typeName) => {
          const name = String(rawName || "").trim().toLowerCase();
          if (!name) return;
          typeHints[name] = String(typeName || "").trim().toLowerCase();
        };
        if (compiled.op === "SET_STR" && compiled.arg) {
          setTypeHint(compiled.arg, "string");
        } else {
          if (compiled.op === "TOKEN_CREATE") {
            setTypeHint(compiled.name_arg, "string");
            setTypeHint(compiled.symbol_arg, "string");
            setTypeHint(compiled.decimals_arg, "u64");
            setTypeHint(compiled.max_supply_arg, "u64");
            setTypeHint(compiled.initial_supply_arg, "u64");
            setTypeHint(compiled.to_arg, "address");
          }
          if (compiled.token_arg) {
            setTypeHint(compiled.token_arg, "string");
          }
          if (compiled.to_arg) {
            setTypeHint(compiled.to_arg, "address");
          }
          if (compiled.from_arg) {
            setTypeHint(compiled.from_arg, "address");
          }
          if (compiled.spender_arg) {
            setTypeHint(compiled.spender_arg, "address");
          }
          if (compiled.arg) {
            setTypeHint(compiled.arg, "u64");
          }
        }
        const args = (Array.isArray(compiled.params) ? compiled.params : []).map((p) => {
          const name = String(p || "").trim();
          const hinted = typeHints[name.toLowerCase()];
          if (hinted) {
            return { name, type: hinted };
          }
          const lower = name.toLowerCase();
          const isAddress = lower.includes("address") || lower === "to" || lower === "recipient" || lower === "from" || lower === "owner" || lower === "spender";
          const isString = lower.includes("string") || lower.includes("label") || lower.includes("name");
          return {
            name,
            type: isAddress ? "address" : (isString ? "string" : "u64"),
          };
        });
        abi.push({ name: compiled.name, args, returns: [] });
      } catch (err) {
        warnings.push(err instanceof Error ? err.message : String(err));
      }
    });

    if (!methods.length) {
      throw new Error("No supported methods transpiled (check function bodies)");
    }

    const derivedName = deriveContractNameFromSource(src, lang);
    const contractName = sanitizeContractDslName(nameOverride, derivedName);
    const creatorAddr = String(creator || "").trim();
    if (!creatorAddr) {
      throw new Error("Creator/from address required for CONTRACT_DEPLOY");
    }

    const storage = Object.entries(parsed.storageTypes || {}).map(([key, type]) => ({
      key,
      type: String(type || "").trim().toLowerCase() === "string" ? "string" : "u64",
      init: String((parsed.init && parsed.init[key]) || (String(type || "").trim().toLowerCase() === "string" ? "" : "0")),
    }));
    const logicMethods = methods.map((method) => {
      const compiled = legacyCompiledMethodToLogicOps(method);
      if (!compiled) {
        throw new Error(`Unsupported compiled op for method ${method.name}`);
      }
      return {
        name: method.name,
        max_steps: compiled.max_steps,
        ops: compiled.ops,
      };
    });
    const logicPack = {
      version: 1,
      name: contractName,
      abi,
      storage,
      methods: logicMethods,
      limits: {
        max_reads: 16,
        max_writes: 16,
        max_token_transfers: 4,
      },
    };

    if (outputMode === "logic_pack") {
      const payload = {
        creator: creatorAddr,
        name: contractName,
        lang: "dtl-script-v1",
        version: 2,
        logic_pack: logicPack,
      };
      return { payload, warnings, lang: "dtl-script-v1", contractName, outputMode };
    }

    const program = bytecodeProgramFromLogicPack(logicPack);
    const bytecodeHex = encodeDTLBytecodeProgramHex(program);
    const payload = {
      creator: creatorAddr,
      name: contractName,
      lang: "dtl-bytecode-v1",
      version: 2,
      bytecode: bytecodeHex,
      bytecode_format: DTL_BYTECODE_FORMAT,
      compiler: "dtl-solc-frontend/0.1",
      abi,
    };
    return { payload, warnings, lang: "dtl-bytecode-v1", contractName, outputMode: "dtl-bc-v1" };
  }

  function loadContractDslSample() {
    const selected = String((els.contractDslLang && els.contractDslLang.value) || "auto").trim().toLowerCase();
    const lang = selected === "vyper-like" ? "vyper-like" : "solidity-like";
    if (els.contractDslSource) {
      els.contractDslSource.value = contractDslSample(lang);
    }
    if (els.contractDslName && !String(els.contractDslName.value || "").trim()) {
      els.contractDslName.value = lang === "vyper-like" ? "VaultBook" : "Counter";
    }
    if (els.customCodeLang) els.customCodeLang.value = selected || "auto";
    if (els.customCodeEditor) els.customCodeEditor.value = String((els.contractDslSource && els.contractDslSource.value) || "");
    if (els.customCodeName && !String(els.customCodeName.value || "").trim()) {
      els.customCodeName.value = String((els.contractDslName && els.contractDslName.value) || "");
    }
    if (els.customCodeOutputMode && els.contractDslOutputMode) {
      els.customCodeOutputMode.value = String(els.contractDslOutputMode.value || "dtl-bc-v1");
    }
    scheduleDraftSave();
  }

  function setCustomCodeOutput(value) {
    if (!els.customCodeOut) return;
    if (typeof value === "string") {
      els.customCodeOut.textContent = value;
      return;
    }
    els.customCodeOut.textContent = asPretty(value);
  }

  function readCustomCodeTranspileInput() {
    const source = String((els.customCodeEditor && els.customCodeEditor.value) || "").trim();
    if (!source) throw new Error("Custom code required");
    const preferredLang = String((els.customCodeLang && els.customCodeLang.value) || "auto").trim();
    const nameOverride = String((els.customCodeName && els.customCodeName.value) || "").trim();
    const outputMode = normalizeContractOutputMode(String((els.customCodeOutputMode && els.customCodeOutputMode.value) || "dtl-bc-v1"));
    const creator = String(els.txFrom.value || (state.wallet && state.wallet.address) || "").trim();
    if (!creator) throw new Error("Set From address first (creator required)");
    return { source, preferredLang, nameOverride, creator, outputMode };
  }

  function payloadMethodNames(payloadObj) {
    const pack = extractLogicPackFromPayload(payloadObj);
    if (!pack || !Array.isArray(pack.methods)) return [];
    return pack.methods
      .map((m) => String(m && m.name ? m.name : "").trim())
      .filter(Boolean);
  }

  function payloadStorageKeys(payloadObj) {
    const pack = extractLogicPackFromPayload(payloadObj);
    if (!pack || !Array.isArray(pack.storage)) return [];
    return pack.storage
      .map((f) => String(f && f.key ? f.key : "").trim())
      .filter(Boolean);
  }

  function analyzeCustomCode() {
    const input = readCustomCodeTranspileInput();
    const out = transpileContractSource(
      input.source,
      input.preferredLang,
      input.creator,
      input.nameOverride,
      input.outputMode
    );
    state.customCodeLastPayload = out.payload;
    const methods = payloadMethodNames(out.payload);
    const storageKeys = payloadStorageKeys(out.payload);
    const summary = {
      status: "analyzed",
      lang: out.lang,
      output_mode: out.outputMode,
      contract: out.contractName,
      method_count: methods.length,
      methods,
      storage_keys: storageKeys,
      warnings: out.warnings,
    };
    setCustomCodeOutput(summary);
    scheduleDraftSave();
    return out;
  }

  function applyCustomCodeAsDeployPayload() {
    const out = analyzeCustomCode();
    if (els.dtlType) {
      els.dtlType.value = "CONTRACT_DEPLOY";
    }
    syncGuidedVisibility();
    els.txPayload.value = asPretty(out.payload);
    setPayloadPreview(out.payload);
    prefillLogicSimulatorFromPayload(out.payload);
    if (els.contractDslLang) els.contractDslLang.value = String((els.customCodeLang && els.customCodeLang.value) || "auto");
    if (els.contractDslName) els.contractDslName.value = String((els.customCodeName && els.customCodeName.value) || "");
    if (els.contractDslOutputMode) els.contractDslOutputMode.value = String((els.customCodeOutputMode && els.customCodeOutputMode.value) || "dtl-bc-v1");
    if (els.contractDslSource) els.contractDslSource.value = String((els.customCodeEditor && els.customCodeEditor.value) || "");
    els.txOut.textContent = asPretty({
      status: "custom_code_payload_ready",
      contract: out.contractName,
      output_mode: out.outputMode,
      methods: payloadMethodNames(out.payload).length,
      warnings: out.warnings,
    });
    refreshWorkspaceMeta();
    scheduleDraftSave();
  }

  function copyCustomCodePayload() {
    if (!state.customCodeLastPayload) {
      analyzeCustomCode();
    }
    copyValueToClipboard(asPretty(state.customCodeLastPayload || {}), "Custom deploy payload copied.");
  }

  function loadCustomCodeSample() {
    const selected = String((els.customCodeLang && els.customCodeLang.value) || "auto").trim().toLowerCase();
    const lang = selected === "vyper-like" ? "vyper-like" : "solidity-like";
    if (els.customCodeEditor) {
      els.customCodeEditor.value = contractDslSample(lang);
    }
    if (els.customCodeName && !String(els.customCodeName.value || "").trim()) {
      els.customCodeName.value = lang === "vyper-like" ? "VaultBook" : "Counter";
    }
    if (els.contractDslOutputMode && els.customCodeOutputMode) {
      els.contractDslOutputMode.value = String(els.customCodeOutputMode.value || "dtl-bc-v1");
    }
    setCustomCodeOutput("Sample loaded. Click Analyze Code.");
    scheduleDraftSave();
  }

  function loadMSC20ContractTemplate() {
    const selected = String((els.customCodeLang && els.customCodeLang.value) || "auto").trim().toLowerCase();
    const lang = selected === "vyper-like" ? "vyper-like" : "solidity-like";
    const source = contractDslMsc20Template(lang);
    if (els.customCodeLang) els.customCodeLang.value = lang;
    if (els.customCodeName) els.customCodeName.value = "MSC20Controller";
    if (els.customCodeEditor) els.customCodeEditor.value = source;
    if (els.contractDslLang) els.contractDslLang.value = lang;
    if (els.contractDslName) els.contractDslName.value = "MSC20Controller";
    if (els.customCodeOutputMode) els.customCodeOutputMode.value = normalizeContractOutputMode(String((els.customCodeOutputMode && els.customCodeOutputMode.value) || "dtl-bc-v1"));
    if (els.contractDslOutputMode) els.contractDslOutputMode.value = normalizeContractOutputMode(String((els.customCodeOutputMode && els.customCodeOutputMode.value) || "dtl-bc-v1"));
    if (els.contractDslSource) els.contractDslSource.value = source;
    if (els.dtlType) els.dtlType.value = "CONTRACT_DEPLOY";
    syncGuidedVisibility();
    setCustomCodeOutput({
      status: "template_loaded",
      template: "MSC20 contract",
      note: "Edit function args as needed, then click Analyze Code -> Use as Deploy Payload.",
    });
    scheduleDraftSave();
  }

  async function loadNFTPayloadTemplate(kind, label) {
    if (els.dtlType) els.dtlType.value = kind;
    syncGuidedVisibility();
    await loadPayloadTemplate();
    setCustomCodeOutput({
      status: "template_loaded",
      template: label,
      mode: "DTL transaction payload",
      note: "NFT templates are payload-based in current DTL (not Solidity-like contract transpile).",
    });
    scheduleDraftSave();
  }

  function transpileContractDslToPayload() {
    const source = String((els.contractDslSource && els.contractDslSource.value) || "").trim();
    if (!source) throw new Error("Paste Solidity/Vyper-like source first");
    const preferredLang = String((els.contractDslLang && els.contractDslLang.value) || "auto").trim();
    const creator = String(els.txFrom.value || (state.wallet && state.wallet.address) || "").trim();
    if (!creator) throw new Error("Set From address first (creator required)");
    const nameOverride = String((els.contractDslName && els.contractDslName.value) || "").trim();
    const outputMode = normalizeContractOutputMode(String((els.contractDslOutputMode && els.contractDslOutputMode.value) || "dtl-bc-v1"));

    const out = transpileContractSource(source, preferredLang, creator, nameOverride, outputMode);
    if (els.dtlType) {
      els.dtlType.value = "CONTRACT_DEPLOY";
    }
    syncGuidedVisibility();
    els.txPayload.value = asPretty(out.payload);
    setPayloadPreview(out.payload);
    els.txOut.textContent = asPretty({
      status: "transpiled",
      lang: out.lang,
      output_mode: out.outputMode,
      contract: out.contractName,
      methods: payloadMethodNames(out.payload).length,
      warnings: out.warnings,
    });
    if (els.customCodeLang) els.customCodeLang.value = String((els.contractDslLang && els.contractDslLang.value) || "auto");
    if (els.customCodeName) els.customCodeName.value = String((els.contractDslName && els.contractDslName.value) || "");
    if (els.customCodeOutputMode) els.customCodeOutputMode.value = String((els.contractDslOutputMode && els.contractDslOutputMode.value) || "dtl-bc-v1");
    if (els.customCodeEditor) els.customCodeEditor.value = source;
    state.customCodeLastPayload = out.payload;
    setCustomCodeOutput({
      status: "analyzed",
      lang: out.lang,
      output_mode: out.outputMode,
      contract: out.contractName,
      method_count: payloadMethodNames(out.payload).length,
      warnings: out.warnings,
    });
    prefillLogicSimulatorFromPayload(out.payload);
    scheduleDraftSave();
  }

  function normalizeLogicMethodName(raw) {
    return String(raw || "").trim().toLowerCase();
  }

  function extractLogicPackFromPayload(payloadObj) {
    if (!payloadObj || typeof payloadObj !== "object") return null;
    const pack = payloadObj.logic_pack;
    if (pack && typeof pack === "object" && Array.isArray(pack.methods) && pack.methods.length) {
      return pack;
    }
    const bytecode = String(payloadObj.bytecode || "").trim();
    const format = String(payloadObj.bytecode_format || "").trim().toLowerCase();
    if (!bytecode || format !== DTL_BYTECODE_FORMAT) return null;
    try {
      const program = decodeDTLBytecodeProgramHex(bytecode);
      return logicPackFromBytecodeProgram(program);
    } catch (_) {
      return null;
    }
  }

  function findLogicPackMethod(pack, methodName) {
    const want = normalizeLogicMethodName(methodName);
    if (!pack || !Array.isArray(pack.methods)) return null;
    for (const method of pack.methods) {
      const current = normalizeLogicMethodName(method && method.name);
      if (current && current === want) return method;
    }
    return null;
  }

  function findLogicPackABIMethod(pack, methodName) {
    const want = normalizeLogicMethodName(methodName);
    if (!pack || !Array.isArray(pack.abi)) return null;
    for (const method of pack.abi) {
      const current = normalizeLogicMethodName(method && method.name);
      if (current && current === want) return method;
    }
    return null;
  }

  function defaultArgsForLogicABI(abi, fallbackAddress) {
    const args = {};
    if (!abi || !Array.isArray(abi.args)) return args;
    abi.args.forEach((arg) => {
      const name = normalizeLogicMethodName(arg && arg.name);
      if (!name) return;
      const typ = normalizeLogicMethodName(arg && arg.type);
      if (typ === "u64") {
        args[name] = "1";
      } else if (typ === "bool") {
        args[name] = "false";
      } else if (typ === "address") {
        args[name] = String(fallbackAddress || "MSC0000000000000000000000000000000000000000").trim();
      } else {
        args[name] = "";
      }
    });
    return args;
  }

  function setLogicSimOutput(value) {
    if (!els.logicSimOut) return;
    if (typeof value === "string") {
      els.logicSimOut.textContent = value;
      return;
    }
    els.logicSimOut.textContent = asPretty(value);
  }

  function prefillLogicSimulatorFromPayload(payloadObj) {
    if (!els.logicSimMethod || !els.logicSimArgs) return;
    const pack = extractLogicPackFromPayload(payloadObj);
    if (!pack) {
      els.logicSimMethod.innerHTML = "";
      setLogicSimOutput("No executable runtime found in payload (logic_pack or dtl-bc-v1).");
      return;
    }
    const previous = normalizeLogicMethodName(els.logicSimMethod.value);
    const names = Array.isArray(pack.methods)
      ? pack.methods
          .map((m) => normalizeLogicMethodName(m && m.name))
          .filter(Boolean)
      : [];
    names.sort();

    els.logicSimMethod.innerHTML = "";
    names.forEach((name) => {
      const opt = document.createElement("option");
      opt.value = name;
      opt.textContent = name;
      els.logicSimMethod.appendChild(opt);
    });
    if (!names.length) {
      setLogicSimOutput("Runtime has no methods.");
      return;
    }
    const chosen = names.includes(previous) ? previous : names[0];
    els.logicSimMethod.value = chosen;

    const fromFallback = String((els.txFrom && els.txFrom.value) || state.walletAccount || "").trim();
    const abi = findLogicPackABIMethod(pack, chosen);
    const sampleArgs = defaultArgsForLogicABI(abi, fromFallback);
    els.logicSimArgs.value = asPretty(sampleArgs);
    setLogicSimOutput({
      status: "runtime_ready",
      method: chosen,
      method_count: names.length,
    });
  }

  function refreshLogicSimulatorArgsForSelectedMethod() {
    if (!els.logicSimMethod || !els.logicSimArgs) return;
    const payload = parseJSONField(els.txPayload && els.txPayload.value, {});
    const pack = extractLogicPackFromPayload(payload);
    if (!pack) return;
    const method = normalizeLogicMethodName(els.logicSimMethod.value);
    if (!method) return;
    const fromFallback = String((els.txFrom && els.txFrom.value) || state.walletAccount || "").trim();
    const abi = findLogicPackABIMethod(pack, method);
    const args = defaultArgsForLogicABI(abi, fromFallback);
    els.logicSimArgs.value = asPretty(args);
  }

  function parseLogicSimArgs(raw) {
    const argsRaw = String(raw || "").trim();
    if (!argsRaw) return {};
    const parsed = JSON.parse(argsRaw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("Simulator args must be JSON object");
    }
    const out = {};
    Object.keys(parsed).forEach((k) => {
      const key = normalizeLogicMethodName(k);
      if (!key) return;
      const value = parsed[k];
      if (value === null || value === undefined) {
        out[key] = "";
      } else {
        out[key] = String(value).trim();
      }
    });
    return out;
  }

  function logicSimParseU64(raw, label) {
    const text = String(raw || "").trim();
    if (!/^[0-9]+$/.test(text)) {
      throw new Error(`${label} must be uint64`);
    }
    const n = BigInt(text);
    if (n < 0n || n > U64_MAX) {
      throw new Error(`${label} out of uint64 range`);
    }
    return n;
  }

  function logicSimToString(v) {
    if (!v || typeof v !== "object") return "";
    if (v.kind === "u64") return String(v.u64);
    if (v.kind === "bool") return v.b ? "true" : "false";
    if (v.kind === "string") return String(v.str || "");
    return "";
  }

  function logicSimCloneRegs(regs) {
    const out = {};
    Object.keys(regs)
      .sort()
      .forEach((key) => {
        const value = regs[key];
        out[key] = {
          kind: value.kind,
          value: logicSimToString(value),
        };
      });
    return out;
  }

  function logicSimCloneStorage(storage) {
    const out = {};
    Object.keys(storage)
      .sort()
      .forEach((key) => {
        out[key] = String(storage[key] || "");
      });
    return out;
  }

  function logicSimReadReg(regs, regName) {
    const reg = normalizeLogicMethodName(regName);
    if (!reg || !Object.prototype.hasOwnProperty.call(regs, reg)) {
      throw new Error(`register not initialized: ${regName}`);
    }
    return regs[reg];
  }

  function logicSimReadRegAsU64(regs, regName) {
    const v = logicSimReadReg(regs, regName);
    if (v.kind !== "u64") throw new Error(`register ${regName} is not u64`);
    return v.u64;
  }

  function logicSimReadRegAsStr(regs, regName) {
    const v = logicSimReadReg(regs, regName);
    if (v.kind !== "string") throw new Error(`register ${regName} is not string`);
    return v.str;
  }

  function logicSimReadRegAsBool(regs, regName) {
    const v = logicSimReadReg(regs, regName);
    if (v.kind !== "bool") throw new Error(`register ${regName} is not bool`);
    return v.b;
  }

  function logicSimExecute(pack, methodName, argsInput) {
    const method = findLogicPackMethod(pack, methodName);
    if (!method) throw new Error(`Unknown logic method: ${methodName}`);
    const abi = findLogicPackABIMethod(pack, methodName);
    if (!abi) throw new Error(`ABI missing for method: ${methodName}`);

    const args = {};
    Object.keys(argsInput || {}).forEach((k) => {
      const key = normalizeLogicMethodName(k);
      if (!key) return;
      args[key] = String(argsInput[k] || "").trim();
    });
    (Array.isArray(abi.args) ? abi.args : []).forEach((arg) => {
      const name = normalizeLogicMethodName(arg && arg.name);
      const typ = normalizeLogicMethodName(arg && arg.type);
      if (!name) return;
      if (!Object.prototype.hasOwnProperty.call(args, name)) {
        throw new Error(`missing arg: ${name}`);
      }
      if (typ === "u64") {
        logicSimParseU64(args[name], `arg ${name}`);
      } else if (typ === "bool") {
        const low = args[name].toLowerCase();
        if (low !== "true" && low !== "false") {
          throw new Error(`arg ${name} must be bool`);
        }
      } else if (typ === "address" && !String(args[name] || "").trim()) {
        throw new Error(`arg ${name} must be address/string`);
      }
    });

    const storageTypes = {};
    const storage = {};
    (Array.isArray(pack.storage) ? pack.storage : []).forEach((field) => {
      const key = normalizeLogicMethodName(field && field.key);
      if (!key) return;
      const typ = normalizeLogicMethodName(field && field.type);
      const init = String((field && field.init) || "").trim();
      storageTypes[key] = typ;
      if (typ === "u64") {
        storage[key] = String(logicSimParseU64(init || "0", `storage ${key}`));
      } else if (typ === "bool") {
        storage[key] = init.toLowerCase() === "true" ? "true" : "false";
      } else {
        storage[key] = init;
      }
    });

    const regs = {};
    const trace = [];
    const transferIntents = [];
    const limits = pack && pack.limits && typeof pack.limits === "object" ? pack.limits : {};
    const maxReads = Math.max(1, Number.parseInt(String(limits.max_reads || "16"), 10) || 16);
    const maxWrites = Math.max(1, Number.parseInt(String(limits.max_writes || "16"), 10) || 16);
    const maxTransfers = Math.max(1, Number.parseInt(String(limits.max_token_transfers || "4"), 10) || 4);
    const maxSteps = Math.max(
      1,
      Number.parseInt(String(method.max_steps || 0), 10) || ((Array.isArray(method.ops) ? method.ops.length : 0) + 1)
    );
    const resolveTokenRef = (opObj) => {
      const tokenArg = normalizeLogicMethodName(opObj && opObj.token_arg);
      const rawRef = tokenArg ? args[tokenArg] : String((opObj && opObj.token_id) || "");
      const tokenRef = String(rawRef || "").trim().toLowerCase();
      if (!tokenRef) throw new Error("token reference required");
      return tokenRef;
    };
    const resolveAddressArg = (argName, label) => {
      const key = normalizeLogicMethodName(argName);
      const value = String(args[key] || "").trim();
      if (!value) throw new Error(`invalid ${label}`);
      return value;
    };
    const resolveU64Arg = (argName, label, allowZero) => {
      const key = normalizeLogicMethodName(argName);
      const value = logicSimParseU64(args[key], `${label} ${key}`);
      if (!allowZero && value <= 0n) {
        throw new Error(`invalid ${label}`);
      }
      return value;
    };
    const resolveFromMode = (raw) => {
      const mode = String(raw || "caller").trim().toLowerCase() || "caller";
      if (mode !== "caller" && mode !== "contract") {
        throw new Error("invalid from mode");
      }
      return mode;
    };

    const ops = Array.isArray(method.ops) ? method.ops : [];
    let reads = 0;
    let writes = 0;
    let transfers = 0;
    let pc = 0;
    let steps = 0;
    while (pc >= 0 && pc < ops.length) {
      steps += 1;
      if (steps > maxSteps) {
        throw new Error("logic method step limit exceeded");
      }
      const op = ops[pc] || {};
      const opName = String(op.op || "").trim().toUpperCase();
      const stepInfo = {
        step: steps,
        pc,
        op: opName,
        reads,
        writes,
        transfers,
      };
      switch (opName) {
        case "ARG_U64": {
          const argName = normalizeLogicMethodName(op.arg);
          regs[normalizeLogicMethodName(op.dest)] = { kind: "u64", u64: logicSimParseU64(args[argName], `arg ${argName}`) };
          pc += 1;
          break;
        }
        case "ARG_STR": {
          const argName = normalizeLogicMethodName(op.arg);
          regs[normalizeLogicMethodName(op.dest)] = { kind: "string", str: String(args[argName] || "").trim() };
          pc += 1;
          break;
        }
        case "LOAD_U64": {
          reads += 1;
          if (reads > maxReads) throw new Error("logic read limit exceeded");
          const key = normalizeLogicMethodName(op.key);
          if (storageTypes[key] !== "u64") throw new Error(`storage key ${key} is not u64`);
          regs[normalizeLogicMethodName(op.dest)] = { kind: "u64", u64: logicSimParseU64(storage[key] || "0", `storage ${key}`) };
          pc += 1;
          break;
        }
        case "LOAD_STR": {
          reads += 1;
          if (reads > maxReads) throw new Error("logic read limit exceeded");
          const key = normalizeLogicMethodName(op.key);
          if (storageTypes[key] !== "string") throw new Error(`storage key ${key} is not string`);
          regs[normalizeLogicMethodName(op.dest)] = { kind: "string", str: String(storage[key] || "") };
          pc += 1;
          break;
        }
        case "STORE_U64": {
          writes += 1;
          if (writes > maxWrites) throw new Error("logic write limit exceeded");
          const key = normalizeLogicMethodName(op.key);
          if (storageTypes[key] !== "u64") throw new Error(`storage key ${key} is not u64`);
          const n = logicSimReadRegAsU64(regs, op.src);
          storage[key] = n.toString(10);
          pc += 1;
          break;
        }
        case "STORE_STR": {
          writes += 1;
          if (writes > maxWrites) throw new Error("logic write limit exceeded");
          const key = normalizeLogicMethodName(op.key);
          if (storageTypes[key] !== "string") throw new Error(`storage key ${key} is not string`);
          storage[key] = logicSimReadRegAsStr(regs, op.src);
          pc += 1;
          break;
        }
        case "ADD_U64":
        case "SUB_U64":
        case "MUL_U64":
        case "DIV_U64": {
          const a = logicSimReadRegAsU64(regs, op.a);
          const b = logicSimReadRegAsU64(regs, op.b);
          let out = 0n;
          if (opName === "ADD_U64") {
            out = a + b;
            if (out > U64_MAX) throw new Error("uint64 overflow");
          } else if (opName === "SUB_U64") {
            if (b > a) throw new Error("contract subtraction underflow");
            out = a - b;
          } else if (opName === "MUL_U64") {
            out = a * b;
            if (out > U64_MAX) throw new Error("uint64 overflow");
          } else {
            if (b === 0n) throw new Error("division by zero");
            out = a / b;
          }
          regs[normalizeLogicMethodName(op.dest)] = { kind: "u64", u64: out };
          pc += 1;
          break;
        }
        case "CMP_EQ":
        case "CMP_NEQ": {
          const av = logicSimReadReg(regs, op.a);
          const bv = logicSimReadReg(regs, op.b);
          if (av.kind !== bv.kind) throw new Error(`compare type mismatch: ${av.kind} vs ${bv.kind}`);
          let eq = false;
          if (av.kind === "u64") eq = av.u64 === bv.u64;
          if (av.kind === "string") eq = av.str === bv.str;
          if (av.kind === "bool") eq = av.b === bv.b;
          if (opName === "CMP_NEQ") eq = !eq;
          regs[normalizeLogicMethodName(op.dest)] = { kind: "bool", b: eq };
          pc += 1;
          break;
        }
        case "CMP_GT":
        case "CMP_GTE":
        case "CMP_LT":
        case "CMP_LTE": {
          const a = logicSimReadRegAsU64(regs, op.a);
          const b = logicSimReadRegAsU64(regs, op.b);
          let result = false;
          if (opName === "CMP_GT") result = a > b;
          if (opName === "CMP_GTE") result = a >= b;
          if (opName === "CMP_LT") result = a < b;
          if (opName === "CMP_LTE") result = a <= b;
          regs[normalizeLogicMethodName(op.dest)] = { kind: "bool", b: result };
          pc += 1;
          break;
        }
        case "JMP_IF": {
          const cond = logicSimReadRegAsBool(regs, op.cond);
          const target = Number.parseInt(String(op.target || 0), 10);
          if (cond) {
            if (!Number.isInteger(target) || target < 0 || target >= ops.length) {
              throw new Error("jump target out of range");
            }
            pc = target;
          } else {
            pc += 1;
          }
          break;
        }
        case "JMP": {
          const target = Number.parseInt(String(op.target || 0), 10);
          if (!Number.isInteger(target) || target < 0 || target >= ops.length) {
            throw new Error("jump target out of range");
          }
          pc = target;
          break;
        }
        case "ASSERT": {
          const cond = logicSimReadRegAsBool(regs, op.cond);
          if (!cond) {
            const msg = String(op.message || "").trim() || "logic assert failed";
            throw new Error(msg);
          }
          pc += 1;
          break;
        }
        case "TOKEN_TRANSFER": {
          transfers += 1;
          if (transfers > maxTransfers) throw new Error("logic transfer limit exceeded");
          const tokenRef = resolveTokenRef(op);
          const to = resolveAddressArg(op.to_arg, "transfer recipient");
          const amount = resolveU64Arg(op.amount_arg || op.arg, "amount", false);
          transferIntents.push({
            op: "TOKEN_TRANSFER",
            token_ref: tokenRef,
            token_id: tokenRef,
            from: resolveFromMode(op.from),
            to,
            amount: amount.toString(10),
          });
          pc += 1;
          break;
        }
        case "TOKEN_APPROVE": {
          transfers += 1;
          if (transfers > maxTransfers) throw new Error("logic transfer limit exceeded");
          const tokenRef = resolveTokenRef(op);
          const spender = resolveAddressArg(op.spender_arg, "spender");
          const amount = resolveU64Arg(op.amount_arg || op.arg, "amount", true);
          transferIntents.push({
            op: "TOKEN_APPROVE",
            token_ref: tokenRef,
            token_id: tokenRef,
            owner: resolveFromMode(op.from),
            spender,
            amount: amount.toString(10),
          });
          pc += 1;
          break;
        }
        case "TOKEN_TRANSFER_FROM": {
          transfers += 1;
          if (transfers > maxTransfers) throw new Error("logic transfer limit exceeded");
          const tokenRef = resolveTokenRef(op);
          const from = resolveAddressArg(op.from_arg, "from account");
          const to = resolveAddressArg(op.to_arg, "to account");
          const amount = resolveU64Arg(op.amount_arg || op.arg, "amount", false);
          transferIntents.push({
            op: "TOKEN_TRANSFER_FROM",
            token_ref: tokenRef,
            token_id: tokenRef,
            spender: resolveFromMode(op.from),
            from,
            to,
            amount: amount.toString(10),
          });
          pc += 1;
          break;
        }
        case "TOKEN_MINT": {
          transfers += 1;
          if (transfers > maxTransfers) throw new Error("logic transfer limit exceeded");
          const tokenRef = resolveTokenRef(op);
          const to = resolveAddressArg(op.to_arg, "mint recipient");
          const amount = resolveU64Arg(op.amount_arg || op.arg, "amount", false);
          transferIntents.push({
            op: "TOKEN_MINT",
            token_ref: tokenRef,
            token_id: tokenRef,
            authority: resolveFromMode(op.from),
            to,
            amount: amount.toString(10),
          });
          pc += 1;
          break;
        }
        case "TOKEN_BURN": {
          transfers += 1;
          if (transfers > maxTransfers) throw new Error("logic transfer limit exceeded");
          const tokenRef = resolveTokenRef(op);
          const amount = resolveU64Arg(op.amount_arg || op.arg, "amount", false);
          transferIntents.push({
            op: "TOKEN_BURN",
            token_ref: tokenRef,
            token_id: tokenRef,
            from: resolveFromMode(op.from),
            amount: amount.toString(10),
          });
          pc += 1;
          break;
        }
        case "TOKEN_CREATE": {
          transfers += 1;
          if (transfers > maxTransfers) throw new Error("logic transfer limit exceeded");
          const name = String(args[normalizeLogicMethodName(op.name_arg)] || "").trim();
          const symbol = String(args[normalizeLogicMethodName(op.symbol_arg)] || "").trim();
          const decimals = resolveU64Arg(op.decimals_arg, "decimals", true);
          const maxSupply = resolveU64Arg(op.max_supply_arg, "max_supply", true);
          const initialSupply = resolveU64Arg(op.initial_supply_arg || op.amount_arg, "initial_supply", true);
          const owner = op.to_arg ? resolveAddressArg(op.to_arg, "owner") : resolveFromMode(op.from);
          if (!name) throw new Error("invalid token name");
          if (!symbol) throw new Error("invalid token symbol");
          if (decimals > 18n) throw new Error("decimals out of range");
          if (maxSupply > 0n && initialSupply > maxSupply) throw new Error("initial_supply exceeds max_supply");
          transferIntents.push({
            op: "TOKEN_CREATE",
            creator: resolveFromMode(op.from),
            owner,
            name,
            symbol,
            decimals: decimals.toString(10),
            max_supply: maxSupply.toString(10),
            initial_supply: initialSupply.toString(10),
          });
          pc += 1;
          break;
        }
        case "RET_OK":
          trace.push({
            ...stepInfo,
            result: "ok",
            registers: logicSimCloneRegs(regs),
            storage: logicSimCloneStorage(storage),
            transfer_intents: transferIntents.slice(),
          });
          return {
            status: "ok",
            method: normalizeLogicMethodName(method.name),
            steps,
            reads,
            writes,
            transfers,
            limits: {
              max_reads: maxReads,
              max_writes: maxWrites,
              max_token_transfers: maxTransfers,
              max_steps: maxSteps,
            },
            registers: logicSimCloneRegs(regs),
            storage: logicSimCloneStorage(storage),
            transfer_intents: transferIntents,
            trace,
          };
        case "RET_ERR": {
          const errMsg = String(op.message || "").trim() || "contract method returned error";
          throw new Error(errMsg);
        }
        default:
          throw new Error(`unsupported logic opcode: ${opName || "unknown"}`);
      }
      trace.push({
        ...stepInfo,
        next_pc: pc,
        registers: logicSimCloneRegs(regs),
        storage: logicSimCloneStorage(storage),
        transfer_intents: transferIntents.slice(),
      });
    }
    throw new Error("logic method terminated without return");
  }

  function simulateLogicPackTrace() {
    const payload = parseJSONField(els.txPayload && els.txPayload.value, {});
    const pack = extractLogicPackFromPayload(payload);
    if (!pack) {
      throw new Error("Current payload has no executable runtime (logic_pack or dtl-bc-v1)");
    }
    const method = normalizeLogicMethodName(els.logicSimMethod && els.logicSimMethod.value);
    if (!method) {
      throw new Error("Select logic method for simulation");
    }
    const args = parseLogicSimArgs(els.logicSimArgs && els.logicSimArgs.value);
    const out = logicSimExecute(pack, method, args);
    setLogicSimOutput(out);
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
    if (dtlType === "CONTRACT_DEPLOY") {
      assertMSCAddress("payload.creator", payloadObj.creator || tx.from);
      return;
    }
    if (dtlType === "CONTRACT_CALL") {
      assertMSCAddress("payload.caller", payloadObj.caller || tx.from);
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
    if (dtlType === "CONTRACT_DEPLOY") {
      if (isHexAddress(payloadObj.creator)) {
        payloadObj.creator = from;
      }
      return payloadObj;
    }
    if (dtlType === "CONTRACT_CALL") {
      if (isHexAddress(payloadObj.caller)) {
        payloadObj.caller = from;
      }
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
    if (dtlType === "CONTRACT_DEPLOY") {
      if (!String(payloadObj.name || "").trim()) throw new Error("CONTRACT_DEPLOY requires name");
      if (!String(payloadObj.lang || "").trim()) throw new Error("CONTRACT_DEPLOY requires lang");
      const hasLegacyMethods = Array.isArray(payloadObj.methods) && payloadObj.methods.length > 0;
      const hasLogicPackMethods = !!(
        payloadObj.logic_pack &&
        Array.isArray(payloadObj.logic_pack.methods) &&
        payloadObj.logic_pack.methods.length > 0
      );
      const hasBytecode = !!(String(payloadObj.bytecode || "").trim());
      const sourceCount = [hasLegacyMethods, hasLogicPackMethods, hasBytecode].filter(Boolean).length;
      if (sourceCount !== 1) {
        throw new Error("CONTRACT_DEPLOY requires exactly one executable source: methods or logic_pack.methods or bytecode");
      }
      if (hasBytecode) {
        if (String(payloadObj.bytecode_format || "").trim().toLowerCase() !== DTL_BYTECODE_FORMAT) {
          throw new Error("CONTRACT_DEPLOY bytecode requires bytecode_format=dtl-bc-v1");
        }
        if (state.bytecodeRuntimeActive === false && els.txOut) {
          els.txOut.textContent =
            "Warning: Bytecode runtime is inactive on this node. Submit is blocked for bytecode deploy; switch Output Mode to logic_pack or enable dtl.bytecode_runtime_enabled and activation height.";
        }
      }
    }
    if (dtlType === "CONTRACT_CALL") {
      if (!String(payloadObj.contract_id || "").trim()) throw new Error("CONTRACT_CALL requires contract_id");
      if (!String(payloadObj.method || "").trim()) throw new Error("CONTRACT_CALL requires method");
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
    // compat subset exposes msc_* read methods; probe without mutating state.
    try {
      await rpc("msc_chainId", [], true);
      state.compatSubset = true;
    } catch (_) {
      state.compatSubset = false;
    }
    state.bytecodeRuntimeEnabled = null;
    state.bytecodeRuntimeActive = null;
    state.bytecodeActivationHeight = null;
    state.contractRuntimeRemoved = true;
    try {
      const status = await getJSON("/status");
      if (status && typeof status === "object") {
        if (typeof status.dtl_contract_runtime_removed === "boolean") {
          state.contractRuntimeRemoved = status.dtl_contract_runtime_removed;
        }
        if (typeof status.dtl_bytecode_runtime_enabled === "boolean") {
          state.bytecodeRuntimeEnabled = status.dtl_bytecode_runtime_enabled;
        }
        if (typeof status.dtl_bytecode_runtime_active === "boolean") {
          state.bytecodeRuntimeActive = status.dtl_bytecode_runtime_active;
        }
        if (status.dtl_bytecode_activation_height !== undefined && status.dtl_bytecode_activation_height !== null) {
          const parsed = Number.parseInt(String(status.dtl_bytecode_activation_height), 10);
          if (Number.isFinite(parsed) && parsed >= 0) {
            state.bytecodeActivationHeight = parsed;
          }
        }
      }
    } catch (_) {
      // /status may require auth; keep bytecode badge in unknown state when unavailable.
    }
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
    prefillLogicSimulatorFromPayload(payload);
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

  function enforceBytecodeRuntimeForSubmit(tx) {
    if (!tx || typeof tx !== "object") return;
    if (String(tx.dtl_tx_type || "").trim() !== "CONTRACT_DEPLOY") return;
    if (state.bytecodeRuntimeActive !== false) return;
    let payloadObj = null;
    try {
      payloadObj = JSON.parse(String(tx.dtl_payload || "{}"));
    } catch (_) {
      payloadObj = null;
    }
    if (!payloadObj || typeof payloadObj !== "object") return;
    const hasBytecode = String(payloadObj.bytecode || "").trim() !== "";
    if (!hasBytecode) return;
    throw new Error(
      "Bytecode deploy blocked: node reports dtl_bytecode_runtime_active=false. Switch Output Mode to logic_pack or enable dtl.bytecode_runtime_enabled and activation height."
    );
  }

  async function submitDTL() {
    await submitWithNonceRetry(async (retried) => {
      const builtTx = await buildTxObject();
      const tx = await ensureSignedForSubmit(builtTx);
      enforceBytecodeRuntimeForSubmit(tx);
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
      const dtlType = String(tx.dtl_tx_type || "").trim();
      const out = {
        status: "accepted",
        via: "dtl_submit",
        tx_id: txID,
      };
      if (dtlType === "CONTRACT_DEPLOY") {
        const deployMeta = await resolveDeployMetadataForTx(tx, acceptedTxID, out);
        const resolved = applyDeployMetadata(deployMeta, true);
        if (resolved.contractID) {
          out.contract_id = resolved.contractID;
        }
        if (resolved.logicHash) {
          out.logic_hash = resolved.logicHash;
          out.logic_pack_hash = resolved.logicHash;
        }
      }
      if (signedTxID && txID && signedTxID.toLowerCase() !== String(txID).toLowerCase()) {
        out.signed_tx_id = signedTxID;
      }
      if (retried) {
        out.nonce_synced = true;
        out.nonce = Number.parseInt(String(els.txNonce.value || "0"), 10) || null;
      }
      els.txOut.textContent = asPretty(out);
      scheduleDraftSave();
      return out;
    });
  }

  async function submitRaw() {
    await submitWithNonceRetry(async (retried) => {
      const builtTx = await buildTxObject();
      const tx = await ensureSignedForSubmit(builtTx);
      enforceBytecodeRuntimeForSubmit(tx);
      requireSignedDTLTx(tx);
      if (tx && tx.id) {
        rememberLastTxID(String(tx.id || "").trim());
      }
      const dtlType = String(tx.dtl_tx_type || "").trim();
      const res = await fetch(endpoint("/submitTx"), {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify(tx),
      });
      const text = await res.text();
      if (!res.ok) throw new Error(`HTTP ${res.status} ${text}`);
      let out = text;
      try {
        out = JSON.parse(text);
        if (out && out.tx_id) {
          els.statusTxId.value = out.tx_id;
          rememberLastTxID(String(out.tx_id || "").trim());
        }
        if (dtlType === "CONTRACT_DEPLOY") {
          const deployMeta = await resolveDeployMetadataForTx(tx, out && out.tx_id ? out.tx_id : tx.id, out);
          const resolved = applyDeployMetadata(deployMeta, true);
          if (out && typeof out === "object") {
            if (resolved.contractID) {
              out.contract_id = resolved.contractID;
            }
            if (resolved.logicHash) {
              out.logic_hash = resolved.logicHash;
              out.logic_pack_hash = resolved.logicHash;
            }
          }
        }
        if (retried && out && typeof out === "object") {
          out.nonce_synced = true;
          out.nonce = Number.parseInt(String(els.txNonce.value || "0"), 10) || null;
        }
      } catch (_) {
        // keep text
      }
      if (dtlType === "CONTRACT_DEPLOY" && typeof out === "string") {
        const deployMeta = await resolveDeployMetadataForTx(tx, tx.id, null);
        const resolved = applyDeployMetadata(deployMeta, true);
        if (resolved.contractID) {
          out = `${out}\ncontract_id=${resolved.contractID}`;
          if (resolved.logicHash) {
            out = `${out}\nlogic_hash=${resolved.logicHash}`;
          }
        }
      }
      if (retried && typeof out === "string") {
        out = `${out}\n(nonce synced to ${String(els.txNonce.value || "").trim()} and retried)`;
      }
      els.txOut.textContent = typeof out === "string" ? out : asPretty(out);
      scheduleDraftSave();
      return out;
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
    const out = await getJSON(`/tx/status?tx_id=${encodeURIComponent(txID)}`);
    const deployMeta = extractDeployMetadata(out);
    if (deployMeta.contractID || deployMeta.logicHash) {
      applyDeployMetadata(deployMeta, true);
    } else {
      setStatusDeployMeta("", "");
    }
    els.statusOut.textContent = asPretty(out);
  }

  function useStatusContractInCall() {
    throw new Error(DTL_CONTRACT_RUNTIME_REMOVED_REASON);
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
  if (els.copyLastContractBtn) {
    els.copyLastContractBtn.addEventListener("click", () =>
      copyValueToClipboard(state.lastContractDeployID, "Last contract ID copied.")
    );
  }
  if (els.copyLastTxBtn) {
    els.copyLastTxBtn.addEventListener("click", () =>
      copyValueToClipboard(state.lastTxID, "Last tx ID copied.")
    );
  }
  if (els.statusCopyContractBtn) {
    els.statusCopyContractBtn.addEventListener("click", () =>
      copyValueToClipboard(
        String((els.statusContractId && els.statusContractId.value) || state.lastContractDeployID || "").trim(),
        "Contract ID copied."
      )
    );
  }
  if (els.statusUseContractBtn) {
    els.statusUseContractBtn.addEventListener("click", () => run(async () => useStatusContractInCall()));
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
  if ($("loadContractDslSampleBtn")) {
    $("loadContractDslSampleBtn").addEventListener("click", () => run(async () => loadContractDslSample()));
  }
  if ($("transpileContractDslBtn")) {
    $("transpileContractDslBtn").addEventListener("click", () => run(async () => transpileContractDslToPayload()));
  }
  if (els.customCodeSampleBtn) {
    els.customCodeSampleBtn.addEventListener("click", () => run(async () => loadCustomCodeSample()));
  }
  if (els.customTplMsc20Btn) {
    els.customTplMsc20Btn.addEventListener("click", () => run(async () => loadMSC20ContractTemplate()));
  }
  if (els.customTplNft721Btn) {
    els.customTplNft721Btn.addEventListener("click", () => run(async () => loadNFTPayloadTemplate("NFT721_CREATE", "NFT721_CREATE")));
  }
  if (els.customTplMsc1155Btn) {
    els.customTplMsc1155Btn.addEventListener("click", () => run(async () => loadNFTPayloadTemplate("NFT1155_CREATE", "MSC1155_CREATE")));
  }
  if (els.presetAmmPoolBtn) {
    els.presetAmmPoolBtn.addEventListener("click", () => run(async () => loadTxPreset("POOL_CREATE", "AMM Pool")));
  }
  if (els.presetLendingBtn) {
    els.presetLendingBtn.addEventListener("click", () => run(async () => loadTxPreset("LEND_MARKET_CREATE", "Lending Market")));
  }
  if (els.presetDuelBtn) {
    els.presetDuelBtn.addEventListener("click", () => run(async () => loadTxPreset("DUEL_CREATE", "Duel Game")));
  }
  if (els.presetTournamentBtn) {
    els.presetTournamentBtn.addEventListener("click", () => run(async () => loadTxPreset("TOURNAMENT_CREATE", "Tournament Game")));
  }
  if (els.customCodeAnalyzeBtn) {
    els.customCodeAnalyzeBtn.addEventListener("click", () => run(async () => analyzeCustomCode()));
  }
  if (els.customCodeUseBtn) {
    els.customCodeUseBtn.addEventListener("click", () => run(async () => applyCustomCodeAsDeployPayload()));
  }
  if (els.customCodeCopyPayloadBtn) {
    els.customCodeCopyPayloadBtn.addEventListener("click", () => run(async () => copyCustomCodePayload()));
  }
  if (els.customCodeOutputMode && els.contractDslOutputMode) {
    els.customCodeOutputMode.addEventListener("change", () => {
      els.contractDslOutputMode.value = normalizeContractOutputMode(els.customCodeOutputMode.value);
      scheduleDraftSave();
    });
    els.contractDslOutputMode.addEventListener("change", () => {
      els.customCodeOutputMode.value = normalizeContractOutputMode(els.contractDslOutputMode.value);
      scheduleDraftSave();
    });
  }
  if ($("simulateLogicPackBtn")) {
    $("simulateLogicPackBtn").addEventListener("click", () => run(async () => simulateLogicPackTrace()));
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
  if (els.logicSimMethod) {
    els.logicSimMethod.addEventListener("change", () => {
      try {
        refreshLogicSimulatorArgsForSelectedMethod();
      } catch (_) {
        // Ignore simulator parse failures while editing payload manually.
      }
    });
  }
  if (els.txPayload) {
    els.txPayload.addEventListener("blur", () => {
      try {
        const payload = parseJSONField(els.txPayload.value, {});
        prefillLogicSimulatorFromPayload(payload);
      } catch (_) {
        // Ignore invalid JSON while user is editing.
      }
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
      prefillLogicSimulatorFromPayload(restoredPayload);
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
  if (els.customCodeEditor && !String(els.customCodeEditor.value || "").trim() && els.contractDslSource) {
    const existingSource = String(els.contractDslSource.value || "").trim();
    if (existingSource) {
      els.customCodeEditor.value = existingSource;
    }
  }
  if (els.customCodeName && !String(els.customCodeName.value || "").trim() && els.contractDslName) {
    const existingName = String(els.contractDslName.value || "").trim();
    if (existingName) {
      els.customCodeName.value = existingName;
    }
  }
  if (els.customCodeLang && els.contractDslLang && String(els.customCodeLang.value || "").trim() === "auto") {
    const existingLang = String(els.contractDslLang.value || "").trim();
    if (existingLang) {
      els.customCodeLang.value = existingLang;
    }
  }
  if (els.customCodeOutputMode && els.contractDslOutputMode) {
    const customMode = normalizeContractOutputMode(els.customCodeOutputMode.value);
    const guidedMode = normalizeContractOutputMode(els.contractDslOutputMode.value);
    if (customMode) {
      els.contractDslOutputMode.value = customMode;
    } else if (guidedMode) {
      els.customCodeOutputMode.value = guidedMode;
    } else {
      els.customCodeOutputMode.value = "dtl-bc-v1";
      els.contractDslOutputMode.value = "dtl-bc-v1";
    }
  }
  if (els.customCodeOut && String(els.customCodeOut.textContent || "").trim() === "") {
    setCustomCodeOutput("No custom code analyzed yet.");
  }
  bindDraftPersistence();
  refreshWorkspaceMeta();
  setStatusDeployMeta(state.lastContractDeployID || "", "");
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
