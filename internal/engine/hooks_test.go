package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func hookEngine(t *testing.T, run func(ctx context.Context, dir, name string, args ...string) error) (*Engine, string) {
	t.Helper()
	home := t.TempDir()
	e := New(&config.Config{TLD: "test", HullHome: home})
	e.Run = run
	return e, home
}

func siteProject(home, name string, hooks manifest.Hooks) *state.Project {
	return &state.Project{
		Name: name,
		Dir:  filepath.Join(home, name),
		Manifest: &manifest.Manifest{
			Schema: 1, Name: name, Type: manifest.TypeSite, Template: "plain", Hooks: hooks,
		},
	}
}

func TestRunHooksExecsInDefaultService(t *testing.T) {
	var calls [][]string
	e, home := hookEngine(t, func(_ context.Context, _, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	})
	p := siteProject(home, "shop", manifest.Hooks{PostUp: []manifest.Hook{{Run: "echo hi"}}})

	if err := e.runHooks(context.Background(), p, "post_up", false); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 exec, got %d: %v", len(calls), calls)
	}
	got := strings.Join(calls[0], " ")
	for _, want := range []string{"compose", "-p shop", "exec", "-T", "app", "sh", "-c", "echo hi"} {
		if !strings.Contains(got, want) {
			t.Errorf("exec missing %q in: %s", want, got)
		}
	}
}

func TestRunHooksWhenOnceSkipsSecondRun(t *testing.T) {
	n := 0
	e, home := hookEngine(t, func(context.Context, string, string, ...string) error { n++; return nil })
	p := siteProject(home, "once", manifest.Hooks{PostUp: []manifest.Hook{{Run: "migrate", When: "once"}}})

	for i := 0; i < 3; i++ {
		if err := e.runHooks(context.Background(), p, "post_up", false); err != nil {
			t.Fatal(err)
		}
	}
	if n != 1 {
		t.Errorf("when:once ran %d times, want 1", n)
	}
}

func TestRunHooksFailFastAndIgnoreFailure(t *testing.T) {
	e, home := hookEngine(t, func(context.Context, string, string, ...string) error {
		return errors.New("boom")
	})

	// Default: a failing hook aborts the event.
	p := siteProject(home, "ff", manifest.Hooks{PostUp: []manifest.Hook{{Run: "boom"}}})
	if err := e.runHooks(context.Background(), p, "post_up", false); err == nil {
		t.Error("expected runHooks to fail when a hook errors")
	}

	// ignore_failure: the error is swallowed.
	p2 := siteProject(home, "ig", manifest.Hooks{PostUp: []manifest.Hook{{Run: "boom", IgnoreFailure: true}}})
	if err := e.runHooks(context.Background(), p2, "post_up", false); err != nil {
		t.Errorf("ignore_failure should swallow the error, got %v", err)
	}
}

func TestRunHooksClusterRequiresService(t *testing.T) {
	e, home := hookEngine(t, func(context.Context, string, string, ...string) error { return nil })
	p := &state.Project{
		Name: "stack", Dir: filepath.Join(home, "stack"),
		Manifest: &manifest.Manifest{
			Schema: 1, Name: "stack", Type: manifest.TypeCluster,
			Hooks: manifest.Hooks{PostUp: []manifest.Hook{{Run: "migrate"}}}, // no service
		},
	}
	if err := e.runHooks(context.Background(), p, "post_up", false); err == nil {
		t.Error("a cluster hook without a service should error")
	}
}
