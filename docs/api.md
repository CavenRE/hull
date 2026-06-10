# Hull local API (v1)

The contract between `hulld` and its clients (CLI, Tauri GUI). Transport per
ADR 0006: HTTP on `127.0.0.1:<port>` with `Authorization: Bearer <token>`,
both read from `~/.hull/daemon.json` (written by a running daemon, 0600).

Errors are `{"error": "..."}` with a 4xx/5xx status. All bodies are JSON.
Long operations are **jobs**: started with a POST, observed by polling or
SSE. Server types live in `internal/api` and `internal/jobs` — Go clients
should import them rather than redeclaring.

## Endpoints

### `GET /v1/status`
Daemon liveness + identity.
```json
{"version": "dev (none)", "tld": "test", "roots": ["/home/u/Work/Sites"], "hull_home": "/home/u/.hull"}
```

### `GET /v1/projects`
All registered projects with live running state.
```json
[{"name": "myapp", "dir": "/home/u/Work/Sites/myapp", "kind": "laravel",
  "url": "https://myapp.test", "running": true}]
```
`kind` is the template name, `"app"`, `"legacy"` (v1 project awaiting
migration), or `"invalid"` (manifest fails to parse; see `error`).

### `POST /v1/projects` → 202
Create a project (job). Body:
```json
{"name": "myapp", "template": "laravel", "db": "postgres", "redis": true,
 "php": "8.3", "version": "", "skip_start": false}
```
Response: `{"job": {"id": "job-1", "kind": "create:myapp", "status": "running", ...}}`

### `POST /v1/projects/{name}/start|stop|restart` → 204
Synchronous lifecycle. `start` re-renders compose.yaml from the manifest
before composing (manifest is truth).

### `GET /v1/jobs` / `GET /v1/jobs/{id}`
Job snapshots: `{"id", "kind", "status": "running|done|failed", "error", "lines": [...], "created"}`.

### `GET /v1/jobs/{id}/stream` (SSE)
Each log line as a `data:` event; terminates with `event: done` carrying the
final job snapshot.

### `GET /v1/events` (SSE)
Pushed when the set of running compose projects changes (plus one initial
snapshot): `data: {"type": "projects", "running": ["myapp", "blog"]}`.
This is the GUI's live-dashboard feed.

### `POST /v1/shutdown` → 204
Graceful daemon exit (removes daemon.json).

## Planned (phase-gated)

- `GET /v1/doctor` — diagnostic report (lands with `hull doctor`).
- `GET/POST /v1/services`, `/v1/services/{id}/start|stop|link|unlink` —
  shared services (Phase 5; CLI-first, API surface added when the GUI
  needs it).
- `POST /v1/imports`, `POST /v1/projects/{name}/export` — bundle jobs
  (Phase 6; same job mechanics as create).
- `GET /v1/projects/{name}/logs` (SSE) — container log streaming for the
  GUI log viewer.
