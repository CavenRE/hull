package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/dockerx"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registered projects and their state",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}

			var infos []api.ProjectInfo
			if client, ok := a.client(); ok {
				infos, err = client.Projects(cmd.Context())
			} else {
				infos, err = api.ProjectList(cmd.Context(), a.Config, dockerx.RunningComposeProjects)
			}
			if err != nil {
				return err
			}
			if len(infos) == 0 {
				fmt.Printf("No projects found in %v.\nCreate one with: hull new <name> <template>\n", a.Config.Roots)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATE\tKIND\tURL\tDIR") // surfaced by Flush
			for _, p := range infos {
				stateStr := "stopped"
				if p.Running {
					stateStr = "running"
				}
				if p.Error != "" {
					stateStr = "broken"
				}
				url := p.URL
				if url == "" {
					url = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, stateStr, p.Kind, url, p.Dir)
			}
			return w.Flush()
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show running containers and ports",
		RunE: func(cmd *cobra.Command, args []string) error {
			return dockerx.Exec(cmd.Context(), "", "docker", "ps",
				"--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}")
		},
	})
}
