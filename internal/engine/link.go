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
		return "", fmt.Errorf("%s is a legacy v1 project — adopt it first with: hull migrate %s", p.Name, p.Name)
	}
	def, version, err := services.Resolve(spec)
	if err != nil {
		return "", err
	}

	key := "redis"
	if def.IsDatabase {
		key = "db"
	}
	m := p.Manifest
	if m.Services == nil {
		m.Services = map[string]*manifest.Service{}
	}
	svc := &manifest.Service{
		Engine:  def.Name,
		Version: version,
		Mode:    manifest.ModeShared,
	}
	if def.IsDatabase {
		svc.Database = strings.ReplaceAll(m.Name, "-", "_")
	}
	prev := m.Services[key]
	m.Services[key] = svc
	if err := m.Validate(); err != nil {
		// Roll back the in-memory change; nothing was written.
		if prev == nil {
			delete(m.Services, key)
		} else {
			m.Services[key] = prev
		}
		return "", fmt.Errorf("cannot link %s to %s: %w", p.Name, spec, err)
	}

	if err := e.WriteArtifacts(m, p.Dir); err != nil {
		return "", err
	}

	instance, err := svcs.EnsureUp(ctx, def.Name, version)
	if err != nil {
		return "", err
	}
	if def.IsDatabase {
		if err := svcs.CreateDatabase(ctx, def.Name, version, svc.Database); err != nil {
			return "", fmt.Errorf("creating database %q in %s: %w", svc.Database, instance, err)
		}
	}

	if m.Template == "laravel" {
		if err := wireLaravelEnv(p.Dir, m); err != nil {
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
	m := p.Manifest
	svc, ok := m.Services[key]
	if !ok {
		return fmt.Errorf("project %s has no service %q", p.Name, key)
	}
	delete(m.Services, key)
	if err := m.Validate(); err != nil {
		m.Services[key] = svc
		return fmt.Errorf("cannot unlink %q: %w", key, err)
	}
	return e.WriteArtifacts(m, p.Dir)
}
