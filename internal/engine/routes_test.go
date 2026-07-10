package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/CavenRE/hull/internal/state"
)

func TestComputeRoutes(t *testing.T) {
	root := t.TempDir()
	writeManifest := func(name, content string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("site-a", "schema: 1\nname: site-a\ntemplate: laravel\n")
	writeManifest("stopped", "schema: 1\nname: stopped\ntemplate: plain\n")
	writeManifest("bigapp", `schema: 1
name: bigapp
type: app
containers:
  web:
    template: laravel
    domain: bigapp
  api:
    build: ./api
    domain: api.bigapp
    port: 3000
  worker:
    template: laravel
    command: work
`)

	projects, err := state.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	running := map[string]bool{"site-a": true, "bigapp": true}
	ports := func(ctx context.Context, p *state.Project, service string, containerPort int) (int, error) {
		switch fmt.Sprintf("%s/%d", service, containerPort) {
		case "app/8080":
			return 50001, nil
		case "web/8080":
			return 50002, nil
		case "api/3000":
			return 50003, nil
		}
		return 0, fmt.Errorf("no mapping for %s:%d", service, containerPort)
	}

	routes := ComputeRoutes(context.Background(), projects, "test", running, ports)
	got := map[string]string{}
	for _, r := range routes {
		got[r.Domain] = r.Upstream
	}
	want := map[string]string{
		"site-a.test":     "127.0.0.1:50001",
		"bigapp.test":     "127.0.0.1:50002",
		"api.bigapp.test": "127.0.0.1:50003",
	}
	if len(got) != len(want) {
		t.Fatalf("routes = %v, want %v", got, want)
	}
	for d, u := range want {
		if got[d] != u {
			t.Errorf("%s -> %s, want %s", d, got[d], u)
		}
	}
}

func TestComputeRoutesCluster(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "core"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ingress: hull, base_domain, an alias, and an internal-only route.
	write("served", `schema: 1
name: served
type: cluster
compose_root: core
base_domain: tapkit.local
ingress: hull
routes:
  api:
    service: management_api
    port: 8081
  t:
    service: edge_router
    port: 8080
    aliases: [tap]
  dash:
    service: dashboard
    port: 8080
`)
	// ingress: none -> the host router serves nothing for it.
	write("selfserved", `schema: 1
name: selfserved
type: cluster
compose_root: core
base_domain: x.local
routes:
  web:
    service: web
    port: 80
`)

	projects, err := state.Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	running := map[string]bool{"served": true, "selfserved": true}
	ports := func(ctx context.Context, p *state.Project, service string, containerPort int) (int, error) {
		switch service {
		case "management_api":
			return 60001, nil
		case "edge_router":
			return 60002, nil
		// dashboard is internal-only: no published port.
		}
		return 0, fmt.Errorf("no mapping for %s", service)
	}

	routes := ComputeRoutes(context.Background(), projects, "test", running, ports)
	got := map[string]string{}
	for _, r := range routes {
		got[r.Domain] = r.Upstream
	}
	want := map[string]string{
		"api.tapkit.local": "127.0.0.1:60001",
		"t.tapkit.local":   "127.0.0.1:60002",
		"tap.tapkit.local": "127.0.0.1:60002", // alias resolves to the same upstream
	}
	if len(got) != len(want) {
		t.Fatalf("cluster routes = %v, want %v", got, want)
	}
	for d, u := range want {
		if got[d] != u {
			t.Errorf("%s -> %s, want %s", d, got[d], u)
		}
	}
	// dashboard is internal-only (no port), so no route.
	if _, ok := got["dash.tapkit.local"]; ok {
		t.Error("internal-only dashboard should not get a live route")
	}

	// AllDomains: hull-mode hosts (incl the down dashboard + alias); the
	// ingress: none cluster contributes nothing.
	domains := map[string]bool{}
	for _, d := range AllDomains(projects, "test") {
		domains[d] = true
	}
	for _, d := range []string{"api.tapkit.local", "t.tapkit.local", "tap.tapkit.local", "dash.tapkit.local"} {
		if !domains[d] {
			t.Errorf("AllDomains missing %s", d)
		}
	}
	if domains["web.x.local"] {
		t.Error("ingress: none cluster must not contribute host domains")
	}
}
