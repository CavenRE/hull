package compose

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/manifest"
)

var update = flag.Bool("update", false, "rewrite golden compose.yaml files")

// testCtx pins machine-dependent values so goldens are stable everywhere.
var testCtx = Context{TLD: "test", HullHome: "/home/test/.hull"}

func goldenDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("testdata", "golden", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no golden cases found under testdata/golden")
	}
	return dirs
}

func renderCase(t *testing.T, dir string) []byte {
	t.Helper()
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatalf("rendering %s: %v", dir, err)
	}
	data, err := Marshal(f)
	if err != nil {
		t.Fatalf("marshaling %s: %v", dir, err)
	}
	return data
}

func TestGolden(t *testing.T) {
	for _, dir := range goldenDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			got := renderCase(t, dir)
			golden := filepath.Join(dir, "compose.yaml")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden file (run: go test ./internal/compose -update): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("rendered output differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
			}
		})
	}
}

// TestRenderDeterministic guards against map-iteration order leaking into
// output — golden tests would flake before users ever saw it, but fail fast
// and clearly here.
func TestRenderDeterministic(t *testing.T) {
	dir := filepath.Join("testdata", "golden", "app-multi")
	first := renderCase(t, dir)
	for i := 0; i < 30; i++ {
		if next := renderCase(t, dir); !bytes.Equal(first, next) {
			t.Fatalf("render is not deterministic (iteration %d)", i)
		}
	}
}

// TestGoldenOutputIsValidCompose re-parses every golden as generic YAML and
// checks the invariants the compose spec cares about (string-typed env
// entries, declared networks, expected top-level shape).
func TestGoldenOutputIsValidCompose(t *testing.T) {
	for _, dir := range goldenDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			var doc struct {
				Name     string                       `yaml:"name"`
				Services map[string]map[string]any    `yaml:"services"`
				Networks map[string]map[string]any    `yaml:"networks"`
				Volumes  map[string]map[string]any    `yaml:"volumes"`
				Extra    map[string]map[string]string `yaml:",inline"`
			}
			data := renderCase(t, dir)
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatalf("output is not valid YAML: %v", err)
			}
			if doc.Name == "" {
				t.Error("missing top-level compose project name")
			}
			if len(doc.Services) == 0 {
				t.Fatal("no services rendered")
			}
			for name, svc := range doc.Services {
				for _, field := range []string{"environment", "labels", "volumes", "networks", "extra_hosts"} {
					raw, ok := svc[field]
					if !ok {
						continue
					}
					list, ok := raw.([]any)
					if !ok {
						t.Errorf("service %s: %s is %T, want list", name, field, raw)
						continue
					}
					for _, item := range list {
						if _, ok := item.(string); !ok {
							t.Errorf("service %s: %s entry %v is %T, want string", name, field, item, item)
						}
					}
				}
				if err := checkCaddyNetworkDeclared(svc, doc.Networks); err != nil {
					t.Errorf("service %s: %v", name, err)
				}
			}
		})
	}
}

func checkCaddyNetworkDeclared(svc map[string]any, networks map[string]map[string]any) error {
	raw, ok := svc["networks"].([]any)
	if !ok {
		return nil
	}
	for _, n := range raw {
		if n == "caddy" {
			net, declared := networks["caddy"]
			if !declared {
				return fmt.Errorf("joins caddy network but it is not declared")
			}
			if ext, _ := net["external"].(bool); !ext {
				return fmt.Errorf("caddy network must be external")
			}
		}
	}
	return nil
}

// TestPlainTemplateOnDefaultNetwork locks in the v1 bug fix: plain sites
// must join the default network so later-added databases are reachable.
func TestPlainTemplateOnDefaultNetwork(t *testing.T) {
	got := string(renderCase(t, filepath.Join("testdata", "golden", "plain-mariadb")))
	m, err := manifest.Load(filepath.Join("testdata", "golden", "plain-mariadb"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	app := f.Services["app"]
	if app == nil {
		t.Fatalf("no app service in output:\n%s", got)
	}
	hasDefault := false
	for _, n := range app.Networks {
		if n == "default" {
			hasDefault = true
		}
	}
	if !hasDefault {
		t.Errorf("plain app service missing default network (v1 bug regression): %v", app.Networks)
	}
}

// TestSharedServicesNotRendered locks in that shared-mode services produce
// no project-local containers.
func TestSharedServicesNotRendered(t *testing.T) {
	m, err := manifest.Load(filepath.Join("testdata", "golden", "laravel-shared-services"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 1 {
		t.Errorf("expected only the app service, got %d services", len(f.Services))
	}
	if len(f.Volumes) != 0 {
		t.Errorf("expected no volumes for shared services, got %v", f.Volumes)
	}
}
