package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/ledger"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// Engine orchestrates project lifecycle on top of the manifest, compose,
// and dockerx packages. In Phase 3 it moves behind the hulld API.
type Engine struct {
	Config *config.Config
	// Run executes host commands; defaults to dockerx.Exec.
	Run dockerx.Runner
	// EnsureNet creates a docker network if missing; defaults to
	// dockerx.EnsureNetwork (stubbed in tests).
	EnsureNet func(ctx context.Context, name string) error
	// RunningHull lists running compose projects carrying Hull's ownership
	// label , the stop-all safety sweep. Injectable for tests; defaults to
	// dockerx.RunningHullProjects.
	RunningHull func(ctx context.Context) ([]string, error)
}

func New(cfg *config.Config) *Engine {
	return &Engine{
		Config:      cfg,
		Run:         dockerx.Exec,
		EnsureNet:   dockerx.EnsureNetwork,
		RunningHull: dockerx.RunningHullProjects,
	}
}

// prepareNetworks creates the external networks generated compose files
// reference , a fresh v2 machine has no v1 setup to have made them.
func (e *Engine) prepareNetworks(ctx context.Context) error {
	if e.EnsureNet == nil {
		return nil
	}
	return e.EnsureNet(ctx, "caddy")
}

// ComposeContext returns the render context for this machine.
func (e *Engine) ComposeContext() compose.Context {
	ctx := compose.Context{
		TLD:      e.Config.TLD,
		HullHome: filepath.ToSlash(e.Config.HullHome),
	}
	// On native Linux, bind-mounted files keep the host's uid/gid and the
	// container's www-data (uid 33) cannot write them , so pass PUID/PGID to
	// remap the container user. macOS/Windows Docker Desktop handle this in
	// the VM, so leave the identity empty there.
	if runtime.GOOS == "linux" {
		ctx.HostUID = strconv.Itoa(os.Getuid())
		ctx.HostGID = strconv.Itoa(os.Getgid())
	}
	return ctx
}

// compose returns the compose driver for a project directory.
func (e *Engine) compose(dir string) dockerx.Compose {
	return dockerx.Compose{Dir: dir, Run: e.Run}
}

// isCluster reports whether a project wraps an external compose stack.
func isCluster(p *state.Project) bool {
	return p.Manifest != nil && p.Manifest.Type == manifest.TypeCluster
}

// projectName is the docker compose project name for p , always a slug, so a
// directory with spaces or capitals never decides (or breaks) the identity.
func projectName(p *state.Project) string {
	if p.Manifest != nil && p.Manifest.Name != "" {
		return p.Manifest.Name // manifest names are validated slugs
	}
	return manifest.Slug(p.Name)
}

// composeFor returns the compose driver for a project , for clusters that's
// the wrapped stack (operational root + -f files + profiles); for sites/apps
// it's the project's own generated compose.yaml. The project name is pinned
// with -p so adopted dirs with spaces/capitals stay deterministic.
func (e *Engine) composeFor(p *state.Project) dockerx.Compose {
	envFile := ""
	if p.Manifest != nil && p.Manifest.EnvFile != "" {
		envFile = filepath.Join(p.Dir, p.Manifest.EnvFile)
	}
	if isCluster(p) {
		return dockerx.Compose{
			Dir:      filepath.Join(p.Dir, p.Manifest.ComposeRoot),
			Run:      e.Run,
			Name:     projectName(p),
			Files:    p.Manifest.ComposeFiles,
			Profiles: p.Manifest.Profiles,
			EnvFile:  envFile,
		}
	}
	return dockerx.Compose{Dir: p.Dir, Run: e.Run, Name: projectName(p), EnvFile: envFile}
}

// NewOptions describes `hull new`.
type NewOptions struct {
	Name     string
	Template string
	// Root receives the project; empty means the first configured root.
	Root string
	// DB is a database engine name, or "" for none.
	DB string
	// DBVersion pins the database engine version.
	DBVersion string
	Redis     bool
	PHP       string
	// Version pins the framework (wordpress tag / laravel constraint).
	Version string
	// ExtraServices are services beyond the DB/Redis shorthands (e.g. from
	// repeatable `--service`). Key defaults to the engine name.
	ExtraServices []ServiceSpec
	// Serve controls whether the project gets a routed domain. nil = default
	// (served).
	Serve *bool
	// SkipScaffold skips the template init hook (tests, dry runs).
	SkipScaffold bool
	// SkipStart skips `docker compose up -d`.
	SkipStart bool
}

