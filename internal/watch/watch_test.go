package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// startWatcher watches dir and returns a channel of the changes it reports.
func startWatcher(t *testing.T, dir string, skip ...string) <-chan Change {
	t.Helper()
	changes := make(chan Change, 16)
	w, err := New(dir, skip, func(c Change) { changes <- c }, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return changes
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func next(t *testing.T, changes <-chan Change) Change {
	t.Helper()
	select {
	case c := <-changes:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no change reported")
		return Change{}
	}
}

func none(t *testing.T, changes <-chan Change) {
	t.Helper()
	select {
	case c := <-changes:
		t.Errorf("unexpected change %+v", c)
	case <-time.After(1 * time.Second):
	}
}

func TestWatcherReportsTheChangedPHPFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "wp-includes", "load.php"), "<?php\n")
	changes := startWatcher(t, dir)

	write(t, filepath.Join(dir, "index.php"), "<?php echo 1;\n")
	if c := next(t, changes); c.Reset || !slices.Equal(c.Files, []string{filepath.Join(dir, "index.php")}) {
		t.Errorf("change = %+v, want just index.php", c)
	}
	// Core files are watched too: a core update from the host must not leave
	// stale compiled copies behind.
	write(t, filepath.Join(dir, "wp-includes", "load.php"), "<?php echo 2;\n")
	if c := next(t, changes); !slices.Equal(c.Files, []string{filepath.Join(dir, "wp-includes", "load.php")}) {
		t.Errorf("change = %+v, want wp-includes/load.php", c)
	}
}

// Only PHP is in the opcode cache, so an asset edit must not cost anything.
func TestWatcherIgnoresNonPHP(t *testing.T) {
	dir := t.TempDir()
	changes := startWatcher(t, dir)
	for _, name := range []string{"style.css", "app.js", "logo.png"} {
		write(t, filepath.Join(dir, name), "x")
	}
	none(t, changes)
}

func TestWatcherIgnoresSkippedFolders(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"vendor", "node_modules", "wp-content/cache", ".git"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(sub)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	changes := startWatcher(t, dir, "vendor", "wp-content/cache")
	for _, sub := range []string{"vendor", "node_modules", "wp-content/cache", ".git"} {
		write(t, filepath.Join(dir, filepath.FromSlash(sub), "x.php"), "<?php\n")
	}
	none(t, changes)
}

// Laravel's bootstrap/cache and a block plugin's build/render.php are PHP a
// site runs, so folders with those names stay watched.
func TestRuntimePHPFoldersAreWatched(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "bootstrap", "cache", "config.php")
	write(t, config, "<?php return [];\n")
	changes := startWatcher(t, dir)

	render := filepath.Join(dir, "wp-content", "plugins", "blocks", "build", "render.php")
	write(t, render, "<?php\n")
	if c := next(t, changes); !slices.Contains(c.Files, render) {
		t.Errorf("change = %+v, want build/render.php", c)
	}
	// artisan config:clear deletes it, and the cache must forget it too.
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if c := next(t, changes); !c.Reset {
		t.Errorf("change = %+v, want a reset after bootstrap/cache/config.php went", c)
	}
}

// A site that writes a file on every request must not hold back the refresh
// of a save for as long as it is busy.
func TestBusySiteDoesNotPostponeASave(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "storage", "logs", "laravel.log")
	write(t, log, "")
	changes := startWatcher(t, dir)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				_ = os.WriteFile(log, []byte(fmt.Sprint(i)), 0o644)
			}
		}
	}()
	defer func() { close(stop); <-done }()
	time.Sleep(200 * time.Millisecond)
	saved := filepath.Join(dir, "app", "User.php")
	write(t, saved, "<?php\n")
	deadline := time.After(3 * time.Second)
	for {
		select {
		case c := <-changes:
			if slices.Contains(c.Files, saved) {
				return
			}
		case <-deadline:
			t.Fatal("the save was not reported while the site kept writing its log")
		}
	}
}

// One save often produces several events, so a burst must settle into one
// change.
func TestWatcherDebouncesBurst(t *testing.T) {
	dir := t.TempDir()
	changes := startWatcher(t, dir)
	for i := 0; i < 10; i++ {
		write(t, filepath.Join(dir, "a.php"), fmt.Sprintf("<?php // %d\n", i))
		time.Sleep(10 * time.Millisecond)
	}
	next(t, changes)
	none(t, changes)
}

