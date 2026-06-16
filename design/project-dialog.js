/* Hull — New-project & Import dialogs.
   Directory presets + Name, a services repeater (scales to a large catalog),
   and an App container flow with Docker Hub search + editable Dockerfile. */
(function () {
  const H = () => window.HULL;

  /* ---------- shared field builders ---------- */
  function dirSelectorHTML() {
    return `
      <div class="form-row">
        <label class="field-label">Location</label>
        <div class="seg" data-dirs>
          ${H().DIRS.map((d, i) => `<button type="button" class="seg-btn${i === 0 ? " active" : ""}" data-path="${d.path}">${d.label}</button>`).join("")}
          <button type="button" class="seg-btn" data-custom>Custom…</button>
        </div>
        <div class="browse-row" data-custom-wrap style="display:none;margin-top:8px">
          <input class="input mono" data-custom-path placeholder="C:/path/to/base">
          <button type="button" class="btn" data-custom-browse title="Choose folder">${icon("folder",15)}Browse</button>
        </div>
      </div>
      <div class="form-row">
        <label class="field-label">Name</label>
        <input class="input mono" data-name placeholder="my-app">
        <div class="path-preview" data-preview></div>
      </div>`;
  }

  function serviceOptionsHTML() {
    const groups = H().CATALOG.map(g => g.cat === "Database"
      ? { cat: g.cat, items: [{ engine: "sqlite" }, ...g.items] }
      : g);
    return groups.map(g => `<optgroup label="${g.cat}">${
      g.items.map(it => `<option value="${it.engine}">${H().ENGINES[it.engine].label}</option>`).join("")
    }</optgroup>`).join("");
  }
  function versionsFor(engine) {
    const it = H().CATALOG.flatMap(g => g.items).find(x => x.engine === engine);
    return (it && it.versions) || ["—"];
  }
  function repeaterHTML() {
    return `
      <div class="repeater" data-rep>
        <div class="rep-empty" data-rep-empty>No services yet — add one below.</div>
        <div class="rep-rows" data-rep-rows></div>
        <button type="button" class="btn btn-sm" data-add-svc>${icon("plus",14)}Add service</button>
      </div>`;
  }
  function makeServiceRow() {
    const row = document.createElement("div");
    row.className = "rep-row";
    row.innerHTML = `
      <select class="select rep-engine">${serviceOptionsHTML()}</select>
      <select class="select rep-version"></select>
      <button type="button" class="btn btn-icon rep-remove" title="Remove">${icon("x",15)}</button>`;
    const eng = row.querySelector(".rep-engine"), ver = row.querySelector(".rep-version");
    const fillVer = () => { ver.innerHTML = versionsFor(eng.value).map(v => `<option>${v}</option>`).join(""); };
    eng.addEventListener("change", fillVer); fillVer();
    row.querySelector(".rep-remove").addEventListener("click", () => {
      const rep = row.closest("[data-rep]"); row.remove(); syncRepEmpty(rep);
    });
    return row;
  }
  function syncRepEmpty(rep) {
    const has = rep.querySelector("[data-rep-rows]").children.length > 0;
    rep.querySelector("[data-rep-empty]").style.display = has ? "none" : "";
  }
  function readRepeater(scope) {
    return [...scope.querySelectorAll(".rep-row")].map(r => ({
      engine: r.querySelector(".rep-engine").value,
      version: r.querySelector(".rep-version").value,
    }));
  }

  function dockerSectionHTML() {
    return `
      <div class="form-row" data-docker style="display:none">
        <label class="field-label">App containers <span class="faint">— search Docker Hub</span></label>
        <div class="search">${icon("search",14)}<input class="input" data-docker-q placeholder="Search images… e.g. node, python, nginx"></div>
        <div class="docker-status" data-docker-status></div>
        <div class="docker-results" data-docker-results></div>
        <button class="adv-toggle" type="button">${icon("chevright",16)}Advanced — edit Dockerfile</button>
        <div class="adv-body">
          <p class="help" style="margin:0 0 8px">Tweak the generated Dockerfile before Hull builds the image. It regenerates from your selection until you edit it.</p>
          <textarea class="input dockerfile-ed" data-dockerfile spellcheck="false"></textarea>
        </div>
      </div>`;
  }

  /* ---------- wiring ---------- */
  function wireDir(scope) {
    let base = H().DIRS[0].path;
    const seg = scope.querySelector("[data-dirs]");
    const customWrap = scope.querySelector("[data-custom-wrap]");
    const custom = scope.querySelector("[data-custom-path]");
    const nameI = scope.querySelector("[data-name]");
    const prev = scope.querySelector("[data-preview]");
    const update = () => {
      const nm = nameI.value.trim();
      prev.innerHTML = `${base.replace(/\/$/, "")}/<b>${nm || "…"}</b>`;
    };
    seg.querySelectorAll(".seg-btn").forEach(b => b.addEventListener("click", () => {
      seg.querySelectorAll(".seg-btn").forEach(x => x.classList.remove("active"));
      b.classList.add("active");
      if (b.hasAttribute("data-custom")) {
        customWrap.style.display = ""; base = custom.value || "C:/"; custom.focus();
      } else {
        customWrap.style.display = "none"; base = b.dataset.path;
      }
      update();
    }));
    custom.addEventListener("input", () => { base = custom.value || "C:/"; update(); });
    scope.querySelector("[data-custom-browse]").addEventListener("click", () => App.toast("Opening folder picker…"));
    nameI.addEventListener("input", update);
    update();
  }

  function wireRepeater(scope) {
    const rep = scope.querySelector("[data-rep]");
    if (!rep) return;
    rep.querySelector("[data-add-svc]").addEventListener("click", () => {
      rep.querySelector("[data-rep-rows]").appendChild(makeServiceRow());
      syncRepEmpty(rep);
    });
  }

  function wireDocker(scope) {
    const sec = scope.querySelector("[data-docker]");
    if (!sec) return;
    const q = sec.querySelector("[data-docker-q]");
    const status = sec.querySelector("[data-docker-status]");
    const results = sec.querySelector("[data-docker-results]");
    const editor = sec.querySelector("[data-dockerfile]");
    const selected = new Set();
    let dirty = false;
    editor.addEventListener("input", () => { dirty = true; });

    function regenDockerfile() {
      if (dirty) return;
      editor.value = dockerfileFor([...selected]);
    }
    function render(list) {
      results.innerHTML = list.map(im => {
        const label = im.ns ? `<span class="ns">${im.ns.split("/")[0]}/</span>${im.name}` : im.name;
        return `<div class="docker-result${selected.has(im.name) ? " sel" : ""}" data-img="${im.name}">
          <span class="dr-ic">${icon("cube",16)}</span>
          <div style="min-width:0"><div class="dr-name">${label}</div><div class="dr-desc">${im.desc}</div></div>
          <div class="dr-meta">${im.official ? `<span class="badge-official">OFFICIAL</span>` : ""}<span>${icon("download",12)} ${im.pulls}</span><span class="dr-check">${icon("check",15)}</span></div>
        </div>`;
      }).join("") || `<div class="rep-empty">No images match “${q.value.trim()}”.</div>`;
      results.querySelectorAll("[data-img]").forEach(r => r.addEventListener("click", () => {
        const n = r.dataset.img;
        if (selected.has(n)) selected.delete(n); else selected.add(n);
        r.classList.toggle("sel");
        regenDockerfile();
      }));
    }
    function search() {
      const term = q.value.trim().toLowerCase();
      status.innerHTML = `${icon("search",13)} Searching Docker Hub…`;
      clearTimeout(search._t);
      search._t = setTimeout(() => {
        const list = term
          ? H().DOCKER_IMAGES.filter(im => im.name.includes(term) || im.desc.toLowerCase().includes(term))
          : H().DOCKER_IMAGES.slice(0, 8);
        status.textContent = term ? `${list.length} result${list.length === 1 ? "" : "s"} for “${q.value.trim()}”` : "Popular images";
        render(list);
      }, 260);
    }
    q.addEventListener("input", search);
    search();
    regenDockerfile();
    sec._selected = selected;
  }

  function dockerfileFor(names) {
    if (!names.length) return "# Select one or more images above to scaffold a Dockerfile.";
    const tmpl = {
      node:   `FROM node:22-alpine\nWORKDIR /app\nCOPY package*.json ./\nRUN npm ci\nCOPY . .\nEXPOSE 3000\nCMD ["npm","start"]`,
      python: `FROM python:3.12-slim\nWORKDIR /app\nCOPY requirements.txt .\nRUN pip install --no-cache-dir -r requirements.txt\nCOPY . .\nEXPOSE 8000\nCMD ["python","app.py"]`,
      php:    `FROM php:8.3-fpm-alpine\nWORKDIR /var/www/html\nCOPY . .\nEXPOSE 9000\nCMD ["php-fpm"]`,
      golang: `FROM golang:1.22-alpine AS build\nWORKDIR /src\nCOPY . .\nRUN go build -o /bin/app ./...\n\nFROM alpine:3.20\nCOPY --from=build /bin/app /bin/app\nEXPOSE 8080\nCMD ["/bin/app"]`,
      ruby:   `FROM ruby:3.3-slim\nWORKDIR /app\nCOPY Gemfile* ./\nRUN bundle install\nCOPY . .\nEXPOSE 3000\nCMD ["ruby","app.rb"]`,
      nginx:  `FROM nginx:alpine\nCOPY ./dist /usr/share/nginx/html\nEXPOSE 80`,
      bun:    `FROM oven/bun:1.1\nWORKDIR /app\nCOPY package.json bun.lockb ./\nRUN bun install\nCOPY . .\nEXPOSE 3000\nCMD ["bun","start"]`,
      deno:   `FROM denoland/deno:alpine\nWORKDIR /app\nCOPY . .\nRUN deno cache main.ts\nEXPOSE 8000\nCMD ["deno","run","-A","main.ts"]`,
    };
    let out = tmpl[names[0]] || `FROM ${names[0]}:latest\nWORKDIR /app\nCOPY . .\n`;
    if (names.length > 1) out += `\n\n# Sidecar containers in compose:\n` + names.slice(1).map(n => `#   - ${n}:latest`).join("\n");
    return out;
  }

  function syncType(scope) {
    const type = scope.querySelector("[data-type]");
    const docker = scope.querySelector("[data-docker]");
    const php = scope.querySelector("[data-php]");
    const isApp = type.value === "App";
    if (docker) docker.style.display = isApp ? "" : "none";
    if (php) php.style.visibility = isApp ? "hidden" : "visible";
  }
  function summary(scope) {
    const svcs = readRepeater(scope);
    const docker = scope.querySelector("[data-docker]");
    const containers = docker && docker.style.display !== "none" && docker._selected ? docker._selected.size : 0;
    const bits = [];
    if (containers) bits.push(`${containers} container${containers > 1 ? "s" : ""}`);
    if (svcs.length) bits.push(`${svcs.length} service${svcs.length > 1 ? "s" : ""}`);
    return bits.length ? " · " + bits.join(", ") : "";
  }

  /* ---------- New project ---------- */
  function openNew() {
    App.openDialog(`
      <div class="dialog dialog-lg">
        <div class="dialog-head"><h3>New project</h3></div>
        <div class="dialog-body">
          ${dirSelectorHTML()}
          <div class="form-grid form-row">
            <div><label class="field-label">Type</label>
              <select class="select" data-type>${["Laravel","WordPress","Plain PHP","App"].map(o=>`<option>${o}</option>`).join("")}</select></div>
            <div data-php><label class="field-label">PHP version</label>
              <select class="select">${["8.3","8.2","8.1","8.0"].map(v=>`<option>${v}</option>`).join("")}</select></div>
          </div>
          ${dockerSectionHTML()}
          <div class="form-row">
            <label class="field-label">Services to provision</label>
            <p class="help" style="margin:-2px 0 8px">Add the databases, caches and tools to create and link from the start.</p>
            ${repeaterHTML()}
          </div>
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-primary" data-submit>Create project</button>
        </div>
      </div>`);
    const scope = document.querySelector(".dialog");
    wireDir(scope); wireRepeater(scope); wireDocker(scope);
    scope.querySelector("[data-type]").addEventListener("change", () => syncType(scope));
    syncType(scope);
    scope.querySelector("[data-submit]").addEventListener("click", () => {
      const extra = summary(scope); App.closeDialog(); App.toast(`Project created${extra}`);
    });
  }

  /* ---------- Import an unmanaged folder ---------- */
  function openImport(name) {
    const detected = /blog|cms|press|wp/i.test(name) ? "WordPress"
      : /legacy|import|scratch|old/i.test(name) ? "Plain PHP" : "Laravel";
    App.openDialog(`
      <div class="dialog dialog-lg">
        <div class="dialog-head"><h3>Import “${name}”</h3></div>
        <div class="dialog-body">
          <div class="form-row"><span class="detect-chip">${icon("check",13)} Detected: ${detected}</span></div>
          <div class="form-grid form-row">
            <div><label class="field-label">Type</label>
              <select class="select" data-type>${["Laravel","WordPress","Plain PHP","App"].map(o=>`<option ${o===detected?"selected":""}>${o}</option>`).join("")}</select></div>
            <div data-php><label class="field-label">PHP version</label>
              <select class="select">${["8.3","8.2","8.1","8.0"].map(v=>`<option>${v}</option>`).join("")}</select></div>
          </div>
          <div class="form-row"><label class="field-label">Local domain</label>
            <input class="input mono" value="${name}.test"></div>
          ${dockerSectionHTML()}
          <div class="form-row">
            <label class="field-label">Import existing database <span class="faint">(optional)</span></label>
            <div class="browse-row">
              <input class="input mono" placeholder="path to .sql dump or .sqlite file">
              <button class="btn" data-sql-browse>${icon("download",15)}Browse</button>
            </div>
            <p class="help">If the project ships a SQL dump, Hull loads it into the linked database on import.</p>
          </div>
          <div class="form-row">
            <label class="field-label">Services to provision</label>
            ${repeaterHTML()}
          </div>
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-primary" data-submit>${icon("download",15)}Import project</button>
        </div>
      </div>`);
    const scope = document.querySelector(".dialog");
    wireRepeater(scope); wireDocker(scope);
    scope.querySelector("[data-type]").addEventListener("change", () => syncType(scope));
    syncType(scope);
    scope.querySelector("[data-sql-browse]").addEventListener("click", () => App.toast("Choose a SQL dump…"));
    scope.querySelector("[data-submit]").addEventListener("click", () => {
      const extra = summary(scope); App.closeDialog(); App.toast(`Imported ${name}${extra}`);
    });
  }

  window.ProjectDialog = { openNew, openImport };
})();
