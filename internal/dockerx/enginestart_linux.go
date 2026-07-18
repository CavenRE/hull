//go:build linux

package dockerx

import (
	"context"
	"errors"
	"os/exec"
)

// startEngine tries the user-scoped services first (Docker Desktop for Linux,
// then rootless dockerd) because those need no elevation, and only then the
// system service, which normally requires root and will simply fail here rather
// than hang on a password prompt.
func startEngine(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "", errors.New("systemctl was not found; start your container engine manually")
	}
	attempts := []struct {
		args []string
		what string
	}{
		{[]string{"--user", "start", "docker-desktop"}, "Docker Desktop"},
		{[]string{"--user", "start", "docker"}, "rootless Docker"},
		{[]string{"start", "docker"}, "the Docker service"},
	}
	for _, a := range attempts {
		if err := exec.CommandContext(ctx, "systemctl", a.args...).Run(); err == nil {
			return a.what, nil
		}
	}
	return "", errors.New("could not start Docker (tried systemctl --user start docker-desktop, then docker, then the system service); start it manually, for example: sudo systemctl start docker")
}
