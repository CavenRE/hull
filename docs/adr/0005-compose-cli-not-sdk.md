# ADR 0005: drive `docker compose` via the CLI through Phase 3

Date: 2026-06-11. Status: accepted (revisit in Phase 3/4).

## Context

The official Docker Engine SDK for Go covers container operations but not
compose: compose v2 is a CLI plugin whose Go module is large, fast-moving,
and not designed for embedding. Hull's project lifecycle is compose-shaped.

## Decision

Through Phases 2–3, internal/dockerx shells out to `docker` / `docker
compose` with stdio attached — the binaries every supported engine setup
already provides (Docker Desktop, docker.io + compose plugin, Podman's
docker compatibility, OrbStack, Colima). The Docker Engine SDK is adopted
in the daemon when event streaming requires it (Phase 3 `/v1/events`),
alongside — not replacing — compose CLI invocation.

## Consequences

- Engine-agnostic for free: anything providing a docker-compatible CLI works.
- Live output streams to the user's terminal exactly as v1 did.
- A `docker` binary in PATH is a hard requirement, checked by EngineCheck
  with a human error message (and later by `hull doctor`).
