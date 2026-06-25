//go:build !windows && !linux

package main

import "fmt"

// runUninstall has a full implementation on Windows and Linux. On macOS (and
// any other Unix) the binaries are managed by ./install.sh or a package
// manager, so we just point the way.
func runUninstall(o uninstallOpts) error {
	fmt.Println("`hull uninstall` automates the Windows and Linux uninstalls.")
	fmt.Println("On macOS, run ./uninstall.sh from the source tree (or remove the Hull")
	fmt.Println("binaries via your package manager), and delete ~/.hull to clear config,")
	fmt.Println("certificates, and service data.")
	return nil
}
