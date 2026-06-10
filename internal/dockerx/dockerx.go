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
	return cmd.Run()
}

// Output runs a command and captures stdout, trimming trailing whitespace.
func Output(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
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
		return errors.New("the 'docker' command was not found in PATH — install Docker (or Podman with docker compatibility) and try again")
	}
	if _, err := Output(ctx, "", "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return errors.New("the container engine is not responding — is Docker (or your Docker-compatible engine) running?")
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

// ForceRemoveProject removes all containers and volumes labeled with the
// compose project name — the fallback when a compose file is corrupted
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
