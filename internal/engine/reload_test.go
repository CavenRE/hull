package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

type piped struct {
	args  string
	stdin string
}

func pipeEngine(t *testing.T, reply string, err error) (*Engine, *[]piped, *[]string) {
	t.Helper()
	var pipes []piped
	var runs []string
	e := New(&config.Config{TLD: "test", HullHome: t.TempDir()})
	e.Pipe = func(_ context.Context, _ string, stdin io.Reader, name string, args ...string) (string, error) {
		in, _ := io.ReadAll(stdin)
		pipes = append(pipes, piped{args: name + " " + strings.Join(args, " "), stdin: string(in)})
		return reply, err
	}
	e.Run = func(_ context.Context, _, name string, args ...string) error {
		runs = append(runs, name+" "+strings.Join(args, " "))
		return nil
	}
	return e, &pipes, &runs
}

func laravelSite(dir string) *state.Project {
	return &state.Project{Name: "shop", Dir: dir, Manifest: &manifest.Manifest{Schema: 1, Name: "shop", Type: manifest.TypeSite, Template: "laravel"}}
}

func TestRefreshSendsContainerPathsToTheHelper(t *testing.T) {
	e, pipes, _ := pipeEngine(t, "hull-opcache: ok invalidated=1\n", nil)
	dir := t.TempDir()
	p := laravelSite(dir)
	files := []string{filepath.Join(dir, "app", "User.php"), filepath.Join(filepath.Dir(dir), "elsewhere.php")}
	if err := e.RefreshQuiet(context.Background(), p, files); err != nil {
		t.Fatal(err)
	}
	if len(*pipes) != 1 {
		t.Fatalf("pipes = %v", *pipes)
	}
	got := (*pipes)[0]
	if !strings.Contains(got.args, "compose -p shop -f compose.yaml exec -T app php /opt/hull/hull-opcache-cli.php invalidate") {
		t.Errorf("args = %s", got.args)
	}
	if got.stdin != "/var/www/html/app/User.php" {
		t.Errorf("stdin = %q, want only the file inside the project, as the container sees it", got.stdin)
	}
}

func TestResetFallsBackToReloadingTheServer(t *testing.T) {
	e, pipes, runs := pipeEngine(t, "", errors.New("php: could not open /opt/hull/hull-opcache-cli.php"))
	if err := e.ResetQuiet(context.Background(), laravelSite(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if len(*pipes) != 1 || !strings.HasSuffix((*pipes)[0].args, " reset") {
		t.Errorf("pipes = %v", *pipes)
	}
	if len(*runs) != 1 || !strings.Contains((*runs)[0], "kill -USR2") {
		t.Errorf("a container without the helper must get the old server reload, runs = %v", *runs)
	}
}

func TestPrecompileListsSourcesAndCountsBatches(t *testing.T) {
	e, pipes, _ := pipeEngine(t, "hull-opcache: ok compiled=250\nhull-opcache: ok compiled=3\n", nil)
	dir := t.TempDir()
	for _, f := range []string{
		"public/index.php", "app/User.php", "routes/web.php",
		"vendor/laravel/x.php", "node_modules/a/b.php", "storage/framework/views/c.php",
		"resources/views/home.blade.php", "tests/Unit/T.php", ".git/hooks/h.php", "README.md",
	} {
		path := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<?php\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := e.PrecompileQuiet(context.Background(), laravelSite(dir))
	if err != nil || n != 253 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	got := strings.Split((*pipes)[0].stdin, "\n")
	slices.Sort(got)
	want := []string{"/var/www/html/app/User.php", "/var/www/html/public/index.php", "/var/www/html/routes/web.php"}
	if !slices.Equal(got, want) {
		t.Errorf("compiled %v, want %v", got, want)
	}
}

func TestWatchSkipFollowsNamedVolumes(t *testing.T) {
	if got := WatchSkip(laravelSite(t.TempDir())); !slices.Equal(got, []string{"vendor"}) {
		t.Errorf("laravel = %v", got)
	}
	wp := &state.Project{Manifest: &manifest.Manifest{Template: "wordpress"}}
	if got := WatchSkip(wp); !slices.Equal(got, []string{"wp-content/cache"}) {
		t.Errorf("wordpress = %v, want only its cache plugins' folder", got)
	}
}
