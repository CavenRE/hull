package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
)

func init() {
	var all bool
	up := &cobra.Command{
		Use:   "up [name...]",
		Short: "Start projects",
		Long: "Start one or more Hull projects.\n\n" +
			"Targets are resolved in priority order: explicit names on the command\n" +
			"line, then --all (every registered project), then the current directory's\n" +
			"project, then an interactive picker when you are outside a project. For\n" +
			"each target Hull checks Docker, regenerates compose.yaml from hull.yaml\n" +
			"(the artifact always tracks the manifest for v2 projects), runs pre_up\n" +
			"hooks, ensures the caddy network exists, brings the compose project up,\n" +
			"records the start in the ledger, and runs post_up hooks.\n\n" +
			"When a daemon is running the work routes through it; otherwise Hull runs\n" +
			"the same steps in-process. Starting stops at the first target that errors.",
		Example: "  hull up\n" +
			"  hull up shop blog\n" +
			"  hull up --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			targets, err := resolveTargets(cmd, a, args, all, "Select projects to start", availableProjects)
			if err != nil {
				return err
			}
			// Sites are only served while the daemon is up; offer to start it.
			if err := ensureDaemonRunning(cmd.Context(), a.Config.HullHome); err != nil {
				return err
			}
			client, viaDaemon := a.client()
			for _, p := range targets {
				fmt.Printf("Starting %s...\n", p.Name)
				if viaDaemon {
					err = client.ProjectAction(cmd.Context(), p.Name, "start")
				} else {
					err = a.Engine.Up(cmd.Context(), p)
				}
				if err != nil {
					return fmt.Errorf("%s: %w", p.Name, err)
				}
			}
			return nil
		},
	}
	up.Flags().BoolVar(&all, "all", false, "start every project")
	rootCmd.AddCommand(up)
}

func init() {
	var all bool
	down := &cobra.Command{
		Use:   "down [name...]",
		Short: "Stop projects",
		Long: "Stop one or more running Hull projects.\n\n" +
			"Uses the same targeting as up (explicit names, then --all, then the\n" +
			"current project, then an interactive picker), except the picker lists\n" +
			"only running projects. For each target Hull runs pre_down hooks\n" +
			"best-effort (a cleanup failure never wedges the stop), runs docker\n" +
			"compose down, and records the stop in the ledger.\n\n" +
			"Containers stop but named volumes and project files are kept, so a\n" +
			"later hull up brings the project back with its data intact. Use --all to\n" +
			"stop every registered project, not just the ones currently running.",
		Example: "  hull down\n" +
			"  hull down shop blog\n" +
			"  hull down --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			targets, err := resolveTargets(cmd, a, args, all, "Select projects to stop", runningProjects)
			if err != nil {
				return err
			}
			client, viaDaemon := a.client()
			for _, p := range targets {
				fmt.Printf("Stopping %s...\n", p.Name)
				if viaDaemon {
					err = client.ProjectAction(cmd.Context(), p.Name, "stop")
				} else {
					err = a.Engine.Down(cmd.Context(), p)
				}
				if err != nil {
					return fmt.Errorf("%s: %w", p.Name, err)
				}
			}
			return nil
		},
	}
	down.Flags().BoolVar(&all, "all", false, "stop every registered project (not just running ones)")
	rootCmd.AddCommand(down)
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "restart [name]",
		Short: "Restart a project's containers",
		Long: "Restart a project by force-recreating its containers.\n\n" +
			"Operates on a single target: the name you pass, or the current\n" +
			"directory's project when you pass nothing. After checking Docker, Hull\n" +
			"force-recreates the containers (compose up --force-recreate for v2\n" +
			"projects, compose recreate for clusters) rather than a plain compose\n" +
			"restart.\n\n" +
			"The force-recreate is deliberate: it repairs config drift and reattaches\n" +
			"detached networks, which a plain restart cannot fix. Routes through the\n" +
			"daemon when one is running, else in-process. Prints nothing on success.\n" +
			"If a container is wedged badly enough that recreate does not recover it,\n" +
			"use hull repair.",
		Example: "  hull restart\n" +
			"  hull restart shop",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			if client, viaDaemon := a.client(); viaDaemon {
				return client.ProjectAction(cmd.Context(), p.Name, "restart")
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			return a.Engine.Restart(cmd.Context(), p)
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "repair [name]",
		Short: "Recreate a project from a clean slate (fixes a wedged/detached state; keeps data)",
		Long: "Recreate a project from a clean slate to recover a wedged or detached\n" +
			"state, keeping its data.\n\n" +
			"Operates on a single target (the name you pass, or the current project).\n" +
			"After checking Docker, Hull brings the project fully down (containers and\n" +
			"networks are removed; named volumes are kept) and then brings it back up.\n" +
			"Removing and recreating the networks is what makes repair stronger than\n" +
			"restart.\n\n" +
			"Reach for this when a half-finished start left a container detached or\n" +
			"wedged and a plain hull restart will not recover it. Routes through the\n" +
			"daemon when one is running, else in-process. A compose-down error is\n" +
			"printed to stderr but does not stop the bring-up. Your data volumes and\n" +
			"project files are never touched.",
		Example: "  hull repair\n" +
			"  hull repair shop",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			if client, viaDaemon := a.client(); viaDaemon {
				return client.ProjectAction(cmd.Context(), p.Name, "repair")
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			return a.Engine.Repair(cmd.Context(), p)
		},
	})
}