// ServiceSpec is one additional service request (key optional).
type ServiceSpec struct {
	Key     string
	Engine  string
	Version string
}

// NewProject scaffolds and boots a new project, returning its directory.
func (e *Engine) NewProject(ctx context.Context, opts NewOptions) (string, error) {
	// Normalize the name to a domain-safe slug so "My App" → "my-app" for the
	// directory, domain, and manifest alike.
	if s := manifest.Slug(opts.Name); s != "" {
		opts.Name = s
	}
	m, err := buildManifest(opts)
	if err != nil {
		return "", err
	}

	root := opts.Root
	if root == "" {
		if len(e.Config.Roots) == 0 {
			return "", fmt.Errorf("no project roots configured")
		}
		root = e.Config.Roots[0]
	}
	dir := filepath.Join(root, opts.Name)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("target directory %s already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	if !opts.SkipScaffold {
		err := templates.Scaffold(ctx, opts.Template, templates.ScaffoldOptions{
			Dir:     dir,
			Version: opts.Version,
			Run:     templates.Runner(e.Run),
		})
		if err != nil {
			return dir, err
		}
	}

	if err := e.WriteArtifacts(m, dir); err != nil {
		return dir, err
	}

	if opts.Template == "laravel" {
		if err := wireLaravelEnv(dir, m); err != nil {
			return dir, err
		}
	}

	if !opts.SkipStart {
		if err := templates.EnsureSystemFiles(e.Config.HullHome); err != nil {
			return dir, err
		}
		if err := e.prepareNetworks(ctx); err != nil {
			return dir, err
		}
		if err := e.compose(dir).Up(ctx); err != nil {
			return dir, err
		}
		p := &state.Project{Name: m.Name, Dir: dir, Manifest: m}
		e.recordStarted(p)
		// post_create hooks run after the project boots (readiness-gated). For
		// Laravel this carries the default `artisan migrate` (best-effort).
		if err := e.runHooks(ctx, p, "post_create", true); err != nil {
			return dir, err
		}
		// If this project brought a database, auto-provision the Adminer console
		// and refresh its picker (best-effort , never fail the create).
		if _, _, hasDB := m.DatabaseService(); hasDB {
			_ = e.EnsureAdminer(ctx)
		}
	}
	return dir, nil
}

// WriteArtifacts writes hull.yaml and the generated compose.yaml.
func (e *Engine) WriteArtifacts(m *manifest.Manifest, dir string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.Filename), data, 0o644); err != nil {
		return err
	}
	return e.Render(m, dir)
}

// Render regenerates compose.yaml from the manifest.
func (e *Engine) Render(m *manifest.Manifest, dir string) error {
	f, err := compose.Render(m, e.ComposeContext())
	if err != nil {
		return err
	}
	data, err := compose.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "compose.yaml"), data, 0o644)
}

// Up starts a project (regenerating compose.yaml first for v2 projects so
// the artifact always tracks the manifest). Clusters drive their own compose
// project as-is (no Hull-generated artifact, no caddy network).
func (e *Engine) Up(ctx context.Context, p *state.Project) error {
	if err := e.runHooks(ctx, p, "pre_up", false); err != nil {
		return err
	}
	if isCluster(p) {
		if err := e.preflightPorts(ctx, p); err != nil {
			return err
		}
		if err := e.composeFor(p).Up(ctx); err != nil {
			return err
		}
	} else {
		if err := e.renderForUp(ctx, p); err != nil {
			return err
		}
		if err := e.compose(p.Dir).Up(ctx); err != nil {
			return err
		}
	}
	e.recordStarted(p)
	return e.runHooks(ctx, p, "post_up", true)
}

// renderForUp prepares a non-cluster project to start: self-heal system files,
// ensure external networks, and regenerate compose.yaml from the manifest.
func (e *Engine) renderForUp(ctx context.Context, p *state.Project) error {
	if err := templates.EnsureSystemFiles(e.Config.HullHome); err != nil {
		return err
	}
	if err := e.prepareNetworks(ctx); err != nil {
		return err
	}
	if p.Manifest != nil {
		return e.Render(p.Manifest, p.Dir)
	}
	return nil
}

