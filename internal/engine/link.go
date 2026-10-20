package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
)

// Link connects a project to a shared service instance ("engine" or
// "engine@version"): updates the manifest to mode:shared, boots the
// instance, creates the project's database, rewires the project .env, and
// regenerates compose.yaml. Returns the instance name.
func (e *Engine) Link(ctx context.Context, p *state.Project, spec string, svcs *services.Manager) (string, error) {
	if p.Manifest == nil {
		return "", fmt.Errorf("%s is a legacy v1 project , adopt it first with: hull migrate %s", p.Name, p.Name)
	}
	def, version, err := services.Resolve(spec)
	if err != nil {
		return "", err
	}

	// Manifest service key by role, so a project has at most one of each.
	key := def.Name
	switch {
	case def.IsDatabase:
		key = "db"
	case def.Name == "redis":
		key = "redis"
	case def.Name == "mailpit":
		key = "mail"
	case def.Category == "search":
		key = "search"
	case def.Category == "storage":
		key = "storage"
	case def.Category == "cache":
		key = "cache"
	}
	change := func(m *manifest.Manifest) error {
		if prev := m.Services[key]; prev != nil && def.IsDatabase && prev.Mode != manifest.ModeShared {
			return fmt.Errorf("%s already has its own %s database, and linking would point it at an empty shared one and leave that data behind. Export it first (hull export %s), then `hull unlink %s` and link again", p.Name, prev.Engine, p.Name, key)
		}
		if m.Services == nil {
			m.Services = map[string]*manifest.Service{}
		}
		svc := &manifest.Service{Engine: def.Name, Version: version, Mode: manifest.ModeShared}
		if def.IsDatabase {
			svc.Database = strings.ReplaceAll(m.Name, "-", "_")
		}
		m.Services[key] = svc
		return nil
	}
	// Try the change on a fresh copy first, so a link that cannot work fails
	// before anything is started, and the files change only after the
	// instance and the database exist.
	probe, err := manifest.Load(p.Dir)
	if err == nil {
		if err = change(probe); err == nil {
			err = probe.Validate()
		}
	}
	if err != nil {
		return "", fmt.Errorf("cannot link %s to %s: %w", p.Name, spec, err)
	}
	database := probe.Services[key].Database

	instance, err := svcs.EnsureUp(ctx, def.Name, version)
	if err != nil {
		return "", err
	}
	if def.IsDatabase {
		if err := svcs.CreateDatabase(ctx, def.Name, version, database); err != nil {
			return "", fmt.Errorf("creating database %q in %s: %w", database, instance, err)
		}
	}
	if err := e.updateManifest(p, change); err != nil {
		return "", fmt.Errorf("cannot link %s to %s: %w", p.Name, spec, err)
	}
	if def.IsDatabase {
		// Auto-provision the Adminer console and refresh its DB picker , a
		// convenience, so a hiccup here never fails the link.
		_ = e.EnsureAdminer(ctx)
	}
	if p.Manifest.Template == "laravel" {
		if err := e.wireLaravelEnv(p.Dir, p.Manifest); err != nil {
			return "", err
		}
	}
	return instance, nil
}

// Unlink removes a service key from the project manifest and regenerates
// artifacts. Shared instance data is left untouched.
func (e *Engine) Unlink(ctx context.Context, p *state.Project, key string) error {
	if p.Manifest == nil {
		return fmt.Errorf("%s is a legacy v1 project", p.Name)
	}
	if err := e.updateManifest(p, func(m *manifest.Manifest) error {
		if _, ok := m.Services[key]; !ok {
			return fmt.Errorf("project %s has no service %q", p.Name, key)
		}
		delete(m.Services, key)
		return nil
	}); err != nil {
		return fmt.Errorf("cannot unlink %q: %w", key, err)
	}
	// Drop the removed service from the Adminer picker (best-effort).
	_ = e.SyncAdminerServers(ctx)
	return nil
}
