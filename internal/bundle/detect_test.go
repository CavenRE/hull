package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectLaravelFromArtisan(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "artisan", "#!/usr/bin/env php")
	write(t, dir, "composer.json", `{"require": {"php": "^8.2"}}`)
	write(t, dir, ".env", "DB_CONNECTION=mysql\nDB_DATABASE=shop_db\nREDIS_HOST=127.0.0.1\n")

	d := Detect(dir)
	if d.Template != "laravel" || d.PHP != "8.2" || d.DB != "mysql" || d.Database != "shop_db" || !d.Redis {
		t.Errorf("detection = %+v", d)
	}
}

func TestDetectLaravelFromComposer(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "composer.json", `{"require": {"php": "^8.1|^8.3", "laravel/framework": "^10.0"}}`)
	d := Detect(dir)
	if d.Template != "laravel" {
		t.Errorf("template = %s", d.Template)
	}
	if d.PHP != "8.3" {
		t.Errorf("php = %s (highest mentioned should win)", d.PHP)
	}
	if d.DB != "postgres" {
		t.Errorf("db = %s (laravel smart default)", d.DB)
	}
}

func TestDetectWordPress(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "wp-config.php", "<?php\ndefine( 'DB_NAME', 'blog_db' );\n")
	d := Detect(dir)
	if d.Template != "wordpress" || d.DB != "mariadb" || d.Database != "blog_db" {
		t.Errorf("detection = %+v", d)
	}
}

func TestDetectPlainAndSQLite(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "index.php", "<?php phpinfo();")
	d := Detect(dir)
	if d.Template != "plain" || d.DB != "" || d.Redis {
		t.Errorf("detection = %+v", d)
	}

	lara := t.TempDir()
	write(t, lara, "artisan", "#!/usr/bin/env php")
	write(t, lara, ".env", "DB_CONNECTION=sqlite\n")
	d = Detect(lara)
	if d.DB != "postgres" {
		// sqlite means "no db service"; laravel smart default applies.
		t.Errorf("sqlite laravel db = %q", d.DB)
	}
}

func TestFindDumps(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "backup.sql", "SELECT 1;")
	write(t, dir, "old.sql.gz", "x")
	write(t, dir, "archive.zip", "x")
	write(t, dir, "myapp-bundle.zip", "x")
	write(t, dir, "readme.md", "x")

	dumps := FindDumps(dir)
	if len(dumps) != 3 {
		t.Errorf("dumps = %v (bundle zips must be excluded)", dumps)
	}
}
