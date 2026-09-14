package platform

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps a spawned helper from flashing a console window. Anything
// Hull runs from the daemon or from a GUI-launched CLI must set this, or every
// call blinks a black box over whatever the user is doing.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
