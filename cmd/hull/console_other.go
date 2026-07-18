//go:build !windows

package main

// hideConsole is a no-op off Windows; systemd and launchd start the daemon
// without a console in the first place.
func hideConsole() {}
