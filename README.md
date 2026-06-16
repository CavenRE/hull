# Hull v2 🚢

The cross-platform rewrite of Hull: a Docker-based local development
environment for Windows, macOS, and Linux (Debian/Arch), with a CLI-first
core and an optional Tauri tray/GUI app.

**Status: working.** The Go daemon + CLI run sites (Laravel/WordPress/plain),
shared service instances, and multi-container clusters, with embedded HTTPS
routing, wildcard `*.test` DNS, and a Tauri GUI. The original bash version of
Hull (v1, Linux/WSL2) lives on [`main`](../../tree/main).

## Architecture

- `hulld` — a single Go daemon that owns everything: project engine,
  shared services (Postgres/MySQL/MariaDB/Redis instances), embedded Caddy
  routing with local SSL, wildcard `*.test` DNS, and trust-store management,
  behind a local socket API.
- `hull` — the CLI, a thin client over that API. Fully featured headless.
- `gui/` — a Tauri v2 tray + GUI app (later phase), another thin client that
  bundles the daemon as a sidecar. Optional by design.
- Per-project `hull.yaml` is the source of truth; compose files are
  generated artifacts.

## Layout

```
cmd/hull/        CLI entrypoint
cmd/hulld/       daemon entrypoint
internal/        core packages (see each package's doc.go for its role)
gui/             Tauri tray + GUI app
```

## Building

```
go build ./...
go test ./...
```

## License

MIT — see [LICENSE](LICENSE).
