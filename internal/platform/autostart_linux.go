//go:build linux

package platform

import (
	"os/exec"
	"os/user"
	"strings"
)

// EnableDaemonAutostart installs and enables the hulld systemd --user unit so
// the daemon starts at login, and enables lingering so it also starts at boot
// and survives logout. hullExe is the hull binary (the unit runs `hull daemon
// run`). It starts the daemon now via the unit, so startedNow is true.
func EnableDaemonAutostart(hullExe string) (startedNow bool, err error) {
	if err := WriteSystemdUserUnit(hullExe); err != nil {
		return false, err
	}
	if err := EnableSystemdUserUnit(); err != nil {
		return false, err
	}
	// Lingering lets the --user unit start at boot and survive logout. It may
	// need polkit/root, so it is best-effort: without it autostart still works
	// at login, just not before it.
	if u, err := user.Current(); err == nil {
		_ = exec.Command("loginctl", "enable-linger", u.Username).Run()
	}
	return true, nil
}

// DisableDaemonAutostart disables, stops, and removes the systemd --user unit.
func DisableDaemonAutostart() error {
	RemoveSystemdUserUnit()
	return nil
}

// DaemonAutostartEnabled reports whether the hulld --user unit is enabled.
func DaemonAutostartEnabled() bool {
	out, _ := exec.Command("systemctl", "--user", "is-enabled", "hulld.service").Output()
	return strings.TrimSpace(string(out)) == "enabled"
}
