package dockerx

import (
	"context"
	"fmt"
	"strconv"
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
	// EnvFile, when set, is passed as `--env-file` so ${VAR} interpolation in
	// the compose file resolves from a chosen env file (adopted clusters).
	EnvFile string
}

// args builds the full `compose …` argument list, prefixing this project's
// identity flags (-p/-f/--env-file/--profile) before the given subcommand args.
// Every compose invocation for a project MUST go through here so a lookup can
// never resolve a different project name than the one `up` used.
func (c Compose) args(sub ...string) []string {
	full := []string{"compose"}
	if c.Name != "" {
		full = append(full, "-p", c.Name)
	}
	if c.EnvFile != "" {
		full = append(full, "--env-file", c.EnvFile)
	}
	for _, f := range c.Files {
		full = append(full, "-f", f)
	}
	for _, p := range c.Profiles {
		full = append(full, "--profile", p)
	}
	return append(full, sub...)
}

func (c Compose) run(ctx context.Context, args ...string) error {
	runner := c.Run
	if runner == nil {
		runner = Exec
	}
	return runner(ctx, c.Dir, "docker", c.args(args...)...)
}

// Port returns the host port docker published for a service's container port,
// using the SAME project identity (-p/-f/--env-file/--profile) this project was
// brought up with. A bare `docker compose port` in the directory would let
// docker re-derive the project name from the dir basename , which diverges from
// the pinned -p name for adopted clusters (underscores, compose-file `name:`,
// ComposeRoot subdirs) and reports the service as "not running", silently 502ing
// ingress: hull routes. Captures stdout (the streaming run() writes to os.Stdout).
func (c Compose) Port(ctx context.Context, service string, containerPort int) (int, error) {
	out, err := Output(ctx, c.Dir, "docker", c.args("port", service, strconv.Itoa(containerPort))...)
	if err != nil {
		return 0, err
	}
	return parsePublishedPort(out)
}

// parsePublishedPort extracts the host port from `docker compose port` output
// like "127.0.0.1:55001" (first line wins).
func parsePublishedPort(out string) (int, error) {
	line, _, _ := strings.Cut(out, "\n")
	idx := strings.LastIndex(strings.TrimSpace(line), ":")
	if idx < 0 {
		return 0, fmt.Errorf("unexpected docker compose port output %q", out)
	}
	var port int
	if _, err := fmt.Sscanf(line[idx+1:], "%d", &port); err != nil || port == 0 {
		return 0, fmt.Errorf("unexpected docker compose port output %q", out)
	}
	return port, nil
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

// Recreate brings the project up, forcing containers to be recreated even when
// their config is unchanged , the repair for a container left with drifted
// config or detached networks, which plain Restart cannot fix.
func (c Compose) Recreate(ctx context.Context) error {
	return c.run(ctx, "up", "-d", "--force-recreate")
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
