<div align="center">

<img src="docs/logo.png" width="96" alt="Hull logo">

# Hull

**A fast, cross-platform local development environment.**
Docker-based dev sites with automatic HTTPS domains, shared databases, and a one-command setup , driven by a CLI and a background daemon.

Runs on **Windows · macOS · Linux** (Arch & Debian/Ubuntu).

</div>

---

## What is Hull?

Hull provisions Docker-based local development environments and serves each project at a trusted `https://<name>.test` address: no port juggling, no manual nginx/Caddy config, no `/etc/hosts` editing.

It scaffolds Laravel, WordPress, and plain-PHP projects in one command, runs shared database instances multiple projects can share, and routes everything through an embedded HTTPS reverse proxy with a locally-trusted certificate authority.

Hull v2 is a ground-up **Go rewrite** of the original bash tool (which lives on the [`legacy`](../../tree/legacy) branch). It is cross-platform and daemon-backed, and the CLI is fully featured on its own.

> **CLI-first:** Hull is the CLI + daemon, and every command works standalone; when the daemon is running the same commands route through it. Native desktop apps are in development, one per platform (see [Desktop apps](#desktop-apps-coming-soon)).

> **Source of truth:** every project is described by a small `hull.yaml`. The `compose.yaml` Hull runs is a generated artifact , never hand-edited.

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

- **One-command scaffolding** , `hull new shop laravel --db postgres` creates the project, wires the framework, and boots it at `https://shop.test`.
- **Automatic HTTPS & DNS** , an embedded Caddy reverse proxy with a local root CA serves every site over trusted TLS; a built-in wildcard resolver answers `*.test` (no `dnsmasq` container required).
- **Shared service instances** , run `postgres-16`, `mariadb-lts`, `redis`, etc. once and link many projects to them; multiple versions live side by side.
- **Central database console** , attach a database to anything and Hull auto-starts Adminer at `https://db.test`, one browser console that reaches every database (MySQL, MariaDB, PostgreSQL), shared instances and per-project alike, with one-click login.
- **Headless or daemon-backed** , the CLI is fully featured on its own. A running daemon adds live routing and background jobs , but is never required.
- **Portable bundles** , `hull export` produces a `hull-bundle.zip` (project + fresh DB dumps) that `hull import` restores on another machine.
- **Adopt what you already have** , import existing projects, wrap multi-container `docker compose` stacks as **clusters**, or migrate projects from bash-Hull (v1).
- **Ephemeral & native tooling** , `hull npm run dev` runs in a throwaway Node container; `hull artisan ...` and `hull exec ...` run straight against your project's app container , no host pollution.

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

- **The daemon** (started with `hull daemon run`) owns everything: the project engine, shared services, the embedded Caddy router (with a local SSL CA), the wildcard DNS resolver, and OS trust-store management , all behind a localhost API guarded by a bearer token.
- **`hull`** is a thin client over that API. When no daemon is running it executes the **same engine code in-process**, so the CLI works fully headless. Run `hull help routing` for exactly how that switch works.
- Each project's **`hull.yaml`** is rendered into a `compose.yaml` (covered by golden tests) and run with `docker compose`. The router discovers each container's published loopback port and proxies `https://<name>.test` to it.

---

## Requirements

| Purpose | Needs |
|---|---|
| **Running Hull** | Docker Engine + the `docker compose` plugin (Docker Desktop, `docker.io`, Podman, OrbStack, or Colima) |
| **Building from source** | Go **1.26+** |

---

## Installation

### Windows

**Installer (recommended).** Download `hull-windows-x64.exe` from the [latest release](../../releases/latest), rename it to `hull.exe`, then run `hull.exe install`. It copies `hull` into `%LOCALAPPDATA%\Hull`, adds it to your PATH, and registers an Apps & Features entry. No admin required. It's unsigned for now, so Windows SmartScreen shows an "unknown publisher" prompt: click **More info -> Run anyway**.

**Build from source** (needs Go and git):

```powershell
git clone https://github.com/CavenRE/hull.git
cd hull
powershell -ExecutionPolicy Bypass -File build.ps1   # → bin\hull.exe
```

Then add `bin\` to your `PATH`, or run `bin\hull.exe install`.

### Linux

**Quickest , one line** (clones and builds the CLI from source; needs Go):

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

The `install.sh` script checks your dependencies, offers to install any that are missing (via your package manager), builds the binaries with version info, and adds `~/.local/bin` to your `PATH`. Other flags: `--prefix DIR`, `--service` (Linux), `--skip-setup`, `--yes` (non-interactive).

---

## First-run setup

On Linux/macOS `install.sh` (and the `get.sh` one-liner) runs this for you and
starts the daemon , so a fresh install is ready to go, nothing to do here. Run
these by hand only if you built from source without `install.sh`, or on Windows:

```bash
hull setup     # embedded router (:80/:443) + DNS + local CA, prompts for sudo where needed
hull doctor    # verify Docker, ports, resolution, certificate, daemon
hull start     # run the daemon in the background
```

`hull setup` binds Hull to its own loopback IP (`127.0.0.2` by default) so it
never fights another local service for `:80`/`:443`/`:53`, installs and trusts
the local CA, and registers `*.test` with your OS resolver. Re-running it applies
config changes and restarts the daemon for you.

See [Platform notes](#platform-notes) for Linux specifics (privileged ports, DNS resolvers).

---

## Quick start

```bash
# Scaffold a Laravel app with a dedicated Postgres database, and boot it
hull new shop laravel --db postgres

# → https://shop.test  (trusted HTTPS, ready to go)

hull list                 # see every project and its state
hull logs shop            # tail its container logs
hull artisan shop migrate # run artisan against the app container
hull down shop            # stop it (data is preserved)
```

---

## CLI reference

Run `hull <command> --help` for full flags on any command, `hull help routing` for the daemon model, and `hull help flags` for the global flags.

### Projects & lifecycle

| Command | What it does |
|---|---|
| `hull new <name> <template>` | Scaffold a project (`laravel`, `wordpress`, `plain`). Flags: `--db`, `--db-version`, `--redis`, `--php`, `--version`, `--service eng[@ver]` (repeatable), `--serve`, `-i/--interactive`, `--no-db`, `--no-start`. |
| `hull up [name...]` | Start the current project, named ones, `--all`, or pick interactively. |
| `hull down [name...]` | Stop projects (data preserved). |
| `hull restart [name]` | Restart a project's containers. |
| `hull rebuild [name]` | Rebuild images and bring the project back up (`--no-cache`). |
| `hull reset [name]` | Wipe the project's data volumes and start fresh. |
| `hull repair [name]` | Recreate a project from a clean slate to fix a wedged or detached state (keeps data). |
| `hull rm <name>` | Destroy a project and its data. |
| `hull logs [name]` | Tail a project's container logs. |
| `hull status` | Show running containers and their ports. |
| `hull list` | List registered projects and their state. |
| `hull render` | Regenerate `compose.yaml` from a project's `hull.yaml`. |
| `hull start` | Start Hull (the daemon) in the background, so your sites are served. |
| `hull stop` | Bring down every project, shared service, and the daemon. |

Scope, at a glance: `up`/`down` act on **projects**; `start`/`stop` act on **Hull itself** (`start` brings the daemon up, `stop` brings everything, daemon included, down). Most parent commands with a list (`config`, `cluster`, `group`, `daemon`, `services`) do the useful thing when run bare (print or list); the full explanation is under `-h`.

### Project settings

| Command | What it does |
|---|---|
| `hull set <project>` | Change `--php`, `--domain`, or `--serve` on a managed project. |
| `hull config get` | Print the current global configuration. |
| `hull config tld` / `roots` / `defaults` | Set the local TLD, manage project root folders, set default tools/versions. |
| `hull group add` / `ls` / `mv` / `order` | Organize projects into virtual groups (stored Hull-side; folders untouched). |

### Shared services

| Command | What it does |
|---|---|
| `hull services add <eng[@ver]>` | Create & start a shared instance (e.g. `postgres@16`). Aliases: `svc`, `service`. |
| `hull services list` / `start` / `stop` / `rm` | Manage shared instances (`rm` destroys all its databases). |
| `hull services alias <name> <inst>` | Name an instance (e.g. `mysql` -> `mysql-8.4`); bare lists, `--rm` removes. |
| `hull link <project> <eng[@ver]>` | Link a project to a shared instance (creates its database, wires the framework env). |
| `hull unlink <project> <key>` | Remove a linked service (e.g. `db`, `redis`) from a project. |

`start` / `stop` / `rm` / `link` accept an alias, or just the engine name when you
run a single version of it (`hull services stop mariadb` finds your sole
`mariadb-*` instance).

**Accessing your databases.** Your apps connect automatically (Hull wires
`DB_HOST` and friends). To browse them yourself, open the console at
`https://db.test`, auto-provisioned the first time any database is attached: it
lists every database, shared instances and per-project alike, across MySQL,
MariaDB, and PostgreSQL, and logs you in with one click (local dev uses trust
auth, no passwords). Shared instances are also published on a stable
`127.0.0.1:<port>` for desktop clients (see `hull services list`). Opt out of
auto-provisioning with `services: { auto_adminer: false }` in `config.yaml`.

### Developer tooling

| Command | What it does |
|---|---|
| `hull artisan <args>` | Run Laravel artisan inside the current project. |
| `hull npm <args>` | Run npm in an ephemeral Node container (e.g. `hull npm run dev`). |
| `hull exec <cmd>` | Run any command inside the current project's app container. |

### Import · export · migrate · clusters

| Command | What it does |
|---|---|
| `hull import <name\|bundle>` | Import an existing project folder or a `hull-bundle.zip`. |
| `hull export <project>` | Export a project as a portable `hull-bundle.zip` (with fresh DB dumps). |
| `hull migrate <name>` | Adopt bash-Hull (v1) projects into v2. |
| `hull cluster add` / `ls` / `urls` / `route` / `ingress` | Adopt a `docker compose` project as a cluster, list adopted ones, assign subdomains to its services, and preview or write the ingress overlay. |

### System & networking

| Command | What it does |
|---|---|
| `hull setup` | Enable the native router + DNS on this machine (`--skip-trust`, `--skip-dns`). |
| `hull trust` | Install/remove Hull's local root certificate in the OS trust store (`--uninstall`). |
| `hull doctor` | Diagnose the environment (Docker, ports, DNS, certs, daemon). |
| `hull daemon run` / `status` / `stop` | Manage the daemon (`hull daemon run` starts it in the foreground). |
| `hull daemon enable` / `disable` | Start Hull automatically at login, or stop doing so (systemd/LaunchAgent/Run entry). |
| `hull autostart add` / `rm` / (bare) | Choose which projects and shared instances come up when Hull starts. |
| `hull deps` | Show dependency status (Docker + embedded components). |
| `hull completion <shell>` | Generate a shell autocompletion script. |
| `hull install` | Install this hull binary onto the system (copy to a stable location + PATH). |
| `hull update` | Update Hull to the latest, rebuilt from source (`--check`, `--branch`, `--reinstall`). |
| `hull uninstall` | Remove Hull from this machine (`--purge-data` also clears `~/.hull`). |

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

**Global** , `~/.hull/config.yaml`:

```yaml
tld: test                     # local top-level domain
roots:                        # folders Hull scans for projects
  - ~/Work/Sites
router:
  enabled: true
  http_port: 80
  https_port: 443
  loopback: 127.0.0.2         # Hull's own loopback IP (default). 127.0.0.1 to
                              # .8 , keeps :80/:443/:53 off 127.0.0.1
dns:
  enabled: true               # set false to use an external resolver for *.tld
  port: 53
defaults:
  php: "8.4"
  editor: code
  db_tool: tableplus
```

**Per project** , `hull.yaml` (the source of truth):

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

**Windows , performance.** Docker Desktop runs Linux in a WSL2 VM, so bind-mounting project files that live on the Windows filesystem (for example `C:\Users\you\Work\Sites`) is slow: every PHP request reads hundreds of files across the VM boundary, which is why pages can take seconds to load. Two fixes, biggest first:

1. **Keep your sites in the WSL2 Linux filesystem.** Run Hull inside your WSL distro with roots under the Linux home, or store projects under `\\wsl$\<distro>\...`. Native-VM files are commonly 10x to 50x faster for this workload.
2. **Exclude the sites folder and Docker's data from Windows Defender.** Real-time scanning of every file read compounds the cost. Add exclusions for your sites directory and Docker Desktop's data (its `ext4.vhdx`).

`hull doctor` warns when a project root is on the Windows filesystem. Hull also enables and tunes PHP OPcache for every PHP container (Laravel, WordPress, and plain sites, plus custom `app` images that set `php_tune: true`) so repeated requests skip recompilation, and new WordPress sites disable page-load wp-cron to speed up the dashboard.

**Linux , privileged ports.** The embedded router binds `:80`/`:443` directly (no container). Grant the capability once during install, or lower the unprivileged-port threshold system-wide:

```bash
sudo setcap 'cap_net_bind_service=+ep' ~/.local/bin/hull
# or, surviving every rebuild:
echo 'net.ipv4.ip_unprivileged_port_start=80' | sudo tee /etc/sysctl.d/10-hull-ports.conf && sudo sysctl --system
```

**Linux , DNS resolver.** `hull setup` auto-detects how your machine resolves DNS and configures `*.<tld>` for you on either backend: a **systemd-resolved** drop-in, or a **NetworkManager + dnsmasq** rule (common on Arch/CachyOS). Both point `*.<tld>` at Hull's loopback IP (default `127.0.0.2`), and `hull uninstall` removes them again. It prompts for `sudo` where needed. (Pass `--skip-dns` if you resolve `*.<tld>` some other way.)

**Linux , file permissions.** On native Docker, Hull automatically remaps PHP containers to your host UID so bind-mounted project files (SQLite databases, `storage/`, caches) stay writable. Docker Desktop on macOS/Windows handles this in its VM.

**Coexisting with other stacks.** Hull binds its own loopback IP (`127.0.0.2` by default) so it never clashes with a service on `127.0.0.1`. If something else already uses `127.0.0.2`, set `router.loopback` to another free `127.0.0.x` (e.g. `127.0.0.3`). On macOS a non-`.1` address is aliased onto `lo0` for you during `hull setup`.

---

## Updating

```bash
hull update            # rebuild + install the latest, in place
hull update --check    # just see whether a newer version is available
```

`hull update` clones the latest source and rebuilds `hull` where it already lives (it needs Go and git, the same as building from source; prebuilt binaries are attached to each release if you would rather download one). Installed from a package manager? Update it there instead, for example `yay -Syu hull`. Your running daemon keeps serving the old version until you restart it, so after updating, restart the daemon to pick up the change. Flags: `--check` (report only), `--branch <name>`, `--reinstall` (rebuild even if up to date).

Hull also checks for a new release on its own, at most once a day, and offers the update the next time you run a command interactively. Decline once and it will not ask again for that version. Set `HULL_NO_UPDATE_CHECK=1` to disable the check entirely.

---

## Uninstalling

**Windows** , run `hull uninstall` in a terminal. It stops the daemon, removes the binaries, the PATH entry, and shortcuts. Add `--purge-data` to also clear `~/.hull` (config, CA, service data).

**Linux** , one line from anywhere (works even if you no longer have the source tree):

```bash
curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh -s -- --uninstall
#  add --purge to also remove ~/.hull (config, CA, service data)
```

…or, if Hull is already on your PATH:

```bash
hull uninstall            # remove binaries, systemd unit, PATH entry; stop the daemon; undo trust/DNS
hull uninstall --purge-data   # also move ~/.hull aside to ~/.hull.bak
```

…or use the script from the source tree (equivalent , both clean up the same things):

```bash
./uninstall.sh            # remove binaries + systemd unit; stop the daemon; undo trust/DNS
./uninstall.sh --purge    # also remove ~/.hull (config, CA, service data)
```

Installed from the **AUR**? Remove it the same way: `sudo pacman -R hull` (`hull uninstall` will detect the package install and point you here).

**macOS**:

```bash
./uninstall.sh            # remove binaries; stop the daemon; undo trust/DNS
./uninstall.sh --purge    # also remove ~/.hull (config, CA, service data)
```

Your project files are never touched.

---

## Philosophy & contributing

**Built for me, free for you.** Hull exists to solve my own local-dev workflow, and it's designed to be simple to hack, modify, and extend.

Fork it and mould it to your stack. Pull requests are welcome, but be aware I may not merge (or review) changes that don't fit how I work , if you need Hull to do something specific, forking is your fastest path.

**Enjoying Hull?** If it saves you some time, you can support its development , it's hugely appreciated: 

[![Support me on Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cavenre)

_(Friend made me do it)_

---

## License

MIT , see [LICENSE](LICENSE). Use it, modify it, ship it, take it apart. No strings attached.
