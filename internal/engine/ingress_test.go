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
	writeTestFile(t, filepath.Join(dir, "hull.yaml"), `schema: 1
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
`)
	writeTestFile(t, filepath.Join(core, "docker-compose.yml"), `name: tapkit
networks:
  tapkit_public: {}
  tapkit_internal: {}
services:
  management_api: { image: x }
  dashboard: { image: y }
`)

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := New(&config.Config{TLD: "test", Router: config.RouterConfig{Loopback: "127.0.0.1"}})
	// A macOS machine has only 127.0.0.1 until an alias is added, so the
	// address check must not depend on the machine running the test.
	e.HasLoopback = func(string) bool { return false }
	p := &state.Project{Name: "tapkit", Dir: dir, Manifest: m}
	if _, err := e.ClusterIngress(p, ""); err == nil || !strings.Contains(err.Error(), "ifconfig lo0 alias") {
		t.Errorf("a missing loopback address = %v, want the ifconfig hint", err)
	}
	e.HasLoopback = func(string) bool { return true }

	art, err := e.ClusterIngress(p, "")
	if err != nil {
		t.Fatal(err)
	}

	// Promote: replacing a proxy scales it to 0 in the overlay.
	promoted, err := e.ClusterIngress(p, "management_api")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(promoted.Overlay), "management_api:") || !strings.Contains(string(promoted.Overlay), "replicas: 0") {
		t.Errorf("promote overlay missing replicas:0 for the replaced proxy:\n%s", promoted.Overlay)
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

func TestIngressIPAvoidsOtherClusters(t *testing.T) {
	e, root := testEngine(t)
	mk := func(name string) *state.Project {
		dir := filepath.Join(root, name)
		mustWrite(t, dir, "schema: 1\nname: "+name+"\ntype: cluster\n")
		m, err := manifest.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		return &state.Project{Name: name, Dir: dir, Manifest: m}
	}
	alpha, beta := mk("alpha"), mk("beta")
	first, err := e.ingressIP(beta)
	if err != nil || first == "127.0.0.1" {
		t.Fatalf("ip = %q, %v", first, err)
	}
	// alpha's written overlay already holds beta's first choice.
	writeTestFile(t, filepath.Join(alpha.Dir, "compose.hull.yaml"), "ports:\n  - "+first+":80:80\n")
	if second, _ := e.ingressIP(beta); second == first {
		t.Errorf("beta got %s, which alpha already binds", second)
	}
	// beta's own written overlay pins its address.
	writeTestFile(t, filepath.Join(beta.Dir, "compose.hull.yaml"), "ports:\n  - 127.0.0.77:80:80\n")
	if got, _ := e.ingressIP(beta); got != "127.0.0.77" {
		t.Errorf("beta moved off its written address: %s", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
