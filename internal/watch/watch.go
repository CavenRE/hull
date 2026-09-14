// Package watch notices source edits inside a project so Hull can clear that
// project's PHP opcode cache.
//
// Why this exists: on a Windows or macOS bind mount, having PHP re-check every
// cached script on each request is the single largest per-request cost, because
// each file operation crosses a slow filesystem boundary. Turning that check off
// removes the cost but leaves PHP serving compiled copies until something tells
// it to let go. Hull's daemon runs on the host, where filesystem events are
// cheap and immediate, so it can watch for edits there and reload the container
// instead. That gives the speed of not revalidating and the behaviour of
// revalidating.
//
// It watches only what a developer actually edits. Dependency and framework
// trees are skipped, which is both what makes the watch count small (a real
// WordPress project drops from 513 directories to 102) and a fair reflection of
// reality: people edit their own code, not vendor/ or wp-includes/.
package watch

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ignoredDirs are never watched. They are large, machine-generated, and not
// edited by hand. Skipping them is what keeps the watch count low enough to be
// free; editing something in here still works, it just needs `hull reload`.
var ignoredDirs = map[string]bool{
	"vendor":       true, // composer, and on Laravel it is a volume anyway
	"node_modules": true,
	".git":         true,
	"wp-includes":  true, // WordPress core
	"wp-admin":     true,
	"cache":        true,
	"tmp":          true,
	"dist":         true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

// watchedExts are the extensions worth a reload. The opcode cache only holds
// compiled PHP, so a CSS or image edit must not cost a reload.
var watchedExts = map[string]bool{
	".php": true,
}

// maxDirs bounds how many directories a single project may watch, so one
// pathological tree cannot exhaust handles. Real projects measured well under
// this; when it trips, the watch is still useful, just partial.
const maxDirs = 2000

// debounce is how long to wait for edits to settle before reloading. Editors
// write several events for a single save (and some write a temp file then
// rename), so reloading per event would reload many times per save.
const debounce = 300 * time.Millisecond

// Watcher reloads a project when its PHP sources change.
type Watcher struct {
	dir      string
	onChange func()

	fsw  *fsnotify.Watcher
	stop chan struct{}
	once sync.Once
	// dirs counts the directories watched so far, across the initial walk and
	// every subtree added later, so maxDirs bounds the watcher's lifetime rather
	// than each walk. Only touched before run() starts and then from run().
	dirs int
}

// skipDir reports whether a directory name should never be watched: the
// machine-generated trees above, and hidden directories, which hold editor and
// tool state rather than source. .hull is the exception, since a project's own
// Hull config lives there.
func skipDir(name string) bool {
	if ignoredDirs[name] {
		return true
	}
	return strings.HasPrefix(name, ".") && name != ".hull"
}

// New starts watching dir. onChange is called, debounced, whenever a watched
// PHP file changes. It never blocks the caller: watching happens on its own
// goroutine and errors are non-fatal, because a missing reload is a slow edit
// loop, not a broken site.
func New(dir string, onChange func()) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{dir: dir, onChange: onChange, fsw: fsw, stop: make(chan struct{})}
	w.addTree(dir)
	// A watcher covering nothing is worse than no watcher: it reports success and
	// then never fires, so an edit silently fails to appear and the reason is
	// invisible. Some filesystems simply cannot be watched from here (a WSL
	// distro reached over UNC answers ReadDirectoryChanges with "Incorrect
	// function"), and the caller has to be told rather than reassured.
	if w.dirs == 0 {
		_ = fsw.Close()
		return nil, fmt.Errorf("cannot watch %s: the filesystem does not report changes to this process", dir)
	}
	go w.run()
	return w, nil
}

// Dir reports which project directory this watcher covers.
func (w *Watcher) Dir() string { return w.dir }

// Close stops watching. Safe to call more than once.
func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.stop)
		_ = w.fsw.Close()
	})
}

// addTree walks dir and watches every directory that is not ignored. fsnotify
// watches a single directory per call, so a subtree means walking it; the
// ignore list is what keeps that cheap.
func (w *Watcher) addTree(dir string) {
	var walk func(string)
	walk = func(p string) {
		if w.dirs >= maxDirs {
			return
		}
		if err := w.fsw.Add(p); err != nil {
			return
		}
		w.dirs++
		entries, err := readDirNames(p)
		if err != nil {
			return
		}
		for _, name := range entries {
			if skipDir(name) {
				continue
			}
			walk(filepath.Join(p, name))
		}
	}
	walk(dir)
}

// run turns raw filesystem events into debounced reloads.
func (w *Watcher) run() {
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-w.stop:
			if timer != nil {
				timer.Stop()
			}
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			// A new directory needs its own watch, or edits inside it are missed.
			if event.Has(fsnotify.Create) && isDir(event.Name) && !skipDir(filepath.Base(event.Name)) {
				w.addTree(event.Name)
			}
			if !watchedExts[strings.ToLower(filepath.Ext(event.Name))] {
				continue
			}
			// Chmod alone does not change content, and Windows emits it liberally.
			if event.Op == fsnotify.Chmod {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(debounce)
			fire = timer.C

		case <-fire:
			fire = nil
			w.onChange()

		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			// Non-fatal: keep watching. A dropped event costs a stale page until
			// the next edit, not a broken project.
		}
	}
}
