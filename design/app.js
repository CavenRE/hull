/* Hull , app shell: routing, theme, nav, shared utilities. */
(function () {
  const NAV = [
    { id: "dashboard", label: "Dashboard",    icon: "dashboard" },
    { id: "sites",     label: "Sites & Apps", icon: "sites" },
    { id: "services",  label: "Services",     icon: "services" },
    { sep: true },
    { id: "mail",      label: "Mail",         icon: "mail" },
    { id: "logs",      label: "Logs",         icon: "logs" },
    { id: "settings",  label: "Settings",     icon: "settings" },
  ];

  const main = document.getElementById("main");
  const navEl = document.getElementById("nav");
  const scrim = document.getElementById("scrim");
  const toastEl = document.getElementById("toast");

  let route = "sites";
  let cleanups = [];

  /* ---------------- THEME ---------------- */
  function currentTheme() { return localStorage.getItem("hull-theme") || "auto"; }
  function applyTheme(t) {
    if (t === "auto") document.documentElement.removeAttribute("data-theme");
    else document.documentElement.setAttribute("data-theme", t);
  }
  function setTheme(t) { localStorage.setItem("hull-theme", t); applyTheme(t); }
  applyTheme(currentTheme());

  /* ---------------- WINDOW CONTROLS ---------------- */
  let maximized = false;
  document.querySelectorAll(".winbtn").forEach(b => b.addEventListener("click", () => {
    const act = b.dataset.win;
    const tw = window.__TAURI__ && window.__TAURI__.window;
    const win = tw && tw.getCurrentWindow ? tw.getCurrentWindow() : (tw && tw.appWindow);
    if (act === "min") { win && win.minimize && win.minimize(); toast("Minimize"); }
    else if (act === "max") {
      maximized = !maximized;
      win && win.toggleMaximize && win.toggleMaximize();
      const mx = document.querySelector('[data-win="max"]');
      mx.innerHTML = maximized
        ? `<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="8" width="11" height="11" rx="1.5"/><path d="M8 8V6.5A1.5 1.5 0 0 1 9.5 5H17.5A1.5 1.5 0 0 1 19 6.5V14.5A1.5 1.5 0 0 1 17.5 16H16"/></svg>`
        : `<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="5" width="14" height="14" rx="1.5"/></svg>`;
      toast(maximized ? "Maximized" : "Restored");
    }
    else if (act === "close") { win && win.close && win.close(); toast("Close"); }
  }));

  /* ---------------- NAV ---------------- */
  function siteCount() {
    return window.HULL.ROOTS.reduce((n, r) => n + r.managed.length, 0);
  }
  function serviceRunning() {
    const s = window.HULL.SERVICES;
    return { on: s.filter(x => x.status === "running").length, total: s.length };
  }
  function renderNav() {
    const counts = { sites: siteCount(), services: window.HULL.SERVICES.length };
    navEl.innerHTML = NAV.map(item => {
      if (item.sep) return `<div class="nav-sep"></div>`;
      const active = item.id === route ? " active" : "";
      const count = counts[item.id] != null ? `<span class="nav-count">${counts[item.id]}</span>` : "";
      return `<button class="nav-item${active}" data-route="${item.id}">${window.icon(item.icon)}<span>${item.label}</span>${count}</button>`;
    }).join("");
  }
  navEl.addEventListener("click", e => {
    const b = e.target.closest("[data-route]");
    if (b) go(b.dataset.route);
  });

  /* ---------------- ROUTING ---------------- */
  function go(r) {
    if (r === route && main.firstChild) return;
    cleanups.forEach(fn => { try { fn(); } catch (e) {} });
    cleanups = [];
    route = r;
    renderNav();
    const screen = document.createElement("div");
    screen.className = "screen";
    main.innerHTML = "";
    main.appendChild(screen);
    renderInto(screen, r);
  }
  function renderInto(el, r) {
    switch (r) {
      case "sites":     window.renderSites(el); break;
      case "dashboard": window.renderDashboard(el); break;
      case "services":  window.renderServices(el); break;
      case "mail":      window.renderMail(el); break;
      case "logs":      window.renderLogs(el); break;
      case "settings":  window.renderSettings(el); break;
      default:          renderStub(el, r); break;
    }
  }
  function renderStub(el, r) {
    const titles = { mail: "Mail", logs: "Logs", settings: "Settings" };
    const blurbs = {
      mail: "SMTP integration surface , wire apps to Mailpit and open the inbox.",
      logs: "Live tailing of any running project or service.",
      settings: "Project folders, defaults, local domain, and the Doctor panel.",
    };
    el.innerHTML = `
      <div class="page">
        <div class="page-head"><h1>${titles[r]}</h1></div>
        <div class="page-body center">
          <div class="empty">
            <div class="ic">${window.icon(r, 22)}</div>
            <h2>${titles[r]} , coming next</h2>
            <p>${blurbs[r]} Designed in a later drop; the route is wired so navigation feels complete.</p>
          </div>
        </div>
      </div>`;
  }

  /* ---------------- SHARED UTILITIES ---------------- */
  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.add("show");
    clearTimeout(toast._t);
    toast._t = setTimeout(() => toastEl.classList.remove("show"), 1700);
  }
  function copyText(text, btn) {
    const done = () => {
      if (btn) { btn.classList.add("copied"); setTimeout(() => btn.classList.remove("copied"), 1000); }
      toast("Copied to clipboard");
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(() => fallbackCopy(text, done));
    } else fallbackCopy(text, done);
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
  function closeDialog() { scrim.classList.remove("open"); setTimeout(() => { scrim.innerHTML = ""; }, 200); }
  scrim.addEventListener("click", e => { if (e.target === scrim) closeDialog(); });
  document.addEventListener("keydown", e => { if (e.key === "Escape" && scrim.classList.contains("open")) closeDialog(); });

  // Delegated wiring for picker rows + advanced collapsibles (used inside dialogs)
  document.addEventListener("click", e => {
    const adv = e.target.closest(".adv-toggle");
    if (adv) {
      adv.classList.toggle("open");
      const body = adv.nextElementSibling;
      if (body && body.classList.contains("adv-body")) body.classList.toggle("open");
      return;
    }
    const row = e.target.closest(".pick-row");
    if (row && !e.target.closest(".pick-ver")) row.classList.toggle("sel");
  });
  function readPicks(scope) {
    return [...scope.querySelectorAll(".pick-row.sel")].map(r => ({
      key: r.dataset.key,
      label: r.dataset.label || r.dataset.key,
      version: (r.querySelector(".pick-ver") || {}).value || null,
    }));
  }

  // Expose to screen modules
  window.App = { go, toast, copyText, openDialog, closeDialog, siteCount, serviceRunning, readPicks, onLeave: fn => cleanups.push(fn), theme: { get: currentTheme, set: setTheme } };

  /* ---------------- DAEMON HEALTH POPOVER ---------------- */
  (function () {
    const pop = document.getElementById("daemonPop");
    if (!pop) return;
    const sites = window.HULL.ROOTS.flatMap(r => r.managed);
    const on = sites.filter(s => s.status === "running").length;
    const dotFor = s => s === "ok" ? "dot-on" : s === "warn" ? "dot-warn" : "dot-err";
    pop.innerHTML = `<div class="dp-head">System health<span class="dp-up">${on}/${sites.length} sites</span></div>` +
      window.HULL.HEALTH.map(h => `<div class="dp-row"><span class="dot ${dotFor(h.status)}"></span><span class="dp-name">${h.name}</span><span class="dp-detail">${h.detail}</span></div>`).join("");
  })();

  /* ---------------- BOOT ---------------- */
  renderNav();
  go("sites");
})();
