package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func testEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{TLD: "test", Roots: []string{root}, HullHome: t.TempDir()}
	e := New(cfg)
	e.Run = func(ctx context.Context, dir, name string, args ...string) error {
		t.Fatalf("unexpected command execution: %s %v", name, args)
		return nil
	}
	return e, root
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
	xdebugSource := filepath.ToSlash(e.Config.HullHome) + "/system/php/xdebug.ini"
	for _, want := range []string{"caddy=myapp.test", "postgres:16-alpine", "redis:alpine", xdebugSource} {
		if !strings.Contains(string(composeData), want) {
			t.Errorf("compose.yaml missing %q", want)
		}
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
	_, err := e.NewProject(context.Background(), NewOptions{
		Name: "Bad_Name", Template: "laravel", SkipScaffold: true, SkipStart: true,
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
