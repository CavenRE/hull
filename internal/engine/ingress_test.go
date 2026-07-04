package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func TestClusterIngressArtifacts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tapkit")
	core := filepath.Join(dir, "core")
	if err := os.MkdirAll(core, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(`schema: 1
name: tapkit
type: cluster
compose_root: core
base_domain: tapkit.local
ingress: delegate
routes:
  api:
    service: management_api
    port: 8081
  dash:
    service: dashboard
    port: 8080
`), 0o644)
	os.WriteFile(filepath.Join(core, "docker-compose.yml"), []byte(`name: tapkit
networks:
  tapkit_public: {}
  tapkit_internal: {}
services:
  management_api: { image: x }
  dashboard: { image: y }
`), 0o644)

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := New(&config.Config{TLD: "test", Router: config.RouterConfig{Loopback: "127.0.0.1"}})
	p := &state.Project{Name: "tapkit", Dir: dir, Manifest: m}

	art, err := e.ClusterIngress(p)
	if err != nil {
		t.Fatal(err)
	}

	// Networks discovered from the compose.
	if len(art.Networks) != 2 || art.Networks[0] != "tapkit_internal" || art.Networks[1] != "tapkit_public" {
		t.Errorf("networks = %v", art.Networks)
	}
	// Bind IP is a non-default loopback.
	if !strings.HasPrefix(art.BindIP, "127.0.0.") || art.BindIP == "127.0.0.1" {
		t.Errorf("bindIP = %q", art.BindIP)
	}
	// Caddyfile has a vhost per host, including the internal-only dashboard,
	// proxying by service name.
	for _, want := range []string{"api.tapkit.local {", "dash.tapkit.local {", "reverse_proxy management_api:8081", "reverse_proxy dashboard:8080", "tls internal"} {
		if !strings.Contains(art.Caddyfile, want) {
			t.Errorf("Caddyfile missing %q:\n%s", want, art.Caddyfile)
		}
	}

	// Overlay is valid YAML, adds the ingress service on both networks with a
	// host alias, and publishes on the bind IP.
	var doc struct {
		Services map[string]struct {
			Image    string                       `yaml:"image"`
			Networks map[string]struct{ Aliases []string } `yaml:"networks"`
			Ports    []string                     `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(art.Overlay, &doc); err != nil {
		t.Fatalf("overlay is not valid YAML: %v\n%s", err, art.Overlay)
	}
	ing, ok := doc.Services[IngressServiceName]
	if !ok {
		t.Fatalf("overlay missing %s service:\n%s", IngressServiceName, art.Overlay)
	}
	if _, ok := ing.Networks["tapkit_public"]; !ok {
		t.Error("ingress not attached to tapkit_public")
	}
	if len(ing.Networks["tapkit_public"].Aliases) == 0 {
		t.Error("ingress has no host aliases on tapkit_public")
	}
	foundPort := false
	for _, port := range ing.Ports {
		if strings.HasPrefix(port, art.BindIP+":443:") {
			foundPort = true
		}
	}
	if !foundPort {
		t.Errorf("overlay does not publish 443 on %s: %v", art.BindIP, ing.Ports)
	}
}

func TestClusterLoopbackDeterministic(t *testing.T) {
	a := clusterLoopback("tapkit", "127.0.0.1")
	b := clusterLoopback("tapkit", "127.0.0.1")
	if a != b {
		t.Errorf("clusterLoopback not deterministic: %q vs %q", a, b)
	}
	if a == "127.0.0.1" {
		t.Error("clusterLoopback must not pick 127.0.0.1")
	}
	if clusterLoopback("other", "127.0.0.1") == a {
		t.Log("note: two names hashed to the same octet (allocation is preview-grade)")
	}
}
