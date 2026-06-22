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
		Args:  cobra.NoArgs,
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
