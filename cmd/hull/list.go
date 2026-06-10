package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/state"
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
			projects, err := state.Scan(a.Config.Roots)
			if err != nil {
				return err
			}
			if len(projects) == 0 {
				fmt.Printf("No projects found in %v.\nCreate one with: hull new <name> <template>\n", a.Config.Roots)
				return nil
			}

			running := map[string]bool{}
			if names, err := dockerx.RunningComposeProjects(cmd.Context()); err == nil {
				for _, n := range names {
					running[n] = true
				}
			}

			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATE\tKIND\tURL\tDIR") // surfaced by Flush
			for _, p := range projects {
				stateStr := "stopped"
				if running[p.Name] {
					stateStr = "running"
				}
				kind, url := describe(&p, a.Config.TLD)
				if p.Err != nil {
					stateStr = "broken"
					kind = "invalid manifest"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, stateStr, kind, url, p.Dir)
			}
			return w.Flush()
		},
	})
}

func describe(p *state.Project, tld string) (kind, url string) {
	switch {
	case p.Legacy:
		return "v1 (legacy)", "https://" + p.Name + "." + tld
	case p.Manifest == nil:
		return "-", "-"
	case p.Manifest.Type == "app":
		return "app", "-"
	default:
		return string(p.Manifest.Template), "https://" + p.Manifest.Domain + "." + tld
	}
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
