package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
// Individually-registered projects (imported in place, outside any root) are
// loaded directly; ones that are gone or lost their hull.yaml are skipped, so
// a stale registration self-heals instead of showing a broken row. A root that
// cannot be read is reported in the error, and the projects of the other
// roots are still returned.
func Scan(roots []string, extra ...string) ([]Project, error) {
	var out []Project
	seen := map[string]bool{}
	err := walk(roots, extra, func(p Project) {
		if seen[p.Name] {
			return
		}
		seen[p.Name] = true
		out = append(out, p)
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}

// walk loads each candidate directory in scan order: the subfolders of every
// root, then the registered dirs (skipping those that are plain folders). A
// directory is visited once. An unreadable root is reported after the walk.
func walk(roots, extra []string, visit func(Project)) error {
	var errs []error
	seenDir := map[string]bool{}
	try := func(dir, dirName string, registered bool) {
		if seenDir[dir] {
			return
		}
		seenDir[dir] = true
		if p, ok := load(dir, dirName); ok && (!registered || !p.Unmanaged) {
			visit(p)
		}
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("scanning %s: %w", root, err))
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				try(filepath.Join(root, e.Name()), e.Name(), false)
			}
		}
	}
	for _, dir := range extra {
		if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
			try(dir, filepath.Base(dir), true)
		}
	}
	return errors.Join(errs...)
}

// CheckUnique refuses a name that more than one folder resolves to (plain
// folders included), so a destructive verb never acts on the wrong folder.
func CheckUnique(roots []string, name string, extra ...string) error {
	var dirs []string
	_ = walk(roots, extra, func(p Project) {
		if p.Name == name {
			dirs = append(dirs, p.Dir)
		}
	})
	if len(dirs) > 1 {
		return fmt.Errorf("%q matches more than one folder (%s); rename one so the name is unique, then retry", name, strings.Join(dirs, ", "))
	}
	return nil
}

// Collision records a project name that resolves to more than one directory.
// Scan returns only the first (by scan order) and silently skips the rest, which
// is a real debugging trap (hull then operates on a directory you did not mean).
type Collision struct {
	Name string
	// Winner is the directory Scan actually resolves this name to (the rest are
	// silently shadowed). Taken from Scan itself so it always matches, including
	// the case where a plain folder outranks the managed copies.
	Winner string
	Dirs   []string
}

// Collisions reports project names found in more than one directory across the
// roots and individually-registered projects. It complements Scan (which hides
// the duplicates by design so a collision never breaks the listing) so a command
// or `hull doctor` can warn about the shadowed directories.
func Collisions(roots []string, extra ...string) []Collision {
	byName := map[string][]string{}
	_ = walk(roots, extra, func(p Project) {
		if !p.Unmanaged {
			byName[p.Name] = append(byName[p.Name], p.Dir)
		}
	})
	var out []Collision
	for name, dirs := range byName {
		if len(dirs) > 1 {
			sort.Strings(dirs)
			out = append(out, Collision{Name: name, Dirs: dirs})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	// Name the winner from Scan itself rather than re-deriving its tie-break
	// (roots order, then ReadDir order, then extras, and a plain folder can win),
	// so the reported winner can never drift from the directory Hull actually uses.
	winners := map[string]string{}
	if len(out) > 0 {
		if projects, err := Scan(roots, extra...); err == nil {
			for i := range projects {
				if _, ok := winners[projects[i].Name]; !ok {
					winners[projects[i].Name] = projects[i].Dir
				}
			}
		}
	}
	for i := range out {
		// Fall back to the first sorted dir if Scan was unavailable, so the winner
		// is never blank (Dirs always has at least two entries here).
		if w := winners[out[i].Name]; w != "" {
			out[i].Winner = w
		} else {
			out[i].Winner = out[i].Dirs[0]
		}
	}
	return out
}

// load classifies a single directory.
func load(dir, dirName string) (Project, bool) {
	if _, err := os.Lstat(filepath.Join(dir, manifest.Filename)); err == nil {
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
	// Inside a link to a project (hull move leaves one at the old path), the
	// link's target is the project.
	resolved, _ := filepath.EvalSymlinks(abs)
	for i := range projects {
		pdir, err := filepath.Abs(projects[i].Dir)
		if err != nil {
			continue
		}
		if abs == pdir || strings.HasPrefix(abs, pdir+string(filepath.Separator)) || (resolved != "" && Under(pdir, resolved)) {
			return &projects[i], true
		}
	}
	return nil, false
}

// Under reports whether dir is inside root (or is root itself), comparing
// cleaned absolute paths, ignoring case where the filesystem does (Windows,
// macOS). Used to decide whether an imported directory needs a standalone
// registration or is already covered by a parked root.
func Under(root, dir string) bool {
	r, err1 := filepath.Abs(root)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	r, d = filepath.Clean(r), filepath.Clean(d)
	if samePathText(r, d) {
		return true
	}
	sep := string(filepath.Separator)
	return len(d) > len(r)+1 && samePathText(d[:len(r)+1], r+sep)
}

// SamePath reports whether a and b name the same directory.
func SamePath(a, b string) bool { return Under(a, b) && Under(b, a) }

// samePathText compares path text the way this OS's filesystem does: case
// folds on Windows and macOS, and is exact on Linux, where Sites and sites are
// two folders.
func samePathText(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
