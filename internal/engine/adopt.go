package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
	"github.com/CavenRE/hull/internal/wpconfig"
)

// sharedHost is the in-network hostname of a shared service instance.
func sharedHost(s *manifest.Service) string {
	return templates.InstanceContainerName(s.Engine, s.Version)
}

// BuildImportManifest turns auto-detection results (plus explicit
// overrides, which win) into a validated manifest for an existing project.
func BuildImportManifest(name string, det bundle.Detection, overrides NewOptions) (*manifest.Manifest, error) {
	// Folders are adopted in place, so the name often carries spaces/capitals
	// (e.g. "My App"). Slugify it to a docker-safe identity the manifest will
	// accept and docker compose can use; the folder itself is left untouched.
	if s := manifest.Slug(name); s != "" {
		name = s
	}
	template := overrides.Template
	if template == "" {
		template = det.Template
	}
	php := overrides.PHP
	if php == "" {
		php = det.PHP
	}
	db := overrides.DB
	if db == "" {
		db = det.DB
	}
	m := &manifest.Manifest{
		Schema:   manifest.CurrentSchema,
		Name:     name,
		Type:     manifest.TypeSite,
		Template: template,
		PHP:      php,
	}
	if template == "wordpress" {
		m.PHP = ""
		if db == "" {
			db = "mariadb"
		}
	}
	if db != "" {
		m.Services = map[string]*manifest.Service{
			"db": {Engine: db, Version: overrides.DBVersion, Database: sanitizeDBName(det.Database)},
		}
	}
	if det.Redis || overrides.Redis {
		if m.Services == nil {
			m.Services = map[string]*manifest.Service{}
		}
		m.Services["redis"] = &manifest.Service{Engine: "redis"}
	}
	// Extra shared services discovered in .env (mailpit, meilisearch, …). The
	// engine name doubles as the manifest key — wireLaravelEnv keys on Engine,
	// so each comes up wired. Keyed by engine, so duplicates collapse.
	for _, eng := range det.Extras {
		if m.Services == nil {
			m.Services = map[string]*manifest.Service{}
		}
		if _, ok := m.Services[eng]; !ok {
			m.Services[eng] = &manifest.Service{Engine: eng}
		}
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	return manifest.Parse(data)
}

// sanitizeDBName keeps detected database names usable across engines.
func sanitizeDBName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "null" {
		return "" // let manifest defaults derive from the project name
	}
	return strings.ReplaceAll(name, "-", "_")
}

// Adopt writes Hull artifacts into an existing project directory and
// patches its framework config to point at Hull's services — the apply
// half of v1's import. Original files get .hull-backup copies.
func (e *Engine) Adopt(m *manifest.Manifest, dir string) error {
	if err := e.WriteArtifacts(m, dir); err != nil {
		return err
	}

	switch m.Template {
	case "laravel", "plain":
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			if err := backup(envPath); err != nil {
				return err
			}
		}
		if m.Template == "laravel" {
			if err := wireLaravelEnv(dir, m); err != nil {
				return err
			}
		}
	case "wordpress":
		cfgPath := filepath.Join(dir, "wp-config.php")
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return nil // container will generate one on first boot
		}
		if err := backup(cfgPath); err != nil {
			return err
		}
		_, db, ok := m.DatabaseService()
		if !ok {
			return fmt.Errorf("wordpress manifest without database service")
		}
		host := "db"
		if db.Mode == manifest.ModeShared {
			host = sharedHost(db)
		}
		content := string(data)
		content = wpconfig.SetDefine(content, "DB_HOST", host)
		content = wpconfig.SetDefine(content, "DB_NAME", db.Database)
		content = wpconfig.SetDefine(content, "DB_USER", "root")
		content = wpconfig.SetDefine(content, "DB_PASSWORD", "")
		content = wpconfig.EnsureProxyFix(content)
		if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".hull-backup", data, 0o644)
}

// ImportExisting turns an unmanaged folder or legacy v1 project into a
// managed one and boots it — the GUI's one-click Import. Progress goes to
// log; SQL dump restore stays interactive (CLI hull import) for now.
func (e *Engine) ImportExisting(ctx context.Context, p *state.Project, log func(string)) error {
	if p.Manifest != nil {
		return fmt.Errorf("%s is already managed by Hull", p.Name)
	}

	if p.Legacy {
		log("legacy v1 compose detected — migrating")
		m, err := e.MigrateV1(p)
		if err != nil {
			return err
		}
		log(fmt.Sprintf("adopted as %s (old compose saved as *.v1-backup)", m.Template))
	} else {
		det := bundle.Detect(p.Dir)
		log(fmt.Sprintf("detected: %s (php %s, db %s)", det.Template, orDefault(det.PHP, "default"), orDefault(det.DB, "none")))
		m, err := BuildImportManifest(p.Name, det, NewOptions{})
		if err != nil {
			return err
		}
		if err := e.Adopt(m, p.Dir); err != nil {
			return err
		}
		log("hull.yaml written, framework config patched (backups: *.hull-backup)")
	}

	fresh, err := state.Find(e.Config.Roots, p.Name)
	if err != nil {
		return err
	}
	log("starting containers...")
	if err := e.Up(ctx, fresh); err != nil {
		return err
	}

	if dumps := bundle.FindDumps(p.Dir); len(dumps) > 0 {
		names := make([]string, len(dumps))
		for i, d := range dumps {
			names[i] = filepath.Base(d)
		}
		log("found database dump(s): " + strings.Join(names, ", "))
		log("restore interactively with: hull import " + p.Name + " (dump wizard lands in the GUI soon)")
	}
	return nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
