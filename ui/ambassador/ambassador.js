(() => {
  "use strict";

  const PAGE = document.body.dataset.page || "home";
  const STORE_KEY = "mscAmbassadorPortal.v1";
  const LAST_SUBMIT_KEY = "mscAmbassadorPortal.lastApplicationSubmit";
  const LAST_BUG_REPORT_KEY = "mscAmbassadorPortal.lastBugReportSubmit";
  const ADMIN_SESSION_KEY = "mscAmbassadorPortal.adminSession";
  const ADMIN_PASSWORD = "MSC-ADMIN-2026";
  const FIREBASE_SETTINGS = window.MSC_AMBASSADOR_FIREBASE || {};
  const ADMIN_EMAIL = String(FIREBASE_SETTINGS.adminEmail || "admin@msc.com").trim().toLowerCase();
  const REAL_WALLET_URL = "https://wallet.mscblockexplorer.in/";
  const LOGO_SRC = "../assets/msc-logo-64.png";
  const NFT_SRC = "../assets/msc-nft-badge.png";
  const VALIDATOR_SRC = "../assets/msc-validator-badge.png";
  const WALLET_SRC = "../assets/msc-wallet-icon.png";

  const pages = [
    ["home", "Home", "index.html", "layout-dashboard"],
    ["program", "Program", "program.html", "badge-check"],
    ["rewards", "Rewards", "rewards.html", "gift"],
    ["apply", "Apply", "apply.html", "send"],
    ["leaderboard", "Leaderboard", "leaderboard.html", "trophy"],
    ["profiles", "Profiles", "profiles.html", "user-round-check"],
    ["influencer", "Influencer", "influencer.html", "user-cog"],
    ["referrals", "Referrals", "referrals.html", "git-branch"],
    ["bug-bounty", "Bug Bounty", "bug-bounty.html", "bug"],
    ["nft", "Founder NFT", "nft.html", "gem"],
    ["validator-benefits", "Validator", "validator-benefits.html", "server"],
    ["analytics", "Analytics", "analytics.html", "bar-chart-3"],
    ["announcements", "Updates", "announcements.html", "megaphone"],
    ["faq", "FAQ", "faq.html", "circle-help"],
    ["contact", "Contact", "contact.html", "mail"],
    ["security", "Security", "security.html", "shield-check"],
    ["admin", "Admin", "admin.html", "lock"],
  ];

  const levelMap = {
    bronze: {
      name: "Bronze Ambassador",
      short: "Bronze",
      allocation: 5000,
      className: "bronze",
      requirements: ["1 Instagram post", "1 Story", "Authentic MSC introduction"],
      benefits: ["Official Ambassador Badge", "5,000 MSC locked allocation", "Website profile page"],
    },
    silver: {
      name: "Silver Ambassador",
      short: "Silver",
      allocation: 15000,
      className: "silver",
      requirements: ["2 Posts", "3 Stories", "1 Video/Reel"],
      benefits: ["15,000 MSC locked allocation", "Founder NFT", "Referral rewards"],
    },
    gold: {
      name: "Gold Ambassador",
      short: "Gold",
      allocation: 50000,
      className: "gold",
      requirements: ["Monthly content", "Community engagement", "Live session"],
      benefits: ["50,000 MSC locked allocation", "Founder NFT", "Future validator priority"],
    },
  };

  const seedState = {
    applications: [
      {
        id: "APP-2401",
        fullName: "Ayesha Khan",
        email: "ayesha@example.com",
        country: "India",
        instagram: "@chainnotes",
        followers: 12800,
        links: "youtube.com/@chainnotes",
        portfolio: "chainnotes.dev",
        reason: "I create simple crypto education content and can explain MSC wallet, explorer, validator, and community milestones.",
        level: "silver",
        audience: "Crypto creators",
        status: "pending",
        createdAt: "2026-06-24",
        referralCode: "AYESHA-MSC",
      },
      {
        id: "APP-2402",
        fullName: "Rohan GameFi",
        email: "rohan@example.com",
        country: "Singapore",
        instagram: "@playchainlab",
        followers: 9400,
        links: "x.com/playchainlab",
        portfolio: "playchainlab.com",
        reason: "My audience follows play-to-earn and crypto gaming projects. I can run wallet demos and weekly community threads.",
        level: "bronze",
        audience: "Gaming creators",
        status: "approved",
        createdAt: "2026-06-22",
        referralCode: "ROHAN-MSC",
      },
    ],
    ambassadors: [
      {
        name: "Zara Blocks",
        country: "UAE",
        level: "gold",
        code: "ZARA-MSC",
        referrals: 184,
        monthlyReferrals: 47,
        reputation: 9430,
        followersGained: 3200,
        websiteVisits: 8400,
        walletCreations: 610,
        telegramJoins: 1280,
        nodeOperators: 17,
        validatorApplications: 9,
        audience: "Web3 influencer",
        username: "@zarablocks",
        followers: 18600,
        status: "active",
        verifiedBadge: true,
        founderNFTBadge: true,
      },
      {
        name: "Dev Ledger",
        country: "India",
        level: "silver",
        code: "DEV-MSC",
        referrals: 128,
        monthlyReferrals: 31,
        reputation: 7210,
        followersGained: 2100,
        websiteVisits: 5600,
        walletCreations: 420,
        telegramJoins: 860,
        nodeOperators: 12,
        validatorApplications: 6,
        audience: "Blockchain developer",
        username: "@devledger",
        followers: 14200,
        status: "active",
        verifiedBadge: true,
        founderNFTBadge: true,
      },
      {
        name: "PlayNode Labs",
        country: "Philippines",
        level: "silver",
        code: "PLAY-MSC",
        referrals: 96,
        monthlyReferrals: 24,
        reputation: 6380,
        followersGained: 1450,
        websiteVisits: 3900,
        walletCreations: 310,
        telegramJoins: 590,
        nodeOperators: 5,
        validatorApplications: 3,
        audience: "Crypto gaming",
        username: "@playnodelabs",
        followers: 9800,
        status: "active",
        verifiedBadge: true,
        founderNFTBadge: true,
      },
      {
        name: "Startup Chain",
        country: "UK",
        level: "bronze",
        code: "STARTUP-MSC",
        referrals: 54,
        monthlyReferrals: 18,
        reputation: 3510,
        followersGained: 900,
        websiteVisits: 2400,
        walletCreations: 140,
        telegramJoins: 320,
        nodeOperators: 3,
        validatorApplications: 2,
        audience: "Startup and AI",
        username: "@startupchain",
        followers: 5200,
        status: "active",
        verifiedBadge: true,
        founderNFTBadge: false,
      },
    ],
    referrals: [
      { code: "ZARA-MSC", userName: "Naveen", email: "naveen@example.com", rewardUser: 10, rewardAmbassador: 5, points: 20, createdAt: "2026-06-24" },
      { code: "DEV-MSC", userName: "Mira", email: "mira@example.com", rewardUser: 10, rewardAmbassador: 5, points: 20, createdAt: "2026-06-23" },
      { code: "PLAY-MSC", userName: "Kai", email: "kai@example.com", rewardUser: 10, rewardAmbassador: 5, points: 20, createdAt: "2026-06-23" },
    ],
    announcements: [
      { type: "MSC updates", title: "MSC Ambassador portal launched", body: "Creator applications, referrals, rewards, Founder NFT tracking, admin review, analytics, and security rules are now organized in one portal.", date: "2026-06-24" },
      { type: "Campaign update", title: "Ambassador intake opens for 1k to 20k follower creators", body: "Priority review starts with crypto educators, blockchain developers, Web3 influencers, gaming creators, and technology pages.", date: "2026-06-24" },
      { type: "New rewards", title: "All ambassador MSC allocations use vesting", body: "No large unlocked token distributions. Rewards remain locked according to the published schedule.", date: "2026-06-24" },
      { type: "Partnership", title: "Sponsored content disclosure rule added", body: "Ambassadors must disclose sponsored partnerships when required by local laws and platform rules.", date: "2026-06-23" },
    ],
    contacts: [],
    bugReports: [],
    influencers: [],
    users: [],
    rewards: [],
    campaigns: [],
    settings: { roles: { adminLimit: 1, influencerLimit: "unlimited", normalUserLimit: "unlimited" } },
  };

  const $ = (id) => document.getElementById(id);
  const esc = (value) => String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[ch]));
  const fmt = (value) => {
    const n = Number(value);
    if (!Number.isFinite(n)) return value === 0 ? "0" : "-";
    return new Intl.NumberFormat("en-US").format(Math.round(n));
  };
  const today = () => new Date().toISOString().slice(0, 10);
  const icon = (name) => `<i data-lucide="${name}" aria-hidden="true"></i>`;
  const firebaseState = {
    enabled: isFirebaseConfigured(),
    mode: FIREBASE_SETTINGS.databaseMode === "firestore" ? "firestore" : "realtime",
    ready: false,
    loading: false,
    error: "",
    app: null,
    auth: null,
    firestore: null,
    realtime: null,
    modules: null,
    user: null,
    admin: false,
    authNotice: "",
    influencer: null,
    influencerReferrals: [],
    influencerRewards: [],
  };
  const isAdmin = () => firebaseState.enabled ? firebaseState.admin : sessionStorage.getItem(ADMIN_SESSION_KEY) === "active";

  function isFirebaseConfigured() {
    const cfg = FIREBASE_SETTINGS.firebaseConfig || {};
    const modeReady = FIREBASE_SETTINGS.databaseMode === "firestore" || !!cfg.databaseURL;
    return !!(
      FIREBASE_SETTINGS.enabled &&
      modeReady &&
      cfg.apiKey &&
      cfg.projectId &&
      !String(cfg.apiKey).includes("YOUR_") &&
      !String(cfg.projectId).includes("YOUR_")
    );
  }

  function emptyState() {
    return {
      applications: [],
      ambassadors: [],
      referrals: [],
      announcements: [],
      contacts: [],
      bugReports: [],
      influencers: [],
      users: [],
      rewards: [],
      campaigns: [],
      settings: {},
    };
  }

  function isSingleAdminEmail(email) {
    return String(email || "").trim().toLowerCase() === ADMIN_EMAIL;
  }

  function dataSourceLabel() {
    if (!firebaseState.enabled) return "Local demo database";
    if (firebaseState.error) return "Database check needed";
    if (!firebaseState.ready) return "Database connecting";
    return "Live database";
  }

  function collectionName(key) {
    const prefix = FIREBASE_SETTINGS.collectionPrefix || "msc_ambassador";
    return `${prefix}_${key}`;
  }

  function loadState() {
    try {
      const raw = JSON.parse(localStorage.getItem(STORE_KEY) || "null");
      const fallback = firebaseState.enabled ? emptyState() : structuredClone(seedState);
      if (!raw || typeof raw !== "object") return fallback;
      return {
        applications: Array.isArray(raw.applications) ? raw.applications : fallback.applications,
        ambassadors: Array.isArray(raw.ambassadors) ? raw.ambassadors : fallback.ambassadors,
        referrals: Array.isArray(raw.referrals) ? raw.referrals : fallback.referrals,
        announcements: Array.isArray(raw.announcements) ? raw.announcements : fallback.announcements,
        contacts: Array.isArray(raw.contacts) ? raw.contacts : [],
        bugReports: Array.isArray(raw.bugReports) ? raw.bugReports : fallback.bugReports,
        influencers: Array.isArray(raw.influencers) ? raw.influencers : fallback.influencers,
        users: Array.isArray(raw.users) ? raw.users : fallback.users,
        rewards: Array.isArray(raw.rewards) ? raw.rewards : fallback.rewards,
        campaigns: Array.isArray(raw.campaigns) ? raw.campaigns : fallback.campaigns,
        settings: raw.settings && typeof raw.settings === "object" ? raw.settings : fallback.settings,
      };
    } catch (_) {
      return firebaseState.enabled ? emptyState() : structuredClone(seedState);
    }
  }

  let db = loadState();

  function saveState() {
    localStorage.setItem(STORE_KEY, JSON.stringify(db));
  }

  function remoteReady() {
    const databaseReady = firebaseState.mode === "firestore" ? firebaseState.firestore : firebaseState.realtime;
    return firebaseState.enabled && firebaseState.ready && databaseReady && firebaseState.modules;
  }

  async function initFirebaseData() {
    if (!firebaseState.enabled || firebaseState.loading || firebaseState.ready) return;
    firebaseState.loading = true;
    try {
      const version = FIREBASE_SETTINGS.sdkVersion || "12.15.0";
      const databaseModule = firebaseState.mode === "firestore" ? "firebase-firestore.js" : "firebase-database.js";
      const [appMod, authMod, dataMod] = await Promise.all([
        import(`https://www.gstatic.com/firebasejs/${version}/firebase-app.js`),
        import(`https://www.gstatic.com/firebasejs/${version}/firebase-auth.js`),
        import(`https://www.gstatic.com/firebasejs/${version}/${databaseModule}`),
      ]);
      firebaseState.modules = firebaseState.mode === "firestore"
        ? { appMod, authMod, firestoreMod: dataMod }
        : { appMod, authMod, databaseMod: dataMod };
      firebaseState.app = appMod.initializeApp(FIREBASE_SETTINGS.firebaseConfig);
      firebaseState.auth = authMod.getAuth(firebaseState.app);
      if (firebaseState.mode === "firestore") {
        firebaseState.firestore = dataMod.getFirestore(firebaseState.app);
      } else {
        firebaseState.realtime = dataMod.getDatabase(firebaseState.app, FIREBASE_SETTINGS.firebaseConfig.databaseURL);
      }
      authMod.onAuthStateChanged(firebaseState.auth, async (user) => {
        firebaseState.user = user || null;
        firebaseState.admin = await checkFirebaseAdmin(user);
        if (PAGE === "admin" && user && !firebaseState.admin) {
          firebaseState.authNotice = `Only ${ADMIN_EMAIL} can access this dashboard.`;
          await authMod.signOut(firebaseState.auth);
          return;
        }
        await refreshRemoteData();
        render();
      });
      firebaseState.ready = true;
      firebaseState.error = "";
      await refreshRemoteData();
    } catch (err) {
      firebaseState.error = err?.message || "Database connection failed";
      console.error("MSC Ambassador database error:", err);
    } finally {
      firebaseState.loading = false;
      render();
    }
  }

  async function readRemoteCollection(key) {
    if (!remoteReady()) return [];
    try {
      if (firebaseState.mode === "firestore") {
        const { collection, getDocs } = firebaseState.modules.firestoreMod;
        const snap = await getDocs(collection(firebaseState.firestore, collectionName(key)));
        return snap.docs.map((docSnap) => ({ id: docSnap.id, ...docSnap.data() }));
      }
      const { get, ref } = firebaseState.modules.databaseMod;
      const snap = await get(ref(firebaseState.realtime, collectionName(key)));
      const value = snap.val() || {};
      return Object.entries(value).map(([id, data]) => ({ id, ...(data || {}) }));
    } catch (err) {
      if (firebaseState.admin) console.warn(`Unable to read ${collectionName(key)}`, err);
      return [];
    }
  }

  async function readRemoteValue(key) {
    if (!remoteReady()) return {};
    try {
      if (firebaseState.mode === "firestore") {
        const { doc, getDoc } = firebaseState.modules.firestoreMod;
        const snap = await getDoc(doc(firebaseState.firestore, collectionName(key), "portal"));
        return snap.exists() ? snap.data() : {};
      }
      const { get, ref } = firebaseState.modules.databaseMod;
      const snap = await get(ref(firebaseState.realtime, collectionName(key)));
      return snap.val() || {};
    } catch (err) {
      if (firebaseState.admin) console.warn(`Unable to read ${collectionName(key)}`, err);
      return {};
    }
  }

  async function readRemotePath(path) {
    if (!remoteReady()) return null;
    try {
      if (firebaseState.mode === "firestore") return null;
      const { get, ref } = firebaseState.modules.databaseMod;
      const snap = await get(ref(firebaseState.realtime, path));
      return snap.val();
    } catch (_) {
      return null;
    }
  }

  async function readInfluencerReferrals(code) {
    if (!remoteReady() || !code) return [];
    try {
      if (firebaseState.mode === "firestore") {
        const { collection, getDocs, query, where } = firebaseState.modules.firestoreMod;
        const q = query(collection(firebaseState.firestore, collectionName("influencer_referrals")), where("code", "==", code));
        const snap = await getDocs(q);
        return snap.docs.map((docSnap) => ({ id: docSnap.id, ...docSnap.data() }));
      }
      const value = await readRemotePath(`${collectionName("influencer_referrals")}/${code}`);
      return Object.entries(value || {}).map(([id, data]) => ({ id, ...(data || {}) }));
    } catch (_) {
      return [];
    }
  }

  async function readInfluencerRewards(code) {
    if (!remoteReady() || !code) return [];
    try {
      if (firebaseState.mode === "firestore") {
        const { collection, getDocs, query, where } = firebaseState.modules.firestoreMod;
        const q = query(collection(firebaseState.firestore, collectionName("influencer_rewards")), where("code", "==", code));
        const snap = await getDocs(q);
        return snap.docs.map((docSnap) => ({ id: docSnap.id, ...docSnap.data() }));
      }
      const value = await readRemotePath(`${collectionName("influencer_rewards")}/${code}`);
      return Object.entries(value || {}).map(([id, data]) => ({ id, ...(data || {}) }));
    } catch (_) {
      return [];
    }
  }

  async function refreshRemoteData() {
    if (!remoteReady()) return;
    const adminOnly = firebaseState.admin;
    const [applications, ambassadors, referrals, announcements, contacts, bugReports, influencers, users, rewards, campaigns, settings] = await Promise.all([
      adminOnly ? readRemoteCollection("applications") : Promise.resolve(db.applications || []),
      readRemoteCollection("ambassadors"),
      readRemoteCollection("referrals"),
      readRemoteCollection("announcements"),
      adminOnly ? readRemoteCollection("contacts") : Promise.resolve(db.contacts || []),
      adminOnly ? readRemoteCollection("bug_reports") : Promise.resolve(db.bugReports || []),
      adminOnly ? readRemoteCollection("influencers") : Promise.resolve(db.influencers || []),
      adminOnly ? readRemoteCollection("users") : Promise.resolve(db.users || []),
      adminOnly ? readRemoteCollection("rewards") : Promise.resolve(db.rewards || []),
      readRemoteCollection("campaigns"),
      readRemoteValue("settings"),
    ]);
    applications.sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    referrals.sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    announcements.sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    contacts.sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    bugReports.sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    rewards.sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    db = {
      applications,
      ambassadors,
      referrals,
      announcements,
      contacts,
      bugReports,
      influencers,
      users,
      rewards,
      campaigns,
      settings,
    };
    saveState();
  }

  async function checkFirebaseAdmin(user) {
    if (!user || !firebaseState.modules || !isSingleAdminEmail(user.email)) return false;
    try {
      const token = await user.getIdTokenResult?.();
      if (token?.signInProvider) return token.signInProvider === "password";
    } catch (_) {
      // Fall back to providerData below.
    }
    return Array.isArray(user.providerData) && user.providerData.some((provider) => provider.providerId === "password");
  }

  async function addRemoteDoc(key, payload) {
    if (firebaseState.mode === "firestore") {
      const { addDoc, collection } = firebaseState.modules.firestoreMod;
      const docRef = await addDoc(collection(firebaseState.firestore, collectionName(key)), payload);
      return { id: docRef.id, ...payload };
    }
    const { push, ref, set } = firebaseState.modules.databaseMod;
    const itemRef = push(ref(firebaseState.realtime, collectionName(key)));
    await set(itemRef, payload);
    return { id: itemRef.key, ...payload };
  }

  async function addRemoteReferral(publicPayload, privatePayload, userPayload) {
    if (firebaseState.mode === "firestore") {
      const { collection, doc, writeBatch } = firebaseState.modules.firestoreMod;
      const publicRef = doc(collection(firebaseState.firestore, collectionName("referrals")));
      const privateRef = doc(firebaseState.firestore, collectionName("referral_claims"), publicRef.id);
      const userRef = doc(collection(firebaseState.firestore, collectionName("users")));
      const influencerReferralRef = doc(firebaseState.firestore, collectionName("influencer_referrals"), `${publicPayload.code}_${publicRef.id}`);
      const batch = writeBatch(firebaseState.firestore);
      batch.set(publicRef, publicPayload);
      batch.set(privateRef, privatePayload);
      batch.set(userRef, userPayload);
      batch.set(influencerReferralRef, {
        code: publicPayload.code,
        referralId: publicRef.id,
        userName: privatePayload.userName,
        emailHash: publicPayload.emailHash,
        points: publicPayload.points,
        rewardAmbassador: publicPayload.rewardAmbassador,
        createdAt: publicPayload.createdAt,
        createdAtMs: publicPayload.createdAtMs,
      });
      await batch.commit();
      return { id: publicRef.id, ...publicPayload };
    }
    const { push, ref, update } = firebaseState.modules.databaseMod;
    const publicRef = push(ref(firebaseState.realtime, collectionName("referrals")));
    const userRef = push(ref(firebaseState.realtime, collectionName("users")));
    const id = publicRef.key;
    await update(ref(firebaseState.realtime), {
      [`${collectionName("referrals")}/${id}`]: publicPayload,
      [`${collectionName("referral_claims")}/${id}`]: privatePayload,
      [`${collectionName("users")}/${userRef.key}`]: userPayload,
      [`${collectionName("influencer_referrals")}/${publicPayload.code}/${id}`]: {
        code: publicPayload.code,
        referralId: id,
        userName: privatePayload.userName,
        emailHash: publicPayload.emailHash,
        points: publicPayload.points,
        rewardAmbassador: publicPayload.rewardAmbassador,
        createdAt: publicPayload.createdAt,
        createdAtMs: publicPayload.createdAtMs,
      },
    });
    return { id, ...publicPayload };
  }

  async function setRemoteDoc(key, id, payload) {
    if (firebaseState.mode === "firestore") {
      const { doc, setDoc } = firebaseState.modules.firestoreMod;
      await setDoc(doc(firebaseState.firestore, collectionName(key), id), payload, { merge: true });
      return;
    }
    const { ref, update } = firebaseState.modules.databaseMod;
    await update(ref(firebaseState.realtime, `${collectionName(key)}/${id}`), payload);
  }

  async function updateRemoteDoc(key, id, payload) {
    if (firebaseState.mode === "firestore") {
      const { doc, updateDoc } = firebaseState.modules.firestoreMod;
      await updateDoc(doc(firebaseState.firestore, collectionName(key), id), payload);
      return;
    }
    const { ref, update } = firebaseState.modules.databaseMod;
    await update(ref(firebaseState.realtime, `${collectionName(key)}/${id}`), payload);
  }

  function stats() {
    const approvedApps = db.applications.filter((item) => item.status === "approved").length;
    const totalApps = db.applications.length;
    const active = activeAmbassadors();
    const totalReferrals = Math.max(
      db.referrals.length,
      active.reduce((sum, item) => sum + Number(item.referrals || 0), 0),
    );
    const kpi = active.reduce((sum, item) => ({
      followersGained: sum.followersGained + Number(item.followersGained || 0),
      websiteVisits: sum.websiteVisits + Number(item.websiteVisits || 0),
      walletCreations: sum.walletCreations + Number(item.walletCreations || 0),
      telegramJoins: sum.telegramJoins + Number(item.telegramJoins || 0),
      nodeOperators: sum.nodeOperators + Number(item.nodeOperators || 0),
      validatorApplications: sum.validatorApplications + Number(item.validatorApplications || 0),
    }), { followersGained: 0, websiteVisits: 0, walletCreations: 0, telegramJoins: 0, nodeOperators: 0, validatorApplications: 0 });
    return {
      totalAmbassadors: active.length,
      totalApplications: totalApps,
      pendingApplications: db.applications.filter((item) => item.status === "pending").length,
      totalReferrals,
      approvalRate: totalApps ? Math.round((approvedApps / totalApps) * 100) : 0,
      expectedReach: Math.max(5000, Math.min(20000, kpi.followersGained + totalReferrals * 35)),
      kpi,
    };
  }

  function walletURLForAmbassador(code = "") {
    const safeCode = String(code || "").trim().toUpperCase();
    if (!safeCode) return REAL_WALLET_URL;
    const url = new URL(REAL_WALLET_URL);
    url.searchParams.set("ref", safeCode);
    url.searchParams.set("ambassador", safeCode);
    url.searchParams.set("utm_source", "msc_ambassador");
    return url.toString();
  }

  function ambassadorStatus(item) {
    return String(item?.status || "active").toLowerCase();
  }

  function isActiveAmbassador(item) {
    const status = ambassadorStatus(item);
    return status !== "suspended" && status !== "banned" && status !== "removed";
  }

  function activeAmbassadors() {
    return db.ambassadors.filter(isActiveAmbassador);
  }

  function navHTML() {
    return pages.filter(([key]) => key !== "admin" || PAGE === "admin" || isAdmin()).map(([key, label, href, iconName]) => (
      `<a class="${key === PAGE ? "active" : ""}" href="${href}">${icon(iconName)}<span>${label}</span></a>`
    )).join("");
  }

  function mobileHTML() {
    return [
      ["home", "Home", "index.html", "layout-dashboard"],
      ["program", "Program", "program.html", "badge-check"],
      ["apply", "Apply", "apply.html", "send"],
      ["leaderboard", "Board", "leaderboard.html", "trophy"],
      ["admin", "Admin", "admin.html", "lock"],
    ].filter(([key]) => key !== "admin" || PAGE === "admin" || isAdmin()).map(([key, label, href, iconName]) => `<a class="${key === PAGE ? "active" : ""}" href="${href}">${icon(iconName)}<span>${label}</span></a>`).join("");
  }

  function shell(content) {
    return `
      <div class="ambassador-shell">
        <header class="topbar">
          <nav class="nav-inner" aria-label="MSC Ambassador Portal">
            <a class="brand" href="index.html">
              <span class="brand-mark"><img src="${LOGO_SRC}" alt="MSC logo" /></span>
              <span><strong>MSC Ambassador</strong><small>Influencer portal</small></span>
            </a>
            <div class="nav-scroll">${navHTML()}</div>
            <div class="top-actions">
              <span class="pill teal">${icon("database")}${esc(dataSourceLabel())}</span>
              <a class="btn primary" href="apply.html">${icon("send")}Join</a>
              ${PAGE === "admin" || isAdmin() ? `<a class="icon-btn" href="admin.html" aria-label="Admin dashboard">${icon("lock")}</a>` : ""}
            </div>
          </nav>
        </header>
        ${content}
        <footer class="footer">
          <div class="footer-inner">
            <a class="brand" href="index.html">
              <span class="brand-mark"><img src="${LOGO_SRC}" alt="MSC logo" /></span>
              <span><strong>MSC Chain</strong><small>Ambassador growth system</small></span>
            </a>
            <div class="footer-links">
              <a href="program.html">Program</a>
              <a href="rewards.html">Rewards</a>
              <a href="security.html">Rules</a>
              <a href="contact.html">Support</a>
            </div>
          </div>
        </footer>
        <nav class="mobile-tabs" aria-label="Mobile ambassador navigation">${mobileHTML()}</nav>
      </div>`;
  }

  function pageTitle(eyebrow, title, lead) {
    return `<section class="page-title"><div class="eyebrow">${esc(eyebrow)}</div><h1>${esc(title)}</h1><p class="lead">${esc(lead)}</p></section>`;
  }

  function statCard(label, value, note = "") {
    return `<article class="stat-card"><span class="label">${esc(label)}</span><strong>${esc(value)}</strong>${note ? `<span class="meta">${esc(note)}</span>` : ""}</article>`;
  }

  function list(items) {
    return `<ul class="list">${items.map((item) => `<li>${esc(item)}</li>`).join("")}</ul>`;
  }

  function levelCard(levelKey) {
    const level = levelMap[levelKey];
    return `<article class="level-card ${level.className}">
      <span class="pill ${levelKey === "gold" ? "gold" : levelKey === "silver" ? "violet" : "teal"}">${esc(level.short)}</span>
      <h2>${esc(level.name)}</h2>
      <p>${fmt(level.allocation)} MSC locked allocation</p>
      <div><span class="label">Requirements</span>${list(level.requirements)}</div>
      <div><span class="label">Benefits</span>${list(level.benefits)}</div>
    </article>`;
  }

  function homePage() {
    const s = stats();
    return shell(`
      <section class="hero-band">
        <div class="hero-layout">
          <div class="hero-copy">
            <div class="eyebrow">MSC Ambassador & Influencer Portal</div>
            <h1>Grow MSC Chain with trusted creators.</h1>
            <p class="lead">A dedicated portal for micro influencers, crypto educators, gaming creators, and technology pages to apply, earn reputation, track referrals, and qualify for locked MSC rewards.</p>
            <div class="cta-row">
              <a class="btn primary" href="apply.html">${icon("send")}Join Ambassador</a>
              <a class="btn gold" href="program.html">${icon("badge-check")}View Levels</a>
              <a class="btn" href="leaderboard.html">${icon("trophy")}Leaderboard</a>
            </div>
            <div class="chip-row">
              <span class="pill teal">1k to 20k followers</span>
              <span class="pill gold">Founder NFT eligible</span>
              <span class="pill violet">No profit promises</span>
            </div>
          </div>
          <aside class="hero-console" aria-label="Ambassador live statistics">
            <div class="console-head">
              <div><span class="label">Live Statistics</span><h2>Program Console</h2></div>
              <span class="console-badge">${icon("activity")}Active</span>
            </div>
            <div class="console-grid">
              <div class="console-metric"><span class="label">Ambassadors</span><strong>${fmt(s.totalAmbassadors)}</strong></div>
              <div class="console-metric"><span class="label">Applications</span><strong>${fmt(s.totalApplications)}</strong></div>
              <div class="console-metric"><span class="label">Referrals</span><strong>${fmt(s.totalReferrals)}</strong></div>
              <div class="console-metric"><span class="label">Approval Rate</span><strong>${fmt(s.approvalRate)}%</strong></div>
            </div>
            <div class="row-item"><span>Expected early reach</span><strong>${fmt(s.expectedReach)} users</strong></div>
          </aside>
        </div>
      </section>
      <main class="portal-main">
        <section class="stats-grid">
          ${statCard("Followers Gained", fmt(s.kpi.followersGained), "KPI tracking")}
          ${statCard("Website Visits", fmt(s.kpi.websiteVisits), "Campaign traffic")}
          ${statCard("Wallet Creations", fmt(s.kpi.walletCreations), "Onboarding signal")}
          ${statCard("Validator Applications", fmt(s.kpi.validatorApplications), "Future operators")}
        </section>
        <section class="two-column">
          <div class="portal-card">
            <span class="label">MSC Chain Introduction</span>
            <h2>Mission & Vision: community growth without direct cash spending</h2>
            <p>MSC ambassadors educate real audiences, bring verified community members, and support wallet, explorer, validator, and product adoption through transparent content.</p>
            <div class="media-strip">
              ${mediaTile("Education", "MSC intro posts", LOGO_SRC)}
              ${mediaTile("Demos", "Wallet and explorer", WALLET_SRC)}
              ${mediaTile("Founder NFT", "Silver and Gold", NFT_SRC)}
              ${mediaTile("Validators", "Future priority", VALIDATOR_SRC)}
            </div>
          </div>
          <div class="portal-card">
            <span class="label">Roadmap Preview</span>
            <div class="timeline">
              ${timeline("Month 1", "10 ambassadors")}
              ${timeline("Month 2", "25 ambassadors")}
              ${timeline("Month 3", "50 ambassadors")}
              ${timeline("Month 6", "100+ ambassadors")}
            </div>
          </div>
        </section>
        <section class="module-grid">
          ${moduleLink("Ambassador Program", "Bronze, Silver, and Gold levels with requirements and benefits.", "program.html", "badge-check")}
          ${moduleLink("Rewards", "Locked MSC, Founder NFT, referrals, reputation, and milestone bonuses.", "rewards.html", "gift")}
          ${moduleLink("Referral System", "Unique codes, tracking, reward stats, and abuse controls.", "referrals.html", "git-branch")}
          ${moduleLink("Real MSC Wallet", "Open the official wallet with an ambassador referral code.", REAL_WALLET_URL, "wallet")}
          ${moduleLink("Testnet Bug Bounty", "Wallet, explorer, chain, RPC, validator, and performance reports.", "bug-bounty.html", "bug")}
          ${moduleLink("Top Influencers", "Verified profiles, Founder NFT badges, referral codes, and monthly rankings.", "profiles.html", "user-round-check")}
          ${isAdmin() ? moduleLink("Admin Dashboard", "Applications, approvals, users, rewards, and analytics.", "admin.html", "lock") : ""}
        </section>
        <section class="notice-panel">
          <span class="label">Expected Result If Executed Well</span>
          <div class="stats-grid">
            ${statCard("Micro Influencers", "20-50", "focused creator cohort")}
            ${statCard("Targeted Reach", "5,000-20,000", "crypto users")}
            ${statCard("Cash Spend", "Low", "growth through allocations and access")}
            ${statCard("Risk Control", "Vesting", "no large unlocked MSC")}
          </div>
        </section>
      </main>`);
  }

  function mediaTile(title, text, img) {
    return `<div class="media-tile"><img src="${img}" alt="" /><strong>${esc(title)}</strong><span class="muted">${esc(text)}</span></div>`;
  }

  function timeline(label, text) {
    return `<div class="timeline-item"><strong>${esc(label)}</strong><span>${esc(text)}</span></div>`;
  }

  function moduleLink(title, text, href, iconName) {
    return `<a class="portal-card" href="${href}"><span class="pill teal">${icon(iconName)}Open</span><h2>${esc(title)}</h2><p>${esc(text)}</p></a>`;
  }

  function programPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Program", "Ambassador levels and influencer priorities", "Bronze, Silver, and Gold ambassadors use content requirements, locked allocations, and compliance rules.")}
      <section class="level-grid">${levelCard("bronze")}${levelCard("silver")}${levelCard("gold")}</section>
      <section class="module-grid">
        <article class="portal-card"><span class="label">Requirements</span><h2>Content proof and community work</h2>${list(["Posts, stories, reels, monthly content, AMAs, and community engagement are reviewed before reward approval."])}</article>
        <article class="portal-card"><span class="label">Benefits</span><h2>Title, badge, NFT, allocation, and access</h2>${list(["Official Ambassador title", "Official Ambassador Badge", "Founder NFT for Silver and Gold", "Early MSC allocation", "Future validator priority for Gold"])}</article>
        <article class="portal-card"><span class="label">Reward Structure</span><h2>Locked incentives and reputation</h2>${list(["MSC token rewards stay locked", "Referral rewards can be token or reputation based", "Top ambassadors can qualify for larger future rewards"])}</article>
      </section>
      <section class="two-column">
        <div class="portal-card">
          <span class="label">Token Lock</span>
          <h2>Vesting applies to every ambassador allocation</h2>
          <div class="timeline">
            ${timeline("25% after 6 months", "First locked allocation release")}
            ${timeline("25% after 12 months", "Second locked allocation release")}
            ${timeline("50% after mainnet milestones", "Final release after published milestones")}
          </div>
        </div>
        <div class="portal-card">
          <span class="label">Target Influencers</span>
          <div class="timeline">
            ${timeline("Priority 1", "Crypto creators: YouTube crypto educators, Blockchain developers, Web3 influencers")}
            ${timeline("Priority 2", "Gaming creators: play-to-earn audience and crypto gaming audience")}
            ${timeline("Priority 3", "Tech creators: startup, AI, and technology pages")}
            ${timeline("Start band", "Review starts with 1,000 to 20,000 follower accounts")}
          </div>
        </div>
      </section>
      <section class="wide-two-column">
        <div class="portal-card">
          <span class="label">Content Requirements</span>
          <div class="timeline">
            ${timeline("Week 1", "MSC introduction post")}
            ${timeline("Week 2", "MSC technology overview")}
            ${timeline("Week 3", "Wallet and explorer demo")}
            ${timeline("Week 4", "Community AMA")}
          </div>
        </div>
        <div class="notice-panel">
          <span class="label">Partnership Agreement</span>
          ${list(["No fake followers", "No bot traffic", "No misleading financial claims", "Disclose sponsored partnership when required by local laws", "MSC reserves the right to remove ambassador status"])}
        </div>
      </section>
      <section class="portal-card">
        <span class="label">Growth Phases</span>
        <div class="timeline">
          ${timeline("Phase 3: Target Influencers", "Crypto creators first, then gaming creators, then startup, AI, and technology pages.")}
          ${timeline("Phase 4: Offer Package", "Official title, Founder NFT, early MSC allocation, referral rewards, profile page, validator access, and early product access.")}
          ${timeline("Phase 5: Referral System", "Unique referral codes, tracking, rewards, reputation points, and leaderboard ranking.")}
          ${timeline("Phase 6: Content Requirements", "Week 1 introduction, Week 2 technology overview, Week 3 wallet and explorer demo, Week 4 community AMA.")}
          ${timeline("Phase 7: Partnership Agreement", "No fake followers, bots, misleading claims, or undisclosed sponsorship where disclosure is required.")}
          ${timeline("Phase 8: Scaling", "Month 1: 10 ambassadors, Month 2: 25, Month 3: 50, Month 6: 100+.")}
        </div>
      </section>
    </main>`);
  }

  function rewardsPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Rewards", "Locked rewards, reputation, and Founder NFT access", "The offer package uses status, early access, referral rewards, and vesting instead of direct cash spending.")}
      <section class="stats-grid">
        ${statCard("Bronze", "5,000 MSC", "locked allocation")}
        ${statCard("Silver", "15,000 MSC", "Founder NFT eligible")}
        ${statCard("Gold", "50,000 MSC", "validator priority")}
        ${statCard("Referral Example", "10 + 5 MSC", "new user plus influencer")}
      </section>
      <section class="two-column">
        <div class="portal-card">
          <span class="label">Offer Package</span>
          ${list(["Official MSC Ambassador title", "Founder NFT", "Early MSC allocation", "Referral rewards", "Website profile page", "Future validator access", "Early product access"])}
        </div>
        <div class="notice-panel">
          <span class="label">Reward Safety</span>
          <h2>No guaranteed profit promise</h2>
          <p>Rewards are community program incentives. The portal never promises token price, investment return, or guaranteed profit.</p>
          ${list(["Do not distribute large amounts of unlocked MSC", "Do not promise future token price", "Use vesting for all ambassador rewards", "Track referrals to prevent abuse and fake signups"])}
        </div>
      </section>
      <section class="level-grid">${levelCard("bronze")}${levelCard("silver")}${levelCard("gold")}</section>
      <section class="portal-card">
        <span class="label">Milestone Bonuses</span>
        <div class="chart-list">
          ${bar("Content completion", 80, "Weekly content calendar")}
          ${bar("Verified referrals", 66, "Quality community joins")}
          ${bar("Wallet activations", 52, "Explorer and wallet demos")}
          ${bar("Node operator leads", 28, "Validator pipeline")}
        </div>
      </section>
    </main>`);
  }

  function applyPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Influencer Application", "Apply to become an MSC Ambassador", "Creator accounts in the 1,000 to 20,000 follower range receive first review for the first program phase.")}
      <section class="wide-two-column">
        <form id="applicationForm" class="form-panel" novalidate>
          <h2>Influencer Application Form</h2>
          <div class="form-grid">
            ${field("fullName", "Full Name", "text", "Your name")}
            ${field("email", "Email", "email", "name@example.com")}
            ${field("country", "Country", "text", "Country")}
            ${field("instagram", "Instagram Username", "text", "@username")}
            ${field("followers", "Followers Count", "number", "1000", "1000", "20000")}
            ${field("links", "YouTube / X Links", "url", "https://x.com/yourprofile")}
            <div class="field">
              <label for="level">Preferred Level</label>
              <select id="level" name="level" required>
                <option value="bronze">Bronze Ambassador</option>
                <option value="silver">Silver Ambassador</option>
                <option value="gold">Gold Ambassador</option>
              </select>
            </div>
            <div class="field">
              <label for="audience">Audience Type</label>
              <select id="audience" name="audience" required>
                <option value="Crypto creators">Crypto creators</option>
                <option value="Gaming creators">Gaming creators</option>
                <option value="Tech creators">Tech creators</option>
              </select>
            </div>
            ${field("portfolio", "Portfolio", "url", "https://your-site.example", "", "", true)}
            <div class="field full">
              <label for="reason">Why join MSC?</label>
              <textarea id="reason" name="reason" required placeholder="Explain your audience, planned content, and how you will grow real MSC community members."></textarea>
            </div>
            <div class="honeypot"><label for="website">Website</label><input id="website" name="website" tabindex="-1" autocomplete="off" /></div>
            <div class="field full checkbox-row">
              <input id="agreement" name="agreement" type="checkbox" required />
              <label for="agreement">I agree to avoid fake followers, bot traffic, misleading financial claims, and undisclosed sponsored content where disclosure is required.</label>
            </div>
          </div>
          <div class="action-row"><button class="btn primary" type="submit">${icon("send")}Submit Application</button><span id="applicationMessage" class="form-message"></span></div>
        </form>
        <aside class="notice-panel">
          <span class="label">Review Rules</span>
          <h2>Quality first</h2>
          ${list(["1,000 to 20,000 followers for first phase", "Real audience and original content required", "No guaranteed profit claims", "All MSC allocations remain locked", "Referral abuse can remove ambassador status"])}
        </aside>
      </section>
    </main>`);
  }

  function field(id, label, type, placeholder, min = "", max = "", full = false) {
    const limits = `${min ? ` min="${esc(min)}"` : ""}${max ? ` max="${esc(max)}"` : ""}`;
    return `<div class="field ${full ? "full" : ""}"><label for="${id}">${esc(label)}</label><input id="${id}" name="${id}" type="${type}" placeholder="${esc(placeholder)}"${limits} required /></div>`;
  }

  function leaderboardPage() {
    const rows = rankedAmbassadors();
    return shell(`<main class="portal-main">
      ${pageTitle("Leaderboard", "Top ambassadors and community ranking", "Rankings combine referral count, reputation points, monthly activity, and community contribution.")}
      <section class="stats-grid">
        ${statCard("Top Ambassador", rows[0]?.name || "-", rows[0]?.code || "")}
        ${statCard("Total Referrals", fmt(stats().totalReferrals), "tracked joins")}
        ${statCard("Reputation Pool", fmt(rows.reduce((sum, item) => sum + Number(item.reputation || 0), 0)), "points")}
        ${statCard("Monthly Leader", rows.slice().sort((a, b) => b.monthlyReferrals - a.monthlyReferrals)[0]?.name || "-", "current month")}
      </section>
      <section class="table-panel">
        <h2>Community Ranking</h2>
        <div class="table-wrap"><table><thead><tr><th>Rank</th><th>Ambassador</th><th>Level</th><th>Badges</th><th>Referral Count</th><th>Reputation Points</th><th>Monthly Ranking</th><th>Audience</th><th>Profile</th></tr></thead><tbody>${rows.map((item, index) => ambassadorRow(item, index)).join("")}</tbody></table></div>
      </section>
    </main>`);
  }

  function profilesPage() {
    const rows = rankedAmbassadors();
    return shell(`<main class="portal-main">
      ${pageTitle("Top Influencers", "Verified MSC ambassador profiles", "Approved influencers get public profile cards, verified ambassador badges, Founder NFT markers, referral codes, reputation, and monthly rankings.")}
      <section class="stats-grid">
        ${statCard("Active Influencers", fmt(rows.length), "approved profiles")}
        ${statCard("Verified Badges", fmt(rows.filter((item) => item.verifiedBadge !== false).length), "public trust signal")}
        ${statCard("Founder NFT", fmt(rows.filter((item) => item.founderNFTBadge || item.level === "silver" || item.level === "gold").length), "eligible profiles")}
        ${statCard("Monthly Leader", rows[0]?.name || "-", rows[0]?.code || "")}
      </section>
      <section class="module-grid">${rows.map(influencerProfileCard).join("")}</section>
    </main>`);
  }

  function rankedAmbassadors(includeInactive = false) {
    const source = includeInactive ? db.ambassadors : activeAmbassadors();
    return source.map(withDerivedReferralStats).sort((a, b) => Number(b.reputation || 0) - Number(a.reputation || 0));
  }

  function withDerivedReferralStats(item) {
    const referrals = db.referrals.filter((ref) => ref.code === item.code);
    const derivedReferrals = referrals.length;
    const derivedPoints = referrals.reduce((sum, ref) => sum + Number(ref.points || 0), 0);
    return {
      ...item,
      referrals: Math.max(Number(item.referrals || 0), derivedReferrals),
      monthlyReferrals: Math.max(Number(item.monthlyReferrals || 0), derivedReferrals),
      reputation: Math.max(Number(item.reputation || 0), derivedPoints),
    };
  }

  function ambassadorRow(item, index) {
    const level = levelMap[item.level] || levelMap.bronze;
    return `<tr>
      <td>${index + 1}</td>
      <td><strong>${esc(item.name)}</strong><div class="mono">${esc(item.code)}</div></td>
      <td><span class="pill ${item.level === "gold" ? "gold" : item.level === "silver" ? "violet" : "teal"}">${esc(level.short)}</span></td>
      <td>${badgeList(item)}</td>
      <td>${fmt(item.referrals)}</td>
      <td>${fmt(item.reputation)}</td>
      <td>${fmt(item.monthlyReferrals || 0)}</td>
      <td>${esc(item.audience || "-")}</td>
      <td><a class="btn" href="profiles.html#${esc(item.code)}">${icon("user-round-check")}Open</a></td>
    </tr>`;
  }

  function badgeList(item) {
    const badges = [];
    if (item.verifiedBadge !== false) badges.push("Verified");
    if (item.founderNFTBadge || item.level === "silver" || item.level === "gold") badges.push("Founder NFT");
    return badges.length ? badges.map((badge) => `<span class="status-pill approved">${esc(badge)}</span>`).join(" ") : `<span class="muted">-</span>`;
  }

  function influencerProfileCard(item, index) {
    const level = levelMap[item.level] || levelMap.bronze;
    const walletHref = walletURLForAmbassador(item.code);
    return `<article id="${esc(item.code)}" class="portal-card">
      <span class="pill ${item.level === "gold" ? "gold" : item.level === "silver" ? "violet" : "teal"}">${esc(level.short)}</span>
      <h2>${esc(item.name)}</h2>
      <p>${esc(item.username || item.code)} · ${esc(item.audience || "MSC influencer")}</p>
      <div class="row-item"><span>Referral Code</span><strong class="mono">${esc(item.code)}</strong></div>
      <div class="row-item"><span>Referral Count</span><strong>${fmt(item.referrals)}</strong></div>
      <div class="row-item"><span>Reputation Score</span><strong>${fmt(item.reputation)}</strong></div>
      <div class="row-item"><span>Monthly Ranking</span><strong>#${index + 1}</strong></div>
      <div class="chip-row">${badgeList(item)}</div>
      <div class="action-row"><a class="btn primary" href="${esc(walletHref)}" target="_blank" rel="noreferrer">${icon("external-link")}Open Real Wallet</a></div>
    </article>`;
  }

  function influencerPage() {
    if (!firebaseState.influencer) {
      const loginFields = firebaseState.enabled ? `
        <div class="field"><label for="influencerEmail">Email</label><input id="influencerEmail" name="influencerEmail" type="email" autocomplete="username" required /></div>
        <div class="field"><label for="influencerPassword">Password</label><input id="influencerPassword" name="influencerPassword" type="password" autocomplete="current-password" required /></div>
        <div class="field"><label for="influencerCode">Referral Code</label><input id="influencerCode" name="influencerCode" type="text" placeholder="YOUR-MSC-CODE" required /></div>
        <p class="muted">Approved influencers can sign in with their email/password account and referral code.</p>` : `
        <p class="muted">Influencer login needs cloud database and email/password auth enabled.</p>`;
      return shell(`<main class="portal-main">
        ${pageTitle("Influencer Portal", "Profile, referrals, rewards, and campaigns", "Approved influencers can review their public profile, referral count, reputation score, reward history, and active campaigns.")}
        <form id="influencerLoginForm" class="form-panel admin-lock" novalidate>
          <h2>Influencer Login</h2>
          ${loginFields}
          <div class="action-row"><button class="btn primary" type="submit">${icon("unlock")}Login</button><span id="influencerLoginMessage" class="form-message"></span></div>
        </form>
      </main>`);
    }

    const profile = firebaseState.influencer;
    const referrals = firebaseState.influencerReferrals.slice().sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    const rewards = firebaseState.influencerRewards.slice().sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    return shell(`<main class="portal-main">
      ${pageTitle("Influencer Portal", profile.name || "MSC Influencer", "Track your ambassador profile, referrals, reputation, locked rewards, and campaigns.")}
      <div class="admin-toolbar"><span class="pill teal">${icon("user-round-check")}${esc(profile.status || "active")}</span><button class="btn" data-influencer-logout="true">${icon("log-out")}Logout</button></div>
      <section class="stats-grid">
        ${statCard("Referral Code", profile.code || "-", "share with new users")}
        ${statCard("Referral Count", fmt(profile.referrals || referrals.length), "tracked joins")}
        ${statCard("Reputation", fmt(profile.reputation || 0), "points")}
        ${statCard("Rewards", fmt(rewards.length), "history")}
      </section>
      <section class="module-grid">
        ${walletHandoffCard(profile)}
      </section>
      <section class="wide-two-column">
        <article class="portal-card">
          <span class="label">Profile</span>
          <h2>${esc(profile.name || "-")}</h2>
          <p>${esc(profile.username || profile.email || "-")} · ${esc(profile.audience || "MSC influencer")}</p>
          ${list([`Status: ${profile.status || "active"}`, `Followers: ${fmt(profile.followers || 0)}`, `Level: ${(levelMap[profile.level] || levelMap.bronze).name}`, `Badges: ${profile.verifiedBadge !== false ? "Verified Ambassador" : "Pending"}${profile.founderNFTBadge ? ", Founder NFT" : ""}`])}
        </article>
        <article class="portal-card">
          <span class="label">Campaigns</span>
          ${db.campaigns.length ? list(db.campaigns.map((campaign) => `${campaign.title || campaign.name || "MSC Campaign"}: ${campaign.status || "open"}`)) : list(["Week 1: MSC introduction post", "Week 2: MSC technology overview", "Week 3: Wallet/explorer demo", "Week 4: Community AMA"])}
        </article>
      </section>
      <section class="table-panel">
        <h2>My Referrals</h2>
        <div class="table-wrap"><table><thead><tr><th>Date</th><th>User</th><th>Reward</th><th>Points</th></tr></thead><tbody>${referrals.map((item) => `<tr><td>${esc(item.createdAt || "-")}</td><td>${esc(item.userName || "Private signup")}</td><td>${fmt(item.rewardAmbassador || 5)} MSC</td><td>${fmt(item.points || 20)}</td></tr>`).join("") || `<tr><td colspan="4">No referrals yet.</td></tr>`}</tbody></table></div>
      </section>
      <section class="table-panel">
        <h2>Reward History</h2>
        <div class="table-wrap"><table><thead><tr><th>Date</th><th>Type</th><th>MSC</th><th>Status</th><th>Vesting</th></tr></thead><tbody>${rewards.map((item) => `<tr><td>${esc(item.createdAt || "-")}</td><td>${esc(item.type || "-")}</td><td>${fmt(item.amountMSC || 0)}</td><td>${esc(item.status || "locked")}</td><td>${esc(item.vesting || "-")}</td></tr>`).join("") || `<tr><td colspan="5">No rewards assigned yet.</td></tr>`}</tbody></table></div>
      </section>
    </main>`);
  }

  function referralsPage() {
    const rows = db.referrals.slice().sort((a, b) => Number(b.createdAtMs || 0) - Number(a.createdAtMs || 0));
    return shell(`<main class="portal-main">
      ${pageTitle("Referral System", "Unique codes, tracking, and reward statistics", "Every verified referral can award the new user 10 MSC and the influencer 5 MSC, or contribute reputation points.")}
      <section class="stats-grid">
        ${statCard("Referral Codes", fmt(activeAmbassadors().length), "approved ambassadors")}
        ${statCard("Tracked Referrals", fmt(db.referrals.length), dataSourceLabel())}
        ${statCard("New User Reward", "10 MSC", "example locked reward")}
        ${statCard("Influencer Reward", "5 MSC", "example locked reward")}
      </section>
      <section class="module-grid">
        ${activeAmbassadors().length ? activeAmbassadors().map(walletHandoffCard).join("") : `<article class="portal-card empty-card"><span class="module-icon">${icon("wallet")}</span><h2>Wallet links appear after approval</h2><p>Approved ambassadors get real MSC Wallet links with their referral code attached.</p></article>`}
      </section>
      <section class="wide-two-column">
        <form id="referralForm" class="form-panel" novalidate>
          <h2>Referral Tracking</h2>
          <div class="form-grid">
            <div class="field full">
              <label for="referralCode">Ambassador Code</label>
              <select id="referralCode" name="referralCode" required>${activeAmbassadors().length ? activeAmbassadors().map((item) => `<option value="${esc(item.code)}">${esc(item.code)} - ${esc(item.name)}</option>`).join("") : `<option value="">No approved ambassadors yet</option>`}</select>
            </div>
            ${field("referralUser", "New User Name", "text", "New community member")}
            ${field("referralEmail", "New User Email", "email", "user@example.com")}
            <div class="field full checkbox-row">
              <input id="referralAgreement" name="referralAgreement" type="checkbox" required />
              <label for="referralAgreement">Referral is not bot traffic, duplicate traffic, or a fake signup.</label>
            </div>
          </div>
          <div class="action-row"><button class="btn primary" type="submit">${icon("plus")}Record Referral</button><span id="referralMessage" class="form-message"></span></div>
        </form>
        <aside class="notice-panel">
          <span class="label">Abuse Controls</span>
          ${list(["Unique referral code per ambassador", "Duplicate email checks", "Manual review for suspicious spikes", "Reputation can replace token rewards", "Top ambassadors get larger rewards later"])}
        </aside>
      </section>
      <section class="table-panel">
        <h2>Referral Statistics</h2>
        <div class="table-wrap"><table><thead><tr><th>Date</th><th>Code</th><th>User</th><th>User Reward</th><th>Influencer Reward</th><th>Points</th></tr></thead><tbody>${rows.map(referralRow).join("")}</tbody></table></div>
      </section>
    </main>`);
  }

  function referralRow(item) {
    return `<tr><td>${esc(item.createdAt)}</td><td class="mono">${esc(item.code)}</td><td>${esc(item.userName || "Private signup")}</td><td>${fmt(item.rewardUser)} MSC</td><td>${fmt(item.rewardAmbassador)} MSC</td><td>${fmt(item.points)}</td></tr>`;
  }

  function walletHandoffCard(item) {
    const href = walletURLForAmbassador(item.code);
    return `<article class="portal-card">
      <span class="pill teal">${icon("wallet")}Real Wallet</span>
      <h2>${esc(item.name)}</h2>
      <p>Use ambassador code <strong class="mono">${esc(item.code)}</strong> inside MSC Wallet.</p>
      <div class="action-row"><a class="btn primary" href="${esc(href)}" target="_blank" rel="noreferrer">${icon("external-link")}Open Wallet</a></div>
    </article>`;
  }

  function bugBountyPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("MSC Blockchain Testnet Bug Bounty", "Improve MSC security, stability, and performance", "Submit wallet, explorer, blockchain, RPC, validator, and performance bugs with clear evidence and reproducible steps.")}
      <section class="stats-grid">
        ${statCard("Wallet Testing", "18", "wallet flows and key safety")}
        ${statCard("Explorer Testing", "15", "blocks, search, APIs")}
        ${statCard("Blockchain Testing", "20", "nodes, consensus, load")}
        ${statCard("Reward Categories", "4", "low to critical severity")}
      </section>
      <section class="module-grid">
        <article class="portal-card"><span class="pill teal">${icon("wallet")}MSC Wallet Testing</span><h2>Wallet and key safety</h2>${list(["Create, import, export, backup, and recovery", "Send, receive, balance accuracy, transaction history", "Pending and failed transaction handling", "QR scanning, address validation, fees, nonce handling", "Wallet encryption, private key protection, seed phrase recovery, multi-device testing"])}</article>
        <article class="portal-card"><span class="pill violet">${icon("search")}MSC Explorer Testing</span><h2>Explorer, charts, and APIs</h2>${list(["Latest blocks, block details, transaction search, address search", "Validator information, block time accuracy, supply display", "Network statistics, transaction status, token information", "Charts and graphs, API responses, pagination, mobile responsiveness, loading performance"])}</article>
        <article class="portal-card"><span class="pill gold">${icon("network")}MSC Blockchain Testing</span><h2>Chain, node, and consensus</h2>${list(["Node synchronization, validator stability, full node operation, P2P connectivity", "Transaction propagation, block production, finality, fork detection, chain recovery", "Snapshot restore, state consistency, mempool behavior, double spend protection", "Invalid transaction rejection, fee processing, reward distribution, upgrade compatibility, RPC reliability, consensus stability, load performance"])}</article>
        <article class="notice-panel"><span class="label">Reward Categories</span><h2>Impact, uniqueness, and report quality</h2><p>Low, medium, high, and critical rewards are reviewed by impact and reproducibility. Duplicate reports may not be eligible.</p></article>
      </section>
      <section class="wide-two-column">
        <form id="bugReportForm" class="form-panel" novalidate>
          <h2>Bug Report Format</h2>
          <div class="form-grid">
            ${field("bugTitle", "Bug Title", "text", "Wallet balance mismatch after confirmed transaction")}
            <div class="field"><label for="bugCategory">Category</label><select id="bugCategory" name="bugCategory" required><option>MSC Wallet</option><option>MSC Explorer</option><option>MSC Blockchain</option><option>Security</option><option>Performance</option><option>RPC / API</option></select></div>
            <div class="field"><label for="bugSeverity">Severity</label><select id="bugSeverity" name="bugSeverity" required><option value="low">Low Severity</option><option value="medium">Medium Severity</option><option value="high">High Severity</option><option value="critical">Critical Severity</option></select></div>
            ${field("reporterName", "Reporter Name", "text", "Your name")}
            ${field("reporterEmail", "Reporter Email", "email", "name@example.com")}
            <div class="field"><label for="evidenceUrl">Screenshot or Video</label><input id="evidenceUrl" name="evidenceUrl" type="url" placeholder="https://..." /></div>
            ${field("environment", "Browser / OS / Device", "text", "Chrome / Windows / Desktop")}
            <div class="field"><label for="nodeVersion">Node Version</label><input id="nodeVersion" name="nodeVersion" type="text" placeholder="Optional" /></div>
            <div class="field full"><label for="bugDescription">Description</label><textarea id="bugDescription" name="bugDescription" required></textarea></div>
            <div class="field full"><label for="stepsToReproduce">Steps to Reproduce</label><textarea id="stepsToReproduce" name="stepsToReproduce" required></textarea></div>
            <div class="field full"><label for="expectedResult">Expected Result</label><textarea id="expectedResult" name="expectedResult" required></textarea></div>
            <div class="field full"><label for="actualResult">Actual Result</label><textarea id="actualResult" name="actualResult" required></textarea></div>
            <div class="honeypot"><label for="bugWebsite">Website</label><input id="bugWebsite" name="bugWebsite" tabindex="-1" autocomplete="off" /></div>
            <div class="field full checkbox-row">
              <input id="bugAgreement" name="bugAgreement" type="checkbox" required />
              <label for="bugAgreement">I confirm this is an original, accurate, reproducible report and understand duplicate reports may not be eligible.</label>
            </div>
          </div>
          <div class="action-row"><button class="btn primary" type="submit">${icon("bug")}Submit Bug Report</button><span id="bugReportMessage" class="form-message"></span></div>
        </form>
        <aside class="notice-panel">
          <span class="label">Review Notes</span>
          ${list(["Rewards depend on impact, uniqueness, and quality", "Include browser, OS, device, and node version when applicable", "Attach screenshot or video evidence where possible", "Do not include guaranteed profit claims", "Critical reports should include exact reproduction and impact details"])}
        </aside>
      </section>
    </main>`);
  }

  function nftPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Founder NFT", "Founder NFT benefits and eligibility", "Silver and Gold ambassadors can qualify for Founder NFT rewards after content and referral quality review.")}
      <section class="wide-two-column">
        <div class="nft-showcase" aria-label="NFT showcase"><div class="nft-card-art"><img src="${NFT_SRC}" alt="MSC Founder NFT badge" /></div></div>
        <div class="portal-card">
          <span class="label">NFT Benefits</span>
          ${list(["Founder identity inside the MSC community", "Public profile signal on the website", "Priority review for future campaigns", "Eligibility marker for future validator access", "Early product access where available"])}
        </div>
      </section>
      <section class="level-grid">
        <article class="level-card bronze"><span class="pill teal">NFT tiers</span><h2>Badge Track</h2><p>Official ambassador badge after approved content.</p></article>
        <article class="level-card silver"><span class="pill violet">NFT tiers</span><h2>Founder NFT Track</h2><p>Founder NFT eligibility after posts, stories, and video or reel review.</p></article>
        <article class="level-card gold"><span class="pill gold">NFT tiers</span><h2>Priority Track</h2><p>Founder NFT plus future validator priority after monthly contribution.</p></article>
      </section>
      <section class="portal-card">
        <span class="label">Future NFT Verification</span>
        <p>Wallet Connect, MSC Wallet Login, NFT verification, and ambassador certificates are planned future upgrades.</p>
      </section>
    </main>`);
  }

  function validatorBenefitsPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Validator Benefits", "Future validator access for trusted ambassadors", "High-trust ambassadors can become early candidates for node operator education, governance participation, and validator access.")}
      <section class="module-grid">
        ${benefit("Future validator access", "Gold ambassadors can receive priority review when validator expansion opens.", "server")}
        ${benefit("Node operator benefits", "Operator guides, monitoring runbooks, and infrastructure readiness support.", "terminal")}
        ${benefit("Early ecosystem access", "Early product access for wallet, explorer, NFT, and community features.", "rocket")}
        ${benefit("Governance participation", "Reputation can support governance participation when voting upgrades ship.", "landmark")}
      </section>
      <section class="notice-panel">
        <span class="label">Important</span>
        <p>Validator access is a future program priority, not a guaranteed slot, income stream, or profit promise.</p>
      </section>
    </main>`);
  }

  function benefit(title, text, iconName) {
    return `<article class="portal-card"><span class="pill teal">${icon(iconName)}Benefit</span><h2>${esc(title)}</h2><p>${esc(text)}</p></article>`;
  }

  function adminPage() {
    if (!isAdmin()) {
      const loginFields = firebaseState.enabled ? `
          <div class="field"><label for="adminEmail">Admin Email</label><input id="adminEmail" name="adminEmail" type="email" autocomplete="username" value="${esc(ADMIN_EMAIL)}" required /></div>
          <div class="field"><label for="adminPassword">Password</label><input id="adminPassword" name="adminPassword" type="password" autocomplete="current-password" required /></div>
          <p class="muted">Email/password admin auth is locked to ${esc(ADMIN_EMAIL)}. Normal users cannot open this dashboard.</p>
          ${firebaseState.authNotice ? `<p class="form-message error">${esc(firebaseState.authNotice)}</p>` : ""}
          ${firebaseState.error ? `<p class="form-message error">${esc(firebaseState.error)}</p>` : ""}` : `
          <div class="field"><label for="adminPassword">Admin Key</label><input id="adminPassword" name="adminPassword" type="password" autocomplete="current-password" required /></div>
          <p class="muted">Local demo key: ${ADMIN_PASSWORD}. Set cloud config to use real admin login and database storage.</p>`;
      return shell(`<main class="portal-main">
        ${pageTitle("Admin Dashboard", "Application and reward management", "Admin access gates application approval, ambassador user management, reward state, and analytics.")}
        <form id="adminLoginForm" class="form-panel admin-lock" novalidate>
          <h2>Admin Login</h2>
          ${loginFields}
          <div class="action-row"><button class="btn primary" type="submit">${icon("unlock")}Login</button><span id="adminLoginMessage" class="form-message"></span></div>
        </form>
      </main>`);
    }
    const s = stats();
    return shell(`<main class="portal-main">
      ${pageTitle("Admin Dashboard", "Applications, users, rewards, and analytics", "Approve or reject ambassadors, review reward tiers, and monitor campaign health.")}
      <div class="admin-toolbar"><span class="pill teal">${icon("database")}${esc(dataSourceLabel())}</span><div class="action-row"><button class="btn" data-admin-refresh="true">${icon("refresh-cw")}Refresh</button><button class="btn" data-admin-logout="true">${icon("log-out")}Logout</button></div></div>
      <section class="stats-grid">
        ${statCard("Active Influencers", fmt(s.totalAmbassadors), `${fmt(db.ambassadors.length)} total profiles`)}
        ${statCard("Applications", fmt(s.totalApplications), `${fmt(s.pendingApplications)} pending`)}
        ${statCard("Normal Users", fmt(db.users.length), "referral joins")}
        ${statCard("Rewards", fmt(db.rewards.length), "assigned history")}
        ${statCard("Bug Reports", fmt(db.bugReports.length), `${fmt(db.bugReports.filter((item) => item.status === "submitted").length)} new`)}
      </section>
      <section class="table-panel">
        <h2>Application Management</h2>
        <div class="table-wrap"><table><thead><tr><th>ID</th><th>Creator</th><th>Followers</th><th>Level</th><th>Status</th><th>Actions</th></tr></thead><tbody>${db.applications.map(applicationAdminRow).join("")}</tbody></table></div>
      </section>
      <section class="table-panel">
        <h2>Bug Bounty Reports</h2>
        <div class="table-wrap"><table><thead><tr><th>ID</th><th>Bug</th><th>Category</th><th>Severity</th><th>Status</th><th>Reporter</th><th>Evidence</th><th>Actions</th></tr></thead><tbody>${db.bugReports.length ? db.bugReports.map(bugReportAdminRow).join("") : `<tr><td colspan="8">No bug reports submitted yet.</td></tr>`}</tbody></table></div>
      </section>
      <section class="table-panel">
        <h2>User Management</h2>
        <div class="table-wrap"><table><thead><tr><th>Influencer</th><th>Code</th><th>Level</th><th>Status</th><th>Login</th><th>Referrals</th><th>Reputation</th><th>Country</th><th>Actions</th></tr></thead><tbody>${rankedAmbassadors(true).map(userAdminRow).join("")}</tbody></table></div>
      </section>
      <section class="table-panel">
        <h2>Reward History</h2>
        <div class="table-wrap"><table><thead><tr><th>Date</th><th>Influencer</th><th>Type</th><th>MSC</th><th>Status</th></tr></thead><tbody>${db.rewards.slice(0, 12).map(rewardHistoryRow).join("") || `<tr><td colspan="5">No rewards assigned yet.</td></tr>`}</tbody></table></div>
      </section>
      <section class="module-grid">
        ${Object.keys(levelMap).map((key) => rewardAdminCard(key)).join("")}
      </section>
    </main>`);
  }

  function applicationAdminRow(item) {
    const level = levelMap[item.level] || levelMap.bronze;
    const disabled = item.status !== "pending" ? "disabled" : "";
    return `<tr>
      <td class="mono">${esc(item.id)}</td>
      <td><strong>${esc(item.fullName)}</strong><div>${esc(item.instagram)} | ${esc(item.country)}</div><div class="muted">${esc(item.email)}</div></td>
      <td>${fmt(item.followers)}</td>
      <td>${esc(level.short)}</td>
      <td><span class="status-pill ${esc(item.status)}">${esc(item.status)}</span></td>
      <td><div class="action-row"><button class="btn primary" data-admin-action="approve" data-app-id="${esc(item.id)}" ${disabled}>Approve</button><button class="btn danger" data-admin-action="reject" data-app-id="${esc(item.id)}" ${disabled}>Reject</button></div></td>
    </tr>`;
  }

  function bugReportAdminRow(item) {
    const status = String(item.status || "submitted").toLowerCase();
    const severity = String(item.severity || "low").toLowerCase();
    const evidence = String(item.evidenceUrl || "").trim();
    return `<tr>
      <td class="mono">${esc(item.id || "-")}</td>
      <td><strong>${esc(item.reportTitle || item.title || "-")}</strong><div class="muted">${esc((item.description || "").slice(0, 86))}${(item.description || "").length > 86 ? "..." : ""}</div></td>
      <td>${esc(item.category || "-")}</td>
      <td><span class="status-pill ${severity === "critical" || severity === "high" ? "rejected" : severity === "medium" ? "pending" : "approved"}">${esc(severity)}</span></td>
      <td><span class="status-pill ${status === "resolved" ? "approved" : status === "duplicate" || status === "rejected" ? "rejected" : "pending"}">${esc(status)}</span></td>
      <td><strong>${esc(item.reporterName || "-")}</strong><div class="muted">${esc(item.reporterEmail || "-")}</div></td>
      <td>${evidence ? `<a class="btn" href="${esc(evidence)}" target="_blank" rel="noreferrer">${icon("external-link")}Open</a>` : `<span class="muted">-</span>`}</td>
      <td><div class="action-row">
        <button class="btn primary" data-bug-action="in_review" data-report-id="${esc(item.id)}">Review</button>
        <button class="btn" data-bug-action="resolved" data-report-id="${esc(item.id)}">Resolve</button>
        <button class="btn" data-bug-action="duplicate" data-report-id="${esc(item.id)}">Duplicate</button>
        <button class="btn danger" data-bug-action="rejected" data-report-id="${esc(item.id)}">Reject</button>
      </div></td>
    </tr>`;
  }

  function userAdminRow(item) {
    const level = levelMap[item.level] || levelMap.bronze;
    const status = ambassadorStatus(item);
    const influencer = db.influencers.find((profile) => profile.code === item.code || profile.id === item.code) || {};
    const authStatus = influencer.authStatus || item.authStatus || "auth_user_required";
    const authReady = authStatus === "ready";
    return `<tr>
      <td><strong>${esc(item.name)}</strong><div>${badgeList(item)}</div></td>
      <td class="mono">${esc(item.code)}</td>
      <td>${esc(level.short)}</td>
      <td><span class="status-pill ${status === "active" ? "approved" : status === "banned" ? "rejected" : "pending"}">${esc(status)}</span></td>
      <td><span class="status-pill ${authReady ? "approved" : "pending"}">${authReady ? "Auth ready" : "Auth user required"}</span><div class="muted">${esc(influencer.email || item.email || "-")}</div></td>
      <td>${fmt(item.referrals)}</td>
      <td>${fmt(item.reputation)}</td>
      <td>${esc(item.country || "-")}</td>
      <td><div class="action-row">
        <button class="btn primary" data-ambassador-action="reward" data-ambassador-code="${esc(item.code)}">${icon("gift")}Reward</button>
        <button class="btn" data-ambassador-action="auth-ready" data-ambassador-code="${esc(item.code)}">Auth Ready</button>
        <button class="btn" data-ambassador-action="${status === "active" ? "suspend" : "activate"}" data-ambassador-code="${esc(item.code)}">${status === "active" ? "Suspend" : "Activate"}</button>
        <button class="btn danger" data-ambassador-action="ban" data-ambassador-code="${esc(item.code)}">Ban</button>
      </div></td>
    </tr>`;
  }

  function rewardHistoryRow(item) {
    return `<tr><td>${esc(item.createdAt || "-")}</td><td>${esc(item.influencerName || item.influencerCode || "-")}</td><td>${esc(item.type || "-")}</td><td>${fmt(item.amountMSC || 0)}</td><td><span class="status-pill pending">${esc(item.status || "locked")}</span></td></tr>`;
  }

  function rewardAdminCard(key) {
    const level = levelMap[key];
    return `<article class="portal-card"><span class="label">Reward Management</span><h2>${esc(level.name)}</h2><p>${fmt(level.allocation)} MSC locked allocation</p>${list(level.benefits)}</article>`;
  }

  function analyticsPage() {
    const s = stats();
    return shell(`<main class="portal-main">
      ${pageTitle("Analytics Panel", "Growth charts and KPI tracking", "Track ambassadors, applications, referrals, approval rate, followers, visits, wallet creations, Telegram joins, node operators, and validator applications.")}
      <section class="stats-grid">
        ${statCard("Total Ambassadors", fmt(s.totalAmbassadors))}
        ${statCard("Total Applications", fmt(s.totalApplications))}
        ${statCard("Total Referrals", fmt(s.totalReferrals))}
        ${statCard("Approval Rate", `${fmt(s.approvalRate)}%`)}
      </section>
      <section class="two-column">
        <div class="portal-card">
          <span class="label">KPI Tracking</span>
          <div class="chart-list">
            ${bar("Followers gained", percentOf(s.kpi.followersGained, 5000), fmt(s.kpi.followersGained))}
            ${bar("Website visits", percentOf(s.kpi.websiteVisits, 12000), fmt(s.kpi.websiteVisits))}
            ${bar("Wallet creations", percentOf(s.kpi.walletCreations, 1200), fmt(s.kpi.walletCreations))}
            ${bar("Telegram joins", percentOf(s.kpi.telegramJoins, 2000), fmt(s.kpi.telegramJoins))}
            ${bar("Node operators", percentOf(s.kpi.nodeOperators, 50), fmt(s.kpi.nodeOperators))}
            ${bar("Validator apps", percentOf(s.kpi.validatorApplications, 30), fmt(s.kpi.validatorApplications))}
          </div>
        </div>
        <div class="portal-card">
          <span class="label">Scaling Plan</span>
          <div class="chart-list">
            ${bar("Month 1", 10, "10 ambassadors")}
            ${bar("Month 2", 25, "25 ambassadors")}
            ${bar("Month 3", 50, "50 ambassadors")}
            ${bar("Month 6", 100, "100+ ambassadors")}
          </div>
        </div>
      </section>
    </main>`);
  }

  function percentOf(value, target) {
    const pct = Math.round((Number(value || 0) / target) * 100);
    return Math.max(2, Math.min(100, pct));
  }

  function bar(label, percent, value) {
    const safePercent = Math.max(0, Math.min(100, Number(percent) || 0));
    return `<div class="chart-row"><span>${esc(label)}</span><div class="bar-track"><div class="bar-fill" style="--bar:${safePercent}%"></div></div><strong>${esc(value)}</strong></div>`;
  }

  function announcementsPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Announcement Center", "MSC updates, campaigns, rewards, and partnerships", "Campaign updates, new rewards, partnership announcements, and compliance reminders live in one place.")}
      ${isAdmin() ? announcementForm() : ""}
      <section class="module-grid">${db.announcements.map((item) => `<article class="notice-panel"><span class="pill ${item.type === "Rewards" ? "gold" : "teal"}">${esc(item.type)}</span><h2>${esc(item.title)}</h2><p>${esc(item.body)}</p><span class="meta">${esc(item.date)}</span></article>`).join("")}</section>
    </main>`);
  }

  function announcementForm() {
    return `<form id="announcementForm" class="form-panel" novalidate>
      <h2>New Announcement</h2>
      <div class="form-grid">
        ${field("announcementType", "Type", "text", "MSC updates")}
        ${field("announcementTitle", "Title", "text", "Announcement title")}
        <div class="field full"><label for="announcementBody">Body</label><textarea id="announcementBody" name="announcementBody" required></textarea></div>
      </div>
      <div class="action-row"><button class="btn primary" type="submit">${icon("megaphone")}Publish</button><span id="announcementMessage" class="form-message"></span></div>
    </form>`;
  }

  function faqPage() {
    const faqs = [
      ["How to join?", "Submit the influencer application with your social links, follower count, portfolio, target audience, and planned MSC content."],
      ["How rewards work?", "Rewards are locked MSC allocations, Founder NFT eligibility, referral rewards, reputation points, and milestone bonuses. Rewards do not guarantee profit."],
      ["NFT details?", "Founder NFT eligibility starts at Silver and Gold levels after content quality, referral quality, and compliance review."],
      ["Referral details?", "Every verified user through an ambassador code can award example rewards of 10 MSC to the user and 5 MSC to the influencer, or reputation points."],
      ["How bug bounty rewards work?", "Bug bounty reports are reviewed by severity, impact, uniqueness, reproduction quality, and evidence. Duplicate reports may not be eligible."],
      ["How wallet referral code works?", "Approved ambassador links open the official MSC Wallet with ?ref=CODE. The wallet saves the code locally and includes it with supported wallet actions such as faucet requests."],
      ["Who should apply first?", "Crypto educators, blockchain developers, Web3 influencers, gaming creators, and tech pages with 1,000 to 20,000 followers."],
      ["Can MSC remove status?", "Yes. Fake followers, bot traffic, misleading financial claims, undisclosed sponsored content, or abuse can remove status."],
    ];
    return shell(`<main class="portal-main">
      ${pageTitle("FAQ", "Program answers and policies", "Joining, rewards, Founder NFT details, referrals, eligibility, and compliance.")}
      <section class="module-grid">${faqs.map(([q, a]) => `<article class="faq-item"><h2>${esc(q)}</h2><p class="muted">${esc(a)}</p></article>`).join("")}</section>
    </main>`);
  }

  function contactPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Contact Center", "Ambassador support and community links", "Send program questions, partnership requests, or support messages to the MSC Ambassador team.")}
      <section class="wide-two-column">
        <form id="contactForm" class="form-panel" novalidate>
          <h2>Contact Form</h2>
          <div class="form-grid">
            ${field("contactName", "Name", "text", "Your name")}
            ${field("contactEmail", "Email", "email", "name@example.com")}
            <div class="field full"><label for="contactTopic">Topic</label><select id="contactTopic" name="contactTopic"><option>Application</option><option>Referral</option><option>Founder NFT</option><option>Partnership</option><option>Support</option></select></div>
            <div class="field full"><label for="contactMessageText">Message</label><textarea id="contactMessageText" name="contactMessageText" required></textarea></div>
          </div>
          <div class="action-row"><button class="btn primary" type="submit">${icon("mail")}Send Message</button><span id="contactMessage" class="form-message"></span></div>
        </form>
        <aside class="portal-card">
          <span class="label">Social Links</span>
          <a class="row-item" href="#"><span>Telegram</span><strong>@MSCChain</strong></a>
          <a class="row-item" href="#"><span>Discord</span><strong>MSC Community</strong></a>
          <a class="row-item" href="mailto:support@mscchain.local"><span>Email support</span><strong>support@mscchain.local</strong></a>
        </aside>
      </section>
    </main>`);
  }

  function securityPage() {
    return shell(`<main class="portal-main">
      ${pageTitle("Security", "Validation, storage, spam protection, and roadmap", "The portal applies client validation now and names the future server-side upgrades needed for production.")}
      <section class="module-grid">
        ${securityFeature("Admin authentication", `Email/password auth is locked to ${ADMIN_EMAIL}. Normal users cannot open the admin dashboard. Local demo mode keeps the temporary key only for development.`, "lock")}
        ${securityFeature("Role model", "MSC keeps 1 Super Admin, unlimited approved influencers, and unlimited normal users. Influencer and user records are separated from the public leaderboard.", "users")}
        ${securityFeature("Form validation", "Applications validate required fields, email format, follower range, agreement, and profile completeness.", "list-checks")}
        ${securityFeature("Database storage", `Cloud database mode stores real applications, ambassadors, referrals, bug reports, announcements, and contact messages in ${dataSourceLabel()}.`, "database")}
        ${securityFeature("Spam protection", "Honeypot field, submit throttling, duplicate referral email checks, and abuse review rules are active.", "shield-alert")}
        ${securityFeature("Real wallet handoff", "Approved ambassador codes open the official MSC Wallet with a referral parameter. Rewards still require admin review and no guaranteed profit is promised.", "lock-keyhole")}
      </section>
      <section class="two-column">
        <div class="notice-panel">
          <span class="label">Partnership Agreement</span>
          ${list(["No fake followers", "No bot traffic", "No misleading financial claims", "Must disclose sponsored partnership if required by local laws", "MSC reserves right to remove ambassador status"])}
        </div>
        <div class="notice-panel">
          <span class="label">Future Upgrades</span>
          ${list(["Wallet Connect", "MSC Wallet Login", "On-chain Reputation", "NFT Verification", "Ambassador Certificates", "Automated Reward Distribution", "Node Runner Verification", "Duplicate IP checks with Cloud Functions", "Governance Voting"])}
        </div>
      </section>
    </main>`);
  }

  function securityFeature(title, text, iconName) {
    return `<article class="portal-card"><span class="pill violet">${icon(iconName)}Security</span><h2>${esc(title)}</h2><p>${esc(text)}</p></article>`;
  }

  const renderers = {
    home: homePage,
    program: programPage,
    rewards: rewardsPage,
    apply: applyPage,
    leaderboard: leaderboardPage,
    profiles: profilesPage,
    influencer: influencerPage,
    referrals: referralsPage,
    "bug-bounty": bugBountyPage,
    nft: nftPage,
    "validator-benefits": validatorBenefitsPage,
    admin: adminPage,
    analytics: analyticsPage,
    announcements: announcementsPage,
    faq: faqPage,
    contact: contactPage,
    security: securityPage,
  };

  function render() {
    document.body.innerHTML = (renderers[PAGE] || homePage)();
    bindUI();
    if (window.lucide) window.lucide.createIcons();
  }

  function bindUI() {
    const applicationForm = $("applicationForm");
    if (applicationForm) applicationForm.addEventListener("submit", submitApplication);

    const referralForm = $("referralForm");
    if (referralForm) referralForm.addEventListener("submit", submitReferral);

    const bugReportForm = $("bugReportForm");
    if (bugReportForm) bugReportForm.addEventListener("submit", submitBugReport);

    const adminLoginForm = $("adminLoginForm");
    if (adminLoginForm) adminLoginForm.addEventListener("submit", submitAdminLogin);

    const influencerLoginForm = $("influencerLoginForm");
    if (influencerLoginForm) influencerLoginForm.addEventListener("submit", submitInfluencerLogin);

    const contactForm = $("contactForm");
    if (contactForm) contactForm.addEventListener("submit", submitContact);

    const announcement = $("announcementForm");
    if (announcement) announcement.addEventListener("submit", submitAnnouncement);

    document.querySelectorAll("[data-admin-action]").forEach((button) => {
      button.addEventListener("click", () => handleAdminAction(button.dataset.adminAction, button.dataset.appId));
    });

    document.querySelectorAll("[data-ambassador-action]").forEach((button) => {
      button.addEventListener("click", () => handleAmbassadorAction(button.dataset.ambassadorAction, button.dataset.ambassadorCode));
    });

    document.querySelectorAll("[data-bug-action]").forEach((button) => {
      button.addEventListener("click", () => handleBugReportAction(button.dataset.bugAction, button.dataset.reportId));
    });

    document.querySelectorAll("[data-admin-logout]").forEach((button) => {
      button.addEventListener("click", async () => {
        if (remoteReady() && firebaseState.auth) {
          await firebaseState.modules.authMod.signOut(firebaseState.auth);
          firebaseState.admin = false;
          firebaseState.user = null;
        } else {
          sessionStorage.removeItem(ADMIN_SESSION_KEY);
        }
        render();
      });
    });

    document.querySelectorAll("[data-influencer-logout]").forEach((button) => {
      button.addEventListener("click", async () => {
        if (remoteReady() && firebaseState.auth) await firebaseState.modules.authMod.signOut(firebaseState.auth);
        firebaseState.influencer = null;
        firebaseState.influencerReferrals = [];
        firebaseState.influencerRewards = [];
        render();
      });
    });

    document.querySelectorAll("[data-admin-refresh]").forEach((button) => {
      button.addEventListener("click", async () => {
        await refreshRemoteData();
        render();
      });
    });
  }

  function setMessage(id, text, type = "") {
    const node = $(id);
    if (!node) return;
    node.textContent = text;
    node.className = `form-message ${type}`.trim();
  }

  async function submitApplication(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form).entries());
    const errors = validateApplication(data);
    if (errors.length) {
      setMessage("applicationMessage", errors[0], "error");
      return;
    }
    const last = Number(localStorage.getItem(LAST_SUBMIT_KEY) || 0);
    if (Date.now() - last < 8000) {
      setMessage("applicationMessage", "Please wait a few seconds before submitting again.", "error");
      return;
    }
    localStorage.setItem(LAST_SUBMIT_KEY, String(Date.now()));
    const payload = {
      fullName: data.fullName.trim(),
      email: data.email.trim().toLowerCase(),
      country: data.country.trim(),
      instagram: data.instagram.trim(),
      followers: Number(data.followers),
      links: data.links.trim(),
      portfolio: data.portfolio.trim(),
      reason: data.reason.trim(),
      level: data.level,
      audience: data.audience,
      status: "pending",
      createdAt: today(),
      createdAtMs: Date.now(),
      referralCode: makeCode(data.fullName),
    };
    try {
      let saved;
      if (remoteReady()) {
        saved = await addRemoteDoc("applications", payload);
        await refreshRemoteData();
      } else {
        saved = { id: `APP-${Date.now().toString().slice(-6)}`, ...payload };
        db.applications.unshift(saved);
        saveState();
      }
      form.reset();
      setMessage("applicationMessage", `Application ${saved.id} submitted for review.`, "success");
    } catch (err) {
      setMessage("applicationMessage", `Database write failed: ${err?.message || "check database rules"}`, "error");
    }
  }

  function validateApplication(data) {
    const errors = [];
    const followers = Number(data.followers);
    if ((data.website || "").trim()) errors.push("Spam protection blocked this submission.");
    if (!data.fullName || data.fullName.trim().length < 2) errors.push("Full name is required.");
    if (!emailOK(data.email)) errors.push("Valid email is required.");
    if (!data.country || data.country.trim().length < 2) errors.push("Country is required.");
    if (!data.instagram || !/^@?[a-z0-9._]{2,30}$/i.test(data.instagram.trim())) errors.push("Valid Instagram username is required.");
    if (!Number.isFinite(followers) || followers < 1000 || followers > 20000) errors.push("Followers count must be between 1,000 and 20,000 for the first phase.");
    if (!data.links || data.links.trim().length < 8) errors.push("YouTube or X link is required.");
    if (!data.reason || data.reason.trim().length < 40) errors.push("Please explain why you want to join MSC in at least 40 characters.");
    if (data.agreement !== "on") errors.push("Partnership agreement is required.");
    return errors;
  }

  function emailOK(value) {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(value || "").trim());
  }

  function makeCode(name) {
    const base = String(name || "MSC").toUpperCase().replace(/[^A-Z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 12) || "MSC";
    const suffix = Math.random().toString(36).slice(2, 5).toUpperCase();
    return `${base}-${suffix}`;
  }

  async function submitReferral(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form).entries());
    if (!data.referralCode || !data.referralUser || !emailOK(data.referralEmail)) {
      setMessage("referralMessage", "Valid referral code, user name, and email are required.", "error");
      return;
    }
    if (data.referralAgreement !== "on") {
      setMessage("referralMessage", "Referral abuse agreement is required.", "error");
      return;
    }
    const ambassador = db.ambassadors.find((item) => item.code === data.referralCode);
    if (!ambassador) {
      setMessage("referralMessage", "Referral code not found.", "error");
      return;
    }
    try {
      const normalizedEmail = data.referralEmail.trim().toLowerCase();
      const emailHash = await emailFingerprint(normalizedEmail);
      const duplicate = await referralEmailExists(normalizedEmail, emailHash);
      if (duplicate) {
        setMessage("referralMessage", "This email is already tracked as a referral.", "error");
        return;
      }
      const publicPayload = {
        code: data.referralCode,
        emailHash,
        rewardUser: 10,
        rewardAmbassador: 5,
        points: 20,
        createdAt: today(),
        createdAtMs: Date.now(),
      };
      const privatePayload = {
        code: data.referralCode,
        userName: data.referralUser.trim(),
        email: normalizedEmail,
        emailHash,
        createdAt: publicPayload.createdAt,
        createdAtMs: publicPayload.createdAtMs,
      };
      const userPayload = {
        name: data.referralUser.trim(),
        email: normalizedEmail,
        emailHash,
        referredBy: data.referralCode,
        rewardMSC: 10,
        role: "normal_user",
        joinDate: publicPayload.createdAt,
        joinDateMs: publicPayload.createdAtMs,
        status: "joined",
      };
      if (remoteReady()) {
        await addRemoteReferral(publicPayload, privatePayload, userPayload);
        if (firebaseState.mode === "firestore") {
          const { doc, increment, updateDoc } = firebaseState.modules.firestoreMod;
          try {
            await updateDoc(doc(firebaseState.firestore, collectionName("ambassadors"), data.referralCode), {
              referrals: increment(1),
              monthlyReferrals: increment(1),
              reputation: increment(20),
              walletCreations: increment(1),
            });
          } catch (err) {
            console.warn("Referral saved; ambassador counter update skipped by database rules.", err);
          }
        }
        await refreshRemoteData();
      } else {
        db.referrals.push({ ...publicPayload, ...privatePayload });
        db.users.push(userPayload);
        ambassador.referrals = Number(ambassador.referrals || 0) + 1;
        ambassador.monthlyReferrals = Number(ambassador.monthlyReferrals || 0) + 1;
        ambassador.reputation = Number(ambassador.reputation || 0) + 20;
        ambassador.walletCreations = Number(ambassador.walletCreations || 0) + 1;
        saveState();
      }
      form.reset();
      setMessage("referralMessage", "Referral recorded and leaderboard updated.", "success");
    } catch (err) {
      setMessage("referralMessage", `Database write failed: ${err?.message || "check database rules"}`, "error");
    }
  }

  async function referralEmailExists(email, emailHash) {
    const normalized = String(email || "").trim().toLowerCase();
    if (!remoteReady()) {
      return db.referrals.some((item) => item.emailHash === emailHash || String(item.email || "").toLowerCase() === normalized);
    }
    if (firebaseState.mode === "realtime") {
      return db.referrals.some((item) => item.emailHash === emailHash);
    }
    const { collection, getDocs, limit, query, where } = firebaseState.modules.firestoreMod;
    const q = query(
      collection(firebaseState.firestore, collectionName("referrals")),
      where("emailHash", "==", emailHash),
      limit(1),
    );
    const snap = await getDocs(q);
    return !snap.empty;
  }

  async function emailFingerprint(email) {
    const normalized = String(email || "").trim().toLowerCase();
    if (window.crypto?.subtle && window.TextEncoder) {
      const bytes = new window.TextEncoder().encode(normalized);
      const digest = await window.crypto.subtle.digest("SHA-256", bytes);
      return Array.from(new Uint8Array(digest)).map((byte) => byte.toString(16).padStart(2, "0")).join("");
    }
    let hash = 2166136261;
    for (let i = 0; i < normalized.length; i += 1) {
      hash ^= normalized.charCodeAt(i);
      hash = Math.imul(hash, 16777619);
    }
    return `fallback-${(hash >>> 0).toString(16)}`;
  }

  async function submitBugReport(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form).entries());
    const errors = validateBugReport(data);
    if (errors.length) {
      setMessage("bugReportMessage", errors[0], "error");
      return;
    }
    const last = Number(localStorage.getItem(LAST_BUG_REPORT_KEY) || 0);
    if (Date.now() - last < 12000) {
      setMessage("bugReportMessage", "Please wait a few seconds before submitting another report.", "error");
      return;
    }
    localStorage.setItem(LAST_BUG_REPORT_KEY, String(Date.now()));
    const payload = {
      reportTitle: data.bugTitle.trim(),
      category: data.bugCategory,
      severity: data.bugSeverity,
      description: data.bugDescription.trim(),
      stepsToReproduce: data.stepsToReproduce.trim(),
      expectedResult: data.expectedResult.trim(),
      actualResult: data.actualResult.trim(),
      evidenceUrl: String(data.evidenceUrl || "").trim(),
      environment: data.environment.trim(),
      nodeVersion: String(data.nodeVersion || "").trim(),
      reporterName: data.reporterName.trim(),
      reporterEmail: data.reporterEmail.trim().toLowerCase(),
      status: "submitted",
      source: "testnet_bug_bounty",
      createdAt: today(),
      createdAtMs: Date.now(),
    };
    try {
      let saved;
      if (remoteReady()) {
        saved = await addRemoteDoc("bug_reports", payload);
        await refreshRemoteData();
      } else {
        saved = { id: `BUG-${Date.now().toString().slice(-6)}`, ...payload };
        db.bugReports.unshift(saved);
        saveState();
      }
      form.reset();
      setMessage("bugReportMessage", `Bug report ${saved.id} submitted for MSC review.`, "success");
    } catch (err) {
      setMessage("bugReportMessage", `Database write failed: ${err?.message || "check database rules"}`, "error");
    }
  }

  function validateBugReport(data) {
    const errors = [];
    const categoryOK = ["MSC Wallet", "MSC Explorer", "MSC Blockchain", "Security", "Performance", "RPC / API"].includes(data.bugCategory);
    const severityOK = ["low", "medium", "high", "critical"].includes(data.bugSeverity);
    const evidence = String(data.evidenceUrl || "").trim();
    if ((data.bugWebsite || "").trim()) errors.push("Spam protection blocked this report.");
    if (!data.bugTitle || data.bugTitle.trim().length < 8) errors.push("Bug title must be at least 8 characters.");
    if (!categoryOK) errors.push("Valid category is required.");
    if (!severityOK) errors.push("Valid severity is required.");
    if (!data.reporterName || data.reporterName.trim().length < 2) errors.push("Reporter name is required.");
    if (!emailOK(data.reporterEmail)) errors.push("Valid reporter email is required.");
    if (evidence && !/^https?:\/\/\S{6,}$/i.test(evidence)) errors.push("Evidence link must start with http:// or https://.");
    if (!data.environment || data.environment.trim().length < 6) errors.push("Browser / OS / device details are required.");
    if (!data.bugDescription || data.bugDescription.trim().length < 30) errors.push("Description must be at least 30 characters.");
    if (!data.stepsToReproduce || data.stepsToReproduce.trim().length < 30) errors.push("Steps to reproduce must be at least 30 characters.");
    if (!data.expectedResult || data.expectedResult.trim().length < 10) errors.push("Expected result is required.");
    if (!data.actualResult || data.actualResult.trim().length < 10) errors.push("Actual result is required.");
    if (data.bugAgreement !== "on") errors.push("Original report confirmation is required.");
    return errors;
  }

  async function submitAdminLogin(event) {
    event.preventDefault();
    if (firebaseState.enabled) {
      if (!remoteReady()) {
        setMessage("adminLoginMessage", "Database is still connecting or config is invalid.", "error");
        return;
      }
      const email = String($("adminEmail")?.value || "").trim().toLowerCase();
      const password = $("adminPassword")?.value || "";
      if (!isSingleAdminEmail(email)) {
        setMessage("adminLoginMessage", `Only ${ADMIN_EMAIL} can access this dashboard.`, "error");
        return;
      }
      try {
        const { signInWithEmailAndPassword, signOut } = firebaseState.modules.authMod;
        const cred = await signInWithEmailAndPassword(firebaseState.auth, email, password);
        const admin = await checkFirebaseAdmin(cred.user);
        if (!admin) {
          await signOut(firebaseState.auth);
          setMessage("adminLoginMessage", `Only ${ADMIN_EMAIL} can access this dashboard.`, "error");
          return;
        }
        firebaseState.user = cred.user;
        firebaseState.admin = true;
        firebaseState.authNotice = "";
        await refreshRemoteData();
        render();
      } catch (err) {
        setMessage("adminLoginMessage", err?.message || "Admin login failed.", "error");
      }
      return;
    }
    const password = $("adminPassword")?.value || "";
    if (password !== ADMIN_PASSWORD) {
      setMessage("adminLoginMessage", "Invalid admin key.", "error");
      return;
    }
    sessionStorage.setItem(ADMIN_SESSION_KEY, "active");
    render();
  }

  async function submitInfluencerLogin(event) {
    event.preventDefault();
    if (!firebaseState.enabled || !remoteReady()) {
      setMessage("influencerLoginMessage", "Cloud auth is still connecting.", "error");
      return;
    }
    const email = String($("influencerEmail")?.value || "").trim().toLowerCase();
    const password = $("influencerPassword")?.value || "";
    const code = String($("influencerCode")?.value || "").trim().toUpperCase();
    if (!emailOK(email) || !password || !code) {
      setMessage("influencerLoginMessage", "Valid email, password, and referral code are required.", "error");
      return;
    }
    try {
      const { signInWithEmailAndPassword, signOut } = firebaseState.modules.authMod;
      const cred = await signInWithEmailAndPassword(firebaseState.auth, email, password);
      if (isSingleAdminEmail(cred.user.email)) {
        await signOut(firebaseState.auth);
        setMessage("influencerLoginMessage", "Use the admin dashboard for the super admin account.", "error");
        return;
      }
      const profile = firebaseState.mode === "firestore"
        ? await readInfluencerProfileFirestore(code)
        : await readRemotePath(`${collectionName("influencers")}/${code}`);
      if (!profile || String(profile.email || "").toLowerCase() !== email) {
        await signOut(firebaseState.auth);
        setMessage("influencerLoginMessage", "Influencer profile not found for this email and referral code.", "error");
        return;
      }
      firebaseState.influencer = { id: code, ...profile };
      firebaseState.influencerReferrals = await readInfluencerReferrals(code);
      firebaseState.influencerRewards = await readInfluencerRewards(code);
      render();
    } catch (err) {
      setMessage("influencerLoginMessage", err?.message || "Influencer login failed.", "error");
    }
  }

  async function readInfluencerProfileFirestore(code) {
    const { doc, getDoc } = firebaseState.modules.firestoreMod;
    const snap = await getDoc(doc(firebaseState.firestore, collectionName("influencers"), code));
    return snap.exists() ? snap.data() : null;
  }

  async function handleAdminAction(action, appId) {
    const item = db.applications.find((app) => app.id === appId);
    if (!item || item.status !== "pending") return;
    if (remoteReady()) {
      try {
        const nextStatus = action === "approve" ? "approved" : "rejected";
        await updateRemoteDoc("applications", appId, {
          status: nextStatus,
          reviewedAt: today(),
          reviewedAtMs: Date.now(),
          reviewedBy: firebaseState.user?.uid || "admin",
        });
        if (action === "approve") {
          const publicProfile = buildPublicAmbassadorProfile(item);
          const influencerProfile = buildInfluencerProfile(item);
          await setRemoteDoc("ambassadors", item.referralCode, publicProfile);
          await setRemoteDoc("influencers", item.referralCode, influencerProfile);
          await setRemoteDoc("referral_codes", item.referralCode, {
            code: item.referralCode,
            influencerId: item.referralCode,
            influencerEmail: item.email,
            status: "active",
            createdAt: publicProfile.approvedAt,
            createdAtMs: publicProfile.approvedAtMs,
          });
        }
        await refreshRemoteData();
        render();
      } catch (err) {
        alert(`Admin update failed: ${err?.message || "check database rules"}`);
      }
      return;
    }
    if (action === "approve") {
      item.status = "approved";
      const exists = db.ambassadors.some((ambassador) => ambassador.code === item.referralCode);
      if (!exists) {
        db.ambassadors.push(buildPublicAmbassadorProfile(item));
        db.influencers.push(buildInfluencerProfile(item));
      }
    }
    if (action === "reject") item.status = "rejected";
    saveState();
    render();
  }

  function buildPublicAmbassadorProfile(item) {
    const approvedAtMs = Date.now();
    return {
      name: item.fullName,
      username: item.instagram,
      country: item.country,
      level: item.level,
      code: item.referralCode,
      followers: Number(item.followers || 0),
      referrals: 0,
      monthlyReferrals: 0,
      reputation: 100,
      followersGained: 0,
      websiteVisits: 0,
      walletCreations: 0,
      telegramJoins: 0,
      nodeOperators: 0,
      validatorApplications: 0,
      audience: item.audience,
      status: "active",
      verifiedBadge: true,
      founderNFTBadge: item.level === "silver" || item.level === "gold",
      applicationId: item.id,
      approvedAt: today(),
      approvedAtMs,
    };
  }

  function buildInfluencerProfile(item) {
    const profile = buildPublicAmbassadorProfile(item);
    return {
      ...profile,
      role: "influencer",
      email: item.email,
      links: item.links || "",
      portfolio: item.portfolio || "",
      rewardHistoryCount: 0,
      profileCreated: false,
      authProvider: "email_password",
      authStatus: "auth_user_required",
      loginUrl: "influencer.html",
    };
  }

  async function handleAmbassadorAction(action, code) {
    const item = db.ambassadors.find((ambassador) => ambassador.code === code);
    if (!item) return;
    try {
      if (action === "auth-ready") {
        const patch = {
          authStatus: "ready",
          authProvider: "email_password",
          authReadyAt: today(),
          authReadyAtMs: Date.now(),
          authReadyBy: firebaseState.user?.uid || "admin",
        };
        if (remoteReady()) {
          await updateRemoteDoc("influencers", code, patch);
          await updateRemoteDoc("ambassadors", code, patch);
          await refreshRemoteData();
        } else {
          Object.assign(item, patch);
          const influencer = db.influencers.find((profile) => profile.code === code || profile.id === code);
          if (influencer) Object.assign(influencer, patch);
          saveState();
        }
        render();
        return;
      }

      if (action === "reward") {
        const reward = buildRewardPayload(item);
        if (remoteReady()) {
          const savedReward = await addRemoteDoc("rewards", reward);
          const influencerRewardId = firebaseState.mode === "firestore" ? `${item.code}_${savedReward.id}` : `${item.code}/${savedReward.id}`;
          await setRemoteDoc("influencer_rewards", influencerRewardId, { ...savedReward, code: item.code });
          await updateRemoteDoc("ambassadors", code, {
            lastRewardAt: reward.createdAt,
            rewardHistoryCount: Number(item.rewardHistoryCount || 0) + 1,
          });
          await updateRemoteDoc("influencers", code, {
            lastRewardAt: reward.createdAt,
            rewardHistoryCount: Number(item.rewardHistoryCount || 0) + 1,
          });
          await refreshRemoteData();
        } else {
          db.rewards.unshift(reward);
          item.lastRewardAt = reward.createdAt;
          item.rewardHistoryCount = Number(item.rewardHistoryCount || 0) + 1;
          saveState();
        }
        render();
        return;
      }

      const nextStatus = action === "ban" ? "banned" : action === "suspend" ? "suspended" : "active";
      const patch = {
        status: nextStatus,
        statusUpdatedAt: today(),
        statusUpdatedAtMs: Date.now(),
        statusUpdatedBy: firebaseState.user?.uid || "admin",
      };
      if (remoteReady()) {
        await updateRemoteDoc("ambassadors", code, patch);
        await updateRemoteDoc("influencers", code, patch);
        await updateRemoteDoc("referral_codes", code, { status: nextStatus });
        await refreshRemoteData();
      } else {
        item.status = nextStatus;
        const influencer = db.influencers.find((profile) => profile.code === code || profile.id === code);
        if (influencer) Object.assign(influencer, patch);
        saveState();
      }
      render();
    } catch (err) {
      alert(`Influencer update failed: ${err?.message || "check database rules"}`);
    }
  }

  async function handleBugReportAction(action, reportId) {
    if (!isAdmin()) return;
    const item = db.bugReports.find((report) => report.id === reportId);
    if (!item) return;
    const allowed = ["in_review", "resolved", "duplicate", "rejected"];
    if (!allowed.includes(action)) return;
    const patch = {
      status: action,
      reviewedAt: today(),
      reviewedAtMs: Date.now(),
      reviewedBy: firebaseState.user?.uid || "admin",
    };
    try {
      if (remoteReady()) {
        await updateRemoteDoc("bug_reports", reportId, patch);
        await refreshRemoteData();
      } else {
        Object.assign(item, patch);
        saveState();
      }
      render();
    } catch (err) {
      alert(`Bug report update failed: ${err?.message || "check database rules"}`);
    }
  }

  function buildRewardPayload(item) {
    const level = levelMap[item.level] || levelMap.bronze;
    return {
      influencerCode: item.code,
      influencerName: item.name,
      influencerEmail: db.influencers.find((profile) => profile.code === item.code || profile.id === item.code)?.email || "",
      type: "locked_allocation",
      amountMSC: level.allocation,
      founderNFTEligible: item.level === "silver" || item.level === "gold",
      verifiedAmbassadorBadge: true,
      status: "locked",
      vesting: "25% after 6 months, 25% after 12 months, 50% after mainnet milestones",
      createdAt: today(),
      createdAtMs: Date.now(),
      assignedBy: firebaseState.user?.uid || "admin",
    };
  }

  async function submitContact(event) {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget).entries());
    if (!data.contactName || !emailOK(data.contactEmail) || !data.contactMessageText || data.contactMessageText.trim().length < 10) {
      setMessage("contactMessage", "Valid name, email, and message are required.", "error");
      return;
    }
    const payload = {
      name: data.contactName.trim(),
      email: data.contactEmail.trim().toLowerCase(),
      topic: data.contactTopic || "Support",
      message: data.contactMessageText.trim(),
      createdAt: today(),
      createdAtMs: Date.now(),
    };
    try {
      if (remoteReady()) {
        await addRemoteDoc("contacts", payload);
        await refreshRemoteData();
      } else {
        db.contacts.push(payload);
        saveState();
      }
      event.currentTarget.reset();
      setMessage("contactMessage", "Message saved for ambassador support review.", "success");
    } catch (err) {
      setMessage("contactMessage", `Database write failed: ${err?.message || "check database rules"}`, "error");
    }
  }

  async function submitAnnouncement(event) {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget).entries());
    if (!data.announcementType || !data.announcementTitle || !data.announcementBody) {
      setMessage("announcementMessage", "Type, title, and body are required.", "error");
      return;
    }
    const payload = {
      type: data.announcementType.trim(),
      title: data.announcementTitle.trim(),
      body: data.announcementBody.trim(),
      date: today(),
      createdAtMs: Date.now(),
      publishedBy: firebaseState.user?.uid || "admin",
    };
    try {
      if (remoteReady()) {
        await addRemoteDoc("announcements", payload);
        await refreshRemoteData();
      } else {
        db.announcements.unshift(payload);
        saveState();
      }
      render();
    } catch (err) {
      setMessage("announcementMessage", `Database write failed: ${err?.message || "check database rules"}`, "error");
    }
  }

  render();
  initFirebaseData();
})();
