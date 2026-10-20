package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/certs"
	"github.com/CavenRE/hull/internal/platform"
)

// uninstallOpts is shared by the platform-specific runUninstall.
type uninstallOpts struct {
	Quiet     bool
	PurgeData bool
}

func init() {
	o := uninstallOpts{}
	var force bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Hull from this machine",
		Long: "Remove the Hull app from this machine: the binaries, the PATH entry,\n" +
			"shortcuts, Hull's local CA in the system trust store, the DNS setup for\n" +
			"the Hull TLD (on Windows also the hosts block), and on Windows the Apps\n" +
			"and Features entry. Your project files are never touched, and by default\n" +
			"your ~/.hull data (config, certificates, shared service data) is left in\n" +
			"place too.\n\n" +
			"This runs locally and is platform-specific: on Windows it self-deletes\n" +
			"the install; on Linux/macOS it cleans up the PATH entry, desktop files,\n" +
			"and any systemd units. It is also what the Windows Uninstall button\n" +
			"invokes, running from the install directory so it still works when a\n" +
			"policy blocks the NSIS uninstaller from launching its temporary copy.\n\n" +
			"Unless you pass --quiet, -f/--force, or the global --yes, it asks for\n" +
			"confirmation first and fails closed on a non-interactive stdin. Add\n" +
			"--purge-data to also move ~/.hull aside so a later reinstall starts\n" +
			"clean. Before uninstalling, consider running hull stop to bring down\n" +
			"any projects, shared services, and the daemon.",
		Example: "  hull uninstall\n" +
			"  hull uninstall --force\n" +
			"  hull uninstall --purge-data",
		RunE: func(cmd *cobra.Command, args []string) error {
			// --quiet (installer button) and -f/--force (or the global --yes)
			// skip the prompt; otherwise confirm, which fails closed on a
			// non-interactive stdin.
			if !o.Quiet && !force {
				ok, err := confirm("Remove Hull from this machine? Your projects are untouched.")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			return runUninstall(o)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt (alias of --yes)")
	cmd.Flags().BoolVar(&o.Quiet, "quiet", false, "run without prompts (used by the Windows Uninstall button)")
	cmd.Flags().BoolVar(&o.PurgeData, "purge-data", false, "also move ~/.hull aside (config, certs, services)")
	rootCmd.AddCommand(cmd)
}

// backupHullHome moves ~/.hull to the first free ~/.hull.bak name, so an older
// backup is never deleted. It returns "" when there is no ~/.hull to move.
func backupHullHome() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	src := filepath.Join(h, ".hull")
	if _, err := os.Stat(src); err != nil {
		return "", nil
	}
	bak := filepath.Join(h, ".hull.bak")
	for i := 1; ; i++ {
		if _, err := os.Lstat(bak); os.IsNotExist(err) {
			break
		}
		bak = filepath.Join(h, fmt.Sprintf(".hull.bak.%d", i))
	}
	return bak, os.Rename(src, bak)
}

// untrustCA removes Hull's local CA from the system and browser trust stores.
// It must run while ~/.hull still holds the certificate, so before any purge.
func untrustCA() {
	a, err := loadApp()
	if err != nil {
		return
	}
	if err := certs.UninstallTrust(a.Config.RouterDataDir()); err != nil {
		fmt.Println("  note: could not remove Hull's certificate from the trust stores:", err)
		return
	}
	fmt.Println("Removed Hull's certificate from the trust stores.")
}

// revertSystem undoes what setup changed outside Hull's own folders for name
// resolution (resolver files, the NRPT rule, the hosts block, the macOS
// loopback alias), printing the manual steps when elevation is unavailable.
func revertSystem() {
	tld := "test"
	if a, err := loadApp(); err == nil {
		tld = a.Config.TLD
	}
	if err := platform.RevertSystemChanges(tld); err != nil {
		var manual *platform.ManualStepsError
		if errors.As(err, &manual) {
			fmt.Println("Undoing the *." + tld + " name setup needs elevation; run:")
			fmt.Println("  " + strings.ReplaceAll(manual.Instructions, "\n", "\n  "))
		} else {
			fmt.Println("  note: could not undo the *."+tld+" name setup:", err)
		}
		return
	}
	fmt.Printf("Removed the *.%s name setup.\n", tld)
}