// Down stops a project. pre_down hooks run best-effort , stopping must always
// be able to proceed (a failing cleanup hook never wedges the machine).
func (e *Engine) Down(ctx context.Context, p *state.Project) error {
	_ = e.runHooks(ctx, p, "pre_down", false)
	if err := e.composeFor(p).Down(ctx); err != nil {
		return err
	}
	e.recordStopped(projectName(p))
	return nil
}

// Restart recreates the project's containers (force-recreate) so a container
// left with drifted config or detached networks is rebuilt, not merely bounced
// , plain `compose restart` cannot repair attachment drift (the Sentinel bug).
func (e *Engine) Restart(ctx context.Context, p *state.Project) error {
	if isCluster(p) {
		if err := e.composeFor(p).Recreate(ctx); err != nil {
			return err
		}
	} else {
		if err := e.renderForUp(ctx, p); err != nil {
			return err
		}
		if err := e.compose(p.Dir).Recreate(ctx); err != nil {
			return err
		}
	}
	e.recordStarted(p)
	return nil
}

// Repair brings a project down (containers + networks; named volumes kept) and
// back up , the clean-slate fix when a half-finished `up` left a container
// detached or otherwise wedged.
func (e *Engine) Repair(ctx context.Context, p *state.Project) error {
	if err := e.composeFor(p).Down(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "repair: compose down reported (continuing to bring it back up):", err)
	}
	return e.Up(ctx, p)
}

// Rebuild rebuilds the project's images and brings it back up. With noCache
// the build ignores layer caches (a from-scratch image build).
func (e *Engine) Rebuild(ctx context.Context, p *state.Project, noCache bool) error {
	if !isCluster(p) {
		if err := templates.EnsureSystemFiles(e.Config.HullHome); err != nil {
			return err
		}
		if err := e.prepareNetworks(ctx); err != nil {
			return err
		}
		if p.Manifest != nil {
			if err := e.Render(p.Manifest, p.Dir); err != nil {
				return err
			}
		}
	}
	c := e.composeFor(p)
	if err := c.Build(ctx, noCache); err != nil {
		return err
	}
	if err := c.Up(ctx); err != nil {
		return err
	}
	e.recordStarted(p)
	return e.runHooks(ctx, p, "post_rebuild", true)
}

// Reset wipes the project's named volumes (databases, caches) and starts it
// fresh , the "drop the data, start from scratch" flow. Bind-mounted files on
// the host are untouched; only named volumes are removed.
func (e *Engine) Reset(ctx context.Context, p *state.Project) error {
	if err := e.composeFor(p).DownVolumes(ctx); err != nil {
		return err
	}
	if err := e.Up(ctx, p); err != nil {
		return err
	}
	return e.runHooks(ctx, p, "post_reset", true)
}

// Volumes lists the project's named volumes , the blast radius of a Reset.
func (e *Engine) Volumes(ctx context.Context, p *state.Project) ([]string, error) {
	return e.composeFor(p).Volumes(ctx)
}

// Logs tails a project's logs.
func (e *Engine) Logs(ctx context.Context, p *state.Project, follow bool) error {
	return e.composeFor(p).Logs(ctx, follow)
}

// ExecIn runs a command in one of the project's service containers.
func (e *Engine) ExecIn(ctx context.Context, p *state.Project, service string, cmd ...string) error {
	return e.composeFor(p).ExecIn(ctx, service, cmd...)
}

// Destroy tears down containers and volumes and deletes the project
// directory. The caller is responsible for confirmation. For a CLUSTER it
// never deletes files (that's the user's repo) , it tears the stack down and
// un-adopts by removing only the Hull manifest.
func (e *Engine) Destroy(ctx context.Context, p *state.Project) error {
	defer e.recordStopped(projectName(p))
	// Once the project is gone, drop its database from the Adminer picker.
	defer func() { _ = e.SyncAdminerServers(ctx) }()
	if isCluster(p) {
		if err := e.composeFor(p).DownVolumes(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cluster compose down failed (continuing to un-adopt):", err)
		}
		return os.Remove(filepath.Join(p.Dir, manifest.Filename))
	}
	if err := e.compose(p.Dir).DownVolumes(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "compose down failed; force-removing containers by label...")
		if err := dockerx.ForceRemoveProject(ctx, p.Name); err != nil {
			return fmt.Errorf("force cleanup: %w", err)
		}
	}
	return e.removeProjectDir(ctx, p.Dir)
}

