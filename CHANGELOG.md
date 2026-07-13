# Changelog

All notable changes to Hull are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Hull follows
[Semantic Versioning](https://semver.org/).

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
