//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
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
	// Safety: only proceed if this really is a Hull install directory.
	if _, e1 := os.Stat(filepath.Join(dir, "hull-gui.exe")); e1 != nil {
		if _, e2 := os.Stat(filepath.Join(dir, "hulld.exe")); e2 != nil {
			return fmt.Errorf("%s does not look like a Hull install — aborting", dir)
		}
	}

	stopApp()

	// Reinstall path: the NSIS installer runs the uninstaller with `_?=<dir>`
	// to remove the previous version, then checks that the main binary is gone
	// (installer.nsi: "$INSTDIR\hull-gui.exe"). So here we delete the files
	// SYNCHRONOUSLY and must NOT schedule the async dir wipe — that would race
	// the fresh install and the leftover hull-gui.exe would fail the check
	// ("Unable to uninstall"). The installer re-creates registry/PATH/shortcuts.
	if isReinstall() {
		removeInstalledFiles(dir) // everything except the running hull.exe
		fmt.Println("Previous version removed.")
		return nil
	}

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

	if o.PurgeData {
		if err := backupHullHome(); err != nil {
			fmt.Printf("  note: %v\n", err)
		} else {
			fmt.Println("Moved ~/.hull -> ~/.hull.bak")
		}
	}

	fmt.Println("Removing program files…")
	removeInstalledFiles(dir) // sync — so most is gone even if the async wipe is blocked
	scheduleDirDelete(dir)     // removes the running hull.exe + dir after we exit
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

func stopApp() {
	for _, p := range []string{"hull-gui.exe", "hulld.exe"} {
		_ = exec.Command("taskkill", "/F", "/IM", p).Run()
	}
}

// removeInstalledFiles deletes everything in dir except the running hull.exe
// (which Windows won't let us delete while it's executing).
func removeInstalledFiles(dir string) {
	self, _ := os.Executable()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if strings.EqualFold(p, self) {
			continue
		}
		_ = os.RemoveAll(p)
	}
}

// removeFromUserPath drops dir from the per-user PATH, preserving REG_EXPAND_SZ.
func removeFromUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
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
		out = append(out, filepath.Join(appdata, `Microsoft\Windows\Start Menu\Programs\Hull.lnk`))
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "Desktop", "Hull.lnk"))
	}
	return out
}

func backupHullHome() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	src := filepath.Join(home, ".hull")
	if _, err := os.Stat(src); err != nil {
		return nil // nothing to move
	}
	bak := filepath.Join(home, ".hull.bak")
	_ = os.RemoveAll(bak)
	return os.Rename(src, bak)
}

// scheduleDirDelete removes dir shortly after this process exits — we can't
// delete the running hull.exe inside it, so a detached cmd waits, then deletes.
// `ping` is the delay (timeout needs a console, which a detached process lacks);
// the rmdir is retried once in case hull.exe is still releasing.
func scheduleDirDelete(dir string) {
	// Set the command line verbatim — letting Go re-quote a single script arg
	// mangles the quoted "dir". ping is the delay (timeout needs a console);
	// the rmdir is retried once while hull.exe finishes releasing.
	line := `cmd /c ping 127.0.0.1 -n 4 >nul & rmdir /s /q "` + dir + `" & ping 127.0.0.1 -n 3 >nul & rmdir /s /q "` + dir + `"`
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x08000000, // DETACHED_PROCESS | CREATE_NO_WINDOW
		CmdLine:       line,
	}
	_ = cmd.Start()
}
