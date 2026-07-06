//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/version"
)

func defaultInstallDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "Hull") }

// runInstall copies the running hull.exe into the install directory, adds it to
// PATH, and registers an Apps & Features entry. Re-running updates the copy in
// place, stopping a running daemon first so its lock on hull.exe releases.
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
	target := filepath.Join(dir, "hull.exe")

	if strings.EqualFold(self, target) {
		fmt.Printf("Hull is already installed at %s; refreshing PATH + registry.\n", dir)
		if err := addToUserPath(dir); err != nil {
			return err
		}
		return writeUninstallEntry(dir)
	}

	fmt.Printf("Installing Hull to %s\n", dir)
	// A running daemon is a hull.exe at target and locks the file; stop it.
	if a, err := loadApp(); err == nil {
		if _, ok := api.Connect(a.Config.HullHome); ok {
			fmt.Println("  stopping the running daemon...")
			stopDaemonAndWait(context.Background(), a.Config.HullHome)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	staged, err := stageReplacement(target, self)
	if err != nil {
		return fmt.Errorf("copying hull.exe: %w", err)
	}
	if err := commitReplacement(target, staged); err != nil {
		return fmt.Errorf("installing hull.exe: %w", err)
	}
	if err := addToUserPath(dir); err != nil {
		return fmt.Errorf("PATH: %w", err)
	}
	if err := writeUninstallEntry(dir); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	fmt.Println("\nHull installed. Open a NEW terminal and run:")
	fmt.Println("  hull setup       # enable the router + DNS, trust the local CA")
	fmt.Println("  hull daemon run  # start the daemon, then: hull new demo laravel")
	return nil
}

func addToUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	cur, _, _ := k.GetStringValue("Path")
	for _, p := range strings.Split(cur, ";") {
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			return nil // already present
		}
	}
	next := dir
	if cur != "" {
		next = cur + ";" + dir
	}
	return k.SetExpandStringValue("Path", next)
}

// writeUninstallEntry registers Hull in Apps & Features (uninstallRegKey is
// defined in uninstall_windows.go, which reads it back on removal).
func writeUninstallEntry(dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRegKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	unstr := `"` + filepath.Join(dir, "hull.exe") + `" uninstall --quiet`
	_ = k.SetStringValue("DisplayName", "Hull")
	_ = k.SetStringValue("DisplayVersion", version.Version)
	_ = k.SetStringValue("Publisher", "CavenRE")
	_ = k.SetStringValue("InstallLocation", dir)
	_ = k.SetStringValue("DisplayIcon", filepath.Join(dir, "hull.exe"))
	_ = k.SetStringValue("UninstallString", unstr)
	_ = k.SetStringValue("QuietUninstallString", unstr)
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return nil
}
