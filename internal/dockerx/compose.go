package dockerx

import "context"

// Compose drives `docker compose` for one project directory.
type Compose struct {
	Dir string
	// Run executes commands; defaults to Exec when nil (tests inject).
	Run Runner
}

func (c Compose) run(ctx context.Context, args ...string) error {
	runner := c.Run
	if runner == nil {
		runner = Exec
	}
	return runner(ctx, c.Dir, "docker", append([]string{"compose"}, args...)...)
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

// Restart restarts the project's containers.
func (c Compose) Restart(ctx context.Context) error {
	return c.run(ctx, "restart")
}

// Logs tails the project's logs.
func (c Compose) Logs(ctx context.Context, follow bool) error {
	if follow {
		return c.run(ctx, "logs", "-f")
	}
	return c.run(ctx, "logs")
}

// ExecIn runs a command inside a service container.
func (c Compose) ExecIn(ctx context.Context, service string, cmd ...string) error {
	return c.run(ctx, append([]string{"exec", service}, cmd...)...)
}
