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
		Long: "Adopt legacy bash-Hull (v1) projects into the v2 layout so they can be\n" +
			"managed by this CLI.\n" +
			"\n" +
			"For each project Hull reconstructs a hull.yaml from the v1 compose file,\n" +
			"backs the old compose file up as *.v1-backup, and regenerates the\n" +
			"compose.yaml artifact. Detected template and database are reported per\n" +
			"project. The project .env is left untouched, and v2 keeps v1's existing\n" +
			"service names for dedicated services so containers line up.\n" +
			"\n" +
			"Pass a single project name, or --all to scan every configured root and\n" +
			"migrate all projects still flagged as legacy. This runs in-process only\n" +
			"(no daemon path). Nothing is started: after migrating, bring a project up\n" +
			"with hull up <name>.",
		Example: "  hull migrate my-old-site\n" +
			"  hull migrate --all",
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
				fmt.Println("); old compose saved as *.v1-backup")
			}
			fmt.Println("Restart migrated projects with: hull up <name>")
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "migrate every legacy project")
	rootCmd.AddCommand(cmd)
}
