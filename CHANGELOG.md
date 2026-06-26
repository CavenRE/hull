# Changelog

All notable changes to Hull are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Hull follows
[Semantic Versioning](https://semver.org/).

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
