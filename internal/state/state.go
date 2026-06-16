package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CavenRE/hull/internal/manifest"
)

// Project is one entry in the registry: a directory under a registered root
// holding either a hull.yaml (v2) or a bare compose file (legacy v1).
type Project struct {
	Name string
	Dir  string
	// Manifest is nil for legacy projects.
	Manifest *manifest.Manifest
	// Legacy marks a v1-era project (compose file, no manifest). It can be
	// started/stopped but not rendered until adopted (hull migrate-v1).
	Legacy bool
	// Unmanaged marks a plain folder under a root: visible in listings
	// with an import affordance, but never touched by docker until the
	// user imports it.
	Unmanaged bool
	// Err records a manifest that exists but fails to parse; the project
	// is still listed so the problem is visible.
	Err error
}

// Scan walks the registered roots and returns all projects, sorted by name.
// Roots that do not exist are skipped silently (a machine may register a
// Sites and an Apps root before both exist). When two roots contain a
// project of the same name, the first one found wins and later duplicates
// are skipped — a name collision across roots must never break the listing
// (a developer can easily have the same folder name in two places).
func Scan(roots []string) ([]Project, error) {
	var projects []Project
	seen := map[string]string{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scanning %s: %w", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, e.Name())
			p, ok := load(dir, e.Name())
			if !ok {
				continue
			}
			if _, dup := seen[p.Name]; dup {
				continue // first root wins; skip the collision
			}
			seen[p.Name] = dir
			projects = append(projects, p)
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

// load classifies a single directory.
func load(dir, dirName string) (Project, bool) {
	if _, err := os.Stat(filepath.Join(dir, manifest.Filename)); err == nil {
		m, err := manifest.Load(dir)
		if err != nil {
			return Project{Name: dirName, Dir: dir, Err: err}, true
		}
		return Project{Name: m.Name, Dir: dir, Manifest: m}, true
	}
	for _, f := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return Project{Name: dirName, Dir: dir, Legacy: true}, true
		}
	}
	// Plain folder: listed so the user can see and import it, but no
	// docker activity until they do.
	return Project{Name: dirName, Dir: dir, Unmanaged: true}, true
}

// Find returns the project with the given name from the registered roots.
func Find(roots []string, name string) (*Project, error) {
	projects, err := Scan(roots)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		if projects[i].Name == name {
			return &projects[i], nil
		}
	}
	return nil, fmt.Errorf("project %q not found in %s", name, strings.Join(roots, ", "))
}

// Current returns the project containing dir (or dir itself), if dir lies
// inside a project directory under any root.
func Current(roots []string, dir string) (*Project, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, false
	}
	projects, err := Scan(roots)
	if err != nil {
		return nil, false
	}
	for i := range projects {
		pdir, err := filepath.Abs(projects[i].Dir)
		if err != nil {
			continue
		}
		if abs == pdir || strings.HasPrefix(abs, pdir+string(filepath.Separator)) {
			return &projects[i], true
		}
	}
	return nil, false
}
