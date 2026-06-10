package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/services"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "link <project> <engine>[@version]",
		Short: "Link a project to a shared service instance",
		Long: `Point a project at a shared instance instead of a dedicated container:
updates hull.yaml to mode: shared, boots the instance, creates the
project's database inside it, and rewires the project's .env.

The project's own database container (if any) is removed from its compose
file on the next start; its volume is not deleted.`,
		Example: `  hull link myapp postgres@16
  hull link myapp redis
  hull link blog mariadb`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			instance, err := a.Engine.Link(cmd.Context(), p, args[1], services.NewManager(a.Config))
			if err != nil {
				return err
			}
			fmt.Printf("✔ %s linked to shared instance %s.\n", p.Name, instance)
			fmt.Println("  Restart the project to apply: hull up", p.Name)
			return nil
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "unlink <project> <service-key>",
		Short: "Remove a service (e.g. db, redis) from a project",
		Long:  "Removes the service from hull.yaml and regenerates compose.yaml.\nShared instance data is never deleted by unlink.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			if err := a.Engine.Unlink(cmd.Context(), p, args[1]); err != nil {
				return err
			}
			fmt.Printf("✔ %s unlinked from %q. Restart with: hull up %s\n", p.Name, args[1], p.Name)
			return nil
		},
	})
}
