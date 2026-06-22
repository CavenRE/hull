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
      <input class="input rep-version" placeholder="version (search)">
      <button type="button" class="btn btn-icon rep-remove" title="Remove">${icon("x",15)}</button>`;
    const eng = row.querySelector(".rep-engine"), ver = row.querySelector(".rep-version");
    const refresh = App.wireVersionField(ver,
      () => eng.value === "sqlite" ? Promise.resolve([]) : H().versions(eng.value),
      (q) => H().versionSearch(eng.value, q));
    eng.addEventListener("change", () => { ver.value = ""; refresh(); });
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
    let base = (H().DIRS[0] && H().DIRS[0].path) || "C:/";
    const seg = scope.querySelector("[data-dirs]");
    const customWrap = scope.querySelector("[data-custom-wrap]");
    const custom = scope.querySelector("[data-custom-path]");
    const nameI = scope.querySelector("[data-name]");
    const prev = scope.querySelector("[data-preview]");
    const domainEl = scope.querySelector("[data-domain]");
    const serveEl = scope.querySelector("[data-serve]");
    const update = () => {
      const slug = H().slug(nameI.value) || "…";
      prev.innerHTML = `${base.replace(/\/$/, "")}/<b>${slug}</b>`;
      if (domainEl) {
        domainEl.innerHTML = (serveEl && !serveEl.checked)
          ? `<span class="faint">headless — no domain</span>`
          : `→ <b>https://${slug}.${H().tld}</b>`;
      }
    };
    if (serveEl) serveEl.addEventListener("change", update);
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
    scope.querySelector("[data-custom-browse]").addEventListener("click", async () => {
      const p = await App.pick("folder", { title: "Choose a base folder for the new project" });
      if (p) { custom.value = p; base = p; update(); }
    });
    nameI.addEventListener("input", update);
    update();
  }

  async function wirePHP(scope) {
    const sel = scope.querySelector("[data-php] select");
    if (!sel) return;
    const live = await H().phpVersions();
    const opts = (live && live.length) ? live : ["8.4", "8.3", "8.2", "8.1"];
    sel.innerHTML = opts.map(v => `<option>${v}</option>`).join("");
  }

  function wireRepeater(scope) {
    const rep = scope.querySelector("[data-rep]");
    if (!rep) return;
    rep.querySelector("[data-add-svc]").addEventListener("click", () => {
      rep.querySelector("[data-rep-rows]").appendChild(makeServiceRow());
      syncRepEmpty(rep);
    });
  }

  function wireCluster(scope) {
    const sec = scope.querySelector("[data-cluster]");
    if (!sec) return;
    const cards = sec.querySelector("[data-ccards]");
    cards.appendChild(makeContainerCard()); // seed one
    sec.querySelector("[data-add-card]").addEventListener("click", () => cards.appendChild(makeContainerCard()));
    const tog = sec.querySelector("[data-subroot-tog]"), sub = sec.querySelector("[data-subroot]");
    tog.addEventListener("change", () => { sub.style.display = tog.checked ? "" : "none"; if (tog.checked && !sub.value) sub.value = "core"; });
    const managed = sec.querySelector("[data-managed]"), note = sec.querySelector("[data-managed-note]");
    managed.addEventListener("change", () => {
      note.textContent = managed.checked
        ? "Hull generates & owns the compose file (edit containers here)."
        : "Hull scaffolds a starter compose you then own & edit by hand.";
    });
  }

  function wireDocker(scope) {
    const sec = scope.querySelector("[data-docker]");
    if (!sec) return;
    const q = sec.querySelector("[data-docker-q]");
    const status = sec.querySelector("[data-docker-status]");
    const results = sec.querySelector("[data-docker-results]");
    const editor = sec.querySelector("[data-dockerfile]");
    const selected = new Map(); // image name -> chosen tag
    let dirty = false;
    editor.addEventListener("input", () => { dirty = true; });

    function regenDockerfile() {
      if (dirty) return;
      editor.value = dockerfileFor([...selected.entries()].map(([name, tag]) => ({ name, tag })));
    }
    // Populate a row's version select from Docker Hub tags for that image.
    async function fillVer(sel, name) {
      const tags = await H().imageTags(name);
      if (!selected.has(name)) return; // deselected while fetching
      const cur = selected.get(name) || tags[0];
      selected.set(name, cur);
      sel.innerHTML = tags.map(t => `<option ${t === cur ? "selected" : ""}>${t}</option>`).join("");
      regenDockerfile();
    }
    function render(list) {
      results.innerHTML = list.map(im => {
        const label = im.ns ? `<span class="ns">${im.ns.split("/")[0]}/</span>${im.name}` : im.name;
        const isSel = selected.has(im.name);
        return `<div class="docker-result${isSel ? " sel" : ""}" data-img="${im.name}">
          <span class="dr-ic">${icon("cube",16)}</span>
          <div style="min-width:0"><div class="dr-name">${label}</div><div class="dr-desc">${im.desc}</div></div>
          <div class="dr-meta">${im.official ? `<span class="badge-official">OFFICIAL</span>` : ""}${im.pulls ? `<span>${icon("download",12)} ${im.pulls}</span>` : ""}<button type="button" class="star-btn${H().isStarred(im.name) ? " on" : ""}" data-star="${im.name}" title="Star — keep at top">${icon("star",14)}</button><select class="select dr-ver" onclick="event.stopPropagation()" style="width:auto${isSel ? "" : ";display:none"}"></select><span class="dr-check">${icon("check",15)}</span></div>
        </div>`;
      }).join("") || `<div class="rep-empty">No images match “${q.value.trim()}”.</div>`;
      results.querySelectorAll("[data-star]").forEach(b => b.addEventListener("click", (e) => {
        e.stopPropagation(); H().toggleStar(b.dataset.star); search();
      }));
      results.querySelectorAll("[data-img]").forEach(r => {
        const name = r.dataset.img;
        const ver = r.querySelector(".dr-ver");
        if (selected.has(name) && ver) fillVer(ver, name);
        ver?.addEventListener("change", () => { selected.set(name, ver.value); regenDockerfile(); });
        r.addEventListener("click", () => {
          if (selected.has(name)) {
            selected.delete(name); r.classList.remove("sel"); if (ver) ver.style.display = "none";
          } else {
            selected.set(name, "latest"); r.classList.add("sel");
            if (ver) { ver.style.display = ""; fillVer(ver, name); }
          }
          regenDockerfile();
        });
      });
    }
    function search() {
      const term = q.value.trim();
      status.innerHTML = `${icon("search",13)} Searching Docker Hub…`;
      clearTimeout(search._t);
      search._t = setTimeout(async () => {
        const full = await H().searchImages(term);
        if (q.value.trim() !== term) return; // a newer keystroke superseded us
        const list = term ? full : full.slice(0, 8);
        status.textContent = term ? `${list.length} result${list.length === 1 ? "" : "s"} for “${term}”` : "Popular images";
        render(list);
      }, 260);
    }
    q.addEventListener("input", search);
    search();
    regenDockerfile();
    sec._selected = selected;
  }

  // entries: [{name, tag}]. The first image is the base; the rest are noted
  // as sidecars. A picked tag (other than "latest") pins the FROM line; for
  // "latest" we use the curated default tag of a known base.
  function dockerfileFor(entries) {
    if (!entries.length) return "# Select one or more images above to scaffold a Dockerfile.";
    const body = {
      node:   `WORKDIR /app\nCOPY package*.json ./\nRUN npm ci\nCOPY . .\nEXPOSE 3000\nCMD ["npm","start"]`,
      python: `WORKDIR /app\nCOPY requirements.txt .\nRUN pip install --no-cache-dir -r requirements.txt\nCOPY . .\nEXPOSE 8000\nCMD ["python","app.py"]`,
      php:    `WORKDIR /var/www/html\nCOPY . .\nEXPOSE 9000\nCMD ["php-fpm"]`,
      ruby:   `WORKDIR /app\nCOPY Gemfile* ./\nRUN bundle install\nCOPY . .\nEXPOSE 3000\nCMD ["ruby","app.rb"]`,
      nginx:  `COPY ./dist /usr/share/nginx/html\nEXPOSE 80`,
      bun:    `WORKDIR /app\nCOPY package.json bun.lockb ./\nRUN bun install\nCOPY . .\nEXPOSE 3000\nCMD ["bun","start"]`,
      deno:   `WORKDIR /app\nCOPY . .\nRUN deno cache main.ts\nEXPOSE 8000\nCMD ["deno","run","-A","main.ts"]`,
    };
    const defTag = { node: "22-alpine", python: "3.12-slim", php: "8.3-fpm-alpine", golang: "1.22-alpine", ruby: "3.3-slim", nginx: "alpine", bun: "1.1", deno: "alpine" };
    const e = entries[0], base = e.name;
    const tag = (e.tag && e.tag !== "latest") ? e.tag : (defTag[base] || "latest");
    let out;
    if (base === "golang") {
      out = `FROM golang:${tag} AS build\nWORKDIR /src\nCOPY . .\nRUN go build -o /bin/app ./...\n\nFROM alpine:3.20\nCOPY --from=build /bin/app /bin/app\nEXPOSE 8080\nCMD ["/bin/app"]`;
    } else if (body[base]) {
      out = `FROM ${base}:${tag}\n${body[base]}`;
    } else {
      out = `FROM ${base}:${tag}\nWORKDIR /app\nCOPY . .\n`;
    }
    if (entries.length > 1) out += `\n\n# Sidecar containers in compose:\n` + entries.slice(1).map(s => `#   - ${s.name}:${s.tag || "latest"}`).join("\n");
    return out;
  }

  // Shared docker-result row (single-select, with a star-to-pin button).
  function dockerResultHTML(im, sel) {
    const label = im.ns ? `<span class="ns">${im.ns.split("/")[0]}/</span>${im.name}` : im.name;
    return `<div class="docker-result${sel ? " sel" : ""}" data-img="${im.name}">
      <span class="dr-ic">${icon("cube",16)}</span>
      <div style="min-width:0"><div class="dr-name">${label}</div><div class="dr-desc">${im.desc||""}</div></div>
      <div class="dr-meta">${im.official?`<span class="badge-official">OFFICIAL</span>`:""}${im.pulls?`<span>${im.pulls}</span>`:""}<button type="button" class="star-btn${H().isStarred(im.name)?" on":""}" data-star="${im.name}" title="Star — keep at top">${icon("star",14)}</button><span class="dr-check">${icon("check",15)}</span></div>
    </div>`;
  }

  /* ---------- cluster wizard: repeatable App-style container cards ---------- */
  const DEFAULT_PORTS = { node: 3000, python: 8000, php: 9000, nginx: 80, httpd: 80, caddy: 80, redis: 6379, postgres: 5432, mysql: 3306, mariadb: 3306, mongo: 27017, golang: 8080, ruby: 3000, rabbitmq: 5672, meilisearch: 7700, minio: 9000 };

  function clusterSectionHTML() {
    return `
      <div data-cluster style="display:none">
        <div class="form-row">
          <label class="field-label">Containers</label>
          <p class="help" style="margin:-2px 0 8px">Each container is a Docker Hub image — search, pick, repeat. Ports default to the image's usual port; add linked services per container.</p>
          <div data-ccards></div>
          <button type="button" class="btn btn-sm" data-add-card>${icon("plus",14)}Add container</button>
        </div>
        <div class="form-row" style="display:flex;align-items:center;gap:12px">
          <label class="switch"><input type="checkbox" data-subroot-tog><span class="track"></span>Compose in a sub-folder</label>
          <input class="input mono" data-subroot placeholder="core" style="width:160px;display:none">
        </div>
        <div class="form-row" style="display:flex;align-items:center;gap:12px">
          <label class="switch"><input type="checkbox" data-managed checked><span class="track"></span>Hull-managed</label>
          <span class="help" style="margin:0" data-managed-note>Hull generates &amp; owns the compose file.</span>
        </div>
      </div>`;
  }

  // engine options without sqlite (a cluster service must be a real container).
  function engineOptionsHTML() {
    return H().CATALOG.map(g => `<optgroup label="${g.cat}">${
      g.items.map(it => `<option value="${it.engine}">${H().ENGINES[it.engine].label}</option>`).join("")
    }</optgroup>`).join("");
  }

  function makeCardSvcRow() {
    const row = document.createElement("div");
    row.className = "csvc-row";
    row.innerHTML = `
      <select class="select csvc-engine" style="flex:1">${engineOptionsHTML()}</select>
      <input class="input csvc-ver" placeholder="version" style="width:110px">
      <button type="button" class="btn btn-icon csvc-remove" title="Remove">${icon("x",14)}</button>`;
    const eng = row.querySelector(".csvc-engine"), ver = row.querySelector(".csvc-ver");
    const refresh = App.wireVersionField(ver, () => H().versions(eng.value), (q) => H().versionSearch(eng.value, q));
    eng.addEventListener("change", () => { ver.value = ""; refresh(); });
    row.querySelector(".csvc-remove").addEventListener("click", () => row.remove());
    return row;
  }

  function makeContainerCard() {
    const card = document.createElement("div");
    card.className = "ccard";
    card.innerHTML = `
      <div class="ccard-head">
        <input class="input" data-cname placeholder="container name (e.g. api)">
        <button type="button" class="btn btn-icon ccard-remove" title="Remove">${icon("x",15)}</button>
      </div>
      <div class="search">${icon("search",14)}<input class="input" data-cq placeholder="Search Docker Hub… node, postgres, redis"></div>
      <div class="docker-results" data-cresults></div>
      <div class="ccard-sel" data-csel style="display:none">
        <span class="chip chip-accent" data-csel-name></span>
        <input class="input mono cver" data-cver placeholder="version" style="width:120px">
        <label class="field-label" style="margin:0">port</label>
        <input class="input mono" data-cport type="number" style="width:84px">
        <label class="switch"><input type="checkbox" data-cserve checked><span class="track"></span>Serve</label>
        <div class="csvc" data-csvc>
          <div class="field-label" style="width:100%;margin:0">Linked services</div>
          <div class="csvc-rows" data-csvc-rows></div>
          <button type="button" class="btn btn-sm" data-add-svc>${icon("plus",13)}Link service</button>
        </div>
      </div>`;
    const q = card.querySelector("[data-cq]");
    const results = card.querySelector("[data-cresults]");
    const sel = card.querySelector("[data-csel]");
    const ver = card.querySelector("[data-cver]");
    const port = card.querySelector("[data-cport]");
    let verRefresh = null;

    function choose(name) {
      card.dataset.image = name;
      card.querySelector("[data-csel-name]").textContent = name;
      sel.style.display = "flex";
      if (!verRefresh) verRefresh = App.wireVersionField(ver, () => H().imageTags(card.dataset.image), (s) => H().tagSearch(card.dataset.image, s));
      ver.value = "";
      verRefresh();
      const base = name.split("/").pop();
      if (DEFAULT_PORTS[base]) port.value = DEFAULT_PORTS[base];
      results.querySelectorAll(".docker-result").forEach(r => r.classList.toggle("sel", r.dataset.img === name));
      if (!card.querySelector("[data-cname]").value) card.querySelector("[data-cname]").value = base;
    }
    function render(list) {
      results.innerHTML = list.map(im => dockerResultHTML(im, card.dataset.image === im.name)).join("")
        || `<div class="rep-empty">No images match “${q.value.trim()}”.</div>`;
      results.querySelectorAll("[data-star]").forEach(b => b.addEventListener("click", (e) => {
        e.stopPropagation(); H().toggleStar(b.dataset.star); search();
      }));
      results.querySelectorAll("[data-img]").forEach(r => r.addEventListener("click", () => choose(r.dataset.img)));
    }
    let t = null;
    function search() {
      const term = q.value.trim();
      clearTimeout(t);
      t = setTimeout(async () => {
        const full = await H().searchImages(term);
        render(term ? full : full.slice(0, 6));
      }, 240);
    }
    q.addEventListener("input", search);
    search(); // popular by default

    const svcRows = card.querySelector("[data-csvc-rows]");
    card.querySelector("[data-add-svc]").addEventListener("click", () => svcRows.appendChild(makeCardSvcRow()));
    card.querySelector(".ccard-remove").addEventListener("click", () => card.remove());
    return card;
  }

  // activeBase returns the selected location root (a configured root path or
  // the typed custom path) from the directory segmented control.
  function activeBase(scope) {
    const active = scope.querySelector("[data-dirs] .seg-btn.active");
    if (active && active.dataset.path) return active.dataset.path;
    const cp = scope.querySelector("[data-custom-path]");
    return cp && cp.value ? cp.value : "";
  }

  function readContainerCards(scope) {
    return [...scope.querySelectorAll(".ccard")].map(card => {
      const services = [...card.querySelectorAll(".csvc-row")].map(r => ({
        engine: r.querySelector(".csvc-engine").value,
        version: r.querySelector(".csvc-ver").value.trim(),
      })).filter(s => s.engine);
      return {
        name: card.querySelector("[data-cname]").value.trim(),
        image: card.dataset.image || "",
        version: card.querySelector("[data-cver]").value.trim(),
        port: parseInt(card.querySelector("[data-cport]").value, 10) || 0,
        serve: card.querySelector("[data-cserve]").checked,
        services,
      };
    }).filter(c => c.name && c.image);
  }

  function syncType(scope) {
    const type = scope.querySelector("[data-type]");
    const docker = scope.querySelector("[data-docker]");
    const php = scope.querySelector("[data-php]");
    const cluster = scope.querySelector("[data-cluster]");
    const services = scope.querySelector("[data-rep]");
    const isApp = type.value === "App";
    const isCluster = type.value === "Cluster";
    if (docker) docker.style.display = isApp ? "" : "none";
    if (php) php.style.display = (isApp || isCluster) ? "none" : "";
    if (cluster) cluster.style.display = isCluster ? "" : "none";
    if (services) services.closest(".form-row").style.display = isCluster ? "none" : "";
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
              <select class="select" data-type>${["Laravel","WordPress","Plain PHP","App","Cluster"].map(o=>`<option>${o}</option>`).join("")}</select></div>
            <div data-php><label class="field-label">PHP version</label>
              <select class="select" data-php-input></select></div>
          </div>
          <div class="form-row" style="display:flex;align-items:center;gap:14px">
            <label class="switch"><input type="checkbox" data-serve checked><span class="track"></span>Serve a domain</label>
            <span class="path-preview" data-domain style="margin-top:0"></span>
          </div>
          ${dockerSectionHTML()}
          ${clusterSectionHTML()}
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
    wireDir(scope); wireRepeater(scope); wireDocker(scope); wirePHP(scope); wireCluster(scope);
    scope.querySelector("[data-type]").addEventListener("change", () => syncType(scope));
    syncType(scope);
    scope.querySelector("[data-submit]").addEventListener("click", () => submitNew(scope));
  }

  // Map the dialog into a CreateProjectRequest. Multi-container "App" and
  // extra services beyond db+redis are backend gaps (noted to the user).
  function templateFor(type) {
    return { "Laravel": "laravel", "WordPress": "wordpress", "Plain PHP": "plain", "App": "app" }[type] || "laravel";
  }
  function dbRedisFrom(svcs) {
    const dbEngines = ["postgres", "mysql", "mariadb"];
    let db = "", redis = false, extra = [];
    svcs.forEach(s => {
      if (s.engine === "sqlite") { /* no service */ }
      else if (dbEngines.includes(s.engine) && !db) db = s.engine;
      else if (s.engine === "redis") redis = true;
      else extra.push(s.engine);
    });
    return { db, redis, extra };
  }
  function submitNew(scope) {
    const name = (scope.querySelector("[data-name]").value || "").trim();
    if (!name) { App.toast("Give the project a name"); return; }
    const type = scope.querySelector("[data-type]").value;

    if (type === "Cluster") {
      const containers = readContainerCards(scope);
      if (!containers.length) { App.toast("Add a container: name it and pick a Docker Hub image"); return; }
      const bad = containers.find(c => c.serve && !c.port);
      if (bad) { App.toast(`Container "${bad.name}" is served — set a port`); return; }
      const tog = scope.querySelector("[data-subroot-tog]");
      const sub = scope.querySelector("[data-subroot]");
      const body = {
        name,
        root: activeBase(scope),
        managed: scope.querySelector("[data-managed]").checked,
        containers,
      };
      if (tog && tog.checked && sub.value.trim()) body.compose_root = sub.value.trim();
      App.closeDialog();
      App.act(App.api("POST", "/v1/clusters/create", body), `Creating cluster ${name}…`);
      return;
    }

    const template = templateFor(type);
    if (template === "app") { App.toast("For multiple containers, pick the Cluster type"); return; }
    const phpSel = scope.querySelector("[data-php] select");
    const serveEl = scope.querySelector("[data-serve]");
    const { db, redis, extra } = dbRedisFrom(readRepeater(scope));
    const body = { name, template, php: (phpSel && phpSel.value) || "", redis };
    if (serveEl) body.serve = serveEl.checked;
    // Laravel defaults to SQLite (no db service); only WordPress needs one.
    if (db) body.db = db; else if (template === "wordpress") body.db = "mariadb";
    App.closeDialog();
    App.act(App.api("POST", "/v1/projects", body), `Creating ${name}…` + (extra.length ? ` (then link: ${extra.join(", ")})` : ""));
  }

  /* ---------- Import an unmanaged folder ---------- */
  function openImport(name) {
    App.openDialog(`
      <div class="dialog dialog-lg">
        <div class="dialog-head"><h3>Import “${name}”</h3></div>
        <div class="dialog-body">
          <div class="form-row"><span class="detect-chip" data-detect>${icon("search",13)} Detecting…</span><div data-detect-note></div></div>
          <div class="form-grid form-row">
            <div><label class="field-label">Type</label>
              <select class="select" data-type>${["Laravel","WordPress","Plain PHP"].map(o=>`<option>${o}</option>`).join("")}</select></div>
            <div data-php><label class="field-label">PHP version</label>
              <select class="select" data-php-input></select></div>
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
    wireRepeater(scope); wireDocker(scope); wirePHP(scope);
    scope.querySelector("[data-type]").addEventListener("change", () => syncType(scope));
    syncType(scope);

    // Real, file-based detection (replaces the old name-guess).
    const LABELS = { laravel: "Laravel", wordpress: "WordPress", plain: "Plain PHP" };
    App.api("GET", `/v1/detect?name=${encodeURIComponent(name)}`).then(det => {
      const chip = scope.querySelector("[data-detect]");
      const noteWrap = scope.querySelector("[data-detect-note]");
      if (det.php_kind) {
        chip.innerHTML = `${icon("check", 13)} Detected: ${LABELS[det.kind] || det.kind}`;
        const sel = scope.querySelector("[data-type]");
        if (LABELS[det.kind]) sel.value = LABELS[det.kind];
        const p = scope.querySelector("[data-php] input");
        if (det.php && p && !p.value) p.value = det.php;
        syncType(scope);
      } else {
        chip.innerHTML = `${icon("alert", 13)} Detected a ${det.kind} project`;
        noteWrap.innerHTML = `<p class="help" style="margin:8px 0 0">Hull imports PHP sites directly. To run a <b>${det.kind}</b> project, create it as a <b>Cluster</b> (New ▸ Cluster) — importing here would treat it as plain PHP.</p>`;
      }
    }).catch(() => {
      const chip = scope.querySelector("[data-detect]");
      if (chip) chip.innerHTML = `${icon("alert", 13)} Detection unavailable`;
    });

    scope.querySelector("[data-sql-browse]").addEventListener("click", async () => {
      const p = await App.pick("file", { title: "Choose a SQL dump", filters: [{ name: "SQL / SQLite", extensions: ["sql", "gz", "zip", "sqlite"] }] });
      if (p) scope.querySelector(".browse-row .input").value = p;
    });
    scope.querySelector("[data-submit]").addEventListener("click", () => {
      App.closeDialog();
      App.act(App.api("POST", "/v1/imports", { name }), `Importing ${name}… (auto-detecting framework)`);
    });
  }

  /* ---------- Adopt an existing compose project as a cluster ---------- */
  function openCluster() {
    App.openDialog(`
      <div class="dialog dialog-lg">
        <div class="dialog-head"><h3>Adopt a cluster</h3></div>
        <div class="dialog-body">
          <p class="help" style="margin:0 0 12px">Wrap an existing docker compose project so Hull manages it as one unit (start/stop/rebuild/reset together). Your compose files are never modified.</p>
          <div class="form-row"><label class="field-label">Project folder</label>
            <div class="browse-row">
              <input class="input mono" data-cl-dir placeholder="path to a compose project">
              <button type="button" class="btn" data-cl-browse>${icon("folder",15)}Browse</button>
            </div></div>
          <div class="form-grid form-row">
            <div><label class="field-label">Compose root</label><input class="input mono" data-cl-root placeholder="core (blank = project root)"></div>
            <div><label class="field-label">Profiles</label><input class="input mono" data-cl-profiles placeholder="dev (comma-separated)"></div>
          </div>
          <p class="help">The folder must sit inside one of your project roots to appear in the list.</p>
        </div>
        <div class="dialog-foot">
          <button class="btn" data-dialog-close>Cancel</button>
          <button class="btn btn-primary" data-cl-submit>${icon("cube",15)}Adopt cluster</button>
        </div>
      </div>`);
    const scope = document.querySelector(".dialog");
    scope.querySelector("[data-cl-browse]").addEventListener("click", async () => {
      const p = await App.pick("folder", { title: "Choose a compose project to adopt" });
      if (p) scope.querySelector("[data-cl-dir]").value = p;
    });
    scope.querySelector("[data-cl-submit]").addEventListener("click", async () => {
      const dir = (scope.querySelector("[data-cl-dir]").value || "").trim();
      if (!dir) { App.toast("Pick the project folder"); return; }
      const root = (scope.querySelector("[data-cl-root]").value || "").trim();
      const profs = (scope.querySelector("[data-cl-profiles]").value || "").split(",").map(x => x.trim()).filter(Boolean);
      const body = { dir };
      if (root) body.compose_root = root;
      if (profs.length) body.profiles = profs;
      try {
        const res = await App.api("POST", "/v1/clusters", body);
        App.closeDialog(); await App.reload();
        App.toast(`Cluster ${res && res.name ? res.name : ""} adopted`);
      } catch (e) { App.toast(e.message || "Adopt failed"); }
    });
  }

  window.ProjectDialog = { openNew, openImport, openCluster };
})();
