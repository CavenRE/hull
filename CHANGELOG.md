# Changelog

All notable changes to Hull are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Hull follows
[Semantic Versioning](https://semver.org/).

## [0.15.1] - 2026-07-19

First-run fixes. A fresh Windows install used to finish with nothing running and
nothing served; installing now actually finishes the job.

### Fixed
- **Installing on Windows now runs setup.** `get.ps1` and `hull install` copied
  the binary, edited PATH, and then just printed "next steps: run hull setup, run
  hull daemon run". Nobody ran them, so a fresh install had no router, no hosts
  block, and no daemon: every site gave connection refused. Both paths now run
  setup, exactly as `install.sh` has always done on Linux and macOS.
- **`hull setup` leaves Hull running.** It used to end by telling you to start
  the daemon yourself. It now registers Hull to start at login, starts (or
  restarts) the daemon, and verifies something actually answers before reporting
  success. `--no-autostart` skips the login part.
- **Docker gets started instead of erroring.** Every command that needs the
  container engine (up, down, restart, repair, logs, new, import, link, services,
  rebuild, reset, rm, export, cluster create) now starts Docker when it is merely
  closed and waits for it, rather than failing with "the container engine is not
  responding". Windows launches Docker Desktop windowless, macOS uses `open -a
  Docker`, and Linux tries the user-scoped services before the system one so it
  never needs root. A missing Docker install is still a hard error.
- **The daemon starts Docker at boot too**, so items marked with `hull autostart`
  actually come up after a reboot even when Docker is not set to launch at login.
  Previously the daemon only waited, then gave up silently.
- **`hull setup` checks Docker** as an explicit step, so the first `hull up`
  cannot fail on a closed engine.

### Changed
- **`hull daemon enable` is now `hull autostart enable`.** Everything about what
  starts automatically lives under one command: `hull autostart` (status),
  `enable` / `disable` (also `stop`, `off`), and `add` / `rm` for the projects and
  shared instances that come up with Hull. The old `hull daemon enable|disable`
  still work but are hidden.

## [0.15.0] - 2026-07-18

Autostart and shared-instance aliases.

### Added
- **`hull daemon enable` / `hull daemon disable`**: register (or unregister) the
  Hull daemon to start at login, so your sites are served after a reboot without
  running `hull start` by hand. Each platform uses its native, no-elevation
  mechanism: a systemd --user unit with lingering on Linux, a per-user
  LaunchAgent on macOS, and a per-user Run entry on Windows that launches the
  daemon with its console hidden. `hull daemon status` reports the state.
- **`hull autostart add|rm <name>`**: choose which projects and shared instances
  Hull brings up when the daemon starts. A project stores the choice in its own
  `hull.yaml` (`autostart: true`); a shared instance is stored in config. On
  daemon start these are brought up **without** re-running a project's `pre_up`/
  `post_up` hooks: a boot is a resume, not a re-provision (use `hull up` for
  that). Bare `hull autostart` lists what is marked and warns when the daemon
  itself is not set to start at login.
- **Shared-instance aliases**: `hull services alias mysql mysql-8.4` lets a short
  name work everywhere an instance name is accepted (`start`, `stop`, `rm`, and
  `link`). You often need no alias at all: if you run only one version of an
  engine, the engine name resolves to it (`hull services stop mariadb` finds your
  sole mariadb instance). Aliases are stored in config and resolved by the CLI,
  so a running daemon never needs to know about a new one.

### Changed
- **CLI cleanup: help stays at `-h`.** Parent commands with an obvious default
  now do it when run bare instead of printing their help essay: `hull config`
  prints the config, `hull config roots` / `hull group` / `hull cluster` list,
  and `hull daemon` reports status. Standing explanatory footnotes were removed
  from normal output (the `hull services` trust-auth blurb, the `cluster urls`
  ingress note, the `autostart` and `setup` notes) and now live under `-h`.
- **More intuitive and consistent.** Projects are called "projects" everywhere
  (not "environments"); `services rm`, `rebuild`, and `reset` print a success
  confirmation; commands that take no arguments now reject stray ones instead of
  silently ignoring them; `hull npm` works in non-TTY shells (CI); and
  `hull autostart` plus the alias list gained `--json`.
- **Clearer flag names** (old spellings still work, hidden, for one release):
  `hull update --reinstall` (was `--force`), `hull cluster add --compose-root`
  (was `--root`), and `hull import --skip-db` (matches `hull export`, was
  `--skip-dumps`). `hull services alias rm <name>` replaces the `--rm` flag, and
  `hull unlink` now also accepts the engine you linked with (`hull unlink app
  mysql`).

### Fixed
- A daemon config PUT (from the GUI or `hull config`) no longer clobbers
  file-only Services settings (`auto_adminer`, and now aliases/autostart) written
  to `config.yaml` while the daemon was running.

## [0.14.3] - 2026-07-18

Adminer console fixes.

### Fixed
- **The database console no longer spawns a duplicate Adminer.** The
  auto-provisioner asked for version `latest`, which produced a second
  `adminer-latest` instance that fought the one `hull services add adminer`
  creates for the `db.<tld>` route. Both paths now resolve to the same instance
  (`adminer`), so the console stays single and stable.
- **`hull services add adminer` now populates the connection picker itself.**
  Previously the `db.<tld>` dropdown only refreshed when a database was attached
  (add a DB engine, `hull link`, `hull new --db`), so adding Adminer on its own
  left an empty list. Adding Adminer now regenerates the list from every
  reachable database immediately.

## [0.14.2] - 2026-07-17

Certificate and shared-service fixes.

### Fixed
- **Local certificates no longer expire overnight.** Caddy's internal issuer
  defaulted to 12-hour leaf certificates, so a site could show
  `ERR_CERT_DATE_INVALID` (with no HSTS click-through) whenever the daemon was
  not running to renew them. Leaves are now signed directly by the 10-year root
  and valid for a year, so a trusted cert survives restarts and sleeps.
- **`hull services list` no longer reports a running instance as stopped.**
  Docker normalizes compose project names (dropping characters like the dot in
  `mysql-8.4`), so the running-state lookup missed any dotted-version instance.
  Detection now matches Docker's normalization.

### Changed
- **Bare `hull services` now lists the instances** (grouped running first, then
  stopped) instead of printing the full command help. The description moves to
  `hull services --help`, keeping the everyday output uncluttered.

## [0.14.1] - 2026-07-13

Polish for the import and setup flows from 0.14.0.

### Added
- **`hull setup` now configures the machine's core settings** before it
  provisions: it prompts for the projects folder, the local domain, and the
  loopback endpoint (127.0.0.1 to 127.0.0.8), each defaulting to your current
  config so enter keeps it. New `--root`, `--tld`, and `--loopback` flags set
  them without prompting, `--yes` accepts the current values, and off a terminal
  it never prompts so installers do not hang. `install.sh --yes` forwards
  `--yes` to setup.
- **`hull import` without `--template` now offers a type picker** (plain,
  laravel, wordpress), defaulting to what detection found, so you choose what to
  import as. `--yes` and non-terminals fall back to detection.

### Changed
- **`hull import` on a folder that looks like it holds several projects now
  warns and asks to confirm** instead of refusing outright, since that check is
  only a heuristic (a multi-site PHP layout trips it). On confirmation it imports
  the whole folder as one project. `--template` or `--yes` skips the prompt; off
  a terminal it fails closed with an actionable hint.

## [0.14.0] - 2026-07-13

Current-directory project management, the way Herd's `park` works.

### Added
- **`hull park` / `hull unpark` / `hull parked`**: one-word management of the
  folders Hull scans for projects (the friendly front end to `hull config roots`),
  defaulting to the folder you are standing in.
- **In-place import**: `hull import` now works on the current directory (no
  argument) or any path (`hull import .\creative`), importing a project where it
  lives instead of requiring you to move it under a root first. A project outside
  every parked root is registered in a new `projects:` config list so it stays
  listed, findable by name, and resolvable when you `cd` into it.
- **`hull forget`**: stop managing a single imported project (brings it down and
  drops the registration) without deleting files. `hull rm` is still the
  destructive one.
- **`hull new <name> --here`**: scaffold a new project in the current directory
  instead of under your first root.

### Changed
- `hull import` no longer tells you to move your project into a root first; a
  bare name that is not a local folder still falls back to the under-roots lookup.
- The daemon `/v1/config` contract gains a `projects` field; a config PUT that
  omits it leaves registrations unchanged, so a GUI save cannot wipe them.

## [0.13.0] - 2026-07

A Linux/CachyOS hardening pass with a central database console.

### Added
- **Central Adminer console** at `db.<tld>`, auto-provisioned the first time a
  database is attached (opt out with `services.auto_adminer: false`). One page
  lists every database, shared instances and per-project alike, across MySQL,
  MariaDB, and PostgreSQL; picking one opens it directly. The list refreshes on
  link, unlink, new, and rm.
- **Install-and-go**: `install.sh` runs `hull setup` and starts the daemon in
  the right order, so a fresh install needs no manual setup or restart. `hull
  setup` restarts a running daemon so config changes apply immediately, and
  `loginctl enable-linger` is set so the user daemon starts at boot.
- NetworkManager + dnsmasq is a first-class DNS backend alongside
  systemd-resolved; setup configures whichever the machine uses and uninstall
  removes it.

### Changed
- **Default loopback is now `127.0.0.2`** so Hull owns its own address and never
  fights another local service for `:80`, `:443`, or `:53` on `127.0.0.1`. The
  address is honored end to end: router bind, embedded DNS bind and answer, and
  the OS DNS registration. On macOS a non-`.1` address is aliased onto `lo0`
  during setup.
- DNS registration elevates via sudo (prompting) rather than only printing the
  commands, matching the certificate step.

### Fixed
- `hull uninstall` no longer aborts on a single-binary install (it required GUI
  binaries that source installs never ship).
- `ingress: hull` cluster routes no longer return a silent 502; the
  published-port lookup uses the same pinned compose project that `up` used.
- The embedded router grants `CAP_NET_BIND_SERVICE` and probes a real bind
  during setup, so `:80`, `:443`, and `:53` no longer fail silently on Linux.
- `hull rm` removes projects whose containers wrote files owned by another user
  (WordPress and similar) instead of failing with a permission error.
- `hull doctor` tells a docker socket-permission problem apart from a stopped
  engine, and a daemon that is up but cannot bind from one that is down.
- The AUR package builds again (pkgver was pinned to a nonexistent tag).

## [0.9.5] — 2026-06

The "almost 1.0" release: Hull ships its own installers and is solid end to end
on Windows, Linux, and Arch.

### Added
- **Own graphical installer** — `Hull-Setup.exe` (Windows): a frameless,
  Hull-themed WebView2 window, no console, no admin, no NSIS. A toggle installs
  **CLI + desktop app** or **CLI only**. Clean one-click uninstall via Apps &
  Features or `hull uninstall`.
- **Linux & Arch** — `get.sh` one-liner, a WebKitGTK `hull-installer`, an
  `install.sh`/`build.sh` source build, an AUR package (`hull` / `hull-gui`),
  and a `systemd --user` unit for the daemon.
- **First-run setup wizard** in the desktop app (Docker check → projects folder
  → base IP/TLD → starter services → apply).
- **Startup settings** — launch-at-login, close-to-tray, auto-start the daemon.
- **Richer import** — detects and wires extra shared services (mailpit,
  meilisearch, typesense, memcached, minio) from `.env`; adopts clusters by
  seeding routes from the compose file when there's no Caddyfile.
- `hulld` writes `~/.hull/hulld.log` (with panic capture) so the detached
  daemon's failures are diagnosable.

### Changed
- The GUI setup now enables the embedded router, and the hosts block + doctor
  honour the configured loopback (e.g. `127.0.0.2` to coexist with Herd) — so
  sites actually resolve on a custom loopback.
- macOS DNS registration uses the native admin prompt instead of printing sudo.
- Settings: "Clear caches" really flushes + re-detects; "Reset" shows accurate
  steps; Doctor reports an "unavailable" state instead of mock data.

### Removed
- The non-functional "Edit instance" dialog (service instances are recreated,
  not edited).

## [0.1.0] — 2026-06

Initial internal build: Go daemon (`hulld`) + CLI (`hull`) + Tauri GUI
(`hull-gui`). Docker-based projects with automatic HTTPS domains, shared
services, framework scaffolding (Laravel/WordPress/plain), and cluster adoption.
