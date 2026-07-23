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
		Long: "List every registered project and its current state.\n\n" +
			"Hull scans your configured roots and reconciles each project against\n" +
			"live Docker state. When a daemon is running the list comes from it;\n" +
			"otherwise Hull computes it in-process. Each row reports the project's\n" +
			"name, state (running, stopped, or broken when it reports an error),\n" +
			"kind (app, cluster, or legacy), routed URL (a dash when it serves no\n" +
			"domain), and absolute directory.\n\n" +
			"The default output is a NAME STATE KIND URL DIR table; pass --json for a\n" +
			"machine-readable array of ProjectInfo objects suited to scripting. If no\n" +
			"projects are found, Hull prints your roots and a hull new hint.",
		Example: "  hull list\n" +
			"  hull ls\n" +
			"  hull list --json",
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
			if flagJSON {
				return printJSON(infos)
			}
			if len(infos) == 0 {
				fmt.Printf("No projects found in %v.\nCreate one with: hull new <name> <template>\n", a.Config.Roots)
				return nil
			}

			// With the engine down every project reports Running=false, which used
			// to render as a confident "stopped" for everything. That is a wrong
			// answer, not a missing one, so say the state is unknown instead.
			engineDown := dockerx.EngineCheck(cmd.Context()) != nil

			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATE\tKIND\tURL\tDIR") // surfaced by Flush
			for _, p := range infos {
				stateStr := "stopped"
				if p.Running {
					stateStr = "running"
				}
				if engineDown {
					stateStr = "unknown"
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
			if err := w.Flush(); err != nil {
				return err
			}
			if engineDown {
				fmt.Println("\nDocker is not running, so the state column is unknown.")
			}
			return nil
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show running containers and ports",
		Long: "Show the running Docker containers and their published ports.\n\n" +
			"This runs docker ps directly and prints a NAMES STATUS PORTS table. It\n" +
			"is intentionally local and never routes through the daemon, so it shows\n" +
			"every container on the machine, not only the ones Hull manages. That\n" +
			"makes it handy for spotting a stray container holding a port or\n" +
			"confirming Docker itself is healthy.\n\n" +
			"For a Hull-centric view of just your projects and their URLs, use hull\n" +
			"list instead.",
		Example: "  hull status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return dockerx.Exec(cmd.Context(), "", "docker", "ps",
				"--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}")
		},
	})
}
