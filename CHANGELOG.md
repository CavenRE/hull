# Changelog

All notable changes to Hull are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Hull follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

Clean wins from the external v0.16 audit (P-03, P-05, P-06, P-09).

### Added
- **`hull doctor` warns about duplicate project names.** When two directories
  declare the same manifest name, Hull silently resolves to one and ignores the
  rest, which is a real debugging trap. Doctor now flags the collision and names
  every directory involved.

### Changed
- **The app waits for its database to be ready before it starts.** Dedicated
  database services now render a healthcheck (`pg_isready` / `mysqladmin ping` /
  the MariaDB `healthcheck.sh`), and a site's app `depends_on` them with
  `condition: service_healthy`. A cold `hull up` no longer races an unready
  database, so the Laravel migrate hook no longer needs to swallow failures: a
  genuinely broken migration now surfaces instead of coming up with an empty
  schema.
- **`hull up` readiness stops reporting false failures.** The per-request probe
  timeout was 3s, so a slow first request over a Windows bind mount aborted every
  probe and the site was wrongly reported as unresponsive. It is now 60s (with a
  240s overall budget), and the timeout message reads "still warming up" rather
  than implying the app is broken.
- **The generated `compose.yaml` is gitignored.** Hull appends `/compose.yaml`
  to a site/app project's `.gitignore` when it writes artifacts (idempotent, and
  it never clobbers a hand-written `.gitignore` or touches a cluster's own
  compose file), so a machine-specific generated artifact stops landing in git.

## [0.15.8] - 2026-08-26

### Fixed
- **`hull start` no longer wedges after a crash plus PID reuse.** The
  single-daemon lock stored only a PID, so when a daemon crashed and the OS
  recycled its PID for an unrelated process (on Windows a dead hulld's PID was
  reused by a ShellHost), `hull start` saw that PID alive and refused with "a
  previous Hull daemon is still running but not responding." Hull now checks that
  the PID actually belongs to a Hull process before refusing; a recycled PID is
  recognized as a stale lock and reclaimed. (Manual recovery on older builds:
  delete `~/.hull/hulld.lock`, then start again.)

### Changed
- **`hull doctor` performance warning is smarter and more actionable.** It now
  also fires when Hull runs inside WSL against a project on a Windows drive
  (`/mnt/<drive>/...`), not only a drive-letter path, and it spells out the real
  fix (move sites onto the WSL2 ext4 filesystem) and a stopgap (set
  `opcache.validate_timestamps=0` in `~/.hull/system/php/opcache.ini`, one file
  shared by every site, to stop the per-request re-stat spikes over the 9p mount).

## [0.15.7] - 2026-08-26

### Changed
- **PHP OPcache tuning now applies to every PHP container, WordPress included.**
  Previously only serversideup sites (Laravel, plain PHP) got OPcache tuning,
  via image env vars, and WordPress and custom images got nothing. Hull now
  mounts one shared `opcache.ini` into every PHP container's conf.d (written to
  `~/.hull/system/php/opcache.ini`, editable, never overwritten), so WordPress,
  Laravel, and plain sites all get the same large-cache, low-revalidation
  settings that keep repeated requests off the slow bind mount. A custom `app`
  container built on a raw image opts in with `php_tune: true`.

### Added
- **WordPress local-dev defaults.** New WordPress sites set
  `WP_ENVIRONMENT_TYPE=local` and disable page-load wp-cron (`DISABLE_WP_CRON`),
  so the dashboard stops firing blocking update-check and news-widget requests
  on every load, the usual cause of a slow first login. Override either in your
  project's `hull.yaml` env if you need them.

## [0.15.6] - 2026-08-26

### Added
- **Automated releases.** A GitHub Actions workflow now builds all four
  platform binaries (Windows x64, Linux x64, macOS Intel, macOS Apple silicon)
  and publishes them to the GitHub release on every `v*` tag, so cutting a
  release is just pushing a tag. Version and commit are stamped from the tag.
- **Continuous integration.** A CI workflow builds, vets, and tests the code on
  every push to `master` and every pull request, so regressions surface before
  a release instead of after.

## [0.15.5] - 2026-08-26

### Fixed
- **`hull update` no longer needs Go or git where a prebuilt binary exists.**
  Now that releases ship binaries, `hull update` downloads the prebuilt binary
  for your platform from the latest release and swaps it in place, so a machine
  without a Go toolchain (the common Windows case) can update itself. It falls
  back to a source build only when there is no prebuilt binary for the platform,
  or when you pass the new `--from-source`. `hull update --check` also works
  without Go or git now. (Existing v0.15.4 installs without Go need to download
  the v0.15.5 binary once by hand; every update after that is toolchain-free.)

