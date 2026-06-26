package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

// MigrateV1 adopts a bash-Hull project: reconstructs a manifest from its
// legacy compose file, backs the file up, and regenerates artifacts. The
// project's .env is left untouched , v2 dedicated services keep v1's
// service names (db, redis), so existing wiring stays valid.
func (e *Engine) MigrateV1(p *state.Project) (*manifest.Manifest, error) {
	if p.Manifest != nil {
		return nil, fmt.Errorf("%s already has a hull.yaml", p.Name)
	}
	info, err := bundle.DetectLegacy(p.Dir)
	if err != nil {
		return nil, err
	}

	m := &manifest.Manifest{
		Schema:   manifest.CurrentSchema,
		Name:     p.Name,
		Type:     manifest.TypeSite,
		Template: info.Template,
		PHP:      info.PHP,
	}
	if host := info.Host; host != "" {
		domain := strings.TrimSuffix(host, "."+e.Config.TLD)
		if domain != p.Name && domain != host {
			m.Domain = domain
		}
	}
	if info.Template == "wordpress" {
		m.PHP = ""
	}
	if info.DB != "" {
		m.Services = map[string]*manifest.Service{
			"db": {Engine: info.DB, Version: info.DBVersion, Database: strings.ReplaceAll(info.Database, "-", "_")},
		}
	}
	if info.Redis {
		if m.Services == nil {
			m.Services = map[string]*manifest.Service{}
		}
		m.Services["redis"] = &manifest.Service{Engine: "redis"}
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	parsed, err := manifest.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("reconstructed manifest invalid: %w", err)
	}

	legacyPath := filepath.Join(p.Dir, info.ComposeFile)
	if err := os.Rename(legacyPath, legacyPath+".v1-backup"); err != nil {
		return nil, err
	}
	if err := e.WriteArtifacts(parsed, p.Dir); err != nil {
		// Restore the legacy file so the project keeps working.
		_ = os.Rename(legacyPath+".v1-backup", legacyPath)
		return nil, err
	}
	return parsed, nil
}
