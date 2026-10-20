// Package watch notices source edits inside a project so Hull can refresh that
// project's PHP opcode cache.
//
// Why this exists: on a Windows or macOS bind mount, having PHP re-check every
// cached script on each request is the single largest per-request cost, because
// each file operation crosses a slow filesystem boundary. Turning that check off
// removes the cost but leaves PHP serving compiled copies until something tells
// it to let go. Hull's daemon runs on the host, where filesystem events are
// cheap and immediate, so it can watch for edits there and tell the container
// exactly which files changed. That gives the speed of not revalidating and the
// behaviour of revalidating, without recompiling the files nobody touched.
package watch

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Change is one settled batch of edits.
type Change struct {
	// Files are the PHP files (host paths) written or created in the batch:
	// the only entries the opcode cache has to drop.
	Files []string
	// Reset means a PHP file or a folder was deleted or renamed away, or the OS
	// lost events. The opcode cache cannot drop the entry of a path that no
	// longer exists, so it has to be emptied.
	Reset bool
}

// ignoredDirs are never watched: no site includes PHP from them, and their
// churn (an npm install) would flood the watcher. Folders such as cache or
// build stay watched, since Laravel's bootstrap/cache and a block plugin's
// build/render.php are PHP a site runs.
var ignoredDirs = map[string]bool{
	"node_modules": true,
}

// debounce is how long to wait for edits to settle. Editors write several
// events for a single save (and some write a temp file then rename it), so the
// batch is judged once the burst is over. maxBatch caps how long a batch can
// keep growing: a site that writes files on every request (sessions, logs)
// must not postpone a save's refresh for as long as it is busy.
const (
	debounce = 300 * time.Millisecond
	maxBatch = time.Second
)

type op uint8

const (
	opCreate op = 1 << iota
	opRenamedIn
	opWrite
	opGone
)

type event struct {
	path     string
	op       op
	overflow bool
}

// Watcher reports settled PHP source changes under one project directory.
type Watcher struct {
	dir      string
	skip     map[string]bool
	onChange func(Change)
	logf     func(string, ...any)

	events       chan event
	stop         chan struct{}
	done         chan struct{}
	once         sync.Once
	closeBackend func()
	// dirs holds the folders below dir, so a removal can be told apart: a
	// removed folder may have held cached scripts, a removed asset did not.
	dirs map[string]bool
	dead atomic.Bool
}

// New starts watching dir. skip lists folders (relative, slash-separated) the
// container does not read from dir, such as a vendor/ a named volume covers.
// onChange runs on the watcher's goroutine for each settled batch, so it must
// not block. A missing refresh is a stale page, never a broken site, so errors
// after start are not fatal.
func New(dir string, skip []string, onChange func(Change), logf func(string, ...any)) (*Watcher, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	w := &Watcher{
		dir:      dir,
		skip:     map[string]bool{},
		onChange: onChange,
		logf:     logf,
		events:   make(chan event, 256),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		dirs:     map[string]bool{},
	}
	for _, s := range skip {
		w.skip[path.Clean(s)] = true
	}
	closeBackend, err := startBackend(w)
	if err != nil {
		return nil, err
	}
	w.closeBackend = closeBackend
	w.addDirs(dir)
	go w.run()
	return w, nil
}

// Dir reports which project directory this watcher covers.
func (w *Watcher) Dir() string { return w.dir }

// Close stops watching. Safe to call more than once.
func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.stop)
		w.closeBackend()
		<-w.done
	})
}

// Alive reports whether the watcher still receives changes. A dead one must be
// replaced: edits from then on go unseen.
func (w *Watcher) Alive() bool { return !w.dead.Load() }

// fail records that the backend stopped, and asks for a reset for what it may
// have missed.
func (w *Watcher) fail(err error) {
	w.logf("watch: %s stopped reporting changes: %v", w.dir, err)
	w.dead.Store(true)
	w.emit(event{overflow: true})
}

// emit hands an event from the backend to run, giving up once Close starts.
func (w *Watcher) emit(ev event) bool {
	select {
	case w.events <- ev:
		return true
	case <-w.stop:
		return false
	}
}

// skipped reports whether p lies in an ignored folder: one named in
// ignoredDirs, a hidden one (except .hull, a project's own Hull config), or a
// folder the caller skips.
func (w *Watcher) skipped(p string) bool {
	rel, err := filepath.Rel(w.dir, p)
	if err != nil || rel == "." {
		return false
	}
	rel = filepath.ToSlash(rel)
	for prefix := range w.skip {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	for _, name := range strings.Split(rel, "/") {
		if ignoredDirs[name] || (strings.HasPrefix(name, ".") && name != ".hull") {
			return true
		}
	}
	return false
}

func (w *Watcher) addDirs(root string) {
	if w.skipped(root) {
		return
	}
	if root != w.dir {
		w.dirs[root] = true
	}
	names, err := readDirNames(root)
	if err != nil {
		return
	}
	for _, name := range names {
		w.addDirs(filepath.Join(root, name))
	}
}

// forgetDir drops p and the folders below it, and reports whether p was a
// folder.
func (w *Watcher) forgetDir(p string) bool {
	if !w.dirs[p] {
		return false
	}
	below := p + string(filepath.Separator)
	for d := range w.dirs {
		if d == p || strings.HasPrefix(d, below) {
			delete(w.dirs, d)
		}
	}
	return true
}

// run collects events into batches and reports each once it settles.
func (w *Watcher) run() {
	defer close(w.done)
	// pending maps a path to whether its first event in the batch created it.
	pending := map[string]bool{}
	lost := false
	var started time.Time
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-w.stop:
			if timer != nil {
				timer.Stop()
			}
			return
		case ev := <-w.events:
			if !ev.overflow && w.skipped(ev.path) {
				continue
			}
			if len(pending) == 0 && !lost {
				started = time.Now()
			}
			if ev.overflow {
				lost = true
			} else {
				if ev.op&(opCreate|opRenamedIn) != 0 && isDir(ev.path) {
					w.addDirs(ev.path)
				}
				if _, seen := pending[ev.path]; !seen {
					pending[ev.path] = ev.op&opCreate != 0
				}
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(min(debounce, max(maxBatch-time.Since(started), 0)))
			fire = timer.C
		case <-fire:
			fire = nil
			ch := w.settle(pending, lost)
			pending, lost = map[string]bool{}, false
			if ch.Reset || len(ch.Files) > 0 {
				w.onChange(ch)
			}
		}
	}
}

// settle judges a batch by what is on disk once it is over, not by the events
// on the way: a save that renames the old file away and a new one into place
// ends with the file present, which is an edit, not a deletion.
func (w *Watcher) settle(pending map[string]bool, lost bool) Change {
	ch := Change{Reset: lost}
	for p, created := range pending {
		if info, err := os.Stat(p); err == nil {
			if info.Mode().IsRegular() && isPHP(p) {
				ch.Files = append(ch.Files, p)
			}
			continue
		}
		wasDir := w.forgetDir(p)
		// A path created and removed within one batch was never cached. A
		// rename into place is not a creation: it may have replaced a file
		// that was.
		if !created && (wasDir || isPHP(p)) {
			ch.Reset = true
		}
	}
	sort.Strings(ch.Files)
	return ch
}

// isPHP reports whether PHP may have cached p: the extensions PHP code is
// included from.
func isPHP(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".php", ".phtml", ".inc":
		return true
	}
	return false
}
