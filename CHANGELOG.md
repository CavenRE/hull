# Changelog

All notable changes to Hull are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Hull aims to
follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- First-run setup wizard (Docker check → projects folder → base IP/TLD →
  starter services → apply), shown until `~/.hull/config.yaml` exists.
- Startup settings: launch-at-login (OS login item), close-to-tray, and
  auto-start the daemon on launch , all persisted in `~/.hull/gui.json`.
- `hulld` now writes `~/.hull/hulld.log` (with panic capture) so failures of
  the detached daemon are diagnosable.
- Release workflow (`v*` tags) building installers for Windows, macOS, and
  Linux, with signing/notarization gated on repo secrets.
- Unit tests for `certs`, `dockerx`, `doctor`, and `templates`.

### Changed
- macOS DNS registration now uses the native admin prompt (osascript) instead
  of printing manual `sudo` steps.
- CI runs on `master` (was the deleted `v2` branch) and now also syntax-checks
  the frontend modules.
- Settings: "Clear caches" actually flushes the registry cache and re-detects
  projects; "Reset Hull" shows accurate manual steps; Doctor reports an
  "unavailable" state instead of static mock data.

### Removed
- The non-functional "Edit instance" dialog (no edit endpoint existed; service
  instances are recreated, not edited).

## [0.1.0] , 2026-06

Initial internal build: Go daemon (`hulld`) + CLI (`hull`) + Tauri GUI
(`hull-gui`). Docker-based projects with automatic HTTPS domains, shared
services, framework scaffolding (Laravel/WordPress/plain), cluster adoption,
and a Windows NSIS installer bundling all three binaries.
