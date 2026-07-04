package manifest

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"My App":         "my-app",
		"Bad_Name":       "bad-name",
		"  spaced  out ": "spaced-out",
		"already-slug":   "already-slug",
		"Foo.Bar_Baz":    "foo-bar-baz",
		"@@@":            "",
		"UPPER":          "upper",
		"a--b__c":        "a-b-c",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServedDefault(t *testing.T) {
	m := &Manifest{}
	if !m.Served() {
		t.Error("nil Serve should default to served")
	}
	no := false
	m.Serve = &no
	if m.Served() {
		t.Error("explicit serve:false should not be served")
	}
}

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
		{"base_domain on site", "schema: 1\nname: x\ntemplate: plain\nbase_domain: y.local\n", "only valid for type: cluster"},
		{"bad ingress", "schema: 1\nname: x\ntype: cluster\ningress: bogus\n", "invalid ingress"},
		{"bad base_domain", "schema: 1\nname: x\ntype: cluster\nbase_domain: under_score\n", "invalid base_domain"},
		{"bad route alias", "schema: 1\nname: x\ntype: cluster\nroutes:\n  api:\n    service: s\n    port: 80\n    aliases: [Bad]\n", "invalid alias"},
		{"dup subdomain", "schema: 1\nname: x\ntype: cluster\nroutes:\n  a:\n    service: s\n    port: 80\n    subdomain: web\n  web:\n    service: t\n    port: 81\n", "both use subdomain"},
		{"alias collides with route", "schema: 1\nname: x\ntype: cluster\nroutes:\n  a:\n    service: s\n    port: 80\n    aliases: [web]\n  web:\n    service: t\n    port: 81\n", "both use subdomain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseErr(t, tc.src, tc.want)
		})
	}
}

func TestClusterRoutesAndURLs(t *testing.T) {
	m := parse(t, `
schema: 1
name: tapkit
type: cluster
compose_root: core
base_domain: tapkit.local
ingress: delegate
routes:
  api:
    service: management_api
    port: 8081
  t:
    service: edge_router
    port: 8080
    aliases: [tap]
`)
	if m.ComposeRoot != "core" {
		t.Errorf("compose_root = %q, want core", m.ComposeRoot)
	}
	if m.Ingress != IngressDelegate {
		t.Errorf("ingress = %q, want delegate", m.Ingress)
	}
	// base_domain wins over the TLD.
	if got := m.ClusterSuffix("test"); got != "tapkit.local" {
		t.Errorf("ClusterSuffix = %q, want tapkit.local", got)
	}
	// Subdomain defaults to the route key.
	if m.Routes["api"].Subdomain != "api" {
		t.Errorf("api subdomain = %q, want api", m.Routes["api"].Subdomain)
	}
	suffix := m.ClusterSuffix("test")
	if got := m.Routes["api"].Hosts(suffix); len(got) != 1 || got[0] != "api.tapkit.local" {
		t.Errorf("api hosts = %v, want [api.tapkit.local]", got)
	}
	// Aliases add hostnames for the same service.
	if got := m.Routes["t"].Hosts(suffix); len(got) != 2 || got[0] != "t.tapkit.local" || got[1] != "tap.tapkit.local" {
		t.Errorf("t hosts = %v, want [t.tapkit.local tap.tapkit.local]", got)
	}
}

func TestClusterSuffixFallsBackToTLD(t *testing.T) {
	m := parse(t, `
schema: 1
name: c
type: cluster
routes:
  api:
    service: web
    port: 80
`)
	if got := m.ClusterSuffix("test"); got != "test" {
		t.Errorf("ClusterSuffix = %q, want test (no base_domain)", got)
	}
	if got := m.Routes["api"].Hosts("test"); len(got) != 1 || got[0] != "api.test" {
		t.Errorf("hosts = %v, want [api.test]", got)
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
