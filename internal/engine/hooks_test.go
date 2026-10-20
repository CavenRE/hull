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

// approve records the user's trust in p, as `hull allow` does.
func approve(t *testing.T, e *Engine, p *state.Project) *state.Project {
	t.Helper()
	if err := e.Trust(p); err != nil {
		t.Fatal(err)
	}
	return p
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
	p := approve(t, e, siteProject(home, "shop", manifest.Hooks{PostUp: []manifest.Hook{{Run: "echo hi"}}}))

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
	p := approve(t, e, siteProject(home, "once", manifest.Hooks{PostUp: []manifest.Hook{{Run: "migrate", When: "once"}}}))

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
	p := approve(t, e, siteProject(home, "ff", manifest.Hooks{PostUp: []manifest.Hook{{Run: "boom"}}}))
	if err := e.runHooks(context.Background(), p, "post_up", false); err == nil {
		t.Error("expected runHooks to fail when a hook errors")
	}

	// ignore_failure: the error is swallowed.
	p2 := approve(t, e, siteProject(home, "ig", manifest.Hooks{PostUp: []manifest.Hook{{Run: "boom", IgnoreFailure: true}}}))
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

func TestPreUpRunsInAOneOffContainerAfterRender(t *testing.T) {
	var calls []string
	e, root := testEngine(t)
	e.Run = func(_ context.Context, _, name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	p := writeSite(t, root, "pre", "hooks:\n  pre_up: [\"./prepare.sh\"]\n")
	approve(t, e, p)
	if err := e.Up(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 2 || !strings.Contains(calls[0], "run --rm --no-deps -T app sh -c ./prepare.sh") || !strings.HasSuffix(calls[1], "up -d") {
		t.Errorf("calls = %v; want the one-off pre_up first, then up", calls)
	}
}

func TestOnceStateSurvivesALaterFailure(t *testing.T) {
	var ran []string
	e, home := hookEngine(t, func(_ context.Context, _, _ string, args ...string) error {
		cmd := args[len(args)-1]
		ran = append(ran, cmd)
		if cmd == "boom" {
			return errors.New("boom")
		}
		return nil
	})
	p := approve(t, e, siteProject(home, "mix", manifest.Hooks{PostUp: []manifest.Hook{{Run: "migrate", When: "once"}, {Run: "boom"}}}))
	for i := 0; i < 2; i++ {
		if err := e.runHooks(context.Background(), p, "post_up", false); err == nil {
			t.Fatal("the failing hook should fail the event")
		}
	}
	if strings.Count(strings.Join(ran, ","), "migrate") != 1 {
		t.Errorf("ran = %v; the once hook must not repeat after a later hook failed", ran)
	}
}

func TestGatedHookWaitsThenRunsOnce(t *testing.T) {
	var cmds []string
	probes := 0
	e, home := hookEngine(t, func(_ context.Context, _, _ string, args ...string) error {
		cmd := args[len(args)-1]
		if cmd == "true" {
			probes++
			if probes < 2 {
				return errors.New("service not running")
			}
			return nil
		}
		cmds = append(cmds, cmd)
		return errors.New("migration failed")
	})
	p := approve(t, e, siteProject(home, "gate", manifest.Hooks{PostUp: []manifest.Hook{{Run: "migrate"}}}))
	if err := e.runHooks(context.Background(), p, "post_up", true); err == nil {
		t.Fatal("the failed migration should fail the event")
	}
	if probes != 2 || len(cmds) != 1 {
		t.Errorf("probes=%d runs=%v; want it to wait for the service, then run the hook once", probes, cmds)
	}
}

func TestHookServiceMustBeInTheProject(t *testing.T) {
	ran := 0
	e, home := hookEngine(t, func(context.Context, string, string, ...string) error { ran++; return nil })
	p := siteProject(home, "svc", manifest.Hooks{PostUp: []manifest.Hook{{Run: "true", Service: "mysql"}}})
	p.Manifest.Services = map[string]*manifest.Service{"mysql": {Engine: "mysql", Mode: manifest.ModeShared}}
	approve(t, e, p)
	if err := e.runHooks(context.Background(), p, "post_up", false); err == nil || !strings.Contains(err.Error(), `service "mysql" is not in svc`) {
		t.Errorf("a hook on a shared service = %v", err)
	}
	p.Manifest.Services["mysql"].Mode = manifest.ModeDedicated
	if err := e.runHooks(context.Background(), p, "post_up", false); err != nil || ran != 1 {
		t.Errorf("a hook on a dedicated service: err = %v, ran = %d", err, ran)
	}
}
