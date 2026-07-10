//go:build linux

package platform

import (
	"os/exec"
	"strings"
)

// RestartDaemonService restarts the hulld systemd --user service when it is the
// active daemon, so a config change (loopback, ports, DNS toggle) takes effect
// without the user running systemctl by hand. It reports whether it actually
// restarted anything: false when the service isn't installed/active (e.g. a
// foreground `hull daemon run`), so the caller can advise a manual restart.
func RestartDaemonService() (bool, error) {
	out, _ := exec.Command("systemctl", "--user", "is-active", "hulld.service").Output()
	if strings.TrimSpace(string(out)) != "active" {
		return false, nil
	}
	if err := exec.Command("systemctl", "--user", "restart", "hulld.service").Run(); err != nil {
		return false, err
	}
	return true, nil
}
