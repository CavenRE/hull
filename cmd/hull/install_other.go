//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// maybeRunAsInstaller is Windows-only (double-click installer UX); elsewhere the
// binary is only ever run from a shell, so this never applies.
func maybeRunAsInstaller() bool { return false }

func defaultInstallDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "bin")
	}
	return "/usr/local/bin"
}

// runInstall copies the running hull binary into the install directory. On
// Linux/macOS the usual install path is install.sh (which also wires PATH and
// an optional service), so this just drops the binary and reminds about PATH.
func runInstall(dir string) error {
	if dir == "" {
		dir = defaultInstallDir()
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	target := filepath.Join(dir, "hull")
	if self == target {
		fmt.Printf("Hull is already installed at %s.\n", dir)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := copyFile(self, target, 0o755); err != nil {
		return fmt.Errorf("copying hull: %w", err)
	}
	fmt.Printf("Installed hull to %s\n", target)
	fmt.Printf("Make sure %s is on your PATH, then: hull setup\n", dir)
	return nil
}
