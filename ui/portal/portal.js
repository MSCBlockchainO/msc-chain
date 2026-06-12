(() => {
  "use strict";

  const PAGE = document.body.dataset.page || "home";
  const CHAIN_ID = "91938";
  const GENESIS_HASH = "d6d7d96ea1a70d2aca31389ce7ef7953794ce77b4c933828295269702768fa3c";
  const VERSION = "v1.0.0-mainnet";
  const TTL = {
    status: 8000,
    publicStatus: 10000,
    validators: 45000,
    campaign: 45000,
    publicNodes: 15000,
    lb: 10000,
  };

  const state = {
    cache: new Map(),
    status: null,
    publicStatus: null,
    validators: null,
    campaign: null,
    publicNodes: null,
    lb: null,
    realtime: {
      connected: false,
      height: 0,
      finalized: 0,
      cmd: "-",
      lastBlockAge: null,
      lastBlockBaseMs: 0,
      eventDelayMs: null,
    },
  };

  const pages = [
    ["home", "Home", "index.html"],
    ["testnet", "Testnet", "testnet.html"],
    ["explorer", "Explorer", "explorer.html"],
    ["validators", "Validators", "validators.html"],
    ["node-setup", "Node Setup", "node-setup.html"],
    ["docs", "Docs", "docs.html"],
    ["community", "Community", "community.html"],
    ["campaign", "Campaign", "campaign.html"],
    ["status", "Status", "status.html"],
    ["transparency", "Transparency", "transparency.html"],
    ["contact", "Contact", "contact.html"],
  ];

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
  const pct = (bps) => {
    const n = Number(bps);
    if (!Number.isFinite(n)) return "-";
    return `${(n / 100).toFixed(2)}%`;
  };
  const secondsAge = () => {
    if (state.realtime.lastBlockAge === null) return null;
    return Math.max(0, state.realtime.lastBlockAge + (Date.now() - state.realtime.lastBlockBaseMs) / 1000);
  };
  const ageText = (seconds) => {
    const n = Number(seconds);
    if (!Number.isFinite(n)) return "-";
    if (n < 60) return `${n.toFixed(1)}s`;
    if (n < 3600) return `${Math.round(n / 60)}m`;
    return `${Math.round(n / 3600)}h`;
  };
  const blockAgeTone = (seconds) => {
    const n = Number(seconds);
    if (!Number.isFinite(n) || n < 0) return "";
    if (n >= 15) return "bad";
    if (n >= 10) return "warn";
    return "good";
  };
  const unwrap = (data) => data && data.success && data.data !== undefined ? data.data : data;

  async function fetchJSON(path, key, ttl) {
    const cached = state.cache.get(key);
    if (cached && Date.now() - cached.ts < ttl) return cached.data;
    const res = await fetch(path, { cache: "no-store" });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`.trim());
    const data = unwrap(await res.json());
    state.cache.set(key, { ts: Date.now(), data });
    return data;
  }

  function setText(id, value) {
    const node = $(id);
    if (node) node.textContent = value ?? "-";
  }

  function setTone(id, tone = "") {
    const node = $(id);
    if (!node) return;
    node.classList.toggle("good", tone === "good");
    node.classList.toggle("warn", tone === "warn");
    node.classList.toggle("bad", tone === "bad");
  }

  function navHTML() {
    return pages.map(([key, label, href]) =>
      `<a class="${key === PAGE ? "active" : ""}" href="${href}">${label}</a>`).join("");
  }

  function shellHTML(content) {
    const mobile = [
      ["home", "Home", "index.html"],
      ["explorer", "Explorer", "explorer.html"],
      ["validators", "Validators", "validators.html"],
      ["docs", "Docs", "docs.html"],
      ["status", "More", "status.html"],
    ].map(([key, label, href]) => `<a class="${key === PAGE ? "active" : ""}" href="${href}">${label}</a>`).join("");

    return `
      <div class="portal-shell">
        <header class="portal-header">
          <nav class="portal-nav" aria-label="MSC Chain Portal">
            <a class="brand" href="index.html">
              <span class="brand-mark">MSC</span>
              <span>
                <span class="brand-title">MSC Chain</span>
                <span class="brand-subtitle">Layer-1 infrastructure</span>
              </span>
            </a>
            <div class="nav-links">${navHTML()}</div>
            <div class="nav-actions">
              <input id="portalSearch" class="search-box" type="search" placeholder="Search tx / address / block" />
              <a class="btn primary" href="node-setup.html">Run Validator</a>
            </div>
          </nav>
        </header>
        ${content}
        <footer class="footer">
          <div class="footer-inner">
            <div class="brand">
              <span class="brand-mark">MSC</span>
              <span>
                <span class="brand-title">MSC Chain</span>
                <span class="brand-subtitle">Copyright MSC Chain</span>
              </span>
            </div>
            <div class="footer-links">
              <a href="explorer.html">Explorer</a>
              <a href="validators.html">Validators</a>
              <a href="docs.html">Docs</a>
              <a href="community.html">GitHub</a>
              <a href="community.html">Discord</a>
              <a href="community.html">Telegram</a>
              <a href="status.html">Status</a>
              <a href="contact.html">Contact</a>
            </div>
          </div>
        </footer>
        <nav class="mobile-nav" aria-label="Mobile portal navigation">${mobile}</nav>
      </div>`;
  }

  function metricCard(label, id, fallback = "-") {
    return `<div class="card"><div class="label">${label}</div><div id="${id}" class="value">${fallback}</div></div>`;
  }

  function pageTitle(eyebrow, title, lead) {
    return `<section class="page-title"><div class="eyebrow">${eyebrow}</div><h1>${title}</h1><p class="lead">${lead}</p></section>`;
  }

  function homePage() {
    return shellHTML(`
      <main>
        <section class="hero">
          <div class="hero-inner">
            <div class="hero-copy">
              <div class="eyebrow">MSC Chain</div>
              <h1>Decentralized Layer-1 Infrastructure</h1>
              <p class="lead">Run a validator, join the testnet, and help secure a public network built for transparent operations.</p>
              <div class="cta-row">
                <a class="btn primary" href="node-setup.html">Run Validator</a>
                <a class="btn" href="campaign.html">Join Testnet</a>
                <a class="btn" href="explorer.html">View Explorer</a>
                <a class="btn" href="docs.html">Documentation</a>
              </div>
              <div class="trust-grid">
                <span class="status-pill good">Testnet Live</span>
                <span class="status-pill good">Explorer Online</span>
                <span class="status-pill good">Public GitHub</span>
                <span class="status-pill good">Docs Available</span>
                <span class="status-pill good">Community Active</span>
              </div>
            </div>
            <div class="hero-visual" aria-label="Animated MSC validator network">
              <span class="node-dot cyan" style="left:18%;top:28%"></span>
              <span class="node-dot" style="left:44%;top:46%"></span>
              <span class="node-dot cyan" style="left:70%;top:32%"></span>
              <span class="node-dot" style="left:64%;top:72%"></span>
              <span class="node-line" style="left:20%;top:31%;width:52%;transform:rotate(8deg)"></span>
              <span class="node-line" style="left:43%;top:48%;width:30%;transform:rotate(-25deg)"></span>
              <span class="node-line" style="left:48%;top:50%;width:28%;transform:rotate(38deg)"></span>
            </div>
          </div>
        </section>
        <section class="page">
          <div class="grid">
            ${metricCard("Block Height", "homeHeight")}
            ${metricCard("Active Validators", "homeValidators")}
            ${metricCard("Total Nodes", "homeNodes")}
            ${metricCard("Connected Peers", "homePeers")}
            ${metricCard("Network Uptime", "homeUptime", "99.98%")}
            ${metricCard("Current TPS", "homeTPS")}
            ${metricCard("Latest Release", "homeRelease", VERSION)}
            ${metricCard("Testnet Season", "homeSeason")}
          </div>
          <section class="grid two">
            <div class="card">
              <div class="label">Network Map</div>
              <div class="network-map">
                <span class="node-dot cyan" style="left:23%;top:36%"></span>
                <span class="node-dot" style="left:52%;top:28%"></span>
                <span class="node-dot cyan" style="left:77%;top:54%"></span>
                <span class="node-line" style="left:25%;top:38%;width:52%;transform:rotate(11deg)"></span>
              </div>
            </div>
            <div class="card">
              <div class="label">Latest News</div>
              <div class="list">
                <div class="list-item"><span>Founder reliability gate enabled</span><span class="meta">Program</span></div>
                <div class="list-item"><span>Public portal pages staged</span><span class="meta">Release</span></div>
                <div class="list-item"><span>Validator docs and installer flow available</span><span class="meta">Docs</span></div>
              </div>
            </div>
          </section>
        </section>
      </main>`);
  }

  function testnetPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Testnet", "Network Details", "Download genesis, snapshots, installers, and connect to public endpoints.")}
      <section class="grid">
        ${metricCard("Chain ID", "testnetChain", CHAIN_ID)}
        ${metricCard("Current Version", "testnetVersion", VERSION)}
        ${metricCard("Network Status", "testnetNetwork")}
        ${metricCard("Genesis Hash", "testnetGenesis", GENESIS_HASH.slice(0, 16) + "...")}
      </section>
      <section class="grid two">
        <div class="card">
          <div class="label">Downloads</div>
          <div class="list">
            <a class="list-item" href="../genesis.json"><span>Genesis File</span><span class="mono">genesis.json</span></a>
            <a class="list-item" href="../snapshot/latest"><span>Snapshot</span><span>Latest verified</span></a>
            <a class="list-item" href="node-setup.html"><span>Installer</span><span>Windows / Linux</span></a>
            <a class="list-item" href="node-setup.html"><span>Docker Compose</span><span>Node package</span></a>
          </div>
        </div>
        <div class="card">
          <div class="label">Connection Info</div>
          <div class="list">
            <div class="list-item"><span>RPC</span><span class="mono">https://mscblockexplorer.in</span></div>
            <div class="list-item"><span>WebSocket</span><span class="mono">wss://mscblockexplorer.in/wallet/events</span></div>
            <div class="list-item"><span>Seed Nodes</span><span id="testnetSeeds">Configured by installer</span></div>
            <div class="list-item"><span>Peer Nodes</span><span id="testnetPeers">Auto discovery</span></div>
          </div>
        </div>
      </section>
      <section class="card"><div class="label">Latest Upgrade Notices</div><div class="list" id="upgradeNotices"></div></section>
    </main>`);
  }

  function explorerPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Explorer", "Blocks, Transactions, Validators", "Search chain data and inspect recent blocks with live network context.")}
      <section class="card">
        <div class="label">Search</div>
        <div class="cta-row"><input id="explorerSearch" class="search-box" type="search" placeholder="Block / transaction / address / validator" /><button class="btn primary" id="explorerSearchBtn">Search</button><a class="btn" href="../explorer.html">Open Full Explorer</a></div>
      </section>
      <section class="grid">
        ${metricCard("TPS", "explorerTPS")}
        ${metricCard("Finality", "explorerFinality")}
        ${metricCard("Supply", "explorerSupply", "9,193,823,602 MSC")}
        ${metricCard("Validators", "explorerValidators")}
      </section>
      <section class="grid two">
        <div class="card"><div class="label">Recent Blocks</div><div class="list" id="recentBlocks"></div></div>
        <div class="card"><div class="label">Recent Transactions</div><div class="list" id="recentTxs"></div></div>
      </section>
    </main>`);
  }

  function validatorsPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Validators", "Validator Leaderboard", "Compare uptime, voting power, location, version, decentralization, and badges.")}
      <section class="grid">
        ${metricCard("Active Validators", "validatorsActive")}
        ${metricCard("Top Uptime", "validatorsTopUptime")}
        ${metricCard("Home-PC Nodes", "validatorsHome")}
        ${metricCard("Founder Badges", "validatorsFounder")}
      </section>
      <section class="card">
        <div class="filter-row">
          <button class="filter-button active" data-filter="all">All</button>
          <button class="filter-button" data-filter="home">Home-PC</button>
          <button class="filter-button" data-filter="founder">Founder</button>
          <button class="filter-button" data-filter="performance">Performance</button>
        </div>
      </section>
      <section class="table-card"><div class="table-wrap"><table><thead><tr><th>Rank</th><th>Name</th><th>Country</th><th>Uptime</th><th>Voting Power</th><th>Commission</th><th>Version</th><th>Status</th></tr></thead><tbody id="validatorsTable"></tbody></table></div></section>
      <section class="grid three"><div class="card"><div class="label">Top Uptime</div><div id="topUptime" class="list"></div></div><div class="card"><div class="label">Top Decentralization</div><div id="topDecentralization" class="list"></div></div><div class="card"><div class="label">Top Home Validator</div><div id="topHome" class="list"></div></div></section>
    </main>`);
  }

  function validatorProfilePage() {
    const id = new URLSearchParams(location.search).get("id") || "";
    return shellHTML(`<main class="page">
      ${pageTitle("Validator Profile", id ? `Validator ${esc(id)}` : "Validator Profile", "Profile, badges, performance, and operator metadata.")}
      <section id="validatorProfile" class="grid two"><div class="card"><div class="label">Profile</div><div class="value">Loading</div></div></section>
    </main>`);
  }

  function nodeSetupPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Node Setup", "Run MSC From Home Or Cloud", "Install, update, rollback, and troubleshoot full nodes, validators, sentries, RPC, and archive nodes.")}
      <section class="grid two">
        <div class="card"><div class="label">Quick Install</div><pre class="code-block"># Linux / Ubuntu