// removeProjectDir deletes a project directory. A container that runs as root or
// www-data (WordPress, and others) can leave bind-mounted files owned by another
// UID, which os.RemoveAll can't unlink as the unprivileged user , `hull rm`
// would fail with "permission denied" and orphan the directory. On a permission
// failure, delete it as root inside a throwaway container (the same docker-run
// approach `new` uses to chown scaffolded files).
func (e *Engine) removeProjectDir(ctx context.Context, dir string) error {
	if err := os.RemoveAll(dir); err == nil || !errors.Is(err, os.ErrPermission) {
		return err
	}
	parent, base := filepath.Dir(dir), filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) || parent == dir {
		return fmt.Errorf("refusing to force-remove suspicious path %q", dir)
	}
	if err := e.Run(ctx, "", "docker", "run", "--rm",
		"-v", parent+":/work", "alpine", "rm", "-rf", "/work/"+base); err != nil {
		return fmt.Errorf("removing %s (contains files owned by a container user): %w", dir, err)
	}
	return nil
}

// --- started-project ledger + stop-all -------------------------------------

func (e *Engine) ledgerEntry(p *state.Project) ledger.Entry {
	ent := ledger.Entry{Name: projectName(p), Dir: p.Dir}
	if p.Manifest != nil {
		ent.Kind = string(p.Manifest.Type)
		if p.Manifest.Type == manifest.TypeCluster {
			ent.ComposeRoot = p.Manifest.ComposeRoot
			ent.ComposeFiles = p.Manifest.ComposeFiles
			ent.Profiles = p.Manifest.Profiles
		}
	}
	return ent
}

// recordStarted / recordStopped maintain the started ledger. Both are
// best-effort , a ledger write must never block a lifecycle action, and the
// roots scan + label sweep in StopAll backstop a stale ledger.
func (e *Engine) recordStarted(p *state.Project) { _ = ledger.Add(e.Config.HullHome, e.ledgerEntry(p)) }
func (e *Engine) recordStopped(name string)      { _ = ledger.Remove(e.Config.HullHome, name) }

// StopAll brings down every project Hull started and every running shared
// service , so nothing keeps holding ports after the daemon stops. It draws
// targets from three deduped sources: the configured roots (managed
// projects), the started ledger (catches adopted clusters living outside the
// roots), and a sweep of containers carrying Hull's ownership label (catches
// rendered orphans). Best-effort: one failure never blocks the rest. Returns
// the number of projects and services stopped.
func (e *Engine) StopAll(ctx context.Context) (int, error) {
	stopped := 0
	var errs []error
	done := map[string]bool{}

	// 1. Managed projects under the configured roots.
	if projects, err := state.Scan(e.Config.Roots, e.Config.Projects...); err == nil {
		for i := range projects {
			p := &projects[i]
			if p.Manifest == nil {
				continue
			}
			name := projectName(p)
			if done[name] {
				continue
			}
			done[name] = true
			if err := e.Down(ctx, p); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			} else {
				stopped++
			}
		}
	}

	// 2. Started ledger , catches adopted clusters and out-of-root projects
	//    a roots scan cannot see.
	for _, ent := range ledger.List(e.Config.HullHome) {
		if done[ent.Name] {
			continue
		}
		done[ent.Name] = true
		c := dockerx.Compose{
			Dir:      filepath.Join(ent.Dir, ent.ComposeRoot),
			Run:      e.Run,
			Name:     ent.Name,
			Files:    ent.ComposeFiles,
			Profiles: ent.Profiles,
		}
		if err := c.Down(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ent.Name, err))
		} else {
			stopped++
			e.recordStopped(ent.Name)
		}
	}

	// 3. Safety sweep , any still-running project carrying Hull's ownership
	//    label that we haven't already handled (a rendered orphan).
	if e.RunningHull != nil {
		if names, err := e.RunningHull(ctx); err == nil {
			for _, name := range names {
				if done[name] {
					continue
				}
				done[name] = true
				if err := e.Run(ctx, "", "docker", "compose", "-p", name, "down"); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", name, err))
				} else {
					stopped++
				}
			}
		}
	}

	// 4. Shared service instances. Stop EVERY instance unconditionally: a
	//    `compose down` on an already-stopped instance is a harmless no-op, so
	//    we never gate on a running-detection (a docker-ps label read) that
	//    could misreport and leave a live service running after `hull stop`.
	m := services.NewManager(e.Config)
	m.Run = e.Run
	if instances, err := m.List(ctx); err == nil {
		for _, in := range instances {
			if err := m.Stop(ctx, in.Name); err != nil {
				errs = append(errs, fmt.Errorf("service %s: %w", in.Name, err))
			} else if in.Running {
				// Count only what was actually up, so the "Stopped N" total is
				// honest even though we down-ed every instance.
				stopped++
			}
		}
	}

	return stopped, errors.Join(errs...)
}

