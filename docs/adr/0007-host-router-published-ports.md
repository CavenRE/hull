# ADR 0007: host-process router with loopback-published upstreams

Date: 2026-06-12. Status: accepted.

## Context

v1 routed with caddy-docker-proxy running *inside* the docker network,
reaching containers by internal IP. Hull v2's router runs embedded in the
hulld host process (ADR 0001), and on Docker Desktop (Windows/macOS) a host
process cannot reach container-internal IPs.

## Decision

Every routed service publishes its upstream port on a loopback ephemeral
host port (compose `127.0.0.1::<port>`). The daemon discovers the assigned
port via `docker compose port` after start and configures the embedded
Caddy route `https://<domain>.<tld>` → `127.0.0.1:<assigned>`. Binding to
127.0.0.1 keeps dev sites off the LAN.

The v1 caddy labels remain on generated services during the transition, so
a running caddy-docker-proxy (v1 stack) keeps routing the same projects —
both routers can serve side by side until cutover.

## Consequences

- Works identically on Linux, macOS, and Windows; no docker network
  membership needed by the router.
- Assigned ports change across restarts — the daemon re-resolves on every
  project start and on running-set changes (events loop).
- One published port per routed service; unrouted services (workers, dbs)
  publish nothing.
