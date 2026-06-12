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
	ports := func(ctx context.Context, dir, service string, containerPort int) (int, error) {
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
