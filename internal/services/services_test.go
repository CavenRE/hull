package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	want := `docker exec hull-postgres-16 psql -U postgres -c CREATE DATABASE "my_app"`
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

func TestCreateDatabaseRejectsRedis(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if err := m.CreateDatabase(context.Background(), "redis", "", "x"); err == nil {
		t.Error("redis should not host databases")
	}
}
