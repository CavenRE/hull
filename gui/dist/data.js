/* Hull — LIVE data layer. Replaces the design mock: static catalogs +
   state shaped from the hulld API into the structure the screen modules
   expect (window.HULL.ROOTS / SERVICES / HEALTH / JOBS / DIRS / ...). */
(function () {
  // ---- static catalogs (engine metadata, pickers) ----
  const ENGINES = {
    postgres:   { label: "PostgreSQL", cat: "Database", icon: "database" },
    mysql:      { label: "MySQL",      cat: "Database", icon: "database" },
    mariadb:    { label: "MariaDB",    cat: "Database", icon: "database" },
    redis:      { label: "Redis",      cat: "Cache",    icon: "cache" },
    memcached:  { label: "Memcached",  cat: "Cache",    icon: "cache" },
    meilisearch:{ label: "Meilisearch",cat: "Search",   icon: "search2" },
    typesense:  { label: "Typesense",  cat: "Search",   icon: "search2" },
    minio:      { label: "MinIO",      cat: "Storage",  icon: "storage" },
    mailpit:    { label: "Mailpit",    cat: "Mail",     icon: "mail" },
    adminer:    { label: "Adminer",    cat: "Tool",     icon: "tool" },
    sqlite:     { label: "SQLite",     cat: "Database", icon: "database" },
  };
  const CATALOG = [
    { cat: "Database", items: [
      { engine: "postgres", blurb: "Object-relational SQL database.", versions: ["16", "15", "14"] },
      { engine: "mysql",    blurb: "The world's most-used SQL database.", versions: ["8.4", "8.0"] },
      { engine: "mariadb",  blurb: "Community MySQL fork.", versions: ["lts", "11", "10.11"] },
    ]},
    { cat: "Cache", items: [
      { engine: "redis",     blurb: "In-memory key-value store.", versions: ["alpine", "7", "6"] },
      { engine: "memcached", blurb: "Distributed memory cache.", versions: ["alpine"] },
    ]},
    { cat: "Search", items: [
      { engine: "meilisearch", blurb: "Lightning-fast full-text search.", versions: ["v1.11"] },
      { engine: "typesense",   blurb: "Typo-tolerant search engine.", versions: ["27.1"] },
    ]},
    { cat: "Storage", items: [
      { engine: "minio", blurb: "S3-compatible object storage.", versions: ["latest"] },
    ]},
    { cat: "Mail", items: [
      { engine: "mailpit", blurb: "Catches outgoing mail for testing.", versions: ["latest"] },
    ]},
    { cat: "Tool", items: [
      { engine: "adminer", blurb: "Web database management UI.", versions: ["latest"] },
    ]},
  ];
  const CONTAINERS = [];
  const DOCKER_IMAGES = [
    { name: "node",     desc: "Node.js JavaScript runtime",     official: true,  pulls: "1B+" },
    { name: "python",   desc: "Python interpreter + pip",       official: true,  pulls: "1B+" },
    { name: "php",      desc: "PHP with FPM / CLI variants",    official: true,  pulls: "500M+" },
    { name: "nginx",    desc: "High-performance web server",    official: true,  pulls: "1B+" },
    { name: "redis",    desc: "In-memory data store",           official: true,  pulls: "1B+" },
    { name: "postgres", desc: "PostgreSQL relational database", official: true,  pulls: "1B+" },
    { name: "mysql",    desc: "MySQL relational database",      official: true,  pulls: "1B+" },
    { name: "golang",   desc: "Go toolchain",                   official: true,  pulls: "500M+" },
    { name: "ruby",     desc: "Ruby interpreter + bundler",     official: true,  pulls: "100M+" },
    { name: "caddy",    desc: "Web server with automatic HTTPS",official: true,  pulls: "100M+" },
    { name: "alpine",   desc: "Minimal Linux base image",       official: true,  pulls: "1B+" },
    { name: "bun",      desc: "Fast all-in-one JS runtime",     official: false, ns: "oven/bun", pulls: "10M+" },
  ];

  // ---- live, populated by load() ----
  const HULL = {
    ENGINES, CATALOG, CONTAINERS, DOCKER_IMAGES,
    DIRS: [], DEPENDENCIES: [], HEALTH: [], ROOTS: [], SERVICES: [], JOBS: [],
    status: null, config: null, tld: "test",
  };

  // ---- API plumbing ----
  let api = null; // { base, token }
  HULL.connected = () => !!api;

  HULL.connect = async function () {
    const tauri = window.__TAURI__;
    if (!tauri || !tauri.core || !tauri.core.invoke) throw new Error("Tauri bridge unavailable");
    const info = await tauri.core.invoke("daemon_info");
    api = { base: `http://127.0.0.1:${info.port}`, token: info.token };
    await HULL.load();
    return true;
  };

  HULL.api = async function (method, path, body) {
    const r = await fetch(api.base + path, {
      method,
      headers: { Authorization: `Bearer ${api.token}`, ...(body ? { "Content-Type": "application/json" } : {}) },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!r.ok) {
      let msg = `HTTP ${r.status}`;
      try { msg = (await r.json()).error || msg; } catch (e) {}
      throw new Error(msg);
    }
    return r.status === 204 ? null : r.json();
  };
  HULL.sseURL = (path) => `${api.base}${path}${path.includes("?") ? "&" : "?"}token=${api.token}`;
  HULL.base = () => api && api.base;

  function baseName(p) { return (p || "").replace(/[\/\\]+$/, "").split(/[\/\\]/).pop() || p; }
  function shortRoot(p) { const parts = (p || "").replace(/[\/\\]+$/, "").split(/[\/\\]/).filter(Boolean); return parts.slice(-2).join("/"); }

  HULL.load = async function () {
    // status + config are connection-critical (failure = not really
    // connected). The rest degrade to empty so one bad endpoint never
    // blanks the whole app.
    const [status, config] = await Promise.all([
      HULL.api("GET", "/v1/status"),
      HULL.api("GET", "/v1/config"),
    ]);
    const [projects, services, doctor, jobs, groupDoc] = await Promise.all([
      HULL.api("GET", "/v1/projects").catch(() => []),
      HULL.api("GET", "/v1/services").catch(() => []),
      HULL.api("GET", "/v1/doctor").catch(() => []),
      HULL.api("GET", "/v1/jobs").catch(() => []),
      HULL.api("GET", "/v1/groups").catch(() => ({ roots: {}, members: {} })),
    ]);
    HULL.GROUPS = groupDoc || { roots: {}, members: {} };
    HULL.status = status;
    HULL.config = config;
    HULL.tld = (status && status.tld) || "test";

    // DIRS from configured roots
    HULL.DIRS = (config.roots || []).map(p => ({ label: baseName(p), path: p.replace(/\\/g, "/") }));

    // SERVICES — map API shape to the screen shape
    HULL.SERVICES = (services || []).map(s => ({
      name: s.name, engine: s.engine, version: s.version || "",
      status: s.running ? "running" : "stopped",
      host: s.host || "127.0.0.1", host_port: s.host_port || null,
      username: s.username || null, password: "",
      url: s.url || null, linked: s.linked_projects || [],
    }));

    // ROOTS — group projects by configured root
    const roots = (config.roots || []).map(p => ({ path: p.replace(/\\/g, "/"), _abs: p, managed: [], unmanaged: [] }));
    const other = { path: "other", _abs: "", managed: [], unmanaged: [] };
    const norm = (p) => (p || "").replace(/\\/g, "/").replace(/\/+$/, "");
    function rootFor(dir) {
      const d = norm(dir);
      let best = null;
      for (const r of roots) { const rp = norm(r._abs); if (d.startsWith(rp) && (!best || rp.length > norm(best._abs).length)) best = r; }
      return best || other;
    }
    (projects || []).forEach(p => {
      const r = rootFor(p.dir);
      if (p.kind === "folder") { r.unmanaged.push(p.name); return; }
      r.managed.push({
        name: p.name,
        kind: p.kind === "laravel" || p.kind === "wordpress" || p.kind === "app" ? p.kind : (p.kind === "plain" ? "php" : p.kind),
        php: p.php || null,
        status: p.error ? "error" : p.running ? "running" : "stopped",
        url: p.url || (p.served === false ? "" : `https://${p.name}.${HULL.tld}`),
        served: p.served !== false,
        group: p.group || "",
        isCluster: p.kind === "cluster",
        routes: p.routes || [],
        dir: (p.dir || "").replace(/\\/g, "/"),
        rawDir: p.dir || "",
        error: p.error || "",
        services: (p.services || []).map(l => ({
          key: l.key, engine: l.engine, version: l.version || "", mode: l.mode,
          instance: l.instance || l.engine,
        })),
      });
    });
    HULL.ROOTS = roots.filter(r => r.managed.length || r.unmanaged.length);
    if (other.managed.length || other.unmanaged.length) HULL.ROOTS.push(other);

    // HEALTH from doctor — pick the headline checks
    const pick = (needle) => (doctor || []).find(c => c.name.toLowerCase().includes(needle));
    const st = (c) => !c ? "warn" : c.status === "ok" ? "ok" : c.status === "warn" ? "warn" : "err";
    const eng = pick("container engine") || pick("docker");
    const clip = (s) => { s = (s || "—").split(" — ")[0]; return s.length > 38 ? s.slice(0, 37) + "…" : s; };
    HULL.HEALTH = [
      { icon: "server", name: "Engine",      status: st(eng), detail: clip(eng && eng.detail) },
      { icon: "route",  name: "Router",      status: st(pick("router")), detail: clip((pick("router") || {}).detail) },
      { icon: "globe",  name: "DNS",         status: st(pick("resolution") || pick("dns")), detail: clip((pick("resolution") || pick("dns") || {}).detail) },
      { icon: "cert",   name: "Certificate", status: st(pick("cert")), detail: clip((pick("certificate") || {}).detail) },
    ];
    HULL._doctor = doctor || [];

    // DEPENDENCIES — reframed: Docker is the only true external dep; the
    // router/DNS/cert are embedded in the daemon (shown as built-in).
    const dockerOk = eng && eng.status === "ok";
    HULL.DEPENDENCIES = [
      { name: "Docker Engine", key: "docker", installed: dockerOk ? (eng.detail || "installed") : null,
        latest: "", status: dockerOk ? "ok" : "missing", blurb: "Container runtime — Hull's one external dependency" },
      { name: "Caddy router", key: "caddy", installed: "embedded", latest: "embedded", status: "ok", blurb: "Built into the Hull daemon" },
      { name: "Local DNS", key: "dns", installed: "embedded", latest: "embedded", status: "ok", blurb: "Built into the Hull daemon" },
      { name: "Local CA (certs)", key: "ca", installed: "embedded", latest: "embedded", status: "ok", blurb: "Built into the Hull daemon" },
    ];

    // JOBS → recent activity feed
    HULL.JOBS = (jobs || []).slice(0, 8).map(j => ({
      t: (j.created || "").slice(11, 19) || "",
      kind: j.status === "done" ? "ok" : j.status === "failed" ? "err" : "warn",
      msg: j.kind, detail: (j.lines && j.lines.length ? j.lines[j.lines.length - 1] : ""),
    }));
  };

  // ---- virtual groups (per-root organizational labels) ----
  function normKey(p) { return (p || "").replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase(); }
  // Ordered group names defined for a root (matches keys across path formats).
  HULL.groupsFor = function (rootPath) {
    const want = normKey(rootPath);
    const roots = (HULL.GROUPS && HULL.GROUPS.roots) || {};
    for (const k in roots) if (normKey(k) === want) return (roots[k] && roots[k].groups) || [];
    return [];
  };
  // The store's existing key for a root, or the GUI path (backend normalizes).
  HULL.groupRootKey = function (rootPath) {
    const want = normKey(rootPath);
    const roots = (HULL.GROUPS && HULL.GROUPS.roots) || {};
    for (const k in roots) if (normKey(k) === want) return k;
    return rootPath;
  };
  // Create a group under a root (PUT the whole doc; backend canonicalizes keys).
  HULL.addGroup = async function (rootPath, name) {
    const doc = JSON.parse(JSON.stringify(HULL.GROUPS || { roots: {}, members: {} }));
    if (!doc.roots) doc.roots = {};
    const k = HULL.groupRootKey(rootPath);
    if (!doc.roots[k]) doc.roots[k] = { groups: [] };
    if (!doc.roots[k].groups) doc.roots[k].groups = [];
    if (!doc.roots[k].groups.includes(name)) doc.roots[k].groups.push(name);
    await HULL.api("PUT", "/v1/groups", doc);
  };
  // Replace a root's group order.
  HULL.setGroupOrder = async function (rootPath, order) {
    const doc = JSON.parse(JSON.stringify(HULL.GROUPS || { roots: {}, members: {} }));
    if (!doc.roots) doc.roots = {};
    const k = HULL.groupRootKey(rootPath);
    doc.roots[k] = { groups: order };
    await HULL.api("PUT", "/v1/groups", doc);
  };

  // slug mirrors the Go manifest.Slug: domain-safe label (lowercase, hyphens).
  HULL.slug = function (s) {
    return (s || "").toLowerCase().trim()
      .replace(/[\s_.]+/g, "-")
      .replace(/[^a-z0-9-]/g, "")
      .replace(/-+/g, "-")
      .replace(/^-+|-+$/g, "");
  };

  // ---- live Docker Hub lookups (version pickers + App search) ----
  // Each resolves to live data when the daemon can reach Docker Hub, and
  // falls back to the static catalog above so pickers are never empty.
  const _vcache = {};
  function staticVersions(engine) {
    for (const g of CATALOG) for (const it of g.items)
      if (it.engine === engine) return it.versions.slice();
    return [];
  }
  HULL.versions = async function (engine) {
    if (_vcache[engine]) return _vcache[engine];
    const fallback = staticVersions(engine);
    try {
      const live = await HULL.api("GET", `/v1/registry/versions?engine=${encodeURIComponent(engine)}`);
      const out = (live && live.length) ? live : fallback;
      _vcache[engine] = out;
      return out;
    } catch (e) { return fallback; }
  };
  const PHP_FALLBACK = ["8.4", "8.3", "8.2", "8.1"];
  HULL.phpVersions = async function () {
    if (_vcache.__php) return _vcache.__php;
    try {
      const live = await HULL.api("GET", "/v1/registry/php");
      const out = (live && live.length) ? live : PHP_FALLBACK;
      _vcache.__php = out;
      return out;
    } catch (e) { return PHP_FALLBACK; }
  };
  // Live version search (specific tags) — q filters Docker Hub tags.
  HULL.versionSearch = async function (engine, q) {
    try { return await HULL.api("GET", `/v1/registry/versions?engine=${encodeURIComponent(engine)}&q=${encodeURIComponent(q)}`); } catch (e) { return []; }
  };
  HULL.phpSearch = async function (q) {
    try { return await HULL.api("GET", `/v1/registry/php?q=${encodeURIComponent(q)}`); } catch (e) { return []; }
  };
  HULL.tagSearch = async function (repo, q) {
    try { return await HULL.api("GET", `/v1/registry/tags?repo=${encodeURIComponent(repo)}&q=${encodeURIComponent(q)}`); } catch (e) { return []; }
  };

  HULL.imageTags = async function (repo) {
    if (!repo) return ["latest"];
    const ck = "__tags_" + repo;
    if (_vcache[ck]) return _vcache[ck];
    try {
      const live = await HULL.api("GET", `/v1/registry/tags?repo=${encodeURIComponent(repo)}`);
      const out = (live && live.length) ? live : ["latest"];
      _vcache[ck] = out;
      return out;
    } catch (e) { return ["latest"]; }
  };
  // ---- starred images (favorites, persisted) ----
  HULL.starredImages = function () { try { return JSON.parse(localStorage.getItem("hull-starred-images") || "[]"); } catch (e) { return []; } };
  HULL.isStarred = function (name) { return HULL.starredImages().includes(name); };
  HULL.toggleStar = function (name) {
    const set = HULL.starredImages();
    const i = set.indexOf(name);
    if (i >= 0) set.splice(i, 1); else set.unshift(name);
    localStorage.setItem("hull-starred-images", JSON.stringify(set));
    return HULL.isStarred(name);
  };

  HULL.searchImages = async function (q) {
    q = (q || "").trim();
    let list;
    if (!q) {
      list = DOCKER_IMAGES.slice();
    } else {
      list = null;
      try {
        const live = await HULL.api("GET", `/v1/registry/search?q=${encodeURIComponent(q)}`);
        if (live && live.length) list = live.map(r => ({
          name: r.name, desc: r.description || "", official: !!r.official,
          pulls: r.stars ? `★ ${r.stars}` : "",
          ns: r.name.includes("/") ? r.name : undefined,
        }));
      } catch (e) {}
      if (!list) {
        const ql = q.toLowerCase();
        list = DOCKER_IMAGES.filter(d => d.name.toLowerCase().includes(ql) || (d.desc || "").toLowerCase().includes(ql));
      }
    }
    // Surface starred images: include matching starred not already present,
    // then float all starred to the top of the list.
    const stars = HULL.starredImages();
    const have = new Set(list.map(x => x.name));
    const ql = q.toLowerCase();
    const extra = stars
      .filter(n => !have.has(n) && (!q || n.toLowerCase().includes(ql)))
      .map(n => ({ name: n, desc: "", official: !n.includes("/"), ns: n.includes("/") ? n : undefined }));
    return [...extra, ...list].sort((a, b) => (stars.includes(b.name) ? 1 : 0) - (stars.includes(a.name) ? 1 : 0));
  };

  window.HULL = HULL;
})();
