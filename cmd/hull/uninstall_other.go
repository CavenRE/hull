//go:build !windows && !linux

package main

import (
	"context"
	"fmt"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/platform"
)

// runUninstall on macOS undoes what Hull set up outside its binaries: the
// daemon and its LaunchAgent, the CA's trust, the resolver file and the
// loopback alias. The binaries themselves are managed by ./install.sh or a
// package manager, so it points the way for those.
func runUninstall(o uninstallOpts) error {
	if a, err := loadApp(); err == nil {
		if c, ok := api.Connect(a.Config.HullHome); ok {
			_ = c.Shutdown(context.Background())
		}
	}
	_ = platform.DisableDaemonAutostart()
	untrustCA()
	revertSystem()
	if o.PurgeData {
		if bak, err := backupHullHome(); err != nil {
			fmt.Printf("  note: %v\n", err)
		} else if bak != "" {
			fmt.Printf("Moved ~/.hull -> %s\n", bak)
		}
	}
	fmt.Println("Remove the Hull binaries with ./uninstall.sh from the source tree, or with the")
	fmt.Println("package manager that installed them.")
	return nil
}
