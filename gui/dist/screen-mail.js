/* Hull — Mail screen. Integration surface for the Mailpit catcher (not an inbox). */
(function () {
  const H = () => window.HULL;
  let rootEl = null;

  function mailService() { return H().SERVICES.find(s => s.engine === "mailpit"); }
  function laravelSites() { return H().ROOTS.flatMap(r => r.managed).filter(s => s.kind === "laravel"); }

  const ENV_BLOCK = `MAIL_MAILER=smtp
MAIL_HOST=hull-mailpit
MAIL_PORT=1025
MAIL_USERNAME=null
MAIL_PASSWORD=null
MAIL_ENCRYPTION=null
MAIL_FROM_ADDRESS="hello@example.test"
MAIL_FROM_NAME="\${APP_NAME}"`;

  const SMTP_BLOCK = `Host       127.0.0.1
Port       1025
Security   none (STARTTLS optional)
Username   (none)
Password   (none)`;

  window.renderMail = function (el) {
    rootEl = el;
    const m = mailService();

    el.innerHTML = `
      <div class="page">
        <div class="page-head">
          <h1>Mail</h1>
          <span class="ph-sub">Outgoing mail catcher for local apps</span>
          <div class="spacer"></div>
          ${m && m.status === "running" ? `<button class="btn btn-primary" data-act="open">${icon("external",15)}Open Mailpit</button>` : ""}
        </div>
        <div class="page-body" id="mailBody"></div>
      </div>`;

    const body = el.querySelector("#mailBody");
    if (!m) { renderNoService(body); }
    else { renderSurface(body, m); }
  };

  function renderNoService(body) {
    body.innerHTML = `
      <div class="card" style="display:flex;align-items:center;gap:16px;max-width:680px">
        <span class="svc-ic" style="width:42px;height:42px">${icon("mail",20)}</span>
        <div style="flex:1">
          <div style="font-size:var(--fs-14);font-weight:600">No mail service yet</div>
          <div class="muted" style="font-size:var(--fs-13);margin-top:3px">Add a Mailpit instance to catch every email your apps send — nothing leaves your machine.</div>
        </div>
        <button class="btn btn-primary" data-act="add">${icon("plus",15)}Add Mailpit</button>
      </div>`;
    body.querySelector('[data-act="add"]').addEventListener("click", () =>
      App.act(App.api("POST", "/v1/services", { engine: "mailpit" }), "Adding Mailpit…"));
  }

  function renderSurface(body, m) {
    const running = m.status === "running";
    const wired = m.linked;
    const sites = laravelSites();

    body.innerHTML = `
      <div class="card" style="display:flex;align-items:center;gap:14px;margin-bottom:18px">
        <span class="dot ${running ? "dot-on pulse" : "dot-off"}"></span>
        <div style="flex:1">
          <div style="font-size:var(--fs-14);font-weight:600">${running ? "Catching mail" : "Stopped"}</div>
          <div class="muted" style="font-size:var(--fs-12);margin-top:2px">Mailpit · SMTP on <span class="mono">127.0.0.1:1025</span> · inbox at <span class="mono">localhost:8025</span></div>
        </div>
        ${running
          ? `<button class="btn" data-act="stop">${icon("stop",15)}Stop</button>`
          : `<button class="btn btn-primary" data-act="start">${icon("play",15)}Start</button>`}
      </div>

      <div class="section-label">Connect your app</div>
      <div class="mail-grid" style="margin-bottom:18px">
        <div class="codeblock">
          <div class="codeblock-head">Laravel <span class="mono faint">.env</span>
            <button class="btn btn-sm cb-copy" data-copy="env">${icon("copy",13)}Copy</button></div>
          <pre>${ENV_BLOCK.replace(/^(\w+)=/gm, '<span class="ck">$1</span>=')}</pre>
        </div>
        <div class="codeblock">
          <div class="codeblock-head">Generic SMTP
            <button class="btn btn-sm cb-copy" data-copy="smtp">${icon("copy",13)}Copy</button></div>
          <pre>${SMTP_BLOCK}</pre>
        </div>
      </div>

      <div class="section-label">Wired apps</div>
      <div class="card">
        ${sites.map(s => {
          const on = wired.includes(s.name);
          return `<div class="wire-row">
            <span class="dot ${s.status === "running" ? "dot-on" : "dot-off"}"></span>
            <div style="flex:1;min-width:0"><div class="wr-name">${s.name}</div><div class="wr-meta">laravel · php ${s.php}</div></div>
            ${on
              ? `<span class="chip" style="color:var(--green)">${icon("check",13)}Wired</span><button class="btn btn-sm" data-unwire="${s.name}">Unwire</button>`
              : `<button class="btn btn-sm btn-primary" data-wire="${s.name}">${icon("link",13)}Wire this app</button>`}
          </div>`;
        }).join("")}
      </div>`;

    // header open button → external Mailpit inbox
    const openBtn = rootEl.querySelector('[data-act="open"]');
    if (openBtn) openBtn.addEventListener("click", () => App.openExternal(`https://mail.${H().tld}`));

    body.querySelectorAll("[data-act]").forEach(b => b.addEventListener("click", () => {
      const a = b.dataset.act;
      if (a === "start") App.act(App.api("POST", `/v1/services/${m.name}/start`), "Starting Mailpit…");
      if (a === "stop")  App.act(App.api("POST", `/v1/services/${m.name}/stop`), "Stopping Mailpit…");
    }));
    body.querySelectorAll(".cb-copy").forEach(b => b.addEventListener("click", () =>
      App.copyText(b.dataset.copy === "env" ? ENV_BLOCK : SMTP_BLOCK, b)));
    body.querySelectorAll("[data-wire]").forEach(b => b.addEventListener("click", () =>
      App.act(App.api("POST", `/v1/services/${m.name}/link`, { project: b.dataset.wire }), `Wiring ${b.dataset.wire} to Mailpit…`)));
    body.querySelectorAll("[data-unwire]").forEach(b => b.addEventListener("click", () =>
      App.act(App.api("POST", `/v1/projects/${b.dataset.unwire}/unlink`, { key: "mail" }), `Unwiring ${b.dataset.unwire}…`)));
  }
})();
