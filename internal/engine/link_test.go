package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/dbauth"
	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
)

func linkFixture(t *testing.T) (*Engine, *state.Project, *services.Manager, *[]string) {
	return linkFixtureDB(t, "postgres")
}

// linkFixtureDB makes a laravel project "shop" with a dedicated db ("" for
// none) and a services manager whose commands are recorded, not run.
func linkFixtureDB(t *testing.T, db string) (*Engine, *state.Project, *services.Manager, *[]string) {
	t.Helper()
	e, root := testEngine(t)
	dir, err := e.NewProject(context.Background(), NewOptions{
		Name: "shop", Template: "laravel", DB: db,
		SkipScaffold: true, SkipStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_CONNECTION=pgsql\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var ops []string
	noop := func(ctx context.Context, d, name string, args ...string) error {
		ops = append(ops, name+" "+strings.Join(args, " "))
		return nil
	}
	mgr := &services.Manager{
		HullHome: e.Config.HullHome,
		Run:      noop,
		Output: func(ctx context.Context, d, name string, args ...string) (string, error) {
			ops = append(ops, name+" "+strings.Join(args, " "))
			return "", nil
		},
		EnsureNet:       func(ctx context.Context, name string) error { return nil },
		RunningProjects: func(ctx context.Context) ([]string, error) { return nil, nil },
	}
	p, err := state.Find([]string{root}, "shop")
	if err != nil {
		t.Fatal(err)
	}
	return e, p, mgr, &ops
}

func TestLinkPostgres(t *testing.T) {
	e, p, mgr, ops := linkFixtureDB(t, "")
	instance, err := e.Link(context.Background(), p, "postgres@16", mgr)
	if err != nil {
		t.Fatal(err)
	}
	if instance != "postgres-16" {
		t.Errorf("instance = %s", instance)
	}

	// Manifest persisted with mode: shared.
	m, err := manifest.Load(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	db := m.Services["db"]
	if db == nil || db.Mode != manifest.ModeShared || db.Database != "shop" {
		t.Fatalf("db service = %+v", db)
	}

	// Compose no longer contains a project-local db container.
	composeData, _ := os.ReadFile(filepath.Join(p.Dir, "compose.yaml"))
	if strings.Contains(string(composeData), "postgres:16-alpine") {
		t.Error("dedicated db container still rendered after link")
	}

	// .env points at the shared instance container.
	envData, _ := os.ReadFile(filepath.Join(p.Dir, ".env"))
	if host, _ := envfile.Get(string(envData), "DB_HOST"); host != "hull-postgres-16" {
		t.Errorf("DB_HOST = %q", host)
	}

	// Instance booted and database created.
	joined := strings.Join(*ops, "\n")
	if !strings.Contains(joined, "compose up -d") || !strings.Contains(joined, `CREATE DATABASE "shop"`) {
		t.Errorf("ops = %s", joined)
	}
}

func TestLinkInvalidCombinationRollsBack(t *testing.T) {
	e, root := testEngine(t)
	if _, err := e.NewProject(context.Background(), NewOptions{
		Name: "blog", Template: "wordpress", SkipScaffold: true, SkipStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := state.Find([]string{root}, "blog")
	if err != nil {
		t.Fatal(err)
	}
	mgr := &services.Manager{HullHome: e.Config.HullHome}
	if _, err := e.Link(context.Background(), p, "postgres@16", mgr); err == nil {
		t.Fatal("linking wordpress to postgres should fail validation")
	}
	// On-disk manifest must still be the valid mariadb one.
	m, err := manifest.Load(p.Dir)
	if err != nil {
		t.Fatalf("manifest corrupted by failed link: %v", err)
	}
	if m.Services["db"].Engine != "mariadb" {
		t.Errorf("db engine = %s", m.Services["db"].Engine)
	}
}

func TestLinkSearchWritesScoutEnv(t *testing.T) {
	e, p, mgr, _ := linkFixture(t)
	if _, err := e.Link(context.Background(), p, "meilisearch", mgr); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Services["search"] == nil || m.Services["search"].Engine != "meilisearch" {
		t.Fatalf("search service = %+v", m.Services["search"])
	}
	key, ok := dbauth.Get(e.Config.HullHome, "instance/meilisearch", "_root")
	if !ok || key.Password == "hullMasterKey" {
		t.Fatalf("a new instance needs its own key, got %+v", key)
	}
	envData, _ := os.ReadFile(filepath.Join(p.Dir, ".env"))
	env := string(envData)
	for _, want := range []string{"SCOUT_DRIVER=meilisearch", "MEILISEARCH_HOST=http://hull-meilisearch:7700", "MEILISEARCH_KEY=" + key.Password} {
		if !strings.Contains(env, want) {
			t.Errorf(".env missing %q:\n%s", want, env)
		}
	}
}

func TestLinkStorageWritesS3Env(t *testing.T) {
	e, p, mgr, _ := linkFixture(t)
	if _, err := e.Link(context.Background(), p, "minio", mgr); err != nil {
		t.Fatal(err)
	}
	envData, _ := os.ReadFile(filepath.Join(p.Dir, ".env"))
	env := string(envData)
	for _, want := range []string{"FILESYSTEM_DISK=s3", "AWS_ENDPOINT=http://hull-minio:9000", "AWS_USE_PATH_STYLE_ENDPOINT=true"} {
		if !strings.Contains(env, want) {
			t.Errorf(".env missing %q:\n%s", want, env)
		}
	}
}

func TestUnlink(t *testing.T) {
	e, p, mgr, _ := linkFixture(t)
	if _, err := e.Link(context.Background(), p, "redis", mgr); err != nil {
		t.Fatal(err)
	}
	if err := e.Unlink(context.Background(), p, "redis"); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Services["redis"]; ok {
		t.Error("redis service still in manifest after unlink")
	}
	if err := e.Unlink(context.Background(), p, "ghost"); err == nil {
		t.Error("unlink of missing key should fail")
	}
}

func TestNewProjectGetsDBPasswordAndOldInstanceKeepsFixedKey(t *testing.T) {
	e, p, mgr, _ := linkFixture(t)
	login, ok := dbauth.Get(e.Config.HullHome, "project/shop", "db")
	if !ok || login.User != "postgres" {
		t.Fatalf("new project has no stored db login: %+v", login)
	}
	compose, _ := os.ReadFile(filepath.Join(p.Dir, "compose.yaml"))
	if !strings.Contains(string(compose), "POSTGRES_PASSWORD="+login.Password) {
		t.Errorf("compose.yaml lacks the generated root password:\n%s", compose)
	}

	// A Meilisearch instance made before keys were generated: its compose
	// file exists, its key does not. Linking must keep the fixed key the
	// instance runs with.
	old := filepath.Join(e.Config.HullHome, "services", "meilisearch")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Link(context.Background(), p, "meilisearch", mgr); err != nil {
		t.Fatal(err)
	}
	env, _ := os.ReadFile(filepath.Join(p.Dir, ".env"))
	if !strings.Contains(string(env), "MEILISEARCH_KEY=hullMasterKey") {
		t.Errorf("an old instance's project got a key it does not run with:\n%s", env)
	}
}

func TestLinkRefusesToSwapADedicatedDatabase(t *testing.T) {
	e, p, mgr, ops := linkFixture(t) // shop has its own postgres
	before, _ := os.ReadFile(filepath.Join(p.Dir, "hull.yaml"))
	if _, err := e.Link(context.Background(), p, "postgres@16", mgr); err == nil || !strings.Contains(err.Error(), "already has its own postgres") {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(p.Dir, "hull.yaml"))
	if string(before) != string(after) || len(*ops) != 0 {
		t.Errorf("a refused link still changed things: ops=%v", *ops)
	}
}
