package bundle

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exportFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "hull.yaml", "schema: 1\nname: shop\ntemplate: laravel\n")
	write(t, dir, "compose.yaml", "generated artifact, must be excluded")
	write(t, dir, ".env", "APP_NAME=Shop\nAPP_KEY=base64:secret123\nDB_PASSWORD=hunter2\nDB_HOST=db\n")
	write(t, dir, "app/Models/User.php", "<?php class User {}")
	write(t, dir, "vendor/autoload.php", "<?php // heavy")
	write(t, dir, "node_modules/lib/index.js", "x")
	write(t, dir, ".git/HEAD", "ref: refs/heads/main")
	write(t, dir, "public/index.php", "<?php")
	return dir
}

func TestBundleRoundTrip(t *testing.T) {
	dir := exportFixture(t)

	var buf bytes.Buffer
	meta, err := WriteBundle(&buf, ExportOptions{
		ProjectDir:  dir,
		ProjectYAML: "schema: 1\nname: shop\ntemplate: laravel\n",
		HullVersion: "test",
		DumpKeys:    []string{"db"},
		DumpDB: func(key string, w io.Writer) error {
			_, err := io.WriteString(w, "CREATE TABLE t (id int);\n")
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Dumps["db"] != "db/db.sql.gz" {
		t.Errorf("dumps = %v", meta.Dumps)
	}
	if len(meta.EnvStripped) == 0 || !contains(meta.EnvStripped, "APP_KEY") || !contains(meta.EnvStripped, "DB_PASSWORD") {
		t.Errorf("stripped = %v", meta.EnvStripped)
	}

	zipPath := filepath.Join(t.TempDir(), "shop-bundle.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Import side.
	gotMeta, err := ReadMeta(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotMeta.ProjectYAML != meta.ProjectYAML {
		t.Error("project yaml mismatch")
	}

	target := t.TempDir()
	if _, err := Extract(zipPath, target); err != nil {
		t.Fatal(err)
	}
	mustExist := []string{"hull.yaml", "app/Models/User.php", "public/index.php", ".env", "db.sql.gz"}
	for _, f := range mustExist {
		if _, err := os.Stat(filepath.Join(target, f)); err != nil {
			t.Errorf("missing %s after extract", f)
		}
	}
	mustNotExist := []string{"compose.yaml", "vendor/autoload.php", "node_modules/lib/index.js", ".git/HEAD"}
	for _, f := range mustNotExist {
		if _, err := os.Stat(filepath.Join(target, f)); err == nil {
			t.Errorf("%s should not be in the bundle", f)
		}
	}

	// Secrets blanked, non-secrets intact.
	envData, err := os.ReadFile(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	env := string(envData)
	if strings.Contains(env, "hunter2") || strings.Contains(env, "secret123") {
		t.Error("secrets leaked into bundle")
	}
	if !strings.Contains(env, "APP_NAME=Shop") || !strings.Contains(env, "DB_HOST=db") {
		t.Error("non-secret env lost")
	}
}

func TestExtractDoesNotClobberEnv(t *testing.T) {
	dir := exportFixture(t)
	var buf bytes.Buffer
	if _, err := WriteBundle(&buf, ExportOptions{ProjectDir: dir, ProjectYAML: "x", HullVersion: "t"}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "b.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir()
	write(t, target, ".env", "PRECIOUS=yes\n")
	if _, err := Extract(zipPath, target); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(target, ".env"))
	if !strings.Contains(string(data), "PRECIOUS=yes") {
		t.Error("existing .env was clobbered")
	}
}

func TestIncludeEnvKeepsSecrets(t *testing.T) {
	dir := exportFixture(t)
	var buf bytes.Buffer
	meta, err := WriteBundle(&buf, ExportOptions{
		ProjectDir: dir, ProjectYAML: "x", HullVersion: "t", IncludeEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.EnvStripped) != 0 {
		t.Errorf("stripped = %v with IncludeEnv", meta.EnvStripped)
	}
}

func TestReadMetaRejectsNonBundle(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "x.zip")
	var buf bytes.Buffer
	// Minimal zip without manifest.json.
	if err := os.WriteFile(zipPath, []byte("PK\x05\x06"+strings.Repeat("\x00", 18)), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = buf
	if _, err := ReadMeta(zipPath); err == nil {
		t.Error("expected error for non-bundle zip")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