// A new folder has to be covered too, or edits inside a brand new plugin are
// silently missed.
func TestWatcherPicksUpNewFolders(t *testing.T) {
	dir := t.TempDir()
	changes := startWatcher(t, dir)
	sub := filepath.Join(dir, "new-plugin")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	write(t, filepath.Join(sub, "plugin.php"), "<?php\n")
	if c := next(t, changes); !slices.Contains(c.Files, filepath.Join(sub, "plugin.php")) {
		t.Errorf("change = %+v, want the new plugin's file", c)
	}
}

// The cache cannot drop the entry of a file that is gone, so a deletion asks
// for a reset.
func TestDeletingAPHPFileAsksForAReset(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.php"), "<?php\n")
	changes := startWatcher(t, dir)
	if err := os.Remove(filepath.Join(dir, "old.php")); err != nil {
		t.Fatal(err)
	}
	if c := next(t, changes); !c.Reset {
		t.Errorf("change = %+v, want a reset", c)
	}
}

// A safe save (PhpStorm's default) renames the old file away and a temporary
// one into its place. It ends with the file present, so it is an edit, not a
// deletion, and must not cost a full reset.
func TestSafeSaveIsAnEdit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "functions.php")
	write(t, file, "<?php // v1\n")
	changes := startWatcher(t, dir)

	write(t, file+"___jb_tmp___", "<?php // v2\n")
	if err := os.Rename(file, file+"___jb_old___"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+"___jb_tmp___", file); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file + "___jb_old___"); err != nil {
		t.Fatal(err)
	}
	if c := next(t, changes); c.Reset || !slices.Equal(c.Files, []string{file}) {
		t.Errorf("change = %+v, want just an edit of functions.php", c)
	}
}

func TestRemovingAFolderAsksForAReset(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "wp-content", "plugins", "gone", "gone.php"), "<?php\n")
	changes := startWatcher(t, dir)
	if err := os.RemoveAll(filepath.Join(dir, "wp-content", "plugins", "gone")); err != nil {
		t.Fatal(err)
	}
	if c := next(t, changes); !c.Reset {
		t.Errorf("change = %+v, want a reset", c)
	}
}

// A PHP file that appears and goes within one batch (a tool's temp file) was
// never cached, so it costs nothing.
func TestShortLivedPHPFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	changes := startWatcher(t, dir)
	write(t, filepath.Join(dir, "tmp-123.php"), "<?php\n")
	if err := os.Remove(filepath.Join(dir, "tmp-123.php")); err != nil {
		t.Fatal(err)
	}
	none(t, changes)
}

// A file renamed over a cached one and then deleted leaves the old entry in
// the cache, so it needs a reset even though the batch saw it arrive.
func TestRenameOverThenDeleteResets(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fsnotify reports a rename into place as a plain create")
	}
	dir := t.TempDir()
	old := filepath.Join(dir, "old.php")
	write(t, old, "<?php // cached\n")
	changes := startWatcher(t, dir)
	write(t, filepath.Join(dir, "new.php"), "<?php // v2\n")
	time.Sleep(400 * time.Millisecond)
	<-changes
	if err := os.Rename(filepath.Join(dir, "new.php"), old); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if c := next(t, changes); !c.Reset {
		t.Errorf("change = %+v, want a reset", c)
	}
}

// A watcher whose folder disappears stops, says so, and must be replaced.
func TestWatcherReportsWhenItStops(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("inotify keeps a removed root's stream open")
	}
	dir := filepath.Join(t.TempDir(), "site")
	write(t, filepath.Join(dir, "index.php"), "<?php\n")
	w, err := New(dir, nil, func(Change) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for w.Alive() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if w.Alive() {
		t.Error("the watcher still claims to be alive after its folder went")
	}
}

// Windows watches the whole tree through one handle, so a big site has no
// folder limit past which edits go unseen.
func TestNoFolderLimitOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("one handle per tree is the Windows backend")
	}
	dir := t.TempDir()
	for i := 0; i < 2100; i++ {
		if err := os.MkdirAll(filepath.Join(dir, "plugins", fmt.Sprintf("p%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	changes := startWatcher(t, dir)
	last := filepath.Join(dir, "plugins", "p2099", "last.php")
	write(t, last, "<?php\n")
	if c := next(t, changes); !slices.Contains(c.Files, last) {
		t.Errorf("change = %+v, want the file in folder 2100", c)
	}
}

// A watcher that covers nothing must say so rather than report success and
// never fire.
func TestNewRefusesAnUnwatchableDirectory(t *testing.T) {
	w, err := New(filepath.Join(t.TempDir(), "does-not-exist"), nil, func(Change) {}, nil)
	if err == nil {
		w.Close()
		t.Fatal("New succeeded on a directory it cannot watch, want an error")
	}
}
