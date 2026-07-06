//go:build windows

package main

import "syscall"

// detachedSysProcAttr backgrounds the daemon so it outlives the CLI. It gives
// the daemon a real but HIDDEN console (CREATE_NEW_CONSOLE + SW_HIDE): every
// descendant, including docker and the docker-compose plugin it spawns,
// inherits that one hidden console, so nothing pops a window on the reconcile
// loop. (DETACHED_PROCESS or CREATE_NO_WINDOW leave grandchildren free to
// allocate their own visible consoles.) CREATE_NEW_PROCESS_GROUP isolates it
// from the launcher's Ctrl-C.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000010 | 0x00000200, // CREATE_NEW_CONSOLE | CREATE_NEW_PROCESS_GROUP
	}
}
