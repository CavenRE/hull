package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/dbauth"
)

type fake struct {
	commands []string
	outputs  []string
	networks []string
	outErr   error
}

func (f *fake) manager(home string) *Manager {
	return &Manager{
		HullHome: home,
		Run: func(ctx context.Context, dir, name string, args ...string) error {
			f.commands = append(f.commands, name+" "+strings.Join(args, " "))
			return nil
		},
		Output: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			f.outputs = append(f.outputs, name+" "+strings.Join(args, " "))
			return "", f.outErr
		},
		EnsureNet: func(ctx context.Context, name string) error {
			f.networks = append(f.networks, name)
			return nil
		},
		RunningProjects: func(ctx context.Context) ([]string, error) {
			return []string{"hull-postgres-16"}, nil
		},
	}
}

func TestResolve(t *testing.T) {
	def, version, err := Resolve("postgres@14")
	if err != nil || def.Name != "postgres" || version != "14" {
		t.Errorf("Resolve(postgres@14) = %s %s %v", def.Name, version, err)
	}
	def, version, err = Resolve("mariadb")
	if err != nil || version != "lts" {
		t.Errorf("Resolve(mariadb) = %s %s %v", def.Name, version, err)
	}
	if _, _, err := Resolve("mongo"); err == nil {
		t.Error("Resolve(mongo) should fail")
	}
}

func TestAddRendersAndBoots(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	name, err := m.Add(context.Background(), "postgres", "16")
	if err != nil {
		t.Fatal(err)
	}
	if name != "postgres-16" {
		t.Errorf("name = %s", name)
	}
	data, err := os.ReadFile(filepath.Join(m.Dir(name), "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"name: hull-postgres-16",
		"container_name: hull-postgres-16",
		"image: postgres:16-alpine",
		"data:/var/lib/postgresql/data",
		"external: true",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("compose missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "POSTGRES_DB=") {
		t.Error("shared instance must not pin POSTGRES_DB")
	}
	if len(f.networks) != 1 || f.networks[0] != "caddy" {
		t.Errorf("networks = %v", f.networks)
	}
	if len(f.commands) != 1 || !strings.HasSuffix(f.commands[0], "compose up -d") {
		t.Errorf("commands = %v", f.commands)
	}
}

func TestListWithRunningState(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if _, err := m.Add(context.Background(), "postgres", "16"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), "redis", ""); err != nil {
		t.Fatal(err)
	}
	instances, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %+v", instances)
	}
	if instances[0].Name != "postgres-16" || !instances[0].Running {
		t.Errorf("postgres instance = %+v", instances[0])
	}
	if instances[1].Name != "redis" || instances[1].Running {
		t.Errorf("redis instance = %+v", instances[1])
	}
}

func TestLifecycleRequiresExistingInstance(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if err := m.Start(context.Background(), "ghost"); err == nil {
		t.Error("Start(ghost) should fail")
	}
	if err := m.Stop(context.Background(), "ghost"); err == nil {
		t.Error("Stop(ghost) should fail")
	}
	if err := m.Remove(context.Background(), "ghost"); err == nil {
		t.Error("Remove(ghost) should fail")
	}
}

func TestRemoveDeletesDir(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	name, err := m.Add(context.Background(), "redis", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Dir(name)); !os.IsNotExist(err) {
		t.Error("instance dir not removed")
	}
	last := f.commands[len(f.commands)-1]
	if !strings.HasSuffix(last, "compose down -v") {
		t.Errorf("last command = %s", last)
	}
}

func TestCreateDatabasePostgres(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if err := m.CreateDatabase(context.Background(), "postgres", "16", "my_app"); err != nil {
		t.Fatal(err)
	}
	want := `docker exec hull-postgres-16 psql -v ON_ERROR_STOP=1 -U postgres -c CREATE DATABASE "my_app"`
	if len(f.outputs) != 1 || f.outputs[0] != want {
		t.Errorf("outputs = %v, want %s", f.outputs, want)
	}
}

func TestCreateDatabaseMariaDB(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if err := m.CreateDatabase(context.Background(), "mariadb", "lts", "my_app"); err != nil {
		t.Fatal(err)
	}
	want := "docker exec hull-mariadb-lts mariadb -u root -e CREATE DATABASE IF NOT EXISTS `my_app`"
	if len(f.outputs) != 1 || f.outputs[0] != want {
		t.Errorf("outputs = %v, want %s", f.outputs, want)
	}
}

