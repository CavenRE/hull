package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/dbauth"
	"github.com/CavenRE/hull/internal/envfile"
)

func TestBuildImportManifestDetectionAndOverrides(t *testing.T) {
	det := bundle.Detection{Template: "laravel", PHP: "8.2", DB: "mysql", Database: "old-shop", Redis: true}
	m, err := BuildImportManifest("shop", det, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.PHP != "8.2" || m.Services["db"].Engine != "mysql" || m.Services["redis"] == nil {
		t.Errorf("manifest = %+v", m)
	}
	if m.Services["db"].Database != "old_shop" {
		t.Errorf("database = %q (hyphens must be sanitized)", m.Services["db"].Database)
	}

	// Flags override detection.
	m, err = BuildImportManifest("shop", det, NewOptions{DB: "postgres", PHP: "8.4"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Services["db"].Engine != "postgres" || m.PHP != "8.4" {
		t.Errorf("override manifest = %+v", m)
	}
}

func TestAdoptLaravel(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "APP_NAME=Legacy\nDB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_DATABASE=legacy_db\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	det := bundle.Detection{Template: "laravel", DB: "mysql", Database: "legacy_db"}
	m, err := BuildImportManifest("legacy", det, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(m, dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".env.hull-backup")); err != nil {
		t.Error("no .env backup written")
	}
	if _, err := os.Stat(filepath.Join(dir, "hull.yaml")); err != nil {
		t.Error("no hull.yaml written")
	}
	envData, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if host, _ := envfile.Get(string(envData), "DB_HOST"); host != "db" {
		t.Errorf("DB_HOST = %q", host)
	}
	if db, _ := envfile.Get(string(envData), "DB_DATABASE"); db != "legacy_db" {
		t.Errorf("DB_DATABASE = %q (detected name must survive)", db)
	}
}

func TestAdoptWordPress(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "oldblog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "<?php\ndefine( 'DB_NAME', 'blog_db' );\ndefine( 'DB_HOST', 'localhost' );\ndefine( 'DB_PASSWORD', 'old' );\ndefine( 'DB_USER', 'webuser' );\n"
	if err := os.WriteFile(filepath.Join(dir, "wp-config.php"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	det := bundle.Detection{Template: "wordpress", DB: "mariadb", Database: "blog_db"}
	m, err := BuildImportManifest("oldblog", det, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(m, dir); err != nil {
		t.Fatal(err)
	}

	// The imported site gets a new dedicated database, so Hull gives it a root
	// password and writes that into wp-config.
	login, ok := dbauth.Get(e.Config.HullHome, "project/oldblog", "db")
	if !ok {
		t.Fatal("no login stored for the new dedicated database")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "wp-config.php"))
	content := string(data)
	for _, want := range []string{
		"define( 'DB_HOST', 'db' )",
		"define( 'DB_NAME', 'blog_db' )",
		"define( 'DB_USER', 'root' )",
		"define( 'DB_PASSWORD', '" + login.Password + "' )",
		"HTTP_X_FORWARDED_PROTO",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("wp-config missing %q:\n%s", want, content)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "wp-config.php.hull-backup")); err != nil {
		t.Error("no wp-config backup written")
	}
}

func TestAdoptRefusesBrokenManifest(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := "schema: [not a manifest\n"
	if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := BuildImportManifest("broken", bundle.Detection{Template: "plain"}, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(m, dir); err == nil {
		t.Fatal("Adopt overwrote a hull.yaml that fails to parse")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "hull.yaml")); string(data) != broken {
		t.Errorf("hull.yaml changed to %q", data)
	}
}

func TestAdoptBackupKeepsEnvMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	e, root := testEngine(t)
	dir := filepath.Join(root, "secret")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := BuildImportManifest("secret", bundle.Detection{Template: "laravel"}, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Adopt(m, dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, ".env.hull-backup"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, %v; want 0600", fi, err)
	}
}
