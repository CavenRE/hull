//go:build !windows

package dockerx

import "os/exec"

// noWindow is a no-op off Windows (no console-window concept).
func noWindow(cmd *exec.Cmd) {}
