//go:build darwin

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "dev.hull.daemon"

// LaunchAgentPath is the per-user launch-at-login agent for hulld.
func LaunchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

func launchAgentPlist(hullExe string) string {
	return strings.Join([]string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		`<plist version="1.0">`,
		`<dict>`,
		`  <key>Label</key>`,
		`  <string>` + launchAgentLabel + `</string>`,
		`  <key>ProgramArguments</key>`,
		`  <array>`,
		`    <string>` + hullExe + `</string>`,
		`    <string>daemon</string>`,
		`    <string>run</string>`,
		`  </array>`,
		`  <key>RunAtLoad</key>`,
		`  <true/>`,
		`  <key>StandardOutPath</key>`,
		`  <string>/dev/null</string>`,
		`  <key>StandardErrorPath</key>`,
		`  <string>/dev/null</string>`,
		`</dict>`,
		`</plist>`,
		``,
	}, "\n")
}

// EnableDaemonAutostart writes a per-user LaunchAgent (login-scoped) that runs
// the hull daemon, and loads it now (RunAtLoad starts it), so startedNow is
// true. hullExe is the hull binary.
func EnableDaemonAutostart(hullExe string) (startedNow bool, err error) {
	path := LaunchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(launchAgentPlist(hullExe)), 0o644); err != nil {
		return false, err
	}
	// Reload so a re-enable picks up a changed exe path, then load to start it.
	_ = exec.Command("launchctl", "unload", path).Run()
	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return false, err
	}
	return true, nil
}

// DisableDaemonAutostart unloads and removes the LaunchAgent.
func DisableDaemonAutostart() error {
	path := LaunchAgentPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DaemonAutostartEnabled reports whether the LaunchAgent is installed.
func DaemonAutostartEnabled() bool {
	_, err := os.Stat(LaunchAgentPath())
	return err == nil
}
