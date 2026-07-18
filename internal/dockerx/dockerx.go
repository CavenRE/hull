package dockerx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"golang.org/x/term"
)

// Runner executes a command attached to the user's terminal. Tests inject a
// recorder; production uses Exec.
type Runner func(ctx context.Context, dir string, name string, args ...string) error

// Exec runs a command with stdio attached, in dir (empty = inherit cwd).
func Exec(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Interactive use (hull exec, from a terminal) needs the real console;
	// background use (the daemon) must not pop a window per docker call.
	if !stdinIsTerminal() {
		noWindow(cmd)
	}
	return cmd.Run()
}

// stdinIsTerminal reports whether standard input is a real interactive console.
// It must NOT be fooled by the NUL device, which a detached daemon inherits as
// its stdin: NUL is a character device, so the old os.ModeCharDevice test
// returned true inside the daemon and made Exec skip noWindow, flashing a
// console window on every `docker compose up`/`down` (the engine's runner is
// dockerx.Exec). term.IsTerminal uses GetConsoleMode on Windows, so it is true
// only for an actual console and false for NUL, pipes, and files , which is
// exactly the "attach a real TTY" condition Exec wants.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// NoWindow configures cmd so that, on Windows, it runs in its own hidden
// windowless console (CREATE_NO_WINDOW) instead of flashing one; it is a no-op
// on other platforms. Exported for daemon-reachable callers that build their
// own *exec.Cmd outside this package (the job runner, the dependency probe) and
// so cannot reference the platform SysProcAttr flags directly without breaking
// the cross-platform build. Prefer Exec/Output/stream helpers where they fit;
// use this when you need a bespoke command but still must not pop a window.
func NoWindow(cmd *exec.Cmd) { noWindow(cmd) }

// Output runs a command and captures stdout, trimming trailing whitespace.
func Output(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	noWindow(cmd)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

// EngineCheck verifies the container engine is reachable, with an error
// message that says what to do rather than how the probe failed.
func EngineCheck(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("the 'docker' command was not found in PATH: install Docker (or Podman with docker compatibility) and try again")
	}
	if !engineResponds(ctx) {
		return errors.New("the container engine is not responding: is Docker (or your Docker-compatible engine) running?")
	}
	return nil
}

// RunningComposeProjects returns the distinct compose project names with at
// least one running container, sorted.
func RunningComposeProjects(ctx context.Context) ([]string, error) {
	out, err := Output(ctx, "", "docker", "ps", "--format", `{{.Label "com.docker.compose.project"}}`)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			set[name] = true
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// RunningHullProjects returns the distinct compose project names that carry
// Hull's ownership label (com.hull.managed=true) and have a running
// container , the safety sweep for stop-all to catch Hull-rendered orphans
// whose directory it can no longer resolve. Adopted clusters are not rendered
// by Hull and so are not found here (the started ledger covers them).
func RunningHullProjects(ctx context.Context) ([]string, error) {
	out, err := Output(ctx, "", "docker", "ps", "--filter", "label=com.hull.managed=true", "--format", `{{.Label "com.docker.compose.project"}}`)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			set[name] = true
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// ForceRemoveProject removes all containers and volumes labeled with the
// compose project name , the fallback when a compose file is corrupted
// (ported from v1's rm).
func ForceRemoveProject(ctx context.Context, project string) error {
	filter := "label=com.docker.compose.project=" + project
	if out, err := Output(ctx, "", "docker", "ps", "-a", "--filter", filter, "-q"); err == nil && out != "" {
		ids := append([]string{"rm", "-f"}, strings.Fields(out)...)
		if err := Exec(ctx, "", "docker", ids...); err != nil {
			return err
		}
	}
	if out, err := Output(ctx, "", "docker", "volume", "ls", "--filter", filter, "-q"); err == nil && out != "" {
		ids := append([]string{"volume", "rm"}, strings.Fields(out)...)
		if err := Exec(ctx, "", "docker", ids...); err != nil {
			return err
		}
	}
	return nil
}

// PublishedPort returns the host port docker assigned to a service's published
// container port (ADR 0007 ephemeral loopback publishing) via a bare
// `docker compose port` in dir. Safe only where the compose project name equals
// the dir basename (Hull-managed shared services). For projects brought up with
// a pinned -p / -f / --env-file (sites, apps, adopted clusters) use
// Compose.Port instead, or the lookup resolves the wrong project.
func PublishedPort(ctx context.Context, dir, service string, containerPort int) (int, error) {
	out, err := Output(ctx, dir, "docker", "compose", "port", service, fmt.Sprintf("%d", containerPort))
	if err != nil {
		return 0, err
	}
	return parsePublishedPort(out)
}

// EnsureNetwork creates the named docker network if it does not exist.
func EnsureNetwork(ctx context.Context, name string) error {
	if out, err := Output(ctx, "", "docker", "network", "ls", "--filter", "name=^"+name+"$", "--format", "{{.Name}}"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == name {
				return nil
			}
		}
	}
	return Exec(ctx, "", "docker", "network", "create", name)
}
