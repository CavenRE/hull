package engine

import (
	"context"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// reloadScript resets the PHP opcode cache in place, without recreating the
// container. It detects the server rather than the image, so it covers both
// families Hull ships: the upstream wordpress image (Apache with mod_php, where
// a graceful restart recycles workers and drops the cache) and serversideup
// (PHP-FPM, whose master reloads on SIGUSR2). pgrep -o picks the oldest php-fpm
// process, which is the master; under s6 that is not PID 1.
//
// apachectl always warns that it cannot determine the server's FQDN, which is
// normal in a container and would read as an error, so its stderr is dropped. A
// genuine failure still surfaces as a non-zero exit.
const reloadScript = `if command -v apachectl >/dev/null 2>&1; then ` +
	`apachectl -k graceful 2>/dev/null && echo "reloaded apache (opcode cache cleared)"; ` +
	`elif pid=$(pgrep -o php-fpm 2>/dev/null); then ` +
	`kill -USR2 "$pid" && echo "reloaded php-fpm (opcode cache cleared)"; ` +
	`else echo "no reloadable PHP server found in this container" >&2; exit 1; fi`

// Reload clears a project's PHP opcode cache in place. It takes about a second,
// where recreating the container takes many, which is what makes it usable as
// the automatic answer to an edit.
func (e *Engine) Reload(ctx context.Context, p *state.Project, service string) error {
	if service == "" {
		service = "app"
	}
	return e.ExecIn(ctx, p, service, "sh", "-c", reloadScript)
}

// ReloadQuiet is Reload without a TTY, for the automatic path where a reload is
// routine and triggered by the daemon rather than typed by a person.
func (e *Engine) ReloadQuiet(ctx context.Context, p *state.Project) error {
	return e.composeFor(p).ExecNoTTY(ctx, "app", "sh", "-c", reloadScript)
}

// IsPHPProject reports whether a project runs on a PHP image, which is the only
// kind with an opcode cache worth reloading. Non-PHP runtimes either restart
// themselves on change or do not cache compiled code at all.
func IsPHPProject(p *state.Project) bool {
	if p.Manifest == nil {
		return false
	}
	def, ok := templates.Site(p.Manifest.Template)
	return ok && def.IsPHP()
}

// opcacheCLI is the in-container entry to Hull's opcode cache helper, which a
// container gets when the daemon watches its project (see compose.Render).
const opcacheCLI = templates.OpcacheHelperDir + "/hull-opcache-cli.php"

// maxPrecompile bounds one precompile below the 65000 files opcache.ini lets
// the cache hold.
const maxPrecompile = 60000

// RefreshQuiet drops the given files (host paths) from a project's opcode
// cache, so the next request recompiles them and nothing else.
func (e *Engine) RefreshQuiet(ctx context.Context, p *state.Project, files []string) error {
	paths := containerPaths(p, files)
	if len(paths) == 0 {
		return nil
	}
	_, err := e.opcache(ctx, p, "invalidate", paths)
	return err
}

// ResetQuiet empties a project's opcode cache from inside PHP. A container
// started before Hull mounted its helper falls back to reloading the server.
func (e *Engine) ResetQuiet(ctx context.Context, p *state.Project) error {
	if _, err := e.opcache(ctx, p, "reset", nil); err == nil {
		return nil
	}
	return e.ReloadQuiet(ctx, p)
}

// PrecompileQuiet compiles a project's PHP files into its opcode cache ahead of
// any request, so no page pays for compiling over a slow mount. It returns how
// many files it added; files already cached are skipped.
func (e *Engine) PrecompileQuiet(ctx context.Context, p *state.Project) (int, error) {
	out, err := e.opcache(ctx, p, "compile", containerPaths(p, phpSources(p)))
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "hull-opcache: ok compiled="); ok {
			k, _ := strconv.Atoi(v)
			n += k
		}
	}
	return n, err
}

func (e *Engine) opcache(ctx context.Context, p *state.Project, mode string, paths []string) (string, error) {
	pipe := e.Pipe
	if pipe == nil {
		pipe = dockerx.Pipe
	}
	c := e.composeFor(p)
	return pipe(ctx, c.Dir, strings.NewReader(strings.Join(paths, "\n")), "docker", c.Command("exec", "-T", "app", "php", opcacheCLI, mode)...)
}

// containerPaths maps a site's host files to where its app container sees
// them, dropping any outside the project.
func containerPaths(p *state.Project, files []string) []string {
	if p.Manifest == nil {
		return nil
	}
	def, ok := templates.Site(p.Manifest.Template)
	if !ok {
		return nil
	}
	var out []string
	for _, f := range files {
		rel, err := filepath.Rel(p.Dir, f)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, path.Join(def.MountTarget(), filepath.ToSlash(rel)))
	}
	return out
}

// WatchSkip lists a site's folders (relative, slash-separated) not worth
// watching: those a named volume covers, so an edit there never reaches PHP,
// and the template's runtime data folders.
func WatchSkip(p *state.Project) []string {
	if p.Manifest == nil {
		return nil
	}
	def, ok := templates.Site(p.Manifest.Template)
	if !ok {
		return nil
	}
	out := append([]string(nil), def.WatchIgnore...)
	for _, nv := range def.NamedVolumes {
		if rel, ok := strings.CutPrefix(nv.Path, def.MountTarget()+"/"); ok {
			out = append(out, rel)
		}
	}
	return out
}

// phpSources lists the PHP files worth compiling before the first request. It
// leaves out folders a named volume covers, dependency and runtime folders,
// tests, and Blade templates, which Laravel compiles itself.
func phpSources(p *state.Project) []string {
	skip := map[string]bool{}
	for _, d := range WatchSkip(p) {
		skip[d] = true
	}
	var files []string
	_ = filepath.WalkDir(p.Dir, func(f string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(p.Dir, f)
			if f != p.Dir && (skip[filepath.ToSlash(rel)] || noPrecompileDir(d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxPrecompile {
			return filepath.SkipAll
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".php") || strings.HasSuffix(name, ".blade.php") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() && info.Size() <= 4<<20 {
			files = append(files, f)
		}
		return nil
	})
	return files
}

func noPrecompileDir(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", "storage", "uploads", "cache", "tmp", "tests", "test", "fixtures":
		return true
	}
	return strings.HasPrefix(name, ".")
}