func TestStablePortPersistsAcrossReAdd(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if _, err := m.Add(context.Background(), "postgres", "16"); err != nil {
		t.Fatal(err)
	}
	instances, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := instances[0].HostPort
	if first < 54320 || first >= 54520 {
		t.Fatalf("postgres host port = %d, want 54320..", first)
	}

	// Re-add must keep the same port.
	if _, err := m.Add(context.Background(), "postgres", "16"); err != nil {
		t.Fatal(err)
	}
	instances, _ = m.List(context.Background())
	if instances[0].HostPort != first {
		t.Errorf("port moved on re-add: %d -> %d", first, instances[0].HostPort)
	}

	// A second instance gets a different port.
	if _, err := m.Add(context.Background(), "postgres", "14"); err != nil {
		t.Fatal(err)
	}
	instances, _ = m.List(context.Background())
	ports := map[int]bool{}
	for _, in := range instances {
		ports[in.HostPort] = true
	}
	if len(ports) != 2 {
		t.Errorf("instances share a host port: %+v", instances)
	}
}

func TestAddAdminerMountsPluginNoVolume(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	name, err := m.Add(context.Background(), "adminer", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.Dir(name), "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "hull-login.php:/var/www/html/plugins-enabled/hull-login.php:ro") {
		t.Errorf("plugin mount missing:\n%s", text)
	}
	if strings.Contains(text, "data:") {
		t.Errorf("adminer should have no data volume:\n%s", text)
	}
	if !strings.Contains(text, "127.0.0.1::8080") {
		t.Errorf("UI port missing:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(m.HullHome, "system", "adminer", "hull-login.php")); err != nil {
		t.Error("plugin file not provisioned")
	}
}

func TestAddSearchAndStorageEngines(t *testing.T) {
	cases := []struct {
		engine    string
		wantImage string
		wantCmd   string
		wantUI    bool
	}{
		{"meilisearch", "getmeili/meilisearch:v1.11", "", true},
		{"typesense", "typesense/typesense:27.1", "--data-dir /data --enable-cors", false},
		{"minio", "minio/minio:latest", "server /data --console-address :9001", true},
		{"memcached", "memcached:alpine", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.engine, func(t *testing.T) {
			f := &fake{}
			m := f.manager(t.TempDir())
			name, err := m.Add(context.Background(), tc.engine, "")
			if err != nil {
				t.Fatal(err)
			}
			if name != tc.engine {
				t.Errorf("instance name = %q, want clean %q", name, tc.engine)
			}
			data, err := os.ReadFile(filepath.Join(m.Dir(name), "compose.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.Contains(text, "image: "+tc.wantImage) {
				t.Errorf("missing image %q:\n%s", tc.wantImage, text)
			}
			if tc.wantCmd != "" && !strings.Contains(text, tc.wantCmd) {
				t.Errorf("missing command %q:\n%s", tc.wantCmd, text)
			}
			if tc.wantUI && !strings.Contains(text, "127.0.0.1::") {
				t.Errorf("expected a UI port publish:\n%s", text)
			}
			for _, fixed := range []string{"hullMasterKey", "hullTypesenseKey", "hullsecret"} {
				if strings.Contains(text, fixed) {
					t.Errorf("a new instance got the fixed key %s:\n%s", fixed, text)
				}
			}
		})
	}
}

func TestStartForceRecreatesAdminerOnly(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if _, err := m.Add(context.Background(), "adminer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), "postgres", "16"); err != nil {
		t.Fatal(err)
	}
	// Add must stay a plain idempotent up so routine EnsureAdminer calls never
	// bounce a healthy console; only an explicit Start force-recreates.
	for _, c := range f.commands {
		if strings.Contains(c, "--force-recreate") {
			t.Errorf("Add force-recreated (churn regression): %s", c)
		}
	}

	f.commands = nil
	if err := m.Start(context.Background(), "adminer"); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || f.commands[0] != "docker compose up -d --force-recreate" {
		t.Errorf("start adminer = %v, want force-recreate", f.commands)
	}

	f.commands = nil
	if err := m.Start(context.Background(), "postgres-16"); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || f.commands[0] != "docker compose up -d" {
		t.Errorf("start postgres = %v, want plain up", f.commands)
	}
}

func TestCreateDatabaseRejectsRedis(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if err := m.CreateDatabase(context.Background(), "redis", "", "x"); err == nil {
		t.Error("redis should not host databases")
	}
}

func TestNewInstanceGetsLoginsOldOneKeepsRoot(t *testing.T) {
	f := &fake{}
	home := t.TempDir()
	m := f.manager(home)
	if _, err := m.Add(context.Background(), "postgres", "16"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(m.Dir("postgres-16"), "compose.yaml"))
	if strings.Contains(string(data), "trust") || !strings.Contains(string(data), "POSTGRES_PASSWORD=") {
		t.Errorf("new postgres instance env:\n%s", data)
	}
	if _, err := m.Add(context.Background(), "mysql", "8.0"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(m.Dir("mysql-8.0"), "compose.yaml"))
	if !strings.Contains(string(data), "MYSQL_ROOT_HOST=localhost") {
		t.Errorf("new mysql instance env:\n%s", data)
	}

	// An instance made before logins existed: its compose file is there, its
	// root login is not. Re-adding it must not switch its auth.
	old := m.Dir("mariadb-lts")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), "mariadb", "lts"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(old, "compose.yaml"))
	if !strings.Contains(string(data), "MYSQL_ALLOW_EMPTY_PASSWORD=yes") || strings.Contains(string(data), "MYSQL_ROOT_HOST") {
		t.Errorf("old instance auth changed:\n%s", data)
	}
	instances, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range instances {
		if in.Logins != (in.Name != "mariadb-lts") {
			t.Errorf("%s: Logins = %v", in.Name, in.Logins)
		}
	}
}