curl -fsSL https://install.mscchain.io | bash

# Local repo
./msc install candidate --id HOME1 --low-ram --auto-start</pre></div>
        <div class="card"><div class="label">Windows</div><pre class="code-block">irm https://install.mscchain.io | iex

.\msc-node.exe install candidate --id HOME1 --low-ram --auto-start
.\msc-node.exe doctor --id HOME1 --json</pre></div>
      </section>
      <section class="grid">
        ${metricCard("Minimum", "minHardware", "4 CPU / 8 GB / SSD")}
        ${metricCard("Recommended", "recHardware", "8 CPU / 16 GB / NVMe")}
        ${metricCard("Production", "prodHardware", "300 GB+ / monitored")}
      </section>
      <section class="card"><div class="label">Guides</div><div class="list"><a class="list-item" href="docs.html"><span>Validator Setup</span><span>Guide</span></a><a class="list-item" href="docs.html"><span>Sentry Setup</span><span>Guide</span></a><a class="list-item" href="docs.html"><span>RPC Setup</span><span>Guide</span></a><a class="list-item" href="docs.html"><span>Recovery Guide</span><span>Doctor reports</span></a></div></section>
    </main>`);
  }

  function docsPage() {
    const groups = [
      ["Getting Started", "Install node", "Create wallet", "Join testnet"],
      ["Architecture", "Consensus layer", "Networking layer", "Storage layer", "VM layer"],
      ["Validator Docs", "Validator guide", "Slashing rules", "Upgrade process"],
      ["Developer Docs", "RPC API", "REST API", "WebSocket API"],
      ["Security Docs", "Node security", "Key security", "Firewall setup"],
      ["FAQ", "Common errors", "Recovery", "Support"],
    ];
    return shellHTML(`<main class="page">
      ${pageTitle("Documentation", "Build And Operate MSC", "Operator, validator, developer, and security references.")}
      <section class="grid">${groups.map((g) => `<div class="card"><div class="label">${g[0]}</div>${g.slice(1).map((x) => `<a class="list-item" href="docs.html"><span>${x}</span><span>Open</span></a>`).join("")}</div>`).join("")}</section>
    </main>`);
  }

  function communityPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Community", "Join The MSC Network", "Discord is the primary hub; Telegram mirrors announcements.")}
      <section class="grid">
        ${["Discord", "Telegram", "GitHub", "X", "YouTube", "Forum"].map((name) => `<a class="card" href="#"><div class="label">${name}</div><div class="value">${name}</div><div class="muted">Community link</div></a>`).join("")}
      </section>
      <section class="grid">${metricCard("Discord Members", "discordMembers", "Pending")}${metricCard("Telegram Members", "telegramMembers", "Pending")}${metricCard("GitHub Stars", "githubStars", "Pending")}${metricCard("Validators Online", "communityValidators")}</section>
    </main>`);
  }

  function campaignPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Campaign", "MSC Founding Validators Program", "Run useful nodes, earn reputation, submit accepted bugs, and compete for early badges.")}
      <section class="grid">
        ${metricCard("Current Season", "campaignSeason")}
        ${metricCard("Countdown", "campaignCountdown")}
        ${metricCard("Participants", "campaignParticipants")}
        ${metricCard("Critical Bugs", "campaignCritical")}
      </section>
      <section class="grid two">
        <div class="card"><div class="label">Leaderboard</div><div id="campaignLeaderboard" class="list"></div></div>
        <div class="card"><div class="label">Points And Badges</div><div id="campaignRules" class="list"></div></div>
      </section>
      <section class="card"><div class="label">Weekly Reports</div><div class="link-row">${[1,2,3,4].map((n) => `<a class="btn" href="/v1/testnet/campaign/export?format=csv&week=${n}">Week ${n}</a>`).join("")}</div></section>
    </main>`);
  }

  function statusPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Network Status", "Live Monitoring", "NOC-style view of public services, chain performance, and validator network status.")}
      <section class="grid">
        ${metricCard("Explorer", "svcExplorer")}
        ${metricCard("RPC", "svcRPC")}
        ${metricCard("API", "svcAPI")}
        ${metricCard("Bootstrap Nodes", "svcBootstrap")}
        ${metricCard("Seed Nodes", "svcSeeds")}
        ${metricCard("Validator Network", "svcValidators")}
      </section>
      <section class="grid">
        ${metricCard("Block Time", "statusBlockTime")}
        ${metricCard("TPS", "statusTPS")}
        ${metricCard("Finality", "statusFinality")}
        ${metricCard("Peer Count", "statusPeers")}
      </section>
      <section class="grid two"><div class="card"><div class="label">Public Nodes</div><div id="statusPublicNodes" class="list"></div></div><div class="card"><div class="label">Historical Charts</div><div class="sparkline"></div><div class="muted">Uptime, block production, and validator growth.</div></div></section>
    </main>`);
  }

  function transparencyPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Transparency", "Roadmap, Reports, Incidents", "Public operational visibility for releases, validators, security, and network events.")}
      <section class="grid three">
        <div class="card"><div class="label">Completed</div><div class="list"><div class="list-item"><span>Multi-RPC wallet</span><span>Done</span></div><div class="list-item"><span>Founder program</span><span>Done</span></div></div></div>
        <div class="card"><div class="label">In Progress</div><div class="list"><div class="list-item"><span>Portal</span><span>Active</span></div><div class="list-item"><span>Archive/indexer</span><span>Staged</span></div></div></div>
        <div class="card"><div class="label">Planned</div><div class="list"><div class="list-item"><span>Light client wallet</span><span>Next</span></div><div class="list-item"><span>Multi-region testing</span><span>Next</span></div></div></div>
      </section>
      <section class="grid">${metricCard("Total Nodes", "transparencyNodes")}${metricCard("Validator Distribution", "transparencyValidators")}${metricCard("Country Distribution", "transparencyCountries", "Configured")}${metricCard("Client Versions", "transparencyVersions", VERSION)}</section>
      <section class="card"><div class="label">Release Notes / Security Reports / Incidents</div><div class="list"><div class="list-item"><span>No public incidents reported</span><span class="meta">Current</span></div></div></section>
    </main>`);
  }

  function contactPage() {
    return shellHTML(`<main class="page">
      ${pageTitle("Contact", "Work With MSC", "Business, validator, technical support, bug reports, and community links.")}
      <section class="grid">
        ${["Contact Team", "Business Partnerships", "Validator Applications", "Technical Support", "Bug Reports", "Social Links"].map((name) => `<div class="card"><div class="label">${name}</div><div class="value">${name}</div><div class="muted">Use Discord or GitHub forms for fastest response.</div></div>`).join("")}
      </section>
    </main>`);
  }

  const renderers = {
    home: homePage,
    testnet: testnetPage,
    explorer: explorerPage,
    validators: validatorsPage,
    validator: validatorProfilePage,
    "node-setup": nodeSetupPage,
    docs: docsPage,
    community: communityPage,
    campaign: campaignPage,
    status: statusPage,
    transparency: transparencyPage,
    contact: contactPage,
  };

  function render() {
    document.body.innerHTML = (renderers[PAGE] || homePage)();
    bindUI();
  }

  function bindUI() {
    const search = $("portalSearch");
    if (search) {
      search.addEventListener("keydown", (event) => {
        if (event.key !== "Enter") return;
        const q = search.value.trim();
        if (q) location.href = `explorer.html?q=${encodeURIComponent(q)}`;
      });
    }
    $("explorerSearchBtn")?.addEventListener("click", () => {
      const q = $("explorerSearch")?.value.trim() || "";
      location.href = q ? `../explorer.html?q=${encodeURIComponent(q)}` : "../explorer.html";
    });
    document.querySelectorAll("[data-filter]").forEach((button) => {
      button.addEventListener("click", () => {
        document.querySelectorAll("[data-filter]").forEach((b) => b.classList.toggle("active", b === button));
        renderValidatorsTable(button.dataset.filter || "all");
      });
    });
  }

  async function refreshStatus() {
    try {
      const data = await fetchJSON("/status", "status", TTL.status);
      state.status = data;
      const height = Number(data.height || data.chain_height || data.best?.height || 0);
      const finalized = Number(data.finalized_height || data.finalized || data.best?.finalized_height || 0);
      setText("homeHeight", fmt(height || state.realtime.height));
      setText("homePeers", fmt(data.peer_count || data.peers || data.best?.peer_count || 0));
      setText("homeTPS", fmt(data.tps || data.current_tps || 0));
      setText("testnetNetwork", data.network_health || data.health || "Connected");
      setText("explorerFinality", finalized ? `h ${fmt(finalized)}` : "-");
      setText("explorerTPS", fmt(data.tps || data.current_tps || 0));
      const statusAge = Number(data.last_block_age_seconds);
      if (Number.isFinite(statusAge)) {
        setText("statusBlockTime", ageText(statusAge));
        setTone("statusBlockTime", blockAgeTone(statusAge));
      } else {
        setText("statusBlockTime", data.block_time_ms ? `${Math.round(Number(data.block_time_ms))}ms` : "Live");
        setTone("statusBlockTime", "");
      }
      setText("statusTPS", fmt(data.tps || data.current_tps || 0));
      setText("statusPeers", fmt(data.peer_count || data.peers || 0));
    } catch (_) {
      setUnavailable(["homeHeight", "homePeers", "homeTPS", "testnetNetwork", "explorerTPS"]);
    }
  }

  async function refreshPublicStatus() {
    try {
      const data = await fetchJSON("/v1/public/status", "public-status", TTL.publicStatus);
      state.publicStatus = data;
      const chain = data.chain || {};
      const validators = data.validators || {};
      const publicRPC = data.public_rpc || {};
      setText("homeNodes", fmt(publicRPC.total || data.public_nodes_total || 0));
      setText("homeValidators", fmt(validators.active || validators.active_ready || 0));
      setText("validatorsActive", fmt(validators.active || validators.active_ready || 0));
      setText("communityValidators", fmt(validators.active || validators.active_ready || 0));
      setText("explorerValidators", fmt(validators.active || validators.active_ready || 0));
      setText("statusFinality", chain.finality_lag !== undefined ? `${fmt(chain.finality_lag)} blocks` : "-");
      setText("svcExplorer", "Online");
      setText("svcRPC", publicRPC.healthy ? "Healthy" : "Check");
      setText("svcAPI", "Online");
      setText("svcBootstrap", "Configured");
      setText("svcSeeds", "Configured");
      setText("svcValidators", validators.active_ready ? "Healthy" : "Monitoring");
      setText("transparencyNodes", fmt(publicRPC.total || 0));
      setText("transparencyValidators", fmt(validators.active || validators.active_ready || 0));
      renderNodeList("statusPublicNodes", publicRPC.nodes || []);
    } catch (_) {
      setUnavailable(["svcExplorer", "svcRPC", "svcAPI", "svcValidators"]);
    }
  }

  async function refreshPublicNodes() {
    try {
      const data = await fetchJSON("/v1/public-nodes", "public-nodes", TTL.publicNodes);
      state.publicNodes = data;
      setText("homeNodes", fmt(data.total || data.nodes?.length || 0));
      renderNodeList("statusPublicNodes", data.nodes || []);
    } catch (_) {
      try {
        const lb = await fetchJSON("/gateway/lb-status.json", "lb", TTL.lb);
        state.lb = lb;
        renderNodeList("statusPublicNodes", lb.public_nodes || lb.backends || []);
      } catch (_) {
        renderNodeList("statusPublicNodes", []);
      }
    }
  }

  async function refreshValidators() {
    try {
      const data = await fetchJSON("/v1/validators/leaderboard", "validators", TTL.validators);
      state.validators = data;
      const entries = data.entries || data.validators || [];
      setText("validatorsActive", fmt(data.active_count || entries.filter((v) => v.active).length));
      setText("validatorsHome", fmt(data.home_pc_count || entries.filter((v) => v.home_pc).length));
      setText("validatorsFounder", fmt(data.founder_count || entries.filter((v) => v.founder_badge).length));
      setText("homeValidators", fmt(data.active_count || entries.filter((v) => v.active).length));
      setText("homeSeason", data.testnet_campaign?.season_id || "-");
      const top = entries.slice().sort((a, b) => Number(b.signed_ratio_bps || 0) - Number(a.signed_ratio_bps || 0))[0];
      setText("validatorsTopUptime", top ? pct(top.signed_ratio_bps) : "-");
      renderValidatorsTable("all");
      renderTopLists(entries);
    } catch (_) {
      setUnavailable(["validatorsActive", "validatorsTopUptime", "validatorsHome", "validatorsFounder"]);
      renderEmptyTable("validatorsTable", 8, "Validator leaderboard unavailable");
    }
  }

  function renderValidatorsTable(filter = "all") {
    const body = $("validatorsTable");
    if (!body) return;
    const entries = state.validators?.entries || state.validators?.validators || [];
    const filtered = entries.filter((v) => {
      const slot = String(v.slot_type || "").toLowerCase();
      if (filter === "home") return !!v.home_pc;
      if (filter === "founder") return !!(v.founder_badge || v.founder_eligible);
      if (filter === "performance") return slot === "performance";
      return true;
    });
    body.innerHTML = filtered.slice(0, 80).map((v, i) => {
      const id = v.validator_id || v.id || `validator-${i + 1}`;
      const status = v.online || v.active ? "Online" : "Offline";
      return `<tr>
        <td>${fmt(v.rank || i + 1)}</td>
        <td><a href="validator.html?id=${encodeURIComponent(id)}">${esc(id)}</a><div class="badge-list">${badgesHTML(v)}</div></td>
        <td>${esc(v.country || v.region || "-")}</td>
        <td>${pct(v.signed_ratio_bps)}</td>
        <td>${fmt(v.effective_stake || v.actual_stake || 0)}</td>
        <td>${v.commission_bps !== undefined ? pct(v.commission_bps) : "-"}</td>
        <td>${esc(v.version || VERSION)}</td>
        <td><span class="status-pill ${status === "Online" ? "good" : "warn"}">${status}</span></td>
      </tr>`;
    }).join("") || `<tr><td colspan="8">Validator leaderboard unavailable</td></tr>`;
  }

  function badgesHTML(v) {
    const list = [];
    if (v.founder_badge) list.push("Founder");
    if (v.home_pc) list.push("Home Validator");
    if (Number(v.signed_ratio_bps || 0) >= 9900) list.push("Uptime Hero");
    if (Array.isArray(v.campaign_badges)) list.push(...v.campaign_badges.slice(0, 3));
    return [...new Set(list)].slice(0, 5).map((b) => `<span class="badge">${esc(b)}</span>`).join("");
  }

  function renderTopLists(entries) {
    const sortedUptime = entries.slice().sort((a, b) => Number(b.signed_ratio_bps || 0) - Number(a.signed_ratio_bps || 0));
    const sortedDec = entries.slice().sort((a, b) => Number(b.decentralization_score || 0) - Number(a.decentralization_score || 0));
    const home = entries.filter((v) => v.home_pc);
    renderMiniList("topUptime", sortedUptime, (v) => pct(v.signed_ratio_bps));
    renderMiniList("topDecentralization", sortedDec, (v) => `${Math.round(Number(v.decentralization_score || 0) * 100)}%`);
    renderMiniList("topHome", home, (v) => pct(v.signed_ratio_bps));
  }

  function renderMiniList(id, entries, valueFn) {
    const box = $(id);
    if (!box) return;
    box.innerHTML = entries.slice(0, 5).map((v, i) => `<div class="list-item"><span>${i + 1}. ${esc(v.validator_id || v.id || "-")}</span><span>${esc(valueFn(v))}</span></div>`).join("") || `<div class="list-item"><span>No data</span><span>-</span></div>`;
  }

  async function refreshCampaign() {
    try {
      const data = await fetchJSON("/v1/testnet/campaign", "campaign", TTL.campaign);
      const campaign = data.campaign || data;
      state.campaign = campaign;
      setText("campaignSeason", campaign.season_id || "-");
      setText("campaignCountdown", campaign.time_remaining || "-");
      setText("campaignParticipants", fmt(campaign.participants || 0));
      setText("campaignCritical", fmt(campaign.critical_bugs || 0));
      setText("homeSeason", campaign.season_id || "-");
      renderCampaign(campaign);
    } catch (_) {
      setUnavailable(["campaignSeason", "campaignCountdown", "campaignParticipants", "campaignCritical"]);
    }
  }

  function renderCampaign(campaign) {
    const board = $("campaignLeaderboard");
    if (board) {
      const top = Array.isArray(campaign.top_validators) ? campaign.top_validators : [];
      board.innerHTML = top.slice(0, 10).map((v) => `<div class="list-item"><span>#${fmt(v.rank)} ${esc(v.validator_id)}</span><span>${fmt(v.points)} pts</span></div>`).join("") || `<div class="list-item"><span>No published standings</span><span>-</span></div>`;
    }
    const rules = $("campaignRules");
    if (rules) {
      const badges = Object.entries(campaign.badge_rules || {}).slice(0, 8);
      rules.innerHTML = badges.map(([name, rule]) => `<div class="list-item"><span>${esc(name)}</span><span>${esc(rule)}</span></div>`).join("") || `<div class="list-item"><span>Rules load from /v1/testnet/campaign</span><span>-</span></div>`;
    }
  }

  async function renderValidatorProfile() {
    const box = $("validatorProfile");
    if (!box) return;
    await refreshValidators();
    const id = new URLSearchParams(location.search).get("id") || "";
    const entries = state.validators?.entries || state.validators?.validators || [];
    const v = entries.find((item) => String(item.validator_id || item.id || "").toLowerCase() === id.toLowerCase());
    if (!v) {
      box.innerHTML = `<div class="card"><div class="label">Validator</div><div class="value">Not found</div><p class="muted">Use validators.html to select a validator profile.</p></div>`;
      return;
    }
    box.innerHTML = `
      <div class="card">
        <div class="label">Validator</div>
        <div class="value">${esc(v.validator_id || id)}</div>
        <div class="badge-list">${badgesHTML(v)}</div>
        <div class="grid">
          ${profileMetric("Country", v.country || v.region || "-")}
          ${profileMetric("Version", v.version || VERSION)}
          ${profileMetric("Uptime", pct(v.signed_ratio_bps))}
          ${profileMetric("Voting Power", fmt(v.effective_stake || v.actual_stake || 0))}
          ${profileMetric("Peer Count", fmt(v.peer_count || 0))}
          ${profileMetric("Status", v.online ? "Online" : "Monitoring")}
        </div>
      </div>
      <div class="card"><div class="label">Performance</div><div class="sparkline"></div><div class="list"><div class="list-item"><span>Block participation</span><span>${pct(v.signed_ratio_bps)}</span></div><div class="list-item"><span>Missed blocks</span><span>${fmt(v.missed_blocks || 0)}</span></div><div class="list-item"><span>Reputation</span><span>${Math.round(Number(v.final_score || 0) * 100)}%</span></div></div></div>`;
  }

  function profileMetric(label, value) {
    return `<div class="card"><div class="label">${label}</div><div class="value">${value}</div></div>`;
  }

  function renderNodeList(id, nodes) {
    const box = $(id);
    if (!box) return;
    box.innerHTML = (nodes || []).slice(0, 12).map((node) => {
      const name = node.id || node.target || node.rpc_url || node.url || "node";
      const healthy = node.health_state ? node.health_state !== "unhealthy" : !!node.healthy || Number(node.status_code) === 200;
      return `<div class="health-row">
        <span class="mono">${esc(name)}</span>
        <span class="health ${healthy ? "good" : "warn"}">${healthy ? "healthy" : "check"}</span>
        <span>h ${fmt(node.height || node.chain_height || 0)}</span>
        <span>${fmt(node.latency_ms || 0)}ms</span>
        <span>${esc(node.consensus_mode || node.cmd || "-")}</span>
      </div>`;
    }).join("") || `<div class="list-item"><span>No public node data yet</span><span>-</span></div>`;
  }

  function renderEmptyTable(id, cols, text) {
    const body = $(id);
    if (body) body.innerHTML = `<tr><td colspan="${cols}">${text}</td></tr>`;
  }

  function setUnavailable(ids) {
    ids.forEach((id) => setText(id, "Unavailable"));
  }

  function connectEvents() {
    if (!("WebSocket" in window)) return;
    const scheme = location.protocol === "https:" ? "wss" : "ws";
    const url = `${scheme}://${location.host}/wallet/events`;
    try {
      const ws = new WebSocket(url);
      ws.onopen = () => {
        state.realtime.connected = true;
      };
      ws.onmessage = (message) => {
        try {
          const event = JSON.parse(message.data);
          if (event.height) {
            state.realtime.height = Math.max(state.realtime.height || 0, Number(event.height) || 0);
            setText("homeHeight", fmt(state.realtime.height));
          }
          if (event.finalized_height) state.realtime.finalized = Number(event.finalized_height) || 0;
          if (event.mode) state.realtime.cmd = event.mode;
          if (event.last_block_age_seconds !== undefined) {
            state.realtime.lastBlockAge = Number(event.last_block_age_seconds) || 0;
            state.realtime.lastBlockBaseMs = Date.now();
          }
          const sent = Number(event.ts_ms || (event.ts ? Number(event.ts) * 1000 : 0));
          if (sent > 0) state.realtime.eventDelayMs = Math.max(0, Date.now() - sent);
        } catch (_) {
          // Ignore malformed event payloads.
        }
      };
    } catch (_) {
      // Polling fallback still renders the portal.
    }
  }

  function tickRealtime() {
    setText("homeRelease", VERSION);
    setText("testnetVersion", VERSION);
    const age = secondsAge();
    if (age !== null) {
      setText("statusBlockTime", ageText(age));
      setTone("statusBlockTime", blockAgeTone(age));
    }
  }

  async function refreshAll() {
    await Promise.allSettled([
      refreshStatus(),
      refreshPublicStatus(),
      refreshPublicNodes(),
      refreshValidators(),
      refreshCampaign(),
    ]);
    if (PAGE === "validator") await renderValidatorProfile();
  }

  render();
  connectEvents();
  refreshAll();
  setInterval(tickRealtime, 1000);
  setInterval(refreshAll, 30000);
})();
