(function () {
  "use strict";

  const EXPLORER = "https://explorer.mscblockexplorer.in";
  const $ = (id) => document.getElementById(id);
  const fmt = (value) => {
    const n = Number(value);
    if (!Number.isFinite(n)) return value ?? "-";
    return n.toLocaleString("en-US");
  };
  const short = (value, left = 9, right = 8) => {
    const text = String(value || "");
    return text.length > left + right + 3 ? `${text.slice(0, left)}...${text.slice(-right)}` : text || "-";
  };
  const esc = (value) => String(value ?? "-").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[ch]));
  const unwrap = (value) => value && typeof value === "object" && "data" in value ? value.data : value;

  async function request(path) {
    const response = await fetch(path, { cache: "no-store" });
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`.trim());
    return unwrap(await response.json());
  }

  async function first(paths) {
    let last;
    for (const path of paths) {
      try {
        return await request(path);
      } catch (error) {
        last = error;
      }
    }
    throw last || new Error("No live source available");
  }

  function setText(id, value) {
    const node = $(id);
    if (node) node.textContent = value ?? "-";
  }

  function routeSearch(query) {
    const raw = String(query || "").trim();
    if (!raw) {
      window.location.href = `${EXPLORER}/explorer.html`;
      return;
    }
    if (/^\d+$/.test(raw)) {
      window.location.href = `${EXPLORER}/explorer-blocks.html?height=${encodeURIComponent(raw)}`;
      return;
    }
    window.location.href = `${EXPLORER}/explorer-search.html?q=${encodeURIComponent(raw)}`;
  }

  function renderBlocks(blocks) {
    const list = $("latestBlocks");
    if (!list) return;
    if (!blocks.length) {
      list.innerHTML = `<div class="empty-state">Latest block data is temporarily unavailable.</div>`;
      return;
    }
    list.innerHTML = blocks.slice(0, 5).map((block) => `
      <a class="data-row" href="${EXPLORER}/explorer-blocks.html?height=${encodeURIComponent(block.height || "")}">
        <span class="data-icon"><i data-lucide="box"></i></span>
        <span>
          <strong>#${esc(fmt(block.height))}</strong>
          <small>${esc(short(block.hash, 14, 10))} | proposer ${esc(block.proposer || "-")}</small>
        </span>
        <span class="data-value">${esc(fmt(block.tx_count || block.transactions?.length || 0))} tx</span>
      </a>
    `).join("");
  }

  function renderTransactions(items) {
    const list = $("latestTransactions");
    if (!list) return;
    if (!items.length) {
      list.innerHTML = `<div class="empty-state">No recent transactions in the sampled live blocks.</div>`;
      return;
    }
    list.innerHTML = items.slice(0, 5).map((item) => {
      const tx = item.tx || {};
      const id = tx.id || tx.tx_id || tx.hash || tx.signature || tx.type || "transaction";
      return `
        <a class="data-row" href="${EXPLORER}/explorer-transactions.html?q=${encodeURIComponent(id)}">
          <span class="data-icon"><i data-lucide="file-text"></i></span>
          <span>
            <strong>${esc(short(id, 14, 8))}</strong>
            <small>Block #${esc(fmt(item.height))} | ${esc(short(tx.from || tx.sender || "-", 9, 5))} to ${esc(short(tx.to || tx.recipient || "-", 9, 5))}</small>
          </span>
          <span class="data-value">${esc(tx.amount ?? tx.value ?? tx.type ?? "MSC")}</span>
        </a>`;
    }).join("");
  }

  function renderSummary(publicStatus, status, tokenomics, governance) {
    const chain = publicStatus?.chain || status || {};
    const items = [
      ["Chain ID", chain.chain_id || tokenomics?.chain_id || "91938"],
      ["Consensus", chain.cmd || status?.consensus_mode || "-"],
      ["Peers", fmt(chain.peers ?? status?.peer_count ?? status?.peers)],
      ["Treasury", `${fmt(governance?.treasury_balance ?? 0)} MSC`],
      ["Max Supply", `${fmt(tokenomics?.max_supply || tokenomics?.economic_policy?.fixed_total_supply)} MSC`],
    ];
    const target = $("networkSummary");
    if (target) {
      target.innerHTML = items.map(([label, value]) => `<div class="summary-item"><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`).join("");
    }
  }

  function renderValidators(leaderboard) {
    const list = $("topValidators");
    if (!list) return;
    const entries = leaderboard?.entries || [];
    if (!entries.length) {
      list.innerHTML = `<div class="empty-state">Validator leaderboard is temporarily unavailable.</div>`;
      return;
    }
    list.innerHTML = entries.slice(0, 3).map((validator, index) => `
      <a class="validator-row" href="${EXPLORER}/explorer-validators.html">
        <span class="validator-rank">${index + 1}</span>
        <span>
          <strong>Validator ${esc(validator.validator_id || validator.id || "-")}</strong>
          <small>${esc(validator.status || (validator.online ? "online" : "offline"))} | stake ${esc(fmt(validator.effective_stake ?? validator.actual_stake ?? 0))}</small>
        </span>
        <span class="score">${esc(((Number(validator.final_score) || 0) * 100).toFixed(1))}%</span>
      </a>
    `).join("");
  }

  async function loadRecentTransactions(blocks) {
    const details = await Promise.allSettled(blocks.slice(0, 8).map((block) => first([
      `/explorer/block?height=${encodeURIComponent(block.height)}`,
      `/v1/block?height=${encodeURIComponent(block.height)}`,
    ])));
    return details.flatMap((result) => {
      if (result.status !== "fulfilled") return [];
      const block = result.value || {};
      return (block.transactions || block.txs || []).map((tx) => ({ height: block.height || block.summary?.height, tx }));
    });
  }

  function updateHeroStats(publicStatus, status, blocks, leaderboard, tokenomics) {
    const chain = publicStatus?.chain || status || {};
    const latest = blocks[0] || {};
    const height = chain.height || latest.height || status?.height;
    const finalized = chain.finalized_height || status?.finalized_height || status?.finalized || latest.height;
    const active = leaderboard?.active_count || (leaderboard?.entries || []).filter((item) => item.active || item.online).length;
    const health = chain.network_health || status?.network_health || status?.block_production_status || "-";
    const cmd = chain.cmd || status?.consensus_mode || status?.cmd || "-";
    setText("latestHeight", fmt(height));
    setText("lastBlockAge", `${fmt(chain.last_block_age_seconds ?? status?.last_block_age_seconds ?? 0)}s ago`);
    setText("finalizedHeight", fmt(finalized));
    setText("finalityLag", `Lag ${fmt(chain.finality_lag ?? Math.max(0, Number(height || 0) - Number(finalized || 0)))}`);
    setText("activeValidators", fmt(active || "-"));
    setText("validatorFoot", `${fmt((leaderboard?.entries || []).length)} known validators`);
    setText("totalSupply", fmt(tokenomics?.total_supply || tokenomics?.max_supply || tokenomics?.economic_policy?.fixed_total_supply));
    setText("networkHealth", health);
    setText("networkCmd", `CMD ${cmd}`);
    setText("networkMode", cmd === "-" ? health : `MSC Mainnet | ${cmd}`);
    const dot = $("statusDot");
    if (dot) {
      dot.classList.remove("good", "bad");
      if (/healthy|normal|producing/i.test(`${health} ${cmd}`)) dot.classList.add("good");
      if (/halt|down|failed|unhealthy|attention/i.test(`${health} ${cmd}`)) dot.classList.add("bad");
    }
  }

  async function loadLanding() {
    try {
      const [publicStatus, status, blocksData, leaderboard, tokenomics, governance] = await Promise.allSettled([
        first(["/v1/public/status", "/public/status"]),
        first(["/v1/status", "/status"]),
        first(["/indexer/blocks?limit=8", "/explorer/blocks?limit=8", "/v1/blocks?limit=8"]),
        first(["/v1/validators/leaderboard", "/validators/leaderboard"]),
        first(["/tokenomics"]),
        first(["/v1/governance/status", "/governance/status"]),
      ]);
      const publicValue = publicStatus.status === "fulfilled" ? publicStatus.value : {};
      const statusValue = status.status === "fulfilled" ? status.value : {};
      const blocksValue = blocksData.status === "fulfilled" ? (blocksData.value.blocks || blocksData.value.data || []) : [];
      const leaderboardValue = leaderboard.status === "fulfilled" ? leaderboard.value : {};
      const tokenValue = tokenomics.status === "fulfilled" ? tokenomics.value : {};
      const governanceValue = governance.status === "fulfilled" ? governance.value : {};
      updateHeroStats(publicValue, statusValue, blocksValue, leaderboardValue, tokenValue);
      renderBlocks(blocksValue);
      renderSummary(publicValue, statusValue, tokenValue, governanceValue);
      renderValidators(leaderboardValue);
      const transactions = await loadRecentTransactions(blocksValue).catch(() => []);
      renderTransactions(transactions);
      window.lucide?.createIcons();
    } catch (error) {
      setText("networkMode", "Live data unavailable");
      $("statusDot")?.classList.add("bad");
      renderBlocks([]);
      renderTransactions([]);
    }
  }

  function initCanvas() {
    const canvas = $("networkCanvas");
    if (!canvas) return;
    const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;
    const saveData = navigator.connection?.saveData;
    const compactViewport = window.matchMedia?.("(max-width: 720px)")?.matches;
    if (reducedMotion || saveData || compactViewport) {
      canvas.setAttribute("data-static", "true");
      return;
    }
    const ctx = canvas.getContext("2d");
    const points = [];
    let width = 0;
    let height = 0;
    let frameID = 0;
    const resize = () => {
      const scale = Math.min(window.devicePixelRatio || 1, 1.5);
      width = canvas.clientWidth;
      height = canvas.clientHeight;
      canvas.width = Math.floor(width * scale);
      canvas.height = Math.floor(height * scale);
      ctx.setTransform(scale, 0, 0, scale, 0, 0);
      points.length = 0;
      const count = Math.min(56, Math.max(28, Math.floor(width / 28)));
      for (let i = 0; i < count; i += 1) {
        points.push({
          x: Math.random() * width,
          y: Math.random() * height,
          vx: (Math.random() - 0.5) * 0.28,
          vy: (Math.random() - 0.5) * 0.28,
        });
      }
    };
    const frame = () => {
      ctx.clearRect(0, 0, width, height);
      ctx.fillStyle = "rgba(5, 7, 12, 0.32)";
      ctx.fillRect(0, 0, width, height);
      points.forEach((point, index) => {
        point.x += point.vx;
        point.y += point.vy;
        if (point.x < 0 || point.x > width) point.vx *= -1;
        if (point.y < 0 || point.y > height) point.vy *= -1;
        for (let j = index + 1; j < points.length; j += 1) {
          const other = points[j];
          const dx = point.x - other.x;
          const dy = point.y - other.y;
          const dist = Math.sqrt(dx * dx + dy * dy);
          if (dist < 160) {
            ctx.globalAlpha = (160 - dist) / 800;
            ctx.strokeStyle = "#8b5cf6";
            ctx.beginPath();
            ctx.moveTo(point.x, point.y);
            ctx.lineTo(other.x, other.y);
            ctx.stroke();
          }
        }
        ctx.globalAlpha = 0.58;
        ctx.fillStyle = index % 4 === 0 ? "#5be7a9" : "#8b5cf6";
        ctx.beginPath();
        ctx.arc(point.x, point.y, index % 4 === 0 ? 1.8 : 1.3, 0, Math.PI * 2);
        ctx.fill();
      });
      ctx.globalAlpha = 1;
      frameID = requestAnimationFrame(frame);
    };
    resize();
    window.addEventListener("resize", resize, { passive: true });
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        cancelAnimationFrame(frameID);
      } else {
        frameID = requestAnimationFrame(frame);
      }
    });
    frameID = requestAnimationFrame(frame);
  }

  $("heroSearch")?.addEventListener("submit", (event) => {
    event.preventDefault();
    routeSearch($("heroSearchInput")?.value);
  });

  initCanvas();
  loadLanding();
  window.setInterval(loadLanding, 30000);
  window.lucide?.createIcons();
})();
