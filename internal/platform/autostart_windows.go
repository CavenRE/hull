//go:build windows

package platform

import (
	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// RunValueName is the HKCU Run entry the CLI writes for the daemon. It is kept
// distinct from the GUI's own autostart entry so the two never clobber each
// other; the daemon's single-instance lock makes a double launch harmless.
// Exported so the uninstaller cleans up the exact same value.
const RunValueName = "HullDaemon"

// EnableDaemonAutostart writes an HKCU Run entry that launches the daemon at
// logon. HKCU needs no elevation (a per-user install has none, and a Scheduled
// Task would). The daemon hides its own console via --background, so this
// console-subsystem binary does not leave a window. The entry only fires at the
// next logon, so startedNow is false and the caller starts the daemon itself.
// hullExe is the hull binary.
func EnableDaemonAutostart(hullExe string) (startedNow bool, err error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	if err := k.SetStringValue(RunValueName, `"`+hullExe+`" daemon run --background`); err != nil {
		return false, err
	}
	return false, nil
}

// DisableDaemonAutostart removes the HKCU Run entry (best-effort).
func DisableDaemonAutostart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return nil // no Run key: nothing to remove
	}
	defer k.Close()
	_ = k.DeleteValue(RunValueName)
	return nil
}

// DaemonAutostartEnabled reports whether the HKCU Run entry exists.
func DaemonAutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(RunValueName)
	return err == nil
}
