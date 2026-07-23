package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

func init() {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Destroy a project and its data",
		Long: "Stop and remove a project's containers and named volumes, then delete\n" +
			"its directory from disk.\n" +
			"\n" +
			"This is irreversible: both the code and the database are destroyed. There\n" +
			"is no undo and no bundle is written, so export the project first (hull\n" +
			"export <name>) if you might want it back.\n" +
			"\n" +
			"You are prompted to confirm unless you pass --force (or the global --yes).\n" +
			"When a daemon is running the removal streams as a job; otherwise it runs\n" +
			"in-process and Docker must be available.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			if !force {
				ok, err := confirm(fmt.Sprintf("Permanently destroy %q and ALL its data (code + database)?", p.Name))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Aborted.")
					return nil
				}
			}
			if err := a.withDaemon(
				func(c *api.Client) error {
					job, err := c.DeleteProject(cmd.Context(), p.Name)
					if err != nil {
						return err
					}
					return streamJob(cmd.Context(), c, job)
				},
				func() error {
					return a.Engine.Destroy(cmd.Context(), p)
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ Project %q removed.\n", p.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt (alias of --yes)")
	rootCmd.AddCommand(cmd)
}
