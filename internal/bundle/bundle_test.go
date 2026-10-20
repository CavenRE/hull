package bundle

import (
	"archive/zip"
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

func TestExportKeepsNestedVendorAndClusterCompose(t *testing.T) {
	dir := exportFixture(t)
	write(t, dir, "public/vendor/horizon/app.css", "body{}")
	write(t, dir, "frontend/node_modules/x.js", "x")
	names := func(keepCompose bool) map[string]bool {
		var buf bytes.Buffer
		if _, err := WriteBundle(&buf, ExportOptions{ProjectDir: dir, KeepCompose: keepCompose}); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, f := range zr.File {
			got[f.Name] = true
		}
		return got
	}
	got := names(false)
	for _, want := range []string{"app/public/vendor/horizon/app.css", "app/frontend/node_modules/x.js"} {
		if !got[want] {
			t.Errorf("nested %s was dropped", want)
		}
	}
	for _, skip := range []string{"app/vendor/autoload.php", "app/node_modules/lib/index.js", "app/compose.yaml"} {
		if got[skip] {
			t.Errorf("%s should be excluded", skip)
		}
	}
	if !names(true)["app/compose.yaml"] {
		t.Error("a cluster's own compose.yaml was dropped")
	}
}

func TestExportSkipsLinksBackupsAndEnvFiles(t *testing.T) {
	dir := exportFixture(t)
	write(t, dir, ".env.production", "DB_PASSWORD=prod\n")
	write(t, dir, "frontend/.env", "VITE_KEY=x\n")
	write(t, dir, ".env.example", "APP_NAME=\n")
	write(t, dir, "wp-config.php.hull-backup", "<?php define('DB_PASSWORD','x');")
	outside := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(outside, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink(outside, filepath.Join(dir, "key")) == nil
	entries := func(includeEnv bool) (map[string]bool, *Meta) {
		var buf bytes.Buffer
		meta, err := WriteBundle(&buf, ExportOptions{ProjectDir: dir, IncludeEnv: includeEnv})
		if err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, f := range zr.File {
			got[f.Name] = true
		}
		return got, meta
	}
	got, meta := entries(false)
	for _, out := range []string{"app/.env.production", "app/frontend/.env", "app/wp-config.php.hull-backup", "app/key"} {
		if got[out] {
			t.Errorf("%s was exported", out)
		}
	}
	if !got["app/.env.example"] {
		t.Error("the .env.example template was dropped")
	}
	if !contains(meta.Skipped, "frontend/.env") || (linked && !contains(meta.Skipped, "key")) {
		t.Errorf("skipped = %v", meta.Skipped)
	}
	got, _ = entries(true)
	if !got["app/.env.production"] || got["app/wp-config.php.hull-backup"] || got["app/key"] {
		t.Errorf("--include-env keeps env files but never backups or links: %v", got)
	}
}

func TestStripSecretsWider(t *testing.T) {
	in := "APP_NAME=Shop\nMAIL_PASS=p1\nDB_PASSWD=p2\nDATABASE_URL=postgres://u:hunter2@db/x\nPLAIN_URL=https://example.test/a\n" +
		"JWT=\"-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----\"\nAFTER=1\n"
	out, keys := stripSecrets(in)
	for _, leak := range []string{"p1", "p2", "hunter2", "MIIabc", "BEGIN"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	for _, keep := range []string{"APP_NAME=Shop", "PLAIN_URL=https://example.test/a", "AFTER=1"} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q lost:\n%s", keep, out)
		}
	}
	if strings.Join(keys, ",") != "DATABASE_URL,DB_PASSWD,JWT,MAIL_PASS" {
		t.Errorf("stripped keys = %v", keys)
	}
}

func TestExtractFileStopsAtBudget(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("app/big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 1000)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "big.txt")
	if _, err := extractFile(zr.File[0], target, 100); err == nil {
		t.Error("an entry larger than the budget was extracted")
	}
	if _, err := extractFile(zr.File[0], target, 5000); err == nil {
		t.Error("extract overwrote an existing file")
	}
}
