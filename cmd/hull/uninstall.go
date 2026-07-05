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
		Long: "Remove the Hull app from this machine: the binaries, the PATH entry,\n" +
			"shortcuts, and (on Windows) the Apps and Features entry. Your project\n" +
			"files are never touched, and by default your ~/.hull data (config,\n" +
			"certificates, shared service data) is left in place too.\n\n" +
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