// PatchOptions are the project fields `hull set` / PATCH /v1/projects can
// change. A nil pointer means "leave unchanged".
type PatchOptions struct {
	PHP    *string
	Domain *string
	Serve  *bool
}

// SetProjectFields mutates a managed project's manifest, validates, and
// re-renders compose.yaml. On invalid input the manifest is left unchanged.
// Shared by the CLI `hull set` and the daemon PATCH handler (core-first).
func (e *Engine) SetProjectFields(p *state.Project, opts PatchOptions) error {
	if p.Manifest == nil {
		return fmt.Errorf("%s is not managed by Hull yet , import it first", p.Name)
	}
	m := p.Manifest
	old := *m
	if opts.PHP != nil {
		m.PHP = *opts.PHP
	}
	if opts.Domain != nil {
		m.Domain = *opts.Domain
	}
	if opts.Serve != nil {
		m.Serve = opts.Serve
	}
	if err := m.Validate(); err != nil {
		*m = old
		return err
	}
	return e.WriteArtifacts(m, p.Dir)
}

// buildManifest assembles and validates the manifest for NewOptions.
func buildManifest(opts NewOptions) (*manifest.Manifest, error) {
	m := &manifest.Manifest{
		Schema:   manifest.CurrentSchema,
		Name:     opts.Name,
		Type:     manifest.TypeSite,
		Template: opts.Template,
		PHP:      opts.PHP,
		Version:  opts.Version,
		Serve:    opts.Serve,
	}
	if opts.Template == "wordpress" {
		m.PHP = "" // wordpress images bundle PHP
		if opts.DB == "" {
			opts.DB = "mariadb"
		}
	}
	if opts.DB != "" {
		m.Services = map[string]*manifest.Service{
			"db": {Engine: opts.DB, Version: opts.DBVersion},
		}
	}
	if opts.Redis {
		if m.Services == nil {
			m.Services = map[string]*manifest.Service{}
		}
		m.Services["redis"] = &manifest.Service{Engine: "redis"}
	}
	for _, s := range opts.ExtraServices {
		if m.Services == nil {
			m.Services = map[string]*manifest.Service{}
		}
		key := s.Key
		if key == "" {
			key = s.Engine
		}
		m.Services[key] = &manifest.Service{Engine: s.Engine, Version: s.Version}
	}

	// Laravel ships SESSION_DRIVER=database etc., so the app 500s until its
	// tables exist. A default post_create hook runs `artisan migrate` once the
	// project is up (best-effort: a project without a reachable DB just leaves
	// the user to re-run it). Persisted into hull.yaml so it's visible/editable.
	if opts.Template == "laravel" {
		m.Hooks.PostCreate = append(m.Hooks.PostCreate, manifest.Hook{
			Run: "php artisan migrate --force", Service: "app", IgnoreFailure: true,
		})
	}

	// Round-trip through Parse to apply defaults and full validation.
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	return manifest.Parse(data)
}

