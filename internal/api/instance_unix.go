//go:build !windows

package api

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether a process with the given PID currently exists.
// Signal 0 performs no action but still does the permission/existence check.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
