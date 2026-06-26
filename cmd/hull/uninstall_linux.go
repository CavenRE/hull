//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	// Package-managed installs (under /usr) belong to the package manager.
	if strings.HasPrefix(dir, "/usr") {
		fmt.Printf("Hull looks installed by a package manager (%s).\n", dir)
		fmt.Println("Remove it that way instead, e.g.:  sudo pacman -R hull")
		return nil
	}
	// Safety: only proceed if this really is a Hull bin directory.
	if !looksLikeHullDir(dir) {
		return fmt.Errorf("%s does not look like a Hull install , aborting", dir)
	}

	// Stop the daemon and undo trust while the binaries still exist.
	stopHullProcesses(dir)
	_ = runHull(dir, "trust", "--uninstall")

	fmt.Println("Removing desktop integration…")
	platform.RemoveSystemdUserUnit()
	platform.RemoveAutostartEntry()
	platform.RemoveDesktopEntry()
	platform.RemoveIcons()

	fmt.Println("Cleaning PATH…")
	platform.RemoveFromShellPath()

	if o.PurgeData {
		if err := backupHullHome(); err != nil {
			fmt.Printf("  note: %v\n", err)
		} else {
			fmt.Println("Moved ~/.hull -> ~/.hull.bak")
		}
	}

	fmt.Println("Removing program files…")
	for _, b := range []string{"hull-gui", "hulld", "hull"} {
		if err := os.Remove(filepath.Join(dir, b)); err == nil {
			fmt.Printf("  removed %s\n", filepath.Join(dir, b))
		}
	}
	fmt.Println("Hull uninstalled. Open a new terminal so the PATH change applies.")
	return nil
}

// looksLikeHullDir reports whether dir holds a Hull install (the daemon or the
// GUI binary), guarding against ever wiping an unrelated directory.
func looksLikeHullDir(dir string) bool {
	for _, b := range []string{"hull-gui", "hulld"} {
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

// backupHullHome moves ~/.hull aside to ~/.hull.bak (matching the Windows
// uninstaller's --purge-data behaviour , reversible, not a delete).
func backupHullHome() error {
	h, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	src := filepath.Join(h, ".hull")
	if _, err := os.Stat(src); err != nil {
		return nil // nothing to move
	}
	bak := filepath.Join(h, ".hull.bak")
	_ = os.RemoveAll(bak)
	return os.Rename(src, bak)
}
