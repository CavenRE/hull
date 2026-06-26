/* Hull , Sites & Apps master–detail screen.
   The New-project / Import dialogs live in project-dialog.js (window.ProjectDialog). */
(function () {
  const H = () => window.HULL;
  let selected = null;     // site name
  let tab = "overview";
  let query = "";
  let logTimer = null;
  let refs = {};
  let currentRoot = null;  // the project-root path this view is scoped to
  let picked = new Set();  // multi-select (site names)

  function rootObj() { return (H().ROOTS || []).find(r => r.path === currentRoot) || { path: currentRoot, managed: [], unmanaged: [] }; }
  function allSites() { return H().ROOTS.flatMap(r => r.managed); }
  function findSite(name) { return allSites().find(s => s.name === name); }
  function rootOf(name) { return (H().ROOTS || []).find(r => r.managed.some(m => m.name === name)); }
  function inst(name) { return H().SERVICES.find(s => s.name === name); }
  function dotClass(status) { return status === "running" ? "dot-on pulse" : status === "error" ? "dot-err" : "dot-off"; }

  function clearLog() { if (logTimer) { clearInterval(logTimer); logTimer = null; } if (typeof clearLogStream === "function") clearLogStream(); }

  // Let other screens (Dashboard) open a specific site , routes to its root.
  window.selectSite = function (name) {
    const root = rootOf(name);
    selected = name; tab = "overview"; App.touchSite(name);
    App.go(root ? "root:" + root.path : "dashboard");
  };

  window.renderSites = function (el, rootPath) {
    clearLog();
    currentRoot = rootPath || (H().DIRS[0] && H().DIRS[0].path) || null;
    picked.clear();
    const sites = rootObj().managed;
    if (!selected || !sites.some(s => s.name === selected)) {
      const firstRunning = sites.find(s => s.status === "running");
      selected = (firstRunning || sites[0] || {}).name || null;
    }
    el.innerHTML = `
      <div class="sites">
        <div class="sites-list">
          <div class="sites-list-head">
            <div class="search">
              ${icon("search", 14)}
              <input class="input" id="siteSearch" placeholder="Search ${rootObj().path ? rootObj().path.split("/").pop() : "sites"}…" value="${query}">
            </div>
            <button class="btn btn-icon" id="newProject" title="New project">${icon("plus", 16)}</button>
          </div>
          <div class="list-scroll" id="listScroll"></div>
          <div class="list-foot">
            <button class="btn btn-sm" id="addGroupBtn">${icon("plus", 14)}Group</button>
            <button class="btn btn-sm" id="adoptCluster" title="Adopt an existing compose project as a cluster">${icon("cube", 14)}Adopt</button>
          </div>
        </div>
        <div class="detail" id="detail"></div>
      </div>`;
    refs.listScroll = el.querySelector("#listScroll");
    refs.detail = el.querySelector("#detail");

    el.querySelector("#siteSearch").addEventListener("input", e => { query = e.target.value; renderList(); });
    el.querySelector("#newProject").addEventListener("click", () => window.ProjectDialog.openNew());
    el.querySelector("#addGroupBtn").addEventListener("click", openAddGroup);
    el.querySelector("#adoptCluster").addEventListener("click", () => window.ProjectDialog.openCluster());

    App.onLeave(clearLog);
    renderList();
    renderDetail();
  };

  function siteRowHTML(s) {
    const sel = s.name === selected ? " active" : "";
    const multi = picked.has(s.name) ? " sel-multi" : "";
    const badge = s.isCluster ? `<span class="sr-badge">cluster</span>` : "";
    const lock = s.served && !s.isCluster ? `<span class="sr-lock" title="HTTPS always on">${icon("lock", 12)}</span>` : "";
    return `<div class="site-row${sel}${multi}" draggable="true" data-site="${s.name}">
      <span class="dot ${dotClass(s.status)}"></span>
      <span class="sr-name">${s.name}</span>${badge}${lock}
    </div>`;
  }

  function renderList() {
    const q = query.trim().toLowerCase();
    const root = rootObj();
    const managed = root.managed.filter(s => !q || s.name.toLowerCase().includes(q));
    const unmanaged = q ? root.unmanaged.filter(n => n.toLowerCase().includes(q)) : root.unmanaged;
    const groupNames = H().groupsFor(currentRoot);

    const buckets = {}; groupNames.forEach(g => (buckets[g] = []));
    const ungrouped = [];
    managed.forEach(s => {
      if (s.group && groupNames.includes(s.group)) buckets[s.group].push(s);
      else ungrouped.push(s);
    });

    const section = (label, gname, rows) => `
      <div class="group">
        <div class="group-label" data-drop-group="${gname}">${icon("folder", 13)}<span>${label}</span><span class="gcount">${rows.length}</span></div>
        <div class="group-body" data-drop-group="${gname}">${rows.map(siteRowHTML).join("")}</div>
      </div>`;

    let html = "";
    groupNames.forEach(g => { html += section(g, g, buckets[g]); });
    // Ungrouped always present as a drop target (drop here to remove from group).
    if (ungrouped.length || groupNames.length) html += section("Ungrouped", "", ungrouped);
    else html += ungrouped.map(siteRowHTML).join("");
    if (unmanaged.length) {
      html += `<div class="group"><div class="group-label"><span>Folders to import</span></div>`;
      unmanaged.forEach(n => {
        html += `<div class="site-row unmanaged" data-import="${n}"><span class="sr-name">${n}</span><span class="import-link">Import</span></div>`;
      });
      html += `</div>`;
    }
    if (!html) html = `<div style="padding:24px 10px;color:var(--text-faint);font-size:13px;text-align:center">No projects in this folder yet</div>`;
    refs.listScroll.innerHTML = html;

    wireRows();
    wireDnD();
  }

  function wireRows() {
    refs.listScroll.querySelectorAll("[data-site]").forEach(r => {
      r.addEventListener("click", e => {
        if (e.ctrlKey || e.metaKey) { togglePick(r.dataset.site); return; }
        picked.clear();
        selected = r.dataset.site; tab = "overview"; App.touchSite(selected);
        renderList(); renderDetail();
      });
      r.addEventListener("contextmenu", e => { e.preventDefault(); openRowMenu(e, r.dataset.site); });
    });
    refs.listScroll.querySelectorAll("[data-import]").forEach(r =>
      r.addEventListener("click", () => window.ProjectDialog.openImport(r.dataset.import)));
  }

  function togglePick(name) {
    if (picked.has(name)) picked.delete(name); else picked.add(name);
    renderList();
  }

  // ---- drag & drop assignment ----
  function wireDnD() {
    let dragName = null;
    refs.listScroll.querySelectorAll("[data-site]").forEach(r => {
      r.addEventListener("dragstart", () => { dragName = r.dataset.site; });
    });
    refs.listScroll.querySelectorAll("[data-drop-group]").forEach(z => {
      z.addEventListener("dragover", e => { e.preventDefault(); z.classList.add("drag-over"); });
      z.addEventListener("dragleave", () => z.classList.remove("drag-over"));
      z.addEventListener("drop", e => {
        e.preventDefault(); z.classList.remove("drag-over");
        if (dragName) assignGroup([dragName], z.dataset.dropGroup);
        dragName = null;
      });
    });
  }

  function assignGroup(names, group) {
    Promise.all(names.map(n => App.api("POST", `/v1/projects/${n}/group`, { group })))
      .then(() => { picked.clear(); App.reload(); App.toast(group ? `Moved to ${group}` : "Ungrouped"); })
      .catch(e => App.toast(e.message || "Group change failed"));
  }

  // ---- context menu ----
  function openRowMenu(e, name) {
    const names = picked.size && picked.has(name) ? [...picked] : [name];
    const groupNames = H().groupsFor(currentRoot);
    const moveItems = [...groupNames.map(g => ({ label: g, group: g })), { label: "Ungrouped", group: "" }, { label: "New group…", group: "__new" }];
    const menu = document.createElement("div");
    menu.className = "ctx-menu";
    menu.style.left = e.clientX + "px";
    menu.style.top = e.clientY + "px";
    menu.innerHTML = `
      <div class="ctx-head">${names.length > 1 ? names.length + " projects" : name}</div>
      <button data-cmd="start">${icon("play", 14)}Start</button>
      <button data-cmd="stop">${icon("stop", 14)}Stop</button>
      <button data-cmd="restart">${icon("restart", 14)}Restart</button>
      <div class="ctx-sep"></div>
      <div class="ctx-label">Move to</div>
      ${moveItems.map(m => `<button data-move="${m.group}">${m.label}</button>`).join("")}`;
    document.body.appendChild(menu);
    const close = () => { menu.remove(); document.removeEventListener("click", close); };
    setTimeout(() => document.addEventListener("click", close), 0);
    menu.querySelectorAll("[data-cmd]").forEach(b => b.addEventListener("click", () => {
      const verb = b.dataset.cmd;
      names.forEach(n => App.act(App.api("POST", `/v1/projects/${n}/${verb}`), `${verb}…`));
      close();
    }));
    menu.querySelectorAll("[data-move]").forEach(b => b.addEventListener("click", async () => {
      let g = b.dataset.move;
      if (g === "__new") { close(); openAddGroup(names); return; }
      assignGroup(names, g); close();
    }));
  }

  // ---- add-group modal ----
  function openAddGroup(assignAfter) {
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>New group</h3></div>
        <div class="dialog-body">
          <div class="form-row"><label class="field-label">Group name</label>
            <input class="input" id="groupName" placeholder="e.g. Frontend" autofocus></div>
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-primary" id="groupCreate">Create</button>
        </div>
      </div>`);
    const create = async () => {
      const name = (document.getElementById("groupName").value || "").trim();
      if (!name) { App.toast("Name the group"); return; }
      try {
        await H().addGroup(currentRoot, name);
        if (assignAfter && assignAfter.length) await Promise.all(assignAfter.map(n => App.api("POST", `/v1/projects/${n}/group`, { group: name })));
        App.closeDialog(); picked.clear(); await App.reload(); App.toast(`Group ${name} created`);
      } catch (e) { App.toast(e.message || "Could not create group"); }
    };
    document.getElementById("groupCreate").addEventListener("click", create);
    document.getElementById("groupName").addEventListener("keydown", e => { if (e.key === "Enter") create(); });
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
    actions += `<button class="btn" data-act="rebuild" title="Rebuild images and restart">${icon("cube",15)}Rebuild</button>
                <button class="btn btn-danger" data-act="reset" title="Wipe data volumes and start fresh">${icon("trash",15)}Reset</button>`;
    const urlRow = s.url
      ? `<button class="url-btn" data-act="open-url">${icon("lock",13)}${s.url}</button>
         <button class="btn" data-act="open-url">${icon("external",15)}Open</button>`
      : `<span class="path-preview" style="margin:0">headless , no routed domain (enable in Settings)</span>`;
    refs.detail.innerHTML = `
      <div class="detail-head">
        <div class="dh-top">
          <span class="dot ${dotClass(s.status)}"></span>
          <span class="dh-title">${s.name}</span>
          <span class="chip">${s.kind}</span>
          ${phpChip}
          <div class="dh-actions" style="margin-left:auto">${actions}</div>
        </div>
        <div class="dh-url-row">${urlRow}</div>
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
    if (act === "open-url") { App.openExternal(s.url); return; }
    if (act === "reset") { openReset(s); return; }
    if (act === "rebuild") { App.act(App.api("POST", `/v1/projects/${s.name}/rebuild`), `Rebuilding ${s.name}…`); return; }
    const verb = act === "start" ? "start" : act === "stop" ? "stop" : "restart";
    App.act(App.api("POST", `/v1/projects/${s.name}/${verb}`), `${verb[0].toUpperCase()+verb.slice(1)}ing ${s.name}…`);
  }

  // Reset = wipe named volumes + restart. Show the blast radius and require
  // the project name typed to confirm (same gate as Destroy).
  function openReset(s) {
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Reset ${s.name}?</h3></div>
        <div class="dialog-body">
          <p style="color:var(--text-dim);margin:0 0 10px">This deletes the project's <strong>named volumes</strong> (databases, caches) and starts it fresh. Host bind-mounted files are NOT touched.</p>
          <div id="resetVols" class="codeblock" style="margin-bottom:12px"><pre>Loading volumes…</pre></div>
          <p style="color:var(--text-dim);margin:0 0 8px;font-size:12px">Type <span class="confirm-name">${s.name}</span> to confirm.</p>
          <input class="input mono" id="resetInput" placeholder="${s.name}">
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-danger" id="resetBtn" disabled style="opacity:.5;pointer-events:none">${icon("trash",15)}Reset</button>
        </div>
      </div>`);
    App.api("GET", `/v1/projects/${s.name}/volumes`).then(vols => {
      const pre = document.querySelector("#resetVols pre");
      if (!pre) return;
      pre.textContent = (vols && vols.length) ? vols.map(v => "• " + v).join("\n") : "No named volumes , nothing to delete.";
    }).catch(() => {});
    const ri = document.getElementById("resetInput"), rb = document.getElementById("resetBtn");
    ri.addEventListener("input", () => {
      const ok = ri.value === s.name;
      rb.disabled = !ok; rb.style.opacity = ok ? "1" : ".5"; rb.style.pointerEvents = ok ? "auto" : "none";
    });
    rb.addEventListener("click", () => { App.closeDialog(); App.act(App.api("POST", `/v1/projects/${s.name}/reset`), `Resetting ${s.name}…`); });
  }

  function renderTab(s, body) {
    if (s.isCluster && tab === "overview") return renderClusterOverview(s, body);
    if (tab === "overview") return renderOverview(s, body);
    if (tab === "services") return renderServicesTab(s, body);
    if (tab === "logs")     return renderLogsTab(s, body);
    if (tab === "settings") return renderSettingsTab(s, body);
  }

  // Cluster overview: the wrapped stack's routed subdomains (its other
  // containers , workers, dbs , run unserved and aren't listed here).
  function renderClusterOverview(s, body) {
    const tld = H().tld;
    const rows = (s.routes || []).map(r => `
      <div class="wire-row">
        <span class="dot ${s.status === "running" ? "dot-on" : "dot-off"}"></span>
        <div style="min-width:0">
          <div class="wr-name">${r.served ? `https://${r.subdomain}.${tld}` : r.subdomain + " (not served)"}</div>
          <div class="wr-meta">→ ${r.service}:${r.port}</div>
        </div>
        ${r.served ? `<button class="btn btn-sm" data-open-route="${r.subdomain}.${tld}" style="margin-left:auto">${icon("external", 13)}Open</button>` : ""}
      </div>`).join("");
    body.innerHTML = `
      <div class="section-label">Cluster routes</div>
      <div class="card">${rows || `<p class="muted" style="margin:0">No routes parsed. Edit hull.yaml to map subdomains to services, or this stack serves itself.</p>`}</div>
      <p class="help" style="margin-top:10px">Lifecycle (Start / Stop / Rebuild / Reset) acts on the whole compose project. Workers, databases, and one-shot init containers run unserved.</p>`;
    body.querySelectorAll("[data-open-route]").forEach(b =>
      b.addEventListener("click", () => App.openExternal("https://" + b.dataset.openRoute)));
  }

  // Resolve a link's display facts from live data (shared instance if it
  // exists, otherwise the dedicated container described by the link).
  function linkFacts(s, link) {
    const i = inst(link.instance);
    const meta = H().ENGINES[link.engine] || { label: link.engine, icon: "database" };
    const ver = link.version && link.version !== "latest" ? " " + link.version : "";
    const status = link.mode === "shared" ? (i ? i.status : "stopped") : s.status;
    const detail = link.mode === "shared"
      ? `${link.instance}${i && i.host_port ? " · " + i.host + ":" + i.host_port : ""} · shared`
      : "dedicated container";
    return { meta, ver, status, detail, label: meta.label };
  }

  /* ---- Overview ---- */
  function renderOverview(s, body) {
    const servicesSection = s.services.length ? `
      <div class="section-label">Linked services</div>
      <div class="grid-cards">${s.services.map(link => {
        const f = linkFacts(s, link);
        return `<div class="card svc-card">
          <span class="svc-ic">${icon(f.meta.icon, 18)}</span>
          <div style="min-width:0">
            <div class="sc-name">${f.label}${f.ver}</div>
            <div class="sc-meta">${link.key} · ${f.detail}</div>
          </div>
          <span class="dot ${dotClass(f.status)}" style="margin-left:auto" title="${f.status}"></span>
        </div>`;
      }).join("")}</div>` : "";

    body.innerHTML = `
      ${servicesSection}
      <div class="section-label${s.services.length ? " mt" : ""}">Location</div>
      <div class="card path-row">
        ${icon("folder", 16)}
        <span class="pr-path" title="${s.dir||""}">${s.dir||""}</span>
        <button class="btn btn-sm" data-open="folder">${icon("folder",13)}Open folder</button>
        <button class="btn btn-sm" data-open="editor">${icon("editor",13)}Open in editor</button>
      </div>`;
    body.querySelectorAll("[data-open]").forEach(b =>
      b.addEventListener("click", () => App.act(App.api("POST", `/v1/projects/${s.name}/open`, { target: b.dataset.open }),
        b.dataset.open === "folder" ? "Revealing folder…" : "Opening in editor…")));
  }

  /* ---- Services (links) ---- */
  function renderServicesTab(s, body) {
    const rows = s.services.length ? `
      <div class="section-label">Linked services</div>
      ${s.services.map(link => {
        const f = linkFacts(s, link);
        return `<div class="card link-row">
          <span class="svc-ic">${icon(f.meta.icon, 18)}</span>
          <div style="min-width:0">
            <div class="sc-name">${link.key} → ${f.label}${f.ver}</div>
            <div class="sc-meta">${f.detail}</div>
          </div>
          <button class="btn btn-sm" data-unlink="${link.key}" style="margin-left:auto">${icon("unlink",13)}Unlink</button>
        </div>`;
      }).join("")}` : `<p class="muted" style="margin:0 0 4px">No services linked to ${s.name} yet.</p>`;
    body.innerHTML = `
      ${rows}
      <div style="margin-top:16px"><button class="btn" id="linkPicker">${icon("link",15)}Link a service…</button></div>`;
    body.querySelector("#linkPicker").addEventListener("click", () => openLinkPicker(s));
    body.querySelectorAll("[data-unlink]").forEach(b =>
      b.addEventListener("click", () => App.act(App.api("POST", `/v1/projects/${s.name}/unlink`, { key: b.dataset.unlink }), `Unlinked ${b.dataset.unlink}`)));
  }

  /* ---- Logs (live SSE from the daemon) ---- */
  let logES = null;
  function clearLogStream() { if (logES) { logES.close(); logES = null; } }
  function renderLogsTab(s, body) {
    body.innerHTML = `
      <div style="display:flex;align-items:center;gap:10px;margin-bottom:12px">
        <div class="section-label" style="margin:0">Live log</div>
        <span class="chip chip-mono">${s.name}</span>
        <button class="btn btn-sm" id="clearLog" style="margin-left:auto">Clear</button>
      </div>
      <div class="logpanel" id="logPanel" style="height:calc(100vh - 320px);min-height:220px"></div>`;
    const panel = body.querySelector("#logPanel");
    body.querySelector("#clearLog").addEventListener("click", () => { panel.innerHTML = ""; });
    clearLogStream();
    if (s.status !== "running") { panel.innerHTML = `<div class="ln faint">Start the project to stream its logs.</div>`; return; }
    try {
      logES = new EventSource(H().sseURL(`/v1/logs?project=${encodeURIComponent(s.name)}&tail=200`));
      logES.onmessage = (e) => {
        const div = document.createElement("div");
        div.className = "ln";
        div.textContent = e.data;
        panel.appendChild(div);
        panel.scrollTop = panel.scrollHeight;
      };
      logES.onerror = () => { /* stream ends when container stops */ };
    } catch (e) {}
    App.onLeave(clearLogStream);
  }

  /* ---- Settings ---- */
  function renderSettingsTab(s, body) {
    // Type-aware: PHP only for PHP sites; serve toggle only for single
    // projects (clusters route per-service, not project-wide).
    const isPHP = ["laravel", "wordpress", "php"].includes(s.kind);
    const domainVal = s.isCluster ? "routes , see Overview" : (s.served ? s.name + "." + H().tld : ", headless ,");
    const phpField = isPHP
      ? `<div><label class="field-label">PHP version</label>
           <select class="select" id="phpSel"></select></div>`
      : `<div><label class="field-label">Type</label><input class="input mono" value="${s.kind}" readonly></div>`;
    const serveRow = s.isCluster ? "" : `
        <div class="setting-row" style="border:none;padding:12px 0 0">
          <div class="sr-info"><div class="sr-name">Serve a domain</div><div class="sr-desc">Off = runs headless (no vhost, DNS, or certificate). Restart to apply.</div></div>
          <label class="switch sr-ctrl"><input type="checkbox" id="serveTog" ${s.served ? "checked" : ""}><span class="track"></span></label>
        </div>`;
    body.innerHTML = `
      <div class="section-label">Configuration</div>
      <div class="card">
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;max-width:520px">
          ${phpField}
          <div><label class="field-label">Local domain</label>
            <input class="input mono" value="${domainVal}" readonly></div>
        </div>
        ${serveRow}
      </div>
      <div class="section-label mt">Danger zone</div>
      <div class="card danger">
        <div class="section-label">${s.isCluster ? "Un-adopt cluster" : "Destroy project"}</div>
        <p style="color:var(--text-dim);margin:0 0 12px">${s.isCluster ? "Stops the stack and removes Hull's manifest , your compose files and repo are left untouched." : "Removes Hull's configuration, certificate, and unlinks services. Your files are not deleted."}</p>
        <p style="color:var(--text-dim);margin:0 0 8px;font-size:12px">Type <span class="confirm-name">${s.name}</span> to confirm.</p>
        <div style="display:flex;gap:8px;max-width:380px">
          <input class="input mono" id="destroyInput" placeholder="${s.name}">
          <button class="btn btn-danger" id="destroyBtn" disabled style="opacity:.5;pointer-events:none">${icon("trash",15)}${s.isCluster ? "Un-adopt" : "Destroy"}</button>
        </div>
      </div>`;
    const phpSel = body.querySelector("#phpSel");
    if (phpSel) {
      H().phpVersions().then(live => {
        const opts = (live && live.length) ? live : ["8.4", "8.3", "8.2", "8.1"];
        if (s.php && !opts.includes(s.php)) opts.unshift(s.php);
        phpSel.innerHTML = opts.map(v => `<option ${v === s.php ? "selected" : ""}>${v}</option>`).join("");
      });
      phpSel.addEventListener("change", e =>
        App.act(App.api("PATCH", `/v1/projects/${s.name}`, { php: e.target.value }), `PHP set to ${e.target.value} , restart to apply`));
    }
    const serveTog = body.querySelector("#serveTog");
    if (serveTog) serveTog.addEventListener("change", e =>
      App.act(App.api("PATCH", `/v1/projects/${s.name}`, { serve: e.target.checked }),
        e.target.checked ? "Domain enabled , restart to apply" : "Domain removed , restart to apply"));
    const di = body.querySelector("#destroyInput"), db = body.querySelector("#destroyBtn");
    di.addEventListener("input", () => {
      const ok = di.value === s.name;
      db.disabled = !ok;
      db.style.opacity = ok ? "1" : ".5";
      db.style.pointerEvents = ok ? "auto" : "none";
    });
    db.addEventListener("click", () => { selected = null; App.act(App.api("DELETE", `/v1/projects/${s.name}`), `Destroying ${s.name}…`); });
  }

  function renderEmpty() {
    refs.detail.innerHTML = `<div class="page-body center"><div class="empty">
      <div class="ic">${icon("sites", 22)}</div>
      <h2>No project selected</h2>
      <p>Choose a site from the list, or press ＋ to point Hull at a new folder.</p>
    </div></div>`;
  }

  /* ---- Link an existing shared instance to this site ---- */
  function openLinkPicker(s) {
    const linkedNames = s.services.map(l => l.instance);
    const opts = H().SERVICES.filter(i => !linkedNames.includes(i.name));
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Link a service to ${s.name}</h3></div>
        <div class="dialog-body">
          ${opts.length ? opts.map(i => {
            const meta = H().ENGINES[i.engine] || { label: i.engine, icon: "database" };
            return `<div class="engine-opt" data-link="${i.name}">
              <span class="eo-ic">${icon(meta.icon,16)}</span>
              <div><div class="eo-name">${i.name}</div><div class="eo-blurb">${meta.label}${i.version&&i.version!=="latest"?" "+i.version:""}${i.host_port?" · "+i.host+":"+i.host_port:""}</div></div>
              <span class="dot ${dotClass(i.status)} eo-ver" style="margin-left:auto"></span>
            </div>`;
          }).join("") : `<div style="color:var(--text-dim)">No shared instances yet , add one on the Services page.</div>`}
        </div>
        <div class="dialog-foot"><button class="btn" data-dialog-close>Done</button></div>
      </div>`);
    document.querySelectorAll("[data-link]").forEach(o =>
      o.addEventListener("click", () => { App.closeDialog(); App.act(App.api("POST", `/v1/services/${o.dataset.link}/link`, { project: s.name }), `Linking ${o.dataset.link} to ${s.name}…`); }));
  }
})();
