//go:build windows

package api

import (
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// processAlive reports whether a process with the given PID exists and has not
// yet exited (still-active exit code), using kernel32 directly so no extra
// dependency is needed.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	var code uint32
	r, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code)))
	if r == 0 {
		return false
	}
	return code == stillActive
}
