package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/dockerx"
)

// TestContainerRoutesRefuseWhenEngineDown pins the daemon half of the fix. The
// CLI guard used to sit inside the in-process branch of withDaemon, so with a
// daemon running every mutating verb reached docker unguarded and relayed a raw
// transport error. These routes must now refuse up front.
func TestContainerRoutesRefuseWhenEngineDown(t *testing.T) {
	s, client, _ := testServer(t)
	s.EngineCheck = func(ctx context.Context) error {
		return dockerx.ErrEngineDown
	}

	ctx := context.Background()
	if _, err := client.CreateProject(ctx, CreateProjectRequest{Name: "x", Template: "plain"}); err == nil {
		t.Error("create project should refuse with the engine down")
	} else if !strings.Contains(err.Error(), "not running") {
		t.Errorf("unhelpful error: %v", err)
	}
	if err := client.ProjectAction(ctx, "alpha", "start"); err == nil {
		t.Error("project action should refuse with the engine down")
	}
	if _, err := client.AddService(ctx, AddServiceRequest{Engine: "redis"}); err == nil {
		t.Error("service add should refuse with the engine down")
	}
}

// TestNonContainerRoutesWorkWhenEngineDown is the other half, and the reason
// the guard is per route rather than on every mutation: editing YAML must keep
// working with Docker closed, and status must keep answering so you can find
// out that Docker is closed.
func TestNonContainerRoutesWorkWhenEngineDown(t *testing.T) {
	s, client, _ := testServer(t)
	s.EngineCheck = func(ctx context.Context) error { return dockerx.ErrEngineDown }

	ctx := context.Background()
	if _, err := client.Status(ctx); err != nil {
		t.Errorf("status must answer with the engine down: %v", err)
	}
	if _, err := client.Config(ctx); err != nil {
		t.Errorf("config read must work with the engine down: %v", err)
	}
	if _, err := client.Projects(ctx); err != nil {
		t.Errorf("project list must work with the engine down: %v", err)
	}
}

// TestShutdownWorksWhenEngineDown: a broken Docker must never leave the user
// unable to stop Hull.
func TestShutdownWorksWhenEngineDown(t *testing.T) {
	s, client, _ := testServer(t)
	s.EngineCheck = func(ctx context.Context) error { return dockerx.ErrEngineDown }
	called := false
	s.OnShutdown = func() { called = true }

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown must work with the engine down: %v", err)
	}
	if !called {
		t.Error("shutdown handler not invoked")
	}
}

// TestEngineStateClassification checks what /v1/status reports, which is what
// lets `hull start` say "Hull is running, Docker is not" instead of implying
// everything is fine.
func TestEngineStateClassification(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{dockerx.ErrEngineDown, "stopped"},
		{dockerx.ErrEngineMissing, "missing"},
		{errors.New("something else"), "stopped"},
	}
	for _, c := range cases {
		got := classifyEngineErr(c.err)
		if got != c.want {
			t.Errorf("classify(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

var _ = http.StatusServiceUnavailable
