package dockerx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// engineWaitTimeout is how long EnsureEngine waits for a freshly launched
// engine to answer. Docker Desktop routinely takes 30 to 60 seconds on a cold
// start, so this is deliberately generous.
const engineWaitTimeout = 2 * time.Minute

// enginePollInterval is how often we re-probe while waiting.
const enginePollInterval = 2 * time.Second

// engineResponds reports whether the container engine answers a version query.
func engineResponds(ctx context.Context) bool {
	_, err := Output(ctx, "", "docker", "version", "--format", "{{.Server.Version}}")
	return err == nil
}

// EnsureEngine makes sure the container engine is running: if it is not, Hull
// launches it and waits until it answers. This is the difference between Hull
// refusing to work because Docker is closed and Hull just getting on with it.
//
// It cannot install Docker, so a missing CLI stays a hard error. log receives
// progress lines and may be nil.
func EnsureEngine(ctx context.Context, log func(string)) error {
	if log == nil {
		log = func(string) {}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("the 'docker' command was not found in PATH: install Docker (or Podman with docker compatibility) and try again")
	}
	if engineResponds(ctx) {
		return nil
	}

	what, err := startEngine(ctx)
	if err != nil {
		return fmt.Errorf("the container engine is not running, and Hull could not start it: %w", err)
	}
	log(what + " is not running. Starting it, this can take a minute...")

	deadline := time.Now().Add(engineWaitTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(enginePollInterval):
		}
		if engineResponds(ctx) {
			log(what + " is ready.")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become ready within %s: start it manually and try again", what, engineWaitTimeout)
		}
	}
}