func TestCreateDatabaseMakesProjectUser(t *testing.T) {
	f := &fake{}
	home := t.TempDir()
	m := f.manager(home)
	if _, err := m.Add(context.Background(), "mysql", "8.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), "postgres", "16"); err != nil {
		t.Fatal(err)
	}
	f.outputs = nil
	if err := m.CreateDatabase(context.Background(), "mysql", "8.0", "shop"); err != nil {
		t.Fatal(err)
	}
	login, ok := dbauth.Get(home, "instance/mysql-8.0", "shop")
	if !ok || login.User != "shop" {
		t.Fatalf("no project login stored: %+v", login)
	}
	if len(f.outputs) != 1 || !strings.Contains(f.outputs[0], "CREATE USER IF NOT EXISTS 'shop'@'%' IDENTIFIED BY '"+login.Password+"'") ||
		!strings.Contains(f.outputs[0], "GRANT ALL PRIVILEGES ON `shop`.* TO 'shop'@'%'") {
		t.Errorf("mysql = %v", f.outputs)
	}
	f.outputs = nil
	if err := m.CreateDatabase(context.Background(), "postgres", "16", "shop"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.outputs, "\n")
	if !strings.Contains(joined, `CREATE ROLE "shop" LOGIN`) || !strings.Contains(joined, `CREATE DATABASE "shop" OWNER "shop"`) {
		t.Errorf("postgres = %v", f.outputs)
	}
	if err := m.Remove(context.Background(), "mysql-8.0"); err != nil {
		t.Fatal(err)
	}
	if _, ok := dbauth.Get(home, "instance/mysql-8.0", "shop"); ok {
		t.Error("Remove kept the instance's logins")
	}
}

func TestInstanceNamesCannotLeaveTheServicesFolder(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	for _, bad := range []string{"../evil", "a/b", `a\b`, "..", "x..y"} {
		if err := m.Remove(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "invalid instance name") {
			t.Errorf("Remove(%q) = %v", bad, err)
		}
	}
	if _, _, err := Resolve("mysql@../../x"); err == nil {
		t.Error("a version with a path in it was accepted")
	}
	if len(f.commands) != 0 {
		t.Errorf("commands ran: %v", f.commands)
	}
}

func TestSQLQuoting(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{pgIdent(`a"b`), `"a""b"`},
		{pgLiteral(`it's`), `'it''s'`},
		{pgLiteral(`a\'b`), `'a\''b'`},
		{myIdent("a`b"), "`a``b`"},
		{myLiteral(`it's`), `'it''s'`},
		{myLiteral(`a\'`), `'a\\'''`},
	} {
		if c.got != c.want {
			t.Errorf("got %s, want %s", c.got, c.want)
		}
	}
}
