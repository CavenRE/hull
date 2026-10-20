//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/platform"
)

const uninstallRegKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Hull`

// runUninstall removes the Hull install this hull.exe lives in. It runs from
// the install directory (which is execution-allowed, since the app runs), so
// it sidesteps the SRP/AppLocker block that stops the NSIS uninstaller from
// launching its %TEMP% copy.
func runUninstall(o uninstallOpts) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	// Safety: only proceed if this really is a Hull install directory. The
	// running hull.exe lives here, so it must be present.
	if _, err := os.Stat(filepath.Join(dir, "hull.exe")); err != nil {
		return fmt.Errorf("%s does not look like a Hull install, aborting", dir)
	}

	stopApp()

	// Reinstall path: the NSIS installer runs the uninstaller with `_?=<dir>`
	// to remove the previous version, then checks that the main binary is gone
	// (installer.nsi: "$INSTDIR\hull-gui.exe"). So here we delete the files
	// SYNCHRONOUSLY and must NOT schedule the async dir wipe , that would race
	// the fresh install and the leftover hull-gui.exe would fail the check
	// ("Unable to uninstall"). The installer re-creates registry/PATH/shortcuts.
	if isReinstall() {
		removeInstalledFiles(dir)
		fmt.Println("Previous version removed.")
		return nil
	}

	untrustCA()
	revertSystem()

	fmt.Println("Removing the Apps & Features entry…")
	if err := registry.DeleteKey(registry.CURRENT_USER, uninstallRegKey); err != nil {
		fmt.Printf("  note: %v\n", err)
	}

	fmt.Println("Cleaning PATH…")
	if err := removeFromUserPath(dir); err != nil {
		fmt.Printf("  note: %v\n", err)
	}

	fmt.Println("Removing shortcuts…")
	for _, lnk := range shortcutPaths() {
		_ = os.Remove(lnk)
	}

	fmt.Println("Removing autostart (launch-at-login)…")
	removeAutostart(dir)

	if o.PurgeData {
		if bak, err := backupHullHome(); err != nil {
			fmt.Printf("  note: %v\n", err)
		} else if bak != "" {
			fmt.Printf("Moved ~/.hull -> %s\n", bak)
		}
	}

	fmt.Println("Removing program files…")
	removeInstalledFiles(dir)
	scheduleSelfDelete(dir)
	fmt.Println("Hull uninstalled. (Open a new terminal so the PATH change applies.)")
	return nil
}

// isReinstall reports whether the NSIS installer invoked us to remove a
// previous version (it appends `_?=<dir>` to the UninstallString).
func isReinstall() bool {
	for _, a := range os.Args {
		if strings.HasPrefix(a, "_?=") {
			return true
		}
	}
	return false
}

// stopApp stops a running daemon gracefully via its API. It must NOT taskkill
// by image name: the daemon is a hull.exe process, and so is this uninstaller,
// so a name-based kill would terminate ourselves mid-uninstall.
func stopApp() {
	if a, err := loadApp(); err == nil {
		if c, ok := api.Connect(a.Config.HullHome); ok {
			_ = c.Shutdown(context.Background())
		}
	}
}

var installedFiles = []string{"hull.exe", "hull-gui.exe", "hulld.exe"}

// removeInstalledFiles deletes only the files Hull installs (and their update
// leftovers), never the rest of dir: hull.exe may run from a folder that holds
// other work, such as a source checkout.
func removeInstalledFiles(dir string) {
	self, _ := os.Executable()
	for _, name := range installedFiles {
		for _, suffix := range []string{"", ".new", ".old"} {
			p := filepath.Join(dir, name+suffix)
			if strings.EqualFold(p, self) {
				continue
			}
			_ = os.Remove(p)
		}
	}
}

// removeFromUserPath drops dir from the per-user PATH, preserving REG_EXPAND_SZ.
func removeFromUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	val, _, err := k.GetStringValue("Path")
	if err != nil {
		return err
	}
	want := strings.TrimRight(strings.ToLower(dir), `\`)
	var kept []string
	for _, p := range strings.Split(val, ";") {
		if p == "" || strings.TrimRight(strings.ToLower(p), `\`) == want {
			continue
		}
		kept = append(kept, p)
	}
	return k.SetExpandStringValue("Path", strings.Join(kept, ";"))
}

func shortcutPaths() []string {
	var out []string
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		out = append(out,
			filepath.Join(appdata, `Microsoft\Windows\Start Menu\Programs\Hull.lnk`),
			filepath.Join(appdata, `Microsoft\Windows\Start Menu\Programs\Startup\Hull.lnk`),
		)
	}
	if pd := os.Getenv("ProgramData"); pd != "" {
		out = append(out, filepath.Join(pd, `Microsoft\Windows\Start Menu\Programs\Hull.lnk`))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "Desktop", "Hull.lnk"))
	}
	return out
}

// removeAutostart deletes the launch-at-login Run entry (tauri-plugin-autostart
// writes one when enabled), matched by name or by a value pointing at the
// install dir , so we never strip an unrelated app's entry.
func removeAutostart(dir string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer func() { _ = k.Close() }()
	names, _ := k.ReadValueNames(0)
	want := strings.ToLower(dir)
	for _, n := range names {
		val, _, _ := k.GetStringValue(n)
		if strings.EqualFold(n, "Hull") || strings.EqualFold(n, "hull-gui") || strings.EqualFold(n, platform.RunValueName) || strings.Contains(strings.ToLower(val), want) {
			_ = k.DeleteValue(n)
		}
	}
}

// scheduleSelfDelete deletes the running hull.exe after this process exits,
// then removes dir only if that left it empty (rmdir without /s). ping is the
// delay because timeout needs a console, which a detached process lacks.
func scheduleSelfDelete(dir string) {
	exe := filepath.Join(dir, "hull.exe")
	step := `del /f /q "` + exe + `" & rmdir "` + dir + `"`
	// The command line is set verbatim: Go re-quoting a single script arg
	// mangles the quoted paths.
	line := `cmd /c ping 127.0.0.1 -n 4 >nul & ` + step + ` & ping 127.0.0.1 -n 3 >nul & ` + step
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x08000000, // DETACHED_PROCESS | CREATE_NO_WINDOW
		CmdLine:       line,
	}
	_ = cmd.Start()
}
