package bundle

import (
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/manifest"
)

func projectManifest(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDumpCommandDedicated(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: postgres\n")
	cmd, err := DumpCommand(m, "db", "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "/proj" || cmd.String() != "docker compose exec -T db pg_dump -U postgres --no-owner shop" {
		t.Errorf("cmd = %+v", cmd)
	}
}

func TestDumpCommandShared(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: mariadb\n    mode: shared\n")
	cmd, err := DumpCommand(m, "db", "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "" || cmd.String() != "docker exec -i hull-mariadb-lts mariadb-dump -u root shop" {
		t.Errorf("cmd = %+v", cmd)
	}
}

func TestRestoreAndReadyCommands(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: mysql\n")
	restore, err := RestoreCommand(m, "db", "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if restore.String() != "docker compose exec -T db mysql -f -u root shop" {
		t.Errorf("restore = %s", restore)
	}
	ready, err := ReadyCommand(m, "db", "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ready.String(), "SELECT 1") {
		t.Errorf("ready = %s", ready)
	}
}

func TestCommandsRejectNonDatabase(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  redis:\n    engine: redis\n")
	if _, err := DumpCommand(m, "redis", "/p"); err == nil {
		t.Error("redis dump should fail")
	}
	if _, err := DumpCommand(m, "ghost", "/p"); err == nil {
		t.Error("missing key should fail")
	}
}

func TestFilterDump(t *testing.T) {
	in := strings.NewReader(
		"CREATE DATABASE foo;\n" +
			"USE foo;\n" +
			"  use bar;\n" +
			"CREATE TABLE users (id int);\n" +
			"INSERT INTO users VALUES (1); -- USE not at start\n")
	var out strings.Builder
	if err := FilterDump(in, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "CREATE DATABASE") || strings.Contains(got, "USE foo") || strings.Contains(got, "use bar") {
		t.Errorf("filter left statements:\n%s", got)
	}
	if !strings.Contains(got, "CREATE TABLE users") || !strings.Contains(got, "INSERT INTO users") {
		t.Errorf("filter removed too much:\n%s", got)
	}
}
