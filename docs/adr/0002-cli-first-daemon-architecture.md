# ADR 0002: CLI-first daemon architecture

Date: 2026-06-10. Status: accepted.

## Context

Hull must work headless ("could even hide the GUI and tray icon altogether")
and the GUI must never be required for any operation.

## Decision

All business logic lives in `hulld`, exposed over a local API (HTTP over a
unix socket on Linux/macOS, a named pipe on Windows). The CLI and the Tauri
GUI are both thin clients of the same API. Long-running operations (scaffold,
import, export) are jobs with streamed progress so both clients render the
same feed.

## Consequences

- GUI/CLI feature parity is guaranteed by construction.
- The daemon is the only component needing privileged setup (ports, DNS,
  certs) — performed once during `hull setup`.
- The Tauri app may start the daemon but never owns it: quitting the GUI
  changes nothing for the CLI.
