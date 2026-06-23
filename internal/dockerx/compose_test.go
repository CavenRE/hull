package dockerx

import (
	"context"
	"strings"
	"testing"
)

// TestComposeArgs verifies the docker-compose argument assembly (files,
// profiles, verbs) via an injected Runner — no real docker needed.
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
	_ = plain.DownVolumes(context.Background())
	if want := "docker compose down -v"; strings.Join(got, " ") != want {
		t.Errorf("DownVolumes args = %q, want %q", strings.Join(got, " "), want)
	}
	_ = plain.ExecNoTTY(context.Background(), "web", "php", "artisan", "migrate")
	if want := "docker compose exec -T web php artisan migrate"; strings.Join(got, " ") != want {
		t.Errorf("ExecNoTTY args = %q, want %q", strings.Join(got, " "), want)
	}
}
