/* Hull — Dashboard screen. */
(function () {
  const H = () => window.HULL;
  function dotClass(s) { return s === "running" ? "dot-on pulse" : s === "error" ? "dot-err" : "dot-off"; }

  window.renderDashboard = function (el) {
    const sites = H().ROOTS.flatMap(r => r.managed);
    const sitesOn = sites.filter(s => s.status === "running").length;
    const svc = App.serviceRunning();
    const errs = sites.filter(s => s.status === "error").length;

    const health = H().HEALTH;
    const hdot = { ok: "dot-on", warn: "dot-warn", err: "dot-err" };

    el.innerHTML = `
      <div class="page">
        <div class="page-head">
          <h1>Dashboard</h1>
          <span class="ph-sub">${sitesOn} of ${sites.length} sites running</span>
          <div class="spacer"></div>
          <button class="btn" data-act="stopall">${icon("stop",15)}Stop all</button>
          <button class="btn btn-primary" data-act="startall">${icon("play",15)}Start all</button>
        </div>
        <div class="page-body">
          <div class="tiles">
            <div class="metric"><p class="metric-label">Sites running</p><div class="metric-value">${sitesOn}<span class="of"> / ${sites.length}</span></div></div>
            <div class="metric"><p class="metric-label">Services running</p><div class="metric-value">${svc.on}<span class="of"> / ${svc.total}</span></div></div>
            <div class="metric"><p class="metric-label">Issues</p><div class="metric-value" style="color:${errs?"var(--red)":"inherit"}">${errs}</div></div>
            <div class="metric"><p class="metric-label">PHP versions</p><div class="metric-value">3</div></div>
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

          <div class="section-label mt">Recent activity</div>
          <div class="card">
            ${H().JOBS.map(j => `
              <div class="activity-row">
                <span class="a-time">${j.t}</span>
                <span class="dot ${j.kind==="ok"?"dot-on":j.kind==="warn"?"dot-warn":"dot-err"}"></span>
                <span class="a-msg">${j.msg}</span>
                ${j.detail ? `<span class="a-detail">${j.detail}</span>` : ""}
              </div>`).join("")}
          </div>
        </div>
      </div>`;

    el.querySelector('[data-act="startall"]').addEventListener("click", () => {
      sites.forEach(s => { if (s.status !== "error") s.status = "running"; });
      App.toast("Starting all sites…"); window.renderDashboard(el);
    });
    el.querySelector('[data-act="stopall"]').addEventListener("click", () => {
      sites.forEach(s => s.status = "stopped");
      App.toast("Stopped all sites"); window.renderDashboard(el);
    });
  };
})();
