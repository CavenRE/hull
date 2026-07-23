package dockerx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// engineWaitTimeout is how long EnsureEngine waits for a freshly launched
// engine to answer. Docker Desktop routinely takes 30 to 60 seconds on a cold
// start, so this is deliberately generous.
const engineWaitTimeout = 2 * time.Minute

// enginePollInterval is how often we re-probe while waiting.
const enginePollInterval = 2 * time.Second

// probeTimeout bounds a single reachability probe. Without it the probe
// inherits the caller's context, which for the CLI has no deadline, so a wedged
// named pipe or socket hangs the command forever and the overall wait cap below
// never gets a chance to apply.
const probeTimeout = 5 * time.Second

// engineProbe caches the last positive probe for the life of the process. A
// single compose sequence shells out to docker a dozen times, and re-probing
// before each one would add real latency for no new information. Only a
// positive result is cached: once the engine is up it does not spontaneously go
// down mid-command, whereas a negative must stay live so the poll loop in
// EnsureEngine can observe Docker finishing its startup.
var engineProbe struct {
	sync.Mutex
	up bool
}

// engineResponds runs one bounded probe against the engine.
func engineResponds(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := Output(ctx, "", "docker", "version", "--format", "{{.Server.Version}}")
	return err == nil
}

// engineReachable is the cached form used by the hot paths.
func engineReachable(ctx context.Context) bool {
	engineProbe.Lock()
	defer engineProbe.Unlock()
	if engineProbe.up {
		return true
	}
	engineProbe.up = engineResponds(ctx)
	return engineProbe.up
}

// markEngineUp records a confirmed-live engine so later calls skip the probe.
func markEngineUp() {
	engineProbe.Lock()
	engineProbe.up = true
	engineProbe.Unlock()
}

// mayStartEngine reports whether Hull is allowed to launch the engine itself.
// Launching takes minutes and needs a desktop session, so an automated context
// must fail fast instead: without this a CI job with no Docker would spend the
// full wait on EVERY command rather than erroring in a second.
func mayStartEngine() bool {
	if os.Getenv("HULL_NO_ENGINE_START") != "" {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	return stdinIsTerminal()
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
		return fmt.Errorf("%w: install Docker (or Podman with docker compatibility) and try again", ErrEngineMissing)
	}
	if engineReachable(ctx) {
		return nil
	}
	// No desktop session (CI, a script, a pipe): launching is either impossible
	// or would burn the full wait on every command. Fail fast and actionably.
	if !mayStartEngine() {
		return fmt.Errorf("%w: %s", ErrEngineDown, EngineDownHint)
	}

	what, err := startEngine(ctx)
	if err != nil {
		return fmt.Errorf("%w, and Hull could not start it: %v", ErrEngineDown, err)
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
			markEngineUp()
			log(what + " is ready.")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become ready within %s: start it manually and try again", what, engineWaitTimeout)
		}
	}
}
