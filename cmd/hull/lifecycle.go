package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/state"
)

func init() {
	var all bool
	up := &cobra.Command{
		Use:   "up [name...]",
		Short: "Start environments",
		Long:  "Start the current project, named projects, all projects (--all),\nor pick interactively when run outside a project.",
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
		Short: "Stop environments",
		Long:  "Stop the current project, named projects, all running projects\n(--all), or pick interactively when run outside a project.",
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
	down.Flags().BoolVar(&all, "all", false, "stop every running project")
	rootCmd.AddCommand(down)
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "restart [name]",
		Short: "Restart a project's containers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			return a.Engine.Restart(cmd.Context(), p)
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "logs [name]",
		Short: "Tail a project's container logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			return a.Engine.Logs(cmd.Context(), p, true)
		},
	})
}

// candidateLister produces the option list for the interactive picker.
type candidateLister func(cmd *cobra.Command, a *app) ([]string, error)

// availableProjects lists every project in the registry.
func availableProjects(_ *cobra.Command, a *app) ([]string, error) {
	projects, err := state.Scan(a.Config.Roots)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
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
		projects, err := state.Scan(a.Config.Roots)
		if err != nil {
			return nil, err
		}
		targets := make([]*state.Project, 0, len(projects))
		for i := range projects {
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
