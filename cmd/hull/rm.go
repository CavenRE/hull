package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
)

func init() {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Destroy an environment and its data",
		Long:  "Stop and remove a project's containers and volumes, then delete its\ndirectory. This is irreversible — code and database are both destroyed.",
		Args:  cobra.ExactArgs(1),
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
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			if err := a.Engine.Destroy(cmd.Context(), p); err != nil {
				return err
			}
			fmt.Printf("✔ Environment %q removed.\n", p.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cmd)
}
