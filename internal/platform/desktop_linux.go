//go:build linux

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// This file holds the Linux paths and markers Hull's installers use, so
// `hull uninstall` removes what install.sh, or an older Hull installer, put in
// place. They must agree with install.sh and uninstall.sh on every path and
// marker below.
//
// Everything lives under the user's XDG dirs (no root): a Hull install is
// per-user, mirroring ~/.local/bin for the binaries.

const (
	// pathMarker is the comment install.sh writes above its PATH export;
	// RemoveFromShellPath strips that exact block.
	pathMarker = "# Added by Hull"

	desktopFileName = "hull.desktop"
	iconName        = "hull" // resolved from the hicolor theme by the .desktop
)

// iconSizes are the hicolor size folders an older Hull installer put hull.png
// in.
var iconSizes = []string{"32x32", "128x128", "256x256", "512x512"}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

func xdgDataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local", "share")
}

func xdgConfigHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(home(), ".config")
}

func applicationsDir() string { return filepath.Join(xdgDataHome(), "applications") }
func hicolorDir() string      { return filepath.Join(xdgDataHome(), "icons", "hicolor") }
func iconAppsDir(size string) string {
	return filepath.Join(hicolorDir(), size, "apps")
}

// DesktopEntryPath is the menu launcher an older Hull installer wrote for the GUI.
func DesktopEntryPath() string { return filepath.Join(applicationsDir(), desktopFileName) }

// AutostartEntryPath is the launch-at-login entry (XDG autostart).
func AutostartEntryPath() string {
	return filepath.Join(xdgConfigHome(), "autostart", desktopFileName)
}

// SystemdUserUnitPath is the optional `systemd --user` unit for hulld.
func SystemdUserUnitPath() string {
	return filepath.Join(xdgConfigHome(), "systemd", "user", "hulld.service")
}

// ── desktop entry ───────────────────────────────────────────────────────────

// RemoveDesktopEntry deletes the menu launcher (best-effort).
func RemoveDesktopEntry() {
	_ = os.Remove(DesktopEntryPath())
	refreshDesktopDB()
}

// RemoveAutostartEntry deletes the launch-at-login entry (best-effort).
func RemoveAutostartEntry() { _ = os.Remove(AutostartEntryPath()) }

// ── icons ───────────────────────────────────────────────────────────────────

// RemoveIcons deletes hull.png from every hicolor size Hull may have used.
func RemoveIcons() {
	for _, size := range iconSizes {
		_ = os.Remove(filepath.Join(iconAppsDir(size), iconName+".png"))
	}
	refreshIconCache()
}

// ── PATH (shell rc) ─────────────────────────────────────────────────────────

// RemoveFromShellPath strips Hull's PATH block (the marker line and the export
// line that follows it) from every rc file it might live in.
func RemoveFromShellPath() {
	for _, rc := range []string{
		filepath.Join(home(), ".zshrc"),
		filepath.Join(home(), ".bashrc"),
		filepath.Join(home(), ".bash_profile"),
		filepath.Join(home(), ".profile"),
	} {
		b, err := os.ReadFile(rc)
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		var kept []string
		for i := 0; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == pathMarker {
				// Skip the marker and the export line right after it.
				if i+1 < len(lines) && strings.Contains(lines[i+1], "export PATH=") {
					i++
				}
				// Also drop a single blank line we inserted before the marker.
				if n := len(kept); n > 0 && strings.TrimSpace(kept[n-1]) == "" {
					kept = kept[:n-1]
				}
				continue
			}
			kept = append(kept, lines[i])
		}
		_ = os.WriteFile(rc, []byte(strings.Join(kept, "\n")), 0o644)
	}
}

// ── systemd --user unit ─────────────────────────────────────────────────────

func systemdUnit(hulldExe string) string {
	return "[Unit]\n" +
		"Description=Hull daemon (local router, DNS, services)\n" +
		"After=network-online.target docker.service\n" +
		"Wants=network-online.target\n\n" +
		"[Service]\n" +
		"ExecStart=" + hulldExe + " daemon run\n" +
		"Restart=on-failure\n" +
		"RestartSec=2\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

// WriteSystemdUserUnit installs the unit and reloads the user manager. It does
// not enable the unit , that's a separate, opt-in step.
func WriteSystemdUserUnit(hulldExe string) error {
	path := SystemdUserUnitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(systemdUnit(hulldExe)), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

// EnableSystemdUserUnit enables and starts hulld for the current user.
func EnableSystemdUserUnit() error {
	return exec.Command("systemctl", "--user", "enable", "--now", "hulld.service").Run()
}

// RemoveSystemdUserUnit disables, stops, and deletes the unit (best-effort).
func RemoveSystemdUserUnit() {
	_ = exec.Command("systemctl", "--user", "disable", "--now", "hulld.service").Run()
	_ = os.Remove(SystemdUserUnitPath())
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
}

// ── privilege: bind low ports ───────────────────────────────────────────────

// GrantPortBind gives hulld CAP_NET_BIND_SERVICE so its embedded router can
// bind 80/443 without root. Runs setcap directly when already root, otherwise
// through pkexec (a graphical polkit prompt). Falls back to manual steps when
// neither is possible.
func GrantPortBind(hulldExe string) error {
	const caps = "cap_net_bind_service=+ep"
	if os.Geteuid() == 0 {
		return exec.Command("setcap", caps, hulldExe).Run()
	}
	if _, err := exec.LookPath("pkexec"); err == nil {
		if err := exec.Command("pkexec", "setcap", caps, hulldExe).Run(); err == nil {
			return nil
		}
	}
	return &ManualStepsError{Instructions: "sudo setcap '" + caps + "' " + hulldExe}
}

// ── refresh caches (best-effort) ────────────────────────────────────────────

func refreshDesktopDB() {
	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command("update-desktop-database", applicationsDir()).Run()
	}
}

func refreshIconCache() {
	if _, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		_ = exec.Command("gtk-update-icon-cache", "-f", "-t", hicolorDir()).Run()
	}
}
