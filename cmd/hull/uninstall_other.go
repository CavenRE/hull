//go:build !windows

package main

import "fmt"

// runUninstall is Windows-specific (registry, PATH, shortcuts, self-delete).
// On macOS/Linux the binaries are managed by the OS installer/package manager.
func runUninstall(o uninstallOpts) error {
	fmt.Println("`hull uninstall` automates the Windows uninstall only.")
	fmt.Println("On macOS/Linux, remove the Hull binaries via your installer or package manager,")
	fmt.Println("and delete ~/.hull to clear config, certificates, and service data.")
	return nil
}
