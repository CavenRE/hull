//go:build linux

package main

import (
	"errors"
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

	// Remove any *.tld DNS config Hull installed (systemd-resolved drop-in or
	// the NetworkManager dnsmasq rule) , previously leaked because nothing ever
	// called UnregisterDNS. Surface manual sudo steps when we aren't root.
	tld := uninstallTLD()
	if err := platform.UnregisterDNS(tld); err != nil {
		var manual *platform.ManualStepsError
		if errors.As(err, &manual) {
			fmt.Println("DNS cleanup needs elevation , run:")
			fmt.Println("  " + strings.ReplaceAll(manual.Instructions, "\n", "\n  "))
		}
	} else {
		fmt.Printf("Removed *.%s DNS configuration.\n", tld)
	}

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

// uninstallTLD returns the configured TLD so DNS cleanup targets the right
// files, falling back to the default when config can't be loaded.
func uninstallTLD() string {
	if a, err := loadApp(); err == nil {
		return a.Config.TLD
	}
	return "test"
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
