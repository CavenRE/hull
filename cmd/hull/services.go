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
		Long: `Shared services are global, versioned instances (postgres-16,
mariadb-lts, redis) that multiple projects link to , lighter than one
database container per project. Versions run side by side.`,
	}

	svc.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List shared instances",
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
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
	rm.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt")
	svc.AddCommand(rm)

	rootCmd.AddCommand(svc)
}
