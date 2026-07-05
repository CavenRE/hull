package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "deps",
		Short: "Show dependency status (Docker + embedded components)",
		Long: "Show the status of the external and embedded pieces Hull depends on,\n" +
			"such as Docker and the components Hull ships with, as a table of name,\n" +
			"status, and detected version.\n\n" +
			"It routes through the daemon when one is running (asking it what it\n" +
			"sees), otherwise it detects dependencies in process. For anything that\n" +
			"is missing or stopped it prints a short blurb plus an install hint and a\n" +
			"link, so you know exactly what to install or start next.\n\n" +
			"Use this when a command complains that Docker is unavailable, or as a\n" +
			"quick preflight before creating your first project. Pass --json to get\n" +
			"a machine-readable array instead of the table.",
		Example: "  hull deps\n" +
			"  hull deps --json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			var deps []api.DependencyInfo
			if client, ok := a.client(); ok {
				deps, err = client.Dependencies(cmd.Context())
				if err != nil {
					return err
				}
			} else {
				deps = api.DetectDependencies(cmd.Context(), a.Config.TLD)
			}
			if flagJSON {
				return printJSON(deps)
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tVERSION")
			for _, d := range deps {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", d.Name, d.Status, dash(d.Version))
			}
			_ = w.Flush()
			for _, d := range deps {
				if d.Status == "missing" || d.Status == "stopped" {
					fmt.Printf("\n%s: %s\n", d.Name, d.Blurb)
					if d.InstallHint != "" {
						fmt.Printf("  install: %s\n", d.InstallHint)
					}
					if d.InstallURL != "" {
						fmt.Printf("  see: %s\n", d.InstallURL)
					}
				}
			}
			return nil
		},
	})
}
