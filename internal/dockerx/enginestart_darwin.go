//go:build darwin

package dockerx

import (
	"context"
	"fmt"
	"os/exec"
)

// startEngine launches Docker Desktop. `open -a` returns as soon as the app is
// launching, which is what we want: EnsureEngine does the waiting.
func startEngine(ctx context.Context) (string, error) {
	if err := exec.CommandContext(ctx, "open", "-a", "Docker").Run(); err != nil {
		return "", fmt.Errorf("could not launch Docker Desktop (open -a Docker): %w", err)
	}
	return "Docker Desktop", nil
}
