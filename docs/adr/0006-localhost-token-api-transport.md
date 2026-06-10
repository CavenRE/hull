# ADR 0006: local API over 127.0.0.1 + token file; CLI falls back in-process

Date: 2026-06-11. Status: accepted (transport revisit allowed pre-GUI).

## Context

ADR 0002 requires a local API consumed by CLI and GUI. Candidate transports:
unix domain sockets (named pipes on Windows) or loopback TCP. Windows AF_UNIX
support exists in modern Go but has sharper edges (cleanup of stale socket
files, older-Windows behavior), and named pipes would mean a per-OS code
path on day one.

## Decision

1. **Transport:** HTTP on `127.0.0.1` with an OS-assigned ephemeral port.
   A running daemon writes `~/.hull/daemon.json` (mode 0600) containing
   `{port, token, pid}`; every request must carry `Authorization: Bearer
   <token>`. The token is 256-bit random, regenerated each daemon start.
   Identical behavior on all three OSes; debuggable with curl.
2. **CLI behavior without a daemon:** the CLI runs the engine in-process.
   It prefers the daemon when one responds (single source of truth for
   long-lived state), but never requires it — preserving the headless
   guarantee. Auto-starting the daemon from the CLI is deferred until the
   daemon owns state that in-process execution cannot replicate (embedded
   router/DNS, Phase 4).

## Consequences

- File permissions are the security boundary, same as ssh keys — adequate
  for a single-user dev machine; revisit if multi-user machines matter.
- A stale daemon.json (crash) is harmless: Connect() probes /v1/status
  before trusting it, and a new daemon overwrites the file.
- Upgrade path to unix sockets stays open behind Connect()/Serve().
