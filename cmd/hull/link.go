package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/templates"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "link <project> <engine>[@version]",
		Short: "Link a project to a shared service instance",
		Long: "Point a project at a shared service instance instead of a dedicated\n" +
			"per-project container.\n" +
			"\n" +
			"Pass the project name and an engine, optionally pinned with @version\n" +
			"(for example postgres@16). Linking sets the project's hull.yaml to\n" +
			"mode: shared, ensures the target instance is running, creates the\n" +
			"project's own database inside that instance, and rewrites the project's\n" +
			".env so it connects to the shared instance.\n" +
			"\n" +
			"The instance is auto-provisioned if it does not exist yet, so you do not\n" +
			"have to run hull services add first. When a daemon is running the link\n" +
			"routes through it (adding the instance when missing, then linking) and\n" +
			"restarts the project automatically; the in-process path does the same\n" +
			"work but prints a restart hint instead of restarting for you.\n" +
			"\n" +
			"The project's old dedicated database container (if any) is dropped from\n" +
			"its compose file on the next start; that container's volume is left in\n" +
			"place, not deleted.",
		Example: "  hull link myapp postgres@16\n" +
			"  hull link myapp redis\n" +
			"  hull link blog mariadb",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			def, version, err := services.Resolve(args[1])
			if err != nil {
				return err
			}
			instance := templates.InstanceName(def.Name, version)

			viaDaemon := false
			if err := a.withDaemon(
				func(c *api.Client) error {
					viaDaemon = true
					// The daemon's link endpoint requires the instance to
					// already exist; the in-process Link auto-provisions it, so
					// mirror that by adding it first when missing.
					existing, err := c.Services(cmd.Context())
					if err != nil {
						return err
					}
					found := false
					for _, in := range existing {
						if in.Name == instance || (in.Engine == def.Name && in.Version == version) {
							instance = in.Name
							found = true
							break
						}
					}
					if !found {
						job, err := c.AddService(cmd.Context(), api.AddServiceRequest{Engine: def.Name, Version: version})
						if err != nil {
							return err
						}
						if err := streamJob(cmd.Context(), c, job); err != nil {
							return err
						}
					}
					job, err := c.LinkService(cmd.Context(), instance, api.LinkRequest{Project: p.Name})
					if err != nil {
						return err
					}
					return streamJob(cmd.Context(), c, job)
				},
				func() error {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return err
					}
					name, err := a.Engine.Link(cmd.Context(), p, args[1], services.NewManager(a.Config))
					if err != nil {
						return err
					}
					instance = name
					return nil
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ %s linked to shared instance %s.\n", p.Name, instance)
			// The daemon path restarts the project as part of the link job; the
			// in-process path does not, so only it needs the restart hint.
			if !viaDaemon {
				fmt.Println("  Restart the project to apply: hull up", p.Name)
			}
			return nil
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "unlink <project> <service-key>",
		Short: "Remove a service (e.g. db, redis) from a project",
		Long: "Remove a service from a project by its key in hull.yaml.\n" +
			"\n" +
			"Pass the project name and the service key as it appears under services\n" +
			"in the project's hull.yaml (commonly db or redis, not the instance\n" +
			"name). Hull deletes that entry and regenerates compose.yaml so the\n" +
			"service is no longer wired into the project.\n" +
			"\n" +
			"When a daemon is running the unlink routes through it; otherwise it runs\n" +
			"in-process. This only detaches the project: if the service was a shared\n" +
			"instance, that instance and its data are never touched, and other linked\n" +
			"projects keep working. Restart the project afterward to apply the change.",
		Example: "  hull unlink myapp db\n" +
			"  hull unlink blog redis",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			if err := a.withDaemon(
				func(c *api.Client) error { return c.Unlink(cmd.Context(), p.Name, api.UnlinkRequest{Key: args[1]}) },
				func() error { return a.Engine.Unlink(cmd.Context(), p, args[1]) },
			); err != nil {
				return err
			}
			fmt.Printf("✔ %s unlinked from %q. Restart with: hull up %s\n", p.Name, args[1], p.Name)
			return nil
		},
	})
}
