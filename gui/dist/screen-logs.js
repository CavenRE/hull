/* Hull , Logs screen. Live SSE from the daemon (/v1/logs) with a source
   picker, heuristic level filter, text filter, follow + clear. */
(function () {
  const H = () => window.HULL;
  let rootEl = null, panel = null, streams = [];
  let source = "all", level = "all", query = "", follow = true;
  let lines = [];

  function runningSites() { return H().ROOTS.flatMap(r => r.managed).filter(s => s.status === "running").map(s => s.name); }
  function runningSvcs() { return H().SERVICES.filter(s => s.status === "running").map(s => s.name); }

  function classify(msg) {
    if (/error|exception|fatal|panic|\b5\d\d\b|refused|failed/i.test(msg)) return "error";
    if (/warn|deprecat|slow|retry|timeout/i.test(msg)) return "warn";
    return "info";
  }
  function closeStreams() { streams.forEach(es => { try { es.close(); } catch (e) {} }); streams = []; }

  window.renderLogs = function (el) {
    rootEl = el;
    const sites = runningSites(), svcs = runningSvcs();
    el.innerHTML = `
      <div class="page">
        <div class="page-head"><h1>Logs</h1><span class="ph-sub" id="logCount"></span></div>
        <div class="logs-toolbar">
          <select class="select" id="logSource" style="width:190px">
            <option value="all">All running</option>
            <optgroup label="Sites">${sites.map(n => `<option ${source === n ? "selected" : ""}>${n}</option>`).join("")}</optgroup>
            <optgroup label="Services">${svcs.map(n => `<option ${source === n ? "selected" : ""}>${n}</option>`).join("")}</optgroup>
          </select>
          <div class="seg" id="logLevel">
            ${["all","info","warn","error"].map(l => `<button type="button" class="seg-btn${level === l ? " active" : ""}" data-level="${l}">${l[0].toUpperCase()+l.slice(1)}</button>`).join("")}
          </div>
          <div class="search" style="flex:1;min-width:160px"><input class="input" id="logSearch" placeholder="Filter log text…" value="${query}"></div>
          <label class="switch"><input type="checkbox" id="logFollow" ${follow ? "checked" : ""}><span class="track"></span>Follow</label>
          <button class="btn" id="logClear">Clear</button>
        </div>
        <div class="logs-body"><div class="logpanel" id="logStream"></div></div>
      </div>`;
    panel = el.querySelector("#logStream");
    el.querySelector("#logSearch").insertAdjacentHTML("beforebegin", icon("search", 14));

    el.querySelector("#logSource").addEventListener("change", e => { source = e.target.value; open(); });
    el.querySelector("#logLevel").addEventListener("click", e => {
      const b = e.target.closest("[data-level]"); if (!b) return;
      level = b.dataset.level;
      el.querySelectorAll("#logLevel .seg-btn").forEach(x => x.classList.toggle("active", x === b));
      repaint();
    });
    el.querySelector("#logSearch").addEventListener("input", e => { query = e.target.value; repaint(); });
    el.querySelector("#logFollow").addEventListener("change", e => { follow = e.target.checked; });
    el.querySelector("#logClear").addEventListener("click", () => { lines = []; repaint(); });

    if (!sites.length && !svcs.length) { panel.innerHTML = `<div class="ln faint">Nothing running yet , start a site or service.</div>`; return; }
    open();
    App.onLeave(closeStreams);
  };

  function open() {
    closeStreams();
    lines = [];
    repaint();
    const targets = [];
    if (source === "all") {
      runningSites().forEach(n => targets.push(["project", n]));
      runningSvcs().forEach(n => targets.push(["service", n]));
    } else {
      const kind = runningSites().includes(source) ? "project" : "service";
      targets.push([kind, source]);
    }
    targets.forEach(([kind, name]) => {
      try {
        const es = new EventSource(H().sseURL(`/v1/logs?${kind}=${encodeURIComponent(name)}&tail=150`));
        es.onmessage = (e) => {
          const l = { t: new Date().toTimeString().slice(0, 8), src: name, lvl: classify(e.data), msg: e.data };
          lines.push(l);
          if (lines.length > 2000) lines.shift();
          if (pass(l)) appendLine(l);
        };
        streams.push(es);
      } catch (e) {}
    });
  }

  function pass(l) {
    if (level !== "all" && l.lvl !== level) return false;
    if (query && !(l.src + " " + l.msg).toLowerCase().includes(query.toLowerCase())) return false;
    return true;
  }
  function lineHTML(l) {
    const cls = l.lvl === "warn" ? "wn" : l.lvl === "error" ? "er" : "";
    const mark = l.lvl === "warn" ? `<span class="wn">!</span> ` : l.lvl === "error" ? `<span class="er">✗</span> ` : "";
    return `<div class="ln"><span class="t">${l.t}</span> <span class="src">${l.src}</span> ${mark}<span class="${cls}">${escapeHTML(l.msg)}</span></div>`;
  }
  function escapeHTML(s) { const d = document.createElement("div"); d.textContent = s; return d.innerHTML; }
  function appendLine(l) {
    if (!panel) return;
    panel.insertAdjacentHTML("beforeend", lineHTML(l));
    if (follow) panel.scrollTop = panel.scrollHeight;
    updateCount();
  }
  function repaint() {
    if (!panel) return;
    const shown = lines.filter(pass);
    panel.innerHTML = shown.map(lineHTML).join("") || `<div class="ln faint">No matching log lines.</div>`;
    if (follow) panel.scrollTop = panel.scrollHeight;
    updateCount();
  }
  function updateCount() {
    const c = rootEl && rootEl.querySelector("#logCount");
    if (c) { const n = panel.querySelectorAll(".ln").length; c.textContent = `${n} line${n === 1 ? "" : "s"}${source !== "all" ? " · " + source : ""}`; }
  }
})();
