package dockerx

import (
	"errors"
	"fmt"
	"strings"
)

// ErrEngineDown means the container engine is installed but not reachable
// (Docker Desktop closed, the daemon stopped, the socket or named pipe absent).
// Callers test it with errors.Is to tell "your engine is off" apart from a real
// command failure, so the user gets an instruction instead of a transport dump.
var ErrEngineDown = errors.New("the container engine is not running")

// ErrEngineMissing means there is no docker command on PATH at all. Hull can
// start a closed engine, but it will not install one.
var ErrEngineMissing = errors.New("the 'docker' command was not found in PATH")

// EngineDownHint is the actionable half of an engine-down message. Kept in one
// place so the CLI, the daemon, and doctor all say the same thing.
const EngineDownHint = "start Docker (or your Docker-compatible engine) and try again"

// engineDownMarkers are substrings docker/compose emit when they cannot reach
// the engine. They cover the Windows named pipe, the unix socket, and the
// wrapped "error during connect" form that compose adds.
var engineDownMarkers = []string{
	"cannot connect to the docker daemon",
	"failed to connect to the docker api",
	"error during connect",
	"docker daemon is not running",
	"the system cannot find the file specified", // npipe on Windows
	"open //./pipe/docker",
	"/var/run/docker.sock",
	"is the docker daemon running",
}

// looksLikeEngineDown reports whether output is docker complaining that it
// cannot reach the engine, as opposed to a genuine command failure.
func looksLikeEngineDown(output string) bool {
	low := strings.ToLower(output)
	for _, m := range engineDownMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// commandError builds the single error shape every docker shell-out returns.
// When the underlying text is the engine being unreachable it collapses to
// ErrEngineDown, so a closed Docker never reaches the user as a transport dump
// like "open //./pipe/dockerDesktopLinuxEngine: The system cannot find the file
// specified." Everything else keeps docker's own message, which is the useful
// part of a real failure.
func commandError(name string, args []string, stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	if looksLikeEngineDown(msg) {
		return fmt.Errorf("%w: %s", ErrEngineDown, EngineDownHint)
	}
	if len(msg) > 800 {
		msg = msg[:800] + "..."
	}
	return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
}
