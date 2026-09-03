package compose

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// output , golden tests would flake before users ever saw it, but fail fast
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
				for _, field := range []string{"environment", "labels", "volumes", "networks", "extra_hosts", "ports"} {
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

// TestLinuxIDRemap locks in the native-Linux bind-mount fix: when a host
// identity is supplied, serversideup/php sites start as root and wrap the
// entrypoint to remap www-data to the host uid/gid. Without it (the default
// testCtx, mirroring macOS/Windows) the service is untouched.
func TestLinuxIDRemap(t *testing.T) {
	m, err := manifest.Load(filepath.Join("testdata", "golden", "laravel-postgres"))
	if err != nil {
		t.Fatal(err)
	}

	// No host identity → no remap (keeps goldens/Docker Desktop behaviour).
	plain, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	if u := plain.Services["app"].User; u != "" {
		t.Errorf("expected no user override without HostUID, got %q", u)
	}

	// Host identity → root + set-id entrypoint wrapper.
	linux := testCtx
	linux.HostUID, linux.HostGID = "1000", "1000"
	f, err := Render(m, linux)
	if err != nil {
		t.Fatal(err)
	}
	app := f.Services["app"]
	if app.User != "0:0" {
		t.Errorf("expected user 0:0 for id-remap, got %q", app.User)
	}
	joined := strings.Join(app.Entrypoint, " ")
	if !strings.Contains(joined, "docker-php-serversideup-set-id www-data 1000:1000") {
		t.Errorf("entrypoint missing set-id remap: %v", app.Entrypoint)
	}
	if app.Command != "/init" {
		t.Errorf("expected command /init to hand off to the image entrypoint, got %q", app.Command)
	}
}

// TestWordPressNoIDRemap ensures the remap is scoped to serversideup images;
// the upstream wordpress image must not be forced to root.
func TestWordPressNoIDRemap(t *testing.T) {
	m, err := manifest.Load(filepath.Join("testdata", "golden", "wordpress-mariadb"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := testCtx
	ctx.HostUID, ctx.HostGID = "1000", "1000"
	f, err := Render(m, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if u := f.Services["app"].User; u == "0:0" {
		t.Errorf("wordpress must not be forced to root for id-remap")
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

// TestOpcacheMountedForAllPHPImages locks in the uniform OPcache tuning: every
// site template mounts Hull's shared opcache.ini into the PHP conf.d, WordPress
// gets the local-dev env, and a raw app image gets the mount only when it opts
// in with php_tune.
func TestOpcacheMountedForAllPHPImages(t *testing.T) {
	const want = "/home/test/.hull/system/php/opcache.ini:/usr/local/etc/php/conf.d/zz-hull-opcache.ini:ro"
	hasMount := func(svc *ServiceDef) bool {
		for _, v := range svc.Volumes {
			if v == want {
				return true
			}
		}
		return false
	}

	for _, tmpl := range []string{"laravel", "plain", "wordpress"} {
		src := "schema: 1\nname: site\ntype: site\ntemplate: " + tmpl + "\n"
		if tmpl == "wordpress" {
			src += "services:\n  db:\n    engine: mariadb\n"
		}
		m, err := manifest.Parse([]byte(src))
		if err != nil {
			t.Fatalf("%s parse: %v", tmpl, err)
		}
		f, err := Render(m, testCtx)
		if err != nil {
			t.Fatalf("%s render: %v", tmpl, err)
		}
		if !hasMount(f.Services["app"]) {
			t.Errorf("%s: app missing the opcache mount: %v", tmpl, f.Services["app"].Volumes)
		}
	}

	// WordPress local-dev defaults.
	m, err := manifest.Parse([]byte("schema: 1\nname: blog\ntype: site\ntemplate: wordpress\nservices:\n  db:\n    engine: mariadb\n"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.Services["app"].Environment, "\n")
	for _, w := range []string{"WP_ENVIRONMENT_TYPE=local", "DISABLE_WP_CRON"} {
		if !strings.Contains(joined, w) {
			t.Errorf("wordpress env missing %q: %v", w, f.Services["app"].Environment)
		}
	}

	// A raw app image gets the mount only when it opts in with php_tune.
	app, err := manifest.Parse([]byte(`schema: 1
name: stack
type: app
containers:
  tuned:
    image: my-php:8.3
    php_tune: true
  raw:
    image: my-php:8.3
`))
	if err != nil {
		t.Fatal(err)
	}
	af, err := Render(app, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMount(af.Services["tuned"]) {
		t.Errorf("php_tune container missing opcache mount: %v", af.Services["tuned"].Volumes)
	}
	if hasMount(af.Services["raw"]) {
		t.Errorf("raw container should not get opcache mount: %v", af.Services["raw"].Volumes)
	}
}

// TestDBHealthcheckAndDependsOn locks in the cold-start race fix: a site with a
// dedicated database gets a healthcheck on the db and the app waits on it with
// condition service_healthy; a site with no database gets neither.
func TestDBHealthcheckAndDependsOn(t *testing.T) {
	m, err := manifest.Parse([]byte("schema: 1\nname: dash\ntype: site\ntemplate: laravel\nservices:\n  db:\n    engine: postgres\n"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	db := f.Services["db"]
	if db == nil || db.HealthCheck == nil || len(db.HealthCheck.Test) == 0 {
		t.Fatalf("dedicated db is missing a healthcheck: %+v", db)
	}
	app := f.Services["app"]
	if app == nil || app.DependsOn["db"].Condition != "service_healthy" {
		t.Errorf("app is missing depends_on db (service_healthy): %+v", app.DependsOn)
	}

	bare, err := manifest.Parse([]byte("schema: 1\nname: bare\ntype: site\ntemplate: laravel\n"))
	if err != nil {
		t.Fatal(err)
	}
	bf, err := Render(bare, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	if bf.Services["app"].DependsOn != nil {
		t.Errorf("a no-db site should have no depends_on: %+v", bf.Services["app"].DependsOn)
	}
}

// TestStaticTemplateRenders locks in the first non-PHP template: nginx serving
// the project directory, with none of the PHP-only machinery.
func TestStaticTemplateRenders(t *testing.T) {
	m, err := manifest.Parse([]byte("schema: 1\nname: site\ntype: site\ntemplate: static\n"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	app := f.Services["app"]
	if app.Image != "nginx:alpine" {
		t.Errorf("static image = %q, want nginx:alpine", app.Image)
	}
	if len(app.Volumes) != 1 || app.Volumes[0] != "./:/usr/share/nginx/html" {
		t.Errorf("static volumes = %v, want [./:/usr/share/nginx/html] (no opcache mount)", app.Volumes)
	}
	if app.User != "" || len(app.Entrypoint) != 0 {
		t.Errorf("static must not get the serversideup id-remap: user=%q entrypoint=%v", app.User, app.Entrypoint)
	}
	// A non-PHP template must reject a php version.
	if _, err := manifest.Parse([]byte("schema: 1\nname: x\ntype: site\ntemplate: static\nphp: \"8.3\"\n")); err == nil {
		t.Error("static template should reject a php version")
	}
}

// TestContainerNetworkIsolation locks in the build-your-own isolation model
// (CLU-21): a container reaches another only on a shared network, so a PII
// backend on its own segment is unreachable by services not on it.
func TestContainerNetworkIsolation(t *testing.T) {
	m, err := manifest.Parse([]byte(`schema: 1
name: stack
type: app
containers:
  vault:
    image: myvault
    networks: [pii_secure]
  db_pii:
    image: postgres:15
    networks: [pii_secure]
  worker:
    image: myworker
    networks: [app_internal]
`))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Render(m, testCtx)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"pii_secure", "app_internal"} {
		if _, ok := f.Networks[n]; !ok {
			t.Errorf("network %q not defined in render", n)
		}
	}
	has := func(svc, net string) bool {
		for _, n := range f.Services[svc].Networks {
			if n == net {
				return true
			}
		}
		return false
	}
	if !has("vault", "pii_secure") || !has("db_pii", "pii_secure") {
		t.Error("vault and db_pii should share pii_secure")
	}
	if has("worker", "pii_secure") {
		t.Error("worker must NOT reach pii_secure (isolation broken)")
	}
	for _, s := range []string{"vault", "db_pii", "worker"} {
		if !has(s, "default") {
			t.Errorf("%s missing default network", s)
		}
	}
}
