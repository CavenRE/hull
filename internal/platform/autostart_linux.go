//go:build linux

package platform

import (
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// EnableDaemonAutostart installs and enables the hulld systemd --user unit so
// the daemon starts at login, and enables lingering so it also starts at boot
// and survives logout. hullExe is the hull binary (the unit runs `hull daemon
// run`). It starts the daemon now via the unit, so startedNow is true.
func EnableDaemonAutostart(hullExe string) (startedNow bool, err error) {
	ours := unitMarksLinger() // survives the unit being rewritten below
	if err := WriteSystemdUserUnit(hullExe); err != nil {
		return false, err
	}
	if err := EnableSystemdUserUnit(); err != nil {
		return false, err
	}
	// Lingering lets the --user unit start at boot and survive logout. It may
	// need polkit/root, so it is best-effort: without it autostart still works
	// at login, just not before it. Hull notes in the unit that it turned
	// lingering on, so turning autostart off can turn it back off.
	if u, err := user.Current(); err == nil {
		if !ours && !lingerOn(u.Username) && exec.Command("loginctl", "enable-linger", u.Username).Run() == nil {
			ours = true
		}
		if ours {
			markUnitLinger()
		}
	}
	return true, nil
}

// DisableDaemonAutostart disables, stops, and removes the systemd --user unit,
// and turns lingering back off when it was Hull that turned it on.
func DisableDaemonAutostart() error {
	if u, err := user.Current(); err == nil && unitMarksLinger() {
		_ = exec.Command("loginctl", "disable-linger", u.Username).Run()
	}
	RemoveSystemdUserUnit()
	return nil
}

// lingerMarker is the line Hull adds to its unit when it enabled lingering.
const lingerMarker = "# hull: enabled lingering for this user\n"

func unitMarksLinger() bool {
	data, err := os.ReadFile(SystemdUserUnitPath())
	return err == nil && strings.Contains(string(data), lingerMarker)
}

func markUnitLinger() {
	if unitMarksLinger() {
		return
	}
	if f, err := os.OpenFile(SystemdUserUnitPath(), os.O_WRONLY|os.O_APPEND, 0); err == nil {
		_, _ = f.WriteString(lingerMarker)
		_ = f.Close()
	}
}

func lingerOn(name string) bool {
	out, err := exec.Command("loginctl", "show-user", name, "--property=Linger", "--value").Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

// DaemonAutostartEnabled reports whether the hulld --user unit is enabled.
func DaemonAutostartEnabled() bool {
	out, _ := exec.Command("systemctl", "--user", "is-enabled", "hulld.service").Output()
	return strings.TrimSpace(string(out)) == "enabled"
}
