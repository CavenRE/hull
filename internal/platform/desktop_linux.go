//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// This file holds the Linux desktop-integration helpers shared by the two
// install routes (install.sh and the graphical installer) and the two
// uninstall routes (uninstall.sh and `hull uninstall`). Keeping the canonical
// paths and the file contents here means any uninstaller cleans up after any
// installer — they must agree on every path and marker below.
//
// Everything lives under the user's XDG dirs (no root): a Hull install is
// per-user, mirroring ~/.local/bin for the binaries.

const (
	// pathMarker is the comment install.sh writes above its PATH export; the
	// Go helpers add and strip the exact same block, so either side can undo
	// the other's edit.
	pathMarker = "# Added by Hull"

	desktopFileName = "hull.desktop"
	iconName        = "hull" // resolved from the hicolor theme by the .desktop
)

// iconSizes maps the hicolor size directory to the source icon for that size.
// Used by RemoveIcons (to know what to delete) and as the canonical set the
// installers fill in.
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

// DesktopEntryPath is the menu launcher Hull installs for the GUI.
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

func desktopEntry(guiExe string, autostart bool) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=Hull\n")
	b.WriteString("GenericName=Local Web Environment\n")
	b.WriteString("Comment=A local environment for your sites & apps\n")
	fmt.Fprintf(&b, "Exec=%s\n", guiExe)
	fmt.Fprintf(&b, "Icon=%s\n", iconName)
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Development;\n")
	b.WriteString("StartupWMClass=Hull\n")
	if autostart {
		b.WriteString("X-GNOME-Autostart-enabled=true\n")
	}
	return b.String()
}

// WriteDesktopEntry installs the application menu launcher pointing at guiExe.
func WriteDesktopEntry(guiExe string) error {
	if err := os.MkdirAll(applicationsDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(DesktopEntryPath(), []byte(desktopEntry(guiExe, false)), 0o644); err != nil {
		return err
	}
	refreshDesktopDB()
	return nil
}

// RemoveDesktopEntry deletes the menu launcher (best-effort).
func RemoveDesktopEntry() {
	_ = os.Remove(DesktopEntryPath())
	refreshDesktopDB()
}

// WriteAutostartEntry installs the launch-at-login entry pointing at guiExe.
func WriteAutostartEntry(guiExe string) error {
	dir := filepath.Dir(AutostartEntryPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(AutostartEntryPath(), []byte(desktopEntry(guiExe, true)), 0o644)
}

// RemoveAutostartEntry deletes the launch-at-login entry (best-effort).
func RemoveAutostartEntry() { _ = os.Remove(AutostartEntryPath()) }

// ── icons ───────────────────────────────────────────────────────────────────

// InstallIcons writes hull.png into the hicolor theme for each provided size
// (keys are hicolor size dirs like "128x128"). Missing sizes are simply
// skipped, so callers can pass whatever resolutions they have.
func InstallIcons(icons map[string][]byte) error {
	for size, data := range icons {
		dir := iconAppsDir(size)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, iconName+".png"), data, 0o644); err != nil {
			return err
		}
	}
	refreshIconCache()
	return nil
}

// RemoveIcons deletes hull.png from every hicolor size Hull may have used.
func RemoveIcons() {
	for _, size := range iconSizes {
		_ = os.Remove(filepath.Join(iconAppsDir(size), iconName+".png"))
	}
	refreshIconCache()
}

// ── PATH (shell rc) ─────────────────────────────────────────────────────────

// shellRC returns the rc file Hull edits for PATH, matching install.sh's
// selection so both write the same place.
func shellRC() string {
	switch sh := os.Getenv("SHELL"); {
	case strings.HasSuffix(sh, "zsh"):
		return filepath.Join(home(), ".zshrc")
	case strings.HasSuffix(sh, "bash"):
		rc := filepath.Join(home(), ".bashrc")
		if _, err := os.Stat(rc); err != nil {
			return filepath.Join(home(), ".bash_profile")
		}
		return rc
	default:
		return filepath.Join(home(), ".profile")
	}
}

// AddToShellPath appends Hull's PATH block to the user's rc file if dir isn't
// already on PATH and the block isn't already present. Returns the rc path it
// touched (empty if nothing was changed).
func AddToShellPath(dir string) (string, error) {
	if onPath(dir) {
		return "", nil
	}
	rc := shellRC()
	if b, err := os.ReadFile(rc); err == nil && strings.Contains(string(b), pathMarker) {
		return "", nil // already added on a previous install
	}
	f, err := os.OpenFile(rc, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n%s\nexport PATH=\"$PATH:%s\"\n", pathMarker, dir); err != nil {
		return "", err
	}
	return rc, nil
}

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

func onPath(dir string) bool {
	want := strings.TrimRight(dir, "/")
	for _, p := range strings.Split(os.Getenv("PATH"), ":") {
		if strings.TrimRight(p, "/") == want {
			return true
		}
	}
	return false
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
// not enable the unit — that's a separate, opt-in step.
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
