<div align="center">

<img src="docs/logo.png" width="96" alt="Hull logo">

# Hull

**A fast, cross-platform local development environment.**
Docker-based dev sites with automatic HTTPS domains, shared databases, and a one-command setup , driven by a CLI and a background daemon.

Runs on **Windows · macOS · Linux** (Arch & Debian/Ubuntu).

</div>

---

## What is Hull?

Hull provisions Docker-based local development environments and serves each project at a trusted `https://<name>.test` address , no port juggling, no manual nginx/Caddy config, no `/etc/hosts` editing.

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

**Installer (recommended).** Download `hull.exe` from the [latest release](../../releases/latest), then run `hull.exe install`. It copies `hull` into `%LOCALAPPDATA%\Hull`, adds it to your PATH, and registers an Apps & Features entry. No admin required. It's unsigned for now, so Windows SmartScreen shows an "unknown publisher" prompt , click **More info → Run anyway**.

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

Enable native networking on the machine (one time):

```bash
hull setup     # enable the embedded router (:80/:443) + DNS, install the local CA
hull trust     # trust Hull's root certificate (may prompt for sudo)
hull doctor    # verify Docker, ports, resolution, certificate, daemon
```

Then start the daemon and you're live:

```bash
hull start   # runs the daemon in the background
```

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
| `hull down [name...]` | Stop environments (data preserved). |
| `hull restart [name]` | Restart a project's containers. |
| `hull rebuild [name]` | Rebuild images and bring the project back up (`--no-cache`). |
| `hull reset [name]` | Wipe the project's data volumes and start fresh. |
| `hull repair [name]` | Recreate a project from a clean slate to fix a wedged or detached state (keeps data). |
| `hull rm <name>` | Destroy an environment and its data. |
| `hull logs [name]` | Tail a project's container logs. |
| `hull status` | Show running containers and their ports. |
| `hull list` | List registered projects and their state. |
| `hull render` | Regenerate `compose.yaml` from a project's `hull.yaml`. |
| `hull start` | Start Hull (the daemon) in the background, so your sites are served. |
| `hull stop` | Bring down every project, shared service, and the daemon. |

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
| `hull link <project> <eng[@ver]>` | Link a project to a shared instance (creates its database, wires the framework env). |
| `hull unlink <project> <key>` | Remove a linked service (e.g. `db`, `redis`) from a project. |

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
| `hull deps` | Show dependency status (Docker + embedded components). |
| `hull completion <shell>` | Generate a shell autocompletion script. |
| `hull install` | Install this hull binary onto the system (copy to a stable location + PATH). |
| `hull update` | Update Hull to the latest, rebuilt from source (`--check`, `--branch`, `--force`). |
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
  loopback: 127.0.0.1         # 127.0.0.1 to .8: bind a different loopback to
                              # coexist with another local proxy
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

**Linux , privileged ports.** The embedded router binds `:80`/`:443` directly (no container). Grant the capability once during install, or lower the unprivileged-port threshold system-wide:

```bash
sudo setcap 'cap_net_bind_service=+ep' ~/.local/bin/hull
# or, surviving every rebuild:
echo 'net.ipv4.ip_unprivileged_port_start=80' | sudo tee /etc/sysctl.d/10-hull-ports.conf && sudo sysctl --system
```

**Linux , DNS resolver.** `hull setup` auto-detects how your machine resolves DNS. On **systemd-resolved** it registers `*.<tld>` for you. On **NetworkManager + dnsmasq** (common on Arch/CachyOS) it detects that systemd-resolved isn't in charge and **leaves DNS to your existing resolver** , it won't enable the embedded resolver or fight for `:53`. Just point `.<tld>` at `127.0.0.1` in your dnsmasq config. (Pass `--skip-dns` to force-skip the step yourself.)

**Linux , file permissions.** On native Docker, Hull automatically remaps PHP containers to your host UID so bind-mounted project files (SQLite databases, `storage/`, caches) stay writable. Docker Desktop on macOS/Windows handles this in its VM.

**Coexisting with other stacks.** If you already run a local proxy on `127.0.0.2:443`, set `router.loopback` to a free `127.0.0.x` so Hull binds its own loopback IP without a port clash.

---

## Updating

```bash
hull update            # rebuild + install the latest, in place
hull update --check    # just see whether a newer version is available
```

Since there are no prebuilt CLI releases yet, `hull update` clones the latest source and rebuilds `hull` where it already lives (it needs Go and git, the same as installing). Installed from a package manager? Update it there instead, for example `yay -Syu hull`. Your running daemon keeps serving the old version until you restart it, so after updating, restart the daemon to pick up the change. Flags: `--check` (report only), `--branch <name>`, `--force`.

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
