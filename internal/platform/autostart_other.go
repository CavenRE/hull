//go:build !linux && !darwin && !windows

package platform

import "fmt"

// EnableDaemonAutostart is unsupported off the three shipped platforms.
func EnableDaemonAutostart(hullExe string) (startedNow bool, err error) {
	return false, fmt.Errorf("daemon autostart is not supported on this platform")
}

// DisableDaemonAutostart is a no-op off the three shipped platforms.
func DisableDaemonAutostart() error { return nil }

// DaemonAutostartEnabled always reports false off the three shipped platforms.
func DaemonAutostartEnabled() bool { return false }
