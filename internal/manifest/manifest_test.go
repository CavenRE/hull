package manifest

import (
	"strings"
	"testing"
)

func parse(t *testing.T, src string) *Manifest {
	t.Helper()
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func parseErr(t *testing.T, src, wantSubstr string) {
	t.Helper()
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err, wantSubstr)
	}
}

func TestSiteDefaults(t *testing.T) {
	m := parse(t, `
schema: 1
name: myapp
template: laravel
services:
  db:
    engine: postgres
`)
	if m.Type != TypeSite {
		t.Errorf("type = %q, want site", m.Type)
	}
	if m.Domain != "myapp" {
		t.Errorf("domain = %q, want myapp", m.Domain)
	}
	if m.PHP != "8.4" {
		t.Errorf("php = %q, want 8.4", m.PHP)
	}
	db := m.Services["db"]
	if db.Mode != ModeDedicated {
		t.Errorf("mode = %q, want dedicated", db.Mode)
	}
	if db.Version != "16" {
		t.Errorf("version = %q, want 16", db.Version)
	}
	if db.Database != "myapp" {
		t.Errorf("database = %q, want myapp", db.Database)
	}
}

func TestDatabaseNameHyphens(t *testing.T) {
	m := parse(t, `
schema: 1
name: my-cool-app
template: laravel
services:
  db:
    engine: mysql
`)
	if got := m.Services["db"].Database; got != "my_cool_app" {
		t.Errorf("database = %q, want my_cool_app", got)
	}
}

func TestWordpressPHPNotDefaulted(t *testing.T) {
	m := parse(t, `
schema: 1
name: blog
template: wordpress
services:
  db:
    engine: mariadb
`)
	if m.PHP != "" {
		t.Errorf("php = %q, want empty for wordpress", m.PHP)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"missing schema", "name: x\ntemplate: plain\n", "missing 'schema'"},
		{"future schema", "schema: 99\nname: x\ntemplate: plain\n", "newer than this Hull"},
		{"missing name", "schema: 1\ntemplate: plain\n", "'name' is required"},
		{"bad name", "schema: 1\nname: My_App\ntemplate: plain\n", "invalid name"},
		{"missing template", "schema: 1\nname: x\n", "'template' is required"},
		{"unknown template", "schema: 1\nname: x\ntemplate: rails\n", "unknown template"},
		{"bad php", "schema: 1\nname: x\ntemplate: plain\nphp: latest\n", "invalid php version"},
		{"unknown field", "schema: 1\nname: x\ntemplate: plain\nbogus: 1\n", "bogus"},
		{"unknown engine", "schema: 1\nname: x\ntemplate: plain\nservices:\n  db:\n    engine: mongo\n", "unknown engine"},
		{"bad mode", "schema: 1\nname: x\ntemplate: plain\nservices:\n  db:\n    engine: redis\n    mode: global\n", "invalid mode"},
		{"redis database", "schema: 1\nname: x\ntemplate: plain\nservices:\n  r:\n    engine: redis\n    database: x\n", "'database' is not valid"},
		{"wordpress needs db", "schema: 1\nname: x\ntemplate: wordpress\n", "requires a database service"},
		{"wordpress postgres", "schema: 1\nname: x\ntemplate: wordpress\nservices:\n  db:\n    engine: postgres\n", "requires a database service"},
		{"service key app", "schema: 1\nname: x\ntemplate: plain\nservices:\n  app:\n    engine: redis\n", "collides with the site web container"},
		{"site with containers", "schema: 1\nname: x\ntemplate: plain\ncontainers:\n  web:\n    image: nginx\n", "only valid for type: app"},
		{"app without containers", "schema: 1\nname: x\ntype: app\n", "'containers' is required"},
		{"app with template", "schema: 1\nname: x\ntype: app\ntemplate: laravel\ncontainers:\n  web:\n    image: nginx\n", "container-level fields"},
		{"container no source", "schema: 1\nname: x\ntype: app\ncontainers:\n  web: {}\n", "needs one of"},
		{"container template+image", "schema: 1\nname: x\ntype: app\ncontainers:\n  web:\n    template: laravel\n    image: nginx\n", "cannot be combined"},
		{"routed raw without port", "schema: 1\nname: x\ntype: app\ncontainers:\n  web:\n    image: nginx\n    domain: x\n", "'port' is required"},
		{"container service collision", "schema: 1\nname: x\ntype: app\ncontainers:\n  db:\n    image: nginx\nservices:\n  db:\n    engine: redis\n", "collides with a container key"},
		{"bad env key", "schema: 1\nname: x\ntemplate: plain\nenv:\n  9BAD: x\n", "invalid env key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseErr(t, tc.src, tc.want)
		})
	}
}

func TestDatabaseService(t *testing.T) {
	m := parse(t, `
schema: 1
name: x
template: laravel
services:
  cache:
    engine: redis
  maindb:
    engine: mariadb
`)
	key, svc, ok := m.DatabaseService()
	if !ok || key != "maindb" || svc.Engine != "mariadb" {
		t.Fatalf("DatabaseService() = %q, %+v, %v", key, svc, ok)
	}
	if _, _, ok := m.DatabaseService("postgres"); ok {
		t.Fatal("DatabaseService(postgres) should not match mariadb")
	}
}