## [0.15.4] - 2026-08-26

Windows performance and an honest `up`. Two long-standing Windows papercuts:
`hull up` reported success before the site could actually serve, and PHP pages
loaded slowly because of how OPcache was tuned over the Docker Desktop bind
mount.

### Fixed
- **`hull up` waits for the site to actually respond.** It used to return as
  soon as `docker compose up -d` had started the container, which on a first
  boot (WordPress copying core, Laravel migrating) is a minute or two before the
  page loads. For a served site behind the daemon, Hull now polls the URL and
  shows an elapsed-time status line, then confirms the real URL once it answers
  (or warns, pointing at `hull logs`, if it takes too long). No more "up" that
  isn't.
- **Faster PHP page loads on Windows.** The serversideup OPcache revalidation
  checked every file on every request, which is expensive over a Windows/WSL2
  bind mount. It now revalidates at most every 2 seconds and caches a larger
  file set (raised the accelerated-files and memory limits), so repeated
  requests skip the recompile-and-stat storm. WordPress already ships this via
  its own image.

### Added
- **`hull doctor` flags slow Windows setups.** When a project root lives on the
  Windows filesystem, doctor warns that Docker bind-mount I/O there is slow and
  points at the fixes: keep sites in the WSL2 Linux filesystem, and exclude the
  sites folder and Docker's data from Windows Defender. A matching "Windows,
  performance" note was added to the README.

### Changed
- Corrected stale docs. ADR 0002 (the API transport is loopback TCP, not a unix
  socket or named pipe; superseded by ADR 0006), ADR 0004 (v2 shipped on
  `master`, not by promoting `main`), and ADR 0005 (the Docker Engine SDK was
  never adopted; `/v1/events` polls `docker ps`). Also corrected the stale "no
  prebuilt CLI releases yet" note in the README, `get.ps1`, and `hull update`
  help: GitHub releases do ship prebuilt binaries, even though the install
  scripts still build from source.

## [0.15.3] - 2026-07-19

Docker handling, done properly. v0.15.1 taught Hull to start a closed Docker but
only upgraded the places that already checked, so plenty of paths still dumped a
raw transport error or, worse, quietly reported the wrong thing.

### Fixed
- **No command leaks a raw Docker error any more.** `hull status` used to print
  `failed to connect to the docker API at npipe:////./pipe/... The system cannot
  find the file specified.` It now says the engine is not running and what to do
  about it. Detection moved into one classifier in the Docker layer, plus a
  pre-flight probe in the two primitives that hand their output straight to your
  terminal, so a transport dump is now structurally unreachable rather than
  patched away command by command.
- **Every command declares what it needs from the engine**, enforced in one
  place before the daemon/in-process fork. This closes the real hole: a dozen
  commands (restart, repair, logs, rebuild, reset, rm, services, link, cluster
  create, import) had their guard inside the in-process branch, so with a daemon
  running (the normal case) they reached Docker completely unguarded. A test
  fails the build if a new command forgets to declare.
- **The daemon refuses container work when the engine is down**, returning a
  clean 503 instead of relaying a compose error, so the desktop app gets a real
  message too. Writes that only touch YAML (project fields, config, groups,
  cluster routes) keep working with Docker closed, and `shutdown` is exempt so a
  broken Docker can never leave you unable to stop Hull.
- **`hull start` stops claiming everything is fine.** It reported "Hull is
  running." while Docker was dead. It now says so, and the daemon probes the
  engine at boot instead of only when autostart items exist.
- **The listers no longer give a confident wrong answer.** `hull list`,
  `hull services`, and `hull cluster list` swallowed the failure and reported
  every project and instance as *stopped*. They now show state as **unknown**
  with a one-line note, and still list everything, since the names, paths, and
  URLs come from disk and stay useful.
- **`hull doctor` and `hull deps` keep working with the engine down**, which is
  precisely when you need them. A guard there would have aborted the diagnostic
  before it could report the failure you ran it to find.
- **No 2-minute stalls in CI.** Hull never launches Docker without an
  interactive session, and honours `CI=true` and `HULL_NO_ENGINE_START=1`,
  failing in under a second instead of trying to boot Docker Desktop on every
  command. The engine probe is also bounded and cached, so a wedged socket can
  no longer hang a command forever.
