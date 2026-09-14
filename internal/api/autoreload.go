package api

import (
	"context"
	"sync"
	"time"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/watch"
)

// autoReloadInterval is how often the daemon reconciles which projects it is
// watching. Projects come and go far less often than routes, so this is
// deliberately slower than the route sync: it only has to notice a project
// starting or stopping.
var autoReloadInterval = 10 * time.Second

// autoReloader keeps a filesystem watcher per running PHP project and clears
// that project's opcode cache when its sources change.
//
// This is what makes it safe to stop PHP revalidating files on every request.
// That revalidation is the largest per-request cost on a Windows or macOS bind
// mount, but turning it off alone would mean edits never appear. Watching from
// the host, where filesystem events are immediate, gives both: the speed of not
// revalidating, and edits showing up on the next refresh.
//
// Only PHP projects on a slow Windows mount are watched, because that is exactly
// the set Hull stops revalidating for. Other runtimes have no opcode cache worth
// clearing, and a project inside WSL is fast enough to revalidate normally (and
// cannot be watched from Windows anyway, since a UNC path reports no events).
type autoReloader struct {
	cfg    *config.Config
	eng    *engine.Engine
	logf   func(string, ...any)
	mu     sync.Mutex
	active map[string]*watch.Watcher // project name -> watcher
	// failed remembers projects whose watch could not be started, so a permanent
	// failure is reported once rather than on every reconcile.
	failed map[string]bool
}

func newAutoReloader(cfg *config.Config, eng *engine.Engine, logf func(string, ...any)) *autoReloader {
	return &autoReloader{cfg: cfg, eng: eng, logf: logf, active: map[string]*watch.Watcher{}, failed: map[string]bool{}}
}

// run reconciles until ctx is cancelled, then drops every watcher.
func (a *autoReloader) run(ctx context.Context) {
	ticker := time.NewTicker(autoReloadInterval)
	defer ticker.Stop()
	a.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			a.closeAll()
			return
		case <-ticker.C:
			a.reconcile(ctx)
		}
	}
}

// reconcile starts watchers for running PHP projects and stops them for
// projects that are no longer running, so the watch set follows what is up.
func (a *autoReloader) reconcile(ctx context.Context) {
	running, err := dockerx.RunningComposeProjects(ctx)
	if err != nil {
		return // docker is down; nothing to watch and nothing to clean up yet
	}
	up := map[string]bool{}
	for _, name := range running {
		up[name] = true
	}

	projects, err := state.Scan(a.cfg.Roots, a.cfg.Projects...)
	if err != nil {
		return
	}
	want := map[string]*state.Project{}
	for i := range projects {
		p := &projects[i]
		// Watch only what the watcher exists for. It is the half that makes it
		// safe to stop PHP revalidating files, and Hull only stops that on a
		// project whose files are on a slow Windows mount. A project inside WSL
		// is fast enough to revalidate normally, so watching it would buy
		// nothing, and it cannot be watched from here anyway.
		if up[p.Name] && engine.IsPHPProject(p) && platform.OnWindowsFilesystem(p.Dir) {
			want[p.Name] = p
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for name, w := range a.active {
		if _, ok := want[name]; !ok {
			w.Close()
			delete(a.active, name)
		}
	}
	for name, p := range want {
		if _, ok := a.active[name]; ok {
			continue
		}
		project := *p // captured by the callback, which outlives this loop
		w, err := watch.New(project.Dir, func() { a.reload(&project) })
		if err != nil {
			// Reconcile runs every few seconds, so a permanent failure would
			// otherwise fill the log forever. Say it once.
			if !a.failed[name] {
				a.failed[name] = true
				a.logf("auto-reload: cannot watch %s: %v", name, err)
			}
			continue
		}
		delete(a.failed, name)
		a.active[name] = w
		a.logf("auto-reload: watching %s (%s)", name, project.Dir)
	}
}

// reload clears one project's opcode cache. It uses its own timeout rather than
// the daemon's lifetime context so a wedged container cannot pin the watcher,
// and failures are logged rather than surfaced: a missed reload is a stale page
// until the next save, not a broken site.
func (a *autoReloader) reload(p *state.Project) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.eng.ReloadQuiet(ctx, p); err != nil {
		a.logf("auto-reload: %s: %v", p.Name, err)
		return
	}
	a.logf("auto-reload: %s reloaded after a source change", p.Name)
}

func (a *autoReloader) closeAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for name, w := range a.active {
		w.Close()
		delete(a.active, name)
	}
}
