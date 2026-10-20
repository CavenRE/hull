//go:build !windows

package watch

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// maxDirs bounds how many folders one project may watch: here each folder
// needs its own watch, and one pathological tree must not exhaust them. When it
// trips, the watch is still useful, just partial.
const maxDirs = 2000

type fsBackend struct {
	w      *Watcher
	fsw    *fsnotify.Watcher
	capped bool
}

func startBackend(w *Watcher) (func(), error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	b := &fsBackend{w: w, fsw: fsw}
	b.addTree(w.dir)
	// A watcher covering nothing would report success and never fire, so an
	// edit silently fails to appear: the caller has to be told instead.
	if len(fsw.WatchList()) == 0 {
		_ = fsw.Close()
		return nil, fmt.Errorf("cannot watch %s: the filesystem does not report changes to this process", w.dir)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.run()
	}()
	return func() {
		_ = fsw.Close()
		<-done
	}, nil
}

func (b *fsBackend) addTree(dir string) {
	if b.w.skipped(dir) {
		return
	}
	if len(b.fsw.WatchList()) >= maxDirs {
		if !b.capped {
			b.w.logf("watch: %s has more than %d folders to watch; edits in the rest need `hull reload`", b.w.dir, maxDirs)
		}
		b.capped = true
		return
	}
	if err := b.fsw.Add(dir); err != nil {
		return
	}
	names, err := readDirNames(dir)
	if err != nil {
		return
	}
	for _, name := range names {
		b.addTree(filepath.Join(dir, name))
	}
}

func (b *fsBackend) run() {
	for {
		select {
		case <-b.w.stop:
			return
		case e, ok := <-b.fsw.Events:
			if !ok {
				b.closed()
				return
			}
			ev := event{path: e.Name}
			switch {
			case e.Has(fsnotify.Create):
				ev.op = opCreate
				if isDir(e.Name) {
					b.addTree(e.Name)
					// Files made in the new folder before its watch existed sent
					// no event of their own.
					if !b.emitTree(e.Name) {
						return
					}
				}
			case e.Has(fsnotify.Remove), e.Has(fsnotify.Rename):
				ev.op = opGone
				_ = b.fsw.Remove(e.Name)
			case e.Has(fsnotify.Write):
				ev.op = opWrite
			default:
				continue // chmod alone does not change content
			}
			if !b.w.emit(ev) {
				return
			}
		case err, ok := <-b.fsw.Errors:
			if !ok {
				b.closed()
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) && !b.w.emit(event{overflow: true}) {
				return
			}
		}
	}
}

// closed reports an event stream that ended without Close, which leaves the
// project unwatched.
func (b *fsBackend) closed() {
	select {
	case <-b.w.stop:
	default:
		b.w.fail(errors.New("the event stream closed"))
	}
}

func (b *fsBackend) emitTree(dir string) bool {
	ok := true
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir():
			if b.w.skipped(p) {
				return filepath.SkipDir
			}
			return nil
		case !b.w.emit(event{path: p, op: opCreate}):
			ok = false
			return filepath.SkipAll
		}
		return nil
	})
	return ok
}
