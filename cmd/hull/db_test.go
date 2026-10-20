package main

import (
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func dbProject(t *testing.T, services string) *state.Project {
	t.Helper()
	m, err := manifest.Parse([]byte("schema: 1\nname: shop\ntemplate: laravel\n" + services))
	if err != nil {
		t.Fatal(err)
	}
	return &state.Project{Name: "shop", Manifest: m}
}

func TestProjectDatabasePicksTheOnlyOne(t *testing.T) {
	one := dbProject(t, "services:\n  db:\n    engine: mariadb\n  cache:\n    engine: redis\n")
	if _, key, err := projectDatabase(one, ""); err != nil || key != "db" {
		t.Errorf("key = %q, err = %v; redis is not a database", key, err)
	}
	if got := dumpFileName(one.Manifest, "db"); got != "shop.sql" {
		t.Errorf("file = %s", got)
	}

	two := dbProject(t, "services:\n  db:\n    engine: mariadb\n  analytics:\n    engine: postgres\n")
	if _, _, err := projectDatabase(two, ""); err == nil || !strings.Contains(err.Error(), "--service") {
		t.Errorf("two databases without --service: %v", err)
	}
	if _, key, err := projectDatabase(two, "analytics"); err != nil || key != "analytics" {
		t.Errorf("key = %q, err = %v", key, err)
	}
	if got := dumpFileName(two.Manifest, "analytics"); got != "shop-analytics.sql" {
		t.Errorf("file = %s", got)
	}
	if _, _, err := projectDatabase(two, "cache"); err == nil {
		t.Error("a service that is not one of the databases was accepted")
	}

	if _, _, err := projectDatabase(dbProject(t, ""), ""); err == nil {
		t.Error("a project with no database was accepted")
	}
	if _, _, err := projectDatabase(&state.Project{Name: "old", Legacy: true}, ""); err == nil {
		t.Error("a legacy project was accepted")
	}
}

func TestWriteDumpReplacesOnlyWhenComplete(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "shop.sql")
	if err := os.WriteFile(target, []byte("old dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeDump(target, func(w io.Writer) error {
		_, _ = io.WriteString(w, "half a du")
		return errors.New("the container stopped")
	})
	if err == nil {
		t.Fatal("a failed dump reported success")
	}
	if got, _ := os.ReadFile(target); string(got) != "old dump" {
		t.Errorf("a failed dump replaced the old file: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".hull-db-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}

	gzTarget := filepath.Join(dir, "shop.sql.gz")
	if err := writeDump(gzTarget, func(w io.Writer) error {
		_, err := io.WriteString(w, "CREATE TABLE t (id int);\n")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(gzTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("a .gz target is not gzip: %v", err)
	}
	if got, _ := io.ReadAll(r); string(got) != "CREATE TABLE t (id int);\n" {
		t.Errorf("gz content = %q", got)
	}
}

// Both commands act on a running database, so Docker must be up first.
func TestDBCommandsNeedTheEngine(t *testing.T) {
	applyEngineAnnotations()
	for _, path := range [][]string{{"db", "export"}, {"db", "import"}} {
		cmd, _, err := rootCmd.Find(path)
		if err != nil || cmd.Name() != path[1] {
			t.Fatalf("%v: %v", path, err)
		}
		if got := cmd.Annotations[engineAnnotation]; got != engineEnsure {
			t.Errorf("hull %s %s engine = %q, want %q", path[0], path[1], got, engineEnsure)
		}
	}
}

func TestOpenCheckedDump(t *testing.T) {
	dir := t.TempDir()
	gz := filepath.Join(dir, "LIVE.SQL.GZ")
	f, err := os.Create(gz)
	if err != nil {
		t.Fatal(err)
	}
	w := gzip.NewWriter(f)
	_, _ = io.WriteString(w, "-- MariaDB dump 10.19\nCREATE TABLE t (id int);\n")
	_ = w.Close()
	_ = f.Close()
	in, closeDump, err := openCheckedDump(gz, "mysql")
	if err != nil {
		t.Fatalf("an upper-case .SQL.GZ name: %v", err)
	}
	head, _ := in.Peek(16)
	closeDump()
	if string(head) != "-- MariaDB dump " {
		t.Errorf("read %q, want the unzipped dump", head)
	}

	pg := filepath.Join(dir, "pg.sql")
	if err := os.WriteFile(pg, []byte("--\n-- PostgreSQL database dump\n--\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openCheckedDump(pg, "mariadb"); err == nil {
		t.Error("a Postgres dump was accepted for MariaDB")
	}
}
