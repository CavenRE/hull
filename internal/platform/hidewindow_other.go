//go:build !windows

package platform

import "os/exec"

// hideConsole is a no-op off Windows, where there is no console to hide.
func hideConsole(*exec.Cmd) {}
