package main

import (
	"fmt"

	"github.com/spf13/cobra"
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
		Long: `Uninstall the Hull app (binaries, PATH entry, shortcuts, and the
Apps & Features entry). Your project files are never touched.

This is what Windows' "Uninstall" button runs , it works from the install
directory, so it isn't blocked when a policy stops the NSIS uninstaller from
launching its temporary copy.`,
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
