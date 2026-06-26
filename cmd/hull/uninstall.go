package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// uninstallOpts is shared by the platform-specific runUninstall.
type uninstallOpts struct {
	Quiet     bool
	PurgeData bool
}

func init() {
	o := uninstallOpts{}
	var yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Hull from this machine",
		Long: `Uninstall the Hull app (binaries, PATH entry, shortcuts, and the
Apps & Features entry). Your project files are never touched.

This is what Windows' "Uninstall" button runs , it works from the install
directory, so it isn't blocked when a policy stops the NSIS uninstaller from
launching its temporary copy.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !o.Quiet && !yes {
				fmt.Print("Remove Hull from this machine? Your projects are untouched. [y/N] ")
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if s := strings.TrimSpace(strings.ToLower(line)); s != "y" && s != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			return runUninstall(o)
		},
	}
	cmd.Flags().BoolVar(&o.Quiet, "quiet", false, "run without prompts (used by the Windows Uninstall button)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&o.PurgeData, "purge-data", false, "also move ~/.hull aside (config, certs, services)")
	rootCmd.AddCommand(cmd)
}