// wireLaravelEnv points a scaffolded Laravel .env at the project's
// services , the Go port of v1's env wiring in commands/new.
func wireLaravelEnv(dir string, m *manifest.Manifest) error {
	envPath := filepath.Join(dir, ".env")
	if _, err := os.Stat(envPath); err != nil {
		return nil // no .env scaffolded (skipped scaffold) , nothing to wire
	}

	set := func(key, value string) error {
		return envfile.SetFile(envPath, key, value)
	}

	dbKey, db, hasDB := m.DatabaseService()
	if hasDB {
		host := dbKey
		if db.Mode == manifest.ModeShared {
			host = templates.InstanceContainerName(db.Engine, db.Version)
		}
		pairs := [][2]string{
			{"DB_HOST", host},
			{"DB_DATABASE", db.Database},
		}
		switch db.Engine {
		case "postgres":
			pairs = append(pairs, [2]string{"DB_CONNECTION", "pgsql"}, [2]string{"DB_PORT", "5432"}, [2]string{"DB_USERNAME", "postgres"}, [2]string{"DB_PASSWORD", ""})
		case "mysql":
			pairs = append(pairs, [2]string{"DB_CONNECTION", "mysql"}, [2]string{"DB_PORT", "3306"}, [2]string{"DB_USERNAME", "root"}, [2]string{"DB_PASSWORD", ""})
		case "mariadb":
			pairs = append(pairs, [2]string{"DB_CONNECTION", "mariadb"}, [2]string{"DB_PORT", "3306"}, [2]string{"DB_USERNAME", "root"}, [2]string{"DB_PASSWORD", ""})
		}
		for _, kv := range pairs {
			if err := set(kv[0], kv[1]); err != nil {
				return err
			}
		}
	} else {
		if err := set("DB_CONNECTION", "sqlite"); err != nil {
			return err
		}
		dbFile := filepath.Join(dir, "database", "database.sqlite")
		if _, err := os.Stat(dbFile); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err == nil {
				_ = os.WriteFile(dbFile, nil, 0o644)
			}
		}
	}

	// Non-database services: write each engine's Laravel env block. The
	// host is the dedicated compose service (= manifest key) or the shared
	// instance container.
	for _, key := range m.ServiceKeys() {
		svc := m.Services[key]
		if eng, ok := templates.Engine(svc.Engine); ok && eng.IsDatabase {
			continue // databases handled above
		}
		host := key
		if svc.Mode == manifest.ModeShared {
			host = templates.InstanceContainerName(svc.Engine, svc.Version)
		}
		for _, kv := range LaravelServiceEnv(svc.Engine, host) {
			if err := set(kv[0], kv[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

// LaravelServiceEnv returns the .env key/value pairs that point a Laravel
// app at a non-database service instance reachable at host.
func LaravelServiceEnv(engine, host string) [][2]string {
	switch engine {
	case "redis":
		return [][2]string{
			{"REDIS_HOST", host},
			{"CACHE_STORE", "redis"},
			{"SESSION_DRIVER", "redis"},
			{"QUEUE_CONNECTION", "redis"},
		}
	case "memcached":
		return [][2]string{
			{"MEMCACHED_HOST", host},
			{"CACHE_STORE", "memcached"},
		}
	case "mailpit":
		return [][2]string{
			{"MAIL_MAILER", "smtp"},
			{"MAIL_HOST", host},
			{"MAIL_PORT", "1025"},
			{"MAIL_USERNAME", "null"},
			{"MAIL_PASSWORD", "null"},
			{"MAIL_ENCRYPTION", "null"},
		}
	case "meilisearch":
		return [][2]string{
			{"SCOUT_DRIVER", "meilisearch"},
			{"MEILISEARCH_HOST", "http://" + host + ":7700"},
			{"MEILISEARCH_KEY", "hullMasterKey"},
		}
	case "typesense":
		return [][2]string{
			{"SCOUT_DRIVER", "typesense"},
			{"TYPESENSE_API_KEY", "hullTypesenseKey"},
			{"TYPESENSE_HOST", host},
			{"TYPESENSE_PORT", "8108"},
			{"TYPESENSE_PROTOCOL", "http"},
		}
	case "minio":
		return [][2]string{
			{"FILESYSTEM_DISK", "s3"},
			{"AWS_ACCESS_KEY_ID", "hull"},
			{"AWS_SECRET_ACCESS_KEY", "hullsecret"},
			{"AWS_DEFAULT_REGION", "us-east-1"},
			{"AWS_BUCKET", "local"},
			{"AWS_ENDPOINT", "http://" + host + ":9000"},
			{"AWS_USE_PATH_STYLE_ENDPOINT", "true"},
		}
	}
	return nil
}
