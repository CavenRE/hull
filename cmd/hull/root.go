package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/groups"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/version"
)

var rootCmd = &cobra.Command{
	Use:           "hull",
	Short:         "Composable local development environment",
	Long:          "Hull provisions Docker-based local dev environments with automatic\nHTTPS domains, databases, and framework scaffolding.",
	Version:       version.String(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

// app bundles what every command needs.
type app struct {
	Config *config.Config
	Engine *engine.Engine
}

// loadApp loads global config and constructs the engine.
func loadApp() (*app, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	return &app{Config: cfg, Engine: engine.New(cfg)}, nil
}

// client returns a connected daemon client, or ok=false to operate
// in-process (the headless guarantee of ADR 0002/0006).
func (a *app) client() (*api.Client, bool) {
	return api.Connect(a.Config.HullHome)
}

// currentProject resolves the project for the working directory, or returns
// ok=false when outside any registered project.
func (a *app) currentProject() (*state.Project, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, false
	}
	return state.Current(a.Config.Roots, wd)
}

// findProject resolves a project by name with a friendly error.
func (a *app) findProject(name string) (*state.Project, error) {
	return state.Find(a.Config.Roots, name)
}

// configView returns the current config as the API shape, from the daemon
// when one is running (so it reflects live state) or from local config.yaml.
func (a *app) configView(ctx context.Context) (api.ConfigInfo, error) {
	if client, ok := a.client(); ok {
		if ci, err := client.Config(ctx); err == nil {
			return *ci, nil
		}
	}
	var ci api.ConfigInfo
	ci.TLD = a.Config.TLD
	ci.Roots = a.Config.Roots
	ci.Defaults.PHP = a.Config.Defaults.PHP
	ci.Defaults.Editor = a.Config.Defaults.Editor
	ci.Defaults.DBTool = a.Config.Defaults.DBTool
	return ci, nil
}

// saveConfig persists a full config view via the daemon (preferred, so the
// running daemon applies it live) or directly to config.yaml.
func (a *app) saveConfig(ctx context.Context, ci api.ConfigInfo) error {
	if client, ok := a.client(); ok {
		_, err := client.PutConfig(ctx, ci)
		return err
	}
	a.Config.TLD = ci.TLD
	a.Config.Roots = ci.Roots
	a.Config.Defaults.PHP = ci.Defaults.PHP
	a.Config.Defaults.Editor = ci.Defaults.Editor
	a.Config.Defaults.DBTool = ci.Defaults.DBTool
	return a.Config.Save()
}

// groupsView / saveGroups read and persist the virtual-group store, via the
// daemon when one is up or directly from groups.yaml.
func (a *app) groupsView(ctx context.Context) (*groups.Store, error) {
	if client, ok := a.client(); ok {
		return client.Groups(ctx)
	}
	return groups.Load(a.Config.HullHome)
}

func (a *app) saveGroups(ctx context.Context, s *groups.Store) error {
	if client, ok := a.client(); ok {
		return client.PutGroups(ctx, s)
	}
	return s.Save(a.Config.HullHome)
}

func main() {
	rootCmd.SetVersionTemplate("hull {{.Version}}\n")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "hull:", err)
		os.Exit(1)
	}
}
