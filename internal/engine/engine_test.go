package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func TestBuildImportManifestSlugsName(t *testing.T) {
	// A folder adopted in place often has spaces/capitals; the manifest name
	// must come out as a docker-safe slug (and pass manifest validation).
	m, err := BuildImportManifest("My App", bundle.Detection{Template: "plain"}, NewOptions{})
	if err != nil {
		t.Fatalf("import with a spaced name failed: %v", err)
	}
	if m.Name != "my-app" {
		t.Errorf("imported Name = %q, want my-app", m.Name)
	}
}

func TestBuildImportManifestExtras(t *testing.T) {
	det := bundle.Detection{Template: "laravel", DB: "postgres", Redis: true, Extras: []string{"mailpit", "meilisearch"}}
	m, err := BuildImportManifest("shop", det, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"db", "redis", "mailpit", "meilisearch"} {
		if m.Services[want] == nil {
			t.Errorf("imported manifest missing service %q: %v", want, m.Services)
		}
	}
	if s := m.Services["mailpit"]; s != nil && s.Engine != "mailpit" {
		t.Errorf("mailpit engine = %q", s.Engine)
	}
}

func testEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	noAdminer := false // isolate primitives: don't auto-provision Adminer in unit tests
	cfg := &config.Config{TLD: "test", Roots: []string{root}, HullHome: t.TempDir(),
		Services: config.ServicesConfig{AutoAdminer: &noAdminer}}
	e := New(cfg)
	e.Run = func(ctx context.Context, dir, name string, args ...string) error {
		t.Fatalf("unexpected command execution: %s %v", name, args)
		return nil
	}
	e.EnsureNet = func(ctx context.Context, name string) error { return nil }
	return e, root
}

