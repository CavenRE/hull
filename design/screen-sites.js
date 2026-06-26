/* Hull , Sites & Apps master–detail screen.
   The New-project / Import dialogs live in project-dialog.js (window.ProjectDialog). */
(function () {
  const H = () => window.HULL;
  let selected = null;     // site name
  let tab = "overview";
  let query = "";
  let logTimer = null;
  let refs = {};

  function allSites() { return H().ROOTS.flatMap(r => r.managed); }
  function findSite(name) { return allSites().find(s => s.name === name); }
  function inst(name) { return H().SERVICES.find(s => s.name === name); }
  function dotClass(status) { return status === "running" ? "dot-on pulse" : status === "error" ? "dot-err" : "dot-off"; }

  function clearLog() { if (logTimer) { clearInterval(logTimer); logTimer = null; } }

  window.renderSites = function (el) {
    clearLog();
    if (!selected) {
      const firstRunning = allSites().find(s => s.status === "running");
      selected = (firstRunning || allSites()[0]).name;
    }
    el.innerHTML = `
      <div class="sites">
        <div class="sites-list">
          <div class="sites-list-head">
            <div class="search">
              ${icon("search", 14)}
              <input class="input" id="siteSearch" placeholder="Search sites…" value="${query}">
            </div>
            <button class="btn btn-icon" id="newProject" title="New project">${icon("plus", 16)}</button>
          </div>
          <div class="list-scroll" id="listScroll"></div>
        </div>
        <div class="detail" id="detail"></div>
      </div>`;
    refs.listScroll = el.querySelector("#listScroll");
    refs.detail = el.querySelector("#detail");

    el.querySelector("#siteSearch").addEventListener("input", e => { query = e.target.value; renderList(); });
    el.querySelector("#newProject").addEventListener("click", () => window.ProjectDialog.openNew());

    App.onLeave(clearLog);
    renderList();
    renderDetail();
  };

  function renderList() {
    const q = query.trim().toLowerCase();
    let html = "";
    H().ROOTS.forEach(root => {
      const managed = root.managed.filter(s => !q || s.name.toLowerCase().includes(q));
      const unmanaged = q ? root.unmanaged.filter(n => n.toLowerCase().includes(q)) : root.unmanaged;
      if (!managed.length && !unmanaged.length) return;
      const shortPath = root.path.replace(/^.*[\/\\]([^\/\\]+[\/\\][^\/\\]+)$/, "$1");
      html += `<div class="group">
        <div class="group-label">${icon("folder", 13)}<span title="${root.path}">${shortPath}</span><span class="gcount">${root.managed.length}</span></div>`;
      managed.forEach(s => {
        html += `<div class="site-row${s.name === selected ? " active" : ""}" data-site="${s.name}">
          <span class="dot ${dotClass(s.status)}"></span>
          <span class="sr-name">${s.name}</span>
          <span class="sr-lock" title="HTTPS always on">${icon("lock", 12)}</span>
        </div>`;
      });
      unmanaged.forEach(n => {
        html += `<div class="site-row unmanaged" data-import="${n}">
          <span class="sr-name">${n}</span>
          <span class="import-link">Import</span>
        </div>`;
      });
      html += `</div>`;
    });
    if (!html) html = `<div style="padding:24px 10px;color:var(--text-faint);font-size:13px;text-align:center">No matches</div>`;
    refs.listScroll.innerHTML = html;

    refs.listScroll.querySelectorAll("[data-site]").forEach(r =>
      r.addEventListener("click", () => { selected = r.dataset.site; tab = "overview"; renderList(); renderDetail(); }));
    refs.listScroll.querySelectorAll("[data-import]").forEach(r =>
      r.addEventListener("click", () => window.ProjectDialog.openImport(r.dataset.import)));
  }

  function renderDetail() {
    clearLog();
    const s = findSite(selected);
    if (!s) { renderEmpty(); return; }
    const phpChip = s.php ? `<span class="chip chip-accent">php ${s.php}</span>` : "";
    let actions;
    if (s.status === "running") {
      actions = `<button class="btn" data-act="restart">${icon("restart",15)}Restart</button>
                 <button class="btn" data-act="stop">${icon("stop",15)}Stop</button>`;
    } else {
      actions = `<button class="btn btn-primary" data-act="start">${icon("play",15)}${s.status === "error" ? "Retry" : "Start"}</button>`;
    }
    refs.detail.innerHTML = `
      <div class="detail-head">
        <div class="dh-top">
          <span class="dot ${dotClass(s.status)}"></span>
          <span class="dh-title">${s.name}</span>
          <span class="chip">${s.kind}</span>
          ${phpChip}
          <div class="dh-actions" style="margin-left:auto">${actions}</div>
        </div>
        <div class="dh-url-row">
          <button class="url-btn" data-act="open-url">${icon("lock",13)}${s.url}</button>
          <button class="btn" data-act="open-url">${icon("external",15)}Open</button>
        </div>
        <div class="tabs" id="siteTabs">
          ${["overview","services","logs","settings"].map(t =>
            `<button class="tab${t===tab?" active":""}" data-tab="${t}">${t[0].toUpperCase()+t.slice(1)}</button>`).join("")}
        </div>
      </div>
      <div class="detail-body" id="detailBody"></div>`;

    refs.detail.querySelectorAll("[data-act]").forEach(b =>
      b.addEventListener("click", () => handleAction(b.dataset.act, s)));
    refs.detail.querySelectorAll("[data-tab]").forEach(b =>
      b.addEventListener("click", () => { tab = b.dataset.tab; renderDetail(); }));

    renderTab(s, refs.detail.querySelector("#detailBody"));
  }

  function handleAction(act, s) {
    if (act === "open-url") { App.toast(`Opening ${s.url}`); return; }
    if (act === "start")   { s.status = "running"; App.toast(`Started ${s.name}`); }
    if (act === "stop")    { s.status = "stopped"; App.toast(`Stopped ${s.name}`); }
    if (act === "restart") { App.toast(`Restarting ${s.name}…`); }
    renderList(); renderDetail();
  }

  function renderTab(s, body) {
    if (tab === "overview") return renderOverview(s, body);
    if (tab === "services") return renderServicesTab(s, body);
    if (tab === "logs")     return renderLogsTab(s, body);
    if (tab === "settings") return renderSettingsTab(s, body);
  }

  /* ---- Overview ---- */
  function renderOverview(s, body) {
    const cards = s.services.length ? s.services.map(link => {
      const i = inst(link.instance);
      const meta = H().ENGINES[i.engine];
      const mode = link.instance && i.linked.length > 1 ? "shared" : "shared";
      return `<div class="card svc-card">
        <span class="svc-ic">${icon(meta.icon, 18)}</span>
        <div style="min-width:0">
          <div class="sc-name">${meta.label}${i.version && i.version!=="latest" ? " " + i.version : ""}</div>
          <div class="sc-meta">${i.name} · ${mode}</div>
        </div>
        <span class="dot ${dotClass(i.status)}" style="margin-left:auto" title="${i.status}"></span>
      </div>`;
    }).join("") : `<div class="card" style="color:var(--text-dim)">No linked services. Add one from the <strong>Services</strong> tab.</div>`;

    const dir = `C:/Users/caven/${s.name === "acme" ? "Sites/acme" : "Sites/" + s.name}`;
    body.innerHTML = `
      <div class="section-label">Linked services</div>
      <div class="grid-cards">${cards}</div>
      <div class="section-label mt">Location</div>
      <div class="card path-row">
        ${icon("folder", 16)}
        <span class="pr-path">${dir}</span>
        <button class="btn btn-sm" data-open="folder">${icon("folder",13)}Open folder</button>
        <button class="btn btn-sm" data-open="editor">${icon("editor",13)}Open in editor</button>
      </div>`;
    body.querySelectorAll("[data-open]").forEach(b =>
      b.addEventListener("click", () => App.toast(b.dataset.open === "folder" ? "Revealing folder…" : "Opening in editor…")));
  }

  /* ---- Services (links) ---- */
  function renderServicesTab(s, body) {
    const rows = s.services.length ? s.services.map(link => {
      const i = inst(link.instance);
      const meta = H().ENGINES[i.engine];
      return `<div class="card link-row">
        <span class="svc-ic">${icon(meta.icon, 18)}</span>
        <div style="min-width:0">
          <div class="sc-name">${i.name}</div>
          <div class="sc-meta">${meta.label}${i.version && i.version!=="latest" ? " " + i.version : ""} · ${i.host}:${i.host_port}</div>
        </div>
        <button class="btn btn-sm" data-unlink="${i.name}" style="margin-left:auto">${icon("unlink",13)}Unlink</button>
      </div>`;
    }).join("") : `<div class="card" style="color:var(--text-dim)">No services linked yet.</div>`;
    body.innerHTML = `
      <div class="section-label">Linked services</div>
      ${rows}
      <div style="margin-top:16px"><button class="btn" id="linkPicker">${icon("link",15)}Link a service…</button></div>`;
    body.querySelector("#linkPicker").addEventListener("click", () => openLinkPicker(s));
    body.querySelectorAll("[data-unlink]").forEach(b =>
      b.addEventListener("click", () => App.toast(`Unlinked ${b.dataset.unlink}`)));
  }

  /* ---- Logs ---- */
  function renderLogsTab(s, body) {
    body.innerHTML = `
      <div style="display:flex;align-items:center;gap:10px;margin-bottom:12px">
        <div class="section-label" style="margin:0">Live log</div>
        <span class="chip chip-mono">${s.name}</span>
        <label style="margin-left:auto;display:flex;align-items:center;gap:7px;font-size:12px;color:var(--text-dim);cursor:pointer;user-select:none">
          <input type="checkbox" id="followToggle" ${s.status==="running"?"checked":""}> Follow
        </label>
        <button class="btn btn-sm" id="clearLog">Clear</button>
      </div>
      <div class="logpanel" id="logPanel" style="height:calc(100vh - 320px);min-height:220px"></div>`;
    const panel = body.querySelector("#logPanel");
    const seed = s.status === "error"
      ? [["10:31:08","",""],["10:31:09","er","php-fpm exited (code 1) , check composer autoload"],["10:31:09","wn","retry in 0s · run Start to retry"]]
      : [["10:41:50","ok",`booting ${s.name}`],["10:41:51","",`php ${s.php||","} · fpm pool ready`],["10:41:55","ok",`https://${s.name}.test bound`],["10:41:55","","router ✓  certificate ✓"]];
    seed.forEach(l => appendLine(panel, l));
    clearLog();
    const follow = body.querySelector("#followToggle");
    const tick = () => { if (s.status === "running") appendLine(panel, randomLine()); };
    function setFollow(on) { clearLog(); if (on) { logTimer = setInterval(tick, 1500); } }
    follow.addEventListener("change", () => setFollow(follow.checked));
    body.querySelector("#clearLog").addEventListener("click", () => { panel.innerHTML = ""; });
    if (follow.checked) setFollow(true);
  }
  function appendLine(panel, [t, kind, msg]) {
    const tag = kind ? `<span class="${kind}">${kind==="ok"?"✓":kind==="wn"?"!":kind==="er"?"✗":""}</span> ` : "";
    const div = document.createElement("div");
    div.className = "ln";
    div.innerHTML = `<span class="t">${t}</span> ${tag}${msg}`;
    panel.appendChild(div);
    panel.scrollTop = panel.scrollHeight;
  }
  function randomLine() {
    const now = new Date();
    const t = now.toTimeString().slice(0, 8);
    const msgs = [
      ["", `GET / 200 · ${(Math.random()*40+8).toFixed(0)}ms`],
      ["", `GET /assets/app.css 200`],
      ["", `query 127.0.0.1:54320 · ${(Math.random()*6+1).toFixed(1)}ms`],
      ["ok", `cache hit redis · users.${(Math.random()*900+100|0)}`],
      ["", `POST /login 302`],
      ["wn", `slow query 214ms · orders index scan`],
    ];
    const m = msgs[Math.random()*msgs.length|0];
    return [t, m[0], m[1]];
  }

  /* ---- Settings ---- */
  function renderSettingsTab(s, body) {
    body.innerHTML = `
      <div class="section-label">Configuration</div>
      <div class="card">
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;max-width:520px">
          <div><label class="field-label">PHP version</label>
            <select class="select" id="phpSel">${["8.3","8.2","8.1","8.0"].map(v=>`<option ${v===s.php?"selected":""}>${v}</option>`).join("")}</select></div>
          <div><label class="field-label">Local domain</label>
            <input class="input mono" value="${s.name}.test"></div>
        </div>
      </div>
      <div class="section-label mt">Danger zone</div>
      <div class="card danger">
        <div class="section-label">Destroy project</div>
        <p style="color:var(--text-dim);margin:0 0 12px">Removes Hull's configuration, certificate, and unlinks services. Your files are not deleted.</p>
        <p style="color:var(--text-dim);margin:0 0 8px;font-size:12px">Type <span class="confirm-name">${s.name}</span> to confirm.</p>
        <div style="display:flex;gap:8px;max-width:380px">
          <input class="input mono" id="destroyInput" placeholder="${s.name}">
          <button class="btn btn-danger" id="destroyBtn" disabled style="opacity:.5;pointer-events:none">${icon("trash",15)}Destroy</button>
        </div>
      </div>`;
    body.querySelector("#phpSel").addEventListener("change", e => App.toast(`PHP set to ${e.target.value}`));
    const di = body.querySelector("#destroyInput"), db = body.querySelector("#destroyBtn");
    di.addEventListener("input", () => {
      const ok = di.value === s.name;
      db.disabled = !ok;
      db.style.opacity = ok ? "1" : ".5";
      db.style.pointerEvents = ok ? "auto" : "none";
    });
    db.addEventListener("click", () => App.toast(`Destroyed ${s.name}`));
  }

  function renderEmpty() {
    refs.detail.innerHTML = `<div class="page-body center"><div class="empty">
      <div class="ic">${icon("sites", 22)}</div>
      <h2>No project selected</h2>
      <p>Choose a site from the list, or press ＋ to point Hull at a new folder.</p>
    </div></div>`;
  }

  /* ---- Link an existing service instance to this site ---- */
  function openLinkPicker(s) {
    const linkedNames = s.services.map(l => l.instance);
    const opts = H().SERVICES.filter(i => !linkedNames.includes(i.name));
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Link a service to ${s.name}</h3></div>
        <div class="dialog-body">
          ${opts.length ? opts.map(i => {
            const meta = H().ENGINES[i.engine];
            return `<div class="engine-opt" data-link="${i.name}">
              <span class="eo-ic">${icon(meta.icon,16)}</span>
              <div><div class="eo-name">${i.name}</div><div class="eo-blurb">${meta.label}${i.version&&i.version!=="latest"?" "+i.version:""} · ${i.host}:${i.host_port}</div></div>
              <span class="dot ${dotClass(i.status)} eo-ver" style="margin-left:auto"></span>
            </div>`;
          }).join("") : `<div style="color:var(--text-dim)">All running services are already linked.</div>`}
        </div>
        <div class="dialog-foot"><button class="btn" data-dialog-close>Done</button></div>
      </div>`);
    document.querySelectorAll("[data-link]").forEach(o =>
      o.addEventListener("click", () => { App.closeDialog(); App.toast(`Linked ${o.dataset.link} to ${s.name}`); }));
  }
})();
