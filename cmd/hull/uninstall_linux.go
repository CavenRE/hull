//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CavenRE/hull/internal/platform"
)

// runUninstall removes the per-user Hull install this hull binary lives in:
// binaries, the menu launcher, icons, the optional systemd unit, autostart,
// and the PATH block. It reverses what install.sh and the graphical installer
// set up. ~/.hull (config, certs, projects) is left alone unless --purge-data.
//
// Unlike Windows, Linux lets us delete a running executable (the kernel keeps
// the open inode), so there's no self-delete dance , we just unlink the files.
func runUninstall(o uninstallOpts) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, e := filepath.EvalSymlinks(exe); e == nil {
		exe = real
	}
	dir := filepath.Dir(exe)

	// A package manager's files are its own to remove; a /usr/local install
	// from install.sh is not, so it is uninstalled here like any other.
	if pm := packageManagerOwner(exe); pm != "" {
		fmt.Printf("Hull was installed by %s (%s); remove it that way instead.\n", pm, exe)
		return nil
	}
	// Safety: only proceed if this really is a Hull bin directory.
	if !looksLikeHullDir(dir) {
		return fmt.Errorf("%s does not look like a Hull install , aborting", dir)
	}

	stopHullProcesses(dir)
	untrustCA()
	revertSystem()

	fmt.Println("Removing desktop integration…")
	_ = platform.DisableDaemonAutostart() // also turns lingering off if Hull turned it on
	platform.RemoveAutostartEntry()
	platform.RemoveDesktopEntry()
	platform.RemoveIcons()

	fmt.Println("Cleaning PATH…")
	platform.RemoveFromShellPath()

	if o.PurgeData {
		if bak, err := backupHullHome(); err != nil {
			fmt.Printf("  note: %v\n", err)
		} else if bak != "" {
			fmt.Printf("Moved ~/.hull -> %s\n", bak)
		}
	}

	fmt.Println("Removing program files…")
	for _, b := range []string{"hull-gui", "hulld", "hull"} {
		p := filepath.Join(dir, b)
		switch err := os.Remove(p); {
		case err == nil:
			fmt.Printf("  removed %s\n", p)
		case errors.Is(err, os.ErrPermission):
			fmt.Printf("  %s needs root to remove: sudo rm -f %s\n", p, p)
		}
	}
	fmt.Println("Hull uninstalled. Open a new terminal so the PATH change applies.")
	return nil
}

// looksLikeHullDir reports whether dir holds a Hull install, guarding against
// ever wiping an unrelated directory. It accepts any of the Hull binaries:
// source/AUR/release installs ship a single `hull`, while a GUI build also
// drops `hull-gui`/`hulld`. Requiring the GUI binaries here made `hull
// uninstall` abort on every real single-binary Linux install (it removed
// nothing and left the trusted CA, PATH block, and unit behind). `hull` is
// the running executable itself, so its presence is always valid evidence;
// os.Remove below only ever targets these known names.
func looksLikeHullDir(dir string) bool {
	for _, b := range []string{"hull", "hull-gui", "hulld"} {
		if _, err := os.Stat(filepath.Join(dir, b)); err == nil {
			return true
		}
	}
	return false
}

func stopHullProcesses(dir string) {
	_ = runHull(dir, "daemon", "stop")
	for _, p := range []string{"hull-gui", "hulld"} {
		_ = exec.Command("pkill", "-x", p).Run()
	}
}

func runHull(dir string, args ...string) error {
	return exec.Command(filepath.Join(dir, "hull"), args...).Run()
}
