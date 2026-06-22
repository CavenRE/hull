/* Hull — realistic sample data (stands in for the local HTTP API JSON). */
(function () {
  // Engine metadata: category, label, glyph icon, accent dot kind
  const ENGINES = {
    postgres:  { label: "PostgreSQL", cat: "Database", icon: "database" },
    mysql:     { label: "MySQL",      cat: "Database", icon: "database" },
    mariadb:   { label: "MariaDB",    cat: "Database", icon: "database" },
    redis:     { label: "Redis",      cat: "Cache",    icon: "cache" },
    memcached: { label: "Memcached",  cat: "Cache",    icon: "cache" },
    meilisearch:{label: "Meilisearch",cat: "Search",   icon: "search2" },
    typesense: { label: "Typesense",  cat: "Search",   icon: "search2" },
    minio:     { label: "MinIO",      cat: "Storage",  icon: "storage" },
    mailpit:   { label: "Mailpit",    cat: "Mail",     icon: "mail" },
    adminer:   { label: "Adminer",    cat: "Tool",     icon: "tool" },
    sqlite:    { label: "SQLite",     cat: "Database", icon: "database" },
  };

  // Popular base containers for the "App" project type
  const CONTAINERS = [
    { key: "node",   label: "Node.js",       blurb: "JavaScript / TypeScript runtime.", versions: ["22", "20", "18"] },
    { key: "python", label: "Python",        blurb: "CPython with pip / venv.",         versions: ["3.12", "3.11", "3.10"] },
    { key: "php",    label: "PHP-FPM",       blurb: "FastCGI process manager.",          versions: ["8.3", "8.2", "8.1"] },
    { key: "bun",    label: "Bun",           blurb: "Fast all-in-one JS runtime.",       versions: ["1.1"] },
    { key: "go",     label: "Go",            blurb: "Compiled toolchain.",               versions: ["1.22", "1.21"] },
    { key: "ruby",   label: "Ruby",          blurb: "Ruby with bundler.",                versions: ["3.3", "3.2"] },
    { key: "static", label: "Static / Nginx", blurb: "Serve a built static directory.",   versions: ["—"] },
  ];

  // Engine catalog for the "Add instance" picker, grouped by category
  const CATALOG = [
    { cat: "Database", items: [
      { engine: "postgres", blurb: "Object-relational SQL database.", versions: ["16", "15", "14"] },
      { engine: "mysql",    blurb: "The world's most-used SQL database.", versions: ["8.4", "8.0"] },
      { engine: "mariadb",  blurb: "Community MySQL fork.", versions: ["11", "10.11"] },
    ]},
    { cat: "Cache", items: [
      { engine: "redis",     blurb: "In-memory key-value store.", versions: ["7", "6"] },
      { engine: "memcached", blurb: "Distributed memory cache.", versions: ["1.6"] },
    ]},
    { cat: "Search", items: [
      { engine: "meilisearch", blurb: "Lightning-fast full-text search.", versions: ["1.8"] },
      { engine: "typesense",   blurb: "Typo-tolerant search engine.", versions: ["27"] },
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

  // Sites grouped by root folder. status: running | stopped | error
  const ROOTS = [
    {
      path: "C:/Users/caven/Sites",
      managed: [
        { name: "acme", kind: "laravel", php: "8.3", status: "running", url: "https://acme.test",
          services: [{ key: "db", instance: "postgres-16" }, { key: "cache", instance: "redis" }, { key: "mail", instance: "mailpit" }] },
        { name: "atlas", kind: "laravel", php: "8.3", status: "stopped", url: "https://atlas.test",
          services: [{ key: "db", instance: "postgres-16" }, { key: "mail", instance: "mailpit" }] },
        { name: "shipyard", kind: "app", php: null, status: "running", url: "https://shipyard.test",
          services: [{ key: "db", instance: "mysql-8" }, { key: "cache", instance: "redis" }] },
        { name: "ledger-api", kind: "laravel", php: "8.2", status: "running", url: "https://ledger-api.test",
          services: [{ key: "db", instance: "postgres-16" }] },
        { name: "marketing", kind: "wordpress", php: "8.1", status: "error", url: "https://marketing.test",
          services: [{ key: "db", instance: "mysql-8" }] },
        { name: "pixel-press", kind: "wordpress", php: "8.2", status: "stopped", url: "https://pixel-press.test",
          services: [{ key: "db", instance: "mariadb-11" }] },
        { name: "snippets", kind: "php", php: "8.3", status: "running", url: "https://snippets.test", services: [] },
      ],
      unmanaged: ["old-blog", "design-scratch"],
    },
    {
      path: "C:/Users/caven/Work/clients",
      managed: [
        { name: "acme-store", kind: "laravel", php: "8.2", status: "running", url: "https://acme-store.test",
          services: [{ key: "db", instance: "mysql-8" }, { key: "cache", instance: "redis" }, { key: "search", instance: "meilisearch" }, { key: "mail", instance: "mailpit" }] },
        { name: "northwind-cms", kind: "wordpress", php: "8.1", status: "stopped", url: "https://northwind-cms.test",
          services: [{ key: "db", instance: "mariadb-11" }] },
        { name: "harbor-docs", kind: "app", php: null, status: "stopped", url: "https://harbor-docs.test",
          services: [{ key: "storage", instance: "minio" }] },
      ],
      unmanaged: ["legacy-import"],
    },
    {
      path: "C:/Users/caven/oss",
      managed: [
        { name: "hull-site", kind: "php", php: "8.3", status: "running", url: "https://hull-site.test", services: [] },
        { name: "feedreader", kind: "laravel", php: "8.3", status: "running", url: "https://feedreader.test",
          services: [{ key: "db", instance: "postgres-16" }, { key: "cache", instance: "redis" }] },
      ],
      unmanaged: [],
    },
  ];

  // Service instances. Versions can coexist.
  const SERVICES = [
    { name: "postgres-16", engine: "postgres", version: "16", status: "running",
      host: "127.0.0.1", host_port: 54320, username: "postgres", password: "", url: null,
      linked: ["acme", "atlas", "ledger-api", "feedreader"] },
    { name: "mysql-8", engine: "mysql", version: "8.0", status: "running",
      host: "127.0.0.1", host_port: 33060, username: "root", password: "", url: null,
      linked: ["shipyard", "marketing", "acme-store"] },
    { name: "mariadb-11", engine: "mariadb", version: "11", status: "stopped",
      host: "127.0.0.1", host_port: 33061, username: "root", password: "", url: null,
      linked: ["pixel-press", "northwind-cms"] },
    { name: "redis", engine: "redis", version: "7", status: "running",
      host: "127.0.0.1", host_port: 63790, username: null, password: "", url: null,
      linked: ["acme", "shipyard", "acme-store", "feedreader"] },
    { name: "meilisearch", engine: "meilisearch", version: "1.8", status: "running",
      host: "127.0.0.1", host_port: 7700, username: null, password: "", url: "http://localhost:7700",
      linked: ["acme-store"] },
    { name: "minio", engine: "minio", version: "latest", status: "running",
      host: "127.0.0.1", host_port: 9000, username: "minioadmin", password: "minioadmin", url: "http://localhost:9001",
      linked: ["harbor-docs"] },
    { name: "mailpit", engine: "mailpit", version: "latest", status: "running",
      host: "127.0.0.1", host_port: 1025, username: null, password: "", url: "http://localhost:8025",
      linked: ["acme", "atlas", "acme-store"] },
  ];

  // Recent jobs for the dashboard activity feed
  const JOBS = [
    { t: "10:42:03", kind: "ok",   msg: "Started postgres-16", detail: "ready in 812ms" },
    { t: "10:41:55", kind: "ok",   msg: "Started acme", detail: "php 8.3 · https://acme.test" },
    { t: "10:40:12", kind: "warn", msg: "Certificate renewal due", detail: "*.test expires in 12 days" },
    { t: "10:38:41", kind: "ok",   msg: "Linked redis to feedreader", detail: "" },
    { t: "10:31:09", kind: "err",  msg: "marketing failed to boot", detail: "php-fpm exited (code 1)" },
    { t: "10:30:58", kind: "ok",   msg: "Started meilisearch", detail: "host_port 7700" },
  ];

  // Preset base directories offered in the New-project dialog (also editable in Settings)
  const DIRS = [
    { label: "Sites", path: "C:/Users/caven/Sites" },
    { label: "Apps",  path: "C:/Users/caven/Apps" },
    { label: "Work",  path: "C:/Users/caven/Work" },
  ];

  // Popular Docker Hub images surfaced by the App container search (stands in for a live registry query)
  const DOCKER_IMAGES = [
    { name: "node",        desc: "Node.js JavaScript runtime",        official: true,  pulls: "1B+" },
    { name: "python",      desc: "Python interpreter + pip",          official: true,  pulls: "1B+" },
    { name: "php",         desc: "PHP with FPM / CLI variants",       official: true,  pulls: "500M+" },
    { name: "nginx",       desc: "High-performance web server",       official: true,  pulls: "1B+" },
    { name: "httpd",       desc: "Apache HTTP server",                official: true,  pulls: "1B+" },
    { name: "redis",       desc: "In-memory data store",              official: true,  pulls: "1B+" },
    { name: "postgres",    desc: "PostgreSQL relational database",    official: true,  pulls: "1B+" },
    { name: "mysql",       desc: "MySQL relational database",         official: true,  pulls: "1B+" },
    { name: "mongo",       desc: "MongoDB document database",         official: true,  pulls: "1B+" },
    { name: "golang",      desc: "Go toolchain",                      official: true,  pulls: "500M+" },
    { name: "ruby",        desc: "Ruby interpreter + bundler",        official: true,  pulls: "100M+" },
    { name: "rust",        desc: "Rust toolchain",                    official: true,  pulls: "50M+" },
    { name: "openjdk",     desc: "Java (OpenJDK)",                    official: true,  pulls: "500M+" },
    { name: "elixir",      desc: "Elixir + Erlang/OTP",               official: true,  pulls: "50M+" },
    { name: "caddy",       desc: "Web server with automatic HTTPS",   official: true,  pulls: "100M+" },
    { name: "alpine",      desc: "Minimal Linux base image",          official: true,  pulls: "1B+" },
    { name: "ubuntu",      desc: "Ubuntu base image",                 official: true,  pulls: "1B+" },
    { name: "bun",         desc: "Fast all-in-one JS runtime",        official: false, ns: "oven/bun",            pulls: "10M+" },
    { name: "deno",        desc: "Secure JS / TS runtime",            official: false, ns: "denoland/deno",       pulls: "10M+" },
    { name: "meilisearch", desc: "Lightning-fast full-text search",   official: false, ns: "getmeili/meilisearch", pulls: "50M+" },
    { name: "minio",       desc: "S3-compatible object storage",      official: false, ns: "minio/minio",         pulls: "500M+" },
  ];

  // Dependency packages Hull manages — surfaced in Settings → Updates
  const DEPENDENCIES = [
    { name: "Docker Engine", key: "docker", installed: "27.0.3", latest: "27.1.1", status: "update", blurb: "Container runtime" },
    { name: "Caddy",         key: "caddy",  installed: "2.8.4",  latest: "2.8.4",  status: "ok",     blurb: "HTTPS router" },
    { name: "mkcert",        key: "mkcert", installed: "1.4.4",  latest: "1.4.4",  status: "ok",     blurb: "Local certificate authority" },
    { name: "PHP 8.3",       key: "php83",  installed: "8.3.8",  latest: "8.3.9",  status: "update", blurb: "Runtime" },
    { name: "dnsmasq",       key: "dnsmasq",installed: null,     latest: "2.90",   status: "missing",blurb: "Local DNS resolver" },
  ];

  // System health — shown on the Dashboard and the daemon hover popover
  const HEALTH = [
    { icon: "server", name: "Engine",      status: "ok",   detail: "Docker · 7 containers up" },
    { icon: "route",  name: "Router",      status: "ok",   detail: "Caddy · :80 / :443" },
    { icon: "globe",  name: "DNS",         status: "ok",   detail: "*.test → 127.0.0.1" },
    { icon: "cert",   name: "Certificate", status: "warn", detail: "renews in 12 days" },
  ];

  window.HULL = { ENGINES, CATALOG, CONTAINERS, DIRS, DOCKER_IMAGES, DEPENDENCIES, HEALTH, ROOTS, SERVICES, JOBS };
})();
