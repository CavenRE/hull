package api

import (
	"context"
	"slices"
	"strings"
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

// precompileTimeout bounds one ahead-of-time compile of a project. A large
// WordPress site compiles thousands of files over the slow mount.
const precompileTimeout = 10 * time.Minute

var (
	precompileAttempts = 24
	precompileRetry    = 5 * time.Second
)

// autoReloader keeps a filesystem watcher per running PHP project and keeps
// that project's opcode cache in step with its sources.
//
// This is what makes it safe to stop PHP revalidating files on every request.
// That revalidation is the largest per-request cost on a Windows or macOS bind
// mount, but turning it off alone would mean edits never appear. Watching from
// the host, where filesystem events are immediate, gives both: the speed of not
// revalidating, and edits showing up on the next refresh. A save drops just the
// saved files from the cache; only a deletion or rename empties it, and then
// the whole project compiles again in the background, as it does when the
// watch starts, so no page anyone opens pays for compiling over the mount.
//
// Only PHP projects on a slow Windows mount are watched, because that is exactly
// the set Hull stops revalidating for. Other runtimes have no opcode cache worth
// clearing, and a project inside WSL is fast enough to revalidate normally (and
// cannot be watched from Windows anyway, since a UNC path reports no events).
type autoReloader struct {
	cfg    func() *config.Config
	eng    *engine.Engine
	logf   func(string, ...any)
	warm   func(ctx context.Context, p *state.Project)
	mu     sync.Mutex
	active map[string]*projectWatch // project name -> watch
	// failed remembers projects whose watch could not be started, so a permanent
	// failure is reported once rather than on every reconcile.
	failed map[string]bool
}

// projectWatch is one running project's watcher and the cache work it queues:
// refreshes run in order on one goroutine, and a background compile, which a
// reset restarts, runs beside them.
type projectWatch struct {
	p      state.Project
	w      *watch.Watcher
	ctx    context.Context
	cancel context.CancelFunc

	mu           sync.Mutex
	queued       *watch.Change
	busy         bool
	stopCompile  context.CancelFunc
	compileNoted bool
}

func newAutoReloader(cfg func() *config.Config, eng *engine.Engine, logf func(string, ...any)) *autoReloader {
	return &autoReloader{cfg: cfg, eng: eng, logf: logf, warm: eng.WarmQuiet, active: map[string]*projectWatch{}, failed: map[string]bool{}}
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

	cfg := a.cfg()
	projects, _ := state.Scan(cfg.Roots, cfg.Projects...) // a bad root must not stop the others' watchers
	want := map[string]*state.Project{}
	for i := range projects {
		p := &projects[i]
		// Watch only what the watcher exists for. It is the half that makes it
		// safe to stop PHP revalidating files, and Hull only stops that on a
		// project whose files are on a slow Windows mount. A project inside WSL
		// is fast enough to revalidate normally, so watching it would buy
		// nothing, and it cannot be watched from here anyway.
		if up[p.Name] && engine.IsPHPProject(p) && platform.SeesWindowsEdits(p.Dir) {
			want[p.Name] = p
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for name, pw := range a.active {
		// A project moved to another folder needs a watch on its new one, and a
		// watcher that stopped reporting needs replacing.
		if p, ok := want[name]; !ok || p.Dir != pw.p.Dir || !pw.w.Alive() {
			pw.close()
			delete(a.active, name)
		}
	}
	for name, p := range want {
		if _, ok := a.active[name]; ok {
			continue
		}
		pw := &projectWatch{p: *p}
		pw.ctx, pw.cancel = context.WithCancel(context.Background())
		w, err := watch.New(p.Dir, engine.WatchSkip(p), func(ch watch.Change) { a.enqueue(pw, ch) }, a.logf)
		if err != nil {
			pw.cancel()
			// Reconcile runs every few seconds, so a permanent failure would
			// otherwise fill the log forever. Say it once.
			if !a.failed[name] {
				a.failed[name] = true
				a.logf("auto-reload: cannot watch %s: %v", name, err)
			}
			continue
		}
		pw.w = w
		delete(a.failed, name)
		a.active[name] = pw
		a.logf("auto-reload: watching %s (%s)", name, p.Dir)
		// Nothing watched the project before now (the daemon was down, or the
		// last watcher died), so the cache may hold copies of files edited since.
		a.enqueue(pw, watch.Change{Reset: true})
	}
}

// enqueue merges a change into the project's queue. It runs on the watcher's
// goroutine, so it only hands the work over.
func (a *autoReloader) enqueue(pw *projectWatch, ch watch.Change) {
	pw.mu.Lock()
	if pw.queued == nil {
		pw.queued = &watch.Change{}
	}
	pw.queued.Files = append(pw.queued.Files, ch.Files...)
	pw.queued.Reset = pw.queued.Reset || ch.Reset
	start := !pw.busy
	pw.busy = true
	pw.mu.Unlock()
	if start {
		go a.drain(pw)
	}
}

func (a *autoReloader) drain(pw *projectWatch) {
	for {
		pw.mu.Lock()
		ch := pw.queued
		pw.queued = nil
		if ch == nil {
			pw.busy = false
			pw.mu.Unlock()
			return
		}
		pw.mu.Unlock()
		a.apply(pw, *ch)
	}
}

// apply brings the cache in step with one change. Failures are logged rather
// than surfaced: a missed refresh is a stale page until the next save, not a
// broken site.
func (a *autoReloader) apply(pw *projectWatch, ch watch.Change) {
	if pw.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(pw.ctx, 30*time.Second)
	defer cancel()
	p := &pw.p
	if !ch.Reset {
		files := slices.Compact(slices.Sorted(slices.Values(ch.Files)))
		err := a.eng.RefreshQuiet(ctx, p, files)
		if err == nil {
			a.logf("auto-reload: %s: refreshed %d file(s)", p.Name, len(files))
			return
		}
		a.logf("auto-reload: %s: cannot refresh single files (%v); clearing the cache", p.Name, err)
	}
	// A container still booting has nothing cached yet, so a failed reset is
	// logged, and the compile, which waits for the container, still runs.
	if err := a.eng.ResetQuiet(ctx, p); err != nil {
		a.logf("auto-reload: %s: cache not cleared: %v", p.Name, err)
	} else {
		a.logf("auto-reload: %s: cache cleared", p.Name)
	}
	a.precompile(pw)
}

// precompile compiles the project ahead of requests in the background and
// then makes one request, restarting a compile that a reset made pointless.
func (a *autoReloader) precompile(pw *projectWatch) {
	pw.mu.Lock()
	if pw.stopCompile != nil {
		pw.stopCompile()
	}
	ctx, cancel := context.WithTimeout(pw.ctx, precompileTimeout)
	pw.stopCompile = cancel
	pw.mu.Unlock()
	go func() {
		defer cancel()
		start := time.Now()
		var n int
		var err error
		// A container that has just started may not serve PHP yet (a first boot
		// can run composer install), so a failure is retried for a while. One
		// created before Hull mounted its helper never will.
		for attempt := 1; ; attempt++ {
			n, err = a.eng.PrecompileQuiet(ctx, &pw.p)
			if err == nil || attempt == precompileAttempts || strings.Contains(err.Error(), "Could not open input file") {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(precompileRetry):
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			pw.mu.Lock()
			noted := pw.compileNoted
			pw.compileNoted = true
			pw.mu.Unlock()
			if noted {
				return
			}
			if strings.Contains(err.Error(), "Could not open input file") {
				a.logf("auto-reload: %s: its container predates Hull's cache helper; `hull restart %s` adds it", pw.p.Name, pw.p.Name)
			} else {
				a.logf("auto-reload: %s: cannot compile ahead of requests: %v", pw.p.Name, err)
			}
			return
		}
		if n > 0 {
			a.logf("auto-reload: %s: compiled %d PHP file(s) ahead of requests in %s", pw.p.Name, n, time.Since(start).Round(time.Second))
		}
		a.warm(ctx, &pw.p)
	}()
}

func (pw *projectWatch) close() {
	pw.w.Close()
	pw.cancel()
}

func (a *autoReloader) closeAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for name, pw := range a.active {
		pw.close()
		delete(a.active, name)
	}
}