func init() {
	var service string
	logs := &cobra.Command{
		Use:   "logs [name]",
		Short: "Tail a project's (or a shared service instance's) container logs",
		Long: "Tail the container logs of a project or a shared service instance.\n\n" +
			"With no arguments Hull tails the current directory's project. Pass a\n" +
			"project name to tail that project, or --service <name> to tail a shared\n" +
			"service instance (for example postgres-16) instead. A project name and\n" +
			"--service are mutually exclusive, and passing neither outside a project\n" +
			"is an error.\n\n" +
			"Under the hood this streams docker compose logs --follow --no-color\n" +
			"--tail 200, so you get up to 200 lines of history and then a live feed.\n" +
			"When a daemon is running it delivers the stream as Server-Sent Events;\n" +
			"otherwise Hull streams directly. Press Ctrl-C to stop tailing.",
		Example: "  hull logs\n" +
			"  hull logs shop\n" +
			"  hull logs --service postgres-16",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if service != "" {
				if len(args) > 0 {
					return fmt.Errorf("pass either a project name or --service, not both")
				}
				return a.withDaemon(
					func(c *api.Client) error {
						return c.Logs(cmd.Context(), "", service, 200, func(l string) { fmt.Println(l) })
					},
					func() error { return serviceLogs(cmd.Context(), a, service) },
				)
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			return a.withDaemon(
				func(c *api.Client) error {
					return c.Logs(cmd.Context(), p.Name, "", 200, func(l string) { fmt.Println(l) })
				},
				func() error {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return err
					}
					return a.Engine.Logs(cmd.Context(), p, true)
				},
			)
		},
	}
	logs.Flags().StringVar(&service, "service", "", "tail a shared service instance instead of a project")
	rootCmd.AddCommand(logs)
}

// serviceLogs tails a shared instance's container logs in-process (the
// headless fallback for `hull logs --service`).
func serviceLogs(ctx context.Context, a *app, name string) error {
	if err := dockerx.EngineCheck(ctx); err != nil {
		return err
	}
	instances, err := services.NewManager(a.Config).List(ctx)
	if err != nil {
		return err
	}
	for _, in := range instances {
		if in.Name == name {
			return dockerx.Exec(ctx, in.Dir, "docker", "compose", "logs", "--follow", "--no-color")
		}
	}
	return fmt.Errorf("no shared instance %q", name)
}

// candidateLister produces the option list for the interactive picker.
type candidateLister func(cmd *cobra.Command, a *app) ([]string, error)

// availableProjects lists every startable project in the registry.
func availableProjects(_ *cobra.Command, a *app) ([]string, error) {
	projects, err := state.Scan(a.Config.Roots, a.Config.Projects...)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		if p.Unmanaged {
			continue // plain folders are import candidates, not targets
		}
		names = append(names, p.Name)
	}
	return names, nil
}

// runningProjects lists registry projects with running containers.
func runningProjects(cmd *cobra.Command, a *app) ([]string, error) {
	running, err := dockerx.RunningComposeProjects(cmd.Context())
	if err != nil {
		return nil, err
	}
	known, err := availableProjects(cmd, a)
	if err != nil {
		return nil, err
	}
	inRegistry := map[string]bool{}
	for _, n := range known {
		inRegistry[n] = true
	}
	var names []string
	for _, n := range running {
		if inRegistry[n] {
			names = append(names, n)
		}
	}
	return names, nil
}

// resolveTargets implements the shared up/down target logic: explicit names
// win; then --all; then the current directory's project; then a picker.
func resolveTargets(cmd *cobra.Command, a *app, names []string, all bool, pickTitle string, list candidateLister) ([]*state.Project, error) {
	if len(names) > 0 {
		var targets []*state.Project
		for _, n := range names {
			p, err := a.findProject(n)
			if err != nil {
				return nil, err
			}
			targets = append(targets, p)
		}
		return targets, nil
	}

	if all {
		projects, err := state.Scan(a.Config.Roots, a.Config.Projects...)
		if err != nil {
			return nil, err
		}
		targets := make([]*state.Project, 0, len(projects))
		for i := range projects {
			if projects[i].Unmanaged {
				continue
			}
			targets = append(targets, &projects[i])
		}
		return targets, nil
	}

	if p, ok := a.currentProject(); ok {
		return []*state.Project{p}, nil
	}

	options, err := list(cmd, a)
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("no projects found in %v", a.Config.Roots)
	}
	picked, err := pickMany(pickTitle, options)
	if err != nil {
		return nil, err
	}
	var targets []*state.Project
	for _, n := range picked {
		p, err := a.findProject(n)
		if err != nil {
			return nil, err
		}
		targets = append(targets, p)
	}
	return targets, nil
}

// oneTarget resolves a single project: by name, or the current directory's.
func oneTarget(a *app, args []string) (*state.Project, error) {
	if len(args) == 1 {
		return a.findProject(args[0])
	}
	if p, ok := a.currentProject(); ok {
		return p, nil
	}
	return nil, fmt.Errorf("run inside a project directory or pass a project name")
}
