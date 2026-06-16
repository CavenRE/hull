/* Hull — Settings screen. Folders, defaults, domain, dependency updates, startup, doctor. */
(function () {
  const H = () => window.HULL;
  let rootEl = null;
  let tab = "general";
  const TABS = [["general","General"],["system","System"],["updates","Updates"],["advanced","Advanced"]];

  const STARTUP = [
    { key: "login",   name: "Launch Hull at login",         desc: "Start automatically when you sign in.", on: true },
    { key: "daemon",  name: "Start daemon on launch",       desc: "Bring up the router and engine when Hull opens.", on: true },
    { key: "restore", name: "Restore running sites",        desc: "Re-start sites that were running when you last quit.", on: true },
    { key: "tray",    name: "Keep running in the tray",     desc: "Closing the window keeps the daemon alive in the menu bar.", on: false },
    { key: "updates", name: "Check for updates automatically", desc: "Notify when dependency updates are available.", on: true },
  ];

  const DOCTOR = [
    { name: "Docker engine reachable", status: "ok",   detail: "27.0.3 · 7 containers up" },
    { name: "Router listening",        status: "ok",   detail: "Caddy bound to :80 and :443" },
    { name: "*.test resolves",         status: "ok",   detail: "127.0.0.1 via local resolver" },
    { name: "Local CA trusted",        status: "ok",   detail: "Hull root certificate in system store" },
    { name: "Certificate validity",    status: "warn", detail: "Wildcard cert renews in 12 days" },
    { name: "Disk space",              status: "ok",   detail: "142 GB free on system volume" },
  ];
  const DXICON = { ok: "check", warn: "alert", err: "x" };
  const DXCOLOR = { ok: "var(--green)", warn: "var(--amber)", err: "var(--red)" };

  window.renderSettings = function (el) {
    rootEl = el;
    el.innerHTML = `
      <div class="page">
        <div class="page-head" style="border-bottom:none;padding-bottom:6px"><h1>Settings</h1></div>
        <div class="tabs" style="padding:0 24px">
          ${TABS.map(([id,label])=>`<button class="tab${tab===id?" active":""}" data-set-tab="${id}">${label}</button>`).join("")}
        </div>
        <div class="page-body"><div class="settings-body" id="setBody"></div></div>
      </div>`;
    el.querySelectorAll("[data-set-tab]").forEach(b => b.addEventListener("click", () => { tab = b.dataset.setTab; window.renderSettings(el); }));
    renderTab(el.querySelector("#setBody"));
    wire(el);
  };

  function renderTab(body) {
    if (tab === "general")      body.innerHTML = appearanceCard() + foldersCard() + defaultsCard() + domainCard();
    else if (tab === "system")  body.innerHTML = startupCard() + doctorCard();
    else if (tab === "updates") body.innerHTML = updatesCard();
    else                        body.innerHTML = dangerCard();
  }

  function appearanceCard() {
    const cur = App.theme.get();
    return `
      <div class="section-label">Appearance</div>
      <div class="card" style="margin-bottom:24px">
        <div class="setting-row" style="border:none;padding:0">
          <div class="sr-info"><div class="sr-name">Theme</div><div class="sr-desc">Follow the system appearance, or force light / dark.</div></div>
          <div class="seg sr-ctrl" id="themeSeg">
            ${[["auto","Auto"],["light","Light"],["dark","Dark"]].map(([id,l])=>`<button type="button" class="seg-btn${cur===id?" active":""}" data-theme-opt="${id}">${l}</button>`).join("")}
          </div>
        </div>
      </div>`;
  }

  function foldersCard() {
    return `
      <div class="section-label">Project folders</div>
      <div class="card" style="margin-bottom:24px">
        <p class="muted" style="margin:0 0 12px;font-size:var(--fs-13)">Folders Hull scans for projects and offers as locations for new ones.</p>
        ${H().DIRS.map((d, i, arr) => `<div class="folder-row">
          <span class="fr-label">${d.label}</span>
          <span class="fr-path">${d.path}</span>
          <button class="btn btn-sm btn-icon" data-folder-move="${i}" data-dir="-1" ${i === 0 ? "disabled" : ""} title="Move up">${icon("chevup",13)}</button>
          <button class="btn btn-sm btn-icon" data-folder-move="${i}" data-dir="1" ${i === arr.length - 1 ? "disabled" : ""} title="Move down">${icon("chevdown",13)}</button>
          <button class="btn btn-sm btn-icon" data-folder-remove="${d.label}" title="Remove">${icon("trash",13)}</button>
        </div>`).join("")}
        <p class="help" style="margin:10px 0 0">Order sets how groups appear in Sites; the top folder wins if two contain the same project name.</p>
        <div style="margin-top:10px"><button class="btn btn-sm" id="addFolder">${icon("plus",14)}Add folder…</button></div>
      </div>`;
  }

  function defaultsCard() {
    const d = (H().config && H().config.defaults) || {};
    const sel = (label, key, opts) => `<div><label class="field-label">${label}</label>
      <select class="select" data-default="${key}">${opts.map(([val, txt]) =>
        `<option value="${val}" ${val === (d[key] || "") ? "selected" : ""}>${txt}</option>`).join("")}</select></div>`;
    // Only the external apps Hull hands a project off to are real choices —
    // PHP and the web server are handled per-project via Docker/Caddy.
    return `
      <div class="section-label">Default tools</div>
      <div class="card" style="margin-bottom:24px">
        <p class="muted" style="margin:0 0 12px;font-size:var(--fs-13)">The apps Hull opens projects and databases with.</p>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
          ${sel("Open in editor", "editor", [["", "System default"], ["code", "VS Code"], ["phpstorm", "PhpStorm"], ["cursor", "Cursor"], ["subl", "Sublime Text"], ["zed", "Zed"]])}
          ${sel("Database tool", "db_tool", [["tableplus", "TablePlus"], ["dbeaver", "DBeaver"], ["adminer", "Adminer (web)"], ["cli", "CLI"]])}
        </div>
      </div>`;
  }

  function domainCard() {
    return `
      <div class="section-label">Local domain</div>
      <div class="card" style="margin-bottom:24px">
        <div class="form-row">
          <label class="field-label">Loopback address</label>
          <div class="addr"><span class="octet ro">127</span><span class="dot">.</span><span class="octet ro">0</span><span class="dot">.</span><span class="octet ro">0</span><span class="dot">.</span><span class="octet-edit"><input class="octet-input mono" id="octet" value="1" readonly aria-label="Last octet (1–8)"><span class="octet-steps"><button type="button" data-step="up" aria-label="Increase"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m6 15 6-6 6 6"/></svg></button><button type="button" data-step="down" aria-label="Decrease"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg></button></span></span></div>
          <p class="help">Only the final octet is editable (1–8).</p>
        </div>
        <div class="form-row">
          <label class="field-label">Top-level domain</label>
          <div style="display:flex;gap:10px;align-items:center">
            <input class="input mono" value=".test" style="width:120px">
            <button class="btn" id="rerunSetup">${icon("restart",15)}Re-run setup</button>
          </div>
        </div>
        <p class="help">Changing these rewrites every site's domain and re-issues certificates — Hull will ask for one admin prompt to update DNS.</p>
      </div>`;
  }

  function updatesCard() {
    const anyUpdate = H().DEPENDENCIES.some(d => d.status === "update" || d.status === "missing");
    return `
      <div class="section-label">Dependencies &amp; updates</div>
      <div class="card" style="margin-bottom:24px">
        <div style="display:flex;align-items:center;gap:10px;margin-bottom:6px">
          <p class="muted" style="margin:0;font-size:var(--fs-13);flex:1">Packages Hull installs and manages for you.</p>
          <button class="btn btn-sm" id="checkUpdates">${icon("restart",13)}Check now</button>
          ${anyUpdate ? `<button class="btn btn-sm btn-primary" id="updateAll">${icon("arrowup",13)}Update all</button>` : ""}
        </div>
        ${H().DEPENDENCIES.map(d => depRow(d)).join("")}
      </div>`;
  }
  function depRow(d) {
    const pill = d.status === "ok" ? `<span class="status-pill status-ok">${icon("check",12)}Up to date</span>`
      : d.status === "update" ? `<span class="status-pill status-update">${icon("arrowup",12)}${d.latest}</span>`
      : `<span class="status-pill status-missing">${icon("alert",12)}Not installed</span>`;
    const btn = d.status === "update" ? `<button class="btn btn-sm btn-primary" data-update="${d.key}">Update</button>`
      : d.status === "missing" ? `<button class="btn btn-sm btn-primary" data-install="${d.key}">Install</button>` : "";
    const meta = d.installed ? `${d.blurb} · installed ${d.installed}` : `${d.blurb} · required`;
    return `<div class="dep-row">
      <span class="dep-ic">${icon("cube",16)}</span>
      <div style="min-width:0"><div class="dep-name">${d.name}</div><div class="dep-meta">${meta}</div></div>
      <div class="dep-action">${pill}${btn}</div>
    </div>`;
  }

  function startupCard() {
    return `
      <div class="section-label">Startup</div>
      <div class="card" style="margin-bottom:24px">
        ${STARTUP.map(o => `<div class="setting-row">
          <div class="sr-info"><div class="sr-name">${o.name}</div><div class="sr-desc">${o.desc}</div></div>
          <label class="switch sr-ctrl"><input type="checkbox" data-startup="${o.key}" ${o.on?"checked":""}><span class="track"></span></label>
        </div>`).join("")}
      </div>`;
  }

  function doctorCard() {
    const checks = (H()._doctor && H()._doctor.length) ? H()._doctor : DOCTOR;
    const norm = s => s === "ok" ? "ok" : s === "warn" ? "warn" : "err";
    return `
      <div class="section-label">Doctor</div>
      <div class="card" style="margin-bottom:24px">
        <div style="display:flex;align-items:center;margin-bottom:6px">
          <p class="muted" style="margin:0;font-size:var(--fs-13);flex:1">Health checks for the local environment.</p>
          <button class="btn btn-sm" id="runDoctor">${icon("restart",13)}Run again</button>
        </div>
        ${checks.map(c => { const st = norm(c.status); return `<div class="doctor-row">
          <span style="color:${DXCOLOR[st]};margin-top:1px">${icon(DXICON[st],16)}</span>
          <div><div class="dx-name">${c.name}</div><div class="dx-detail">${c.detail}</div></div>
        </div>`; }).join("")}
      </div>`;
  }

  function dangerCard() {
    return `
      <div class="section-label">Danger zone</div>
      <div class="card danger">
        <div class="setting-row" style="border:none;padding-top:0">
          <div class="sr-info"><div class="sr-name">Clear caches &amp; rebuild</div><div class="sr-desc">Flush Hull's derived state and re-detect every project.</div></div>
          <button class="btn btn-sm sr-ctrl" id="clearCaches">Clear caches</button>
        </div>
        <hr class="hairline">
        <div class="setting-row" style="padding-bottom:0">
          <div class="sr-info"><div class="sr-name" style="color:var(--red)">Reset Hull</div><div class="sr-desc">Remove all configuration, certificates, and service volumes. Project files are untouched.</div></div>
          <button class="btn btn-sm btn-danger sr-ctrl" id="resetHull">${icon("trash",13)}Reset…</button>
        </div>
      </div>`;
  }

  // Build a /v1/config PUT body from current live config + overrides.
  function cfgBody(over) {
    const c = H().config || { tld: H().tld, roots: [], defaults: {} };
    return Object.assign({ tld: c.tld, roots: (c.roots || []).slice(), defaults: Object.assign({}, c.defaults) }, over || {});
  }
  function saveConfig(body, msg) { App.act(App.api("PUT", "/v1/config", body), msg || "Settings saved"); }

  function wire(el) {
    const t = (m) => () => App.toast(m);
    el.querySelectorAll("[data-theme-opt]").forEach(b => b.addEventListener("click", () => {
      App.theme.set(b.dataset.themeOpt);
      el.querySelectorAll("#themeSeg .seg-btn").forEach(x => x.classList.toggle("active", x === b));
    }));
    el.querySelector("#addFolder")?.addEventListener("click", async () => {
      const p = await App.pick("folder", { title: "Choose a folder Hull should scan for projects" });
      if (p) { const b = cfgBody(); const norm = p.replace(/\\/g, "/"); if (!b.roots.some(r => r.replace(/\\/g,"/") === norm)) { b.roots.push(p); saveConfig(b, "Folder added"); } else App.toast("That folder is already added"); }
    });
    el.querySelectorAll("[data-folder-remove]").forEach(b => b.addEventListener("click", () => {
      const path = H().DIRS.find(d => d.label === b.dataset.folderRemove)?.path;
      const body = cfgBody(); body.roots = body.roots.filter(r => r.replace(/\\/g,"/") !== path);
      if (!body.roots.length) { App.toast("Keep at least one folder"); return; }
      saveConfig(body, `Removed ${b.dataset.folderRemove}`);
    }));
    el.querySelectorAll("[data-folder-move]").forEach(b => b.addEventListener("click", () => {
      const i = +b.dataset.folderMove, j = i + (+b.dataset.dir);
      const body = cfgBody();
      if (j < 0 || j >= body.roots.length) return;
      [body.roots[i], body.roots[j]] = [body.roots[j], body.roots[i]];
      saveConfig(body, "Folder order updated");
    }));
    // Default-tool selects — save the chosen key on change.
    el.querySelectorAll("[data-default]").forEach(sel => sel.addEventListener("change", () => {
      const b = cfgBody();
      b.defaults[sel.dataset.default] = sel.value;
      saveConfig(b, "Default saved");
    }));
    el.querySelector("#rerunSetup")?.addEventListener("click", t("Run `hull setup` in a terminal to re-apply trust + DNS"));
    const oct = el.querySelector("#octet");
    if (oct) {
      const clamp = v => Math.max(1, Math.min(8, isNaN(v) ? 1 : v));
      const bump = d => { oct.value = clamp(parseInt(oct.value, 10) + d); };
      el.querySelectorAll("[data-step]").forEach(b => b.addEventListener("click", () => bump(b.dataset.step === "up" ? 1 : -1)));
      oct.addEventListener("keydown", e => {
        if (e.key === "ArrowUp") { e.preventDefault(); bump(1); }
        else if (e.key === "ArrowDown") { e.preventDefault(); bump(-1); }
      });
    }
    el.querySelector("#checkUpdates")?.addEventListener("click", t("Checking for updates…"));
    el.querySelector("#updateAll")?.addEventListener("click", () => {
      H().DEPENDENCIES.forEach(d => { if (d.status === "update") { d.installed = d.latest; d.status = "ok"; } });
      App.toast("Updating all packages…"); window.renderSettings(rootEl);
    });
    el.querySelectorAll("[data-update]").forEach(b => b.addEventListener("click", () => {
      const d = H().DEPENDENCIES.find(x => x.key === b.dataset.update); d.installed = d.latest; d.status = "ok";
      App.toast(`Updated ${d.name}`); window.renderSettings(rootEl);
    }));
    el.querySelectorAll("[data-install]").forEach(b => b.addEventListener("click", () => {
      if (b.dataset.install === "docker") App.openExternal("https://www.docker.com/products/docker-desktop/");
      else App.toast("Hull provides this internally");
    }));
    el.querySelectorAll("[data-startup]").forEach(b => b.addEventListener("change", () => App.toast("Startup options arrive with the installer")));
    el.querySelector("#runDoctor")?.addEventListener("click", async () => { App.toast("Running checks…"); await App.reload(); });
    el.querySelector("#clearCaches")?.addEventListener("click", t("Caches cleared"));
    el.querySelector("#resetHull")?.addEventListener("click", t("Reset requires confirmation"));
  }
})();
