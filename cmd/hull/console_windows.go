//go:build windows

package main

import "golang.org/x/sys/windows"

var (
	user32kernel32     = windows.NewLazySystemDLL("kernel32.dll")
	user32dll          = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWin  = user32kernel32.NewProc("GetConsoleWindow")
	procShowWindowCall = user32dll.NewProc("ShowWindow")
)

// hideConsole hides this process's console window. The autostart Scheduled Task
// launches `hull daemon run --background`; Task Scheduler allocates a console
// for a console-subsystem app, so the daemon hides it immediately (it logs to
// hulld.log, never the console). At most a brief flash precedes this call.
func hideConsole() {
	hwnd, _, _ := procGetConsoleWin.Call()
	if hwnd != 0 {
		const swHide = 0
		_, _, _ = procShowWindowCall.Call(hwnd, uintptr(swHide))
	}
}
