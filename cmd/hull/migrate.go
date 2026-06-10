package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/state"
)

func init() {
	var all bool
	cmd := &cobra.Command{
		Use:   "migrate [name]",
		Short: "Adopt bash-Hull (v1) projects into v2",
		Long: `Reconstruct a hull.yaml from a v1 project's compose file, back the old
file up as *.v1-backup, and regenerate the compose artifact. The project
.env is untouched — v2 keeps v1's service names for dedicated services.`,
		Example: `  hull migrate my-old-site
  hull migrate --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			var targets []*state.Project
			if all {
				projects, err := state.Scan(a.Config.Roots)
				if err != nil {
					return err
				}
				for i := range projects {
					if projects[i].Legacy {
						targets = append(targets, &projects[i])
					}
				}
				if len(targets) == 0 {
					fmt.Println("No legacy v1 projects found.")
					return nil
				}
			} else {
				if len(args) != 1 {
					return fmt.Errorf("pass a project name or --all")
				}
				p, err := a.findProject(args[0])
				if err != nil {
					return err
				}
				targets = append(targets, p)
			}

			for _, p := range targets {
				m, err := a.Engine.MigrateV1(p)
				if err != nil {
					return fmt.Errorf("%s: %w", p.Name, err)
				}
				fmt.Printf("✔ %s migrated (%s", p.Name, m.Template)
				if _, db, ok := m.DatabaseService(); ok {
					fmt.Printf(", db: %s", db.Engine)
				}
				fmt.Println(") — old compose saved as *.v1-backup")
			}
			fmt.Println("Restart migrated projects with: hull up <name>")
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "migrate every legacy project")
	rootCmd.AddCommand(cmd)
}