- The reconcile loop logs when Docker goes away and comes back instead of
  silently retrying dozens of times a minute, and `docker compose config
  --volumes` now carries the project's own `-p`/`-f`/`--profile` flags, so the
  reset preview on an adopted cluster reads the right project.

## [0.15.2] - 2026-07-19

### Added
- **Hull tells you when there is a new version.** It asks GitHub for the newest
  release at most once a day, caches the answer, and on your next interactive
  command offers the update:

      Hull v0.15.3 is available (you are on v0.15.2).
      Update now? [y/N]

  Answering yes runs `hull update` and stops so you can re-run your command
  against the new binary; answering no remembers that version and does not ask
  again. It stays quiet off a terminal, under `--yes` or `--json`, on dev builds,
  and for commands where it would recurse or corrupt output (`update`,
  `completion`, `daemon run`, `install`, `uninstall`). Set
  `HULL_NO_UPDATE_CHECK=1` to turn it off entirely. Every failure path is silent,
  so a flaky network never blocks the command you actually ran.

## [0.15.1] - 2026-07-19

First-run fixes. A fresh Windows install used to finish with nothing running and
nothing served; installing now actually finishes the job.

### Fixed
- **Installing on Windows now runs setup.** `get.ps1` and `hull install` copied
  the binary, edited PATH, and then just printed "next steps: run hull setup, run
  hull daemon run". Nobody ran them, so a fresh install had no router, no hosts
  block, and no daemon: every site gave connection refused. Both paths now run
  setup, exactly as `install.sh` has always done on Linux and macOS.
- **`hull setup` leaves Hull running.** It used to end by telling you to start
  the daemon yourself. It now registers Hull to start at login, starts (or
  restarts) the daemon, and verifies something actually answers before reporting
  success. `--no-autostart` skips the login part.
