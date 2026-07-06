//go:build windows

package main

import "syscall"

// detachedSysProcAttr fully detaches a child (no console, its own group) so it
// outlives the CLI that spawned it. Same flags the uninstaller's cleanup uses.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x08000000, // DETACHED_PROCESS | CREATE_NO_WINDOW
	}
}
