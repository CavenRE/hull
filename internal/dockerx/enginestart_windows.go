//go:build windows

package dockerx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// startEngine launches Docker Desktop from its standard install locations.
// Docker Desktop is a GUI app, so this returns as soon as it is launching;
// EnsureEngine does the waiting.
func startEngine(ctx context.Context) (string, error) {
	var candidates []string
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles(x86)")} {
		if base != "" {
			candidates = append(candidates, filepath.Join(base, "Docker", "Docker", "Docker Desktop.exe"))
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates, filepath.Join(local, "Docker", "Docker Desktop.exe"))
	}

	for _, exe := range candidates {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		cmd := exec.Command(exe)
		NoWindow(cmd) // never flash a console (see the no-flash guardrail)
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("launching %s: %w", exe, err)
		}
		_ = cmd.Process.Release()
		return "Docker Desktop", nil
	}
	return "", errors.New("Docker Desktop was not found in Program Files; start it manually")
}
