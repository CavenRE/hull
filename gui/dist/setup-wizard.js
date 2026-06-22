/* Hull — first-run setup wizard. Shown once (no ~/.hull/config.yaml yet):
   a guided "going through the motions" flow that collects the base IP, TLD,
   project folder and starter services, then writes config (which flips
   first_run off) and provisions everything. Drives the same daemon endpoints
   as Settings (PUT /v1/config, POST /v1/services, POST /v1/setup/reapply). */
(function () {
  const H = () => window.HULL;

  // Curated starter services — the common shared backends. Versions match the
  // catalog defaults; the user can add anything else later in Services.
  const STARTERS = [
    { engine: "postgres", version: "16",     icon: "database", name: "PostgreSQL", blurb: "Relational database", on: false },
    { engine: "mysql",    version: "8.4",    icon: "database", name: "MySQL",      blurb: "Relational database", on: false },
    { engine: "redis",    version: "alpine", icon: "cache",    name: "Redis",      blurb: "In-memory cache & queues", on: false },
    { engine: "mailpit",  version: "latest", icon: "mail",     name: "Mailpit",    blurb: "Catches outgoing email", on: true },
  ];

  const STEPS = [
    { id: "welcome",  label: "Welcome",  icon: "sites" },
    { id: "docker",   label: "Docker",   icon: "cube" },
    { id: "folder",   label: "Projects", icon: "folder" },
    { id: "domain",   label: "Domain",   icon: "globe" },
    { id: "services", label: "Services", icon: "services" },
    { id: "finish",   label: "Finish",   icon: "check" },
  ];

  let st, host, onDone, injected = false;

  function injectCSS() {
    if (injected) return; injected = true;
    const css = `
    body.setup-mode .sidebar { display:none; }
    body.setup-mode .main { left:0; }
    .wz { position:absolute; inset:0; display:flex; background:var(--bg-app); overflow:hidden;
          animation:wz-in .4s ease both; }
    @keyframes wz-in { from{opacity:0} to{opacity:1} }
    /* Left brand rail */
    .wz-rail { width:248px; flex:0 0 248px; padding:34px 26px; display:flex; flex-direction:column;
               background:var(--bg-panel); border-right:1px solid var(--border); color:var(--text); }
    .wz-brand { display:flex; align-items:center; gap:11px; margin-bottom:34px; }
    .wz-brand img { width:32px; height:32px; }
    .wz-brand span { font-size:21px; font-weight:700; letter-spacing:-.02em; color:var(--text); }
    .wz-steps { display:flex; flex-direction:column; gap:3px; }
    .wz-st { display:flex; align-items:center; gap:11px; padding:9px 11px; border-radius:9px;
             font-size:var(--fs-13); font-weight:500; color:var(--text-dim); transition:.18s; }
    .wz-st.on { background:var(--accent-soft); color:var(--accent-text); }
    .wz-st.done { color:var(--text); }
    .wz-st .wz-dot { width:22px; height:22px; flex:0 0 22px; border-radius:50%; display:grid; place-items:center;
                     background:var(--bg-card-2); color:var(--text-dim); }
    .wz-st.on .wz-dot { background:var(--accent); color:var(--text-on-accent); }
    .wz-st.done .wz-dot { background:var(--green); color:#fff; }
    .wz-st svg { width:13px; height:13px; }
    .wz-rail-foot { margin-top:auto; font-size:var(--fs-12); color:var(--text-faint); line-height:1.5; }
    /* Right panel */
    .wz-main { flex:1; display:flex; flex-direction:column; min-width:0; }
    .wz-head { height:46px; flex:0 0 46px; }            /* drag region under winctrls */
    .wz-body { flex:1; overflow:auto; padding:8px 56px 24px; }
    .wz-pane { max-width:560px; margin:0 auto; animation:wz-slide .32s ease both; }
    @keyframes wz-slide { from{opacity:0; transform:translateY(8px)} to{opacity:1; transform:none} }
    .wz-eyebrow { font-size:var(--fs-12); font-weight:700; letter-spacing:.08em; text-transform:uppercase;
                  color:var(--accent); margin-bottom:9px; }
    .wz-h { font-size:25px; font-weight:700; letter-spacing:-.02em; margin:0 0 8px; }
    .wz-sub { color:var(--text-dim); font-size:var(--fs-14); line-height:1.55; margin:0 0 26px; max-width:460px; }
    .wz-foot { flex:0 0 auto; display:flex; align-items:center; gap:10px; padding:16px 56px;
               border-top:1px solid var(--border); }
    .wz-foot .spacer { flex:1; }
    .wz-hero { display:grid; place-items:center; text-align:center; padding-top:24px; }
    .wz-hero img { width:78px; height:78px; margin-bottom:22px;
                   filter:drop-shadow(0 8px 24px color-mix(in srgb,var(--accent) 40%, transparent)); }
    /* Choice cards */
    .wz-choice { display:flex; align-items:center; gap:14px; padding:15px 17px; border:1px solid var(--border);
                 border-radius:12px; margin-bottom:11px; background:var(--bg-card); cursor:pointer; transition:.16s; }
    .wz-choice:hover { border-color:var(--accent); }
    .wz-choice.sel { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-ring); }
    .wz-choice .ic { width:40px; height:40px; flex:0 0 40px; border-radius:10px; display:grid; place-items:center;
                     background:var(--accent-soft); color:var(--accent-text); }
    .wz-choice .ic svg { width:19px; height:19px; }
    .wz-choice .gro { flex:1; min-width:0; }
    .wz-choice .gro .nm { font-weight:600; font-size:var(--fs-14); }
    .wz-choice .gro .bl { color:var(--text-dim); font-size:var(--fs-13); margin-top:1px; }
    /* status banner */
    .wz-banner { display:flex; align-items:center; gap:13px; padding:16px 18px; border-radius:12px;
                 border:1px solid var(--border); background:var(--bg-card); margin-bottom:18px; }
    .wz-banner .bic { width:38px; height:38px; flex:0 0 38px; border-radius:10px; display:grid; place-items:center; }
    .wz-banner.ok  .bic { background:color-mix(in srgb,var(--green) 16%,transparent); color:var(--green); }
    .wz-banner.bad .bic { background:color-mix(in srgb,var(--amber) 18%,transparent); color:var(--amber); }
    .wz-banner .bt { font-weight:600; font-size:var(--fs-14); }
    .wz-banner .bd { color:var(--text-dim); font-size:var(--fs-13); margin-top:1px; }
    .wz-pathbox { display:flex; align-items:center; gap:10px; padding:13px 15px; border:1px solid var(--border);
                  border-radius:11px; background:var(--bg-card); font-family:var(--font-mono); font-size:var(--fs-13);
                  word-break:break-all; }
    .wz-pathbox .pic { color:var(--accent); flex:0 0 auto; }
    .wz-pathbox .pp { flex:1; min-width:0; color:var(--text); }
    .wz-preview { margin-top:20px; padding:15px 17px; border-radius:11px; background:var(--bg-inset);
                  border:1px dashed var(--border); font-size:var(--fs-13); color:var(--text-dim); }
    .wz-preview b { color:var(--text); font-family:var(--font-mono); }
    /* apply checklist */
    .wz-task { display:flex; align-items:center; gap:12px; padding:12px 2px; }
    .wz-task .tk { width:24px; height:24px; flex:0 0 24px; border-radius:50%; display:grid; place-items:center;
                   border:2px solid var(--border); color:var(--text-faint); }
    .wz-task.run .tk { border-color:var(--accent); color:var(--accent); animation:wz-spin .9s linear infinite; }
    .wz-task.ok  .tk { border-color:var(--green); background:var(--green); color:#fff; animation:none; }
    .wz-task.warn .tk { border-color:var(--amber); color:var(--amber); }
    @keyframes wz-spin { to { transform:rotate(360deg) } }
    .wz-task .tl { font-size:var(--fs-14); font-weight:500; }
    .wz-task .td { font-size:var(--fs-12); color:var(--text-dim); margin-top:1px; }
    .wz-manual { margin-top:16px; padding:14px 16px; border-radius:11px; border:1px solid var(--border);
                 background:var(--bg-card); }
    .wz-manual .mh { font-size:var(--fs-12); color:var(--text-dim); margin-bottom:8px; }
    .wz-manual code { display:flex; align-items:center; justify-content:space-between; gap:10px; font-family:var(--font-mono);
                      font-size:var(--fs-13); background:var(--bg-app); border:1px solid var(--border); border-radius:8px;
                      padding:8px 11px; margin-bottom:6px; }
    `;
    const s = document.createElement("style"); s.id = "wz-css"; s.textContent = css;
    document.head.appendChild(s);
  }

  // ---------- public entry ----------
  window.renderWizard = function (mainEl, opts) {
    injectCSS();
    onDone = (opts && opts.onDone) || function () {};
    const cfg = (opts && opts.config) || {};
    st = {
      step: 0,
      docker: null,
      folder: (cfg.roots && cfg.roots[0]) || "",
      tld: (cfg.tld || "test").replace(/^\./, ""),
      octet: parseInt(String(cfg.loopback || "127.0.0.1").split(".")[3], 10) || 1,
      services: STARTERS.map(s => Object.assign({}, s)),
      applied: false,
    };
    host = mainEl;
    refreshDocker();   // kick off a live Docker probe in the background
    render();
  };

  async function refreshDocker() {
    try {
      const deps = await App.api("GET", "/v1/dependencies");
      st.docker = (deps || []).find(d => d.key === "docker") || null;
    } catch (e) { st.docker = null; }
    if (st && STEPS[st.step].id === "docker") render();
  }

  function render() {
    const cur = STEPS[st.step];
    host.innerHTML = `
      <div class="wz">
        <aside class="wz-rail">
          <div class="wz-brand"><img src="logo.svg" alt=""><span>hull</span></div>
          <div class="wz-steps">
            ${STEPS.map((s, i) => `<div class="wz-st ${i === st.step ? "on" : ""} ${i < st.step ? "done" : ""}">
              <span class="wz-dot">${i < st.step ? icon("check", 12) : icon(s.icon, 12)}</span>${s.label}</div>`).join("")}
          </div>
          <div class="wz-rail-foot">Local-first development.<br>No accounts, no cloud — everything runs on your machine.</div>
        </aside>
        <div class="wz-main">
          <div class="wz-head page-head" style="border:none"></div>
          <div class="wz-body"><div class="wz-pane" id="wzPane">${paneHTML(cur.id)}</div></div>
          <div class="wz-foot" id="wzFoot">${footHTML(cur.id)}</div>
        </div>
      </div>`;
    wire(cur.id);
  }

  function paneHTML(id) {
    switch (id) {
      case "welcome": return `
        <div class="wz-hero">
          <img src="logo.svg" alt="">
          <div class="wz-h">Welcome to Hull</div>
          <p class="wz-sub" style="margin-inline:auto">A local environment for your sites and apps — automatic HTTPS,
            shared databases, and a real domain for every project. Let's get a few basics set up.</p>
        </div>`;

      case "docker": {
        const d = st.docker;
        const ready = d && (d.status === "ok" || d.status === "embedded");
        const checking = d === null;
        return `
          <div class="wz-eyebrow">System check</div>
          <div class="wz-h">Docker</div>
          <p class="wz-sub">Hull runs your sites in containers, so it needs Docker. It's the only thing Hull
            doesn't bundle itself.</p>
          <div class="wz-banner ${ready ? "ok" : "bad"}">
            <span class="bic">${icon(ready ? "check" : "alert", 19)}</span>
            <div style="flex:1">
              <div class="bt">${checking ? "Checking for Docker…" : ready ? "Docker is ready" : "Docker not detected"}</div>
              <div class="bd">${checking ? "Looking for a running engine…"
                : ready ? (d.name + (d.version ? " · " + d.version : ""))
                : "Install Docker Desktop, start it, then re-check."}</div>
            </div>
            ${!ready && !checking ? `<button class="btn btn-sm" id="wzRecheck">${icon("restart", 13)}Re-check</button>` : ""}
          </div>
          ${(!ready && !checking) ? `<button class="btn btn-primary" id="wzGetDocker">${icon("cube", 15)}Get Docker Desktop</button>
            <p class="help" style="margin-top:12px">You can continue setup now and install Docker afterwards —
            sites just won't start until it's running.</p>` : ""}`;
      }

      case "folder": return `
        <div class="wz-eyebrow">Where your projects live</div>
        <div class="wz-h">Projects folder</div>
        <p class="wz-sub">Hull scans this folder for sites and offers it as the home for new ones.
          You can add more folders later in Settings.</p>
        <div class="wz-pathbox">
          <span class="pic">${icon("folder", 17)}</span>
          <span class="pp" id="wzFolderPath">${st.folder || "Choose a folder…"}</span>
          <button class="btn btn-sm" id="wzPickFolder">Change…</button>
        </div>`;

      case "domain": return `
        <div class="wz-eyebrow">How your sites are reached</div>
        <div class="wz-h">Local domain</div>
        <p class="wz-sub">Every project gets a real hostname with trusted HTTPS. Pick the suffix, and the
          loopback address Hull binds — change the address only if another local proxy already uses port 80/443.</p>
        <div class="form-row">
          <label class="field-label">Top-level domain</label>
          <input class="input mono" id="wzTld" value=".${st.tld}" style="width:160px">
        </div>
        <div class="form-row" style="margin-top:16px">
          <label class="field-label">Loopback address</label>
          <div class="addr">
            <span class="octet ro">127</span><span class="octet-sep">.</span><span class="octet ro">0</span><span class="octet-sep">.</span><span class="octet ro">0</span><span class="octet-sep">.</span>
            <span class="octet-edit"><input class="octet-input mono" id="wzOctet" value="${st.octet}" readonly aria-label="Last octet (1–8)">
              <span class="octet-steps">
                <button type="button" data-step="up" aria-label="Increase"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m6 15 6-6 6 6"/></svg></button>
                <button type="button" data-step="down" aria-label="Decrease"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg></button>
              </span></span>
          </div>
        </div>
        <div class="wz-preview" id="wzDomainPreview"></div>`;

      case "services": return `
        <div class="wz-eyebrow">Optional starters</div>
        <div class="wz-h">Base services</div>
        <p class="wz-sub">Shared databases and tools, available to every project. Pick any you know you'll want —
          you can add or remove these anytime in Services.</p>
        ${st.services.map((s, i) => `
          <div class="wz-choice ${s.on ? "sel" : ""}" data-svc="${i}">
            <span class="ic">${icon(s.icon, 19)}</span>
            <div class="gro"><div class="nm">${s.name}</div><div class="bl">${s.blurb}</div></div>
            <label class="switch" onclick="event.stopPropagation()"><input type="checkbox" data-svc-cb="${i}" ${s.on ? "checked" : ""}><span class="track"></span></label>
          </div>`).join("")}`;

      case "finish": return st.applied ? applyHTML() : `
        <div class="wz-eyebrow">Almost there</div>
        <div class="wz-h">Review &amp; finish</div>
        <p class="wz-sub">Here's what Hull will set up. You can change any of it later in Settings.</p>
        <div class="wz-banner ok"><span class="bic">${icon("folder", 19)}</span><div><div class="bt">Projects</div><div class="bd mono">${st.folder}</div></div></div>
        <div class="wz-banner ok"><span class="bic">${icon("globe", 19)}</span><div><div class="bt">Domain &amp; address</div><div class="bd mono">*.${st.tld} → 127.0.0.${st.octet}</div></div></div>
        <div class="wz-banner ok"><span class="bic">${icon("services", 19)}</span><div><div class="bt">Base services</div><div class="bd">${st.services.filter(s => s.on).map(s => s.name).join(", ") || "None — add them later"}</div></div></div>`;

      default: return "";
    }
  }

  function applyHTML() {
    const tasks = st.tasks || [];
    return `
      <div class="wz-eyebrow">Setting things up</div>
      <div class="wz-h">${st.done ? "You're all set" : "Applying your setup…"}</div>
      <p class="wz-sub">${st.done ? "Hull is configured and ready. Service images keep downloading in the background." : "This only takes a moment."}</p>
      <div>${tasks.map(t => `
        <div class="wz-task ${t.state}">
          <span class="tk">${t.state === "ok" ? icon("check", 13) : t.state === "warn" ? icon("alert", 12) : t.state === "run" ? icon("restart", 12) : ""}</span>
          <div><div class="tl">${t.label}</div>${t.detail ? `<div class="td">${t.detail}</div>` : ""}</div>
        </div>`).join("")}</div>
      ${st.manual && st.manual.length ? `<div class="wz-manual"><div class="mh">Two steps need a terminal with admin rights — run these once:</div>
        ${st.manual.map(m => `<code><span>${m}</span><button class="btn btn-sm btn-icon" data-copy="${m.replace(/"/g, "&quot;")}">${icon("copy", 13)}</button></code>`).join("")}</div>` : ""}`;
  }

  function footHTML(id) {
    if (id === "finish") {
      if (st.done) return `<div class="spacer"></div><button class="btn btn-primary" id="wzOpen">${icon("play", 15)}Open Hull</button>`;
      if (st.applied) return `<div class="spacer"></div><button class="btn" disabled>Working…</button>`;
      return `<button class="btn" id="wzBack">Back</button><div class="spacer"></div>
              <button class="btn btn-primary" id="wzApply">${icon("check", 15)}Apply &amp; finish</button>`;
    }
    const back = st.step === 0 ? "" : `<button class="btn" id="wzBack">Back</button>`;
    const next = id === "welcome" ? "Get started" : "Continue";
    return `${back}<div class="spacer"></div><button class="btn btn-primary" id="wzNext">${next} ${icon("chevright", 14)}</button>`;
  }

  function wire(id) {
    host.querySelector("#wzNext")?.addEventListener("click", () => go(1));
    host.querySelector("#wzBack")?.addEventListener("click", () => go(-1));

    if (id === "docker") {
      host.querySelector("#wzRecheck")?.addEventListener("click", () => { st.docker = null; render(); refreshDocker(); });
      host.querySelector("#wzGetDocker")?.addEventListener("click", () => App.openExternal((st.docker && st.docker.install_url) || "https://www.docker.com/products/docker-desktop/"));
    }
    if (id === "folder") {
      host.querySelector("#wzPickFolder")?.addEventListener("click", async () => {
        const p = await App.pick("folder", { title: "Choose a folder for your projects" });
        if (p) { st.folder = p; host.querySelector("#wzFolderPath").textContent = p; }
      });
    }
    if (id === "domain") {
      const tldIn = host.querySelector("#wzTld");
      const oct = host.querySelector("#wzOctet");
      const preview = () => {
        const t = (tldIn.value || "").trim().replace(/^\.+/, "").toLowerCase() || "test";
        host.querySelector("#wzDomainPreview").innerHTML =
          `A project named <b>shop</b> will be reachable at <b>https://shop.${t}</b>, served from <b>127.0.0.${st.octet}</b>.`;
      };
      const clamp = v => Math.max(1, Math.min(8, isNaN(v) ? 1 : v));
      host.querySelectorAll("[data-step]").forEach(b => b.addEventListener("click", () => {
        st.octet = clamp(st.octet + (b.dataset.step === "up" ? 1 : -1)); oct.value = st.octet; preview();
      }));
      tldIn.addEventListener("input", preview);
      tldIn.addEventListener("change", () => { st.tld = (tldIn.value || "").trim().replace(/^\.+/, "").toLowerCase() || "test"; tldIn.value = "." + st.tld; preview(); });
      preview();
    }
    if (id === "services") {
      host.querySelectorAll("[data-svc]").forEach(card => card.addEventListener("click", () => {
        const i = +card.dataset.svc; st.services[i].on = !st.services[i].on;
        card.classList.toggle("sel", st.services[i].on);
        const cb = card.querySelector("[data-svc-cb]"); if (cb) cb.checked = st.services[i].on;
      }));
      host.querySelectorAll("[data-svc-cb]").forEach(cb => cb.addEventListener("change", () => {
        const i = +cb.dataset.svcCb; st.services[i].on = cb.checked;
        cb.closest(".wz-choice").classList.toggle("sel", cb.checked);
      }));
    }
    if (id === "finish") {
      host.querySelector("#wzApply")?.addEventListener("click", apply);
      host.querySelector("#wzOpen")?.addEventListener("click", () => onDone(st.restart));
      host.querySelectorAll("[data-copy]").forEach(b => b.addEventListener("click", () => App.copyText(b.dataset.copy, b)));
    }
  }

  function go(delta) {
    // Validate folder before leaving its step.
    if (delta > 0 && STEPS[st.step].id === "folder" && !st.folder) { App.toast("Choose a projects folder first"); return; }
    st.step = Math.max(0, Math.min(STEPS.length - 1, st.step + delta));
    render();
  }

  // ---------- the apply sequence ----------
  async function apply() {
    st.applied = true; st.done = false; st.restart = false; st.manual = [];
    const chosen = st.services.filter(s => s.on);
    st.tasks = [
      { key: "config",   label: "Saving configuration",  state: "run", detail: "" },
      { key: "services", label: chosen.length ? `Provisioning ${chosen.length} service${chosen.length === 1 ? "" : "s"}` : "Services", state: "idle", detail: chosen.length ? "" : "none selected" },
      { key: "secure",   label: "Local HTTPS & DNS",      state: "idle", detail: "" },
    ];
    render();
    const set = (key, state, detail) => { const t = st.tasks.find(x => x.key === key); if (t) { t.state = state; if (detail != null) t.detail = detail; } render(); };

    // 1) config — writes config.yaml (flips first_run off).
    try {
      const body = {
        tld: st.tld,
        roots: [st.folder],
        loopback: "127.0.0." + st.octet,
        defaults: { php: "", editor: "", db_tool: "tableplus" },
      };
      const res = await App.api("PUT", "/v1/config", body);
      st.restart = !!(res && res.restart_required && res.restart_required.length);
      set("config", "ok", "saved" + (st.restart ? " · restart to apply address/domain" : ""));
    } catch (e) { set("config", "warn", e.message || "failed"); }

    // 2) starter services — fire each (image pulls run as background jobs).
    if (chosen.length) {
      set("services", "run");
      let okN = 0;
      for (const s of chosen) {
        try { await App.api("POST", "/v1/services", { engine: s.engine, version: s.version }); okN++; }
        catch (e) { /* keep going */ }
      }
      set("services", okN === chosen.length ? "ok" : "warn", `${okN}/${chosen.length} starting · images download in the background`);
    } else {
      set("services", "ok", "none selected");
    }

    // 3) local CA / trust / DNS — best effort; surface anything needing admin.
    set("secure", "run");
    try {
      const res = await App.api("POST", "/v1/setup/reapply");
      const steps = (res && res.steps) || [];
      const manual = steps.filter(s => s.status === "manual").map(s => s.manual).filter(Boolean);
      st.manual = [...new Set(manual)];
      const okN = steps.filter(s => s.status === "ok").length;
      set("secure", st.manual.length ? "warn" : "ok", st.manual.length ? `${okN} applied · ${st.manual.length} need a terminal` : `${okN} steps applied`);
    } catch (e) { set("secure", "warn", e.message || "skipped"); }

    st.done = true;
    render();
  }
})();
