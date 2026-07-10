package dockerx

import (
	"context"
	"strings"
	"testing"
)

// TestComposeArgs verifies the docker-compose argument assembly (files,
// profiles, verbs) via an injected Runner , no real docker needed.
func TestComposeArgs(t *testing.T) {
	var got []string
	fake := func(_ context.Context, _ string, name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}

	c := Compose{Dir: "/p", Run: fake, Files: []string{"a.yml", "b.yml"}, Profiles: []string{"dev"}}
	if err := c.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := "docker compose -f a.yml -f b.yml --profile dev up -d"; strings.Join(got, " ") != want {
		t.Errorf("Up args = %q, want %q", strings.Join(got, " "), want)
	}

	plain := Compose{Run: fake}
	_ = plain.Build(context.Background(), true)
	if want := "docker compose build --no-cache"; strings.Join(got, " ") != want {
		t.Errorf("Build(noCache) args = %q, want %q", strings.Join(got, " "), want)
	}

	// Name pins the project via -p (so spaced/capitalized dirs don't decide it).
	named := Compose{Run: fake, Name: "my-app"}
	_ = named.Up(context.Background())
	if want := "docker compose -p my-app up -d"; strings.Join(got, " ") != want {
		t.Errorf("named Up args = %q, want %q", strings.Join(got, " "), want)
	}
	_ = plain.DownVolumes(context.Background())
	if want := "docker compose down -v"; strings.Join(got, " ") != want {
		t.Errorf("DownVolumes args = %q, want %q", strings.Join(got, " "), want)
	}
	_ = plain.ExecNoTTY(context.Background(), "web", "php", "artisan", "migrate")
	if want := "docker compose exec -T web php artisan migrate"; strings.Join(got, " ") != want {
		t.Errorf("ExecNoTTY args = %q, want %q", strings.Join(got, " "), want)
	}

	// EnvFile is passed as a global --env-file option before the subcommand.
	withEnv := Compose{Run: fake, Name: "stack", EnvFile: "/repo/.env"}
	_ = withEnv.Up(context.Background())
	if want := "docker compose -p stack --env-file /repo/.env up -d"; strings.Join(got, " ") != want {
		t.Errorf("env-file Up args = %q, want %q", strings.Join(got, " "), want)
	}

	// Recreate forces a clean container rebuild.
	_ = plain.Recreate(context.Background())
	if want := "docker compose up -d --force-recreate"; strings.Join(got, " ") != want {
		t.Errorf("Recreate args = %q, want %q", strings.Join(got, " "), want)
	}
}

// TestComposePortArgs guards the ingress: hull 502 regression: Port MUST reuse
// the SAME project identity (-p/-f/--env-file/--profile) Up used. A bare
// `docker compose port` in the dir lets docker re-derive a different project
// name for adopted clusters and report the service as not running.
func TestComposePortArgs(t *testing.T) {
	c := Compose{
		Dir:      "/p/core",
		Name:     "my_cluster",
		Files:    []string{"docker-compose.yml", "override.yml"},
		Profiles: []string{"prod"},
		EnvFile:  "/p/.env",
	}
	got := strings.Join(c.args("port", "edge_router", "8080"), " ")
	want := "compose -p my_cluster --env-file /p/.env -f docker-compose.yml -f override.yml --profile prod port edge_router 8080"
	if got != want {
		t.Errorf("Port args = %q, want %q", got, want)
	}
}

func TestParsePublishedPort(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"127.0.0.1:55001", 55001, true},
		{"0.0.0.0:80\n[::]:80", 80, true},
		{"", 0, false},
		{"garbage", 0, false},
		{"127.0.0.1:0", 0, false},
	}
	for _, tc := range cases {
		got, err := parsePublishedPort(tc.in)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("parsePublishedPort(%q) = %d, %v; want %d, nil", tc.in, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("parsePublishedPort(%q) = %d, nil; want error", tc.in, got)
		}
	}
}
