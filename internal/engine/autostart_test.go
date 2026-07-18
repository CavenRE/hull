package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

// TestStartEnabledSelectsMarkedItems verifies boot bring-up starts only the
// projects and instances marked for autostart, and crucially does NOT run a
// project's lifecycle hooks (a resume must not re-run migrations/build steps).
func TestStartEnabledSelectsMarkedItems(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	// Project marked autostart, with a post_up hook that must NOT run on boot.
	dirOn := filepath.Join(root, "blog")
	mustWrite(t, dirOn, "schema: 1\nname: blog\ntemplate: plain\nautostart: true\n"+
		"hooks:\n  post_up:\n    - echo HOOK_SHOULD_NOT_RUN\n")

	// Project not marked: must be skipped.
	dirOff := filepath.Join(root, "idle")
	mustWrite(t, dirOff, "schema: 1\nname: idle\ntemplate: plain\n")

	// A shared instance listed in services.autostart.
	instDir := filepath.Join(home, "services", "redis-alpine")
	if err := os.MkdirAll(instDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, "compose.yaml"),
		[]byte("name: hull-redis-alpine\nservices:\n  redis:\n    image: redis\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{TLD: "test", Roots: []string{root}, HullHome: home}
	cfg.Services.Autostart = []string{"redis-alpine"}
	e := New(cfg)
	var calls []string
	e.Run = func(ctx context.Context, dir, name string, args ...string) error {
		calls = append(calls, dir+" "+name+" "+strings.Join(args, " "))
		return nil
	}
	e.EnsureNet = func(ctx context.Context, name string) error { return nil }

	started, err := e.StartEnabled(context.Background())
	if err != nil {
		t.Fatalf("StartEnabled errored: %v", err)
	}
	if started != 2 {
		t.Errorf("started = %d, want 2 (blog + redis-alpine)", started)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, dirOn) || !strings.Contains(joined, "compose up -d") {
		t.Errorf("autostart project not brought up; calls:\n%s", joined)
	}
	if strings.Contains(joined, dirOff) {
		t.Errorf("non-autostart project must be skipped; calls:\n%s", joined)
	}
	if !strings.Contains(joined, instDir) {
		t.Errorf("autostart instance not started; calls:\n%s", joined)
	}
	if strings.Contains(joined, "HOOK_SHOULD_NOT_RUN") {
		t.Errorf("post_up hook must NOT run on autostart boot; calls:\n%s", joined)
	}
}

// TestSetProjectFieldsAutostart checks the autostart flag round-trips through
// the manifest.
func TestSetProjectFieldsAutostart(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "app")
	mustWrite(t, dir, "schema: 1\nname: app\ntemplate: plain\n")
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	e := New(&config.Config{TLD: "test", Roots: []string{root}, HullHome: t.TempDir()})
	e.Run = func(ctx context.Context, dir, name string, args ...string) error { return nil }
	e.EnsureNet = func(ctx context.Context, name string) error { return nil }
	p := &state.Project{Name: "app", Dir: dir, Manifest: m}

	on := true
	if err := e.SetProjectFields(p, PatchOptions{Autostart: &on}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Autostarts() {
		t.Error("autostart: true did not persist to the manifest")
	}
}

func mustWrite(t *testing.T, dir, manifestYAML string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}
