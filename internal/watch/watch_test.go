package watch

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// newTestWatcher starts a watcher over a temp dir and returns it plus a counter
// of how many times it fired.
func newTestWatcher(t *testing.T, dir string) (*Watcher, *atomic.Int32) {
	t.Helper()
	var fired atomic.Int32
	w, err := New(dir, func() { fired.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w, &fired
}

// waitFor polls until cond holds or the deadline passes, so the tests are not
// hostage to exact timing.
func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestWatcherFiresOnPHPChange(t *testing.T) {
	dir := t.TempDir()
	_, fired := newTestWatcher(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(func() bool { return fired.Load() > 0 }, 3*time.Second) {
		t.Fatal("editing a .php file should trigger a reload")
	}
}

// A reload costs about a second, so an asset edit must not pay for one. Only
// PHP is in the opcode cache.
func TestWatcherIgnoresNonPHP(t *testing.T) {
	dir := t.TempDir()
	_, fired := newTestWatcher(t, dir)

	for _, name := range []string{"style.css", "app.js", "logo.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1 * time.Second)
	if n := fired.Load(); n != 0 {
		t.Errorf("non-PHP edits should not reload, fired %d times", n)
	}
}

// Dependency trees are huge and machine-generated. Watching them is what would
// make this expensive, and nobody hand-edits them.
func TestWatcherIgnoresDependencyDirs(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"vendor", "node_modules", "wp-includes"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, fired := newTestWatcher(t, dir)

	for _, sub := range []string{"vendor", "node_modules", "wp-includes"} {
		if err := os.WriteFile(filepath.Join(dir, sub, "x.php"), []byte("<?php\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1 * time.Second)
	if n := fired.Load(); n != 0 {
		t.Errorf("edits under ignored dirs should not reload, fired %d times", n)
	}
}

// One save often produces several events (and some editors write then rename),
// so a burst must collapse into a single reload.
func TestWatcherDebouncesBurst(t *testing.T) {
	dir := t.TempDir()
	_, fired := newTestWatcher(t, dir)

	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, "a.php"), []byte("<?php // x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waitFor(func() bool { return fired.Load() > 0 }, 3*time.Second) {
		t.Fatal("burst should still produce a reload")
	}
	time.Sleep(500 * time.Millisecond)
	if n := fired.Load(); n > 2 {
		t.Errorf("a burst of edits should collapse into ~1 reload, got %d", n)
	}
}

// A newly created directory has to be watched too, or edits inside a brand new
// plugin or module are silently missed.
func TestWatcherPicksUpNewDirectories(t *testing.T) {
	dir := t.TempDir()
	_, fired := newTestWatcher(t, dir)

	sub := filepath.Join(dir, "new-plugin")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Give the watcher a moment to notice and watch the new directory.
	time.Sleep(400 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(sub, "plugin.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(func() bool { return fired.Load() > 0 }, 3*time.Second) {
		t.Error("edits in a newly created directory should trigger a reload")
	}
}

// A watcher that covers nothing must say so. It used to be created happily with
// zero directories watched, which reported success and then never fired: the
// exact shape of failure that is hardest to notice, because everything looks
// fine until an edit quietly does not appear.
func TestNewRefusesAnUnwatchableDirectory(t *testing.T) {
	w, err := New(filepath.Join(t.TempDir(), "does-not-exist"), func() {})
	if err == nil {
		w.Close()
		t.Fatal("New succeeded on a directory it cannot watch, want an error")
	}
}
