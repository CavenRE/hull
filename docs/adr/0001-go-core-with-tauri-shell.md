# ADR 0001: Go core with a Tauri GUI shell

Date: 2026-06-10. Status: accepted.

## Context

Hull v1 is Bash and Linux-only. v2 must run natively on Windows, macOS, and
Linux (Deb/Arch), ship a tray + GUI app built once for all three, and remain
fully usable from the CLI with the GUI absent.

## Decision

The core (daemon `hulld` + CLI `hull`) is written in Go. The GUI is a Tauri v2
app that bundles the daemon as a sidecar binary; its Rust layer is a thin
shell (tray, windows, sidecar lifecycle) with no business logic.

## Rationale

Go has the strongest ecosystem for exactly this domain: the official Docker
Engine SDK, mkcert's cross-platform trust-store code, mature DNS server
libraries (miekg/dns), and Caddy itself is Go — the router can be embedded in
the daemon rather than orchestrated as a container. Single static binaries
cross-compile to every target. Tauri provides the best packaged
installer/updater/tray story for a webview GUI.

## Alternatives considered

- All Rust + Tauri: one language, but no mkcert equivalent, no embeddable
  Caddy, and a less mature Docker client.
- All Go + Wails: one language and the same libraries, but weaker
  installer/updater/tray polish than Tauri.
