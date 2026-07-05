package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/templates"
)

func init() {
	svc := &cobra.Command{
		Use:     "services",
		Aliases: []string{"service", "svc"},
		Short:   "Manage shared service instances (databases, redis)",
		Long: "Manage shared service instances: global, versioned Docker containers\n" +
			"(for example postgres-16, mariadb-lts, redis) that many projects link to\n" +
			"instead of each project running its own database container.\n" +
			"\n" +
			"One shared instance can hold many project databases side by side, and\n" +
			"multiple engine versions (postgres-15 and postgres-16, say) can run at\n" +
			"the same time without conflict. This is lighter on resources than one\n" +
			"database container per project and keeps data in one place.\n" +
			"\n" +
			"Subcommands: list (show every instance), add (create and start one),\n" +
			"start/stop (control an instance without losing its data), and rm\n" +
			"(destroy an instance and every database in it). Use hull link to point a\n" +
			"project at an instance once it exists.",
		Example: "  hull services list\n" +
			"  hull services add postgres@16\n" +
			"  hull services stop postgres-16",
	}

	svc.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List shared instances",
		Long: "List every shared service instance and its current state.\n" +
			"\n" +
			"Runs in-process (there is no daemon route for this lister), reading the\n" +
			"instances from the services manager built from your config. Each row\n" +
			"shows the instance name (for example postgres-16), whether it is running\n" +
			"or stopped, its container name, and the on-disk data directory.\n" +
			"\n" +
			"Use --json for a machine-readable array (Name, Running, Container, Dir).\n" +
			"When no instances exist, a hint shows how to add one.",
		Example: "  hull services list\n" +
			"  hull services list --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			instances, err := services.NewManager(a.Config).List(cmd.Context())
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(instances)
			}
			if len(instances) == 0 {
				fmt.Println("No shared instances. Add one with: hull services add postgres@16")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATE\tCONTAINER\tDIR")
			for _, in := range instances {
				stateStr := "stopped"
				if in.Running {
					stateStr = "running"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", in.Name, stateStr, in.Container, in.Dir)
			}
			return w.Flush()
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "add <engine>[@version]",
		Short: "Create and start a shared instance (e.g. postgres@16)",
		Long: "Create and start a shared service instance.\n" +
			"\n" +
			"Pass exactly one engine, optionally pinned to a version with @version\n" +
			"(for example postgres@16). The engine and version are resolved to an\n" +
			"instance name like postgres-16. When a daemon is running the work routes\n" +
			"through it and job output is streamed; otherwise Hull checks that the\n" +
			"Docker engine is reachable and creates the instance in-process.\n" +
			"\n" +
			"The instance starts empty. Once it is up, attach a project to it with\n" +
			"hull link <project> <engine>[@version], which creates that project's\n" +
			"database inside the shared instance. Adding an instance that already\n" +
			"exists simply ensures it is running.",
		Example: "  hull services add postgres@16\n" +
			"  hull services add mariadb\n" +
			"  hull services add redis",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			def, version, err := services.Resolve(args[0])
			if err != nil {
				return err
			}
			if err := a.withDaemon(
				func(c *api.Client) error {
					job, err := c.AddService(cmd.Context(), api.AddServiceRequest{Engine: def.Name, Version: version})
					if err != nil {
						return err
					}
					return streamJob(cmd.Context(), c, job)
				},
				func() error {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return err
					}
					_, err := services.NewManager(a.Config).Add(cmd.Context(), def.Name, version)
					return err
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ Shared instance %s is up. Link a project with: hull link <project> %s\n", templates.InstanceName(def.Name, version), args[0])
			return nil
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "start <instance>",
		Short: "Start a shared instance",
		Long: "Start a shared service instance that was previously stopped.\n" +
			"\n" +
			"Pass the instance name (for example postgres-16), not the engine spec;\n" +
			"run hull services list to see the available names. When a daemon is\n" +
			"running the start routes through it and streams job output; otherwise\n" +
			"Hull checks the Docker engine and starts the container in-process.\n" +
			"\n" +
			"Data is preserved across stop and start, so this just brings a dormant\n" +
			"instance back online. On success there is no output on the in-process\n" +
			"path.",
		Example: "  hull services start postgres-16\n" +
			"  hull services start redis",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			return a.withDaemon(
				func(c *api.Client) error { return c.ServiceAction(cmd.Context(), args[0], "start") },
				func() error {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return err
					}
					return services.NewManager(a.Config).Start(cmd.Context(), args[0])
				},
			)
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "stop <instance>",
		Short: "Stop a shared instance (data preserved)",
		Long: "Stop a running shared service instance without losing its data.\n" +
			"\n" +
			"Pass the instance name (for example postgres-16); run hull services list\n" +
			"to see the names. When a daemon is running the stop routes through it and\n" +
			"streams job output; otherwise Hull checks the Docker engine and stops the\n" +
			"container in-process.\n" +
			"\n" +
			"Only the container is stopped: the instance's data volume is retained, so\n" +
			"every database inside it survives and hull services start brings it back\n" +
			"exactly as it was. To destroy the data instead, use hull services rm.",
		Example: "  hull services stop postgres-16\n" +
			"  hull services stop mariadb-lts",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			return a.withDaemon(
				func(c *api.Client) error { return c.ServiceAction(cmd.Context(), args[0], "stop") },
				func() error {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return err
					}
					return services.NewManager(a.Config).Stop(cmd.Context(), args[0])
				},
			)
		},
	})

	var force bool
	rm := &cobra.Command{
		Use:   "rm <instance>",
		Short: "Destroy a shared instance and ALL its databases",
		Long: "Destroy a shared service instance and every database stored in it.\n" +
			"\n" +
			"This is irreversible and affects every project linked to the instance:\n" +
			"the container and its data volume are both removed, so all databases\n" +
			"inside it are gone. Unless you pass --force (or the global --yes), Hull\n" +
			"prompts for confirmation first; the prompt only appears on an interactive\n" +
			"terminal.\n" +
			"\n" +
			"When a daemon is running the removal routes through it; otherwise Hull\n" +
			"checks the Docker engine and removes the instance in-process. Linked\n" +
			"projects keep their mode: shared setting in hull.yaml, so re-adding the\n" +
			"instance and re-linking is possible, but the old data is not recoverable.",
		Example: "  hull services rm postgres-16\n" +
			"  hull services rm redis --force",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if !force {
				ok, err := confirm(fmt.Sprintf("Destroy instance %q and every database in it?", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Aborted.")
					return nil
				}
			}
			return a.withDaemon(
				func(c *api.Client) error { return c.RemoveService(cmd.Context(), args[0]) },
				func() error {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return err
					}
					return services.NewManager(a.Config).Remove(cmd.Context(), args[0])
				},
			)
		},
	}
	rm.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt (alias of --yes)")
	svc.AddCommand(rm)

	rootCmd.AddCommand(svc)
}
