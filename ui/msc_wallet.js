const enc = new TextEncoder();
const QR_DEBUG = true;

const STORAGE_KEY = "msc_wallet_browser_v1";
const MSC_ONLY_CHAIN_ID_DEC = 91938;
const MSC_ONLY_CHAIN_ID = String(MSC_ONLY_CHAIN_ID_DEC);
const MSC_ONLY_CHAIN_ID_HEX = `0x${MSC_ONLY_CHAIN_ID_DEC.toString(16)}`;
const MSC_COIN_FULL_NAME = "Mythical System Coin";
// 23 months at ~3s/epoch => 19,872,000 epochs.
const DEFAULT_STAKE_EPOCHS = 19872000;
const AUTO_SYNC_MS_KEY = "msc_autosync_ms";
const DEFAULT_AUTO_SYNC_MS = 1000;
const MIN_AUTO_SYNC_MS = 250;
const MAX_AUTO_SYNC_MS = 60000;
const HIDDEN_TAB_SYNC_MS = 5000;
const OFFLINE_SYNC_MS = 10000;
const MAX_BACKOFF_SYNC_MS = 30000;
const VALIDATORS_SYNC_MS = 2000;
const WALLET_STATUS_SYNC_MS = 3000;
const TX_HISTORY_SYNC_MS = 3000;
const QUICK_BALANCE_SYNC_MS = 3000;
const FULL_BALANCE_SYNC_MS = 15000;
const COINS_SYNC_MS = 12000;
const NFT_SYNC_MS = 12000;
const TOKENOMICS_SYNC_MS = 30000;
const DEFAULT_RATE_LIMIT_COOLDOWN_MS = 5000;
const METADATA_CACHE_TTL_MS = 10 * 60 * 1000;
const METADATA_FETCH_TIMEOUT_MS = 5000;
const METADATA_MAX_BYTES = 256 * 1024;
const IPFS_GATEWAY = "https://ipfs.io/ipfs/";
const DEFAULT_BASE_COIN_LOGOS = Object.freeze({
  MSC: "https://ipfs.io/ipfs/bafybeifywdc2qj4zbdbcjyxs27mokq5knwf7uki6wm32yzg5fshbnjvyjy",
});
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
  // Allow pasted formats like "<token>" or "'token'" or "\"token\"".
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

const normalizeRPCBaseURL = (rpc) => {
  let raw = String(rpc || "").trim();
  if (!raw) return "";
  if (!/^[a-zA-Z][a-zA-Z\d+\-.]*:\/\//.test(raw)) {
    raw = `http://${raw}`;
  }
  try {
    const url = new URL(raw);
    const path = url.pathname.replace(/\/+$/, "");
    if (path === "/rpc" || path === "/jsonrpc" || path === "/v1/rpc") {
      url.pathname = "";
      url.search = "";
      url.hash = "";
    } else if (path === "" || path === "/") {
      url.pathname = "";
    } else {
      url.pathname = path;
    }
    return url.toString().replace(/\/+$/, "");
  } catch (err) {
    return raw;
  }
};

const isLoopbackHost = (host) => {
  const h = String(host || "").trim().toLowerCase();
  return h === "localhost" || h === "::1" || /^127(?:\.\d{1,3}){1,3}$/.test(h);
};

const inferDefaultRPCBase = () => {
  try {
    const params = new URLSearchParams(window.location.search);
    const fromQuery = String(params.get("rpc") || "").trim();
    if (fromQuery) return fromQuery;
  } catch (_) {
    // Ignore query parse errors and fall through to origin heuristics.
  }
  const localHost = isLoopbackHost(window.location.hostname);
  if (localHost && String(window.location.port || "").trim() && window.location.port !== "26657") {
    return `${window.location.protocol === "https:" ? "https" : "http"}://127.0.0.1:26657`;
  }
  return window.location.origin;
};

const savedRPCListForCurrentPage = () => {
  const values = String(localStorage.getItem("msc_rpc") || "")
    .split(",")
    .map((item) => normalizeRPCBaseURL(item.trim()))
    .filter(Boolean);
  if (!values.length) return [];
  if (isLoopbackHost(window.location.hostname)) return values;
  return values.filter((rpc) => {
    try {
      const url = new URL(rpc, window.location.href);
      return !isLoopbackHost(url.hostname);
    } catch (_) {
      return false;
    }
  });
};

const initialRPCBase = () => {
  const saved = savedRPCListForCurrentPage();
  return saved.length ? saved[0] : inferDefaultRPCBase();
};

const publicJSONRPCExplicitlyEnabled = () =>
  String(localStorage.getItem("msc_wallet_allow_public_jsonrpc") || "").trim() === "1";

const isProtectedPublicGatewayRPC = (rpc) => {
  if (publicJSONRPCExplicitlyEnabled()) return false;
  try {
    const url = new URL(normalizeRPCBaseURL(rpc || window.location.origin), window.location.href);
    if (isLoopbackHost(url.hostname)) return false;
    return url.origin === window.location.origin;
  } catch (_) {
    return false;
  }
};

const state = {
  rpcUrl: normalizeRPCBaseURL(
    preferHttpsForLocalRpc(initialRPCBase())
  ),
  rpcUrls: [],
  chainId: MSC_ONLY_CHAIN_ID,
  apiToken: normalizeAuthToken(localStorage.getItem("msc_token") || ""),
  broadcastMode: localStorage.getItem("msc_broadcast") || "auto",
  autoSync: (localStorage.getItem("msc_autosync") || "on") === "on",
  autoSyncMs: Math.max(MIN_AUTO_SYNC_MS, parseInt(localStorage.getItem(AUTO_SYNC_MS_KEY) || `${DEFAULT_AUTO_SYNC_MS}`, 10) || DEFAULT_AUTO_SYNC_MS),
  syncTimer: null,
  syncing: false,
  syncErrorStreak: 0,
  lastSyncAt: 0,
  rateLimitedUntil: 0,
  inflight: Object.create(null),
  lastValidatorsSyncAt: 0,
  lastWalletStatusSyncAt: 0,
  lastTxHistorySyncAt: 0,
  lastQuickBalanceSyncAt: 0,
  lastFullBalanceSyncAt: 0,
  lastCoinsSyncAt: 0,
  lastNFTSyncAt: 0,
  lastTokenomicsSyncAt: 0,
  network: null,
  validatorSnapshot: null,
  cooldowns: {},
  wallet: null,
  secretKey: null,
  activity: [],
  tokenomics: null,
  pendingNonces: {},
  sending: false,
  staking: false,
  unstaking: false,
  feePolicy: { ...DEFAULT_FEE_POLICY },
  adminMode: localStorage.getItem("msc_admin_mode") === "1",
  authMode: false,
  authSession: "",
  authNode: "",
  authRpcUrl: "",
  authStakeHint: false,
  authStakeValidator: "",
  authStakeCoin: "MSC",
  authInFlight: false,
  walletStatus: null,
  walletStatusError: "",
  walletEVMAddress: "",
  baseCoinsBySymbol: Object.create(null),
  dtlTokensBySymbol: Object.create(null),
  metadataCache: new Map(),
  nftTab: "721",
  nft721Items: [],
  nft1155Items: [],
  dexPools: [],
  dexLastQuote: null,
  bridgeApprovalQueue: [],
  bridgeApprovalActive: null,
  bridgeApprovalTimer: null,
};

let mscInjectedProvider = null;
let mscProviderLastAccounts = [];
let mscProviderLastChainIdHex = "";
const MSC_PROVIDER_BRIDGE_NAMESPACE = "msc-wallet-bridge-v1";
const MSC_PROVIDER_BRIDGE_EXTRA_ORIGINS_KEY = "msc_bridge_allowed_origins";
const MSC_PROVIDER_BRIDGE_ALLOW_ALL_KEY = "msc_bridge_allow_all";
let mscBridgeClients = [];

const EVM_WEI_PER_MSC = 1000000000000000000n;
const API_REQUEST_TIMEOUT_MS = 15000;

const el = (id) => document.getElementById(id);

const setStatus = (element, message, tone = "info") => {
  element.textContent = message;
  element.dataset.tone = tone;
};

const buildWalletQrPayload = () => {
  if (!state.wallet || !state.wallet.address) return "";
  const chain = state.chainId || MSC_ONLY_CHAIN_ID;
  const pubkey = state.wallet.publicKey || "";
  const evm = String(el("walletEvmAddress")?.textContent || "").trim();
  const parts = [`address=${encodeURIComponent(state.wallet.address)}`];
  if (pubkey) {
    parts.push(`pubkey=${encodeURIComponent(pubkey)}`);
  }
  if (evm && evm !== "—") {
    parts.push(`evm=${encodeURIComponent(evm)}`);
  }
  parts.push(`chain=${encodeURIComponent(chain)}`);
  return `msc://wallet?${parts.join("&")}`;
};

const buildPayQrPayload = () => {
  const chain = state.chainId || MSC_ONLY_CHAIN_ID;
  const to = String(el("sendTo")?.value || "").trim() || (state.wallet ? state.wallet.address : "");
  const amount = parseInt(el("sendAmount")?.value, 10) || 0;
  const coin = normalizeCoinSymbolInput(el("sendCoin")?.value || "") || "MSC";
  if (!to) return "";
  const parts = [
    `address=${encodeURIComponent(to)}`,
    `amount=${encodeURIComponent(String(amount || 0))}`,
    `coin=${encodeURIComponent(coin)}`,
    `chain=${encodeURIComponent(chain)}`,
  ];
  return `msc://pay?${parts.join("&")}`;
};

