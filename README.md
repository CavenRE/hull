<div align="center">

<img src="docs/logo.png" width="96" alt="Hull logo">

# Hull

**A fast, cross-platform local development environment.**
Docker-based dev sites with automatic HTTPS domains, shared databases, and a one-command setup, driven by a CLI and a background daemon.

Runs on **Windows · macOS · Linux** (Arch & Debian/Ubuntu).

</div>

---

## What is Hull?

Hull provisions Docker-based local development environments and serves each project at a trusted `https://<name>.test` address: no port juggling, no manual nginx/Caddy config, no `/etc/hosts` editing.

It scaffolds Laravel, WordPress, plain-PHP, static, Python, Node, and Go projects in one command, runs shared database instances multiple projects can share, and routes everything through an embedded HTTPS reverse proxy with a locally-trusted certificate authority.

Hull v2 is a ground-up **Go rewrite** of the original bash tool (which lives on the [`legacy`](../../tree/legacy) branch). It is cross-platform and daemon-backed, and the CLI is fully featured on its own.

> **CLI-first:** Hull is the CLI + daemon, and every command works standalone; when the daemon is running the same commands route through it. Native desktop apps are in development, one per platform (see [Desktop apps](#desktop-apps-coming-soon)).

> **Source of truth:** every project is described by a small `hull.yaml`. The `compose.yaml` Hull runs is a generated artifact, never hand-edited.

---

## Table of contents

- [Features](#features)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Installation](#installation)
- [First-run setup](#first-run-setup)
- [Quick start](#quick-start)
- [CLI reference](#cli-reference)
- [Desktop apps](#desktop-apps-coming-soon)
- [Configuration](#configuration)
- [Platform notes](#platform-notes)
- [Updating](#updating)
- [Uninstalling](#uninstalling)
- [Philosophy & contributing](#philosophy--contributing)
- [License](#license)

---

## Features

- **One-command scaffolding**: `hull new shop laravel --db postgres` creates the project, wires the framework, and boots it at `https://shop.test`.
- **Automatic HTTPS & DNS**: an embedded Caddy reverse proxy with a local root CA serves every site over trusted TLS; a built-in wildcard resolver answers `*.test` (no `dnsmasq` container required).
- **Shared service instances**: run `postgres-16`, `mariadb-lts`, `redis`, etc. once and link many projects to them; multiple versions live side by side.
- **A database login for each project**: a new database gets a generated password, and a project linked to a shared instance gets a user that can reach only its own database.
- **Central database console**: attach a database to anything and Hull auto-starts Adminer at `https://db.test`, one browser console for your MySQL, MariaDB, and PostgreSQL databases.
- **You approve what runs**: Hull starts a project and runs its hooks only after you approve it, and asks again when its `hull.yaml` or compose files change.
- **Fast PHP on a Windows drive**: the daemon watches your sites and refreshes only the PHP files you save in the opcode cache, so PHP can skip checking every file on every request.
- **Headless or daemon-backed**: the CLI is fully featured on its own. A running daemon adds live routing and background jobs, but is never required.
- **Portable bundles**: `hull export` produces a `<name>-bundle.zip` (project + fresh DB dumps, secrets blanked) that `hull import` restores on another machine; `hull db export` and `hull db import` move just one database.
- **Adopt what you already have**: import existing projects where they live, park a folder of projects, wrap multi-container `docker compose` stacks as **clusters**, or migrate projects from bash-Hull (v1).
- **Ephemeral & native tooling**: `hull npm run dev` runs in a throwaway Node container; `hull artisan`, `hull composer`, `hull python`, `hull pip`, `hull node`, `hull go`, and `hull exec` run straight against your project's app container, with no host pollution.

---

## How it works

```
          ┌──────────────┐        ┌──────────────┐
 hull ───▶│  hull daemon │───────▶│  Docker      │
 (CLI)    │  (daemon)    │        │  Engine      │
          │              │        └──────────────┘
          │  • engine    │   embedded, in-process:
          │  • router    │   • Caddy HTTPS proxy + local CA
          │  • DNS       │   • wildcard *.test resolver
          │  • services  │   • OS trust-store integration
          └──────────────┘
                 ▲
        hull.yaml │ generates  compose.yaml ──▶ docker compose
```

- **The daemon** (`hull daemon run` in the foreground, or `hull start` in the background) owns everything: the project engine, shared services, the embedded Caddy router (with a local SSL CA), the wildcard DNS resolver, and OS trust-store management, all behind a localhost API guarded by a bearer token.
- **`hull`** is a thin client over that API. When no daemon is running it executes the **same engine code in-process**, so the CLI works fully headless. Run `hull help routing` for exactly how that switch works.
- Each project's **`hull.yaml`** is rendered into a `compose.yaml` (covered by golden tests) and run with `docker compose`. The router discovers each container's published loopback port and proxies `https://<name>.test` to it.

---

## Requirements

| Purpose | Needs |
|---|---|
| **Running Hull** | Docker Engine + the `docker compose` plugin (Docker Desktop, `docker.io`, Podman, OrbStack, or Colima) |
| **Building from source** | Go **1.26+** and git |

---

## Installation

### Windows

**One line** (builds from source; needs Go and git). In PowerShell:

```powershell
irm https://raw.githubusercontent.com/CavenRE/hull/master/get.ps1 | iex
```

It installs `hull` into `%LOCALAPPDATA%\Hull`, adds it to your PATH, and runs `hull setup` for you. Installing needs no admin; setup asks for confirmation or elevation for the certificate and DNS steps. Options (pass them through a scriptblock, see the top of `get.ps1`): `-Branch <tag>`, `-Prefix <dir>`, `-Uninstall`, `-Purge`.

**Prebuilt binary.** Download `hull-windows-x64.exe` from the [latest release](../../releases/latest), rename it to `hull.exe`, then run `hull.exe install` (or double-click it). It copies `hull` into `%LOCALAPPDATA%\Hull`, adds it to your PATH, and registers an Apps & Features entry. No admin required. Then run `hull setup` in a new terminal. The binary has no Authenticode signature yet, so Windows SmartScreen shows an "unknown publisher" prompt: click **More info -> Run anyway**.

**Build from source** (needs Go and git):

```powershell
git clone https://github.com/CavenRE/hull.git
cd hull
powershell -ExecutionPolicy Bypass -File build.ps1   # → bin\hull.exe
```

Then add `bin\` to your `PATH`, or run `bin\hull.exe install`.

### Linux

**Quickest: one line** (clones the newest release and builds the CLI from source; needs Go and git):

```bash
curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh
```

Flags after `--` are passed through: `--service` (run the daemon as a `systemd --user` service), `--prefix DIR`, `-y/--yes`.

**Arch (AUR).**

```bash
yay -S hull            # CLI + daemon
```

(The PKGBUILD lives in [`packaging/aur/`](packaging/aur).)

**From source (any distro).**

```bash
git clone https://github.com/CavenRE/hull.git
cd hull
./install.sh           # builds & installs hull to ~/.local/bin
./install.sh --service # additionally run the daemon as a systemd --user service
```

### macOS

```bash
curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh   # clones & builds
```

…or clone and build explicitly:

```bash
git clone https://github.com/CavenRE/hull.git
cd hull
./install.sh           # builds & installs hull to ~/.local/bin
```

The `install.sh` script checks your dependencies, offers to install any that are missing (via your package manager), builds the binary with version info, adds `~/.local/bin` to your `PATH`, and runs `hull setup`. Other flags: `--prefix DIR`, `--service` (Linux), `--skip-setup`, `--yes` (non-interactive).

---

## First-run setup

`install.sh`, the `get.sh` one-liner, and `get.ps1` run this for you, so an install made with them is ready to go. Run it by hand after `hull install` on Windows, or after a source build without those scripts:

```bash
hull setup     # router (:80/:443), DNS, and local CA; starts Hull and runs it at login
hull doctor    # verify Docker, ports, resolution, certificate, daemon
```

`hull setup` asks where your projects live, the local domain, and Hull's loopback IP. The default, `127.0.0.2`, means Hull never fights another local service for `:80`/`:443`/`:53`. It then trusts the local CA, registers `*.test` with your OS resolver, starts Hull, and registers it to start at login. It prompts for sudo or elevation where needed. Each prompt defaults to your current setting, so re-running setup and pressing Enter changes nothing; a re-run applies config changes and restarts Hull for you.

Flags: `--root`, `--tld`, and `--loopback` answer the prompts; `--skip-trust` and `--skip-dns` skip those steps; `--no-autostart` does not register Hull to start at login.

See [Platform notes](#platform-notes) for Linux specifics (privileged ports, DNS resolvers).

---

## Quick start

```bash
# Scaffold a Laravel app with a dedicated Postgres database, and boot it
hull new shop laravel --db postgres

# → https://shop.test  (trusted HTTPS, ready to go)

hull list                 # see every project and its state
hull logs shop            # tail its container logs
cd ~/Work/Sites/shop      # the project folder, under your first root
hull artisan migrate      # run artisan in the app container
hull down shop            # stop it (data is preserved)
```

A folder that arrives in your projects folder some other way, for example with `git clone`, does not run until you approve it: `hull up` shows what it will run and asks, or run `hull allow`.

---

## CLI reference

Run `hull <command> --help` for full flags on any command, `hull help routing` for the daemon model, and `hull help flags` for the global flags (`--home`, `--no-daemon`, `--json`, `-y/--yes`).

### Projects & lifecycle

| Command | What it does |
|---|---|
| `hull new <name> <template>` | Scaffold a project (`laravel`, `wordpress`, `plain`, `static`, `python`, `node`, `go`). Flags: `--db`, `--db-version`, `--redis`, `--php`, `--version`, `--service eng[@ver]` (repeatable), `--serve`, `-i/--interactive`, `--no-db`, `--no-start`, `--here`. |
| `hull up [name...]` | Start the current project, named ones, `--all`, or pick interactively. |
| `hull down [name...]` | Stop projects (data preserved). `--all` stops every registered project. |
| `hull restart [name]` | Restart a project's containers. |
| `hull reload [name]` | Clear a PHP project's opcode cache in place (a second, against the many a restart costs). Usually automatic, see "Watched reload". |
| `hull allow [name]` | Show what a project will run, and approve it. Hull runs a project only after you approve it, and asks again when its `hull.yaml` (or a cluster's compose files) changed. |
| `hull move <name> --to-wsl` | Move a project into the WSL2 filesystem, where a container reads it at native speed (`--distro`, `--dest`). Windows only. |
| `hull url [name]` | Print a project's URL for scripting. `--direct` gives the container's own `127.0.0.1` address, bypassing the router. |
| `hull rebuild [name]` | Rebuild images and bring the project back up (`--no-cache`). |
| `hull reset [name]` | Wipe the project's data volumes and start fresh. |
| `hull repair [name]` | Recreate a project from a clean slate to fix a wedged or detached state (keeps data). |
| `hull rm <name>` | Destroy a project and its data. |
| `hull logs [name]` | Tail a project's container logs, or a shared instance's with `--service <instance>`. |
| `hull status` | Show running containers and their ports. |
| `hull list` | List registered projects and their state. |
| `hull render [dir]` | Regenerate `compose.yaml` from a project's `hull.yaml` (`-o`, `--stdout`). |
| `hull start` | Start Hull (the daemon) in the background, so your sites are served. |
| `hull stop` | Bring down every project, shared service, and the daemon. |

Scope, at a glance: `up`/`down` act on **projects**; `start`/`stop` act on **Hull itself** (`start` brings the daemon up, `stop` brings everything, daemon included, down). Most parent commands with a list (`config`, `cluster`, `group`, `daemon`, `services`, `autostart`) do the useful thing when run bare (print or list); the full explanation is under `-h`.

### Project folders & settings

| Command | What it does |
|---|---|
| `hull park [dir]` | Park a folder: Hull serves every project inside it at `<name>.<tld>`. With no argument it parks the current folder. |
| `hull unpark [dir]` | Stop scanning a parked folder (the files stay as they are). |
| `hull parked` | List the parked folders. |
| `hull forget [name]` | Stop managing a project imported in place, outside a parked folder (files are kept; `hull rm` is the destructive one). |
| `hull set <project>` | Change `--php`, `--domain`, or `--serve` on a managed project. |
| `hull config get` | Print the current global configuration. |
| `hull config tld` / `roots` / `defaults` | Set the local TLD, manage the parked folders (`roots list` / `add` / `rm`), set default tools/versions (`php`, `editor`, `db-tool`). |
| `hull group list` / `add` / `mv` / `order` / `rm` | Organize projects into virtual groups (stored Hull-side; folders untouched). |

### Shared services & databases

| Command | What it does |
|---|---|
| `hull services add <eng[@ver]>` | Create & start a shared instance (e.g. `postgres@16`). Aliases: `svc`, `service`. |
| `hull services list` / `start` / `stop` / `rm` | Manage shared instances (`rm` destroys all its databases). |
| `hull services alias <name> <inst>` | Name an instance (e.g. `mysql` -> `mysql-8.4`); bare lists, `hull services alias rm <name>` removes one. |
| `hull link <project> <eng[@ver]>` | Link a project to a shared instance (creates its database and its own login, wires the framework env). |
| `hull unlink <project> <key>` | Remove a linked service (e.g. `db`, `redis`) from a project. |
| `hull db export <name>` | Dump a project's database to `<name>.sql` (`-o <file>`; a `.gz` name is compressed). |
| `hull db import <name> <file>` | Load a `.sql` or `.sql.gz` dump into a project's database. It checks that the dump comes from the same kind of database, drops the statements that aim it at another database or owner, and creates a missing linked database first. |

`start` / `stop` / `rm` / `link` accept an alias, or just the engine name when you
run a single version of it (`hull services stop mariadb` finds your sole
`mariadb-*` instance). `hull db` works on a project's own database and on one it
links in a shared instance; a project with more than one database takes
`--service` to say which.

**Accessing your databases.** Your apps connect automatically: Hull wires
`DB_HOST` and friends, with the project's own login. To browse them yourself,
open the console at `https://db.test`, auto-provisioned the first time any
database is attached. Its picker lists each project's database in a shared
instance, across MySQL, MariaDB, and PostgreSQL. Click an entry to fill in the
login form, then type the password. A Laravel project has it as `DB_PASSWORD` in
its `.env`; for the other templates it is in the generated `compose.yaml`
(`WORDPRESS_DB_PASSWORD`, or inside `DATABASE_URL`). To list each project's own
database container too, set
`services: { adminer_project_dbs: true }` in `config.yaml`; it is off by default
because it puts the Adminer container on every project's private network.
Databases made before Hull 0.18 keep their passwordless root until you recreate
them (`hull doctor` names them), and the picker leaves them out. Shared
instances are also published on a stable `127.0.0.1:<port>` for desktop clients
(see `hull services list`). Opt out of auto-provisioning with
`services: { auto_adminer: false }` in `config.yaml`.

### Developer tooling

| Command | What it does |
|---|---|
| `hull artisan <args>` | Run Laravel artisan in the current project's app container. |
| `hull composer <args>` | Run Composer in the current project's app container. |
| `hull npm <args>` | Run npm in an ephemeral Node container (e.g. `hull npm run dev`; `--image` picks the image). |
| `hull node` / `python` / `pip` / `go` `<args>` | Run that tool in the current project's app container. `hull node` and `hull python` with no arguments open a REPL. |
| `hull exec <cmd>` | Run any command inside the current project's app container. |

Run these from inside the project folder, with the project up (`hull up`). All but
`hull npm` take `--service` to run in another compose service, for example a queue
worker.

### Import · export · migrate · clusters

| Command | What it does |
|---|---|
| `hull import [name\|path\|bundle.zip]` | Import a project folder where it lives (with no argument, the current folder), or restore a bundle. Hull detects the framework, PHP version, database, and Redis; `--template`, `--db`, `--php`, and `--redis` override them. |
| `hull export <name>` | Export a project as a portable `<name>-bundle.zip` with fresh DB dumps (`-o`, `--include-env`, `--include-vendor`, `--skip-db`). |
| `hull migrate [name]` | Adopt bash-Hull (v1) projects into v2 (`--all`). |
| `hull cluster add <dir>` | Adopt an existing `docker compose` project as a cluster. |
| `hull cluster create <name>` | Build a new multi-container cluster, one repeatable `--container name=..,template=..\|image=..,port=..,serve` per container. |
| `hull cluster list` / `urls` | List clusters, and every URL a cluster serves. |
| `hull cluster route add` / `rm` / `list` | Assign subdomains to a cluster's services. |
| `hull cluster set <name>` | Set a cluster's `--base-domain` and its `--ingress` mode (`none`, `delegate`, or `hull`). |
| `hull cluster ingress <name>` | Preview or write the ingress-container overlay (delegate mode). |

**What a bundle carries.** The project code (without `.git`, and by default
without `vendor` and `node_modules` at the project root), the `hull.yaml`, a
fresh dump of each running database, and the `.env` with secret-looking values
blanked. Other `.env.*` files, Hull's `*.hull-backup` copies, and symbolic links
are left out. Secrets in other files, such as `wp-config.php`, and the data in
the dumps travel as they are, so keep a bundle private. When you import a
bundle, Hull lists its hooks and asks before it runs them.

### System & networking

| Command | What it does |
|---|---|
| `hull setup` | One-time machine setup: router, DNS, local CA, and Hull at login (see [First-run setup](#first-run-setup)). |
| `hull trust` | Install/remove Hull's local root certificate in the OS trust store (`--uninstall`). |
| `hull doctor` | Diagnose the environment (Docker, ports, DNS, certs, daemon). `--fix` applies the remedies Hull is confident about. |
| `hull daemon run` / `status` / `stop` | Manage the daemon (`hull daemon run` starts it in the foreground). |
| `hull autostart enable` / `disable` | Start Hull automatically at login, or stop doing so (systemd `--user` unit, LaunchAgent, or Run entry). |
| `hull autostart add` / `rm` / (bare) | Choose which projects and shared instances come up when Hull starts. |
| `hull deps` | Show dependency status (Docker + embedded components). |
| `hull completion <shell>` | Generate a shell autocompletion script. |
| `hull install` | Install this hull binary onto the system (copy to a stable location + PATH; `--dir`). |
| `hull update` | Update Hull to the latest signed release (`--check`, `--from-source`, `--branch`, `--reinstall`). |
| `hull uninstall` | Remove Hull from this machine (`--purge-data` also moves `~/.hull` aside). |

---

## Desktop apps (coming soon)

Hull is CLI-first and stays that way. Native desktop apps are in development, one per platform, each a thin client over the daemon's `/v1` API, so they can do nothing the CLI cannot. When they land you will add one without leaving the terminal:

```bash
hull gui install        # fetches and installs the desktop app for your system
```

or download a bundled installer that lays down the CLI and the app together, with a CLI-only option inside it.

| Platform | Repository | Status |
|---|---|---|
| Windows | [hull-gui-windows](https://github.com/CavenRE/hull-gui-windows) | In progress |
| macOS | [hull-gui-macos](https://github.com/CavenRE/hull-gui-macos) | Planned |
| Linux | [hull-gui-linux](https://github.com/CavenRE/hull-gui-linux) | Planned |

The CLI never depends on a desktop app; the apps depend only on Hull's stable API. Nothing here blocks or changes the CLI.

---

## Configuration

**Global**: `~/.hull/config.yaml`

```yaml
tld: test                     # local top-level domain
roots:                        # folders Hull scans for projects (hull park)
  - ~/Work/Sites
router:
  enabled: true
  http_port: 80
  https_port: 443
  loopback: 127.0.0.2         # Hull's own loopback IP (default), 127.0.0.1 to
                              # .8; keeps :80/:443/:53 off 127.0.0.1
dns:
  enabled: true               # set false to use an external resolver for *.tld
  port: 53
services:
  auto_adminer: true          # start Adminer at db.<tld> when a database is attached
  adminer_project_dbs: false  # also reach each project's own database from db.<tld>
defaults:
  php: "8.4"
  editor: code
  db_tool: tableplus
auto_reload: true             # watch PHP sources and refresh saved files in the
                              # opcode cache (default). See "Watched reload" below.
```

**Per project**: `hull.yaml` (the source of truth)

```yaml
schema: 1
name: shop
template: laravel
php: "8.4"
services:
  db:
    engine: postgres
    version: "16"
    mode: dedicated           # or "shared" to use a global instance
```

---

## Platform notes

**Windows: performance.** Docker Desktop runs Linux in a WSL2 VM, so bind-mounting project files that live on the Windows filesystem (for example `C:\Users\you\Work\Sites`) is slow: every PHP request reads hundreds of files across the VM boundary, which is why pages can take seconds to load. Two fixes, biggest first:

1. **Move your projects into the WSL2 Linux filesystem, with `hull move <name> --to-wsl`.** This is not a marginal gain. Measured on a 1,059 file WordPress tree, a container reads it in 4,450 ms from `W:\` and **10 ms** from a WSL distro, which is as fast as the container's own disk (20 ms). A stock WordPress page went from 1,010 ms to **50 ms**.

   Hull still runs on Windows and still serves the same `https://<name>.test` address; only the files move. Your editor opens the new path normally (VS Code and the JetBrains IDEs handle `\\wsl.localhost\...` directly, and it appears in Explorer under Linux). Docker identifies containers and volumes by project name rather than folder, so databases and data volumes come through untouched, and the old folder is renamed aside rather than deleted.

   Two things to know. First, this needs Docker Desktop's WSL integration switched on for the distro (Settings > Resources > WSL Integration); `hull doctor` tells you whether it is, and `hull move` refuses rather than half-working if it is not. Second, files in a distro carry real Unix ownership, unlike a Windows bind mount, so Hull passes the distro user's identity into the container. That is what keeps the site able to write its own uploads and caches while every file stays editable from your editor. Worth checking, too, that whatever backs up your Windows drive still covers the new location.

2. **Exclude the sites folder and Docker's data from your antivirus.** Real-time scanning of every file read compounds the cost, and is the usual explanation for a page that normally takes 7 seconds occasionally taking 16. `hull doctor` prints the exact `Add-MpPreference` commands for your machine, including where Docker keeps its disk images; run them in an elevated PowerShell when Microsoft Defender is your active antivirus. If a third-party antivirus or EDR is active instead, those commands will error, so set the equivalent path exclusions in that product, and on a managed or corporate machine you may need whoever manages the device to do it. This removes the scan overhead and some of the jitter; it does not remove the underlying mount cost, and Hull will not change your security settings for you.

`hull doctor` warns when a project root is on the Windows filesystem, measures what a file operation actually costs there, and `hull new` says so the moment you create a project in one. Hull also enables and tunes PHP OPcache for every PHP container (Laravel, WordPress, and plain sites, plus custom `app` images that set `php_tune: true`) so repeated requests skip recompilation, and new WordPress sites disable page-load wp-cron to speed up the dashboard.

**Watched reload.** When the daemon runs on Windows and a PHP project is on a Windows drive, Hull goes one step further: PHP stops checking the modification time of every cached file on every request, which is the largest remaining per-request cost there (a WordPress page went from a mean of 1483 ms to a steady 985 ms in testing). That is safe because the daemon watches the whole project folder, and when you save, it refreshes only the saved files in the opcode cache. The edit shows on the next refresh, and that page compiles only what changed. Deleting or renaming a PHP file empties the cache instead. When a watched site or the daemon starts, Hull compiles the site's PHP in the background, so the first pages are fast too. The watcher skips `node_modules`, hidden folders such as `.git`, and on WordPress `wp-content/cache`, and it groups a burst of saves into one refresh. A Linux daemon cannot see edits that Windows programs make under `/mnt/c`, so there PHP keeps its file checks. Your project's own `.hull/php.ini` still loads last if you want the check back for one project. To turn the whole thing off, for example if you run your own watcher:

```yaml
auto_reload: false            # in ~/.hull/config.yaml
```

**Linux: privileged ports.** The embedded router binds `:80`/`:443` directly (no container). Grant the capability once during install, or lower the unprivileged-port threshold system-wide:

```bash
sudo setcap 'cap_net_bind_service=+ep' ~/.local/bin/hull
# or, surviving every rebuild:
echo 'net.ipv4.ip_unprivileged_port_start=80' | sudo tee /etc/sysctl.d/10-hull-ports.conf && sudo sysctl --system
```

**Linux: DNS resolver.** `hull setup` auto-detects how your machine resolves DNS and configures `*.<tld>` for you on either backend: a **systemd-resolved** drop-in, or a **NetworkManager + dnsmasq** rule (common on Arch/CachyOS). Both point `*.<tld>` at Hull's loopback IP (default `127.0.0.2`), and `hull uninstall` removes them again. It prompts for `sudo` where needed. (Pass `--skip-dns` if you resolve `*.<tld>` some other way.)

**Linux: file permissions.** On native Docker, Hull automatically remaps PHP containers to your host UID so bind-mounted project files (SQLite databases, `storage/`, caches) stay writable. Docker Desktop on macOS/Windows handles this in its VM.

**Coexisting with other stacks.** Hull binds its own loopback IP (`127.0.0.2` by default) so it never clashes with a service on `127.0.0.1`. If something else already uses `127.0.0.2`, set `router.loopback` to another free `127.0.0.x` (e.g. `127.0.0.3`). On macOS a non-`.1` address is aliased onto `lo0` for you during `hull setup`.

---

## Updating

```bash
hull update            # download and install the latest release, in place
hull update --check    # just see whether a newer version is available
```

`hull update` downloads the prebuilt binary for your platform from the latest GitHub release and replaces `hull` where it already lives, so it needs neither Go nor git. It installs the download only when the release's `SHA256SUMS` file has a valid signature and the binary matches its checksum. With `--from-source`, or when there is no prebuilt binary for your platform, it clones the repo and builds instead, which needs Go and git (`--branch <name>` picks the branch, default `master`). `--reinstall` installs again even when you are up to date. Installed from a package manager? Update it there instead, for example `yay -Syu hull`. Your running daemon keeps serving the old version until you restart it: run `hull stop`, then `hull start`.

Hull also checks for a new release on its own, at most once a day, and offers the update the next time you run a command interactively. Decline once and it will not ask again for that version. Set `HULL_NO_UPDATE_CHECK=1` to disable the check entirely.

---

## Uninstalling

**Windows**: run `hull uninstall` in a terminal. It stops the daemon and removes Hull's CA from the trust store, the `*.test` DNS rule and hosts block, the start-at-login entry, the Apps & Features entry, the PATH entry, the shortcuts, and the program files. Add `--purge-data` to also move `~/.hull` (config, CA, service data) aside to `~/.hull.bak`.

**Linux**: one line from anywhere (works even if you no longer have the source tree):

```bash
curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh -s -- --uninstall
#  add --purge to also clear ~/.hull (config, CA, service data)
```

…or, if Hull is already on your PATH:

```bash
hull uninstall            # stop the daemon; undo trust/DNS; remove binaries, systemd unit, PATH entry
hull uninstall --purge-data   # also move ~/.hull aside to ~/.hull.bak
```

…or use the script from the source tree. It does less: it does not undo the DNS setup, so prefer `hull uninstall`.

```bash
./uninstall.sh            # stop the daemon; undo trust; remove the binary, systemd unit, PATH entry
./uninstall.sh --purge    # also delete ~/.hull (config, CA, service data)
```

Installed from the **AUR**? Remove it the same way: `sudo pacman -R hull` (`hull uninstall` will detect the package install and point you here).

**macOS**: run `hull uninstall` first, then remove the binary with the script from the source tree:

```bash
hull uninstall            # stop the daemon; remove the LaunchAgent; undo trust, DNS, and the loopback alias
./uninstall.sh            # remove the binary and the PATH entry
./uninstall.sh --purge    # also delete ~/.hull (config, CA, service data)
```

Your project files are never touched.

---

## Philosophy & contributing

**Built for me, free for you.** Hull exists to solve my own local-dev workflow, and it's designed to be simple to hack, modify, and extend.

Fork it and mould it to your stack. Pull requests are welcome, but be aware I may not merge (or review) changes that don't fit how I work. If you need Hull to do something specific, forking is your fastest path.

**Enjoying Hull?** If it saves you some time, you can support its development; it's hugely appreciated:

[![Support me on Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cavenre)

_(Friend made me do it)_

---

## License

MIT. See [LICENSE](LICENSE). Use it, modify it, ship it, take it apart. No strings attached.
