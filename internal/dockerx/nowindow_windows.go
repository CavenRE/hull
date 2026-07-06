//go:build windows

package dockerx

import (
	"os/exec"
	"syscall"
)

// noWindow makes a child console app (docker) run without popping its own
// console window. This matters when the daemon shells out to docker on its
// reconcile loop: the daemon has no visible console, so without this each
// docker invocation would flash a window.
func noWindow(cmd *exec.Cmd) {
	// HideWindow only: the daemon already runs in a hidden console that docker
	// inherits, so we must NOT force a new console here (CREATE_NO_WINDOW would
	// give docker its own, and its grandchildren would pop visible windows).
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
