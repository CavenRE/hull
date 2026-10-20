package api

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/watch"
)

type opcacheCalls struct {
	mu    sync.Mutex
	modes []string
	warms int
}

func (c *opcacheCalls) snapshot() ([]string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.modes...), c.warms
}

func testReloader(t *testing.T) (*autoReloader, *projectWatch, *opcacheCalls) {
	t.Helper()
	calls := &opcacheCalls{}
	eng := engine.New(&config.Config{TLD: "test", HullHome: t.TempDir()})
	eng.Pipe = func(_ context.Context, _ string, stdin io.Reader, _ string, args ...string) (string, error) {
		mode := args[len(args)-1]
		in, _ := io.ReadAll(stdin)
		calls.mu.Lock()
		calls.modes = append(calls.modes, mode+":"+strings.ReplaceAll(string(in), "\n", ","))
		calls.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		return "hull-opcache: ok " + mode + "\n", nil
	}
	a := newAutoReloader(func() *config.Config { return eng.Config }, eng, func(string, ...any) {})
	a.warm = func(context.Context, *state.Project) {
		calls.mu.Lock()
		calls.warms++
		calls.mu.Unlock()
	}
	dir := t.TempDir()
	pw := &projectWatch{p: state.Project{Name: "shop", Dir: dir, Manifest: &manifest.Manifest{Schema: 1, Name: "shop", Type: manifest.TypeSite, Template: "laravel"}}}
	pw.ctx, pw.cancel = context.WithCancel(context.Background())
	t.Cleanup(pw.cancel)
	return a, pw, calls
}

func waitCalls(t *testing.T, calls *opcacheCalls, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if modes, _ := calls.snapshot(); len(modes) >= n {
			return modes
		}
		time.Sleep(10 * time.Millisecond)
	}
	modes, _ := calls.snapshot()
	t.Fatalf("got %d opcache call(s) %v, want %d", len(modes), modes, n)
	return nil
}

func TestASaveRefreshesOnlyThatFile(t *testing.T) {
	a, pw, calls := testReloader(t)
	a.enqueue(pw, watch.Change{Files: []string{filepath.Join(pw.p.Dir, "app", "User.php")}})
	if modes := waitCalls(t, calls, 1); modes[0] != "invalidate:/var/www/html/app/User.php" {
		t.Errorf("calls = %v", modes)
	}
}

func TestADeletionResetsThenCompilesAndWarms(t *testing.T) {
	a, pw, calls := testReloader(t)
	a.enqueue(pw, watch.Change{Reset: true})
	modes := waitCalls(t, calls, 2)
	if !strings.HasPrefix(modes[0], "reset:") || !strings.HasPrefix(modes[1], "compile:") {
		t.Errorf("calls = %v, want a reset and then a compile", modes)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, warms := calls.snapshot(); warms == 0 && time.Now().Before(deadline); _, warms = calls.snapshot() {
		time.Sleep(10 * time.Millisecond)
	}
	if _, warms := calls.snapshot(); warms != 1 {
		t.Errorf("warmed %d time(s) after the compile, want 1", warms)
	}
}

// Saves that land while a refresh runs are merged into the next one, so a
// burst of saves never queues a refresh per save.
func TestChangesDuringARefreshAreMerged(t *testing.T) {
	a, pw, calls := testReloader(t)
	a.enqueue(pw, watch.Change{Files: []string{filepath.Join(pw.p.Dir, "a.php")}})
	time.Sleep(10 * time.Millisecond)
	a.enqueue(pw, watch.Change{Files: []string{filepath.Join(pw.p.Dir, "b.php")}})
	a.enqueue(pw, watch.Change{Files: []string{filepath.Join(pw.p.Dir, "b.php"), filepath.Join(pw.p.Dir, "c.php")}})
	waitCalls(t, calls, 2)
	time.Sleep(200 * time.Millisecond)
	if modes, _ := calls.snapshot(); len(modes) != 2 || modes[1] != "invalidate:/var/www/html/b.php,/var/www/html/c.php" {
		t.Errorf("calls = %v, want the two later saves merged and deduplicated", modes)
	}
}
