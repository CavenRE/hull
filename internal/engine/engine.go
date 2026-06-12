package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/manifest"
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
}

func New(cfg *config.Config) *Engine {
	return &Engine{Config: cfg, Run: dockerx.Exec, EnsureNet: dockerx.EnsureNetwork}
}

// prepareNetworks creates the external networks generated compose files
// reference — a fresh v2 machine has no v1 setup to have made them.
func (e *Engine) prepareNetworks(ctx context.Context) error {
	if e.EnsureNet == nil {
		return nil
	}
	return e.EnsureNet(ctx, "caddy")
}

// ComposeContext returns the render context for this machine.
func (e *Engine) ComposeContext() compose.Context {
	return compose.Context{
		TLD:      e.Config.TLD,
		HullHome: filepath.ToSlash(e.Config.HullHome),
	}
}

// compose returns the compose driver for a project directory.
func (e *Engine) compose(dir string) dockerx.Compose {
	return dockerx.Compose{Dir: dir, Run: e.Run}
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
	// SkipScaffold skips the template init hook (tests, dry runs).
	SkipScaffold bool
	// SkipStart skips `docker compose up -d`.
	SkipStart bool
}

// NewProject scaffolds and boots a new project, returning its directory.
func (e *Engine) NewProject(ctx context.Context, opts NewOptions) (string, error) {
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
// the artifact always tracks the manifest).
func (e *Engine) Up(ctx context.Context, p *state.Project) error {
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
	return e.compose(p.Dir).Up(ctx)
}

// Down stops a project.
func (e *Engine) Down(ctx context.Context, p *state.Project) error {
	return e.compose(p.Dir).Down(ctx)
}

// Restart restarts a project.
func (e *Engine) Restart(ctx context.Context, p *state.Project) error {
	return e.compose(p.Dir).Restart(ctx)
}

// Logs tails a project's logs.
func (e *Engine) Logs(ctx context.Context, p *state.Project, follow bool) error {
	return e.compose(p.Dir).Logs(ctx, follow)
}

// ExecIn runs a command in one of the project's service containers.
func (e *Engine) ExecIn(ctx context.Context, p *state.Project, service string, cmd ...string) error {
	return e.compose(p.Dir).ExecIn(ctx, service, cmd...)
}

// Destroy tears down containers and volumes and deletes the project
// directory. The caller is responsible for confirmation.
func (e *Engine) Destroy(ctx context.Context, p *state.Project) error {
	if err := e.compose(p.Dir).DownVolumes(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "compose down failed; force-removing containers by label...")
		if err := dockerx.ForceRemoveProject(ctx, p.Name); err != nil {
			return fmt.Errorf("force cleanup: %w", err)
		}
	}
	return os.RemoveAll(p.Dir)
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

	// Round-trip through Parse to apply defaults and full validation.
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	return manifest.Parse(data)
}

// wireLaravelEnv points a scaffolded Laravel .env at the project's
// services — the Go port of v1's env wiring in commands/new.
func wireLaravelEnv(dir string, m *manifest.Manifest) error {
	envPath := filepath.Join(dir, ".env")
	if _, err := os.Stat(envPath); err != nil {
		return nil // no .env scaffolded (skipped scaffold) — nothing to wire
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

	if redis, ok := m.Services["redis"]; ok {
		host := "redis"
		if redis.Mode == manifest.ModeShared {
			host = templates.InstanceContainerName("redis", redis.Version)
		}
		for _, kv := range [][2]string{
			{"REDIS_HOST", host},
			{"CACHE_STORE", "redis"},
			{"SESSION_DRIVER", "redis"},
			{"QUEUE_CONNECTION", "redis"},
		} {
			if err := set(kv[0], kv[1]); err != nil {
				return err
			}
		}
	}
	return nil
}
