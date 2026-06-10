# ADR 0003: hull.yaml manifest as source of truth

Date: 2026-06-10. Status: accepted.

## Context

In v1 the generated compose.yaml is the only record of a project's shape;
after yq merges run, Hull can no longer reason about the project (v1's `add`
command greps image names to guess the framework). This blocked features like
services linking, export bundles, and a GUI that displays project structure.

## Decision

Every project carries a versioned `hull.yaml` manifest (sites and
multi-container apps are the same primitive). Compose files are generated
artifacts, rendered from the manifest, and never edited in place. The
daemon's state store is only a rebuildable index over manifests found in
registered roots; files on disk are the recoverable truth.

## Consequences

- All tooling (CLI, GUI, import, export) reads and writes the manifest.
- Schema is versioned (`schema: 1`) with a migration hook from day one.
- v1 projects are adopted by generating a manifest from their existing
  compose.yaml (`hull migrate-v1`, Phase 9).
