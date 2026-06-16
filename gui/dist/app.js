/* Hull — app shell: async boot against the live daemon, routing, theme,
   window controls (frameless), shared utilities. Screen modules and the
   data layer (data.js → window.HULL) are unchanged in spirit; this wires
   them to hulld. */
(function () {
  // Nav is data-driven: Dashboard, then one item per configured project root
  // (location is the navigation; type is a per-item badge), then the global
  // functional screens.
  function navItems() {
    const items = [{ id: "dashboard", label: "Dashboard", icon: "dashboard" }];
    (window.HULL.DIRS || []).forEach(d => items.push({ id: "root:" + d.path, label: d.label, icon: "sites", root: d.path }));
    items.push({ sep: true });
    items.push({ id: "services", label: "Services", icon: "services" });
    items.push({ id: "mail", label: "Mail", icon: "mail" });
    items.push({ id: "logs", label: "Logs", icon: "logs" });
    items.push({ id: "settings", label: "Settings", icon: "settings" });
    return items;
  }
  function firstRootRoute() {
    const d = (window.HULL.DIRS || [])[0];
    return d ? "root:" + d.path : "dashboard";
  }
  // Keep `route` valid across reloads (roots can be added/removed).
  function ensureRoute() {
    if (["dashboard", "services", "mail", "logs", "settings"].includes(route)) return;
    if (route.indexOf("root:") === 0 && (window.HULL.DIRS || []).some(d => "root:" + d.path === route)) return;
    route = firstRootRoute();
  }

  const main = document.getElementById("main");
  const navEl = document.getElementById("nav");
  const scrim = document.getElementById("scrim");
  const toastEl = document.getElementById("toast");
  const daemonPill = document.getElementById("daemonPill");

  let route = "sites";
  let cleanups = [];
  let connected = false;

  const tauriWin = () => {
    const tw = window.__TAURI__ && window.__TAURI__.window;
    return tw && (tw.getCurrentWindow ? tw.getCurrentWindow() : tw.appWindow);
  };

  /* ---------------- THEME ---------------- */
  function currentTheme() { return localStorage.getItem("hull-theme") || "auto"; }
  function applyTheme(t) {
    if (t === "auto") document.documentElement.removeAttribute("data-theme");
    else document.documentElement.setAttribute("data-theme", t);
  }
  function setTheme(t) { localStorage.setItem("hull-theme", t); applyTheme(t); }
  applyTheme(currentTheme());

  /* ---------------- WINDOW CONTROLS + DRAG (frameless) ---------------- */
  let maximized = false;
  document.querySelectorAll(".winbtn").forEach(b => b.addEventListener("click", async () => {
    const win = tauriWin();
    if (!win) return;
    const act = b.dataset.win;
    try {
      if (act === "min") await win.minimize();
      else if (act === "max") { await win.toggleMaximize(); maximized = !maximized; }
      else if (act === "close") await win.close();
    } catch (e) {}
  }));
  // WebView2 ignores -webkit-app-region; drag designated regions manually.
  document.addEventListener("mousedown", (e) => {
    if (e.button !== 0) return;
    const region = e.target.closest(".brand, .page-head, .detail-head");
    if (!region) return;
    if (e.target.closest("button, a, input, select, textarea, .tabs, .url-btn, .search, .seg")) return;
    const win = tauriWin();
    if (win && win.startDragging) win.startDragging();
  });

  /* ---------------- NAV ---------------- */
  function renderNav() {
    navEl.innerHTML = navItems().map(item => {
      if (item.sep) return `<div class="nav-sep"></div>`;
      const active = item.id === route ? " active" : "";
      let count = "";
      if (item.root) {
        const r = (window.HULL.ROOTS || []).find(x => x.path === item.root);
        count = `<span class="nav-count">${r ? r.managed.length : 0}</span>`;
      } else if (item.id === "services") {
        count = `<span class="nav-count">${window.HULL.SERVICES.length}</span>`;
      }
      return `<button class="nav-item${active}" data-route="${item.id}">${window.icon(item.icon)}<span>${item.label}</span>${count}</button>`;
    }).join("");
  }
  navEl.addEventListener("click", e => {
    const b = e.target.closest("[data-route]");
    if (b && connected) go(b.dataset.route);
  });

  /* ---------------- ROUTING ---------------- */
  function runCleanups() { cleanups.forEach(fn => { try { fn(); } catch (e) {} }); cleanups = []; }
  function mount(r) {
    runCleanups();
    route = r;
    renderNav();
    const screen = document.createElement("div");
    screen.className = "screen";
    main.innerHTML = "";
    main.appendChild(screen);
    renderInto(screen, r);
  }
  function go(r) { if (r === route && main.firstChild) return; mount(r); }
  function rerender() { if (connected) { ensureRoute(); mount(route); } }
  function renderInto(el, r) {
    if (r.indexOf("root:") === 0) { window.renderSites(el, r.slice(5)); return; }
    switch (r) {
      case "dashboard": window.renderDashboard(el); break;
      case "services":  window.renderServices(el); break;
      case "mail":      window.renderMail(el); break;
      case "logs":      window.renderLogs(el); break;
      case "settings":  window.renderSettings(el); break;
    }
  }

  /* ---------------- SHARED UTILITIES ---------------- */
  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.add("show");
    clearTimeout(toast._t);
    toast._t = setTimeout(() => toastEl.classList.remove("show"), 1900);
  }
  function copyText(text, btn) {
    const done = () => { if (btn) { btn.classList.add("copied"); setTimeout(() => btn.classList.remove("copied"), 1000); } toast("Copied to clipboard"); };
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(done).catch(() => fallbackCopy(text, done));
    else fallbackCopy(text, done);
  }
  function fallbackCopy(text, done) {
    const ta = document.createElement("textarea");
    ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta); ta.select();
    try { document.execCommand("copy"); } catch (e) {}
    document.body.removeChild(ta); done();
  }
  function openDialog(html) {
    scrim.innerHTML = html;
    scrim.classList.add("open");
    const close = scrim.querySelector("[data-dialog-close]");
    if (close) close.addEventListener("click", closeDialog);
  }
  function closeDialog() { detailOpen = false; scrim.classList.remove("open"); setTimeout(() => { scrim.innerHTML = ""; }, 200); }
  scrim.addEventListener("click", e => { if (e.target === scrim) closeDialog(); });
  document.addEventListener("keydown", e => { if (e.key === "Escape" && scrim.classList.contains("open")) closeDialog(); });
  document.addEventListener("click", e => {
    const adv = e.target.closest(".adv-toggle");
    if (adv) { adv.classList.toggle("open"); const body = adv.nextElementSibling; if (body && body.classList.contains("adv-body")) body.classList.toggle("open"); return; }
    const row = e.target.closest(".pick-row");
    if (row && !e.target.closest(".pick-ver")) row.classList.toggle("sel");
  });
  function readPicks(scope) {
    return [...scope.querySelectorAll(".pick-row.sel")].map(r => ({ key: r.dataset.key, label: r.dataset.label || r.dataset.key, version: (r.querySelector(".pick-ver") || {}).value || null }));
  }
  function openExternal(url) {
    const op = window.__TAURI__ && window.__TAURI__.opener;
    if (op && op.openUrl) op.openUrl(url); else window.open(url, "_blank");
  }
  // Native OS picker. kind: "folder" | "file". Returns a path or null.
  // Uses the core invoke bridge (reliable) rather than the plugin JS global.
  async function pick(kind, opts) {
    const inv = window.__TAURI__ && window.__TAURI__.core && window.__TAURI__.core.invoke;
    if (!inv) { toast("Picker unavailable outside the app"); return null; }
    const options = Object.assign({ directory: kind === "folder", multiple: false }, opts || {});
    try {
      const sel = await inv("plugin:dialog|open", { options });
      return typeof sel === "string" ? sel : (Array.isArray(sel) && sel.length ? sel[0] : null);
    } catch (e) {
      toast("Picker error: " + (e && e.message ? e.message : e));
      return null;
    }
  }

  /* ---------------- LIVE API ---------------- */
  // App.api(method, path, body) → daemon; App.reload() refreshes state + view.
  async function api(method, path, body) { return window.HULL.api(method, path, body); }
  async function reload() {
    try { await window.HULL.load(); } catch (e) { return; }
    renderNav();
    buildPopover();
    rerender();
  }
  // Optimistic action helper: run an API call, then reload. Job-returning
  // endpoints (create/import/rebuild/reset/cluster) stream progress in the
  // status bar instead of a one-shot toast.
  async function act(promise, okMsg) {
    let result;
    try { result = await promise; }
    catch (e) { toast(e.message || "Action failed"); return; }
    if (result && result.job && result.job.id) {
      streamJob(result.job.id, okMsg || prettyKind(result.job.kind));
      return; // streamJob reloads on completion
    }
    if (okMsg) toast(okMsg);
    await reload();
  }

  /* ---------------- LIVE JOB STATUS BAR + DETAIL MODAL ----------------
     The bar lives outside the dialog scrim, so opening it never races with a
     just-closed create/import dialog (the old empty-overlay bug). Click it
     for the full streamed log. */
  const statusbar = document.getElementById("statusbar");
  const sbTitle = statusbar && statusbar.querySelector(".sb-title");
  const sbLine = statusbar && statusbar.querySelector(".sb-line");
  let job = null;        // { title, lines:[], done, failed, es }
  let detailOpen = false;
  let sbHide = null;
  function prettyKind(k) { return (k || "Working").replace(/[:_-]/g, " "); }

  function streamJob(id, title) {
    if (job && job.es) job.es.close();
    job = { title: title || "Working…", lines: [], done: false, failed: false, es: null };
    clearTimeout(sbHide);
    paintBar();
    try {
      job.es = new EventSource(window.HULL.sseURL("/v1/jobs/" + id + "/stream"));
      job.es.onmessage = (e) => pushLine(e.data);
      job.es.addEventListener("done", (e) => {
        if (job.es) { job.es.close(); job.es = null; }
        let info = {}; try { info = JSON.parse(e.data); } catch (_) {}
        job.failed = info.status === "failed";
        if (job.failed && info.error) pushLine(info.error);
        job.done = true; paintBar(); reload();
      });
      job.es.onerror = () => {
        if (job.es) { job.es.close(); job.es = null; }
        if (!job.done) { job.failed = true; job.done = true; pushLine("connection lost"); paintBar(); reload(); }
      };
    } catch (e) { job.failed = true; job.done = true; paintBar(); }
  }
  function pushLine(line) {
    if (!job) return;
    job.lines.push(line);
    if (sbLine) sbLine.textContent = line;
    if (detailOpen) paintDetail();
  }
  function paintBar() {
    if (!statusbar || !job) return;
    statusbar.className = "statusbar show" + (job.done ? (job.failed ? " err" : " ok") : "");
    if (sbTitle) sbTitle.textContent = job.title + (job.done ? (job.failed ? " — failed" : " — done") : "");
    if (sbLine && !job.lines.length) sbLine.textContent = job.done ? "" : "starting…";
    if (detailOpen) paintDetail();
    if (job.done) { clearTimeout(sbHide); sbHide = setTimeout(() => { if (statusbar) statusbar.className = "statusbar"; }, job.failed ? 14000 : 4500); }
  }
  function openJobDetail() {
    if (!job) return;
    detailOpen = true;
    scrim.innerHTML = `
      <div class="dialog">
        <div class="dialog-head"><h3 id="jmTitle"></h3></div>
        <div class="dialog-body">
          <div class="jobbar" id="jmBar"><div class="jobbar-fill"></div></div>
          <pre class="joblog" id="jmLog"></pre>
        </div>
        <div class="dialog-foot"><button class="btn" data-dialog-close>Close</button></div>
      </div>`;
    scrim.classList.add("open");
    scrim.querySelector("[data-dialog-close]").addEventListener("click", closeDialog);
    paintDetail();
  }
  function paintDetail() {
    if (!job) return;
    const t = document.getElementById("jmTitle"); if (t) t.textContent = job.title + (job.done ? (job.failed ? " — failed" : " — done") : " — running");
    const bar = document.getElementById("jmBar"); if (bar) bar.className = "jobbar" + (job.done ? (job.failed ? " err" : " ok") : "");
    const log = document.getElementById("jmLog"); if (log) { log.textContent = job.lines.join("\n"); log.scrollTop = log.scrollHeight; }
  }
  if (statusbar) statusbar.addEventListener("click", openJobDetail);

  // wireVersionField turns a text input into a searchable version combobox:
  // a datalist of quick picks, refreshed with live tag-search matches as you
  // type. Free text is allowed, so any exact tag (e.g. mysql 8.0.35) works.
  let _dlSeq = 0;
  function wireVersionField(input, picks, search) {
    if (!input) return;
    const dl = document.createElement("datalist");
    dl.id = "dl" + (++_dlSeq);
    input.setAttribute("list", dl.id);
    input.setAttribute("autocomplete", "off");
    (input.parentNode || input).appendChild(dl);
    const fill = (tags) => { dl.innerHTML = (tags || []).map(t => `<option value="${t}"></option>`).join(""); };
    const refresh = () => Promise.resolve(picks()).then(fill);
    refresh();
    let t = null;
    input.addEventListener("input", () => {
      const q = input.value.trim();
      clearTimeout(t);
      if (q.length < 1) { refresh(); return; }
      t = setTimeout(() => Promise.resolve(search(q)).then(fill), 220);
    });
    return refresh; // callers can re-fill quick picks (e.g. engine changed)
  }

  // Recently-used sites (most-recent first), persisted across launches.
  function recentSites() { try { return JSON.parse(localStorage.getItem("hull-recent") || "[]"); } catch (e) { return []; } }
  function touchSite(name) {
    if (!name) return;
    const list = [name, ...recentSites().filter(n => n !== name)].slice(0, 12);
    localStorage.setItem("hull-recent", JSON.stringify(list));
  }

  window.App = {
    go, toast, copyText, openDialog, closeDialog, openExternal, pick,
    api, reload, act, recentSites, touchSite, wireVersionField,
    siteCount: () => window.HULL.ROOTS.reduce((n, r) => n + r.managed.length, 0),
    serviceRunning: () => { const s = window.HULL.SERVICES; return { on: s.filter(x => x.status === "running").length, total: s.length }; },
    readPicks, onLeave: fn => cleanups.push(fn), theme: { get: currentTheme, set: setTheme },
  };

  /* ---------------- DAEMON HEALTH POPOVER + PILL ---------------- */
  const pop = document.getElementById("daemonPop");
  function buildPopover() {
    if (!pop) return;
    const sites = window.HULL.ROOTS.flatMap(r => r.managed);
    const on = sites.filter(s => s.status === "running").length;
    const dotFor = s => s === "ok" ? "dot-on" : s === "warn" ? "dot-warn" : "dot-err";
    pop.innerHTML = `<div class="dp-head">System health<span class="dp-up">${on}/${sites.length} sites</span></div>` +
      window.HULL.HEALTH.map(h => `<div class="dp-row"><span class="dot ${dotFor(h.status)}"></span><span class="dp-name">${h.name}</span><span class="dp-detail">${h.detail}</span></div>`).join("");
  }
  function setPill(on, text) {
    if (!daemonPill) return;
    daemonPill.innerHTML = `<span class="dot ${on ? "dot-on pulse" : "dot-err"}"></span><span>${text}</span>`;
  }

  /* ---------------- OFFLINE ---------------- */
  function showOffline(detail) {
    connected = false;
    setPill(false, "Daemon offline");
    main.innerHTML = `
      <div class="page"><div class="page-body center">
        <div class="empty" style="max-width:380px">
          <div class="ic">${window.icon("services", 22)}</div>
          <h2>Daemon not running</h2>
          <p>Hull's background service powers your sites and this window.${detail ? `<br><span class="mono faint" style="font-size:11px">${detail}</span>` : ""}</p>
          <div style="display:flex;gap:8px;justify-content:center">
            <button class="btn btn-primary" id="startDaemon">${window.icon("play", 15)}Start daemon</button>
            <button class="btn" id="retry">Retry</button>
          </div>
        </div>
      </div>`;
    document.getElementById("retry").addEventListener("click", boot);
    document.getElementById("startDaemon").addEventListener("click", async (e) => {
      e.target.disabled = true;
      try { await window.__TAURI__.core.invoke("start_daemon"); } catch (err) {}
      for (let i = 0; i < 16; i++) { await new Promise(r => setTimeout(r, 600)); if (await tryConnect()) return; }
      e.target.disabled = false;
    });
  }

  /* ---------------- BOOT ---------------- */
  let events = null;
  let reconnectTimer = null;
  function startEvents() {
    if (events) events.close();
    try {
      events = new EventSource(window.HULL.sseURL("/v1/events"));
      events.onmessage = () => reload();
      events.onerror = () => {
        if (events) { events.close(); events = null; }
        connected = false;
        scheduleReconnect();
      };
    } catch (e) {}
  }
  // Persistent reconnect — keep trying until the daemon is back (survives a
  // daemon restart without dropping the user to the offline screen).
  function scheduleReconnect() {
    if (reconnectTimer) return;
    setPill(false, "Reconnecting…");
    reconnectTimer = setTimeout(async () => {
      reconnectTimer = null;
      if (!(await tryConnect())) scheduleReconnect();
    }, 1200);
  }
  async function tryConnect() {
    try {
      await window.HULL.connect();
      connected = true;
      setPill(true, "Daemon running");
      buildPopover();
      ensureRoute();
      renderNav();
      mount(route);
      startEvents();
      return true;
    } catch (e) { return false; }
  }
  async function boot() {
    setPill(false, "Connecting…");
    if (!(await tryConnect())) showOffline();
  }
  // Clicking the daemon pill forces an immediate reconnect attempt.
  if (daemonPill) daemonPill.addEventListener("click", () => { if (!connected) { if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; } tryConnect().then(ok => { if (!ok) scheduleReconnect(); }); } });

  renderNav();
  boot();
})();