- **Docker gets started instead of erroring.** Every command that needs the
  container engine (up, down, restart, repair, logs, new, import, link, services,
  rebuild, reset, rm, export, cluster create) now starts Docker when it is merely
  closed and waits for it, rather than failing with "the container engine is not
  responding". Windows launches Docker Desktop windowless, macOS uses `open -a
  Docker`, and Linux tries the user-scoped services before the system one so it
  never needs root. A missing Docker install is still a hard error.
- **The daemon starts Docker at boot too**, so items marked with `hull autostart`
  actually come up after a reboot even when Docker is not set to launch at login.
  Previously the daemon only waited, then gave up silently.
- **`hull setup` checks Docker** as an explicit step, so the first `hull up`
  cannot fail on a closed engine.

### Changed
- **`hull daemon enable` is now `hull autostart enable`.** Everything about what
  starts automatically lives under one command: `hull autostart` (status),
  `enable` / `disable` (also `stop`, `off`), and `add` / `rm` for the projects and
  shared instances that come up with Hull. The old `hull daemon enable|disable`
  still work but are hidden.

## [0.15.0] - 2026-07-18

Autostart and shared-instance aliases.

### Added
- **`hull daemon enable` / `hull daemon disable`**: register (or unregister) the
  Hull daemon to start at login, so your sites are served after a reboot without
  running `hull start` by hand. Each platform uses its native, no-elevation
  mechanism: a systemd --user unit with lingering on Linux, a per-user
  LaunchAgent on macOS, and a per-user Run entry on Windows that launches the
  daemon with its console hidden. `hull daemon status` reports the state.
- **`hull autostart add|rm <name>`**: choose which projects and shared instances
  Hull brings up when the daemon starts. A project stores the choice in its own
  `hull.yaml` (`autostart: true`); a shared instance is stored in config. On
  daemon start these are brought up **without** re-running a project's `pre_up`/
  `post_up` hooks: a boot is a resume, not a re-provision (use `hull up` for
  that). Bare `hull autostart` lists what is marked and warns when the daemon
  itself is not set to start at login.
- **Shared-instance aliases**: `hull services alias mysql mysql-8.4` lets a short
  name work everywhere an instance name is accepted (`start`, `stop`, `rm`, and
  `link`). You often need no alias at all: if you run only one version of an
  engine, the engine name resolves to it (`hull services stop mariadb` finds your
  sole mariadb instance). Aliases are stored in config and resolved by the CLI,
  so a running daemon never needs to know about a new one.

### Changed
- **CLI cleanup: help stays at `-h`.** Parent commands with an obvious default
  now do it when run bare instead of printing their help essay: `hull config`
  prints the config, `hull config roots` / `hull group` / `hull cluster` list,
  and `hull daemon` reports status. Standing explanatory footnotes were removed
  from normal output (the `hull services` trust-auth blurb, the `cluster urls`
  ingress note, the `autostart` and `setup` notes) and now live under `-h`.
- **More intuitive and consistent.** Projects are called "projects" everywhere
  (not "environments"); `services rm`, `rebuild`, and `reset` print a success
  confirmation; commands that take no arguments now reject stray ones instead of
  silently ignoring them; `hull npm` works in non-TTY shells (CI); and
  `hull autostart` plus the alias list gained `--json`.
- **Clearer flag names** (old spellings still work, hidden, for one release):
  `hull update --reinstall` (was `--force`), `hull cluster add --compose-root`
  (was `--root`), and `hull import --skip-db` (matches `hull export`, was
  `--skip-dumps`). `hull services alias rm <name>` replaces the `--rm` flag, and
  `hull unlink` now also accepts the engine you linked with (`hull unlink app
  mysql`).

### Fixed
- A daemon config PUT (from the GUI or `hull config`) no longer clobbers
  file-only Services settings (`auto_adminer`, and now aliases/autostart) written
  to `config.yaml` while the daemon was running.

## [0.14.3] - 2026-07-18

Adminer console fixes.

### Fixed
- **The database console no longer spawns a duplicate Adminer.** The
  auto-provisioner asked for version `latest`, which produced a second
  `adminer-latest` instance that fought the one `hull services add adminer`
  creates for the `db.<tld>` route. Both paths now resolve to the same instance
  (`adminer`), so the console stays single and stable.
- **`hull services add adminer` now populates the connection picker itself.**
  Previously the `db.<tld>` dropdown only refreshed when a database was attached
  (add a DB engine, `hull link`, `hull new --db`), so adding Adminer on its own
  left an empty list. Adding Adminer now regenerates the list from every
  reachable database immediately.

## [0.14.2] - 2026-07-17

Certificate and shared-service fixes.

### Fixed
- **Local certificates no longer expire overnight.** Caddy's internal issuer
  defaulted to 12-hour leaf certificates, so a site could show
  `ERR_CERT_DATE_INVALID` (with no HSTS click-through) whenever the daemon was
  not running to renew them. Leaves are now signed directly by the 10-year root
  and valid for a year, so a trusted cert survives restarts and sleeps.
- **`hull services list` no longer reports a running instance as stopped.**
  Docker normalizes compose project names (dropping characters like the dot in
  `mysql-8.4`), so the running-state lookup missed any dotted-version instance.
  Detection now matches Docker's normalization.

### Changed
- **Bare `hull services` now lists the instances** (grouped running first, then
  stopped) instead of printing the full command help. The description moves to
  `hull services --help`, keeping the everyday output uncluttered.

## [0.14.1] - 2026-07-13

Polish for the import and setup flows from 0.14.0.

### Added
- **`hull setup` now configures the machine's core settings** before it
  provisions: it prompts for the projects folder, the local domain, and the
  loopback endpoint (127.0.0.1 to 127.0.0.8), each defaulting to your current
  config so enter keeps it. New `--root`, `--tld`, and `--loopback` flags set
  them without prompting, `--yes` accepts the current values, and off a terminal
  it never prompts so installers do not hang. `install.sh --yes` forwards
  `--yes` to setup.
- **`hull import` without `--template` now offers a type picker** (plain,
  laravel, wordpress), defaulting to what detection found, so you choose what to
  import as. `--yes` and non-terminals fall back to detection.

### Changed
- **`hull import` on a folder that looks like it holds several projects now
  warns and asks to confirm** instead of refusing outright, since that check is
  only a heuristic (a multi-site PHP layout trips it). On confirmation it imports
  the whole folder as one project. `--template` or `--yes` skips the prompt; off
  a terminal it fails closed with an actionable hint.

## [0.14.0] - 2026-07-13

Current-directory project management, the way Herd's `park` works.

### Added
- **`hull park` / `hull unpark` / `hull parked`**: one-word management of the
  folders Hull scans for projects (the friendly front end to `hull config roots`),
  defaulting to the folder you are standing in.
- **In-place import**: `hull import` now works on the current directory (no
  argument) or any path (`hull import .\creative`), importing a project where it
  lives instead of requiring you to move it under a root first. A project outside
  every parked root is registered in a new `projects:` config list so it stays
  listed, findable by name, and resolvable when you `cd` into it.
- **`hull forget`**: stop managing a single imported project (brings it down and
  drops the registration) without deleting files. `hull rm` is still the
  destructive one.
- **`hull new <name> --here`**: scaffold a new project in the current directory
  instead of under your first root.

### Changed
- `hull import` no longer tells you to move your project into a root first; a
  bare name that is not a local folder still falls back to the under-roots lookup.
- The daemon `/v1/config` contract gains a `projects` field; a config PUT that
  omits it leaves registrations unchanged, so a GUI save cannot wipe them.

## [0.13.0] - 2026-07

A Linux/CachyOS hardening pass with a central database console.

### Added
- **Central Adminer console** at `db.<tld>`, auto-provisioned the first time a
  database is attached (opt out with `services.auto_adminer: false`). One page
  lists every database, shared instances and per-project alike, across MySQL,
  MariaDB, and PostgreSQL; picking one opens it directly. The list refreshes on
  link, unlink, new, and rm.
- **Install-and-go**: `install.sh` runs `hull setup` and starts the daemon in
  the right order, so a fresh install needs no manual setup or restart. `hull
  setup` restarts a running daemon so config changes apply immediately, and
  `loginctl enable-linger` is set so the user daemon starts at boot.
- NetworkManager + dnsmasq is a first-class DNS backend alongside
  systemd-resolved; setup configures whichever the machine uses and uninstall
  removes it.

### Changed
- **Default loopback is now `127.0.0.2`** so Hull owns its own address and never
  fights another local service for `:80`, `:443`, or `:53` on `127.0.0.1`. The
  address is honored end to end: router bind, embedded DNS bind and answer, and
  the OS DNS registration. On macOS a non-`.1` address is aliased onto `lo0`
  during setup.
- DNS registration elevates via sudo (prompting) rather than only printing the
  commands, matching the certificate step.

### Fixed
- `hull uninstall` no longer aborts on a single-binary install (it required GUI
  binaries that source installs never ship).
- `ingress: hull` cluster routes no longer return a silent 502; the
  published-port lookup uses the same pinned compose project that `up` used.
- The embedded router grants `CAP_NET_BIND_SERVICE` and probes a real bind
  during setup, so `:80`, `:443`, and `:53` no longer fail silently on Linux.
- `hull rm` removes projects whose containers wrote files owned by another user
  (WordPress and similar) instead of failing with a permission error.
- `hull doctor` tells a docker socket-permission problem apart from a stopped
  engine, and a daemon that is up but cannot bind from one that is down.
- The AUR package builds again (pkgver was pinned to a nonexistent tag).

## [0.9.5] — 2026-06

The "almost 1.0" release: Hull ships its own installers and is solid end to end
on Windows, Linux, and Arch.

### Added
- **Own graphical installer** — `Hull-Setup.exe` (Windows): a frameless,
  Hull-themed WebView2 window, no console, no admin, no NSIS. A toggle installs
  **CLI + desktop app** or **CLI only**. Clean one-click uninstall via Apps &
  Features or `hull uninstall`.
- **Linux & Arch** — `get.sh` one-liner, a WebKitGTK `hull-installer`, an
  `install.sh`/`build.sh` source build, an AUR package (`hull` / `hull-gui`),
  and a `systemd --user` unit for the daemon.
- **First-run setup wizard** in the desktop app (Docker check → projects folder
  → base IP/TLD → starter services → apply).
- **Startup settings** — launch-at-login, close-to-tray, auto-start the daemon.
- **Richer import** — detects and wires extra shared services (mailpit,
  meilisearch, typesense, memcached, minio) from `.env`; adopts clusters by
  seeding routes from the compose file when there's no Caddyfile.
- `hulld` writes `~/.hull/hulld.log` (with panic capture) so the detached
  daemon's failures are diagnosable.

### Changed
- The GUI setup now enables the embedded router, and the hosts block + doctor
  honour the configured loopback (e.g. `127.0.0.2` to coexist with Herd) — so
  sites actually resolve on a custom loopback.
- macOS DNS registration uses the native admin prompt instead of printing sudo.
- Settings: "Clear caches" really flushes + re-detects; "Reset" shows accurate
  steps; Doctor reports an "unavailable" state instead of mock data.

### Removed
- The non-functional "Edit instance" dialog (service instances are recreated,
  not edited).

## [0.1.0] — 2026-06

Initial internal build: Go daemon (`hulld`) + CLI (`hull`) + Tauri GUI
(`hull-gui`). Docker-based projects with automatic HTTPS domains, shared
services, framework scaffolding (Laravel/WordPress/plain), and cluster adoption.
