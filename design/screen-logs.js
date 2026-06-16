/* Hull — Logs screen. Source + level + text filters over a live monospace stream. */
(function () {
  const H = () => window.HULL;
  let rootEl = null, panel = null, timer = null;
  let source = "all", level = "all", query = "", follow = true;
  let lines = [];

  function sources() {
    const sites = H().ROOTS.flatMap(r => r.managed).filter(s => s.status === "running").map(s => s.name);
    const svcs = H().SERVICES.filter(s => s.status === "running").map(s => s.name);
    return { sites, svcs };
  }
  function allSourceNames() { const s = sources(); return [...s.sites, ...s.svcs]; }

  const SAMPLES = {
    info: ["GET / 200 · {ms}ms", "GET /assets/app.css 200", "POST /login 302", "GET /api/user 200 · {ms}ms", "view rendered home.blade.php", "cache write users.{n}"],
    warn: ["slow query {ms}ms · orders index scan", "deprecated: Str::random() signature", "queue backlog {n} jobs", "cache miss · regenerating"],
    error: ["SQLSTATE[08006] connection refused", "uncaught TypeError in OrderController:42", "500 Internal Server Error · GET /checkout", "failed to bind :443 — in use"],
  };
  function seed() {
    lines = [];
    const names = allSourceNames();
    let base = Date.now() - names.length * 4000;
    for (let i = 0; i < 26; i++) {
      const src = names[(Math.random() * names.length) | 0] || "engine";
      const roll = Math.random();
      const lvl = roll > 0.92 ? "error" : roll > 0.78 ? "warn" : "info";
      lines.push(makeLine(src, lvl, new Date(base += 3200)));
    }
  }
  function makeLine(src, lvl, date) {
    const pool = SAMPLES[lvl];
    const msg = pool[(Math.random() * pool.length) | 0]
      .replace("{ms}", (Math.random() * 60 + 4).toFixed(0))
      .replace("{n}", (Math.random() * 900 + 100) | 0);
    return { t: (date || new Date()).toTimeString().slice(0, 8), src, lvl, msg };
  }

  window.renderLogs = function (el) {
    rootEl = el;
    if (!lines.length) seed();
    const names = allSourceNames();
    el.innerHTML = `
      <div class="page">
        <div class="page-head">
          <h1>Logs</h1>
          <span class="ph-sub" id="logCount"></span>
        </div>
        <div class="logs-toolbar">
          <select class="select" id="logSource" style="width:190px">
            <option value="all">All sources</option>
            <optgroup label="Sites">${sources().sites.map(n=>`<option ${source===n?"selected":""}>${n}</option>`).join("")}</optgroup>
            <optgroup label="Services">${sources().svcs.map(n=>`<option ${source===n?"selected":""}>${n}</option>`).join("")}</optgroup>
          </select>
          <div class="seg" id="logLevel">
            ${["all","info","warn","error"].map(l=>`<button type="button" class="seg-btn${level===l?" active":""}" data-level="${l}">${l[0].toUpperCase()+l.slice(1)}</button>`).join("")}
          </div>
          <div class="search" style="flex:1;min-width:160px"><input class="input" id="logSearch" placeholder="Filter log text…" value="${query}"></div>
          <select class="select" id="logTail" style="width:auto"><option>200 lines</option><option>500 lines</option><option>1000 lines</option><option>All</option></select>
          <label class="switch"><input type="checkbox" id="logFollow" ${follow?"checked":""}><span class="track"></span>Follow</label>
          <button class="btn" id="logClear">Clear</button>
        </div>
        <div class="logs-body"><div class="logpanel" id="logStream"></div></div>
      </div>`;
    panel = el.querySelector("#logStream");
    // search icon
    el.querySelector("#logSearch").insertAdjacentHTML("beforebegin", icon("search",14));

    el.querySelector("#logSource").value = source;
    el.querySelector("#logSource").addEventListener("change", e => { source = e.target.value; repaint(); });
    el.querySelector("#logLevel").addEventListener("click", e => {
      const b = e.target.closest("[data-level]"); if (!b) return;
      level = b.dataset.level;
      el.querySelectorAll("#logLevel .seg-btn").forEach(x => x.classList.toggle("active", x === b));
      repaint();
    });
    el.querySelector("#logSearch").addEventListener("input", e => { query = e.target.value; repaint(); });
    const followEl = el.querySelector("#logFollow");
    followEl.addEventListener("change", () => { follow = followEl.checked; setStream(); });
    el.querySelector("#logClear").addEventListener("click", () => { lines = []; repaint(); });

    repaint();
    setStream();
    App.onLeave(() => { if (timer) { clearInterval(timer); timer = null; } });
  };

  function pass(l) {
    if (source !== "all" && l.src !== source) return false;
    if (level !== "all" && l.lvl !== level) return false;
    if (query && !(l.src + " " + l.msg).toLowerCase().includes(query.toLowerCase())) return false;
    return true;
  }
  function lineHTML(l) {
    const cls = l.lvl === "warn" ? "wn" : l.lvl === "error" ? "er" : "";
    const mark = l.lvl === "warn" ? `<span class="wn">!</span> ` : l.lvl === "error" ? `<span class="er">✗</span> ` : "";
    return `<div class="ln"><span class="t">${l.t}</span> <span class="src">${l.src}</span> ${mark}<span class="${cls}">${l.msg}</span></div>`;
  }
  function repaint() {
    if (!panel) return;
    const shown = lines.filter(pass);
    panel.innerHTML = shown.map(lineHTML).join("") || `<div class="ln faint">No matching log lines.</div>`;
    panel.scrollTop = panel.scrollHeight;
    const c = rootEl.querySelector("#logCount");
    if (c) c.textContent = `${shown.length} line${shown.length === 1 ? "" : "s"}${source !== "all" ? " · " + source : ""}`;
  }
  function setStream() {
    if (timer) { clearInterval(timer); timer = null; }
    if (!follow) return;
    timer = setInterval(() => {
      const names = allSourceNames();
      const src = names[(Math.random() * names.length) | 0] || "engine";
      const roll = Math.random();
      const lvl = roll > 0.9 ? "error" : roll > 0.75 ? "warn" : "info";
      const l = makeLine(src, lvl);
      lines.push(l);
      if (lines.length > 1200) lines.shift();
      if (pass(l) && panel) {
        panel.insertAdjacentHTML("beforeend", lineHTML(l));
        panel.scrollTop = panel.scrollHeight;
        const c = rootEl.querySelector("#logCount");
        if (c) c.textContent = `${panel.children.length} lines${source !== "all" ? " · " + source : ""}`;
      }
    }, 1300);
  }
})();
