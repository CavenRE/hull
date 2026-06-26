/* Hull , Dashboard screen: metrics, recent sites, active services
   control, system health, recent activity. */
(function () {
  const H = () => window.HULL;
  function dotClass(s) { return s === "running" ? "dot-on pulse" : s === "error" ? "dot-err" : "dot-off"; }
  const ENG = (e) => H().ENGINES[e] || { label: e, icon: "database" };

  window.renderDashboard = function (el) {
    const sites = H().ROOTS.flatMap(r => r.managed);
    const sitesOn = sites.filter(s => s.status === "running").length;
    const services = H().SERVICES;
    const svcOn = services.filter(s => s.status === "running").length;
    const errs = sites.filter(s => s.status === "error").length;
    const health = H().HEALTH;
    const hdot = { ok: "dot-on", warn: "dot-warn", err: "dot-err" };

    // Recent sites , last used (persisted), falling back to running ones.
    let recent = App.recentSites().map(n => sites.find(s => s.name === n)).filter(Boolean).slice(0, 5);
    if (!recent.length) recent = sites.slice().sort((a, b) => (b.status === "running") - (a.status === "running")).slice(0, 5);

    el.innerHTML = `
      <div class="page">
        <div class="page-head">
          <h1>Dashboard</h1>
          <span class="ph-sub">${sitesOn} of ${sites.length} sites running</span>
          <div class="spacer"></div>
          <button class="btn" data-act="stopall">${icon("stop",15)}Stop all sites</button>
          <button class="btn btn-primary" data-act="startall">${icon("play",15)}Start all sites</button>
        </div>
        <div class="page-body">
          <div class="tiles">
            <div class="metric"><p class="metric-label">Sites running</p><div class="metric-value">${sitesOn}<span class="of"> / ${sites.length}</span></div></div>
            <div class="metric"><p class="metric-label">Services running</p><div class="metric-value">${svcOn}<span class="of"> / ${services.length}</span></div></div>
            <div class="metric"><p class="metric-label">Issues</p><div class="metric-value" style="color:${errs?"var(--red)":"inherit"}">${errs}</div></div>
            <div class="metric"><p class="metric-label">Shared services</p><div class="metric-value">${services.length}</div></div>
          </div>

          <div class="dash-cols">
            <div>
              <div class="section-label mt">Recent sites</div>
              <div class="card" style="padding:6px 8px">
                ${recent.length ? recent.map(s => `
                  <div class="dash-row" data-site="${s.name}">
                    <span class="dot ${dotClass(s.status)}"></span>
                    <span class="dr-name">${s.name}</span>
                    <span class="chip">${s.kind}</span>
                    <div class="dr-actions">
                      ${s.status === "running"
                        ? `<button class="btn btn-sm btn-icon" data-open="${s.name}" title="Open ${s.url}">${icon("external",13)}</button>`
                        : `<button class="btn btn-sm" data-start-site="${s.name}">${icon("play",13)}Start</button>`}
                    </div>
                  </div>`).join("") : `<div class="muted" style="padding:10px">No sites yet.</div>`}
              </div>
            </div>

            <div>
              <div class="section-label mt" style="display:flex;align-items:center;gap:10px">
                <span>Active services</span>
                <div style="margin-left:auto;display:flex;gap:6px">
                  <button class="btn btn-sm" data-svc-all="stop">${icon("stop",13)}Stop all</button>
                  <button class="btn btn-sm" data-svc-all="start">${icon("play",13)}Start all</button>
                </div>
              </div>
              <div class="card" style="padding:6px 8px">
                ${services.length ? services.map(s => {
                  const m = ENG(s.engine);
                  return `<div class="dash-row">
                    <span class="svc-ic" style="width:24px;height:24px">${icon(m.icon,14)}</span>
                    <span class="dr-name">${s.name}</span>
                    ${s.host_port ? `<span class="chip chip-mono">:${s.host_port}</span>` : ""}
                    <div class="dr-actions">
                      ${s.status === "running"
                        ? `<button class="btn btn-sm" data-svc="stop" data-name="${s.name}">${icon("stop",13)}Stop</button>`
                        : `<button class="btn btn-sm btn-primary" data-svc="start" data-name="${s.name}">${icon("play",13)}Start</button>`}
                    </div>
                  </div>`;
                }).join("") : `<div class="muted" style="padding:10px">No shared services , add one on the Services page.</div>`}
              </div>
            </div>
          </div>

          <div class="section-label mt">System health</div>
          <div class="health-grid">
            ${health.map(h => `
              <div class="card health">
                <span class="h-ic">${icon(h.icon, 18)}</span>
                <div>
                  <div class="h-name"><span class="dot ${hdot[h.status]}"></span>${h.name}</div>
                  <div class="h-detail">${h.detail}</div>
                </div>
              </div>`).join("")}
          </div>

          ${H().JOBS.length ? `
          <div class="section-label mt">Recent activity</div>
          <div class="card">
            ${H().JOBS.map(j => `
              <div class="activity-row">
                <span class="a-time">${j.t}</span>
                <span class="dot ${j.kind==="ok"?"dot-on":j.kind==="warn"?"dot-warn":"dot-err"}"></span>
                <span class="a-msg">${j.msg}</span>
                ${j.detail ? `<span class="a-detail">${j.detail}</span>` : ""}
              </div>`).join("")}
          </div>` : ""}
        </div>
      </div>`;

    // ---- site controls ----
    el.querySelector('[data-act="startall"]').addEventListener("click", async () => {
      App.toast("Starting all sites…");
      for (const s of sites) if (s.status === "stopped") { try { await App.api("POST", `/v1/projects/${s.name}/start`); } catch (e) {} }
      App.reload();
    });
    el.querySelector('[data-act="stopall"]').addEventListener("click", async () => {
      App.toast("Stopping all sites…");
      for (const s of sites) if (s.status === "running") { try { await App.api("POST", `/v1/projects/${s.name}/stop`); } catch (e) {} }
      App.reload();
    });
    el.querySelectorAll("[data-site]").forEach(r => r.addEventListener("click", (e) => {
      if (e.target.closest("[data-open],[data-start-site]")) return;
      window.selectSite(r.dataset.site);
    }));
    el.querySelectorAll("[data-open]").forEach(b => b.addEventListener("click", () => {
      const s = sites.find(x => x.name === b.dataset.open); if (s) { App.touchSite(s.name); App.openExternal(s.url); }
    }));
    el.querySelectorAll("[data-start-site]").forEach(b => b.addEventListener("click", () =>
      App.act(App.api("POST", `/v1/projects/${b.dataset.startSite}/start`), `Starting ${b.dataset.startSite}…`)));

    // ---- service controls ----
    el.querySelectorAll("[data-svc]").forEach(b => b.addEventListener("click", () =>
      App.act(App.api("POST", `/v1/services/${b.dataset.name}/${b.dataset.svc}`), `${b.dataset.svc === "start" ? "Starting" : "Stopping"} ${b.dataset.name}…`)));
    el.querySelectorAll("[data-svc-all]").forEach(b => b.addEventListener("click", async () => {
      const verb = b.dataset.svcAll;
      App.toast(`${verb === "start" ? "Starting" : "Stopping"} all services…`);
      for (const s of services) {
        const want = verb === "start" ? s.status !== "running" : s.status === "running";
        if (want) { try { await App.api("POST", `/v1/services/${s.name}/${verb}`); } catch (e) {} }
      }
      App.reload();
    }));
  };
})();
