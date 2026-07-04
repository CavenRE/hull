package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func TestSetClusterRoutePreservesComments(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "stack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `# top comment
schema: 1
name: stack
type: cluster
compose_root: core   # inline comment
routes:
  # existing route
  api:
    service: api_svc
    port: 8081
`
	if err := os.WriteFile(filepath.Join(dir, manifest.Filename), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &state.Project{Name: "stack", Dir: dir, Manifest: m}

	if err := e.SetClusterRoute(p, "web", ClusterRouteSpec{Service: "web_svc", Port: 80}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, manifest.Filename))
	for _, want := range []string{"# top comment", "# inline comment", "# existing route", "web_svc", "api_svc"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("after set, missing %q in:\n%s", want, got)
		}
	}

	m2, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Routes["web"] == nil || m2.Routes["web"].Service != "web_svc" || m2.Routes["api"] == nil {
		t.Fatalf("routes = %+v", m2.Routes)
	}
	// Subdomain defaults to the key on load (not written explicitly).
	if m2.Routes["web"].Subdomain != "web" {
		t.Errorf("web subdomain = %q, want web", m2.Routes["web"].Subdomain)
	}

	p.Manifest = m2
	if err := e.RemoveClusterRoute(p, "api"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, manifest.Filename))
	if !strings.Contains(string(after), "# top comment") {
		t.Errorf("rm dropped the top comment:\n%s", after)
	}
	if strings.Contains(string(after), "api_svc") {
		t.Errorf("rm did not remove the api route:\n%s", after)
	}
}

func TestClusterRouteRejectsNonCluster(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.Filename), []byte("schema: 1\nname: app\ntemplate: plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := manifest.Load(dir)
	p := &state.Project{Name: "app", Dir: dir, Manifest: m}
	if err := e.SetClusterRoute(p, "x", ClusterRouteSpec{Service: "s", Port: 80}); err == nil {
		t.Fatal("expected error setting a route on a non-cluster")
	}
}
