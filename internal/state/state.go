package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CavenRE/hull/internal/ledger"
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
// are skipped , a name collision across roots must never break the listing
// (a developer can easily have the same folder name in two places).
func Scan(roots []string, extra ...string) ([]Project, error) {
	var out []Project
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
			out = append(out, p)
		}
	}
	// Individually-registered projects (imported in place, outside any root):
	// each entry is a project directory itself, so load it directly rather than
	// scanning it as a parent. Entries that no longer exist, or that lost their
	// hull.yaml and reverted to a bare folder, are skipped so a stale
	// registration self-heals instead of showing a broken row. A name already
	// found under a root wins.
	for _, dir := range extra {
		if dir == "" {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		p, ok := load(dir, filepath.Base(dir))
		if !ok || p.Unmanaged {
			continue
		}
		if _, dup := seen[p.Name]; dup {
			continue
		}
		seen[p.Name] = dir
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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

// Find returns the project with the given name from the registered roots and
// any individually-registered project directories (extra).
func Find(roots []string, name string, extra ...string) (*Project, error) {
	projects, err := Scan(roots, extra...)
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

// FindCluster resolves an adopted cluster recorded in the started ledger by
// name, even when its directory lives outside the configured roots. This keeps
// a cluster that appears in `hull cluster list` (which is ledger-reconciled)
// operable by name after its root was removed. ok=false when there is no such
// ledger cluster or its manifest no longer loads.
func FindCluster(hullHome, name string) (*Project, bool) {
	for _, e := range ledger.List(hullHome) {
		if e.Kind != "cluster" || e.Name != name {
			continue
		}
		m, err := manifest.Load(e.Dir)
		if err != nil {
			return nil, false
		}
		return &Project{Name: e.Name, Dir: e.Dir, Manifest: m}, true
	}
	return nil, false
}

// Current returns the project containing dir (or dir itself), if dir lies
// inside a project directory under any root.
func Current(roots []string, dir string, extra ...string) (*Project, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, false
	}
	projects, err := Scan(roots, extra...)
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

// Under reports whether dir is inside root (or is root itself), comparing
// cleaned absolute paths case-insensitively so it behaves on Windows. Used to
// decide whether an imported directory needs a standalone registration or is
// already covered by a parked root.
func Under(root, dir string) bool {
	r, err1 := filepath.Abs(root)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	r, d = filepath.Clean(r), filepath.Clean(d)
	if strings.EqualFold(r, d) {
		return true
	}
	sep := string(filepath.Separator)
	return len(d) > len(r)+1 && strings.EqualFold(d[:len(r)+1], r+sep)
}
