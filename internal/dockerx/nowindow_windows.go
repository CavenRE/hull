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
	// CREATE_NO_WINDOW gives docker its own hidden, windowless console. The
	// compose plugin / com.docker.cli grandchildren docker spawns inherit THAT
	// console and stay invisible too. Merely hoping to inherit a hidden console
	// from the daemon is the fragile path; a fresh CREATE_NO_WINDOW per docker
	// call is the arrangement that actually holds.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
