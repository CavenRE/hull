# ADR 0004: v2 developed on an orphan branch in the existing repo

Date: 2026-06-10. Status: accepted.

## Context

v2 is a ground-up rewrite in a different language. It could live in a fresh
repository, a subfolder, or a branch of the existing repo.

## Decision

v2 is developed on the orphan branch `v2` of the existing `CavenRE/hull`
repository: a clean Go tree with no bash history in its file set, while
`main` remains the working bash v1 throughout the rewrite. CI runs only on
the `v2` branch. When v2 ships (end of Phase 9), `v2` is merged/promoted to
`main` and v1 is tagged `v1-final`.

## Consequences

- One repo, one issue tracker, one star count; v1 stays installable from
  `main` the whole time (v1's installer pulls `main` explicitly).
- Branch switching swaps the entire working tree between bash and Go —
  expected and harmless; local untracked notes carry across.
