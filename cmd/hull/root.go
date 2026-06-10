package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/engine"
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

func main() {
	rootCmd.SetVersionTemplate("hull {{.Version}}\n")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "hull:", err)
		os.Exit(1)
	}
}
