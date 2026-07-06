//go:build windows

package main

import "syscall"

// detachedSysProcAttr backgrounds the daemon so it outlives the CLI. The daemon
// runs console-less (DETACHED_PROCESS): it logs to hulld.log, not a console.
// Every docker call it makes gets its OWN hidden console via CREATE_NO_WINDOW
// (see internal/dockerx/nowindow_windows.go), and docker's grandchildren inherit
// that, so nothing pops a window on the reconcile loop, without the daemon owning
// a console of its own (a hidden CREATE_NEW_CONSOLE can still briefly flash).
// CREATE_NEW_PROCESS_GROUP isolates the daemon from the launcher's Ctrl-C.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
}
