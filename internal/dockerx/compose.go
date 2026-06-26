package dockerx

import (
	"context"
	"strings"
)

// Compose drives `docker compose` for one project directory.
type Compose struct {
	Dir string
	// Run executes commands; defaults to Exec when nil (tests inject).
	Run Runner
	// Name pins the compose project name via `-p`. When empty, docker derives
	// it from the directory basename , which breaks for dirs with spaces or
	// capitals. Set it to a slug so the project identity is deterministic.
	Name string
	// Files are extra `-f` compose files (for wrapped cluster stacks). Empty
	// means docker auto-detects compose.yaml/docker-compose.yml in Dir.
	Files []string
	// Profiles are `--profile` selections (cluster dev/test/prod gating).
	Profiles []string
}

func (c Compose) run(ctx context.Context, args ...string) error {
	runner := c.Run
	if runner == nil {
		runner = Exec
	}
	full := []string{"compose"}
	if c.Name != "" {
		full = append(full, "-p", c.Name)
	}
	for _, f := range c.Files {
		full = append(full, "-f", f)
	}
	for _, p := range c.Profiles {
		full = append(full, "--profile", p)
	}
	full = append(full, args...)
	return runner(ctx, c.Dir, "docker", full...)
}

// Up starts the project detached.
func (c Compose) Up(ctx context.Context) error {
	return c.run(ctx, "up", "-d")
}

// Down stops and removes the project's containers.
func (c Compose) Down(ctx context.Context) error {
	return c.run(ctx, "down")
}

// DownVolumes additionally removes named volumes (destructive).
func (c Compose) DownVolumes(ctx context.Context) error {
	return c.run(ctx, "down", "-v")
}

// Build (re)builds the project's images. noCache forces a clean rebuild.
func (c Compose) Build(ctx context.Context, noCache bool) error {
	args := []string{"build"}
	if noCache {
		args = append(args, "--no-cache")
	}
	return c.run(ctx, args...)
}

// Restart restarts the project's containers.
func (c Compose) Restart(ctx context.Context) error {
	return c.run(ctx, "restart")
}

// Volumes lists the project's named volumes (for a destructive-reset preview).
func (c Compose) Volumes(ctx context.Context) ([]string, error) {
	out, err := Output(ctx, c.Dir, "docker", "compose", "config", "--volumes")
	if err != nil {
		return nil, err
	}
	var vols []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			vols = append(vols, line)
		}
	}
	return vols, nil
}

// Logs tails the project's logs.
func (c Compose) Logs(ctx context.Context, follow bool) error {
	if follow {
		return c.run(ctx, "logs", "-f")
	}
	return c.run(ctx, "logs")
}

// ExecIn runs a command inside a service container (interactive TTY).
func (c Compose) ExecIn(ctx context.Context, service string, cmd ...string) error {
	return c.run(ctx, append([]string{"exec", service}, cmd...)...)
}

// ExecNoTTY runs a command inside a service container without a TTY , for
// programmatic/daemon use (interactive `compose exec` fails when stdin is not
// a terminal). Used by post-create steps like `artisan migrate`.
func (c Compose) ExecNoTTY(ctx context.Context, service string, cmd ...string) error {
	return c.run(ctx, append([]string{"exec", "-T", service}, cmd...)...)
}
