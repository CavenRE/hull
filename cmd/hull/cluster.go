package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/state"
)

func init() {
	cluster := &cobra.Command{
		Use:   "cluster",
		Short: "Adopt and inspect multi-container clusters",
		Long: `A cluster is a Hull project that wraps an existing docker compose
project (many containers managed as one unit). Lifecycle uses the normal verbs:
hull up/down/restart/rebuild/reset <name> all operate on the whole cluster.`,
	}

	var (
		root     string
		files    []string
		profiles []string
		name     string
	)
	add := &cobra.Command{
		Use:   "add <dir>",
		Short: "Adopt an existing compose project as a cluster",
		Args:  cobra.ExactArgs(1),
		Example: `  hull cluster add ./my-stack --root core
  hull cluster add . --profile dev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			req := api.AdoptClusterRequest{Dir: args[0], Name: name, ComposeRoot: root, ComposeFiles: files, Profiles: profiles}
			var created string
			if client, ok := a.client(); ok {
				created, err = client.AdoptCluster(cmd.Context(), req)
			} else {
				mf, e := a.Engine.AdoptCluster(engine.ClusterOptions{Dir: req.Dir, Name: req.Name, ComposeRoot: req.ComposeRoot, ComposeFiles: req.ComposeFiles, Profiles: req.Profiles})
				err = e
				if e == nil {
					created = mf.Name
				}
			}
			if err != nil {
				return err
			}
			fmt.Printf("✔ cluster %q adopted. Start it with: hull up %s\n", created, created)
			return nil
		},
	}
	add.Flags().StringVar(&root, "root", "", "compose root within the cluster (e.g. core)")
	add.Flags().StringVar(&name, "name", "", "cluster name (default: folder name)")
	add.Flags().StringArrayVar(&files, "compose", nil, "extra -f compose file (repeatable)")
	add.Flags().StringArrayVar(&profiles, "profile", nil, "active compose profile (repeatable)")
	cluster.AddCommand(add)

	cluster.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List adopted clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			projects, err := state.Scan(a.Config.Roots)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tROOT\tROUTES\tDIR")
			found := false
			for i := range projects {
				m := projects[i].Manifest
				if m == nil || m.Type != "cluster" {
					continue
				}
				found = true
				_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", m.Name, m.ComposeRoot, len(m.Routes), projects[i].Dir)
			}
			if !found {
				fmt.Println("No clusters. Adopt one with: hull cluster add <dir> --root <subdir>")
				return nil
			}
			return w.Flush()
		},
	})

	rootCmd.AddCommand(cluster)
}
