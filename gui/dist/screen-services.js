/* Hull , Services screen: instance cards + add-instance engine picker. */
(function () {
  const H = () => window.HULL;
  function dotClass(s) { return s === "running" ? "dot-on pulse" : s === "error" ? "dot-err" : "dot-off"; }
  const DB = { postgres: 1, mysql: 1, mariadb: 1 };

  let rootEl = null;

  window.renderServices = function (el) {
    rootEl = el;
    const svc = App.serviceRunning();
    el.innerHTML = `
      <div class="page">
        <div class="page-head">
          <h1>Services</h1>
          <span class="ph-sub">${svc.on} of ${svc.total} running</span>
          <div class="spacer"></div>
          <button class="btn btn-primary" id="addInstance">${icon("plus",15)}Add instance</button>
        </div>
        <div class="page-body"><div class="inst-grid" id="instGrid"></div></div>
      </div>`;
    el.querySelector("#addInstance").addEventListener("click", openAddInstance);
    renderGrid();
  };

  function renderGrid() {
    const grid = rootEl.querySelector("#instGrid");
    grid.innerHTML = H().SERVICES.map(card).join("");
    grid.querySelectorAll("[data-act]").forEach(b =>
      b.addEventListener("click", () => action(b.dataset.act, b.dataset.name)));
    grid.querySelectorAll(".copy-btn").forEach(b =>
      b.addEventListener("click", () => App.copyText(b.dataset.copy, b)));
    grid.querySelectorAll(".inst-url").forEach(b =>
      b.addEventListener("click", () => App.openExternal(b.dataset.url)));
  }

  function card(i) {
    const meta = H().ENGINES[i.engine];
    const verLabel = i.version && i.version !== "latest" ? " " + i.version : "";
    const connRows = [
      ["host", i.host, false],
      ["port", String(i.host_port), false],
    ];
    if (i.username) connRows.push(["user", i.username, false]);
    connRows.push(["password", i.password || "(no password)", !i.password]);
    const conn = connRows.map(([k, v, empty]) => `
      <div class="conn-row">
        <span class="conn-key">${k}</span>
        <span class="conn-val${empty ? " empty" : ""}">${v}</span>
        <button class="copy-btn" data-copy="${empty ? "" : v}" title="Copy ${k}">${icon("copy",14)}</button>
      </div>`).join("");

    const chips = i.linked.length
      ? `<span class="lc-label">Linked</span>` + i.linked.map(n => `<span class="chip">${n}</span>`).join("")
      : `<span class="lc-label">No linked projects</span>`;

    const running = i.status === "running";
    return `<div class="card inst">
      <div class="inst-head">
        <span class="ih-ic">${icon(meta.icon, 18)}</span>
        <div style="min-width:0">
          <div class="ih-name" style="display:flex;align-items:center;gap:8px"><span class="dot ${dotClass(i.status)}"></span>${i.name}</div>
        </div>
        <span class="chip chip-accent" style="margin-left:8px">${meta.label}${verLabel}</span>
        <div class="ih-actions">
          <button class="btn btn-sm" data-act="${running?"stop":"start"}" data-name="${i.name}">${running?icon("stop",13)+"Stop":icon("play",13)+"Start"}</button>
        </div>
      </div>
      <div class="inst-body">
        ${i.url ? `<div class="inst-url" data-url="${i.url}">${icon("external",12)}${i.url}</div>` : ""}
        <div class="conn">${conn}</div>
        <div class="linked-chips">${chips}</div>
      </div>
      <div class="inst-foot">
        ${DB[i.engine] ? `<button class="btn btn-sm" data-act="open-with" data-name="${i.name}">${icon("external",13)}Open with…</button>` : ""}
        <button class="btn btn-sm" data-act="link" data-name="${i.name}">${icon("link",13)}Link a project</button>
        <div style="flex:1"></div>
        <button class="btn btn-sm btn-danger" data-act="destroy" data-name="${i.name}">${icon("trash",13)}Destroy</button>
      </div>
    </div>`;
  }

  function action(act, name) {
    const i = H().SERVICES.find(s => s.name === name);
    if (act === "start")     App.act(App.api("POST", `/v1/services/${name}/start`), `Starting ${name}…`);
    else if (act === "stop") App.act(App.api("POST", `/v1/services/${name}/stop`), `Stopping ${name}…`);
    else if (act === "open-with") openOpenWith(i);
    else if (act === "link") openLinkProject(i);
    else if (act === "destroy") openDestroy(i);
  }

  /* ---- Add instance: engine picker grouped by category ---- */
  function openAddInstance() {
    let sel = null;
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Add instance</h3></div>
        <div class="dialog-body">
          ${H().CATALOG.map(g => `
            <div class="cat-group">
              <div class="cat-label">${g.cat}</div>
              ${g.items.map(it => {
                const meta = H().ENGINES[it.engine];
                return `<div class="engine-opt" data-engine="${it.engine}">
                  <span class="eo-ic">${icon(meta.icon,16)}</span>
                  <div style="min-width:0"><div class="eo-name">${meta.label}</div><div class="eo-blurb">${it.blurb}</div></div>
                  <input class="input eo-ver" style="width:130px" onclick="event.stopPropagation()" placeholder="version" value="${it.versions[0]||''}">
                </div>`;
              }).join("")}
            </div>`).join("")}
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-primary" id="createInst" disabled style="opacity:.5;pointer-events:none">Add instance</button>
        </div>
      </div>`);
    const create = document.getElementById("createInst");
    let selVer = "";
    // Each version field is a searchable combobox (quick picks + live tag search).
    document.querySelectorAll(".engine-opt").forEach(o => {
      const verInp = o.querySelector(".eo-ver");
      if (!verInp) return;
      App.wireVersionField(verInp, () => H().versions(o.dataset.engine), (q) => H().versionSearch(o.dataset.engine, q));
    });
    document.querySelectorAll(".engine-opt").forEach(o => o.addEventListener("click", () => {
      document.querySelectorAll(".engine-opt").forEach(x => x.classList.remove("sel"));
      o.classList.add("sel"); sel = o.dataset.engine;
      const verSel = o.querySelector(".eo-ver"); selVer = verSel ? verSel.value : "";
      create.disabled = false; create.style.opacity = "1"; create.style.pointerEvents = "auto";
    }));
    create.addEventListener("click", () => {
      const verSel = document.querySelector(".engine-opt.sel .eo-ver");
      const version = verSel ? verSel.value : selVer;
      App.closeDialog();
      App.act(App.api("POST", "/v1/services", { engine: sel, version }), `Creating ${H().ENGINES[sel].label} instance…`);
    });
  }

  function openOpenWith(i) {
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Open ${i.name} with…</h3></div>
        <div class="dialog-body">
          ${["TablePlus","DBeaver","Adminer (web)","psql / mysql CLI"].map(t =>
            `<div class="engine-opt" data-tool="${t}"><span class="eo-ic">${icon("tool",16)}</span><div class="eo-name">${t}</div></div>`).join("")}
        </div>
        <div class="dialog-foot"><button class="btn" data-dialog-close>Cancel</button></div>
      </div>`);
    document.querySelectorAll("[data-tool]").forEach(o =>
      o.addEventListener("click", () => { App.closeDialog(); openInTool(i, o.dataset.tool); }));
  }

  function openInTool(i, tool) {
    const port = i.host_port, user = i.username || "";
    if (tool === "Adminer (web)") {
      const param = i.engine === "postgres" ? `pgsql=${i.host}&username=postgres` : `server=${i.host}&username=root`;
      App.openExternal(`https://db.${H().tld}/?${param}`); return;
    }
    if (/CLI/.test(tool)) {
      const cmd = i.engine === "postgres" ? `psql -h 127.0.0.1 -p ${port} -U postgres`
        : `mysql -h 127.0.0.1 -P ${port} -u root`;
      App.copyText(cmd); App.toast("CLI command copied"); return;
    }
    // TablePlus / DBeaver register connection-URI schemes.
    const uri = i.engine === "postgres" ? `postgres://postgres@127.0.0.1:${port}/postgres`
      : `mysql://root@127.0.0.1:${port}`;
    App.openExternal(uri);
  }

  function openLinkProject(i) {
    const sites = H().ROOTS.flatMap(r => r.managed).filter(s => !i.linked.includes(s.name));
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Link a project to ${i.name}</h3></div>
        <div class="dialog-body">
          ${sites.map(s => `<div class="engine-opt" data-site="${s.name}"><span class="eo-ic">${icon("sites",16)}</span><div><div class="eo-name">${s.name}</div><div class="eo-blurb">${s.kind}${s.php?" · php "+s.php:""}</div></div></div>`).join("")}
        </div>
        <div class="dialog-foot"><button class="btn" data-dialog-close>Done</button></div>
      </div>`);
    document.querySelectorAll("[data-site]").forEach(o =>
      o.addEventListener("click", () => { App.closeDialog(); App.act(App.api("POST", `/v1/services/${i.name}/link`, { project: o.dataset.site }), `Linking ${o.dataset.site} to ${i.name}…`); }));
  }

  function openDestroy(i) {
    App.openDialog(`
      <div class="dialog">
        <div class="dialog-head"><h3>Destroy ${i.name}?</h3></div>
        <div class="dialog-body">
          <p style="color:var(--text-dim);margin:0 0 12px">This stops and removes the <strong>${i.name}</strong> container and its volume. Data in this instance will be lost.</p>
          <p style="color:var(--text-dim);margin:0 0 8px;font-size:12px">Type <span class="confirm-name">${i.name}</span> to confirm.</p>
          <input class="input mono" id="destroyInstInput" placeholder="${i.name}">
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-danger" id="destroyInstBtn" disabled style="opacity:.5;pointer-events:none">${icon("trash",15)}Destroy</button>
        </div>
      </div>`);
    const di = document.getElementById("destroyInstInput"), db = document.getElementById("destroyInstBtn");
    di.addEventListener("input", () => {
      const ok = di.value === i.name;
      db.disabled = !ok; db.style.opacity = ok ? "1" : ".5"; db.style.pointerEvents = ok ? "auto" : "none";
    });
    db.addEventListener("click", () => { App.closeDialog(); App.act(App.api("DELETE", `/v1/services/${i.name}`), `Destroying ${i.name}…`); });
  }
})();