func TestNewClusterManaged(t *testing.T) {
	e, root := testEngine(t)
	dir, err := e.NewCluster(context.Background(), NewClusterOptions{
		Name:      "stack",
		Managed:   true,
		SkipStart: true,
		Containers: []ContainerSpec{
			{Name: "web", Image: "nginx", Version: "1.27", Port: 80, Serve: true},
			{Name: "worker", Image: "redis", Serve: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != manifest.TypeApp || len(m.Containers) != 2 {
		t.Fatalf("manifest = %+v", m)
	}
	if web := m.Containers["web"]; web == nil || web.Image != "nginx:1.27" || web.Domain != "web" || !web.Served() {
		t.Errorf("web container = %+v", m.Containers["web"])
	}
	if wk := m.Containers["worker"]; wk == nil || wk.Served() {
		t.Errorf("worker should be unserved: %+v", wk)
	}
	if root == "" {
		t.Fatal("no root")
	}
}

func TestNewClusterOwned(t *testing.T) {
	e, _ := testEngine(t)
	dir, err := e.NewCluster(context.Background(), NewClusterOptions{
		Name:        "owned",
		ComposeRoot: "core",
		Managed:     false,
		SkipStart:   true,
		Containers: []ContainerSpec{
			{Name: "api", Image: "node", Version: "22", Port: 3000, Serve: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != manifest.TypeCluster || m.ComposeRoot != "core" {
		t.Fatalf("manifest = %+v", m)
	}
	if r := m.Routes["api"]; r == nil || r.Service != "api" || r.Port != 3000 {
		t.Errorf("api route = %+v", m.Routes["api"])
	}
	if _, err := os.Stat(filepath.Join(dir, "core", "compose.yaml")); err != nil {
		t.Errorf("owned compose not written: %v", err)
	}
}

func TestAdoptClusterParsesCaddyfile(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "mystack")
	core := filepath.Join(dir, "core")
	if err := os.MkdirAll(core, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caddy := `api.mystack.local {
	import tls_local
	reverse_proxy mystack_api:8081
}
t.mystack.local {
	reverse_proxy mystack_edge:8080
}
`
	if err := os.WriteFile(filepath.Join(core, "Caddyfile"), []byte(caddy), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := e.AdoptCluster(ClusterOptions{Dir: dir, ComposeRoot: "core"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != manifest.TypeCluster || m.Name != "mystack" || m.ComposeRoot != "core" {
		t.Fatalf("manifest = %+v", m)
	}
	if len(m.Routes) != 2 {
		t.Fatalf("routes = %+v", m.Routes)
	}
	if r := m.Routes["api"]; r == nil || r.Service != "mystack_api" || r.Port != 8081 {
		t.Errorf("api route = %+v", m.Routes["api"])
	}
	// Re-adopt must refuse (hull.yaml now exists).
	if _, err := e.AdoptCluster(ClusterOptions{Dir: dir, ComposeRoot: "core"}); err == nil {
		t.Error("expected re-adopt to fail")
	}
}

func TestAdoptClusterSeedsRoutesFromCompose(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "stack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := `services:
  web:
    image: nginx
    ports:
      - "8080:80"
  api:
    image: node
    ports:
      - target: 3000
        published: 3000
  db:
    image: postgres
    ports:
      - "5432:5432"
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := e.AdoptCluster(ClusterOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if r := m.Routes["web"]; r == nil || r.Port != 80 || r.Service != "web" {
		t.Errorf("web route = %+v, want service=web port=80", r)
	}
	if r := m.Routes["api"]; r == nil || r.Port != 3000 {
		t.Errorf("api route = %+v, want port=3000", r)
	}
	if m.Routes["db"] != nil {
		t.Errorf("db (5432) should not be routed: %+v", m.Routes["db"])
	}
}

func TestNewProjectWritesArtifacts(t *testing.T) {
	e, root := testEngine(t)
	dir, err := e.NewProject(context.Background(), NewOptions{
		Name:         "myapp",
		Template:     "laravel",
		DB:           "postgres",
		Redis:        true,
		SkipScaffold: true,
		SkipStart:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "myapp") {
		t.Errorf("dir = %s", dir)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("written hull.yaml does not load: %v", err)
	}
	if m.Name != "myapp" || m.Services["db"].Engine != "postgres" || m.Services["redis"] == nil {
		t.Errorf("manifest mismatch: %+v", m)
	}

	composeData, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"caddy=myapp.test", "postgres:16-alpine", "redis:alpine", "zz-hull-opcache.ini:ro"} {
		if !strings.Contains(string(composeData), want) {
			t.Errorf("compose.yaml missing %q", want)
		}
	}

	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("no .gitignore written for the generated compose: %v", err)
	}
	if !strings.Contains(string(gitignore), "/compose.yaml") {
		t.Errorf(".gitignore missing /compose.yaml: %q", gitignore)
	}
}

func TestNewProjectRejectsExisting(t *testing.T) {
	e, root := testEngine(t)
	if err := os.MkdirAll(filepath.Join(root, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := e.NewProject(context.Background(), NewOptions{
		Name: "taken", Template: "plain", SkipScaffold: true, SkipStart: true,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected already-exists error, got %v", err)
	}
}

func TestNewProjectValidatesBeforeTouchingDisk(t *testing.T) {
	e, root := testEngine(t)
	// "@@@" slugifies to empty, so it cannot be normalized and must still be
	// rejected before any directory is created.
	_, err := e.NewProject(context.Background(), NewOptions{
		Name: "@@@", Template: "laravel", SkipScaffold: true, SkipStart: true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("invalid project left files behind: %v", entries)
	}
}

func TestWordpressDefaultsToMariaDB(t *testing.T) {
	e, root := testEngine(t)
	_, err := e.NewProject(context.Background(), NewOptions{
		Name: "blog", Template: "wordpress", SkipScaffold: true, SkipStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(root, "blog"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Services["db"] == nil || m.Services["db"].Engine != "mariadb" {
		t.Errorf("wordpress db = %+v, want mariadb", m.Services["db"])
	}
}

func TestWireLaravelEnvPostgres(t *testing.T) {
	_, root := testEngine(t)
	dir := filepath.Join(root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "APP_NAME=Laravel\nDB_CONNECTION=sqlite\n# DB_HOST=127.0.0.1\n# DB_PORT=3306\n# DB_DATABASE=laravel\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := manifest.Parse([]byte("schema: 1\nname: app\ntemplate: laravel\nservices:\n  db:\n    engine: postgres\n  redis:\n    engine: redis\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := wireLaravelEnv(dir, m); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"DB_CONNECTION": "pgsql",
		"DB_HOST":       "db",
		"DB_PORT":       "5432",
		"DB_DATABASE":   "app",
		"DB_USERNAME":   "postgres",
		"REDIS_HOST":    "redis",
		"CACHE_STORE":   "redis",
	} {
		got, ok := envfile.Get(string(content), key)
		if !ok || got != want {
			t.Errorf("%s = %q (found=%v), want %q", key, got, ok, want)
		}
	}
}

func TestWireLaravelEnvSQLiteFallback(t *testing.T) {
	_, root := testEngine(t)
	dir := filepath.Join(root, "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse([]byte("schema: 1\nname: api\ntemplate: laravel\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := wireLaravelEnv(dir, m); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if got, _ := envfile.Get(string(content), "DB_CONNECTION"); got != "sqlite" {
		t.Errorf("DB_CONNECTION = %q, want sqlite", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "database", "database.sqlite")); err != nil {
		t.Error("database.sqlite not created")
	}
}

func TestUpRegeneratesCompose(t *testing.T) {
	e, _ := testEngine(t)
	dir, err := e.NewProject(context.Background(), NewOptions{
		Name: "site", Template: "plain", SkipScaffold: true, SkipStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sabotage the artifact; Up must rebuild it before composing.
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	var ran [][]string
	e.Run = func(ctx context.Context, d, name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	p, err := state.Find(e.Config.Roots, "site")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Up(context.Background(), p); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if !strings.Contains(string(data), "caddy=site.test") {
		t.Error("compose.yaml was not regenerated before up")
	}
	if len(ran) != 1 || strings.Join(ran[0], " ") != "docker compose up -d" {
		t.Errorf("commands run = %v", ran)
	}
}

// TestNewProjectRejectsDuplicateName guards a real footgun: the compose project
// name IS the Hull project name, so a second project sharing a name fights over
// the same containers. Creating it either re-points the first project's
// containers at the new directory, or (when compose considers them current)
// leaves the new directory unpopulated while the old project keeps serving.
// Note this is a DIFFERENT directory, so the existing already-exists check on
// the target path cannot catch it.
func TestNewProjectRejectsDuplicateName(t *testing.T) {
	e, root := testEngine(t)
	// An existing project called "dupe" living under a differently-named folder.
	other := filepath.Join(root, "some-other-folder")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestYAML := "schema: 1\nname: dupe\ntype: site\ntemplate: plain\n"
	if err := os.WriteFile(filepath.Join(other, "hull.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := e.NewProject(context.Background(), NewOptions{
		Name: "dupe", Template: "plain", SkipScaffold: true, SkipStart: true,
	})
	if err == nil {
		t.Fatal("expected a duplicate-name refusal, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should name the collision, got: %v", err)
	}
	// It must name WHERE the other project lives, or the message is unactionable.
	if !strings.Contains(err.Error(), "some-other-folder") {
		t.Errorf("error should point at the conflicting directory, got: %v", err)
	}
	// And it must not have created the new directory.
	if _, statErr := os.Stat(filepath.Join(root, "dupe")); statErr == nil {
		t.Error("refused creation still made the project directory")
	}
}
