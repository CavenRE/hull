package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
)

func linkFixture(t *testing.T) (*Engine, *state.Project, *services.Manager, *[]string) {
	t.Helper()
	e, root := testEngine(t)
	dir, err := e.NewProject(context.Background(), NewOptions{
		Name: "shop", Template: "laravel", DB: "postgres",
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
	e, p, mgr, ops := linkFixture(t)
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