const parseQrPayload = (payload) => {
  const raw = String(payload || "").trim();
  if (!raw) return null;
  if (!raw.startsWith("msc://")) {
    if (QR_DEBUG) console.log("QR PARSE FAILED: missing msc://", raw);
    return null;
  }
  try {
    const clean = raw.replace(/^msc:\/\//i, "http://");
    const url = new URL(clean);
    const kind = url.hostname;
    const address = String(url.searchParams.get("address") || "").trim();
    const amount = parseInt(url.searchParams.get("amount") || "0", 10) || 0;
    const coin = normalizeCoinSymbolInput(url.searchParams.get("coin") || "MSC") || "MSC";
    const chain = String(url.searchParams.get("chain") || "").trim();
    const pubkey = String(url.searchParams.get("pubkey") || "").trim();
    const evm = String(url.searchParams.get("evm") || "").trim();
    return { kind, address, amount, coin, chain, pubkey, evm, raw };
  } catch (err) {
    if (QR_DEBUG) console.log("QR PARSE FAILED: invalid URL", raw, err);
    return null;
  }
};

const applyQrPayloadToSend = (parsed) => {
  if (QR_DEBUG) console.log("QR PARSED:", parsed);
  if (!parsed || !parsed.address) return;
  const chain = parsed.chain;
  if (chain && String(chain) !== String(state.chainId || MSC_ONLY_CHAIN_ID)) {
    setStatus(statusEls.send, `QR chain mismatch (${chain})`, "error");
    return;
  }
  el("sendTo").value = parsed.address;
  if (parsed.amount > 0) {
    el("sendAmount").value = String(parsed.amount);
  }
  if (parsed.coin) {
    el("sendCoin").value = parsed.coin;
  }
  updateFeeLabels();
  setStatus(statusEls.send, "QR applied", "success");
  const sendForm = el("sendForm");
  if (sendForm) {
    sendForm.scrollIntoView({ behavior: "smooth", block: "center" });
  }
  el("sendAmount").focus();

  const autoSubmit = String(localStorage.getItem("msc_qr_auto_submit") || "1").trim();
  if (parsed.kind === "pay" && parsed.amount > 0 && autoSubmit === "1") {
    setTimeout(() => {
      if (!state.sending && sendForm && typeof sendForm.requestSubmit === "function") {
        sendForm.requestSubmit();
      }
    }, 600);
  }
};

const openQrModal = (title, payload) => {
  const overlay = el("qrOverlay");
  const img = el("qrImage");
  const canvasWrap = el("qrCanvas");
  const payloadEl = el("qrPayload");
  const titleEl = el("qrTitle");
  if (!overlay || !img || !payloadEl || !titleEl) return;
  if (!payload) {
    payloadEl.textContent = "Missing wallet address";
    img.removeAttribute("src");
    if (canvasWrap) {
      canvasWrap.innerHTML = "";
      canvasWrap.style.display = "none";
    }
    img.style.display = "block";
  } else if (window.QRCode && canvasWrap) {
    canvasWrap.innerHTML = "";
    canvasWrap.style.display = "block";
    img.removeAttribute("src");
    img.style.display = "none";
    new QRCode(canvasWrap, {
      text: payload,
      width: 320,
      height: 320,
    });
    const parsed = parseQrPayload(payload);
    if (parsed && parsed.kind === "wallet") {
      payloadEl.textContent =
        `address: ${parsed.address}\n` +
        `chain: ${parsed.chain || "—"}\n` +
        `pubkey: ${parsed.pubkey || "—"}\n` +
        `evm: ${parsed.evm || "—"}`;
    } else {
      payloadEl.textContent = payload;
    }
  } else if (window.MSCQRCode && typeof window.MSCQRCode.toDataURL === "function") {
    if (canvasWrap) {
      canvasWrap.innerHTML = "";
      canvasWrap.style.display = "none";
    }
    img.style.display = "block";
    img.src = window.MSCQRCode.toDataURL(payload, 320);
    const parsed = parseQrPayload(payload);
    if (parsed && parsed.kind === "wallet") {
      payloadEl.textContent =
        `address: ${parsed.address}\n` +
        `chain: ${parsed.chain || "—"}\n` +
        `pubkey: ${parsed.pubkey || "—"}\n` +
        `evm: ${parsed.evm || "—"}`;
    } else {
      payloadEl.textContent = payload;
    }
  } else {
    if (canvasWrap) {
      canvasWrap.innerHTML = "";
      canvasWrap.style.display = "none";
    }
    img.style.display = "block";
    payloadEl.textContent = payload;
    img.removeAttribute("src");
  }
  titleEl.textContent = title;
  overlay.classList.remove("hidden");
  overlay.setAttribute("aria-hidden", "false");
};

const closeQrModal = () => {
  const overlay = el("qrOverlay");
  if (!overlay) return;
  overlay.classList.add("hidden");
  overlay.setAttribute("aria-hidden", "true");
};

let qrScanStream = null;
let qrScanTimer = null;
let qrScanActive = false;
let qrScanLastPayload = "";
let qrScanMode = "auto";
let qrStopOnSyncComplete = false;
let qrPendingPayload = null;

const stopQrScanAfterSync = () => {
  qrStopOnSyncComplete = true;
  if (!state.syncing) {
    syncAll({ silent: true }).catch(() => {});
  }
};

const maybeApplyPendingQr = () => {
  if (!qrPendingPayload) return;
  const sendTo = el("sendTo");
  if (sendTo && String(sendTo.value || "").trim()) {
    qrPendingPayload = null;
    return;
  }
  applyQrPayloadToSend(qrPendingPayload);
  qrPendingPayload = null;
};

const detectQrFromImage = async (imageBitmap) => {
  if (!("BarcodeDetector" in window)) return null;
  const detector = new BarcodeDetector({ formats: ["qr_code"] });
  const barcodes = await detector.detect(imageBitmap);
  if (!barcodes || barcodes.length === 0) return null;
  return barcodes[0].rawValue || "";
};

const scanQrFromFile = async (file) => {
  const statusEl = el("qrScanStatus");
  if (!file || !statusEl) return;
  if (!("createImageBitmap" in window)) {
    statusEl.textContent = "Image scan not supported.";
    return;
  }
  try {
    const bitmap = await createImageBitmap(file);
    const raw = await detectQrFromImage(bitmap);
    if (QR_DEBUG) console.log("QR RAW (image):", raw);
    if (!raw) {
      statusEl.textContent = "QR not found in image.";
      return;
    }
    const parsed = parseQrPayload(raw);
    if (parsed) {
      qrPendingPayload = parsed;
      maybeApplyPendingQr();
      stopQrScanAfterSync();
      statusEl.textContent = "QR applied";
      stopQrScan();
      return;
    }
    statusEl.textContent = "Invalid QR payload";
  } catch (_) {
    statusEl.textContent = "Failed to scan image.";
  }
};

const stopQrScan = () => {
  qrScanActive = false;
  if (qrScanTimer) {
    clearInterval(qrScanTimer);
    qrScanTimer = null;
  }
  if (qrScanStream) {
    qrScanStream.getTracks().forEach((track) => track.stop());
    qrScanStream = null;
  }
  const overlay = el("qrScanOverlay");
  if (overlay) {
    overlay.classList.add("hidden");
    overlay.setAttribute("aria-hidden", "true");
  }
};

const startQrScan = async (mode = "auto") => {
  const overlay = el("qrScanOverlay");
  const statusEl = el("qrScanStatus");
  const video = el("qrScanVideo");
  if (!overlay || !statusEl || !video) return;
  overlay.classList.remove("hidden");
  overlay.setAttribute("aria-hidden", "false");
  statusEl.textContent = "Requesting camera…";
  qrScanMode = mode;

  if (!("BarcodeDetector" in window)) {
    statusEl.textContent = "QR scan not supported on this browser.";
    return;
  }

  try {
    qrScanStream = await navigator.mediaDevices.getUserMedia({
      video: {
        facingMode: "environment",
        width: { ideal: 1280 },
        height: { ideal: 720 },
      },
    });
    video.srcObject = qrScanStream;
    await video.play();
  } catch (err) {
    statusEl.textContent = "Camera permission denied.";
    return;
  }

  const detector = new BarcodeDetector({ formats: ["qr_code"] });
  statusEl.textContent = "Scanning…";
  qrScanActive = true;

  qrScanTimer = setInterval(async () => {
    if (!qrScanStream || !qrScanActive) return;
    try {
      let raw = "";
      const barcodes = await detector.detect(video);
      if (barcodes && barcodes.length > 0) {
        raw = barcodes[0].rawValue || "";
      }
      if (raw) {
        if (QR_DEBUG) console.log("QR RAW (camera):", raw);
        if (!raw || raw === qrScanLastPayload) {
          return;
        }
        if (QR_DEBUG) console.log("HANDLE QR CALLED:", raw);
        const parsed = parseQrPayload(raw);
        if (parsed) {
          qrScanLastPayload = raw;
          qrPendingPayload = parsed;
          maybeApplyPendingQr();
          stopQrScanAfterSync();
          statusEl.textContent = "QR applied";
          if (navigator.vibrate) {
            navigator.vibrate(120);
          }
          if (qrScanMode === "once") {
            stopQrScan();
          }
        } else {
          statusEl.textContent = "Invalid QR payload";
        }
      }
    } catch (_) {
      // ignore scan errors
    }
  }, 200);
};

const copyQrPayload = async () => {
  const payloadEl = el("qrPayload");
  if (!payloadEl) return;
  const payload = String(payloadEl.textContent || "").trim();
  if (!payload || payload === "—") return;
  await navigator.clipboard.writeText(payload);
};

const statusEls = {
  connection: el("connectionStatus"),
  wallet: el("walletState"),
  balance: el("balanceStatus"),
  send: el("sendStatus"),
  faucet: el("faucetStatus"),
  stake: el("stakeStatus"),
  unstake: el("unstakeStatus"),
  tx: el("txStatus"),
  dex: el("dexStatus"),
  poolTransfer: el("poolTransferStatus"),
  validator: el("validatorStatus"),
};

const tokenList = el("tokenList");
const poolList = el("poolList");
const poolFromSelect = el("poolFrom");
const poolToInput = el("poolTo");
const poolAmountInput = el("poolAmount");
const poolCoinInput = el("poolCoin");
const poolNoteInput = el("poolNote");
const tokenomicsSupply = el("tokenomicsSupply");
const tokenomicsTotal = el("tokenomicsTotal");
const tokenomicsChart = el("tokenomicsChart");
const tokenomicsLegend = el("tokenomicsLegend");
const validatorList = el("validatorList");
const activeValidatorList = el("activeValidatorList");
const inactiveValidatorList = el("inactiveValidatorList");
const pendingValidatorList = el("pendingValidatorList");
const stakeValidatorState = el("stakeValidatorState");
const stakeActivationHint = el("stakeActivationHint");
const stakeValidatorPubKeyInput = el("stakeValidatorPubKey");
const stakeValidatorPubKeyState = el("stakeValidatorPubKeyState");
const stakeValidatorPubKeyHint = el("stakeValidatorPubKeyHint");
const walletValidatorSummary = el("walletValidatorSummary");
const walletStakeMeta = el("walletStakeMeta");
const validatorHint = el("validatorHint");
const authCard = el("authCard");
const authStatus = el("authStatus");
const authNodeLabel = el("authNodeLabel");
const authConnect = el("authConnect");
const networkRpc = el("networkRpc");
const networkFinalized = el("networkFinalized");
const networkPeers = el("networkPeers");
const networkChain = el("networkChain");
const networkHealth = el("networkHealth");
const networkBlockProduction = el("networkBlockProduction");
const networkTxLane = el("networkTxLane");
const networkConsensus = el("networkConsensus");
const networkLiveness = el("networkLiveness");
const networkLivenessMode = el("networkLivenessMode");
const networkAutoheal = el("networkAutoheal");
const networkAutohealMismatch = el("networkAutohealMismatch");
const networkBootstrapLane = el("networkBootstrapLane");
const networkSyncMode = el("networkSyncMode");
const networkSyncAnchor = el("networkSyncAnchor");
const networkOnboardingState = el("networkOnboardingState");
const networkActivationModel = el("networkActivationModel");
const networkBarrierRetryMode = el("networkBarrierRetryMode");
const networkActivationWindow = el("networkActivationWindow");
const txList = el("txList");
const refreshTokensBtn = el("refreshTokens");
const refreshNFTsBtn = el("refreshNFTs");
const nftTab721Btn = el("nftTab721");
const nftTab1155Btn = el("nftTab1155");
const nft721List = el("nft721List");
const nft1155List = el("nft1155List");
const refreshPoolsBtn = el("refreshPools");
const refreshTxsBtn = el("refreshTxs");
const refreshDexDataBtn = el("refreshDexData");
const dexRefreshPoolsBtn = el("dexRefreshPools");
const dexQuoteForm = el("dexQuoteForm");
const dexSwapForm = el("dexSwapForm");
const dexCreatePoolForm = el("dexCreatePoolForm");
const dexAddLiquidityForm = el("dexAddLiquidityForm");
const dexRemoveLiquidityForm = el("dexRemoveLiquidityForm");
const dexQuoteResult = el("dexQuoteResult");
const dexPoolList = el("dexPoolList");
const dexUseQuoteBtn = el("dexUseQuoteBtn");
const dexOpenIdeBtn = el("dexOpenIdeBtn");
const dexOpenLendingIdeBtn = el("dexOpenLendingIdeBtn");
const dexOpenFarmIdeBtn = el("dexOpenFarmIdeBtn");

const dexQuoteTokenInInput = el("dexQuoteTokenIn");
const dexQuoteTokenOutInput = el("dexQuoteTokenOut");
const dexQuoteAmountInInput = el("dexQuoteAmountIn");
const dexQuoteMaxHopsInput = el("dexQuoteMaxHops");

const dexSwapTokenInInput = el("dexSwapTokenIn");
const dexSwapAmountInInput = el("dexSwapAmountIn");
const dexSwapMinOutInput = el("dexSwapMinOut");
const dexSwapDeadlineInput = el("dexSwapDeadline");
const dexSwapPathInput = el("dexSwapPath");

const dexCreateTokenAInput = el("dexCreateTokenA");
const dexCreateTokenBInput = el("dexCreateTokenB");
const dexCreateAmountAInput = el("dexCreateAmountA");
const dexCreateAmountBInput = el("dexCreateAmountB");
const dexCreateFeeBpsInput = el("dexCreateFeeBps");

const dexLiqPoolIdInput = el("dexLiqPoolId");
const dexLiqAmountAInput = el("dexLiqAmountA");
const dexLiqAmountBInput = el("dexLiqAmountB");
const dexLiqMinSharesInput = el("dexLiqMinShares");

const dexRemovePoolIdInput = el("dexRemovePoolId");
const dexRemoveLPSharesInput = el("dexRemoveLPShares");
const dexRemoveMinAInput = el("dexRemoveMinA");
const dexRemoveMinBInput = el("dexRemoveMinB");
const autoSyncSelect = el("autoSync");
const autoSyncMsInput = el("autoSyncMs");
const broadcastSelect = el("broadcastMode");
const netControls = el("netControls");
const toggleAdminSettingsBtn = el("toggleAdminSettings");
const bridgeApprovalOverlay = el("bridgeApprovalOverlay");
const bridgeApprovalStatus = el("bridgeApprovalStatus");
const bridgeApprovalTitle = el("bridgeApprovalTitle");
const bridgeApprovalSubtitle = el("bridgeApprovalSubtitle");
const bridgeApprovalNetwork = el("bridgeApprovalNetwork");
const bridgeApprovalOrigin = el("bridgeApprovalOrigin");
const bridgeApprovalAccount = el("bridgeApprovalAccount");
const bridgeApprovalTo = el("bridgeApprovalTo");
const bridgeApprovalAmount = el("bridgeApprovalAmount");
const bridgeApprovalGas = el("bridgeApprovalGas");
const bridgeApprovalFee = el("bridgeApprovalFee");
const bridgeApprovalSpeed = el("bridgeApprovalSpeed");
const bridgeApproveBtn = el("bridgeApproveBtn");
const bridgeRejectBtn = el("bridgeRejectBtn");

const applyAdminMode = (forceEnabled) => {
  const enabled = forceEnabled === true || state.adminMode || !!state.apiToken;
  state.adminMode = !!enabled;
  if (netControls) {
    netControls.classList.toggle("show-admin", state.adminMode);
  }
  if (toggleAdminSettingsBtn) {
    toggleAdminSettingsBtn.textContent = state.adminMode ? "Hide Admin" : "Admin";
  }
  localStorage.setItem("msc_admin_mode", state.adminMode ? "1" : "0");
};

const normalizeAutoSyncMs = (value) => {
  const parsed = parseInt(value, 10);
  if (!Number.isFinite(parsed)) return DEFAULT_AUTO_SYNC_MS;
  if (parsed < MIN_AUTO_SYNC_MS) return MIN_AUTO_SYNC_MS;
  if (parsed > MAX_AUTO_SYNC_MS) return MAX_AUTO_SYNC_MS;
  return parsed;
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

const effectiveAutoSyncMs = () => {
  let interval = normalizeAutoSyncMs(state.autoSyncMs);

  if (document.hidden) {
    interval = Math.max(interval, HIDDEN_TAB_SYNC_MS);
  }
  if (typeof navigator !== "undefined" && navigator.onLine === false) {
    interval = Math.max(interval, OFFLINE_SYNC_MS);
  }

  if (state.syncErrorStreak > 0) {
    const backoff = Math.min(MAX_BACKOFF_SYNC_MS, interval * 2 ** Math.min(5, state.syncErrorStreak));
    interval = Math.max(interval, backoff);
  }

  // Small jitter prevents many clients from polling in lockstep.
  const jitter = Math.floor(interval * 0.1);
  if (jitter > 0) {
    const delta = Math.floor(Math.random() * (jitter * 2 + 1)) - jitter;
    interval = Math.max(MIN_AUTO_SYNC_MS, interval + delta);
  }

  return interval;
};

const shouldRunInterval = (lastAt, intervalMs, force = false) => {
  if (force) return true;
  if (!lastAt) return true;
  return Date.now() - lastAt >= intervalMs;
};

const inRateLimitCooldown = () => Date.now() < Number(state.rateLimitedUntil || 0);

const parseRetryAfterMs = (value) => {
  if (!value) return 0;
  const raw = String(value).trim();
  if (!raw) return 0;
  const seconds = Number(raw);
  if (Number.isFinite(seconds) && seconds > 0) {
    return Math.round(seconds * 1000);
  }
  const asDate = Date.parse(raw);
  if (!Number.isNaN(asDate)) {
    return Math.max(0, asDate - Date.now());
  }
  return 0;
};

const applyRateLimitCooldown = (err) => {
  if (!err || err.status !== 429) return;
  const retryAfterMs = Number(err.retryAfterMs || 0);
  const cooldownMs =
    retryAfterMs > 0
      ? Math.max(DEFAULT_RATE_LIMIT_COOLDOWN_MS, retryAfterMs)
      : DEFAULT_RATE_LIMIT_COOLDOWN_MS;
  state.rateLimitedUntil = Math.max(state.rateLimitedUntil || 0, Date.now() + cooldownMs);
};

const runWithInFlight = (key, fn) => {
  if (state.inflight[key]) {
    return state.inflight[key];
  }
  const task = Promise.resolve()
    .then(fn)
    .finally(() => {
      delete state.inflight[key];
    });
  state.inflight[key] = task;
  return task;
};

const bytesToHex = (bytes) =>
  Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");

const hexToBytes = (hex) => {
  const clean = hex.trim();
  if (!clean) return new Uint8Array();
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

const hasSubtleCrypto = () =>
  typeof crypto !== "undefined" && crypto && crypto.subtle;

const requireCryptoFallback = () => {
  if (
    !window.MSC_CRYPTO_FALLBACK ||
    typeof window.MSC_CRYPTO_FALLBACK.sha256 !== "function" ||
    typeof window.MSC_CRYPTO_FALLBACK.hmacSha512 !== "function" ||
    typeof window.MSC_CRYPTO_FALLBACK.pbkdf2HmacSha512 !== "function"
  ) {
    throw new Error("Browser crypto unavailable. Open over HTTPS or localhost.");
  }
  return window.MSC_CRYPTO_FALLBACK;
};

const sha256 = async (bytes) => {
  if (!hasSubtleCrypto()) {
    return requireCryptoFallback().sha256(bytes);
  }
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return new Uint8Array(hash);
};

const normalizeHexData = (value) => {
  const raw = String(value || "").trim().replace(/^0x/i, "");
  if (!raw) return "0x";
  const padded = raw.length % 2 === 0 ? raw : `0${raw}`;
  return `0x${padded.toLowerCase()}`;
};

const normalizeHexHash = (value) => normalizeHexData(value);

const isHexAddress = (value) => /^0x[0-9a-fA-F]{40}$/.test(String(value || "").trim());

const normalizeHexAddress = (value) => {
  if (!isHexAddress(value)) return "";
  return `0x${String(value).trim().slice(2).toLowerCase()}`;
};

const isLikelyMSCWalletAddress = (raw) => {
  const value = String(raw || "").trim();
  if (!value) return false;
  const hasPrefix = /^MSC/i.test(value);
  const body = hasPrefix ? value.slice(3) : value;
  if (!/^[0-9a-fA-F]+$/.test(body)) return false;
  if (body.length === 42) return true; // v1 21-byte payload (with or without MSC prefix)
  if (body.length === 40) return hasPrefix; // legacy 20-byte payload only accepted with prefix
  return false;
};

const parseRPCQuantityBigInt = (value, fieldName = "value") => {
  const raw = String(value ?? "").trim();
  if (!raw) return 0n;
  if (/^0x[0-9a-fA-F]+$/.test(raw)) return BigInt(raw);
  if (/^[0-9]+$/.test(raw)) return BigInt(raw);
  throw new Error(`invalid ${fieldName}`);
};

const encodeRPCQuantityBigInt = (value) => {
  const bi = typeof value === "bigint" ? value : BigInt(value || 0);
  const safe = bi < 0n ? 0n : bi;
  return `0x${safe.toString(16)}`;
};

const chainIdHex = () => {
  return MSC_ONLY_CHAIN_ID_HEX;
};

const isMSCChainID = (value) => {
  const parsed = Number.parseInt(String(value || "").trim(), 10);
  return Number.isFinite(parsed) && parsed === MSC_ONLY_CHAIN_ID_DEC;
};

const enforceMSCChainID = () => {
  state.chainId = MSC_ONLY_CHAIN_ID;
  const chainInput = el("chainId");
  if (chainInput && chainInput.value !== MSC_ONLY_CHAIN_ID) {
    chainInput.value = MSC_ONLY_CHAIN_ID;
  }
  localStorage.setItem("msc_chain", MSC_ONLY_CHAIN_ID);
};

const weiToWholeMSCAmount = (weiValue) => {
  if (weiValue < 0n) {
    throw new Error("value must be non-negative");
  }
  if (weiValue === 0n) return 0;
  const whole = weiValue / EVM_WEI_PER_MSC;
  const remainder = weiValue % EVM_WEI_PER_MSC;
  if (remainder !== 0n) {
    // Compatibility: some tools occasionally attach tiny wei dust on deploy/call.
    // Native MSC accounting is whole-unit only, so sub-1 MSC gets treated as zero.
    if (whole === 0n) return 0;
    throw new Error("value must be in whole MSC units (18 decimals)");
  }
  const maxSafe = BigInt(Number.MAX_SAFE_INTEGER);
  if (whole > maxSafe) {
    throw new Error("value too large");
  }
  return Number(whole);
};

const evmAliasFromAddressLocal = async (addr) => {
  let value = String(addr || "").trim();
  if (!value) return "";
  if (isHexAddress(value)) return normalizeHexAddress(value);
  if (isLikelyMSCWalletAddress(value) && !/^MSC/i.test(value)) {
    value = `MSC${value}`;
  }
  const hash = await sha256(enc.encode(value.toLowerCase()));
  return `0x${bytesToHex(hash.slice(12))}`;
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
  if (!hasSubtleCrypto()) {
    return requireCryptoFallback().hmacSha512(keyBytes, dataBytes);
  }
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

const parseHDInputValue = (inputId, label, fallback) => {
  const node = el(inputId);
  if (!node) return fallback;
  const raw = String(node.value || "").trim();
  if (!raw) return fallback;
  if (!/^\d+$/.test(raw)) {
    throw new Error(`${label} must be a non-negative integer`);
  }
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isSafeInteger(parsed) || parsed < 0 || parsed > HD_MAX_NON_HARDENED) {
    throw new Error(`${label} must be between 0 and ${HD_MAX_NON_HARDENED}`);
  }
  return parsed;
};

const readHDSelection = (prefix) => ({
  account: parseHDInputValue(`${prefix}Account`, "HD Account", HD_DEFAULT_ACCOUNT),
  change: parseHDInputValue(`${prefix}Change`, "HD Change", HD_DEFAULT_CHANGE),
  index: parseHDInputValue(`${prefix}Index`, "HD Index", HD_DEFAULT_INDEX),
});

const formatNumber = (value) => {
  if (value === undefined || value === null) return "—";
  const num = Number(value);
  if (Number.isNaN(num)) return String(value);
  return num.toLocaleString();
};

const shortAddress = (address) => {
  if (!address || address.length < 12) return address || "—";
  return `${address.slice(0, 6)}...${address.slice(-4)}`;
};

const asIntOrNull = (value) => {
  const num = Number(value);
  if (!Number.isFinite(num)) return null;
  return Math.trunc(num);
};

const asTextOrDash = (value) => {
  if (value === undefined || value === null) return "—";
  const text = String(value).trim();
  return text || "—";
};

const shortHashText = (value, n = 6) => {
  const text = asTextOrDash(value);
  if (text === "—") return text;
  if (text.length <= n * 2) return text;
  return `${text.slice(0, n)}...${text.slice(-n)}`;
};

const buildDTLIDEURL = (prefill = {}) => {
  const url = new URL("dtl_ide.html", window.location.href);
  const rpc = String(state.rpcUrl || "").trim();
  if (rpc) url.searchParams.set("rpc", rpc);
  const setParam = (key, value) => {
    const normalized = String(value || "").trim();
    if (!normalized) return;
    url.searchParams.set(key, normalized);
  };
  setParam("token", prefill.token);
  setParam("pool", prefill.pool);
  setParam("farm", prefill.farm);
  setParam("account", prefill.account || (state.wallet && state.wallet.address ? state.wallet.address : ""));
  setParam("route_out", prefill.routeOut);
  setParam("from", prefill.from || (state.wallet && state.wallet.address ? state.wallet.address : ""));
  setParam("dtl_type", prefill.dtlType);
  return url.toString();
};

const openDTLIDE = (prefill = {}) => {
  const target = buildDTLIDEURL(prefill);
  const popup = window.open(target, "_blank", "noopener");
  if (!popup) {
    window.location.href = target;
  }
};

const initAuthParams = () => {
  const params = new URLSearchParams(window.location.search);
  state.authMode = params.get("auth") === "1" || params.get("auth") === "true";
  state.authSession = String(params.get("session") || "").trim();
  state.authNode = String(params.get("node") || "").trim().toUpperCase();
  state.authRpcUrl = normalizeRPCBaseURL(
    preferHttpsForLocalRpc(params.get("rpc") || window.location.origin)
  );
  state.authStakeHint = params.get("stake") === "1" || params.get("stake") === "true";
  state.authStakeValidator = String(
    params.get("stake_validator") || state.authNode || ""
  ).trim().toUpperCase();
  state.authStakeCoin = normalizeCoinSymbolInput(params.get("stake_coin") || "MSC") || "MSC";
  if (state.authMode && state.authRpcUrl) {
    state.rpcUrl = state.authRpcUrl;
    state.rpcUrls = [state.authRpcUrl];
    const rpcInput = el("rpcUrl");
    if (rpcInput) {
      rpcInput.value = state.authRpcUrl;
    }
  }
  if (authCard) {
    authCard.classList.remove("hidden");
  }
  if (authNodeLabel) {
    authNodeLabel.textContent = state.authNode ? `Node ${state.authNode}` : "Node (auto)";
  }
  if (authStatus) {
    setStatus(authStatus, state.secretKey ? "Ready to sign" : "Unlock wallet first", "info");
  }
  if (state.authStakeHint) {
    applyAuthStakePrefill();
  }
};

const applyAuthStakePrefill = () => {
  if (!state.authStakeHint) return;
  const vid = String(state.authStakeValidator || state.authNode || "").trim().toUpperCase();
  const coin = normalizeCoinSymbolInput(state.authStakeCoin) || "MSC";
  const stakeValidatorInput = el("stakeValidator");
  const unstakeValidatorInput = el("unstakeValidator");
  const stakeCoinInput = el("stakeCoin");
  if (stakeValidatorInput && vid && !stakeValidatorInput.value.trim()) {
    stakeValidatorInput.value = vid;
  }
  if (unstakeValidatorInput && vid && !unstakeValidatorInput.value.trim()) {
    unstakeValidatorInput.value = vid;
  }
  if (stakeCoinInput && !stakeCoinInput.value.trim()) {
    stakeCoinInput.value = coin;
  }
  if (validatorHint) {
    validatorHint.textContent =
      vid !== ""
        ? `Node ${vid} onboarding: Authorize Node, then submit stake to activate validator.`
        : "Node onboarding: Authorize Node, then submit stake to activate validator.";
  }
  if (authStatus && state.secretKey) {
    setStatus(authStatus, "Ready to sign", "info");
  }
  if (typeof updateStakeValidatorStatus === "function") {
    updateStakeValidatorStatus();
  }
};

const startAuthFlow = async () => {
  if (state.authInFlight) return;
  if (!state.wallet || !state.secretKey) {
    setStatus(authStatus, "Unlock wallet first", "error");
    return;
  }
  if (authNodeLabel) {
    authNodeLabel.textContent = state.authNode ? `Node ${state.authNode}` : "Node (auto)";
  }
  state.authInFlight = true;
  try {
    setStatus(authStatus, "Requesting challenge...", "info");
    const requestChallenge = async () => {
      const query = new URLSearchParams();
      if (state.authSession) query.set("session", state.authSession);
      if (state.authNode) query.set("node_id", state.authNode);
      return api(`/auth/challenge?${query.toString()}`, {
        method: "GET",
        baseUrl: state.authRpcUrl || undefined,
      });
    };
    let challenge;
    try {
      challenge = await requestChallenge();
    } catch (err) {
      const message = getErrorText(err);
      if (/session node mismatch/i.test(message)) {
        state.authSession = "";
        challenge = await requestChallenge();
      } else {
        throw err;
      }
    }
    if (!challenge || !challenge.message) {
      throw new Error("Invalid challenge response");
    }
    if (challenge.session_id) {
      state.authSession = String(challenge.session_id).trim();
    }
    if (challenge.node_id) {
      state.authNode = String(challenge.node_id).trim().toUpperCase();
      if (authNodeLabel) {
        authNodeLabel.textContent = `Node ${state.authNode}`;
      }
    }
    const signature = nacl.sign.detached(enc.encode(challenge.message), state.secretKey);
    setStatus(authStatus, "Verifying signature...", "info");
    const verify = await api("/auth/verify", {
      method: "POST",
      baseUrl: state.authRpcUrl || undefined,
      body: {
        session_id: challenge.session_id,
        public_key: state.wallet.publicKey,
        signature: bytesToHex(signature),
      },
    });
    if (verify && verify.token) {
      state.apiToken = normalizeAuthToken(verify.token);
      localStorage.setItem("msc_token", state.apiToken);
      el("apiToken").value = state.apiToken;
      applyAdminMode(true);
    }
    if (verify && verify.validator_eligible) {
      setStatus(authStatus, "Authorized", "success");
    } else {
      const note = verify && verify.note ? ` (${verify.note})` : "";
      setStatus(authStatus, `Authorized, not eligible${note}`, "error");
    }
    if (authNodeLabel && challenge.node_id) {
      authNodeLabel.textContent = `Node ${challenge.node_id} · ${shortAddress(state.wallet.address)}`;
    }
  } catch (err) {
    const message = await formatError(err);
    setStatus(authStatus, message, "error");
  } finally {
    state.authInFlight = false;
  }
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

const LEGACY_AES_GCM_ITERATIONS = 150000;
// HTTP public-IP pages do not get SubtleCrypto, so keep this fallback responsive.
// Production wallet hosting should use HTTPS, which uses LEGACY_AES_GCM_ITERATIONS.
const SECRETBOX_FALLBACK_ITERATIONS = 2048;

const deriveAesGcmKey = async (password, salt, iterations = LEGACY_AES_GCM_ITERATIONS) => {
  if (!hasSubtleCrypto()) {
    throw new Error("WebCrypto unavailable for AES-GCM wallet");
  }
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

const deriveSecretboxKey = async (
  password,
  salt,
  iterations = SECRETBOX_FALLBACK_ITERATIONS,
) => {
  if (hasSubtleCrypto()) {
    const keyMaterial = await crypto.subtle.importKey(
      "raw",
      enc.encode(password),
      "PBKDF2",
      false,
      ["deriveBits"],
    );
    const bits = await crypto.subtle.deriveBits(
      {
        name: "PBKDF2",
        salt,
        iterations,
        hash: "SHA-512",
      },
      keyMaterial,
      256,
    );
    return new Uint8Array(bits);
  }
  return requireCryptoFallback().pbkdf2HmacSha512(
    enc.encode(password),
    salt,
    iterations,
    nacl.secretbox.keyLength,
  );
};

const encryptSecretKey = async (secretKey, password) => {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  if (!hasSubtleCrypto()) {
    const nonce = crypto.getRandomValues(new Uint8Array(nacl.secretbox.nonceLength));
    const key = await deriveSecretboxKey(password, salt);
    const cipher = nacl.secretbox(secretKey, nonce, key);
    return {
      cipher: "nacl-secretbox-xsalsa20poly1305",
      kdf: "pbkdf2-hmac-sha512",
      ciphertext: bytesToHex(cipher),
      nonce: bytesToHex(nonce),
      salt: bytesToHex(salt),
      iterations: SECRETBOX_FALLBACK_ITERATIONS,
    };
  }
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await deriveAesGcmKey(password, salt);
  const cipher = await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, secretKey);
  return {
    cipher: "aes-256-gcm",
    kdf: "pbkdf2-sha256",
    ciphertext: bytesToHex(new Uint8Array(cipher)),
    iv: bytesToHex(iv),
    salt: bytesToHex(salt),
    iterations: LEGACY_AES_GCM_ITERATIONS,
  };
};

const decryptSecretKey = async (cryptoData, password) => {
  const salt = hexToBytes(cryptoData.salt);
  if (cryptoData.cipher === "nacl-secretbox-xsalsa20poly1305") {
    const nonce = hexToBytes(cryptoData.nonce);
    const ciphertext = hexToBytes(cryptoData.ciphertext);
    const key = await deriveSecretboxKey(
      password,
      salt,
      cryptoData.iterations || SECRETBOX_FALLBACK_ITERATIONS,
    );
    const plain = nacl.secretbox.open(ciphertext, nonce, key);
    if (!plain) {
      throw new Error("invalid password");
    }
    return plain;
  }
  const iv = hexToBytes(cryptoData.iv);
  const ciphertext = hexToBytes(cryptoData.ciphertext);
  const key = await deriveAesGcmKey(
    password,
    salt,
    cryptoData.iterations || LEGACY_AES_GCM_ITERATIONS,
  );
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

const normalizeWalletFromSecretKey = async (wallet, secretKey) => {
  if (!wallet || !secretKey || secretKey.length !== 64) return wallet;
  const publicKeyBytes = secretKey.slice(32);
  const publicKey = bytesToHex(publicKeyBytes);
  const address = await addressFromPublicKey(publicKeyBytes, state.chainId);
  if (wallet.publicKey === publicKey && wallet.address === address) {
    return wallet;
  }
  const normalized = {
    ...wallet,
    address,
    publicKey,
  };
  storeWallet(normalized);
  logActivity("Wallet address normalized from private key");
  return normalized;
};

const api = async (path, { method = "GET", body, baseUrl } = {}) => {
  const headers = {};
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
  }
  const token = normalizeAuthToken(state.apiToken);
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const url = `${baseUrl || state.rpcUrl}${path}`;
  let res;
  const controller = typeof AbortController !== "undefined" ? new AbortController() : null;
  const timeoutId = controller
    ? setTimeout(() => {
        try {
          controller.abort("timeout");
        } catch (err) {
          // no-op
        }
      }, API_REQUEST_TIMEOUT_MS)
    : null;
  try {
    res = await fetch(url, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
      signal: controller ? controller.signal : undefined,
    });
  } catch (err) {
    const isTimeout =
      (controller && controller.signal && controller.signal.aborted) ||
      String(err?.name || "").toLowerCase() === "aborterror";
    const netErr = new Error(isTimeout ? "request timeout" : "network error");
    netErr.cause = err;
    netErr.url = url;
    netErr.isNetwork = true;
    throw netErr;
  } finally {
    if (timeoutId) {
      clearTimeout(timeoutId);
    }
  }

  const retryAfterHeader = res.headers ? res.headers.get("Retry-After") : "";
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
    const message = (data && data.error) || (data && data.message) || text || res.statusText;
    const apiErr = new Error(message);
    apiErr.status = res.status;
    apiErr.data = data;
    apiErr.url = url;
    apiErr.retryAfterMs = parseRetryAfterMs(retryAfterHeader);
    throw apiErr;
  }
  return data;
};

const shouldRetry = (err) => {
  if (!err) return false;
  if (err.isNetwork) return true;
  if (err.status === undefined || err.status === null) return true;
  return err.status >= 500 || err.status === 404;
};

const apiWithFallback = async (path, options = {}) => {
  const rpcTargets = state.rpcUrls.length ? state.rpcUrls : [state.rpcUrl];
  const mode = state.broadcastMode || "auto";
  const method = options.method || "GET";

  if (mode === "primary" || rpcTargets.length === 1) {
    return api(path, { ...options, baseUrl: rpcTargets[0] });
  }

  if (mode === "fanout" && method !== "GET") {
    const results = await Promise.allSettled(
      rpcTargets.map((rpc) => api(path, { ...options, baseUrl: rpc })),
    );
    const ok = results.find((res) => res.status === "fulfilled");
    if (ok) return ok.value;
    const lastErr = results.find((res) => res.status === "rejected");
    if (lastErr) throw lastErr.reason;
  }

  let lastErr = null;
  for (const rpc of rpcTargets) {
    try {
      return await api(path, { ...options, baseUrl: rpc });
    } catch (err) {
      lastErr = err;
      if (!shouldRetry(err)) {
        throw err;
      }
    }
  }
  if (lastErr) throw lastErr;
  throw new Error("No RPC endpoints available");
};

const rpcRequest = async (method, params = [], { useFallback = true } = {}) => {
  const rpcTargets = state.rpcUrls.length ? state.rpcUrls : [state.rpcUrl];
  if (rpcTargets.some(isProtectedPublicGatewayRPC)) {
    throw new Error("JSON-RPC is protected on the public gateway. Use DTL IDE for advanced DTL RPC.");
  }
  const payload = {
    jsonrpc: "2.0",
    id: Date.now(),
    method: String(method || "").trim(),
    params: Array.isArray(params) ? params : [],
  };
  if (!payload.method) {
    throw new Error("missing JSON-RPC method");
  }
  const rpcCall = useFallback ? apiWithFallback : api;
  const res = await rpcCall("/rpc", { method: "POST", body: payload });
  if (res && typeof res === "object" && res.error) {
    const rpcMessage = String(res.error.message || "JSON-RPC error");
    throw new Error(rpcMessage);
  }
  return res ? res.result : null;
};

const parseRPCAddressAliasResult = (payload, fallbackInput) => {
  if (payload && typeof payload === "object") {
    if (typeof payload.evm_address === "string" && payload.evm_address.trim()) {
      return normalizeHexAddress(payload.evm_address);
    }
    if (typeof payload.result === "string" && payload.result.trim()) {
      return normalizeHexAddress(payload.result);
    }
  }
  if (typeof payload === "string" && payload.trim()) {
    return normalizeHexAddress(payload);
  }
  if (isHexAddress(fallbackInput)) {
    return normalizeHexAddress(fallbackInput);
  }
  return "";
};

const fetchEVMAddressAlias = async (addressLike) => {
  const input = String(addressLike || "").trim();
  if (!input) return "";
  if (isHexAddress(input)) return normalizeHexAddress(input);

  try {
    const resolved = await rpcRequest("msc_getEvmAddress", [input]);
    const alias = parseRPCAddressAliasResult(resolved, input);
    if (alias) return alias;
  } catch (err) {
    // Fallback to local deterministic derivation if RPC alias registration fails.
  }
  return evmAliasFromAddressLocal(input);
};

const ensureWalletEVMAddress = async () => {
  if (!state.wallet || !state.wallet.address) {
    state.walletEVMAddress = "";
    return "";
  }
  if (state.walletEVMAddress && isHexAddress(state.walletEVMAddress)) {
    return normalizeHexAddress(state.walletEVMAddress);
  }
  const alias = await fetchEVMAddressAlias(state.wallet.address);
  state.walletEVMAddress = alias || "";
  return state.walletEVMAddress;
};

const resolveEVMRecipientAddress = async (value) => {
  const input = String(value || "").trim();
  if (!input) return "";
  if (isHexAddress(input)) return normalizeHexAddress(input);
  if (isLikelyMSCWalletAddress(input)) {
    return fetchEVMAddressAlias(input);
  }
  throw new Error("invalid recipient address");
};

const setMetricText = (node, text) => {
  if (node) node.textContent = text;
};

const resetNetworkDiagnostics = (chainValue = "—") => {
  setMetricText(networkRpc, "—");
  setMetricText(networkFinalized, "—");
  setMetricText(networkPeers, "—");
  setMetricText(networkChain, chainValue);
  setMetricText(networkHealth, "—");
  setMetricText(networkBlockProduction, "—");
  setMetricText(networkTxLane, "—");
  setMetricText(networkConsensus, "—");
  setMetricText(networkLiveness, "—");
  setMetricText(networkLivenessMode, "—");
  setMetricText(networkAutoheal, "—");
  setMetricText(networkAutohealMismatch, "—");
  setMetricText(networkBootstrapLane, "—");
  setMetricText(networkSyncMode, "—");
  setMetricText(networkSyncAnchor, "—");
  setMetricText(networkOnboardingState, "—");
  setMetricText(networkActivationModel, "—");
  setMetricText(networkBarrierRetryMode, "—");
  setMetricText(networkActivationWindow, "—");
  if (validatorHint) {
    validatorHint.textContent = "Activation diagnostics unavailable.";
  }
};

const updateValidatorHintFromStatus = (status) => {
  if (!validatorHint) return;
  const activationModel = asTextOrDash(status.activation_delay_model);
  const switchHeight = asIntOrNull(status.activation_delay_model_switch_height);
  const scheduledHeight = asIntOrNull(status.scheduled_height);
  const effectiveHeight = asIntOrNull(status.effective_height);
  const barrierMode = asTextOrDash(status.barrier_retry_mode);
  const checkpointHeight = asIntOrNull(status.sync_anchor_checkpoint_height);
  const hasCheckpointModel = activationModel !== "—" && /checkpoint/i.test(activationModel);
  const activationFamily = hasCheckpointModel ? "checkpoint-bound" : "height-delay";

  let hint = `Activation: ${activationFamily} | model=${activationModel}`;
  if (switchHeight !== null) {
    hint += ` switch@${switchHeight}`;
  }
  if (scheduledHeight !== null || effectiveHeight !== null) {
    hint += ` | sched=${scheduledHeight === null ? "—" : scheduledHeight} eff=${effectiveHeight === null ? "—" : effectiveHeight}`;
  }
  if (checkpointHeight !== null) {
    hint += ` | checkpoint=${checkpointHeight}`;
  }
  if (barrierMode !== "—") {
    hint += ` | barrier=${barrierMode}`;
  }
  validatorHint.textContent = hint;
};

const renderNetworkDiagnostics = (status, chainValue = MSC_ONLY_CHAIN_ID) => {
  setMetricText(networkRpc, String(status.rpc || "—").replace(/^https?:\/\//, ""));
  setMetricText(networkFinalized, formatNumber(status.finalized_height));
  setMetricText(networkPeers, formatNumber(status.peers));
  setMetricText(networkChain, chainValue || "—");
  const networkHealthState = asTextOrDash(status.network_health);
  const networkBestHeight = asIntOrNull(status.network_best_height);
  const networkLagBlocks = asIntOrNull(status.network_lag_blocks);
  const blockStatus = asTextOrDash(status.block_production_status);
  const blockReason = asTextOrDash(status.block_production_reason);
  const lastBlockAge = asIntOrNull(status.last_block_age_seconds);
  const txLaneStatus = asTextOrDash(status.tx_lane_status);
  const txLaneReason = asTextOrDash(status.tx_lane_reason);
  const mempoolDepth = asIntOrNull(status.mempool_depth);
  setMetricText(
    networkHealth,
    `${networkHealthState} | best=${networkBestHeight === null ? "—" : networkBestHeight} lag=${networkLagBlocks === null ? "—" : networkLagBlocks}`,
  );
  setMetricText(
    networkBlockProduction,
    `${blockStatus}${lastBlockAge === null ? "" : ` | age=${lastBlockAge}s`}${blockReason === "—" ? "" : ` | ${blockReason}`}`,
  );
  setMetricText(
    networkTxLane,
    `${txLaneStatus} | mempool=${mempoolDepth === null ? "—" : mempoolDepth}${txLaneReason === "—" ? "" : ` | ${txLaneReason}`}`,
  );

  const strictLive = asIntOrNull(
    status.validator_live_strict_count !== undefined
      ? status.validator_live_strict_count
      : status.live_validators,
  );
  const heartbeatLive = asIntOrNull(status.validator_live_heartbeat_count);
  const outOfDrift = asIntOrNull(status.validator_live_out_of_drift_count);
  const requiredQuorum = asIntOrNull(status.required_quorum);
  const waitReason = asTextOrDash(status.wait_reason);
  const livenessMode = asTextOrDash(status.validator_liveness_mode);
  const driftLimit = asIntOrNull(status.validator_liveness_max_height_drift_blocks);
  const autohealState = asTextOrDash(status.validator_autoheal_state);
  const autohealReason = asTextOrDash(status.validator_autoheal_last_reason);
  const mismatchHeight = asIntOrNull(status.validator_autoheal_last_mismatch_height);
  const autohealSuccess = asIntOrNull(status.validator_autoheal_last_success_height);
  const laneCandidates = asIntOrNull(status.validator_bootstrap_lane_candidates);
  const laneUsed = asIntOrNull(status.validator_bootstrap_lane_slots_used);
  const syncMode = asTextOrDash(status.sync_mode);
  const syncAction = asTextOrDash(status.sync_action);
  const syncTarget = asIntOrNull(status.sync_target);
  const deltaRemaining = asIntOrNull(status.delta_remaining_blocks);
  const anchorActive = !!status.sync_anchor_active;
  const anchorStage = asTextOrDash(status.sync_anchor_stage);
  const anchorHeight = asIntOrNull(status.sync_anchor_height);
  const anchorCheckpoint = asIntOrNull(status.sync_anchor_checkpoint_height);
  const anchorVotes = asIntOrNull(status.sync_anchor_votes);
  const anchorRequired = asIntOrNull(status.sync_anchor_required);
  const anchorRetry = asIntOrNull(status.sync_anchor_retry_count);
  const anchorProvider = asTextOrDash(status.sync_anchor_provider);
  const anchorError = asTextOrDash(status.sync_anchor_last_error);
  const anchorDeadline = asIntOrNull(status.sync_anchor_deadline_unix);
  const onboardingState = asTextOrDash(status.onboarding_state);
  const activationBlocker = asTextOrDash(status.activation_blocker_reason);
  const activationModel = asTextOrDash(status.activation_delay_model);
  const activationModelSwitch = asIntOrNull(status.activation_delay_model_switch_height);
  const barrierRetryMode = asTextOrDash(status.barrier_retry_mode);
  const scheduledHeight = asIntOrNull(status.scheduled_height);
  const effectiveHeight = asIntOrNull(status.effective_height);

  const liveText = `${strictLive === null ? "—" : strictLive}/${requiredQuorum === null ? "—" : requiredQuorum}`;
  let consensusText = status.ready ? `ready ${liveText}` : `not_ready ${liveText}`;
  if (status.syncing) {
    consensusText = `syncing ${liveText}`;
  } else if (waitReason !== "—") {
    consensusText = `${consensusText} | ${waitReason}`;
  }
  setMetricText(networkConsensus, consensusText);
  setMetricText(
    networkLiveness,
    `${strictLive === null ? "—" : strictLive}/${heartbeatLive === null ? "—" : heartbeatLive}/${outOfDrift === null ? "—" : outOfDrift}`,
  );
  setMetricText(
    networkLivenessMode,
    driftLimit === null ? livenessMode : `${livenessMode} drift<=${driftLimit}`,
  );
  setMetricText(
    networkAutoheal,
    autohealReason === "—" ? autohealState : `${autohealState} | ${autohealReason}`,
  );

  const expectedHash = asTextOrDash(status.validator_autoheal_expected_hash);
  const gotHash = asTextOrDash(status.validator_autoheal_got_hash);
  const mismatchText =
    expectedHash === "—" && gotHash === "—" && mismatchHeight === null
      ? autohealSuccess === null
        ? "—"
        : `last_ok=${autohealSuccess}`
      : `h=${mismatchHeight === null ? "—" : mismatchHeight} ${shortHashText(expectedHash)}/${shortHashText(gotHash)} ok=${autohealSuccess === null ? "—" : autohealSuccess}`;
  setMetricText(networkAutohealMismatch, mismatchText);
  setMetricText(
    networkBootstrapLane,
    laneUsed === null && laneCandidates === null
      ? "—"
      : `used=${laneUsed === null ? "—" : laneUsed} candidates=${laneCandidates === null ? "—" : laneCandidates}`,
  );

  const syncParts = [`${syncMode}`, `${syncAction}`];
  if (waitReason !== "—") {
    syncParts.push(waitReason);
  }
  if (syncTarget !== null) {
    syncParts.push(`target=${syncTarget}`);
  }
  if (deltaRemaining !== null) {
    syncParts.push(`delta=${deltaRemaining}`);
  }
  setMetricText(networkSyncMode, syncParts.join(" | "));

  let anchorText = anchorActive ? anchorStage : "inactive";
  if (anchorActive) {
    anchorText += ` h=${anchorHeight === null ? "—" : anchorHeight}`;
    anchorText += ` ckpt=${anchorCheckpoint === null ? "—" : anchorCheckpoint}`;
    anchorText += ` votes=${anchorVotes === null ? "—" : anchorVotes}/${anchorRequired === null ? "—" : anchorRequired}`;
    anchorText += ` retry=${anchorRetry === null ? "—" : anchorRetry}`;
    if (anchorProvider !== "—") {
      anchorText += ` p=${shortHashText(anchorProvider, 8)}`;
    }
    if (anchorDeadline !== null) {
      anchorText += ` dl=${anchorDeadline}`;
    }
    if (anchorError !== "—") {
      anchorText += ` err=${shortHashText(anchorError, 10)}`;
    }
  }
  setMetricText(networkSyncAnchor, anchorText);

  setMetricText(
    networkOnboardingState,
    activationBlocker === "—" ? onboardingState : `${onboardingState} | ${activationBlocker}`,
  );
  setMetricText(
    networkActivationModel,
    activationModelSwitch === null ? activationModel : `${activationModel} (switch@${activationModelSwitch})`,
  );
  setMetricText(networkBarrierRetryMode, barrierRetryMode);
  setMetricText(
    networkActivationWindow,
    scheduledHeight === null && effectiveHeight === null
      ? "—"
      : `scheduled=${scheduledHeight === null ? "—" : scheduledHeight} effective=${effectiveHeight === null ? "—" : effectiveHeight}`,
  );
  updateValidatorHintFromStatus(status);
};

const fetchNetworkStatus = async () => {
  const rpcTargets = state.rpcUrls.length ? state.rpcUrls : [state.rpcUrl];
  const results = await Promise.allSettled(
    rpcTargets.map(async (rpc) => {
      let data;
      try {
        data = await api("/status", { baseUrl: rpc });
      } catch (statusErr) {
        data = await api("/v1/status", { baseUrl: rpc });
      }
      const finalized = Number(data.finalized_height || data.height || 0);
      return {
        rpc,
        chain_id: data.chain_id,
        height: Number(data.height || 0),
        finalized_height: finalized,
        peers: Number(data.peers || 0),
        node_id: data.node_id,
        live_validators: data.live_validators,
        required_quorum: data.required_quorum,
        ready: data.ready,
        syncing: data.syncing,
        sync_complete: data.sync_complete,
        wait_reason: data.wait_reason,
        sync_mode: data.sync_mode,
        sync_action: data.sync_action,
        sync_target: data.sync_target,
        delta_remaining_blocks: data.delta_remaining_blocks,
        network_health: data.network_health,
        network_health_summary: data.network_health_summary,
        network_best_height: data.network_best_height,
        network_best_height_votes: data.network_best_height_votes,
        network_quorum_height: data.network_quorum_height,
        network_quorum_votes: data.network_quorum_votes,
        network_quorum_required: data.network_quorum_required,
        network_lag_blocks: data.network_lag_blocks,
        block_production_status: data.block_production_status,
        block_production_reason: data.block_production_reason,
        last_block_age_seconds: data.last_block_age_seconds,
        last_commit_height: data.last_commit_height,
        tx_lane_status: data.tx_lane_status,
        tx_lane_reason: data.tx_lane_reason,
        mempool_depth: data.mempool_depth,
        sync_anchor_active: data.sync_anchor_active,
        sync_anchor_stage: data.sync_anchor_stage,
        sync_anchor_height: data.sync_anchor_height,
        sync_anchor_checkpoint_height: data.sync_anchor_checkpoint_height,
        sync_anchor_votes: data.sync_anchor_votes,
        sync_anchor_required: data.sync_anchor_required,
        sync_anchor_retry_count: data.sync_anchor_retry_count,
        sync_anchor_provider: data.sync_anchor_provider,
        sync_anchor_last_error: data.sync_anchor_last_error,
        sync_anchor_deadline_unix: data.sync_anchor_deadline_unix,
        onboarding_state: data.onboarding_state,
        activation_blocker_reason: data.activation_blocker_reason,
        scheduled_height: data.scheduled_height,
        effective_height: data.effective_height,
        activation_delay_model: data.activation_delay_model,
        activation_delay_model_switch_height: data.activation_delay_model_switch_height,
        barrier_retry_mode: data.barrier_retry_mode,
        validator_liveness_mode: data.validator_liveness_mode,
        validator_liveness_max_height_drift_blocks: data.validator_liveness_max_height_drift_blocks,
        validator_live_strict_count: data.validator_live_strict_count,
        validator_live_heartbeat_count: data.validator_live_heartbeat_count,
        validator_live_out_of_drift_count: data.validator_live_out_of_drift_count,
        validator_autoheal_state: data.validator_autoheal_state,
        validator_autoheal_last_reason: data.validator_autoheal_last_reason,
        validator_autoheal_last_mismatch_height: data.validator_autoheal_last_mismatch_height,
        validator_autoheal_expected_hash: data.validator_autoheal_expected_hash,
        validator_autoheal_got_hash: data.validator_autoheal_got_hash,
        validator_autoheal_last_success_height: data.validator_autoheal_last_success_height,
        validator_bootstrap_lane_candidates: data.validator_bootstrap_lane_candidates,
        validator_bootstrap_lane_slots_used: data.validator_bootstrap_lane_slots_used,
        fee_policy: normalizeFeePolicy(data.fee_policy),
      };
    }),
  );

  const ok = results.filter((res) => res.status === "fulfilled").map((res) => res.value);
  if (!ok.length) {
    state.network = null;
    resetNetworkDiagnostics(state.chainId || "—");
    setStatus(statusEls.connection, "Offline", "error");
    setStatus(statusEls.validator, "Offline", "error");
    return null;
  }

  ok.sort((a, b) => b.finalized_height - a.finalized_height);
  const best = ok[0];
  const remoteChainID = String(best.chain_id || "").trim();
  if (remoteChainID && !isMSCChainID(remoteChainID)) {
    state.network = null;
    setMetricText(networkRpc, best.rpc.replace(/^https?:\/\//, ""));
    setMetricText(networkFinalized, formatNumber(best.finalized_height));
    setMetricText(networkPeers, formatNumber(best.peers));
    setMetricText(networkChain, remoteChainID);
    setMetricText(networkHealth, "—");
    setMetricText(networkBlockProduction, "—");
    setMetricText(networkTxLane, "—");
    setMetricText(networkConsensus, "—");
    setMetricText(networkLiveness, "—");
    setMetricText(networkLivenessMode, "—");
    setMetricText(networkAutoheal, "—");
    setMetricText(networkAutohealMismatch, "—");
    setMetricText(networkBootstrapLane, "—");
    setMetricText(networkSyncMode, "—");
    setMetricText(networkSyncAnchor, "—");
    setMetricText(networkOnboardingState, "—");
    setMetricText(networkActivationModel, "—");
    setMetricText(networkBarrierRetryMode, "—");
    setMetricText(networkActivationWindow, "—");
    if (validatorHint) {
      validatorHint.textContent = "Activation diagnostics unavailable on unsupported chain.";
    }
    setStatus(
      statusEls.connection,
      `Wrong chain ${remoteChainID}. Only ${MSC_COIN_FULL_NAME} (${MSC_ONLY_CHAIN_ID}) supported.`,
      "error",
    );
    setStatus(statusEls.validator, "Wrong chain", "error");
    return null;
  }
  enforceMSCChainID();
  state.rpcUrl = best.rpc;
  state.feePolicy = normalizeFeePolicy(best.fee_policy);
  state.network = {
    best,
    all: ok,
    finalizedHeight: best.finalized_height,
  };
  updateFeeLabels();
  renderNetworkDiagnostics(best, MSC_ONLY_CHAIN_ID);

  setStatus(statusEls.connection, "Connected", "success");
  const waitReason = asTextOrDash(best.wait_reason);
  const hasMismatchWait =
    waitReason !== "—" &&
    /validator_set_.*mismatch|validator-set-.*mismatch|validator[_-]set[_-]hash[_-]mismatch/i.test(waitReason);
  const anchorError = asTextOrDash(best.sync_anchor_last_error);
  const hasAnchorWarning =
    !!best.sync_anchor_active && !!best.syncing && !best.sync_complete && anchorError !== "—";

  if (hasMismatchWait) {
    setStatus(statusEls.validator, `Mismatch (${waitReason})`, "error");
  } else if (best.sync_complete && best.ready) {
    setStatus(statusEls.validator, "Ready", "success");
    autoFillReceiveAddress();
    maybeApplyPendingQr();
  } else if (hasAnchorWarning) {
    setStatus(statusEls.validator, `Sync anchor warning (${anchorError})`, "warning");
  } else if (best.syncing) {
    setStatus(statusEls.validator, "Syncing", "info");
  } else if (best.ready) {
    setStatus(statusEls.validator, "Ready", "success");
  } else {
    const strictLive = asIntOrNull(
      best.validator_live_strict_count !== undefined
        ? best.validator_live_strict_count
        : best.live_validators,
    );
    const requiredQuorum = asIntOrNull(best.required_quorum);
    const quorumText =
      strictLive !== null || requiredQuorum !== null
        ? `${strictLive === null ? "—" : strictLive}/${requiredQuorum === null ? "—" : requiredQuorum}`
        : "";
    if (waitReason !== "—") {
      setStatus(
        statusEls.validator,
        quorumText ? `Waiting (${waitReason}, ${quorumText})` : `Waiting (${waitReason})`,
        "info",
      );
    } else if (quorumText) {
      setStatus(statusEls.validator, `Waiting quorum ${quorumText}`, "info");
    } else {
      setStatus(statusEls.validator, "Not ready", "info");
    }
  }
  return state.network;
};

const syncAll = async ({ silent = false } = {}) => {
  if (state.syncing) return;
  state.syncing = true;
  state.lastSyncAt = Date.now();
  if (!silent) {
    setStatus(statusEls.connection, "Syncing", "info");
  }
  try {
    const network = await fetchNetworkStatus();
    const now = Date.now();
    const tasks = [
      loadValidators({ force: false }),
      loadTxHistory({ force: false }),
    ];
    if (shouldRunInterval(state.lastCoinsSyncAt, COINS_SYNC_MS, false) && !inRateLimitCooldown()) {
      tasks.push(loadCoins({ force: false }));
    }
    if (
      shouldRunInterval(state.lastTokenomicsSyncAt, TOKENOMICS_SYNC_MS, false) &&
      !inRateLimitCooldown()
    ) {
      tasks.push(loadTokenomics({ force: false }));
    }
    if (state.wallet) {
      tasks.push(loadWalletStatus({ force: false }));
      tasks.push(refreshBalance({ quick: true, force: false }));
      if (
        shouldRunInterval(state.lastFullBalanceSyncAt, FULL_BALANCE_SYNC_MS, false) &&
        !inRateLimitCooldown()
      ) {
        tasks.push(refreshBalance({ quick: false, force: false }));
      }
      if (shouldRunInterval(state.lastNFTSyncAt, NFT_SYNC_MS, false) && !inRateLimitCooldown()) {
        tasks.push(loadNFTPortfolio({ force: false }));
      }
    }
    const results = await Promise.allSettled(tasks);
    const failed = results.filter((item) => item.status === "rejected").length;
    const throttled = results.some(
      (item) => item.status === "rejected" && item.reason && item.reason.status === 429,
    );

    if (!network || failed > 0 || throttled || inRateLimitCooldown()) {
      state.syncErrorStreak = Math.min(8, state.syncErrorStreak + 1);
    } else {
      state.syncErrorStreak = 0;
    }
    if (inRateLimitCooldown()) {
      state.lastSyncAt = now;
    }
  } finally {
    state.syncing = false;
    if (qrStopOnSyncComplete) {
      qrStopOnSyncComplete = false;
      stopQrScan();
    }
  }
};

const scheduleAutoSync = () => {
  if (state.syncTimer) {
    clearTimeout(state.syncTimer);
    state.syncTimer = null;
  }
  if (!state.autoSync) return;
  const run = async () => {
    if (!state.autoSync) {
      state.syncTimer = null;
      return;
    }
    await syncAll({ silent: true });
    state.syncTimer = setTimeout(run, effectiveAutoSyncMs());
  };
  state.syncTimer = setTimeout(run, effectiveAutoSyncMs());
};

const triggerImmediateSync = async () => {
  if (!state.autoSync) return;
  if (state.syncTimer) {
    clearTimeout(state.syncTimer);
    state.syncTimer = null;
  }
  await syncAll({ silent: true });
  scheduleAutoSync();
};

const connectToRPC = async ({ persist = false } = {}) => {
  const rawRpc = el("rpcUrl").value.trim();
  const list = rawRpc
    .split(",")
    .map((item) => normalizeRPCBaseURL(preferHttpsForLocalRpc(item.trim())))
    .filter((item) => item);
  state.rpcUrls = list.length
    ? list
    : [normalizeRPCBaseURL(preferHttpsForLocalRpc(state.rpcUrl || window.location.origin))];
  state.rpcUrl = state.rpcUrls[0];
  el("rpcUrl").value = state.rpcUrls.join(", ");
  state.chainId = MSC_ONLY_CHAIN_ID;
  const chainInput = el("chainId");
  if (chainInput) {
    chainInput.value = MSC_ONLY_CHAIN_ID;
  }
  state.apiToken = normalizeAuthToken(el("apiToken").value);
  el("apiToken").value = state.apiToken;
  if (state.apiToken) {
    applyAdminMode(true);
  }
  state.broadcastMode = broadcastSelect?.value || state.broadcastMode || "auto";
  state.autoSync = (autoSyncSelect?.value || (state.autoSync ? "on" : "off")) === "on";
  state.autoSyncMs = normalizeAutoSyncMs(autoSyncMsInput?.value || state.autoSyncMs);
  if (autoSyncMsInput) {
    autoSyncMsInput.value = String(state.autoSyncMs);
  }

  if (persist) {
    localStorage.setItem("msc_rpc", state.rpcUrls.join(", "));
    localStorage.setItem("msc_chain", MSC_ONLY_CHAIN_ID);
    localStorage.setItem("msc_token", state.apiToken);
    localStorage.setItem("msc_broadcast", state.broadcastMode);
    localStorage.setItem("msc_autosync", state.autoSync ? "on" : "off");
    localStorage.setItem(AUTO_SYNC_MS_KEY, String(state.autoSyncMs));
  }

  setStatus(statusEls.connection, "Connecting", "info");
  await syncAll();
  scheduleAutoSync();
  await syncInjectedProviderState({ emitAccounts: false, emitChain: true });
};

const logActivity = (message) => {
  const log = el("activityLog");
  const item = document.createElement("div");
  item.className = "log-item";
  item.textContent = `${new Date().toLocaleTimeString()} — ${message}`;
  log.prepend(item);
  if (log.children.length > 8) {
    log.removeChild(log.lastChild);
  }
};

const clearBridgeApprovalTimer = () => {
  if (state.bridgeApprovalTimer) {
    clearTimeout(state.bridgeApprovalTimer);
    state.bridgeApprovalTimer = null;
  }
};

const formatBridgeRequestOrigin = (value) => {
  const raw = String(value || "").trim();
  if (!raw) return "unknown dapp";
  try {
    const parsed = new URL(raw);
    return parsed.hostname || raw;
  } catch (err) {
    return raw.replace(/^https?:\/\//i, "");
  }
};

const describeBridgeTx = (details) => {
  if (!details) {
    return {
      title: "Approve transaction",
      subtitle: "This site wants to submit a transaction.",
    };
  }
  if (details.kind === "deploy") {
    return {
      title: "Deploy a contract",
      subtitle: "This site wants you to deploy a contract.",
    };
  }
  if (details.kind === "call") {
    return {
      title: "Contract interaction",
      subtitle: "This site wants to execute a contract function.",
    };
  }
  if (details.kind === "connect") {
    return {
      title: "Connect wallet",
      subtitle: "This site wants access to your wallet account.",
    };
  }
  if (details.kind === "stake") {
    return {
      title: "Stake transaction",
      subtitle: "Confirm staking transaction from your wallet.",
    };
  }
  if (details.kind === "unstake") {
    return {
      title: "Unstake transaction",
      subtitle: "Confirm unstake transaction from your wallet.",
    };
  }
  return {
    title: "Send transaction",
    subtitle: "This site wants to send a transaction.",
  };
};

const hideBridgeApprovalOverlay = () => {
  if (!bridgeApprovalOverlay) return;
  bridgeApprovalOverlay.classList.add("hidden");
  bridgeApprovalOverlay.setAttribute("aria-hidden", "true");
};

const showBridgeApprovalOverlay = (details) => {
  if (!bridgeApprovalOverlay) return;
  const txUi = describeBridgeTx(details);
  if (bridgeApprovalTitle) bridgeApprovalTitle.textContent = txUi.title;
  if (bridgeApprovalSubtitle) bridgeApprovalSubtitle.textContent = txUi.subtitle;
  if (bridgeApprovalNetwork) {
    bridgeApprovalNetwork.textContent = `${MSC_COIN_FULL_NAME} (${MSC_ONLY_CHAIN_ID_DEC})`;
  }
  if (bridgeApprovalOrigin) {
    bridgeApprovalOrigin.textContent = formatBridgeRequestOrigin(details.origin);
  }
  if (bridgeApprovalAccount) {
    bridgeApprovalAccount.textContent = state.walletEVMAddress || state.wallet?.address || "—";
  }
  if (details.kind === "connect") {
    if (bridgeApprovalTo) bridgeApprovalTo.textContent = "Account access";
    if (bridgeApprovalAmount) bridgeApprovalAmount.textContent = "0 MSC";
    if (bridgeApprovalGas) bridgeApprovalGas.textContent = "0";
    if (bridgeApprovalFee) bridgeApprovalFee.textContent = "0 MSC";
    if (bridgeApprovalSpeed) bridgeApprovalSpeed.textContent = "Instant";
  } else {
    const amountText = String(details.amountLabel || `${details.amount} MSC`);
    const feeText = String(details.feeLabel || `${details.fee} MSC`);
    if (bridgeApprovalTo) bridgeApprovalTo.textContent = details.to || "(contract deployment)";
    if (bridgeApprovalAmount) bridgeApprovalAmount.textContent = amountText;
    if (bridgeApprovalGas) bridgeApprovalGas.textContent = `${details.gasLimit}`;
    if (bridgeApprovalFee) bridgeApprovalFee.textContent = feeText;
    if (bridgeApprovalSpeed) bridgeApprovalSpeed.textContent = "Market ~1 sec";
  }
  if (bridgeApprovalStatus) setStatus(bridgeApprovalStatus, "Waiting", "info");
  bridgeApprovalOverlay.classList.remove("hidden");
  bridgeApprovalOverlay.setAttribute("aria-hidden", "false");
  try {
    window.focus();
  } catch (err) {
    // ignore focus restrictions
  }
};

const settleBridgeApproval = (approved) => {
  const active = state.bridgeApprovalActive;
  clearBridgeApprovalTimer();
  hideBridgeApprovalOverlay();
  state.bridgeApprovalActive = null;
  if (!active) return;
  if (approved) {
    active.resolve();
  } else {
    const reason = active.details && active.details.kind === "connect"
      ? "user rejected wallet connection"
      : "user rejected transaction";
    active.reject(new Error(reason));
  }
  if (state.bridgeApprovalQueue.length > 0) {
    setTimeout(() => {
      pumpBridgeApprovalQueue();
    }, 0);
  }
};

const pumpBridgeApprovalQueue = () => {
  if (state.bridgeApprovalActive) return;
  if (!state.bridgeApprovalQueue.length) return;
  const next = state.bridgeApprovalQueue.shift();
  if (!next) return;
  state.bridgeApprovalActive = next;
  showBridgeApprovalOverlay(next.details);
  if (bridgeApprovalStatus) setStatus(bridgeApprovalStatus, "Approve or reject", "info");
  logActivity(
    `Approval requested: ${describeBridgeTx(next.details).title} from=${formatBridgeRequestOrigin(next.details.origin)} fee=${next.details.fee} MSC`
  );
  state.bridgeApprovalTimer = setTimeout(() => {
    if (bridgeApprovalStatus) setStatus(bridgeApprovalStatus, "Timed out", "error");
    settleBridgeApproval(false);
  }, 170000);
};

const enqueueBridgeApproval = (details) =>
  new Promise((resolve, reject) => {
    state.bridgeApprovalQueue.push({ details, resolve, reject });
    pumpBridgeApprovalQueue();
  });

const getErrorText = (err) => {
  if (!err) return "";
  if (typeof err === "string") return err;
  if (err.message) return err.message;
  if (typeof err.data === "string") return err.data;
  if (err.data && typeof err.data === "object") {
    if (err.data.error) return err.data.error;
    if (err.data.message) return err.data.message;
  }
  return "";
};

const extractExpectedNonce = (err) => {
  const message = getErrorText(err);
  const nonceMatch = message.match(/expected\s+(\d+)/i);
  if (/nonce/i.test(message) && nonceMatch) {
    return Number(nonceMatch[1]);
  }
  return null;
};

const syncNonceFromError = (err, address) => {
  const expected = extractExpectedNonce(err);
  if (expected && address) {
    state.pendingNonces[address] = expected;
    return expected;
  }
  const message = getErrorText(err);
  if (/nonce/i.test(message)) {
    clearPendingNonce(address);
  }
  return null;
};

const formatError = async (err) => {
  if (!err) return "Unknown error";
  const message = getErrorText(err) || "Request failed";
  if (/rate limit|too many/i.test(message) || err.status === 429) {
    const waitMs = Number(err.retryAfterMs || 0);
    if (waitMs > 0) {
      return `Rate limit hit — wait ${Math.max(1, Math.ceil(waitMs / 1000))}s`;
    }
    return "Rate limit hit — wait a few seconds";
  }
  if (err.status === 401 || /unauthorized/i.test(message)) {
    return "Unauthorized — check API token";
  }
  if (err.status === 403 || /forbidden/i.test(message)) {
    if (/treasury|tokenomics|allow/i.test(message)) {
      return message;
    }
    return "Unauthorized — check API token";
  }
  if (/dtl:\s*unknown token/i.test(message)) {
    return "Unknown token_id. DEX me DTL token_id use karo (symbol ya MSC base coin nahi).";
  }
  if (/dtl:\s*unknown pool/i.test(message)) {
    return "Unknown pool_id. DTL Pools list se exact pool_id paste karo.";
  }
  if (/dtl:\s*pool tokens with transfer tax are not supported/i.test(message)) {
    return "Ye token tax-enabled hai, isliye pool/swap me supported nahi. Tax=0 token use karo.";
  }
  const expected = extractExpectedNonce(err);
  if (expected) {
    return `Invalid nonce — expected ${expected}. Refresh & retry.`;
  }
  if (/network error/i.test(message) || err.isNetwork) {
    return "Network error — switching RPC";
  }
  if (/session node mismatch/i.test(message)) {
    return "Stale auth session detected — retry Authorize Node";
  }
  return message;
};

const cooldownRemaining = (key) => {
  const until = state.cooldowns[key] || 0;
  return Math.max(0, until - Date.now());
};

const setCooldown = (key, ms) => {
  state.cooldowns[key] = Date.now() + ms;
};

const clearPendingNonce = (address) => {
  if (!address) return;
  delete state.pendingNonces[address];
};

const bumpPendingNonce = (address, nextNonce) => {
  if (!address) return;
  state.pendingNonces[address] = nextNonce;
};

const normalizeCoinSymbolInput = (raw) =>
  String(raw || "")
    .trim()
    .replace(/\s*\(DTL\)\s*$/i, "")
    .trim();

const normalizeCoinSymbolKey = (raw) =>
  normalizeCoinSymbolInput(raw).toUpperCase();

const readDTLTokenFromInfo = (info, fallbackSymbol) => {
  if (!info || typeof info !== "object") return null;
  const tokenID = String(info.token_id || info.tokenID || "").trim();
  if (!tokenID) return null;
  const symbol = normalizeCoinSymbolKey(info.symbol || fallbackSymbol);
  if (!symbol) return null;
  return {
    symbol,
    token_id: tokenID,
    name: String(info.name || symbol),
    decimals: Number(info.decimals || 0),
    kind: "dtl",
  };
};

const resolveDTLTokenBySymbol = async (symbolRaw) => {
  const symbol = normalizeCoinSymbolKey(symbolRaw);
  if (!symbol) return null;
  if (state.baseCoinsBySymbol[symbol]) return null;

  const cached = state.dtlTokensBySymbol[symbol];
  if (cached && cached.token_id) return cached;

  try {
    const info = await rpcRequest("dtl_tokenInfo", [symbol]);
    const parsed = readDTLTokenFromInfo(info, symbol);
    if (!parsed) return null;
    state.dtlTokensBySymbol[parsed.symbol] = parsed;
    return parsed;
  } catch (_) {
    return null;
  }
};

const clearElementChildren = (node) => {
  if (!node) return;
  while (node.firstChild) {
    node.removeChild(node.firstChild);
  }
};

const appendInfoRow = (container, message) => {
  if (!container) return;
  const row = document.createElement("div");
  row.className = "token-row";
  const span = document.createElement("span");
  span.textContent = message;
  row.appendChild(span);
  container.appendChild(row);
};

const makeTokenFallbackBadge = (symbol) => {
  const fallback = document.createElement("div");
  fallback.className = "token-logo-fallback";
  const label = String(symbol || "?")
    .replace(/[^a-zA-Z0-9]/g, "")
    .slice(0, 2)
    .toUpperCase() || "?";
  fallback.textContent = label;
  return fallback;
};

const normalizeIPFSURI = (raw) => {
  const value = String(raw || "").trim();
  if (!/^ipfs:\/\//i.test(value)) return "";
  let suffix = value.replace(/^ipfs:\/\//i, "").trim();
  suffix = suffix.replace(/^ipfs\//i, "").replace(/^\/+/, "");
  if (!suffix) return "";
  return `${IPFS_GATEWAY}${suffix}`;
};

const normalizeAssetURI = (raw, { allowDataImage = false } = {}) => {
  const value = String(raw || "").trim();
  if (!value) return "";

  if (/^ipfs:\/\//i.test(value)) {
    return normalizeIPFSURI(value);
  }

  if (/^data:/i.test(value)) {
    if (!allowDataImage) return "";
    if (/^data:image\/[a-zA-Z0-9.+-]+(;base64)?,/i.test(value)) {
      return value;
    }
    return "";
  }

  let parsed;
  try {
    parsed = new URL(value);
  } catch (_) {
    return "";
  }

  const protocol = String(parsed.protocol || "").toLowerCase();
  if (protocol === "https:") {
    return parsed.toString();
  }
  if (protocol === "http:" && isLoopbackHost(parsed.hostname)) {
    return parsed.toString();
  }
  return "";
};

const readResponseTextCapped = async (response, maxBytes) => {
  if (!response) return "";
  if (response.body && typeof response.body.getReader === "function") {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let total = 0;
    let out = "";
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      const value = chunk.value || new Uint8Array();
      total += value.length;
      if (total > maxBytes) {
        throw new Error("metadata too large");
      }
      out += decoder.decode(value, { stream: true });
    }
    out += decoder.decode();
    return out;
  }

  const bodyText = await response.text();
  if (enc.encode(bodyText).length > maxBytes) {
    throw new Error("metadata too large");
  }
  return bodyText;
};

const getCachedMetadata = (uri) => {
  if (!uri) return undefined;
  const entry = state.metadataCache.get(uri);
  if (!entry) return undefined;
  if (entry.expiresAt <= Date.now()) {
    state.metadataCache.delete(uri);
    return undefined;
  }
  return entry.data;
};

const setCachedMetadata = (uri, data, ttlMs = METADATA_CACHE_TTL_MS) => {
  if (!uri) return;
  state.metadataCache.set(uri, {
    expiresAt: Date.now() + ttlMs,
    data,
  });
};

const fetchMetadataJSON = async (uriRaw) => {
  const uri = normalizeAssetURI(uriRaw);
  if (!uri) return null;

  const cached = getCachedMetadata(uri);
  if (cached !== undefined) {
    return cached;
  }

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), METADATA_FETCH_TIMEOUT_MS);
  try {
    const response = await fetch(uri, {
      method: "GET",
      signal: controller.signal,
      headers: {
        Accept: "application/json,text/plain,*/*",
      },
      credentials: "omit",
      cache: "no-store",
    });
    if (!response.ok) {
      throw new Error(`metadata fetch failed: ${response.status}`);
    }

    const contentLength = Number(response.headers.get("content-length") || "0");
    if (Number.isFinite(contentLength) && contentLength > METADATA_MAX_BYTES) {
      throw new Error("metadata too large");
    }

    const bodyText = await readResponseTextCapped(response, METADATA_MAX_BYTES);
    const parsed = JSON.parse(bodyText);
    const data = parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : null;
    setCachedMetadata(uri, data);
    return data;
  } catch (_) {
    setCachedMetadata(uri, null, 60 * 1000);
    return null;
  } finally {
    clearTimeout(timer);
  }
};

const pickMetadataImage = (obj, keys) => {
  if (!obj || typeof obj !== "object") return "";
  for (const key of keys) {
    const value = obj[key];
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return "";
};

const looksLikeImageURI = (raw) => {
  const value = String(raw || "").trim();
  if (!value) return false;
  if (/^data:image\//i.test(value)) return true;
  return /\.(png|jpe?g|gif|webp|svg)(\?.*)?$/i.test(value);
};

const resolveTokenLogoURL = async (token) => {
  if (!token || token.kind !== "dtl") return "";
  const metadataURI = String(token.metadata_uri || "").trim();
  if (!metadataURI) return "";
  const metadata = await fetchMetadataJSON(metadataURI);
  const imageRaw = pickMetadataImage(metadata, ["logo", "logo_uri", "image", "image_url"]);
  return normalizeAssetURI(imageRaw, { allowDataImage: true });
};

const tokenIDToHex64 = (tokenIDRaw) => {
  const value = String(tokenIDRaw || "").trim();
  if (!value) return "";
  try {
    const id = BigInt(value);
    if (id < 0n) return "";
    return id.toString(16).padStart(64, "0");
  } catch (_) {
    return "";
  }
};

const joinTokenURI = (baseURI, tokenID) => {
  const base = String(baseURI || "").trim();
  const id = String(tokenID || "").trim();
  if (!base || !id) return "";
  if (base.includes("{id}")) {
    return base.replace(/\{id\}/gi, id);
  }
  return base.endsWith("/") ? `${base}${id}` : `${base}/${id}`;
};

const buildNFTMetadataURI = (item, kind) => {
  if (!item || typeof item !== "object") return "";
  const tokenID = String(item.token_id || "").trim();
  const tokenURI = String(item.token_uri || "").trim();
  const baseURI = String(item.base_uri || "").trim();

  if (kind === "721") {
    if (tokenURI) return tokenURI;
    return joinTokenURI(baseURI, tokenID);
  }

  if (kind === "1155") {
    if (!baseURI || !tokenID) return "";
    if (baseURI.includes("{id}")) {
      const hexID = tokenIDToHex64(tokenID);
      if (!hexID) return "";
      return baseURI.replace(/\{id\}/gi, hexID);
    }
    return joinTokenURI(baseURI, tokenID);
  }

  return "";
};

const resolveNFTImageURL = async (item, kind) => {
  const metadataURI = buildNFTMetadataURI(item, kind);
  if (!metadataURI) return "";

  const metadata = await fetchMetadataJSON(metadataURI);
  const imageFromMetadata = pickMetadataImage(metadata, ["image", "image_url"]);
  const normalizedFromMetadata = normalizeAssetURI(imageFromMetadata, { allowDataImage: true });
  if (normalizedFromMetadata) {
    return normalizedFromMetadata;
  }

  if (looksLikeImageURI(metadataURI)) {
    return normalizeAssetURI(metadataURI, { allowDataImage: true });
  }

  return "";
};

const makeTokenLogoNode = (symbol, logoURL) => {
  const fallback = makeTokenFallbackBadge(symbol);
  if (!logoURL) {
    return fallback;
  }

  const img = document.createElement("img");
  img.className = "token-logo";
  img.alt = `${String(symbol || "TOKEN").trim()} logo`;
  img.decoding = "async";
  img.loading = "lazy";
  img.src = logoURL;
  img.onerror = () => {
    img.replaceWith(fallback);
  };
  return img;
};

const makeTokenActionButton = (label, onClick) => {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ghost small token-action-btn";
  btn.textContent = label;
  btn.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    onClick();
  });
  return btn;
};

const resolveBaseCoinLogoURL = (symbol) => {
  const key = String(symbol || "").trim().toUpperCase();
  if (!key) return "";
  const raw = DEFAULT_BASE_COIN_LOGOS[key];
  if (!raw) return "";
  return normalizeAssetURI(raw, { allowDataImage: true });
};

const renderTokenList = async (coins, balances) => {
  clearElementChildren(tokenList);
  if (!coins || coins.length === 0) {
    appendInfoRow(tokenList, "No tokens");
    return;
  }

  const logoBySymbol = Object.create(null);
  for (const coin of coins) {
    if (!coin || coin.kind !== "base") continue;
    const symbol = String(coin.symbol || "").trim();
    if (!symbol) continue;
    logoBySymbol[symbol] = resolveBaseCoinLogoURL(symbol);
  }
  await Promise.allSettled(
    coins.map(async (coin) => {
      if (!coin || coin.kind !== "dtl" || !coin.metadata_uri) return;
      const key = String(coin.symbol || "").trim();
      if (!key) return;
      logoBySymbol[key] = await resolveTokenLogoURL(coin);
    }),
  );

  for (const coin of coins) {
    const row = document.createElement("div");
    row.className = "token-row";

    const symbol = String(coin && coin.symbol ? coin.symbol : "").trim() || "TOKEN";
    const balance =
      balances && balances[symbol] !== undefined
        ? balances[symbol]
        : "—";

    const left = document.createElement("div");
    left.className = "token-cell";
    left.appendChild(makeTokenLogoNode(symbol, logoBySymbol[symbol] || ""));

    const details = document.createElement("div");
    details.className = "token-details";

    const symbolNode = document.createElement("strong");
    symbolNode.className = "token-symbol";
    symbolNode.textContent = symbol;
    details.appendChild(symbolNode);

    if (coin.kind === "dtl" && coin.token_id) {
      const tokenMeta = document.createElement("div");
      tokenMeta.className = "token-submeta mono";
      tokenMeta.textContent = `token_id: ${String(coin.token_id)}`;
      details.appendChild(tokenMeta);
      row.title = `DTL token: ${String(coin.token_id)}`;
    }
    left.appendChild(details);

    const right = document.createElement("div");
    right.className = "token-side";

    const balanceNode = document.createElement("span");
    balanceNode.className = "mono token-balance";
    balanceNode.textContent = String(balance);
    right.appendChild(balanceNode);

    const actions = document.createElement("div");
    actions.className = "token-actions";
    if (coin.kind === "dtl" && coin.token_id) {
      const tokenID = String(coin.token_id).trim();
      actions.appendChild(
        makeTokenActionButton("Copy ID", () => {
          if (navigator && navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard
              .writeText(tokenID)
              .then(() => {
                logActivity(`Token ID copied: ${tokenID}`);
              })
              .catch(() => {
                logActivity("Token ID copy failed");
              });
            return;
          }
          logActivity("Clipboard unavailable");
        }),
      );
      actions.appendChild(
        makeTokenActionButton("Open IDE", () => {
          openDTLIDE({ token: tokenID, account: state.wallet && state.wallet.address });
        }),
      );
    } else {
      actions.appendChild(
        makeTokenActionButton("Open IDE", () => {
          openDTLIDE({ token: symbol, account: state.wallet && state.wallet.address });
        }),
      );
    }
    right.appendChild(actions);

    row.appendChild(left);
    row.appendChild(right);

    tokenList.appendChild(row);
  }
};

const normalizeTokenBalanceDisplay = (value) => {
  if (value === undefined || value === null) return "0";
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return "0";
    return String(Math.trunc(value));
  }
  const raw = String(value).trim();
  if (!raw) return "0";
  if (/^0x[0-9a-fA-F]+$/.test(raw)) {
    try {
      return BigInt(raw).toString(10);
    } catch (_) {
      return "0";
    }
  }
  return raw;
};

const renderPoolList = (buckets) => {
  poolList.innerHTML = "";
  if (!buckets || buckets.length === 0) {
    poolList.innerHTML = "<div class=\"token-row\">No pools</div>";
    if (poolFromSelect) poolFromSelect.innerHTML = "";
    return;
  }
  if (poolFromSelect) {
    poolFromSelect.innerHTML = "";
    buckets.forEach((bucket) => {
      const opt = document.createElement("option");
      const address = bucket.address || "";
      opt.value = address;
      opt.textContent = `${bucket.name} (${shortAddress(address)})`;
      poolFromSelect.appendChild(opt);
    });
  }
  buckets.forEach((bucket) => {
    const row = document.createElement("div");
    row.className = "pool-row";
    row.dataset.address = bucket.address || "";
    const allocation = formatNumber(bucket.allocation);
    const balance = formatNumber(bucket.balance);
    const percent = bucket.percent !== undefined ? `${bucket.percent}%` : "";
    const address = bucket.address || "—";
    row.innerHTML = `
      <div class="pool-meta">
        <strong>${bucket.name}</strong>
        <span>${balance} / ${allocation} ${percent}</span>
      </div>
      <div class="pool-address mono">${address}</div>
      <div class="pool-actions">
        <button type="button" class="ghost small" data-action="send">Send To</button>
        <button type="button" class="ghost small" data-action="from">Use Pool</button>
      </div>
    `;
    poolList.appendChild(row);
  });
};

const normalizeDTLTokenInput = (raw) => {
  const value = String(raw || "").trim();
  if (!value) return "";
  const bySymbol = state.dtlTokensBySymbol[value.toUpperCase()];
  if (bySymbol && bySymbol.token_id) {
    return String(bySymbol.token_id).trim().toLowerCase();
  }
  return value.toLowerCase();
};

const normalizeDEXPoolID = (raw) => String(raw || "").trim().toLowerCase();

const parseUIntInput = (node, label, { allowZero = false, fallback = 0 } = {}) => {
  if (!node) return fallback;
  const raw = String(node.value || "").trim();
  if (!raw) {
    if (allowZero) return 0;
    throw new Error(`${label} required`);
  }
  if (!/^\d+$/.test(raw)) {
    throw new Error(`${label} must be a whole number`);
  }
  const value = Number.parseInt(raw, 10);
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${label} is out of range`);
  }
  if (!allowZero && value <= 0) {
    throw new Error(`${label} must be > 0`);
  }
  return value;
};

const toRPCDisplayAmount = (value) => {
  try {
    const bi = parseRPCQuantityBigInt(value, "amount");
    if (bi <= BigInt(Number.MAX_SAFE_INTEGER)) {
      return formatNumber(Number(bi));
    }
    return bi.toString();
  } catch (_) {
    return String(value ?? "0");
  }
};

const getCurrentFinalizedHeight = () => {
  const network = state.network && state.network.best ? state.network.best : null;
  const height = Number(network && network.finalized_height ? network.finalized_height : 0);
  return Number.isFinite(height) && height > 0 ? Math.floor(height) : 0;
};

const setDEXStatus = (message, tone = "info") => {
  if (!statusEls.dex) return;
  setStatus(statusEls.dex, message, tone);
};

const listKnownDTLTokens = () => {
  const rows = Object.values(state.dtlTokensBySymbol || {}).filter(
    (row) => row && row.token_id,
  );
  rows.sort((a, b) => {
    const left = String(a.symbol || "").toUpperCase();
    const right = String(b.symbol || "").toUpperCase();
    if (left === right) {
      return String(a.token_id || "").localeCompare(String(b.token_id || ""));
    }
    return left.localeCompare(right);
  });
  return rows;
};

const buildTokenSymbolByID = () => {
  const out = Object.create(null);
  const rows = listKnownDTLTokens();
  rows.forEach((row) => {
    const tokenID = String(row.token_id || "").trim().toLowerCase();
    const symbol = String(row.symbol || "").trim().toUpperCase();
    if (tokenID) out[tokenID] = symbol || tokenID;
  });
  return out;
};

const tokenLabelForDEX = (tokenID, byID) => {
  const id = String(tokenID || "").trim().toLowerCase();
  if (!id) return "—";
  const symbol = byID && byID[id] ? byID[id] : "";
  if (symbol) return `${symbol} (${id.slice(0, 8)}...)`;
  return id;
};

const parseDEXPath = (raw) =>
  String(raw || "")
    .split(/[,\s]+/)
    .map((part) => normalizeDEXPoolID(part))
    .filter((part) => part.length > 0);

const renderDEXQuoteResult = (quote) => {
  if (!dexQuoteResult) return;
  if (!quote) {
    dexQuoteResult.textContent = "—";
    return;
  }
  const path = Array.isArray(quote.best_path) ? quote.best_path.join(" -> ") : "";
  const out = toRPCDisplayAmount(quote.expected_amount_out);
  const impact = quote.price_impact_bps !== undefined ? String(quote.price_impact_bps) : "—";
  const validUntil = quote.valid_until_height !== undefined ? String(quote.valid_until_height) : "—";
  dexQuoteResult.textContent =
    `out=${out} | impact=${impact} bps | path=${path || "n/a"} | deadline<=${validUntil}`;
};

const renderDEXPoolList = () => {
  if (!dexPoolList) return;
  clearElementChildren(dexPoolList);
  const pools = Array.isArray(state.dexPools) ? state.dexPools : [];
  if (!pools.length) {
    appendInfoRow(dexPoolList, "No DTL pools found");
    return;
  }

  const byID = buildTokenSymbolByID();
  pools.forEach((pool) => {
    const poolID = normalizeDEXPoolID(pool.pool_id);
    if (!poolID) return;
    const tokenA = String(pool.token_a || "").trim().toLowerCase();
    const tokenB = String(pool.token_b || "").trim().toLowerCase();

    const row = document.createElement("div");
    row.className = "dex-pool-row";
    row.dataset.poolId = poolID;
    row.dataset.tokenA = tokenA;
    row.dataset.tokenB = tokenB;

    const head = document.createElement("div");
    head.className = "dex-pool-head";
    const title = document.createElement("strong");
    title.textContent = `${tokenLabelForDEX(tokenA, byID)} / ${tokenLabelForDEX(tokenB, byID)}`;
    const idNode = document.createElement("span");
    idNode.className = "mono";
    idNode.textContent = poolID.slice(0, 12) + "...";
    head.appendChild(title);
    head.appendChild(idNode);

    const meta = document.createElement("div");
    meta.className = "dex-pool-meta";
    const reserveA = toRPCDisplayAmount(pool.reserve_a);
    const reserveB = toRPCDisplayAmount(pool.reserve_b);
    const feeBps = String(pool.fee_bps ?? "—");
    meta.textContent = `reserves: ${reserveA} / ${reserveB} | fee: ${feeBps} bps`;

    const actions = document.createElement("div");
    actions.className = "dex-pool-actions";
    const makeBtn = (label, action) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "ghost small";
      btn.dataset.action = action;
      btn.textContent = label;
      return btn;
    };
    actions.appendChild(makeBtn("Use In Swap", "swap"));
    actions.appendChild(makeBtn("Use In Liquidity", "liquidity"));
    actions.appendChild(makeBtn("Open IDE", "ide"));

    row.appendChild(head);
    row.appendChild(meta);
    row.appendChild(actions);
    dexPoolList.appendChild(row);
  });
};

const syncDEXDefaults = () => {
  const tokens = listKnownDTLTokens();
  if (tokens.length >= 1) {
    const firstID = String(tokens[0].token_id || "").trim().toLowerCase();
    if (dexQuoteTokenInInput && !String(dexQuoteTokenInInput.value || "").trim()) {
      dexQuoteTokenInInput.value = firstID;
    }
    if (dexSwapTokenInInput && !String(dexSwapTokenInInput.value || "").trim()) {
      dexSwapTokenInInput.value = firstID;
    }
    if (dexCreateTokenAInput && !String(dexCreateTokenAInput.value || "").trim()) {
      dexCreateTokenAInput.value = firstID;
    }
  }
  if (tokens.length >= 2) {
    const secondID = String(tokens[1].token_id || "").trim().toLowerCase();
    if (dexQuoteTokenOutInput && !String(dexQuoteTokenOutInput.value || "").trim()) {
      dexQuoteTokenOutInput.value = secondID;
    }
    if (dexCreateTokenBInput && !String(dexCreateTokenBInput.value || "").trim()) {
      dexCreateTokenBInput.value = secondID;
    }
  }
  if (Array.isArray(state.dexPools) && state.dexPools.length > 0) {
    const firstPool = normalizeDEXPoolID(state.dexPools[0].pool_id);
    if (dexLiqPoolIdInput && !String(dexLiqPoolIdInput.value || "").trim()) {
      dexLiqPoolIdInput.value = firstPool;
    }
    if (dexRemovePoolIdInput && !String(dexRemovePoolIdInput.value || "").trim()) {
      dexRemovePoolIdInput.value = firstPool;
    }
  }
  if (dexSwapDeadlineInput) {
    const existing = Number.parseInt(String(dexSwapDeadlineInput.value || "0"), 10);
    if (!Number.isFinite(existing) || existing <= 0) {
      const height = getCurrentFinalizedHeight();
      if (height > 0) {
        dexSwapDeadlineInput.value = String(height + 30);
      }
    }
  }
};

const loadDEXPools = async ({ force = false } = {}) => {
  if (!force && inRateLimitCooldown()) return;
  return runWithInFlight("loadDEXPools", async () => {
    try {
      const out = await rpcRequest("dtl_listPools", []);
      state.dexPools = Array.isArray(out) ? out : [];
      renderDEXPoolList();
      syncDEXDefaults();
      if (force) {
        setDEXStatus(`Pools loaded: ${state.dexPools.length}`, "success");
      }
    } catch (err) {
      const message = await formatError(err);
      state.dexPools = [];
      renderDEXPoolList();
      setDEXStatus(message || "DEX pool sync failed", "error");
    }
  });
};

const loadDEXData = async ({ force = false } = {}) => {
  await loadDEXPools({ force });
  syncDEXDefaults();
};

const submitDEXDTLTx = async ({ dtlTxType, payload, amountHint = 1, logLabel = "DEX tx" }) => {
  if (!state.wallet || !state.secretKey) {
    throw new Error("Unlock wallet first");
  }
  const safeAmountHint = Number.isFinite(Number(amountHint)) && Number(amountHint) > 0
    ? Number(amountHint)
    : 1;
  const fee = computeTxFee(safeAmountHint);
  await confirmWalletTransaction({
    to: `DTL:${dtlTxType}`,
    amount: safeAmountHint,
    coin: "MSC",
    fee,
    kind: "send",
  });
  const payloadJSON = JSON.stringify(payload);
  const { txId, retried } = await submitUserTx((nonce) => ({
    from: state.wallet.address,
    to: state.wallet.address,
    amount: 1,
    nonce,
    publicKey: state.wallet.publicKey,
    signature: "",
    fee,
    expiry: Math.floor(Date.now() / 1000) + 120,
    type: 8,
    coin: "MSC",
    dtl_tx_type: dtlTxType,
    dtl_payload: payloadJSON,
  }));
  const suffix = retried ? " (nonce synced)" : "";
  logActivity(`${logLabel} submitted: ${shortAddress(txId)}${suffix}`);
  return { txId, retried };
};

const handleDEXRouteQuote = async (event) => {
  event.preventDefault();
  try {
    const tokenIn = normalizeDTLTokenInput(dexQuoteTokenInInput && dexQuoteTokenInInput.value);
    const tokenOut = normalizeDTLTokenInput(dexQuoteTokenOutInput && dexQuoteTokenOutInput.value);
    const amountIn = parseUIntInput(dexQuoteAmountInInput, "Quote amount");
    const maxHops = parseUIntInput(dexQuoteMaxHopsInput, "Max hops");
    if (!tokenIn || !tokenOut) {
      throw new Error("Token in/out required");
    }
    const query = new URLSearchParams({
      token_in: tokenIn,
      token_out: tokenOut,
      amount_in: String(amountIn),
      max_hops: String(maxHops),
    });
    const quote = await apiWithFallback(`/dtl/route_quote?${query.toString()}`);
    state.dexLastQuote = quote || null;
    renderDEXQuoteResult(state.dexLastQuote);
    setDEXStatus("Route quote ready", "success");
  } catch (err) {
    const message = await formatError(err);
    state.dexLastQuote = null;
    renderDEXQuoteResult(null);
    setDEXStatus(message || "Route quote failed", "error");
  }
};

const applyDEXQuoteToSwap = () => {
  const quote = state.dexLastQuote;
  if (!quote) {
    setDEXStatus("Quote not available", "error");
    return;
  }
  if (dexSwapTokenInInput) {
    dexSwapTokenInInput.value = String(quote.token_in || "").trim().toLowerCase();
  }
  if (dexSwapAmountInInput) {
    dexSwapAmountInInput.value = String(quote.amount_in || "0");
  }
  if (dexSwapPathInput) {
    const path = Array.isArray(quote.best_path) ? quote.best_path.map((x) => String(x).trim().toLowerCase()).join(",") : "";
    dexSwapPathInput.value = path;
  }
  if (dexSwapDeadlineInput && quote.valid_until_height !== undefined) {
    dexSwapDeadlineInput.value = String(quote.valid_until_height);
  }
  if (dexSwapMinOutInput) {
    try {
      const out = parseRPCQuantityBigInt(quote.expected_amount_out ?? "0", "expected_amount_out");
      const min = out > 0n ? (out * 99n) / 100n : 0n;
      dexSwapMinOutInput.value = min.toString();
    } catch (_) {
      dexSwapMinOutInput.value = "0";
    }
  }
  setDEXStatus("Quote applied to swap form", "info");
};

const handleDEXRouteSwap = async (event) => {
  event.preventDefault();
  try {
    const tokenIn = normalizeDTLTokenInput(dexSwapTokenInInput && dexSwapTokenInInput.value);
    const amountIn = parseUIntInput(dexSwapAmountInInput, "Swap amount");
    const minOut = parseUIntInput(dexSwapMinOutInput, "Min amount out", { allowZero: true, fallback: 0 });
    const deadline = parseUIntInput(dexSwapDeadlineInput, "Deadline height");
    const path = parseDEXPath(dexSwapPathInput && dexSwapPathInput.value);
    if (!tokenIn) throw new Error("Token in required");
    if (!path.length) throw new Error("Swap path required");

    const payload = {
      trader: state.wallet.address,
      token_in: tokenIn,
      amount_in: amountIn,
      min_amount_out: minOut,
      path,
      deadline_height: deadline,
    };
    const { retried } = await submitDEXDTLTx({
      dtlTxType: "POOL_SWAP_ROUTE",
      payload,
      amountHint: amountIn,
      logLabel: "Route swap",
    });
    const suffix = retried ? " (nonce synced)" : "";
    setDEXStatus(`Route swap submitted${suffix}`, "success");
    loadTxHistory({ force: true });
    loadCoins({ force: true });
    loadDEXPools({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setDEXStatus(message || "Route swap failed", "error");
  }
};

const handleDEXPoolCreate = async (event) => {
  event.preventDefault();
  try {
    const tokenA = normalizeDTLTokenInput(dexCreateTokenAInput && dexCreateTokenAInput.value);
    const tokenB = normalizeDTLTokenInput(dexCreateTokenBInput && dexCreateTokenBInput.value);
    const amountA = parseUIntInput(dexCreateAmountAInput, "Amount A");
    const amountB = parseUIntInput(dexCreateAmountBInput, "Amount B");
    const feeBPS = parseUIntInput(dexCreateFeeBpsInput, "Fee bps");
    if (!tokenA || !tokenB) throw new Error("Token A/B required");
    if (tokenA === tokenB) throw new Error("Token pair must be different");

    const payload = {
      creator: state.wallet.address,
      token_a: tokenA,
      token_b: tokenB,
      amount_a: amountA,
      amount_b: amountB,
      fee_bps: feeBPS,
    };
    const { retried } = await submitDEXDTLTx({
      dtlTxType: "POOL_CREATE",
      payload,
      amountHint: Math.max(amountA, amountB),
      logLabel: "Pool create",
    });
    const suffix = retried ? " (nonce synced)" : "";
    setDEXStatus(`Pool create submitted${suffix}`, "success");
    loadTxHistory({ force: true });
    loadCoins({ force: true });
    loadDEXPools({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setDEXStatus(message || "Pool create failed", "error");
  }
};

const handleDEXAddLiquidity = async (event) => {
  event.preventDefault();
  try {
    const poolID = normalizeDEXPoolID(dexLiqPoolIdInput && dexLiqPoolIdInput.value);
    const amountA = parseUIntInput(dexLiqAmountAInput, "Amount A");
    const amountB = parseUIntInput(dexLiqAmountBInput, "Amount B");
    const minShares = parseUIntInput(dexLiqMinSharesInput, "Min LP shares", { allowZero: true, fallback: 0 });
    if (!poolID) throw new Error("Pool ID required");

    const payload = {
      provider: state.wallet.address,
      pool_id: poolID,
      amount_a: amountA,
      amount_b: amountB,
      min_lp_shares: minShares,
    };
    const { retried } = await submitDEXDTLTx({
      dtlTxType: "POOL_ADD_LIQUIDITY",
      payload,
      amountHint: Math.max(amountA, amountB),
      logLabel: "Add liquidity",
    });
    const suffix = retried ? " (nonce synced)" : "";
    setDEXStatus(`Add liquidity submitted${suffix}`, "success");
    loadTxHistory({ force: true });
    loadCoins({ force: true });
    loadDEXPools({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setDEXStatus(message || "Add liquidity failed", "error");
  }
};

const handleDEXRemoveLiquidity = async (event) => {
  event.preventDefault();
  try {
    const poolID = normalizeDEXPoolID(dexRemovePoolIdInput && dexRemovePoolIdInput.value);
    const lpShares = parseUIntInput(dexRemoveLPSharesInput, "LP shares");
    const minA = parseUIntInput(dexRemoveMinAInput, "Min amount A", { allowZero: true, fallback: 0 });
    const minB = parseUIntInput(dexRemoveMinBInput, "Min amount B", { allowZero: true, fallback: 0 });
    if (!poolID) throw new Error("Pool ID required");

    const payload = {
      provider: state.wallet.address,
      pool_id: poolID,
      lp_shares: lpShares,
      min_amount_a: minA,
      min_amount_b: minB,
    };
    const { retried } = await submitDEXDTLTx({
      dtlTxType: "POOL_REMOVE_LIQUIDITY",
      payload,
      amountHint: lpShares,
      logLabel: "Remove liquidity",
    });
    const suffix = retried ? " (nonce synced)" : "";
    setDEXStatus(`Remove liquidity submitted${suffix}`, "success");
    loadTxHistory({ force: true });
    loadCoins({ force: true });
    loadDEXPools({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setDEXStatus(message || "Remove liquidity failed", "error");
  }
};

const handleDEXPoolAction = (event) => {
  const actionButton = event.target.closest("button[data-action]");
  if (!actionButton) return;
  const row = actionButton.closest(".dex-pool-row");
  if (!row) return;
  const poolID = normalizeDEXPoolID(row.dataset.poolId);
  const tokenA = normalizeDTLTokenInput(row.dataset.tokenA);
  const tokenB = normalizeDTLTokenInput(row.dataset.tokenB);

  switch (actionButton.dataset.action) {
    case "swap":
      if (dexSwapPathInput) dexSwapPathInput.value = poolID;
      if (dexSwapTokenInInput && tokenA) dexSwapTokenInInput.value = tokenA;
      if (dexQuoteTokenInInput && tokenA) dexQuoteTokenInInput.value = tokenA;
      if (dexQuoteTokenOutInput && tokenB) dexQuoteTokenOutInput.value = tokenB;
      setDEXStatus("Pool copied to swap/quote", "info");
      break;
    case "liquidity":
      if (dexLiqPoolIdInput) dexLiqPoolIdInput.value = poolID;
      if (dexRemovePoolIdInput) dexRemovePoolIdInput.value = poolID;
      setDEXStatus("Pool copied to liquidity forms", "info");
      break;
    case "ide":
      openDTLIDE({ pool: poolID, dtlType: "POOL_SWAP_ROUTE", account: state.wallet && state.wallet.address });
      break;
    default:
      break;
  }
};

const renderTokenomicsChart = (buckets, totalSupply, symbol) => {
  if (!tokenomicsChart || !tokenomicsLegend) return;
  const palette = ["#ff8a2c", "#2ad1a3", "#ffc857", "#2f80ed", "#ef476f", "#6bc2aa"];
  let offset = 0;
  const slices = [];
  tokenomicsLegend.innerHTML = "";
  if (!buckets || buckets.length === 0) {
    tokenomicsChart.style.background =
      "conic-gradient(#2f2d35 0 25%, #24222a 25% 50%, #1c1a21 50% 75%, #15131a 75% 100%)";
    tokenomicsLegend.innerHTML = "<div class=\"log-item\">No buckets</div>";
    return;
  }

  buckets.forEach((bucket, index) => {
    let percent = bucket.percent;
    if ((percent === undefined || percent === null) && totalSupply) {
      percent = (Number(bucket.allocation) / Number(totalSupply)) * 100;
    }
    percent = Number(percent);
    if (!Number.isFinite(percent)) {
      percent = 0;
    }
    const color = palette[index % palette.length];
    const start = offset;
    const end = offset + percent;
    slices.push(`${color} ${start}% ${end}%`);
    offset = end;

    const item = document.createElement("div");
    item.className = "legend-item";
    const main = document.createElement("div");
    main.className = "legend-main";
    const swatch = document.createElement("span");
    swatch.className = "legend-swatch";
    swatch.style.background = color;
    const label = document.createElement("span");
    label.textContent = bucket.name;
    main.appendChild(swatch);
    main.appendChild(label);

    const value = document.createElement("span");
    const allocation = formatNumber(bucket.allocation);
    const pctText = percent !== null && percent !== undefined ? `${percent.toFixed(1)}%` : "—";
    value.textContent = `${allocation} ${symbol || ""} · ${pctText}`;

    item.appendChild(main);
    item.appendChild(value);
    tokenomicsLegend.appendChild(item);
  });

  tokenomicsChart.style.background = `conic-gradient(${slices.join(",")})`;
};

const loadTokenomics = async ({ force = false } = {}) => {
  if (!shouldRunInterval(state.lastTokenomicsSyncAt, TOKENOMICS_SYNC_MS, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    return;
  }

  return runWithInFlight("loadTokenomics", async () => {
    try {
      const data = await apiWithFallback("/tokenomics");
      state.tokenomics = data;
      state.lastTokenomicsSyncAt = Date.now();
      renderPoolList(data.buckets || []);
      if (tokenomicsSupply) {
        tokenomicsSupply.textContent = `${formatNumber(data.total_supply)} ${data.symbol || ""}`;
      }
      if (tokenomicsTotal) {
        tokenomicsTotal.textContent = `${formatNumber(data.total_supply)} ${data.symbol || ""}`;
      }
      renderTokenomicsChart(data.buckets || [], data.total_supply, data.symbol);
      if (poolFromSelect && poolFromSelect.options.length && !poolFromSelect.value) {
        poolFromSelect.value = poolFromSelect.options[0].value;
      }
    } catch (err) {
      applyRateLimitCooldown(err);
      poolList.innerHTML = "<div class=\"token-row\">Failed to load pools</div>";
      if (tokenomicsSupply) tokenomicsSupply.textContent = "—";
      if (tokenomicsTotal) tokenomicsTotal.textContent = "—";
      if (tokenomicsLegend) tokenomicsLegend.innerHTML = "<div class=\"log-item\">No data</div>";
      if (tokenomicsChart) {
        tokenomicsChart.style.background =
          "conic-gradient(#2f2d35 0 25%, #24222a 25% 50%, #1c1a21 50% 75%, #15131a 75% 100%)";
      }
    }
  });
};

const loadCoins = async ({ force = false } = {}) => {
  if (!shouldRunInterval(state.lastCoinsSyncAt, COINS_SYNC_MS, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    return;
  }

  return runWithInFlight("loadCoins", async () => {
    try {
      const data = await apiWithFallback("/coins");
      const baseCoins = data.coins || [];
      let dtlTokens = [];
      let dtlLookupLoaded = false;
      try {
        const account = state.wallet && state.wallet.address ? String(state.wallet.address) : "";
        const dtlRes = await rpcRequest("dtl_listTokens", account ? [account] : []);
        if (Array.isArray(dtlRes)) {
          dtlTokens = dtlRes;
          dtlLookupLoaded = true;
        }
      } catch (_) {
        // Keep wallet UX stable even if DTL read RPC is unavailable.
      }

      const seenSymbols = new Set();
      const baseLookup = Object.create(null);
      const dtlLookup = Object.create(null);
      const coins = [];
      baseCoins.forEach((coin) => {
        const symbol = String(coin && coin.symbol ? coin.symbol : "").trim();
        if (!symbol) return;
        const key = symbol.toUpperCase();
        if (seenSymbols.has(key)) return;
        seenSymbols.add(key);
        baseLookup[key] = {
          symbol: key,
          name: String(coin.name || key),
          decimals: Number(coin.decimals || 0),
          kind: "base",
        };
        coins.push({ ...coin, kind: "base", metadata_uri: "" });
      });
      dtlTokens.forEach((token) => {
        const symbol = String(token && token.symbol ? token.symbol : "").trim();
        if (!symbol) return;
        const key = symbol.toUpperCase();
        if (seenSymbols.has(key)) return;
        seenSymbols.add(key);
        const tokenID = String(token && token.token_id ? token.token_id : "").trim();
        const metadataURI = String(token && token.metadata_uri ? token.metadata_uri : "").trim();
        if (tokenID) {
          dtlLookup[key] = {
            symbol: key,
            token_id: tokenID,
            name: String(token.name || key),
            decimals: Number(token.decimals || 0),
            kind: "dtl",
            metadata_uri: metadataURI,
          };
        }
        coins.push({
          symbol,
          name: String(token.name || symbol),
          decimals: Number(token.decimals || 0),
          kind: "dtl",
          token_id: tokenID,
          metadata_uri: metadataURI,
          _dtl_balance_hint: normalizeTokenBalanceDisplay(token.balance),
        });
      });
      state.baseCoinsBySymbol = baseLookup;
      if (dtlLookupLoaded) {
        state.dtlTokensBySymbol = dtlLookup;
      }

      if (!state.wallet) {
        state.lastCoinsSyncAt = Date.now();
        await renderTokenList(coins, null);
        await loadNFTPortfolio({ force: true });
        await loadDEXData({ force });
        return;
      }

      const balances = {};
      await Promise.allSettled(
        coins.map(async (coin) => {
          if (coin.kind === "dtl") {
            if (coin._dtl_balance_hint !== undefined) {
              balances[coin.symbol] = coin._dtl_balance_hint;
              return;
            }
            const out = await rpcRequest("dtl_balanceOf", [coin.symbol, state.wallet.address]);
            balances[coin.symbol] = normalizeTokenBalanceDisplay(out);
            return;
          }
          const bal = await apiWithFallback(
            `/balance?address=${encodeURIComponent(state.wallet.address)}&coin=${encodeURIComponent(coin.symbol)}&state=finalized`,
          );
          balances[coin.symbol] = bal.balance;
        }),
      );
      state.lastCoinsSyncAt = Date.now();
      await renderTokenList(coins, balances);
      await loadNFTPortfolio({ force });
      await loadDEXData({ force });
    } catch (err) {
      applyRateLimitCooldown(err);
      clearElementChildren(tokenList);
      appendInfoRow(tokenList, "Failed to load tokens");
    }
  });
};

const setNFTContainerMessage = (container, message) => {
  if (!container) return;
  clearElementChildren(container);
  appendInfoRow(container, message);
};

const setActiveNFTTab = (tab) => {
  const normalized = tab === "1155" ? "1155" : "721";
  state.nftTab = normalized;

  if (nftTab721Btn) nftTab721Btn.classList.toggle("active", normalized === "721");
  if (nftTab1155Btn) nftTab1155Btn.classList.toggle("active", normalized === "1155");
  if (nft721List) nft721List.classList.toggle("hidden", normalized !== "721");
  if (nft1155List) nft1155List.classList.toggle("hidden", normalized !== "1155");
};

const makeNFTFallbackThumb = (tokenID) => {
  const node = document.createElement("div");
  node.className = "nft-thumb-fallback";
  const label = String(tokenID || "NFT").replace(/\s+/g, "").slice(0, 8).toUpperCase() || "NFT";
  node.textContent = label;
  return node;
};

const makeNFTThumbNode = (imageURL, tokenID) => {
  const fallback = makeNFTFallbackThumb(tokenID);
  if (!imageURL) return fallback;

  const img = document.createElement("img");
  img.className = "nft-thumb";
  img.alt = `NFT ${String(tokenID || "")}`;
  img.decoding = "async";
  img.loading = "lazy";
  img.src = imageURL;
  img.onerror = () => {
    img.replaceWith(fallback);
  };
  return img;
};

const renderNFTCards = async (container, items, kind) => {
  if (!container) return;
  clearElementChildren(container);

  if (!state.wallet) {
    setNFTContainerMessage(container, `Connect wallet to view NFT${kind} assets`);
    return;
  }

  if (!Array.isArray(items) || items.length === 0) {
    setNFTContainerMessage(container, `No NFT${kind} assets`);
    return;
  }

  const imageURLs = await Promise.all(
    items.map((item) =>
      resolveNFTImageURL(item, kind).catch(() => ""),
    ),
  );

  for (let i = 0; i < items.length; i++) {
    const item = items[i] || {};
    const tokenID = String(item.token_id || "").trim() || "0";
    const collectionSymbol = String(item.collection_symbol || "").trim();
    const collectionName = String(item.collection_name || "").trim();
    const collectionID = String(item.collection_id || "").trim();

    const card = document.createElement("div");
    card.className = "nft-card";
    card.appendChild(makeNFTThumbNode(imageURLs[i] || "", tokenID));

    const meta = document.createElement("div");
    meta.className = "nft-meta";

    const title = document.createElement("div");
    title.className = "nft-title";
    title.textContent = collectionSymbol
      ? `${collectionSymbol} #${tokenID}`
      : `${collectionName || "NFT"} #${tokenID}`;
    meta.appendChild(title);

    const sub = document.createElement("div");
    sub.className = "nft-sub";
    sub.textContent = collectionName || collectionID || "Unknown collection";
    meta.appendChild(sub);

    if (kind === "1155") {
      const balanceLine = document.createElement("div");
      balanceLine.className = "nft-sub";
      balanceLine.textContent = `Balance: ${normalizeTokenBalanceDisplay(item.balance)}`;
      meta.appendChild(balanceLine);
    }

    const actions = document.createElement("div");
    actions.className = "nft-actions";
    actions.appendChild(
      makeTokenActionButton("Open IDE", () => {
        openDTLIDE({
          token: collectionID,
          account: state.wallet && state.wallet.address,
          from: state.wallet && state.wallet.address,
          dtlType: kind === "1155" ? "NFT1155_TRANSFER" : "NFT721_TRANSFER",
        });
      }),
    );
    meta.appendChild(actions);

    card.appendChild(meta);
    container.appendChild(card);
  }
};

const parseNFTItems = (result) => {
  if (result && typeof result === "object" && Array.isArray(result.items)) {
    return result.items;
  }
  if (Array.isArray(result)) {
    return result;
  }
  return [];
};

const loadNFTPortfolio = async ({ force = false } = {}) => {
  if (!shouldRunInterval(state.lastNFTSyncAt, NFT_SYNC_MS, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    return;
  }

  return runWithInFlight("loadNFTPortfolio", async () => {
    if (!state.wallet || !state.wallet.address) {
      state.nft721Items = [];
      state.nft1155Items = [];
      state.lastNFTSyncAt = Date.now();
      setNFTContainerMessage(nft721List, "Connect wallet to view NFT721 assets");
      setNFTContainerMessage(nft1155List, "Connect wallet to view NFT1155 assets");
      setActiveNFTTab(state.nftTab);
      return;
    }

    try {
      const account = String(state.wallet.address || "").trim();
      const [res721, res1155] = await Promise.all([
        rpcRequest("dtl_listNFT721ByOwner", [account, 0, 200]),
        rpcRequest("dtl_listNFT1155ByOwner", [account, 0, 200]),
      ]);
      state.nft721Items = parseNFTItems(res721);
      state.nft1155Items = parseNFTItems(res1155);
      state.lastNFTSyncAt = Date.now();

      await Promise.all([
        renderNFTCards(nft721List, state.nft721Items, "721"),
        renderNFTCards(nft1155List, state.nft1155Items, "1155"),
      ]);
      setActiveNFTTab(state.nftTab);
    } catch (err) {
      applyRateLimitCooldown(err);
      setNFTContainerMessage(nft721List, "Failed to load NFT721 assets");
      setNFTContainerMessage(nft1155List, "Failed to load NFT1155 assets");
      setActiveNFTTab(state.nftTab);
    }
  });
};

const renderValidatorChips = (container, items, className) => {
  if (!container) return;
  container.innerHTML = "";
  if (!items || items.length === 0) {
    container.innerHTML = "<div class=\"token-row\">None</div>";
    return;
  }
  items.forEach((item) => {
    const chip = document.createElement("div");
    chip.className = `validator-chip ${className || ""}`.trim();
    chip.textContent = item.label || item;
    container.appendChild(chip);
  });
};

const DEFAULT_STAKE_VALIDATOR_PUBKEY_HINT =
  "Required for first non-core stake. Auto-filled from this node when available.";

const sameValidatorID = (a, b) =>
  String(a || "").trim().toLowerCase() === String(b || "").trim().toLowerCase();

const setStakeValidatorPubKeyFieldState = ({
  value = "",
  autofilled = false,
  userEdited = false,
} = {}) => {
  if (!stakeValidatorPubKeyInput) return;
  stakeValidatorPubKeyInput.value = value;
  stakeValidatorPubKeyInput.dataset.autofilled = autofilled ? "1" : "0";
  stakeValidatorPubKeyInput.dataset.userEdited = userEdited ? "1" : "0";
};

const stakeValidatorPubKeyWasAutofilled = () =>
  Boolean(stakeValidatorPubKeyInput && stakeValidatorPubKeyInput.dataset.autofilled === "1");

const stakeValidatorPubKeyWasUserEdited = () =>
  Boolean(stakeValidatorPubKeyInput && stakeValidatorPubKeyInput.dataset.userEdited === "1");

const setStakeValidatorPubKeyMessage = (
  message = "Validator pubkey: —",
  tone = "info",
  hint = DEFAULT_STAKE_VALIDATOR_PUBKEY_HINT,
) => {
  if (stakeValidatorPubKeyState) {
    setStatus(stakeValidatorPubKeyState, message, tone);
  }
  if (stakeValidatorPubKeyHint) {
    stakeValidatorPubKeyHint.textContent = hint;
  }
};

const focusStakeValidatorPubKeyField = () => {
  if (!stakeValidatorPubKeyInput) return;
  stakeValidatorPubKeyInput.focus();
  if (typeof stakeValidatorPubKeyInput.select === "function") {
    stakeValidatorPubKeyInput.select();
  }
};

const setStakeActivationHint = (message) => {
  if (stakeActivationHint) {
    stakeActivationHint.textContent = message;
  }
};

const updateStakeValidatorPubKeyUI = () => {
  if (!stakeValidatorPubKeyInput) return;

  const selectedValidatorID = el("stakeValidator")?.value.trim() || "";
  const localValidatorID = String(state.walletStatus?.local_validator_id || "").trim();
  const walletValidatorID = String(state.walletStatus?.validator_id || "").trim();
  const walletStake = Number(state.walletStatus?.stake || 0);
  const walletAnchored = Boolean(state.walletStatus?.validator_consensus_pubkey_anchored);
  const walletSource = String(
    state.walletStatus?.validator_consensus_pubkey_source || "none",
  ).trim();
  const localKeyLoaded = Boolean(state.walletStatus?.local_validator_key_loaded);
  const localAnchored = Boolean(state.walletStatus?.local_validator_consensus_pubkey_anchored);
  const localSource = String(
    state.walletStatus?.local_validator_consensus_pubkey_source || "none",
  ).trim();

  let localValidatorPubKey = "";
  try {
    localValidatorPubKey = normalizeValidatorPubKeyHex(
      state.walletStatus?.local_validator_consensus_pubkey || "",
    );
  } catch (_) {
    localValidatorPubKey = "";
  }
  let walletValidatorPubKey = "";
  try {
    walletValidatorPubKey = normalizeValidatorPubKeyHex(
      state.walletStatus?.validator_consensus_pubkey || "",
    );
  } catch (_) {
    walletValidatorPubKey = "";
  }

  if (stakeValidatorPubKeyWasAutofilled() && !sameValidatorID(selectedValidatorID, localValidatorID)) {
    setStakeValidatorPubKeyFieldState({ value: "", autofilled: false, userEdited: false });
  }

  const currentValue = String(stakeValidatorPubKeyInput.value || "").trim();
  const matchesLocalValidator = sameValidatorID(selectedValidatorID, localValidatorID);
  const matchesWalletValidator = sameValidatorID(selectedValidatorID, walletValidatorID);

  if (!selectedValidatorID) {
    setStakeValidatorPubKeyMessage("Validator pubkey: —", "info");
    return;
  }

  if (matchesLocalValidator) {
    if (localKeyLoaded && localValidatorPubKey) {
      if (
        !stakeValidatorPubKeyWasUserEdited() &&
        (!currentValue || stakeValidatorPubKeyWasAutofilled())
      ) {
        setStakeValidatorPubKeyFieldState({
          value: localValidatorPubKey,
          autofilled: true,
          userEdited: false,
        });
      }

      const anchorText = localAnchored
        ? `Anchored via ${localSource.replace(/_/g, " ")}.`
        : "Loaded from this node but not yet anchored in registry state.";
      if (stakeValidatorPubKeyWasAutofilled()) {
        setStakeValidatorPubKeyMessage(
          "Validator pubkey: auto-filled",
          "success",
          `${DEFAULT_STAKE_VALIDATOR_PUBKEY_HINT} ${anchorText}`,
        );
        return;
      }
      if (currentValue) {
        setStakeValidatorPubKeyMessage(
          "Validator pubkey: manual override",
          "info",
          `${DEFAULT_STAKE_VALIDATOR_PUBKEY_HINT} ${anchorText}`,
        );
        return;
      }
    } else if (!currentValue) {
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: local key unavailable",
        "error",
        "This node does not currently have a loaded validator consensus key. Paste the validator consensus pubkey manually for first non-core stake.",
      );
      return;
    }
  }

  if (matchesWalletValidator && walletStake > 0) {
    if (walletAnchored && walletValidatorPubKey) {
      if (!currentValue && !stakeValidatorPubKeyWasUserEdited()) {
        setStakeValidatorPubKeyFieldState({
          value: walletValidatorPubKey,
          autofilled: true,
          userEdited: false,
        });
      }
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: anchored",
        "success",
        `Wallet validator pubkey is anchored via ${walletSource.replace(/_/g, " ")}.`,
      );
      return;
    }
    if (!currentValue) {
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: required",
        "error",
        "This wallet already has stake, but validator consensus pubkey is not anchored. Paste the validator pubkey and submit Add Stake once.",
      );
      return;
    }
  }

  if (currentValue) {
    try {
      normalizeValidatorPubKeyHex(currentValue);
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: manual",
        "info",
        "Manual pubkey entry will be sent with the stake transaction if provided.",
      );
    } catch (err) {
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: invalid",
        "error",
        err.message || "Validator consensus pubkey must be 32-byte hex",
      );
    }
    return;
  }

  setStakeValidatorPubKeyMessage(
    "Validator pubkey: optional",
    "info",
    "Leave blank for already-anchored validators. For first non-core stake, paste the validator consensus pubkey if auto-fill is unavailable.",
  );
};

const updateStakeValidatorStatus = () => {
  if (!stakeValidatorState) return;
  const id = el("stakeValidator").value.trim();
  const stakeBtn = el("stakeForm")?.querySelector("button[type='submit']");
  const walletValidator = state.walletStatus?.validator_id || "";
  const boundMismatch = Boolean(walletValidator && id && walletValidator !== id);
  updateStakeValidatorPubKeyUI();
  if (stakeBtn) {
    stakeBtn.disabled = state.staking || boundMismatch;
  }
  if (boundMismatch) {
    setStatus(
      stakeValidatorState,
      `Wallet already bound to validator ${walletValidator}`,
      "error",
    );
    setStakeActivationHint("Use this wallet's bound validator id before changing stake.");
    return;
  }
  if (!id || !state.validatorSnapshot) {
    setStatus(stakeValidatorState, "Validator status: —", "info");
    setStakeActivationHint("Activation depends on validator stake, consensus pubkey, and scheduling.");
    return;
  }

  const {
    active = [],
    offline = [],
    pendingAdds = [],
    pendingRemoves = [],
    height,
  } = state.validatorSnapshot;
  const isActive = active.some((v) => sameValidatorID(v, id));
  const isOffline = offline.some((v) => sameValidatorID(v, id));
  const pendingRemove = pendingRemoves.find((p) => sameValidatorID(p.id, id));
  if (isActive && pendingRemove) {
    const remaining = Math.max(0, Number(pendingRemove.activation_height) - Number(height || 0));
    setStatus(
      stakeValidatorState,
      `Validator ${id}: ACTIVE (LEAVING ${remaining} blocks)`,
      "info",
    );
    return;
  }
  if (isActive) {
    if (isOffline) {
      setStatus(stakeValidatorState, `Validator ${id}: ACTIVE (OFFLINE)`, "error");
      setStakeActivationHint("Validator is in the active set but currently offline.");
      return;
    }
    setStatus(stakeValidatorState, `Validator ${id}: ACTIVE`, "success");
    setStakeActivationHint("Validator is active in the current validator set.");
    return;
  }
  const pendingAdd = pendingAdds.find((p) => sameValidatorID(p.id, id));
  if (pendingAdd) {
    const remaining = Math.max(0, Number(pendingAdd.activation_height) - Number(height || 0));
    setStatus(
      stakeValidatorState,
      `Validator ${id}: PENDING (${remaining} blocks)`,
      "info",
    );
    setStakeActivationHint(`Pending add activates at height ${formatNumber(pendingAdd.activation_height)} (${remaining} blocks).`);
    return;
  }
  if (pendingRemove) {
    const remaining = Math.max(0, Number(pendingRemove.activation_height) - Number(height || 0));
    setStatus(
      stakeValidatorState,
      `Validator ${id}: LEAVING (${remaining} blocks)`,
      "error",
    );
    setStakeActivationHint(`Pending remove activates at height ${formatNumber(pendingRemove.activation_height)} (${remaining} blocks).`);
    return;
  }
  setStatus(stakeValidatorState, `Validator ${id}: INACTIVE`, "error");
  const walletStatus = state.walletStatus || {};
  const walletStake = Number(walletStatus.stake || 0);
  const matchesWalletValidator = sameValidatorID(id, walletStatus.validator_id || "");
  if (
    matchesWalletValidator &&
    walletStake > 0 &&
    !walletStatus.validator_consensus_pubkey_anchored
  ) {
    setStakeActivationHint("Stake exists, but validator consensus pubkey is not anchored yet.");
  } else if (
    sameValidatorID(id, walletStatus.local_validator_id || "") &&
    !walletStatus.local_validator_key_loaded
  ) {
    setStakeActivationHint("This public node has no loaded validator key; run a validator node or paste its consensus pubkey.");
  } else {
    setStakeActivationHint("Inactive validators need valid stake, an anchored consensus pubkey, and scheduler admission.");
  }
};

const applyWalletStatusUI = () => {
  if (!walletValidatorSummary || !walletStakeMeta) return;
  const stakeValidatorInput = el("stakeValidator");
  const unstakeValidatorInput = el("unstakeValidator");
  const stakeBtn = el("stakeForm")?.querySelector("button[type='submit']");
  const unstakeBtn = el("unstakeForm")?.querySelector("button[type='submit']");

  if (!state.wallet) {
    setStatus(walletValidatorSummary, "Wallet validator: —", "info");
    walletStakeMeta.textContent = "Connect wallet to see stake status.";
    if (stakeBtn) stakeBtn.textContent = "Submit Stake";
    if (unstakeBtn) unstakeBtn.disabled = true;
    if (stakeValidatorInput) stakeValidatorInput.disabled = false;
    if (unstakeValidatorInput) unstakeValidatorInput.disabled = false;
    updateStakeValidatorStatus();
    return;
  }

  if (state.walletStatusError) {
    setStatus(walletValidatorSummary, "Wallet validator: unavailable", "error");
    walletStakeMeta.textContent = state.walletStatusError;
    if (stakeBtn) stakeBtn.textContent = "Submit Stake";
    if (unstakeBtn) unstakeBtn.disabled = true;
    if (stakeValidatorInput) stakeValidatorInput.disabled = false;
    if (unstakeValidatorInput) unstakeValidatorInput.disabled = false;
    updateStakeValidatorStatus();
    return;
  }

  const status = state.walletStatus;
  if (!status) {
    setStatus(walletValidatorSummary, "Wallet validator: —", "info");
    walletStakeMeta.textContent = "Fetching stake status…";
    if (stakeBtn) stakeBtn.textContent = "Submit Stake";
    if (unstakeBtn) unstakeBtn.disabled = true;
    if (stakeValidatorInput) stakeValidatorInput.disabled = false;
    if (unstakeValidatorInput) unstakeValidatorInput.disabled = false;
    updateStakeValidatorStatus();
    return;
  }

  const vid = status.validator_id || "";
  const stake = Number(status.stake || 0);
  const lockedUntil = Number(status.locked_until_epoch || 0);
  const statusLabel = status.status || (vid ? "inactive" : "none");

  if (!vid) {
    setStatus(walletValidatorSummary, "Wallet validator: none", "info");
    walletStakeMeta.textContent = "No validator bound. Stake to become candidate.";
    if (stakeBtn) stakeBtn.textContent = "Submit Stake";
    if (unstakeBtn) unstakeBtn.disabled = true;
    if (stakeValidatorInput) stakeValidatorInput.disabled = false;
    if (unstakeValidatorInput) unstakeValidatorInput.disabled = false;
    updateStakeValidatorStatus();
    return;
  }

  let tone = "info";
  if (statusLabel === "active") tone = "success";
  else if (statusLabel === "active_pending_remove") tone = "info";
  else if (statusLabel === "pending_add") tone = "info";
  else if (statusLabel === "pending_remove" || statusLabel === "inactive") tone = "error";

  const label = statusLabel.replace(/_/g, " ");
  setStatus(walletValidatorSummary, `Wallet validator: ${vid} (${label})`, tone);
  const lockText = lockedUntil ? `Locked until epoch ${formatNumber(lockedUntil)}` : "Unlocked";
  walletStakeMeta.textContent = `Stake: ${formatNumber(stake)} MSC · ${lockText}`;

  if (stakeValidatorInput) {
    stakeValidatorInput.value = vid;
    stakeValidatorInput.disabled = true;
  }
  if (unstakeValidatorInput) {
    unstakeValidatorInput.value = vid;
    unstakeValidatorInput.disabled = false;
  }
  if (stakeBtn) stakeBtn.textContent = "Add Stake";
  if (unstakeBtn) unstakeBtn.disabled = stake <= 0;

  updateStakeValidatorStatus();
};

const loadWalletStatus = async ({ force = false } = {}) => {
  if (!shouldRunInterval(state.lastWalletStatusSyncAt, WALLET_STATUS_SYNC_MS, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    return;
  }
  if (!state.wallet) {
    state.walletStatus = null;
    state.walletStatusError = "";
    state.lastWalletStatusSyncAt = Date.now();
    applyWalletStatusUI();
    return;
  }
  return runWithInFlight("loadWalletStatus", async () => {
    try {
      const data = await apiWithFallback(
        `/wallet/status?address=${encodeURIComponent(state.wallet.address)}`,
      );
      state.walletStatus = data;
      state.walletStatusError = "";
      state.lastWalletStatusSyncAt = Date.now();
      applyWalletStatusUI();
    } catch (err) {
      applyRateLimitCooldown(err);
      const message = await formatError(err);
      state.walletStatus = null;
      state.walletStatusError = message;
      applyWalletStatusUI();
    }
  });
};

const loadValidators = async ({ force = false } = {}) => {
  if (!shouldRunInterval(state.lastValidatorsSyncAt, VALIDATORS_SYNC_MS, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    return;
  }

  return runWithInFlight("loadValidators", async () => {
    try {
      const net = state.network || (await fetchNetworkStatus());
      const height = net?.finalizedHeight ? net.finalizedHeight + 1 : undefined;
      const data = await apiWithFallback(height ? `/validators?height=${height}` : "/validators");
      const validators = Array.isArray(data.validators) ? data.validators : [];
      const onlineValidators = Array.isArray(data.online_validators)
        ? data.online_validators
        : validators;
      const onlineLookup = new Set(
        onlineValidators.map((v) => String(v || "").trim().toLowerCase()).filter(Boolean),
      );
      const inactiveValidators = Array.isArray(data.inactive_validators)
        ? data.inactive_validators
        : validators.filter((v) => !onlineLookup.has(String(v || "").trim().toLowerCase()));
      validatorList.innerHTML = "";
      validators.forEach((val) => {
        const opt = document.createElement("option");
        opt.value = val;
        validatorList.appendChild(opt);
      });
      if (validators.length && !el("stakeValidator").value) {
        el("stakeValidator").value = validators[0];
      }
      if (validators.length && !el("unstakeValidator").value) {
        el("unstakeValidator").value = validators[0];
      }

      let pendingAdds = [];
      let pendingRemoves = [];
      let pendingHeight = data.height || 0;
      try {
        const pending = await apiWithFallback("/validators/pending");
        pendingAdds = pending.pending_add || [];
        pendingRemoves = pending.pending_remove || [];
        pendingHeight = pending.height || pendingHeight;
      } catch (err) {
        applyRateLimitCooldown(err);
        if (!shouldRetry(err)) {
          pendingAdds = [];
          pendingRemoves = [];
        }
      }

      state.validatorSnapshot = {
        active: validators,
        online: onlineValidators,
        offline: inactiveValidators,
        pendingAdds,
        pendingRemoves,
        height: pendingHeight,
      };

      const activeLabels = onlineValidators.map((id) => ({ label: id }));
      renderValidatorChips(activeValidatorList, activeLabels, "active");
      const inactiveLabels = inactiveValidators.map((id) => ({ label: id }));
      renderValidatorChips(inactiveValidatorList, inactiveLabels, "inactive");

      const pendingEntries = [];
      pendingAdds.forEach((entry) => {
        const remaining = Math.max(0, Number(entry.activation_height) - Number(pendingHeight || 0));
        pendingEntries.push({
          label: `+${entry.id} (${remaining})`,
          kind: "pending",
        });
      });
      pendingRemoves.forEach((entry) => {
        const remaining = Math.max(0, Number(entry.activation_height) - Number(pendingHeight || 0));
        pendingEntries.push({
          label: `-${entry.id} (${remaining})`,
          kind: "remove",
        });
      });

      if (pendingEntries.length === 0 && pendingValidatorList) {
        pendingValidatorList.innerHTML = "<div class=\"token-row\">No pending changes</div>";
      } else {
        pendingValidatorList.innerHTML = "";
        pendingEntries.forEach((entry) => {
          const chip = document.createElement("div");
          chip.className = `validator-chip ${entry.kind === "remove" ? "remove" : "pending"}`;
          chip.textContent = entry.label;
          pendingValidatorList.appendChild(chip);
        });
      }

      state.lastValidatorsSyncAt = Date.now();
      updateStakeValidatorStatus();
    } catch (err) {
      applyRateLimitCooldown(err);
      validatorList.innerHTML = "";
      if (activeValidatorList) {
        activeValidatorList.innerHTML = "<div class=\"token-row\">Unavailable</div>";
      }
      if (pendingValidatorList) {
        pendingValidatorList.innerHTML = "<div class=\"token-row\">Unavailable</div>";
      }
      if (inactiveValidatorList) {
        inactiveValidatorList.innerHTML = "<div class=\"token-row\">Unavailable</div>";
      }
      if (statusEls.validator) {
        setStatus(statusEls.validator, "Validator sync failed", "error");
      }
    }
  });
};

const loadTxHistory = async ({ force = false } = {}) => {
  if (!shouldRunInterval(state.lastTxHistorySyncAt, TX_HISTORY_SYNC_MS, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    return;
  }
  if (!state.wallet) {
    txList.innerHTML = "<div class=\"log-item\">Unlock wallet to view history</div>";
    state.lastTxHistorySyncAt = Date.now();
    return;
  }

  return runWithInFlight("loadTxHistory", async () => {
    setStatus(statusEls.tx, "Loading", "info");
    try {
      const data = await apiWithFallback(
        `/txs?address=${encodeURIComponent(state.wallet.address)}&limit=20`,
      );
      const txs = data.txs || [];
      if (txs.length === 0) {
        txList.innerHTML = "<div class=\"log-item\">No transactions yet</div>";
      } else {
        txList.innerHTML = "";
        txs.forEach((tx) => {
          const dir = tx.from === state.wallet.address ? "Sent" : "Received";
          let typeLabel = "Transfer";
          if (tx.type === 2) {
            typeLabel = "Stake";
          } else if (tx.type === 6) {
            typeLabel = "Unstake";
          } else if (tx.type === 7) {
            typeLabel = "EVM";
          }
          const peer = tx.from === state.wallet.address ? tx.to : tx.from;
          const item = document.createElement("div");
          item.className = "log-item";
          item.textContent = `#${tx.height} ${typeLabel} · ${dir} ${tx.amount} ${tx.coin} · ${peer}`;
          txList.appendChild(item);
        });
      }
      state.lastTxHistorySyncAt = Date.now();
      setStatus(statusEls.tx, "Updated", "success");
    } catch (err) {
      applyRateLimitCooldown(err);
      txList.innerHTML = "<div class=\"log-item\">Failed to load transactions</div>";
      const message = await formatError(err);
      setStatus(statusEls.tx, message, "error");
    }
  });
};

const updateWalletUI = () => {
  const wallet = state.wallet;
  el("walletAddress").textContent = wallet ? wallet.address : "—";
  el("walletPublicKey").textContent = wallet ? wallet.publicKey : "—";
  const authWalletPublicKeyEl = el("authWalletPublicKey");
  if (authWalletPublicKeyEl) {
    authWalletPublicKeyEl.textContent = wallet ? wallet.publicKey : "—";
  }
  const evmAddressEl = el("walletEvmAddress");
  if (evmAddressEl) {
    evmAddressEl.textContent = "—";
  }
  if (wallet) {
    el("balanceAddress").value = wallet.address;
    el("faucetAddress").value = wallet.address;
    if (poolToInput) {
      poolToInput.value = wallet.address;
    }
    syncDEXDefaults();
  }
  if (state.secretKey) {
    setStatus(el("walletState"), "Unlocked", "success");
  } else if (wallet) {
    setExportedPrivateKey("");
    setStatus(el("walletState"), "Locked", "info");
  } else {
    setExportedPrivateKey("");
    setStatus(el("walletState"), "No wallet", "error");
  }
  if (authStatus) {
    if (state.secretKey) {
      setStatus(authStatus, "Ready to sign", "info");
    } else {
      setStatus(authStatus, "Unlock wallet first", "info");
    }
  }

  if (!wallet) {
    state.walletStatus = null;
    state.walletStatusError = "";
    state.walletEVMAddress = "";
  } else if (state.walletStatus?.wallet && state.walletStatus.wallet !== wallet.address) {
    state.walletStatus = null;
    state.walletStatusError = "";
    state.walletEVMAddress = "";
  }
  applyWalletStatusUI();

  if (wallet && evmAddressEl) {
    ensureWalletEVMAddress()
      .then((alias) => {
        evmAddressEl.textContent = alias || "—";
      })
      .catch(() => {
        evmAddressEl.textContent = "—";
      });
  }

  syncInjectedProviderState({ emitAccounts: true, emitChain: false });
};

const autoFillReceiveAddress = () => {
  if (!state.wallet || !state.wallet.address) return;
  const addr = state.wallet.address;
  const balanceEl = el("balanceAddress");
  if (balanceEl && (!balanceEl.value || balanceEl.value === "—")) {
    balanceEl.value = addr;
  }
  const faucetEl = el("faucetAddress");
  if (faucetEl && (!faucetEl.value || faucetEl.value === "—")) {
    faucetEl.value = addr;
  }
  if (poolToInput && (!poolToInput.value || poolToInput.value === "—")) {
    poolToInput.value = addr;
  }
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
  const stripHexPrefix = (value) => String(value || "").trim().replace(/^0x/i, "");
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
  pushInt64(tx.evm_gas_limit || tx.evmGasLimit || 0);
  pushString(stripHexPrefix(tx.evm_code || tx.evmCode || ""));
  pushString(stripHexPrefix(tx.evm_input || tx.evmInput || ""));
  pushString(stripHexPrefix(tx.evm_raw_tx || tx.evmRawTx || ""));
  pushString(stripHexPrefix(tx.evm_tx_hash || tx.evmTxHash || ""));
  if (txType === 8) {
    pushString(String(tx.dtl_tx_type || tx.DTLTxType || "").trim());
    pushString(String(tx.dtl_token_id || tx.DTLTokenID || "").trim());
    pushString(String(tx.dtl_payload || tx.DTLPayload || "").trim());
    pushString(String(tx.dtl_governance_cert || tx.DTLGovernanceCert || "").trim());
  }
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
  connectToRPC({ persist: true });
};

const createWallet = async (event) => {
  event.preventDefault();
  const password = el("createPassword").value.trim();
  if (!password) {
    setStatus(el("walletState"), "Password required", "error");
    return;
  }

  let hdSelection;
  try {
    hdSelection = readHDSelection("create");
  } catch (err) {
    setStatus(el("walletState"), err.message || "Invalid HD inputs", "error");
    return;
  }

  let mnemonic;
  let keyPair;
  let hd;
  let address;
  let cryptoData;
  try {
    mnemonic = await bip39.generateMnemonic(256);
    ({ keyPair, hd } = await deriveHDKeyPairFromMnemonic(mnemonic, password, hdSelection));
    address = await addressFromPublicKey(keyPair.publicKey, state.chainId);
    cryptoData = await encryptSecretKey(keyPair.secretKey, password);
  } catch (err) {
    setStatus(el("walletState"), err.message || "Wallet create failed", "error");
    logActivity(`Wallet create failed: ${err.message || err}`);
    return;
  }

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
  setStatus(el("walletState"), `Wallet created (${hd.path})`, "success");
  logActivity(`Wallet created • ${hd.path}`);
  loadCoins({ force: true });
  loadTxHistory({ force: true });
  refreshBalance({ force: true });
  loadWalletStatus({ force: true });
};

const importMnemonic = async (event) => {
  event.preventDefault();
  const mnemonic = el("importMnemonic").value.trim();
  const password = el("importPassword").value.trim();
  if (!mnemonic || !password) {
    setStatus(el("walletState"), "Mnemonic + password required", "error");
    return;
  }
  if (!(await bip39.validateMnemonic(mnemonic))) {
    setStatus(el("walletState"), "Invalid mnemonic", "error");
    return;
  }

  let hdSelection;
  try {
    hdSelection = readHDSelection("import");
  } catch (err) {
    setStatus(el("walletState"), err.message || "Invalid HD inputs", "error");
    return;
  }

  const { keyPair, hd } = await deriveHDKeyPairFromMnemonic(mnemonic, password, hdSelection);
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
  updateWalletUI();
  setStatus(el("walletState"), `Wallet imported (${hd.path})`, "success");
  logActivity(`Mnemonic imported • ${hd.path}`);
  loadCoins({ force: true });
  loadTxHistory({ force: true });
  refreshBalance({ force: true });
  loadWalletStatus({ force: true });
};

const importPrivateKey = async (event) => {
  event.preventDefault();
  const rawKey = el("importPrivateKey").value.trim();
  const password = el("importKeyPassword").value.trim();
  if (!rawKey || !password) {
    setStatus(el("walletState"), "Private key + password required", "error");
    return;
  }
  const secretKey = hexToBytes(rawKey);
  if (secretKey.length !== 64) {
    setStatus(el("walletState"), "Invalid key length", "error");
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
  updateWalletUI();
  setStatus(el("walletState"), "Private key imported", "success");
  logActivity("Private key imported");
  loadCoins({ force: true });
  loadTxHistory({ force: true });
  refreshBalance({ force: true });
  loadWalletStatus({ force: true });
};

const unlockWallet = async (event) => {
  event.preventDefault();
  const password = el("unlockPassword").value.trim();
  if (!password) {
    setStatus(el("walletState"), "Password required", "error");
    return;
  }
  const wallet = state.wallet || loadWallet();
  if (!wallet) {
    setStatus(el("walletState"), "No wallet found", "error");
    return;
  }
  try {
    const secretKey = await decryptSecretKey(wallet.crypto, password);
    const normalizedWallet = await normalizeWalletFromSecretKey(wallet, secretKey);
    state.wallet = normalizedWallet;
    state.secretKey = secretKey;
    state.pendingNonces = {};
    el("unlockPassword").value = "";
    updateWalletUI();
    setStatus(el("walletState"), "Unlocked", "success");
    logActivity("Wallet unlocked");
    loadCoins({ force: true });
    loadTxHistory({ force: true });
    refreshBalance({ force: true });
    loadWalletStatus({ force: true });
    if (state.authMode) {
      await startAuthFlow();
    }
  } catch (err) {
    setStatus(el("walletState"), "Unlock failed", "error");
  }
};

const lockWallet = () => {
  if (state.bridgeApprovalActive) {
    logActivity("Bridge approval cancelled (wallet locked)");
    settleBridgeApproval(false);
  }
  state.secretKey = null;
  state.pendingNonces = {};
  setExportedPrivateKey("");
  updateWalletUI();
  setStatus(el("walletState"), "Locked", "info");
  logActivity("Wallet locked");
  loadCoins({ force: true });
  loadTxHistory({ force: true });
};

const setExportedPrivateKey = (value) => {
  const box = el("exportKeyBox");
  const copyBtn = el("copyPrivateKey");
  const privateKey = String(value || "").trim();
  if (box) {
    if ("value" in box) {
      box.value = privateKey || "Private key will appear here";
    } else {
      box.textContent = privateKey || "Private key will appear here";
    }
    box.dataset.empty = privateKey ? "0" : "1";
  }
  if (copyBtn) {
    copyBtn.disabled = !privateKey;
  }
};

const getExportedPrivateKey = () => {
  const box = el("exportKeyBox");
  if (!box) return "";
  const raw = "value" in box ? box.value : box.textContent;
  const value = String(raw || "").trim();
  return /^[0-9a-fA-F]{128}$/.test(value) ? value : "";
};

const exportPrivateKey = () => {
  if (!state.secretKey) {
    setStatus(el("walletState"), "Unlock wallet first", "error");
    return;
  }
  const privateKey = bytesToHex(state.secretKey);
  setExportedPrivateKey(privateKey);
  const box = el("exportKeyBox");
  if (box && typeof box.focus === "function") {
    box.focus();
    if (typeof box.select === "function") {
      box.select();
    }
  }
  setStatus(el("walletState"), "Private key visible", "success");
  logActivity("Private key exported");
};

const copyPrivateKey = async () => {
  const privateKey = getExportedPrivateKey();
  if (!privateKey) {
    setStatus(el("walletState"), "Export private key first", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(privateKey);
    setStatus(el("walletState"), "Private key copied", "success");
    logActivity("Private key copied");
  } catch (err) {
    const box = el("exportKeyBox");
    if (box && typeof box.focus === "function") {
      box.focus();
      if (typeof box.select === "function") {
        box.select();
      }
    }
    setStatus(el("walletState"), "Select and copy manually", "info");
  }
};

const copyAddress = async () => {
  const address = el("walletAddress").textContent.trim();
  if (!address || address === "—") {
    setStatus(el("walletState"), "No address", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(address);
    logActivity("Address copied");
  } catch (err) {
    setStatus(el("walletState"), "Copy failed", "error");
  }
};

const copyPublicKey = async () => {
  const publicKey = el("walletPublicKey").textContent.trim();
  if (!publicKey || publicKey === "—") {
    setStatus(el("walletState"), "No public key", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(publicKey);
    logActivity("Public key copied");
  } catch (err) {
    setStatus(el("walletState"), "Copy failed", "error");
  }
};

const copyEVMAddress = async () => {
  const evmAddressEl = el("walletEvmAddress");
  if (!evmAddressEl) return;
  let address = evmAddressEl.textContent.trim();
  if (!isHexAddress(address)) {
    address = await ensureWalletEVMAddress();
  }
  if (!isHexAddress(address)) {
    setStatus(el("walletState"), "No EVM address", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(address);
    logActivity("EVM address copied");
  } catch (err) {
    setStatus(el("walletState"), "Copy failed", "error");
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

const refreshBalance = async ({ quick = false, force = false } = {}) => {
  const address = el("balanceAddress").value.trim();
  const coin = normalizeCoinSymbolInput(el("balanceCoin").value) || "MSC";
  const statusEl = el("balanceStatus");

  if (!address) {
    if (!quick) {
      setStatus(statusEl, "Address required", "error");
    }
    return;
  }

  const intervalMs = quick ? QUICK_BALANCE_SYNC_MS : FULL_BALANCE_SYNC_MS;
  const lastAt = quick ? state.lastQuickBalanceSyncAt : state.lastFullBalanceSyncAt;
  if (!shouldRunInterval(lastAt, intervalMs, force)) {
    return;
  }
  if (!force && inRateLimitCooldown()) {
    if (!quick) {
      setStatus(statusEl, "Rate limited — waiting before next balance check", "info");
    }
    return;
  }

  const key = quick ? "refreshBalanceQuick" : "refreshBalanceFull";
  return runWithInFlight(key, async () => {
    try {
	      if (quick) {
	        const bal = await apiWithFallback(
	          `/balance?address=${encodeURIComponent(address)}&coin=${encodeURIComponent(coin)}&state=finalized`,
	        );
	        state.lastQuickBalanceSyncAt = Date.now();
	        el("balanceResult").innerHTML = `<div class="log-item">Finalized: ${bal.balance} ${bal.coin || coin}</div>`;
	        setStatus(statusEl, "Balance updated", "success");
	        return;
	      }

      const rpcTargets = state.rpcUrls.length ? state.rpcUrls : [state.rpcUrl];
	      const results = await Promise.allSettled(
	        rpcTargets.map(async (rpc) => {
	          const [bal, status] = await Promise.all([
	            api(`/balance?address=${encodeURIComponent(address)}&coin=${encodeURIComponent(coin)}&state=finalized`, {
	              baseUrl: rpc,
	            }),
	            api("/status", { baseUrl: rpc }),
	          ]);
          const height = Number(status.finalized_height || status.height || 0);
          return { rpc, balance: bal.balance, coin: bal.coin, height };
        }),
      );

      const rows = [];
      const counts = new Map();
      let successCount = 0;
      let maxHeight = 0;

      results.forEach((res) => {
        if (res.status === "fulfilled") {
          successCount += 1;
          maxHeight = Math.max(maxHeight, res.value.height || 0);
        }
      });

      results.forEach((res, index) => {
        if (res.status === "fulfilled") {
          const { rpc, balance, height } = res.value;
          const keyValue = String(balance);
          const eligible = height === maxHeight;
          counts.set(keyValue, (counts.get(keyValue) || 0) + (eligible ? 1 : 0));
          const lag = maxHeight > 0 ? ` (h=${height}${height < maxHeight ? `, -${maxHeight - height}` : ""})` : "";
          rows.push(`<div class="log-item">${rpc} ? ${balance} ${coin}${lag}</div>`);
        } else {
          const rpc = rpcTargets[index] || "node";
          rows.push(`<div class="log-item">${rpc} ? error</div>`);
        }
      });

      let consensus = null;
      let consensusCount = 0;
      counts.forEach((count, value) => {
        if (count > consensusCount) {
          consensusCount = count;
          consensus = value;
        }
      });

      const total = rpcTargets.length;
      const consensusLine =
        consensus !== null
          ? `Consensus@h=${maxHeight}: ${consensus} ${coin} (${consensusCount}/${total})`
          : "Consensus: unavailable";

      state.lastFullBalanceSyncAt = Date.now();
      state.lastQuickBalanceSyncAt = state.lastFullBalanceSyncAt;
      el("balanceResult").innerHTML = `<div class="log-item">${consensusLine}</div>${rows.join("")}`;
      setStatus(
        statusEl,
        successCount ? "Balance updated" : "Balance failed",
        successCount ? "success" : "error",
      );
      logActivity("Balance checked (multi-node)");
    } catch (err) {
      applyRateLimitCooldown(err);
      const message = await formatError(err);
      setStatus(statusEl, message, "error");
    }
  });
};

const fetchBalance = async (event) => {
  event.preventDefault();
  await refreshBalance({ force: true });
};

const requestFaucet = async (event) => {
  event.preventDefault();
  const address = el("faucetAddress").value.trim();
  const amount = parseInt(el("faucetAmount").value, 10);
  const coin = normalizeCoinSymbolInput(el("faucetCoin").value) || "MSC";
  if (!address || !amount) {
    setStatus(el("faucetStatus"), "Address + amount required", "error");
    return;
  }
  const cooldown = cooldownRemaining("faucet");
  if (cooldown > 0) {
    setStatus(el("faucetStatus"), `Wait ${Math.ceil(cooldown / 1000)}s`, "error");
    return;
  }
  try {
    const data = await apiWithFallback("/faucet", {
      method: "POST",
      body: { address, amount, coin },
    });
    setStatus(el("faucetStatus"), `Funded ${data.amount} ${data.coin}`, "success");
    logActivity(`Faucet: +${data.amount} ${data.coin}`);
    setCooldown("faucet", 10000);
    loadCoins({ force: true });
    loadTxHistory({ force: true });
    refreshBalance({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setStatus(el("faucetStatus"), message || "Faucet failed", "error");
  }
};

const submitTransaction = async (tx) => {
  try {
    return await apiWithFallback("/submitTx", { method: "POST", body: tx });
  } catch (err) {
    syncNonceFromError(err, tx.from);
    throw err;
  }
};

const getNextNonce = async () => {
  if (!state.wallet) {
    throw new Error("Wallet not loaded");
  }
  const address = state.wallet.address;
  try {
    const nonceData = await apiWithFallback(`/nonce/pending?address=${encodeURIComponent(address)}`);
    const nextNonce = Number(nonceData.nonce) || 1;
    state.pendingNonces[address] = nextNonce;
    return nextNonce;
  } catch (err) {
    if (state.pendingNonces[address]) {
      return state.pendingNonces[address];
    }
    throw err;
  }
};

const submitUserTx = async (buildTx) => {
  if (!state.wallet) {
    throw new Error("Wallet not loaded");
  }
  const address = state.wallet.address;
  const attempt = async (nonce) => {
    const tx = buildTx(nonce);
    const signed = await signTransaction(tx, state.chainId);
    const outgoing = {
      ...signed,
      ChainID: state.chainId,
      Coin: tx.coin || tx.Coin || "MSC",
      Type: tx.type ?? tx.Type,
    };
    await submitTransaction(outgoing);
    return { nonce, txId: signed.id };
  };

  const firstNonce = await getNextNonce();
  try {
    const sent = await attempt(firstNonce);
    bumpPendingNonce(address, sent.nonce + 1);
    return { nonce: sent.nonce, txId: sent.txId, retried: false };
  } catch (err) {
    const expected = syncNonceFromError(err, address);
    if (expected && expected !== firstNonce) {
      const sent = await attempt(expected);
      bumpPendingNonce(address, sent.nonce + 1);
      return { nonce: sent.nonce, txId: sent.txId, retried: true };
    }
    throw err;
  }
};

const providerAccounts = async (requireUnlock) => {
  if (!state.wallet) {
    if (requireUnlock) {
      throw new Error("MSC wallet not loaded");
    }
    return [];
  }
  if (!state.secretKey) {
    if (requireUnlock) {
      throw new Error("MSC wallet is locked");
    }
    return [];
  }
  const alias = await ensureWalletEVMAddress();
  return alias ? [alias] : [];
};

const confirmBridgeEVMTransaction = async ({ to, amount, gasLimit, fee, origin, kind }) => {
  // msc_bridge_tx_confirm:
  // - "0" => auto-approve bridge tx (no prompt)
  // - "1" => show in-app approval sheet (MetaMask-like popup)
  const mode = String(localStorage.getItem("msc_bridge_tx_confirm") || "1").trim();
  if (mode === "0") return;

  const details = {
    to: to || "",
    amount,
    gasLimit,
    fee,
    origin: origin || "",
    kind: kind || "send",
  };

  if (bridgeApprovalOverlay && bridgeApproveBtn && bridgeRejectBtn) {
    await enqueueBridgeApproval(details);
    return;
  }

  // If approval UI is unavailable, fail open for bridge compatibility.
  logActivity("Bridge approval UI unavailable, auto-approving request");
};

const confirmWalletTransaction = async ({ to, amount, coin, fee, kind }) => {
  // msc_wallet_tx_confirm:
  // - "0" => auto-approve local wallet tx (no prompt)
  // - "1" => show in-app approval sheet (same as wallet connect)
  const mode = String(localStorage.getItem("msc_wallet_tx_confirm") || "1").trim();
  if (mode === "0") return;

  const normalizedCoin = normalizeCoinSymbolKey(coin) || "MSC";
  const amountText = `${String(amount)} ${normalizedCoin}`;
  const details = {
    to: to || "",
    amount: Number.isFinite(Number(amount)) ? Number(amount) : 0,
    amountLabel: amountText,
    gasLimit: 0,
    fee: Number.isFinite(Number(fee)) ? Number(fee) : 0,
    feeLabel: `${Number.isFinite(Number(fee)) ? Number(fee) : 0} MSC`,
    origin: "MSC Wallet",
    kind: kind || "send",
  };

  if (bridgeApprovalOverlay && bridgeApproveBtn && bridgeRejectBtn) {
    await enqueueBridgeApproval(details);
    return;
  }

  logActivity("Approval UI unavailable, auto-approving wallet transaction");
};

const confirmBridgeWalletConnect = async ({ origin }) => {
  // msc_bridge_connect_confirm:
  // - "0" => auto-approve wallet connect
  // - "1" => show in-app approval sheet
  const mode = String(localStorage.getItem("msc_bridge_connect_confirm") || "1").trim();
  if (mode === "0") return;

  const details = {
    to: "Account access",
    amount: 0,
    gasLimit: 0,
    fee: 0,
    origin: origin || "",
    kind: "connect",
  };

  if (bridgeApprovalOverlay && bridgeApproveBtn && bridgeRejectBtn) {
    await enqueueBridgeApproval(details);
    return;
  }

  logActivity("Bridge connect approval UI unavailable, auto-approving request");
};

const defaultEVMGasLimit = (hasData) => (hasData ? 3_000_000 : 21_000);

const sendEVMTransactionViaWallet = async (txObject) => {
  if (!txObject || typeof txObject !== "object") {
    throw new Error("invalid transaction object");
  }
  if (!state.wallet || !state.secretKey) {
    throw new Error("unlock MSC wallet first");
  }

  const walletAlias = await ensureWalletEVMAddress();
  if (!walletAlias) {
    throw new Error("failed to derive wallet EVM address");
  }

  const fromInput = String(txObject.from || "").trim();
  if (fromInput) {
    const fromAlias = await resolveEVMRecipientAddress(fromInput);
    if (!fromAlias || fromAlias !== walletAlias) {
      throw new Error("from address does not match unlocked MSC wallet");
    }
  }

  const to = await resolveEVMRecipientAddress(txObject.to);
  const data = normalizeHexData(txObject.input || txObject.data || "0x");
  const requestOrigin = String(txObject.__bridgeOrigin || txObject.origin || "").trim();
  const valueWei = parseRPCQuantityBigInt(txObject.value || "0x0", "value");
  const amount = weiToWholeMSCAmount(valueWei);

  const gasField = txObject.gas ?? txObject.gasLimit;
  const hasData = data !== "0x";
  const gasBig = gasField === undefined
    ? BigInt(defaultEVMGasLimit(hasData))
    : parseRPCQuantityBigInt(gasField, "gas");
  if (gasBig <= 0n) {
    throw new Error("gas must be greater than zero");
  }
  if (gasBig > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error("gas too large");
  }
  const gasLimit = Number(gasBig);

  if (!to && data === "0x") {
    throw new Error("contract deployment requires bytecode in data");
  }

  let kind = "send";
  if (!to && data !== "0x") kind = "deploy";
  else if (to && data !== "0x") kind = "call";

  const fee = Math.max(1, Math.floor(gasLimit / 1000));
  await confirmBridgeEVMTransaction({ to, amount, gasLimit, fee, origin: requestOrigin, kind });
  const { txId, retried } = await submitUserTx((nonce) => ({
    from: state.wallet.address,
    to: to || "",
    amount,
    nonce,
    publicKey: state.wallet.publicKey,
    signature: "",
    fee,
    expiry: Math.floor(Date.now() / 1000) + 120,
    type: 7,
    coin: "MSC",
    evm_gas_limit: gasLimit,
    evm_code: to ? "0x00" : data,
    evm_input: to ? data : "",
  }));

  const outHash = normalizeHexHash(txId);
  if (!outHash || outHash === "0x") {
    throw new Error("transaction submitted but hash unavailable");
  }

  const retryMsg = retried ? " (nonce synced)" : "";
  setStatus(el("sendStatus"), `EVM tx submitted${retryMsg}`, "success");
  logActivity(`EVM tx sent ${outHash.slice(0, 10)}...`);
  loadTxHistory({ force: true });
  refreshBalance({ force: true });
  loadWalletStatus({ force: true });
  return outHash;
};

const normalizeBridgeOrigin = (value) => {
  const raw = String(value || "").trim();
  if (!raw) return "";
  if (raw === "null") return "null";
  try {
    return new URL(raw).origin.toLowerCase();
  } catch (err) {
    return "";
  }
};

const isLoopbackOrigin = (origin) =>
  /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?$/i.test(String(origin || ""));

const extraBridgeOrigins = () => {
  const raw = localStorage.getItem(MSC_PROVIDER_BRIDGE_EXTRA_ORIGINS_KEY);
  if (!raw) return new Set();
  const out = new Set();
  const parts = raw
    .split(/[,\s]+/)
    .map((item) => normalizeBridgeOrigin(item))
    .filter(Boolean);
  parts.forEach((item) => out.add(item));
  return out;
};

const isAllowedBridgeOrigin = (origin) => {
  const normalized = normalizeBridgeOrigin(origin);
  if (!normalized) return false;
  if (localStorage.getItem(MSC_PROVIDER_BRIDGE_ALLOW_ALL_KEY) === "1") return true;
  if (normalized === normalizeBridgeOrigin(window.location.origin)) return true;
  if (isLoopbackOrigin(normalized)) return true;
  return extraBridgeOrigins().has(normalized);
};

const compactBridgeClients = () => {
  mscBridgeClients = mscBridgeClients.filter((client) => {
    if (!client || !client.source) return false;
    if ("closed" in client.source && client.source.closed) return false;
    return true;
  });
};

const upsertBridgeClient = (source, origin) => {
  if (!source || typeof source.postMessage !== "function") return;
  const normalized = normalizeBridgeOrigin(origin);
  if (!normalized) return;
  compactBridgeClients();
  const existing = mscBridgeClients.find(
    (client) => client.source === source && client.origin === normalized
  );
  if (!existing) {
    mscBridgeClients.push({ source, origin: normalized });
  }
};

const sendBridgeResponse = (source, origin, payload) => {
  if (!source || typeof source.postMessage !== "function") return;
  try {
    source.postMessage(
      {
        namespace: MSC_PROVIDER_BRIDGE_NAMESPACE,
        ...payload,
      },
      origin
    );
  } catch (err) {
    // Best-effort bridge response.
  }
};

const broadcastBridgeEvent = (eventName, payload) => {
  compactBridgeClients();
  mscBridgeClients.forEach((client) => {
    sendBridgeResponse(client.source, client.origin, {
      type: "event",
      event: eventName,
      payload,
      ts: Date.now(),
    });
  });
};

const installMSCProviderWindowBridge = () => {
  if (typeof window === "undefined") return;
  if (window.__mscBridgeInstalled) return;
  window.__mscBridgeInstalled = true;

  window.addEventListener("message", async (event) => {
    const data = event?.data;
    if (!data || typeof data !== "object") return;
    if (data.namespace !== MSC_PROVIDER_BRIDGE_NAMESPACE || data.type !== "request") return;

    const source = event.source;
    const origin = normalizeBridgeOrigin(event.origin);
    const requestID = data.id === undefined || data.id === null ? null : String(data.id);
    const rejectUnauthorized = () => {
      sendBridgeResponse(source, event.origin, {
        type: "response",
        id: requestID,
        error: {
          code: 4100,
          message: `origin not allowed: ${event.origin || "unknown"}`,
        },
      });
    };

    if (!isAllowedBridgeOrigin(origin)) {
      rejectUnauthorized();
      return;
    }
    upsertBridgeClient(source, origin);

    const methodRaw = String(data.method || "").trim();
    const method = methodRaw.toLowerCase();
    const params = Array.isArray(data.params) ? data.params : [];
    if (!methodRaw) {
      sendBridgeResponse(source, event.origin, {
        type: "response",
        id: requestID,
        error: { code: -32600, message: "missing request method" },
      });
      return;
    }
    if (method === "msc_sendtransaction") {
      logActivity(`Bridge tx request from ${origin || "unknown"}`);
    }

    let bridgeParams = params;
    if (method === "msc_sendtransaction") {
      bridgeParams = [
        {
          ...(params[0] && typeof params[0] === "object" ? params[0] : {}),
          __bridgeOrigin: origin || "",
        },
      ];
    } else if (method === "msc_requestaccounts") {
      bridgeParams = [
        {
          ...(params[0] && typeof params[0] === "object" ? params[0] : {}),
          __bridgeOrigin: origin || "",
        },
      ];
    }

    if (method === "msc_bridge_ping") {
      sendBridgeResponse(source, event.origin, {
        type: "response",
        id: requestID,
        result: {
          ok: true,
          chainId: chainIdHex(),
          walletLoaded: !!state.wallet,
          unlocked: !!state.secretKey,
        },
      });
      return;
    }
    if (method === "msc_accounts") {
      try {
        const accounts = await providerAccounts(false);
        if (mscInjectedProvider) {
          mscInjectedProvider.selectedAddress = accounts[0] || null;
        }
        sendBridgeResponse(source, event.origin, {
          type: "response",
          id: requestID,
          result: accounts,
        });
      } catch (err) {
        sendBridgeResponse(source, event.origin, {
          type: "response",
          id: requestID,
          error: {
            code: -32000,
            message: String(err?.message || "request failed"),
          },
        });
      }
      return;
    }
    if (method === "msc_requestaccounts" || method === "msc_request_accounts") {
      try {
        const meta = bridgeParams[0] && typeof bridgeParams[0] === "object" ? bridgeParams[0] : {};
        const requestOrigin = String(meta.__bridgeOrigin || "").trim();
        if (requestOrigin) {
          await confirmBridgeWalletConnect({ origin: requestOrigin });
        }
        const accounts = await providerAccounts(true);
        if (mscInjectedProvider) {
          mscInjectedProvider.selectedAddress = accounts[0] || null;
          if (typeof mscInjectedProvider._syncState === "function") {
            await mscInjectedProvider._syncState({ emitAccounts: true, emitChain: false });
          }
        }
        sendBridgeResponse(source, event.origin, {
          type: "response",
          id: requestID,
          result: accounts,
        });
      } catch (err) {
        sendBridgeResponse(source, event.origin, {
          type: "response",
          id: requestID,
          error: {
            code: -32000,
            message: String(err?.message || "request failed"),
          },
        });
      }
      return;
    }

    try {
      if (!mscInjectedProvider || typeof mscInjectedProvider.request !== "function") {
        throw new Error("MSC wallet provider unavailable");
      }
      const result = await mscInjectedProvider.request({ method: methodRaw, params: bridgeParams });
      sendBridgeResponse(source, event.origin, {
        type: "response",
        id: requestID,
        result,
      });
    } catch (err) {
      if (method === "msc_sendtransaction") {
        logActivity(`Bridge tx failed: ${String(err?.message || "request failed")}`);
      }
      sendBridgeResponse(source, event.origin, {
        type: "response",
        id: requestID,
        error: {
          code: -32000,
          message: String(err?.message || "request failed"),
        },
      });
    }
  });
};

const createMSCInjectedProvider = () => {
  const listeners = new Map();

  const emit = (event, payload) => {
    const handlers = listeners.get(event);
    if (!handlers || !handlers.size) return;
    handlers.forEach((handler) => {
      try {
        handler(payload);
      } catch (err) {
        // Ignore consumer handler errors.
      }
    });
  };

  const on = (event, handler) => {
    if (!event || typeof handler !== "function") return provider;
    const key = String(event);
    if (!listeners.has(key)) listeners.set(key, new Set());
    listeners.get(key).add(handler);
    return provider;
  };

  const removeListener = (event, handler) => {
    const key = String(event || "");
    const handlers = listeners.get(key);
    if (!handlers) return provider;
    handlers.delete(handler);
    if (!handlers.size) listeners.delete(key);
    return provider;
  };

  const normalizeParams = (params) => (Array.isArray(params) ? params : []);

  const syncState = async ({ emitAccounts = false, emitChain = false } = {}) => {
    const accounts = await providerAccounts(false);
    provider.selectedAddress = accounts[0] || null;
    provider.isConnected = () => true;

    if (emitAccounts) {
      const changed =
        accounts.length !== mscProviderLastAccounts.length ||
        accounts.some((v, i) => v !== mscProviderLastAccounts[i]);
      if (changed) {
        mscProviderLastAccounts = accounts.slice();
        emit("accountsChanged", accounts);
      }
    } else {
      mscProviderLastAccounts = accounts.slice();
    }

    const currentChainHex = chainIdHex();
    if (emitChain && currentChainHex !== mscProviderLastChainIdHex) {
      mscProviderLastChainIdHex = currentChainHex;
      emit("chainChanged", currentChainHex);
    } else if (!mscProviderLastChainIdHex) {
      mscProviderLastChainIdHex = currentChainHex;
    }
  };

  const request = async (args) => {
    const methodRaw = String(args?.method || "").trim();
    const method = methodRaw.toLowerCase();
    const params = normalizeParams(args?.params);
    if (!method) {
      throw new Error("missing request method");
    }

    switch (method) {
      case "msc_chainid":
        return chainIdHex();
      case "net_version":
        return MSC_ONLY_CHAIN_ID;
      case "wallet_getPermissions":
        return [{ parentCapability: "msc_accounts" }];
      case "wallet_requestPermissions":
        await providerAccounts(true);
        return [{ parentCapability: "msc_accounts" }];
      case "msc_accounts": {
        const accounts = await providerAccounts(false);
        provider.selectedAddress = accounts[0] || null;
        return accounts;
      }
      case "msc_requestaccounts": {
        const meta = params[0] && typeof params[0] === "object" ? params[0] : {};
        const requestOrigin = String(meta.__bridgeOrigin || "").trim();
        if (requestOrigin) {
          await confirmBridgeWalletConnect({ origin: requestOrigin });
        }
        const accounts = await providerAccounts(true);
        provider.selectedAddress = accounts[0] || null;
        await syncState({ emitAccounts: true, emitChain: false });
        return accounts;
      }
      case "msc_request_accounts": {
        const meta = params[0] && typeof params[0] === "object" ? params[0] : {};
        const requestOrigin = String(meta.__bridgeOrigin || "").trim();
        if (requestOrigin) {
          await confirmBridgeWalletConnect({ origin: requestOrigin });
        }
        const accounts = await providerAccounts(true);
        provider.selectedAddress = accounts[0] || null;
        await syncState({ emitAccounts: true, emitChain: false });
        return accounts;
      }
      case "msc_coinbase": {
        const accounts = await providerAccounts(false);
        return accounts[0] || null;
      }
      case "wallet_switchethereumchain": {
        const cfg = params[0];
        if (!cfg || typeof cfg !== "object") {
          throw new Error("invalid switchEthereumChain params");
        }
        const nextChain = parseRPCQuantityBigInt(cfg.chainId, "chainId");
        if (nextChain !== BigInt(MSC_ONLY_CHAIN_ID_DEC)) {
          throw new Error(`Only ${MSC_COIN_FULL_NAME} chain (${MSC_ONLY_CHAIN_ID_DEC}) supported`);
        }
        enforceMSCChainID();
        await connectToRPC({ persist: true });
        await syncState({ emitAccounts: false, emitChain: true });
        return null;
      }
      case "wallet_addethereumchain": {
        const cfg = params[0];
        if (!cfg || typeof cfg !== "object") {
          throw new Error("invalid addEthereumChain params");
        }
        if (cfg.chainId) {
          const nextChain = parseRPCQuantityBigInt(cfg.chainId, "chainId");
          if (nextChain !== BigInt(MSC_ONLY_CHAIN_ID_DEC)) {
            throw new Error(`Only ${MSC_COIN_FULL_NAME} chain (${MSC_ONLY_CHAIN_ID_DEC}) supported`);
          }
        }
        if (Array.isArray(cfg.rpcUrls) && cfg.rpcUrls.length) {
          const rpcInput = el("rpcUrl");
          if (rpcInput) {
            rpcInput.value = cfg.rpcUrls.join(", ");
          }
        }
        enforceMSCChainID();
        await connectToRPC({ persist: true });
        await syncState({ emitAccounts: false, emitChain: true });
        return null;
      }
      case "msc_sendtransaction": {
        const txObject = params[0];
        return sendEVMTransactionViaWallet(txObject);
      }
      case "msc_sign":
      case "personal_sign":
      case "msc_signtypeddata":
      case "msc_signtypeddata_v4":
        throw new Error(`${methodRaw} is not supported by MSC Wallet`);
      default:
        return rpcRequest(methodRaw, params);
    }
  };

  const provider = {
    isMSCWallet: true,
    isMetaMask: false,
    selectedAddress: null,
    request,
    on,
    addListener: on,
    once: (event, handler) => {
      if (!event || typeof handler !== "function") return provider;
      const wrapped = (payload) => {
        try {
          handler(payload);
        } finally {
          removeListener(event, wrapped);
        }
      };
      return on(event, wrapped);
    },
    removeListener,
    off: removeListener,
    removeAllListeners: (event) => {
      if (event === undefined || event === null) {
        listeners.clear();
        return provider;
      }
      listeners.delete(String(event));
      return provider;
    },
    enable: async () => request({ method: "msc_requestAccounts", params: [] }),
    send: (payloadOrMethod, paramsOrCallback) => {
      if (typeof payloadOrMethod === "string") {
        return request({ method: payloadOrMethod, params: normalizeParams(paramsOrCallback) });
      }
      const payload = payloadOrMethod || {};
      const callback = typeof paramsOrCallback === "function" ? paramsOrCallback : null;
      const promise = request({ method: payload.method, params: normalizeParams(payload.params) })
        .then((result) => ({
          jsonrpc: "2.0",
          id: payload.id ?? null,
          result,
        }))
        .catch((error) => ({
          jsonrpc: "2.0",
          id: payload.id ?? null,
          error: { code: -32000, message: String(error?.message || "request failed") },
        }));
      if (callback) {
        promise.then((res) => callback(null, res));
        return undefined;
      }
      return promise;
    },
    sendAsync: (payload, callback) => {
      const cb = typeof callback === "function" ? callback : () => {};
      provider.send(payload, cb);
    },
    _syncState: syncState,
    _emit: emit,
  };

  return provider;
};

const announceMSCProviderEIP6963 = (provider) => {
  if (typeof window === "undefined" || typeof CustomEvent === "undefined") return;
  try {
    window.dispatchEvent(
      new CustomEvent("eip6963:announceProvider", {
        detail: {
          info: {
            uuid: "msc-wallet-provider",
            name: "MSC Wallet",
            icon: "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Ccircle cx='32' cy='32' r='30' fill='%230b5fff'/%3E%3Ctext x='32' y='40' text-anchor='middle' font-size='24' fill='white' font-family='Arial'%3EM%3C/text%3E%3C/svg%3E",
            rdns: "msc.wallet.local",
          },
          provider,
        },
      })
    );
  } catch (err) {
    // Best-effort event announce.
  }
};

const installMSCInjectedProvider = () => {
  if (mscInjectedProvider) return mscInjectedProvider;
  mscInjectedProvider = createMSCInjectedProvider();
  window.mscEthereum = mscInjectedProvider;

  const params = new URLSearchParams(window.location.search);
  const forceInject = params.get("mscInjectEthereum") === "1" || localStorage.getItem("msc_force_ethereum") === "1";
  if (!window.ethereum || forceInject) {
    window.ethereum = mscInjectedProvider;
    window.dispatchEvent(new Event("ethereum#initialized"));
  }
  if (window.ethereum === mscInjectedProvider) {
    if (!Array.isArray(window.ethereum.providers)) {
      window.ethereum.providers = [mscInjectedProvider];
    } else if (!window.ethereum.providers.includes(mscInjectedProvider)) {
      window.ethereum.providers.push(mscInjectedProvider);
    }
  }
  if (!window.__mscEip6963Handler) {
    window.__mscEip6963Handler = () => announceMSCProviderEIP6963(mscInjectedProvider);
    window.addEventListener("eip6963:requestProvider", window.__mscEip6963Handler);
  }
  announceMSCProviderEIP6963(mscInjectedProvider);
  installMSCProviderWindowBridge();
  if (!window.__mscBridgeProviderEventsAttached && typeof mscInjectedProvider.on === "function") {
    window.__mscBridgeProviderEventsAttached = true;
    mscInjectedProvider.on("accountsChanged", (accounts) =>
      broadcastBridgeEvent("accountsChanged", Array.isArray(accounts) ? accounts : [])
    );
    mscInjectedProvider.on("chainChanged", (nextChain) =>
      broadcastBridgeEvent("chainChanged", String(nextChain || chainIdHex()))
    );
  }
  mscInjectedProvider
    ._syncState({ emitAccounts: false, emitChain: false })
    .catch(() => {});
  return mscInjectedProvider;
};

const syncInjectedProviderState = ({ emitAccounts = true, emitChain = true } = {}) => {
  if (!mscInjectedProvider || typeof mscInjectedProvider._syncState !== "function") {
    return Promise.resolve();
  }
  return mscInjectedProvider._syncState({ emitAccounts, emitChain }).catch(() => {});
};

const submitPoolTransfer = async (event) => {
  event.preventDefault();
  const from = poolFromSelect ? poolFromSelect.value.trim() : "";
  const to = poolToInput ? poolToInput.value.trim() : "";
  const amount = poolAmountInput ? parseInt(poolAmountInput.value, 10) : 0;
  const coinRaw = poolCoinInput ? poolCoinInput.value : "MSC";
  const coin = normalizeCoinSymbolInput(coinRaw) || "MSC";
  const note = poolNoteInput ? poolNoteInput.value.trim() : "";

  if (!from || !to || !amount) {
    setStatus(statusEls.poolTransfer, "From, to, amount required", "error");
    return;
  }

  try {
    await apiWithFallback("/pool/transfer", {
      method: "POST",
      body: { from, to, amount, coin, note },
    });
    setStatus(statusEls.poolTransfer, "Transfer complete", "success");
    logActivity(`Pool ${shortAddress(from)} ? ${shortAddress(to)} (${amount} ${coin})`);
    loadTokenomics();
    loadCoins({ force: true });
    loadTxHistory({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setStatus(statusEls.poolTransfer, message || "Transfer failed", "error");
  }
};

const sendTransfer = async (event) => {
  event.preventDefault();
  if (!state.wallet || !state.secretKey) {
    setStatus(el("sendStatus"), "Unlock wallet first", "error");
    return;
  }
  if (state.sending) {
    return;
  }
  const to = el("sendTo").value.trim();
  const amount = parseInt(el("sendAmount").value, 10);
  const coin = normalizeCoinSymbolInput(el("sendCoin").value) || "MSC";
  if (!to || !amount) {
    setStatus(el("sendStatus"), "Recipient + amount required", "error");
    return;
  }

  state.sending = true;
  const sendBtn = el("sendForm").querySelector("button[type='submit']");
  if (sendBtn) sendBtn.disabled = true;
  try {
    const fee = computeTxFee(amount);
    const dtlToken = await resolveDTLTokenBySymbol(coin);
    await confirmWalletTransaction({ to, amount, coin, fee, kind: "send" });
    let retried = false;
    if (dtlToken && dtlToken.token_id) {
      const dtlPayload = JSON.stringify({
        from: state.wallet.address,
        to,
        token_id: dtlToken.token_id,
        amount,
      });
      const sent = await submitUserTx((nonce) => ({
        from: state.wallet.address,
        to,
        amount,
        nonce,
        publicKey: state.wallet.publicKey,
        signature: "",
        fee,
        expiry: Math.floor(Date.now() / 1000) + 120,
        type: 8,
        coin: "MSC",
        dtl_tx_type: "TOKEN_TRANSFER",
        dtl_token_id: dtlToken.token_id,
        dtl_payload: dtlPayload,
      }));
      retried = sent.retried;
    } else {
      const sent = await submitUserTx((nonce) => ({
        from: state.wallet.address,
        to,
        amount,
        nonce,
        publicKey: state.wallet.publicKey,
        signature: "",
        fee,
        expiry: Math.floor(Date.now() / 1000) + 120,
        type: 0,
        coin,
      }));
      retried = sent.retried;
    }
    const suffix = retried ? " (nonce synced)" : "";
    setStatus(el("sendStatus"), `Transaction submitted${suffix}`, "success");
    logActivity(`Sent ${amount} ${normalizeCoinSymbolKey(coin) || coin}`);
    loadTxHistory({ force: true });
    refreshBalance({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setStatus(el("sendStatus"), message || "Send failed", "error");
  } finally {
    state.sending = false;
    if (sendBtn) sendBtn.disabled = false;
  }
};

const sendStake = async (event) => {
  event.preventDefault();
  if (!state.wallet || !state.secretKey) {
    setStatus(el("stakeStatus"), "Unlock wallet first", "error");
    return;
  }
  if (state.staking) {
    return;
  }
  const validatorId = el("stakeValidator").value.trim();
  const amount = parseInt(el("stakeAmount").value, 10);
  const coin = normalizeCoinSymbolInput(el("stakeCoin").value) || "MSC";
  if (!validatorId || !amount) {
    setStatus(el("stakeStatus"), "Validator + amount required", "error");
    return;
  }
  const boundValidator = state.walletStatus?.validator_id;
  if (boundValidator && boundValidator !== validatorId) {
    setStatus(
      el("stakeStatus"),
      `Wallet already bound to validator ${boundValidator}`,
      "error",
    );
    return;
  }
  let validatorPubKey = "";
  try {
    validatorPubKey = normalizeValidatorPubKeyHex(stakeValidatorPubKeyInput?.value || "");
  } catch (err) {
    setStakeValidatorPubKeyMessage(
      "Validator pubkey: invalid",
      "error",
      err.message || "Validator consensus pubkey must be 32-byte hex",
    );
    setStatus(el("stakeStatus"), err.message || "Invalid validator consensus pubkey", "error");
    focusStakeValidatorPubKeyField();
    return;
  }
  if (
    validatorPubKey &&
    state.wallet.publicKey &&
    validatorPubKey.toLowerCase() === String(state.wallet.publicKey).trim().toLowerCase()
  ) {
    setStakeValidatorPubKeyMessage(
      "Validator pubkey: wallet key",
      "error",
      "Use the validator node consensus pubkey, not this wallet public key.",
    );
    setStatus(
      el("stakeStatus"),
      "Validator consensus pubkey cannot be the wallet public key",
      "error",
    );
    focusStakeValidatorPubKeyField();
    return;
  }

  state.staking = true;
  const stakeBtn = el("stakeForm").querySelector("button[type='submit']");
  if (stakeBtn) stakeBtn.disabled = true;
  try {
    const fee = computeTxFee(amount);
    await confirmWalletTransaction({ to: validatorId, amount, coin, fee, kind: "stake" });
    const lockEpochsInput = el("stakeLockEpochs");
    let lockEpochs = parseInt(lockEpochsInput?.value, 10);
    if (!lockEpochs || lockEpochs <= 0) {
      lockEpochs = DEFAULT_STAKE_EPOCHS;
    }
    const { retried } = await submitUserTx((nonce) => ({
      from: state.wallet.address,
      to: validatorId,
      amount,
      nonce,
      publicKey: state.wallet.publicKey,
      signature: "",
      fee,
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 2,
      stake_epochs: lockEpochs,
      coin,
      ...(validatorPubKey ? { validator_pubkey: validatorPubKey } : {}),
    }));
    const suffix = retried ? " (nonce synced)" : "";
    setStatus(el("stakeStatus"), `Stake submitted${suffix}`, "success");
    updateStakeValidatorPubKeyUI();
    logActivity(`Staked ${amount} ${coin} to ${validatorId}`);
    loadTxHistory({ force: true });
    loadValidators({ force: true });
    refreshBalance({ force: true });
    loadWalletStatus({ force: true });
  } catch (err) {
    const message = await formatError(err);
    if (/validator_pubkey required for first non-core stake/i.test(message || "")) {
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: required",
        "error",
        "Paste the validator consensus pubkey for this first non-core stake, then submit again.",
      );
      setStatus(
        el("stakeStatus"),
        "Validator consensus pubkey required for first non-core stake",
        "error",
      );
      focusStakeValidatorPubKeyField();
    } else if (/validator_pubkey conflicts with anchored consensus pubkey/i.test(message || "")) {
      setStakeValidatorPubKeyMessage(
        "Validator pubkey: mismatch",
        "error",
        "The entered pubkey does not match the anchored consensus pubkey for this validator.",
      );
      setStatus(
        el("stakeStatus"),
        "Validator consensus pubkey does not match the anchored validator identity",
        "error",
      );
      focusStakeValidatorPubKeyField();
    } else {
      setStatus(el("stakeStatus"), message || "Stake failed", "error");
      updateStakeValidatorPubKeyUI();
    }
  } finally {
    state.staking = false;
    if (stakeBtn) stakeBtn.disabled = false;
  }
};

const sendUnstake = async (event) => {
  event.preventDefault();
  if (!state.wallet || !state.secretKey) {
    setStatus(el("unstakeStatus"), "Unlock wallet first", "error");
    return;
  }
  if (state.unstaking) {
    return;
  }
  const validatorId = el("unstakeValidator").value.trim();
  const amount = parseInt(el("unstakeAmount").value, 10);
  const coin = normalizeCoinSymbolInput(el("unstakeCoin").value) || "MSC";
  if (!validatorId || !amount) {
    setStatus(el("unstakeStatus"), "Validator + amount required", "error");
    return;
  }
  const boundValidator = state.walletStatus?.validator_id;
  if (boundValidator && boundValidator !== validatorId) {
    setStatus(
      el("unstakeStatus"),
      `Wallet bound to validator ${boundValidator}`,
      "error",
    );
    return;
  }

  state.unstaking = true;
  const unstakeBtn = el("unstakeForm").querySelector("button[type='submit']");
  if (unstakeBtn) unstakeBtn.disabled = true;
  try {
    const fee = computeTxFee(amount);
    await confirmWalletTransaction({ to: validatorId, amount, coin, fee, kind: "unstake" });
    const { retried } = await submitUserTx((nonce) => ({
      from: state.wallet.address,
      to: validatorId,
      amount,
      nonce,
      publicKey: state.wallet.publicKey,
      signature: "",
      fee,
      expiry: Math.floor(Date.now() / 1000) + 120,
      type: 6,
      coin,
    }));
    const suffix = retried ? " (nonce synced)" : "";
    setStatus(el("unstakeStatus"), `Unstake submitted${suffix}`, "success");
    logActivity(`Unstaked ${amount} ${coin} from ${validatorId}`);
    loadTxHistory({ force: true });
    loadValidators({ force: true });
    refreshBalance({ force: true });
    loadWalletStatus({ force: true });
  } catch (err) {
    const message = await formatError(err);
    setStatus(el("unstakeStatus"), message || "Unstake failed", "error");
  } finally {
    state.unstaking = false;
    if (unstakeBtn) unstakeBtn.disabled = false;
  }
};

const switchTab = (event) => {
  const target = event.target.closest(".tab");
  if (!target) return;
  const tabId = target.dataset.tab;
  document.querySelectorAll(".tab").forEach((tab) => tab.classList.remove("active"));
  target.classList.add("active");
  document.querySelectorAll(".tab-panel").forEach((panel) => panel.classList.remove("active"));
  const panel = document.getElementById(`tab-${tabId}`);
  if (panel) panel.classList.add("active");
};

const handlePoolAction = (event) => {
  const actionButton = event.target.closest("button[data-action]");
  if (!actionButton) return;
  const row = actionButton.closest(".pool-row");
  if (!row) return;
  const address = row.dataset.address;
  if (!address) return;

  if (actionButton.dataset.action === "from" && poolFromSelect) {
    poolFromSelect.value = address;
    if (poolToInput && state.wallet) {
      poolToInput.value = state.wallet.address;
    }
    logActivity(`Pool selected: ${shortAddress(address)}`);
    return;
  }

  if (actionButton.dataset.action === "send") {
    el("sendTo").value = address;
    logActivity(`Send to pool: ${shortAddress(address)}`);
  }
};

const handleBridgeApprove = () => {
  if (bridgeApprovalStatus) setStatus(bridgeApprovalStatus, "Approved", "success");
  logActivity("Bridge approval accepted");
  settleBridgeApproval(true);
};

const handleBridgeReject = () => {
  if (bridgeApprovalStatus) setStatus(bridgeApprovalStatus, "Rejected", "error");
  logActivity("Bridge approval rejected");
  settleBridgeApproval(false);
};

const init = () => {
  const savedRPCs = savedRPCListForCurrentPage();
  el("rpcUrl").value = preferHttpsForLocalRpc(savedRPCs.length ? savedRPCs.join(", ") : state.rpcUrl);
  enforceMSCChainID();
  state.apiToken = normalizeAuthToken(state.apiToken);
  el("apiToken").value = state.apiToken;
  applyAdminMode(state.adminMode);
  initAuthParams();
  if (autoSyncSelect) {
    autoSyncSelect.value = state.autoSync ? "on" : "off";
  }
  if (autoSyncMsInput) {
    state.autoSyncMs = normalizeAutoSyncMs(state.autoSyncMs);
    autoSyncMsInput.value = String(state.autoSyncMs);
  }
  if (broadcastSelect) {
    broadcastSelect.value = state.broadcastMode || "auto";
  }

  state.wallet = loadWallet();
  installMSCInjectedProvider();
  updateWalletUI();
  setActiveNFTTab(state.nftTab);
  updateFeeLabels();
  if (state.wallet) {
    refreshBalance({ force: true });
  }

  el("saveConnection").addEventListener("click", () => connectToRPC({ persist: true }));
  if (toggleAdminSettingsBtn) {
    toggleAdminSettingsBtn.addEventListener("click", () => {
      state.adminMode = !state.adminMode;
      applyAdminMode(state.adminMode);
    });
  }
  el("createWalletForm").addEventListener("submit", createWallet);
  el("importMnemonicForm").addEventListener("submit", importMnemonic);
  el("importKeyForm").addEventListener("submit", importPrivateKey);
  el("unlockForm").addEventListener("submit", unlockWallet);
  el("lockWallet").addEventListener("click", lockWallet);
  el("exportKey").addEventListener("click", exportPrivateKey);
  const copyPrivateKeyBtn = el("copyPrivateKey");
  if (copyPrivateKeyBtn) {
    copyPrivateKeyBtn.addEventListener("click", copyPrivateKey);
  }
  el("copyAddress").addEventListener("click", copyAddress);
  const copyPublicKeyBtn = el("copyPublicKey");
  if (copyPublicKeyBtn) {
    copyPublicKeyBtn.addEventListener("click", copyPublicKey);
  }
  const showWalletQrBtn = el("showWalletQr");
  if (showWalletQrBtn) {
    showWalletQrBtn.addEventListener("click", () => {
      openQrModal("Wallet QR", buildWalletQrPayload());
    });
  }
  const showPayQrBtn = el("showPayQr");
  if (showPayQrBtn) {
    showPayQrBtn.addEventListener("click", () => {
      openQrModal("Payment QR", buildPayQrPayload());
    });
  }
  const qrCloseBtn = el("qrClose");
  if (qrCloseBtn) {
    qrCloseBtn.addEventListener("click", closeQrModal);
  }
  const qrCopyBtn = el("qrCopy");
  if (qrCopyBtn) {
    qrCopyBtn.addEventListener("click", copyQrPayload);
  }
  const qrDownloadBtn = el("qrDownload");
  if (qrDownloadBtn) {
    qrDownloadBtn.addEventListener("click", () => {
      const canvasWrap = el("qrCanvas");
      const canvas = canvasWrap ? canvasWrap.querySelector("canvas") : null;
      if (canvas) {
        const link = document.createElement("a");
        link.href = canvas.toDataURL("image/png");
        link.download = "msc_wallet_qr.png";
        link.click();
        return;
      }
      const img = el("qrImage");
      if (!img || !img.src) return;
      const link = document.createElement("a");
      link.href = img.src;
      link.download = "msc_wallet_qr.png";
      link.click();
    });
  }
  const qrOverlay = el("qrOverlay");
  if (qrOverlay) {
    qrOverlay.addEventListener("click", (event) => {
      if (event.target === qrOverlay) {
        closeQrModal();
      }
    });
  }
  const scanQrBtn = el("scanQr");
  if (scanQrBtn) {
    scanQrBtn.addEventListener("click", () => startQrScan("auto"));
  }
  const qrScanClose = el("qrScanClose");
  if (qrScanClose) {
    qrScanClose.addEventListener("click", stopQrScan);
  }
  const qrScanStop = el("qrScanStop");
  if (qrScanStop) {
    qrScanStop.addEventListener("click", stopQrScan);
  }
  const qrScanOverlay = el("qrScanOverlay");
  if (qrScanOverlay) {
    qrScanOverlay.addEventListener("click", (event) => {
      if (event.target === qrScanOverlay) {
        stopQrScan();
      }
    });
  }
  const qrScanAuto = el("qrScanAuto");
  if (qrScanAuto) {
    qrScanAuto.addEventListener("click", () => startQrScan("auto"));
  }
  const qrScanOnce = el("qrScanOnce");
  if (qrScanOnce) {
    qrScanOnce.addEventListener("click", () => startQrScan("once"));
  }
  const qrUploadInput = el("qrUpload");
  if (qrUploadInput) {
    qrUploadInput.addEventListener("change", async (event) => {
      const file = event.target.files && event.target.files[0];
      if (file) {
        await scanQrFromFile(file);
        event.target.value = "";
      }
    });
  }
  const copyEvmBtn = el("copyEvmAddress");
  if (copyEvmBtn) {
    copyEvmBtn.addEventListener("click", () => {
      copyEVMAddress();
    });
  }
  if (authConnect) {
    authConnect.addEventListener("click", startAuthFlow);
  }

  el("balanceForm").addEventListener("submit", fetchBalance);
  el("faucetForm").addEventListener("submit", requestFaucet);
  el("sendForm").addEventListener("submit", sendTransfer);
  el("stakeForm").addEventListener("submit", sendStake);
  el("unstakeForm").addEventListener("submit", sendUnstake);
  if (el("poolTransferForm")) {
    el("poolTransferForm").addEventListener("submit", submitPoolTransfer);
  }
  if (dexQuoteForm) {
    dexQuoteForm.addEventListener("submit", handleDEXRouteQuote);
  }
  if (dexSwapForm) {
    dexSwapForm.addEventListener("submit", handleDEXRouteSwap);
  }
  if (dexCreatePoolForm) {
    dexCreatePoolForm.addEventListener("submit", handleDEXPoolCreate);
  }
  if (dexAddLiquidityForm) {
    dexAddLiquidityForm.addEventListener("submit", handleDEXAddLiquidity);
  }
  if (dexRemoveLiquidityForm) {
    dexRemoveLiquidityForm.addEventListener("submit", handleDEXRemoveLiquidity);
  }
  refreshTokensBtn.addEventListener("click", () => loadCoins({ force: true }));
  if (refreshNFTsBtn) {
    refreshNFTsBtn.addEventListener("click", () => loadNFTPortfolio({ force: true }));
  }
  if (nftTab721Btn) {
    nftTab721Btn.addEventListener("click", () => setActiveNFTTab("721"));
  }
  if (nftTab1155Btn) {
    nftTab1155Btn.addEventListener("click", () => setActiveNFTTab("1155"));
  }
  refreshPoolsBtn.addEventListener("click", async () => {
    await loadTokenomics({ force: true });
    await loadDEXPools({ force: true });
  });
  if (refreshDexDataBtn) {
    refreshDexDataBtn.addEventListener("click", async () => {
      await loadCoins({ force: true });
      await loadDEXPools({ force: true });
    });
  }
  if (dexRefreshPoolsBtn) {
    dexRefreshPoolsBtn.addEventListener("click", () => loadDEXPools({ force: true }));
  }
  if (dexUseQuoteBtn) {
    dexUseQuoteBtn.addEventListener("click", applyDEXQuoteToSwap);
  }
  if (dexOpenIdeBtn) {
    dexOpenIdeBtn.addEventListener("click", () => {
      openDTLIDE({ dtlType: "POOL_SWAP_ROUTE", account: state.wallet && state.wallet.address });
    });
  }
  if (dexOpenLendingIdeBtn) {
    dexOpenLendingIdeBtn.addEventListener("click", () => {
      openDTLIDE({ dtlType: "LEND_MARKET_CREATE", account: state.wallet && state.wallet.address });
    });
  }
  if (dexOpenFarmIdeBtn) {
    dexOpenFarmIdeBtn.addEventListener("click", () => {
      openDTLIDE({ dtlType: "FARM_STAKE_LP", account: state.wallet && state.wallet.address });
    });
  }
  refreshTxsBtn.addEventListener("click", () => loadTxHistory({ force: true }));

  el("sendAmount").addEventListener("input", updateFeeLabels);
  el("stakeAmount").addEventListener("input", updateFeeLabels);
  el("unstakeAmount").addEventListener("input", updateFeeLabels);
  el("balanceCoin").addEventListener("change", () => refreshBalance({ force: true }));
  if (autoSyncSelect) {
    autoSyncSelect.addEventListener("change", () => {
      state.autoSync = autoSyncSelect.value === "on";
      localStorage.setItem("msc_autosync", state.autoSync ? "on" : "off");
      scheduleAutoSync();
    });
  }
  if (autoSyncMsInput) {
    const updateAutoSyncMs = () => {
      state.autoSyncMs = normalizeAutoSyncMs(autoSyncMsInput.value);
      autoSyncMsInput.value = String(state.autoSyncMs);
      localStorage.setItem(AUTO_SYNC_MS_KEY, String(state.autoSyncMs));
      if (state.autoSync) {
        scheduleAutoSync();
      }
    };
    autoSyncMsInput.addEventListener("change", updateAutoSyncMs);
    autoSyncMsInput.addEventListener("blur", updateAutoSyncMs);
  }
  if (broadcastSelect) {
    broadcastSelect.addEventListener("change", () => {
      state.broadcastMode = broadcastSelect.value;
      localStorage.setItem("msc_broadcast", state.broadcastMode);
    });
  }
  const stakeValidatorInput = el("stakeValidator");
  if (stakeValidatorInput) {
    stakeValidatorInput.addEventListener("input", updateStakeValidatorStatus);
    stakeValidatorInput.addEventListener("change", updateStakeValidatorStatus);
  }
  if (stakeValidatorPubKeyInput) {
    const handleStakeValidatorPubKeyEdit = () => {
      stakeValidatorPubKeyInput.dataset.userEdited = "1";
      stakeValidatorPubKeyInput.dataset.autofilled = "0";
      updateStakeValidatorPubKeyUI();
    };
    stakeValidatorPubKeyInput.addEventListener("input", handleStakeValidatorPubKeyEdit);
    stakeValidatorPubKeyInput.addEventListener("change", handleStakeValidatorPubKeyEdit);
  }

  document.querySelector(".tabs").addEventListener("click", switchTab);
  if (poolList) {
    poolList.addEventListener("click", handlePoolAction);
  }
  if (dexPoolList) {
    dexPoolList.addEventListener("click", handleDEXPoolAction);
  }
  if (bridgeApproveBtn) {
    bridgeApproveBtn.addEventListener("click", handleBridgeApprove);
  }
  if (bridgeRejectBtn) {
    bridgeRejectBtn.addEventListener("click", handleBridgeReject);
  }
  if (bridgeApprovalOverlay) {
    bridgeApprovalOverlay.addEventListener("click", (event) => {
      if (event.target === bridgeApprovalOverlay) {
        handleBridgeReject();
      }
    });
  }
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && state.bridgeApprovalActive) {
      handleBridgeReject();
    }
  });

  document.addEventListener("visibilitychange", () => {
    if (!state.autoSync) return;
    if (document.hidden) {
      scheduleAutoSync();
      return;
    }
    state.syncErrorStreak = 0;
    triggerImmediateSync();
  });
  window.addEventListener("focus", () => {
    if (!state.autoSync) return;
    triggerImmediateSync();
  });
  window.addEventListener("online", () => {
    if (!state.autoSync) return;
    state.syncErrorStreak = 0;
    triggerImmediateSync();
  });
  window.addEventListener("offline", () => {
    if (!state.autoSync) return;
    scheduleAutoSync();
  });

  connectToRPC({ persist: false });
  window.MSC_WALLET_APP_READY = true;
};

init();



